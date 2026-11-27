package main

import (
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const fixedStep = time.Second / stepRate

func withTestAudio(t *testing.T) nsgotest.Option {
	t.Helper()
	srcs, err := audioSources()
	if err != nil {
		t.Fatalf("audioSources: %v", err)
	}
	return func(a *nsgotest.App) {
		nsgotest.Environment(core.Environment{Audio: true})(a)
		if a.AudioDurations == nil {
			a.AudioDurations = make(map[string]time.Duration)
		}
		a.AudioDurations[srcs.ambient.URI] = 4 * time.Second
		a.AudioDurations[string(srcs.ambient.Data)] = 4 * time.Second
		a.AudioDurations[srcs.chime.URI] = 290 * time.Millisecond
		a.AudioDurations[string(srcs.chime.Data)] = 290 * time.Millisecond
		a.AudioDurations[srcs.tick.URI] = 60 * time.Millisecond
		a.AudioDurations[string(srcs.tick.Data)] = 60 * time.Millisecond
	}
}

func soundPlayCount(app *nsgotest.App, uri string) int {
	for _, snd := range app.Sounds {
		if snd.Source.URI == uri {
			return len(snd.Plays)
		}
	}
	return 0
}

func findCanvasChildren(stage *nsgotest.View, excludeTestIDs ...string) []*nsgotest.View {
	var out []*nsgotest.View
	var walk func(v *nsgotest.View)
	walk = func(v *nsgotest.View) {
		if v == nil {
			return
		}
		if v.Kind == nsgotest.KindCanvas && !slices.Contains(excludeTestIDs, v.Props.TestID) {
			out = append(out, v)
		}
		for _, ch := range v.Children {
			walk(ch)
		}
	}
	walk(stage)
	return out
}

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
	if len(sc.Model.Nodes) < 10 {
		t.Fatalf("glTF nodes = %d, want >= 10", len(sc.Model.Nodes))
	}
	if len(sc.Model.Images) < 2 || len(sc.Model.Textures) < 2 {
		t.Fatalf("glTF images=%d textures=%d, want >= 2 each", len(sc.Model.Images), len(sc.Model.Textures))
	}
	// The level's scene file draws the whole diorama: a node for each of the model's nodes under the diorama object, every one that draws with its geometry's bounds.
	nodes := 0
	sc.Level.Objects["diorama"].Walk(func(*scene3d.Node) bool { nodes++; return true })
	if nodes != len(sc.Model.Nodes)+1 {
		t.Fatalf("diorama object has %d nodes, want the object and the model's %d", nodes, len(sc.Model.Nodes))
	}
	for _, d := range sc.Level.Drawables {
		if d.Object != "diorama" || d.Node.WorldBounds().Empty() {
			t.Fatalf("drawable %q of object %q has bounds %v", d.Node.Name, d.Object, d.Node.WorldBounds())
		}
	}
	for i := range sc.Model.Images {
		img, err := sc.Model.DecodeImage(i)
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
	if sc.wisp == nil || sc.core == nil {
		t.Fatalf("missing animated nodes: wisp=%v core=%v", sc.wisp, sc.core)
	}

	// Advancing with AnimateLights moves the orbiting point light.
	posBefore := sc.Lights.Point[wispLight].Position
	if err := sc.Step(500 * time.Millisecond); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if sc.Lights.Point[wispLight].Position.ApproxEqual(posBefore, 1e-4) {
		t.Fatalf("orbiting point light did not move: before=%v after=%v", posBefore, sc.Lights.Point[wispLight].Position)
	}
}

func TestLightsFollowTheirNodesUnderAMovedRoot(t *testing.T) {
	sc, err := NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	root := sc.World.Root().FindByName("CourtyardRoot")
	if root == nil {
		t.Fatal("the level lacks CourtyardRoot")
	}
	root.SetPosition(math3d.Vec3{X: 3, Y: 0.5, Z: -2})
	root.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.7))
	if err := sc.Step(fixedStep); err != nil {
		t.Fatalf("Step: %v", err)
	}

	wisp := sc.wisp.WorldMatrix().TransformPoint(math3d.Vec3{})
	if got := sc.Lights.Point[wispLight].Position; !got.ApproxEqual(wisp, 1e-4) {
		t.Fatalf("wisp light at %v, wisp mesh at %v", got, wisp)
	}
	// The wisp orbits in world space, whatever its parent's transform.
	if r := math.Hypot(float64(wisp.X), float64(wisp.Z)); math.Abs(r-1.95) > 1e-3 {
		t.Fatalf("wisp orbit radius = %.4f, want 1.95", r)
	}

	glow := sc.core.WorldMatrix().TransformPoint(shrineGlowLocal)
	if got := sc.Lights.Point[shrineLight].Position; !got.ApproxEqual(glow, 1e-4) {
		t.Fatalf("shrine glow light at %v, shrine core at %v", got, glow)
	}
	// The shrine stands at the root's origin, so its glow moves with the root.
	if math.Abs(float64(glow.X-3)) > 1e-3 || math.Abs(float64(glow.Z+2)) > 1e-3 {
		t.Fatalf("shrine glow at %v, want above the moved root at (3, _, -2)", glow)
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

func TestSwitch3DTo2DAndBackWithSharedInputAndSound(t *testing.T) {
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
			opts := []nsgotest.Option{
				nsgotest.Size(128, 96),
				nsgotest.ManualFrames(),
				withTestAudio(t),
			}
			opts = append(opts, p.opts...)

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				var node core.Node
				c, node = NewView(cx)
				c.SwitchFade = 60 * time.Millisecond
				return node
			}, opts...)

			// 1. Starts in 3D shrine with looping ambient music playing and 3D GPU pixels drawn.
			app.Advance(20 * time.Millisecond)
			if c.Mode() != Mode3D || c.ViewBuilds3D != 1 || c.DeviceReady != 1 {
				t.Fatalf("initial 3D state: mode=%v builds=%d ready=%d", c.Mode(), c.ViewBuilds3D, c.DeviceReady)
			}
			if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
				t.Fatalf("ambient music not playing in 3D: %+v", app.Players)
			}
			imgBefore, err := app.GPUReadPixels("id:lantern-gpu")
			if err != nil {
				t.Fatalf("GPUReadPixels initial 3D: %v", err)
			}
			if st := analyzeFrame(imgBefore); st.nonSkyPixels < 200 || st.warmLitPixels == 0 {
				t.Fatalf("initial 3D frame missing lit shrine geometry: %+v", st)
			}

			// 2. Shared input.Actions drives 3D camera orbit (Space disables auto-orbit, D orbits right).
			app.Key("space")
			app.Post(func() {
				c.Actions.Step()
				c.Manager.Update(fixedStep)
				c.Actions.Step()
				c.Scene.Orbit.Snap()
			})
			app.Settle()
			yawBefore := c.Scene.Orbit.Yaw
			app.KeyDown("d")
			app.Post(func() {
				c.Actions.Step()
				c.Manager.Update(fixedStep)
				c.Scene.Orbit.Snap()
			})
			app.Settle()
			app.KeyUp("d")
			if math.Abs(float64(c.Scene.Orbit.Yaw-yawBefore-keyOrbitStep)) > 1e-4 {
				t.Fatalf("shared Actions did not orbit 3D camera: yaw %.4f -> %.4f", yawBefore, c.Scene.Orbit.Yaw)
			}

			// 3. Hold Tab to fade 3D -> 2D; Actions.Reset() prevents bouncing back while Tab stays held.
			app.KeyDown("tab")
			app.Advance(20 * time.Millisecond)
			if !c.Manager.Transitioning() || c.Mode() != Mode2D {
				t.Fatalf("after Tab in 3D: transitioning=%v mode=%v", c.Manager.Transitioning(), c.Mode())
			}
			if got := soundPlayCount(app, switchChimeURI); got != 1 {
				t.Fatalf("switch chime plays after 3D->2D = %d, want 1", got)
			}
			app.Advance(c.SwitchFade + 20*time.Millisecond)
			app.KeyUp("tab")

			if c.Manager.Transitioning() || c.Mode() != Mode2D {
				t.Fatalf("after 3D->2D fade: transitioning=%v mode=%v (held Tab should not bounce back)", c.Manager.Transitioning(), c.Mode())
			}
			if c.ViewCleanups3D != 1 || c.Renderer.Ready() {
				t.Fatalf("3D view did not unmount cleanly on exit: cleanups=%d ready=%v", c.ViewCleanups3D, c.Renderer.Ready())
			}
			if got := len(app.Query("id:lantern-gpu")); got != 0 {
				t.Fatalf("expected 0 GPU views in 2D map, got %d", got)
			}
			canvases := findCanvasChildren(app.Find("id:lantern-stage"), "lantern-transition")
			if len(canvases) != 1 || len(canvases[0].Ops) == 0 {
				t.Fatalf("expected 1 active 2D map canvas with draw ops, got %d canvases", len(canvases))
			}

			// 4. Shared ambient music stayed playing uninterrupted across 3D -> 2D!
			if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
				t.Fatalf("ambient music interrupted across 3D->2D: %+v", app.Players)
			}

			// 5. The SAME shared input.Actions ("w") moves the 2D traveler north into the shrine portal!
			posBefore := c.Map.TravelerPos()
			app.KeyDown("w")
			app.Advance(500 * time.Millisecond)
			app.KeyUp("w")
			app.Advance(20 * time.Millisecond)

			posAfter := c.Map.TravelerPos()
			if posAfter.Y >= posBefore.Y-30 {
				t.Fatalf("shared Actions W did not move 2D traveler north: before=%v after=%v", posBefore, posAfter)
			}
			if !c.Map.NearPortal() {
				t.Fatalf("traveler at %v should have reached shrine portal", posAfter)
			}

			// 6. Pressing E on the shrine portal fades 2D -> 3D, remounting gpu.View cleanly!
			app.KeyDown("e")
			app.Advance(20 * time.Millisecond)
			app.KeyUp("e")
			if !c.Manager.Transitioning() || c.Mode() != Mode3D {
				t.Fatalf("pressing E at portal did not switch 2D->3D: transitioning=%v mode=%v", c.Manager.Transitioning(), c.Mode())
			}
			if got := soundPlayCount(app, switchChimeURI); got != 2 {
				t.Fatalf("switch chime plays after 2D->3D = %d, want 2", got)
			}
			app.Advance(c.SwitchFade + 20*time.Millisecond)

			if c.Manager.Transitioning() || c.Mode() != Mode3D {
				t.Fatalf("after 2D->3D fade: transitioning=%v mode=%v", c.Manager.Transitioning(), c.Mode())
			}
			if c.ViewBuilds3D != 2 || c.DeviceReady != 2 || !c.Renderer.Ready() {
				t.Fatalf("3D view did not remount cleanly: builds=%d readyCount=%d ready=%v", c.ViewBuilds3D, c.DeviceReady, c.Renderer.Ready())
			}
			imgAfter, err := app.GPUReadPixels("id:lantern-gpu")
			if err != nil {
				t.Fatalf("GPUReadPixels after 2D->3D return: %v", err)
			}
			if st := analyzeFrame(imgAfter); st.nonSkyPixels < 200 || st.warmLitPixels == 0 {
				t.Fatalf("remounted 3D frame missing lit geometry: %+v", st)
			}
			if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
				t.Fatalf("ambient music interrupted across 2D->3D: %+v", app.Players)
			}
		})
	}
}

func TestSwitch2DTo3DTo2DViaHUDButtonAndPauseOverlayPreserves3D(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = newView(cx, Mode2D)
		c.SwitchFade = 40 * time.Millisecond
		return node
	}, nsgotest.Size(280, 140), nsgotest.ManualFrames(), withTestAudio(t))

	// Starts in 2D Map with 0 GPU views mounted and ambient music playing.
	app.Advance(20 * time.Millisecond)
	if c.Mode() != Mode2D || c.ViewBuilds3D != 0 {
		t.Fatalf("initial 2D start: mode=%v builds3D=%d", c.Mode(), c.ViewBuilds3D)
	}
	if app.Find("id:hud-title").Text != "COURTYARD 2D" {
		t.Fatalf("hud-title in 2D = %q, want COURTYARD 2D", app.Find("id:hud-title").Text)
	}

	// HUD button switches 2D -> 3D.
	app.Press("id:btn-switch-scene")
	app.Advance(c.SwitchFade + 20*time.Millisecond)
	if c.Mode() != Mode3D || c.ViewBuilds3D != 1 || c.DeviceReady != 1 || !c.Renderer.Ready() {
		t.Fatalf("after HUD 2D->3D: mode=%v builds=%d readyCount=%d ready=%v", c.Mode(), c.ViewBuilds3D, c.DeviceReady, c.Renderer.Ready())
	}
	if app.Find("id:hud-title").Text != "LANTERN 3D" {
		t.Fatalf("hud-title in 3D = %q, want LANTERN 3D", app.Find("id:hud-title").Text)
	}

	// Push transparent 2D pause overlay over 3D: 3D gpu.View stays mounted & alive while simulation pauses!
	stepsBeforePause := c.Scene.Steps
	app.Press("id:btn-pause")
	app.Advance(20 * time.Millisecond)
	if !c.Paused() || c.Shrine3DPauses != 1 || app.Find("id:pause-banner") == nil {
		t.Fatalf("pause overlay did not push cleanly: paused=%v pauses=%d", c.Paused(), c.Shrine3DPauses)
	}
	app.Post(func() {
		for range 10 {
			c.Actions.Step()
			c.Manager.Update(fixedStep)
		}
	})
	if c.Scene.Steps != stepsBeforePause {
		t.Fatalf("3D scene stepped while paused: before=%d after=%d", stepsBeforePause, c.Scene.Steps)
	}
	if !c.Renderer.Ready() || c.ViewBuilds3D != 1 || c.ViewCleanups3D != 0 {
		t.Fatalf("pushing 2D pause overlay tore down 3D GPU view: ready=%v builds=%d cleanups=%d", c.Renderer.Ready(), c.ViewBuilds3D, c.ViewCleanups3D)
	}

	// Pop pause overlay via Resume button: 3D resumes with ZERO GPU rebuilds!
	app.Press("id:btn-resume")
	app.Advance(20 * time.Millisecond)
	if c.Paused() || c.Shrine3DResumes != 1 {
		t.Fatalf("resume failed: paused=%v resumes=%d", c.Paused(), c.Shrine3DResumes)
	}
	if c.ViewBuilds3D != 1 || c.DeviceReady != 1 || !c.Renderer.Ready() {
		t.Fatalf("popping pause overlay rebuilt 3D GPU view: builds=%d readyCount=%d ready=%v", c.ViewBuilds3D, c.DeviceReady, c.Renderer.Ready())
	}
	if c.Scene.Steps <= stepsBeforePause {
		t.Fatalf("3D scene did not resume stepping after pop: before=%d after=%d", stepsBeforePause, c.Scene.Steps)
	}

	// Switch back 3D -> 2D via HUD button; shared music stays uninterrupted and mute button toggles master volume across scenes.
	app.Press("id:btn-switch-scene")
	app.Advance(c.SwitchFade + 20*time.Millisecond)
	if c.Mode() != Mode2D || c.ViewCleanups3D != 1 {
		t.Fatalf("after 3D->2D button: mode=%v cleanups=%d", c.Mode(), c.ViewCleanups3D)
	}
	if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
		t.Fatalf("music interrupted across 2D->3D->pause->2D: %+v", app.Players)
	}

	app.Press("id:btn-mute")
	if !c.Audio.Muted() || c.Audio.Mixer.Master() != 0 {
		t.Fatalf("mute button did not mute shared mixer: muted=%v master=%v", c.Audio.Muted(), c.Audio.Mixer.Master())
	}
	app.Press("id:btn-mute")
	if c.Audio.Muted() || c.Audio.Mixer.Master() != 1 {
		t.Fatalf("second mute press did not unmute shared mixer: muted=%v master=%v", c.Audio.Muted(), c.Audio.Mixer.Master())
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

	// Orbit significantly via shared input.Actions right arrow + zoom in.
	for range 8 {
		app.Key("right")
		app.Post(func() {
			c.Actions.Step()
			c.Manager.Update(fixedStep)
			c.Actions.Step()
		})
		app.Settle()
	}
	app.Key("=")
	app.Post(func() {
		c.Actions.Step()
		c.Manager.Update(fixedStep)
		c.Actions.Step()
		for range 24 {
			if err := c.Scene.Step(fixedStep); err != nil {
				t.Errorf("Step: %v", err)
			}
		}
	})
	app.Settle()
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
			if err := c.Scene.Step(fixedStep); err != nil {
				t.Errorf("Step: %v", err)
			}
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
			if err := c.Scene.Step(fixedStep); err != nil {
				t.Errorf("Step: %v", err)
			}
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
			if err := c.Scene.Step(fixedStep); err != nil {
				t.Errorf("Step: %v", err)
			}
		}
	})
	app.Settle()
	if c.Scene.Orbit.Distance >= distBefore-0.1 {
		t.Fatalf("mouse wheel did not zoom in: before=%.3f after=%.3f", distBefore, c.Scene.Orbit.Distance)
	}

	// H key toggles HUD visibility via shared input.Actions.
	if !c.HUD.Visible() {
		t.Fatal("HUD should start visible")
	}
	stepKey := func(k string) {
		app.Key(k)
		app.Post(func() {
			c.Actions.Step()
			if c.Actions.JustPressed("toggle_hud") {
				c.HUD.Toggle()
			}
			c.Actions.Step()
		})
		app.Settle()
	}
	stepKey("h")
	if c.HUD.Visible() {
		t.Fatal("H key did not hide HUD")
	}
	stepKey("h")
	if !c.HUD.Visible() {
		t.Fatal("second H key did not restore HUD")
	}
}

func TestPinchZoomsAndExtraFingersDoNotOrbit(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(480, 320), nsgotest.ManualFrames())
	app.Press("id:btn-orbit")

	// A pinch to twice the size halves the distance it began at.
	start := c.Scene.Orbit.Distance
	app.Pinch("id:lantern-viewport", 1, 1.5, 2)
	app.Post(func() {
		c.Scene.Orbit.Snap()
	})
	app.Settle()
	if want := start / 2; math.Abs(float64(c.Scene.Orbit.Distance-want)) > 1e-3 {
		t.Fatalf("pinch to 2x: distance = %.3f, want %.3f", c.Scene.Orbit.Distance, want)
	}

	// Three fingers moving together belong to the pinch: they must not orbit, and lifting them in any order must leave no finger behind.
	yaw := c.Scene.Orbit.Yaw
	three := func(phase core.TouchPhase, dx core.Points, changed int) core.TouchEvent {
		ev := core.TouchEvent{Phase: phase}
		for id := 1; id <= 3; id++ {
			ev.Touches = append(ev.Touches, core.Touch{
				ID:       id,
				Position: core.Point{X: core.Points(100*id) + dx, Y: 150},
				Changed:  changed == 0 || changed == id,
			})
		}
		return ev
	}
	app.Post(func() {
		c.onTouch(three(core.TouchStart, 0, 0))
		for step := range 10 {
			c.onTouch(three(core.TouchMove, core.Points(step*12), 0))
		}
		if !c.Scene.Dragging {
			t.Error("Dragging = false with three fingers down")
		}
		c.onTouch(three(core.TouchEnd, 120, 2))
		c.onTouch(three(core.TouchEnd, 120, 0))
		for range 20 {
			if err := c.Scene.Step(fixedStep); err != nil {
				t.Errorf("Step: %v", err)
			}
		}
	})
	app.Settle()
	if math.Abs(float64(c.Scene.Orbit.Yaw-yaw)) > 1e-4 {
		t.Fatalf("three-finger move orbited the camera: yaw %.4f -> %.4f", yaw, c.Scene.Orbit.Yaw)
	}
	if len(c.activeTouches) != 0 || c.Scene.Dragging {
		t.Fatalf("after lifting every finger: activeTouches=%v Dragging=%v", c.activeTouches, c.Scene.Dragging)
	}
}

func TestKeyboardControls(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(480, 320), nsgotest.ManualFrames())

	pressAndStep := func(key string) {
		t.Helper()
		app.Key(key)
		app.Post(func() {
			c.Actions.Step()
			c.Manager.Update(fixedStep)
			c.Actions.Step()
			c.Scene.Orbit.Snap()
		})
		app.Settle()
	}

	pressAndStep("space")
	if c.Scene.AutoOrbit || app.Find("Orbit: Off") == nil {
		t.Fatalf("Space did not turn auto-orbit off: AutoOrbit=%v", c.Scene.AutoOrbit)
	}
	pressAndStep("space")
	if !c.Scene.AutoOrbit {
		t.Fatal("second Space did not turn auto-orbit back on")
	}
	pressAndStep("space")

	pressAndStep("l")
	if c.Scene.AnimateLights || app.Find("Lights: Off") == nil {
		t.Fatalf("L did not stop the lights: AnimateLights=%v", c.Scene.AnimateLights)
	}

	for _, k := range []struct {
		keys        []string
		yaw, pitch  float32
		zoomOutSign float32
	}{
		{keys: []string{"left", "a"}, yaw: -keyOrbitStep},
		{keys: []string{"right", "d"}, yaw: keyOrbitStep},
		{keys: []string{"up", "w"}, pitch: keyOrbitStep},
		{keys: []string{"down", "s"}, pitch: -keyOrbitStep},
		{keys: []string{"=", "shift+="}, zoomOutSign: -1},
		{keys: []string{"-"}, zoomOutSign: 1},
	} {
		for _, key := range k.keys {
			yaw, pitch, dist := c.Scene.Orbit.Yaw, c.Scene.Orbit.Pitch, c.Scene.Orbit.Distance
			pressAndStep(key)
			if d := c.Scene.Orbit.Yaw - yaw; math.Abs(float64(d-k.yaw)) > 1e-4 {
				t.Errorf("%s: yaw moved %.4f, want %.4f", key, d, k.yaw)
			}
			if d := c.Scene.Orbit.Pitch - pitch; math.Abs(float64(d-k.pitch)) > 1e-4 {
				t.Errorf("%s: pitch moved %.4f, want %.4f", key, d, k.pitch)
			}
			if d := c.Scene.Orbit.Distance - dist; math.Abs(float64(d-k.zoomOutSign*keyZoomStep)) > 1e-4 {
				t.Errorf("%s: distance moved %.4f, want %.4f", key, d, k.zoomOutSign*keyZoomStep)
			}
		}
	}

	pressAndStep("r")
	if math.Abs(float64(c.Scene.Orbit.Yaw-defaultOrbitYaw)) > 1e-3 ||
		math.Abs(float64(c.Scene.Orbit.Pitch-defaultOrbitPitch)) > 1e-3 ||
		math.Abs(float64(c.Scene.Orbit.Distance-defaultOrbitDist)) > 1e-3 {
		t.Fatalf("R did not reset the camera: yaw=%.3f pitch=%.3f dist=%.3f", c.Scene.Orbit.Yaw, c.Scene.Orbit.Pitch, c.Scene.Orbit.Distance)
	}
}

func TestMouseLeaveEndsDrag(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(480, 320), nsgotest.ManualFrames(),
		nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.HoverAt("id:lantern-root", core.Point{X: 220, Y: 170})
	app.MouseDownAt("id:lantern-root", core.Point{X: 220, Y: 170}, core.MouseButtonLeft)
	if !c.mouseDragging || !c.Scene.Dragging {
		t.Fatal("MouseDown on the open 3D area did not start a drag")
	}
	app.HoverOut()
	if c.mouseDragging || c.Scene.Dragging {
		t.Fatalf("leaving the view kept the drag: mouseDragging=%v Dragging=%v", c.mouseDragging, c.Scene.Dragging)
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

func TestNoGPUFallbackShowsMessageAndAllowsSwitchingTo2D(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx)
		return node
	}, nsgotest.Size(480, 320), nsgotest.ManualFrames(), nsgotest.NoGPU())

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

	// Switching to the 2D Courtyard Map works cleanly even without a GPU backend!
	app.Press("id:btn-switch-scene")
	app.Advance(switchFadeDuration + 40*time.Millisecond)
	if c.Mode() != Mode2D {
		t.Fatalf("mode after switch under NoGPU = %v, want Mode2D", c.Mode())
	}
	if len(app.Query("id:no-gpu-fallback")) != 0 {
		t.Fatal("no-gpu-fallback banner should hide in 2D Map mode")
	}
	canvases := findCanvasChildren(app.Find("id:lantern-stage"), "lantern-transition")
	if len(canvases) != 1 || len(canvases[0].Ops) == 0 {
		t.Fatalf("expected 1 active 2D map canvas under NoGPU, got %d canvases", len(canvases))
	}
}
