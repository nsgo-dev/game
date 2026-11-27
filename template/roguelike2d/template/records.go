package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/ability"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/loot"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/save"
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

// runSave is the run slot: everything a run holds, taken as the player arrives on a floor and after every turn. Resuming regenerates the floor's cells from the seed and restores the rest, so the run carries on exactly where it stood, with the same monsters, items and dice rolls to come. Permadeath is the slot's lifetime: it is deleted the moment the run is won or lost, so a dead run cannot be loaded again, and quitting before a blow lands does not undo it, because every turn the player took is already in the slot.
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
	// Position is where the player stands on the floor.
	Position Position `json:"position"`
	// Seen marks the floor's cells the player has seen, one bit per cell in row order, as encoding/json writes a []byte: base64.
	Seen []byte `json:"seen"`
	// Actors is the floor's world of monsters and items, as entities.World.Snapshot writes it.
	Actors entities.WorldSnapshot `json:"actors"`
	// Combat and Drops are where the floor's combat stream and the loot roller's stream have got to, so the rolls to come are the ones the run would have had.
	Combat random.State `json:"combat"`
	Drops  random.State `json:"drops"`
}

// maxSaved bounds every count the run slot holds, far above what a run reaches, so a corrupt slot cannot overflow the arithmetic the rules do with them.
const maxSaved = 1 << 24

// maxLevel bounds the level the run slot holds, for the same reason.
const maxLevel = 999

// tonicSave is one tonic still working.
type tonicSave struct {
	Effect    string `json:"effect"`
	Remaining int    `json:"remaining"`
}

// save returns the run's state as the run slot holds it.
func (r *run) save() runSave {
	s := runSave{
		Seed: r.seed, Floor: r.floor.index, Health: r.health, Level: r.level, XP: r.xp,
		Draughts: r.draughts, Shards: r.shards, Kills: r.kills, Turns: r.turns,
		Weapon: r.weapon.ID, Abilities: r.caster.Snapshot(), Loot: r.roller.State(),
		Position: positionOf(r.pos), Seen: packBits(r.floor.seen),
		Combat: r.combat.State(), Drops: r.roller.Stream().State(),
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
	actors, err := r.floor.actors.Snapshot()
	if err != nil {
		// Every component has a codec and plain fields, so a snapshot cannot fail.
		panic(err)
	}
	s.Actors = actors
	return s
}

// check reports the first number of s outside what a run can reach.
func (s runSave) check(d *Dungeon) error {
	p := d.Player
	for _, v := range []struct {
		name     string
		got, max int
		min      int
	}{
		{"floor", s.Floor, len(d.Floors) - 1, 0},
		{"level", s.Level, maxLevel, 1},
		{"health", s.Health, maxSaved, 1},
		{"xp", s.XP, s.Level*p.LevelXP - 1, 0},
		{"draughts", s.Draughts, maxSaved, 0},
		{"shards", s.Shards, maxSaved, 0},
		{"kills", s.Kills, maxSaved, 0},
		{"turns", s.Turns, maxSaved, 0},
	} {
		if v.got < v.min || v.got > v.max {
			return fmt.Errorf("run slot: %s %d is outside %d to %d", v.name, v.got, v.min, v.max)
		}
	}
	return nil
}

// resumeRun rebuilds the run s was saved from, where it stood when it was saved.
func resumeRun(data *gameData, s runSave, persist func(runSave) error) (*run, error) {
	d := data.dungeon
	if err := s.check(d); err != nil {
		return nil, err
	}
	r, err := newRunState(data, s.Seed, persist)
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

	// The floor's cells come from the seed; its actors, what the player has seen and where the dice have got to come from the slot. The roller generation spends is a scratch one, since the slot's world replaces the items it lays out.
	f, err := generateFloor(d, s.Floor, s.Seed, loot.NewRoller(data.loot, floorStream(s.Seed, s.Floor, "loot")))
	if err != nil {
		return nil, err
	}
	if err := f.actors.Restore(s.Actors); err != nil {
		return nil, fmt.Errorf("run slot: %w", err)
	}
	if err := f.checkActors(d); err != nil {
		return nil, fmt.Errorf("run slot: %w", err)
	}
	if !unpackBits(s.Seen, f.seen) {
		return nil, fmt.Errorf("run slot: seen holds %d bytes, want %d", len(s.Seen), (len(f.seen)+7)/8)
	}
	pos := s.Position.cell()
	if _, taken := f.foeAt(pos); !f.walkable(pos) || taken {
		return nil, fmt.Errorf("run slot: the player cannot stand on %v", pos)
	}
	r.floor, r.pos = f, pos
	r.combat = floorStream(s.Seed, s.Floor, "combat")
	r.combat.SetState(s.Combat)
	r.roller.Stream().SetState(s.Drops)
	r.visible = make([]bool, len(f.grid.Cells))
	r.look()
	r.say(fmt.Sprintf("You return to %s, floor %d: %s.", d.Name, s.Floor+1, f.def.Name))
	return r, nil
}

// packBits packs bits into bytes, eight to a byte, lowest bit first.
func packBits(bits []bool) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b {
			out[i/8] |= 1 << (i % 8)
		}
	}
	return out
}

// unpackBits fills dst from packed, as packBits wrote it, and reports whether packed has the length dst needs.
func unpackBits(packed []byte, dst []bool) bool {
	if len(packed) != (len(dst)+7)/8 {
		return false
	}
	for i := range dst {
		dst[i] = packed[i/8]&(1<<(i%8)) != 0
	}
	return true
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
