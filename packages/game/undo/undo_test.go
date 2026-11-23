package undo_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/undo"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// board is a small crate-pushing puzzle: '#' walls, '$' crates, '.' floor.
type board struct {
	W      int
	Cells  []byte
	X, Y   int
	Steps  int
	Pushes int
}

func (b *board) at(x, y int) byte { return b.Cells[y*b.W+x] }

func (b *board) set(x, y int, c byte) { b.Cells[y*b.W+x] = c }

func (b *board) clone() *board {
	c := *b
	c.Cells = slices.Clone(b.Cells)
	return &c
}

func newBoard() *board {
	rows := []string{
		"##########",
		"#........#",
		"#..$..$..#",
		"#...##...#",
		"#.$....$.#",
		"#........#",
		"##########",
	}
	b := &board{W: len(rows[0]), X: 1, Y: 1}
	for _, r := range rows {
		b.Cells = append(b.Cells, r...)
	}
	return b
}

var dirs = map[string][2]int{"left": {-1, 0}, "right": {1, 0}, "up": {0, -1}, "down": {0, 1}}

// push walks the player one cell, pushing a crate ahead when the cell behind it is free. It records what it did in unexported fields so Revert can take it back.
type push struct {
	Dir string `yaml:"dir"`

	moved, pushed bool
}

func (p *push) Apply(b *board) {
	d := dirs[p.Dir]
	nx, ny := b.X+d[0], b.Y+d[1]
	p.moved, p.pushed = false, false
	switch b.at(nx, ny) {
	case '#':
		return
	case '$':
		bx, by := nx+d[0], ny+d[1]
		if b.at(bx, by) != '.' {
			return
		}
		b.set(bx, by, '$')
		b.set(nx, ny, '.')
		b.Pushes++
		p.pushed = true
	}
	b.X, b.Y = nx, ny
	b.Steps++
	p.moved = true
}

func (p *push) Revert(b *board) {
	if !p.moved {
		return
	}
	d := dirs[p.Dir]
	if p.pushed {
		b.set(b.X+d[0], b.Y+d[1], '.')
		b.set(b.X, b.Y, '$')
		b.Pushes--
	}
	b.X, b.Y = b.X-d[0], b.Y-d[1]
	b.Steps--
}

func randomPushes(r *rand.Rand, n int) []*push {
	names := []string{"left", "right", "up", "down"}
	out := make([]*push, n)
	for i := range out {
		out[i] = &push{Dir: names[r.IntN(len(names))]}
	}
	return out
}

func TestUndoEveryMoveReturnsToStart(t *testing.T) {
	for seed := range uint64(20) {
		r := rand.New(rand.NewPCG(seed, 520))
		start := newBoard()
		b := start.clone()
		h := undo.New[*board, *push](b)
		var after []*board
		for _, p := range randomPushes(r, 400) {
			h.Do(p)
			after = append(after, b.clone())
		}
		final := b.clone()
		if final.Pushes == 0 {
			t.Fatalf("seed %d: no crate was pushed, the test walks too little", seed)
		}

		for i := len(after) - 1; i >= 0; i-- {
			if !reflect.DeepEqual(b, after[i]) {
				t.Fatalf("seed %d: state before undoing move %d differs", seed, i)
			}
			if !h.Undo() {
				t.Fatalf("seed %d: Undo of move %d returned false", seed, i)
			}
		}
		if !reflect.DeepEqual(b, start) {
			t.Fatalf("seed %d: undoing every move left %+v, want the start %+v", seed, b, start)
		}
		if h.Undo() || h.CanUndo() || !h.CanRedo() {
			t.Fatalf("seed %d: at the start Undo/CanUndo/CanRedo = %v/%v/%v", seed, h.Undo(), h.CanUndo(), h.CanRedo())
		}

		for i := range after {
			if !h.Redo() {
				t.Fatalf("seed %d: Redo of move %d returned false", seed, i)
			}
			if !reflect.DeepEqual(b, after[i]) {
				t.Fatalf("seed %d: redoing move %d differs from its first apply", seed, i)
			}
		}
		if h.Redo() || !reflect.DeepEqual(b, final) {
			t.Fatalf("seed %d: redoing every move did not return to the final state", seed)
		}

		h.Seek(0)
		if !reflect.DeepEqual(b, start) || h.Applied() != 0 || h.Len() != 400 {
			t.Fatalf("seed %d: Seek(0) gave applied=%d len=%d, state equal=%v", seed, h.Applied(), h.Len(), reflect.DeepEqual(b, start))
		}
		h.Seek(250)
		if !reflect.DeepEqual(b, after[249]) {
			t.Fatalf("seed %d: Seek(250) differs from the state after move 250", seed)
		}
		h.Seek(h.Len())
		if !reflect.DeepEqual(b, final) {
			t.Fatalf("seed %d: Seek(Len) differs from the final state", seed)
		}
	}
}

func TestDoAfterUndoDiscardsRedo(t *testing.T) {
	var log []string
	add := func(s string) undo.Move[*[]string] {
		return undo.Func[*[]string]{
			Do:   func(l *[]string) { *l = append(*l, s) },
			Undo: func(l *[]string) { *l = (*l)[:len(*l)-1] },
		}
	}
	h := undo.New[*[]string, undo.Move[*[]string]](&log)
	h.Do(add("a"))
	h.Do(add("b"))
	h.Do(add("c"))
	h.Undo()
	h.Undo()
	if !slices.Equal(log, []string{"a"}) || h.Len() != 3 || h.Applied() != 1 {
		t.Fatalf("after two undos log=%v len=%d applied=%d", log, h.Len(), h.Applied())
	}
	h.Do(add("d"))
	if !slices.Equal(log, []string{"a", "d"}) || h.Len() != 2 || h.CanRedo() {
		t.Fatalf("after a new move log=%v len=%d canRedo=%v, want [a d] 2 false", log, h.Len(), h.CanRedo())
	}
	if h.Redo() {
		t.Fatal("Redo after a new move returned true")
	}
}

func TestGroupRevertsInReverseOrder(t *testing.T) {
	x := 3
	double := undo.Func[*int]{Do: func(v *int) { *v *= 2 }, Undo: func(v *int) { *v /= 2 }}
	addOne := undo.Func[*int]{Do: func(v *int) { *v++ }, Undo: func(v *int) { *v-- }}
	h := undo.New[*int, undo.Move[*int]](&x)
	h.Do(undo.Group[*int]{double, addOne, double})
	if x != 14 || h.Len() != 1 {
		t.Fatalf("group applied x=%d len=%d, want 14 1", x, h.Len())
	}
	h.Undo()
	if x != 3 {
		t.Fatalf("group reverted x=%d, want 3", x)
	}
	h.Redo()
	if x != 14 {
		t.Fatalf("group redone x=%d, want 14", x)
	}
}

func TestOnChangeAndClear(t *testing.T) {
	x := 0
	inc := undo.Func[*int]{Do: func(v *int) { *v++ }, Undo: func(v *int) { *v-- }}
	h := undo.New[*int, undo.Func[*int]](&x)
	calls := 0
	h.OnChange = func() { calls++ }

	h.Do(inc)
	h.Do(inc)
	h.Undo()
	h.Redo()
	h.Seek(0)
	h.Seek(0) // already there: no change
	h.Seek(2)
	h.Undo()
	if calls != 7 {
		t.Fatalf("OnChange called %d times, want 7", calls)
	}
	if h.Redo(); h.Redo() {
		t.Fatal("Redo past the last move returned true")
	}
	if calls != 8 {
		t.Fatalf("OnChange called %d times after a failed Redo, want 8", calls)
	}

	h.Clear()
	if x != 2 || h.Len() != 0 || h.Applied() != 0 || h.CanUndo() || h.CanRedo() || calls != 9 {
		t.Fatalf("after Clear x=%d len=%d applied=%d calls=%d", x, h.Len(), h.Applied(), calls)
	}
	h.Do(inc)
	h.Seek(0)
	if x != 2 {
		t.Fatalf("Seek(0) after Clear gave x=%d, want the cleared state 2", x)
	}
	if h.State() != &x {
		t.Fatal("State does not return the history's state")
	}
}

func TestMovesIsACopy(t *testing.T) {
	b := newBoard()
	h := undo.New[*board, *push](b)
	h.Do(&push{Dir: "right"})
	moves := h.Moves()
	moves[0] = &push{Dir: "down"}
	if h.Moves()[0].Dir != "right" {
		t.Fatal("changing the slice from Moves changed the history")
	}
}

func TestPanics(t *testing.T) {
	expectPanic := func(name, want string, f func()) {
		t.Helper()
		defer func() {
			r := recover()
			if s, _ := r.(string); !strings.Contains(s, want) {
				t.Fatalf("%s: recovered %v, want a panic containing %q", name, r, want)
			}
		}()
		f()
	}
	h := undo.New[*board, *push](newBoard())
	expectPanic("nil pointer move", "nil move", func() { h.Do(nil) })
	g := undo.New[*int, undo.Move[*int]](new(int))
	expectPanic("nil interface move", "nil move", func() { g.Do(nil) })
	expectPanic("seek past the end", "Seek(1)", func() { h.Seek(1) })
	expectPanic("seek before the start", "Seek(-1)", func() { h.Seek(-1) })
}

func ExampleHistory() {
	type counter struct{ N int }
	add := func(d int) undo.Move[*counter] {
		return undo.Func[*counter]{
			Do:   func(c *counter) { c.N += d },
			Undo: func(c *counter) { c.N -= d },
		}
	}
	c := &counter{}
	h := undo.New[*counter, undo.Move[*counter]](c)
	h.Do(add(5))
	h.Do(add(3))
	h.Undo()
	fmt.Println(c.N, h.CanRedo())
	h.Redo()
	h.Seek(0)
	fmt.Println(c.N, h.Applied(), h.Len())
	// Output:
	// 5 true
	// 0 0 2
}

func BenchmarkDoUndo(b *testing.B) {
	x := 0
	inc := undo.Func[*int]{Do: func(v *int) { *v++ }, Undo: func(v *int) { *v-- }}
	h := undo.New[*int, undo.Func[*int]](&x)
	for range 1024 {
		h.Do(inc)
	}
	h.Seek(0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.Do(inc)
		h.Undo()
	}
}
