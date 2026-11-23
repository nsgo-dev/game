package particles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the current schema version of an emitter definition, written by MarshalConfigYAML and MarshalConfigJSON and required by UnmarshalConfigYAML and UnmarshalConfigJSON.
const FormatVersion = 1

const maxConfigSubDepth = 8

type docDuration time.Duration

// MarshalYAML writes the duration in time.Duration.String form, such as "1.25s".
func (d docDuration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// UnmarshalYAML accepts only a duration string such as "500ms" or "1.5s"; a bare number is refused because its unit would be ambiguous.
func (d *docDuration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" {
		return fmt.Errorf("line %d: duration %q must be a string such as \"500ms\" or \"1.5s\"", value.Line, value.Value)
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q: %w", value.Line, value.Value, err)
	}
	*d = docDuration(parsed)
	return nil
}

func (d docDuration) MarshalJSON() ([]byte, error) {
	td := time.Duration(d)
	if td == math.MinInt64 {
		return strconv.AppendInt(nil, int64(td), 10), nil
	}
	return json.Marshal(td.String())
}

func (d *docDuration) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*d = 0
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		if s == "" {
			*d = 0
			return nil
		}
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		*d = docDuration(parsed)
		return nil
	}
	var ns int64
	if err := json.Unmarshal(trimmed, &ns); err != nil {
		return fmt.Errorf("invalid duration %s: %w", string(trimmed), err)
	}
	*d = docDuration(ns)
	return nil
}

type docColor core.Color

// hex returns the color as "#rrggbbaa" in lowercase.
func (c docColor) hex() string {
	var buf [9]byte
	buf[0] = '#'
	v := uint32(c)
	const digits = "0123456789abcdef"
	for i := range 8 {
		shift := uint((7 - i) * 4)
		buf[1+i] = digits[(v>>shift)&0xf]
	}
	return string(buf[:])
}

// parseHexColor parses "#rrggbb" (opaque) or "#rrggbbaa".
func parseHexColor(s string) (docColor, error) {
	if !strings.HasPrefix(s, "#") {
		return 0, fmt.Errorf("invalid color %q: must start with #", s)
	}
	hexPart := s[1:]
	if len(hexPart) != 6 && len(hexPart) != 8 {
		return 0, fmt.Errorf("invalid color %q: expected #rrggbb or #rrggbbaa", s)
	}
	v, err := strconv.ParseUint(hexPart, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid color %q: %w", s, err)
	}
	if len(hexPart) == 6 {
		return docColor((uint32(v) << 8) | 0xff), nil
	}
	return docColor(uint32(v)), nil
}

func (c docColor) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.hex())
}

// MarshalYAML writes the color as a double-quoted "#rrggbbaa" string, since an unquoted # would start a YAML comment.
func (c docColor) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: c.hex()}, nil
}

// UnmarshalYAML accepts only a quoted "#rrggbb" or "#rrggbbaa" string.
func (c *docColor) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" {
		return fmt.Errorf("line %d: color %q must be a quoted string such as \"#ff8800\" or \"#ff8800cc\"", value.Line, value.Value)
	}
	parsed, err := parseHexColor(value.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", value.Line, err)
	}
	*c = parsed
	return nil
}

func (c *docColor) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = 0
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*c = 0
			return nil
		}
		parsed, err := parseHexColor(s)
		if err != nil {
			return err
		}
		*c = parsed
		return nil
	}
	var raw uint32
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return fmt.Errorf("invalid color %s: %w", string(trimmed), err)
	}
	*c = docColor(raw)
	return nil
}

type floatStopDoc struct {
	T     float32 `json:"t" yaml:"t"`
	Value float32 `json:"value" yaml:"value"`
}

type colorStopDoc struct {
	T     float32  `json:"t" yaml:"t"`
	Color docColor `json:"color" yaml:"color"`
}

type shapeDoc struct {
	Kind   string  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Radius float32 `json:"radius,omitempty" yaml:"radius,omitempty"`
	Width  float32 `json:"width,omitempty" yaml:"width,omitempty"`
	Height float32 `json:"height,omitempty" yaml:"height,omitempty"`
}

type collisionDoc struct {
	Enabled  bool    `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Mode     string  `json:"mode,omitempty" yaml:"mode,omitempty"`
	Bounce   float32 `json:"bounce,omitempty" yaml:"bounce,omitempty"`
	Friction float32 `json:"friction,omitempty" yaml:"friction,omitempty"`
	Radius   float32 `json:"radius,omitempty" yaml:"radius,omitempty"`
}

type trailDoc struct {
	MaxPoints     int            `json:"maxPoints,omitempty" yaml:"maxPoints,omitempty"`
	Lifetime      float32        `json:"lifetime,omitempty" yaml:"lifetime,omitempty"`
	MinDistance   float32        `json:"minDistance,omitempty" yaml:"minDistance,omitempty"`
	Width         float32        `json:"width,omitempty" yaml:"width,omitempty"`
	WidthCurve    []floatStopDoc `json:"widthCurve,omitempty" yaml:"widthCurve,omitempty"`
	ColorGradient []colorStopDoc `json:"colorGradient,omitempty" yaml:"colorGradient,omitempty"`
	Color         *docColor      `json:"color,omitempty" yaml:"color,omitempty"`
	TextureURI    string         `json:"textureURI,omitempty" yaml:"textureURI,omitempty"`
}

type subEmitterDoc struct {
	Trigger         string     `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	BurstCount      int        `json:"burstCount,omitempty" yaml:"burstCount,omitempty"`
	InheritVelocity float32    `json:"inheritVelocity,omitempty" yaml:"inheritVelocity,omitempty"`
	Config          *configDoc `json:"config,omitempty" yaml:"config,omitempty"`
}

type configDoc struct {
	Version            int             `json:"version,omitempty" yaml:"version,omitempty"`
	X                  float32         `json:"x,omitempty" yaml:"x,omitempty"`
	Y                  float32         `json:"y,omitempty" yaml:"y,omitempty"`
	Rotation           float32         `json:"rotation,omitempty" yaml:"rotation,omitempty"`
	Space              string          `json:"space,omitempty" yaml:"space,omitempty"`
	Shape              *shapeDoc       `json:"shape,omitempty" yaml:"shape,omitempty"`
	Rate               float32         `json:"rate,omitempty" yaml:"rate,omitempty"`
	MaxParticles       int             `json:"maxParticles,omitempty" yaml:"maxParticles,omitempty"`
	LifetimeMin        docDuration     `json:"lifetimeMin,omitempty" yaml:"lifetimeMin,omitempty"`
	LifetimeMax        docDuration     `json:"lifetimeMax,omitempty" yaml:"lifetimeMax,omitempty"`
	SpeedMin           float32         `json:"speedMin,omitempty" yaml:"speedMin,omitempty"`
	SpeedMax           float32         `json:"speedMax,omitempty" yaml:"speedMax,omitempty"`
	AngleMin           float32         `json:"angleMin,omitempty" yaml:"angleMin,omitempty"`
	AngleMax           float32         `json:"angleMax,omitempty" yaml:"angleMax,omitempty"`
	GravityX           float32         `json:"gravityX,omitempty" yaml:"gravityX,omitempty"`
	GravityY           float32         `json:"gravityY,omitempty" yaml:"gravityY,omitempty"`
	InitialRotationMin float32         `json:"initialRotationMin,omitempty" yaml:"initialRotationMin,omitempty"`
	InitialRotationMax float32         `json:"initialRotationMax,omitempty" yaml:"initialRotationMax,omitempty"`
	RotationSpeedMin   float32         `json:"rotationSpeedMin,omitempty" yaml:"rotationSpeedMin,omitempty"`
	RotationSpeedMax   float32         `json:"rotationSpeedMax,omitempty" yaml:"rotationSpeedMax,omitempty"`
	SizeCurve          []floatStopDoc  `json:"sizeCurve,omitempty" yaml:"sizeCurve,omitempty"`
	AlphaCurve         []floatStopDoc  `json:"alphaCurve,omitempty" yaml:"alphaCurve,omitempty"`
	RotationCurve      []floatStopDoc  `json:"rotationCurve,omitempty" yaml:"rotationCurve,omitempty"`
	ColorCurve         []colorStopDoc  `json:"colorCurve,omitempty" yaml:"colorCurve,omitempty"`
	Seed               uint64          `json:"seed,omitempty" yaml:"seed,omitempty"`
	Z                  int             `json:"z,omitempty" yaml:"z,omitempty"`
	FrameNames         []string        `json:"frameNames,omitempty" yaml:"frameNames,omitempty"`
	Collision          *collisionDoc   `json:"collision,omitempty" yaml:"collision,omitempty"`
	Trail              *trailDoc       `json:"trail,omitempty" yaml:"trail,omitempty"`
	SubEmitters        []subEmitterDoc `json:"subEmitters,omitempty" yaml:"subEmitters,omitempty"`
}

// MarshalConfigYAML encodes cfg as a versioned YAML emitter definition (version: 1) with fixed key order and 2-space indentation, so saving the same Config always yields the same bytes.
func MarshalConfigYAML(cfg Config) ([]byte, error) {
	doc, err := configToDoc(cfg, true, 0)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := root.Encode(doc); err != nil {
		return nil, fmt.Errorf("particles: encode config YAML: %w", err)
	}
	plainYAMLKeys(&root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("particles: encode config YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("particles: encode config YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// UnmarshalConfigYAML decodes a versioned YAML emitter definition into a Config. It refuses unknown fields, a key with no value, a second YAML document, a version other than FormatVersion, durations that are not strings such as "500ms", and colors that are not quoted "#rrggbb" or "#rrggbbaa" strings.
func UnmarshalConfigYAML(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("particles: empty config YAML")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, errors.New("particles: empty config YAML")
		}
		return Config{}, fmt.Errorf("particles: decode config YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("particles: unexpected trailing YAML document in config")
		}
		return Config{}, fmt.Errorf("particles: decode config YAML: %w", err)
	}
	if err := refuseYAMLNulls(&root); err != nil {
		return Config{}, err
	}

	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var doc configDoc
	if err := strict.Decode(&doc); err != nil {
		return Config{}, fmt.Errorf("particles: decode config YAML: %w", err)
	}
	if doc.Version != FormatVersion {
		return Config{}, fmt.Errorf("particles: unsupported config version %d (want %d)", doc.Version, FormatVersion)
	}
	return configFromDoc(&doc, 0)
}

// plainYAMLKeys writes every mapping key without quotes. The encoder quotes "y" because YAML 1.1 read it as a boolean, but the schema's keys are all plain identifiers and YAML 1.2 (which the decoder follows) reads "y" as a string, so a hand-written `y: 120` saves back unchanged.
func plainYAMLKeys(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			n.Content[i].Style = 0
		}
	}
	for _, child := range n.Content {
		plainYAMLKeys(child)
	}
}

// refuseYAMLNulls rejects a mapping value or list item with no value. The schema has no field where null means anything, and the usual cause is an unquoted color such as `color: #ff8800`, which YAML reads as a comment. Aliases are not followed, since their anchors are checked where they are defined.
func refuseYAMLNulls(n *yaml.Node) error {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if n.Kind == yaml.SequenceNode && isYAMLNull(child) {
				return fmt.Errorf("particles: line %d: list item has no value", child.Line)
			}
			if err := refuseYAMLNulls(child); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if isYAMLNull(value) {
				return fmt.Errorf("particles: line %d: %q has no value (quote colors such as \"#ff8800\": an unquoted # starts a YAML comment)", key.Line, key.Value)
			}
			if err := refuseYAMLNulls(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func isYAMLNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// MarshalConfigJSON encodes cfg into the versioned JSON form of the emitter definition (version: 1). Files people write use YAML (MarshalConfigYAML, SaveConfigFile); the JSON form is for programs that exchange definitions as JSON.
func MarshalConfigJSON(cfg Config) ([]byte, error) {
	doc, err := configToDoc(cfg, true, 0)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// UnmarshalConfigJSON decodes the versioned JSON form of an emitter definition into a Config.
func UnmarshalConfigJSON(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("particles: empty config JSON")
	}
	var doc configDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return Config{}, fmt.Errorf("particles: decode config JSON: %w", err)
	}
	if doc.Version != FormatVersion {
		return Config{}, fmt.Errorf("particles: unsupported config version %d (want %d)", doc.Version, FormatVersion)
	}
	return configFromDoc(&doc, 0)
}

// LoadConfigFile reads and decodes a versioned YAML emitter definition file from disk.
func LoadConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return UnmarshalConfigYAML(data)
}

// SaveConfigFile encodes cfg as a versioned YAML emitter definition and writes it to path.
func SaveConfigFile(path string, cfg Config) error {
	data, err := MarshalConfigYAML(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadConfigFS reads and decodes a versioned YAML emitter definition file from fsys.
func LoadConfigFS(fsys fs.FS, name string) (Config, error) {
	if fsys == nil {
		return Config{}, errors.New("particles: nil fs.FS")
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Config{}, err
	}
	return UnmarshalConfigYAML(data)
}

func configToDoc(cfg Config, includeVersion bool, depth int) (*configDoc, error) {
	if depth > maxConfigSubDepth {
		return nil, fmt.Errorf("particles: sub-emitter nesting exceeds max depth %d", maxConfigSubDepth)
	}
	if err := validateConfigFloats(cfg); err != nil {
		return nil, err
	}
	doc := &configDoc{
		X:                  cfg.X,
		Y:                  cfg.Y,
		Rotation:           cfg.Rotation,
		Space:              cfg.Space.String(),
		Shape:              shapeToDoc(cfg.Shape),
		Rate:               cfg.Rate,
		MaxParticles:       cfg.MaxParticles,
		LifetimeMin:        docDuration(cfg.LifetimeMin),
		LifetimeMax:        docDuration(cfg.LifetimeMax),
		SpeedMin:           cfg.SpeedMin,
		SpeedMax:           cfg.SpeedMax,
		AngleMin:           cfg.AngleMin,
		AngleMax:           cfg.AngleMax,
		GravityX:           cfg.GravityX,
		GravityY:           cfg.GravityY,
		InitialRotationMin: cfg.InitialRotationMin,
		InitialRotationMax: cfg.InitialRotationMax,
		RotationSpeedMin:   cfg.RotationSpeedMin,
		RotationSpeedMax:   cfg.RotationSpeedMax,
		SizeCurve:          floatCurveToDoc(cfg.SizeCurve),
		AlphaCurve:         floatCurveToDoc(cfg.AlphaCurve),
		RotationCurve:      floatCurveToDoc(cfg.RotationCurve),
		ColorCurve:         colorCurveToDoc(cfg.ColorCurve),
		Seed:               cfg.Seed,
		Z:                  cfg.Z,
		FrameNames:         append([]string(nil), cfg.FrameNames...),
	}
	if includeVersion {
		doc.Version = FormatVersion
	}

	col := cfg.Collision
	if col.Enabled || col.Mode != CollideNone || col.Bounce != 0 || col.Friction != 0 || col.Radius != 0 {
		doc.Collision = &collisionDoc{
			Enabled:  col.Enabled,
			Mode:     col.Mode.String(),
			Bounce:   col.Bounce,
			Friction: col.Friction,
			Radius:   col.Radius,
		}
	}

	if cfg.Trail != nil {
		jt := &trailDoc{
			MaxPoints:     cfg.Trail.MaxPoints,
			Lifetime:      cfg.Trail.Lifetime,
			MinDistance:   cfg.Trail.MinDistance,
			Width:         cfg.Trail.Width,
			WidthCurve:    floatCurveToDoc(cfg.Trail.WidthCurve),
			ColorGradient: colorCurveToDoc(cfg.Trail.ColorGradient),
			TextureURI:    cfg.Trail.Texture.URI,
		}
		if cfg.Trail.Color != 0 {
			jc := docColor(cfg.Trail.Color)
			jt.Color = &jc
		}
		doc.Trail = jt
	}

	if len(cfg.SubEmitters) > 0 {
		doc.SubEmitters = make([]subEmitterDoc, len(cfg.SubEmitters))
		for i, rule := range cfg.SubEmitters {
			subDoc, err := configToDoc(rule.Config, false, depth+1)
			if err != nil {
				return nil, err
			}
			doc.SubEmitters[i] = subEmitterDoc{
				Trigger:         rule.Trigger.String(),
				BurstCount:      rule.BurstCount,
				InheritVelocity: rule.InheritVelocity,
				Config:          subDoc,
			}
		}
	}
	return doc, nil
}

func configFromDoc(doc *configDoc, depth int) (Config, error) {
	if doc == nil {
		return Config{}, nil
	}
	if depth > maxConfigSubDepth {
		return Config{}, fmt.Errorf("particles: sub-emitter nesting exceeds max depth %d", maxConfigSubDepth)
	}
	space, err := parseSimulationSpace(doc.Space)
	if err != nil {
		return Config{}, err
	}
	shape, err := shapeFromDoc(doc.Shape)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		X:                  doc.X,
		Y:                  doc.Y,
		Rotation:           doc.Rotation,
		Space:              space,
		Shape:              shape,
		Rate:               doc.Rate,
		MaxParticles:       doc.MaxParticles,
		LifetimeMin:        time.Duration(doc.LifetimeMin),
		LifetimeMax:        time.Duration(doc.LifetimeMax),
		SpeedMin:           doc.SpeedMin,
		SpeedMax:           doc.SpeedMax,
		AngleMin:           doc.AngleMin,
		AngleMax:           doc.AngleMax,
		GravityX:           doc.GravityX,
		GravityY:           doc.GravityY,
		InitialRotationMin: doc.InitialRotationMin,
		InitialRotationMax: doc.InitialRotationMax,
		RotationSpeedMin:   doc.RotationSpeedMin,
		RotationSpeedMax:   doc.RotationSpeedMax,
		SizeCurve:          floatCurveFromDoc(doc.SizeCurve),
		AlphaCurve:         floatCurveFromDoc(doc.AlphaCurve),
		RotationCurve:      floatCurveFromDoc(doc.RotationCurve),
		ColorCurve:         colorCurveFromDoc(doc.ColorCurve),
		Seed:               doc.Seed,
		Z:                  doc.Z,
		FrameNames:         append([]string(nil), doc.FrameNames...),
	}

	if doc.Collision != nil {
		mode, err := parseCollisionMode(doc.Collision.Mode)
		if err != nil {
			return Config{}, err
		}
		cfg.Collision = CollisionConfig{
			Enabled:  doc.Collision.Enabled,
			Mode:     mode,
			Bounce:   doc.Collision.Bounce,
			Friction: doc.Collision.Friction,
			Radius:   doc.Collision.Radius,
		}
	}

	if doc.Trail != nil {
		tc := TrailConfig{
			MaxPoints:     doc.Trail.MaxPoints,
			Lifetime:      doc.Trail.Lifetime,
			MinDistance:   doc.Trail.MinDistance,
			Width:         doc.Trail.Width,
			WidthCurve:    floatCurveFromDoc(doc.Trail.WidthCurve),
			ColorGradient: colorCurveFromDoc(doc.Trail.ColorGradient),
		}
		if doc.Trail.Color != nil {
			tc.Color = core.Color(*doc.Trail.Color)
		}
		if doc.Trail.TextureURI != "" {
			tc.Texture = core.ImageSource{URI: doc.Trail.TextureURI}
		}
		cfg.Trail = &tc
	}

	if len(doc.SubEmitters) > 0 {
		cfg.SubEmitters = make([]SubEmitterRule, len(doc.SubEmitters))
		for i, sub := range doc.SubEmitters {
			trig, err := parseSubEmitterTrigger(sub.Trigger)
			if err != nil {
				return Config{}, err
			}
			var subCfg Config
			if sub.Config != nil {
				subCfg, err = configFromDoc(sub.Config, depth+1)
				if err != nil {
					return Config{}, err
				}
			}
			cfg.SubEmitters[i] = SubEmitterRule{
				Trigger:         trig,
				Config:          subCfg,
				BurstCount:      sub.BurstCount,
				InheritVelocity: sub.InheritVelocity,
			}
		}
	}

	if err := validateConfigFloats(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfigFloats(cfg Config) error {
	floats := []float32{
		cfg.X, cfg.Y, cfg.Rotation, cfg.Rate,
		cfg.SpeedMin, cfg.SpeedMax, cfg.AngleMin, cfg.AngleMax,
		cfg.GravityX, cfg.GravityY,
		cfg.InitialRotationMin, cfg.InitialRotationMax,
		cfg.RotationSpeedMin, cfg.RotationSpeedMax,
		cfg.Shape.radius, cfg.Shape.width, cfg.Shape.height,
		cfg.Collision.Bounce, cfg.Collision.Friction, cfg.Collision.Radius,
	}
	for _, v := range floats {
		if isBadFloat(v) {
			return errors.New("particles: config contains NaN or Inf float value")
		}
	}
	for _, curve := range []FloatCurve{cfg.SizeCurve, cfg.AlphaCurve, cfg.RotationCurve} {
		for _, s := range curve {
			if isBadFloat(s.T) || isBadFloat(s.Value) {
				return errors.New("particles: float curve contains NaN or Inf")
			}
		}
	}
	for _, s := range cfg.ColorCurve {
		if isBadFloat(s.T) {
			return errors.New("particles: color curve contains NaN or Inf")
		}
	}
	if cfg.Trail != nil {
		for _, v := range []float32{cfg.Trail.Lifetime, cfg.Trail.MinDistance, cfg.Trail.Width} {
			if isBadFloat(v) {
				return errors.New("particles: trail config contains NaN or Inf")
			}
		}
		for _, s := range cfg.Trail.WidthCurve {
			if isBadFloat(s.T) || isBadFloat(s.Value) {
				return errors.New("particles: trail width curve contains NaN or Inf")
			}
		}
		for _, s := range cfg.Trail.ColorGradient {
			if isBadFloat(s.T) {
				return errors.New("particles: trail color gradient contains NaN or Inf")
			}
		}
	}
	for _, sub := range cfg.SubEmitters {
		if isBadFloat(sub.InheritVelocity) {
			return errors.New("particles: sub-emitter inheritVelocity contains NaN or Inf")
		}
	}
	return nil
}

func shapeToDoc(s Shape) *shapeDoc {
	switch s.kind {
	case shapeCircle:
		return &shapeDoc{Kind: "circle", Radius: s.radius}
	case shapeRing:
		return &shapeDoc{Kind: "ring", Radius: s.radius}
	case shapeRect:
		return &shapeDoc{Kind: "rect", Width: s.width, Height: s.height}
	default:
		return &shapeDoc{Kind: "point"}
	}
}

func shapeFromDoc(js *shapeDoc) (Shape, error) {
	if js == nil || js.Kind == "" || js.Kind == "point" {
		return Point(), nil
	}
	switch js.Kind {
	case "circle":
		return Circle(js.Radius), nil
	case "ring":
		return Ring(js.Radius), nil
	case "rect":
		return Rect(js.Width, js.Height), nil
	default:
		return Shape{}, fmt.Errorf("particles: unknown shape kind %q", js.Kind)
	}
}

func parseSimulationSpace(s string) (SimulationSpace, error) {
	switch s {
	case "", "world":
		return SpaceWorld, nil
	case "local":
		return SpaceLocal, nil
	default:
		return SpaceWorld, fmt.Errorf("particles: unknown simulation space %q", s)
	}
}

func parseCollisionMode(s string) (CollisionMode, error) {
	switch s {
	case "", "none":
		return CollideNone, nil
	case "stop":
		return CollideStop, nil
	case "bounce":
		return CollideBounce, nil
	case "kill":
		return CollideKill, nil
	default:
		return CollideNone, fmt.Errorf("particles: unknown collision mode %q", s)
	}
}

func parseSubEmitterTrigger(s string) (SubEmitterTrigger, error) {
	switch s {
	case "", "birth":
		return TriggerOnBirth, nil
	case "death":
		return TriggerOnDeath, nil
	case "collision":
		return TriggerOnCollision, nil
	default:
		return TriggerOnBirth, fmt.Errorf("particles: unknown sub-emitter trigger %q", s)
	}
}

func floatCurveToDoc(c FloatCurve) []floatStopDoc {
	if len(c) == 0 {
		return nil
	}
	out := make([]floatStopDoc, len(c))
	for i, s := range c {
		out[i] = floatStopDoc{T: s.T, Value: s.Value}
	}
	return out
}

func floatCurveFromDoc(stops []floatStopDoc) FloatCurve {
	if len(stops) == 0 {
		return nil
	}
	out := make(FloatCurve, len(stops))
	for i, s := range stops {
		out[i] = FloatStop{T: s.T, Value: s.Value}
	}
	return out
}

func colorCurveToDoc(c ColorCurve) []colorStopDoc {
	if len(c) == 0 {
		return nil
	}
	out := make([]colorStopDoc, len(c))
	for i, s := range c {
		out[i] = colorStopDoc{T: s.T, Color: docColor(s.Color)}
	}
	return out
}

func colorCurveFromDoc(stops []colorStopDoc) ColorCurve {
	if len(stops) == 0 {
		return nil
	}
	out := make(ColorCurve, len(stops))
	for i, s := range stops {
		out[i] = ColorStop{T: s.T, Color: core.Color(s.Color)}
	}
	return out
}

// ReloadConfig applies cfg to a running Emitter without resetting live particles, PRNG state, attachment callbacks, or runtime colliders.
func (e *Emitter) ReloadConfig(cfg Config) {
	cloned := cfg.clone()

	if e.attachFn == nil && e.attachTransformFn == nil {
		e.X = cloned.X
		e.Y = cloned.Y
	}
	if e.attachTransformFn == nil {
		e.Rotation = cloned.Rotation
	}
	e.Space = cloned.Space
	e.Shape = cloned.Shape
	e.Rate = cloned.Rate
	e.MaxParticles = cloned.MaxParticles
	e.LifetimeMin = cloned.LifetimeMin
	e.LifetimeMax = cloned.LifetimeMax
	e.SpeedMin = cloned.SpeedMin
	e.SpeedMax = cloned.SpeedMax
	e.AngleMin = cloned.AngleMin
	e.AngleMax = cloned.AngleMax
	e.GravityX = cloned.GravityX
	e.GravityY = cloned.GravityY
	e.InitialRotationMin = cloned.InitialRotationMin
	e.InitialRotationMax = cloned.InitialRotationMax
	e.RotationSpeedMin = cloned.RotationSpeedMin
	e.RotationSpeedMax = cloned.RotationSpeedMax
	e.SizeCurve = cloned.SizeCurve
	e.AlphaCurve = cloned.AlphaCurve
	e.RotationCurve = cloned.RotationCurve
	e.ColorCurve = cloned.ColorCurve
	e.Seed = cloned.Seed
	e.Z = cloned.Z

	if e.Atlas != nil && len(cloned.FrameNames) > 0 {
		if resolved := resolveAtlasFrames(e.Atlas, cloned.FrameNames); len(resolved) > 0 {
			e.Frames = resolved
		}
	}

	// Preserve the runtime Collider, which a loaded definition cannot carry.
	prevCollider := e.Collision.Collider
	e.Collision = cloned.Collision
	if e.Collision.Collider == nil {
		e.Collision.Collider = prevCollider
	}

	if cloned.Trail != nil {
		e.AttachTrail(*cloned.Trail)
	} else if e.Trail != nil {
		e.DetachTrail()
	}

	e.SubEmitters = cloned.SubEmitters
	if len(e.children) > len(e.SubEmitters) {
		clear(e.children[len(e.SubEmitters):])
		e.children = e.children[:len(e.SubEmitters)]
	}
	for i := range e.children {
		if e.children[i] != nil && i < len(e.SubEmitters) {
			e.children[i].ReloadConfig(e.SubEmitters[i].Config)
			if e.Atlas != nil && len(e.SubEmitters[i].Config.FrameNames) > 0 {
				e.bindChildAtlas(e.children[i], e.SubEmitters[i].Config.FrameNames)
			}
		}
	}

	// Re-evaluate curves on live particles so visual updates appear immediately.
	for i := range e.particles {
		e.evalCurves(&e.particles[i])
	}
}

// HotConfig holds a live, versioned emitter Config and pushes hot-reloaded definitions to bound Emitters.
// It is composable with file-watching hot-reload hooks via ReloadHook.
type HotConfig struct {
	mu       sync.Mutex
	path     string
	cfg      Config
	version  uint64
	emitters []*Emitter
	onReload func(Config)
	pending  *Config
}

// NewHotConfig creates a HotConfig initialized with initial.
func NewHotConfig(initial Config) *HotConfig {
	return &HotConfig{
		cfg:     initial.clone(),
		version: 1,
	}
}

// LoadHotConfigFile loads a versioned YAML emitter definition from path and returns a HotConfig bound to that file path.
func LoadHotConfigFile(path string) (*HotConfig, error) {
	cfg, err := LoadConfigFile(path)
	if err != nil {
		return nil, err
	}
	return &HotConfig{
		path:    path,
		cfg:     cfg,
		version: 1,
	}, nil
}

// Bind registers e to receive config updates whenever Apply, ReloadBytes, ReloadFile, or Flush runs,
// and immediately applies the current config to e.
func (h *HotConfig) Bind(e *Emitter) {
	if e == nil {
		return
	}
	h.mu.Lock()
	for _, existing := range h.emitters {
		if existing == e {
			h.mu.Unlock()
			return
		}
	}
	h.emitters = append(h.emitters, e)
	cfg := h.cfg.clone()
	h.mu.Unlock()
	e.ReloadConfig(cfg)
}

// Unbind removes e from receiving further hot-reload updates.
func (h *HotConfig) Unbind(e *Emitter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, existing := range h.emitters {
		if existing == e {
			copy(h.emitters[i:], h.emitters[i+1:])
			h.emitters[len(h.emitters)-1] = nil
			h.emitters = h.emitters[:len(h.emitters)-1]
			return
		}
	}
}

// OnReload sets an optional callback invoked after a config reload succeeds.
func (h *HotConfig) OnReload(fn func(Config)) {
	h.mu.Lock()
	h.onReload = fn
	h.mu.Unlock()
}

// Config returns a copy of the currently active Config.
func (h *HotConfig) Config() Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.clone()
}

// Version returns the monotonic reload counter (starts at 1 and increments on each successful reload).
func (h *HotConfig) Version() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.version
}

// Apply updates the stored Config and reloads all bound Emitters immediately.
func (h *HotConfig) Apply(cfg Config) {
	cloned := cfg.clone()
	h.mu.Lock()
	h.cfg = cloned
	h.version++
	emitters := append([]*Emitter(nil), h.emitters...)
	cb := h.onReload
	h.mu.Unlock()

	for _, e := range emitters {
		e.ReloadConfig(cloned)
	}
	if cb != nil {
		cb(cloned.clone())
	}
}

// ReloadBytes decodes a versioned YAML emitter definition and, if valid, applies the new Config to all bound Emitters.
// An invalid definition returns an error and leaves the current Config and Emitters unchanged.
func (h *HotConfig) ReloadBytes(data []byte) error {
	cfg, err := UnmarshalConfigYAML(data)
	if err != nil {
		return err
	}
	h.Apply(cfg)
	return nil
}

// ReloadFile reads a versioned YAML emitter definition from path (or the stored file path if path is empty) and applies it.
func (h *HotConfig) ReloadFile(path string) error {
	h.mu.Lock()
	if path == "" {
		path = h.path
	} else {
		h.path = path
	}
	h.mu.Unlock()
	if path == "" {
		return errors.New("particles: no config file path configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return h.ReloadBytes(data)
}

// ReloadHook returns a file-change callback suitable for plugging into hot-reload watchers (such as tooling/hotreload).
func (h *HotConfig) ReloadHook() func(path string) error {
	return func(path string) error {
		return h.ReloadFile(path)
	}
}

// StageBytes decodes a versioned YAML emitter definition from a background watcher thread without touching Emitters yet.
// Call Flush from the game loop thread to apply the staged config safely.
func (h *HotConfig) StageBytes(data []byte) error {
	cfg, err := UnmarshalConfigYAML(data)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.pending = &cfg
	h.mu.Unlock()
	return nil
}

// Flush applies any config staged via StageBytes to all bound Emitters and reports whether a reload occurred.
func (h *HotConfig) Flush() bool {
	h.mu.Lock()
	p := h.pending
	h.pending = nil
	h.mu.Unlock()
	if p == nil {
		return false
	}
	h.Apply(*p)
	return true
}
