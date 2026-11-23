package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const testFrame = 16667 * time.Microsecond

func startCaveRun(t *testing.T) (*nsgotest.App, *caveRun) {
	t.Helper()
	var g *caveRun
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return rootWithHook(cx, func(c *caveRun) { g = c })
	}, nsgotest.Size(960, 540), nsgotest.ManualFrames())

	// Populate test audio durations so audio.LoadSound succeeds cleanly in headless mode.
	app.AudioDurations = map[string]time.Duration{
		"asset:sfx_jump.wav":   180 * time.Millisecond,
		"asset:sfx_gem.wav":    330 * time.Millisecond,
		"asset:sfx_hurt.wav":   260 * time.Millisecond,
		"asset:sfx_win.wav":    940 * time.Millisecond,
		"asset:music_cave.wav": 6400 * time.Millisecond,
	}
	app.Advance(3 * testFrame)

	if g == nil {
		t.Fatal("caveRun instance was not initialized")
	}
	return app, g
}

func startPlaying(t *testing.T, app *nsgotest.App, g *caveRun) {
	t.Helper()
	if !app.Exists("id:title-menu") {
		t.Fatal("expected title-menu before starting")
	}
	app.Press("id:start-btn")
	app.Advance(250 * time.Millisecond)
	if g.phase.Get() != phasePlaying {
		t.Fatalf("phase = %v, want phasePlaying", g.phase.Get())
	}
}

func TestPlayLevelStartToEnd(t *testing.T) {
	app, g := startCaveRun(t)

	// Title screen renders the cave backdrop into the Canvas.
	canvas := app.Find("id:game-canvas")
	if len(canvas.Ops) == 0 {
		t.Fatal("expected title screen canvas to record drawing operations")
	}

	startPlaying(t, app, g)

	// Verify initial HUD readouts and level entity counts.
	if got := len(g.level.gems); got != 8 {
		t.Fatalf("level gems = %d, want 8", got)
	}
	if got := len(g.level.slimes); got != 2 {
		t.Fatalf("level slimes = %d, want 2", got)
	}
	if got := app.Find("id:hud-hearts").Text; got != "♥♥♥" {
		t.Fatalf("initial hud-hearts = %q, want %q", got, "♥♥♥")
	}

	// Drive the explorer across all three chambers of level1.tmj using real HUD button inputs:
	//   Chamber 1: spawn (x=32) -> bridge (gem 1) -> step ledge (x=160)
	//   Chamber 2: Lava Pit 1 bridge (gem 2) -> central island (over slime_1) -> Lava Pit 2 bridge (gem 5)
	//   Chamber 3: slime_2 floor (gem 6) -> row 9 ascending bridge (gem 8) -> row 7 exit portal (x=592, y=96)
	const (
		stateMoveRight = iota
		stateClimbToPortal
	)
	mode := stateMoveRight
	rightHeld := false
	leftHeld := false
	jumpHeld := false
	jumpCooldown := 0

	setMove := func(dir int) {
		if dir > 0 {
			if leftHeld {
				if app.Exists("id:ctrl-left") {
					app.PressUp("id:ctrl-left")
				}
				leftHeld = false
			}
			if !rightHeld && app.Exists("id:ctrl-right") {
				app.PressDown("id:ctrl-right")
				rightHeld = true
			}
		} else if dir < 0 {
			if rightHeld {
				if app.Exists("id:ctrl-right") {
					app.PressUp("id:ctrl-right")
				}
				rightHeld = false
			}
			if !leftHeld && app.Exists("id:ctrl-left") {
				app.PressDown("id:ctrl-left")
				leftHeld = true
			}
		} else {
			if rightHeld {
				if app.Exists("id:ctrl-right") {
					app.PressUp("id:ctrl-right")
				}
				rightHeld = false
			}
			if leftHeld {
				if app.Exists("id:ctrl-left") {
					app.PressUp("id:ctrl-left")
				}
				leftHeld = false
			}
		}
	}

	releaseJump := func() {
		if jumpHeld {
			if app.Exists("id:ctrl-jump") {
				app.PressUp("id:ctrl-jump")
			}
			jumpHeld = false
		}
	}
	pressJump := func() {
		if !jumpHeld && app.Exists("id:ctrl-jump") {
			app.PressDown("id:ctrl-jump")
			jumpHeld = true
			jumpCooldown = 14
		}
	}

	defer func() {
		setMove(0)
		releaseJump()
	}()

	const maxFrames = 900 // 15 seconds of simulated 60 Hz gameplay
	for frame := 0; frame < maxFrames && g.phase.Get() == phasePlaying; frame++ {
		pb := g.level.playerBody.Bounds()
		px := pb.X + pb.W/2
		py := pb.Bottom()
		grounded := g.level.wasGrounded

		// Release jump once descending so subsequent jumps trigger cleanly on landing.
		if jumpHeld && ((!grounded && g.level.playerBody.VY >= 0) || jumpCooldown <= 0) {
			releaseJump()
		}
		if jumpCooldown > 0 {
			jumpCooldown--
		}

		// Determine if a slime is close ahead on our level so we hop cleanly over it.
		slimeAhead := false
		for i := range g.level.slimes {
			sx := g.level.slimes[i].x + 8
			sy := g.level.slimes[i].y + 16
			if math.Abs(float64(py-sy)) < 20 && sx > px-4 && sx-px < 34 {
				slimeAhead = true
			}
		}

		switch mode {
		case stateMoveRight:
			setMove(1)
			wantJump := false

			// 1. Jump onto Chamber 1 bridge (cols 5..8, x=80..144, y=160) to collect gem 1.
			if grounded && px >= 54 && px <= 76 && py >= 188 {
				wantJump = true
			}
			// 2. Jump onto row 11 step ledge (cols 10..11, x=160..192, y=176).
			if grounded && px >= 136 && px <= 156 && py >= 188 {
				wantJump = true
			}
			// 3. Jump from right edge of Chamber 1 step (x=180..192) onto Lava Pit 1 bridge (x=192..224, y=160) / central island.
			if grounded && px >= 180 && px <= 192 && py >= 170 {
				wantJump = true
			}
			// 4. Jump from Lava Pit 1 bridge (x=212..224, y=160) cleanly onto central island (x=256..368).
			if grounded && px >= 210 && px <= 226 && py <= 165 {
				wantJump = true
			}
			// 5. Jump over patrolling slimes when on the same floor.
			if grounded && slimeAhead {
				wantJump = true
			}
			// 6. Jump from right edge of central island (x=354..368, y=176) onto Lava Pit 2 bridge (x=384..416, y=160).
			if grounded && px >= 352 && px <= 368 && py >= 170 {
				wantJump = true
			}
			// 7. Jump from Lava Pit 2 bridge (x=402..416, y=160) onto Chamber 3 floor (x=432..544).
			if grounded && px >= 402 && px <= 418 && py <= 165 {
				wantJump = true
			}
			// 8. Approaching Chamber 3 ascending bridge (cols 32..34, x=512..560, y=144).
			if px >= 496 {
				mode = stateClimbToPortal
			}

			if wantJump {
				pressJump()
			}

		case stateClimbToPortal:
			// Ascend from Chamber 3 floor (y=176) -> row 9 bridge (cols 32..34, x=512..560, y=144) -> row 7 portal ledge (cols 35..38, x=560..624, y=112).
			if py > 152 {
				// Position near x=505..520 and jump onto the row 9 bridge.
				if px < 508 {
					setMove(1)
				} else if px > 544 {
					setMove(-1)
				} else {
					setMove(1)
				}
				if grounded && px >= 494 && px <= 548 {
					pressJump()
				}
			} else if py > 120 {
				// On row 9 bridge (y=144): move right and jump onto row 7 portal ledge (y=112).
				setMove(1)
				if grounded && px >= 530 && px <= 562 {
					pressJump()
				}
			} else {
				// On row 7 portal ledge (y=112): run straight into exit portal at x=592!
				setMove(1)
			}
		}

		app.Advance(testFrame)
	}

	setMove(0)
	releaseJump()

	// Allow victory fade transition to settle.
	app.Advance(250 * time.Millisecond)

	if g.phase.Get() != phaseVictory {
		pb := g.level.playerBody.Bounds()
		t.Fatalf("did not reach victory within %d frames: phase=%v pos=(%.1f, %.1f) hearts=%d gems=%d score=%d",
			maxFrames, g.phase.Get(), pb.X, pb.Y, g.level.hearts, g.level.gemsCollected, g.level.score)
	}

	if g.level.gemsCollected < 2 {
		t.Fatalf("gemsCollected = %d, want at least 2 collected along the route", g.level.gemsCollected)
	}
	if g.level.score <= 0 {
		t.Fatalf("score = %d, want > 0", g.level.score)
	}

	// Verify native Victory overlay is displayed with score summary.
	if !app.Exists("id:victory-overlay") {
		t.Fatal("expected id:victory-overlay to be visible on victory")
	}
	finalText := app.Find("id:final-score").Text
	if !strings.Contains(finalText, "Score:") {
		t.Fatalf("unexpected final-score text: %q", finalText)
	}

	// Verify canvas recorded tilemap/sprite draw ops throughout gameplay.
	canvas = app.Find("id:game-canvas")
	if len(canvas.Ops) < 10 {
		t.Fatalf("expected rich canvas drawing ops, got %d", len(canvas.Ops))
	}

	// Pressing Play Again resets cleanly back into active gameplay.
	app.Press("id:replay-btn")
	app.Advance(250 * time.Millisecond)
	if g.phase.Get() != phasePlaying {
		t.Fatalf("after replay-btn phase = %v, want phasePlaying", g.phase.Get())
	}
	if g.level.score != 0 || g.level.gemsCollected != 0 || g.level.hearts != maxHearts {
		t.Fatalf("after replay: score=%d gems=%d hearts=%d, want 0, 0, %d",
			g.level.score, g.level.gemsCollected, g.level.hearts, maxHearts)
	}
}

func TestHUDAndPauseMenu(t *testing.T) {
	app, g := startCaveRun(t)
	startPlaying(t, app, g)

	// Drive movement via input.Actions touch region on id:game-view.
	startX := g.level.playerBody.Bounds().X
	rightViewPt := g.viewport.GameToView(g.currentViewSize(), core.Point{X: 108, Y: 148})
	finger := app.Touch("id:game-view", rightViewPt)
	app.Advance(350 * time.Millisecond)
	finger.Lift()
	app.Advance(2 * testFrame)

	movedX := g.level.playerBody.Bounds().X
	if movedX <= startX+15 {
		t.Fatalf("expected TouchRegion right input to move player: startX=%.1f movedX=%.1f", startX, movedX)
	}

	// Press Pause button -> pause menu appears and gameplay stops stepping.
	app.Press("id:pause-btn")
	app.Settle()
	if !app.Exists("id:pause-menu") {
		t.Fatal("expected id:pause-menu after pressing pause-btn")
	}
	if !g.paused.Get() {
		t.Fatal("expected paused signal to be true")
	}

	pausedX := g.level.playerBody.Bounds().X
	pausedSteps := g.level.steps
	app.Advance(500 * time.Millisecond)
	if g.level.steps != pausedSteps || g.level.playerBody.Bounds().X != pausedX {
		t.Fatalf("simulation advanced while paused: steps %d->%d, x %.1f->%.1f",
			pausedSteps, g.level.steps, pausedX, g.level.playerBody.Bounds().X)
	}

	// Toggle Mute via pause menu button and verify sound.Mixer master volume updates.
	if g.mixer.Master() != 1 {
		t.Fatalf("initial mixer.Master() = %v, want 1", g.mixer.Master())
	}
	app.Press("id:mute-btn")
	app.Settle()
	if g.mixer.Master() != 0 {
		t.Fatalf("after mute-btn mixer.Master() = %v, want 0", g.mixer.Master())
	}
	app.Press("id:mute-btn")
	app.Settle()
	if g.mixer.Master() != 1 {
		t.Fatalf("after second mute-btn mixer.Master() = %v, want 1", g.mixer.Master())
	}

	// Verify SFX and music mixer groups can be tuned independently.
	app.Post(func() {
		_ = g.mixer.SetVolume(soundGroupSFX, 0.5)
		_ = g.mixer.SetVolume(soundGroupMusic, 0.25)
	})
	app.Settle()
	if got := g.mixer.Volume(soundGroupSFX); got != 0.5 {
		t.Fatalf("sfx volume = %v, want 0.5", got)
	}
	if got := g.mixer.Volume(soundGroupMusic); got != 0.25 {
		t.Fatalf("music volume = %v, want 0.25", got)
	}

	// Resume via Resume button -> pause menu closes and simulation ticks again.
	app.Press("id:resume-btn")
	app.Settle()
	if app.Exists("id:pause-menu") {
		t.Fatal("expected pause-menu to close after resume-btn")
	}
	app.Advance(100 * time.Millisecond)
	if g.level.steps <= pausedSteps {
		t.Fatalf("expected simulation steps to resume after unpause: %d <= %d", g.level.steps, pausedSteps)
	}

	// Keyboard pause & resume via Escape and P.
	app.Key("escape")
	app.Settle()
	if !app.Exists("id:pause-menu") {
		t.Fatal("expected Escape key to open pause-menu")
	}
	app.Key("p")
	app.Settle()
	if app.Exists("id:pause-menu") {
		t.Fatal("expected P key to resume from pause-menu")
	}

	// Open pause menu and press Restart -> resets player position & score.
	app.Press("id:pause-btn")
	app.Settle()
	app.Press("id:restart-btn")
	app.Advance(250 * time.Millisecond)
	if app.Exists("id:pause-menu") {
		t.Fatal("expected pause-menu to close after restart-btn")
	}
	resetX := g.level.playerBody.Bounds().X
	wantSpawnX := g.level.spawnX - playerSpriteOX
	if math.Abs(float64(resetX-wantSpawnX)) > 2 {
		t.Fatalf("after restart player X = %.1f, want near spawn %.1f", resetX, wantSpawnX)
	}
	if g.level.score != 0 || g.level.hearts != maxHearts {
		t.Fatalf("after restart: score=%d hearts=%d, want 0, %d", g.level.score, g.level.hearts, maxHearts)
	}
}

func TestHazardDamageAndGameOver(t *testing.T) {
	t.Run("lava_damage_shake_and_gameover", func(t *testing.T) {
		app, g := startCaveRun(t)
		startPlaying(t, app, g)

		// Walk right over the Chamber 1 step ledge and straight into Lava Pit 1 (cols 12..15).
		// Hop once onto the row 11 ledge (cols 10..11), then walk off into lava!
		app.PressDown("id:ctrl-right")
		var sawShake bool
		jumpedStep := false

		for i := 0; i < 600 && g.phase.Get() == phasePlaying; i++ {
			pb := g.level.playerBody.Bounds()
			if !jumpedStep && pb.X >= 132 && g.level.wasGrounded {
				app.PressDown("id:ctrl-jump")
				app.Advance(3 * testFrame)
				app.PressUp("id:ctrl-jump")
				jumpedStep = true
				continue
			}
			// After getting hurt on lava, pause briefly to let invulnerability wear off before walking back in.
			if g.level.invulnTimer > 0 {
				off := g.level.cam.ShakeOffset()
				if off.X != 0 || off.Y != 0 {
					sawShake = true
				}
				if app.Exists("id:ctrl-right") {
					app.PressUp("id:ctrl-right")
				}
				app.Advance(g.level.invulnTimer + 2*testFrame)
				if g.phase.Get() == phasePlaying && app.Exists("id:ctrl-right") {
					app.PressDown("id:ctrl-right")
				}
				continue
			}
			app.Advance(testFrame)
		}
		if app.Exists("id:ctrl-right") {
			app.PressUp("id:ctrl-right")
		}
		app.Advance(250 * time.Millisecond)

		if !sawShake {
			t.Fatal("expected camera shake offset on hazard damage")
		}
		if g.level.hearts != 0 {
			t.Fatalf("hearts = %d, want 0 after repeated lava hazards", g.level.hearts)
		}
		if g.phase.Get() != phaseGameOver {
			t.Fatalf("phase = %v, want phaseGameOver", g.phase.Get())
		}
		if !app.Exists("id:gameover-overlay") {
			t.Fatal("expected id:gameover-overlay on game over")
		}

		// Play Again recovers cleanly from Game Over.
		app.Press("id:replay-btn")
		app.Advance(250 * time.Millisecond)
		if g.phase.Get() != phasePlaying || g.level.hearts != maxHearts {
			t.Fatalf("after replay: phase=%v hearts=%d, want phasePlaying and %d",
				g.phase.Get(), g.level.hearts, maxHearts)
		}
	})

	t.Run("slime_contact_damage_and_knockback", func(t *testing.T) {
		app, g := startCaveRun(t)
		startPlaying(t, app, g)

		// Place explorer on Chamber 2 island directly in slime_1's patrol path to verify slime collision deterministically.
		app.Post(func() {
			s := g.level.slimes[0]
			g.level.playerBody.Shape = g.level.playerBody.Shape.Translated(
				(s.x+16)-g.level.playerBody.Bounds().X,
				(s.y+2)-g.level.playerBody.Bounds().Y,
			)
		})
		app.Settle()

		hitDetected := false
		shakeDetected := false
		for i := 0; i < 120; i++ {
			app.Advance(testFrame)
			if g.level.hearts < maxHearts {
				hitDetected = true
				off := g.level.cam.ShakeOffset()
				if off.X != 0 || off.Y != 0 {
					shakeDetected = true
				}
				break
			}
		}
		if !hitDetected {
			t.Fatalf("expected slime contact to reduce hearts (hearts=%d)", g.level.hearts)
		}
		if !shakeDetected {
			t.Fatal("expected camera shake when damaged by slime")
		}
		if got := app.Find("id:hud-hearts").Text; got != "♥♥♡" {
			t.Fatalf("hud-hearts after slime hit = %q, want %q", got, "♥♥♡")
		}
	})
}

func TestLocksLandscape(t *testing.T) {
	app, g := startCaveRun(t)
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation on the title screen = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
	startPlaying(t, app, g)
	app.Advance(10 * testFrame)
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation while playing = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}
