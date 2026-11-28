package input

import (
	"bytes"
	"fmt"
	"go.yaml.in/yaml/v3"
	"io"
)

// GlyphSet is a strict versioned YAML map from device family and input token to prompt glyphs.
type GlyphSet struct {
	Version int                          `yaml:"version"`
	Devices map[string]map[string]Prompt `yaml:"devices"`
}

// ParseGlyphSet decodes a version-1 glyph YAML document.
func ParseGlyphSet(data []byte) (*GlyphSet, error) {
	var g GlyphSet
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&g); err != nil {
		return nil, fmt.Errorf("input: decode glyph set: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return nil, fmt.Errorf("input: glyph set: trailing document")
	}
	if err := ValidateGlyphSet(&g); err != nil {
		return nil, err
	}
	return &g, nil
}

// ValidateGlyphSet checks device names, tokens and prompt fields.
func ValidateGlyphSet(g *GlyphSet) error {
	if g == nil || g.Version != 1 {
		return fmt.Errorf("input: glyph set version must be 1")
	}
	for d, m := range g.Devices {
		if d == "" {
			return fmt.Errorf("input: glyph device name is required")
		}
		for k, p := range m {
			if k == "" || p.Label == "" && p.Glyph == "" && p.Icon == "" {
				return fmt.Errorf("input: glyph %s/%s is empty", d, k)
			}
		}
	}
	return nil
}

// EncodeGlyphSet writes deterministic two-space YAML.
func EncodeGlyphSet(g *GlyphSet) ([]byte, error) {
	if err := ValidateGlyphSet(g); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(g); err != nil {
		return nil, err
	}
	if err := e.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

var _ = io.EOF
