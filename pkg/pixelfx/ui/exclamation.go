// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/exclamation.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	exclStartled = pixelfx.Pose{}.With(
		pixelfx.Turn.Is(1), pixelfx.Y.Is(-5), pixelfx.Crouch.Is(-1),
		pixelfx.FX.Is(-14), pixelfx.FY.Is(-34), pixelfx.BX.Is(15), pixelfx.BY.Is(-34),
		pixelfx.FFX.Is(-3), pixelfx.FFY.Is(-6), pixelfx.FBX.Is(4), pixelfx.FBY.Is(-6),
	)
	exclHold = pixelfx.Pose{}.With(
		pixelfx.Turn.Is(1),
		pixelfx.FX.Is(-10), pixelfx.FY.Is(-23), pixelfx.BX.Is(11), pixelfx.BY.Is(-23),
		pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4),
	)
)

// Exclamation springs a bold "!" up off the hero's head, stretching as it shoots up and squashing as it lands, with surprise ticks and a wobble before popping away. It is registered as "ui/exclamation".
var Exclamation = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Exclamation",
		Desc:     "A bold ! springs up off the hero's head, stretching as it shoots up and squashing as it lands; surprise ticks flick out, it wobbles, then pops away.",
		Tags:     []string{"ui", "alert", "surprise", "emote", "character", "one-shot"},
		Size:     [2]int{84, 104},
		Origin:   [2]int{42, 92},
		Duration: exclDuration,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1405", "#6a2e06", "#b85a0a", "#f08c14", "#ffc23a", "#ffe580", "#fff6c8", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: -0.08, Pose: pixelfx.Ready},
			{T: 0, Pose: exclStartled, How: "snap"},
			{T: 0.14, Pose: exclStartled.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(4), pixelfx.FFY.Is(-4), pixelfx.FBY.Is(-4), pixelfx.FY.Is(-26), pixelfx.BY.Is(-26)), How: "in"},
			{T: 0.3, Pose: exclHold},
			{T: 1.1, Pose: exclHold},
			{T: 1.4, Pose: pixelfx.Ready},
		},
		Dust: []pixelfx.DustKick{{T: 0.14, X: 0, N: 4}},
	},
	New: newExclamation,
}

func init() { pixelfx.Register("ui/exclamation", Exclamation) }

var (
	exclDuration = 1.4
	exclHead     = -40.0
	exclRest     = -45.0
	exclPop      = 1.1
)

func drawBang(p *pixelfx.Painter, C []uint32, x, y, sx, sy, a float64, white bool) {
	ca, sa := math.Cos(a), math.Sin(a)
	hw := func(V float64) float64 { return 2.3 + (-V-10)*0.09 }
	inBar := func(U, V float64) bool { return V > -24 && V < -9.5 && math.Abs(U) < hw(V) }
	inDot := func(U, V float64) bool { return pixelfx.Hypot(U, V+3.6) < 3.3 }
	inside := func(U, V float64) bool { return inBar(U, V) || inDot(U, V) }
	p.Shade(x-16, y-34, x+16, y+6, func(px, py float64, _ *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		u, v := (dx*ca+dy*sa)/sx, (-dx*sa+dy*ca)/sy
		ex, ey := 1/sx, 1/sy
		if !inside(u, v) {
			if inside(u-ex, v) || inside(u+ex, v) || inside(u, v-ey) || inside(u, v+ey) {
				return C[0]
			}
			return 0
		}
		if white {
			return pixelfx.White
		}
		if inDot(u, v) {
			if pixelfx.Hypot(u+1.2, v+4.8) < 1.2 {
				return pixelfx.White
			}
			if u+v+3.6 < -0.5 {
				return C[6]
			}
			if u+v+3.6 < 1.5 {
				return C[5]
			}
			return C[4]
		}
		w := hw(v)
		q := (u + w) / (2 * w)
		if q < 0.28 {
			if v < -20 {
				return pixelfx.White
			}
			return C[7]
		}
		if q > 0.74 {
			return C[4]
		}
		if v < -18 {
			return C[6]
		}
		return C[5]
	})
}

func newExclamation(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	lights := &pixelfx.Lights{}
	my, mvy, msq, mlanded := exclHead, -420.0, 0.0, false
	t, popped, sprung := 0.0, false, false
	end := exclDuration - 0.05

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < exclPop {
				mvy += (-(my-exclRest)*900 - mvy*14) * dt
				my += mvy * dt
				if !mlanded && mvy > 0 {
					mlanded = true
					msq = 1
				}
			}
			msq = math.Max(0, msq-dt/0.12)
			if !sprung && t >= 0.04 {
				sprung = true
				lights.Add(0, exclHead-4, 26, C[5], 0.6, 0.25)
				for i := range 8 {
					an := -pi/2 + R.Range(-1.3, 1.3)
					sp := R.Range(40, 90)
					px := R.Range(-4, 4)
					life := R.Range(0.2, 0.4)
					col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					size := 0.6
					if i%3 == 0 {
						size = 1.3
					}
					ps.Spawn(pixelfx.StarKind, px, exclHead-2, math.Cos(an)*sp, math.Sin(an)*sp, life, col, size, 0, 0)
				}
			}
			if !popped && t >= exclPop+0.05 {
				popped = true
				lights.Add(0, exclRest-13, 28, C[5], 0.5, 0.2)
				for i := range 7 {
					an := (float64(i)/7)*tau + R.Range(-0.2, 0.2)
					sp := R.Range(30, 60)
					life := math.Min(R.Range(0.2, 0.35), end-t)
					col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					size := 0.6
					if i%3 == 0 {
						size = 1.4
					}
					ps.Spawn(pixelfx.StarKind, math.Cos(an)*5, exclRest-13+math.Sin(an)*9, math.Cos(an)*sp, math.Sin(an)*sp, life, col, size, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			k := t
			for _, beat := range [2][2]float64{{0.04, 6}, {0.48, 4.5}} {
				u := (k - beat[0]) / 0.24
				if u <= 0 || u >= 1 {
					continue
				}
				for _, s := range [2]float64{-1, 1} {
					for i := range 3 {
						an := -pi/2 + s*(0.55+float64(i)*0.42)
						r0 := 14 + 7*pixelfx.Ease.OutCubic(u)
						mult := 0.75
						if i == 1 {
							mult = 1
						}
						L := beat[1] * math.Sin(u*pi) * mult
						cx, cy := 0.0, exclRest-13
						ca, sa := math.Cos(an), math.Sin(an)
						if L < 0.8 {
							continue
						}
						p.LineFn(cx+ca*r0, cy+sa*r0, cx+ca*(r0+L), cy+sa*(r0+L), func(q float64) (uint32, float64) {
							if q < 0.5 {
								return pixelfx.White, 1
							}
							return C[5], 1
						})
					}
				}
			}
			pu := (k - exclPop) / 0.06
			if pu < 1 {
				speed := math.Min(1, math.Abs(mvy)/420)
				up := 0.0
				if mvy < 0 {
					up = speed
				}
				sx := 1 - 0.3*up + 0.3*msq
				sy := 1 + 0.45*up - 0.28*msq
				if k < 0.05 {
					sx *= k / 0.05
					sy *= k / 0.05
				}
				if pu > 0 {
					sx = 1 + 0.3*pu
					sy = sx
				}
				wob := 0.28 * math.Exp(-k*4) * math.Sin(k*26)
				white := k < 0.06 || pu > 0
				if sx > 0.1 {
					drawBang(p, C, 0, my, sx, sy, wob, white)
				}
				extra := 0.0
				if white {
					extra = 0.3
				}
				p.Glow(0, my-13, 20, C[5], 0.35+extra)
				if k > 0.6 && k < 0.85 {
					pixelfx.Sparkle(p, -3, my-22, 3.5*math.Sin(((k-0.6)/0.25)*pi), C[7], C[6], 0.4)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
