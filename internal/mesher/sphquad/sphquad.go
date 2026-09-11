// Package sphquad implements the thesis method: an adaptive QuadTree background
// grid, the scalar fields phi (signed distance), psi (distance to singular
// corners) and kappa (boundary curvature), a target element-size function
// h_t(phi, psi, kappa), SPH relaxation of a particle cloud toward that size
// distribution, and a final Delaunay triangulation. The geometry is normalised
// to a unit bounding box, so all size parameters are fractions of the domain.
//
// Remaining MVP simplifications vs the thesis: phi is exact rather than
// narrow-band + Hamilton-Jacobi reinitialised; kappa is sampled by
// nearest-boundary lookup rather than diffused inward on the grid; the SPH step
// keeps only the pressure term (no viscosity, no Shepard boundary
// renormalisation) and pins boundary particles instead of evolving them.
package sphquad

import (
	"context"
	"math"
	"math/rand"

	"MTBuild/internal/delaunay"
	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
	"MTBuild/internal/mesher/sphquad/field"
	"MTBuild/internal/mesher/sphquad/quadtree"
	"MTBuild/internal/mesher/sphquad/sph"
)

func init() { mesher.Register("sph-quadtree", func() mesher.Mesher { return Mesher{} }) }

type Mesher struct{}

func (Mesher) Name() string { return "sph-quadtree" }

// Parameters are expressed as fractions of the geometry's bounding-box size
// (curvature parameters are 1/fraction).
var (
	pHFar   = mesher.ParamSpec{Key: "h_max", Label: "Element size, interior", Min: 0.01, Max: 0.5, Def: 0.09, Step: 0.005}
	pHMaxS  = mesher.ParamSpec{Key: "h_max_s", Label: "Element size at a flat boundary", Min: 0.005, Max: 0.4, Def: 0.06, Step: 0.005}
	pHMinS  = mesher.ParamSpec{Key: "h_min_s", Label: "Element size at a sharp boundary", Min: 0.003, Max: 0.3, Def: 0.02, Step: 0.005}
	pGrade  = mesher.ParamSpec{Key: "grade", Label: "Grading distance (surface->interior)", Min: 0.02, Max: 1.0, Def: 0.25, Step: 0.02}
	pKappaA = mesher.ParamSpec{Key: "kappa_a", Label: "Curvature reaching h_min_s (0 disables)", Min: 0, Max: 500, Def: 15, Step: 1}
	pSharp  = mesher.ParamSpec{Key: "sharp_deg", Label: "Turning angle counted as a singularity", Min: 5, Max: 90, Def: 25, Step: 5}
	pIters  = mesher.ParamSpec{Key: "iters", Label: "Max SPH iterations", Min: 10, Max: 3000, Def: 250, Step: 10}
)

func (Mesher) Params() []mesher.ParamSpec {
	return []mesher.ParamSpec{pHFar, pHMaxS, pHMinS, pGrade, pKappaA, pSharp, pIters}
}

func (Mesher) Generate(ctx context.Context, g geometry.Geometry, p mesher.Params, sink mesher.Sink) (mesh.Mesh, mesh.QualityMetrics, error) {
	hFar := p.Get(pHFar)
	hMaxS := math.Min(p.Get(pHMaxS), hFar)
	hMinS := math.Min(p.Get(pHMinS), hMaxS)
	grade := p.Get(pGrade)
	kappaA := p.Get(pKappaA)
	sharpDeg := p.Get(pSharp)
	iters := int(p.Get(pIters))

	// --- normalise to a unit bounding box --------------------------------
	min, max := g.BoundingBox()
	cx, cy := (min.X+max.X)/2, (min.Y+max.Y)/2
	scale := math.Max(max.X-min.X, max.Y-min.Y)
	if scale == 0 {
		return mesh.Mesh{}, mesh.QualityMetrics{}, errDegenerate
	}
	norm := func(pt geometry.Point) geometry.Point {
		return geometry.Point{X: (pt.X - cx) / scale, Y: (pt.Y - cy) / scale}
	}
	denorm := func(pt geometry.Point) geometry.Point {
		return geometry.Point{X: pt.X*scale + cx, Y: pt.Y*scale + cy}
	}
	gn := mapGeometry(g, norm)

	// --- fields (phi, psi, kappa) -------------------------------------
	sink.Progress("fields", 0)
	fields := field.Analyze(gn, sharpDeg, grade)
	sz := sizer{f: fields, hFar: hFar, hMaxS: hMaxS, hMinS: hMinS, grade: grade, kappaA: kappaA}
	sizeAt := sz.at
	sink.Progress("fields", 1)

	// --- QuadTree ----------------------------------------------------
	sink.Progress("quadtree", 0)
	tree := quadtree.Build(gn, quadtree.Options{
		MaxDepth: 9,
		HNear:    hMinS,
		HFar:     hFar,
		Band:     grade,
	})
	tree.EachLeaf(func(n *quadtree.Node) {
		c := geometry.Point{X: n.CX, Y: n.CY}
		n.Phi = fields.Phi(c)
		n.Ht = sizeAt(c)
	})
	sink.Progress("quadtree", 1)

	// --- particle initialisation --------------------------------------
	sink.Progress("init", 0)
	contours := []sph.Contour{sph.NewContour(gn.Outer)}
	for _, h := range gn.Holes {
		contours = append(contours, sph.NewContour(h))
	}
	rng := rand.New(rand.NewSource(1))
	parts := initParticles(gn, contours, sizeAt, hMinS, rng)
	if err := ctx.Err(); err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	sink.Progress("init", 1)
	sink.Snapshot(mesher.Frame{Stage: "init", Particles: denormAll(parts, denorm)})

	// --- SPH relaxation ----------------------------------------------
	dom := sph.Domain{
		Contains: gn.Contains,
		Signed:   func(pt geometry.Point) float64 { return field.SignedDistance(gn, pt) },
	}
	res, err := sph.Relax(ctx, parts, sizeAt, dom, contours,
		sph.Config{Iterations: iters, Seed: 1},
		func(it int, ps []sph.Particle) {
			sink.Progress("sph-relax", float64(it)/float64(iters))
			sink.Snapshot(mesher.Frame{Stage: "sph-relax", Iter: it, Particles: denormAll(ps, denorm)})
		})
	if err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	sink.Progress("sph-relax", 1)

	// --- triangulate + trim ----------------------------------------
	sink.Progress("triangulate", 0)
	normPos := make([]geometry.Point, 0, len(res.Particles))
	for _, pt := range res.Particles {
		normPos = append(normPos, pt.Pos)
	}
	normPos = dedupe(normPos, 0.2*hMinS) // drop points the relaxation collapsed together
	worldPos := make([]geometry.Point, len(normPos))
	for i, pt := range normPos {
		worldPos[i] = denorm(pt)
	}
	tris, err := delaunay.Triangulate(worldPos)
	if err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	kept := tris[:0]
	for _, t := range tris {
		c := geometry.Point{
			X: (normPos[t[0]].X + normPos[t[1]].X + normPos[t[2]].X) / 3,
			Y: (normPos[t[0]].Y + normPos[t[1]].Y + normPos[t[2]].Y) / 3,
		}
		if gn.Contains(c) {
			kept = append(kept, t)
		}
	}
	m := mesh.Mesh{Vertices: worldPos, Triangles: kept}
	sink.Progress("triangulate", 1)
	sink.Snapshot(mesher.Frame{Stage: "done", Iter: res.Iters, Particles: worldPos, Mesh: &m})

	return m, mesh.Evaluate(m), nil
}

// sizer turns the (phi, psi, kappa) fields into a target element size h_t.
//
// At the boundary the size depends on curvature: h_max_s where flat, shrinking
// toward h_min_s as the corrected curvature approaches kappa_a (so it is
// h_min_s at singularities). Moving inward, the size ramps from that surface
// value up to h_max over the grading distance.
type sizer struct {
	f                  *field.Fields
	hFar, hMaxS, hMinS float64
	grade, kappaA      float64
}

func (s sizer) surfaceSize(p geometry.Point) float64 {
	if s.kappaA <= 0 {
		return s.hMaxS
	}
	kc := s.f.KappaCorrected(p, s.kappaA)
	t := kc / s.kappaA
	if t > 1 {
		t = 1
	}
	return s.hMaxS - (s.hMaxS-s.hMinS)*t
}

func (s sizer) at(p geometry.Point) float64 {
	hs := s.surfaceSize(p)
	if s.grade <= 0 {
		return hs
	}
	t := math.Abs(s.f.Phi(p)) / s.grade
	if t > 1 {
		t = 1
	}
	return hs + (s.hFar-hs)*t
}

// dedupe removes points that lie within tol of an earlier point, using a
// uniform hash so it stays linear.
func dedupe(pts []geometry.Point, tol float64) []geometry.Point {
	if tol <= 0 {
		return pts
	}
	type key struct{ x, y int }
	seen := make(map[key][]geometry.Point)
	out := pts[:0]
	for _, p := range pts {
		k := key{int(math.Floor(p.X / tol)), int(math.Floor(p.Y / tol))}
		dup := false
		for dx := -1; dx <= 1 && !dup; dx++ {
			for dy := -1; dy <= 1 && !dup; dy++ {
				for _, q := range seen[key{k.x + dx, k.y + dy}] {
					if p.Dist(q) < tol {
						dup = true
					}
				}
			}
		}
		if !dup {
			seen[k] = append(seen[k], p)
			out = append(out, p)
		}
	}
	return out
}

func denormAll(ps []sph.Particle, denorm func(geometry.Point) geometry.Point) []geometry.Point {
	out := make([]geometry.Point, len(ps))
	for i, p := range ps {
		out[i] = denorm(p.Pos)
	}
	return out
}

func mapGeometry(g geometry.Geometry, f func(geometry.Point) geometry.Point) geometry.Geometry {
	mapPl := func(pl geometry.Polyline) geometry.Polyline {
		out := make(geometry.Polyline, len(pl))
		for i, p := range pl {
			out[i] = f(p)
		}
		return out
	}
	out := geometry.Geometry{Outer: mapPl(g.Outer)}
	for _, h := range g.Holes {
		out.Holes = append(out.Holes, mapPl(h))
	}
	return out
}

type degenerateErr struct{}

func (degenerateErr) Error() string { return "sphquad: degenerate geometry (zero-size bounding box)" }

var errDegenerate = degenerateErr{}
