package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/cards"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// frame is one step of the game's 60 Hz loop; settle is long enough for a card to land after a move.
const (
	frame  = time.Second / 60
	settle = 250 * time.Millisecond
)

// startGame starts the game on storage holding entries, in a window the size of game.yaml's display, and returns it with the game it runs.
func startGame(t *testing.T, entries map[string]string, opts ...nsgotest.Option) (*nsgotest.App, *cardGame) {
	t.Helper()
	var g *cardGame
	opts = append([]nsgotest.Option{nsgotest.Size(360, 640), nsgotest.Storage(entries)}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return root(cx, func(c *cardGame) { g = c })
	}, opts...)
	app.Advance(2 * frame)
	if g == nil {
		t.Fatal("the game did not start")
	}
	return app, g
}

// controls is one way a player makes a move.
type controls interface {
	make(m move)
}

// keyControls plays with the keyboard keys game.yaml binds: the arrows select a card of the hand, Enter climbs with it, X throws it away and Space draws.
type keyControls struct {
	app *nsgotest.App
	g   *cardGame
}

func (k keyControls) press(key string) {
	k.app.Key(key)
	k.app.Advance(2 * frame)
}

func (k keyControls) make(m move) {
	if m.kind == moveDraw {
		k.press("space")
		return
	}
	for range k.g.table.hand.Capacity {
		if k.g.selectedCard() == m.card {
			break
		}
		k.press("right")
	}
	if m.kind == movePlay {
		k.press("enter")
	} else {
		k.press("x")
	}
}

// padControls plays with game controller 1: the D-pad selects, South climbs, West throws away and North draws.
type padControls struct {
	app *nsgotest.App
	g   *cardGame
}

func (p padControls) press(b gamepad.Button) {
	p.app.SetGamepadButton(1, b, 1)
	p.app.Advance(2 * frame)
	p.app.SetGamepadButton(1, b, 0)
	p.app.Advance(2 * frame)
}

func (p padControls) make(m move) {
	if m.kind == moveDraw {
		p.press(gamepad.ButtonNorth)
		return
	}
	for range p.g.table.hand.Capacity {
		if p.g.selectedCard() == m.card {
			break
		}
		p.press(gamepad.ButtonDPadLeft)
	}
	if m.kind == movePlay {
		p.press(gamepad.ButtonSouth)
	} else {
		p.press(gamepad.ButtonWest)
	}
}

// touchControls plays with a finger: it drags a card of the hand onto its ladder or onto the discard pile, taps the draw pile to draw, and presses the Draw button to turn the discard pile over.
type touchControls struct {
	app *nsgotest.App
	g   *cardGame
}

// at returns where a point of the table is in the view, the window's coordinates.
func (c touchControls) at(p math2d.Vec2) core.Point {
	return c.g.viewport.GameToView(c.g.size, p.Point())
}

func (c touchControls) drag(card *cards.Card, to math2d.Vec2) {
	// Take the card left of its middle, where the next card of the fan does not cover it.
	from := card.Pose().Pos.Add(math2d.Vec2{X: -12})
	f := c.app.Touch("id:table", c.at(from))
	for i := 1; i <= 4; i++ {
		f.Move(c.at(from.Lerp(to, float32(i)/4)))
	}
	f.Lift()
}

func (c touchControls) make(m move) {
	t := c.g.table
	c.app.Advance(settle)
	switch m.kind {
	case moveDraw:
		if t.deck.Len() == 0 {
			c.app.Press("role:button Draw")
			break
		}
		c.app.Touch("id:table", c.at(t.deck.Top().Pose().Pos)).Lift()
	case movePlay:
		c.drag(m.card, t.ladderFor(m.card).Rect.Center())
	case moveThrow:
		c.drag(m.card, t.discard.Rect.Center())
	}
	c.app.Advance(frame)
}

// playThrough plays the game shown to its end with strategy through ctl, checking after every move that the move was made, and returns how it ended.
func playThrough(t *testing.T, g *cardGame, ctl controls, strategy func(*table) (move, bool)) outcome {
	t.Helper()
	for range 2000 {
		m, ok := strategy(g.table)
		if !ok {
			return g.table.result
		}
		before := g.table.snapshot()
		ctl.make(m)
		if after := g.table.snapshot(); fmt.Sprint(after) == fmt.Sprint(before) {
			t.Fatalf("deal %d: %+v of %v changed nothing", g.table.deal, m, cardName(m.card))
		}
		checkConserved(t, g.table)
	}
	t.Fatal("the game did not end")
	return playing
}

func cardName(c *cards.Card) string {
	if c == nil {
		return "the draw pile"
	}
	return c.ID
}

func TestPlayThroughWinsAndLoses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  nsgotest.Option
		start func(*nsgotest.App)
		ctl   func(*nsgotest.App, *cardGame) controls
	}{
		{name: "keyboard", ctl: func(app *nsgotest.App, g *cardGame) controls { return keyControls{app: app, g: g} }},
		{name: "gamepad", start: func(app *nsgotest.App) {
			app.ConnectGamepad(1, "Xbox Wireless Controller")
			app.Advance(2 * frame)
		}, ctl: func(app *nsgotest.App, g *cardGame) controls { return padControls{app: app, g: g} }},
		// The game.yaml window is larger than the display, so the touches go through the viewport's scaling.
		{name: "touch", size: nsgotest.Size(450, 800), ctl: func(app *nsgotest.App, g *cardGame) controls { return touchControls{app: app, g: g} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts []nsgotest.Option
			if tc.size != nil {
				opts = append(opts, tc.size)
			}
			app, g := startGame(t, map[string]string{recordsKey: recordsJSON(t, records{NextDeal: winDeal})}, opts...)
			if tc.start != nil {
				tc.start(app)
			}
			ctl := tc.ctl(app, g)
			if len(app.Find("id:table-canvas").Ops) == 0 {
				t.Fatal("the table drew nothing")
			}
			if got := app.Find("id:deal").Text; got != fmt.Sprintf("Deal %d", winDeal) {
				t.Fatalf("deal = %q", got)
			}

			if got := playThrough(t, g, ctl, nextMove); got != won {
				t.Fatalf("deal %d ended %v, want won", winDeal, got)
			}
			app.Advance(settle)
			if got := app.Find("id:result-heading").Text; got != "Every ladder climbed" {
				t.Fatalf("result = %q", got)
			}
			if got := app.Find("id:record").Text; got != "Won 1 · Lost 0" {
				t.Fatalf("record = %q", got)
			}
			if _, ok := app.Storage().GetItem(tableKey); ok {
				t.Fatal("the finished game is still saved")
			}

			// The next deal is the losing one: play it out with the same controls.
			app.Press("role:button Next deal")
			app.Advance(settle)
			if app.Exists("id:result") || g.table.deal != loseDeal {
				t.Fatalf("after Next deal: result shown %v, deal %d", app.Exists("id:result"), g.table.deal)
			}
			if got := playThrough(t, g, ctl, nextMove); got != lost {
				t.Fatalf("deal %d ended %v, want lost", loseDeal, got)
			}
			app.Advance(settle)
			if got := app.Find("id:result-heading").Text; got != "No moves left" {
				t.Fatalf("result = %q", got)
			}
			if got := app.Find("id:record").Text; got != "Won 1 · Lost 1" {
				t.Fatalf("record = %q", got)
			}
			// No card moves once the game is over.
			if c := g.table.hand.Card(0); c != nil && (g.table.throw(c) || g.table.board.CanDrop(c, g.table.discard)) {
				t.Fatal("a card moved after the game was lost")
			}
		})
	}
}

// TestDealKeyStartsTheNextDealAndCountsALoss checks the deal action from the keyboard and the gamepad: a game abandoned before its end counts as a loss.
func TestDealKeyStartsTheNextDealAndCountsALoss(t *testing.T) {
	app, g := startGame(t, nil)
	if got := app.Find("id:deal").Text; got != "Deal 1" {
		t.Fatalf("first launch deal = %q, want Deal 1", got)
	}
	app.Key("n")
	app.Advance(2 * frame)
	if g.table.deal != 2 || app.Find("id:record").Text != "Won 0 · Lost 1" {
		t.Fatalf("after n: deal %d, record %q", g.table.deal, app.Find("id:record").Text)
	}
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(2 * frame)
	padControls{app: app, g: g}.press(gamepad.ButtonStart)
	if g.table.deal != 3 || app.Find("id:record").Text != "Won 0 · Lost 2" {
		t.Fatalf("after Start: deal %d, record %q", g.table.deal, app.Find("id:record").Text)
	}
}

// TestTapClimbsOrSelects checks that a tapped card of the hand climbs its ladder when it fits and is selected when it does not.
func TestTapClimbsOrSelects(t *testing.T) {
	app, g := startGame(t, map[string]string{recordsKey: recordsJSON(t, records{NextDeal: winDeal})})
	touch := touchControls{app: app, g: g}
	keys := keyControls{app: app, g: g}
	tb := g.table
	// Play on until the hand holds a card that fits and one that does not.
	var fits, other *cards.Card
	for range 100 {
		fits, other = nil, nil
		for _, c := range tb.hand.Cards() {
			if tb.fits(c) && fits == nil {
				fits = c
			} else if !tb.fits(c) && other == nil {
				other = c
			}
		}
		if fits != nil && other != nil {
			break
		}
		m, _ := nextMove(tb)
		keys.make(m)
	}
	if fits == nil || other == nil {
		t.Fatalf("deal %d never held both a card that fits and one that does not", winDeal)
	}
	app.Advance(settle)
	app.Touch("id:table", touch.at(other.Pose().Pos.Add(math2d.Vec2{X: -12}))).Lift()
	app.Advance(frame)
	if g.selectedCard() != other || other.Slot() != tb.hand {
		t.Fatalf("tapping %s did not select it", other.ID)
	}
	app.Touch("id:table", touch.at(fits.Pose().Pos.Add(math2d.Vec2{X: -12}))).Lift()
	app.Advance(frame)
	if fits.Slot() != tb.ladderFor(fits) {
		t.Fatalf("tapping %s left it in %s", fits.ID, fits.Slot().ID)
	}
}

// TestRefusedDropFliesBack drags a card onto a ladder it does not fit and checks that it goes back to the hand.
func TestRefusedDropFliesBack(t *testing.T) {
	app, g := startGame(t, map[string]string{recordsKey: recordsJSON(t, records{NextDeal: winDeal})})
	touch := touchControls{app: app, g: g}
	tb := g.table
	for _, c := range tb.hand.Cards() {
		if !tb.fits(c) {
			touch.drag(c, tb.ladderFor(c).Rect.Center())
			app.Advance(settle)
			if c.Slot() != tb.hand {
				t.Fatalf("%s dropped on %s", c.ID, c.Slot().ID)
			}
			return
		}
	}
	t.Fatalf("every card of deal %d's opening hand fits", winDeal)
}

func TestDrawButtonRedeals(t *testing.T) {
	app, g := startGame(t, map[string]string{recordsKey: recordsJSON(t, records{NextDeal: winDeal})})
	keys := keyControls{app: app, g: g}
	tb := g.table
	for tb.deck.Len() > 0 {
		keys.make(move{kind: moveThrow, card: tb.hand.Card(0)})
		keys.make(move{kind: moveDraw})
	}
	keys.make(move{kind: moveThrow, card: tb.hand.Card(0)})
	if got := app.Find("id:draw-text").Text; got != fmt.Sprintf("Redeal (%d)", tb.rules.Redeals) {
		t.Fatalf("Draw button reads %q on an empty draw pile", got)
	}
	app.Press("role:button Draw")
	if tb.redeals != tb.rules.Redeals-1 || tb.deck.Len() == 0 || tb.hand.Len() != tb.hand.Capacity {
		t.Fatalf("after Redeal: %d redeals, deck %d, hand %d", tb.redeals, tb.deck.Len(), tb.hand.Len())
	}
	if got := app.Find("id:piles").Text; got != fmt.Sprintf("Draw pile %d · Redeals %d", tb.deck.Len(), tb.redeals) {
		t.Fatalf("piles = %q", got)
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(360, 640))
	if app.Orientation != core.OrientationPortrait {
		t.Fatalf("orientation = %v, want portrait (%v)", app.Orientation, core.OrientationPortrait)
	}
}
