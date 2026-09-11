package sph

import "math"

// Cubic spline smoothing kernel (2D), following the thesis:
//
//	s = r/h
//	W = alpha*(s^3 - 6s + 6)   0 <= s < 1
//	  = alpha*(2 - s)^3        1 <= s < 2
//	  = 0                      s >= 2
//	alpha = 1 / (3*pi*h^2)
//
// Interaction cut-off is r_c = 2h.

func kernelAlpha(h float64) float64 { return 1.0 / (3.0 * math.Pi * h * h) }

// kernelW evaluates W(r, h).
func kernelW(r, h float64) float64 {
	s := r / h
	switch {
	case s < 1:
		return kernelAlpha(h) * (s*s*s - 6*s + 6)
	case s < 2:
		d := 2 - s
		return kernelAlpha(h) * d * d * d
	default:
		return 0
	}
}

// kernelDWDR evaluates dW/dr at (r, h). Negative over (0, 2h): the kernel
// decreases with distance, which makes the pressure term repulsive.
func kernelDWDR(r, h float64) float64 {
	s := r / h
	switch {
	case s < 1:
		return kernelAlpha(h) / h * (3*s*s - 6)
	case s < 2:
		d := 2 - s
		return kernelAlpha(h) / h * (-3 * d * d)
	default:
		return 0
	}
}
