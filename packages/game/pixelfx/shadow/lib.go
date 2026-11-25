// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame puff (after sim.js's FIRE) and the swirling pull (attract) from Sprite Fusion's open-source black-hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), MIT License, Copyright (c) 2026 Hugo Duprez.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Shared bits for the shadow effects: the void palette, light that black shapes cut, smoke lit from one side, violet flame, obsidian slivers, ink drops.

// Float64 variables for constants combined with other constants so expressions round step by step as JavaScript's do.
var (
	pi      = math.Pi
	tau     = pixelfx.Tau
	gravity = float64(pixelfx.Gravity)
)

// voidHex is dark magic, dark to bright: void black, deep violet, a white-lilac edge.
var voidHex = []string{"#06040b", "#150c26", "#2a1150", "#461a84", "#6f2cc0", "#9d5cec", "#cfa6ff", "#f6eaff"}

func voidPalette() pixelfx.Value { return pixelfx.Palette(voidHex...) }

// unlight takes the light back off every pixel in a local box where dark(x, y) is true, keeping a share keep (0..1): call it last, so black shapes stay black under every glow, spark and ring drawn before.
func unlight(p *pixelfx.Painter, x0, y0, x1, y1 float64, dark func(x, y float64) bool, keep float64) {
	L, w, ox, oy := p.L, p.W, p.OX, p.OY
	p.Each(x0, y0, x1, y1, func(x, y float64, sx, sy int) {
		if !dark(x, y) {
			return
		}
		i := (sy-oy)*w + (sx - ox)
		d := L[i]
		if d == 0 {
			return
		}
		if keep <= 0 {
			L[i] = 0
		} else {
			b := uint32(pixelfx.ToInt32(float64((d>>16)&255) * keep))
			g := uint32(pixelfx.ToInt32(float64((d>>8)&255) * keep))
			r := uint32(pixelfx.ToInt32(float64(d&255) * keep))
			L[i] = (255 << 24) | (b << 16) | (g << 8) | r
		}
	})
}

// annulus visits the pixel cells of the annulus r0 <= d < r1 round (x, y), row by row, jumping the hole: fn(cx, cy, d).
func annulus(x, y, r0, r1 float64, fn func(cx, cy, d float64)) {
	r02 := -1.0
	if r0 > 0 {
		r02 = r0 * r0
	}
	r12 := r1 * r1
	for py := math.Floor(y - r1); py <= math.Ceil(y+r1); py++ {
		cy := py + 0.5
		dy := cy - y
		dy2 := dy * dy
		if dy2 >= r12 {
			continue
		}
		xo := math.Sqrt(r12 - dy2)
		xi := 0.0
		if dy2 < r02 {
			xi = math.Sqrt(r02 - dy2)
		}
		for px, x1 := math.Floor(x-xo), math.Ceil(x+xo); px <= x1; px++ {
			cx := px + 0.5
			dx := cx - x
			if xi > 0 && dx > -xi && dx < xi {
				px = math.Max(px, math.Floor(x+xi)-1)
				continue
			}
			d2 := dx*dx + dy2
			if d2 < r12 && d2 >= r02 {
				fn(cx, cy, math.Sqrt(d2))
			}
		}
	}
}

// ringFast draws a ring from radius r - w out to r round (x, y), like p.Ring but visiting only the band's own pixels, so a big thin shock ring stays cheap.
func ringFast(p *pixelfx.Painter, x, y, r, w float64, c uint32, a float64) {
	if r <= 0 {
		return
	}
	r0 := math.Max(0, r-w)
	annulus(x, y, r0, r, func(cx, cy, _ float64) {
		p.Dot(cx, cy, c, a)
	})
}

// ringFastFn draws a ring from radius r - w out to r round (x, y) whose color and alpha are fn(v, ang): v 0 inner to 1 outer.
func ringFastFn(p *pixelfx.Painter, x, y, r, w float64, fn func(v, ang float64) (uint32, float64)) {
	if r <= 0 {
		return
	}
	r0 := math.Max(0, r-w)
	den := r - r0
	if den == 0 {
		den = 1
	}
	annulus(x, y, r0, r, func(cx, cy, d float64) {
		v := (d - r0) / den
		ang := math.Atan2(cy-y, cx-x)
		c, a := fn(v, ang)
		p.Dot(cx, cy, c, a)
	})
}

// ringLight adds light hugging a circle of radius R at (x, y), falling off over w either side, only where keep(x, y) (all pixels when keep is nil).
func ringLight(p *pixelfx.Painter, x, y, R, w float64, c uint32, k float64, keep func(x, y float64) bool) {
	if w < 1 || k <= 0.004 {
		return
	}
	annulus(x, y, math.Max(0, R-w), R+w, func(cx, cy, d float64) {
		if keep != nil && !keep(cx, cy) {
			return
		}
		f := 1 - math.Abs(d-R)/w
		p.Light(cx, cy, c, f*f*k)
	})
}

// halo draws a ring of light hugging a black disc: k at radius r0, falling to nothing at r1, and none inside r0.
func halo(p *pixelfx.Painter, x, y, r0, r1 float64, c uint32, k float64) {
	if r1 <= r0 || k <= 0.004 {
		return
	}
	w := r1 - r0
	annulus(x, y, math.Max(0, r0), r1, func(cx, cy, d float64) {
		f := 1 - (d-r0)/w
		p.Light(cx, cy, c, f*f*k)
	})
}

type starGrid struct {
	cy   float64
	r0   int
	n    int
	data []uint8
}

// blackStar rasterizes a black star once: four long arms (L) and four short ones, turned rot, at (0, cy), its cells in a grid. The arms keep a 1 px black spine to their tips.
func blackStar(cy, L, rot, short, q float64) *starGrid {
	r0 := int(math.Ceil(L)) + 2
	n := r0*2 + 1
	data := make([]uint8, n*n)
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			x, y := float64(i-r0)+0.5, cy+float64(j-r0)+0.5
			if inStar(x, y, 0, cy, L, rot, q) || inStar(x, y, 0, cy, L*short, rot+pi/4, q) {
				data[j*n+i] = 1
			}
		}
	}
	for a := 0; a < 4; a++ {
		ca, sa := math.Cos(rot+(float64(a)*pi)/2), math.Sin(rot+(float64(a)*pi)/2)
		for d := 0.0; d < L-1; d += 0.5 {
			i, j := int(math.Floor(ca*d))+r0, int(math.Floor(sa*d))+r0
			data[j*n+i] = 1
		}
	}
	return &starGrid{cy: cy, r0: r0, n: n, data: data}
}

func (s *starGrid) at(x, y float64) bool {
	i, j := int(math.Floor(x))+s.r0, int(math.Floor(y-s.cy))+s.r0
	return i >= 0 && j >= 0 && i < s.n && j < s.n && s.data[j*s.n+i] == 1
}

// unlight takes the light off the star's black cells (call last, like unlight).
func (s *starGrid) unlight(p *pixelfx.Painter) {
	Lb, w, h, ox, oy := p.L, p.W, p.H, p.OX, p.OY
	for j := 0; j < s.n; j++ {
		for i := 0; i < s.n; i++ {
			if s.data[j*s.n+i] == 0 {
				continue
			}
			x, y := float64(i-s.r0)+0.5, s.cy+float64(j-s.r0)+0.5
			sx, sy := p.CellX(x, y)-ox, p.CellY(x, y)-oy
			if sx >= 0 && sy >= 0 && sx < w && sy < h {
				Lb[sy*w+sx] = 0
			}
		}
	}
}

// draw fills the star and burns a rim on the cells just outside it (except where skip(x, y)).
func (s *starGrid) draw(p *pixelfx.Painter, c, rim uint32, skip func(x, y float64) bool) {
	for j := 0; j < s.n; j++ {
		for i := 0; i < s.n; i++ {
			x, y := float64(i-s.r0)+0.5, s.cy+float64(j-s.r0)+0.5
			if s.data[j*s.n+i] != 0 {
				p.Px(x, y, c)
			} else if rim != 0 && !skip(x, y) && (s.at(x-1, y) || s.at(x+1, y) || s.at(x, y-1) || s.at(x, y+1)) {
				p.Px(x, y, rim)
			}
		}
	}
}

// inStar reports whether (x, y) is inside a four-point star at (cx, cy), arms L long, turned rot; q < 1 hollows the sides into thin spikes.
func inStar(x, y, cx, cy, L, rot, q float64) bool {
	if L <= 0 {
		return false
	}
	c, s := math.Cos(rot), math.Sin(rot)
	dx, dy := x-cx, y-cy
	u := math.Abs(dx*c+dy*s) / L
	v := math.Abs(-dx*s+dy*c) / L
	// inside needs the smaller of u, v under 0.5^(1/q): most points fail that before any pow
	return u < 1 && v < 1 && math.Min(u, v) <= math.Pow(0.5, 1/q) && math.Pow(u, q)+math.Pow(v, q) <= 1
}

// attractStep applies one particle's pull toward (x, y) and reports whether it was eaten.
func attractStep(qx, qy float64, qvx, qvy *float64, x, y, G, reach, swirl, r0, dt, wt, vmax float64) bool {
	dx, dy := x-qx, y-qy
	d2 := dx*dx + dy*dy
	if d2 > reach*reach {
		return false
	}
	d := math.Sqrt(d2)
	if d == 0 {
		d = 1
	}
	if d < r0 {
		return true
	}
	a := (G / (d2 + r0*r0)) * math.Min(1, (reach-d)/(reach*0.25)) * wt * dt
	ux, uy := dx/d, dy/d
	*qvx += (ux - uy*swirl) * a
	*qvy += (uy + ux*swirl) * a
	drag := math.Min(0.2, (1-d/reach)*1.6*dt)
	*qvx -= *qvx * drag
	*qvy -= *qvy * drag
	v := pixelfx.Hypot(*qvx, *qvy)
	if v > vmax {
		*qvx *= vmax / v
		*qvy *= vmax / v
	}
	return false
}

// attractParticles pulls particles toward (x, y) and removes those that reach r0.
func attractParticles(ps *pixelfx.Particles, x, y, G, reach, swirl, r0, dt float64, w func(q *pixelfx.Particle) float64, vmax float64) int {
	eaten := 0
	for i := len(ps.List) - 1; i >= 0; i-- {
		q := &ps.List[i]
		wt := w(q)
		if wt == 0 || q.Delay > 0 {
			continue
		}
		if attractStep(q.X, q.Y, &q.VX, &q.VY, x, y, G, reach, swirl, r0, dt, wt, vmax) {
			ps.List = slices.Delete(ps.List, i, i+1)
			eaten++
		}
	}
	return eaten
}

// attractPuffs pulls puffs toward (x, y) and removes those that reach r0.
func attractPuffs(pf *puffs, x, y, G, reach, swirl, r0, dt float64, w func(q *puff) float64, vmax float64) int {
	eaten := 0
	for i := len(pf.list) - 1; i >= 0; i-- {
		q := &pf.list[i]
		wt := w(q)
		if wt == 0 || q.delay > 0 {
			continue
		}
		if attractStep(q.x, q.y, &q.vx, &q.vy, x, y, G, reach, swirl, r0, dt, wt, vmax) {
			pf.list = slices.Delete(pf.list, i, i+1)
			eaten++
		}
	}
	return eaten
}

type puffKind int

const (
	pkFlame puffKind = 0
	pkSoot  puffKind = 1
)

type puff struct {
	k               puffKind
	x, y, vx, vy    float64
	life, max, size float64
	delay, seed     float64
	smokes          bool
}

type puffLight struct {
	x, y, k, reach float64
}

type puffsOpts struct {
	rng   *pixelfx.Rng
	flame []uint32
	soot  []uint32
	cap   uint32
	rim   []uint32
	floor func(x float64) float64
}

// puffs simulates violet flame and dark smoke lit from one side.
type puffs struct {
	R     *pixelfx.Rng
	list  []puff
	t     float64
	F     []uint32
	soot  []uint32
	cap   uint32
	rim   []uint32
	floor func(x float64) float64
}

func newPuffs(o puffsOpts) *puffs {
	return &puffs{R: o.rng, F: o.flame, soot: o.soot, cap: o.cap, rim: o.rim, floor: o.floor}
}

func (pf *puffs) spawn(k puffKind, x, y, vx, vy, life, size, delay float64, smokes bool) {
	pf.list = append(pf.list, puff{k: k, x: x, y: y, vx: vx, vy: vy, life: life, max: life, size: size, delay: delay, smokes: smokes, seed: pf.R.Next() * 100})
}

func (pf *puffs) update(dt float64) {
	pf.t += dt
	t := pf.t
	for i := len(pf.list) - 1; i >= 0; i-- {
		q := &pf.list[i]
		if q.delay > 0 {
			q.delay -= dt
			continue
		}
		q.life -= dt
		if q.life <= 0 {
			if q.k == pkFlame && q.smokes {
				x, y, vx, vy, sz := q.x, q.y, q.vx, q.vy, q.size
				pf.spawn(pkSoot, x, y-1, vx*0.5, vy*0.5-6, pf.R.Range(0.9, 1.6), sz*1.25, 0, false)
			}
			pf.list = slices.Delete(pf.list, i, i+1)
			continue
		}
		if q.k == pkFlame {
			q.vy -= 95 * dt
			q.vx += math.Sin(t*17+q.seed) * 30 * dt
			q.vx -= q.vx * 5 * dt
			q.vy -= q.vy * 3.2 * dt
		} else {
			q.vy -= 18 * dt
			q.vx += math.Sin(t*1.3+q.seed) * 8 * dt
			q.vx -= q.vx * 1.6 * dt
			q.vy -= q.vy * 1.3 * dt
		}
		q.x += q.vx * dt
		q.y += q.vy * dt
		// smoke rolls along the floor rather than through it
		if pf.floor != nil && q.k == pkSoot {
			f := pf.floor(q.x) - q.size*0.6
			if q.y > f {
				q.y = f
				q.vy = math.Min(0, q.vy)
				q.vx *= 1 - 2*dt
			}
		}
	}
}

func (pf *puffs) draw(p *pixelfx.Painter, kind puffKind, light *puffLight) {
	F, n := pf.F, len(pf.F)
	for qi := range pf.list {
		q := &pf.list[qi]
		if q.delay > 0 || q.k != kind {
			continue
		}
		k, age, x, y := q.life/q.max, 1-q.life/q.max, q.x, q.y
		if q.k == pkFlame {
			// a ball of flame: a darker rim, a lit core up and left, a tongue on top, a hot heart while young
			heat := math.Min(0.999, math.Pow(age, 0.62)+math.Mod(q.seed, 1)*0.1)
			idx := int(math.Min(float64(n-2), math.Floor(heat*float64(n-1))))
			r := q.size * (0.55 + 0.45*math.Min(1, age*7)) * (1 - age*0.55)
			c := F[idx]
			if idx >= n-3 {
				p.Disc(x, y, r, c, math.Min(1, k*3)*0.8)
			} else {
				p.Circle(x, y, r, F[min(n-1, idx+2)], 1)
				p.Circle(x-r*0.16, y-r*0.22, r*0.78, c, 1)
				if r > 1.6 {
					p.Circle(x, y-r*0.9, r*0.42, c, 1)
				}
				if idx <= 3 && r > 1.8 {
					p.Circle(x-r*0.28, y-r*0.34, r*0.36, F[max(0, idx-1)], 1)
				}
				if idx < 6 {
					p.Glow(x, y, r*2.4+2, c, 0.2*k)
				}
			}
			continue
		}
		// soot: a soft body and a denser core, a dim cap on top, and lit from the side facing the light by two discs pushed toward it (a soft wash over the near half, a brighter lip at its edge)
		r := q.size * (0.65 + math.Min(1, age*2.5)*0.45 + age*0.45)
		a := math.Min(1, age*10) * math.Min(1, k*2.2) * 0.72
		body := pixelfx.Mix(pf.soot[0], pf.soot[1], age)
		// one pass for the body: thinner at the edge, denser in the core (two layers' worth)
		edge, core := a*0.5, 1-(1-a*0.5)*(1-a*0.62)
		p.DiscFn(x, y+r*0.06, r, func(d, _ float64) (uint32, float64) {
			if d < 0.72 {
				return body, core
			}
			return body, edge
		})
		lk := 0.0
		if light != nil {
			lk = light.k
		}
		if lk < 0.5 {
			p.Disc(x-r*0.2, y-r*0.32, r*0.48, pf.cap, a*0.32*(1-age*0.7)*(1-lk*2))
		}
		if lk <= 0.02 {
			continue
		}
		lx, ly := light.x-x, light.y-y
		dl := math.Sqrt(lx*lx + ly*ly)
		if dl == 0 {
			dl = 1
		}
		ux, uy := lx/dl, ly/dl
		lit := math.Min(1, light.k*pixelfx.Clamp(1.3-dl/light.reach)) * a
		if lit <= 0.02 {
			continue
		}
		hot, warm, dim := pf.rim[0], pf.rim[1], pf.rim[2]
		p.Disc(x+ux*r*0.26, y+uy*r*0.26, r*0.72, dim, lit*0.55)
		p.Disc(x+ux*r*0.5, y+uy*r*0.5, r*0.46, warm, lit*0.5)
		if r > 3 {
			p.Disc(x+ux*r*0.68, y+uy*r*0.68, r*0.24, hot, lit*0.4)
		}
	}
}

// shard draws an obsidian shard from pixelfx.Bit: a thin triangle pointing along its spin angle, a dark violet body, the long edge facing the light at (lx, ly) lit (brighter with lk), the other in shadow, a white glint at the tip while the light is strong. C is an 8-step palette.
func shard(p *pixelfx.Painter, b *pixelfx.Bit, C []uint32, lx, ly, lk float64) {
	fa, ca, sa := math.Min(1, b.Life/0.4), math.Cos(b.A), math.Sin(b.A)
	L := math.Max(2, b.S)
	W := math.Max(2, L*0.55)
	side := -1.0
	if -sa*(lx-b.X)+ca*(ly-b.Y) > 0 {
		side = 1
	}
	rim := C[3]
	switch {
	case lk > 0.6:
		rim = C[6]
	case lk > 0.3:
		rim = C[5]
	case lk > 0.08:
		rim = C[4]
	}
	rimDim := C[max(3, slices.Index(C, rim)-1)]
	E := L/2 + 1.5
	p.Shade(b.X-E, b.Y-E, b.X+E, b.Y+E, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		u := dx*ca + dy*sa
		v := (-dx*sa + dy*ca) * side
		if u < -L/2-0.4 || u > L/2+0.4 {
			return 0
		}
		half := ((W/2)*(L/2-u))/L + 0.4
		if math.Abs(v) > half {
			return 0
		}
		out.A = fa
		if u > L/2-1.3 && lk > 0.3 {
			return pixelfx.White
		}
		if v > half-1 {
			if u > -L/4 {
				return rim
			}
			return rimDim
		}
		if v < -half+1 {
			return C[1]
		}
		return C[2]
	})
	if lk > 0.04 {
		p.Glow(b.X, b.Y, 3+L*0.6, C[5], 0.2*lk*fa)
	}
}

type drop struct {
	x, y, vx, vy, size float64
}

type dropsOpts struct {
	floor  func(x float64) float64
	onLand func(x, f, vy, size float64)
	g      float64
}

// drops simulates ink drops that fly under gravity, land on the floor and call onLand.
type drops struct {
	list   []drop
	floor  func(x float64) float64
	onLand func(x, f, vy, size float64)
	g      float64
}

func newDrops(o dropsOpts) *drops {
	fl := o.floor
	if fl == nil {
		fl = func(float64) float64 { return 0 }
	}
	g := o.g
	if g == 0 {
		g = 0.8
	}
	return &drops{floor: fl, onLand: o.onLand, g: g}
}

func (dr *drops) add(x, y, vx, vy, size float64) {
	dr.list = append(dr.list, drop{x: x, y: y, vx: vx, vy: vy, size: size})
}

func (dr *drops) update(dt float64) {
	for i := len(dr.list) - 1; i >= 0; i-- {
		d := &dr.list[i]
		d.vy += gravity * dr.g * dt
		d.vx -= d.vx * 0.4 * dt
		d.x += d.vx * dt
		d.y += d.vy * dt
		f := dr.floor(d.x)
		if d.vy > 0 && d.y >= f {
			if dr.onLand != nil {
				dr.onLand(d.x, f, d.vy, d.size)
			}
			dr.list = slices.Delete(dr.list, i, i+1)
		}
	}
}

func (dr *drops) draw(p *pixelfx.Painter, body, shine uint32) {
	for _, d := range dr.list {
		up := d.vy < 0
		p.Px(d.x, d.y, body)
		if d.size > 1.4 {
			off := -1.0
			if up {
				off = 1
			}
			p.Px(d.x, d.y+off, body)
		}
		soff := 0.0
		if d.size > 1.4 && !up {
			soff = 1
		}
		p.Px(d.x, d.y-soff, shine)
	}
}

type poolRipple struct {
	x, t0, life, w float64
}

// floorPool draws a dark pool opened in the floor, seen at a slant (radius rx, centred on the floor at x = 0): the far lip lit, the near lip dark, ripples running out across it, a sheen along the far side, a wet fringe.
func floorPool(p *pixelfx.Painter, C []uint32, rx, t float64, ripples []poolRipple) {
	if rx < 1 {
		return
	}
	ry := rx*0.17 + 0.6
	p.Shade(-rx-3, -ry-2, rx+3, ry+2, func(x, y float64, _ *pixelfx.Out) uint32 {
		dx, dy := x/rx, (y-0.5)/ry
		d := math.Sqrt(dx*dx + dy*dy)
		if d > 1.18 {
			return 0
		}
		if d > 1 {
			if dy < 0 && d < 1.09 {
				switch {
				case math.Abs(dx) < 0.55:
					return C[6]
				case math.Abs(dx) < 0.85:
					return C[5]
				}
				return C[4]
			}
			if dy >= 0 && d < 1.09 {
				return C[2]
			}
			if y > -0.5 && pixelfx.Noise(x*0.7, 9) > 0.15 {
				return C[1]
			}
			return 0
		}
		// ripples: ellipses on the slanted surface, kept about a pixel thick all the way round
		for _, r := range ripples {
			age := t - r.t0
			rr := age*46*r.w + 1
			ex, ey := x-r.x, (y-0.5)/0.17
			e := math.Sqrt(ex*ex + ey*ey)
			if e == 0 {
				e = 1
			}
			ey17 := ey / 0.17
			g := math.Sqrt(ex*ex+ey17*ey17) / e
			if math.Abs(e-rr) < 0.55*g && age < r.life {
				if age < r.life*0.5 {
					return C[4]
				}
				return C[3]
			}
		}
		if dy < -0.55 && dy > -0.85 && pixelfx.Noise(x*0.25+t*1.3, 4) > 0.2 {
			if pixelfx.Noise(x*0.6-t*2, 6) > 0.55 {
				return C[5]
			}
			return C[2]
		}
		// the far inner wall catches the glow from below: violet just under the lip, falling to black
		if dy < -0.35 && d > 0.72 {
			if d > 0.9 {
				return C[3]
			}
			return C[2]
		}
		if d > 0.86 {
			return C[1]
		}
		return C[0]
	})
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
