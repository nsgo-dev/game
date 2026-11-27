package procgen

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/pkg/random"
)

// ErrInvalidConfig is wrapped by every error a generator returns for a configuration it cannot use.
var ErrInvalidConfig = errors.New("procgen: invalid config")

// MaxSide is the largest Width or Height any generator accepts.
const MaxSide = 1 << 16

// MaxCells is the largest Width*Height any generator accepts. It keeps cell counts within int on 32-bit targets and cell indexes within int32, and bounds a level's memory.
const MaxCells = 1 << 24

// tooManyCells reports whether a width by height level has more than MaxCells cells, without computing a product that could overflow int.
func tooManyCells(width, height int) bool {
	return width > 0 && height > 0 && width > MaxCells/height
}

// BSPConfig configures BSP. Zero fields take the defaults given on each field.
type BSPConfig struct {
	// Width and Height are the level's size in cells. The outermost ring of cells is always Wall, so both must be at least MinLeaf+2 and at most MaxSide, and Width*Height at most MaxCells.
	Width, Height int
	// MinLeaf is the smallest side of a partition, in cells; a partition is split while one of its sides is at least twice MinLeaf. The default is 10, and it must be at least MinRoom+2*Padding.
	MinLeaf int
	// MinRoom is the smallest side of a room, in cells. The default is 4.
	MinRoom int
	// MaxRoom caps a room's side, in cells; 0 leaves rooms as large as their partition allows. When set it must be at least MinRoom.
	MaxRoom int
	// Padding is the number of Wall cells kept between a room and the edges of its partition. The default and minimum is 1, which keeps neighbouring rooms from merging.
	Padding int
	// Loops adds this many extra corridors between randomly chosen rooms on top of the spanning corridors, so the level has cycles. The default is 0.
	Loops int
}

func (c BSPConfig) withDefaults() BSPConfig {
	if c.MinLeaf == 0 {
		c.MinLeaf = 10
	}
	if c.MinRoom == 0 {
		c.MinRoom = 4
	}
	if c.Padding == 0 {
		c.Padding = 1
	}
	return c
}

func (c BSPConfig) validate() error {
	switch {
	case c.Width > MaxSide || c.Height > MaxSide:
		return fmt.Errorf("%w: BSP size %dx%d exceeds MaxSide", ErrInvalidConfig, c.Width, c.Height)
	case tooManyCells(c.Width, c.Height):
		return fmt.Errorf("%w: BSP size %dx%d exceeds MaxCells", ErrInvalidConfig, c.Width, c.Height)
	case c.MinLeaf < 1 || c.MinLeaf > MaxSide:
		return fmt.Errorf("%w: BSP MinLeaf %d is outside [1, MaxSide]", ErrInvalidConfig, c.MinLeaf)
	case c.MinRoom < 1 || c.MinRoom > c.MinLeaf:
		return fmt.Errorf("%w: BSP MinRoom %d is outside [1, MinLeaf]", ErrInvalidConfig, c.MinRoom)
	case c.Padding < 1 || c.Padding > c.MinLeaf:
		return fmt.Errorf("%w: BSP Padding %d is outside [1, MinLeaf]", ErrInvalidConfig, c.Padding)
	case c.MaxRoom != 0 && c.MaxRoom < c.MinRoom:
		return fmt.Errorf("%w: BSP MaxRoom %d is below MinRoom %d", ErrInvalidConfig, c.MaxRoom, c.MinRoom)
	case c.MinLeaf < c.MinRoom+2*c.Padding:
		return fmt.Errorf("%w: BSP MinLeaf %d is below MinRoom+2*Padding (%d)", ErrInvalidConfig, c.MinLeaf, c.MinRoom+2*c.Padding)
	case c.Loops < 0:
		return fmt.Errorf("%w: BSP Loops %d is negative", ErrInvalidConfig, c.Loops)
	case c.Width < c.MinLeaf+2 || c.Height < c.MinLeaf+2:
		return fmt.Errorf("%w: BSP size %dx%d is below MinLeaf+2 (%d)", ErrInvalidConfig, c.Width, c.Height, c.MinLeaf+2)
	}
	return nil
}

// Corridor is a passage BSP carved between two rooms.
type Corridor struct {
	// From and To index the rooms the corridor joins in Dungeon.Rooms.
	From, To int
	// Path lists the corridor's cells in order from the center of room From to the center of room To.
	Path []Point
}

// Dungeon is the result of BSP.
type Dungeon struct {
	// Grid holds Wall and Floor cells.
	Grid *Grid
	// Rooms lists every room in the order of the partition tree's leaves, left or top child first.
	Rooms []Rect
	// Corridors lists the spanning corridors, innermost partitions first, followed by the extra Loops corridors.
	Corridors []Corridor
}

type bspNode struct {
	area        Rect
	left, right *bspNode
	room        int
}

// BSP partitions a level with binary space partitioning, places one room in each leaf partition and joins sibling partitions with L-shaped corridors, so every Floor cell is reachable from every other.
//
// All randomness comes from s, so the same stream state and cfg produce the same Dungeon. It returns an error wrapping ErrInvalidConfig when cfg cannot be satisfied.
func BSP(s *random.Stream, cfg BSPConfig) (*Dungeon, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	d := &Dungeon{Grid: NewGrid(cfg.Width, cfg.Height)}
	root := &bspNode{area: Rect{X: 1, Y: 1, W: cfg.Width - 2, H: cfg.Height - 2}}
	bspSplit(s, root, cfg)
	bspPlaceRooms(s, root, cfg, d)
	bspConnect(s, root, d)
	if len(d.Rooms) >= 2 {
		for range cfg.Loops {
			a := s.Intn(len(d.Rooms))
			b := s.Intn(len(d.Rooms) - 1)
			if b >= a {
				b++
			}
			d.carveCorridor(s, a, b)
		}
	}
	return d, nil
}

func bspSplit(s *random.Stream, n *bspNode, cfg BSPConfig) {
	w, h := n.area.W, n.area.H
	canV, canH := w >= 2*cfg.MinLeaf, h >= 2*cfg.MinLeaf
	if !canV && !canH {
		return
	}
	vertical := canV
	if canV && canH {
		switch {
		case w*4 > h*5:
			vertical = true
		case h*4 > w*5:
			vertical = false
		default:
			vertical = s.Bool()
		}
	}
	a := n.area
	if vertical {
		cut := s.IntRange(cfg.MinLeaf, w-cfg.MinLeaf)
		n.left = &bspNode{area: Rect{X: a.X, Y: a.Y, W: cut, H: h}}
		n.right = &bspNode{area: Rect{X: a.X + cut, Y: a.Y, W: w - cut, H: h}}
	} else {
		cut := s.IntRange(cfg.MinLeaf, h-cfg.MinLeaf)
		n.left = &bspNode{area: Rect{X: a.X, Y: a.Y, W: w, H: cut}}
		n.right = &bspNode{area: Rect{X: a.X, Y: a.Y + cut, W: w, H: h - cut}}
	}
	bspSplit(s, n.left, cfg)
	bspSplit(s, n.right, cfg)
}

func bspPlaceRooms(s *random.Stream, n *bspNode, cfg BSPConfig, d *Dungeon) {
	if n.left != nil {
		bspPlaceRooms(s, n.left, cfg, d)
		bspPlaceRooms(s, n.right, cfg, d)
		return
	}
	side := func(avail int) (offset, size int) {
		hi := avail
		if cfg.MaxRoom > 0 {
			hi = min(hi, cfg.MaxRoom)
		}
		size = s.IntRange(cfg.MinRoom, hi)
		return s.IntRange(0, avail-size), size
	}
	ox, w := side(n.area.W - 2*cfg.Padding)
	oy, h := side(n.area.H - 2*cfg.Padding)
	room := Rect{X: n.area.X + cfg.Padding + ox, Y: n.area.Y + cfg.Padding + oy, W: w, H: h}
	n.room = len(d.Rooms)
	d.Rooms = append(d.Rooms, room)
	d.Grid.Fill(room, Floor)
}

// bspConnect joins the two children of every internal node through their closest pair of rooms and returns the rooms under n.
func bspConnect(s *random.Stream, n *bspNode, d *Dungeon) []int {
	if n.left == nil {
		return []int{n.room}
	}
	left := bspConnect(s, n.left, d)
	right := bspConnect(s, n.right, d)
	bestA, bestB, best := -1, -1, 0
	for _, a := range left {
		ca := d.Rooms[a].Center()
		for _, b := range right {
			cb := d.Rooms[b].Center()
			dist := abs(ca.X-cb.X) + abs(ca.Y-cb.Y)
			if bestA < 0 || dist < best {
				bestA, bestB, best = a, b, dist
			}
		}
	}
	d.carveCorridor(s, bestA, bestB)
	return append(left, right...)
}

// carveCorridor carves an L-shaped Floor path between the centers of rooms a and b, bending horizontally or vertically first at random.
func (d *Dungeon) carveCorridor(s *random.Stream, a, b int) {
	from, to := d.Rooms[a].Center(), d.Rooms[b].Center()
	corner := Point{X: to.X, Y: from.Y}
	if s.Bool() {
		corner = Point{X: from.X, Y: to.Y}
	}
	path := []Point{from}
	for _, target := range [2]Point{corner, to} {
		p := path[len(path)-1]
		for p != target {
			p.X += sign(target.X - p.X)
			p.Y += sign(target.Y - p.Y)
			path = append(path, p)
		}
	}
	for _, p := range path {
		d.Grid.Set(p.X, p.Y, Floor)
	}
	d.Corridors = append(d.Corridors, Corridor{From: a, To: b, Path: path})
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
