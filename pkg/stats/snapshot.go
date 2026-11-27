package stats

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/pkg/save"
)

// ErrInvalidSnapshot reports a Snapshot that Sheet.Restore refuses because a stat is unknown,
// a value is non-finite, an op is invalid, or modifier IDs or remaining steps are invalid.
var ErrInvalidSnapshot = errors.New("stats: invalid snapshot")

// BaseState is one stat's base value in a Snapshot.
type BaseState struct {
	// Stat is the stat's name.
	Stat string `json:"stat" yaml:"stat"`
	// Base is the stat's base value before modifiers.
	Base float64 `json:"base" yaml:"base"`
}

// ModifierState is one active modifier in a Snapshot.
type ModifierState struct {
	// ID is the modifier's handle.
	ID ID `json:"id" yaml:"id"`
	// Source is the modifier's source tag.
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Stat is the target stat name.
	Stat string `json:"stat" yaml:"stat"`
	// Op is OpAdd, OpMul or OpOverride.
	Op Op `json:"op" yaml:"op"`
	// Value is the modifier's numeric operand.
	Value float64 `json:"value" yaml:"value"`
	// Priority orders override modifiers.
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`
	// Remaining is how many Step calls the modifier lasts (0 for permanent).
	Remaining int `json:"remaining,omitempty" yaml:"remaining,omitempty"`
}

// Snapshot is a deterministic, JSON-serializable snapshot of a Sheet's base values, active
// modifiers, modifier ID counter and step counter.
type Snapshot struct {
	// Bases lists each defined stat's base value in definition order.
	Bases []BaseState `json:"bases,omitempty" yaml:"bases,omitempty"`
	// Modifiers lists every active modifier in the order it was added.
	Modifiers []ModifierState `json:"modifiers,omitempty" yaml:"modifiers,omitempty"`
	// NextID is the last issued modifier ID so newly added modifiers never reuse an ID.
	NextID ID `json:"nextId,omitempty" yaml:"nextId,omitempty"`
	// Steps is the number of Step calls completed.
	Steps uint64 `json:"steps,omitempty" yaml:"steps,omitempty"`
}

// Snapshot returns s's runtime state as plain data. It shares no memory with s.
func (s *Sheet) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	bases := make([]BaseState, len(s.stats))
	for i, st := range s.stats {
		bases[i] = BaseState{Stat: st.def.Name, Base: st.def.Base}
	}
	var mods []ModifierState
	if len(s.mods) > 0 {
		mods = make([]ModifierState, len(s.mods))
		for i, e := range s.mods {
			mods[i] = ModifierState{
				ID:        e.id,
				Source:    e.source,
				Stat:      e.mod.Stat,
				Op:        e.mod.Op,
				Value:     e.mod.Value,
				Priority:  e.mod.Priority,
				Remaining: e.remaining,
			}
		}
	}
	return Snapshot{
		Bases:     bases,
		Modifiers: mods,
		NextID:    s.nextID,
		Steps:     s.steps,
	}
}

type preparedSheet struct {
	bases  []float64
	mods   []*entry
	byStat [][]*entry
	nextID ID
	steps  uint64
}

// Restore validates snap against s's defined stats and replaces s's base values, active modifiers
// and counters without firing OnChange. On error s is left unchanged.
func (s *Sheet) Restore(snap Snapshot) error {
	if s == nil {
		return fmt.Errorf("%w: nil sheet", ErrInvalidSnapshot)
	}
	p, err := s.prepareRestore(snap)
	if err != nil {
		return err
	}
	s.commitRestore(p)
	return nil
}

func (s *Sheet) prepareRestore(snap Snapshot) (*preparedSheet, error) {
	bases := make([]float64, len(s.stats))
	indexOf := make(map[string]int, len(s.stats))
	for i, st := range s.stats {
		bases[i] = st.initBase
		indexOf[st.def.Name] = i
	}
	seenBase := make(map[string]struct{}, len(snap.Bases))
	for _, b := range snap.Bases {
		idx, ok := indexOf[b.Stat]
		if !ok {
			return nil, fmt.Errorf("%w: %w %q", ErrInvalidSnapshot, ErrUnknownStat, b.Stat)
		}
		if _, dup := seenBase[b.Stat]; dup {
			return nil, fmt.Errorf("%w: duplicate base for stat %q", ErrInvalidSnapshot, b.Stat)
		}
		seenBase[b.Stat] = struct{}{}
		if !finite(b.Base) {
			return nil, fmt.Errorf("%w: stat %q base %w: %v", ErrInvalidSnapshot, b.Stat, ErrInvalidValue, b.Base)
		}
		bases[idx] = b.Base
	}
	byStat := make([][]*entry, len(s.stats))
	mods := make([]*entry, 0, len(snap.Modifiers))
	seenID := make(map[ID]struct{}, len(snap.Modifiers))
	var maxID ID
	for _, ms := range snap.Modifiers {
		if ms.ID == 0 {
			return nil, fmt.Errorf("%w: modifier on %q has zero ID", ErrInvalidSnapshot, ms.Stat)
		}
		if _, dup := seenID[ms.ID]; dup {
			return nil, fmt.Errorf("%w: duplicate modifier ID %d", ErrInvalidSnapshot, ms.ID)
		}
		seenID[ms.ID] = struct{}{}
		if ms.ID > maxID {
			maxID = ms.ID
		}
		m := Modifier{Stat: ms.Stat, Op: ms.Op, Value: ms.Value, Priority: ms.Priority}
		st, err := s.check(m, ms.Remaining)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidSnapshot, err)
		}
		idx := indexOf[ms.Stat]
		e := &entry{
			id:        ms.ID,
			source:    ms.Source,
			mod:       m,
			remaining: ms.Remaining,
			stat:      st,
		}
		mods = append(mods, e)
		byStat[idx] = append(byStat[idx], e)
	}
	nextID := snap.NextID
	if nextID < maxID {
		return nil, fmt.Errorf("%w: nextId %d is below highest modifier ID %d", ErrInvalidSnapshot, nextID, maxID)
	}
	return &preparedSheet{
		bases:  bases,
		mods:   mods,
		byStat: byStat,
		nextID: nextID,
		steps:  snap.Steps,
	}, nil
}

func (s *Sheet) commitRestore(p *preparedSheet) {
	s.mods = p.mods
	s.nextID = p.nextID
	s.steps = p.steps
	s.dirty = false
	for i, st := range s.stats {
		st.def.Base = p.bases[i]
		st.mods = p.byStat[i]
		st.dirty = false
		st.value = st.compute()
	}
}

func (s *Sheet) resetState() {
	s.mods = nil
	s.nextID = 0
	s.steps = 0
	s.dirty = false
	for _, st := range s.stats {
		st.def.Base = st.initBase
		st.mods = nil
		st.dirty = false
		st.value = st.compute()
	}
}

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores s through save.Game. Loading validates every stat and modifier before mutating s, and
// resets s to its initial base values with no modifiers when loading an older slot that omits it.
func (s *Sheet) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (Snapshot, error) {
			if s == nil {
				return Snapshot{}, errors.New("stats: nil sheet")
			}
			return s.Snapshot(), nil
		},
		func(snap Snapshot) (func(), error) {
			if s == nil {
				return nil, errors.New("stats: nil sheet")
			}
			p, err := s.prepareRestore(snap)
			if err != nil {
				return nil, err
			}
			return func() { s.commitRestore(p) }, nil
		},
		func() {
			if s != nil {
				s.resetState()
			}
		},
	)
}
