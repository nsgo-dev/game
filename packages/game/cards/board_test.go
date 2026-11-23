package cards_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

const ms = time.Millisecond

// table is a board with a three-card hand fanned along the bottom, a grid on the table that takes up to two red cards, and a deck that takes blue cards and gives only its top one.
type table struct {
	b                *cards.Board
	hand, play, deck *cards.Slot
	a, bc, c         *cards.Card
	drops            []cards.DropEvent
	returns          []cards.ReturnEvent
	taps, picks      []string
}

func newTable(t *testing.T) *table {
	t.Helper()
	tb := &table{b: &cards.Board{CardSize: cardSize, SnapDuration: 100 * ms, ReturnDuration: 200 * ms}}
	tb.hand = &cards.Slot{ID: "hand", Rect: math2d.Rect{X: 0, Y: 300, W: 400, H: 100}, Layout: cards.Fan{}}
	tb.play = &cards.Slot{ID: "play", Rect: math2d.Rect{X: 100, Y: 50, W: 200, H: 120}, Layout: cards.Grid{}, Accepts: []string{"red"}, Capacity: 2}
	tb.deck = &cards.Slot{ID: "deck", Rect: math2d.Rect{X: 320, Y: 50, W: 60, H: 80}, Layout: cards.Stack{Offset: math2d.Vec2{Y: -1}}, Accepts: []string{"blue"}, Grab: cards.GrabTop}
	for _, s := range []*cards.Slot{tb.hand, tb.play, tb.deck} {
		if err := tb.b.AddSlot(s); err != nil {
			t.Fatalf("AddSlot(%s): %v", s.ID, err)
		}
	}
	tb.a = &cards.Card{ID: "a", Tags: []string{"red"}}
	tb.bc = &cards.Card{ID: "b", Tags: []string{"red"}}
	tb.c = &cards.Card{ID: "c", Tags: []string{"red"}}
	for _, c := range []*cards.Card{tb.a, tb.bc, tb.c} {
		if err := tb.b.Add(c, tb.hand, -1); err != nil {
			t.Fatalf("Add(%s): %v", c.ID, err)
		}
	}
	tb.b.OnDrop = func(e cards.DropEvent) { tb.drops = append(tb.drops, e) }
	tb.b.OnReturn = func(e cards.ReturnEvent) { tb.returns = append(tb.returns, e) }
	tb.b.OnTap = func(c *cards.Card) { tb.taps = append(tb.taps, c.ID) }
	tb.b.OnPickUp = func(c *cards.Card) { tb.picks = append(tb.picks, c.ID) }
	return tb
}

func ids(cs []*cards.Card) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func v(x, y float32) math2d.Vec2 { return math2d.Vec2{X: x, Y: y} }

func TestAddLaysOutAtOnce(t *testing.T) {
	tb := newTable(t)
	// Fan spacing defaults to 60% of 60 = 36 around the hand's center (200, 350).
	for c, x := range map[*cards.Card]float32{tb.a: 164, tb.bc: 200, tb.c: 236} {
		if !nearVec(c.Pose().Pos, v(x, 350)) || c.Moving() {
			t.Fatalf("%s at %v (moving %v), want (%g, 350) at rest", c.ID, c.Pose().Pos, c.Moving(), x)
		}
	}
	if tb.a.Index() != 0 || tb.c.Index() != 2 || tb.a.Slot() != tb.hand {
		t.Fatalf("indices %d %d, slot %v", tb.a.Index(), tb.c.Index(), tb.a.Slot())
	}
	if got := tb.b.CardAt(v(185, 350)); got != tb.bc {
		t.Fatalf("CardAt over a and b = %v, want the later card b", got)
	}
}

func TestDragSnapsIntoAcceptingSlot(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	if !b.PointerDown(7, v(150, 350)) {
		t.Fatal("pointer should take card a")
	}
	// Inside the slop the card stays put.
	b.PointerMove(7, v(150, 345))
	if tb.a.Held() || !nearVec(tb.a.Pose().Pos, v(164, 350)) || len(tb.picks) != 0 {
		t.Fatalf("card moved within the slop: %v held=%v", tb.a.Pose().Pos, tb.a.Held())
	}
	b.PointerMove(7, v(200, 110))
	if !tb.a.Held() || !nearVec(tb.a.Pose().Pos, v(214, 110)) || !slices.Equal(tb.picks, []string{"a"}) {
		t.Fatalf("held card at %v, want it to keep its grab offset at (214, 110); picks %v", tb.a.Pose().Pos, tb.picks)
	}
	if s, ok := b.DropTarget(tb.a); s != tb.play || !ok {
		t.Fatalf("DropTarget = %v %v, want play, true", s, ok)
	}
	if got := b.DrawOrder(nil); got[len(got)-1] != tb.a {
		t.Fatalf("held card drawn under others: %v", ids(got))
	}

	b.PointerUp(7, v(200, 110))
	if tb.a.Slot() != tb.play || tb.a.Index() != 0 || tb.a.Held() {
		t.Fatalf("card a in %v at %d, want play at 0", tb.a.Slot().ID, tb.a.Index())
	}
	if want := []cards.DropEvent{{Card: tb.a, From: tb.hand, To: tb.play, FromIndex: 0, Index: 0}}; !slices.Equal(tb.drops, want) {
		t.Fatalf("drops = %+v", tb.drops)
	}
	// Halfway through the snap the card is halfway to its cell at (130, 90).
	b.Update(50 * ms)
	if !nearVec(tb.a.Pose().Pos, v(172, 100)) {
		t.Fatalf("mid-snap at %v, want (172, 100)", tb.a.Pose().Pos)
	}
	b.Update(50 * ms)
	if !nearVec(tb.a.Pose().Pos, v(130, 90)) || tb.a.Pose().Rot != 0 || tb.a.Moving() {
		t.Fatalf("landed at %+v (moving %v), want (130, 90)", tb.a.Pose(), tb.a.Moving())
	}
	// The hand closed up around the gap.
	if !nearVec(tb.bc.Pose().Pos, v(182, 350)) || !nearVec(tb.c.Pose().Pos, v(218, 350)) {
		t.Fatalf("hand at %v %v, want (182, 350) (218, 350)", tb.bc.Pose().Pos, tb.c.Pose().Pos)
	}
	if got := ids(b.DrawOrder(nil)); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Fatalf("draw order after landing = %v", got)
	}
	if len(tb.returns) != 0 || len(tb.taps) != 0 {
		t.Fatalf("unexpected returns %v taps %v", tb.returns, tb.taps)
	}
}

func TestRejectedDropFliesBack(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	start := tb.bc.Pose()
	b.PointerDown(1, v(200, 350))
	b.PointerMove(1, v(350, 90))
	if s, ok := b.DropTarget(tb.bc); s != tb.deck || ok {
		t.Fatalf("DropTarget over the deck = %v %v, want deck, false", s, ok)
	}
	b.PointerUp(1, v(350, 90))
	if tb.bc.Slot() != tb.hand || tb.bc.Index() != 1 || len(tb.drops) != 0 {
		t.Fatalf("rejected card in %s at %d, drops %v", tb.bc.Slot().ID, tb.bc.Index(), tb.drops)
	}
	if want := []cards.ReturnEvent{{Card: tb.bc, Slot: tb.hand, Rejected: tb.deck}}; !slices.Equal(tb.returns, want) {
		t.Fatalf("returns = %+v", tb.returns)
	}
	b.Update(100 * ms)
	if !tb.bc.Moving() || nearVec(tb.bc.Pose().Pos, start.Pos) {
		t.Fatalf("return took the snap duration instead of ReturnDuration: %v", tb.bc.Pose().Pos)
	}
	b.Update(100 * ms)
	if tb.bc.Pose() != start || tb.bc.Moving() {
		t.Fatalf("returned to %+v, want %+v", tb.bc.Pose(), start)
	}
	if got := ids(tb.hand.Cards()); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("hand order = %v", got)
	}
}

func TestReleaseOverNothingReturns(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	b.PointerDown(1, v(240, 350))
	b.PointerMove(1, v(900, 900))
	if s, ok := b.DropTarget(tb.c); s != nil || ok {
		t.Fatalf("DropTarget over nothing = %v %v", s, ok)
	}
	b.PointerUp(1, v(900, 900))
	if len(tb.returns) != 1 || tb.returns[0].Rejected != nil || tb.returns[0].Slot != tb.hand {
		t.Fatalf("returns = %+v", tb.returns)
	}
	b.Settle()
	if !nearVec(tb.c.Pose().Pos, v(236, 350)) {
		t.Fatalf("settled at %v", tb.c.Pose().Pos)
	}
}

func TestDropByOverlapWhenPointerIsOutsideSlots(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	// Grab card a 14 points left of its center, so the card hangs into the play area while the pointer is just outside it.
	b.PointerDown(1, v(150, 350))
	b.PointerMove(1, v(80, 100))
	b.PointerUp(1, v(80, 100))
	if tb.a.Slot() != tb.play {
		t.Fatalf("card a in %s, want play by overlap", tb.a.Slot().ID)
	}
}

func TestCapacityAndPredicates(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	drag := func(c *cards.Card, to math2d.Vec2) {
		t.Helper()
		at := c.Pose().Pos
		if !b.PointerDown(1, at) {
			t.Fatalf("pointer missed %s at %v", c.ID, at)
		}
		b.PointerMove(1, to)
		b.PointerUp(1, to)
		b.Settle()
	}
	drag(tb.a, v(200, 110))
	drag(tb.bc, v(200, 110))
	if tb.play.Len() != 2 {
		t.Fatalf("play holds %d, want 2", tb.play.Len())
	}
	drag(tb.c, v(200, 110))
	if tb.c.Slot() != tb.hand || len(tb.returns) != 1 || tb.returns[0].Rejected != tb.play {
		t.Fatalf("a full slot took a third card: %v", tb.returns)
	}

	// Slot.Accept and Board.Accept both have to agree.
	tb.deck.Accepts = nil
	tb.deck.Accept = func(c *cards.Card, from *cards.Slot) bool { return from == tb.hand }
	b.Accept = func(c *cards.Card, from, to *cards.Slot) bool { return c.ID != "c" }
	if b.CanDrop(tb.c, tb.deck) {
		t.Fatal("Board.Accept should refuse card c")
	}
	b.Accept = nil
	if !b.CanDrop(tb.c, tb.deck) || b.CanDrop(tb.a, tb.deck) {
		t.Fatal("Slot.Accept should admit cards from the hand only")
	}
	// A card can always go back into its own slot, even a full one.
	if !b.CanDrop(tb.a, tb.play) {
		t.Fatal("own slot refused its card")
	}
	if b.CanDrop(nil, tb.play) || b.CanDrop(tb.a, nil) || b.CanDrop(tb.a, &cards.Slot{ID: "x"}) {
		t.Fatal("CanDrop accepted a nil or foreign argument")
	}
}

func TestReorderWithinHand(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	b.PointerDown(1, v(150, 350)) // card a, grabbed 14 left of center
	b.PointerMove(1, v(240, 350))
	b.PointerUp(1, v(240, 350))
	if got := ids(tb.hand.Cards()); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Fatalf("hand order = %v, want [b c a]", got)
	}
	if len(tb.drops) != 1 || tb.drops[0].From != tb.hand || tb.drops[0].To != tb.hand || tb.drops[0].Index != 2 {
		t.Fatalf("drops = %+v", tb.drops)
	}
}

func TestTapGrabRulesAndLocks(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	b.PointerDown(1, v(200, 350))
	b.PointerMove(1, v(203, 352))
	b.PointerUp(1, v(203, 352))
	if !slices.Equal(tb.taps, []string{"b"}) || len(tb.drops)+len(tb.returns) != 0 || tb.bc.Moving() {
		t.Fatalf("taps %v drops %v returns %v", tb.taps, tb.drops, tb.returns)
	}

	blue1 := &cards.Card{ID: "blue1", Tags: []string{"blue"}}
	blue2 := &cards.Card{ID: "blue2", Tags: []string{"blue"}}
	_ = b.Add(blue1, tb.deck, -1)
	_ = b.Add(blue2, tb.deck, 0) // under blue1
	if tb.deck.Top() != blue1 {
		t.Fatalf("deck top = %v", tb.deck.Top().ID)
	}
	// The deck's cards nearly coincide; the top one is under the pointer and may be taken.
	if !b.PointerDown(2, v(350, 85)) {
		t.Fatal("deck top should be grabbable")
	}
	b.PointerCancel(2)
	// Pressing where only the lower card shows finds it, and GrabTop refuses it.
	if got := b.CardAt(v(350, 129.5)); got != blue2 {
		t.Fatalf("CardAt the deck's bottom edge = %v", got)
	}
	if b.PointerDown(2, v(350, 129.5)) {
		t.Fatal("GrabTop let a lower card go")
	}
	tb.deck.Grab = cards.GrabNone
	if b.PointerDown(2, v(350, 85)) {
		t.Fatal("GrabNone let a card go")
	}
	tb.a.Locked = true
	if b.PointerDown(3, v(150, 350)) {
		t.Fatal("a locked card was taken")
	}
	if b.PointerDown(4, v(50, 50)) {
		t.Fatal("a press over no card took one")
	}
	if got := tb.deck.Card(5); got != nil {
		t.Fatalf("Card(5) = %v", got)
	}
}

func TestPointerCancelReturns(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	b.PointerDown(1, v(240, 350))
	b.PointerMove(1, v(200, 100))
	b.PointerCancel(1)
	if len(tb.returns) != 1 || tb.returns[0].Rejected != nil || tb.c.Held() {
		t.Fatalf("returns = %+v", tb.returns)
	}
	b.Settle()
	if !nearVec(tb.c.Pose().Pos, v(236, 350)) || tb.c.Slot() != tb.hand {
		t.Fatalf("cancelled card at %v", tb.c.Pose().Pos)
	}
	// A pointer that never took a card is ignored.
	b.PointerMove(9, v(0, 0))
	b.PointerUp(9, v(0, 0))
	b.PointerCancel(9)
}

func TestRotatedCardStraightensWhileHeldAndHitsFollowRotation(t *testing.T) {
	b := &cards.Board{CardSize: cardSize, SnapDuration: 100 * ms}
	hand := &cards.Slot{ID: "hand", Rect: math2d.Rect{W: 300, H: 100}, Layout: cards.Fan{Arc: 90}}
	_ = b.AddSlot(hand)
	left := &cards.Card{ID: "l"}
	right := &cards.Card{ID: "r"}
	_ = b.Add(left, hand, -1)
	_ = b.Add(right, hand, -1)
	r := right.Pose()
	if r.Rot <= 0 {
		t.Fatalf("right card rotation %g, want clockwise", r.Rot)
	}
	// A point inside the card's upright bounds but outside its rotated body misses it.
	corner := r.Pos.Add(v(29, -39))
	if b.CardAt(corner) == right {
		t.Fatal("hit test ignored the card's rotation")
	}
	if !b.PointerDown(1, r.Pos) {
		t.Fatal("pointer missed the right card")
	}
	b.PointerMove(1, r.Pos.Add(v(0, -100)))
	b.Update(50 * ms)
	if got := right.Pose().Rot; !near32(got, r.Rot/2) {
		t.Fatalf("held rotation %g, want %g halfway upright", got, r.Rot/2)
	}
	b.Update(50 * ms)
	if right.Pose().Rot != 0 {
		t.Fatalf("held rotation %g, want upright", right.Pose().Rot)
	}
}

func TestMultiTouchDragsTwoCardsAtOnce(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	b.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1, Position: core.Point{X: 150, Y: 350}, Changed: true}}})
	b.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{
		{ID: 1, Position: core.Point{X: 150, Y: 350}},
		{ID: 2, Position: core.Point{X: 250, Y: 350}, Changed: true},
	}})
	b.OnTouch(core.TouchEvent{Phase: core.TouchMove, Touches: []core.Touch{
		{ID: 1, Position: core.Point{X: 140, Y: 100}, Changed: true},
		{ID: 2, Position: core.Point{X: 900, Y: 900}, Changed: true},
	}})
	if !tb.a.Held() || !tb.c.Held() {
		t.Fatalf("held a=%v c=%v, want both", tb.a.Held(), tb.c.Held())
	}
	// A second finger on a card already taken gets nothing.
	if b.PointerDown(3, tb.a.Pose().Pos) {
		t.Fatal("two pointers took one card")
	}
	b.OnTouch(core.TouchEvent{Phase: core.TouchEnd, Touches: []core.Touch{
		{ID: 1, Position: core.Point{X: 140, Y: 100}, Changed: true},
		{ID: 2, Position: core.Point{X: 900, Y: 900}},
	}})
	b.OnTouch(core.TouchEvent{Phase: core.TouchCancel, Touches: []core.Touch{{ID: 2, Position: core.Point{X: 900, Y: 900}, Changed: true}}})
	if tb.a.Slot() != tb.play || tb.c.Slot() != tb.hand || len(tb.drops) != 1 || len(tb.returns) != 1 {
		t.Fatalf("a in %s, c in %s, drops %d returns %d", tb.a.Slot().ID, tb.c.Slot().ID, len(tb.drops), len(tb.returns))
	}
}

func TestMouseAsFingerZeroAndMapPoint(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	// The view is drawn at half scale: view points double into board points.
	b.MapPoint = func(p core.Point) (math2d.Vec2, bool) {
		if p.X < 0 {
			return math2d.Vec2{}, false
		}
		return math2d.Vec2{X: float32(p.X) * 2, Y: float32(p.Y) * 2}, true
	}
	mouse := func(phase core.TouchPhase, x, y core.Points) {
		b.OnTouch(core.TouchEvent{Phase: phase, Touches: []core.Touch{{ID: 0, Position: core.Point{X: x, Y: y}, Changed: true}}})
	}
	mouse(core.TouchStart, 75, 175)
	mouse(core.TouchMove, 100, 55)
	mouse(core.TouchEnd, 100, 55)
	if tb.a.Slot() != tb.play {
		t.Fatalf("mouse drag left card a in %s", tb.a.Slot().ID)
	}
	b.Settle()
	// A release the map refuses cancels the drag.
	mouse(core.TouchStart, 80, 175)
	mouse(core.TouchMove, 100, 55)
	mouse(core.TouchEnd, -1, 0)
	if tb.bc.Slot() != tb.hand || len(tb.returns) != 1 || tb.bc.Held() {
		t.Fatalf("refused release: b in %s, returns %v", tb.bc.Slot().ID, tb.returns)
	}
}

func TestAttachChainsExistingHandler(t *testing.T) {
	tb := newTable(t)
	var heard int
	view := core.View{OnTouch: func(core.TouchEvent) { heard++ }}
	tb.b.Attach(&view)
	view.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 0, Position: core.Point{X: 150, Y: 350}, Changed: true}}})
	if heard != 1 {
		t.Fatalf("previous handler heard %d events", heard)
	}
	var bare core.View
	tb.b.Attach(&bare)
	if bare.OnTouch == nil {
		t.Fatal("Attach left a bare view without OnTouch")
	}
	tb.b.Attach(nil)
}

func TestMoveRemoveAndErrors(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	if err := b.Move(tb.c, tb.deck, -1); err != nil {
		t.Fatal(err)
	}
	if tb.c.Slot() != tb.deck || !tb.c.Moving() {
		t.Fatalf("moved card in %s moving=%v", tb.c.Slot().ID, tb.c.Moving())
	}
	if got := b.DrawOrder(nil); got[len(got)-1] != tb.c {
		t.Fatalf("flying card drawn under others: %v", ids(got))
	}
	b.Update(100 * ms)
	if !nearVec(tb.c.Pose().Pos, v(350, 90)) || tb.c.Moving() {
		t.Fatalf("moved card at %v", tb.c.Pose().Pos)
	}
	// Moving a held card ends its drag.
	b.PointerDown(1, tb.a.Pose().Pos)
	b.PointerMove(1, v(10, 10))
	if err := b.Move(tb.a, tb.play, 0); err != nil || tb.a.Held() {
		t.Fatalf("Move of a held card: err %v held %v", err, tb.a.Held())
	}
	b.PointerUp(1, v(10, 10))
	if len(tb.drops)+len(tb.returns) != 0 {
		t.Fatal("a released pointer still dropped the moved card")
	}

	if err := b.Remove(tb.bc); err != nil || tb.bc.Slot() != nil || tb.bc.Index() != -1 || tb.hand.Len() != 0 {
		t.Fatalf("Remove: err %v slot %v hand %d", err, tb.bc.Slot(), tb.hand.Len())
	}
	if err := b.Remove(tb.bc); !errors.Is(err, cards.ErrNoCard) {
		t.Fatalf("second Remove = %v", err)
	}
	if err := b.Add(tb.a, tb.hand, 0); !errors.Is(err, cards.ErrCardOwned) {
		t.Fatalf("Add of a card on the board = %v", err)
	}
	if err := b.Add(&cards.Card{}, &cards.Slot{ID: "loose"}, 0); !errors.Is(err, cards.ErrSlotOwned) {
		t.Fatalf("Add into a foreign slot = %v", err)
	}
	if err := b.Move(&cards.Card{}, tb.hand, 0); !errors.Is(err, cards.ErrNoCard) {
		t.Fatalf("Move of a foreign card = %v", err)
	}
	if err := b.Move(tb.a, &cards.Slot{}, 0); !errors.Is(err, cards.ErrSlotOwned) {
		t.Fatalf("Move into a foreign slot = %v", err)
	}
	if err := b.AddSlot(&cards.Slot{ID: "hand"}); !errors.Is(err, cards.ErrSlotID) {
		t.Fatalf("duplicate slot = %v", err)
	}
	if err := b.AddSlot(&cards.Slot{}); !errors.Is(err, cards.ErrSlotID) {
		t.Fatalf("unnamed slot = %v", err)
	}
	var other cards.Board
	if err := other.AddSlot(tb.hand); !errors.Is(err, cards.ErrSlotOwned) {
		t.Fatalf("slot on two boards = %v", err)
	}
	if b.Slot("deck") != tb.deck || b.Slot("nope") != nil || len(b.Slots()) != 3 || tb.hand.Board() != b {
		t.Fatal("slot lookup")
	}
}

func TestInstantMoveAndAddLandInDrawOrder(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	b.SnapDuration = -1
	if err := b.Move(tb.bc, tb.deck, -1); err != nil {
		t.Fatal(err)
	}
	if err := b.Move(tb.a, tb.deck, 0); err != nil {
		t.Fatal(err)
	}
	z := &cards.Card{ID: "z"}
	if err := b.Add(z, tb.deck, -1); err != nil {
		t.Fatal(err)
	}
	// No Update has run: the instant moves must already be back in slot order.
	if got, want := ids(b.DrawOrder(nil)), []string{"c", "a", "b", "z"}; !slices.Equal(got, want) {
		t.Fatalf("draw order %v, want %v", got, want)
	}
	if tb.a.Moving() || tb.bc.Moving() {
		t.Fatal("an instant move is still tweening")
	}
	if got := b.CardAt(z.Pose().Pos); got != z {
		t.Fatalf("card under the deck's top = %v, want z", got)
	}
	if !b.PointerDown(1, z.Pose().Pos) {
		t.Fatal("the deck's top card could not be grabbed")
	}
}

func TestPickUpCallbackMayMoveTheCard(t *testing.T) {
	tb := newTable(t)
	b := tb.b
	var moved cards.Pose
	b.OnPickUp = func(c *cards.Card) {
		if err := b.Move(c, tb.deck, -1); err != nil {
			t.Fatal(err)
		}
		moved = c.Pose()
	}
	b.PointerDown(1, tb.a.Pose().Pos)
	b.PointerMove(1, v(200, 110))
	if got := tb.a.Pose(); got != moved {
		t.Fatalf("pose after OnPickUp moved the card = %+v, want %+v where Move left it", got, moved)
	}
	if tb.a.Slot() != tb.deck || tb.a.Held() || !tb.a.Moving() {
		t.Fatalf("card in %s held %v moving %v", tb.a.Slot().ID, tb.a.Held(), tb.a.Moving())
	}
	b.Update(100 * ms)
	if !nearVec(tb.a.Pose().Pos, v(350, 90)) {
		t.Fatalf("card landed at %v, want the deck", tb.a.Pose().Pos)
	}
}

func TestRelayoutAndZeroValueDefaults(t *testing.T) {
	var b cards.Board
	if b.Size() != cards.DefaultCardSize {
		t.Fatalf("zero board card size = %v", b.Size())
	}
	s := &cards.Slot{ID: "s", Rect: math2d.Rect{W: 100, H: 100}}
	_ = b.AddSlot(s)
	c := &cards.Card{ID: "x"}
	_ = b.Add(c, s, -1)
	if !nearVec(c.Pose().Pos, v(31.5, 44)) {
		t.Fatalf("nil layout stacks at the top-left, got %v", c.Pose().Pos)
	}
	s.Rect.X = 100
	b.Relayout(s)
	b.Update(cards.DefaultSnapDuration - ms)
	if !c.Moving() {
		t.Fatal("relayout finished before DefaultSnapDuration")
	}
	b.Update(ms)
	if !nearVec(c.Pose().Pos, v(131.5, 44)) || c.Moving() {
		t.Fatalf("relaid out at %v", c.Pose().Pos)
	}
	// Default slop: a 6-point move is still a press.
	b.PointerDown(1, c.Pose().Pos)
	b.PointerMove(1, c.Pose().Pos.Add(v(6, 0)))
	if c.Held() {
		t.Fatal("a move within DefaultDragSlop started a drag")
	}
	b.PointerCancel(1)
	// A negative slop drags on any move, and negative durations settle at once.
	b.DragSlop, b.SnapDuration, b.ReturnDuration = -1, -1, -1
	b.PointerDown(1, c.Pose().Pos)
	b.PointerMove(1, c.Pose().Pos)
	if !c.Held() {
		t.Fatal("negative slop did not drag at once")
	}
	b.PointerUp(1, v(500, 500))
	if c.Moving() || !nearVec(c.Pose().Pos, v(131.5, 44)) {
		t.Fatalf("instant return left the card at %v moving=%v", c.Pose().Pos, c.Moving())
	}
	if c.HasTag("x") || cards.GrabTop.String() != "top" || cards.GrabRule(9).String() != "GrabRule(9)" {
		t.Fatal("HasTag or GrabRule.String")
	}
}

func TestBoardReplaysDeterministically(t *testing.T) {
	run := func() []cards.Pose {
		tb := newTable(t)
		b := tb.b
		var trace []cards.Pose
		b.PointerDown(1, v(150, 350))
		for i := range 20 {
			b.PointerMove(1, v(150+float32(i)*3, 350-float32(i)*12))
			b.Update(16 * ms)
			trace = append(trace, tb.a.Pose(), tb.bc.Pose())
		}
		b.PointerUp(1, v(207, 122))
		for range 20 {
			b.Update(16 * ms)
			trace = append(trace, tb.a.Pose(), tb.bc.Pose(), tb.c.Pose())
		}
		return trace
	}
	if a, b := run(), run(); !slices.Equal(a, b) {
		t.Fatal("two identical runs diverged")
	}
}

func BenchmarkBoardDragFrame(b *testing.B) {
	board := &cards.Board{CardSize: cardSize}
	hand := &cards.Slot{ID: "hand", Rect: math2d.Rect{W: 800, H: 120}, Layout: cards.Fan{Arc: 24, Sag: 14}}
	_ = board.AddSlot(hand)
	for range 10 {
		_ = board.Add(&cards.Card{}, hand, -1)
	}
	board.PointerDown(1, hand.Card(9).Pose().Pos)
	board.PointerMove(1, v(400, 10))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		board.PointerMove(1, v(400+float32(i%50), 10))
		board.Update(16 * ms)
	}
}
