package sphquad

import (
	"context"
	"math"
	"testing"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesher"
	"MTBuild/internal/mesher/sphquad/field"
)

// Golden check against the thesis reference: a correct implementation puts
// GAvg for the Zalesak disk in roughly [0.85, 0.95] (see example_info.md 7.7).
func TestSPHQuad_ZalesakQuality(t *testing.T) {
	g, err := geometry.Preset("zalesak")
	if err != nil {
		t.Fatal(err)
	}
	_, q, err := Mesher{}.Generate(context.Background(), g, mesher.Params{
		"h_max": 0.05, "h_max_s": 0.04, "h_min_s": 0.025, "grade": 0.3, "iters": 400,
	}, mesher.NopSink{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("triangles=%d GAvg=%.3f GMin=%.3f thetaMin=%.1f bad=%.1f%%",
		q.Triangles, q.GAvg, q.GMin, q.ThetaMinDeg, q.BadFraction*100)

	if q.Triangles < 300 {
		t.Fatalf("only %d triangles", q.Triangles)
	}
	if q.GAvg < 0.80 {
		t.Errorf("GAvg = %.3f, below acceptable floor for the method", q.GAvg)
	}
	if q.BadFraction > 0.05 {
		t.Errorf("bad-triangle fraction %.2f too high", q.BadFraction)
	}
}

func TestSPHQuad_Cancellation(t *testing.T) {
	g, _ := geometry.Preset("ellipse")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (Mesher{}).Generate(ctx, g, nil, mesher.NopSink{}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestSizer_CurvatureAndGrading(t *testing.T) {
	// A circle of normalised radius 0.5: curvature 2 everywhere, no singularities.
	var pl geometry.Polyline
	for i := 0; i < 240; i++ {
		a := 2 * math.Pi * float64(i) / 240
		pl = append(pl, geometry.Point{X: 0.5 * math.Cos(a), Y: 0.5 * math.Sin(a)})
	}
	gc, _ := geometry.Geometry{Outer: pl}.Normalized()
	f := field.Analyze(gc, 25, 0.3)
	s := sizer{f: f, hFar: 0.2, hMaxS: 0.1, hMinS: 0.02, grade: 0.3, kappaA: 20}

	// On the (low-curvature) boundary: near h_max_s, well above h_min_s.
	hb := s.at(geometry.Point{X: 0.5, Y: 0})
	if hb < 0.06 || hb > 0.1001 {
		t.Errorf("boundary size %.3f, expected ~h_max_s", hb)
	}
	// At the centre (|phi| = 0.5 > grade): h_max.
	hc := s.at(geometry.Point{X: 0, Y: 0})
	if math.Abs(hc-0.2) > 1e-9 {
		t.Errorf("centre size %.3f, expected h_max 0.2", hc)
	}
}

func TestSizer_ShrinksAtSharpCorner(t *testing.T) {
	// A square has four 90-degree corners -> singularities.
	sq, _ := geometry.Geometry{Outer: geometry.Polyline{
		{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0.5, Y: 0.5}, {X: -0.5, Y: 0.5},
	}}.Normalized()
	f := field.Analyze(sq, 25, 0.3)
	if len(f.Singularities()) != 4 {
		t.Fatalf("expected 4 corners, got %d", len(f.Singularities()))
	}
	s := sizer{f: f, hFar: 0.2, hMaxS: 0.1, hMinS: 0.02, grade: 0.3, kappaA: 20}
	atCorner := s.at(geometry.Point{X: 0.49, Y: 0.49})
	atEdge := s.at(geometry.Point{X: 0.0, Y: 0.5})
	if atCorner >= atEdge {
		t.Errorf("corner size %.3f should be below mid-edge size %.3f", atCorner, atEdge)
	}
}
