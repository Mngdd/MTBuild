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
