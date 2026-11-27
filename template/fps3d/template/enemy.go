package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/nsgo/core"
)

// enemyState is what an enemy is doing, as its behavior tree last decided.
type enemyState int

const (
	// statePatrol walks the arena's patrol route.
	statePatrol enemyState = iota
	// stateHunt walks to where the player was last seen or heard.
	stateHunt
	// stateAttack stands and shoots at the player it sees.
	stateAttack
)

// enemy is one enemy: a capsule walked by a character controller, a kinematic body that shots and the player collide with, and a behavior tree that picks patrolling, hunting or attacking each step.
type enemy struct {
	kind      *EnemyKind
	ctrl      *physics3d.CharacterController
	body      *physics3d.Body
	prevFeet  physics3d.Vec3
	yaw       float32
	health    int
	state     enemyState
	brain     ai.Node
	hurtFlash float32

	waypoint int
	cooldown float32

	// lead is where the player was last seen or heard, while hasLead holds.
	lead     physics3d.Vec3
	hasLead  bool
	leadIdle float32

	// path is the smoothed route to pathGoal, followed from pathAt; repath counts down to the next search.
	path     []core.Point
	pathAt   int
	pathGoal ai.Cell
	partial  bool // the path ends short of pathGoal, which the grid cannot reach
	repath   float32
	wish     physics3d.Vec3
}

const (
	repathInterval = 0.5
	waypointReach  = 0.6
	leadGiveUp     = 3.0
	noticeRadius   = 4.0
)

// newEnemy places an enemy of kind at feet, heading for the nearest patrol waypoint.
func newEnemy(m *match, kind *EnemyKind, feet physics3d.Vec3) *enemy {
	e := &enemy{kind: kind, health: kind.Health, prevFeet: feet}
	e.ctrl = physics3d.NewCharacterController(feet, kind.Radius, kind.Height)
	e.ctrl.Mask = layerWall | layerEnemy | layerPlayer
	e.body = m.world.AddBody(physics3d.Body{
		Shape:     physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{Y: kind.Height / 2}, kind.Radius, kind.Height)),
		Pos:       feet,
		Kinematic: true,
		Layer:     layerEnemy,
	})
	e.ctrl.IgnoreBodyID = e.body.ID
	best := float32(math.MaxFloat32)
	for i, w := range m.route {
		if d := horizontalDist(w, feet); d < best {
			best, e.waypoint = d, i
		}
	}
	// Attack the player in sight, else hunt the last lead, else patrol. Guards check their condition on every tick, so a running attack stops the moment the player breaks the line of sight.
	e.brain = ai.Selector(
		ai.Guard(func() bool { return e.sees(m) }, ai.Action(func(time.Duration) ai.Status { return e.attack(m) })),
		ai.Guard(func() bool { return e.hasLead }, ai.Action(func(dt time.Duration) ai.Status { return e.hunt(m, dt) })),
		ai.Action(func(time.Duration) ai.Status { return e.patrol(m) }),
	)
	return e
}

// center returns the middle of e's capsule.
func (e *enemy) center() physics3d.Vec3 {
	return e.ctrl.Pos.Add(physics3d.Vec3{Y: e.kind.Height / 2})
}

// eye returns the point e sees and shoots from.
func (e *enemy) eye() physics3d.Vec3 {
	return e.ctrl.Pos.Add(physics3d.Vec3{Y: e.kind.Height * 0.8})
}

// alert gives e a lead to the player's position at.
func (e *enemy) alert(at physics3d.Vec3) {
	e.lead, e.hasLead, e.leadIdle = at, true, 0
}

// think runs e's behavior tree for one step and walks it toward whatever the tree chose.
func (e *enemy) think(m *match, dt time.Duration) {
	secs := float32(dt.Seconds())
	e.cooldown = max(0, e.cooldown-secs)
	e.hurtFlash = max(0, e.hurtFlash-secs)
	e.repath -= secs
	e.wish = physics3d.Vec3{}
	e.brain.Tick(dt)
	e.ctrl.Move(m.world, e.wish, dt)
	e.body.Pos = e.ctrl.Pos
}

// sees reports whether e sees the player: within sight range, inside its field of view or close by, with a clear line from its eye to the player's chest, which is where its bolts fly, so it never stands shooting at a wall. Seeing the player refreshes e's lead.
func (e *enemy) sees(m *match) bool {
	from, to := e.eye(), m.chest()
	d := from.Dist(to)
	if d > e.kind.SightRange {
		return false
	}
	if d > noticeRadius {
		facing := flatForward(e.yaw)
		dir := to.Sub(from)
		dir.Y = 0
		if l := dir.Len(); l > 0 && facing.Dot(dir.Scale(1/l)) < float32(math.Cos(float64(e.kind.FieldOfView)/2)) {
			return false
		}
	}
	if !m.clearLine(from, to, sightMargin) {
		return false
	}
	e.alert(m.player.ctrl.Pos)
	return true
}

// sightMargin is how far an enemy's line of sight must pass from every wall edge, so an enemy never sees the player along a line that grazes a corner the player cannot see back along.
const sightMargin = 0.15

// attack closes to attack range along the navigation grid, then stands, faces the player and fires bolts at its chest.
func (e *enemy) attack(m *match) ai.Status {
	e.state = stateAttack
	target := m.player.ctrl.Pos
	e.yaw = yawTo(e.ctrl.Pos, target)
	if horizontalDist(e.ctrl.Pos, target) > e.kind.AttackRange {
		e.walkTo(m, target)
		return ai.StatusRunning
	}
	if e.cooldown == 0 {
		e.cooldown = e.kind.FireInterval
		from := e.eye()
		dir := m.chest().Sub(from).Norm()
		m.shots = append(m.shots, shot{pos: from.Add(dir.Scale(e.kind.Radius + 0.2)), vel: dir.Scale(e.kind.BoltSpeed), left: e.kind.SightRange, damage: e.kind.BoltDamage, fromEnemy: true})
	}
	return ai.StatusRunning
}

// hunt walks to the lead and gives it up after waiting there without seeing the player. A lead the grid cannot reach, such as the top of a crate, counts as reached once the enemy stands as close to it as the grid allows, so it expires like any other.
func (e *enemy) hunt(m *match, dt time.Duration) ai.Status {
	e.state = stateHunt
	if horizontalDist(e.ctrl.Pos, e.lead) > m.arena.CellSize && e.walkTo(m, e.lead) {
		return ai.StatusRunning
	}
	e.yaw = wrapAngle(e.yaw + float32(dt.Seconds())*2)
	if e.leadIdle += float32(dt.Seconds()); e.leadIdle >= leadGiveUp {
		e.hasLead = false
	}
	return ai.StatusRunning
}

// patrol walks the route's waypoints in order.
func (e *enemy) patrol(m *match) ai.Status {
	e.state = statePatrol
	target := m.route[e.waypoint]
	if horizontalDist(e.ctrl.Pos, target) < m.arena.CellSize/2 {
		e.waypoint = (e.waypoint + 1) % len(m.route)
		target = m.route[e.waypoint]
	}
	e.walkTo(m, target)
	return ai.StatusRunning
}

// walkTo sets e's wish to follow an A* path over the navigation grid toward target, searching again when the goal cell changes or the path goes stale, and reports whether e has anywhere left to walk. When target's cell is not walkable, such as a crate the player stands on, the path ends at the reachable cell closest to it.
func (e *enemy) walkTo(m *match, target physics3d.Vec3) bool {
	goal := m.cellAt(target)
	if goal != e.pathGoal || e.repath <= 0 || e.pathAt >= len(e.path) {
		e.pathGoal, e.repath, e.pathAt = goal, repathInterval, 0
		e.path = e.path[:0]
		e.partial = false
		if r := m.astar.FindWithOptions(m.nav, m.cellAt(e.ctrl.Pos), goal, ai.PathOptions{AllowPartial: true}); r.Found {
			e.partial = r.Partial
			e.path = ai.SmoothGridPathInto(e.path, m.nav, r.Path, ai.GridPathOptions{CellSize: m.arena.CellSize, Radius: e.kind.Radius})
			// The first point is e's own cell; head for the next one.
			if len(e.path) > 1 {
				e.pathAt = 1
			}
		}
	}
	for e.pathAt < len(e.path) {
		next := e.path[e.pathAt]
		p := physics3d.Vec3{X: float32(next.X), Z: float32(next.Y)}
		last := e.pathAt == len(e.path)-1
		if last && !e.partial {
			p.X, p.Z = target.X, target.Z
		}
		dir := p.Sub(e.ctrl.Pos)
		dir.Y = 0
		if l := dir.Len(); l > waypointReach || (last && l > 0.05) {
			e.wish = dir.Scale(e.kind.Speed / l)
			if e.state != stateAttack {
				e.yaw = yawTo(e.ctrl.Pos, p)
			}
			return true
		}
		e.pathAt++
	}
	return false
}
