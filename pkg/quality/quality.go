// Package quality picks a game's low, medium or high quality preset (resolution scale, particle budget, effects and shadows) from the device it runs on and the frame times it measures, and lets the player override the choice in settings.
//
// A Config, written in YAML, holds the three presets, rules that place known device models, and how frame times move the level. A Selector starts at the level the Config gives the Device, steps down when frames run slower than the target and up when they keep well inside it, and holds whatever level the player picks instead of "auto".
package quality

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// Level is a quality tier, from Low to High.
type Level uint8

const (
	// Low is the tier for low-end phones and anything that cannot hold its frame rate higher.
	Low Level = iota
	// Medium is the tier for mid-range devices and for hardware the Selector cannot place.
	Medium
	// High is the tier for capable desktops, recent phones and tablets.
	High
)

// levelCount is how many levels there are.
const levelCount = int(High) + 1

// Levels returns every level from Low to High.
func Levels() []Level { return []Level{Low, Medium, High} }

// String returns the level's name: "low", "medium" or "high".
func (l Level) String() string {
	switch l {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	default:
		return fmt.Sprintf("Level(%d)", uint8(l))
	}
}

// Valid reports whether l is Low, Medium or High.
func (l Level) Valid() bool { return l <= High }

// ParseLevel returns the level named s ("low", "medium" or "high"), ignoring case and surrounding space.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return Low, nil
	case "medium":
		return Medium, nil
	case "high":
		return High, nil
	default:
		return 0, fmt.Errorf("quality: unknown level %q (want low, medium or high)", s)
	}
}

// MarshalText returns the level's name, so YAML and JSON write it as "low", "medium" or "high".
func (l Level) MarshalText() ([]byte, error) {
	if !l.Valid() {
		return nil, fmt.Errorf("quality: invalid level %d", uint8(l))
	}
	return []byte(l.String()), nil
}

// UnmarshalText reads a level's name as ParseLevel does.
func (l *Level) UnmarshalText(text []byte) error {
	v, err := ParseLevel(string(text))
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// SettingAuto is the setting that lets the Selector choose the level from the device and its frame times.
const SettingAuto = "auto"

// NormalizeSetting returns the canonical form of a player's quality setting: "auto" for s empty or "auto", or the name of the level s names, ignoring case and surrounding space. It returns an error for anything else.
func NormalizeSetting(s string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" || t == SettingAuto {
		return SettingAuto, nil
	}
	l, err := ParseLevel(t)
	if err != nil {
		return "", fmt.Errorf("quality: unknown setting %q (want auto, low, medium or high)", s)
	}
	return l.String(), nil
}

// Preset is what one quality level turns on and how far it scales the game's costs.
type Preset struct {
	// ResolutionScale scales the resolution of buffers the game renders or rasterizes itself, such as render3d.OcclusionBuffer and procedurally drawn images, in (0, 1]. Read it through Scale and ScaleSize. It does not lower the resolution of a core.Canvas itself: NSGo v0.3.0 draws a canvas at the display's full pixel density and has no setting to render it smaller and upscale it. Nothing in this module applies it for the game; the game sizes its own buffers with it.
	ResolutionScale float32 `yaml:"resolution_scale"`
	// ParticleScale scales particle budgets and burst counts, in (0, 1]. Read it through Particles.
	ParticleScale float32 `yaml:"particle_scale"`
	// Effects turns on effects that only decorate the frame, such as screen flashes, parallax backdrops, post effects and trails.
	Effects bool `yaml:"effects"`
	// Shadows turns on shadow casting, such as lights2d's occluder clipping (lights2d.Scene.DisableShadows is its inverse).
	Shadows bool `yaml:"shadows"`
}

// DefaultPreset returns the built-in preset for l: Low renders at half resolution with a quarter of the particles and no effects or shadows, Medium at three quarters with 60% of the particles and effects, and High at full resolution with everything on. It returns the Medium preset for an invalid level.
func DefaultPreset(l Level) Preset {
	switch l {
	case Low:
		return Preset{ResolutionScale: 0.5, ParticleScale: 0.25}
	case High:
		return Preset{ResolutionScale: 1, ParticleScale: 1, Effects: true, Shadows: true}
	default:
		return Preset{ResolutionScale: 0.75, ParticleScale: 0.6, Effects: true}
	}
}

// Validate reports an error when a scale is not finite or lies outside (0, 1].
func (p Preset) Validate() error {
	return p.validateAt("")
}

func (p Preset) validateAt(prefix string) error {
	field := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	if !unitScale(p.ResolutionScale) {
		return loaderr.Errorf(field("resolution_scale"), "set resolution_scale to a number in (0, 1] such as 0.75 or 1", "quality: %s %v outside (0, 1]", field("resolution_scale"), p.ResolutionScale)
	}
	if !unitScale(p.ParticleScale) {
		return loaderr.Errorf(field("particle_scale"), "set particle_scale to a number in (0, 1] such as 0.6 or 1", "quality: %s %v outside (0, 1]", field("particle_scale"), p.ParticleScale)
	}
	return nil
}

// Particles returns n scaled by ParticleScale and rounded, at least 1 when n is positive, so it can set a particles.Emitter's MaxParticles (where 0 means the default of 256) or a Burst's count. It returns n unchanged when ParticleScale is not a valid scale, and 0 when n is not positive.
func (p Preset) Particles(n int) int { return scaleCount(n, p.ParticleScale) }

// Scale returns n, a pixel count, scaled by ResolutionScale and rounded, at least 1 when n is positive. It returns n unchanged when ResolutionScale is not a valid scale, and 0 when n is not positive.
func (p Preset) Scale(n int) int { return scaleCount(n, p.ResolutionScale) }

// ScaleSize returns size scaled by ResolutionScale, or size unchanged when ResolutionScale is not a valid scale.
func (p Preset) ScaleSize(size core.Size) core.Size {
	if !unitScale(p.ResolutionScale) {
		return size
	}
	return core.Size{Width: size.Width * core.Points(p.ResolutionScale), Height: size.Height * core.Points(p.ResolutionScale)}
}

func unitScale(v float32) bool {
	return v > 0 && v <= 1
}

func scaleCount(n int, scale float32) int {
	if n <= 0 {
		return 0
	}
	if !unitScale(scale) {
		return n
	}
	return max(1, int(math.Round(float64(n)*float64(scale))))
}
