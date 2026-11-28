package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/physics3d"
)

// Collision layers: walls and crates, enemies and the player. Hitscan shots and rounds test walls and enemies, enemy sight tests walls alone, and enemy bolts test walls and the player.
var (
	layerWall   = physics3d.LayerBit(0)
	layerEnemy  = physics3d.LayerBit(1)
	layerPlayer = physics3d.LayerBit(2)
)

// phase is where the match is.
type phase int

const (
	// phaseCountdown waits for the first wave.
	phaseCountdown phase = iota
	// phaseFight runs while a wave's enemies are alive.
	phaseFight
	// phaseIntermission waits between waves.
	phaseIntermission
	// phaseWon follows clearing the last wave.
	phaseWon
	// phaseLost follows the player's health reaching 0.
	phaseLost
)

// controls are one step's intent, from whichever device the player uses: the match never reads a device itself, so tests and replays drive it directly.
type controls struct {
	// moveX strafes right and moveY walks forward, each in [-1, 1].
	moveX, moveY float32
	// turn turns the view left and tilt raises it, in radians for this step.
	turn, tilt float32
	// jump and fire are held this step; reload and next are pressed this step.
	jump, fire, reload, next bool
	// slot, from 1, draws that weapon this step; 0 keeps the current one.
	slot int
}

// match is one game from the countdown to a win or a loss: the physics world, the player, the enemies, the rounds in flight and the pickups. It is deterministic: the same controls each step play the same game.
type match struct {
	arena *Arena
	world *physics3d.World
	nav   *pathfind.BoolGrid
	astar *pathfind.AStar
	route []physics3d.Vec3

	player  player
	enemies []*enemy
	shots   []shot
	tracers []tracer
	pickups []pickup
	spawns  []spawnPoint

	phase   phase
	wave    int // index of the wave being fought or coming next
	pending []*EnemyKind
	timer   float32 // seconds left in the countdown or intermission
	clock   float32 // seconds since the match began

	hitFlash  float32 // seconds the crosshair shows a hit
	aimAssist aimAssist3D
}

// player is the player's body, view, health and weapons.
type player struct {
	ctrl       *physics3d.CharacterController
	body       *physics3d.Body
	prevFeet   physics3d.Vec3
	yaw, pitch float32
	health     int
	weapons    []weaponState
	weapon     int
	cooldown   float32
	reloading  float32
	hurtFlash  float32
	recoil     float32
}

// weaponState is one weapon's ammunition.
type weaponState struct {
	cfg     *Weapon
	mag     int
	reserve int
}

// shot is a round in flight: the player's projectile weapon or an enemy's bolt.
type shot struct {
	pos, vel  physics3d.Vec3
	left      float32 // distance it may still fly
	damage    int
	splash    float32
	fromEnemy bool
}

// tracer is the short-lived streak of a hitscan shot.
type tracer struct {
	from, to physics3d.Vec3
	life     float32
}

// pickup is a health or ammo pickup at its layout cell.
type pickup struct {
	kind    *PickupKind
	pos     physics3d.Vec3
	respawn float32 // seconds until it returns; 0 while it is there
}

// spawnPoint is an S cell enemies enter at, one at a time.
type spawnPoint struct {
	pos      physics3d.Vec3
	cooldown float32
}

const (
	tracerLife    = 0.08
	hitFlashTime  = 0.12
	hurtFlashTime = 0.35
	spawnCooldown = 1.0
	pitchLimit    = 1.45
	pickupRadius  = 1.1
	shotHitRadius = 0.1
	fallLimit     = -20
)

// newMatch builds the arena's physics world, navigation grid, player, pickups and spawns and starts the countdown.
func newMatch(a *Arena) *match {
	m := &match{
		arena:     a,
		world:     physics3d.NewWorld(),
		nav:       pathfind.NewBoolGrid(len(a.Layout[0]), len(a.Layout)),
		astar:     pathfind.NewAStar(),
		phase:     phaseCountdown,
		timer:     a.Countdown,
		aimAssist: aimAssist3D{Strength: 0.7},
	}
	m.astar.Diagonal = true
	route := map[byte]physics3d.Vec3{}
	for r, line := range a.Layout {
		for c := 0; c < len(line); c++ {
			ch := line[c]
			cell := gridCell{c, r}
			m.nav.SetWalkable(c, r, a.open(cell))
			center := m.cellCenter(cell)
			switch {
			case ch == cellWall:
				m.addBlock(cell, a.WallHeight)
			case ch == cellCrate:
				m.addBlock(cell, a.CrateHeight)
			case ch == cellSpawn:
				m.spawns = append(m.spawns, spawnPoint{pos: center})
			case ch >= '1' && ch <= '9':
				route[ch] = center
			case a.pickup(ch) != nil:
				m.pickups = append(m.pickups, pickup{kind: a.pickup(ch), pos: center})
			}
		}
	}
	for i := range len(route) {
		m.route = append(m.route, route[byte('1'+i)])
	}
	// The floor is one slab under the whole arena.
	w, d := float32(len(a.Layout[0]))*a.CellSize, float32(len(a.Layout))*a.CellSize
	floor := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: w / 2, Y: 0.5, Z: d / 2}}), physics3d.Vec3{X: w / 2, Y: -0.5, Z: d / 2})
	floor.Layer = layerWall
	m.world.AddBody(floor)

	start, _ := a.find(cellPlayer)
	p := a.Player
	m.player = player{
		ctrl:   physics3d.NewCharacterController(m.cellCenter(start), p.Radius, p.Height),
		health: p.Health,
	}
	body := physics3d.Body{Shape: physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{Y: p.Height / 2}, p.Radius, p.Height)), Pos: m.player.ctrl.Pos, Kinematic: true, Layer: layerPlayer}
	m.player.body = m.world.AddBody(body)
	m.player.ctrl.IgnoreBodyID = m.player.body.ID
	m.player.ctrl.Mask = layerWall | layerEnemy
	m.player.prevFeet = m.player.ctrl.Pos
	for i := range a.Weapons {
		w := &a.Weapons[i]
		m.player.weapons = append(m.player.weapons, weaponState{cfg: w, mag: w.Magazine, reserve: w.Reserve})
	}
	return m
}

// addBlock adds a static box filling cell up to height.
func (m *match) addBlock(cell gridCell, height float32) {
	s := m.arena.CellSize
	c := m.cellCenter(cell)
	b := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: s / 2, Y: height / 2, Z: s / 2}}), physics3d.Vec3{X: c.X, Y: height / 2, Z: c.Z})
	b.Layer = layerWall
	m.world.AddBody(b)
}

// cellCenter returns the floor point at the middle of cell.
func (m *match) cellCenter(cell gridCell) physics3d.Vec3 {
	s := m.arena.CellSize
	return physics3d.Vec3{X: (float32(cell.col) + 0.5) * s, Z: (float32(cell.row) + 0.5) * s}
}

// cellAt returns the cell under p.
func (m *match) cellAt(p physics3d.Vec3) pathfind.Cell {
	s := m.arena.CellSize
	return pathfind.Cell{Col: int(p.X / s), Row: int(p.Z / s)}
}

// over reports whether the match has been won or lost.
func (m *match) over() bool { return m.phase == phaseWon || m.phase == phaseLost }

// step advances the match by dt under c.
func (m *match) step(c controls, dt time.Duration) {
	secs := float32(dt.Seconds())
	if m.over() {
		return
	}
	m.clock += secs
	m.player.prevFeet = m.player.ctrl.Pos
	for _, e := range m.enemies {
		e.prevFeet = e.ctrl.Pos
	}
	m.hitFlash = max(0, m.hitFlash-secs)
	m.player.hurtFlash = max(0, m.player.hurtFlash-secs)

	m.movePlayer(c, dt)
	m.useWeapon(c, secs)
	m.runWaves(secs)
	// Clearing the last wave ends the match at once, so a bolt still in flight from an enemy that has since died cannot turn the win into a loss.
	if m.over() {
		return
	}
	for _, e := range m.enemies {
		e.think(m, dt)
	}
	m.world.Step(dt)
	m.flyShots(secs)
	// A lethal hit ends the match before pickups, so a health pickup taken in the same step cannot undo it.
	if m.player.health <= 0 || m.player.ctrl.Pos.Y < fallLimit {
		m.player.health = max(0, m.player.health)
		m.phase = phaseLost
		return
	}
	m.collectPickups(secs)
	m.fadeTracers(secs)
}

// movePlayer turns the view and walks, strafes and jumps the player's capsule through the world.
func (m *match) movePlayer(c controls, dt time.Duration) {
	p := &m.player
	cfg := m.arena.Player
	p.yaw = wrapAngle(p.yaw + finite(c.turn))
	p.pitch = clamp(p.pitch+finite(c.tilt), -pitchLimit, pitchLimit)

	fwd, right := flatForward(p.yaw), flatRight(p.yaw)
	wish := fwd.Scale(clamp(finite(c.moveY), -1, 1)).Add(right.Scale(clamp(finite(c.moveX), -1, 1)))
	if l := wish.Len(); l > 1 {
		wish = wish.Scale(1 / l)
	}
	wish = wish.Scale(cfg.Speed)
	if c.jump && p.ctrl.Grounded {
		wish.Y = cfg.JumpSpeed
	}
	p.ctrl.Move(m.world, wish, dt)
	p.body.Pos = p.ctrl.Pos
}

// useWeapon switches, reloads and fires the drawn weapon.
func (m *match) useWeapon(c controls, secs float32) {
	p := &m.player
	p.cooldown = max(0, p.cooldown-secs)
	p.recoil = max(0, p.recoil-secs*6)
	switch {
	case c.slot >= 1 && c.slot <= len(p.weapons) && c.slot-1 != p.weapon:
		p.weapon, p.reloading = c.slot-1, 0
	case c.next && len(p.weapons) > 1:
		p.weapon, p.reloading = (p.weapon+1)%len(p.weapons), 0
	}
	w := &p.weapons[p.weapon]
	if p.reloading > 0 {
		p.reloading -= secs
		if p.reloading <= 0 {
			p.reloading = 0
			n := min(w.cfg.Magazine-w.mag, w.reserve)
			w.mag += n
			w.reserve -= n
		}
		return
	}
	if c.reload && w.mag < w.cfg.Magazine && w.reserve > 0 {
		p.reloading = w.cfg.ReloadTime
		return
	}
	if !c.fire || p.cooldown > 0 {
		return
	}
	if w.mag == 0 {
		if w.reserve > 0 {
			p.reloading = w.cfg.ReloadTime
		}
		return
	}
	w.mag--
	p.cooldown = w.cfg.FireInterval
	p.recoil = 1
	m.fire(w.cfg)
}

// eye returns the player's eye point.
func (m *match) eye() physics3d.Vec3 {
	return m.player.ctrl.Pos.Add(physics3d.Vec3{Y: m.arena.Player.EyeHeight})
}

// clearLine reports whether no wall or crate blocks the segment from from to to, nor the two segments beside it margin to the left and right.
func (m *match) clearLine(from, to physics3d.Vec3, margin float32) bool {
	d := to.Sub(from)
	side := physics3d.Vec3{X: -d.Z, Z: d.X}
	if l := side.Len(); l > 0 {
		side = side.Scale(margin / l)
	}
	for _, off := range [3]float32{0, 1, -1} {
		o := side.Scale(off)
		if _, blocked := m.world.RaycastFirstLayer(from.Add(o), to.Add(o), layerWall); blocked {
			return false
		}
	}
	return true
}

// chest returns the point enemies aim at.
func (m *match) chest() physics3d.Vec3 {
	return m.player.ctrl.Pos.Add(physics3d.Vec3{Y: m.arena.Player.Height * 0.6})
}

// aim returns the unit direction the player looks along.
func (m *match) aim() physics3d.Vec3 {
	direction := lookDir(m.player.yaw, m.player.pitch)
	if m.aimAssist.Strength <= 0 || len(m.enemies) == 0 {
		return direction
	}
	from := m.eye()
	targets := make([]aimTarget3D, 0, len(m.enemies))
	for _, e := range m.enemies {
		targets = append(targets, aimTarget3D{Position: toMath(e.center()), Radius: e.kind.Radius, Priority: 1})
	}
	return toPhysics(m.aimAssist.evaluate(toMath(from), toMath(direction), targets, func(from, to math3d.Vec3) bool {
		return m.clearLine(toPhysics(from), toPhysics(to), 0)
	}))
}

// fire shoots w from the player's eye: a hitscan shot hits at once, a projectile round starts flying. Either one alerts every enemy that hears it.
func (m *match) fire(w *Weapon) {
	from, dir := m.eye(), m.aim()
	switch w.Kind {
	case WeaponHitscan:
		to := from.Add(dir.Scale(w.Range))
		if hit, ok := m.world.RaycastFirstLayer(from, to, layerWall|layerEnemy); ok {
			to = hit.Point
			if e := m.enemyOf(hit.Body); e != nil {
				m.damageEnemy(e, w.Damage)
			}
		}
		muzzle := from.Add(dir.Scale(0.6)).Add(flatRight(m.player.yaw).Scale(0.25)).Add(physics3d.Vec3{Y: -0.2})
		m.tracers = append(m.tracers, tracer{from: muzzle, to: to, life: tracerLife})
	case WeaponProjectile:
		m.shots = append(m.shots, shot{pos: from.Add(dir.Scale(0.5)), vel: dir.Scale(w.Speed), left: w.Range, damage: w.Damage, splash: w.SplashRadius})
	}
	for _, e := range m.enemies {
		if e.ctrl.Pos.Dist(m.player.ctrl.Pos) <= hearingRange {
			e.alert(m.player.ctrl.Pos)
		}
	}
}

// hearingRange is how far a shot alerts enemies.
const hearingRange = 22

// flyShots moves every round along its path, bursting the player's on walls and enemies and landing enemy bolts on the player.
func (m *match) flyShots(secs float32) {
	kept := m.shots[:0]
	for _, s := range m.shots {
		step := s.vel.Scale(secs)
		dist := step.Len()
		if dist > s.left {
			step = step.Scale(s.left / dist)
			dist = s.left
		}
		to := s.pos.Add(step)
		mask := layerWall | layerEnemy
		if s.fromEnemy {
			mask = layerWall | layerPlayer
		}
		hit, ok := m.world.RaycastFirstLayer(s.pos, to, mask)
		switch {
		case ok && s.fromEnemy:
			if hit.Body == m.player.body {
				m.hurtPlayer(s.damage)
			}
		case ok:
			m.burst(hit.Point, s)
		case dist >= s.left:
			if !s.fromEnemy {
				m.burst(to, s)
			}
		default:
			s.pos, s.left = to, s.left-dist
			kept = append(kept, s)
		}
	}
	clear(m.shots[len(kept):])
	m.shots = kept
}

// burst hurts every enemy within the round's splash radius, less the further it stands from the center.
func (m *match) burst(at physics3d.Vec3, s shot) {
	for _, e := range append([]*enemy(nil), m.enemies...) {
		d := e.center().Dist(at)
		if d < s.splash {
			m.damageEnemy(e, max(1, int(float32(s.damage)*(1-d/s.splash))))
		}
	}
	m.tracers = append(m.tracers, tracer{from: at, to: at, life: tracerLife * 3})
}

// hurtPlayer removes damage from the player's health.
func (m *match) hurtPlayer(damage int) {
	m.player.health -= damage
	m.player.hurtFlash = hurtFlashTime
}

// damageEnemy removes damage from e, which leaves the world at 0 health, and makes it hunt the player.
func (m *match) damageEnemy(e *enemy, damage int) {
	m.hitFlash = hitFlashTime
	e.health -= damage
	e.hurtFlash = hitFlashTime
	e.alert(m.player.ctrl.Pos)
	if e.health > 0 {
		return
	}
	m.world.RemoveBody(e.body.ID)
	for i, o := range m.enemies {
		if o == e {
			m.enemies = append(m.enemies[:i], m.enemies[i+1:]...)
			break
		}
	}
}

// enemyOf returns the enemy whose body b is, or nil.
func (m *match) enemyOf(b *physics3d.Body) *enemy {
	if b == nil {
		return nil
	}
	for _, e := range m.enemies {
		if e.body == b {
			return e
		}
	}
	return nil
}

// runWaves counts the countdown and intermissions down, queues each wave's enemies, spawns them at free spawns one at a time and ends the match when the last wave is cleared.
func (m *match) runWaves(secs float32) {
	switch m.phase {
	case phaseCountdown, phaseIntermission:
		m.timer -= secs
		if m.timer > 0 {
			return
		}
		m.timer = 0
		m.phase = phaseFight
		for _, g := range m.arena.Waves[m.wave].Groups {
			for range g.Count {
				m.pending = append(m.pending, m.arena.enemy(g.Enemy))
			}
		}
	case phaseFight:
		if len(m.pending) == 0 && len(m.enemies) == 0 {
			if m.wave == len(m.arena.Waves)-1 {
				m.phase = phaseWon
				return
			}
			m.wave++
			m.phase = phaseIntermission
			m.timer = m.arena.Intermission
		}
	}
	for i := range m.spawns {
		sp := &m.spawns[i]
		sp.cooldown = max(0, sp.cooldown-secs)
		if len(m.pending) == 0 || sp.cooldown > 0 || m.spawnBlocked(sp.pos) {
			continue
		}
		m.enemies = append(m.enemies, newEnemy(m, m.pending[0], sp.pos))
		m.pending = m.pending[1:]
		sp.cooldown = spawnCooldown
	}
}

// spawnBlocked reports whether an enemy or the player stands too close to spawn at p.
func (m *match) spawnBlocked(p physics3d.Vec3) bool {
	if horizontalDist(m.player.ctrl.Pos, p) < 1.5 {
		return true
	}
	for _, e := range m.enemies {
		if horizontalDist(e.ctrl.Pos, p) < 1.5 {
			return true
		}
	}
	return false
}

// collectPickups gives the player any pickup it walks over that it can use, and brings taken pickups back.
func (m *match) collectPickups(secs float32) {
	p := &m.player
	for i := range m.pickups {
		pk := &m.pickups[i]
		if pk.respawn > 0 {
			pk.respawn = max(0, pk.respawn-secs)
			continue
		}
		if horizontalDist(p.ctrl.Pos, pk.pos) > pickupRadius || p.ctrl.Pos.Y-pk.pos.Y > 1 {
			continue
		}
		used := false
		switch pk.kind.Effect {
		case EffectHealth:
			if p.health < m.arena.Player.Health {
				p.health = min(m.arena.Player.Health, p.health+pk.kind.Amount)
				used = true
			}
		case EffectAmmo:
			for j := range p.weapons {
				w := &p.weapons[j]
				if w.reserve < w.cfg.MaxReserve {
					w.reserve = min(w.cfg.MaxReserve, w.reserve+pk.kind.Amount*w.cfg.Magazine)
					used = true
				}
			}
		}
		if used {
			pk.respawn = pk.kind.Respawn
		}
	}
}

func (m *match) fadeTracers(secs float32) {
	kept := m.tracers[:0]
	for _, t := range m.tracers {
		if t.life -= secs; t.life > 0 {
			kept = append(kept, t)
		}
	}
	m.tracers = kept
}

// lookDir returns the unit view direction for yaw (0 looks along -Z, positive turns left) and pitch (positive looks up).
func lookDir(yaw, pitch float32) physics3d.Vec3 {
	sy, cy := sincos(yaw)
	sp, cp := sincos(pitch)
	return physics3d.Vec3{X: -sy * cp, Y: sp, Z: -cy * cp}
}

// flatForward returns the horizontal unit direction for yaw.
func flatForward(yaw float32) physics3d.Vec3 {
	s, c := sincos(yaw)
	return physics3d.Vec3{X: -s, Z: -c}
}

// flatRight returns the horizontal unit direction to the right of yaw.
func flatRight(yaw float32) physics3d.Vec3 {
	s, c := sincos(yaw)
	return physics3d.Vec3{X: c, Z: -s}
}

// yawTo returns the yaw that faces from a toward b.
func yawTo(a, b physics3d.Vec3) float32 {
	return float32(math.Atan2(float64(a.X-b.X), float64(a.Z-b.Z)))
}

func horizontalDist(a, b physics3d.Vec3) float32 {
	return float32(math.Hypot(float64(a.X-b.X), float64(a.Z-b.Z)))
}

func sincos(a float32) (float32, float32) {
	s, c := math.Sincos(float64(a))
	return float32(s), float32(c)
}

// wrapAngle returns a in [-π, π].
func wrapAngle(a float32) float32 {
	return float32(math.Remainder(float64(a), 2*math.Pi))
}

// finite returns v, or 0 when v is not a finite number, so one corrupt reading cannot poison the view.
func finite(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return 0
	}
	return v
}

func clamp(v, lo, hi float32) float32 { return min(max(v, lo), hi) }
