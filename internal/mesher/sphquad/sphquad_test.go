package sphquad

import (
	"context"
	"math"
	"testing"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesher"
	"MTBuild/internal/mesher/sphquad/field"
	"MTBuild/internal/mesher/sphquad/sph"
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
	// Post-triangulation smoothing (default smooth_passes=3, not overridden
	// above) reliably puts this in the 0.89-0.90 range now — floor set with
	// headroom below that as a regression guard, not the ceiling.
	if q.GAvg < 0.86 {
		t.Errorf("GAvg = %.3f, below acceptable floor for the method", q.GAvg)
	}
	if q.BadFraction > 0.02 {
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

func TestLaplacianStep_OneTriangle(t *testing.T) {
	// A single triangle: vertex 0 is interior, 1 and 2 are boundary. 0's only
	// neighbours are 1 and 2, so it must move to exactly their midpoint;
	// 1 and 2 must not move at all.
	pts := []geometry.Point{{X: 5, Y: -5}, {X: 2, Y: 0}, {X: 0, Y: 2}}
	tris := [][3]int{{0, 1, 2}}
	isBoundary := []bool{false, true, true}

	out := laplacianStep(pts, tris, isBoundary)
	want0 := geometry.Point{X: 1, Y: 1} // midpoint of (2,0) and (0,2)
	if out[0].Dist(want0) > 1e-9 {
		t.Errorf("interior vertex = %v, want %v", out[0], want0)
	}
	if out[1] != pts[1] || out[2] != pts[2] {
		t.Errorf("boundary vertices moved: got %v,%v want %v,%v", out[1], out[2], pts[1], pts[2])
	}
}

func TestLaplacianStep_SharedEdgeAccumulates(t *testing.T) {
	// Two triangles sharing edge A-C: A=0 (interior), B=1, C=2, D=3
	// (boundary). A's neighbours across both triangles are B, C (from
	// {0,1,2}) and C, D (from {0,2,3}) — C counted twice, once per triangle
	// it's shared through — so A's average is (B + 2C + D) / 4, not a plain
	// average over the 3 distinct neighbours.
	pts := []geometry.Point{
		{X: 99, Y: 99}, // A: interior, overwritten
		{X: 2, Y: 0},   // B
		{X: 0, Y: 2},   // C
		{X: -2, Y: 0},  // D
	}
	tris := [][3]int{{0, 1, 2}, {0, 2, 3}}
	isBoundary := []bool{false, true, true, true}

	out := laplacianStep(pts, tris, isBoundary)
	want := geometry.Point{X: 0, Y: 1} // (B + 2C + D)/4 = ((2,0)+2*(0,2)+(-2,0))/4
	if out[0].Dist(want) > 1e-9 {
		t.Errorf("interior vertex = %v, want %v", out[0], want)
	}
}

func TestDedupeIndices(t *testing.T) {
	pts := []geometry.Point{{X: 0, Y: 0}, {X: 0.001, Y: 0}, {X: 5, Y: 5}, {X: 0, Y: 0.001}}
	got := dedupeIndices(pts, 0.01)
	want := []int{0, 2} // 1 and 3 collapse into 0's cell
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	// tol<=0 keeps every index, unchanged order.
	if got := dedupeIndices(pts, 0); len(got) != len(pts) {
		t.Errorf("tol=0: got %d indices, want %d (no dedup)", len(got), len(pts))
	}
}

func TestDomainArea(t *testing.T) {
	sq, _ := geometry.Geometry{Outer: geometry.Polyline{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2},
	}}.Normalized()
	if got := domainArea(sq); math.Abs(got-4) > 1e-9 {
		t.Errorf("area = %v, want 4", got)
	}
	hole, _ := geometry.Geometry{Outer: geometry.Polyline{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2},
	}, Holes: []geometry.Polyline{{{X: 0.5, Y: 0.5}, {X: 1.5, Y: 0.5}, {X: 1.5, Y: 1.5}, {X: 0.5, Y: 1.5}}}}.Normalized()
	if got := domainArea(hole); math.Abs(got-3) > 1e-9 {
		t.Errorf("area with hole = %v, want 3 (4 - 1)", got)
	}
}

func TestCalibrateTheta(t *testing.T) {
	// Footprint already matches area exactly -> theta stays 1.
	parts := []sph.Particle{
		{Kind: sph.Interior, Ht: 1}, {Kind: sph.Interior, Ht: 1}, {Kind: sph.Interior, Ht: 1}, {Kind: sph.Interior, Ht: 1},
		{Kind: sph.Surface, Ht: 100}, // must be ignored: only Interior counts toward footprint
	}
	if got := calibrateTheta(parts, 4); math.Abs(got-1) > 1e-9 {
		t.Errorf("theta = %v, want 1", got)
	}
	// Degenerate inputs fall back to 1 rather than dividing by zero.
	if got := calibrateTheta(nil, 4); got != 1 {
		t.Errorf("no particles: theta = %v, want 1", got)
	}
	if got := calibrateTheta(parts, 0); got != 1 {
		t.Errorf("zero area: theta = %v, want 1", got)
	}
	// Wildly mismatched footprint is clamped, not left to swing freely.
	tiny := []sph.Particle{{Kind: sph.Interior, Ht: 0.001}}
	if got := calibrateTheta(tiny, 1000); got > 1.6+1e-9 {
		t.Errorf("theta = %v, want clamped to <= 1.6", got)
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
