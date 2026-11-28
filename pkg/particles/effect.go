package particles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"sort"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/quality"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// EffectFormatVersion is the current schema version of an effect file, written by MarshalEffectYAML and required by UnmarshalEffectYAML.
const EffectFormatVersion = 1

const fixEffect = "correct the effect file as docs/en/particles.md describes it"

// EffectParamType names the type of an effect parameter.
type EffectParamType string

// The effect parameter types.
const (
	EffectParamFloat EffectParamType = "float"
	EffectParamInt   EffectParamType = "int"
	EffectParamColor EffectParamType = "color"
	EffectParamVec2  EffectParamType = "vec2"
)

// Effect is an effect file: several emitters that play as one effect, and the typed parameters the game sets on it.
type Effect struct {
	Emitters []EffectEmitter
	// Params maps a parameter's name to its type and current value; the file holds the defaults.
	Params map[string]EffectParam
}

// EffectEmitter is one emitter of an Effect: exactly one of File and Config is set.
type EffectEmitter struct {
	// File is the slash-separated path of a particle emitter definition, relative to the asset root.
	File string
	// Config is an inline emitter definition, in the schema of an emitter file without its version.
	Config *Config
	// OffsetX and OffsetY move the emitter from the effect's origin.
	OffsetX, OffsetY float32
	// Delay is how long after the effect starts the emitter starts.
	Delay time.Duration
	// Duration is how long the emitter emits once started; zero means until the effect stops.
	Duration time.Duration
	// MinQuality is the lowest quality level the emitter plays at ("low", "medium" or "high"); empty means every level.
	MinQuality string
}

// EffectParam is a typed effect parameter. Only the value field of its Type is set.
type EffectParam struct {
	Type  EffectParamType
	Float float64
	Int   int
	Color core.Color
	Vec2  [2]float32
}

type effectDoc struct {
	Version  docInt                    `yaml:"version"`
	Emitters []effectEmitterDoc        `yaml:"emitters"`
	Params   map[string]effectParamDoc `yaml:"params,omitempty"`
}

type effectEmitterDoc struct {
	File       string      `yaml:"file,omitempty"`
	Config     *configDoc  `yaml:"config,omitempty"`
	OffsetX    float32     `yaml:"offsetX,omitempty"`
	OffsetY    float32     `yaml:"offsetY,omitempty"`
	Delay      docDuration `yaml:"delay,omitempty"`
	Duration   docDuration `yaml:"duration,omitempty"`
	MinQuality string      `yaml:"minQuality,omitempty"`
}

type effectParamDoc struct {
	Type  string     `yaml:"type"`
	Float *float64   `yaml:"float,omitempty"`
	Int   *docInt    `yaml:"int,omitempty"`
	Color *docColor  `yaml:"color,omitempty"`
	Vec2  *[]float32 `yaml:"vec2,omitempty,flow"`
}

// MarshalEffectYAML encodes e as a versioned YAML effect file (version: 1) with fixed key order (parameters sorted by name) and 2-space indentation, so saving the same Effect always yields the same bytes. It refuses an Effect that UnmarshalEffectYAML would refuse.
func MarshalEffectYAML(e Effect) ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	doc := effectDoc{Version: EffectFormatVersion, Emitters: make([]effectEmitterDoc, len(e.Emitters))}
	for i, em := range e.Emitters {
		d := effectEmitterDoc{File: em.File, OffsetX: em.OffsetX, OffsetY: em.OffsetY, Delay: docDuration(em.Delay), Duration: docDuration(em.Duration), MinQuality: em.MinQuality}
		if em.Config != nil {
			cfg, err := configToDoc(*em.Config, false, 0)
			if err != nil {
				return nil, fmt.Errorf("particles: effect emitter %d: %w", i, err)
			}
			d.Config = cfg
		}
		doc.Emitters[i] = d
	}
	if len(e.Params) > 0 {
		doc.Params = make(map[string]effectParamDoc, len(e.Params))
		for name, p := range e.Params {
			d := effectParamDoc{Type: string(p.Type)}
			switch p.Type {
			case EffectParamFloat:
				d.Float = &p.Float
			case EffectParamInt:
				v := docInt(p.Int)
				d.Int = &v
			case EffectParamColor:
				v := docColor(p.Color)
				d.Color = &v
			case EffectParamVec2:
				v := []float32{p.Vec2[0], p.Vec2[1]}
				d.Vec2 = &v
			}
			doc.Params[name] = d
		}
	}
	var root yaml.Node
	if err := root.Encode(yamlzero.Clean(doc)); err != nil {
		return nil, fmt.Errorf("particles: encode effect YAML: %w", err)
	}
	plainYAMLKeys(&root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("particles: encode effect YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("particles: encode effect YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// UnmarshalEffectYAML decodes a versioned YAML effect file. Besides what UnmarshalConfigYAML refuses in an inline config (and a version inside one), it refuses a version other than EffectFormatVersion, an effect with no emitters, an emitter with both or neither of file and config, a file path that is not a valid slash-separated relative path, a negative delay or duration, an unknown minQuality, a parameter of an unknown type, and a parameter whose value is missing, not finite, or written under another type's key.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it.
func UnmarshalEffectYAML(data []byte) (Effect, error) {
	e, err := unmarshalEffectYAML(data)
	if err != nil {
		return Effect{}, loaderr.Locate(err, "", data, fixEffect)
	}
	return e, nil
}

func unmarshalEffectYAML(data []byte) (Effect, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Effect{}, errors.New("particles: empty effect YAML")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return Effect{}, errors.New("particles: empty effect YAML")
		}
		return Effect{}, fmt.Errorf("particles: decode effect YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Effect{}, errors.New("particles: unexpected trailing YAML document in effect")
		}
		return Effect{}, fmt.Errorf("particles: decode effect YAML: %w", err)
	}
	if err := refuseYAMLNulls(&root); err != nil {
		return Effect{}, err
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var doc effectDoc
	if err := strict.Decode(&doc); err != nil {
		return Effect{}, fmt.Errorf("particles: decode effect YAML: %w", err)
	}
	if doc.Version != EffectFormatVersion {
		return Effect{}, &loaderr.Error{Field: "version", Problem: fmt.Sprintf("particles: unsupported effect version %d (want %d)", doc.Version, EffectFormatVersion), Fix: fmt.Sprintf("set version: %d", EffectFormatVersion)}
	}
	e := Effect{Emitters: make([]EffectEmitter, len(doc.Emitters))}
	for i, d := range doc.Emitters {
		at := loaderr.Path("emitters", i)
		em := EffectEmitter{File: d.File, OffsetX: d.OffsetX, OffsetY: d.OffsetY, Delay: time.Duration(d.Delay), Duration: time.Duration(d.Duration), MinQuality: d.MinQuality}
		if d.Config != nil {
			if d.Config.Version != 0 {
				return Effect{}, loaderr.Errorf(loaderr.Join(at, "config", "version"), "remove version from the inline config; the effect's version covers it", "particles: version is not allowed in an effect's inline config")
			}
			cfg, err := configFromDoc(d.Config, 0, loaderr.Join(at, "config"))
			if err != nil {
				return Effect{}, err
			}
			em.Config = &cfg
		}
		e.Emitters[i] = em
	}
	if len(doc.Params) > 0 {
		e.Params = make(map[string]EffectParam, len(doc.Params))
		for name, d := range doc.Params {
			p, err := paramFromDoc(d)
			if err != nil {
				return Effect{}, loaderr.Errorf(loaderr.Join("params", name), "give the parameter one of the types float, int, color or vec2 and its value under the key of that type", "particles: param %q: %v", name, err)
			}
			e.Params[name] = p
		}
	}
	yamlzero.Normalize(&e)
	if err := e.Validate(); err != nil {
		return Effect{}, err
	}
	return e, nil
}

func paramFromDoc(d effectParamDoc) (EffectParam, error) {
	p := EffectParam{Type: EffectParamType(d.Type)}
	set := 0
	if d.Float != nil {
		set++
	}
	if d.Int != nil {
		set++
	}
	if d.Color != nil {
		set++
	}
	if d.Vec2 != nil {
		set++
	}
	var ok bool
	switch p.Type {
	case EffectParamFloat:
		if ok = d.Float != nil; ok {
			p.Float = *d.Float
		}
	case EffectParamInt:
		if ok = d.Int != nil; ok {
			p.Int = int(*d.Int)
		}
	case EffectParamColor:
		if ok = d.Color != nil; ok {
			p.Color = core.Color(*d.Color)
		}
	case EffectParamVec2:
		if ok = d.Vec2 != nil && len(*d.Vec2) == 2; ok {
			p.Vec2 = [2]float32{(*d.Vec2)[0], (*d.Vec2)[1]}
		}
	default:
		return EffectParam{}, fmt.Errorf("unknown type %q", d.Type)
	}
	if !ok || set != 1 {
		return EffectParam{}, fmt.Errorf("type %s needs exactly one value, under %q (a vec2 is a list of two numbers)", d.Type, d.Type)
	}
	return p, nil
}

// Validate reports the first problem that would make e an invalid effect file.
func (e Effect) Validate() error {
	if len(e.Emitters) == 0 {
		return loaderr.Errorf("emitters", "list at least one emitter", "particles: effect has no emitters")
	}
	for i, em := range e.Emitters {
		at := loaderr.Path("emitters", i)
		switch {
		case (em.File == "") == (em.Config == nil):
			return loaderr.Errorf(at, "give the emitter either file or config", "particles: effect emitter %d needs exactly one of file and config", i)
		case em.File != "" && !fs.ValidPath(em.File):
			return loaderr.Errorf(loaderr.Join(at, "file"), "write a slash-separated path relative to the asset root, without . or .. elements", "particles: effect emitter %d: invalid file path %q", i, em.File)
		case !finite32(em.OffsetX) || !finite32(em.OffsetY):
			return loaderr.Errorf(at, "use finite offsets", "particles: effect emitter %d: offset is not finite", i)
		case em.Delay < 0 || em.Duration < 0:
			return loaderr.Errorf(at, "use a delay and duration of zero or more", "particles: effect emitter %d: negative delay or duration", i)
		}
		if em.MinQuality != "" {
			if l, err := quality.ParseLevel(em.MinQuality); err != nil || l.String() != em.MinQuality {
				return loaderr.Errorf(loaderr.Join(at, "minQuality"), "set minQuality to low, medium or high", "particles: effect emitter %d: unknown minQuality %q", i, em.MinQuality)
			}
		}
	}
	names := make([]string, 0, len(e.Params))
	for name := range e.Params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := e.Params[name].validate(); err != nil {
			return loaderr.Errorf(loaderr.Join("params", name), "give the parameter a finite value of its type", "particles: param %q: %v", name, err)
		}
	}
	return nil
}

func (p EffectParam) validate() error {
	switch p.Type {
	case EffectParamFloat:
		if math.IsNaN(p.Float) || math.IsInf(p.Float, 0) {
			return errors.New("value is not finite")
		}
	case EffectParamVec2:
		if !finite32(p.Vec2[0]) || !finite32(p.Vec2[1]) {
			return errors.New("value is not finite")
		}
	case EffectParamInt, EffectParamColor:
	default:
		return fmt.Errorf("unknown type %q", p.Type)
	}
	return nil
}

func finite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// SetParam sets the declared parameter name to value, which must be of the parameter's type: a float64 or float32 for float, an int for int, a core.Color for color, and a [2]float32 for vec2. It refuses an undeclared name, a value of another type, and a value that is not finite, leaving the parameter unchanged.
func (e *Effect) SetParam(name string, value any) error {
	p, ok := e.Params[name]
	if !ok {
		return fmt.Errorf("particles: unknown param %q", name)
	}
	switch p.Type {
	case EffectParamFloat:
		switch v := value.(type) {
		case float64:
			p.Float = v
		case float32:
			p.Float = float64(v)
		default:
			return fmt.Errorf("particles: float param %q needs a float64, got %T", name, value)
		}
	case EffectParamInt:
		v, ok := value.(int)
		if !ok {
			return fmt.Errorf("particles: int param %q needs an int, got %T", name, value)
		}
		p.Int = v
	case EffectParamColor:
		v, ok := value.(core.Color)
		if !ok {
			return fmt.Errorf("particles: color param %q needs a core.Color, got %T", name, value)
		}
		p.Color = v
	case EffectParamVec2:
		v, ok := value.([2]float32)
		if !ok {
			return fmt.Errorf("particles: vec2 param %q needs a [2]float32, got %T", name, value)
		}
		p.Vec2 = v
	default:
		return fmt.Errorf("particles: param %q has unknown type %q", name, p.Type)
	}
	if err := p.validate(); err != nil {
		return fmt.Errorf("particles: param %q: %w", name, err)
	}
	e.Params[name] = p
	return nil
}
