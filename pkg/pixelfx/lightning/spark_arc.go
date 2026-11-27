// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/spark-arc.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The arc's re-struck bowed shape, its halo-body-core inking, its flicker and the nodes where it roots follow Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// SparkArc is a climbing arc between two rods: it strikes white at their narrow foot, climbs on rising heat as it stretches and bows, sputters and snaps at the top, then strikes again. It is registered as "lightning/spark-arc".
var SparkArc = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spark arc",
		Desc:     "A climbing arc between two rods: it strikes white at their narrow foot, climbs on rising heat as it stretches and bows, sputters and snaps at the top, then strikes again.",
		Tags:     []string{"lightning", "electric", "arc", "jacobs-ladder", "machine", "prop", "loop"},
		Size:     [2]int{124, 120},
		Origin:   [2]int{62, 108},
		Duration: 2,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options:  append(voltOptions(), pixelfx.Option{Name: "seed", Default: pixelfx.Num(2)}),
	},
	New: newSparkArc,
}

func init() { pixelfx.Register("lightning/spark-arc", SparkArc) }

const (
	saFPS  = 30
	saN    = 60
	saP    = 2
	saSnap = 1.42
	saH    = 58
)

// saExp is the climb's exponent and saAnticipate when sparks start jumping the gap, variables so 1 / saExp and saP - 0.16 round as JavaScript's do.
var saExp, saAnticipate = 1.35, float64(saP)

var (
	saSteel   = pixelfx.Ramp("#151823", "#262c3b", "#3d4558", "#5f6a82", "#8f9bb2", "#d2daea")
	saCeramic = pixelfx.Ramp("#4b4458", "#857d96", "#c9c1d6", "#efe9f6")
	saAsh     = pixelfx.Hex("#6a6478")
)

// saRodAt returns where the arc roots on rod s (-1 left, 1 right) at height h above the gap: the rods are a V from the insulators up.
func saRodAt(s, h float64) (float64, float64) { return s * (4.5 + (h/60)*19.5), -9 - h }

// saHeight returns how high the arc has climbed at t.
func saHeight(t float64) float64 { return 1.5 + saH*math.Pow(pixelfx.Clamp(t/saSnap), saExp) }

// saSide returns the rod a burst's spark j leaves from.
func saSide(j int) float64 {
	if j%2 != 0 {
		return 1
	}
	return -1
}

func newSparkArc(o *pixelfx.Options) pixelfx.Instance {
	C, S, seed := pixelfx.Fit(o.Palette(), 8), o.Ramp("sparks"), o.Seed()
	St, Ce := saSteel, saCeramic
	anticipate := saAnticipate - 0.16
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		f := frameOf(t, saFPS, saN)
		r := pixelfx.RngOf(seed, 1, float64(f))
		climbing, h, u, a := t < saSnap, saHeight(t), pixelfx.Clamp(t/saSnap), t-saSnap
		// the floor and the stand: a pool of cold light while it burns, a plate, two ribbed insulators
		pool := 0.06
		if climbing {
			pool = 0.2 + 0.15*(1-u)
		}
		p.Glow(0, -14, 50, C[3], pool)
		p.Rect(-18, -3, 36, 3, St[1], 1)
		p.Line(-18, -3, 17, -3, St[3], 1)
		for _, s := range [2]float64{-1, 1} {
			for y := -9; y < -3; y++ {
				rib := y&1 == 0
				for x := 2; x <= 6; x++ {
					var c uint32
					switch {
					case rib && x < 4:
						c = Ce[3]
					case rib && x < 6:
						c = Ce[2]
					case rib:
						c = Ce[1]
					case x < 5:
						c = Ce[1]
					default:
						c = Ce[0]
					}
					px := s * float64(x)
					if s < 0 {
						px--
					}
					p.Px(px, float64(y), c)
				}
			}
		}
		// the rods: steel, a lit edge, glowing where the arc has passed and cooling behind it
		for _, s := range [2]float64{-1, 1} {
			for hh := 0.0; hh <= 60; hh += 0.5 {
				// when the arc passed this point; not yet this time round, it is still cooling from the last
				x, y := saRodAt(s, hh)
				tp := saSnap * math.Pow(pixelfx.Clamp((hh-1.5)/saH), 1/saExp)
				age := t - tp
				if t < tp {
					age = t - tp + saP
				}
				hot := 0.0
				if hh <= saH+2 {
					hot = math.Exp(-age / 0.26)
				}
				c := heat(hot*1.05 - 0.12)
				c0 := c
				if c0 == 0 {
					c0 = St[3]
					if s < 0 {
						c0 = St[4]
					}
				}
				p.Px(x, y, c0)
				var c1 uint32
				switch {
				case c != 0 && hot > 0.35:
					c1 = heat(hot * 0.6)
					if c1 == 0 {
						c1 = St[2]
					}
				case s < 0:
					c1 = St[2]
				default:
					c1 = St[1]
				}
				p.Px(x+s, y, c1)
				if hot > 0.3 && int(pixelfx.Round(hh*2))&3 == 0 {
					gc := c
					if gc == 0 {
						gc = C[4]
					}
					p.Glow(x, y, 5, gc, 0.25*hot)
				}
			}
			tx, ty := saRodAt(s, 60)
			p.Circle(tx+s*0.5, ty, 1.6, St[4], 1)
			p.Px(tx+s*0.5-0.5, ty-1, St[5])
		}
		// the arc: struck white at the foot, flickering as it climbs and stretches, sputtering at the top
		switch {
		case climbing:
			xl, yl := saRodAt(-1, h)
			xr, yr := saRodAt(1, h)
			first := t < 1.0/saFPS
			k0, sput := 1-0.22*u, 1.0
			if first {
				k0 = 1
			}
			if u > 0.88 && r.Next() < 0.25 {
				sput = 0.4
			}
			k := k0 * sput * (0.82 + 0.18*r.Next())
			bow := -(1.5 + 13*u*u) * (0.8 + 0.4*r.Next())
			s := 0.8
			if first {
				s = 0.95
			}
			stroke(p, bowed(r, xl, yl, xr, yr, bow, 0.18+0.12*u, 0.12+0.25*u), k, C, s, strokeOpts{hero: first, light: pixelfx.Val(0.7)})
			p.GlowLine(xl, yl+bow*0.5, xr, yr+bow*0.5, 12, C[4], 0.22*k)
			for _, e := range [2][2]float64{{xl, yl}, {xr, yr}} {
				p.Circle(e[0], e[1], 1.8+0.6*k, C[5], 1)
				p.Px(e[0], e[1], pixelfx.White)
				p.Glow(e[0], e[1], 12, C[5], 0.55*k)
			}
			if t < 0.1 {
				p.Glow(0, -11, 26, C[6], 0.45*(1-t/0.1))
			}
		case a < 2.0/saFPS:
			// the snap: two stubs left clinging to the rods, a flash where it broke
			xl, yl := saRodAt(-1, saH+1.5)
			xr, yr := saRodAt(1, saH+1.5)
			for _, e := range [2][3]float64{{xl, yl, 1}, {xr, yr, -1}} {
				x0, y0, s := e[0], e[1], e[2]
				crackle(p, zigzag(r, x0, y0, x0+s*r.Range(5, 9), y0-r.Range(2, 6), 0.4, 0.3), 0.7, C, 0.4, 1)
			}
			p.Glow(0, yl-8, 30, C[4], 0.5)
		case t > anticipate:
			// anticipation: little sparks jump the narrow gap before it strikes again
			if r.Next() < 0.7 {
				crackle(p, zigzag(r, -4.5, -10.5-r.Range(0, 2), 4.5, -10.5-r.Range(0, 2), 0.5, 0), 0.4+0.3*r.Next(), C, 0.35, 1)
			}
		}
		// ozone and smoke rise off the arc while it climbs, and from where it snapped
		for j := range 3 {
			fj := float64(j)
			born := ((fj + 1) / 3.2) * saSnap
			age, life := pixelfx.Wrap(t-born, saP), 1.0
			if age >= life {
				continue
			}
			y0 := -9 - saHeight(born) - 6
			x0 := -3.0
			if j%2 != 0 {
				x0 = 3
			}
			puff(p, x0+math.Sin(age*3+fj)*2, y0-age*20, 1.8+fj*0.3, age/life, pixelfx.Mix(saAsh, C[5], 0.3))
		}
		// sparks: thrown from the roots as it strikes, sprayed up where it snaps, molten drips off the rods
		burst := func(t0 float64, n int, salt float64, from func(q *pixelfx.Rng, j int) (x0, y0, a0, spread, sp0, sp1 float64)) {
			age := pixelfx.Wrap(t-t0, saP)
			for j := range n {
				q := pixelfx.RngOf(seed, salt, float64(j))
				life := q.Range(0.25, 0.6)
				if age >= life {
					continue
				}
				x0, y0, a0, spread, sp0, sp1 := from(q, j)
				ang := a0 + q.Range(-spread, spread)
				sp := q.Range(sp0, sp1)
				x, y, vx, vy := flight(x0, y0, math.Cos(ang)*sp, math.Sin(ang)*sp, age, 0)
				spark(p, x, y, vx, vy, 1-age/life, S)
			}
		}
		burst(0, 12, 11, func(_ *pixelfx.Rng, j int) (float64, float64, float64, float64, float64, float64) {
			x, y := saRodAt(saSide(j), 1.5)
			return x, y, -pi/2 + 0.6*saSide(j), 1, 60, 170
		})
		burst(saSnap, 14, 12, func(q *pixelfx.Rng, _ int) (float64, float64, float64, float64, float64, float64) {
			x, _ := saRodAt(1, saH)
			return x * q.Range(-0.8, 0.8), -9 - saH - 6, -pi / 2, 1.3, 50, 160
		})
		for k, tt := range [3]float64{0.45, 0.8, 1.15} {
			burst(tt, 2, 20+float64(k), func(_ *pixelfx.Rng, j int) (float64, float64, float64, float64, float64, float64) {
				x, y := saRodAt(saSide(j), saHeight(tt))
				return x, y, pi / 2, 0.5, 5, 30
			})
		}
		if climbing {
			p.Glow(0, -9-h-3, 26, C[4], 0.25)
		} else if a < 0.25 {
			p.Glow(0, -9-h-3, 26, C[4], 0.4*(1-a/0.25))
		}
	}}
}
