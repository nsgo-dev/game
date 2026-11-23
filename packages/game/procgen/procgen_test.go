package procgen_test

import (
	"errors"
	"hash/fnv"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/procgen"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const seeds = 200

func gridHash(g *procgen.Grid) uint64 {
	h := fnv.New64a()
	var buf [4]byte
	for _, v := range g.Cells {
		buf[0], buf[1], buf[2], buf[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
		h.Write(buf[:])
	}
	return h.Sum64()
}

func gridFromText(t *testing.T, text string) *procgen.Grid {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	g := procgen.NewGrid(len(lines[0]), len(lines))
	for y, line := range lines {
		for x, c := range line {
			if c == '.' {
				g.Set(x, y, procgen.Floor)
			}
		}
	}
	return g
}

// assertReachable checks with ai.AStar, independently of Regions, that every passable cell is reachable from the first one.
func assertReachable(t *testing.T, g *procgen.Grid, passable func(uint32) bool) {
	t.Helper()
	grid := ai.GridFunc{Cols: g.Width, Rows: g.Height, Walk: func(c, r int) bool { return passable(g.At(c, r)) }}
	astar := ai.NewAStar()
	start := ai.Cell{Col: -1}
	for y := range g.Height {
		for x := range g.Width {
			if !passable(g.At(x, y)) {
				continue
			}
			if start.Col < 0 {
				start = ai.Cell{Col: x, Row: y}
				continue
			}
			if _, _, ok := astar.Find(grid, start, ai.Cell{Col: x, Row: y}); !ok {
				t.Fatalf("cell (%d,%d) unreachable from (%d,%d):\n%s", x, y, start.Col, start.Row, g)
			}
		}
	}
}

func assertBorderWall(t *testing.T, g *procgen.Grid) {
	t.Helper()
	for x := range g.Width {
		if g.At(x, 0) != procgen.Wall || g.At(x, g.Height-1) != procgen.Wall {
			t.Fatalf("border column %d is open:\n%s", x, g)
		}
	}
	for y := range g.Height {
		if g.At(0, y) != procgen.Wall || g.At(g.Width-1, y) != procgen.Wall {
			t.Fatalf("border row %d is open:\n%s", y, g)
		}
	}
}

func TestRegionsOrderAndConnected(t *testing.T) {
	g := gridFromText(t, `
.#..#
.#..#
##.##
.#...`)
	regions := g.Regions(procgen.IsFloor)
	want := [][]procgen.Point{
		{{2, 0}, {3, 0}, {2, 1}, {3, 1}, {2, 2}, {2, 3}, {3, 3}, {4, 3}},
		{{0, 0}, {0, 1}},
		{{0, 3}},
	}
	if len(regions) != len(want) {
		t.Fatalf("got %d regions, want %d: %v", len(regions), len(want), regions)
	}
	for i := range want {
		if len(regions[i]) != len(want[i]) {
			t.Fatalf("region %d = %v, want %v", i, regions[i], want[i])
		}
		for j := range want[i] {
			if regions[i][j] != want[i][j] {
				t.Fatalf("region %d = %v, want %v", i, regions[i], want[i])
			}
		}
	}
	if g.Connected(procgen.IsFloor) {
		t.Fatal("Connected = true for three regions")
	}
	g.Fill(procgen.Rect{X: 0, Y: 0, W: 2, H: 4}, procgen.Wall)
	if !g.Connected(procgen.IsFloor) {
		t.Fatalf("Connected = false for one region:\n%s", g)
	}
	if !procgen.NewGrid(4, 4).Connected(procgen.IsFloor) {
		t.Fatal("an all-wall grid must count as connected")
	}
	if got := procgen.NewGrid(4, 4).Regions(procgen.IsFloor); got != nil {
		t.Fatalf("Regions of all-wall grid = %v, want nil", got)
	}
}

func TestRegionsDoNotWrapRows(t *testing.T) {
	// The last cell of row 0 and the first of row 1 are adjacent in Cells but not on the grid.
	g := gridFromText(t, `
##.
.##`)
	if g.Connected(procgen.IsFloor) {
		t.Fatal("flood fill wrapped across a row boundary")
	}
}

func TestGridAccessorsAndString(t *testing.T) {
	g := procgen.NewGrid(3, 2)
	g.Set(1, 0, procgen.Floor)
	g.Set(2, 1, 4)
	g.Set(5, 5, procgen.Floor)
	g.Set(0, 1, 1000)
	if g.At(-1, 0) != procgen.Wall || g.At(1, 0) != procgen.Floor || g.At(3, 0) != procgen.Wall {
		t.Fatal("At out of bounds must read Wall")
	}
	if got, want := g.String(), "#.#\n?#c\n"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
	c := g.Clone()
	if !c.Equal(g) {
		t.Fatal("Clone not Equal")
	}
	c.Set(0, 0, procgen.Floor)
	if c.Equal(g) || g.At(0, 0) != procgen.Wall {
		t.Fatal("Clone shares cells")
	}
	if n := procgen.NewGrid(-3, 2); n.Width != 0 || len(n.Cells) != 0 {
		t.Fatal("negative width not clamped")
	}
	r := procgen.Rect{X: 2, Y: 3, W: 4, H: 3}
	if r.Center() != (procgen.Point{X: 3, Y: 4}) || !r.Contains(procgen.Point{X: 5, Y: 5}) || r.Contains(procgen.Point{X: 6, Y: 5}) {
		t.Fatal("Rect Center/Contains")
	}
}

func TestPaintWritesTilemapLayer(t *testing.T) {
	g := gridFromText(t, `
#.#
...`)
	m := tilemap.New(6, 4, 16, 16)
	m.Layers = append(m.Layers, tilemap.TileLayer{Name: "ground", Visible: true})
	g.Paint(m, 0, 4, 3, func(v uint32) uint32 { return 10 + v })
	if got := m.TileAt(0, 4, 3); got != 10 {
		t.Fatalf("TileAt(4,3) = %d, want 10", got)
	}
	if got := m.TileAt(0, 5, 3); got != 11 {
		t.Fatalf("TileAt(5,3) = %d, want 11", got)
	}
	// Row 1 of g falls below the 4-row map and is clipped.
	if got := m.TileAt(0, 0, 0); got != 0 {
		t.Fatalf("TileAt(0,0) = %d, want untouched 0", got)
	}
}

func TestBSPSameSeedSameLevelAndConnected(t *testing.T) {
	cfg := procgen.BSPConfig{Width: 60, Height: 40, Loops: 2}
	distinct := map[uint64]bool{}
	for seed := range uint64(seeds) {
		a, err := procgen.BSP(random.New(seed), cfg)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		b, _ := procgen.BSP(random.New(seed), cfg)
		if !a.Grid.Equal(b.Grid) || len(a.Rooms) != len(b.Rooms) || len(a.Corridors) != len(b.Corridors) {
			t.Fatalf("seed %d: two runs differ", seed)
		}
		if !a.Grid.Connected(procgen.IsFloor) {
			t.Fatalf("seed %d: disconnected:\n%s", seed, a.Grid)
		}
		assertBorderWall(t, a.Grid)
		if len(a.Rooms) < 2 {
			t.Fatalf("seed %d: %d rooms", seed, len(a.Rooms))
		}
		if want := len(a.Rooms) - 1 + cfg.Loops; len(a.Corridors) != want {
			t.Fatalf("seed %d: %d corridors, want %d", seed, len(a.Corridors), want)
		}
		for i, r := range a.Rooms {
			if r.W < 4 || r.H < 4 || r.X < 1 || r.Y < 1 || r.X+r.W > cfg.Width-1 || r.Y+r.H > cfg.Height-1 {
				t.Fatalf("seed %d: room %d %+v out of bounds", seed, i, r)
			}
			for j, o := range a.Rooms[:i] {
				// Rooms keep at least one wall between them: their 1-cell grown boxes must not overlap.
				if r.X <= o.X+o.W && o.X <= r.X+r.W && r.Y <= o.Y+o.H && o.Y <= r.Y+r.H {
					t.Fatalf("seed %d: rooms %d %+v and %d %+v touch", seed, i, r, j, o)
				}
			}
		}
		for _, c := range a.Corridors {
			if !a.Rooms[c.From].Contains(c.Path[0]) || !a.Rooms[c.To].Contains(c.Path[len(c.Path)-1]) {
				t.Fatalf("seed %d: corridor %+v does not join its rooms", seed, c)
			}
		}
		distinct[gridHash(a.Grid)] = true
	}
	if len(distinct) < seeds*9/10 {
		t.Fatalf("only %d distinct levels from %d seeds", len(distinct), seeds)
	}
}

func TestBSPReachableWithAStar(t *testing.T) {
	for seed := range uint64(20) {
		d, err := procgen.BSP(random.New(seed), procgen.BSPConfig{Width: 48, Height: 32, MinLeaf: 8, MinRoom: 3, MaxRoom: 6})
		if err != nil {
			t.Fatal(err)
		}
		assertReachable(t, d.Grid, procgen.IsFloor)
	}
}

func TestBSPGolden(t *testing.T) {
	d, err := procgen.BSP(random.New(531), procgen.BSPConfig{Width: 40, Height: 24})
	if err != nil {
		t.Fatal(err)
	}
	const want = uint64(0x563296e99a085b55)
	if got := gridHash(d.Grid); got != want {
		t.Fatalf("hash = %#x, want %#x; BSP output changed for seed 531:\n%s", got, want, d.Grid)
	}
}

func TestBSPInvalidConfig(t *testing.T) {
	for _, cfg := range []procgen.BSPConfig{
		{Width: 11, Height: 40},
		{Width: 40, Height: 40, MinLeaf: 5},
		{Width: 40, Height: 40, MinRoom: -1},
		{Width: 40, Height: 40, MinRoom: 5, MaxRoom: 4},
		{Width: 40, Height: 40, Padding: -1},
		{Width: 40, Height: 40, Loops: -1},
	} {
		if _, err := procgen.BSP(random.New(1), cfg); !errors.Is(err, procgen.ErrInvalidConfig) {
			t.Errorf("BSP(%+v) error = %v, want ErrInvalidConfig", cfg, err)
		}
	}
}

func TestBSPSmallestLevelHasOneRoom(t *testing.T) {
	d, err := procgen.BSP(random.New(7), procgen.BSPConfig{Width: 12, Height: 12})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rooms) != 1 || len(d.Corridors) != 0 {
		t.Fatalf("rooms %d corridors %d, want 1 and 0", len(d.Rooms), len(d.Corridors))
	}
}

func TestCaveSameSeedSameLevelAndConnected(t *testing.T) {
	for _, join := range []bool{false, true} {
		cfg := procgen.CaveConfig{Width: 64, Height: 40, Join: join, MinRegion: 6}
		distinct := map[uint64]bool{}
		for seed := range uint64(seeds) {
			a, err := procgen.Cave(random.New(seed), cfg)
			if err != nil {
				t.Fatalf("join %v seed %d: %v", join, seed, err)
			}
			b, _ := procgen.Cave(random.New(seed), cfg)
			if !a.Equal(b) {
				t.Fatalf("join %v seed %d: two runs differ", join, seed)
			}
			if !a.Connected(procgen.IsFloor) {
				t.Fatalf("join %v seed %d: disconnected:\n%s", join, seed, a)
			}
			assertBorderWall(t, a)
			if n := a.Count(procgen.IsFloor); n < cfg.Width*cfg.Height/5 {
				t.Fatalf("join %v seed %d: only %d floor cells:\n%s", join, seed, n, a)
			}
			distinct[gridHash(a)] = true
		}
		if len(distinct) < seeds*9/10 {
			t.Fatalf("join %v: only %d distinct levels from %d seeds", join, len(distinct), seeds)
		}
	}
}

func TestCaveJoinKeepsMoreFloor(t *testing.T) {
	more := 0
	for seed := range uint64(12) {
		kept, err := procgen.Cave(random.New(seed), procgen.CaveConfig{Width: 64, Height: 40})
		if err != nil {
			t.Fatal(err)
		}
		joined, err := procgen.Cave(random.New(seed), procgen.CaveConfig{Width: 64, Height: 40, Join: true})
		if err != nil {
			t.Fatal(err)
		}
		k, j := kept.Count(procgen.IsFloor), joined.Count(procgen.IsFloor)
		if j < k {
			t.Fatalf("seed %d: joined cave has %d floor cells, fewer than %d kept", seed, j, k)
		}
		if j > k {
			more++
		}
		// Every floor cell of the kept cave is the largest region, which Join keeps untouched.
		for i, v := range kept.Cells {
			if v == procgen.Floor && joined.Cells[i] != procgen.Floor {
				t.Fatalf("seed %d: Join removed a floor cell of the largest region", seed)
			}
		}
		assertReachable(t, joined, procgen.IsFloor)
	}
	if more == 0 {
		t.Fatal("Join never added a region")
	}
}

func TestCaveGolden(t *testing.T) {
	g, err := procgen.Cave(random.New(531), procgen.CaveConfig{Width: 48, Height: 32, Join: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = uint64(0x8a0a2f1799294d44)
	if got := gridHash(g); got != want {
		t.Fatalf("hash = %#x, want %#x; Cave output changed for seed 531:\n%s", got, want, g)
	}
}

func TestCaveNoFloorAndInvalidConfig(t *testing.T) {
	// A 3x3 cave has one inner cell whose 8 neighbours are the wall border, so smoothing always walls it.
	if _, err := procgen.Cave(random.New(1), procgen.CaveConfig{Width: 3, Height: 3}); !errors.Is(err, procgen.ErrNoFloor) {
		t.Fatalf("error = %v, want ErrNoFloor", err)
	}
	for _, cfg := range []procgen.CaveConfig{
		{Width: 2, Height: 10},
		{Width: 10, Height: 10, WallChance: 1},
		{Width: 10, Height: 10, WallChance: -0.1},
		{Width: 10, Height: 10, Steps: -1},
		{Width: 10, Height: 10, WallAt: 9},
		{Width: 10, Height: 10, WallAt: 3, FloorAt: 3},
		{Width: 10, Height: 10, MinRegion: -1},
	} {
		if _, err := procgen.Cave(random.New(1), cfg); !errors.Is(err, procgen.ErrInvalidConfig) {
			t.Errorf("Cave(%+v) error = %v, want ErrInvalidConfig", cfg, err)
		}
	}
}

func loadRules(t testing.TB) *procgen.Rules {
	t.Helper()
	data, err := os.ReadFile("testdata/ruins.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r, err := procgen.ParseRules(data)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// allowedPairs returns the set of allowed (a, b, vertical) triples of r.
func allowedPairs(r *procgen.Rules) map[[3]int]bool {
	pairs := map[[3]int]bool{}
	for _, n := range r.Neighbors {
		if n.Left != "" {
			pairs[[3]int{r.Index(n.Left), r.Index(n.Right), 0}] = true
		} else {
			pairs[[3]int{r.Index(n.Up), r.Index(n.Down), 1}] = true
		}
	}
	return pairs
}

func TestWFCSameSeedSameLevelAndConnected(t *testing.T) {
	rules := loadRules(t)
	pairs := allowedPairs(rules)
	cfg := procgen.WFCConfig{Width: 32, Height: 20, Rules: rules}
	distinct := map[uint64]bool{}
	for seed := range uint64(seeds) {
		a, err := procgen.WFC(random.New(seed), cfg)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		b, _ := procgen.WFC(random.New(seed), cfg)
		if !a.Equal(b) {
			t.Fatalf("seed %d: two runs differ", seed)
		}
		if !a.Connected(rules.Walkable) || a.Count(rules.Walkable) == 0 {
			t.Fatalf("seed %d: walkable tiles disconnected:\n%s", seed, a)
		}
		for y := range a.Height {
			for x := range a.Width {
				v := int(a.At(x, y))
				if x+1 < a.Width && !pairs[[3]int{v, int(a.At(x+1, y)), 0}] {
					t.Fatalf("seed %d: (%d,%d) breaks a horizontal rule:\n%s", seed, x, y, a)
				}
				if y+1 < a.Height && !pairs[[3]int{v, int(a.At(x, y+1)), 1}] {
					t.Fatalf("seed %d: (%d,%d) breaks a vertical rule:\n%s", seed, x, y, a)
				}
			}
		}
		distinct[gridHash(a)] = true
	}
	if len(distinct) < seeds*9/10 {
		t.Fatalf("only %d distinct levels from %d seeds", len(distinct), seeds)
	}
	g, _ := procgen.WFC(random.New(3), cfg)
	assertReachable(t, g, rules.Walkable)
}

func TestWFCUsesEveryTile(t *testing.T) {
	rules := loadRules(t)
	g, err := procgen.WFC(random.New(9), procgen.WFCConfig{Width: 40, Height: 30, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	for i, tile := range rules.Tiles {
		if g.Count(func(v uint32) bool { return v == uint32(i) }) == 0 {
			t.Fatalf("tile %q never placed:\n%s", tile.Name, g)
		}
	}
}

func TestWFCGolden(t *testing.T) {
	g, err := procgen.WFC(random.New(531), procgen.WFCConfig{Width: 24, Height: 16, Rules: loadRules(t)})
	if err != nil {
		t.Fatal(err)
	}
	const want = uint64(0x4b8edf0eac379806)
	if got := gridHash(g); got != want {
		t.Fatalf("hash = %#x, want %#x; WFC output changed for seed 531:\n%s", got, want, g)
	}
}

func TestWFCNoSolution(t *testing.T) {
	// a and b alternate horizontally but nothing may sit above anything, so any grid taller than one row contradicts.
	rules := &procgen.Rules{
		Version:   1,
		Tiles:     []procgen.TileRule{{Name: "a", Weight: 1, Walkable: true}, {Name: "b", Weight: 1}},
		Neighbors: []procgen.Neighbor{{Left: "a", Right: "b"}, {Left: "b", Right: "a"}},
	}
	if _, err := procgen.WFC(random.New(1), procgen.WFCConfig{Width: 4, Height: 2, Rules: rules, Attempts: 3}); !errors.Is(err, procgen.ErrNoSolution) {
		t.Fatalf("error = %v, want ErrNoSolution", err)
	}
	// A single row solves, but its alternating walkable tiles are separate regions, so it is accepted only with AllowDisconnected.
	if _, err := procgen.WFC(random.New(1), procgen.WFCConfig{Width: 6, Height: 1, Rules: rules, Attempts: 2}); !errors.Is(err, procgen.ErrNoSolution) {
		t.Fatalf("disconnected row error = %v, want ErrNoSolution", err)
	}
	g, err := procgen.WFC(random.New(1), procgen.WFCConfig{Width: 6, Height: 1, Rules: rules, AllowDisconnected: true})
	if err != nil {
		t.Fatalf("AllowDisconnected: %v", err)
	}
	if s := g.String(); s != "#.#.#.\n" && s != ".#.#.#\n" {
		t.Fatalf("single row = %q, want alternating tiles", s)
	}
}

func TestWFCInvalidConfig(t *testing.T) {
	rules := loadRules(t)
	for _, cfg := range []procgen.WFCConfig{
		{Width: 0, Height: 4, Rules: rules},
		{Width: 4, Height: 4},
		{Width: 4, Height: 4, Rules: rules, Attempts: -1},
		{Width: 4, Height: 4, Rules: &procgen.Rules{Version: 2}},
	} {
		if _, err := procgen.WFC(random.New(1), cfg); !errors.Is(err, procgen.ErrInvalidConfig) {
			t.Errorf("WFC(%+v) error = %v, want ErrInvalidConfig", cfg, err)
		}
	}
}

func TestWFCManyTiles(t *testing.T) {
	// 70 tiles spill the bitsets into a second word; each tile may only sit next to itself or its successor.
	rules := &procgen.Rules{Version: 1}
	names := make([]string, 70)
	for i := range names {
		names[i] = "t" + string(rune('A'+i/26)) + string(rune('a'+i%26))
		rules.Tiles = append(rules.Tiles, procgen.TileRule{Name: names[i], Weight: 1, Walkable: true})
	}
	for i, a := range names {
		for _, b := range []string{a, names[(i+1)%len(names)]} {
			rules.Neighbors = append(rules.Neighbors, procgen.Neighbor{Left: a, Right: b}, procgen.Neighbor{Up: a, Down: b})
		}
	}
	g, err := procgen.WFC(random.New(2), procgen.WFCConfig{Width: 12, Height: 12, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	for y := range g.Height {
		for x := range g.Width - 1 {
			a, b := int(g.At(x, y)), int(g.At(x+1, y))
			if b != a && b != (a+1)%len(names) {
				t.Fatalf("(%d,%d): %d next to %d", x, y, a, b)
			}
		}
	}
}
