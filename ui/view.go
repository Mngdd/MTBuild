package main

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
)

// Иконки глаза для легенды — из набора Material, который Gio и так тянет
// для своей темы.
var (
	iconShown  = mustIcon(icons.ActionVisibility)
	iconHidden = mustIcon(icons.ActionVisibilityOff)
)

// mustIcon — иконка из встроенного набора. data: байты из
// golang.org/x/exp/shiny/materialdesign/icons. Ошибка тут — битые байты в
// самой библиотеке, а не что-то от пользователя, поэтому паника.
func mustIcon(data []byte) *widget.Icon {
	icon, err := widget.NewIcon(data)
	if err != nil {
		panic("ui: bad built-in icon: " + err.Error())
	}
	return icon
}

// Палитра повторяет SVG-рендер (internal/viz/svg.go), чтобы превью в окне и
// экспортированный SVG выглядели одинаково.
var (
	colViewBG       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colMeshFill     = color.NRGBA{R: 0xee, G: 0xf4, B: 0xfb, A: 0xff}
	colMeshEdge     = color.NRGBA{R: 0x46, G: 0x82, B: 0xb4, A: 0xff}
	colPolyFill     = color.NRGBA{R: 0xfb, G: 0xee, B: 0xe0, A: 0xff}
	colPolyEdge     = color.NRGBA{R: 0xb4, G: 0x7a, B: 0x34, A: 0xff}
	colOuterContour = color.NRGBA{R: 0x15, G: 0x60, B: 0xb0, A: 0xff}
	colHoleContour  = color.NRGBA{R: 0xb0, G: 0x15, B: 0x15, A: 0xff}
	colSite         = color.NRGBA{R: 0xd6, G: 0x28, B: 0x28, A: 0xff} // частица-генератор ячейки (крестик)
	colCentroid     = color.NRGBA{R: 0x2a, G: 0x22, B: 0x1c, A: 0xff} // центр масс ячейки (точка)
	// Призраки — копии периодической сетки вокруг квадрата-периода, бледнее самой сетки.
	colGhostFill = color.NRGBA{R: 0xfd, G: 0xf8, B: 0xf2, A: 0xff}
	colGhostEdge = color.NRGBA{R: 0xd8, G: 0xbd, B: 0x9c, A: 0xff}
)

const (
	edgeWidth    = unit.Dp(0.8) // толщина рёбер ячеек на экране — не зависит от зума
	contourWidth = unit.Dp(1.6) // толщина внешнего контура и дырок
	// Маркеры центров ячеек — тоже постоянного размера на экране:
	centroidRadius    = unit.Dp(2.5) // радиус точки центроида
	siteCrossHalfSize = unit.Dp(2.2) // полуразмах крестика частицы (от центра до конца луча по каждой оси)
	siteCrossWidth    = unit.Dp(1.5) // толщина линий крестика

	// Легенда:
	legendEyeSize    = unit.Dp(21) // сторона иконки глаза
	legendMarkerSize = unit.Dp(9)  // сторона квадрата, в который вписан маркер слоя
	legendTextSize   = unit.Sp(14.3)
	fitMargin        = unit.Dp(12) // поле вокруг геометрии при zoom=1

	wheelNotch   = 120  // Scroll.Y за один щелчок колеса на Windows (сырое WM_MOUSEWHEEL)
	zoomPerNotch = 1.15 // во сколько раз приближает один щелчок колеса
	minZoom      = 0.2
	maxZoom      = 500
)

// scrollAnyAmount — диапазон для pointer.Filter: принимаем прокрутку любой
// силы, иначе Gio отдаст событие колеса кому-то другому под курсором.
var scrollAnyAmount = pointer.ScrollRange{Min: -1 << 30, Max: 1 << 30}

// meshView — векторный просмотр готовой сетки с камерой. Сетка хранится как
// есть (вершины + треугольники/ячейки) и перерисовывается путями Gio каждый
// кадр в экранных координатах, поэтому зум не мылит картинку и толщина линий
// на экране постоянна. Колесо — зум к курсору, перетаскивание левой кнопкой —
// сдвиг, кнопка "Сбросить вид" — назад к "вписать целиком".
type meshView struct {
	mesh  mesh.Mesh
	geom  geometry.Geometry
	edges [][2]int // уникальные рёбра сетки (пары индексов вершин), считаются один раз в setMesh
	// centroids — центр масс каждой многоугольной ячейки (centroids[i] для
	// mesh.Cells[i]); считаются в setMesh, у треугольной сетки nil — там
	// центры не показываем.
	centroids []geometry.Point
	hasMesh   bool
	// periodContour — квадрат-период периодической сетки (Mesh.Period > 0):
	// рисуется вместо контура, по которому запускали, — его алгоритм
	// игнорировал. Один и тот же, пока период прежний, чтобы sameContour
	// видел "контур не меняли" и не сбрасывал камеру между запусками.
	periodContour geometry.Geometry

	zoom float32   // множитель поверх "вписать целиком": 1 — исходный вид
	pan  f32.Point // сдвиг картинки в пикселях вида от положения "по центру"

	dragging bool
	dragLast f32.Point // позиция курсора на прошлом событии перетаскивания

	resetClick  widget.Clickable
	cellMetrics cellMetricView

	// Видимость слоёв точек переживает новые результаты: спрятал частицы — они
	// остаются спрятанными и при следующем Start.
	sitesLayer     dotLayer
	centroidsLayer dotLayer
}

// dotLayer — один слой точек на холсте (частицы или центроиды): скрыт ли он и
// кнопка-глаз в легенде, которая его переключает.
type dotLayer struct {
	hidden bool
	toggle widget.Clickable
}

// setMesh показывает новую сетку. m: готовая сетка; g: контур, по которому её
// строили (его bbox задаёт "вписать целиком", сам контур рисуется поверх).
// qualities: метрики по порядку ячеек, nil для треугольной сетки.
// Камера сбрасывается, только если сменился контур — при перезапуске на том же
// контуре с другими параметрами зум и сдвиг остаются, чтобы сравнивать один и
// тот же увеличенный участок. У периодической сетки вместо g — квадрат-период.
func (v *meshView) setMesh(m mesh.Mesh, g geometry.Geometry, qualities []mesh.CellQuality) {
	if m.Period > 0 {
		g = v.periodSquare(m.Period)
	}
	if !v.hasMesh || !sameContour(v.geom, g) {
		v.resetCamera()
	}
	v.mesh, v.geom, v.edges, v.hasMesh = m, g, uniqueEdges(m), true
	v.centroids = nil
	v.cellMetrics.reset(len(qualities))
	if m.Polygonal {
		v.centroids = make([]geometry.Point, len(m.Cells))
		for i := range m.Cells {
			polygon := geometry.Polyline(m.Cell(i))
			v.centroids[i] = polygonCentroid(polygon)
			if i < len(qualities) {
				v.cellMetrics.setCell(i, qualities[i], polygon, v.centroids[i])
			}
		}
	}
}

// periodSquare — квадрат [0, period]² как контур. period: Mesh.Period.
// Возвращает тот же объект, пока период не сменился (см. periodContour).
func (v *meshView) periodSquare(period float64) geometry.Geometry {
	if outer := v.periodContour.Outer; len(outer) == 0 || outer[2].X != period {
		v.periodContour = geometry.Geometry{Outer: geometry.Polyline{
			{X: 0, Y: 0}, {X: period, Y: 0}, {X: period, Y: period}, {X: 0, Y: period},
		}}
	}
	return v.periodContour
}

// polygonCentroid — центр масс многоугольника (не среднее вершин: у ячейки с
// густо натыканными вершинами с одной стороны среднее туда бы и уехало).
// cell: вершины по кругу, в любую сторону обхода. Для вырожденной ячейки
// (площадь ~0) возвращает среднее вершин.
func polygonCentroid(cell []geometry.Point) geometry.Point {
	var twiceArea, sumX, sumY float64
	count := len(cell)
	for k := 0; k < count; k++ {
		a, b := cell[k], cell[(k+1)%count]
		cross := a.X*b.Y - b.X*a.Y
		twiceArea += cross
		sumX += (a.X + b.X) * cross
		sumY += (a.Y + b.Y) * cross
	}
	if math.Abs(twiceArea) < 1e-14 {
		var mean geometry.Point
		for _, p := range cell {
			mean.X += p.X / float64(count)
			mean.Y += p.Y / float64(count)
		}
		return mean
	}
	return geometry.Point{X: sumX / (3 * twiceArea), Y: sumY / (3 * twiceArea)}
}

// cellSites — частицы ячеек, если алгоритм их отдал и их ровно по одной на
// ячейку; иначе nil (нечего показывать).
func (v *meshView) cellSites() []geometry.Point {
	if !v.mesh.Polygonal || len(v.mesh.Sites) != len(v.mesh.Cells) {
		return nil
	}
	return v.mesh.Sites
}

// uniqueEdges — все рёбра ячеек сетки (треугольников или многоугольников),
// каждое ровно один раз, хотя у соседних ячеек оно общее. m: сетка. Возвращает
// пары индексов в m.Vertices, меньший индекс первым.
// Зачем один раз, а не обводить ячейки целиком: Gio заливает обводку по
// правилу nonzero, и внутри одного пути перекрывающиеся обводки общего ребра
// соседних ячеек частично гасят друг друга — на экране рёбра пропадали
// случайными кусками. У обводки отдельного отрезка ориентация от направления
// не зависит, так что пересечения в вершинах только складываются.
func uniqueEdges(m mesh.Mesh) [][2]int {
	seen := make(map[[2]int]bool)
	var edges [][2]int
	addCycle := func(vertexIndices []int) {
		count := len(vertexIndices)
		if count < 2 {
			return
		}
		for k := 0; k < count; k++ {
			a, b := vertexIndices[k], vertexIndices[(k+1)%count]
			edge := [2]int{min(a, b), max(a, b)}
			if a != b && !seen[edge] {
				seen[edge] = true
				edges = append(edges, edge)
			}
		}
	}
	if m.Polygonal {
		for _, cell := range m.Cells {
			addCycle(cell)
		}
	} else {
		for _, tri := range m.Triangles {
			addCycle(tri[:])
		}
	}
	return edges
}

// sameContour — тот же ли это объект контура, а не просто похожий: geomState
// держит один и тот же срез Outer, пока пользователь не выберет другую фигуру
// (даже повторный выбор того же пресета создаёт новый срез), так что
// совпадение адреса первой точки и есть "контур не меняли".
func sameContour(a, b geometry.Geometry) bool {
	return len(a.Outer) > 0 && len(a.Outer) == len(b.Outer) && &a.Outer[0] == &b.Outer[0]
}

func (v *meshView) resetCamera() {
	v.zoom, v.pan, v.dragging = 1, f32.Point{}, false
}

// Layout рисует сетку на весь отведённый прямоугольник (gtx.Constraints.Max),
// кнопки вида в правом верхнем углу и легенду точек (с глазами для
// скрытия) в левом верхнем. Возвращает размер всей области.
func (v *meshView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	size := gtx.Constraints.Max
	if v.resetClick.Clicked(gtx) {
		v.resetCamera()
	}
	v.cellMetrics.update(gtx)
	for _, layer := range []*dotLayer{&v.sitesLayer, &v.centroidsLayer} {
		if layer.toggle.Clicked(gtx) {
			layer.hidden = !layer.hidden
		}
	}
	v.handleInput(gtx, size)

	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, colViewBG)
	{
		area := clip.Rect{Max: size}.Push(gtx.Ops)
		event.Op(gtx.Ops, v)
		if v.dragging {
			pointer.CursorGrabbing.Add(gtx.Ops)
		} else {
			pointer.CursorGrab.Add(gtx.Ops)
		}
		area.Pop()
	}
	v.drawMesh(gtx, th, size)

	gtx.Constraints.Min = size
	// Легенда — тоже после холста, чтобы клики по её пунктам доставались ей.
	if len(v.centroids) > 0 {
		layout.NW.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				items := []layout.FlexChild{}
				if v.cellSites() != nil {
					items = append(items,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.sitesLayer.legendItem(gtx, th, colSite, drawCrossMarker, "частица")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
				}
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.centroidsLayer.legendItem(gtx, th, colCentroid, drawDotMarker, "центроид")
				}))
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			})
		})
	}
	if v.cellMetrics.active() {
		layout.SE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.cellMetrics.scale(gtx, th)
			})
		})
	}
	if v.cellMetrics.open {
		v.cellMetrics.backdrop.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: size}
		})
	}
	layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			reset := material.Button(th, &v.resetClick, "Сбросить вид")
			reset.TextSize, reset.Background = unit.Sp(12), colTextSecond
			reset.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}
			if len(v.cellMetrics.labels) == 0 {
				return reset.Layout(gtx)
			}
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
				layout.Rigid(reset.Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.cellMetrics.menu(gtx, th)
				}),
			)
		})
	})
	return layout.Dimensions{Size: size}
}

// legendItem — пункт легенды: глаз, маркер слоя и подпись, всё вместе — одна
// невидимая кнопка (клик в любое место строки прячет/показывает слой).
// th: тема; markerColor: цвет маркеров слоя на холсте; drawMarker: как
// нарисовать маркер (drawDotMarker/drawCrossMarker); label: что эти маркеры
// значат. У скрытого слоя глаз перечёркнут, а маркер с подписью бледные.
// Возвращает размер пункта.
func (layer *dotLayer) legendItem(gtx layout.Context, th *material.Theme, markerColor color.NRGBA, drawMarker func(*op.Ops, color.NRGBA, int), label string) layout.Dimensions {
	eye, textColor := iconShown, colTextSecond
	if layer.hidden {
		eye, markerColor, textColor = iconHidden, faded(markerColor), faded(colTextSecond)
	}
	return layer.toggle.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				side := gtx.Dp(legendEyeSize)
				gtx.Constraints = layout.Exact(image.Pt(side, side))
				return eye.Layout(gtx, textColor)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				side := gtx.Dp(legendMarkerSize)
				drawMarker(gtx.Ops, markerColor, side)
				return layout.Dimensions{Size: image.Pt(side, side)}
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := material.Label(th, legendTextSize, label)
				text.Color = textColor
				return text.Layout(gtx)
			}),
		)
	})
}

// drawDotMarker — маркер центроида для легенды: круг, вписанный в квадрат
// side×side пикселей от текущего начала координат. markerColor: цвет.
func drawDotMarker(ops *op.Ops, markerColor color.NRGBA, side int) {
	paint.FillShape(ops, markerColor, clip.Ellipse{Max: image.Pt(side, side)}.Op(ops))
}

// drawCrossMarker — маркер частицы для легенды: крестик "×" на весь квадрат
// side×side пикселей, с той же пропорцией толщины к размаху, что на холсте.
// markerColor: цвет.
func drawCrossMarker(ops *op.Ops, markerColor color.NRGBA, side int) {
	half := float32(side) / 2
	width := float32(side) * float32(siteCrossWidth) / float32(2*siteCrossHalfSize)
	center := f32.Pt(half, half)
	paint.FillShape(ops, markerColor, clip.Stroke{Path: crossesAt(ops, []f32.Point{center}, half), Width: width}.Op())
}

// faded — тот же цвет, но полупрозрачный: так легенда показывает скрытый слой.
func faded(c color.NRGBA) color.NRGBA {
	c.A = 0x60
	return c
}

// handleInput разбирает мышь над холстом: нажатие левой кнопки начинает
// перетаскивание, движение двигает картинку, колесо зумит к курсору.
// size: размер холста в пикселях (нужен зуму, чтобы знать центр вида).
func (v *meshView) handleInput(gtx layout.Context, size image.Point) {
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  v,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollY: scrollAnyAmount,
		})
		if !ok {
			return
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonPrimary) {
				v.dragging, v.dragLast = true, e.Position
			}
		case pointer.Drag:
			if v.dragging {
				v.pan = v.pan.Add(e.Position.Sub(v.dragLast))
				v.dragLast = e.Position
			}
		case pointer.Release, pointer.Cancel:
			v.dragging = false
		case pointer.Scroll:
			v.zoomAt(e.Position, e.Scroll.Y, size)
		}
	}
}

// zoomAt меняет масштаб так, чтобы точка сетки под курсором осталась под ним.
// cursor: позиция курсора в пикселях холста; wheelDelta: Scroll.Y события
// (отрицательное — колесо от себя, приближаем); size: размер холста.
func (v *meshView) zoomAt(cursor f32.Point, wheelDelta float32, size image.Point) {
	factor := float32(math.Pow(zoomPerNotch, float64(-wheelDelta/wheelNotch)))
	newZoom := min(max(v.zoom*factor, minZoom), maxZoom)
	center := f32.Pt(float32(size.X)/2, float32(size.Y)/2)
	// Курсор сейчас на cursorFromOrigin пикселей от точки, куда попадает центр
	// мира; после зума это расстояние вырастет в newZoom/zoom раз — сдвигаем
	// pan так, чтобы курсор снова оказался над той же точкой.
	cursorFromOrigin := cursor.Sub(center).Sub(v.pan)
	v.pan = cursor.Sub(center).Sub(cursorFromOrigin.Mul(newZoom / v.zoom))
	v.zoom = newZoom
}

// worldToScreen возвращает перевод точки мира в пиксели холста для текущей
// камеры. size: размер холста; marginPx: поле вокруг геометрии при zoom=1.
// Ось Y мира смотрит вверх, экрана — вниз, поэтому y переворачивается.
func (v *meshView) worldToScreen(size image.Point, marginPx int) func(geometry.Point) f32.Point {
	bbMin, bbMax := v.geom.BoundingBox()
	if period := v.mesh.Period; period > 0 {
		// у периодической сетки при zoom=1 в кадр попадают и призраки — по
		// полквадрата с каждой стороны
		bbMin = geometry.Point{X: bbMin.X - period/2, Y: bbMin.Y - period/2}
		bbMax = geometry.Point{X: bbMax.X + period/2, Y: bbMax.Y + period/2}
	}
	worldW, worldH := bbMax.X-bbMin.X, bbMax.Y-bbMin.Y
	availW, availH := float64(size.X-2*marginPx), float64(size.Y-2*marginPx)
	fit := 1.0
	if worldW > 0 && worldH > 0 && availW > 0 && availH > 0 {
		fit = math.Min(availW/worldW, availH/worldH)
	}
	scale := fit * float64(v.zoom)
	worldCX, worldCY := (bbMin.X+bbMax.X)/2, (bbMin.Y+bbMax.Y)/2
	screenCX := float64(size.X)/2 + float64(v.pan.X)
	screenCY := float64(size.Y)/2 + float64(v.pan.Y)
	return func(p geometry.Point) f32.Point {
		return f32.Pt(float32(screenCX+(p.X-worldCX)*scale), float32(screenCY-(p.Y-worldCY)*scale))
	}
}

// drawMesh рисует заливку ячеек, их рёбра и поверх — контур с дырками.
// th: тема подписей метрик; size: размер холста в пикселях.
func (v *meshView) drawMesh(gtx layout.Context, th *material.Theme, size image.Point) {
	toScreen := v.worldToScreen(size, gtx.Dp(fitMargin))
	fillCol, edgeCol := colMeshFill, colMeshEdge
	if v.mesh.Polygonal {
		fillCol, edgeCol = colPolyFill, colPolyEdge
	}
	// не gtx.Dp(): он округляет до целых пикселей, а рёбрам нужны доли
	edgePx := float32(edgeWidth) * gtx.Metric.PxPerDp
	contourPx := float32(contourWidth) * gtx.Metric.PxPerDp

	// Призраки: 8 копий периодической сетки вокруг квадрата-периода, бледнее
	// самой сетки, — видно, как она стыкуется сама с собой. Рисуются первыми:
	// ячейки сетки у края выходят за квадрат и ложатся поверх своих копий.
	if period := v.mesh.Period; period > 0 {
		for shiftX := -1.0; shiftX <= 1; shiftX++ {
			for shiftY := -1.0; shiftY <= 1; shiftY++ {
				if shiftX == 0 && shiftY == 0 {
					continue
				}
				ghostToScreen := func(p geometry.Point) f32.Point {
					return toScreen(geometry.Point{X: p.X + shiftX*period, Y: p.Y + shiftY*period})
				}
				paint.FillShape(gtx.Ops, colGhostFill, clip.Outline{Path: v.cellsPath(gtx.Ops, ghostToScreen)}.Op())
				paint.FillShape(gtx.Ops, colGhostEdge, clip.Stroke{Path: v.edgesPath(gtx.Ops, ghostToScreen), Width: edgePx}.Op())
			}
		}
	}

	if v.cellMetrics.active() {
		v.drawCellMetricFills(gtx.Ops, toScreen)
	} else {
		paint.FillShape(gtx.Ops, fillCol, clip.Outline{Path: v.cellsPath(gtx.Ops, toScreen)}.Op())
	}
	paint.FillShape(gtx.Ops, edgeCol, clip.Stroke{Path: v.edgesPath(gtx.Ops, toScreen), Width: edgePx}.Op())
	paint.FillShape(gtx.Ops, colOuterContour, clip.Stroke{Path: contourPath(gtx.Ops, v.geom.Outer, toScreen), Width: contourPx}.Op())
	for _, hole := range v.geom.Holes {
		paint.FillShape(gtx.Ops, colHoleContour, clip.Stroke{Path: contourPath(gtx.Ops, hole, toScreen), Width: contourPx}.Op())
	}

	// частицы (крестики) поверх центроидов (точек): рисуются позже, поэтому
	// крестик всегда виден целиком, даже если точка под ним
	pxPerDp := gtx.Metric.PxPerDp
	if len(v.centroids) > 0 && !v.centroidsLayer.hidden {
		paint.FillShape(gtx.Ops, colCentroid, clip.Outline{Path: dotsPath(gtx.Ops, v.centroids, toScreen, float32(centroidRadius)*pxPerDp)}.Op())
	}
	if sites := v.cellSites(); sites != nil && !v.sitesLayer.hidden {
		centers := make([]f32.Point, len(sites))
		for i, site := range sites {
			centers[i] = toScreen(site)
		}
		crosses := crossesAt(gtx.Ops, centers, float32(siteCrossHalfSize)*pxPerDp)
		paint.FillShape(gtx.Ops, colSite, clip.Stroke{Path: crosses, Width: float32(siteCrossWidth) * pxPerDp}.Op())
	}
	if v.cellMetrics.active() {
		v.drawCellMetrics(gtx, th, size, toScreen)
	}
}

// crossesAt — крестики "×" одним путём для обводки: у каждого две диагонали
// отдельными отрезками (как рёбра — см. uniqueEdges, почему не замкнутым
// контуром). ops: куда пишется путь; centers: центры в пикселях экрана;
// halfSizePx: от центра до конца луча по каждой оси, в пикселях.
func crossesAt(ops *op.Ops, centers []f32.Point, halfSizePx float32) clip.PathSpec {
	var path clip.Path
	path.Begin(ops)
	for _, c := range centers {
		path.MoveTo(c.Add(f32.Pt(-halfSizePx, -halfSizePx)))
		path.LineTo(c.Add(f32.Pt(halfSizePx, halfSizePx)))
		path.MoveTo(c.Add(f32.Pt(-halfSizePx, halfSizePx)))
		path.LineTo(c.Add(f32.Pt(halfSizePx, -halfSizePx)))
	}
	return path.End()
}

// dotsPath — круглые точки в заданных местах одним путём, каждая — отдельным
// маленьким 12-угольником (на радиусе в пару пикселей от круга не отличить, а
// так все точки — одна заливка). ops: куда пишется путь; points: центры точек
// в мировых координатах; toScreen: перевод в пиксели; radiusPx: радиус в
// пикселях экрана.
func dotsPath(ops *op.Ops, points []geometry.Point, toScreen func(geometry.Point) f32.Point, radiusPx float32) clip.PathSpec {
	const sides = 12
	var offsets [sides]f32.Point
	for k := range offsets {
		angle := 2 * math.Pi * float64(k) / sides
		offsets[k] = f32.Pt(radiusPx*float32(math.Cos(angle)), radiusPx*float32(math.Sin(angle)))
	}
	var path clip.Path
	path.Begin(ops)
	for _, p := range points {
		center := toScreen(p)
		path.MoveTo(center.Add(offsets[0]))
		for _, offset := range offsets[1:] {
			path.LineTo(center.Add(offset))
		}
		path.Close()
	}
	return path.End()
}

// cellsPath собирает все ячейки сетки (треугольники или многоугольники) в один
// путь для заливки, каждую — отдельным замкнутым подпутём: одна заливка на
// всю сетку вместо тысяч отдельных. Ячейки не перекрываются, поэтому разный
// обход у соседей заливке не мешает. ops: куда пишется путь; toScreen:
// перевод точки мира в пиксели.
func (v *meshView) cellsPath(ops *op.Ops, toScreen func(geometry.Point) f32.Point) clip.PathSpec {
	var path clip.Path
	path.Begin(ops)
	if v.mesh.Polygonal {
		for _, cell := range v.mesh.Cells {
			v.addCellPath(&path, cell, toScreen)
		}
	} else {
		for _, tri := range v.mesh.Triangles {
			v.addCellPath(&path, tri[:], toScreen)
		}
	}
	return path.End()
}

// addCellPath добавляет замкнутый контур ячейки с индексами vertexIndices.
// toScreen переводит вершины сетки в координаты холста.
func (v *meshView) addCellPath(path *clip.Path, vertexIndices []int, toScreen func(geometry.Point) f32.Point) {
	if len(vertexIndices) < 3 {
		return
	}
	path.MoveTo(toScreen(v.mesh.Vertices[vertexIndices[0]]))
	for _, vertexIndex := range vertexIndices[1:] {
		path.LineTo(toScreen(v.mesh.Vertices[vertexIndex]))
	}
	path.Close()
}

// edgesPath — все уникальные рёбра сетки (v.edges) одним путём, каждое —
// отдельным открытым отрезком. ops: куда пишется путь; toScreen: перевод
// точки мира в пиксели. Почему не обводка cellsPath — см. uniqueEdges.
func (v *meshView) edgesPath(ops *op.Ops, toScreen func(geometry.Point) f32.Point) clip.PathSpec {
	var path clip.Path
	path.Begin(ops)
	for _, edge := range v.edges {
		path.MoveTo(toScreen(v.mesh.Vertices[edge[0]]))
		path.LineTo(toScreen(v.mesh.Vertices[edge[1]]))
	}
	return path.End()
}

// contourPath — замкнутый путь по ломаной контура (внешняя граница или дырка).
// ops: куда пишется путь; contour: точки в мировых координатах; toScreen:
// перевод в пиксели. Для пустой ломаной возвращает пустой путь.
func contourPath(ops *op.Ops, contour geometry.Polyline, toScreen func(geometry.Point) f32.Point) clip.PathSpec {
	var path clip.Path
	path.Begin(ops)
	if len(contour) >= 2 {
		path.MoveTo(toScreen(contour[0]))
		for _, p := range contour[1:] {
			path.LineTo(toScreen(p))
		}
		path.Close()
	}
	return path.End()
}
