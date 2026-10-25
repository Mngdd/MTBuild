package mesher

import (
	"math"

	"MTBuild/internal/geometry"
)

// ParticleMetrics — дешёвые метрики облака частиц на одной итерации
// релаксации. Считаются без построения сетки, поэтому их собирают всегда.
type ParticleMetrics struct {
	// MeanStep — средний сдвиг подвижной частицы за эту итерацию, в долях её
	// целевого размера ячейки. Падает к нулю, когда облако успокоилось.
	MeanStep float64
	// NeighborSpread — коэффициент вариации (std/mean) расстояния от
	// подвижной частицы до ближайшего соседа, делённого на её целевой размер.
	// 0 — все стоят ровно на своём шаге; чем больше, тем неравномернее.
	NeighborSpread float64
}

// NeighborSpread считает ParticleMetrics.NeighborSpread.
// points — все частицы; target(i) — целевой размер ячейки у частицы i, а 0
// значит «эту частицу не мерить, она только сосед для других» (например,
// закреплённые граничные). Возвращает 0, если мерить некого.
//
// Ближайшего соседа ищет по равномерной сетке ячеек размером со средний
// target: обходит кольца ячеек вокруг частицы, пока ближайший найденный
// сосед не окажется ближе, чем самое близкое необойдённое кольцо.
func NeighborSpread(points []geometry.Point, target func(i int) float64) float64 {
	var targetSum float64
	measuredCount := 0
	for i := range points {
		if t := target(i); t > 0 {
			targetSum += t
			measuredCount++
		}
	}
	if measuredCount == 0 || len(points) < 2 {
		return 0
	}
	cellSize := targetSum / float64(measuredCount)

	type cellKey struct{ x, y int }
	keyOf := func(p geometry.Point) cellKey {
		return cellKey{int(math.Floor(p.X / cellSize)), int(math.Floor(p.Y / cellSize))}
	}
	cells := make(map[cellKey][]int, len(points))
	for i, p := range points {
		k := keyOf(p)
		cells[k] = append(cells[k], i)
	}

	var ratioSum, ratioSquareSum float64
	for i, p := range points {
		t := target(i)
		if t <= 0 {
			continue
		}
		center := keyOf(p)
		nearest := math.Inf(1)
		// кольцо ring — ячейки на чебышёвском расстоянии ring от своей; любая
		// точка в нём не ближе (ring-1)*cellSize, так что дальше можно не идти
		for ring := 0; float64(ring-1)*cellSize < nearest && ring <= len(points); ring++ {
			for dx := -ring; dx <= ring; dx++ {
				for dy := -ring; dy <= ring; dy++ {
					if max(abs(dx), abs(dy)) != ring {
						continue
					}
					for _, j := range cells[cellKey{center.x + dx, center.y + dy}] {
						if j != i {
							nearest = math.Min(nearest, p.Dist(points[j]))
						}
					}
				}
			}
		}
		ratio := nearest / t
		ratioSum += ratio
		ratioSquareSum += ratio * ratio
	}
	mean := ratioSum / float64(measuredCount)
	if mean == 0 {
		return 0
	}
	variance := math.Max(ratioSquareSum/float64(measuredCount)-mean*mean, 0)
	return math.Sqrt(variance) / mean
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
