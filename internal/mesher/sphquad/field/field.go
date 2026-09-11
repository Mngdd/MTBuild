// Package field computes the scalar fields the sizing function needs. For the
// MVP only phi (signed distance) is real; psi and kappa from the thesis are
// left for later and treated as absent (kappa = 0).
package field

import (
	"math"

	"MTBuild/internal/geometry"
)

// SignedDistance returns the signed distance from p to the geometry boundary:
// positive inside the domain, negative outside, magnitude = Euclidean distance
// to the nearest edge. Computed by brute force over all segments — fine for the
// few-hundred-segment contours the MVP targets.
func SignedDistance(g geometry.Geometry, p geometry.Point) float64 {
	d := unsignedDist(g, p)
	if g.Contains(p) {
		return d
	}
	return -d
}

// BoundaryDistance is the unsigned distance from p to the nearest boundary edge.
func BoundaryDistance(g geometry.Geometry, p geometry.Point) float64 {
	return unsignedDist(g, p)
}

func unsignedDist(g geometry.Geometry, p geometry.Point) float64 {
	best := edgeDist(g.Outer, p)
	for _, h := range g.Holes {
		if dh := edgeDist(h, p); dh < best {
			best = dh
		}
	}
	return best
}

func edgeDist(pl geometry.Polyline, p geometry.Point) float64 {
	best := math.Inf(1)
	for i := 0; i < len(pl); i++ {
		a := pl[i]
		b := pl[(i+1)%len(pl)]
		if d := pointSeg(p, a, b); d < best {
			best = d
		}
	}
	return best
}

func pointSeg(p, a, b geometry.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return p.Dist(a)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return p.Dist(geometry.Point{X: a.X + t*dx, Y: a.Y + t*dy})
}
