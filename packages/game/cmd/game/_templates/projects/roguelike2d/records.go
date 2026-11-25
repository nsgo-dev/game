package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/ability"
	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
)

// The save slots, kept in the app's storage under keys starting with savePrefix: the run in progress, which death or victory deletes, and the records of every run that ended.
const (
	savePrefix  = "roguelike2d"
	runSlot     = "run"
	recordsSlot = "records"
	saveVersion = 1
)

// newSaveStore returns the store both slots are kept in. Raise saveVersion and give the store a save.WithMigration when either slot changes shape.
func newSaveStore(store core.Storage) *save.Store {
	return save.NewStorageStore(store, savePrefix, save.WithVersion(saveVersion))
}

// runSave is the run slot: everything a run carries from one floor to the next, taken as the player arrives on a floor. Resuming regenerates that floor from the seed, so the slot needs no cells, monsters or items. Permadeath is the slot's lifetime: it is deleted the moment the run is won or lost, so a dead run cannot be loaded again.
type runSave struct {
	Seed     uint64 `json:"seed"`
	Floor    int    `json:"floor"`
	Health   int    `json:"health"`
	Level    int    `json:"level"`
	XP       int    `json:"xp"`
	Draughts int    `json:"draughts"`
	Shards   int    `json:"shards"`
	Kills    int    `json:"kills"`
	Turns    int    `json:"turns"`
	Weapon   string `json:"weapon"`
	Armor    string `json:"armor,omitempty"`
	// Tonics are the tonics still working, with the turns each has left.
	Tonics []tonicSave `json:"tonics,omitempty"`
	// Abilities is the caster's cooldowns and charges.
	Abilities ability.State `json:"abilities"`
	// Loot is the loot roller's pity counters, which carry from floor to floor.
	Loot loot.State `json:"loot"`
}

// tonicSave is one tonic still working.
type tonicSave struct {
	Effect    string `json:"effect"`
	Remaining int    `json:"remaining"`
}

// save returns the run's state as the run slot holds it, for arriving on floor index.
func (r *run) save(index int) runSave {
	s := runSave{
		Seed: r.seed, Floor: index, Health: r.health, Level: r.level, XP: r.xp,
		Draughts: r.draughts, Shards: r.shards, Kills: r.kills, Turns: r.turns,
		Weapon: r.weapon.ID, Abilities: r.caster.Snapshot(), Loot: r.roller.State(),
	}
	if r.armor != nil {
		s.Armor = r.armor.ID
	}
	seen := map[string]bool{}
	for _, m := range r.sheet.Modifiers() {
		if name, ok := strings.CutPrefix(m.Source, tonicSource); ok && !seen[name] {
			seen[name] = true
			s.Tonics = append(s.Tonics, tonicSave{Effect: name, Remaining: m.Remaining})
		}
	}
	return s
}

// resumeRun rebuilds the run s was saved from, on the floor it was saved arriving on.
func resumeRun(data *gameData, s runSave, onFloor func(runSave) error) (*run, error) {
	d := data.dungeon
	if s.Floor < 0 || s.Floor >= len(d.Floors) || s.Level < 1 || s.Health < 1 {
		return nil, fmt.Errorf("run slot: floor %d, level %d, health %d are out of range", s.Floor, s.Level, s.Health)
	}
	r, err := newRunState(data, s.Seed, onFloor)
	if err != nil {
		return nil, err
	}
	r.level, r.xp, r.draughts, r.shards, r.kills, r.turns = s.Level, s.XP, s.Draughts, s.Shards, s.Kills, s.Turns
	r.applyLevel()
	for _, slot := range []struct {
		id, kind string
	}{{s.Weapon, kindWeapon}, {s.Armor, kindArmor}} {
		if slot.id == "" && slot.kind == kindArmor {
			continue
		}
		it, ok := d.item(slot.id)
		if !ok || it.Kind != slot.kind {
			return nil, fmt.Errorf("run slot: %q is not a %s", slot.id, slot.kind)
		}
		r.equip(it)
	}
	for _, t := range s.Tonics {
		e, ok := data.stats.Effect(t.Effect)
		if !ok || t.Remaining < 1 || t.Remaining > e.Steps {
			return nil, fmt.Errorf("run slot: tonic %q with %d turns left", t.Effect, t.Remaining)
		}
		e.Steps = t.Remaining
		if _, err := r.sheet.Refresh(tonicSource+t.Effect, e); err != nil {
			return nil, err
		}
	}
	if err := r.caster.Restore(&s.Abilities); err != nil {
		return nil, fmt.Errorf("run slot: %w", err)
	}
	if err := r.roller.SetState(s.Loot); err != nil {
		return nil, fmt.Errorf("run slot: %w", err)
	}
	r.health = min(s.Health, r.maxHealth())
	r.say(fmt.Sprintf("You return to %s.", d.Name))
	// The slot already holds this floor's arrival, so entering it again need not write it.
	r.onFloor = nil
	if err := r.enterFloor(s.Floor); err != nil {
		return nil, err
	}
	r.onFloor = onFloor
	return r, nil
}

// summary is how a run ended, for the end-of-run panel and the records.
type summary struct {
	Won    bool   `json:"won"`
	Floor  int    `json:"floor"`
	Floors int    `json:"floors"`
	Turns  int    `json:"turns"`
	Kills  int    `json:"kills"`
	Level  int    `json:"level"`
	Shards int    `json:"shards"`
	Killer string `json:"killer,omitempty"`
	Seed   uint64 `json:"seed"`
}

// summarize returns how r ended.
func (r *run) summarize() summary {
	return summary{
		Won: r.state == won, Floor: r.floor.index + 1, Floors: len(r.data.dungeon.Floors),
		Turns: r.turns, Kills: r.kills, Level: r.level, Shards: r.shards, Killer: r.killer, Seed: r.seed,
	}
}

// headline is the summary's title.
func (s summary) headline() string {
	if s.Won {
		return "The Heart Ember is yours"
	}
	return fmt.Sprintf("Slain on floor %d", s.Floor)
}

// cause says what ended the run.
func (s summary) cause() string {
	if s.Won {
		return fmt.Sprintf("You climbed out of all %d floors.", s.Floors)
	}
	return fmt.Sprintf("A %s ended the run.", s.Killer)
}

// tally lists the run's numbers on one line.
func (s summary) tally() string {
	return fmt.Sprintf("Level %d · %s · %s · %s", s.Level, plural(s.Turns, "turn"), plural(s.Kills, "foe"), plural(s.Shards, "shard"))
}

// records is the records slot: every run that ended, counted.
type records struct {
	Runs       int      `json:"runs"`
	Wins       int      `json:"wins"`
	Deepest    int      `json:"deepest"`
	BestShards int      `json:"bestShards"`
	Last       *summary `json:"last,omitempty"`
}

// add counts the run s summarizes.
func (rec *records) add(s summary) {
	rec.Runs++
	if s.Won {
		rec.Wins++
	}
	rec.Deepest = max(rec.Deepest, s.Floor)
	rec.BestShards = max(rec.BestShards, s.Shards)
	rec.Last = &s
}

// line lists the records on one line.
func (rec records) line() string {
	return fmt.Sprintf("Runs %d · Wins %d · Deepest floor %d · Most shards %d", rec.Runs, rec.Wins, rec.Deepest, rec.BestShards)
}

// loadRecords reads the records slot; a missing or unreadable slot is no records, and a slot from a newer version of the game is reported, so it is left for that version rather than overwritten.
func loadRecords(st *save.Store) (rec records, newer bool) {
	if err := st.Load(recordsSlot, &rec); err != nil {
		return records{}, errors.Is(err, save.ErrFutureVersion)
	}
	return rec, false
}

// loadRun reads the run slot, reporting ok false when there is no run to resume, and newer when the slot holds a run from a newer version of the game, which this one must leave alone.
func loadRun(st *save.Store) (s runSave, ok, newer bool) {
	if err := st.Load(runSlot, &s); err != nil {
		return runSave{}, false, errors.Is(err, save.ErrFutureVersion)
	}
	return s, true, false
}
