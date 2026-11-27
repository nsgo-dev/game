package statedump

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the schema version Encode writes and Parse accepts.
const FormatVersion = 1

// Dump is the state of a game after a number of fixed steps.
type Dump struct {
	// Version is the schema version; it must equal FormatVersion.
	Version int
	// Seed is the random seed the run started from.
	Seed uint64
	// Rate is the run's fixed steps per second, or 0 when unknown.
	Rate int
	// Steps is how many fixed steps the run took before the dump.
	Steps int
	// Scene is the scene stack's state, or nil when the game has no scene.Manager.
	Scene *SceneState
	// Values holds game-wide state outside the entity world (score, level, RNG position), each as its JSON encoding, keyed by a non-empty name.
	Values map[string]json.RawMessage
	// Entities are the live entities in ascending slot index, as entities.World.Snapshot records them.
	Entities []entities.EntitySnapshot
}

// SceneState is the state of a scene.Manager's stack.
type SceneState struct {
	// Depth is the number of scenes on the stack.
	Depth int `yaml:"depth"`
	// Current names the scene at the top of the stack, or is empty when the game does not name its scenes.
	Current string `yaml:"current,omitempty"`
	// Transitioning reports whether a Switch transition is running.
	Transitioning bool `yaml:"transitioning,omitempty"`
	// Progress is the running transition's eased progress, as scene.Manager.TransitionProgress reports it; it is 0 when no transition runs. It usually lies in [0, 1], but a curve that overshoots, such as an ease-out-back, takes it past either end for part of the transition.
	Progress float64 `yaml:"progress,omitempty"`
}

// SetValue stores v's JSON encoding in d.Values under name.
func (d *Dump) SetValue(name string, v any) error {
	if d == nil {
		return errors.New("statedump: nil dump")
	}
	if name == "" {
		return errors.New("statedump: empty value name")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("statedump: value %q: %w", name, err)
	}
	if d.Values == nil {
		d.Values = make(map[string]json.RawMessage)
	}
	d.Values[name] = data
	return nil
}

// Snapshot returns d's entities as an entities.WorldSnapshot that entities.World.Restore loads.
func (d *Dump) Snapshot() entities.WorldSnapshot {
	if d == nil {
		return entities.WorldSnapshot{}
	}
	return entities.WorldSnapshot{Entities: d.Entities}
}

// Validate checks that d uses FormatVersion, has non-negative counts, a finite scene progress that is 0 unless a transition runs, non-empty value names with valid JSON, and entities in strictly ascending slot order with non-zero generations, parents present in the dump, and components with non-empty names in strictly ascending order holding valid JSON.
func Validate(d *Dump) error {
	if d == nil {
		return loaderr.Errorf("", "pass a non-nil *statedump.Dump to Validate", "statedump: nil dump")
	}
	if d.Version != FormatVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the state dump", FormatVersion), "statedump: unsupported version %d (want %d)", d.Version, FormatVersion)
	}
	if d.Rate < 0 {
		return loaderr.Errorf("rate", "set rate to a non-negative integer", "statedump: invalid rate %d", d.Rate)
	}
	if d.Steps < 0 {
		return loaderr.Errorf("steps", "set steps to a non-negative integer", "statedump: invalid steps %d", d.Steps)
	}
	if s := d.Scene; s != nil {
		if s.Depth < 0 {
			return loaderr.Errorf("scene.depth", "set scene.depth to a non-negative integer", "statedump: invalid scene depth %d", s.Depth)
		}
		if math.IsNaN(s.Progress) || math.IsInf(s.Progress, 0) {
			return loaderr.Errorf("scene.progress", "set scene.progress to a finite number", "statedump: scene progress %v is not finite", s.Progress)
		}
		if !s.Transitioning && s.Progress != 0 {
			return loaderr.Errorf("scene.progress", "set scene.progress to 0 when scene.transitioning is false", "statedump: scene progress %v without a transition (want 0)", s.Progress)
		}
	}
	if len(d.Values) > 0 {
		names := make([]string, 0, len(d.Values))
		for name := range d.Values {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			data := d.Values[name]
			if name == "" {
				return loaderr.Errorf("values", "use a non-empty string key for every entry in values", "statedump: empty value name")
			}
			if !json.Valid(data) {
				return loaderr.Errorf("values."+name, "encode the value as valid JSON", "statedump: value %q is not valid JSON", name)
			}
		}
	}
	indices := make(map[uint32]struct{}, len(d.Entities))
	for i, e := range d.Entities {
		ePath := fmt.Sprintf("entities[%d]", i)
		if i > 0 && e.Index <= d.Entities[i-1].Index {
			return loaderr.Errorf(ePath+".index", "order entities in strictly ascending index order without duplicates", "statedump: entity %d is not after entity %d", e.Index, d.Entities[i-1].Index)
		}
		if e.Generation == 0 {
			return loaderr.Errorf(ePath+".generation", "set generation to a positive integer (at least 1)", "statedump: entity %d has zero generation", e.Index)
		}
		indices[e.Index] = struct{}{}
		for j, c := range e.Components {
			cPath := fmt.Sprintf("%s.components[%d]", ePath, j)
			if c.Type == "" {
				return loaderr.Errorf(cPath+".type", "set type to a non-empty component type name", "statedump: entity %d component %d has an empty type", e.Index, j)
			}
			if j > 0 && c.Type <= e.Components[j-1].Type {
				return loaderr.Errorf(cPath+".type", "order entity components in strictly ascending type name order without duplicates", "statedump: entity %d component %q is not after %q", e.Index, c.Type, e.Components[j-1].Type)
			}
			if len(c.Data) == 0 {
				return loaderr.Errorf(cPath+".data", "provide component data for this component", "statedump: entity %d component %q has no data", e.Index, c.Type)
			}
			if !json.Valid(c.Data) {
				return loaderr.Errorf(cPath+".data", "encode component data as valid JSON", "statedump: entity %d component %q is not valid JSON", e.Index, c.Type)
			}
		}
	}
	for i, e := range d.Entities {
		if e.Parent == nil {
			continue
		}
		ePath := fmt.Sprintf("entities[%d].parent", i)
		if *e.Parent == e.Index {
			return loaderr.Errorf(ePath, "remove the self-referencing parent or point it to another live entity", "statedump: entity %d is its own parent", e.Index)
		}
		if _, ok := indices[*e.Parent]; !ok {
			return loaderr.Errorf(ePath, "set parent to the index of an entity present in entities or omit it", "statedump: entity %d has unknown parent %d", e.Index, *e.Parent)
		}
	}
	return nil
}

// fileDoc is the YAML layout of a Dump; its field order is the key order Encode writes.
//
// Values and component data are yaml.Node rather than a type with UnmarshalYAML: yaml.v3 never calls an Unmarshaler for a null node and leaves the field at its zero value, which would lose a JSON null. A Node of Kind 0 is a missing key; an explicit null is a scalar with the !!null tag. Values is a mapping Encode builds in byte order of the names, because yaml.v3's own map order compares runs of digits as numbers.
type fileDoc struct {
	Version  int          `yaml:"version"`
	Seed     uint64       `yaml:"seed"`
	Rate     int          `yaml:"rate,omitempty"`
	Steps    int          `yaml:"steps"`
	Scene    *SceneState  `yaml:"scene,omitempty"`
	Values   yaml.Node    `yaml:"values,omitempty"`
	Entities []fileEntity `yaml:"entities"`
}

type fileEntity struct {
	Index      uint32          `yaml:"index"`
	Generation uint32          `yaml:"generation"`
	Parent     *uint32         `yaml:"parent,omitempty"`
	Components []fileComponent `yaml:"components,omitempty"`
}

type fileComponent struct {
	Type string    `yaml:"type"`
	Data yaml.Node `yaml:"data"`
}

// valuesNode returns values as a YAML mapping in ascending byte order of the names.
func valuesNode(values map[string]json.RawMessage) (yaml.Node, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	m := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, name := range names {
		v, err := jsonToNode(values[name])
		if err != nil {
			return yaml.Node{}, fmt.Errorf("statedump: value %q: %w", name, err)
		}
		m.Content = append(m.Content, strNode(name), v)
	}
	return m, nil
}

// parseValues turns the values mapping back into JSON by name, refusing keys that are not plain strings and duplicate names.
func parseValues(n *yaml.Node) (map[string]json.RawMessage, error) {
	switch {
	case n.Kind == 0, n.Kind == yaml.ScalarNode && n.Tag == tagNull:
		return nil, nil
	case n.Kind != yaml.MappingNode || n.Anchor != "":
		return nil, &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Field:   "values",
			Problem: fmt.Sprintf("statedump: line %d: values must be a mapping", n.Line),
			Fix:     "write values as a YAML mapping of string names to JSON-compatible values without anchors",
		}
	}
	values := make(map[string]json.RawMessage, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind != yaml.ScalarNode || k.Tag != tagStr || k.Anchor != "" {
			return nil, &loaderr.Error{
				Line:    k.Line,
				Column:  k.Column,
				Field:   "values",
				Problem: fmt.Sprintf("statedump: line %d: value name must be a string (quote it)", k.Line),
				Fix:     "quote the value name so YAML parses it as a string without anchors",
			}
		}
		if _, dup := values[k.Value]; dup {
			return nil, &loaderr.Error{
				Line:    k.Line,
				Column:  k.Column,
				Field:   "values." + k.Value,
				Problem: fmt.Sprintf("statedump: line %d: duplicate value %q", k.Line, k.Value),
				Fix:     "remove or rename the duplicate key under values",
			}
		}
		data, err := nodeToJSON(n.Content[i+1])
		if err != nil {
			if le, ok := loaderr.As(err); ok && le.Field == "" {
				le.Field = "values." + k.Value
			}
			return nil, err
		}
		values[k.Value] = data
	}
	return values, nil
}

// Encode validates d and writes it as deterministic 2-space YAML.
func Encode(d *Dump) ([]byte, error) {
	if err := Validate(d); err != nil {
		return nil, err
	}
	doc := fileDoc{
		Version:  d.Version,
		Seed:     d.Seed,
		Rate:     d.Rate,
		Steps:    d.Steps,
		Entities: make([]fileEntity, len(d.Entities)),
	}
	if d.Scene != nil {
		s := *d.Scene
		doc.Scene = &s
	}
	if len(d.Values) > 0 {
		values, err := valuesNode(d.Values)
		if err != nil {
			return nil, err
		}
		doc.Values = values
	}
	for i, e := range d.Entities {
		fe := fileEntity{Index: e.Index, Generation: e.Generation, Parent: e.Parent}
		if len(e.Components) > 0 {
			fe.Components = make([]fileComponent, len(e.Components))
			for j, c := range e.Components {
				data, err := jsonToNode(c.Data)
				if err != nil {
					return nil, fmt.Errorf("statedump: entity %d component %q: %w", e.Index, c.Type, err)
				}
				fe.Components[j] = fileComponent{Type: c.Type, Data: *data}
			}
		}
		doc.Entities[i] = fe
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("statedump: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("statedump: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Parse reads a version 1 YAML dump, refusing unknown fields, trailing documents, and data JSON cannot hold (aliases, non-string keys, binary, NaN and infinities), then runs Validate.
func Parse(data []byte) (*Dump, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a state dump starting with version: %d", FormatVersion), "statedump: empty dump"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc fileDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("statedump: decode: %w", err), "", data, "check the YAML syntax and field names in the state dump")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the state dump and remove any --- separator", "statedump: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("statedump: decode: %w", err), "", data, "remove trailing YAML content after the state dump")
	}
	d := &Dump{
		Version: doc.Version,
		Seed:    doc.Seed,
		Rate:    doc.Rate,
		Steps:   doc.Steps,
		Scene:   doc.Scene,
	}
	values, err := parseValues(&doc.Values)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	if len(values) > 0 {
		d.Values = values
	}
	if len(doc.Entities) > 0 {
		d.Entities = make([]entities.EntitySnapshot, len(doc.Entities))
		for i, fe := range doc.Entities {
			e := entities.EntitySnapshot{Index: fe.Index, Generation: fe.Generation, Parent: fe.Parent}
			if len(fe.Components) > 0 {
				e.Components = make([]entities.ComponentSnapshot, len(fe.Components))
				for j, c := range fe.Components {
					var raw json.RawMessage
					if c.Data.Kind != 0 {
						if raw, err = nodeToJSON(&c.Data); err != nil {
							if le, ok := loaderr.As(err); ok && le.Field == "" {
								le.Field = fmt.Sprintf("entities[%d].components[%d].data", i, j)
							}
							return nil, loaderr.Locate(err, "", data, "")
						}
					}
					e.Components[j] = entities.ComponentSnapshot{Type: c.Type, Data: raw}
				}
			}
			d.Entities[i] = e
		}
	}
	if err := Validate(d); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return d, nil
}

// WriteFile encodes d and writes it to path.
func WriteFile(path string, d *Dump) error {
	data, err := Encode(d)
	if err != nil {
		return fmt.Errorf("statedump: write %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("statedump: write %q: %w", path, err)
	}
	return nil
}

// ReadFile reads and parses the dump at path.
func ReadFile(path string) (*Dump, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("statedump: read %q: %w", path, err), path, nil, "check that the state dump file exists and is readable")
	}
	d, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return d, nil
}
