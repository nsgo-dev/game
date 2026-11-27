package procgen

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"go.yaml.in/yaml/v3"
)

// RoomTemplatesVersion is the room template set schema version this package reads and writes.
const RoomTemplatesVersion = 1

// Side is one of the four cardinal edges of a room template.
type Side string

const (
	// SideNorth is the top edge (y = 0) of a room.
	SideNorth Side = "north"
	// SideSouth is the bottom edge (y = Height-1) of a room.
	SideSouth Side = "south"
	// SideEast is the right edge (x = Width-1) of a room.
	SideEast Side = "east"
	// SideWest is the left edge (x = 0) of a room.
	SideWest Side = "west"
)

var allSides = [4]Side{SideNorth, SideEast, SideSouth, SideWest}

// Valid reports whether s is one of SideNorth, SideSouth, SideEast or SideWest.
func (s Side) Valid() bool {
	switch s {
	case SideNorth, SideSouth, SideEast, SideWest:
		return true
	}
	return false
}

// Opposite returns the cardinal side facing s, or empty for an invalid side.
func (s Side) Opposite() Side {
	switch s {
	case SideNorth:
		return SideSouth
	case SideSouth:
		return SideNorth
	case SideEast:
		return SideWest
	case SideWest:
		return SideEast
	}
	return ""
}

// Delta returns the unit cell step pointing outward from s.
func (s Side) Delta() Point {
	switch s {
	case SideNorth:
		return Point{0, -1}
	case SideSouth:
		return Point{0, 1}
	case SideEast:
		return Point{1, 0}
	case SideWest:
		return Point{-1, 0}
	}
	return Point{}
}

// RotateCW returns s rotated clockwise by 90 degrees.
func (s Side) RotateCW() Side {
	switch s {
	case SideNorth:
		return SideEast
	case SideEast:
		return SideSouth
	case SideSouth:
		return SideWest
	case SideWest:
		return SideNorth
	}
	return ""
}

// DoorSocket is an opening on one side of a room template where another room can attach.
type DoorSocket struct {
	// Side is north, south, east or west.
	Side Side `yaml:"side"`
	// Offset is the 0-based cell position along Side from its left (north/south) or top (east/west) edge.
	Offset int `yaml:"offset"`
	// Width is the opening's span in cells along Side; 0 defaults to 1.
	Width int `yaml:"width,omitempty"`
	// Channel optionally restricts matching to sockets with the same Channel.
	Channel string `yaml:"channel,omitempty"`
}

// Span returns the socket's width in cells, treating 0 as 1.
func (d DoorSocket) Span() int {
	if d.Width <= 0 {
		return 1
	}
	return d.Width
}

// Matches reports whether d can connect to other on an opposing room edge.
func (d DoorSocket) Matches(other DoorSocket) bool {
	return d.Side.Opposite() == other.Side && d.Span() == other.Span() && d.Channel == other.Channel
}

// Cells returns the room-local cell coordinates covered by d on a w-by-h room.
func (d DoorSocket) Cells(w, h int) []Point {
	span := d.Span()
	out := make([]Point, span)
	switch d.Side {
	case SideNorth:
		for i := range span {
			out[i] = Point{X: d.Offset + i, Y: 0}
		}
	case SideSouth:
		for i := range span {
			out[i] = Point{X: d.Offset + i, Y: h - 1}
		}
	case SideWest:
		for i := range span {
			out[i] = Point{X: 0, Y: d.Offset + i}
		}
	case SideEast:
		for i := range span {
			out[i] = Point{X: w - 1, Y: d.Offset + i}
		}
	}
	return out
}

// RoomTemplates is a versioned YAML collection of hand-made room templates for dungeon generation.
type RoomTemplates struct {
	// Version is the schema version; it must equal RoomTemplatesVersion.
	Version int `yaml:"version"`
	// Rooms lists the room templates in deterministic order.
	Rooms []RoomTemplate `yaml:"rooms"`
}

// RoomTemplate describes one hand-made room: either an inline tile grid or a reference to an external .tmj or scene file, together with door sockets, tags and relative weight.
type RoomTemplate struct {
	// ID uniquely identifies the template within its RoomTemplates set.
	ID string `yaml:"id"`
	// Weight is the template's relative selection frequency; 0 defaults to 1. Negative, NaN and infinite values are refused.
	Weight float64 `yaml:"weight,omitempty"`
	// Tags classify the room for graph roles or game logic (such as "start", "goal", "key", "combat", "shop").
	Tags []string `yaml:"tags,omitempty"`
	// Width and Height are the room's dimensions in cells. Required when Source is set; optional when Tiles is set (and must match Tiles if provided).
	Width  int `yaml:"width,omitempty"`
	Height int `yaml:"height,omitempty"`
	// Source names an external .tmj tilemap or scene file that holds this room's tiles/entities. Mutually exclusive with Tiles.
	Source string `yaml:"source,omitempty"`
	// AllowRotate permits the placer to rotate this template in 90-degree increments.
	AllowRotate bool `yaml:"allowRotate,omitempty"`
	// Doors lists the room's door sockets.
	Doors []DoorSocket `yaml:"doors"`
	// Tiles holds an inline ASCII tile grid, one string per row: '#' is Wall, '.' is Floor, and 'a'..'z' map to values 2..27. Mutually exclusive with Source.
	Tiles []string `yaml:"tiles,omitempty"`
}

// EffectiveWeight returns t.Weight, or 1 when Weight is 0.
func (t *RoomTemplate) EffectiveWeight() float64 {
	if t.Weight == 0 {
		return 1
	}
	return t.Weight
}

// Size returns the template's width and height in cells.
func (t *RoomTemplate) Size() (w, h int) {
	if len(t.Tiles) > 0 {
		return len(t.Tiles[0]), len(t.Tiles)
	}
	return t.Width, t.Height
}

// HasTag reports whether t carries tag.
func (t *RoomTemplate) HasTag(tag string) bool {
	for _, s := range t.Tags {
		if s == tag {
			return true
		}
	}
	return false
}

// ToGrid decodes t's inline Tiles into a Grid, or synthesizes a walled floor footprint for a Source-only template.
func (t *RoomTemplate) ToGrid() (*Grid, error) {
	w, h := t.Size()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("procgen: room template %q has invalid size %dx%d", t.ID, w, h)
	}
	g := NewGrid(w, h)
	if len(t.Tiles) > 0 {
		for y, row := range t.Tiles {
			if len(row) != w {
				return nil, fmt.Errorf("procgen: room template %q row %d has width %d (want %d)", t.ID, y, len(row), w)
			}
			for x := range w {
				v, ok := decodeTileChar(row[x])
				if !ok {
					return nil, fmt.Errorf("procgen: room template %q has unsupported tile char %q at (%d,%d)", t.ID, row[x], x, y)
				}
				g.Set(x, y, v)
			}
		}
		return g, nil
	}
	// Source-only template default footprint: walled perimeter with walkable interior.
	if w <= 2 || h <= 2 {
		g.Fill(Rect{X: 0, Y: 0, W: w, H: h}, Floor)
		return g, nil
	}
	g.Fill(Rect{X: 1, Y: 1, W: w - 2, H: h - 2}, Floor)
	return g, nil
}

func decodeTileChar(c byte) (uint32, bool) {
	switch {
	case c == '#':
		return Wall, true
	case c == '.':
		return Floor, true
	case c >= 'a' && c <= 'z':
		return uint32(2 + c - 'a'), true
	default:
		return 0, false
	}
}

// ByID returns the template with the given ID, or nil if not found.
func (rt *RoomTemplates) ByID(id string) *RoomTemplate {
	if rt == nil {
		return nil
	}
	for i := range rt.Rooms {
		if rt.Rooms[i].ID == id {
			return &rt.Rooms[i]
		}
	}
	return nil
}

// ParseRoomTemplates decodes and validates a YAML room template set. Unknown fields and trailing YAML documents are rejected.
func ParseRoomTemplates(data []byte) (*RoomTemplates, error) {
	rt, err := parseRoomTemplates(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixRoomTemplates)
	}
	return rt, nil
}

func parseRoomTemplates(data []byte) (*RoomTemplates, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixRoomTemplates, "procgen: empty room templates document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var rt RoomTemplates
	if err := dec.Decode(&rt); err != nil {
		return nil, fmt.Errorf("procgen: decode room templates: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in room templates; remove the trailing --- document", "procgen: unexpected trailing YAML document in room templates")
		}
		return nil, fmt.Errorf("procgen: decode room templates: %w", err)
	}
	if err := rt.Validate(); err != nil {
		return nil, err
	}
	return &rt, nil
}

// EncodeRoomTemplates validates rt and writes it as deterministic 2-space YAML with fields in fixed order, so ParseRoomTemplates followed by EncodeRoomTemplates reproduces the exact bytes.
func EncodeRoomTemplates(rt *RoomTemplates) ([]byte, error) {
	if rt == nil {
		return nil, loaderr.Errorf("", fixRoomTemplates, "procgen: nil room templates")
	}
	if err := rt.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(rt); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("procgen: encode room templates: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("procgen: encode room templates: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks rt against the version 1 schema: version, non-empty rooms, unique IDs, valid finite non-negative weights whose sum is finite, valid dimensions, mutually exclusive Tiles or Source, non-overlapping in-bounds door sockets, and valid inline tile characters.
func (rt *RoomTemplates) Validate() error {
	if rt == nil {
		return loaderr.Errorf("", fixRoomTemplates, "procgen: nil room templates")
	}
	if rt.Version != RoomTemplatesVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", RoomTemplatesVersion), "procgen: unsupported room templates version %d (want %d)", rt.Version, RoomTemplatesVersion)
	}
	if len(rt.Rooms) == 0 {
		return loaderr.Errorf("rooms", "add at least one room template under rooms", "procgen: room templates have no rooms")
	}
	seenID := make(map[string]bool, len(rt.Rooms))
	totalWeight := 0.0
	for i := range rt.Rooms {
		r := &rt.Rooms[i]
		rPath := loaderr.Path("rooms", i)
		if r.ID == "" {
			return loaderr.Errorf(loaderr.Join(rPath, "id"), "give the room template a non-empty id", "procgen: room %d has no id", i)
		}
		if seenID[r.ID] {
			return loaderr.Errorf(loaderr.Join(rPath, "id"), "give each room template a unique id", "procgen: duplicate room id %q", r.ID)
		}
		seenID[r.ID] = true
		if r.Weight < 0 || math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) {
			return loaderr.Errorf(loaderr.Join(rPath, "weight"), "set weight to a non-negative finite number", "procgen: room %q weight %v must be non-negative and finite", r.ID, r.Weight)
		}
		totalWeight += r.EffectiveWeight()
		if math.IsInf(totalWeight, 0) {
			return loaderr.Errorf("rooms", "scale room weights down so their sum fits in float64", "procgen: room weights sum to more than the largest float64")
		}
		seenTag := make(map[string]bool, len(r.Tags))
		for ti, tag := range r.Tags {
			if tag == "" {
				return loaderr.Errorf(loaderr.Join(rPath, "tags", ti), "give the tag a non-empty name or remove it", "procgen: room %q has empty tag", r.ID)
			}
			if seenTag[tag] {
				return loaderr.Errorf(loaderr.Join(rPath, "tags", ti), "remove the duplicate tag", "procgen: room %q repeats tag %q", r.ID, tag)
			}
			seenTag[tag] = true
		}
		hasTiles := len(r.Tiles) > 0
		hasSource := r.Source != ""
		switch {
		case hasTiles && hasSource:
			return loaderr.Errorf(loaderr.Join(rPath, "source"), "keep either tiles or source, not both", "procgen: room %q sets both tiles and source", r.ID)
		case !hasTiles && !hasSource:
			return loaderr.Errorf(rPath, "set either inline tiles or an external source", "procgen: room %q must set either tiles or source", r.ID)
		}
		var w, h int
		if hasTiles {
			h = len(r.Tiles)
			w = len(r.Tiles[0])
			if w == 0 {
				return loaderr.Errorf(loaderr.Join(rPath, "tiles", 0), "write non-empty ASCII rows in tiles", "procgen: room %q has empty tile row", r.ID)
			}
			if r.Width != 0 && r.Width != w {
				return loaderr.Errorf(loaderr.Join(rPath, "width"), fmt.Sprintf("set width to %d to match tiles, or omit width", w), "procgen: room %q width %d does not match tiles width %d", r.ID, r.Width, w)
			}
			if r.Height != 0 && r.Height != h {
				return loaderr.Errorf(loaderr.Join(rPath, "height"), fmt.Sprintf("set height to %d to match tiles, or omit height", h), "procgen: room %q height %d does not match tiles height %d", r.ID, r.Height, h)
			}
		} else {
			w, h = r.Width, r.Height
			if w <= 0 || h <= 0 {
				field := "width"
				if w > 0 {
					field = "height"
				}
				return loaderr.Errorf(loaderr.Join(rPath, field), "set positive width and height for a source room template", "procgen: room %q with source must set positive width and height", r.ID)
			}
		}
		if w > MaxSide || h > MaxSide || tooManyCells(w, h) {
			return loaderr.Errorf(rPath, fmt.Sprintf("keep room dimensions within %dx%d and %d cells", MaxSide, MaxSide, MaxCells), "procgen: room %q size %dx%d exceeds grid limits", r.ID, w, h)
		}
		if len(r.Doors) == 0 {
			return loaderr.Errorf(loaderr.Join(rPath, "doors"), "add at least one door socket under doors", "procgen: room %q has no doors", r.ID)
		}
		if err := validateDoors(rPath, r.ID, w, h, r.Doors); err != nil {
			return err
		}
		if hasTiles {
			g, err := r.ToGrid()
			if err != nil {
				return loaderr.Errorf(loaderr.Join(rPath, "tiles"), "make every row the same width using '#', '.' or 'a'..'z'", "%v", err)
			}
			// Opening all doors must yield at least one floor cell and a single 4-connected region
			// (using non-Wall passability when custom tiles exist, or IsFloor for standard rooms).
			for _, d := range r.Doors {
				for _, p := range d.Cells(w, h) {
					if g.At(p.X, p.Y) == Wall {
						g.Set(p.X, p.Y, Floor)
					}
				}
			}
			if g.Count(isNonWall) == 0 {
				return loaderr.Errorf(loaderr.Join(rPath, "tiles"), "add at least one walkable (non-'#') cell", "procgen: room %q has no walkable cells", r.ID)
			}
			if !g.Connected(isNonWall) {
				return loaderr.Errorf(loaderr.Join(rPath, "tiles"), "connect all walkable cells and doors into a single 4-connected region", "procgen: room %q walkable cells and doors are not 4-connected", r.ID)
			}
		}
	}
	return nil
}

func isNonWall(v uint32) bool { return v != Wall }

func validateDoors(rPath, roomID string, w, h int, doors []DoorSocket) error {
	// Track occupied spans per side to detect overlapping sockets.
	type span struct{ lo, hi int }
	occupied := map[Side][]span{}
	for i, d := range doors {
		dPath := loaderr.Join(rPath, "doors", i)
		if !d.Side.Valid() {
			return loaderr.Errorf(loaderr.Join(dPath, "side"), "set side to north, south, east or west", "procgen: room %q door %d has invalid side %q", roomID, i, d.Side)
		}
		if d.Width < 0 {
			return loaderr.Errorf(loaderr.Join(dPath, "width"), "set width to a positive integer, or omit it for 1", "procgen: room %q door %d has negative width %d", roomID, i, d.Width)
		}
		sideLen := w
		if d.Side == SideEast || d.Side == SideWest {
			sideLen = h
		}
		sp := d.Span()
		if d.Offset < 0 || d.Offset+sp > sideLen {
			return loaderr.Errorf(loaderr.Join(dPath, "offset"), fmt.Sprintf("keep offset >= 0 and offset + width <= %d", sideLen), "procgen: room %q door %d on %s (offset %d, width %d) lies outside side length %d", roomID, i, d.Side, d.Offset, sp, sideLen)
		}
		lo, hi := d.Offset, d.Offset+sp
		for _, prev := range occupied[d.Side] {
			if lo < prev.hi && prev.lo < hi {
				return loaderr.Errorf(dPath, "move or narrow the door so it does not overlap another door on the same side", "procgen: room %q door %d overlaps another door on %s", roomID, i, d.Side)
			}
		}
		occupied[d.Side] = append(occupied[d.Side], span{lo: lo, hi: hi})
	}
	return nil
}

// GridFromTilemap converts one layer of a tilemap (such as one loaded from a Tiled .tmj file with tilemap.LoadTiled) into a Grid of Wall and Floor cells. A cell is marked Floor when passable returns true for its GID (or when GID == 0 if passable is nil).
func GridFromTilemap(m *tilemap.Tilemap, layer int, passable func(gid uint32) bool) *Grid {
	if m == nil || m.Width <= 0 || m.Height <= 0 {
		return NewGrid(0, 0)
	}
	if passable == nil {
		passable = func(gid uint32) bool { return gid == 0 }
	}
	g := NewGrid(m.Width, m.Height)
	for y := range m.Height {
		for x := range m.Width {
			if passable(m.TileAt(layer, x, y)) {
				g.Set(x, y, Floor)
			}
		}
	}
	return g
}
