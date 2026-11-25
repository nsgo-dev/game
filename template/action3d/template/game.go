package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// phase is where a run stands.
type phase int

const (
	// phasePlaying is a run in progress.
	phasePlaying phase = iota
	// phaseWon is a run that reached the goal.
	phaseWon
	// phaseLost is a run whose hero fell.
	phaseLost
)

func (p phase) String() string {
	switch p {
	case phaseWon:
		return "won"
	case phaseLost:
		return "lost"
	default:
		return "playing"
	}
}

// stepRate is how many fixed steps the game takes a second.
const stepRate = 60

// The camera orbits the hero at a fixed height and distance; the player turns it around the hero.
const (
	cameraPitch    = 0.42
	cameraDistance = 7.5
	cameraLift     = 1.3
	// cameraTurnRate is how fast a held turn control orbits the camera, in radians per second.
	cameraTurnRate = 2.6
	// pickupRadius is how close the hero's center must come to the key or the goal, in meters.
	pickupRadius = 1.1
	// doorOpenTime is how long the door takes to sink into the floor once unlocked.
	doorOpenTime = 900 * time.Millisecond
	// fallLimit is the height below which an actor that left the level counts as fallen.
	fallLimit = -10
)

// controls is what the player asks of the hero on one step, read from whichever device they use.
type controls struct {
	// moveX and moveY are the movement stick, each from -1 to 1: x to the right and y toward the camera, so pushing forward is negative y, as a game controller's stick reads.
	moveX, moveY float32
	// turn orbits the camera, -1 to the left and 1 to the right.
	turn float32
	// jump and attack are pressed on this step.
	jump, attack bool
}

// actor is the hero or an enemy: a capsule moving through the physics world, its fighting state and its animation.
type actor struct {
	tune  *Fighter
	paint map[string]core.Color
	body  *physics3d.CharacterController
	home  math3d.Vec3
	anim  *animator

	yaw             float32
	prevPos, pos    math3d.Vec3
	prevYaw         float32
	health          int
	grace, cooldown float32
	push            math3d.Vec3
	speed           float32
	down            bool
	// hits counts the actor's swings that reached their hit event, and hitsLanded those that struck someone.
	hits, hitsLanded int
}

// game is a run of the level: the physics world, the hero and the enemies, the key, the door and the camera. It knows nothing of devices or the GPU, so tests drive it with controls and read its state directly.
type game struct {
	actors *Actors
	rig    *rig
	level  *level
	reg    *sceneformat.ComponentRegistry

	world    *physics3d.World
	signals  *sceneformat.Signals
	doorBody int

	hero    *actor
	enemies []*actor

	phase      phase
	hasKey     bool
	doorOpen   bool
	doorSink   time.Duration
	camYaw     float32
	prevCamYaw float32
	elapsed    time.Duration
	// partsBuf is scratch space for posing a rig when drawing.
	partsBuf []math3d.Mat4

	// onChange runs after a step that changed what the HUD shows: the hero's health, the key, the enemies left or the phase.
	onChange func()
}

// newGame starts a run of a built level.
func newGame(a *Actors, l *level, reg *sceneformat.ComponentRegistry) (*game, error) {
	g := &game{actors: a, rig: newRig(a), level: l, reg: reg}
	if err := g.reset(); err != nil {
		return nil, err
	}
	return g, nil
}

// reset puts everything back where the level starts it.
func (g *game) reset() error {
	g.world = physics3d.NewWorld()
	g.world.Gravity = physics3d.Vec3{Y: -g.actors.Gravity}
	for _, b := range g.level.blocks {
		g.world.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: p3(b.half)}), p3(b.center)))
	}
	door := g.world.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: p3(g.level.door.half)}), p3(g.level.door.center)))
	g.doorBody = door.ID
	// Connect binds the scene's connections to this game, which the door's unlock handler receives as its context.
	sig, err := sceneformat.Connect(g.level.doc, g.reg, g)
	if err != nil {
		return fmt.Errorf("level connections: %w", err)
	}
	g.signals = sig

	g.hero = g.newActor(&g.actors.Hero, g.level.spawn, g.level.spawnYaw)
	g.enemies = g.enemies[:0]
	for _, at := range g.level.enemies {
		g.enemies = append(g.enemies, g.newActor(&g.actors.Enemy, at, 0))
	}
	g.phase, g.hasKey, g.doorOpen, g.doorSink = phasePlaying, false, false, 0
	// The camera starts behind the hero, looking the way it faces.
	g.camYaw, g.prevCamYaw = g.level.spawnYaw, g.level.spawnYaw
	g.elapsed = 0
	return nil
}

func (g *game) newActor(t *Fighter, at math3d.Vec3, yaw float32) *actor {
	return &actor{
		tune:    t,
		paint:   palette(t.Paint),
		body:    physics3d.NewCharacterController(p3(at), t.Radius, t.Height),
		home:    at,
		anim:    g.rig.newAnimator(t.Speed, t.AttackSpeed),
		yaw:     yaw,
		prevYaw: yaw,
		pos:     at,
		prevPos: at,
		health:  t.Health,
	}
}

// step advances the run by one fixed step of dt under c.
func (g *game) step(c controls, dt time.Duration) {
	g.elapsed += dt
	secs := float32(dt.Seconds())
	before := g.hud()

	g.prevCamYaw = g.camYaw
	if g.phase == phasePlaying {
		g.camYaw = wrapAngle(g.camYaw - c.turn*cameraTurnRate*secs)
	}
	if g.doorOpen && g.doorSink < doorOpenTime {
		g.doorSink = min(g.doorSink+dt, doorOpenTime)
	}

	g.stepHero(c, dt)
	for _, e := range g.enemies {
		g.stepEnemy(e, dt)
	}
	g.separate()

	if g.phase == phasePlaying {
		h := g.hero
		if !g.hasKey && h.center().Sub(g.level.key).Len() < pickupRadius {
			g.hasKey = true
			// The scene's connections decide what picking up the key does; keep.scene.yaml unlocks the door. buildLevel and Connect have checked the key's object and event, so Emit cannot fail.
			_ = g.signals.Emit(g.level.keyID, eventCollected, nil)
		}
		if h.down {
			g.phase = phaseLost
		} else if flat(h.pos.Sub(g.level.goal)).Len() < pickupRadius && h.pos.Y > g.level.goal.Y-0.5 {
			g.phase = phaseWon
		}
	}
	if g.onChange != nil && g.hud() != before {
		g.onChange()
	}
}

// orbit turns the camera around the hero by delta radians, as dragging the view does.
func (g *game) orbit(delta float32) {
	if g.phase == phasePlaying {
		g.camYaw = wrapAngle(g.camYaw + delta)
	}
}

// unlock opens the door with ID id, the handler of the door component's unlock.
func (g *game) unlock(id string) {
	if g.doorOpen || id != g.level.doorID {
		return
	}
	g.doorOpen = true
	g.world.RemoveBody(g.doorBody)
	_ = g.signals.Emit(id, eventOpened, nil)
}

// stepHero moves the hero relative to the camera, jumps, swings and lands its blows.
func (g *game) stepHero(c controls, dt time.Duration) {
	h := g.hero
	var wish math3d.Vec3
	jump, attack := false, false
	if g.phase == phasePlaying && !h.down {
		// The stick's forward is away from the camera.
		forward := math3d.Vec3{X: -sin(g.camYaw), Z: -cos(g.camYaw)}
		right := math3d.Vec3{X: cos(g.camYaw), Z: -sin(g.camYaw)}
		wish = right.Scale(c.moveX).Add(forward.Scale(-c.moveY))
		if l := wish.Len(); l > 1 {
			wish = wish.Scale(1 / l)
		}
		jump, attack = c.jump, c.attack
	}
	if attack && h.anim.state() != stateAttack && h.anim.state() != stateHurt {
		h.anim.play(stateAttack)
	}
	if h.anim.state() == stateAttack {
		// Swinging roots the hero to the spot, mostly.
		wish = wish.Scale(0.25)
	}
	h.move(g, wish, jump, dt)
	for _, ev := range h.events(dt) {
		if ev.Event.Name != hitEvent {
			continue
		}
		h.hits++
		for _, e := range g.enemies {
			if h.strikes(e) {
				h.hitsLanded++
				e.hurt(h)
			}
		}
	}
}

// stepEnemy runs one enemy: it chases the hero inside its sight and leash, swings when in reach, and walks home otherwise.
func (g *game) stepEnemy(e *actor, dt time.Duration) {
	secs := float32(dt.Seconds())
	e.cooldown = max(0, e.cooldown-secs)
	h := g.hero
	var wish math3d.Vec3
	if g.phase == phasePlaying && !e.down {
		toHero := flat(h.pos.Sub(e.pos))
		dist := toHero.Len()
		hunting := !h.down && dist < e.tune.Sight && flat(h.pos.Sub(e.home)).Len() < e.tune.Leash
		busy := e.anim.state() == stateAttack || e.anim.state() == stateHurt
		switch {
		case hunting && dist <= e.tune.Reach*0.8+h.tune.Radius:
			if !busy && e.cooldown == 0 {
				e.yaw = yawOf(toHero)
				e.anim.play(stateAttack)
				e.cooldown = float32(g.rig.clips[clipAttack].Duration.Seconds())/e.tune.AttackSpeed + e.tune.Cooldown
			}
			if !busy {
				// Too close to swing yet: turn to face the hero without walking into it.
				e.yaw = turnToward(e.yaw, yawOf(toHero), math3d.DegToRad(e.tune.TurnSpeed)*secs)
			}
		case hunting && !busy:
			wish = toHero.Scale(1 / dist)
		case !hunting && !busy:
			if home := flat(e.home.Sub(e.pos)); home.Len() > 0.4 {
				wish = home.Scale(1 / home.Len())
			}
		}
	}
	e.move(g, wish, false, dt)
	for _, ev := range e.events(dt) {
		if ev.Event.Name == hitEvent {
			e.hits++
			if e.strikes(h) && g.phase == phasePlaying {
				e.hitsLanded++
				h.hurt(e)
			}
		}
	}
}

// move turns a toward wish, runs it there at its speed plus any knockback, jumps it when asked and it stands on the ground, and lets the physics world resolve the rest.
func (a *actor) move(g *game, wish math3d.Vec3, jump bool, dt time.Duration) {
	secs := float32(dt.Seconds())
	a.prevPos, a.prevYaw = a.pos, a.yaw
	a.grace = max(0, a.grace-secs)
	if wish.LenSq() > 1e-4 && !a.down {
		a.yaw = turnToward(a.yaw, yawOf(wish), math3d.DegToRad(a.tune.TurnSpeed)*secs)
	}
	vel := wish.Scale(a.tune.Speed).Add(a.push)
	a.push = a.push.Scale(float32(math.Exp(-8 * float64(secs))))
	var vy float32
	if jump && a.body.Grounded && a.tune.JumpSpeed > 0 {
		vy = a.tune.JumpSpeed
	}
	a.body.Move(g.world, physics3d.Vec3{X: vel.X, Y: vy, Z: vel.Z}, dt)
	a.pos = m3(a.body.Pos)
	a.speed = flat(a.pos.Sub(a.prevPos)).Len() / secs
	if a.pos.Y < fallLimit && !a.down {
		a.health = 0
		a.down = true
	}
	a.anim.grounded = a.body.Grounded
	a.anim.falling = a.down
}

// events advances a's animation and returns the clip events it passed.
func (a *actor) events(dt time.Duration) []anim3d.FiredEvent {
	return a.anim.step(dt, a.speed)
}

// strikes reports whether a blow from a lands on b: b stands within a's reach, inside the cone a faces, near a's height.
func (a *actor) strikes(b *actor) bool {
	if b.down || a.down {
		return false
	}
	to := flat(b.pos.Sub(a.pos))
	dist := to.Len()
	if dist > a.tune.Reach+b.tune.Radius || math.Abs(float64(b.pos.Y-a.pos.Y)) > 1.2 {
		return false
	}
	if dist < 1e-3 {
		return true
	}
	facing := math3d.Vec3{X: -sin(a.yaw), Z: -cos(a.yaw)}
	return facing.Dot(to.Scale(1/dist)) >= cos(math3d.DegToRad(a.tune.Arc/2))
}

// hurt takes a's damage from b's health unless b has just been hit, knocks b away and flinches it, or fells it.
func (b *actor) hurt(a *actor) {
	if b.down || b.grace > 0 {
		return
	}
	b.health = max(0, b.health-a.tune.Damage)
	b.grace = b.tune.Grace
	if away := flat(b.pos.Sub(a.pos)); away.Len() > 1e-3 {
		b.push = away.Scale(b.tune.Knockback / away.Len())
	}
	if b.health == 0 {
		b.down = true
		b.anim.falling = true
		return
	}
	b.anim.play(stateHurt)
}

// center is the middle of a's capsule.
func (a *actor) center() math3d.Vec3 { return a.pos.Add(math3d.Vec3{Y: a.tune.Height / 2}) }

// separate pushes standing actors' capsules apart, since character controllers do not collide with each other.
func (g *game) separate() {
	all := append([]*actor{g.hero}, g.enemies...)
	for i, a := range all {
		for _, b := range all[i+1:] {
			if a.down || b.down {
				continue
			}
			d := flat(b.pos.Sub(a.pos))
			gap := a.tune.Radius + b.tune.Radius - d.Len()
			if gap <= 0 || d.Len() < 1e-4 {
				continue
			}
			n := d.Scale(1 / d.Len())
			a.shove(g, n.Scale(-gap/2))
			b.shove(g, n.Scale(gap/2))
		}
	}
}

// shove moves a along the ground by off unless that would put it inside a wall. The capsule is tested a little above the floor so standing on it does not count.
func (a *actor) shove(g *game, off math3d.Vec3) {
	moved := a.body.Shape().Translated(physics3d.Vec3{X: off.X, Y: 0.06, Z: off.Z})
	if len(g.world.QueryOverlap(moved)) > 0 {
		return
	}
	a.body.Pos = a.body.Pos.Add(p3(off))
	a.pos = m3(a.body.Pos)
}

// enemiesLeft counts the enemies still standing.
func (g *game) enemiesLeft() int {
	n := 0
	for _, e := range g.enemies {
		if !e.down {
			n++
		}
	}
	return n
}

// hudState is what the HUD shows, compared after each step to know when to redraw it.
type hudState struct {
	health, enemies int
	key             bool
	phase           phase
}

func (g *game) hud() hudState {
	return hudState{health: g.hero.health, enemies: g.enemiesLeft(), key: g.hasKey, phase: g.phase}
}

// objective says what to do next.
func (g *game) objective() string {
	switch {
	case g.phase == phaseWon:
		return "The keep is yours"
	case g.phase == phaseLost:
		return "You fell"
	case !g.hasKey:
		return "Find the key"
	default:
		return "Reach the shrine beyond the gate"
	}
}

func p3(v math3d.Vec3) physics3d.Vec3 { return physics3d.Vec3{X: v.X, Y: v.Y, Z: v.Z} }
func m3(v physics3d.Vec3) math3d.Vec3 { return math3d.Vec3{X: v.X, Y: v.Y, Z: v.Z} }

// flat drops v's height, leaving its direction on the ground.
func flat(v math3d.Vec3) math3d.Vec3 { return math3d.Vec3{X: v.X, Z: v.Z} }

// yawOf is the heading that faces along v on the ground: 0 faces -Z, as the rig does.
func yawOf(v math3d.Vec3) float32 { return float32(math.Atan2(float64(-v.X), float64(-v.Z))) }

// turnToward turns from toward to by at most step radians, the short way round.
func turnToward(from, to, step float32) float32 {
	d := wrapAngle(to - from)
	if d > step {
		d = step
	} else if d < -step {
		d = -step
	}
	return wrapAngle(from + d)
}

// wrapAngle wraps a into [-π, π).
func wrapAngle(a float32) float32 {
	return float32(math.Remainder(float64(a), 2*math.Pi))
}

func sin(a float32) float32 { return float32(math.Sin(float64(a))) }
func cos(a float32) float32 { return float32(math.Cos(float64(a))) }
