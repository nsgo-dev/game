package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/idle"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// frame is one fixed step of the game loop.
const frame = time.Second / 60

// startGame starts the game headless and returns it with its state.
func startGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *hearthGame) {
	t.Helper()
	var g *hearthGame
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return rootWith(cx, func(game *hearthGame) { g = game })
	}, opts...)
	app.Advance(2 * frame)
	return app, g
}

// widgetBounds returns where the shop places w for the hearth's state now, which a test may have changed since the last frame.
func widgetBounds(g *hearthGame, w gameui.Widget) core.Bounds {
	g.shop.refresh()
	g.shop.layout()
	return w.Bounds()
}

// tap touches the middle of the shop widget w with one finger and lifts it.
func tap(app *nsgotest.App, g *hearthGame, w gameui.Widget) {
	b := widgetBounds(g, w)
	p := core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
	app.Touch("hearth", g.viewport.GameToView(g.canvasSize, p)).Lift()
	app.Advance(frame)
}

// click clicks the middle of the shop widget w with the mouse.
func click(app *nsgotest.App, g *hearthGame, w gameui.Widget) {
	b := widgetBounds(g, w)
	p := core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
	app.PressAt("hearth", g.viewport.GameToView(g.canvasSize, p))
	app.Advance(frame)
}

// key presses and releases a key and lets the loop see it.
func key(app *nsgotest.App, k string) {
	app.Key(k)
	app.Advance(2 * frame)
}

// pad presses and releases button b on controller 1 across a few fixed steps.
func pad(app *nsgotest.App, b gamepad.Button) {
	app.SetGamepadButton(1, b, 1)
	app.Advance(2 * frame)
	app.SetGamepadButton(1, b, 0)
	app.Advance(2 * frame)
}

// goAway sends the app to the background for d, as closing a phone app does, and brings it back.
func goAway(app *nsgotest.App, d time.Duration) {
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.FastForward(d)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(2 * frame)
}

// text returns the text of the view with the test ID id.
func text(app *nsgotest.App, id string) string { return app.Find(id).Text }

// shopAll taps every affordable thing in the shop, cheapest generator last, until nothing is affordable, as a player making the most of their embers does.
func shopAll(t *testing.T, app *nsgotest.App, g *hearthGame) {
	t.Helper()
	for range 1000 {
		s, h := g.shop, g.h
		s.refresh()
		s.layout()
		var next gameui.Widget
		fuel := nextFuelUpgrade(h)
		switch {
		case !s.goal.Disabled:
			next = s.goal
		case fuel >= 0 && h.netFuel() < 0:
			// Save for the next fuel upgrade while the fire burns faster than it is fed.
			for k := range s.upgrades {
				if s.offered[k] == fuel && s.upgrades[k].affordable {
					next = s.upgrades[k].buy
				}
			}
			if next == nil {
				return
			}
		default:
			for k := range s.upgrades {
				if s.offered[k] >= 0 && s.upgrades[k].affordable {
					next = s.upgrades[k].buy
					break
				}
			}
		}
		if next == nil {
			cheapest := -1
			for i := range s.gens {
				if !s.gens[i].row.Hidden && (cheapest < 0 || h.generatorCost(i).Cmp(h.generatorCost(cheapest)) < 0) {
					cheapest = i
				}
			}
			if !s.gens[cheapest].affordable {
				return
			}
			next = s.gens[cheapest].buy
		}
		if tap(app, g, next); g.shown.Get() != overlayNone {
			return
		}
	}
	t.Fatal("the shop never ran out of affordable purchases")
}

// TestPlaythroughReachesTheWin plays the game to the Everflame with touch alone: tapping the hearth, buying, leaving for the offline cap, coming back and scattering the ashes when that at least doubles them.
func TestPlaythroughReachesTheWin(t *testing.T) {
	app, g := startGame(t)
	if got := text(app, "embers"); got != "0 embers" {
		t.Fatalf("a new game shows %q", got)
	}
	for session := 1; session <= 100; session++ {
		for range 15 {
			tap(app, g, g.shop.flame)
			app.Advance(time.Second)
			if shopAll(t, app, g); g.h.won {
				break
			}
		}
		if g.h.cold {
			t.Fatalf("session %d: the fire went out during active play", session)
		}
		if g.h.won {
			t.Logf("won in session %d after %v of play and %d runs", session, g.h.played, g.h.runs+1)
			if got, want := text(app, "overlay-title"), "The Everflame burns"; got != want {
				t.Fatalf("overlay %q after the win, want %q", got, want)
			}
			app.Press("overlay-button")
			app.Advance(2 * frame)
			if app.Exists("overlay") || g.shop.goal.Text != "Everflame lit" {
				t.Fatalf("Play on left overlay %v and goal %q", app.Exists("overlay"), g.shop.goal.Text)
			}
			return
		}
		if g.h.netFuel() < 0 || g.h.rate().IsZero() {
			continue // play on until the fire feeds itself, or it goes out while away, and makes embers
		}
		if gain := g.h.ashGain(); gain.Cmp(g.h.ash.Max(idle.New(1))) >= 0 {
			ash := g.h.ash.Add(gain)
			tap(app, g, g.shop.prestige)
			if g.h.ash.Cmp(ash) != 0 || !g.h.embers.IsZero() {
				t.Fatalf("session %d: scattering the ashes left ash %v and embers %v, want %v and 0", session, g.h.ash, g.h.embers, ash)
			}
			continue
		}
		before := g.h.embers
		goAway(app, 8*time.Hour)
		if got := text(app, "overlay-title"); got != "Welcome back" {
			t.Fatalf("session %d: overlay %q after 8h away", session, got)
		}
		if g.h.embers.Cmp(before) <= 0 {
			t.Fatalf("session %d: 8h away earned nothing", session)
		}
		app.Press("overlay-button")
		app.Advance(2 * frame)
	}
	t.Fatalf("no win in 100 sessions: embers %s, ash %v, overlay %v, offered %v, bought %v, owned %v, cold %v", text(app, "embers"), g.h.ash, g.shown.Get(), g.shop.offered, g.h.bought, g.h.owned, g.h.cold)
}

// TestUnfedFireIsALoss lets a new fire burn out while the player watches, then rekindles it.
func TestUnfedFireIsALoss(t *testing.T) {
	app, g := startGame(t)
	click(app, g, g.shop.flame)
	g.h.ash = idle.New(2)
	app.Advance(100 * time.Second)
	if !g.h.cold {
		t.Fatalf("the fire still burns with %v fuel after 100s unfed", g.h.fuel)
	}
	if got, want := text(app, "overlay-title"), "The hearth went cold"; got != want {
		t.Fatalf("overlay %q, want %q", got, want)
	}
	app.Press("overlay-button")
	app.Advance(2 * frame)
	if g.h.cold || app.Exists("overlay") || g.h.fuel < g.h.data.Fuel.Capacity-1 || g.h.ash.Cmp(idle.New(2)) != 0 || g.h.runs != 1 {
		t.Fatalf("Rekindle: cold %v, overlay %v, fuel %v, ash %v, runs %d", g.h.cold, app.Exists("overlay"), g.h.fuel, g.h.ash, g.h.runs)
	}
}

func TestKeyboardPlays(t *testing.T) {
	app, g := startGame(t)
	for range 10 {
		key(app, "space")
	}
	if got := text(app, "embers"); got != "10 embers" {
		t.Fatalf("ten Spaces gathered %q", got)
	}
	// Focus starts on the hearth; Down moves it to the spark's price and Enter buys one.
	key(app, "down")
	if g.shop.root.Focused() != g.shop.gens[0].buy {
		t.Fatalf("Down focused %v, want the spark's button", g.shop.root.Focused())
	}
	key(app, "enter")
	if g.h.owned[0] != 1 || g.h.embers.Cmp(idle.New(1)) >= 0 {
		t.Fatalf("Enter on the spark: %d owned, %v embers left", g.h.owned[0], g.h.embers)
	}
	// Up and Enter stoke from the hearth too.
	key(app, "up")
	key(app, "enter")
	if g.h.embers.Cmp(idle.New(0.5)) < 0 {
		t.Fatalf("Enter on the hearth did not stoke: %v embers", g.h.embers)
	}
	// N switches the notation, and Escape closes the away report.
	key(app, "n")
	if got := g.format.Notation; got != idle.Letters || !app.Exists("text:letters") {
		t.Fatalf("N switched the notation to %q", got)
	}
	g.h.embers = idle.New(1e6)
	for j, u := range g.h.data.Upgrades {
		if u.Effect == effectFuel {
			g.h.buyUpgrade(j)
		}
	}
	goAway(app, 2*time.Hour)
	if !app.Exists("overlay") {
		t.Fatal("no away report after 2h away")
	}
	key(app, "escape")
	if app.Exists("overlay") {
		t.Fatal("Escape did not close the away report")
	}
}

// TestCardsFollowTheGame checks that the hearth waits under a card, that a card changes when the game does, and that only Enter or Escape closes it: the win card turns into the loss when the fire goes out while away, Space leaves it up and Enter rekindles.
func TestCardsFollowTheGame(t *testing.T) {
	app, g := startGame(t)
	g.h.embers = g.h.data.Goal.Cost
	g.buyGoal()
	app.Advance(2 * frame)
	if got, want := text(app, "overlay-title"), "The Everflame burns"; got != want {
		t.Fatalf("overlay %q after the win, want %q", got, want)
	}
	fuel := g.h.fuel
	app.Advance(200 * time.Second)
	if g.h.cold || g.h.fuel != fuel {
		t.Fatalf("under the win card the fire burned from %v to %v fuel (cold %v)", fuel, g.h.fuel, g.h.cold)
	}

	goAway(app, time.Hour)
	if got, want := text(app, "overlay-title"), "The hearth went cold"; got != want {
		t.Fatalf("after an hour away with an unfed fire the card reads %q, want %q", got, want)
	}
	if got := text(app, "overlay-detail"); !strings.Contains(got, "while you were away") {
		t.Fatalf("the loss card's detail is %q", got)
	}
	key(app, "space")
	if !g.h.cold || !app.Exists("overlay") {
		t.Fatalf("Space on the loss card: cold %v, overlay %v", g.h.cold, app.Exists("overlay"))
	}
	key(app, "enter")
	if g.h.cold || app.Exists("overlay") || g.h.runs != 1 {
		t.Fatalf("Enter did not rekindle: cold %v, overlay %v, runs %d", g.h.cold, app.Exists("overlay"), g.h.runs)
	}
}

func TestGamepadPlays(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Test Pad")
	for range 10 {
		pad(app, gamepad.ButtonWest)
	}
	if got := text(app, "embers"); got != "10 embers" {
		t.Fatalf("ten West presses gathered %q", got)
	}
	pad(app, gamepad.ButtonDPadDown)
	pad(app, gamepad.ButtonSouth)
	if g.h.owned[0] != 1 {
		t.Fatalf("D-pad down and South bought %d sparks, want 1", g.h.owned[0])
	}
	pad(app, gamepad.ButtonNorth)
	if got := g.format.Notation; got != idle.Letters || !app.Exists("text:letters") {
		t.Fatalf("North switched the notation to %q", got)
	}

	// Leaving with an unfed fire loses the run while away; South rekindles it.
	goAway(app, time.Hour)
	if got := text(app, "overlay-title"); got != "The hearth went cold" {
		t.Fatalf("after an hour away with an unfed fire the overlay is %q", got)
	}
	pad(app, gamepad.ButtonSouth)
	if g.h.cold || app.Exists("overlay") || g.h.owned[0] != 0 {
		t.Fatalf("South did not rekindle: cold %v, overlay %v, sparks %d", g.h.cold, app.Exists("overlay"), g.h.owned[0])
	}
}

func TestTouchAndMousePlay(t *testing.T) {
	app, g := startGame(t)
	for range 5 {
		tap(app, g, g.shop.flame)
		click(app, g, g.shop.flame)
	}
	if got := text(app, "embers"); got != "10 embers" {
		t.Fatalf("ten taps and clicks on the hearth gathered %q", got)
	}
	tap(app, g, g.shop.gens[0].buy)
	if g.h.owned[0] != 1 || g.shop.gens[1].row.Hidden {
		t.Fatalf("tapping the spark's price: %d owned, kindling hidden %v", g.h.owned[0], g.shop.gens[1].row.Hidden)
	}
	tap(app, g, g.shop.gens[0].buy)
	if g.h.owned[0] != 1 {
		t.Fatal("tapping an unaffordable price bought it")
	}
	// While an overlay shows, the canvas under it takes no presses.
	g.h.embers = g.h.data.Goal.Cost
	g.h.owned[0] = 0
	tap(app, g, g.shop.goal)
	if !g.h.won || !app.Exists("overlay") {
		t.Fatalf("tapping the affordable goal: won %v, overlay %v", g.h.won, app.Exists("overlay"))
	}
	tap(app, g, g.shop.flame)
	if !g.h.embers.IsZero() {
		t.Fatalf("a tap on the hearth under the overlay gathered %v embers", g.h.embers)
	}
	app.Press("overlay-button")
	app.Advance(2 * frame)

	app.Press("notation")
	app.Advance(2 * frame)
	if got := g.format.Notation; got != idle.Letters || !app.Exists("text:letters") {
		t.Fatalf("the notation button switched to %q", got)
	}
}

// TestSaveKeepsTheGameAndCreditsTheTimeAway closes the game, opens it again three hours later with the same storage and checks it carries on where it was, with the time away credited.
func TestSaveKeepsTheGameAndCreditsTheTimeAway(t *testing.T) {
	app, g := startGame(t)
	g.h.embers = idle.New(5000)
	for j, u := range g.h.data.Upgrades {
		if u.Effect == effectFuel {
			g.h.buyUpgrade(j)
		}
	}
	g.h.buyGenerator(1)
	g.h.ash = idle.NewExp(1.5, 400)
	key(app, "n")
	key(app, "n")
	embers, rate := g.h.embers, g.h.rate()
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	closed := app.Clock.WallTime()

	entries := map[string]string{}
	storage := app.Storage()
	for _, k := range storage.Keys() {
		entries[k], _ = storage.GetItem(k)
	}
	app2, g2 := startGame(t, nsgotest.Storage(entries), nsgotest.WallTime(closed.Add(3*time.Hour)))
	if g2.h.owned[1] != 1 || g2.h.ash.Cmp(g.h.ash) != 0 || g2.format.Notation != idle.Scientific || g2.h.netFuel() <= 0 {
		t.Fatalf("reloaded: kindling %d, ash %v, notation %v, net fuel %v", g2.h.owned[1], g2.h.ash, g2.format.Notation, g2.h.netFuel())
	}
	want := embers.Add(rate.Scale(3 * 3600 * g.h.data.Offline.Efficiency))
	if diff := g2.h.embers.Add(want.Neg()).Abs(); diff.Cmp(want.Scale(0.01)) > 0 {
		t.Fatalf("after 3h away: %v embers, want about %v", g2.h.embers, want)
	}
	if got := text(app2, "overlay-title"); got != "Welcome back" {
		t.Fatalf("overlay %q on reopening", got)
	}
	if got := text(app2, "ash"); got != "1.5e400 ash · +1.5e401%" {
		t.Fatalf("ash shows %q", got)
	}
}

// TestUnreadableSaveIsNotOverwritten starts on a save that does not decode and checks the game runs without saving over it.
func TestUnreadableSaveIsNotOverwritten(t *testing.T) {
	app, _ := startGame(t, nsgotest.Storage(map[string]string{savePrefix + ".slot." + saveSlot: "{not json"}))
	if !app.Exists("problem") {
		t.Fatal("no message about the unreadable save")
	}
	key(app, "space")
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	if got, _ := app.Storage().GetItem(savePrefix + ".slot." + saveSlot); got != "{not json" {
		t.Fatalf("the unreadable save was overwritten with %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		42 * time.Second:                  "42s",
		5*time.Minute + 3*time.Second:     "5m",
		8 * time.Hour:                     "8h 0m",
		26*time.Hour + 14*time.Minute + 1: "26h 14m",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(420, 720))
	if app.Orientation != core.OrientationPortrait {
		t.Fatalf("orientation = %v, want portrait (%v)", app.Orientation, core.OrientationPortrait)
	}
}
