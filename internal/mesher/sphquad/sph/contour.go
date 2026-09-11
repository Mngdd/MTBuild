package sph

import (
	"math"
	"sort"

	"MTBuild/internal/geometry"
)

// Contour is a closed boundary polyline parameterised by arc length. Surface
// particles live on it as a scalar position s in [0, Perimeter); this keeps
// them from jumping across a concave notch or piling up at a corner, which a
// global nearest-point projection would allow.
//
// After geometry.Normalized the outer contour is CCW and holes are CW, so the
// left normal of the forward direction always points into the domain.
type Contour struct {
	pts   geometry.Polyline
	cum   []float64 // cum[i] = arc length from pts[0] to pts[i]; len = len(pts)+1
	perim float64
}

// NewContour wraps a normalised contour polyline.
func NewContour(pl geometry.Polyline) Contour {
	c := Contour{pts: pl, cum: make([]float64, len(pl)+1)}
	for i := 0; i < len(pl); i++ {
		c.cum[i+1] = c.cum[i] + pl[i].Dist(pl[(i+1)%len(pl)])
	}
	c.perim = c.cum[len(pl)]
	return c
}

// Perimeter is the total arc length.
func (c Contour) Perimeter() float64 { return c.perim }

// Wrap maps s into [0, Perimeter).
func (c Contour) Wrap(s float64) float64 {
	if c.perim == 0 {
		return 0
	}
	s = math.Mod(s, c.perim)
	if s < 0 {
		s += c.perim
	}
	return s
}

// At returns the point at arc length s, plus the unit inward normal and unit
// forward tangent of the segment there.
func (c Contour) At(s float64) (pos, normal, tangent geometry.Point) {
	s = c.Wrap(s)
	// Segment i is [cum[i], cum[i+1]).
	i := sort.SearchFloat64s(c.cum, s)
	if i > 0 && (i == len(c.cum) || c.cum[i] > s) {
		i--
	}
	if i >= len(c.pts) {
		i = len(c.pts) - 1
	}
	a := c.pts[i]
	b := c.pts[(i+1)%len(c.pts)]
	segLen := c.cum[i+1] - c.cum[i]
	t := 0.0
	if segLen > 0 {
		t = (s - c.cum[i]) / segLen
	}
	pos = geometry.Point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		l = 1
	}
	tangent = geometry.Point{X: dx / l, Y: dy / l}
	normal = geometry.Point{X: -tangent.Y, Y: tangent.X}
	return pos, normal, tangent
}

// ArcLengthOf returns the arc length of the point on the contour closest to p.
func (c Contour) ArcLengthOf(p geometry.Point) float64 {
	best := math.Inf(1)
	bestS := 0.0
	for i := 0; i < len(c.pts); i++ {
		a := c.pts[i]
		b := c.pts[(i+1)%len(c.pts)]
		dx, dy := b.X-a.X, b.Y-a.Y
		l2 := dx*dx + dy*dy
		t := 0.0
		if l2 > 0 {
			t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
		}
		proj := geometry.Point{X: a.X + t*dx, Y: a.Y + t*dy}
		if d := p.Dist(proj); d < best {
			best = d
			bestS = c.cum[i] + t*(c.cum[i+1]-c.cum[i])
		}
	}
	return bestS
}
