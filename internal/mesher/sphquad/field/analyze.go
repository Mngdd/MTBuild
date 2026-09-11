package field

import (
	"math"

	"MTBuild/internal/geometry"
)

// Fields carries the boundary-derived scalar data the sizing function needs:
// phi (signed distance), psi (distance to a singularity) and kappa (boundary
// curvature). phi is exact; psi and kappa are precomputed on the boundary and
// sampled elsewhere by nearest-boundary lookup, which approximates the inward
// diffusion the thesis performs (see example_info.md 2.4).
type Fields struct {
	g geometry.Geometry

	// per boundary sample, in traversal order over Outer then each hole
	pts   []geometry.Point
	kappa []float64 // |curvature| at that sample, 1/length

	singular []geometry.Point // sharp corners
	psiMax   float64          // influence range of a singularity
}

// Analyze precomputes the fields. sharpDeg is the turning-angle threshold (in
// degrees) above which a boundary vertex counts as a singularity; psiMax is the
// distance beyond which a singularity no longer affects sizing.
func Analyze(g geometry.Geometry, sharpDeg, psiMax float64) *Fields {
	f := &Fields{g: g, psiMax: math.Max(psiMax, 1e-9)}
	f.addContour(g.Outer, sharpDeg)
	for _, h := range g.Holes {
		f.addContour(h, sharpDeg)
	}
	return f
}

func (f *Fields) addContour(pl geometry.Polyline, sharpDeg float64) {
	n := len(pl)
	if n < 3 {
		return
	}
	thr := sharpDeg * math.Pi / 180
	for i := 0; i < n; i++ {
		prev := pl[(i-1+n)%n]
		cur := pl[i]
		next := pl[(i+1)%n]

		e0 := geometry.Point{X: cur.X - prev.X, Y: cur.Y - prev.Y}
		e1 := geometry.Point{X: next.X - cur.X, Y: next.Y - cur.Y}
		l0, l1 := math.Hypot(e0.X, e0.Y), math.Hypot(e1.X, e1.Y)
		if l0 == 0 || l1 == 0 {
			continue
		}
		// Exterior turning angle at this vertex.
		cross := e0.X*e1.Y - e0.Y*e1.X
		dot := e0.X*e1.X + e0.Y*e1.Y
		turn := math.Atan2(cross, dot) // signed, (-pi, pi]

		// Discrete curvature magnitude: turning angle over the local edge length.
		k := math.Abs(turn) / (0.5 * (l0 + l1))
		f.pts = append(f.pts, cur)
		f.kappa = append(f.kappa, k)

		if math.Abs(turn) >= thr {
			f.singular = append(f.singular, cur)
		}
	}
}

// Phi is the signed distance to the boundary (>0 inside).
func (f *Fields) Phi(p geometry.Point) float64 { return SignedDistance(f.g, p) }

// Psi is the distance to the nearest singularity, or psiMax if there are none.
func (f *Fields) Psi(p geometry.Point) float64 {
	if len(f.singular) == 0 {
		return f.psiMax
	}
	best := math.Inf(1)
	for _, s := range f.singular {
		if d := p.Dist(s); d < best {
			best = d
		}
	}
	return best
}

// Kappa is the |curvature| of the nearest boundary sample.
func (f *Fields) Kappa(p geometry.Point) float64 {
	if len(f.pts) == 0 {
		return 0
	}
	best := math.Inf(1)
	bk := 0.0
	for i, q := range f.pts {
		if d := p.Dist(q); d < best {
			best, bk = d, f.kappa[i]
		}
	}
	return bk
}

// KappaCorrected applies the thesis singularity fix: near a singularity the
// real (diverging) curvature is replaced by the artificial ceiling kappaA,
// blended out linearly over psiMax.
//
//	kappa_final = max( kappa, (1 - psi/psiMax) * kappaA )
func (f *Fields) KappaCorrected(p geometry.Point, kappaA float64) float64 {
	k := f.Kappa(p)
	if kappaA <= 0 {
		return k
	}
	psi := f.Psi(p) / f.psiMax
	if psi > 1 {
		psi = 1
	}
	if art := (1 - psi) * kappaA; art > k {
		return art
	}
	return k
}

// Singularities returns the detected sharp corners (for visualisation/tests).
func (f *Fields) Singularities() []geometry.Point { return f.singular }
