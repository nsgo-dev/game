// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/blink-out.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The blink (a crackling outline as it gathers, a white flash, the silhouette squeezed into a line, a ring on the floor, a fading afterimage) is adapted from Sprite Fusion's open-source blink (https://www.spritefusion.com/pixel-art-vfx/movement), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// the beats: the caster gathers light, is squeezed into a line, the line collapses and pops; variables so expressions with them round step by step as JavaScript's do
var (
	blinkOutOut      = 0.32
	blinkOutSqueeze  = 0.12
	blinkOutCollapse = 0.08
	blinkOutPop      = blinkOutOut + blinkOutSqueeze + blinkOutCollapse
)

var blinkOutIn = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(4), pixelfx.FX.Is(-2), pixelfx.FY.Is(-25), pixelfx.BX.Is(2), pixelfx.BY.Is(-25), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(0), pixelfx.FBX.Is(2))

// BlinkOut makes the caster crackle with gathering light, flash white and squeeze into a line of light that collapses and pops away in sparks. The stand-in character is hidden from the flash on, when the effect draws it itself. It is registered as "arcane/blink-out".
var BlinkOut = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Blink out",
		Desc:     "The caster crackles with gathering light, flashes white, squeezes into a line of light that collapses and pops away in sparks.",
		Tags:     []string{"arcane", "blink", "teleport", "vanish", "movement", "magic", "one-shot"},
		Size:     [2]int{136, 96},
		Origin:   [2]int{68, 80},
		Duration: 1.6,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.36, Pose: pixelfx.Ready},
			{T: -0.1, Pose: blinkOutIn},
			{T: 0.2, Pose: blinkOutIn.With(pixelfx.Crouch.Is(5))},
			{T: 0.32, Pose: blinkOutIn.With(pixelfx.Crouch.Is(0)), How: "snap"},
			{T: 1.6, Pose: blinkOutIn.With(pixelfx.Crouch.Is(0))},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < blinkOutOut {
			return 1
		}
		return 0
	},
	New: newBlinkOut,
}

func init() { pixelfx.Register("arcane/blink-out", BlinkOut) }

func newBlinkOut(o *pixelfx.Options) pixelfx.Instance {
	C, R, A := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Actor
	sh := shades(A)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	mx, my := A.X+float64(A.W)/2, A.Y+float64(A.H)*0.55
	cols := []uint32{pixelfx.White, C[6], C[6], C[5], C[4]}
	inside := func(x, y int) bool { return x >= 0 && y >= 0 && x < A.W && y < A.H && A.Mask.Data[y*A.W+x] != 0 }
	t, out, popped := 0.0, false, false
	// ring is the band thrown out across the floor as it pops: the far half behind, the near half in front
	ring := func(p *pixelfx.Painter, far bool) {
		k := (t - blinkOutPop) / 0.5
		if k >= 0 && k < 1 {
			keep := func(g float64) bool { return math.Sin(g) >= 0 }
			if far {
				keep = func(g float64) bool { return math.Sin(g) < 0 }
			}
			pixelfx.Band(p, 0, 0, 4+30*pixelfx.Ease.OutCubic(k), 0.2, math.Max(1, 3*(1-k)), 1.15-k, []uint32{C[3], C[5], C[6], pixelfx.White}, keep, k < 0.25)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// gathering: streaks rush in to the chest
			if t < blinkOutOut-0.04 && R.Chance(0.7) {
				a, D, T := R.Range(0, tau), R.Range(22, 44), R.Range(0.12, 0.18)
				v := (D * 3) / (math.Exp(3*T) - 1)
				x, y := mx+math.Cos(a)*D, math.Min(-2, my+math.Sin(a)*D)
				l := or1(pixelfx.Hypot(x-mx, y-my))
				ps.Spawn(pixelfx.Streak, x, y, ((mx-x)/l)*v, ((my-y)/l)*v, T, pick(R.Chance(0.5), C[6], C[5]), 1, 0, 0)
			}
			if !out && t >= blinkOutOut {
				out = true
				lights.Add(mx, my-6, 54, C[5], 0.9, 0.3)
				lights.Add(mx, my, 26, pixelfx.White, 0.8, 0.1)
			}
			if !popped && t >= blinkOutPop {
				popped = true
				lights.Add(mx, my-6, 58, C[5], 0.9, 0.35)
				lights.Add(mx, my, 24, pixelfx.White, 0.9, 0.08)
				// the line bursts: bits of the character fly off and drift up, sparks and stars scatter
				for range 110 {
					y, s := A.Y+R.Range(2, float64(A.H)-2), R.Sign()
					x := mx + R.Range(-1, 1)
					vx := s * R.Range(25, 105)
					vy := R.Range(-70, 10)
					life := R.Range(0.5, 0.95)
					c := pixelfx.Pick(R, cols)
					ps.Spawn(pixelfx.Ember, x, y, vx, vy, life, c, pickF(R.Chance(0.3), 2, 1), 0, 0)
				}
				for range 18 {
					a, sp := R.Range(0, tau), R.Range(80, 240)
					ps.Spawn(pixelfx.Spark, mx, my, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
				}
				for i := range 14 {
					a, sp := R.Range(0, tau), R.Range(20, 110)
					k := pixelfx.Mote
					if i%2 != 0 {
						k = pixelfx.StarKind
					}
					life := R.Range(0.4, 0.9)
					c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
					ps.Spawn(k, mx, my, math.Cos(a)*sp, math.Sin(a)*sp*0.8-20, life, c, R.Range(0.8, 1.8), 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the afterimage it leaves: a translucent violet ghost that thins away
			if t > blinkOutPop && t < blinkOutPop+0.6 {
				k := 0.5 * (1 - (t-blinkOutPop)/0.6)
				for y := 0; y < A.H; y++ {
					for x := 0; x < A.W; x++ {
						if v := sh[y*A.W+x]; v != 0 {
							p.Blend(A.X+float64(x), A.Y+float64(y), pick(v == 1, C[3], C[4]), k)
						}
					}
				}
			}
			ring(p, true)
			if t < blinkOutPop+0.3 {
				k := 1 - pixelfx.Clamp((t-blinkOutPop)/0.3)
				if t < blinkOutOut {
					k = t / blinkOutOut
				}
				p.Glow(0, -1, 26, C[5], 0.3*k)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f := math.Floor(t * 30)
			switch {
			case t < blinkOutOut:
				// the body brightens and an outline of light crackles round it, thicker as it gathers
				k := t / blinkOutOut
				for y := 0; y < A.H; y++ {
					for x := 0; x < A.W; x++ {
						fx, fy := float64(x), float64(y)
						if inside(x, y) {
							// the body fills with light: its lit pixels most
							p.Light(A.X+fx, A.Y+fy, C[6], k*k*pickF(sh[y*A.W+x] == 1, 0.3, 0.75))
						} else if (inside(x-1, y) || inside(x+1, y) || inside(x, y-1) || inside(x, y+1)) && pixelfx.Rand(fx, fy, f) < 0.25+0.65*k {
							p.Px(A.X+fx, A.Y+fy, pick(k > 0.6 && pixelfx.Rand(fx, fy, 7) < 0.5, pixelfx.White, C[6]))
						}
					}
				}
				p.Glow(mx, my, 30, C[5], 0.5*k)
			case t < blinkOutOut+0.05:
				squeezed(p, A, sh, 1, pixelfx.White, C[6], 1, 0)
				p.Glow(mx, my, 40, C[5], 0.8)
			case t < blinkOutOut+blinkOutSqueeze:
				// squeezed into a line
				q := pixelfx.Ease.InCubic((t - blinkOutOut - 0.05) / (blinkOutSqueeze - 0.05))
				squeezed(p, A, sh, 1-q, pixelfx.White, C[6], 1, 0)
				p.Glow(mx, my, 34, C[5], 0.8)
			case t < blinkOutPop:
				// the line shrinks to a point
				q := (t - blinkOutOut - blinkOutSqueeze) / blinkOutCollapse
				hh := (float64(A.H)/2 + 2) * (1 - pixelfx.Ease.InQuad(q))
				for y := -hh - 3; y <= hh+3; y++ {
					in := math.Abs(y) < hh
					p.Dot(mx, my+y, pick(in, pixelfx.White, C[6]), pickF(in, 1, 0.7))
				}
				if hh > 4 {
					for y := -hh * 0.7; y <= hh*0.7; y++ {
						p.Dot(mx-1, my+y, C[5], 0.6)
						p.Dot(mx+1, my+y, C[5], 0.6)
					}
				}
				p.Glow(mx, my, 30, C[5], 0.9)
				p.Glow(mx, my, 8, pixelfx.White, 0.5)
			}
			if t >= blinkOutPop && t < blinkOutPop+0.22 {
				q := (t - blinkOutPop) / 0.22
				pixelfx.Flare(p, mx, my, 22*math.Pow(1-q, 1.5)+2, 1-q, C)
				if q < 0.3 {
					p.Circle(mx, my, 4*(1-q/0.3)+1, pixelfx.White, 1)
				}
			}
			ring(p, false)
			if t > blinkOutPop && t < blinkOutPop+0.3 {
				pixelfx.Sparkle(p, mx, my-1, 3, C[6], C[5], 0.3*(1-(t-blinkOutPop)/0.3))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
