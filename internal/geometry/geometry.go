// Package geometry - базовая логика 2д геомы прописана в этом пакете
// после загрузки 2д файла он конвертируется в Geometry и только потом приходит в mesher.
package geometry

import (
	"fmt"
	"math"
)

type Point struct {
	X, Y float64
}

// Dist 2д евклидово расстояние >= 0
func (p Point) Dist(q Point) float64 { return math.Hypot(p.X-q.X, p.Y-q.Y) }

// Polyline is an ordered list of points describing a closed contour. The
// closing edge (last -> first) is implicit; callers must not duplicate the
// first point at the end.
type Polyline []Point

// SignedArea формула площади многоугольника по гауссу. положительная => против часовой/counter-clockwise (CCW).
func (pl Polyline) SignedArea() float64 {
	if len(pl) < 3 {
		return 0
	}
	var a float64
	for i := range pl {
		j := (i + 1) % len(pl)
		a += pl[i].X*pl[j].Y - pl[j].X*pl[i].Y
	}
	return a / 2
}

// IsCCW проверка на то, идем ли мы против часовой(counter-clockwise)?
func (pl Polyline) IsCCW() bool { return pl.SignedArea() > 0 }

func (pl Polyline) Reversed() Polyline {
	out := make(Polyline, len(pl))
	for i, p := range pl {
		out[len(pl)-1-i] = p
	}
	return out
}

// Geometry 2д "домен" который замешить надо: внешняя граница + неотриц к-во дырок
// По дефолту Outer явл CCW и каждая дырка CW (таким образом мы всегда смотрим в нашу область).
type Geometry struct {
	Outer Polyline
	Holes []Polyline
}

// BoundingBox коробка-контур для объекта Geometry
func (g Geometry) BoundingBox() (min, max Point) {
	min = Point{math.Inf(1), math.Inf(1)}
	max = Point{math.Inf(-1), math.Inf(-1)}
	visit := func(pl Polyline) {
		for _, p := range pl {
			min.X, min.Y = math.Min(min.X, p.X), math.Min(min.Y, p.Y)
			max.X, max.Y = math.Max(max.X, p.X), math.Max(max.Y, p.Y)
		}
	}
	visit(g.Outer)
	// TODO: мы должны аутер область сами генерить!!! тогда и визитить дырки не надо...
	for _, h := range g.Holes {
		visit(h)
	}
	return min, max
}

// Normalized валидируем домен: проверяем к-во дырок + корректим направления контуров аутер и дырок
func (g Geometry) Normalized() (Geometry, error) {
	if len(g.Outer) < 3 {
		return Geometry{}, fmt.Errorf("geometry: outer contour has %d points, need >= 3", len(g.Outer))
	}
	out := Geometry{Outer: g.Outer}
	if !out.Outer.IsCCW() {
		out.Outer = out.Outer.Reversed()
	}
	for _, h := range g.Holes {
		if len(h) < 3 {
			return Geometry{}, fmt.Errorf("geometry: hole has %d points, need >= 3", len(h))
		}
		if h.IsCCW() {
			h = h.Reversed()
		}
		out.Holes = append(out.Holes, h)
	}
	return out, nil
}
