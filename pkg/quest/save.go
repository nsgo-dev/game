package quest

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/save"
)

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores l through save.Game. Loading validates the snapshot before mutating l, and resets
// every quest to Inactive when loading an older save slot that omits this part.
func (l *Log) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (Snapshot, error) {
			if l == nil {
				return Snapshot{}, errors.New("quest: nil log")
			}
			return l.Snapshot(), nil
		},
		func(s Snapshot) (func(), error) {
			if l == nil {
				return nil, errors.New("quest: nil log")
			}
			next, err := l.prepareRestore(s)
			if err != nil {
				return nil, err
			}
			return func() { l.quests = next }, nil
		},
		func() {
			if l != nil {
				l.quests = l.emptyProgress()
			}
		},
	)
}
