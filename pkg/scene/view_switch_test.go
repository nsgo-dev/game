package scene_test

import (
	"fmt"
	"image/color"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// ExampleManager_switch2D3D switches between a 2D canvas scene and a 3D gpu.View scene through one Manager and one shared input.Actions, fading through black each way. Mounting the scenes with Manager.View needs a running app, so this example shows the scene lifecycle the host follows; TestManagerSwitch2DTo3DAndBackWithSharedInputAndSound mounts the same pair and checks the 3D view mounts once and unmounts on exit.
func ExampleManager_switch2D3D() {
	clock := core.NewManualClock(time.Unix(0, 0))
	frameWanted := false
	root := core.NewRoot(core.RootOptions{Clock: clock, RequestFrame: func() { frameWanted = true }})
	defer root.Dispose()
	frame := func(d time.Duration) {
		clock.Advance(d)
		if frameWanted {
			frameWanted = false
			root.RunFrame()
		}
	}

	mgr := scene.NewManager(root)
	mgr.Actions = input.NewActions()
	mgr.Actions.Bind("toggle", input.Key("tab"))

	var scene2D, scene3D *scene.Controller
	scene2D = &scene.Controller{
		Scene: scene.NewScene(nil),
		Enter: func(*core.Scope) { fmt.Println("enter 2D") },
		Exit:  func() { fmt.Println("exit 2D") },
		Update: func(time.Duration) {
			if mgr.Actions.JustPressed("toggle") {
				mgr.Switch(scene3D, scene.Fade(200*time.Millisecond, core.RGB(0x000000)))
			}
		},
	}
	scene3D = &scene.Controller{
		Enter: func(*core.Scope) { fmt.Println("enter 3D") },
		Exit:  func() { fmt.Println("exit 3D") },
		View: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style:       core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				DepthFormat: gpu.FormatDepth24Plus,
			})
		},
		Update: func(time.Duration) {
			if mgr.Actions.JustPressed("toggle") {
				mgr.Switch(scene2D, scene.Fade(200*time.Millisecond, core.RGB(0x000000)))
			}
		},
	}

	mgr.Switch(scene2D, nil)
	mgr.Actions.Trigger("toggle")
	mgr.Actions.Step()
	mgr.Update(16 * time.Millisecond) // switches 2D -> 3D with fade
	frame(0)
	frame(100 * time.Millisecond)
	fmt.Println("mid-fade transitioning:", mgr.Transitioning())
	frame(100 * time.Millisecond)
	fmt.Println("after fade, in 3D:", mgr.Current() == scene3D)

	mgr.Actions.Trigger("toggle")
	mgr.Actions.Step()
	mgr.Update(16 * time.Millisecond) // switches 3D -> 2D with fade
	frame(0)
	frame(200 * time.Millisecond)
	fmt.Println("back in 2D:", mgr.Current() == scene2D)
	// Output:
	// enter 2D
	// enter 3D
	// mid-fade transitioning: true
	// exit 2D
	// after fade, in 3D: true
	// enter 2D
	// exit 3D
	// back in 2D: true
	// exit 2D
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

func overlayFillAlpha(ops []core.DrawOp) (float32, core.Color, bool) {
	var curAlpha float32 = 1
	var curColor core.Color
	for _, op := range ops {
		switch op.Kind {
		case core.DrawAlpha:
			curAlpha = op.A
		case core.DrawFillColor:
			curColor = op.Color
		case core.DrawFill:
			return curAlpha, curColor, true
		}
	}
	return 0, 0, false
}

func TestManagerSwitch2DTo3DAndBackWithSharedInputAndSound(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	const (
		size     = 64
		sfxURI   = "asset:sfx/step.wav"
		musicURI = "asset:music/theme.m4a"
	)

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			var (
				mgr          *scene.Manager
				actions      *input.Actions
				mixer        *sound.Mixer
				music        *sound.Music
				sfx          *sound.Effect
				frameTick    *core.Signal[int]
				lifecycle    []string
				moves2D      int
				moves3D      int
				onReadyCount int
				viewBuilds3D int
				viewCleanups int
				hero         sprite.Sprite
				scene2DCtrl  *scene.Controller
				scene3DCtrl  *scene.Controller
				cam3D        *scene3d.Camera
				quadMesh     render3d.Mesh
			)

			// 2D scene with a sprite batch.
			heroAtlas := solidSpriteAtlas("hero", 12, 12, color.RGBA{R: 40, G: 200, B: 90, A: 255})
			hero = sprite.NewSprite(heroAtlas.Frames["hero"])
			hero.X, hero.Y = 8, 8
			sc2D := scene.NewScene(&tilemap.Camera{X: 32, Y: 32, Zoom: 1, Width: size, Height: size})
			world2D := sc2D.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
			world2D.AddBatch(&sprite.Batch{Atlas: heroAtlas, Sprites: []*sprite.Sprite{&hero}})

			// 3D scene camera and quad mesh.
			cam3D = scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(60), 1, 0.1, 20)
			quadMesh = overlayQuad3D(math3d.Vec3{X: 0, Y: 0, Z: 0}, 1.0)

			fadeBlack := scene.Fade(400*time.Millisecond, core.RGB(0x000000))

			opts := []nsgotest.Option{
				nsgotest.Size(size, size),
				nsgotest.ManualFrames(),
				nsgotest.Environment(core.Environment{Audio: true}),
				func(a *nsgotest.App) {
					a.AudioDurations = map[string]time.Duration{
						sfxURI:   80 * time.Millisecond,
						musicURI: time.Minute,
					}
				},
			}
			opts = append(opts, prof.opts...)

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				frameTick = core.NewSignal(cx, 0)
				actions = input.NewActions()
				actions.Bind("move", input.Key("d"))
				actions.Bind("switch", input.Key("tab"))
				actions.WatchKeys(cx)

				mixer = sound.NewMixer(cx, "switch.sound")
				music = mixer.Music("music")
				music.Loop = true
				audio.LoadSound(cx, audio.Source{URI: sfxURI}, func(s *audio.Sound, err error) {
					if err != nil {
						t.Errorf("LoadSound: %v", err)
						return
					}
					sfx = mixer.Effect(s, "sfx", 4)
				})
				music.Play(audio.Source{URI: musicURI}, 0)

				mgr = scene.NewManager(cx)
				mgr.Actions = actions

				scene2DCtrl = &scene.Controller{
					Scene: sc2D,
					Enter: func(*core.Scope) { lifecycle = append(lifecycle, "enter-2d") },
					Exit:  func() { lifecycle = append(lifecycle, "exit-2d") },
					Update: func(time.Duration) {
						if mgr.Actions.JustPressed("move") {
							moves2D++
							hero.X += 4
							sfx.Play(audio.Voice{})
						}
						if mgr.Actions.JustPressed("switch") {
							mgr.Switch(scene3DCtrl, fadeBlack)
						}
					},
				}

				scene3DCtrl = &scene.Controller{
					Enter: func(*core.Scope) { lifecycle = append(lifecycle, "enter-3d") },
					Exit:  func() { lifecycle = append(lifecycle, "exit-3d") },
					Update: func(time.Duration) {
						if mgr.Actions.JustPressed("move") {
							moves3D++
							sfx.Play(audio.Voice{})
						}
						if mgr.Actions.JustPressed("switch") {
							mgr.Switch(scene2DCtrl, fadeBlack)
						}
					},
					View: func(scx *core.Scope) core.Node {
						viewBuilds3D++
						scx.Cleanup(func() { viewCleanups++ })
						props := new3DQuadViewProps(t, scx, size, cam3D, quadMesh, gpu.ViewProps{
							TestID: "scene-3d-gpu",
							Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						})
						origReady := props.OnReady
						props.OnReady = func(dev *gpu.Device) {
							onReadyCount++
							origReady(dev)
						}
						return gpu.View(scx, props)
					},
				}

				mgr.Switch(scene2DCtrl, nil)

				loop := &game.Loop{
					Rate: 60,
					Update: func(dt time.Duration) {
						actions.Step()
						mgr.Update(dt)
					},
					Draw: func(float64) {
						frameTick.Set(frameTick.GetUntracked() + 1)
					},
				}
				loop.Run(cx)

				return mgr.View(scene.HostOptions{
					TestID:           "host",
					StageTestID:      "stage",
					TransitionTestID: "transition-overlay",
					Style:            core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					Alpha: func() float64 {
						_ = frameTick.Get()
						return 0
					},
				})
			}, opts...)

			if sfx == nil {
				t.Fatal("shared SFX failed to load")
			}

			// 1. Starts in 2D scene: 2D sprite canvas is mounted, no GPU view yet, music is playing.
			app.Advance(20 * time.Millisecond)
			if mgr.Current() != scene2DCtrl {
				t.Fatalf("initial scene = %v, want scene2D", mgr.Current())
			}
			if got := len(app.Query("id:scene-3d-gpu")); got != 0 {
				t.Fatalf("expected 0 GPU views while in 2D, got %d", got)
			}
			canvases := findCanvasChildren(app.Find("id:stage"), "transition-overlay", "scene-3d-overlay")
			if len(canvases) != 1 || len(canvases[0].Ops) == 0 {
				t.Fatalf("expected 1 active 2D scene canvas with draw ops, got %d canvases", len(canvases))
			}
			if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
				t.Fatalf("music player not playing in 2D: %+v", app.Players)
			}

			// 2. Shared input drives the 2D scene and triggers a shared SFX.
			app.KeyDown("d")
			app.Advance(20 * time.Millisecond)
			app.KeyUp("d")
			app.Advance(20 * time.Millisecond)
			if moves2D != 1 || hero.X != 12 {
				t.Fatalf("after 2D move: moves2D=%d hero.X=%v, want 1 and 12", moves2D, hero.X)
			}
			if plays := len(app.Sounds[0].Plays); plays != 1 {
				t.Fatalf("2D SFX plays = %d, want 1", plays)
			}

			// 3. Pressing "switch" (and keeping it held!) transitions 2D -> 3D with Fade.
			app.KeyDown("tab")
			app.Advance(20 * time.Millisecond) // triggers Switch(scene3D, fadeBlack) + Actions.Reset()
			if !mgr.Transitioning() || mgr.Current() != scene3DCtrl {
				t.Fatalf("after switch key: transitioning=%v current==3D:%v", mgr.Transitioning(), mgr.Current() == scene3DCtrl)
			}
			// Bindings must survive Reset(), and holding "tab" must NOT immediately switch back!
			if len(actions.Bindings("switch")) == 0 || len(actions.Bindings("move")) == 0 {
				t.Fatal("input bindings lost across 2D -> 3D switch")
			}

			// Advance to t = 100ms (p = 0.25): fade overlay is 50% black, 3D mounted once & hidden until midpoint.
			app.Advance(100 * time.Millisecond)
			if !mgr.Transitioning() {
				t.Fatal("fade ended prematurely at 100ms")
			}
			if a, col, ok := overlayFillAlpha(app.Find("id:transition-overlay").Ops); !ok || col != core.RGB(0x000000) || math.Abs(float64(a-0.5)) > 0.1 {
				t.Fatalf("at p=0.25 transition overlay = (alpha=%v, color=%#x, ok=%v), want ~0.5 black", a, col, ok)
			}
			if got := len(app.Query("id:scene-3d-gpu")); got != 0 {
				t.Fatalf("at p=0.25 visible 3D views = %d, want 0 (hidden until midpoint)", got)
			}
			mounted3D := app.QueryMounted("id:scene-3d-gpu")
			if len(mounted3D) != 1 || !mounted3D[0].Parent.Style.Hidden {
				t.Fatalf("at p=0.25 expected 1 mounted hidden 3D view, got %d", len(mounted3D))
			}
			if onReadyCount != 1 || viewBuilds3D != 1 {
				t.Fatalf("at p=0.25 3D viewBuilds=%d onReady=%d, want 1 and 1", viewBuilds3D, onReadyCount)
			}

			// Advance to t = 200ms (p = 0.50): midpoint flip! 3D becomes visible, overlay at peak opacity.
			app.Advance(100 * time.Millisecond)
			gpuNode := app.Find("id:scene-3d-gpu")
			if gpuNode.Parent.Style.Hidden {
				t.Fatalf("at p=0.50 3D wrapper Hidden = %v, want false", gpuNode.Parent.Style.Hidden)
			}
			if a, _, ok := overlayFillAlpha(app.Find("id:transition-overlay").Ops); !ok || math.Abs(float64(a-1.0)) > 0.1 {
				t.Fatalf("at p=0.50 transition overlay alpha = %v (ok=%v), want ~1.0", a, ok)
			}

			// Advance to completion (t = 400ms+): 2D exited & unmounted, 3D STILL single mount, overlay cleared!
			app.Advance(220 * time.Millisecond)
			if mgr.Transitioning() {
				t.Fatal("2D -> 3D fade did not finish")
			}
			if mgr.Current() != scene3DCtrl {
				t.Fatal("holding switch key bounced back out of 3D scene")
			}
			if onReadyCount != 1 || viewBuilds3D != 1 {
				t.Fatalf("3D view was rebuilt during fade! viewBuilds=%d onReady=%d, want 1 and 1", viewBuilds3D, onReadyCount)
			}
			if ops := len(app.Find("id:transition-overlay").Ops); ops != 0 {
				t.Fatalf("transition overlay still has %d ops after completion, want 0", ops)
			}
			if got := len(findCanvasChildren(app.Find("id:stage"), "transition-overlay")); got != 0 {
				t.Fatalf("expected 0 2D scene canvases after 2D exited, got %d", got)
			}

			// 4. Verify 3D GPU pixel readback renders the 3D mesh cleanly!
			gpuImg, err := app.GPUReadPixels("id:scene-3d-gpu")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantOverlayPixel(t, gpuImg, 2, 2, overlay3DClearColor)
			wantOverlayPixel(t, gpuImg, 32, 32, color.RGBA{R: 255, G: 0, B: 255, A: 255})

			// 5. Music stayed playing uninterrupted across 2D -> 3D!
			if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
				t.Fatalf("music interrupted across 2D -> 3D: %+v", app.Players)
			}

			// 6. Release "tab", press "move" in 3D: same Actions & Mixer drive 3D!
			app.KeyUp("tab")
			app.Advance(20 * time.Millisecond)
			app.KeyDown("d")
			app.Advance(20 * time.Millisecond)
			app.KeyUp("d")
			app.Advance(20 * time.Millisecond)
			if moves3D != 1 || moves2D != 1 {
				t.Fatalf("after 3D move: moves3D=%d moves2D=%d, want 1 and 1", moves3D, moves2D)
			}
			if plays := len(app.Sounds[0].Plays); plays != 2 {
				t.Fatalf("SFX plays after 3D action = %d, want 2", plays)
			}

			// 7. Press "switch" again in 3D -> fade cleanly back to 2D!
			app.KeyDown("tab")
			app.Advance(20 * time.Millisecond)
			app.KeyUp("tab")
			if !mgr.Transitioning() || mgr.Current() != scene2DCtrl {
				t.Fatalf("after 3D->2D switch: transitioning=%v current==2D:%v", mgr.Transitioning(), mgr.Current() == scene2DCtrl)
			}
			app.Advance(420 * time.Millisecond)
			if mgr.Transitioning() || mgr.Current() != scene2DCtrl {
				t.Fatalf("after 3D->2D fade: transitioning=%v current==2D:%v", mgr.Transitioning(), mgr.Current() == scene2DCtrl)
			}
			if viewCleanups != 1 {
				t.Fatalf("3D view scope cleanup count = %d, want 1", viewCleanups)
			}
			if got := len(app.Query("id:scene-3d-gpu")); got != 0 {
				t.Fatalf("expected 0 GPU views after switching back to 2D, got %d", got)
			}
			canvases = findCanvasChildren(app.Find("id:stage"), "transition-overlay")
			if len(canvases) != 1 || len(canvases[0].Ops) == 0 {
				t.Fatalf("expected 2D canvas back with draw ops, got %d canvases", len(canvases))
			}

			// 8. Music STILL uninterrupted, and "move" drives 2D again!
			if len(app.Players) != 1 || !app.Players[0].Playing || app.Players[0].Closed {
				t.Fatalf("music interrupted across 3D -> 2D: %+v", app.Players)
			}
			app.KeyDown("d")
			app.Advance(20 * time.Millisecond)
			app.KeyUp("d")
			app.Advance(20 * time.Millisecond)
			if moves2D != 2 || moves3D != 1 {
				t.Fatalf("after return to 2D move: moves2D=%d moves3D=%d, want 2 and 1", moves2D, moves3D)
			}
			if plays := len(app.Sounds[0].Plays); plays != 3 {
				t.Fatalf("SFX plays after return to 2D = %d, want 3", plays)
			}

			wantLifecycle := []string{"enter-2d", "enter-3d", "exit-2d", "enter-2d", "exit-3d"}
			if !reflect.DeepEqual(lifecycle, wantLifecycle) {
				t.Fatalf("lifecycle = %v, want %v", lifecycle, wantLifecycle)
			}
		})
	}
}

func TestManagerCrossfadeBetween2DAnd3D(t *testing.T) {
	const size = 64
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(60), 1, 0.1, 20)
	mesh := overlayQuad3D(math3d.Vec3{X: 0, Y: 0, Z: 0}, 1.0)

	var (
		mgr          *scene.Manager
		onReadyCount int
		viewCleanups int
	)

	scene2D := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(core.RGB(0x2266cc)).FillRect(0, 0, size, size)
		},
	}
	scene3D := &scene.Controller{
		View: func(scx *core.Scope) core.Node {
			scx.Cleanup(func() { viewCleanups++ })
			props := new3DQuadViewProps(t, scx, size, cam, mesh, gpu.ViewProps{
				TestID: "xfade-3d",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
			})
			origReady := props.OnReady
			props.OnReady = func(dev *gpu.Device) {
				onReadyCount++
				origReady(dev)
			}
			return gpu.View(scx, props)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(scene2D, nil)
		return mgr.View(scene.HostOptions{
			TestID:           "host",
			StageTestID:      "stage",
			TransitionTestID: "transition-overlay",
			Style:            core.Style{Width: core.Pt(size), Height: core.Pt(size)},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	// Crossfade 2D -> 3D over 400ms.
	app.Post(func() { mgr.Switch(scene3D, scene.Crossfade(400*time.Millisecond)) })
	app.Settle()

	// At p = 0.25: 2D opacity = 0.75, 3D opacity = 0.25, overlay canvas has 0 ops.
	app.Advance(100 * time.Millisecond)
	canvases := findCanvasChildren(app.Find("id:stage"), "transition-overlay", "scene-3d-overlay")
	if len(canvases) != 1 {
		t.Fatalf("at p=0.25 expected 1 2D canvas, got %d", len(canvases))
	}
	oldAlpha := canvases[0].Parent.Style.Opacity
	newAlpha := app.Find("id:xfade-3d").Parent.Style.Opacity
	if math.Abs(float64(oldAlpha-0.75)) > 0.05 || math.Abs(float64(newAlpha-0.25)) > 0.05 {
		t.Fatalf("at p=0.25 opacities = (old=%v, new=%v), want (0.75, 0.25)", oldAlpha, newAlpha)
	}
	if ops := len(app.Find("id:transition-overlay").Ops); ops != 0 {
		t.Fatalf("crossfade transition overlay ops = %d, want 0", ops)
	}

	// At p = 0.75: 2D opacity = 0.25, 3D opacity = 0.75.
	app.Advance(200 * time.Millisecond)
	oldAlpha = findCanvasChildren(app.Find("id:stage"), "transition-overlay")[0].Parent.Style.Opacity
	newAlpha = app.Find("id:xfade-3d").Parent.Style.Opacity
	if math.Abs(float64(oldAlpha-0.25)) > 0.05 || math.Abs(float64(newAlpha-0.75)) > 0.05 {
		t.Fatalf("at p=0.75 opacities = (old=%v, new=%v), want (0.25, 0.75)", oldAlpha, newAlpha)
	}

	// Complete crossfade: 2D unmounted, 3D full opacity, OnReady ran once.
	app.Advance(120 * time.Millisecond)
	if mgr.Transitioning() {
		t.Fatal("crossfade did not complete")
	}
	if onReadyCount != 1 {
		t.Fatalf("onReadyCount = %d, want 1", onReadyCount)
	}
	if got := len(findCanvasChildren(app.Find("id:stage"), "transition-overlay")); got != 0 {
		t.Fatalf("expected 0 2D canvases after crossfade, got %d", got)
	}
	gpuImg, err := app.GPUReadPixels("id:xfade-3d")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	wantOverlayPixel(t, gpuImg, 32, 32, color.RGBA{R: 255, G: 0, B: 255, A: 255})

	// Crossfade 3D -> 2D and verify clean unmount of 3D.
	app.Post(func() { mgr.Switch(scene2D, scene.Crossfade(200*time.Millisecond)) })
	app.Settle()
	app.Advance(220 * time.Millisecond)
	if mgr.Transitioning() || viewCleanups != 1 || len(app.Query("id:xfade-3d")) != 0 {
		t.Fatalf("after 3D->2D crossfade: transitioning=%v cleanups=%d gpuViews=%d",
			mgr.Transitioning(), viewCleanups, len(app.Query("id:xfade-3d")))
	}
}

func TestManagerPushPop2DOverlayOver3DPreserves3DState(t *testing.T) {
	const size = 64
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(60), 1, 0.1, 20)
	mesh := overlayQuad3D(math3d.Vec3{X: 0, Y: 0, Z: 0}, 1.0)

	var (
		mgr          *scene.Manager
		lifecycle    []string
		viewBuilds3D int
		onReadyCount int
		viewCleanups int
		ticks3D      int
		ticksPause   int
		ticksInv     int
		scene3DState = 42
	)

	scene3D := &scene.Controller{
		Enter:  func(*core.Scope) { lifecycle = append(lifecycle, "enter-3d") },
		Pause:  func() { lifecycle = append(lifecycle, "pause-3d") },
		Resume: func() { lifecycle = append(lifecycle, "resume-3d") },
		Exit:   func() { lifecycle = append(lifecycle, "exit-3d") },
		Update: func(time.Duration) {
			ticks3D++
			scene3DState++
		},
		View: func(scx *core.Scope) core.Node {
			viewBuilds3D++
			scx.Cleanup(func() { viewCleanups++ })
			props := new3DQuadViewProps(t, scx, size, cam, mesh, gpu.ViewProps{
				TestID: "world-3d",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
			})
			origReady := props.OnReady
			props.OnReady = func(dev *gpu.Device) {
				onReadyCount++
				origReady(dev)
			}
			return gpu.View(scx, props)
		},
	}

	pause2D := &scene.Controller{
		Transparent: true,
		Enter:       func(*core.Scope) { lifecycle = append(lifecycle, "enter-pause") },
		Pause:       func() { lifecycle = append(lifecycle, "pause-pause") },
		Resume:      func() { lifecycle = append(lifecycle, "resume-pause") },
		Exit:        func() { lifecycle = append(lifecycle, "exit-pause") },
		Update:      func(time.Duration) { ticksPause++ },
		Draw: func(d *core.Drawing, _ float64) {
			d.SetAlpha(0.5)
			d.SetFillColor(core.RGB(0x000000))
			d.FillRect(0, 0, size, size)
		},
	}

	inventory2D := &scene.Controller{
		Transparent: false, // opaque 2D screen pushed over pause+3D
		Enter:       func(*core.Scope) { lifecycle = append(lifecycle, "enter-inv") },
		Exit:        func() { lifecycle = append(lifecycle, "exit-inv") },
		Update:      func(time.Duration) { ticksInv++ },
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(core.RGB(0x223344))
			d.FillRect(0, 0, size, size)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(scene3D, nil)
		return mgr.View(scene.HostOptions{
			TestID:           "host",
			StageTestID:      "stage",
			TransitionTestID: "transition-overlay",
			Style:            core.Style{Width: core.Pt(size), Height: core.Pt(size)},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	app.Post(func() { mgr.Update(16 * time.Millisecond) })
	app.Settle()
	if ticks3D != 1 || scene3DState != 43 || onReadyCount != 1 {
		t.Fatalf("before push: ticks3D=%d state=%d onReady=%d", ticks3D, scene3DState, onReadyCount)
	}

	// Push transparent 2D pause overlay over 3D scene.
	app.Post(func() { mgr.Push(pause2D) })
	app.Settle()

	// 3D must stay mounted (NOT rebuilt or cleaned up), visible underneath, with PointerEventsNone!
	if viewBuilds3D != 1 || onReadyCount != 1 || viewCleanups != 0 {
		t.Fatalf("after push pause: viewBuilds=%d onReady=%d cleanups=%d, want 1, 1, 0",
			viewBuilds3D, onReadyCount, viewCleanups)
	}
	worldWrapper := app.Find("id:world-3d").Parent
	if worldWrapper.Style.Hidden {
		t.Fatalf("3D wrapper Hidden under transparent pause = %v, want false", worldWrapper.Style.Hidden)
	}
	if worldWrapper.Props.PointerEvents != core.PointerEventsNone {
		t.Fatalf("3D wrapper PointerEvents while paused = %v, want PointerEventsNone", worldWrapper.Props.PointerEvents)
	}

	// Pause canvas draws above 3D, and Update advances ONLY pause2D (3D state frozen!).
	pauseCanvases := findCanvasChildren(app.Find("id:stage"), "transition-overlay")
	if len(pauseCanvases) != 1 || len(pauseCanvases[0].Ops) == 0 {
		t.Fatalf("expected 1 2D pause canvas with ops, got %d", len(pauseCanvases))
	}
	app.Post(func() { mgr.Update(16 * time.Millisecond) })
	app.Settle()
	if ticks3D != 1 || scene3DState != 43 || ticksPause != 1 {
		t.Fatalf("while paused: ticks3D=%d state=%d ticksPause=%d, want 1, 43, 1", ticks3D, scene3DState, ticksPause)
	}

	// 3D GPU surface underneath pause overlay still holds rendered pixels!
	gpuImg, err := app.GPUReadPixels("id:world-3d")
	if err != nil {
		t.Fatalf("GPUReadPixels under pause: %v", err)
	}
	wantOverlayPixel(t, gpuImg, 32, 32, color.RGBA{R: 255, G: 0, B: 255, A: 255})

	// Push opaque 2D inventory screen on top: 3D stays mounted (GPU preserved!) while hidden.
	app.Post(func() { mgr.Push(inventory2D) })
	app.Settle()
	if viewBuilds3D != 1 || onReadyCount != 1 || viewCleanups != 0 {
		t.Fatalf("after push opaque inventory: viewBuilds=%d onReady=%d cleanups=%d",
			viewBuilds3D, onReadyCount, viewCleanups)
	}
	if got := len(app.Query("id:world-3d")); got != 0 {
		t.Fatalf("visible 3D views under opaque inventory = %d, want 0", got)
	}
	mounted3D := app.QueryMounted("id:world-3d")
	if len(mounted3D) != 1 || !mounted3D[0].Parent.Style.Hidden {
		t.Fatalf("expected 1 mounted hidden 3D view under opaque inventory, got %d", len(mounted3D))
	}
	app.Post(func() { mgr.Update(16 * time.Millisecond) })
	app.Settle()
	if ticksInv != 1 || ticksPause != 1 || ticks3D != 1 {
		t.Fatalf("while in inventory: ticksInv=%d ticksPause=%d ticks3D=%d", ticksInv, ticksPause, ticks3D)
	}

	// Pop inventory -> back to transparent pause over visible 3D.
	app.Post(func() { mgr.Pop() })
	app.Settle()
	if hidden := app.Find("id:world-3d").Parent.Style.Hidden; hidden {
		t.Fatalf("3D Hidden after popping opaque inventory = %v, want false", hidden)
	}

	// Pop pause -> back to active 3D scene with exact preserved state & zero GPU rebuilds!
	app.Post(func() { mgr.Pop() })
	app.Settle()
	if viewBuilds3D != 1 || onReadyCount != 1 || viewCleanups != 0 {
		t.Fatalf("after popping back to 3D: viewBuilds=%d onReady=%d cleanups=%d, want 1, 1, 0",
			viewBuilds3D, onReadyCount, viewCleanups)
	}
	worldWrapper = app.Find("id:world-3d").Parent
	if worldWrapper.Style.Hidden || worldWrapper.Props.PointerEvents != core.PointerEventsAuto {
		t.Fatalf("resumed 3D wrapper: hidden=%v pointer=%v, want false & Auto",
			worldWrapper.Style.Hidden, worldWrapper.Props.PointerEvents)
	}
	if got := len(findCanvasChildren(app.Find("id:stage"), "transition-overlay")); got != 0 {
		t.Fatalf("expected 0 2D overlay canvases after popping back to 3D, got %d", got)
	}

	app.Post(func() { mgr.Update(16 * time.Millisecond) })
	app.Settle()
	if ticks3D != 2 || scene3DState != 44 {
		t.Fatalf("after resuming 3D: ticks3D=%d state=%d, want 2 and 44", ticks3D, scene3DState)
	}

	app.Advance(16 * time.Millisecond)
	gpuImg, err = app.GPUReadPixels("id:world-3d")
	if err != nil {
		t.Fatalf("GPUReadPixels after resume: %v", err)
	}
	wantOverlayPixel(t, gpuImg, 32, 32, color.RGBA{R: 255, G: 0, B: 255, A: 255})

	wantLifecycle := []string{
		"enter-3d", "pause-3d", "enter-pause",
		"pause-pause", "enter-inv",
		"exit-inv", "resume-pause",
		"exit-pause", "resume-3d",
	}
	if !reflect.DeepEqual(lifecycle, wantLifecycle) {
		t.Fatalf("lifecycle = %v, want %v", lifecycle, wantLifecycle)
	}
}

func TestManagerKeepInputOnSwitchViewportAndPersistentHUD(t *testing.T) {
	const winW, winH = 160, 120
	vp := &screen.Viewport{Width: 160, Height: 90, Mode: screen.ModeFit}

	var (
		mgr           *scene.Manager
		actions       *input.Actions
		hudClicks     int
		scene3DClicks int
		draws2D       int
		customRun     int
	)

	scene2D := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) {
			draws2D++
			d.SetFillColor(core.RGB(0x114488)).FillRect(10, 10, 20, 10)
		},
	}
	scene3D := &scene.Controller{
		View: func(scx *core.Scope) core.Node {
			ov := scene.NewOverlay3D(scx)
			ov.TestID = "scene-3d-overlay"
			return ov.View(scx, gpu.ViewProps{
				TestID:      "hud-test-3d",
				Style:       core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				DepthFormat: gpu.FormatDepth24Plus,
				Children: []core.Node{
					core.View{
						TestID:  "scene-3d-btn",
						Style:   core.Style{Position: core.Absolute, Bottom: core.Pt(4), Right: core.Pt(4), Width: core.Pt(40), Height: core.Pt(20)},
						OnPress: func(core.PressEvent) { scene3DClicks++ },
					},
				},
			})
		},
	}
	overlay2D := &scene.Controller{
		Transparent: true,
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(core.RGB(0xffaa00)).FillRect(5, 5, 10, 10)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		actions = input.NewActions()
		actions.Bind("hold", input.Key("w"))
		actions.WatchKeys(cx)

		mgr = scene.NewManager(cx)
		mgr.Actions = actions
		mgr.KeepInputOnSwitch = true
		mgr.Switch(scene2D, nil)

		return mgr.View(scene.HostOptions{
			TestID:           "host",
			StageTestID:      "stage",
			TransitionTestID: "transition-overlay",
			Style:            core.Style{Width: core.Pt(winW), Height: core.Pt(winH)},
			Viewport:         vp,
			Children: []core.Node{
				core.View{
					TestID:  "persistent-hud-btn",
					Style:   core.Style{Position: core.Absolute, Top: core.Pt(4), Left: core.Pt(4), Width: core.Pt(40), Height: core.Pt(20)},
					OnPress: func(core.PressEvent) { hudClicks++ },
				},
			},
		})
	}, nsgotest.Size(winW, winH), nsgotest.ManualFrames())

	// 1. Invalidate redraws 2D scene cleanly.
	beforeDraws := draws2D
	app.Post(func() { mgr.Invalidate() })
	app.Settle()
	if draws2D <= beforeDraws {
		t.Fatalf("Invalidate did not redraw 2D scene: %d -> %d", beforeDraws, draws2D)
	}

	// 2. Persistent HUD button is pressable in 2D.
	app.Press("id:persistent-hud-btn")
	if hudClicks != 1 {
		t.Fatalf("persistent HUD clicks in 2D = %d, want 1", hudClicks)
	}

	// 3. KeepInputOnSwitch keeps held key active across immediate 2D -> 3D switch!
	app.KeyDown("w")
	app.Post(func() {
		actions.Step()
		if !actions.Pressed("hold") {
			t.Error("hold should be pressed before switch")
		}
		mgr.Switch(scene3D, nil)
		actions.Step()
		if !actions.Pressed("hold") {
			t.Error("KeepInputOnSwitch=true dropped held key across switch")
		}
	})
	app.Settle()
	app.KeyUp("w")

	// 4. Persistent HUD button and 3D-scene native HUD button are both pressable in 3D!
	app.Press("id:persistent-hud-btn")
	if hudClicks != 2 {
		t.Fatalf("persistent HUD clicks in 3D = %d, want 2", hudClicks)
	}
	app.Press("id:scene-3d-btn")
	if scene3DClicks != 1 {
		t.Fatalf("3D scene HUD button clicks = %d, want 1", scene3DClicks)
	}

	// 5. Transparent 2D overlay pushed over 3D uses transparent Viewport bars (1 fill, not 3).
	app.Post(func() { mgr.Push(overlay2D) })
	app.Settle()
	canvases := findCanvasChildren(app.Find("id:stage"), "transition-overlay", "scene-3d-overlay")
	if len(canvases) != 1 {
		t.Fatalf("expected 1 pushed 2D overlay canvas, got %d", len(canvases))
	}
	fillCount := 0
	for _, op := range canvases[0].Ops {
		if op.Kind == core.DrawFill {
			fillCount++
		}
	}
	if fillCount != 1 {
		t.Fatalf("transparent 2D overlay over 3D recorded %d fills, want 1 (transparent viewport bars)", fillCount)
	}
	if vp.BarColor != 0 {
		t.Fatalf("Host Viewport.BarColor was mutated to %#x", vp.BarColor)
	}

	// 6. CustomTransition across 3D -> 2D invokes custom overlay draw and finishes cleanly.
	custom := scene.CustomTransition(200*time.Millisecond, func(d *core.Drawing, size core.Size, progress float64, _, _ func(*core.Drawing)) {
		customRun++
		d.SetFillColor(core.RGB(0xffffff)).FillRect(0, 0, float32(size.Width)*float32(progress), 4)
	})
	app.Post(func() { mgr.Switch(scene2D, custom) })
	app.Settle()
	app.Advance(100 * time.Millisecond)
	if customRun == 0 {
		t.Fatal("CustomTransition draw did not run on 3D -> 2D transition")
	}
	app.Advance(120 * time.Millisecond)
	if mgr.Transitioning() || mgr.Current() != scene2D {
		t.Fatalf("after custom transition: transitioning=%v current==2D:%v", mgr.Transitioning(), mgr.Current() == scene2D)
	}

	// 7. Pure 2D -> 2D Wipe renders through transition overlay and completes cleanly.
	scene2DB := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) { d.SetFillColor(core.RGB(0x00ff00)).FillRect(0, 0, 160, 90) },
	}
	app.Post(func() { mgr.Switch(scene2DB, scene.Wipe(200*time.Millisecond, scene.DirectionLeft)) })
	app.Settle()
	app.Advance(100 * time.Millisecond)
	if len(app.Find("id:transition-overlay").Ops) == 0 {
		t.Fatal("2D->2D Wipe recorded 0 ops on transition overlay")
	}
	app.Advance(120 * time.Millisecond)
	if mgr.Transitioning() || mgr.Current() != scene2DB {
		t.Fatalf("after 2D->2D Wipe: transitioning=%v current==2DB:%v", mgr.Transitioning(), mgr.Current() == scene2DB)
	}

	// 8. Switch(nil, nil) clears the stage, and nil Manager.View is safe.
	app.Post(func() { mgr.Switch(nil, nil) })
	app.Settle()
	if got := len(findCanvasChildren(app.Find("id:stage"), "transition-overlay")); got != 0 {
		t.Fatalf("after Switch(nil, nil) expected 0 canvases, got %d", got)
	}

	var nilMgr *scene.Manager
	nilMgr.Invalidate()
	if node := nilMgr.View(scene.HostOptions{TestID: "nil-host"}); node == nil {
		t.Fatal("nil Manager.View returned nil node")
	}
}

func TestManagerSwitch2D3DNoGPUFallback(t *testing.T) {
	const size = 64
	var (
		mgr          *scene.Manager
		lifecycle    []string
		viewCleanups int
	)

	scene2D := &scene.Controller{
		Enter: func(*core.Scope) { lifecycle = append(lifecycle, "enter-2d") },
		Exit:  func() { lifecycle = append(lifecycle, "exit-2d") },
		Draw:  func(d *core.Drawing, _ float64) { d.SetFillColor(core.RGB(0x336699)).FillRect(0, 0, size, size) },
	}
	scene3D := &scene.Controller{
		Enter: func(*core.Scope) { lifecycle = append(lifecycle, "enter-3d") },
		Exit:  func() { lifecycle = append(lifecycle, "exit-3d") },
		View: func(scx *core.Scope) core.Node {
			scx.Cleanup(func() { viewCleanups++ })
			return gpu.View(scx, gpu.ViewProps{
				TestID:      "nogpu-3d",
				Style:       core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				DepthFormat: gpu.FormatDepth24Plus,
				Children: []core.Node{
					core.Text{TestID: "nogpu-hud", Content: "3D HUD"},
				},
			})
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(scene2D, nil)
		return mgr.View(scene.HostOptions{
			TestID:           "host",
			StageTestID:      "stage",
			TransitionTestID: "transition-overlay",
			Style:            core.Style{Width: core.Pt(size), Height: core.Pt(size)},
		})
	}, nsgotest.Size(size, size), nsgotest.NoGPU())

	app.Post(func() { mgr.Switch(scene3D, scene.Fade(200*time.Millisecond, core.RGB(0x000000))) })
	app.Settle()
	app.Advance(220 * time.Millisecond)

	if mgr.Current() != scene3D {
		t.Fatal("did not switch to 3D scene under NoGPU")
	}
	if app.Find("id:nogpu-hud") == nil {
		t.Fatal("3D scene HUD child missing under NoGPU fallback")
	}

	app.Post(func() { mgr.Switch(scene2D, nil) })
	app.Settle()
	if mgr.Current() != scene2D || viewCleanups != 1 {
		t.Fatalf("after return to 2D under NoGPU: current==2D:%v cleanups=%d", mgr.Current() == scene2D, viewCleanups)
	}
	want := []string{"enter-2d", "enter-3d", "exit-2d", "exit-3d", "enter-2d"}
	if !reflect.DeepEqual(lifecycle, want) {
		t.Fatalf("NoGPU lifecycle = %v, want %v", lifecycle, want)
	}
}
