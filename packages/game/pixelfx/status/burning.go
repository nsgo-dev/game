// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/burning.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame tongues, embers and thin smoke from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const (
	burningDuration = 1.2
	burningTick     = 0.6
	burningWind     = -14.0
)

var (
	burningA = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.Lean.Is(2), pixelfx.FX.Is(-13), pixelfx.FY.Is(-38), pixelfx.BX.Is(12), pixelfx.BY.Is(-17), pixelfx.FFX.Is(-3), pixelfx.FFY.Is(-7), pixelfx.FBX.Is(5))
	burningB = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(-2), pixelfx.FX.Is(-12), pixelfx.FY.Is(-17), pixelfx.BX.Is(13), pixelfx.BY.Is(-38), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(4), pixelfx.FBY.Is(-7))
)

// Burning is burning over time: small flames lick from four hot spots, trailing sparks and thin smoke; every 0.6 s they flare, the body flashes white then orange, scorch marks spread. It is registered as "status/burning".
var Burning = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Burning",
		Desc:     "Burning over time: small flames lick from four hot spots, trailing sparks and thin smoke; every 0.6 s they flare, the body flashes white then orange, scorch marks spread.",
		Tags:     []string{"status", "fire", "burn", "dot", "debuff", "loop"},
		Size:     [2]int{72, 80},
		Origin:   [2]int{36, 70},
		Duration: burningDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "char", Default: pixelfx.Palette("#120c0a", "#2a1a14", "#4a2c1c")},
			{Name: "smoke", Default: pixelfx.Color("#5b5566")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: burningA},
			{T: 0.15, Pose: burningB, How: "snap"},
			{T: 0.3, Pose: burningA, How: "snap"},
			{T: 0.45, Pose: burningB, How: "snap"},
			{T: 0.6, Pose: burningA, How: "snap"},
			{T: 0.75, Pose: burningB, How: "snap"},
			{T: 0.9, Pose: burningA, How: "snap"},
			{T: 1.05, Pose: burningB, How: "snap"},
			{T: 1.2, Pose: burningA, How: "snap"},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if burningSince(t) < 0.034 {
			return 0
		}
		return 1
	},
	New: newBurning,
}

func init() { pixelfx.Register("status/burning", Burning) }

func burningSince(t float64) float64 { return pixelfx.Wrap(t, burningTick) }
func burningFlare(t float64) float64 { return math.Exp(-burningSince(t) / 0.12) }

type burnSpotTongue struct {
	dx, h, n float64
}

type burnSpot struct {
	x, y    float64
	side    int
	n       int
	h       float64
	tongues []burnSpotTongue
}

type scorchMark struct {
	x, y, r float64
}

type burnSpots struct {
	list   [4]burnSpot
	scorch [2][2]scorchMark
}

func burnSpotsOf(a *pixelfx.Actor, S *actorShape, seed float64) burnSpots {
	H, W := S.h, S.w
	R := pixelfx.NewRng(seed*13 + 5)
	at := func(q float64, side int) [2]int {
		my := int(pixelfx.Round(float64(H) * q))
		row := S.rows[my]
		if side < 0 {
			return [2]int{row[0], my}
		}
		return [2]int{row[1], my}
	}
	var hairCand [][2]int
	for _, pt := range S.up {
		if float64(pt[0]) < float64(W)*0.42 {
			hairCand = append(hairCand, pt)
		}
	}
	slices.SortStableFunc(hairCand, func(p, q [2]int) int {
		if p[1] != q[1] {
			return p[1] - q[1]
		}
		return p[0] - q[0]
	})
	hair := hairCand[0]

	var shCand [][2]int
	for _, pt := range S.up {
		if float64(pt[0]) > float64(W)/2 && float64(pt[1]) > float64(H)*0.4 && float64(pt[1]) < float64(H)*0.62 {
			shCand = append(shCand, pt)
		}
	}
	slices.SortStableFunc(shCand, func(p, q [2]int) int { return p[1] - q[1] })
	shoulder := at(0.5, 1)
	if len(shCand) > 0 {
		shoulder = shCand[0]
	}

	raw := [4]struct {
		at   [2]int
		side int
		n    int
		h    float64
	}{
		{hair, -1, 3, 4.5},
		{shoulder, 1, 2, 4},
		{at(0.62, -1), -1, 2, 3.5},
		{at(0.6, 1), 1, 2, 3.5},
	}
	var out burnSpots
	for i, sp := range raw {
		ts := make([]burnSpotTongue, sp.n)
		for j := 0; j < sp.n; j++ {
			dx := (float64(j)-float64(sp.n-1)/2)*1.6 + R.Range(-0.4, 0.4)
			h := sp.h * R.Range(0.8, 1.2)
			ts[j] = burnSpotTongue{dx: dx, h: h, n: float64(i*17+j*5) + seed}
		}
		out.list[i] = burnSpot{
			x:       a.X + float64(sp.at[0]) + 0.5,
			y:       a.Y + float64(sp.at[1]) + 1.5,
			side:    sp.side,
			n:       sp.n,
			h:       sp.h,
			tongues: ts,
		}
	}
	for tick := 0; tick < 2; tick++ {
		for k := 0; k < 2; k++ {
			sp := out.list[(tick*2+k+1)%len(out.list)]
			var cand [][2]int
			for _, pt := range S.body {
				x, y := pt[0], pt[1]
				if pixelfx.Hypot(a.X+float64(x)+0.5-sp.x, a.Y+float64(y)+0.5-sp.y) < 9 && float64(y) > float64(H)*0.42 {
					l := pixelfx.Luma(colorAt(a, x, y))
					if l > 0.3 && l < 0.6 {
						cand = append(cand, pt)
					}
				}
			}
			if len(cand) == 0 {
				for _, pt := range S.body {
					if float64(pt[1]) > float64(H)*0.45 {
						cand = append(cand, pt)
					}
				}
			}
			pt := cand[int(math.Floor(R.Next()*float64(len(cand))))]
			out.scorch[tick][k] = scorchMark{
				x: a.X + float64(pt[0]) + 0.5,
				y: a.Y + float64(pt[1]) + 0.5,
				r: R.Range(3.2, 4.4),
			}
		}
	}
	return out
}

func newBurning(o *pixelfx.Options) pixelfx.Instance {
	F := hotFirst(pixelfx.Fit(o.Palette(), 9))
	CH := pixelfx.Fit(o.Ramp("char"), 3)
	smokeCol := o.Color("smoke")
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	bs := burnSpotsOf(a, S, seed)
	D := burningDuration
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			fl := burningFlare(t)
			flick := 0.8 + 0.2*tnoise(t, D, 7.5, 3, 0)
			p.Shade(-30, -3, 30, 7, func(x, y float64, out *pixelfx.Out) uint32 {
				d := pixelfx.Hypot(x/26, (y-0.5)/4.5)
				if d < 1 {
					out.L = pixelfx.Mix(F[3], F[4], 0.5)
					out.K = (0.16 + 0.22*fl) * flick * (1 - d) * (1 - d)
				}
				return 0
			})
			smokeC := pixelfx.Mix(smokeCol, F[6], 0.2)
			pixelfx.Cycle(t, D, 16, seed+4, func(r *pixelfx.Rng) float64 { return r.Range(0.8, 1.2) }, func(age, life float64, r *pixelfx.Rng, i int) {
				sp := bs.list[i%len(bs.list)]
				x := sp.x + travel(burningWind*0.6, burningWind*0.5, 0.8, age) + math.Sin(age*4+float64(i))*1.2
				y := sp.y - 3 - travel(18, 6, 1, age)
				smoke(p, x, y, r.Range(1.1, 1.9), age/life, smokeC, 0.9, 0)
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := burningSince(t)
			tick := int(math.Floor(t/burningTick)) % 2
			fl := burningFlare(t)
			if ph < 0.034 {
				for _, pt := range S.body {
					p.Px(a.X+float64(pt[0]), a.Y+float64(pt[1]), pixelfx.White)
				}
			} else if ph < 0.22 {
				k := 1 - (ph-0.034)/0.186
				for _, pt := range S.body {
					p.Blend(a.X+float64(pt[0]), a.Y+float64(pt[1]), F[4], 0.55*k)
				}
			}
			for _, sc := range bs.scorch[tick] {
				grow := pixelfx.Clamp(ph / 0.14)
				fade := 1 - pixelfx.Clamp((ph-0.25)/0.33)
				r := sc.r * (0.4 + 0.6*grow)
				if fade <= 0 {
					continue
				}
				for _, pt := range S.body {
					mx, my := pt[0], pt[1]
					x, y := a.X+float64(mx)+0.5, a.Y+float64(my)+0.5
					d := pixelfx.Hypot(x-sc.x, (y-sc.y)*1.2) + pixelfx.Rand(float64(mx), float64(my), 7)*0.9
					if d > r {
						continue
					}
					if d > r-1 {
						col := F[4]
						if pixelfx.Rand(float64(mx), float64(my), math.Floor(t*20)) < 0.5 {
							col = F[3]
						}
						p.Dot(x-0.5, y-0.5, col, fade*1.2)
					} else {
						col := CH[1]
						if d < r*0.5 {
							col = CH[0]
						}
						p.Dot(x-0.5, y-0.5, col, fade*1.1)
					}
				}
				p.Glow(sc.x, sc.y, 6, F[4], 0.3*fade)
			}
			for _, sp := range bs.list {
				for _, tg := range sp.tongues {
					h := tg.h * (0.85 + 0.25*tnoise(t, D, 5, tg.n, 0)) * (1 + 1.6*fl)
					tongue(p, sp.x+tg.dx, sp.y, h, 1.1+0.3*fl, burningWind*0.08*h/4+tnoise(t, D, 3, tg.n+9, 0)*0.8, F, t, D, tg.n, nil)
				}
				p.Glow(sp.x, sp.y-2, 9+6*fl, F[4], 0.22+0.35*fl)
			}
			pixelfx.Cycle(t, D, 20, seed+6, func(r *pixelfx.Rng) float64 { return r.Range(0.5, 0.9) }, func(age, life float64, r *pixelfx.Rng, i int) {
				sp := bs.list[i%len(bs.list)]
				x := sp.x + r.Range(-1.5, 1.5) + travel(burningWind+r.Range(-10, 6), burningWind*0.6, 1.2, age) + math.Sin(age*9+float64(i))*1.2*age
				y := sp.y - 2 + travel(-r.Range(25, 55), -18, 1.4, age)
				ember(p, x, y, age/life, [3]uint32{F[1], F[3], F[5]}, age+float64(i), 1)
			})
			if ph < 0.3 {
				for k, sp := range bs.list {
					for j := 0; j < 4; j++ {
						an := -pi/2 + (pixelfx.Rand(float64(k), float64(j), float64(tick))-0.5)*2.2
						v := 50 + 50*pixelfx.Rand(float64(k), float64(j), 2)
						x := sp.x + math.Cos(an)*travel(v, 0, 3, ph)
						y := sp.y - 2 + math.Sin(an)*travel(v, 0, 3, ph) + 60*ph*ph
						col := F[3]
						if ph < 0.12 {
							col = F[1]
						}
						p.Px(x, y, col)
						p.Light(x, y, F[3], 0.4*(1-ph/0.3))
					}
				}
			}
		},
	}
}
