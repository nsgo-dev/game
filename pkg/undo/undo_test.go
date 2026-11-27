package undo_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/undo"
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

func TestNilAndEmptyGroupsAreMoves(t *testing.T) {
	x := 5
	h := undo.New[*int, undo.Move[*int]](&x)
	var none undo.Group[*int]
	h.Do(none)
	h.Do(undo.Group[*int]{})
	if x != 5 || h.Len() != 2 {
		t.Fatalf("nil and empty groups gave x=%d len=%d, want 5 2", x, h.Len())
	}
	h.Seek(0)
	h.Seek(2)
	if x != 5 {
		t.Fatalf("undoing and redoing empty groups gave x=%d, want 5", x)
	}
	groups := undo.New[*int, undo.Group[*int]](&x)
	groups.Do(nil)
	if x != 5 || groups.Len() != 1 {
		t.Fatalf("nil Group as M gave x=%d len=%d, want 5 1", x, groups.Len())
	}
}

func TestGroupWithNilMovePanicsBeforeApplying(t *testing.T) {
	x := 1
	inc := undo.Func[*int]{Do: func(v *int) { *v++ }, Undo: func(v *int) { *v-- }}
	h := undo.New[*int, undo.Move[*int]](&x)
	for name, g := range map[string]undo.Group[*int]{
		"nil element":         {inc, nil},
		"nil in nested group": {inc, undo.Group[*int]{inc, nil}},
		"typed nil pointer":   {inc, (*undo.Func[*int])(nil)},
	} {
		func() {
			defer func() {
				if s, _ := recover().(string); !strings.Contains(s, "nil move") {
					t.Fatalf("%s: recovered %q, want a nil move panic", name, s)
				}
			}()
			h.Do(g)
		}()
		if x != 1 || h.Len() != 0 {
			t.Fatalf("%s: a refused group changed the state or history: x=%d len=%d", name, x, h.Len())
		}
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

// stepBy adds D; it is not a Merger.
type stepBy struct {
	D int `yaml:"d"`
}

func (s *stepBy) Apply(v *int)  { *v += s.D }
func (s *stepBy) Revert(v *int) { *v -= s.D }

// slide sets *v to a value and records the old one; consecutive slides fold into one, so one drag of a slider is one move to undo. It refuses a negative target, for the refused-merge path.
type slide struct {
	to  int
	old int
}

func (s *slide) Apply(v *int)  { s.old = *v; *v = s.to }
func (s *slide) Revert(v *int) { *v = s.old }

func (s *slide) Merge(next *slide) bool {
	if next.to < 0 {
		return false
	}
	s.to = next.to
	return true
}

func TestDoMergeFoldsConsecutiveMoves(t *testing.T) {
	x := 0
	h := undo.New[*int, *slide](&x)
	h.DoMerge(&slide{to: 1})
	h.DoMerge(&slide{to: 2})
	h.DoMerge(&slide{to: 3})
	if x != 3 || h.Len() != 1 {
		t.Fatalf("three folded slides gave x=%d len=%d, want 3 1", x, h.Len())
	}
	if !h.Undo() || x != 0 {
		t.Fatalf("undoing the folded drag gave x=%d, want 0", x)
	}
	if !h.Redo() || x != 3 {
		t.Fatalf("redoing the folded drag gave x=%d, want 3", x)
	}
}

func TestDoMergeRefusedRecordsANewMove(t *testing.T) {
	x := 0
	h := undo.New[*int, *slide](&x)
	h.DoMerge(&slide{to: 1})
	h.DoMerge(&slide{to: -2}) // refused: negative target
	if x != -2 || h.Len() != 2 {
		t.Fatalf("a refused merge gave x=%d len=%d, want -2 2", x, h.Len())
	}
	h.Undo()
	if x != 1 {
		t.Fatalf("undoing the refused move gave x=%d, want 1", x)
	}
}

func TestEndMergeStartsANewMove(t *testing.T) {
	x := 0
	h := undo.New[*int, *slide](&x)
	h.DoMerge(&slide{to: 1})
	h.DoMerge(&slide{to: 2})
	h.EndMerge()
	h.DoMerge(&slide{to: 3})
	if h.Len() != 2 || x != 3 {
		t.Fatalf("DoMerge after EndMerge gave len=%d x=%d, want 2 3", h.Len(), x)
	}
	h.Undo()
	if x != 2 {
		t.Fatalf("undoing the second drag gave x=%d, want 2", x)
	}
	h.Redo()
	h.DoMerge(&slide{to: 5}) // folds into the second drag again
	if h.Len() != 2 || x != 5 {
		t.Fatalf("DoMerge on the redone drag gave len=%d x=%d, want 2 5", h.Len(), x)
	}
	h.Seek(0)
	if x != 0 {
		t.Fatalf("undoing both drags gave x=%d, want 0", x)
	}
}

func TestDoMergeAfterUndoStartsANewMove(t *testing.T) {
	x := 0
	h := undo.New[*int, *slide](&x)
	h.DoMerge(&slide{to: 1})
	h.DoMerge(&slide{to: 2}) // folded into the first
	h.Undo()
	h.DoMerge(&slide{to: 3}) // nothing applied to fold into: a new move, the redo branch discarded
	if h.Len() != 1 || h.Applied() != 1 || x != 3 || h.CanRedo() {
		t.Fatalf("DoMerge after undo gave len=%d applied=%d x=%d canRedo=%v, want 1 1 3 false", h.Len(), h.Applied(), x, h.CanRedo())
	}
	h.Undo()
	if x != 0 || h.CanUndo() {
		t.Fatalf("undoing the new move gave x=%d canUndo=%v, want 0 false", x, h.CanUndo())
	}
}

func TestLimitForgetsOldestAppliedMoves(t *testing.T) {
	x := 0
	h := undo.New[*int, *stepBy](&x)
	h.Limit = 3
	calls := 0
	h.OnChange = func() { calls++ }
	for i := 1; i <= 5; i++ {
		h.Do(&stepBy{D: i})
	}
	if x != 15 || h.Len() != 3 || h.Applied() != 3 || h.Version() != 5 || calls != 5 {
		t.Fatalf("five moves with limit 3 gave x=%d len=%d applied=%d version=%d calls=%d, want 15 3 3 5 5", x, h.Len(), h.Applied(), h.Version(), calls)
	}
	h.Seek(0)
	if x != 3 || h.CanUndo() { // the forgotten +1 and +2 stay in the state
		t.Fatalf("undoing every kept move gave x=%d canUndo=%v, want 3 false", x, h.CanUndo())
	}
	h.Seek(3)
	if x != 15 {
		t.Fatalf("redoing every kept move gave x=%d, want 15", x)
	}

	// A new move on a redo branch discards the branch, and the limit keeps forgetting from the front.
	h.Undo()
	h.Undo()
	h.Do(&stepBy{D: 100})
	if h.Len() != 2 || x != 106 || h.CanRedo() {
		t.Fatalf("branching gave len=%d x=%d canRedo=%v, want 2 106 false", h.Len(), x, h.CanRedo())
	}
	h.Do(&stepBy{D: 200})
	h.Do(&stepBy{D: 300})
	if h.Len() != 3 || x != 606 || h.Version() != 6 {
		t.Fatalf("two more moves with limit 3 gave len=%d x=%d version=%d, want 3 606 6", h.Len(), x, h.Version())
	}
	h.Seek(0)
	if x != 6 { // the forgotten +1, +2 and +3 stay; +100 is kept and undone
		t.Fatalf("undoing every kept move on the new branch gave x=%d, want 6", x)
	}
}

func TestVersion(t *testing.T) {
	x := 0
	h := undo.New[*int, *slide](&x)
	if h.Version() != 0 {
		t.Fatalf("a new history is at version %d, want 0", h.Version())
	}
	h.Do(&slide{to: 1})
	h.DoMerge(&slide{to: 2}) // folds into the Do: the version is unchanged
	if h.Version() != 1 || h.Len() != 1 {
		t.Fatalf("Do then a folded DoMerge gave version=%d len=%d, want 1 1", h.Version(), h.Len())
	}
	h.EndMerge()
	h.DoMerge(&slide{to: 3})
	if h.Version() != 2 {
		t.Fatalf("DoMerge after EndMerge gave version %d, want 2", h.Version())
	}
	h.Undo()
	if h.Version() != 1 {
		t.Fatalf("Undo gave version %d, want 1", h.Version())
	}
	h.Redo()
	if h.Version() != 2 {
		t.Fatalf("Redo gave version %d, want 2", h.Version())
	}
	h.Seek(0)
	if h.Version() != 0 {
		t.Fatalf("Seek(0) gave version %d, want 0", h.Version())
	}
	h.Seek(2)
	h.Clear()
	if h.Version() != 0 {
		t.Fatalf("Clear gave version %d, want 0", h.Version())
	}
}

func TestDirtyAcrossUndoRedoAndSeek(t *testing.T) {
	x := 0
	h := undo.New[*int, *stepBy](&x)
	if !h.Dirty() {
		t.Fatal("a history with no saved point is clean")
	}
	h.MarkSaved()
	if h.Dirty() {
		t.Fatal("dirty right after MarkSaved")
	}
	h.Do(&stepBy{D: 1})
	h.Do(&stepBy{D: 2})
	if !h.Dirty() {
		t.Fatal("clean with moves past the saved point")
	}
	h.Undo()
	h.Undo()
	if h.Dirty() {
		t.Fatal("undoing back to the saved point is still dirty")
	}
	h.Seek(2)
	h.Seek(0)
	if h.Dirty() {
		t.Fatal("seeking back to the saved point is still dirty")
	}
	h.MarkSaved()
	h.Do(&stepBy{D: 1})
	h.Do(&stepBy{D: 2})
	h.MarkSaved() // saved at position 2
	h.Undo()
	if !h.Dirty() {
		t.Fatal("clean one move before the saved point")
	}
	h.Redo()
	if h.Dirty() {
		t.Fatal("redoing back to the saved point is still dirty")
	}
	h.Seek(1)
	h.Do(&stepBy{D: 9}) // discards the branch the saved point was on
	h.Seek(0)
	h.Seek(2)
	if !h.Dirty() {
		t.Fatal("a different branch at the saved position reports clean")
	}
}

func TestMarkSavedEndsTheMergeRun(t *testing.T) {
	x := 0
	h := undo.New[*int, *slide](&x)
	h.DoMerge(&slide{to: 1})
	h.DoMerge(&slide{to: 2})
	h.MarkSaved()
	if h.Dirty() {
		t.Fatal("dirty right after MarkSaved")
	}
	h.DoMerge(&slide{to: 3}) // a new move: folding it in would change the saved point's state behind its version
	if h.Len() != 2 || !h.Dirty() {
		t.Fatalf("DoMerge after MarkSaved gave len=%d dirty=%v, want 2 true", h.Len(), h.Dirty())
	}
	h.Undo()
	if h.Dirty() || x != 2 {
		t.Fatalf("undoing back to the saved point gave dirty=%v x=%d, want false 2", h.Dirty(), x)
	}
}

func TestDirtyWhenLimitForgetsTheSavedPoint(t *testing.T) {
	x := 0
	h := undo.New[*int, *stepBy](&x)
	h.Limit = 2
	h.Do(&stepBy{D: 1})
	h.MarkSaved() // saved at position 1
	h.Do(&stepBy{D: 2})
	h.Do(&stepBy{D: 3}) // forgets the first move
	h.Do(&stepBy{D: 4}) // forgets the second: position 1 is no longer reachable
	h.Seek(0)
	if !h.Dirty() {
		t.Fatal("clean at the oldest kept state, which is past the forgotten saved point")
	}
	if x != 3 { // the forgotten +1 and +2 stay
		t.Fatalf("oldest kept state is x=%d, want 3", x)
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
