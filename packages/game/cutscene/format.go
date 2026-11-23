package cutscene

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the version of the cutscene file format this package reads and writes.
const CurrentVersion = 1

// Fit says how a cutscene's picture fits the screen.
type Fit string

const (
	// FitContain shows the whole picture, with bars where its shape differs from the screen's. It is the default.
	FitContain Fit = "contain"
	// FitCover fills the screen, cropping what overflows.
	FitCover Fit = "cover"
	// FitStretch fills the screen, distorting the picture.
	FitStretch Fit = "stretch"
)

// resizeMode is f as core.Video takes it.
func (f Fit) resizeMode() core.ResizeMode {
	switch f {
	case FitCover:
		return core.Cover
	case FitStretch:
		return core.Stretch
	default:
		return core.Contain
	}
}

// Cutscene is one video cutscene, and the document a cutscene file holds:
//
//	version: 1
//	source: asset:cutscenes/intro.mp4
//	group: cutscenes
//	skippable: true
//	skipAfter: 1.5s
//	fit: cover
//
// Parse reads such a file and Encode writes one, keys in the order of the fields below and zero values left out, so a file Encode wrote reads back and writes again unchanged.
type Cutscene struct {
	// Version is the file format's version, which Parse and Encode require to be CurrentVersion. Player.Play ignores it, so a cutscene made in code may leave it 0.
	Version int `yaml:"version"`
	// Source is the video: an "asset:" path in the app's resources, a file path or a URL, as core.Video takes.
	Source string `yaml:"source"`
	// Group is the mixer group whose volume, times the master volume, the cutscene plays at; empty for the master volume alone.
	Group string `yaml:"group,omitempty"`
	// Skippable lets the player skip the cutscene by a press, a skip key or back.
	Skippable bool `yaml:"skippable,omitempty"`
	// SkipAfter is how far into the video a skippable cutscene must play before it can be skipped, so a press meant for the game before it does not skip it at once; 0 for from the start.
	SkipAfter Duration `yaml:"skipAfter,omitempty"`
	// Fit is how the picture fits the screen, FitContain when empty.
	Fit Fit `yaml:"fit,omitempty"`
	// Transparent leaves the screen behind the picture clear rather than black, so the game shows in the bars and through a video with an alpha channel, where the platform keeps it (core.Video).
	Transparent bool `yaml:"transparent,omitempty"`
}

// Duration is a time.Duration written in a cutscene file as Go writes durations, such as "1.5s" or "250ms".
type Duration time.Duration

// MarshalYAML writes d as time.Duration.String does.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// UnmarshalYAML reads a duration time.ParseDuration takes, refusing any other value.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return fmt.Errorf("line %d: a duration is a string such as \"1.5s\", not %q", n.Line, n.Value)
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

// Parse reads a cutscene file and checks it as Validate does. It refuses unknown keys, more than one document and a version other than CurrentVersion.
func Parse(data []byte) (Cutscene, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Cutscene{}, errors.New("cutscene: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Cutscene
	if err := dec.Decode(&c); err != nil {
		return Cutscene{}, fmt.Errorf("cutscene: decode: %w", err)
	}
	if err := checkVersionTag(data); err != nil {
		return Cutscene{}, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Cutscene{}, errors.New("cutscene: more than one YAML document")
		}
		return Cutscene{}, fmt.Errorf("cutscene: decode: %w", err)
	}
	if err := Validate(c); err != nil {
		return Cutscene{}, err
	}
	return c, nil
}

// checkVersionTag refuses a version that is not written as an integer, which the decoder would otherwise truncate into one, reading "1.5" as 1. data has decoded into a Cutscene already, so it is one mapping document.
func checkVersionTag(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("cutscene: decode: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, v := m.Content[i], m.Content[i+1]; k.Value == "version" && (v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int") {
			return fmt.Errorf("cutscene: line %d: version is an integer, not %q", v.Line, v.Value)
		}
	}
	return nil
}

// Encode checks c as Validate does and writes it as a cutscene file, with 2-space indents and its keys in a fixed order.
func Encode(c Cutscene) ([]byte, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("cutscene: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("cutscene: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks c as a cutscene file: its version is CurrentVersion, and it is a cutscene Player.Play plays.
func Validate(c Cutscene) error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("cutscene: unsupported version %d (want %d)", c.Version, CurrentVersion)
	}
	return check(c)
}

// check checks what Play needs of c: a source, a known fit and a skip delay that is not negative.
func check(c Cutscene) error {
	if strings.TrimSpace(c.Source) == "" {
		return errors.New("cutscene: no source")
	}
	switch c.Fit {
	case "", FitContain, FitCover, FitStretch:
	default:
		return fmt.Errorf("cutscene: unknown fit %q (want %q, %q or %q)", c.Fit, FitContain, FitCover, FitStretch)
	}
	if c.SkipAfter < 0 {
		return fmt.Errorf("cutscene: negative skipAfter %v", time.Duration(c.SkipAfter))
	}
	return nil
}
