package main

import (
	"image"
	"math"
	"slices"
	"strconv"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"MTBuild/internal/mesh"
)

const (
	metricDistributionStep    = 0.005
	maxMetricDistributionBins = 512
)

type metricDistribution struct {
	min, max, ideal float64
	step            float64
	bins            []float32
	peak            float32
}

type cellMetricDistributions struct {
	perimeter metricDistribution
	skewness  metricDistribution
}

func buildCellDistributions(cells []mesh.CellQuality) cellMetricDistributions {
	return cellMetricDistributions{
		perimeter: buildMetricDistribution(cells, 1, func(c mesh.CellQuality) float64 { return c.PerimeterRatio }),
		skewness:  buildMetricDistribution(cells, 0, func(c mesh.CellQuality) float64 { return c.Skewness }),
	}
}

// buildMetricDistribution группирует значения выбранной метрики готовых ячеек
// и возвращает сглаженные частоты и отметки для оси X.
func buildMetricDistribution(cells []mesh.CellQuality, ideal float64, valueOf func(mesh.CellQuality) float64) metricDistribution {
	result := metricDistribution{min: math.Inf(1), max: math.Inf(-1), ideal: ideal}
	usable := func(cell mesh.CellQuality, value float64) bool {
		return cell.Valid && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	for _, cell := range cells {
		if value := valueOf(cell); usable(cell, value) {
			result.min = min(result.min, value)
			result.max = max(result.max, value)
		}
	}
	if result.min > result.max {
		return result
	}
	span := result.max - result.min
	result.step = max(metricDistributionStep, span/maxMetricDistributionBins)
	count := max(1, min(maxMetricDistributionBins, int(math.Ceil(span/result.step))))
	result.bins = make([]float32, count)
	for _, cell := range cells {
		if value := valueOf(cell); usable(cell, value) {
			bin := min(int((value-result.min)/result.step), count-1)
			result.bins[bin]++
		}
	}

	// Взвешенное среднее по трём корзинам. На краях повторяем крайнюю
	// корзину, чтобы сумма частот сохранилась.
	previous := result.bins[0]
	for i, current := range result.bins {
		next := result.bins[min(i+1, count-1)]
		result.bins[i] = (previous + 2*current + next) * 0.25
		result.peak = max(result.peak, result.bins[i])
		previous = current
	}
	return result
}

func (p *metricsPanel) distributionCards(gtx layout.Context, th *material.Theme, distributions cellMetricDistributions) layout.Dimensions {
	cards := [...]struct {
		label        string
		distribution metricDistribution
	}{
		{"Распределение r", distributions.perimeter},
		{"Распределение S", distributions.skewness},
	}
	return layoutGrid(gtx, len(cards), chartCardMinWidth, metricsGap, func(gtx layout.Context, i, roomRight int) layout.Dimensions {
		entry := cards[i]
		return card(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					const description = "Частота значений по ячейкам, сглаженная по соседним корзинам. Вертикальная линия отмечает идеальное значение."
					return p.metricTitle(gtx, th, entry.label, description, gtx.Constraints.Max.X+roomRight)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return entry.distribution.layout(gtx, th)
				}),
			)
		})
	})
}

func (d metricDistribution) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	width, height := gtx.Constraints.Max.X, gtx.Dp(histogramHeight)
	if len(d.bins) == 0 {
		label := material.Label(th, unit.Sp(11), "Нет значений")
		label.Color = colTextSecond
		return label.Layout(gtx)
	}
	if width < 2 || height < 2 {
		return layout.Dimensions{Size: image.Pt(width, height)}
	}
	left, right := d.min, d.max
	if len(d.bins) == 1 {
		// Одна корзина имеет видимую ширину и у сетки с одинаковыми значениями.
		left = max(0, left+(right-left-d.step)/2)
		right = max(right, left+d.step)
	}
	axisMin, axisMax := min(left, d.ideal), max(right, d.ideal)
	toX := func(value float64) float32 {
		return float32((value-axisMin)/(axisMax-axisMin)) * float32(width-1)
	}
	baseY, peakY := float32(height-1), float32(gtx.Dp(unit.Dp(4)))
	toY := func(count float32) float32 { return baseY - count/d.peak*(baseY-peakY) }
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(toX(left), baseY))
	previous := f32.Pt(toX(left), toY(d.bins[0]))
	path.LineTo(previous)
	for i, count := range d.bins {
		binLeft := left + float64(i)*d.step
		binCenter := binLeft + min(d.step, right-binLeft)/2
		next := f32.Pt(toX(binCenter), toY(count))
		path.QuadTo(previous, previous.Add(next).Mul(0.5))
		previous = next
	}
	path.QuadTo(previous, f32.Pt(toX(right), previous.Y))
	path.LineTo(f32.Pt(toX(right), baseY))
	path.Close()
	fill := colAccent
	fill.A = 0x90
	paint.FillShape(gtx.Ops, fill, clip.Outline{Path: path.End()}.Op())
	paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, height-1, width, height)).Op())
	idealX := int(math.Round(float64(toX(d.ideal))))
	paint.FillShape(gtx.Ops, colTextSecond, clip.Rect(image.Rect(idealX, 0, idealX+1, height)).Op())
	axisHeight := d.layoutAxis(gtx, th, height+gtx.Dp(unit.Dp(2)), toX)
	return layout.Dimensions{Size: image.Pt(width, axisHeight)}
}

// layoutAxis ставит числовые подписи у их отметок. Соседние подписи
// переносятся ниже, если они пересекаются на узкой карточке.
func (d metricDistribution) layoutAxis(gtx layout.Context, th *material.Theme, top int, toX func(float64) float32) int {
	ticks := [...]float64{d.min, d.ideal, d.max}
	slices.Sort(ticks[:])
	gap := gtx.Dp(unit.Dp(4))
	rowRight := [len(ticks)]int{-gap, -gap, -gap}
	rowHeight := gtx.Sp(unit.Sp(14))
	bottom := top
	gtx.Constraints.Min = image.Point{}
	for i, value := range ticks {
		if (i > 0 && value == ticks[i-1]) || (value != d.ideal && math.Abs(value-d.ideal) < 1e-9) {
			continue
		}
		label := material.Label(th, unit.Sp(10), strconv.FormatFloat(value, 'g', 4, 64))
		label.Color = colTextSecond
		label.MaxLines = 1
		if value == d.ideal {
			label.Color = colTextNormal
		}
		macro := op.Record(gtx.Ops)
		dims := label.Layout(gtx)
		call := macro.Stop()
		center := int(math.Round(float64(toX(value))))
		left := max(0, min(center-dims.Size.X/2, gtx.Constraints.Max.X-dims.Size.X))
		row := 0
		for rowRight[row]+gap > left {
			row++
		}
		rowRight[row] = left + dims.Size.X
		y := top + row*rowHeight
		offset := op.Offset(image.Pt(left, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		offset.Pop()
		bottom = max(bottom, y+dims.Size.Y)
	}
	return bottom
}
