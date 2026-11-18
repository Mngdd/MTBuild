package mesh

import (
	"math"

	"MTBuild/internal/geometry"
)

// CellQuality — метрики одной многоугольной ячейки по методике «Качество сеток
// в круговой области»: нормированный периметр rᵢ и равноугольная асимметрия S.
type CellQuality struct {
	// PerimeterRatio — rᵢ = pᵢ / p_hex. pᵢ — полный периметр ячейки, участок
	// внешней границы меряется по контуру, а не хордой. p_hex = √(8√3·A/N) —
	// периметр правильного шестиугольника целевой площади A/N (A — площадь
	// области, N — число ячеек). 1 — как у такого шестиугольника, больше — хуже.
	PerimeterRatio float64
	// Skewness — равноугольная асимметрия S. Для k-угольника α₀ = (k−2)·180°/k,
	// S = max[(α_max − α₀)/(180° − α₀), (α₀ − α_min)/α₀]. 0 — все углы равны,
	// больше — углы искажены сильнее. Граничный участок ячейки (подряд идущие
	// рёбра по контуру) заменяется хордой между его концами. NaN — после этого у
	// ячейки не осталось трёх углов (она целиком граница).
	Skewness float64
	// Valid — false у вырожденной ячейки (меньше 3 вершин или нулевая площадь);
	// остальные поля у неё нулевые, в средние она не идёт.
	Valid bool
}

// boundaryEdgeArcToChord — во сколько раз путь по контуру между концами ребра
// может быть длиннее самого ребра, чтобы ребро считалось лежащим вдоль контура.
// У ребра по дуге отношение около 1 (на углу контура — до √2 ≈ 1.41), у ребра
// поперёк узкого места оба конца тоже на контуре, но путь по контуру между
// ними во много раз длиннее.
const boundaryEdgeArcToChord = 1.5

// CellQualities считает CellQuality для каждой ячейки многоугольной сетки.
//
// m: сетка (Mesh.Cells); g: контур, по которому её строили, в тех же
// координатах. У периодической сетки (m.Period > 0) контур не используется:
// A = Period², границы нет. Пустой g — тоже без границы, A — сумма площадей
// ячеек. Возвращает срез длины len(m.Cells), i-й элемент — для m.Cells[i].
func CellQualities(m Mesh, g geometry.Geometry) []CellQuality {
	out := make([]CellQuality, len(m.Cells))
	if len(m.Cells) == 0 {
		return out
	}

	var tracks []contourTrack
	if m.Period <= 0 && len(g.Outer) >= 3 {
		tracks = append(tracks, newContourTrack(g.Outer))
		for _, hole := range g.Holes {
			if len(hole) >= 3 {
				tracks = append(tracks, newContourTrack(hole))
			}
		}
	}

	validCells := 0
	var cellAreaSum float64
	for i := range m.Cells {
		if poly := m.Cell(i); len(poly) >= 3 {
			if area := polyArea(poly); area > 0 {
				validCells++
				cellAreaSum += area
			}
		}
	}
	if validCells == 0 {
		return out
	}
	domainArea := cellAreaSum
	switch {
	case m.Period > 0:
		domainArea = m.Period * m.Period
	case len(tracks) > 0:
		domainArea = math.Abs(g.Outer.SignedArea())
		for _, hole := range g.Holes {
			domainArea -= math.Abs(hole.SignedArea())
		}
	}
	hexPerimeter := math.Sqrt(8 * math.Sqrt(3) * domainArea / float64(validCells))

	// Где каждая вершина на контуре (если на нём): считаем один раз на вершину,
	// а не на каждую ячейку, где она встречается.
	onContour := make([]contourSpot, len(m.Vertices))
	if len(tracks) > 0 {
		minBox, maxBox := g.BoundingBox()
		tolerance := 1e-9 * minBox.Dist(maxBox) // вершины у края алгоритм прижимает на контур точно, с точностью до округления
		for v, p := range m.Vertices {
			onContour[v] = locateOnContours(tracks, p, tolerance)
		}
	}
	alongContour := func(a, b int) (arc float64, ok bool) {
		spotA, spotB := onContour[a], onContour[b]
		if !spotA.on || !spotB.on || spotA.track != spotB.track {
			return 0, false
		}
		arc = tracks[spotA.track].arcBetween(spotA.along, spotB.along)
		return arc, arc <= boundaryEdgeArcToChord*m.Vertices[a].Dist(m.Vertices[b])
	}

	for i, cell := range m.Cells {
		poly := m.Cell(i)
		if len(poly) < 3 || polyArea(poly) <= 0 {
			continue
		}
		count := len(cell)
		var perimeter float64
		boundaryEdge := make([]bool, count) // ребро cell[k] -> cell[k+1] лежит вдоль контура
		for k := range cell {
			a, b := cell[k], cell[(k+1)%count]
			if arc, ok := alongContour(a, b); ok {
				boundaryEdge[k] = true
				perimeter += arc
			} else {
				perimeter += m.Vertices[a].Dist(m.Vertices[b])
			}
		}

		// граничный участок -> хорда: выкидываем вершины внутри участка (у
		// которых оба соседних ребра вдоль контура), его концы остаются
		var corners []geometry.Point
		for k := range cell {
			if !(boundaryEdge[(k+count-1)%count] && boundaryEdge[k]) {
				corners = append(corners, poly[k])
			}
		}

		out[i] = CellQuality{
			PerimeterRatio: perimeter / hexPerimeter,
			Skewness:       equiangleSkewness(corners),
			Valid:          true,
		}
	}
	return out
}

// equiangleSkewness — равноугольная асимметрия S многоугольника (см.
// CellQuality.Skewness). corners: вершины по кругу, обход в любую сторону.
// NaN, если вершин меньше трёх или площадь нулевая.
func equiangleSkewness(corners []geometry.Point) float64 {
	count := len(corners)
	if count < 3 {
		return math.NaN()
	}
	orientation := geometry.Polyline(corners).SignedArea()
	if orientation == 0 {
		return math.NaN()
	}
	minAngle, maxAngle := math.Inf(1), math.Inf(-1)
	for k := range corners {
		prev, here, next := corners[(k+count-1)%count], corners[k], corners[(k+1)%count]
		toPrevX, toPrevY := prev.X-here.X, prev.Y-here.Y
		toNextX, toNextY := next.X-here.X, next.Y-here.Y
		// угол от направления на следующую вершину до направления на
		// предыдущую: при обходе против часовой это внутренний угол (у
		// вогнутой вершины он больше 180°), при обходе по часовой — наоборот
		cross := toNextX*toPrevY - toNextY*toPrevX
		dot := toNextX*toPrevX + toNextY*toPrevY
		if orientation < 0 {
			cross = -cross
		}
		angle := math.Atan2(cross, dot)
		if angle < 0 {
			angle += 2 * math.Pi
		}
		minAngle = math.Min(minAngle, angle)
		maxAngle = math.Max(maxAngle, angle)
	}
	ideal := float64(count-2) * math.Pi / float64(count)
	return math.Max((maxAngle-ideal)/(math.Pi-ideal), (ideal-minAngle)/ideal)
}

// contourTrack — замкнутый контур с длиной, накопленной от первой точки: по
// ней меряется путь вдоль контура между двумя его точками.
type contourTrack struct {
	points     geometry.Polyline
	cumulative []float64 // cumulative[i] — длина от points[0] до points[i]
	length     float64   // длина всего контура, с замыкающим ребром
}

func newContourTrack(points geometry.Polyline) contourTrack {
	track := contourTrack{points: points, cumulative: make([]float64, len(points))}
	for i := range points {
		track.cumulative[i] = track.length
		track.length += points[i].Dist(points[(i+1)%len(points)])
	}
	return track
}

// locate — ближайшая к p точка контура: расстояние до неё и её место вдоль
// контура (длина от первой точки).
func (track contourTrack) locate(p geometry.Point) (dist, along float64) {
	dist = math.Inf(1)
	count := len(track.points)
	for i := range track.points {
		a, b := track.points[i], track.points[(i+1)%count]
		dx, dy := b.X-a.X, b.Y-a.Y
		lengthSquared := dx*dx + dy*dy
		t := 0.0
		if lengthSquared > 0 {
			t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/lengthSquared))
		}
		foot := geometry.Point{X: a.X + t*dx, Y: a.Y + t*dy}
		if d := p.Dist(foot); d < dist {
			dist, along = d, track.cumulative[i]+t*math.Sqrt(lengthSquared)
		}
	}
	return dist, along
}

// arcBetween — путь вдоль контура между двумя его точками (места вдоль
// контура alongA, alongB), короткой из двух дорог.
func (track contourTrack) arcBetween(alongA, alongB float64) float64 {
	direct := math.Abs(alongA - alongB)
	return math.Min(direct, track.length-direct)
}

// contourSpot — где вершина лежит на контуре: on — лежит ли вообще, track —
// номер контура, along — место вдоль него.
type contourSpot struct {
	on    bool
	track int
	along float64
}

// locateOnContours — на каком из контуров лежит p (ближе tolerance) и где.
func locateOnContours(tracks []contourTrack, p geometry.Point, tolerance float64) contourSpot {
	best := contourSpot{}
	bestDist := math.Inf(1)
	for trackIndex, track := range tracks {
		if dist, along := track.locate(p); dist < bestDist {
			bestDist = dist
			best = contourSpot{on: dist < tolerance, track: trackIndex, along: along}
		}
	}
	return best
}
