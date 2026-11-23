package cards

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math2d"
)

// Pose places a card: Pos is its center in board coordinates and Rot its rotation in radians around that center, turning from +X toward +Y (clockwise on NSGo's Y-down canvas).
type Pose struct {
	Pos math2d.Vec2
	Rot float32
}

// Lerp returns the pose t of the way from p to q, interpolating the center and the rotation linearly.
func (p Pose) Lerp(q Pose, t float32) Pose {
	return Pose{Pos: p.Pos.Lerp(q.Pos, t), Rot: p.Rot + (q.Rot-p.Rot)*t}
}

// Layout arranges the cards of a slot inside the slot's rectangle. Fan, Stack and Grid implement it; a game may supply its own.
type Layout interface {
	// Arrange appends the poses of n cards of size card laid out in area to dst, first card first, and returns the extended slice. It must append exactly n poses (none for n <= 0) and depend only on its arguments.
	Arrange(area math2d.Rect, card math2d.Vec2, n int, dst []Pose) []Pose
	// InsertIndex returns where, in [0, n], a card dropped with its center at p goes among the n cards already laid out in area.
	InsertIndex(area math2d.Rect, card math2d.Vec2, n int, p math2d.Vec2) int
}

// Fan lays a hand out in a row centered in the slot, tilting and dropping the cards toward its ends as a hand held in a fan.
//
// Cards sit Spacing apart, closer when the row would not fit the slot's width. A card's tilt and drop grow with its distance from the middle relative to half the room the slot has, so a full-width hand reaches ±Arc/2 at its ends and a short hand stays nearly flat.
type Fan struct {
	// Spacing is the distance in points between neighboring cards' centers; 0 uses 60% of the card's width.
	Spacing float32
	// Arc is the rotation in degrees from the leftmost to the rightmost card of a full-width hand; 0 keeps every card upright.
	Arc float32
	// Sag is how many points the end cards of a full-width hand sit below the middle one (negative raises them).
	Sag float32
}

// fanGeometry returns the center-to-center step and the half width the cards' centers can use.
func (f Fan) fanGeometry(area math2d.Rect, card math2d.Vec2, n int) (step, half float32) {
	step = f.Spacing
	if step <= 0 {
		step = card.X * 0.6
	}
	half = max((area.W-card.X)/2, 0)
	if n > 1 && step*float32(n-1)/2 > half {
		step = 2 * half / float32(n-1)
	}
	return step, half
}

// Arrange implements Layout.
func (f Fan) Arrange(area math2d.Rect, card math2d.Vec2, n int, dst []Pose) []Pose {
	if n <= 0 {
		return dst
	}
	step, half := f.fanGeometry(area, card, n)
	c := area.Center()
	tilt := f.Arc / 2 * math.Pi / 180
	for i := range n {
		off := (float32(i) - float32(n-1)/2) * step
		var u float32
		if half > 0 {
			u = off / half
		}
		dst = append(dst, Pose{Pos: math2d.Vec2{X: c.X + off, Y: c.Y + f.Sag*u*u}, Rot: u * tilt})
	}
	return dst
}

// InsertIndex implements Layout: the place in the row, once it holds n+1 cards, nearest to p horizontally.
func (f Fan) InsertIndex(area math2d.Rect, card math2d.Vec2, n int, p math2d.Vec2) int {
	if n <= 0 {
		return 0
	}
	step, _ := f.fanGeometry(area, card, n+1)
	if step <= 0 {
		return n
	}
	pos := (p.X-area.Center().X)/step + float32(n)/2
	return clampIndex(roundHalfUp(pos), n)
}

// Stack piles cards from the slot's top-left corner, each Offset from the one below it: a small offset makes a deck, a card-height fraction makes a pile whose cards all show, as a solitaire column.
type Stack struct {
	// Offset moves each card from the one before it, in points.
	Offset math2d.Vec2
	// Depth caps how many distinct offsets show: cards past the first Depth sit where the Depth-th does, so a tall deck keeps its footprint. 0 means no cap.
	Depth int
}

// Arrange implements Layout.
func (s Stack) Arrange(area math2d.Rect, card math2d.Vec2, n int, dst []Pose) []Pose {
	base := math2d.Vec2{X: area.X + card.X/2, Y: area.Y + card.Y/2}
	for i := range max(n, 0) {
		k := i
		if s.Depth > 0 {
			k = min(k, s.Depth-1)
		}
		dst = append(dst, Pose{Pos: base.Add(s.Offset.Scale(float32(k)))})
	}
	return dst
}

// InsertIndex implements Layout: a stack only grows on top.
func (s Stack) InsertIndex(_ math2d.Rect, _ math2d.Vec2, n int, _ math2d.Vec2) int {
	return max(n, 0)
}

// Grid lays cards out in rows from the slot's top-left corner, left to right and then top to bottom, as a board of cells or an inventory.
type Grid struct {
	// Columns is how many cards fill a row; 0 fits as many as the slot's width holds (at least one).
	Columns int
	// Gap is the space in points between neighboring cards, across (X) and down (Y).
	Gap math2d.Vec2
}

func (g Grid) columns(area math2d.Rect, card math2d.Vec2) int {
	if g.Columns > 0 {
		return g.Columns
	}
	cell := card.X + g.Gap.X
	if cell <= 0 {
		return 1
	}
	return max(int(math.Floor(float64((area.W+g.Gap.X)/cell))), 1)
}

// Arrange implements Layout.
func (g Grid) Arrange(area math2d.Rect, card math2d.Vec2, n int, dst []Pose) []Pose {
	cols := g.columns(area, card)
	for i := range max(n, 0) {
		col, row := i%cols, i/cols
		dst = append(dst, Pose{Pos: math2d.Vec2{
			X: area.X + card.X/2 + float32(col)*(card.X+g.Gap.X),
			Y: area.Y + card.Y/2 + float32(row)*(card.Y+g.Gap.Y),
		}})
	}
	return dst
}

// InsertIndex implements Layout: the cell under p, kept within the n+1 cells the slot then fills.
func (g Grid) InsertIndex(area math2d.Rect, card math2d.Vec2, n int, p math2d.Vec2) int {
	if n <= 0 {
		return 0
	}
	cols := g.columns(area, card)
	col, row := 0, 0
	if w := card.X + g.Gap.X; w > 0 {
		col = min(max(int(math.Floor(float64((p.X-area.X)/w))), 0), cols-1)
	}
	if h := card.Y + g.Gap.Y; h > 0 {
		row = max(int(math.Floor(float64((p.Y-area.Y)/h))), 0)
	}
	return clampIndex(row*cols+col, n)
}

func roundHalfUp(v float32) int {
	if v != v {
		return 0
	}
	f := math.Floor(float64(v) + 0.5)
	if f > math.MaxInt32 {
		return math.MaxInt32
	}
	if f < math.MinInt32 {
		return math.MinInt32
	}
	return int(f)
}

func clampIndex(i, n int) int { return min(max(i, 0), n) }
