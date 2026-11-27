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

// Merger is a move that can absorb the next one of its kind, such as two steps of one drag of a slider or an object. History.DoMerge folds a move into the latest applied move when that move is a Merger whose Merge accepts it.
type Merger[M any] interface {
	// Merge folds next into the receiver and reports whether it did; next is already applied to the state, so a refused merge leaves both moves in place.
	Merge(next M) bool
}

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

// History holds a game's state and the moves made on it, with linear undo and redo. Every move is kept until Clear unless Limit is set.
//
// M is the type of the moves: Move[S] for moves of several kinds, or one concrete move type, which lets the history be saved as a Log. A History is not safe for concurrent use.
type History[S any, M Move[S]] struct {
	// OnChange, when set, is called after each call that applies or reverts moves or forgets them, so a game can update its undo and redo buttons.
	OnChange func()

	// Limit, when positive, is the most moves kept. As a new move is recorded past the limit, the oldest applied moves are forgotten: their effects stay in the state but they can no longer be undone, and Seek and a Log count from the oldest kept move. Zero keeps every move.
	Limit int

	state     S
	moves     []M
	applied   int
	forgotten int  // moves Limit dropped from the front
	saved     int  // position (forgotten + applied) MarkSaved recorded
	hasSaved  bool // saved is set
	noMerge   bool // the next DoMerge starts a new move
}

// New returns an empty History over state, the game's starting state.
func New[S any, M Move[S]](state S) *History[S, M] {
	return &History[S, M]{state: state}
}

// State returns the state the history's moves change.
func (h *History[S, M]) State() S { return h.state }

// Do applies m to the state and records it as the latest move, discarding any moves that were undone and not redone. It panics, before applying anything, if m is nil or is a Group holding a nil move; a nil or empty Group is a valid move that changes nothing.
func (h *History[S, M]) Do(m M) {
	if nilMove[S](m) {
		panic("undo: Do with a nil move")
	}
	h.discardRedo()
	m.Apply(h.state)
	h.record(m)
	h.changed()
}

// DoMerge applies m to the state like Do, then folds it into the latest applied move when that move is a Merger whose Merge accepts it, so the steps of one drag become one move to undo. A refused merge, or one after EndMerge or MarkSaved, records m as a move of its own.
func (h *History[S, M]) DoMerge(m M) {
	if nilMove[S](m) {
		panic("undo: DoMerge with a nil move")
	}
	h.discardRedo()
	m.Apply(h.state)
	if !h.noMerge && h.applied > 0 {
		if mg, ok := any(h.moves[h.applied-1]).(Merger[M]); ok && mg.Merge(m) {
			h.noMerge = false
			h.changed()
			return
		}
	}
	h.record(m)
	h.changed()
}

// EndMerge ends the current merge run: the next DoMerge starts a new move even if the latest one would accept it. Call it when a drag ends, so the next drag of the same slider or object does not fold into this one's move.
func (h *History[S, M]) EndMerge() { h.noMerge = true }

// record appends m as the latest move and enforces Limit.
func (h *History[S, M]) record(m M) {
	h.moves = append(h.moves, m)
	h.applied++
	h.noMerge = false
	for h.Limit > 0 && len(h.moves) > h.Limit && h.applied > 0 {
		var zero M
		h.moves[0] = zero
		h.moves = h.moves[1:]
		h.applied--
		h.forgotten++
	}
}

// discardRedo forgets the moves that were undone and not redone, which a new move replaces. A saved position among them is lost, so the history stays dirty until the next MarkSaved.
func (h *History[S, M]) discardRedo() {
	if h.applied == len(h.moves) {
		return
	}
	if h.hasSaved && h.saved > h.forgotten+h.applied {
		h.hasSaved = false
	}
	clear(h.moves[h.applied:])
	h.moves = h.moves[:h.applied]
}

// Version identifies the current point in the history: it changes on every Do, Undo, Redo, Seek and Clear, and a DoMerge that starts a new move counts as a Do. A DoMerge that folds into the latest move keeps the version, since that move's place in the history is unchanged.
func (h *History[S, M]) Version() uint64 { return uint64(h.forgotten + h.applied) }

// MarkSaved records the current point as the saved one, for an in-game editor's "unsaved changes" mark, and ends the current merge run as EndMerge does, so the first DoMerge after a save cannot rewrite the move the saved point rests on.
func (h *History[S, M]) MarkSaved() {
	h.saved = h.forgotten + h.applied
	h.hasSaved = true
	h.noMerge = true
}

// Dirty reports whether the state differs from the one MarkSaved saw: the current point is not the saved one, or no point was ever marked. It compares positions, so undoing or redoing back to the saved point reports clean. Once Limit forgets moves past the saved point or a new move discards the branch the saved point was on, the point is unreachable and the history stays dirty until the next MarkSaved.
func (h *History[S, M]) Dirty() bool {
	return !h.hasSaved || h.saved != h.forgotten+h.applied
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

// Seek undoes or redoes moves until n moves are applied: Seek(0) returns to the starting state (the oldest kept one, when Limit forgot moves) and Seek(h.Len()) redoes every move. It panics if n is outside [0, h.Len()].
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

// Clear forgets every move and keeps the state as it is, which becomes the new starting state. A Log taken after Clear holds only the later moves, so a game that saves it must also save the state at the Clear and pass that to Restore. The saved point is forgotten with the moves, so the history is dirty until the next MarkSaved.
func (h *History[S, M]) Clear() {
	clear(h.moves)
	h.moves = h.moves[:0]
	h.applied = 0
	h.forgotten = 0
	h.hasSaved = false
	h.noMerge = true
	h.changed()
}

func (h *History[S, M]) changed() {
	if h.OnChange != nil {
		h.OnChange()
	}
}

// nilMove reports whether m, or a move inside m when m is a Group, is nil in the sense of isNil.
func nilMove[S any](m Move[S]) bool {
	if g, ok := m.(Group[S]); ok {
		for _, e := range g {
			if nilMove(e) {
				return true
			}
		}
		return false
	}
	return isNil(m)
}

// isNil reports whether m is a nil interface or a nil pointer, map, func or chan, whose methods cannot run. A nil slice is not nil here, since a slice move such as a nil Group ranges over nothing.
func isNil(m any) bool {
	if m == nil {
		return true
	}
	switch v := reflect.ValueOf(m); v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}
