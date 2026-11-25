package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// The size of the test window; the touch tests place fingers in it.
const testW, testH = 640, 400

// startPlay starts the game in a headless app and returns the app and its play. The gameplay tests run without a GPU: the game steps the same, and the render tests draw it.
func startPlay(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *play) {
	t.Helper()
	var p *play
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		p, node = newPlay(cx)
		return node
	}, append([]nsgotest.Option{nsgotest.Size(testW, testH), nsgotest.NoGPU()}, opts...)...)
	if p == nil {
		t.Fatalf("the game did not load: %s", app.Find("id:load-error").Text)
	}
	return app, p
}

// pressOnce holds button on pad 1 for one step when want and releases it after, so each press is a new JustPressed.
func pressOnce(app *nsgotest.App, held map[gamepad.Button]bool, button gamepad.Button, want bool) {
	switch {
	case want && !held[button]:
		app.SetGamepadButton(1, button, 1)
		held[button] = true
	case held[button]:
		app.SetGamepadButton(1, button, 0)
		held[button] = false
	}
}

// TestGamepadPlaythroughWins plays the whole run with a game controller, the bot steering the left stick and pressing the face buttons, to the shrine, and then plays again from the start.
func TestGamepadPlaythroughWins(t *testing.T) {
	app, p := startPlay(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(step)
	b := &bot{route: winRoute}
	held := map[gamepad.Button]bool{}
	for p.g.phase == phasePlaying && p.g.elapsed < 2*time.Minute {
		c := b.next(p.g)
		app.SetGamepadAxis(1, gamepad.AxisLeftX, c.moveX)
		app.SetGamepadAxis(1, gamepad.AxisLeftY, c.moveY)
		pressOnce(app, held, gamepad.ButtonWest, c.attack)
		pressOnce(app, held, gamepad.ButtonSouth, c.jump)
		app.Advance(step)
	}
	if p.g.phase != phaseWon {
		t.Fatalf("after %v the run is %v at %v, key %v", p.g.elapsed, p.g.phase, p.g.hero.pos, p.g.hasKey)
	}
	app.Find("text:Victory")
	app.Find("text:Key found")
	app.Find("text:Enemies 0")

	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
	app.SetGamepadAxis(1, gamepad.AxisLeftY, 0)
	app.SetGamepadButton(1, gamepad.ButtonStart, 1)
	app.Advance(2 * step)
	app.SetGamepadButton(1, gamepad.ButtonStart, 0)
	app.Advance(step)
	if p.g.phase != phasePlaying || p.g.hasKey || p.g.enemiesLeft() != len(p.g.enemies) {
		t.Fatalf("Start did not restart the run: %v, key %v, %d enemies", p.g.phase, p.g.hasKey, p.g.enemiesLeft())
	}
	app.Find("text:No key")
}

// TestKeyboardRunIntoTheGuardLoses walks north with the keyboard into the locked gate, where the guard fells the hero, and plays again with the banner's button.
func TestKeyboardRunIntoTheGuardLoses(t *testing.T) {
	app, p := startPlay(t)
	app.Find("text:Health 5/5")
	app.KeyCodeDown("KeyW")
	for p.g.phase == phasePlaying && p.g.elapsed < time.Minute {
		app.Advance(100 * time.Millisecond)
	}
	app.KeyCodeUp("KeyW")
	if p.g.phase != phaseLost {
		t.Fatalf("after %v the run is %v with %d health at %v", p.g.elapsed, p.g.phase, p.g.hero.health, p.g.hero.pos)
	}
	app.Find("text:Defeated")
	app.Find("text:Health 0/5")
	app.Press("role:button Play again")
	app.Find("text:Health 5/5")
	if app.Exists("id:banner") {
		t.Fatal("the banner stayed after playing again")
	}
}

func TestKeyboardJumpsSwingsAndTurnsTheCamera(t *testing.T) {
	app, p := startPlay(t)
	app.Advance(100 * time.Millisecond)

	app.KeyDown("space")
	app.Advance(150 * time.Millisecond)
	app.KeyUp("space")
	if p.g.hero.pos.Y < 0.3 {
		t.Fatalf("space did not jump: hero at %v", p.g.hero.pos)
	}
	app.Advance(time.Second)

	app.KeyCodeDown("KeyJ")
	app.Advance(50 * time.Millisecond)
	app.KeyCodeUp("KeyJ")
	if p.g.hero.anim.state() != stateAttack {
		t.Fatalf("J did not swing: state %q", p.g.hero.anim.state())
	}

	yaw := p.g.camYaw
	app.KeyCodeDown("KeyE")
	app.Advance(300 * time.Millisecond)
	app.KeyCodeUp("KeyE")
	if p.g.camYaw >= yaw {
		t.Fatalf("E did not turn the camera right: yaw %v, was %v", p.g.camYaw, yaw)
	}
}

func TestMouseDragTurnsTheCamera(t *testing.T) {
	app, p := startPlay(t)
	yaw := p.g.camYaw
	app.MouseDownAt("id:game-view", core.Point{X: 300, Y: 200}, core.MouseButtonLeft)
	app.HoverAt("id:game-view", core.Point{X: 200, Y: 200})
	app.MouseUpAt("id:game-view", core.Point{X: 200, Y: 200}, core.MouseButtonLeft)
	if p.g.camYaw <= yaw {
		t.Fatalf("dragging left did not turn the camera left: yaw %v, was %v", p.g.camYaw, yaw)
	}
}

// TestTouchControlsMoveAndAttack drags the left stick forward and taps the attack button, and checks the controls appear once the screen is touched.
func TestTouchControlsMoveAndAttack(t *testing.T) {
	app, p := startPlay(t)
	if ops := app.Find("id:touch-controls").Ops; len(ops) != 0 {
		t.Fatalf("the touch controls drew %d ops before any touch", len(ops))
	}
	start := p.g.hero.pos
	finger := app.Touch("id:game-view", core.Point{X: 120, Y: 300})
	finger.Move(core.Point{X: 120, Y: 240})
	finger.Hold(600 * time.Millisecond)
	if p.g.hero.pos.Z > start.Z-1 {
		t.Fatalf("pushing the stick forward moved the hero from %v to %v", start, p.g.hero.pos)
	}
	if ops := app.Find("id:touch-controls").Ops; len(ops) == 0 {
		t.Fatal("the touch controls did not draw while touched")
	}
	finger.Lift()

	attack := p.buttons[0].Center
	tap := app.Touch("id:game-view", attack)
	app.Advance(2 * step)
	tap.Lift()
	if p.g.hero.anim.state() != stateAttack {
		t.Fatalf("tapping Attack did not swing: state %q", p.g.hero.anim.state())
	}
}

func TestNoGPUSaysSoAndStillPlays(t *testing.T) {
	app, p := startPlay(t)
	app.Find("id:gpu-status")
	app.KeyCodeDown("KeyW")
	app.Advance(500 * time.Millisecond)
	app.KeyCodeUp("KeyW")
	if p.g.hero.pos.Z >= p.g.level.spawn.Z-1 {
		t.Fatalf("the hero did not move without a GPU: %v", p.g.hero.pos)
	}
}
