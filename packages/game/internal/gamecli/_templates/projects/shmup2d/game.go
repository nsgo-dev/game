package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// phase is which screen the game is on.
type phase uint8

const (
	phaseTitle phase = iota
	phasePlaying
	phaseEnded
)

// fadeTime is how long a screen takes to fade into the next.
const fadeTime = 250 * time.Millisecond

// Root is the game's root NSGo component: a full-screen Canvas drawing the playfield through the viewport game.yaml sets, driven by a fixed-step game.Loop, with native HUD and menus over it.
func Root(cx *core.Scope) core.Node { return root(cx, nil) }

// root builds the game under cx and hands it to ready, when not nil, as the tests take it.
func root(cx *core.Scope, ready func(*shooter)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	// The playfield is tall: a phone stays upright while the game runs.
	cx.RequestOrientation(core.OrientationPortrait)
	g, err := newShooter(cx)
	if err != nil {
		return core.Text{
			Content: fmt.Sprintf("The game failed to load: %v", err),
			Style:   core.Style{Color: core.RGB(0xf87171), FontSize: 14, Padding: core.All(16)},
		}
	}
	if ready != nil {
		ready(g)
	}
	return g.view()
}

// shooter is the running game: its data, the current run, the screens and the controls that feed it.
type shooter struct {
	cx      *core.Scope
	data    *gameData
	run     *world
	scenes  *scene.Manager
	actions *input.Actions
	drag    drag
	sounds  *sounds
	loop    game.Loop
	alpha   float64
	// sawTouch is whether a finger has touched the game, after which the touch bomb button shows.
	sawTouch bool
	// viewBounds is the game view's laid-out size, which the canvas draws the playfield into and pointer input maps through.
	viewBounds core.Size
	bombIcon   sprite.Batch
	bombSpr    sprite.Sprite

	// What the native HUD and menus show.
	phase  *core.Signal[phase]
	paused *core.Signal[bool]
	result *core.Signal[outcome]
	score  *core.Signal[int]
	lives  *core.Signal[int]
	bombs  *core.Signal[int]
	boss   *core.Signal[string]
	bossHP *core.Signal[float32]
	frame  *core.Signal[uint64]
}

// newShooter loads the game's data, binds its controls and starts its loop on the title screen.
func newShooter(cx *core.Scope) (*shooter, error) {
	data, err := loadData()
	if err != nil {
		return nil, err
	}
	g := &shooter{
		cx:      cx,
		data:    data,
		scenes:  scene.NewManager(cx),
		actions: input.NewActions(),
		phase:   core.NewSignal(cx, phaseTitle),
		paused:  core.NewSignal(cx, false),
		result:  core.NewSignal(cx, running),
		score:   core.NewSignal(cx, 0),
		lives:   core.NewSignal(cx, 0),
		bombs:   core.NewSignal(cx, 0),
		boss:    core.NewSignal(cx, ""),
		bossHP:  core.NewSignal(cx, float32(0)),
		frame:   core.NewSignal(cx, uint64(0)),
	}
	if err := g.newRun(); err != nil {
		return nil, err
	}
	g.sounds = newSounds(cx)
	toGame := func(p core.Point) (core.Point, bool) { return viewport.ViewToGame(g.viewSize(), p) }
	g.drag.toGame = toGame
	g.actions.MapPoint = toGame
	Project.BindInput(g.actions)
	// Keys count wherever the keyboard focus is, such as on a menu button, and every connected controller plays.
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)
	// A run pauses as the app goes to the background, so the player comes back to the pause menu rather than straight into the bullets.
	cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			g.setPaused(true)
		}
	})

	g.bombSpr = sprite.NewSprite(data.atlas.Frames["item_"+itemBomb])
	g.bombSpr.ScaleX, g.bombSpr.ScaleY = 2.5, 2.5
	g.bombSpr.Alpha = 0.7
	g.bombIcon = sprite.Batch{Atlas: data.atlas, Sprites: []*sprite.Sprite{&g.bombSpr}}

	g.loop = game.Loop{
		Rate:      60,
		Immersive: true,
		Update:    g.update,
		Draw: func(alpha float64) {
			g.alpha = alpha
			g.frame.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	g.scenes.Switch(g.titleScene(), nil)
	g.loop.Run(cx)
	return g, nil
}

// viewSize is the size of the game view as last laid out, or the window's before the first layout.
func (g *shooter) viewSize() core.Size {
	if g.viewBounds.Width > 0 && g.viewBounds.Height > 0 {
		return g.viewBounds
	}
	return g.cx.Env().Window()
}

// newRun starts a fresh run of the stage.
func (g *shooter) newRun() error {
	run, err := g.data.newRun(viewport.Width, viewport.Height)
	if err != nil {
		return err
	}
	g.run = run
	g.syncHUD()
	return nil
}

// update runs one fixed step: it samples the controls once and moves the current screen on.
func (g *shooter) update(dt time.Duration) {
	g.actions.Step()
	g.scenes.Update(dt)
}

// syncHUD copies the run's score, lives, bombs and boss health into the signals the HUD shows.
func (g *shooter) syncHUD() {
	if g.score.GetUntracked() != g.run.score {
		g.score.Set(g.run.score)
	}
	if g.lives.GetUntracked() != g.run.ship.lives {
		g.lives.Set(g.run.ship.lives)
	}
	if g.bombs.GetUntracked() != g.run.ship.bombs {
		g.bombs.Set(g.run.ship.bombs)
	}
	name, health, _ := g.run.bossHealth()
	if g.boss.GetUntracked() != name {
		g.boss.Set(name)
	}
	if g.bossHP.GetUntracked() != health {
		g.bossHP.Set(health)
	}
}

// clearInput lets go of every held key, button and pointer, so nothing held on one screen acts on the next.
func (g *shooter) clearInput() {
	g.actions.Reset()
	g.drag.reset()
}

// start starts a new run from the title or end screen.
func (g *shooter) start() {
	if err := g.newRun(); err != nil {
		return
	}
	g.clearInput()
	// A restart from the pause menu ends the pause the way setPaused would, so the new run is heard.
	g.paused.Set(false)
	g.sounds.pause(false)
	g.result.Set(running)
	g.phase.Set(phasePlaying)
	g.scenes.Switch(g.playScene(), scene.Fade(fadeTime, core.RGB(0x02040c)))
	g.sounds.startMusic()
}

// setPaused pauses or resumes a run.
func (g *shooter) setPaused(paused bool) {
	if g.phase.GetUntracked() != phasePlaying || g.paused.GetUntracked() == paused {
		return
	}
	g.clearInput()
	g.paused.Set(paused)
	g.sounds.pause(paused)
}

// end shows the end screen of a run with its outcome.
func (g *shooter) end() {
	g.clearInput()
	g.result.Set(g.run.outcome)
	g.phase.Set(phaseEnded)
	g.sounds.stopMusic()
	g.scenes.Switch(g.endScene(), scene.Fade(fadeTime, core.RGB(0x02040c)))
}

// titleScene scrolls the starfield behind the title menu and starts a run on confirm or fire.
func (g *shooter) titleScene() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			g.run.scroll += g.data.stage.ScrollSpeed * float32(dt.Seconds())
			if g.actions.JustPressed(actionConfirm) || g.actions.JustPressed(actionFire) {
				g.start()
			}
		},
		Draw: func(d *core.Drawing, alpha float64) {
			d.SetFillColor(core.RGB(0x070b1a)).FillRect(0, 0, viewport.Width, viewport.Height)
			g.run.drawStars(d)
		},
	}
}

// playScene steps the run with the player's controls until it ends, and pauses and resumes it.
func (g *shooter) playScene() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			if g.actions.JustPressed(actionPause) {
				g.setPaused(!g.paused.GetUntracked())
			}
			if g.paused.GetUntracked() {
				return
			}
			g.run.step(dt, sample(g.actions, &g.drag))
			g.sounds.play(g.run.events)
			g.syncHUD()
			if g.run.done() {
				g.end()
			}
		},
		Draw: func(d *core.Drawing, alpha float64) {
			g.run.draw(d, float32(alpha))
			if g.sawTouch || g.cx.Env().Pointer() == core.PointerCoarse {
				g.drawBombButton(d)
			}
		},
	}
}

// endScene keeps the last explosions playing behind the end menu and starts a new run on confirm.
func (g *shooter) endScene() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			g.run.step(dt, controls{})
			if g.actions.JustPressed(actionConfirm) {
				g.start()
			}
		},
		Draw: func(d *core.Drawing, alpha float64) { g.run.draw(d, float32(alpha)) },
	}
}

// drawBombButton draws the touch bomb button at bombRegion, dimmed without bombs.
func (g *shooter) drawBombButton(d *core.Drawing) {
	r := bombRegion
	cx, cy := float32(r.Origin.X+r.Size.Width/2), float32(r.Origin.Y+r.Size.Height/2)
	fill := core.RGBA(0xf97316, 0.3)
	if g.run.ship.bombs == 0 {
		fill = core.RGBA(0x475569, 0.25)
	}
	d.SetFillColor(fill).BeginPath().Arc(cx, cy, float32(r.Size.Width/2), 0, 6.2831855, false).Fill()
	g.bombSpr.X, g.bombSpr.Y = cx, cy
	g.bombIcon.Draw(d)
}

// view is the game's view tree: the playfield canvas, which hears touches, the mouse and keys, and the HUD and menus over it.
func (g *shooter) view() core.Node {
	gameView := core.View{
		TestID: "game-view",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(0x02040c)},
		OnLayout: func(b core.Bounds) {
			if b.Size.Width > 0 && b.Size.Height > 0 {
				g.viewBounds = b.Size
			}
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Playfield",
				Style:  fill(),
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.frame.Get()
					viewport.Draw(d, size, func(d *core.Drawing, _ core.Size) {
						g.scenes.Draw(d, core.Size{Width: core.Points(viewport.Width), Height: core.Points(viewport.Height)}, g.alpha)
					})
				},
			},
		},
	}
	g.drag.attach(&gameView)
	onTouch := gameView.OnTouch
	gameView.OnTouch = func(ev core.TouchEvent) {
		g.sawTouch = true
		onTouch(ev)
	}
	g.actions.Attach(&gameView)
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1},
		Children: []core.Node{
			gameView,
			g.hud(),
			g.menus(),
		},
	}
}
