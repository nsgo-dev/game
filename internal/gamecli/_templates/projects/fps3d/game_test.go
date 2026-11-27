package main

import (
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// desktopEnv is a desktop window that can lock the pointer, as a mouse player's has.
var desktopEnv = core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, CanLockPointer: true}

// start mounts the game at 960x540 without a GPU, which the playthroughs do not need, and returns the app and the session.
func start(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *session) {
	t.Helper()
	var s *session
	opts = append([]nsgotest.Option{nsgotest.Size(960, 540), nsgotest.NoGPU()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newSession(cx)
		return s.view(cx)
	}, opts...)
	return app, s
}

// device is what the bot plays through: it turns the bot's intent into that device's input.
type device interface {
	apply(in intent)
	release()
}

// mouseKeys plays with locked mouse motion to look, the primary mouse button to fire, WASD to walk and R to reload.
type mouseKeys struct {
	app  *nsgotest.App
	held map[string]bool
}

func (d *mouseKeys) apply(in intent) {
	sens := float64(arena.Player.MouseSensitivity)
	if in.turn != 0 || in.tilt != 0 {
		d.app.PointerDelta(-float64(in.turn)/sens, -float64(in.tilt)/sens)
	}
	// Holding the left button is a press held down on the arena, which input.MouseButton follows. nsgotest.App.MouseDown only reaches views with OnMouseDown, not the press events a real left click also sends, so the playthrough holds the button with PressDown and PressUp.
	d.hold("fire", in.fire, func() { d.app.PressDown("arena") }, func() { d.app.PressUp("arena") })
	d.key("KeyR", in.reload)
	d.key("KeyD", in.moveX > 0.3)
	d.key("KeyA", in.moveX < -0.3)
	d.key("KeyW", in.moveY > 0.3)
	d.key("KeyS", in.moveY < -0.3)
}

func (d *mouseKeys) key(code string, want bool) {
	d.hold(code, want, func() { d.app.KeyCodeDown(code) }, func() { d.app.KeyCodeUp(code) })
}

func (d *mouseKeys) hold(name string, want bool, down, up func()) {
	if d.held[name] == want {
		return
	}
	d.held[name] = want
	if want {
		down()
	} else {
		up()
	}
}

func (d *mouseKeys) release() {
	d.apply(intent{})
}

// pad plays with game controller 1: the right stick looks, the left stick walks, the right trigger fires and West reloads.
type pad struct {
	app *nsgotest.App
}

func (d *pad) apply(in intent) {
	turnStep := arena.Player.TurnSpeed * float32(step.Seconds())
	x, y := stickFor(-in.turn/turnStep, in.tilt/turnStep)
	d.app.SetGamepadAxis(1, gamepad.AxisRightX, x)
	d.app.SetGamepadAxis(1, gamepad.AxisRightY, -y)
	x, y = stickFor(in.moveX, in.moveY)
	d.app.SetGamepadAxis(1, gamepad.AxisLeftX, x)
	d.app.SetGamepadAxis(1, gamepad.AxisLeftY, -y)
	d.app.SetGamepadButton(1, gamepad.ButtonRT, press(in.fire))
	d.app.SetGamepadButton(1, gamepad.ButtonWest, press(in.reload))
}

func (d *pad) release() { d.apply(intent{}) }

// stickFor returns the stick deflection that reads as (x, y) once input's default radial dead zone is taken out.
func stickFor(x, y float32) (float32, float32) {
	l := float32(math.Hypot(float64(x), float64(y)))
	if l < 1e-4 {
		return 0, 0
	}
	l = min(l, 1)
	raw := 0.15 + l*0.85
	n := float32(math.Hypot(float64(x), float64(y)))
	return x / n * raw, y / n * raw
}

func press(held bool) float32 {
	if held {
		return 1
	}
	return 0
}

// playThrough lets the bot play through d, one display frame at a time, until the match ends or limit passes.
func playThrough(app *nsgotest.App, s *session, d device, maxTurn float32, limit time.Duration) {
	var b bot
	for elapsed := time.Duration(0); !s.match.over() && elapsed < limit; elapsed += step {
		d.apply(b.decide(s.match, maxTurn))
		app.Advance(step)
	}
	d.release()
	app.Advance(step)
}

func TestKeyboardAndMousePlayThroughToAWin(t *testing.T) {
	app, s := start(t, nsgotest.Environment(desktopEnv))
	app.Find("text:Wave 1 in 3")
	// The first press on the arena locks the pointer for mouselook.
	app.Press("arena")
	playThrough(app, s, &mouseKeys{app: app, held: map[string]bool{}}, 0.2, 5*time.Minute)
	if s.match.phase != phaseWon {
		t.Fatalf("the match ended in phase %d with %d health, want a win", s.match.phase, s.match.player.health)
	}
	t.Logf("won in %.1fs with %d health", s.match.clock, s.match.player.health)
	if s.actions.PointerLockRequested() {
		t.Fatal("the pointer is still locked over the end panel")
	}
	app.Find("text:Arena cleared")
	app.Find("text:Wave 3 / 3")
	app.Find("text:Enemies 0")

	// Play again starts over from the countdown.
	app.Press("role:button Play again")
	app.Find("text:Wave 1 in 3")
	app.Find("text:Health 100")
	if app.Exists("end-panel") {
		t.Fatal("the end panel is still up after Play again")
	}
}

func TestGamepadPlaysThroughToAWin(t *testing.T) {
	app, s := start(t)
	app.ConnectGamepad(1, "Standard Gamepad")
	playThrough(app, s, &pad{app: app}, arena.Player.TurnSpeed*float32(step.Seconds()), 5*time.Minute)
	if s.match.phase != phaseWon {
		t.Fatalf("the match ended in phase %d with %d health on wave %d, want a win", s.match.phase, s.match.player.health, s.match.wave+1)
	}
	t.Logf("won in %.1fs with %d health", s.match.clock, s.match.player.health)
	app.Find("text:Arena cleared")
	// Start plays again once the match is over.
	app.SetGamepadButton(1, gamepad.ButtonStart, 1)
	app.Advance(step * 2)
	app.SetGamepadButton(1, gamepad.ButtonStart, 0)
	app.Advance(step)
	app.Find("text:Wave 1 in 3")
}

func TestStandingStillIsALoss(t *testing.T) {
	app, s := start(t)
	app.Find("text:Health 100")
	for elapsed := time.Duration(0); !s.match.over() && elapsed < 3*time.Minute; elapsed += time.Second {
		app.Advance(time.Second)
	}
	if s.match.phase != phaseLost {
		t.Fatalf("an idle player ended in phase %d with %d health, want a loss", s.match.phase, s.match.player.health)
	}
	app.Find("text:You were downed")
	app.Find("text:Health 0")
	app.Find("end-panel")
	// Enter plays again from the keyboard.
	app.Key("enter")
	app.Advance(step * 2)
	app.Find("text:Wave 1 in 3")
}

func TestKeysWalkTurnSwitchReloadAndPause(t *testing.T) {
	app, s := start(t)
	m := s.match
	z := m.player.ctrl.Pos.Z
	app.KeyCodeDown("KeyW")
	app.Advance(500 * time.Millisecond)
	app.KeyCodeUp("KeyW")
	if m.player.ctrl.Pos.Z > z-2 {
		t.Fatalf("W for half a second moved the player from z %v to %v, want at least 2 m forward", z, m.player.ctrl.Pos.Z)
	}
	app.KeyDown("left")
	app.Advance(250 * time.Millisecond)
	app.KeyUp("left")
	if m.player.yaw < 0.4 {
		t.Fatalf("the left arrow for a quarter second turned to yaw %v, want at least 0.4", m.player.yaw)
	}
	app.KeyCode("Digit2")
	app.Advance(step * 2)
	app.Find("text:Arc launcher 3 / 6")
	app.KeyCode("KeyQ")
	app.Advance(step * 2)
	app.Find("text:Pulse rifle 20 / 100")
	app.KeyCodeDown("KeyF")
	app.Advance(step * 3)
	app.KeyCodeUp("KeyF")
	app.Advance(step)
	app.KeyCode("KeyR")
	app.Advance(step * 2)
	app.Find("text:Pulse rifle reloading")
	app.Advance(2 * time.Second)
	app.Find("text:Pulse rifle 20 / 99")

	app.KeyCode("KeyP")
	app.Advance(step * 2)
	app.Find("text:Paused")
	clock := m.clock
	app.Advance(time.Second)
	if m.clock != clock {
		t.Fatal("the match ran while paused")
	}
	app.KeyCode("KeyP")
	app.Advance(step * 2)
	if m.clock == clock {
		t.Fatal("the match did not resume")
	}
}

func controlFrame(t *testing.T, s *session, id string) core.Bounds {
	t.Helper()
	for _, c := range s.touch.Controls() {
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

func TestTouchSticksAndButtonsPlay(t *testing.T) {
	app, s := start(t, nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}))
	m := s.match
	if !s.touch.Visible() {
		t.Fatal("the touch controls are hidden on a phone")
	}
	app.Advance(step)

	// The move stick: drag up from inside its zone and hold. Up-positive moveY moves forward.
	z := m.player.ctrl.Pos.Z
	moveAt := centerOf(controlFrame(t, s, "move"))
	f := app.Touch("arena", moveAt)
	f.Move(core.Point{X: moveAt.X, Y: moveAt.Y - 60})
	app.Advance(500 * time.Millisecond)
	f.Lift()
	if m.player.ctrl.Pos.Z > z-1.5 {
		t.Fatalf("the move stick moved the player from z %v to %v, want at least 1.5 m forward", z, m.player.ctrl.Pos.Z)
	}

	// The look stick: drag left to turn left, then up to tilt up via shared lookY.
	lookAt := centerOf(controlFrame(t, s, "look"))
	f = app.Touch("arena", lookAt)
	f.Move(core.Point{X: lookAt.X - 60, Y: lookAt.Y})
	app.Advance(250 * time.Millisecond)
	f.Lift()
	if m.player.yaw < 0.3 {
		t.Fatalf("the look stick turned to yaw %v, want at least 0.3 to the left", m.player.yaw)
	}
	pitchBefore := m.player.pitch
	f = app.Touch("arena", lookAt)
	f.Move(core.Point{X: lookAt.X, Y: lookAt.Y - 60})
	app.Advance(250 * time.Millisecond)
	f.Lift()
	if m.player.pitch <= pitchBefore+0.2 {
		t.Fatalf("pushing the look stick up changed pitch %v -> %v, want upward tilt", pitchBefore, m.player.pitch)
	}

	// The fire button.
	before := m.player.weapons[0].mag
	f = app.Touch("arena", centerOf(controlFrame(t, s, "fire")))
	app.Advance(step * 3)
	f.Lift()
	if m.player.weapons[0].mag >= before {
		t.Fatalf("the fire button left the magazine at %d, want fewer than %d", m.player.weapons[0].mag, before)
	}

	// Weapon 2 selects the launcher directly; Weapon 1 selects the rifle directly; Swap switches again.
	f = app.Touch("arena", centerOf(controlFrame(t, s, "weapon2")))
	app.Advance(step * 2)
	f.Lift()
	app.Advance(step)
	app.Find("text:Arc launcher 3 / 6")

	f = app.Touch("arena", centerOf(controlFrame(t, s, "weapon1")))
	app.Advance(step * 2)
	f.Lift()
	app.Advance(step)
	if m.player.weapon != 0 {
		t.Fatalf("weapon1 selected slot %d, want 0", m.player.weapon)
	}

	f = app.Touch("arena", centerOf(controlFrame(t, s, "swap")))
	app.Advance(step * 2)
	f.Lift()
	app.Advance(step)
	app.Find("text:Arc launcher 3 / 6")
}

func TestTouchControlsShowFromFirstFrameOnPhonesAndHideOnDesktop(t *testing.T) {
	safe := core.Edges{Left: 47, Right: 47, Bottom: 21}
	ids := []string{"move", "look", "fire", "jump", "reload", "swap", "weapon1", "weapon2"}
	for _, tc := range []struct {
		name string
		os   core.OS
	}{
		{"ios", core.OSIOS},
		{"android", core.OSAndroid},
		{"phone web", core.OSWeb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, s := start(t, nsgotest.Size(844, 390), nsgotest.Environment(core.Environment{OS: tc.os, Idiom: core.IdiomPhone, SafeArea: safe}))
			if !s.touch.Visible() {
				t.Fatal("the touch controls are hidden on the first frame")
			}
			for _, id := range ids {
				if !app.Exists("id:touch-" + id) {
					t.Fatalf("control %q is not drawn on the first frame", id)
				}
				f := controlFrame(t, s, id)
				if f.Origin.X < safe.Left || f.Origin.X+f.Size.Width > 844-safe.Right || f.Origin.Y < safe.Top || f.Origin.Y+f.Size.Height > 390-safe.Bottom {
					t.Errorf("control %q frame %+v is outside the safe area %+v", id, f, safe)
				}
				if f.Size.Width < 44 || f.Size.Height < 44 {
					t.Errorf("control %q size %+v is smaller than 44pt", id, f.Size)
				}
			}
		})
	}

	// Desktop starts hidden, shows on touch, and hides again on keyboard input.
	app, s := start(t, nsgotest.Environment(desktopEnv))
	if s.touch.Visible() || app.Exists("id:touch-fire") {
		t.Fatal("the touch controls show on a desktop before any touch")
	}
	f := app.Touch("arena", centerOf(controlFrame(t, s, "weapon2")))
	app.Advance(step * 2)
	f.Lift()
	if !s.touch.Visible() || !app.Exists("id:touch-weapon2") {
		t.Fatal("a touch did not show the touch controls")
	}
	app.KeyCodeDown("KeyW")
	app.Advance(step * 2)
	app.KeyCodeUp("KeyW")
	app.Advance(step)
	if s.touch.Visible() || app.Exists("id:touch-fire") {
		t.Fatal("the keyboard did not hide the touch controls")
	}
}

func TestRootTitlesTheWindowFromTheProject(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.NoGPU())
	if app.Title != Project.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, Project.Title())
	}
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}

func TestArenaRendersInThreeDimensions(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(160, 90), nsgotest.ManualFrames())
	app.Advance(32 * time.Millisecond)
	img, err := app.GPUReadPixels("arena-gpu")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		t.Fatal("the GPU view drew nothing")
	}
	sky := color.RGBA{R: 0x10, G: 0x18, B: 0x26, A: 0xff}
	colors := map[color.RGBA]bool{}
	lit := 0
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			c := img.RGBAAt(x, y)
			colors[c] = true
			if c != sky {
				lit++
			}
		}
	}
	// The floor fills the lower half and the walls the horizon, in many shades.
	if lit < len(colors) || len(colors) < 16 {
		t.Fatalf("the frame has %d shades and %d pixels off the sky, want a lit 3D scene", len(colors), lit)
	}
	if img.RGBAAt(b.Min.X+b.Dx()/2, b.Max.Y-2) == sky {
		t.Fatal("the bottom of the frame is sky, want floor")
	}
	if msg := app.Find("gpu-message").Text; strings.TrimSpace(msg) != "" {
		t.Fatalf("the renderer reported %q", msg)
	}
}
