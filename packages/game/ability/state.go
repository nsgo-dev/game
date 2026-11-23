package ability

import (
	"errors"
	"fmt"
)

// State is a Caster's runtime state at the end of a step, for save slots, rollback netcode and replays. It holds step counts, not durations, so a restored Caster continues on exactly the same steps.
//
// It encodes as JSON or YAML with the field names below. Restore refuses a State taken from a Caster with a different rate or ability set.
type State struct {
	// Rate is the Caster's steps a second.
	Rate int `json:"rate" yaml:"rate"`
	// Step is the Caster's step count.
	Step uint64 `json:"step" yaml:"step"`
	// Abilities holds each ability's charges and timers, in set order.
	Abilities []AbilityState `json:"abilities" yaml:"abilities"`
	// Active is the ability casting or channeling, or nil when none is.
	Active *ActiveState `json:"active,omitempty" yaml:"active,omitempty"`
}

// AbilityState is one ability's charges and timers in a State.
type AbilityState struct {
	// ID is the ability's ID.
	ID string `json:"id" yaml:"id"`
	// Charges is how many uses are stored.
	Charges int `json:"charges" yaml:"charges"`
	// Cooldown is how many steps of cooldown are left.
	Cooldown int `json:"cooldown,omitempty" yaml:"cooldown,omitempty"`
	// Recharge is how many steps are left until the next charge comes back.
	Recharge int `json:"recharge,omitempty" yaml:"recharge,omitempty"`
}

// ActiveState is the ability casting or channeling in a State.
type ActiveState struct {
	// Ability is the ability's ID.
	Ability string `json:"ability" yaml:"ability"`
	// Phase is PhaseCasting or PhaseChanneling.
	Phase Phase `json:"phase" yaml:"phase"`
	// Elapsed is how many steps of Phase have run.
	Elapsed int `json:"elapsed" yaml:"elapsed"`
}

// Snapshot returns the Caster's state. Restoring it into a Caster made from the same Set at the same rate, and giving both the same inputs, produces the same events on the same steps.
func (c *Caster) Snapshot() State {
	st := State{
		Rate:      c.rate,
		Step:      c.step,
		Abilities: make([]AbilityState, len(c.slots)),
	}
	for i := range c.slots {
		s := &c.slots[i]
		st.Abilities[i] = AbilityState{ID: s.id, Charges: s.charges, Cooldown: s.cooldown, Recharge: s.recharge}
	}
	if c.active >= 0 {
		st.Active = &ActiveState{Ability: c.slots[c.active].id, Phase: c.phase, Elapsed: c.elapsed}
	}
	return st
}

// Restore replaces the Caster's state with st, without sending events. It refuses, and changes nothing, when st's rate or abilities differ from the Caster's or a value is out of the range a Caster could reach.
func (c *Caster) Restore(st *State) error {
	if st == nil {
		return errNilState
	}
	if st.Rate != c.rate {
		return errState("rate %d, want %d", st.Rate, c.rate)
	}
	if len(st.Abilities) != len(c.slots) {
		return errState("%d abilities, want %d", len(st.Abilities), len(c.slots))
	}
	for i, a := range st.Abilities {
		s := &c.slots[i]
		if a.ID != s.id {
			return errState("ability %d is %q, want %q", i, a.ID, s.id)
		}
		if a.Charges < 0 || a.Charges > s.maxCharges {
			return errState("ability %q charges %d is outside 0..%d", a.ID, a.Charges, s.maxCharges)
		}
		if a.Cooldown < 0 || a.Cooldown > s.cooldownSteps {
			return errState("ability %q cooldown %d is outside 0..%d", a.ID, a.Cooldown, s.cooldownSteps)
		}
		if a.Recharge < 0 || a.Recharge > s.rechargeSteps {
			return errState("ability %q recharge %d is outside 0..%d", a.ID, a.Recharge, s.rechargeSteps)
		}
		if (a.Recharge > 0) != (a.Charges < s.maxCharges) {
			return errState("ability %q has %d of %d charges with recharge %d", a.ID, a.Charges, s.maxCharges, a.Recharge)
		}
	}
	active, phase, elapsed := -1, PhaseIdle, 0
	if st.Active != nil {
		i, ok := c.index[st.Active.Ability]
		if !ok {
			return errState("active ability %q is not in the set", st.Active.Ability)
		}
		s := &c.slots[i]
		var limit int
		switch st.Active.Phase {
		case PhaseCasting:
			limit = s.castSteps
		case PhaseChanneling:
			limit = s.channelSteps
		default:
			return errState("active ability %q phase %q is not %q or %q", s.id, st.Active.Phase, PhaseCasting, PhaseChanneling)
		}
		if st.Active.Elapsed < 0 || st.Active.Elapsed >= limit {
			return errState("active ability %q %s elapsed %d is outside 0..%d", s.id, st.Active.Phase, st.Active.Elapsed, limit-1)
		}
		active, phase, elapsed = i, st.Active.Phase, st.Active.Elapsed
	}
	c.step = st.Step
	for i, a := range st.Abilities {
		s := &c.slots[i]
		s.charges, s.cooldown, s.recharge = a.Charges, a.Cooldown, a.Recharge
	}
	c.active, c.phase, c.elapsed = active, phase, elapsed
	c.queue = c.queue[:0]
	return nil
}

// errState builds a Restore error.
func errState(format string, args ...any) error {
	return fmt.Errorf("ability: restore: "+format, args...)
}

var errNilState = errors.New("ability: restore: nil state")
