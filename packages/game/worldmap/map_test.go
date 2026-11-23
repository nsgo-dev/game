package worldmap_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/game/packages/game/worldmap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func loadCastle(t testing.TB) *worldmap.Map {
	t.Helper()
	m, err := worldmap.Load(readCastle(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m
}

func TestNewBuildsRoomsLinksAndAreas(t *testing.T) {
	m := loadCastle(t)
	if got := len(m.Rooms()); got != 4 {
		t.Fatalf("Rooms = %d, want 4", got)
	}
	entrance, ok := m.Room("entrance")
	if !ok || entrance.Name != "Castle Gate" || entrance.Bounds != (math2d.Rect{W: 320, H: 180}) {
		t.Fatalf("Room(entrance) = %+v, %v", entrance, ok)
	}
	if !slices.Equal(entrance.Links, []string{"cellar", "hall"}) {
		t.Fatalf("entrance links = %v, want [cellar hall]", entrance.Links)
	}
	hall, _ := m.Room("hall")
	if !slices.Equal(hall.Links, []string{"entrance", "tower"}) {
		t.Fatalf("hall links = %v, want both directions", hall.Links)
	}
	if !slices.Equal(m.Areas(), []string{"", "crypt"}) {
		t.Fatalf("Areas = %q", m.Areas())
	}
	if b, ok := m.AreaBounds(""); !ok || b != (math2d.Rect{X: 0, Y: -180, W: 800, H: 360}) {
		t.Fatalf("AreaBounds(\"\") = %+v, %v", b, ok)
	}
	if _, ok := m.Room("missing"); ok {
		t.Fatal("Room(missing) found a room")
	}

	entrance.Links[0] = "changed"
	if again, _ := m.Room("entrance"); again.Links[0] != "cellar" {
		t.Fatal("Room returned a slice that aliases the map")
	}
}

func TestNewCopiesTheDefinition(t *testing.T) {
	doc, err := worldmap.Parse(readCastle(t))
	if err != nil {
		t.Fatal(err)
	}
	m, err := worldmap.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Rooms[0].ID = "renamed"
	doc.Markers[0].X = -1
	if _, ok := m.Room("entrance"); !ok {
		t.Fatal("changing the Doc changed the Map")
	}
	if mk := m.Markers()[0]; mk.ID != "boss" || mk.X != 720 {
		t.Fatalf("marker changed with the Doc: %+v", mk)
	}
	if _, err := worldmap.New(&worldmap.Doc{Version: 1}); err == nil {
		t.Fatal("New accepted an invalid Doc")
	}
}

func TestVisitMarksRoomsVisited(t *testing.T) {
	m := loadCastle(t)
	if _, ok := m.Current(); ok {
		t.Fatal("new map has a current room")
	}
	id, first := m.Visit(math2d.Vec2{X: 10, Y: 10})
	if id != "entrance" || !first {
		t.Fatalf("Visit = %q, %v; want entrance, true", id, first)
	}
	if id, first = m.Visit(math2d.Vec2{X: 20, Y: 20}); id != "entrance" || first {
		t.Fatalf("second Visit = %q, %v; want entrance, false", id, first)
	}
	if id, first = m.Visit(math2d.Vec2{X: 320, Y: 0}); id != "hall" || !first {
		t.Fatalf("Visit on hall's left edge = %q, %v; want hall, true", id, first)
	}
	if id, first = m.Visit(math2d.Vec2{X: -50, Y: -50}); id != "" || first {
		t.Fatalf("Visit outside every room = %q, %v", id, first)
	}
	if cur, _ := m.Current(); cur != "hall" {
		t.Fatalf("Current after leaving the map = %q, want hall kept", cur)
	}
	if got := m.Visited(); !slices.Equal(got, []string{"entrance", "hall"}) {
		t.Fatalf("Visited = %v", got)
	}
	if s := m.Status("tower"); s != worldmap.Unknown {
		t.Fatalf("Status(tower) = %v", s)
	}
	if v, total := m.Explored(""); v != 2 || total != 3 {
		t.Fatalf("Explored = %d/%d, want 2/3", v, total)
	}
}

func TestVisitPrefersTheCurrentArea(t *testing.T) {
	m := loadCastle(t)
	if id, _ := m.Visit(math2d.Vec2{X: 5, Y: 5}); id != "entrance" {
		t.Fatalf("Visit with no current room = %q, want the first room in definition order", id)
	}
	if first, err := m.Enter("cellar"); err != nil || !first {
		t.Fatalf("Enter(cellar) = %v, %v", first, err)
	}
	if id, _ := m.Visit(math2d.Vec2{X: 5, Y: 5}); id != "cellar" {
		t.Fatalf("Visit inside the cellar's area = %q, want cellar", id)
	}
	if id, ok := m.RoomAt(math2d.Vec2{X: 330, Y: 5}); !ok || id != "hall" {
		t.Fatalf("RoomAt outside the current area = %q, %v; want hall", id, ok)
	}
	if _, err := m.Enter("nowhere"); !errors.Is(err, worldmap.ErrUnknownRoom) {
		t.Fatalf("Enter(nowhere) = %v, want ErrUnknownRoom", err)
	}
}

func TestRevealDoesNotDowngradeAndIsAtomic(t *testing.T) {
	m := loadCastle(t)
	if _, err := m.Enter("hall"); err != nil {
		t.Fatal(err)
	}
	if err := m.Reveal("tower", "nowhere"); !errors.Is(err, worldmap.ErrUnknownRoom) {
		t.Fatalf("Reveal with an unknown ID = %v", err)
	}
	if m.Status("tower") != worldmap.Unknown {
		t.Fatal("a failed Reveal changed the map")
	}
	if err := m.Reveal("tower", "hall"); err != nil {
		t.Fatal(err)
	}
	if m.Status("tower") != worldmap.Revealed || m.Status("hall") != worldmap.Visited {
		t.Fatalf("statuses = %v, %v", m.Status("tower"), m.Status("hall"))
	}
	if err := m.RevealArea("crypt"); err != nil || m.Status("cellar") != worldmap.Revealed {
		t.Fatalf("RevealArea = %v, cellar %v", err, m.Status("cellar"))
	}
	if err := m.RevealArea("attic"); !errors.Is(err, worldmap.ErrUnknownRoom) {
		t.Fatalf("RevealArea(attic) = %v", err)
	}
	if got := m.Revealed(); !slices.Equal(got, []string{"cellar", "tower"}) {
		t.Fatalf("Revealed = %v", got)
	}
	if first, _ := m.Enter("tower"); !first {
		t.Fatal("entering a revealed room is not its first visit")
	}
}

func TestMarkers(t *testing.T) {
	m := loadCastle(t)
	if got := m.VisibleMarkers(); len(got) != 0 {
		t.Fatalf("VisibleMarkers before discovery = %v", got)
	}
	pin := worldmap.Marker{ID: "pin-1", Kind: "note", Room: "hall", X: 400, Y: 90, Label: "locked door"}
	if err := m.AddMarker(pin); err != nil {
		t.Fatalf("AddMarker: %v", err)
	}
	for name, bad := range map[string]worldmap.Marker{
		"fixed id": {ID: "boss", Kind: "note", Room: "hall", X: 400, Y: 90},
		"taken id": {ID: "pin-1", Kind: "note", Room: "hall", X: 400, Y: 90},
		"no room":  {ID: "p", Kind: "note", Room: "attic", X: 0, Y: 0},
		"outside":  {ID: "p", Kind: "note", Room: "hall", X: 0, Y: 0},
		"no kind":  {ID: "p", Room: "hall", X: 400, Y: 90},
		"empty id": {Kind: "note", Room: "hall", X: 400, Y: 90},
	} {
		if err := m.AddMarker(bad); !errors.Is(err, worldmap.ErrInvalidMarker) {
			t.Errorf("AddMarker(%s) = %v, want ErrInvalidMarker", name, err)
		}
	}
	if err := m.AddMarker(worldmap.Marker{ID: "a-pin", Kind: "note", Room: "entrance", X: 320, Y: 180}); err != nil {
		t.Fatalf("AddMarker on the room's corner: %v", err)
	}
	ids := func(ms []worldmap.Marker) []string {
		out := []string{}
		for _, mk := range ms {
			out = append(out, mk.ID)
		}
		return out
	}
	if got := ids(m.Markers()); !slices.Equal(got, []string{"a-pin", "boss", "pin-1", "save-gate"}) {
		t.Fatalf("Markers = %v", got)
	}
	if _, err := m.Enter("hall"); err != nil {
		t.Fatal(err)
	}
	if got := ids(m.VisibleMarkers()); !slices.Equal(got, []string{"pin-1"}) {
		t.Fatalf("VisibleMarkers = %v, want [pin-1]", got)
	}
	if m.RemoveMarker("boss") {
		t.Fatal("RemoveMarker removed a fixed marker")
	}
	if !m.RemoveMarker("pin-1") || m.RemoveMarker("pin-1") {
		t.Fatal("RemoveMarker(pin-1) should succeed once")
	}
	if got := ids(m.PlayerMarkers()); !slices.Equal(got, []string{"a-pin"}) {
		t.Fatalf("PlayerMarkers = %v", got)
	}
	if err := m.AddMarker(pin); err != nil {
		t.Fatalf("re-adding a removed ID: %v", err)
	}
}

func discoverSome(t testing.TB, m *worldmap.Map) {
	t.Helper()
	m.Visit(math2d.Vec2{X: 10, Y: 10})
	m.Visit(math2d.Vec2{X: 400, Y: 10})
	if err := m.Reveal("tower"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddMarker(worldmap.Marker{ID: "pin-2", Kind: "note", Room: "hall", X: 500, Y: 100, Label: "chest"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddMarker(worldmap.Marker{ID: "pin-1", Kind: "warning", Room: "entrance", X: 100, Y: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestStateIsSortedAndDeterministic(t *testing.T) {
	m := loadCastle(t)
	empty, err := json.Marshal(m.State())
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != `{"version":1,"visited":[]}` {
		t.Fatalf("empty state JSON = %s", empty)
	}
	discoverSome(t, m)
	got, err := json.Marshal(m.State())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"current":"hall","visited":["entrance","hall"],"revealed":["tower"],"markers":[{"id":"pin-1","kind":"warning","room":"entrance","x":100,"y":100},{"id":"pin-2","kind":"note","room":"hall","x":500,"y":100,"label":"chest"}]}`
	if string(got) != want {
		t.Fatalf("state JSON\n got %s\nwant %s", got, want)
	}

	other := loadCastle(t)
	if err := other.Restore(m.State()); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(other.State())
	if string(again) != want {
		t.Fatalf("restored state JSON = %s", again)
	}
}

// TestVisitedRoomsPersistThroughASave is the issue's acceptance check: rooms visited before a save are on the map after loading it into a fresh map.
func TestVisitedRoomsPersistThroughASave(t *testing.T) {
	for _, backend := range []string{"file", "storage"} {
		t.Run(backend, func(t *testing.T) {
			var store *save.Store
			if backend == "file" {
				store = save.NewFileStore(t.TempDir())
			} else {
				nsgotest.Start(t, func(cx *core.Scope) core.Node {
					store = save.NewStorageStore(cx.Storage(), "game")
					return core.View{}
				})
			}

			type slot struct {
				Gold int            `json:"gold"`
				Map  worldmap.State `json:"map"`
			}
			m := loadCastle(t)
			discoverSome(t, m)
			if err := store.Save("slot1", slot{Gold: 12, Map: m.State()}); err != nil {
				t.Fatalf("Save: %v", err)
			}

			var loaded slot
			if err := store.Load("slot1", &loaded); err != nil {
				t.Fatalf("Load: %v", err)
			}
			fresh := loadCastle(t)
			if err := fresh.Restore(loaded.Map); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if !reflect.DeepEqual(fresh.State(), m.State()) {
				t.Fatalf("state after save and load\n got %+v\nwant %+v", fresh.State(), m.State())
			}
			for id, want := range map[string]worldmap.Discovery{"entrance": worldmap.Visited, "hall": worldmap.Visited, "tower": worldmap.Revealed, "cellar": worldmap.Unknown} {
				if got := fresh.Status(id); got != want {
					t.Errorf("Status(%s) = %v, want %v", id, got, want)
				}
			}
			if cur, _ := fresh.Current(); cur != "hall" {
				t.Errorf("Current = %q, want hall", cur)
			}

			// The loaded map draws the same rooms as the one that was saved.
			var before, after core.Drawing
			dst := core.Bounds{Size: core.Size{Width: 200, Height: 100}}
			m.DrawWorldMap(&before, dst, "", nil, nil)
			fresh.DrawWorldMap(&after, dst, "", nil, nil)
			if !reflect.DeepEqual(before.Ops, after.Ops) {
				t.Fatal("restored map draws differently from the saved one")
			}
		})
	}
}

func TestRestoreRefusesBadStatesAtomically(t *testing.T) {
	m := loadCastle(t)
	discoverSome(t, m)
	good := m.State()
	cases := map[string]worldmap.State{
		"version":         {Version: 2, Visited: []string{"hall"}},
		"zero version":    {Visited: []string{"hall"}},
		"unknown visited": {Version: 1, Visited: []string{"attic"}},
		"unknown reveal":  {Version: 1, Revealed: []string{"attic"}},
		"repeated":        {Version: 1, Visited: []string{"hall", "hall"}},
		"both":            {Version: 1, Visited: []string{"hall"}, Revealed: []string{"hall"}},
		"current unknown": {Version: 1, Current: "attic", Visited: []string{"hall"}},
		"current unseen":  {Version: 1, Current: "tower", Visited: []string{"hall"}, Revealed: []string{"tower"}},
		"marker room":     {Version: 1, Markers: []worldmap.Marker{{ID: "p", Kind: "k", Room: "attic"}}},
		"marker fixed id": {Version: 1, Markers: []worldmap.Marker{{ID: "boss", Kind: "k", Room: "hall", X: 400, Y: 10}}},
		"marker repeated": {Version: 1, Markers: []worldmap.Marker{{ID: "p", Kind: "k", Room: "hall", X: 400, Y: 10}, {ID: "p", Kind: "k", Room: "hall", X: 401, Y: 10}}},
	}
	for name, s := range cases {
		if err := m.Restore(s); !errors.Is(err, worldmap.ErrInvalidState) {
			t.Errorf("Restore(%s) = %v, want ErrInvalidState", name, err)
		}
		if !reflect.DeepEqual(m.State(), good) {
			t.Fatalf("Restore(%s) changed the map", name)
		}
	}
}

func TestStateJSONRefusesUnknownFields(t *testing.T) {
	var s worldmap.State
	if err := json.Unmarshal([]byte(`{"version":1,"visited":[],"secret":true}`), &s); !errors.Is(err, worldmap.ErrInvalidState) {
		t.Fatalf("unknown field: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"version":1,"visited":[],"markers":[{"id":"p","kind":"k","room":"hall","x":1,"y":2,"z":3}]}`), &s); err == nil {
		t.Fatal("unknown marker field accepted")
	}
	if err := json.Unmarshal([]byte(`{"version":1,"visited":["hall"]}`), &s); err != nil || !slices.Equal(s.Visited, []string{"hall"}) {
		t.Fatalf("valid state: %v, %+v", err, s)
	}
}

func TestResetForgetsDiscovery(t *testing.T) {
	m := loadCastle(t)
	discoverSome(t, m)
	m.Reset()
	if !reflect.DeepEqual(m.State(), worldmap.State{Version: 1, Visited: []string{}}) {
		t.Fatalf("State after Reset = %+v", m.State())
	}
	if err := m.AddMarker(worldmap.Marker{ID: "pin-1", Kind: "note", Room: "hall", X: 400, Y: 10}); err != nil {
		t.Fatalf("marker ID still taken after Reset: %v", err)
	}
}

func TestDiscoveryString(t *testing.T) {
	for d, want := range map[worldmap.Discovery]string{worldmap.Unknown: "unknown", worldmap.Revealed: "revealed", worldmap.Visited: "visited", 9: "Discovery(9)"} {
		if got := d.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", d, got, want)
		}
	}
}
