package bullets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"go.yaml.in/yaml/v3"
)

// FormatVersion is the pattern file schema version that Parse accepts and Encode writes.
const FormatVersion = 1

// MaxCount is the most bullets one shot of a pattern may fire.
const MaxCount = 4096

// Kind is how a pattern places the bullets of a shot.
type Kind string

const (
	// Ring fires Count bullets spaced evenly around a full turn, the first at Angle, turned by Spin more each shot.
	Ring Kind = "ring"
	// Spiral is a ring that turns by Spin (which must not be 0) each shot, so short intervals trace spiral arms: one arm per bullet of Count.
	Spiral Kind = "spiral"
	// Aimed fires Count bullets spread evenly across Spread degrees, centered on the direction from the emitter to its target, turned by Angle and by Spin each shot.
	Aimed Kind = "aimed"
)

// File is a pattern file: a schema version and its patterns, in the order written.
type File struct {
	// Version is the schema version; it must equal FormatVersion.
	Version int `yaml:"version"`
	// Patterns are the file's patterns, each with a unique name.
	Patterns []Pattern `yaml:"patterns"`
}

// Pattern is one bullet pattern: how its shots place, aim and time their bullets, and how those bullets move, live and look.
type Pattern struct {
	// Name identifies the pattern in its file; it must be unique and not empty.
	Name string `yaml:"name"`
	// Kind is how a shot places its bullets: Ring, Spiral or Aimed.
	Kind Kind `yaml:"kind"`
	// Frame is the name of the atlas frame that draws each bullet; an empty Frame spawns bullets that move and collide but draw nothing.
	Frame string `yaml:"frame,omitempty"`
	// Count is the number of bullets each shot fires, from 1 to MaxCount.
	Count int `yaml:"count"`
	// Speed is each bullet's starting speed in points a second.
	Speed float32 `yaml:"speed"`
	// SpeedJitter varies each bullet's starting speed by a random amount in [-SpeedJitter, SpeedJitter), never below 0.
	SpeedJitter float32 `yaml:"speedJitter,omitempty"`
	// Angle is the direction of a shot's first bullet in degrees, clockwise from +X; for Aimed, it turns the whole burst from the target's direction.
	Angle float32 `yaml:"angle,omitempty"`
	// Spread is the arc in degrees an Aimed burst covers, from its first bullet to its last; it applies to Aimed only, and must be above 0 when Count is above 1.
	Spread float32 `yaml:"spread,omitempty"`
	// AngleJitter turns each bullet by a random amount in [-AngleJitter, AngleJitter) degrees.
	AngleJitter float32 `yaml:"angleJitter,omitempty"`
	// Spin turns each shot by this many degrees more than the one before.
	Spin float32 `yaml:"spin,omitempty"`
	// Shots is the number of shots the pattern fires; 0 fires until the emitter is stopped.
	Shots int `yaml:"shots"`
	// Delay is the time before the first shot.
	Delay time.Duration `yaml:"delay,omitempty"`
	// Interval is the time between shots; it must be above 0 unless Shots is 1.
	Interval time.Duration `yaml:"interval,omitempty"`
	// Lifetime is how long each bullet lives; 0 keeps it until it leaves the field's bounds or is removed.
	Lifetime time.Duration `yaml:"lifetime,omitempty"`
	// Accel changes each bullet's speed by this many points a second, each second; a bullet slowed to 0 stops.
	Accel float32 `yaml:"accel,omitempty"`
	// Turn curves each bullet's path by this many degrees a second, clockwise when positive.
	Turn float32 `yaml:"turn,omitempty"`
	// Radius is each bullet's hit radius in points, which the field also uses to tell when a bullet has left its bounds.
	Radius float32 `yaml:"radius,omitempty"`
	// FaceHeading turns each bullet's sprite to the direction it moves, for bullets whose picture has a front.
	FaceHeading bool `yaml:"faceHeading,omitempty"`
	// Z orders the bullets' sprites in the field's batch.
	Z int `yaml:"z,omitempty"`
}

// Parse decodes and validates a YAML pattern file. Unknown fields, trailing YAML documents and invalid patterns are refused.
func Parse(data []byte) (*File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("bullets: empty pattern file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("bullets: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("bullets: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("bullets: decode: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Encode validates f and writes it as YAML with 2-space indents, its fields in a fixed order and optional fields left out when zero, so encoding the same file always gives the same bytes and Parse reads them back unchanged.
func Encode(f *File) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("bullets: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("bullets: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks the file's version, that its pattern names are unique and that each pattern is valid.
func (f *File) Validate() error {
	if f == nil {
		return errors.New("bullets: nil pattern file")
	}
	if f.Version != FormatVersion {
		return fmt.Errorf("bullets: unsupported version %d (want %d)", f.Version, FormatVersion)
	}
	seen := make(map[string]bool, len(f.Patterns))
	for i := range f.Patterns {
		p := &f.Patterns[i]
		if err := p.Validate(); err != nil {
			return fmt.Errorf("bullets: patterns[%d]: %w", i, err)
		}
		if seen[p.Name] {
			return fmt.Errorf("bullets: patterns[%d]: duplicate name %q", i, p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

// Pattern returns the file's pattern named name, or nil and false when it has none.
func (f *File) Pattern(name string) (*Pattern, bool) {
	for i := range f.Patterns {
		if f.Patterns[i].Name == name {
			return &f.Patterns[i], true
		}
	}
	return nil, false
}

// Validate checks that p names itself, has a known kind, fires 1 to MaxCount bullets a shot, has finite numbers in range, and times its shots so that they end or repeat after a positive interval.
func (p *Pattern) Validate() error {
	if p.Name == "" {
		return errors.New("pattern has no name")
	}
	switch p.Kind {
	case Ring, Spiral, Aimed:
	default:
		return fmt.Errorf("pattern %q: unknown kind %q (want ring, spiral or aimed)", p.Name, p.Kind)
	}
	if p.Count < 1 || p.Count > MaxCount {
		return fmt.Errorf("pattern %q: count %d out of range [1, %d]", p.Name, p.Count, MaxCount)
	}
	for _, v := range []struct {
		name     string
		value    float32
		positive bool
	}{
		{"speed", p.Speed, true},
		{"speedJitter", p.SpeedJitter, true},
		{"angle", p.Angle, false},
		{"spread", p.Spread, true},
		{"angleJitter", p.AngleJitter, true},
		{"spin", p.Spin, false},
		{"accel", p.Accel, false},
		{"turn", p.Turn, false},
		{"radius", p.Radius, true},
	} {
		if !finite(v.value) {
			return fmt.Errorf("pattern %q: %s %v is not finite", p.Name, v.name, v.value)
		}
		if v.positive && v.value < 0 {
			return fmt.Errorf("pattern %q: %s %v is negative", p.Name, v.name, v.value)
		}
	}
	if p.Spread > 360 {
		return fmt.Errorf("pattern %q: spread %v is above 360", p.Name, p.Spread)
	}
	if p.Kind != Aimed && p.Spread != 0 {
		return fmt.Errorf("pattern %q: spread applies to aimed patterns only", p.Name)
	}
	if p.Kind == Aimed && p.Count > 1 && p.Spread == 0 {
		return fmt.Errorf("pattern %q: an aimed pattern of %d bullets needs a spread above 0", p.Name, p.Count)
	}
	if p.Kind == Spiral && p.Spin == 0 {
		return fmt.Errorf("pattern %q: a spiral needs a spin other than 0", p.Name)
	}
	if p.Shots < 0 {
		return fmt.Errorf("pattern %q: shots %d is negative", p.Name, p.Shots)
	}
	if p.Delay < 0 || p.Interval < 0 || p.Lifetime < 0 {
		return fmt.Errorf("pattern %q: delay, interval and lifetime must not be negative", p.Name)
	}
	if p.Shots != 1 && p.Interval == 0 {
		return fmt.Errorf("pattern %q: %s shots need an interval above 0", p.Name, shotsName(p.Shots))
	}
	return nil
}

func shotsName(n int) string {
	if n == 0 {
		return "unlimited"
	}
	return fmt.Sprint(n)
}

func finite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
