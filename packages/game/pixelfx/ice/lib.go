// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// Shared by the ice effects: panes of ice that tumble and skid, polygon helpers, a character turned to ice, cracks that race through a shape. pixelfx adapts the crack walk and the faceted crystal shading from Sprite Fusion's open-source ice (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// pi and tau as float64 variables so expressions combining them with constants round step by step as JavaScript's do.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// icePalette is the ice ramp every effect of the category defaults to, dark to bright.
var icePalette = []string{"#151a3d", "#212f6b", "#2c58aa", "#3a8bd8", "#62c8f4", "#a6ecff", "#dcfbff", "#ffffff"}

// cross4 is the four neighbours of a pixel, as pixelfx lists them for a twinkle's arms.
var cross4 = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// clampIdx clamps a ramp index as pixelfx's clamp(x, a, b) does and returns it as an int.
func clampIdx(x, a, b float64) int { return int(pixelfx.ClampTo(x, a, b)) }

// floor0 is the floor every ice effect stands on: y 0 everywhere.
func floor0(float64) float64 { return 0 }

// iceSparks is the head-to-tail ramp of a spark in an ice palette.
func iceSparks(C []uint32) []uint32 { return []uint32{C[7], C[6], C[5], C[4]} }

// newSim returns the particle system, debris and lights an ice effect runs, drawing from R over a floor at y 0.
func newSim(R *pixelfx.Rng, C []uint32) (*pixelfx.Particles, *pixelfx.Bits, *pixelfx.Lights) {
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor0, Sparks: iceSparks(C), Haze: C[5]})
	return ps, pixelfx.NewBits(floor0), &pixelfx.Lights{}
}

// inPoly reports whether (x, y) is inside the polygon pts (even-odd rule).
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

// polyCenter is a polygon's centroid and area.
type polyCenter struct{ x, y, area float64 }

// centroid returns a polygon's centroid and area; a polygon with no area returns the mean of its corners.
func centroid(pts [][2]float64) polyCenter {
	a, cx, cy := 0.0, 0.0, 0.0
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		x0, y0, x1, y1 := pts[j][0], pts[j][1], pts[i][0], pts[i][1]
		k := x0*y1 - x1*y0
		a += k
		cx += (x0 + x1) * k
		cy += (y0 + y1) * k
	}
	if math.Abs(a) < 1e-6 {
		n := float64(len(pts))
		sx, sy := 0.0, 0.0
		for _, q := range pts {
			sx += q[0]
		}
		for _, q := range pts {
			sy += q[1]
		}
		return polyCenter{x: sx / n, y: sy / n}
	}
	return polyCenter{x: cx / (3 * a), y: cy / (3 * a), area: math.Abs(a) / 2}
}

// splitPoly cuts a polygon along the line through (x, y) at angle a and returns the pieces with three corners or more.
func splitPoly(pts [][2]float64, x, y, a float64) [][][2]float64 {
	nx, ny := -math.Sin(a), math.Cos(a)
	side := func(q [2]float64) float64 { return (q[0]-x)*nx + (q[1]-y)*ny }
	var L, Rt [][2]float64
	for i := range pts {
		p, q := pts[i], pts[(i+1)%len(pts)]
		sp, sq := side(p), side(q)
		if sp >= 0 {
			L = append(L, p)
		} else {
			Rt = append(Rt, p)
		}
		if (sp >= 0) != (sq >= 0) {
			u := sp / (sp - sq)
			m := [2]float64{p[0] + (q[0]-p[0])*u, p[1] + (q[1]-p[1])*u}
			L = append(L, m)
			Rt = append(Rt, m)
		}
	}
	var out [][][2]float64
	for _, s := range [2][][2]float64{L, Rt} {
		if len(s) >= 3 {
			out = append(out, s)
		}
	}
	return out
}

// splitAll cuts every polygon of polys with cut and gathers the pieces in order, as JavaScript's flatMap does.
func splitAll(polys [][][2]float64, cut func(poly [][2]float64) [][][2]float64) [][][2]float64 {
	var out [][][2]float64
	for _, poly := range polys {
		out = append(out, cut(poly)...)
	}
	return out
}

// iceIndex returns the step of the ice ramp C (dark to bright) a color turns to: its brightness picks it, never the darkest.
func iceIndex(c uint32, C []uint32) int {
	n, l := float64(len(C)), pixelfx.Luma(c)
	k := 2 + l*5.2
	if l < 0.12 {
		k = 2
	}
	return clampIdx(pixelfx.Round((k*(n-1))/7), 1, n-1)
}

// glassPoly fills a polygon with translucent color (true blending, no dither): glass, ice, light.
func glassPoly(p *pixelfx.Painter, pts [][2]float64, c uint32, a float64) {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range pts {
		x0, x1, y0, y1 = math.Min(x0, q[0]), math.Max(x1, q[0]), math.Min(y0, q[1]), math.Max(y1, q[1])
	}
	for y := math.Floor(y0); y <= math.Ceil(y1); y++ {
		for x := math.Floor(x0); x <= math.Ceil(x1); x++ {
			if inPoly(pts, x+0.5, y+0.5) {
				p.Blend(x, y, c, a)
			}
		}
	}
}

// pane is a flat polygon of ice: pts are its corners around (0, 0), x and y where it is, a its turn, f its tumble end over end, lit how much light its face catches (0..1), g its gravity scale, brittle how many hard landings break it (0 never), crumble how long it lies still before it crumbles (unset never), solid whether it is opaque (one tone, or shaded like a blade split along ridge, an angle) rather than glass.
type pane struct {
	pts                        [][2]float64
	x, y, vx, vy, a, va, f, vf float64
	lit, g, drag, bounce       float64
	life, max                  float64
	brittle, hits              int
	crumble                    pixelfx.Opt
	restT                      float64
	rest, solid                bool
	tone, ridge                pixelfx.Opt
}

// panes are panes of ice that fly, spin and tumble end over end (they squash as they turn edge-on and flash as they square up to the light), bounce on the floor, skid, settle and fade. onLand is called when one hits the floor hard, onCrumble when one that lay still crumbles away, and onSkid while one slides along the floor.
type panes struct {
	list      []*pane
	ground    func(x float64) float64
	onLand    func(q *pane, x, y, speed float64)
	onCrumble func(q *pane)
	onSkid    func(q *pane, x, y float64)
}

// add adds q with its max set to its life. Unlike pixelfx's add it fills in no defaults (lit 0.6, g 1, drag 0.3, bounce 0.35): each effect sets every field it relies on.
func (ps *panes) add(q pane) {
	q.max = q.life
	ps.list = append(ps.list, &q)
}

// corners returns the pane's corners as drawn: tumbled (squashed across its own width), turned and placed.
func (ps *panes) corners(q *pane) [][2]float64 {
	ca, sa, k := math.Cos(q.a), math.Sin(q.a), math.Cos(q.f)
	out := make([][2]float64, len(q.pts))
	for i, c := range q.pts {
		x, yy := c[0], c[1]*k
		out[i] = [2]float64{q.x + x*ca - yy*sa, q.y + x*sa + yy*ca}
	}
	return out
}

func (ps *panes) seat(q *pane) float64 {
	low := math.Inf(-1)
	for _, c := range ps.corners(q) {
		low = math.Max(low, c[1])
	}
	return low
}

func (ps *panes) remove(i int) { ps.list = append(ps.list[:i], ps.list[i+1:]...) }

func (ps *panes) update(dt float64) {
	for i := len(ps.list) - 1; i >= 0; i-- {
		q := ps.list[i]
		if q.life -= dt; q.life <= 0 {
			ps.remove(i)
			continue
		}
		floor := ps.ground(q.x) - 0.5
		if q.rest && q.crumble.Set {
			if q.restT += dt; q.restT > q.crumble.V {
				if ps.onCrumble != nil {
					ps.onCrumble(q)
				}
				ps.remove(i)
				continue
			}
		}
		if q.rest {
			// skidding: friction slows the slide and the spin, the pane stays seated on the floor
			if math.Abs(q.vx) > 8 && ps.onSkid != nil {
				ps.onSkid(q, q.x, floor+0.5)
			}
			q.x += q.vx * dt
			q.vx -= q.vx * 7 * dt
			q.a += q.va * dt
			q.va -= q.va * 9 * dt
			q.vf -= q.vf * 9 * dt
			q.f += q.vf * dt
			q.y -= ps.seat(q) - floor
			continue
		}
		q.vy += pixelfx.Gravity * q.g * dt
		q.vx -= q.vx * q.drag * dt
		q.vy -= q.vy * q.drag * dt
		q.x += q.vx * dt
		q.y += q.vy * dt
		q.a += q.va * dt
		q.f += q.vf * dt
		low := ps.seat(q)
		if q.g <= 0 || low < floor || q.vy <= 0 {
			continue
		}
		q.y -= low - floor
		if q.vy > 70 {
			q.hits++
			if ps.onLand != nil {
				ps.onLand(q, q.x, floor+0.5, q.vy)
			}
		}
		if q.brittle > 0 && q.vy > 70 && q.hits >= q.brittle {
			ps.remove(i)
			continue
		}
		if q.vy > 70 && q.bounce > 0 {
			q.vy = -q.vy * q.bounce
			q.vx *= 0.75
			q.va *= 0.6
			q.vf *= 0.5
		} else {
			q.rest = true
			q.vy = 0
		}
	}
}

// draw draws the panes as glass in the ice ramp C (dark to bright): a translucent face, edges lit where they face the light, a glint.
func (ps *panes) draw(p *pixelfx.Painter, C []uint32) {
	n := len(C)
	fn := float64(n)
	for _, q := range ps.list {
		a, pts, k := math.Min(1, q.life/0.35), ps.corners(q), math.Cos(q.f)
		face := math.Abs(k)
		// the face: glassy, brighter the more it faces us and the more light it catches; it flashes as it squares up
		lv, flash := q.lit*(0.35+0.65*face), face > 0.97 && !q.rest
		if face > 0.25 {
			switch {
			case q.solid && q.tone.Set:
				// a chunk of one face: its tone, brighter as it squares up to the light
				p.Poly(pts, pick(flash, C[n-1], C[clampIdx(pixelfx.Round(1+q.tone.V*5*(0.6+0.4*face)), 1, fn-2)]), a)
			case q.solid:
				// shaded like the blade it broke from: a lit side, a pale ridge, a deep side
				rr := q.ridge.Or(-pi / 2)
				cr, sr, ca, sa := math.Cos(rr), math.Sin(rr), math.Cos(q.a), math.Sin(q.a)
				rx, ry := cr*ca-sr*k*sa, cr*sa+sr*k*ca
				nx, ny := -ry, rx
				if nx*-0.55+ny*-0.84 < 0 {
					nx, ny = -nx, -ny
				}
				lit, deep := pick(flash, C[n-1], C[clampIdx(pixelfx.Round(3+lv*3), 3, fn-2)]), C[clampIdx(pixelfx.Round(1+lv*2), 1, 3)]
				p.PolyFn(pts, func(x, y float64) (uint32, float64) {
					d := (x-q.x)*nx + (y-q.y)*ny
					if d > 0.6 {
						return lit, a
					}
					if d > -0.6 {
						return C[n-2], a
					}
					return deep, a
				})
			default:
				glassPoly(p, pts, pick(flash, C[n-1], C[clampIdx(pixelfx.Round(3+lv*3), 3, fn-2)]), (0.3+0.35*lv+pick(flash, 0.3, 0))*a)
			}
		}
		cx, cy, top := 0.0, 0.0, 0
		for _, c := range pts {
			cx += c[0]
			cy += c[1]
		}
		cx /= float64(len(pts))
		cy /= float64(len(pts))
		// edges: lit where they face the light (up and left), cool where they face away
		for i := range pts {
			x0, y0, x1, y1 := pts[i][0], pts[i][1], pts[(i+1)%len(pts)][0], pts[(i+1)%len(pts)][1]
			nx, ny := y1-y0, x0-x1
			if nx*((x0+x1)/2-cx)+ny*((y0+y1)/2-cy) < 0 {
				nx, ny = -nx, -ny
			}
			l := pixelfx.Hypot(nx, ny)
			if l == 0 {
				l = 1
			}
			lit := (-nx*0.55 - ny*0.84) / l
			var c uint32
			switch {
			case face < 0.25 || lit > 0.35:
				c = C[n-1]
			case lit > -0.25:
				c = C[n-2]
			case q.solid:
				c = C[1]
			default:
				c = C[max(2, n-4)]
			}
			p.Line(x0, y0, x1, y1, c, a)
			if pts[i][1] < pts[top][1] {
				top = i
			}
		}
		if face > 0.7 {
			p.Dot(pts[top][0], pts[top][1], pixelfx.White, a)
		}
		p.Glow(cx, cy, 5, C[n-3], pick(flash, 0.5, 0.14)*a)
	}
}

// bladeOpts are how blade draws: a its alpha (0 is read as 1), white flashes it white, glint (0..1) puts a highlight across it at that height (below 0 none), jag (0..1) is where the notch starts (below 0 none), dim draws it that many steps darker, for back rows.
type bladeOpts struct {
	a          float64
	white      bool
	glint, jag float64
	dim        float64
}

// noBlade is blade's options when an effect gives none: no glint, no notch.
var noBlade = bladeOpts{glint: -1, jag: -1}

// blade draws a jagged ice blade standing on its base (bx, by), tip at (tx, ty), w wide at the base, shaded in an ice ramp C (dark to bright, 8 steps): a lit facet on the left with a pale edge, a bright ridge, a deep facet on the right, a white tip, a dark rim, and a notch cut into one side so it reads as a shard, not a cone.
func blade(p *pixelfx.Painter, bx, by, tx, ty, w float64, C []uint32, o bladeOpts) {
	dx, dy := tx-bx, ty-by
	L := pixelfx.Hypot(dx, dy)
	if L < 1.5 || w < 0.5 {
		return
	}
	ux, uy := dx/L, dy/L
	nx, ny, a, dim, jag, glint := -uy, ux, o.a, o.dim, o.jag, o.glint
	if a == 0 {
		a = 1
	}
	K := func(i float64) uint32 { return C[clampIdx(i-dim, 0, float64(len(C)-1))] }
	hw := w / 2
	// half widths on the left (lit) and right (shadow) sides at height u; the notch steps the right side in
	left := func(u float64) float64 { return hw*(1-u) + 0.35 }
	right := func(u float64) float64 { return hw*(1-u)*pick(jag >= 0 && u > jag, 0.55, 1) + 0.35 }
	ridge := func(u float64) float64 { return -w * 0.12 * (1 - u) }
	pad := hw + 2
	p.Shade(math.Min(bx, tx)-pad, math.Min(by, ty)-pad, math.Max(bx, tx)+pad, math.Max(by, ty), func(x, y float64, out *pixelfx.Out) uint32 {
		rx, ry := x-bx, y-by
		u, s := (rx*ux+ry*uy)/L, rx*nx+ry*ny
		if u < 0 {
			return 0
		}
		l, r := left(math.Min(u, 1)), right(math.Min(u, 1))
		if u > 1 || s < -l || s > r {
			// a dark rim just outside the blade
			over := 0.0
			if u > 1 {
				over = (u - 1) * L
			}
			outBy := math.Max(over, pick(s < -l, -l-s, s-r))
			if outBy < 1 && u < 1+1/L {
				out.C = K(0)
				out.A = a
			}
			return 0
		}
		out.A = a
		if o.white {
			out.C = pixelfx.White
			return out.C
		}
		rd := ridge(u)
		var c uint32
		switch {
		case u > 0.88:
			c = pixelfx.White
		case math.Abs(s-rd) < 0.55:
			c = K(6)
		case s < rd:
			if s+l < 1 {
				c = K(6)
			} else if (s+l)/math.Max(1, rd+l) > 0.55 {
				c = K(5)
			} else {
				c = K(4)
			}
		default:
			if r-s < 1.1 {
				c = K(1)
			} else if (s-rd)/math.Max(1, r-rd) > 0.5 {
				c = K(2)
			} else {
				c = K(3)
			}
		}
		// the notch's ledge catches light; a little glitter in the lit facet
		if jag >= 0 && math.Abs(u-jag)*L < 0.8 && s > rd {
			c = K(6)
		} else if s < rd && c != K(6) && (pixelfx.ToInt32(math.Floor(x)*73856093)^pixelfx.ToInt32(math.Floor(y)*19349663))%997 < 30 {
			c = K(7)
		}
		if glint >= 0 && s <= rd+0.6 {
			g := math.Abs(u-glint) * L
			if g < 0.8 {
				c = pixelfx.White
			} else if g < 1.8 {
				c = K(6)
			}
		}
		out.C = c
		return 0
	})
}

// flake is a snowflake design: pairs of side branches along each arm ({where 0..1, length 0..1 of the arm}), maybe a hexagonal plate round the heart, maybe forked tips.
type flake struct {
	branches    [][2]float64
	plate, fork bool
}

// flakeDesign draws a snowflake design from R.
func flakeDesign(R *pixelfx.Rng) flake {
	n := R.Int(1, 3)
	var d flake
	for i := 0.0; i < n; i++ {
		where := 0.38 + (0.4*(i+R.Range(0.2, 0.8)))/n
		d.branches = append(d.branches, [2]float64{where, R.Range(0.24, 0.36) * (1 - i*0.15)})
	}
	d.plate = R.Chance(0.45)
	d.fork = R.Chance(0.45)
	return d
}

// flakeOpts are how snowflake draws: a its alpha (0 is read as 1) and hot (0..1), how white it twinkles.
type flakeOpts struct{ a, hot float64 }

// snowflake draws a six-armed snowflake of radius r turned by rot, crisp 1 px lines: pale arms, side branches at 60 degrees, a plate or forked tips by design, a white heart, light.
func snowflake(p *pixelfx.Painter, x, y, r, rot float64, d flake, C []uint32, o flakeOpts) {
	n := len(C)
	a, hot := o.a, o.hot
	if a == 0 {
		a = 1
	}
	W := pixelfx.White
	rot = pixelfx.Round(rot/(pi/18)) * (pi / 18) // turn in 10 degree ticks, like sprite frames
	if r < 1.2 {
		p.Dot(x, y, W, a)
		return
	}
	if r < 2.6 {
		// too small for branches: a six-point star that flips between two poses as it turns
		pose := int(math.Floor((math.Mod(rot, pi/3)+pi/3)/(pi/6))) % 2
		pts := [4][2]float64{{1, 1}, {-1, -1}, {1, -1}, {-1, 1}}
		if pose != 0 {
			pts = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
		}
		for _, q := range pts {
			p.Dot(x+q[0], y+q[1], pick(hot > 0.5, W, C[n-3]), a)
		}
		p.Dot(x, y, W, a)
		p.Glow(x, y, 4, C[n-3], 0.2*a)
		return
	}
	// detail by size: a bare star when small, one pair of branches when middling, the full design when big
	arm, br, big := pick(hot > 0.5, W, C[n-2]), pick(hot > 0.5, C[n-2], C[n-3]), r >= 7
	var branches [][2]float64
	switch {
	case r < 4:
	case big:
		branches = d.branches[:min(2, len(d.branches))]
	default:
		branches = [][2]float64{{0.58, 0.36}}
	}
	for k := range 6 {
		g := rot + (float64(k)*tau)/6
		ca, sa := math.Cos(g), math.Sin(g)
		p.Line(x, y, x+ca*r, y+sa*r, arm, a)
		for _, b := range branches {
			u, l := b[0], b[1]
			bx, by, bl := x+ca*r*u, y+sa*r*u, math.Max(1, r*l)
			for _, s := range [2]float64{-1, 1} {
				p.Line(bx, by, bx+math.Cos(g+(s*pi)/3)*bl, by+math.Sin(g+(s*pi)/3)*bl, br, a)
			}
		}
		if d.fork && big && !d.plate {
			for _, s := range [2]float64{-1, 1} {
				p.Dot(x+ca*r+math.Cos(g+s*0.9)*1.2, y+sa*r+math.Sin(g+s*0.9)*1.2, br, a)
			}
		}
		p.Dot(x+ca*r, y+sa*r, W, a)
	}
	if d.plate && big {
		q := r * 0.34
		for k := range 6 {
			g0 := rot + pi/6 + (float64(k)*tau)/6
			g1 := g0 + tau/6
			p.Line(x+math.Cos(g0)*q, y+math.Sin(g0)*q, x+math.Cos(g1)*q, y+math.Sin(g1)*q, C[n-3], a)
		}
	}
	p.Px(x, y, W)
	p.Glow(x, y, 3+r*1.2, C[n-4], (0.14+0.35*hot)*a)
	if hot > 0.3 {
		p.Glow(x, y, 2+r*0.5, W, 0.3*hot*a)
	}
}

// endBy trims the life of every particle, bit and pane that would outlive an effect of duration D at time t, so nothing does; any of them may be nil.
func endBy(t, D float64, ps *pixelfx.Particles, bits *pixelfx.Bits, pn *panes) {
	left := math.Max(0.001, D-0.05-t)
	if ps != nil {
		for i := range ps.List {
			if ps.List[i].Life > left {
				ps.List[i].Life = left
			}
		}
	}
	if bits != nil {
		for i := range bits.List {
			if bits.List[i].Life > left {
				bits.List[i].Life = left
			}
		}
	}
	if pn != nil {
		for _, q := range pn.list {
			if q.life > left {
				q.life = left
			}
		}
	}
}

// crackPt is a point of a crack: where it is and its distance from the crack's start along it.
type crackPt struct{ x, y, d float64 }

// fractureOpts shape the cracks fractures walks: step the range of each step's length ([1.6, 2.6] when zero), len the most steps (18 when 0), fork the chance of forking at each step (0.16 when unset), a0 the first crack's angle (drawn from the generator when unset) and spread the angle the cracks fan over (a full turn when unset).
type fractureOpts struct {
	step             [2]float64
	len              int
	fork, a0, spread pixelfx.Opt
}

// fractures walks n cracks out from (x, y) through a shape: jagged lines that wander, fork now and then and stop at the shape's edge (inside says what's in it). Each point carries its distance from the start along the crack, so drawFractures can grow them at a speed.
func fractures(R *pixelfx.Rng, inside func(x, y float64) bool, x, y float64, n int, o fractureOpts) [][]crackPt {
	var out [][]crackPt
	step := o.step
	if step == [2]float64{} {
		step = [2]float64{1.6, 2.6}
	}
	length := o.len
	if length == 0 {
		length = 18
	}
	fork := o.fork.Or(0.16)
	var a0 float64
	if o.a0.Set {
		a0 = o.a0.V
	} else {
		a0 = R.Range(0, tau)
	}
	spread := o.spread.Or(tau)
	var walk func(x, y, d, a float64, length float64, depth int)
	walk = func(x, y, d, a float64, length float64, depth int) {
		pts := []crackPt{{x, y, d}}
		for s := 0.0; s < length; s++ {
			a += R.Range(-0.55, 0.55)
			st := R.Range(step[0], step[1])
			nx, ny := x+math.Cos(a)*st, y+math.Sin(a)*st
			if !inside(nx, ny) {
				break
			}
			x, y, d = nx, ny, d+st
			pts = append(pts, crackPt{x, y, d})
			if depth < 2 && len(pts) > 2 && R.Chance(fork) {
				walk(x, y, d, a+R.Sign()*R.Range(0.6, 1.2), math.Ceil(length*0.5), depth+1)
			}
		}
		if len(pts) > 1 {
			out = append(out, pts)
		}
	}
	fn := float64(n)
	for i := 0.0; i < fn; i++ {
		walk(x, y, 0, a0+(i/fn)*spread+R.Range(-0.3, 0.3)*math.Min(1, spread/tau), float64(length), 0)
	}
	return out
}

// crackOpts are how drawFractures draws: ox and oy shift the cracks, cool leaves off the white-hot running tips, and a is their alpha (0 is read as 1).
type crackOpts struct {
	ox, oy, a float64
	cool      bool
}

// drawFractures draws cracks grown out to distance reach from their start: a bright line over a dark one a pixel lower (an engraved fracture), a white-hot tip while they run, light leaking out along them.
func drawFractures(p *pixelfx.Painter, cracks [][]crackPt, reach float64, C []uint32, o crackOpts) {
	n := len(C)
	ox, oy, hot, a := o.ox, o.oy, !o.cool, o.a
	if a == 0 {
		a = 1
	}
	type segment struct {
		x0, y0, x1, y1 float64
		tip            bool
	}
	seg := func(pts []crackPt, i int) (segment, bool) {
		q0, q1 := pts[i-1], pts[i]
		if q0.d >= reach {
			return segment{}, false
		}
		u := math.Min(1, (reach-q0.d)/math.Max(0.01, q1.d-q0.d))
		return segment{q0.x + ox, q0.y + oy, q0.x + ox + (q1.x-q0.x)*u, q0.y + oy + (q1.y-q0.y)*u, reach-q1.d < 3}, true
	}
	for _, pts := range cracks {
		for i := 1; i < len(pts); i++ {
			s, ok := seg(pts, i)
			if !ok {
				break
			}
			p.Line(s.x0, s.y0+1, s.x1, s.y1+1, C[min(2, n-1)], a)
		}
	}
	for _, pts := range cracks {
		for i := 1; i < len(pts); i++ {
			s, ok := seg(pts, i)
			if !ok {
				break
			}
			tip := hot && s.tip
			p.Line(s.x0, s.y0, s.x1, s.y1, pick(tip, pixelfx.White, C[n-2]), a)
			if tip {
				p.Glow(s.x1, s.y1, 5, C[n-3], 0.5*a)
			} else if i%2 != 0 {
				p.Glow(s.x1, s.y1, 3, C[n-3], 0.18*a)
			}
		}
	}
}
