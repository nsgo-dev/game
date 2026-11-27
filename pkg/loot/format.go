package loot

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the loot document schema version that Parse accepts and Encode writes.
const CurrentVersion = 1

// Limits that keep every roll bounded. Validate refuses documents that exceed them.
const (
	// MaxRolls is the largest number of weighted picks one roll of a table may make.
	MaxRolls = 1024
	// MaxCount is the largest quantity of an item entry, or the largest number of times a table entry rolls its table.
	MaxCount = 1 << 30
	// MaxWeight is the largest entry weight and the largest SoftStep.
	MaxWeight = 1e15
	// MaxGuaranteeAt is the largest GuaranteeAt and SoftFrom of a pity rule.
	MaxGuaranteeAt = 1 << 20
	// MaxRollSteps bounds the work of one Roller.Roll: the worst case of picks, sub-table rolls and drops a table can produce, which Validate computes from Rolls and Count maxima, must not exceed it. A weighted pick counts as many steps as its table has entries and pity rules, because it scans all of them.
	MaxRollSteps = 1 << 16
)

// Document is the root of a versioned YAML loot document.
type Document struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Tables are the document's tables. Their names are unique, and entries refer to them by name.
	Tables []Table `yaml:"tables"`
}

// Table is one named drop table.
type Table struct {
	// Name identifies the table to Roller.Roll and to entries of other tables.
	Name string `yaml:"name"`
	// Rolls is how many weighted picks one roll of the table makes, drawn uniformly from the range; nil means exactly one. It must be nil when Entries is empty.
	Rolls *Range `yaml:"rolls,omitempty"`
	// Guaranteed entries drop every time the table is rolled, before its weighted picks. Their Weight must be 0 and they cannot be Nothing.
	Guaranteed []Entry `yaml:"guaranteed,omitempty"`
	// Entries are the weighted choices of each pick. Every Weight must be positive, and no two entries may name the same item or the same table.
	Entries []Entry `yaml:"entries,omitempty"`
	// Pity rules raise the chance of one entry of Entries after a run of picks without it.
	Pity []Pity `yaml:"pity,omitempty"`
}

// Entry is one possible drop of a table. Exactly one of Item, Table and Nothing is set.
type Entry struct {
	// Item names the item this entry drops.
	Item string `yaml:"item,omitempty"`
	// Table names another table of the document that this entry rolls.
	Table string `yaml:"table,omitempty"`
	// Nothing marks an empty pick, which drops nothing; its weight sets the chance of a miss.
	Nothing bool `yaml:"nothing,omitempty"`
	// Weight is the entry's relative chance among the table's Entries. It must be positive there and 0 in Guaranteed.
	Weight float64 `yaml:"weight,omitempty"`
	// Count is the item's quantity, or how many times the table entry rolls its table, drawn uniformly from the range; nil means exactly one. A Nothing entry has no Count.
	Count *Range `yaml:"count,omitempty"`
}

// Pity is a rule that raises the chance of one entry of its table after a run of picks without it. Exactly one of Item and Table names the target entry, and at least one of GuaranteeAt and SoftFrom is set.
type Pity struct {
	// Name identifies the rule's miss counter in State; it is unique within its table.
	Name string `yaml:"name"`
	// Item names the target entry by its Item.
	Item string `yaml:"item,omitempty"`
	// Table names the target entry by its Table.
	Table string `yaml:"table,omitempty"`
	// GuaranteeAt, when positive, forces the target on the GuaranteeAt-th pick since it was last picked. When several rules of a table force a pick at once, the first one in order wins and the others keep counting.
	GuaranteeAt int `yaml:"guaranteeAt,omitempty"`
	// SoftFrom, when positive, starts the soft ramp: on the n-th pick since the target was last picked, with n >= SoftFrom, the target's weight grows by (n-SoftFrom+1)*SoftStep. It must be below GuaranteeAt when both are set.
	SoftFrom int `yaml:"softFrom,omitempty"`
	// SoftStep is the weight the soft ramp adds per pick. It is positive exactly when SoftFrom is.
	SoftStep float64 `yaml:"softStep,omitempty"`
}

// Range is an inclusive integer range [Min, Max]. In YAML it is written as a single integer when Min equals Max and as a mapping with min and max otherwise.
type Range struct {
	Min int
	Max int
}

// Exactly returns a pointer to the range [n, n].
func Exactly(n int) *Range { return &Range{Min: n, Max: n} }

// Between returns a pointer to the range [min, max].
func Between(min, max int) *Range { return &Range{Min: min, Max: max} }

// MarshalYAML writes r as an integer when Min equals Max and as a min/max mapping otherwise.
func (r Range) MarshalYAML() (any, error) {
	if r.Min == r.Max {
		return r.Min, nil
	}
	return struct {
		Min int `yaml:"min"`
		Max int `yaml:"max"`
	}{r.Min, r.Max}, nil
}

// UnmarshalYAML reads an integer or a mapping with exactly the keys min and max.
func (r *Range) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var n int
		if err := value.Decode(&n); err != nil {
			return err
		}
		*r = Range{Min: n, Max: n}
		return nil
	case yaml.MappingNode:
		var out Range
		var seenMin, seenMax bool
		for i := 0; i+1 < len(value.Content); i += 2 {
			key, val := value.Content[i], value.Content[i+1]
			var dst *int
			switch key.Value {
			case "min":
				if seenMin {
					return fmt.Errorf("line %d: duplicate range key min", key.Line)
				}
				seenMin, dst = true, &out.Min
			case "max":
				if seenMax {
					return fmt.Errorf("line %d: duplicate range key max", key.Line)
				}
				seenMax, dst = true, &out.Max
			default:
				return fmt.Errorf("line %d: field %s not found in type loot.Range", key.Line, key.Value)
			}
			if err := val.Decode(dst); err != nil {
				return err
			}
		}
		if !seenMin || !seenMax {
			return fmt.Errorf("line %d: a range needs both min and max", value.Line)
		}
		*r = out
		return nil
	default:
		return fmt.Errorf("line %d: a range is an integer or a min/max mapping", value.Line)
	}
}

// Parse decodes a YAML loot document and validates it. Unknown fields, duplicate keys and trailing YAML documents are rejected. Empty lists decode as nil, so the result equals the Document that its Encode text parses to.
func Parse(data []byte) (*Document, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("loot: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("loot: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("loot: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("loot: decode: %w", err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	normalize(&doc)
	return &doc, nil
}

// normalize turns empty lists such as `pity: []` into nil, which is how Encode omits them, so Parse(Encode(doc)) equals doc.
func normalize(doc *Document) {
	for i := range doc.Tables {
		t := &doc.Tables[i]
		if len(t.Guaranteed) == 0 {
			t.Guaranteed = nil
		}
		if len(t.Entries) == 0 {
			t.Entries = nil
		}
		if len(t.Pity) == 0 {
			t.Pity = nil
		}
	}
}

// Encode validates doc and writes it as deterministic 2-space YAML that Parse decodes to an equal Document.
func Encode(doc *Document) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("loot: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("loot: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks doc against the version 1 schema: names, entry kinds, weights, ranges, pity rules, table references without cycles, and the MaxRollSteps bound on every table.
func Validate(doc *Document) error {
	_, err := compile(doc)
	return err
}

func validName(s string) bool {
	return s != "" && strings.TrimSpace(s) == s
}

func validateRange(where string, r *Range, max int) error {
	if r == nil {
		return nil
	}
	if r.Min < 0 || r.Max < r.Min || r.Max > max {
		return fmt.Errorf("loot: %s: range [%d, %d] must satisfy 0 <= min <= max <= %d", where, r.Min, r.Max, max)
	}
	return nil
}

func validWeight(w float64) bool {
	return w > 0 && w <= MaxWeight && !math.IsNaN(w)
}

func validateEntry(where string, e *Entry, guaranteed bool) error {
	kinds := 0
	if e.Item != "" {
		kinds++
		if !validName(e.Item) {
			return fmt.Errorf("loot: %s: item name %q has surrounding spaces", where, e.Item)
		}
	}
	if e.Table != "" {
		kinds++
		if !validName(e.Table) {
			return fmt.Errorf("loot: %s: table name %q has surrounding spaces", where, e.Table)
		}
	}
	if e.Nothing {
		kinds++
	}
	if kinds != 1 {
		return fmt.Errorf("loot: %s: exactly one of item, table and nothing must be set", where)
	}
	if guaranteed {
		if e.Nothing {
			return fmt.Errorf("loot: %s: a guaranteed entry cannot be nothing", where)
		}
		if e.Weight != 0 {
			return fmt.Errorf("loot: %s: a guaranteed entry has no weight", where)
		}
	} else if !validWeight(e.Weight) {
		return fmt.Errorf("loot: %s: weight %v must be in (0, %g]", where, e.Weight, MaxWeight)
	}
	if e.Nothing && e.Count != nil {
		return fmt.Errorf("loot: %s: a nothing entry has no count", where)
	}
	return validateRange(where+": count", e.Count, MaxCount)
}

func validatePity(where string, p *Pity) error {
	if !validName(p.Name) {
		return fmt.Errorf("loot: %s: name %q must be non-empty without surrounding spaces", where, p.Name)
	}
	if (p.Item == "") == (p.Table == "") {
		return fmt.Errorf("loot: %s: exactly one of item and table must name the target", where)
	}
	if p.GuaranteeAt < 0 || p.GuaranteeAt > MaxGuaranteeAt {
		return fmt.Errorf("loot: %s: guaranteeAt %d must be in [0, %d]", where, p.GuaranteeAt, MaxGuaranteeAt)
	}
	if p.SoftFrom < 0 || p.SoftFrom > MaxGuaranteeAt {
		return fmt.Errorf("loot: %s: softFrom %d must be in [0, %d]", where, p.SoftFrom, MaxGuaranteeAt)
	}
	if p.GuaranteeAt == 0 && p.SoftFrom == 0 {
		return fmt.Errorf("loot: %s: set guaranteeAt, softFrom or both", where)
	}
	if p.SoftFrom > 0 {
		if !validWeight(p.SoftStep) {
			return fmt.Errorf("loot: %s: softStep %v must be in (0, %g]", where, p.SoftStep, MaxWeight)
		}
		if p.GuaranteeAt > 0 && p.SoftFrom >= p.GuaranteeAt {
			return fmt.Errorf("loot: %s: softFrom %d must be below guaranteeAt %d", where, p.SoftFrom, p.GuaranteeAt)
		}
	} else if p.SoftStep != 0 {
		return fmt.Errorf("loot: %s: softStep needs softFrom", where)
	}
	return nil
}
