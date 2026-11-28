package input

import (
	"bytes"
	"fmt"
	"go.yaml.in/yaml/v3"
)

type GlyphSet struct {
	Version int                          `yaml:"version"`
	Devices map[string]map[string]Prompt `yaml:"devices"`
}

func ParseGlyphSet(data []byte) (GlyphSet, error) {
	var g GlyphSet
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&g); err != nil {
		return g, err
	}
	if g.Version != 1 || len(g.Devices) == 0 {
		return g, fmt.Errorf("input: glyph set requires version 1 and devices")
	}
	return g, nil
}
func EncodeGlyphSet(g GlyphSet) ([]byte, error) {
	if g.Version == 0 {
		g.Version = 1
	}
	if _, err := ParseGlyphSet(mustGlyphYAML(g)); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(g); err != nil {
		return nil, err
	}
	e.Close()
	return b.Bytes(), nil
}
func mustGlyphYAML(g GlyphSet) []byte { b, _ := yaml.Marshal(g); return b }
