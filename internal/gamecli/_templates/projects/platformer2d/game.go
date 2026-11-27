package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/platformer"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// The actions game.yaml's input map declares and this file reads.
const (
	actionMove    = "move"
	actionJump    = "jump"
	actionRestart = "restart"
)

// platformerGame is the running app: the current run of the level, the input it is played with, the virtual screen it is drawn on and the signals the native HUD shows.
type platformerGame struct {
	data     gameData
	world    *world
	painter  *painter
	actions  *input.Actions
	viewport screen.Viewport
	env      *core.Env
	viewSize core.Size
	// pointerSeen turns the on-screen controls on once the game view has been touched or clicked, so a player on a desktop sees the regions the mouse plays through.
	pointerSeen bool

	sparks *core.Signal[int]
	lives  *core.Signal[int]
	state  *core.Signal[outcome]
	frame  *core.Signal[uint64]
}

// Root is the game's root NSGo component: the level drawn on a full-window Canvas by a fixed-step game.Loop, with a native HUD on top.
func Root(cx *core.Scope) core.Node {
	return newRoot(cx, nil)
}

// newRoot builds the root component and passes the game to hook, which the tests use to watch the run.
func newRoot(cx *core.Scope, hook func(*platformerGame)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	viewport, _ := Project.Viewport()
	g := &platformerGame{
		data:     mustLoadData(),
		actions:  input.NewActions(),
		viewport: viewport,
		env:      cx.Env(),
		sparks:   core.NewSignal(cx, 0),
		lives:    core.NewSignal(cx, 0),
		state:    core.NewSignal(cx, running),
		frame:    core.NewSignal(cx, uint64(0)),
	}
	g.restart()

	// Keyboard, controller and touch bindings all come from game.yaml's input map; touch and mouse regions are in the virtual screen, which MapPoint maps view points into.
	Project.BindInput(g.actions)
	g.actions.MapPoint = func(p core.Point) (core.Point, bool) {
		return g.viewport.ViewToGame(g.currentViewSize(), p)
	}
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)

	loop := &game.Loop{
		Rate:   60,
		Update: g.update,
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	loop.Run(cx)
	if hook != nil {
		hook(g)
	}
	return g.view()
}

// restart starts a new run of the level.
func (g *platformerGame) restart() {
	w, err := newWorld(g.data, g.viewport.Width, g.viewport.Height)
	if err != nil {
		// The tests build the embedded level, so this only happens after an edit they would have caught.
		panic(err)
	}
	g.world = w
	g.painter = newPainter(w)
	g.actions.Reset()
	g.syncHUD()
}

// update runs one fixed step: it samples the actions, steps the run and updates the HUD. Once the run has ended, restart starts another.
func (g *platformerGame) update(dt time.Duration) {
	g.actions.Step()
	if g.world.state != running && g.actions.JustPressed(actionRestart) {
		g.restart()
		return
	}
	g.world.step(platformer.Input{
		Move:        float32(g.actions.Axis(actionMove)),
		JumpPressed: g.actions.JustPressed(actionJump),
		JumpHeld:    g.actions.Pressed(actionJump),
	}, dt)
	g.painter.update(g.world, dt)
	g.syncHUD()
}

// syncHUD copies the run's score into the signals the HUD shows.
func (g *platformerGame) syncHUD() {
	g.sparks.Set(g.world.collected)
	g.lives.Set(g.world.lives)
	g.state.Set(g.world.state)
}

// currentViewSize returns the game view's size, or the window's before the view is laid out.
func (g *platformerGame) currentViewSize() core.Size {
	if g.viewSize.Width > 0 && g.viewSize.Height > 0 {
		return g.viewSize
	}
	return g.env.Window()
}

// showTouchControls reports whether to draw the on-screen controls: on phones and tablets, and anywhere once the game view has been touched or clicked.
func (g *platformerGame) showTouchControls() bool {
	switch g.env.Idiom() {
	case core.IdiomDesktop, core.IdiomMac, core.IdiomTV:
		return g.pointerSeen
	default:
		return true
	}
}

// view lays out the game view, which hears the input and draws the level, under the HUD and the end-of-run panel.
func (g *platformerGame) view() core.Node {
	gameView := core.View{
		TestID: "game-view",
		Style: core.Style{
			Position: core.Absolute,
			Top:      core.Pt(0),
			Right:    core.Pt(0),
			Bottom:   core.Pt(0),
			Left:     core.Pt(0),
		},
		Focusable:   func() bool { return true },
		OnLayout:    func(b core.Bounds) { g.viewSize = b.Size },
		OnTouch:     func(core.TouchEvent) { g.pointerSeen = true },
		OnPressDown: func(core.PressEvent) { g.pointerSeen = true },
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Platformer level",
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
					Left:     core.Pt(0),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.frame.Get()
					g.viewport.Draw(d, size, func(d *core.Drawing, virtual core.Size) {
						g.painter.draw(d, g.world, virtual)
						if g.showTouchControls() {
							g.painter.drawTouchControls(d, g.actions)
						}
					})
				},
			},
		},
	}
	g.actions.Attach(&gameView)

	return core.View{
		Style: core.Style{FlexGrow: 1, Background: core.RGB(0x10162b)},
		Children: []core.Node{
			gameView,
			g.hud(),
			core.Show{
				When: func() bool { return g.state.Get() != running },
				Then: func(*core.Scope) core.Node { return g.endPanel() },
			},
		},
	}
}

var (
	hudText   = core.Style{Color: core.RGB(0xf8fafc), FontSize: 15, FontWeight: core.FontWeightBold}
	panelText = core.Style{Color: core.RGB(0xcbd5e1), FontSize: 14}
)

// hud shows the sparks collected and the lives left in the top left corner, letting touches through to the game.
func (g *platformerGame) hud() core.Node {
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Left:       core.Pt(0),
			Padding:    core.All(12),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					Gap:          14,
					Padding:      core.Symmetric(6, 12),
					CornerRadius: 8,
					Background:   core.RGBA(0x0f172a, 0.75),
				},
				Children: []core.Node{
					core.Text{
						TestID:      "sparks",
						ContentFunc: func() string { return fmt.Sprintf("Sparks %d/%d", g.sparks.Get(), g.world.sparksTotal()) },
						Style:       hudText,
					},
					core.Text{
						TestID:      "lives",
						ContentFunc: func() string { return fmt.Sprintf("Lives %d", g.lives.Get()) },
						Style:       hudText,
					},
				},
			},
		},
	}
}

// endPanel says how the run ended and offers another.
func (g *platformerGame) endPanel() core.Node {
	return core.View{
		TestID: "end-panel",
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Left:           core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     core.RGBA(0x020617, 0.55),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					AlignItems:   core.AlignCenter,
					Gap:          10,
					Padding:      core.Symmetric(18, 28),
					CornerRadius: 12,
					Background:   core.RGBA(0x0f172a, 0.92),
				},
				Children: []core.Node{
					core.Text{
						TestID:  "level-name",
						Content: g.data.level.Name,
						Style:   core.Style{Color: core.RGB(0x94a3b8), FontSize: 12},
					},
					core.Text{
						TestID: "end-title",
						ContentFunc: func() string {
							if g.state.Get() == won {
								return "Beacon lit!"
							}
							return "Out of lives"
						},
						Style: core.Style{Color: core.RGB(0xfde68a), FontSize: 24, FontWeight: core.FontWeightBold},
					},
					core.Text{
						ContentFunc: func() string {
							return fmt.Sprintf("%d of %d sparks", g.sparks.Get(), g.world.sparksTotal())
						},
						Style: panelText,
					},
					base.Button{
						TestID:  "play-again",
						Label:   "Play again",
						OnPress: func(core.PressEvent) { g.restart() },
						Style: core.Style{
							Padding:      core.Symmetric(8, 18),
							CornerRadius: 8,
							Background:   core.RGB(0x2563eb),
							Active:       &core.Style{Opacity: core.Alpha(0.8)},
						},
						Children: []core.Node{
							core.Text{Content: "Play again", Style: hudText},
						},
					},
					core.Text{Content: "R, Enter or Start", Style: core.Style{Color: core.RGB(0x94a3b8), FontSize: 12}},
				},
			},
		},
	}
}
