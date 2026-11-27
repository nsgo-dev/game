package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// LevelsVersion is the version of the levels file format this game reads and writes.
const LevelsVersion = 1

// Tile characters of a level's rows.
const (
	charWall  = '#'
	charFloor = '.'
	charStart = 'S'
	charLamp  = 'o'
	charVoid  = 'x'
)

// levelsYAML is the game's levels, in the order the level select lists them and each unlocks the next.
//
//go:embed levels.yaml
var levelsYAML []byte

// Levels are the game's parsed levels.
var Levels = MustParseLevels(levelsYAML)

// LevelFile is levels.yaml: a version and the levels in play order.
type LevelFile struct {
	// Version is the format version, LevelsVersion.
	Version int `yaml:"version"`
	// Levels are the levels in play order; clearing one unlocks the next.
	Levels []Level `yaml:"levels"`
}

// Level is one puzzle: a grid of tiles, the spark's start, the lamps to light and the moves allowed.
type Level struct {
	// ID names the level in the save file, so renaming or reordering levels keeps the stars earned; lower-case letters, digits and -.
	ID string `yaml:"id"`
	// Name is the title the game shows.
	Name string `yaml:"name"`
	// Moves is how many moves the player has to light every lamp.
	Moves int `yaml:"moves"`
	// Stars are the move counts that earn three and two stars; any win earns one.
	Stars Stars `yaml:"stars"`
	// Rows are the grid, top row first, one character per tile: # wall, . floor, S the spark's start on floor, o a lamp, x a void the spark falls into. Every row has the same length; outside the grid counts as wall.
	Rows []string `yaml:"rows"`
}

// Stars are the most moves a win may take for each star rating.
type Stars struct {
	// Three is the most moves that earn three stars, the level's par.
	Three int `yaml:"three"`
	// Two is the most moves that earn two stars; at least Three and at most the level's Moves.
	Two int `yaml:"two"`
}

var levelIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ParseLevels decodes and validates a levels file, refusing unknown fields and any version but LevelsVersion.
func ParseLevels(data []byte) (*LevelFile, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f LevelFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("levels: the file is empty")
		}
		return nil, fmt.Errorf("levels: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("levels: the file holds more than one document")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// MustParseLevels is ParseLevels for the levels the game embeds, which its tests check, so it panics on an error.
func MustParseLevels(data []byte) *LevelFile {
	f, err := ParseLevels(data)
	if err != nil {
		panic(err)
	}
	return f
}

// EncodeLevels writes f in the canonical form levels.yaml is kept in: fields in declaration order, two-space indents, no comments. ParseLevels of the result gives f back.
func EncodeLevels(f *LevelFile) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Validate reports the first problem with f: a wrong version, no levels, or a level Level.Validate refuses, or two levels with one ID.
func (f *LevelFile) Validate() error {
	if f.Version != LevelsVersion {
		return fmt.Errorf("levels: version %d, want %d", f.Version, LevelsVersion)
	}
	if len(f.Levels) == 0 {
		return errors.New("levels: no levels")
	}
	seen := make(map[string]bool, len(f.Levels))
	for i := range f.Levels {
		l := &f.Levels[i]
		if err := l.Validate(); err != nil {
			return fmt.Errorf("levels: level %d: %w", i+1, err)
		}
		if seen[l.ID] {
			return fmt.Errorf("levels: level %d: id %q is used twice", i+1, l.ID)
		}
		seen[l.ID] = true
	}
	return nil
}

// Validate reports the first problem with l: a bad ID or name, star thresholds out of order, a grid that is ragged or holds an unknown tile, or a grid without exactly one start and at least one lamp.
func (l *Level) Validate() error {
	if !levelIDPattern.MatchString(l.ID) {
		return fmt.Errorf("id %q: use lower-case letters, digits and -, starting with a letter", l.ID)
	}
	if strings.TrimSpace(l.Name) == "" {
		return fmt.Errorf("%s: no name", l.ID)
	}
	if l.Stars.Three < 1 || l.Stars.Two < l.Stars.Three || l.Moves < l.Stars.Two {
		return fmt.Errorf("%s: want 1 <= stars.three (%d) <= stars.two (%d) <= moves (%d)", l.ID, l.Stars.Three, l.Stars.Two, l.Moves)
	}
	if len(l.Rows) == 0 {
		return fmt.Errorf("%s: no rows", l.ID)
	}
	width := len(l.Rows[0])
	if width == 0 {
		return fmt.Errorf("%s: row 1 is empty", l.ID)
	}
	starts, lamps := 0, 0
	for y, row := range l.Rows {
		if len(row) != width {
			return fmt.Errorf("%s: row %d is %d tiles wide, want %d", l.ID, y+1, len(row), width)
		}
		for x := range len(row) {
			switch row[x] {
			case charStart:
				starts++
			case charLamp:
				lamps++
			case charWall, charFloor, charVoid:
			default:
				return fmt.Errorf("%s: row %d, column %d: unknown tile %q", l.ID, y+1, x+1, row[x])
			}
		}
	}
	if starts != 1 {
		return fmt.Errorf("%s: %d starts (S), want 1", l.ID, starts)
	}
	if lamps == 0 {
		return fmt.Errorf("%s: no lamps (o)", l.ID)
	}
	return nil
}

// StarsFor returns the stars a win in moves earns: 3 at par or better, 2 within Stars.Two and 1 otherwise.
func (l *Level) StarsFor(moves int) int {
	switch {
	case moves <= l.Stars.Three:
		return 3
	case moves <= l.Stars.Two:
		return 2
	default:
		return 1
	}
}
