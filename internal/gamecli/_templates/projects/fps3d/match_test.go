package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

const step = time.Second / 60

// playMatch runs m with the bot (or standing still when idle) until it ends or limit passes, and returns the steps it took.
func playMatch(m *match, idle bool, limit time.Duration) int {
	var b bot
	maxTurn := arena.Player.TurnSpeed * 3 * float32(step.Seconds())
	steps := 0
	for ; !m.over() && time.Duration(steps)*step < limit; steps++ {
		var c controls
		if !idle {
			in := b.decide(m, maxTurn)
			c = controls{turn: in.turn, tilt: in.tilt, fire: in.fire, reload: in.reload, moveX: in.moveX, moveY: in.moveY}
		}
		m.step(c, step)
	}
	return steps
}

func TestBotClearsEveryWave(t *testing.T) {
	m := newMatch(arena)
	steps := playMatch(m, false, 6*time.Minute)
	if m.phase != phaseWon {
		for _, e := range m.enemies {
			t.Logf("enemy %s at %+v state %d lead %v %+v path %v at %d wish %+v", e.kind.Name, e.ctrl.Pos, e.state, e.hasLead, e.lead, e.path, e.pathAt, e.wish)
		}
		t.Logf("player at %+v", m.player.ctrl.Pos)
		t.Fatalf("after %v the match is in phase %d with %d health on wave %d and %d enemies left, want a win", time.Duration(steps)*step, m.phase, m.player.health, m.wave+1, len(m.enemies)+len(m.pending))
	}
	t.Logf("won in %v with %d health", time.Duration(steps)*step, m.player.health)
}

func TestStandingStillLoses(t *testing.T) {
	m := newMatch(arena)
	steps := playMatch(m, true, 4*time.Minute)
	if m.phase != phaseLost || m.player.health != 0 {
		t.Fatalf("after %v the match is in phase %d with %d health, want a loss at 0", time.Duration(steps)*step, m.phase, m.player.health)
	}
	t.Logf("lost in %v", time.Duration(steps)*step)
}

func TestMatchIsDeterministic(t *testing.T) {
	a, b := newMatch(arena), newMatch(arena)
	playMatch(a, false, 40*time.Second)
	playMatch(b, false, 40*time.Second)
	if a.player.ctrl.Pos != b.player.ctrl.Pos || a.player.health != b.player.health || len(a.enemies) != len(b.enemies) || a.clock != b.clock {
		t.Fatalf("two matches under the same controls diverged: %+v %d vs %+v %d", a.player.ctrl.Pos, a.player.health, b.player.ctrl.Pos, b.player.health)
	}
	for i := range a.enemies {
		if a.enemies[i].ctrl.Pos != b.enemies[i].ctrl.Pos {
			t.Fatalf("enemy %d diverged: %+v vs %+v", i, a.enemies[i].ctrl.Pos, b.enemies[i].ctrl.Pos)
		}
	}
}

func TestPlayerWalksJumpsAndIsStoppedByWalls(t *testing.T) {
	m := newMatch(arena)
	start := m.player.ctrl.Pos
	for range 30 {
		m.step(controls{moveY: 1}, step)
	}
	if d := start.Z - m.player.ctrl.Pos.Z; d < 2 {
		t.Fatalf("walking forward for half a second moved %v m along -Z, want at least 2", d)
	}
	m.step(controls{jump: true}, step)
	for range 10 {
		m.step(controls{}, step)
	}
	if m.player.ctrl.Pos.Y < 0.3 {
		t.Fatalf("a jump reached %v m, want at least 0.3", m.player.ctrl.Pos.Y)
	}
	// Run east for five seconds: the wall at the east edge stops the player inside the arena.
	m = newMatch(arena)
	for range 300 {
		m.step(controls{moveX: 1}, step)
	}
	east := float32(len(arena.Layout[0])-1) * arena.CellSize
	if x := m.player.ctrl.Pos.X; x > east-arena.Player.Radius+0.01 {
		t.Fatalf("running east ended at x %v, past the wall at %v", x, east)
	}
}

func TestHitscanDamagesAndKillsAnEnemy(t *testing.T) {
	m := quietMatch()
	e := placeEnemy(m, "drone", physics3d.Vec3{X: m.player.ctrl.Pos.X + 4, Z: m.player.ctrl.Pos.Z})
	rifle := m.player.weapons[0]
	for i := 0; e.health > 0 && i < 200; i++ {
		turn, tilt := aimAt(m, e)
		m.step(controls{fire: true, turn: turn, tilt: tilt}, step)
	}
	if e.health > 0 || len(m.enemies) != 0 {
		t.Fatalf("the drone has %d health after the rifle fired, want it dead and gone", e.health)
	}
	if got := m.player.weapons[0].mag; got >= rifle.mag {
		t.Fatalf("the magazine holds %d after firing, want fewer than %d", got, rifle.mag)
	}
	if len(m.world.Bodies()) != wallBodies(m) {
		t.Fatalf("the dead drone's body is still in the world")
	}
}

func TestProjectileBurstsOnAnEnemy(t *testing.T) {
	m := quietMatch()
	e := placeEnemy(m, "warden", physics3d.Vec3{X: m.player.ctrl.Pos.X - 5, Z: m.player.ctrl.Pos.Z})
	m.step(controls{slot: 2}, step)
	turn, tilt := aimAt(m, e)
	m.step(controls{fire: true, turn: turn, tilt: tilt}, step)
	if len(m.shots) != 1 {
		t.Fatalf("the launcher has %d rounds in flight, want 1", len(m.shots))
	}
	for i := 0; len(m.shots) > 0 && i < 120; i++ {
		m.step(controls{}, step)
	}
	if e.health >= e.kind.Health || e.health <= 0 {
		t.Fatalf("the warden has %d of %d health after one burst, want it hurt but standing", e.health, e.kind.Health)
	}
}

func TestReloadRefillsTheMagazineFromTheReserve(t *testing.T) {
	m := quietMatch()
	w := &m.player.weapons[0]
	w.mag = 3
	reserve := w.reserve
	m.step(controls{reload: true}, step)
	if m.player.reloading <= 0 {
		t.Fatal("reload did not start")
	}
	for range int(w.cfg.ReloadTime*60) + 2 {
		m.step(controls{}, step)
	}
	if w.mag != w.cfg.Magazine || w.reserve != reserve-(w.cfg.Magazine-3) {
		t.Fatalf("after reloading the magazine holds %d and the reserve %d, want %d and %d", w.mag, w.reserve, w.cfg.Magazine, reserve-(w.cfg.Magazine-3))
	}
	// An empty magazine with no reserve stays empty.
	w.mag, w.reserve = 0, 0
	m.step(controls{fire: true}, step)
	if m.player.reloading != 0 || w.mag != 0 {
		t.Fatalf("firing an empty weapon with no reserve reloaded it")
	}
}

func TestPickupsHealAndRefillAndRespawn(t *testing.T) {
	m := quietMatch()
	var health, ammo *pickup
	for i := range m.pickups {
		switch m.pickups[i].kind.Effect {
		case EffectHealth:
			health = &m.pickups[i]
		case EffectAmmo:
			ammo = &m.pickups[i]
		}
	}
	// A pickup the player cannot use stays.
	m.player.ctrl.Pos = health.pos
	m.step(controls{}, step)
	if health.respawn != 0 {
		t.Fatal("the health pickup was taken at full health")
	}
	m.player.health = 50
	m.step(controls{}, step)
	if m.player.health != 50+health.kind.Amount || health.respawn == 0 {
		t.Fatalf("health is %d after the pickup, want %d", m.player.health, 50+health.kind.Amount)
	}
	w := &m.player.weapons[0]
	w.reserve = 0
	m.player.ctrl.Pos = ammo.pos
	m.step(controls{}, step)
	if want := ammo.kind.Amount * w.cfg.Magazine; w.reserve != want {
		t.Fatalf("reserve is %d after the ammo pickup, want %d", w.reserve, want)
	}
	for range int(health.kind.Respawn*60) + 2 {
		m.step(controls{}, step)
	}
	if health.respawn != 0 {
		t.Fatal("the health pickup did not come back")
	}
}

func TestEnemiesPatrolTheRouteInOrder(t *testing.T) {
	m := quietMatch()
	// A blind drone never notices the player, so it only patrols.
	blind := *m.arena.enemy("drone")
	blind.SightRange = 0.01
	e := newEnemy(m, &blind, m.route[0])
	m.enemies = append(m.enemies, e)
	visited := []int{}
	for range 60 * 60 {
		m.step(controls{}, step)
		if e.state != statePatrol {
			t.Fatalf("the blind drone left its patrol for state %d", e.state)
		}
		if len(visited) == 0 || visited[len(visited)-1] != e.waypoint {
			visited = append(visited, e.waypoint)
		}
	}
	// Starting at waypoint 1 it heads for 2 and goes round the route.
	want := []int{1, 2, 3, 0, 1}
	if len(visited) < len(want) {
		t.Fatalf("in a minute the drone headed for waypoints %v, want at least %v", visited, want)
	}
	for i, w := range want {
		if visited[i] != w {
			t.Fatalf("the drone headed for waypoints %v, want them in route order %v", visited, want)
		}
	}
}

func TestAShotSendsAnEnemyHuntingUntilItAttacks(t *testing.T) {
	m := quietMatch()
	// The center block stands between the drone north of it and the player south of it.
	m.player.ctrl.Pos = m.cellCenter(gridCell{8, 6})
	e := placeEnemy(m, "drone", m.cellCenter(gridCell{8, 2}))
	m.step(controls{}, step)
	if e.state != statePatrol {
		t.Fatalf("before the shot the drone is in state %d, want patrol", e.state)
	}
	m.step(controls{fire: true}, step)
	seen := map[enemyState]bool{}
	for range 20 * 60 {
		m.step(controls{}, step)
		seen[e.state] = true
		if seen[stateAttack] {
			break
		}
	}
	if !seen[stateHunt] || !seen[stateAttack] {
		t.Fatalf("after the shot the drone was in states %v, want it to hunt and then attack", seen)
	}
	if m.player.health == m.arena.Player.Health {
		for range 5 * 60 {
			m.step(controls{}, step)
		}
		if m.player.health == m.arena.Player.Health {
			t.Fatal("the attacking drone never hit the player standing still")
		}
	}
}

func TestWallsBlockEnemySight(t *testing.T) {
	m := newMatch(arena)
	// The center block (row 4) stands between these two cells.
	m.player.ctrl.Pos = m.cellCenter(gridCell{8, 6})
	e := placeEnemy(m, "drone", m.cellCenter(gridCell{8, 2}))
	e.yaw = yawTo(e.ctrl.Pos, m.player.ctrl.Pos)
	if e.sees(m) {
		t.Fatal("the drone sees the player through the wall")
	}
	e.ctrl.Pos = m.cellCenter(gridCell{8, 5})
	e.ctrl.Pos.Z -= 0.5
	if !e.sees(m) {
		t.Fatal("the drone does not see the player in the open")
	}
}

// quietMatch returns a match whose countdown never ends, so no wave interferes with a test that places its own enemies.
func quietMatch() *match {
	m := newMatch(arena)
	m.timer = float32(time.Hour.Seconds())
	return m
}

// placeEnemy adds an enemy of the named kind at feet.
func placeEnemy(m *match, kind string, feet physics3d.Vec3) *enemy {
	e := newEnemy(m, m.arena.enemy(kind), feet)
	m.enemies = append(m.enemies, e)
	return e
}

// aimAt returns the turn and tilt that point the player's view at e's center.
func aimAt(m *match, e *enemy) (turn, tilt float32) {
	to := e.center().Sub(m.eye())
	return wrapAngle(yawTo(m.eye(), e.center()) - m.player.yaw), float32(asin(to.Y/to.Len())) - m.player.pitch
}

// wallBodies counts the bodies a fresh match starts with: the floor, the walls, the crates and the player.
func wallBodies(m *match) int {
	return len(newMatch(m.arena).world.Bodies())
}

func TestALeadOnACrateStillMovesTheHunterAndExpires(t *testing.T) {
	m := quietMatch()
	// A blind drone never sees the player, so only the lead on top of the crate at (8, 3), a cell the grid cannot reach, drives it.
	blind := *m.arena.enemy("drone")
	blind.SightRange = 0.01
	start := m.cellCenter(gridCell{1, 11})
	e := newEnemy(m, &blind, start)
	m.enemies = append(m.enemies, e)
	crate := m.cellCenter(gridCell{8, 3})
	crate.Y = m.arena.CrateHeight
	e.alert(crate)
	closest := horizontalDist(start, crate)
	for i := 0; e.hasLead && i < 60*60; i++ {
		m.step(controls{}, step)
		closest = min(closest, horizontalDist(e.ctrl.Pos, crate))
	}
	if e.hasLead {
		t.Fatalf("after a minute the drone at %+v still hunts the lead on the crate", e.ctrl.Pos)
	}
	if closest > 2*m.arena.CellSize {
		t.Fatalf("the drone came no closer than %v m to the lead on the crate, want it to walk up to the crate", closest)
	}
	m.step(controls{}, step)
	if e.state != statePatrol {
		t.Fatalf("after giving the lead up the drone is in state %d, want patrol", e.state)
	}
}

// boltAtChest returns an enemy bolt that hits the player's chest in the next step.
func boltAtChest(m *match, damage int) shot {
	return shot{pos: m.chest().Add(physics3d.Vec3{X: 1}), vel: physics3d.Vec3{X: -60}, left: 10, damage: damage, fromEnemy: true}
}

func TestABoltInFlightCannotUndoAWin(t *testing.T) {
	m := newMatch(arena)
	m.phase, m.wave, m.timer = phaseFight, len(m.arena.Waves)-1, 0
	m.player.health = 5
	m.shots = append(m.shots, boltAtChest(m, 20))
	m.step(controls{}, step)
	if m.phase != phaseWon || m.player.health != 5 {
		t.Fatalf("clearing the last wave with a bolt in flight left phase %d and %d health, want a win at 5", m.phase, m.player.health)
	}
}

func TestAPickupCannotUndoALethalHit(t *testing.T) {
	m := quietMatch()
	for _, pk := range m.pickups {
		if pk.kind.Effect == EffectHealth {
			m.player.ctrl.Pos = pk.pos
		}
	}
	m.player.health = 5
	m.shots = append(m.shots, boltAtChest(m, 8))
	m.step(controls{}, step)
	if m.phase != phaseLost || m.player.health != 0 {
		t.Fatalf("a lethal bolt on a health pickup left phase %d and %d health, want a loss at 0", m.phase, m.player.health)
	}
}
