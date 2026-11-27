package flow

import (
	"errors"
	"maps"

	"github.com/nsgo-dev/game/pkg/save"
)

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores m through save.Game. Loading validates the snapshot before mutating m, and resets m to
// its unstarted initial state when loading an older save slot that omits this part.
func (m *Machine[S]) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (MachineSnapshot[S], error) {
			if m == nil {
				return MachineSnapshot[S]{}, errors.New("flow: nil machine")
			}
			return m.Snapshot(), nil
		},
		func(snap MachineSnapshot[S]) (func(), error) {
			if err := m.validateRestore(snap); err != nil {
				return nil, err
			}
			return func() { m.commitRestore(snap) }, nil
		},
		func() {
			if m != nil {
				m.resetState()
			}
		},
	)
}

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores r's variables and node visit counts through save.Game. Loading stages the maps before
// mutating r, and clears variables and visit counts when loading an older save slot that omits it.
func (r *DialogueRunner) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (DialogueState, error) {
			if r == nil {
				return DialogueState{}, errors.New("flow: nil dialogue runner")
			}
			return r.State(), nil
		},
		func(st DialogueState) (func(), error) {
			if r == nil {
				return nil, errors.New("flow: nil dialogue runner")
			}
			vars := make(map[string]any, len(st.Vars))
			for k, v := range st.Vars {
				vars[normalizeVarName(k)] = normalizeDialogueValue(v)
			}
			visited := make(map[string]int, len(st.Visited))
			maps.Copy(visited, st.Visited)
			return func() {
				r.vars = vars
				r.visited = visited
			}, nil
		},
		func() {
			if r != nil {
				r.vars = make(map[string]any)
				r.visited = make(map[string]int)
			}
		},
	)
}
