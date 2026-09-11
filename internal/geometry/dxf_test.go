package geometry

import (
	"os"
	"testing"
)

func TestLoadDXF_Kulman(t *testing.T) {
	f, err := os.Open("testdata/kulman.dxf")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	g, err := LoadDXF(f)
	if err != nil {
		t.Fatalf("LoadDXF: %v", err)
	}
	if len(g.Outer) < 3 {
		t.Fatalf("outer contour too small: %d points", len(g.Outer))
	}
	if !g.Outer.IsCCW() {
		t.Errorf("outer contour must be normalized to CCW, area=%v", g.Outer.SignedArea())
	}
	if len(g.Holes) != 0 {
		t.Errorf("expected no holes, got %d", len(g.Holes))
	}
	min, max := g.BoundingBox()
	if max.X-min.X <= 0 || max.Y-min.Y <= 0 {
		t.Errorf("degenerate bbox: %+v %+v", min, max)
	}
	t.Logf("kulman.dxf: %d pts, bbox [%.2f,%.2f]x[%.2f,%.2f]", len(g.Outer), min.X, max.X, min.Y, max.Y)
}

func TestDeBoor_ClampedCubicEndpoints(t *testing.T) {
	// A clamped B-spline interpolates its first and last control points.
	ctrl := []Point{{0, 0}, {1, 2}, {3, 2}, {4, 0}, {5, 1}}
	knots := clampedUniformKnots(len(ctrl), 3)
	if got := deBoor(knots[3], 3, knots, ctrl); got.Dist(ctrl[0]) > 1e-9 {
		t.Errorf("start: got %+v want %+v", got, ctrl[0])
	}
	end := deBoor(knots[len(knots)-4], 3, knots, ctrl)
	if end.Dist(ctrl[len(ctrl)-1]) > 1e-9 {
		t.Errorf("end: got %+v want %+v", end, ctrl[len(ctrl)-1])
	}
}

func TestPresets(t *testing.T) {
	for _, name := range []string{"ellipse", "zalesak"} {
		g, err := Preset(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !g.Outer.IsCCW() {
			t.Errorf("%s: outer not CCW", name)
		}
	}
}
