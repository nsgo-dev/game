package statedump

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/nsgo-dev/game/packages/game/entities"
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
	// Progress is the running transition's eased progress in [0, 1]; it is 0 when no transition runs.
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

// Validate checks that d uses FormatVersion, has non-negative counts, a scene progress in [0, 1] that is 0 unless a transition runs, non-empty value names with valid JSON, and entities in strictly ascending slot order with non-zero generations, parents present in the dump, and components with non-empty names in strictly ascending order holding valid JSON.
func Validate(d *Dump) error {
	if d == nil {
		return errors.New("statedump: nil dump")
	}
	if d.Version != FormatVersion {
		return fmt.Errorf("statedump: unsupported version %d (want %d)", d.Version, FormatVersion)
	}
	if d.Rate < 0 {
		return fmt.Errorf("statedump: invalid rate %d", d.Rate)
	}
	if d.Steps < 0 {
		return fmt.Errorf("statedump: invalid steps %d", d.Steps)
	}
	if s := d.Scene; s != nil {
		if s.Depth < 0 {
			return fmt.Errorf("statedump: invalid scene depth %d", s.Depth)
		}
		if math.IsNaN(s.Progress) || s.Progress < 0 || s.Progress > 1 {
			return fmt.Errorf("statedump: scene progress %v is outside [0, 1]", s.Progress)
		}
		if !s.Transitioning && s.Progress != 0 {
			return fmt.Errorf("statedump: scene progress %v without a transition (want 0)", s.Progress)
		}
	}
	for name, data := range d.Values {
		if name == "" {
			return errors.New("statedump: empty value name")
		}
		if !json.Valid(data) {
			return fmt.Errorf("statedump: value %q is not valid JSON", name)
		}
	}
	indices := make(map[uint32]struct{}, len(d.Entities))
	for i, e := range d.Entities {
		if i > 0 && e.Index <= d.Entities[i-1].Index {
			return fmt.Errorf("statedump: entity %d is not after entity %d", e.Index, d.Entities[i-1].Index)
		}
		if e.Generation == 0 {
			return fmt.Errorf("statedump: entity %d has zero generation", e.Index)
		}
		indices[e.Index] = struct{}{}
		for j, c := range e.Components {
			if c.Type == "" {
				return fmt.Errorf("statedump: entity %d component %d has an empty type", e.Index, j)
			}
			if j > 0 && c.Type <= e.Components[j-1].Type {
				return fmt.Errorf("statedump: entity %d component %q is not after %q", e.Index, c.Type, e.Components[j-1].Type)
			}
			if len(c.Data) == 0 {
				return fmt.Errorf("statedump: entity %d component %q has no data", e.Index, c.Type)
			}
			if !json.Valid(c.Data) {
				return fmt.Errorf("statedump: entity %d component %q is not valid JSON", e.Index, c.Type)
			}
		}
	}
	for _, e := range d.Entities {
		if e.Parent == nil {
			continue
		}
		if *e.Parent == e.Index {
			return fmt.Errorf("statedump: entity %d is its own parent", e.Index)
		}
		if _, ok := indices[*e.Parent]; !ok {
			return fmt.Errorf("statedump: entity %d has unknown parent %d", e.Index, *e.Parent)
		}
	}
	return nil
}

// fileDoc is the YAML layout of a Dump; its field order is the key order Encode writes.
type fileDoc struct {
	Version  int                `yaml:"version"`
	Seed     uint64             `yaml:"seed"`
	Rate     int                `yaml:"rate,omitempty"`
	Steps    int                `yaml:"steps"`
	Scene    *SceneState        `yaml:"scene,omitempty"`
	Values   map[string]rawJSON `yaml:"values,omitempty"`
	Entities []fileEntity       `yaml:"entities"`
}

type fileEntity struct {
	Index      uint32          `yaml:"index"`
	Generation uint32          `yaml:"generation"`
	Parent     *uint32         `yaml:"parent,omitempty"`
	Components []fileComponent `yaml:"components,omitempty"`
}

type fileComponent struct {
	Type string  `yaml:"type"`
	Data rawJSON `yaml:"data"`
}

// rawJSON is JSON data written to YAML as the mapping, sequence or scalar it encodes.
type rawJSON json.RawMessage

// MarshalYAML implements yaml.Marshaler.
func (r rawJSON) MarshalYAML() (any, error) {
	return jsonToNode(r)
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (r *rawJSON) UnmarshalYAML(n *yaml.Node) error {
	data, err := nodeToJSON(n)
	if err != nil {
		return err
	}
	*r = data
	return nil
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
		doc.Values = make(map[string]rawJSON, len(d.Values))
		for name, data := range d.Values {
			doc.Values[name] = rawJSON(data)
		}
	}
	for i, e := range d.Entities {
		fe := fileEntity{Index: e.Index, Generation: e.Generation, Parent: e.Parent}
		if len(e.Components) > 0 {
			fe.Components = make([]fileComponent, len(e.Components))
			for j, c := range e.Components {
				fe.Components[j] = fileComponent{Type: c.Type, Data: rawJSON(c.Data)}
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
		return nil, errors.New("statedump: empty dump")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc fileDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("statedump: decode: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("statedump: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("statedump: decode: %w", err)
	}
	d := &Dump{
		Version: doc.Version,
		Seed:    doc.Seed,
		Rate:    doc.Rate,
		Steps:   doc.Steps,
		Scene:   doc.Scene,
	}
	if len(doc.Values) > 0 {
		d.Values = make(map[string]json.RawMessage, len(doc.Values))
		for name, data := range doc.Values {
			d.Values[name] = json.RawMessage(data)
		}
	}
	if len(doc.Entities) > 0 {
		d.Entities = make([]entities.EntitySnapshot, len(doc.Entities))
		for i, fe := range doc.Entities {
			e := entities.EntitySnapshot{Index: fe.Index, Generation: fe.Generation, Parent: fe.Parent}
			if len(fe.Components) > 0 {
				e.Components = make([]entities.ComponentSnapshot, len(fe.Components))
				for j, c := range fe.Components {
					e.Components[j] = entities.ComponentSnapshot{Type: c.Type, Data: json.RawMessage(c.Data)}
				}
			}
			d.Entities[i] = e
		}
	}
	if err := Validate(d); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("statedump: read %q: %w", path, err)
	}
	d, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("statedump: read %q: %w", path, err)
	}
	return d, nil
}
