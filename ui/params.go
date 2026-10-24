package main

import (
	"image"
	"math"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"MTBuild/internal/mesher"
)

// paramCtrl is one ParamSpec's live UI state: a normalized [0,1] slider plus
// an editable number box showing the real value. widget.Float only knows
// [0,1], so the spec's Min/Max live here to convert both ways. A KindBool
// spec uses check instead and leaves float/editor unused.
type paramCtrl struct {
	spec   mesher.ParamSpec
	float  widget.Float
	editor widget.Editor
	check  widget.Bool // used only for KindBool
}

func newParamCtrl(spec mesher.ParamSpec) *paramCtrl {
	c := &paramCtrl{spec: spec}
	if spec.Kind == mesher.KindBool {
		c.check.Value = spec.Def >= 0.5
		return c
	}
	c.editor.SingleLine = true
	c.editor.Submit = true
	c.setValue(spec.Def)
	return c
}

func (c *paramCtrl) value() float64 {
	if c.spec.Kind == mesher.KindBool {
		if c.check.Value {
			return 1
		}
		return 0
	}
	return c.spec.Min + float64(c.float.Value)*(c.spec.Max-c.spec.Min)
}

func (c *paramCtrl) setValue(v float64) {
	if v < c.spec.Min {
		v = c.spec.Min
	}
	if v > c.spec.Max {
		v = c.spec.Max
	}
	if c.spec.Max > c.spec.Min {
		c.float.Value = float32((v - c.spec.Min) / (c.spec.Max - c.spec.Min))
	}
	c.editor.SetText(formatParam(v, c.spec.Step))
}

// Layout draws the label, the number box and the slider, and reconciles
// edits made through either control. A KindBool spec is a single checkbox.
func (c *paramCtrl) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if c.spec.Kind == mesher.KindBool {
		cb := material.CheckBox(th, &c.check, c.spec.Label)
		cb.TextSize = unit.Sp(11)
		cb.Color = colTextSecond
		cb.IconColor = colAccent
		cb.Size = unit.Dp(18)
		return cb.Layout(gtx)
	}

	for {
		ev, ok := c.editor.Update(gtx)
		if !ok {
			break
		}
		if _, submitted := ev.(widget.SubmitEvent); submitted {
			if v, err := strconv.ParseFloat(strings.TrimSpace(c.editor.Text()), 64); err == nil {
				c.setValue(v)
			} else {
				c.setValue(c.value()) // bad input: revert the box to the last good value
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(th, unit.Sp(11), c.spec.Label)
					lbl.Color = colTextSecond
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return numberBox(gtx, th, &c.editor)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			before := c.float.Value
			dims := material.Slider(th, &c.float).Layout(gtx)
			if c.float.Value != before {
				c.snapToStep()
				if !gtx.Source.Focused(&c.editor) {
					c.editor.SetText(formatParam(c.value(), c.spec.Step))
				}
			}
			return dims
		}),
	)
}

// snapToStep rounds the slider to Min + n*Step, so a dragged value lands on
// the same grid the number box formats to. Typed values aren't snapped.
func (c *paramCtrl) snapToStep() {
	s := c.spec
	if s.Step <= 0 || s.Max <= s.Min {
		return
	}
	v := math.Min(s.Min+math.Round((c.value()-s.Min)/s.Step)*s.Step, s.Max)
	c.float.Value = float32((v - s.Min) / (s.Max - s.Min))
}

const numberBoxWidth = unit.Dp(56)

func numberBox(gtx layout.Context, th *material.Theme, ed *widget.Editor) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(numberBoxWidth)
	gtx.Constraints.Max.X = gtx.Dp(numberBoxWidth)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Min
			paint.FillShape(gtx.Ops, colBorder, clip.Rect{Max: sz}.Op())
			b := gtx.Dp(unit.Dp(1))
			inner := image.Rect(b, b, sz.X-b, sz.Y-b)
			if inner.Dx() > 0 && inner.Dy() > 0 {
				paint.FillShape(gtx.Ops, colFieldBG, clip.Rect(inner).Op())
			}
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(th, ed, "")
				e.TextSize = unit.Sp(11)
				e.Color = colTextNormal
				return e.Layout(gtx)
			})
		}),
	)
}

// formatParam renders v with the number of decimals implied by step (e.g.
// step=0.005 -> 3 decimals, step=10 -> integer), matching what the slider
// can actually reach.
func formatParam(v, step float64) string {
	dec := 0
	if step > 0 && step < 1 {
		dec = int(math.Ceil(-math.Log10(step)))
	}
	return strconv.FormatFloat(v, 'f', dec, 64)
}

// algoParams holds one algorithm's live parameter controls, built once from
// its Params() so the same []mesher.ParamSpec-driven code works for every
// registered algorithm.
type algoParams struct {
	order []string
	ctrls map[string]*paramCtrl
}

func newAlgoParams(specs []mesher.ParamSpec) *algoParams {
	ap := &algoParams{ctrls: make(map[string]*paramCtrl, len(specs))}
	for _, spec := range specs {
		ap.order = append(ap.order, spec.Key)
		ap.ctrls[spec.Key] = newParamCtrl(spec)
	}
	// A DependsOn that points nowhere would silently hide the control
	// forever — that's a bug in the algorithm's Params(), so fail loudly.
	for _, key := range ap.order {
		dep := ap.ctrls[key].spec.DependsOn
		if dep == "" {
			continue
		}
		if d, ok := ap.ctrls[dep]; !ok || d.spec.Kind != mesher.KindBool {
			panic("ui: param " + key + " DependsOn " + dep + ", which is not a KindBool param of the same algorithm")
		}
	}
	return ap
}

// visible reports whether key's control should be drawn: a param with
// DependsOn is shown only while that checkbox is ticked.
func (ap *algoParams) visible(key string) bool {
	dep := ap.ctrls[key].spec.DependsOn
	return dep == "" || ap.ctrls[dep].check.Value
}

func (ap *algoParams) values() mesher.Params {
	out := make(mesher.Params, len(ap.ctrls))
	for key, c := range ap.ctrls {
		out[key] = c.value()
	}
	return out
}

// paramsState holds every registered algorithm's parameter controls,
// keyed by name, built once up front.
type paramsState struct {
	byAlgo map[string]*algoParams
}

func newParamsState() *paramsState {
	ps := &paramsState{byAlgo: make(map[string]*algoParams, len(algoOrder))}
	for _, name := range algoOrder {
		m, err := mesher.New(name)
		if err != nil {
			panic("ui: algoOrder names an unregistered mesher: " + name)
		}
		ps.byAlgo[name] = newAlgoParams(m.Params())
	}
	return ps
}
