package rts

import (
	"slices"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// Selection is the set of units a player has selected, in the order they were selected.
//
// The zero value is an empty selection ready to use. A Selection is not safe for concurrent use.
type Selection struct {
	ids []UnitID
	set map[UnitID]struct{}
}

// IDs returns the selected units in selection order. The slice is the selection's own: read it, and copy it before changing the selection if you need to keep it.
func (s *Selection) IDs() []UnitID { return s.ids }

// Len returns the number of selected units.
func (s *Selection) Len() int { return len(s.ids) }

// Contains reports whether id is selected.
func (s *Selection) Contains(id UnitID) bool {
	_, ok := s.set[id]
	return ok
}

// Clear deselects every unit.
func (s *Selection) Clear() {
	s.ids = s.ids[:0]
	clear(s.set)
}

// Set replaces the selection with ids, dropping repeats.
func (s *Selection) Set(ids []UnitID) {
	if len(ids) > 0 && len(s.ids) > 0 && &ids[0] == &s.ids[0] {
		// ids is the selection's own slice; Clear would empty it.
		ids = slices.Clone(ids)
	}
	s.Clear()
	s.Add(ids...)
}

// Add selects each of ids that is not selected yet, after the units already selected.
func (s *Selection) Add(ids ...UnitID) {
	if s.set == nil {
		s.set = make(map[UnitID]struct{}, len(ids))
	}
	for _, id := range ids {
		if _, ok := s.set[id]; ok || id == 0 {
			continue
		}
		s.set[id] = struct{}{}
		s.ids = append(s.ids, id)
	}
}

// Remove deselects each of ids, keeping the others in order.
func (s *Selection) Remove(ids ...UnitID) {
	removed := false
	for _, id := range ids {
		if _, ok := s.set[id]; ok {
			delete(s.set, id)
			removed = true
		}
	}
	if removed {
		s.compact()
	}
}

// Toggle deselects the units of ids that are selected and selects the others. When every unit of ids is already selected they are all deselected; otherwise the unselected ones are added, as a Ctrl-drag over a mixed group does in most strategy games.
func (s *Selection) Toggle(ids ...UnitID) {
	all := len(ids) > 0
	for _, id := range ids {
		if !s.Contains(id) {
			all = false
			break
		}
	}
	if all {
		s.Remove(ids...)
		return
	}
	s.Add(ids...)
}

// Retain keeps only the selected units keep reports true for, such as those still alive.
func (s *Selection) Retain(keep func(UnitID) bool) {
	removed := false
	for _, id := range s.ids {
		if !keep(id) {
			delete(s.set, id)
			removed = true
		}
	}
	if removed {
		s.compact()
	}
}

func (s *Selection) compact() {
	out := s.ids[:0]
	for _, id := range s.ids {
		if _, ok := s.set[id]; ok {
			out = append(out, id)
		}
	}
	clear(s.ids[len(out):])
	s.ids = out
}

// SelectMode is how a pick changes the selection.
type SelectMode uint8

// The select modes.
const (
	// Replace selects only the picked units.
	Replace SelectMode = iota
	// Extend adds the picked units to the selection.
	Extend
	// Invert toggles the picked units, as Selection.Toggle does.
	Invert
)

// ModeFor returns the mode the held modifier keys ask for: Shift extends the selection, Control (or Command on macOS) toggles units in and out, and no modifier replaces it.
func ModeFor(m core.Modifiers) SelectMode {
	switch {
	case m.Shift:
		return Extend
	case m.Control || m.Platform:
		return Invert
	}
	return Replace
}

// Apply changes s by picked according to mode.
func (s *Selection) Apply(mode SelectMode, picked []UnitID) {
	switch mode {
	case Extend:
		s.Add(picked...)
	case Invert:
		s.Toggle(picked...)
	default:
		s.Set(picked)
	}
}

// Picker finds units in world space; Crowd implements it.
type Picker interface {
	// UnitsIn appends to out[:0] the units whose bodies overlap r, in a stable order.
	UnitsIn(r math2d.Rect, out []UnitID) []UnitID
	// UnitAt returns the unit nearest p within slop world units.
	UnitAt(p math2d.Vec2, slop float32) (UnitID, bool)
	// KindOf returns the unit's game-defined kind.
	KindOf(id UnitID) (int, bool)
}

// BoxSelector turns pointer presses, drags and releases in world space into selections: a press and release without moving far picks the unit under the pointer, a drag picks every unit its rectangle touches, and a double click picks every unit in View of the same kind as the one clicked.
//
// Convert pointer positions from view to world coordinates (through the game's camera) before passing them in. A BoxSelector is not safe for concurrent use.
type BoxSelector struct {
	// Picker finds the units; usually the game's Crowd.
	Picker Picker
	// Selection is the selection the selector changes.
	Selection *Selection
	// Filter, when set, keeps only the units it reports true for, such as the player's own.
	Filter func(UnitID) bool
	// DragThreshold is how far in world units the pointer moves before a press becomes a box drag; 0 or less uses 4.
	DragThreshold float32
	// ClickSlop is how far in world units from a unit's body a click still picks it; negative values count as 0.
	ClickSlop float32
	// View is the world area on screen, where a double click looks for units of the same kind; an empty View looks in the whole world.
	View math2d.Rect

	pressed  bool
	dragging bool
	start    math2d.Vec2
	current  math2d.Vec2
	mode     SelectMode
	clicks   int
	scratch  []UnitID
	filtered []UnitID
}

// Press starts a pick at world position p with the modifier keys held and the platform's click count (2 for a double click), as core.PressEvent reports them.
func (b *BoxSelector) Press(p math2d.Vec2, mods core.Modifiers, clicks int) {
	b.pressed, b.dragging = true, false
	b.start, b.current = p, p
	b.mode = ModeFor(mods)
	b.clicks = max(clicks, 1)
}

// Drag moves the pointer of a pick in progress to world position p.
func (b *BoxSelector) Drag(p math2d.Vec2) {
	if !b.pressed {
		return
	}
	b.current = p
	threshold := b.DragThreshold
	if threshold <= 0 {
		threshold = 4
	}
	if !b.dragging && p.Dist(b.start) >= threshold {
		b.dragging = true
	}
}

// Cancel abandons a pick in progress without changing the selection, as when the pointer leaves the view or Escape is pressed.
func (b *BoxSelector) Cancel() { b.pressed, b.dragging = false, false }

// Dragging reports whether a box drag is in progress.
func (b *BoxSelector) Dragging() bool { return b.pressed && b.dragging }

// Box returns the drag rectangle in world space while a box drag is in progress, for drawing.
func (b *BoxSelector) Box() (math2d.Rect, bool) {
	if !b.Dragging() {
		return math2d.Rect{}, false
	}
	return math2d.FromMinMax(b.start.Min(b.current), b.start.Max(b.current)), true
}

// Release ends the pick at world position p, changes the selection, and returns the units picked (after Filter), in the selector's own slice that the next Release reuses. A click on empty ground in Replace mode clears the selection.
func (b *BoxSelector) Release(p math2d.Vec2) []UnitID {
	if !b.pressed {
		return nil
	}
	b.Drag(p)
	dragging := b.dragging
	b.pressed, b.dragging = false, false
	if b.Picker == nil || b.Selection == nil {
		return nil
	}
	if dragging {
		b.scratch = b.Picker.UnitsIn(math2d.FromMinMax(b.start.Min(p), b.start.Max(p)), b.scratch)
	} else {
		b.scratch = b.scratch[:0]
		slop := max(b.ClickSlop, 0)
		if id, ok := b.Picker.UnitAt(b.start, slop); ok && b.keep(id) {
			b.scratch = append(b.scratch, id)
			if b.clicks >= 2 {
				b.scratch = b.sameKind(id)
			}
		}
	}
	b.filtered = b.filtered[:0]
	for _, id := range b.scratch {
		if b.keep(id) {
			b.filtered = append(b.filtered, id)
		}
	}
	b.Selection.Apply(b.mode, b.filtered)
	return b.filtered
}

func (b *BoxSelector) keep(id UnitID) bool { return b.Filter == nil || b.Filter(id) }

// sameKind returns the units in View of the same kind as id, id first.
func (b *BoxSelector) sameKind(id UnitID) []UnitID {
	kind, ok := b.Picker.KindOf(id)
	if !ok {
		return b.scratch
	}
	view := b.View.Normalized()
	if view.Empty() {
		view = math2d.Rect{X: -maxWorld, Y: -maxWorld, W: 2 * maxWorld, H: 2 * maxWorld}
	}
	all := b.Picker.UnitsIn(view, nil)
	out := append(b.scratch[:0], id)
	for _, other := range all {
		if other == id {
			continue
		}
		if k, ok := b.Picker.KindOf(other); ok && k == kind {
			out = append(out, other)
		}
	}
	return out
}

// maxWorld bounds the world area searched when View is empty.
const maxWorld = 1 << 40
