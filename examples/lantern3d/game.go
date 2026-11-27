package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	dragOrbitSensitivity = 0.0085 // radians per point dragged
	wheelZoomSensitivity = 0.0045 // meters per wheel point
	keyOrbitStep         = 0.14   // radians per single tap
	keyOrbitRate         = 1.6    // radians per second while held
	keyZoomStep          = 0.55   // meters per single tap
	keyZoomRate          = 4.5    // meters per second while held

	switchFadeDuration = 320 * time.Millisecond
)

// SceneMode identifies which primary scene is active in Lantern 3D's scene.Manager.
type SceneMode int

const (
	// Mode3D shows the interactive 3D glTF Lantern Shrine on a gpu.View.
	Mode3D SceneMode = iota
	// Mode2D shows the top-down 2D Courtyard Overworld map on a 2D Canvas.
	Mode2D
)

// Controller connects the 3D glTF Scene, the 2D Courtyard Map2D, the GPU Renderer, scene.Manager (with shared input.Actions and shared sound.Mixer), fixed-step game.Loop, and native scene.HUD3D overlay.
type Controller struct {
	Manager  *scene.Manager
	Actions  *input.Actions
	Audio    *Audio
	Scene    *Scene
	Map      *Map2D
	Renderer *Renderer
	Loop     *game.Loop
	HUD      *scene.HUD3D

	// SwitchFade is the duration of the twilight fade used when switching between 2D and 3D scenes.
	SwitchFade time.Duration

	RenderedFrames uint64
	DeviceReady    int
	DeviceLost     int
	DeviceRestored int

	Shrine3DEnters  int
	Shrine3DExits   int
	Shrine3DPauses  int
	Shrine3DResumes int
	Map2DEnters     int
	Map2DExits      int
	ViewBuilds3D    int
	ViewCleanups3D  int
	SwitchCount     int

	shrine3D     *scene.Controller
	map2D        *scene.Controller
	pauseOverlay *scene.Controller
	stageRef     *core.Ref
	env          *core.Env
	viewport     screen.Viewport
	moveTouch    *input.TouchControls
	useTouch     *input.TouchControls

	alpha       float32
	lastFrameAt time.Time
	fpsAccum    float64
	fpsFrames   int

	modeSig       *core.Signal[SceneMode]
	pausedSig     *core.Signal[bool]
	nearPortalSig *core.Signal[bool]
	autoOrbitSig  *core.Signal[bool]
	animLightsSig *core.Signal[bool]
	fpsSig        *core.Signal[int]
	gpuErrSig     *core.Signal[string]
	frameTick     *core.Signal[uint64]

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

// NewView builds the 3D glTF shrine scene, 2D courtyard map scene, shared input actions, shared audio mixer, scene.Manager host, fixed-step loop, and native HUD overlay, starting in the 3D shrine scene.
func NewView(cx *core.Scope) (*Controller, core.Node) {
	return newView(cx, Mode3D)
}

func newView(cx *core.Scope, initialMode SceneMode) (*Controller, core.Node) {
	cx.SetTitle(gameProject.Title())

	sc, err := NewScene()
	if err != nil {
		panic(err)
	}
	aud, err := NewAudio(cx)
	if err != nil {
		panic(err)
	}

	actions := newSharedActions(cx)
	actions.Mixer = aud.Mixer
	mgr := scene.NewManager(cx)
	mgr.Actions = actions

	moveTouch, err := input.NewTouchControls(cx, filterTouchLayout(touchLayout, "move"))
	if err != nil {
		panic(err)
	}
	useTouch, err := input.NewTouchControls(cx, filterTouchLayout(touchLayout, "use"))
	if err != nil {
		panic(err)
	}
	haptics := input.NewHaptics(cx)
	moveTouch.Haptics = haptics
	useTouch.Haptics = haptics

	c := &Controller{
		Manager:       mgr,
		Actions:       actions,
		Audio:         aud,
		Scene:         sc,
		Map:           NewMap2D(),
		Renderer:      &Renderer{},
		HUD:           scene.NewHUD3D(cx),
		SwitchFade:    switchFadeDuration,
		stageRef:      &core.Ref{},
		env:           cx.Env(),
		viewport:      screen.Viewport{Width: mapWidth, Height: mapHeight, Mode: screen.ModeFit},
		moveTouch:     moveTouch,
		useTouch:      useTouch,
		modeSig:       core.NewSignal(cx, initialMode),
		pausedSig:     core.NewSignal(cx, false),
		nearPortalSig: core.NewSignal(cx, false),
		autoOrbitSig:  core.NewSignal(cx, sc.AutoOrbit),
		animLightsSig: core.NewSignal(cx, sc.AnimateLights),
		fpsSig:        core.NewSignal(cx, 60),
		gpuErrSig:     core.NewSignal(cx, ""),
		frameTick:     core.NewSignal[uint64](cx, 0),
		activeTouches: make(map[int]core.Point, 2),
	}

	c.initScenes()
	if initialMode == Mode2D {
		c.Manager.Switch(c.map2D, nil)
	} else {
		c.Manager.Switch(c.shrine3D, nil)
	}

	c.Loop = &game.Loop{
		Rate: stepRate,
		Update: func(dt time.Duration) {
			c.Actions.Step()
			if c.Actions.JustPressed("toggle_hud") {
				c.HUD.Toggle()
			}
			c.Manager.Update(dt)
		},
		Draw: func(alpha float64) {
			c.alpha = float32(alpha)
			if c.modeSig.GetUntracked() == Mode2D || c.pausedSig.GetUntracked() || c.Manager.Transitioning() {
				c.frameTick.Set(c.frameTick.GetUntracked() + 1)
			}
		},
	}
	c.Loop.Run(cx)

	return c, c.view(cx)
}

func newSharedActions(cx *core.Scope) *input.Actions {
	a := input.NewActions()
	a.Bind("move_left", input.Key("left"), input.Key("a"))
	a.Bind("move_right", input.Key("right"), input.Key("d"))
	a.Bind("move_up", input.Key("up"), input.Key("w"))
	a.Bind("move_down", input.Key("down"), input.Key("s"))
	a.BindAxis("move_x", "move_left", "move_right")
	// move_y is positive upward so keyboard W/Up and TouchControls sticks agree on one axis.
	a.BindAxis("move_y", "move_down", "move_up")

	a.Bind("zoom_in", input.Key("="), input.Key("+"), input.KeyCode("Equal"))
	a.Bind("zoom_out", input.Key("-"), input.KeyCode("Minus"))
	a.Bind("toggle_orbit", input.Key("space"))
	a.Bind("toggle_lights", input.Key("l"))
	a.Bind("reset", input.Key("r"))
	a.Bind("toggle_hud", input.Key("h"))
	a.Bind("switch_scene", input.Key("tab"), input.Key("m"))
	a.Bind("interact", input.Key("e"), input.Key("enter"))
	a.Bind("pause", input.Key("p"), input.Key("escape"))
	a.WatchKeys(cx)
	return a
}

func (c *Controller) initScenes() {
	c.shrine3D = &scene.Controller{
		Enter: func(*core.Scope) {
			c.Shrine3DEnters++
			c.modeSig.Set(Mode3D)
			c.nearPortalSig.Set(false)
			c.deactivateTouchControls()
		},
		Exit: func() {
			c.Shrine3DExits++
			c.clearPointerDrag()
		},
		Pause: func() {
			c.Shrine3DPauses++
			c.clearPointerDrag()
		},
		Resume: func() {
			c.Shrine3DResumes++
		},
		Update: c.update3D,
		View:   c.build3DSceneView,
	}

	c.map2D = &scene.Controller{
		Enter: func(*core.Scope) {
			c.Map2DEnters++
			c.modeSig.Set(Mode2D)
			c.nearPortalSig.Set(c.Map.NearPortal())
			c.activateTouchControls()
		},
		Exit: func() {
			c.Map2DExits++
			c.deactivateTouchControls()
		},
		Pause: func() {
			c.deactivateTouchControls()
		},
		Resume: func() {
			c.activateTouchControls()
		},
		Update: c.update2D,
		View:   c.build2DSceneView,
	}

	c.pauseOverlay = &scene.Controller{
		Transparent: true,
		Enter: func(*core.Scope) {
			c.pausedSig.Set(true)
		},
		Exit: func() {
			c.pausedSig.Set(false)
		},
		Update: func(time.Duration) {
			if c.Actions.JustPressed("pause") {
				c.TogglePause()
				return
			}
			if c.Actions.JustPressed("switch_scene") {
				c.SwitchMode()
			}
		},
		Draw: drawPauseOverlay,
	}
}

func filterTouchLayout(l input.TouchLayout, id string) input.TouchLayout {
	out := l
	out.Controls = nil
	for _, spec := range l.Controls {
		if spec.ID == id {
			out.Controls = append(out.Controls, spec)
		}
	}
	return out
}

func (c *Controller) activateTouchControls() {
	if c.moveTouch == nil || c.useTouch == nil {
		return
	}
	c.moveTouch.BindActions(c.Actions)
	c.moveTouch.Wake()
	c.syncUseTouch(c.Map.NearPortal())
}

func (c *Controller) deactivateTouchControls() {
	if c.moveTouch != nil {
		c.moveTouch.Reset()
		c.moveTouch.BindActions(nil)
	}
	if c.useTouch != nil {
		c.useTouch.Reset()
		c.useTouch.BindActions(nil)
	}
}

func (c *Controller) syncUseTouch(near bool) {
	if c.useTouch == nil {
		return
	}
	if near && c.Mode() == Mode2D && !c.Paused() {
		c.useTouch.BindActions(c.Actions)
		c.useTouch.Wake()
		if c.moveTouch != nil {
			c.useTouch.SetVisible(c.moveTouch.Visible())
		}
		return
	}
	c.useTouch.Reset()
	c.useTouch.BindActions(nil)
}

func (c *Controller) updateTouchControls(dt time.Duration) {
	if c.moveTouch == nil || c.useTouch == nil {
		return
	}
	c.moveTouch.Update(dt)
	if stick := c.moveTouch.Stick("move"); stick != nil && stick.Active() {
		c.useTouch.Wake()
	}
	c.useTouch.Update(dt)
	c.useTouch.SetVisible(c.moveTouch.Visible())
}

func (c *Controller) clearPointerDrag() {
	c.mouseDragging = false
	clear(c.activeTouches)
	c.Scene.Dragging = false
}

func (c *Controller) update3D(dt time.Duration) {
	if c.Actions.JustPressed("switch_scene") {
		c.SwitchTo2D()
		return
	}
	if c.Actions.JustPressed("pause") {
		c.TogglePause()
		return
	}
	if c.Actions.JustPressed("toggle_orbit") {
		c.ToggleAutoOrbit()
	}
	if c.Actions.JustPressed("toggle_lights") {
		c.ToggleLights()
	}
	if c.Actions.JustPressed("reset") {
		c.ResetCamera()
	}

	sec := float32(dt.Seconds())
	if c.Actions.JustPressed("move_left") {
		c.Scene.Orbit.Orbit(-keyOrbitStep, 0)
	} else if c.Actions.Pressed("move_left") {
		c.Scene.Orbit.Orbit(-keyOrbitRate*sec, 0)
	}
	if c.Actions.JustPressed("move_right") {
		c.Scene.Orbit.Orbit(keyOrbitStep, 0)
	} else if c.Actions.Pressed("move_right") {
		c.Scene.Orbit.Orbit(keyOrbitRate*sec, 0)
	}
	if c.Actions.JustPressed("move_up") {
		c.Scene.Orbit.Orbit(0, keyOrbitStep)
	} else if c.Actions.Pressed("move_up") {
		c.Scene.Orbit.Orbit(0, keyOrbitRate*sec)
	}
	if c.Actions.JustPressed("move_down") {
		c.Scene.Orbit.Orbit(0, -keyOrbitStep)
	} else if c.Actions.Pressed("move_down") {
		c.Scene.Orbit.Orbit(0, -keyOrbitRate*sec)
	}
	if c.Actions.JustPressed("zoom_in") {
		c.Scene.Orbit.Zoom(-keyZoomStep)
	} else if c.Actions.Pressed("zoom_in") {
		c.Scene.Orbit.Zoom(-keyZoomRate * sec)
	}
	if c.Actions.JustPressed("zoom_out") {
		c.Scene.Orbit.Zoom(keyZoomStep)
	} else if c.Actions.Pressed("zoom_out") {
		c.Scene.Orbit.Zoom(keyZoomRate * sec)
	}

	if err := c.Scene.Step(dt); err != nil && c.gpuErrSig.GetUntracked() == "" {
		c.gpuErrSig.Set(fmt.Sprintf("Failed to animate the 3D scene: %v", err))
	}
}

func (c *Controller) update2D(dt time.Duration) {
	c.updateTouchControls(dt)

	if c.Actions.JustPressed("switch_scene") {
		c.SwitchTo3D()
		return
	}
	if c.Actions.JustPressed("pause") {
		c.TogglePause()
		return
	}
	if c.Actions.JustPressed("reset") {
		c.ResetCurrentView()
	}

	if c.Scene.AnimateLights {
		c.Scene.LightPhase += lightOrbitRate * dt.Seconds()
	}
	// Map2D Y points downward on screen while move_y is positive upward.
	c.Map.Step(dt, c.Actions.Axis("move_x"), -c.Actions.Axis("move_y"), c.Scene.LightPhase)

	near := c.Map.NearPortal()
	if near != c.nearPortalSig.GetUntracked() {
		c.nearPortalSig.Set(near)
		c.syncUseTouch(near)
	}
	if near && c.Actions.JustPressed("interact") {
		c.SwitchTo3D()
	}
}

// Mode returns the currently active primary scene mode (Mode3D or Mode2D).
func (c *Controller) Mode() SceneMode {
	return c.modeSig.GetUntracked()
}

// Paused reports whether the transparent 2D pause overlay is pushed on top of the active scene.
func (c *Controller) Paused() bool {
	return c.pausedSig.GetUntracked()
}

// SwitchMode fades between the 3D Lantern Shrine and the 2D Courtyard Map.
func (c *Controller) SwitchMode() {
	if c.Mode() == Mode3D {
		c.SwitchTo2D()
	} else {
		c.SwitchTo3D()
	}
}

// SwitchTo2D transitions to the 2D Courtyard Map scene with a twilight fade, keeping shared input and audio alive.
func (c *Controller) SwitchTo2D() {
	if c.Mode() == Mode2D && !c.Paused() {
		return
	}
	c.SwitchCount++
	c.Audio.PlaySwitch()
	c.Manager.Switch(c.map2D, scene.Fade(c.SwitchFade, core.RGB(clearSkyHex)))
}

// SwitchTo3D transitions to the 3D Lantern Shrine scene with a twilight fade, keeping shared input and audio alive.
func (c *Controller) SwitchTo3D() {
	if c.Mode() == Mode3D && !c.Paused() {
		return
	}
	c.SwitchCount++
	c.Audio.PlaySwitch()
	c.Manager.Switch(c.shrine3D, scene.Fade(c.SwitchFade, core.RGB(clearSkyHex)))
}

// TogglePause pushes or pops the transparent 2D pause overlay without unmounting the underlying 3D gpu.View.
func (c *Controller) TogglePause() {
	if c.Manager.Transitioning() {
		return
	}
	c.Audio.PlayAction()
	if c.Paused() {
		c.Manager.Pop()
	} else {
		c.Manager.Push(c.pauseOverlay)
	}
}

// ToggleAutoOrbit switches continuous gentle camera orbiting on or off.
func (c *Controller) ToggleAutoOrbit() {
	c.Scene.AutoOrbit = !c.Scene.AutoOrbit
	c.autoOrbitSig.Set(c.Scene.AutoOrbit)
	c.Audio.PlayAction()
}

// ToggleLights switches animated point light & crystal motion on or off.
func (c *Controller) ToggleLights() {
	c.Scene.AnimateLights = !c.Scene.AnimateLights
	c.animLightsSig.Set(c.Scene.AnimateLights)
	c.Audio.PlayAction()
}

// ResetCamera restores the default 3D camera angle and zoom distance.
func (c *Controller) ResetCamera() {
	c.Scene.ResetCamera()
	c.Audio.PlayAction()
}

// ResetCurrentView resets the 3D camera in 3D mode or returns the 2D traveler to the courtyard gate in 2D mode.
func (c *Controller) ResetCurrentView() {
	if c.Mode() == Mode2D {
		c.Map.Reset()
		near := c.Map.NearPortal()
		c.nearPortalSig.Set(near)
		c.syncUseTouch(near)
		c.Audio.PlayAction()
		return
	}
	c.ResetCamera()
}

func (c *Controller) onMouseDown(ev core.MouseButtonEvent) {
	c.Audio.Unblock()
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
	if ev.Phase == core.TouchStart || ev.Phase == core.TouchEnd {
		c.Audio.Unblock()
	}
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
				TestID: "hud-title",
				ContentFunc: func() string {
					if c.modeSig.Get() == Mode2D {
						return "COURTYARD 2D"
					}
					return "LANTERN 3D"
				},
				Style: core.Style{
					FontSize:   13,
					FontWeight: core.FontWeightBold,
					Color:      core.RGB(0xF8D27A),
				},
			},
			core.Text{
				TestID: "hud-stats",
				ContentFunc: func() string {
					if c.modeSig.Get() == Mode2D {
						if c.nearPortalSig.Get() {
							return "2D Map · Shrine portal ready (E / USE)"
						}
						return "2D Map · Walk north to the shrine"
					}
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

	// Top-right: scene-specific interactive native NSGo buttons.
	c.HUD.AddAt(scene.HUDTopRight, core.View{
		Style: core.Style{
			Direction: core.Row,
			Gap:       8,
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return c.modeSig.Get() == Mode3D },
				Then: func(*core.Scope) core.Node {
					return core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
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
						},
					}
				},
			},
			ui.Button{
				TestID:  "btn-reset",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				Text:    "Reset",
				OnPress: c.ResetCurrentView,
			},
		},
	})

	// Bottom-left: contextual controls hint (pass-through so drags work everywhere).
	c.HUD.AddPassThrough(scene.HUDBottomLeft, core.View{
		Style: core.Style{
			Padding:      core.Edges{Top: 5, Right: 10, Bottom: 5, Left: 10},
			Background:   core.RGBA(0x0B1222, 0.72),
			CornerRadius: 6,
		},
		Children: []core.Node{
			core.Text{
				TestID: "hud-hint",
				ContentFunc: func() string {
					if c.modeSig.Get() == Mode2D {
						return "WASD / Stick: move · E / USE: enter shrine · Tab: 3D · P: pause · H: HUD"
					}
					return "Drag: orbit · Scroll: zoom · Tab: 2D Map · P: pause · Space/L/R/H"
				},
				Style: core.Style{
					FontSize: 11,
					Color:    core.RGB(0x94A3B8),
				},
			},
		},
	})

	// Bottom-right: persistent cross-scene controls (2D <-> 3D switch, pause overlay, shared audio mute).
	c.HUD.AddAt(scene.HUDBottomRight, core.View{
		Style: core.Style{
			Direction: core.Row,
			Gap:       8,
		},
		Children: []core.Node{
			ui.Button{
				TestID:  "btn-switch-scene",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonPrimary,
				TextFunc: func() string {
					if c.modeSig.Get() == Mode3D {
						return "2D Map"
					}
					return "3D Shrine"
				},
				OnPress: c.SwitchMode,
			},
			ui.Button{
				TestID:  "btn-pause",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				TextFunc: func() string {
					if c.pausedSig.Get() {
						return "Resume"
					}
					return "Pause"
				},
				OnPress: c.TogglePause,
			},
			ui.Button{
				TestID:  "btn-mute",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				TextFunc: func() string {
					if c.Audio.Muted() {
						return "Audio: Off"
					}
					return "Audio: On"
				},
				OnPress: c.Audio.ToggleMute,
			},
		},
	})

	// Center: pause menu card & graceful NoGPU fallback banner (shown only when in 3D without a GPU).
	c.HUD.AddAt(scene.HUDCenter, core.View{
		Style: core.Style{
			Direction:  core.Column,
			Gap:        10,
			AlignItems: core.AlignCenter,
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return c.pausedSig.Get() },
				Then: func(*core.Scope) core.Node {
					return core.View{
						TestID: "pause-banner",
						Style: core.Style{
							Direction:    core.Column,
							Gap:          8,
							AlignItems:   core.AlignCenter,
							Padding:      core.All(16),
							Background:   core.RGBA(0x0F172A, 0.94),
							CornerRadius: 10,
							MaxWidth:     core.Pt(320),
						},
						Children: []core.Node{
							core.Text{
								Content: "Paused",
								Style: core.Style{
									FontSize:   16,
									FontWeight: core.FontWeightBold,
									Color:      core.RGB(0xF8D27A),
								},
							},
							core.Text{
								ContentFunc: func() string {
									if c.modeSig.Get() == Mode3D {
										return "3D scene paused underneath without rebuilding GPU pipelines."
									}
									return "2D courtyard map paused underneath."
								},
								Style: core.Style{
									FontSize: 12,
									Color:    core.RGB(0xCBD5E1),
								},
							},
							core.View{
								Style: core.Style{Direction: core.Row, Gap: 8},
								Children: []core.Node{
									ui.Button{
										TestID:  "btn-resume",
										Size:    ui.SizeSmall,
										Variant: ui.ButtonPrimary,
										Text:    "Resume",
										OnPress: c.TogglePause,
									},
									ui.Button{
										TestID:  "btn-pause-switch",
										Size:    ui.SizeSmall,
										Variant: ui.ButtonSecondary,
										TextFunc: func() string {
											if c.modeSig.Get() == Mode3D {
												return "Switch to 2D Map"
											}
											return "Switch to 3D Shrine"
										},
										OnPress: c.SwitchMode,
									},
								},
							},
						},
					}
				},
			},
			core.Show{
				When: func() bool { return c.modeSig.Get() == Mode3D && c.gpuErrSig.Get() != "" },
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
			},
		},
	})
}

func (c *Controller) build3DSceneView(scx *core.Scope) core.Node {
	c.ViewBuilds3D++
	scx.Cleanup(func() {
		c.ViewCleanups3D++
		c.Renderer.Reset()
	})

	return core.View{
		TestID:       "lantern-viewport",
		Style:        core.Style{Width: core.Pct(100), Height: core.Pct(100)},
		OnMouseDown:  c.onMouseDown,
		OnMouseMove:  c.onMouseMove,
		OnMouseUp:    c.onMouseUp,
		OnMouseLeave: c.onMouseLeave,
		OnWheel:      c.onWheel,
		OnTouch:      c.onTouch,
		OnPinch:      c.onPinch,
		Children: []core.Node{
			gpu.View(scx, gpu.ViewProps{
				TestID:          "lantern-gpu",
				DepthFormat:     gpu.FormatDepth24Plus,
				RetainResources: true,
				Style:           core.Style{FlexGrow: 1, FlexShrink: 1},
				OnReady: func(dev *gpu.Device) {
					c.DeviceReady++
					if err := c.Renderer.Setup(scx, dev, c.Scene); err != nil {
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
					if err := c.Renderer.Setup(scx, dev, c.Scene); err != nil {
						c.gpuErrSig.Set(fmt.Sprintf("Failed to restore 3D renderer: %v", err))
					}
				},
				OnError: func(err gpu.Error) {
					if err.Code == gpu.ErrUnsupported {
						c.gpuErrSig.Set("This environment has no GPU backend. Run Lantern 3D on macOS, iOS, Windows, Linux, Android, or a WebGL 2 / WebGPU browser, or switch to the 2D Map.")
						return
					}
					c.gpuErrSig.Set(fmt.Sprintf("GPU error: %v", err))
				},
			}),
		},
	}
}

func (c *Controller) build2DSceneView(*core.Scope) core.Node {
	view := core.View{
		TestID: "lantern-map2d",
		Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
		Children: []core.Node{
			core.Canvas{
				TestID: "lantern-map2d-canvas",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = c.frameTick.Get()
					mode := c.modeSig.Get()
					near := c.nearPortalSig.Get()
					paused := c.pausedSig.Get()
					c.viewport.Draw(d, size, func(dst *core.Drawing, _ core.Size) {
						c.Map.Draw(dst, float64(c.alpha))
					})
					safe := c.env.SafeArea()
					c.moveTouch.Resize(size, safe)
					c.useTouch.Resize(size, safe)
					if mode == Mode2D && !paused {
						c.moveTouch.Draw(d)
						if near {
							c.useTouch.Draw(d)
						}
					}
				},
			},
		},
	}
	c.moveTouch.Attach(&view)
	c.useTouch.Attach(&view)
	return view
}

func (c *Controller) view(cx *core.Scope) core.Node {
	c.buildHUD()

	return c.Manager.View(scene.HostOptions{
		TestID:           "lantern-root",
		StageTestID:      "lantern-stage",
		TransitionTestID: "lantern-transition",
		Ref:              c.stageRef,
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(clearSkyHex),
			Overflow:   core.Hidden,
		},
		Alpha: func() float64 {
			_ = c.frameTick.Get()
			return float64(c.alpha)
		},
		Viewport: &c.viewport,
		Children: []core.Node{
			c.HUD.Layer(cx),
		},
	})
}
