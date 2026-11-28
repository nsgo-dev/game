package sceneformat

import "fmt"

// BehaviorConfig is the shared, versioned configuration for input-driven
// scene behaviors. Runtime components may consume the typed fields directly.
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
	Enabled      bool     `yaml:"enabled,omitempty"`
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
