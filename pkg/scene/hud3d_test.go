package scene_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

func viewCenter(v *nsgotest.View) core.Point {
	b := v.Bounds()
	return core.Point{
		X: b.Origin.X + b.Size.Width/2,
		Y: b.Origin.Y + b.Size.Height/2,
	}
}

func TestHUD3DMouseAndTouchOverGPUView(t *testing.T) {
	var (
		pauseClicks int
		jumpClicks  int
		gpuPresses  []core.Point
		gpuReady    bool
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		hud := scene.NewHUD3D(cx)
		hud.TestID = "hud3d"
		hud.Padding = core.All(12)

		hud.AddPassThrough(scene.HUDTopLeft, ui.Badge{
			Text: "HP 100",
		})
		hud.AddPassThrough(scene.HUDCenter, core.View{
			TestID: "crosshair",
			Style:  core.Style{Width: core.Pt(16), Height: core.Pt(16)},
		})
		hud.AddAt(scene.HUDTopRight, ui.Button{
			TestID:  "pause-btn",
			Text:    "Pause",
			OnPress: func() { pauseClicks++ },
		})
		hud.AddAt(scene.HUDBottomRight, ui.Button{
			TestID:  "jump-btn",
			Text:    "Jump",
			OnPress: func() { jumpClicks++ },
		})

		return hud.View(cx, gpu.ViewProps{
			TestID:      "gpu3d",
			DepthFormat: gpu.FormatDepth24Plus,
			Style:       core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			OnReady: func(dev *gpu.Device) {
				gpuReady = dev != nil
			},
			OnFrame: func(frame *gpu.Frame) {
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{
					ColorAttachments: []gpu.ColorAttachment{{
						LoadOp:     gpu.LoadClear,
						StoreOp:    gpu.StoreStore,
						ClearColor: core.RGB(0x1a334d),
					}},
				})
				pass.End()
			},
			OnPress: func(ev core.PressEvent) {
				gpuPresses = append(gpuPresses, ev.Position)
			},
		})
	}, nsgotest.Size(400, 300))

	if !gpuReady {
		t.Fatal("gpu.View did not report OnReady")
	}

	// Verify HUD layer covers the 3D view with PointerEventsBoxNone.
	hudView := app.Find("id:hud3d")
	if hudView.Props.PointerEvents != core.PointerEventsBoxNone {
		t.Fatalf("hud PointerEvents = %v, want PointerEventsBoxNone", hudView.Props.PointerEvents)
	}

	pauseBtn := app.Find("id:pause-btn")
	jumpBtn := app.Find("id:jump-btn")
	pausePos := viewCenter(pauseBtn)
	jumpPos := viewCenter(jumpBtn)

	// 1. Mouse click on Pause button hits the HUD control, NOT the 3D view.
	app.Press("id:pause-btn")
	app.PressAt("id:gpu3d", pausePos)
	if pauseClicks != 2 {
		t.Fatalf("pauseClicks = %d, want 2", pauseClicks)
	}
	if len(gpuPresses) != 0 {
		t.Fatalf("clicking Pause leaked to gpu.View: %v", gpuPresses)
	}

	// 2. Touch tap on Jump button hits the HUD control, NOT the 3D view.
	f := app.Touch("id:gpu3d", jumpPos)
	f.Lift()
	if jumpClicks != 1 {
		t.Fatalf("jumpClicks = %d, want 1", jumpClicks)
	}
	if len(gpuPresses) != 0 {
		t.Fatalf("touching Jump leaked to gpu.View: %v", gpuPresses)
	}

	// 3. Mouse click on open 3D space passes straight through to gpu.View.
	openPt := core.Point{X: 120, Y: 180}
	app.PressAt("id:gpu3d", openPt)
	if len(gpuPresses) != 1 || gpuPresses[0] != openPt {
		t.Fatalf("mouse click on open space: gpuPresses = %v, want [%v]", gpuPresses, openPt)
	}

	// 4. Touch tap on open 3D space passes straight through to gpu.View.
	openTouchPt := core.Point{X: 260, Y: 140}
	tf := app.Touch("id:gpu3d", openTouchPt)
	tf.Lift()
	if len(gpuPresses) != 2 || gpuPresses[1] != openTouchPt {
		t.Fatalf("touch tap on open space: gpuPresses = %v, want second point %v", gpuPresses, openTouchPt)
	}

	// 5. Mouse click and touch tap on PassThrough decorative HUD slots pass through to gpu.View.
	crosshairPos := viewCenter(app.Find("id:crosshair"))
	app.PressAt("id:gpu3d", crosshairPos)
	cf := app.Touch("id:gpu3d", crosshairPos)
	cf.Lift()
	if len(gpuPresses) != 4 {
		t.Fatalf("PassThrough slot clicks: gpuPresses len = %d, want 4 (%v)", len(gpuPresses), gpuPresses)
	}
	if pauseClicks != 2 || jumpClicks != 1 {
		t.Fatalf("unexpected HUD clicks after pass-through: pause=%d jump=%d", pauseClicks, jumpClicks)
	}
}

func TestHUD3DWrapMouseDownMoveAndTouchPassThrough(t *testing.T) {
	var (
		btnClicks      int
		soundEnabled   *core.Signal[bool]
		scenePresses   int
		sceneMouseDown []core.MouseButtonEvent
		sceneMouseUp   []core.MouseButtonEvent
		sceneMoves     int
		sceneTouches   []core.TouchPhase
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		soundEnabled = core.NewSignal(cx, true)

		hud := scene.NewHUD3D(cx)
		hud.TestID = "hud-layer"
		hud.Padding = core.All(10)
		hud.AddAt(scene.HUDTopRight, ui.Button{
			TestID:  "action-btn",
			Text:    "Fire",
			OnPress: func() { btnClicks++ },
		})
		hud.AddAt(scene.HUDBottomLeft, ui.Switch{
			TestID:  "sound-switch",
			Label:   "Sound",
			Checked: soundEnabled.Get,
			OnChange: func(v bool) {
				soundEnabled.Set(v)
			},
		})

		view3D := core.View{
			TestID: "viewport3d",
			Style:  core.Style{FlexGrow: 1, FlexShrink: 1},
			OnPress: func(core.PressEvent) {
				scenePresses++
			},
			OnMouseDown: func(ev core.MouseButtonEvent) {
				sceneMouseDown = append(sceneMouseDown, ev)
			},
			OnMouseUp: func(ev core.MouseButtonEvent) {
				sceneMouseUp = append(sceneMouseUp, ev)
			},
			OnMouseMove: func(core.MouseEvent) {
				sceneMoves++
			},
			OnTouch: func(ev core.TouchEvent) {
				sceneTouches = append(sceneTouches, ev.Phase)
			},
			Children: []core.Node{
				gpu.View(cx, gpu.ViewProps{
					TestID:      "gpu-surface",
					DepthFormat: gpu.FormatDepth24Plus,
					Style:       core.Style{FlexGrow: 1, FlexShrink: 1},
				}),
			},
		}

		return core.View{
			TestID: "root",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{
				hud.Wrap(cx, view3D),
			},
		}
	}, nsgotest.Size(400, 300), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	btnCenter := viewCenter(app.Find("id:action-btn"))
	openPt := core.Point{X: 160, Y: 150}

	// Mouse down/up on the HUD button must NOT leak into the 3D scene's OnMouseDown.
	app.MouseDownAt("id:root", btnCenter, core.MouseButtonLeft)
	app.MouseUpAt("id:root", btnCenter, core.MouseButtonLeft)
	if len(sceneMouseDown) != 0 || len(sceneMouseUp) != 0 {
		t.Fatalf("mouse down/up on HUD button leaked to 3D viewport: down=%d up=%d", len(sceneMouseDown), len(sceneMouseUp))
	}

	// Hovering over the HUD button must NOT fire 3D viewport OnMouseMove.
	app.HoverAt("id:root", btnCenter)
	if sceneMoves != 0 {
		t.Fatalf("hover over HUD button leaked to 3D viewport: moves=%d", sceneMoves)
	}

	// Touching the HUD button taps the button and must NOT leak OnTouch to the 3D viewport.
	bf := app.Touch("id:root", btnCenter)
	bf.Lift()
	if btnClicks != 1 {
		t.Fatalf("btnClicks = %d, want 1", btnClicks)
	}
	if len(sceneTouches) != 0 || scenePresses != 0 {
		t.Fatalf("touch on HUD button leaked to 3D viewport: touches=%v presses=%d", sceneTouches, scenePresses)
	}

	// Native ui.Switch toggles via press and touch over the 3D scene.
	app.Press("id:sound-switch")
	if soundEnabled.GetUntracked() {
		t.Fatal("sound switch did not toggle off on mouse press")
	}
	swCenter := viewCenter(app.Find("id:sound-switch"))
	sf := app.Touch("id:root", swCenter)
	sf.Lift()
	if !soundEnabled.GetUntracked() {
		t.Fatal("sound switch did not toggle back on via touch")
	}
	if len(sceneTouches) != 0 || scenePresses != 0 {
		t.Fatalf("switch interaction leaked to 3D viewport: touches=%v presses=%d", sceneTouches, scenePresses)
	}

	// Open 3D region: mouse down/up, hover, and touch drag ALL reach the 3D viewport.
	app.MouseDownAt("id:root", openPt, core.MouseButtonRight)
	app.HoverAt("id:root", core.Point{X: 180, Y: 160})
	app.MouseUpAt("id:root", core.Point{X: 180, Y: 160}, core.MouseButtonRight)
	if len(sceneMouseDown) != 1 || sceneMouseDown[0].Button != core.MouseButtonRight {
		t.Fatalf("open region mouse down: got %+v", sceneMouseDown)
	}
	if len(sceneMouseUp) != 1 || sceneMouseUp[0].Button != core.MouseButtonRight {
		t.Fatalf("open region mouse up: got %+v", sceneMouseUp)
	}
	if sceneMoves == 0 {
		t.Fatal("open region hover did not reach 3D viewport OnMouseMove")
	}

	// Touch drag (orbit gesture) on open 3D region delivers start/move/end to 3D viewport.
	drag := app.Touch("id:root", openPt)
	drag.Move(core.Point{X: 220, Y: 175})
	drag.Lift()
	if len(sceneTouches) < 3 || sceneTouches[0] != core.TouchStart || sceneTouches[len(sceneTouches)-1] != core.TouchEnd {
		t.Fatalf("open region touch drag: got phases %v", sceneTouches)
	}
}

func TestHUD3DReactiveUpdates(t *testing.T) {
	var (
		score       *core.Signal[int]
		health      *core.Signal[float32]
		readyCount  int
		actionLabel *core.Signal[string]
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		score = core.NewSignal(cx, 0)
		health = core.NewSignal[float32](cx, 1.0)
		actionLabel = core.NewSignal(cx, "Boost")

		hud := scene.NewHUD3D(cx)
		hud.Padding = core.All(8)
		hud.AddPassThrough(scene.HUDTopLeft, core.View{
			Style: core.Style{Gap: 4},
			Children: []core.Node{
				core.Text{
					TestID:      "score-text",
					ContentFunc: func() string { return fmt.Sprintf("Score: %d", score.Get()) },
				},
				ui.Progress{
					TestID: "hp-bar",
					Value:  health.Get,
					Style:  core.Style{Width: core.Pt(120)},
				},
			},
		})
		hud.AddAt(scene.HUDBottomRight, ui.Button{
			TestID:   "boost-btn",
			TextFunc: actionLabel.Get,
			OnPress: func() {
				score.Set(score.GetUntracked() + 10)
				health.Set(0.5)
				actionLabel.Set("Charging")
			},
		})

		return hud.View(cx, gpu.ViewProps{
			TestID: "gpu3d",
			Style:  core.Style{Width: core.Pt(320), Height: core.Pt(240)},
			OnReady: func(*gpu.Device) {
				readyCount++
			},
		})
	}, nsgotest.Size(320, 240))

	if got := app.Find("id:score-text").Text; got != "Score: 0" {
		t.Fatalf("initial score text = %q, want %q", got, "Score: 0")
	}

	app.Press("id:boost-btn")

	if got := app.Find("id:score-text").Text; got != "Score: 10" {
		t.Fatalf("updated score text = %q, want %q", got, "Score: 10")
	}
	if got := app.Find("id:boost-btn").Props.Label; got != "Charging" {
		t.Fatalf("updated button label = %q, want %q", got, "Charging")
	}

	// External signal update (e.g., from game loop) updates HUD without remounting gpu.View.
	app.Post(func() {
		score.Set(250)
	})
	app.Settle()
	if got := app.Find("id:score-text").Text; got != "Score: 250" {
		t.Fatalf("loop score text = %q, want %q", got, "Score: 250")
	}
	if readyCount != 1 {
		t.Fatalf("gpu.View OnReady count = %d, want 1 (must not remount on HUD signal updates)", readyCount)
	}
}

func TestHUD3DVisibilityToggle(t *testing.T) {
	var (
		hud        *scene.HUD3D
		btnClicks  int
		gpuPresses int
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		hud = scene.NewHUD3D(cx)
		hud.TestID = "hud"
		hud.Padding = core.All(10)
		hud.AddAt(scene.HUDTopRight, ui.Button{
			TestID:  "menu-btn",
			Text:    "Menu",
			OnPress: func() { btnClicks++ },
		})

		return hud.View(cx, gpu.ViewProps{
			TestID: "gpu3d",
			Style:  core.Style{Width: core.Pt(320), Height: core.Pt(240)},
			OnPress: func(core.PressEvent) {
				gpuPresses++
			},
		})
	}, nsgotest.Size(320, 240))

	if !hud.Visible() {
		t.Fatal("HUD should start visible")
	}

	btnCenter := viewCenter(app.Find("id:menu-btn"))

	// Visible: clicking the button hits the button.
	app.PressAt("id:gpu3d", btnCenter)
	if btnClicks != 1 || gpuPresses != 0 {
		t.Fatalf("visible click: btn=%d gpu=%d, want 1, 0", btnClicks, gpuPresses)
	}

	// Hide HUD: both mouse click and touch at the button's former position reach gpu.View.
	app.Post(func() {
		hud.SetHidden(true)
	})
	app.Settle()
	if hud.Visible() {
		t.Fatal("HUD should report hidden after SetHidden(true)")
	}
	if got := app.Query("id:menu-btn"); len(got) != 0 {
		t.Fatalf("hidden HUD button still visible to Query: %d", len(got))
	}

	app.PressAt("id:gpu3d", btnCenter)
	tf := app.Touch("id:gpu3d", btnCenter)
	tf.Lift()
	if btnClicks != 1 || gpuPresses != 2 {
		t.Fatalf("hidden input at button pos: btn=%d gpu=%d, want 1, 2", btnClicks, gpuPresses)
	}

	// Toggle back to visible: button is interactive again.
	var toggled bool
	app.Post(func() {
		toggled = hud.Toggle()
	})
	app.Settle()
	if !toggled || !hud.Visible() {
		t.Fatalf("Toggle() = %v, Visible() = %v, want true, true", toggled, hud.Visible())
	}

	app.PressAt("id:gpu3d", btnCenter)
	if btnClicks != 2 || gpuPresses != 2 {
		t.Fatalf("re-shown click: btn=%d gpu=%d, want 2, 2", btnClicks, gpuPresses)
	}

	// SetVisible(false) hides again.
	app.Post(func() {
		hud.SetVisible(false)
	})
	app.Settle()
	if hud.Visible() {
		t.Fatal("HUD should be hidden after SetVisible(false)")
	}
}

func TestHUD3DSafeAreaInsetsAndAnchors(t *testing.T) {
	var hud *scene.HUD3D

	const (
		winW = 400
		winH = 300
		pad  = 8
	)
	portraitSafe := core.Edges{Top: 44, Right: 12, Bottom: 34, Left: 16}

	box := func(id string) core.Node {
		return core.View{
			TestID: id,
			Style:  core.Style{Width: core.Pt(40), Height: core.Pt(20)},
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		hud = scene.NewHUD3D(cx)
		hud.TestID = "hud"
		hud.Padding = core.All(pad)

		hud.AddAt(scene.HUDTopLeft, box("tl"))
		hud.AddAt(scene.HUDTopCenter, box("tc"))
		hud.AddAt(scene.HUDTopRight, box("tr"))
		hud.AddAt(scene.HUDCenterLeft, box("cl"))
		hud.AddAt(scene.HUDCenter, box("cc"))
		hud.AddAt(scene.HUDCenterRight, box("cr"))
		hud.AddAt(scene.HUDBottomLeft, box("bl"))
		hud.AddAt(scene.HUDBottomCenter, box("bc"))
		hud.AddAt(scene.HUDBottomRight, box("br"))

		return hud.View(cx, gpu.ViewProps{
			TestID: "gpu3d",
			Style:  core.Style{Width: core.Pt(winW), Height: core.Pt(winH)},
		})
	}, nsgotest.Size(winW, winH), nsgotest.Environment(core.Environment{
		OS:       core.OSIOS,
		SafeArea: portraitSafe,
	}))

	near := func(name string, got, want core.Points) {
		t.Helper()
		if math.Abs(float64(got-want)) > 0.5 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	checkAnchors := func(label string, safe core.Edges) {
		t.Helper()
		left := safe.Left + pad
		top := safe.Top + pad
		right := core.Points(winW) - safe.Right - pad
		bottom := core.Points(winH) - safe.Bottom - pad
		midX := (left + right) / 2
		midY := (top + bottom) / 2

		tl := app.Find("id:tl").Bounds()
		near(label+" tl.X", tl.Origin.X, left)
		near(label+" tl.Y", tl.Origin.Y, top)

		tc := app.Find("id:tc").Bounds()
		near(label+" tc.CenterX", tc.Origin.X+tc.Size.Width/2, midX)
		near(label+" tc.Y", tc.Origin.Y, top)

		tr := app.Find("id:tr").Bounds()
		near(label+" tr.Right", tr.Origin.X+tr.Size.Width, right)
		near(label+" tr.Y", tr.Origin.Y, top)

		cl := app.Find("id:cl").Bounds()
		near(label+" cl.X", cl.Origin.X, left)
		near(label+" cl.CenterY", cl.Origin.Y+cl.Size.Height/2, midY)

		cc := app.Find("id:cc").Bounds()
		near(label+" cc.CenterX", cc.Origin.X+cc.Size.Width/2, midX)
		near(label+" cc.CenterY", cc.Origin.Y+cc.Size.Height/2, midY)

		cr := app.Find("id:cr").Bounds()
		near(label+" cr.Right", cr.Origin.X+cr.Size.Width, right)
		near(label+" cr.CenterY", cr.Origin.Y+cr.Size.Height/2, midY)

		bl := app.Find("id:bl").Bounds()
		near(label+" bl.X", bl.Origin.X, left)
		near(label+" bl.Bottom", bl.Origin.Y+bl.Size.Height, bottom)

		bc := app.Find("id:bc").Bounds()
		near(label+" bc.CenterX", bc.Origin.X+bc.Size.Width/2, midX)
		near(label+" bc.Bottom", bc.Origin.Y+bc.Size.Height, bottom)

		br := app.Find("id:br").Bounds()
		near(label+" br.Right", br.Origin.X+br.Size.Width, right)
		near(label+" br.Bottom", br.Origin.Y+br.Size.Height, bottom)
	}

	if !hud.SafeAreaEnabled() {
		t.Fatal("SafeAreaEnabled should default to true")
	}
	checkAnchors("portrait", portraitSafe)

	// Dynamic environment change (e.g., landscape rotation with side notches).
	landscapeSafe := core.Edges{Top: 0, Right: 44, Bottom: 21, Left: 44}
	app.SetEnvironment(core.Environment{OS: core.OSIOS, SafeArea: landscapeSafe})
	checkAnchors("landscape", landscapeSafe)

	// Disabling safe-area insets reactively expands HUD to Padding-only bounds.
	app.Post(func() {
		hud.SetSafeArea(false)
	})
	app.Settle()
	if hud.SafeAreaEnabled() {
		t.Fatal("SafeAreaEnabled should be false after SetSafeArea(false)")
	}
	checkAnchors("safe-area-disabled", core.Edges{})

	// Re-enabling safe-area insets restores landscape safe area.
	app.Post(func() {
		hud.SetSafeArea(true)
	})
	app.Settle()
	checkAnchors("safe-area-reenabled", landscapeSafe)
}

func TestHUD3DNoGPUFallbackAndNilSafety(t *testing.T) {
	// Verify nil *HUD3D safety and HUDAnchor.String.
	var nilHUD *scene.HUD3D
	nilHUD.Add(core.Text{Content: "ignored"})
	nilHUD.AddAt(scene.HUDTopLeft, core.Text{Content: "ignored"})
	nilHUD.AddPassThrough(scene.HUDCenter, core.Text{Content: "ignored"})
	nilHUD.AddSlot(scene.HUDSlot{Anchor: scene.HUDTopLeft, Node: core.Text{Content: "ignored"}})
	nilHUD.SetHidden(true)
	nilHUD.SetVisible(true)
	nilHUD.SetSafeArea(false)
	if nilHUD.Visible() || nilHUD.Toggle() || nilHUD.SafeAreaEnabled() {
		t.Fatal("nil HUD3D should report false for Visible/Toggle/SafeAreaEnabled")
	}

	anchors := map[scene.HUDAnchor]string{
		scene.HUDFull:         "full",
		scene.HUDTopLeft:      "top-left",
		scene.HUDTopCenter:    "top-center",
		scene.HUDTopRight:     "top-right",
		scene.HUDCenterLeft:   "center-left",
		scene.HUDCenter:       "center",
		scene.HUDCenterRight:  "center-right",
		scene.HUDBottomLeft:   "bottom-left",
		scene.HUDBottomCenter: "bottom-center",
		scene.HUDBottomRight:  "bottom-right",
		scene.HUDAnchor(99):   "unknown",
	}
	for a, want := range anchors {
		if got := a.String(); got != want {
			t.Errorf("HUDAnchor(%d).String() = %q, want %q", a, got, want)
		}
	}

	// Verify HUD3D.View works cleanly when GPU is unavailable (NoGPU fallback).
	var (
		hudClicks int
		bgPresses int
		gotErr    gpu.ErrorCode
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		hud := &scene.HUD3D{
			Padding: core.All(8),
		}
		hud.AddAt(scene.HUDTopLeft, ui.Button{
			TestID:  "fallback-btn",
			Text:    "OK",
			OnPress: func() { hudClicks++ },
		})
		return hud.View(cx, gpu.ViewProps{
			TestID: "gpu-fallback",
			Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
			OnError: func(err gpu.Error) {
				gotErr = err.Code
			},
			OnPress: func(core.PressEvent) {
				bgPresses++
			},
		})
	}, nsgotest.Size(300, 200), nsgotest.NoGPU())

	if gotErr != gpu.ErrUnsupported {
		t.Fatalf("NoGPU OnError = %v, want ErrUnsupported", gotErr)
	}
	app.Press("id:fallback-btn")
	app.PressAt("id:gpu-fallback", core.Point{X: 250, Y: 150})
	if hudClicks != 1 || bgPresses != 1 {
		t.Fatalf("NoGPU fallback: hudClicks=%d bgPresses=%d, want 1, 1", hudClicks, bgPresses)
	}
}
