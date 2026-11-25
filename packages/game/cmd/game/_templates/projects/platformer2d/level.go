package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// levelVersion is the level file schema version this game reads and writes.
const levelVersion = 1

// The glyphs of a level's tiles, one character per tile.
const (
	glyphEmpty      = '.'
	glyphGround     = '#'
	glyphLedge      = '='
	glyphSpikes     = '^'
	glyphSpark      = 'o'
	glyphCheckpoint = 'C'
	glyphGoal       = 'G'
	glyphStart      = 'P'
)

// Level is a level file such as data/level.yaml: a grid of tiles drawn as text, one character per tile and one line per row, with the tile size and the lives a run starts with.
//
//	.  empty           #  ground (solid)    =  ledge (one-way, jump up through it)
//	^  spikes (hurt)   o  spark (collect)   C  checkpoint lamp
//	G  goal beacon     P  where the player starts
//
// Every row has the same width, and a level has exactly one P and one G. Parse it with parseLevel and write it with encodeLevel, which gives back the same bytes for a file it wrote.
type Level struct {
	// Version is the schema version; it must be 1.
	Version int `yaml:"version"`
	// Name is the level's display name.
	Name string `yaml:"name"`
	// TileSize is the width and height of a tile in world points, from 4 to 64.
	TileSize int `yaml:"tileSize"`
	// Lives is how many times the player can be hurt before the run is lost, from 1 to 9.
	Lives int `yaml:"lives"`
	// Tiles holds the rows of the grid, top first, each ending in a newline.
	Tiles string `yaml:"tiles"`
}

// parseLevel decodes and validates a level file, refusing unknown fields and trailing documents.
func parseLevel(data []byte) (*Level, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("level: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var l Level
	if err := dec.Decode(&l); err != nil {
		return nil, fmt.Errorf("level: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("level: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("level: decode: %w", err)
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// encodeLevel validates l and writes it as 2-space YAML with its fields in a fixed order and the tiles as a literal block, so parseLevel reads back the same Level and encoding that again gives the same bytes.
func encodeLevel(l *Level) ([]byte, error) {
	if err := l.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(l); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("level: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("level: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// validate reports the first way l breaks the level schema.
func (l *Level) validate() error {
	if l.Version != levelVersion {
		return fmt.Errorf("level: unsupported version %d (want %d)", l.Version, levelVersion)
	}
	if strings.TrimSpace(l.Name) == "" {
		return errors.New("level: name is empty")
	}
	if l.TileSize < 4 || l.TileSize > 64 {
		return fmt.Errorf("level: tileSize %d is outside 4 to 64", l.TileSize)
	}
	if l.Lives < 1 || l.Lives > 9 {
		return fmt.Errorf("level: lives %d is outside 1 to 9", l.Lives)
	}
	if !strings.HasSuffix(l.Tiles, "\n") {
		return errors.New("level: tiles must end with a newline")
	}
	rows := l.rows()
	if len(rows) < 2 {
		return fmt.Errorf("level: tiles have %d rows, want at least 2", len(rows))
	}
	width := len(rows[0])
	if width < 2 {
		return fmt.Errorf("level: tiles are %d wide, want at least 2", width)
	}
	starts, goals := 0, 0
	for r, row := range rows {
		if len(row) != width {
			return fmt.Errorf("level: row %d is %d tiles wide, want %d like row 0", r, len(row), width)
		}
		for c := 0; c < len(row); c++ {
			switch row[c] {
			case glyphEmpty, glyphGround, glyphLedge, glyphSpikes, glyphSpark, glyphCheckpoint:
			case glyphStart:
				starts++
			case glyphGoal:
				goals++
			default:
				return fmt.Errorf("level: row %d column %d: unknown tile %q", r, c, row[c])
			}
		}
	}
	if starts != 1 {
		return fmt.Errorf("level: tiles have %d starts (P), want 1", starts)
	}
	if goals != 1 {
		return fmt.Errorf("level: tiles have %d goals (G), want 1", goals)
	}
	return nil
}

// rows returns the tile rows, top first.
func (l *Level) rows() []string {
	return strings.Split(strings.TrimSuffix(l.Tiles, "\n"), "\n")
}
