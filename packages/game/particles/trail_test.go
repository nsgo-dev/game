package particles_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestTrailBasicAndExpiry(t *testing.T) {
	tr := particles.NewTrail(particles.TrailConfig{
		MaxPoints:   4,
		Lifetime:    0.4,
		MinDistance: 5,
		Width:       10,
	})

	tr.AddPoint(0, 0)
	if tr.PointCount() != 1 {
		t.Fatalf("PointCount = %d, want 1", tr.PointCount())
	}
	// Closer than MinDistance (5) is skipped.
	tr.AddPoint(2, 2)
	if tr.PointCount() != 1 {
		t.Fatalf("PointCount after sub-MinDistance point = %d, want 1", tr.PointCount())
	}
	// Exact duplicate refreshes age without adding a point.
	tr.Update(0.1)
	tr.AddPoint(0, 0)
	if tr.PointCount() != 1 || tr.Points()[0].Age != 0 {
		t.Fatalf("duplicate point did not refresh age: count=%d age=%v", tr.PointCount(), tr.Points()[0].Age)
	}

	// Add points past MaxPoints (4) — oldest is evicted.
	for i := 1; i <= 5; i++ {
		tr.Update(0.05)
		tr.AddPoint(float32(i*10), 0)
	}
	if tr.PointCount() != 4 {
		t.Fatalf("PointCount after cap = %d, want 4", tr.PointCount())
	}
	pts := tr.Points()
	if pts[0].X != 20 || pts[3].X != 50 {
		t.Fatalf("retained range = [%v..%v], want [20..50]", pts[0].X, pts[3].X)
	}
	if !near(pts[0].Length, 0) || !near(pts[3].Length, 30) {
		t.Fatalf("lengths = [%v..%v], want [0..30]", pts[0].Length, pts[3].Length)
	}

	// Non-positive dt does nothing.
	tr.Update(0)
	tr.Update(-1)
	if tr.PointCount() != 4 {
		t.Fatalf("non-positive Update changed count to %d", tr.PointCount())
	}

	// Advance past Lifetime to expire all points.
	tr.Update(0.5)
	if tr.PointCount() != 0 {
		t.Fatalf("PointCount after full expiry = %d, want 0", tr.PointCount())
	}

	// Clear empties points immediately.
	tr.AddPoint(1, 1)
	tr.AddPoint(10, 1)
	tr.Clear()
	if tr.PointCount() != 0 {
		t.Fatalf("PointCount after Clear = %d, want 0", tr.PointCount())
	}
}

func TestTrailWidthAndColorEvaluation(t *testing.T) {
	cHead := core.Color(0xff0000ff)
	cTail := core.Color(0x0000ff00)
	tr := particles.NewTrail(particles.TrailConfig{
		Width: 20,
		WidthCurve: particles.Curve{
			{T: 0, Value: 1},
			{T: 0.5, Value: 0.5},
			{T: 1, Value: 0},
		},
		ColorGradient: particles.ColorCurve{
			{T: 0, Color: cHead},
			{T: 1, Color: cTail},
		},
	})

	w0, col0 := tr.EvaluateAt(0)
	if !near(w0, 20) || col0 != cHead {
		t.Fatalf("EvaluateAt(0) = (%v, %#08x), want (20, %#08x)", w0, uint32(col0), uint32(cHead))
	}
	wMid, colMid := tr.EvaluateAt(0.5)
	wantMid := core.Color(0x80008080)
	if !near(wMid, 10) || colMid != wantMid {
		t.Fatalf("EvaluateAt(0.5) = (%v, %#08x), want (10, %#08x)", wMid, uint32(colMid), uint32(wantMid))
	}
	w1, col1 := tr.EvaluateAt(1)
	if !near(w1, 0) || col1 != cTail {
		t.Fatalf("EvaluateAt(1) = (%v, %#08x), want (0, %#08x)", w1, uint32(col1), uint32(cTail))
	}
}

func TestTrailDrawDeterministicAndRGBA(t *testing.T) {
	// Horizontal straight line from (10, 50) to (110, 50) with Width=10 and constant curve.
	// Perpendicular normal is (0, +1) -> left edge at Y=55, right edge at Y=45.
	solidColor := core.Color(0x112233ff)
	solid := particles.NewTrail(particles.TrailConfig{
		Width: 10,
		Color: solidColor,
	})
	solid.AddPoint(10, 50)
	solid.AddPoint(60, 50)
	solid.AddPoint(110, 50)

	// Gradient trail with tapering width curve.
	gradCfg := particles.TrailConfig{
		Width: 12,
		WidthCurve: particles.Curve{
			{T: 0, Value: 1}, // head (newest)
			{T: 1, Value: 0}, // tail (oldest)
		},
		ColorGradient: particles.ColorCurve{
			{T: 0, Color: core.Color(0xff8000ff)},
			{T: 1, Color: core.Color(0x0040ff00)},
		},
	}
	grad1 := particles.NewTrail(gradCfg)
	grad2 := particles.NewTrail(gradCfg)
	for _, pt := range [][2]float32{{0, 20}, {30, 20}, {60, 40}, {90, 40}} {
		grad1.AddPoint(pt[0], pt[1])
		grad2.AddPoint(pt[0], pt[1])
	}

	texTrail := particles.NewTrail(particles.TrailConfig{
		Width:   8,
		Color:   core.Color(0xffffff80),
		Texture: core.ImageSource{URI: "asset:ribbon.png"},
	})
	texTrail.AddPoint(0, 10)
	texTrail.AddPoint(40, 10)
	texTrail.AddPoint(80, 30)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "solid", Style: core.Style{Width: core.Pt(150), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				solid.Draw(d)
			}},
			core.Canvas{TestID: "grad1", Style: core.Style{Width: core.Pt(150), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				grad1.Draw(d)
			}},
			core.Canvas{TestID: "grad2", Style: core.Style{Width: core.Pt(150), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				grad2.Draw(d)
			}},
			core.Canvas{TestID: "tex", Style: core.Style{Width: core.Pt(150), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				texTrail.Draw(d)
			}},
		}}
	})

	solidOps := app.Find("solid").Ops
	if len(solidOps) == 0 {
		t.Fatal("solid trail emitted no draw ops")
	}
	// Verify exact solid RGBA color and perpendicular ribbon coordinates in the recorded ops.
	foundColor := false
	var ys []float32
	for _, op := range solidOps {
		if op.Color == solidColor {
			foundColor = true
		}
		if op.Y == 45 || op.Y == 55 {
			ys = append(ys, op.Y)
		}
	}
	if !foundColor {
		t.Fatalf("solid trail did not emit expected fill RGBA %#08x", uint32(solidColor))
	}
	if len(ys) < 6 {
		t.Fatalf("expected 6 perpendicular ribbon vertices at Y=45/55, got %v", ys)
	}

	// Deterministic gradient draw ops and RGBA stops check.
	g1Ops := app.Find("grad1").Ops
	g2Ops := app.Find("grad2").Ops
	if len(g1Ops) == 0 {
		t.Fatal("gradient trail emitted no draw ops")
	}
	if !reflect.DeepEqual(g1Ops, g2Ops) {
		t.Fatal("identical gradient trails produced different draw ops")
	}
	gradCount := 0
	var firstTailColor, lastHeadColor core.Color
	for _, op := range g1Ops {
		if op.Gradient != nil && len(op.Gradient.Stops) == 2 {
			if gradCount == 0 {
				firstTailColor = op.Gradient.Stops[0].Color
			}
			lastHeadColor = op.Gradient.Stops[1].Color
			gradCount++
		}
	}
	if gradCount != 3 {
		t.Fatalf("expected 3 linear-gradient segment ops for 4 points, got %d", gradCount)
	}
	if firstTailColor != core.Color(0x0040ff00) {
		t.Fatalf("tail RGBA = %#08x, want 0x0040ff00", uint32(firstTailColor))
	}
	if lastHeadColor != core.Color(0xff8000ff) {
		t.Fatalf("head RGBA = %#08x, want 0xff8000ff", uint32(lastHeadColor))
	}

	// Textured trail emits image draw ops.
	texOps := app.Find("tex").Ops
	imgOps := 0
	for _, op := range texOps {
		if op.Image != nil && op.Image.URI == "asset:ribbon.png" {
			imgOps++
		}
	}
	if imgOps != 2 {
		t.Fatalf("textured trail emitted %d image ops for 3 points, want 2", imgOps)
	}
}

func TestTrailAttachAndManager(t *testing.T) {
	mgr := particles.NewTrailManager()
	var x, y float32 = 0, 10
	tr := mgr.Add(particles.TrailConfig{
		Lifetime:    1.0,
		MinDistance: 2,
		Width:       6,
		Color:       core.RGB(0x00ff00),
	})
	tr.Attach(func() (float32, float32) { return x, y })

	for i := 1; i <= 5; i++ {
		x = float32(i * 10)
		y = 10 + float32(math.Sin(float64(i)))*5
		mgr.Update(0.05)
	}
	if mgr.Count() != 1 {
		t.Fatalf("manager Count = %d, want 1", mgr.Count())
	}
	if tr.PointCount() != 6 {
		t.Fatalf("attached trail PointCount = %d, want 6", tr.PointCount())
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "mgr", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
			mgr.Draw(d)
		}}
	})
	if len(app.Find("mgr").Ops) == 0 {
		t.Fatal("TrailManager.Draw emitted no ops")
	}

	mgr.Clear()
	if tr.PointCount() != 0 {
		t.Fatalf("PointCount after mgr.Clear = %d, want 0", tr.PointCount())
	}
	mgr.Remove(tr)
	if mgr.Count() != 0 {
		t.Fatalf("manager Count after Remove = %d, want 0", mgr.Count())
	}
}

func TestEmitterAttachTrail(t *testing.T) {
	e := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		X:           10,
		Y:           20,
		SpeedMin:    100,
		SpeedMax:    100,
		AngleMin:    0,
		AngleMax:    0,
		LifetimeMin: 150 * time.Millisecond,
		LifetimeMax: 150 * time.Millisecond,
		Seed:        7,
	}
	e.AttachTrail(particles.TrailConfig{
		Lifetime:    0.2,
		MinDistance: 1,
		Width:       6,
		Color:       core.Color(0xffaa00ff),
	})

	e.Burst(2)
	if e.TrailCount() != 2 {
		t.Fatalf("TrailCount after burst = %d, want 2", e.TrailCount())
	}

	for range 3 {
		e.Update(40 * time.Millisecond)
	}
	for i, tr := range e.ParticleTrails() {
		if tr.PointCount() < 3 {
			t.Fatalf("particle trail %d has %d points, want >= 3", i, tr.PointCount())
		}
	}

	// Draw emits both ribbon trail fill ops and sprite image ops.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "trailed", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
			e.Draw(d)
		}}
	})
	ops := app.Find("trailed").Ops
	hasTrailColor := false
	hasSprite := false
	for _, op := range ops {
		if op.Color == core.Color(0xffaa00ff) {
			hasTrailColor = true
		}
		if op.Image != nil && op.Image.URI == "asset:particles.png" {
			hasSprite = true
		}
	}
	if !hasTrailColor || !hasSprite {
		t.Fatalf("expected both trail fill and sprite ops, got trail=%v sprite=%v", hasTrailColor, hasSprite)
	}

	// Expire particles: trails continue fading out gracefully before disappearing.
	e.Update(50 * time.Millisecond)
	if e.Count() != 0 {
		t.Fatalf("Count after expiry = %d, want 0", e.Count())
	}
	if e.TrailCount() == 0 {
		t.Fatal("expected fading trails to remain briefly after particle expiry")
	}
	e.Update(300 * time.Millisecond)
	if e.TrailCount() != 0 {
		t.Fatalf("TrailCount after fadeout = %d, want 0", e.TrailCount())
	}
}
