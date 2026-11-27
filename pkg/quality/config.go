package quality

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the schema version of the quality YAML file, the one ParseConfig accepts and EncodeConfig writes.
const FormatVersion = 1

// Defaults that apply when a Config's Adapt leaves a field at 0.
const (
	// DefaultTargetFPS is the frame rate the Selector holds when Adapt.TargetFPS is 0.
	DefaultTargetFPS = 30
	// DefaultWindowFrames is how many frames the Selector judges at a time when Adapt.WindowFrames is 0.
	DefaultWindowFrames = 90
	// DefaultUpgradeWindows is how many fast windows in a row the Selector waits for before stepping up when Adapt.UpgradeWindows is 0.
	DefaultUpgradeWindows = 4
)

// minTargetFPS is the lowest Adapt.TargetFPS Validate accepts: a slow frame at 3 fps (350 ms with the Selector's slack) is still shorter than hitchLimit, so the Selector can still step down.
const minTargetFPS = 3

// Config is a game's quality configuration: its presets, the device rules that place known models, and how measured frame times move the level. It is written in YAML (see ParseConfig) and validated by Validate.
type Config struct {
	// Version is the schema version; it must be FormatVersion.
	Version int `yaml:"version"`
	// Presets holds the preset for each level.
	Presets Presets `yaml:"presets"`
	// Devices places device models at a starting level ahead of Classify's heuristic; the first rule that matches wins.
	Devices []DeviceRule `yaml:"devices,omitempty"`
	// Adapt controls how measured frame times move the level while the setting is auto.
	Adapt Adapt `yaml:"adapt,omitempty"`
}

// Presets holds one Preset per level.
type Presets struct {
	// Low is the preset for Low.
	Low Preset `yaml:"low"`
	// Medium is the preset for Medium.
	Medium Preset `yaml:"medium"`
	// High is the preset for High.
	High Preset `yaml:"high"`
}

// For returns the preset for l, or Medium's for an invalid level.
func (p Presets) For(l Level) Preset {
	switch l {
	case Low:
		return p.Low
	case High:
		return p.High
	default:
		return p.Medium
	}
}

// DeviceRule starts devices whose model begins with Model, ignoring case, at Level, optionally only on one OS.
type DeviceRule struct {
	// Model is the start of core.Env.DeviceModel to match, such as "Google Pixel 8" or "iPhone12,".
	Model string `yaml:"model"`
	// OS limits the rule to one operating system, such as "android" or "ios"; empty matches any.
	OS core.OS `yaml:"os,omitempty"`
	// Level is the level matching devices start at.
	Level Level `yaml:"level"`
}

// Matches reports whether d's model starts with r.Model, ignoring case, on r.OS when it is set.
func (r DeviceRule) Matches(d Device) bool {
	if r.OS != "" && r.OS != d.OS {
		return false
	}
	if r.Model == "" || len(d.Model) < len(r.Model) {
		return false
	}
	return strings.EqualFold(d.Model[:len(r.Model)], r.Model)
}

// Adapt controls how the Selector moves the level from measured frame times. Zero fields take the defaults.
type Adapt struct {
	// Off keeps the starting level for the whole run, ignoring frame times.
	Off bool `yaml:"off,omitempty"`
	// TargetFPS is the frame rate to hold, in [3, 240]; below 3 fps a slow frame would be longer than the half second past which the Selector ignores frames, so it could never step down. DefaultTargetFPS when 0. More than a quarter of a window's frames running slower steps the level down.
	TargetFPS int `yaml:"target_fps,omitempty"`
	// WindowFrames is how many frames the Selector judges at a time, in [10, 3600]; DefaultWindowFrames when 0.
	WindowFrames int `yaml:"window_frames,omitempty"`
	// UpgradeWindows is how many windows in a row must run with nine in ten frames inside 60% of the target frame time before the level steps up, in [1, 100]; DefaultUpgradeWindows when 0.
	UpgradeWindows int `yaml:"upgrade_windows,omitempty"`
}

func (a Adapt) targetFPS() int {
	if a.TargetFPS == 0 {
		return DefaultTargetFPS
	}
	return a.TargetFPS
}

func (a Adapt) windowFrames() int {
	if a.WindowFrames == 0 {
		return DefaultWindowFrames
	}
	return a.WindowFrames
}

func (a Adapt) upgradeWindows() int {
	if a.UpgradeWindows == 0 {
		return DefaultUpgradeWindows
	}
	return a.UpgradeWindows
}

// DefaultConfig returns the built-in configuration: DefaultPreset for each level, no device rules and the default adaptation.
func DefaultConfig() Config {
	return Config{
		Version: FormatVersion,
		Presets: Presets{Low: DefaultPreset(Low), Medium: DefaultPreset(Medium), High: DefaultPreset(High)},
	}
}

// StartLevel returns the level a device starts at: the level of the first device rule that matches it, else Classify's.
func (c Config) StartLevel(d Device) Level {
	for _, r := range c.Devices {
		if r.Matches(d) {
			return r.Level
		}
	}
	return Classify(d)
}

var knownOS = []core.OS{core.OSMacOS, core.OSIOS, core.OSAndroid, core.OSWindows, core.OSLinux, core.OSWeb}

// Validate reports the first way c breaks the schema: a version other than FormatVersion, a preset scale outside (0, 1], a device rule with no model, an unknown OS or level or the same model and OS twice, or an adapt field out of range.
func (c Config) Validate() error {
	if c.Version != FormatVersion {
		return fmt.Errorf("quality: unsupported version %d (want %d)", c.Version, FormatVersion)
	}
	for _, l := range Levels() {
		if err := c.Presets.For(l).Validate(); err != nil {
			return fmt.Errorf("quality: presets.%s: %w", l, err)
		}
	}
	seen := make(map[string]bool, len(c.Devices))
	for i, r := range c.Devices {
		if strings.TrimSpace(r.Model) == "" {
			return fmt.Errorf("quality: devices[%d]: empty model", i)
		}
		if strings.TrimSpace(r.Model) != r.Model {
			return fmt.Errorf("quality: devices[%d]: model %q has leading or trailing spaces, so it would never match a device", i, r.Model)
		}
		if r.OS != "" && !isKnownOS(r.OS) {
			return fmt.Errorf("quality: devices[%d]: unknown os %q", i, r.OS)
		}
		if !r.Level.Valid() {
			return fmt.Errorf("quality: devices[%d]: invalid level %d", i, uint8(r.Level))
		}
		key := string(r.OS) + "\x00" + strings.ToLower(r.Model)
		if seen[key] {
			return fmt.Errorf("quality: devices[%d]: duplicate model %q", i, r.Model)
		}
		seen[key] = true
	}
	a := c.Adapt
	if a.TargetFPS != 0 && (a.TargetFPS < minTargetFPS || a.TargetFPS > 240) {
		return fmt.Errorf("quality: adapt.target_fps %d outside [%d, 240]", a.TargetFPS, minTargetFPS)
	}
	if a.WindowFrames != 0 && (a.WindowFrames < 10 || a.WindowFrames > 3600) {
		return fmt.Errorf("quality: adapt.window_frames %d outside [10, 3600]", a.WindowFrames)
	}
	if a.UpgradeWindows != 0 && (a.UpgradeWindows < 1 || a.UpgradeWindows > 100) {
		return fmt.Errorf("quality: adapt.upgrade_windows %d outside [1, 100]", a.UpgradeWindows)
	}
	return nil
}

func isKnownOS(os core.OS) bool {
	for _, k := range knownOS {
		if os == k {
			return true
		}
	}
	return false
}

// ParseConfig decodes a quality YAML document and validates it. Unknown fields, a missing or other version, and trailing documents are refused.
func ParseConfig(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("quality: empty config document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("quality: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("quality: unexpected trailing YAML document")
		}
		return Config{}, fmt.Errorf("quality: decode: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	yamlzero.Normalize(&c)
	return c, nil
}

// EncodeConfig validates c and writes it as deterministic 2-space YAML in field order, so a file read by ParseConfig and written back is unchanged.
func EncodeConfig(c Config) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(c)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("quality: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("quality: encode: %w", err)
	}
	return buf.Bytes(), nil
}
