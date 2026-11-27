// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/thunderclap.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The flash and flare, the bolts and their flicker, the plasma spray, the scorch where bolts land and the dust thrown along the floor follow Sprite Fusion's open-source lightning and explosion (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	thWide = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-1), pixelfx.FX.Is(-15), pixelfx.FY.Is(-33), pixelfx.BX.Is(15), pixelfx.BY.Is(-33), pixelfx.FFX.Is(-7), pixelfx.FBX.Is(8))
	thClap = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Y.Is(-2), pixelfx.Crouch.Is(0), pixelfx.FX.Is(-1), pixelfx.FY.Is(-45), pixelfx.BX.Is(1), pixelfx.BY.Is(-45), pixelfx.FFX.Is(-3), pixelfx.FFY.Is(-5), pixelfx.FBX.Is(4), pixelfx.FBY.Is(-5))
)

// Thunderclap is a thunderclap round the caster: a white flash, forked bolts bursting out and stabbing the floor, a heavy shock ring tearing across the ground raising dust and grit, sparks streaking away. It is registered as "lightning/thunderclap".
var Thunderclap = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Thunderclap",
		Desc:     "A thunderclap round the caster: a white flash, forked bolts burst out and stab the floor, a heavy shock ring tears across the ground raising dust and grit, sparks streak away.",
		Tags:     []string{"lightning", "electric", "nova", "aoe", "spell", "shockwave", "one-shot"},
		Size:     [2]int{288, 156},
		Origin:   [2]int{144, 124},
		Duration: 1.9,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: append(voltOptions(),
			pixelfx.Option{Name: "reach", Default: pixelfx.Num(104)},
			pixelfx.Option{Name: "seed", Default: pixelfx.Num(6)},
		),
	},
	Act: &pixelfx.Act{
		OnBody:  true,
		Follow:  pixelfx.Val(0),
		Targets: []float64{104},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: pixelfx.Ready},
			{T: -0.18, Pose: thWide},
			{T: 0.08, Pose: thWide.With(pixelfx.Crouch.Is(6))},
			{T: 0.12, Pose: thClap, How: "snap"},
			{T: 0.55, Pose: thClap.With(pixelfx.Y.Is(0), pixelfx.FFY.Is(-4), pixelfx.FBY.Is(-4), pixelfx.Crouch.Is(1))},
			{T: 1.05, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(3), pixelfx.FX.Is(-10), pixelfx.FY.Is(-28), pixelfx.BX.Is(11), pixelfx.BY.Is(-28), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5))},
			{T: 1.7, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: 0.12, Stop: 0.13, Shake: 0.3, Amp: pixelfx.Val(3), NoTarget: true},
			{T: 0.42, Push: pixelfx.Val(2), Straw: pixelfx.Val(14), Y: pixelfx.Val(-8), Skin: "shock", Shiver: 0.5},
			{T: 1.2, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
	},
	New: newThunderclap,
}

func init() { pixelfx.Register("lightning/thunderclap", Thunderclap) }

// the chest the clap bursts from
const thCY = -22

// thClapT is when it claps, a variable so sums with it round as JavaScript's do.
var thClapT = 0.12

// thBK is a bolt's brightness over the frames it lives.
var thBK = [...]float64{1, 0.7, 0.95, 0.5, 0.8, 0.3}

var (
	thAsh   = pixelfx.Hex("#5b5566")
	thDust  = pixelfx.Hex("#6e6382")
	thStone = pixelfx.Ramp("#655b7a", "#4a4263", "#342d45")
)

type thBolt struct {
	down       bool
	x1, y1, t0 float64
}

func newThunderclap(o *pixelfx.Options) pixelfx.Instance {
	C, seed, REACH, A := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Num("reach"), o.Actor
	R := pixelfx.NewRng(seed)
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: o.Ramp("sparks")})
	hot := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor})
	lights := &pixelfx.Lights{}
	// eight bolts: the lower ones stab the floor, the upper ones tear into the air
	var bolts [8]thBolt
	for i := range bolts {
		down := i%2 == 0
		side := 1.0
		if i%4 < 2 {
			side = -1
		}
		var a float64
		switch {
		case down && side > 0:
			a = R.Range(0.35, 0.75)
		case down:
			a = pi - R.Range(0.35, 0.75)
		case side > 0:
			a = -R.Range(0.15, 1.2)
		default:
			a = pi + R.Range(0.15, 1.2)
		}
		var L, y1 float64
		if down {
			L = -thCY / math.Sin(a)
		} else {
			L = R.Range(40, 62)
		}
		x1 := math.Cos(a) * L
		if !down {
			y1 = thCY + math.Sin(a)*L
		}
		bolts[i] = thBolt{down: down, x1: x1, y1: y1, t0: thClapT + R.Range(0, 0.06)}
	}
	front := func(t float64) float64 {
		if t < thClapT {
			return 0
		}
		return 8 + (REACH-8)*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-thClapT)/0.5))
	}
	ringEnd := thClapT + 0.5
	ramp := []uint32{C[1], C[2], C[4], C[6]}
	smokeC := pixelfx.Mix(thAsh, C[4], 0.3)
	t, clapped := 0.0, false
	var landed [8]bool
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the breath before: sparks rush in to the caster's chest
			if t < thClapT && R.Chance(60*dt) {
				a, d, T := R.Range(0, tau), R.Range(16, 30), R.Range(0.07, 0.1)
				v := (d * 3) / (math.Exp(3*T) - 1)
				c := C[5]
				if R.Chance(0.5) {
					c = C[6]
				}
				ps.Spawn(pixelfx.Streak, math.Cos(a)*d, thCY+math.Sin(a)*d*0.8, -math.Cos(a)*v, -math.Sin(a)*v*0.8, T, c, 1, 0, 0)
			}
			if t >= thClapT && !clapped {
				clapped = true
				lights.Add(0, thCY-10, 100, C[4], 0.65, 0.45)
				lights.Add(0, thCY, 26, pixelfx.White, 0.5, 0.07)
				for range 64 {
					a := R.Range(0, tau)
					sp := R.Range(170, 380)
					if math.Sin(a) > 0.4 {
						sp *= 0.5
					}
					ps.Spawn(pixelfx.Spark, math.Cos(a)*6, thCY+math.Sin(a)*6, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.2, 0.55), pixelfx.White, 1, 0, 0)
				}
				for range 8 {
					ps.Spawn(pixelfx.Smoke, R.Range(-8, 8), thCY+R.Range(-6, 6), R.Range(-25, 25), -R.Range(10, 30), R.Range(0.8, 1.3), smokeC, R.Range(4, 7), R.Range(0.05, 0.2), 0)
				}
			}
			for i, b := range bolts {
				if b.down && !landed[i] && t >= b.t0 {
					landed[i] = true
					lights.Add(b.x1, -4, 46, C[4], 0.7, 0.25)
					for range 12 {
						a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(70, 240)
						ps.Spawn(pixelfx.Spark, b.x1, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
					}
					for range 5 {
						hot.Spawn(pixelfx.Spark, b.x1+R.Range(-2, 2), -1, R.Range(-80, 80), -R.Range(60, 180), R.Range(0.4, 0.8), pixelfx.White, 1, 0, 0)
					}
				}
			}
			// the ring tears the floor up as it passes: dust rolling out, grit tossed
			fr := front(t)
			if t > thClapT && t < ringEnd {
				for _, s := range [2]float64{-1, 1} {
					if R.Chance(70 * dt) {
						ps.Spawn(pixelfx.Smoke, s*fr+R.Range(-3, 3), -R.Range(1, 4), s*R.Range(10, 40), -R.Range(5, 20), R.Range(0.6, 1.1), thDust, R.Range(3, 6)*(1-0.4*(fr/REACH)), 0, pixelfx.Big)
					}
					if R.Chance(50 * dt) {
						x, vx, vy, life, c := s*fr, s*R.Range(10, 60), -R.Range(60, 180), R.Range(0.8, 1.3), pixelfx.Pick(R, thStone)
						size := 1.0
						if R.Chance(0.3) {
							size = 2
						}
						ps.Spawn(pixelfx.Grit, x, -1, vx, vy, life, c, size, 0, 0)
					}
				}
			}
			ps.Update(dt)
			hot.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// scorch where the bolts struck, and the far half of the shock ring
			for _, b := range bolts {
				if b.down && t >= b.t0 {
					scorch(p, b.x1, 0, 9, math.Pow(pixelfx.Clamp(1-(t-b.t0)/1.1), 1.3), 1-pixelfx.Smooth(1.4, 1.85, t))
				}
			}
			if u := (t - thClapT) / 0.5; u > 0 && u < 1 {
				pixelfx.Band(p, 0, 0.5, front(t), 0.18, math.Max(2, 16*(1-u)), 1.25-u, ramp, func(g float64) bool { return math.Sin(g) < 0 }, u < 0.15)
			}
			ps.Draw(p, pixelfx.LayerBack)
			hot.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f := int(math.Floor(t*30 + 1e-4))
			u := (t - thClapT) / 0.5
			// the flash: the caster lit white for a frame, a flare, a core
			a := t - thClapT
			if a >= 0 && a < 0.07 {
				c := C[6]
				if a < 0.035 {
					c = pixelfx.White
				}
				p.Mask(A.Mask, A.X, A.Y, c, 1)
			}
			if a >= 0 && a < 0.2 {
				q := a / 0.2
				pixelfx.Flare(p, 0, thCY, 44*math.Pow(1-q, 1.4), 1-q, C)
				if q < 0.35 {
					p.Circle(0, thCY, 10*(1-q*2)+2, pixelfx.White, 1)
				}
				p.Glow(0, thCY, 44, C[5], 0.32*(1-q))
			}
			// the bolts: struck, flickering, re-struck every other frame
			for i, b := range bolts {
				ba := int(math.Floor((t-b.t0)*30 + 1e-4))
				if t < b.t0 || ba >= len(thBK) {
					continue
				}
				r := pixelfx.RngOf(seed, 3, float64(i), float64(ba>>1))
				forks, s := 0.8, 0.8
				if b.down {
					forks = 0.5
				}
				if ba == 0 {
					s = 1
				}
				stroke(p, zigzag(r, 0, thCY, b.x1, b.y1, 0.25, forks), thBK[ba], C, s, strokeOpts{hero: ba == 0})
				if b.down {
					p.Circle(b.x1, -1, 3*thBK[ba]+1, C[5], 1)
					p.Px(b.x1, -1, pixelfx.White)
					p.Glow(b.x1, -3, 20, C[5], 0.5*thBK[ba])
				} else if ba < 2 {
					pixelfx.Sparkle(p, b.x1, b.y1, 4, C[6], C[4], 0.4)
				}
			}
			// the shock: the near half of the floor ring, and a ring in the air that bursts out from the clap
			if u > 0 && u < 1 {
				pixelfx.Band(p, 0, 0.5, front(t), 0.18, math.Max(2, 16*(1-u)), 1.25-u, ramp, func(g float64) bool { return math.Sin(g) >= 0 }, u < 0.15)
				for _, s := range [2]float64{-1, 1} {
					p.Glow(s*front(t), -3, 28, C[4], 0.45*(1-u))
				}
				ua := (t - thClapT) / 0.26
				ra := 8 + 60*pixelfx.Ease.OutCubic(ua)
				if ua < 1 {
					c := C[3]
					if ua < 0.25 {
						c = C[5]
					} else if ua < 0.6 {
						c = C[4]
					}
					p.RingFn(0, thCY, ra, math.Max(1, 2*(1-ua)), func(_, g float64) (uint32, float64) {
						if thCY+math.Sin(g)*ra > -1 {
							return c, 0
						}
						return c, 0.85 - 0.85*ua
					})
				}
			}
			// the hum after: small arcs crackle round the caster as the charge bleeds away
			if a > 0.2 && a < 0.9 {
				r := pixelfx.RngOf(seed, 9, float64(f))
				k := 1 - (a-0.2)/0.7
				if r.Next() < 0.8 {
					x0, y0, b, l := r.Range(-14, 14), r.Range(-34, -4), r.Range(0, tau), r.Range(6, 13)
					crackle(p, zigzag(r, x0, y0, x0+math.Cos(b)*l, y0+math.Sin(b)*l, 0.4, 0.3), 0.85*k+0.1, C, 0.35, 1)
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			hot.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
