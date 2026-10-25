// Package sph runs the particle relaxation from the thesis (Algorithm 3).
//
// Forces: Shepard-renormalised pressure (formulas 33-36) with an extra boundary
// repulsion term along the inward normal, plus the artificial viscosity of
// formula 26. Integration is leap-frog with a CFL pseudo-time step. Relaxation
// is two-phase (section 4.7): momentum accumulates freely at first for a fast
// approach to the target size distribution, then velocities are killed every
// step for a quiet final polish.
//
// Surface particles are seeded at the target spacing along their contour and
// held there as fixed force sources and normal providers. The thesis also
// evolves them tangentially; with a precomputed (static) h_t field their
// spacing is already optimal, so that step is skipped.
package sph

import (
	"context"
	"math"
	"math/rand"

	"MTBuild/internal/geometry"
)

// Kind distinguishes free interior particles from boundary-constrained ones.
type Kind uint8

const (
	Interior Kind = iota
	Surface
)

// Particle is one relaxation point.
type Particle struct {
	Pos, Vel geometry.Point
	S, Vs    float64 // arc-length position and velocity along the contour (Surface only)
	Kind     Kind
	Ht       float64 // target element size at Pos, refreshed each iteration
	Contour  int     // index into the contours slice for Surface particles; -1 otherwise
}

// SizeFn returns the target element size h_t at an arbitrary point.
type SizeFn func(geometry.Point) float64

// Domain answers containment and signed distance for projecting escapees back.
type Domain struct {
	Contains func(geometry.Point) bool
	Signed   func(geometry.Point) float64 // >0 inside
}

// Config tunes the relaxation.
type Config struct {
	Iterations   int
	P0           float64 // pressure coefficient (thesis uses ~1)
	CFL          float64 // max fraction of local h a particle may move per step
	Tol          float64 // convergence: stop when mean move / h < Tol
	RampFraction float64 // fraction of iterations spent in the free-momentum phase
	Damping      float64 // velocity retained between steps during the ramp phase
	Theta        float64 // scaling coefficient applied to every h_t sample (thesis section 3); 0 => 1 (no scaling)
	Seed         int64
}

func (c Config) withDefaults() Config {
	if c.Iterations == 0 {
		c.Iterations = 300
	}
	if c.P0 == 0 {
		c.P0 = 1
	}
	if c.CFL == 0 {
		c.CFL = 0.15
	}
	if c.Tol == 0 {
		c.Tol = 8e-4
	}
	if c.RampFraction == 0 {
		c.RampFraction = 0.5
	}
	if c.Damping == 0 {
		c.Damping = 0.9
	}
	if c.Theta == 0 {
		c.Theta = 1
	}
	return c
}

// Result is the outcome of a relaxation run.
type Result struct {
	Particles []Particle
	Iters     int
	Converged bool
}

// Relax moves interior particles toward the target size distribution and slides
// surface particles along their contours to equalise boundary spacing.
// report, if non-nil, is called every 10th iteration (and on the last) with
// the particles and meanStep — the mean interior move of that iteration in
// units of local h (the same number the convergence check uses).
func Relax(ctx context.Context, ps []Particle, size SizeFn, dom Domain, contours []Contour,
	cfg Config, report func(iter int, ps []Particle, meanStep float64)) (Result, error) {

	cfg = cfg.withDefaults()
	_ = rand.New(rand.NewSource(cfg.Seed)) // reserved for future jitter

	rampIters := int(float64(cfg.Iterations) * cfg.RampFraction)

	for it := 0; it < cfg.Iterations; it++ {
		if err := ctx.Err(); err != nil {
			return Result{Particles: ps, Iters: it}, err
		}

		hMax := 0.0
		for i := range ps {
			ps[i].Ht = clampPos(cfg.Theta * size(ps[i].Pos))
			if ps[i].Ht > hMax {
				hMax = ps[i].Ht
			}
		}
		grid := newCellGrid(ps, 2*hMax)

		// Inward normal and forward tangent at each surface particle.
		snorm := make([]geometry.Point, len(ps))
		stan := make([]geometry.Point, len(ps))
		for i := range ps {
			if ps[i].Kind == Surface {
				_, snorm[i], stan[i] = contours[ps[i].Contour].At(ps[i].S)
			}
		}

		forces := make([]geometry.Point, len(ps))
		for i := range ps {
			gamma := shepardGamma(i, ps, grid)
			fp := pressureForce(i, ps, grid, cfg.P0, gamma, snorm)
			fv := viscosityForce(i, ps, grid)
			forces[i] = geometry.Point{X: fp.X + fv.X, Y: fp.Y + fv.Y}
		}

		// CFL pseudo-time: scale the step so the fastest particle moves exactly
		// CFL * its local h. This stays large near equilibrium (fast polish) and
		// shrinks when a force spikes.
		maxRate := 1e-30
		for i := range ps {
			if ps[i].Kind == Surface {
				continue
			}
			if r := math.Hypot(forces[i].X, forces[i].Y) / ps[i].Ht; r > maxRate {
				maxRate = r
			}
		}
		dt := cfg.CFL / maxRate

		ramp := it < rampIters
		var moveSum float64
		var moved int
		for i := range ps {
			p := &ps[i]

			// Surface particles sit at the target spacing along the contour;
			// for a precomputed size field that placement is already optimal,
			// so they act only as fixed force sources and normal providers.
			// (The thesis evolves them tangentially; with a static h_t that is
			// a no-op.)
			if p.Kind == Surface {
				continue
			}

			vMax := cfg.CFL * p.Ht // hard per-particle displacement limit
			ax, ay := forces[i].X, forces[i].Y
			if ramp {
				p.Vel.X = cfg.Damping*p.Vel.X + dt*ax
				p.Vel.Y = cfg.Damping*p.Vel.Y + dt*ay
			} else {
				p.Vel.X, p.Vel.Y = dt*ax, dt*ay // momentum killed each step
			}
			p.Vel = clampMag(geometry.Point{X: sanitise(p.Vel.X), Y: sanitise(p.Vel.Y)}, vMax)
			np := projectInside(geometry.Point{X: p.Pos.X + p.Vel.X, Y: p.Pos.Y + p.Vel.Y}, dom)
			moveSum += p.Pos.Dist(np) / p.Ht
			moved++
			p.Pos = np
		}

		meanStep := 0.0
		if moved > 0 {
			meanStep = moveSum / float64(moved)
		}
		if report != nil && (it%10 == 0 || it == cfg.Iterations-1) {
			report(it, ps, meanStep)
		}
		if !ramp && moved > 0 && meanStep < cfg.Tol {
			return Result{Particles: ps, Iters: it + 1, Converged: true}, nil
		}
	}
	return Result{Particles: ps, Iters: cfg.Iterations, Converged: false}, nil
}

// shepardGamma is the boundary renormalisation weight (thesis formula 35):
//
//	gamma_i = sum_j W(r_ij, h_i) * V_tj ,   V_tj = h_tj^2
//
// For a full neighbourhood this integrates to ~1 (the kernel is normalised and
// V_tj is the reciprocal number density). We floor it at that reference value:
// dividing by gamma then only *averages* the force in well-packed regions
// rather than amplifying it near the boundary, which keeps the near-boundary
// layer stable. The missing-neighbour compensation there is carried instead by
// the explicit boundary repulsion term.
func shepardGamma(i int, ps []Particle, grid *cellGrid) float64 {
	pi := ps[i]
	var g float64
	grid.forNeighbours(pi.Pos, func(j int) {
		pj := ps[j]
		r := pi.Pos.Dist(pj.Pos)
		if r < 2*pi.Ht {
			g += kernelW(r, pi.Ht) * pj.Ht * pj.Ht
		}
	})
	const gammaRef = 1.0
	if g < gammaRef {
		return gammaRef
	}
	return g
}

// pressureForce is the renormalised SPH pressure term (formulas 33, 36):
//
//	F_p,i = -(1/gamma_i) * sum_{j interior} (p0*(h_i^4 + h_j^4)) * dW/dr * e_ij
//	        +(1/gamma_i) * sum_{b surface}  (p0*(h_i^4 + h_b^4)) * W(r_ib,h_ib) * n_b * A_tb
//
// The first sum spreads particles to the target spacing (the h^4 = 1/rho_t^2
// weighting makes coarse regions push harder); the second is a repulsion from
// the boundary that stops particles leaking into the exterior.
func pressureForce(i int, ps []Particle, grid *cellGrid, p0, gamma float64, snorm []geometry.Point) geometry.Point {
	pi := ps[i]
	hi4 := pow4(pi.Ht)
	var fx, fy float64

	grid.forNeighbours(pi.Pos, func(j int) {
		if j == i {
			return
		}
		pj := ps[j]
		dx := pi.Pos.X - pj.Pos.X
		dy := pi.Pos.Y - pj.Pos.Y
		r := math.Hypot(dx, dy)
		hij := 0.5 * (pi.Ht + pj.Ht)
		if r <= 0 || r >= 2*hij {
			return
		}
		coeff := p0 * (hi4 + pow4(pj.Ht))

		// -grad of pressure: repulsive (dW/dr < 0), pushing i away from j.
		g := coeff * kernelDWDR(r, hij) / gamma
		fx -= g * dx / r
		fy -= g * dy / r

		if pj.Kind == Surface {
			// Extra repulsion straight along the inward normal so particles do
			// not leak across the boundary (thesis formula 36 boundary term).
			const boundaryScale = 0.25
			nrm := snorm[j]
			w := kernelW(r, hij) * pj.Ht
			fx += boundaryScale * coeff * w * nrm.X / gamma
			fy += boundaryScale * coeff * w * nrm.Y / gamma
		}
	})
	return geometry.Point{X: fx, Y: fy}
}

// viscosityForce is the artificial viscosity of formula 26, using target
// density throughout. It opposes the relative velocity of neighbours and so
// damps oscillations during the ramp phase.
func viscosityForce(i int, ps []Particle, grid *cellGrid) geometry.Point {
	pi := ps[i]
	speedI := math.Hypot(pi.Vel.X, pi.Vel.Y)
	etaI := (1 / (pi.Ht * pi.Ht)) * 0.1 * (2 * pi.Ht) * speedI // rho_t * nu, nu ~ 0.1 r_c |v|
	if etaI == 0 {
		return geometry.Point{}
	}
	hi4 := pow4(pi.Ht)
	var fx, fy float64
	grid.forNeighbours(pi.Pos, func(j int) {
		if j == i {
			return
		}
		pj := ps[j]
		dx := pi.Pos.X - pj.Pos.X
		dy := pi.Pos.Y - pj.Pos.Y
		r := math.Hypot(dx, dy)
		hij := 0.5 * (pi.Ht + pj.Ht)
		if r <= 0 || r >= 2*hij {
			return
		}
		speedJ := math.Hypot(pj.Vel.X, pj.Vel.Y)
		etaJ := (1 / (pj.Ht * pj.Ht)) * 0.1 * (2 * pj.Ht) * speedJ
		if etaI+etaJ == 0 {
			return
		}
		etaF := 2 * etaI * etaJ / (etaI + etaJ)
		term := etaF * (hi4 + pow4(pj.Ht)) * kernelDWDR(r, hij) / r // < 0
		fx += term * (pi.Vel.X - pj.Vel.X)
		fy += term * (pi.Vel.Y - pj.Vel.Y)
	})
	return geometry.Point{X: fx, Y: fy}
}

func projectInside(p geometry.Point, dom Domain) geometry.Point {
	if dom.Contains(p) {
		return p
	}
	const eps = 1e-6
	phi := dom.Signed(p)
	gx := (dom.Signed(geometry.Point{X: p.X + eps, Y: p.Y}) - dom.Signed(geometry.Point{X: p.X - eps, Y: p.Y})) / (2 * eps)
	gy := (dom.Signed(geometry.Point{X: p.X, Y: p.Y + eps}) - dom.Signed(geometry.Point{X: p.X, Y: p.Y - eps})) / (2 * eps)
	n := math.Hypot(gx, gy)
	if n == 0 {
		return p
	}
	step := -phi + eps
	np := geometry.Point{X: p.X + step*gx/n, Y: p.Y + step*gy/n}
	if dom.Contains(np) {
		return np
	}
	return p
}

func clampPos(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 1e-6
	}
	return v
}

func pow4(x float64) float64 { return x * x * x * x }

func clampAbs(v, lim float64) float64 {
	if v > lim {
		return lim
	}
	if v < -lim {
		return -lim
	}
	return v
}

func clampMag(p geometry.Point, lim float64) geometry.Point {
	m := math.Hypot(p.X, p.Y)
	if m > lim && m > 0 {
		return geometry.Point{X: p.X * lim / m, Y: p.Y * lim / m}
	}
	return p
}

func sanitise(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
