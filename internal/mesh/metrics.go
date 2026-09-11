package mesh

import (
	"math"

	"MTBuild/internal/geometry"
)

// TODO: есть ли еще метрики? почитать код, впринципе с жпт это осознал
// QualityMetrics summarises how close a triangulation is to being composed of
// equilateral triangles. Reference figures from the source thesis: a good mesh
// has GAvg in roughly [0.85, 0.95].
type QualityMetrics struct {
	Triangles int

	GMin float64 // worst triangle quality, G = 2*sqrt(3)*S/(p*h)
	GAvg float64 // mean quality (G = 1 for an equilateral triangle)

	ThetaMinDeg float64 // smallest angle anywhere in the mesh, degrees
	ThetaAvgDeg float64 // mean of each triangle's smallest angle, degrees

	BadFraction float64 // fraction of triangles whose min angle < 30 deg

	// AngleHistogram buckets every triangle angle into 15-degree bins:
	// [0,15), [15,30), ... [165,180).
	AngleHistogram [12]int
}

// Evaluate computes quality metrics over the whole mesh. Degenerate triangles
// (zero area) are counted but contribute G = 0 and are skipped for angles.
func Evaluate(m Mesh) QualityMetrics {
	var q QualityMetrics
	q.Triangles = len(m.Triangles)
	if q.Triangles == 0 {
		return q
	}

	q.GMin = math.Inf(1)
	q.ThetaMinDeg = math.Inf(1)
	var gSum, thetaMinSum float64
	bad := 0

	for i := range m.Triangles {
		a, b, c := m.Tri(i)
		g := triQuality(a, b, c)
		gSum += g
		if g < q.GMin {
			q.GMin = g
		}

		angs := triAnglesDeg(a, b, c)
		triMin := math.Inf(1)
		for _, ang := range angs {
			if ang < triMin {
				triMin = ang
			}
			if ang < q.ThetaMinDeg {
				q.ThetaMinDeg = ang
			}
			bucket := int(ang / 15)
			if bucket < 0 {
				bucket = 0
			}
			if bucket > 11 {
				bucket = 11
			}
			q.AngleHistogram[bucket]++
		}
		if !math.IsInf(triMin, 1) {
			thetaMinSum += triMin
			if triMin < 30 {
				bad++
			}
		}
	}

	n := float64(q.Triangles)
	q.GAvg = gSum / n
	q.ThetaAvgDeg = thetaMinSum / n
	q.BadFraction = float64(bad) / n
	if math.IsInf(q.GMin, 1) {
		q.GMin = 0
	}
	if math.IsInf(q.ThetaMinDeg, 1) {
		q.ThetaMinDeg = 0
	}
	return q
}

// triQuality returns G = 2*sqrt(3)*S / (p*h): area S, semi-perimeter p, longest
// edge h. G = 1 for an equilateral triangle, G -> 0 as it degenerates.
func triQuality(a, b, c geometry.Point) float64 {
	ab := a.Dist(b)
	bc := b.Dist(c)
	ca := c.Dist(a)
	s := triArea(a, b, c)
	p := (ab + bc + ca) / 2
	h := math.Max(ab, math.Max(bc, ca))
	if p == 0 || h == 0 {
		return 0
	}
	return 2 * math.Sqrt(3) * s / (p * h)
}

func triArea(a, b, c geometry.Point) float64 {
	return math.Abs((b.X-a.X)*(c.Y-a.Y)-(c.X-a.X)*(b.Y-a.Y)) / 2
}

// triAnglesDeg returns the three interior angles in degrees.
func triAnglesDeg(a, b, c geometry.Point) [3]float64 {
	la := b.Dist(c) // side opposite a
	lb := c.Dist(a)
	lc := a.Dist(b)
	ang := func(opp, s1, s2 float64) float64 {
		if s1 == 0 || s2 == 0 {
			return 0
		}
		cosv := (s1*s1 + s2*s2 - opp*opp) / (2 * s1 * s2)
		cosv = math.Max(-1, math.Min(1, cosv))
		return math.Acos(cosv) * 180 / math.Pi
	}
	return [3]float64{
		ang(la, lb, lc),
		ang(lb, lc, la),
		ang(lc, la, lb),
	}
}
