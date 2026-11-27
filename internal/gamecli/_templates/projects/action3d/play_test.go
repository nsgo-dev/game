package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
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
		app.SetGamepadAxis(1, gamepad.AxisLeftY, -c.moveY)
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

func TestReleasingTheMouseOutsideEndsTheDrag(t *testing.T) {
	_, p := startPlay(t)
	p.onMouseDown(core.MouseButtonEvent{Position: core.Point{X: 300, Y: 200}, Button: core.MouseButtonLeft})
	// The button came up outside the window; the mouse comes back with no button held.
	yaw := p.g.camYaw
	p.onMouseMove(core.MouseEvent{Position: core.Point{X: 200, Y: 200}})
	if p.dragging || p.g.camYaw != yaw {
		t.Fatalf("hovering after the release still drags (%v) the camera from %v to %v", p.dragging, yaw, p.g.camYaw)
	}
}

// controlFrame returns the frame of touch control id, failing t when absent.
func controlFrame(t *testing.T, p *play, id string) core.Bounds {
	t.Helper()
	for _, c := range p.touch.Controls() {
		if c.ID == id {
			return c.Frame
		}
	}
	t.Fatalf("touch control %q not found", id)
	return core.Bounds{}
}

func centerOf(b core.Bounds) core.Point {
	return core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

// TestTouchControlsMoveAndAttack drags the left stick forward and taps the attack button, and checks the controls appear once the screen is touched and hide again on keyboard input.
func TestTouchControlsMoveAndAttack(t *testing.T) {
	app, p := startPlay(t, nsgotest.Idiom(core.IdiomDesktop))
	if ops := app.Find("id:touch-controls").Ops; len(ops) != 0 {
		t.Fatalf("the touch controls drew %d ops before any touch", len(ops))
	}
	if app.Exists("id:touch-attack") {
		t.Fatal("the touch controls show on a desktop before any touch")
	}
	start := p.g.hero.pos
	moveAt := centerOf(controlFrame(t, p, "move"))
	finger := app.Touch("id:game-view", moveAt)
	finger.Move(core.Point{X: moveAt.X, Y: moveAt.Y - 60})
	finger.Hold(600 * time.Millisecond)
	if p.g.hero.pos.Z > start.Z-1 {
		t.Fatalf("pushing the stick forward moved the hero from %v to %v", start, p.g.hero.pos)
	}
	if ops := app.Find("id:touch-controls").Ops; len(ops) == 0 || !app.Exists("id:touch-attack") {
		t.Fatal("the touch controls did not draw while touched")
	}
	finger.Lift()

	attack := centerOf(controlFrame(t, p, "attack"))
	tap := app.Touch("id:game-view", attack)
	app.Advance(2 * step)
	tap.Lift()
	if p.g.hero.anim.state() != stateAttack {
		t.Fatalf("tapping Attack did not swing: state %q", p.g.hero.anim.state())
	}

	// Using the keyboard hides the touch controls again.
	app.KeyCodeDown("KeyW")
	app.Advance(2 * step)
	app.KeyCodeUp("KeyW")
	app.Advance(step)
	if p.touch.Visible() || app.Exists("id:touch-attack") {
		t.Fatal("the keyboard did not hide the touch controls")
	}
}

// TestTouchJumpsAndTurnsTheCamera taps Jump and drags the camera stick on the right side of the screen.
func TestTouchJumpsAndTurnsTheCamera(t *testing.T) {
	app, p := startPlay(t)
	app.Advance(100 * time.Millisecond)
	tap := app.Touch("id:game-view", centerOf(controlFrame(t, p, "jump")))
	app.Advance(150 * time.Millisecond)
	tap.Lift()
	if p.g.hero.pos.Y < 0.3 {
		t.Fatalf("tapping Jump did not jump: hero at %v", p.g.hero.pos)
	}
	app.Advance(time.Second)

	yaw := p.g.camYaw
	lookAt := centerOf(controlFrame(t, p, "look"))
	finger := app.Touch("id:game-view", lookAt)
	finger.Move(core.Point{X: lookAt.X + 50, Y: lookAt.Y})
	finger.Hold(300 * time.Millisecond)
	finger.Lift()
	if p.g.camYaw >= yaw {
		t.Fatalf("dragging the camera stick right did not turn the camera right: yaw %v, was %v", p.g.camYaw, yaw)
	}
}

// TestTouchControlsShowFromFirstFrameOnPhonesAndRespectSafeArea checks native phones and phone browsers start with every control visible, inside the safe area and at least 44pt wide and tall.
func TestTouchControlsShowFromFirstFrameOnPhonesAndRespectSafeArea(t *testing.T) {
	safe := core.Edges{Left: 47, Right: 47, Bottom: 21}
	for _, tc := range []struct {
		name string
		os   core.OS
	}{
		{"ios", core.OSIOS},
		{"android", core.OSAndroid},
		{"phone web", core.OSWeb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, p := startPlay(t, nsgotest.Size(844, 390), nsgotest.Environment(core.Environment{OS: tc.os, Idiom: core.IdiomPhone, SafeArea: safe}))
			if !p.touch.Visible() {
				t.Fatal("the touch controls are hidden on the first frame")
			}
			for _, id := range []string{"move", "look", "attack", "jump"} {
				if !app.Exists("id:touch-" + id) {
					t.Fatalf("control %q is not drawn on the first frame", id)
				}
				f := controlFrame(t, p, id)
				if f.Origin.X < safe.Left || f.Origin.X+f.Size.Width > 844-safe.Right || f.Origin.Y < safe.Top || f.Origin.Y+f.Size.Height > 390-safe.Bottom {
					t.Errorf("control %q frame %+v is outside the safe area %+v", id, f, safe)
				}
				if f.Size.Width < 44 || f.Size.Height < 44 {
					t.Errorf("control %q size %+v is smaller than 44pt", id, f.Size)
				}
			}
		})
	}
}

// TestGamepadTurnsTheCameraWithTheRightStickAndShoulders turns the camera with the right stick and the shoulder buttons.
func TestGamepadTurnsTheCameraWithTheRightStickAndShoulders(t *testing.T) {
	app, p := startPlay(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(step)

	yaw := p.g.camYaw
	app.SetGamepadAxis(1, gamepad.AxisRightX, 1)
	app.Advance(300 * time.Millisecond)
	app.SetGamepadAxis(1, gamepad.AxisRightX, 0)
	if p.g.camYaw >= yaw {
		t.Fatalf("the right stick did not turn the camera right: yaw %v, was %v", p.g.camYaw, yaw)
	}

	yaw = p.g.camYaw
	app.SetGamepadButton(1, gamepad.ButtonLB, 1)
	app.Advance(300 * time.Millisecond)
	app.SetGamepadButton(1, gamepad.ButtonLB, 0)
	if p.g.camYaw <= yaw {
		t.Fatalf("the left shoulder did not turn the camera left: yaw %v, was %v", p.g.camYaw, yaw)
	}

	yaw = p.g.camYaw
	app.SetGamepadButton(1, gamepad.ButtonRB, 1)
	app.Advance(300 * time.Millisecond)
	app.SetGamepadButton(1, gamepad.ButtonRB, 0)
	if p.g.camYaw >= yaw {
		t.Fatalf("the right shoulder did not turn the camera right: yaw %v, was %v", p.g.camYaw, yaw)
	}
}

// TestArrowKeysMoveTheHero walks the hero with each arrow key, which game.yaml binds alongside WASD.
func TestArrowKeysMoveTheHero(t *testing.T) {
	app, p := startPlay(t)
	walk := func(key string) (dx, dz float32) {
		from := p.g.hero.pos
		app.KeyDown(key)
		app.Advance(300 * time.Millisecond)
		app.KeyUp(key)
		app.Advance(300 * time.Millisecond)
		return p.g.hero.pos.X - from.X, p.g.hero.pos.Z - from.Z
	}
	// The camera starts behind the hero, so on screen up is the way the camera looks.
	forward := math3d.Vec3{X: -sin(p.g.camYaw), Z: -cos(p.g.camYaw)}
	right := math3d.Vec3{X: cos(p.g.camYaw), Z: -sin(p.g.camYaw)}
	for _, tc := range []struct {
		key  string
		want math3d.Vec3
	}{{"up", forward}, {"down", forward.Scale(-1)}, {"left", right.Scale(-1)}, {"right", right}} {
		dx, dz := walk(tc.key)
		if moved := dx*tc.want.X + dz*tc.want.Z; moved < 0.5 {
			t.Errorf("the %s arrow moved the hero %v m that way (%v, %v)", tc.key, moved, dx, dz)
		}
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
