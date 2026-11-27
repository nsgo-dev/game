package stats

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const fixStats = "write the stats file as version: 1 YAML with valid stats and effects"

// CurrentVersion is the version of the stats file schema this package reads and writes.
const CurrentVersion = 1

// Op is how a modifier changes a stat. Its YAML form is the constant's string.
type Op string

const (
	// OpAdd adds the modifier's value to the stat's base, before any multiplication.
	OpAdd Op = "add"
	// OpMul multiplies the stat by the modifier's value after all additions; several multiply together.
	OpMul Op = "mul"
	// OpOverride replaces the computed value with the modifier's value, ignoring every add and mul; among several, the highest Priority wins and the latest added breaks ties.
	OpOverride Op = "override"
)

// Valid reports whether o is OpAdd, OpMul or OpOverride.
func (o Op) Valid() bool {
	return o == OpAdd || o == OpMul || o == OpOverride
}

// Def defines one stat: its name, base value and optional bounds.
type Def struct {
	// Name identifies the stat within a sheet; it must be non-empty and unique.
	Name string `yaml:"name"`
	// Base is the stat's value before modifiers.
	Base float64 `yaml:"base"`
	// Min, when set, is the lowest value the stat takes after modifiers.
	Min *float64 `yaml:"min,omitempty"`
	// Max, when set, is the highest value the stat takes after modifiers; it must not be below Min.
	Max *float64 `yaml:"max,omitempty"`
}

// Modifier changes one stat.
type Modifier struct {
	// Stat names the stat the modifier changes.
	Stat string `yaml:"stat"`
	// Op is how the modifier changes it.
	Op Op `yaml:"op"`
	// Value is the amount added, the factor multiplied by, or the value an override sets; it must be finite.
	Value float64 `yaml:"value"`
	// Priority orders override modifiers on the same stat: the highest wins. Add and mul modifiers ignore it.
	Priority int `yaml:"priority,omitempty"`
}

// Effect is a named group of modifiers applied together from one source, such as an item's bonuses or a buff.
type Effect struct {
	// Name identifies the effect within a file; it must be non-empty and unique.
	Name string `yaml:"name"`
	// Steps is how many fixed steps the effect's modifiers last once applied; 0 keeps them until they are removed.
	Steps int `yaml:"steps,omitempty"`
	// Modifiers are the effect's modifiers, applied in order.
	Modifiers []Modifier `yaml:"modifiers"`
}

// File is the root of a versioned YAML stats file: stat definitions and the effects that modify them.
type File struct {
	// Version is the schema version; it must equal CurrentVersion.
	Version int `yaml:"version"`
	// Stats are the stat definitions, in the order a sheet built from the file reports changes.
	Stats []Def `yaml:"stats"`
	// Effects are named groups of modifiers on those stats.
	Effects []Effect `yaml:"effects,omitempty"`
}

// Effect returns the effect with the given name.
func (f *File) Effect(name string) (Effect, bool) {
	if f == nil {
		return Effect{}, false
	}
	for _, e := range f.Effects {
		if e.Name == name {
			return e, true
		}
	}
	return Effect{}, false
}

// NewSheet returns a sheet holding the file's stats at their base values, with no modifiers.
func (f *File) NewSheet() (*Sheet, error) {
	if f == nil {
		return nil, errors.New("stats: nil file")
	}
	return NewSheet(f.Stats...)
}

// Parse decodes a YAML stats file and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*File, error) {
	f, err := parseFile(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixStats)
	}
	return f, nil
}

func parseFile(data []byte) (*File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixStats, "stats: empty stats file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("stats: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the stats file; remove the trailing --- document", "stats: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("stats: decode: %w", err)
	}
	if err := Validate(&f); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&f)
	return &f, nil
}

// Encode validates f and writes it as deterministic YAML with 2-space indentation: fields in declaration order and lists in their given order, so Parse then Encode reproduces an encoded file byte for byte.
func Encode(f *File) ([]byte, error) {
	if err := Validate(f); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(f)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("stats: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("stats: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks f against the version 1 schema: the version, unique non-empty names, finite numbers, Min not above Max, non-negative Steps, known ops, and modifiers that name defined stats.
func Validate(f *File) error {
	if f == nil {
		return loaderr.Errorf("", fixStats, "stats: nil file")
	}
	if f.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", CurrentVersion), "stats: unsupported version %d (want %d)", f.Version, CurrentVersion)
	}
	defined := make(map[string]bool, len(f.Stats))
	for i, d := range f.Stats {
		if err := validateDefAt(loaderr.Path("stats", i), d); err != nil {
			return err
		}
		if defined[d.Name] {
			return loaderr.Errorf(loaderr.Path("stats", i, "name"), "give each stat a unique name", "stats: stats[%d]: %w %q", i, ErrDuplicateStat, d.Name)
		}
		defined[d.Name] = true
	}
	effects := make(map[string]bool, len(f.Effects))
	for i, e := range f.Effects {
		ePath := loaderr.Path("effects", i)
		where := fmt.Sprintf("stats: effects[%d]", i)
		if e.Name == "" {
			return loaderr.Errorf(loaderr.Join(ePath, "name"), "give the effect a non-empty name", "%s: empty name", where)
		}
		if effects[e.Name] {
			return loaderr.Errorf(loaderr.Join(ePath, "name"), "give each effect a unique name", "%s: duplicate effect %q", where, e.Name)
		}
		effects[e.Name] = true
		if e.Steps < 0 {
			return loaderr.Errorf(loaderr.Join(ePath, "steps"), "set steps to a non-negative integer, or omit it for permanent", "%s (%s): %w %d", where, e.Name, ErrInvalidSteps, e.Steps)
		}
		if len(e.Modifiers) == 0 {
			return loaderr.Errorf(loaderr.Join(ePath, "modifiers"), "add at least one modifier to the effect", "%s (%s): no modifiers", where, e.Name)
		}
		for j, m := range e.Modifiers {
			mPath := loaderr.Join(ePath, "modifiers", j)
			if err := validateModifierAt(mPath, fmt.Sprintf("%s (%s): modifiers[%d]", where, e.Name, j), m); err != nil {
				return err
			}
			if !defined[m.Stat] {
				return loaderr.Errorf(loaderr.Join(mPath, "stat"), "name a stat declared under stats", "%s (%s): modifiers[%d]: %w %q", where, e.Name, j, ErrUnknownStat, m.Stat)
			}
		}
	}
	return nil
}

func validateDefAt(base string, d Def) error {
	if d.Name == "" {
		return loaderr.Errorf(loaderr.Join(base, "name"), "give the stat a non-empty name", "stats: %s: empty name", base)
	}
	if !finite(d.Base) {
		return loaderr.Errorf(loaderr.Join(base, "base"), "set base to a finite number", "stats: %s: stat %q: base: %w: %v", base, d.Name, ErrInvalidValue, d.Base)
	}
	if d.Min != nil && !finite(*d.Min) {
		return loaderr.Errorf(loaderr.Join(base, "min"), "set min to a finite number", "stats: %s: stat %q: min: %w: %v", base, d.Name, ErrInvalidValue, *d.Min)
	}
	if d.Max != nil && !finite(*d.Max) {
		return loaderr.Errorf(loaderr.Join(base, "max"), "set max to a finite number", "stats: %s: stat %q: max: %w: %v", base, d.Name, ErrInvalidValue, *d.Max)
	}
	if d.Min != nil && d.Max != nil && *d.Min > *d.Max {
		return loaderr.Errorf(loaderr.Join(base, "max"), "set max to at least min", "stats: %s: stat %q: min %v is above max %v", base, d.Name, *d.Min, *d.Max)
	}
	return nil
}

func validateModifierAt(base, where string, m Modifier) error {
	if m.Stat == "" {
		return loaderr.Errorf(loaderr.Join(base, "stat"), "set stat to a defined stat name", "%s: empty stat", where)
	}
	if !m.Op.Valid() {
		return loaderr.Errorf(loaderr.Join(base, "op"), "set op to add, mul or override", "%s: %w %q", where, ErrInvalidOp, m.Op)
	}
	if !finite(m.Value) {
		return loaderr.Errorf(loaderr.Join(base, "value"), "set value to a finite number", "%s: %w: %v", where, ErrInvalidValue, m.Value)
	}
	return nil
}

func validateDef(d Def) error {
	if d.Name == "" {
		return errors.New("empty name")
	}
	if !finite(d.Base) {
		return fmt.Errorf("stat %q: base: %w: %v", d.Name, ErrInvalidValue, d.Base)
	}
	if d.Min != nil && !finite(*d.Min) {
		return fmt.Errorf("stat %q: min: %w: %v", d.Name, ErrInvalidValue, *d.Min)
	}
	if d.Max != nil && !finite(*d.Max) {
		return fmt.Errorf("stat %q: max: %w: %v", d.Name, ErrInvalidValue, *d.Max)
	}
	if d.Min != nil && d.Max != nil && *d.Min > *d.Max {
		return fmt.Errorf("stat %q: min %v is above max %v", d.Name, *d.Min, *d.Max)
	}
	return nil
}

func validateModifier(m Modifier) error {
	if m.Stat == "" {
		return errors.New("empty stat")
	}
	if !m.Op.Valid() {
		return fmt.Errorf("%w %q", ErrInvalidOp, m.Op)
	}
	if !finite(m.Value) {
		return fmt.Errorf("%w: %v", ErrInvalidValue, m.Value)
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// Float returns a pointer to v, for Def's optional Min and Max.
func Float(v float64) *float64 { return &v }
