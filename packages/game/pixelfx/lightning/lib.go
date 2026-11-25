// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The bolt shape and the way a bolt is inked (a deep halo, a pale body, a white core, light along it) are adapted from Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"
	"sync"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Shared by the lightning effects: bolts that fork and thin as they split, inked at the 2x character scale, thin crackles, bowed arcs, heat scorch, and trajectories for pure loops.

// pi, tau and gravity are variables so expressions combining them with constants round step by step as JavaScript's do, rather than folding exactly at compile time.
var (
	pi      = math.Pi
	tau     = pixelfx.Tau
	gravity = float64(pixelfx.Gravity)
)

// volt is the category's default palette, dark to bright: deep violet shadow, electric blue, icy white.
var volt = []string{"#120a33", "#2a1b78", "#4630c0", "#5a62f0", "#58a6ff", "#9ed8ff", "#e4f6ff", "#ffffff"}

// plasma is the default spark ramp, head to tail: cyan that reads apart from the violet bolt.
var plasma = []string{"#ffffff", "#c8f8ff", "#7ff3ff", "#1ab8e6"}

var (
	soot  = pixelfx.Hex("#110c16")
	soot2 = pixelfx.Hex("#1c1522")
)

// voltOptions returns the palette and spark options most lightning effects open with.
func voltOptions() []pixelfx.Option {
	return []pixelfx.Option{
		{Name: "palette", Default: pixelfx.Palette(volt...)},
		{Name: "sparks", Default: pixelfx.Palette(plasma...)},
	}
}

// path is one stroke of a bolt: its points and its weight (1 for the trunk, less for each level of fork).
type path struct {
	pts [][2]float64
	w   float64
}

// bolt is a bolt's strokes, the trunk first.
type bolt []path

// zigzag returns a bolt from (x0, y0) to (x1, y1): midpoint displacement down to about 3 px segments (jag: how far the first midpoint may swing, as a share of the length), with forks that split off along the way, each thinner and forking again up to two levels.
func zigzag(R *pixelfx.Rng, x0, y0, x1, y1, jag, forks float64) bolt {
	return zigzagAt(R, x0, y0, x1, y1, jag, forks, 1, 0)
}

func zigzagAt(R *pixelfx.Rng, x0, y0, x1, y1, jag, forks, w float64, depth int) bolt {
	L := pixelfx.Hypot(x1-x0, y1-y0)
	pts := [][2]float64{{x0, y0}, {x1, y1}}
	off := L * jag
	for float64(len(pts)) < 2+L/3 {
		nxt := make([][2]float64, 1, 2*len(pts)-1)
		nxt[0] = pts[0]
		for i := 0; i < len(pts)-1; i++ {
			ax, ay, bx, by := pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]
			l := pixelfx.Hypot(bx-ax, by-ay)
			if l == 0 || l != l {
				l = 1
			}
			o := R.Range(-off, off)
			nxt = append(nxt, [2]float64{(ax+bx)/2 + (-(by-ay)/l)*o, (ay+by)/2 + ((bx-ax)/l)*o}, [2]float64{bx, by})
		}
		pts = nxt
		off *= 0.52
	}
	out := bolt{{pts: pts, w: w}}
	if depth < 2 {
		n := float64(len(pts))
		for i := 2; i < len(pts)-3; i++ {
			if R.Chance(forks / float64(1+depth) / math.Max(4, n/6)) {
				a, b := pts[i], pts[min(len(pts)-1, i+3)]
				ang := math.Atan2(b[1]-a[1], b[0]-a[0]) + R.Sign()*R.Range(0.35, 0.9)
				l := (L - (float64(i)/n)*L) * R.Range(0.25, 0.55)
				out = append(out, zigzagAt(R, a[0], a[1], a[0]+math.Cos(ang)*l, a[1]+math.Sin(ang)*l, jag*1.1, forks, w*0.55, depth+1)...)
			}
		}
	}
	return out
}

// bowed returns a bolt bowed off the straight line by bow px at its middle (negative: up for a left-to-right arc).
func bowed(R *pixelfx.Rng, x0, y0, x1, y1, bow, jag, forks float64) bolt {
	L := pixelfx.Hypot(x1-x0, y1-y0)
	if L == 0 || L != L {
		L = 1
	}
	nx, ny := -(y1-y0)/L, (x1-x0)/L
	mx := (x0+x1)/2 + nx*bow + R.Range(-2, 2)
	my := (y0+y1)/2 + ny*bow
	a := zigzag(R, x0, y0, mx, my, jag, forks)
	b := zigzag(R, mx, my, x1, y1, jag, forks)
	trunk := make([][2]float64, 0, len(a[0].pts)+len(b[0].pts)-1)
	trunk = append(append(trunk, a[0].pts...), b[0].pts[1:]...)
	out := make(bolt, 0, len(a)+len(b)-1)
	out = append(out, path{pts: trunk, w: 1})
	out = append(out, a[1:]...)
	return append(out, b[1:]...)
}

// bend returns the bolt with every point moved by fn.
func bend(b bolt, fn func(x, y float64) (float64, float64)) bolt {
	out := make(bolt, len(b))
	for i, q := range b {
		pts := make([][2]float64, len(q.pts))
		for j, pt := range q.pts {
			pts[j][0], pts[j][1] = fn(pt[0], pt[1])
		}
		out[i] = path{pts: pts, w: q.w}
	}
	return out
}

// ---- inking. Pixels are gathered in a grid at a level (higher wins), then drawn once each, so a fork's halo never covers the trunk's body and overlapping passes cost nothing extra.

const (
	inkN    = 1024
	inkHalf = 512
)

// ink is the grid a stroke gathers its pixels in. pixelfx keeps one grid for the whole module; here each stroke takes one from inkPool, so effects can be drawn from several goroutines at once.
type ink struct {
	grid []uint8
	hit  []int32
}

var inkPool = sync.Pool{New: func() any { return &ink{grid: make([]uint8, inkN*inkN)} }}

func (g *ink) mark(x, y float64, lvl uint8) {
	X, Y := math.Floor(x)+inkHalf, math.Floor(y)+inkHalf
	if !(X >= 0 && Y >= 0 && X < inkN && Y < inkN) {
		return
	}
	i := int32(Y)*inkN + int32(X)
	v := g.grid[i]
	if v == 0 {
		g.hit = append(g.hit, i)
	}
	if lvl > v {
		g.grid[i] = lvl
	}
}

func (g *ink) stamp(x, y, r float64, lvl uint8) {
	if r <= 0.5 {
		g.mark(pixelfx.Round(x), pixelfx.Round(y), lvl)
		return
	}
	r2 := r * r
	for py := math.Floor(y - r); py <= math.Ceil(y+r); py++ {
		dy := py + 0.5 - y
		for px := math.Floor(x - r); px <= math.Ceil(x+r); px++ {
			dx := px + 0.5 - x
			if dx*dx+dy*dy <= r2 {
				g.mark(px, py, lvl)
			}
		}
	}
}

// flush draws what was gathered, level n in cols[n-1] at alpha alphas[n-1] (dithered below 1), and clears the grid.
func (g *ink) flush(p *pixelfx.Painter, cols [4]uint32, alphas [4]float64) {
	for _, i := range g.hit {
		v := g.grid[i]
		g.grid[i] = 0
		x, y := float64(i%inkN-inkHalf), float64(i/inkN-inkHalf)
		a := alphas[v-1]
		if a >= 1 {
			p.Px(x, y, cols[v-1])
		} else if a > 0 {
			p.Dot(x, y, cols[v-1], a)
		}
	}
	g.hit = g.hit[:0]
}

// walk calls fn every half pixel along pts.
func walk(pts [][2]float64, fn func(x, y float64)) {
	for i := 0; i < len(pts)-1; i++ {
		ax, ay, bx, by := pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]
		l := math.Max(1, pixelfx.Hypot(bx-ax, by-ay))
		for d := 0.0; d <= l; d += 0.5 {
			fn(ax+((bx-ax)*d)/l, ay+((by-ay)*d)/l)
		}
	}
}

// strokeOpts are a stroke's options: Hero inks it for a white frame (the body goes white), Light is its glow strength (0.5 when unsaid), A its alpha (1), UpTo draws the trunk only to that share of its length, Core is the radius of a thicker white core (0 for a 1 px one), and MinY puts no glow above that y.
type strokeOpts struct {
	hero  bool
	light pixelfx.Opt
	a     pixelfx.Opt
	upTo  pixelfx.Opt
	core  float64
	minY  pixelfx.Opt
}

// stroke inks a bolt at full power: a deep halo, a pale body, a white core on the trunk, light every few points. k is the stroke's brightness (forks drop out below 0.5, the trunk thins below 0.6), s scales the thickness.
func stroke(p *pixelfx.Painter, b bolt, k float64, C []uint32, s float64, o strokeOpts) {
	if k <= 0.02 {
		return
	}
	g := inkPool.Get().(*ink)
	defer inkPool.Put(g)
	lk := o.light.Or(0.5)
	for _, q := range b {
		trunk := q.w >= 1
		if !trunk && k < 0.5 {
			continue
		}
		var rh, rb float64
		body := uint8(2)
		switch {
		case trunk && k > 0.6:
			rh, rb, body = 2.1, 1.3, 3
		case trunk:
			rh, rb, body = 1.6, 0.8, 3
		default:
			rh, rb = 1.1, 0.5
		}
		rh, rb = rh*s, rb*s
		pts := q.pts
		if o.upTo.Set && trunk {
			pts = pts[:min(len(pts), int(math.Max(2, math.Ceil(float64(len(pts))*o.upTo.V))))]
		}
		walk(pts, func(x, y float64) {
			g.stamp(x, y, rh, 1)
			g.stamp(x, y, rb, body)
			if trunk && k > 0.3 {
				if o.core != 0 {
					g.stamp(x, y, o.core, 4)
				} else {
					g.mark(x, y, 4)
				}
			}
		})
		r := 7.0
		if trunk {
			r = 14
		}
		for i := 0; i < len(pts)-1; i += 3 {
			if !o.minY.Set || pts[i][1] > o.minY.V {
				p.Glow(pts[i][0], pts[i][1], r*s, C[4], lk*k*q.w)
			}
		}
	}
	a := o.a.Or(1)
	cols := [4]uint32{C[2], C[4], C[5], C[7]}
	if o.hero {
		cols = [4]uint32{C[3], C[6], C[7], C[7]}
	}
	g.flush(p, cols, [4]float64{a, a, a, a})
}

var cross = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// crackle inks a thin crackle: a 1 px core (white on the trunk while bright), a 2 px pale body, a dithered halo while k is high, light along it (glowK, 0.5 in pixelfx's default), at alpha a. For short arcs that live a frame or two.
func crackle(p *pixelfx.Painter, b bolt, k float64, C []uint32, glowK, a float64) {
	if k <= 0.02 {
		return
	}
	g := inkPool.Get().(*ink)
	defer inkPool.Put(g)
	for _, q := range b {
		thick := q.w >= 1
		walk(q.pts, func(x, y float64) {
			if k > 0.45 && thick {
				for _, d := range cross {
					g.mark(x+d[0]*1.5, y+d[1]*1.5, 1)
				}
			}
			if k > 0.25 && (thick || q.w > 0.4) {
				g.mark(x+1, y, 2)
				g.mark(x, y+1, 2)
			}
			if thick && k > 0.3 {
				g.mark(x, y, 4)
			} else {
				g.mark(x, y, 3)
			}
		})
		r := 5.0
		if thick {
			r = 9
		}
		for i := 0; i < len(q.pts)-1; i += 3 {
			p.Glow(q.pts[i][0], q.pts[i][1], r*(0.5+k*0.5), C[4], glowK*k*q.w)
		}
	}
	g.flush(p, [4]uint32{C[3], C[5], C[5], C[7]}, [4]float64{math.Min(1, (k-0.45)*1.6) * a, a, a, a})
}

// trace draws a bolt's trunk as a 1 px line in one color from point from to point to (leaders, afterimages, cooling channels).
func trace(p *pixelfx.Painter, pts [][2]float64, c uint32, a float64, from, to int) {
	for i := max(0, from); i < min(to, len(pts)-1); i++ {
		p.Line(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1], c, a)
	}
}

// traceAll draws the whole trunk, trace's default span.
func traceAll(p *pixelfx.Painter, pts [][2]float64, c uint32, a float64) {
	trace(p, pts, c, a, 0, len(pts)-1)
}

// heatAt returns the fire ramp's color for heat T (1 white-hot .. 0 cold).
func heatAt(T float64) uint32 {
	F := pixelfx.FireRamp
	n := float64(len(F) - 1)
	return F[int(math.Min(n, math.Floor((1-math.Min(1, T))*n)))]
}

// scorch draws heat left on the floor: a flat ellipse, white-hot in the middle cooling through the fire ramp to soot. T0 is the heat (1 fresh, 0 cold), fade takes the soot away.
func scorch(p *pixelfx.Painter, cx, cy, rx, T0, fade float64) {
	if fade <= 0 {
		return
	}
	ry := math.Max(2.5, rx*0.12)
	for y := math.Floor(cy); y <= cy+ry; y++ {
		for x := math.Floor(cx - rx); x <= math.Ceil(cx+rx); x++ {
			dx, dy := (x+0.5-cx)/rx, (y+0.5-cy)/ry
			d := math.Sqrt(dx*dx + dy*dy)
			if d > 1 {
				continue
			}
			n := pixelfx.Rand(x, y, 41)
			T := T0*(1.25-d) - n*0.35
			if T > 0.06 {
				c := heatAt(T)
				p.Dot(x, y, c, fade)
				if int(x)&3 == 0 {
					p.Glow(x+0.5, y, 6, c, 0.18*T*fade)
				}
				continue
			}
			c, a := soot2, 1-d
			if d < 0.5 {
				c = soot
			}
			if d < 0.95-n*0.3 {
				a = 1
			}
			p.Dot(x, y, c, a*fade)
		}
	}
}

// heat returns a heat-ramp color for T (1 white-hot .. 0 cold), or 0 when it's cold.
func heat(T float64) uint32 {
	if T > 0.06 {
		return heatAt(T)
	}
	return 0
}

// flight returns where a spark launched at (x, y, vx, vy) is after age seconds, stepped like the simulation's Spark (gravity, drag, bouncing on a floor at floor), so pure loops get the same physics.
func flight(x, y, vx, vy, age, floor float64) (float64, float64, float64, float64) {
	const g, drag, bounce = 0.45, 2.2, 0.45
	dt := 1.0 / 120
	for s := 0.0; s < age; s += dt {
		h := math.Min(dt, age-s)
		vy += gravity * g * h
		vx -= vx * drag * h
		vy -= vy * drag * h
		ny := y + vy*h
		x += vx * h
		if vy > 0 && y <= floor && ny >= floor {
			vy = -vy * bounce
			vx *= 0.7
			y = floor - 0.25
		} else {
			y = ny
		}
	}
	return x, y, vx, vy
}

// spark draws a spark the way the simulation draws one (a streak stretched by speed, a hot head, light), for pure loops; S runs head to tail.
func spark(p *pixelfx.Painter, x, y, vx, vy, k float64, S []uint32) {
	sp := pixelfx.Hypot(vx, vy)
	if sp == 0 || sp != sp {
		sp = 1
	}
	ux, uy := vx/sp, vy/sp
	head := S[2]
	if k > 0.6 {
		head = S[0]
	} else if k > 0.3 {
		head = S[1]
	}
	l := math.Min(9, 1+sp*0.022)
	tail := S[len(S)-1]
	if k > 0.5 {
		tail = head
	}
	p.Line(x-ux*l, y-uy*l, x-ux*l*0.4, y-uy*l*0.4, tail, 1)
	p.Line(x-ux*l*0.45, y-uy*l*0.45, x, y, head, 1)
	p.Glow(x, y, 3, head, 0.45*k)
}

// puff draws a soft smoke puff at age u (0..1) of its life, like the simulation's Smoke, for pure loops.
func puff(p *pixelfx.Painter, x, y, size, u float64, c uint32) {
	k := 1 - u
	r := size * (0.65 + math.Min(1, u*2.5)*0.45 + u*0.4)
	a := math.Min(1, u*12) * math.Min(1, k*2.4) * 0.58
	if a <= 0.01 {
		return
	}
	S := pixelfx.SmokeRamp
	col := pixelfx.Mix(c, S[int(math.Min(float64(len(S)-1), math.Floor(u*float64(len(S)))))], u)
	p.Disc(x, y, r, col, a*0.45)
	p.Disc(x+r*0.05, y+r*0.06, r*0.74, col, a*0.62)
	if u < 0.5 {
		p.Disc(x-r*0.22, y-r*0.3, r*0.5, pixelfx.Mix(col, pixelfx.White, 0.12), a*0.4*(1-u*2))
	}
}

// frameOf returns the frame of a loop at t, as pixelfx's Math.floor(t * fps + 1e-6) % n.
func frameOf(t, fps float64, n int) int {
	return int(math.Floor(t*fps+1e-6)) % n
}

// actorEdge returns the pixels of the actor's silhouette that touch its outside, in local space, each offset by off (pixelfx offsets them by 0.5 in one effect and not in another).
func actorEdge(A *pixelfx.Actor, off float64) [][2]float64 {
	m := A.Mask
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < m.W && y < m.H && m.Data[y*m.W+x] != 0 }
	var e [][2]float64
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			if on(x, y) && (!on(x-1, y) || !on(x+1, y) || !on(x, y-1) || !on(x, y+1)) {
				e = append(e, [2]float64{A.X + float64(x) + off, A.Y + float64(y) + off})
			}
		}
	}
	return e
}
