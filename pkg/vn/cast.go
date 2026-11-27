package vn

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// CastVersion is the cast file schema version this package reads and writes.
const CastVersion = 1

// Cast is a visual novel's cast file: the stage slots portraits stand in and the characters, each with a name plate and the portraits of its expressions. It is the YAML document ParseCast reads and EncodeCast writes, such as cast.yaml.
type Cast struct {
	// Version is the schema version; it must equal CastVersion (1).
	Version int `yaml:"version"`
	// Slots are the named places on the stage a portrait stands in, in document order. Empty uses DefaultSlots.
	Slots []Slot `yaml:"slots,omitempty"`
	// Characters are the cast members, in document order.
	Characters []Character `yaml:"characters"`
}

// Slot is a named place on the stage where a portrait stands.
type Slot struct {
	// Name identifies the slot in line tags (#at:left) and commands (<<show aiko left>>).
	Name string `yaml:"name"`
	// X is where the portrait's center stands, as a fraction of the stage's width from its left edge, from 0 to 1.
	X float32 `yaml:"x"`
}

// Character is one cast member.
type Character struct {
	// ID identifies the character in commands and is matched against a line's speaker before Name. It holds no whitespace.
	ID string `yaml:"id"`
	// Name is the text of the character's name plate and is also matched against a line's speaker; empty uses ID.
	Name string `yaml:"name,omitempty"`
	// Color is the name plate's color as a CSS color string (core.ParseColor); empty uses the style's NamePlateColor.
	Color string `yaml:"color,omitempty"`
	// Slot is the slot the character's portrait enters when a line or command names none; empty uses the cast's middle slot.
	Slot string `yaml:"slot,omitempty"`
	// DefaultExpression is the expression the portrait enters with; empty uses the first expression.
	DefaultExpression string `yaml:"defaultExpression,omitempty"`
	// Expressions are the character's portraits, each a frame of the portrait atlas.
	Expressions []Expression `yaml:"expressions"`
}

// Expression is one portrait of a character.
type Expression struct {
	// Name identifies the expression in line tags (#expr:smile) and commands (<<expr aiko smile>>).
	Name string `yaml:"name"`
	// Frame names the portrait's frame in the atlas a Style draws portraits from.
	Frame string `yaml:"frame"`
}

// DefaultSlots returns the slots a cast without any uses: left at 0.2, center at 0.5 and right at 0.8 of the stage's width.
func DefaultSlots() []Slot {
	return []Slot{{Name: "left", X: 0.2}, {Name: "center", X: 0.5}, {Name: "right", X: 0.8}}
}

// EffectiveSlots returns the cast's slots, or DefaultSlots when it declares none.
func (c *Cast) EffectiveSlots() []Slot {
	if c == nil || len(c.Slots) == 0 {
		return DefaultSlots()
	}
	return append([]Slot(nil), c.Slots...)
}

// Slot returns the slot named name and whether there is one.
func (c *Cast) Slot(name string) (Slot, bool) {
	for _, s := range c.EffectiveSlots() {
		if s.Name == name {
			return s, true
		}
	}
	return Slot{}, false
}

// defaultSlot is the slot a character without one of its own enters: the middle one of the cast's slots.
func (c *Cast) defaultSlot() string {
	slots := c.EffectiveSlots()
	return slots[(len(slots)-1)/2].Name
}

// Character returns the character whose ID is id, and whether there is one.
func (c *Cast) Character(id string) (*Character, bool) {
	if c == nil {
		return nil, false
	}
	for i := range c.Characters {
		if c.Characters[i].ID == id {
			return &c.Characters[i], true
		}
	}
	return nil, false
}

// Speaker returns the character a line's speaker names: the one whose ID is speaker, or else the first whose Name is.
func (c *Cast) Speaker(speaker string) (*Character, bool) {
	if speaker == "" {
		return nil, false
	}
	if ch, ok := c.Character(speaker); ok {
		return ch, true
	}
	if c == nil {
		return nil, false
	}
	for i := range c.Characters {
		if c.Characters[i].Name == speaker {
			return &c.Characters[i], true
		}
	}
	return nil, false
}

// DisplayName returns the text of the character's name plate: Name, or ID when Name is empty.
func (ch *Character) DisplayName() string {
	if ch.Name != "" {
		return ch.Name
	}
	return ch.ID
}

// EffectiveExpression returns the expression the character's portrait enters with: DefaultExpression, or the first expression when it is empty.
func (ch *Character) EffectiveExpression() string {
	if ch.DefaultExpression != "" {
		return ch.DefaultExpression
	}
	if len(ch.Expressions) > 0 {
		return ch.Expressions[0].Name
	}
	return ""
}

// Expression returns the expression named name, and whether there is one.
func (ch *Character) Expression(name string) (Expression, bool) {
	for _, e := range ch.Expressions {
		if e.Name == name {
			return e, true
		}
	}
	return Expression{}, false
}

// ParseCast decodes a YAML cast file and validates it. Unknown fields and trailing YAML documents are refused.
func ParseCast(data []byte) (*Cast, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("vn: empty cast document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Cast
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("vn: decode cast: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("vn: unexpected trailing YAML document in cast")
		}
		return nil, fmt.Errorf("vn: decode cast: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&c)
	return &c, nil
}

// EncodeCast validates c and writes it as deterministic 2-space YAML, fields in schema order and lists in their order, so that ParseCast reads back the same cast.
func EncodeCast(c *Cast) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(c)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("vn: encode cast: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("vn: encode cast: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks c against the version 1 schema: unique, non-empty slot names with X from 0 to 1; unique character IDs without whitespace; parseable colors; slots and default expressions that exist; and at least one expression per character, each with a frame and a unique name that no slot has, so that <<show>> reads its arguments one way.
func (c *Cast) Validate() error {
	if c == nil {
		return errors.New("vn: nil cast")
	}
	if c.Version != CastVersion {
		return fmt.Errorf("vn: unsupported cast version %d (want %d)", c.Version, CastVersion)
	}
	slots := make(map[string]bool, len(c.Slots))
	for i, s := range c.Slots {
		if !isToken(s.Name) {
			return fmt.Errorf("vn: slot %d: name %q must be non-empty without whitespace", i, s.Name)
		}
		if slots[s.Name] {
			return fmt.Errorf("vn: duplicate slot %q", s.Name)
		}
		slots[s.Name] = true
		if math.IsNaN(float64(s.X)) || s.X < 0 || s.X > 1 {
			return fmt.Errorf("vn: slot %q: x %v must be from 0 to 1", s.Name, s.X)
		}
	}
	if len(c.Characters) == 0 {
		return errors.New("vn: cast has no characters")
	}
	ids := make(map[string]bool, len(c.Characters))
	for i := range c.Characters {
		ch := &c.Characters[i]
		if !isToken(ch.ID) {
			return fmt.Errorf("vn: character %d: id %q must be non-empty without whitespace", i, ch.ID)
		}
		if ids[ch.ID] {
			return fmt.Errorf("vn: duplicate character %q", ch.ID)
		}
		ids[ch.ID] = true
		if ch.Color != "" {
			if _, err := core.ParseColor(ch.Color); err != nil {
				return fmt.Errorf("vn: character %q: color %q: %w", ch.ID, ch.Color, err)
			}
		}
		if ch.Slot != "" {
			if _, ok := c.Slot(ch.Slot); !ok {
				return fmt.Errorf("vn: character %q: unknown slot %q", ch.ID, ch.Slot)
			}
		}
		if len(ch.Expressions) == 0 {
			return fmt.Errorf("vn: character %q has no expressions", ch.ID)
		}
		exprs := make(map[string]bool, len(ch.Expressions))
		for _, e := range ch.Expressions {
			if !isToken(e.Name) {
				return fmt.Errorf("vn: character %q: expression name %q must be non-empty without whitespace", ch.ID, e.Name)
			}
			if exprs[e.Name] {
				return fmt.Errorf("vn: character %q: duplicate expression %q", ch.ID, e.Name)
			}
			exprs[e.Name] = true
			if _, ok := c.Slot(e.Name); ok {
				return fmt.Errorf("vn: character %q: expression %q has the name of a slot", ch.ID, e.Name)
			}
			if strings.TrimSpace(e.Frame) == "" {
				return fmt.Errorf("vn: character %q: expression %q has no frame", ch.ID, e.Name)
			}
		}
		if ch.DefaultExpression != "" && !exprs[ch.DefaultExpression] {
			return fmt.Errorf("vn: character %q: unknown default expression %q", ch.ID, ch.DefaultExpression)
		}
	}
	return nil
}

// CheckFrames reports the first expression whose frame atlas lacks, so a game finds a missing portrait as it loads rather than as a scene shows it.
func (c *Cast) CheckFrames(atlas *sprite.Atlas) error {
	if c == nil {
		return errors.New("vn: nil cast")
	}
	if atlas == nil {
		return errors.New("vn: nil portrait atlas")
	}
	for _, ch := range c.Characters {
		for _, e := range ch.Expressions {
			if _, ok := atlas.Frames[e.Frame]; !ok {
				return fmt.Errorf("vn: character %q: expression %q: atlas has no frame %q", ch.ID, e.Name, e.Frame)
			}
		}
	}
	return nil
}

// isToken reports whether s is non-empty and holds no whitespace, so it reads as one word in a tag or command.
func isToken(s string) bool {
	return s != "" && strings.IndexFunc(s, unicode.IsSpace) < 0
}
