package tilemap_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func nearPt(a, b core.Point, tol float64) bool {
	return math.Abs(float64(a.X-b.X)) <= tol && math.Abs(float64(a.Y-b.Y)) <= tol
}

func TestCameraWorldViewRoundTrip(t *testing.T) {
	zooms := []float32{0.25, 0.5, 1, 2, 4}
	rotations := []float32{0, math.Pi / 6, math.Pi / 2, math.Pi, -1.23}
	worldPoints := []core.Point{
		{X: 0, Y: 0},
		{X: 120, Y: -85},
		{X: -340.5, Y: 210.25},
		{X: 1024, Y: 768},
	}

	for _, zoom := range zooms {
		for _, rot := range rotations {
			cam := &tilemap.Camera{
				X:        120,
				Y:        -85,
				Zoom:     zoom,
				Rotation: rot,
				Width:    640,
				Height:   360,
			}

			// Camera center always maps to the center of the view.
			centerView := cam.WorldToView(core.Point{X: core.Points(cam.X), Y: core.Points(cam.Y)})
			wantCenter := core.Point{X: 320, Y: 180}
			if !nearPt(centerView, wantCenter, 1e-3) {
				t.Fatalf("zoom=%g rot=%g: WorldToView(center) = %+v, want %+v", zoom, rot, centerView, wantCenter)
			}

			for _, wp := range worldPoints {
				vp := cam.WorldToView(wp)
				back := cam.ViewToWorld(vp)
				if !nearPt(back, wp, 1e-2) {
					t.Fatalf("zoom=%g rot=%g: ViewToWorld(WorldToView(%+v)) = %+v (via %+v)", zoom, rot, wp, back, vp)
				}
				vp2 := cam.WorldToView(back)
				if !nearPt(vp2, vp, 1e-2) {
					t.Fatalf("zoom=%g rot=%g: WorldToView(ViewToWorld(%+v)) = %+v", zoom, rot, vp, vp2)
				}
			}

			// VisibleBounds must tightly enclose all four view corners mapped into world space.
			vb := cam.VisibleBounds()
			corners := []core.Point{
				cam.ViewToWorld(core.Point{X: 0, Y: 0}),
				cam.ViewToWorld(core.Point{X: 640, Y: 0}),
				cam.ViewToWorld(core.Point{X: 0, Y: 360}),
				cam.ViewToWorld(core.Point{X: 640, Y: 360}),
			}
			minX, maxX := corners[0].X, corners[0].X
			minY, maxY := corners[0].Y, corners[0].Y
			for _, c := range corners[1:] {
				minX = min(minX, c.X)
				maxX = max(maxX, c.X)
				minY = min(minY, c.Y)
				maxY = max(maxY, c.Y)
			}
			if math.Abs(float64(vb.Origin.X-minX)) > 1e-2 ||
				math.Abs(float64(vb.Origin.Y-minY)) > 1e-2 ||
				math.Abs(float64(vb.Size.Width-(maxX-minX))) > 1e-2 ||
				math.Abs(float64(vb.Size.Height-(maxY-minY))) > 1e-2 {
				t.Fatalf("zoom=%g rot=%g: VisibleBounds=%+v, corners span [%g..%g] x [%g..%g]",
					zoom, rot, vb, minX, maxX, minY, maxY)
			}
		}
	}

	// Non-positive zoom defaults to 1.
	for _, z := range []float32{0, -2} {
		cam := &tilemap.Camera{X: 10, Y: 20, Zoom: z, Width: 100, Height: 80}
		got := cam.WorldToView(core.Point{X: 15, Y: 25})
		want := core.Point{X: 55, Y: 45}
		if !nearPt(got, want, 1e-4) {
			t.Fatalf("zoom=%g: WorldToView = %+v, want %+v", z, got, want)
		}
	}

	// Clockwise camera rotation by pi/2 moves a point to the right of the camera upward in the view.
	rotCam := &tilemap.Camera{X: 0, Y: 0, Zoom: 2, Rotation: math.Pi / 2, Width: 200, Height: 200}
	got := rotCam.WorldToView(core.Point{X: 10, Y: 0})
	want := core.Point{X: 100, Y: 80}
	if !nearPt(got, want, 1e-3) {
		t.Fatalf("clockwise pi/2 rotation: WorldToView(10, 0) = %+v, want %+v", got, want)
	}
}

func TestCameraApplyRecordsExpectedTransformOps(t *testing.T) {
	fullCam := &tilemap.Camera{
		X:        100,
		Y:        50,
		Zoom:     2,
		Rotation: math.Pi / 6,
		Width:    320,
		Height:   180,
	}
	identCam := &tilemap.Camera{Zoom: 0}
	shakeCam := &tilemap.Camera{
		X:         40,
		Y:         60,
		Zoom:      1.5,
		Width:     200,
		Height:    100,
		ShakeSeed: 7,
	}
	shakeCam.Shake(12, 200*time.Millisecond)
	shakeOff := shakeCam.ShakeOffset()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "full-got", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				fullCam.Apply(d)
			}},
			core.Canvas{TestID: "full-want", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Translate(160, 90)
				d.Scale(2, 2)
				d.Rotate(-math.Pi / 6)
				d.Translate(-100, -50)
			}},
			core.Canvas{TestID: "ident-got", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				identCam.Apply(d)
			}},
			core.Canvas{TestID: "shake-got", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				shakeCam.Apply(d)
			}},
			core.Canvas{TestID: "shake-want", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Translate(100, 50)
				d.Scale(1.5, 1.5)
				d.Translate(-(40 + float32(shakeOff.X)), -(60 + float32(shakeOff.Y)))
			}},
		}}
	})

	sameOps(t, app.Find("full-got").Ops, app.Find("full-want").Ops)
	if len(app.Find("ident-got").Ops) != 0 {
		t.Fatalf("identity camera recorded %d ops, want 0", len(app.Find("ident-got").Ops))
	}
	sameOps(t, app.Find("shake-got").Ops, app.Find("shake-want").Ops)
}

func TestCameraFollowConvergesDeterministicallyOnLoop(t *testing.T) {
	run := func() []core.Point {
		cam := &tilemap.Camera{
			X:      0,
			Y:      0,
			Zoom:   1,
			Width:  100,
			Height: 80,
		}
		target := core.Point{X: 300, Y: -150}
		var trail []core.Point
		l := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				cam.Follow(target, 8, dt)
				trail = append(trail, core.Point{X: core.Points(cam.X), Y: core.Points(cam.Y)})
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			l.Run(cx)
			return core.View{}
		})
		app.Advance(2 * time.Second)
		return trail
	}

	trailA := run()
	trailB := run()
	if len(trailA) != 120 || len(trailB) != 120 {
		t.Fatalf("got %d and %d steps, want 120", len(trailA), len(trailB))
	}

	target := core.Point{X: 300, Y: -150}
	prevDist := math.Inf(1)
	for i := range trailA {
		if trailA[i] != trailB[i] {
			t.Fatalf("step %d: non-deterministic follow %+v != %+v", i, trailA[i], trailB[i])
		}
		d := math.Hypot(float64(trailA[i].X-target.X), float64(trailA[i].Y-target.Y))
		if d > prevDist {
			t.Fatalf("step %d: distance increased from %g to %g", i, prevDist, d)
		}
		prevDist = d
	}
	if prevDist > 1e-3 {
		t.Fatalf("after 2s: distance to target is %g, want <= 1e-3 (pos=%+v)", prevDist, trailA[len(trailA)-1])
	}

	// smoothing <= 0 snaps immediately and clamps to Bounds.
	bounds := core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 400, Height: 300}}
	snapCam := &tilemap.Camera{X: 100, Y: 100, Zoom: 1, Width: 100, Height: 80, Bounds: &bounds}
	snapCam.Follow(core.Point{X: 999, Y: -50}, 0, 16*time.Millisecond)
	if snapCam.X != 350 || snapCam.Y != 40 {
		t.Fatalf("snap+clamp pos = (%g, %g), want (350, 40)", snapCam.X, snapCam.Y)
	}
}

func TestCameraClampBounds(t *testing.T) {
	bounds := core.Bounds{
		Origin: core.Point{X: -100, Y: -50},
		Size:   core.Size{Width: 1000, Height: 600},
	}

	// Normal and zoomed cameras never let VisibleBounds escape Bounds.
	for _, tc := range []struct {
		name       string
		zoom       float32
		rot        float32
		tryX, tryY float32
	}{
		{"top-left zoom 1", 1, 0, -500, -500},
		{"bottom-right zoom 1", 1, 0, 2000, 2000},
		{"top-left zoom 2", 2, 0, -500, -500},
		{"bottom-right zoom 2", 2, 0, 2000, 2000},
		{"top-left zoom 0.5", 0.5, 0, -500, -500},
		{"bottom-right zoom 0.5", 0.5, 0, 2000, 2000},
		{"rotated top-left", 1.5, math.Pi / 4, -500, -500},
		{"rotated bottom-right", 1.5, math.Pi / 4, 2000, 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cam := &tilemap.Camera{
				X:        tc.tryX,
				Y:        tc.tryY,
				Zoom:     tc.zoom,
				Rotation: tc.rot,
				Width:    200,
				Height:   120,
				Bounds:   &bounds,
			}
			cam.Clamp()
			vb := cam.VisibleBounds()
			const eps = 1e-3
			if float64(vb.Origin.X) < float64(bounds.Origin.X)-eps ||
				float64(vb.Origin.Y) < float64(bounds.Origin.Y)-eps ||
				float64(vb.Origin.X+vb.Size.Width) > float64(bounds.Origin.X+bounds.Size.Width)+eps ||
				float64(vb.Origin.Y+vb.Size.Height) > float64(bounds.Origin.Y+bounds.Size.Height)+eps {
				t.Fatalf("VisibleBounds %+v escaped Bounds %+v (cam at %g,%g)", vb, bounds, cam.X, cam.Y)
			}
		})
	}

	// World smaller than view on both axes centers on Bounds.
	small := core.Bounds{
		Origin: core.Point{X: 20, Y: 40},
		Size:   core.Size{Width: 100, Height: 60},
	}
	cam := &tilemap.Camera{X: -999, Y: 999, Zoom: 1, Width: 320, Height: 240, Bounds: &small}
	cam.Clamp()
	if cam.X != 70 || cam.Y != 70 {
		t.Fatalf("small world centered at (%g, %g), want (70, 70)", cam.X, cam.Y)
	}

	// Zooming out so the view becomes larger than Bounds also centers on Bounds.
	camZoomOut := &tilemap.Camera{X: 800, Y: 500, Zoom: 0.25, Width: 400, Height: 300, Bounds: &bounds}
	camZoomOut.Clamp()
	if camZoomOut.X != 400 || camZoomOut.Y != 250 {
		t.Fatalf("zoomed-out world centered at (%g, %g), want (400, 250)", camZoomOut.X, camZoomOut.Y)
	}

	// World smaller on X only: X centers, Y clamps.
	tall := core.Bounds{
		Origin: core.Point{X: 10, Y: 0},
		Size:   core.Size{Width: 100, Height: 500},
	}
	camTall := &tilemap.Camera{X: 999, Y: 999, Zoom: 1, Width: 200, Height: 100, Bounds: &tall}
	camTall.Clamp()
	if camTall.X != 60 || camTall.Y != 450 {
		t.Fatalf("tall world clamped to (%g, %g), want (60, 450)", camTall.X, camTall.Y)
	}
}

func TestCameraShakeOffsetsAndReturnsToExactZero(t *testing.T) {
	runShake := func(seed uint64) []core.Point {
		cam := &tilemap.Camera{
			X:         100,
			Y:         100,
			Zoom:      1,
			Width:     320,
			Height:    180,
			ShakeSeed: seed,
		}
		cam.Shake(16, 200*time.Millisecond)
		offsets := []core.Point{cam.ShakeOffset()}

		l := &game.Loop{
			Rate: 100, // 10ms per step
			Update: func(dt time.Duration) {
				cam.Update(dt)
				offsets = append(offsets, cam.ShakeOffset())
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			l.Run(cx)
			return core.View{}
		})
		// 300ms = 30 steps: steps 0..19 are within 200ms, steps 20..30 are at/after 200ms.
		app.Advance(300 * time.Millisecond)

		// Round-trip still holds during/after shake.
		p := core.Point{X: 123, Y: 456}
		if back := cam.ViewToWorld(cam.WorldToView(p)); !nearPt(back, p, 1e-3) {
			t.Fatalf("post-shake round-trip failed: %+v -> %+v", p, back)
		}
		return offsets
	}

	a := runShake(42)
	b := runShake(42)
	c := runShake(99)

	if len(a) != 31 {
		t.Fatalf("recorded %d shake samples, want 31", len(a))
	}

	// Deterministic for same seed.
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("step %d: non-deterministic shake %+v != %+v", i, a[i], b[i])
		}
	}
	if a[0] == c[0] {
		t.Fatalf("different seeds produced identical initial shake offset %+v", a[0])
	}

	// Non-zero and bounded during duration (samples 0..19 correspond to 0ms..190ms).
	for i := 0; i < 20; i++ {
		mag := math.Hypot(float64(a[i].X), float64(a[i].Y))
		if mag == 0 {
			t.Fatalf("step %d (elapsed %dms): shake offset was 0 during duration", i, i*10)
		}
		if mag > 16+1e-3 {
			t.Fatalf("step %d: shake magnitude %g exceeded intensity 16", i, mag)
		}
	}

	// Exact (0, 0) at and after duration (samples 20..30 correspond to 200ms..300ms).
	for i := 20; i < len(a); i++ {
		if a[i].X != 0 || a[i].Y != 0 {
			t.Fatalf("step %d (elapsed %dms): shake offset = %+v, want exact (0, 0)", i, i*10, a[i])
		}
	}

	// Shake with non-positive intensity or duration cancels immediately to exact (0, 0).
	cam := &tilemap.Camera{ShakeSeed: 5}
	cam.Shake(10, time.Second)
	if cam.ShakeOffset() == (core.Point{}) {
		t.Fatal("expected non-zero shake offset immediately after Shake")
	}
	cam.Shake(0, time.Second)
	if off := cam.ShakeOffset(); off.X != 0 || off.Y != 0 {
		t.Fatalf("after cancel: ShakeOffset = %+v, want exact (0, 0)", off)
	}
}

func TestCameraParallaxScalesPositionAndShake(t *testing.T) {
	cam := &tilemap.Camera{
		X:         120,
		Y:         -80,
		Zoom:      2,
		Rotation:  math.Pi / 8,
		Width:     320,
		Height:    180,
		ShakeSeed: 9,
	}
	cam.Shake(10, 200*time.Millisecond)
	shake := cam.ShakeOffset()

	p := cam.Parallax(0.5, 0.25)
	wantX := (120 + float32(shake.X)) * 0.5
	wantY := (-80 + float32(shake.Y)) * 0.25
	if math.Abs(float64(p.X-wantX)) > 1e-4 || math.Abs(float64(p.Y-wantY)) > 1e-4 {
		t.Fatalf("Parallax(0.5, 0.25) pos = (%g, %g), want (%g, %g)", p.X, p.Y, wantX, wantY)
	}
	if p.Zoom != cam.Zoom || p.Rotation != cam.Rotation || p.Width != cam.Width || p.Height != cam.Height {
		t.Fatalf("Parallax did not preserve zoom/rotation/size: %+v vs %+v", p, *cam)
	}
	if p.ShakeOffset() != (core.Point{}) {
		t.Fatalf("Parallax camera should have shake baked into X/Y with zero residual shake: %+v", p.ShakeOffset())
	}

	var nilCam *tilemap.Camera
	if got := nilCam.Parallax(0.5, 0.5); got != (tilemap.Camera{}) {
		t.Fatalf("nil Camera.Parallax = %+v, want zero Camera", got)
	}
}
