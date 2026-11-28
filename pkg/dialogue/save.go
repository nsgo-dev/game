package dialogue

import (
	"errors"
	"maps"

	"github.com/nsgo-dev/game/pkg/save"
)

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and restores r's variables and node visit counts through save.Game. Loading stages the maps before mutating r, and clears variables and visit counts when loading an older save slot that omits it.
func (r *Runner) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (State, error) {
			if r == nil {
				return State{}, errors.New("dialogue: nil runner")
			}
			return r.State(), nil
		},
		func(st State) (func(), error) {
			if r == nil {
				return nil, errors.New("dialogue: nil runner")
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
