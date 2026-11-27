package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// frame is one display frame, one step of the game's loop.
const frame = time.Second / StepRate

// startGame starts the game's view in a test app the size of its window and returns the app and the game.
func startGame(t *testing.T) (*nsgotest.App, *Game) {
	t.Helper()
	var g *Game
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		g, node = NewGameView(cx)
		return node
	}, nsgotest.Size(960, 800))
	app.Advance(2 * frame)
	return app, g
}

// controls play the game the way one kind of player does: with the keyboard, a gamepad or touches.
type controls interface {
	// moveTo puts the cursor on c.
	moveTo(c pathfind.Cell)
	// choose picks the tower kind at index i of data/towers.yaml.
	choose(i int)
	// build builds the chosen tower on the cursor.
	build()
	// upgrade upgrades the tower on the cursor.
	upgrade()
	// callWave calls the next wave.
	callWave()
	// speedUp steps the speed once.
	speedUp()
	// playAgain starts a new game from the end screen.
	playAgain()
}

// keyboardControls press keys, which game.yaml binds.
type keyboardControls struct {
	app *nsgotest.App
	g   *Game
}

func (k keyboardControls) key(name string) {
	k.app.Key(name)
	k.app.Advance(2 * frame)
}

func (k keyboardControls) moveTo(c pathfind.Cell) {
	steer(k.g, c, func(dx, dy int) {
		switch {
		case dx > 0:
			k.key("right")
		case dx < 0:
			k.key("a")
		case dy > 0:
			k.key("s")
		default:
			k.key("up")
		}
	})
}

func (k keyboardControls) choose(i int) { k.key(fmt.Sprint(i + 1)) }
func (k keyboardControls) build()       { k.key("enter") }
func (k keyboardControls) upgrade()     { k.key("u") }
func (k keyboardControls) callWave()    { k.key("n") }
func (k keyboardControls) speedUp()     { k.key("f") }
func (k keyboardControls) playAgain()   { k.key("enter") }

// padControls press the buttons of gamepad 1.
type padControls struct {
	app *nsgotest.App
	g   *Game
}

func (p padControls) press(b gamepad.Button) {
	p.app.SetGamepadButton(1, b, 1)
	p.app.Advance(2 * frame)
	p.app.SetGamepadButton(1, b, 0)
	p.app.Advance(2 * frame)
}

func (p padControls) moveTo(c pathfind.Cell) {
	steer(p.g, c, func(dx, dy int) {
		switch {
		case dx > 0:
			p.press(gamepad.ButtonDPadRight)
		case dx < 0:
			p.press(gamepad.ButtonDPadLeft)
		case dy > 0:
			p.press(gamepad.ButtonDPadDown)
		default:
			p.press(gamepad.ButtonDPadUp)
		}
	})
}

func (p padControls) choose(i int) {
	for range len(p.g.World.Data.Towers.Towers) {
		if p.g.Kind == i {
			return
		}
		p.press(gamepad.ButtonRB)
	}
}

func (p padControls) build()     { p.press(gamepad.ButtonSouth) }
func (p padControls) upgrade()   { p.press(gamepad.ButtonNorth) }
func (p padControls) callWave()  { p.press(gamepad.ButtonStart) }
func (p padControls) speedUp()   { p.press(gamepad.ButtonRT) }
func (p padControls) playAgain() { p.press(gamepad.ButtonSouth) }

// touchControls tap the battlefield and the HUD's buttons.
type touchControls struct {
	app *nsgotest.App
	g   *Game
}

func (tc touchControls) tap(c pathfind.Cell) {
	tc.app.Touch("battlefield", tc.g.CellPoint(c)).Lift()
	tc.app.Advance(2 * frame)
}

func (tc touchControls) button(id string) {
	tc.app.Press(id)
	tc.app.Advance(2 * frame)
}

func (tc touchControls) moveTo(c pathfind.Cell) {
	if tc.g.Cursor != c {
		tc.tap(c)
	}
}

func (tc touchControls) choose(i int) { tc.button("tower-" + tc.g.World.Data.Towers.Towers[i].Name) }

// build taps the cursor's cell again, which builds on it.
func (tc touchControls) build()     { tc.tap(tc.g.Cursor) }
func (tc touchControls) upgrade()   { tc.button("upgrade") }
func (tc touchControls) callWave()  { tc.button("next-wave") }
func (tc touchControls) speedUp()   { tc.button("speed") }
func (tc touchControls) playAgain() { tc.button("play-again") }

// steer calls move with the direction from the cursor to c until the cursor is there.
func steer(g *Game, c pathfind.Cell, move func(dx, dy int)) {
	for range 64 {
		dx, dy := c.Col-g.Cursor.Col, c.Row-g.Cursor.Row
		if dx == 0 && dy == 0 {
			return
		}
		move(dx, dy)
	}
}

var controllers = []struct {
	name string
	new  func(app *nsgotest.App, g *Game) controls
}{
	{"keyboard", func(app *nsgotest.App, g *Game) controls { return keyboardControls{app, g} }},
	{"gamepad", func(app *nsgotest.App, g *Game) controls {
		app.ConnectGamepad(1, "Xbox Wireless Controller")
		app.Advance(2 * frame)
		return padControls{app, g}
	}},
	{"touch", func(app *nsgotest.App, g *Game) controls { return touchControls{app, g} }},
}

// play plays plan with ctl at full speed until the game is over, checking that every order lands, and fails if it takes longer than limit.
func play(t *testing.T, app *nsgotest.App, g *Game, ctl controls, plan [][]order, limit time.Duration) {
	t.Helper()
	for g.Speed < maxSpeed {
		ctl.speedUp()
	}
	app.Find(fmt.Sprintf("text:Speed %dx", maxSpeed))
	w := g.World
	queued := -1
	var pending []order
	for elapsed := time.Duration(0); w.Outcome == Playing; elapsed += frame {
		if elapsed > limit {
			t.Fatalf("the game is still on after %v: wave %d, %d lives, %d money", limit, w.Wave, w.Lives, w.Money)
		}
		if between(w) && queued < w.Wave {
			queued = w.Wave
			pending = append(pending, plan[w.Wave]...)
		}
		for len(pending) > 0 {
			o := pending[0]
			cost, ok := o.cost(w)
			if !ok {
				t.Fatalf("order %+v cannot be carried out", o)
			}
			if w.Money < cost {
				break
			}
			money := w.Money
			ctl.moveTo(o.cell)
			if g.Cursor != o.cell {
				t.Fatalf("cursor on %v, want %v", g.Cursor, o.cell)
			}
			if o.kind == "" {
				ctl.upgrade()
			} else {
				for i, def := range w.Data.Towers.Towers {
					if def.Name == o.kind {
						ctl.choose(i)
					}
				}
				ctl.build()
			}
			if w.Money != money-cost {
				t.Fatalf("order %+v: money %d, want %d (%s)", o, w.Money, money-cost, g.message)
			}
			pending = pending[1:]
		}
		if len(pending) == 0 && between(w) {
			wave := w.Wave
			ctl.callWave()
			if w.Wave != wave+1 {
				t.Fatalf("calling wave %d left the wave at %d", wave+1, w.Wave)
			}
			app.Find(fmt.Sprintf("text:Wave %d/%d", w.Wave, w.Waves()))
			continue
		}
		app.Advance(frame)
	}
}

// TestPlaythroughWins plays the winning plan to a win with each kind of controls, then plays again.
func TestPlaythroughWins(t *testing.T) {
	for _, c := range controllers {
		t.Run(c.name, func(t *testing.T) {
			app, g := startGame(t)
			ctl := c.new(app, g)
			play(t, app, g, ctl, winningPlan, 4*time.Minute)
			if g.World.Outcome != Won {
				t.Fatalf("the game ended %v, want won", g.World.Outcome)
			}
			app.Advance(2 * frame)
			app.Find("text:The gate holds!")
			app.Find(fmt.Sprintf("text:All %d waves stopped with %d lives left", g.World.Waves(), g.World.Lives))

			ctl.playAgain()
			if g.World.Outcome != Playing || g.World.Wave != 0 || len(g.World.Towers) != 0 || app.Exists("outcome") {
				t.Fatalf("play again left the game %v on wave %d with %d towers", g.World.Outcome, g.World.Wave, len(g.World.Towers))
			}
			app.Find(fmt.Sprintf("text:Money %d", g.World.Data.Level.Money))
		})
	}
}

// TestPlaythroughLoses plays the thin plan to a loss with each kind of controls.
func TestPlaythroughLoses(t *testing.T) {
	for _, c := range controllers {
		t.Run(c.name, func(t *testing.T) {
			app, g := startGame(t)
			ctl := c.new(app, g)
			play(t, app, g, ctl, thinPlan, 4*time.Minute)
			if g.World.Outcome != Lost {
				t.Fatalf("the game ended %v, want lost", g.World.Outcome)
			}
			app.Advance(2 * frame)
			app.Find("text:The gate has fallen")
			app.Find("text:Lives 0")
			if app.Enabled("next-wave") || app.Enabled("pause") {
				t.Fatal("the wave and pause buttons are enabled after a loss")
			}
		})
	}
}

func TestStickMovesTheCursorAndRepeats(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(2 * frame)
	start := g.Cursor

	// A push inside the dead zone does nothing; a full push moves one tile at once and then repeats while held.
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.1)
	app.Advance(10 * frame)
	if g.Cursor != start {
		t.Fatalf("a slight push moved the cursor to %v", g.Cursor)
	}
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	app.Advance(2 * frame)
	if g.Cursor.Col != start.Col+1 {
		t.Fatalf("a full push moved the cursor to %v, want one tile right of %v", g.Cursor, start)
	}
	app.Advance(time.Duration(cursorDelay+2*cursorRepeat) * frame)
	if g.Cursor.Col < start.Col+3 {
		t.Fatalf("holding the stick moved the cursor to %v, want it repeating past column %d", g.Cursor, start.Col+3)
	}
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
	app.SetGamepadAxis(1, gamepad.AxisLeftY, 1)
	app.Advance(2 * frame)
	app.SetGamepadAxis(1, gamepad.AxisLeftY, 0)
	app.Advance(2 * frame)
	if g.Cursor.Row != start.Row+1 {
		t.Fatalf("pushing the stick down moved the cursor to %v", g.Cursor)
	}
}

func TestPauseStopsTheWorld(t *testing.T) {
	app, g := startGame(t)
	k := keyboardControls{app, g}
	// While paused, the toolbar refuses the wave, as the keys do.
	k.key("p")
	app.Find("text:Paused")
	if app.Enabled("next-wave") {
		t.Fatal("next wave is enabled while the game is paused")
	}
	g.CallWave()
	if g.World.Wave != 0 {
		t.Fatal("a wave was called while the game was paused")
	}
	k.key("p")
	k.callWave()
	app.Advance(time.Second)
	if len(g.World.Enemies) == 0 {
		t.Fatal("no enemy spawned a second after calling the wave")
	}
	k.key("p")
	app.Find("text:Paused")
	progress := g.World.Enemies[0].Progress
	app.Advance(time.Second)
	if g.World.Enemies[0].Progress != progress {
		t.Fatal("an enemy walked while the game was paused")
	}
	app.Press("pause")
	app.Advance(time.Second)
	if g.World.Enemies[0].Progress == progress {
		t.Fatal("the resume button did not resume the game")
	}
}

func TestHUDShowsTheTowerOnTheCursorAndRefusesWhatItCannotDo(t *testing.T) {
	app, g := startGame(t)
	k := keyboardControls{app, g}
	app.Find(fmt.Sprintf("text:Money %d", g.World.Money))
	app.Find(fmt.Sprintf("text:Lives %d", g.World.Lives))
	app.Find(fmt.Sprintf("text:Wave 0/%d", g.World.Waves()))
	if app.Enabled("upgrade") || app.Enabled("sell") {
		t.Fatal("upgrade and sell are enabled with no tower on the cursor")
	}

	// The cursor starts on the spawn, on the road.
	k.build()
	app.Find("text:Cannot build there")

	k.moveTo(pathfind.Cell{Col: 5, Row: 4})
	k.choose(0)
	k.build()
	bolt := g.World.Data.Towers.Towers[0]
	mortar := g.World.Data.Towers.Towers[2]
	k.moveTo(pathfind.Cell{Col: 6, Row: 2})
	k.choose(2)
	k.build()
	app.Find(fmt.Sprintf("text:Mortar needs %d", mortar.Cost))
	if len(g.World.Towers) != 1 {
		t.Fatal("built a mortar without the money for it")
	}
	k.moveTo(pathfind.Cell{Col: 5, Row: 4})
	app.Find(fmt.Sprintf("text:Money %d", g.World.Data.Level.Money-bolt.Cost))
	app.Find(fmt.Sprintf("text:Upgrade %d", bolt.Upgrades[0].Cost))
	app.Find(fmt.Sprintf("text:Sell %d", int(float64(bolt.Cost)*g.World.Data.Towers.Refund)))
	// The tower shows once the refusal's message has gone.
	app.Advance(time.Duration(messageSteps) * frame)
	app.Find("text:Bolt, level 1")

	k.key("x")
	if len(g.World.Towers) != 0 {
		t.Fatal("X did not sell the tower")
	}
	app.Find("text:Sell")
}

func TestKindButtonsAndKeysChooseTheTower(t *testing.T) {
	app, g := startGame(t)
	k := keyboardControls{app, g}
	k.key("e")
	if g.Kind != 1 {
		t.Fatalf("E chose kind %d, want 1", g.Kind)
	}
	k.key("q")
	k.key("q")
	if g.Kind != len(g.World.Data.Towers.Towers)-1 {
		t.Fatalf("Q twice from the first chose kind %d, want the last", g.Kind)
	}
	app.Press("tower-frost")
	app.Advance(frame)
	if g.Kind != 1 {
		t.Fatalf("the Frost button chose kind %d", g.Kind)
	}
	if !app.Find("tower-frost").Props.Selected {
		t.Fatal("the chosen tower's button is not selected")
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 800))
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}
