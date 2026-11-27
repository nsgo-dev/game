// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/flamethrower.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame-puff, smoke and spark recipe from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Flamethrower is a pressurised jet shooting right: a blue pilot at the nozzle, a tight stream with surges racing down it, a sputter, burning drops falling off, a tip that curls up into smoke. It is registered as "fire/flamethrower".
var Flamethrower = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Flamethrower",
		Desc:     "A pressurised jet shooting right: a blue pilot at the nozzle, a tight stream with surges racing down it, a sputter, burning drops falling off, a tip that curls up into smoke.",
		Tags:     []string{"fire", "flamethrower", "stream", "weapon", "directional", "loop"},
		Size:     [2]int{184, 104},
		Origin:   [2]int{30, 60},
		Duration: throwerDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "pilot", Default: pixelfx.Palette("#1b2a8a", "#3a62e0", "#a8d4ff")},
			{Name: "reach", Default: pixelfx.Num(112)},
			{Name: "speed", Default: pixelfx.Num(280)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newFlamethrower,
}

func init() { pixelfx.Register("fire/flamethrower", Flamethrower) }

const throwerDuration = 1.6

var (
	throwerSoot = pixelfx.Hex("#2a2430")
	// throwerCough is when in the loop the jet coughs before it surges back; a variable so sums with it round as pixelfx's do.
	throwerCough = 0.55
)

// throwerFlow is how hard the jet pushes at time te: it sputters once a loop, then surges back.
func throwerFlow(te float64) float64 {
	D, C := float64(throwerDuration), throwerCough
	u := math.Mod(math.Mod(te, D)+D, D)
	return 1 - pixelfx.Smooth(C, C+0.04, u)*(1-pixelfx.Smooth(C+0.1, C+0.16, u)) + 0.25*math.Exp(-math.Pow((u-C-0.2)/0.05, 2))
}

type (
	throwerCurl struct{ a0, R, w, up, dx, fl, sl, s, f float64 }
	throwerFire struct {
		u, x, y float64
		q       *throwerCurl
		k       float64
	}
	throwerSmoke struct {
		u, x, y float64
		q       *throwerCurl
	}
	throwerDrop  struct{ d, vx, vy float64 }
	throwerSpark struct{ born, a, v float64 }
	throwerBlob  struct{ born, v, vy, s float64 }
)

func newFlamethrower(o *pixelfx.Options) pixelfx.Instance {
	D, COUGH := float64(throwerDuration), throwerCough
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, PL, S, L, V := hotFirst(F), pixelfx.Fit(o.Ramp("pilot"), 3), o.Seed(), o.Num("reach"), o.Num("speed")
	GLOW, SMOKE := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25), pixelfx.Mix(throwerSoot, H[7], 0.3)
	SPARKS := [4]uint32{H[1], H[2], H[4], H[5]}
	flow := throwerFlow
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }
		// the stream's centre line: dead straight at the nozzle, sagging and wavering as it runs out
		cy := func(x float64) float64 {
			return 5*math.Pow(x/L, 2) + N(1.2, S+9, x*0.02)*2.2*math.Pow(x/L, 1.5)
		}
		width := func(x float64) float64 { return (2.1 + 5*math.Pow(x/L, 0.7)) * flow(t-x/V) }

		// the curl: where the stream runs out of push, burning fuel rolls up and back on itself and turns to smoke
		var fire []throwerFire
		var smoke []throwerSmoke
		looped(t, D, 80, S+100, func(r *pixelfx.Rng, _ int) (throwerCurl, float64) {
			var q throwerCurl
			q.a0 = r.Range(-0.3, 0.5)
			q.R = r.Range(9, 15)
			q.w = r.Range(5, 7.5)
			q.up = r.Range(4, 12)
			q.dx = r.Range(-6, 2)
			q.fl = r.Range(0.38, 0.62)
			q.sl = r.Range(0.6, 1.0)
			q.s = r.Range(2.4, 4.4)
			q.f = r.Next()
			return q, 1.6
		}, func(q *throwerCurl, age, _ float64, _ int) {
			tb := t - age
			k := flow(tb - L/V)
			if k < 0.4 {
				return
			}
			// round a curl: forward, up and back over the top, rising as it goes
			x0, y0, a := L+q.dx, cy(L), math.Min(age, q.fl)*q.w+q.a0
			x, y := x0+q.R*math.Sin(a), y0-q.R*(1-math.Cos(a))-q.up*age
			if age < q.fl {
				fire = append(fire, throwerFire{age / q.fl, x, y, q, k})
				return
			}
			if age-q.fl < q.sl {
				smoke = append(smoke, throwerSmoke{(age - q.fl) / q.sl, x - (age-q.fl)*6, y - (age-q.fl)*16, q})
			}
		})
		for _, s := range smoke {
			smokePuff(p, s.x, s.y, 1-s.u, s.q.s*1.2, SMOKE, true, 0.85)
		}
		slices.SortStableFunc(fire, func(a, b throwerFire) int { return cmpDesc(a.u, b.u) })
		for _, f := range fire {
			firePuff(p, f.x, f.y, 1-f.u, f.q.s*(0.8+1.1*f.u)*f.k, f.q.f, H, 0.1, 0, -1)
		}

		// flames licking up off the burning fuel, riding along the stream, swept back by the wind of it
		for i := range 13 {
			n := (S*3 + float64(i)) * 19
			ph := math.Mod(float64(i)*0.618+t/D, 1)
			x := 10 + ph*(L-14)
			k := flow(t - x/V)
			if k < 0.35 {
				continue
			}
			h := (2 + 11*(x/L)) * (0.55 + 0.6*(0.5+0.5*N(5, n+1, 0))) * k * math.Min(1, (1-ph)*6)
			flameTongue(p, x, cy(x)-width(x)*0.6, h, 1.1+1.6*(x/L), -h*0.55, N, n, H, layers, 0.5, 1.1)
		}
		// the stream: a white core in yellow, orange and a red skin, surges racing down it
		P := pixelfx.Round(D * 3)
		lvlC := [5]uint32{H[0], H[2], H[4], H[6], H[7]}
		for x := 0.0; x <= L; x++ {
			c, w := cy(x), width(x)*(1+0.15*pnoise(x*0.4-(t/D)*24, 24, S+4))
			if w < 0.4 {
				continue
			}
			surge := 0
			if math.Mod(math.Mod(x*0.035-(t/D)*P, 1)+1, 1) < 0.12 {
				surge = 1
			}
			for y := math.Floor(c - w - 1); y <= math.Ceil(c+w+1); y++ {
				q := math.Abs(y+0.5-c) / w
				if q > 1.15 {
					continue
				}
				// a white core near the nozzle cooling to yellow down the stream; ragged red skin
				lvl := 4
				switch {
				case q < 0.3:
					lvl = 0
				case q < 0.55:
					lvl = 1
				case q < 0.78:
					lvl = 2
				case q < 1:
					lvl = 3
				}
				if x > L*0.45 && q < 0.55 {
					lvl++
				}
				lvl = max(0, min(4, lvl-surge))
				if lvl == 4 && pnoise(x*0.5+y*7.3-(t/D)*40, 40, S+6) < 0 {
					continue
				}
				p.Px(x, y, lvlC[lvl])
			}
			// two hot strands twisting through the fuel, racing downstream
			for _, ph := range [2]float64{0, pi} {
				a := x*0.22 - (t/D)*math.Pi*2*9 + ph
				sy := c + math.Sin(a)*w*0.55
				if math.Cos(a) > -0.1 && w > 1.2 {
					c := H[1]
					if math.Cos(a) > 0.55 {
						c = H[0]
					}
					p.Px(x, sy, c)
				}
			}
		}
		// the pilot at the nozzle: a blue cone of gas where it lights
		pk := flow(t)
		for x := 0.0; x < 6; x++ {
			for y := -2.0; y <= 2; y++ {
				if math.Abs(y) <= 2-x*0.3 {
					c := PL[0]
					if math.Abs(y) < 1 {
						c = PL[1]
						if x < 2 {
							c = PL[2]
						}
					}
					p.Px(x, y, c)
				}
			}
		}
		solid(p, 1, 0, 1.6*pk, pixelfx.White)

		// burning drops shaken off the stream: they arc down trailing flame and burn out
		looped(t, D, 10, S+200, func(r *pixelfx.Rng, _ int) (throwerDrop, float64) {
			var q throwerDrop
			q.d = r.Range(20, 95)
			q.vx = r.Range(0.3, 0.6)
			q.vy = r.Range(-25, 15)
			return q, r.Range(0.35, 0.6)
		}, func(q *throwerDrop, age, life float64, _ int) {
			tb := t - age
			if flow(tb-q.d/V) < 0.5 {
				return
			}
			at := func(a float64) (float64, float64) {
				x, y, _, _ := drift(q.d, cy(q.d), V*q.vx, q.vy, a, 0, 260, 1.2, 0.5)
				return x, y
			}
			for j := 4.0; j >= 1; j-- {
				a := age - j*0.035
				if a < 0 {
					continue
				}
				x, y := at(a)
				firePuff(p, x, y, 1-math.Min(0.99, j*0.18+age/life*0.4), 1.8-j*0.25, 0.3, H, 0.06, 0, -1)
			}
			x, y := at(age)
			k := 1 - age/life
			c := H[2]
			if k > 0.5 {
				c = H[0]
			}
			p.Px(x, y, c)
			p.Px(x+1, y, H[3])
			p.Glow(x, y, 6, H[3], 0.45*k)
		})
		// the cough: when the jet sputters the nozzle spits sparks and a puff of smoke
		looped(t, D, 12, S+300, func(r *pixelfx.Rng, _ int) (throwerSpark, float64) {
			var q throwerSpark
			q.born = COUGH + r.Range(0, 0.08)
			q.a = r.Range(-0.8, 0.8)
			q.v = r.Range(120, 240)
			return q, r.Range(0.2, 0.45)
		}, func(q *throwerSpark, age, life float64, _ int) {
			u := math.Mod(math.Mod(t-age, D)+D, D)
			if math.Abs(u-q.born) > 0.06 {
				return
			}
			x, y, vx, vy := drift(4, 0, math.Cos(q.a)*q.v, math.Sin(q.a)*q.v, age, 0, 300, 2.5, 2.5)
			spark(p, x, y, vx, vy, 1-age/life, SPARKS)
		})
		// and spits a few blobs of burning fuel that fall short
		looped(t, D, 5, S+350, func(r *pixelfx.Rng, _ int) (throwerBlob, float64) {
			var q throwerBlob
			q.born = COUGH + r.Range(0.01, 0.1)
			q.v = r.Range(90, 170)
			q.vy = r.Range(-30, 10)
			life := r.Range(0.25, 0.4)
			q.s = r.Range(1.6, 2.6)
			return q, life
		}, func(q *throwerBlob, age, life float64, _ int) {
			u := math.Mod(math.Mod(t-age, D)+D, D)
			if math.Abs(u-q.born) > 0.05 {
				return
			}
			x, y, _, _ := drift(4, 0, q.v, q.vy, age, 0, 200, 2, 1)
			firePuff(p, x, y, 1-age/life, q.s, 0.2, H, 0.1, -0.86, -0.5)
		})
		if a := math.Mod(math.Mod(t, D)+D, D) - COUGH; a > 0 && a < 0.7 {
			smokePuff(p, 8+a*20, -a*18, 1-a/0.7, 3, SMOKE, false, 0.8)
		}

		// light: the pilot, the stream, the burning tip
		p.Glow(2, 0, 34*pk, GLOW, 0.8*pk)
		p.Glow(2, 0, 9, PL[2], 0.45*pk)
		for d := 8.0; d <= L; d += 8 {
			p.Glow(d, cy(d), 12+d*0.16, GLOW, 0.3*flow(t-d/V))
		}
		p.Glow(L, cy(L)-10, 52, GLOW, 0.34*flow(t-L/V))
	}}
}
