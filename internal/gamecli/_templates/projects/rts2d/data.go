package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/rts"
	"go.yaml.in/yaml/v3"
)

// dataFS holds the game's design data, every file of it YAML a designer edits and reviews as text: rules.yaml (units, buildings and the economy), map.yaml (the battlefield), formations.yaml (how ordered groups line up, in rts's format) and opponent.yaml (the computer player's choices, in ai's utility format).
//
//go:embed data/*.yaml
var dataFS embed.FS

const (
	// rulesVersion is the schema version of data/rules.yaml that parseRules reads and encodeRules writes.
	rulesVersion = 1
	// mapVersion is the schema version of data/map.yaml that parseMap reads and encodeMap writes.
	mapVersion = 1
	// stepRate is the number of fixed simulation steps a second; the rules give times in seconds and the world counts steps.
	stepRate = 60
)

// Map legend: one character per cell of map.yaml's tiles.
const (
	tileGround     = '.'
	tileRock       = '#'
	tileCrystal    = '*'
	tilePlayerHQ   = 'A'
	tileEnemyHQ    = 'B'
	tilePlayerPad  = 'a'
	tileEnemyPad   = 'b'
	hqBuildingName = "hq"
	barracksName   = "barracks"
)

// The unit types the controls, the command bar, the drawing and the computer player name; rules.yaml must define them, and may add others.
const (
	workerName  = "worker"
	soldierName = "soldier"
	rangerName  = "ranger"
)

// maxSeconds is the longest duration rules.yaml may give, a day, so a duration always converts to a number of steps an int holds.
const maxSeconds = 24 * 60 * 60

// rules is data/rules.yaml: the stats of every unit and building and the economy both sides play by.
type rules struct {
	// Version is the schema version; it must be rulesVersion.
	Version int `yaml:"version"`
	// StartCrystals is how many crystals each side starts with.
	StartCrystals int `yaml:"startCrystals"`
	// MaxUnits is the most units one side may have at once, counting those in production.
	MaxUnits int `yaml:"maxUnits"`
	// CrystalsPerNode is how many crystals one crystal cell of the map holds before it is mined out.
	CrystalsPerNode int `yaml:"crystalsPerNode"`
	// CarryAmount is how many crystals a worker carries back per trip.
	CarryAmount int `yaml:"carryAmount"`
	// GatherSeconds is how long a worker mines before it carries a load home.
	GatherSeconds float64 `yaml:"gatherSeconds"`
	// QueueLimit is how many units one building may have queued, the one in production included.
	QueueLimit int `yaml:"queueLimit"`
	// OpponentThinkSeconds is how often the computer player picks its next action.
	OpponentThinkSeconds float64 `yaml:"opponentThinkSeconds"`
	// Start lists the units each side starts with beside its headquarters.
	Start []startUnits `yaml:"start"`
	// Units are the unit types, in the order the HUD lists them.
	Units []unitRules `yaml:"units"`
	// Buildings are the building types; the map places each side's hq and the barracks are built on build pads.
	Buildings []buildingRules `yaml:"buildings"`
}

// startUnits is a number of units of one type a side starts with.
type startUnits struct {
	// Unit names a unit type of rules.Units.
	Unit string `yaml:"unit"`
	// Count is how many of them, at least 1.
	Count int `yaml:"count"`
}

// unitRules is one unit type.
type unitRules struct {
	// Name identifies the type. The rules must define worker (the one type that gathers), soldier and ranger, which the controls, the command bar and the computer player train, and may add others.
	Name string `yaml:"name"`
	// HP is the unit's health.
	HP int `yaml:"hp"`
	// Speed is the unit's top speed in world points a second.
	Speed float32 `yaml:"speed"`
	// Radius is the unit's body radius in world points.
	Radius float32 `yaml:"radius"`
	// Damage is the health one attack takes; 0 for a unit that does not fight.
	Damage int `yaml:"damage,omitempty"`
	// Range is how far beyond the two bodies' edges the unit reaches, in world points.
	Range float32 `yaml:"range,omitempty"`
	// Cooldown is the time between two attacks, in seconds.
	Cooldown float64 `yaml:"cooldown,omitempty"`
	// Sight is how far the unit sees through the fog of war, in cells; idle units attack enemies they see.
	Sight int `yaml:"sight"`
	// Cost is the unit's price in crystals.
	Cost int `yaml:"cost"`
	// TrainSeconds is how long the building takes to produce it.
	TrainSeconds float64 `yaml:"trainSeconds"`
	// TrainedAt names the building type that produces it.
	TrainedAt string `yaml:"trainedAt"`
	// Gathers marks the type that mines crystals.
	Gathers bool `yaml:"gathers,omitempty"`
}

// buildingRules is one building type.
type buildingRules struct {
	// Name identifies the type: hq and barracks.
	Name string `yaml:"name"`
	// HP is the building's health when complete.
	HP int `yaml:"hp"`
	// Size is the building's width and height in cells.
	Size int `yaml:"size"`
	// Sight is how far the building sees, in cells.
	Sight int `yaml:"sight"`
	// Cost is the building's price in crystals; 0 for one that cannot be built.
	Cost int `yaml:"cost,omitempty"`
	// BuildSeconds is how long its construction takes.
	BuildSeconds float64 `yaml:"buildSeconds,omitempty"`
}

// battleMap is data/map.yaml: the battlefield as rows of characters, one per cell.
type battleMap struct {
	// Version is the schema version; it must be mapVersion.
	Version int `yaml:"version"`
	// CellSize is the width and height of a cell in world points.
	CellSize float32 `yaml:"cellSize"`
	// Tiles are the map's rows, one line each and all as wide: . ground, # rock that blocks movement and sight, * a crystal cell, A and B the top-left cell of the player's and the opponent's headquarters, a and b the top-left cell of a build pad of each side. The cells a headquarters or pad covers besides its marker are ground.
	Tiles string `yaml:"tiles"`

	cols, rows int
	cells      []byte
}

// gameData is every data file, parsed and checked against each other.
type gameData struct {
	rules      *rules
	battleMap  *battleMap
	formations *rts.Formations
	opponent   *ai.UtilityConfig
}

// loadGameData reads and checks the embedded data files.
func loadGameData() (*gameData, error) {
	read := func(name string) ([]byte, error) { return dataFS.ReadFile("data/" + name) }
	src, err := read("rules.yaml")
	if err != nil {
		return nil, err
	}
	r, err := parseRules(src)
	if err != nil {
		return nil, fmt.Errorf("data/rules.yaml: %w", err)
	}
	if src, err = read("map.yaml"); err != nil {
		return nil, err
	}
	m, err := parseMap(src)
	if err != nil {
		return nil, fmt.Errorf("data/map.yaml: %w", err)
	}
	if src, err = read("formations.yaml"); err != nil {
		return nil, err
	}
	f, err := rts.ParseFormations(src)
	if err != nil {
		return nil, fmt.Errorf("data/formations.yaml: %w", err)
	}
	if _, ok := f.Get(moveFormation); !ok {
		return nil, fmt.Errorf("data/formations.yaml: no formation named %q", moveFormation)
	}
	if src, err = read("opponent.yaml"); err != nil {
		return nil, err
	}
	o, err := ai.ParseUtility(src)
	if err != nil {
		return nil, fmt.Errorf("data/opponent.yaml: %w", err)
	}
	if err := m.fits(r); err != nil {
		return nil, fmt.Errorf("data/map.yaml: %w", err)
	}
	return &gameData{rules: r, battleMap: m, formations: f, opponent: o}, nil
}

// mustLoadGameData returns the embedded data files, which the tests check, so a problem is a programming error.
func mustLoadGameData() *gameData {
	d, err := loadGameData()
	if err != nil {
		panic(err)
	}
	return d
}

// decodeStrict decodes one YAML document from data into v, refusing unknown fields and a second document.
func decodeStrict(data []byte, v any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("a second YAML document")
		}
		return err
	}
	return nil
}

// encodeYAML writes v as YAML with two-space indentation, its fields in declaration order.
func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseRules decodes and validates data/rules.yaml.
func parseRules(data []byte) (*rules, error) {
	var r rules
	if err := decodeStrict(data, &r); err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// encodeRules validates r and writes it in the form parseRules reads, so a parsed file encodes back to the same bytes.
func encodeRules(r *rules) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return encodeYAML(r)
}

// validate reports the first problem with r.
func (r *rules) validate() error {
	if r.Version != rulesVersion {
		return fmt.Errorf("version %d, want %d", r.Version, rulesVersion)
	}
	switch {
	case r.StartCrystals < 0:
		return errors.New("startCrystals is negative")
	case r.MaxUnits < 1:
		return errors.New("maxUnits must be at least 1")
	case r.CrystalsPerNode < 1 || r.CarryAmount < 1:
		return errors.New("crystalsPerNode and carryAmount must be at least 1")
	case r.QueueLimit < 1:
		return errors.New("queueLimit must be at least 1")
	}
	if !positiveSeconds(r.GatherSeconds) || !positiveSeconds(r.OpponentThinkSeconds) {
		return errors.New("gatherSeconds and opponentThinkSeconds must be positive numbers of seconds")
	}
	buildings := map[string]bool{}
	for i, b := range r.Buildings {
		switch {
		case b.Name == "" || buildings[b.Name]:
			return fmt.Errorf("building %d: name %q is empty or repeated", i, b.Name)
		case b.HP < 1 || b.Size < 1 || b.Sight < 0 || b.Cost < 0:
			return fmt.Errorf("building %s: hp and size must be at least 1, sight and cost not negative", b.Name)
		case b.Cost > 0 && !positiveSeconds(b.BuildSeconds):
			return fmt.Errorf("building %s: a building with a cost needs a positive buildSeconds", b.Name)
		}
		buildings[b.Name] = true
	}
	for _, need := range []string{hqBuildingName, barracksName} {
		if !buildings[need] {
			return fmt.Errorf("no building named %s", need)
		}
	}
	if b := r.building(barracksName); b.Cost == 0 {
		return errors.New("barracks needs a cost to be built")
	}
	units := map[string]bool{}
	gatherers := 0
	for i, u := range r.Units {
		switch {
		case u.Name == "" || units[u.Name] || buildings[u.Name]:
			return fmt.Errorf("unit %d: name %q is empty or repeated", i, u.Name)
		case u.HP < 1 || u.Sight < 0 || u.Cost < 0 || u.Damage < 0:
			return fmt.Errorf("unit %s: hp must be at least 1, sight, cost and damage not negative", u.Name)
		case !positiveFinite32(u.Speed) || !positiveFinite32(u.Radius) || !finite32(u.Range) || u.Range < 0:
			return fmt.Errorf("unit %s: speed and radius must be positive and range not negative", u.Name)
		case !positiveSeconds(u.TrainSeconds):
			return fmt.Errorf("unit %s: trainSeconds %v must be a positive number of seconds", u.Name, u.TrainSeconds)
		case u.Damage > 0 && !positiveSeconds(u.Cooldown):
			return fmt.Errorf("unit %s: a unit that deals damage needs a positive cooldown", u.Name)
		case u.Gathers && u.Damage > 0:
			// A worker only mines and walks, so it could never carry out the attack orders a unit with damage gets.
			return fmt.Errorf("unit %s: a unit that gathers cannot deal damage", u.Name)
		case !buildings[u.TrainedAt]:
			return fmt.Errorf("unit %s: trainedAt %q is not a building", u.Name, u.TrainedAt)
		}
		units[u.Name] = true
		if u.Gathers {
			gatherers++
		}
	}
	if gatherers != 1 {
		return fmt.Errorf("%d unit types gather crystals, want exactly 1", gatherers)
	}
	if u := r.unit(workerName); u == nil || !u.Gathers {
		return fmt.Errorf("no unit named %s that gathers crystals; the controls, the command bar and the computer player train it", workerName)
	}
	for _, name := range []string{soldierName, rangerName} {
		if u := r.unit(name); u == nil || u.Damage == 0 {
			return fmt.Errorf("no unit named %s that deals damage; the controls, the command bar and the computer player train it", name)
		}
	}
	total := 0
	for _, s := range r.Start {
		if !units[s.Unit] || s.Count < 1 {
			return fmt.Errorf("start: unit %q is not a unit type or count %d is less than 1", s.Unit, s.Count)
		}
		total += s.Count
	}
	if total > r.MaxUnits {
		return fmt.Errorf("start: %d units exceed maxUnits %d", total, r.MaxUnits)
	}
	return nil
}

// unit returns the unit type named name, or nil.
func (r *rules) unit(name string) *unitRules {
	for i := range r.Units {
		if r.Units[i].Name == name {
			return &r.Units[i]
		}
	}
	return nil
}

// building returns the building type named name, or nil.
func (r *rules) building(name string) *buildingRules {
	for i := range r.Buildings {
		if r.Buildings[i].Name == name {
			return &r.Buildings[i]
		}
	}
	return nil
}

// worker returns the unit type that gathers crystals, which validate guarantees.
func (r *rules) worker() *unitRules {
	for i := range r.Units {
		if r.Units[i].Gathers {
			return &r.Units[i]
		}
	}
	return nil
}

// parseMap decodes and validates data/map.yaml.
func parseMap(data []byte) (*battleMap, error) {
	var m battleMap
	if err := decodeStrict(data, &m); err != nil {
		return nil, err
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// encodeMap validates m and writes it in the form parseMap reads.
func encodeMap(m *battleMap) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	return encodeYAML(m)
}

// validate checks m's version, cell size and tiles, and indexes the tiles.
func (m *battleMap) validate() error {
	if m.Version != mapVersion {
		return fmt.Errorf("version %d, want %d", m.Version, mapVersion)
	}
	if !positiveFinite32(m.CellSize) {
		return fmt.Errorf("cellSize %v must be positive", m.CellSize)
	}
	lines := strings.Split(strings.TrimSuffix(m.Tiles, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return errors.New("tiles: no rows")
	}
	cols := len(lines[0])
	cells := make([]byte, 0, cols*len(lines))
	for i, line := range lines {
		if len(line) != cols {
			return fmt.Errorf("tiles: row %d is %d cells wide, want %d", i, len(line), cols)
		}
		for j := range len(line) {
			if !strings.ContainsRune(".#*ABab", rune(line[j])) {
				return fmt.Errorf("tiles: row %d column %d: unknown tile %q", i, j, line[j])
			}
		}
		cells = append(cells, line...)
	}
	m.cols, m.rows, m.cells = cols, len(lines), cells
	for _, marker := range []byte{tilePlayerHQ, tileEnemyHQ} {
		if n := len(m.find(marker)); n != 1 {
			return fmt.Errorf("tiles: %d %c markers, want 1", n, marker)
		}
	}
	return nil
}

// fits checks that every headquarters and pad of m has room for its building under r, and that no two footprints share a cell.
func (m *battleMap) fits(r *rules) error {
	claimed := map[ai.Cell]bool{}
	for _, site := range []struct {
		marker byte
		name   string
	}{{tilePlayerHQ, hqBuildingName}, {tileEnemyHQ, hqBuildingName}, {tilePlayerPad, barracksName}, {tileEnemyPad, barracksName}} {
		name := site.name
		size := r.building(name).Size
		for _, c := range m.find(site.marker) {
			for row := c.Row; row < c.Row+size; row++ {
				for col := c.Col; col < c.Col+size; col++ {
					cell := ai.Cell{Col: col, Row: row}
					if claimed[cell] {
						return fmt.Errorf("tiles: the %s at column %d row %d overlaps another headquarters or pad at column %d row %d", name, c.Col, c.Row, col, row)
					}
					claimed[cell] = true
					if col == c.Col && row == c.Row {
						continue
					}
					if t, ok := m.tile(col, row); !ok || t != tileGround {
						return fmt.Errorf("tiles: the %s at column %d row %d needs ground at column %d row %d", name, c.Col, c.Row, col, row)
					}
				}
			}
		}
	}
	return nil
}

// tile returns the tile at (col, row) and whether it is on the map.
func (m *battleMap) tile(col, row int) (byte, bool) {
	if col < 0 || row < 0 || col >= m.cols || row >= m.rows {
		return 0, false
	}
	return m.cells[row*m.cols+col], true
}

// find returns every cell holding tile t, row by row.
func (m *battleMap) find(t byte) []ai.Cell {
	var out []ai.Cell
	for i, c := range m.cells {
		if c == t {
			out = append(out, ai.Cell{Col: i % m.cols, Row: i / m.cols})
		}
	}
	return out
}

// secondsToSteps converts a duration in seconds to whole fixed steps, at least 1.
func secondsToSteps(s float64) int {
	return max(1, int(math.Round(s*stepRate)))
}

// positiveSeconds reports whether s is a duration rules.yaml may give: more than 0 and at most maxSeconds.
func positiveSeconds(s float64) bool { return s > 0 && s <= maxSeconds }

func finite32(v float32) bool { return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v)) }

func positiveFinite32(v float32) bool { return v > 0 && finite32(v) }
