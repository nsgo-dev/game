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
	app  *nsgotest.App
	g    *platformerGame
	down map[int]core.Point
}

func newTouchscreen(app *nsgotest.App, g *platformerGame) *touchscreen {
	return &touchscreen{app: app, g: g, down: map[int]core.Point{}}
}

func (s *touchscreen) left(down bool)  { s.finger(1, "left", down) }
func (s *touchscreen) right(down bool) { s.finger(1, "right", down) }
func (s *touchscreen) jump(down bool)  { s.finger(2, actionJump, down) }

// finger puts finger id down on the middle of action's region, or lifts it, and reports every finger that is down, as a touch screen does.
func (s *touchscreen) finger(id int, action string, down bool) {
	r, ok := Project.Region(action)
	if !ok {
		panic("game.yaml binds no region to " + action)
	}
	at := s.g.viewport.GameToView(s.g.currentViewSize(), core.Point{X: r.Origin.X + r.Size.Width/2, Y: r.Origin.Y + r.Size.Height/2})
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
	app, g := startGame(t)
	play(app, g, newTouchscreen(app, g), true, 40*time.Second)
	checkWin(t, app, g)
}

// TestDesktopShowsTheControlsOncePointedAt checks that on a desktop the on-screen controls stay hidden until the game view is touched, or clicked, since the mouse plays through the same regions.
func TestDesktopShowsTheControlsOncePointedAt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		point func(app *nsgotest.App, g *platformerGame)
	}{
		{"touch", func(app *nsgotest.App, g *platformerGame) { newTouchscreen(app, g).jump(true) }},
		{"click", func(app *nsgotest.App, _ *platformerGame) {
			app.PressDown("game-view")
			app.PressUp("game-view")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startGame(t, nsgotest.Idiom(core.IdiomDesktop))
			if g.env.Idiom() != core.IdiomDesktop {
				t.Skipf("the platform's idiom is %v", g.env.Idiom())
			}
			if g.showTouchControls() {
				t.Fatal("the on-screen controls are drawn on a desktop before the game view was pointed at")
			}
			tc.point(app, g)
			app.Advance(frame)
			if !g.showTouchControls() {
				t.Errorf("the on-screen controls are hidden after a %s", tc.name)
			}
		})
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

// regionPoint returns a view point 10 points inside the top left of the region game.yaml binds to action.
func regionPoint(g *platformerGame, action string) core.Point {
	r, ok := Project.Region(action)
	if !ok {
		panic("game.yaml binds no region to " + action)
	}
	return g.viewport.GameToView(g.currentViewSize(), core.Point{X: r.Origin.X + 10, Y: r.Origin.Y + 10})
}

// click holds the left mouse button over action's on-screen control.
func click(app *nsgotest.App, g *platformerGame, action string) {
	app.PressDown("game-view")
	app.HoverAt("game-view", regionPoint(g, action))
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
	mouse := func(action string) (device, device) {
		return func(app *nsgotest.App, g *platformerGame) { click(app, g, action) }, func(app *nsgotest.App, _ *platformerGame) { app.PressUp("game-view") }
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
		{"touch left", [2]device{func(app *nsgotest.App, g *platformerGame) { newTouchscreen(app, g).left(true) }, nil}, -1},
		{"touch right", [2]device{func(app *nsgotest.App, g *platformerGame) { newTouchscreen(app, g).right(true) }, nil}, 1},
		{"click left", both(mouse("left")), -1},
		{"click right", both(mouse("right")), 1},
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
		{"touch", func(app *nsgotest.App, g *platformerGame) { newTouchscreen(app, g).jump(true) }},
		{"click", func(app *nsgotest.App, g *platformerGame) { click(app, g, actionJump) }},
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
