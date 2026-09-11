package geometry

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Load читает файл геомы, с учетом расширения. пока что поддерживаем .dxf, НОВЫЕ ФОРМАТЫ ПИСАТЬ ТУТ
func Load(path string) (Geometry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Geometry{}, err
	}
	defer f.Close()

	switch strings.ToLower(filepath.Ext(path)) {
	case ".dxf":
		return LoadDXF(f)
	default:
		return Geometry{}, fmt.Errorf("geometry: unsupported file extension %q", filepath.Ext(path))
	}
}

// SplineSamples на сколько вершин сплайн/кривая режется
// TODO: очень тупой хардкод и мне НЕ НРАВИТСЯ, но это mvp так что фиксим потом
var SplineSamples = 200

// dxfPair is one (group code, value) record from an ASCII DXF file.
type dxfPair struct {
	code int
	val  string
}

func (p dxfPair) f() float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(p.val), 64); return v }
func (p dxfPair) i() int     { v, _ := strconv.Atoi(strings.TrimSpace(p.val)); return v }

// я с гпт поосознавал код идейно и поисправлял всякие моменты
// но сам код не читал дальше чем объявление LoadDXF...
// TODO: прочесть самому код.

// LoadDXF parses the ENTITIES section of an ASCII DXF and returns the contours
// it can reconstruct. Supported entities: LWPOLYLINE, SPLINE, LINE.
//
// When several closed contours are found, the one with the largest absolute
// area becomes Outer and the rest become Holes.
func LoadDXF(r io.Reader) (Geometry, error) {
	pairs, err := readDXFPairs(r)
	if err != nil {
		return Geometry{}, err
	}

	// Narrow to the ENTITIES section.
	start, end := -1, len(pairs)
	for i := 0; i+1 < len(pairs); i++ {
		if pairs[i].code == 0 && pairs[i].val == "SECTION" && pairs[i+1].code == 2 && pairs[i+1].val == "ENTITIES" {
			start = i + 2
		} else if start >= 0 && pairs[i].code == 0 && pairs[i].val == "ENDSEC" {
			end = i
			break
		}
	}
	if start < 0 {
		return Geometry{}, fmt.Errorf("dxf: no ENTITIES section")
	}

	var closed []Polyline // closed contours (polylines, splines)
	var segs [][2]Point   // loose LINE segments, chained later

	i := start
	for i < end {
		if pairs[i].code != 0 {
			i++
			continue
		}
		etype := pairs[i].val
		// Collect this entity's pairs up to the next code-0.
		j := i + 1
		for j < end && pairs[j].code != 0 {
			j++
		}
		body := pairs[i+1 : j]

		switch etype {
		case "LWPOLYLINE":
			pl, isClosed := parseLWPolyline(body)
			if len(pl) >= 2 {
				if isClosed {
					closed = append(closed, pl)
				} else {
					for k := 0; k+1 < len(pl); k++ {
						segs = append(segs, [2]Point{pl[k], pl[k+1]})
					}
				}
			}
		case "SPLINE":
			if pl := parseSpline(body); len(pl) >= 3 {
				closed = append(closed, pl)
			}
		case "LINE":
			if a, b, ok := parseLine(body); ok {
				segs = append(segs, [2]Point{a, b})
			}
		}
		i = j
	}

	// Chain loose segments into closed loops.
	for _, loop := range chainSegments(segs) {
		if len(loop) >= 3 {
			closed = append(closed, loop)
		}
	}

	if len(closed) == 0 {
		return Geometry{}, fmt.Errorf("dxf: no closed contour found (supported: LWPOLYLINE, SPLINE, chained LINE)")
	}

	// Largest by |area| is the outer boundary.
	sort.SliceStable(closed, func(a, b int) bool {
		return math.Abs(closed[a].SignedArea()) > math.Abs(closed[b].SignedArea())
	})
	g := Geometry{Outer: closed[0], Holes: closed[1:]}
	return g.Normalized()
}

// ------- тут чета унылое -------
func readDXFPairs(r io.Reader) ([]dxfPair, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(lines)%2 != 0 {
		lines = lines[:len(lines)-1] // tolerate a trailing stray line
	}
	pairs := make([]dxfPair, 0, len(lines)/2)
	for k := 0; k+1 < len(lines); k += 2 {
		code, err := strconv.Atoi(strings.TrimSpace(lines[k]))
		if err != nil {
			return nil, fmt.Errorf("dxf: bad group code %q at line %d", lines[k], k+1)
		}
		pairs = append(pairs, dxfPair{code: code, val: strings.TrimSpace(lines[k+1])})
	}
	return pairs, nil
}

func parseLWPolyline(body []dxfPair) (pl Polyline, closed bool) {
	var x float64
	haveX := false
	for _, p := range body {
		switch p.code {
		case 70:
			closed = p.i()&1 == 1
		case 10:
			x, haveX = p.f(), true
		case 20:
			if haveX {
				pl = append(pl, Point{x, p.f()})
				haveX = false
			}
		}
	}
	return pl, closed
}

func parseLine(body []dxfPair) (a, b Point, ok bool) {
	var got int
	for _, p := range body {
		switch p.code {
		case 10:
			a.X, got = p.f(), got|1
		case 20:
			a.Y, got = p.f(), got|2
		case 11:
			b.X, got = p.f(), got|4
		case 21:
			b.Y, got = p.f(), got|8
		}
	}
	return a, b, got == 15
}

// -------------------------------

// parseSpline reads a (possibly rational) B-spline entity and evaluates it into
// a polyline. Weights (code 41) are ignored — adequate for MVP contours.
func parseSpline(body []dxfPair) Polyline {
	var (
		degree int
		knots  []float64
		ctrl   []Point
		cx     float64
		haveCX bool
		flags  int
	)
	for _, p := range body {
		switch p.code {
		case 70:
			flags = p.i()
		case 71:
			degree = p.i()
		case 40:
			knots = append(knots, p.f())
		case 10:
			cx, haveCX = p.f(), true
		case 20:
			if haveCX {
				ctrl = append(ctrl, Point{cx, p.f()})
				haveCX = false
			}
		}
	}
	if degree < 1 || len(ctrl) < degree+1 {
		return nil
	}
	// A clamped knot vector has length len(ctrl)+degree+1. If it doesn't match
	// (e.g. periodic spline), fall back to a uniform clamped vector.
	if len(knots) != len(ctrl)+degree+1 {
		knots = clampedUniformKnots(len(ctrl), degree)
	}

	closed := flags&1 == 1
	umin, umax := knots[degree], knots[len(knots)-1-degree]
	n := SplineSamples
	pts := make(Polyline, 0, n)
	last := n
	if closed {
		last = n - 1 // don't duplicate the seam point
	}
	for k := 0; k <= last; k++ {
		u := umin + (umax-umin)*float64(k)/float64(n)
		pts = append(pts, deBoor(u, degree, knots, ctrl))
	}
	return pts
}

func clampedUniformKnots(nCtrl, degree int) []float64 {
	m := nCtrl + degree + 1
	kn := make([]float64, m)
	inner := nCtrl - degree // number of unique interior spans + 1
	for i := 0; i < m; i++ {
		switch {
		case i <= degree:
			kn[i] = 0
		case i >= m-1-degree:
			kn[i] = 1
		default:
			kn[i] = float64(i-degree) / float64(inner)
		}
	}
	return kn
}

// deBoor вычисляет одну точку на кривой сплайна при параметре u ∈ [0,1]
func deBoor(u float64, p int, knots []float64, ctrl []Point) Point {
	// Locate knot span index k with knots[k] <= u < knots[k+1], capped so the
	// control-point window d[j] = ctrl[j+k-p] stays in range (k <= n).
	k := p
	for k < len(ctrl)-1 && u >= knots[k+1] {
		k++
	}
	d := make([]Point, p+1)
	for j := 0; j <= p; j++ {
		d[j] = ctrl[j+k-p]
	}
	for r := 1; r <= p; r++ {
		for j := p; j >= r; j-- {
			den := knots[j+1+k-r] - knots[j+k-p]
			a := 0.0
			if den != 0 {
				a = (u - knots[j+k-p]) / den
			}
			d[j] = Point{
				X: (1-a)*d[j-1].X + a*d[j].X,
				Y: (1-a)*d[j-1].Y + a*d[j].Y,
			}
		}
	}
	return d[p]
}

// chainSegments stitches loose segments into closed loops by matching endpoints.
func chainSegments(segs [][2]Point) []Polyline {
	const eps = 1e-6
	near := func(a, b Point) bool { return a.Dist(b) < eps }

	used := make([]bool, len(segs))
	var loops []Polyline
	for i := range segs {
		if used[i] {
			continue
		}
		used[i] = true
		loop := Polyline{segs[i][0], segs[i][1]}
		for {
			tail := loop[len(loop)-1]
			progressed := false
			for k := range segs {
				if used[k] {
					continue
				}
				switch {
				case near(segs[k][0], tail):
					loop = append(loop, segs[k][1])
				case near(segs[k][1], tail):
					loop = append(loop, segs[k][0])
				default:
					continue
				}
				used[k] = true
				progressed = true
				break
			}
			if !progressed {
				break
			}
		}
		if near(loop[0], loop[len(loop)-1]) {
			loops = append(loops, loop[:len(loop)-1])
		}
	}
	return loops
}
