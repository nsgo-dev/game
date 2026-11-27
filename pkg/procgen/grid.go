package procgen

import (
	"strings"

	"github.com/nsgo-dev/game/pkg/tilemap"
)

// Cell values written by BSP and Cave.
const (
	// Wall is a solid cell. It is also the value of every cell of a new Grid.
	Wall uint32 = 0
	// Floor is a walkable cell.
	Floor uint32 = 1
)

// Point is a cell position: X is the column and Y the row, both from 0 at the top left.
type Point struct {
	X, Y int
}

// Rect is an axis-aligned block of cells covering columns [X, X+W) and rows [Y, Y+H).
type Rect struct {
	X, Y, W, H int
}

// Center returns the cell at the middle of r, rounded toward its top left.
func (r Rect) Center() Point {
	return Point{X: r.X + (r.W-1)/2, Y: r.Y + (r.H-1)/2}
}

// Contains reports whether p lies inside r.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X < r.X+r.W && p.Y >= r.Y && p.Y < r.Y+r.H
}

// Grid is a Width by Height block of cell values stored row by row in Cells, so the cell at (x, y) is Cells[y*Width+x].
type Grid struct {
	Width  int
	Height int
	Cells  []uint32
}

// NewGrid returns a width by height Grid with every cell set to Wall. Negative sizes are treated as 0.
func NewGrid(width, height int) *Grid {
	width = max(width, 0)
	height = max(height, 0)
	return &Grid{Width: width, Height: height, Cells: make([]uint32, width*height)}
}

// In reports whether (x, y) lies inside g.
func (g *Grid) In(x, y int) bool {
	return x >= 0 && y >= 0 && x < g.Width && y < g.Height
}

// At returns the value at (x, y), or Wall when (x, y) lies outside g.
func (g *Grid) At(x, y int) uint32 {
	if !g.In(x, y) {
		return Wall
	}
	return g.Cells[y*g.Width+x]
}

// Set writes v at (x, y); calls outside g are ignored.
func (g *Grid) Set(x, y int, v uint32) {
	if g.In(x, y) {
		g.Cells[y*g.Width+x] = v
	}
}

// Fill writes v across r clipped to g.
func (g *Grid) Fill(r Rect, v uint32) {
	x0, y0 := max(r.X, 0), max(r.Y, 0)
	x1, y1 := min(r.X+r.W, g.Width), min(r.Y+r.H, g.Height)
	for y := y0; y < y1; y++ {
		row := g.Cells[y*g.Width : (y+1)*g.Width]
		for x := x0; x < x1; x++ {
			row[x] = v
		}
	}
}

// Clone returns a deep copy of g.
func (g *Grid) Clone() *Grid {
	return &Grid{Width: g.Width, Height: g.Height, Cells: append([]uint32(nil), g.Cells...)}
}

// Equal reports whether g and o have the same size and cell values.
func (g *Grid) Equal(o *Grid) bool {
	if g.Width != o.Width || g.Height != o.Height || len(g.Cells) != len(o.Cells) {
		return false
	}
	for i, v := range g.Cells {
		if o.Cells[i] != v {
			return false
		}
	}
	return true
}

// Count returns how many cells satisfy passable.
func (g *Grid) Count(passable func(v uint32) bool) int {
	n := 0
	for _, v := range g.Cells {
		if passable(v) {
			n++
		}
	}
	return n
}

// IsFloor reports whether v is Floor. It is the passable function for grids made by BSP and Cave.
func IsFloor(v uint32) bool { return v == Floor }

// Regions returns the 4-connected regions of cells that satisfy passable, largest first; regions of equal size keep the order of their first cell in row-major order, and the cells of each region are listed in row-major order.
func (g *Grid) Regions(passable func(v uint32) bool) [][]Point {
	labels, sizes := g.label(passable)
	if len(sizes) == 0 {
		return nil
	}
	regions := make([][]Point, len(sizes))
	for i, n := range sizes {
		regions[i] = make([]Point, 0, n)
	}
	for i, l := range labels {
		if l >= 0 {
			regions[l] = append(regions[l], Point{X: i % g.Width, Y: i / g.Width})
		}
	}
	sortRegions(regions)
	return regions
}

// sortRegions orders regions by size descending with a stable insertion sort, keeping first-cell order among equal sizes.
func sortRegions(regions [][]Point) {
	for i := 1; i < len(regions); i++ {
		for j := i; j > 0 && len(regions[j]) > len(regions[j-1]); j-- {
			regions[j], regions[j-1] = regions[j-1], regions[j]
		}
	}
}

// Connected reports whether the cells that satisfy passable form at most one 4-connected region. A grid with no passable cell is connected.
func (g *Grid) Connected(passable func(v uint32) bool) bool {
	_, sizes := g.label(passable)
	return len(sizes) <= 1
}

// label flood-fills g, returning each cell's region index (or -1 for blocked cells) and each region's size, with regions numbered by their first cell in row-major order.
func (g *Grid) label(passable func(v uint32) bool) ([]int32, []int) {
	labels := make([]int32, len(g.Cells))
	for i := range labels {
		labels[i] = -1
	}
	var sizes []int
	var stack []int
	for start, v := range g.Cells {
		if labels[start] >= 0 || !passable(v) {
			continue
		}
		id := int32(len(sizes))
		size := 0
		labels[start] = id
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			var near [4]int
			k := 0
			x, y := i%g.Width, i/g.Width
			if y > 0 {
				near[k] = i - g.Width
				k++
			}
			if x > 0 {
				near[k] = i - 1
				k++
			}
			if x < g.Width-1 {
				near[k] = i + 1
				k++
			}
			if y < g.Height-1 {
				near[k] = i + g.Width
				k++
			}
			for _, n := range near[:k] {
				if labels[n] < 0 && passable(g.Cells[n]) {
					labels[n] = id
					stack = append(stack, n)
				}
			}
		}
		sizes = append(sizes, size)
	}
	return labels, sizes
}

// Paint writes every cell of g into layer of m with its top left at (col, row), mapping each value to a tile GID with gid. Cells that fall outside the layer are skipped, and the layer's static cache is invalidated as tilemap.Tilemap.SetTile does.
func (g *Grid) Paint(m *tilemap.Tilemap, layer, col, row int, gid func(v uint32) uint32) {
	for y := range g.Height {
		for x := range g.Width {
			m.SetTile(layer, col+x, row+y, gid(g.Cells[y*g.Width+x]))
		}
	}
}

// String draws g as text, one line per row: '.' for Floor, '#' for Wall and, for any other value v, the letter 'a'+v-2 up to 'z' and '?' beyond. It is meant for tests and debugging.
func (g *Grid) String() string {
	var b strings.Builder
	b.Grow((g.Width + 1) * g.Height)
	for y := range g.Height {
		for x := range g.Width {
			switch v := g.Cells[y*g.Width+x]; {
			case v == Wall:
				b.WriteByte('#')
			case v == Floor:
				b.WriteByte('.')
			case v-2 < 26:
				b.WriteByte(byte('a' + v - 2))
			default:
				b.WriteByte('?')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
