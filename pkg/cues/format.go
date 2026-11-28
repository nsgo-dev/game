// Package cues maps named gameplay cue events to presentation handlers.
//
// The package contains only the deterministic data and event boundary. A game
// supplies Hooks to connect handlers to particles, sound, camera and sprites.
// This keeps cue data usable by headless simulations and dedicated servers.
package cues

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const CurrentVersion = 1
const maxCues = 4096

// Duration is a Go duration string in YAML, such as 250ms or 1.5s.
type Duration time.Duration

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return fmt.Errorf("duration must be a string")
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return err
	}
	if v < 0 {
		return errors.New("duration must not be negative")
	}
	*d = Duration(v)
	return nil
}

// Set is a versioned cue mapping file.
type Set struct {
	Version int   `yaml:"version"`
	Cues    []Cue `yaml:"cues"`
}

// Cue maps one gameplay tag to presentation handlers. Once and WhileActive
// may both be true, allowing an impact to fire once and retain a loop.
type Cue struct {
	Tag         string     `yaml:"tag"`
	Once        bool       `yaml:"once,omitempty"`
	WhileActive bool       `yaml:"whileActive,omitempty"`
	Particles   *Particles `yaml:"particles,omitempty"`
	Sound       *Sound     `yaml:"sound,omitempty"`
	Shake       *Shake     `yaml:"shake,omitempty"`
	Flash       *Flash     `yaml:"flash,omitempty"`
	Animation   string     `yaml:"animation,omitempty"`
}

type Particles struct {
	File   string  `yaml:"file"`
	Attach string  `yaml:"attach,omitempty"`
	Offset *Offset `yaml:"offset,omitempty"`
}
type Sound struct {
	File string `yaml:"file"`
	Loop bool   `yaml:"loop,omitempty"`
}
type Shake struct {
	Amplitude float64  `yaml:"amplitude"`
	Duration  Duration `yaml:"duration"`
}
type Flash struct {
	Color    string   `yaml:"color"`
	Duration Duration `yaml:"duration"`
}
type Offset struct {
	X, Y float64 `yaml:"x,omitempty"`
	Z    float64 `yaml:"z,omitempty"`
}

var tagPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z][A-Za-z0-9_-]*)*$`)

// Parse decodes and validates one cue mapping file.
func Parse(data []byte) (*Set, error) {
	set, err := parse(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, "write cues as version: 1 YAML with unique cue tags")
	}
	return set, nil
}
func parse(data []byte) (*Set, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("cues: empty cue set")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var set Set
	if err := dec.Decode(&set); err != nil {
		return nil, fmt.Errorf("cues: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("cues: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("cues: decode: %w", err)
	}
	if err := Validate(&set); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&set)
	if set.Cues == nil {
		set.Cues = []Cue{}
	}
	return &set, nil
}

// Encode writes deterministic two-space YAML.
func Encode(set *Set) ([]byte, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(set)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("cues: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("cues: encode: %w", err)
	}
	return b.Bytes(), nil
}

// Validate checks cue names, uniqueness, handler fields and limits.
func Validate(set *Set) error {
	if set == nil {
		return errors.New("cues: nil set")
	}
	if set.Version != CurrentVersion {
		return fmt.Errorf("cues: unsupported version %d (want %d)", set.Version, CurrentVersion)
	}
	if len(set.Cues) > maxCues {
		return fmt.Errorf("cues: %d cues, more than %d", len(set.Cues), maxCues)
	}
	seen := map[string]bool{}
	for i := range set.Cues {
		c := &set.Cues[i]
		if !tagPattern.MatchString(c.Tag) {
			return fmt.Errorf("cues: cues[%d].tag %q is not a dotted tag", i, c.Tag)
		}
		if seen[c.Tag] {
			return fmt.Errorf("cues: duplicate cue tag %q", c.Tag)
		}
		seen[c.Tag] = true
		if !c.Once && !c.WhileActive {
			return fmt.Errorf("cues: cue %q must set once or whileActive", c.Tag)
		}
		if c.Particles != nil && strings.TrimSpace(c.Particles.File) == "" {
			return fmt.Errorf("cues: cue %q particles.file is empty", c.Tag)
		}
		if c.Sound != nil && strings.TrimSpace(c.Sound.File) == "" {
			return fmt.Errorf("cues: cue %q sound.file is empty", c.Tag)
		}
		if c.Shake != nil && c.Shake.Amplitude < 0 {
			return fmt.Errorf("cues: cue %q shake.amplitude is negative", c.Tag)
		}
		if c.Flash != nil && c.Flash.Color == "" {
			return fmt.Errorf("cues: cue %q flash.color is empty", c.Tag)
		}
	}
	return nil
}
