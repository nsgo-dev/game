package stats

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

var (
	// ErrUnknownStat is returned when a stat name is not defined on the sheet or in the file.
	ErrUnknownStat = errors.New("stats: unknown stat")
	// ErrDuplicateStat is returned when a stat is defined twice.
	ErrDuplicateStat = errors.New("stats: duplicate stat")
	// ErrInvalidOp is returned for an Op other than OpAdd, OpMul and OpOverride.
	ErrInvalidOp = errors.New("stats: invalid modifier op")
	// ErrInvalidValue is returned for a NaN or infinite number.
	ErrInvalidValue = errors.New("stats: value is not finite")
	// ErrInvalidSteps is returned for a negative step count.
	ErrInvalidSteps = errors.New("stats: negative steps")
)

// ID identifies one modifier on a sheet. IDs start at 1 and are never reused by the same sheet, so the zero ID names no modifier.
type ID uint64

// Active describes a modifier currently on a sheet.
type Active struct {
	// ID is the modifier's handle for Sheet.Remove.
	ID ID
	// Source is the tag the modifier was added with, such as "equip:weapon" or "buff:haste".
	Source string
	// Modifier is the modifier itself.
	Modifier Modifier
	// Remaining is how many more calls of Sheet.Step the modifier lasts, 0 for a modifier without a time limit.
	Remaining int
}

// Change reports a stat whose value changed.
type Change struct {
	// Stat is the stat's name.
	Stat string
	// Old and New are its value before and after the change.
	Old, New float64
}

// Sheet holds one entity's stats and the modifiers on them.
//
// Every method that changes a base value or a modifier recomputes the stats it touched before returning and calls OnChange once for each one whose value changed, in definition order. OnChange may itself change the sheet; those changes are recomputed and reported before the outer call returns. A Sheet is not safe for concurrent use.
type Sheet struct {
	// OnChange, when set, is called with each stat whose value changed.
	OnChange func(Change)

	stats  []*stat
	byName map[string]*stat
	mods   []*entry
	nextID ID
	steps  uint64

	dirty    bool
	flushing bool
}

type stat struct {
	def   Def
	value float64
	mods  []*entry
	dirty bool
}

type entry struct {
	id        ID
	source    string
	mod       Modifier
	remaining int
	stat      *stat
}

// NewSheet returns a sheet holding the given stats at their base values, in the given order.
func NewSheet(defs ...Def) (*Sheet, error) {
	s := &Sheet{byName: make(map[string]*stat, len(defs))}
	for _, d := range defs {
		if err := s.Define(d); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Define adds a stat at its base value after the stats already defined. Defining a name twice returns ErrDuplicateStat.
func (s *Sheet) Define(d Def) error {
	if err := validateDef(d); err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	if s.byName == nil {
		s.byName = make(map[string]*stat)
	}
	if _, ok := s.byName[d.Name]; ok {
		return fmt.Errorf("%w %q", ErrDuplicateStat, d.Name)
	}
	if d.Min != nil {
		d.Min = Float(*d.Min)
	}
	if d.Max != nil {
		d.Max = Float(*d.Max)
	}
	st := &stat{def: d}
	st.value = st.compute()
	s.stats = append(s.stats, st)
	s.byName[d.Name] = st
	return nil
}

// Has reports whether the sheet defines the named stat.
func (s *Sheet) Has(name string) bool {
	_, ok := s.byName[name]
	return ok
}

// Stats returns the sheet's stat names in definition order.
func (s *Sheet) Stats() []string {
	names := make([]string, len(s.stats))
	for i, st := range s.stats {
		names[i] = st.def.Name
	}
	return names
}

// Value returns the named stat's current value, or 0 when it is not defined.
func (s *Sheet) Value(name string) float64 {
	v, _ := s.Lookup(name)
	return v
}

// Lookup returns the named stat's current value and whether it is defined.
func (s *Sheet) Lookup(name string) (float64, bool) {
	st, ok := s.byName[name]
	if !ok {
		return 0, false
	}
	return st.value, true
}

// Base returns the named stat's base value, or 0 when it is not defined.
func (s *Sheet) Base(name string) float64 {
	if st, ok := s.byName[name]; ok {
		return st.def.Base
	}
	return 0
}

// SetBase sets the named stat's base value and recomputes it.
func (s *Sheet) SetBase(name string, base float64) error {
	st, ok := s.byName[name]
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownStat, name)
	}
	if !finite(base) {
		return fmt.Errorf("%w: %v", ErrInvalidValue, base)
	}
	st.def.Base = base
	s.mark(st)
	s.flush()
	return nil
}

// Add adds a modifier from source that lasts until it is removed, and returns its ID.
func (s *Sheet) Add(source string, m Modifier) (ID, error) {
	return s.AddTimed(source, m, 0)
}

// AddTimed adds a modifier from source that lasts steps calls of Step and is removed by the last of them; 0 steps lasts until it is removed. It returns the modifier's ID.
func (s *Sheet) AddTimed(source string, m Modifier, steps int) (ID, error) {
	st, err := s.check(m, steps)
	if err != nil {
		return 0, err
	}
	id := s.add(st, source, m, steps)
	s.flush()
	return id, nil
}

// Apply adds every modifier of e from source, lasting e.Steps, and returns their IDs in the effect's order. It adds none of them when any is invalid.
//
// Applying an effect again stacks a second copy; to refresh a buff instead, use Refresh.
func (s *Sheet) Apply(source string, e Effect) ([]ID, error) {
	targets, err := s.checkEffect(e)
	if err != nil {
		return nil, err
	}
	ids := s.addEffect(targets, source, e)
	s.flush()
	return ids, nil
}

// Refresh removes every modifier added from source and applies e from source in their place, as one change: OnChange reports only each stat's value before the old modifiers left and after the new ones arrived, never the value in between. It returns the new modifiers' IDs in the effect's order. When any of e's modifiers is invalid it changes nothing.
//
// Refresh restarts a buff's duration without the listeners seeing the buff end and begin again.
func (s *Sheet) Refresh(source string, e Effect) ([]ID, error) {
	targets, err := s.checkEffect(e)
	if err != nil {
		return nil, err
	}
	s.detachWhere(func(o *entry) bool { return o.source == source })
	ids := s.addEffect(targets, source, e)
	s.flush()
	return ids, nil
}

// Remove removes the modifier with the given ID and reports whether it was on the sheet.
func (s *Sheet) Remove(id ID) bool {
	i := slices.IndexFunc(s.mods, func(e *entry) bool { return e.id == id })
	if i < 0 {
		return false
	}
	s.detach(s.mods[i])
	s.mods = slices.Delete(s.mods, i, i+1)
	s.flush()
	return true
}

// RemoveSource removes every modifier added from source, such as all of an unequipped item's bonuses, and returns how many it removed.
func (s *Sheet) RemoveSource(source string) int {
	return s.removeWhere(func(e *entry) bool { return e.source == source })
}

// Clear removes every modifier, leaving the stats at their base values.
func (s *Sheet) Clear() {
	s.removeWhere(func(*entry) bool { return true })
}

// Step advances timed modifiers by one fixed step and removes those whose last step this was. Call it once from each game.Loop Update.
func (s *Sheet) Step() {
	s.steps++
	s.removeWhere(func(e *entry) bool {
		if e.remaining == 0 {
			return false
		}
		e.remaining--
		return e.remaining == 0
	})
}

// Steps returns how many times Step has been called.
func (s *Sheet) Steps() uint64 {
	return s.steps
}

// Modifiers returns the modifiers on the sheet in the order they were added.
func (s *Sheet) Modifiers() []Active {
	out := make([]Active, len(s.mods))
	for i, e := range s.mods {
		out[i] = Active{ID: e.id, Source: e.source, Modifier: e.mod, Remaining: e.remaining}
	}
	return out
}

// Remaining returns how many more calls of Step the modifier lasts (0 for one without a time limit) and whether it is on the sheet.
func (s *Sheet) Remaining(id ID) (int, bool) {
	for _, e := range s.mods {
		if e.id == id {
			return e.remaining, true
		}
	}
	return 0, false
}

func (s *Sheet) check(m Modifier, steps int) (*stat, error) {
	if !m.Op.Valid() {
		return nil, fmt.Errorf("%w %q", ErrInvalidOp, m.Op)
	}
	if !finite(m.Value) {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, m.Value)
	}
	if steps < 0 {
		return nil, fmt.Errorf("%w %d", ErrInvalidSteps, steps)
	}
	st, ok := s.byName[m.Stat]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownStat, m.Stat)
	}
	return st, nil
}

func (s *Sheet) checkEffect(e Effect) ([]*stat, error) {
	targets := make([]*stat, len(e.Modifiers))
	for i, m := range e.Modifiers {
		st, err := s.check(m, e.Steps)
		if err != nil {
			return nil, fmt.Errorf("stats: effect %q: modifiers[%d]: %w", e.Name, i, err)
		}
		targets[i] = st
	}
	return targets, nil
}

func (s *Sheet) addEffect(targets []*stat, source string, e Effect) []ID {
	ids := make([]ID, len(e.Modifiers))
	for i, m := range e.Modifiers {
		ids[i] = s.add(targets[i], source, m, e.Steps)
	}
	return ids
}

func (s *Sheet) add(st *stat, source string, m Modifier, steps int) ID {
	s.nextID++
	e := &entry{id: s.nextID, source: source, mod: m, remaining: steps, stat: st}
	s.mods = append(s.mods, e)
	st.mods = append(st.mods, e)
	s.mark(st)
	return e.id
}

func (s *Sheet) removeWhere(match func(*entry) bool) int {
	n := s.detachWhere(match)
	if n > 0 {
		s.flush()
	}
	return n
}

// detachWhere removes the matching modifiers and marks their stats dirty without recomputing them.
func (s *Sheet) detachWhere(match func(*entry) bool) int {
	n := 0
	s.mods = slices.DeleteFunc(s.mods, func(e *entry) bool {
		if !match(e) {
			return false
		}
		s.detach(e)
		n++
		return true
	})
	return n
}

func (s *Sheet) detach(e *entry) {
	st := e.stat
	st.mods = slices.DeleteFunc(st.mods, func(o *entry) bool { return o == e })
	s.mark(st)
}

func (s *Sheet) mark(st *stat) {
	st.dirty = true
	s.dirty = true
}

// flush recomputes dirty stats in definition order and reports changed values. A change made from OnChange marks stats dirty again; the outermost flush keeps going until none are.
func (s *Sheet) flush() {
	if s.flushing {
		return
	}
	s.flushing = true
	defer func() { s.flushing = false }()
	for s.dirty {
		s.dirty = false
		for _, st := range s.stats {
			if !st.dirty {
				continue
			}
			st.dirty = false
			old := st.value
			st.value = st.compute()
			if st.value != old && s.OnChange != nil {
				s.OnChange(Change{Stat: st.def.Name, Old: old, New: st.value})
			}
		}
	}
}

// compute applies the stacking rule: (base + adds) * muls, or the winning override, then the bounds. Every partial result saturates at ±math.MaxFloat64, so finite inputs never give an infinite or NaN value (an overflowed sum times 0 is 0, not NaN) and the bounds always apply.
func (st *stat) compute() float64 {
	v := st.def.Base
	var override *entry
	for _, e := range st.mods {
		if e.mod.Op == OpAdd {
			v = saturate(v + e.mod.Value)
		}
	}
	for _, e := range st.mods {
		switch e.mod.Op {
		case OpMul:
			v = saturate(v * e.mod.Value)
		case OpOverride:
			if override == nil || e.mod.Priority >= override.mod.Priority {
				override = e
			}
		}
	}
	if override != nil {
		v = override.mod.Value
	}
	if st.def.Min != nil && v < *st.def.Min {
		v = *st.def.Min
	}
	if st.def.Max != nil && v > *st.def.Max {
		v = *st.def.Max
	}
	return v
}

// saturate maps an overflowed result to the largest finite value of its sign. Its inputs are finite, so v is never NaN.
func saturate(v float64) float64 {
	if math.IsInf(v, 1) {
		return math.MaxFloat64
	}
	if math.IsInf(v, -1) {
		return -math.MaxFloat64
	}
	return v
}
