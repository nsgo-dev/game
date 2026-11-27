// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/slime.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the puddle (a domed wet surface with glints sliding along it, bubbles that swell and pop) from Sprite Fusion's open-source poison puddle (https://www.spritefusion.com/pixel-art-vfx/poison), MIT License, Copyright (c) 2026 Hugo Duprez.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Slime is a glossy slime hopping in place: it squashes to wind up, stretches into the jump, rounds out in the air, splats down flinging droplets that creep back, then wobbles and blinks. It is registered as "critters/slime".
var Slime = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Slime",
		Desc:     "A glossy slime hops in place: it squashes to wind up, stretches into the jump, rounds out in the air, splats down flinging droplets that creep back, then wobbles and blinks.",
		Tags:     []string{"critter", "slime", "monster", "hop", "goo", "loop"},
		Size:     [2]int{112, 88},
		Origin:   [2]int{56, 72},
		Duration: 1.8,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#061412", "#0b2f2b", "#12594a", "#1f8f4f", "#47c752", "#a6f06a", "#f0ffd2")},
			{Name: "hop", Default: pixelfx.Num(24)},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newSlime,
}

func init() { pixelfx.Register("critters/slime", Slime) }

var (
	slimeD                                 = 1.8
	slimeSq, slimeUp, slimeLift, slimeLand = 0.5, 0.84, 0.9, 1.44
	slimeSnap, slimePop                    = 0.965, 0.4
	slimeW0, slimeH0, slimeVC, slimeG      = 30.0, 22.0, 0.4, 640.0
	slimeAirCurve                          = [][2]float64{{0, 0.36}, {0.4, 0.02}, {0.55, -0.05}, {0.85, 0.08}, {1, 0.16}}
	slimeDrips                             = [2][2]float64{{1.02, -4}, {1.18, 3}}
	slimePopFlecks                         = [5][2]float64{{1, 0}, {-1, 0}, {0.6, -0.7}, {-0.6, -0.7}, {0, -1}}
)

const slimeDrops = 12

// slimePose is the body's shape and squash at a moment: s > 0 stretched tall, s < 0 squashed flat; by its lift, q its progress through the air (-1 on the floor), age the time since it landed.
type slimePose struct {
	s, by, lean, round, q, age, W, H float64
}

func slimePoseAt(t, hop float64) slimePose {
	age := pixelfx.Wrap(t-slimeLand, slimeD)
	var s, by, lean float64
	round, q := 0.1, -1.0
	switch {
	case t >= slimeLift && t < slimeLand:
		q = (t - slimeLift) / (slimeLand - slimeLift)
		by = -hop * 4 * q * (1 - q)
		s = curve(q, slimeAirCurve)
		round = 0.1 + 0.8*pixelfx.Smooth(0, 0.16, q)*(1-pixelfx.Smooth(0.86, 1, q))
	case t >= slimeUp && t < slimeLift:
		s = pixelfx.Lerp(-0.3, 0.36, pixelfx.Ease.OutCubic(pixelfx.Seg(t, slimeUp, slimeLift)))
	case t >= slimeSq && t < slimeUp:
		u := pixelfx.Seg(t, slimeSq, slimeUp)
		s = -0.3*pixelfx.Ease.InOutSine(u) + 0.018*math.Sin(t*tau*16)*pixelfx.Smooth(0.55, 1, u)
	default:
		// the splat holds for a beat, then rings out into a wobble; the top sways a little as it settles
		if age < 0.06 {
			s = -0.46
		} else {
			s = -0.46 * spring(age-0.06, 2.1, 4.6)
		}
		lean = 0.07 * math.Exp(-age*4) * math.Sin(age*tau*1.9)
		if t < slimeSq {
			s += 0.02 * math.Sin((t/slimeSq)*tau)
		}
	}
	return slimePose{s: s, by: by, lean: lean, round: round, q: q, age: age, W: slimeW0 / math.Sqrt(1+s), H: slimeH0 * (1 + s)}
}

// slimeHalfW is half the body's width at height v (0 base .. 1 top): a dome over a soft foot with rounded corners; -1 outside it.
func slimeHalfW(v float64, P *slimePose) float64 {
	if v < 0 || v > 1 {
		return -1
	}
	W, H, round := P.W, P.H, P.round
	if v >= slimeVC {
		q := (v - slimeVC) / (1 - slimeVC)
		return (W / 2) * math.Sqrt(1-q*q)
	}
	q, hb, rc := (slimeVC-v)/slimeVC, v*H, 3.0
	hw := (W / 2) * math.Sqrt(math.Max(0, 1-round*q*q))
	if hb < rc {
		hw -= rc - math.Sqrt(rc*rc-math.Pow(rc-hb, 2))
	}
	return hw
}

func slimeInside(P *slimePose, x, y float64) bool {
	v := (P.by - 1 - y) / P.H
	hw := slimeHalfW(v, P)
	return hw >= 0 && math.Abs(x-P.lean*v*P.H) <= hw
}

// drawSlimeBody shades the body the way a pixel artist shades jelly: a dark dome over light pooling low inside the body, a shadow crescent (the rim the body leaves bare when nudged toward the light), a glossy streak hugging the top-left contour with a hot spot under it, light bounced up off the floor caught just inside the lower rim.
func drawSlimeBody(g *grid, C []uint32, P *slimePose) {
	yb, half := P.by-1, P.W/2+3
	cx, cy := P.lean*0.45*P.H, yb-P.H*0.5
	lx, ly := cx-P.W*0.24, cy-P.H*0.36
	in := func(x, y float64) bool { return slimeInside(P, x, y) }
	lit := func(x, y, k float64) bool { return in(lx+(x-lx)/k, ly+(y-ly)/k) }
	g.clear()
	g.shade(-half, yb-P.H-1, half, yb, func(x, y float64, o *gridOut) uint32 {
		if !in(x, y) {
			return 0
		}
		o.t, o.a = 1, 0.92
		i := 3
		if lit(x, y, 0.42) {
			i = 5
		} else if lit(x, y, 0.7) {
			i = 4
		}
		// light pooled in the jelly along the bottom, brightest just inside the rim
		if v := (yb - y) / P.H; v < 0.3 && in(x, y+1.4) {
			if !in(x, y+2.4) {
				i = 5
			} else if v < 0.2 {
				i = 4
			}
		}
		if !in(x+2, y+2.4) {
			i, o.a = 2, 1
		}
		ang := math.Atan2(y-cy, (x-cx)*(P.H/P.W))
		if !in(x-1.6, y-1.6) && in(x-0.7, y-0.7) && ang > -2.75 && ang < -1.55 {
			i, o.a = 5, 1
			if ang > -2.45 && ang < -1.8 {
				i = 6
			}
		}
		if i == 2 && in(x+1, y+1.2) && !in(x+2, y+2.4) && ang > 0.3 && ang < 1.5 {
			i = 4
		}
		return C[i]
	})
	// the hot spot: a tiny bright window reflection under the streak
	hx, hy := pixelfx.Round(cx-P.W*0.27), pixelfx.Round(cy-P.H*0.14)
	if in(hx-1, hy) && in(hx+1, hy+1) {
		g.put(hx, hy, C[6])
		g.put(hx, hy+1, C[5])
	}
}

// drawSlimeFace draws the eyes riding the squash: squint while winding up, wide in the air, screwed shut on the splat, a blink at rest.
func drawSlimeFace(g *grid, C []uint32, P *slimePose, t float64) {
	yb, ev := P.by-1, 0.47
	ey, ex, xc := pixelfx.Round(yb-P.H*ev), math.Max(3, pixelfx.Round(P.W*0.16)), pixelfx.Round(P.lean*ev*P.H)
	air := P.q >= 0
	shut, blink := P.age < 0.16 && !air, t > 0.22 && t < 0.29
	wind, look := 0.0, 0.0
	if t >= slimeSq && t < slimeLift {
		wind = pixelfx.Seg(t, slimeSq, slimeUp)
	}
	if air {
		if P.q < 0.45 {
			look = -1
		} else if P.q > 0.7 {
			look = 1
		}
	}
	wide := 0.0
	if air && P.q < 0.6 {
		wide = 1
	}
	eh := pixelfx.ClampTo(pixelfx.Round(4*(1+P.s*0.8)-wind*2+wide), 1, 5)
	for _, side := range [2]float64{-1, 1} {
		x := xc + side*ex
		if side > 0 {
			x--
		}
		if shut {
			a := 1.0
			if side < 0 {
				a = 0
			}
			b := 1 - a
			g.put(x+a, ey-1, C[0])
			g.put(x+b, ey, C[0])
			g.put(x+a, ey+1, C[0])
			continue
		}
		if blink {
			g.put(x, ey+1, C[0])
			g.put(x+1, ey+1, C[0])
			continue
		}
		top := ey + 2 - eh + look
		for k := 0.0; k < eh; k++ {
			g.put(x, top+k, C[0])
			g.put(x+1, top+k, C[0])
		}
		if look > 0 {
			g.put(x+1, top+1, C[6])
		} else {
			g.put(x, top, C[6])
		}
		if eh >= 4 {
			g.put(x+1, top+eh-1, C[2])
		}
	}
	my := pixelfx.Round(yb - P.H*0.31)
	if air && P.q < 0.75 {
		g.put(xc, my, C[0])
		g.put(xc-1, my, C[0])
		g.put(xc, my+1, C[1])
		g.put(xc-1, my+1, C[1])
	} else if !shut {
		g.put(xc, my, C[1])
		g.put(xc-1, my, C[1])
	}
}

// drawSlimeBubble draws a bubble working its way up through the jelly while it rests; it pops at the top.
func drawSlimeBubble(g *grid, C []uint32, P *slimePose, t float64) {
	if t > slimePop {
		return
	}
	v := pixelfx.Lerp(0.18, 0.86, pixelfx.Ease.InQuad(t/slimePop))
	hw := slimeHalfW(v, P)
	x, y := P.lean*v*P.H-hw*pixelfx.Lerp(0.35, 0.05, t/slimePop), P.by-1-v*P.H
	g.put(x, y, C[6])
	g.put(x+1, y, C[5])
	g.put(x, y+1, C[5])
	g.put(x+1, y+1, C[4])
}

// slimeDrop is a droplet flung off the splat: its flight, size, and once landed how much of it is left (k), whether it has rejoined the body and how flat its splat still is.
type slimeDrop struct {
	flight
	size     int
	k, splat float64
	gone     bool
}

// slimeDropAt is droplet i: it arcs, lands with a little splat, and creeps back to the body to rejoin it.
func slimeDropAt(i int, seed, age float64) slimeDrop {
	r := pixelfx.RngOf(seed, float64(i), 7)
	a, sp := -pi/2+r.Range(-1.2, 1.2), r.Range(60, 118)
	size := 3
	if r.Chance(0.35) {
		size = 1
	} else if r.Chance(0.7) {
		size = 2
	}
	fa := a + pi/2
	x0, y0 := math.Sin(fa)*21, -1-math.Cos(fa)*12.5
	f := slimeDrop{flight: toss(x0, y0, math.Cos(a)*sp*0.75, math.Sin(a)*sp, age, slimeG, -1), size: size}
	if f.landed {
		lx := f.lx
		if lx == 0 || math.IsNaN(lx) {
			lx = 1
		}
		home := pixelfx.Sign(lx) * (slimeW0/2 - 2)
		dur := math.Min(0.55, 0.22+math.Abs(f.lx-home)/60)
		u := pixelfx.Seg(age-f.at-0.14, 0, dur)
		f.x = pixelfx.Lerp(f.lx, home, pixelfx.Ease.InOutSine(u))
		f.k, f.gone, f.splat = 1-u, u >= 1, pixelfx.Clamp(1-(age-f.at)/0.14)
	}
	return f
}

// slimePuddleR is the wet patch it sits in: wider while squashed, splashed out on landing, left behind while it jumps.
func slimePuddleR(t float64, P *slimePose) float64 {
	if P.q >= 0 {
		return (slimeW0/math.Sqrt(1.36)/2 + 3) * (1 - 0.22*P.q)
	}
	k := 0.0
	if t >= slimeLand || t < slimeSq {
		k = 1
	}
	return P.W/2 + 3 + 9*math.Exp(-P.age*5)*(1-math.Exp(-P.age*40))*k
}

func newSlime(o *pixelfx.Options) pixelfx.Instance {
	C, hop, seed := pixelfx.Fit(o.Palette(), 7), o.Num("hop"), o.Seed()
	g := newGrid(-30, -64, 60, 66)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, time float64) { drawSlimeBack(p, C, hop, seed, time) },
		DrawFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, slimeD, seed)
			P := slimePoseAt(t, hop)
			yb, age := P.by-1, P.age
			drawSlimeBody(g, C, &P)
			drawSlimeFace(g, C, &P, t)
			drawSlimeBubble(g, C, &P, t)
			g.outlineFn(func(_, y, nx, ny float64) uint32 {
				if ny < 0 && nx <= 0 && y < yb-P.H*0.5 {
					return C[1]
				}
				return C[0]
			}, 1)
			g.draw(p, 1, 0, 0)

			// the bubble pops out of the top: a ring and two flecks
			if t >= slimePop && t < slimePop+0.16 {
				k := (t - slimePop) / 0.16
				x, y := P.lean*P.H-slimeHalfW(0.86, &P)*0.05, yb-P.H-0.5
				for a := 0.0; a < tau; a += 0.6 {
					p.Dot(x+math.Cos(a)*(1+k*3), y+math.Sin(a)*(1+k*3)*0.7, C[6], 1.2-k)
				}
				for _, s := range [2]float64{-1, 1} {
					p.Dot(x+s*(1+k*4), y-1-k*6+k*k*4, C[5], 1.3-k)
				}
				p.Glow(x, y, 6, C[5], 0.4*(1-k))
			}
			// droplets in flight
			for i := 0; i < slimeDrops; i++ {
				f := slimeDropAt(i, seed, age)
				if f.landed || slimeInside(&P, f.x, f.y) {
					continue
				}
				sv := pixelfx.Hypot(f.vx, f.vy)
				if sv == 0 {
					sv = 1
				}
				ux, uy, L := f.vx/sv, f.vy/sv, math.Min(2.5, sv/50)
				p.Line(f.x-ux*L, f.y-uy*L, f.x, f.y, C[3], 1)
				if f.size >= 2 {
					p.Px(f.x-1, f.y, C[4])
					p.Px(f.x-1, f.y+1, C[3])
					p.Px(f.x, f.y+1, C[2])
				}
				if f.size >= 3 {
					p.Px(f.x+1, f.y, C[3])
					p.Px(f.x-1, f.y-1, C[4])
					p.Px(f.x, f.y-1, C[5])
				}
				c := C[5]
				if f.size >= 2 {
					c = C[6]
				}
				p.Px(f.x, f.y, c)
				p.Glow(f.x, f.y, 5+float64(f.size), C[4], 0.32)
			}
			// spores drifting up out of the puddle, glowing
			pixelfx.Cycle(t, slimeD, 7, seed*13+1, func(r *pixelfx.Rng) float64 { return r.Range(1.2, 1.8) }, func(ag, L float64, r *pixelfx.Rng, i int) {
				k, fi := ag/L, float64(i)
				x := r.Range(-24, 24) + math.Sin(ag*2.1+fi*1.7)*3
				y := -2 - r.Range(9, 17)*ag - 5*ag*ag
				a, tw, big := math.Min(1, k*4)*math.Min(1, (1-k)*2.5), 0.6+0.4*math.Sin(ag*7+fi*2.3), r.Chance(0.35)
				c := C[5]
				if tw > 0.8 {
					c = C[6]
				}
				p.Dot(x, y, c, a)
				gr, gk := 6.0, 0.32
				if big {
					p.Dot(x+1, y, C[5], a)
					p.Dot(x, y+1, C[4], a)
					p.Dot(x+1, y+1, C[4], a*0.6)
					gr, gk = 9, 0.45
				}
				p.Glow(x, y, gr, C[5], gk*a*tw)
			})
			// drips let go of its underside while it's in the air and plop into the puddle
			for _, d := range slimeDrips {
				t0, dx := d[0], d[1]
				a2 := pixelfx.Wrap(t-t0, slimeD)
				if a2 > 0.6 {
					continue
				}
				P0 := slimePoseAt(t0, hop)
				f := toss(dx, P0.by-0.5, 0, 12, a2, slimeG, -1)
				if !f.landed {
					p.Px(dx, f.y, C[5])
					p.Px(dx, f.y-1, C[4])
					p.Glow(dx, f.y, 4, C[4], 0.3)
				} else if a2-f.at < 0.14 {
					k := (a2 - f.at) / 0.14
					for _, s2 := range [2]float64{-1, 1} {
						p.Dot(dx+s2*(1+k*3), -2-k*3+k*k*3, C[5], 1.2-k)
					}
				}
			}
			// a glint on the dome at the top of the hop
			if P.q > 0.4 && P.q < 0.64 {
				k := math.Sin(pi * pixelfx.Seg(P.q, 0.4, 0.64))
				pixelfx.Sparkle(p, P.lean*0.8*P.H-P.W*0.21, yb-P.H*0.8, 1+2.5*k, C[6], C[5], 0.5*k)
			}
			// light: the slime glows through and tints the air round it, the puddle lights the floor, a flash on the splat
			cx, cy, near := P.lean*P.H*0.4, yb-P.H*0.45, pixelfx.Clamp(1+P.by/(hop*1.1))
			R, br := slimePuddleR(t, &P), 0.9+0.1*math.Sin((t/slimeD)*tau*2)
			p.Glow(cx*0.5, -14, 64, C[3], 0.3*br)
			p.Glow(cx, cy, 30+P.W*0.3, C[4], 0.2)
			p.Glow(cx, cy+3, 14, C[5], 0.16)
			p.Glow(0, -2, R*1.3+6, C[4], 0.12+0.08*near)
			if t >= slimeLand || t < slimeSq {
				p.Glow(0, -3, 40, C[5], 0.42*math.Exp(-age*9))
			}
		},
	}
}

func drawSlimeBack(p *pixelfx.Painter, C []uint32, hop, seed, time float64) {
	t := shift(time, slimeD, seed)
	P := slimePoseAt(t, hop)
	age := P.age
	// the puddle: a low dome of goo, lit along its top with glints sliding over it, darker where it meets the floor
	R := slimePuddleR(t, &P)
	hp := math.Min(2.4, 0.9+R*0.06)
	for x := math.Floor(-R); x <= math.Ceil(R); x++ {
		d := math.Abs(x+0.5) / R
		if d > 1 {
			continue
		}
		top := -1 - pixelfx.Round(math.Sqrt(1-d*d)*hp)
		for y := top; y <= 0; y++ {
			c := C[4]
			if y == top {
				c = C[5]
			} else if y == 0 {
				c = C[2]
			}
			if d > 0.88 {
				c = C[3]
				if y == top {
					c = C[4]
				}
			}
			if y == top && d < 0.88 {
				if n := lnoise(t, slimeD, 7, x*0.18, 3) + lnoise(t, slimeD, 4, x*0.07, 4)*0.6; n > 0.55 {
					c = C[6]
				}
			} else if y == top+1 && d < 0.8 && lnoise(t, slimeD, 3, x*0.12, 5) > 0.3 {
				c = C[5]
			}
			p.Px(x, y, c)
		}
		if d > 0.96 {
			p.Px(x, top-1, C[1])
		}
	}
	// bubbles swell out of the puddle and pop, flicking flecks
	pixelfx.Cycle(t, slimeD, 4, seed*17+3, func(r *pixelfx.Rng) float64 { return r.Range(0.45, 0.75) }, func(ag, L float64, r *pixelfx.Rng, _ int) {
		x, k := r.Range(-0.85, 0.85)*R, ag/L
		rb := r.Range(1.4, 2.6) * (0.3 + 0.7*pixelfx.Ease.OutCubic(math.Min(1, k*1.25)))
		if math.Abs(x) < P.W/2+1 && P.q < 0 {
			return
		}
		sy := -1 - pixelfx.Round(math.Sqrt(math.Max(0, 1-math.Pow(x/R, 2)))*hp)
		if k < 0.85 {
			for y := math.Floor(sy - rb); y <= sy; y++ {
				for xx := math.Floor(x - rb - 1); xx <= math.Ceil(x+rb+1); xx++ {
					dx, dy := xx+0.5-x, y+0.5-sy
					dd := pixelfx.Hypot(dx, dy)
					if dd > rb+0.5 {
						continue
					}
					if dd > rb-0.7 {
						c := C[4]
						if dx < 0 && dy < -rb*0.3 {
							c = C[6]
						}
						p.Px(xx, y, c)
					} else {
						p.Blend(xx, y, C[5], 0.3)
					}
				}
			}
			p.Glow(x, sy-rb*0.5, rb*2+3, C[5], 0.2)
			return
		}
		q := (k - 0.85) / 0.15
		for _, f := range slimePopFlecks {
			p.Dot(x+f[0]*(2+3*q), sy+f[1]*(2+3*q), C[6], 1.2-q)
		}
	})
	// landed droplets: a flattened splat, then a bead of goo creeping home, a wet trail behind it
	for i := 0; i < slimeDrops; i++ {
		f := slimeDropAt(i, seed, age)
		if !f.landed || f.gone {
			continue
		}
		x, w := pixelfx.Round(f.x), f.size
		if f.splat > 0 {
			w++
		}
		for j := 0; j < w; j++ {
			c := C[4]
			if j == 0 {
				c = C[5]
			}
			p.Px(x-float64(w>>1)+float64(j), -1, c)
		}
		if f.size >= 2 && f.splat <= 0 {
			p.Px(x, -2, C[5])
			p.Px(x-1, -2, C[4])
		}
		p.Px(x, 0, C[3])
		dir := pixelfx.Sign(f.lx - f.x)
		for j := 1.0; j < math.Min(8, math.Abs(f.lx-f.x)); j++ {
			p.Dot(x+dir*j, 0, C[2], 0.7-j*0.08)
		}
		p.Glow(x, -1, 5, C[4], 0.25*f.k+0.1)
	}
	// the goo string it pulls off the floor as it leaves; when it snaps, a drip falls back and the rest pulls up
	if t >= slimeLift && t < slimeSnap {
		bot, u := P.by-1, (t-slimeLift)/(slimeSnap-slimeLift)
		for y := math.Floor(bot); y < 0; y++ {
			m := math.Sin(((y - bot) / -bot) * pi)
			p.Px(-1, y, C[4])
			if m*u < 0.45 {
				p.Px(0, y, C[3])
			}
		}
	} else if t >= slimeSnap && t < slimeSnap+0.3 {
		a2, ys := t-slimeSnap, slimePoseAt(slimeSnap, hop).by-1
		f := toss(-1, ys*0.45, 0, 0, a2, slimeG*1.3, -2)
		if !f.landed {
			p.Px(-1, f.y, C[5])
			p.Px(-1, f.y-1, C[3])
		}
		nub := 3 * (1 - a2/0.12)
		for j := 0.0; j < nub; j++ {
			c := C[4]
			if j == 0 {
				c = C[3]
			}
			p.Px(-1, P.by+j, c)
		}
	}
}
