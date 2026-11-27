package rts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"go.yaml.in/yaml/v3"
)

// FormationsVersion is the formation file schema version this package reads and writes.
const FormationsVersion = 1

// Formations is a formation file: the named formations a game offers its players, kept in YAML so a designer can edit them and review the diff.
//
//	version: 1
//	formations:
//	  - name: line
//	    shape: line
//	    spacing: 1.5
//	  - name: block
//	    shape: box
//	    spacing: 1.2
//	    width: 8
type Formations struct {
	// Version is the schema version; it must be FormationsVersion.
	Version int `yaml:"version"`
	// Formations are the formations in the order the game lists them; names are unique.
	Formations []Formation `yaml:"formations"`
}

// Get returns the formation named name.
func (fs *Formations) Get(name string) (Formation, bool) {
	if fs == nil {
		return Formation{}, false
	}
	for _, f := range fs.Formations {
		if f.Name == name {
			return f, true
		}
	}
	return Formation{}, false
}

// ParseFormations decodes and validates a formation file. Unknown fields, a second YAML document and any value Validate refuses are errors.
func ParseFormations(data []byte) (*Formations, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("rts: empty formation file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var fs Formations
	if err := dec.Decode(&fs); err != nil {
		return nil, fmt.Errorf("rts: decode formations: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("rts: unexpected trailing YAML document in formation file")
		}
		return nil, fmt.Errorf("rts: decode formations: %w", err)
	}
	if err := fs.Validate(); err != nil {
		return nil, err
	}
	return &fs, nil
}

// Encode validates fs and writes it as YAML with two-space indentation and fields in a fixed order, so encoding a parsed file gives back the same bytes for files written in that form.
func (fs *Formations) Encode() ([]byte, error) {
	if err := fs.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fs); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("rts: encode formations: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("rts: encode formations: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate reports the first problem with fs: a version other than FormationsVersion, a formation with an empty or repeated name, an unknown shape, a spacing that is not a positive finite number, or a negative width.
func (fs *Formations) Validate() error {
	if fs == nil {
		return errors.New("rts: nil formation file")
	}
	if fs.Version != FormationsVersion {
		return fmt.Errorf("rts: unsupported formation file version %d (want %d)", fs.Version, FormationsVersion)
	}
	seen := make(map[string]bool, len(fs.Formations))
	for i, f := range fs.Formations {
		if f.Name == "" {
			return fmt.Errorf("rts: formation %d has no name", i)
		}
		if seen[f.Name] {
			return fmt.Errorf("rts: formation %q is defined twice", f.Name)
		}
		seen[f.Name] = true
		if err := f.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate reports whether f has a known shape, a positive finite spacing and a non-negative width.
func (f Formation) Validate() error {
	switch f.Shape {
	case ShapeLine, ShapeColumn, ShapeBox, ShapeWedge:
	default:
		return fmt.Errorf("rts: formation %q has unknown shape %q (want line, column, box or wedge)", f.Name, f.Shape)
	}
	if !(f.Spacing > 0) || math.IsInf(float64(f.Spacing), 0) {
		return fmt.Errorf("rts: formation %q needs a positive finite spacing, got %v", f.Name, f.Spacing)
	}
	if f.Width < 0 {
		return fmt.Errorf("rts: formation %q has negative width %d", f.Name, f.Width)
	}
	return nil
}
