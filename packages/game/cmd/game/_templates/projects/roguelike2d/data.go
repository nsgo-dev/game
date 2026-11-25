package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/ability"
	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/stats"
)

// The game's data files, embedded so the game runs the same on every target without reading files at run time. Edit them and rebuild; the tests check that each one is valid and canonical and that they agree with each other.
var (
	// dungeonYAML is the dungeon: the player's progression, the floors, the monsters and the items, in the format Dungeon describes.
	//
	//go:embed data/dungeon.yaml
	dungeonYAML []byte
	// statsYAML defines the player's stats and the effects items apply to them, in package stats' format.
	//
	//go:embed data/stats.yaml
	statsYAML []byte
	// lootYAML holds the weighted drop tables floors and monsters roll, in package loot's format.
	//
	//go:embed data/loot.yaml
	lootYAML []byte
	// abilitiesYAML holds the player's abilities, in package ability's format. The game runs its Caster at one step a second, so every duration in the file is a number of turns: a cooldown of 8s lasts eight turns.
	//
	//go:embed data/abilities.yaml
	abilitiesYAML []byte
)

// The abilities the game gives an effect to; data/abilities.yaml must define both.
const (
	abilityQuaff  = "quaff"
	abilityCleave = "cleave"
)

// The resource the quaff ability spends and the conditions the abilities test, as data/abilities.yaml names them.
const (
	resourceDraughts = "draughts"
	conditionHurt    = "hurt"
	conditionFoes    = "foes-adjacent"
)

// The stats the game reads, which data/stats.yaml must define.
const (
	statMaxHealth = "maxHealth"
	statAttack    = "attack"
	statDefense   = "defense"
)

// gameData is the parsed contents of the data files.
type gameData struct {
	dungeon   *Dungeon
	stats     *stats.File
	lootDoc   *loot.Document
	loot      *loot.Library
	abilities *ability.Set
}

// loadData parses the embedded data files and checks the names each one uses from the others.
func loadData() (*gameData, error) {
	return parseData(dungeonYAML, statsYAML, lootYAML, abilitiesYAML)
}

// parseData parses the four data files and checks the references between them.
func parseData(dungeonFile, statsFile, lootFile, abilitiesFile []byte) (*gameData, error) {
	d, err := parseDungeon(dungeonFile)
	if err != nil {
		return nil, fmt.Errorf("data/dungeon.yaml: %w", err)
	}
	sf, err := stats.Parse(statsFile)
	if err != nil {
		return nil, fmt.Errorf("data/stats.yaml: %w", err)
	}
	doc, err := loot.Parse(lootFile)
	if err != nil {
		return nil, fmt.Errorf("data/loot.yaml: %w", err)
	}
	lib, err := loot.New(doc)
	if err != nil {
		return nil, fmt.Errorf("data/loot.yaml: %w", err)
	}
	set, err := ability.Parse(abilitiesFile)
	if err != nil {
		return nil, fmt.Errorf("data/abilities.yaml: %w", err)
	}
	g := &gameData{dungeon: d, stats: sf, lootDoc: doc, loot: lib, abilities: set}
	if err := g.check(); err != nil {
		return nil, err
	}
	return g, nil
}

// check reports the first name one data file uses that another does not define.
func (g *gameData) check() error {
	sheet, err := g.stats.NewSheet()
	if err != nil {
		return fmt.Errorf("data/stats.yaml: %w", err)
	}
	for _, s := range []string{statMaxHealth, statAttack, statDefense} {
		if !sheet.Has(s) {
			return fmt.Errorf("data/stats.yaml: no %s stat", s)
		}
	}
	for _, it := range g.dungeon.Items {
		if it.Effect == "" {
			continue
		}
		e, ok := g.stats.Effect(it.Effect)
		if !ok {
			return fmt.Errorf("data/dungeon.yaml: item %q: no effect %q in data/stats.yaml", it.ID, it.Effect)
		}
		if it.Kind == kindTonic && e.Steps == 0 {
			return fmt.Errorf("data/dungeon.yaml: item %q: the tonic's effect %q has no steps, so it would never wear off", it.ID, it.Effect)
		}
		if it.Kind != kindTonic && e.Steps != 0 {
			return fmt.Errorf("data/dungeon.yaml: item %q: the %s's effect %q has steps, so it would wear off while equipped", it.ID, it.Kind, it.Effect)
		}
	}
	for i, f := range g.dungeon.Floors {
		if f.Loot != "" && !g.loot.Has(f.Loot) {
			return fmt.Errorf("data/dungeon.yaml: floors[%d]: no loot table %q in data/loot.yaml", i, f.Loot)
		}
	}
	for _, m := range g.dungeon.Monsters {
		if m.Drops != "" && !g.loot.Has(m.Drops) {
			return fmt.Errorf("data/dungeon.yaml: monster %q: no loot table %q in data/loot.yaml", m.ID, m.Drops)
		}
	}
	for _, t := range g.lootDoc.Tables {
		for _, e := range append(append([]loot.Entry(nil), t.Guaranteed...), t.Entries...) {
			if e.Item == "" {
				continue
			}
			if _, ok := g.dungeon.item(e.Item); !ok {
				return fmt.Errorf("data/loot.yaml: table %q drops %q, which data/dungeon.yaml has no item for", t.Name, e.Item)
			}
		}
	}
	for _, id := range []string{abilityQuaff, abilityCleave} {
		found := false
		for _, a := range g.abilities.Abilities {
			found = found || a.ID == id
		}
		if !found {
			return fmt.Errorf("data/abilities.yaml: no %s ability", id)
		}
	}
	return nil
}

// mustLoadData is loadData for data embedded in the binary, which the tests keep valid.
func mustLoadData() *gameData {
	d, err := loadData()
	if err != nil {
		panic(err)
	}
	return d
}
