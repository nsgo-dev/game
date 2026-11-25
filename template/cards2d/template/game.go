package main

import (
	"errors"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// The input map's actions, which game.yaml binds to keys and gamepad buttons.
const (
	actionLeft    = "left"
	actionRight   = "right"
	actionPlay    = "play"
	actionDiscard = "discard"
	actionDraw    = "draw"
	actionDeal    = "deal"
)

// Root is the game's root NSGo component: the table on a Canvas stepped by a fixed-step game.Loop, the pointer dragging its cards, the keyboard and gamepads playing through the input map, and native views for the counts and buttons.
func Root(cx *core.Scope) core.Node { return root(cx, nil) }

// root builds the game for Root, handing it to started when that is set, as the tests do to look at the table.
func root(cx *core.Scope, started func(*cardGame)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	g, err := newCardGame(cx, cx.Storage())
	if err != nil {
		// rules.yaml and board.yaml are embedded, and the tests load them, so this only happens to a build whose data was edited without running them.
		panic(err)
	}
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)
	loop := &game.Loop{
		Rate:   60,
		Update: g.update,
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	loop.Run(cx)
	if started != nil {
		started(g)
	}
	return g.view()
}

// cardGame ties a table to the screen: the loop steps its cards' moves and the player's input, the Canvas draws it in the virtual resolution of game.yaml, and the game is saved after every move.
type cardGame struct {
	table    *table
	actions  *input.Actions
	viewport screen.Viewport
	// colors are the suits' colors, in the order of the rules' suits.
	colors []core.Color

	store   *save.Store
	records records
	// readOnly is set when a save slot was written by a newer version of the game, whose data this build must not save over.
	readOnly bool
	// recorded is set once the current game's win or loss has been counted.
	recorded bool

	// selected is the place in the hand of the card the keyboard and gamepad play or discard.
	selected int
	// size is the Canvas's size when it last drew, which pointers are mapped from.
	size core.Size
	// order is reused from frame to frame for the board's draw order.
	order []*cards.Card

	// frame changes on every Draw of the loop, so the Canvas that reads it draws again; state changes after every move and every change of the selection, so the native views that read it update.
	frame, state *core.Signal[uint64]
}

// newCardGame loads the rules and the table's layout, and resumes the saved game from storage or deals the next one.
func newCardGame(cx *core.Scope, storage core.Storage) (*cardGame, error) {
	rules, err := parseRules(rulesYAML)
	if err != nil {
		return nil, err
	}
	doc, err := cards.ParseBoardDoc(boardYAML)
	if err != nil {
		return nil, err
	}
	t, err := newTable(rules, doc)
	if err != nil {
		return nil, err
	}
	viewport, ok := Project.Viewport()
	if !ok {
		return nil, errors.New("game.yaml has no display, the virtual resolution board.yaml is laid out in")
	}
	g := &cardGame{
		table:    t,
		actions:  input.NewActions(),
		viewport: viewport,
		store:    newStore(storage),
		frame:    core.NewSignal(cx, uint64(0)),
		state:    core.NewSignal(cx, uint64(0)),
	}
	for _, s := range rules.Suits {
		c, _ := s.color()
		g.colors = append(g.colors, c)
	}
	Project.BindInput(g.actions)
	t.board.Curve = core.EaseOut
	t.board.MapPoint = func(p core.Point) (math2d.Vec2, bool) {
		q, ok := g.viewport.ViewToGame(g.size, p)
		return math2d.FromPoint(q), ok
	}
	t.board.OnTap = g.tap

	var newer bool
	g.records, newer = loadRecords(g.store)
	var saved tableSave
	err = g.store.Load(tableSlot, &saved)
	g.readOnly = newer || errors.Is(err, save.ErrFutureVersion)
	t.onMove = g.moved
	if err != nil || t.restore(saved) != nil {
		g.deal()
	}
	return g, nil
}

// deal starts the next deal of the records.
func (g *cardGame) deal() {
	n := g.records.NextDeal
	g.records.NextDeal++
	g.saveRecords()
	g.recorded = false
	g.selected = 0
	g.table.dealNew(n)
}

// newDeal starts the next deal, counting the current game as a loss when it is not over.
func (g *cardGame) newDeal() {
	if g.table.result == playing {
		g.records.Losses++
	}
	g.deal()
}

// moved saves the game after a move of the table, counts the game once it is over, and keeps the selection in the hand.
func (g *cardGame) moved() {
	t := g.table
	switch {
	case t.result == playing:
		if !g.readOnly {
			_ = g.store.Save(tableSlot, t.snapshot())
		}
	case !g.recorded:
		g.recorded = true
		if t.result == won {
			g.records.Wins++
		} else {
			g.records.Losses++
		}
		g.saveRecords()
		if !g.readOnly {
			_ = g.store.Delete(tableSlot)
		}
	}
	g.selected = min(max(g.selected, 0), max(t.hand.Len()-1, 0))
	g.changed()
}

func (g *cardGame) saveRecords() {
	if !g.readOnly {
		_ = g.store.Save(recordsSlot, g.records)
	}
}

// changed tells the native views that the table or the selection changed.
func (g *cardGame) changed() {
	if g.state != nil {
		g.state.Update(func(n uint64) uint64 { return n + 1 })
	}
}

// selectedCard returns the hand's selected card, or nil when the hand is empty.
func (g *cardGame) selectedCard() *cards.Card { return g.table.hand.Card(g.selected) }

// step moves the selection by d places along the hand, wrapping around at its ends.
func (g *cardGame) step(d int) {
	if n := g.table.hand.Len(); n > 0 {
		g.selected = ((g.selected+d)%n + n) % n
		g.changed()
	}
}

// tap is the board's OnTap: a card tapped in the hand climbs its ladder, or is selected when it cannot, and the draw pile tapped draws.
func (g *cardGame) tap(c *cards.Card) {
	switch c.Slot() {
	case g.table.hand:
		if !g.table.play(c) {
			g.selected = c.Index()
			g.changed()
		}
	case g.table.deck:
		g.table.draw()
	}
}

// update is one fixed step: the player's actions from the keyboard and gamepads, then the cards' moves.
func (g *cardGame) update(dt time.Duration) {
	a := g.actions
	a.Step()
	if a.JustPressed(actionLeft) {
		g.step(-1)
	}
	if a.JustPressed(actionRight) {
		g.step(1)
	}
	if a.JustPressed(actionPlay) {
		g.table.play(g.selectedCard())
	}
	if a.JustPressed(actionDiscard) {
		g.table.throw(g.selectedCard())
	}
	if a.JustPressed(actionDraw) {
		g.table.draw()
	}
	if a.JustPressed(actionDeal) {
		g.newDeal()
	}
	g.table.board.Update(dt)
}
