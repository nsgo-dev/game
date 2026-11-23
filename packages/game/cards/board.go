package cards

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/tween"
	"github.com/nsgo-dev/nsgo/core"
)

// Defaults a Board uses for its zero fields.
const (
	// DefaultDragSlop is how far, in points, a pointer moves from where it pressed a card before the card starts to follow it.
	DefaultDragSlop float32 = 6
	// DefaultSnapDuration is how long a card takes to settle into its place after a valid drop or a layout change.
	DefaultSnapDuration = 180 * time.Millisecond
	// DefaultReturnDuration is how long a card rejected by a drop takes to fly back to its slot.
	DefaultReturnDuration = 240 * time.Millisecond
)

// DefaultCardSize is a poker card's 63×88 aspect at one point per millimeter, used while Board.CardSize is zero.
var DefaultCardSize = math2d.Vec2{X: 63, Y: 88}

// Errors returned by Board's methods.
var (
	// ErrSlotID reports a slot with an empty ID or one the board already holds.
	ErrSlotID = errors.New("cards: slot ID is empty or already on the board")
	// ErrSlotOwned reports a slot that already belongs to a board, or that does not belong to this one.
	ErrSlotOwned = errors.New("cards: slot belongs to another board")
	// ErrCardOwned reports adding a card that is already on a board.
	ErrCardOwned = errors.New("cards: card is already on a board")
	// ErrNoCard reports a card that is not on this board.
	ErrNoCard = errors.New("cards: card is not on this board")
)

// GrabRule says which cards of a slot a pointer may pick up.
type GrabRule uint8

const (
	// GrabAny lets any card of the slot be dragged, as from a hand.
	GrabAny GrabRule = iota
	// GrabTop lets only the last (topmost) card be dragged, as from a deck or a discard pile.
	GrabTop
	// GrabNone keeps every card of the slot in place.
	GrabNone
)

// String returns the rule's name as BoardDoc writes it: "any", "top" or "none".
func (g GrabRule) String() string {
	switch g {
	case GrabAny:
		return "any"
	case GrabTop:
		return "top"
	case GrabNone:
		return "none"
	default:
		return fmt.Sprintf("GrabRule(%d)", uint8(g))
	}
}

// Card is one card on a Board. Its exported fields are the game's; the board owns where it is and how it moves.
type Card struct {
	// ID names the card for the game; the board does not require it to be unique.
	ID string
	// Tags are matched against Slot.Accepts.
	Tags []string
	// Locked keeps the card from being picked up by a pointer; programmatic moves still apply.
	Locked bool
	// Data is the game's payload for the card.
	Data any

	board *Board
	slot  *Slot
	index int
	pose  Pose

	move   *tween.Tween // settling into a pose
	tilt   *tween.Tween // straightening while held
	held   bool         // following a pointer
	claim  bool         // a pointer pressed it and may start dragging it
	lifted bool         // drawn above the slots, in Board.lift
}

// Pose returns where the card is drawn now.
func (c *Card) Pose() Pose { return c.pose }

// Slot returns the slot holding the card, or nil when it is not on a board.
func (c *Card) Slot() *Slot { return c.slot }

// Index returns the card's place in its slot, 0 being the first (bottom) card, or -1 when it is not on a board.
func (c *Card) Index() int {
	if c.slot == nil {
		return -1
	}
	return c.index
}

// Held reports whether a pointer is dragging the card.
func (c *Card) Held() bool { return c.held }

// Moving reports whether the card is tweening to a pose.
func (c *Card) Moving() bool { return c.move != nil || c.tilt != nil }

// HasTag reports whether tag is one of the card's Tags.
func (c *Card) HasTag(tag string) bool { return slices.Contains(c.Tags, tag) }

// Slot is a place on a Board that holds cards in order and lays them out in Rect.
type Slot struct {
	// ID names the slot; it must be unique on its board.
	ID string
	// Rect is the slot's area in board coordinates: where its layout places cards and where a pointer drops onto it.
	Rect math2d.Rect
	// Layout arranges the slot's cards; nil stacks them all at Rect's top-left corner, as Stack{}.
	Layout Layout
	// Accepts, when not empty, admits only cards with at least one of these tags.
	Accepts []string
	// Capacity, when positive, is how many cards the slot holds at most.
	Capacity int
	// Grab says which of the slot's cards a pointer may pick up.
	Grab GrabRule
	// Accept, when set, also has to admit a card dropped from slot from.
	Accept func(c *Card, from *Slot) bool

	board *Board
	cards []*Card
}

// Board returns the board holding the slot, or nil.
func (s *Slot) Board() *Board { return s.board }

// Len returns how many cards the slot holds.
func (s *Slot) Len() int { return len(s.cards) }

// Card returns the slot's i-th card, 0 being the first (bottom) one, or nil when i is out of range.
func (s *Slot) Card(i int) *Card {
	if i < 0 || i >= len(s.cards) {
		return nil
	}
	return s.cards[i]
}

// Top returns the slot's last card, or nil when it is empty.
func (s *Slot) Top() *Card { return s.Card(len(s.cards) - 1) }

// Cards returns a copy of the slot's cards in order.
func (s *Slot) Cards() []*Card { return slices.Clone(s.cards) }

func (s *Slot) layout() Layout {
	if s.Layout == nil {
		return Stack{}
	}
	return s.Layout
}

// DropEvent reports a card dropped into a slot that accepted it.
type DropEvent struct {
	Card *Card
	// From and FromIndex are where the card was; To and Index where it is now. From equals To when a card was reordered within its slot.
	From, To         *Slot
	FromIndex, Index int
}

// ReturnEvent reports a dragged card going back to its slot.
type ReturnEvent struct {
	Card *Card
	// Slot is the slot the card returns to.
	Slot *Slot
	// Rejected is the slot that refused the drop, or nil when the card was released over no slot or its pointer was cancelled.
	Rejected *Slot
}

// drag is one pointer that pressed a card.
type drag struct {
	id     int
	card   *Card
	start  math2d.Vec2 // where the pointer pressed
	at     math2d.Vec2 // where the pointer is
	grab   math2d.Vec2 // card center minus pointer at the press
	active bool        // past the slop: the card follows the pointer
}

// Board holds slots of cards and drags cards between them with pointers, tweening every move. Step it with Update on each game.Loop step.
//
// The zero value is ready to use; zero tuning fields use the Default constants.
type Board struct {
	// CardSize is every card's width and height in board coordinates; zero uses DefaultCardSize.
	CardSize math2d.Vec2
	// DragSlop is how far a pointer moves from its press before the card follows it; 0 uses DefaultDragSlop and a negative value starts the drag on the first move.
	DragSlop float32
	// SnapDuration is how long a card takes to settle after a valid drop, a Move or a layout change; 0 uses DefaultSnapDuration and a negative value settles at once.
	SnapDuration time.Duration
	// ReturnDuration is how long a rejected card takes to fly back; 0 uses DefaultReturnDuration and a negative value returns at once.
	ReturnDuration time.Duration
	// Curve eases every move; the zero value is core.Linear, and core.EaseOut gives a softer landing.
	Curve core.Curve
	// MapPoint, when set, maps a touch's position in the view hearing it to board coordinates, as for a camera or a scaled viewport; returning false ignores the point. Nil uses the view's coordinates as they are.
	MapPoint func(core.Point) (math2d.Vec2, bool)
	// Accept, when set, also has to admit every drop of card c from slot from onto slot to other than back into from.
	Accept func(c *Card, from, to *Slot) bool

	// OnPickUp runs when a card starts following a pointer.
	OnPickUp func(c *Card)
	// OnTap runs when a pointer presses and releases a card without dragging it.
	OnTap func(c *Card)
	// OnDrop runs after a card lands in a slot that accepted it.
	OnDrop func(DropEvent)
	// OnReturn runs after a dragged card is sent back to its slot.
	OnReturn func(ReturnEvent)

	slots []*Slot
	lift  []*Card // cards drawn above the slots: held or flying, oldest first
	drags []drag
	poses []Pose
}

// Size returns the card size the board uses.
func (b *Board) Size() math2d.Vec2 {
	if b.CardSize.X > 0 && b.CardSize.Y > 0 {
		return b.CardSize
	}
	return DefaultCardSize
}

func (b *Board) slop() float32 {
	switch {
	case b.DragSlop < 0:
		return -1
	case b.DragSlop == 0:
		return DefaultDragSlop
	}
	return b.DragSlop
}

func (b *Board) snapDuration() time.Duration {
	if b.SnapDuration == 0 {
		return DefaultSnapDuration
	}
	return max(b.SnapDuration, 0)
}

func (b *Board) returnDuration() time.Duration {
	if b.ReturnDuration == 0 {
		return DefaultReturnDuration
	}
	return max(b.ReturnDuration, 0)
}

// AddSlot adds s on top of the slots already on the board: where slots overlap, the later one takes pointer drops.
func (b *Board) AddSlot(s *Slot) error {
	if s == nil || s.ID == "" || b.Slot(s.ID) != nil {
		return ErrSlotID
	}
	if s.board != nil {
		return ErrSlotOwned
	}
	s.board = b
	b.slots = append(b.slots, s)
	return nil
}

// Slot returns the slot named id, or nil.
func (b *Board) Slot(id string) *Slot {
	for _, s := range b.slots {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// Slots returns a copy of the board's slots in the order they were added.
func (b *Board) Slots() []*Slot { return slices.Clone(b.slots) }

// Add puts c into s at index (appending when index is negative or past the end) and settles s at once, without tweening: for dealing the opening position. Add ignores the slot's rules.
func (b *Board) Add(c *Card, s *Slot, index int) error {
	if c == nil || c.board != nil {
		return ErrCardOwned
	}
	if s == nil || s.board != b {
		return ErrSlotOwned
	}
	c.board = b
	s.insert(c, index)
	b.relayout(s, 0, 0)
	return nil
}

// Move sends c into slot to at index (appending when index is negative or past the end), tweening it over SnapDuration and drawing it above the slots until it lands. A drag holding c is cancelled. Move ignores the slot's rules, as a game dealing or undoing does.
func (b *Board) Move(c *Card, to *Slot, index int) error {
	if c == nil || c.board != b {
		return ErrNoCard
	}
	if to == nil || to.board != b {
		return ErrSlotOwned
	}
	b.release(c)
	from := c.slot
	from.remove(c)
	to.insert(c, index)
	b.raise(c)
	d := b.snapDuration()
	if from != to {
		b.relayout(from, d, d)
	}
	b.relayout(to, d, d)
	return nil
}

// Remove takes c off the board, cancelling a drag holding it, and tweens the rest of its slot closed.
func (b *Board) Remove(c *Card) error {
	if c == nil || c.board != b {
		return ErrNoCard
	}
	b.release(c)
	b.unlift(c)
	s := c.slot
	s.remove(c)
	c.board, c.slot, c.move, c.tilt = nil, nil, nil, nil
	b.relayout(s, b.snapDuration(), b.snapDuration())
	return nil
}

// Relayout tweens the cards of s to its layout, as after changing its Rect or Layout. Held cards keep following their pointers.
func (b *Board) Relayout(s *Slot) {
	if s != nil && s.board == b {
		b.relayout(s, b.snapDuration(), b.snapDuration())
	}
}

// Settle finishes every tween at once, leaving held cards where their pointers put them.
func (b *Board) Settle() {
	for _, s := range b.slots {
		for _, c := range s.cards {
			if c.tilt != nil {
				c.tilt.Seek(c.tilt.Duration())
				c.tilt = nil
			}
			if c.move != nil {
				c.move.Seek(c.move.Duration())
				c.move = nil
			}
		}
	}
	b.compactLift()
}

// Update advances every card's tween by dt. Call it once per game.Loop.Update step.
func (b *Board) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	for _, s := range b.slots {
		for _, c := range s.cards {
			if c.tilt != nil && c.tilt.Update(dt) {
				c.tilt = nil
			}
			if c.move != nil && c.move.Update(dt) {
				c.move = nil
			}
		}
	}
	b.compactLift()
}

// DrawOrder appends the board's cards to dst in the order to draw them, bottom first, and returns the extended slice: each slot's cards in slot order, then the cards that are held or flying, in the order they were lifted.
func (b *Board) DrawOrder(dst []*Card) []*Card {
	for _, s := range b.slots {
		for _, c := range s.cards {
			if !c.lifted {
				dst = append(dst, c)
			}
		}
	}
	return append(dst, b.lift...)
}

// CardAt returns the topmost card drawn under p, following each card's rotation, or nil.
func (b *Board) CardAt(p math2d.Vec2) *Card {
	size := b.Size()
	for i := len(b.lift) - 1; i >= 0; i-- {
		if hit(b.lift[i].pose, size, p) {
			return b.lift[i]
		}
	}
	for i := len(b.slots) - 1; i >= 0; i-- {
		cards := b.slots[i].cards
		for j := len(cards) - 1; j >= 0; j-- {
			if c := cards[j]; !c.lifted && hit(c.pose, size, p) {
				return c
			}
		}
	}
	return nil
}

// SlotAt returns the topmost slot whose Rect holds p, or nil.
func (b *Board) SlotAt(p math2d.Vec2) *Slot {
	for i := len(b.slots) - 1; i >= 0; i-- {
		if b.slots[i].Rect.Contains(p) {
			return b.slots[i]
		}
	}
	return nil
}

// CanDrop reports whether slot to would accept c dropped onto it now: its own slot always does; another needs room under Capacity, a tag in Accepts when that is set, and the approval of to.Accept and Board.Accept when they are set.
func (b *Board) CanDrop(c *Card, to *Slot) bool {
	if c == nil || to == nil || c.board != b || to.board != b {
		return false
	}
	if to == c.slot {
		return true
	}
	if to.Capacity > 0 && len(to.cards) >= to.Capacity {
		return false
	}
	if len(to.Accepts) > 0 && !slices.ContainsFunc(to.Accepts, c.HasTag) {
		return false
	}
	if to.Accept != nil && !to.Accept(c, c.slot) {
		return false
	}
	return b.Accept == nil || b.Accept(c, c.slot, to)
}

// DropTarget returns the slot a held card would drop onto if released now, and whether that slot accepts it, so a game can highlight it. It returns nil and false for a card no pointer is dragging, and nil and false while the card is over no slot.
func (b *Board) DropTarget(c *Card) (*Slot, bool) {
	d := b.dragOf(c)
	if d == nil || !d.active {
		return nil, false
	}
	s := b.dropSlot(c, d.at)
	return s, b.CanDrop(c, s)
}

// PointerDown presses pointer id at p, in board coordinates, and reports whether it took a card: the topmost card under p that is not Locked, not already pressed by another pointer and that its slot's Grab rule lets go. The card follows the pointer once it moves past DragSlop.
func (b *Board) PointerDown(id int, p math2d.Vec2) bool {
	if b.dragIndex(id) >= 0 {
		return false
	}
	c := b.CardAt(p)
	if c == nil || c.Locked || c.claim {
		return false
	}
	switch c.slot.Grab {
	case GrabNone:
		return false
	case GrabTop:
		if c != c.slot.Top() {
			return false
		}
	}
	c.claim = true
	b.drags = append(b.drags, drag{id: id, card: c, start: p, at: p, grab: c.pose.Pos.Sub(p)})
	return true
}

// PointerMove moves pointer id to p, dragging its card once it has moved past DragSlop from its press.
func (b *Board) PointerMove(id int, p math2d.Vec2) {
	i := b.dragIndex(id)
	if i < 0 {
		return
	}
	d := &b.drags[i]
	d.at = p
	c := d.card
	if !d.active {
		if p.Dist(d.start) <= b.slop() {
			return
		}
		d.active = true
		c.held = true
		c.move = nil
		b.raise(c)
		if c.pose.Rot != 0 {
			c.tilt = tween.New(c.pose.Rot, 0, b.snapDuration(), b.Curve, lerpFloat32, func(r float32) { c.pose.Rot = r })
		}
		if b.OnPickUp != nil {
			b.OnPickUp(c)
		}
	}
	c.pose.Pos = p.Add(d.grab)
}

// PointerUp releases pointer id at p: a card it was dragging drops into the slot under it when that slot accepts it and flies back otherwise, and a card it pressed without dragging is tapped.
func (b *Board) PointerUp(id int, p math2d.Vec2) {
	i := b.dragIndex(id)
	if i < 0 {
		return
	}
	d := b.drags[i]
	c := d.card
	if !d.active {
		b.drags = slices.Delete(b.drags, i, i+1)
		c.claim = false
		if b.OnTap != nil {
			b.OnTap(c)
		}
		return
	}
	b.PointerMove(id, p)
	b.drags = slices.Delete(b.drags, i, i+1)
	c.claim, c.held = false, false
	to := b.dropSlot(c, p)
	if !b.CanDrop(c, to) {
		b.sendBack(c, to)
		return
	}
	from, fromIndex := c.slot, c.index
	from.remove(c)
	index := to.layout().InsertIndex(to.Rect, b.Size(), len(to.cards), c.pose.Pos)
	to.insert(c, index)
	d0 := b.snapDuration()
	if from != to {
		b.relayout(from, d0, d0)
	}
	b.relayout(to, d0, d0)
	b.compactLift()
	if b.OnDrop != nil {
		b.OnDrop(DropEvent{Card: c, From: from, To: to, FromIndex: fromIndex, Index: c.index})
	}
}

// PointerCancel ends pointer id without a drop, as when the system takes the touches: a card it was dragging flies back to its slot.
func (b *Board) PointerCancel(id int) {
	i := b.dragIndex(id)
	if i < 0 {
		return
	}
	d := b.drags[i]
	b.drags = slices.Delete(b.drags, i, i+1)
	c := d.card
	c.claim = false
	if d.active {
		c.held = false
		b.sendBack(c, nil)
	}
}

// OnTouch feeds a core.TouchEvent to the board, each finger that changed being its own pointer by Touch.ID; desktops report the primary mouse button as finger 0 and the web every pointer as a finger, so this handles the mouse too. A finger lifting where MapPoint refuses its point is cancelled.
func (b *Board) OnTouch(ev core.TouchEvent) {
	for _, t := range ev.Touches {
		if !t.Changed {
			continue
		}
		p, ok := b.mapPoint(t.Position)
		switch ev.Phase {
		case core.TouchStart:
			if ok {
				b.PointerDown(t.ID, p)
			}
		case core.TouchMove:
			if ok {
				b.PointerMove(t.ID, p)
			}
		case core.TouchEnd:
			if ok {
				b.PointerUp(t.ID, p)
			} else {
				b.PointerCancel(t.ID)
			}
		case core.TouchCancel:
			b.PointerCancel(t.ID)
		}
	}
}

// Attach wires b.OnTouch onto v, the view holding the game's Canvas, chaining any OnTouch v already has after the board's.
func (b *Board) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { b.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = b.OnTouch
	}
}

func (b *Board) mapPoint(p core.Point) (math2d.Vec2, bool) {
	if b.MapPoint != nil {
		return b.MapPoint(p)
	}
	return math2d.FromPoint(p), true
}

func (b *Board) dragIndex(id int) int {
	return slices.IndexFunc(b.drags, func(d drag) bool { return d.id == id })
}

func (b *Board) dragOf(c *Card) *drag {
	for i := range b.drags {
		if b.drags[i].card == c {
			return &b.drags[i]
		}
	}
	return nil
}

// release forgets any pointer pressing c, leaving the card where it is.
func (b *Board) release(c *Card) {
	b.drags = slices.DeleteFunc(b.drags, func(d drag) bool { return d.card == c })
	c.claim, c.held = false, false
}

// dropSlot returns the slot a card released with the pointer at p lands on: the topmost slot holding the pointer, else the slot the card's upright bounds overlap most (the later slot on a tie), else nil.
func (b *Board) dropSlot(c *Card, p math2d.Vec2) *Slot {
	if s := b.SlotAt(p); s != nil {
		return s
	}
	r := math2d.FromCenter(c.pose.Pos, b.Size())
	var best *Slot
	var bestArea float32
	for i := len(b.slots) - 1; i >= 0; i-- {
		if in, ok := b.slots[i].Rect.Intersection(r); ok && in.W*in.H > bestArea {
			best, bestArea = b.slots[i], in.W*in.H
		}
	}
	return best
}

func (b *Board) sendBack(c *Card, rejected *Slot) {
	b.relayout(c.slot, b.snapDuration(), b.returnDuration())
	b.compactLift()
	if b.OnReturn != nil {
		b.OnReturn(ReturnEvent{Card: c, Slot: c.slot, Rejected: rejected})
	}
}

// relayout tweens the cards of s to its layout: lifted (flying) cards over flyDur, the others over d. Held cards are left to their pointers.
func (b *Board) relayout(s *Slot, d, flyDur time.Duration) {
	b.poses = s.layout().Arrange(s.Rect, b.Size(), len(s.cards), b.poses[:0])
	for i, c := range s.cards {
		if c.held || i >= len(b.poses) {
			continue
		}
		dur := d
		if c.lifted {
			dur = flyDur
		}
		b.animate(c, b.poses[i], dur)
	}
}

func (b *Board) animate(c *Card, to Pose, d time.Duration) {
	c.tilt = nil
	if d <= 0 || c.pose == to {
		c.pose, c.move = to, nil
		return
	}
	c.move = tween.New(c.pose, to, d, b.Curve, lerpPose, func(p Pose) { c.pose = p })
}

func (b *Board) raise(c *Card) {
	b.unlift(c)
	c.lifted = true
	b.lift = append(b.lift, c)
}

func (b *Board) unlift(c *Card) {
	if c.lifted {
		c.lifted = false
		b.lift = slices.DeleteFunc(b.lift, func(l *Card) bool { return l == c })
	}
}

// compactLift returns the lifted cards that are neither held nor moving to their slots' order.
func (b *Board) compactLift() {
	b.lift = slices.DeleteFunc(b.lift, func(c *Card) bool {
		if c.held || c.move != nil || c.tilt != nil {
			return false
		}
		c.lifted = false
		return true
	})
}

func (s *Slot) insert(c *Card, index int) {
	if index < 0 || index > len(s.cards) {
		index = len(s.cards)
	}
	s.cards = slices.Insert(s.cards, index, c)
	c.slot = s
	s.reindex(index)
}

func (s *Slot) remove(c *Card) {
	i := c.index
	s.cards = slices.Delete(s.cards, i, i+1)
	s.reindex(i)
}

func (s *Slot) reindex(from int) {
	for i := from; i < len(s.cards); i++ {
		s.cards[i].index = i
	}
}

// hit reports whether p lies on a card of size at pose, following its rotation.
func hit(pose Pose, size, p math2d.Vec2) bool {
	d := p.Sub(pose.Pos)
	if pose.Rot != 0 {
		d = d.Rotate(-pose.Rot)
	}
	hx, hy := size.X/2, size.Y/2
	return d.X >= -hx && d.X < hx && d.Y >= -hy && d.Y < hy
}

func lerpPose(a, b Pose, t float64) Pose { return a.Lerp(b, float32(t)) }

func lerpFloat32(a, b float32, t float64) float32 { return a + (b-a)*float32(t) }
