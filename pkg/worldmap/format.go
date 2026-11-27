package worldmap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Version is the map definition schema version this package reads and writes.
const Version = 1

// Doc is the root of a versioned YAML map definition.
type Doc struct {
	// Version is the schema version; it must equal Version (1).
	Version int `yaml:"version"`
	// Rooms are the map's rooms. Their order decides which room wins a lookup when rooms of different areas overlap, and the order they are drawn in.
	Rooms []RoomDoc `yaml:"rooms"`
	// Markers are fixed markers placed by the map's author, shown once their room is discovered.
	Markers []Marker `yaml:"markers,omitempty"`
}

// RoomDoc describes one room of a map definition.
type RoomDoc struct {
	// ID uniquely identifies the room; it is what saves refer to.
	ID string `yaml:"id"`
	// Name is an optional display name.
	Name string `yaml:"name,omitempty"`
	// Area groups rooms that share one map, such as a floor or a region. Rooms in the same area must not overlap. Empty is the default area.
	Area string `yaml:"area,omitempty"`
	// X and Y are the room's top-left corner in world coordinates.
	X float32 `yaml:"x"`
	Y float32 `yaml:"y"`
	// Width and Height are the room's size in world units; both must be positive.
	Width  float32 `yaml:"width"`
	Height float32 `yaml:"height"`
	// Links are the IDs of rooms this room connects to, such as through a door. Links are undirected, so each pair is listed once, on either room.
	Links []string `yaml:"links,omitempty,flow"`
}

// Marker is a point of interest inside a room: fixed in a Doc, or placed by the player with Map.AddMarker.
type Marker struct {
	// ID uniquely identifies the marker among the map's fixed markers and the player's markers.
	ID string `yaml:"id" json:"id"`
	// Kind says what the marker is, such as "save" or "boss"; Style.MarkerKinds can color each kind.
	Kind string `yaml:"kind" json:"kind"`
	// Room is the ID of the room the marker is in; the marker is shown once that room is discovered.
	Room string `yaml:"room" json:"room"`
	// X and Y are the marker's position in world coordinates, inside the room's bounds (edges included).
	X float32 `yaml:"x" json:"x"`
	Y float32 `yaml:"y" json:"y"`
	// Label is optional text for the marker, such as a player's note.
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
}

// Parse decodes a YAML map definition and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*Doc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("worldmap: empty map document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc Doc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("worldmap: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("worldmap: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("worldmap: decode: %w", err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Encode validates doc and writes it as deterministic 2-space YAML that Parse reads back to an equal Doc, and that encodes again to the same bytes. Comments, flow style and other formatting of a hand-written file are not preserved.
func Encode(doc *Doc) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("worldmap: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("worldmap: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks doc against the version 1 schema: a supported version, at least one room, unique non-empty IDs, finite coordinates, positive sizes that survive float32 rounding at the room's position, areas whose combined bounds fit in float32, links to other existing rooms with no pair listed twice, no overlapping rooms within an area, and fixed markers inside existing rooms.
func Validate(doc *Doc) error {
	if doc == nil {
		return errors.New("worldmap: nil map document")
	}
	if doc.Version != Version {
		return fmt.Errorf("worldmap: unsupported version %d (want %d)", doc.Version, Version)
	}
	if len(doc.Rooms) == 0 {
		return errors.New("worldmap: map has no rooms")
	}
	index := make(map[string]int, len(doc.Rooms))
	for i := range doc.Rooms {
		r := &doc.Rooms[i]
		if err := validateID("room", r.ID); err != nil {
			return err
		}
		if _, dup := index[r.ID]; dup {
			return fmt.Errorf("worldmap: duplicate room ID %q", r.ID)
		}
		index[r.ID] = i
		if !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) {
			return fmt.Errorf("worldmap: room %q has a non-finite position or size", r.ID)
		}
		if r.Width <= 0 || r.Height <= 0 {
			return fmt.Errorf("worldmap: room %q needs a positive width and height", r.ID)
		}
		right, bottom := float32(r.X+r.Width), float32(r.Y+r.Height)
		if !finite(right) || !finite(bottom) {
			return fmt.Errorf("worldmap: room %q extends past the float32 range", r.ID)
		}
		// A size far below the position's float32 precision rounds away, leaving a room no point is inside.
		if right <= r.X || bottom <= r.Y {
			return fmt.Errorf("worldmap: room %q is too small for float32 precision at its position", r.ID)
		}
	}
	if err := checkAreaSpans(doc.Rooms); err != nil {
		return err
	}
	type pair struct{ a, b string }
	links := make(map[pair]struct{})
	for i := range doc.Rooms {
		r := &doc.Rooms[i]
		for _, to := range r.Links {
			if to == r.ID {
				return fmt.Errorf("worldmap: room %q links to itself", r.ID)
			}
			if _, ok := index[to]; !ok {
				return fmt.Errorf("worldmap: room %q links to unknown room %q", r.ID, to)
			}
			p := pair{r.ID, to}
			if p.b < p.a {
				p.a, p.b = p.b, p.a
			}
			if _, dup := links[p]; dup {
				return fmt.Errorf("worldmap: link between rooms %q and %q is listed twice", p.a, p.b)
			}
			links[p] = struct{}{}
		}
	}
	if err := checkOverlaps(doc.Rooms); err != nil {
		return err
	}
	ids := make(map[string]struct{}, len(doc.Markers))
	for i := range doc.Markers {
		mk := &doc.Markers[i]
		if _, dup := ids[mk.ID]; dup {
			return fmt.Errorf("worldmap: duplicate marker ID %q", mk.ID)
		}
		if err := validateMarker(mk, func(id string) (*RoomDoc, bool) {
			ri, ok := index[id]
			if !ok {
				return nil, false
			}
			return &doc.Rooms[ri], true
		}); err != nil {
			return err
		}
		ids[mk.ID] = struct{}{}
	}
	return nil
}

// checkAreaSpans refuses an area whose rooms together span more than float32 can hold, which would leave the area's bounds infinite and its world map undrawable.
func checkAreaSpans(rooms []RoomDoc) error {
	type span struct{ x0, y0, x1, y1 float32 }
	spans := make(map[string]span)
	for i := range rooms {
		r := &rooms[i]
		x1, y1 := float32(r.X+r.Width), float32(r.Y+r.Height)
		if s, ok := spans[r.Area]; ok {
			spans[r.Area] = span{min(s.x0, r.X), min(s.y0, r.Y), max(s.x1, x1), max(s.y1, y1)}
		} else {
			spans[r.Area] = span{r.X, r.Y, x1, y1}
		}
	}
	for i := range rooms {
		area := rooms[i].Area
		s, ok := spans[area]
		if !ok {
			continue
		}
		delete(spans, area)
		if !finite(float32(s.x1-s.x0)) || !finite(float32(s.y1-s.y0)) {
			return fmt.Errorf("worldmap: area %q spans past the float32 range", area)
		}
	}
	return nil
}

// checkOverlaps refuses two rooms of the same area whose interiors overlap; rooms that only share an edge are fine. It sweeps rooms sorted by X so maps with many rooms stay fast.
func checkOverlaps(rooms []RoomDoc) error {
	order := make([]int, len(rooms))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		ra, rb := &rooms[a], &rooms[b]
		if c := strings.Compare(ra.Area, rb.Area); c != 0 {
			return c
		}
		switch {
		case ra.X < rb.X:
			return -1
		case ra.X > rb.X:
			return 1
		}
		return 0
	})
	for i, ai := range order {
		a := &rooms[ai]
		for _, bi := range order[i+1:] {
			b := &rooms[bi]
			if b.Area != a.Area || b.X >= a.X+a.Width {
				break
			}
			if b.Y < a.Y+a.Height && a.Y < b.Y+b.Height {
				first, second := a, b
				if bi < ai {
					first, second = b, a
				}
				return fmt.Errorf("worldmap: rooms %q and %q overlap in area %q", first.ID, second.ID, a.Area)
			}
		}
	}
	return nil
}

func validateMarker(mk *Marker, room func(string) (*RoomDoc, bool)) error {
	if err := validateID("marker", mk.ID); err != nil {
		return err
	}
	if strings.TrimSpace(mk.Kind) == "" {
		return fmt.Errorf("worldmap: marker %q has no kind", mk.ID)
	}
	r, ok := room(mk.Room)
	if !ok {
		return fmt.Errorf("worldmap: marker %q is in unknown room %q", mk.ID, mk.Room)
	}
	if !finite(mk.X) || !finite(mk.Y) {
		return fmt.Errorf("worldmap: marker %q has a non-finite position", mk.ID)
	}
	if mk.X < r.X || mk.Y < r.Y || mk.X > r.X+r.Width || mk.Y > r.Y+r.Height {
		return fmt.Errorf("worldmap: marker %q at (%g, %g) is outside room %q", mk.ID, mk.X, mk.Y, mk.Room)
	}
	return nil
}

func validateID(what, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("worldmap: %s with an empty ID", what)
	}
	if strings.TrimSpace(id) != id {
		return fmt.Errorf("worldmap: %s ID %q has leading or trailing space", what, id)
	}
	return nil
}

func finite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
