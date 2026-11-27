package rts

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/math2d"
)

// Shape is the layout of a formation's slots.
type Shape string

// The formation shapes. Rows run across the facing direction and row 0 is the front.
const (
	// ShapeLine puts the units side by side in rows of Width slots, one row when Width is 0.
	ShapeLine Shape = "line"
	// ShapeColumn puts the units one behind another in rows of Width slots, one slot wide when Width is 0.
	ShapeColumn Shape = "column"
	// ShapeBox fills a square, or rows of Width slots when Width is set.
	ShapeBox Shape = "box"
	// ShapeWedge puts one unit at the point and each row behind it one slot wider, as a V of filled rows.
	ShapeWedge Shape = "wedge"
)

// Formation is a named layout that units ordered together take up around their target.
type Formation struct {
	// Name identifies the formation in its set.
	Name string `yaml:"name"`
	// Shape is the layout of the slots.
	Shape Shape `yaml:"shape"`
	// Spacing is the distance between neighboring slot centers in world units; it should be at least twice the largest unit radius so units in their slots do not push each other.
	Spacing float32 `yaml:"spacing"`
	// Width is the number of slots per row for the line, column and box shapes; 0 picks the shape's default. The wedge ignores it.
	Width int `yaml:"width,omitempty"`
}

// Slot is one place in a formation.
type Slot struct {
	// Offset is the slot's position relative to the formation's center, with X across the facing direction (positive to the right of it) and Y along it (positive ahead).
	Offset math2d.Vec2
	// Row is the slot's row, 0 at the front.
	Row int
}

// Layout returns the n slots of f around the formation's center, appending them to dst[:0]. Rows are filled front to back and centered across, so the slots' bounding box is centered on the origin. A formation with a non-positive or non-finite Spacing, or an unknown Shape, lays out a box with spacing 1.
func (f Formation) Layout(dst []Slot, n int) []Slot {
	dst = dst[:0]
	if n <= 0 {
		return dst
	}
	spacing := f.Spacing
	if !(spacing > 0) || math.IsInf(float64(spacing), 0) {
		spacing = 1
	}
	widths := rowWidths(f.Shape, f.Width, n)
	rows := len(widths)
	for r, w := range widths {
		y := (float32(rows-1)*0.5 - float32(r)) * spacing
		// Each row, a partial last one included, is centered across the facing direction.
		start := -float32(w-1) * 0.5 * spacing
		for i := range w {
			dst = append(dst, Slot{Offset: math2d.Vec2{X: start + float32(i)*spacing, Y: y}, Row: r})
		}
	}
	return dst
}

// rowWidths returns how many of n slots each row holds, front to back.
func rowWidths(shape Shape, width, n int) []int {
	per := 0
	switch shape {
	case ShapeLine:
		per = n
		if width > 0 {
			per = width
		}
	case ShapeColumn:
		per = 1
		if width > 0 {
			per = width
		}
	case ShapeWedge:
		var out []int
		for w := 1; n > 0; w++ {
			k := min(w, n)
			out = append(out, k)
			n -= k
		}
		return out
	default:
		per = int(math.Ceil(math.Sqrt(float64(n))))
		if width > 0 {
			per = width
		}
	}
	// A width beyond n changes nothing, and clamping it first keeps the row count from overflowing for a huge width.
	per = max(1, min(per, n))
	out := make([]int, 0, n/per+1)
	for n > 0 {
		k := min(per, n)
		out = append(out, k)
		n -= k
	}
	return out
}

// Place returns the world positions of slots for a formation centered on center and facing along facing, appending them to dst[:0]. A zero or non-finite facing faces up the screen (negative Y).
func Place(dst []math2d.Vec2, slots []Slot, center, facing math2d.Vec2) []math2d.Vec2 {
	fwd, right := frame(facing)
	dst = dst[:0]
	for _, s := range slots {
		dst = append(dst, center.Add(right.Scale(s.Offset.X)).Add(fwd.Scale(s.Offset.Y)))
	}
	return dst
}

// frame returns the unit forward vector and the unit vector to its right (screen coordinates, Y down) for facing.
func frame(facing math2d.Vec2) (fwd, right math2d.Vec2) {
	fwd = facing.Normalize()
	if fwd.LenSq() < 0.5 || !finite(fwd.X) || !finite(fwd.Y) {
		fwd = math2d.Vec2{Y: -1}
	}
	right = math2d.Vec2{X: -fwd.Y, Y: fwd.X}
	return fwd, right
}

func finite(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// Assign matches units at positions to slots so that front units take front slots and units on the left take left slots, which keeps their paths from crossing. It returns, for each unit, the index of its slot, appended to dst[:0]; slots must have at least len(positions) entries, laid out by Layout (row-major, front row first), and facing is the formation's facing.
//
// The match runs in O(n log n): units are ranked by how far ahead they stand along facing and dealt out to the rows front to back, then within a row ranked across and paired with the row's slots left to right. Ties fall back to the units' order in positions, so the result is deterministic.
func Assign(dst []int, positions []math2d.Vec2, slots []Slot, facing math2d.Vec2) []int {
	n := len(positions)
	dst = slices.Grow(dst[:0], n)[:n]
	if n == 0 {
		return dst
	}
	fwd, right := frame(facing)
	var centroid math2d.Vec2
	for _, p := range positions {
		centroid = centroid.Add(p)
	}
	centroid = centroid.Scale(1 / float32(n))
	type ranked struct {
		idx          int
		ahead, along float32
	}
	units := make([]ranked, n)
	for i, p := range positions {
		d := p.Sub(centroid)
		units[i] = ranked{idx: i, ahead: d.Dot(fwd), along: d.Dot(right)}
	}
	slices.SortStableFunc(units, func(a, b ranked) int {
		switch {
		case a.ahead > b.ahead:
			return -1
		case a.ahead < b.ahead:
			return 1
		}
		return 0
	})
	byAlong := func(a, b ranked) int {
		switch {
		case a.along < b.along:
			return -1
		case a.along > b.along:
			return 1
		}
		return a.idx - b.idx
	}
	start := 0
	for start < n {
		row := slots[start].Row
		end := start
		for end < n && slots[end].Row == row {
			end++
		}
		chunk := units[start:end]
		slices.SortFunc(chunk, byAlong)
		// Slots in a row are laid out left to right already.
		for i, u := range chunk {
			dst[u.idx] = start + i
		}
		start = end
	}
	return dst
}
