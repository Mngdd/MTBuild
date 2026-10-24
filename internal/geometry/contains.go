package geometry

import "math"

// ContainsPoint проверка принадлежности точки многоугольнику методом луча (чет снаружи, нечет внутри)
// TODO: вроде звучит ок но стоит слопокод проверить
func (pl Polyline) ContainsPoint(p Point) bool {
	inside := false
	n := len(pl)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		pi, pj := pl[i], pl[j]
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			xCross := pi.X + (p.Y-pi.Y)/(pj.Y-pi.Y)*(pj.X-pi.X)
			if p.X < xCross {
				inside = !inside
			}
		}
	}
	return inside
}

// Contains проверяем лежит ли точка в ДОМЕНЕ (с учетом дырок и аутера)
func (g Geometry) Contains(p Point) bool {
	if !g.Outer.ContainsPoint(p) {
		return false
	}
	for _, h := range g.Holes {
		if h.ContainsPoint(p) {
			return false
		}
	}
	return true
}

// Nearest возвращает ближайшую к p точку на границе домена (Outer или любая
// из Holes) — снэп точки, вылетевшей за домен, обратно на его край. Сейчас
// используется только в mesher/charge. Похожий перебор "точка -> ближайшая
// точка на отрезке" своими копиями живёт ещё в mesher/grid (distToSegment),
// sphquad/field (pointSeg) и sph/contour.go (ArcLengthOf) — на этот хелпер
// они не переведены.
func (g Geometry) Nearest(p Point) Point {
	best := g.Outer.nearestOn(p)
	bd := p.Dist(best)
	for _, h := range g.Holes {
		if c := h.nearestOn(p); p.Dist(c) < bd {
			best, bd = c, p.Dist(c)
		}
	}
	return best
}

// nearestOn возвращает ближайшую к p точку на замкнутой ломаной pl.
func (pl Polyline) nearestOn(p Point) Point {
	best := pl[0]
	bd := math.Inf(1)
	n := len(pl)
	for i := 0; i < n; i++ {
		a := pl[i]
		b := pl[(i+1)%n]
		dx, dy := b.X-a.X, b.Y-a.Y
		l2 := dx*dx + dy*dy
		t := 0.0
		if l2 > 0 {
			t = ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
			if t < 0 {
				t = 0
			}
			if t > 1 {
				t = 1
			}
		}
		c := Point{X: a.X + t*dx, Y: a.Y + t*dy}
		if d := p.Dist(c); d < bd {
			best, bd = c, d
		}
	}
	return best
}
