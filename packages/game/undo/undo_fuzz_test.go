package undo_test

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/undo"
)

// setAt overwrites one element and remembers the old value; it is not its own inverse, so a wrong Revert order shows.
type setAt struct {
	i, v, old int
}

func (m *setAt) Apply(s *[]int) {
	i := m.i % len(*s)
	m.old = (*s)[i]
	(*s)[i] = m.v
}

func (m *setAt) Revert(s *[]int) { (*s)[m.i%len(*s)] = m.old }

type pushBack struct{ v int }

func (m pushBack) Apply(s *[]int)  { *s = append(*s, m.v) }
func (m pushBack) Revert(s *[]int) { *s = (*s)[:len(*s)-1] }

// FuzzHistory drives a History with arbitrary Do, Undo, Redo, Seek and Clear calls and checks every state against a model that keeps a copy of the state after each move.
func FuzzHistory(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{0, 0, 0, 3, 3, 3, 1, 4, 4, 4})
	f.Add([]byte{2, 9, 2, 8, 5, 0, 7, 1, 3, 3, 6, 4})
	f.Fuzz(func(t *testing.T, ops []byte) {
		state := []int{0}
		h := undo.New[*[]int, undo.Move[*[]int]](&state)
		model := [][]int{slices.Clone(state)} // model[i] is the state with i moves applied
		applied := 0
		for k, op := range ops {
			arg := int(op >> 3)
			moved := true
			switch op & 7 {
			case 0, 1:
				h.Do(pushBack{v: arg})
			case 2:
				h.Do(&setAt{i: arg, v: k})
			case 3:
				h.Do(undo.Group[*[]int]{pushBack{v: arg}, &setAt{i: arg + 1, v: -k}, pushBack{v: k}})
			case 4:
				moved = false
				if h.Undo() != (applied > 0) {
					t.Fatalf("op %d: Undo result disagrees with %d applied", k, applied)
				}
				applied = max(applied-1, 0)
			case 5:
				moved = false
				if h.Redo() != (applied < len(model)-1) {
					t.Fatalf("op %d: Redo result disagrees with %d of %d applied", k, applied, len(model)-1)
				}
				applied = min(applied+1, len(model)-1)
			case 6:
				moved = false
				applied = arg % len(model)
				h.Seek(applied)
			case 7:
				moved = false
				h.Clear()
				model = [][]int{slices.Clone(state)}
				applied = 0
			}
			if moved {
				model = append(model[:applied+1], slices.Clone(state))
				applied++
			}
			if h.Applied() != applied || h.Len() != len(model)-1 {
				t.Fatalf("op %d: history applied=%d len=%d, model %d/%d", k, h.Applied(), h.Len(), applied, len(model)-1)
			}
			if !slices.Equal(state, model[applied]) {
				t.Fatalf("op %d: state %v, model %v", k, state, model[applied])
			}
		}
		h.Seek(0)
		if !slices.Equal(state, model[0]) {
			t.Fatalf("undoing every move left %v, want %v", state, model[0])
		}
	})
}
