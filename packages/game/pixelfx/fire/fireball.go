// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/fireball.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame-puff, smoke and spark recipe from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Fireball is a fireball flying right: a churning white-hot head with a corona of flame, a long torn tail of flame puffs cooling into smoke, embers shed behind. It is registered as "fire/fireball".
var Fireball = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fireball",
		Desc:     "A fireball flying right: a churning white-hot head with a corona of flame, a long torn tail of flame puffs cooling into smoke, embers shed behind.",
		Tags:     []string{"fire", "fireball", "projectile", "magic", "directional", "loop"},
		Size:     [2]int{136, 56},
		Origin:   [2]int{112, 28},
		Duration: fireballDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "radius", Default: pixelfx.Num(5)},
			{Name: "speed", Default: pixelfx.Num(120)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newFireball,
}

func init() { pixelfx.Register("fire/fireball", Fireball) }

const fireballDuration = 1.2

var (
	// fireballBody is the comet's body: tongues rooted in the head streaming back, the longest behind: length, half-width, y offset.
	fireballBody = [4][3]float64{{30, 4.2, 0}, {23, 4.4, -1.6}, {22, 4.4, 1.6}, {15, 4.6, 0}}
	// fireballCorona is the corona: thin licks peeling back off the head: angle on the head (0 is the nose), length, half-width.
	fireballCorona = [6][3]float64{{-1.3, 6, 1.1}, {-1.8, 8, 1.2}, {-2.3, 7, 1.1}, {1.3, 6, 1.1}, {1.8, 8, 1.2}, {2.3, 7, 1.1}}
)

// fireballHead draws the head: a ball whose outline churns and whose heat swirls in a spiral, white at the heart, swept back.
func fireballHead(p *pixelfx.Painter, H []uint32, t, R0, S, hx, hy float64) {
	ph := t / fireballDuration
	p.Shade(hx-R0*2.4, hy-R0*1.7, hx+R0*1.5, hy+R0*1.7, func(x, y float64, _ *pixelfx.Out) uint32 {
		x -= hx
		y -= hy
		d, th := pixelfx.Hypot(x, y), math.Atan2(y, x)
		back, a := math.Max(0, -math.Cos(th)), th/tau
		bump := 0.55*pnoise(a*9+ph*9, 9, S) + 0.35*pnoise(a*5-ph*10, 5, S+1)
		rim := R0*(1+0.55*back*back) + bump
		if d > rim {
			return 0
		}
		swirl := pnoise(a*5-ph*10+d*0.45, 5, S+2)
		h := 1 - d/rim + 0.26*swirl + 0.1*(1-back)
		// the leading edge: a crisp hot rim where it cuts the air
		if rim-d < 1 && back < 0.3 {
			if h > 0.3 {
				return H[3]
			}
			return H[5]
		}
		switch {
		case h > 0.74:
			return H[0]
		case h > 0.58:
			return H[1]
		case h > 0.42:
			return H[2]
		case h > 0.27:
			return H[4]
		case h > 0.13:
			return H[5]
		}
		return H[6]
	})
}

type (
	fireballPuff struct {
		x, y, v, vy, fl, sl, s, f float64
		sm                        bool
	}
	fireballDrawn struct {
		age, x, y float64
		q         *fireballPuff
	}
	fireballMote  struct{ a, v float64 }
	fireballEmber struct {
		x, y, v, vy, ph float64
		big             bool
	}
)

func newFireball(o *pixelfx.Options) pixelfx.Instance {
	D := float64(fireballDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, S, R0, V := hotFirst(F), o.Seed(), o.Num("radius"), o.Num("speed")
	GLOW, SMOKE := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25), pixelfx.Mix(smoke1, H[6], 0.3)
	SPARKS, EMB := [4]uint32{H[1], H[2], H[4], H[5]}, [4]uint32{H[1], H[2], H[4], H[6]}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }
		// the head bobs a hair as it flies
		hx, hy := N(3, S+50, 0)*0.5, N(2.5, S+51, 0)*0.7

		// the tail: flame puffs thrown off the back that the head leaves behind; they cool, swell, rise and some burn out into smoke. Positions are relative to the head, which outruns them.
		var fire, smoke []fireballDrawn
		looped(t, D, 130, S+100, func(r *pixelfx.Rng, _ int) (fireballPuff, float64) {
			var q fireballPuff
			q.fl = r.Range(0.22, 0.55)
			q.sm = r.Chance(0.55)
			q.x = -r.Range(2, 24)
			q.y = r.Range(-1, 1) * (R0 - 0.1*-q.x)
			q.v = V * r.Range(0.3, 0.6)
			q.vy = r.Range(-16, 16)
			q.sl = r.Range(0.25, 0.45)
			q.s = r.Range(1.3, 2.9)
			q.f = r.Next()*0.5 + 0.5*(-q.x/24)
			life := q.fl
			if q.sm {
				life = q.fl + 0.45
			}
			return q, life
		}, func(q *fireballPuff, age, _ float64, _ int) {
			ta := math.Min(age, q.fl)
			wx, y, wvx, vy := drift(0, q.y, q.v, q.vy, ta, 0, -70, 4.5, 3)
			x := q.x + wx - V*ta
			if age < q.fl {
				fire = append(fire, fireballDrawn{age, x, y, q})
				return
			}
			if !q.sm || age-q.fl > q.sl {
				return
			}
			sa := age - q.fl
			sx, sy, _, _ := drift(x, y, wvx-V, vy*0.6-6, sa, V*2, -30, 2, 1.2)
			smoke = append(smoke, fireballDrawn{sa, sx, sy, q})
		})
		for _, d := range smoke {
			smokePuff(p, d.x, d.y, 1-d.age/d.q.sl, d.q.s*0.9, SMOKE, false, 1)
		}
		slices.SortStableFunc(fire, func(a, b fireballDrawn) int { return cmpDesc(a.age, b.age) })
		for _, d := range fire {
			u := d.age / d.q.fl
			firePuff(p, d.x, d.y, 1-u, d.q.s*(1+1.3*u), d.q.f, H, 0.07, -0.86, -0.5)
		}

		// the body: tongues of flame rooted in the head streaming back, and a corona licking round it
		for i, b := range fireballBody {
			ln, w, dy := b[0], b[1], b[2]
			n := (S*5 + float64(i)) * 17
			L := ln * (0.82 + 0.25*N(4, n, 0)) * (1 + 0.3*math.Max(0, N(2.4, n+3, 0)))
			flameBlade(p, hx+1, hy+dy*0.5, -1, dy*0.02, L, w*R0/5, N(3, n+5, 0)*3+dy, N, n, H, layers, 0.5, 1.1+L*0.05)
		}
		for i, c := range fireballCorona {
			a, ln, w := c[0], c[1], c[2]
			n := (S*5 + float64(i) + 20) * 17
			L := ln * (0.7 + 0.4*(0.5+0.5*N(6, n, 0)))
			ox, oy := math.Cos(a)*R0*0.85, math.Sin(a)*R0*0.85
			dx, dy := math.Cos(a)*0.55-1, math.Sin(a)*0.55
			l := pixelfx.Hypot(dx, dy)
			flameBlade(p, hx+ox, hy+oy, dx/l, dy/l, L, w, -pixelfx.Sign(a)*1.5, N, n, H, layers, 0.5, 1.1)
		}
		fireballHead(p, H, t, R0, S, hx, hy)

		// motes flicked off the head's skin: short hot streaks that die at once
		looped(t, D, 26, S+200, func(r *pixelfx.Rng, _ int) (fireballMote, float64) {
			var q fireballMote
			q.a = r.Range(-2.4, 2.4)
			q.v = r.Range(30, 70)
			return q, r.Range(0.08, 0.18)
		}, func(q *fireballMote, age, life float64, _ int) {
			ca, sa := math.Cos(q.a), math.Sin(q.a)
			d := R0*0.9 + q.v*age
			x, y := hx+ca*d-V*0.4*age, hy+sa*d
			k := 1 - age/life
			L := 1 + 2*k
			c := H[3]
			if k > 0.5 {
				c = H[1]
			}
			p.Line(x-ca*L+V*0.02, y-sa*L, x, y, c, 1)
			p.Glow(x, y, 3, H[2], 0.4*k)
		})
		// embers left behind: streaks that the head outruns, falling as they cool
		looped(t, D, 30, S+300, func(r *pixelfx.Rng, _ int) (fireballEmber, float64) {
			var q fireballEmber
			q.x = -r.Range(2, 16)
			q.y = r.Range(-3, 3)
			q.v = V * r.Range(0.3, 0.8)
			q.vy = r.Range(-20, 20)
			life := r.Range(0.25, 0.55)
			q.big = r.Chance(0.25)
			q.ph = r.Range(0, 6.3)
			return q, life
		}, func(q *fireballEmber, age, life float64, _ int) {
			wx, y, wvx, vy := drift(0, q.y, q.v, q.vy, age, 0, 50, 3, 2)
			x, k := q.x+wx-V*age, 1-age/life
			if q.big {
				ember(p, x, y, k, 2, EMB, age*40+q.ph)
			} else {
				spark(p, x, y, wvx-V, vy, k, SPARKS)
			}
		})

		// light: a hot heart, a warm halo, light running down the tail
		fl := 0.85 + 0.15*N(9, S+7, 0)
		p.GlowLine(hx-6, hy, -50, -2, 12, GLOW, 0.2*fl)
		p.Glow(hx-2, hy, 42, GLOW, 0.42*fl)
		p.Glow(hx, hy, 13, H[1], 0.3*fl)
	}}
}
