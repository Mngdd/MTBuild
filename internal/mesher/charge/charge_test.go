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
	q := mesh.EvaluatePoly(m, g)
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
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("mergeCoincident = %v, want [a b]", got)
	}
}

// TestCharge_Connected — сетка связная: соседние ячейки делят вершины (а не
// держат свои копии), каждое ребро входит не больше чем в 2 ячейки, а ребро
// ровно одной ячейки лежит на контуре. Ребро одной ячейки внутри фигуры —
// щель между сеткой и краем или дырка между ячейками.
func TestCharge_Connected(t *testing.T) {
	cases := map[string]geometry.Geometry{"circle": circle(1, 200)}
	if z, err := geometry.Preset("zalesak"); err == nil {
		cases["zalesak"], _ = z.Normalized()
	} else {
		t.Fatal(err)
	}
	for name, g := range cases {
		m, _, err := Mesher{}.Generate(context.Background(), g, mesher.Params{"h": 0.09}, mesher.NopSink{})
		if err != nil {
			t.Fatal(err)
		}
		type edge struct{ a, b int }
		uses := map[edge]int{}
		for _, c := range m.Cells {
			for k := range c {
				a, b := c[k], c[(k+1)%len(c)]
				if a > b {
					a, b = b, a
				}
				uses[edge{a, b}]++
			}
		}
		onBoundary := boundaryCheck(g, m.Vertices)
		var over, inner int
		for e, n := range uses {
			if n > 2 {
				over++
			}
			if n == 1 && !(onBoundary(e.a) && onBoundary(e.b)) {
				inner++
			}
		}
		t.Logf("%s: cells=%d verts=%d edges=%d over2=%d innerOpen=%d", name, len(m.Cells), len(m.Vertices), len(uses), over, inner)
		if over > 0 {
			t.Errorf("%s: %d edges shared by more than 2 cells", name, over)
		}
		if inner > 0 {
			t.Errorf("%s: %d edges belong to one cell but lie inside the shape (gap at the boundary)", name, inner)
		}
	}
}

// boundaryCheck — лежит ли вершина на контуре g (с точностью до 1e-9 от
// габарита).
func boundaryCheck(g geometry.Geometry, verts []geometry.Point) func(int) bool {
	min, max := g.BoundingBox()
	tol := 1e-9 * math.Max(max.X-min.X, max.Y-min.Y)
	return func(i int) bool { return verts[i].Dist(g.Nearest(verts[i])) < tol }
}

func TestWeldVertices(t *testing.T) {
	pos := []geometry.Point{{X: 0.2, Y: 0.2}, {X: 5, Y: 5}, {X: 0.5, Y: 0.4}, {}}
	valid := []bool{true, true, true, false}
	got := weldVertices(pos, valid, 1)
	want := []int{0, 1, 0, -1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tol=1: got %v, want %v", got, want)
		}
	}
	// tol=0 — сварки нет, и не должно быть мусора от деления на ноль
	got = weldVertices(pos, valid, 0)
	want = []int{0, 1, 2, -1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tol=0: got %v, want %v", got, want)
		}
	}
}

func TestReflectAcrossBoundary(t *testing.T) {
	square := geometry.Geometry{Outer: geometry.Polyline{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}}

	// у правой стенки — симметричная точка снаружи
	got := reflectAcrossBoundary([]geometry.Point{{X: 0.9, Y: 0.2}}, square, 0.3)
	if len(got) != 1 || got[0].Dist(geometry.Point{X: 1.1, Y: 0.2}) > 1e-12 {
		t.Errorf("near the wall: got %v, want [(1.1, 0.2)]", got)
	}
	// ровно на контуре — отражение совпало бы с самой частицей, не делаем
	if got := reflectAcrossBoundary([]geometry.Point{{X: 1, Y: 0.2}}, square, 0.3); len(got) != 0 {
		t.Errorf("on the contour: got %v, want none", got)
	}
	// дальше порога — отражения нет
	if got := reflectAcrossBoundary([]geometry.Point{{X: 0, Y: 0}}, square, 0.3); len(got) != 0 {
		t.Errorf("far from the wall: got %v, want none", got)
	}

	// в углу — по отражению через каждую стенку
	if got := reflectAcrossBoundary([]geometry.Point{{X: 0.9, Y: 0.85}}, square, 0.3); len(got) != 2 {
		t.Errorf("in the corner: got %v, want 2 mirrors", got)
	}
	// в тонкой полосе — через обе стенки, иначе ячейка не замкнётся
	strip := geometry.Geometry{Outer: geometry.Polyline{{X: -1, Y: -0.1}, {X: 1, Y: -0.1}, {X: 1, Y: 0.1}, {X: -1, Y: 0.1}}}
	if got := reflectAcrossBoundary([]geometry.Point{{X: 0, Y: 0.02}}, strip, 0.3); len(got) != 2 {
		t.Errorf("in a thin strip: got %v, want 2 mirrors", got)
	}
	// дуга из многих отрезков — одна стенка, одно отражение
	if got := reflectAcrossBoundary([]geometry.Point{{X: 0.9, Y: 0}}, circle(1, 200), 0.3); len(got) != 1 {
		t.Errorf("near an arc: got %v, want 1 mirror", got)
	}

	// узкая дырка шириной 0.1: отражение частицы через её левую стенку
	// перелетает дырку и попадает обратно в фигуру — его надо выкинуть
	slotted := geometry.Geometry{
		Outer: square.Outer,
		Holes: []geometry.Polyline{{{X: 0, Y: -0.5}, {X: 0.1, Y: -0.5}, {X: 0.1, Y: 0.5}, {X: 0, Y: 0.5}}},
	}
	if got := reflectAcrossBoundary([]geometry.Point{{X: -0.15, Y: 0}}, slotted, 0.3); len(got) != 0 {
		t.Errorf("mirror across a thin hole lands inside the shape and must be dropped, got %v", got)
	}
}

// TestCharge_EvenCellAreas: ячейки у края и внутри одного размера. Проверяем
// именно диапазон: на zalesak среднее у края было 1.03 при разбросе 0.22..2.49,
// так что по среднему эта проблема не видна.
func TestCharge_EvenCellAreas(t *testing.T) {
	for _, name := range []string{"ellipse", "zalesak"} {
		g, err := geometry.Preset(name)
		if err != nil {
			t.Fatal(err)
		}
		const h = 0.09
		min, max := g.BoundingBox()
		cellSize := h * math.Max(max.X-min.X, max.Y-min.Y)
		m, _, err := Mesher{}.Generate(context.Background(), g, mesher.Params{"h": h}, mesher.NopSink{})
		if err != nil {
			t.Fatal(err)
		}
		smallest, largest := math.Inf(1), 0.0
		for i := range m.Cells {
			area := polyArea(m.Cell(i)) / (cellSize * cellSize)
			smallest = math.Min(smallest, area)
			largest = math.Max(largest, area)
		}
		t.Logf("%s: cells=%d area/h^2 %.2f..%.2f", name, len(m.Cells), smallest, largest)
		if smallest < 0.6 || largest > 1.5 {
			t.Errorf("%s: cell areas %.2f..%.2f h^2, want within 0.6..1.5", name, smallest, largest)
		}
	}
}

func TestPolyCentroid(t *testing.T) {
	square := []geometry.Point{{X: 1, Y: 1}, {X: 3, Y: 1}, {X: 3, Y: 5}, {X: 1, Y: 5}}
	if c, ok := polyCentroid(square); !ok || c.Dist(geometry.Point{X: 2, Y: 3}) > 1e-12 {
		t.Errorf("polyCentroid(square) = %v, %v, want (2, 3)", c, ok)
	}
	line := []geometry.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}}
	if _, ok := polyCentroid(line); ok {
		t.Error("zero-area polygon: expected ok=false")
	}
}

func TestLloydStep_KeepsCountAndOrder(t *testing.T) {
	g := circle(0.5, 100)
	interior := []geometry.Point{{X: 0.1, Y: 0}, {X: -0.2, Y: 0.1}, {X: 0, Y: -0.25}, {X: 0.25, Y: 0.2}}
	moved, err := lloydStep(interior, g, 0.2, 0.004, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != len(interior) {
		t.Fatalf("got %d particles, want %d", len(moved), len(interior))
	}
	shifted := false
	for i := range moved {
		if !g.Contains(moved[i]) {
			t.Errorf("particle %d left the shape: %v", i, moved[i])
		}
		if moved[i].Dist(interior[i]) > 1e-9 {
			shifted = true
		}
	}
	if !shifted {
		t.Error("no particle moved")
	}
}

// Периодическая сетка: квадрат-период замощён ячейками без дыр и наложений
// (сумма площадей = 1), и каждое ребро общее ровно у двух ячеек, считая
// ячейки через шов (вершины сравниваем по модулю периода). Не сойдись шов —
// у рёбер на краю квадрата не нашлось бы пары. Последний случай — с большим
// допуском сварки и недоосевшими частицами: там у шва много сваренных вершин,
// и простая сварка (weldVertices) давала тут рёбра без пары.
func TestCharge_PeriodicTilesWithoutSeams(t *testing.T) {
	cases := []mesher.Params{
		{"periodic": 1, "h": 0.1},
		{"periodic": 1, "h": 0.1, "random_init": 1},
		{"periodic": 1, "h": 0.05, "random_init": 1, "seed": 3, "weld_tol": 0.1, "iters": 100, "lloyd_iters": 0},
	}
	for caseIndex, params := range cases {
		m, _, err := Mesher{}.Generate(context.Background(), circle(1, 200), params, mesher.NopSink{})
		if err != nil {
			t.Fatal(err)
		}
		if m.Period != 1 {
			t.Fatalf("case %v: Period = %v, want 1", caseIndex, m.Period)
		}
		var areaSum float64
		for i := range m.Cells {
			areaSum += polyArea(m.Cell(i))
		}
		if math.Abs(areaSum-1) > 1e-9 {
			t.Errorf("case %v: cell areas sum to %v, want 1 (gaps or overlaps)", caseIndex, areaSum)
		}
		for i, site := range m.Sites {
			if site.X < 0 || site.X >= 1 || site.Y < 0 || site.Y >= 1 {
				t.Errorf("case %v: site %d = %v outside the period square", caseIndex, i, site)
			}
		}

		type vertexKey struct{ x, y int64 }
		keyOf := func(p geometry.Point) vertexKey {
			const grid = 1e7
			x, y := p.X-math.Floor(p.X), p.Y-math.Floor(p.Y)
			return vertexKey{int64(math.Round(x*grid)) % grid, int64(math.Round(y*grid)) % grid}
		}
		edgeCount := map[[2]vertexKey]int{}
		for i := range m.Cells {
			cell := m.Cell(i)
			for k := range cell {
				a, b := keyOf(cell[k]), keyOf(cell[(k+1)%len(cell)])
				if b.x < a.x || (b.x == a.x && b.y < a.y) {
					a, b = b, a
				}
				edgeCount[[2]vertexKey{a, b}]++
			}
		}
		for edge, count := range edgeCount {
			if count != 2 {
				t.Errorf("case %v: edge %v shared by %d cells, want 2", caseIndex, edge, count)
			}
		}
	}
}

func TestPeriodicHelpers(t *testing.T) {
	if p := wrapIntoSquare(geometry.Point{X: 0.7, Y: -0.6}); p.Dist(geometry.Point{X: -0.3, Y: 0.4}) > 1e-12 {
		t.Errorf("wrapIntoSquare = %v, want (-0.3, 0.4)", p)
	}
	if p := nearestImage(geometry.Point{X: 0.45, Y: 0}, geometry.Point{X: -0.45, Y: 0}); p.Dist(geometry.Point{X: -0.55, Y: 0}) > 1e-12 {
		t.Errorf("nearestImage = %v, want (-0.55, 0) — through the edge", p)
	}
	if n := len(periodicImages([]geometry.Point{{X: 0, Y: 0}, {X: 0.1, Y: 0.2}})); n != 16 {
		t.Errorf("periodicImages: %d copies, want 16", n)
	}
}
