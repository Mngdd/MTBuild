package main

import (
	"context"
	"fmt"
	"image"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
	"MTBuild/internal/viz"
)

// previewWidth is the final (post-antialiasing) pixel width of the mesh
// preview — viz.MeshImage internally rasterizes at a multiple of this and
// downsamples, so this number is the sharpness knob, not a cost knob by
// itself.
const previewWidth = 1100

type generateResult struct {
	img         *image.RGBA
	metrics     mesh.QualityMetrics
	polyMetrics mesh.PolyQualityMetrics
	polygonal   bool // which of metrics/polyMetrics is meaningful — mesher.Mesher.Generate always returns a zero QualityMetrics for a polygonal mesh (see internal/mesher/charge's doc comment), so this is computed here rather than inferred from metrics being non-zero
	err         error
}

// runState owns Start/Stop and the outcome of the background Generate call.
// Generate runs on its own goroutine; resultCh is how it hands the finished
// mesh back to the frame loop, which is the only goroutine allowed to touch
// the rest of these fields.
type runState struct {
	win *app.Window

	clickStart widget.Clickable
	clickStop  widget.Clickable

	running  bool
	cancel   context.CancelFunc
	resultCh chan generateResult

	hasImage    bool
	imgOp       paint.ImageOp
	metrics     mesh.QualityMetrics
	polyMetrics mesh.PolyQualityMetrics
	polygonal   bool
	err         error
}

func newRunState(w *app.Window) *runState {
	return &runState{win: w, resultCh: make(chan generateResult, 1)}
}

// poll drains a finished result, if any. Call it once per FrameEvent before
// laying out anything that reads running/hasImage/err.
func (r *runState) poll() {
	select {
	case res := <-r.resultCh:
		r.running = false
		r.cancel = nil
		r.err = res.err
		if res.err == nil {
			r.imgOp = paint.NewImageOp(res.img)
			r.hasImage = true
			r.metrics = res.metrics
			r.polyMetrics = res.polyMetrics
			r.polygonal = res.polygonal
		}
	default:
	}
}

func (r *runState) start(algoName string, geom geometry.Geometry, params mesher.Params) {
	if r.running {
		return
	}
	r.running = true
	r.err = nil
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	go func() {
		algo, err := mesher.New(algoName)
		if err != nil {
			r.resultCh <- generateResult{err: err}
			r.win.Invalidate()
			return
		}
		m, metrics, err := algo.Generate(ctx, geom, params, mesher.NopSink{})
		if err != nil {
			r.resultCh <- generateResult{err: err}
			r.win.Invalidate()
			return
		}
		// A polygonal mesher (Mesh.Cells, e.g. charge-voronoi) returns a zero
		// QualityMetrics — that type is triangle-only — so its real numbers
		// come from mesh.EvaluatePoly instead, and the image comes from the
		// Cells-aware rasterizer, not MeshImage.
		var img *image.RGBA
		var polyMetrics mesh.PolyQualityMetrics
		if m.Polygonal {
			img, err = viz.PolyMeshImage(geom, m, previewWidth)
			polyMetrics = mesh.EvaluatePoly(m)
		} else {
			img, err = viz.MeshImage(geom, m, previewWidth)
		}
		r.resultCh <- generateResult{img: img, metrics: metrics, polyMetrics: polyMetrics, polygonal: m.Polygonal, err: err}
		r.win.Invalidate()
	}()
}

func (r *runState) stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// Layout draws the Start/Stop row and handles their clicks. algoName/geom/
// params describe what a Start click right now would run.
func (r *runState) Layout(gtx layout.Context, th *material.Theme, algoName string, geom geometry.Geometry, params mesher.Params) layout.Dimensions {
	if r.clickStart.Clicked(gtx) {
		r.start(algoName, geom, params)
	}
	if r.clickStop.Clicked(gtx) {
		r.stop()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			btn := material.Button(th, &r.clickStart, "Start")
			btn.Background = colAccent
			if r.running {
				gtx = gtx.Disabled()
			}
			return btn.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			btn := material.Button(th, &r.clickStop, "Stop")
			btn.Background = colDanger
			if !r.running {
				gtx = gtx.Disabled()
			}
			return btn.Layout(gtx)
		}),
	)
}

// contentView fills the right-hand pane: the last rendered mesh, an error,
// a "running" note, or an idle placeholder.
func (r *runState) contentView(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Constraints.Min = gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colContentBG, clip.Rect{Max: gtx.Constraints.Min}.Op())

	switch {
	case r.err != nil:
		lbl := material.Body2(th, "Ошибка: "+r.err.Error())
		lbl.Color = colDanger
		return layout.Center.Layout(gtx, lbl.Layout)
	case r.hasImage:
		return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					var s string
					if r.polygonal {
						q := r.polyMetrics
						s = fmt.Sprintf("%d ячеек · avg_sides %.1f · iso_avg %.2f", q.Cells, q.AvgSides, q.IsoAvg)
					} else {
						q := r.metrics
						s = fmt.Sprintf("%d треугольников · G_avg %.2f · θ_min %.1f°", q.Triangles, q.GAvg, q.ThetaMinDeg)
					}
					lbl := material.Body2(th, s)
					lbl.Color = colTextSecond
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					img := widget.Image{Src: r.imgOp, Fit: widget.Contain, Position: layout.Center}
					return img.Layout(gtx)
				}),
			)
		})
	case r.running:
		lbl := material.Body2(th, "Считаю…")
		lbl.Color = colTextSecond
		return layout.Center.Layout(gtx, lbl.Layout)
	default:
		lbl := material.Body2(th, "Нажмите Start")
		lbl.Color = colTextSecond
		return layout.Center.Layout(gtx, lbl.Layout)
	}
}
