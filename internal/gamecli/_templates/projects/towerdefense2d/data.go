package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/bullets"
	"github.com/nsgo-dev/game/pkg/stats"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// dataFS holds the game's data files, which game.yaml's autoload resources name: the level, the towers and the projectiles they fire.
//
//go:embed data
var dataFS embed.FS

// Autoload resource names in game.yaml.
const (
	resourceLevel       = "level"
	resourceTowers      = "towers"
	resourceProjectiles = "projectiles"
)

// LevelVersion is the version of the level file schema (data/level.yaml) this game reads and writes.
const LevelVersion = 1

// TowersVersion is the version of the tower file schema (data/towers.yaml) this game reads and writes.
const TowersVersion = 1

// MaxTileSize is the largest tileSize a level may have, in game points; the art is painted at the tile size when the game starts, so a typo such as 32000 would otherwise allocate gigabytes.
const MaxTileSize = 256

// Map legend: each character of a level's map row is one tile.
const (
	// TileGrass is open ground a tower can be built on.
	TileGrass = '.'
	// TileRoad is the road enemies walk along.
	TileRoad = '='
	// TileSpawn is the road tile enemies enter from; a map has exactly one.
	TileSpawn = 'S'
	// TileGate is the road tile enemies leave by, costing lives; a map has exactly one.
	TileGate = 'G'
	// TileRock is ground nothing can be built on or walk across.
	TileRock = '^'
	// TileWater is water nothing can be built on or walk across.
	TileWater = '~'
)

// Level is the root of data/level.yaml: the map, the player's starting money and lives, the enemies and the waves they come in.
type Level struct {
	// Version is the schema version; it must equal LevelVersion.
	Version int `yaml:"version"`
	// Name is the level's name, shown in the HUD.
	Name string `yaml:"name"`
	// TileSize is the edge of one map tile in game points, at most MaxTileSize. The map's size in game points, tileSize times its columns by tileSize times its rows, must equal the display width and height in game.yaml.
	TileSize float32 `yaml:"tileSize"`
	// Money is what the player starts with.
	Money int `yaml:"money"`
	// Lives is how many enemies (counting each one's Leak) may reach the gate before the game is lost.
	Lives int `yaml:"lives"`
	// Map is the level's tiles, one string per row from the top, one character per tile from the left, in the legend of the Tile constants. Every row has the same length.
	Map []MapRow `yaml:"map"`
	// Enemies are the kinds of enemy the waves send.
	Enemies []EnemyDef `yaml:"enemies"`
	// Waves are sent one at a time when the player calls them, in order; the game is won when the last one is cleared.
	Waves []Wave `yaml:"waves"`
}

// MapRow is one row of a level's map. It is written double-quoted, so the rows line up in the file whatever tiles they start with.
type MapRow string

// MarshalYAML writes the row as a double-quoted string.
func (r MapRow) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: string(r)}, nil
}

// EnemyDef is one kind of enemy.
type EnemyDef struct {
	// Name identifies the enemy kind in Group.Enemy.
	Name string `yaml:"name"`
	// Color is the CSS color the enemy is drawn in.
	Color string `yaml:"color"`
	// Radius is the enemy's hit radius in game points.
	Radius float32 `yaml:"radius"`
	// Reward is the money the player earns for destroying one.
	Reward int `yaml:"reward"`
	// Leak is the lives the player loses when one reaches the gate.
	Leak int `yaml:"leak"`
	// Stats are the enemy's stats: health (its hit points), speed (game points a second) and optionally armor (subtracted from each hit's damage). A frost tower slows the speed stat with a timed modifier.
	Stats []stats.Def `yaml:"stats"`
}

// Wave is one wave of enemies: groups that spawn side by side.
type Wave struct {
	// Bonus is the money the player earns when the wave has spawned and every enemy of it is gone.
	Bonus int `yaml:"bonus,omitempty"`
	// Groups spawn at the same time, each on its own clock.
	Groups []Group `yaml:"groups"`
}

// Group is a run of enemies of one kind within a wave.
type Group struct {
	// Enemy names the EnemyDef.
	Enemy string `yaml:"enemy"`
	// Count is how many spawn.
	Count int `yaml:"count"`
	// Delay is the time from the wave's call to the first spawn.
	Delay time.Duration `yaml:"delay,omitempty"`
	// Interval is the time between spawns.
	Interval time.Duration `yaml:"interval"`
}

// Towers is the root of data/towers.yaml: the towers the player can build.
type Towers struct {
	// Version is the schema version; it must equal TowersVersion.
	Version int `yaml:"version"`
	// Refund is the share of what a tower cost, with its upgrades, that selling it returns, in [0, 1].
	Refund float64 `yaml:"refund"`
	// Towers are the kinds of tower, in the order the HUD offers them.
	Towers []TowerDef `yaml:"towers"`
}

// TowerDef is one kind of tower.
type TowerDef struct {
	// Name identifies the tower kind.
	Name string `yaml:"name"`
	// Label is the tower's name in the HUD.
	Label string `yaml:"label"`
	// Color is the CSS color the tower and its projectiles are drawn in.
	Color string `yaml:"color"`
	// Cost is the money building one takes.
	Cost int `yaml:"cost"`
	// Pattern names the bullets pattern in data/projectiles.yaml the tower fires at its target; an aimed pattern aims at it.
	Pattern string `yaml:"pattern"`
	// Stats are the tower's stats: damage, range (game points) and cooldown (seconds between shots) are required; splash (radius of area damage), slow (the share of speed a hit takes away) and chill (seconds the slow lasts) are optional.
	Stats []stats.Def `yaml:"stats"`
	// Upgrades are bought one after another, each changing the tower's stats for good.
	Upgrades []Upgrade `yaml:"upgrades,omitempty"`
}

// Upgrade is one level of a tower's upgrades.
type Upgrade struct {
	// Cost is the money the upgrade takes.
	Cost int `yaml:"cost"`
	// Modifiers change the tower's stats from the time the upgrade is bought.
	Modifiers []stats.Modifier `yaml:"modifiers"`
}

// Effect returns the upgrade as a stats.Effect named name that lasts until it is removed.
func (u Upgrade) Effect(name string) stats.Effect {
	return stats.Effect{Name: name, Modifiers: u.Modifiers}
}

// Stat names the game reads from tower and enemy sheets.
const (
	statDamage   = "damage"
	statRange    = "range"
	statCooldown = "cooldown"
	statSplash   = "splash"
	statSlow     = "slow"
	statChill    = "chill"
	statHealth   = "health"
	statSpeed    = "speed"
	statArmor    = "armor"
)

// ParseLevel decodes and validates a level file. Unknown fields, trailing documents and invalid levels are refused.
func ParseLevel(data []byte) (*Level, error) {
	var l Level
	if err := decodeStrict(data, &l); err != nil {
		return nil, fmt.Errorf("level: %w", err)
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// EncodeLevel validates l and writes it as deterministic YAML that ParseLevel reads back to the same level.
func EncodeLevel(l *Level) ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return encode(l)
}

// ParseTowers decodes and validates a tower file. Unknown fields, trailing documents and invalid towers are refused.
func ParseTowers(data []byte) (*Towers, error) {
	var t Towers
	if err := decodeStrict(data, &t); err != nil {
		return nil, fmt.Errorf("towers: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// EncodeTowers validates t and writes it as deterministic YAML that ParseTowers reads back to the same towers.
func EncodeTowers(t *Towers) ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return encode(t)
}

// decodeStrict decodes the single YAML document data into v, refusing unknown fields and further documents.
func decodeStrict(data []byte, v any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// encode writes v as YAML with 2-space indentation, fields in declaration order and lists in their order.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Validate checks the level against the version 1 schema: the version, a positive tile size, money and lives, a rectangular map in the legend with one spawn and one gate joined by road, enemies with valid stats including a positive health and speed, and waves of known enemies with positive counts and intervals.
func (l *Level) Validate() error {
	if l == nil {
		return errors.New("level: nil level")
	}
	if l.Version != LevelVersion {
		return fmt.Errorf("level: version %d, want %d", l.Version, LevelVersion)
	}
	if !(l.TileSize > 0) || !finite32(l.TileSize) {
		return fmt.Errorf("level: tileSize %v must be positive", l.TileSize)
	}
	if l.TileSize > MaxTileSize {
		return fmt.Errorf("level: tileSize %v must be at most %d", l.TileSize, MaxTileSize)
	}
	if l.Money < 0 {
		return fmt.Errorf("level: money %d must not be negative", l.Money)
	}
	if l.Lives <= 0 {
		return fmt.Errorf("level: lives %d must be positive", l.Lives)
	}
	if _, err := l.route(); err != nil {
		return err
	}
	enemies := map[string]bool{}
	for i, e := range l.Enemies {
		if e.Name == "" {
			return fmt.Errorf("level: enemy %d has no name", i)
		}
		if enemies[e.Name] {
			return fmt.Errorf("level: enemy %q is defined twice", e.Name)
		}
		enemies[e.Name] = true
		if _, err := core.ParseColor(e.Color); err != nil {
			return fmt.Errorf("level: enemy %q color: %w", e.Name, err)
		}
		if !(e.Radius > 0) || !finite32(e.Radius) || e.Radius > l.TileSize {
			return fmt.Errorf("level: enemy %q radius %v must be positive and at most the tile size", e.Name, e.Radius)
		}
		if e.Reward < 0 || e.Leak < 0 {
			return fmt.Errorf("level: enemy %q reward and leak must not be negative", e.Name)
		}
		if err := validateSheet(e.Stats, nil, statHealth, statSpeed); err != nil {
			return fmt.Errorf("level: enemy %q: %w", e.Name, err)
		}
		sheet, _ := stats.NewSheet(e.Stats...)
		if !(sheet.Value(statHealth) > 0) || !(sheet.Value(statSpeed) > 0) {
			return fmt.Errorf("level: enemy %q health and speed must be positive", e.Name)
		}
	}
	if len(l.Waves) == 0 {
		return errors.New("level: no waves")
	}
	for i, w := range l.Waves {
		if w.Bonus < 0 {
			return fmt.Errorf("level: wave %d bonus %d must not be negative", i+1, w.Bonus)
		}
		if len(w.Groups) == 0 {
			return fmt.Errorf("level: wave %d has no groups", i+1)
		}
		for j, g := range w.Groups {
			if !enemies[g.Enemy] {
				return fmt.Errorf("level: wave %d group %d sends unknown enemy %q", i+1, j+1, g.Enemy)
			}
			if g.Count <= 0 {
				return fmt.Errorf("level: wave %d group %d count %d must be positive", i+1, j+1, g.Count)
			}
			if g.Delay < 0 || g.Interval <= 0 {
				return fmt.Errorf("level: wave %d group %d needs a positive interval and a delay that is not negative", i+1, j+1)
			}
		}
	}
	return nil
}

// Size returns the map's width and height in tiles.
func (l *Level) Size() (cols, rows int) {
	if len(l.Map) == 0 {
		return 0, 0
	}
	return len(l.Map[0]), len(l.Map)
}

// Tile returns the legend character at (col, row), or TileRock outside the map.
func (l *Level) Tile(col, row int) byte {
	if row < 0 || row >= len(l.Map) || col < 0 || col >= len(l.Map[row]) {
		return TileRock
	}
	return l.Map[row][col]
}

// Walkable reports whether enemies can walk on (col, row): road, spawn and gate tiles.
func (l *Level) Walkable(col, row int) bool {
	switch l.Tile(col, row) {
	case TileRoad, TileSpawn, TileGate:
		return true
	}
	return false
}

// Enemy returns the enemy kind called name.
func (l *Level) Enemy(name string) (*EnemyDef, bool) {
	for i := range l.Enemies {
		if l.Enemies[i].Name == name {
			return &l.Enemies[i], true
		}
	}
	return nil, false
}

// route checks the map and returns the shortest road from the spawn to the gate, found with A* over the road tiles.
func (l *Level) route() ([]ai.Cell, error) {
	if len(l.Map) == 0 {
		return nil, errors.New("level: empty map")
	}
	cols := len(l.Map[0])
	if cols == 0 {
		return nil, errors.New("level: empty map row")
	}
	var spawn, gate []ai.Cell
	for row, line := range l.Map {
		if len(line) != cols {
			return nil, fmt.Errorf("level: map row %d has %d tiles, want %d", row+1, len(line), cols)
		}
		for col := range len(line) {
			switch line[col] {
			case TileSpawn:
				spawn = append(spawn, ai.Cell{Col: col, Row: row})
			case TileGate:
				gate = append(gate, ai.Cell{Col: col, Row: row})
			case TileGrass, TileRoad, TileRock, TileWater:
			default:
				return nil, fmt.Errorf("level: map row %d column %d: unknown tile %q", row+1, col+1, line[col])
			}
		}
	}
	if len(spawn) != 1 || len(gate) != 1 {
		return nil, fmt.Errorf("level: the map has %d spawn (S) and %d gate (G) tiles, want one of each", len(spawn), len(gate))
	}
	grid := ai.GridFunc{Cols: cols, Rows: len(l.Map), Walk: l.Walkable}
	path, _, ok := ai.NewAStar().Find(grid, spawn[0], gate[0])
	if !ok {
		return nil, errors.New("level: no road joins the spawn (S) to the gate (G)")
	}
	return path, nil
}

// Validate checks the towers against the version 1 schema: the version, a refund in [0, 1], and towers with unique names, colors, positive costs, a pattern, valid stats including damage, range and cooldown, and upgrades whose modifiers change those stats.
func (t *Towers) Validate() error {
	if t == nil {
		return errors.New("towers: nil towers")
	}
	if t.Version != TowersVersion {
		return fmt.Errorf("towers: version %d, want %d", t.Version, TowersVersion)
	}
	if !(t.Refund >= 0 && t.Refund <= 1) {
		return fmt.Errorf("towers: refund %v must be in [0, 1]", t.Refund)
	}
	if len(t.Towers) == 0 {
		return errors.New("towers: no towers")
	}
	names := map[string]bool{}
	for i, d := range t.Towers {
		if d.Name == "" || d.Label == "" {
			return fmt.Errorf("towers: tower %d needs a name and a label", i)
		}
		if names[d.Name] {
			return fmt.Errorf("towers: tower %q is defined twice", d.Name)
		}
		names[d.Name] = true
		if _, err := core.ParseColor(d.Color); err != nil {
			return fmt.Errorf("towers: tower %q color: %w", d.Name, err)
		}
		if d.Cost <= 0 {
			return fmt.Errorf("towers: tower %q cost %d must be positive", d.Name, d.Cost)
		}
		if d.Pattern == "" {
			return fmt.Errorf("towers: tower %q has no pattern", d.Name)
		}
		for j, u := range d.Upgrades {
			if u.Cost <= 0 {
				return fmt.Errorf("towers: tower %q upgrade %d cost %d must be positive", d.Name, j+1, u.Cost)
			}
			if len(u.Modifiers) == 0 {
				return fmt.Errorf("towers: tower %q upgrade %d changes nothing", d.Name, j+1)
			}
		}
		if err := validateSheet(d.Stats, d.Upgrades, statDamage, statRange, statCooldown); err != nil {
			return fmt.Errorf("towers: tower %q: %w", d.Name, err)
		}
		// Check the range and cooldown before any upgrade and after each one, as World.Upgrade applies them in turn: a cooldown of 0 would fire every step and a range of 0 never.
		sheet, _ := stats.NewSheet(d.Stats...)
		for j := 0; ; j++ {
			if !(sheet.Value(statRange) > 0) || !(sheet.Value(statCooldown) > 0) {
				if j == 0 {
					return fmt.Errorf("towers: tower %q range and cooldown must be positive", d.Name)
				}
				return fmt.Errorf("towers: tower %q range and cooldown must stay positive after upgrade %d", d.Name, j)
			}
			if j == len(d.Upgrades) {
				break
			}
			name := fmt.Sprintf("upgrade-%d", j+1)
			// validateSheet checked every upgrade's modifiers against the tower's stats.
			_, _ = sheet.Apply(name, d.Upgrades[j].Effect(name))
		}
	}
	return nil
}

// Tower returns the tower kind called name.
func (t *Towers) Tower(name string) (*TowerDef, bool) {
	for i := range t.Towers {
		if t.Towers[i].Name == name {
			return &t.Towers[i], true
		}
	}
	return nil, false
}

// validateSheet checks that every stat of required is defined, then defs and upgrades' modifiers on them with stats.Validate.
func validateSheet(defs []stats.Def, upgrades []Upgrade, required ...string) error {
	for _, name := range required {
		if !slices.ContainsFunc(defs, func(d stats.Def) bool { return d.Name == name }) {
			return fmt.Errorf("no %s stat", name)
		}
	}
	f := stats.File{Version: stats.CurrentVersion, Stats: defs}
	for i, u := range upgrades {
		f.Effects = append(f.Effects, u.Effect(fmt.Sprintf("upgrade-%d", i+1)))
	}
	return stats.Validate(&f)
}

func finite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// Data is everything the game reads from its data files, checked against each other.
type Data struct {
	// Level is data/level.yaml.
	Level *Level
	// Towers is data/towers.yaml.
	Towers *Towers
	// Projectiles is data/projectiles.yaml, a bullets pattern file.
	Projectiles *bullets.File
}

// LoadData reads the level, towers and projectiles that game.yaml autoloads from fsys, the project root, and checks that every tower's pattern is in the projectile file and draws a frame.
func LoadData(fsys fs.FS) (*Data, error) {
	loaded, err := Project.LoadAutoloads(fsys)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{resourceLevel, resourceTowers, resourceProjectiles} {
		if _, ok := loaded.Resources[name]; !ok {
			return nil, fmt.Errorf("game.yaml autoloads no resource %q", name)
		}
	}
	d := &Data{}
	if d.Level, err = ParseLevel(loaded.Resources[resourceLevel]); err != nil {
		return nil, err
	}
	// The viewport clips drawing and taps to the display's size, so a map of another size would be cut off or draw into the bars.
	if vp, ok := Project.Viewport(); ok {
		cols, rows := d.Level.Size()
		if w, h := d.Level.TileSize*float32(cols), d.Level.TileSize*float32(rows); w != vp.Width || h != vp.Height {
			return nil, fmt.Errorf("level: the map is %v by %v game points (tileSize %v times %d by %d tiles), but game.yaml's display is %v by %v; make them match", w, h, d.Level.TileSize, cols, rows, vp.Width, vp.Height)
		}
	}
	if d.Towers, err = ParseTowers(loaded.Resources[resourceTowers]); err != nil {
		return nil, err
	}
	if d.Projectiles, err = bullets.Parse(loaded.Resources[resourceProjectiles]); err != nil {
		return nil, err
	}
	for _, t := range d.Towers.Towers {
		p, ok := d.Projectiles.Pattern(t.Pattern)
		if !ok {
			return nil, fmt.Errorf("towers: tower %q fires pattern %q, which data/projectiles.yaml does not have", t.Name, t.Pattern)
		}
		if p.Frame == "" {
			return nil, fmt.Errorf("projectiles: pattern %q needs a frame to draw", p.Name)
		}
		// A tower fires its pattern's one shot each time its cooldown runs out, so the tower's cooldown stat, not the pattern, times its shots.
		if p.Shots != 1 || p.Delay != 0 || !(p.Speed > 0) {
			return nil, fmt.Errorf("projectiles: pattern %q fires from a tower, so it needs shots: 1, no delay and a positive speed", p.Name)
		}
	}
	return d, nil
}

// MustLoadData is LoadData from the data files built into the game, which the tests check load.
func MustLoadData() *Data {
	d, err := LoadData(dataFS)
	if err != nil {
		panic(err)
	}
	return d
}
