package geometry

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
