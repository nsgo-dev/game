// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/eclipse.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Eclipse is a black moon over the sun: a breathing pearl corona with long streamers, a thin white rim, and one diamond bead blazing on its edge. It is registered as "shadow/eclipse".
var Eclipse = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Eclipse",
		Desc:     "A black moon over the sun: a breathing pearl corona with long streamers, a thin white rim, and one diamond bead blazing on its edge.",
		Tags:     []string{"shadow", "eclipse", "moon", "sun", "corona", "sky", "magic", "loop"},
		Size:     [2]int{160, 144},
		Origin:   [2]int{80, 72},
		Duration: 4,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "pearl", Default: pixelfx.Palette("#5a4f8a", "#a99be0", "#e6defa", "#fffaf2")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newEclipse,
}

func init() { pixelfx.Register("shadow/eclipse", Eclipse) }

// the moon's radius, the loop, where the bead blazes on the rim (radians, 0 = right, up is negative)
const (
	eclipseR    = 19.0
	eclipseD    = 4.0
	eclipseBead = -0.72
)

func eclipseBreathe(t, k, ph float64) float64 { return math.Sin((tau*k*t)/eclipseD + ph) }

type eclipseStream struct {
	a, len, curl, k, ph, w float64
}

// eclipseStreams are crowded toward the equator (left and right), longer there, each curving off as it goes out.
var eclipseStreams = func() [30]eclipseStream {
	var out [30]eclipseStream
	for i := range out {
		fi := float64(i)
		side := pi
		if i%2 != 0 {
			side = 0
		}
		width := 3.1
		if pixelfx.Rand(fi, 2) < 0.7 {
			width = 1.3
		}
		spread := (pixelfx.Rand(fi, 1) - 0.5) * width
		a := side + spread
		eq := math.Abs(math.Cos(a))
		out[i] = eclipseStream{
			a:    a,
			len:  (10 + pixelfx.Rand(fi, 3)*28) * (0.45 + 0.75*eq),
			curl: (pixelfx.Rand(fi, 4) - 0.5) * 0.00018,
			k:    float64(1 + (i % 3)),
			ph:   pixelfx.Rand(fi, 5) * tau,
			w:    pixelfx.Rand(fi, 6),
		}
	}
	return out
}()

func newEclipse(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	Q := pixelfx.Fit(o.Ramp("pearl"), 4)
	S := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			// stars twinkling in the dark sky round it
			pixelfx.Cycle(t, eclipseD, 26, S, func(r *pixelfx.Rng) float64 { return r.Range(1.2, 2.6) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				x, y := r.Range(-78, 78), r.Range(-70, 70)
				if pixelfx.Hypot(x, y) < eclipseR+22 {
					return
				}
				k := math.Sin((pi * age) / L)
				p.Dot(x, y, pick(k > 0.7, pixelfx.White, Q[1]), k*1.2)
				if k > 0.85 && r.Next() < 0.4 {
					p.Dot(x-1, y, Q[0], 0.8)
					p.Dot(x+1, y, Q[0], 0.8)
				}
			})
			// the outer corona's light: a wide pearly bloom, breathing
			b := 1 + 0.08*eclipseBreathe(t, 1, 0)
			p.Glow(0, 0, 72*b, C[4], 0.38)
			p.Glow(0, 0, 46*b, Q[1], 0.34)
			// streamers: fine rays of pearl, white at the root, thinning to violet, curling as they go out
			for _, s := range eclipseStreams {
				L := s.len * (1 + 0.18*eclipseBreathe(t, s.k, s.ph))
				for d := 4.0; d <= L; d += 0.5 {
					u, a, r := d/L, s.a+s.curl*d*d, eclipseR+1+d
					x, y := math.Cos(a)*r, math.Sin(a)*r
					c := C[5]
					if u < 0.3 {
						c = Q[2]
					} else if u < 0.65 {
						c = Q[1]
					}
					al := 1.0
					if u >= 0.75 {
						al = (1 - u) * 4
					}
					p.Dot(x, y, c, al)
				}
				p.GlowLine(math.Cos(s.a)*(eclipseR+4), math.Sin(s.a)*(eclipseR+4), math.Cos(s.a)*(eclipseR+L*0.6), math.Sin(s.a)*(eclipseR+L*0.6), 3, Q[1], 0.12)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			b := 1 + 0.08*eclipseBreathe(t, 1, 0)
			// the inner corona: a bright collar hugging the moon, finely streaked, fading out through pearl to violet
			const W = eclipseR + 10
			p.Shade(-W, -W, W, W, func(x, y float64, out *pixelfx.Out) uint32 {
				r := math.Sqrt(x*x + y*y)
				if r < eclipseR-0.5 || r > eclipseR+9*b {
					return 0
				}
				a, e := math.Atan2(y, x), (r-eclipseR)/(9*b)
				streak := 0.55 + 0.3*math.Cos(a*23+math.Sin(a*5)*2) + 0.15*math.Cos(a*41-(tau*t)/eclipseD)
				I := math.Pow(1-e, 2.2) * streak * (0.75 + 0.4*math.Abs(math.Cos(a)))
				if r < eclipseR+0.9 {
					return Q[3]
				}
				if I < 0.1 {
					return 0
				}
				if I > 0.45 && (pixelfx.ToInt32(math.Floor(x)+math.Floor(y))&1) == 0 {
					out.K = 0.2 * I
				}
				switch {
				case I > 0.72:
					return Q[2]
				case I > 0.42:
					return Q[1]
				case I > 0.22:
					return C[5]
				}
				return C[4]
			})
			// the moon: black, a whisper of earthshine on its left limb
			p.Shade(-eclipseR, -eclipseR, eclipseR, eclipseR, func(x, y float64, _ *pixelfx.Out) uint32 {
				r := math.Sqrt(x*x + y*y)
				if r >= eclipseR-0.5 {
					return 0
				}
				return pick(r > eclipseR-2.2 && x < -eclipseR*0.45, C[1], C[0])
			})
			// the diamond bead: a blazing point on the rim, rays flaring out and breathing
			bx, by := math.Cos(eclipseBead)*(eclipseR+0.4), math.Sin(eclipseBead)*(eclipseR+0.4)
			k := 0.75 + 0.25*eclipseBreathe(t, 2, 1)
			rCut := eclipseR - 0.5
			for i := range 8 {
				a := (float64(i)*pi)/4 + 0.12
				base := 22.0
				if i%2 != 0 {
					base = 7
				}
				mul := 1.0
				if i == 1 || i == 5 {
					mul = 1.6
				}
				ln := base * k * mul
				for d := 1.0; d <= ln; d += 0.5 {
					u := d / ln
					x, y := bx+math.Cos(a)*d, by+math.Sin(a)*d
					if x*x+y*y < rCut*rCut {
						break
					}
					c := Q[1]
					if u < 0.35 {
						c = pixelfx.White
					} else if u < 0.7 {
						c = Q[2]
					}
					al := 1.0
					if u >= 0.5 {
						al = (1 - u) * 2.2
					}
					p.Dot(x, y, c, al)
				}
			}
			p.Circle(bx, by, 3*k, Q[2], 1)
			p.Circle(bx, by, 2.2*k, Q[3], 1)
			p.Circle(bx, by, 1.4, pixelfx.White, 1)
			p.Glow(bx, by, 46*k, Q[2], 0.8*k)
			p.Glow(bx, by, 14, pixelfx.White, 0.8)
			halo(p, 0, 0, eclipseR-0.5, eclipseR+16, Q[2], 0.35*b)
			rLim := eclipseR - 0.6
			unlight(p, -eclipseR, -eclipseR, eclipseR, eclipseR, func(x, y float64) bool {
				return x*x+y*y < rLim*rLim
			}, 0)
		},
	}
}
