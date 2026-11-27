package main

import (
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/quality"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/text"
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
	actionLeft     = "left"
	actionRight    = "right"
	actionJump     = "jump"
	actionMove     = "move"
	actionConfirm  = "confirm"
	actionPause    = "pause"
	actionRestart  = "restart"
	actionSwarm    = "swarm"
	actionSettings = "settings"
)

// stickDeadZone is the left stick deflection below which the explorer stands still.
const stickDeadZone = 0.3

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
	// touch is the on-screen d-pad and jump button, which drive the same left, right and jump actions as the keyboard and controllers.
	touch *input.TouchControls
	level *levelState

	// Audio mixer, effects, and music track.
	mixer       *sound.Mixer
	music       *sound.Music
	fxJump      *sound.Effect
	fxGem       *sound.Effect
	fxHurt      *sound.Effect
	fxWin       *sound.Effect
	savedMaster float32

	// Interpolation alpha for the current draw frame.
	alpha float64

	// Reactive signals consumed by the native HUD & menus.
	phase       *core.Signal[gamePhase]
	paused      *core.Signal[bool]
	levelLoaded *core.Signal[bool]
	levelErr    *core.Signal[error]
	pendingPlay bool
	hearts      *core.Signal[int]
	gems        *core.Signal[int]
	totalGems   *core.Signal[int]
	score       *core.Signal[int]
	frameTick   *core.Signal[int]
	// swarm is whether the 1000-firefly stress swarm is on; it stays on across restarts. sprites is how many sprites the level draws.
	swarm   *core.Signal[bool]
	sprites *core.Signal[int]

	// Ready settings menu and persisted player settings.
	settingsStore *save.DefaultSettingsStore
	settingsMenu  *gameui.SettingsMenu
	settingsOpen  *core.Signal[bool]
	settingsTick  *core.Signal[int]
	settingsRef   core.Ref
	video         *screen.Video
	quality       *quality.Selector
	catalog       *l10n.Catalog
	locale        *core.Signal[string]
	a11y          save.AccessibilitySettings
	resolution    string
	reduceFlashes bool
}

func newCaveRun(cx *core.Scope) (*caveRun, error) {
	assets, err := loadTitleAssets(cx)
	if err != nil {
		return nil, err
	}

	g := &caveRun{
		cx:           cx,
		env:          cx.Env(),
		assets:       assets,
		viewport:     gameViewport,
		scenes:       scene.NewManager(cx),
		actions:      input.NewActions(),
		mixer:        sound.NewMixer(cx, "caverun.sound"),
		savedMaster:  1,
		phase:        core.NewSignal(cx, phaseTitle),
		paused:       core.NewSignal(cx, false),
		levelLoaded:  core.NewSignal(cx, false),
		levelErr:     core.NewSignalFunc[error](cx, nil, func(a, b error) bool { return a == nil && b == nil }),
		hearts:       core.NewSignal(cx, maxHearts),
		gems:         core.NewSignal(cx, 0),
		totalGems:    core.NewSignal(cx, 0),
		score:        core.NewSignal(cx, 0),
		frameTick:    core.NewSignal(cx, 0),
		swarm:        core.NewSignal(cx, false),
		sprites:      core.NewSignal(cx, 0),
		settingsOpen: core.NewSignal(cx, false),
		settingsTick: core.NewSignal(cx, 0),
		locale:       core.NewSignal(cx, ""),
	}

	gameProject.ConfigureMixer(g.mixer)
	g.music = g.mixer.Music(soundGroupMusic)
	g.music.Loop = true
	g.loadAudio(cx)

	// Map view pointer coordinates into 320x180 virtual canvas coordinates.
	g.actions.MapPoint = func(p core.Point) (core.Point, bool) {
		size := g.currentViewSize()
		return g.effectiveViewport(size).ViewToGame(size, p)
	}
	bindControls(g.actions)
	touch, err := input.NewTouchControls(cx, touchLayout)
	if err != nil {
		return nil, err
	}
	touch.BindActions(g.actions)
	touch.Haptics = input.NewHaptics(cx)
	g.touch = touch
	// Keys count wherever the keyboard focus is, such as on a HUD button, and every connected controller drives the explorer.
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)

	// Fixed-step 60 Hz game loop driving updates and triggering Canvas redraws.
	g.loop = game.Loop{
		Rate:   60,
		Update: g.update,
		Draw:   g.onLoopDraw,
	}

	g.initSettings(cx)

	// Show the title screen immediately using the embedded title font, and
	// stream the level group from resources/ in the background.
	g.scenes.Switch(g.titleController(), nil)
	g.assets.loadLevelGroup(func(err error) {
		if err != nil {
			g.levelErr.Set(err)
			return
		}
		if err := g.resetLevel(); err != nil {
			g.levelErr.Set(err)
			return
		}
		g.levelLoaded.Set(true)
		if g.pendingPlay {
			g.pendingPlay = false
			g.startGame()
		}
	})

	g.loop.Run(cx)

	return g, nil
}

func (g *caveRun) loadAudio(_ *core.Scope) {
	loadFX := func(src audio.Source, dest **sound.Effect) {
		g.assets.mgr.LoadSound(src, func(s *audio.Sound, err error) {
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
	g.syncLevelAccessibility()
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
	// Reset also releases the touch controls, which are bound to the actions.
	g.actions.Reset()
	// Play starts, resumes or pauses here, and the controls are drawn only during play, so they return at full opacity rather than faded by the time spent on a menu.
	g.touch.Wake()
}

func (g *caveRun) startGame() {
	if !g.levelLoaded.GetUntracked() {
		g.pendingPlay = true
		return
	}
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

// bindControls binds Cave Run's actions as game.yaml's input map declares them, and to the keyboard and game controllers, which game.yaml cannot bind. Touch drives the same actions through the on-screen controls in controls.touch.yaml, the game's one touch and pointer path.
func bindControls(a *input.Actions) {
	gameProject.BindInput(a)
	a.Bind(actionLeft, append(a.Bindings(actionLeft),
		input.Key("a"), input.Key("left"),
		input.GamepadButton(gamepad.ButtonDPadLeft),
	)...)
	a.Bind(actionRight, append(a.Bindings(actionRight),
		input.Key("d"), input.Key("right"),
		input.GamepadButton(gamepad.ButtonDPadRight),
	)...)
	a.Bind(actionJump, append(a.Bindings(actionJump),
		input.Key("space"), input.Key("w"), input.Key("up"), input.Key("z"),
		input.GamepadButton(gamepad.ButtonSouth),
	)...)
	a.BindGamepadAxis(actionMove, gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: stickDeadZone, Axial: true})

	a.Bind(actionConfirm, input.Key("enter"), input.GamepadButton(gamepad.ButtonStart))
	a.Bind(actionPause, input.Key("escape"), input.Key("p"), input.GamepadButton(gamepad.ButtonStart))
	a.Bind(actionRestart, input.Key("r"), input.GamepadButton(gamepad.ButtonBack))
	a.Bind(actionSwarm, input.Key("f"), input.GamepadButton(gamepad.ButtonNorth))
	a.Bind(actionSettings, input.GamepadButton(gamepad.ButtonWest))
}

// handleMenuActions acts on the step's menu actions: start from the title, pause, restart, toggle the firefly swarm, open settings, and play again from an end screen.
func (g *caveRun) handleMenuActions() {
	a := g.actions
	switch g.phase.Get() {
	case phaseTitle:
		switch {
		case a.JustPressed(actionSettings):
			g.openSettings()
		case a.JustPressed(actionConfirm) || a.JustPressed(actionJump):
			g.startGame()
		case a.JustPressed(actionSwarm):
			g.toggleSwarm()
		}
	case phasePlaying:
		switch {
		case g.paused.Get() && a.JustPressed(actionSettings):
			g.openSettings()
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

// sampleInput reads the step's actions, from the keyboard, controllers and touch controls alike, into the explorer's input.
func (g *caveRun) sampleInput() stepInput {
	a := g.actions
	return stepInput{
		moveAxis:     float32(a.Axis(actionMove)),
		jumpPressed:  a.JustPressed(actionJump),
		jumpHeld:     a.Pressed(actionJump),
		jumpReleased: a.JustReleased(actionJump),
	}
}

// update runs one fixed step: it samples input once, acts on the menu actions, and advances the current scene.
func (g *caveRun) update(dt time.Duration) {
	g.actions.Step()
	g.touch.Update(dt)
	if g.settingsOpen.Get() {
		if g.settingsMenu != nil && g.settingsMenu.StepGamepad(g.actions.Gamepads()) {
			g.bumpSettingsTick()
		}
		return
	}
	g.handleMenuActions()
	g.scenes.Update(dt)
}

func (g *caveRun) onLoopDraw(alpha float64) {
	g.alpha = alpha
	g.frameTick.Update(func(n int) int { return n + 1 })
}

// effectiveViewport returns the viewport used to render and hit-test a view of size. On touch-first devices (phones or coarse pointers) or whenever whole-integer scaling would drop below 2x (such as a landscape phone browser whose height is under 360 pt), it relaxes IntegerScale to fractional ModeFit so the playfield fills the view height instead of shrinking to 1x; drawCanvas disables image smoothing so pixel art stays crisp.
func (g *caveRun) effectiveViewport(size core.Size) screen.Viewport {
	vp := g.viewport
	if !vp.IntegerScale || vp.Width <= 0 || vp.Height <= 0 || size.Width <= 0 || size.Height <= 0 {
		return vp
	}
	touchFirst := g.env != nil && (g.env.Idiom() == core.IdiomPhone || g.env.Pointer() == core.PointerCoarse)
	sx := float64(size.Width) / float64(vp.Width)
	sy := float64(size.Height) / float64(vp.Height)
	if touchFirst || min(sx, sy) < 2 {
		vp.IntegerScale = false
	}
	return vp
}

func (g *caveRun) drawCanvas(d *core.Drawing, size core.Size) {
	// Subscribe canvas redraws to the fixed-step game loop frames.
	_ = g.frameTick.Get()
	vp := g.effectiveViewport(size)
	vp.Draw(d, size, func(d *core.Drawing, vsize core.Size) {
		// Nearest-neighbor sampling keeps pixel-art tiles, sprites and bitmap text sharp at both integer and fractional scales, and avoids atlas edge bleeding.
		d.SetImageSmoothing(false)
		// Clear background to deep cave indigo.
		d.SetFillColor(core.RGB(0x131628)).FillRect(0, 0, float32(vsize.Width), float32(vsize.Height))
		g.scenes.Draw(d, vsize, g.alpha)
	})
	// The touch controls sit in the view's points, over the letterbox, and show only while the game is played.
	g.touch.Resize(size, g.env.SafeArea())
	if g.phase.Get() == phasePlaying && !g.paused.Get() {
		g.touch.Draw(d)
	}
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
			stepDT := dt
			if speed := g.a11y.GameSpeed; speed > 0 && speed != 1.0 {
				stepDT = time.Duration(float64(dt) * speed)
			}
			g.level.update(stepDT, g.sampleInput())
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
