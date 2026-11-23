package undo

import (
	"fmt"
	"reflect"
)

// Move is one reversible change to a game's state S, which is usually a pointer to the game's board or world.
//
// Apply makes the change and Revert takes it back; Revert(s) after Apply(s) must leave s exactly as it was before Apply. Apply may store what it needs to revert in the move itself (a move with a pointer receiver), since a History applies a move again on Redo only to the same state it first saw. Neither method may call the History that holds the move.
type Move[S any] interface {
	Apply(state S)
	Revert(state S)
}

// Func is a Move made of two functions, for moves that close over what they change.
type Func[S any] struct {
	Do   func(state S) // makes the change
	Undo func(state S) // takes the change back
}

// Apply calls f.Do.
func (f Func[S]) Apply(state S) { f.Do(state) }

// Revert calls f.Undo.
func (f Func[S]) Revert(state S) { f.Undo(state) }

// Group is a Move made of several moves that undo and redo as one, such as a turn that moves a piece and captures another. It applies its moves in order and reverts them in reverse order.
type Group[S any] []Move[S]

// Apply applies each move of g in order.
func (g Group[S]) Apply(state S) {
	for _, m := range g {
		m.Apply(state)
	}
}

// Revert reverts each move of g in reverse order.
func (g Group[S]) Revert(state S) {
	for i := len(g) - 1; i >= 0; i-- {
		g[i].Revert(state)
	}
}

// History holds a game's state and the moves made on it, with unlimited linear undo and redo.
//
// M is the type of the moves: Move[S] for moves of several kinds, or one concrete move type, which lets the history be saved as a Log. A History is not safe for concurrent use.
type History[S any, M Move[S]] struct {
	// OnChange, when set, is called after each call that applies or reverts moves or forgets them, so a game can update its undo and redo buttons.
	OnChange func()

	state   S
	moves   []M
	applied int
}

// New returns an empty History over state, the game's starting state.
func New[S any, M Move[S]](state S) *History[S, M] {
	return &History[S, M]{state: state}
}

// State returns the state the history's moves change.
func (h *History[S, M]) State() S { return h.state }

// Do applies m to the state and records it as the latest move, discarding any moves that were undone and not redone. It panics if m is nil.
func (h *History[S, M]) Do(m M) {
	if isNil(m) {
		panic("undo: Do with a nil move")
	}
	if h.applied < len(h.moves) {
		clear(h.moves[h.applied:])
		h.moves = h.moves[:h.applied]
	}
	m.Apply(h.state)
	h.moves = append(h.moves, m)
	h.applied++
	h.changed()
}

// Undo reverts the latest applied move and reports whether there was one.
func (h *History[S, M]) Undo() bool {
	if h.applied == 0 {
		return false
	}
	h.applied--
	h.moves[h.applied].Revert(h.state)
	h.changed()
	return true
}

// Redo applies the latest undone move again and reports whether there was one.
func (h *History[S, M]) Redo() bool {
	if h.applied == len(h.moves) {
		return false
	}
	h.moves[h.applied].Apply(h.state)
	h.applied++
	h.changed()
	return true
}

// Seek undoes or redoes moves until n moves are applied: Seek(0) returns to the starting state and Seek(h.Len()) redoes every move. It panics if n is outside [0, h.Len()].
func (h *History[S, M]) Seek(n int) {
	if n < 0 || n > len(h.moves) {
		panic(fmt.Sprintf("undo: Seek(%d) outside [0, %d]", n, len(h.moves)))
	}
	if n == h.applied {
		return
	}
	for h.applied > n {
		h.applied--
		h.moves[h.applied].Revert(h.state)
	}
	for h.applied < n {
		h.moves[h.applied].Apply(h.state)
		h.applied++
	}
	h.changed()
}

// CanUndo reports whether Undo has a move to revert.
func (h *History[S, M]) CanUndo() bool { return h.applied > 0 }

// CanRedo reports whether Redo has a move to apply again.
func (h *History[S, M]) CanRedo() bool { return h.applied < len(h.moves) }

// Len returns the number of moves recorded, applied and undone.
func (h *History[S, M]) Len() int { return len(h.moves) }

// Applied returns the number of moves applied, which is the index of the move Redo would apply.
func (h *History[S, M]) Applied() int { return h.applied }

// Moves returns a copy of the recorded moves in the order they were made; the first Applied of them are applied.
func (h *History[S, M]) Moves() []M {
	return append([]M(nil), h.moves...)
}

// Clear forgets every move and keeps the state as it is, which becomes the new starting state.
func (h *History[S, M]) Clear() {
	clear(h.moves)
	h.moves = h.moves[:0]
	h.applied = 0
	h.changed()
}

func (h *History[S, M]) changed() {
	if h.OnChange != nil {
		h.OnChange()
	}
}

// isNil reports whether m is a nil interface or a nil pointer, map, slice or func, which no move can be.
func isNil(m any) bool {
	if m == nil {
		return true
	}
	switch v := reflect.ValueOf(m); v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}
