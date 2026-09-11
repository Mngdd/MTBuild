package sph

import (
	"math"
	"testing"
)

// The kernel must integrate to 1 over the plane: 2*pi * int_0^{2h} W(r,h) r dr = 1.
func TestKernelNormalisation(t *testing.T) {
	const h = 0.7
	const n = 200000
	rc := 2 * h
	var sum float64
	dr := rc / n
	for k := 0; k < n; k++ {
		r := (float64(k) + 0.5) * dr
		sum += kernelW(r, h) * r * dr
	}
	got := 2 * math.Pi * sum
	if math.Abs(got-1) > 1e-3 {
		t.Errorf("kernel integral = %.5f, want 1", got)
	}
}

func TestKernelDerivativeSign(t *testing.T) {
	const h = 1.0
	for _, r := range []float64{0.2, 0.5, 0.9, 1.1, 1.5, 1.9} {
		if kernelDWDR(r, h) > 0 {
			t.Errorf("dW/dr(%.1f) = %v, expected <= 0", r, kernelDWDR(r, h))
		}
	}
	if kernelDWDR(2.5, h) != 0 {
		t.Errorf("kernel should vanish beyond 2h")
	}
}

func TestKernelDerivativeMatchesFiniteDiff(t *testing.T) {
	const h = 0.9
	const eps = 1e-6
	for _, r := range []float64{0.3, 0.8, 1.2, 1.7} {
		fd := (kernelW(r+eps, h) - kernelW(r-eps, h)) / (2 * eps)
		if math.Abs(fd-kernelDWDR(r, h)) > 1e-4 {
			t.Errorf("r=%.1f: analytic %.6f vs fd %.6f", r, kernelDWDR(r, h), fd)
		}
	}
}
