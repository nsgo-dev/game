package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// step is one fixed step of the game's loop.
const step = time.Second / stepRate

// waypoint is a place on the bot's route; jump asks for a jump on the way to it, to climb onto a ledge.
type waypoint struct {
	at   math3d.Vec3
	jump bool
}

// winRoute runs from the spawn to the ledge, jumps onto it for the key, drops off, goes through the gate the key opens and on to the shrine.
var winRoute = []waypoint{
	{at: math3d.Vec3{X: 8, Z: 8}},
	{at: math3d.Vec3{X: 8, Z: 3}, jump: true},
	{at: math3d.Vec3{X: 2, Z: -2}},
	{at: math3d.Vec3{X: 0, Z: -3.5}},
	{at: math3d.Vec3{X: 0, Z: -7}},
	{at: math3d.Vec3{X: 0, Z: -13}},
}

// bot plays the game as a player would: it follows a route and fights any enemy that comes close, or, when passive, walks to the first waypoint and stands there.
type bot struct {
	route   []waypoint
	leg     int
	passive bool
}

// next returns the controls for g's next step.
func (b *bot) next(g *game) controls {
	h := g.hero
	if !b.passive {
		if e, to := nearestEnemy(g, 3.5); e != nil {
			c := stick(g, to)
			if to.Len() < h.tune.Reach {
				// In reach: turn to face it without crowding it, and swing once facing it.
				c.moveX, c.moveY = c.moveX*0.2, c.moveY*0.2
				facing := math3d.Vec3{X: -sin(h.yaw), Z: -cos(h.yaw)}
				c.attack = h.anim.state() != stateAttack && facing.Dot(to.Scale(1/to.Len())) > 0.8
			}
			return c
		}
	}
	for b.leg < len(b.route) {
		wp := b.route[b.leg]
		to := flat(wp.at.Sub(h.pos))
		if to.Len() > 0.5 {
			c := stick(g, to)
			c.jump = wp.jump && h.body.Grounded && h.pos.Y < 0.3 && to.Len() < 3.6
			return c
		}
		if b.passive {
			return controls{}
		}
		b.leg++
	}
	return controls{}
}

// nearestEnemy returns the closest standing enemy within radius of the hero and the way to it on the ground.
func nearestEnemy(g *game, radius float32) (*actor, math3d.Vec3) {
	var best *actor
	var bestTo math3d.Vec3
	for _, e := range g.enemies {
		to := flat(e.pos.Sub(g.hero.pos))
		if e.down || to.Len() > radius || (best != nil && to.Len() >= bestTo.Len()) {
			continue
		}
		best, bestTo = e, to
	}
	return best, bestTo
}

// stick is the movement stick that runs the hero along the ground direction to, from where g's camera looks.
func stick(g *game, to math3d.Vec3) controls {
	if to.LenSq() < 1e-6 {
		return controls{}
	}
	d := to.Scale(1 / to.Len())
	forward := math3d.Vec3{X: -sin(g.camYaw), Z: -cos(g.camYaw)}
	right := math3d.Vec3{X: cos(g.camYaw), Z: -sin(g.camYaw)}
	return controls{moveX: d.Dot(right), moveY: -d.Dot(forward)}
}

func newTestGame(t testing.TB) *game {
	t.Helper()
	c, err := loadContent()
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGame(c.actors, c.level, c.reg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// playUntil steps g under b until the run ends or limit passes, and returns how long it took.
func playUntil(t *testing.T, g *game, b *bot, limit time.Duration) time.Duration {
	t.Helper()
	for g.elapsed < limit && g.phase == phasePlaying {
		g.step(b.next(g), step)
	}
	return g.elapsed
}

func TestBotWinsTheRun(t *testing.T) {
	g := newTestGame(t)
	took := playUntil(t, g, &bot{route: winRoute}, 2*time.Minute)
	if g.phase != phaseWon {
		t.Fatalf("after %v the run is %v at %v with %d health, key %v, %d enemies left", took, g.phase, g.hero.pos, g.hero.health, g.hasKey, g.enemiesLeft())
	}
	if !g.hasKey || !g.doorOpen {
		t.Fatalf("won without the key (%v) or the door open (%v)", g.hasKey, g.doorOpen)
	}
	if g.hero.hitsLanded == 0 {
		t.Fatal("the bot won without landing a blow; the route no longer meets the enemies")
	}
	t.Logf("won in %v with %d health, %d enemies left", took, g.hero.health, g.enemiesLeft())
}

func TestStandingStillAmongEnemiesLoses(t *testing.T) {
	g := newTestGame(t)
	took := playUntil(t, g, &bot{route: []waypoint{{at: math3d.Vec3{X: 4, Z: 2}}}, passive: true}, time.Minute)
	if g.phase != phaseLost {
		t.Fatalf("after %v the run is %v with %d health", took, g.phase, g.hero.health)
	}
	// The hero falls over once the step that felled it is past.
	for range 10 {
		g.step(controls{}, step)
	}
	if g.hero.health != 0 || g.hero.anim.state() != stateFall {
		t.Fatalf("lost with %d health in state %q", g.hero.health, g.hero.anim.state())
	}
}

func TestTheDoorBlocksTheGateUntilTheKeyOpensIt(t *testing.T) {
	g := newTestGame(t)
	// Teleport the hero in front of the gate and walk north into the door.
	g.hero.body.Pos = p3(math3d.Vec3{Z: -3})
	g.hero.pos = m3(g.hero.body.Pos)
	for range 90 {
		g.step(controls{moveY: -1}, step)
	}
	if g.hero.pos.Z < -4.5 {
		t.Fatalf("walked through the locked door to %v", g.hero.pos)
	}
	// Picking up the key fires the scene's collected event, whose connection unlocks the door.
	g.hero.body.Pos = p3(g.level.key.Sub(math3d.Vec3{Y: 0.6}))
	g.step(controls{}, step)
	if !g.hasKey || !g.doorOpen {
		t.Fatalf("key %v, door open %v after reaching the key", g.hasKey, g.doorOpen)
	}
	g.hero.body.Pos = p3(math3d.Vec3{Z: -3})
	g.hero.body.Vel = p3(math3d.Vec3{})
	for range 90 {
		g.step(controls{moveY: -1}, step)
	}
	if g.hero.pos.Z > -6 {
		t.Fatalf("the open gate still stops the hero at %v", g.hero.pos)
	}
}

func TestAttackLandsOnItsHitEventAndStaggers(t *testing.T) {
	g := newTestGame(t)
	e := g.enemies[0]
	// Stand the hero just south of the enemy, facing it.
	g.hero.body.Pos = p3(e.pos.Add(math3d.Vec3{Z: 1.2}))
	g.hero.pos, g.hero.yaw = m3(g.hero.body.Pos), 0
	g.step(controls{attack: true}, step)
	if g.hero.anim.state() != stateAttack {
		t.Fatalf("state %q after attacking, want %q", g.hero.anim.state(), stateAttack)
	}
	health := e.health
	for range 30 {
		g.step(controls{}, step)
	}
	if g.hero.hits != 1 || e.health != health-g.actors.Hero.Damage {
		t.Fatalf("hits %d, enemy health %d (was %d)", g.hero.hits, e.health, health)
	}
}

func TestRigPosesEveryJointInEveryState(t *testing.T) {
	g := newTestGame(t)
	a := g.hero.anim
	for _, state := range []string{stateMove, stateJump, stateAttack, stateHurt, stateFall} {
		a.machine.SetState(state, 0)
		a.step(step, 0)
		if len(a.pose.Joints) != len(g.rig.joints) {
			t.Fatalf("state %q poses %d joints, want %d", state, len(a.pose.Joints), len(g.rig.joints))
		}
		parts := g.rig.parts(nil, math3d.Mat4Identity(), a.pose)
		for i, m := range parts {
			p := m.TransformPoint(math3d.Vec3{})
			if !finite(p.X) || !finite(p.Y) || !finite(p.Z) {
				t.Fatalf("state %q joint %q is at %v", state, g.rig.joints[i].Name, p)
			}
		}
	}
}

func BenchmarkStep(b *testing.B) {
	g := newTestGame(b)
	bt := &bot{route: winRoute}
	for b.Loop() {
		if g.phase != phasePlaying {
			_ = g.reset()
			bt.leg = 0
		}
		g.step(bt.next(g), step)
	}
}
