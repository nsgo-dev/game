package main

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/undo"
)

// testLevel returns a level for the rules' tests:
//
//	#######
//	#S..o.#
//	#.#####
//	#x....#
//	#o....#
//	#######
func testLevel() *Level {
	return &Level{
		ID: "test", Name: "Test", Moves: 4, Stars: Stars{Three: 2, Two: 3},
		Rows: []string{
			"#######",
			"#S..o.#",
			"#.#####",
			"#x....#",
			"#o....#",
			"#######",
		},
	}
}

func TestSparkSlidesUntilAWallAndLightsLamps(t *testing.T) {
	b := NewBoard(testLevel())
	if b.Spark != (Cell{1, 1}) || b.Lamps() != 2 || b.Status() != Playing {
		t.Fatalf("start: spark %v, %d lamps, status %v", b.Spark, b.Lamps(), b.Status())
	}
	if path := b.Path(DirUp); len(path) != 0 {
		t.Fatalf("a wall is above the spark, but up slides %v", path)
	}
	want := []Cell{{2, 1}, {3, 1}, {4, 1}, {5, 1}}
	if path := b.Path(DirRight); !slices.Equal(path, want) {
		t.Fatalf("right slides %v, want %v", path, want)
	}
	(&slideMove{Dir: DirRight}).Apply(b)
	if b.Spark != (Cell{5, 1}) || !b.Lit(Cell{4, 1}) || b.LitCount != 1 || b.Moves != 1 || b.MovesLeft() != 3 {
		t.Fatalf("after right: spark %v, lit %d, moves %d", b.Spark, b.LitCount, b.Moves)
	}
}

func TestVoidLosesTheLevel(t *testing.T) {
	b := NewBoard(testLevel())
	(&slideMove{Dir: DirDown}).Apply(b)
	if b.Spark != (Cell{1, 3}) || !b.Fallen || b.Status() != Lost || b.Stars() != 0 {
		t.Fatalf("down: spark %v, fallen %v, status %v", b.Spark, b.Fallen, b.Status())
	}
	if path := b.Path(DirRight); path != nil {
		t.Fatalf("a lost board still moves: %v", path)
	}
}

func TestRunningOutOfMovesLosesTheLevel(t *testing.T) {
	b := NewBoard(testLevel())
	for _, d := range []Dir{DirRight, DirLeft, DirRight, DirLeft} {
		(&slideMove{Dir: d}).Apply(b)
	}
	if b.Status() != Lost || b.Fallen || b.MovesLeft() != 0 {
		t.Fatalf("after four moves with a lamp dark: status %v, fallen %v, %d left", b.Status(), b.Fallen, b.MovesLeft())
	}
}

func TestLightingEveryLampWinsWithStars(t *testing.T) {
	l := testLevel()
	l.Rows[3] = "#.....#"
	b := NewBoard(l)
	(&slideMove{Dir: DirRight}).Apply(b)
	(&slideMove{Dir: DirLeft}).Apply(b)
	(&slideMove{Dir: DirDown}).Apply(b)
	if b.Status() != Won || b.Stars() != 2 {
		t.Fatalf("status %v with %d stars after 3 moves, want won with 2", b.Status(), b.Stars())
	}
}

// TestUndoAndRedoRestoreTheBoard checks that undoing every move of a game through an undo.History gives back the board it started from, and that redoing them gives back the board they made.
func TestUndoAndRedoRestoreTheBoard(t *testing.T) {
	h := undo.New[*Board, *slideMove](NewBoard(testLevel()))
	start := snapshot(h.State())
	for _, d := range []Dir{DirRight, DirLeft, DirRight, DirLeft} {
		h.Do(&slideMove{Dir: d})
	}
	end := snapshot(h.State())
	for h.Undo() {
	}
	if got := snapshot(h.State()); got != start {
		t.Fatalf("undoing every move left %+v, want %+v", got, start)
	}
	for h.Redo() {
	}
	if got := snapshot(h.State()); got != end {
		t.Fatalf("redoing every move left %+v, want %+v", got, end)
	}
}

// boardSnapshot is everything moves change on a board, comparable with ==.
type boardSnapshot struct {
	spark         Cell
	fallen        bool
	moves, litNum int
	lit           string
}

func snapshot(b *Board) boardSnapshot {
	s := boardSnapshot{spark: b.Spark, fallen: b.Fallen, moves: b.Moves, litNum: b.LitCount}
	for y := range b.Height {
		for x := range b.Width {
			if b.Lit(Cell{x, y}) {
				s.lit += "1"
			} else {
				s.lit += "0"
			}
		}
	}
	return s
}

// BenchmarkSlide measures one move and its undo on the largest level, which a game makes at most once a step.
func BenchmarkSlide(b *testing.B) {
	l := &Levels.Levels[len(Levels.Levels)-1]
	h := undo.New[*Board, *slideMove](NewBoard(l))
	for b.Loop() {
		for _, d := range []Dir{DirRight, DirDown, DirLeft, DirUp} {
			if len(h.State().Path(d)) > 0 {
				h.Do(&slideMove{Dir: d})
				h.Undo()
			}
		}
	}
}
