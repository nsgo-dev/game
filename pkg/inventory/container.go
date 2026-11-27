package inventory

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknownItem reports an item ID the catalog does not define.
	ErrUnknownItem = errors.New("inventory: unknown item")
	// ErrInvalidCount reports a non-positive count, or a count larger than a stack holds.
	ErrInvalidCount = errors.New("inventory: invalid count")
	// ErrSlotOutOfRange reports a container slot index outside [0, Len).
	ErrSlotOutOfRange = errors.New("inventory: slot out of range")
	// ErrEmptySlot reports an operation that needs an item in a slot that holds none.
	ErrEmptySlot = errors.New("inventory: slot is empty")
	// ErrSlotOccupied reports a destination slot holding a different item, or one without room for the items being moved.
	ErrSlotOccupied = errors.New("inventory: slot is occupied")
	// ErrFull reports that a container has no room for all of the items asked for.
	ErrFull = errors.New("inventory: container is full")
	// ErrNotEnough reports a container holding fewer of an item than asked for.
	ErrNotEnough = errors.New("inventory: not enough items")
	// ErrCatalogMismatch reports an operation across containers or equipment built from different catalogs.
	ErrCatalogMismatch = errors.New("inventory: different catalogs")
)

// Stack is a count of one item in a slot. The zero Stack is an empty slot.
type Stack struct {
	// Item is the item's ID in the catalog; empty for an empty slot.
	Item string
	// Count is how many of the item the slot holds; 0 for an empty slot.
	Count int
}

// Empty reports whether the stack holds nothing.
func (s Stack) Empty() bool { return s.Item == "" || s.Count <= 0 }

// Container is a fixed number of slots holding item stacks from one Catalog. The zero Container is not usable; create one with NewContainer.
type Container struct {
	catalog *Catalog
	slots   []Stack
}

// NewContainer returns an empty container with size slots, in [1, MaxContainerSize], holding items from catalog.
func NewContainer(catalog *Catalog, size int) (*Container, error) {
	if catalog == nil {
		return nil, errors.New("inventory: nil catalog")
	}
	if size < 1 || size > MaxContainerSize {
		return nil, fmt.Errorf("inventory: container size %d must be in [1, %d]", size, MaxContainerSize)
	}
	return &Container{catalog: catalog, slots: make([]Stack, size)}, nil
}

// Catalog returns the catalog the container's items come from.
func (c *Container) Catalog() *Catalog { return c.catalog }

// Len returns the number of slots.
func (c *Container) Len() int { return len(c.slots) }

// Slot returns the stack in slot i, or an empty Stack when i is out of range.
func (c *Container) Slot(i int) Stack {
	if i < 0 || i >= len(c.slots) {
		return Stack{}
	}
	return c.slots[i]
}

// Slots returns a copy of every slot in order.
func (c *Container) Slots() []Stack {
	out := make([]Stack, len(c.slots))
	copy(out, c.slots)
	return out
}

// Count returns how many of item the container holds across all slots.
func (c *Container) Count(item string) int {
	n := 0
	for _, s := range c.slots {
		if s.Item == item {
			n += s.Count
		}
	}
	return n
}

// IsEmpty reports whether every slot is empty.
func (c *Container) IsEmpty() bool {
	for _, s := range c.slots {
		if !s.Empty() {
			return false
		}
	}
	return true
}

// Room returns how many more of item the container can hold: the free space in its stacks of item plus a full stack per empty slot. It returns 0 for an item the catalog does not define.
func (c *Container) Room(item string) int {
	def, ok := c.catalog.def(item)
	if !ok {
		return 0
	}
	return c.room(item, def.EffectiveMaxStack())
}

func (c *Container) room(item string, max int) int {
	n := 0
	for _, s := range c.slots {
		switch {
		case s.Empty():
			n += max
		case s.Item == item && s.Count < max:
			n += max - s.Count
		}
	}
	return n
}

// Add puts up to count of item into the container and returns how many did not fit. It first tops up existing stacks of item in slot order, then fills empty slots in slot order. It returns ErrUnknownItem or ErrInvalidCount, adding nothing, when item is not in the catalog or count is not positive.
func (c *Container) Add(item string, count int) (remaining int, err error) {
	def, err := c.lookup(item, count)
	if err != nil {
		return count, err
	}
	max := def.EffectiveMaxStack()
	remaining = count
	for i := range c.slots {
		if remaining == 0 {
			return 0, nil
		}
		s := &c.slots[i]
		if s.Item == item && s.Count < max {
			n := min(remaining, max-s.Count)
			s.Count += n
			remaining -= n
		}
	}
	for i := range c.slots {
		if remaining == 0 {
			return 0, nil
		}
		s := &c.slots[i]
		if s.Empty() {
			n := min(remaining, max)
			*s = Stack{Item: item, Count: n}
			remaining -= n
		}
	}
	return remaining, nil
}

// AddAll puts all count of item into the container the way Add does, or returns ErrFull and adds nothing when they do not all fit.
func (c *Container) AddAll(item string, count int) error {
	def, err := c.lookup(item, count)
	if err != nil {
		return err
	}
	if c.room(item, def.EffectiveMaxStack()) < count {
		return fmt.Errorf("%w: no room for %d %q", ErrFull, count, item)
	}
	_, err = c.Add(item, count)
	return err
}

// Remove takes count of item out of the container, from the last slot holding it backwards, or returns ErrNotEnough and removes nothing when the container holds fewer.
func (c *Container) Remove(item string, count int) error {
	if _, err := c.lookup(item, count); err != nil {
		return err
	}
	if have := c.Count(item); have < count {
		return fmt.Errorf("%w: have %d %q, want %d", ErrNotEnough, have, item, count)
	}
	for i := len(c.slots) - 1; i >= 0 && count > 0; i-- {
		s := &c.slots[i]
		if s.Item != item {
			continue
		}
		n := min(count, s.Count)
		s.Count -= n
		count -= n
		if s.Count == 0 {
			*s = Stack{}
		}
	}
	return nil
}

// RemoveAt takes count items out of slot i and returns them as a Stack. It returns ErrEmptySlot for an empty slot and ErrInvalidCount when count is not in [1, the slot's count].
func (c *Container) RemoveAt(i, count int) (Stack, error) {
	if err := c.checkSlot(i); err != nil {
		return Stack{}, err
	}
	s := &c.slots[i]
	if s.Empty() {
		return Stack{}, fmt.Errorf("%w: slot %d", ErrEmptySlot, i)
	}
	if count < 1 || count > s.Count {
		return Stack{}, fmt.Errorf("%w: %d from a stack of %d", ErrInvalidCount, count, s.Count)
	}
	out := Stack{Item: s.Item, Count: count}
	s.Count -= count
	if s.Count == 0 {
		*s = Stack{}
	}
	return out, nil
}

// Clear empties every slot.
func (c *Container) Clear() {
	clear(c.slots)
}

// Move moves the stack in slot from to slot to within the container, as MoveTo does.
func (c *Container) Move(from, to int) error {
	return c.MoveTo(from, c, to)
}

// MoveTo moves the whole stack in slot from into slot to of dst, which may be c itself:
//   - into an empty slot, the stack moves there;
//   - onto a stack of the same item, as many as that stack has room for merge into it and the rest stay in from;
//   - onto a different item, the two stacks swap.
//
// Moving a slot onto itself does nothing. It returns ErrEmptySlot when slot from is empty and ErrCatalogMismatch when dst uses a different catalog.
func (c *Container) MoveTo(from int, dst *Container, to int) error {
	if err := c.checkPair(from, dst, to); err != nil {
		return err
	}
	src := &c.slots[from]
	if src.Empty() {
		return fmt.Errorf("%w: slot %d", ErrEmptySlot, from)
	}
	if dst == c && from == to {
		return nil
	}
	d := &dst.slots[to]
	switch {
	case d.Empty():
		*d, *src = *src, Stack{}
	case d.Item == src.Item:
		def, _ := c.catalog.def(src.Item)
		n := min(src.Count, def.EffectiveMaxStack()-d.Count)
		if n <= 0 {
			return nil
		}
		d.Count += n
		src.Count -= n
		if src.Count == 0 {
			*src = Stack{}
		}
	default:
		*d, *src = *src, *d
	}
	return nil
}

// Split moves count items from slot from to slot to within the container, as SplitTo does.
func (c *Container) Split(from, to, count int) error {
	return c.SplitTo(from, c, to, count)
}

// SplitTo moves count items, in [1, the stack's count], from slot from into slot to of dst, which must be empty or hold the same item with room for all count. It returns ErrSlotOccupied, moving nothing, when slot to holds a different item or lacks the room, and does nothing when from and to are the same slot.
func (c *Container) SplitTo(from int, dst *Container, to int, count int) error {
	if err := c.checkPair(from, dst, to); err != nil {
		return err
	}
	src := &c.slots[from]
	if src.Empty() {
		return fmt.Errorf("%w: slot %d", ErrEmptySlot, from)
	}
	if count < 1 || count > src.Count {
		return fmt.Errorf("%w: %d from a stack of %d", ErrInvalidCount, count, src.Count)
	}
	if dst == c && from == to {
		return nil
	}
	d := &dst.slots[to]
	if !d.Empty() {
		def, _ := c.catalog.def(src.Item)
		if d.Item != src.Item || d.Count+count > def.EffectiveMaxStack() {
			return fmt.Errorf("%w: slot %d cannot take %d %q", ErrSlotOccupied, to, count, src.Item)
		}
		d.Count += count
	} else {
		*d = Stack{Item: src.Item, Count: count}
	}
	src.Count -= count
	if src.Count == 0 {
		*src = Stack{}
	}
	return nil
}

// Transfer quick-moves the stack in slot from into dst the way Add does (topping up dst's stacks of the item, then filling its empty slots) and returns how many moved; what does not fit stays in slot from. dst must be a different container from the same catalog.
func (c *Container) Transfer(from int, dst *Container) (moved int, err error) {
	if err := c.checkSlot(from); err != nil {
		return 0, err
	}
	if dst == nil {
		return 0, errors.New("inventory: nil destination container")
	}
	if dst == c {
		return 0, errors.New("inventory: transfer into the same container")
	}
	if dst.catalog != c.catalog {
		return 0, ErrCatalogMismatch
	}
	src := &c.slots[from]
	if src.Empty() {
		return 0, fmt.Errorf("%w: slot %d", ErrEmptySlot, from)
	}
	remaining, err := dst.Add(src.Item, src.Count)
	if err != nil {
		return 0, err
	}
	moved = src.Count - remaining
	src.Count = remaining
	if src.Count == 0 {
		*src = Stack{}
	}
	return moved, nil
}

func (c *Container) lookup(item string, count int) (*ItemDef, error) {
	def, ok := c.catalog.def(item)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownItem, item)
	}
	if count < 1 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidCount, count)
	}
	return def, nil
}

func (c *Container) checkSlot(i int) error {
	if i < 0 || i >= len(c.slots) {
		return fmt.Errorf("%w: %d not in [0, %d)", ErrSlotOutOfRange, i, len(c.slots))
	}
	return nil
}

func (c *Container) checkPair(from int, dst *Container, to int) error {
	if dst == nil {
		return errors.New("inventory: nil destination container")
	}
	if dst.catalog != c.catalog {
		return ErrCatalogMismatch
	}
	if err := c.checkSlot(from); err != nil {
		return err
	}
	return dst.checkSlot(to)
}

// ContainerState is a container's contents in a form encoding/json writes deterministically, for storing in a save slot.
type ContainerState struct {
	// Slots lists the non-empty slots in ascending slot order.
	Slots []SlotState `json:"slots"`
}

// SlotState is one non-empty slot in a ContainerState.
type SlotState struct {
	// Slot is the slot index.
	Slot int `json:"slot"`
	// Item is the item's ID.
	Item string `json:"item"`
	// Count is how many of the item the slot holds.
	Count int `json:"count"`
}

// Snapshot returns the container's non-empty slots in ascending order.
func (c *Container) Snapshot() ContainerState {
	st := ContainerState{Slots: []SlotState{}}
	for i, s := range c.slots {
		if !s.Empty() {
			st.Slots = append(st.Slots, SlotState{Slot: i, Item: s.Item, Count: s.Count})
		}
	}
	return st
}

// Restore replaces the container's contents with st after checking every entry: the slot must be in range and listed once, the item defined by the catalog, and the count in [1, the item's maximum stack]. On error the container is left unchanged.
func (c *Container) Restore(st ContainerState) error {
	slots, err := c.prepareRestore(st)
	if err != nil {
		return err
	}
	c.slots = slots
	return nil
}

func (c *Container) prepareRestore(st ContainerState) ([]Stack, error) {
	slots := make([]Stack, len(c.slots))
	seen := make(map[int]struct{}, len(st.Slots))
	for _, e := range st.Slots {
		if err := c.checkSlot(e.Slot); err != nil {
			return nil, err
		}
		if _, dup := seen[e.Slot]; dup {
			return nil, fmt.Errorf("inventory: restore lists slot %d twice", e.Slot)
		}
		seen[e.Slot] = struct{}{}
		def, ok := c.catalog.def(e.Item)
		if !ok {
			return nil, fmt.Errorf("%w: %q in slot %d", ErrUnknownItem, e.Item, e.Slot)
		}
		if e.Count < 1 || e.Count > def.EffectiveMaxStack() {
			return nil, fmt.Errorf("%w: %d %q in slot %d (max stack %d)", ErrInvalidCount, e.Count, e.Item, e.Slot, def.EffectiveMaxStack())
		}
		slots[e.Slot] = Stack{Item: e.Item, Count: e.Count}
	}
	return slots, nil
}
