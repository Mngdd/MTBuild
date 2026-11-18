// Package mesh держит универсальную итоговую сетку + метрики для нее. Опирается только на пакет geometry (для Point)
package mesh

import "MTBuild/internal/geometry"

// Mesh это 2д сетка: вершины + либо треуги (Triangles), либо произвольные
// многоугольные ячейки (Cells). Какой из двух вариантов — говорит Polygonal,
// его выставляет сам алгоритм.
// Cells нужен для не-Делоне методов (например Вороного из заряженных частиц):
// там ячейка — не треугольник, а многоугольник с произвольным числом вершин.
// пока что из-за делоне у нас не гарантирован порядок обхода вершин для треугольника (по/против часовой)!!!
type Mesh struct {
	Vertices  []geometry.Point
	Triangles [][3]int // индексы вершин, по 3 на треугольник
	Cells     [][]int  // индексы вершин по кругу ячейки, по 1 срезу на многоугольник
	Polygonal bool     // true — сетка из Cells, false — из Triangles
	// Sites — генератор (частица) каждой ячейки, Sites[i] для Cells[i]; только
	// у сеток, где ячейка строится вокруг точки (Вороной из charge). nil, если
	// у алгоритма генераторов нет.
	Sites []geometry.Point
	// Period — сторона квадрата-периода у периодической сетки, 0 — сетка
	// обычная. Квадрат — [0, Period] по X и Y, сетка повторяется со сдвигом на
	// Period по обеим осям. Ячейки у края выходят за квадрат: копии соседних
	// квадратов стыкуются с ними без швов.
	Period float64
}

// Tri возвращаем вершины i-го треугольника
func (m Mesh) Tri(i int) (a, b, c geometry.Point) {
	t := m.Triangles[i]
	return m.Vertices[t[0]], m.Vertices[t[1]], m.Vertices[t[2]]
}

// Centroid возвращаем центр пересечения медиан (барицентр) i-го треугольника
func (m Mesh) Centroid(i int) geometry.Point {
	a, b, c := m.Tri(i)
	return geometry.Point{X: (a.X + b.X + c.X) / 3, Y: (a.Y + b.Y + c.Y) / 3}
}

// Cell возвращаем вершины i-й многоугольной ячейки, по кругу.
func (m Mesh) Cell(i int) []geometry.Point {
	idx := m.Cells[i]
	out := make([]geometry.Point, len(idx))
	for k, vi := range idx {
		out[k] = m.Vertices[vi]
	}
	return out
}
