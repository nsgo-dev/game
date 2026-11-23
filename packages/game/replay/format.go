package replay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"go.yaml.in/yaml/v3"
)

// recordingDoc is the YAML form of a Recording. Field order here is the key order Encode writes.
type recordingDoc struct {
	Version    int       `yaml:"version"`
	Seed       uint64    `yaml:"seed"`
	Rate       int       `yaml:"rate"`
	Actions    []string  `yaml:"actions,omitempty,flow"`
	Axes       []string  `yaml:"axes,omitempty,flow"`
	TotalSteps int       `yaml:"total_steps"`
	Steps      []stepDoc `yaml:"steps,omitempty"`
}

// stepDoc is the YAML form of a StepEvent, naming actions and axes instead of indexing them. Step is a pointer so a missing step key is refused rather than read as step 0.
type stepDoc struct {
	Step    *int       `yaml:"step"`
	Press   []string   `yaml:"press,omitempty,flow"`
	Release []string   `yaml:"release,omitempty,flow"`
	Axes    axisValues `yaml:"axes,omitempty"`
}

// MarshalYAML writes the step as a flow mapping so every event stays on one line.
func (s stepDoc) MarshalYAML() (any, error) {
	type plain stepDoc
	var n yaml.Node
	if err := n.Encode(plain(s)); err != nil {
		return nil, err
	}
	n.Style = yaml.FlowStyle
	return &n, nil
}

// axisValue is one axis name and its new value within a step.
type axisValue struct {
	name  string
	value float64
}

// axisValues is an ordered mapping from axis name to value. It keeps the order written in the file so a load and save round trip does not reorder keys.
type axisValues []axisValue

// MarshalYAML writes the axis values as a flow mapping in slice order.
func (a axisValues) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	for _, av := range a {
		var k, v yaml.Node
		if err := k.Encode(av.name); err != nil {
			return nil, err
		}
		if err := v.Encode(av.value); err != nil {
			return nil, err
		}
		n.Content = append(n.Content, &k, &v)
	}
	return n, nil
}

// UnmarshalYAML reads a mapping from axis name to number, keeping key order and refusing repeated names.
func (a *axisValues) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: axes must map axis names to values", node.Line)
	}
	out := make(axisValues, 0, len(node.Content)/2)
	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		k, v := node.Content[i], node.Content[i+1]
		var av axisValue
		if err := k.Decode(&av.name); err != nil {
			return err
		}
		if _, dup := seen[av.name]; dup {
			return fmt.Errorf("line %d: axis %q is listed more than once", k.Line, av.name)
		}
		seen[av.name] = struct{}{}
		if err := v.Decode(&av.value); err != nil {
			return err
		}
		out = append(out, av)
	}
	*a = out
	return nil
}

// Encode validates r and writes it as deterministic YAML text: a header with the version, seed, rate, watched names and total step count, then one line per step on which input changed.
//
//	version: 2
//	seed: 99537266
//	rate: 60
//	actions: [left, right, jump, dash]
//	axes: [move_x]
//	total_steps: 254
//	steps:
//	  - {step: 6, press: [right], axes: {move_x: 1}}
//	  - {step: 85, press: [jump, dash]}
//	  - {step: 95, release: [jump, dash]}
//
// Each step line names the actions it presses and releases and the new values of the axes that changed. The same Recording always encodes to the same bytes, and Decode followed by Encode reproduces a file Encode wrote byte for byte.
func (r *Recording) Encode() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	doc := recordingDoc{
		Version:    r.Version,
		Seed:       r.Seed,
		Rate:       r.Rate,
		Actions:    r.Actions,
		Axes:       r.Axes,
		TotalSteps: r.TotalSteps,
	}
	if len(r.Events) > 0 {
		doc.Steps = make([]stepDoc, len(r.Events))
		for i, ev := range r.Events {
			step := ev.Step
			sd := stepDoc{Step: &step}
			if len(ev.Press) > 0 {
				sd.Press = make([]string, len(ev.Press))
				for j, idx := range ev.Press {
					sd.Press[j] = r.Actions[idx]
				}
			}
			if len(ev.Release) > 0 {
				sd.Release = make([]string, len(ev.Release))
				for j, idx := range ev.Release {
					sd.Release[j] = r.Actions[idx]
				}
			}
			if len(ev.Axes) > 0 {
				sd.Axes = make(axisValues, len(ev.Axes))
				for j, ch := range ev.Axes {
					sd.Axes[j] = axisValue{name: r.Axes[ch.Index], value: ch.Value}
				}
			}
			doc.Steps[i] = sd
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("replay: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("replay: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode reads a Recording from the YAML text Encode writes, including text a person has edited, and validates it.
//
// Unknown keys, a missing step number, names that are not in the header's actions or axes, and trailing YAML documents are refused. Steps must be listed in increasing order. A recording with another version, such as the compact JSON of version 1, is refused with an error naming that version.
func Decode(data []byte) (*Recording, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("replay: decode: empty recording")
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return nil, errors.New("replay: decode: data is gzip-compressed; recordings are plain YAML text")
	}

	// Check the version before the strict decode so a recording in another version's layout reports its version instead of its first unknown key.
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("replay: decode: %w", err)
	}
	if probe.Version != FormatVersion {
		return nil, fmt.Errorf("replay: unsupported version %d (want %d)", probe.Version, FormatVersion)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc recordingDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("replay: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("replay: decode: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("replay: decode: %w", err)
	}

	rec := &Recording{
		Version:    doc.Version,
		Seed:       doc.Seed,
		Rate:       doc.Rate,
		Actions:    doc.Actions,
		Axes:       doc.Axes,
		TotalSteps: doc.TotalSteps,
	}
	if err := validateNames("action", rec.Actions); err != nil {
		return nil, err
	}
	if err := validateNames("axis", rec.Axes); err != nil {
		return nil, err
	}
	actionIdx := indexNames(rec.Actions)
	axisIdx := indexNames(rec.Axes)

	if len(doc.Steps) > 0 {
		rec.Events = make([]StepEvent, len(doc.Steps))
		for i, sd := range doc.Steps {
			if sd.Step == nil {
				return nil, fmt.Errorf("replay: decode: steps entry %d has no step", i)
			}
			ev := StepEvent{Step: *sd.Step}
			var err error
			if ev.Press, err = lookupActions(actionIdx, ev.Step, "presses", sd.Press); err != nil {
				return nil, err
			}
			if ev.Release, err = lookupActions(actionIdx, ev.Step, "releases", sd.Release); err != nil {
				return nil, err
			}
			if len(sd.Axes) > 0 {
				ev.Axes = make([]AxisChange, len(sd.Axes))
				for j, av := range sd.Axes {
					idx, ok := axisIdx[av.name]
					if !ok {
						return nil, fmt.Errorf("replay: step %d sets axis %q, which is not in axes", ev.Step, av.name)
					}
					ev.Axes[j] = AxisChange{Index: idx, Value: av.value}
				}
			}
			rec.Events[i] = ev
		}
	}
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	return rec, nil
}

func indexNames(names []string) map[string]int {
	m := make(map[string]int, len(names))
	for i, name := range names {
		m[name] = i
	}
	return m
}

func lookupActions(idx map[string]int, step int, verb string, names []string) ([]int, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]int, len(names))
	for i, name := range names {
		j, ok := idx[name]
		if !ok {
			return nil, fmt.Errorf("replay: step %d %s action %q, which is not in actions", step, verb, name)
		}
		out[i] = j
	}
	return out, nil
}

// SaveFile writes rec to path as the YAML text Encode produces. The conventional extension is ".replay.yaml".
func SaveFile(path string, rec *Recording) error {
	data, err := rec.Encode()
	if err != nil {
		return fmt.Errorf("replay: save %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("replay: save %q: %w", path, err)
	}
	return nil
}

// LoadFile reads and decodes the YAML Recording at path.
func LoadFile(path string) (*Recording, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: load %q: %w", path, err)
	}
	rec, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("replay: load %q: %w", path, err)
	}
	return rec, nil
}
