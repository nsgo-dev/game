// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the droplet sprites (a pixel, a shaded 2x2, a round bead with a highlight, each trailing a tail along its motion) from Sprite Fusion's open-source blood (https://www.spritefusion.com/pixel-art-vfx/blood), MIT License, Copyright (c) 2026 Hugo Duprez.

package water

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the water effects: the water ramp, droplets that fly over a water surface seen at a low angle and land with rings, ripple trains, lacy foam, foam billows, wet stone.

var (
	// pi, tau and sqrt2 are variables so expressions combining them with other constants round step by step as JavaScript's do.
	pi    = math.Pi
	tau   = pixelfx.Tau
	sqrt2 = math.Sqrt2

	// waterPal is water, deep to white: deep color only in shadow, tinted middles, white crests.
	waterPal = pixelfx.Palette("#0a1630", "#123063", "#1b5596", "#2a7fc4", "#45a9e3", "#86d3f5", "#cdf1ff", "#ffffff")
	// rockRamp is wet stone for props (lips, basins, vents), outline to highlight.
	rockRamp = pixelfx.Ramp("#141219", "#221f2b", "#34303f", "#4a4558", "#655f74", "#8a8498")
)

func rock() []uint32 { return rockRamp }

// bead draws a droplet at (x, y) moving at (vx, vy) px/s: s 1 is a pixel, 2 a shaded 2x2, 3 a round bead with a highlight; a tail trails back along its motion, longer the faster it goes. C is an 8-step water ramp.
func bead(p *pixelfx.Painter, x, y, vx, vy, s float64, C []uint32, a float64) {
	sp := pixelfx.Hypot(vx, vy)
	if sp == 0 {
		sp = 1
	}
	ux, uy, tail := vx/sp, vy/sp, math.Min(5, sp*0.011)
	if tail >= 1 {
		p.LineFn(x-ux*(tail+1), y-uy*(tail+1), x-ux*1.2, y-uy*1.2, func(u float64) (uint32, float64) {
			if u < 0.45 {
				return C[3], a
			}
			if s > 1 {
				return C[5], a
			}
			return C[4], a
		})
	}
	if s >= 3 {
		p.Circle(x, y, 1.75, C[4], a)
		p.Circle(x-0.25, y-0.25, 1.15, C[6], a)
		p.Dot(x-0.7, y-0.7, pixelfx.White, a)
	} else if s >= 2 {
		X, Y := math.Floor(x), math.Floor(y)
		p.Dot(X, Y, pixelfx.White, a)
		p.Dot(X+1, Y, C[6], a)
		p.Dot(X, Y+1, C[5], a)
		p.Dot(X+1, Y+1, C[4], a)
	} else {
		c := C[5]
		if sp > 160 {
			c = C[6]
		}
		p.Dot(x, y, c, a)
	}
}

type drop struct {
	x, z, h, vx, vz, vh, s, life, max, delay float64
	small                                    bool
}

// drops simulates droplets in flight over a flat surface seen at a low angle: x across, z into the screen (negative is far), h height above the surface; on screen they sit at (x, z * sy - h). Gravity pulls h down; when a drop reaches the surface it calls onLand(d) and is gone.
type drops struct {
	sy, g, drag float64
	onLand      func(*drop)
	list        []drop
}

func newDrops(sy, g, drag float64, onLand func(*drop)) *drops {
	if sy == 0 {
		sy = 0.22
	}
	if g == 0 {
		g = 820
	}
	if drag == 0 {
		drag = 0.35
	}
	return &drops{sy: sy, g: g, drag: drag, onLand: onLand}
}

func (ds *drops) add(d drop) {
	if d.life == 0 {
		d.life = 4
	}
	if d.s == 0 {
		d.s = 1
	}
	d.max = d.life
	ds.list = append(ds.list, d)
}

func (ds *drops) update(dt float64) {
	k := ds.drag * dt
	for i := len(ds.list) - 1; i >= 0; i-- {
		d := &ds.list[i]
		if d.delay > 0 {
			d.delay -= dt
			continue
		}
		d.life -= dt
		if d.life <= 0 {
			ds.list = append(ds.list[:i], ds.list[i+1:]...)
			continue
		}
		d.vh -= float64(ds.g * dt)
		d.vx -= float64(d.vx * k)
		d.vz -= float64(d.vz * k)
		d.vh -= float64(d.vh * k * 0.5)
		d.x += float64(d.vx * dt)
		d.z += float64(d.vz * dt)
		d.h += float64(d.vh * dt)
		if d.h <= 0 && d.vh < 0 {
			d.h = 0
			landed := *d
			ds.list = append(ds.list[:i], ds.list[i+1:]...)
			if ds.onLand != nil {
				ds.onLand(&landed)
			}
		}
	}
}

// draw draws the drops behind z = 0 (LayerBack), in front of it (LayerFront), or all (LayerBoth).
func (ds *drops) draw(p *pixelfx.Painter, C []uint32, layer pixelfx.Layer) {
	for i := range ds.list {
		d := &ds.list[i]
		if d.delay > 0 || (layer == pixelfx.LayerBack && d.z >= 0) || (layer == pixelfx.LayerFront && d.z < 0) {
			continue
		}
		x, y := d.x, d.z*ds.sy-d.h
		bead(p, x, y, d.vx, d.vz*ds.sy-d.vh, d.s, C, math.Min(1, d.life/0.2))
		p.Glow(x, y, 2.5+d.s, C[5], 0.1+0.05*d.s)
	}
}

type ringOpts struct {
	n     int
	gap   float64
	half  int
	hot   bool
	glint float64
	seed  float64
	wob   float64
}

// ring draws a ripple on a surface seen at a low angle: a train of n crests gap px apart around (cx, cy), squashed by sy, the outer crest brightest. k 0..1+ strength: a crisp 1 px crest that steps down the ramp as k falls and only breaks into dashes at the very end. half: 0 the whole ring, -1 the far half, 1 the near half.
func ring(p *pixelfx.Painter, cx, cy, r, sy, k float64, C []uint32, o ringOpts) {
	if k <= 0.02 || r < 0.5 {
		return
	}
	n := o.n
	if n == 0 {
		n = 2
	}
	gap := o.gap
	if gap == 0 {
		gap = 3
	}
	for j := 0; j < n; j++ {
		rr := r - float64(j)*gap
		if rr < 0.6 {
			break
		}
		kj := k * (1 - float64(j)*0.42)
		m := max(6, int(math.Ceil(rr*tau*1.3)))
		need := 1.15 - 2.3*math.Min(1, kj*1.25)
		for i := 0; i < m; i++ {
			a := (float64(i) / float64(m)) * tau
			s := math.Sin(a)
			near := s > 0
			if (o.half > 0 && !near) || (o.half < 0 && near) {
				continue
			}
			// as it weakens the crest breaks into glinting dashes (whole pixels, no dither), fixed to the ring
			if pixelfx.Noise(a*4.7+o.seed*13.1+float64(j)*5.3, o.seed)*0.65+pixelfx.Noise(a*11.3+o.seed*7.7, o.seed+3)*0.35 < need {
				continue
			}
			// the near side and the side toward the light (upper left) read brighter
			nearK := 0.7
			if near {
				nearK = 1
			}
			lit := kj * nearK * (0.88 - 0.12*math.Cos(a))
			var c uint32
			if o.hot && near && j == 0 {
				c = pixelfx.White
			} else if lit > 0.7 {
				c = C[6]
			} else if lit > 0.38 {
				c = C[5]
			} else {
				c = C[4]
			}
			ra := rr
			if o.wob != 0 {
				ra = rr + o.wob*pixelfx.Noise(a*2.6+o.seed*1.9+float64(j), o.seed+11)
			}
			x, y := cx+math.Cos(a)*ra, cy+s*ra*sy
			p.Dot(x, y, c, 1)
			if j == 0 {
				p.Light(x, y, C[4], 0.3*lit)
			}
		}
	}
	if o.glint > 0 && o.half >= 0 {
		x, y := cx+math.Cos(2.05)*r, cy+math.Sin(2.05)*r*sy
		p.Px(x, y, pixelfx.White)
		p.Glow(x, y, 3+o.glint*2, C[5], 0.35*o.glint)
	}
}

type ripple struct {
	x, z, r0, r1, life, age, k, gap, hot, glint, seed float64
	n                                                 int
}

type rippleOpts struct {
	n     int
	gap   float64
	k     float64
	hot   float64
	glint float64
	delay float64
}

// ripples are ripples that spread and fade.
type ripples struct {
	sy   float64
	seq  int
	list []ripple
}

func newRipples(sy float64) *ripples {
	if sy == 0 {
		sy = 0.22
	}
	return &ripples{sy: sy}
}

func (rs *ripples) add(x, z, r0, r1, life float64, o rippleOpts) {
	k := o.k
	if k == 0 {
		k = 1
	}
	n := o.n
	if n == 0 {
		n = 2
	}
	gap := o.gap
	if gap == 0 {
		gap = 3
	}
	hot := o.hot
	if hot == 0 {
		hot = 0.1
	}
	rs.seq++
	rs.list = append(rs.list, ripple{
		x: x, z: z, r0: r0, r1: r1, life: life,
		age: -o.delay, k: k, n: n, gap: gap, hot: hot, glint: o.glint, seed: float64(rs.seq),
	})
}

func (rs *ripples) update(dt float64) {
	for i := len(rs.list) - 1; i >= 0; i-- {
		rs.list[i].age += dt
		if rs.list[i].age >= rs.list[i].life {
			rs.list = append(rs.list[:i], rs.list[i+1:]...)
		}
	}
}

func (rs *ripples) draw(p *pixelfx.Painter, C []uint32, half int) {
	for i := range rs.list {
		q := &rs.list[i]
		if q.age < 0 {
			continue
		}
		u := q.age / q.life
		r := q.r0 + (q.r1-q.r0)*(1-math.Pow(1-u, 2.6))
		k := q.k * math.Pow(1-u, 1.7)
		ring(p, q.x, q.z*rs.sy, r, rs.sy, k, C, ringOpts{
			n: q.n, gap: q.gap * (0.55 + 0.6*u), half: half, hot: q.age < q.hot, glint: q.glint * k, seed: q.seed,
		})
	}
}

// lace draws lacy foam floating on the surface around (cx, cy), over an rx x ry patch: the crests of a noise field make a white net with holes, thinning out as k falls (1 thick, 0 gone). t drifts it; seed picks the pattern.
func lace(p *pixelfx.Painter, cx, cy, rx, ry, k float64, C []uint32, t, seed float64) {
	if k <= 0.01 || rx < 1 {
		return
	}
	sy := ry / rx
	p.Shade(cx-rx, cy-ry, cx+rx, cy+ry, func(x, y float64, o *pixelfx.Out) uint32 {
		dx, dy := (x-cx)/rx, (y-cy)/ry
		d := dx*dx + dy*dy
		if d > 1 {
			return 0
		}
		u, v := (x-cx)*0.36, ((y-cy)/sy)*0.36
		n := pixelfx.Noise2(u+seed*7.13, v-t*0.35, seed)
		m := pixelfx.Noise2(u*2.2-t*0.3, v*2.2+seed*3.1, seed+9)
		net := 1 - math.Abs(n)*4.6 + m*0.3
		need := 1.02 - k*(1.15-d*0.9)
		if net < need {
			return 0
		}
		if net > need+0.4 {
			o.C = pixelfx.White
		} else {
			o.C = C[6]
		}
		return 0
	})
}

type mistPuff struct {
	x, y, vx, vy, r0, r1, life, max, a, delay float64
	c                                         uint32
}

// mist holds soft puffs with no hard edge (alpha falls off to the rim), lit on top, that swell, drift on a slowing push and thin away.
type mist struct {
	list []mistPuff
}

func (ms *mist) add(x, y, vx, vy, r0, r1, life float64, c uint32, a, delay float64) {
	ms.list = append(ms.list, mistPuff{x: x, y: y, vx: vx, vy: vy, r0: r0, r1: r1, life: life, max: life, c: c, a: a, delay: delay})
}

func (ms *mist) update(dt float64) {
	for i := len(ms.list) - 1; i >= 0; i-- {
		m := &ms.list[i]
		if m.delay > 0 {
			m.delay -= dt
			continue
		}
		m.life -= dt
		if m.life <= 0 {
			ms.list = append(ms.list[:i], ms.list[i+1:]...)
			continue
		}
		m.vx -= float64(m.vx * 2.4 * dt)
		m.vy -= float64(m.vy*2.4*dt) + float64(7*dt)
		m.x += float64(m.vx * dt)
		m.y += float64(m.vy * dt)
	}
}

func (ms *mist) draw(p *pixelfx.Painter, haze uint32) {
	for i := range ms.list {
		m := &ms.list[i]
		if m.delay > 0 {
			continue
		}
		age := 1 - m.life/m.max
		r := m.r0 + (m.r1-m.r0)*(1-math.Pow(1-age, 2.2))
		a := m.a * math.Min(1, age/0.12) * math.Pow(1-age, 1.4)
		if a < 0.01 {
			continue
		}
		p.DiscFn(m.x, m.y, r, func(d, _ float64) (uint32, float64) {
			return m.c, a * (1 - d*d) * (1 - d*0.5)
		})
		p.DiscFn(m.x-r*0.2, m.y-r*0.32, r*0.55, func(d, _ float64) (uint32, float64) {
			return pixelfx.White, a * 0.25 * (1 - d) * (1 - d)
		})
		if haze != 0 {
			p.Glow(m.x, m.y, r*1.5, haze, ((0.16*a)/m.a)*math.Min(1, m.a*3))
		}
	}
}

// billow draws a billow of churning foam: a shadowed underside, a white body, a lit cap. a < 1 dithers it away.
func billow(p *pixelfx.Painter, x, y, r float64, C []uint32, a float64) {
	if r < 0.9 {
		p.Dot(x, y, C[6], a)
		return
	}
	p.Circle(x+r*0.12, y+r*0.2, r, C[4], a)
	p.Circle(x, y, r*0.84, C[6], a)
	if r > 1.6 {
		p.Circle(x-r*0.3, y-r*0.32, r*0.46, pixelfx.White, a)
	}
}

// pebble draws a pebble or rock: lit upper left, shaded lower right, a dark rim. ST is a 6-step stone ramp.
func pebble(p *pixelfx.Painter, x, y, r float64, ST []uint32) {
	p.Shade(x-r-1, y-r-1, x+r+1, y+r+1, func(px, py float64, o *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		d := pixelfx.Hypot(dx*1.1, dy)
		if d > r+0.8 {
			return 0
		}
		lit := (dx + dy) / math.Max(0.5, r)
		switch {
		case d > r:
			o.C = ST[0]
		case lit < -0.7:
			o.C = ST[5]
		case lit < 0.2:
			o.C = ST[4]
		case lit < 0.8:
			o.C = ST[3]
		default:
			o.C = ST[2]
		}
		return 0
	})
}
