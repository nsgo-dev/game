package main

import (
	"github.com/nsgo-dev/game/packages/game/random"
)

const (
	// BoardSize is the width and height of the square SlideGem board.
	BoardSize = 6
	// GemKinds is the number of distinct gem types on the board.
	GemKinds = 6
	// InitialMoves is the starting move budget for each puzzle run.
	InitialMoves = 25
)

// GemKind identifies one of the 6 gem varieties (or GemNone for an empty cell).
type GemKind int8

const (
	GemNone GemKind = -1
	GemRuby GemKind = iota
	GemSapphire
	GemEmerald
	GemAmber
	GemAmethyst
	GemPearl
)

// GemNames maps each GemKind to its sprite atlas frame name.
var GemNames = [GemKinds]string{
	"gem_ruby",
	"gem_sapphire",
	"gem_emerald",
	"gem_amber",
	"gem_amethyst",
	"gem_pearl",
}

// Cell is a zero-based (Row, Col) coordinate on the 6x6 board.
type Cell struct {
	Row int
	Col int
}

// Move is a swap between two adjacent cells.
type Move struct {
	A Cell
	B Cell
}

// MatchResult describes all horizontal and vertical runs of 3+ identical gems.
type MatchResult struct {
	Cells  []Cell
	Groups [][]Cell
	Mask   [BoardSize][BoardSize]bool
}

// Empty reports whether no matches were found.
func (m MatchResult) Empty() bool {
	return len(m.Cells) == 0
}

// FallMove describes one gem dropping from FromRow to ToRow in column Col.
// Newly spawned gems enter from negative FromRow values above the top edge.
type FallMove struct {
	FromRow int
	ToRow   int
	Col     int
	Kind    GemKind
	Spawned bool
}

// Board holds the deterministic 6x6 match-3 puzzle state.
type Board struct {
	Seed         uint64
	Grid         [BoardSize][BoardSize]GemKind
	Score        int
	MovesLeft    int
	Combo        int
	MaxCombo     int
	TotalMatches int
	Reshuffles   int
	GameOver     bool

	rng *random.Stream
}

// NewBoard creates a deterministic seeded 6x6 board with no initial 3-in-a-row
// matches and at least one guaranteed valid move.
func NewBoard(seed uint64) *Board {
	b := &Board{
		Seed:      seed,
		MovesLeft: InitialMoves,
		rng:       random.New(seed),
	}
	b.fillPlayable()
	return b
}

// InBounds reports whether c lies inside the 6x6 board.
func InBounds(c Cell) bool {
	return c.Row >= 0 && c.Row < BoardSize && c.Col >= 0 && c.Col < BoardSize
}

// Adjacent reports whether a and b are orthogonally adjacent in-bounds cells.
func Adjacent(a, b Cell) bool {
	if !InBounds(a) || !InBounds(b) {
		return false
	}
	dr := a.Row - b.Row
	if dr < 0 {
		dr = -dr
	}
	dc := a.Col - b.Col
	if dc < 0 {
		dc = -dc
	}
	return dr+dc == 1
}

// At returns the gem kind at c, or GemNone if out of bounds.
func (b *Board) At(c Cell) GemKind {
	if !InBounds(c) {
		return GemNone
	}
	return b.Grid[c.Row][c.Col]
}

// Swap exchanges two adjacent cells in place and reports whether the swap was valid.
func (b *Board) Swap(a, bCell Cell) bool {
	if !Adjacent(a, bCell) {
		return false
	}
	b.Grid[a.Row][a.Col], b.Grid[bCell.Row][bCell.Col] = b.Grid[bCell.Row][bCell.Col], b.Grid[a.Row][a.Col]
	return true
}

// WouldMatch reports whether swapping adjacent cells a and b produces at least one match.
func (b *Board) WouldMatch(a, bCell Cell) bool {
	if !Adjacent(a, bCell) {
		return false
	}
	ka := b.Grid[a.Row][a.Col]
	kb := b.Grid[bCell.Row][bCell.Col]
	if ka == GemNone || kb == GemNone || ka == kb {
		return false
	}
	b.Grid[a.Row][a.Col], b.Grid[bCell.Row][bCell.Col] = kb, ka
	matched := b.hasAnyMatch()
	b.Grid[a.Row][a.Col], b.Grid[bCell.Row][bCell.Col] = ka, kb
	return matched
}

// FindMatches locates every horizontal and vertical run of 3 or more identical gems.
func (b *Board) FindMatches() MatchResult {
	var res MatchResult

	// Horizontal runs.
	for r := range BoardSize {
		c := 0
		for c < BoardSize {
			k := b.Grid[r][c]
			if k == GemNone {
				c++
				continue
			}
			end := c + 1
			for end < BoardSize && b.Grid[r][end] == k {
				end++
			}
			if end-c >= 3 {
				group := make([]Cell, 0, end-c)
				for col := c; col < end; col++ {
					cell := Cell{Row: r, Col: col}
					group = append(group, cell)
					res.Mask[r][col] = true
				}
				res.Groups = append(res.Groups, group)
			}
			c = end
		}
	}

	// Vertical runs.
	for c := range BoardSize {
		r := 0
		for r < BoardSize {
			k := b.Grid[r][c]
			if k == GemNone {
				r++
				continue
			}
			end := r + 1
			for end < BoardSize && b.Grid[end][c] == k {
				end++
			}
			if end-r >= 3 {
				group := make([]Cell, 0, end-r)
				for row := r; row < end; row++ {
					cell := Cell{Row: row, Col: c}
					group = append(group, cell)
					res.Mask[row][c] = true
				}
				res.Groups = append(res.Groups, group)
			}
			r = end
		}
	}

	// Deduplicate into deterministic row-major order.
	for r := range BoardSize {
		for c := range BoardSize {
			if res.Mask[r][c] {
				res.Cells = append(res.Cells, Cell{Row: r, Col: c})
			}
		}
	}
	return res
}

// ClearMatches clears all matched cells, increments the cascade combo multiplier,
// updates Score, and returns the points awarded for this clear step.
func (b *Board) ClearMatches(m MatchResult) int {
	if m.Empty() {
		return 0
	}
	b.Combo++
	if b.Combo > b.MaxCombo {
		b.MaxCombo = b.Combo
	}
	b.TotalMatches += len(m.Groups)

	for _, cell := range m.Cells {
		b.Grid[cell.Row][cell.Col] = GemNone
	}

	base := len(m.Cells) * 30
	for _, g := range m.Groups {
		if len(g) > 3 {
			base += (len(g) - 3) * 40
		}
	}
	if len(m.Groups) > 1 {
		base += (len(m.Groups) - 1) * 50
	}
	points := base * b.Combo
	b.Score += points
	return points
}

// ApplyGravity drops remaining gems down into empty spaces and spawns new gems
// from above the board, returning every moved or spawned gem for animation.
func (b *Board) ApplyGravity() []FallMove {
	var moves []FallMove
	for c := range BoardSize {
		writeRow := BoardSize - 1
		for r := BoardSize - 1; r >= 0; r-- {
			k := b.Grid[r][c]
			if k == GemNone {
				continue
			}
			if r != writeRow {
				b.Grid[writeRow][c] = k
				b.Grid[r][c] = GemNone
				moves = append(moves, FallMove{
					FromRow: r,
					ToRow:   writeRow,
					Col:     c,
					Kind:    k,
					Spawned: false,
				})
			}
			writeRow--
		}
		// Fill empty slots above writeRow with fresh gems falling from above row 0.
		missing := writeRow + 1
		for toRow := writeRow; toRow >= 0; toRow-- {
			k := GemKind(b.rng.Intn(GemKinds))
			b.Grid[toRow][c] = k
			moves = append(moves, FallMove{
				FromRow: toRow - missing,
				ToRow:   toRow,
				Col:     c,
				Kind:    k,
				Spawned: true,
			})
		}
	}
	return moves
}

// FinishTurn concludes a completed player swap (after all cascades finish):
// resets Combo, decrements MovesLeft, and sets GameOver when moves reach 0.
func (b *Board) FinishTurn() {
	b.Combo = 0
	if b.MovesLeft > 0 {
		b.MovesLeft--
	}
	if b.MovesLeft <= 0 {
		b.MovesLeft = 0
		b.GameOver = true
	}
}

// FindHint returns the first valid move in deterministic row-major order.
func (b *Board) FindHint() (Move, bool) {
	for r := range BoardSize {
		for c := range BoardSize {
			a := Cell{Row: r, Col: c}
			if c+1 < BoardSize {
				right := Cell{Row: r, Col: c + 1}
				if b.WouldMatch(a, right) {
					return Move{A: a, B: right}, true
				}
			}
			if r+1 < BoardSize {
				down := Cell{Row: r + 1, Col: c}
				if b.WouldMatch(a, down) {
					return Move{A: a, B: down}, true
				}
			}
		}
	}
	return Move{}, false
}

// EnsurePlayable checks whether at least one valid move exists on a live board;
// if deadlocked, it deterministically reshuffles until a playable layout is found.
func (b *Board) EnsurePlayable() bool {
	if b.GameOver {
		return false
	}
	if _, ok := b.FindHint(); ok {
		return false
	}
	b.Reshuffles++
	b.fillPlayable()
	return true
}

func (b *Board) hasAnyMatch() bool {
	for r := range BoardSize {
		for c := 0; c <= BoardSize-3; c++ {
			k := b.Grid[r][c]
			if k != GemNone && b.Grid[r][c+1] == k && b.Grid[r][c+2] == k {
				return true
			}
		}
	}
	for c := range BoardSize {
		for r := 0; r <= BoardSize-3; r++ {
			k := b.Grid[r][c]
			if k != GemNone && b.Grid[r+1][c] == k && b.Grid[r+2][c] == k {
				return true
			}
		}
	}
	return false
}

func (b *Board) fillPlayable() {
	for attempt := 0; attempt < 64; attempt++ {
		b.fillNoMatches()
		if _, ok := b.FindHint(); ok {
			return
		}
	}
	// Deterministic guaranteed-playable fallback pattern (no initial matches,
	// swapping (0,0) and (0,1) creates a vertical ruby match in column 1).
	for r := range BoardSize {
		for c := range BoardSize {
			b.Grid[r][c] = GemKind((r*2 + c) % GemKinds)
		}
	}
	b.Grid[0][0] = GemRuby
	b.Grid[0][1] = GemSapphire
	b.Grid[1][1] = GemRuby
	b.Grid[2][1] = GemRuby
}

func (b *Board) fillNoMatches() {
	var choices [GemKinds]GemKind
	for r := range BoardSize {
		for c := range BoardSize {
			n := 0
			for k := GemKind(0); k < GemKinds; k++ {
				if c >= 2 && b.Grid[r][c-1] == k && b.Grid[r][c-2] == k {
					continue
				}
				if r >= 2 && b.Grid[r-1][c] == k && b.Grid[r-2][c] == k {
					continue
				}
				choices[n] = k
				n++
			}
			b.Grid[r][c] = choices[b.rng.Intn(n)]
		}
	}
}
