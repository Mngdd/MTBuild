package grid

import (
	"context"
	"testing"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesher"
)

func TestGrid_EllipseEndToEnd(t *testing.T) {
	g, err := geometry.Preset("ellipse")
	if err != nil {
		t.Fatal(err)
	}
	m, q, err := Mesher{}.Generate(context.Background(), g, mesher.Params{"h": 0.08}, mesher.NopSink{})
	if err != nil {
		t.Fatal(err)
	}
	if q.Triangles < 50 {
		t.Fatalf("only %d triangles, expected a filled ellipse", q.Triangles)
	}
	// Every kept triangle's centroid must be inside the domain.
	for i := range m.Triangles {
		if !g.Contains(m.Centroid(i)) {
			t.Fatalf("triangle %d centroid outside domain", i)
		}
	}
	// A regular lattice triangulation is mediocre but not broken.
	if q.GAvg < 0.5 || q.GAvg > 0.95 {
		t.Errorf("GAvg = %.3f, expected a middling regular-grid value", q.GAvg)
	}
}

func TestGrid_Cancellation(t *testing.T) {
	g, _ := geometry.Preset("ellipse")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (Mesher{}).Generate(ctx, g, nil, mesher.NopSink{}); err == nil {
		t.Fatal("expected cancellation error")
	}
}
