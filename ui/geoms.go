package main

import (
	"bytes"
	"image"
	"io"
	"os"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"MTBuild/internal/geometry"
)

// geomOrder controls which of geometry.Preset's built-ins the dropdown
// offers, and in what order. Like algoOrder, absence here means invisible
// in the GUI even if geometry.Preset would accept the name.
var geomOrder = []string{"ellipse", "circle", "zalesak", "wing"}

var geomLabels = map[string]string{
	"ellipse": "Ellipse",
	"circle":  "Circle",
	"zalesak": "Zalesak disk",
	"wing":    "Wing profile",
}

type loadResult struct {
	label string
	geom  geometry.Geometry
	err   error
}

// geomState is the contour picker: a dropdown bar showing the active shape,
// which expands into a "Load file..." action plus the preset list. Selection
// is independent of which algorithm is chosen — any algorithm can run on
// any contour.
type geomState struct {
	win      *app.Window
	explorer *explorer.Explorer

	label      string
	presetName string // one of geomOrder, or "" when current came from a loaded file
	current    geometry.Geometry
	err        error // last "Load file..." failure, shown until the next attempt

	open          bool
	barClick      widget.Clickable
	backdropClick widget.Clickable
	loadClick     widget.Clickable
	presetClicks  map[string]*widget.Clickable

	loadCh chan loadResult
}

func newGeomState(w *app.Window) *geomState {
	g := &geomState{
		win:          w,
		explorer:     explorer.NewExplorer(w),
		presetClicks: make(map[string]*widget.Clickable, len(geomOrder)),
		loadCh:       make(chan loadResult, 1),
	}
	for _, name := range geomOrder {
		g.presetClicks[name] = &widget.Clickable{}
	}
	g.selectPreset(geomOrder[0])
	return g
}

// ListenEvents must see every event this window's main loop receives — the
// explorer needs the platform view carried by some of them to open native
// dialogs. See gioui.org/x/explorer's package doc.
func (g *geomState) ListenEvents(e event.Event) {
	g.explorer.ListenEvents(e)
}

func (g *geomState) selectPreset(name string) {
	geo, err := geometry.Preset(name)
	if err != nil {
		// Only our own hardcoded names reach here; a failure means a bug in
		// this file, not bad user input.
		panic("ui: built-in preset " + name + ": " + err.Error())
	}
	g.current = geo
	g.label = geomLabels[name]
	g.presetName = name
	g.err = nil
	g.open = false
}

// startLoad opens the native file picker on its own goroutine — ChooseFile
// blocks until the dialog closes, which would freeze the window if called
// from the frame loop.
func (g *geomState) startLoad() {
	go func() {
		rc, err := g.explorer.ChooseFile(".dxf")
		if err != nil {
			if err != explorer.ErrUserDecline {
				g.loadCh <- loadResult{err: err}
				g.win.Invalidate()
			}
			return
		}
		defer rc.Close()

		label := "custom.dxf"
		if f, ok := rc.(*os.File); ok {
			label = filepathBase(f.Name())
		}

		data, err := io.ReadAll(rc)
		if err != nil {
			g.loadCh <- loadResult{err: err}
			g.win.Invalidate()
			return
		}
		geo, err := geometry.LoadDXF(bytes.NewReader(data))
		if err == nil {
			geo, err = geo.Normalized()
		}
		g.loadCh <- loadResult{label: label, geom: geo, err: err}
		g.win.Invalidate()
	}()
}

// poll drains a finished file load, if any. Call it once per FrameEvent.
func (g *geomState) poll() {
	select {
	case res := <-g.loadCh:
		if res.err != nil {
			g.err = res.err
			return
		}
		g.current = res.geom
		g.label = res.label
		g.presetName = ""
		g.err = nil
		g.open = false
	default:
	}
}

// menuBarHeight is the height of the window-top menu bar (MenuBar) and of
// each row inside the dropdown panel (dropdownRow) — one constant for both
// since they're the same kind of "flat clickable row" the user sees size
// consistently.
const menuBarHeight = unit.Dp(28)

// menuItemPad is the inset around the "Фигура: ..." label inside MenuBar —
// Overlay reuses it so the dropdown panel's left edge lines up with the
// label's left edge, the way a native menu's dropdown aligns under it.
const menuItemPad = unit.Dp(6)

// panelWidth is the dropdown panel's fixed width — independent of the
// sidebar now that the trigger lives in the window-top menu bar rather than
// inside the sidebar.
const panelWidth = unit.Dp(200)

// MenuBar draws the window-top menu row (Layout no longer does — the
// contour picker used to be a bar inside the sidebar, but that's not how
// menus look in any normal Windows app: a menu lives in its own row at the
// very top of the window, sized to its own label, not full width). Call it
// once, above the rest of the window content.
func (g *geomState) MenuBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Constraints.Min.Y = gtx.Dp(menuBarHeight)
	gtx.Constraints.Max.Y = gtx.Dp(menuBarHeight)
	size := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Max.Y)

	paint.FillShape(gtx.Ops, colSidebarBG, clip.Rect{Max: size}.Op())
	bw := gtx.Dp(unit.Dp(1))
	paint.FillShape(gtx.Ops, colBorder, clip.Rect{Min: image.Pt(0, size.Y-bw), Max: size}.Op())

	if g.barClick.Clicked(gtx) {
		g.open = !g.open
	}

	layout.Inset{Left: unit.Dp(4), Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return g.barClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			dims := layout.UniformInset(menuItemPad).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(12), "Фигура: "+g.label)
				lbl.Color = colTextNormal
				return lbl.Layout(gtx)
			})
			call := macro.Stop()
			if g.barClick.Hovered() || g.open {
				paint.FillShape(gtx.Ops, colRowHover, clip.Rect{Max: dims.Size}.Op())
			}
			call.Add(gtx.Ops)
			return dims
		})
	})

	return layout.Dimensions{Size: size}
}

// Notice shows the last "Load file..." failure, if any — collapses to
// nothing otherwise. Rendered in the sidebar (algos.go), separately from
// MenuBar, since it's feedback about the sidebar's contents, not part of
// the menu itself.
func (g *geomState) Notice(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if g.err == nil {
		return layout.Dimensions{}
	}
	lbl := material.Label(th, unit.Sp(11), "Не удалось загрузить: "+g.err.Error())
	lbl.Color = colDanger
	return lbl.Layout(gtx)
}

// Overlay draws the floating dropdown panel when open: a window-sized
// invisible backdrop that closes it on any outside click, then the panel
// itself, anchored just under the menu bar label. Call it last, from the
// window root, after the rest of the UI is laid out — paint order in Gio's
// op stream is z-order, so drawing this last is what makes it float above
// everything.
func (g *geomState) Overlay(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !g.open {
		return layout.Dimensions{}
	}
	if g.loadClick.Clicked(gtx) {
		g.startLoad()
	}
	for _, name := range geomOrder {
		if g.presetClicks[name].Clicked(gtx) {
			g.selectPreset(name)
		}
	}
	if g.backdropClick.Clicked(gtx) {
		g.open = false
	}

	g.backdropClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})

	anchor := image.Pt(gtx.Dp(unit.Dp(4)), gtx.Dp(menuBarHeight))
	width := gtx.Dp(panelWidth)
	defer op.Offset(anchor).Push(gtx.Ops).Pop()

	pgtx := gtx
	pgtx.Constraints.Min = image.Point{}
	pgtx.Constraints.Max.X = width
	pgtx.Constraints.Max.Y -= anchor.Y
	if pgtx.Constraints.Max.Y < 0 {
		pgtx.Constraints.Max.Y = 0
	}
	g.panel(pgtx, th)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// panel lays out the dropdown's content (Load file, divider, presets) and
// draws a bordered background sized to fit it exactly — the same
// record-measure-then-paint-behind trick as outputBadge/numberBox, since Gio
// has no "size to content" shortcut for a background.
func (g *geomState) panel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx, g.panelRows(th)...)
	call := macro.Stop()

	paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: dims.Size}.Op())
	b := gtx.Dp(unit.Dp(1))
	inner := image.Rect(b, b, dims.Size.X-b, dims.Size.Y-b)
	if inner.Dx() > 0 && inner.Dy() > 0 {
		paint.FillShape(gtx.Ops, colFieldBG, clip.Rect(inner).Op())
	}
	call.Add(gtx.Ops)
	return dims
}

func (g *geomState) panelRows(th *material.Theme) []layout.FlexChild {
	rows := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dropdownRow(gtx, th, &g.loadClick, "Load file…", false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return divider(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(10), Top: unit.Dp(6), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(10), "Готовые фигуры")
				lbl.Color = colTextSecond
				return lbl.Layout(gtx)
			})
		}),
	}
	for _, name := range geomOrder {
		name := name
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dropdownRow(gtx, th, g.presetClicks[name], geomLabels[name], name == g.presetName)
		}))
	}
	return rows
}

func dropdownRow(gtx layout.Context, th *material.Theme, click *widget.Clickable, label string, selected bool) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = gtx.Dp(menuBarHeight)
	gtx.Constraints.Max.Y = gtx.Dp(menuBarHeight)

	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				switch {
				case selected:
					paint.FillShape(gtx.Ops, colRowTint, clip.Rect{Max: gtx.Constraints.Min}.Op())
				case click.Hovered():
					paint.FillShape(gtx.Ops, colRowHover, clip.Rect{Max: gtx.Constraints.Min}.Op())
				}
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				// Same full-width fix as algoRow: Stack zeroes Min for
				// Stacked children, so reclaim it here or the highlight
				// only covers the label text.
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(th, unit.Sp(12), label)
						lbl.Color = colTextNormal
						if selected {
							lbl.Color = colAccent
							lbl.Font.Weight = font.Medium
						}
						return lbl.Layout(gtx)
					})
				})
			}),
		)
	})
}

func divider(gtx layout.Context) layout.Dimensions {
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(1)))
	paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: sz}.Op())
	return layout.Dimensions{Size: sz}
}

func filepathBase(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
