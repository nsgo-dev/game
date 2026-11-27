package ability

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the ability set schema version this package reads and writes.
const CurrentVersion = 1

// Limits on an ability set, checked by Validate so a crafted file cannot make a Caster allocate or loop without bound.
const (
	// MaxAbilities is the most abilities one Set holds.
	MaxAbilities = 1024
	// MaxCharges is the largest Def.Charges.
	MaxCharges = 99
	// MaxDuration is the longest cooldown, recharge, cast, channel or tick interval.
	MaxDuration = 24 * time.Hour
	// MaxNameLength is the longest ability ID, action, resource or condition name, in bytes.
	MaxNameLength = 64
)

// Trigger says which input edge of an ability's action tries to activate it.
type Trigger string

const (
	// TriggerPress tries to activate the ability on the step its action is first pressed. It is the default when Def.Trigger is empty.
	TriggerPress Trigger = "press"
	// TriggerHold tries to activate the ability on every step its action is held, and cancels its cast or channel on the step the action is no longer held.
	TriggerHold Trigger = "hold"
)

// Set is the root of a versioned YAML ability set: the abilities one kind of character, unit or item can use.
type Set struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Abilities are the set's abilities. Their order is the order a Caster advances them in, and the order it tries abilities that share an action.
	Abilities []Def `yaml:"abilities"`
}

// Def defines one ability: what using it costs, how often it can be used, how long it takes and when it is allowed.
//
// Durations are written as Go duration strings such as "500ms" or "1m30s". A Caster turns each into a whole number of its fixed steps, rounding up, so a duration that is not a multiple of the step still waits at least that long.
type Def struct {
	// ID uniquely names the ability within its Set. It is 1 to MaxNameLength bytes of ASCII letters, digits, '_', '-' and '.'.
	ID string `yaml:"id"`
	// Name is an optional display name or localization key.
	Name string `yaml:"name,omitempty"`
	// Action is the input action that activates the ability, as bound on an input.Actions. Empty means only code activates it, through Caster.TryActivate.
	Action string `yaml:"action,omitempty"`
	// Trigger is TriggerPress or TriggerHold; empty means TriggerPress. TriggerHold needs an Action.
	Trigger Trigger `yaml:"trigger,omitempty"`
	// Costs are the resources one use spends, each resource at most once. They are checked when activation starts and spent when the cast completes.
	Costs []Cost `yaml:"costs,omitempty"`
	// Cooldown is the shortest time between two uses, counted from the step a use completes its cast. It applies even while charges remain.
	Cooldown time.Duration `yaml:"cooldown,omitempty"`
	// Charges is how many uses the ability stores, 0 meaning 1. More than one charge needs a Recharge.
	Charges int `yaml:"charges,omitempty"`
	// Recharge is how long one spent charge takes to come back. Spent charges come back one after another, not all at once. Zero means a use never spends a charge.
	Recharge time.Duration `yaml:"recharge,omitempty"`
	// CastTime is how long the ability casts before it takes effect. Zero takes effect on the step it is activated.
	CastTime time.Duration `yaml:"castTime,omitempty"`
	// ChannelTime is how long the ability channels after it takes effect. Zero means it does not channel.
	ChannelTime time.Duration `yaml:"channelTime,omitempty"`
	// TickInterval is how often a channel ticks. Zero means the channel does not tick; otherwise it needs a ChannelTime and must not exceed it.
	TickInterval time.Duration `yaml:"tickInterval,omitempty"`
	// Conditions are names that must all hold to start the ability and to keep casting or channeling it. A leading '!' means the named condition must not hold, such as "!stunned". Each condition appears at most once.
	Conditions []string `yaml:"conditions,omitempty,flow"`
}

// Cost is one resource an ability spends.
type Cost struct {
	// Resource names the resource, such as "mana" or "stamina".
	Resource string `yaml:"resource"`
	// Amount is how much of Resource one use spends; it must be finite and positive.
	Amount float64 `yaml:"amount"`
}

// MaxChargesOrOne returns Charges, or 1 when Charges is 0.
func (d *Def) MaxChargesOrOne() int {
	if d.Charges <= 0 {
		return 1
	}
	return d.Charges
}

// EffectiveTrigger returns Trigger, or TriggerPress when Trigger is empty.
func (d *Def) EffectiveTrigger() Trigger {
	if d.Trigger == "" {
		return TriggerPress
	}
	return d.Trigger
}

// Parse decodes a YAML ability set and validates it. Unknown fields, duplicate keys and trailing YAML documents are refused.
func Parse(data []byte) (*Set, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("ability: empty ability set")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var set Set
	if err := dec.Decode(&set); err != nil {
		return nil, fmt.Errorf("ability: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("ability: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("ability: decode: %w", err)
	}
	if err := Validate(&set); err != nil {
		return nil, err
	}
	// Encode writes no abilities as "abilities: []" and omits empty cost and condition lists, so read them that way; a Set then equals the one its own encoding parses to.
	if set.Abilities == nil {
		set.Abilities = []Def{}
	}
	for i := range set.Abilities {
		d := &set.Abilities[i]
		if len(d.Costs) == 0 {
			d.Costs = nil
		}
		if len(d.Conditions) == 0 {
			d.Conditions = nil
		}
	}
	return &set, nil
}

// Encode validates set and writes it as deterministic 2-space YAML: fields in a fixed order, abilities in set order, durations as Go duration strings. Parse reads the result back to an equal Set.
func Encode(set *Set) ([]byte, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(set); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("ability: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ability: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks set against the version 1 schema rules and the package limits.
func Validate(set *Set) error {
	if set == nil {
		return errors.New("ability: nil ability set")
	}
	if set.Version != CurrentVersion {
		return fmt.Errorf("ability: unsupported version %d (want %d)", set.Version, CurrentVersion)
	}
	if len(set.Abilities) > MaxAbilities {
		return fmt.Errorf("ability: %d abilities, more than %d", len(set.Abilities), MaxAbilities)
	}
	ids := make(map[string]struct{}, len(set.Abilities))
	for i := range set.Abilities {
		d := &set.Abilities[i]
		if err := checkName(d.ID); err != nil {
			return fmt.Errorf("ability: ability %d id: %w", i, err)
		}
		if _, dup := ids[d.ID]; dup {
			return fmt.Errorf("ability: duplicate ability id %q", d.ID)
		}
		ids[d.ID] = struct{}{}
		if err := validateDef(d); err != nil {
			return fmt.Errorf("ability: ability %q: %w", d.ID, err)
		}
	}
	return nil
}

func validateDef(d *Def) error {
	if d.Action != "" {
		if err := checkName(d.Action); err != nil {
			return fmt.Errorf("action: %w", err)
		}
	}
	switch d.Trigger {
	case "", TriggerPress:
	case TriggerHold:
		if d.Action == "" {
			return errors.New("trigger hold needs an action")
		}
	default:
		return fmt.Errorf("trigger %q is not %q or %q", d.Trigger, TriggerPress, TriggerHold)
	}
	resources := make(map[string]struct{}, len(d.Costs))
	for i, c := range d.Costs {
		if err := checkName(c.Resource); err != nil {
			return fmt.Errorf("cost %d resource: %w", i, err)
		}
		if _, dup := resources[c.Resource]; dup {
			return fmt.Errorf("resource %q is costed twice", c.Resource)
		}
		resources[c.Resource] = struct{}{}
		if math.IsNaN(c.Amount) || math.IsInf(c.Amount, 0) || c.Amount <= 0 {
			return fmt.Errorf("cost of %q is %v, want a finite positive amount", c.Resource, c.Amount)
		}
	}
	if d.Charges < 0 || d.Charges > MaxCharges {
		return fmt.Errorf("charges %d is outside 0..%d", d.Charges, MaxCharges)
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"cooldown", d.Cooldown},
		{"recharge", d.Recharge},
		{"castTime", d.CastTime},
		{"channelTime", d.ChannelTime},
		{"tickInterval", d.TickInterval},
	} {
		if f.d < 0 || f.d > MaxDuration {
			return fmt.Errorf("%s %v is outside 0..%v", f.name, f.d, MaxDuration)
		}
	}
	if d.Charges > 1 && d.Recharge == 0 {
		return fmt.Errorf("%d charges need a recharge", d.Charges)
	}
	if d.TickInterval > 0 {
		if d.ChannelTime == 0 {
			return errors.New("tickInterval needs a channelTime")
		}
		if d.TickInterval > d.ChannelTime {
			return fmt.Errorf("tickInterval %v is longer than channelTime %v", d.TickInterval, d.ChannelTime)
		}
	}
	seen := make(map[string]struct{}, len(d.Conditions))
	for _, cond := range d.Conditions {
		name, _ := splitCondition(cond)
		if err := checkName(name); err != nil {
			return fmt.Errorf("condition %q: %w", cond, err)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("condition %q appears twice", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// splitCondition returns cond's name and whether it is negated with a leading '!'.
func splitCondition(cond string) (name string, negated bool) {
	if len(cond) > 0 && cond[0] == '!' {
		return cond[1:], true
	}
	return cond, false
}

func checkName(s string) error {
	if s == "" {
		return errors.New("is empty")
	}
	if len(s) > MaxNameLength {
		return fmt.Errorf("is %d bytes, more than %d", len(s), MaxNameLength)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		default:
			return fmt.Errorf("%q has %q; use ASCII letters, digits, '_', '-' and '.'", s, c)
		}
	}
	return nil
}
