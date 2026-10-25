package main

import (
	"image"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

const (
	// collapseFraction — панель сворачивается, когда её тянут меньше чем
	// minSize*collapseFraction; между этим порогом и минимумом она просто
	// стоит на минимуме (запас, чтобы не схлопнуть её случайно).
	collapseFraction = 0.5

	collapsedStripSize = unit.Dp(20)
	stripArrowSize     = unit.Dp(22) // сама галочка в иконке material занимает меньше половины её квадрата

	splitLineWidth = unit.Dp(1)
	// Ручка разделителя ловит мышь шире, чем видна линия, иначе в неё не
	// попасть. Почти вся она лежит со стороны основной области, над её полем:
	// у края панели стоят полосы прокрутки, их ручка перекрывать не должна.
	splitGripInside  = unit.Dp(1)
	splitGripOutside = unit.Dp(6)

	// clickSlop — если между нажатием и отпусканием мышь ушла меньше, чем
	// на столько, это клик, а не перетаскивание.
	clickSlop = unit.Dp(4)
)

var (
	iconExpandRight = mustIcon(icons.NavigationChevronRight)
	iconExpandUp    = mustIcon(icons.NavigationExpandLess)
)

// dragGrip — ручка, которую тащат мышью: разделитель между панелями или
// свёрнутая полоска. Сам ничего не рисует, только разбирает события.
//
// События мыши приходят в координатах того места, где ручку
// зарегистрировали (layoutArea). Регистрировать её надо в неподвижной
// системе — у контейнера, а не у самой ручки: ручка едет вслед за мышью, и
// в её собственных координатах курсор почти не двигался бы.
type dragGrip struct {
	pressed    bool      // левая кнопка зажата на ручке
	moved      bool      // с нажатия мышь ушла дальше clickSlop — это перетаскивание
	pressPos   f32.Point // где нажали
	grabOffset float32   // на сколько курсор был от позиции ручки в момент нажатия, вдоль оси
}

// update разбирает события мыши ручки с прошлого кадра.
// axis — вдоль какой оси тянут; handlePos — где ручка сейчас вдоль этой оси,
// в пикселях, в тех же координатах, что и layoutArea.
// Возвращает target — куда пользователь ставит ручку (с поправкой на то, за
// какое место её схватили), dragged — ручку тащили и target свежий, clicked —
// нажали и отпустили, почти не сдвинув.
func (g *dragGrip) update(gtx layout.Context, axis layout.Axis, handlePos float32) (target float32, dragged, clicked bool) {
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: g,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel,
		})
		if !ok {
			return target, dragged, clicked
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		along := e.Position.X
		if axis == layout.Vertical {
			along = e.Position.Y
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonPrimary) {
				g.pressed, g.moved = true, false
				g.pressPos = e.Position
				g.grabOffset = along - handlePos
			}
		case pointer.Drag:
			if !g.pressed {
				continue
			}
			if shift := e.Position.Sub(g.pressPos); max(abs32(shift.X), abs32(shift.Y)) > float32(gtx.Dp(clickSlop)) {
				g.moved = true
			}
			if g.moved {
				target, dragged = along-g.grabOffset, true
			}
		case pointer.Release:
			if g.pressed && !g.moved {
				clicked = true
			}
			g.pressed = false
		case pointer.Cancel:
			g.pressed = false
		}
	}
}

// dragging — ручку прямо сейчас тащат (не просто нажали).
func (g *dragGrip) dragging() bool { return g.pressed && g.moved }

// layoutArea регистрирует прямоугольник rect, в котором ручка ловит мышь, и
// курсор над ним. rect — в координатах контейнера (см. doc dragGrip). Звать
// после того, что лежит под ручкой: кто зарегистрирован позже, тот и ловит.
func (g *dragGrip) layoutArea(gtx layout.Context, rect image.Rectangle, cursor pointer.Cursor) {
	defer clip.Rect(rect).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, g)
	cursor.Add(gtx.Ops)
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// dockEdge — к какому краю прижата панель.
type dockEdge int

const (
	dockLeft   dockEdge = iota // слева, тянут вбок (сайдбар)
	dockBottom                 // снизу, тянут вверх-вниз (метрики)
)

// dockPanel делит место на основную область и прижатую к краю панель с
// линией-разделителем между ними. Размер панели тянут мышью за разделитель;
// если утянуть её сильно меньше минимума, она сворачивается в узкую полоску
// со стрелкой. Полоску можно кликнуть (панель вернётся) или вытянуть.
type dockPanel struct {
	edge        dockEdge
	size        unit.Dp // размер развёрнутой панели поперёк края; при сворачивании не меняется
	maxFraction float32 // больше этой доли места панель не растягивается
	collapsed   bool
	grip        dragGrip // один на разделитель и полоску: схватили полоску — тащим дальше уже разделитель
	squeezed    bool     // тянут меньше минимума, но ещё не до сворачивания — разделитель краснеет
}

// ensureAtLeast растит развёрнутую панель до size, если она меньше. Свёрнутую
// не разворачивает.
func (p *dockPanel) ensureAtLeast(size unit.Dp) {
	p.size = max(p.size, size)
}

// Layout раскладывает основную область и панель на всё место gtx.
// minSize — меньше этого панель не сжимается (дальше — только сворачивание);
// panel получает точный размер панели, main — остальное.
// Возвращает полный размер.
func (p *dockPanel) Layout(gtx layout.Context, minSize unit.Dp, panel, main layout.Widget) layout.Dimensions {
	size := gtx.Constraints.Max
	axis, total := layout.Horizontal, size.X
	if p.edge == dockBottom {
		axis, total = layout.Vertical, size.Y
	}
	minPx := gtx.Dp(minSize)
	maxPx := max(int(float32(total)*p.maxFraction), minPx)
	collapseAtPx := float32(minPx) * collapseFraction

	// splitAt — позиция разделителя вдоль оси при размере панели panelPx
	// (слева — правый край панели, снизу — её верхний край), и обратно
	splitAt := func(panelPx int) int {
		if p.edge == dockBottom {
			return total - panelPx
		}
		return panelPx
	}
	panelSizeAt := func(split float32) float32 {
		if p.edge == dockBottom {
			return float32(total) - split
		}
		return split
	}

	panelPx := min(max(gtx.Dp(p.size), minPx), maxPx)
	handlePos := splitAt(panelPx)
	if p.collapsed {
		handlePos = splitAt(0)
	}
	target, dragged, clicked := p.grip.update(gtx, axis, float32(handlePos))
	if !p.grip.dragging() {
		p.squeezed = false // отпустили — краснота уходит; пока тянут, держится до следующего сдвига
	}
	switch wanted := panelSizeAt(target); {
	case dragged && wanted < collapseAtPx:
		p.collapsed, p.squeezed = true, false
	case dragged:
		p.collapsed = false
		p.squeezed = wanted < float32(minPx)
		panelPx = min(max(int(wanted), minPx), maxPx)
		p.size = unit.Dp(float32(panelPx) / gtx.Metric.PxPerDp)
	case clicked && p.collapsed:
		p.collapsed = false
	}

	if p.collapsed {
		panelPx = gtx.Dp(collapsedStripSize)
	}
	split := splitAt(panelPx)
	// along(a0, a1) — прямоугольник от a0 до a1 вдоль оси и во всю ширину поперёк
	along := func(a0, a1 int) image.Rectangle {
		if p.edge == dockBottom {
			return image.Rect(0, a0, size.X, a1)
		}
		return image.Rect(a0, 0, a1, size.Y)
	}
	panelRect, mainRect := along(0, split), along(split, total)
	if p.edge == dockBottom {
		panelRect, mainRect = along(split, total), along(0, split)
	}

	layoutIn := func(rect image.Rectangle, w layout.Widget) {
		childGtx := gtx
		childGtx.Constraints = layout.Exact(rect.Size())
		defer op.Offset(rect.Min).Push(gtx.Ops).Pop()
		w(childGtx)
	}
	layoutIn(mainRect, main)
	if p.collapsed {
		layoutIn(panelRect, p.layoutStrip)
	} else {
		layoutIn(panelRect, panel)
	}

	// разделитель и ручка — поверх обеих, чтобы ловить мышь раньше них
	if p.collapsed {
		p.grip.layoutArea(gtx, panelRect, pointer.CursorPointer)
		return layout.Dimensions{Size: size}
	}
	lineColor, linePx := colBorder, gtx.Dp(splitLineWidth)
	switch {
	case p.squeezed:
		lineColor, linePx = colDanger, 2*linePx
	case p.grip.dragging():
		lineColor, linePx = colAccent, 2*linePx
	}
	inside, outside := gtx.Dp(splitGripInside), gtx.Dp(splitGripOutside)
	cursor := pointer.CursorColResize
	lineRect, gripRect := along(split-linePx, split), along(split-inside, split+outside)
	if p.edge == dockBottom {
		cursor = pointer.CursorRowResize
		lineRect, gripRect = along(split, split+linePx), along(split-outside, split+inside)
	}
	paint.FillShape(gtx.Ops, lineColor, clip.Rect(lineRect).Op())
	p.grip.layoutArea(gtx, gripRect, cursor)
	return layout.Dimensions{Size: size}
}

// layoutStrip рисует свёрнутую панель: полоску во весь край с тёмной
// стрелкой посередине, смотрящей туда, куда панель выдвигается.
// Возвращает размер полоски.
func (p *dockPanel) layoutStrip(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Min
	paint.FillShape(gtx.Ops, colSidebarBG, clip.Rect{Max: size}.Op())
	border := gtx.Dp(splitLineWidth)
	arrow := gtx.Dp(stripArrowSize)
	var borderRect image.Rectangle
	var arrowAt image.Point
	var icon *widget.Icon
	if p.edge == dockBottom {
		borderRect = image.Rect(0, 0, size.X, border)
		arrowAt = image.Pt((size.X-arrow)/2, (size.Y+border-arrow)/2)
		icon = iconExpandUp
	} else {
		borderRect = image.Rect(size.X-border, 0, size.X, size.Y)
		arrowAt = image.Pt((size.X-border-arrow)/2, (size.Y-arrow)/2)
		icon = iconExpandRight
	}
	paint.FillShape(gtx.Ops, colBorder, clip.Rect(borderRect).Op())

	defer op.Offset(arrowAt).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(arrow, arrow))
	icon.Layout(gtx, colTextNormal)
	return layout.Dimensions{Size: size}
}
