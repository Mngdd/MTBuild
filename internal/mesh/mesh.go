// Package mesh держит универсальную итоговую сетку + метрики для нее. Опирается только на пакет geometry (для Point)
package mesh

import "MTBuild/internal/geometry"

// Mesh это 2д сетка: вершины + треуги которые просто держат индексы вершин
// пока что из-за делоне у нас не гарантирован порядок обхода вершин для треугольника (по/против часовой)!!!
type Mesh struct {
	Vertices  []geometry.Point
	Triangles [][3]int
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
