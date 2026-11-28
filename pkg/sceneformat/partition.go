package sceneformat

import (
	"fmt"
	"math"
	"sort"
)

// Partition describes the deterministic grid used to stream a large scene: square cells of CellSize world units, loaded within LoadRadius cells of a streaming source and kept until a source is more than UnloadRadius cells away.
type Partition struct {
	CellSize     float64 `yaml:"cellSize"`
	LoadRadius   int     `yaml:"loadRadius,omitempty"`
	UnloadRadius int     `yaml:"unloadRadius,omitempty"`
}

// CellCoord is the integer coordinate of a partition cell; cell (0, 0) covers [0, CellSize) on both axes.
type CellCoord struct{ X, Y int }

// StreamingSource is a position cells stream around, such as the player or a teleport destination. RadiusScale multiplies the partition's radii; zero or less means 1.
type StreamingSource struct {
	X, Y        float64
	RadiusScale float64
}

// CellObject is the bounds of an object to assign to cells: a rectangle of Width by Height centered on (X, Y). An AlwaysLoaded object belongs to no cell and stays loaded whatever the sources.
type CellObject struct {
	ID            string
	X, Y          float64
	Width, Height float64
	AlwaysLoaded  bool
}

// Validate reports whether the cell size is a positive finite number and the radii satisfy 0 <= LoadRadius <= UnloadRadius, so a source on a cell boundary does not load and unload the same cell every frame.
func (p Partition) Validate() error {
	if !(p.CellSize > 0) || math.IsInf(p.CellSize, 0) {
		return fmt.Errorf("sceneformat: partition cellSize must be a positive finite number, got %v", p.CellSize)
	}
	if p.LoadRadius < 0 {
		return fmt.Errorf("sceneformat: partition loadRadius must not be negative, got %d", p.LoadRadius)
	}
	if p.UnloadRadius < p.LoadRadius {
		return fmt.Errorf("sceneformat: partition unloadRadius %d is smaller than loadRadius %d", p.UnloadRadius, p.LoadRadius)
	}
	return nil
}

// Coord returns the cell containing the point (x, y).
func (p Partition) Coord(x, y float64) CellCoord {
	return CellCoord{int(math.Floor(x / p.CellSize)), int(math.Floor(y / p.CellSize))}
}

// Cells assigns an object to every cell its bounds touch, in row-major order (Y, then X), so an object larger than one cell is never attached only by its origin. A bounds edge lying exactly on a cell boundary does not touch the next cell; a zero-size object belongs to the cell containing its position. It returns nil for an AlwaysLoaded object, for an invalid partition, and for non-finite or negative bounds.
func (p Partition) Cells(o CellObject) []CellCoord {
	if o.AlwaysLoaded || p.Validate() != nil || !finite(o.X, o.Y, o.Width, o.Height) || o.Width < 0 || o.Height < 0 {
		return nil
	}
	x0, y0 := o.X-o.Width/2, o.Y-o.Height/2
	x1, y1 := o.X+o.Width/2, o.Y+o.Height/2
	lo, hi := p.Coord(x0, y0), p.Coord(math.Nextafter(x1, math.Inf(-1)), math.Nextafter(y1, math.Inf(-1)))
	hi.X, hi.Y = max(hi.X, lo.X), max(hi.Y, lo.Y)
	out := make([]CellCoord, 0, (hi.X-lo.X+1)*(hi.Y-lo.Y+1))
	for y := lo.Y; y <= hi.Y; y++ {
		for x := lo.X; x <= hi.X; x++ {
			out = append(out, CellCoord{x, y})
		}
	}
	return out
}

// Needed returns the cells within LoadRadius cells (scaled per source) of any source, in row-major order (Y, then X). Sources with a non-finite position are skipped, and an invalid partition returns nil.
func (p Partition) Needed(sources []StreamingSource) []CellCoord {
	return p.around(sources, p.LoadRadius)
}

// Retained returns the cells within UnloadRadius cells (scaled per source) of any source, in the same order as Needed. A loaded cell missing from Retained can be unloaded; because UnloadRadius is at least LoadRadius, every cell in Needed is also in Retained.
func (p Partition) Retained(sources []StreamingSource) []CellCoord {
	return p.around(sources, p.UnloadRadius)
}

func (p Partition) around(sources []StreamingSource, radius int) []CellCoord {
	if p.Validate() != nil {
		return nil
	}
	set := map[CellCoord]struct{}{}
	for _, s := range sources {
		if !finite(s.X, s.Y) {
			continue
		}
		scale := s.RadiusScale
		if !(scale > 0) || math.IsInf(scale, 0) {
			scale = 1
		}
		center := p.Coord(s.X, s.Y)
		r := int(math.Ceil(float64(radius) * scale))
		for y := center.Y - r; y <= center.Y+r; y++ {
			for x := center.X - r; x <= center.X+r; x++ {
				set[CellCoord{x, y}] = struct{}{}
			}
		}
	}
	out := make([]CellCoord, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].X < out[j].X
	})
	return out
}

func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}
