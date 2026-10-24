package charge

import (
	"context"
	"math"
	"math/rand"
	"testing"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
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

// On a circle — the simplest possible test case, no curvature/size field to
// muddy things — a same-charge repulsion relaxation should settle into a
// roughly hexagonal packing: mean cell valence close to 6, cells reasonably
// round (high isoperimetric quotient), reasonably even in area.
func TestCharge_Circle(t *testing.T) {
	g := circle(1, 200)
	m, _, err := Mesher{}.Generate(context.Background(), g, mesher.Params{
		"h": 0.12, "n": 2, "damping": 0.85, "iters": 300,
	}, mesher.NopSink{})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Polygonal {
		t.Fatal("expected a polygonal mesh (Cells set)")
	}
	q := mesh.EvaluatePoly(m)
	t.Logf("cells=%d avgSides=%.2f isoAvg=%.3f isoMin=%.3f areaCV=%.3f sidesHist=%v",
		q.Cells, q.AvgSides, q.IsoAvg, q.IsoMin, q.AreaCV, q.SidesHistogram)

	if q.Cells < 20 {
		t.Fatalf("only %d cells", q.Cells)
	}
	if q.AvgSides < 5 || q.AvgSides > 7 {
		t.Errorf("AvgSides = %.2f, want roughly hexagonal (~6)", q.AvgSides)
	}
	if q.IsoAvg < 0.75 {
		t.Errorf("IsoAvg = %.3f, cells too irregular", q.IsoAvg)
	}
	if q.IsoMin < 0.4 {
		t.Errorf("IsoMin = %.3f, worst cell is a sliver", q.IsoMin)
	}
	if q.AreaCV > 0.35 {
		t.Errorf("AreaCV = %.3f, cell sizes too uneven", q.AreaCV)
	}

	for i := range m.Cells {
		if bad := selfIntersecting(m.Cell(i)); bad {
			t.Errorf("cell %d is self-intersecting: %v", i, m.Cell(i))
		}
	}
}

// TestCharge_NoSelfIntersectingCellsNearBoundary is a regression test for a
// bug the edge-snapping in voronoiCell introduced: ordering a cell's vertices
// by the angle of their *snapped* position (rather than the original
// circumcenter) could put them in the wrong cyclic order when several
// circumcenters snapped onto nearby points of a curved boundary, producing a
// self-crossing polygon — invisible in the metrics (area/perimeter still
// compute a number), visible on screen as an unfilled hole, since the
// even-odd scanline fill only works on a simple polygon. A tight ring
// (small h relative to a small circle) maximises how often several cells
// touch the boundary, so it's the case most likely to catch a regression.
func TestCharge_NoSelfIntersectingCellsNearBoundary(t *testing.T) {
	g := circle(0.3, 120)
	m, _, err := Mesher{}.Generate(context.Background(), g, mesher.Params{
		"h": 0.06, "n": 2, "damping": 0.85, "iters": 300,
	}, mesher.NopSink{})
	if err != nil {
		t.Fatal(err)
	}
	bad := 0
	for i := range m.Cells {
		if selfIntersecting(m.Cell(i)) {
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%d/%d cells are self-intersecting", bad, len(m.Cells))
	}
}

// selfIntersecting does a plain O(n^2) check for whether any two non-adjacent
// edges of a polygon cross. Good enough for the small cells this test looks
// at; not meant as a general-purpose geometry utility.
func selfIntersecting(poly []geometry.Point) bool {
	n := len(poly)
	if n < 4 {
		return false // a triangle can never self-intersect
	}
	for i := 0; i < n; i++ {
		a1, a2 := poly[i], poly[(i+1)%n]
		for j := i + 1; j < n; j++ {
			if j == i || (j+1)%n == i || j == (i+1)%n {
				continue // skip the edge itself and its two neighbours (they share an endpoint)
			}
			b1, b2 := poly[j], poly[(j+1)%n]
			if segmentsIntersect(a1, a2, b1, b2) {
				return true
			}
		}
	}
	return false
}

// segmentsIntersect reports whether a1-a2 and b1-b2 properly cross (each
// segment's endpoints fall on opposite sides of the other's line).
func segmentsIntersect(a1, a2, b1, b2 geometry.Point) bool {
	d1 := ccw(b1, b2, a1)
	d2 := ccw(b1, b2, a2)
	d3 := ccw(a1, a2, b1)
	d4 := ccw(a1, a2, b2)
	return ((d1 > 0) != (d2 > 0)) && ((d3 > 0) != (d4 > 0))
}

// ccw is twice the signed area of triangle p,q,r — sign says which side of
// line p->q the point r falls on.
func ccw(p, q, r geometry.Point) float64 {
	return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X)
}

func TestCharge_Cancellation(t *testing.T) {
	g := circle(1, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (Mesher{}).Generate(ctx, g, mesher.Params{"h": 0.1, "iters": 300}, mesher.NopSink{}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestCircumcenter(t *testing.T) {
	// Right triangle with legs on the axes: circumcentre is the midpoint of
	// the hypotenuse.
	a := geometry.Point{X: 0, Y: 0}
	b := geometry.Point{X: 4, Y: 0}
	c := geometry.Point{X: 0, Y: 3}
	got, ok := circumcenter(a, b, c)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := geometry.Point{X: 2, Y: 1.5}
	if got.Dist(want) > 1e-9 {
		t.Errorf("circumcenter = %v, want %v", got, want)
	}

	if _, ok := circumcenter(a, b, geometry.Point{X: 2, Y: 0}); ok {
		t.Error("collinear points: expected ok=false")
	}
}

func TestRepel(t *testing.T) {
	a := geometry.Point{X: 1, Y: 0}
	b := geometry.Point{X: 0, Y: 0}
	fx, fy := repel(a, b, 1, 2)
	if fx <= 0 || math.Abs(fy) > 1e-9 {
		t.Errorf("repel(a,b) = (%v,%v), want a pushed in +X only", fx, fy)
	}
	// Coincident points: no NaN/Inf, force is zero.
	if fx, fy := repel(a, a, 1, 2); fx != 0 || fy != 0 {
		t.Errorf("repel of coincident points = (%v,%v), want (0,0)", fx, fy)
	}
	// k scales the force linearly; k=0 switches it off.
	if fx2, _ := repel(a, b, 0.5, 2); math.Abs(fx2-0.5*fx) > 1e-12 {
		t.Errorf("repel with k=0.5 = %v, want %v", fx2, 0.5*fx)
	}
	if fx, fy := repel(a, b, 0, 2); fx != 0 || fy != 0 {
		t.Errorf("repel with k=0 = (%v,%v), want (0,0)", fx, fy)
	}
}

func TestSeparateCoincident(t *testing.T) {
	g := circle(1, 100)
	const dist = 0.01
	p := geometry.Point{X: 0.1, Y: 0.1}
	rng := rand.New(rand.NewSource(1))

	// Стоят обе — разлёт в случайную сторону, на 2*dist друг от друга.
	in := []geometry.Point{p, p}
	vel := []geometry.Point{{}, {}}
	separateCoincident(in, vel, nil, g, dist, rng)
	if d := in[0].Dist(in[1]); math.Abs(d-2*dist) > 1e-9 {
		t.Errorf("both at rest: distance after = %v, want %v", d, 2*dist)
	}

	// Разные скорости — разлёт вдоль их разности (тут по X).
	in = []geometry.Point{p, p}
	vel = []geometry.Point{{X: 1}, {X: -1}}
	separateCoincident(in, vel, nil, g, dist, rng)
	if in[0].X <= in[1].X || math.Abs(in[0].Y-in[1].Y) > 1e-12 {
		t.Errorf("moving apart along X: got %v %v", in[0], in[1])
	}

	// Внутренняя на граничном заряде — уходит внутрь на 2*dist, граничный на месте.
	b := geometry.Point{X: 0.5, Y: 0} // на окружности circle(1) после нормализации
	in = []geometry.Point{b}
	vel = []geometry.Point{{}}
	separateCoincident(in, vel, []geometry.Point{b}, g, dist, rng)
	if !g.Contains(in[0]) || math.Abs(in[0].Dist(b)-2*dist) > 1e-9 {
		t.Errorf("on boundary charge: moved to %v (dist %v), want inside at %v", in[0], in[0].Dist(b), 2*dist)
	}
}

func TestMergeCoincident(t *testing.T) {
	a := geometry.Point{X: 0, Y: 0}
	b := geometry.Point{X: 1, Y: 0}
	got := mergeCoincident([]geometry.Point{a, b, a, b, a})
	if len(got) != 2 {
		t.Errorf("mergeCoincident kept %d points, want 2: %v", len(got), got)
	}
}
