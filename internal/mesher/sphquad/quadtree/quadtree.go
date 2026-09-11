// Package quadtree builds the adaptive Cartesian background grid described in
// the thesis: a square root cell recursively split into four, refined near the
// input boundary and 2:1 balanced. Scalar fields (phi, h_t) are stored per
// leaf and sampled piecewise-constant.
package quadtree

import (
	"math"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesher/sphquad/field"
)

// Node is one cell. A node is a leaf iff Children[0] == nil.
type Node struct {
	Parent   *Node
	Children [4]*Node
	Depth    int
	I, J     int // cell index at this depth (origin at the tree's SW corner)

	CX, CY float64 // cell centre in world coordinates
	Size   float64 // side length

	// Per-leaf scalar fields, filled after the tree is built.
	Phi float64 // signed distance to the boundary (>0 inside)
	Ht  float64 // target element size at this cell
}

// Leaf reports whether n has no children.
func (n *Node) Leaf() bool { return n.Children[0] == nil }

// Options controls refinement.
type Options struct {
	MaxDepth int     // hard recursion cap
	HNear    float64 // desired cell size at the boundary
	HFar     float64 // desired cell size far from the boundary
	Band     float64 // distance out to which cells are refined toward HNear
}

// Tree is the built background grid.
type Tree struct {
	Root     *Node
	OriginX  float64 // SW corner of the root cell
	OriginY  float64
	RootSize float64
	geom     geometry.Geometry
}

// Build constructs the background grid for g. The root is a square centred on
// the geometry's bounding box, enlarged so the boundary sits comfortably
// inside it.
func Build(g geometry.Geometry, opt Options) *Tree {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 10
	}
	min, max := g.BoundingBox()
	cx, cy := (min.X+max.X)/2, (min.Y+max.Y)/2
	span := math.Max(max.X-min.X, max.Y-min.Y)
	root := span * 1.5 // padding so exterior cells exist for sign/interp

	t := &Tree{
		OriginX:  cx - root/2,
		OriginY:  cy - root/2,
		RootSize: root,
		geom:     g,
	}
	t.Root = &Node{CX: cx, CY: cy, Size: root}
	t.refine(t.Root, opt)
	t.balance(opt)
	return t
}

func (t *Tree) refine(n *Node, opt Options) {
	if n.Depth >= opt.MaxDepth {
		return
	}
	d := distToBoundary(t.geom, n.CX, n.CY)
	// Only cells near the boundary are worth refining.
	if d-n.Size*math.Sqrt2/2 > opt.Band {
		return
	}
	if n.Size <= hRequired(d, opt) {
		return
	}
	t.split(n)
	for _, c := range n.Children {
		t.refine(c, opt)
	}
}

// hRequired is the target cell size at distance d from the boundary: HNear at
// the boundary, ramping linearly up to HFar at the band edge.
func hRequired(d float64, opt Options) float64 {
	if d >= opt.Band || opt.Band <= 0 {
		return opt.HFar
	}
	frac := d / opt.Band
	return opt.HNear + (opt.HFar-opt.HNear)*frac
}

// quadrant offsets: SW, SE, NE, NW.
var dq = [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
var di = [4]int{0, 1, 1, 0}
var dj = [4]int{0, 0, 1, 1}

func (t *Tree) split(n *Node) {
	h := n.Size / 2
	for k := 0; k < 4; k++ {
		n.Children[k] = &Node{
			Parent: n,
			Depth:  n.Depth + 1,
			I:      2*n.I + di[k],
			J:      2*n.J + dj[k],
			CX:     n.CX + dq[k][0]*h/2,
			CY:     n.CY + dq[k][1]*h/2,
			Size:   h,
		}
	}
}

// balance enforces the 2:1 rule: no leaf may border a leaf more than twice its
// size. Oversized neighbours are split until the tree is balanced.
func (t *Tree) balance(opt Options) {
	for pass := 0; pass < opt.MaxDepth+2; pass++ {
		changed := false
		var leaves []*Node
		t.eachLeaf(func(n *Node) { leaves = append(leaves, n) })
		for _, n := range leaves {
			if !n.Leaf() {
				continue
			}
			for _, nb := range t.edgeNeighbours(n) {
				if nb.Leaf() && nb.Size > 2*n.Size+1e-12 {
					t.split(nb)
					changed = true
				}
			}
		}
		if !changed {
			return
		}
	}
}

// edgeNeighbours returns the leaves bordering n's four edges (one per edge,
// possibly larger than n).
func (t *Tree) edgeNeighbours(n *Node) []*Node {
	eps := n.Size * 0.01
	probes := [4][2]float64{
		{n.CX - n.Size/2 - eps, n.CY}, // W
		{n.CX + n.Size/2 + eps, n.CY}, // E
		{n.CX, n.CY - n.Size/2 - eps}, // S
		{n.CX, n.CY + n.Size/2 + eps}, // N
	}
	var out []*Node
	for _, p := range probes {
		if leaf := t.LeafAt(p[0], p[1]); leaf != nil && leaf != n {
			out = append(out, leaf)
		}
	}
	return out
}

// LeafAt returns the leaf containing (x,y), or nil if outside the root.
func (t *Tree) LeafAt(x, y float64) *Node {
	n := t.Root
	if x < n.CX-n.Size/2 || x > n.CX+n.Size/2 || y < n.CY-n.Size/2 || y > n.CY+n.Size/2 {
		return nil
	}
	for !n.Leaf() {
		k := 0
		if x >= n.CX {
			k |= 1
		}
		if y >= n.CY {
			k |= 2
		}
		// map (right?,top?) bits to SW,SE,NE,NW order
		switch {
		case k == 0:
			n = n.Children[0] // SW
		case k == 1:
			n = n.Children[1] // SE
		case k == 3:
			n = n.Children[2] // NE
		case k == 2:
			n = n.Children[3] // NW
		}
	}
	return n
}

// EachLeaf calls fn for every leaf.
func (t *Tree) EachLeaf(fn func(*Node)) { t.eachLeaf(fn) }

func (t *Tree) eachLeaf(fn func(*Node)) {
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Leaf() {
			fn(n)
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t.Root)
}

// distToBoundary is the unsigned distance from (x,y) to the nearest boundary
// segment (outer or hole).
func distToBoundary(g geometry.Geometry, x, y float64) float64 {
	return field.BoundaryDistance(g, geometry.Point{X: x, Y: y})
}
