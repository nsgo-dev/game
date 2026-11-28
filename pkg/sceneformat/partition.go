package sceneformat

import (
	"fmt"
	"math"
	"sort"
)

// Partition describes the deterministic grid used to stream a large scene.
type Partition struct {
	CellSize     float64 `yaml:"cellSize"`
	LoadRadius   int     `yaml:"loadRadius,omitempty"`
	UnloadRadius int     `yaml:"unloadRadius,omitempty"`
}
type CellCoord struct{ X, Y int }
type StreamingSource struct {
	X, Y        float64
	RadiusScale float64
	Priority    int
	Wait        bool
}
type CellObject struct {
	ID            string
	X, Y          float64
	Width, Height float64
	AlwaysLoaded  bool
}

func (p Partition) Validate() error {
	if p.CellSize <= 0 || math.IsNaN(p.CellSize) || math.IsInf(p.CellSize, 0) {
		return fmt.Errorf("sceneformat: partition cellSize must be positive")
	}
	if p.LoadRadius < 0 || p.UnloadRadius < p.LoadRadius {
		return fmt.Errorf("sceneformat: partition radii are invalid")
	}
	return nil
}
func (p Partition) Coord(x, y float64) CellCoord {
	return CellCoord{int(math.Floor(x / p.CellSize)), int(math.Floor(y / p.CellSize))}
}

// Cells assigns an object to every cell touched by its bounds. Objects larger
// than one cell are returned in deterministic coordinate order.
func (p Partition) Cells(o CellObject) []CellCoord {
	if p.CellSize <= 0 {
		return nil
	}
	x0, y0 := o.X-o.Width/2, o.Y-o.Height/2
	x1, y1 := o.X+o.Width/2, o.Y+o.Height/2
	min, max := p.Coord(x0, y0), p.Coord(math.Nextafter(x1, math.Inf(-1)), math.Nextafter(y1, math.Inf(-1)))
	out := make([]CellCoord, 0, (max.X-min.X+1)*(max.Y-min.Y+1))
	for y := min.Y; y <= max.Y; y++ {
		for x := min.X; x <= max.X; x++ {
			out = append(out, CellCoord{x, y})
		}
	}
	return out
}

func (p Partition) Needed(sources []StreamingSource) []CellCoord {
	set := map[CellCoord]bool{}
	for _, s := range sources {
		scale := s.RadiusScale
		if scale <= 0 {
			scale = 1
		}
		center := p.Coord(s.X, s.Y)
		r := int(math.Ceil(float64(p.LoadRadius) * scale))
		for y := center.Y - r; y <= center.Y+r; y++ {
			for x := center.X - r; x <= center.X+r; x++ {
				set[CellCoord{x, y}] = true
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
