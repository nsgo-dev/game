package fog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// FormatVersion is the snapshot schema version MarshalJSON writes and the newest UnmarshalJSON reads.
const FormatVersion = 1

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

// snapshot is the JSON text form of a Fog. Grids are rows of '.' and '#', one string per grid row, so a person reading a save sees each team's explored map.
type snapshot struct {
	Version       int          `json:"version"`
	Width         int          `json:"width"`
	Height        int          `json:"height"`
	TileWidth     float32      `json:"tileWidth"`
	TileHeight    float32      `json:"tileHeight"`
	OriginX       float32      `json:"originX,omitempty"`
	OriginY       float32      `json:"originY,omitempty"`
	Orientation   string       `json:"orientation,omitempty"`
	StaggerAxis   string       `json:"staggerAxis,omitempty"`
	StaggerIndex  string       `json:"staggerIndex,omitempty"`
	HexSideLength float32      `json:"hexSideLength,omitempty"`
	Opaque        []string     `json:"opaque,omitempty"`
	NextSource    SourceID     `json:"nextSource"`
	Sources       []sourceJSON `json:"sources,omitempty"`
	Teams         []teamJSON   `json:"teams,omitempty"`
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
//
// MarshalJSON has a value receiver so a Fog held by value in a struct that is saved by value still writes its snapshot rather than its exported fields alone.
func (f Fog) MarshalJSON() ([]byte, error) {
	s := snapshot{
		Version:    FormatVersion,
		Width:      f.width,
		Height:     f.height,
		TileWidth:  f.TileWidth,
		TileHeight: f.TileHeight,
		OriginX:    float32(f.Origin.X),
		OriginY:    float32(f.Origin.Y),
		NextSource: f.nextID,
	}
	switch f.Orientation {
	case tilemap.OrientationIsometric:
		s.Orientation = "isometric"
	case tilemap.OrientationStaggered:
		s.Orientation = "staggered"
		s.StaggerAxis = staggerAxisString(f.StaggerAxis)
		s.StaggerIndex = staggerIndexString(f.StaggerIndex)
	case tilemap.OrientationHexagonal:
		s.Orientation = "hexagonal"
		s.StaggerAxis = staggerAxisString(f.StaggerAxis)
		s.StaggerIndex = staggerIndexString(f.StaggerIndex)
		if f.HexSideLength > 0 {
			s.HexSideLength = f.HexSideLength
		}
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
// It refuses unknown fields, a missing or newer version (ErrFutureVersion), a grid larger than MaxCells or with exactly one zero size, rows that disagree with the grid size, sources or teams out of order or repeated, a source ID above nextSource, and a source whose team has no explored grid. On error the fog is unchanged.
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
	case s.Version > FormatVersion:
		return fmt.Errorf("%w: version %d, newest is %d", ErrFutureVersion, s.Version, FormatVersion)
	}
	if !validSize(s.Width, s.Height) {
		return fmt.Errorf("%w: grid size %dx%d", ErrInvalidSnapshot, s.Width, s.Height)
	}
	orient, err := parseOrientation(s.Orientation)
	if err != nil {
		return err
	}
	axis, err := parseStaggerAxis(s.StaggerAxis)
	if err != nil {
		return err
	}
	index, err := parseStaggerIndex(s.StaggerIndex)
	if err != nil {
		return err
	}
	if s.HexSideLength < 0 || math.IsNaN(float64(s.HexSideLength)) || math.IsInf(float64(s.HexSideLength), 0) {
		return fmt.Errorf("%w: invalid hexSideLength %v", ErrInvalidSnapshot, s.HexSideLength)
	}
	if math.IsNaN(float64(s.OriginX)) || math.IsInf(float64(s.OriginX), 0) ||
		math.IsNaN(float64(s.OriginY)) || math.IsInf(float64(s.OriginY), 0) {
		return fmt.Errorf("%w: invalid origin (%v, %v)", ErrInvalidSnapshot, s.OriginX, s.OriginY)
	}
	n := New(s.Width, s.Height, s.TileWidth, s.TileHeight)
	n.Origin = core.Point{X: core.Points(s.OriginX), Y: core.Points(s.OriginY)}
	n.Orientation = orient
	n.StaggerAxis = axis
	n.StaggerIndex = index
	n.HexSideLength = s.HexSideLength
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
	// Every loaded team gets a fresh revision, including teams with no sources that Update skips, and without reading the receiver's old state: save.Store.Load decodes into a zero Fog and copies it over the caller's.
	for _, l := range n.teams {
		l.revision = nextRevision()
	}
	*f = *n
	return nil
}

func staggerAxisString(a tilemap.StaggerAxis) string {
	if a == tilemap.StaggerX {
		return "x"
	}
	return "y"
}

func staggerIndexString(i tilemap.StaggerIndex) string {
	if i == tilemap.StaggerEven {
		return "even"
	}
	return "odd"
}

func parseOrientation(s string) (tilemap.Orientation, error) {
	switch s {
	case "", "orthogonal":
		return tilemap.OrientationOrthogonal, nil
	case "isometric":
		return tilemap.OrientationIsometric, nil
	case "staggered":
		return tilemap.OrientationStaggered, nil
	case "hexagonal":
		return tilemap.OrientationHexagonal, nil
	default:
		return 0, fmt.Errorf("%w: unknown orientation %q", ErrInvalidSnapshot, s)
	}
}

func parseStaggerAxis(s string) (tilemap.StaggerAxis, error) {
	switch s {
	case "", "y":
		return tilemap.StaggerY, nil
	case "x":
		return tilemap.StaggerX, nil
	default:
		return 0, fmt.Errorf("%w: unknown staggerAxis %q", ErrInvalidSnapshot, s)
	}
}

func parseStaggerIndex(s string) (tilemap.StaggerIndex, error) {
	switch s {
	case "", "odd":
		return tilemap.StaggerOdd, nil
	case "even":
		return tilemap.StaggerEven, nil
	default:
		return 0, fmt.Errorf("%w: unknown staggerIndex %q", ErrInvalidSnapshot, s)
	}
}
