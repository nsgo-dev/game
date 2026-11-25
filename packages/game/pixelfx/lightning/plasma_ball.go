// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/plasma-ball.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The re-struck filaments, the glass shell with its specular arc and the core follow Sprite Fusion's open-source energy orb (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// PlasmaBall is a glass globe on a dark pedestal whose wandering plasma tendrils reach from the core to the glass and fan out where they touch it, lighting the stand and floor. It is registered as "lightning/plasma-ball".
var PlasmaBall = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Plasma ball",
		Desc:     "A glass globe on a dark pedestal: wandering plasma tendrils reach from the core to the glass and fan out in bright feet where they touch it, lighting the stand and floor.",
		Tags:     []string{"lightning", "electric", "plasma", "lamp", "prop", "loop"},
		Size:     [2]int{112, 104},
		Origin:   [2]int{56, 96},
		Duration: 3,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0828", "#3b0f5e", "#6a1d9c", "#a03ad6", "#d46cf2", "#f2a8ff", "#ffe2ff", "#ffffff")},
			{Name: "tendrils", Default: pixelfx.Num(9)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newPlasmaBall,
}

func init() { pixelfx.Register("lightning/plasma-ball", PlasmaBall) }

const (
	pbFPS = 30
	pbN   = 90
)

// the globe's centre and radius, variables so sums with fractions round as JavaScript's do
var pbGX, pbGY, pbRG = 0.0, -55.0, 24.0

var pbBase = pixelfx.Ramp("#0d0b12", "#17131f", "#231d2d", "#342b43", "#4b3f60", "#6b5c87")

type pbTendril struct {
	a, ex, ey, w1, w2 float64
	front, rim        bool
}

// pbTendrilAt returns tendril i of n at phase ph: its end wanders over the glass, front ones end on the near face, back ones on the far.
func pbTendrilAt(i int, n, ph, seed float64) pbTendril {
	fi := float64(i)
	a := ((fi+0.5)/n)*tau + 0.9*pixelfx.LoopNoise(ph, 0.9, seed+fi*7)
	z := pixelfx.LoopNoise(ph, 0.7, seed+fi*7+3)
	rho := pbRG * (0.72 + 0.28*math.Sqrt(1-math.Min(1, z*z*1.6)))
	return pbTendril{
		a: a, front: z > -0.15, ex: pbGX + math.Cos(a)*rho, ey: pbGY + math.Sin(a)*rho, rim: rho > pbRG*0.93,
		w1: pixelfx.LoopNoise(ph, 1.1, seed+fi*7+5), w2: pixelfx.LoopNoise(ph, 1.3, seed+fi*7+6),
	}
}

// pbPedestal draws the pedestal: a flared stand lit from above by the globe, a collar under the glass.
func pbPedestal(p *pixelfx.Painter, C []uint32, glow float64) {
	B := pbBase
	p.Shade(-18, -32, 18, 0, func(x, y float64, o *pixelfx.Out) uint32 {
		hw := 16.0
		if y < -27 {
			hw = 7
		} else if y < -2 {
			hw = 9 + ((y+27)/25)*6
		}
		if math.Abs(x) > hw {
			return 0
		}
		u := (x/hw + 1) / 2
		if y < -27 {
			switch {
			case y < -30 && u < 0.6:
				o.C = C[4]
			case y < -30:
				o.C = B[5]
			case u < 0.3:
				o.C = B[5]
			case u < 0.75:
				o.C = B[3]
			default:
				o.C = B[1]
			}
			return o.C
		}
		if y >= -2 {
			o.C = B[1]
			if y < -1 {
				o.C = B[2]
				if u < 0.55 {
					o.C = B[4]
				}
			}
			return o.C
		}
		// glossy body: a lit flank, a specular stripe, the far side in shadow, plasma light on the upper rim
		var i int
		switch {
		case u < 0.1:
			i = 2
		case u < 0.2:
			i = 3
		case u < 0.3:
			i = 5
		case u < 0.42:
			i = 4
		case u < 0.6:
			i = 3
		case u < 0.8:
			i = 2
		case u < 0.92:
			i = 1
		}
		switch {
		case y < -24 && u < 0.5:
			o.C = pixelfx.Mix(B[i], C[4], 0.3*glow)
		case y > -5:
			o.C = B[max(0, i-1)]
		default:
			o.C = B[i]
		}
		return 0
	})
	p.Px(9, -9, C[5])
	p.Glow(9, -9, 3, C[5], 0.5)
}

func newPlasmaBall(o *pixelfx.Options) pixelfx.Instance {
	C, seed, n := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Num("tendrils")
	count := 0
	if n > 0 {
		count = int(math.Floor(n))
	}
	GX, GY, RG := pbGX, pbGY, pbRG
	ts := make([]pbTendril, count)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		f := frameOf(t, pbFPS, pbN)
		ff := float64(f)
		ph := ff / pbN
		R := pixelfx.RngOf(seed, 3, ff)
		pulse := 0.85 + 0.15*math.Sin(ph*tau*6) + 0.1*(R.Next()-0.5)
		for i := range ts {
			ts[i] = pbTendrilAt(i, n, ph, seed)
		}
		// light the floor and the stand, then the stand
		p.Glow(0, -6, 52, C[3], 0.22*pulse)
		pbPedestal(p, C, pulse)
		// the glass: a faint tint over what's behind, the plasma's glow inside it
		p.Shade(GX-RG, GY-RG, GX+RG, GY+RG, func(x, y float64, _ *pixelfx.Out) uint32 {
			d := pixelfx.Hypot(x-GX, y-GY)
			if d < RG-0.5 {
				c := C[0]
				if d > RG-4 {
					c = C[1]
				}
				p.Blend(x, y, c, 0.22+0.4*math.Pow(d/RG, 4))
			}
			return 0
		})
		p.Glow(GX, GY, RG+14, C[3], 0.32*pulse)
		p.GlowRing(GX, GY, RG-1, 5, C[3], 0.22*pulse)
		drawTendril := func(q pbTendril, i int, bright bool) {
			fi := float64(i)
			sx, sy := GX+math.Cos(q.a)*4.5, GY+math.Sin(q.a)*4.5
			dx, dy := q.ex-sx, q.ey-sy
			L := pixelfx.Hypot(dx, dy)
			nx, ny := -dy/L, dx/L
			r := pixelfx.RngOf(seed, 17, fi, ff)
			m := int(math.Max(4, pixelfx.Round(L/2.2)))
			pts := make([][2]float64, 0, m+1)
			for j := 0; j <= m; j++ {
				u := float64(j) / float64(m)
				w := 3.2*q.w1*math.Sin(pi*u) + 1.6*q.w2*math.Sin(tau*u+fi)
				jit := 0.0
				if j != 0 && j < m {
					jit = r.Range(-0.7, 0.7)
				}
				pts = append(pts, [2]float64{sx + dx*u + nx*(w+jit), sy + dy*u + ny*(w+jit)})
			}
			// the foot: the filament fans into short fingers where it meets the glass
			paths := bolt{{pts: pts, w: 1}}
			for k := range 3 {
				bx, by := pts[m-2][0], pts[m-2][1]
				a := q.a + float64(k-1)*r.Range(0.25, 0.45)
				l := r.Range(2.5, 5)
				mx := bx + math.Cos(a)*l*0.5 + r.Range(-0.6, 0.6)
				my := by + math.Sin(a)*l*0.5 + r.Range(-0.6, 0.6)
				paths = append(paths, path{pts: [][2]float64{{bx, by}, {mx, my}, {bx + math.Cos(a)*l, by + math.Sin(a)*l}}, w: 0.5})
			}
			hot := r.Next() < 0.2
			for _, q2 := range paths {
				for j := 0; j < len(q2.pts)-1; j++ {
					ax, ay, bx, by := q2.pts[j][0], q2.pts[j][1], q2.pts[j+1][0], q2.pts[j+1][1]
					u := float64(j) / float64(len(q2.pts)-1)
					if bright && q2.w >= 1 {
						p.Line(ax+1, ay+1, bx+1, by+1, C[3], 1)
					}
					var c uint32
					switch {
					case !bright && q2.w >= 1:
						c = C[3]
					case !bright:
						c = C[2]
					case q2.w < 1, hot || u > 0.75, u < 0.2:
						c = C[6]
					default:
						c = C[5]
					}
					p.Line(ax, ay, bx, by, c, 1)
					if q2.w >= 1 && j%3 == 0 {
						gr, gk := 4.0, 0.12
						if bright {
							gr, gk = 6, 0.24
							if hot {
								gk = 0.4
							}
						}
						p.Glow(ax, ay, gr, C[4], gk)
					}
				}
			}
			// where it touches, the glass flares
			if !bright {
				p.Glow(q.ex, q.ey, 6, C[4], 0.25)
				return
			}
			p.Circle(q.ex, q.ey, 1.6, C[6], 1)
			p.Px(q.ex, q.ey, pixelfx.White)
			p.Glow(q.ex, q.ey, 9, C[5], 0.55)
			if q.rim {
				for s := -1.0; s <= 1; s += 2 {
					p.Px(GX+math.Cos(q.a+s*0.09)*(RG-0.5), GY+math.Sin(q.a+s*0.09)*(RG-0.5), C[6])
				}
			}
		}
		for i, q := range ts {
			if !q.front {
				drawTendril(q, i, false)
			}
		}
		// the core: an electrode ball, pulsing
		cr := 3.6 + 0.5*pulse
		p.Circle(GX, GY, cr+1.3, C[3], 1)
		p.Circle(GX, GY, cr, C[5], 1)
		p.Circle(GX-0.6, GY-0.6, cr*0.55, C[6], 1)
		p.Circle(GX-0.8, GY-0.8, cr*0.25, pixelfx.White, 1)
		p.Glow(GX, GY, 16, C[5], 0.65*pulse)
		for i, q := range ts {
			if q.front {
				drawTendril(q, i, true)
			}
		}
		// the glass shell: a thin rim, a bright specular arc upper left, a faint reflection lower right
		p.Ring(GX, GY, RG+1.6, 1, C[1], 1)
		p.RingFn(GX, GY, RG+0.6, 1.1, func(_, a float64) (uint32, float64) {
			switch {
			case a > 2.2 && a < 3.3:
				return C[5], 1
			case a > 0.3 && a < 1.3:
				return C[4], 1
			}
			return C[3], 1
		})
		p.ArcFn(GX, GY, RG-1.6, 1.3, 3.55, 4.55, func(u, _ float64) (uint32, float64) {
			if u > 0.25 && u < 0.7 {
				return pixelfx.White, 1
			}
			return C[6], 1
		})
		for a := 0.35; a < 1.15; a += 0.045 {
			p.Dot(GX+math.Cos(a)*(RG-2.6), GY+math.Sin(a)*(RG-2.6), C[4], 0.85)
		}
		p.Px(GX-RG*0.42, GY-RG*0.62, pixelfx.White)
	}}
}
