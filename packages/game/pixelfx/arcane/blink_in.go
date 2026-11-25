// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/blink-in.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The arrival (bits converging, a line of light that opens into the silhouette, a ring on the floor, a burst of stars) is adapted from Sprite Fusion's open-source blink (https://www.spritefusion.com/pixel-art-vfx/movement), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// the beats: the bits gather into a line, it grows, it opens into the caster; variables so expressions with them round step by step as JavaScript's do
var (
	blinkInLine   = 0.26
	blinkInGrow   = 0.1
	blinkInOpen   = 0.12
	blinkInArrive = blinkInLine + blinkInGrow + blinkInOpen
)

var blinkInLanded = pixelfx.Pose{}.With(pixelfx.Crouch.Is(7), pixelfx.Lean.Is(3), pixelfx.FX.Is(9), pixelfx.FY.Is(-12), pixelfx.BX.Is(12), pixelfx.BY.Is(-12), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-6))

// BlinkIn spirals glittering bits in to a point of light that opens into a line, then into a white silhouette that cools into the caster. The stand-in character appears once the silhouette has opened. It is registered as "arcane/blink-in".
var BlinkIn = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Blink in",
		Desc:     "Glittering bits spiral in to a point of light that opens into a line, then into a white silhouette that cools into the caster.",
		Tags:     []string{"arcane", "blink", "teleport", "appear", "movement", "magic", "one-shot"},
		Size:     [2]int{136, 96},
		Origin:   [2]int{68, 80},
		Duration: 1.4,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: blinkInLanded},
			{T: 0.48, Pose: blinkInLanded},
			{T: 0.56, Pose: blinkInLanded.With(pixelfx.Crouch.Is(6)), How: "snap"},
			{T: 0.95, Pose: pixelfx.Ready},
			{T: 1.4, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{{T: 0.48, Stop: 0.03, Shake: 0.1, Amp: pixelfx.Val(1)}},
		Dust: []pixelfx.DustKick{{T: 0.48, X: 0, N: 7}},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < blinkInArrive {
			return 0
		}
		return 1
	},
	New: newBlinkIn,
}

func init() { pixelfx.Register("arcane/blink-in", BlinkIn) }

type blinkInBit struct {
	tx, ty, ox, oy, t0, t1, spin float64
	c                            uint32
}

func newBlinkIn(o *pixelfx.Options) pixelfx.Instance {
	C, R, A := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Actor
	sh := shades(A)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	mx, my := A.X+float64(A.W)/2, A.Y+float64(A.H)*0.55
	cols := []uint32{pixelfx.White, C[6], C[6], C[5], C[4]}
	inside := func(x, y int) bool { return x >= 0 && y >= 0 && x < A.W && y < A.H && A.Mask.Data[y*A.W+x] != 0 }
	// the bits: each spirals in from its own spot to a point on the line, arriving as the line forms
	bits := make([]blinkInBit, 230)
	for i := range bits {
		a, d := R.Range(0, tau), R.Range(16, 46)
		ty := A.Y + R.Range(3, float64(A.H)-2)
		oy := math.Sin(a)*d*0.8 - 6
		if ty+oy > -2 {
			oy = -math.Abs(oy) - 2 // none start under the floor
		}
		b := blinkInBit{tx: mx + R.Range(-1, 1), ty: ty, ox: math.Cos(a) * d, oy: oy}
		b.t0 = R.Range(0, 0.1)
		b.t1 = blinkInLine + R.Range(-0.02, blinkInGrow)
		b.spin = R.Range(0.8, 1.8) * R.Sign()
		b.c = pixelfx.Pick(R, cols)
		bits[i] = b
	}
	t, arrived := 0.0, false
	at := func(b *blinkInBit, tt float64) (float64, float64, float64) {
		k := pixelfx.Clamp((tt - b.t0) / (b.t1 - b.t0))
		e := pixelfx.Ease.OutCubic(k)
		r := 1 - e
		sp := b.spin * r * 1.2
		ox, oy := b.ox*math.Cos(sp)-b.oy*math.Sin(sp), b.ox*math.Sin(sp)+b.oy*math.Cos(sp)
		return b.tx + ox*r, math.Min(-1, b.ty+oy*r), k
	}
	// ring is the band thrown out across the floor as the caster arrives: the far half behind, the near half in front
	ring := func(p *pixelfx.Painter, far bool) {
		k := (t - blinkInArrive + 0.03) / 0.5
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
			if t < blinkInLine && R.Chance(0.6) {
				a, D, T := R.Range(0, tau), R.Range(22, 44), R.Range(0.12, 0.18)
				v := (D * 3) / (math.Exp(3*T) - 1)
				x, y := mx+math.Cos(a)*D, math.Min(-2, my+math.Sin(a)*D)
				l := or1(pixelfx.Hypot(x-mx, y-my))
				ps.Spawn(pixelfx.Streak, x, y, ((mx-x)/l)*v, ((my-y)/l)*v, T, pick(R.Chance(0.5), C[6], C[5]), 1, 0, 0)
			}
			if !arrived && t >= blinkInArrive {
				arrived = true
				lights.Add(mx, my-6, 56, C[5], 0.9, 0.35)
				lights.Add(mx, my, 24, pixelfx.White, 0.8, 0.1)
				for range 30 {
					a, sp := R.Range(0, tau), R.Range(35, 110)
					k := pixelfx.Mote
					if R.Chance(0.6) {
						k = pixelfx.StarKind
					}
					life := R.Range(0.4, 0.85)
					c := pixelfx.Pick(R, cols)
					ps.Spawn(k, mx, my, math.Cos(a)*sp, math.Sin(a)*sp*0.7-30, life, c, R.Range(0.6, 1.6), 0, 0)
				}
				for range 12 {
					s := R.Sign()
					x := mx + s*R.Range(2, 8)
					vx := s * R.Range(60, 180)
					vy := -R.Range(20, 110)
					ps.Spawn(pixelfx.Spark, x, -1, vx, vy, R.Range(0.2, 0.45), pixelfx.White, 1, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, true)
			k := 1 - pixelfx.Clamp((t-blinkInArrive)/0.5)
			if t < blinkInArrive {
				k = t / blinkInArrive
			}
			p.Glow(0, -1, 26, C[5], 0.3*k)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the bits rushing in, streaking as they speed up, white as they land
			for i := range bits {
				b := &bits[i]
				if t < b.t0 || t > b.t1+0.02 {
					continue
				}
				x, y, k := at(b, t)
				p.Dot(x, y, pick(k > 0.75, pixelfx.White, b.c), math.Min(1, 0.35+k*2))
				// a short tail once they're rushing
				if k > 0.55 {
					px, py, _ := at(b, t-0.015)
					p.Dot((x+px)/2, (y+py)/2, C[5], 0.8)
				}
				if k > 0.8 {
					p.Glow(x, y, 3, C[6], 0.3)
				}
			}
			switch {
			case t >= blinkInLine-0.04 && t < blinkInLine:
				// a point of light where it gathers
				q := (t - blinkInLine + 0.04) / 0.04
				pixelfx.Sparkle(p, mx, my, 2+3*q, C[6], C[5], 0.6)
			case t >= blinkInLine && t < blinkInLine+blinkInGrow:
				// it opens into a line, top to bottom
				q := pixelfx.Ease.OutCubic((t - blinkInLine) / blinkInGrow)
				hh := (float64(A.H)/2 + 3) * q
				for y := -hh - 2; y <= hh+2; y++ {
					in := math.Abs(y) < hh
					p.Dot(mx, my+y, pick(in, pixelfx.White, C[6]), pickF(in, 1, 0.7))
				}
				for y := -hh * 0.7; y <= hh*0.7; y++ {
					p.Dot(mx-1, my+y, C[5], 0.6)
					p.Dot(mx+1, my+y, C[5], 0.6)
				}
				p.Glow(mx, my, 30, C[5], 0.9)
				p.Glow(mx, my, 8, pixelfx.White, 0.5)
			case t >= blinkInLine+blinkInGrow && t < blinkInArrive:
				// the line opens sideways into the silhouette, white hot
				q := pixelfx.Ease.OutCubic((t - blinkInLine - blinkInGrow) / blinkInOpen)
				squeezed(p, A, sh, q, pixelfx.White, C[6], 1, 0)
				p.Glow(mx, my, 36, C[5], 0.8)
			case t >= blinkInArrive && t < blinkInArrive+0.42:
				// the caster cools out of the white: its body full of light, an outline crackling round it, both fading
				k, f := 1-(t-blinkInArrive)/0.42, math.Floor(t*30)
				if k > 0.86 {
					squeezed(p, A, sh, 1, pixelfx.White, C[6], 1, 0)
				}
				for y := 0; y < A.H; y++ {
					for x := 0; x < A.W; x++ {
						fx, fy := float64(x), float64(y)
						if inside(x, y) {
							p.Light(A.X+fx, A.Y+fy, C[6], k*k*pickF(sh[y*A.W+x] == 1, 0.3, 0.8))
						} else if (inside(x-1, y) || inside(x+1, y) || inside(x, y-1) || inside(x, y+1)) && pixelfx.Rand(fx, fy, f) < 0.85*k {
							p.Px(A.X+fx, A.Y+fy, pick(k > 0.5 && pixelfx.Rand(fx, fy, 7) < 0.5, pixelfx.White, C[6]))
						}
					}
				}
				p.Glow(mx, my, 34, C[5], 0.6*k)
			}
			if t >= blinkInArrive && t < blinkInArrive+0.2 {
				q := (t - blinkInArrive) / 0.2
				pixelfx.Flare(p, mx, my-4, 18*math.Pow(1-q, 1.5)+2, 1-q, C)
			}
			ring(p, false)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
