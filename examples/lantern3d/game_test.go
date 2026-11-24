package main

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func viewCenter(v *nsgotest.View) core.Point {
	b := v.Bounds()
	return core.Point{
		X: b.Origin.X + b.Size.Width/2,
		Y: b.Origin.Y + b.Size.Height/2,
	}
}

func colorDiff(a, b color.RGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	if dr < 0 {
		dr = -dr
	}
	if dg < 0 {
		dg = -dg
	}
	if db < 0 {
		db = -db
	}
	return dr + dg + db
}

func clearSkyRGBA() color.RGBA {
	return color.RGBA{
		R: uint8((clearSkyHex >> 16) & 0xFF),
		G: uint8((clearSkyHex >> 8) & 0xFF),
		B: uint8(clearSkyHex & 0xFF),
		A: 255,
	}
}

type frameStats struct {
	nonSkyPixels   int
	warmLitPixels  int
	distinctColors int
}

func analyzeFrame(img *image.RGBA) frameStats {
	sky := clearSkyRGBA()
	b := img.Bounds()
	seen := make(map[uint32]struct{}, 256)
	var st frameStats
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if colorDiff(c, sky) > 12 {
				st.nonSkyPixels++
				// Quantize to 4-bit channels to count genuinely distinct shaded tones.
				q := (uint32(c.R>>4) << 8) | (uint32(c.G>>4) << 4) | uint32(c.B>>4)
				seen[q] = struct{}{}
				// Warm lantern / amber highlight pixels have R noticeably higher than B.
				if int(c.R) > 140 && int(c.R)-int(c.B) > 35 {
					st.warmLitPixels++
				}
			}
		}
	}
	st.distinctColors = len(seen)
	return st
}

func imagesDiffer(a, b *image.RGBA) int {
	bounds := a.Bounds()
	changed := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if colorDiff(a.RGBAAt(x, y), b.RGBAAt(x, y)) > 15 {
				changed++
			}
		}
	}
	return changed
}

func TestGLBAssetLoadsWithTexturesAndHierarchy(t *testing.T) {
	sc, err := NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	if len(sc.Doc.Nodes) < 10 {
		t.Fatalf("glTF nodes = %d, want >= 10", len(sc.Doc.Nodes))
	}
	if len(sc.Doc.Images) < 2 || len(sc.Doc.Textures) < 2 {
		t.Fatalf("glTF images=%d textures=%d, want >= 2 each", len(sc.Doc.Images), len(sc.Doc.Textures))
	}
	for i := range sc.Doc.Images {
		img, err := sc.Doc.DecodeImage(i)
		if err != nil {
			t.Fatalf("DecodeImage(%d): %v", i, err)
		}
		if img.Bounds().Dx() < 16 || img.Bounds().Dy() < 16 {
			t.Fatalf("image %d size = %v, want >= 16x16", i, img.Bounds())
		}
	}
	if sc.MeshCount() < 10 || sc.TriangleCount() < 100 {
		t.Fatalf("scene meshes=%d tris=%d, want >= 10 meshes and >= 100 tris", sc.MeshCount(), sc.TriangleCount())
	}
	if sc.wispNodeIdx < 0 || sc.coreNodeIdx < 0 {
		t.Fatalf("missing animated nodes: wisp=%d core=%d", sc.wispNodeIdx, sc.coreNodeIdx)
	}

	// Advancing with AnimateLights moves the orbiting point light.
	posBefore := sc.Lights.PointPos
	sc.Step(500 * time.Millisecond)
	if sc.Lights.PointPos.ApproxEqual(posBefore, 1e-4) {
		t.Fatalf("orbiting point light did not move: before=%v after=%v", posBefore, sc.Lights.PointPos)
	}
}

func TestSceneDrawsOnFullAndWebGL2Profiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			var c *Controller
			opts := append([]nsgotest.Option{nsgotest.Size(160, 120), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				var node core.Node
				c, node = NewView(cx)
				return node
			}, opts...)

			app.Advance(32 * time.Millisecond)
			if c.DeviceReady == 0 || !c.Renderer.Ready() {
				t.Fatalf("renderer not ready: readyCount=%d ready=%v", c.DeviceReady, c.Renderer.Ready())
			}
			if c.RenderedFrames == 0 {
				t.Fatal("OnFrame did not render any frame")
			}

			img, err := app.GPUReadPixels("id:lantern-gpu")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			// Top-left sky pixel should match clear sky color.
			sky := clearSkyRGBA()
			if diff := colorDiff(img.RGBAAt(2, 2), sky); diff > 10 {
				t.Fatalf("corner sky pixel = %+v, want ~%+v", img.RGBAAt(2, 2), sky)
			}

			st := analyzeFrame(img)
			total := img.Bounds().Dx() * img.Bounds().Dy()
			if st.nonSkyPixels < total/8 {
				t.Fatalf("too few lit geometry pixels: %d of %d (distinct=%d)", st.nonSkyPixels, total, st.distinctColors)
			}
			if st.distinctColors < 12 {
				t.Fatalf("too few shaded colors: %d, want >= 12", st.distinctColors)
			}
			if st.warmLitPixels == 0 {
				t.Fatalf("expected warm lantern / amber lit pixels, found 0 (nonSky=%d distinct=%d)", st.nonSkyPixels, st.distinctColors)
			}

			// HUD stats label reports loaded mesh and triangle counts.
			statsText := app.Find("id:hud-stats").Text
			if !strings.Contains(statsText, "meshes") || !strings.Contains(statsText, "tris") {
				t.Fatalf("hud-stats = %q, want meshes and tris", statsText)
			}
		})
	}
}

func TestCameraOrbitAndResetChangeRenderedView(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(280, 160), nsgotest.ManualFrames())

	// Disable auto-orbit and light animation so pixel changes come solely from camera orbit.
	app.Press("id:btn-orbit")
	if c.Scene.AutoOrbit {
		t.Fatal("btn-orbit did not disable AutoOrbit")
	}
	app.Press("id:btn-lights")
	if c.Scene.AnimateLights {
		t.Fatal("btn-lights did not disable AnimateLights")
	}

	app.Advance(16 * time.Millisecond)
	beforeImg, err := app.GPUReadPixels("id:lantern-gpu")
	if err != nil {
		t.Fatalf("GPUReadPixels before: %v", err)
	}
	yawBefore := c.Scene.Orbit.Yaw

	// Orbit significantly via keyboard right arrow + zoom in.
	for range 8 {
		app.Key("right")
	}
	app.Key("=")
	// Advance several fixed steps without rendering 30 full software frames.
	app.Post(func() {
		for range 24 {
			c.Scene.Step(time.Second / stepRate)
		}
	})
	app.Advance(16 * time.Millisecond)

	if math.Abs(float64(c.Scene.Orbit.Yaw-yawBefore)) < 0.4 {
		t.Fatalf("camera yaw did not orbit: before=%.3f after=%.3f", yawBefore, c.Scene.Orbit.Yaw)
	}
	afterImg, err := app.GPUReadPixels("id:lantern-gpu")
	if err != nil {
		t.Fatalf("GPUReadPixels after: %v", err)
	}
	if changed := imagesDiffer(beforeImg, afterImg); changed < 200 {
		t.Fatalf("camera orbit barely changed rendered frame: %d pixels changed", changed)
	}

	// Reset button returns camera to default vantage point.
	app.Press("id:btn-reset")
	if math.Abs(float64(c.Scene.Orbit.Yaw-defaultOrbitYaw)) > 1e-3 ||
		math.Abs(float64(c.Scene.Orbit.Pitch-defaultOrbitPitch)) > 1e-3 ||
		math.Abs(float64(c.Scene.Orbit.Distance-defaultOrbitDist)) > 1e-3 {
		t.Fatalf("Reset did not restore defaults: yaw=%.3f pitch=%.3f dist=%.3f",
			c.Scene.Orbit.Yaw, c.Scene.Orbit.Pitch, c.Scene.Orbit.Distance)
	}
}

func TestHUDControlsAndOpenAreaDragPassThrough(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(480, 320), nsgotest.ManualFrames(),
		nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Turn off auto-orbit so only drag events move the camera.
	app.Press("id:btn-orbit")
	if c.Scene.AutoOrbit {
		t.Fatal("AutoOrbit should be false after pressing btn-orbit")
	}
	if app.Find("Orbit: Off") == nil {
		t.Fatal("btn-orbit did not update label to Orbit: Off")
	}

	// Mouse down on a HUD button must NOT start a 3D viewport drag.
	resetPos := viewCenter(app.Find("id:btn-reset"))
	app.MouseDownAt("id:lantern-root", resetPos, core.MouseButtonLeft)
	if c.mouseDragging {
		t.Fatal("clicking HUD Reset button leaked MouseDown into 3D orbit drag")
	}
	app.MouseUpAt("id:lantern-root", resetPos, core.MouseButtonLeft)

	// Mouse drag on open center 3D area MUST reach lantern-viewport and orbit the camera.
	yawBefore := c.Scene.Orbit.Yaw
	pitchBefore := c.Scene.Orbit.Pitch
	startPt := core.Point{X: 220, Y: 170}
	endPt := core.Point{X: 310, Y: 135}
	app.MouseDownAt("id:lantern-root", startPt, core.MouseButtonLeft)
	if !c.mouseDragging {
		t.Fatal("MouseDown on open 3D area did not reach lantern-viewport")
	}
	app.HoverAt("id:lantern-root", endPt)
	app.MouseUpAt("id:lantern-root", endPt, core.MouseButtonLeft)
	if c.mouseDragging {
		t.Fatal("MouseUp did not end drag")
	}
	app.Post(func() {
		for range 20 {
			c.Scene.Step(time.Second / stepRate)
		}
	})
	app.Settle()

	if math.Abs(float64(c.Scene.Orbit.Yaw-yawBefore)) < 0.2 {
		t.Fatalf("open-area mouse drag did not change yaw: before=%.3f after=%.3f", yawBefore, c.Scene.Orbit.Yaw)
	}
	if math.Abs(float64(c.Scene.Orbit.Pitch-pitchBefore)) < 0.05 {
		t.Fatalf("open-area mouse drag did not change pitch: before=%.3f after=%.3f", pitchBefore, c.Scene.Orbit.Pitch)
	}

	// Touch drag on open 3D area also orbits the camera.
	yawBeforeTouch := c.Scene.Orbit.Yaw
	finger := app.Touch("id:lantern-root", core.Point{X: 200, Y: 180})
	finger.Move(core.Point{X: 120, Y: 180})
	finger.Lift()
	app.Post(func() {
		for range 20 {
			c.Scene.Step(time.Second / stepRate)
		}
	})
	app.Settle()
	if math.Abs(float64(c.Scene.Orbit.Yaw-yawBeforeTouch)) < 0.2 {
		t.Fatalf("open-area touch drag did not change yaw: before=%.3f after=%.3f", yawBeforeTouch, c.Scene.Orbit.Yaw)
	}

	// Mouse wheel zooms the camera in and out.
	distBefore := c.Scene.Orbit.Distance
	app.Wheel("id:lantern-viewport", core.Point{Y: -120}, core.Modifiers{})
	app.Post(func() {
		for range 20 {
			c.Scene.Step(time.Second / stepRate)
		}
	})
	app.Settle()
	if c.Scene.Orbit.Distance >= distBefore-0.1 {
		t.Fatalf("mouse wheel did not zoom in: before=%.3f after=%.3f", distBefore, c.Scene.Orbit.Distance)
	}

	// H key toggles HUD visibility.
	if !c.HUD.Visible() {
		t.Fatal("HUD should start visible")
	}
	app.Key("h")
	if c.HUD.Visible() {
		t.Fatal("H key did not hide HUD")
	}
	app.Key("h")
	if !c.HUD.Visible() {
		t.Fatal("second H key did not restore HUD")
	}
}

func TestDeviceLossRestoreAndCleanShutdown(t *testing.T) {
	var (
		c    *Controller
		show *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{
			When: show.Get,
			Then: func(cx *core.Scope) core.Node {
				var node core.Node
				c, node = NewView(cx)
				return node
			},
		}
	}, nsgotest.Size(128, 96), nsgotest.ManualFrames())

	app.Advance(32 * time.Millisecond)
	imgBefore, err := app.GPUReadPixels("id:lantern-gpu")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	if analyzeFrame(imgBefore).nonSkyPixels == 0 {
		t.Fatal("expected lit 3D pixels before device loss")
	}

	// Simulate GPU context loss and recovery.
	app.TriggerGPUDeviceLost("id:lantern-gpu", nsgotest.GPULostContextLost, "simulated context loss")
	if c.DeviceLost != 1 {
		t.Fatalf("DeviceLost = %d, want 1", c.DeviceLost)
	}

	app.RestoreGPUDevice("id:lantern-gpu")
	if c.DeviceRestored != 1 {
		t.Fatalf("DeviceRestored = %d, want 1", c.DeviceRestored)
	}

	app.Advance(32 * time.Millisecond)
	imgAfter, err := app.GPUReadPixels("id:lantern-gpu")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	if st := analyzeFrame(imgAfter); st.nonSkyPixels == 0 || st.distinctColors < 10 {
		t.Fatalf("restored frame missing 3D geometry: %+v", st)
	}

	// Clean unmount releases scope-owned GPU resources without panicking.
	app.Post(func() { show.Set(false) })
	app.Settle()
	if c.Renderer.Ready() {
		t.Fatal("renderer still reports Ready after scope disposal")
	}
}

func TestNoGPUFallbackShowsMessage(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(480, 320), nsgotest.NoGPU())

	fallback := app.Find("id:no-gpu-fallback")
	if fallback == nil {
		t.Fatal("expected no-gpu-fallback view under NoGPU")
	}
	msg := app.Find("id:no-gpu-message").Text
	if !strings.Contains(msg, "no GPU backend") {
		t.Fatalf("no-gpu-message = %q, want explanation of missing GPU backend", msg)
	}

	// HUD controls remain interactive even without a GPU backend.
	app.Press("id:btn-orbit")
	if c.Scene.AutoOrbit {
		t.Fatal("btn-orbit did not toggle AutoOrbit under NoGPU fallback")
	}
}
