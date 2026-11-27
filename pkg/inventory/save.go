package inventory

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/save"
)

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores c through save.Game. Loading validates every slot before mutating c, and resets c to
// empty when loading an older save slot that omits this part.
func (c *Container) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (ContainerState, error) {
			if c == nil {
				return ContainerState{}, errors.New("inventory: nil container")
			}
			return c.Snapshot(), nil
		},
		func(st ContainerState) (func(), error) {
			if c == nil {
				return nil, errors.New("inventory: nil container")
			}
			slots, err := c.prepareRestore(st)
			if err != nil {
				return nil, err
			}
			return func() { c.slots = slots }, nil
		},
		func() {
			if c != nil {
				c.slots = make([]Stack, len(c.slots))
			}
		},
	)
}

// SavePart returns a save.Part (implementing save.StagedPart and save.Resetter) that saves and
// restores e through save.Game. Loading validates every slot before mutating e, and resets e to
// empty when loading an older save slot that omits this part.
func (e *Equipment) SavePart() save.Part {
	return save.StagedFuncPart(
		func() (EquipmentState, error) {
			if e == nil {
				return EquipmentState{}, errors.New("inventory: nil equipment")
			}
			return e.Snapshot(), nil
		},
		func(st EquipmentState) (func(), error) {
			if e == nil {
				return nil, errors.New("inventory: nil equipment")
			}
			items, err := e.prepareRestore(st)
			if err != nil {
				return nil, err
			}
			return func() { e.items = items }, nil
		},
		func() {
			if e != nil {
				e.items = make([]string, len(e.slots))
			}
		},
	)
}
