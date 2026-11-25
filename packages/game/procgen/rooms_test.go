package procgen_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/procgen"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/tilemap"
)

func loadRoomTemplates(tb testing.TB) *procgen.RoomTemplates {
	tb.Helper()
	data, err := os.ReadFile("testdata/dungeon_rooms.yaml")
	if err != nil {
		tb.Fatal(err)
	}
	rt, err := procgen.ParseRoomTemplates(data)
	if err != nil {
		tb.Fatal(err)
	}
	return rt
}

// variableSizeRoomTemplates returns a valid template set mixing different room dimensions,
// rotations, tags, weights and an external .tmj reference for PlacementStitch testing.
func variableSizeRoomTemplates(tb testing.TB) *procgen.RoomTemplates {
	tb.Helper()
	const doc = `version: 1
rooms:
  - id: entry_room
    weight: 1
    tags: [start]
    doors:
      - {side: north, offset: 3}
      - {side: south, offset: 3}
      - {side: east, offset: 2}
      - {side: west, offset: 2}
    tiles:
      - "###.###"
      - "#.....#"
      - "......."
      - "#.....#"
      - "###.###"
  - id: small_hub
    weight: 3
    tags: [combat]
    doors:
      - {side: north, offset: 2}
      - {side: south, offset: 2}
      - {side: east, offset: 2}
      - {side: west, offset: 2}
    tiles:
      - "##.##"
      - "#...#"
      - "....."
      - "#...#"
      - "##.##"
  - id: rect_hall
    weight: 2
    tags: [combat]
    allowRotate: true
    doors:
      - {side: north, offset: 4}
      - {side: south, offset: 4}
      - {side: east, offset: 2}
      - {side: west, offset: 2}
    tiles:
      - "####.####"
      - "#.......#"
      - "........."
      - "#.......#"
      - "####.####"
  - id: key_alcove
    weight: 1
    tags: [key]
    allowRotate: true
    doors:
      - {side: north, offset: 2}
      - {side: south, offset: 2}
      - {side: east, offset: 2}
      - {side: west, offset: 2}
    tiles:
      - "##.##"
      - "#...#"
      - "....."
      - "#...#"
      - "##.##"
  - id: boss_chamber
    weight: 1
    tags: [goal]
    width: 9
    height: 7
    source: rooms/boss_chamber.tmj
    doors:
      - {side: north, offset: 4}
      - {side: south, offset: 4}
      - {side: east, offset: 3}
      - {side: west, offset: 3}
`
	rt, err := procgen.ParseRoomTemplates([]byte(doc))
	if err != nil {
		tb.Fatal(err)
	}
	return rt
}

func assertNoRoomOverlap(t *testing.T, rooms []procgen.PlacedRoom) {
	t.Helper()
	for i := range rooms {
		a := rooms[i].Bounds
		if a.W <= 0 || a.H <= 0 {
			t.Fatalf("room %d has non-positive bounds %+v", i, a)
		}
		for j := i + 1; j < len(rooms); j++ {
			b := rooms[j].Bounds
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Fatalf("rooms %d (%+v) and %d (%+v) overlap", i, a, j, b)
			}
		}
	}
}

func roomFloorSeed(g *procgen.Grid, b procgen.Rect, passable func(uint32) bool) (procgen.Point, bool) {
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			if passable(g.At(x, y)) {
				return procgen.Point{X: x, Y: y}, true
			}
		}
	}
	return procgen.Point{}, false
}

// assertGridLocksSound verifies on the stamped tile Grid itself (independently of the graph)
// that blocking lock k's door cells (and all later locks) still leaves key k's room reachable
// from the start room, while blocking lock k disconnects the goal room from start.
func assertGridLocksSound(t *testing.T, lvl *procgen.RoomLevel, passable func(uint32) bool) {
	t.Helper()
	startPt, ok := roomFloorSeed(lvl.Grid, lvl.Rooms[lvl.Graph.Start].Bounds, passable)
	if !ok {
		t.Fatal("start room has no walkable cell")
	}
	goalPt, ok := roomFloorSeed(lvl.Grid, lvl.Rooms[lvl.Graph.Goal].Bounds, passable)
	if !ok {
		t.Fatal("goal room has no walkable cell")
	}

	lockRank := make(map[string]int, len(lvl.Graph.Locks))
	for i, lk := range lvl.Graph.Locks {
		lockRank[lk.ID] = i
	}

	for k, lk := range lvl.Graph.Locks {
		keyPt, ok := roomFloorSeed(lvl.Grid, lvl.Rooms[lk.KeyNode].Bounds, passable)
		if !ok {
			t.Fatalf("key room %d has no walkable cell", lk.KeyNode)
		}
		// Copy grid and wall off lock k and all subsequent locks.
		g := lvl.Grid.Clone()
		for _, d := range lvl.Doors {
			if d.LockID == "" {
				continue
			}
			if lockRank[d.LockID] >= k {
				for _, p := range d.Cells {
					g.Set(p.X, p.Y, procgen.Wall)
				}
			}
		}
		if !gridReachable(g, startPt, keyPt, passable) {
			t.Fatalf("grid check: key %q (room %d) unreachable from start without crossing lock >= %d", lk.ID, lk.KeyNode, k)
		}
		if gridReachable(g, startPt, goalPt, passable) {
			t.Fatalf("grid check: goal room reachable from start while lock %q is sealed (bypass leak)", lk.ID)
		}
	}
}

func gridReachable(g *procgen.Grid, from, to procgen.Point, passable func(uint32) bool) bool {
	if from == to {
		return true
	}
	seen := make([]bool, g.Width*g.Height)
	seen[from.Y*g.Width+from.X] = true
	queue := []procgen.Point{from}
	dirs := [4]procgen.Point{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		for _, d := range dirs {
			np := procgen.Point{X: p.X + d.X, Y: p.Y + d.Y}
			if !g.In(np.X, np.Y) {
				continue
			}
			idx := np.Y*g.Width + np.X
			if seen[idx] || !passable(g.At(np.X, np.Y)) {
				continue
			}
			if np == to {
				return true
			}
			seen[idx] = true
			queue = append(queue, np)
		}
	}
	return false
}

func TestRoomTemplatesRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/dungeon_rooms.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := procgen.ParseRoomTemplates(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.Rooms) != 5 {
		t.Fatalf("parsed %d rooms, want 5", len(rt.Rooms))
	}
	if rt.ByID("boss_sanctum") == nil || rt.ByID("boss_sanctum").Source != "rooms/boss_sanctum.tmj" {
		t.Fatalf("boss_sanctum = %+v", rt.ByID("boss_sanctum"))
	}
	if rt.ByID("missing") != nil {
		t.Fatal("ByID(missing) != nil")
	}

	out, err := procgen.EncodeRoomTemplates(rt)
	if err != nil {
		t.Fatal(err)
	}
	rt2, err := procgen.ParseRoomTemplates(out)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := procgen.EncodeRoomTemplates(rt2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatalf("encode is not stable:\n%s\n---\n%s", out, out2)
	}
	if !strings.HasPrefix(string(out), "version: 1\nrooms:\n  - id: entry\n") {
		t.Fatalf("unexpected canonical output prefix:\n%s", out)
	}

	// Round-tripped templates drive GenerateRooms to the identical level.
	cfg := procgen.RoomsConfig{
		Templates: rt,
		Mode:      procgen.PlacementGrid,
		GridCols:  4,
		GridRows:  4,
		Graph:     procgen.GraphConfig{CriticalPath: 6, Branches: 3, Loops: 1, Locks: 2},
	}
	a, err := procgen.GenerateRooms(random.New(77), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Templates = rt2
	b, err := procgen.GenerateRooms(random.New(77), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Grid.Equal(b.Grid) || !reflect.DeepEqual(a.Rooms, b.Rooms) || !reflect.DeepEqual(a.Doors, b.Doors) || !reflect.DeepEqual(a.Graph, b.Graph) {
		t.Fatal("round-tripped room templates produced a different level")
	}
}

func TestParseRoomTemplatesRejects(t *testing.T) {
	const validRoom = "  - id: r1\n    doors:\n      - {side: north, offset: 1}\n    tiles:\n      - '#.#'\n      - '...'\n      - '###'\n"
	for name, doc := range map[string]string{
		"empty":             "  \n",
		"no version":        "rooms:\n" + validRoom,
		"bad version":       "version: 2\nrooms:\n" + validRoom,
		"unknown root":      "version: 1\nseed: 5\nrooms:\n" + validRoom,
		"unknown room":      "version: 1\nrooms:\n  - id: r1\n    extra: true\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"unknown door":      "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 1, locked: true}]\n    tiles: ['#.#', '...', '###']\n",
		"no rooms":          "version: 1\nrooms: []\n",
		"no id":             "version: 1\nrooms:\n  - doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"duplicate id":      "version: 1\nrooms:\n" + validRoom + validRoom,
		"negative weight":   "version: 1\nrooms:\n  - id: r1\n    weight: -1\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"nan weight":        "version: 1\nrooms:\n  - id: r1\n    weight: .nan\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"inf weight":        "version: 1\nrooms:\n  - id: r1\n    weight: .inf\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"empty tag":         "version: 1\nrooms:\n  - id: r1\n    tags: ['']\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"dup tag":           "version: 1\nrooms:\n  - id: r1\n    tags: [a, a]\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"neither tiles src": "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 1}]\n",
		"both tiles src":    "version: 1\nrooms:\n  - id: r1\n    source: a.tmj\n    width: 3\n    height: 3\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"src no size":       "version: 1\nrooms:\n  - id: r1\n    source: a.tmj\n    doors: [{side: north, offset: 1}]\n",
		"width mismatch":    "version: 1\nrooms:\n  - id: r1\n    width: 4\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"ragged tiles":      "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '..', '###']\n",
		"bad tile char":     "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 1}]\n    tiles: ['#.#', '.?.', '###']\n",
		"no doors":          "version: 1\nrooms:\n  - id: r1\n    doors: []\n    tiles: ['#.#', '...', '###']\n",
		"bad side":          "version: 1\nrooms:\n  - id: r1\n    doors: [{side: up, offset: 1}]\n    tiles: ['#.#', '...', '###']\n",
		"door out of bound": "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 3}]\n    tiles: ['#.#', '...', '###']\n",
		"door neg width":    "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 1, width: -1}]\n    tiles: ['#.#', '...', '###']\n",
		"overlap doors":     "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 0, width: 2}, {side: north, offset: 1}]\n    tiles: ['...', '...', '###']\n",
		"disconnected room": "version: 1\nrooms:\n  - id: r1\n    doors: [{side: north, offset: 0}]\n    tiles: ['.#.', '###', '...']\n",
		"trailing doc":      "version: 1\nrooms:\n" + validRoom + "---\nversion: 1\n",
	} {
		if _, err := procgen.ParseRoomTemplates([]byte(doc)); err == nil {
			t.Errorf("%s: ParseRoomTemplates accepted:\n%s", name, doc)
		}
	}
}

func TestGenerateGraph1000SeedsLockKeyReachability(t *testing.T) {
	cfg := procgen.GraphConfig{
		CriticalPath:   7,
		Branches:       5,
		BranchMaxDepth: 2,
		Loops:          3,
		Locks:          3,
		MaxDegree:      4,
	}
	sawLoop := false
	for seed := range uint64(1000) {
		g, err := procgen.GenerateGraph(random.New(seed), cfg)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if len(g.CriticalPath) != cfg.CriticalPath {
			t.Fatalf("seed %d: CriticalPath len = %d, want %d", seed, len(g.CriticalPath), cfg.CriticalPath)
		}
		if len(g.Locks) != cfg.Locks {
			t.Fatalf("seed %d: Locks len = %d, want %d", seed, len(g.Locks), cfg.Locks)
		}
		if err := g.VerifyLocks(); err != nil {
			t.Fatalf("seed %d: VerifyLocks: %v", seed, err)
		}
		// Determinism check against a second stream with the same seed.
		g2, err := procgen.GenerateGraph(random.New(seed), cfg)
		if err != nil {
			t.Fatalf("seed %d (repeat): %v", seed, err)
		}
		if !reflect.DeepEqual(g, g2) {
			t.Fatalf("seed %d: GenerateGraph is non-deterministic", seed)
		}
		// Check MaxDegree and same-tier loop invariant.
		deg := make([]int, len(g.Nodes))
		for _, e := range g.Edges {
			deg[e.From]++
			deg[e.To]++
			if e.Loop {
				sawLoop = true
				if g.Nodes[e.From].Tier != g.Nodes[e.To].Tier {
					t.Fatalf("seed %d: loop edge (%d,%d) crosses lock tiers %d != %d", seed, e.From, e.To, g.Nodes[e.From].Tier, g.Nodes[e.To].Tier)
				}
			}
		}
		for id, d := range deg {
			if d > cfg.MaxDegree {
				t.Fatalf("seed %d: node %d degree %d exceeds MaxDegree %d", seed, id, d, cfg.MaxDegree)
			}
		}
	}
	if !sawLoop {
		t.Fatal("expected loops to be generated across 1000 seeds")
	}
}

func TestGenerateRoomsStitch1000Seeds(t *testing.T) {
	rt := variableSizeRoomTemplates(t)
	cfg := procgen.RoomsConfig{
		Templates: rt,
		Mode:      procgen.PlacementStitch,
		Graph: procgen.GraphConfig{
			CriticalPath: 6,
			Branches:     4,
			Loops:        2,
			Locks:        2,
		},
	}
	distinct := map[uint64]bool{}
	sawRotation := false
	sawSource := false
	sawLoop := false
	for seed := range uint64(1000) {
		lvl, err := procgen.GenerateRooms(random.New(seed), cfg)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if !lvl.Grid.Connected(procgen.IsFloor) {
			t.Fatalf("seed %d: Grid is disconnected:\n%s", seed, lvl.Grid)
		}
		assertNoRoomOverlap(t, lvl.Rooms)
		if err := lvl.Graph.VerifyLocks(); err != nil {
			t.Fatalf("seed %d: VerifyLocks: %v", seed, err)
		}
		assertGridLocksSound(t, lvl, procgen.IsFloor)
		if len(lvl.Rooms) != len(lvl.Graph.Nodes) || len(lvl.Doors) != len(lvl.Graph.Edges) {
			t.Fatalf("seed %d: rooms/doors count mismatch", seed)
		}
		if lvl.Rooms[lvl.Graph.Start].TemplateID != "entry_room" {
			t.Fatalf("seed %d: start template = %q, want entry_room", seed, lvl.Rooms[lvl.Graph.Start].TemplateID)
		}
		if lvl.Rooms[lvl.Graph.Goal].TemplateID != "boss_chamber" {
			t.Fatalf("seed %d: goal template = %q, want boss_chamber", seed, lvl.Rooms[lvl.Graph.Goal].TemplateID)
		}
		for _, r := range lvl.Rooms {
			if r.Rotation != 0 {
				sawRotation = true
			}
			if r.Source != "" {
				sawSource = true
			}
		}
		for _, e := range lvl.Graph.Edges {
			if e.Loop {
				sawLoop = true
			}
		}
		distinct[gridHash(lvl.Grid)] = true

		// Check determinism on first 50 seeds to keep test fast while thorough.
		if seed < 50 {
			lvl2, err := procgen.GenerateRooms(random.New(seed), cfg)
			if err != nil {
				t.Fatalf("seed %d (repeat): %v", seed, err)
			}
			if !lvl.Grid.Equal(lvl2.Grid) || !reflect.DeepEqual(lvl.Rooms, lvl2.Rooms) || !reflect.DeepEqual(lvl.Doors, lvl2.Doors) || !reflect.DeepEqual(lvl.Graph, lvl2.Graph) {
				t.Fatalf("seed %d: PlacementStitch is non-deterministic", seed)
			}
		}
	}
	if len(distinct) < 500 {
		t.Fatalf("too few distinct stitched dungeons: %d", len(distinct))
	}
	if !sawRotation || !sawSource || !sawLoop {
		t.Fatalf("expected rotations (%v), source templates (%v), and loops (%v) across 1000 seeds", sawRotation, sawSource, sawLoop)
	}
}

func TestGenerateRoomsGridSpelunky1000Seeds(t *testing.T) {
	rt := loadRoomTemplates(t)
	passable := func(v uint32) bool { return v != procgen.Wall }
	cfg := procgen.RoomsConfig{
		Templates: rt,
		Mode:      procgen.PlacementGrid,
		GridCols:  4,
		GridRows:  4,
		Graph: procgen.GraphConfig{
			CriticalPath: 6,
			Branches:     4,
			Loops:        2,
			Locks:        2,
		},
		Passable: passable,
	}
	distinct := map[uint64]bool{}
	sawLoop := false
	for seed := range uint64(1000) {
		lvl, err := procgen.GenerateRooms(random.New(seed), cfg)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if !lvl.Grid.Connected(passable) {
			t.Fatalf("seed %d: Grid is disconnected:\n%s", seed, lvl.Grid)
		}
		assertNoRoomOverlap(t, lvl.Rooms)
		if err := lvl.Graph.VerifyLocks(); err != nil {
			t.Fatalf("seed %d: VerifyLocks: %v", seed, err)
		}
		assertGridLocksSound(t, lvl, passable)

		// Verify all grid slots are unique, inside [0, 4) x [0, 4), and Start is on row 0.
		if lvl.Rooms[lvl.Graph.Start].GridSlot.Y != 0 {
			t.Fatalf("seed %d: Start slot Y = %d, want 0", seed, lvl.Rooms[lvl.Graph.Start].GridSlot.Y)
		}
		seenSlot := map[procgen.Point]bool{}
		for _, r := range lvl.Rooms {
			if r.GridSlot.X < 0 || r.GridSlot.X >= 4 || r.GridSlot.Y < 0 || r.GridSlot.Y >= 4 {
				t.Fatalf("seed %d: slot %+v out of 4x4 grid", seed, r.GridSlot)
			}
			if seenSlot[r.GridSlot] {
				t.Fatalf("seed %d: duplicate grid slot %+v", seed, r.GridSlot)
			}
			seenSlot[r.GridSlot] = true
		}
		for _, e := range lvl.Graph.Edges {
			if e.Loop {
				sawLoop = true
			}
			a := lvl.Rooms[e.From].GridSlot
			b := lvl.Rooms[e.To].GridSlot
			dx := a.X - b.X
			if dx < 0 {
				dx = -dx
			}
			dy := a.Y - b.Y
			if dy < 0 {
				dy = -dy
			}
			if dx+dy != 1 {
				t.Fatalf("seed %d: edge (%d,%d) connects non-adjacent slots %+v and %+v", seed, e.From, e.To, a, b)
			}
		}
		distinct[gridHash(lvl.Grid)] = true

		if seed < 50 {
			lvl2, err := procgen.GenerateRooms(random.New(seed), cfg)
			if err != nil {
				t.Fatalf("seed %d (repeat): %v", seed, err)
			}
			if !lvl.Grid.Equal(lvl2.Grid) || !reflect.DeepEqual(lvl.Rooms, lvl2.Rooms) || !reflect.DeepEqual(lvl.Doors, lvl2.Doors) || !reflect.DeepEqual(lvl.Graph, lvl2.Graph) {
				t.Fatalf("seed %d: PlacementGrid is non-deterministic", seed)
			}
		}
	}
	if len(distinct) < 500 {
		t.Fatalf("too few distinct grid dungeons: %d", len(distinct))
	}
	if !sawLoop {
		t.Fatal("expected loops in PlacementGrid mode")
	}
}

func TestPlaceRoomsPrebuiltGraphBothModes(t *testing.T) {
	rt := loadRoomTemplates(t)
	passable := func(v uint32) bool { return v != procgen.Wall }
	graphCfg := procgen.GraphConfig{
		CriticalPath: 5,
		Branches:     3,
		Loops:        1,
		Locks:        1,
	}
	for _, mode := range []procgen.PlacementMode{procgen.PlacementStitch, procgen.PlacementGrid} {
		t.Run(string(mode), func(t *testing.T) {
			for seed := range uint64(50) {
				g, err := procgen.GenerateGraph(random.New(seed), graphCfg)
				if err != nil {
					t.Fatalf("seed %d: GenerateGraph: %v", seed, err)
				}
				lvl, err := procgen.PlaceRooms(random.New(seed+1000), g, rt, procgen.PlaceConfig{
					Mode:     mode,
					Padding:  1,
					Passable: passable,
				})
				if err != nil {
					t.Fatalf("seed %d: PlaceRooms(%s): %v", seed, mode, err)
				}
				if !lvl.Grid.Connected(passable) {
					t.Fatalf("seed %d: disconnected", seed)
				}
				assertNoRoomOverlap(t, lvl.Rooms)
				assertBorderWall(t, lvl.Grid)
				assertGridLocksSound(t, lvl, passable)
			}
		})
	}
}

func TestGridFromTilemapAndResolveGrid(t *testing.T) {
	// Build a 7x7 tilemap representing rooms/boss_sanctum.tmj with GID 1 = wall, GID 2 = floor.
	tm := tilemap.New(7, 7, 16, 16)
	tm.Layers = append(tm.Layers, tilemap.TileLayer{Name: "tiles", Visible: true})
	for y := range 7 {
		for x := range 7 {
			gid := uint32(2)
			if x == 0 || y == 0 || x == 6 || y == 6 || (x == 3 && y == 3) {
				gid = 1
			}
			tm.SetTile(0, x, y, gid)
		}
	}
	bossGrid := procgen.GridFromTilemap(tm, 0, func(gid uint32) bool { return gid == 2 })
	if bossGrid.At(3, 3) != procgen.Wall || bossGrid.At(2, 2) != procgen.Floor {
		t.Fatalf("unexpected GridFromTilemap:\n%s", bossGrid)
	}

	rt := loadRoomTemplates(t)
	resolved := false
	lvl, err := procgen.GenerateRooms(random.New(42), procgen.RoomsConfig{
		Templates: rt,
		Mode:      procgen.PlacementGrid,
		GridCols:  4,
		GridRows:  4,
		Graph:     procgen.GraphConfig{CriticalPath: 5, Branches: 2, Locks: 1},
		ResolveGrid: func(tmpl *procgen.RoomTemplate) (*procgen.Grid, error) {
			if tmpl.Source == "rooms/boss_sanctum.tmj" {
				resolved = true
				return bossGrid, nil
			}
			return nil, errors.New("unexpected source: " + tmpl.Source)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved {
		t.Fatal("ResolveGrid was not invoked for boss_sanctum.tmj")
	}
	// Verify the center pillar (3, 3) from bossGrid was stamped into the goal room.
	goal := lvl.Rooms[lvl.Graph.Goal]
	if lvl.Grid.At(goal.Bounds.X+3, goal.Bounds.Y+3) != procgen.Wall {
		t.Fatalf("expected center pillar from resolved .tmj in goal room at (%d,%d)", goal.Bounds.X+3, goal.Bounds.Y+3)
	}
}

func TestRoomDungeonInvalidConfigAndNoLayout(t *testing.T) {
	rt := loadRoomTemplates(t)
	for name, cfg := range map[string]procgen.RoomsConfig{
		"nil templates":      {Templates: nil},
		"critical < 2":       {Templates: rt, Graph: procgen.GraphConfig{CriticalPath: 1}},
		"locks >= critical":  {Templates: rt, Graph: procgen.GraphConfig{CriticalPath: 4, Locks: 4}},
		"neg branches":       {Templates: rt, Graph: procgen.GraphConfig{Branches: -1}},
		"neg loops":          {Templates: rt, Graph: procgen.GraphConfig{Loops: -1}},
		"bad mode":           {Templates: rt, Mode: "hex"},
		"half grid size":     {Templates: rt, Mode: procgen.PlacementGrid, GridCols: 4},
		"grid too small":     {Templates: rt, Mode: procgen.PlacementGrid, GridCols: 2, GridRows: 2, Graph: procgen.GraphConfig{CriticalPath: 5}},
		"non-uniform grid":   {Templates: variableSizeRoomTemplates(t), Mode: procgen.PlacementGrid},
		"neg padding":        {Templates: rt, Padding: -1},
		"max degree too low": {Templates: rt, Graph: procgen.GraphConfig{CriticalPath: 4, Branches: 1, MaxDegree: 2}},
	} {
		if _, err := procgen.GenerateRooms(random.New(1), cfg); !errors.Is(err, procgen.ErrInvalidConfig) {
			t.Errorf("%s: got %v, want ErrInvalidConfig", name, err)
		}
	}

	// Incompatible door channels make stitching impossible -> ErrNoLayout.
	const unconnectable = `version: 1
rooms:
  - id: only_a
    doors:
      - {side: east, offset: 1, channel: red}
      - {side: west, offset: 1, channel: blue}
    tiles:
      - "###"
      - "..."
      - "###"
`
	badRT, err := procgen.ParseRoomTemplates([]byte(unconnectable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := procgen.GenerateRooms(random.New(1), procgen.RoomsConfig{
		Templates: badRT,
		Attempts:  5,
		Graph:     procgen.GraphConfig{CriticalPath: 3},
	}); !errors.Is(err, procgen.ErrNoLayout) {
		t.Fatalf("unconnectable channels: got %v, want ErrNoLayout", err)
	}
}
