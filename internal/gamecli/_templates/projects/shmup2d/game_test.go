package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// stageLimit bounds how long a playthrough may take before the test gives up on it.
const stageLimit = 3 * time.Minute

// startGame starts the game on its title screen and returns the app and the game.
func startGame(t *testing.T) (*nsgotest.App, *shooter) {
	t.Helper()
	var g *shooter
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return root(cx, func(s *shooter) { g = s })
	})
	if g == nil {
		t.Fatal("the game did not start")
	}
	app.Find("id:title-menu")
	return app, g
}

// play advances the app one frame at a time until the run ends or stageLimit passes, calling each before every frame, and fails unless the end screen shows want.
func play(t *testing.T, app *nsgotest.App, g *shooter, want string, each func()) {
	t.Helper()
	for start := g.run.elapsed; g.phase.GetUntracked() != phaseEnded; {
		if g.run.elapsed-start > stageLimit {
			t.Fatalf("the run had not ended after %v: outcome %d, lives %d", stageLimit, g.run.outcome, g.run.ship.lives)
		}
		each()
		app.Advance(step)
	}
	app.Advance(2 * fadeTime)
	if got := app.Find("id:result").Text; got != want {
		t.Fatalf("the end screen says %q, want %q", got, want)
	}
}

// pressPad presses and releases button b on controller 1 across a few frames.
func pressPad(app *nsgotest.App, b gamepad.Button) {
	app.SetGamepadButton(1, b, 1)
	app.Advance(2 * step)
	app.SetGamepadButton(1, b, 0)
	app.Advance(2 * step)
}

// TestGamepadPlaythroughClearsTheStage plays the whole stage with a controller, the pilot steering with the left stick, firing with South, focusing with the left shoulder and bombing with East, until the boss falls and the end screen shows the win.
func TestGamepadPlaythroughClearsTheStage(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	pressPad(app, gamepad.ButtonStart)
	if g.phase.GetUntracked() != phasePlaying {
		t.Fatal("Start on the title screen did not start a run")
	}
	app.Advance(2 * fadeTime)

	var p pilot
	bombed, sawBoss := false, false
	level := func(on bool) float32 {
		if on {
			return 1
		}
		return 0
	}
	play(t, app, g, "Stage clear", func() {
		in := p.decide(g.run)
		app.SetGamepadAxis(1, gamepad.AxisLeftX, in.MoveX)
		app.SetGamepadAxis(1, gamepad.AxisLeftY, in.MoveY)
		app.SetGamepadButton(1, gamepad.ButtonSouth, level(in.Fire))
		app.SetGamepadButton(1, gamepad.ButtonLB, level(in.Focus))
		// The East button goes down for one frame and up for the next, as a press does.
		app.SetGamepadButton(1, gamepad.ButtonEast, level(in.Bomb && !bombed))
		bombed = in.Bomb && !bombed
		if b := g.run.boss; b != nil && b.entered && !b.dead && !sawBoss {
			sawBoss = app.Exists("id:boss-health") && app.Exists("text:"+b.def.Name)
		}
	})
	if !sawBoss {
		t.Error("the HUD did not show the boss's name and health while it fought")
	}
	if g.run.outcome != cleared || g.score.GetUntracked() < g.data.stage.Boss.Score {
		t.Fatalf("outcome %d with score %d, want the stage cleared and the boss scored", g.run.outcome, g.score.GetUntracked())
	}
	if !app.Exists("text:Score " + strconv.Itoa(g.run.score)) {
		t.Fatalf("the end screen does not show the score %d", g.run.score)
	}
}

// TestKeyboardPlaythroughEndsInGameOver starts a run with Return and never touches the ship, which the enemies shoot down, until the end screen shows the loss; Play again then starts a fresh run.
func TestKeyboardPlaythroughEndsInGameOver(t *testing.T) {
	app, g := startGame(t)
	app.Key("enter")
	app.Advance(2 * fadeTime)
	if g.phase.GetUntracked() != phasePlaying || !app.Exists("text:Score 0") {
		t.Fatal("Return on the title screen did not start a run")
	}
	play(t, app, g, "Game over", func() {})
	if g.run.outcome != lost || g.run.ship.lives != 0 {
		t.Fatalf("outcome %d with %d lives, want the run lost", g.run.outcome, g.run.ship.lives)
	}

	app.Press("id:retry")
	app.Advance(2 * fadeTime)
	if g.phase.GetUntracked() != phasePlaying || g.run.ship.lives != g.data.stage.Ship.Lives || g.run.elapsed > time.Second {
		t.Fatalf("Play again did not start a fresh run: phase %d, lives %d, elapsed %v", g.phase.GetUntracked(), g.run.ship.lives, g.run.elapsed)
	}
}

// startPlaying starts a run from the title screen's button and lets the fade finish.
func startPlaying(t *testing.T) (*nsgotest.App, *shooter) {
	t.Helper()
	app, g := startGame(t)
	app.Press("id:start")
	app.Advance(2 * fadeTime)
	if g.phase.GetUntracked() != phasePlaying {
		t.Fatal("the Start button did not start a run")
	}
	return app, g
}

func TestKeyboardFliesFiresBombsAndPauses(t *testing.T) {
	app, g := startPlaying(t)
	s := &g.run.ship

	x, y := s.x, s.y
	app.KeyDown("left")
	app.KeyDown("up")
	app.Advance(300 * time.Millisecond)
	app.KeyUp("left")
	app.KeyUp("up")
	if s.x >= x || s.y >= y {
		t.Fatalf("the arrow keys did not fly the ship up and left: (%.1f, %.1f) -> (%.1f, %.1f)", x, y, s.x, s.y)
	}

	// Shift slows the ship to its focus speed.
	x = s.x
	app.KeyCodeDown("KeyD")
	app.Advance(250 * time.Millisecond)
	fast := s.x - x
	x = s.x
	app.KeyDown("shift")
	app.Advance(250 * time.Millisecond)
	app.KeyUp("shift")
	app.KeyCodeUp("KeyD")
	if slow := s.x - x; slow <= 0 || slow >= fast*0.6 {
		t.Fatalf("D moved the ship %.1f, and %.1f with Shift held; want Shift to slow it", fast, slow)
	}

	if g.run.shipShots.Len() != 0 {
		t.Fatal("the ship fired with no fire key held")
	}
	app.KeyDown("z")
	app.Advance(200 * time.Millisecond)
	if g.run.shipShots.Len() == 0 {
		t.Fatal("holding Z did not fire")
	}
	app.KeyUp("z")
	app.Advance(2 * step)
	shots := g.run.weapon.Shots()
	app.Advance(300 * time.Millisecond)
	if g.run.weapon.Shots() != shots {
		t.Fatal("the ship kept firing after Z came up")
	}

	bombs := s.bombs
	app.Key("x")
	app.Advance(2 * step)
	if s.bombs != bombs-1 || g.run.bombFlash == 0 {
		t.Fatalf("X did not set off a bomb: bombs %d -> %d", bombs, s.bombs)
	}
	if !app.Exists("text:Lives 3  Bombs " + strconv.Itoa(bombs-1)) {
		t.Fatal("the HUD does not show the bomb spent")
	}

	app.Key("escape")
	app.Advance(2 * step)
	if !app.Exists("id:pause-menu") {
		t.Fatal("Escape did not pause")
	}
	elapsed := g.run.elapsed
	app.Advance(500 * time.Millisecond)
	if g.run.elapsed != elapsed {
		t.Fatal("the run went on while paused")
	}
	app.Key("escape")
	app.Advance(2 * step)
	if app.Exists("id:pause-menu") || g.run.elapsed == elapsed {
		t.Fatal("Escape did not resume")
	}
}

// gameToView returns where playfield point (x, y) is in the game view.
func gameToView(g *shooter, x, y float32) core.Point {
	return viewport.GameToView(g.viewSize(), core.Point{X: core.Points(x), Y: core.Points(y)})
}

func TestTouchDragsFiresAndBombs(t *testing.T) {
	app, g := startPlaying(t)
	s := &g.run.ship

	// A drag that starts anywhere moves the ship as far as the finger goes, without jumping it to the finger.
	x, y := s.x, s.y
	finger := app.Touch("id:game-view", gameToView(g, 100, 300))
	for i := 1; i <= 10; i++ {
		finger.Move(gameToView(g, 100+float32(i)*4, 300-float32(i)*3))
		app.Advance(step)
	}
	app.Advance(100 * time.Millisecond)
	if dx, dy := s.x-x, s.y-y; abs(dx-40) > 1 || abs(dy+30) > 1 {
		t.Fatalf("dragging 40 right and 30 up moved the ship by (%.1f, %.1f)", dx, dy)
	}
	if g.run.shipShots.Len() == 0 {
		t.Fatal("the ship did not fire while the finger was down")
	}
	finger.Lift()
	app.Advance(2 * step)
	shots := g.run.weapon.Shots()
	app.Advance(500 * time.Millisecond)
	if g.run.weapon.Shots() != shots {
		t.Fatal("the ship kept firing after the finger lifted")
	}

	// A tap on the bomb button sets off a bomb and does not drag the ship.
	bombs := s.bombs
	x, y = s.x, s.y
	r := bombRegion
	tap := app.Touch("id:game-view", gameToView(g, float32(r.Origin.X+r.Size.Width/2), float32(r.Origin.Y+r.Size.Height/2)))
	app.Advance(2 * step)
	tap.Lift()
	app.Advance(2 * step)
	if s.bombs != bombs-1 {
		t.Fatalf("tapping the bomb button left %d bombs, want %d", s.bombs, bombs-1)
	}
	if s.x != x || s.y != y {
		t.Fatal("tapping the bomb button moved the ship")
	}
	if !g.sawTouch {
		t.Fatal("the game did not notice the touch screen, so it hides the bomb button")
	}

	// A finger that slides onto the bomb button while it drags the ship does not set off a bomb.
	bombs = s.bombs
	slide := app.Touch("id:game-view", gameToView(g, 180, 444))
	for i := 1; i <= 10; i++ {
		slide.Move(gameToView(g, 180+float32(i)*5, 444))
		app.Advance(step)
	}
	app.Advance(4 * step)
	slide.Lift()
	app.Advance(2 * step)
	if s.bombs != bombs {
		t.Fatalf("sliding a dragging finger over the bomb button left %d bombs, want %d", s.bombs, bombs)
	}

	// The HUD's pause button pauses, and Resume resumes.
	app.Press("id:pause")
	if !app.Exists("id:pause-menu") {
		t.Fatal("the pause button did not pause")
	}
	app.Press("id:resume")
	if app.Exists("id:pause-menu") {
		t.Fatal("Resume did not resume")
	}
}

func TestMouseDragSteersAndFires(t *testing.T) {
	app, g := startPlaying(t)
	s := &g.run.ship

	x := s.x
	from := gameToView(g, 120, 300)
	app.HoverAt("id:game-view", from)
	app.MouseDownAt("id:game-view", from, core.MouseButtonLeft)
	for i := 1; i <= 10; i++ {
		app.HoverAt("id:game-view", gameToView(g, 120-float32(i)*5, 300))
		app.Advance(step)
	}
	app.Advance(100 * time.Millisecond)
	if dx := s.x - x; abs(dx+50) > 1 {
		t.Fatalf("dragging the mouse 50 left moved the ship %.1f", dx)
	}
	if g.run.shipShots.Len() == 0 {
		t.Fatal("the ship did not fire while the mouse button was down")
	}
	app.MouseUpAt("id:game-view", gameToView(g, 70, 300), core.MouseButtonLeft)
	app.Advance(2 * step)
	shots := g.run.weapon.Shots()
	app.Advance(500 * time.Millisecond)
	if g.run.weapon.Shots() != shots {
		t.Fatal("the ship kept firing after the mouse button came up")
	}
}

func TestRestartFromThePauseMenuResumesTheSound(t *testing.T) {
	app, g := startPlaying(t)
	app.Key("escape")
	app.Advance(2 * step)
	if !app.Exists("id:pause-menu") || !g.sounds.mixer.Paused() {
		t.Fatal("Escape did not pause the run and its sound")
	}
	app.Press("id:restart")
	app.Advance(2 * fadeTime)
	if g.phase.GetUntracked() != phasePlaying || g.paused.GetUntracked() || app.Exists("id:pause-menu") {
		t.Fatal("Restart did not start a new run")
	}
	if g.sounds.mixer.Paused() {
		t.Fatal("the new run after Restart has its sound still paused")
	}
}

func TestBackgroundPausesTheRun(t *testing.T) {
	app, g := startPlaying(t)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(2 * step)
	if !g.paused.GetUntracked() || !app.Exists("id:pause-menu") {
		t.Fatal("the run did not pause as the app went to the background")
	}
}

func TestGamepadPausesAndResumes(t *testing.T) {
	app, g := startPlaying(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	pressPad(app, gamepad.ButtonStart)
	if !app.Exists("id:pause-menu") {
		t.Fatal("Start did not pause")
	}
	pressPad(app, gamepad.ButtonStart)
	if app.Exists("id:pause-menu") {
		t.Fatal("Start did not resume")
	}

	// A held stick that the controller loses lets go of the ship.
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	app.Advance(200 * time.Millisecond)
	app.DisconnectGamepad(1)
	app.Advance(2 * step)
	x := g.run.ship.x
	app.Advance(200 * time.Millisecond)
	if g.run.ship.x != x {
		t.Fatal("the ship kept flying after its controller disconnected")
	}
}

func abs(v float32) float32 { return max(v, -v) }
