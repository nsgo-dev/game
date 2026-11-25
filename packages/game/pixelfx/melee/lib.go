// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent and trace recipe from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Shared by the melee effects: swings on a tilted circle (the smear, its dotted trace), floor cracks, rocks.

var (
	outline = pixelfx.Hex("#1a1422")
	// pi is π as a variable, so sums with it round as pixelfx's do
	pi = math.Pi
	// floor is the flat floor every melee effect's particles and debris land on
	floor = func(float64) float64 { return 0 }
)

// swing is a circle of radius R around (cx, cy), squashed to sy across its plane (1 stands up facing us, 0.16 lies on the floor) and turned by rot, swept from angle a0 in direction dir (+1 turns clockwise on screen). k lifts the band into a shallow cone: a point dr inside the rim sits k * dr higher, so a level ring shows its near half broad and its far half thin, like a blade angled down from the hilt (keep k below sy). Positions along it are u, radians from a0 in the swing's direction.
//
// A slash along it also says its span (radians), its width W, when it starts (t0), how long its edge whips out (sweep), holds (hold) and its tail follows (trail), its ramp (dark to bright), how hot it flashes (hot, 0..1) and whether it is a lens, thickest in the middle rather than just behind the edge.
type swing struct {
	cx, cy, R, sy, rot, a0, dir, k float64

	span, W, t0, sweep, hold, trail float64
	ramp                            []uint32
	hot                             float64
	lens                            bool
}

// through returns a swing on radius R whose arc passes through (x, y) heading along angle th, that point at of the way along span (pixelfx's default for at is 0.55).
func through(x, y, th, dir, R, span, at float64) swing {
	a := th - (dir*pi)/2
	return swing{cx: x - math.Cos(a)*R, cy: y - math.Sin(a)*R, R: R, sy: 1, rot: 0, a0: a - dir*span*at, dir: dir, span: span}
}

// onSwing returns the screen point at plane radius r, u radians along the swing.
func onSwing(s *swing, r, u float64) (float64, float64) {
	a := s.a0 + s.dir*u
	lx, ly := math.Cos(a)*r, math.Sin(a)*r*s.sy-s.k*(s.R-r)
	c, n := math.Cos(s.rot), math.Sin(s.rot)
	return s.cx + lx*c - ly*n, s.cy + lx*n + ly*c
}

// travelAt is where a swing is at some u, the on-screen unit direction it travels there, and the in-plane angle there (sin < 0 is the far half).
type travelAt struct{ x, y, dx, dy, a float64 }

// travel returns where the swing is at u and which way it travels.
func travel(s *swing, u float64) travelAt {
	x0, y0 := onSwing(s, s.R, u)
	x1, y1 := onSwing(s, s.R, u+0.01)
	l := pixelfx.Hypot(x1-x0, y1-y0)
	if l == 0 {
		l = 1
	}
	return travelAt{x: x0, y: y0, dx: (x1 - x0) / l, dy: (y1 - y0) / l, a: s.a0 + s.dir*u}
}

// plane returns the plane radius and angle under a screen point (it inverts onSwing, cone included).
func plane(s *swing, x, y float64) (float64, float64) {
	c, n := math.Cos(s.rot), math.Sin(s.rot)
	dx, dy := x-s.cx, y-s.cy
	lx, ly := dx*c+dy*n, -dx*n+dy*c
	if s.k == 0 {
		return pixelfx.Hypot(lx, ly/s.sy), math.Atan2(ly/s.sy, lx)
	}
	// r sin(a) sy = m - k r, r cos(a) = lx  =>  r^2 (sy^2 - k^2) + 2 m k r - (lx^2 sy^2 + m^2) = 0
	m := ly + s.k*s.R
	A, B, Cc := s.sy*s.sy-s.k*s.k, 2*m*s.k, lx*lx*s.sy*s.sy+m*m
	r := (-B + math.Sqrt(B*B+4*A*Cc)) / (2 * A)
	return r, math.Atan2((m-s.k*r)/s.sy, lx)
}

// halfOf is the half of a swing angle a lies in: -1 the far half, 1 the near half.
func halfOf(a float64) float64 {
	if math.Sin(a) < 0 {
		return -1
	}
	return 1
}

// smear shades a swing's band from u0 to u1 (u1 - u0 at most a full turn), plane radius R + pad down to R - wMax - 1. fn(u, r, a, out) shades each pixel: u along the swing, r its plane radius, a the in-plane angle. half: 0 the whole band, -1 only the far half (behind a character), 1 only the near half. (pixelfx's defaults are half 0 and pad 1.)
func smear(p *pixelfx.Painter, s *swing, u0, u1, wMax float64, fn func(u, r, a float64, out *pixelfx.Out), half, pad float64) {
	if u1-u0 < 0.005 {
		return
	}
	c, n := math.Cos(s.rot), math.Sin(s.rot)
	A := s.R + pad + 1
	Bn := A*s.sy + s.k*(wMax+2)
	hx, hy, rIn := pixelfx.Hypot(A*c, Bn*n)+1, pixelfx.Hypot(A*n, Bn*c)+1, s.R-wMax-1
	// each half only needs its side of the centre (plus what the tilt and the cone lift can reach across)
	over := math.Abs(n)*A + s.k*(wMax+2) + 2
	y0, y1 := s.cy-hy, s.cy+hy
	if half > 0 {
		y0 = s.cy - over
	}
	if half < 0 {
		y1 = s.cy + over
	}
	p.Shade(s.cx-hx, y0, s.cx+hx, y1, func(x, y float64, out *pixelfx.Out) uint32 {
		r, a := plane(s, x, y)
		if r > s.R+pad || r < rIn {
			return 0
		}
		if half != 0 && halfOf(a) != half {
			return 0
		}
		u := math.Mod(s.dir*(a-s.a0), pixelfx.Tau)
		if u < 0 {
			u += pixelfx.Tau
		}
		for u < u0 {
			u += pixelfx.Tau
		}
		if u > u1 {
			return 0
		}
		fn(u, r, a, out)
		return 0
	})
}

// constK returns a function of u that is always k, for trace and glowAlong.
func constK(k float64) func(float64) float64 { return func(float64) float64 { return k } }

// trace draws dots along a swing at plane radius r from u0 to u1, every step px, at alpha a(u) (dithered into a dotted trace). (pixelfx's defaults are half 0 and step 0.6.)
func trace(p *pixelfx.Painter, s *swing, r, u0, u1 float64, c uint32, a func(u float64) float64, half, step float64) {
	du := step / math.Max(4, r)
	for u := u0; u <= u1; u += du {
		g := s.a0 + s.dir*u
		if half != 0 && halfOf(g) != half {
			continue
		}
		x, y := onSwing(s, r, u)
		if k := a(u); k > 0 {
			p.Dot(x, y, c, k)
		}
	}
}

// glowAlong lays glow of radius rad along a swing from u0 to u1, k(u) strong (light along the arc). (pixelfx's defaults are half 0 and step 0.35.)
func glowAlong(p *pixelfx.Painter, s *swing, r, u0, u1, rad float64, c uint32, k func(u float64) float64, half, step float64) {
	for u := u0; u <= u1; u += step {
		g := s.a0 + s.dir*u
		if half != 0 && halfOf(g) != half {
			continue
		}
		x, y := onSwing(s, r, u)
		if kk := k(u); kk > 0.004 {
			p.Glow(x, y, rad, c, kk)
		}
	}
}

// slash draws a slash crescent along swing s at time t (Sprite Fusion's recipe): the edge whips out over s.span radians in s.sweep seconds, holds, then the tail follows over s.trail, thinning and cooling down s.ramp (dark to bright); a dotted trace lingers where it passed. half picks the half of it to draw, as smear's does.
func slash(p *pixelfx.Painter, s *swing, t, half float64) {
	u := t - s.t0
	if u < 0 {
		return
	}
	R, n := s.ramp, len(s.ramp)
	life, hot := s.sweep+s.hold+s.trail, s.hot
	head, tail, age := pixelfx.Ease.OutExpo(pixelfx.Clamp(u/s.sweep)), pixelfx.Smooth(s.sweep+s.hold, life, u), pixelfx.Clamp(u/life)
	U, T, W := s.span*head, s.span*tail, s.W*(1-tail*0.55)
	if tail < 1 && U-T > 0.01 {
		pw, pa, aDiv := 2.2, 0.6, 0.4
		if s.lens {
			pw, pa, aDiv = 1, 0.75, 0.1
		}
		smear(p, s, T, U, W, func(uu, r, _ float64, out *pixelfx.Out) {
			v := (uu - T) / (U - T)
			w := W * math.Pow(math.Sin(pi*math.Pow(v, pw)), pa)
			if w < 0.35 {
				return
			}
			q := (s.R - r) / w
			if q < 0 || q > 1 {
				return
			}
			ci := 0.0
			if !((q < 0.22 && age < 0.5) || q < hot*0.7) {
				ci = pixelfx.Round(q*float64(n-1)*0.75 + age*2.4 - v*0.8 + 0.4 - hot*3)
			}
			out.C = R[n-1-int(pixelfx.ClampTo(ci, 0, float64(n-1)))]
			out.A = math.Min(1, v/aDiv)
		}, half, 1)
		glowAlong(p, s, s.R-W*0.4, T, U, 5+W, R[n-3], func(uu float64) float64 { return (0.22 + hot*0.3) * (1 - age) * ((uu - T) / (U - T)) }, half, 0.3)
	}
	th := 0.7 * (1 - pixelfx.Clamp((u-s.sweep-s.hold)/(s.trail+0.2)))
	if tail > 0.02 && th > 0 {
		trace(p, s, s.R-0.5, 0, T, R[2], func(uu float64) float64 { return th * (0.4 + 0.6*uu/s.span) }, half, 0.6)
	}
}

// slashEnd returns where a slash's edge ends up, and which way it is travelling there.
func slashEnd(s swing) travelAt {
	s.R -= s.W * 0.3
	return travel(&s, s.span)
}

// shavings flings n shavings off a slash's edge, outward from its circle, in colors (Sprite Fusion's slash). from is how far along the span they start (pixelfx's default is 0.3).
func shavings(ps *pixelfx.Particles, R *pixelfx.Rng, s *swing, n int, colors []uint32, from float64) {
	for range n {
		x, y := onSwing(s, s.R, s.span*R.Range(from, 1))
		ox, oy := x-s.cx, y-s.cy
		l := pixelfx.Hypot(ox, oy)
		if l == 0 {
			l = 1
		}
		sp := R.Range(30, 100)
		ps.Spawn(pixelfx.Mote, x, y, (ox/l)*sp, math.Min(25, (oy/l)*sp)-10, R.Range(0.25, 0.55), pixelfx.Pick(R, colors), 0.6, 0, 0)
	}
}

// stoneCols are stone shades for rocks, grit and dust from one body color (as Sprite Fusion's explosion does).
type stoneCols struct{ light, body, shade, dust, dark uint32 }

func stone(c uint32) stoneCols {
	return stoneCols{
		light: pixelfx.Mix(pixelfx.Scale(c, 1.35), pixelfx.White, 0.08),
		body:  c,
		shade: pixelfx.Scale(c, 0.64),
		dust:  pixelfx.Mix(pixelfx.Scale(c, 1.2), pixelfx.RGB(150, 140, 150), 0.3),
		dark:  pixelfx.Scale(c, 0.42),
	}
}

// rock draws a tumbling rock (a Chunk): a lit side, a shaded side, an outline, fading at the end of its life.
func rock(p *pixelfx.Painter, b *pixelfx.Bit, st stoneCols) {
	r, fade := 0.6+b.S*0.75, math.Min(1, b.Life/0.5)
	lx, ly := math.Cos(b.A)*r*0.4, math.Sin(b.A)*r*0.4
	p.Shade(b.X-r-1, b.Y-r-1, b.X+r+1, b.Y+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		d := pixelfx.Hypot(dx*1.15, dy)
		if d > r+0.9 {
			return 0
		}
		lit := dx*lx + dy*ly
		switch {
		case d > r:
			out.C = outline
		case lit < -0.4:
			out.C = st.light
		case lit > 0.6:
			out.C = st.shade
		default:
			out.C = st.body
		}
		out.A = fade
		return 0
	})
}

// crackLine is one jagged crack in the floor: its points, how far out it starts (0..1 of the reach) and whether it is a fork.
type crackLine struct {
	pts  [][2]float64
	at   float64
	fork bool
}

// cracks returns jagged cracks splitting the floor from x0: each runs out along the floor's edge and zig-zags down into its face (y from 0 to about depth), with short forks. n cracks alternate sides (bias > 0 sends more forward). (pixelfx's defaults are depth 7 and bias 0.3.)
func cracks(R *pixelfx.Rng, x0 float64, n int, reach, depth, bias float64) []crackLine {
	var out []crackLine
	for i := range n {
		side := -1.0
		if R.Chance(0.5 + bias*0.5) {
			side = 1
		}
		k := 0.7
		if i < 2 {
			k = 1
		}
		L := reach * R.Range(0.45, 1) * k
		pts := [][2]float64{{x0 + side*R.Range(0, 1.5), 0}}
		x, y := pts[0][0], 0.0
		for math.Abs(x-x0) < L && y < depth {
			x += side * R.Range(1.5, 3.5)
			f := 0.3
			if math.Abs(x-x0) > 3 {
				f = 1
			}
			y = math.Min(depth, math.Max(0, y+R.Range(-0.6, 1.6)*f))
			pts = append(pts, [2]float64{pixelfx.Round(x), pixelfx.Round(y)})
			if R.Chance(0.18) && len(pts) > 3 && y < depth-2 {
				f := [][2]float64{{pixelfx.Round(x), pixelfx.Round(y)}}
				fx, fy := x, y
				for j := R.Int(1, 3); j > 0; j-- {
					fx += side * R.Range(1, 3)
					fy += R.Range(0.8, 2)
					f = append(f, [2]float64{pixelfx.Round(fx), pixelfx.Round(math.Min(depth+1, fy))})
				}
				out = append(out, crackLine{pts: f, at: math.Abs(x-x0) / reach, fork: true})
			}
		}
		out = append(out, crackLine{pts: pts})
	}
	return out
}

// spike is one tapered spike of an impact star: its angle, length and root half-width.
type spike struct{ a, len, w float64 }

// star draws an impact star (after Sprite Fusion's impact): tapered spikes around (x, y), drawn in three passes (rim, body, core) with a hot disc in the middle. sc stretches, th thins.
func star(p *pixelfx.Painter, x, y float64, spikes []spike, sc, th float64, core, body, inner, rim uint32) {
	for pass := range 3 {
		for _, s := range spikes {
			c, sn, L := math.Cos(s.a), math.Sin(s.a), s.len*sc
			for d := 0.0; d <= L; d += 0.5 {
				hw0 := s.w * th * (1 - d/L) * math.Min(1, 0.5+d/3)
				var hw float64
				var col uint32
				switch pass {
				case 0:
					hw, col = hw0+0.8, rim
				case 1:
					hw, col = hw0, body
					if d < L*0.35 {
						col = inner
					}
				default:
					hw, col = hw0*0.45, core
				}
				if pass == 2 && d > L*0.55 {
					break
				}
				for o := -hw; o <= hw; o += 0.5 {
					p.Px(x+c*d-sn*o, y+sn*d+c*o, col)
				}
			}
		}
	}
	r := 2.4 * sc * math.Sqrt(th)
	p.Circle(x, y, r+0.8, inner, 1)
	p.Circle(x, y, r, core, 1)
}

// sliver draws a thin arc flying out from (x, y): a sliver at radius r around angle a, arc radians long, dithered by k.
func sliver(p *pixelfx.Painter, x, y, a, r, arc float64, c uint32, k float64) {
	n := math.Ceil(r * arc * 2.2)
	for j := 0.0; j <= n; j++ {
		u := j/n - 0.5
		th, w := a+u*arc, 1.4*(1-math.Abs(u)*2)
		for o := -w * 0.5; o <= w*0.5+0.01; o += 0.5 {
			p.Dot(x+math.Cos(th)*(r+o), y+math.Sin(th)*(r+o), c, k)
		}
	}
}

// along walks the first length px of a polyline, calling fn(x0, y0, x1, y1, d) for each piece (d is the distance along it).
func along(pts [][2]float64, length float64, fn func(x0, y0, x1, y1, d float64)) {
	d := 0.0
	for i := 1; i < len(pts) && d < length; i++ {
		x0, y0, x1, y1 := pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1]
		l := pixelfx.Hypot(x1-x0, y1-y0)
		f := math.Min(1, (length-d)/l)
		fn(x0, y0, x0+(x1-x0)*f, y0+(y1-y0)*f, d)
		d += l
	}
}

// newSim returns a generator seeded with seed, particles over the flat floor that draw from it (sparks colors their sparks; pixelfx's hot ramp when nil) and no lights yet, as most melee effects start.
func newSim(seed float64, sparks []uint32) (*pixelfx.Rng, *pixelfx.Particles, *pixelfx.Lights) {
	R := pixelfx.NewRng(seed)
	return R, pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: sparks}), &pixelfx.Lights{}
}

// either returns a when c holds, else b: pixelfx's c ? a : b, as a call so that random draws in an argument list stay in pixelfx's order.
func either[T any](c bool, a, b T) T {
	if c {
		return a
	}
	return b
}

// backIf returns the flag that draws a particle behind the character when far holds.
func backIf(far bool) pixelfx.Flag { return either(far, pixelfx.Back, 0) }
