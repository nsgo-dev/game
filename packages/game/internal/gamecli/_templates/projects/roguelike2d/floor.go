package main

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/procgen"
	"github.com/nsgo-dev/game/packages/game/random"
)

// cell is a position on a floor: X is the column and Y the row, from 0 at the top left.
type cell = procgen.Point

// The four directions a step can take, in the order the monsters try them.
var steps = [4]cell{{X: 0, Y: -1}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: -1, Y: 0}}

// minMonsterDistance is the fewest steps from the player's arrival that a monster spawns, so a floor never starts with a fight.
const minMonsterDistance = 6

// floor is one generated floor of a run: its cells, where the player arrives, its exit, and the world of actors, the monsters and items on it.
type floor struct {
	// index is the floor's place in Dungeon.Floors, from 0.
	index int
	def   *FloorDef
	grid  *procgen.Grid
	// start is where the player arrives.
	start cell
	// exit is the stairs down, or on the last floor the relic.
	exit cell
	// relic reports whether exit is the relic, which wins the run, rather than stairs.
	relic bool
	// actors holds the floor's monsters and items as entities, with the components actors.go defines.
	actors *entities.World
	// seen marks the cells the player has seen, which stay drawn, dimmer, once out of sight.
	seen []bool
}

// floorStream returns the stream floor index of the run with seed is generated from. Each floor has its own, so a resumed run regenerates the floor it was saved on exactly.
func floorStream(seed uint64, index int, purpose string) *random.Stream {
	return random.New(seed).Fork(fmt.Sprintf("%s-%d", purpose, index))
}

// generateFloor builds floor index of the run with seed: it carves the cells with the floor's generator, puts the exit as far from the start as the floor allows, spawns monsters by weight away from the start, and lays out the drops of the floor's loot table, rolled on roller.
func generateFloor(d *Dungeon, index int, seed uint64, roller *loot.Roller) (*floor, error) {
	def := &d.Floors[index]
	s := floorStream(seed, index, "floor")
	f := &floor{index: index, def: def, relic: index == len(d.Floors)-1, actors: newActors()}

	switch def.Generator {
	case generatorBSP:
		dg, err := procgen.BSP(s, procgen.BSPConfig{Width: def.Width, Height: def.Height, MinLeaf: 8, MinRoom: 4, MaxRoom: 10, Loops: 2})
		if err != nil {
			return nil, fmt.Errorf("floor %d: %w", index+1, err)
		}
		f.grid = dg.Grid
		f.start = dg.Rooms[0].Center()
	default:
		g, err := cave(s, def)
		if err != nil {
			return nil, fmt.Errorf("floor %d: %w", index+1, err)
		}
		f.grid = g
		region := g.Regions(procgen.IsFloor)[0]
		f.start = region[s.Intn(len(region))]
	}
	f.seen = make([]bool, len(f.grid.Cells))

	// The exit is the floor cell farthest from the start, the first in row order among ties.
	dist := distances(f.grid, f.start)
	far := 0
	for i, n := range dist {
		if n > dist[far] {
			far = i
		}
	}
	f.exit = cell{X: far % f.grid.Width, Y: far / f.grid.Width}

	// Every other floor cell is a place a monster or an item may go, in an order the stream shuffles.
	var free []cell
	for i, n := range dist {
		c := cell{X: i % f.grid.Width, Y: i / f.grid.Width}
		if n > 0 && c != f.exit {
			free = append(free, c)
		}
	}
	random.Shuffle(s, free)
	taken := map[cell]bool{}

	if def.Guardian != "" {
		m, _ := d.monster(def.Guardian)
		f.spawnFoe(m, f.exit, true)
	}
	kinds := d.spawnable(index + 1)
	weights := make([]float64, len(kinds))
	for i, k := range kinds {
		weights[i] = d.Monsters[k].Weight
	}
	for n := 0; n < def.Monsters; n++ {
		at, ok := takeCell(free, taken, func(c cell) bool { return dist[c.Y*f.grid.Width+c.X] >= minMonsterDistance })
		if !ok {
			break
		}
		f.spawnFoe(&d.Monsters[kinds[s.WeightedIndex(weights)]], at, false)
	}

	var drops []loot.Drop
	for n := 0; n < def.Items; n++ {
		var err error
		if drops, err = roller.AppendRoll(drops[:0], def.Loot); err != nil {
			return nil, fmt.Errorf("floor %d: %w", index+1, err)
		}
		for _, dr := range drops {
			at, ok := takeCell(free, taken, func(cell) bool { return true })
			if !ok {
				break
			}
			f.spawnLoot(dr.Item, dr.Count, at)
		}
	}
	return f, nil
}

// cave grows a cave floor, retrying on a fork of s when the noise smooths into solid rock, as procgen.ErrNoFloor allows.
func cave(s *random.Stream, def *FloorDef) (*procgen.Grid, error) {
	cfg := procgen.CaveConfig{Width: def.Width, Height: def.Height, WallChance: 0.44, MinRegion: 12, Join: true}
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		var g *procgen.Grid
		if g, err = procgen.Cave(s, cfg); err == nil {
			return g, nil
		}
		if !errors.Is(err, procgen.ErrNoFloor) {
			return nil, err
		}
		s = s.Fork("retry")
	}
	return nil, err
}

// takeCell returns the first cell of free that is not taken and passes ok, and marks it taken.
func takeCell(free []cell, taken map[cell]bool, ok func(cell) bool) (cell, bool) {
	for _, c := range free {
		if !taken[c] && ok(c) {
			taken[c] = true
			return c, true
		}
	}
	return cell{}, false
}

// distances returns the number of steps from from to every cell of g, -1 for walls and cells it cannot reach: a breadth-first search over the floor cells, the map the monsters walk down to reach the player.
func distances(g *procgen.Grid, from cell) []int {
	dist := make([]int, len(g.Cells))
	for i := range dist {
		dist[i] = -1
	}
	if !procgen.IsFloor(g.At(from.X, from.Y)) {
		return dist
	}
	queue := []cell{from}
	dist[from.Y*g.Width+from.X] = 0
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, st := range steps {
			n := cell{X: c.X + st.X, Y: c.Y + st.Y}
			if !g.In(n.X, n.Y) || !procgen.IsFloor(g.At(n.X, n.Y)) || dist[n.Y*g.Width+n.X] >= 0 {
				continue
			}
			dist[n.Y*g.Width+n.X] = dist[c.Y*g.Width+c.X] + 1
			queue = append(queue, n)
		}
	}
	return dist
}

// visibleFrom marks the cells within radius of from that a straight line from from reaches without passing through a wall: what the player sees.
func visibleFrom(g *procgen.Grid, from cell, radius int, dst []bool) {
	clear(dst)
	for y := from.Y - radius; y <= from.Y+radius; y++ {
		for x := from.X - radius; x <= from.X+radius; x++ {
			dx, dy := x-from.X, y-from.Y
			if !g.In(x, y) || dx*dx+dy*dy > radius*radius+radius {
				continue
			}
			if lineOfSight(g, from, cell{X: x, Y: y}) {
				dst[y*g.Width+x] = true
			}
		}
	}
}

// lineOfSight reports whether the cells strictly between a and b on a Bresenham line are all floor; b itself may be a wall, so walls bounding a room are seen.
func lineOfSight(g *procgen.Grid, a, b cell) bool {
	dx, dy := abs(b.X-a.X), -abs(b.Y-a.Y)
	sx, sy := sign(b.X-a.X), sign(b.Y-a.Y)
	err := dx + dy
	x, y := a.X, a.Y
	for {
		if x == b.X && y == b.Y {
			return true
		}
		if (x != a.X || y != a.Y) && !procgen.IsFloor(g.At(x, y)) {
			return false
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
}

// walkable reports whether c is a floor cell.
func (f *floor) walkable(c cell) bool { return procgen.IsFloor(f.grid.At(c.X, c.Y)) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}
