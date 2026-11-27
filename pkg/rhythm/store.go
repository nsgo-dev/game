package rhythm

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// CalibrationsVersion is the calibration file schema version this package reads and writes.
const CalibrationsVersion = 1

// Limits a calibration file is validated against.
const (
	// MaxOffsetMsAbs is the largest offset, either way, a device may keep: ten seconds.
	MaxOffsetMsAbs = 10_000
	// MaxDevices is the most devices a calibration file may keep.
	MaxDevices = 256
	// MaxDeviceKey is the longest device key, in bytes.
	MaxDeviceKey = 256
)

// Calibrations are the offsets calibration measured on each device, as the YAML text an OffsetStore keeps:
//
//	version: 1
//	devices:
//	  ios/iPhone15,2:
//	    audioMs: 182.5
//	    videoMs: 21
//
// Devices are written in key order, so the same offsets always encode to the same text, and ParseCalibrations reads it back unchanged.
type Calibrations struct {
	// Version is the schema version; it must be CalibrationsVersion.
	Version int `yaml:"version"`
	// Devices are the offsets by device key, such as DeviceKey returns.
	Devices map[string]DeviceOffsets `yaml:"devices,omitempty"`
}

// DeviceOffsets are a device's Offsets in milliseconds, as written.
type DeviceOffsets struct {
	// AudioMs is Offsets.Audio in milliseconds.
	AudioMs float64 `yaml:"audioMs"`
	// VideoMs is Offsets.Video in milliseconds.
	VideoMs float64 `yaml:"videoMs"`
}

// Offsets returns d as durations, rounded to the nanosecond.
func (d DeviceOffsets) Offsets() Offsets {
	return Offsets{Audio: msDuration(d.AudioMs), Video: msDuration(d.VideoMs)}
}

// DeviceOffsetsOf returns o in milliseconds.
func DeviceOffsetsOf(o Offsets) DeviceOffsets {
	return DeviceOffsets{AudioMs: durationMs(o.Audio), VideoMs: durationMs(o.Video)}
}

func msDuration(ms float64) time.Duration { return time.Duration(math.Round(ms * 1e6)) }

func durationMs(d time.Duration) float64 { return float64(d) / 1e6 }

// Validate reports the first way c breaks the version 1 schema: a wrong version, too many devices, an empty, overlong or invalid UTF-8 key, or an offset that is not a number within ±MaxOffsetMsAbs.
func (c *Calibrations) Validate() error {
	if c == nil {
		return loaderr.Errorf("", fixCalibrations, "rhythm: nil calibrations")
	}
	if c.Version != CalibrationsVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", CalibrationsVersion), "rhythm: calibrations version %d, want %d", c.Version, CalibrationsVersion)
	}
	if len(c.Devices) > MaxDevices {
		return loaderr.Errorf("devices", fmt.Sprintf("keep at most %d devices", MaxDevices), "rhythm: %d devices, at most %d", len(c.Devices), MaxDevices)
	}
	keys := make([]string, 0, len(c.Devices))
	for k := range c.Devices {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		d := c.Devices[k]
		if strings.TrimSpace(k) == "" || len(k) > MaxDeviceKey || !utf8.ValidString(k) {
			return loaderr.Errorf(loaderr.Path("devices", k), fmt.Sprintf("use a non-empty UTF-8 device key of at most %d bytes", MaxDeviceKey), "rhythm: device key %q is empty, longer than %d bytes or not UTF-8", k, MaxDeviceKey)
		}
		for _, f := range []struct {
			name string
			ms   float64
		}{{"audioMs", d.AudioMs}, {"videoMs", d.VideoMs}} {
			if !finite(f.ms) || math.Abs(f.ms) > MaxOffsetMsAbs {
				return loaderr.Errorf(loaderr.Path("devices", k, f.name), fmt.Sprintf("set %s to a finite number within ±%d", f.name, MaxOffsetMsAbs), "rhythm: device %q: offset %v ms is not within ±%d", k, f.ms, MaxOffsetMsAbs)
			}
		}
	}
	return nil
}

// ParseCalibrations decodes a YAML calibration file and validates it. Unknown fields, a second YAML document, anchors and aliases, and a fractional version are refused.
func ParseCalibrations(data []byte) (*Calibrations, error) {
	c, err := parseCalibrations(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixCalibrations)
	}
	return c, nil
}

func parseCalibrations(data []byte) (*Calibrations, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixCalibrations, "rhythm: empty calibrations")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Calibrations
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("rhythm: decode calibrations: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in calibrations; remove the trailing --- document", "rhythm: calibrations have a second YAML document")
		}
		return nil, fmt.Errorf("rhythm: decode calibrations: %w", err)
	}
	if err := requireIntegers(data, []string{"version"}, nil); err != nil {
		return nil, err
	}
	// An empty map is written as none, so it reads back as none.
	if len(c.Devices) == 0 {
		c.Devices = nil
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&c)
	return &c, nil
}

// EncodeCalibrations validates c and writes it as deterministic 2-space YAML, devices in key order.
func EncodeCalibrations(c *Calibrations) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return encodeYAML(c)
}

// DeviceKey returns a key for the device cx runs on: its OS and hardware model, such as "ios/iPhone15,2", or the OS and "unknown" where the platform names no model, as the web does, cut to MaxDeviceKey bytes at a character's start. NSGo names no audio output route, so a game that wants separate offsets for headphones and speakers adds its own suffix, such as "/bluetooth", chosen by the player.
func DeviceKey(cx *core.Scope) string {
	env := cx.Env()
	model := strings.TrimSpace(core.Untracked(cx, env.DeviceModel))
	if model == "" {
		model = "unknown"
	}
	sys := strings.TrimSpace(string(core.Untracked(cx, env.OS)))
	if sys == "" {
		sys = "unknown"
	}
	key := sys + "/" + model
	if len(key) > MaxDeviceKey {
		// Cut at a character's start, so a long model name in any script stays valid UTF-8.
		n := MaxDeviceKey
		for n > 0 && !utf8.RuneStart(key[n]) {
			n--
		}
		key = key[:n]
	}
	return key
}

// OffsetStore keeps each device's offsets as a Calibrations YAML document in the app's storage (core.Storage) under one key, so they hold across launches and a player who moves their save to another device calibrates that device anew.
type OffsetStore struct {
	store core.Storage
	key   string
}

// NewOffsetStore returns a store keeping its document in cx's storage under key, such as "rhythm.calibration".
func NewOffsetStore(cx *core.Scope, key string) *OffsetStore {
	return &OffsetStore{store: cx.Storage(), key: key}
}

// Load returns the document kept, an empty one when none is, or why the text kept is not one. Read in a computation, it subscribes it to changes, as core.Storage does.
func (s *OffsetStore) Load() (*Calibrations, error) {
	text, ok := s.store.GetItem(s.key)
	if !ok {
		return &Calibrations{Version: CalibrationsVersion}, nil
	}
	c, err := ParseCalibrations([]byte(text))
	if err != nil {
		return nil, loaderr.WithFile(err, s.key)
	}
	return c, nil
}

// Get returns device's offsets and whether they are kept; a document that is not readable keeps none.
func (s *OffsetStore) Get(device string) (Offsets, bool) {
	c, err := s.Load()
	if err != nil {
		return Offsets{}, false
	}
	d, ok := c.Devices[device]
	return d.Offsets(), ok
}

// Set keeps o for device, rounded to the nanosecond as milliseconds, alongside the other devices'. A document that is not readable, such as one a later schema wrote, is replaced. It fails when o or device is out of range, or with a *core.StorageFullError when the storage is full, keeping what it held.
func (s *OffsetStore) Set(device string, o Offsets) error {
	c, err := s.Load()
	if err != nil {
		c = &Calibrations{Version: CalibrationsVersion}
	}
	devices := maps.Clone(c.Devices)
	if devices == nil {
		devices = map[string]DeviceOffsets{}
	}
	devices[device] = DeviceOffsetsOf(o)
	return s.save(&Calibrations{Version: CalibrationsVersion, Devices: devices})
}

// Remove forgets device's offsets. It fails as Set does, and also when the document kept is not readable, which it leaves as it is.
func (s *OffsetStore) Remove(device string) error {
	c, err := s.Load()
	if err != nil {
		return err
	}
	if _, ok := c.Devices[device]; !ok {
		return nil
	}
	devices := maps.Clone(c.Devices)
	delete(devices, device)
	return s.save(&Calibrations{Version: CalibrationsVersion, Devices: devices})
}

func (s *OffsetStore) save(c *Calibrations) error {
	text, err := EncodeCalibrations(c)
	if err != nil {
		return err
	}
	return s.store.SetItem(s.key, string(text))
}
