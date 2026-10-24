package viz

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
)

var (
	rasterBG       = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	rasterFill     = color.RGBA{R: 0xee, G: 0xf4, B: 0xfb, A: 0xff}
	rasterStroke   = color.RGBA{R: 0x46, G: 0x82, B: 0xb4, A: 0xff}
	rasterBoundary = color.RGBA{R: 0x15, G: 0x60, B: 0xb0, A: 0xff}
	rasterHole     = color.RGBA{R: 0xb0, G: 0x15, B: 0x15, A: 0xff}
	// Poly* mirror PolyMeshSVG's palette (#fbeee0/#b47a34) so the GUI preview
	// and the CLI's -svg output for a Cells mesh read as the same thing.
	rasterPolyFill   = color.RGBA{R: 0xfb, G: 0xee, B: 0xe0, A: 0xff}
	rasterPolyStroke = color.RGBA{R: 0xb4, G: 0x7a, B: 0x34, A: 0xff}
)

// superSample is how much larger than the requested width MeshImage
// rasterizes before downsampling back down. fillTriangle/drawLine have no
// antialiasing of their own (hard-edged, one pixel at a time) — rendering
// at N× and box-filtering down is what turns their jagged edges smooth,
// without touching either algorithm.
const superSample = 2

// MeshImage rasterizes a triangulation over its geometry into an antialiased
// RGBA image of the given pixel width, using the same fit/margin as MeshSVG
// so the two views agree.
func MeshImage(g geometry.Geometry, m mesh.Mesh, width int) (*image.RGBA, error) {
	big, err := renderMesh(g, m, width*superSample)
	if err != nil {
		return nil, err
	}
	return downsample(big, superSample), nil
}

func renderMesh(g geometry.Geometry, m mesh.Mesh, width int) (*image.RGBA, error) {
	min, max := g.BoundingBox()
	f, err := NewFitter(min, max, width)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, f.Height()))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: rasterBG}, image.Point{}, draw.Src)

	for i := range m.Triangles {
		a, b, c := m.Tri(i)
		ax, ay := f.Tx(a)
		bx, by := f.Tx(b)
		cx, cy := f.Tx(c)
		fillTriangle(img, point{ax, ay}, point{bx, by}, point{cx, cy}, rasterFill)
	}
	for i := range m.Triangles {
		a, b, c := m.Tri(i)
		ax, ay := f.Tx(a)
		bx, by := f.Tx(b)
		cx, cy := f.Tx(c)
		drawLine(img, ax, ay, bx, by, rasterStroke)
		drawLine(img, bx, by, cx, cy, rasterStroke)
		drawLine(img, cx, cy, ax, ay, rasterStroke)
	}
	drawPolyline(img, f, g.Outer, rasterBoundary)
	for _, h := range g.Holes {
		drawPolyline(img, f, h, rasterHole)
	}
	return img, nil
}

// PolyMeshImage is MeshImage's counterpart for a polygonal mesh (Mesh.Cells)
// — same fit/supersampling/antialiasing approach, walking cells instead of
// triangles.
func PolyMeshImage(g geometry.Geometry, m mesh.Mesh, width int) (*image.RGBA, error) {
	big, err := renderPolyMesh(g, m, width*superSample)
	if err != nil {
		return nil, err
	}
	return downsample(big, superSample), nil
}

func renderPolyMesh(g geometry.Geometry, m mesh.Mesh, width int) (*image.RGBA, error) {
	min, max := g.BoundingBox()
	f, err := NewFitter(min, max, width)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, f.Height()))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: rasterBG}, image.Point{}, draw.Src)

	for i := range m.Cells {
		cell := m.Cell(i)
		if len(cell) < 3 {
			continue
		}
		pts := make([]point, len(cell))
		for k, v := range cell {
			x, y := f.Tx(v)
			pts[k] = point{x, y}
		}
		fillPolygon(img, pts, rasterPolyFill)
	}
	for i := range m.Cells {
		cell := m.Cell(i)
		if len(cell) < 3 {
			continue
		}
		n := len(cell)
		for k := 0; k < n; k++ {
			x0, y0 := f.Tx(cell[k])
			x1, y1 := f.Tx(cell[(k+1)%n])
			drawLine(img, x0, y0, x1, y1, rasterPolyStroke)
		}
	}
	drawPolyline(img, f, g.Outer, rasterBoundary)
	for _, h := range g.Holes {
		drawPolyline(img, f, h, rasterHole)
	}
	return img, nil
}

type point struct{ x, y float64 }

// fillTriangle scan-converts a triangle with a flat color. It is a plain
// per-scanline edge intersection fill, not antialiased — good enough for a
// preview image, not for print output.
func fillTriangle(img *image.RGBA, p0, p1, p2 point, col color.RGBA) {
	fillPolygon(img, []point{p0, p1, p2}, col)
}

// fillPolygon scan-converts an arbitrary simple polygon (convex or not) with
// a flat color — the same per-scanline edge-intersection approach as
// fillTriangle, generalised from 3 fixed edges to however many the polygon
// has. Not antialiased on its own; MeshImage/PolyMeshImage rely on
// supersample+downsample for that, same as the triangle path.
func fillPolygon(img *image.RGBA, pts []point, col color.RGBA) {
	if len(pts) < 3 {
		return
	}
	yMin, yMax := pts[0].y, pts[0].y
	for _, p := range pts {
		yMin = math.Min(yMin, p.y)
		yMax = math.Max(yMax, p.y)
	}

	b := img.Bounds()
	y0, y1 := int(math.Ceil(yMin)), int(math.Floor(yMax))
	n := len(pts)
	for y := y0; y <= y1; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		fy := float64(y)
		var xs []float64
		for i := 0; i < n; i++ {
			if x, ok := edgeX(pts[i], pts[(i+1)%n], fy); ok {
				xs = append(xs, x)
			}
		}
		if len(xs) < 2 {
			continue
		}
		sort.Float64s(xs)
		// Fill every other span (even-odd rule) rather than just the outer
		// span: fillTriangle only ever saw 2 crossings so this reduces to the
		// old behaviour there, but a non-convex polygon can cross a scanline
		// more than twice.
		for i := 0; i+1 < len(xs); i += 2 {
			xStart, xEnd := int(math.Round(xs[i])), int(math.Round(xs[i+1]))
			if xStart < b.Min.X {
				xStart = b.Min.X
			}
			if xEnd >= b.Max.X {
				xEnd = b.Max.X - 1
			}
			for x := xStart; x <= xEnd; x++ {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

// edgeX returns the x where segment a-b crosses horizontal line y, if it does.
func edgeX(a, b point, y float64) (float64, bool) {
	if a.y == b.y {
		return 0, false
	}
	t := (y - a.y) / (b.y - a.y)
	if t < 0 || t > 1 {
		return 0, false
	}
	return a.x + t*(b.x-a.x), true
}

// drawLine plots a line with a basic DDA walk; thin (1px) but adequate for a
// wireframe overlay at preview resolution.
func drawLine(img *image.RGBA, x0, y0, x1, y1 float64, col color.RGBA) {
	dx, dy := x1-x0, y1-y0
	steps := math.Max(math.Abs(dx), math.Abs(dy))
	if steps < 1 {
		steps = 1
	}
	b := img.Bounds()
	n := int(steps)
	for i := 0; i <= n; i++ {
		t := float64(i) / steps
		x := int(math.Round(x0 + dx*t))
		y := int(math.Round(y0 + dy*t))
		if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
			img.SetRGBA(x, y, col)
		}
	}
}

func drawPolyline(img *image.RGBA, f Fitter, pl geometry.Polyline, col color.RGBA) {
	if len(pl) < 2 {
		return
	}
	for i := 1; i < len(pl); i++ {
		x0, y0 := f.Tx(pl[i-1])
		x1, y1 := f.Tx(pl[i])
		drawLine(img, x0, y0, x1, y1, col)
	}
	x0, y0 := f.Tx(pl[len(pl)-1])
	x1, y1 := f.Tx(pl[0])
	drawLine(img, x0, y0, x1, y1, col)
}

// downsample box-filters src down by factor: each output pixel is the
// average of the factor×factor block of source pixels behind it. This is
// where the antialiasing actually comes from — a hard edge at N× resolution
// blends into a soft gradient once averaged back down.
func downsample(src *image.RGBA, factor int) *image.RGBA {
	sb := src.Bounds()
	dw, dh := sb.Dx()/factor, sb.Dy()/factor
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	n := uint32(factor * factor)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var r, g, b, a uint32
			for sy := 0; sy < factor; sy++ {
				for sx := 0; sx < factor; sx++ {
					c := src.RGBAAt(sb.Min.X+x*factor+sx, sb.Min.Y+y*factor+sy)
					r += uint32(c.R)
					g += uint32(c.G)
					b += uint32(c.B)
					a += uint32(c.A)
				}
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: uint8(a / n)})
		}
	}
	return dst
}
