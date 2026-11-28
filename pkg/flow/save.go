package flow

import (
	"errors"

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
