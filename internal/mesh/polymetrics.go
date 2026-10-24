package mesh

import (
	"math"

	"MTBuild/internal/geometry"
)

// PolyQualityMetrics — метрики качества для многоугольной сетки (Mesh.Cells),
// аналог QualityMetrics для треугольников, но там где треугольнику меряли
// углы и G, тут меряем "на сколько ячейка похожа на круг" (изопериметрия) и
// на сколько равномерна сетка по числу сторон и по площади ячеек.
type PolyQualityMetrics struct {
	Cells int

	AvgSides float64 // среднее число сторон на ячейку (для равномерной сетки из точек ~6, как у сот)
	MinSides int
	MaxSides int
	// SidesHistogram — сколько ячеек с числом сторон 3,4,5,6,7,8,9,10+
	// (индекс = сторон-3, индекс 7 = "10 и больше").
	SidesHistogram [8]int

	// IsoAvg/IsoMin — изопериметрический коэффициент 4*pi*Area/Perimeter^2.
	// 1 у идеального круга, у правильного n-угольника < 1 и растёт к 1 с
	// ростом n (шестиугольник ~0.91, квадрат ~0.79, треугольник ~0.60).
	// Чем ближе к 1 — тем "круглее"/регулярнее ячейка.
	IsoAvg float64
	IsoMin float64

	// AreaCV — коэффициент вариации площади ячеек (std/mean). Чем меньше,
	// тем равномернее сетка по размеру ячеек.
	AreaMean float64
	AreaCV   float64
}

// EvaluatePoly считает PolyQualityMetrics по всей многоугольной сетке.
// Вырожденные ячейки (< 3 вершин, нулевая площадь) пропускаются везде, кроме
// Cells (считаются, но не портят остальные средние — их эффективно 0 таких
// на нормальной сетке).
func EvaluatePoly(m Mesh) PolyQualityMetrics {
	var q PolyQualityMetrics
	q.Cells = len(m.Cells)
	if q.Cells == 0 {
		return q
	}

	q.MinSides = math.MaxInt32
	q.IsoMin = math.Inf(1)
	var sidesSum float64
	var isoSum float64
	var areas []float64
	var areaSum float64
	valid := 0

	for i := range m.Cells {
		poly := m.Cell(i)
		n := len(poly)
		if n < 3 {
			continue
		}
		area := polyArea(poly)
		perim := polyPerimeter(poly)
		if area <= 0 || perim <= 0 {
			continue
		}
		valid++

		sidesSum += float64(n)
		if n < q.MinSides {
			q.MinSides = n
		}
		if n > q.MaxSides {
			q.MaxSides = n
		}
		bucket := n - 3
		if bucket > 7 {
			bucket = 7
		}
		q.SidesHistogram[bucket]++

		iso := 4 * math.Pi * area / (perim * perim)
		isoSum += iso
		if iso < q.IsoMin {
			q.IsoMin = iso
		}

		areas = append(areas, area)
		areaSum += area
	}

	if valid == 0 {
		q.MinSides = 0
		q.IsoMin = 0
		return q
	}

	n := float64(valid)
	q.AvgSides = sidesSum / n
	q.IsoAvg = isoSum / n
	q.AreaMean = areaSum / n

	var varSum float64
	for _, a := range areas {
		d := a - q.AreaMean
		varSum += d * d
	}
	std := math.Sqrt(varSum / n)
	if q.AreaMean > 0 {
		q.AreaCV = std / q.AreaMean
	}
	return q
}

// polyArea — площадь простого многоугольника по формуле шнурков (знак не
// важен, тут всегда берём модуль — намотка ячейки не гарантирована).
func polyArea(pts []geometry.Point) float64 {
	var a float64
	n := len(pts)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return math.Abs(a) / 2
}

func polyPerimeter(pts []geometry.Point) float64 {
	var p float64
	n := len(pts)
	for i := 0; i < n; i++ {
		p += pts[i].Dist(pts[(i+1)%n])
	}
	return p
}
