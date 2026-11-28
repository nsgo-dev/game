package sceneformat

import (
	"bytes"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// BehaviorConfig is the shared, versioned configuration for input-driven scene behaviors (#1752). It only describes and validates settings; no runtime component steps these behaviors yet.
type BehaviorConfig struct {
	Version      int      `yaml:"version"`
	Kind         string   `yaml:"kind"`
	Axis         string   `yaml:"axis,omitempty"`
	MaxSpeed     float32  `yaml:"maxSpeed,omitempty"`
	Acceleration float32  `yaml:"acceleration,omitempty"`
	Deceleration float32  `yaml:"deceleration,omitempty"`
	Directions   int      `yaml:"directions,omitempty"`
	TileSize     float32  `yaml:"tileSize,omitempty"`
	Range        float32  `yaml:"range,omitempty"`
	RotateSpeed  float32  `yaml:"rotateSpeed,omitempty"`
	FireRate     float32  `yaml:"fireRate,omitempty"`
	Targets      []string `yaml:"targets,omitempty"`
}

// ParseBehaviorConfig decodes one behavior config from YAML, refusing unknown fields and trailing documents, and validates it.
func ParseBehaviorConfig(data []byte) (BehaviorConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c BehaviorConfig
	if err := dec.Decode(&c); err != nil {
		return BehaviorConfig{}, fmt.Errorf("sceneformat: decode behavior: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return BehaviorConfig{}, fmt.Errorf("sceneformat: behavior: unexpected trailing YAML document")
		}
		return BehaviorConfig{}, fmt.Errorf("sceneformat: decode behavior: %w", err)
	}
	if err := c.Validate(); err != nil {
		return BehaviorConfig{}, err
	}
	return c, nil
}

// Validate checks a behavior config independently of a scene registry.
func (c BehaviorConfig) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("sceneformat: behavior %q has unsupported version %d", c.Kind, c.Version)
	}
	switch c.Kind {
	case "platform", "eight_direction", "tile_movement", "car", "drag_drop", "turret", "line_of_sight", "path_agent":
	default:
		return fmt.Errorf("sceneformat: unknown behavior %q", c.Kind)
	}
	if c.MaxSpeed < 0 || c.Acceleration < 0 || c.Deceleration < 0 || c.TileSize < 0 || c.Range < 0 || c.RotateSpeed < 0 || c.FireRate < 0 {
		return fmt.Errorf("sceneformat: behavior %q has a negative numeric setting", c.Kind)
	}
	if c.Kind == "eight_direction" && c.Directions != 4 && c.Directions != 8 {
		return fmt.Errorf("sceneformat: eight_direction directions must be 4 or 8")
	}
	if c.Kind == "tile_movement" && c.TileSize <= 0 {
		return fmt.Errorf("sceneformat: tile_movement tileSize must be positive")
	}
	return nil
}
