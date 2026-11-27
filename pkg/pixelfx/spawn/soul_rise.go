// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/soul-rise.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source movement and heal (https://www.spritefusion.com/pixel-art-vfx/movement) for the pale ghost of the body and the motes it sheds, MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	soulDuration = 3.2
	soulSlump    = pixelfx.Pose{}.With(pixelfx.Crouch.Is(8), pixelfx.Lean.Is(6), pixelfx.HY.Is(2), pixelfx.FX.Is(7), pixelfx.FY.Is(-9), pixelfx.BX.Is(11), pixelfx.BY.Is(-9), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-8))
	soulLeave    = 0.35
	soulTop      = 2.05
	soulGone     = 2.55
	soulShadow   = pixelfx.Hex("#0c0f1e")
)

// SoulRise flares and dims the body's outline, then lifts a pale glowing copy of it out, stretching as its legs melt into a rippling wisp, shedding motes until it thins into light. It is registered as "spawn/soul-rise".
var SoulRise = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Soul rise",
		Desc:     "The body's outline flares and dims, then a pale glowing copy of it lifts out, stretching as its legs melt into a rippling wisp, shedding motes until it thins into light.",
		Tags:     []string{"spawn", "death", "soul", "ghost", "spirit", "rise", "light", "one-shot"},
		Size:     [2]int{96, 140},
		Origin:   [2]int{48, 126},
		Duration: soulDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#101a33", "#1f3563", "#35609e", "#5c96d4", "#94c8f0", "#d2f0ff", "#ffffff")},
			{Name: "height", Default: pixelfx.Num(50)},
			{Name: "seed", Default: pixelfx.Num(27)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: -0.2, Pose: pixelfx.Ready},
			{T: 0.3, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.HY.Is(-1), pixelfx.FX.Is(-10), pixelfx.FY.Is(-24), pixelfx.BX.Is(11), pixelfx.BY.Is(-24), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 0.7, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(0.7), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(3), pixelfx.HY.Is(1), pixelfx.FX.Is(2), pixelfx.FY.Is(-14), pixelfx.BX.Is(8), pixelfx.BY.Is(-14), pixelfx.FFX.Is(4), pixelfx.FBX.Is(-3))},
			{T: 1.1, Pose: soulSlump},
			{T: 2.7, Pose: soulSlump},
			{T: 3.2, Pose: soulSlump.With(pixelfx.Crouch.Is(7))},
		},
		Dust: []pixelfx.DustKick{{T: 1.1, X: 0, N: 5}},
	},
	ActorAlpha: func(float64, *pixelfx.Options) float64 { return 1 },
	New:        newSoulRise,
}

func init() { pixelfx.Register("spawn/soul-rise", SoulRise) }

func newSoulRise(o *pixelfx.Options) pixelfx.Instance {
	C, R, height := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed()), o.Num("height")
	floor := func(float64) float64 { return 0 }
	body := bodyOf(o.Actor, C)
	rim := rimOf(body)
	W, Hh := body.w, body.h
	waist := int(pixelfx.Round(float64(Hh) * 0.6))
	// the soul is the body's own picture in pale light: each pixel's brightness picks a step of the ramp
	toneAt := make([]int8, W*Hh)
	for i := range toneAt {
		toneAt[i] = -1
	}
	for _, b := range body.list {
		ti := int8(3 + int(math.Min(2, math.Floor(pixelfx.Luma(b.c)*3.6))))
		if b.edge {
			ti = 6
		}
		toneAt[b.my*W+b.mx] = ti
	}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[5], C[4], C[3]}})
	lights := &pixelfx.Lights{}
	t, left := 0.0, false
	dur := soulDuration
	starCols1 := []uint32{pixelfx.White, C[5]}
	starCols2 := []uint32{pixelfx.White, C[5], C[5], C[4]}

	// where the soul is: lifted, stretched taller and thinner, swaying; legs melt into a wisp below the waist
	lift := func() float64 {
		return height * pixelfx.Ease.InOutSine(pixelfx.Clamp((t-soulLeave)/(soulTop-soulLeave)))
	}
	stretch := func() float64 {
		return 1 + 0.22*pixelfx.Ease.InOutSine(pixelfx.Clamp((t-soulLeave)/(soulTop-soulLeave)))
	}
	sway := func(row float64) float64 {
		return math.Sin(t*3-row*0.12) * 2.2 * pixelfx.Smooth(soulLeave, soulLeave+0.5, t)
	}
	melt := func() float64 { return pixelfx.Smooth(soulLeave+0.1, soulLeave+0.9, t) }
	// it thins from the bottom up once it's high
	thin := func() float64 { return pixelfx.Smooth(soulTop-0.25, soulGone, t) }

	// a ring of light across the floor as it tears loose: far half behind the body, near half in front
	ring := func(p *pixelfx.Painter, near bool) {
		k := (t - soulLeave) / 0.5
		if k > 0 && k < 1 {
			pixelfx.Band(p, 0, 0, 6+26*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 3*(1-k)), 1.15-k, C, func(g float64) bool { return (math.Sin(g) >= 0) == near }, k < 0.12)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !left && t >= soulLeave {
				left = true
				lights.Add(0, -20, 50, C[4], 0.85, 0.55)
				lights.Add(0, -20, 24, pixelfx.White, 0.8, 0.12)
				for range 10 {
					c := pixelfx.Pick(R, rim)
					vx, vy, life, col, size := R.Range(-20, 20), -R.Range(20, 60), R.Range(0.4, 0.8), pixelfx.Pick(R, starCols1), R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, c.x, c.y, vx, vy, life, col, size, 0, 0)
				}
			}
			// motes come off its edges as it rises, and pour off it as it thins
			if t > soulLeave && t < soulGone {
				s, y0, th := stretch(), body.y*stretch()-lift(), thin()
				count, prob := 1, 0.5
				if th > 0 {
					count, prob = 3, 0.9
				}
				for range count {
					if !R.Chance(prob) {
						continue
					}
					c := pixelfx.Pick(R, rim)
					row := c.my
					if float64(row)/float64(Hh) < 1-th*1.1-0.05 && th > 0 {
						continue
					}
					x := c.x*(1-0.12*(s-1)*4) + sway(float64(row))
					y := y0 + float64(c.my)*s
					// nothing outlives the effect, and nothing climbs off the top
					life := math.Min(R.Range(0.5, 1.1), math.Min(dur-0.1-t, (y+120)/60))
					if life > 0.1 {
						kind := pixelfx.StarKind
						if R.Chance(0.7) {
							kind = pixelfx.Mote
						}
						vx, vy, col, size := R.Range(-12, 12), -R.Range(25, 60), pixelfx.Pick(R, starCols2), R.Range(0.5, 1)
						ps.Spawn(kind, x, y, vx, vy, life, col, size, 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a pale pool on the floor while it leaves, and a ring from the moment it tears loose
			k := pixelfx.Smooth(0, soulLeave, t) * (1 - pixelfx.Smooth(soulLeave+0.4, soulTop, t))
			if k > 0 {
				p.Glow(0, -1, 26, C[4], 0.3*k)
			}
			ring(p, false)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the body: its outline flares as the soul tears loose, then the body is left dimmed
			if t < soulLeave+0.3 {
				k := t / soulLeave
				if t >= soulLeave {
					k = 1 - (t-soulLeave)/0.3
				}
				fr := math.Floor(t * 30)
				col := C[5]
				if k > 0.7 {
					col = pixelfx.White
				}
				for _, c := range rim {
					if pixelfx.Rand(float64(c.mx), float64(c.my), fr) < 0.25+0.7*k {
						p.Px(c.x, c.y, col)
					}
				}
				p.Glow(0, -19, 26, C[4], 0.45*k)
			}
			dim := pixelfx.Smooth(soulLeave, soulLeave+0.6, t) * (1 - pixelfx.Smooth(soulGone, dur-0.1, t))
			if dim > 0 {
				for _, b := range body.list {
					p.Blend(b.x, b.y, soulShadow, 0.45*dim)
				}
			}
			ring(p, true)
			if t < soulLeave || t > soulGone+0.1 {
				ps.Draw(p, pixelfx.LayerBoth)
				lights.Draw(p)
				return
			}
			// the soul, drawn backward from the screen so stretching leaves no holes; see-through, its rim solid
			s, L, m, th := stretch(), lift(), melt(), thin()
			top, sx := body.y*s-L, 1-0.12*(s-1)*4
			tail := int(pixelfx.Round(10 * m))
			y1 := top + float64(Hh+tail)*s
			fadeIn := pixelfx.Clamp((t - soulLeave) / 0.15)
			for y := math.Floor(top); y <= math.Ceil(y1); y++ {
				my := int(math.Floor((y + 0.5 - top) / s))
				if my < 0 {
					continue
				}
				// rows below the waist pull in toward a wavy spine: legs melting into a wisp
				k := 0.0
				if my >= waist {
					k = float64(my-waist+1) / float64(Hh-waist+tail)
				}
				narrow := 1 - k*m*0.95
				if narrow <= 0.05 {
					continue
				}
				cx := sway(float64(my)) + math.Sin(float64(my)*0.7-t*8)*2*k*m
				a := fadeIn
				if my >= waist {
					a = (1 - k*0.45) * fadeIn
				}
				for x := math.Floor(cx - float64(W)/2 - 4); x <= math.Ceil(cx+float64(W)/2+4); x++ {
					mx := int(math.Floor((x+0.5-cx)/(sx*narrow) - body.x))
					// as it thins the lower rows go first, with a ragged edge
					if th > 0 && float64(my)/float64(Hh+tail) > 1-th*1.25+0.25*pixelfx.Rand(float64(mx), float64(my), 5)-0.1 {
						continue
					}
					var ti int8
					if my < Hh {
						if mx < 0 || mx >= W {
							continue
						}
						ti = toneAt[my*W+mx]
						if ti < 0 {
							continue
						}
					} else {
						// the wisp below the feet: a thin trail that tapers to nothing
						w := (1 - float64(my-Hh)/math.Max(1, float64(tail))) * 3.2 * m
						d := math.Abs(x + 0.5 - cx)
						if d > w {
							continue
						}
						if d > w-1 {
							ti = 5
						} else {
							ti = 4
						}
					}
					// the first frames it tears loose it is all white
					if t < soulLeave+0.07 {
						col := pixelfx.White
						if ti >= 6 {
							col = C[5]
						}
						p.Px(x, y, col)
					} else if ti >= 5 {
						col := C[5]
						if ti >= 6 {
							col = pixelfx.White
						}
						p.Dot(x, y, col, math.Min(1, a*1.3))
					} else {
						p.Blend(x, y, C[ti], a*0.7)
					}
				}
			}
			// its light
			cy := top + float64(Hh)*s*0.45
			p.Glow(sway(10), cy, 30, C[4], 0.42*fadeIn*(1-th))
			p.Glow(sway(10), cy-6, 14, C[5], 0.3*fadeIn*(1-th))
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
