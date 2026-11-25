package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/stats"
	"github.com/nsgo-dev/game/packages/game/tilemap"
)

// StepRate is how many fixed steps the world runs a second.
const StepRate = 60

// stepTime is the length of one fixed step.
const stepTime = time.Second / StepRate

// Outcome is how a game stands: still being played, won or lost.
type Outcome uint8

const (
	// Playing is a game that is neither won nor lost.
	Playing Outcome = iota
	// Won is a game whose last wave was cleared with lives left.
	Won
	// Lost is a game whose lives ran out.
	Lost
)

// String returns the outcome's name.
func (o Outcome) String() string {
	switch o {
	case Won:
		return "won"
	case Lost:
		return "lost"
	default:
		return "playing"
	}
}

// Tile flags the world sets on the map's cells for the tilemap queries.
const (
	// flagRoad marks a cell enemies walk on.
	flagRoad tilemap.TileFlags = 1 << 8
	// flagBuildable marks open ground a tower may be built on.
	flagBuildable tilemap.TileFlags = 1 << 9
)

// World is one game of the level: its map and road, the player's money and lives, the towers, the enemies and the waves. It runs on fixed steps with no clock or randomness of its own, so the same calls give the same game.
type World struct {
	// Data is what the world was made from.
	Data *Data
	// Map is the level's tilemap, its cells flagged road or buildable.
	Map *tilemap.Tilemap

	// Money is the player's money.
	Money int
	// Lives is the lives the player has left.
	Lives int
	// Wave is how many waves have been called.
	Wave int
	// Outcome is how the game stands.
	Outcome Outcome
	// Towers are the built towers, in the order they were built.
	Towers []*Tower
	// Enemies are the enemies on the road, in the order they spawned.
	Enemies []*Enemy

	route    []math2d.Vec2
	routeLen []float32
	spawners []*spawner
	paid     []bool
	atlas    *sprite.Atlas
	steps    uint64
	nextID   int
}

// Tower is one built tower.
type Tower struct {
	// Def is the tower's kind.
	Def *TowerDef
	// Cell is where it stands.
	Cell ai.Cell
	// X and Y are its center in game points.
	X, Y float32
	// Stats holds its damage, range, cooldown and other stats, with its upgrades applied.
	Stats *stats.Sheet
	// Level is how many upgrades it has.
	Level int
	// Spent is the money it has cost with its upgrades.
	Spent int
	// Aim is the direction it last fired in, in radians clockwise from +X.
	Aim float32
	// Shots are its projectiles in flight.
	Shots *bullets.Field

	emitter  *bullets.Emitter
	cooldown float64
}

// Enemy is one enemy on the road.
type Enemy struct {
	// ID tells enemies apart, counting from 1 in the order they spawned.
	ID int
	// Def is the enemy's kind.
	Def *EnemyDef
	// Wave is the index in data/level.yaml of the wave that sent it.
	Wave int
	// Stats holds its health, speed and armor, slowed by frost.
	Stats *stats.Sheet
	// Health is its hit points left.
	Health float64
	// Progress is how far along the road it has walked, in game points.
	Progress float32
	// X and Y are where it is.
	X, Y float32
}

// Chilled reports whether a frost tower's slow is on the enemy.
func (e *Enemy) Chilled() bool { return len(e.Stats.Modifiers()) > 0 }

// spawner is one group of a called wave, still spawning.
type spawner struct {
	def     *EnemyDef
	left    int
	wait    time.Duration
	every   time.Duration
	waveIdx int
}

// NewWorld returns a new game of d's level, drawing projectiles from atlas.
func NewWorld(d *Data, atlas *sprite.Atlas) *World {
	l := d.Level
	cols, rows := l.Size()
	m := tilemap.New(cols, rows, l.TileSize, l.TileSize, groundTileset(atlas))
	layer := tilemap.NewTileLayer("ground", cols, rows, nil)
	for row := range rows {
		for col := range cols {
			t := l.Tile(col, row)
			layer.Data[row*cols+col] = groundGID(t, col, row)
			switch {
			case l.Walkable(col, row):
				m.SetCellFlags(col, row, flagRoad)
			case t == TileGrass:
				m.SetCellFlags(col, row, flagBuildable)
			default:
				m.SetCellFlags(col, row, tilemap.TileSolid)
			}
		}
	}
	m.Layers = append(m.Layers, layer)

	// Validate found this route, so the level always has one.
	cells, _ := l.route()
	w := &World{Data: d, Map: m, atlas: atlas}
	w.route = make([]math2d.Vec2, len(cells))
	w.routeLen = make([]float32, len(cells))
	for i, c := range cells {
		w.route[i] = w.cellCenter(c)
		if i > 0 {
			w.routeLen[i] = w.routeLen[i-1] + w.route[i].Sub(w.route[i-1]).Len()
		}
	}
	w.Reset()
	return w
}

// Reset starts the game over: the starting money and lives, no towers, no enemies and no waves called.
func (w *World) Reset() {
	w.Money = w.Data.Level.Money
	w.Lives = w.Data.Level.Lives
	w.Wave = 0
	w.Outcome = Playing
	w.Towers = nil
	w.Enemies = nil
	w.spawners = nil
	w.paid = make([]bool, len(w.Data.Level.Waves))
	w.steps = 0
	w.nextID = 0
}

// Waves returns how many waves the level has.
func (w *World) Waves() int { return len(w.Data.Level.Waves) }

// Route returns the centers of the road's tiles from the spawn to the gate.
func (w *World) Route() []math2d.Vec2 { return w.route }

// RouteLength returns the length of the road from the spawn to the gate, in game points.
func (w *World) RouteLength() float32 { return w.routeLen[len(w.routeLen)-1] }

// Spawning reports whether the last called wave still has enemies to send.
func (w *World) Spawning() bool { return len(w.spawners) > 0 }

// CanCallWave reports whether the next wave may be called: the game is on, waves are left, and the last one has sent all its enemies.
func (w *World) CanCallWave() bool {
	return w.Outcome == Playing && w.Wave < w.Waves() && !w.Spawning()
}

// CallWave sends the next wave and reports whether it did, as CanCallWave says.
func (w *World) CallWave() bool {
	if !w.CanCallWave() {
		return false
	}
	idx := w.Wave
	w.Wave++
	for _, g := range w.Data.Level.Waves[idx].Groups {
		// Validate checked that every group names a known enemy.
		def, _ := w.Data.Level.Enemy(g.Enemy)
		w.spawners = append(w.spawners, &spawner{def: def, left: g.Count, wait: g.Delay, every: g.Interval, waveIdx: idx})
	}
	return true
}

// Size returns the map's size in game points.
func (w *World) Size() (width, height float32) {
	b := w.Map.WorldBounds()
	return float32(b.Size.Width), float32(b.Size.Height)
}

// gridSize returns the map's columns and rows.
func (w *World) gridSize() (cols, rows int) { return w.Map.Width, w.Map.Height }

// InMap reports whether c is a cell of the map.
func (w *World) InMap(c ai.Cell) bool {
	cols, rows := w.gridSize()
	return c.Col >= 0 && c.Row >= 0 && c.Col < cols && c.Row < rows
}

// CellAt returns the cell under the game point (x, y), and false outside the map.
func (w *World) CellAt(x, y float32) (ai.Cell, bool) {
	if x < 0 || y < 0 {
		return ai.Cell{}, false
	}
	c := ai.Cell{Col: int(x / w.Map.TileWidth), Row: int(y / w.Map.TileHeight)}
	return c, w.InMap(c)
}

// cellCenter returns the center of c in game points.
func (w *World) cellCenter(c ai.Cell) math2d.Vec2 {
	return math2d.Vec2{X: (float32(c.Col) + 0.5) * w.Map.TileWidth, Y: (float32(c.Row) + 0.5) * w.Map.TileHeight}
}

// TowerAt returns the tower standing on c.
func (w *World) TowerAt(c ai.Cell) (*Tower, bool) {
	for _, t := range w.Towers {
		if t.Cell == c {
			return t, true
		}
	}
	return nil, false
}

// Buildable reports whether a tower may stand on c: open ground with no tower on it.
func (w *World) Buildable(c ai.Cell) bool {
	if !w.InMap(c) || w.Map.FlagsAt(c.Col, c.Row)&flagBuildable == 0 {
		return false
	}
	_, taken := w.TowerAt(c)
	return !taken
}

// Build builds a tower of def on c, paying its cost. It fails, changing nothing, once the game is over, where Buildable refuses c, and when the money is short.
func (w *World) Build(def *TowerDef, c ai.Cell) (*Tower, error) {
	switch {
	case w.Outcome != Playing:
		return nil, fmt.Errorf("the game is over")
	case !w.Buildable(c):
		return nil, fmt.Errorf("cannot build there")
	case w.Money < def.Cost:
		return nil, fmt.Errorf("%s needs %d", def.Label, def.Cost)
	}
	// Validate checked the stats, and LoadData that the pattern exists and draws a frame that the atlas holds.
	sheet, _ := stats.NewSheet(def.Stats...)
	pattern, _ := w.Data.Projectiles.Pattern(def.Pattern)
	field := bullets.NewField(w.atlas, 16)
	mw, mh := w.Size()
	field.Bounds = math2d.Rect{W: mw, H: mh}
	emitter, err := bullets.NewEmitter(field, pattern, uint64(len(w.Towers)+1))
	if err != nil {
		return nil, err
	}
	center := w.cellCenter(c)
	t := &Tower{Def: def, Cell: c, X: center.X, Y: center.Y, Stats: sheet, Spent: def.Cost, Aim: -math.Pi / 2, Shots: field, emitter: emitter}
	w.Money -= def.Cost
	w.Towers = append(w.Towers, t)
	return t, nil
}

// UpgradeCost returns the cost of t's next upgrade, and false when it has none left.
func (t *Tower) UpgradeCost() (int, bool) {
	if t.Level >= len(t.Def.Upgrades) {
		return 0, false
	}
	return t.Def.Upgrades[t.Level].Cost, true
}

// Upgrade buys t's next upgrade. It fails, changing nothing, once the game is over, when t has no upgrade left and when the money is short.
func (w *World) Upgrade(t *Tower) error {
	cost, ok := t.UpgradeCost()
	switch {
	case w.Outcome != Playing:
		return fmt.Errorf("the game is over")
	case !ok:
		return fmt.Errorf("%s is fully upgraded", t.Def.Label)
	case w.Money < cost:
		return fmt.Errorf("upgrade needs %d", cost)
	}
	name := fmt.Sprintf("upgrade-%d", t.Level+1)
	// Validate checked every upgrade's modifiers against the tower's stats.
	_, _ = t.Stats.Apply(name, t.Def.Upgrades[t.Level].Effect(name))
	t.Level++
	t.Spent += cost
	w.Money -= cost
	return nil
}

// SellValue returns the money selling t returns.
func (w *World) SellValue(t *Tower) int {
	return int(math.Floor(float64(t.Spent) * w.Data.Towers.Refund))
}

// Sell removes t, refunding SellValue. It fails once the game is over.
func (w *World) Sell(t *Tower) error {
	if w.Outcome != Playing {
		return fmt.Errorf("the game is over")
	}
	for i, o := range w.Towers {
		if o == t {
			w.Money += w.SellValue(t)
			w.Towers = append(w.Towers[:i], w.Towers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no such tower")
}

// Step runs the world on by one fixed step: enemies spawn and walk, towers aim and fire, projectiles fly and hit, and the outcome is settled. A game that is over does not change.
func (w *World) Step() {
	if w.Outcome != Playing {
		return
	}
	w.steps++
	w.spawn()
	w.walk()
	for _, t := range w.Towers {
		t.Stats.Step()
		t.Shots.Update(stepTime)
		w.fire(t)
		w.hit(t)
	}
	w.settle()
}

// spawn sends the enemies each group has due.
func (w *World) spawn() {
	n := 0
	for _, s := range w.spawners {
		s.wait -= stepTime
		for s.left > 0 && s.wait <= 0 {
			w.addEnemy(s.def, s.waveIdx)
			s.left--
			s.wait += s.every
		}
		if s.left > 0 {
			w.spawners[n] = s
			n++
		}
	}
	clear(w.spawners[n:])
	w.spawners = w.spawners[:n]
}

func (w *World) addEnemy(def *EnemyDef, wave int) {
	// Validate checked the enemy's stats.
	sheet, _ := stats.NewSheet(def.Stats...)
	w.nextID++
	e := &Enemy{ID: w.nextID, Def: def, Wave: wave, Stats: sheet, Health: sheet.Value(statHealth)}
	e.X, e.Y = w.routePoint(0)
	w.Enemies = append(w.Enemies, e)
}

// walk moves every enemy along the road and takes lives for those that reach the gate.
func (w *World) walk() {
	end := w.RouteLength()
	n := 0
	for _, e := range w.Enemies {
		e.Stats.Step()
		e.Progress += float32(e.Stats.Value(statSpeed) * stepTime.Seconds())
		if e.Progress >= end {
			w.Lives = max(0, w.Lives-e.Def.Leak)
			continue
		}
		e.X, e.Y = w.routePoint(e.Progress)
		w.Enemies[n] = e
		n++
	}
	clear(w.Enemies[n:])
	w.Enemies = w.Enemies[:n]
}

// routePoint returns the point progress game points along the road.
func (w *World) routePoint(progress float32) (x, y float32) {
	if progress <= 0 {
		return w.route[0].X, w.route[0].Y
	}
	last := len(w.route) - 1
	for i := 1; i <= last; i++ {
		if progress <= w.routeLen[i] {
			seg := w.routeLen[i] - w.routeLen[i-1]
			p := w.route[i-1].Lerp(w.route[i], (progress-w.routeLen[i-1])/seg)
			return p.X, p.Y
		}
	}
	return w.route[last].X, w.route[last].Y
}

// fire has t shoot at the enemy in its range that is furthest along the road, leading it by the time its projectile takes to get there.
func (w *World) fire(t *Tower) {
	t.cooldown -= stepTime.Seconds()
	if t.cooldown > 0 {
		return
	}
	reach := float32(t.Stats.Value(statRange))
	var target *Enemy
	for _, e := range w.Enemies {
		if dist(t.X, t.Y, e.X, e.Y) <= reach+e.Def.Radius && (target == nil || e.Progress > target.Progress) {
			target = e
		}
	}
	if target == nil {
		t.cooldown = 0
		return
	}
	speed := t.emitter.Pattern().Speed
	aimX, aimY := target.X, target.Y
	for range 2 {
		flight := dist(t.X, t.Y, aimX, aimY) / speed
		aimX, aimY = w.routePoint(target.Progress + float32(target.Stats.Value(statSpeed))*flight)
	}
	t.Aim = float32(math.Atan2(float64(aimY-t.Y), float64(aimX-t.X)))
	t.emitter.X, t.emitter.Y = t.X, t.Y
	t.emitter.TargetX, t.emitter.TargetY = aimX, aimY
	// The pattern fires one shot, so a reset emitter fires it again on this step.
	t.emitter.Reset()
	t.emitter.Update(stepTime)
	t.cooldown += t.Stats.Value(statCooldown)
}

// hit removes t's projectiles that touch an enemy and damages the enemy, and those around it for a splash, slowing them for a frost tower.
func (w *World) hit(t *Tower) {
	if t.Shots.Len() == 0 || len(w.Enemies) == 0 {
		return
	}
	damage := t.Stats.Value(statDamage)
	splash := float32(t.Stats.Value(statSplash))
	slow := t.Stats.Value(statSlow)
	chill := int(math.Round(t.Stats.Value(statChill) * StepRate))
	t.Shots.RemoveFunc(func(b *bullets.Bullet) bool {
		for _, e := range w.Enemies {
			if e.Health <= 0 || dist(b.X, b.Y, e.X, e.Y) > b.Radius+e.Def.Radius {
				continue
			}
			if splash <= 0 {
				w.damage(e, damage, slow, chill)
				return true
			}
			for _, o := range w.Enemies {
				if o.Health > 0 && dist(b.X, b.Y, o.X, o.Y) <= splash+o.Def.Radius {
					w.damage(o, damage, slow, chill)
				}
			}
			return true
		}
		return false
	})
	n := 0
	for _, e := range w.Enemies {
		if e.Health <= 0 {
			w.Money += e.Def.Reward
			continue
		}
		w.Enemies[n] = e
		n++
	}
	clear(w.Enemies[n:])
	w.Enemies = w.Enemies[:n]
}

// damage takes damage, less the enemy's armor and at least 1, from e's health, and slows it by slow for chill steps.
func (w *World) damage(e *Enemy, damage, slow float64, chill int) {
	e.Health -= max(1, damage-e.Stats.Value(statArmor))
	if slow > 0 && chill > 0 {
		factor := max(0, 1-min(slow, 1))
		// The speed stat exists on every enemy, so the modifier is valid.
		_, _ = e.Stats.Refresh("chill", stats.Effect{Name: "chill", Steps: chill, Modifiers: []stats.Modifier{{Stat: statSpeed, Op: stats.OpMul, Value: factor}}})
	}
}

// settle pays each cleared wave's bonus and decides whether the game is won or lost.
func (w *World) settle() {
	if w.Lives <= 0 {
		w.Outcome = Lost
		w.Enemies = nil
		w.spawners = nil
		return
	}
	// A wave is cleared once it has sent every enemy and none of them is left, even while a later wave is still on the road.
	for i := range w.Wave {
		if !w.paid[i] && w.waveCleared(i) {
			w.paid[i] = true
			w.Money += w.Data.Level.Waves[i].Bonus
		}
	}
	if w.Wave == w.Waves() && !w.Spawning() && len(w.Enemies) == 0 {
		w.Outcome = Won
	}
}

// waveCleared reports whether wave i has no enemy left to send and none on the road.
func (w *World) waveCleared(i int) bool {
	for _, s := range w.spawners {
		if s.waveIdx == i {
			return false
		}
	}
	for _, e := range w.Enemies {
		if e.Wave == i {
			return false
		}
	}
	return true
}

func dist(ax, ay, bx, by float32) float32 {
	return float32(math.Hypot(float64(bx-ax), float64(by-ay)))
}
