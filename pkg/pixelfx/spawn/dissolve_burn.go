// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/dissolve-burn.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flames that cool into smoke, the embers and the smouldering glow from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	dissolveDuration = 3.6
	dissolveWrithe   = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-13), pixelfx.FY.Is(-35), pixelfx.BX.Is(13), pixelfx.BY.Is(-26), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	dissolveIgnite   = 0.15
	dissolveBurn     = 1.9
	dissolveChar     = [3]uint32{pixelfx.Hex("#100908"), pixelfx.Hex("#22130e"), pixelfx.Hex("#3a2016")}
)

// DissolveBurn lights an ember seam at the feet and climbs the body: above it the body scorches and smoulders, at it flames lick up, below it there's only falling ash, embers and smoke. It is registered as "spawn/dissolve-burn".
var DissolveBurn = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Dissolve burn",
		Desc:     "An ember seam lights at the feet and climbs the body: above it the body scorches and smoulders, at it flames lick up, below it there's only falling ash, embers and smoke.",
		Tags:     []string{"spawn", "death", "burn", "fire", "ash", "dissolve", "embers", "one-shot"},
		Size:     [2]int{96, 112},
		Origin:   [2]int{48, 100},
		Duration: dissolveDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0e08", "#5a1a0c", "#a8320f", "#e8601a", "#ff9a2e", "#ffd25a", "#fff3c0", "#ffffff")},
			{Name: "ash", Default: pixelfx.Palette("#2a2428", "#4a4248", "#6e6670")},
			{Name: "seed", Default: pixelfx.Num(16)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.16, Pose: pixelfx.Ready},
			{T: 0.4, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-9), pixelfx.FY.Is(-22), pixelfx.BX.Is(10), pixelfx.BY.Is(-22), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 0.8, Pose: dissolveWrithe},
			{T: 1.2, Pose: dissolveWrithe.With(pixelfx.Lean.Is(2), pixelfx.FY.Is(-26), pixelfx.BY.Is(-36))},
			{T: 1.6, Pose: dissolveWrithe.With(pixelfx.Lean.Is(-2), pixelfx.FY.Is(-36), pixelfx.BY.Is(-26))},
			{T: 2.1, Pose: dissolveWrithe.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-38), pixelfx.BX.Is(9), pixelfx.BY.Is(-38))},
			{T: 3.6, Pose: dissolveWrithe.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-36), pixelfx.BX.Is(9), pixelfx.BY.Is(-36))},
		},
	},
	ActorAlpha: func(float64, *pixelfx.Options) float64 { return 0 },
	New:        newDissolveBurn,
}

func init() { pixelfx.Register("spawn/dissolve-burn", DissolveBurn) }

type burnSeamTongue struct {
	x, h, s float64
}

func newDissolveBurn(o *pixelfx.Options) pixelfx.Instance {
	C, A, R := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("ash"), 3), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	body := bodyOf(o.Actor, C)
	H := float64(body.h)
	// when each pixel catches: row by row from the feet, a ragged, licking front; the rim a beat early
	for _, b := range body.list {
		edgeOff := 0.0
		if b.edge {
			edgeOff = 0.02
		}
		b.key = float64(body.h-1-b.my)/H + 0.08*(pixelfx.Rand(float64(b.mx), float64(b.my), 3)-0.5) + 0.06*pixelfx.Noise(float64(b.mx)*0.35, 4) - edgeOff
		b.gone = false
	}
	front := func(tt float64) float64 {
		return -0.04 + 1.1*pixelfx.Ease.InOutSine(pixelfx.Clamp((tt-dissolveIgnite)/dissolveBurn))
	}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[6], C[5], C[4], C[3]}})
	lights := &pixelfx.Lights{}
	t := 0.0
	dur := dissolveDuration
	// a life that ends before the effect does
	left := func(a, b float64) float64 { return math.Max(0.05, math.Min(R.Range(a, b), dur-0.1-t)) }
	// tongues of flame along the seam, licking up the body still standing: layered, flickering, swaying
	var tongues [6]burnSeamTongue
	for i := range tongues {
		fi := float64(i)
		tongues[i] = burnSeamTongue{x: -9 + fi*3.6 + pixelfx.Rand(fi, 1)*1.5, h: 6 + pixelfx.Rand(fi, 2)*6, s: fi * 13}
	}
	tongueLayers := [4]struct {
		lw, lh float64
		c      uint32
	}{{1, 1, C[3]}, {0.7, 0.8, C[4]}, {0.45, 0.6, C[5]}, {0.25, 0.35, C[6]}}
	tongue := func(p *pixelfx.Painter, x, y, h, n float64) {
		for _, l := range tongueLayers {
			Hh := h * l.lh
			for r := 0.0; r <= Hh; r++ {
				u := r / math.Max(1, Hh)
				half := 1.8*l.lw*math.Pow(1-u, 0.9) + pixelfx.Noise(t*9+r*0.5, n)*0.4
				cxr := x + pixelfx.Noise(t*4+r*0.25, n+7)*u*1.6
				if half < 0.3 {
					continue
				}
				for px, x1 := pixelfx.Round(cxr-half), pixelfx.Round(cxr+half); px <= x1; px++ {
					p.Px(px, pixelfx.Round(y-r), l.c)
				}
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := front(t)
			for _, b := range body.list {
				if b.gone || b.key >= f {
					continue
				}
				b.gone = true
				// a burnt pixel goes to ash that sifts down, an ember that rises, or a lick of flame
				r := R.Next()
				switch {
				case r < 0.2:
					// ash: low down it drops and settles, higher up it sifts down slowly (and is gone before the floor)
					if b.y > -7 {
						vx, vy, life, col := R.Range(-12, 12), R.Range(-20, 0), left(0.6, 1.2), pixelfx.Pick(R, A)
						ps.Spawn(pixelfx.Grit, b.x, b.y, vx, vy, life, col, 1, 0, 0)
					} else {
						vx, vy, life, col := R.Range(-10, 10), R.Range(-8, 4), left(0.5, math.Min(1.5, -b.y/18)), pixelfx.Pick(R, A)
						size := 1.0
						if R.Chance(0.2) {
							size = 1.4
						}
						ps.Spawn(pixelfx.Flake, b.x, b.y, vx, vy, life, col, size, 0, 0)
					}
				case r < 0.32:
					vx, vy, life, size := R.Range(-12, 12), -R.Range(15, 45), left(0.6, 1.4), R.Range(1, 1.6)
					ps.Spawn(pixelfx.Ember, b.x, b.y, vx, vy, life, C[5], size, 0, 0)
				case r < 0.46:
					vx, vy, life, size := R.Range(-6, 6), -R.Range(14, 30), R.Range(0.2, 0.42), R.Range(1.2, 2.2)
					var fl pixelfx.Flag
					if R.Chance(0.5) {
						fl = pixelfx.Smokes
					}
					ps.Spawn(pixelfx.Fire, b.x, b.y, vx, vy, life, 0, size, 0, fl)
				case r < 0.5:
					vx, vy, life, size := R.Range(-5, 5), -R.Range(14, 26), left(1.2, 2), R.Range(2, 3.4)
					ps.Spawn(pixelfx.Smoke, b.x, b.y-2, vx, vy, life, pixelfx.Mix(A[1], C[1], 0.3), size, 0, 0)
				}
			}
			// the feet catch with a spit of sparks and a puff of light
			if t >= dissolveIgnite && t-dt < dissolveIgnite {
				lights.Add(0, -4, 34, C[4], 0.8, 0.4)
				for range 10 {
					x, vx, vy, life := R.Range(-8, 8), R.Range(-80, 80), -R.Range(40, 150), R.Range(0.2, 0.5)
					ps.Spawn(pixelfx.Spark, x, -1, vx, vy, life, pixelfx.White, 1, 0, 0)
				}
			}
			// while it burns: steady embers and smoke off the front
			if f > 0 && f < 1.02 {
				fy := body.y + (1-pixelfx.Clamp(f))*H
				if R.Chance(0.34) {
					x, vx, vy, life, size := R.Range(-7, 7), R.Range(-6, 6), -R.Range(16, 30), left(1.4, 2.4), R.Range(2.6, 4.2)
					ps.Spawn(pixelfx.Smoke, x, fy-5, vx, vy, life, pixelfx.Mix(A[1], C[3], 0.35), size, 0, 0)
				}
				if R.Chance(0.5) {
					x, vx, vy, life, size := R.Range(-9, 9), R.Range(-6, 6), -R.Range(18, 34), R.Range(0.18, 0.36), R.Range(1.2, 2.4)
					var fl pixelfx.Flag
					if R.Chance(0.35) {
						fl = pixelfx.Smokes
					}
					ps.Spawn(pixelfx.Fire, x, fy, vx, vy, life, 0, size, 0, fl)
				}
				if R.Chance(0.15) {
					x, vx, vy, life := R.Range(-9, 9), R.Range(-15, 15), -R.Range(30, 70), left(0.8, 1.6)
					ps.Spawn(pixelfx.Ember, x, fy, vx, vy, life, C[5], 1.4, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// warm light on the floor while the body burns down to it
			k := pixelfx.Smooth(0, dissolveIgnite+0.2, t) * (1 - pixelfx.Smooth(dissolveIgnite+dissolveBurn*0.6, dissolveIgnite+dissolveBurn+0.4, t))
			if k > 0 {
				p.Glow(0, -1, 30, C[4], 0.3*k)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f, fr := front(t), math.Floor(t*30)
			for _, b := range body.list {
				if b.gone {
					continue
				}
				d := b.key - f
				var c uint32
				switch {
				// the seam: white-hot at the very edge, flickering through yellow and orange
				case d < 0.025:
					if pixelfx.Rand(float64(b.mx), float64(b.my), fr) < 0.35 {
						c = pixelfx.White
					} else {
						c = C[6]
					}
				case d < 0.05:
					if pixelfx.Rand(float64(b.mx), float64(b.my), fr+7) < 0.5 {
						c = C[5]
					} else {
						c = C[4]
					}
				// just above it the body is charred, with live sparks in the char
				case d < 0.11:
					if pixelfx.Rand(float64(b.mx), float64(b.my), 13) < 0.15 && pixelfx.Rand(float64(b.mx), fr, 2) < 0.6 {
						c = C[3]
					} else if d < 0.075 {
						c = dissolveChar[0]
					} else {
						c = dissolveChar[1]
					}
				case d < 0.16:
					c = pixelfx.Mix(b.c, dissolveChar[2], 0.6)
				// above that the heat browns it
				case d < 0.26:
					c = pixelfx.Mix(b.c, C[2], 0.18)
				default:
					c = b.c
				}
				p.Px(b.x, b.y, c)
			}
			// light along the seam, and flames licking up from it while there's body left to burn
			if f > -0.02 && f < 1.05 {
				fy, k := body.y+(1-pixelfx.Clamp(f))*H, 1-pixelfx.Smooth(0.95, 1.05, f)
				for _, g := range tongues {
					row, mx := int(pixelfx.Round(fy-body.y)), int(pixelfx.Round(g.x-body.x))
					if !body.on(mx, min(body.h-1, max(0, row))) {
						continue
					}
					h := g.h * k * (0.6 + 0.6*(pixelfx.Noise(t*2.6+g.s, g.s)*0.5+0.5)) * pixelfx.Smooth(-0.02, 0.06, f)
					if h >= 1 {
						tongue(p, g.x, fy+1, h, g.s)
					}
				}
				fl := 0.85 + 0.15*pixelfx.Noise(t*11, 9)
				p.GlowLine(-11, fy, 11, fy, 9, C[4], 0.45*k)
				p.Glow(0, fy-6, 56, C[4], 0.38*k*fl)
				p.Glow(0, fy-2, 18, C[5], 0.28*k*fl)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
