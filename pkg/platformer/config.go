package platformer

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const fixConfig = "write the platformer config as version: 1 YAML with non-negative finite tunables"

// CurrentVersion is the controller config schema version this package reads and writes.
const CurrentVersion = 1

// Config holds a Controller's tunables. Speeds are in points per second and accelerations in points per second squared, with Y increasing downward as in package physics; every value is non-negative, and the controller applies the sign. Durations are Go duration strings in YAML, such as "100ms".
type Config struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`

	// CoyoteTime is how long after the body was last on the ground a jump still leaves from the ground. Zero allows a ground jump only while grounded.
	CoyoteTime time.Duration `yaml:"coyote_time"`
	// JumpBuffer is how long a jump press waits for the body to be able to jump. Zero honors a press only on the step it happens.
	JumpBuffer time.Duration `yaml:"jump_buffer"`
	// JumpSpeed is the upward speed a ground jump starts with.
	JumpSpeed float32 `yaml:"jump_speed"`
	// JumpCut multiplies the upward speed once when the jump button is let go during a jump, in [0, 1]; 1 turns variable jump height off and 0 stops the rise at once.
	JumpCut float32 `yaml:"jump_cut"`
	// JumpCutMinSpeed is the upward speed a release must exceed to be cut, so a release near the top of a jump changes nothing.
	JumpCutMinSpeed float32 `yaml:"jump_cut_min_speed"`
	// Gravity is the downward acceleration applied every step.
	Gravity float32 `yaml:"gravity"`
	// MaxFallSpeed caps the downward speed; zero leaves it uncapped.
	MaxFallSpeed float32 `yaml:"max_fall_speed"`

	// RunSpeed is the horizontal speed full input reaches. Zero leaves horizontal velocity to the game, apart from wall jumps.
	RunSpeed float32 `yaml:"run_speed"`
	// GroundAccel is the horizontal acceleration toward the input's speed on the ground; zero reaches it at once.
	GroundAccel float32 `yaml:"ground_accel"`
	// GroundDecel is the horizontal deceleration on the ground without input; zero stops at once.
	GroundDecel float32 `yaml:"ground_decel"`
	// AirAccel is the horizontal acceleration toward the input's speed in the air; zero reaches it at once.
	AirAccel float32 `yaml:"air_accel"`

	// WallSlideSpeed caps the downward speed while the body falls against a wall it is pushing into; zero turns wall sliding off.
	WallSlideSpeed float32 `yaml:"wall_slide_speed"`
	// WallCoyoteTime is how long after the body last touched a wall in the air a jump still leaves from that wall.
	WallCoyoteTime time.Duration `yaml:"wall_coyote_time"`
	// WallJumpSpeedX is the horizontal speed a wall jump starts with, away from the wall.
	WallJumpSpeedX float32 `yaml:"wall_jump_speed_x"`
	// WallJumpSpeedY is the upward speed a wall jump starts with; zero turns wall jumps off.
	WallJumpSpeedY float32 `yaml:"wall_jump_speed_y"`
	// WallJumpLock is how long horizontal input is ignored after a wall jump, so holding toward the wall does not cancel the push away from it.
	WallJumpLock time.Duration `yaml:"wall_jump_lock"`
}

// DefaultConfig returns a Config tuned for a 16-point tile platformer: 100 ms of coyote time and jump buffer, a cut-short jump, wall slides and wall jumps.
func DefaultConfig() Config {
	return Config{
		Version:         CurrentVersion,
		CoyoteTime:      100 * time.Millisecond,
		JumpBuffer:      100 * time.Millisecond,
		JumpSpeed:       284,
		JumpCut:         0.55,
		JumpCutMinSpeed: 110,
		Gravity:         720,
		MaxFallSpeed:    320,
		RunSpeed:        98,
		GroundAccel:     960,
		GroundDecel:     1150,
		AirAccel:        680,
		WallSlideSpeed:  60,
		WallCoyoteTime:  80 * time.Millisecond,
		WallJumpSpeedX:  150,
		WallJumpSpeedY:  260,
		WallJumpLock:    150 * time.Millisecond,
	}
}

// Validate reports whether c is a version 1 config with finite, non-negative values and JumpCut in [0, 1].
func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", CurrentVersion), "platformer: unsupported config version %d (want %d)", c.Version, CurrentVersion)
	}
	durations := []struct {
		name string
		v    time.Duration
	}{
		{"coyote_time", c.CoyoteTime},
		{"jump_buffer", c.JumpBuffer},
		{"wall_coyote_time", c.WallCoyoteTime},
		{"wall_jump_lock", c.WallJumpLock},
	}
	for _, d := range durations {
		if d.v < 0 {
			return loaderr.Errorf(d.name, fmt.Sprintf("set %s to a non-negative duration such as 100ms", d.name), "platformer: %s is negative (%v)", d.name, d.v)
		}
	}
	values := []struct {
		name string
		v    float32
	}{
		{"jump_speed", c.JumpSpeed},
		{"jump_cut", c.JumpCut},
		{"jump_cut_min_speed", c.JumpCutMinSpeed},
		{"gravity", c.Gravity},
		{"max_fall_speed", c.MaxFallSpeed},
		{"run_speed", c.RunSpeed},
		{"ground_accel", c.GroundAccel},
		{"ground_decel", c.GroundDecel},
		{"air_accel", c.AirAccel},
		{"wall_slide_speed", c.WallSlideSpeed},
		{"wall_jump_speed_x", c.WallJumpSpeedX},
		{"wall_jump_speed_y", c.WallJumpSpeedY},
	}
	for _, f := range values {
		v := float64(f.v)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return loaderr.Errorf(f.name, fmt.Sprintf("set %s to a finite non-negative number", f.name), "platformer: %s is not finite", f.name)
		}
		if v < 0 {
			return loaderr.Errorf(f.name, fmt.Sprintf("set %s to a non-negative number", f.name), "platformer: %s is negative (%v)", f.name, f.v)
		}
	}
	if c.JumpCut > 1 {
		return loaderr.Errorf("jump_cut", "set jump_cut to a number in [0, 1]", "platformer: jump_cut %v is above 1", c.JumpCut)
	}
	return nil
}

// ParseConfig decodes and validates a YAML controller config. Unknown fields, a missing or unsupported version, and trailing YAML documents are refused.
func ParseConfig(data []byte) (Config, error) {
	c, err := parseConfig(data)
	if err != nil {
		return Config{}, loaderr.Locate(err, "", data, fixConfig)
	}
	return c, nil
}

func parseConfig(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, loaderr.Errorf("", fixConfig, "platformer: empty config document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("platformer: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, loaderr.Errorf("", "keep one YAML document in the config; remove the trailing --- document", "platformer: unexpected trailing YAML document")
		}
		return Config{}, fmt.Errorf("platformer: decode: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	yamlzero.Normalize(&c)
	return c, nil
}

// EncodeConfig validates c and writes it as 2-space YAML with every field in a fixed order, so ParseConfig reads back the same Config and encoding that again gives the same bytes.
func EncodeConfig(c Config) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(c)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("platformer: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("platformer: encode: %w", err)
	}
	return buf.Bytes(), nil
}
