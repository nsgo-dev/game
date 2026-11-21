// Package replay records per-step game input to compact versioned files and plays it back deterministically for debugging and automated tests.
package replay

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"time"
)

// FormatVersion is the version written to every Recording and accepted by Decode.
const FormatVersion = 1

const defaultRate = 60

// ActionSource supplies named actions and axes sampled once per game.Loop step.
//
// *input.Actions satisfies ActionSource for live play, *Recorder wraps an ActionSource to record it, and *Player satisfies ActionSource for playback, so a game reading input through ActionSource behaves identically in all three modes.
type ActionSource interface {
	Pressed(name string) bool
	JustPressed(name string) bool
	JustReleased(name string) bool
	Axis(name string) float64
	Step()
}

// Recording holds a game's random seed, fixed step rate, watched action and axis names, total step count and delta-encoded step events.
//
// Only steps where at least one watched action's pressed state or watched axis's value changed from the previous step appear in Events, keeping long recordings compact.
type Recording struct {
	Version    int         `json:"version"`
	Seed       uint64      `json:"seed"`
	Rate       int         `json:"rate"`
	Actions    []string    `json:"actions,omitempty"`
	Axes       []string    `json:"axes,omitempty"`
	TotalSteps int         `json:"total_steps"`
	Events     []StepEvent `json:"events,omitempty"`
}

// StepEvent holds the action and axis changes that occurred on Step.
//
// Press and Release hold indices into Recording.Actions. Axes holds index-to-value updates into Recording.Axes.
type StepEvent struct {
	Step    int          `json:"s"`
	Press   []int        `json:"p,omitempty"`
	Release []int        `json:"r,omitempty"`
	Axes    []AxisChange `json:"a,omitempty"`
}

// AxisChange records a new value for Recording.Axes[Index].
type AxisChange struct {
	Index int     `json:"i"`
	Value float64 `json:"v"`
}

// StepDuration returns the fixed duration of one step, 1/Rate seconds (1/60 s when Rate <= 0).
func (r *Recording) StepDuration() time.Duration {
	rate := defaultRate
	if r != nil && r.Rate > 0 {
		rate = r.Rate
	}
	return time.Second / time.Duration(rate)
}

// Validate checks that r uses the supported format version, has a valid rate and step count, unique non-empty action and axis names, and strictly increasing in-bounds step events.
func (r *Recording) Validate() error {
	if r == nil {
		return fmt.Errorf("replay: recording is nil")
	}
	if r.Version != FormatVersion {
		return fmt.Errorf("replay: unsupported version %d (want %d)", r.Version, FormatVersion)
	}
	if r.Rate <= 0 {
		return fmt.Errorf("replay: invalid rate %d", r.Rate)
	}
	if r.TotalSteps < 0 {
		return fmt.Errorf("replay: invalid total_steps %d", r.TotalSteps)
	}
	if err := validateNames("action", r.Actions); err != nil {
		return err
	}
	if err := validateNames("axis", r.Axes); err != nil {
		return err
	}

	prevStep := -1
	for i, ev := range r.Events {
		if ev.Step < 0 || ev.Step >= r.TotalSteps {
			return fmt.Errorf("replay: event %d step %d out of range [0, %d)", i, ev.Step, r.TotalSteps)
		}
		if ev.Step <= prevStep {
			return fmt.Errorf("replay: event %d step %d is not after previous step %d", i, ev.Step, prevStep)
		}
		prevStep = ev.Step

		var seenAction map[int]struct{}
		if len(ev.Press)+len(ev.Release) > 1 {
			seenAction = make(map[int]struct{}, len(ev.Press)+len(ev.Release))
		}
		for _, idx := range ev.Press {
			if idx < 0 || idx >= len(r.Actions) {
				return fmt.Errorf("replay: step %d press index %d out of range [0, %d)", ev.Step, idx, len(r.Actions))
			}
			if seenAction != nil {
				if _, dup := seenAction[idx]; dup {
					return fmt.Errorf("replay: step %d duplicate action index %d", ev.Step, idx)
				}
				seenAction[idx] = struct{}{}
			}
		}
		for _, idx := range ev.Release {
			if idx < 0 || idx >= len(r.Actions) {
				return fmt.Errorf("replay: step %d release index %d out of range [0, %d)", ev.Step, idx, len(r.Actions))
			}
			if seenAction != nil {
				if _, dup := seenAction[idx]; dup {
					return fmt.Errorf("replay: step %d duplicate action index %d", ev.Step, idx)
				}
				seenAction[idx] = struct{}{}
			}
		}

		var seenAxis map[int]struct{}
		if len(ev.Axes) > 1 {
			seenAxis = make(map[int]struct{}, len(ev.Axes))
		}
		for _, ch := range ev.Axes {
			if ch.Index < 0 || ch.Index >= len(r.Axes) {
				return fmt.Errorf("replay: step %d axis index %d out of range [0, %d)", ev.Step, ch.Index, len(r.Axes))
			}
			if math.IsNaN(ch.Value) || math.IsInf(ch.Value, 0) {
				return fmt.Errorf("replay: step %d axis %d has non-finite value", ev.Step, ch.Index)
			}
			if seenAxis != nil {
				if _, dup := seenAxis[ch.Index]; dup {
					return fmt.Errorf("replay: step %d duplicate axis index %d", ev.Step, ch.Index)
				}
				seenAxis[ch.Index] = struct{}{}
			}
		}
	}
	return nil
}

func validateNames(kind string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	for i, name := range names {
		if name == "" {
			return fmt.Errorf("replay: %s %d has an empty name", kind, i)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("replay: duplicate %s name %q", kind, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// Clone returns an independent deep copy of r, or nil if r is nil.
func (r *Recording) Clone() *Recording {
	if r == nil {
		return nil
	}
	out := &Recording{
		Version:    r.Version,
		Seed:       r.Seed,
		Rate:       r.Rate,
		Actions:    slices.Clone(r.Actions),
		Axes:       slices.Clone(r.Axes),
		TotalSteps: r.TotalSteps,
	}
	if len(r.Events) > 0 {
		out.Events = make([]StepEvent, len(r.Events))
		for i, ev := range r.Events {
			out.Events[i] = StepEvent{
				Step:    ev.Step,
				Press:   slices.Clone(ev.Press),
				Release: slices.Clone(ev.Release),
				Axes:    slices.Clone(ev.Axes),
			}
		}
	}
	return out
}

// Encode validates r and encodes it as compact JSON.
func (r *Recording) Encode() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("replay: encode: %w", err)
	}
	return data, nil
}

// Decode decodes a JSON-encoded Recording and validates its version and structure.
func Decode(data []byte) (*Recording, error) {
	var rec Recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("replay: decode: %w", err)
	}
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	return &rec, nil
}

// EncodeGzip validates r, encodes it as JSON, and compresses the result with gzip.
func (r *Recording) EncodeGzip() ([]byte, error) {
	raw, err := r.Encode()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, fmt.Errorf("replay: gzip encode: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("replay: gzip encode: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeGzip decompresses a gzip-encoded Recording and validates its version and structure.
func DecodeGzip(data []byte) (*Recording, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("replay: gzip decode: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("replay: gzip decode: %w", err)
	}
	return Decode(raw)
}

// SaveFile writes rec to path as JSON, or as gzip-compressed JSON when path ends with ".gz".
func SaveFile(path string, rec *Recording) error {
	var (
		data []byte
		err  error
	)
	if strings.HasSuffix(path, ".gz") {
		data, err = rec.EncodeGzip()
	} else {
		data, err = rec.Encode()
	}
	if err != nil {
		return fmt.Errorf("replay: save %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("replay: save %q: %w", path, err)
	}
	return nil
}

// LoadFile reads and decodes a Recording from path, accepting both plain JSON and gzip-compressed files.
func LoadFile(path string) (*Recording, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: load %q: %w", path, err)
	}
	var rec *Recording
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		rec, err = DecodeGzip(data)
	} else {
		rec, err = Decode(data)
	}
	if err != nil {
		return nil, fmt.Errorf("replay: load %q: %w", path, err)
	}
	return rec, nil
}
