package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// WorldVersion is the version of data/world.yaml this game reads and writes.
const WorldVersion = 1

// World is data/world.yaml: the tiles the maps are drawn with, the foes the player can fight, where a new game starts, and every map with its exits, people, foes and items.
type World struct {
	// Version is the schema version; it must be WorldVersion.
	Version int `yaml:"version"`
	// TileSize is the width and height of a tile in game points.
	TileSize int `yaml:"tileSize"`
	// Start is where a new game puts the player.
	Start Place `yaml:"start"`
	// Tiles is the legend of the maps' rows: one character per kind of tile.
	Tiles []TileDef `yaml:"tiles"`
	// Foes are the kinds of enemy a map can place.
	Foes []FoeDef `yaml:"foes"`
	// Maps are the places the player walks, the first one the town.
	Maps []MapDef `yaml:"maps"`
}

// Place is a tile on a map.
type Place struct {
	// Map is the ID of the map.
	Map string `yaml:"map"`
	// At is the tile's column and row.
	At Cell `yaml:"at,flow"`
}

// Cell is a tile's column and row, written [column, row].
type Cell [2]int

// X returns the cell's column.
func (c Cell) X() int { return c[0] }

// Y returns the cell's row.
func (c Cell) Y() int { return c[1] }

// TileDef is one character of the maps' rows.
type TileDef struct {
	// Key is the character the rows use for the tile.
	Key string `yaml:"key"`
	// Art is the name of the tile's picture in the game's atlas, drawn in art.go.
	Art string `yaml:"art"`
	// Solid tiles block the player and foes.
	Solid bool `yaml:"solid,omitempty"`
}

// FoeDef is a kind of enemy and its battle stats.
type FoeDef struct {
	// ID names the kind; a quest's defeat objective targets it.
	ID string `yaml:"id"`
	// Name is what the battle screen calls it.
	Name string `yaml:"name"`
	// Art is the name of its picture in the game's atlas.
	Art string `yaml:"art"`
	// HP is how much damage it takes to defeat.
	HP int `yaml:"hp"`
	// Attack and Defense set the damage it deals and takes: an attack deals the attacker's attack less the target's defense, at least 1.
	Attack  int `yaml:"attack"`
	Defense int `yaml:"defense"`
	// Coins is what defeating it pays.
	Coins int `yaml:"coins,omitempty"`
	// Boss foes cannot be fled from.
	Boss bool `yaml:"boss,omitempty"`
}

// MapDef is one map: its tiles row by row and what stands on them.
type MapDef struct {
	// ID names the map in exits and saves.
	ID string `yaml:"id"`
	// Name is what the HUD shows.
	Name string `yaml:"name"`
	// Rows are the map's tiles, one string per row and one Tiles key per column; every row is as long as the first.
	Rows []string `yaml:"rows"`
	// Exits are the tiles that take the player to another map.
	Exits []Exit `yaml:"exits,omitempty"`
	// People are the characters the player can talk to.
	People []Person `yaml:"people,omitempty"`
	// Foes are the enemies that wander the map.
	Foes []Foe `yaml:"foes,omitempty"`
	// Items are the things lying on the map, picked up by walking onto them.
	Items []Item `yaml:"items,omitempty"`
}

// Exit is a tile that moves the player to another map.
type Exit struct {
	// At is the exit's tile.
	At Cell `yaml:"at,flow"`
	// To is where the player arrives.
	To Place `yaml:"to"`
}

// Person is a character the player talks to by facing them and pressing confirm.
type Person struct {
	// ID names the character in the cast (data/cast.yaml) and in quests' talk objectives.
	ID string `yaml:"id"`
	// Art is the name of the character's picture in the game's atlas.
	Art string `yaml:"art"`
	// At is where the character stands.
	At Cell `yaml:"at,flow"`
	// Node is the node of data/dialogue.yarn that talking to the character starts.
	Node string `yaml:"node"`
}

// Foe is an enemy placed on a map.
type Foe struct {
	// ID names this enemy in saves, unique in the world.
	ID string `yaml:"id"`
	// Kind is the FoeDef it is.
	Kind string `yaml:"kind"`
	// At is where it starts.
	At Cell `yaml:"at,flow"`
	// Patrol is the tiles it walks to in turn, one step at a time, after which it walks back to At; none keeps it still.
	Patrol []Cell `yaml:"patrol,omitempty,flow"`
}

// Item is an item lying on a map.
type Item struct {
	// ID names this pickup in saves, unique in the world.
	ID string `yaml:"id"`
	// Item is the item's ID in data/items.yaml.
	Item string `yaml:"item"`
	// Count is how many are picked up; 0 means 1.
	Count int `yaml:"count,omitempty"`
	// At is where it lies.
	At Cell `yaml:"at,flow"`
}

// Quantity returns Count, or 1 when Count is 0.
func (it Item) Quantity() int {
	if it.Count <= 0 {
		return 1
	}
	return it.Count
}

// ParseWorld decodes and validates a world file. Unknown fields and trailing documents are refused.
func ParseWorld(data []byte) (*World, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("world: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w World
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("world: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("world: unexpected second YAML document")
		}
		return nil, fmt.Errorf("world: %w", err)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return &w, nil
}

// EncodeWorld validates w and writes it as 2-space YAML with fields in schema order, which ParseWorld reads back to the same World.
func EncodeWorld(w *World) ([]byte, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(w); err != nil {
		return nil, fmt.Errorf("world: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("world: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks w: the version; a positive tile size; one-character tile keys used once; foe kinds with an ID, a name and positive HP; maps with unique IDs whose rows are equally long and use only tile keys; and exits, people, foes and items on walkable tiles inside their map, with exits leading to walkable tiles of maps that exist and IDs unique across the world.
func (w *World) Validate() error {
	if w == nil {
		return errors.New("world: nil world")
	}
	if w.Version != WorldVersion {
		return fmt.Errorf("world: version %d, want %d", w.Version, WorldVersion)
	}
	if w.TileSize <= 0 {
		return fmt.Errorf("world: tileSize %d must be positive", w.TileSize)
	}
	if len(w.Tiles) == 0 {
		return errors.New("world: no tiles")
	}
	keys := map[rune]bool{}
	for i, t := range w.Tiles {
		r, n := utf8.DecodeRuneInString(t.Key)
		if n == 0 || n != len(t.Key) || r == utf8.RuneError {
			return fmt.Errorf("world: tile %d: key %q must be one character", i, t.Key)
		}
		if keys[r] {
			return fmt.Errorf("world: tile key %q is used twice", t.Key)
		}
		keys[r] = true
		if t.Art == "" {
			return fmt.Errorf("world: tile %q has no art", t.Key)
		}
	}
	foes := map[string]bool{}
	for i, f := range w.Foes {
		if f.ID == "" || f.Name == "" || f.Art == "" {
			return fmt.Errorf("world: foe %d needs an id, a name and art", i)
		}
		if foes[f.ID] {
			return fmt.Errorf("world: foe kind %q is defined twice", f.ID)
		}
		foes[f.ID] = true
		if f.HP <= 0 || f.Attack < 0 || f.Defense < 0 || f.Coins < 0 {
			return fmt.Errorf("world: foe %q needs positive hp and no negative attack, defense or coins", f.ID)
		}
	}
	if len(w.Maps) == 0 {
		return errors.New("world: no maps")
	}
	maps := map[string]*MapDef{}
	for i := range w.Maps {
		m := &w.Maps[i]
		if m.ID == "" {
			return fmt.Errorf("world: map %d has no id", i)
		}
		if maps[m.ID] != nil {
			return fmt.Errorf("world: map %q is defined twice", m.ID)
		}
		maps[m.ID] = m
		if len(m.Rows) == 0 {
			return fmt.Errorf("world: map %q has no rows", m.ID)
		}
		width := utf8.RuneCountInString(m.Rows[0])
		for y, row := range m.Rows {
			if utf8.RuneCountInString(row) != width {
				return fmt.Errorf("world: map %q: row %d is %d tiles long, want %d", m.ID, y, utf8.RuneCountInString(row), width)
			}
			for x, r := range []rune(row) {
				if !keys[r] {
					return fmt.Errorf("world: map %q: tile %q at [%d, %d] is not in tiles", m.ID, r, x, y)
				}
			}
		}
	}
	ids := map[string]bool{}
	unique := func(kind, id string) error {
		if id == "" {
			return fmt.Errorf("world: a %s has no id", kind)
		}
		if ids[id] {
			return fmt.Errorf("world: id %q is used twice", id)
		}
		ids[id] = true
		return nil
	}
	walkable := func(m *MapDef, c Cell, what string) error {
		if !w.inside(m, c) {
			return fmt.Errorf("world: map %q: %s at %v is outside the map", m.ID, what, c)
		}
		if w.solidAt(m, c) {
			return fmt.Errorf("world: map %q: %s at %v is on a solid tile", m.ID, what, c)
		}
		return nil
	}
	start := maps[w.Start.Map]
	if start == nil {
		return fmt.Errorf("world: start map %q does not exist", w.Start.Map)
	}
	if err := walkable(start, w.Start.At, "the start"); err != nil {
		return err
	}
	for i := range w.Maps {
		m := &w.Maps[i]
		for _, e := range m.Exits {
			if err := walkable(m, e.At, "an exit"); err != nil {
				return err
			}
			to := maps[e.To.Map]
			if to == nil {
				return fmt.Errorf("world: map %q: exit at %v leads to map %q, which does not exist", m.ID, e.At, e.To.Map)
			}
			if err := walkable(to, e.To.At, "the arrival of an exit from "+m.ID); err != nil {
				return err
			}
		}
		for _, p := range m.People {
			if err := unique("person", p.ID); err != nil {
				return err
			}
			if p.Art == "" || p.Node == "" {
				return fmt.Errorf("world: person %q needs art and a dialogue node", p.ID)
			}
			if err := walkable(m, p.At, "person "+p.ID); err != nil {
				return err
			}
		}
		for _, f := range m.Foes {
			if err := unique("foe", f.ID); err != nil {
				return err
			}
			if !foes[f.Kind] {
				return fmt.Errorf("world: foe %q is of kind %q, which is not in foes", f.ID, f.Kind)
			}
			if err := walkable(m, f.At, "foe "+f.ID); err != nil {
				return err
			}
			for _, c := range f.Patrol {
				if err := walkable(m, c, "a patrol stop of foe "+f.ID); err != nil {
					return err
				}
			}
		}
		for _, it := range m.Items {
			if err := unique("item", it.ID); err != nil {
				return err
			}
			if it.Item == "" || it.Count < 0 {
				return fmt.Errorf("world: item %q needs an item and no negative count", it.ID)
			}
			if err := walkable(m, it.At, "item "+it.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Map returns the map called id, or nil.
func (w *World) Map(id string) *MapDef {
	for i := range w.Maps {
		if w.Maps[i].ID == id {
			return &w.Maps[i]
		}
	}
	return nil
}

// Foe returns the foe kind called id.
func (w *World) Foe(id string) (FoeDef, bool) {
	for _, f := range w.Foes {
		if f.ID == id {
			return f, true
		}
	}
	return FoeDef{}, false
}

// Size returns m's width and height in tiles.
func (m *MapDef) Size() (width, height int) {
	return utf8.RuneCountInString(m.Rows[0]), len(m.Rows)
}

// tile returns the legend entry of the tile at c, which must be inside m.
func (w *World) tile(m *MapDef, c Cell) TileDef {
	r := []rune(m.Rows[c.Y()])[c.X()]
	for _, t := range w.Tiles {
		if t.Key == string(r) {
			return t
		}
	}
	return TileDef{}
}

// inside reports whether c is a tile of m.
func (w *World) inside(m *MapDef, c Cell) bool {
	width, height := m.Size()
	return c.X() >= 0 && c.Y() >= 0 && c.X() < width && c.Y() < height
}

// solidAt reports whether the tile at c, inside m, blocks movement.
func (w *World) solidAt(m *MapDef, c Cell) bool {
	return w.tile(m, c).Solid
}
