// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/snowflake-burst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The pop recipe (a gathering, a white flash with a ring and flares, twinkling motes that linger and fade) is adapted from Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// SnowflakeBurst is a cold pop: frost gathers and flashes white in a ring, then crisp six-armed snowflakes spin out, slow, twinkle and drift down to settle and melt. It is registered as "ice/snowflake-burst".
var SnowflakeBurst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Snowflake burst",
		Desc:     "A cold pop: frost gathers and flashes white in a ring, then crisp six-armed snowflakes spin out, slow, twinkle and drift down to settle and melt.",
		Tags:     []string{"ice", "snow", "snowflake", "burst", "magic", "one-shot"},
		Size:     [2]int{140, 120},
		Origin:   [2]int{70, 106},
		Duration: burstDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "count", Default: pixelfx.Num(9)},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newSnowflakeBurst,
}

func init() { pixelfx.Register("ice/snowflake-burst", SnowflakeBurst) }

var (
	burstDuration = 3.2
	burstPop      = 0.16
	burstY        = -48.0
	// burstSizes is the size of each flake in turn: 2 big, 1 middling, 0 small.
	burstSizes = [9]int{2, 0, 1, 0, 2, 0, 1, 0, 1}
)

// burstFlake is one snowflake of the burst.
type burstFlake struct {
	x, y, vx, vy, r, rot, w0, w1 float64
	d                            flake
	life, age, ph, tw, tw0       float64
	rest, popped                 bool
}

func newSnowflakeBurst(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	ps, _, lights := newSim(R, C)
	W := pixelfx.White
	// the flakes: two big, a few middling, the rest small; small ones fly further
	count := o.Num("count")
	var flakes []*burstFlake
	for i := 0.0; i < count; i++ {
		z := burstSizes[int(i)%len(burstSizes)]
		var r float64
		switch z {
		case 2:
			r = R.Range(9.5, 11.5)
		case 1:
			r = R.Range(6, 7.5)
		default:
			r = R.Range(3, 4.5)
		}
		a := (i/count)*tau + R.Range(-0.3, 0.3) + 0.4
		sp := R.Range(80, 120) * (1.4 - r/14)
		f := &burstFlake{x: 0, y: burstY, vx: math.Cos(a) * sp, vy: math.Sin(a)*sp*0.85 - 20, r: r}
		f.rot = R.Range(0, tau)
		f.w0 = R.Sign() * R.Range(7, 12)
		f.w1 = R.Sign() * R.Range(0.4, 1)
		f.d = flakeDesign(R)
		f.life = R.Range(2.1, 2.8)
		f.ph = R.Range(0, tau)
		f.tw = R.Range(0.5, 0.85)
		f.tw0 = R.Range(0.15, 0.6)
		flakes = append(flakes, f)
	}
	t, popped := 0.0, false
	sz := func(f *burstFlake) float64 {
		return f.r * math.Min(1, pixelfx.OutBack(pixelfx.Clamp(f.age/0.18), 2)) * math.Min(1, (f.life-f.age)/0.35)
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the cold gathers: streaks of frost spiral in, snow motes drift in
			if t < burstPop-0.03 && R.Chance(0.8) {
				a, D, T := R.Range(0, tau), R.Range(22, 44), R.Range(0.12, 0.16)
				v := (D * 3) / (math.Exp(3*T) - 1)
				ps.Spawn(pixelfx.Streak, math.Cos(a)*D, burstY+math.Sin(a)*D, -math.Cos(a)*v, -math.Sin(a)*v, T, pick(R.Chance(0.5), C[6], C[5]), 1, 0, 0)
			}
			if !popped && t >= burstPop {
				popped = true
				lights.Add(0, burstY, 44, W, 0.8, 0.1)
				lights.Add(0, burstY, 72, C[4], 0.5, 0.6)
				// frost thrown out: streaking motes, snow, glints that come and go round it
				for range 22 {
					a, sp := R.Range(0, tau), R.Range(80, 150)
					ps.Spawn(pixelfx.Mote, 0, burstY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.25, 0.5), pick(R.Chance(0.5), C[6], C[5]), R.Range(0.6, 1), 0, 0)
				}
				for range 18 {
					a, sp := R.Range(-pi, 0.2), R.Range(30, 110)
					ps.Spawn(pixelfx.Flake, 0, burstY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1, 1.6), pick(R.Chance(0.6), W, C[6]), R.Range(0.5, 1.5), 0, 0)
				}
				for range 12 {
					a, d := R.Range(0, tau), R.Range(10, 44)
					ps.Spawn(pixelfx.StarKind, math.Cos(a)*d, burstY+math.Sin(a)*d*0.85, 0, R.Range(2, 6), R.Range(0.3, 0.6), pick(R.Chance(0.5), W, C[6]), R.Range(0.8, 2.2), R.Range(0.08, 1.6), 0)
				}
			}
			if popped {
				for _, f := range flakes {
					f.age += dt
					if f.age >= f.life {
						continue
					}
					if !f.rest {
						f.vx -= f.vx * 2.8 * dt
						f.vy -= f.vy*2.8*dt - 26*dt
						f.x += f.vx*dt + math.Sin(t*1.8+f.ph)*5*dt
						f.y += f.vy * dt
						f.rot += (f.w1 + (f.w0-f.w1)*math.Exp(-f.age*2.5)) * dt
						// it settles on the floor, flat and still, and melts there
						if f.y > -sz(f)*0.3 {
							f.rest = true
							f.y = -1
							f.life = math.Min(f.life, f.age+0.6)
						}
					}
					// a melted or spent flake goes out with a glint or two
					if !f.popped && f.life-f.age < 0.1 {
						f.popped = true
						for range 2 {
							ps.Spawn(pixelfx.StarKind, f.x+R.Range(-2, 2), f.y+R.Range(-2, 2), R.Range(-6, 6), -R.Range(2, 8), R.Range(0.2, 0.4), pick(R.Chance(0.5), W, C[6]), R.Range(0.8, 1.6), 0, 0)
						}
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
			endBy(t, burstDuration, ps, nil, nil)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the gathering: a tiny flake spins up at the heart, faster and brighter (a count of 0 has no first flake to gather; pixelfx throws there, the port draws no gathering flake)
			if t < burstPop && len(flakes) > 0 {
				k := t / burstPop
				snowflake(p, 0, burstY, 1+3.2*k*k, t*20, flakes[0].d, C, flakeOpts{hot: k})
				p.Glow(0, burstY, 8+14*k, C[5], 0.3+0.4*k)
			}
			// the pop: a white flash, a flare and a ring of frost running out
			if q := t - burstPop; q >= 0 && q < 0.4 {
				if q < 0.07 {
					p.Circle(0, burstY, 6*(1-q/0.07)+1, W, 1)
				}
				pixelfx.Flare(p, 0, burstY, 28*math.Pow(math.Max(0, 1-q/0.26), 1.5), 1-q/0.26, C)
				e := pixelfx.Ease.OutCubic(q / 0.4)
				r := 5 + 34*e
				p.Ring(0, burstY, r, math.Max(1, 2.4*(1-e)), pick(q < 0.08, W, pick(q < 0.2, C[6], C[5])), 1.15-e)
				p.GlowRing(0, burstY, r, 5, C[5], 0.4*(1-e))
			}
			// the cold light left where it burst, fading
			if popped {
				p.Glow(0, burstY, 46, C[4], 0.3*(1-pixelfx.Smooth(burstPop, burstPop+1.6, t)))
			}
			// the flakes: spinning out, twinkling now and then, landing and melting
			for _, f := range flakes {
				if !popped || f.age >= f.life {
					continue
				}
				r := sz(f)
				tw := math.Pow(math.Max(0, math.Sin(((f.age-f.tw0)/f.tw)*tau)), 8) * pick(f.age > f.tw0, 1.0, 0)
				if f.rest {
					// lying on the floor: flattened to a sparkling smear that shrinks away
					L := math.Max(1, pixelfx.Round(r*1.2*((f.life-f.age)/0.6)))
					for i := -L; i <= L; i++ {
						p.Px(f.x+i, -1, pick(math.Abs(i) == L, C[5], pick(i == 0, W, C[6])))
					}
					p.Glow(f.x, -1, 3+L, C[4], 0.3)
					continue
				}
				snowflake(p, f.x, f.y, r, f.rot, f.d, C, flakeOpts{hot: math.Max(tw, 1-pixelfx.Clamp(f.age/0.12))})
				p.Glow(f.x, f.y, 6+r*1.4, C[4], 0.12)
				if tw > 0.5 {
					pixelfx.Flare(p, f.x, f.y, (3+r*0.7)*tw, tw, C)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
