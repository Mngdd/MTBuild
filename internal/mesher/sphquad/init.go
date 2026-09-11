package sphquad

import (
	"math"
	"math/rand"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesher/sphquad/field"
	"MTBuild/internal/mesher/sphquad/sph"
)

// initParticles seeds the relaxation: surface particles along every contour at
// the local target spacing (contour 0 is Outer, 1.. are holes), plus interior
// particles laid on a fine jittered lattice and thinned so their expected
// spacing matches h_t.
func initParticles(g geometry.Geometry, contours []sph.Contour, sizeAt sph.SizeFn, hNear float64, rng *rand.Rand) []sph.Particle {
	var parts []sph.Particle

	addContour := func(pl geometry.Polyline, id int) {
		for _, p := range walkContour(pl, sizeAt) {
			parts = append(parts, sph.Particle{
				Pos:     p,
				S:       contours[id].ArcLengthOf(p),
				Kind:    sph.Surface,
				Ht:      sizeAt(p),
				Contour: id,
			})
		}
	}
	addContour(g.Outer, 0)
	for i, h := range g.Holes {
		addContour(h, i+1)
	}

	min, max := g.BoundingBox()
	step := hNear * 0.5
	for y := min.Y + step/2; y < max.Y; y += step {
		for x := min.X + step/2; x < max.X; x += step {
			p := geometry.Point{
				X: x + (rng.Float64()-0.5)*step*0.4,
				Y: y + (rng.Float64()-0.5)*step*0.4,
			}
			if !g.Contains(p) {
				continue
			}
			ht := sizeAt(p)
			if field.BoundaryDistance(g, p) < 0.6*ht {
				continue // leave room for the boundary row
			}
			// Accept with probability (step/ht)^2 so density ~ 1/ht^2.
			if rng.Float64() < (step/ht)*(step/ht) {
				parts = append(parts, sph.Particle{Pos: p, Kind: sph.Interior, Ht: ht, Contour: -1})
			}
		}
	}
	return parts
}

// walkContour emits points along a closed contour spaced by the local target
// size, so the boundary is sampled finely where elements are small.
func walkContour(pl geometry.Polyline, sizeAt sph.SizeFn) []geometry.Point {
	if len(pl) < 2 {
		return append([]geometry.Point(nil), pl...)
	}
	var out []geometry.Point
	acc := 0.0
	target := sizeAt(pl[0])
	for i := 0; i < len(pl); i++ {
		a := pl[i]
		b := pl[(i+1)%len(pl)]
		seg := a.Dist(b)
		if seg == 0 {
			continue
		}
		dx, dy := (b.X-a.X)/seg, (b.Y-a.Y)/seg
		for acc+target < seg {
			acc += target
			p := geometry.Point{X: a.X + dx*acc, Y: a.Y + dy*acc}
			out = append(out, p)
			target = math.Max(sizeAt(p), 1e-4)
		}
		acc -= seg
	}
	if len(out) == 0 {
		out = append(out, pl...)
	}
	return out
}
