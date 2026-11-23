package inventory

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknownEquipSlot reports an equipment slot name the Equipment does not have.
	ErrUnknownEquipSlot = errors.New("inventory: unknown equipment slot")
	// ErrNotEquippable reports an item whose equipment kind does not match the slot's kind.
	ErrNotEquippable = errors.New("inventory: item does not fit equipment slot")
)

// EquipSlot is one named equipment slot.
type EquipSlot struct {
	// Name identifies the slot, such as "head" or "ring-left"; unique within an Equipment.
	Name string
	// Kind is the equipment kind the slot accepts, matched against ItemDef.Equip; several slots may share a kind.
	Kind string
}

// Equipment is a set of typed slots, each holding at most one item. The zero Equipment is not usable; create one with NewEquipment.
type Equipment struct {
	catalog *Catalog
	slots   []EquipSlot
	index   map[string]int
	items   []string
}

// NewEquipment returns empty equipment with the given slots, in order, holding items from catalog. Slot names must be unique and, like kinds, non-empty without whitespace or control characters.
func NewEquipment(catalog *Catalog, slots ...EquipSlot) (*Equipment, error) {
	if catalog == nil {
		return nil, errors.New("inventory: nil catalog")
	}
	e := &Equipment{
		catalog: catalog,
		slots:   append([]EquipSlot(nil), slots...),
		index:   make(map[string]int, len(slots)),
		items:   make([]string, len(slots)),
	}
	for i, s := range slots {
		if !validName(s.Name) {
			return nil, fmt.Errorf("inventory: equipment slot %d has invalid name %q", i, s.Name)
		}
		if !validName(s.Kind) {
			return nil, fmt.Errorf("inventory: equipment slot %q has invalid kind %q", s.Name, s.Kind)
		}
		if _, dup := e.index[s.Name]; dup {
			return nil, fmt.Errorf("inventory: duplicate equipment slot %q", s.Name)
		}
		e.index[s.Name] = i
	}
	return e, nil
}

// Catalog returns the catalog the equipment's items come from.
func (e *Equipment) Catalog() *Catalog { return e.catalog }

// Slots returns a copy of the equipment's slots in order.
func (e *Equipment) Slots() []EquipSlot {
	return append([]EquipSlot(nil), e.slots...)
}

// Equipped returns the item in slot, and false when slot is empty or unknown.
func (e *Equipment) Equipped(slot string) (string, bool) {
	i, ok := e.index[slot]
	if !ok || e.items[i] == "" {
		return "", false
	}
	return e.items[i], true
}

// CanEquip reports whether item is in the catalog and fits slot's kind.
func (e *Equipment) CanEquip(slot, item string) bool {
	i, ok := e.index[slot]
	if !ok {
		return false
	}
	def, ok := e.catalog.def(item)
	return ok && def.Equip == e.slots[i].Kind
}

// Equip takes one item from slot from of src and puts it in the equipment slot. An item already in the slot goes back to src: into slot from when taking the item emptied it, otherwise wherever src.AddAll would put it. It returns ErrNotEquippable when the item's kind does not match the slot, ErrFull when src has no room for the replaced item, and ErrCatalogMismatch when src uses a different catalog; on any error nothing changes. Equipping the item that is already in the slot does nothing.
func (e *Equipment) Equip(slot string, src *Container, from int) error {
	i, ok := e.index[slot]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownEquipSlot, slot)
	}
	if src == nil {
		return errors.New("inventory: nil source container")
	}
	if src.catalog != e.catalog {
		return ErrCatalogMismatch
	}
	if err := src.checkSlot(from); err != nil {
		return err
	}
	stack := src.slots[from]
	if stack.Empty() {
		return fmt.Errorf("%w: slot %d", ErrEmptySlot, from)
	}
	if !e.CanEquip(slot, stack.Item) {
		return fmt.Errorf("%w: %q in %q (kind %q)", ErrNotEquippable, stack.Item, slot, e.slots[i].Kind)
	}
	old := e.items[i]
	if old == stack.Item {
		return nil
	}
	if old != "" && stack.Count > 1 && src.Room(old) < 1 {
		return fmt.Errorf("%w: no room to unequip %q", ErrFull, old)
	}
	if _, err := src.RemoveAt(from, 1); err != nil {
		return err
	}
	e.items[i] = stack.Item
	if old == "" {
		return nil
	}
	if src.slots[from].Empty() {
		src.slots[from] = Stack{Item: old, Count: 1}
		return nil
	}
	_, err := src.Add(old, 1)
	return err
}

// Unequip moves the item in slot into dst the way dst.AddAll does. It returns ErrEmptySlot when slot holds nothing and ErrFull, leaving the item equipped, when dst has no room.
func (e *Equipment) Unequip(slot string, dst *Container) error {
	i, ok := e.index[slot]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownEquipSlot, slot)
	}
	if dst == nil {
		return errors.New("inventory: nil destination container")
	}
	if dst.catalog != e.catalog {
		return ErrCatalogMismatch
	}
	if e.items[i] == "" {
		return fmt.Errorf("%w: equipment slot %q", ErrEmptySlot, slot)
	}
	if err := dst.AddAll(e.items[i], 1); err != nil {
		return err
	}
	e.items[i] = ""
	return nil
}

// Clear empties every equipment slot.
func (e *Equipment) Clear() {
	clear(e.items)
}

// EquipmentState is the equipped items in a form encoding/json writes deterministically, for storing in a save slot.
type EquipmentState struct {
	// Slots lists the occupied equipment slots in the Equipment's slot order.
	Slots []EquippedState `json:"slots"`
}

// EquippedState is one occupied equipment slot in an EquipmentState.
type EquippedState struct {
	// Slot is the equipment slot's name.
	Slot string `json:"slot"`
	// Item is the equipped item's ID.
	Item string `json:"item"`
}

// Snapshot returns the occupied slots in slot order.
func (e *Equipment) Snapshot() EquipmentState {
	st := EquipmentState{Slots: []EquippedState{}}
	for i, item := range e.items {
		if item != "" {
			st.Slots = append(st.Slots, EquippedState{Slot: e.slots[i].Name, Item: item})
		}
	}
	return st
}

// Restore replaces the equipped items with st after checking every entry: the slot must exist and be listed once, and the item must be in the catalog and fit the slot's kind. On error the equipment is left unchanged.
func (e *Equipment) Restore(st EquipmentState) error {
	items := make([]string, len(e.slots))
	for _, s := range st.Slots {
		i, ok := e.index[s.Slot]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownEquipSlot, s.Slot)
		}
		if items[i] != "" {
			return fmt.Errorf("inventory: restore lists equipment slot %q twice", s.Slot)
		}
		if !e.catalog.Has(s.Item) {
			return fmt.Errorf("%w: %q in equipment slot %q", ErrUnknownItem, s.Item, s.Slot)
		}
		if !e.CanEquip(s.Slot, s.Item) {
			return fmt.Errorf("%w: %q in %q (kind %q)", ErrNotEquippable, s.Item, s.Slot, e.slots[i].Kind)
		}
		items[i] = s.Item
	}
	e.items = items
	return nil
}
