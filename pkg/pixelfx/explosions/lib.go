// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The fire balls, rocks and scorch are adapted from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// pi and tau are variables so expressions with them round at each step, as JavaScript's do, instead of being folded exactly at compile time.
var pi, tau = math.Pi, pixelfx.Tau

// fireRamp returns a 9-step fire ramp, white-hot down to smouldering (the shape of pixelfx.FireRamp), from an 8-step palette (dark to bright).
func fireRamp(F []uint32) []uint32 {
	return []uint32{F[7], F[6], F[5], F[4], F[3], F[2], F[1], pixelfx.Mix(F[1], F[0], 0.5), F[0]}
}

// heatRamp returns the heat for a scorch, from a fire ramp (bright to dark) down to cinders.
func heatRamp(FR []uint32) []uint32 {
	out := slices.Clone(FR[:8])
	return append(out, pixelfx.Mix(FR[8], soot, 0.3), pixelfx.Mix(FR[8], soot, 0.65))
}

// fireBall draws one Fire particle in the fire ramp FR: a darker rim, a lit core, a tongue on top, a hot heart.
func fireBall(p *pixelfx.Painter, q *pixelfx.Particle, FR []uint32) {
	k := q.Life / q.Max
	age, n, x, y := 1-k, float64(len(FR)), q.X, q.Y
	heat := math.Min(0.999, math.Pow(age, 0.62)+math.Mod(q.Seed, 1)*0.1)
	idx := int(math.Min(n-2, math.Floor(heat*(n-1))))
	r, c := q.Size*(0.55+0.45*math.Min(1, age*7))*(1-age*0.55), FR[idx]
	if float64(idx) >= n-2 {
		p.Disc(x, y, r, c, math.Min(1, k*3)*0.85)
		return
	}
	// the hottest puffs get a rim a step darker still, so a mass of them reads as separate balls
	p.Circle(x, y, r, FR[min(len(FR)-1, idx+pick(idx <= 2, 3, 2))], 1)
	p.Circle(x-r*0.16, y-r*0.22, r*0.78, c, 1)
	if r > 1.6 {
		p.Circle(x, y-r*0.9, r*0.42, c, 1)
	}
	if idx <= 3 && r > 1.8 {
		p.Circle(x-r*0.28, y-r*0.34, r*0.36, FR[max(0, idx-1)], 1)
	}
	if idx < 6 {
		p.Glow(x, y, r*2.4+2, c, 0.18*k)
	}
}

// drawPs draws a particle system's layer with its Fire puffs in the fire ramp FR, so a palette recolors them.
func drawPs(p *pixelfx.Painter, ps *pixelfx.Particles, layer pixelfx.Layer, FR []uint32) {
	all := ps.List
	var fires []int
	rest := make([]pixelfx.Particle, 0, len(all))
	for i := range all {
		if all[i].K == pixelfx.Fire {
			fires = append(fires, i)
		} else {
			rest = append(rest, all[i])
		}
	}
	back := layer == pixelfx.LayerBack
	if !back {
		for _, i := range fires {
			if q := &all[i]; q.Delay <= 0 && q.Flag&pixelfx.Back == 0 {
				fireBall(p, q, FR)
			}
		}
	}
	ps.List = rest
	ps.Draw(p, layer)
	ps.List = all
	if back {
		for _, i := range fires {
			if q := &all[i]; q.Delay <= 0 && q.Flag&pixelfx.Back != 0 {
				fireBall(p, q, FR)
			}
		}
	}
}

func lifts(k pixelfx.Kind) bool {
	return k == pixelfx.Smoke || k == pixelfx.Dust || k == pixelfx.Fire || k == pixelfx.Ember || k == pixelfx.Vapour
}

func softKind(k pixelfx.Kind) bool {
	return k == pixelfx.Smoke || k == pixelfx.Dust || k == pixelfx.Vapour
}

// draftOpts shape an updraft column: its axis X, the Top above which nothing is lifted, its half-width W, the lift Up (px/s²) and Squeeze toward the axis, and near the floor (within Low px) the Pull that draws particles in from as far as Reach; K scales it all.
type draftOpts struct {
	X, Top, W, Up, Squeeze, Pull, Low, Reach, K float64
}

// draft is an updraft column: smoke, dust, fire and embers below the top and within reach of the axis are carried up and squeezed toward it, and near the floor drawn in from the sides, the afterwinds that feed a blast's column.
func draft(ps *pixelfx.Particles, dt float64, o draftOpts) {
	if o.K <= 0 {
		return
	}
	for i := range ps.List {
		q := &ps.List[i]
		if q.Delay > 0 || !lifts(q.K) || q.Y < o.Top {
			continue
		}
		dx := q.X - o.X
		ax := math.Abs(dx)
		u := ax / o.W
		if u < 1 {
			f := (1 - u*u) * o.K
			q.VY -= o.Up * f * dt
			q.VX -= dx * o.Squeeze * f * dt
		}
		if o.Pull != 0 && q.Y > -o.Low && ax > o.W*0.4 && ax < o.Reach {
			q.VX -= pixelfx.Sign(dx) * o.Pull * o.K * (1 - ax/o.Reach) * dt
		}
	}
}

// cull drops the particles that leave the box (local px), so nothing is drawn past the canvas.
func cull(ps *pixelfx.Particles, x0, y0, x1, y1 float64) {
	ps.List = slices.DeleteFunc(ps.List, func(q pixelfx.Particle) bool {
		return q.X < x0 || q.X > x1 || q.Y < y0 || q.Y > y1
	})
}

// fadeEdges fades soft particles (smoke, dust, vapour) out over w px as they near the sides x0 and x1 and drops them before they reach them; y1, a bottom edge, gets a thin margin of 6 px.
func fadeEdges(ps *pixelfx.Particles, x0, x1, w, y1 float64) {
	L := ps.List[:0]
	for _, q := range ps.List {
		if softKind(q.K) {
			r := q.Size * 1.65
			e := math.Min((math.Min(q.X-x0, x1-q.X)-r)/w, (y1-q.Y-r)/6)
			if e <= 0 {
				continue
			}
			if e < 1 {
				q.Life = math.Min(q.Life, q.Max*0.42*e)
			}
		}
		L = append(L, q)
	}
	ps.List = L
}

// pool is a soft pool of light, an ellipse rx by ry at (x, y): light falling on the floor (keep ry small, it is perspective).
func pool(p *pixelfx.Painter, x, y, rx, ry float64, c uint32, k float64) {
	if k <= 0.004 || rx < 1 || ry < 0.5 {
		return
	}
	p.Shade(x-rx, y-ry, x+rx, y+ry, func(px, py float64, out *pixelfx.Out) uint32 {
		dx, dy := (px-x)/rx, (py-y)/ry
		d := dx*dx + dy*dy
		if d >= 1 {
			return 0
		}
		f := 1 - math.Sqrt(d)
		out.L = c
		out.K = f * f * k
		return 0
	})
}

// haze is a translucent ellipse (blended, never dithered) whose color c and alpha a are functions of d (0 centre .. 1 rim).
func haze(p *pixelfx.Painter, x, y, rx, ry float64, c func(d float64) uint32, a func(d float64) float64) {
	if rx < 0.5 || ry < 0.5 {
		return
	}
	p.Each(x-rx, y-ry, x+rx, y+ry, func(lx, ly float64, sx, sy int) {
		dx, dy := (lx-x)/rx, (ly-y)/ry
		d2 := dx*dx + dy*dy
		if d2 > 1 {
			return
		}
		d := math.Sqrt(d2)
		p.BlendPx(sx, sy, c(d), a(d))
	})
}

// scorch draws the floor under a blast: a burn that spreads out in a blink, glows white-hot and cools through the heat ramp HR to soot over cool seconds, while a few cracks hold their glow twice as long. rows is how deep it is, fade 0..1.
func scorch(p *pixelfx.Painter, HR []uint32, t, rx0, cool, fade, rows float64) {
	if fade <= 0 {
		return
	}
	rx, ry, n := rx0*math.Min(1, t/0.08), rows+1.5, len(HR)
	for y := 0.0; y < rows; y++ {
		for x := math.Floor(-rx); x <= math.Ceil(rx); x++ {
			dx, dy := (x+0.5)/rx, (y+0.5)/ry
			d := math.Sqrt(dx*dx + dy*dy)
			if d > 1 {
				continue
			}
			k := pixelfx.Rand(x*31+y, 3)
			T := math.Pow(pixelfx.Clamp(1-t/cool), 1.4)*(1.25-d) - k*0.35
			crack := 0.0
			if k > 0.88 && d < 0.85 {
				crack = math.Pow(pixelfx.Clamp(1-t/(cool*2)), 1.2) * (1.15 - d) * 0.8
			}
			if H := math.Max(T, crack); H > 0.06 {
				c := HR[min(n-1, int(math.Floor((1-math.Min(1, H))*float64(n-1))))]
				p.Dot(x, y, c, fade)
				if pixelfx.ToInt32(x)&3 == 0 || crack > T {
					p.Glow(x+0.5, y, 6, c, 0.18*H*fade)
				}
				continue
			}
			c, a := soot2, 1-d
			if d < 0.5 {
				c = soot
			}
			if d < 0.95-k*0.3 {
				a = 1
			}
			p.Dot(x, y, c, a*fade)
		}
	}
}

// heatOf returns a rock's heat: 0..1, kept in its Data (none when unset).
func heatOf(b *pixelfx.Bit) float64 {
	h, _ := b.Data.(float64)
	return h
}

// rock draws a rock (a Chunk): a lit side, a shaded side and an outline, turning as it spins. Its heat (0..1, see heatOf) makes it molten: its heart and shaded side glow through the fire ramp FR and it gives off light, cooling to stone.
func rock(p *pixelfx.Painter, b *pixelfx.Bit, st stoneCols, FR []uint32) {
	r, fade := 0.6+b.S*0.75, math.Min(1, b.Life/0.5)
	lx, ly, heat := math.Cos(b.A)*r*0.4, math.Sin(b.A)*r*0.4, heatOf(b)
	p.Shade(b.X-r-1, b.Y-r-1, b.X+r+1, b.Y+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		d := pixelfx.Hypot(dx*1.15, dy)
		if d > r+0.9 {
			return 0
		}
		out.A = fade
		if d > r {
			return outline
		}
		lit := dx*lx + dy*ly
		if heat > 0.08 {
			g := heat*(1.1-d/(r+0.5)) + pick(lit > 0.3, heat*0.45, 0)
			switch {
			case g > 1:
				return FR[1]
			case g > 0.8:
				return FR[2]
			case g > 0.62:
				return FR[3]
			case g > 0.5:
				return FR[4]
			case g > 0.32:
				return FR[5]
			}
		}
		switch {
		case lit < -0.4:
			return st.light
		case lit > 0.6:
			return st.shade
		}
		return st.body
	})
	if heat > 0.05 {
		p.Glow(b.X, b.Y, r*2+3, FR[3], 0.32*heat*fade)
	}
}

// noBox is glowDome's box when nothing bounds it.
var noBox = [4]float64{math.Inf(-1), math.Inf(-1), math.Inf(1), math.Inf(1)}

// glowDome is light hugging the upper half of a circle of radius R at (x, y) (a dome's shock), falling off over width w. box [x0, y0, x1, y1] (local) keeps it inside a canvas.
func glowDome(p *pixelfx.Painter, x, y, R, w float64, c uint32, k float64, box [4]float64) {
	if k <= 0.004 || w < 1 {
		return
	}
	p.Shade(math.Max(box[0], x-R-w), math.Max(box[1], y-R-w), math.Min(box[2], x+R+w), math.Min(box[3], y+0.5), func(px, py float64, out *pixelfx.Out) uint32 {
		d := math.Abs(pixelfx.Hypot(px-x, py-y)-R) / w
		if d >= 1 {
			return 0
		}
		out.L = c
		out.K = (1 - d) * (1 - d) * k
		return 0
	})
}

// trail draws a burning thing's trail: pts are x, y, t triples (oldest first) and t the time now, drawn as a ribbon that is fire near the head (fire ramp FR) and swells into thin smoke behind (smoke ramp SM, light to dark) over life seconds.
func trail(p *pixelfx.Painter, pts []float64, t float64, FR, SM []uint32, life, w, heat float64) {
	lx, ly := math.NaN(), math.NaN()
	for i := len(pts)/3 - 1; i >= 0; i-- {
		qx, qy, qt := pts[i*3], pts[i*3+1], pts[i*3+2]
		age := (t - qt) / life
		if age >= 1 {
			break
		}
		if pixelfx.Hypot(qx-lx, qy-ly) < 1.6 {
			continue
		}
		lx, ly = qx, qy
		r := w * (0.7 + age*2.6)
		if age < 0.12*heat {
			p.Circle(qx, qy-age*6, r*0.8, FR[pick(age < 0.05, 3, 5)], 1)
		} else {
			p.Disc(qx, qy-age*6, r, SM[min(len(SM)-1, 1+int(math.Floor(age*3)))], 0.55*(1-age))
		}
	}
}

// drawDirt draws a particle system's Grit as dirt in flight: fast bits stretch into short streaks (a darker tail, the bit at the head), slow ones are the usual 1 or 2 px chips. The rest of the system draws as usual.
func drawDirt(p *pixelfx.Painter, ps *pixelfx.Particles, layer pixelfx.Layer) {
	all := ps.List
	var grit []int
	rest := make([]pixelfx.Particle, 0, len(all))
	for i := range all {
		if all[i].K == pixelfx.Grit {
			grit = append(grit, i)
		} else {
			rest = append(rest, all[i])
		}
	}
	ps.List = rest
	ps.Draw(p, layer)
	ps.List = all
	if layer == pixelfx.LayerBack {
		return
	}
	for _, i := range grit {
		q := &all[i]
		if q.Delay > 0 {
			continue
		}
		k, sp := q.Life/q.Max, pixelfx.Hypot(q.VX, q.VY)
		if k < 0.25 && k*4 < 0.5 {
			continue
		}
		if sp > 140 {
			L, ux, uy, col := math.Min(5, sp*0.012), q.VX/sp, q.VY/sp, q.Col
			p.LineFn(q.X-ux*L, q.Y-uy*L, q.X, q.Y, func(u float64) (uint32, float64) {
				if u < 0.5 {
					return pixelfx.Scale(col, 0.7), 1
				}
				return col, 1
			})
			continue
		}
		p.Px(q.X, q.Y, q.Col)
		if q.Size >= 2 {
			p.Px(q.X+1, q.Y, q.Col)
			p.Px(q.X, q.Y+1, pixelfx.Scale(q.Col, 0.8))
			p.Px(q.X+1, q.Y+1, pixelfx.Scale(q.Col, 0.7))
		}
	}
}

// crater draws a crater in the floor at x: a dark pit (an ellipse in perspective) with its far wall lit, dirt heaped in a lip on either side, and loose earth scattered round it. E is an earth ramp (dark to bright, 5+ steps), r the half-width, k 0..1 how far it has opened and a its alpha.
func crater(p *pixelfx.Painter, E []uint32, x, r, k, a float64) {
	if k <= 0 || a <= 0 {
		return
	}
	n := len(E)
	rx, ry := r*k, math.Max(1.5, r*0.17*k)
	for py := -1.0; py <= math.Ceil(ry)+1; py++ {
		for px := math.Floor(-rx - 1); px <= math.Ceil(rx+1); px++ {
			dx, dy := (px+0.5)/rx, (py+0.5-ry*0.35)/ry
			d := math.Sqrt(dx*dx + dy*dy)
			if d > 1.08 {
				continue
			}
			// the pit: darkest at its far side under the lit rim, the near wall a step lighter
			var c uint32
			switch {
			case d > 0.92:
				c = pick(dy < 0, E[n-2], E[2])
			case dy < -0.2:
				c = E[0]
			case dy < 0.35:
				c = E[1]
			default:
				c = E[2]
			}
			p.Dot(x+px, py, c, a)
		}
	}
	// the lip: dirt heaped up either side, lit on top, and a scatter of loose earth
	for _, s := range []float64{-1, 1} {
		for i := range 9 {
			fi := float64(i)
			u := fi / 8
			h := pixelfx.Round(math.Sin(pi*u)*2.2*k + pick(pixelfx.Rand(fi, pick(s > 0, 3.0, 4)) < 0.4, 1.0, 0)*k)
			hx := x + s*(rx*0.82+u*r*0.55)
			for j := 0.0; j <= h; j++ {
				c := E[2]
				if j == h {
					c = E[n-2]
				} else if j == h-1 {
					c = E[3]
				}
				p.Dot(hx, -j, c, a)
			}
		}
	}
	for i := range 26 {
		fi := float64(i)
		s, d, y := pick(pixelfx.Rand(fi, 7) < 0.5, -1.0, 1), rx*(1.05+pixelfx.Rand(fi, 8)*1.4), pixelfx.Round(pixelfx.Rand(fi, 9)*3)-1
		if pixelfx.Rand(fi, 10) < k {
			p.Dot(x+s*d, y, pick(pixelfx.Rand(fi, 11) < 0.5, E[3], E[2]), a)
		}
	}
}

// dome draws a hemisphere of white-hot air on the floor at x, radius r: an orange rim, a hot body, a white core; k goes from 1 to 0 as it dies.
func dome(p *pixelfx.Painter, F []uint32, x, r, k float64) {
	e := 1.4 / r
	p.Shade(x-r-1.5, -r-1.5, x+r+1.5, 0.5, func(px, py float64, _ *pixelfx.Out) uint32 {
		d := pixelfx.Hypot(px-x, py) / r
		switch {
		case d > 1+e:
			return 0
		case d > 1:
			return F[2]
		case d > 0.8:
			return pick(k > 0.45, F[6], F[5])
		case d > 0.5:
			return pick(k > 0.45, pixelfx.White, F[6])
		}
		return pick(k > 0.2, pixelfx.White, F[6])
	})
}

// lobe draws a lobe of a billowing cloud, shaded like a ball. The sky lights it from the top left through the smoke ramp SM (light to dark); fire (0..1) burns in its heart in the fire ramp FR (bright to dark), shrinking toward the middle as it cools, with a darker band and a dark orange-red seam at its edge so a mass of burning lobes reads apart; glow (0..1) lights its underside from below in fire colors. a is its opacity (smoke edges stay soft), z -1 far .. 1 near (the far side sits a step darker), and dark shifts the smoke down the ramp. style 1 cel-shades the smoke: a lit crescent on top, a mid tone, a dark underside the fire tints while it is hot.
func lobe(p *pixelfx.Painter, x, y, r float64, FR, SM []uint32, fire, glow, a, z float64, dark, style int) {
	if r < 0.7 || a <= 0.01 {
		return
	}
	n, m := len(FR), len(SM)
	back := pick(z < -0.35, 1, 0)
	rim, seam := 1-math.Min(0.35, 1.6/r), 1-math.Min(0.3, 1.3/r)
	// the soft edge: a step more see-through than the body, less of a step as the whole lobe thins (no bubble rims)
	D := func(i int) uint32 { return SM[min(m-1, i+back+dark)] }
	soft := 0.55 + 0.4*(1-math.Min(1, a))
	px := func(lx, ly float64, sx, sy int) {
		dx, dy := (lx-x)/r, (ly-y)/r
		d2 := dx*dx + dy*dy
		if d2 > 1 {
			return
		}
		d, nz := math.Sqrt(d2), math.Sqrt(1-d2)
		sky := -0.5*dx - 0.66*dy + 0.56*nz
		v := fire*(1.32-0.78*d) + 0.14*sky*fire
		var c uint32
		k, solid := 0.0, false
		switch {
		case v > 0.44:
			i := int(math.Min(float64(n-3), math.Max(0, math.Floor((1.3-v)*float64(n-1)*0.78))))
			if d > seam {
				i = min(6, max(4, i+2))
			} else if d > seam-0.22 {
				i = min(n-3, i+1)
			}
			c, k, solid = FR[i], 0.05*fire, true
		case style != 0:
			lam := -0.42*dx - 0.78*dy + 0.46*nz
			switch {
			case lam > 0.66 && d > 0.4:
				c = D(0)
			case lam > 0.16:
				c = D(1)
			case lam > -0.3:
				c = D(2)
			default:
				c = D(pick(lam < -0.64, 4, 3))
				g := glow * (0.6 + 0.4*dy)
				switch {
				case g > 0.55:
					c, k = FR[pick(g > 0.8, 5, 6)], 0.08*g
				case g > 0.34:
					c = pixelfx.Mix(c, FR[6], 0.6)
				case g > 0.16:
					c = pixelfx.Mix(c, FR[7], 0.45)
				}
			}
		default:
			g := glow * (0.82*dy + 0.3*nz)
			switch {
			case g > 0.45:
				c, k = FR[pick(g > 0.66, 4, 5)], 0.1*glow
			case g > 0.3:
				c = FR[6]
			case g > 0.2:
				c = FR[7]
			default:
				s := sky*0.5 + 0.5
				switch {
				case s > 0.8:
					c = D(0)
				case s > 0.62:
					c = D(1)
				case s > 0.42:
					c = D(2)
				case s > 0.22:
					c = D(3)
				default:
					c = D(4)
				}
			}
		}
		if solid || d <= rim {
			p.BlendPx(sx, sy, c, a)
		} else {
			p.BlendPx(sx, sy, c, a*soft)
		}
		if k > 0 {
			p.Add(sx, sy, c, k)
		}
	}
	if p.SN != 0 || p.CS != 1 {
		p.Each(x-r, y-r, x+r, y+r, px)
		return
	}
	// unrotated (the usual case): walk the screen pixels directly
	ax, bx := p.TX+(x-r)*p.FX, p.TX+(x+r)*p.FX
	sx0, sx1 := math.Max(float64(p.OX), math.Floor(math.Min(ax, bx))), math.Min(float64(p.OX+p.W-1), math.Ceil(math.Max(ax, bx)))
	sy0, sy1 := math.Max(float64(p.OY), math.Floor(p.TY+y-r)), math.Min(float64(p.OY+p.H-1), math.Ceil(p.TY+y+r))
	for sy := sy0; sy <= sy1; sy++ {
		ly := sy + 0.5 - p.TY
		q := (ly - y) / r
		if q*q >= 1 {
			continue
		}
		// only the span of this row inside the circle
		hw, cx := r*math.Sqrt(1-q*q), p.TX+x*p.FX
		for sx, e := math.Max(sx0, math.Floor(cx-hw-0.5)), math.Min(sx1, math.Ceil(cx+hw)); sx <= e; sx++ {
			px((sx+0.5-p.TX)*p.FX, ly, int(sx), int(sy))
		}
	}
}

// plumeOpts shape a rolling fireball (see plume): where it starts (X, Y), how far it climbs (Rise, slowing with time constant Tau, then drifting up at Drift px/s), its tube and ring radii from start to end (growing with time constant Grow), its N lobes (sized Size, swelling by Swell as they cool), its roll (Spin, slowing with SpinTau), its heat (Heat, cooling by Cool per second, the underside keeping Under of it), how it is seen (Tilt, Squash), when its lobes fade (Fade) and scatter (Spread), the strength of its Glow, and for a nuclear cap Style 1 (cel-shaded lobes), Inner (a glow that lingers inside and draws back to the middle as it cools), Hollow (the underside sits a step darker) and Fray [t0, rate, t1] (the rim sheds small puffs late on, through onFray).
type plumeOpts struct {
	X, Y, Rise, Tau, Drift float64
	Tube, Ring             [2]float64
	Grow                   float64
	N                      int
	Size                   [2]float64
	Swell, Spin, SpinTau   float64
	Heat, Cool, Under      float64
	Tilt, Squash           float64
	Fade                   [2]float64
	Glow, Spread           float64
	Style                  int
	Inner, Hollow          bool
	Fray                   []float64
}

type plumeLobe struct{ th, ph, j, s, h, dx, dy, f, gv float64 }

type plumeShape struct{ x, y, tube, ring, g float64 }

type plumeAt struct {
	x, y, z, up, out, heat float64
	q                      *plumeLobe
	fade, r, d, c          float64
}

// plume is a rolling fireball. Its lobes ride a vortex ring that climbs, swells and rolls (up through the middle, out over the top, down the outside, back in underneath), while it cools from white-hot fire into smoke that glows from inside, then thins away. Seen from a little below: the near side sits lower.
type plume struct {
	o       plumeOpts
	R       *pixelfx.Rng
	t, roll float64
	onFray  func(x, y, r, ux, uy float64)
	lobes   []plumeLobe
}

func newPlume(R *pixelfx.Rng, o plumeOpts) *plume {
	pl := &plume{o: o, R: R, lobes: make([]plumeLobe, o.N)}
	// spread over the ring evenly: the roll angle on a golden-ratio walk, the turn round the axis in order
	n := float64(o.N)
	for i := range pl.lobes {
		fi := float64(i)
		q := &pl.lobes[i]
		q.th = tau * math.Mod(fi*0.618034+R.Range(-0.08, 0.08), 1)
		q.ph = (tau * (fi + R.Range(-0.35, 0.35))) / n
		q.j = R.Range(-0.55, 0.12)
		q.s = R.Range(o.Size[0], o.Size[1])
		q.h = R.Range(-0.22, 0.08)
		q.dx = R.Range(-1, 1)
		q.dy = R.Range(-1, 0.4)
		q.f = R.Range(-0.9, 0.5)
		q.gv = R.Range(0.55, 1.15)
	}
	return pl
}

// shape returns the plume's centre and radii at time t.
func (pl *plume) shape(t float64) plumeShape {
	o := &pl.o
	u := math.Max(0, t)
	g := 1 - math.Exp(-u/o.Grow)
	return plumeShape{x: o.X, y: o.Y - o.Rise*(1-math.Exp(-u/o.Tau)) - o.Drift*u, tube: o.Tube[0] + (o.Tube[1]-o.Tube[0])*g, ring: o.Ring[0] + (o.Ring[1]-o.Ring[0])*g, g: g}
}

// hot returns how hot the plume's heart still is at time t (0..1).
func (pl *plume) hot(t float64) float64 { return pixelfx.Clamp(pl.o.Heat - pl.o.Cool*t + 0.3) }

// ember returns how strongly the plume's inside still glows at time t (0..1), lingering well after the fire has gone.
func (pl *plume) ember(t float64) float64 {
	return pixelfx.Clamp((pl.o.Heat - pl.o.Cool*t + 1) / 1.1)
}

// lobesAt returns where every lobe is at time t, far to near.
func (pl *plume) lobesAt(t float64) []plumeAt {
	o := &pl.o
	S := pl.shape(t)
	base, span := o.Heat-o.Cool*t, S.ring+S.tube
	if span == 0 {
		span = 1
	}
	list := make([]plumeAt, 0, len(pl.lobes))
	for i := range pl.lobes {
		q := &pl.lobes[i]
		fade := 1 - pixelfx.Smooth(o.Fade[0]+q.f, o.Fade[1]+q.f*0.5, t)
		loose := math.Pow(pixelfx.Smooth(o.Fade[0]-1.8+q.f, o.Fade[1], t), 1.5) * o.Spread
		if fade <= 0 {
			continue
		}
		th := q.th - pl.roll
		rt := S.tube * (1 + q.j)
		rho, up := S.ring+rt*math.Cos(th), math.Sin(th)
		X, Z := rho*math.Cos(q.ph), rho*math.Sin(q.ph)
		// the outside cools first; the underside keeps its heat, fed from below
		heat := base + q.h - 0.18*math.Max(0, math.Cos(th))*pixelfx.Clamp(t*2) + o.Under*math.Max(0, -up)*pixelfx.Clamp(base+0.6)
		r := q.s * (0.5 + 0.5*S.g) * (1 + o.Swell*pixelfx.Clamp(1-heat)) * (1 + 0.35*(1-fade))
		list = append(list, plumeAt{
			x: S.x + X + q.dx*loose, y: S.y - rt*up*o.Squash + Z*o.Tilt + q.dy*loose, z: Z / span, up: up, out: math.Cos(th), heat: heat,
			q: q, fade: fade, r: r, d: pixelfx.Hypot(X, Z) / span, c: 1 - math.Min(1, math.Abs(X)/span),
		})
	}
	slices.SortStableFunc(list, func(a, b plumeAt) int { return cmp.Compare(a.z, b.z) })
	return list
}

func (pl *plume) update(dt float64) {
	o := &pl.o
	pl.t += dt
	pl.roll += o.Spin * (0.12 + 0.88*math.Exp(-pl.t/o.SpinTau)) * dt
	// late on, the rim frays: lobes on the outside shed small puffs that drift off and thin
	if o.Fray == nil || pl.onFray == nil || pl.t <= o.Fray[0] {
		return
	}
	end := math.Inf(1)
	if len(o.Fray) > 2 {
		end = o.Fray[2]
	}
	if pl.t >= end {
		return
	}
	S := pl.shape(pl.t)
	k := o.Fray[1] * dt * pixelfx.Smooth(o.Fray[0], o.Fray[0]+1, pl.t)
	for _, e := range pl.lobesAt(pl.t) {
		if e.out < 0.2 || !pl.R.Chance(k*e.fade) {
			continue
		}
		ux, uy := e.x-S.x, e.y-S.y
		l := pixelfx.Hypot(ux, uy)
		if l == 0 {
			l = 1
		}
		pl.onFray(e.x+(ux/l)*e.r*0.6, e.y+(uy/l)*e.r*0.6, e.r, ux/l, uy/l)
	}
}

// draw draws the plume in the fire ramp FR (bright to dark) and the smoke ramp SM (light to dark).
func (pl *plume) draw(p *pixelfx.Painter, FR, SM []uint32) {
	o, t := &pl.o, pl.t
	if t >= o.Fade[1]+0.5 {
		return
	}
	S := pl.shape(t)
	base, span := o.Heat-o.Cool*t, S.ring+S.tube
	if span == 0 {
		span = 1
	}
	list, age := pl.lobesAt(t), pixelfx.Clamp(t/o.Fade[1])
	E := pl.ember(t)
	reach := 0.3 + 1.15*E
	for i := range list {
		e := &list[i]
		h, fade := e.heat, e.fade
		fire := pixelfx.Clamp((h - 0.2) / 0.75)
		// the glow from inside: strongest under the middle, uneven from lobe to lobe; with Inner, it draws back to the middle as the cap cools
		var glow float64
		if o.Inner {
			glow = E * pixelfx.Clamp((reach-e.d)/0.45) * (0.45 + 0.55*math.Max(0, -e.up)) * e.q.gv * o.Glow
		} else {
			glow = pixelfx.Clamp((base+0.45)/0.85) * o.Glow * (0.4 + 0.35*math.Max(0, -e.up) + 0.4*e.c) * e.q.gv
		}
		dark := pick(age > 0.72, 1, 0) + pick(o.Hollow && e.up < -0.35, 1, 0)
		lobe(p, e.x, e.y, e.r, FR, SM, fire, glow, fade*(0.82+0.18*fire), e.z, dark, o.Style)
		if fire > 0.25 {
			p.Glow(e.x, e.y, e.r*1.25+2, FR[2], 0.07*fire)
		}
	}
	if hot := pl.hot(t); hot > 0 {
		p.Glow(S.x, S.y, span*1.8+14, FR[3], 0.32*hot*o.Glow)
	}
	if o.Inner && E > 0.05 {
		p.Glow(S.x, S.y+S.tube*0.3, span*(0.5+0.7*E)+8, FR[4], 0.22*E*E)
	}
}

// columnOpts shape a rising column: its stem's half-width W, how much wider its foot is (Foot), how fast its lobes rise (Speed), how big they are (Size), how many a stem's height holds (N), how far they sway (Sway) and Style 1 to cel-shade them.
type columnOpts struct {
	W, Foot, Speed float64
	Size           [2]float64
	N, Sway        float64
	Style          int
}

type columnLobe struct{ y, x, s, ph, a, v float64 }

// column is a rising column: lobes boil up a stem from the floor into whatever sits on top, wide at the foot, swaying, cooling from fire to smoke and dust.
type column struct {
	R      *pixelfx.Rng
	o      columnOpts
	t, acc float64
	lobes  []columnLobe
}

func newColumn(R *pixelfx.Rng, o columnOpts) *column { return &column{R: R, o: o} }

func (c *column) spawn(y float64) {
	R, o := c.R, &c.o
	var b columnLobe
	b.y = y
	b.x = R.Range(-1, 1)
	b.s = R.Range(o.Size[0], o.Size[1])
	b.ph = R.Range(0, tau)
	b.v = R.Range(0.8, 1.2)
	c.lobes = append(c.lobes, b)
}

// update moves the column on: top is the y the stem rises to, feed 0..1 how strongly it is still being fed (new lobes stop when it is 0, and the stem breaks up from the bottom).
func (c *column) update(dt, top, feed float64) {
	o := &c.o
	L := math.Max(10, -top)
	c.t += dt
	if feed > 0 {
		for c.acc += dt * feed * o.N * (o.Speed / L); c.acc > 1; c.acc-- {
			c.spawn(-1)
		}
	}
	for i := len(c.lobes) - 1; i >= 0; i-- {
		b := &c.lobes[i]
		u := pixelfx.Clamp(-b.y / L)
		b.y -= o.Speed * b.v * (0.45 + 0.55*math.Min(1, -b.y/24)) * dt
		b.a = math.Min(1, b.a+dt*4)
		if b.y < top {
			c.lobes = slices.Delete(c.lobes, i, i+1)
			continue
		}
		if feed <= 0 {
			if b.a -= dt * (2 - u); b.a <= 0 {
				c.lobes = slices.Delete(c.lobes, i, i+1)
			}
		}
	}
}

// draw draws the column up to top in the fire ramp FR and the smoke ramp SM, with fire the heat at its foot (0..1) and glow its under-light (0..1).
func (c *column) draw(p *pixelfx.Painter, top float64, FR, SM []uint32, fire, glow float64) {
	o := &c.o
	L := math.Max(10, -top)
	// draw from the top down so the foot sits in front
	list := slices.Clone(c.lobes)
	slices.SortStableFunc(list, func(a, b columnLobe) int { return cmp.Compare(a.y, b.y) })
	for _, b := range list {
		u := pixelfx.Clamp(-b.y / L)
		foot := math.Pow(1-u, 4)
		w := o.W + o.Foot*foot
		x := b.x*w*0.45 + math.Sin(c.t*1.7+b.ph+u*3)*o.Sway*u
		r := b.s * (0.75 + 0.5*foot + 0.35*u) * math.Min(1, b.a*1.5)
		end := 1 - pixelfx.Smooth(0.85, 1, u)
		lobe(p, x, b.y-r*0.3*foot, r, FR, SM, pixelfx.Clamp(fire*(1.1-u*0.9)), glow*pixelfx.Smooth(0.55, 1, u), math.Min(1, b.a)*end*0.9, 0, pick(u > 0.5, 0, 1), o.Style)
	}
}

// rangeOr returns a number in [a, b) with chance p, else or; it draws the second number only when the chance comes up, as pixelfx's R.chance(p) ? R.range(a, b) : or does.
func rangeOr(R *pixelfx.Rng, p, a, b, or float64) float64 {
	if R.Chance(p) {
		return R.Range(a, b)
	}
	return or
}
