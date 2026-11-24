package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	dragOrbitSensitivity = 0.0085 // radians per point dragged
	wheelZoomSensitivity = 0.0045 // meters per wheel point
	keyOrbitStep         = 0.14   // radians per keypress
	keyZoomStep          = 0.55   // meters per keypress
)

// Controller connects the glTF Scene, GPU Renderer, fixed-step game.Loop, mouse/touch/keyboard camera orbit controls, and native scene.HUD3D overlay.
type Controller struct {
	Scene    *Scene
	Renderer *Renderer
	Loop     *game.Loop
	HUD      *scene.HUD3D

	RenderedFrames uint64
	DeviceReady    int
	DeviceLost     int
	DeviceRestored int

	alpha       float32
	lastFrameAt time.Time
	fpsAccum    float64
	fpsFrames   int

	autoOrbitSig  *core.Signal[bool]
	animLightsSig *core.Signal[bool]
	fpsSig        *core.Signal[int]
	gpuErrSig     *core.Signal[string]

	mouseDragging bool
	lastMousePos  core.Point

	activeTouches map[int]core.Point
	pinchStart    float32 // orbit distance when the current pinch began
}

// Root is Lantern 3D's NSGo root component.
func Root(cx *core.Scope) core.Node {
	_, node := NewView(cx)
	return node
}

// NewView builds the 3D scene, GPU renderer, fixed-step loop, and native HUD overlay, returning the controller alongside the root node.
func NewView(cx *core.Scope) (*Controller, core.Node) {
	cx.SetTitle(gameProject.Title())

	sc, err := NewScene()
	if err != nil {
		panic(err)
	}

	c := &Controller{
		Scene:         sc,
		Renderer:      &Renderer{},
		HUD:           scene.NewHUD3D(cx),
		autoOrbitSig:  core.NewSignal(cx, sc.AutoOrbit),
		animLightsSig: core.NewSignal(cx, sc.AnimateLights),
		fpsSig:        core.NewSignal(cx, 60),
		gpuErrSig:     core.NewSignal(cx, ""),
		activeTouches: make(map[int]core.Point, 2),
	}

	c.Loop = &game.Loop{
		Rate: stepRate,
		Update: func(dt time.Duration) {
			if err := c.Scene.Step(dt); err != nil && c.gpuErrSig.GetUntracked() == "" {
				c.gpuErrSig.Set(fmt.Sprintf("Failed to animate the 3D scene: %v", err))
			}
		},
		Draw: func(alpha float64) {
			c.alpha = float32(alpha)
		},
	}
	c.Loop.Run(cx)

	c.bindKeys(cx)
	return c, c.view(cx)
}

func (c *Controller) bindKeys(cx *core.Scope) {
	repeat := core.KeyOptions{When: core.KeyAlways, Repeat: true}
	once := core.KeyOptions{When: core.KeyAlways}

	cx.OnKeyWith("left", repeat, func() { c.Scene.Orbit.Orbit(-keyOrbitStep, 0) })
	cx.OnKeyWith("right", repeat, func() { c.Scene.Orbit.Orbit(keyOrbitStep, 0) })
	cx.OnKeyWith("a", repeat, func() { c.Scene.Orbit.Orbit(-keyOrbitStep, 0) })
	cx.OnKeyWith("d", repeat, func() { c.Scene.Orbit.Orbit(keyOrbitStep, 0) })
	cx.OnKeyWith("up", repeat, func() { c.Scene.Orbit.Orbit(0, keyOrbitStep) })
	cx.OnKeyWith("down", repeat, func() { c.Scene.Orbit.Orbit(0, -keyOrbitStep) })
	cx.OnKeyWith("w", repeat, func() { c.Scene.Orbit.Orbit(0, keyOrbitStep) })
	cx.OnKeyWith("s", repeat, func() { c.Scene.Orbit.Orbit(0, -keyOrbitStep) })
	cx.OnKeyWith("=", repeat, func() { c.Scene.Orbit.Zoom(-keyZoomStep) })
	cx.OnKeyWith("+", repeat, func() { c.Scene.Orbit.Zoom(-keyZoomStep) })
	cx.OnKeyWith("-", repeat, func() { c.Scene.Orbit.Zoom(keyZoomStep) })
	cx.OnKeyWith("space", once, c.ToggleAutoOrbit)
	cx.OnKeyWith("l", once, c.ToggleLights)
	cx.OnKeyWith("r", once, c.ResetCamera)
	cx.OnKeyWith("h", once, func() { c.HUD.Toggle() })
}

// ToggleAutoOrbit switches continuous gentle camera orbiting on or off.
func (c *Controller) ToggleAutoOrbit() {
	c.Scene.AutoOrbit = !c.Scene.AutoOrbit
	c.autoOrbitSig.Set(c.Scene.AutoOrbit)
}

// ToggleLights switches animated point light & crystal motion on or off.
func (c *Controller) ToggleLights() {
	c.Scene.AnimateLights = !c.Scene.AnimateLights
	c.animLightsSig.Set(c.Scene.AnimateLights)
}

// ResetCamera restores the default camera angle and zoom distance.
func (c *Controller) ResetCamera() {
	c.Scene.ResetCamera()
}

func (c *Controller) onMouseDown(ev core.MouseButtonEvent) {
	if ev.Button != core.MouseButtonLeft {
		return
	}
	c.mouseDragging = true
	c.Scene.Dragging = true
	c.lastMousePos = ev.Position
}

func (c *Controller) onMouseMove(ev core.MouseEvent) {
	if !c.mouseDragging {
		return
	}
	dx := float32(ev.Position.X - c.lastMousePos.X)
	dy := float32(ev.Position.Y - c.lastMousePos.Y)
	c.lastMousePos = ev.Position
	c.Scene.Orbit.Orbit(-dx*dragOrbitSensitivity, dy*dragOrbitSensitivity)
}

func (c *Controller) onMouseUp(ev core.MouseButtonEvent) {
	if ev.Button != core.MouseButtonLeft {
		return
	}
	c.mouseDragging = false
	c.Scene.Dragging = len(c.activeTouches) > 0
}

func (c *Controller) onMouseLeave(core.MouseEvent) {
	if c.mouseDragging {
		c.mouseDragging = false
		c.Scene.Dragging = len(c.activeTouches) > 0
	}
}

func (c *Controller) onWheel(ev core.WheelEvent) {
	if ev.Delta.Y == 0 {
		return
	}
	c.Scene.Orbit.Zoom(float32(ev.Delta.Y) * wheelZoomSensitivity)
}

// onTouch orbits the camera while exactly one finger is down; two or more fingers belong to the pinch, which onPinch follows.
func (c *Controller) onTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart:
		for _, t := range ev.Touches {
			c.activeTouches[t.ID] = t.Position
		}
	case core.TouchMove:
		if len(c.activeTouches) == 1 {
			for _, t := range ev.Touches {
				prev, ok := c.activeTouches[t.ID]
				if !ok {
					continue
				}
				dx := float32(t.Position.X - prev.X)
				dy := float32(t.Position.Y - prev.Y)
				c.Scene.Orbit.Orbit(-dx*dragOrbitSensitivity, dy*dragOrbitSensitivity)
			}
		}
		for _, t := range ev.Touches {
			if _, ok := c.activeTouches[t.ID]; ok {
				c.activeTouches[t.ID] = t.Position
			}
		}
	case core.TouchEnd, core.TouchCancel:
		anyChanged := false
		for _, t := range ev.Touches {
			anyChanged = anyChanged || t.Changed
		}
		if len(ev.Touches) == 0 || ev.Phase == core.TouchCancel || !anyChanged {
			clear(c.activeTouches)
		}
		for _, t := range ev.Touches {
			if t.Changed {
				delete(c.activeTouches, t.ID)
			}
		}
	}
	c.Scene.Dragging = c.mouseDragging || len(c.activeTouches) > 0
}

// onPinch zooms by the pinch's scale, two fingers on a touch screen or a trackpad's pinch on desktops: scale 2 halves the orbit distance it began at.
func (c *Controller) onPinch(ev core.GestureEvent) {
	switch ev.Phase {
	case core.GestureBegan:
		c.pinchStart = c.Scene.Orbit.Distance
	case core.GestureChanged, core.GestureEnded:
		if c.pinchStart > 0 && ev.Value > 0 {
			c.Scene.Orbit.SetDistance(c.pinchStart / ev.Value)
		}
	}
}

func (c *Controller) recordFPS(now time.Time) {
	if !c.lastFrameAt.IsZero() {
		dt := now.Sub(c.lastFrameAt).Seconds()
		if dt > 0 && dt < 1.0 {
			c.fpsAccum += dt
			c.fpsFrames++
			if c.fpsAccum >= 0.25 {
				fps := int(math.Round(float64(c.fpsFrames) / c.fpsAccum))
				if fps != c.fpsSig.GetUntracked() {
					c.fpsSig.Set(fps)
				}
				c.fpsAccum = 0
				c.fpsFrames = 0
			}
		}
	}
	c.lastFrameAt = now
}

func (c *Controller) buildHUD() {
	c.HUD.TestID = "lantern-hud"
	c.HUD.Padding = core.All(12)

	// Top-left: translucent pass-through title & live scene statistics.
	c.HUD.AddPassThrough(scene.HUDTopLeft, core.View{
		Style: core.Style{
			Direction:    core.Column,
			Gap:          2,
			Padding:      core.Edges{Top: 8, Right: 12, Bottom: 8, Left: 12},
			Background:   core.RGBA(0x0B1222, 0.78),
			CornerRadius: 8,
		},
		Children: []core.Node{
			core.Text{
				TestID:  "hud-title",
				Content: "LANTERN 3D",
				Style: core.Style{
					FontSize:   13,
					FontWeight: core.FontWeightBold,
					Color:      core.RGB(0xF8D27A),
				},
			},
			core.Text{
				TestID: "hud-stats",
				ContentFunc: func() string {
					return fmt.Sprintf("%d meshes · %d tris · %d FPS",
						c.Scene.MeshCount(),
						c.Scene.TriangleCount(),
						c.fpsSig.Get(),
					)
				},
				Style: core.Style{
					FontFamily:  core.FontMonospace,
					FontSize:    11,
					FontVariant: core.FontTabularNums,
					Color:       core.RGB(0xCBD5E1),
				},
			},
		},
	})

	// Top-right: interactive native NSGo buttons over the 3D view.
	c.HUD.AddAt(scene.HUDTopRight, core.View{
		Style: core.Style{
			Direction: core.Row,
			Gap:       8,
		},
		Children: []core.Node{
			ui.Button{
				TestID:  "btn-orbit",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				TextFunc: func() string {
					if c.autoOrbitSig.Get() {
						return "Orbit: On"
					}
					return "Orbit: Off"
				},
				OnPress: c.ToggleAutoOrbit,
			},
			ui.Button{
				TestID:  "btn-lights",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				TextFunc: func() string {
					if c.animLightsSig.Get() {
						return "Lights: On"
					}
					return "Lights: Off"
				},
				OnPress: c.ToggleLights,
			},
			ui.Button{
				TestID:  "btn-reset",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				Text:    "Reset",
				OnPress: c.ResetCamera,
			},
		},
	})

	// Bottom-left: subtle controls hint (pass-through so drags work everywhere).
	c.HUD.AddPassThrough(scene.HUDBottomLeft, core.View{
		Style: core.Style{
			Padding:      core.Edges{Top: 5, Right: 10, Bottom: 5, Left: 10},
			Background:   core.RGBA(0x0B1222, 0.72),
			CornerRadius: 6,
		},
		Children: []core.Node{
			core.Text{
				TestID:  "hud-hint",
				Content: "Drag to orbit · Scroll to zoom · Space: orbit · L: lights · R: reset · H: HUD",
				Style: core.Style{
					FontSize: 11,
					Color:    core.RGB(0x94A3B8),
				},
			},
		},
	})

	// Center: graceful fallback banner when no GPU backend is available.
	c.HUD.AddAt(scene.HUDCenter, core.Show{
		When: func() bool { return c.gpuErrSig.Get() != "" },
		Then: func(*core.Scope) core.Node {
			return core.View{
				TestID: "no-gpu-fallback",
				Style: core.Style{
					Direction:    core.Column,
					Gap:          6,
					Padding:      core.All(16),
					Background:   core.RGBA(0x111827, 0.94),
					CornerRadius: 10,
					MaxWidth:     core.Pt(420),
				},
				Children: []core.Node{
					core.Text{
						Content: "3D View Unavailable",
						Style: core.Style{
							FontSize:   15,
							FontWeight: core.FontWeightBold,
							Color:      core.RGB(0xF8D27A),
						},
					},
					core.Text{
						TestID: "no-gpu-message",
						ContentFunc: func() string {
							return c.gpuErrSig.Get()
						},
						Style: core.Style{
							FontSize: 12,
							Color:    core.RGB(0xCBD5E1),
						},
					},
				},
			}
		},
	})
}

func (c *Controller) view(cx *core.Scope) core.Node {
	c.buildHUD()

	view3D := core.View{
		TestID:       "lantern-viewport",
		Style:        core.Style{FlexGrow: 1, FlexShrink: 1},
		OnMouseDown:  c.onMouseDown,
		OnMouseMove:  c.onMouseMove,
		OnMouseUp:    c.onMouseUp,
		OnMouseLeave: c.onMouseLeave,
		OnWheel:      c.onWheel,
		OnTouch:      c.onTouch,
		OnPinch:      c.onPinch,
		Children: []core.Node{
			gpu.View(cx, gpu.ViewProps{
				TestID:          "lantern-gpu",
				DepthFormat:     gpu.FormatDepth24Plus,
				RetainResources: true,
				Style:           core.Style{FlexGrow: 1, FlexShrink: 1},
				OnReady: func(dev *gpu.Device) {
					c.DeviceReady++
					if err := c.Renderer.Setup(cx, dev, c.Scene); err != nil {
						c.gpuErrSig.Set(fmt.Sprintf("Failed to initialize 3D renderer: %v", err))
						return
					}
					if c.gpuErrSig.GetUntracked() != "" {
						c.gpuErrSig.Set("")
					}
				},
				OnResize: func(width, height int) {
					c.Scene.SetViewport(width, height)
				},
				OnFrame: func(frame *gpu.Frame) {
					c.recordFPS(frame.Time())
					cam := c.Scene.CameraFor(c.alpha)
					if err := c.Renderer.Draw(frame, c.Scene, cam); err != nil {
						c.gpuErrSig.Set(fmt.Sprintf("Failed to draw the 3D scene: %v", err))
						return
					}
					c.RenderedFrames++
				},
				OnDeviceLost: func(gpu.DeviceLostEvent) {
					c.DeviceLost++
				},
				OnDeviceRestored: func(dev *gpu.Device) {
					c.DeviceRestored++
					if err := c.Renderer.Setup(cx, dev, c.Scene); err != nil {
						c.gpuErrSig.Set(fmt.Sprintf("Failed to restore 3D renderer: %v", err))
					}
				},
				OnError: func(err gpu.Error) {
					if err.Code == gpu.ErrUnsupported {
						c.gpuErrSig.Set("This environment has no GPU backend. Run Lantern 3D on macOS, iOS, Windows, Linux, Android, or a WebGL 2 / WebGPU browser.")
						return
					}
					c.gpuErrSig.Set(fmt.Sprintf("GPU error: %v", err))
				},
			}),
		},
	}

	return core.View{
		TestID: "lantern-root",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(clearSkyHex),
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			c.HUD.Wrap(cx, view3D),
		},
	}
}
