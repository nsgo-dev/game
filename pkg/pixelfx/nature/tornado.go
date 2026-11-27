// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/tornado.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Tornado is a snaking funnel of spinning wind bands under a churning cloud crown flickering with lightning, whirling leaves, debris and boiling floor dust. It is registered as "nature/tornado".
var Tornado = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Tornado",
		Desc:     "A snaking funnel of spinning wind bands under a flared, churning top that flickers with lightning; leaves and debris whirl round it front and back, dust boils at its foot.",
		Tags:     []string{"nature", "wind", "tornado", "storm", "hazard", "ground", "loop"},
		Size:     [2]int{196, 168},
		Origin:   [2]int{98, 152},
		Duration: tornadoDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#141221", "#231f33", "#36314a", "#4d4762", "#68617d", "#8a839e", "#b3adc4", "#e2def0")},
			{Name: "dust", Default: pixelfx.Palette("#2a2018", "#4e3e2e", "#76624a", "#a08a6a", "#cbb894")},
			{Name: "leaves", Default: pixelfx.Palette("#4a200c", "#a8541c", "#e8962e", "#ffd27a", "#123818", "#2f7a2a", "#6cb83a", "#c8f070")},
			{Name: "height", Default: pixelfx.Num(112)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newTornado,
}

func init() { pixelfx.Register("nature/tornado", Tornado) }

const (
	tornadoDuration = 2.0
	tornadoSY       = 0.28
)

type tornadoGeo struct {
	fh, ph float64
}

func (g tornadoGeo) ax(u float64) float64 {
	return 8.5*math.Sin(g.ph-2.6*u)*(0.2+0.8*u) + 3*math.Sin(2*g.ph+1.3-4.5*u)*u
}

func (g tornadoGeo) rad(u float64) float64 {
	s := pixelfx.Smooth(0.78, 1, u)
	return 3 + 20*math.Pow(u, 1.5) + 17*math.Pow(s, 2)
}

func tornadoGeoAt(fh, t float64) tornadoGeo {
	return tornadoGeo{fh: fh, ph: (tau * t) / tornadoDuration}
}

// tornadoBody draws the translucent spinning column, thicker through its middle, lit from the left, with shearing dust streaks.
func tornadoBody(p *pixelfx.Painter, C, Dst []uint32, g tornadoGeo, t, seed float64) {
	for y := -math.Ceil(g.fh) + 2; y <= 0; y++ {
		u := -y / g.fh
		cx, r := g.ax(u), g.rad(u)
		K := 2.0
		if pixelfx.Rand(y, seed) < 0.35 {
			K = 3
		}
		rot := (tau * K * t) / tornadoDuration
		fade := math.Min(1, u*8+0.25) * (1 - pixelfx.Smooth(0.9, 1.02, u))
		for x := math.Floor(cx - r); x <= math.Ceil(cx+r); x++ {
			c := (x + 0.5 - cx) / r
			if math.Abs(c) > 1 {
				continue
			}
			n := pixelfx.TileNoise(((math.Acos(c)+rot)*6)/tau, y*0.34, 6, 0, seed)
			v := -c*0.55 + 0.15 + n*0.55
			var col uint32
			switch {
			case v > 0.6:
				col = C[6]
			case v > 0.28:
				col = C[5]
			case v > -0.05:
				col = C[4]
			case v > -0.4:
				col = C[3]
			default:
				col = C[2]
			}
			if u < 0.3 {
				di := 1
				if v > 0.3 {
					di = 3
				} else if v > -0.2 {
					di = 2
				}
				col = pixelfx.Mix(col, Dst[di], 0.7*(1-u/0.3))
			}
			p.Blend(x, y, col, (0.14+0.36*math.Sqrt(1-c*c))*fade)
		}
	}
}

// tornadoBands draws wind bands wrapped round the funnel.
func tornadoBands(p *pixelfx.Painter, C []uint32, g tornadoGeo, t, seed float64, back bool) {
	for j := 0; j < 58; j++ {
		R := pixelfx.RngOf(seed, float64(j))
		u := (float64(j) + R.Range(0.1, 0.9)) / 58
		K := R.Int(2, 4)
		span := R.Range(0.7, 2.1)
		lvl := R.Next()
		cx := g.ax(u)
		r := g.rad(u) * R.Range(1.0, 1.1)
		y0 := -u * g.fh
		head := R.Range(0, tau) + (tau*K*t)/tornadoDuration
		if u > 0.97 {
			continue
		}
		for s := 0.0; s <= span; s += 0.5 / math.Max(2, r) {
			th := head - s
			sn := math.Sin(th)
			if (sn > 0) == back {
				continue
			}
			k := s / span
			x := cx + r*math.Cos(th)
			y := y0 + r*tornadoSY*sn
			lit := math.Cos(th) < 0.2
			var i int
			switch {
			case lvl < 0.18:
				i = 2
			case k < 0.15 && lvl > 0.55:
				i = 7
			case k < 0.45:
				if lit {
					i = 6
				} else {
					i = 5
				}
			default:
				if lit {
					i = 5
				} else {
					i = 4
				}
			}
			if back {
				i = max(1, i-2)
			}
			if k < 0.5 {
				p.Dot(x, y, C[i], 1)
			} else {
				p.Blend(x, y, C[i], 2*(1-k))
			}
		}
		if !back && lvl > 0.6 && math.Sin(head) > 0 {
			hx := cx + r*math.Cos(head)
			hy := y0 + r*tornadoSY*math.Sin(head)
			p.Dot(hx, hy, pixelfx.White, 1)
			p.Glow(hx, hy, 5, C[6], 0.3)
		}
	}
}

// tornadoBlob draws a soft flattened puff: rx wide, ry tall, blended.
func tornadoBlob(p *pixelfx.Painter, cx, cy, rx, ry float64, c uint32, a float64) {
	for y := math.Floor(cy - ry); y <= math.Ceil(cy+ry); y++ {
		for x := math.Floor(cx - rx); x <= math.Ceil(cx+rx); x++ {
			dx, dy := (x+0.5-cx)/rx, (y+0.5-cy)/ry
			if dx*dx+dy*dy <= 1 {
				p.Blend(x, y, c, a)
			}
		}
	}
}

// tornadoCrown draws the flared cloud lid sheared into flat bands riding round with the funnel.
func tornadoCrown(p *pixelfx.Painter, C []uint32, g tornadoGeo, t, seed float64, back bool, flash float64) {
	cx, r := g.ax(1), g.rad(1)
	L := func(c uint32) uint32 { return pixelfx.Mix(c, C[7], flash*0.6) }
	y0 := -g.fh - 3
	if back {
		tornadoBlob(p, cx, y0+1, r*0.85, 6, C[1], 0.6)
		tornadoBlob(p, cx-4, y0-3, r*0.6, 3.5, L(C[3]), 0.45)
	}
	d := 0
	backMul := 1.0
	if back {
		d = 1
		backMul = 0.75
	}
	for j := 0; j < 16; j++ {
		R := pixelfx.RngOf(seed, float64(j))
		rr := r * R.Range(0.4, 1.15)
		K := R.Int(1, 2)
		span := R.Range(1.4, 2.8)
		head := R.Range(0, tau) + (tau*K*t)/tornadoDuration
		th0 := R.Range(3.2, 6)
		lift := R.Range(-4, 3) - (1-rr/(r*1.15))*5
		for s2 := 0.0; s2 <= span; s2 += 0.6 / rr {
			th := head - s2
			sn := math.Sin(th)
			if (sn > 0) == back {
				continue
			}
			k := s2 / span
			th2 := th0 * math.Sin(pi*math.Pow(1-k, 0.7))
			x := cx + rr*math.Cos(th)
			y := y0 + lift + rr*tornadoSY*sn
			T := max(1, int(pixelfx.Round(th2)))
			lit := math.Cos(th) < 0.3
			for i := 0; i < T; i++ {
				var c uint32
				switch {
				case i == 0:
					idx := 5
					if lit {
						idx = 6
					}
					c = L(C[idx-d])
				case i == T-1 && T > 2:
					c = C[1]
				default:
					idx := 3
					if i == 1 {
						idx = 4
					}
					c = L(C[idx-d])
				}
				p.Blend(x, y-float64(T)/2+float64(i), c, (0.55+0.35*(1-k))*backMul)
			}
		}
	}
}

// tornadoDebris draws leaves, twigs and pebbles spiralling up round the funnel and flung off the top.
func tornadoDebris(p *pixelfx.Painter, L []uint32, g tornadoGeo, t, seed float64, back bool, S []uint32) {
	dk := 0
	if back {
		dk = 1
	}
	pixelfx.Cycle(t, tornadoDuration, 24, seed, func(q *pixelfx.Rng) float64 { return q.Range(1.3, 1.95) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		// the first stretch skids in across the floor, then it spirals up and is flung off the top
		k := age / life
		sk := R.Range(0.15, 0.3)
		q := pixelfx.Clamp((k - sk) / (1 - sk))
		u := pixelfx.Lerp(0.03, 0.98, math.Pow(q, 0.85))
		th := R.Range(0, tau) + R.Range(2, 3.2)*tau*k
		sn := math.Sin(th)
		if (sn > 0) == back {
			return
		}
		fling := pixelfx.Smooth(0.78, 1, q)
		inward := 1 - pixelfx.Smooth(0, sk, k)
		far := R.Range(26, 44)
		rr := pixelfx.Lerp(math.Min(68, g.rad(u)*(1.12+fling*1.1)+R.Range(2, 7)), far, inward)
		x := g.ax(u) + rr*math.Cos(th)
		y := math.Min(-1, -u*g.fh+rr*tornadoSY*sn-fling*8)
		a := math.Min(1, k*8) * (1 - pixelfx.Smooth(0.85, 1, k))
		kind := R.Next()
		spin := R.Range(-14, 14) * age
		switch {
		case kind < 0.62:
			o := 4
			if R.Next() < 0.5 {
				o = 0
			}
			pal := [4]uint32{L[o], L[o+1], L[o+2-dk], L[o+3-dk]}
			length := R.Range(6, 9)
			wid := R.Range(1.8, 2.6)
			fl := age * R.Range(5, 10)
			drawLeaf(p, x, y, length, wid, spin, fl, pal, a)
		case kind < 0.8:
			ca, sa := math.Cos(spin)*2.5, math.Sin(spin)*2.5
			col := L[1]
			if back {
				col = L[0]
			}
			p.Line(x-ca, y-sa, x+ca, y+sa, col, a)
			p.Dot(x+ca, y+sa, L[2], a)
		default:
			sz := R.Range(1.6, 2.6)
			drawRock(p, x, y, sz, spin, stoneShape(R, 5), S, a, false)
		}
	})
}

// tornadoSkirt draws dust boiling at the tornado's foot, grit hopping, and scuffs swept round the floor.
func tornadoSkirt(p *pixelfx.Painter, D []uint32, g tornadoGeo, t, seed float64, back bool) {
	cx0 := g.ax(0)
	pixelfx.Cycle(t, tornadoDuration, 40, seed, func(q *pixelfx.Rng) float64 { return q.Range(0.7, 1.3) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		k := age / life
		th := R.Range(0, tau) + R.Range(5, 9)*age
		sn := math.Sin(th)
		if (sn > 0) == back {
			return
		}
		rr := 4 + k*R.Range(14, 28)
		x := cx0 + rr*math.Cos(th)
		y := -1.5 - k*R.Range(4, 16) + rr*0.3*sn
		s := R.Range(3, 6) * (0.6 + 0.8*k)
		a := math.Min(1, k*6) * (1 - k) * 0.6
		c1, c2 := D[2], D[3]
		if back {
			c1, c2 = D[1], D[2]
		}
		p.Disc(x, y+s*0.2, s, c1, a)
		p.Disc(x-s*0.2, y-s*0.3, s*0.55, c2, a*0.8)
	})
	if back {
		return
	}
	pixelfx.Cycle(t, tornadoDuration, 16, seed+7, func(q *pixelfx.Rng) float64 { return q.Range(0.35, 0.6) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		th := R.Range(0, tau)
		v := R.Range(25, 55)
		x := cx0 + math.Cos(th)*(4+v*age)
		y := math.Sin(th)*2 - R.Range(40, 80)*age + 260*age*age
		if y < 0.5 {
			col := D[4]
			if R.Next() < 0.5 {
				col = D[3]
			}
			p.Dot(x, math.Min(0, y), col, 1-age/life)
		}
	})
	for j := 0; j < 7; j++ {
		R := pixelfx.RngOf(seed+3, float64(j))
		rr := R.Range(12, 28)
		th0 := R.Range(0, tau) + (tau*3*t)/tornadoDuration
		span := R.Range(0.5, 1.1)
		for s := 0.0; s < span; s += 0.6 / rr {
			th := th0 - s
			col := D[2]
			if s < span*0.4 {
				col = D[3]
			}
			p.Dot(cx0+rr*math.Cos(th), 0.5+rr*0.18*math.Sin(th), col, 1-s/span)
		}
	}
}

// tornadoFlashAt returns the lightning intensity in the crown at time t.
func tornadoFlashAt(t float64) float64 {
	q := t / tornadoDuration
	return math.Max(0, 1-math.Abs(q-0.62)/0.025) + 0.6*math.Max(0, 1-math.Abs(q-0.69)/0.015)
}

func newTornado(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	D := pixelfx.Fit(o.Ramp("dust"), 5)
	L := pixelfx.Fit(o.Ramp("leaves"), 8)
	fh := o.Num("height")
	seed := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			g := tornadoGeoAt(fh, t)
			f := tornadoFlashAt(t)
			tornadoSkirt(p, D, g, t, seed*5+1, true)
			tornadoCrown(p, C, g, t, seed*7+2, true, f)
			tornadoBands(p, C, g, t, seed*3+5, true)
			tornadoDebris(p, L, g, t, seed*11+3, true, C)
			tornadoBody(p, C, D, g, t, seed)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			g := tornadoGeoAt(fh, t)
			f := tornadoFlashAt(t)
			cx := g.ax(1)
			tornadoBands(p, C, g, t, seed*3+5, false)
			tornadoCrown(p, C, g, t, seed*7+2, false, f)
			tornadoSkirt(p, D, g, t, seed*5+1, false)
			tornadoDebris(p, L, g, t, seed*11+3, false, C)
			// light: a soft pale column, a cold pool at its foot, the crown's lightning
			p.GlowLine(g.ax(0.1), -g.fh*0.1, cx, -g.fh*0.9, 26, C[5], 0.08)
			p.Glow(g.ax(0), -2, 30, D[3], 0.12)
			if f > 0.05 {
				R := pixelfx.RngOf(seed, 99, math.Floor(t/tornadoDuration*7))
				path := pixelfx.Bolt(R, cx-18, -g.fh-6, cx+16, -g.fh+2, 0.3, 3)
				col := C[7]
				if f > 0.6 {
					col = pixelfx.White
				}
				p.Polyline(path, col, math.Min(1, f*1.5))
				p.Glow(cx, -g.fh-2, 70, C[7], 0.45*f)
			}
		},
	}
}
