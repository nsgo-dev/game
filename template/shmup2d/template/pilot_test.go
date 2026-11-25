package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/bullets"
)

// step is the fixed step the tests run the world at, the game loop's.
const step = time.Second / 60

// pilot is a player for the tests: it reads the world as a person reads the screen and decides one step's controls, flying under its target while weaving away from the bullets about to reach the ship, collecting items and bombing its way out of a hit it cannot dodge. It plays through the input devices in the game's tests, so it exercises the same path a player does.
type pilot struct{}

// lookAhead is how far ahead the pilot judges the bullets, and dangerRange how near one must come to be dodged.
const (
	lookAhead   = 400 * time.Millisecond
	dangerRange = 30
)

// decide returns the controls the pilot plays w with this step.
func (pilot) decide(w *world) controls {
	s := &w.ship
	in := controls{Fire: true}
	if !s.alive || w.outcome != running {
		return in
	}
	// Go after an item first, then under the boss, then under the lowest enemy on the screen.
	tx, ty := w.width/2, w.height-70
	if len(w.items) > 0 {
		tx, ty = w.items[0].x, max(w.items[0].y, w.height*0.55)
	} else if b := w.boss; b != nil && !b.dead {
		tx = b.x
	} else {
		lowest := float32(-1)
		for _, e := range w.enemies {
			if !e.dead && w.onScreen(e.x, e.y) && e.y > lowest && e.y < s.y-40 {
				lowest, tx = e.y, e.x
			}
		}
	}
	ax, ay := (tx-s.x)/40, (ty-s.y)/40

	// Push away from every bullet that will pass near, harder the nearer, and from enemies about to ram.
	var rx, ry float32
	threat := float32(math.MaxFloat32)
	push := func(px, py, radius float32) {
		dx, dy := s.x-px, s.y-py
		d := float32(math.Hypot(float64(dx), float64(dy))) - radius
		if d > dangerRange {
			return
		}
		d = max(d, 0.5)
		threat = min(threat, d)
		rx += dx / (d * d) * 60
		ry += dy / (d * d) * 60
	}
	for _, b := range w.enemyShots.Bullets() {
		for t := time.Duration(0); t <= lookAhead; t += 100 * time.Millisecond {
			px, py := predict(b, t)
			push(px, py, b.Radius)
		}
	}
	for _, e := range w.enemies {
		push(e.x, e.y+e.def.Speed*0.2, e.def.Radius+4)
	}
	// Walls push back, so dodging never corners the ship.
	rx += 1/max(s.x-6, 1)*30 - 1/max(w.width-6-s.x, 1)*30
	ry += 1/max(s.y-w.height*0.45, 1)*30 - 1/max(w.height-6-s.y, 1)*30

	in.MoveX = clampf(ax+rx, -1, 1)
	in.MoveY = clampf(ay+ry, -1, 1)
	in.Focus = threat < 10
	in.Bomb = threat < w.stage.Ship.Radius+3 && s.invulnerable == 0 && s.bombs > 0
	return in
}

// predict returns where b will be after t, flying straight.
func predict(b *bullets.Bullet, t time.Duration) (float32, float32) {
	v := b.Velocity()
	secs := float32(t.Seconds())
	return b.X + v.X*secs, b.Y + v.Y*secs
}
