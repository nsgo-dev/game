package inventory

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the item catalog schema version this package reads and writes.
const CurrentVersion = 1

// MaxStackLimit is the largest MaxStack an item definition may declare.
const MaxStackLimit = 1 << 30

// MaxContainerSize is the largest number of slots a Container may have.
const MaxContainerSize = 1 << 16

// ItemDef defines one kind of item.
type ItemDef struct {
	// ID uniquely identifies the item within its catalog and is what stacks, snapshots and saves refer to. It must be non-empty and contain no whitespace or control characters.
	ID string `yaml:"id"`
	// Name is the item's display name or localization key.
	Name string `yaml:"name,omitempty"`
	// Description is optional longer display text or a localization key.
	Description string `yaml:"description,omitempty"`
	// Icon is an optional asset URI for the item's icon, such as "asset:icons/potion.png".
	Icon string `yaml:"icon,omitempty"`
	// MaxStack is how many of the item one slot holds, in [1, MaxStackLimit]; 0 means 1 and is written by omitting the field, since Parse refuses an explicit "maxStack: 0".
	MaxStack int `yaml:"maxStack,omitempty"`
	// Equip is the equipment kind the item fits, matched against EquipSlot.Kind; empty means the item cannot be equipped.
	Equip string `yaml:"equip,omitempty"`
	// Tags are free-form labels such as "consumable" or "quest", unique within the item.
	Tags []string `yaml:"tags,omitempty,flow"`
	// Stats holds finite numeric values such as damage, armor, weight or price, keyed by non-empty names.
	Stats map[string]float64 `yaml:"stats,omitempty"`
}

// EffectiveMaxStack returns MaxStack, or 1 when MaxStack is 0.
func (d ItemDef) EffectiveMaxStack() int {
	if d.MaxStack <= 0 {
		return 1
	}
	return d.MaxStack
}

// HasTag reports whether the item carries tag.
func (d ItemDef) HasTag(tag string) bool {
	return slices.Contains(d.Tags, tag)
}

func (d ItemDef) clone() ItemDef {
	d.Tags = slices.Clone(d.Tags)
	if d.Stats != nil {
		stats := make(map[string]float64, len(d.Stats))
		for k, v := range d.Stats {
			stats[k] = v
		}
		d.Stats = stats
	}
	return d
}

// catalogFile is the YAML document layout of a Catalog.
type catalogFile struct {
	Version int       `yaml:"version"`
	Items   []ItemDef `yaml:"items"`
}

// Catalog is an immutable, validated set of item definitions. Build one with Parse or NewCatalog.
type Catalog struct {
	items []ItemDef
	byID  map[string]int
}

// NewCatalog validates defs and returns a catalog holding copies of them in the given order.
func NewCatalog(defs ...ItemDef) (*Catalog, error) {
	items := make([]ItemDef, len(defs))
	for i, d := range defs {
		items[i] = d.clone()
	}
	return newCatalog(items)
}

func newCatalog(items []ItemDef) (*Catalog, error) {
	byID := make(map[string]int, len(items))
	for i := range items {
		d := &items[i]
		if err := validateDef(i, d); err != nil {
			return nil, err
		}
		if _, dup := byID[d.ID]; dup {
			return nil, fmt.Errorf("inventory: duplicate item id %q", d.ID)
		}
		byID[d.ID] = i
	}
	return &Catalog{items: items, byID: byID}, nil
}

// Parse decodes a version 1 YAML item catalog and validates it. Unknown fields, duplicate item IDs, invalid definitions and trailing YAML documents are refused.
func Parse(data []byte) (*Catalog, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("inventory: empty item catalog")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc catalogFile
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("inventory: decode item catalog: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("inventory: unexpected trailing YAML document in item catalog")
		}
		return nil, fmt.Errorf("inventory: decode item catalog: %w", err)
	}
	if doc.Version != CurrentVersion {
		return nil, fmt.Errorf("inventory: unsupported item catalog version %d (want %d)", doc.Version, CurrentVersion)
	}
	if err := refuseExplicitZeroStack(data); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&doc)
	return newCatalog(doc.Items)
}

// refuseExplicitZeroStack rejects a written "maxStack: 0". ItemDef.MaxStack treats 0 as omitted and Encode leaves it out, so accepting the explicit spelling would make a load-and-save round trip drop the line.
func refuseExplicitZeroStack(data []byte) error {
	var stacks struct {
		Items []struct {
			ID       string `yaml:"id"`
			MaxStack *int   `yaml:"maxStack"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal(data, &stacks); err != nil {
		return fmt.Errorf("inventory: decode item catalog: %w", err)
	}
	for _, it := range stacks.Items {
		if it.MaxStack != nil && *it.MaxStack == 0 {
			return fmt.Errorf("inventory: item %q maxStack 0 must be in [1, %d] or omitted", it.ID, MaxStackLimit)
		}
	}
	return nil
}

// Encode writes the catalog as deterministic 2-space YAML (definitions in catalog order, stats keys sorted) that Parse reads back unchanged.
func (c *Catalog) Encode() ([]byte, error) {
	doc := catalogFile{Version: CurrentVersion, Items: c.items}
	if doc.Items == nil {
		doc.Items = []ItemDef{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(&doc)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("inventory: encode item catalog: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("inventory: encode item catalog: %w", err)
	}
	return buf.Bytes(), nil
}

// Item returns a copy of the definition with the given ID.
func (c *Catalog) Item(id string) (ItemDef, bool) {
	i, ok := c.byID[id]
	if !ok {
		return ItemDef{}, false
	}
	return c.items[i].clone(), true
}

// Has reports whether the catalog defines id.
func (c *Catalog) Has(id string) bool {
	_, ok := c.byID[id]
	return ok
}

// Items returns copies of every definition in catalog order.
func (c *Catalog) Items() []ItemDef {
	out := make([]ItemDef, len(c.items))
	for i := range c.items {
		out[i] = c.items[i].clone()
	}
	return out
}

// Len returns the number of definitions in the catalog.
func (c *Catalog) Len() int { return len(c.items) }

// def returns the stored definition for id without copying it; callers must not modify it.
func (c *Catalog) def(id string) (*ItemDef, bool) {
	i, ok := c.byID[id]
	if !ok {
		return nil, false
	}
	return &c.items[i], true
}

func validateDef(index int, d *ItemDef) error {
	if !validName(d.ID) {
		return fmt.Errorf("inventory: item %d has invalid id %q (want non-empty, no whitespace or control characters)", index, d.ID)
	}
	if d.MaxStack < 0 || d.MaxStack > MaxStackLimit {
		return fmt.Errorf("inventory: item %q maxStack %d must be in [1, %d] or omitted", d.ID, d.MaxStack, MaxStackLimit)
	}
	if d.Equip != "" && !validName(d.Equip) {
		return fmt.Errorf("inventory: item %q has invalid equip kind %q (want no whitespace or control characters)", d.ID, d.Equip)
	}
	for i, tag := range d.Tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("inventory: item %q tag %d is empty", d.ID, i)
		}
		if slices.Contains(d.Tags[:i], tag) {
			return fmt.Errorf("inventory: item %q has duplicate tag %q", d.ID, tag)
		}
	}
	for name, v := range d.Stats {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("inventory: item %q has an empty stat name", d.ID)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("inventory: item %q stat %q is not finite", d.ID, name)
		}
	}
	return nil
}

// validName reports whether s is a non-empty identifier without whitespace or control characters.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
