package particles

import (
	"bytes"
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
	"math"
	"strings"
)

type Effect struct {
	Version  int                    `yaml:"version"`
	Emitters []EffectEmitter        `yaml:"emitters"`
	Params   map[string]EffectParam `yaml:"params,omitempty"`
}
type EffectEmitter struct {
	File       string     `yaml:"file,omitempty"`
	Config     *Config    `yaml:"config,omitempty"`
	Offset     [2]float32 `yaml:"offset,omitempty"`
	Delay      float32    `yaml:"delay,omitempty"`
	Duration   float32    `yaml:"duration,omitempty"`
	MinQuality string     `yaml:"minQuality,omitempty"`
}
type EffectParam struct {
	Type  string     `yaml:"type"`
	Float float64    `yaml:"float,omitempty"`
	Int   int        `yaml:"int,omitempty"`
	Color string     `yaml:"color,omitempty"`
	Vec2  [2]float32 `yaml:"vec2,omitempty"`
}

func ParseEffect(data []byte) (Effect, error) {
	var e Effect
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&e); err != nil {
		return e, err
	}
	if e.Version != 1 || len(e.Emitters) == 0 {
		return e, errors.New("particles: effect requires version 1 and emitters")
	}
	for n, p := range e.Params {
		switch p.Type {
		case "float":
			if math.IsNaN(p.Float) || math.IsInf(p.Float, 0) {
				return e, fmt.Errorf("particles: param %q is not finite", n)
			}
		case "int", "color", "vec2":
		default:
			return e, fmt.Errorf("particles: param %q has unknown type %q", n, p.Type)
		}
	}
	return e, nil
}
func EncodeEffect(e Effect) ([]byte, error) {
	if e.Version == 0 {
		e.Version = 1
	}
	if e.Version != 1 || len(e.Emitters) == 0 {
		return nil, errors.New("particles: invalid effect")
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	enc.Close()
	return b.Bytes(), nil
}
func (e Effect) SetParam(name string, value any) error {
	p, ok := e.Params[name]
	if !ok {
		return fmt.Errorf("particles: unknown param %q", name)
	}
	switch p.Type {
	case "float":
		v, ok := value.(float64)
		if !ok {
			return errors.New("particles: float param needs float64")
		}
		p.Float = v
	case "int":
		v, ok := value.(int)
		if !ok {
			return errors.New("particles: int param needs int")
		}
		p.Int = v
	case "color":
		v, ok := value.(string)
		if !ok || !strings.HasPrefix(v, "#") {
			return errors.New("particles: invalid color param")
		}
		p.Color = v
	default:
		return errors.New("particles: unsupported param")
	}
	e.Params[name] = p
	return nil
}
