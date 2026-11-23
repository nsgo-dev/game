package fog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Version is the snapshot schema version MarshalJSON writes and the newest UnmarshalJSON reads.
const Version = 1

var (
	// ErrInvalidSnapshot reports a fog snapshot that is malformed, has unknown fields, or disagrees with its own grid size.
	ErrInvalidSnapshot = errors.New("fog: invalid snapshot")
	// ErrFutureVersion reports a fog snapshot written by a newer schema version than this package reads.
	ErrFutureVersion = errors.New("fog: snapshot version newer than supported")
)

const (
	cellOff = '.'
	cellOn  = '#'
)

// snapshot is the JSON text form of a Fog. Grids are rows of '.' and '#' so a diff of two saves shows where a team explored.
type snapshot struct {
	Version    int          `json:"version"`
	Width      int          `json:"width"`
	Height     int          `json:"height"`
	TileWidth  float32      `json:"tileWidth"`
	TileHeight float32      `json:"tileHeight"`
	Opaque     []string     `json:"opaque,omitempty"`
	NextSource SourceID     `json:"nextSource"`
	Sources    []sourceJSON `json:"sources,omitempty"`
	Teams      []teamJSON   `json:"teams,omitempty"`
}

type sourceJSON struct {
	ID     SourceID `json:"id"`
	Team   Team     `json:"team"`
	Col    int      `json:"col"`
	Row    int      `json:"row"`
	Radius int      `json:"radius"`
}

type teamJSON struct {
	Team     Team     `json:"team"`
	Explored []string `json:"explored"`
}

// MarshalJSON writes the fog as versioned JSON text: the grid size, tile size, opaque cells, sources and each team's explored cells, in a fixed order with sources by ID and teams in increasing order, so the same fog always writes the same bytes.
//
// Visible cells are not written; UnmarshalJSON recomputes them from the sources and opaque cells, so a Fog passed to save.Store.Save and back through save.Store.Load sees and has explored exactly what it did.
func (f *Fog) MarshalJSON() ([]byte, error) {
	s := snapshot{
		Version:    Version,
		Width:      f.width,
		Height:     f.height,
		TileWidth:  f.TileWidth,
		TileHeight: f.TileHeight,
		NextSource: f.nextID,
	}
	if f.opaqueCount > 0 {
		s.Opaque = f.rows(f.opaque)
	}
	for _, id := range f.Sources() {
		src := f.sources[id]
		s.Sources = append(s.Sources, sourceJSON{ID: id, Team: src.Team, Col: src.Col, Row: src.Row, Radius: src.Radius})
	}
	for _, team := range f.Teams() {
		s.Teams = append(s.Teams, teamJSON{Team: team, Explored: f.rows(f.teams[team].explored)})
	}
	return json.Marshal(s)
}

// rows renders a bitset as one string per grid row.
func (f *Fog) rows(set []uint64) []string {
	out := make([]string, f.height)
	var b strings.Builder
	for row := range f.height {
		b.Reset()
		b.Grow(f.width)
		for col := range f.width {
			if bit(set, f.index(col, row)) {
				b.WriteByte(cellOn)
			} else {
				b.WriteByte(cellOff)
			}
		}
		out[row] = b.String()
	}
	return out
}

// parseRows reads rows written by rows into set, returning how many cells were set.
func (f *Fog) parseRows(rows []string, set []uint64, what string) (int, error) {
	if len(rows) != f.height {
		return 0, fmt.Errorf("%w: %s has %d rows, want %d", ErrInvalidSnapshot, what, len(rows), f.height)
	}
	n := 0
	for row, s := range rows {
		if len(s) != f.width {
			return 0, fmt.Errorf("%w: %s row %d has %d cells, want %d", ErrInvalidSnapshot, what, row, len(s), f.width)
		}
		for col := range len(s) {
			switch s[col] {
			case cellOn:
				setBit(set, f.index(col, row))
				n++
			case cellOff:
			default:
				return 0, fmt.Errorf("%w: %s row %d has %q, want %q or %q", ErrInvalidSnapshot, what, row, s[col], cellOff, cellOn)
			}
		}
	}
	return n, nil
}

// UnmarshalJSON replaces the fog with one read from JSON text written by MarshalJSON, and recomputes every team's visible cells from the sources.
//
// It refuses unknown fields, a missing or newer version (ErrFutureVersion), a grid larger than MaxCells, rows that disagree with the grid size, sources or teams out of order or repeated, a source ID above nextSource, and a source whose team has no explored grid. On error the fog is unchanged.
func (f *Fog) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s snapshot
	if err := dec.Decode(&s); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing data after snapshot", ErrInvalidSnapshot)
	}
	switch {
	case s.Version < 1:
		return fmt.Errorf("%w: missing version", ErrInvalidSnapshot)
	case s.Version > Version:
		return fmt.Errorf("%w: version %d, newest is %d", ErrFutureVersion, s.Version, Version)
	}
	if s.Width < 0 || s.Height < 0 || (s.Width > 0 && s.Height > MaxCells/s.Width) {
		return fmt.Errorf("%w: grid size %dx%d", ErrInvalidSnapshot, s.Width, s.Height)
	}
	n := New(s.Width, s.Height, s.TileWidth, s.TileHeight)
	if s.Opaque != nil {
		count, err := n.parseRows(s.Opaque, n.opaque, "opaque")
		if err != nil {
			return err
		}
		n.opaqueCount = count
	}
	for i, t := range s.Teams {
		if i > 0 && t.Team <= s.Teams[i-1].Team {
			return fmt.Errorf("%w: team %d out of order", ErrInvalidSnapshot, t.Team)
		}
		l := n.layer(t.Team)
		if _, err := n.parseRows(t.Explored, l.explored, fmt.Sprintf("team %d explored", t.Team)); err != nil {
			return err
		}
	}
	n.nextID = s.NextSource
	for i, src := range s.Sources {
		switch {
		case src.ID == 0 || src.ID > s.NextSource:
			return fmt.Errorf("%w: source ID %d outside 1..%d", ErrInvalidSnapshot, src.ID, s.NextSource)
		case i > 0 && src.ID <= s.Sources[i-1].ID:
			return fmt.Errorf("%w: source %d out of order", ErrInvalidSnapshot, src.ID)
		case n.teams[src.Team] == nil:
			return fmt.Errorf("%w: source %d has team %d with no explored grid", ErrInvalidSnapshot, src.ID, src.Team)
		}
		n.sources[src.ID] = Source{Team: src.Team, Col: src.Col, Row: src.Row, Radius: src.Radius}
		n.teams[src.Team].dirty = true
	}
	n.Update()
	// Revisions keep growing across a load, so a cache keyed on one never mistakes the loaded fog for the one it replaced.
	for team, l := range n.teams {
		if old := f.teams[team]; old != nil {
			l.revision += old.revision + 1
		}
	}
	*f = *n
	return nil
}
