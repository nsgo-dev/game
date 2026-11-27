package ability

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/save"
)

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores c through save.Game. Loading validates the snapshot before mutating c, and resets
// charges, cooldowns and active casts to their initial state when loading an older slot that omits it.
func (c *Caster) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (State, error) {
			if c == nil {
				return State{}, errors.New("ability: nil caster")
			}
			return c.Snapshot(), nil
		},
		func(st State) (func(), error) {
			if c == nil {
				return nil, errors.New("ability: nil caster")
			}
			p, err := c.prepareRestore(&st)
			if err != nil {
				return nil, err
			}
			return func() { c.commitRestore(p) }, nil
		},
		func() {
			if c != nil {
				c.resetState()
			}
		},
	)
}
