// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/blessing.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var blessingOpen = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.FX.Is(-13), pixelfx.FY.Is(-31), pixelfx.BX.Is(14), pixelfx.BY.Is(-31), pixelfx.HY.Is(-1), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))

// Blessing is a soft cone of light resting on the character from above; sparkles drift down through it, twinkling, and wink out where they land on the head and shoulders. It is registered as "holy/blessing".
var Blessing = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Blessing",
		Desc:     "A soft cone of light rests on the character from above; sparkles drift down through it, twinkling, and wink out where they land on the head and shoulders.",
		Tags:     []string{"holy", "blessing", "buff", "light", "sparkle", "character", "loop"},
		Size:     [2]int{80, 128},
		Origin:   [2]int{40, 108},
		Duration: blessingT,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: goldPalette()},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: blessingOpen},
			{T: 0.75, Pose: blessingOpen.With(pixelfx.FY.Is(-33), pixelfx.BY.Is(-33))},
			{T: 1.5, Pose: blessingOpen.With(pixelfx.Crouch.Is(-1), pixelfx.FY.Is(-35), pixelfx.BY.Is(-35))},
			{T: 2.25, Pose: blessingOpen.With(pixelfx.FY.Is(-33), pixelfx.BY.Is(-33))},
			{T: 3, Pose: blessingOpen},
		},
	},
	New: newBlessing,
}

func init() { pixelfx.Register("holy/blessing", Blessing) }

const blessingT = 3

var blessingTop = -108.0

func blessingHalfW(y float64) float64 { return pixelfx.Lerp(5, 20, (y-blessingTop)/-blessingTop) }

// blessingRays are the rays in the cone, [offset, width, shimmers a loop, phase], each shimmering a whole number of times a loop so it closes.
var blessingRays = [4][4]float64{{-0.62, 0.16, 1, 0.3}, {-0.2, 0.2, 2, 1.7}, {0.22, 0.14, 1, 4.1}, {0.6, 0.18, 2, 2.6}}

func blessingRay(q, t float64) float64 {
	T := float64(blessingT)
	I := 0.3 * math.Max(0, 1-q*q)
	for _, r := range blessingRays {
		o, w, k, ph := r[0], r[1], r[2], r[3]
		d := (q - o) / w
		I += math.Exp(-(d * d)) * (0.55 + 0.45*math.Sin((t/T)*tau*k+ph)) * 0.6
	}
	return I * pixelfx.Smooth(1.25, 0.75, math.Abs(q))
}

// blessingLanding is one falling sparkle: where across the cone it falls (q), its speed, sway and phase, its size, and when it meets the character or the floor (+Inf when never).
type blessingLanding struct {
	q, v, sway, ph, size, land float64
}

// blessingLandings works out where each falling sparkle meets the character or the floor.
func blessingLandings(seed float64, S *shape) []blessingLanding {
	TOP := blessingTop
	out := make([]blessingLanding, 0, 20)
	for i := range 20 {
		R := pixelfx.RngOf(seed, float64(i))
		q, v, sway, ph, size := R.Range(-0.9, 0.9), R.Range(22, 34), R.Range(1, 3), R.Range(0, tau), R.Range(0.8, 2.4)
		land := math.Inf(1)
		for a := 0.0; a < 6; a += 0.02 {
			y := TOP + 6 + v*a
			x := q*blessingHalfW(y) + math.Sin(a*2.2+ph)*sway
			if y >= -1 || S.at(x, y) {
				land = a
				break
			}
		}
		out = append(out, blessingLanding{q, v, sway, ph, size, land})
	}
	return out
}

func newBlessing(o *pixelfx.Options) pixelfx.Instance {
	C, seed, A := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Actor
	W, TOP, T := pixelfx.White, blessingTop, float64(blessingT)
	var S *shape
	var L []blessingLanding
	if A != nil {
		S = shapeOf(A)
		L = blessingLandings(seed, S)
	}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			// the cone: soft light with a faint gold haze in it
			p.Each(-24, TOP, 24, 0.5, func(x, y float64, sx, sy int) {
				I := blessingRay(x/blessingHalfW(y), t) * (0.6 + 0.4*pixelfx.Smooth(TOP, 0, y))
				if I < 0.02 {
					return
				}
				p.BlendPx(sx, sy, C[6], I*0.13)
				p.Add(sx, sy, C[4], I*0.22)
			})
			// two of the rays show as fine lines, light running slowly down them
			for _, r := range [2][4]float64{blessingRays[1], blessingRays[2]} {
				o2, k, ph := r[0], r[2], r[3]
				for y := TOP; y < -1; y++ {
					pulse := 0.5 + 0.5*math.Sin(y*0.11-(t/T)*tau*3+ph)
					c := C[6]
					if pulse > 0.85 {
						c = W
					}
					p.Dot(o2*blessingHalfW(y), y, c, (0.1+0.45*pulse)*(0.5+0.5*math.Sin((t/T)*tau*k+ph))*pixelfx.Smooth(TOP, TOP+20, y))
				}
			}
			p.Glow(0, -4, 30, C[4], 0.22)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			// light catching the top of the head and the shoulders
			if A != nil {
				m := A.Mask
				if len(A.Pixels) > 0 {
					for i := range m.W {
						for j := range m.H {
							c := A.Pixels[j*m.W+i]
							if c == 0 || pixelfx.Luma(c) < 0.15 {
								continue
							}
							p.Px(A.X+float64(i), A.Y+float64(j), pixelfx.Mix(c, C[6], 0.35))
							break
						}
					}
				}
				p.Glow(0, -30, 20, C[5], 0.22)
			}
			// the floor catches it: a lit line and a pool fading out into the stone
			for y := 0; y <= 3; y++ {
				for x := -24; x <= 24; x++ {
					u := pixelfx.Hypot(float64(x)/24, float64(y)/4)
					if u >= 1 {
						continue
					}
					var c uint32
					switch {
					case u < 0.3 && y != 0:
						c = C[5]
					case u < 0.3:
						c = W
					case u < 0.6:
						c = C[5]
					case u < 0.85:
						c = C[4]
					default:
						c = C[3]
					}
					a := (1 - u) * 0.7
					if y == 0 {
						a = math.Sqrt(1 - u)
					}
					p.Dot(float64(x), float64(y), c, a)
				}
			}
			// sparkles drifting down, twinkling, winking out where they land
			if S != nil {
				pixelfx.Cycle(t, T, len(L), seed, pixelfx.Fixed(3), func(age, _ float64, _ *pixelfx.Rng, i int) {
					s := L[i]
					y := TOP + 6 + s.v*age
					x := s.q*blessingHalfW(y) + math.Sin(age*2.2+s.ph)*s.sway
					fade := math.Min(1, age/0.3)
					if age > s.land {
						w := (age - s.land) / 0.14
						if w < 1 {
							ly := TOP + 6 + s.v*s.land
							lx := s.q*blessingHalfW(ly) + math.Sin(s.land*2.2+s.ph)*s.sway
							pixelfx.Sparkle(p, lx, ly, 3*(1-w)+1, W, C[6], 0.5*(1-w))
						}
						return
					}
					tw := 0.6 + 0.4*math.Sin(age*(7+float64(i%4))+s.ph)
					if s.size > 1.2 {
						c := C[6]
						if tw > 0.85 {
							c = W
						}
						pixelfx.Sparkle(p, x, y, s.size*tw*fade, c, C[5], 0.35*fade)
					} else {
						c := C[6]
						if tw > 0.8 {
							c = W
						}
						p.Dot(x, y, c, fade)
						p.Glow(x, y, 3, C[5], 0.35*fade)
					}
				})
			}
			// fine motes hanging in the light
			pixelfx.Cycle(t, T, 16, seed+9, func(g *pixelfx.Rng) float64 { return g.Range(1.2, 2.4) }, func(age, life float64, R *pixelfx.Rng, _ int) {
				u := age / life
				y := TOP + 10 + R.Range(0, 90) + u*R.Range(6, 14)
				q := R.Range(-1, 1)
				x := q*blessingHalfW(y) + math.Sin(age*1.7+q*5)*1.5
				k := math.Sin(u*pi) * blessingRay(q, t)
				if k > 0.12 {
					c := C[6]
					if k > 0.45 {
						c = W
					}
					p.Dot(x, y, c, math.Min(1, k*2.2))
				}
			})
		},
	}
}
