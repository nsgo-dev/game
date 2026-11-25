// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/glitch.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	glitchTwitchA = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.3), pixelfx.Lean.Is(3), pixelfx.Crouch.Is(1), pixelfx.FX.Is(13), pixelfx.FY.Is(-31), pixelfx.BX.Is(6), pixelfx.BY.Is(-14), pixelfx.HX.Is(1))
	glitchTwitchB = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.2), pixelfx.Lean.Is(-3), pixelfx.Crouch.Is(0), pixelfx.FX.Is(-9), pixelfx.FY.Is(-14), pixelfx.BX.Is(14), pixelfx.BY.Is(-31), pixelfx.HX.Is(-1))
	glitchFine    = pixelfx.Pose{}
)

// Glitch breaks up the hero's signal in bursts: an RGB split, slices torn sideways, rows smeared out, junk blocks and dead pixels, then snaps back clean. It is registered as "scifi/glitch".
var Glitch = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Glitch",
		Desc:     "The hero's signal breaks up in bursts: an RGB split, slices torn sideways, rows smeared out, junk blocks and dead pixels, then snaps back clean.",
		Tags:     []string{"scifi", "glitch", "digital", "corrupt", "hologram", "status", "loop"},
		Size:     [2]int{88, 64},
		Origin:   [2]int{44, 54},
		Duration: glitchDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#14101f", "#3b1f5c", "#ff2e63", "#ff7aa8", "#2ef2ff", "#b4fbff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: glitchFine},
			{T: 0.3, Pose: glitchTwitchA, How: "snap"},
			{T: 0.37, Pose: glitchTwitchB, How: "snap"},
			{T: 0.44, Pose: glitchTwitchA, How: "snap"},
			{T: 0.53, Pose: glitchFine, How: "snap"},
			{T: 1.1, Pose: glitchTwitchB, How: "snap"},
			{T: 1.23, Pose: glitchFine, How: "snap"},
			{T: 1.7, Pose: glitchTwitchA, How: "snap"},
			{T: 1.8, Pose: glitchTwitchB.With(pixelfx.Crouch.Is(3)), How: "snap"},
			{T: 1.9, Pose: glitchTwitchA, How: "snap"},
			{T: 2, Pose: glitchTwitchB, How: "snap"},
			{T: 2.07, Pose: glitchFine, How: "snap"},
			{T: 2.4, Pose: glitchFine},
		},
	},
	// the effect draws the hero itself, clean or broken
	ActorAlpha: func(float64, *pixelfx.Options) float64 { return 0 },
	New:        newGlitch,
}

func init() { pixelfx.Register("scifi/glitch", Glitch) }

const (
	glitchDuration = 2.4
	glitchFPS      = 30
)

var glitchNF = int(pixelfx.Round(glitchDuration * glitchFPS))

type glitchBurst struct {
	a, b int
	k    float64
}

// the bursts: [first frame, last frame, how hard]
var glitchBursts = []glitchBurst{
	{9, 16, 0.8},
	{33, 37, 0.5},
	{51, 62, 1},
}

type glitchTear struct {
	y0, h, dx, tint float64
}

func newGlitch(o *pixelfx.Options) pixelfx.Instance {
	C, A, S := pixelfx.Fit(o.Palette(), 7), o.Actor, o.Seed()
	RED, CYAN := C[2], C[4]
	junkCols := [5]uint32{pixelfx.White, RED, CYAN, C[5], C[3]}
	deadCols := [3]uint32{pixelfx.White, RED, CYAN}

	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			f := int(math.Floor(t*glitchFPS+1e-6)) % glitchNF
			ff := float64(f)
			var burst *glitchBurst
			k := 0.0
			for i := range glitchBursts {
				if f >= glitchBursts[i].a && f <= glitchBursts[i].b {
					burst = &glitchBursts[i]
					k = burst.k
					break
				}
			}
			r1 := func(n float64) float64 { return pixelfx.Rand(S, ff, n) }
			r2 := func(n, m float64) float64 { return pixelfx.Rand(S, ff, n, m) }

			// what this frame's corruption is
			split := 0.0
			if burst != nil {
				dir := 1.0
				if r1(2) < 0.5 {
					dir = -1
				}
				split = pixelfx.Round(1+r1(1)*3*k) * dir
			}
			var tears []glitchTear
			if burst != nil {
				nt := 1 + int(math.Floor(r1(3)*4*k))
				for i := range nt {
					fi := float64(i)
					tears = append(tears, glitchTear{
						y0:   A.Y + math.Floor(r2(4, fi)*float64(A.H)),
						h:    1 + math.Floor(r2(5, fi)*5),
						dx:   pixelfx.Round((r2(6, fi) - 0.5) * 16 * k),
						tint: r2(7, fi),
					})
				}
			}
			// a lone row slips now and then even when it's calm
			if burst == nil && r1(8) < 0.07 {
				dx := 2.0
				if r1(10) < 0.5 {
					dx = -2
				}
				tears = append(tears, glitchTear{y0: A.Y + math.Floor(r1(9)*float64(A.H)), h: 1, dx: dx, tint: 1})
			}
			interlace := burst != nil && r1(11) < 0.3
			roll := 0.0
			if burst != nil && r1(12) < 0.15 {
				roll = -2 - math.Floor(r1(13)*2)
			}
			poster := burst != nil && r1(14) < 0.15*k
			tearAt := func(y float64) (float64, float64, bool) {
				for _, tr := range tears {
					if y >= tr.y0 && y < tr.y0+tr.h {
						return tr.dx, tr.tint, true
					}
				}
				return 0, 0, false
			}
			// the calm signal: a faint bar rolling down the body
			span := float64(A.H + 12)
			bar := A.Y + math.Mod((t/glitchDuration)*2*span, span) - 6

			// the split: the red and cyan ghosts first, offset either side
			if split != 0 {
				for my := range A.H {
					for mx := range A.W {
						if A.Mask.Data[my*A.W+mx] == 0 {
							continue
						}
						y := A.Y + float64(my) + roll
						dx, _, _ := tearAt(A.Y + float64(my))
						p.Px(A.X+float64(mx)+dx-split, y, RED)
						p.Px(A.X+float64(mx)+dx+split, y, CYAN)
					}
				}
			}
			// the body itself, row by row: torn, interlaced, rolled, posterised, tinted where the bar passes
			for my := range A.H {
				ry := A.Y + float64(my)
				y := ry + roll
				if interlace && my&1 != 0 {
					continue
				}
				dx, tintVal, hasTr := tearAt(ry)
				var tint uint32
				if hasTr && tintVal < 0.35 {
					if tintVal < 0.18 {
						tint = RED
					} else {
						tint = CYAN
					}
				}
				near := math.Abs(ry - bar)
				for mx := range A.W {
					c := A.Pixels[my*A.W+mx]
					if c == 0 {
						continue
					}
					col := c
					switch {
					case poster:
						if pixelfx.Luma(c) > 0.32 {
							col = C[5]
						} else {
							col = C[1]
						}
					case tint != 0:
						col = pixelfx.Mix(c, tint, 0.6)
					case near < 1:
						col = pixelfx.Mix(c, C[5], 0.35)
					}
					p.Px(A.X+float64(mx)+dx, y, col)
				}
				// a torn row smears its edge pixel out sideways, fading
				if hasTr && math.Abs(dx) > 2 && pixelfx.Rand(S, ff, ry) < 0.6 {
					right := dx > 0
					mx := -1
					if right {
						mx = A.W
						for i := range A.W {
							if A.Mask.Data[my*A.W+A.W-1-i] != 0 {
								mx = A.W - 1 - i
								break
							}
						}
					} else {
						for i := range A.W {
							if A.Mask.Data[my*A.W+i] != 0 {
								mx = i
								break
							}
						}
					}
					if mx >= 0 && mx < A.W {
						c := A.Pixels[my*A.W+mx]
						L := 4 + math.Floor(pixelfx.Rand(S, ff, ry, 1)*10)
						for i := 1.0; i <= L; i++ {
							off := -i
							if right {
								off = i
							}
							p.Dot(A.X+float64(mx)+dx+off, y, c, 1-i/(L+1))
						}
					}
				}
			}
			// a roll leaves the top slice repeated at the bottom
			if roll != 0 {
				for my := 0; float64(my) < -roll; my++ {
					for mx := range A.W {
						c := A.Pixels[my*A.W+mx]
						if c != 0 && (mx+my)&1 != 0 {
							p.Px(A.X+float64(mx), A.Y+float64(A.H)+roll+float64(my), pixelfx.Mix(c, C[1], 0.5))
						}
					}
				}
			}
			// junk blocks round the body
			if burst != nil {
				nj := 2 + int(math.Floor(r1(20)*5*k))
				for i := range nj {
					fi := float64(i)
					w := 2 + math.Floor(r2(21, fi)*9)
					h := 1 + math.Floor(r2(22, fi)*3)
					x := A.X - 6 + math.Floor(r2(23, fi)*float64(A.W+12))
					y := A.Y + math.Floor(r2(24, fi)*float64(A.H))
					c := junkCols[int(math.Floor(r2(25, fi)*5))]
					if r2(26, fi) < 0.4 {
						for yy := 0.0; yy < h; yy++ {
							for xx := 0.0; xx < w; xx++ {
								cc := C[0]
								if (int(xx)+int(yy))&1 != 0 {
									cc = c
								}
								p.Px(x+xx, y+yy, cc)
							}
						}
					} else {
						p.Rect(x, y, w, h, c, 1)
					}
				}
			}
			// dead pixels thrown off by a burst, drifting and blinking out
			for _, br := range glitchBursts {
				age := (f - br.a + glitchNF) % glitchNF
				if age > br.b-br.a+8 {
					continue
				}
				fa := float64(br.a)
				for i := 0; float64(i) < 10*br.k; i++ {
					fi := float64(i)
					life := 4 + math.Floor(pixelfx.Rand(S, fa, fi)*6)
					at := math.Floor(pixelfx.Rand(S, fa, fi, 1) * float64(br.b-br.a+1))
					q := float64(age) - at
					if q < 0 || q > life || (q > life/2 && pixelfx.ToInt32(q)&1 != 0) {
						continue
					}
					x0 := A.X + pixelfx.Rand(S, fa, fi, 2)*float64(A.W)
					y0 := A.Y + pixelfx.Rand(S, fa, fi, 3)*float64(A.H)
					side := 1.0
					if pixelfx.Rand(S, fa, fi, 4) < 0.5 {
						side = -1
					}
					p.Px(x0+side*q*(1+pixelfx.Rand(S, fa, fi, 5)*2), y0-q*0.5, deadCols[i%3])
				}
			}
			// light: a cold flicker in the bursts, the split's colors glowing at its edges
			if burst != nil {
				if f == burst.a {
					p.Glow(A.X+float64(A.W)/2, A.Y+float64(A.H)/2, 26, C[5], 0.4)
				}
				p.Glow(A.X+float64(A.W)/2-split*4, A.Y+float64(A.H)/2, 18, RED, 0.18*k)
				p.Glow(A.X+float64(A.W)/2+split*4, A.Y+float64(A.H)/2, 18, CYAN, 0.18*k)
			}
			if math.Abs(bar-(A.Y+float64(A.H)/2)) < float64(A.H)/2+3 {
				p.GlowLine(A.X-2, bar, A.X+float64(A.W)+2, bar, 4, C[4], 0.12)
			}
		},
	}
}
