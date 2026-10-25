package geometry

import (
	_ "embed"
	"fmt"
	"math"
	"strings"
)

//go:embed testdata/kulman.dxf
var wingDXF string

// Preset возвращает захардкоженные моим клодкодом Geometry-объекты, таким образом можно без
// входных файлов работать для тестов. Known names: "ellipse", "circle", "zalesak", "wing"
// тут все тотально нанейрослоплено и меняться не будет ибо це тест код
func Preset(name string) (Geometry, error) {
	switch name {
	case "ellipse":
		return ellipse(1.0, 0.6, 128).Normalized()
	case "circle":
		return ellipse(1.0, 1.0, 128).Normalized()
	case "zalesak":
		return zalesak().Normalized()
	case "wing":
		g, err := LoadDXF(strings.NewReader(wingDXF))
		if err != nil {
			return Geometry{}, fmt.Errorf("geometry: embedded wing preset: %w", err)
		}
		return g.Normalized()
	default:
		return Geometry{}, fmt.Errorf("geometry: unknown preset %q", name)
	}
}

func ellipse(a, b float64, n int) Geometry {
	pl := make(Polyline, n)
	for i := 0; i < n; i++ {
		t := 2 * math.Pi * float64(i) / float64(n)
		pl[i] = Point{a * math.Cos(t), b * math.Sin(t)}
	}
	return Geometry{Outer: pl}
}

// zalesak is the classic slotted disk: a unit-ish circle with a rectangular
// notch cut from the bottom. Standard mesh-generation benchmark.
func zalesak() Geometry {
	const (
		r         = 1.0
		slotHalfW = 0.15
		slotTop   = 0.5 // y where the slot ends inside the disk
	)
	var pl Polyline
	// Circle arc, skipping the wedge occupied by the slot mouth.
	a0 := math.Asin(slotHalfW / r) // angle from -Y axis to slot edge
	start := -math.Pi/2 + a0
	end := 3*math.Pi/2 - a0
	const steps = 200
	for i := 0; i <= steps; i++ {
		t := start + (end-start)*float64(i)/float64(steps)
		pl = append(pl, Point{r * math.Cos(t), r * math.Sin(t)})
	}
	// Walk the slot: up the left wall, across the top, down the right wall.
	yMouth := -math.Sqrt(r*r - slotHalfW*slotHalfW)
	pl = append(pl,
		Point{-slotHalfW, yMouth},
		Point{-slotHalfW, slotTop},
		Point{slotHalfW, slotTop},
		Point{slotHalfW, yMouth},
	)
	return Geometry{Outer: pl}
}
