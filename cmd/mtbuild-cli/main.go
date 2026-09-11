// Command mtbuild-cli is the headless entry point for the mesh generator:
// load a geometry (DXF file or a built-in preset), optionally run a meshing
// algorithm, and write an SVG plus quality metrics.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"MTBuild/internal/export"
	"MTBuild/internal/geometry"
	"MTBuild/internal/mesh"
	"MTBuild/internal/mesher"
	"MTBuild/internal/viz"

	_ "MTBuild/internal/mesher/grid"    // register the "grid" algorithm
	_ "MTBuild/internal/mesher/sphquad" // register the "sph-quadtree" algorithm
)

func main() {
	var (
		geomArg = flag.String("geom", "ellipse", "geometry: a .dxf path, or a preset name (ellipse, zalesak)")
		algoArg = flag.String("algo", "", "meshing algorithm to run; empty just loads the geometry")
		outSVG  = flag.String("svg", "", "write an SVG (geometry, or the mesh if -algo is set) to this path")
		outMesh = flag.String("out", "", "write the mesh to this path (.obj, .json or .msh)")
		width   = flag.Int("width", 900, "SVG width in pixels")
		samples = flag.Int("spline-samples", geometry.SplineSamples, "segments per spline when tessellating DXF")
		params  paramFlag
	)
	flag.Var(&params, "param", "algorithm parameter as key=value (repeatable)")
	flag.Parse()

	geometry.SplineSamples = *samples

	if *algoArg == "" && len(mesher.List()) > 0 {
		fmt.Fprintln(os.Stderr, "available algorithms:", strings.Join(mesher.List(), ", "))
	}

	g, err := loadGeometry(*geomArg)
	if err != nil {
		fail(err)
	}
	fmt.Println(viz.Describe(g))

	if *algoArg == "" {
		if *outSVG != "" {
			writeFile(*outSVG, func(f *os.File) error { return viz.GeometrySVG(f, g, *width) })
		}
		return
	}

	m, metrics, err := runAlgo(*algoArg, g, mesher.Params(params))
	if err != nil {
		fail(err)
	}
	printMetrics(metrics)
	if *outSVG != "" {
		writeFile(*outSVG, func(f *os.File) error { return viz.MeshSVG(f, g, m, *width) })
	}
	if *outMesh != "" {
		writeFile(*outMesh, func(f *os.File) error { return export.Write(f, m, *outMesh) })
	}
}

func runAlgo(name string, g geometry.Geometry, p mesher.Params) (mesh.Mesh, mesh.QualityMetrics, error) {
	algo, err := mesher.New(name)
	if err != nil {
		return mesh.Mesh{}, mesh.QualityMetrics{}, err
	}
	fmt.Fprintf(os.Stderr, "running %q with params %v\n", algo.Name(), map[string]float64(p))
	return algo.Generate(context.Background(), g, p, stderrSink{})
}

func loadGeometry(arg string) (geometry.Geometry, error) {
	if _, err := os.Stat(arg); err == nil {
		return geometry.Load(arg)
	}
	return geometry.Preset(arg)
}

// stderrSink prints progress and snapshot summaries to stderr.
type stderrSink struct{}

func (stderrSink) Progress(stage string, frac float64) {
	fmt.Fprintf(os.Stderr, "  [%-14s] %3.0f%%\n", stage, frac*100)
}
func (stderrSink) Snapshot(fr mesher.Frame) {
	tris := 0
	if fr.Mesh != nil {
		tris = len(fr.Mesh.Triangles)
	}
	fmt.Fprintf(os.Stderr, "  snapshot %-14s iter=%d points=%d tris=%d\n", fr.Stage, fr.Iter, len(fr.Particles), tris)
}

func printMetrics(q mesh.QualityMetrics) {
	fmt.Printf("triangles=%d  G_avg=%.3f  G_min=%.3f  theta_min=%.1f deg  theta_avg=%.1f deg  bad(<30)=%.1f%%\n",
		q.Triangles, q.GAvg, q.GMin, q.ThetaMinDeg, q.ThetaAvgDeg, q.BadFraction*100)
	fmt.Print("angle histogram (15 deg bins): ")
	for i, c := range q.AngleHistogram {
		fmt.Printf("%d-%d:%d ", i*15, i*15+15, c)
	}
	fmt.Println()
}

func writeFile(path string, render func(*os.File) error) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	if err := render(f); err != nil {
		fail(err)
	}
	fmt.Println("wrote", path)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// paramFlag collects repeated -param key=value flags.
type paramFlag map[string]float64

func (p *paramFlag) String() string { return fmt.Sprintf("%v", map[string]float64(*p)) }
func (p *paramFlag) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("want key=value, got %q", s)
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return fmt.Errorf("param %q: %w", k, err)
	}
	if *p == nil {
		*p = paramFlag{}
	}
	(*p)[strings.TrimSpace(k)] = f
	return nil
}
