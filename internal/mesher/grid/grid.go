// Package grid is a trivial reference mesher: lay points on a regular lattice,
// add resampled boundary points, run Delaunay, drop triangles outside the
// domain. It exists to exercise the pipeline end to end and to give the SPH
// mesher something to be compared against. It is not adaptive.
package grid

import (
	"context"
	"math"

	"MTBuild/internal/delaunay"
	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
)

func init() { mesher.Register("grid", func() mesher.Mesher { return Mesher{} }) }

type Mesher struct{}

func (Mesher) Name() string { return "grid" }

var paramH = mesher.ParamSpec{
	Key: "h", Label: "Target edge length", Min: 1e-4, Max: 1e9, Def: 0.05, Step: 0.005,
}

func (Mesher) Params() []mesher.ParamSpec { return []mesher.ParamSpec{paramH} }

func (Mesher) Generate(ctx context.Context, g geometry.Geometry, p mesher.Params, sink mesher.Sink) (mesh.Mesh, mesh.QualityMetrics, error) {
	h := p.Get(paramH)

	sink.Progress("sample", 0)
	pts := resampleBoundary(g.Outer, h)
	for _, hole := range g.Holes {
		pts = append(pts, resampleBoundary(hole, h)...)
	}
	pts = append(pts, interiorLattice(g, h)...)
	if err := ctx.Err(); err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	sink.Progress("sample", 1)
	sink.Snapshot(mesher.Frame{Stage: "sample", Particles: pts})

	sink.Progress("triangulate", 0)
	tris, err := delaunay.Triangulate(pts)
	if err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}

	kept := tris[:0]
	for _, t := range tris {
		c := centroid(pts, t)
		if g.Contains(c) {
			kept = append(kept, t)
		}
	}
	m := mesh.Mesh{Vertices: pts, Triangles: kept}
	sink.Progress("triangulate", 1)
	sink.Snapshot(mesher.Frame{Stage: "triangulate", Particles: pts, Mesh: &m})

	return m, mesh.Evaluate(m), nil
}

// resampleBoundary walks a closed contour, emitting points spaced ~step apart.
func resampleBoundary(pl geometry.Polyline, step float64) []geometry.Point {
	if len(pl) < 2 || step <= 0 {
		return append([]geometry.Point(nil), pl...)
	}
	var out []geometry.Point
	carry := 0.0
	for i := 0; i < len(pl); i++ {
		a := pl[i]
		b := pl[(i+1)%len(pl)]
		seg := a.Dist(b)
		if seg == 0 {
			continue
		}
		dx, dy := (b.X-a.X)/seg, (b.Y-a.Y)/seg
		for d := carry; d < seg; d += step {
			out = append(out, geometry.Point{X: a.X + dx*d, Y: a.Y + dy*d})
		}
		carry = math.Mod(carry-seg, step)
		if carry < 0 {
			carry += step
		}
	}
	return out
}

// interiorLattice returns lattice points strictly inside the domain, inset by
// half a step from the boundary to avoid near-duplicate points.
func interiorLattice(g geometry.Geometry, step float64) []geometry.Point {
	min, max := g.BoundingBox()
	var out []geometry.Point
	for y := min.Y + step/2; y < max.Y; y += step {
		for x := min.X + step/2; x < max.X; x += step {
			p := geometry.Point{X: x, Y: y}
			if g.Contains(p) && edgeClearance(g, p) > step/2 {
				out = append(out, p)
			}
		}
	}
	return out
}

func edgeClearance(g geometry.Geometry, p geometry.Point) float64 {
	d := distToPolyline(g.Outer, p)
	for _, h := range g.Holes {
		if dh := distToPolyline(h, p); dh < d {
			d = dh
		}
	}
	return d
}

func distToPolyline(pl geometry.Polyline, p geometry.Point) float64 {
	best := math.Inf(1)
	for i := 0; i < len(pl); i++ {
		a := pl[i]
		b := pl[(i+1)%len(pl)]
		if d := distToSegment(p, a, b); d < best {
			best = d
		}
	}
	return best
}

func distToSegment(p, a, b geometry.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return p.Dist(a)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return p.Dist(geometry.Point{X: a.X + t*dx, Y: a.Y + t*dy})
}

func centroid(pts []geometry.Point, t [3]int) geometry.Point {
	a, b, c := pts[t[0]], pts[t[1]], pts[t[2]]
	return geometry.Point{X: (a.X + b.X + c.X) / 3, Y: (a.Y + b.Y + c.Y) / 3}
}
