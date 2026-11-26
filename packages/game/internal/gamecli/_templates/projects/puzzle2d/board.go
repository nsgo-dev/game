package main

// Tile is what one cell of a board holds.
type Tile uint8

// The tiles a level is made of.
const (
	// TileFloor is open ground the spark slides over.
	TileFloor Tile = iota
	// TileWall stops the spark in the cell before it; outside the grid counts as wall.
	TileWall
	// TileLamp is floor with a lamp that lights when the spark slides over or onto it.
	TileLamp
	// TileVoid swallows the spark, which loses the level.
	TileVoid
)

// Cell is a column and row of a board, from the top left.
type Cell struct{ X, Y int }

// Dir is a direction the spark slides in.
type Dir uint8

// The four directions, and DirNone for no move.
const (
	DirNone Dir = iota
	DirLeft
	DirRight
	DirUp
	DirDown
)

// String returns d's name.
func (d Dir) String() string {
	switch d {
	case DirLeft:
		return "left"
	case DirRight:
		return "right"
	case DirUp:
		return "up"
	case DirDown:
		return "down"
	default:
		return "none"
	}
}

// step returns c moved one cell in d.
func (d Dir) step(c Cell) Cell {
	switch d {
	case DirLeft:
		c.X--
	case DirRight:
		c.X++
	case DirUp:
		c.Y--
	case DirDown:
		c.Y++
	}
	return c
}

// Status is where a level being played stands.
type Status uint8

// The statuses of a board.
const (
	// Playing is a level with lamps left to light and moves left to make.
	Playing Status = iota
	// Won is a level with every lamp lit.
	Won
	// Lost is a level whose spark fell into a void or whose moves ran out with lamps still dark.
	Lost
)

// Board is one level being played: its fixed grid and what the moves made so far changed, the spark's cell, the lamps lit and the moves used. Moves change it only through slideMove, so every change can be undone.
type Board struct {
	// Level is the level the board was made from.
	Level *Level
	// Width and Height are the grid's size in cells.
	Width, Height int
	tiles         []Tile

	// Spark is the spark's cell.
	Spark Cell
	// Fallen is set once the spark has slid into a void.
	Fallen bool
	// Moves is how many moves have been made.
	Moves int
	lit   []bool
	lamps int
	// LitCount is how many lamps are lit.
	LitCount int
}

// NewBoard returns l at its start, which Level.Validate has accepted.
func NewBoard(l *Level) *Board {
	b := &Board{Level: l, Width: len(l.Rows[0]), Height: len(l.Rows)}
	b.tiles = make([]Tile, b.Width*b.Height)
	b.lit = make([]bool, b.Width*b.Height)
	for y, row := range l.Rows {
		for x := range len(row) {
			t := TileFloor
			switch row[x] {
			case charWall:
				t = TileWall
			case charLamp:
				t = TileLamp
				b.lamps++
			case charVoid:
				t = TileVoid
			case charStart:
				b.Spark = Cell{x, y}
			}
			b.tiles[y*b.Width+x] = t
		}
	}
	return b
}

// At returns the tile at c, TileWall outside the grid.
func (b *Board) At(c Cell) Tile {
	if c.X < 0 || c.Y < 0 || c.X >= b.Width || c.Y >= b.Height {
		return TileWall
	}
	return b.tiles[c.Y*b.Width+c.X]
}

// Lit reports whether c holds a lit lamp.
func (b *Board) Lit(c Cell) bool {
	return b.At(c) == TileLamp && b.lit[c.Y*b.Width+c.X]
}

// Lamps returns how many lamps the level has.
func (b *Board) Lamps() int { return b.lamps }

// MovesLeft returns how many moves remain.
func (b *Board) MovesLeft() int { return max(b.Level.Moves-b.Moves, 0) }

// Status returns whether the level is still being played, won or lost.
func (b *Board) Status() Status {
	switch {
	case b.Fallen:
		return Lost
	case b.LitCount == b.lamps:
		return Won
	case b.Moves >= b.Level.Moves:
		return Lost
	default:
		return Playing
	}
}

// Stars returns the stars the board has earned: none until it is won.
func (b *Board) Stars() int {
	if b.Status() != Won {
		return 0
	}
	return b.Level.StarsFor(b.Moves)
}

// Path returns the cells the spark passes through sliding in d, the last being where it stops: it slides until a wall is next, or into a void, where it falls. It is empty when a wall is next to the spark already, or the level is over, and so d is no move.
func (b *Board) Path(d Dir) []Cell {
	if d == DirNone || b.Status() != Playing {
		return nil
	}
	var path []Cell
	for c := b.Spark; ; {
		next := d.step(c)
		t := b.At(next)
		if t == TileWall {
			return path
		}
		path = append(path, next)
		if t == TileVoid {
			return path
		}
		c = next
	}
}

// slideMove is one move of the spark: it slides in Dir, lighting the lamps it passes. Apply records where it came from and what it lit, so Revert can put the board back exactly, and a History can redo it on the same board.
type slideMove struct {
	// Dir is the direction slid.
	Dir Dir

	from Cell
	lit  []int
	fell bool
}

// Apply slides the spark in m.Dir; the caller has checked with Board.Path that it moves.
func (m *slideMove) Apply(b *Board) {
	m.from, m.lit, m.fell = b.Spark, m.lit[:0], false
	for _, c := range b.Path(m.Dir) {
		b.Spark = c
		i := c.Y*b.Width + c.X
		switch b.tiles[i] {
		case TileLamp:
			if !b.lit[i] {
				b.lit[i] = true
				b.LitCount++
				m.lit = append(m.lit, i)
			}
		case TileVoid:
			m.fell = true
		}
	}
	b.Fallen = m.fell
	b.Moves++
}

// Revert takes the move back.
func (m *slideMove) Revert(b *Board) {
	for _, i := range m.lit {
		b.lit[i] = false
	}
	b.LitCount -= len(m.lit)
	b.Spark = m.from
	b.Fallen = false
	b.Moves--
}
