package main

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/random"
)

// boardYAML lays the table out in the virtual resolution game.yaml's display sets: a ladder per suit along the top, the draw pile and the discard pile below them, and the hand fanned out at the bottom. Move, resize or restyle the piles here; the slot IDs are the ones table.go looks up.
//
//go:embed board.yaml
var boardYAML []byte

// The IDs of the slots board.yaml must hold besides one ladder per suit, named ladderPrefix followed by the suit's name.
const (
	deckSlot     = "deck"
	discardSlot  = "discard"
	handSlot     = "hand"
	ladderPrefix = "ladder-"
)

// outcome is how the current game stands.
type outcome uint8

const (
	// playing is a game with moves left.
	playing outcome = iota
	// won is a game whose ladders are all complete.
	won
	// lost is a game with no move left that can climb a ladder: the draw pile is empty, it cannot be dealt again and no card in the hand fits a ladder.
	lost
)

// face is what a card shows: its suit, an index into Rules.Suits, and its rank, from 1 to Rules.Ranks. It is the Data of every card on the board.
type face struct {
	suit, rank int
}

// table is one game of Suit Ladders: the rules, kept on a cards.Board whose slots hold every card, so the board is the only record of where each card is.
//
// The rules: each suit has a ladder that climbs from 1 to Rules.Ranks in order, one card at a time. Cards come from the draw pile into the hand, which holds as many as its slot's capacity; a card in the hand can climb its suit's ladder when it is the next rank there, or be thrown on the discard pile to make room. When the draw pile is empty, drawing turns the discard pile over into a new draw pile, at most Rules.Redeals times a game. A game is won when every ladder is complete and lost when no card can climb any more.
type table struct {
	rules *Rules
	board *cards.Board

	deck, discard, hand *cards.Slot
	// ladders are the ladders' slots, in the order of rules.Suits.
	ladders []*cards.Slot
	// all holds every card of the deck, in suit then rank order.
	all []*cards.Card

	// deal is the number of the game being played: deal n always shuffles the same way.
	deal int
	// redeals is how many times the discard pile can still be turned over.
	redeals int
	// result is how the game stands, which moved sets after every move.
	result outcome

	// onMove runs after every move that changes the table, including the dealing of a new game.
	onMove func()
}

// newTable lays out the board from doc and makes the deck's cards for rules, checking that doc has the slots the rules need: a deck, a discard pile, a hand with a capacity, and a ladder for every suit that takes only that suit's cards.
func newTable(rules *Rules, doc *cards.BoardDoc) (*table, error) {
	b, err := cards.LoadBoard(doc)
	if err != nil {
		return nil, err
	}
	t := &table{rules: rules, board: b}
	for _, id := range []string{deckSlot, discardSlot, handSlot} {
		if b.Slot(id) == nil {
			return nil, fmt.Errorf("board.yaml has no %q slot", id)
		}
	}
	t.deck, t.discard, t.hand = b.Slot(deckSlot), b.Slot(discardSlot), b.Slot(handSlot)
	if t.hand.Capacity <= 0 {
		return nil, errors.New("board.yaml: the hand slot needs a capacity, the most cards a hand holds")
	}
	if t.hand.Capacity > rules.cardCount() {
		return nil, fmt.Errorf("board.yaml: the hand holds %d cards but rules.yaml's deck has only %d", t.hand.Capacity, rules.cardCount())
	}
	for _, s := range rules.Suits {
		l := b.Slot(ladderPrefix + s.Name)
		if l == nil {
			return nil, fmt.Errorf("board.yaml has no %q slot for the %s suit", ladderPrefix+s.Name, s.Name)
		}
		if !slices.Equal(l.Accepts, []string{s.Name}) {
			return nil, fmt.Errorf("board.yaml: slot %q must accept exactly [%s]", l.ID, s.Name)
		}
		t.ladders = append(t.ladders, l)
	}
	if n := 3 + len(rules.Suits); len(b.Slots()) != n {
		return nil, fmt.Errorf("board.yaml has %d slots, want %d: a deck, a discard pile, a hand and a ladder per suit", len(b.Slots()), n)
	}
	for si, s := range rules.Suits {
		for rank := 1; rank <= rules.Ranks; rank++ {
			t.all = append(t.all, &cards.Card{ID: cardID(s.Name, rank), Tags: []string{s.Name}, Data: face{suit: si, rank: rank}})
		}
	}
	b.Accept = t.allows
	b.OnDrop = func(cards.DropEvent) { t.moved() }
	return t, nil
}

// cardID names the card of suit and rank, as saves record it: "flame-3".
func cardID(suit string, rank int) string { return suit + "-" + strconv.Itoa(rank) }

// faceOf returns what c shows.
func faceOf(c *cards.Card) face { return c.Data.(face) }

// dealSeed returns the seed deal n shuffles with: every player's deal n is the same game.
func dealSeed(n int) uint64 { return uint64(n) }

// dealNew starts game number n: the whole deck shuffled onto the draw pile, a full hand drawn from it and every redeal left.
func (t *table) dealNew(n int) {
	order := slices.Clone(t.all)
	random.Shuffle(random.New(dealSeed(n)), order)
	hand := t.hand.Capacity
	t.place(n, t.rules.Redeals, order[hand:], order[:hand], nil, make([][]*cards.Card, len(t.ladders)))
}

// place clears the board and puts the given cards on it at once, without tweening: deck and discard bottom first, hand left to right, and each ladder from its 1 up.
func (t *table) place(deal, redeals int, deck, hand, discard []*cards.Card, ladders [][]*cards.Card) {
	for _, s := range t.board.Slots() {
		for s.Len() > 0 {
			_ = t.board.Remove(s.Top())
		}
	}
	add := func(s *cards.Slot, cs []*cards.Card) {
		for _, c := range cs {
			_ = t.board.Add(c, s, -1)
		}
	}
	add(t.deck, deck)
	add(t.hand, hand)
	add(t.discard, discard)
	for i, l := range ladders {
		add(t.ladders[i], l)
	}
	t.board.Settle()
	t.deal, t.redeals = deal, redeals
	t.moved()
}

// allows is the board's Accept: the rules for a card a pointer drops onto slot to from slot from. A card climbs its ladder from the hand when it is the next rank there, the discard pile takes any card from the hand, and the hand takes the top card of the draw pile while it has room; nothing moves once the game is over.
func (t *table) allows(c *cards.Card, from, to *cards.Slot) bool {
	if t.result != playing {
		return false
	}
	switch {
	case from == t.hand && to == t.discard:
		return true
	case from == t.hand:
		return to == t.ladderFor(c) && t.fits(c)
	case from == t.deck:
		return to == t.hand
	}
	return false
}

// ladderFor returns the ladder of c's suit.
func (t *table) ladderFor(c *cards.Card) *cards.Slot { return t.ladders[faceOf(c).suit] }

// fits reports whether c is the next card its ladder takes.
func (t *table) fits(c *cards.Card) bool { return faceOf(c).rank == t.ladderFor(c).Len()+1 }

// canDraw reports whether draw would move a card: the game is on, the hand has room, and the draw pile has a card or the discard pile can be turned over into one.
func (t *table) canDraw() bool {
	if t.result != playing || t.hand.Len() >= t.hand.Capacity {
		return false
	}
	return t.deck.Len() > 0 || t.canRedeal()
}

// canRedeal reports whether the discard pile can be turned over now: the draw pile is empty, it holds cards and a redeal is left.
func (t *table) canRedeal() bool {
	return t.deck.Len() == 0 && t.discard.Len() > 0 && t.redeals > 0
}

// draw moves the top card of the draw pile to the end of the hand, first turning the discard pile over when the draw pile is empty, and reports whether it did.
func (t *table) draw() bool {
	if !t.canDraw() {
		return false
	}
	if t.deck.Len() == 0 {
		// Turning the pile over puts the first card discarded on top of the new draw pile.
		for t.discard.Len() > 0 {
			_ = t.board.Move(t.discard.Top(), t.deck, -1)
		}
		t.redeals--
	}
	_ = t.board.Move(t.deck.Top(), t.hand, -1)
	t.moved()
	return true
}

// play moves c from the hand onto its ladder, and reports whether the rules let it.
func (t *table) play(c *cards.Card) bool {
	if c == nil || c.Slot() != t.hand || !t.allows(c, t.hand, t.ladderFor(c)) {
		return false
	}
	_ = t.board.Move(c, t.ladderFor(c), -1)
	t.moved()
	return true
}

// throw moves c from the hand onto the discard pile, and reports whether the rules let it.
func (t *table) throw(c *cards.Card) bool {
	if c == nil || c.Slot() != t.hand || !t.allows(c, t.hand, t.discard) {
		return false
	}
	_ = t.board.Move(c, t.discard, -1)
	t.moved()
	return true
}

// moved works out how the game stands after a move and tells onMove.
func (t *table) moved() {
	t.result = t.standing()
	if t.onMove != nil {
		t.onMove()
	}
}

// standing returns how the game stands now.
func (t *table) standing() outcome {
	return t.judge(func(i int) int { return t.ladders[i].Len() }, t.deck.Len(), t.redeals, t.hand.Cards())
}

// judge returns how a game stands whose ladder i holds ladderLen(i) cards, whose draw pile holds deckLen, with redeals left and hand in the hand: won with every ladder complete, lost when the draw pile is empty and cannot be dealt again and no card of the hand is the next rank of its ladder, and playing otherwise.
func (t *table) judge(ladderLen func(i int) int, deckLen, redeals int, hand []*cards.Card) outcome {
	complete := true
	for i := range t.ladders {
		complete = complete && ladderLen(i) == t.rules.Ranks
	}
	if complete {
		return won
	}
	if deckLen > 0 || redeals > 0 {
		return playing
	}
	for _, c := range hand {
		if f := faceOf(c); f.rank == ladderLen(f.suit)+1 {
			return playing
		}
	}
	return lost
}
