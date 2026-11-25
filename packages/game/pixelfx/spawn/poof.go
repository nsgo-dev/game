// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/poof.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion) for the soft, top-lit smoke puffs (a body, a denser core, a lit cap), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Poof is a cartoon death poof: a white pop, a ring of soft round smoke balls bursting out between speed lines, little stars spinning off, and the cloud lifting and thinning away. It is registered as "spawn/poof".
var Poof = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Poof",
		Desc:     "A cartoon death poof: a white pop, a ring of soft round smoke balls bursting out between speed lines, little stars spinning off, and the cloud lifting and thinning away.",
		Tags:     []string{"spawn", "death", "despawn", "smoke", "poof", "cartoon", "one-shot"},
		Size:     [2]int{112, 92},
		Origin:   [2]int{56, 80},
		Duration: 1.6,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a2440", "#5e5878", "#9a94b4", "#cdc8e0", "#f2f0fa", "#ffffff")},
			{Name: "stars", Default: pixelfx.Palette("#ffd84a", "#fff3a6", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(21)},
		},
	},
	New: newPoof,
}

func init() { pixelfx.Register("spawn/poof", Poof) }

const poofCY = -17.0

type poofBall struct {
	x, y, vx, vy, r0, r1, life, max, delay float64
}

type poofLine struct {
	a, r1, d float64
}

func newPoof(o *pixelfx.Options) pixelfx.Instance {
	C, S, R := pixelfx.Fit(o.Palette(), 6), pixelfx.Fit(o.Ramp("stars"), 3), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, S[1], S[0], C[2]}})
	lights := &pixelfx.Lights{}
	var balls []poofBall
	var lines []poofLine

	// a ring of fat balls round a fat core, thrown out fast and stopped by drag, each swelling as it slows
	const ring = 9
	for i := range ring {
		a := (float64(i)/ring)*tau + R.Range(-0.2, 0.2)
		sp := R.Range(95, 140)
		r := R.Range(5, 7)
		life := R.Range(0.7, 1.05)
		delay := R.Range(0, 0.03)
		balls = append(balls, poofBall{
			x: math.Cos(a) * 3, y: poofCY + math.Sin(a)*3,
			vx: math.Cos(a) * sp, vy: math.Sin(a)*sp*0.8 - 12,
			r0: r * 0.5, r1: r, life: life, max: life, delay: delay,
		})
	}
	for range 5 {
		a, sp := R.Range(0, tau), R.Range(20, 60)
		r1, life, delay := R.Range(6.5, 8.5), R.Range(0.85, 1.2), R.Range(0.02, 0.06)
		balls = append(balls, poofBall{
			x: math.Cos(a) * 2, y: poofCY + math.Sin(a)*2,
			vx: math.Cos(a) * sp, vy: math.Sin(a)*sp - 16,
			r0: 4, r1: r1, life: life, max: life, delay: delay,
		})
	}
	// a few small puffs flung further out, gone quickly, so the cloud's edge is ragged
	for range 7 {
		a, sp, r := R.Range(0, tau), R.Range(170, 240), R.Range(2.2, 3.6)
		life, delay := R.Range(0.32, 0.5), R.Range(0.01, 0.04)
		balls = append(balls, poofBall{
			x: math.Cos(a) * 4, y: poofCY + math.Sin(a)*4,
			vx: math.Cos(a) * sp, vy: math.Sin(a)*sp*0.75 - 10,
			r0: r * 0.6, r1: r, life: life, max: life, delay: delay,
		})
	}
	// speed lines between the balls, flicked out and gone in a few frames
	for i := range 12 {
		a := (float64(i)/12)*tau + R.Range(-0.12, 0.12) + 0.13
		sa := math.Sin(a)
		// the ones that point down stop at the floor
		lim := 99.0
		if sa > 0.1 {
			lim = (-2 - poofCY) / sa
		}
		r1 := math.Min(R.Range(24, 34), lim)
		d := R.Range(0, 0.04)
		lines = append(lines, poofLine{a: a, r1: r1, d: d})
	}
	// little stars spun off the pop
	starCols1 := []uint32{S[2], S[1], S[1], S[0]}
	starCols2 := []uint32{pixelfx.White, S[1]}
	for range 7 {
		a, sp := R.Range(-pi, 0.3), R.Range(70, 125)
		life, col, size, delay := R.Range(0.55, 0.95), pixelfx.Pick(R, starCols1), R.Range(1.3, 2.4), R.Range(0.02, 0.1)
		ps.Spawn(pixelfx.StarKind, math.Cos(a)*6, poofCY+math.Sin(a)*6, math.Cos(a)*sp, math.Sin(a)*sp-30, life, col, size, delay, 0)
	}
	for range 10 {
		a, sp := R.Range(0, tau), R.Range(40, 110)
		life, col, delay := R.Range(0.3, 0.6), pixelfx.Pick(R, starCols2), R.Range(0, 0.08)
		ps.Spawn(pixelfx.StarKind, math.Cos(a)*5, poofCY+math.Sin(a)*5, math.Cos(a)*sp, math.Sin(a)*sp-20, life, col, 0.6, delay, 0)
	}
	// the feet go too: a skid of dust along the floor
	for range 6 {
		s := R.Sign()
		x, vx, vy, life, size, delay := s*R.Range(1, 6), s*R.Range(25, 70), -R.Range(2, 10), R.Range(0.4, 0.7), R.Range(1.6, 3), R.Range(0.02, 0.08)
		ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, C[2], size, delay, 0)
	}
	lights.Add(0, poofCY, 46, C[4], 0.8, 0.4)
	lights.Add(0, poofCY, 22, pixelfx.White, 0.8, 0.1)
	t := 0.0

	// a ball swells out, breathes, then shrinks to nothing (a cartoon puff goes out ball by ball, it doesn't fade)
	radius := func(b *poofBall) float64 {
		age := b.max - b.life
		u := age / b.max
		grow := b.r0 + (b.r1-b.r0)*pixelfx.OutBack(pixelfx.Clamp(age/0.22), 2)
		breathe := 1 + 0.06*math.Sin(age*14+b.r1)
		shrink := 1.0
		if u >= 0.62 {
			shrink = 1 - pixelfx.InBack(pixelfx.Clamp((u-0.62)/0.38), 1.4)
		}
		return grow * breathe * shrink
	}
	drawBall := func(p *pixelfx.Painter, b *poofBall, layer int) {
		age, r := b.max-b.life, radius(b)
		a, lit := math.Min(1, age*20), age < 0.12
		if r < 0.6 {
			return
		}
		// the whole cloud is lit from above: the balls underneath are a step darker
		hy := (b.y - poofCY + 6) / 14
		lo, hi := hy > 0.4, hy < -0.3
		switch layer {
		case 0: // shade
			col := C[2]
			if lit {
				col = C[3]
			} else if lo {
				col = C[1]
			}
			p.Disc(b.x+r*0.16, b.y+r*0.2, r, col, 0.8*a)
		case 1: // body
			col := C[3]
			if lit {
				col = C[4]
			} else if lo {
				col = C[2]
			}
			p.Disc(b.x-r*0.06, b.y-r*0.08, r*0.86, col, 0.88*a)
		case 2: // cap
			if !lo || lit {
				col := C[4]
				if lit || hi {
					col = pixelfx.White
				}
				p.Disc(b.x-r*0.28, b.y-r*0.34, r*0.42, col, 0.85*a)
				if age < 0.25 {
					p.Disc(b.x-r*0.3, b.y-r*0.38, r*0.2, pixelfx.White, 0.9*a*(1-age/0.25))
				}
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := len(balls) - 1; i >= 0; i-- {
				b := &balls[i]
				if b.delay > 0 {
					b.delay -= dt
					continue
				}
				b.life -= dt
				if b.life <= 0 {
					// each ball goes with a pip of light
					if b.r1 > 4 {
						ps.Spawn(pixelfx.StarKind, b.x, b.y, 0, -8, 0.22, pixelfx.White, 0.6, 0, 0)
					}
					balls = append(balls[:i], balls[i+1:]...)
					continue
				}
				// fast burst, hard drag, then a slow lift
				b.vx -= b.vx * 7 * dt
				b.vy -= b.vy*7*dt + 30*dt
				b.x += b.vx * dt
				b.y = math.Min(b.y+b.vy*dt, -radius(b)*0.7)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a soft shadow where the body stood, and the light of the pop on the floor
			s := 1 - pixelfx.Smooth(0.1, 0.9, t)
			if s > 0 {
				p.Glow(0, -1, 26, C[4], 0.25*s)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the cloud: shade, body, then caps, so neighbouring balls merge into one lumpy cloud
			for layer := range 3 {
				for i := range balls {
					if balls[i].delay <= 0 {
						drawBall(p, &balls[i], layer)
					}
				}
			}
			// the pop: a white ball and an 8-ray flare for a couple of frames, then a ring
			if t < 0.07 {
				k := t / 0.07
				p.Circle(0, poofCY, 9+3*k, C[4], 1)
				p.Circle(0, poofCY, 7+3*k, pixelfx.White, 1)
				pixelfx.Flare(p, 0, poofCY, 20*(1-k*0.5), 1-k, C)
			}
			rk := (t - 0.03) / 0.2
			if rk > 0 && rk < 1 {
				col := C[4]
				if rk < 0.3 {
					col = pixelfx.White
				}
				p.Ring(0, poofCY, 8+18*pixelfx.Ease.OutCubic(rk), math.Max(1, 2.5*(1-rk)), col, 1.1-rk)
			}
			for _, l := range lines {
				u := (t - l.d) / 0.2
				if u <= 0 || u >= 1 {
					continue
				}
				head := 8 + (l.r1-8)*pixelfx.Ease.OutExpo(pixelfx.Clamp(u/0.6))
				tail := 8 + (l.r1-8)*pixelfx.Ease.InCubic(u)
				ca, sa := math.Cos(l.a), math.Sin(l.a)
				p.LineFn(ca*tail, poofCY+sa*tail, ca*head, poofCY+sa*head, func(v float64) (uint32, float64) {
					if v > 0.6 {
						return pixelfx.White, 1
					}
					return C[4], 1
				})
				p.GlowLine(ca*tail, poofCY+sa*tail, ca*head, poofCY+sa*head, 3, C[4], 0.25)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
