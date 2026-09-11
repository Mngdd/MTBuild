package sph

import (
	"math"

	"MTBuild/internal/geometry"
)

// cellGrid is a uniform spatial hash for neighbour queries within a fixed
// radius. Rebuilt each iteration; adequate for the MVP particle counts.
type cellGrid struct {
	cell    float64
	minX    float64
	minY    float64
	cols    int
	rows    int
	buckets [][]int
	pos     []geometry.Point
}

func newCellGrid(ps []Particle, cell float64) *cellGrid {
	if cell <= 0 {
		cell = 1
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range ps {
		minX = math.Min(minX, p.Pos.X)
		minY = math.Min(minY, p.Pos.Y)
		maxX = math.Max(maxX, p.Pos.X)
		maxY = math.Max(maxY, p.Pos.Y)
	}
	g := &cellGrid{
		cell: cell,
		minX: minX,
		minY: minY,
		cols: int((maxX-minX)/cell) + 2,
		rows: int((maxY-minY)/cell) + 2,
		pos:  make([]geometry.Point, len(ps)),
	}
	g.buckets = make([][]int, g.cols*g.rows)
	for i, p := range ps {
		g.pos[i] = p.Pos
		b := g.index(p.Pos)
		g.buckets[b] = append(g.buckets[b], i)
	}
	return g
}

func (g *cellGrid) cellOf(p geometry.Point) (int, int) {
	cx := int((p.X - g.minX) / g.cell)
	cy := int((p.Y - g.minY) / g.cell)
	return clampInt(cx, 0, g.cols-1), clampInt(cy, 0, g.rows-1)
}

func (g *cellGrid) index(p geometry.Point) int {
	cx, cy := g.cellOf(p)
	return cy*g.cols + cx
}

// forNeighbours calls fn with the index of every particle in the 3x3 block of
// cells around p (a superset of particles within `cell` of p).
func (g *cellGrid) forNeighbours(p geometry.Point, fn func(j int)) {
	cx, cy := g.cellOf(p)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || x >= g.cols || y < 0 || y >= g.rows {
				continue
			}
			for _, j := range g.buckets[y*g.cols+x] {
				fn(j)
			}
		}
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
