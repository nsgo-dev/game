// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the star and flying-arc recipes from Sprite Fusion's open-source impact (https://www.spritefusion.com/pixel-art-vfx/impact), and the stone colors from its explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the impacts: the hit star, a ring that breaks into flying arcs, floor cracks that glow and cool, standing shock fronts, soft lumpy dust rings, faceted tumbling rocks, and the stone colors they share.

var (
	soot    = pixelfx.Hex("#0d0a12")
	outline = pixelfx.Hex("#1a1422")
	// rockLight is where light comes from: the top left
	rockLight = math.Atan2(-1, -1)
	// pi is π as a variable, so sums with it round as pixelfx's do
	pi = math.Pi
)

// stoneCols are stone colors from one body color: light, body, shade, dust (lighter and greyer), lip (a lit broken edge).
type stoneCols struct{ light, body, shade, dust, lip uint32 }

func stoneOf(c uint32) stoneCols {
	return stoneCols{
		light: pixelfx.Mix(pixelfx.Scale(c, 1.35), pixelfx.White, 0.08),
		body:  c,
		shade: pixelfx.Scale(c, 0.64),
		dust:  pixelfx.Mix(pixelfx.Scale(c, 1.2), pixelfx.RGB(150, 140, 150), 0.3),
		lip:   pixelfx.Mix(pixelfx.Scale(c, 1.55), pixelfx.White, 0.12),
	}
}

// spike is one spike of a hit star: its angle, length and root half-width.
type spike struct{ a, len, w float64 }

// spikeOpts shape a hit star's spikes: len0..len1 and w0..w1 are the base length and root half-width, fwd and back lengthen them towards aim and a little behind it, jag scales every other spike (short teeth between), jit jitters their angles. (pixelfx's defaults are len [7, 10], w [1.5, 2.1], fwd 1.8, back 0.6, jag 1 and jit 0.18.)
type spikeOpts struct {
	len0, len1, w0, w1, fwd, back, jag, jit float64
}

// spikes returns n spikes for a hit star round the circle, longer towards aim.
func spikes(R *pixelfx.Rng, n int, aim float64, o spikeOpts) []spike {
	out := make([]spike, n)
	for i := range out {
		a := aim + (float64(i)/float64(n))*pixelfx.Tau + R.Range(-o.jit, o.jit)
		cb := math.Cos(a - aim)
		jag, wj := 1.0, 1.0
		if i%2 != 0 {
			jag, wj = o.jag, math.Sqrt(o.jag)
		}
		L := R.Range(o.len0, o.len1) * (1 + o.fwd*math.Pow(math.Max(0, cb), 3) + o.back*math.Pow(math.Max(0, -cb), 3)) * jag
		out[i] = spike{a: a, len: L, w: R.Range(o.w0, o.w1) * wj}
	}
	return out
}

// star draws a hit star: tapered spikes in three passes (a rim, a body that's paler near the root, a hot core) round a hot heart. sc stretches the spikes, th thins them, cols are [rim, body, inner, core], a dithers it out, heart sizes the heart (pixelfx's defaults are 1 and 2.4).
func star(p *pixelfx.Painter, x, y float64, list []spike, sc, th float64, cols [4]uint32, a, heart float64) {
	rim, body, inner, core := cols[0], cols[1], cols[2], cols[3]
	for pass := range 3 {
		for _, s := range list {
			c, sn, L := math.Cos(s.a), math.Sin(s.a), s.len*sc
			for d := 0.0; d <= L; d += 0.5 {
				if pass == 2 && d > L*0.55 {
					break
				}
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
				for o := -hw; o <= hw; o += 0.5 {
					p.Dot(x+c*d-sn*o, y+sn*d+c*o, col, a)
				}
			}
		}
	}
	r := heart * sc * math.Sqrt(th)
	p.Circle(x, y, r+0.8, inner, a)
	p.Circle(x, y, r, core, a)
}

// ringSeg is one arc of a broken ring: its centre angle, the share of its span it loses as the ring breaks (gap), how fast it flies out (dr) and its thickness scale (w).
type ringSeg struct{ a, gap, dr, w float64 }

// brokenRing draws a ring that breaks into arcs, segs evenly round it. brk 0 draws a whole ring th thick; as brk grows each arc shrinks, tapers at both ends into a crescent and drifts out at its own speed. c is the outer edge's color, c2 the inner's; sy squashes it onto a plane; nothing is drawn at or below maxY (a floor).
func brokenRing(p *pixelfx.Painter, x, y, r, th float64, segs []ringSeg, brk float64, c, c2 uint32, k, sy, maxY float64) {
	if r <= 0 || k <= 0 {
		return
	}
	full := pixelfx.Tau / float64(len(segs))
	for _, s := range segs {
		span, rr := full*(1-s.gap*brk)+0.02, r*(1+s.dr*brk)
		m := math.Max(4, math.Ceil(rr*span*1.8))
		for j := 0.0; j <= m; j++ {
			u := j / m
			ang := s.a + (u-0.5)*span
			w := math.Max(0.5, th*s.w*(1-brk*math.Pow(math.Abs(2*u-1), 1.6)))
			ca, sa := math.Cos(ang), math.Sin(ang)
			for o := -w / 2; o <= w/2+0.01; o += 0.5 {
				if y+sa*(rr+o)*sy < maxY {
					col := c2
					if o > -0.26 {
						col = c
					}
					p.Dot(x+ca*(rr+o), y+sa*(rr+o)*sy, col, k)
				}
			}
		}
	}
}

// crackPt is a point of a crack: where it is, and how far along the crack.
type crackPt struct{ x, y, d float64 }

type walkOpts struct {
	jit, steer, step0, step1, maxY, d0 float64
}

// walk returns a jagged walk from (x, y) heading a for L px.
func walk(R *pixelfx.Rng, x, y, a, L float64, o walkOpts) []crackPt {
	pts := []crackPt{{x, y, o.d0}}
	d, cur := 0.0, a
	for d < L-0.01 {
		s := math.Min(L-d, R.Range(o.step0, o.step1))
		cur += R.Range(-o.jit, o.jit) + (a-cur)*o.steer
		x += math.Cos(cur) * s
		y = pixelfx.ClampTo(y+math.Sin(cur)*s, 0.5, o.maxY)
		d += s
		pts = append(pts, crackPt{x, y, o.d0 + d})
	}
	return pts
}

// crack is one crack in the floor: its points, length, when it starts and its forks.
type crack struct {
	pts  []crackPt
	len  float64
	t0   float64
	kids [][]crackPt
}

type crackOpts struct {
	len0, len1, dive, maxY, forks, x00, x01 float64
}

// makeCracks returns cracks in the floor face below (0, 0): n jagged lines, alternately left and right, mostly shallow, a few diving steeply, some forked. (pixelfx's defaults are len [14, 40], dive 0.85, maxY 22, forks 0.5 and x0 [1, 5].)
func makeCracks(R *pixelfx.Rng, n int, o crackOpts) []crack {
	out := make([]crack, 0, n)
	for i := range n {
		side := -1.0
		if i%2 != 0 {
			side = 1
		}
		dip := 0.06 + math.Pow(R.Next(), 1.7)*o.dive
		L := R.Range(o.len0, o.len1) * (1 - (0.5*dip)/(o.dive+0.06))
		a := pi - dip
		if side > 0 {
			a = dip
		}
		x := side * R.Range(o.x00, o.x01)
		y := R.Range(0.5, 1.5)
		pts := walk(R, x, y, a, L, walkOpts{jit: 0.55, steer: 0.45, step0: 2, step1: 4, maxY: o.maxY})
		c := crack{pts: pts, len: L, t0: R.Range(0, 0.035)}
		for j := 2; j < len(pts)-1; j++ {
			if R.Chance(o.forks * 0.4) {
				f := pts[j]
				ka := a + R.Sign()*R.Range(0.45, 1)
				kl := R.Range(3, 9) * (1 - (0.5*float64(j))/float64(len(pts)))
				c.kids = append(c.kids, walk(R, f.x, f.y, ka, kl, walkOpts{jit: 0.7, steer: 0.45, step0: 1.5, step1: 3, maxY: o.maxY, d0: f.d}))
			}
		}
		out = append(out, c)
	}
	return out
}

// drawCracks draws cracks grown to g(c) px of their length. heat(d) 0..1 at distance d along a crack lights it from the hot ramp (bright to dark) before it cools to dark; lip is the lit lower edge; k dithers them out. It adds a glow along the hot parts.
func drawCracks(p *pixelfx.Painter, cracks []crack, g func(c *crack) float64, heat func(d float64) float64, hot [4]uint32, dark, lip uint32, k float64) {
	if k <= 0 {
		return
	}
	col := func(d float64) uint32 {
		h := heat(d)
		switch {
		case h > 0.75:
			return hot[0]
		case h > 0.5:
			return hot[1]
		case h > 0.3:
			return hot[2]
		case h > 0.12:
			return hot[3]
		}
		return dark
	}
	for ci := range cracks {
		c := &cracks[ci]
		G := g(c)
		if G <= 0 {
			continue
		}
		paths := append([][]crackPt{c.pts}, c.kids...)
		for _, path := range paths {
			for i := 1; i < len(path); i++ {
				x0, y0, d0 := path[i-1].x, path[i-1].y, path[i-1].d
				x1, y1, d1 := path[i].x, path[i].y, path[i].d
				if d0 >= G {
					break
				}
				f := 1.0
				if d1 > G {
					f = (G - d0) / (d1 - d0)
				}
				ex, ey := x0+(x1-x0)*f, y0+(y1-y0)*f
				p.LineFn(x0, y0+1, ex, ey+1, func(u float64) (uint32, float64) {
					if pixelfx.Rand(pixelfx.Round(x0+(ex-x0)*u), pixelfx.Round(y0+(ey-y0)*u), 5) < 0.3 {
						return lip, k * 0.8
					}
					return lip, 0
				})
				p.LineFn(x0, y0, ex, ey, func(u float64) (uint32, float64) { return col(d0 + (d1-d0)*f*u), k })
				if h := heat(d0); h > 0.12 {
					p.Glow((x0+ex)/2, (y0+ey)/2, 4+h*4, hot[2], 0.22*h*k)
				}
			}
		}
	}
}

// frontCrescent draws a standing crescent shock front on the floor (y = 0): it leans back from a leading edge low at x, dir 1 racing right ")", -1 left "(". h tall, w thick at its widest (thick where it meets the floor, a point at the top); cols [edge, body, back] bright to dark; k dithers it out.
func frontCrescent(p *pixelfx.Painter, x, dir, h, w float64, cols [3]uint32, k float64) {
	if h < 1 || w < 0.3 || k <= 0 {
		return
	}
	ry, cy, rx := h*0.8, -h*0.2, math.Max(2, h*0.55)
	x0, x1 := x-1, x+rx+w+1
	if dir > 0 {
		x0, x1 = x-rx-w-1, x+1
	}
	p.Shade(x0, -h-1, x1, 0, func(px, py float64, out *pixelfx.Out) uint32 {
		if py > 0 {
			return 0
		}
		ey := (py - cy) / ry
		if ey <= -1 || ey >= 1 {
			return 0
		}
		e2 := 1 - ey*ey
		edge, wt := rx*math.Sqrt(e2)-rx, w*math.Pow(e2, 0.45)
		q := (edge - (px-x)*dir) / math.Max(0.5, wt)
		if q < 0 || q > 1 {
			return 0
		}
		switch {
		case q < 0.28:
			out.C = cols[0]
		case q < 0.62:
			out.C = cols[1]
		default:
			out.C = cols[2]
		}
		a := k
		if ey < -0.8 {
			a = k * 0.7
		}
		if q > 0.7 {
			a *= 1.25 - q*0.8
		} else {
			a *= 1
		}
		out.A = a
		return 0
	})
}

// dustOpts shape a dust ring: sy squashes it onto the floor, ph varies it, brk 0..1 tears it into clumps as it thins, lit is how much paler its lit side is, nothing is drawn at or below maxY, and half -1 draws only the far half (above the centre), 1 only the near half, 0 both. (pixelfx's defaults are sy 1, ph 0, brk 0, lit 0.35, maxY Infinity and half 0.)
type dustOpts struct {
	sy, ph, brk, lit, maxY, half float64
}

// dustRing draws a soft, lumpy ring of dust round (cx, cy): radius r, w thick (lumps swell it), alpha k, translucent and lit on its upper side.
func dustRing(p *pixelfx.Painter, cx, cy, r, w, k float64, c uint32, o dustOpts) {
	if k <= 0.01 || r <= 0 {
		return
	}
	W, lite := w*1.6, pixelfx.Mix(c, pixelfx.White, o.lit)
	y1 := math.Min(o.maxY-1, math.Ceil(cy+(r+W)*o.sy)+1)
	for y := math.Floor(cy-(r+W)*o.sy) - 1; y <= y1; y++ {
		for x := math.Floor(cx-r-W) - 1; x <= math.Ceil(cx+r+W)+1; x++ {
			dx, dy := x+0.5-cx, (y+0.5-cy)/o.sy
			d, g := pixelfx.Hypot(dx, dy), math.Atan2(dy, dx)
			if (o.half < 0 && dy >= 0) || (o.half > 0 && dy < 0) {
				continue
			}
			lumpy := 0.6 + 0.3*math.Sin(5*g+o.ph) + 0.18*math.Sin(11*g+o.ph*2.1) + 0.12*math.Sin(17*g+o.ph*0.7)
			wt, off := w*lumpy, math.Abs(d-r-w*0.25*math.Sin(7*g+o.ph))
			if off >= wt {
				continue
			}
			tear := 1.0
			if o.brk > 0 {
				tear = pixelfx.Clamp((0.55 + 0.45*math.Sin(6*g+o.ph*1.7) - o.brk) / 0.25)
			}
			a := k * (1 - math.Pow(off/wt, 2)) * tear
			if a > 0.01 {
				col := c
				if dy < 0 && d > r-wt*0.3 {
					col = lite
				}
				p.Blend(x, y, col, a)
			}
		}
	}
}

// rockShape returns a rock's outline: n vertex radii round its centre.
func rockShape(R *pixelfx.Rng, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = R.Range(0.7, 1.12)
	}
	return out
}

// rock draws a faceted rock tumbling at angle b.A: every facet shaded by how it faces the light, a lit top face, a dark outline, dithering out over its last half second. b.Pts is its outline (rockShape).
func rock(p *pixelfx.Painter, b *pixelfx.Bit, st stoneCols) {
	r, n := 0.75+b.S*0.8, len(b.Pts)
	fade, ca, sa := math.Min(1, b.Life/0.5), math.Cos(b.A), math.Sin(b.A)
	p.Shade(b.X-r-2, b.Y-r-2, b.X+r+2, b.Y+r+2, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		u, v := dx*ca+dy*sa, -dx*sa+dy*ca
		d := pixelfx.Hypot(u, v)
		ang := math.Atan2(v, u)
		if ang < 0 {
			ang += pixelfx.Tau
		}
		f := (ang / pixelfx.Tau) * float64(n)
		i := int(math.Mod(math.Floor(f), float64(n)))
		w := f - math.Floor(f)
		edge := r * (b.Pts[i]*(1-w) + b.Pts[(i+1)%n]*w)
		if d > edge+0.85 {
			return 0
		}
		out.A = fade
		if d > edge {
			return outline
		}
		lit := math.Cos(((float64(i)+0.5)/float64(n))*pixelfx.Tau + b.A - rockLight)
		if d < edge*0.42 {
			if lit > 0.55 {
				return st.light
			}
			return st.body
		}
		switch {
		case lit > 0.35:
			return st.light
		case lit < -0.4:
			return st.shade
		}
		return st.body
	})
}
