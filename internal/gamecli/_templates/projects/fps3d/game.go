package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/gpu"
)

// session is the running game: the match, the devices feeding it, the 60 Hz loop and the renderer. Play again replaces the match and keeps the rest.
type session struct {
	match    *match
	actions  *input.Actions
	pad      *input.VirtualPad
	loop     *game.Loop
	renderer renderer
	viewRef  core.Ref
	alpha    float64
	paused   bool

	hud    *core.Signal[hudState]
	touch  *core.Signal[bool]
	tick   *core.Signal[uint64]
	gpuErr *core.Signal[string]
}

// hudState is everything the HUD shows; it changes only when one of its fields does, so the HUD rebuilds rarely.
type hudState struct {
	health       int
	weapon       string
	mag, reserve int
	reloading    bool
	wave, waves  int
	enemies      int
	phase        phase
	countdown    int
	paused       bool
}

// Root is the game's root component: the 3D arena on a GPU view, the touch controls and crosshair drawn over it, and a native HUD on top.
func Root(cx *core.Scope) core.Node {
	return newSession(cx).view(cx)
}

// newSession starts a match on cx with its devices and its loop.
func newSession(cx *core.Scope) *session {
	desktop.SetTitle(cx, Project.Title())
	s := &session{
		match:  newMatch(arena),
		pad:    newTouchPad(),
		hud:    core.NewSignal(cx, hudState{}),
		touch:  core.NewSignal(cx, cx.Env().Idiom() == core.IdiomPhone || cx.Env().Idiom() == core.IdiomTablet),
		tick:   core.NewSignal(cx, uint64(0)),
		gpuErr: core.NewSignal(cx, ""),
	}
	s.actions = newActions(s.pad)
	// Keys count wherever the keyboard focus is, and every connected controller plays.
	s.actions.WatchKeys(cx)
	s.actions.WatchGamepads(cx)
	s.publish()
	// Immersive hides a phone's system bars while the game plays.
	s.loop = &game.Loop{
		Rate:      60,
		Immersive: true,
		Update:    s.update,
		Draw: func(alpha float64) {
			s.alpha = alpha
			s.tick.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	s.loop.Run(cx)
	return s
}

// view is the session's component tree.
func (s *session) view(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(skyHex)},
		Children: []core.Node{
			s.arenaView(cx),
			s.hudView(),
			s.endPanel(),
		},
	}
}

// update runs one fixed step: read the devices, handle pause and play again, then step the match.
func (s *session) update(dt time.Duration) {
	s.actions.Step()
	if s.actions.JustPressed(actionPause) {
		if s.match.over() {
			s.restart()
		} else {
			s.paused = !s.paused
		}
	}
	if !s.paused {
		s.match.step(readControls(s.actions, dt), dt)
	}
	// Give the pointer back while the game is paused or over, so the mouse reaches the end panel's button.
	if s.paused || s.match.over() {
		s.actions.UnlockPointer()
	}
	s.publish()
}

// restart starts a new match from the countdown.
func (s *session) restart() {
	s.match = newMatch(arena)
	s.paused = false
	s.actions.Reset()
	s.publish()
}

// publish copies the match into the HUD signal.
func (s *session) publish() {
	m := s.match
	w := m.player.weapons[m.player.weapon]
	s.hud.Set(hudState{
		health:    m.player.health,
		weapon:    w.cfg.Name,
		mag:       w.mag,
		reserve:   w.reserve,
		reloading: m.player.reloading > 0,
		wave:      m.wave + 1,
		waves:     len(m.arena.Waves),
		enemies:   len(m.enemies) + len(m.pending),
		phase:     m.phase,
		countdown: int(math.Ceil(float64(m.timer))),
		paused:    s.paused,
	})
}

// arenaView is the game view: the GPU view and the overlay canvas. A press locks the pointer for mouselook, touches drive the touch pad, and the mouse fires.
func (s *session) arenaView(cx *core.Scope) core.Node {
	v := core.View{
		TestID: "arena",
		Label:  "Arena",
		Ref:    &s.viewRef,
		Style:  fill(),
		OnPress: func(core.PressEvent) {
			s.actions.LockPointer(cx, &s.viewRef)
		},
		OnTouch: func(core.TouchEvent) {
			if !s.touch.GetUntracked() {
				s.touch.Set(true)
			}
		},
		OnLayout: func(b core.Bounds) {
			layoutTouchPad(s.pad, b.Size)
		},
		Children: []core.Node{
			gpu.View(cx, gpu.ViewProps{
				TestID:          "arena-gpu",
				DepthFormat:     gpu.FormatDepth24Plus,
				RetainResources: true,
				Style:           fill(),
				OnReady: func(dev *gpu.Device) {
					s.reportGPU(s.renderer.setup(cx, dev, s.match))
				},
				OnDeviceRestored: func(dev *gpu.Device) {
					s.reportGPU(s.renderer.setup(cx, dev, s.match))
				},
				OnFrame: func(frame *gpu.Frame) {
					s.reportGPU(s.renderer.frame(frame, s.match, float32(s.alpha)))
				},
				OnError: func(err gpu.Error) {
					if err.Code == gpu.ErrUnsupported {
						s.gpuErr.Set("This device has no GPU backend for the 3D view.")
						return
					}
					s.gpuErr.Set(fmt.Sprintf("GPU error: %v", err))
				},
				Children: []core.Node{
					core.Text{
						TestID:      "gpu-message",
						ContentFunc: s.gpuErr.Get,
						Style:       core.Style{Color: core.RGB(0xcbd5e1), FontSize: 14, Margin: core.All(24)},
					},
				},
			}),
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style:         fill(),
				Children: []core.Node{core.Canvas{
					TestID: "overlay",
					Style:  fill(),
					Draw: func(d *core.Drawing, size core.Size) {
						_ = s.tick.Get()
						s.drawOverlay(d, size)
					},
				}},
			},
		},
	}
	s.actions.Attach(&v)
	return v
}

// reportGPU shows a renderer error over the arena, or clears the last one.
func (s *session) reportGPU(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if s.gpuErr.GetUntracked() != msg {
		s.gpuErr.Set(msg)
	}
}

// drawOverlay draws the crosshair, the red edge of a hit taken and, on touch, the touch controls.
func (s *session) drawOverlay(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	m := s.match
	if m.player.hurtFlash > 0 {
		a := m.player.hurtFlash / hurtFlashTime * 0.35
		d.SetStrokeColor(core.RGBA(0xef4444, a)).SetLineWidth(28).BeginPath().Rect(0, 0, w, h).Stroke()
	}
	cx, cy := w/2, h/2
	color := core.RGBA(0xf8fafc, 0.85)
	if m.hitFlash > 0 {
		color = core.RGB(0xfacc15)
	}
	d.SetStrokeColor(color).SetLineWidth(2).BeginPath().
		MoveTo(cx-10, cy).LineTo(cx-4, cy).MoveTo(cx+4, cy).LineTo(cx+10, cy).
		MoveTo(cx, cy-10).LineTo(cx, cy-4).MoveTo(cx, cy+4).LineTo(cx, cy+10).Stroke()
	if s.touch.Get() {
		s.pad.Draw(d, input.DefaultVirtualStyle())
	}
}

// hudView shows health, ammunition, the wave and the status line; it takes no pointer events, so presses reach the arena.
func (s *session) hudView() core.Node {
	text := func(id string, size core.Points, content func(hudState) string) core.Node {
		return core.Text{
			TestID:      id,
			ContentFunc: func() string { return content(s.hud.Get()) },
			Style:       core.Style{Color: core.RGB(0xf8fafc), FontSize: size, FontWeight: core.FontWeightBold},
		}
	}
	panel := func(children ...core.Node) core.Node {
		return core.View{
			Style:    core.Style{Background: core.RGBA(0x0f172a, 0.7), CornerRadius: 10, Padding: core.Symmetric(8, 14), Gap: 2},
			Children: children,
		}
	}
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Direction:      core.Row,
			JustifyContent: core.JustifySpaceBetween,
			AlignItems:     core.AlignStart,
			Padding:        core.All(16),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			panel(
				text("health", 18, func(h hudState) string { return fmt.Sprintf("Health %d", h.health) }),
				text("ammo", 14, func(h hudState) string {
					if h.reloading {
						return fmt.Sprintf("%s reloading", h.weapon)
					}
					return fmt.Sprintf("%s %d / %d", h.weapon, h.mag, h.reserve)
				}),
			),
			text("status", 20, statusLine),
			panel(
				text("wave", 18, func(h hudState) string { return fmt.Sprintf("Wave %d / %d", h.wave, h.waves) }),
				text("enemies", 14, func(h hudState) string { return fmt.Sprintf("Enemies %d", h.enemies) }),
			),
		},
	}
}

// statusLine is the line across the top: the countdown, the coming wave, the pause or the result.
func statusLine(h hudState) string {
	switch {
	case h.phase == phaseWon:
		return "Arena cleared"
	case h.phase == phaseLost:
		return "You were downed"
	case h.paused:
		return "Paused"
	case h.phase == phaseCountdown, h.phase == phaseIntermission:
		return fmt.Sprintf("Wave %d in %d", h.wave, h.countdown)
	}
	return ""
}

// endPanel offers another match once this one is won or lost.
func (s *session) endPanel() core.Node {
	return core.Show{
		When: func() bool { p := s.hud.Get().phase; return p == phaseWon || p == phaseLost },
		Then: func(*core.Scope) core.Node {
			return core.View{
				TestID: "end-panel",
				Style: core.Style{
					Position:       core.Absolute,
					Top:            core.Pt(0),
					Left:           core.Pt(0),
					Right:          core.Pt(0),
					Bottom:         core.Pt(0),
					JustifyContent: core.JustifyCenter,
					AlignItems:     core.AlignCenter,
					Background:     core.RGBA(0x020617, 0.55),
				},
				Children: []core.Node{
					base.Button{
						Label:     "Play again",
						AutoFocus: true,
						OnPress:   func(core.PressEvent) { s.restart() },
						Style: core.Style{
							Background:   core.RGB(0x0284c7),
							CornerRadius: 10,
							Padding:      core.Symmetric(12, 22),
							Active:       &core.Style{Opacity: core.Alpha(0.8)},
						},
						Children: []core.Node{
							core.Text{Content: "Play again", Style: core.Style{Color: core.RGB(0xffffff), FontSize: 16, FontWeight: core.FontWeightBold}},
						},
					},
				},
			}
		},
	}
}

func fill() core.Style {
	return core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}
}
