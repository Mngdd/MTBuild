package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
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

type cellMetricMode int

const (
	metricNone cellMetricMode = iota
	metricPerimeter
	metricSkewness
	metricModeCount
)

const (
	metricColorSteps = 100
	metricNoValue    = metricColorSteps + 1
)

var cellMetricNames = [metricModeCount]string{"Ничего", "r", "S"}
var iconMetricDropdown = mustIcon(icons.NavigationArrowDropDown)

type cellMetricScale struct {
	min, max float64
	ticks    [3]string
	colors   [9][3]uint8
}

// Цвета сняты со шкал на рис. 1 и 2 отчёта о качестве сеток в круге.
var cellMetricScales = [metricModeCount]cellMetricScale{
	metricPerimeter: {
		min: 1, max: 1.1, ticks: [3]string{"1.000", "1.050", "1.100"},
		colors: [9][3]uint8{
			{255, 255, 255}, {255, 255, 218}, {255, 255, 187}, {255, 255, 163},
			{255, 255, 148}, {255, 239, 148}, {255, 214, 148}, {255, 184, 148}, {255, 148, 148},
		},
	},
	metricSkewness: {
		min: 0, max: 0.4, ticks: [3]string{"0", "0.2", "0.4"},
		colors: [9][3]uint8{
			{255, 255, 255}, {218, 255, 255}, {187, 255, 255}, {163, 255, 255},
			{148, 255, 255}, {148, 249, 239}, {148, 241, 214}, {148, 231, 184}, {148, 219, 148},
		},
	},
}

type cellMetricLabel struct {
	text   [metricModeCount]string
	radius float64 // расстояние от центроида до ближайшего ребра
}

type cellMetricView struct {
	mode              cellMetricMode
	open              bool
	trigger, backdrop widget.Clickable
	options           [metricModeCount]widget.Clickable
	labels            []cellMetricLabel
	// Ячейки одного цвета рисуем одним путём.
	fillGroups [metricModeCount][metricNoValue + 1][]int
}

func (v *cellMetricView) reset(count int) {
	v.open = false
	v.labels = make([]cellMetricLabel, count)
	v.fillGroups = [metricModeCount][metricNoValue + 1][]int{}
}

func (v *cellMetricView) active() bool {
	return v.mode != metricNone && len(v.labels) > 0
}

// setCell сохраняет подпись и цвет ячейки i для обеих метрик.
// polygon и center задают контур ячейки и её центроид.
func (v *cellMetricView) setCell(i int, quality mesh.CellQuality, polygon geometry.Polyline, center geometry.Point) {
	label := &v.labels[i]
	if quality.Valid && polygon.ContainsPoint(center) {
		cell := geometry.Geometry{Outer: polygon}
		label.radius = center.Dist(cell.Nearest(center))
	}
	values := [metricModeCount]float64{metricPerimeter: quality.PerimeterRatio, metricSkewness: quality.Skewness}
	for mode := metricPerimeter; mode < metricModeCount; mode++ {
		scale, value := cellMetricScales[mode], values[mode]
		band := metricNoValue
		label.text[mode] = "—"
		if quality.Valid && !math.IsNaN(value) && !math.IsInf(value, 0) {
			position := min(max((value-scale.min)/(scale.max-scale.min), 0), 1)
			band = int(math.Round(position * metricColorSteps))
			label.text[mode] = fmt.Sprintf("%.3f", value)
		}
		v.fillGroups[mode][band] = append(v.fillGroups[mode][band], i)
	}
}

// colorAt возвращает цвет шкалы в точке position от 0 до 1.
func (s cellMetricScale) colorAt(position float64) color.NRGBA {
	step := position * float64(len(s.colors)-1)
	i := min(int(step), len(s.colors)-2)
	fraction := step - float64(i)
	start, end := s.colors[i], s.colors[i+1]
	channel := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + fraction*(float64(b)-float64(a))))
	}
	return color.NRGBA{R: channel(start[0], end[0]), G: channel(start[1], end[1]), B: channel(start[2], end[2]), A: 0xff}
}

// drawCellMetricFills закрашивает основную сетку по выбранной метрике.
// toScreen переводит вершины сетки в пиксели.
func (v *meshView) drawCellMetricFills(ops *op.Ops, toScreen func(geometry.Point) f32.Point) {
	mode := v.cellMetrics.mode
	scale := cellMetricScales[mode]
	for band, cells := range v.cellMetrics.fillGroups[mode] {
		if len(cells) == 0 {
			continue
		}
		fill := color.NRGBA{R: 0xe5, G: 0xe5, B: 0xe5, A: 0xff}
		if band != metricNoValue {
			fill = scale.colorAt(float64(band) / metricColorSteps)
		}
		var path clip.Path
		path.Begin(ops)
		for _, i := range cells {
			v.addCellPath(&path, v.mesh.Cells[i], toScreen)
		}
		paint.FillShape(ops, fill, clip.Outline{Path: path.End()}.Op())
	}
}

// drawCellMetrics выводит выбранную метрику и подбирает размер текста под ячейку.
// size — размер холста в пикселях, toScreen переводит точки сетки в пиксели.
func (v *meshView) drawCellMetrics(gtx layout.Context, th *material.Theme, size image.Point, toScreen func(geometry.Point) f32.Point) {
	const minTextSize, maxTextSize = unit.Sp(8), unit.Sp(12)
	labelGtx := gtx
	labelGtx.Constraints = layout.Constraints{Max: size}
	showMarkers := !v.centroidsLayer.hidden || (!v.sitesLayer.hidden && v.cellSites() != nil)
	for i, label := range v.cellMetrics.labels {
		if label.radius <= 0 {
			continue
		}
		center := v.centroids[i]
		screen := toScreen(center)
		if screen.X < 0 || screen.Y < 0 || screen.X >= float32(size.X) || screen.Y >= float32(size.Y) {
			continue
		}
		radiusPx := toScreen(geometry.Point{X: center.X + label.radius, Y: center.Y}).X - screen.X
		textSize := min(maxTextSize, gtx.Metric.PxToSp(int(radiusPx*0.6)))
		if textSize < minTextSize {
			continue
		}

		value := material.Label(th, textSize, label.text[v.cellMetrics.mode])
		value.Color = colTextNormal
		value.MaxLines = 1
		macro := op.Record(gtx.Ops)
		dims := value.Layout(labelGtx)
		call := macro.Stop()

		halfWidth := float64(dims.Size.X)/2 + 1
		maxOffset := math.Sqrt(max(0, float64(radiusPx)*float64(radiusPx)-halfWidth*halfWidth)) - float64(dims.Size.Y)/2
		if maxOffset < 0 {
			continue
		}
		// При видимых маркерах ставим число чуть ниже центроида.
		if showMarkers {
			screen.Y += float32(min(maxOffset, float64(gtx.Dp(unit.Dp(9)))))
		}
		centerPx := image.Pt(int(math.Round(float64(screen.X))), int(math.Round(float64(screen.Y))))
		stack := op.Offset(centerPx.Sub(dims.Size.Div(2))).Push(gtx.Ops)
		call.Add(gtx.Ops)
		stack.Pop()
	}
}

func (v *cellMetricView) update(gtx layout.Context) {
	if len(v.labels) == 0 {
		return
	}
	if v.trigger.Clicked(gtx) {
		v.open = !v.open
	}
	if v.backdrop.Clicked(gtx) {
		v.open = false
	}
	for mode := metricNone; mode < metricModeCount; mode++ {
		if v.options[mode].Clicked(gtx) {
			v.mode, v.open = mode, false
		}
	}
}

// menu рисует кнопку текущего режима и список вариантов под ней.
func (v *cellMetricView) menu(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(126)))
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	trigger := material.ButtonLayout(th, &v.trigger)
	trigger.Background = colTextSecond
	drawTrigger := func(gtx layout.Context) layout.Dimensions {
		return trigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			inset := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}
			return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				label := material.Label(th, unit.Sp(12), "Метрика: "+cellMetricNames[v.mode])
				label.Color = colTextOnAccent
				label.MaxLines = 1
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(label.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						side := min(gtx.Dp(unit.Dp(16)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
						gtx.Constraints = layout.Exact(image.Pt(side, side))
						return iconMetricDropdown.Layout(gtx, colTextOnAccent)
					}),
				)
			})
		})
	}
	if !v.open {
		return drawTrigger(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(drawTrigger),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				rect := image.Rectangle{Max: gtx.Constraints.Min}
				paint.FillShape(gtx.Ops, colBorder, clip.Rect(rect).Op())
				paint.FillShape(gtx.Ops, colFieldBG, clip.Rect(rect.Inset(gtx.Dp(unit.Dp(1)))).Op())
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}, func(gtx layout.Context) layout.Dimensions {
				var rows [metricModeCount]layout.FlexChild
				for mode := metricNone; mode < metricModeCount; mode++ {
					choice := mode
					rows[mode] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return dropdownRow(gtx, th, &v.options[choice], cellMetricNames[choice], v.mode == choice)
					})
				}
				return layout.UniformInset(unit.Dp(1)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows[:]...)
				})
			})
		}),
	)
}

// scale рисует легенду активной метрики в границах, заданных gtx.
func (v *cellMetricView) scale(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(140)))
	scale := cellMetricScales[v.mode]
	return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, colFieldBG, clip.Rect{Max: gtx.Constraints.Min}.Op())
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(7)))
					paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: size}.Op())
					for band := 0; band <= metricColorSteps; band++ {
						x0 := 1 + (size.X-2)*band/(metricColorSteps+1)
						x1 := 1 + (size.X-2)*(band+1)/(metricColorSteps+1)
						paint.FillShape(gtx.Ops, scale.colorAt(float64(band)/metricColorSteps), clip.Rect(image.Rect(x0, 1, x1, size.Y-1)).Op())
					}
					return layout.Dimensions{Size: size}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					var ticks [len(scale.ticks)]layout.FlexChild
					for i, tick := range scale.ticks {
						ticks[i] = layout.Rigid(material.Label(th, unit.Sp(10), tick).Layout)
					}
					return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx, ticks[:]...)
				}),
			)
		})
	})
}
