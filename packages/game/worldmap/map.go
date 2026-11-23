package worldmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math2d"
)

// StateVersion is the schema version of State values written by Map.State and accepted by Map.Restore.
const StateVersion = 1

var (
	// ErrUnknownRoom reports a room ID that the map does not define.
	ErrUnknownRoom = errors.New("worldmap: unknown room")
	// ErrInvalidMarker reports a marker that is malformed, outside its room or whose ID is already taken.
	ErrInvalidMarker = errors.New("worldmap: invalid marker")
	// ErrInvalidState reports a State that does not match the map or the state schema.
	ErrInvalidState = errors.New("worldmap: invalid state")
)

// Discovery is how much the player knows about a room.
type Discovery uint8

const (
	// Unknown rooms are not drawn.
	Unknown Discovery = iota
	// Revealed rooms are known, for example from a map item, but have not been entered; they are drawn in Style.Revealed.
	Revealed
	// Visited rooms have been entered; they are drawn in Style.Visited.
	Visited
)

// String returns "unknown", "revealed" or "visited".
func (d Discovery) String() string {
	switch d {
	case Unknown:
		return "unknown"
	case Revealed:
		return "revealed"
	case Visited:
		return "visited"
	}
	return fmt.Sprintf("Discovery(%d)", uint8(d))
}

// Room is a room of a built Map.
type Room struct {
	// ID uniquely identifies the room.
	ID string
	// Name is the room's display name, possibly empty.
	Name string
	// Area is the area the room belongs to; empty is the default area.
	Area string
	// Bounds is the room's rectangle in world coordinates.
	Bounds math2d.Rect
	// Links are the IDs of the rooms connected to this one, in both directions, sorted.
	Links []string
}

// Map holds a map definition and what the player has discovered of it. It is not safe for concurrent use.
type Map struct {
	rooms    []Room
	index    map[string]int
	areas    map[string]math2d.Rect
	links    [][2]int // undirected links as room index pairs, a < b, in definition order
	fixed    []Marker // sorted by ID
	fixedIDs map[string]struct{}
	status   []Discovery
	current  int      // -1 when the player is in no known room
	pins     []Marker // sorted by ID
	pinIDs   map[string]struct{}
}

// Load parses a YAML map definition and builds a Map from it.
func Load(data []byte) (*Map, error) {
	doc, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return New(doc)
}

// New validates doc and builds a Map with nothing discovered. The Map keeps its own copy of the definition, so later changes to doc do not affect it.
func New(doc *Doc) (*Map, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	m := &Map{
		rooms:    make([]Room, len(doc.Rooms)),
		index:    make(map[string]int, len(doc.Rooms)),
		areas:    make(map[string]math2d.Rect),
		fixedIDs: make(map[string]struct{}, len(doc.Markers)),
		status:   make([]Discovery, len(doc.Rooms)),
		current:  -1,
		pinIDs:   make(map[string]struct{}),
	}
	for i, r := range doc.Rooms {
		b := math2d.Rect{X: r.X, Y: r.Y, W: r.Width, H: r.Height}
		m.rooms[i] = Room{ID: r.ID, Name: r.Name, Area: r.Area, Bounds: b}
		m.index[r.ID] = i
		if ab, ok := m.areas[r.Area]; ok {
			m.areas[r.Area] = ab.Union(b)
		} else {
			m.areas[r.Area] = b
		}
	}
	for i, r := range doc.Rooms {
		for _, to := range r.Links {
			j := m.index[to]
			m.rooms[i].Links = append(m.rooms[i].Links, to)
			m.rooms[j].Links = append(m.rooms[j].Links, r.ID)
			a, b := i, j
			if b < a {
				a, b = b, a
			}
			m.links = append(m.links, [2]int{a, b})
		}
	}
	for i := range m.rooms {
		slices.Sort(m.rooms[i].Links)
	}
	m.fixed = slices.Clone(doc.Markers)
	sortMarkers(m.fixed)
	for _, mk := range m.fixed {
		m.fixedIDs[mk.ID] = struct{}{}
	}
	return m, nil
}

// Rooms returns every room in definition order. The returned slice is a copy.
func (m *Map) Rooms() []Room {
	out := make([]Room, len(m.rooms))
	for i, r := range m.rooms {
		r.Links = slices.Clone(r.Links)
		out[i] = r
	}
	return out
}

// Room returns the room with the given ID.
func (m *Map) Room(id string) (Room, bool) {
	i, ok := m.index[id]
	if !ok {
		return Room{}, false
	}
	r := m.rooms[i]
	r.Links = slices.Clone(r.Links)
	return r, true
}

// Areas returns the names of the map's areas, sorted.
func (m *Map) Areas() []string {
	out := make([]string, 0, len(m.areas))
	for a := range m.areas {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// AreaBounds returns the smallest rectangle holding every room of area, discovered or not.
func (m *Map) AreaBounds(area string) (math2d.Rect, bool) {
	b, ok := m.areas[area]
	return b, ok
}

// RoomAt returns the ID of the room containing p (half-open bounds, as math2d.Rect.Contains). When rooms of different areas overlap, the current room wins, then rooms of the current area, then the first room in definition order.
func (m *Map) RoomAt(p math2d.Vec2) (string, bool) {
	i := m.roomAt(p)
	if i < 0 {
		return "", false
	}
	return m.rooms[i].ID, true
}

func (m *Map) roomAt(p math2d.Vec2) int {
	if m.current >= 0 && m.rooms[m.current].Bounds.Contains(p) {
		return m.current
	}
	first := -1
	area := m.currentArea()
	for i := range m.rooms {
		if !m.rooms[i].Bounds.Contains(p) {
			continue
		}
		if m.rooms[i].Area == area {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

func (m *Map) currentArea() string {
	if m.current < 0 {
		return ""
	}
	return m.rooms[m.current].Area
}

// Status returns how much the player knows about the room; Unknown for an ID the map does not define.
func (m *Map) Status(id string) Discovery {
	i, ok := m.index[id]
	if !ok {
		return Unknown
	}
	return m.status[i]
}

// Visit marks the room under the player's position p as visited and makes it the current room. It returns that room's ID and whether this is the first visit; when p is in no room, it returns "" and false and keeps the current room.
func (m *Map) Visit(p math2d.Vec2) (id string, first bool) {
	i := m.roomAt(p)
	if i < 0 {
		return "", false
	}
	return m.rooms[i].ID, m.enter(i)
}

// Enter marks the room as visited and makes it the current room, as when a room transition moves the player into it. It reports whether this is the first visit.
func (m *Map) Enter(id string) (first bool, err error) {
	i, ok := m.index[id]
	if !ok {
		return false, fmt.Errorf("%w %q", ErrUnknownRoom, id)
	}
	return m.enter(i), nil
}

func (m *Map) enter(i int) bool {
	m.current = i
	if m.status[i] == Visited {
		return false
	}
	m.status[i] = Visited
	return true
}

// Reveal marks unknown rooms as revealed, as a map item would. Visited rooms stay visited. It changes nothing and returns an error wrapping ErrUnknownRoom if any ID is unknown.
func (m *Map) Reveal(ids ...string) error {
	for _, id := range ids {
		if _, ok := m.index[id]; !ok {
			return fmt.Errorf("%w %q", ErrUnknownRoom, id)
		}
	}
	for _, id := range ids {
		i := m.index[id]
		if m.status[i] == Unknown {
			m.status[i] = Revealed
		}
	}
	return nil
}

// RevealArea reveals every unknown room of area. It returns an error wrapping ErrUnknownRoom when the map has no such area.
func (m *Map) RevealArea(area string) error {
	if _, ok := m.areas[area]; !ok {
		return fmt.Errorf("%w: no area %q", ErrUnknownRoom, area)
	}
	for i := range m.rooms {
		if m.rooms[i].Area == area && m.status[i] == Unknown {
			m.status[i] = Revealed
		}
	}
	return nil
}

// Current returns the ID of the room the player was last in, if any.
func (m *Map) Current() (string, bool) {
	if m.current < 0 {
		return "", false
	}
	return m.rooms[m.current].ID, true
}

// Visited returns the IDs of the visited rooms, sorted.
func (m *Map) Visited() []string { return m.withStatus(Visited) }

// Revealed returns the IDs of the rooms that are revealed but not visited, sorted.
func (m *Map) Revealed() []string { return m.withStatus(Revealed) }

func (m *Map) withStatus(s Discovery) []string {
	out := []string{}
	for i, st := range m.status {
		if st == s {
			out = append(out, m.rooms[i].ID)
		}
	}
	slices.Sort(out)
	return out
}

// Explored returns how many rooms of area are visited and how many it has, for a completion percentage.
func (m *Map) Explored(area string) (visited, total int) {
	for i := range m.rooms {
		if m.rooms[i].Area != area {
			continue
		}
		total++
		if m.status[i] == Visited {
			visited++
		}
	}
	return visited, total
}

// AddMarker places a player marker. The marker needs a non-empty ID not used by any other marker, a kind, and a position inside an existing room; otherwise AddMarker returns an error wrapping ErrInvalidMarker and changes nothing.
func (m *Map) AddMarker(mk Marker) error {
	if err := m.checkPin(mk, m.pinIDs); err != nil {
		return err
	}
	m.pinIDs[mk.ID] = struct{}{}
	i, _ := slices.BinarySearchFunc(m.pins, mk.ID, func(a Marker, id string) int { return strings.Compare(a.ID, id) })
	m.pins = slices.Insert(m.pins, i, mk)
	return nil
}

func (m *Map) checkPin(mk Marker, taken map[string]struct{}) error {
	if _, dup := m.fixedIDs[mk.ID]; dup {
		return fmt.Errorf("%w: ID %q is a fixed marker", ErrInvalidMarker, mk.ID)
	}
	if _, dup := taken[mk.ID]; dup {
		return fmt.Errorf("%w: ID %q is already used", ErrInvalidMarker, mk.ID)
	}
	err := validateMarker(&mk, func(id string) (*RoomDoc, bool) {
		i, ok := m.index[id]
		if !ok {
			return nil, false
		}
		b := m.rooms[i].Bounds
		return &RoomDoc{X: b.X, Y: b.Y, Width: b.W, Height: b.H}, true
	})
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidMarker, strings.TrimPrefix(err.Error(), "worldmap: "))
	}
	return nil
}

// RemoveMarker removes the player marker with the given ID and reports whether there was one. Fixed markers from the definition cannot be removed.
func (m *Map) RemoveMarker(id string) bool {
	i, ok := slices.BinarySearchFunc(m.pins, id, func(a Marker, id string) int { return strings.Compare(a.ID, id) })
	if !ok {
		return false
	}
	m.pins = slices.Delete(m.pins, i, i+1)
	delete(m.pinIDs, id)
	return true
}

// Markers returns every marker, fixed and player-placed, sorted by ID, whether or not its room is discovered.
func (m *Map) Markers() []Marker {
	out := make([]Marker, 0, len(m.fixed)+len(m.pins))
	out = append(out, m.fixed...)
	out = append(out, m.pins...)
	sortMarkers(out)
	return out
}

// VisibleMarkers returns the markers whose room is revealed or visited, sorted by ID.
func (m *Map) VisibleMarkers() []Marker {
	out := []Marker{}
	for _, mk := range m.Markers() {
		if m.status[m.index[mk.Room]] != Unknown {
			out = append(out, mk)
		}
	}
	return out
}

// PlayerMarkers returns the markers placed with AddMarker, sorted by ID.
func (m *Map) PlayerMarkers() []Marker { return slices.Clone(m.pins) }

// Reset forgets everything discovered, the current room and the player's markers.
func (m *Map) Reset() {
	clear(m.status)
	m.current = -1
	m.pins = nil
	clear(m.pinIDs)
}

// State is what a Map has discovered, in a form a save.Store slot (JSON) or a YAML file can hold. Lists are sorted, so equal states encode to equal bytes.
type State struct {
	// Version is the state schema version; Map.State writes StateVersion.
	Version int `json:"version" yaml:"version"`
	// Current is the ID of the room the player was last in, or empty.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Visited holds the IDs of the visited rooms, sorted.
	Visited []string `json:"visited" yaml:"visited"`
	// Revealed holds the IDs of the rooms revealed but not visited, sorted.
	Revealed []string `json:"revealed,omitempty" yaml:"revealed,omitempty"`
	// Markers holds the player's markers, sorted by ID; fixed markers come from the definition and are not saved.
	Markers []Marker `json:"markers,omitempty" yaml:"markers,omitempty"`
}

// UnmarshalJSON decodes a State, refusing unknown fields and trailing data so a mistyped or foreign save is reported instead of silently dropped.
func (s *State) UnmarshalJSON(data []byte) error {
	type plain State
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var v plain
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: trailing data", ErrInvalidState)
	}
	*s = State(v)
	return nil
}

// State returns what the map has discovered and the player's markers.
func (m *Map) State() State {
	s := State{
		Version: StateVersion,
		Visited: m.Visited(),
	}
	if id, ok := m.Current(); ok {
		s.Current = id
	}
	if r := m.Revealed(); len(r) > 0 {
		s.Revealed = r
	}
	if len(m.pins) > 0 {
		s.Markers = slices.Clone(m.pins)
	}
	return s
}

// Restore replaces the map's discovery, current room and player markers with s. It checks s first and, on any problem (another version, an unknown or repeated room, a current room that is not visited, an invalid marker), returns an error wrapping ErrInvalidState and changes nothing.
//
// A save that names a room the definition no longer has is refused; when a game renames or removes rooms, add a save.Store migration that rewrites the saved IDs.
func (m *Map) Restore(s State) error {
	if s.Version != StateVersion {
		return fmt.Errorf("%w: version %d (want %d)", ErrInvalidState, s.Version, StateVersion)
	}
	status := make([]Discovery, len(m.rooms))
	if err := m.markAll(status, s.Visited, Visited); err != nil {
		return err
	}
	if err := m.markAll(status, s.Revealed, Revealed); err != nil {
		return err
	}
	current := -1
	if s.Current != "" {
		i, ok := m.index[s.Current]
		if !ok {
			return fmt.Errorf("%w: current room %q is unknown", ErrInvalidState, s.Current)
		}
		if status[i] != Visited {
			return fmt.Errorf("%w: current room %q is not visited", ErrInvalidState, s.Current)
		}
		current = i
	}
	pinIDs := make(map[string]struct{}, len(s.Markers))
	for _, mk := range s.Markers {
		if err := m.checkPin(mk, pinIDs); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidState, err)
		}
		pinIDs[mk.ID] = struct{}{}
	}
	pins := slices.Clone(s.Markers)
	sortMarkers(pins)
	m.status = status
	m.current = current
	m.pins = pins
	m.pinIDs = pinIDs
	return nil
}

func (m *Map) markAll(status []Discovery, ids []string, s Discovery) error {
	for _, id := range ids {
		i, ok := m.index[id]
		if !ok {
			return fmt.Errorf("%w: %s room %q is unknown", ErrInvalidState, s, id)
		}
		if status[i] != Unknown {
			return fmt.Errorf("%w: room %q is listed twice", ErrInvalidState, id)
		}
		status[i] = s
	}
	return nil
}

func sortMarkers(ms []Marker) {
	slices.SortFunc(ms, func(a, b Marker) int { return strings.Compare(a.ID, b.ID) })
}
