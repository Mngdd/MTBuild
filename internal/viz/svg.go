// Package viz renders geometry and meshes to SVG for visual inspection. It is a
// consumer of the core types and is never imported by a mesher.
package viz

import (
	"fmt"
	"io"
	"math"
	"strings"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
)

// Fitter maps world coordinates into a Y-flipped pixel box of the given width.
type Fitter struct {
	min    geometry.Point
	scale  float64
	height int
	margin float64
}

func NewFitter(min, max geometry.Point, width int) (Fitter, error) {
	dw, dh := max.X-min.X, max.Y-min.Y
	if dw <= 0 || dh <= 0 {
		return Fitter{}, fmt.Errorf("viz: degenerate bounding box")
	}
	const margin = 20.0
	scale := (float64(width) - 2*margin) / dw
	return Fitter{min: min, scale: scale, height: int(dh*scale + 2*margin), margin: margin}, nil
}

func (f Fitter) Tx(p geometry.Point) (float64, float64) {
	return f.margin + (p.X-f.min.X)*f.scale, float64(f.height) - f.margin - (p.Y-f.min.Y)*f.scale
}

// MeshSVG writes an SVG view of a triangulation over its geometry.
func MeshSVG(w io.Writer, g geometry.Geometry, m mesh.Mesh, width int) error {
	min, max := g.BoundingBox()
	f, err := NewFitter(min, max, width)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n",
		width, f.height, width, f.height)
	fmt.Fprint(w, `<rect width="100%" height="100%" fill="white"/>`+"\n")
	for i := range m.Triangles {
		a, b, c := m.Tri(i)
		ax, ay := f.Tx(a)
		bx, by := f.Tx(b)
		cx, cy := f.Tx(c)
		fmt.Fprintf(w, `<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="#eef4fb" stroke="#4682b4" stroke-width="0.6"/>`+"\n",
			ax, ay, bx, by, cx, cy)
	}
	// Boundary on top.
	fmt.Fprintf(w, `<path d="%s" fill="none" stroke="#1560b0" stroke-width="1.6"/>`+"\n", closedPath(f, g.Outer))
	for _, h := range g.Holes {
		fmt.Fprintf(w, `<path d="%s" fill="none" stroke="#b01515" stroke-width="1.6"/>`+"\n", closedPath(f, h))
	}
	fmt.Fprintln(w, "</svg>")
	return nil
}

// PolyMeshSVG writes an SVG view of a polygonal mesh (Mesh.Cells) over its
// geometry — the Voronoi-style counterpart to MeshSVG's triangulation view.
func PolyMeshSVG(w io.Writer, g geometry.Geometry, m mesh.Mesh, width int) error {
	min, max := g.BoundingBox()
	f, err := NewFitter(min, max, width)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n",
		width, f.height, width, f.height)
	fmt.Fprint(w, `<rect width="100%" height="100%" fill="white"/>`+"\n")
	for i := range m.Cells {
		cell := m.Cell(i)
		if len(cell) < 3 {
			continue
		}
		var b strings.Builder
		for _, p := range cell {
			x, y := f.Tx(p)
			fmt.Fprintf(&b, "%.2f,%.2f ", x, y)
		}
		fmt.Fprintf(w, `<polygon points="%s" fill="#fbeee0" stroke="#b47a34" stroke-width="0.8"/>`+"\n", b.String())
	}
	fmt.Fprintf(w, `<path d="%s" fill="none" stroke="#1560b0" stroke-width="1.6"/>`+"\n", closedPath(f, g.Outer))
	for _, h := range g.Holes {
		fmt.Fprintf(w, `<path d="%s" fill="none" stroke="#b01515" stroke-width="1.6"/>`+"\n", closedPath(f, h))
	}
	fmt.Fprintln(w, "</svg>")
	return nil
}

func closedPath(f Fitter, pl geometry.Polyline) string {
	var b strings.Builder
	for i, p := range pl {
		x, y := f.Tx(p)
		if i == 0 {
			fmt.Fprintf(&b, "M%.2f,%.2f", x, y)
		} else {
			fmt.Fprintf(&b, "L%.2f,%.2f", x, y)
		}
	}
	b.WriteString("Z")
	return b.String()
}

// GeometrySVG writes an SVG view of g, fitted to the given pixel width with a
// margin. The Y axis is flipped so the image reads like a normal plot.
func GeometrySVG(w io.Writer, g geometry.Geometry, width int) error {
	min, max := g.BoundingBox()
	f, err := NewFitter(min, max, width)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n",
		width, f.height, width, f.height)
	fmt.Fprint(w, `<rect width="100%" height="100%" fill="white"/>`+"\n")
	fmt.Fprintf(w, `<path d="%s" fill="#cfe8ff" stroke="#1560b0" stroke-width="1.5"/>`+"\n", closedPath(f, g.Outer))
	for _, h := range g.Holes {
		fmt.Fprintf(w, `<path d="%s" fill="white" stroke="#b01515" stroke-width="1.5"/>`+"\n", closedPath(f, h))
	}
	// Mark the vertices so tessellation density is visible.
	for _, p := range g.Outer {
		x, y := f.Tx(p)
		fmt.Fprintf(w, `<circle cx="%.2f" cy="%.2f" r="1.4" fill="#1560b0"/>`+"\n", x, y)
	}
	fmt.Fprintln(w, "</svg>")
	return nil
}

// Describe returns a one-line human summary of a geometry.
func Describe(g geometry.Geometry) string {
	min, max := g.BoundingBox()
	return fmt.Sprintf("outer=%d pts, holes=%d, bbox=[%.3g,%.3g]x[%.3g,%.3g] (%.3gx%.3g), area=%.4g",
		len(g.Outer), len(g.Holes),
		min.X, max.X, min.Y, max.Y,
		max.X-min.X, max.Y-min.Y,
		math.Abs(g.Outer.SignedArea()))
}
