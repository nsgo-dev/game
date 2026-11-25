// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame tongue and fire puff recipes from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), and the bubble from its open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// The critters' kit. A creature is built in a small offscreen grid from shaded parts, given a 1 px outline (picked per side, so it can be darker in shadow and break into a highlight), then put on the painter, opaque or translucent. Pure loops also get loop-safe noise, springs, closed-form flight with a floor, and the toolkit's own particle looks for particles whose state is a function of time.

// pi and tau as float64 variables so expressions combining them with constants round step by step as JavaScript's do.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// shift returns the loop time for a critter: seeds other than 1 start it at a different point in its loop of D seconds, so a crowd of the same critter doesn't move in lockstep.
func shift(t, D, seed float64) float64 { return pixelfx.Wrap(t+(seed-1)*0.618*D, D) }

// curve is a smoothstepped curve through keyframes {x, value} that doesn't wrap: held flat before the first and after the last.
func curve(x float64, list [][2]float64) float64 {
	if x <= list[0][0] {
		return list[0][1]
	}
	for i := 1; i < len(list); i++ {
		x0, v0, x1, v1 := list[i-1][0], list[i-1][1], list[i][0], list[i][1]
		if x <= x1 {
			d := x1 - x0
			if d == 0 {
				d = 1
			}
			u := (x - x0) / d
			return v0 + (v1-v0)*u*u*(3-2*u)
		}
	}
	return list[len(list)-1][1]
}

// lnoise is smooth noise in -1..1 that repeats every D seconds: F bumps a loop (a whole number), off shifts it along.
func lnoise(t, D, F, off, seed float64) float64 {
	return pixelfx.TileNoise((t/D)*F+off, 0.5, F, 0, seed)
}

// spring is a damped spring's swing after a kick: 1 at age 0, ringing at hz, dying at rate damp.
func spring(age, hz, damp float64) float64 {
	if age < 0 {
		return 0
	}
	return math.Exp(-age*damp) * math.Cos(age*hz*tau)
}

// flight is where a tossed body is: its position and velocity, whether it has landed, when (at) and where (lx) it lands.
type flight struct {
	x, y, vx, vy float64
	landed       bool
	at, lx       float64
}

// toss is a ballistic body thrown from (x, y) at (vx, vy) px/s under gravity g, stopping on the floor y = floor.
func toss(x, y, vx, vy, age, g, floor float64) flight {
	tl := math.Inf(1)
	if g > 0 {
		tl = (-vy + math.Sqrt(math.Max(0, vy*vy+2*g*(floor-y)))) / g
	}
	if age < tl {
		return flight{x: x + vx*age, y: y + vy*age + 0.5*g*age*age, vx: vx, vy: vy + g*age, at: tl, lx: x + vx*tl}
	}
	return flight{x: x + vx*tl, y: floor, landed: true, at: tl, lx: x + vx*tl}
}

// hop is where a bouncing body is: its position and velocity, how many times it has bounced, and whether it has come to rest.
type hop struct {
	x, y, vx, vy float64
	hops         int
	rest         bool
}

// bounce is a ballistic body thrown from (x, y) at (vx, vy) under gravity g that bounces on the floor y = floor, keeping e of its speed each bounce (and 70% of its run), until it rests.
func bounce(x, y, vx, vy, age, g, floor, e float64) hop {
	hops := 0
	for ; hops < 6; hops++ {
		f := toss(x, y, vx, vy, age, g, floor)
		if !f.landed {
			return hop{x: f.x, y: f.y, vx: f.vx, vy: f.vy, hops: hops}
		}
		vl := vy + g*f.at
		age -= f.at
		x, y, vx, vy = f.lx, floor-0.01, vx*0.7, -vl*e
		if math.Abs(vy) < 25 {
			break
		}
	}
	return hop{x: x, y: floor - 0.25, hops: hops, rest: true}
}

// gridOut is what a grid shader sets for its cell besides the color: a its alpha (below 1 is drawn translucent) and t its tag.
type gridOut struct {
	a float64
	t uint8
}

// grid is a creature-sized canvas in local coordinates: cells [x0, x0 + w) x [y0, y0 + h), each a color, an alpha and a tag. Alphas are float32, as pixelfx keeps them in a Float32Array.
type grid struct {
	x0, y0 float64
	w, h   int
	c      []uint32
	a      []float32
	tag    []uint8
	d      []uint8
	o      gridOut
}

func newGrid(x0, y0 float64, w, h int) *grid {
	return &grid{x0: math.Floor(x0), y0: math.Floor(y0), w: w, h: h, c: make([]uint32, w*h), a: make([]float32, w*h), tag: make([]uint8, w*h), o: gridOut{a: 1}}
}

func (g *grid) idx(x, y float64) int {
	ix, iy := math.Floor(x)-g.x0, math.Floor(y)-g.y0
	if ix < 0 || iy < 0 || ix >= float64(g.w) || iy >= float64(g.h) {
		return -1
	}
	return int(iy)*g.w + int(ix)
}

// set paints a cell: color c (0 leaves it), alpha a, tag (any 0..255 label for later passes).
func (g *grid) set(x, y float64, c uint32, a float64, tag uint8) {
	if i := g.idx(x, y); i >= 0 && c != 0 {
		g.c[i], g.a[i], g.tag[i] = c, float32(a), tag
	}
}

// put paints a cell opaque with no tag.
func (g *grid) put(x, y float64, c uint32) { g.set(x, y, c, 1, 0) }

// shade calls fn(x, y, out) at each cell center in the box; it returns a color (or 0 to leave the cell) and may set out.a and out.t.
func (g *grid) shade(x0, y0, x1, y1 float64, fn func(x, y float64, o *gridOut) uint32) {
	o := &g.o
	for y := math.Floor(y0); y <= math.Floor(y1); y++ {
		for x := math.Floor(x0); x <= math.Floor(x1); x++ {
			o.a, o.t = 1, 0
			if c := fn(x+0.5, y+0.5, o); c != 0 {
				g.set(x, y, c, o.a, o.t)
			}
		}
	}
}

// ellipse fills an ellipse at alpha a, its color fn(nx, ny) of the normalized offset (-1..1).
func (g *grid) ellipse(cx, cy, rx, ry float64, fn func(nx, ny float64) uint32, a float64) {
	if rx <= 0 || ry <= 0 {
		return
	}
	g.shade(cx-rx, cy-ry, cx+rx, cy+ry, func(x, y float64, o *gridOut) uint32 {
		nx, ny := (x-cx)/rx, (y-cy)/ry
		if nx*nx+ny*ny > 1 {
			return 0
		}
		o.a = a
		return fn(nx, ny)
	})
}

// line draws a 1 px line in color c.
func (g *grid) line(x0, y0, x1, y1 float64, c uint32) {
	g.lineFn(x0, y0, x1, y1, func(float64) uint32 { return c })
}

// lineFn draws a 1 px line whose color is fn(u), u 0 at the start and 1 at the end.
func (g *grid) lineFn(x0, y0, x1, y1 float64, fn func(u float64) uint32) {
	n := math.Max(1, math.Ceil(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))))
	for i := 0.0; i <= n; i++ {
		g.put(x0+((x1-x0)*i)/n, y0+((y1-y0)*i)/n, fn(i/n))
	}
}

// tube draws a line of discs through pts: radius r(u), color c(u, v) with v 0 at the center and 1 at the edge.
func (g *grid) tube(pts [][2]float64, r func(u float64) float64, c func(u, v float64) uint32) {
	for k := 1; k < len(pts); k++ {
		ax, ay, bx, by := pts[k-1][0], pts[k-1][1], pts[k][0], pts[k][1]
		L := math.Max(1, pixelfx.Hypot(bx-ax, by-ay))
		for s := 0.0; s <= L; s += 0.5 {
			u := (float64(k-1) + s/L) / float64(len(pts)-1)
			rr := r(u)
			x, y := ax+((bx-ax)*s)/L, ay+((by-ay)*s)/L
			if rr < 0.75 {
				g.put(x, y, c(u, 0))
				continue
			}
			g.shade(x-rr, y-rr, x+rr, y+rr, func(px, py float64, o *gridOut) uint32 {
				d := pixelfx.Hypot(px-x, py-y) / rr
				if d > 1 {
					return 0
				}
				return c(u, d)
			})
		}
	}
}

// poly fills a polygon (even-odd) in color c at alpha a.
func (g *grid) poly(pts [][2]float64, c uint32, a float64) {
	g.polyFn(pts, func(float64, float64) uint32 { return c }, a)
}

// polyFn fills a polygon (even-odd) at alpha a, its color fn(x, y).
func (g *grid) polyFn(pts [][2]float64, fn func(x, y float64) uint32, a float64) {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		x0, x1 = math.Min(x0, p[0]), math.Max(x1, p[0])
		y0, y1 = math.Min(y0, p[1]), math.Max(y1, p[1])
	}
	g.shade(x0, y0, x1, y1, func(x, y float64, o *gridOut) uint32 {
		o.a = a
		if inPoly(pts, x, y) {
			return fn(x, y)
		}
		return 0
	})
}

// inPoly reports whether (x, y) is inside the polygon pts (even-odd).
func inPoly(pts [][2]float64, x, y float64) bool {
	inside := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		xi, yi, xj, yj := pts[i][0], pts[i][1], pts[j][0], pts[j][1]
		if (yi > y) != (yj > y) && x < ((xj-xi)*(y-yi))/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

// depths works out how deep each filled cell sits: 1 on the edge (next to an empty cell, 4-neighbour), 2 one in, ... up to limit; depth reads it.
func (g *grid) depths(limit uint8) {
	w, h, c := g.w, g.h, g.c
	d := make([]uint8, w*h)
	var q []int
	for i := 0; i < w*h; i++ {
		if c[i] == 0 {
			continue
		}
		x, y := i%w, i/w
		if x == 0 || y == 0 || x == w-1 || y == h-1 || c[i-1] == 0 || c[i+1] == 0 || c[i-w] == 0 || c[i+w] == 0 {
			d[i] = 1
			q = append(q, i)
		}
	}
	for k := 0; k < len(q); k++ {
		i := q[k]
		v := d[i]
		if v >= limit {
			continue
		}
		for _, j := range [4]int{i - 1, i + 1, i - w, i + w} {
			if j >= 0 && j < w*h && c[j] != 0 && d[j] == 0 {
				d[j] = v + 1
				q = append(q, j)
			}
		}
	}
	for i := 0; i < w*h; i++ {
		if c[i] != 0 && d[i] == 0 {
			d[i] = limit
		}
	}
	g.d = d
}

func (g *grid) depth(x, y float64) uint8 {
	i := g.idx(x, y)
	if i < 0 || g.d == nil {
		return 0
	}
	return g.d[i]
}

// outline wraps everything drawn so far in a 1 px outline (4-neighbour) of color c at alpha a.
func (g *grid) outline(c uint32, a float64) {
	g.outlineFn(func(_, _, _, _ float64) uint32 { return c }, a)
}

// outlineFn wraps everything drawn so far in a 1 px outline (4-neighbour) at alpha a, its color fn(x, y, nx, ny) with (nx, ny) the outward direction there (summed from the filled neighbours), so it can be picked per side; 0 leaves a gap.
func (g *grid) outlineFn(fn func(x, y, nx, ny float64) uint32, a float64) {
	src := append([]uint32(nil), g.c...)
	w, h := g.w, g.h
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if src[i] != 0 {
				continue
			}
			l := x > 0 && src[i-1] != 0
			r := x < w-1 && src[i+1] != 0
			u := y > 0 && src[i-w] != 0
			d := y < h-1 && src[i+w] != 0
			if !(l || r || u || d) {
				continue
			}
			if col := fn(g.x0+float64(x), g.y0+float64(y), b2f(l)-b2f(r), b2f(u)-b2f(d)); col != 0 {
				g.c[i], g.a[i], g.tag[i] = col, float32(a), 255
			}
		}
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// recolor reworks every filled cell: fn(x, y, color, alpha, tag) returns its new color (0 erases it).
func (g *grid) recolor(fn func(x, y float64, c uint32, a float64, tag uint8) uint32) {
	for i := 0; i < g.w*g.h; i++ {
		if g.c[i] == 0 {
			continue
		}
		g.c[i] = fn(g.x0+float64(i%g.w), g.y0+float64(i/g.w), g.c[i], float64(g.a[i]), g.tag[i])
	}
}

// fillAlpha sets every cell's alpha to a.
func (g *grid) fillAlpha(a float64) {
	for i := range g.a {
		g.a[i] = float32(a)
	}
}

// draw puts the grid on the painter, moved by (dx, dy): opaque cells dithered by fade, translucent ones blended.
func (g *grid) draw(p *pixelfx.Painter, fade, dx, dy float64) {
	X, Y := g.x0+pixelfx.Round(dx), g.y0+pixelfx.Round(dy)
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			i := y*g.w + x
			c := g.c[i]
			if c == 0 {
				continue
			}
			a := float64(g.a[i])
			if a >= 0.999 {
				p.Dot(X+float64(x), Y+float64(y), c, fade)
			} else {
				p.Blend(X+float64(x), Y+float64(y), c, a*fade)
			}
		}
	}
}

func (g *grid) clear() *grid {
	clear(g.c)
	clear(g.tag)
	g.d = nil
	return g
}

// swarm draws the toolkit's particle looks for particles whose state is a function of time (pure loops): begin(t), then add each one alive now, then draw.
type swarm struct{ ps *pixelfx.Particles }

func newSwarm() *swarm {
	return &swarm{ps: pixelfx.NewParticles(pixelfx.SimOpts{Rng: pixelfx.NewRng(1)})}
}

func (s *swarm) begin(t float64) *swarm {
	s.ps.List = s.ps.List[:0]
	s.ps.T = t
	return s
}

// add adds a particle of kind k at age age of its life, if it is alive.
func (s *swarm) add(k pixelfx.Kind, x, y, vx, vy, age, life float64, col uint32, size float64, flag pixelfx.Flag, seed float64) {
	if age < 0 || age >= life {
		return
	}
	s.ps.List = append(s.ps.List, pixelfx.Particle{K: k, X: x, Y: y, VX: vx, VY: vy, Life: life - age, Max: life, Col: col, Size: size, Seed: seed, Flag: flag})
}

func (s *swarm) draw(p *pixelfx.Painter) { s.ps.Draw(p, pixelfx.LayerBoth) }

// flameLayers are Hugo's flame layers: width, height and ramp step, from the widest dark outer flame to the white-hot heart.
var flameLayers = [4][3]float64{{1, 1, 6}, {0.76, 0.86, 4}, {0.54, 0.68, 2}, {0.3, 0.42, 0}}

// tongue draws a tongue of flame standing on (x, y), h tall, w half-wide at its root, its tip swaying sx px, as four nested layers down a 9-step fire ramp given bright-first (heat[0] white). n seeds its flicker; t, D keep it looping. only draws just that layer (0 outer .. 3 heart; -1 all), so a bunch of tongues can be laid down layer by layer and merge into one flame with one dark rim.
func tongue(p *pixelfx.Painter, x, y, h, w, sx, t, D, n float64, heat []uint32, only int) {
	if h < 1 {
		return
	}
	F1, F2 := math.Max(1, pixelfx.Round(9*D)), math.Max(1, pixelfx.Round(4*D))
	for li, l := range flameLayers {
		if only >= 0 && li != only {
			continue
		}
		lw, lh := l[0], l[1]
		H, col := h*lh, heat[int(l[2])]
		for r := 0.0; r <= H; r++ {
			u := r / math.Max(1, H)
			half := w*lw*math.Pow(1-u, 0.85)*math.Min(1, (r+1.5)/3.5) + lnoise(t, D, F1, r*0.45, n)*0.5
			cx := x + sx*u*u*lh + lnoise(t, D, F2, r*0.2, n+7)*u*1.1
			if half < 0.3 {
				continue
			}
			for px := pixelfx.Round(cx - half); px <= pixelfx.Round(cx+half); px++ {
				p.Px(px, pixelfx.Round(y-r), col)
			}
		}
	}
}

// firePuff draws a puff of flame with k of its life left (1 new .. 0 gone), size px, cooling down heat (bright-first, 9 steps): a darker rim, a lit core, a tongue on top, light.
func firePuff(p *pixelfx.Painter, x, y, k, size, seed float64, heat []uint32) {
	age, n := 1-k, len(heat)
	hot := math.Min(0.999, math.Pow(age, 0.62)+math.Mod(seed, 1)*0.1)
	idx := min(n-2, int(math.Floor(hot*float64(n-1))))
	r, c := size*(0.55+0.45*math.Min(1, age*7))*(1-age*0.55), heat[idx]
	if idx >= n-2 {
		p.Disc(x, y, r, c, math.Min(1, k*3)*0.85)
		return
	}
	p.Circle(x, y, r, heat[min(n-1, idx+2)], 1)
	p.Circle(x-r*0.16, y-r*0.22, r*0.78, c, 1)
	if r > 1.6 {
		p.Circle(x, y-r*0.9, r*0.42, c, 1)
	}
	if idx <= 3 && r > 1.8 {
		p.Circle(x-r*0.28, y-r*0.34, r*0.36, heat[max(0, idx-1)], 1)
	}
	if idx < 6 {
		p.Glow(x, y, r*2.4+2, c, 0.18*k)
	}
}

// bubble draws a bubble of radius r at alpha a, squashed by wob: a rim lit top-left and shaded bottom-right, a white glint, light. cols are highlight, lit, rim and shade.
func bubble(p *pixelfx.Painter, cx, cy, r float64, cols [4]uint32, a, wob float64) {
	HI, LIT, RIM, SHADE := cols[0], cols[1], cols[2], cols[3]
	if r < 0.9 {
		c := LIT
		if r < 0.7 {
			c = RIM
		}
		p.Dot(cx, cy, c, a)
		return
	}
	if r < 1.5 {
		px, py := math.Floor(cx), math.Floor(cy)
		p.Dot(px, py, HI, a)
		p.Dot(px+1, py, LIT, a)
		p.Dot(px, py+1, RIM, a)
		p.Dot(px+1, py+1, SHADE, a)
		p.Glow(cx+0.5, cy+0.5, 3, LIT, 0.12*a)
		return
	}
	rx, ry := r*(1+wob), r*(1-wob)
	for y := math.Floor(cy - ry - 1); y <= math.Ceil(cy+ry+1); y++ {
		for x := math.Floor(cx - rx - 1); x <= math.Ceil(cx+rx+1); x++ {
			dx, dy := (x+0.5-cx)/rx, (y+0.5-cy)/ry
			d := math.Sqrt(dx*dx+dy*dy) * r
			if d > r+0.5 {
				continue
			}
			l := (-dx - dy) / math.Sqrt2 / math.Max(0.01, d/r)
			if d > r-0.6 {
				c := RIM
				if l > 0.45 {
					c = LIT
				} else if l < -0.55 {
					c = SHADE
				}
				p.Dot(x, y, c, a)
			}
		}
	}
	hx, hy := pixelfx.Round(cx-rx*0.45), pixelfx.Round(cy-ry*0.45)
	p.Dot(hx, hy, HI, a)
	if r >= 2.5 {
		p.Dot(hx+1, hy, LIT, a)
		p.Dot(hx, hy+1, LIT, a)
	}
	if r >= 2 {
		p.Dot(pixelfx.Round(cx+rx*0.5), pixelfx.Round(cy+ry*0.5), LIT, a)
	}
	p.Glow(cx, cy, r*2.4+2, RIM, 0.14*a)
}

// shadow draws a soft shadow: an ellipse blended over what's there, a alpha at the center fading to nothing at the rim.
func shadow(p *pixelfx.Painter, cx, cy, rx, ry float64, c uint32, a float64) {
	if rx <= 0 || ry <= 0 || a <= 0 {
		return
	}
	for y := math.Floor(cy - ry); y <= math.Ceil(cy+ry); y++ {
		for x := math.Floor(cx - rx); x <= math.Ceil(cx+rx); x++ {
			d := math.Pow((x+0.5-cx)/rx, 2) + math.Pow((y+0.5-cy)/ry, 2)
			if d < 1 {
				p.Blend(x, y, c, a*math.Pow(1-d, 0.7))
			}
		}
	}
}

// reversed returns a reversed copy of r.
func reversed(r []uint32) []uint32 {
	out := make([]uint32, len(r))
	for i, c := range r {
		out[len(r)-1-i] = c
	}
	return out
}
