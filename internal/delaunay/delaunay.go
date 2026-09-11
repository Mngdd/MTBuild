// Package delaunay wraps a third-party Delaunay triangulator behind a small
// API in our own types, so algorithms don't depend on the library directly and
// it can be swapped later.
package delaunay

import (
	"fmt"

	ext "github.com/fogleman/delaunay"

	"MTBuild/internal/geometry"
)

// Triangulate returns the Delaunay triangulation of pts as index triples into
// pts. Fewer than three points yields an empty result.
func Triangulate(pts []geometry.Point) ([][3]int, error) {
	if len(pts) < 3 {
		return nil, nil
	}
	in := make([]ext.Point, len(pts))
	for i, p := range pts {
		in[i] = ext.Point{X: p.X, Y: p.Y}
	}
	tri, err := ext.Triangulate(in)
	if err != nil {
		return nil, fmt.Errorf("delaunay: %w", err)
	}
	out := make([][3]int, 0, len(tri.Triangles)/3)
	for i := 0; i+2 < len(tri.Triangles); i += 3 {
		out = append(out, [3]int{tri.Triangles[i], tri.Triangles[i+1], tri.Triangles[i+2]})
	}
	return out, nil
}
