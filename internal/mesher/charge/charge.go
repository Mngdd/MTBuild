// Package charge — второй алгоритм построения сетки ("черная коробка" #2):
// центры ячеек — одноимённо заряженные частицы, которые отталкиваются по
// закону F = k / r^n (n — параметр, по умолчанию 2 — обычный Кулон), с
// затуханием, чтобы они успокоились, а не разлетелись. Поля целевого размера,
// как h_t у sph-quadtree, тут нет — фиксированное число частиц сеется с
// примерно равномерной плотностью, и одно отталкивание раскладывает их в
// ровную упаковку (физически — релаксация 2D однокомпонентной плазмы, в духе
// задачи Томсона; и, не случайно, это способ получить центроидальное разбиение
// Вороного через силы, а не итерациями Ллойда).
//
// Итоговые ячейки берутся из диаграммы Вороного осевших частиц, а не из
// триангуляции Делоне напрямую — поэтому, в отличие от grid и sph-quadtree,
// выход этого алгоритма — Mesh.Cells (многоугольники), а не Mesh.Triangles.
// Метрики качества для них — mesh.EvaluatePoly.
//
// Упрощения MVP: только равномерная плотность (без поля размеров и адаптации
// по кривизне); силы считаются попарно за O(n^2) (на количествах частиц MVP
// нормально — без пространственного хеша); ячейки Вороного строятся для
// каждой внутренней частицы (граничные заряды — только стенки для
// релаксации, своих ячеек у них нет) из циркумцентров Делоне с сортировкой по
// углу, а не через нормальную структуру half-edge. Вместо честного отсечения
// ячеек по контуру частицы у края отражаются через него (как в PolyMesher):
// граница между частицей и её отражением сама ложится на контур. Вершины,
// оказавшиеся снаружи, просто прижимаются к контуру, поэтому точно в угол
// контура вершина не попадает. После релаксации несколько итераций
// Ллойда (lloydStep) выравнивают ячейки, силы дают только начальную расстановку.
package charge

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"time"

	"MTBuild/internal/delaunay"
	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
)

func init() { mesher.Register("charge-voronoi", func() mesher.Mesher { return Mesher{} }) }

type Mesher struct{}

func (Mesher) Name() string { return "charge-voronoi" }

// Параметры — в долях габарита геометрии, как у sph-quadtree, чтобы оба
// алгоритма в UI ощущались одинаково.
var (
	pH       = mesher.ParamSpec{Key: "h", Label: "Target cell spacing", Min: 0.02, Max: 0.5, Def: 0.09, Step: 0.005}
	pExp     = mesher.ParamSpec{Key: "n", Label: "Force exponent (F ~ k/r^n)", Min: 1, Max: 10, Def: 4, Step: 0.25}
	pK       = mesher.ParamSpec{Key: "k", Label: "Force coefficient k", Min: 0, Max: 1, Def: 1, Step: 0.05}
	pDamping = mesher.ParamSpec{Key: "damping", Label: "Velocity damping per step", Min: 0.5, Max: 0.99, Def: 0.85, Step: 0.01}
	pClamp   = mesher.ParamSpec{Key: "clamp", Label: "Limit step length", Kind: mesher.KindBool, Def: 1}
	pMaxStep = mesher.ParamSpec{Key: "max_step", Label: "Max step (× h)", Min: 0.05, Max: 1, Def: 0.3, Step: 0.05, DependsOn: "clamp"}
	// Без затухания предел шага не отпускает: сила ~1/h^n на порядки больше него,
	// каждая частица до конца шагает ровно на max_step и не оседает, а итоговая
	// сетка — снимок этого дрожания. Гасим предел линейно к нулю.
	pStepDecay = mesher.ParamSpec{Key: "step_decay", Label: "Decay step limit to zero", Kind: mesher.KindBool, Def: 1, DependsOn: "clamp"}
	// Шаг кольца граничных зарядов. Сдвигает баланс плотности край/середина:
	// замерено на zalesak и эллипсе, 1h при n=3 ровнее старых 0.5h при n=2.
	pBoundaryStep = mesher.ParamSpec{Key: "boundary_step", Label: "Boundary charge spacing (× h)", Min: 0.25, Max: 2, Def: 1, Step: 0.05}
	// Случайный посев вместо шестиугольной решётки — сравнить, насколько итог
	// зависит от начальной укладки. Частиц столько же, сколько дала бы решётка.
	pRandomInit = mesher.ParamSpec{Key: "random_init", Label: "Random initial placement", Kind: mesher.KindBool, Def: 0}
	// Фиксированный сид генератора случайных чисел вместо случайного
	pFixSeed = mesher.ParamSpec{Key: "fix_seed", Label: "Prevent random seed", Kind: mesher.KindBool, Def: 1}
	pSeed    = mesher.ParamSpec{Key: "seed", Label: "Random seed", Min: 1, Max: 1024, Def: 1, Step: 1, DependsOn: "fix_seed"}
	// Попытаться раскидать слипшиеся частицы - сейчас из-за сварки оно устарело и юзлес
	pSeparate = mesher.ParamSpec{Key: "separate", Label: "Push apart coincident particles", Kind: mesher.KindBool, Def: 0}
	// Вершины ячеек ближе этого склеиваются в одну — всегда, настраивается только
	// расстояние. Убирает ложные рёбра ~0 длины (почти "круговые" четвёрки
	// частиц) и этим возвращает шестиугольники; при большем допуске начинают
	// сливаться настоящие короткие рёбра и растёт число пятиугольников.
	// Замерено на ellipse/zalesak: максимум шестиугольников около 0.02h.
	pWeldTol = mesher.ParamSpec{Key: "weld_tol", Label: "Merge cell vertices closer than (× h)", Min: 0.0001, Max: 0.1, Def: 0.02, Step: 0.0001}
	pIters   = mesher.ParamSpec{Key: "iters", Label: "Relaxation iterations", Min: 10, Max: 2000, Def: 300, Step: 10}
	// Итерации Ллойда после релаксации: частица -> центр масс своей ячейки.
	// Выравнивают площади у края. 30 — с запасом: на zalesak при 10 частица,
	// прижатая к контуру, ещё не успевает уйти (площади 0.40..0.99 h², при 30 —
	// 0.64..0.99). 0 — выключено.
	pLloydIters = mesher.ParamSpec{Key: "lloyd_iters", Label: "Lloyd iterations (0 = off)", Min: 0, Max: 150, Def: 30, Step: 1}
)

func (Mesher) Params() []mesher.ParamSpec {
	return []mesher.ParamSpec{pH, pExp, pK, pDamping, pClamp, pMaxStep, pStepDecay, pBoundaryStep, pRandomInit, pFixSeed, pSeed, pSeparate, pWeldTol, pIters, pLloydIters}
}

func (Mesher) Generate(ctx context.Context, g geometry.Geometry, p mesher.Params, sink mesher.Sink) (mesh.Mesh, mesh.QualityMetrics, error) {
	h := p.Get(pH)
	expo := p.Get(pExp)
	k := p.Get(pK)
	clamp := p.Bool(pClamp)
	stepDecay := p.Bool(pStepDecay)
	boundaryStep := p.Get(pBoundaryStep) * h
	randomInit := p.Bool(pRandomInit)
	separate := p.Bool(pSeparate)
	weldTol := p.Get(pWeldTol) * h
	damping := p.Get(pDamping)
	iters := int(p.Get(pIters))
	lloydIters := int(p.Get(pLloydIters))
	totalIters := iters + lloydIters

	// --- нормализация в единичный габарит, как у sph-quadtree --------------
	min, max := g.BoundingBox()
	cx, cy := (min.X+max.X)/2, (min.Y+max.Y)/2
	scale := math.Max(max.X-min.X, max.Y-min.Y)
	if scale == 0 {
		return mesh.Mesh{}, mesh.QualityMetrics{}, errDegenerate
	}
	// нормируем ибо параметры такие как h требуют нормировки размера
	norm := func(pt geometry.Point) geometry.Point {
		return geometry.Point{X: (pt.X - cx) / scale, Y: (pt.Y - cy) / scale}
	}
	// расплющить обратно, заготовили заранее
	denorm := func(pt geometry.Point) geometry.Point {
		return geometry.Point{X: pt.X*scale + cx, Y: pt.Y*scale + cy}
	}
	// нормир геома
	gn := mapGeometry(g, norm)

	// --- инишал спавн частиц: фиксированные граничные заряды + подвижные внутренние ------
	// Граничные заряды — только стенки для релаксации: в ячейки они не идут, до
	// контура сетку дотягивают отражения (см. buildCells).
	sink.Progress("init", 0)
	boundary := resampleBoundary(gn.Outer, boundaryStep) // сыпем у границ
	for _, hole := range gn.Holes {
		boundary = append(boundary, resampleBoundary(hole, boundaryStep)...)
	}
	// Случайный сид — из того же диапазона, что и pSeed, чтобы его можно было
	// вбить обратно и повторить запуск. Отдаём наружу только его (randomSeed),
	// заданный пользователем он и так знает.
	var seed, randomSeed int64
	if p.Bool(pFixSeed) {
		seed = int64(math.Round(p.Get(pSeed))) // round, а не int(): из GUI целые приходят как 579.99997 (ползунок во float32)
	} else {
		randomSeed = 1 + time.Now().UnixNano()%int64(pSeed.Max)
		seed = randomSeed
	}
	rng := rand.New(rand.NewSource(seed))
	var interior []geometry.Point
	if randomInit {
		interior = interiorRandom(gn, h, rng) // сыпем внутрянку на рандом
	} else {
		interior = interiorLattice(gn, h, rng) // сыпем внутрянку
	}
	if len(interior) < 3 {
		return mesh.Mesh{}, mesh.QualityMetrics{}, errTooFewCells
	}
	if err := ctx.Err(); err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	sink.Progress("init", 1)
	sink.Snapshot(mesher.Frame{Stage: "init", Particles: denormAll(append(append([]geometry.Point{}, interior...), boundary...), denorm), Seed: randomSeed})

	// --- релаксация: попарное отталкивание + затухание ---------------------
	vel := make([]geometry.Point, len(interior))
	maxStep := p.Get(pMaxStep) * h // предел смещения частицы за шаг; держит систему в рамках при любых damping/n
	// граничные заряды — только соседи для NeighborSpread, мерим внутренние
	withBoundary := make([]geometry.Point, len(interior)+len(boundary))
	copy(withBoundary[len(interior):], boundary)
	interiorTarget := func(i int) float64 {
		if i < len(interior) {
			return h
		}
		return 0
	}
	for it := 0; it < iters; it++ {
		if err := ctx.Err(); err != nil {
			return mesh.Mesh{}, mesh.QualityMetrics{}, err
		}
		if separate {
			separateCoincident(interior, vel, boundary, gn, 0.1*h, rng)
		}
		forces := make([]geometry.Point, len(interior))
		for i := range interior {
			var fx, fy float64
			for j := range interior {
				if j == i {
					continue
				}
				fx2, fy2 := repel(interior[i], interior[j], k, expo)
				fx += fx2
				fy += fy2
			}
			for j := range boundary {
				fx2, fy2 := repel(interior[i], boundary[j], k, expo)
				fx += fx2
				fy += fy2
			}
			forces[i] = geometry.Point{X: fx, Y: fy}
		}
		var stepSum float64
		stepLimit := maxStep
		if stepDecay {
			stepLimit = maxStep * (1 - float64(it)/float64(iters))
		}
		for i := range interior {
			vel[i].X = damping*vel[i].X + forces[i].X
			vel[i].Y = damping*vel[i].Y + forces[i].Y
			vel[i] = sanitise(vel[i])
			if clamp {
				vel[i] = clampMag(vel[i], stepLimit)
			}
			np := geometry.Point{X: interior[i].X + vel[i].X, Y: interior[i].Y + vel[i].Y}
			if !gn.Contains(np) {
				np = gn.Nearest(np) // граничные заряды почти не выпускают; редкого беглеца хватает вернуть на край
				vel[i] = geometry.Point{}
			}
			stepSum += interior[i].Dist(np)
			interior[i] = np
		}
		if it%10 == 0 || it == iters-1 {
			copy(withBoundary, interior)
			sink.Progress("relax", float64(it)/float64(iters))
			sink.Snapshot(mesher.Frame{
				Stage: "relax", Iter: it, TotalIters: totalIters, Particles: denormAll(interior, denorm),
				Particle: mesher.ParticleMetrics{
					MeanStep:       stepSum / float64(len(interior)) / h,
					NeighborSpread: mesher.NeighborSpread(withBoundary, interiorTarget),
				},
				BuildMesh: func() (mesh.Mesh, error) { return buildCells(interior, gn, h, weldTol, denorm) },
			})
		}
	}
	if separate {
		// последний шаг цикла мог снова снэпнуть двоих в одну точку
		separateCoincident(interior, vel, boundary, gn, 0.1*h, rng)
	}
	sink.Progress("relax", 1)

	// --- итерации Ллойда: выравниваем ячейки ---------------------------------
	// Номера итераций продолжают релаксацию, чтобы счётчик и графики метрик шли подряд.
	for it := 0; it < lloydIters; it++ {
		if err := ctx.Err(); err != nil {
			return mesh.Mesh{}, mesh.QualityMetrics{}, err
		}
		moved, err := lloydStep(interior, gn, h, weldTol)
		if err != nil {
			return mesh.Mesh{}, mesh.QualityMetrics{}, err
		}
		var stepSum float64
		for i := range interior {
			stepSum += interior[i].Dist(moved[i])
		}
		interior = moved
		copy(withBoundary, interior)
		sink.Progress("lloyd", float64(it)/float64(lloydIters))
		sink.Snapshot(mesher.Frame{
			Stage: "lloyd", Iter: iters + it, TotalIters: totalIters, Particles: denormAll(interior, denorm),
			Particle: mesher.ParticleMetrics{
				MeanStep:       stepSum / float64(len(interior)) / h,
				NeighborSpread: mesher.NeighborSpread(withBoundary, interiorTarget),
			},
			BuildMesh: func() (mesh.Mesh, error) { return buildCells(interior, gn, h, weldTol, denorm) },
		})
	}
	sink.Progress("lloyd", 1)

	// --- ячейки Вороного по осевшим частицам -------------------------------
	sink.Progress("voronoi", 0)
	m, err := buildCells(interior, gn, h, weldTol, denorm)
	if err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	if len(m.Cells) == 0 {
		return mesh.Mesh{}, mesh.QualityMetrics{}, errNoCells
	}
	sink.Progress("voronoi", 1)
	sink.Snapshot(mesher.Frame{Stage: "done", Iter: iters, Particles: m.Vertices, Mesh: &m})

	// QualityMetrics — метрики для треугольной сетки, здесь неприменимы
	// (Mesh.Triangles пуст); вызывающий проверяет Mesh.Polygonal и зовёт
	// mesh.EvaluatePoly(m) — см. mesh.PolyQualityMetrics.
	return m, mesh.QualityMetrics{}, nil
}

// buildCells строит ячейки Вороного вокруг частиц.
// interior — подвижные частицы в нормированных координатах (сам срез не
// меняется, так что годится и посреди релаксации — для живых метрик);
// gn — нормированный контур; h — шаг ячеек; weldTol — допуск склейки вершин,
// уже × h; denorm — перевод обратно в мировые координаты.
// Возвращает многоугольную сетку в мировых координатах.
func buildCells(interior []geometry.Point, gn geometry.Geometry, h, weldTol float64, denorm func(geometry.Point) geometry.Point) (mesh.Mesh, error) {
	// Совпавшие частицы склеиваем в одну перед делоне всегда — до отражений,
	// чтобы внутренние шли в sites ровно первыми nInterior штуками
	// (mergeCoincident собирает новый срез, исходный не трогает)
	interior = mergeCoincident(interior)
	mirrors := reflectAcrossBoundary(interior, gn, reflectDist*h)
	// sites = [внутренние | отражения]: индекс < nInterior — настоящая частица
	nInterior := len(interior)
	sites := append(append([]geometry.Point{}, interior...), mirrors...)
	tris, err := delaunay.Triangulate(sites) // делонешим
	if err != nil {
		return mesh.Mesh{}, err
	}
	// Фильтра треугольников по центроиду теперь нет: треугольник "частица + отражение"
	// всегда наполовину снаружи, а его циркумцентр — как раз вершина на
	// контуре. Ошибочное соединение вершин через прорезь теперь контрится отражениями!

	// теперь для каждой частицы собираем треугольники к которым она относится
	trisAtSite := triangleAdjacency(len(sites), tris)

	// Отсев слишком мелких ячеек. но как будто особо не вызывается
	// TODO: вроде как полезно но может и бесполезен стать в будущем
	minCellArea := 0.01 * h * h

	// --- вершины Вороного: по одной на треугольник Делоне, общие для соседей ---
	// тут все треуги (с учетом тех которые появились после отражения)
	circumcenters := make([]geometry.Point, len(tris)) // тупо все циркумы
	vertexPos := make([]geometry.Point, len(tris))     // тут все внутренние + проекция на грань внешних циркумов
	valid := make([]bool, len(tris))
	for ti, t := range tris {
		c, ok := circumcenter(sites[t[0]], sites[t[1]], sites[t[2]])
		if !ok {
			continue // вырожденный — эту вершину пропустят все ячейки одинаково
		}
		circumcenters[ti], vertexPos[ti], valid[ti] = c, c, true
		// Вершина между частицей и отражением лежит на контуре (на прямой
		// стенке — точно, на кривой — почти): прижимаем, чтобы край сетки шёл
		// ровно по контуру. Так же — любую вершину снаружи (у углов, где
		// отражение одно, а нужно два).
		if usesMirror(t, nInterior) || !gn.Contains(c) {
			vertexPos[ti] = gn.Nearest(c)
		}
	}
	// тут свариваем микротреугольники в "один" просто дав им один номер (kinda)
	circumcentersByTriangles := weldVertices(vertexPos, valid, weldTol)

	var cells [][]int
	var cellSites []geometry.Point // частица каждой оставленной ячейки, по индексу cells (мировые координаты)
	// ячейки только у внутренних частиц: отражения — вспомогательные точки
	// снаружи, их ячейки в сетку не идут
	for i := 0; i < nInterior; i++ {
		cell := voronoiCell(sites[i], trisAtSite[i], circumcenters, circumcentersByTriangles)
		if len(cell) < 3 || polyArea(gather(vertexPos, cell)) < minCellArea {
			continue // убиваем вырожденные клетки
		}
		cells = append(cells, cell)
		cellSites = append(cellSites, denorm(sites[i]))
	}
	vertsNorm, cells := compactVertices(vertexPos, cells)

	worldPos := make([]geometry.Point, len(vertsNorm))
	for i, v := range vertsNorm {
		worldPos[i] = denorm(v)
	}
	return mesh.Mesh{Vertices: worldPos, Cells: cells, Polygonal: true, Sites: cellSites}, nil
}

// lloydStep — одна итерация Ллойда: строит ячейки и переставляет каждую частицу
// в центр масс её ячейки. Частица остаётся на месте, если её ячейку отсеяли или
// центр масс вне фигуры (у вогнутых мест ячейка бывает невыпуклой).
//
// interior: подвижные частицы (не меняется), gn: нормированный контур, h: шаг
// ячеек, weldTol: допуск склейки вершин, уже × h. Возвращает новый срез той же
// длины и в том же порядке.
func lloydStep(interior []geometry.Point, gn geometry.Geometry, h, weldTol float64) ([]geometry.Point, error) {
	sameCoords := func(p geometry.Point) geometry.Point { return p }
	cellMesh, err := buildCells(interior, gn, h, weldTol, sameCoords)
	if err != nil {
		return nil, err
	}
	centroidBySite := make(map[geometry.Point]geometry.Point, len(cellMesh.Cells))
	for i := range cellMesh.Cells {
		if centroid, ok := polyCentroid(cellMesh.Cell(i)); ok {
			centroidBySite[cellMesh.Sites[i]] = centroid
		}
	}
	moved := make([]geometry.Point, len(interior))
	for i, particle := range interior {
		moved[i] = particle
		if centroid, ok := centroidBySite[particle]; ok && gn.Contains(centroid) {
			moved[i] = centroid
		}
	}
	return moved, nil
}

// polyCentroid — центр масс простого многоугольника.
//
// pts: вершины по кругу, в любую сторону. Возвращает центр масс; ok = false,
// если площадь нулевая и центра нет.
func polyCentroid(pts []geometry.Point) (centroid geometry.Point, ok bool) {
	var doubledArea, sumX, sumY float64
	for i := range pts {
		a, b := pts[i], pts[(i+1)%len(pts)]
		cross := a.X*b.Y - b.X*a.Y
		doubledArea += cross
		sumX += (a.X + b.X) * cross
		sumY += (a.Y + b.Y) * cross
	}
	if math.Abs(doubledArea) < 1e-14 {
		return geometry.Point{}, false
	}
	return geometry.Point{X: sumX / (3 * doubledArea), Y: sumY / (3 * doubledArea)}, true
}

// reflectDist — с какого расстояния от края (× h) частицу отражаем. Ячейка
// частицы ~0.6h в радиусе, так что дальше ~h отражение её уже не касается;
// 1.5h — с запасом, как в PolyMesher.
const reflectDist = 1.5

// sameWallCos — косинус угла, ближе которого две точки контура считаются одной
// стенкой (60°). Участки одной дуги видны почти в одном направлении, и отдельные
// отражения для них дали бы кучу почти совпавших точек.
const sameWallCos = 0.5

// reflectAcrossBoundary — отражения частиц у края через контур (как в
// PolyMesher): для частицы ближе maxDist к контуру — точка p' = 2q − p для
// каждой стенки рядом, где q — точка стенки напротив частицы. Серединный
// перпендикуляр к p–p' проходит через q по касательной к контуру, так что
// граница между ячейкой частицы и ячейкой её отражения сама ложится на край.
// Стенок бывает несколько: в углу и в тонкой фигуре ячейку надо закрыть с
// разных сторон. Отражение, попавшее внутрь фигуры (у вогнутых углов и в узких
// местах), выкидываем — иначе оно отъест кусок у настоящих ячеек. Частица ровно
// на контуре отражений не даёт: они совпали бы с ней самой.
//
// particles: внутренние частицы, gn: фигура (нормированная), maxDist: порог
// расстояния до контура. Возвращает отражения (связь "отражение -> частица" не
// хранится, она не нужна).
func reflectAcrossBoundary(particles []geometry.Point, gn geometry.Geometry, maxDist float64) []geometry.Point {
	var mirrors []geometry.Point
	for _, p := range particles {
		nearest := gn.Nearest(p)
		if d := p.Dist(nearest); d >= maxDist || d < coincideEps {
			continue
		}
		for _, q := range nearbyWalls(p, nearest, gn, maxDist) {
			mirror := geometry.Point{X: 2*q.X - p.X, Y: 2*q.Y - p.Y}
			if !gn.Contains(mirror) {
				mirrors = append(mirrors, mirror)
			}
		}
	}
	return mirrors
}

// nearbyWalls ищет стенки контура вокруг частицы: основания перпендикуляров из p
// на отрезки контура ближе maxDist, по одному на направление (см. sameWallCos).
//
// p: частица, nearest: ближайшая к ней точка контура, она всегда идёт первой
// (у вогнутого угла это вершина, перпендикуляра на неё нет), gn: фигура,
// maxDist: порог расстояния. Возвращает точки на контуре, от ближней к дальней.
func nearbyWalls(p, nearest geometry.Point, gn geometry.Geometry, maxDist float64) []geometry.Point {
	var feet []geometry.Point
	for _, contour := range append([]geometry.Polyline{gn.Outer}, gn.Holes...) {
		for i := range contour {
			a, b := contour[i], contour[(i+1)%len(contour)]
			dx, dy := b.X-a.X, b.Y-a.Y
			lengthSquared := dx*dx + dy*dy
			if lengthSquared == 0 {
				continue
			}
			t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / lengthSquared
			if t <= 0 || t >= 1 {
				continue // перпендикуляр не попадает на отрезок
			}
			foot := geometry.Point{X: a.X + t*dx, Y: a.Y + t*dy}
			if p.Dist(foot) < maxDist {
				feet = append(feet, foot)
			}
		}
	}
	sort.Slice(feet, func(i, j int) bool { return p.Dist(feet[i]) < p.Dist(feet[j]) })

	walls := []geometry.Point{nearest}
	for _, foot := range feet {
		newWall := true
		for _, wall := range walls {
			ax, ay := foot.X-p.X, foot.Y-p.Y
			bx, by := wall.X-p.X, wall.Y-p.Y
			if (ax*bx+ay*by)/(math.Hypot(ax, ay)*math.Hypot(bx, by)) > sameWallCos {
				newWall = false
				break
			}
		}
		if newWall {
			walls = append(walls, foot)
		}
	}
	return walls
}

// usesMirror — есть ли в треугольнике отражение (палим чисто по индексу в какой "половине" массива)
func usesMirror(t [3]int, nInterior int) bool {
	return t[0] >= nInterior || t[1] >= nInterior || t[2] >= nInterior
}

// coincideEps — расстояние, на котором частицы считаются совпавшими: repel
// для них возвращает 0, и без разлёта они так и едут вместе навсегда.
const coincideEps = 1e-9

// separateCoincident разводит совпавшие частицы (separate включён).
//   - две внутренние: в противоположные стороны вдоль разности их скоростей,
//     каждая на dist; если разность нулевая (обе стоят или летят одинаково) —
//     в случайную сторону. Вылетевшую за контур возвращаем на край через Nearest;
//   - внутренняя на граничном заряде: граничные неподвижны, двигается только
//     внутренняя, на 2*dist в случайную сторону, до 8 попыток попасть внутрь.
func separateCoincident(interior, vel, boundary []geometry.Point, gn geometry.Geometry, dist float64, rng *rand.Rand) {
	for i := range interior {
		for j := i + 1; j < len(interior); j++ {
			if interior[i].Dist(interior[j]) >= coincideEps {
				continue
			}
			rx, ry := vel[i].X-vel[j].X, vel[i].Y-vel[j].Y
			rl := math.Hypot(rx, ry)
			var d geometry.Point
			if rl < coincideEps {
				d = randomDir(rng)
			} else {
				d = geometry.Point{X: rx / rl, Y: ry / rl}
			}
			interior[i] = shiftInside(interior[i], d, dist, gn)
			interior[j] = shiftInside(interior[j], d, -dist, gn)
		}
		for _, b := range boundary {
			if interior[i].Dist(b) >= coincideEps {
				continue
			}
			for try := 0; try < 8; try++ {
				d := randomDir(rng)
				np := geometry.Point{X: interior[i].X + 2*dist*d.X, Y: interior[i].Y + 2*dist*d.Y}
				if gn.Contains(np) {
					interior[i] = np
					break
				}
			}
		}
	}
}

func shiftInside(p, d geometry.Point, s float64, gn geometry.Geometry) geometry.Point {
	np := geometry.Point{X: p.X + s*d.X, Y: p.Y + s*d.Y}
	if !gn.Contains(np) {
		np = gn.Nearest(np)
	}
	return np
}

func randomDir(rng *rand.Rand) geometry.Point {
	a := 2 * math.Pi * rng.Float64()
	return geometry.Point{X: math.Cos(a), Y: math.Sin(a)}
}

// mergeCoincident оставляет из каждой группы совпавших (ближе coincideEps)
// точек первую, порядок сохраняется. O(N²), но один раз за Generate — на фоне
// релаксации незаметно.
//
// pts: точки (не меняется). Возвращает новый срез без дублей.
func mergeCoincident(pts []geometry.Point) []geometry.Point {
	out := pts[:0:0]
	for _, p := range pts {
		dup := false
		for _, q := range out {
			if p.Dist(q) < coincideEps {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

// repel возвращает силу на a от одноимённого заряда b: величина k/r^n,
// направление — от b. Защищено от r=0 (совпавших точек).
func repel(a, b geometry.Point, k, expo float64) (fx, fy float64) {
	dx, dy := a.X-b.X, a.Y-b.Y
	r := math.Hypot(dx, dy)
	if r < coincideEps {
		return 0, 0
	}
	mag := k / math.Pow(r, expo)
	return mag * dx / r, mag * dy / r
}

func clampMag(p geometry.Point, lim float64) geometry.Point {
	m := math.Hypot(p.X, p.Y)
	if m > lim && m > 0 {
		return geometry.Point{X: p.X * lim / m, Y: p.Y * lim / m}
	}
	return p
}

func sanitise(p geometry.Point) geometry.Point {
	fix := func(v float64) float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		return v
	}
	return geometry.Point{X: fix(p.X), Y: fix(p.Y)}
}

// triangleAdjacency возвращает для каждой частицы индексы (в tris)
// треугольников, в которые она входит.
func triangleAdjacency(nSites int, tris [][3]int) [][]int {
	out := make([][]int, nSites)
	for ti, t := range tris {
		for _, vi := range t {
			out[vi] = append(out[vi], ti)
		}
	}
	return out
}

// voronoiCell — ячейка частицы site как список id вершин, упорядоченных по
// углу циркумцентра вокруг частицы. circumcentersByTriangles[ti] — id вершины
// треугольника ti после сварки (см. weldVertices), -1 — треугольник
// вырожденный, вершины нет.
//
// site: сама частица, myTris: треуги в которых есть точка, raw: циркумцентры
// по индексу треугольника
func voronoiCell(site geometry.Point, myTris []int, raw []geometry.Point, circumcentersByTriangles []int) []int {
	type v struct {
		id  int
		ang float64
	}
	vs := make([]v, 0, len(myTris)) // углы
	for _, ti := range myTris {
		if circumcentersByTriangles[ti] < 0 {
			continue
		}
		vs = append(vs, v{circumcentersByTriangles[ti], math.Atan2(raw[ti].Y-site.Y, raw[ti].X-site.X)})
	}
	// сортируем по углу
	sort.Slice(vs, func(a, b int) bool { return vs[a].ang < vs[b].ang })

	// чуть раньше была сварка (пометили близкие) - теперь схлопываем
	out := make([]int, 0, len(vs))
	for _, x := range vs {
		if len(out) == 0 || out[len(out)-1] != x.id {
			out = append(out, x.id)
		}
	}
	// края повторные типа срезать
	if len(out) > 1 && out[0] == out[len(out)-1] {
		out = out[:len(out)-1]
	}
	return out
}

// weldVertices — глобальная сварка: circumcentersByTriangles[i] = id вершины,
// которая поглотила i (первая по порядку в радиусе tol), -1 для невалидных.
// Хеш-сетка с шагом tol — соседей ищем в 3×3 клетках, O(n).
func weldVertices(pos []geometry.Point, valid []bool, tol float64) []int {
	circumcentersByTriangles := make([]int, len(pos))
	if tol <= 0 {
		// сварка выключена: каждая валидная вершина сама себе id (и без
		// деления на ноль в ключе хеш-сетки)
		for i := range pos {
			circumcentersByTriangles[i] = -1
			if valid[i] {
				circumcentersByTriangles[i] = i
			}
		}
		return circumcentersByTriangles
	}
	type key struct{ x, y int }
	grid := map[key][]int{}
	for i, p := range pos {
		circumcentersByTriangles[i] = -1
		if !valid[i] {
			continue
		}
		circumcentersByTriangles[i] = i
		k := key{int(math.Floor(p.X / tol)), int(math.Floor(p.Y / tol))}
	search:
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for _, j := range grid[key{k.x + dx, k.y + dy}] {
					if pos[j].Dist(p) < tol {
						circumcentersByTriangles[i] = j
						break search
					}
				}
			}
		}
		if circumcentersByTriangles[i] == i {
			grid[k] = append(grid[k], i)
		}
	}
	return circumcentersByTriangles
}

// compactVertices оставляет только вершины, на которые ссылаются ячейки, и
// перенумеровывает ячейки под новый плотный массив.
func compactVertices(pos []geometry.Point, cells [][]int) ([]geometry.Point, [][]int) {
	newID := make([]int, len(pos))
	for i := range newID {
		newID[i] = -1
	}
	var verts []geometry.Point
	for _, c := range cells {
		for k, id := range c {
			if newID[id] < 0 {
				newID[id] = len(verts)
				verts = append(verts, pos[id])
			}
			c[k] = newID[id]
		}
	}
	return verts, cells
}

func gather(pos []geometry.Point, ids []int) []geometry.Point {
	out := make([]geometry.Point, len(ids))
	for k, id := range ids {
		out[k] = pos[id]
	}
	return out
}

// polyArea — площадь простого многоугольника по формуле шнурков (знак не
// важен — она идёт только в сравнение "слишком мелкая или нет", где намотка
// роли не играет).
func polyArea(pts []geometry.Point) float64 {
	var a float64
	n := len(pts)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return math.Abs(a) / 2
}

// circumcenter возвращает центр окружности через a, b, c. ok = false для
// вырожденного треугольника (точки на одной прямой).
func circumcenter(a, b, c geometry.Point) (geometry.Point, bool) {
	d := 2 * (a.X*(b.Y-c.Y) + b.X*(c.Y-a.Y) + c.X*(a.Y-b.Y))
	if math.Abs(d) < 1e-12 {
		return geometry.Point{}, false
	}
	a2 := a.X*a.X + a.Y*a.Y
	b2 := b.X*b.X + b.Y*b.Y
	c2 := c.X*c.X + c.Y*c.Y
	ux := (a2*(b.Y-c.Y) + b2*(c.Y-a.Y) + c2*(a.Y-b.Y)) / d
	uy := (a2*(c.X-b.X) + b2*(a.X-c.X) + c2*(b.X-a.X)) / d
	return geometry.Point{X: ux, Y: uy}, true
}

// resampleBoundary идёт вдоль замкнутого контура и ставит точки примерно
// через step — та же техника, что в internal/mesher/grid (там она не
// экспортирована, а делить её между двумя MVP-алгоритмами не стоит того).
func resampleBoundary(pl geometry.Polyline, step float64) []geometry.Point {
	if len(pl) < 2 || step <= 0 {
		return append([]geometry.Point(nil), pl...)
	}
	var out []geometry.Point
	carry := 0.0
	for i := 0; i < len(pl); i++ {
		a := pl[i]
		b := pl[(i+1)%len(pl)]
		seg := a.Dist(b)
		if seg == 0 {
			continue
		}
		dx, dy := (b.X-a.X)/seg, (b.Y-a.Y)/seg
		for d := carry; d < seg; d += step {
			out = append(out, geometry.Point{X: a.X + dx*d, Y: a.Y + dy*d})
		}
		carry = math.Mod(carry-seg, step)
		if carry < 0 {
			carry += step
		}
	}
	return out
}

// interiorRandom сеет столько же подвижных зарядов, сколько дала бы
// шестиугольная решётка (interiorLattice), но равномерно случайно по фигуре.
//
// g: нормированная фигура, step: шаг решётки (нужен только для числа частиц),
// rng: источник случайности. Возвращает частицы внутри фигуры.
func interiorRandom(g geometry.Geometry, step float64, rng *rand.Rand) []geometry.Point {
	count := len(interiorLattice(g, step, rng))
	min, max := g.BoundingBox()
	out := make([]geometry.Point, 0, count)
	for len(out) < count {
		p := geometry.Point{
			X: min.X + rng.Float64()*(max.X-min.X),
			Y: min.Y + rng.Float64()*(max.Y-min.Y),
		}
		if g.Contains(p) {
			out = append(out, p)
		}
	}
	return out
}

// interiorLattice сеет подвижные заряды на шестиугольной решётке с частицей
// ровно в центре габарита. Только равномерная плотность — в отличие от
// sph-quadtree, тут нет поля размеров, по которому прореживать, расстановку
// делает отталкивание. Решётка шестиугольная и с центром в фигуре, потому что
// укладку потом ни силы, ни Ллойд не меняют: с квадратной решётки на круге из
// 18 ячеек выходило около 25 разных укладок, часто с пятиугольником в центре,
// а отсюда почти всегда 1-6-12. Джиттер маленький и нужен только чтобы сломать
// идеальную симметрию решётки (в ней силы уравновешены в точности); для Делоне
// ровная шестиугольная решётка не вырождена — на окружности каждого
// треугольника ровно 3 точки.
//
// g: нормированная фигура, step: шаг решётки, rng: источник джиттера.
// Возвращает частицы внутри фигуры.
func interiorLattice(g geometry.Geometry, step float64, rng *rand.Rand) []geometry.Point {
	min, max := g.BoundingBox()
	centerX, centerY := (min.X+max.X)/2, (min.Y+max.Y)/2
	rowStep := step * math.Sqrt(3) / 2
	rows := int((max.Y-min.Y)/rowStep) + 2
	cols := int((max.X-min.X)/step) + 2
	var out []geometry.Point
	for row := -rows; row <= rows; row++ {
		oddRowShift := 0.0
		if row%2 != 0 {
			oddRowShift = step / 2
		}
		for col := -cols; col <= cols; col++ {
			p := geometry.Point{
				X: centerX + float64(col)*step + oddRowShift + (rng.Float64()-0.5)*step*0.05,
				Y: centerY + float64(row)*rowStep + (rng.Float64()-0.5)*step*0.05,
			}
			if g.Contains(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func denormAll(pts []geometry.Point, denorm func(geometry.Point) geometry.Point) []geometry.Point {
	out := make([]geometry.Point, len(pts))
	for i, p := range pts {
		out[i] = denorm(p)
	}
	return out
}

func mapGeometry(g geometry.Geometry, f func(geometry.Point) geometry.Point) geometry.Geometry {
	mapPl := func(pl geometry.Polyline) geometry.Polyline {
		out := make(geometry.Polyline, len(pl))
		for i, p := range pl {
			out[i] = f(p)
		}
		return out
	}
	out := geometry.Geometry{Outer: mapPl(g.Outer)}
	for _, h := range g.Holes {
		out.Holes = append(out.Holes, mapPl(h))
	}
	return out
}

type chargeErr string

func (e chargeErr) Error() string { return string(e) }

const (
	errDegenerate  = chargeErr("charge: degenerate geometry (zero-size bounding box)")
	errTooFewCells = chargeErr("charge: h too large for this geometry (fewer than 3 interior sites seeded)")
	errNoCells     = chargeErr("charge: h too large for this geometry (no cell survived)")
)
