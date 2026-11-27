// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/confetti.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flutter (a swing that slows at its ends, a tilt that follows it, a paper flip that shows the back) from Sprite Fusion's open-source falling leaves (https://www.spritefusion.com/pixel-art-vfx/falling-leaves), MIT License, Copyright (c) 2026 Hugo Duprez.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Confetti fires two party poppers one after the other so paper bits and curly streamers burst out, stall, tumble and flutter down, flipping, and settle on the floor. It is registered as "ui/confetti".
var Confetti = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Confetti",
		Desc:     "Two party poppers bang one after the other: paper bits and curly streamers burst out, stall, then tumble and flutter down, flipping, and settle on the floor.",
		Tags:     []string{"ui", "celebration", "confetti", "party", "reward", "one-shot"},
		Size:     [2]int{200, 140},
		Origin:   [2]int{100, 126},
		Duration: confettiDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#ff4a5a", "#ff9a2a", "#ffe03c", "#5ad84a", "#3cc8ff", "#7a6cff", "#f53cd1")},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	New: newConfetti,
}

func init() { pixelfx.Register("ui/confetti", Confetti) }

type popperSpec struct {
	x, y, a, t float64
}

var (
	confettiDuration = 3.4
	confettiPoppers  = [2]popperSpec{
		{x: -74, y: -1, a: -pi/2 + 0.62, t: 0.1},
		{x: 74, y: -1, a: -pi/2 - 0.62, t: 0.26},
	}
	confettiInk      = pixelfx.Hex("#141019")
	confettiStripeBg = pixelfx.Hex("#c8c0d8")
	confettiSmokeCol = pixelfx.Hex("#5d5660")
	confettiSparks   = pixelfx.Ramp("#ffffff", "#fff3a6", "#ffc23d", "#ff8a2a")
)

func drawPopper(p *pixelfx.Painter, x, y, a, k float64, C []uint32, alpha float64) {
	ca, sa := math.Cos(a), math.Sin(a)
	bx, by := x-ca*k, y-sa*k
	p.Shade(bx-12, by-12, bx+12, by+12, func(px, py float64, o *pixelfx.Out) uint32 {
		dx, dy := px-bx, py-by
		u, v := dx*ca+dy*sa, -dx*sa+dy*ca
		if u < 0 || u > 11 {
			return 0
		}
		hw := 0.8 + u*0.32
		if math.Abs(v) > hw+0.6 {
			return 0
		}
		o.A = alpha
		if math.Abs(v) > hw-0.4 || u > 10.2 || u < 0.8 {
			return confettiInk
		}
		stripe := int(math.Floor((u+v*0.6)/2.2))%2 == 0
		if stripe {
			if v < 0 {
				return C[2]
			}
			return pixelfx.Scale(C[2], 0.75)
		}
		if v < 0 {
			return pixelfx.White
		}
		return confettiStripeBg
	})
}

type confettiBit struct {
	x, y, vx, vy       float64
	c                  uint32
	w, h, ang, spin    float64
	flip, fs, ph, amp  float64
	fw, x0, rest, fall float64
	mode               int
}

type confettiRibbon struct {
	pts             [][2]float64
	vx, vy          float64
	c               uint32
	ph, amp, fw, x0 float64
	curl, fall      float64
	mode            int
}

func newConfetti(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	R := pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{
		Rng:    R,
		Ground: func(float64) float64 { return 0 },
		Sparks: confettiSparks,
	})
	lights := &pixelfx.Lights{}
	var bits []confettiBit
	var ribbons []confettiRibbon
	var fired [2]bool
	end := confettiDuration - 0.05
	kick := func(t float64, pp popperSpec) float64 {
		if t < pp.t {
			return 0
		}
		return 3 * math.Exp(-(t-pp.t)*14)
	}

	fire := func(pp popperSpec) {
		mx, my := pp.x+math.Cos(pp.a)*11, pp.y+math.Sin(pp.a)*11
		lights.Add(mx, my, 34, pixelfx.White, 0.6, 0.12)
		lights.Add(mx, my, 50, C[2], 0.4, 0.4)
		for range 6 {
			vx := math.Cos(pp.a)*R.Range(20, 60) + R.Range(-10, 10)
			vy := math.Sin(pp.a) * R.Range(20, 60)
			life := R.Range(0.6, 1)
			size := R.Range(2.5, 4)
			ps.Spawn(pixelfx.Smoke, mx, my, vx, vy, life, confettiSmokeCol, size, 0, 0)
		}
		for range 12 {
			an := pp.a + R.Range(-0.5, 0.5)
			sp := R.Range(120, 260)
			ps.Spawn(pixelfx.Spark, mx, my, math.Cos(an)*sp, math.Sin(an)*sp, R.Range(0.1, 0.25), pixelfx.White, 1, 0, 0)
		}
		for range 46 {
			an := pp.a + R.Range(-0.38, 0.38)
			sp := R.Range(200, 380)
			c := pixelfx.Pick(R, C)
			w := R.Range(2, 3)
			h := R.Range(3, 4.6)
			ang := R.Range(0, tau)
			spin := R.Range(-14, 14)
			flip := R.Range(0, tau)
			fs := R.Range(5, 13)
			ph := R.Range(0, tau)
			amp := R.Range(5, 12)
			fw := R.Range(2, 3.4)
			fall := R.Range(26, 40)
			bits = append(bits, confettiBit{
				x: mx, y: my, vx: math.Cos(an) * sp, vy: math.Sin(an) * sp,
				c: c, w: w, h: h, ang: ang, spin: spin, flip: flip, fs: fs, ph: ph, amp: amp, fw: fw, fall: fall,
			})
		}
		for range 4 {
			an := pp.a + R.Range(-0.25, 0.25)
			sp := R.Range(220, 330)
			pts := make([][2]float64, 11)
			for j := range pts {
				pts[j] = [2]float64{mx, my}
			}
			c := pixelfx.Pick(R, C)
			ph := R.Range(0, tau)
			amp := R.Range(6, 12)
			fw := R.Range(1.5, 2.4)
			curl := R.Range(0.7, 1.1)
			fall := R.Range(22, 32)
			ribbons = append(ribbons, confettiRibbon{
				pts: pts, vx: math.Cos(an) * sp, vy: math.Sin(an) * sp,
				c: c, ph: ph, amp: amp, fw: fw, curl: curl, fall: fall,
			})
		}
	}

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i, pp := range confettiPoppers {
				if !fired[i] && t >= pp.t {
					fired[i] = true
					fire(pp)
				}
			}
			for bi := range bits {
				b := &bits[bi]
				if b.mode == 2 {
					b.rest += dt
					continue
				}
				b.flip += b.fs * dt
				if b.mode == 0 {
					b.vx -= b.vx * 3.4 * dt
					b.vy -= b.vy*3.4*dt - 140*dt
					b.x += b.vx * dt
					b.y += b.vy * dt
					b.ang += b.spin * dt
					if pixelfx.Hypot(b.vx, b.vy) < 40 {
						b.mode = 1
						b.x0 = b.x - math.Sin(b.ph)*b.amp
					}
				} else {
					b.ph += b.fw * dt
					b.x0 += b.vx * dt
					b.vx -= b.vx * 2 * dt
					b.x = b.x0 + math.Sin(b.ph)*b.amp
					b.y += b.fall * (0.4 + 0.9*math.Abs(math.Cos(b.ph))) * dt
					b.ang = math.Cos(b.ph) * 0.9
					if b.y >= -1 {
						b.y = -0.5
						b.mode = 2
						b.ang = R.Range(-0.3, 0.3)
						b.flip = 0
					}
				}
			}
			for ri := range ribbons {
				r := &ribbons[ri]
				h := &r.pts[0]
				if r.mode == 0 {
					r.vx -= r.vx * 3 * dt
					r.vy -= r.vy*3*dt - 120*dt
					h[0] += r.vx * dt
					h[1] += r.vy * dt
					if pixelfx.Hypot(r.vx, r.vy) < 35 {
						r.mode = 1
						r.x0 = h[0] - math.Sin(r.ph)*r.amp
					}
				} else if h[1] < -1 {
					r.ph += r.fw * dt
					r.x0 += r.vx * dt
					r.vx -= r.vx * 2 * dt
					h[0] = r.x0 + math.Sin(r.ph)*r.amp
					h[1] += r.fall * (0.5 + 0.7*math.Abs(math.Cos(r.ph))) * dt
				}
				for i := 1; i < len(r.pts); i++ {
					a, b := &r.pts[i-1], &r.pts[i]
					b[1] = math.Min(-0.5, b[1]+20*dt)
					dx, dy := b[0]-a[0], b[1]-a[1]
					d := pixelfx.Hypot(dx, dy)
					if d == 0 {
						d = 1
					}
					if d > 2.6 {
						b[0] = a[0] + (dx/d)*2.6
						b[1] = a[1] + (dy/d)*2.6
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			fade := 1 - pixelfx.Seg(t, end-0.45, end-0.05)
			for _, pp := range confettiPoppers {
				k := kick(t, pp)
				drawPopper(p, pp.x, pp.y, pp.a, k, C, 1-pixelfx.Seg(t, 2.5, 2.9))
				ft := t - pp.t
				if ft > 0 && ft < 0.12 {
					pixelfx.Sparkle(p, pp.x+math.Cos(pp.a)*13, pp.y+math.Sin(pp.a)*13, 7*(1-ft/0.12), pixelfx.White, C[2], 0.7)
				}
			}
			for ri := range ribbons {
				r := &ribbons[ri]
				pts := make([][2]float64, len(r.pts))
				for i, q := range r.pts {
					off := 0.0
					if i != 0 {
						off = 1.6
					}
					pts[i] = [2]float64{q[0] + math.Sin(float64(i)*r.curl+t*9+r.ph)*off, q[1]}
				}
				for i := 1; i < len(pts); i++ {
					x0, y0, x1, y1 := pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1]
					front := math.Sin(float64(i)*r.curl+t*9+r.ph) > 0
					c0, c1 := pixelfx.Scale(r.c, 0.6), pixelfx.Scale(r.c, 0.45)
					if front {
						c0, c1 = r.c, pixelfx.Mix(r.c, pixelfx.White, 0.35)
					}
					p.Line(x0, y0, x1, y1, c0, fade)
					p.Line(x0+1, y0, x1+1, y1, c1, fade)
				}
			}
			for bi := range bits {
				b := &bits[bi]
				if b.y > 1 {
					continue
				}
				fw := math.Cos(b.flip)
				if b.mode == 2 {
					fw = 1
				}
				w := math.Max(0.5, math.Abs(fw)*b.w)
				ca, sa := math.Cos(b.ang), math.Sin(b.ang)
				c := b.c
				if fw < 0 {
					c = pixelfx.Scale(b.c, 0.62)
				}
				a := fade
				if b.mode == 2 {
					p.Dot(b.x, b.y, pixelfx.Scale(b.c, 0.8), a)
					p.Dot(b.x+1, b.y, pixelfx.Scale(b.c, 0.6), a)
					continue
				}
				p.Shade(b.x-4, b.y-4, b.x+4, b.y+4, func(x, y float64, out *pixelfx.Out) uint32 {
					dx, dy := x-b.x, y-b.y
					u, v := dx*ca+dy*sa, -dx*sa+dy*ca
					if math.Abs(u) > w/2+0.2 || math.Abs(v) > b.h/2 {
						return 0
					}
					out.A = a
					if math.Abs(fw) > 0.92 && v < -b.h/2+1 {
						return pixelfx.White
					}
					return c
				})
				if math.Abs(fw) > 0.97 {
					p.Glow(b.x, b.y, 5, pixelfx.Mix(b.c, pixelfx.White, 0.5), 0.35*fade)
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
