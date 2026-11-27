// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/heal-aoe.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// HealAoe is a healing ring: a seed of green light sinks into the floor and a soft ring rolls out from it; the floor glows where it passes and plus signs sprout and float up. It is registered as "holy/heal-aoe".
var HealAoe = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Healing ring",
		Desc:     "A seed of green light sinks into the floor and a soft ring rolls out from it; the floor glows where it passes and plus signs sprout and float up.",
		Tags:     []string{"holy", "heal", "aoe", "ring", "plus", "ground", "one-shot"},
		Size:     [2]int{256, 136},
		Origin:   [2]int{128, 104},
		Duration: 2.9,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: greenPalette()},
			{Name: "reach", Default: pixelfx.Num(104)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newHealAoe,
}

func init() { pixelfx.Register("holy/heal-aoe", HealAoe) }

var healAoeSY, healAoeSeed, healAoeRoll = 0.2, 0.3, 1.0

// healSite is where a plus sign sprouts: scattered over the floor at (x, z), waiting until t0 for the ring to reach it, then rising rise px over life seconds, swaying from phase sway, arm L and thickness thick.
type healSite struct {
	x, z, t0, rise, life, sway float64
	L, thick                   int
	popped                     bool
}

type healRider struct{ a, w float64 }

func newHealAoe(o *pixelfx.Options) pixelfx.Instance {
	C, REACH, R, W := pixelfx.Fit(o.Palette(), 8), bounded(o.Num("reach"), 8, 1024), pixelfx.NewRng(o.Seed()), pixelfx.White
	SY, SEED, ROLL := healAoeSY, healAoeSeed, healAoeRoll
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{W, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	front := func(tt float64) float64 {
		if tt < SEED {
			return 0
		}
		return REACH * pixelfx.Ease.OutCubic(pixelfx.Seg(tt, SEED, SEED+ROLL))
	}
	passed := func(d float64) float64 { return SEED + ROLL*(1-math.Cbrt(1-math.Min(0.999, d/REACH))) }
	// where plus signs sprout: scattered over the floor, each waiting for the ring to reach it
	sites := make([]healSite, 26)
	for i := range sites {
		a := R.Range(0, tau)
		d := REACH * math.Sqrt(R.Range(0.04, 0.92))
		big := i%5 < 2
		t0 := passed(d) + R.Range(0, 0.06)
		rise, life, sway := R.Range(18, 34), R.Range(0.9, 1.3), R.Range(0, tau)
		L, thick := 2, 1
		switch {
		case big && i%5 == 0:
			L, thick = 4, 3
		case big:
			L, thick = 3, 3
		case i%5 == 2:
			L = 1
		}
		sites[i] = healSite{x: math.Cos(a) * d, z: math.Sin(a) * d, t0: t0, rise: rise, life: life, sway: sway, L: L, thick: thick}
	}
	// the rolling ring carries little lights round with it
	riders := make([]healRider, 14)
	for i := range riders {
		a := (float64(i)/14)*tau + R.Range(-0.2, 0.2)
		w := R.Range(0.6, 1.1)
		if i%2 == 0 {
			w = -w
		}
		riders[i] = healRider{a: a, w: w}
	}
	t, released := 0.0, false

	wash := func(p *pixelfx.Painter) {
		// the floor glows behind the front and fades after it
		fr := front(t)
		if fr < 2 || t > 2.6 {
			return
		}
		p.Shade(-fr-1, -fr*SY-1, fr+1, fr*SY+1, func(x, y float64, out *pixelfx.Out) uint32 {
			d := pixelfx.Hypot(x, y/SY)
			if d > fr {
				return 0
			}
			age := t - passed(d)
			k := math.Exp(-age/0.35) * pixelfx.Smooth(fr, fr-6, d) * (1 - pixelfx.Smooth(1.4, 1.9, t))
			if k < 0.04 {
				return 0
			}
			out.L = C[4]
			out.K = 0.32 * k
			return 0
		})
	}

	ring := func(p *pixelfx.Painter, back bool) {
		if t < SEED {
			return
		}
		u, fr := pixelfx.Seg(t, SEED, SEED+ROLL+0.25), front(t)
		k := 1.0
		if u >= 0.7 {
			k = (1 - u) / 0.3
		}
		if k <= 0 {
			return
		}
		keep := halfOf(back)
		pixelfx.Band(p, 0, 0, fr, SY, math.Max(2, 7*(1-u*0.7)), k, C[2:], keep, t < SEED+0.08)
		ka := k
		if back {
			ka = k * 0.7
		}
		ovalFn(p, 0, 0, fr+1, (fr+1)*SY, func(g float64) (uint32, float64) {
			if math.Sin(g) > 0.3 {
				return W, ka
			}
			return C[6], ka
		}, keep)
		// a crest of light blades flickers up along the front as it rolls
		for j, n := 0, int(math.Ceil(fr*0.9)); j < n; j++ {
			g := (float64(j) / float64(n)) * tau
			isNear := math.Sin(g) >= 0
			if isNear == back || pixelfx.Rand(float64(j), math.Floor(t*20)) > 0.45 {
				continue
			}
			x, y := math.Cos(g)*fr, math.Sin(g)*fr*SY
			h := (1 + pixelfx.Rand(float64(j), 7)*4) * k
			if !isNear {
				h *= 0.6
			}
			for i := 1; float64(i) <= h; i++ {
				fi := float64(i)
				c := C[6]
				if fi > h-1 {
					c = W
				}
				p.Dot(x, y-fi, c, k*(1.2-fi/(h+1)))
			}
		}
		// lights riding round the ring as it rolls
		for _, r := range riders {
			g := r.a + r.w*(t-SEED)*2.2
			isNear := math.Sin(g) >= 0
			if isNear == back {
				continue
			}
			x, y := math.Cos(g)*fr, math.Sin(g)*fr*SY
			if isNear {
				arm := 2.0
				if math.Sin(t*9+r.a*5) > 0.3 {
					arm = 3
				}
				pixelfx.Sparkle(p, x, y-1, arm, W, C[6], 0.35*k)
			} else {
				p.Dot(x, y, C[6], k)
			}
		}
		if !back {
			for i := range 8 {
				g := (float64(i) / 8) * tau
				p.Glow(math.Cos(g)*fr, math.Sin(g)*fr*SY-2, 16, C[4], 0.22*k)
			}
		}
	}

	sprouts := func(p *pixelfx.Painter, back bool) {
		for _, s := range sites {
			a := t - s.t0
			if a < 0 || a > s.life || (s.z < 0) != back {
				continue
			}
			u := a / s.life
			h := s.rise * pixelfx.Ease.OutCubic(u)
			x, y := s.x+math.Sin(a*4+s.sway)*1.5*u, s.z*SY-2-h
			pop, end := pixelfx.OutBack(pixelfx.Clamp(a/0.12), 2.6), s.life-0.15
			lift := 0.0
			if back {
				lift = 1
			}
			L := int(math.Max(1, pixelfx.Round(float64(s.L)*pop-lift)))
			switch {
			case a < 0.04:
				pixelfx.Sparkle(p, x, y, 3, W, C[6], 0.5)
			case a < end:
				th, ramp, al := s.thick, C, 1.0
				if back {
					ramp, al = C[:7], 0.8
					if L < 3 {
						th = 1
					}
				}
				plus(p, x, y, L, th, ramp, al, !back && math.Sin(a*12+s.sway*3) > 0.8)
			default:
				v := (a - end) / 0.15
				c := C[6]
				if v < 0.4 {
					c = W
				}
				for _, d := range [4][2]float64{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
					p.Dot(x+d[0]*(2+v*5), y+d[1]*(2+v*5)-v*2, c, 1-v)
				}
			}
			// a thread of light still ties it to the floor while it rises
			if u < 0.5 {
				k := 0.8
				if back {
					k = 0.5
				}
				for j := 1.0; j < h*(1-u*2); j += 1.5 {
					p.Dot(s.x, s.z*SY-2-h+j+3, C[5], (1-j/h)*(1-u*2)*k)
				}
			}
			gk := 0.34
			if back {
				gk = 0.2
			}
			fade := 1.0
			if a >= end {
				fade = 1 - (a-end)/0.15
			}
			p.Glow(x, y, 7+float64(L)*2, C[5], gk*fade)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the seed gathers: motes slide in over the floor to a point that brightens
			if t < SEED-0.04 && R.Chance(0.7) {
				a, D := R.Range(0, tau), R.Range(24, 48)
				x := math.Cos(a) * D
				y := math.Sin(a)*D*SY - R.Range(1, 8)
				T := R.Range(0.16, 0.24)
				l := pixelfx.Hypot(x, y+3)
				if l == 0 {
					l = 1
				}
				v := (l * 3) / (math.Exp(3*T) - 1)
				ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, ((-3-y)/l)*v, T, chance(R, 0.5, C[6], C[5]), 1, 0, 0)
			}
			if !released && t >= SEED {
				released = true
				lights.Add(0, -16, 76, C[5], 0.85, 0.55)
				lights.Add(0, -6, 28, W, 0.8, 0.12)
				for range 10 {
					x, y, vx, vy, life := R.Range(-2, 2), -R.Range(2, 10), R.Range(-25, 25), -R.Range(80, 170), R.Range(0.3, 0.6)
					col, size := chance(R, 0.5, W, C[6]), R.Range(0.6, 1)
					ps.Spawn(pixelfx.Mote, x, y, vx, vy, life, col, size, 0, 0)
				}
				for range 16 {
					a, sp := R.Range(pi*1.05, pi*1.95), R.Range(40, 120)
					life, col, size := R.Range(0.4, 0.8), chance(R, 0.5, W, C[6]), R.Range(0.6, 2)
					ps.Spawn(pixelfx.StarKind, 0, -3, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
				}
			}
			for i := range sites {
				s := &sites[i]
				if s.popped || t < s.t0 {
					continue
				}
				s.popped = true
				// a splash of dew where each one breaks the floor
				var flag pixelfx.Flag
				if s.z < 0 {
					flag = pixelfx.Back
				}
				for range 3 {
					x, vx, vy, life := s.x+R.Range(-3, 3), R.Range(-10, 10), -R.Range(15, 40), R.Range(0.3, 0.6)
					col, size := chance(R, 0.5, W, C[6]), R.Range(0.5, 1.2)
					ps.Spawn(pixelfx.StarKind, x, s.z*SY-1, vx, vy, life, col, size, 0, flag)
				}
			}
			// dew rising off the glowing floor behind the ring
			if t > SEED+0.1 && t < 1.9 && R.Chance(0.35) {
				fr, a := front(t), R.Range(0, tau)
				d := fr * math.Sqrt(R.Next())
				var flag pixelfx.Flag
				if math.Sin(a) < 0 {
					flag = pixelfx.Back
				}
				vx, vy, life := R.Range(-5, 5), -R.Range(10, 30), R.Range(0.5, 1)
				ps.Spawn(pixelfx.Ember, math.Cos(a)*d, math.Sin(a)*d*SY-1, vx, vy, life, C[5], 1, 0, flag)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			wash(p)
			ring(p, true)
			sprouts(p, true)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, false)
			// the seed: a point of light pulsing on the floor, then a flare as it lets go
			if t < SEED {
				k := t / SEED
				pixelfx.Sparkle(p, 0, -3, 2+4*k*k, W, C[6], 0.3+0.4*k)
				p.Glow(0, -3, 10+16*k, C[4], 0.5*k)
			} else if t < SEED+0.2 {
				pixelfx.Flare(p, 0, -3, 22*math.Pow(1-(t-SEED)/0.2, 1.5), 1-(t-SEED)/0.2, C[3:])
			}
			// as it lets go, a jet of light leaps out of the floor, lets go of it and dissolves upward
			g := pixelfx.Seg(t, SEED, SEED+0.5)
			if g > 0 && g < 1 {
				top := -4 - 46*pixelfx.Ease.OutCubic(math.Min(1, g*2.2)) - 10*g
				bot := -2 - 50*pixelfx.Ease.InQuad(pixelfx.Seg(g, 0.35, 1))
				w := 1 + 2.4*(1-g)
				if bot-top > 2 {
					p.BeamFn(0, bot, 0, top, w*2, func(u, v float64) (uint32, float64) {
						switch {
						case v < 0.45:
							return W, 1
						case u > 0.8:
							return C[5], 1
						}
						return C[6], 1
					})
					p.GlowLine(0, bot, 0, top, 12, C[5], 0.55*(1-g))
					pixelfx.Sparkle(p, 0, top, 4*(1-g)+1, W, C[6], 0.5*(1-g))
				}
			}
			sprouts(p, false)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
