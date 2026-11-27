package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// dungeonVersion is the dungeon file schema version this game reads and writes.
const dungeonVersion = 1

// The generators a floor can be built with, from package procgen.
const (
	generatorBSP  = "bsp"
	generatorCave = "cave"
)

// The kinds of item a run can find.
const (
	// kindWeapon is equipped in the weapon slot when it outranks the weapon held; its stats effect is the bonus it gives.
	kindWeapon = "weapon"
	// kindArmor is equipped in the armor slot when it outranks the armor worn.
	kindArmor = "armor"
	// kindDraught adds the drop's count to the draughts the quaff ability spends.
	kindDraught = "draught"
	// kindTonic applies its timed stats effect at once.
	kindTonic = "tonic"
	// kindShards adds the drop's count to the run's shards, its score.
	kindShards = "shards"
)

// Limits that keep a dungeon file small enough to generate and play at once.
const (
	maxFloors    = 20
	minFloorSide = 16
	maxFloorSide = 96
	maxSpawns    = 64
	maxDefs      = 64
)

// Dungeon is the dungeon file, data/dungeon.yaml: the player's progression, the floors a run descends through, the monsters that live on them and the items they hold. The stats, loot tables and abilities it names live in their own files. Parse it with parseDungeon and write it with encodeDungeon, which gives back the same bytes for a file it wrote.
type Dungeon struct {
	// Version is the schema version; it must be 1.
	Version int `yaml:"version"`
	// Name is the dungeon's display name.
	Name string `yaml:"name"`
	// Player is how the player starts and grows.
	Player PlayerDef `yaml:"player"`
	// Floors are the floors in the order a run descends them. Every floor but the last has stairs down; the last holds the relic, and taking it wins the run.
	Floors []FloorDef `yaml:"floors"`
	// Monsters are the kinds of monster the floors spawn.
	Monsters []MonsterDef `yaml:"monsters"`
	// Items are the items the loot tables drop.
	Items []ItemDef `yaml:"items"`
}

// PlayerDef is how the player starts a run and grows during it. Health, attack and defense are stats in data/stats.yaml.
type PlayerDef struct {
	// Weapon is the item the player starts with, a weapon from Items.
	Weapon string `yaml:"weapon"`
	// Draughts is how many draughts a run starts with.
	Draughts int `yaml:"draughts"`
	// DraughtHeal is how much health quaffing a draught restores.
	DraughtHeal int `yaml:"draughtHeal"`
	// LevelXP is the experience each level costs: level n needs n times LevelXP more to reach level n+1.
	LevelXP int `yaml:"levelXP"`
	// LevelHealth is how much a level raises the maxHealth stat's base, and heals.
	LevelHealth int `yaml:"levelHealth"`
	// LevelAttack is how much a level raises the attack stat's base.
	LevelAttack int `yaml:"levelAttack"`
	// RegenTurns is how many turns the player takes to recover 1 health; 0 turns recovery off, leaving draughts and levels as the only healing.
	RegenTurns int `yaml:"regenTurns,omitempty"`
}

// FloorDef is one floor of the dungeon.
type FloorDef struct {
	// Name is the floor's display name.
	Name string `yaml:"name"`
	// Generator is bsp, rooms joined by corridors, or cave, a cellular-automaton cave.
	Generator string `yaml:"generator"`
	// Width and Height are the floor's size in cells, from 16 to 96 each.
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
	// Monsters is how many monsters the floor spawns, picked by weight among those whose floors include it.
	Monsters int `yaml:"monsters"`
	// Items is how many times the floor rolls its Loot table, each roll laid on a cell of its own.
	Items int `yaml:"items"`
	// Loot is the loot table, in data/loot.yaml, that fills the floor's item cells.
	Loot string `yaml:"loot"`
	// Guardian, when set, is a monster that stands on the floor's exit, so the stairs or the relic are reached only by defeating it.
	Guardian string `yaml:"guardian,omitempty"`
}

// MonsterDef is one kind of monster.
type MonsterDef struct {
	// ID names the monster to floors' Guardian; lower-case letters, digits and '-'.
	ID string `yaml:"id"`
	// Name is its display name, used in the message log and the run summary.
	Name string `yaml:"name"`
	// Glyph is the letter it is drawn as.
	Glyph string `yaml:"glyph"`
	// Color is its colour, as #rrggbb.
	Color string `yaml:"color"`
	// Health, Attack and Defense are its combat numbers.
	Health  int `yaml:"health"`
	Attack  int `yaml:"attack"`
	Defense int `yaml:"defense"`
	// XP is the experience defeating it gives.
	XP int `yaml:"xp"`
	// Sense is the walking distance, in cells, at which it notices the player and starts to hunt; once hunting it does not stop.
	Sense int `yaml:"sense"`
	// Drops, when set, is the loot table rolled where it falls.
	Drops string `yaml:"drops,omitempty"`
	// From and To are the first and last floors, counted from 1, that spawn it; 0 for both keeps it out of the random spawns, for a guardian.
	From int `yaml:"from,omitempty"`
	To   int `yaml:"to,omitempty"`
	// Weight is its relative chance among the monsters a floor can spawn.
	Weight float64 `yaml:"weight,omitempty"`
}

// ItemDef is one kind of item.
type ItemDef struct {
	// ID names the item to the loot tables and the player's Weapon; lower-case letters, digits and '-'.
	ID string `yaml:"id"`
	// Name is its display name.
	Name string `yaml:"name"`
	// Kind is weapon, armor, draught, tonic or shards.
	Kind string `yaml:"kind"`
	// Effect is the stats effect, in data/stats.yaml, that a weapon, armor or tonic applies.
	Effect string `yaml:"effect,omitempty"`
	// Rank orders weapons and armor: a found one is equipped only when it outranks the one in its slot.
	Rank int `yaml:"rank,omitempty"`
	// Color is its colour on the floor, as #rrggbb.
	Color string `yaml:"color"`
}

// parseDungeon decodes and validates a dungeon file, refusing unknown fields, duplicate keys and trailing documents.
func parseDungeon(data []byte) (*Dungeon, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("dungeon: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var d Dungeon
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("dungeon: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("dungeon: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("dungeon: decode: %w", err)
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// encodeDungeon validates d and writes it as 2-space YAML with its fields in a fixed order, so parseDungeon reads back the same Dungeon and encoding that again gives the same bytes.
func encodeDungeon(d *Dungeon) ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("dungeon: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("dungeon: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// validate reports the first way d breaks the dungeon schema. References into the other data files are checked by loadData, which has them.
func (d *Dungeon) validate() error {
	if d.Version != dungeonVersion {
		return fmt.Errorf("dungeon: unsupported version %d (want %d)", d.Version, dungeonVersion)
	}
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("dungeon: name is empty")
	}
	if len(d.Floors) == 0 || len(d.Floors) > maxFloors {
		return fmt.Errorf("dungeon: %d floors, want 1 to %d", len(d.Floors), maxFloors)
	}
	if len(d.Monsters) == 0 || len(d.Monsters) > maxDefs {
		return fmt.Errorf("dungeon: %d monsters, want 1 to %d", len(d.Monsters), maxDefs)
	}
	if len(d.Items) == 0 || len(d.Items) > maxDefs {
		return fmt.Errorf("dungeon: %d items, want 1 to %d", len(d.Items), maxDefs)
	}
	items := map[string]*ItemDef{}
	for i := range d.Items {
		it := &d.Items[i]
		if err := it.validate(); err != nil {
			return fmt.Errorf("dungeon: items[%d]: %w", i, err)
		}
		if items[it.ID] != nil {
			return fmt.Errorf("dungeon: items[%d]: duplicate id %q", i, it.ID)
		}
		items[it.ID] = it
	}
	monsters := map[string]*MonsterDef{}
	for i := range d.Monsters {
		m := &d.Monsters[i]
		if err := m.validate(len(d.Floors)); err != nil {
			return fmt.Errorf("dungeon: monsters[%d]: %w", i, err)
		}
		if monsters[m.ID] != nil {
			return fmt.Errorf("dungeon: monsters[%d]: duplicate id %q", i, m.ID)
		}
		monsters[m.ID] = m
	}
	for i := range d.Floors {
		f := &d.Floors[i]
		if err := f.validate(); err != nil {
			return fmt.Errorf("dungeon: floors[%d]: %w", i, err)
		}
		if f.Guardian != "" && monsters[f.Guardian] == nil {
			return fmt.Errorf("dungeon: floors[%d]: unknown guardian %q", i, f.Guardian)
		}
		if f.Monsters > 0 && len(d.spawnable(i+1)) == 0 {
			return fmt.Errorf("dungeon: floors[%d]: spawns %d monsters but no monster lives on floor %d", i, f.Monsters, i+1)
		}
	}
	if err := d.Player.validate(); err != nil {
		return fmt.Errorf("dungeon: player: %w", err)
	}
	if w := items[d.Player.Weapon]; w == nil || w.Kind != kindWeapon {
		return fmt.Errorf("dungeon: player: weapon %q is not a weapon in items", d.Player.Weapon)
	}
	return nil
}

func (p *PlayerDef) validate() error {
	switch {
	case p.Draughts < 0 || p.Draughts > 99:
		return fmt.Errorf("draughts %d is outside 0 to 99", p.Draughts)
	case p.DraughtHeal < 1 || p.DraughtHeal > 999:
		return fmt.Errorf("draughtHeal %d is outside 1 to 999", p.DraughtHeal)
	case p.LevelXP < 1 || p.LevelXP > 9999:
		return fmt.Errorf("levelXP %d is outside 1 to 9999", p.LevelXP)
	case p.LevelHealth < 0 || p.LevelHealth > 999:
		return fmt.Errorf("levelHealth %d is outside 0 to 999", p.LevelHealth)
	case p.LevelAttack < 0 || p.LevelAttack > 99:
		return fmt.Errorf("levelAttack %d is outside 0 to 99", p.LevelAttack)
	case p.RegenTurns < 0 || p.RegenTurns > 999:
		return fmt.Errorf("regenTurns %d is outside 0 to 999", p.RegenTurns)
	}
	return nil
}

func (f *FloorDef) validate() error {
	switch {
	case strings.TrimSpace(f.Name) == "":
		return errors.New("name is empty")
	case f.Generator != generatorBSP && f.Generator != generatorCave:
		return fmt.Errorf("generator %q is not bsp or cave", f.Generator)
	case f.Width < minFloorSide || f.Width > maxFloorSide || f.Height < minFloorSide || f.Height > maxFloorSide:
		return fmt.Errorf("size %dx%d is outside %d to %d a side", f.Width, f.Height, minFloorSide, maxFloorSide)
	case f.Monsters < 0 || f.Monsters > maxSpawns:
		return fmt.Errorf("monsters %d is outside 0 to %d", f.Monsters, maxSpawns)
	case f.Items < 0 || f.Items > maxSpawns:
		return fmt.Errorf("items %d is outside 0 to %d", f.Items, maxSpawns)
	case f.Items > 0 && f.Loot == "":
		return errors.New("items without a loot table")
	}
	return nil
}

func (m *MonsterDef) validate(floors int) error {
	if err := checkID(m.ID); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(m.Name) == "":
		return errors.New("name is empty")
	case len(m.Glyph) != 1 || !isLetter(m.Glyph[0]):
		return fmt.Errorf("glyph %q is not one ASCII letter", m.Glyph)
	case m.Health < 1 || m.Health > 9999:
		return fmt.Errorf("health %d is outside 1 to 9999", m.Health)
	case m.Attack < 0 || m.Attack > 999 || m.Defense < 0 || m.Defense > 999:
		return errors.New("attack and defense must be 0 to 999")
	case m.XP < 0 || m.XP > 9999:
		return fmt.Errorf("xp %d is outside 0 to 9999", m.XP)
	case m.Sense < 1 || m.Sense > maxFloorSide:
		return fmt.Errorf("sense %d is outside 1 to %d", m.Sense, maxFloorSide)
	}
	if err := checkColor(m.Color); err != nil {
		return err
	}
	if m.From == 0 && m.To == 0 {
		if m.Weight != 0 {
			return errors.New("weight without floors to spawn on")
		}
		return nil
	}
	if m.From < 1 || m.To < m.From || m.To > floors {
		return fmt.Errorf("floors %d to %d are not within 1 to %d", m.From, m.To, floors)
	}
	if !(m.Weight > 0 && m.Weight <= 1e6) {
		return fmt.Errorf("weight %v is outside (0, 1e6]", m.Weight)
	}
	return nil
}

func (it *ItemDef) validate() error {
	if err := checkID(it.ID); err != nil {
		return err
	}
	if strings.TrimSpace(it.Name) == "" {
		return errors.New("name is empty")
	}
	switch it.Kind {
	case kindWeapon, kindArmor:
		if it.Effect == "" || it.Rank < 1 || it.Rank > 99 {
			return fmt.Errorf("%s needs an effect and a rank from 1 to 99", it.Kind)
		}
	case kindTonic:
		if it.Effect == "" || it.Rank != 0 {
			return errors.New("tonic needs an effect and no rank")
		}
	case kindDraught, kindShards:
		if it.Effect != "" || it.Rank != 0 {
			return fmt.Errorf("%s takes no effect or rank", it.Kind)
		}
	default:
		return fmt.Errorf("kind %q is not weapon, armor, draught, tonic or shards", it.Kind)
	}
	return checkColor(it.Color)
}

// spawnable returns the indexes of the monsters that spawn on floor n, counted from 1.
func (d *Dungeon) spawnable(n int) []int {
	var out []int
	for i, m := range d.Monsters {
		if m.From != 0 && m.From <= n && n <= m.To {
			out = append(out, i)
		}
	}
	return out
}

// monster returns the monster with the given id.
func (d *Dungeon) monster(id string) (*MonsterDef, bool) {
	for i := range d.Monsters {
		if d.Monsters[i].ID == id {
			return &d.Monsters[i], true
		}
	}
	return nil, false
}

// item returns the item with the given id.
func (d *Dungeon) item(id string) (*ItemDef, bool) {
	for i := range d.Items {
		if d.Items[i].ID == id {
			return &d.Items[i], true
		}
	}
	return nil, false
}

func checkID(id string) error {
	if id == "" || len(id) > 32 {
		return fmt.Errorf("id %q is not 1 to 32 characters", id)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0) {
			return fmt.Errorf("id %q is not lower-case letters, digits and '-'", id)
		}
	}
	return nil
}

func checkColor(c string) error {
	if _, ok := parseColor(c); !ok {
		return fmt.Errorf("color %q is not #rrggbb", c)
	}
	return nil
}

// parseColor reads a #rrggbb colour as a 0xrrggbb number.
func parseColor(c string) (uint32, bool) {
	if len(c) != 7 || c[0] != '#' {
		return 0, false
	}
	var v uint32
	for i := 1; i < 7; i++ {
		ch := c[i]
		var n byte
		switch {
		case ch >= '0' && ch <= '9':
			n = ch - '0'
		case ch >= 'a' && ch <= 'f':
			n = ch - 'a' + 10
		default:
			return 0, false
		}
		v = v<<4 | uint32(n)
	}
	return v, true
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
