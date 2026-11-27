package entities

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/save"
)

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores w through save.Game. Loading decodes and validates every entity, component codec and
// hierarchy link into staging state before committing to w, and clears w when loading an older
// save slot that omits this part.
func (w *World) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (WorldSnapshot, error) {
			return w.Snapshot()
		},
		func(snap WorldSnapshot) (func(), error) {
			if w == nil {
				return nil, errors.New("entities: nil world")
			}
			if w.disposed {
				return nil, ErrWorldDisposed
			}
			r, err := w.prepareRestore(snap)
			if err != nil {
				return nil, err
			}
			return func() {
				_ = w.commitRestore(r)
			}, nil
		},
		func() {
			if w != nil && !w.disposed {
				if r, err := w.prepareRestore(WorldSnapshot{}); err == nil {
					_ = w.commitRestore(r)
				}
			}
		},
	)
}
