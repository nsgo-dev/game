//go:build !release

package navbake3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai/navbake3d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDebugLines(t *testing.T) {
	f, err := navbake3d.Parse(readRamp(t))
	if err != nil {
		t.Fatal(err)
	}
	m := &f.Meshes[0]
	// The two quads share one edge: 7 outline edges, then the 4 spokes of the first quad's detail and the diagonal of the second.
	outlines := m.DebugLines(true)
	if len(outlines) != 7 {
		t.Fatalf("%d outline edges, want 7", len(outlines))
	}
	all := m.DebugLines(false)
	if len(all) != 7+4+1 {
		t.Fatalf("%d lines, want 12", len(all))
	}
	if all[7][0] == all[7][1] {
		t.Fatal("a degenerate line")
	}
}

func TestDrawDebug(t *testing.T) {
	f, err := navbake3d.Parse(readRamp(t))
	if err != nil {
		t.Fatal(err)
	}
	m := &f.Meshes[0]
	view := math3d.LookAt(math3d.Vec3{X: 4, Y: 8, Z: 12}, math3d.Vec3{X: 4, Z: 2}, math3d.Vec3{Y: 1})
	proj := math3d.Perspective(math3d.DegToRad(60), 4.0/3, 0.1, 100)
	vp := proj.Mul(view)
	viewport := core.Bounds{Size: core.Size{Width: 400, Height: 300}}
	style := navbake3d.DebugStyle{PolygonColor: core.RGB(0xFF0000), LineWidth: 2}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "nav",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Draw: func(d *core.Drawing, _ core.Size) {
				navbake3d.DrawDebug(d, vp, viewport, m, style)
				navbake3d.DrawDebug(nil, vp, viewport, m, style)
				navbake3d.DrawDebug(d, vp, viewport, nil, style)
			},
		}
	})
	ops := app.Find("nav").Ops
	strokes, red, depth := 0, 0, 0
	for _, op := range ops {
		for _, v := range []float32{op.X, op.Y, op.X1, op.Y1} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("non-finite coordinate in %v", op)
			}
		}
		switch op.Kind {
		case core.DrawStroke:
			strokes++
		case core.DrawStrokeColor:
			if op.Color == core.RGB(0xFF0000) {
				red++
			}
		case core.DrawSave:
			depth++
		case core.DrawRestore:
			depth--
		}
	}
	if strokes != 12 || red != 7 || depth != 0 {
		t.Fatalf("%d strokes, %d in the polygon color, save depth %d; want 12, 7 and 0", strokes, red, depth)
	}
}
