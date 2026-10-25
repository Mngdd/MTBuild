package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"MTBuild/data"
	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
)

// logoImage — логотип стартового экрана, декодирован один раз из вшитого PNG.
var logoImage = paint.NewImageOp(mustDecodePNG(data.LogoPNG))

// mustDecodePNG — картинка из байтов PNG. pngBytes: содержимое файла.
// Ошибка тут — битый файл, вшитый в сам бинарник, а не что-то от
// пользователя, поэтому паника.
func mustDecodePNG(pngBytes []byte) image.Image {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		panic("ui: bad embedded PNG: " + err.Error())
	}
	return img
}

type generateResult struct {
	mesh        mesh.Mesh
	geom        geometry.Geometry // контур, по которому строили — meshView рисует его поверх сетки
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

	view        meshView // последняя готовая сетка + камера просмотра
	metrics     mesh.QualityMetrics
	polyMetrics mesh.PolyQualityMetrics
	polygonal   bool
	err         error

	progress *progressSink // текущего запуска; nil — ещё не запускали
	title    string        // последний выставленный заголовок окна
}

// titleRefresh — не чаще чем раз в столько будим окно ради счётчика итераций
// в заголовке: алгоритмы отчитываются каждые 10 итераций, и на быстрых
// расчётах это сотни перерисовок в секунду ради одной цифры.
const titleRefresh = 100 * time.Millisecond

// progressSink запоминает текущую итерацию расчёта для заголовка окна.
// Snapshot зовётся из горутины Generate, поэтому только атомики + Invalidate;
// сам заголовок меняется в цикле событий (runState.poll).
type progressSink struct {
	win          *app.Window
	iter         atomic.Int64 // 1..totalIters, 0 — итераций ещё не было
	totalIters   atomic.Int64
	lastWakeNano atomic.Int64 // когда последний раз звали Invalidate (UnixNano)
}

func (s *progressSink) Progress(string, float64) {}

// Snapshot берёт из кадра только номер итерации и их общее число; кадры
// неитерационных стадий (TotalIters == 0) пропускает.
func (s *progressSink) Snapshot(f mesher.Frame) {
	if f.TotalIters <= 0 {
		return
	}
	s.iter.Store(int64(f.Iter + 1))
	s.totalIters.Store(int64(f.TotalIters))
	if now := time.Now().UnixNano(); now-s.lastWakeNano.Load() >= int64(titleRefresh) {
		s.lastWakeNano.Store(now)
		s.win.Invalidate()
	}
}

func newRunState(w *app.Window) *runState {
	return &runState{win: w, resultCh: make(chan generateResult, 1)}
}

// poll drains a finished result, if any. Call it once per FrameEvent before
// laying out anything that reads running/view/err.
func (r *runState) poll() {
	select {
	case res := <-r.resultCh:
		r.running = false
		r.cancel = nil
		r.err = res.err
		if res.err == nil {
			r.view.setMesh(res.mesh, res.geom)
			r.metrics = res.metrics
			r.polyMetrics = res.polyMetrics
			r.polygonal = res.polygonal
		}
	default:
	}

	// заголовок: "MTBuild (130/300)" пока идёт итерационная стадия, иначе просто имя
	title := "MTBuild"
	if r.running && r.progress != nil {
		if total := r.progress.totalIters.Load(); total > 0 {
			title = fmt.Sprintf("MTBuild (%d/%d)", r.progress.iter.Load(), total)
		}
	}
	if title != r.title {
		r.win.Option(app.Title(title))
		r.title = title
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
	progress := &progressSink{win: r.win}
	r.progress = progress

	go func() {
		algo, err := mesher.New(algoName)
		if err != nil {
			r.resultCh <- generateResult{err: err}
			r.win.Invalidate()
			return
		}
		m, metrics, err := algo.Generate(ctx, geom, params, progress)
		if err != nil {
			r.resultCh <- generateResult{err: err}
			r.win.Invalidate()
			return
		}
		// A polygonal mesher (Mesh.Cells, e.g. charge-voronoi) returns a zero
		// QualityMetrics — that type is triangle-only — so its real numbers
		// come from mesh.EvaluatePoly instead.
		var polyMetrics mesh.PolyQualityMetrics
		if m.Polygonal {
			polyMetrics = mesh.EvaluatePoly(m)
		}
		r.resultCh <- generateResult{mesh: m, geom: geom, metrics: metrics, polyMetrics: polyMetrics, polygonal: m.Polygonal}
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
	case r.view.hasMesh:
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
					return r.view.Layout(gtx, th)
				}),
			)
		})
	case r.running:
		lbl := material.Body2(th, "Считаю…")
		lbl.Color = colTextSecond
		return layout.Center.Layout(gtx, lbl.Layout)
	default:
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return idleView(gtx, th)
		})
	}
}

// idleView — стартовый экран до первого запуска: логотип и под ним "Нажмите
// Start", оба по центру по горизонтали. Логотип квадратный, вдвое шире
// надписи (но не выше, чем помещается в окно вместе с ней).
// Возвращает размер всего блока.
func idleView(gtx layout.Context, th *material.Theme) layout.Dimensions {
	const logoToTextWidth = 2
	gap := gtx.Dp(unit.Dp(12))

	// Надпись раскладываем заранее и записываем: ширина логотипа считается
	// от её ширины, а нарисовать её надо уже под логотипом.
	labelMacro := op.Record(gtx.Ops)
	lbl := material.Body2(th, "Нажмите Start")
	lbl.Color = colTextSecond
	labelDims := lbl.Layout(gtx)
	labelCall := labelMacro.Stop()

	logoSide := min(logoToTextWidth*labelDims.Size.X, gtx.Constraints.Max.Y-labelDims.Size.Y-gap)
	logoSide = max(logoSide, 0)

	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(logoSide, logoSide))
			return widget.Image{Src: logoImage, Fit: widget.Contain}.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			labelCall.Add(gtx.Ops)
			return labelDims
		}),
	)
}
