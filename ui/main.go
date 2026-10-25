// Command ui is MTBuild's GUI entry point, built on Gio (gioui.org). It
// owns the window and the frame loop; everything it draws lives in
// sidebarState (algos.go), geomState (geoms.go), paramsState (params.go)
// and runState (run.go).
package main

import (
	"os"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget/material"

	_ "MTBuild/internal/mesher/charge"  // register "charge-voronoi"
	_ "MTBuild/internal/mesher/grid"    // register "grid"
	_ "MTBuild/internal/mesher/sphquad" // register "sph-quadtree"
)

func main() {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("MTBuild"), app.Size(1000, 700))
		if err := run(w); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	sb := newSidebarState(w)
	rs := newRunState(w)
	sidebarDock := &dockPanel{edge: dockLeft, size: defaultSidebarWidth, maxFraction: maxSidebarFraction}

	var ops op.Ops
	for {
		evt := w.Event()
		sb.geom.ListenEvents(evt) // explorer needs every event to drive the native file dialog
		switch e := evt.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			rs.poll()
			sb.geom.poll()
			gtx := app.NewContext(&ops, e)
			layout.Stack{}.Layout(gtx,
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return sb.geom.MenuBar(gtx, th)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return sidebarDock.Layout(gtx, minSidebarWidth,
								func(gtx layout.Context) layout.Dimensions {
									return sb.Layout(gtx, th, rs)
								},
								func(gtx layout.Context) layout.Dimensions {
									return rs.contentView(gtx, th)
								},
							)
						}),
					)
				}),
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					return sb.geom.Overlay(gtx, th)
				}),
			)
			e.Frame(gtx.Ops)
		}
	}
}
