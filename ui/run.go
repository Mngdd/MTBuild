package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"sync"
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
	cellMetrics []mesh.CellQuality
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
	polygonal   bool // форма сетки текущего (или последнего) запуска — известна уже при Start
	hasFinal    bool // metrics/polyMetrics — итог последнего запуска (он дошёл до конца без ошибки)
	err         error

	progress *progressSink  // текущего запуска; nil — ещё не запускали
	samples  []metricSample // копия истории метрик из progress, обновляется в poll
	panel    *metricsPanel  // нижняя панель метрик под областью просмотра
	title    string         // последний выставленный заголовок окна
}

// titleRefresh — не чаще чем раз в столько будим окно ради счётчика итераций
// в заголовке: алгоритмы отчитываются каждые 10 итераций, и на быстрых
// расчётах это сотни перерисовок в секунду ради одной цифры.
const titleRefresh = 100 * time.Millisecond

// progressSink запоминает текущую итерацию расчёта для заголовка окна и
// копит историю метрик для панели метрик. Snapshot зовётся из горутины
// Generate, поэтому атомики и мьютекс + Invalidate; читает всё это цикл
// событий (runState.poll).
type progressSink struct {
	win          *app.Window
	liveMesh     bool              // строить ли сетку в каждом кадре ради её метрик; задан до старта, дальше только читается
	geom         geometry.Geometry // контур запуска — для метрик сетки (периметр по границе); задан до старта, дальше только читается
	iter         atomic.Int64      // 1..totalIters, 0 — итераций ещё не было
	totalIters   atomic.Int64
	lastWakeNano atomic.Int64 // когда последний раз звали Invalidate (UnixNano)
	seed         atomic.Int64 // случайный сид запуска из кадра (Frame.Seed); 0 — не случайный

	historyMutex sync.Mutex
	history      []metricSample // по кадру на каждые 10 итераций
}

func (s *progressSink) Progress(string, float64) {}

// Snapshot берёт из кадра номер итерации, их общее число и метрики частиц,
// а с живыми метриками сетки ещё и строит по кадру сетку и меряет её —
// прямо тут, в горутине расчёта, пока частицы стоят (этим живые метрики и
// замедляют расчёт). Кадры неитерационных стадий (TotalIters == 0) пропускает.
func (s *progressSink) Snapshot(f mesher.Frame) {
	if f.Seed != 0 { // сид приходит в кадре init, а он неитерационный — ловим до проверки ниже
		s.seed.Store(f.Seed)
	}
	if f.TotalIters <= 0 {
		return
	}
	sample := metricSample{iter: f.Iter, particle: f.Particle}
	if s.liveMesh && f.BuildMesh != nil {
		if m, err := f.BuildMesh(); err == nil { // не вышло — у этого кадра просто не будет метрик сетки
			sample.hasMesh = true
			if m.Polygonal {
				sample.poly = mesh.EvaluatePoly(m, s.geom)
			} else {
				sample.tri = mesh.Evaluate(m)
			}
		}
	}
	s.historyMutex.Lock()
	s.history = append(s.history, sample)
	s.historyMutex.Unlock()

	s.iter.Store(int64(f.Iter + 1))
	s.totalIters.Store(int64(f.TotalIters))
	if now := time.Now().UnixNano(); now-s.lastWakeNano.Load() >= int64(titleRefresh) {
		s.lastWakeNano.Store(now)
		s.win.Invalidate()
	}
}

// historyCopy — копия накопленной истории метрик (Snapshot дописывает её из
// другой горутины, так что наружу — только копия).
func (s *progressSink) historyCopy() []metricSample {
	s.historyMutex.Lock()
	defer s.historyMutex.Unlock()
	return append([]metricSample(nil), s.history...)
}

func newRunState(w *app.Window) *runState {
	return &runState{win: w, resultCh: make(chan generateResult, 1), panel: newMetricsPanel()}
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
			r.view.setMesh(res.mesh, res.geom, res.cellMetrics)
			r.metrics = res.metrics
			r.polyMetrics = res.polyMetrics
			r.polygonal = res.polygonal
			r.hasFinal = true
		}
	default:
	}
	if r.progress != nil {
		r.samples = r.progress.historyCopy()
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
	r.hasFinal = false
	r.polygonal = algoMetas[algoName].Output == "polygonal"
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	progress := &progressSink{win: r.win, liveMesh: r.panel.liveMesh, geom: geom}
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
		var cellMetrics []mesh.CellQuality
		if m.Polygonal {
			polyMetrics, cellMetrics = mesh.EvaluatePolyWithCells(m, geom)
		}
		r.resultCh <- generateResult{
			mesh: m, geom: geom, metrics: metrics, polyMetrics: polyMetrics,
			cellMetrics: cellMetrics, polygonal: m.Polygonal,
		}
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

// contentView fills the right-hand pane: the mesh view area (the last
// rendered mesh, an error, a "running" note, or an idle placeholder) with the
// metrics panel docked under it.
func (r *runState) contentView(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return r.panel.Layout(gtx, th, r.metricsView(), func(gtx layout.Context) layout.Dimensions {
		return r.viewArea(gtx, th)
	})
}

// metricsView собирает для панели метрик то, что ей показывать сейчас.
func (r *runState) metricsView() metricsView {
	view := metricsView{running: r.running, polygonal: r.polygonal, samples: r.samples}
	if r.progress != nil {
		view.totalIters = int(r.progress.totalIters.Load())
		view.seed = r.progress.seed.Load()
	}
	if r.hasFinal {
		view.final = &metricSample{hasMesh: true, tri: r.metrics, poly: r.polyMetrics}
	}
	return view
}

// viewArea — область просмотра над панелью метрик на всё место gtx.
func (r *runState) viewArea(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Constraints.Min = gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colContentBG, clip.Rect{Max: gtx.Constraints.Min}.Op())

	switch {
	case r.err != nil:
		lbl := material.Body2(th, "Ошибка: "+r.err.Error())
		lbl.Color = colDanger
		return layout.Center.Layout(gtx, lbl.Layout)
	case r.view.hasMesh:
		return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return r.view.Layout(gtx, th)
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
