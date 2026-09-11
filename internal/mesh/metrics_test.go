package mesh

import (
	"math"
	"testing"

	"MTBuild/internal/geometry"
)

func TestEvaluate_Equilateral(t *testing.T) {
	m := Mesh{
		Vertices: []geometry.Point{
			{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0.5, Y: math.Sqrt(3) / 2},
		},
		Triangles: [][3]int{{0, 1, 2}},
	}
	q := Evaluate(m)
	if math.Abs(q.GAvg-1) > 1e-9 {
		t.Errorf("GAvg = %v, want 1", q.GAvg)
	}
	if math.Abs(q.ThetaMinDeg-60) > 1e-6 {
		t.Errorf("ThetaMinDeg = %v, want 60", q.ThetaMinDeg)
	}
	if q.BadFraction != 0 {
		t.Errorf("BadFraction = %v, want 0", q.BadFraction)
	}
}

func TestEvaluate_RightIsoceles(t *testing.T) {
	m := Mesh{
		Vertices:  []geometry.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}},
		Triangles: [][3]int{{0, 1, 2}},
	}
	q := Evaluate(m)
	if math.Abs(q.ThetaMinDeg-45) > 1e-6 {
		t.Errorf("ThetaMinDeg = %v, want 45", q.ThetaMinDeg)
	}
	if q.GAvg <= 0 || q.GAvg >= 1 {
		t.Errorf("GAvg = %v, want in (0,1)", q.GAvg)
	}
}
