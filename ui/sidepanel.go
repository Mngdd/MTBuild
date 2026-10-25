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
	"golang.org/x/exp/shiny/materialdesign/icons"
)

const (
	defaultSidebarWidth = unit.Dp(240)
	minSidebarWidth     = unit.Dp(180) // уже не сжимается — дальше только сворачивание
	// collapseFraction — панель сворачивается, когда её тянут уже чем
	// minSidebarWidth*collapseFraction; между этим порогом и минимумом она
	// просто стоит на минимуме (запас, чтобы не схлопнуть её случайно).
	collapseFraction = 0.5
	// maxSidebarFraction — шире этой доли окна панель не растягивается.
	maxSidebarFraction = 0.6

	collapsedStripWidth = unit.Dp(20)
	stripArrowSize      = unit.Dp(22) // сама галочка в иконке material занимает меньше половины её квадрата

	splitLineWidth = unit.Dp(1)
	// Ручка разделителя ловит мышь шире, чем видна линия, иначе в неё не
	// попасть. Почти вся она лежит справа от линии, над полем области
	// просмотра: слева у края панели стоят полосы прокрутки списков.
	splitGripLeft  = unit.Dp(1)
	splitGripRight = unit.Dp(6)

	// clickSlop — если между нажатием и отпусканием мышь ушла меньше, чем
	// на столько, это клик, а не перетаскивание.
	clickSlop = unit.Dp(4)
)

var iconExpandSidebar = mustIcon(icons.NavigationChevronRight)

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

// sidePanel делит окно на боковую панель слева и область просмотра справа.
// Ширину панели тянут мышью за разделитель; если утянуть её сильно уже
// минимума, она сворачивается в узкую полоску со стрелкой. Полоску можно
// кликнуть (панель вернётся) или вытянуть вправо.
type sidePanel struct {
	width     unit.Dp // ширина развёрнутой панели; при сворачивании не меняется
	collapsed bool
	grip      dragGrip // один на разделитель и полоску: схватили полоску — тащим дальше уже разделитель
	squeezed  bool     // тянут уже минимума, но ещё не до сворачивания — разделитель краснеет
}

func newSidePanel() *sidePanel {
	return &sidePanel{width: defaultSidebarWidth}
}

// Layout раскладывает панель и область просмотра на всё место gtx.
// sidebar получает точную ширину панели, content — остаток справа.
// Возвращает полный размер.
func (p *sidePanel) Layout(gtx layout.Context, sidebar, content layout.Widget) layout.Dimensions {
	size := gtx.Constraints.Max
	minPx := gtx.Dp(minSidebarWidth)
	maxPx := max(int(float32(size.X)*maxSidebarFraction), minPx)
	collapseAtPx := float32(minPx) * collapseFraction

	widthPx := min(max(gtx.Dp(p.width), minPx), maxPx)
	handlePos := float32(widthPx)
	if p.collapsed {
		handlePos = 0
	}
	target, dragged, clicked := p.grip.update(gtx, layout.Horizontal, handlePos)
	if !p.grip.dragging() {
		p.squeezed = false // отпустили — краснота уходит; пока тянут, держится до следующего сдвига
	}
	switch {
	case dragged && target < collapseAtPx:
		p.collapsed, p.squeezed = true, false
	case dragged:
		p.collapsed = false
		p.squeezed = target < float32(minPx)
		widthPx = min(max(int(target), minPx), maxPx)
		p.width = unit.Dp(float32(widthPx) / gtx.Metric.PxPerDp)
	case clicked && p.collapsed:
		p.collapsed = false
	}

	leftPx := widthPx
	if p.collapsed {
		leftPx = gtx.Dp(collapsedStripWidth)
	}

	// панель (или полоска) слева
	leftGtx := gtx
	leftGtx.Constraints = layout.Exact(image.Pt(leftPx, size.Y))
	if p.collapsed {
		p.layoutStrip(leftGtx)
	} else {
		sidebar(leftGtx)
	}

	// область просмотра справа
	contentGtx := gtx
	contentGtx.Constraints = layout.Exact(image.Pt(max(size.X-leftPx, 0), size.Y))
	offset := op.Offset(image.Pt(leftPx, 0)).Push(gtx.Ops)
	content(contentGtx)
	offset.Pop()

	// разделитель и ручка — поверх обеих, чтобы ловить мышь раньше них
	if p.collapsed {
		p.grip.layoutArea(gtx, image.Rect(0, 0, leftPx, size.Y), pointer.CursorPointer)
	} else {
		lineColor, lineWidth := colBorder, gtx.Dp(splitLineWidth)
		switch {
		case p.squeezed:
			lineColor, lineWidth = colDanger, 2*lineWidth
		case p.grip.dragging():
			lineColor, lineWidth = colAccent, 2*lineWidth
		}
		paint.FillShape(gtx.Ops, lineColor, clip.Rect{Min: image.Pt(leftPx-lineWidth, 0), Max: image.Pt(leftPx, size.Y)}.Op())
		gripRect := image.Rect(leftPx-gtx.Dp(splitGripLeft), 0, leftPx+gtx.Dp(splitGripRight), size.Y)
		p.grip.layoutArea(gtx, gripRect, pointer.CursorColResize)
	}
	return layout.Dimensions{Size: size}
}

// layoutStrip рисует свёрнутую панель: полоску на всю высоту с тёмной
// стрелкой вправо посередине — подсказкой, что панель можно вернуть.
func (p *sidePanel) layoutStrip(gtx layout.Context) {
	size := gtx.Constraints.Min
	paint.FillShape(gtx.Ops, colSidebarBG, clip.Rect{Max: size}.Op())
	border := gtx.Dp(splitLineWidth)
	paint.FillShape(gtx.Ops, colBorder, clip.Rect{Min: image.Pt(size.X-border, 0), Max: size}.Op())

	arrow := gtx.Dp(stripArrowSize)
	defer op.Offset(image.Pt((size.X-border-arrow)/2, (size.Y-arrow)/2)).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(arrow, arrow))
	iconExpandSidebar.Layout(gtx, colTextNormal)
}
