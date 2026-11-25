// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/particles.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// Particles without a simulation: each one's position is a closed-form function of its age, so an effect can be drawn at any time t, scrubbed, looped and rendered to a sprite sheet frame by frame. (particles.js also has a small simulated list, which pixelfx's index exports sim.js's Particles in place of, so no effect can reach it and it is not ported.)

// Fly returns where a particle is after t seconds, launched from (x, y) at (vx, vy) px/s, under gravity g (px/s², positive is down) and linear drag (1/s).
func Fly(x, y, vx, vy, t, g, drag float64) (float64, float64) {
	if drag <= 0 {
		return x + vx*t, y + vy*t + 0.5*g*t*t
	}
	f, vt := (1-math.Exp(-drag*t))/drag, g/drag
	return x + vx*f, y + vt*t + (vy-vt)*f
}

// FlyV returns a flying particle's velocity at t, for streaks that point along the motion.
func FlyV(vx, vy, t, g, drag float64) (float64, float64) {
	if drag <= 0 {
		return vx, vy + g*t
	}
	e, vt := math.Exp(-drag*t), g/drag
	return vx * e, vt + (vy-vt)*e
}

// Burst calls fn for n one-shot particles, each with a repeatable generator (the same numbers every frame).
func Burst(n int, seed float64, fn func(r *Rng, i int)) {
	for i := range n {
		fn(RngOf(seed, float64(i)), i)
	}
}

// Fixed returns a particle life that is always life seconds, for Cycle and Stream.
func Fixed(life float64) func(g *Rng) float64 {
	return func(*Rng) float64 { return life }
}

// Cycle runs particles that repeat every period seconds, for loops with no seam. Particle i is born at its own phase in the period and lives life(g) seconds (g is its own generator, already used once for its phase). For every live copy, fn(age, life, r, i) is called; r is a fresh generator for particle i, the same each period.
func Cycle(t, period float64, count int, seed float64, life func(g *Rng) float64, fn func(age, life float64, r *Rng, i int)) {
	for i := range count {
		g := RngOf(seed, float64(i), 911)
		born := g.Next() * period
		L := life(g)
		age := math.Mod(math.Mod(t-born, period)+period, period)
		for ; age < L; age += period {
			fn(age, L, RngOf(seed, float64(i)), i)
		}
	}
}

// Stream runs particles born at a steady rate (per second) between t0 and t1, an emitter that stops: fn(age, life, r, i) for every particle alive at t.
func Stream(t, t0, t1, rate, seed float64, life func(g *Rng) float64, fn func(age, life float64, r *Rng, i int)) {
	n := int(math.Floor((math.Min(t, t1) - t0) * rate))
	for i := 0; i <= n; i++ {
		born, g := t0+float64(i)/rate, RngOf(seed, float64(i), 911)
		L := life(g)
		age := t - born
		if age >= 0 && age < L {
			fn(age, L, RngOf(seed, float64(i)), i)
		}
	}
}
