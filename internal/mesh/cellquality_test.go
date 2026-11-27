package mesh

import (
	"math"
	"testing"

	"MTBuild/internal/geometry"
)

func TestEquiangleSkewness(t *testing.T) {
	var hexagon []geometry.Point
	for k := 0; k < 6; k++ {
		angle := float64(k) * math.Pi / 3
		hexagon = append(hexagon, geometry.Point{X: math.Cos(angle), Y: math.Sin(angle)})
	}
	square := []geometry.Point{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2}}
	// прямоугольный равнобедренный: углы 90, 45, 45 при α₀ = 60 —
	// max[(90−60)/120, (60−45)/60] = 0.25
	rightTriangle := []geometry.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	reversed := []geometry.Point{{X: 0, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 0}}

	cases := []struct {
		name    string
		corners []geometry.Point
		want    float64
	}{
		{"regular hexagon", hexagon, 0},
		{"square", square, 0},
		{"right isosceles", rightTriangle, 0.25},
		{"right isosceles, clockwise", reversed, 0.25},
	}
	for _, c := range cases {
		if got := equiangleSkewness(c.corners); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: S = %v, want %v", c.name, got, c.want)
		}
	}
	if got := equiangleSkewness(square[:2]); !math.IsNaN(got) {
		t.Errorf("two corners: S = %v, want NaN", got)
	}
}

// Без контура A — сумма площадей ячеек, так что единственная ячейка-правильный
// шестиугольник — ровно целевой: r = 1, S = 0.
func TestCellQualities_RegularHexagonWithoutContour(t *testing.T) {
	m := Mesh{Polygonal: true, Cells: [][]int{{0, 1, 2, 3, 4, 5}}}
	for k := 0; k < 6; k++ {
		angle := float64(k) * math.Pi / 3
		m.Vertices = append(m.Vertices, geometry.Point{X: 3 + math.Cos(angle), Y: -1 + math.Sin(angle)})
	}
	cell := CellQualities(m, geometry.Geometry{})[0]
	if !cell.Valid || math.Abs(cell.PerimeterRatio-1) > 1e-12 || math.Abs(cell.Skewness) > 1e-12 {
		t.Errorf("got %+v, want r = 1, S = 0", cell)
	}
}

// Круг — правильный 128-угольник радиуса 1, четыре ячейки-четверти: центр и
// 33 вершины контура. Периметр включает все отрезки ячейки вдоль круга, A —
// площадь 128-угольника, для S дуга заменяется хордой — остаётся
// прямоугольный равнобедренный треугольник, S = 0.25.
func TestCellQualities_QuarterDisks(t *testing.T) {
	const sides = 128
	var circle geometry.Polyline
	for k := 0; k < sides; k++ {
		angle := 2 * math.Pi * float64(k) / sides
		circle = append(circle, geometry.Point{X: math.Cos(angle), Y: math.Sin(angle)})
	}
	m := Mesh{Polygonal: true, Vertices: append(geometry.Polyline{{X: 0, Y: 0}}, circle...)}
	for quarter := 0; quarter < 4; quarter++ {
		cell := []int{0}
		for k := quarter * sides / 4; k <= (quarter+1)*sides/4; k++ {
			cell = append(cell, 1+k%sides)
		}
		m.Cells = append(m.Cells, cell)
	}

	circleArea := math.Abs(circle.SignedArea())
	circleLength := float64(sides) * circle[0].Dist(circle[1])
	wantRatio := (2 + circleLength/4) / math.Sqrt(8*math.Sqrt(3)*circleArea/4)
	for i, cell := range CellQualities(m, geometry.Geometry{Outer: circle}) {
		if math.Abs(cell.PerimeterRatio-wantRatio) > 1e-9 || math.Abs(cell.Skewness-0.25) > 1e-9 {
			t.Errorf("cell %d: got r = %v, S = %v; want r = %v, S = 0.25", i, cell.PerimeterRatio, cell.Skewness, wantRatio)
		}
	}
}

func TestCellQualities_PerimeterUsesMeshEdges(t *testing.T) {
	outer := geometry.Polyline{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
	m := Mesh{Polygonal: true, Vertices: outer[:3], Cells: [][]int{{0, 1, 2}}}
	// Замыкающее ребро ячейки — диагональ квадрата длины √2.
	want := (2 + math.Sqrt2) / math.Sqrt(8*math.Sqrt(3))
	cell := CellQualities(m, geometry.Geometry{Outer: outer})[0]
	if !cell.Valid || math.Abs(cell.PerimeterRatio-want) > 1e-12 {
		t.Errorf("cell r = %v, valid = %v; want r = %v", cell.PerimeterRatio, cell.Valid, want)
	}
}

// Полоса 10×1, порезанная на 10 единичных квадратов. Вертикальные рёбра
// концами лежат на контуре, но идут поперёк полосы (путь по контуру между
// концами куда длиннее) — это не граница, и средние квадраты остаются
// квадратами: S = 0, периметр 4. У крайних квадратов граница — три стороны,
// после замены хордой углов не остаётся: S = NaN, и в среднее S они не идут.
func TestCellQualities_StripCrossingEdges(t *testing.T) {
	strip := geometry.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 1}, {X: 0, Y: 1}}
	var m Mesh
	m.Polygonal = true
	for x := 0; x <= 10; x++ {
		m.Vertices = append(m.Vertices, geometry.Point{X: float64(x), Y: 0}, geometry.Point{X: float64(x), Y: 1})
	}
	for x := 0; x < 10; x++ {
		bottomLeft, bottomRight := 2*x, 2*(x+1)
		m.Cells = append(m.Cells, []int{bottomLeft, bottomRight, bottomRight + 1, bottomLeft + 1})
	}
	g := geometry.Geometry{Outer: strip}

	wantRatio := 4 / math.Sqrt(8*math.Sqrt(3)*10/10)
	cells := CellQualities(m, g)
	for i, cell := range cells {
		if i == 0 || i == 9 {
			if !math.IsNaN(cell.Skewness) {
				t.Errorf("end cell %d: S = %v, want NaN", i, cell.Skewness)
			}
			continue
		}
		if math.Abs(cell.PerimeterRatio-wantRatio) > 1e-12 || math.Abs(cell.Skewness) > 1e-12 {
			t.Errorf("cell %d: got r = %v, S = %v; want r = %v, S = 0", i, cell.PerimeterRatio, cell.Skewness, wantRatio)
		}
	}
	if q := EvaluatePoly(m, g); math.Abs(q.SkewnessAvg) > 1e-12 || math.IsNaN(q.SkewnessMax) {
		t.Errorf("EvaluatePoly: SkewnessAvg = %v, SkewnessMax = %v; want 0, 0 (NaN cells skipped)", q.SkewnessAvg, q.SkewnessMax)
	}
}
