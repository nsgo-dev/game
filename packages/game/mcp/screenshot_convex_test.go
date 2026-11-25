package mcp_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestScreenshotOutlinesConvexShapes(t *testing.T) {
	// A 30 by 4 polygon turned a quarter: its stored Shape is already upright, so the overlay must not rotate it again.
	rw := &physics.RigidWorld{}
	plank := physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(30, 24, 30, 4, 0)), 1)
	plank.Angle = math.Pi / 2
	rw.AddBody(plank)
	rw.Step(time.Millisecond)

	pw := &physics.World{}
	pw.Add(
		&physics.Body{Shape: physics.SegmentShape(physics.Segment{X1: 5, Y1: 44, X2: 90, Y2: 44})},
		&physics.Body{Shape: physics.CapsuleShape(physics.Capsule{X1: 60, Y1: 20, X2: 80, Y2: 20, R: 5})},
		&physics.Body{Shape: physics.ChainShape([]core.Point{{X: 5, Y: 2}, {X: 90, Y: 2}}, false)},
	)

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Physics:      pw,
			RigidPhysics: rw,
			DrawFrame: func(d *core.Drawing, w, h int) {
				d.SetFillColor(core.RGB(0x202020)).FillRect(0, 0, float32(w), float32(h))
			},
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})
	res := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"width":            96,
		"height":           48,
		"collision_shapes": true,
	})
	d := res.OverlaysDrawn
	if d.PhysicsPolygons != 1 || d.PhysicsCapsules != 1 || d.PhysicsLines != 2 {
		t.Fatalf("overlays = %+v", d)
	}
	img := decodeBase64PNG(t, res.PNGBase64)
	style := mcp.DefaultOverlayStyle()
	boxCol := coreColorToRGBA(style.BoxColor)
	circleCol := coreColorToRGBA(style.CircleColor)
	assertPixel(t, img, 28, 24, boxCol, "upright plank left edge")
	if got := img.RGBAAt(40, 22); got == boxCol {
		t.Errorf("plank drawn lying down at (40,22): rotated twice")
	}
	assertPixel(t, img, 50, 44, boxCol, "segment")
	assertPixel(t, img, 50, 2, boxCol, "chain")
	assertPixel(t, img, 70, 15, circleCol, "capsule top side")
}
