package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"go.yaml.in/yaml/v3"
)

// ArenaVersion is the arena file schema version that ParseArena accepts and EncodeArena writes.
const ArenaVersion = 1

// Layout cells. Every other character in a layout row is a pickup's symbol or, for 1 to 9, a patrol waypoint on an open floor cell.
const (
	cellWall   = '#'
	cellCrate  = 'c'
	cellFloor  = '.'
	cellPlayer = 'P'
	cellSpawn  = 'S'
)

// Weapon kinds.
const (
	// WeaponHitscan hits the first thing on the line of sight the moment it fires.
	WeaponHitscan = "hitscan"
	// WeaponProjectile fires a round that flies at Speed and bursts on impact, hurting every enemy within SplashRadius.
	WeaponProjectile = "projectile"
)

// Pickup effects.
const (
	// EffectHealth restores Amount health, up to the player's maximum.
	EffectHealth = "health"
	// EffectAmmo adds Amount magazines to every weapon's reserve, up to its MaxReserve.
	EffectAmmo = "ammo"
)

// Arena is arena.yaml: the level's layout and every gameplay number the match reads, so tuning the game is editing this file. Lengths are meters, times seconds and angles radians.
type Arena struct {
	// Version is the schema version; it must equal ArenaVersion.
	Version int `yaml:"version"`
	// Name is shown on the HUD.
	Name string `yaml:"name"`
	// CellSize is the width and depth of one layout cell.
	CellSize float32 `yaml:"cellSize"`
	// WallHeight is how tall wall cells stand.
	WallHeight float32 `yaml:"wallHeight"`
	// CrateHeight is how tall crate cells stand: low enough to shoot over, high enough to block walking.
	CrateHeight float32 `yaml:"crateHeight"`
	// Layout is the level seen from above, one string per row from north (-Z) to south (+Z) and one character per cell from west (-X) to east (+X): # wall, c crate, . floor, P the player's start, S an enemy spawn, 1 to 9 the patrol route in order, and a pickup's symbol. Every row has the same length and the border is wall.
	Layout []string `yaml:"layout"`
	// Player tunes the player.
	Player PlayerConfig `yaml:"player"`
	// Weapons are the player's weapons in slot order; the first is drawn at the start.
	Weapons []Weapon `yaml:"weapons"`
	// Enemies are the enemy kinds waves send.
	Enemies []EnemyKind `yaml:"enemies"`
	// Pickups are the pickup kinds the layout places by symbol.
	Pickups []PickupKind `yaml:"pickups"`
	// Waves are fought in order; clearing the last one wins the match.
	Waves []Wave `yaml:"waves"`
	// Countdown is the pause before the first wave.
	Countdown float32 `yaml:"countdown"`
	// Intermission is the pause between waves.
	Intermission float32 `yaml:"intermission"`
}

// PlayerConfig tunes the player's body, movement and look speed.
type PlayerConfig struct {
	// Health is the player's starting and maximum health.
	Health int `yaml:"health"`
	// Radius and Height size the player's capsule.
	Radius float32 `yaml:"radius"`
	Height float32 `yaml:"height"`
	// EyeHeight is the camera's height above the feet.
	EyeHeight float32 `yaml:"eyeHeight"`
	// Speed is the running speed.
	Speed float32 `yaml:"speed"`
	// JumpSpeed is the upward speed a jump starts with.
	JumpSpeed float32 `yaml:"jumpSpeed"`
	// TurnSpeed is how fast a fully pushed stick or a held look key turns the view, in radians a second.
	TurnSpeed float32 `yaml:"turnSpeed"`
	// MouseSensitivity is how far one point of locked mouse motion turns the view, in radians.
	MouseSensitivity float32 `yaml:"mouseSensitivity"`
}

// Weapon is one of the player's weapons.
type Weapon struct {
	// Name is shown on the HUD and unique among the weapons.
	Name string `yaml:"name"`
	// Kind is WeaponHitscan or WeaponProjectile.
	Kind string `yaml:"kind"`
	// Damage is the health a hit removes; a projectile's burst removes less the further an enemy is from its center.
	Damage int `yaml:"damage"`
	// FireInterval is the time between shots while the trigger is held.
	FireInterval float32 `yaml:"fireInterval"`
	// Magazine is how many rounds one load holds.
	Magazine int `yaml:"magazine"`
	// Reserve is how many rounds the player starts with outside the magazine.
	Reserve int `yaml:"reserve"`
	// MaxReserve caps the reserve that ammo pickups refill.
	MaxReserve int `yaml:"maxReserve"`
	// ReloadTime is how long a reload takes.
	ReloadTime float32 `yaml:"reloadTime"`
	// Range is how far a hitscan shot reaches, or a projectile flies before it bursts.
	Range float32 `yaml:"range"`
	// Speed is a projectile's flight speed; hitscan weapons leave it 0.
	Speed float32 `yaml:"speed,omitempty"`
	// SplashRadius is a projectile's burst radius; hitscan weapons leave it 0.
	SplashRadius float32 `yaml:"splashRadius,omitempty"`
}

// EnemyKind is one kind of enemy: its body, senses and attack.
type EnemyKind struct {
	// Name is what waves call the kind, unique among the kinds.
	Name string `yaml:"name"`
	// Health is the kind's starting health.
	Health int `yaml:"health"`
	// Radius and Height size the kind's capsule.
	Radius float32 `yaml:"radius"`
	Height float32 `yaml:"height"`
	// Speed is the walking speed.
	Speed float32 `yaml:"speed"`
	// SightRange is how far the kind sees the player, along a clear line of sight inside its field of view.
	SightRange float32 `yaml:"sightRange"`
	// FieldOfView is the full angle of the kind's view cone; the player is noticed inside it, or anywhere within 4 m.
	FieldOfView float32 `yaml:"fieldOfView"`
	// AttackRange is the distance within which the kind stops and shoots.
	AttackRange float32 `yaml:"attackRange"`
	// FireInterval is the time between the kind's shots.
	FireInterval float32 `yaml:"fireInterval"`
	// BoltSpeed is how fast the kind's bolts fly.
	BoltSpeed float32 `yaml:"boltSpeed"`
	// BoltDamage is the health one bolt removes from the player.
	BoltDamage int `yaml:"boltDamage"`
	// Color is the body's linear RGB color, each channel in [0, 1].
	Color [3]float32 `yaml:"color,flow"`
}

// PickupKind is a pickup the layout places wherever its symbol appears.
type PickupKind struct {
	// Symbol is the one-character layout cell that places this pickup.
	Symbol string `yaml:"symbol"`
	// Effect is EffectHealth or EffectAmmo.
	Effect string `yaml:"effect"`
	// Amount is the health restored or the magazines added.
	Amount int `yaml:"amount"`
	// Respawn is how long a taken pickup stays away.
	Respawn float32 `yaml:"respawn"`
}

// Wave is one wave of enemies.
type Wave struct {
	// Groups are the enemies the wave sends, spread over the spawns in order.
	Groups []WaveGroup `yaml:"groups"`
}

// WaveGroup is Count enemies of one kind.
type WaveGroup struct {
	// Enemy names an EnemyKind.
	Enemy string `yaml:"enemy"`
	// Count is how many of it the wave sends.
	Count int `yaml:"count"`
}

//go:embed arena.yaml
var arenaYAML []byte

// ParseArena decodes and validates an arena file. Unknown fields and trailing YAML documents are refused.
func ParseArena(data []byte) (*Arena, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var a Arena
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("arena: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("arena: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("arena: decode: %w", err)
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

// EncodeArena validates a and writes it as the 2-space YAML ParseArena reads back unchanged.
func EncodeArena(a *Arena) ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(a); err != nil {
		return nil, fmt.Errorf("arena: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("arena: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// mustParseArena is ParseArena for the embedded arena.yaml, which is part of the program.
func mustParseArena(data []byte) *Arena {
	a, err := ParseArena(data)
	if err != nil {
		panic(err)
	}
	return a
}

// Validate reports the first problem that would stop a match from running on a.
func (a *Arena) Validate() error {
	if a.Version != ArenaVersion {
		return fmt.Errorf("arena: version %d, want %d", a.Version, ArenaVersion)
	}
	if a.Name == "" {
		return errors.New("arena: name is empty")
	}
	if !positive(a.CellSize) || !positive(a.WallHeight) || !positive(a.CrateHeight) {
		return errors.New("arena: cellSize, wallHeight and crateHeight must be positive")
	}
	if !nonNegative(a.Countdown) || !nonNegative(a.Intermission) {
		return errors.New("arena: countdown and intermission must not be negative")
	}
	if err := a.Player.validate(); err != nil {
		return err
	}
	if len(a.Weapons) == 0 || len(a.Weapons) > 9 {
		return errors.New("arena: between 1 and 9 weapons are required")
	}
	for i, w := range a.Weapons {
		if err := w.validate(); err != nil {
			return fmt.Errorf("arena: weapon %d: %w", i, err)
		}
		if slices.ContainsFunc(a.Weapons[:i], func(o Weapon) bool { return o.Name == w.Name }) {
			return fmt.Errorf("arena: weapon %q is declared twice", w.Name)
		}
	}
	if len(a.Enemies) == 0 {
		return errors.New("arena: at least one enemy kind is required")
	}
	for i, e := range a.Enemies {
		if err := e.validate(); err != nil {
			return fmt.Errorf("arena: enemy %d: %w", i, err)
		}
		if slices.ContainsFunc(a.Enemies[:i], func(o EnemyKind) bool { return o.Name == e.Name }) {
			return fmt.Errorf("arena: enemy %q is declared twice", e.Name)
		}
	}
	for i, p := range a.Pickups {
		if err := p.validate(); err != nil {
			return fmt.Errorf("arena: pickup %d: %w", i, err)
		}
		if slices.ContainsFunc(a.Pickups[:i], func(o PickupKind) bool { return o.Symbol == p.Symbol }) {
			return fmt.Errorf("arena: pickup symbol %q is declared twice", p.Symbol)
		}
	}
	if len(a.Waves) == 0 {
		return errors.New("arena: at least one wave is required")
	}
	for i, w := range a.Waves {
		if len(w.Groups) == 0 {
			return fmt.Errorf("arena: wave %d has no groups", i+1)
		}
		total := 0
		for _, g := range w.Groups {
			if a.enemy(g.Enemy) == nil {
				return fmt.Errorf("arena: wave %d sends unknown enemy %q", i+1, g.Enemy)
			}
			if g.Count < 1 || g.Count > maxWaveEnemies {
				return fmt.Errorf("arena: wave %d sends %d of %q, want 1 to %d", i+1, g.Count, g.Enemy, maxWaveEnemies)
			}
			if total += g.Count; total > maxWaveEnemies {
				return fmt.Errorf("arena: wave %d sends more than %d enemies", i+1, maxWaveEnemies)
			}
		}
	}
	return a.validateLayout()
}

// maxWaveEnemies bounds the enemies in one wave, across all its groups, so a malformed file cannot ask the renderer for unbounded draws.
const maxWaveEnemies = 16

func (p PlayerConfig) validate() error {
	if !count(p.Health, 1) {
		return fmt.Errorf("arena: player health must be 1 to %d", maxQuantity)
	}
	for _, v := range []float32{p.Radius, p.Height, p.EyeHeight, p.Speed, p.JumpSpeed, p.TurnSpeed, p.MouseSensitivity} {
		if !positive(v) {
			return errors.New("arena: player radius, height, eyeHeight, speed, jumpSpeed, turnSpeed and mouseSensitivity must be positive")
		}
	}
	if p.Height < 2*p.Radius || p.EyeHeight > p.Height {
		return errors.New("arena: player height must be at least twice its radius and at least its eyeHeight")
	}
	return nil
}

func (w Weapon) validate() error {
	if w.Name == "" {
		return errors.New("name is empty")
	}
	switch w.Kind {
	case WeaponHitscan:
		if w.Speed != 0 || w.SplashRadius != 0 {
			return errors.New("a hitscan weapon has no speed or splashRadius")
		}
	case WeaponProjectile:
		if !positive(w.Speed) || !positive(w.SplashRadius) {
			return errors.New("a projectile weapon needs a positive speed and splashRadius")
		}
	default:
		return fmt.Errorf("kind %q, want %s or %s", w.Kind, WeaponHitscan, WeaponProjectile)
	}
	if !count(w.Damage, 1) || !count(w.Magazine, 1) || !count(w.MaxReserve, 0) || w.Reserve < 0 || w.Reserve > w.MaxReserve {
		return fmt.Errorf("damage and magazine must be 1 to %d, maxReserve 0 to %d and reserve between 0 and maxReserve", maxQuantity, maxQuantity)
	}
	if !positive(w.FireInterval) || !positive(w.ReloadTime) || !positive(w.Range) {
		return errors.New("fireInterval, reloadTime and range must be positive")
	}
	return nil
}

func (e EnemyKind) validate() error {
	if e.Name == "" {
		return errors.New("name is empty")
	}
	if !count(e.Health, 1) || !count(e.BoltDamage, 1) {
		return fmt.Errorf("health and boltDamage must be 1 to %d", maxQuantity)
	}
	for _, v := range []float32{e.Radius, e.Height, e.Speed, e.SightRange, e.FieldOfView, e.AttackRange, e.FireInterval, e.BoltSpeed} {
		if !positive(v) {
			return errors.New("radius, height, speed, sightRange, fieldOfView, attackRange, fireInterval and boltSpeed must be positive")
		}
	}
	if e.Height < 2*e.Radius || e.FieldOfView > 2*math.Pi {
		return errors.New("height must be at least twice the radius and fieldOfView at most 2π")
	}
	for _, c := range e.Color {
		if !(c >= 0 && c <= 1) {
			return errors.New("color channels must be in [0, 1]")
		}
	}
	return nil
}

func (p PickupKind) validate() error {
	if len(p.Symbol) != 1 || bytes.ContainsAny([]byte(p.Symbol), "#c.PS123456789") || p.Symbol[0] <= ' ' || p.Symbol[0] > '~' {
		return fmt.Errorf("symbol %q must be one printable character the layout does not already use", p.Symbol)
	}
	if p.Effect != EffectHealth && p.Effect != EffectAmmo {
		return fmt.Errorf("effect %q, want %s or %s", p.Effect, EffectHealth, EffectAmmo)
	}
	if !count(p.Amount, 1) || !positive(p.Respawn) {
		return fmt.Errorf("amount must be 1 to %d and respawn positive", maxQuantity)
	}
	return nil
}

func (a *Arena) validateLayout() error {
	rows := len(a.Layout)
	if rows < 3 || rows > 64 {
		return errors.New("arena: layout must have 3 to 64 rows")
	}
	cols := len(a.Layout[0])
	if cols < 3 || cols > 64 {
		return errors.New("arena: layout rows must have 3 to 64 cells")
	}
	players, spawns := 0, 0
	waypoints := map[byte]bool{}
	for r, line := range a.Layout {
		if len(line) != cols {
			return fmt.Errorf("arena: layout row %d has %d cells, want %d", r+1, len(line), cols)
		}
		for c := 0; c < cols; c++ {
			ch := line[c]
			border := r == 0 || c == 0 || r == rows-1 || c == cols-1
			if border && ch != cellWall {
				return fmt.Errorf("arena: layout cell (%d, %d) is on the border and must be wall", c, r)
			}
			switch {
			case ch == cellWall, ch == cellCrate, ch == cellFloor:
			case ch == cellPlayer:
				players++
			case ch == cellSpawn:
				spawns++
			case ch >= '1' && ch <= '9':
				if waypoints[ch] {
					return fmt.Errorf("arena: patrol waypoint %c appears twice", ch)
				}
				waypoints[ch] = true
			case a.pickup(ch) != nil:
			default:
				return fmt.Errorf("arena: layout cell (%d, %d) is %q, which is not a cell or a pickup symbol", c, r, ch)
			}
		}
	}
	if players != 1 {
		return fmt.Errorf("arena: layout has %d player starts, want 1", players)
	}
	if spawns == 0 {
		return errors.New("arena: layout has no enemy spawn")
	}
	if len(waypoints) < 2 {
		return errors.New("arena: layout needs a patrol route of at least two waypoints")
	}
	for i := range len(waypoints) {
		if !waypoints[byte('1'+i)] {
			return fmt.Errorf("arena: patrol waypoints must run from 1 without gaps; %d is missing", i+1)
		}
	}
	return a.validateReachable()
}

// validateReachable checks that the player can walk to every open cell, so no spawn, waypoint or pickup is walled off.
func (a *Arena) validateReachable() error {
	start, _ := a.find(cellPlayer)
	rows, cols := len(a.Layout), len(a.Layout[0])
	seen := make([]bool, rows*cols)
	stack := []gridCell{start}
	seen[start.row*cols+start.col] = true
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, d := range [4]gridCell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := gridCell{c.col + d.col, c.row + d.row}
			if a.open(n) && !seen[n.row*cols+n.col] {
				seen[n.row*cols+n.col] = true
				stack = append(stack, n)
			}
		}
	}
	for r := range rows {
		for c := range cols {
			if a.open(gridCell{c, r}) && !seen[r*cols+c] {
				return fmt.Errorf("arena: layout cell (%d, %d) cannot be reached from the player's start", c, r)
			}
		}
	}
	return nil
}

// gridCell is a layout cell by column and row.
type gridCell struct{ col, row int }

// open reports whether c is inside the layout and walkable.
func (a *Arena) open(c gridCell) bool {
	if c.row < 0 || c.row >= len(a.Layout) || c.col < 0 || c.col >= len(a.Layout[c.row]) {
		return false
	}
	ch := a.Layout[c.row][c.col]
	return ch != cellWall && ch != cellCrate
}

// find returns the first cell holding ch, scanning rows top to bottom.
func (a *Arena) find(ch byte) (gridCell, bool) {
	for r, line := range a.Layout {
		for c := 0; c < len(line); c++ {
			if line[c] == ch {
				return gridCell{c, r}, true
			}
		}
	}
	return gridCell{}, false
}

func (a *Arena) enemy(name string) *EnemyKind {
	for i := range a.Enemies {
		if a.Enemies[i].Name == name {
			return &a.Enemies[i]
		}
	}
	return nil
}

func (a *Arena) pickup(symbol byte) *PickupKind {
	for i := range a.Pickups {
		if a.Pickups[i].Symbol[0] == symbol {
			return &a.Pickups[i]
		}
	}
	return nil
}

// maxQuantity bounds every number in an arena file, lengths, speeds and times as well as health, damage, ammunition and pickup amounts, so a malformed file cannot overflow the physics or the player's health and ammunition.
const maxQuantity = 10000

func positive(v float32) bool    { return v > 0 && v <= maxQuantity }
func nonNegative(v float32) bool { return v >= 0 && v <= maxQuantity }

// wholeQuantity reports whether v is a whole quantity from lo to maxQuantity.
func count(v, lo int) bool { return v >= lo && v <= maxQuantity }
