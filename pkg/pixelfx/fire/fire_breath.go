// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/fire-breath.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame-puff, smoke and spark recipe from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FireBreath is a roaring cone of fire streaming right from the mouth: white-hot at the lips, billowing out and cooling to red and smoke at its edges, sparks thrown out ahead. It is registered as "fire/fire-breath".
var FireBreath = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fire breath",
		Desc:     "A roaring cone of fire streaming right from the mouth: white-hot at the lips, billowing out and cooling to red and smoke at its edges, sparks thrown out ahead.",
		Tags:     []string{"fire", "breath", "dragon", "stream", "cone", "directional", "loop"},
		Size:     [2]int{160, 116},
		Origin:   [2]int{12, 72},
		Duration: breathDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "spread", Default: pixelfx.Num(0.24)},
			{Name: "speed", Default: pixelfx.Num(320)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newFireBreath,
}

func init() { pixelfx.Register("fire/fire-breath", FireBreath) }

const breathDuration = 1.5

// coolLayers are the dying licks: red and deep orange, no white heart.
var coolLayers = []layer{{1, 1, 7}, {0.72, 0.82, 6}, {0.44, 0.58, 5}}

// breathJet is the jet at the lips: length, half-width, angle.
var breathJet = [4][3]float64{{42, 4.2, 0}, {32, 3.8, -0.08}, {30, 3.8, 0.08}, {20, 4.6, 0}}

type (
	breathPuff struct {
		e, sp, fl, sl, s, f, ox float64
		sm                      bool
	}
	breathDrawn struct {
		age, x, y float64
		q         *breathPuff
	}
	breathStreak struct{ e, v, l float64 }
	breathSpark  struct{ d, e, v float64 }
	breathEmber  struct{ x, y, vx, vy, s, ph float64 }
)

func newFireBreath(o *pixelfx.Options) pixelfx.Instance {
	D := float64(breathDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, S, A, V := hotFirst(F), o.Seed(), o.Num("spread"), o.Num("speed")
	GLOW, SMOKE := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25), pixelfx.Mix(smoke1, H[6], 0.3)
	SPARKS, EMB := [4]uint32{H[1], H[2], H[4], H[5]}, [4]uint32{H[1], H[2], H[4], H[6]}
	// the roar: the stream surges and the cone sways a little
	roar := func(tt float64) float64 { return 0.85 + 0.15*NL(tt, 3, S+1, 0) }
	sway := func(tt float64) float64 { return NL(tt, 1.3, S+2, 0) * 0.05 }
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }

		// the cone: puffs of flame blown out of the mouth that slow, swell, rise and cool; those near the edges cool first, and many burn out into rolling smoke
		var fire, smoke []breathDrawn
		looped(t, D, 260, S+100, func(r *pixelfx.Rng, _ int) (breathPuff, float64) {
			var q breathPuff
			q.e = r.Range(-1, 1)
			q.fl = r.Range(0.42, 0.8) * (1 - 0.3*math.Abs(q.e))
			q.sm = r.Chance(0.2 + 0.4*math.Abs(q.e))
			q.sp = r.Range(0.75, 1.1)
			q.sl = r.Range(0.5, 0.85)
			q.s = r.Range(3, 5.2)
			q.f = r.Next()
			q.ox = r.Range(4, 10)
			life := q.fl
			if q.sm {
				life = q.fl + 0.85
			}
			return q, life
		}, func(q *breathPuff, age, _ float64, _ int) {
			tb := t - age
			a, v := q.e*A+sway(tb)-0.04, V*q.sp*roar(tb)
			ta := math.Min(age, q.fl)
			x, y, vx, vy := drift(q.ox, q.e*1.5, math.Cos(a)*v, math.Sin(a)*v, ta, 0, -100, 2, 2.4)
			if age < q.fl {
				fire = append(fire, breathDrawn{age, x, y, q})
				return
			}
			if !q.sm || age-q.fl > q.sl {
				return
			}
			sa := age - q.fl
			sx, sy, _, _ := drift(x, y, vx*0.5, vy*0.6-8, sa, 0, -26, 1.8, 1.2)
			smoke = append(smoke, breathDrawn{sa, sx, sy, q})
		})
		for _, d := range smoke {
			smokePuff(p, d.x, d.y, 1-d.age/d.q.sl, d.q.s*1.1, SMOKE, true, 0.7)
		}
		slices.SortStableFunc(fire, func(a, b breathDrawn) int { return cmpDesc(a.age, b.age) })
		for _, d := range fire {
			q := d.q
			u, edge := d.age/q.fl, math.Abs(q.e)
			size := q.s * (0.75 + 1.6*u)
			// the heart of the stream stays hot longer than its edges
			heat := math.Min(0.999, math.Pow(u, 1.5)*(0.85+0.2*edge)+0.3*edge*edge)
			// near the mouth the fire rolls in round billows; further out each billow tears into a tongue licking up and back, thinning to a lick as it cools, before the smoke takes it
			tear := pixelfx.Clamp((u - 0.32) / 0.2)
			firePuff(p, d.x, d.y, 1-heat, size*(1-0.55*tear), q.f, H, 0.1, -0.5, -0.86)
			if tear > 0 {
				n := math.Floor(q.f * 1e5)
				h := size * (1.5 + 0.5*N(4, n, 0)) * (1.2 - heat*0.6) * tear
				ls := coolLayers
				switch {
				case heat < 0.45:
					ls = layers
				case heat < 0.72:
					ls = backLayers
				}
				flameTongue(p, d.x, d.y+size*0.3, h, size*0.5*(1.1-heat*0.5), -h*0.4+N(2.5, n+1, 0)*2, N, n, H, ls, 0.5, 1.1)
			}
		}
		// licks curling off the edges of the cone
		for i := range 6 {
			n := (S*3 + float64(i)) * 19
			side := -1.0
			if i%2 != 0 {
				side = 1
			}
			d := 18 + float64(i>>1)*16 + N(2, n, 0)*4
			L := (9 + 5*(0.5+0.5*N(5, n+1, 0))) * (1 - d/120)
			ex, ey := d, side*d*A*0.85-d*0.06
			flameBlade(p, ex, ey, 0.75, side*0.45-0.48, L, 1.6+L*0.08, -side*3, N, n, H, layers, 0.5, 1.1)
		}
		// the roar: bright streaks racing out through the heart of the cone
		looped(t, D, 70, S+250, func(r *pixelfx.Rng, _ int) (breathStreak, float64) {
			var q breathStreak
			q.e = r.Range(-0.6, 0.6)
			q.v = r.Range(330, 470)
			life := r.Range(0.12, 0.26)
			q.l = r.Range(4, 9)
			return q, life
		}, func(q *breathStreak, age, life float64, _ int) {
			a := q.e*A - 0.03
			x, y, vx, vy := drift(6, q.e*2, math.Cos(a)*q.v, math.Sin(a)*q.v, age, 0, -60, 2, 2)
			sp := pixelfx.Hypot(vx, vy)
			k := 1 - age/life
			L := q.l * (sp / 400)
			p.LineFn(x-(vx/sp)*L, y-(vy/sp)*L, x, y, func(u float64) (uint32, float64) {
				if u > 0.6 {
					if k > 0.5 {
						return H[0], 1
					}
					return H[1], 1
				}
				if k > 0.5 {
					return H[2], 1
				}
				return H[3], 1
			})
		})
		// the jet at the lips: white-hot, overlapping tongues that flutter as the breath roars
		for i, j := range breathJet {
			ln, w, a := j[0], j[1], j[2]
			n := (S*5 + float64(i)) * 17
			L := ln * roar(t) * (0.85 + 0.2*N(5, n, 0))
			aa := a + sway(t)
			flameBlade(p, 0, 0, math.Cos(aa), math.Sin(aa), L, w, N(3, n+3, 0)*3, N, n, H, layers, 0.5, 1.5)
		}

		// sparks flung out ahead and falling; embers lifting off the far end
		looped(t, D, 40, S+300, func(r *pixelfx.Rng, _ int) (breathSpark, float64) {
			var q breathSpark
			q.d = r.Range(4, 60)
			q.e = r.Range(-1, 1)
			q.v = r.Range(140, 280)
			return q, r.Range(0.25, 0.55)
		}, func(q *breathSpark, age, life float64, _ int) {
			a := q.e*A*1.8 - 0.12
			x, y, vx, vy := drift(q.d, q.e*q.d*A, math.Cos(a)*q.v, math.Sin(a)*q.v-30, age, 0, 190, 2, 2)
			spark(p, x, y, vx, vy, 1-age/life, SPARKS)
		})
		looped(t, D, 18, S+400, func(r *pixelfx.Rng, _ int) (breathEmber, float64) {
			var q breathEmber
			q.x = r.Range(50, 100)
			q.y = r.Range(-14, 6)
			q.vx = r.Range(10, 40)
			q.vy = -r.Range(20, 50)
			life := r.Range(0.8, 1.4)
			q.s = 1
			if r.Chance(0.2) {
				q.s = 2
			}
			q.ph = r.Range(0, 6.3)
			return q, life
		}, func(q *breathEmber, age, life float64, _ int) {
			x, y, _, _ := drift(q.x, q.y, q.vx, q.vy, age, 0, -30, 1.6, 1.6)
			ember(p, x+5*(math.Sin(age*2.3+q.ph)-math.Sin(q.ph)), y, 1-age/life, q.s, EMB, age*40+q.ph)
		})

		// light: a flare at the lips, light all along the cone widening with it, a broad halo
		k := roar(t)
		p.Glow(3, 0, 34*k, GLOW, 0.75*k)
		p.Glow(3, 0, 10, H[0], 0.55)
		for d := 10.0; d < 126; d += 8 {
			p.Glow(d, -d*0.1, 10+d*0.2, GLOW, 0.32*k*(1-d/150))
		}
		p.Glow(64, -12, 96, GLOW, 0.16*k)
	}}
}
