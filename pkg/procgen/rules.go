package procgen

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"go.yaml.in/yaml/v3"
)

// RulesVersion is the WFC rule file schema version this package reads and writes.
const RulesVersion = 1

// Rules is a wave function collapse rule set: the tiles a level may use and which tiles may sit next to each other. It is written by hand as a YAML file, for example:
//
//	version: 1
//	tiles:
//	  - name: floor
//	    weight: 4
//	    walkable: true
//	  - name: wall
//	    weight: 1
//	neighbors:
//	  - {left: floor, right: floor}
//	  - {left: floor, right: wall}
//	  - {up: floor, down: floor}
//
// Each neighbor entry allows one ordered pair: Left directly left of Right, or Up directly above Down. A pair that is not listed is forbidden, so a symmetric rule lists both orders.
type Rules struct {
	// Version is the schema version; it must equal RulesVersion.
	Version int `yaml:"version"`
	// Tiles lists the tiles. WFC writes a tile's index in this list into each Grid cell.
	Tiles []TileRule `yaml:"tiles"`
	// Neighbors lists the allowed adjacent pairs.
	Neighbors []Neighbor `yaml:"neighbors"`
}

// TileRule is one tile of a Rules set.
type TileRule struct {
	// Name identifies the tile in Neighbors; it must be non-empty and unique.
	Name string `yaml:"name"`
	// Weight is the tile's relative frequency when a cell collapses; it must be positive and finite.
	Weight float64 `yaml:"weight"`
	// Walkable marks the tile as passable for the connectivity check.
	Walkable bool `yaml:"walkable,omitempty"`
}

// Neighbor allows one ordered pair of adjacent tiles: either Left and Right for a horizontal pair or Up and Down for a vertical pair, never both.
type Neighbor struct {
	// Left is the tile on the left of a horizontal pair.
	Left string `yaml:"left,omitempty"`
	// Right is the tile on the right of a horizontal pair.
	Right string `yaml:"right,omitempty"`
	// Up is the tile on top of a vertical pair.
	Up string `yaml:"up,omitempty"`
	// Down is the tile below in a vertical pair.
	Down string `yaml:"down,omitempty"`
}

// ParseRules decodes and validates a YAML rule set. Unknown fields and trailing YAML documents are rejected.
func ParseRules(data []byte) (*Rules, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("procgen: empty rules document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var r Rules
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("procgen: decode rules: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("procgen: unexpected trailing YAML document in rules")
		}
		return nil, fmt.Errorf("procgen: decode rules: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// EncodeRules validates r and writes it as 2-space YAML with fields in a fixed order and tiles and neighbors in their given order, so ParseRules followed by EncodeRules reproduces a file EncodeRules wrote.
func EncodeRules(r *Rules) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(r); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("procgen: encode rules: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("procgen: encode rules: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks r against the version 1 schema: the version, at least one tile, unique non-empty names, positive finite weights whose sum is finite, and neighbor entries that name known tiles as exactly one horizontal or vertical pair, each listed once.
func (r *Rules) Validate() error {
	if r.Version != RulesVersion {
		return fmt.Errorf("procgen: unsupported rules version %d (want %d)", r.Version, RulesVersion)
	}
	if len(r.Tiles) == 0 {
		return errors.New("procgen: rules have no tiles")
	}
	index := make(map[string]int, len(r.Tiles))
	total := 0.0
	for i, t := range r.Tiles {
		if t.Name == "" {
			return fmt.Errorf("procgen: tile %d has no name", i)
		}
		if _, dup := index[t.Name]; dup {
			return fmt.Errorf("procgen: duplicate tile %q", t.Name)
		}
		if !(t.Weight > 0) || math.IsInf(t.Weight, 0) {
			return fmt.Errorf("procgen: tile %q weight %v must be positive and finite", t.Name, t.Weight)
		}
		index[t.Name] = i
		total += t.Weight
	}
	// WFC picks tiles with random.Stream.WeightedIndex, which panics when the weights overflow float64.
	if math.IsInf(total, 0) {
		return errors.New("procgen: tile weights sum to more than the largest float64")
	}
	seen := make(map[Neighbor]bool, len(r.Neighbors))
	for i, n := range r.Neighbors {
		horizontal := n.Left != "" || n.Right != ""
		vertical := n.Up != "" || n.Down != ""
		var a, b string
		switch {
		case horizontal && !vertical:
			a, b = n.Left, n.Right
		case vertical && !horizontal:
			a, b = n.Up, n.Down
		default:
			return fmt.Errorf("procgen: neighbor %d must set either left and right or up and down", i)
		}
		for _, name := range [2]string{a, b} {
			if _, ok := index[name]; !ok {
				return fmt.Errorf("procgen: neighbor %d names unknown tile %q", i, name)
			}
		}
		if seen[n] {
			return fmt.Errorf("procgen: neighbor %d repeats an earlier pair", i)
		}
		seen[n] = true
	}
	return nil
}

// Walkable reports whether the tile at index v is walkable. It is the passable function for grids made by WFC with r; indexes outside r are not walkable.
func (r *Rules) Walkable(v uint32) bool {
	return int64(v) < int64(len(r.Tiles)) && r.Tiles[v].Walkable
}

// Index returns the index of the tile named name, or -1 when r has no such tile.
func (r *Rules) Index(name string) int {
	for i, t := range r.Tiles {
		if t.Name == name {
			return i
		}
	}
	return -1
}
