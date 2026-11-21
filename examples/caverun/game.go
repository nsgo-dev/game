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

	keyMoveHoldDuration = 180 * time.Millisecond
	keyJumpHoldDuration = 260 * time.Millisecond
	sceneFadeDuration   = 200 * time.Millisecond
)

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

	// Native touch-button & keyboard input state merged with input.Actions each fixed step.
	padLeft          bool
	padRight         bool
	padJump          bool
	padJumpLatched   bool
	keyLeftTimer     time.Duration
	keyRightTimer    time.Duration
	keyJumpTimer     time.Duration
	keyJumpLatched   bool
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
	}

	g.music = g.mixer.Music(soundGroupMusic)
	g.music.Loop = true
	g.loadAudio(cx)

	// Map view pointer coordinates into 320x180 virtual canvas coordinates.
	g.actions.MapPoint = func(p core.Point) (core.Point, bool) {
		return g.viewport.ViewToGame(g.currentViewSize(), p)
	}
	g.actions.Bind("left",
		input.TouchRegion(touchRegionLeft),
		input.MouseRegion(touchRegionLeft, core.MouseButtonLeft),
	)
	g.actions.Bind("right",
		input.TouchRegion(touchRegionRight),
		input.MouseRegion(touchRegionRight, core.MouseButtonLeft),
	)
	g.actions.Bind("jump",
		input.TouchRegion(touchRegionJump),
		input.MouseRegion(touchRegionJump, core.MouseButtonLeft),
	)
	g.actions.BindAxis("move", "left", "right")

	// Build initial level for title backdrop.
	if err := g.resetLevel(); err != nil {
		return nil, err
	}
	g.scenes.Switch(g.titleController(), nil)

	// Global keyboard shortcuts so Escape/P/R work even when focus is on a HUD button.
	cx.OnKey("escape", func() {
		if g.phase.Get() == phasePlaying {
			g.togglePause()
		}
	})
	cx.OnKey("p", func() {
		if g.phase.Get() == phasePlaying {
			g.togglePause()
		}
	})
	cx.OnKey("r", func() {
		if g.phase.Get() != phaseTitle {
			g.restartGame()
		}
	})

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
}

func (g *caveRun) clearHeldInputs() {
	g.actions.Reset()
	g.padLeft = false
	g.padRight = false
	g.padJump = false
	g.padJumpLatched = false
	g.keyLeftTimer = 0
	g.keyRightTimer = 0
	g.keyJumpTimer = 0
	g.keyJumpLatched = false
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

func (g *caveRun) handleKeyDown(ev core.KeyEvent) bool {
	switch ev.Key {
	case "escape", "p":
		if g.phase.Get() == phasePlaying {
			g.togglePause()
			return true
		}
	case "r":
		if g.phase.Get() != phaseTitle {
			g.restartGame()
			return true
		}
	case "enter":
		switch g.phase.Get() {
		case phaseTitle:
			g.startGame()
			return true
		case phaseVictory, phaseGameOver:
			g.restartGame()
			return true
		}
	case "left", "a":
		if g.phase.Get() == phasePlaying && !g.paused.Get() {
			g.keyLeftTimer = keyMoveHoldDuration
			g.keyRightTimer = 0
			return true
		}
	case "right", "d":
		if g.phase.Get() == phasePlaying && !g.paused.Get() {
			g.keyRightTimer = keyMoveHoldDuration
			g.keyLeftTimer = 0
			return true
		}
	case "space", "up", "w", "z":
		switch g.phase.Get() {
		case phaseTitle:
			g.startGame()
			return true
		case phasePlaying:
			if !g.paused.Get() {
				g.keyJumpTimer = keyJumpHoldDuration
				if !ev.Repeat {
					g.keyJumpLatched = true
				}
				return true
			}
		}
	}
	return false
}

func (g *caveRun) sampleInput(dt time.Duration) stepInput {
	g.actions.Step()

	// Decay keyboard hold timers.
	if g.keyLeftTimer > 0 {
		g.keyLeftTimer -= dt
		if g.keyLeftTimer < 0 {
			g.keyLeftTimer = 0
		}
	}
	if g.keyRightTimer > 0 {
		g.keyRightTimer -= dt
		if g.keyRightTimer < 0 {
			g.keyRightTimer = 0
		}
	}
	if g.keyJumpTimer > 0 {
		g.keyJumpTimer -= dt
		if g.keyJumpTimer < 0 {
			g.keyJumpTimer = 0
		}
	}

	// Merge touch/mouse Actions, HUD virtual pad, and keyboard holds.
	var move float32
	axis := float32(g.actions.Axis("move"))
	if axis != 0 {
		move = axis
	}
	leftHeld := g.padLeft || g.keyLeftTimer > 0
	rightHeld := g.padRight || g.keyRightTimer > 0
	if leftHeld && !rightHeld {
		move = -1
	} else if rightHeld && !leftHeld {
		move = 1
	}

	jumpHeld := g.actions.Pressed("jump") || g.padJump || g.keyJumpTimer > 0
	jumpPressed := g.actions.JustPressed("jump") || g.padJumpLatched || g.keyJumpLatched || (jumpHeld && !g.prevCombinedJump)
	jumpReleased := (!jumpHeld && g.prevCombinedJump) || g.actions.JustReleased("jump")

	g.padJumpLatched = false
	g.keyJumpLatched = false
	g.prevCombinedJump = jumpHeld

	return stepInput{
		moveAxis:     move,
		jumpPressed:  jumpPressed,
		jumpHeld:     jumpHeld,
		jumpReleased: jumpReleased,
	}
}

func (g *caveRun) update(dt time.Duration) {
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
			in := g.sampleInput(dt)
			g.level.update(dt, in)
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
