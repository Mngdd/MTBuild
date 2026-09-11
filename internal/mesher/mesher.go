// Package mesher defines the "black box" contract every meshing algorithm
// implements. Callers (the CLI now, a GUI later) depend only on this package;
// concrete algorithms live in sub-packages and register themselves.
package mesher

import (
	"context"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
)

// ParamSpec declares one tunable knob of an algorithm. The UI builds its
// controls from a []ParamSpec without knowing what the algorithm does.
type ParamSpec struct {
	Key   string  // machine name, the key used in Params
	Label string  // human-readable description for the UI
	Min   float64 // lower bound (inclusive)
	Max   float64 // upper bound (inclusive)
	Def   float64 // value used when the caller omits this key
	Step  float64 // UI slider increment; ignored by the CLI
}

// Params carries user-supplied values keyed by ParamSpec.Key.
type Params map[string]float64

// Get returns the value for spec, falling back to spec.Def, and clamps the
// result to [spec.Min, spec.Max].
func (p Params) Get(spec ParamSpec) float64 {
	v, ok := p[spec.Key]
	if !ok {
		v = spec.Def
	}
	if spec.Max > spec.Min {
		if v < spec.Min {
			v = spec.Min
		}
		if v > spec.Max {
			v = spec.Max
		}
	}
	return v
}

// Frame is a snapshot of intermediate state emitted during Generate, so callers
// can visualise progress (particles relaxing, mesh forming) rather than only
// the final result. Fields are populated as the relevant stage produces them.
type Frame struct {
	Stage     string           // e.g. "sph-relax"
	Iter      int              // iteration index within the stage, if applicable
	Particles []geometry.Point // current point positions
	Mesh      *mesh.Mesh       // set once a triangulation exists
}

// Sink receives progress reports and snapshots. Implementations must be safe to
// call from the goroutine running Generate and must not block.
type Sink interface {
	Progress(stage string, frac float64) // frac in [0,1]
	Snapshot(Frame)
}

// NopSink discards everything. Use it in tests or headless runs.
type NopSink struct{}

func (NopSink) Progress(string, float64) {}
func (NopSink) Snapshot(Frame)           {}

// Mesher is one meshing algorithm.
type Mesher interface {
	// Name is the stable identifier used in the registry and the UI dropdown.
	Name() string

	// Params declares the algorithm's tunable parameters.
	Params() []ParamSpec

	// Generate builds a mesh for g. It must honour ctx cancellation, report
	// progress and snapshots through sink, and return an error rather than a
	// partial mesh on failure.
	Generate(ctx context.Context, g geometry.Geometry, p Params, sink Sink) (mesh.Mesh, mesh.QualityMetrics, error)
}
