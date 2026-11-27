package main

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"

	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
)

const (
	metricsSideInset      = unit.Dp(16) // поле тела панели слева и справа
	defaultMetricsHeight  = unit.Dp(140)
	detailedMetricsHeight = unit.Dp(340) // при включении подробного режима панель дорастает хотя бы до этого
	maxMetricsFraction    = 0.75
	// metricsPeek — сколько тела панели видно над минимумом: шапка с кнопками
	// плюс кусок первой строки метрик.
	metricsPeek = unit.Dp(28)

	compactCellMinWidth = unit.Dp(150)
	compactCellHeight   = unit.Dp(32)
	chartCardMinWidth   = unit.Dp(200)
	chartHeight         = unit.Dp(56)
	histogramHeight     = unit.Dp(80)
	infoIconSize        = unit.Dp(14)
	metricsGap          = unit.Dp(10)
)

var (
	iconWarning     = mustIcon(icons.AlertWarning)
	iconInfo        = mustIcon(icons.ActionInfo)
	colTooltipBG    = color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xf0}
	colTooltipText  = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colDisabledFill = color.NRGBA{R: 0xb8, G: 0xb8, B: 0xb8, A: 0xff}
	colTextOnAccent = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// metricSample — метрики одного кадра расчёта (алгоритмы шлют кадр раз в 10
// итераций). Метрики частиц есть всегда, метрики сетки — только если на этом
// запуске были включены живые метрики сетки.
type metricSample struct {
	iter     int
	particle mesher.ParticleMetrics
	hasMesh  bool
	tri      mesh.QualityMetrics     // при hasMesh и треугольной сетке
	poly     mesh.PolyQualityMetrics // при hasMesh и многоугольной
}

// metricDef — одна метрика панели: как её назвать, откуда взять и как
// показать.
type metricDef struct {
	label       string
	description string // что это за метрика — подсказка у значка (i) рядом с названием
	fromMesh    bool   // считается по сетке; иначе — по частицам
	noChart     bool   // в подробном режиме без графика (число ячеек — не про качество)
	value       func(s metricSample) float64
	format      string // fmt-шаблон значения
}

var particleMetricDefs = []metricDef{
	{label: "Сдвиг частиц за шаг", description: "Средний сдвиг подвижной частицы за итерацию, в долях её целевого размера h. Падает к нулю, когда частицы успокоились; если держится на одном уровне — они всё ещё дёргаются.", value: func(s metricSample) float64 { return s.particle.MeanStep }, format: "%.4f h"},
	{label: "Расстояние до соседа", description: "Разброс расстояний до ближайшего соседа: std/среднее, каждое расстояние поделено на целевой размер h частицы. 0 — все стоят ровно на своём шаге, чем больше — тем неравномернее.", value: func(s metricSample) float64 { return s.particle.NeighborSpread }, format: "%.3f"},
}

var triMetricDefs = append([]metricDef{
	{label: "Треугольники", description: "Сколько треугольников в сетке.", fromMesh: true, noChart: true, value: func(s metricSample) float64 { return float64(s.tri.Triangles) }, format: "%.0f"},
	{label: "G avg", description: "Средняя форма треугольника: G = 2√3·S / (p·a), S — площадь, p — полупериметр, a — длиннейшая сторона. 1 — равносторонний, к 0 — вырожденный.", fromMesh: true, value: func(s metricSample) float64 { return s.tri.GAvg }, format: "%.3f"},
	{label: "G min", description: "Форма G самого плохого треугольника сетки (1 — равносторонний, к 0 — вырожденный).", fromMesh: true, value: func(s metricSample) float64 { return s.tri.GMin }, format: "%.3f"},
	{label: "θ min", description: "Самый маленький угол во всей сетке, в градусах. У равностороннего треугольника — 60°.", fromMesh: true, value: func(s metricSample) float64 { return s.tri.ThetaMinDeg }, format: "%.1f°"},
	{label: "θ avg", description: "Средний по треугольникам наименьший угол треугольника, в градусах. У равносторонних — 60°.", fromMesh: true, value: func(s metricSample) float64 { return s.tri.ThetaAvgDeg }, format: "%.1f°"},
	{label: "Плохих (θ<30°)", description: "Доля треугольников, у которых наименьший угол меньше 30°.", fromMesh: true, value: func(s metricSample) float64 { return 100 * s.tri.BadFraction }, format: "%.1f%%"},
}, particleMetricDefs...)

var polyMetricDefs = append([]metricDef{
	{label: "Ячейки", description: "Сколько ячеек (многоугольников) в сетке.", fromMesh: true, noChart: true, value: func(s metricSample) float64 { return float64(s.poly.Cells) }, format: "%.0f"},
	{label: "Сторон avg", description: "Среднее число сторон у ячейки. У равномерной упаковки — около 6, как у сот.", fromMesh: true, value: func(s metricSample) float64 { return s.poly.AvgSides }, format: "%.2f"},
	{label: "iso avg", description: "Средняя «круглость» ячейки: 4π·S / P², S — площадь, P — периметр. 1 — круг, шестиугольник ≈ 0.91, квадрат ≈ 0.79, треугольник ≈ 0.60.", fromMesh: true, value: func(s metricSample) float64 { return s.poly.IsoAvg }, format: "%.3f"},
	{label: "iso min", description: "«Круглость» 4π·S / P² самой вытянутой ячейки (1 — круг, правильный шестиугольник ≈ 0.91).", fromMesh: true, value: func(s metricSample) float64 { return s.poly.IsoMin }, format: "%.3f"},
	{label: "Разброс площади", description: "Насколько различаются площади ячеек: std/среднее. 0 — все одной площади.", fromMesh: true, value: func(s metricSample) float64 { return s.poly.AreaCV }, format: "%.3f"},
	{label: "Периметр ph", description: "Средний нормированный периметр rᵢ = pᵢ / p_hex: сумма длин рёбер ячейки к периметру правильного шестиугольника целевой площади A/N. 1 — сетка из одинаковых правильных шестиугольников, больше — хуже.", fromMesh: true, value: func(s metricSample) float64 { return s.poly.PerimeterMean }, format: "%.4f"},
	{label: "Периметр p2h", description: "Среднеквадратичный нормированный периметр √(Σrᵢ²/N). При том же среднем (ph) больше у сетки с бо́льшим разбросом периметров.", fromMesh: true, value: func(s metricSample) float64 { return s.poly.PerimeterRMS }, format: "%.4f"},
	{label: "Асимметрия S avg", description: "Средняя равноугольная асимметрия: S = max[(α_max − α₀)/(180° − α₀), (α₀ − α_min)/α₀], α₀ = (k−2)·180°/k — угол правильного k-угольника. 0 — все углы ячейки равны. У края граничный участок ячейки заменяется хордой.", fromMesh: true, value: func(s metricSample) float64 { return s.poly.SkewnessAvg }, format: "%.4f"},
	{label: "Асимметрия S max", description: "Равноугольная асимметрия S самой перекошенной ячейки (0 — все углы равны).", fromMesh: true, value: func(s metricSample) float64 { return s.poly.SkewnessMax }, format: "%.3f"},
}, particleMetricDefs...)

// metricsView — то, что панель показывает в этом кадре; собирается из
// runState (см. runState.metricsView), чтобы панель не лезла в его поля.
type metricsView struct {
	running       bool
	polygonal     bool
	totalIters    int            // итераций у запуска; 0 — неитерационный алгоритм или ещё не знаем
	samples       []metricSample // история по кадрам
	final         *metricSample  // итог по готовой сетке; nil — расчёт идёт, упал или ещё не запускали
	seed          int64          // случайный сид запуска; 0 — сид задан параметром или его нет
	distributions cellMetricDistributions
}

// series — точки графика одной метрики: x — итерация, y — значение.
type series []f32.Point

// metricSeries собирает точки графика d. Живые метрики сетки — по кадрам и
// итоговая точка в конце; итог без живых метрик — ровная линия от нуля до
// конца; метрики частиц — по кадрам.
func (v metricsView) metricSeries(d metricDef) series {
	var points series
	for _, s := range v.samples {
		if !d.fromMesh || s.hasMesh {
			points = append(points, f32.Pt(float32(s.iter), float32(d.value(s))))
		}
	}
	if d.fromMesh && v.final != nil {
		end := float32(max(v.lastIter(), 1)) // без итераций (Grid) ось 0..1 — линия всё равно во всю ширину
		if len(points) == 0 {
			points = append(points, f32.Pt(0, float32(d.value(*v.final))))
		}
		points = append(points, f32.Pt(end, float32(d.value(*v.final))))
	}
	return points
}

// current — последнее значение d или ok=false, если его ещё нет.
func (v metricsView) current(d metricDef) (value float64, ok bool) {
	if d.fromMesh && v.final != nil {
		return d.value(*v.final), true
	}
	for i := len(v.samples) - 1; i >= 0; i-- {
		if s := v.samples[i]; !d.fromMesh || s.hasMesh {
			return d.value(s), true
		}
	}
	return 0, false
}

// lastIter — правый край оси итераций.
func (v metricsView) lastIter() int {
	if len(v.samples) > 0 {
		return v.samples[len(v.samples)-1].iter
	}
	return v.totalIters
}

// defs — метрики для текущей формы сетки. Метрики частиц убираются, если
// расчёт кончился, а кадров с ними не было (у Grid итераций нет).
func (v metricsView) defs() []metricDef {
	all := triMetricDefs
	if v.polygonal {
		all = polyMetricDefs
	}
	if v.running || len(v.samples) > 0 {
		return all
	}
	return all[:len(all)-len(particleMetricDefs)]
}

// metricsPanel — нижняя панель под областью просмотра: шапка с кнопками и
// метрики в коротком (одна сетка чисел) или подробном (гистограмма + графики
// по итерациям) режиме. Сама держит и свою выдвижную панель (dock).
type metricsPanel struct {
	dock          dockPanel
	detailed      bool
	liveMesh      bool // строить сетку каждые 10 итераций; читается при нажатии Start
	detailedClick widget.Clickable
	liveClick     widget.Clickable
	list          hoverScrollList
	headerHeight  int                       // высота шапки на прошлом кадре, px — из неё минимум панели
	infoHovers    map[string]*gesture.Hover // наведение на значок (i) у названия, по названию
}

func newMetricsPanel() *metricsPanel {
	p := &metricsPanel{
		dock:       dockPanel{edge: dockBottom, size: defaultMetricsHeight, maxFraction: maxMetricsFraction},
		infoHovers: make(map[string]*gesture.Hover),
	}
	p.list.Axis = layout.Vertical
	return p
}

// Layout раскладывает область просмотра main и панель метрик под ней на всё
// место gtx. view — что показывать. Возвращает полный размер.
func (p *metricsPanel) Layout(gtx layout.Context, th *material.Theme, view metricsView, main layout.Widget) layout.Dimensions {
	// клики — до раскладки dock, чтобы подросшая под подробный режим панель
	// раскрылась в этом же кадре
	if p.detailedClick.Clicked(gtx) {
		p.detailed = !p.detailed
		if p.detailed {
			p.dock.ensureAtLeast(detailedMetricsHeight)
		}
	}
	if p.liveClick.Clicked(gtx) && !view.running {
		p.liveMesh = !p.liveMesh
	}

	minHeight := unit.Dp(float32(p.headerHeight)/gtx.Metric.PxPerDp) + metricsPeek
	return p.dock.Layout(gtx, minHeight, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Min
		paint.FillShape(gtx.Ops, colSidebarBG, clip.Rect{Max: size}.Op())
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				dims := p.header(gtx, th, view)
				p.headerHeight = dims.Size.Y
				return dims
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return p.list.Layout(gtx, th, 1, func(gtx layout.Context, _ int) layout.Dimensions {
					return layout.Inset{Left: metricsSideInset, Right: metricsSideInset, Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return p.body(gtx, th, view)
					})
				})
			}),
		)
	}, main)
}

// header — "Метрики", серый разделитель и две кнопки-переключателя; если не
// влезают в ширину, кнопки переносятся. Возвращает размер шапки.
func (p *metricsPanel) header(gtx layout.Context, th *material.Theme, view metricsView) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(10), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return flowRow(gtx, unit.Dp(12), unit.Dp(6),
			func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(14), "Метрики")
				lbl.Font.Weight = font.Medium
				lbl.Color = colTextNormal
				return lbl.Layout(gtx)
			},
			func(gtx layout.Context) layout.Dimensions {
				size := image.Pt(gtx.Dp(unit.Dp(1)), gtx.Dp(unit.Dp(18)))
				paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: size}.Op())
				return layout.Dimensions{Size: size}
			},
			func(gtx layout.Context) layout.Dimensions {
				return toggleButton(gtx, th, &p.detailedClick, "Подробный режим", nil, p.detailed)
			},
			func(gtx layout.Context) layout.Dimensions {
				if view.running {
					gtx = gtx.Disabled() // переключить посреди расчёта нельзя: читается при Start
				}
				dims := toggleButton(gtx, th, &p.liveClick, "Живые метрики сетки", iconWarning, p.liveMesh)
				if p.liveClick.Hovered() && gtx.Enabled() {
					tooltipBelow(gtx, th, dims.Size, noTooltipLimit, "Строит сетку каждые 10 итераций — расчёт медленнее")
				}
				return dims
			},
			func(gtx layout.Context) layout.Dimensions {
				if view.seed == 0 {
					return layout.Dimensions{}
				}
				lbl := material.Label(th, unit.Sp(12), fmt.Sprintf("Случайный сид: %d", view.seed))
				lbl.Color = colTextSecond
				return lbl.Layout(gtx)
			},
		)
	})
}

// body — сами метрики под шапкой. Возвращает их размер.
func (p *metricsPanel) body(gtx layout.Context, th *material.Theme, view metricsView) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	if !view.running && view.final == nil && len(view.samples) == 0 {
		lbl := material.Label(th, unit.Sp(12), "Метрики появятся после запуска")
		lbl.Color = colTextSecond
		return lbl.Layout(gtx)
	}
	if !p.detailed {
		return p.compactGrid(gtx, th, view)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !view.polygonal {
				return layout.Dimensions{}
			}
			return layout.Inset{Bottom: metricsGap}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return p.sidesHistogramCard(gtx, th, view)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !view.polygonal || view.final == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Bottom: metricsGap}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return p.distributionCards(gtx, th, view.distributions)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.chartGrid(gtx, th, view)
		}),
	)
}

// gridColumns — сколько колонок шириной не меньше minWidth (с промежутком
// gap между ними) влезает в width, но не больше count ячеек: иначе на
// широком окне справа оставались бы пустые колонки, а ячейки не
// растягивались. Минимум одна.
func gridColumns(width, minWidth, gap, count int) int {
	return max(1, min((width+gap)/(minWidth+gap), count))
}

// layoutGrid раскладывает count ячеек равной ширины по колонкам на всю
// ширину gtx, ряд за рядом. gap — промежуток между ячейками; cell(gtx, i,
// roomRight) рисует i-ю ячейку в точную ширину и свою высоту, roomRight —
// сколько пикселей сетки ещё правее этой ячейки (подсказкам — чтобы не
// вылезать за край). Возвращает размер сетки.
func layoutGrid(gtx layout.Context, count int, minWidth, gap unit.Dp, cell func(gtx layout.Context, i, roomRight int) layout.Dimensions) layout.Dimensions {
	width, gapPx := gtx.Constraints.Max.X, gtx.Dp(gap)
	columns := gridColumns(width, gtx.Dp(minWidth), gapPx, count)
	cellWidth := (width - gapPx*(columns-1)) / columns
	y := 0
	for rowStart := 0; rowStart < count; rowStart += columns {
		rowHeight := 0
		for column := 0; column < columns && rowStart+column < count; column++ {
			cellGtx := gtx
			cellGtx.Constraints = layout.Constraints{Min: image.Pt(cellWidth, 0), Max: image.Pt(cellWidth, gtx.Constraints.Max.Y)}
			offset := op.Offset(image.Pt(column*(cellWidth+gapPx), y)).Push(gtx.Ops)
			cellLeft := column * (cellWidth + gapPx)
			dims := cell(cellGtx, rowStart+column, width-cellLeft-cellWidth)
			offset.Pop()
			rowHeight = max(rowHeight, dims.Size.Y)
		}
		y += rowHeight
		if rowStart+columns < count {
			y += gapPx
		}
	}
	return layout.Dimensions{Size: image.Pt(width, y)}
}

// compactGrid — короткий режим: блоки одинаковой ширины "название ... число",
// между колонками тонкие разделители. Возвращает размер.
func (p *metricsPanel) compactGrid(gtx layout.Context, th *material.Theme, view metricsView) layout.Dimensions {
	defs := view.defs()
	columns := gridColumns(gtx.Constraints.Max.X, gtx.Dp(compactCellMinWidth), 0, len(defs))
	return layoutGrid(gtx, len(defs), compactCellMinWidth, 0, func(gtx layout.Context, i, roomRight int) layout.Dimensions {
		size := image.Pt(gtx.Constraints.Min.X, gtx.Dp(compactCellHeight))
		if i%columns != 0 { // разделитель — между колонками, не у левого края
			paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: image.Pt(gtx.Dp(unit.Dp(1)), size.Y)}.Op())
		}
		gtx.Constraints = layout.Exact(size)
		return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					// правее названия — остаток ячейки за полем 12dp, соседние ячейки и поле тела
					maxRight := size.X - gtx.Dp(unit.Dp(12)) + roomRight + gtx.Dp(metricsSideInset)
					return p.metricTitle(gtx, th, defs[i].label, defs[i].description, maxRight)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return metricValue(gtx, th, view, defs[i])
				}),
			)
		})
	})
}

// chartGrid — подробный режим: карточки равной ширины, в каждой название,
// текущее значение и ломаная по итерациям. Возвращает размер.
func (p *metricsPanel) chartGrid(gtx layout.Context, th *material.Theme, view metricsView) layout.Dimensions {
	var defs []metricDef
	for _, d := range view.defs() {
		if !d.noChart {
			defs = append(defs, d)
		}
	}
	return layoutGrid(gtx, len(defs), chartCardMinWidth, metricsGap, func(gtx layout.Context, i, roomRight int) layout.Dimensions {
		// правее названия — остаток карточки за полем 10dp, соседние карточки и поле тела
		maxRight := gtx.Constraints.Max.X - gtx.Dp(unit.Dp(10)) + roomRight + gtx.Dp(metricsSideInset)
		d := defs[i]
		return card(gtx, func(gtx layout.Context) layout.Dimensions {
			points := view.metricSeries(d)
			lastIter := max(view.totalIters, view.lastIter())
			left, caption, right := "0", "итерации", fmt.Sprint(lastIter)
			switch {
			case len(points) == 0 && d.fromMesh:
				caption = "посчитается в конце"
			case lastIter == 0 && !view.running:
				left, caption, right = "", "без итераций", ""
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return p.metricTitle(gtx, th, d.label, d.description, maxRight)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return metricValue(gtx, th, view, d)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return lineChart(gtx, points, float32(max(lastIter, 1)))
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return axisCaption(gtx, th, left, caption, right)
				}),
			)
		})
	})
}

// sidesHistogramCard — гистограмма числа сторон у ячеек (только для
// многоугольной сетки): итог, а пока считается — последний живой кадр.
// Возвращает размер карточки.
func (p *metricsPanel) sidesHistogramCard(gtx layout.Context, th *material.Theme, view metricsView) layout.Dimensions {
	var source *mesh.PolyQualityMetrics
	if view.final != nil {
		source = &view.final.poly
	} else {
		for i := len(view.samples) - 1; i >= 0; i-- {
			if view.samples[i].hasMesh {
				source = &view.samples[i].poly
				break
			}
		}
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		summary := "посчитается в конце"
		if source != nil {
			summary = fmt.Sprintf("avg %.2f", source.AvgSides)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return p.metricTitle(gtx, th, "Стороны ячеек", "Сколько ячеек с 3, 4, … 9 и 10+ сторонами. У равномерной упаковки больше всего шестиугольников.", noTooltipLimit)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(th, unit.Sp(11), summary)
						lbl.Color = colTextSecond
						return lbl.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				var counts []int
				if source != nil {
					counts = source.SidesHistogram[:]
				}
				return histogram(gtx, th, counts, []string{"3", "4", "5", "6", "7", "8", "9", "10+"})
			}),
		)
	})
}

// histogram рисует столбики counts на всю ширину gtx с подписями labels под
// ними (столбиков столько, сколько подписей; пустой counts — одни подписи).
// Возвращает размер.
func histogram(gtx layout.Context, th *material.Theme, counts []int, labels []string) layout.Dimensions {
	width, barsHeight := gtx.Constraints.Max.X, gtx.Dp(histogramHeight)
	slot := float32(width) / float32(len(labels))
	pad := slot * 0.15
	highest := 1
	for _, c := range counts {
		highest = max(highest, c)
	}
	labelTop := barsHeight + gtx.Dp(unit.Dp(2))
	labelHeight := 0
	for i, text := range labels {
		if i < len(counts) && counts[i] > 0 {
			h := int(float32(counts[i]) / float32(highest) * float32(barsHeight))
			bar := image.Rect(int(float32(i)*slot+pad), barsHeight-h, int(float32(i+1)*slot-pad), barsHeight)
			paint.FillShape(gtx.Ops, colAccent, clip.UniformRRect(bar, gtx.Dp(unit.Dp(2))).Op(gtx.Ops))
		}
		macro := op.Record(gtx.Ops)
		lbl := material.Label(th, unit.Sp(10), text)
		lbl.Color = colTextSecond
		labelGtx := gtx
		labelGtx.Constraints.Min = image.Point{}
		dims := lbl.Layout(labelGtx)
		call := macro.Stop()
		labelHeight = max(labelHeight, dims.Size.Y)
		offset := op.Offset(image.Pt(int(float32(i)*slot+(slot-float32(dims.Size.X))/2), labelTop)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		offset.Pop()
	}
	return layout.Dimensions{Size: image.Pt(width, labelTop+labelHeight)}
}

// lineChart рисует ломаную points на всю ширину gtx высотой chartHeight: x
// от 0 до xMax, y — от минимума до максимума точек (ровную линию — посередине).
// Возвращает размер.
func lineChart(gtx layout.Context, points series, xMax float32) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(chartHeight))
	paint.FillShape(gtx.Ops, colBorder, clip.Rect{Min: image.Pt(0, size.Y-1), Max: size}.Op())
	if len(points) == 0 {
		return layout.Dimensions{Size: size}
	}
	yMin, yMax := points[0].Y, points[0].Y
	for _, pt := range points {
		yMin, yMax = min(yMin, pt.Y), max(yMax, pt.Y)
	}
	pad := float32(gtx.Dp(unit.Dp(4)))
	toScreen := func(pt f32.Point) f32.Point {
		y := float32(size.Y) / 2
		if yMax > yMin {
			y = pad + (yMax-pt.Y)/(yMax-yMin)*(float32(size.Y)-2*pad)
		}
		return f32.Pt(pt.X/xMax*float32(size.X), y)
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(toScreen(points[0]))
	for _, pt := range points[1:] {
		path.LineTo(toScreen(pt))
	}
	if len(points) == 1 { // одна точка — короткий штрих, чтобы её было видно
		path.LineTo(toScreen(points[0]).Add(f32.Pt(float32(gtx.Dp(unit.Dp(2))), 0)))
	}
	paint.FillShape(gtx.Ops, colAccent, clip.Stroke{Path: path.End(), Width: float32(gtx.Dp(unit.Dp(1.5)))}.Op())
	return layout.Dimensions{Size: size}
}

// axisCaption — подписи под графиком: left у левого края, middle по центру,
// right у правого. Возвращает размер строки.
func axisCaption(gtx layout.Context, th *material.Theme, left, middle, right string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	caption := func(content string, alignment text.Alignment) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(th, unit.Sp(10), content)
			lbl.Color = colTextSecond
			lbl.MaxLines = 1
			lbl.Alignment = alignment
			return lbl.Layout(gtx)
		}
	}
	gap := layout.Spacer{Width: unit.Dp(6)}.Layout
	return layout.Flex{}.Layout(gtx,
		layout.Rigid(caption(left, text.Start)),
		layout.Rigid(gap),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			// Label выравнивает текст внутри ширины Min, а Flexed даёт Min=0
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return caption(middle, text.Middle)(gtx)
		}),
		layout.Rigid(gap),
		layout.Rigid(caption(right, text.End)),
	)
}

// metricTitle — название метрики и за ним серый значок (i): наведёшь мышь —
// под ним всплывает description. Название обрезается с «…», а значок
// остаётся виден. maxRight — правее этого x (от левого края названия)
// подсказка не вылезает. Занимает хотя бы Min.X (чтобы значение рядом
// уехало вправо, как и раньше без значка); возвращает размер с базовой
// линией названия — для выравнивания по ней со значением.
func (p *metricsPanel) metricTitle(gtx layout.Context, th *material.Theme, label, description string, maxRight int) layout.Dimensions {
	iconSide, gap := gtx.Dp(infoIconSize), gtx.Dp(unit.Dp(4))
	labelGtx := gtx
	labelGtx.Constraints.Min = image.Point{}
	labelGtx.Constraints.Max.X = max(gtx.Constraints.Max.X-iconSide-gap, 0)
	macro := op.Record(gtx.Ops)
	labelDims := metricLabel(labelGtx, th, label)
	labelCall := macro.Stop()

	height := max(labelDims.Size.Y, iconSide)
	labelTop := (height - labelDims.Size.Y) / 2
	offset := op.Offset(image.Pt(0, labelTop)).Push(gtx.Ops)
	labelCall.Add(gtx.Ops)
	offset.Pop()

	hover := p.infoHovers[label]
	if hover == nil {
		hover = new(gesture.Hover)
		p.infoHovers[label] = hover
	}
	iconAt := image.Pt(labelDims.Size.X+gap, (height-iconSide)/2)
	offset = op.Offset(iconAt).Push(gtx.Ops)
	iconGtx := gtx
	iconGtx.Constraints = layout.Exact(image.Pt(iconSide, iconSide))
	iconInfo.Layout(iconGtx, colDisabledFill)
	area := clip.Rect{Max: image.Pt(iconSide, iconSide)}.Push(gtx.Ops)
	hover.Add(gtx.Ops)
	area.Pop()
	if hover.Update(gtx.Source) {
		tooltipBelow(gtx, th, image.Pt(iconSide, iconSide), maxRight-iconAt.X, description)
	}
	offset.Pop()

	width := max(iconAt.X+iconSide, gtx.Constraints.Min.X)
	return layout.Dimensions{Size: image.Pt(width, height), Baseline: height - labelTop - labelDims.Size.Y + labelDims.Baseline}
}

// metricLabel — название метрики в одну строку. Возвращает размер.
func metricLabel(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	lbl := material.Label(th, unit.Sp(12), text)
	lbl.Font.Weight = font.Medium
	lbl.Color = colTextNormal
	lbl.MaxLines = 1
	return lbl.Layout(gtx)
}

// metricValue — крупное текущее значение d или "—", если его ещё нет.
// Возвращает размер.
func metricValue(gtx layout.Context, th *material.Theme, view metricsView, d metricDef) layout.Dimensions {
	text := "—"
	if v, ok := view.current(d); ok {
		text = fmt.Sprintf(d.format, v)
	}
	lbl := material.Label(th, unit.Sp(15), text)
	lbl.Font.Weight = font.Medium
	lbl.Color = colTextNormal
	lbl.MaxLines = 1
	return lbl.Layout(gtx)
}

// card — рамка со скруглёнными углами и отступом вокруг content на всю
// ширину gtx. Возвращает размер карточки.
func card(gtx layout.Context, content layout.Widget) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, content)
	call := macro.Stop()
	rect := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(unit.Dp(6))
	paint.FillShape(gtx.Ops, colFieldBG, clip.UniformRRect(rect, radius).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, colBorder, clip.Stroke{Path: clip.UniformRRect(rect, radius).Path(gtx.Ops), Width: float32(gtx.Dp(unit.Dp(1)))}.Op())
	call.Add(gtx.Ops)
	return dims
}

// toggleButton — кнопка-переключатель в духе Start: включённая залита
// акцентным цветом, выключенная — только контур. На выключенном gtx
// (gtx.Disabled) серая и не нажимается. icon может быть nil.
// Возвращает размер кнопки.
func toggleButton(gtx layout.Context, th *material.Theme, click *widget.Clickable, label string, icon *widget.Icon, on bool) layout.Dimensions {
	stroke, fill, text := colAccent, color.NRGBA{}, colAccent
	switch {
	case !gtx.Enabled() && on:
		stroke, fill, text = colDisabledFill, colDisabledFill, colTextOnAccent
	case !gtx.Enabled():
		stroke, text = colBorder, colTextSecond
	case on:
		fill, text = colAccent, colTextOnAccent
	case click.Hovered():
		fill = colRowTint
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		dims := layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if icon == nil {
						return layout.Dimensions{}
					}
					side := gtx.Dp(unit.Dp(16))
					gtx.Constraints = layout.Exact(image.Pt(side, side))
					icon.Layout(gtx, text)
					return layout.Dimensions{Size: image.Pt(side+gtx.Dp(unit.Dp(4)), side)}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(th, unit.Sp(13), label)
					lbl.Color = text
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				}),
			)
		})
		call := macro.Stop()
		rect := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(unit.Dp(4))
		if fill.A > 0 {
			paint.FillShape(gtx.Ops, fill, clip.UniformRRect(rect, radius).Op(gtx.Ops))
		}
		paint.FillShape(gtx.Ops, stroke, clip.Stroke{Path: clip.UniformRRect(rect, radius).Path(gtx.Ops), Width: float32(gtx.Dp(unit.Dp(1)))}.Op())
		call.Add(gtx.Ops)
		if gtx.Enabled() {
			defer clip.Rect(rect).Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
		}
		return dims
	})
}

// noTooltipLimit — у tooltipBelow нет ограничения справа.
const noTooltipLimit = 1 << 30

// tooltipBelow рисует подсказку text под элементом размера anchor (в его
// координатах) поверх всего остального — через op.Defer, иначе её закрыло бы
// то, что раскладывается позже. maxRight — правее этого x (от левого края
// элемента) подсказка не вылезает: если не влезает, сдвигается влево (иначе
// у правой колонки её обрезал бы край окна).
func tooltipBelow(gtx layout.Context, th *material.Theme, anchor image.Point, maxRight int, text string) {
	tipGtx := gtx
	tipGtx.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(unit.Dp(260)), gtx.Dp(unit.Dp(200)))}
	inner := op.Record(gtx.Ops)
	dims := layout.UniformInset(unit.Dp(6)).Layout(tipGtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(th, unit.Sp(11), text)
		lbl.Color = colTooltipText
		return lbl.Layout(gtx)
	})
	innerCall := inner.Stop()

	macro := op.Record(gtx.Ops)
	left := min(0, maxRight-dims.Size.X)
	op.Offset(image.Pt(left, anchor.Y+gtx.Dp(unit.Dp(4)))).Add(gtx.Ops)
	paint.FillShape(gtx.Ops, colTooltipBG, clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(unit.Dp(4))).Op(gtx.Ops))
	innerCall.Add(gtx.Ops)
	op.Defer(gtx.Ops, macro.Stop())
}
