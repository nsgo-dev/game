package main

import (
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

type gamePhase uint8

const (
	phaseTitle gamePhase = iota
	phasePlaying
	phaseVictory
	phaseGameOver
)

const (
	soundGroupSFX   = "sfx"
	soundGroupMusic = "music"

	sceneFadeDuration = 200 * time.Millisecond
)

// Action names bound by bindControls. left, right and jump drive the explorer; the rest drive the menus.
const (
	actionLeft    = "left"
	actionRight   = "right"
	actionJump    = "jump"
	actionMove    = "move"
	actionConfirm = "confirm"
	actionPause   = "pause"
	actionRestart = "restart"
	actionSwarm   = "swarm"
)

// stickDeadZone is the left stick deflection below which the explorer stands still.
const stickDeadZone = 0.3

// Virtual-space input regions (in 320x180 virtual canvas coordinates).
var (
	touchRegionLeft  = core.Bounds{Origin: core.Point{X: 0, Y: 116}, Size: core.Size{Width: 72, Height: 64}}
	touchRegionRight = core.Bounds{Origin: core.Point{X: 72, Y: 116}, Size: core.Size{Width: 72, Height: 64}}
	touchRegionJump  = core.Bounds{Origin: core.Point{X: 224, Y: 116}, Size: core.Size{Width: 96, Height: 64}}
)

type caveRun struct {
	cx       *core.Scope
	env      *core.Env
	assets   *gameAssets
	viewport screen.Viewport
	viewSize core.Size
	gameRef  core.Ref

	loop    game.Loop
	scenes  *scene.Manager
	actions *input.Actions
	level   *levelState

	// Audio mixer, effects, and music track.
	mixer       *sound.Mixer
	music       *sound.Music
	fxJump      *sound.Effect
	fxGem       *sound.Effect
	fxHurt      *sound.Effect
	fxWin       *sound.Effect
	savedMaster float32

	// Native HUD touch-button state merged with input.Actions each fixed step; the HUD buttons are native views, not Actions inputs.
	padLeft          bool
	padRight         bool
	padJump          bool
	padJumpLatched   bool
	prevCombinedJump bool

	// Interpolation alpha for the current draw frame.
	alpha float64

	// Reactive signals consumed by the native HUD & menus.
	phase     *core.Signal[gamePhase]
	paused    *core.Signal[bool]
	hearts    *core.Signal[int]
	gems      *core.Signal[int]
	totalGems *core.Signal[int]
	score     *core.Signal[int]
	frameTick *core.Signal[int]
	// swarm is whether the 1000-firefly stress swarm is on; it stays on across restarts. sprites is how many sprites the level draws.
	swarm   *core.Signal[bool]
	sprites *core.Signal[int]
}

func newCaveRun(cx *core.Scope) (*caveRun, error) {
	assets, err := loadGameAssets()
	if err != nil {
		return nil, err
	}

	g := &caveRun{
		cx:     cx,
		env:    cx.Env(),
		assets: assets,
		viewport: screen.Viewport{
			Width:        virtualWidth,
			Height:       virtualHeight,
			Mode:         screen.ModeFit,
			IntegerScale: true,
			PixelSnap:    true,
			BarColor:     core.RGB(0x0b0d17),
		},
		scenes:      scene.NewManager(cx),
		actions:     input.NewActions(),
		mixer:       sound.NewMixer(cx, "caverun.sound"),
		savedMaster: 1,
		phase:       core.NewSignal(cx, phaseTitle),
		paused:      core.NewSignal(cx, false),
		hearts:      core.NewSignal(cx, maxHearts),
		gems:        core.NewSignal(cx, 0),
		totalGems:   core.NewSignal(cx, 0),
		score:       core.NewSignal(cx, 0),
		frameTick:   core.NewSignal(cx, 0),
		swarm:       core.NewSignal(cx, false),
		sprites:     core.NewSignal(cx, 0),
	}

	g.music = g.mixer.Music(soundGroupMusic)
	g.music.Loop = true
	g.loadAudio(cx)

	// Map view pointer coordinates into 320x180 virtual canvas coordinates.
	g.actions.MapPoint = func(p core.Point) (core.Point, bool) {
		return g.viewport.ViewToGame(g.currentViewSize(), p)
	}
	bindControls(g.actions)
	// Keys count wherever the keyboard focus is, such as on a HUD button, and every connected controller drives the explorer.
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)

	// Build initial level for title backdrop.
	if err := g.resetLevel(); err != nil {
		return nil, err
	}
	g.scenes.Switch(g.titleController(), nil)

	// Fixed-step 60 Hz game loop driving updates and triggering Canvas redraws.
	g.loop = game.Loop{
		Rate:   60,
		Update: g.update,
		Draw:   g.onLoopDraw,
	}
	g.loop.Run(cx)

	return g, nil
}

func (g *caveRun) loadAudio(cx *core.Scope) {
	loadFX := func(src audio.Source, dest **sound.Effect) {
		audio.LoadSound(cx, src, func(s *audio.Sound, err error) {
			if err != nil || s == nil {
				return
			}
			*dest = g.mixer.Effect(s, soundGroupSFX, 4)
		})
	}
	loadFX(g.assets.sfxJump, &g.fxJump)
	loadFX(g.assets.sfxGem, &g.fxGem)
	loadFX(g.assets.sfxHurt, &g.fxHurt)
	loadFX(g.assets.sfxWin, &g.fxWin)
}

func (g *caveRun) playFX(fx *sound.Effect) {
	if fx != nil {
		fx.Play(audio.Voice{Volume: 1, Rate: 1})
	}
}

func (g *caveRun) currentViewSize() core.Size {
	if g.viewSize.Width > 0 && g.viewSize.Height > 0 {
		return g.viewSize
	}
	if g.env != nil {
		return g.env.Window()
	}
	return core.Size{Width: core.Points(virtualWidth), Height: core.Points(virtualHeight)}
}

func (g *caveRun) resetLevel() error {
	lvl, err := newLevelState(g.assets, levelCallbacks{
		onJump: func() {
			g.playFX(g.fxJump)
		},
		onGem: func(int) {
			g.playFX(g.fxGem)
			g.syncHUD()
		},
		onHurt: func(int) {
			g.playFX(g.fxHurt)
			g.syncHUD()
		},
		onWin: func(score, gems, hearts int) {
			g.playFX(g.fxWin)
			g.syncHUD()
			g.music.Stop(300 * time.Millisecond)
			g.phase.Set(phaseVictory)
			g.paused.Set(false)
			g.clearHeldInputs()
			g.scenes.Switch(g.victoryController(), scene.Fade(sceneFadeDuration, core.RGB(0x0b0d17)))
		},
		onLose: func(score, gems int) {
			g.syncHUD()
			g.music.Stop(250 * time.Millisecond)
			g.phase.Set(phaseGameOver)
			g.paused.Set(false)
			g.clearHeldInputs()
			g.scenes.Switch(g.gameOverController(), scene.Fade(sceneFadeDuration, core.RGB(0x1a0810)))
		},
	})
	if err != nil {
		return err
	}
	lvl.setSwarm(g.swarm.GetUntracked())
	g.level = lvl
	g.syncHUD()
	return nil
}

func (g *caveRun) syncHUD() {
	if g.level == nil {
		return
	}
	g.hearts.Set(g.level.hearts)
	g.gems.Set(g.level.gemsCollected)
	g.totalGems.Set(len(g.level.gems))
	g.score.Set(g.level.score)
	g.sprites.Set(g.level.spriteCount())
}

// toggleSwarm turns the 1000-firefly stress swarm on or off.
func (g *caveRun) toggleSwarm() {
	on := !g.swarm.GetUntracked()
	g.swarm.Set(on)
	if g.level != nil {
		g.level.setSwarm(on)
	}
	g.syncHUD()
}

func (g *caveRun) clearHeldInputs() {
	g.actions.Reset()
	g.padLeft = false
	g.padRight = false
	g.padJump = false
	g.padJumpLatched = false
	g.prevCombinedJump = false
}

func (g *caveRun) startGame() {
	if err := g.resetLevel(); err != nil {
		return
	}
	g.clearHeldInputs()
	g.paused.Set(false)
	g.phase.Set(phasePlaying)
	g.scenes.Switch(g.playingController(), scene.Fade(sceneFadeDuration, core.RGB(0x0b0d17)))
	g.music.Play(g.assets.musicCave, 300*time.Millisecond)
	g.gameRef.Focus()
}

func (g *caveRun) restartGame() {
	if err := g.resetLevel(); err != nil {
		return
	}
	g.clearHeldInputs()
	g.paused.Set(false)
	g.phase.Set(phasePlaying)
	g.scenes.Switch(g.playingController(), scene.Fade(sceneFadeDuration, core.RGB(0x0b0d17)))
	g.music.Play(g.assets.musicCave, 200*time.Millisecond)
	g.gameRef.Focus()
}

func (g *caveRun) pauseGame() {
	if g.phase.Get() != phasePlaying || g.paused.Get() {
		return
	}
	g.paused.Set(true)
	g.clearHeldInputs()
	g.music.Stop(150 * time.Millisecond)
}

func (g *caveRun) resumeGame() {
	if g.phase.Get() != phasePlaying || !g.paused.Get() {
		return
	}
	g.paused.Set(false)
	g.clearHeldInputs()
	g.music.Play(g.assets.musicCave, 150*time.Millisecond)
	g.gameRef.Focus()
}

func (g *caveRun) togglePause() {
	if g.paused.Get() {
		g.resumeGame()
	} else {
		g.pauseGame()
	}
}

func (g *caveRun) isMuted() bool {
	return g.mixer.Master() <= 0
}

func (g *caveRun) toggleMute() {
	cur := g.mixer.Master()
	if cur > 0 {
		g.savedMaster = cur
		_ = g.mixer.SetMaster(0)
	} else {
		restore := g.savedMaster
		if restore <= 0 {
			restore = 1
		}
		_ = g.mixer.SetMaster(restore)
	}
}

// bindControls binds Cave Run's actions to the keyboard, game controllers, touch and the mouse. Touch and mouse regions are in the 320x180 virtual canvas, which Actions.MapPoint maps view points into.
func bindControls(a *input.Actions) {
	a.Bind(actionLeft,
		input.Key("a"), input.Key("left"),
		input.GamepadButton(gamepad.ButtonDPadLeft),
		input.TouchRegion(touchRegionLeft),
		input.MouseRegion(touchRegionLeft, core.MouseButtonLeft),
	)
	a.Bind(actionRight,
		input.Key("d"), input.Key("right"),
		input.GamepadButton(gamepad.ButtonDPadRight),
		input.TouchRegion(touchRegionRight),
		input.MouseRegion(touchRegionRight, core.MouseButtonLeft),
	)
	a.Bind(actionJump,
		input.Key("space"), input.Key("w"), input.Key("up"), input.Key("z"),
		input.GamepadButton(gamepad.ButtonSouth),
		input.TouchRegion(touchRegionJump),
		input.MouseRegion(touchRegionJump, core.MouseButtonLeft),
	)
	a.BindAxis(actionMove, actionLeft, actionRight)
	a.BindGamepadAxis(actionMove, gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: stickDeadZone, Axial: true})

	a.Bind(actionConfirm, input.Key("enter"), input.GamepadButton(gamepad.ButtonStart))
	a.Bind(actionPause, input.Key("escape"), input.Key("p"), input.GamepadButton(gamepad.ButtonStart))
	a.Bind(actionRestart, input.Key("r"), input.GamepadButton(gamepad.ButtonBack))
	a.Bind(actionSwarm, input.Key("f"), input.GamepadButton(gamepad.ButtonNorth))
}

// handleMenuActions acts on the step's menu actions: start from the title, pause, restart, toggle the firefly swarm, and play again from an end screen.
func (g *caveRun) handleMenuActions() {
	a := g.actions
	switch g.phase.Get() {
	case phaseTitle:
		switch {
		case a.JustPressed(actionConfirm) || a.JustPressed(actionJump):
			g.startGame()
		case a.JustPressed(actionSwarm):
			g.toggleSwarm()
		}
	case phasePlaying:
		switch {
		case a.JustPressed(actionRestart):
			g.restartGame()
		case a.JustPressed(actionPause):
			g.togglePause()
		case a.JustPressed(actionSwarm):
			g.toggleSwarm()
		}
	case phaseVictory, phaseGameOver:
		if a.JustPressed(actionConfirm) || a.JustPressed(actionRestart) {
			g.restartGame()
		}
	}
}

// sampleInput merges the step's actions with the native HUD touch buttons into the explorer's input.
func (g *caveRun) sampleInput() stepInput {
	a := g.actions
	move := float32(a.Axis(actionMove))
	// A HUD button held alone overrides the analog axis, as a held key does.
	if g.padLeft && !g.padRight {
		move = -1
	} else if g.padRight && !g.padLeft {
		move = 1
	}

	jumpHeld := a.Pressed(actionJump) || g.padJump
	jumpPressed := a.JustPressed(actionJump) || g.padJumpLatched || (jumpHeld && !g.prevCombinedJump)
	jumpReleased := !jumpHeld && g.prevCombinedJump

	g.padJumpLatched = false
	g.prevCombinedJump = jumpHeld

	return stepInput{
		moveAxis:     move,
		jumpPressed:  jumpPressed,
		jumpHeld:     jumpHeld,
		jumpReleased: jumpReleased,
	}
}

// update runs one fixed step: it samples input once, acts on the menu actions, and advances the current scene.
func (g *caveRun) update(dt time.Duration) {
	g.actions.Step()
	g.handleMenuActions()
	g.scenes.Update(dt)
}

func (g *caveRun) onLoopDraw(alpha float64) {
	g.alpha = alpha
	g.frameTick.Update(func(n int) int { return n + 1 })
}

func (g *caveRun) drawCanvas(d *core.Drawing, size core.Size) {
	// Subscribe canvas redraws to the fixed-step game loop frames.
	_ = g.frameTick.Get()
	g.viewport.Draw(d, size, func(d *core.Drawing, vsize core.Size) {
		// Clear background to deep cave indigo.
		d.SetFillColor(core.RGB(0x131628)).FillRect(0, 0, float32(vsize.Width), float32(vsize.Height))
		g.scenes.Draw(d, vsize, g.alpha)
	})
}

func (g *caveRun) titleController() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			if g.level != nil {
				g.level.updateDemo(dt)
			}
		},
		Draw: func(d *core.Drawing, alpha float64) {
			if g.level != nil {
				g.level.draw(d, alpha, g.viewport)
			}
			// Render retro bitmap-font title banner directly on the virtual canvas.
			if g.assets.font != nil {
				g.assets.font.Draw(d, "CAVE RUN", virtualWidth/2, 20, text.Options{
					Align: text.AlignCenter,
					Scale: 2,
				})
			}
		},
	}
}

func (g *caveRun) playingController() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			if g.paused.Get() || g.level == nil {
				return
			}
			g.level.update(dt, g.sampleInput())
		},
		Draw: func(d *core.Drawing, alpha float64) {
			if g.level != nil {
				g.level.draw(d, alpha, g.viewport)
			}
		},
	}
}

func (g *caveRun) victoryController() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			if g.level != nil {
				g.level.updateEffects(dt)
			}
		},
		Draw: func(d *core.Drawing, alpha float64) {
			if g.level != nil {
				g.level.draw(d, alpha, g.viewport)
			}
			if g.assets.font != nil {
				g.assets.font.Draw(d, "CLEAR!", virtualWidth/2, 22, text.Options{
					Align: text.AlignCenter,
					Scale: 2,
				})
			}
		},
	}
}

func (g *caveRun) gameOverController() *scene.Controller {
	return &scene.Controller{
		Update: func(dt time.Duration) {
			if g.level != nil {
				g.level.updateEffects(dt)
			}
		},
		Draw: func(d *core.Drawing, alpha float64) {
			if g.level != nil {
				g.level.draw(d, alpha, g.viewport)
			}
			if g.assets.font != nil {
				g.assets.font.Draw(d, "GAME OVER", virtualWidth/2, 22, text.Options{
					Align: text.AlignCenter,
					Scale: 2,
				})
			}
		},
	}
}
