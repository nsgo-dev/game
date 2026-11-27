// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/fire-tornado.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame-puff, smoke and ember recipe from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FireTornado is a spinning funnel of fire: spiral bands turning round it, licks flung off its sides, a ring of flame whirling at its foot, embers orbiting it in and out of view. It is registered as "fire/fire-tornado".
var FireTornado = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fire tornado",
		Desc:     "A spinning funnel of fire: spiral bands turning round it, licks flung off its sides, a ring of flame whirling at its foot, embers orbiting it in and out of view.",
		Tags:     []string{"fire", "tornado", "vortex", "spell", "aoe", "loop"},
		Size:     [2]int{120, 152},
		Origin:   [2]int{60, 136},
		Duration: tornadoDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "height", Default: pixelfx.Num(80)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newFireTornado,
}

func init() { pixelfx.Register("fire/fire-tornado", FireTornado) }

const (
	tornadoDuration = 2
	// tornadoSpin is how many turns the funnel's surface makes a loop.
	tornadoSpin = 3
)

type (
	tornadoOrbiter struct {
		v0, rise, a, w, out float64
		big                 bool
	}
	tornadoEmber struct {
		x, y, z, k, vx, vy float64
		big                bool
	}
	tornadoSmoke struct{ a, s, vy float64 }
	tornadoFlung struct{ v, side, sp, s, f float64 }
)

func newFireTornado(o *pixelfx.Options) pixelfx.Instance {
	D := float64(tornadoDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, S, HT := hotFirst(F), o.Seed(), o.Num("height")
	GLOW, SMOKE := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25), pixelfx.Mix(smoke1, H[6], 0.25)
	EMB := [4]uint32{H[1], H[2], H[4], H[6]}
	// the funnel: narrow at the foot, flaring at the top, its spine snaking as it turns
	cx := func(v, tt float64) float64 {
		return 3.2*math.Sin(v*4.2-(tt/D)*tau*2)*v + 2*math.Sin((tt/D)*tau)*v
	}
	rad := func(v float64) float64 { return 3.5 + 15*math.Pow(v, 1.35) }
	// the surface turns, twisted up the column
	spin := func(v, tt float64) float64 { return (tt/D)*tau*tornadoSpin + v*5.5 }
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		ph := t / D
		N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }

		// embers orbiting: behind the column first, in front after it
		var orbit []tornadoEmber
		looped(t, D, 30, S+500, func(r *pixelfx.Rng, _ int) (tornadoOrbiter, float64) {
			var q tornadoOrbiter
			q.v0 = r.Range(0.02, 0.7)
			q.rise = r.Range(0.12, 0.3)
			q.a = r.Range(0, tau)
			q.w = r.Range(4, 7)
			q.out = r.Range(4, 12)
			life := r.Range(1.2, 2.4)
			q.big = r.Chance(0.2)
			return q, life
		}, func(q *tornadoOrbiter, age, life float64, _ int) {
			v := q.v0 + q.rise*age
			R, a, k := rad(math.Min(1, v))+q.out+age*4, q.a+q.w*age, 1-age/life
			x, y, z := cx(math.Min(1, v), t)+math.Cos(a)*R, -v*HT+math.Sin(a)*R*0.28, math.Sin(a)
			orbit = append(orbit, tornadoEmber{x, y, z, k, -math.Sin(a) * R * q.w, math.Cos(a) * R * q.w * 0.28, q.big})
		})
		embers := func(front bool) {
			for _, e := range orbit {
				if (e.z >= 0) != front || e.k <= 0 {
					continue
				}
				sp := pixelfx.Hypot(e.vx, e.vy)
				if sp == 0 {
					sp = 1
				}
				L := math.Min(5, 1+sp*0.025)
				c := EMB[2]
				if e.k > 0.6 {
					c = EMB[0]
				} else if e.k > 0.3 {
					c = EMB[1]
				}
				p.LineFn(e.x-(e.vx/sp)*L, e.y-(e.vy/sp)*L, e.x, e.y, func(u float64) (uint32, float64) {
					switch {
					case u > 0.6:
						return c, 1
					case front:
						return EMB[3], 1
					}
					return H[7], 1
				})
				if front {
					p.Glow(e.x, e.y, 4, c, 0.4*e.k)
				} else {
					p.Glow(e.x, e.y, 3, c, 0.15*e.k)
				}
			}
		}
		// the ring of flame whirling at the foot: tongues round an ellipse, the far half dimmer
		ring := func(front bool) {
			for i := range 12 {
				a := (float64(i)/12)*tau + (t/D)*tau*2
				z := math.Sin(a)
				if (z >= 0) != front {
					continue
				}
				n := (S*3 + float64(i)) * 19
				R := 12 + 2*N(2, n, 0)
				x, y := math.Cos(a)*R, z*R*0.25
				h := 5 + 5*(0.5+0.5*N(5, n+1, 0))
				ls := layers
				if !front {
					h *= 0.8
					ls = backLayers
				}
				flameTongue(p, x, y, h, 1.6, -math.Cos(a+pi/2)*3, N, n, H, ls, 0.5, 1.1)
			}
		}

		// smoke spiralling off the top
		looped(t, D, 20, S+300, func(r *pixelfx.Rng, _ int) (tornadoSmoke, float64) {
			var q tornadoSmoke
			q.a = r.Range(0, tau)
			life := r.Range(1.2, 2)
			q.s = r.Range(2.5, 4)
			q.vy = -r.Range(12, 22)
			return q, life
		}, func(q *tornadoSmoke, age, life float64, _ int) {
			R, a := rad(1)*(0.8+age*0.5), q.a+age*3
			x, y := cx(1, t-age)+math.Cos(a)*R, -HT-2+q.vy*age+math.Sin(a)*R*0.25
			smokePuff(p, x, y, 1-age/life, q.s, SMOKE, false, 1)
		})
		floorLight(p, 0, 0.5, 40, 8, GLOW, 0.4)
		embers(false)
		ring(false)

		// the column: each pixel's place round the funnel picks its heat; spiral bands turn up it; licks at its edges
		fl := 0.85 + 0.15*N(7, S+1, 0)
		for y := 0.0; y >= -HT*1.2; y-- {
			v := -y / HT
			vv := math.Min(1, v)
			c, r, sp := cx(vv, t), rad(vv)*(1+0.08*N(3, S+2, y*0.1)), spin(v, t)
			// near the top the funnel comes apart into spiral strands of flame climbing past its rim
			cut := pixelfx.Clamp((v - 0.72) / 0.42)
			lickL, lickR := 0.3*math.Max(0, pnoise(-y*0.22+ph*18, 18, S+3)), 0.3*math.Max(0, pnoise(-y*0.22+ph*18+9, 18, S+4))
			for x := math.Floor(c - r*1.35); x <= math.Ceil(c+r*1.35); x++ {
				dx := (x + 0.5 - c) / r
				edge := 1 + lickR
				if dx < 0 {
					edge = 1 + lickL
				}
				if math.Abs(dx) > edge {
					continue
				}
				th := math.Asin(pixelfx.ClampTo(dx, -1, 1))
				depth, tear := math.Cos(th), 0.18*pnoise(y*0.45+dx*2-ph*30, 30, S+6)
				band := 0.5 + 0.5*math.Sin((th+sp+tear)*3)
				streak := band * band * band
				if cut > 0 && band < 0.35+cut*0.6+0.1*pnoise(x*0.7+ph*20, 20, S+5) {
					continue
				}
				// a darker body with thin bright streaks spiralling up it, hottest where it faces us, cooling up the column
				heat := 0.22 + 0.36*depth + 0.5*streak - 0.22*vv + 0.1*(1-vv) - 0.25*cut
				if math.Abs(dx) > 1 {
					heat = 0.3 - (math.Abs(dx)-1)*1.5
				}
				heat *= fl
				col := H[7]
				switch {
				case heat > 0.95:
					col = H[0]
				case heat > 0.82:
					col = H[1]
				case heat > 0.7:
					col = H[2]
				case heat > 0.58:
					col = H[3]
				case heat > 0.46:
					col = H[4]
				case heat > 0.34:
					col = H[5]
				case heat > 0.2:
					col = H[6]
				}
				p.Px(x, y, col)
			}
			if math.Mod(y, 10) == 0 && v <= 1 {
				p.Glow(c, y, r*2+8, GLOW, 0.22*fl)
			}
		}

		// flame flung off the spinning sides, cooling as it flies, some into smoke
		looped(t, D, 70, S+200, func(r *pixelfx.Rng, _ int) (tornadoFlung, float64) {
			var q tornadoFlung
			q.v = r.Range(0.05, 0.95)
			q.side = r.Sign()
			q.sp = r.Range(20, 50)
			life := r.Range(0.25, 0.5)
			q.s = r.Range(1.4, 2.8)
			q.f = r.Next()
			return q, life
		}, func(q *tornadoFlung, age, life float64, _ int) {
			tb := t - age
			x0, y0 := cx(q.v, tb)+q.side*rad(q.v), -q.v*HT
			x, y, _, _ := drift(x0, y0, q.side*q.sp, -q.sp*0.6, age, 0, -60, 3, 3)
			firePuff(p, x, y, 1-age/life, q.s, q.f, H, 0.1, -q.side*0.6, -0.8)
		})
		ring(true)
		embers(true)
		// light: the column's own glow and a wash round it
		p.Glow(0, -HT*0.5, 80, GLOW, 0.28*fl)
		p.Glow(0, -6, 18, H[2], 0.3*fl)
	}}
}
