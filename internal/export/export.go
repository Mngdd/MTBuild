// Package export writes a mesh.Mesh to interchange formats so the result can be
// loaded into FEM/CFD tools or other viewers.
package export

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"MTBuild/internal/mesh"
)

// Write dispatches on the file extension of path: .obj, .json or .msh (Gmsh 2.2).
func Write(w io.Writer, m mesh.Mesh, path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".obj":
		return WriteOBJ(w, m)
	case ".json":
		return WriteJSON(w, m)
	case ".msh":
		return WriteGmsh(w, m)
	default:
		return fmt.Errorf("export: unsupported extension %q (use .obj, .json or .msh)", filepath.Ext(path))
	}
}

// WriteOBJ writes a Wavefront OBJ with z = 0.
func WriteOBJ(w io.Writer, m mesh.Mesh) error {
	b := bufio.NewWriter(w)
	fmt.Fprintln(b, "# MTBuild mesh")
	for _, v := range m.Vertices {
		fmt.Fprintf(b, "v %.9g %.9g 0\n", v.X, v.Y)
	}
	for _, t := range m.Triangles {
		fmt.Fprintf(b, "f %d %d %d\n", t[0]+1, t[1]+1, t[2]+1) // OBJ is 1-indexed
	}
	return b.Flush()
}

type jsonMesh struct {
	Vertices  [][2]float64 `json:"vertices"`
	Triangles [][3]int     `json:"triangles"`
}

// WriteJSON writes {"vertices": [[x,y],...], "triangles": [[i,j,k],...]}.
func WriteJSON(w io.Writer, m mesh.Mesh) error {
	out := jsonMesh{
		Vertices:  make([][2]float64, len(m.Vertices)),
		Triangles: m.Triangles,
	}
	for i, v := range m.Vertices {
		out.Vertices[i] = [2]float64{v.X, v.Y}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	return enc.Encode(out)
}

// WriteGmsh writes the legacy Gmsh 2.2 ASCII format (element type 2 = triangle).
func WriteGmsh(w io.Writer, m mesh.Mesh) error {
	b := bufio.NewWriter(w)
	fmt.Fprint(b, "$MeshFormat\n2.2 0 8\n$EndMeshFormat\n")
	fmt.Fprintf(b, "$Nodes\n%d\n", len(m.Vertices))
	for i, v := range m.Vertices {
		fmt.Fprintf(b, "%d %.9g %.9g 0\n", i+1, v.X, v.Y)
	}
	fmt.Fprint(b, "$EndNodes\n")
	fmt.Fprintf(b, "$Elements\n%d\n", len(m.Triangles))
	for i, t := range m.Triangles {
		// id type ntags tag1 tag2 n1 n2 n3
		fmt.Fprintf(b, "%d 2 2 0 1 %d %d %d\n", i+1, t[0]+1, t[1]+1, t[2]+1)
	}
	fmt.Fprint(b, "$EndElements\n")
	return b.Flush()
}
