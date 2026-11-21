package particles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// FormatVersion is the current schema version written by MarshalConfigJSON and accepted by UnmarshalConfigJSON.
const FormatVersion = 1

const maxConfigSubDepth = 8

type jsonDuration time.Duration

func (d jsonDuration) MarshalJSON() ([]byte, error) {
	td := time.Duration(d)
	if td == math.MinInt64 {
		return strconv.AppendInt(nil, int64(td), 10), nil
	}
	return json.Marshal(td.String())
}

func (d *jsonDuration) UnmarshalJSON(data []byte) error {
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
		*d = jsonDuration(parsed)
		return nil
	}
	var ns int64
	if err := json.Unmarshal(trimmed, &ns); err != nil {
		return fmt.Errorf("invalid duration %s: %w", string(trimmed), err)
	}
	*d = jsonDuration(ns)
	return nil
}

type jsonColor core.Color

func (c jsonColor) MarshalJSON() ([]byte, error) {
	var buf [11]byte
	buf[0] = '"'
	buf[1] = '#'
	v := uint32(c)
	const hex = "0123456789abcdef"
	for i := range 8 {
		shift := uint((7 - i) * 4)
		buf[2+i] = hex[(v>>shift)&0xf]
	}
	buf[10] = '"'
	return buf[:], nil
}

func (c *jsonColor) UnmarshalJSON(data []byte) error {
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
		if !strings.HasPrefix(s, "#") {
			return fmt.Errorf("invalid color %q: must start with #", s)
		}
		hexPart := s[1:]
		switch len(hexPart) {
		case 6:
			v, err := strconv.ParseUint(hexPart, 16, 32)
			if err != nil {
				return fmt.Errorf("invalid color %q: %w", s, err)
			}
			*c = jsonColor((uint32(v) << 8) | 0xff)
			return nil
		case 8:
			v, err := strconv.ParseUint(hexPart, 16, 32)
			if err != nil {
				return fmt.Errorf("invalid color %q: %w", s, err)
			}
			*c = jsonColor(uint32(v))
			return nil
		default:
			return fmt.Errorf("invalid color %q: expected #rrggbb or #rrggbbaa", s)
		}
	}
	var raw uint32
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return fmt.Errorf("invalid color %s: %w", string(trimmed), err)
	}
	*c = jsonColor(raw)
	return nil
}

type jsonFloatStop struct {
	T     float32 `json:"t"`
	Value float32 `json:"value"`
}

type jsonColorStop struct {
	T     float32   `json:"t"`
	Color jsonColor `json:"color"`
}

type jsonShape struct {
	Kind   string  `json:"kind,omitempty"`
	Radius float32 `json:"radius,omitempty"`
	Width  float32 `json:"width,omitempty"`
	Height float32 `json:"height,omitempty"`
}

type jsonCollision struct {
	Enabled  bool    `json:"enabled,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	Bounce   float32 `json:"bounce,omitempty"`
	Friction float32 `json:"friction,omitempty"`
	Radius   float32 `json:"radius,omitempty"`
}

type jsonTrail struct {
	MaxPoints     int             `json:"maxPoints,omitempty"`
	Lifetime      float32         `json:"lifetime,omitempty"`
	MinDistance   float32         `json:"minDistance,omitempty"`
	Width         float32         `json:"width,omitempty"`
	WidthCurve    []jsonFloatStop `json:"widthCurve,omitempty"`
	ColorGradient []jsonColorStop `json:"colorGradient,omitempty"`
	Color         *jsonColor      `json:"color,omitempty"`
	TextureURI    string          `json:"textureURI,omitempty"`
}

type jsonSubEmitter struct {
	Trigger         string      `json:"trigger,omitempty"`
	BurstCount      int         `json:"burstCount,omitempty"`
	InheritVelocity float32     `json:"inheritVelocity,omitempty"`
	Config          *jsonConfig `json:"config,omitempty"`
}

type jsonConfig struct {
	Version            int              `json:"version,omitempty"`
	X                  float32          `json:"x,omitempty"`
	Y                  float32          `json:"y,omitempty"`
	Rotation           float32          `json:"rotation,omitempty"`
	Space              string           `json:"space,omitempty"`
	Shape              *jsonShape       `json:"shape,omitempty"`
	Rate               float32          `json:"rate,omitempty"`
	MaxParticles       int              `json:"maxParticles,omitempty"`
	LifetimeMin        jsonDuration     `json:"lifetimeMin,omitempty"`
	LifetimeMax        jsonDuration     `json:"lifetimeMax,omitempty"`
	SpeedMin           float32          `json:"speedMin,omitempty"`
	SpeedMax           float32          `json:"speedMax,omitempty"`
	AngleMin           float32          `json:"angleMin,omitempty"`
	AngleMax           float32          `json:"angleMax,omitempty"`
	GravityX           float32          `json:"gravityX,omitempty"`
	GravityY           float32          `json:"gravityY,omitempty"`
	InitialRotationMin float32          `json:"initialRotationMin,omitempty"`
	InitialRotationMax float32          `json:"initialRotationMax,omitempty"`
	RotationSpeedMin   float32          `json:"rotationSpeedMin,omitempty"`
	RotationSpeedMax   float32          `json:"rotationSpeedMax,omitempty"`
	SizeCurve          []jsonFloatStop  `json:"sizeCurve,omitempty"`
	AlphaCurve         []jsonFloatStop  `json:"alphaCurve,omitempty"`
	RotationCurve      []jsonFloatStop  `json:"rotationCurve,omitempty"`
	ColorCurve         []jsonColorStop  `json:"colorCurve,omitempty"`
	Seed               uint64           `json:"seed,omitempty"`
	Z                  int              `json:"z,omitempty"`
	FrameNames         []string         `json:"frameNames,omitempty"`
	Collision          *jsonCollision   `json:"collision,omitempty"`
	Trail              *jsonTrail       `json:"trail,omitempty"`
	SubEmitters        []jsonSubEmitter `json:"subEmitters,omitempty"`
}

// MarshalConfigJSON encodes cfg into the versioned JSON emitter definition format (version: 1).
func MarshalConfigJSON(cfg Config) ([]byte, error) {
	doc, err := configToJSON(cfg, true, 0)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// UnmarshalConfigJSON decodes a versioned JSON emitter definition into a Config.
func UnmarshalConfigJSON(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("particles: empty config JSON")
	}
	var doc jsonConfig
	if err := json.Unmarshal(data, &doc); err != nil {
		return Config{}, fmt.Errorf("particles: decode config JSON: %w", err)
	}
	if doc.Version != FormatVersion {
		return Config{}, fmt.Errorf("particles: unsupported config version %d (want %d)", doc.Version, FormatVersion)
	}
	return configFromJSON(&doc, 0)
}

// LoadConfigFile reads and decodes a versioned emitter definition JSON file from disk.
func LoadConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return UnmarshalConfigJSON(data)
}

// SaveConfigFile encodes cfg to versioned JSON and writes it to path.
func SaveConfigFile(path string, cfg Config) error {
	data, err := MarshalConfigJSON(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadConfigFS reads and decodes a versioned emitter definition JSON file from fsys.
func LoadConfigFS(fsys fs.FS, name string) (Config, error) {
	if fsys == nil {
		return Config{}, errors.New("particles: nil fs.FS")
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Config{}, err
	}
	return UnmarshalConfigJSON(data)
}

func configToJSON(cfg Config, includeVersion bool, depth int) (*jsonConfig, error) {
	if depth > maxConfigSubDepth {
		return nil, fmt.Errorf("particles: sub-emitter nesting exceeds max depth %d", maxConfigSubDepth)
	}
	if err := validateConfigFloats(cfg); err != nil {
		return nil, err
	}
	doc := &jsonConfig{
		X:                  cfg.X,
		Y:                  cfg.Y,
		Rotation:           cfg.Rotation,
		Space:              cfg.Space.String(),
		Shape:              shapeToJSON(cfg.Shape),
		Rate:               cfg.Rate,
		MaxParticles:       cfg.MaxParticles,
		LifetimeMin:        jsonDuration(cfg.LifetimeMin),
		LifetimeMax:        jsonDuration(cfg.LifetimeMax),
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
		SizeCurve:          floatCurveToJSON(cfg.SizeCurve),
		AlphaCurve:         floatCurveToJSON(cfg.AlphaCurve),
		RotationCurve:      floatCurveToJSON(cfg.RotationCurve),
		ColorCurve:         colorCurveToJSON(cfg.ColorCurve),
		Seed:               cfg.Seed,
		Z:                  cfg.Z,
		FrameNames:         append([]string(nil), cfg.FrameNames...),
	}
	if includeVersion {
		doc.Version = FormatVersion
	}

	col := cfg.Collision
	if col.Enabled || col.Mode != CollideNone || col.Bounce != 0 || col.Friction != 0 || col.Radius != 0 {
		doc.Collision = &jsonCollision{
			Enabled:  col.Enabled,
			Mode:     col.Mode.String(),
			Bounce:   col.Bounce,
			Friction: col.Friction,
			Radius:   col.Radius,
		}
	}

	if cfg.Trail != nil {
		jt := &jsonTrail{
			MaxPoints:     cfg.Trail.MaxPoints,
			Lifetime:      cfg.Trail.Lifetime,
			MinDistance:   cfg.Trail.MinDistance,
			Width:         cfg.Trail.Width,
			WidthCurve:    floatCurveToJSON(cfg.Trail.WidthCurve),
			ColorGradient: colorCurveToJSON(cfg.Trail.ColorGradient),
			TextureURI:    cfg.Trail.Texture.URI,
		}
		if cfg.Trail.Color != 0 {
			jc := jsonColor(cfg.Trail.Color)
			jt.Color = &jc
		}
		doc.Trail = jt
	}

	if len(cfg.SubEmitters) > 0 {
		doc.SubEmitters = make([]jsonSubEmitter, len(cfg.SubEmitters))
		for i, rule := range cfg.SubEmitters {
			subDoc, err := configToJSON(rule.Config, false, depth+1)
			if err != nil {
				return nil, err
			}
			doc.SubEmitters[i] = jsonSubEmitter{
				Trigger:         rule.Trigger.String(),
				BurstCount:      rule.BurstCount,
				InheritVelocity: rule.InheritVelocity,
				Config:          subDoc,
			}
		}
	}
	return doc, nil
}

func configFromJSON(doc *jsonConfig, depth int) (Config, error) {
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
	shape, err := shapeFromJSON(doc.Shape)
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
		SizeCurve:          floatCurveFromJSON(doc.SizeCurve),
		AlphaCurve:         floatCurveFromJSON(doc.AlphaCurve),
		RotationCurve:      floatCurveFromJSON(doc.RotationCurve),
		ColorCurve:         colorCurveFromJSON(doc.ColorCurve),
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
			WidthCurve:    floatCurveFromJSON(doc.Trail.WidthCurve),
			ColorGradient: colorCurveFromJSON(doc.Trail.ColorGradient),
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
				subCfg, err = configFromJSON(sub.Config, depth+1)
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

func shapeToJSON(s Shape) *jsonShape {
	switch s.kind {
	case shapeCircle:
		return &jsonShape{Kind: "circle", Radius: s.radius}
	case shapeRing:
		return &jsonShape{Kind: "ring", Radius: s.radius}
	case shapeRect:
		return &jsonShape{Kind: "rect", Width: s.width, Height: s.height}
	default:
		return &jsonShape{Kind: "point"}
	}
}

func shapeFromJSON(js *jsonShape) (Shape, error) {
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

func floatCurveToJSON(c FloatCurve) []jsonFloatStop {
	if len(c) == 0 {
		return nil
	}
	out := make([]jsonFloatStop, len(c))
	for i, s := range c {
		out[i] = jsonFloatStop{T: s.T, Value: s.Value}
	}
	return out
}

func floatCurveFromJSON(stops []jsonFloatStop) FloatCurve {
	if len(stops) == 0 {
		return nil
	}
	out := make(FloatCurve, len(stops))
	for i, s := range stops {
		out[i] = FloatStop{T: s.T, Value: s.Value}
	}
	return out
}

func colorCurveToJSON(c ColorCurve) []jsonColorStop {
	if len(c) == 0 {
		return nil
	}
	out := make([]jsonColorStop, len(c))
	for i, s := range c {
		out[i] = jsonColorStop{T: s.T, Color: jsonColor(s.Color)}
	}
	return out
}

func colorCurveFromJSON(stops []jsonColorStop) ColorCurve {
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

	// Preserve runtime Collider when reloading from JSON where Collider is nil.
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

// LoadHotConfigFile loads a versioned JSON emitter definition from path and returns a HotConfig bound to that file path.
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

// ReloadBytes decodes versioned JSON bytes and, if valid, applies the new Config to all bound Emitters.
// Invalid JSON returns an error and leaves the current Config and Emitters unchanged.
func (h *HotConfig) ReloadBytes(data []byte) error {
	cfg, err := UnmarshalConfigJSON(data)
	if err != nil {
		return err
	}
	h.Apply(cfg)
	return nil
}

// ReloadFile reads a versioned JSON file from path (or the stored file path if path is empty) and applies it.
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

// StageBytes decodes versioned JSON bytes from a background watcher thread without touching Emitters yet.
// Call Flush from the game loop thread to apply the staged config safely.
func (h *HotConfig) StageBytes(data []byte) error {
	cfg, err := UnmarshalConfigJSON(data)
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
