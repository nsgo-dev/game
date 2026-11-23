package procgen

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/random"
)

// ErrNoFloor is returned when a generator ends with no walkable cell, for example a cave whose noise smoothed into solid rock. Retrying with another seed usually succeeds.
var ErrNoFloor = errors.New("procgen: generated level has no floor")

// CaveConfig configures Cave. Zero fields take the defaults given on each field.
type CaveConfig struct {
	// Width and Height are the level's size in cells, from 3 to MaxSide each, with Width*Height at most MaxCells. The outermost ring of cells is always Wall.
	Width, Height int
	// WallChance is the probability, in (0, 1), that an inner cell starts as Wall. The default is 0.45.
	WallChance float64
	// Steps is the number of smoothing passes. Zero takes the default of 5, so a cave always gets at least one pass.
	Steps int
	// WallAt turns a cell into Wall when at least this many of its 8 neighbours are Wall, cells outside the grid counting as Wall. It must be in [1, 8]. The default is 5.
	WallAt int
	// FloorAt turns a cell into Floor when at most this many of its 8 neighbours are Wall; a cell between FloorAt and WallAt keeps its value. It must be in [0, WallAt). When WallAt is 0 and FloorAt is 0, both take their defaults, 5 and 3; once WallAt is set, FloorAt is used as given, so 0 means only cells with no Wall neighbour become Floor.
	FloorAt int
	// MinRegion fills with Wall every separate cave region of fewer cells than this before regions are joined. The largest region is always kept. The default is 0, which keeps every region.
	MinRegion int
	// Join connects every remaining region to the largest with the shortest tunnel through rock. When false, every region but the largest is filled with Wall instead.
	Join bool
}

func (c CaveConfig) withDefaults() CaveConfig {
	if c.WallChance == 0 {
		c.WallChance = 0.45
	}
	if c.Steps == 0 {
		c.Steps = 5
	}
	// FloorAt 0 is a valid threshold, so it takes its default only together with WallAt.
	if c.WallAt == 0 && c.FloorAt == 0 {
		c.FloorAt = 3
	}
	if c.WallAt == 0 {
		c.WallAt = 5
	}
	return c
}

func (c CaveConfig) validate() error {
	switch {
	case c.Width < 3 || c.Height < 3 || c.Width > MaxSide || c.Height > MaxSide:
		return fmt.Errorf("%w: cave size %dx%d is outside [3, MaxSide]", ErrInvalidConfig, c.Width, c.Height)
	case tooManyCells(c.Width, c.Height):
		return fmt.Errorf("%w: cave size %dx%d exceeds MaxCells", ErrInvalidConfig, c.Width, c.Height)
	case math.IsNaN(c.WallChance) || c.WallChance <= 0 || c.WallChance >= 1:
		return fmt.Errorf("%w: cave WallChance %v is outside (0, 1)", ErrInvalidConfig, c.WallChance)
	case c.Steps < 0:
		return fmt.Errorf("%w: cave Steps %d is negative", ErrInvalidConfig, c.Steps)
	case c.WallAt < 1 || c.WallAt > 8:
		return fmt.Errorf("%w: cave WallAt %d is outside [1, 8]", ErrInvalidConfig, c.WallAt)
	case c.FloorAt < 0 || c.FloorAt >= c.WallAt:
		return fmt.Errorf("%w: cave FloorAt %d is outside [0, WallAt)", ErrInvalidConfig, c.FloorAt)
	case c.MinRegion < 0:
		return fmt.Errorf("%w: cave MinRegion %d is negative", ErrInvalidConfig, c.MinRegion)
	}
	return nil
}

// Cave grows a cave with a cellular automaton: inner cells start as Wall with probability WallChance, Steps smoothing passes apply the WallAt and FloorAt rule, and the resulting regions are then reduced to one connected cave, either by keeping only the largest or, with Join, by tunnelling every region into it.
//
// All randomness comes from s, so the same stream state and cfg produce the same Grid. It returns an error wrapping ErrInvalidConfig for an unusable cfg, and ErrNoFloor when smoothing leaves no Floor cell.
func Cave(s *random.Stream, cfg CaveConfig) (*Grid, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	g := NewGrid(cfg.Width, cfg.Height)
	for y := 1; y < g.Height-1; y++ {
		for x := 1; x < g.Width-1; x++ {
			if !s.Chance(cfg.WallChance) {
				g.Cells[y*g.Width+x] = Floor
			}
		}
	}
	next := g.Clone()
	for range cfg.Steps {
		for y := 1; y < g.Height-1; y++ {
			for x := 1; x < g.Width-1; x++ {
				n := wallNeighbours(g, x, y)
				v := g.Cells[y*g.Width+x]
				switch {
				case n >= cfg.WallAt:
					v = Wall
				case n <= cfg.FloorAt:
					v = Floor
				}
				next.Cells[y*g.Width+x] = v
			}
		}
		g, next = next, g
	}

	regions := g.Regions(IsFloor)
	if len(regions) == 0 {
		return nil, ErrNoFloor
	}
	kept := regions[:1]
	for _, r := range regions[1:] {
		if cfg.Join && len(r) >= cfg.MinRegion {
			kept = append(kept, r)
			continue
		}
		for _, p := range r {
			g.Cells[p.Y*g.Width+p.X] = Wall
		}
	}
	if len(kept) > 1 {
		joinRegions(g, kept)
	}
	return g, nil
}

// wallNeighbours counts the Wall cells among the 8 neighbours of (x, y), cells outside g counting as Wall.
func wallNeighbours(g *Grid, x, y int) int {
	n := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if (dx != 0 || dy != 0) && g.At(x+dx, y+dy) == Wall {
				n++
			}
		}
	}
	return n
}

// joinRegions tunnels each of regions[1:] into the growing set that starts as regions[0], each time along a shortest 4-connected path through inner cells found by breadth-first search from the region's cells.
func joinRegions(g *Grid, regions [][]Point) {
	const (
		unseen = -1
		source = -2
	)
	main := make([]bool, len(g.Cells))
	for _, p := range regions[0] {
		main[p.Y*g.Width+p.X] = true
	}
	parent := make([]int, len(g.Cells))
	var queue []int
	for _, r := range regions[1:] {
		for i := range parent {
			parent[i] = unseen
		}
		queue = queue[:0]
		for _, p := range r {
			i := p.Y*g.Width + p.X
			parent[i] = source
			queue = append(queue, i)
		}
		hit := -1
		for head := 0; head < len(queue) && hit < 0; head++ {
			i := queue[head]
			x, y := i%g.Width, i/g.Width
			for _, d := range [4]Point{{0, -1}, {-1, 0}, {1, 0}, {0, 1}} {
				nx, ny := x+d.X, y+d.Y
				if nx < 1 || ny < 1 || nx >= g.Width-1 || ny >= g.Height-1 {
					continue
				}
				n := ny*g.Width + nx
				if parent[n] != unseen {
					continue
				}
				parent[n] = i
				if main[n] {
					hit = n
					break
				}
				queue = append(queue, n)
			}
		}
		// Every inner cell is reachable from any other, and main is non-empty, so hit is always found.
		for i := hit; i != source; i = parent[i] {
			g.Cells[i] = Floor
			main[i] = true
		}
		for _, p := range r {
			main[p.Y*g.Width+p.X] = true
		}
	}
}
