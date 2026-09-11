package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
)

func sample() mesh.Mesh {
	return mesh.Mesh{
		Vertices:  []geometry.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}},
		Triangles: [][3]int{{0, 1, 2}, {1, 3, 2}},
	}
}

func TestWriteOBJ(t *testing.T) {
	var b bytes.Buffer
	if err := WriteOBJ(&b, sample()); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if strings.Count(s, "\nv ") != 4 {
		t.Errorf("want 4 vertex lines:\n%s", s)
	}
	if !strings.Contains(s, "f 1 2 3") || !strings.Contains(s, "f 2 4 3") {
		t.Errorf("faces must be 1-indexed:\n%s", s)
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	var b bytes.Buffer
	if err := WriteJSON(&b, sample()); err != nil {
		t.Fatal(err)
	}
	var out jsonMesh
	if err := json.Unmarshal(b.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Vertices) != 4 || len(out.Triangles) != 2 {
		t.Errorf("got %d verts, %d tris", len(out.Vertices), len(out.Triangles))
	}
}

func TestWriteGmsh(t *testing.T) {
	var b bytes.Buffer
	if err := WriteGmsh(&b, sample()); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{"$MeshFormat", "$Nodes\n4", "$Elements\n2", " 2 2 0 1 "} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestWriteDispatch(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, sample(), "mesh.stl"); err == nil {
		t.Error("expected error for unsupported extension")
	}
}
