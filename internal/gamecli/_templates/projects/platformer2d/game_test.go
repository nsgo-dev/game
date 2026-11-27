package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

const frame = time.Second / 60

// startGame starts the app in a 960 by 540 window, three times the virtual screen, with opts after those, and returns it with its game.
func startGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *platformerGame) {
	t.Helper()
	var g *platformerGame
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return newRoot(cx, func(pg *platformerGame) { g = pg })
	}, append([]nsgotest.Option{nsgotest.Size(960, 540), nsgotest.ManualFrames()}, opts...)...)
	app.Advance(3 * frame)
	if g == nil {
		t.Fatal("the game did not start")
	}
	app.Find("text:Sparks 0/8")
	app.Find("text:Lives 3")
	return app, g
}

// controls is one way to play: a device that can hold right and hold jump.
type controls interface {
	right(down bool)
	jump(down bool)
}

type keyboard struct{ app *nsgotest.App }

func (k keyboard) right(down bool) { keyChange(k.app, "right", down) }
func (k keyboard) jump(down bool)  { keyChange(k.app, "space", down) }

func keyChange(app *nsgotest.App, key string, down bool) {
	if down {
		app.KeyDown(key)
	} else {
		app.KeyUp(key)
	}
}

type controller struct{ app *nsgotest.App }

func (c controller) right(down bool) {
	c.app.SetGamepadAxis(1, gamepad.AxisLeftX, value(down))
}

func (c controller) jump(down bool) {
	c.app.SetGamepadButton(1, gamepad.ButtonSouth, value(down))
}

func value(down bool) float32 {
	if down {
		return 1
	}
	return 0
}

// touchscreen holds fingers on the on-screen controls, one finger per control, as a player's two thumbs do. It reports the touches with App.Report because App.Touch gives every finger the same ID, so two fingers it puts down would be one.
type touchscreen struct {
	t    *testing.T
	app  *nsgotest.App
	g    *platformerGame
	down map[int]core.Point
}

func newTouchscreen(t *testing.T, app *nsgotest.App, g *platformerGame) *touchscreen {
	return &touchscreen{t: t, app: app, g: g, down: map[int]core.Point{}}
}

func controlFrame(t *testing.T, g *platformerGame, id string) core.Bounds {
	t.Helper()
	for _, c := range g.touch.Controls() {
		if c.ID == id {
			return c.Frame
		}
	}
	t.Fatalf("no touch control %q", id)
	return core.Bounds{}
}

func centerOf(f core.Bounds) core.Point {
	return core.Point{X: f.Origin.X + f.Size.Width/2, Y: f.Origin.Y + f.Size.Height/2}
}

func dpadPoint(t *testing.T, g *platformerGame, dx float64) core.Point {
	t.Helper()
	f := controlFrame(t, g, "move")
	c := centerOf(f)
	r := min(f.Size.Width, f.Size.Height) / 2
	return core.Point{X: c.X + core.Points(dx)*r*0.65, Y: c.Y}
}

func (s *touchscreen) left(down bool)  { s.finger(1, dpadPoint(s.t, s.g, -1), down) }
func (s *touchscreen) right(down bool) { s.finger(1, dpadPoint(s.t, s.g, 1), down) }
func (s *touchscreen) jump(down bool)  { s.finger(2, centerOf(controlFrame(s.t, s.g, "jump")), down) }

// finger puts finger id down at at, or lifts it, and reports every finger that is down, as a touch screen does.
func (s *touchscreen) finger(id int, at core.Point, down bool) {
	_, held := s.down[id]
	if held == down {
		return
	}
	phase := core.GestureBegan
	if down {
		s.down[id] = at
	} else {
		phase = core.GestureEnded
	}
	var touches []core.Touch
	for fid, p := range s.down {
		touches = append(touches, core.Touch{ID: fid, PagePosition: p, Changed: fid == id})
	}
	if !down {
		touches = append(touches, core.Touch{ID: id, PagePosition: at, Changed: true})
		delete(s.down, id)
	}
	s.app.Report(nsgotest.Event{Kind: nsgotest.EventTouch, ID: s.app.Find("game-view").ID, Phase: phase, Touches: touches})
}

// play runs right until the run ends or limit passes. With jumping, it jumps whenever the player stands before spikes, a pit or a wall, which is all this level needs; without it, it runs into the first spikes until the lives run out.
func play(app *nsgotest.App, g *platformerGame, c controls, jumping bool, limit time.Duration) {
	c.right(true)
	held := 0
	for elapsed := time.Duration(0); elapsed < limit && g.world.state == running; elapsed += frame {
		if jumping {
			switch {
			case held > 0:
				held--
				if held == 0 {
					c.jump(false)
				}
			case g.world.body.Grounded && obstacleAhead(g.world):
				c.jump(true)
				held = 18
			}
		}
		app.Advance(frame)
	}
	c.right(false)
	if held > 0 {
		c.jump(false)
	}
	app.Advance(2 * frame)
}

// obstacleAhead reports whether there are spikes, a wall or a bottomless pit just ahead of the player.
func obstacleAhead(w *world) bool {
	b := w.body.Bounds()
	rows := w.level.rows()
	col := int((b.Right() + 10) / w.tile)
	foot := int((b.Bottom() - 1) / w.tile)
	at := func(c, r int) byte {
		if c < 0 || r < 0 || r >= len(rows) || c >= len(rows[r]) {
			return glyphGround
		}
		return rows[r][c]
	}
	if g := at(col, foot); g == glyphGround || g == glyphSpikes {
		return true
	}
	for r := foot + 1; r < len(rows); r++ {
		if g := at(col, r); g == glyphGround || g == glyphLedge {
			return false
		}
	}
	return true
}

// checkWin checks that a run played with c reached the beacon through every room, lighting the lamp and collecting sparks on the way.
func checkWin(t *testing.T, app *nsgotest.App, g *platformerGame) {
	t.Helper()
	w := g.world
	if w.state != won {
		t.Fatalf("the run ended %v with the player at %+v and %d lives, want won", w.state, w.body.Bounds(), w.lives)
	}
	app.Find("text:Beacon lit!")
	if !w.checkpoints[0].lit {
		t.Error("the run never lit the lamp")
	}
	if w.collected == 0 {
		t.Error("the run collected no sparks")
	}
	if got := w.director.Room().ID; got != "summit" {
		t.Errorf("the run ended in room %q, want summit", got)
	}
}

func TestKeyboardPlaythroughWins(t *testing.T) {
	app, g := startGame(t)
	play(app, g, keyboard{app}, true, 40*time.Second)
	checkWin(t, app, g)
}

func TestGamepadPlaythroughWins(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Test Controller")
	app.Advance(2 * frame)
	play(app, g, controller{app}, true, 40*time.Second)
	checkWin(t, app, g)
}

func TestTouchPlaythroughWins(t *testing.T) {
	app, g := startGame(t, nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21}}))
	if !g.touch.Visible() || !app.Exists("id:touch-move") || !app.Exists("id:touch-jump") {
		t.Fatal("the touch controls are not visible on a phone")
	}
	play(app, g, newTouchscreen(t, app, g), true, 40*time.Second)
	checkWin(t, app, g)
}

// TestTouchControlsHideOnDesktopUntilTouched checks that on a desktop the touch controls stay hidden until touched, mouse clicks neither show them nor drive movement, and pressing a key hides them again.
func TestTouchControlsHideOnDesktopUntilTouched(t *testing.T) {
	app, g := startGame(t, nsgotest.Idiom(core.IdiomDesktop))
	if g.touch.Visible() || app.Exists("id:touch-jump") {
		t.Fatal("the touch controls show on a desktop before any touch")
	}

	// Clicking the mouse where the jump button and d-pad sit neither shows the controls nor moves or jumps the player.
	startX, startY := g.world.body.Bounds().X, g.world.body.Bounds().Y
	app.PressDown("game-view")
	app.HoverAt("game-view", dpadPoint(t, g, 1))
	app.Advance(300 * time.Millisecond)
	app.PressUp("game-view")
	app.PressDown("game-view")
	app.HoverAt("game-view", centerOf(controlFrame(t, g, "jump")))
	app.Advance(100 * time.Millisecond)
	app.PressUp("game-view")
	if g.touch.Visible() || app.Exists("id:touch-jump") {
		t.Fatal("a mouse click showed the touch controls")
	}
	if g.world.body.Bounds().X != startX || g.world.body.Bounds().Y != startY {
		t.Fatalf("a mouse click moved the player from (%.1f, %.1f) to (%.1f, %.1f)", startX, startY, g.world.body.Bounds().X, g.world.body.Bounds().Y)
	}

	// Touching the jump button shows the controls and jumps at once.
	finger := app.Touch("id:game-view", centerOf(controlFrame(t, g, "jump")))
	app.Advance(2 * frame)
	if !g.touch.Visible() || !app.Exists("id:touch-jump") || !app.Exists("id:touch-move") {
		t.Fatal("a touch did not show the touch controls")
	}
	if g.world.body.VY >= 0 {
		t.Fatalf("a touch on the jump button did not jump: VY=%v", g.world.body.VY)
	}
	finger.Lift()

	// Using the keyboard hides them again.
	app.KeyDown("right")
	app.Advance(2 * frame)
	app.KeyUp("right")
	app.Advance(frame)
	if g.touch.Visible() || app.Exists("id:touch-jump") {
		t.Fatal("the keyboard did not hide the touch controls")
	}
}

func TestTouchControlsStayInsideSafeAreaAndOutsideLetterbox(t *testing.T) {
	t.Run("landscape phone", func(t *testing.T) {
		safe := core.Edges{Left: 47, Right: 47, Bottom: 21}
		_, g := startGame(t, nsgotest.Size(844, 390), nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: safe}))
		move, jump := controlFrame(t, g, "move"), controlFrame(t, g, "jump")
		for _, c := range []struct {
			name string
			f    core.Bounds
		}{{"move", move}, {"jump", jump}} {
			if c.f.Size.Width < 44 || c.f.Size.Height < 44 {
				t.Errorf("%s is smaller than 44pt: %+v", c.name, c.f.Size)
			}
			if c.f.Origin.X < safe.Left || c.f.Origin.X+c.f.Size.Width > 844-safe.Right ||
				c.f.Origin.Y < safe.Top || c.f.Origin.Y+c.f.Size.Height > 390-safe.Bottom {
				t.Errorf("%s is outside the safe area: %+v", c.name, c.f)
			}
		}
	})
	t.Run("portrait phone", func(t *testing.T) {
		safe := core.Edges{Top: 59, Bottom: 34}
		size := core.Size{Width: 390, Height: 844}
		_, g := startGame(t, nsgotest.Size(size.Width, size.Height), nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: safe}))
		playfieldBottom := g.viewport.GameToView(size, core.Point{X: core.Points(g.viewport.Width), Y: core.Points(g.viewport.Height)}).Y
		for _, c := range g.touch.Controls() {
			if c.Frame.Size.Width < 44 || c.Frame.Size.Height < 44 {
				t.Errorf("%s is smaller than 44pt: %+v", c.ID, c.Frame.Size)
			}
			if c.Frame.Origin.X < safe.Left || c.Frame.Origin.X+c.Frame.Size.Width > size.Width-safe.Right ||
				c.Frame.Origin.Y < safe.Top || c.Frame.Origin.Y+c.Frame.Size.Height > size.Height-safe.Bottom {
				t.Errorf("%s is outside the safe area: %+v", c.ID, c.Frame)
			}
			if c.Frame.Origin.Y < playfieldBottom {
				t.Errorf("%s (%+v) overlaps the letterboxed playfield (bottom %.1f)", c.ID, c.Frame, playfieldBottom)
			}
		}
	})
}

func TestDPadFingerSlideReversesDirection(t *testing.T) {
	app, g := startGame(t, nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone}))
	x0 := g.world.body.Bounds().X
	finger := app.Touch("id:game-view", dpadPoint(t, g, 1))
	app.Advance(400 * time.Millisecond)
	xRight := g.world.body.Bounds().X
	if xRight-x0 < 20 {
		t.Fatalf("holding right on the d-pad moved %.1f points, want at least 20", xRight-x0)
	}
	// Slide the same finger to the left side of the d-pad without lifting.
	finger.Move(dpadPoint(t, g, -1))
	app.Advance(400 * time.Millisecond)
	finger.Lift()
	if xLeft := g.world.body.Bounds().X; xRight-xLeft < 20 {
		t.Fatalf("sliding left on the d-pad moved %.1f points left, want at least 20", xRight-xLeft)
	}
}

func TestRunningIntoSpikesLosesAndPlayAgainRestarts(t *testing.T) {
	app, g := startGame(t)
	play(app, g, keyboard{app}, false, 20*time.Second)
	if g.world.state != lost {
		t.Fatalf("the run ended %v, want lost", g.world.state)
	}
	app.Find("text:Out of lives")
	app.Find("text:Lives 0")
	app.Find("text:" + g.data.level.Name)

	app.Press("play-again")
	app.Advance(2 * frame)
	if g.world.state != running || app.Exists("end-panel") {
		t.Fatalf("Play again left the run %v", g.world.state)
	}
	app.Find("text:Lives 3")
}

func TestRestartKeysStartANewRunOnlyAfterTheEnd(t *testing.T) {
	app, g := startGame(t)
	first := g.world
	app.Key("r")
	app.Advance(2 * frame)
	if g.world != first {
		t.Fatal("R restarted a run that was still being played")
	}
	play(app, g, keyboard{app}, false, 20*time.Second)
	app.ConnectGamepad(1, "Test Controller")
	app.Advance(2 * frame)
	app.SetGamepadButton(1, gamepad.ButtonStart, 1)
	app.Advance(2 * frame)
	app.SetGamepadButton(1, gamepad.ButtonStart, 0)
	app.Advance(2 * frame)
	if g.world == first || g.world.state != running {
		t.Fatalf("Start after the run was lost did not start a new one")
	}
}

func TestEachBindingMovesThePlayer(t *testing.T) {
	type device = func(app *nsgotest.App, g *platformerGame)
	key := func(name string) (device, device) {
		return func(app *nsgotest.App, _ *platformerGame) { app.KeyDown(name) }, func(app *nsgotest.App, _ *platformerGame) { app.KeyUp(name) }
	}
	code := func(name string) (device, device) {
		return func(app *nsgotest.App, _ *platformerGame) { app.KeyCodeDown(name) }, func(app *nsgotest.App, _ *platformerGame) { app.KeyCodeUp(name) }
	}
	button := func(b gamepad.Button) (device, device) {
		return func(app *nsgotest.App, _ *platformerGame) {
				app.ConnectGamepad(1, "Test Controller")
				app.SetGamepadButton(1, b, 1)
			}, func(app *nsgotest.App, _ *platformerGame) {
				app.SetGamepadButton(1, b, 0)
			}
	}
	stick := func(x float32) (device, device) {
		return func(app *nsgotest.App, _ *platformerGame) {
				app.ConnectGamepad(1, "Test Controller")
				app.SetGamepadAxis(1, gamepad.AxisLeftX, x)
			}, func(app *nsgotest.App, _ *platformerGame) {
				app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
			}
	}
	both := func(hold, leave device) [2]device { return [2]device{hold, leave} }
	for _, tc := range []struct {
		name string
		dev  [2]device
		dir  float32
	}{
		{"left arrow", both(key("left")), -1},
		{"right arrow", both(key("right")), 1},
		{"A", both(code("KeyA")), -1},
		{"D", both(code("KeyD")), 1},
		{"dpad left", both(button(gamepad.ButtonDPadLeft)), -1},
		{"dpad right", both(button(gamepad.ButtonDPadRight)), 1},
		{"left stick left", both(stick(-1)), -1},
		{"left stick right", both(stick(1)), 1},
		{"touch left", [2]device{func(app *nsgotest.App, g *platformerGame) { newTouchscreen(t, app, g).left(true) }, nil}, -1},
		{"touch right", [2]device{func(app *nsgotest.App, g *platformerGame) { newTouchscreen(t, app, g).right(true) }, nil}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startGame(t)
			if tc.dir < 0 {
				// The start is next to the left wall; stand where there is room to run left.
				g.world.placePlayer(g.world.standingAt(6, 9))
				app.Advance(2 * frame)
			}
			x := g.world.body.Bounds().X
			tc.dev[0](app, g)
			app.Advance(500 * time.Millisecond)
			if moved := (g.world.body.Bounds().X - x) * tc.dir; moved < 20 {
				t.Fatalf("holding %s moved the player %.1f points, want at least 20 the right way", tc.name, moved)
			}
			if tc.dev[1] != nil {
				tc.dev[1](app, g)
			}
		})
	}
}

func TestEachJumpBindingJumps(t *testing.T) {
	for _, tc := range []struct {
		name string
		hold func(app *nsgotest.App, g *platformerGame)
	}{
		{"space", func(app *nsgotest.App, _ *platformerGame) { app.KeyDown("space") }},
		{"up arrow", func(app *nsgotest.App, _ *platformerGame) { app.KeyDown("up") }},
		{"W", func(app *nsgotest.App, _ *platformerGame) { app.KeyCodeDown("KeyW") }},
		{"south button", func(app *nsgotest.App, _ *platformerGame) {
			app.ConnectGamepad(1, "Test Controller")
			app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
		}},
		{"touch", func(app *nsgotest.App, g *platformerGame) { newTouchscreen(t, app, g).jump(true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startGame(t)
			if !g.world.body.Grounded {
				t.Fatal("the player is not standing at the start")
			}
			y := g.world.body.Bounds().Y
			tc.hold(app, g)
			top := y
			for range 20 {
				app.Advance(frame)
				top = min(top, g.world.body.Bounds().Y)
			}
			if rose := y - top; rose < 16 {
				t.Fatalf("holding %s raised the player %.1f points, want at least a tile", tc.name, rose)
			}
		})
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 540), nsgotest.ManualFrames())
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}
