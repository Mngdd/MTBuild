package main

import (
	"image"
	"image/color"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// algoMeta is GUI-only presentation data. The mesher.Mesher contract (Name,
// Params, Generate) says nothing about a human label, description or output
// shape, so that lives here rather than in the algorithm package.
type algoMeta struct {
	Label       string
	Description string
	Output      string // "triangular" or "polygonal" — drives the detail-panel badge
}

// algoOrder controls display order AND visibility: an algorithm registered in
// mesher but absent here (e.g. a raw Delaunay debug algorithm) simply never
// appears in the GUI, without needing a "hidden" flag anywhere.
var algoOrder = []string{"sph-quadtree", "grid", "charge-voronoi"}

var algoMetas = map[string]algoMeta{
	"sph-quadtree": {
		Label:       "sph",
		Description: "sph метод и потом делоне",
		Output:      "triangular",
	},
	"grid": {
		Label:       "Grid straight",
		Description: "Регулярная решётка + Делоне. Для сравнения качества с основным методом.",
		Output:      "triangular",
	},
	"charge-voronoi": {
		Label: "Voronoi (charges)",
		Description: "Центры ячеек — одноимённые заряды, отталкивание F~1/rⁿ с затуханием, " +
			"оседают в равномерную упаковку. Ячейки — многоугольники Вороного вокруг них, не треугольники.",
		Output: "polygonal",
	},
}

const rowHeight = unit.Dp(36)
const accentWidth = unit.Dp(3)
const sidebarWidth = unit.Dp(240)

var (
	colSidebarBG    = color.NRGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}
	colRowTint      = color.NRGBA{R: 0xe5, G: 0xf0, B: 0xfa, A: 0xff}
	colRowHover     = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0xff}
	colAccent       = color.NRGBA{R: 0x00, G: 0x78, B: 0xd4, A: 0xff}
	colTextNormal   = color.NRGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}
	colTextSecond   = color.NRGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff}
	colBorder       = color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	colContentBG    = color.NRGBA{R: 0xf0, G: 0xf0, B: 0xf0, A: 0xff}
	colBadgeTriBG   = color.NRGBA{R: 0xe6, G: 0xf1, B: 0xfb, A: 0xff} // c-blue 50
	colBadgeTriText = color.NRGBA{R: 0x0c, G: 0x44, B: 0x7c, A: 0xff} // c-blue 800
	colBadgePolyBG  = color.NRGBA{R: 0xfa, G: 0xee, B: 0xda, A: 0xff} // c-amber 50
	colBadgePolyTxt = color.NRGBA{R: 0x63, G: 0x38, B: 0x06, A: 0xff} // c-amber 800
	colFieldBG      = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colDanger       = color.NRGBA{R: 0xd1, G: 0x34, B: 0x38, A: 0xff}
)

// sidebarState is the whole left column: which algorithm is selected, its
// parameter controls, the contour picker, and the per-row click state that
// must survive across frames (a widget.Clickable created fresh each frame
// would never see its own pointer events land).
type sidebarState struct {
	selected   string
	clickables map[string]*widget.Clickable
	params     *paramsState
	geom       *geomState
	list       widget.List
}

func newSidebarState(w *app.Window) *sidebarState {
	s := &sidebarState{
		selected:   algoOrder[0],
		clickables: make(map[string]*widget.Clickable, len(algoOrder)),
		params:     newParamsState(),
		geom:       newGeomState(w),
	}
	s.list.Axis = layout.Vertical
	for _, name := range algoOrder {
		s.clickables[name] = &widget.Clickable{}
	}
	return s
}

// Layout renders the fixed-width sidebar: a scrollable region holding the
// algorithm list and the detail panel (name, output badge, description,
// parameters), and the Start/Stop row pinned to the bottom below it. The
// scroll region exists because sph-quadtree alone has 7 sliders — on a
// short window that easily runs taller than the screen, and without
// scrolling it pushed Start/Stop off the bottom edge entirely.
func (s *sidebarState) Layout(gtx layout.Context, th *material.Theme, rs *runState) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(sidebarWidth)
	gtx.Constraints.Max.X = gtx.Dp(sidebarWidth)
	size := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
	paint.FillShape(gtx.Ops, colSidebarBG, clip.Rect{Max: size}.Op())

	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The contour picker itself is a window-top menu bar now
			// (geomState.MenuBar, called from main.go) — this is just its
			// load-failure feedback, contextually still part of the
			// sidebar. Collapses to nothing when there's nothing to say.
			if s.geom.err == nil {
				return layout.Dimensions{}
			}
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return s.geom.Notice(gtx, th)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, &s.list).LayoutWidgets(gtx,
				func(gtx layout.Context) layout.Dimensions {
					rows := make([]layout.FlexChild, 0, len(algoOrder))
					for _, name := range algoOrder {
						name := name
						rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return s.algoRow(gtx, th, name)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
				},
				func(gtx layout.Context) layout.Dimensions {
					return s.detailPanel(gtx, th)
				},
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				ap := s.params.byAlgo[s.selected]
				return rs.Layout(gtx, th, s.selected, s.geom.current, ap.values())
			})
		}),
	)
	return layout.Dimensions{Size: size}
}

func (s *sidebarState) algoRow(gtx layout.Context, th *material.Theme, name string) layout.Dimensions {
	click := s.clickables[name]
	if click.Clicked(gtx) {
		s.selected = name
	}
	selected := name == s.selected
	meta := algoMetas[name]

	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = gtx.Dp(rowHeight)
	gtx.Constraints.Max.Y = gtx.Dp(rowHeight)

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
				// Stack zeroes Min for Stacked children (see gioui.org/layout's
				// Stack.Layout), so without this the row-content Flex — all
				// Rigid, no Flexed filler — shrinks to its own content width
				// instead of the full row, and the background above ends up
				// only as wide as the label text.
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						w, h := gtx.Dp(accentWidth), gtx.Dp(rowHeight)
						if selected {
							paint.FillShape(gtx.Ops, colAccent, clip.Rect{Max: image.Pt(w, h)}.Op())
						}
						return layout.Dimensions{Size: image.Pt(w, h)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(9)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(th, unit.Sp(12), meta.Label)
							lbl.Color = colTextNormal
							if selected {
								lbl.Color = colAccent
								lbl.Font.Weight = font.Medium
							}
							return lbl.Layout(gtx)
						})
					}),
				)
			}),
		)
	})
}

// detailPanel shows the name, output-shape badge, description and tunable
// parameters of the selected algorithm.
func (s *sidebarState) detailPanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	meta := algoMetas[s.selected]
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: image.Pt(gtx.Constraints.Min.X, gtx.Dp(1))}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(th, unit.Sp(14), meta.Label)
								lbl.Font.Weight = font.Medium
								lbl.Color = colTextNormal
								return lbl.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return outputBadge(gtx, th, meta.Output)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(th, unit.Sp(12), meta.Description)
						lbl.Color = colTextSecond
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return s.paramsList(gtx, th)
					}),
				)
			})
		}),
	)
}

// paramsList renders the selected algorithm's ParamSpecs as slider+number
// rows, in Params() order.
func (s *sidebarState) paramsList(gtx layout.Context, th *material.Theme) layout.Dimensions {
	ap := s.params.byAlgo[s.selected]
	children := make([]layout.FlexChild, 0, 2*len(ap.order))
	for _, key := range ap.order {
		key := key
		if !ap.visible(key) {
			continue
		}
		if len(children) > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout))
		}
		indent := unit.Dp(0)
		if ap.ctrls[key].spec.DependsOn != "" {
			indent = unit.Dp(16) // dependent slider sits under its checkbox
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: indent}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ap.ctrls[key].Layout(gtx, th)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// outputBadge draws the small pill marking the final mesh shape the
// algorithm produces ("triangular" or "polygonal").
func outputBadge(gtx layout.Context, th *material.Theme, output string) layout.Dimensions {
	text, bg, fg := "треугольная", colBadgeTriBG, colBadgeTriText
	if output == "polygonal" {
		text, bg, fg = "многоугольная", colBadgePolyBG, colBadgePolyTxt
	}

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(th, unit.Sp(11), text)
			lbl.Color = fg
			return lbl.Layout(gtx)
		})
	call := macro.Stop()
	paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(4)).Op(gtx.Ops))
	call.Add(gtx.Ops)
	return dims
}
