package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/inventory"
	"github.com/nsgo-dev/game/pkg/quest"
)

// saveVersion is the version of saveFile; raise it and register a save.Migration when saveFile changes so older saves still load.
const saveVersion = 1

// saveKeyPrefix is where the save slots live in the app's storage.
const saveKeyPrefix = "rpg2d.save"

// saveFile is what a save slot holds, as JSON in a versioned save.Store envelope. Foes that still stand are back at their posts after a load.
type saveFile struct {
	Map      string                   `json:"map"`
	At       Cell                     `json:"at"`
	Facing   dir                      `json:"facing"`
	HP       int                      `json:"hp"`
	Bag      inventory.ContainerState `json:"bag"`
	Gear     inventory.EquipmentState `json:"gear"`
	Quests   quest.Snapshot           `json:"quests"`
	Dialogue flow.DialogueState       `json:"dialogue"`
	Defeated []string                 `json:"defeated,omitempty"`
	Taken    []string                 `json:"taken,omitempty"`
}

// snapshot returns the playthrough as a saveFile.
func (g *Game) snapshot() saveFile {
	return saveFile{
		Map:      g.mapID,
		At:       g.pos,
		Facing:   g.facing,
		HP:       g.hp,
		Bag:      g.bag.Snapshot(),
		Gear:     g.gear.Snapshot(),
		Quests:   g.quests.Snapshot(),
		Dialogue: g.talk.Runner().State(),
		Defeated: slices.Sorted(maps.Keys(g.defeated)),
		Taken:    slices.Sorted(maps.Keys(g.taken)),
	}
}

// restore replaces the playthrough with s, leaving it as it was when s does not fit the game's data.
func (g *Game) restore(s saveFile) error {
	m := g.c.world.Map(s.Map)
	if m == nil {
		return fmt.Errorf("the save is on map %q, which the game does not have", s.Map)
	}
	if !g.c.world.inside(m, s.At) || g.c.world.solidAt(m, s.At) {
		return fmt.Errorf("the save stands at %v, which is not a walkable tile of %s", s.At, s.Map)
	}
	if s.Facing < dirUp || s.Facing > dirRight {
		return errors.New("the save faces no direction")
	}
	p := g.newProgress()
	if err := p.bag.Restore(s.Bag); err != nil {
		return err
	}
	if err := p.gear.Restore(s.Gear); err != nil {
		return err
	}
	if err := p.quests.Restore(s.Quests); err != nil {
		return err
	}
	p.talk.Runner().LoadState(s.Dialogue)
	for _, id := range s.Defeated {
		p.defeated[id] = true
	}
	for _, id := range s.Taken {
		p.taken[id] = true
	}
	applyGear(g.c, &p)
	p.hp = min(max(s.HP, 1), maxHP(&p))
	p.facing = s.Facing
	g.progress = p
	g.battle, g.won, g.grace, g.arriving = nil, false, 0, false
	g.enter(Place{Map: s.Map, At: s.At})
	return nil
}

// saveTo writes the playthrough to slot and returns to the map.
func (g *Game) saveTo(slot string) {
	if err := g.store.Save(slot, g.snapshot()); err != nil {
		g.say("Could not save: " + err.Error())
		return
	}
	g.readSlots()
	g.closeMenu()
	g.say("Saved to " + slotName(slot) + ".")
}

// loadFrom replaces the playthrough with the one in slot and goes to the map.
func (g *Game) loadFrom(slot string) {
	var s saveFile
	err := g.store.Load(slot, &s)
	if err == nil {
		err = g.restore(s)
	}
	if err != nil {
		g.say("Could not load " + slotName(slot) + ": " + err.Error())
		return
	}
	g.mode = modeField
	g.say("Loaded " + slotName(slot) + ".")
}

// slotSummary describes what slot holds, and reports whether it holds a save.
func (g *Game) slotSummary(slot string) (string, bool) {
	if !g.store.Exists(slot) {
		return "empty", false
	}
	var s saveFile
	if err := g.store.Load(slot, &s); err != nil {
		return "unreadable", false
	}
	name := s.Map
	if m := g.c.world.Map(s.Map); m != nil {
		name = m.Name
	}
	return fmt.Sprintf("%s · HP %d", name, s.HP), true
}

// slotRead is what a save slot held when readSlots last looked: slotSummary's description, and whether it holds a save.
type slotRead struct {
	summary string
	ok      bool
}

// readSlots reads every save slot's summary into g.slotsRead. The menus show the slots from there, since they are built on every fixed step and storage is too slow to read that often.
func (g *Game) readSlots() {
	g.slotsRead = g.slotsRead[:0]
	for _, slot := range slots {
		summary, ok := g.slotSummary(slot)
		g.slotsRead = append(g.slotsRead, slotRead{summary, ok})
	}
}

// anySave reports whether any slot held a save when readSlots last looked.
func (g *Game) anySave() bool {
	for _, r := range g.slotsRead {
		if r.ok {
			return true
		}
	}
	return false
}

// slotName is how messages name slot: "slot 2".
func slotName(slot string) string {
	return "slot " + slot[len("slot"):]
}
