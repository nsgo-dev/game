// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the gas (puffs that drift off their push, rise, swell and thin, a lit cap) from Sprite Fusion's open-source poison (https://www.spritefusion.com/pixel-art-vfx/poison), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Shared by the nature effects: faceted stones that tumble and break, the stone spike they break from, leaves, droplets and gas.

// pi, tau and gravity are variables so expressions combining them with constants round step by step as JavaScript's do, rather than folding exactly at compile time.
var (
	pi      = math.Pi
	tau     = pixelfx.Tau
	gravity = float64(pixelfx.Gravity)
)

// stoneShape returns corners of an irregular stone, unit size, in angular order. When n <= 0, it picks R.Int(5, 7) corners.
func stoneShape(R *pixelfx.Rng, n int) [][2]float64 {
	if n <= 0 {
		n = int(R.Int(5, 7))
	}
	pts := make([][2]float64, 0, n)
	a0 := R.Range(0, tau)
	squash := R.Range(0.7, 1)
	for i := 0; i < n; i++ {
		a := a0 + ((float64(i)+R.Range(-0.28, 0.28))*tau)/float64(n)
		r := R.Range(0.72, 1.12)
		pts = append(pts, [2]float64{math.Cos(a) * r, math.Sin(a) * r * squash})
	}
	return pts
}

const (
	rockLX = -0.6
	rockLY = -0.8
)

func inPoly(V [][2]float64, x, y float64) bool {
	inside := false
	for i, j := 0, len(V)-1; i < len(V); j, i = i, i+1 {
		xi, yi := V[i][0], V[i][1]
		xj, yj := V[j][0], V[j][1]
		if (yi > y) != (yj > y) && x < ((xj-xi)*(y-yi))/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

// drawRock draws a faceted stone at (x, y): corners pts (from stoneShape) scaled by s and turned by a. Each wedge between neighbouring corners is a facet, lit by how squarely it faces the top left; edges facing the light catch a bright rim, a dark rim goes round the rest. S: stone ramp, dark to bright (7+ steps). alpha dithers it out.
func drawRock(p *pixelfx.Painter, x, y, s, a float64, pts [][2]float64, S []uint32, alpha float64, white bool) {
	n := len(S)
	top := n - 2
	ca, sa := math.Cos(a), math.Sin(a)
	if s < 1.4 {
		p.Dot(x, y, S[top-1], alpha)
		p.Dot(x+1, y, S[top-3], alpha)
		return
	}
	V := make([][2]float64, len(pts))
	for i, q := range pts {
		u, v := q[0], q[1]
		V[i] = [2]float64{(u*ca - v*sa) * s, (u*sa + v*ca) * s}
	}
	ang := make([]float64, len(V))
	for i, q := range V {
		ang[i] = math.Atan2(q[1], q[0])
	}
	// each facet's light: its edge's outward normal against the light
	lit := make([]float64, len(V))
	for i, q := range V {
		ax, ay := q[0], q[1]
		nxt := V[(i+1)%len(V)]
		bx, by := nxt[0], nxt[1]
		ex, ey := bx-ax, by-ay
		l := pixelfx.Hypot(ex, ey)
		if l == 0 {
			l = 1
		}
		nx, ny := ey/l, -ex/l
		if nx*(ax+bx)+ny*(ay+by) < 0 {
			nx, ny = -nx, -ny
		}
		lit[i] = nx*rockLX + ny*rockLY
	}
	var mask [64 * 64]uint8
	var face [64 * 64]int8
	X0 := math.Floor(x) - math.Ceil(s*1.25) - 1
	Y0 := math.Floor(y) - math.Ceil(s*1.25) - 1
	N := int(math.Min(62, 2*math.Ceil(s*1.25)+3))
	for j := 0; j < N; j++ {
		for i := 0; i < N; i++ {
			dx := X0 + float64(i) + 0.5 - x
			dy := Y0 + float64(j) + 0.5 - y
			k := j*64 + i
			if inPoly(V, dx, dy) {
				mask[k] = 1
			} else {
				mask[k] = 0
				continue
			}
			g := math.Atan2(dy, dx)
			f := 0
			for q := 0; q < len(ang); q++ {
				a0, a1 := ang[q], ang[(q+1)%len(ang)]
				d := math.Mod(g-a0, tau)
				span := math.Mod(a1-a0, tau)
				if d < 0 {
					d += tau
				}
				if span < 0 {
					span += tau
				}
				if d <= span {
					f = q
					break
				}
			}
			face[k] = int8(f)
		}
	}
	at := func(i, j int) bool {
		return i >= 0 && j >= 0 && i < N && j < N && mask[j*64+i] == 1
	}
	for j := -1; j <= N; j++ {
		for i := -1; i <= N; i++ {
			X := X0 + float64(i)
			Y := Y0 + float64(j)
			if !at(i, j) {
				if at(i-1, j) || at(i+1, j) || at(i, j-1) || at(i, j+1) {
					p.Dot(X, Y, S[0], alpha)
				}
				continue
			}
			if white {
				p.Dot(X, Y, pixelfx.White, alpha)
				continue
			}
			L := lit[face[j*64+i]]
			edge := !at(i-1, j) || !at(i+1, j) || !at(i, j-1) || !at(i, j+1)
			var c uint32
			switch {
			case L > 0.55:
				c = S[top]
			case L > 0.15:
				c = S[top-1]
			case L > -0.3:
				c = S[top-2]
			default:
				c = S[top-3]
			}
			if edge {
				if L > 0.3 {
					c = S[top+1]
				} else if L < -0.2 {
					c = S[max(1, top-4)]
				}
			}
			p.Dot(X, Y, c, alpha)
		}
	}
}

type spikeNotch struct {
	u, side, d float64
}

type spikeLookData struct {
	notches []spikeNotch
	strata  []float64
	slope   float64
	seed    float64
}

type spikeOpts struct {
	a        float64
	white    bool
	glint    float64
	clip     float64
	half     int
	floorY   float64
	cracks   [][][2]float64
	crack    float64
	heat     float64
	glowRamp []uint32
}

// drawSpike draws a faceted stone spike standing on (x, base): h tall, w wide at the base, its tip lean px over.
func drawSpike(p *pixelfx.Painter, x, base, h, w, lean float64, sp spikeLookData, S []uint32, opts spikeOpts) {
	if h < 1.5 {
		return
	}
	a := opts.a
	white := opts.white
	glint := opts.glint
	clip := opts.clip
	floorY := opts.floorY
	hw := func(u, side float64) float64 {
		v := (w/2)*math.Pow(math.Max(0, 1-u), 0.85) + 0.3
		for _, n := range sp.notches {
			if n.side == side && u > n.u && u < n.u+0.07 {
				v -= n.d * (1 - u*0.5)
			}
		}
		return math.Max(0, v)
	}
	half := opts.half
	Y0 := math.Floor(base-h) - 2
	const maxRows = 400
	NR := int(math.Min(maxRows, math.Ceil(base)-Y0+2))
	var rl, rr, ru [maxRows]float32
	// each row's edges, once
	for k := 0; k < NR; k++ {
		u := (base - (Y0 + float64(k) + 0.5)) / h
		ru[k] = float32(u)
		if u < 0 || u > 1 || Y0+float64(k)+0.5 > floorY {
			rl[k] = float32(math.Inf(1))
			rr[k] = float32(math.Inf(-1))
		} else {
			rl[k] = float32(x + lean*u - hw(u, -1))
			rr[k] = float32(x + lean*u + hw(u, 1))
		}
	}
	inside := func(px, py float64) bool {
		k := int(py - Y0)
		fx := px + 0.5
		if k < 0 || k >= NR || fx < float64(rl[k]) || fx > float64(rr[k]) {
			return false
		}
		if clip < 1 && float64(ru[k]) > clip+(pixelfx.Rand(px, 77)-0.5)*0.07 {
			return false
		}
		if half == 0 {
			return true
		}
		q := ((fx-float64(rl[k]))/math.Max(0.5, float64(rr[k])-float64(rl[k])))*2 - 1
		if half < 0 {
			return q < -0.18
		}
		return q >= -0.18
	}
	X0 := math.Floor(x - w/2 - math.Abs(lean) - 2)
	X1 := math.Ceil(x + w/2 + math.Abs(lean) + 2)
	for py := math.Floor(base-h) - 1; py < math.Ceil(base); py++ {
		for px := X0; px <= X1; px++ {
			if !inside(px, py) {
				if py+0.5 <= floorY && (inside(px-1, py) || inside(px+1, py) || inside(px, py+1) || (inside(px, py-1) && py < base-1)) {
					p.Dot(px, py, S[0], a)
				}
				continue
			}
			if white {
				p.Dot(px, py, pixelfx.White, a)
				continue
			}
			k0 := int(py - Y0)
			u := float64(ru[k0])
			c := x + lean*u
			l, r := float64(rl[k0]), float64(rr[k0])
			fx := px + 0.5
			// which stratum this row sits in: facets jog sideways at each bedding plane, like stacked rock
			band := 0
			st := false
			for _, s := range sp.strata {
				d := u*h - s*h - sp.slope*(fx-c)
				if d > 0 {
					band++
				}
				if math.Abs(d) < 0.5 {
					st = true
				}
			}
			jog := (pixelfx.Rand(float64(band), sp.seed) - 0.5) * 0.3
			q := ((fx-l)/math.Max(0.5, r-l))*2 - 1
			qr := -0.18 + jog
			var i int
			switch {
			case u > 0.93:
				if q < qr+0.3 {
					i = 7
				} else {
					i = 5
				}
			case math.Abs(q-qr) < math.Max(0.09, 0.55/math.Max(1, r-l)):
				i = 7
			case q < qr:
				if fx-l < 1 {
					i = 7
				} else if q < -0.5+jog {
					i = 6
				} else {
					i = 5
				}
			default:
				if r-fx < 1 {
					i = 3
				} else if q < 0.38+jog {
					i = 4
				} else {
					i = 2
				}
			}
			if st && i < 7 {
				i--
			}
			fleck := pixelfx.Rand(px*7+3, py*13+5)
			if fleck < 0.035 {
				if q < qr {
					i = 7
				} else {
					i--
				}
			} else if fleck > 0.965 {
				i--
			}
			if glint >= 0 && q < qr+0.1 {
				g := math.Abs(u - glint)
				if g < 0.035 {
					i = 8
				} else if g < 0.07 {
					i = max(i, 7)
				}
			}
			if i >= 8 {
				p.Dot(px, py, pixelfx.White, a)
			} else {
				p.Dot(px, py, S[int(pixelfx.ClampTo(float64(i), 0, float64(len(S)-1)))], a)
			}
		}
	}
	// cracks spreading over it, a lit lip on one side of each
	if len(opts.cracks) > 0 && opts.crack > 0 {
		for _, path := range opts.cracks {
			m := max(1, int(math.Floor(float64(len(path))*opts.crack)))
			for k := 1; k < m; k++ {
				u0, v0 := path[k-1][0], path[k-1][1]
				u1, v1 := path[k][0], path[k][1]
				side0 := 1.0
				if v0 < 0 {
					side0 = -1
				}
				side1 := 1.0
				if v1 < 0 {
					side1 = -1
				}
				ax, ay := x+lean*u0+v0*hw(u0, side0), base-u0*h
				bx, by := x+lean*u1+v1*hw(u1, side1), base-u1*h
				if u1 > clip {
					break
				}
				if !inside(math.Floor(ax), math.Floor(ay)) || !inside(math.Floor(bx), math.Floor(by)) {
					continue
				}
				hot, Gl := opts.heat, opts.glowRamp
				if hot > 0.05 && len(Gl) > 0 {
					// a seam of light inside the crack, a dark lip below it
					p.Line(ax+1, ay, bx+1, by, S[0], a)
					gc := Gl[1]
					switch {
					case hot > 0.7:
						gc = Gl[4]
					case hot > 0.4:
						gc = Gl[3]
					case hot > 0.2:
						gc = Gl[2]
					}
					p.Line(ax, ay, bx, by, gc, a)
					if k%2 == 0 {
						p.Glow(bx, by, 7, Gl[2], 0.35*hot)
					}
				} else {
					p.Line(ax, ay, bx, by, S[0], a)
					p.Dot(bx+1, by-1, S[6], a*0.7)
				}
			}
		}
	}
}

// spikeLook returns the random look of one spike: notches on its edges, strata across it.
func spikeLook(R *pixelfx.Rng) spikeLookData {
	nNotches := int(R.Int(3, 5))
	notches := make([]spikeNotch, 0, nNotches)
	for i := 0; i < nNotches; i++ {
		u := R.Range(0.1, 0.8)
		side := R.Sign()
		d := R.Range(0.8, 1.8)
		notches = append(notches, spikeNotch{u: u, side: side, d: d})
	}
	nStrata := int(R.Int(2, 4))
	strata := make([]float64, 0, nStrata)
	for i := 0; i < nStrata; i++ {
		strata = append(strata, R.Range(0.12, 0.8))
	}
	slope := R.Range(-0.25, 0.25)
	seed := R.Int(1, 9999)
	return spikeLookData{notches: notches, strata: strata, slope: slope, seed: seed}
}

// spikeCrack returns a crack that branches up a spike: points [u (height 0..1), v (-1 left edge..1 right edge)].
func spikeCrack(R *pixelfx.Rng, u0, v0, dir float64) [][2]float64 {
	pts := [][2]float64{{u0, v0}}
	u, v := u0, v0
	for i := 0; i < 9; i++ {
		u += R.Range(0.03, 0.08)
		v += dir*R.Range(0.05, 0.28) + R.Range(-0.12, 0.12)
		if u > 0.95 || math.Abs(v) > 1 {
			break
		}
		pts = append(pts, [2]float64{u, v})
	}
	return pts
}

// floorCrack returns a jagged floor crack from x = 0 out to side * len: points [x, y], y 0..2 into the floor.
func floorCrack(R *pixelfx.Rng, side, length float64) [][2]float64 {
	pts := [][2]float64{{0, 0}}
	for x := 0.0; math.Abs(x) < length; {
		x += side * R.Range(2, 5)
		pts = append(pts, [2]float64{x, R.Int(0, 2)})
	}
	return pts
}

// drawLeaf draws a leaf length px long and wid px across at its widest, centered on (x, y), turned by ang and flipping about its midrib by fl.
func drawLeaf(p *pixelfx.Painter, x, y, length, wid, ang, fl float64, pal [4]uint32, alpha float64) {
	ca, sa := math.Cos(ang), math.Sin(ang)
	sq := math.Cos(fl)
	w := math.Max(0.55, math.Abs(sq)*wid)
	under := sq < 0
	R := int(math.Ceil(length/2 + 2))
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	inside := func(i, j int) int {
		fi, fj := float64(i)+0.5, float64(j)+0.5
		u := fi*ca + fj*sa + length/2
		v := -fi*sa + fj*ca
		if u < 0 || u > length {
			return 0
		}
		half := w * math.Pow(math.Sin((pi*u)/length), 0.75)
		if math.Abs(v) <= half {
			if v < 0 {
				return 1
			}
			return 2
		}
		return 0
	}
	for j := -R; j <= R; j++ {
		for i := -R; i <= R; i++ {
			s := inside(i, j)
			if s == 0 {
				if inside(i-1, j) != 0 || inside(i+1, j) != 0 || inside(i, j-1) != 0 || inside(i, j+1) != 0 {
					p.Dot(X+float64(i), Y+float64(j), pal[0], alpha)
				}
				continue
			}
			fi, fj := float64(i)+0.5, float64(j)+0.5
			u := fi*ca + fj*sa + length/2
			v := -fi*sa + fj*ca
			rib := math.Abs(v) < 0.5 && u > length*0.12 && u < length*0.88 && w > 1.2
			var c uint32
			if rib {
				c = pal[1]
			} else if (s == 1) != under {
				c = pal[3]
			} else {
				c = pal[2]
			}
			p.Dot(X+float64(i), Y+float64(j), c, alpha)
		}
	}
	// the stem trails off the base
	p.Dot(X-ca*(length/2+1), Y-sa*(length/2+1), pal[1], alpha)
}

type drop struct {
	x, y, vx, vy, s, delay float64
}

// drops simulates droplets of liquid arcing under gravity and landing on a floor.
type drops struct {
	list   []drop
	ground func(x float64) float64
}

func newDrops(ground func(x float64) float64) *drops {
	if ground == nil {
		ground = func(float64) float64 { return 0 }
	}
	return &drops{ground: ground}
}

func (ds *drops) add(x, y, vx, vy, s, delay float64) {
	ds.list = append(ds.list, drop{x: x, y: y, vx: vx, vy: vy, s: s, delay: delay})
}

func (ds *drops) update(dt float64, onLand func(d drop, y float64)) {
	for i := len(ds.list) - 1; i >= 0; i-- {
		d := &ds.list[i]
		if d.delay > 0 {
			d.delay -= dt
			continue
		}
		drag := 0.6 / d.s
		py := d.y
		d.vy += gravity * dt
		d.vx -= d.vx * drag * dt
		d.vy -= d.vy * drag * 0.5 * dt
		d.x += d.vx * dt
		d.y += d.vy * dt
		g := ds.ground(d.x)
		if d.vy > 0 && py <= g && d.y >= g {
			landed := *d
			if onLand != nil {
				onLand(landed, g)
			}
			ds.list = append(ds.list[:i], ds.list[i+1:]...)
		}
	}
}

func (ds *drops) draw(p *pixelfx.Painter, C []uint32) {
	n := len(C)
	for _, d := range ds.list {
		if d.delay > 0 {
			continue
		}
		sp := pixelfx.Hypot(d.vx, d.vy)
		if sp == 0 {
			sp = 1
		}
		ux, uy := d.vx/sp, d.vy/sp
		length := math.Min(5, 0.5+sp*0.012)
		p.Line(d.x-ux*length, d.y-uy*length, d.x-ux, d.y-uy, C[n-4], 1)
		p.Px(d.x, d.y, C[n-1])
		if d.s > 1 {
			p.Px(d.x-ux, d.y-uy, C[n-2])
			p.Px(d.x+1, d.y, C[n-3])
		}
		p.Glow(d.x, d.y, 3+d.s*1.5, C[n-2], 0.3)
	}
}

// gasPuff draws a puff of gas: a darker belly hanging low, a body, a denser core, a lit cap up and to the left (hi: a rim).
func gasPuff(p *pixelfx.Painter, x, y, r, a float64, belly, body, cap, hi uint32) {
	if a < 0.02 || r < 0.6 {
		return
	}
	p.Disc(x+r*0.06, y+r*0.28, r*0.9, belly, a*0.55)
	p.Disc(x, y, r, body, a*0.45)
	p.Disc(x+r*0.03, y-r*0.02, r*0.76, body, a*0.5)
	p.Disc(x-r*0.2, y-r*0.34, r*0.52, cap, a*0.45)
	if hi != 0 {
		p.Disc(x-r*0.3, y-r*0.55, r*0.26, hi, a*0.32)
	}
}

type gasItem struct {
	x, y, vx, vy, r0, r1, t0, end, rise, seed float64
}

type gasSample struct {
	x, y, r, a, u float64
}

// gas holds puffs of gas with their own clock.
type gas struct {
	list []gasItem
	n    int
}

func (g *gas) add(t, x, y, vx, vy, r0, r1, end, rise float64) {
	seed := math.Mod(float64(g.n)*37.7, 100)
	g.n++
	g.list = append(g.list, gasItem{x: x, y: y, vx: vx, vy: vy, r0: r0, r1: r1, t0: t, end: end, rise: rise, seed: seed})
}

func (g *gas) prune(t float64) {
	for i := len(g.list) - 1; i >= 0; i-- {
		if t > g.list[i].end {
			g.list = append(g.list[:i], g.list[i+1:]...)
		}
	}
}

func (g *gas) at(q gasItem, t float64) gasSample {
	age := t - q.t0
	e := 1 - math.Exp(-age*2.2)
	sw := math.Min(1, age)
	x := q.x + (q.vx/2.2)*e + math.Sin(t*1.7+q.seed)*2.5*sw
	y := q.y + (q.vy/2.2)*e - q.rise*age + math.Sin(t*1.3+q.seed*2)*1.5*sw
	u := pixelfx.Clamp(age / math.Max(0.01, q.end-q.t0))
	r := q.r0 + (q.r1-q.r0)*(1-math.Pow(1-math.Min(1, age/1.2), 3))
	return gasSample{x: x, y: y, r: r, a: pixelfx.Clamp(age/0.15) * (1 - pixelfx.Clamp((u-0.4)/0.6)), u: u}
}

func (g *gas) draw(p *pixelfx.Painter, t float64, belly, body, cap, hi uint32, k float64, glow uint32) {
	for _, q := range g.list {
		s := g.at(q, t)
		if s.a <= 0.02 {
			continue
		}
		h := uint32(0)
		if s.u < 0.5 {
			h = hi
		}
		gasPuff(p, s.x, s.y, s.r, s.a*k, belly, body, cap, h)
		if glow != 0 && s.r > 4 {
			p.Glow(s.x, s.y, s.r*1.6, glow, 0.07*s.a*k)
		}
	}
}
