package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/random"
)

// Deals the tests play: a careful player, as nextMove plays, wins winDeal and runs out of moves in loseDeal.
const (
	winDeal  = 1
	loseDeal = 2
)

// newTestTable returns a table from the embedded rules and layout, with deal n dealt.
func newTestTable(t testing.TB, n int) *table {
	t.Helper()
	rules, err := parseRules(rulesYAML)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := cards.ParseBoardDoc(boardYAML)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := newTable(rules, doc)
	if err != nil {
		t.Fatal(err)
	}
	tb.dealNew(n)
	return tb
}

// moveKind is a move a player makes.
type moveKind uint8

const (
	movePlay moveKind = iota
	moveThrow
	moveDraw
)

// move is a move and the card of the hand it takes, nil for a draw.
type move struct {
	kind moveKind
	card *cards.Card
}

// nextMove is the move a careful player makes: climb with the lowest card of the hand that fits, else draw, else throw away the card furthest from its turn. It reports false when the game is over.
func nextMove(t *table) (move, bool) {
	if t.result != playing {
		return move{}, false
	}
	var best *cards.Card
	for _, c := range t.hand.Cards() {
		if t.fits(c) && (best == nil || faceOf(c).rank < faceOf(best).rank) {
			best = c
		}
	}
	if best != nil {
		return move{kind: movePlay, card: best}, true
	}
	if t.canDraw() {
		return move{kind: moveDraw}, true
	}
	var worst *cards.Card
	gap := func(c *cards.Card) int { return faceOf(c).rank - t.ladderFor(c).Len() }
	for _, c := range t.hand.Cards() {
		if worst == nil || gap(c) > gap(worst) {
			worst = c
		}
	}
	return move{kind: moveThrow, card: worst}, worst != nil
}

// giveUpMove is the move of a player who never climbs: draw while the hand has room and throw the first card away otherwise, which always ends in a loss.
func giveUpMove(t *table) (move, bool) {
	if t.result != playing {
		return move{}, false
	}
	if t.canDraw() {
		return move{kind: moveDraw}, true
	}
	if c := t.hand.Card(0); c != nil {
		return move{kind: moveThrow, card: c}, true
	}
	return move{}, false
}

// apply makes m on t through the table's own moves, and reports whether the rules let it.
func apply(t *table, m move) bool {
	switch m.kind {
	case movePlay:
		return t.play(m.card)
	case moveThrow:
		return t.throw(m.card)
	default:
		return t.draw()
	}
}

// playOut plays t to its end with strategy, failing past a bound no game reaches.
func playOut(tb testing.TB, t *table, strategy func(*table) (move, bool)) outcome {
	tb.Helper()
	for range 2000 {
		m, ok := strategy(t)
		if !ok {
			return t.result
		}
		if !apply(t, m) {
			tb.Fatalf("deal %d: the rules refused %+v", t.deal, m)
		}
		checkConserved(tb, t)
	}
	tb.Fatalf("deal %d did not end", t.deal)
	return playing
}

// checkConserved fails unless every card of the deck is on the board exactly once and every ladder climbs in its suit from 1.
func checkConserved(tb testing.TB, t *table) {
	tb.Helper()
	seen := map[*cards.Card]bool{}
	for _, s := range t.board.Slots() {
		for _, c := range s.Cards() {
			if seen[c] {
				tb.Fatalf("%s is on the board twice", c.ID)
			}
			seen[c] = true
		}
	}
	if len(seen) != t.rules.cardCount() {
		tb.Fatalf("%d cards on the board, want %d", len(seen), t.rules.cardCount())
	}
	for i, l := range t.ladders {
		for j, c := range l.Cards() {
			if f := faceOf(c); f.suit != i || f.rank != j+1 {
				tb.Fatalf("ladder %s holds %s at step %d", l.ID, c.ID, j+1)
			}
		}
	}
	if t.hand.Len() > t.hand.Capacity {
		tb.Fatalf("hand holds %d cards, more than %d", t.hand.Len(), t.hand.Capacity)
	}
}

func TestDealIsTheSameEveryTime(t *testing.T) {
	ids := func(n int) []string {
		var out []string
		for _, c := range newTestTable(t, n).board.Slots() {
			for _, card := range c.Cards() {
				out = append(out, card.ID)
			}
		}
		return out
	}
	random.AssertDeterministic(t, 3, func() string { return strings.Join(ids(7), ",") })
	if a, b := ids(7), ids(8); slices.Equal(a, b) {
		t.Fatal("deals 7 and 8 shuffled the same way")
	}
	tb := newTestTable(t, 7)
	if tb.hand.Len() != tb.hand.Capacity || tb.deck.Len() != tb.rules.cardCount()-tb.hand.Capacity || tb.redeals != tb.rules.Redeals || tb.result != playing {
		t.Fatalf("deal 7 opened with hand %d, deck %d, redeals %d, result %v", tb.hand.Len(), tb.deck.Len(), tb.redeals, tb.result)
	}
	checkConserved(t, tb)
}

func TestCarefulPlayWinsAndLoses(t *testing.T) {
	if got := playOut(t, newTestTable(t, winDeal), nextMove); got != won {
		t.Fatalf("deal %d ended %v, want won", winDeal, got)
	}
	if got := playOut(t, newTestTable(t, loseDeal), nextMove); got != lost {
		t.Fatalf("deal %d ended %v, want lost", loseDeal, got)
	}
}

func TestGivingUpLosesEveryDeal(t *testing.T) {
	for n := 1; n <= 20; n++ {
		if got := playOut(t, newTestTable(t, n), giveUpMove); got != lost {
			t.Fatalf("deal %d ended %v giving up, want lost", n, got)
		}
	}
}

// TestSomeDealsAreWon keeps the rules winnable: a careful player wins a fair share of deals but not every one.
func TestSomeDealsAreWon(t *testing.T) {
	wins := 0
	const deals = 200
	for n := 1; n <= deals; n++ {
		if playOut(t, newTestTable(t, n), nextMove) == won {
			wins++
		}
	}
	t.Logf("careful play wins %d of %d deals", wins, deals)
	if wins < deals/10 || wins > deals*9/10 {
		t.Fatalf("careful play wins %d of %d deals; tune rules.yaml so the game is neither hopeless nor certain", wins, deals)
	}
}

func TestRulesRefuseIllegalMoves(t *testing.T) {
	tb := newTestTable(t, winDeal)
	// A card that is not the next of its ladder cannot climb it, by a move or a drop.
	for _, c := range tb.hand.Cards() {
		if !tb.fits(c) {
			if tb.play(c) {
				t.Fatalf("%s climbed an empty ladder", c.ID)
			}
			if tb.board.CanDrop(c, tb.ladderFor(c)) {
				t.Fatalf("the board would drop %s on its empty ladder", c.ID)
			}
		}
		for _, l := range tb.ladders {
			if l != tb.ladderFor(c) && tb.board.CanDrop(c, l) {
				t.Fatalf("the board would drop %s on the %s ladder", c.ID, l.ID)
			}
		}
	}
	// A full hand cannot draw, and the top of the draw pile cannot go anywhere but the hand.
	if tb.canDraw() || tb.draw() {
		t.Fatal("a full hand drew")
	}
	top := tb.deck.Top()
	for _, s := range []*cards.Slot{tb.discard, tb.ladderFor(top)} {
		if tb.board.CanDrop(top, s) {
			t.Fatalf("the draw pile's top could drop on %s", s.ID)
		}
	}
	// Nothing is taken from the draw pile or the discard pile but by the rules.
	if !tb.throw(tb.hand.Card(0)) {
		t.Fatal("the hand could not discard")
	}
	if tb.play(tb.discard.Top()) || tb.throw(tb.deck.Top()) {
		t.Fatal("a card off the hand moved")
	}
	if !tb.board.CanDrop(top, tb.hand) {
		t.Fatal("the draw pile's top could not drop in a hand with room")
	}
}

func TestRedealTurnsTheDiscardPileOver(t *testing.T) {
	tb := newTestTable(t, winDeal)
	// Throw and draw until the draw pile is empty, remembering the order the cards were thrown in.
	var thrown []string
	for tb.deck.Len() > 0 {
		c := tb.hand.Card(0)
		thrown = append(thrown, c.ID)
		if !tb.throw(c) || !tb.draw() {
			t.Fatal("throw and draw refused")
		}
	}
	c := tb.hand.Card(0)
	thrown = append(thrown, c.ID)
	tb.throw(c)
	if !tb.canRedeal() || tb.redeals != tb.rules.Redeals {
		t.Fatalf("canRedeal %v with %d redeals", tb.canRedeal(), tb.redeals)
	}
	if !tb.draw() {
		t.Fatal("drawing from an empty draw pile did not redeal")
	}
	if tb.redeals != tb.rules.Redeals-1 || tb.discard.Len() != 0 {
		t.Fatalf("after a redeal: %d redeals, %d discarded", tb.redeals, tb.discard.Len())
	}
	// The first card thrown is drawn first.
	if got := tb.hand.Top().ID; got != thrown[0] {
		t.Fatalf("drew %s after the redeal, want the first card thrown, %s", got, thrown[0])
	}
	if got := tb.deck.Top().ID; got != thrown[1] {
		t.Fatalf("the new draw pile's top is %s, want %s", got, thrown[1])
	}
	checkConserved(t, tb)
}

func TestBoardDropsFollowTheRules(t *testing.T) {
	tb := newTestTable(t, winDeal)
	tb.board.DragSlop = -1
	// A card dragged from the hand onto the discard pile lands there.
	c := tb.hand.Card(0)
	from := c.Pose().Pos
	to := tb.discard.Rect.Center()
	if !tb.board.PointerDown(0, from) {
		t.Fatal("the pointer did not take the card")
	}
	tb.board.PointerMove(0, to)
	tb.board.PointerUp(0, to)
	if c.Slot() != tb.discard {
		t.Fatalf("%s dropped in %s, want discard", c.ID, c.Slot().ID)
	}
	tb.board.Settle()
	// The discard pile keeps its cards.
	if tb.board.PointerDown(1, tb.discard.Top().Pose().Pos) {
		t.Fatal("a pointer took a card off the discard pile")
	}
	// The top of the draw pile dragged into the hand is drawn.
	top := tb.deck.Top()
	if !tb.board.PointerDown(2, top.Pose().Pos) {
		t.Fatal("the pointer did not take the draw pile's top")
	}
	tb.board.PointerMove(2, tb.hand.Rect.Center())
	tb.board.PointerUp(2, tb.hand.Rect.Center())
	if top.Slot() != tb.hand || tb.hand.Len() != tb.hand.Capacity {
		t.Fatalf("the draw pile's top dropped in %s, hand %d", top.Slot().ID, tb.hand.Len())
	}
	checkConserved(t, tb)
}

func TestNewTableRefusesABoardWithoutTheRulesSlots(t *testing.T) {
	rules, err := parseRules(rulesYAML)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*cards.BoardDoc){
		"no hand": func(d *cards.BoardDoc) {
			d.Slots = slices.DeleteFunc(d.Slots, func(s cards.SlotDoc) bool { return s.ID == handSlot })
		},
		"hand without room": func(d *cards.BoardDoc) { slotDoc(d, handSlot).Capacity = 0 },
		"no ladder":         func(d *cards.BoardDoc) { slotDoc(d, ladderPrefix+"tide").ID = "tide" },
		"ladder of two":     func(d *cards.BoardDoc) { slotDoc(d, ladderPrefix+"leaf").Accepts = []string{"leaf", "stone"} },
		"extra slot": func(d *cards.BoardDoc) {
			d.Slots = append(d.Slots, cards.SlotDoc{ID: "spare", Rect: cards.RectDoc{W: 10, H: 10}, Layout: cards.LayoutDoc{Stack: &cards.StackDoc{}}})
		},
	} {
		doc, err := cards.ParseBoardDoc(boardYAML)
		if err != nil {
			t.Fatal(err)
		}
		edit(doc)
		if _, err := newTable(rules, doc); err == nil {
			t.Errorf("%s: newTable accepted the board", name)
		}
	}
}

func slotDoc(d *cards.BoardDoc, id string) *cards.SlotDoc {
	for i := range d.Slots {
		if d.Slots[i].ID == id {
			return &d.Slots[i]
		}
	}
	panic("no slot " + id)
}

func BenchmarkCarefulGame(b *testing.B) {
	for i := range b.N {
		tb := newTestTable(b, i%50+1)
		playOut(b, tb, nextMove)
	}
}
