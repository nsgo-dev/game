package vehicle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"go.yaml.in/yaml/v3"
)

// ConfigVersion is the Config schema version written by EncodeConfig and accepted by ParseConfig.
const ConfigVersion = 1

// maxWheels bounds Config.Wheels so a malformed file cannot ask for unbounded work per step.
const maxWheels = 32

// Config is a car's tuning: chassis mass and box, engine, brakes, steering, drag and wheels. Lengths are meters, masses kilograms, forces newtons and angles degrees.
//
// Positions are in chassis space relative to the center of mass (the chassis body's Pos), with -Z forward, +X right and +Y up.
type Config struct {
	// Version is the schema version; it must equal ConfigVersion.
	Version int `yaml:"version"`
	// Name optionally labels the car.
	Name string `yaml:"name,omitempty"`
	// Mass is the chassis mass in kilograms (> 0).
	Mass float32 `yaml:"mass"`
	// ChassisHalfExtents are the half sizes of the chassis collision box (each > 0).
	ChassisHalfExtents [3]float32 `yaml:"chassisHalfExtents,flow"`
	// ChassisOffset moves the chassis box relative to the center of mass; a positive Y lowers the center of mass inside the box, which steadies the car.
	ChassisOffset [3]float32 `yaml:"chassisOffset,flow"`
	// EngineForce is the total forward drive force at full throttle and standstill, split evenly over the drive wheels (>= 0).
	EngineForce float32 `yaml:"engineForce"`
	// ReverseForce is the total drive force at full reverse throttle (>= 0).
	ReverseForce float32 `yaml:"reverseForce"`
	// MaxSpeed is the forward speed in m/s at which the engine force fades to zero (> 0).
	MaxSpeed float32 `yaml:"maxSpeed"`
	// MaxReverseSpeed is the reverse speed in m/s at which the reverse force fades to zero (> 0).
	MaxReverseSpeed float32 `yaml:"maxReverseSpeed"`
	// BrakeForce is the total braking force at full brake, split evenly over the brake wheels (>= 0).
	BrakeForce float32 `yaml:"brakeForce"`
	// HandbrakeGrip scales the lateral grip of handbrake wheels while the handbrake is held, in [0, 1]; lower values slide more.
	HandbrakeGrip float32 `yaml:"handbrakeGrip"`
	// MaxSteerAngle is the largest steering angle of steered wheels in degrees, in [0, 80].
	MaxSteerAngle float32 `yaml:"maxSteerAngle"`
	// SteerRate is how fast steered wheels turn toward the requested angle, in degrees per second (> 0).
	SteerRate float32 `yaml:"steerRate"`
	// Drag is the aerodynamic drag coefficient: the drag force is Drag * speed² against the velocity (>= 0).
	Drag float32 `yaml:"drag"`
	// RollingResistance is the rolling resistance coefficient applied against each grounded wheel's rolling speed, as a fraction of its load (>= 0).
	RollingResistance float32 `yaml:"rollingResistance"`
	// RollInfluence scales how much lateral tire force rolls the chassis, in [0, 1]: 0 applies it at the center of mass' height, 1 at the contact point.
	RollInfluence float32 `yaml:"rollInfluence"`
	// AngularDamping damps the chassis' angular velocity (>= 0), as physics3d.Body.AngularDamping.
	AngularDamping float32 `yaml:"angularDamping"`
	// Wheels lists the wheels in a fixed order (1 to 32, unique names).
	Wheels []WheelConfig `yaml:"wheels"`
}

// WheelConfig describes one wheel: where its suspension is mounted, its spring and damper, its tire and which inputs it receives.
type WheelConfig struct {
	// Name identifies the wheel, such as "front-left"; names are unique within a Config.
	Name string `yaml:"name"`
	// Anchor is the top of the suspension in chassis space, relative to the center of mass.
	Anchor [3]float32 `yaml:"anchor,flow"`
	// Radius is the wheel radius (> 0).
	Radius float32 `yaml:"radius"`
	// RestLength is the suspension length with no load (>= 0); the wheel ray is RestLength + Radius long.
	RestLength float32 `yaml:"restLength"`
	// Stiffness is the spring rate in newtons per meter of compression (> 0).
	Stiffness float32 `yaml:"stiffness"`
	// Damping is the damper rate in newton-seconds per meter (>= 0).
	Damping float32 `yaml:"damping"`
	// MaxForce caps the suspension force in newtons; 0 leaves it uncapped.
	MaxForce float32 `yaml:"maxForce,omitempty"`
	// Friction is the tire's friction coefficient: the combined tire force stays within Friction times the wheel's load (>= 0).
	Friction float32 `yaml:"friction"`
	// Steer marks a wheel turned by steering input.
	Steer bool `yaml:"steer,omitempty"`
	// Drive marks a wheel driven by the engine.
	Drive bool `yaml:"drive,omitempty"`
	// Brake marks a wheel slowed by brake input.
	Brake bool `yaml:"brake,omitempty"`
	// Handbrake marks a wheel locked by the handbrake.
	Handbrake bool `yaml:"handbrake,omitempty"`
}

// ReferenceConfig returns the tuning of the reference car: a 1200 kg rear-wheel-drive hatchback with four wheels, about 0.5 g of acceleration, 1 g of braking and a 40 m/s top speed.
func ReferenceConfig() Config {
	wheel := func(name string, x, z float32, front bool) WheelConfig {
		return WheelConfig{
			Name:       name,
			Anchor:     [3]float32{x, -0.1, z},
			Radius:     0.35,
			RestLength: 0.3,
			Stiffness:  30000,
			Damping:    4000,
			MaxForce:   20000,
			Friction:   1.2,
			Steer:      front,
			Drive:      !front,
			Brake:      true,
			Handbrake:  !front,
		}
	}
	return Config{
		Version:            ConfigVersion,
		Name:               "reference",
		Mass:               1200,
		ChassisHalfExtents: [3]float32{0.9, 0.3, 2},
		ChassisOffset:      [3]float32{0, 0.2, 0},
		EngineForce:        6000,
		ReverseForce:       3000,
		MaxSpeed:           40,
		MaxReverseSpeed:    8,
		BrakeForce:         12000,
		HandbrakeGrip:      0.4,
		MaxSteerAngle:      30,
		SteerRate:          120,
		Drag:               0.4,
		RollingResistance:  0.015,
		RollInfluence:      0.1,
		AngularDamping:     0.5,
		Wheels: []WheelConfig{
			wheel("front-left", -0.8, -1.3, true),
			wheel("front-right", 0.8, -1.3, true),
			wheel("rear-left", -0.8, 1.3, false),
			wheel("rear-right", 0.8, 1.3, false),
		},
	}
}

// Validate checks that cfg uses ConfigVersion and that every value is finite and within the range its field documents.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return errors.New("vehicle: nil config")
	}
	if cfg.Version != ConfigVersion {
		return fmt.Errorf("vehicle: unsupported config version %d (want %d)", cfg.Version, ConfigVersion)
	}
	if err := positive("mass", cfg.Mass); err != nil {
		return err
	}
	for i, e := range cfg.ChassisHalfExtents {
		if err := positive(fmt.Sprintf("chassisHalfExtents[%d]", i), e); err != nil {
			return err
		}
	}
	for i, o := range cfg.ChassisOffset {
		if !finite(o) {
			return fmt.Errorf("vehicle: chassisOffset[%d] must be finite", i)
		}
	}
	for _, f := range []struct {
		name string
		v    float32
	}{
		{"engineForce", cfg.EngineForce},
		{"reverseForce", cfg.ReverseForce},
		{"brakeForce", cfg.BrakeForce},
		{"drag", cfg.Drag},
		{"rollingResistance", cfg.RollingResistance},
		{"angularDamping", cfg.AngularDamping},
	} {
		if err := nonNegative(f.name, f.v); err != nil {
			return err
		}
	}
	for _, f := range []struct {
		name string
		v    float32
	}{
		{"maxSpeed", cfg.MaxSpeed},
		{"maxReverseSpeed", cfg.MaxReverseSpeed},
		{"steerRate", cfg.SteerRate},
	} {
		if err := positive(f.name, f.v); err != nil {
			return err
		}
	}
	if err := inRange("handbrakeGrip", cfg.HandbrakeGrip, 0, 1); err != nil {
		return err
	}
	if err := inRange("rollInfluence", cfg.RollInfluence, 0, 1); err != nil {
		return err
	}
	if err := inRange("maxSteerAngle", cfg.MaxSteerAngle, 0, 80); err != nil {
		return err
	}
	if len(cfg.Wheels) == 0 {
		return errors.New("vehicle: config has no wheels")
	}
	if len(cfg.Wheels) > maxWheels {
		return fmt.Errorf("vehicle: config has %d wheels (max %d)", len(cfg.Wheels), maxWheels)
	}
	seen := make(map[string]struct{}, len(cfg.Wheels))
	for i := range cfg.Wheels {
		w := &cfg.Wheels[i]
		if w.Name == "" {
			return fmt.Errorf("vehicle: wheel %d has an empty name", i)
		}
		if _, dup := seen[w.Name]; dup {
			return fmt.Errorf("vehicle: duplicate wheel name %q", w.Name)
		}
		seen[w.Name] = struct{}{}
		if err := w.validate(); err != nil {
			return fmt.Errorf("vehicle: wheel %q: %w", w.Name, err)
		}
	}
	return nil
}

func (w *WheelConfig) validate() error {
	for i, a := range w.Anchor {
		if !finite(a) {
			return fmt.Errorf("anchor[%d] must be finite", i)
		}
	}
	if err := positive("radius", w.Radius); err != nil {
		return err
	}
	if err := positive("stiffness", w.Stiffness); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		v    float32
	}{
		{"restLength", w.RestLength},
		{"damping", w.Damping},
		{"maxForce", w.MaxForce},
		{"friction", w.Friction},
	} {
		if err := nonNegative(f.name, f.v); err != nil {
			return err
		}
	}
	return nil
}

func finite(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func positive(name string, v float32) error {
	if !finite(v) || v <= 0 {
		return fmt.Errorf("vehicle: %s must be finite and > 0, got %v", name, v)
	}
	return nil
}

func nonNegative(name string, v float32) error {
	if !finite(v) || v < 0 {
		return fmt.Errorf("vehicle: %s must be finite and >= 0, got %v", name, v)
	}
	return nil
}

func inRange(name string, v, lo, hi float32) error {
	if !finite(v) || v < lo || v > hi {
		return fmt.Errorf("vehicle: %s must be in [%v, %v], got %v", name, lo, hi, v)
	}
	return nil
}

// ParseConfig decodes a YAML car Config and validates it. Unknown fields and trailing YAML documents are rejected.
func ParseConfig(data []byte) (Config, error) {
	var cfg Config
	if err := decodeStrict(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("vehicle: config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// EncodeConfig validates cfg and serializes it as deterministic 2-space YAML that ParseConfig reads back unchanged.
func EncodeConfig(cfg Config) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	data, err := encodeYAML(&cfg)
	if err != nil {
		return nil, fmt.Errorf("vehicle: config: %w", err)
	}
	return data, nil
}

func decodeStrict(data []byte, out any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), nil
}
