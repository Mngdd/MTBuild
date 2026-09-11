package field

import (
	"math"
	"testing"

	"MTBuild/internal/geometry"
)

func circle(r float64, n int) geometry.Geometry {
	var pl geometry.Polyline
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		pl = append(pl, geometry.Point{X: r * math.Cos(a), Y: r * math.Sin(a)})
	}
	g, _ := geometry.Geometry{Outer: pl}.Normalized()
	return g
}

func TestCurvature_Circle(t *testing.T) {
	// Discrete curvature of a fine circle of radius 0.5 should approach 1/r = 2.
	f := Analyze(circle(0.5, 360), 25, 0.3)
	k := f.Kappa(geometry.Point{X: 0.5, Y: 0})
	if math.Abs(k-2) > 0.05 {
		t.Errorf("kappa = %.4f, want ~2", k)
	}
	if len(f.Singularities()) != 0 {
		t.Errorf("smooth circle should have no singularities, got %d", len(f.Singularities()))
	}
}

func TestSingularities_Square(t *testing.T) {
	sq, _ := geometry.Geometry{Outer: geometry.Polyline{
		{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
	}}.Normalized()
	f := Analyze(sq, 45, 0.25)
	if len(f.Singularities()) != 4 {
		t.Fatalf("want 4 corners, got %d", len(f.Singularities()))
	}
	// psi is 0 at a corner, ~grade-sized mid-domain.
	if psi := f.Psi(geometry.Point{X: 0, Y: 0}); psi > 1e-9 {
		t.Errorf("psi at corner = %.4f, want 0", psi)
	}
	if psi := f.Psi(geometry.Point{X: 0, Y: 0.5}); psi <= 0 {
		t.Errorf("psi mid-edge should be positive, got %.4f", psi)
	}
}

func TestKappaCorrected_SingularityCeiling(t *testing.T) {
	sq, _ := geometry.Geometry{Outer: geometry.Polyline{
		{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
	}}.Normalized()
	f := Analyze(sq, 45, 0.25)
	const kappaA = 30
	// Right at a corner the correction forces kappa up to kappaA.
	if kc := f.KappaCorrected(geometry.Point{X: 0, Y: 0}, kappaA); math.Abs(kc-kappaA) > 1e-6 {
		t.Errorf("corrected kappa at corner = %.3f, want %v", kc, float64(kappaA))
	}
	// Far from any corner the artificial term has decayed to zero.
	center := f.KappaCorrected(geometry.Point{X: 0.5, Y: 0.5}, kappaA)
	if center >= kappaA {
		t.Errorf("corrected kappa mid-domain = %.3f, should be well below %v", center, float64(kappaA))
	}
}
