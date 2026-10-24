package quadtree

import (
	"math"
	"testing"

	"MTBuild/internal/geometry"
)

func unitSquare() geometry.Geometry {
	g := geometry.Geometry{Outer: geometry.Polyline{
		{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: 0.5, Y: 0.5}, {X: -0.5, Y: 0.5},
	}}
	n, _ := g.Normalized()
	return n
}

func TestBuild_RefinesNearBoundary(t *testing.T) {
	tr := Build(unitSquare(), Options{MaxDepth: 8, HNear: 0.02, HFar: 0.3, Band: 0.25})

	var nearMax, farMax float64
	tr.EachLeaf(func(n *Node) {
		d := distToBoundary(unitSquare(), n.CX, n.CY)
		if d < 0.03 {
			nearMax = math.Max(nearMax, n.Size)
		}
		if d > 0.4 {
			farMax = math.Max(farMax, n.Size)
		}
	})
	if nearMax == 0 || nearMax > 0.05 {
		t.Errorf("near-boundary leaves too coarse: max size %.3f", nearMax)
	}
	if farMax <= nearMax {
		t.Errorf("expected coarser cells away from boundary (near=%.3f far=%.3f)", nearMax, farMax)
	}
}

func TestBalance_2to1(t *testing.T) {
	tr := Build(unitSquare(), Options{MaxDepth: 9, HNear: 0.01, HFar: 0.5, Band: 0.2})
	tr.EachLeaf(func(n *Node) {
		for _, nb := range tr.edgeNeighbours(n) {
			ratio := math.Max(n.Size, nb.Size) / math.Min(n.Size, nb.Size)
			if ratio > 2+1e-6 {
				t.Errorf("2:1 violation: %.4f vs %.4f (ratio %.2f)", n.Size, nb.Size, ratio)
			}
		}
	})
}

func TestLeafAt(t *testing.T) {
	tr := Build(unitSquare(), Options{MaxDepth: 6, HNear: 0.05, HFar: 0.3, Band: 0.2})
	leaf := tr.LeafAt(0, 0)
	if leaf == nil || !leaf.Leaf() {
		t.Fatal("expected a leaf at the centre")
	}
	if math.Abs(leaf.CX) > leaf.Size || math.Abs(leaf.CY) > leaf.Size {
		t.Errorf("leaf %v does not contain the origin", *leaf)
	}
	if tr.LeafAt(100, 100) != nil {
		t.Error("point outside root should return nil")
	}
}

func TestSampleHt(t *testing.T) {
	tr := Build(unitSquare(), Options{MaxDepth: 8, HNear: 0.02, HFar: 0.3, Band: 0.25})

	if _, ok := tr.SampleHt(100, 100); ok {
		t.Error("expected ok=false outside the root")
	}

	// A uniform field must interpolate back to the same constant everywhere,
	// including at leaf centres and near T-junctions between differently
	// sized leaves.
	tr.EachLeaf(func(n *Node) { n.Ht = 7 })
	for _, p := range [][2]float64{{0, 0}, {0.1, 0.2}, {-0.3, 0.05}, {0.45, -0.45}, {0.02, 0.49}} {
		v, ok := tr.SampleHt(p[0], p[1])
		if !ok {
			t.Fatalf("SampleHt(%v) unexpectedly outside root", p)
		}
		if math.Abs(v-7) > 1e-9 {
			t.Errorf("uniform field: SampleHt(%v) = %.6f, want 7", p, v)
		}
	}

	// A non-uniform field must stay within the range of Ht values actually
	// present in the tree (no overshoot from the interpolation).
	tr.EachLeaf(func(n *Node) { n.Ht = n.Size })
	minHt, maxHt := math.Inf(1), math.Inf(-1)
	tr.EachLeaf(func(n *Node) {
		minHt = math.Min(minHt, n.Ht)
		maxHt = math.Max(maxHt, n.Ht)
	})
	for _, p := range [][2]float64{{0.1, 0.2}, {-0.3, 0.05}, {0.45, -0.45}} {
		v, ok := tr.SampleHt(p[0], p[1])
		if !ok {
			t.Fatalf("SampleHt(%v) unexpectedly outside root", p)
		}
		if v < minHt-1e-9 || v > maxHt+1e-9 {
			t.Errorf("SampleHt(%v) = %.4f outside [%.4f,%.4f]", p, v, minHt, maxHt)
		}
	}
}
