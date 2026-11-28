package replay

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// recordingDoc is the YAML form of a Recording. Field order here is the key order Encode writes. Seed and TotalSteps are pointers so a missing key is refused rather than read as 0, which would replay from the wrong seed or as an empty run.
type recordingDoc struct {
	Version    int               `yaml:"version"`
	Seed       *uint64           `yaml:"seed"`
	Rate       int               `yaml:"rate"`
	Actions    []string          `yaml:"actions,omitempty,flow"`
	Axes       []string          `yaml:"axes,omitempty,flow"`
	TotalSteps *int              `yaml:"total_steps"`
	Build      string            `yaml:"build,omitempty"`
	Ticket     string            `yaml:"ticket,omitempty"`
	Result     map[string]string `yaml:"result,omitempty"`
	Steps      []stepDoc         `yaml:"steps,omitempty"`
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

// MarshalYAML writes the axis values as a flow mapping in slice order. Negative zero is written as 0, because YAML reads the plain scalar -0 back as the integer 0 and the text would otherwise change on the next save.
func (a axisValues) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	for _, av := range a {
		var k, v yaml.Node
		if err := k.Encode(av.name); err != nil {
			return nil, err
		}
		value := av.value
		if value == 0 {
			value = 0
		}
		if err := v.Encode(value); err != nil {
			return nil, err
		}
		n.Content = append(n.Content, &k, &v)
	}
	return n, nil
}

// UnmarshalYAML reads a mapping from axis name to number, keeping key order and refusing repeated names. A written -0.0 is read as 0 so every zero decodes and encodes the same way.
func (a *axisValues) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return &loaderr.Error{Line: node.Line, Column: node.Column, Problem: "axes must map axis names to values", Fix: "write axes as a mapping such as {move_x: 1}"}
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
			return &loaderr.Error{Line: k.Line, Column: k.Column, Problem: fmt.Sprintf("axis %q is listed more than once", av.name), Fix: "set each axis once per step"}
		}
		seen[av.name] = struct{}{}
		if err := v.Decode(&av.value); err != nil {
			return err
		}
		if av.value == 0 {
			av.value = 0
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
	seed, totalSteps := r.Seed, r.TotalSteps
	doc := recordingDoc{
		Version:    r.Version,
		Seed:       &seed,
		Rate:       r.Rate,
		Actions:    r.Actions,
		Axes:       r.Axes,
		TotalSteps: &totalSteps,
		Build:      r.Build, Ticket: r.Ticket, Result: r.Result,
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
// Unknown keys, a missing seed, total_steps or step number, names that are not in the header's actions or axes, and trailing YAML documents are refused. Steps must be listed in increasing order. A recording with another version, such as the compact JSON of version 1, is refused with an error naming that version.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile, as LoadFile does.
func Decode(data []byte) (*Recording, error) {
	rec, err := decode(data)
	return rec, loaderr.Locate(err, "", data, fixRecordAgain)
}

func decode(data []byte) (*Recording, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", "pass the text of a recording that Encode or SaveFile wrote", "empty recording")
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return nil, loaderr.Errorf("", "decompress the file first; recordings are plain YAML text", "data is gzip-compressed")
	}

	// Check the version before the strict decode so a recording in another version's layout reports its version instead of its first unknown key.
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	if probe.Version != FormatVersion {
		return nil, &loaderr.Error{Field: "version", Problem: fmt.Sprintf("unsupported version %d", probe.Version), Expected: fmt.Sprint(FormatVersion), Fix: fixRecordAgain}
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc recordingDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			e := &loaderr.Error{Line: extra.Line, Column: extra.Column, Problem: "unexpected trailing YAML document", Fix: "keep one recording in the file; remove the document that follows it"}
			if len(extra.Content) > 0 {
				e.Line, e.Column = extra.Content[0].Line, extra.Content[0].Column
			}
			return nil, e
		}
		return nil, err
	}

	if doc.Seed == nil {
		return nil, loaderr.Errorf("", "add seed: the random seed the recorded run started from", "missing seed")
	}
	if doc.TotalSteps == nil {
		return nil, loaderr.Errorf("", "add total_steps: the number of steps recorded", "missing total_steps")
	}
	rec := &Recording{
		Version:    doc.Version,
		Seed:       *doc.Seed,
		Rate:       doc.Rate,
		Actions:    doc.Actions,
		Axes:       doc.Axes,
		TotalSteps: *doc.TotalSteps, Build: doc.Build, Ticket: doc.Ticket, Result: doc.Result,
	}
	if err := validateNames("actions", "action", rec.Actions); err != nil {
		return nil, err
	}
	if err := validateNames("axes", "axis", rec.Axes); err != nil {
		return nil, err
	}
	actionIdx := indexNames(rec.Actions)
	axisIdx := indexNames(rec.Axes)

	if len(doc.Steps) > 0 {
		rec.Events = make([]StepEvent, len(doc.Steps))
		for i, sd := range doc.Steps {
			at := loaderr.Path("steps", i)
			if sd.Step == nil {
				return nil, loaderr.Errorf(at, "add step: the zero-based step the changes happen on", "steps entry %d has no step", i)
			}
			ev := StepEvent{Step: *sd.Step}
			var err error
			if ev.Press, err = lookupActions(actionIdx, loaderr.Join(at, "press"), ev.Step, "presses", sd.Press); err != nil {
				return nil, err
			}
			if ev.Release, err = lookupActions(actionIdx, loaderr.Join(at, "release"), ev.Step, "releases", sd.Release); err != nil {
				return nil, err
			}
			if len(sd.Axes) > 0 {
				ev.Axes = make([]AxisChange, len(sd.Axes))
				for j, av := range sd.Axes {
					idx, ok := axisIdx[av.name]
					if !ok {
						return nil, loaderr.Errorf(loaderr.Join(at, "axes", av.name), "add the axis to axes in the header, or correct its name", "step %d sets axis %q, which is not in axes", ev.Step, av.name)
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

// lookupActions returns the indices of names in idx; field is where names is in the document.
func lookupActions(idx map[string]int, field string, step int, verb string, names []string) ([]int, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]int, len(names))
	for i, name := range names {
		j, ok := idx[name]
		if !ok {
			return nil, loaderr.Errorf(loaderr.Join(field, i), "add the action to actions in the header, or correct its name", "step %d %s action %q, which is not in actions", step, verb, name)
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

// LoadFile reads and decodes the YAML Recording at path. Its errors are *loaderr.Error values that name path.
func LoadFile(path string) (*Recording, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(err, path, nil, "check that the recording exists at this path and can be read")
	}
	rec, err := Decode(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return rec, nil
}
