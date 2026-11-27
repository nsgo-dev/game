package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// step is one fixed step of the game loop.
const step = time.Second / stepRate

// fastForward is how many times as fast as real time the playthroughs run the skirmish.
const fastForward = 4

// startGame starts Root in a 960 by 640 window and returns the app and its skirmish.
func startGame(t *testing.T) (*nsgotest.App, *rootSkirmish) {
	t.Helper()
	return startGameAt(t, 1)
}

// startGameAt starts Root as startGame does with the game loop running speed times as fast, so a whole skirmish plays in fewer display frames; every step still reads the input as it does at speed 1.
func startGameAt(t *testing.T, speed float64) (*nsgotest.App, *rootSkirmish) {
	t.Helper()
	var g *rootSkirmish
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return rootWith(cx, func(s *rootSkirmish) { g = s })
	}, nsgotest.Size(960, 640))
	if g == nil {
		t.Fatal("Root did not start a skirmish")
	}
	g.loop.TimeScale = speed
	g.loop.MaxSteps = int(speed) + 1
	app.Advance(3 * step)
	return app, g
}

// press taps key and lets two steps pass, one that reads it pressed and one that reads it released, so keys pressed one after another reach the game in that order and the same key twice counts twice.
func press(app *nsgotest.App, key string) {
	app.Key(key)
	app.Advance(2 * step)
}

// chord taps key with modifier held.
func chord(app *nsgotest.App, modifier, key string) {
	app.KeyDown(modifier)
	app.Advance(step)
	press(app, key)
	app.KeyUp(modifier)
	app.Advance(step)
}

// gameViewport returns game.yaml's virtual resolution, as Root uses it.
func gameViewport() screen.Viewport {
	v, _ := Project.Viewport()
	return v
}

// rightClick gives a command at world point p with the mouse.
func rightClick(app *nsgotest.App, g *rootSkirmish, p math2d.Vec2) {
	at := g.toView(p)
	app.MouseDownAt("id:battlefield", at, core.MouseButtonRight)
	app.MouseUpAt("id:battlefield", at, core.MouseButtonRight)
}

// boxSelect drags a box from world point from to world point to with the left mouse button.
func boxSelect(app *nsgotest.App, g *rootSkirmish, from, to math2d.Vec2) {
	app.MouseDownAt("id:battlefield", g.toView(from), core.MouseButtonLeft)
	app.HoverAt("id:battlefield", g.toView(from.Lerp(to, 0.5)))
	app.HoverAt("id:battlefield", g.toView(to))
	app.MouseUpAt("id:battlefield", g.toView(to), core.MouseButtonLeft)
}

// army returns how many combat units t has.
func army(w *world, t team) int {
	n := 0
	for _, e := range w.ents {
		if e.team == t && e.fights() {
			n++
		}
	}
	return n
}

// TestPlaythroughWin plays a whole skirmish with the keyboard and mouse as a player would: grow the economy, build barracks, train an army, hold the base against the opponent's attacks, and once fourteen strong group the army and send it at the opponent's headquarters until it falls.
func TestPlaythroughWin(t *testing.T) {
	app, g := startGameAt(t, fastForward)
	w := g.w
	app.Find("text:Crystals 100")
	enemyHQ := w.buildingCenter(w.sides[opponent].hq)
	grouped := false
	for second := 0; second < 400 && !w.over; second++ {
		// Keep the headquarters training workers up to seven, build both barracks once six are mining, then keep the barracks training rangers and soldiers in turn.
		if w.countUnits(player, w.r.worker()) < 7 && len(w.sides[player].hq.queue) == 0 {
			press(app, "q")
		}
		if w.countUnits(player, w.r.worker()) >= 6 {
			press(app, "b")
		}
		if len(w.buildings(player, w.r.building(barracksName))) == 2 {
			if second%2 == 0 {
				press(app, "e")
			} else {
				press(app, "w")
			}
		}
		if army(w, player) >= 14 {
			grouped = true
		}
		if grouped && second%10 == 0 {
			// Select the whole army with its reinforcements, bind it to control group 1, clear the selection, recall the group with its key and send it at the opponent's headquarters.
			press(app, "a")
			chord(app, "ctrl", "1")
			press(app, "escape")
			if g.sel.Len() != 0 {
				t.Fatal("Escape did not clear the selection")
			}
			press(app, "1")
			if n := len(g.groups.Group(1)); g.sel.Len() == 0 || g.sel.Len() != n {
				t.Fatalf("1 recalled %d units, want group 1's %d", g.sel.Len(), n)
			}
			rightClick(app, g, enemyHQ)
		}
		app.Advance(time.Second / fastForward)
	}
	if !w.over || w.winner != player {
		t.Fatalf("the skirmish did not end in a win: over=%v winner=%d after %d steps", w.over, w.winner, w.steps)
	}
	app.Find("text:Victory")
	app.Press("id:overlay-action")
	app.Advance(2 * step)
	if g.w.over || app.Exists("id:overlay") {
		t.Fatal("Play again did not start a new skirmish")
	}
	app.Find("text:Crystals 100")
}

// TestPlaythroughLoss leaves the base to itself: the opponent builds up, attacks and destroys the player's headquarters, and Enter starts again.
func TestPlaythroughLoss(t *testing.T) {
	app, g := startGameAt(t, fastForward)
	for range 300 {
		if g.w.over {
			break
		}
		app.Advance(time.Second / fastForward)
	}
	if !g.w.over || g.w.winner != opponent {
		t.Fatalf("the opponent did not win: over=%v winner=%d", g.w.over, g.w.winner)
	}
	app.Find("text:Defeat")
	app.Key("enter")
	app.Advance(2 * step)
	if g.w.over || app.Exists("id:overlay") {
		t.Fatal("Enter on the result screen did not start a new skirmish")
	}
}
