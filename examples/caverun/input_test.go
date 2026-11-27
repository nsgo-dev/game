package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestKeyboardHoldsMoveUntilRelease(t *testing.T) {
	app, g := startCaveRun(t)
	startWithEnter(t, app, g)

	// A held key keeps the explorer running for as long as it is down, well past one key-repeat interval.
	startX := g.level.playerBody.Bounds().X
	app.KeyDown("d")
	app.Advance(time.Second)
	held := g.level.playerBody.Bounds().X
	if held-startX < 60 {
		t.Fatalf("holding D for 1s moved the explorer %.1f px, want at least 60", held-startX)
	}

	// Releasing it stops the explorer.
	app.KeyUp("d")
	app.Advance(500 * time.Millisecond)
	if vx := g.level.playerBody.VX; vx != 0 {
		t.Fatalf("explorer VX = %v after D came up, want 0", vx)
	}
	stopped := g.level.playerBody.Bounds().X
	app.Advance(250 * time.Millisecond)
	if x := g.level.playerBody.Bounds().X; x != stopped {
		t.Fatalf("explorer kept moving after D came up: %.1f -> %.1f", stopped, x)
	}

	// The left arrow runs back the other way.
	app.KeyDown("left")
	app.Advance(300 * time.Millisecond)
	app.KeyUp("left")
	if !g.level.facingLeft || g.level.playerBody.Bounds().X >= stopped {
		t.Fatalf("left arrow did not move the explorer left: x %.1f -> %.1f, facingLeft=%v", stopped, g.level.playerBody.Bounds().X, g.level.facingLeft)
	}

	// Space jumps, and holding it jumps higher than tapping it.
	app.Advance(500 * time.Millisecond)
	tapPeak := jumpPeak(app, g, func() { app.Key("space") })
	holdPeak := jumpPeak(app, g, func() { app.KeyDown("space") })
	app.KeyUp("space")
	if tapPeak <= 0 || holdPeak <= tapPeak {
		t.Fatalf("jump heights: tap %.1f px, hold %.1f px; want a held jump higher than a tapped one", tapPeak, holdPeak)
	}
}

// jumpPeak starts a jump with press and returns how high above its starting height the explorer rises before it lands again.
func jumpPeak(app *nsgotest.App, g *caveRun, press func()) float32 {
	startY := g.level.playerBody.Bounds().Y
	minY := startY
	press()
	for range 90 {
		app.Advance(testFrame)
		if y := g.level.playerBody.Bounds().Y; y < minY {
			minY = y
		}
		if g.level.wasGrounded && g.level.playerBody.Bounds().Y >= startY && minY < startY {
			break
		}
	}
	app.Advance(200 * time.Millisecond)
	return startY - minY
}

func TestKeyboardPauseRestartAndSwarm(t *testing.T) {
	app, g := startCaveRun(t)

	// F on the title screen, where its hint shows, turns the swarm on for the run it starts; F again during play turns it off.
	app.Key("f")
	app.Advance(2 * testFrame)
	if !g.swarm.Get() || g.level.swarm == nil {
		t.Fatal("F on the title screen did not turn the firefly swarm on")
	}
	startWithEnter(t, app, g)
	if g.level.swarm == nil {
		t.Fatal("the run started from the title screen dropped the firefly swarm")
	}
	app.Key("f")
	app.Advance(2 * testFrame)
	if g.swarm.Get() || g.level.swarm != nil {
		t.Fatal("F during play did not turn the firefly swarm off")
	}

	app.KeyDown("d")
	app.Advance(300 * time.Millisecond)

	// Escape pauses while D is still held; the held key does not move the explorer once play resumes, until it is pressed again.
	app.Key("escape")
	app.Advance(2 * testFrame)
	if !g.paused.Get() || !app.Exists("id:pause-menu") {
		t.Fatal("Escape did not pause the game")
	}
	app.Key("escape")
	app.Advance(2 * testFrame)
	if g.paused.Get() {
		t.Fatal("Escape did not resume the game")
	}
	app.Advance(500 * time.Millisecond)
	x := g.level.playerBody.Bounds().X
	app.Advance(300 * time.Millisecond)
	if moved := g.level.playerBody.Bounds().X; moved != x {
		t.Fatalf("D held through the pause moved the explorer after resuming: %.1f -> %.1f", x, moved)
	}
	app.KeyUp("d")

	// F toggles the firefly swarm.
	app.Key("f")
	app.Advance(2 * testFrame)
	if !g.swarm.Get() || g.level.swarm == nil {
		t.Fatal("F did not turn the firefly swarm on")
	}
	if got, want := app.Find("id:hud-sprites").Text, "SPRITES 1012"; got != want {
		t.Fatalf("hud-sprites = %q, want %q", got, want)
	}
	app.Key("f")
	app.Advance(2 * testFrame)
	if g.swarm.Get() || g.level.swarm != nil || app.Exists("id:hud-sprites") {
		t.Fatal("F did not turn the firefly swarm off")
	}

	// R restarts the run from the spawn point.
	app.KeyDown("d")
	app.Advance(500 * time.Millisecond)
	app.KeyUp("d")
	app.Key("r")
	app.Advance(250 * time.Millisecond)
	if got, want := g.level.playerBody.Bounds().X, g.level.spawnX-playerSpriteOX; got != want {
		t.Fatalf("after R the explorer is at x=%.1f, want spawn x=%.1f", got, want)
	}
}

func TestGamepadPlaysAndPauses(t *testing.T) {
	app, g := startCaveRun(t)
	startWithGamepad(t, app, g)

	// The left stick moves the explorer in proportion to how far it is pushed, and not at all inside the dead zone.
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.2)
	x := g.level.playerBody.Bounds().X
	app.Advance(300 * time.Millisecond)
	if moved := g.level.playerBody.Bounds().X; moved != x {
		t.Fatalf("a stick push inside the dead zone moved the explorer: %.1f -> %.1f", x, moved)
	}
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	app.Advance(time.Second)
	full := g.level.playerBody.Bounds().X - x
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
	app.Advance(500 * time.Millisecond)
	if full < 60 {
		t.Fatalf("a full stick push for 1s moved the explorer %.1f px, want at least 60", full)
	}

	// The D-pad moves it too.
	x = g.level.playerBody.Bounds().X
	app.SetGamepadButton(1, gamepad.ButtonDPadLeft, 1)
	app.Advance(300 * time.Millisecond)
	app.SetGamepadButton(1, gamepad.ButtonDPadLeft, 0)
	if g.level.playerBody.Bounds().X >= x {
		t.Fatalf("D-pad left did not move the explorer left: %.1f -> %.1f", x, g.level.playerBody.Bounds().X)
	}
	app.Advance(500 * time.Millisecond)

	// South jumps.
	app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	app.Advance(2 * testFrame)
	if g.level.playerBody.VY >= 0 {
		t.Fatalf("South did not start a jump: VY=%v", g.level.playerBody.VY)
	}
	app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	app.Advance(time.Second)

	// Start pauses and resumes.
	pressPad(app, gamepad.ButtonStart)
	if !g.paused.Get() || !app.Exists("id:pause-menu") {
		t.Fatal("Start did not pause the game")
	}
	steps := g.level.steps
	app.Advance(300 * time.Millisecond)
	if g.level.steps != steps {
		t.Fatal("the level kept stepping while paused")
	}
	pressPad(app, gamepad.ButtonStart)
	if g.paused.Get() {
		t.Fatal("Start did not resume the game")
	}

	// North toggles the firefly swarm, and Back restarts with it still on.
	pressPad(app, gamepad.ButtonNorth)
	if g.level.swarm == nil {
		t.Fatal("North did not turn the firefly swarm on")
	}
	pressPad(app, gamepad.ButtonBack)
	app.Advance(250 * time.Millisecond)
	if g.level.steps > 20 || g.level.swarm == nil {
		t.Fatalf("Back did not restart with the swarm on: steps=%d swarm=%v", g.level.steps, g.level.swarm != nil)
	}

	// Disconnecting the controller mid-push releases what it held.
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	app.Advance(200 * time.Millisecond)
	app.DisconnectGamepad(1)
	app.Advance(500 * time.Millisecond)
	if vx := g.level.playerBody.VX; vx != 0 {
		t.Fatalf("explorer VX = %v after the controller disconnected, want 0", vx)
	}
}

// pressPad presses and releases button b on controller 1 across a few fixed steps.
func pressPad(app *nsgotest.App, b gamepad.Button) {
	app.SetGamepadButton(1, b, 1)
	app.Advance(2 * testFrame)
	app.SetGamepadButton(1, b, 0)
	app.Advance(2 * testFrame)
}

func TestTouchControlsHideOnDesktopUntilTouched(t *testing.T) {
	app, g := startCaveRun(t, nsgotest.Idiom(core.IdiomDesktop))
	startWithEnter(t, app, g)
	if app.Exists("id:touch-jump") {
		t.Fatal("the touch controls show on a desktop before any touch")
	}

	// The first touch shows them, and a touch on the jump button jumps at once.
	finger := app.Touch("id:game-view", centerOf(controlFrame(t, g, "jump")))
	app.Advance(2 * testFrame)
	if !app.Exists("id:touch-jump") || !app.Exists("id:touch-move") {
		t.Fatalf("a touch did not show the touch controls: visible=%v device=%v alpha=%v", g.touch.Visible(), g.touch.Tracker().LastDevice(), g.touch.Alpha())
	}
	if g.level.playerBody.VY >= 0 {
		t.Fatalf("a touch on the jump button did not start a jump: VY=%v", g.level.playerBody.VY)
	}
	finger.Lift()

	// Using the keyboard again hides them.
	app.KeyDown("d")
	app.Advance(2 * testFrame)
	app.KeyUp("d")
	app.Advance(testFrame)
	if app.Exists("id:touch-jump") {
		t.Fatal("the keyboard did not hide the touch controls")
	}
}

func TestTouchControlsStayInsideTheSafeArea(t *testing.T) {
	// A landscape iPhone's notch and home indicator.
	app, g := startCaveRun(t, nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21}}))
	startPlaying(t, app, g)
	move, jump := controlFrame(t, g, "move"), controlFrame(t, g, "jump")
	if move.Origin.X < 47 || jump.Origin.X+jump.Size.Width > 960-47 || move.Origin.Y+move.Size.Height > 540-21 {
		t.Fatalf("controls outside the safe area: move %+v, jump %+v", move, jump)
	}
	if move.Size.Width < 44 || jump.Size.Width <= move.Size.Width*0.6 {
		t.Fatalf("control sizes: move %+v, jump %+v", move.Size, jump.Size)
	}
}

func TestTouchControlsLeaveTheExplorerAtSpawnUncovered(t *testing.T) {
	// A landscape iPhone, where the letterboxed canvas puts the spawn close to the d-pad.
	app, g := startCaveRun(t, nsgotest.Size(844, 390), nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21}}))
	startPlaying(t, app, g)
	size := g.currentViewSize()
	b := g.level.playerBody.Bounds()
	topLeft := g.viewport.GameToView(size, g.level.cam.WorldToView(core.Point{X: core.Points(b.X), Y: core.Points(b.Y)}))
	bottomRight := g.viewport.GameToView(size, g.level.cam.WorldToView(core.Point{X: core.Points(b.X + b.W), Y: core.Points(b.Y + b.H)}))
	explorer := core.Bounds{Origin: topLeft, Size: core.Size{Width: bottomRight.X - topLeft.X, Height: bottomRight.Y - topLeft.Y}}
	for _, id := range []string{"move", "jump"} {
		if f := controlFrame(t, g, id); overlaps(f, explorer) {
			t.Fatalf("the %s control %+v covers the explorer at spawn %+v", id, f, explorer)
		}
	}
}

func overlaps(a, b core.Bounds) bool {
	return a.Origin.X < b.Origin.X+b.Size.Width && b.Origin.X < a.Origin.X+a.Size.Width &&
		a.Origin.Y < b.Origin.Y+b.Size.Height && b.Origin.Y < a.Origin.Y+a.Size.Height
}
