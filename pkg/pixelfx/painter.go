// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/painter.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import (
	"math"
	"unicode/utf8"
)

// bayer holds the 4x4 ordered (Bayer) thresholds: a pixel at alpha a is drawn when a is above its cell's threshold.
var bayer = func() [16]float64 {
	var out [16]float64
	for i, v := range [16]float64{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5} {
		out[i] = (v + 0.5) / 16
	}
	return out
}()

// Layer is which of an effect's layers is being drawn.
type Layer uint8

const (
	// LayerBoth draws both layers, back then front.
	LayerBoth Layer = iota
	// LayerBack is what goes behind a character.
	LayerBack
	// LayerFront is what goes in front of a character.
	LayerFront
)

// Out is what a Shade function sets for its pixel: C its color, A its alpha (0..1, faded by ordered dither), L the color of light it adds (C when 0) and K how much.
type Out struct {
	C uint32
	A float64
	L uint32
	K float64
}

// Painter draws into a frame's two buffers: Color (opaque pixels, faded with ordered dither, never blended) and Light (additive glow, composited on top). Effects draw in local coordinates around their origin; the painter places, mirrors and rotates them, so an effect never needs to know where it is.
//
// Every shape comes in two forms: one with a color c and alpha a, and one whose name ends in Fn taking a function of the shape's own coordinates that returns both, as pixelfx lets a color or an alpha be a function.
type Painter struct {
	// Frame is the frame the painter draws into.
	Frame *Frame
	// W, H, OX and OY are the frame's size and top-left, and C and L its color and light buffers.
	W, H, OX, OY int
	C, L         []uint32
	// TX and TY are where the effect's origin sits in frame space, FX is -1 when it is mirrored (else 1), and CS and SN are the cosine and sine of its angle.
	TX, TY, FX, CS, SN float64
	// Fade scales every alpha and light (1 while an effect plays, falling to 0 as a stopped loop fades out), and LightK scales light (an effect's light option).
	Fade, LightK float64
	// Layer is the layer being drawn.
	Layer Layer

	clip               bool
	cx0, cy0, cx1, cy1 float64
}

// Reset re-reads the frame and clears placement, fade, light strength and clip.
func (p *Painter) Reset() *Painter {
	f := p.Frame
	p.W, p.H, p.OX, p.OY, p.C, p.L = f.W, f.H, f.OX, f.OY, f.Color, f.Light
	p.TX, p.TY, p.FX, p.CS, p.SN = 0, 0, 1, 1, 0
	p.Fade, p.LightK, p.Layer = 1, 1, LayerFront
	p.clip = false
	return p
}

// Clipped returns a copy of p drawing into the same frame that drops any pixel whose frame center falls outside [x0, x1) x [y0, y1) in local space.
func (p *Painter) Clipped(x0, y0, x1, y1 float64) *Painter {
	q := *p
	if q.clip {
		q.cx0, q.cy0 = math.Max(q.cx0, x0), math.Max(q.cy0, y0)
		q.cx1, q.cy1 = math.Min(q.cx1, x1), math.Min(q.cy1, y1)
	} else {
		q.clip = true
		q.cx0, q.cy0, q.cx1, q.cy1 = x0, y0, x1, y1
	}
	return &q
}

func (p *Painter) clipped(sx, sy int) bool {
	if !p.clip {
		return false
	}
	lx, ly := p.FromX(float64(sx)+0.5, float64(sy)+0.5), p.FromY(float64(sx)+0.5, float64(sy)+0.5)
	return lx < p.cx0 || ly < p.cy0 || lx >= p.cx1 || ly >= p.cy1
}

// Place puts the effect's origin at (x, y) in frame space, mirrored if flip, rotated by angle (radians).
func (p *Painter) Place(x, y float64, flip bool, angle float64) *Painter {
	p.TX, p.TY = x, y
	p.FX = 1
	if flip {
		p.FX = -1
	}
	p.CS, p.SN = math.Cos(angle), math.Sin(angle)
	return p
}

// ToX returns the frame x of the local point (x, y).
func (p *Painter) ToX(x, y float64) float64 { return p.TX + x*p.FX*p.CS - y*p.SN }

// ToY returns the frame y of the local point (x, y).
func (p *Painter) ToY(x, y float64) float64 { return p.TY + x*p.FX*p.SN + y*p.CS }

// FromX returns the local x of the frame point (sx, sy).
func (p *Painter) FromX(sx, sy float64) float64 {
	dx, dy := sx-p.TX, sy-p.TY
	return (dx*p.CS + dy*p.SN) * p.FX
}

// FromY returns the local y of the frame point (sx, sy).
func (p *Painter) FromY(sx, sy float64) float64 {
	dx, dy := sx-p.TX, sy-p.TY
	return -dx*p.SN + dy*p.CS
}

// CellX returns the frame x of the local pixel cell holding (x, y): mirrors and rotations land on whole cells.
func (p *Painter) CellX(x, y float64) int {
	return int(math.Floor(p.ToX(math.Floor(x)+0.5, math.Floor(y)+0.5)))
}

// CellY returns the frame y of the local pixel cell holding (x, y).
func (p *Painter) CellY(x, y float64) int {
	return int(math.Floor(p.ToY(math.Floor(x)+0.5, math.Floor(y)+0.5)))
}

// Put writes color c at the frame pixel (sx, sy) at alpha a (0..1, faded by ordered dither). Color 0 means "nothing": it never erases what's there.
func (p *Painter) Put(sx, sy int, c uint32, a float64) {
	if p.clipped(sx, sy) || c == 0 {
		return
	}
	a *= p.Fade
	if a < 1 && a <= bayer[(sy&3)<<2|(sx&3)] {
		return
	}
	x, y := sx-p.OX, sy-p.OY
	if x < 0 || y < 0 || x >= p.W || y >= p.H {
		return
	}
	p.C[y*p.W+x] = c
}

// Add adds k of color c's light to the frame pixel (sx, sy).
func (p *Painter) Add(sx, sy int, c uint32, k float64) {
	if p.clipped(sx, sy) {
		return
	}
	k *= p.Fade * p.LightK
	if k <= 0.004 {
		return
	}
	x, y := sx-p.OX, sy-p.OY
	if x < 0 || y < 0 || x >= p.W || y >= p.H {
		return
	}
	i := y*p.W + x
	d := p.L[i]
	r := uint32(ToInt32(math.Min(255, float64(d&255)+float64(c&255)*k)))
	g := uint32(ToInt32(math.Min(255, float64((d>>8)&255)+float64((c>>8)&255)*k)))
	b := uint32(ToInt32(math.Min(255, float64((d>>16)&255)+float64((c>>16)&255)*k)))
	p.L[i] = 255<<24 | b<<16 | g<<8 | r
}

// BlendPx blends color c over what's at the frame pixel (sx, sy) at alpha a (0..1), keeping the result's own alpha: soft smoke, vapour and mist. Use Dot for hard pixel fades and Blend for translucent volumes.
func (p *Painter) BlendPx(sx, sy int, c uint32, a float64) {
	if p.clipped(sx, sy) {
		return
	}
	a *= p.Fade
	if c == 0 || a <= 0.004 {
		return
	}
	x, y := sx-p.OX, sy-p.OY
	if x < 0 || y < 0 || x >= p.W || y >= p.H {
		return
	}
	i := y*p.W + x
	d := p.C[i]
	da := float64(d>>24) / 255
	oa := a + da*(1-a)
	k := a / oa
	ch := func(s uint) uint32 {
		return uint32(ToInt32(float64((c>>s)&255)*k+float64((d>>s)&255)*(1-k)) & 255)
	}
	p.C[i] = uint32(ToInt32(oa*255)&255)<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
}

// Blend draws a translucent pixel at the local point (x, y).
func (p *Painter) Blend(x, y float64, c uint32, a float64) {
	p.BlendPx(p.CellX(x, y), p.CellY(x, y), c, a)
}

// Disc draws a translucent disc: puffs of smoke, vapour, dust.
func (p *Painter) Disc(cx, cy, r float64, c uint32, a float64) {
	if r < 0.5 {
		p.Blend(cx, cy, c, a)
		return
	}
	p.discFn(cx, cy, r, c, a, nil)
}

// DiscFn draws a translucent disc whose color and alpha are fn(d, angle): d 0 at the center to 1 at the rim.
func (p *Painter) DiscFn(cx, cy, r float64, fn func(d, ang float64) (uint32, float64)) {
	if r < 0.5 {
		c, a := fn(0, 0)
		p.Blend(cx, cy, c, a)
		return
	}
	p.discFn(cx, cy, r, 0, 0, fn)
}

func (p *Painter) discFn(cx, cy, r float64, c uint32, a float64, fn func(d, ang float64) (uint32, float64)) {
	p.Each(cx-r, cy-r, cx+r, cy+r, func(x, y float64, sx, sy int) {
		dx, dy := x-cx, y-cy
		d2 := dx*dx + dy*dy
		if d2 > r*r {
			return
		}
		if fn == nil {
			p.BlendPx(sx, sy, c, a)
			return
		}
		cc, aa := fn(math.Sqrt(d2)/r, math.Atan2(dy, dx))
		p.BlendPx(sx, sy, cc, aa)
	})
}

// Get reads back the color under the local point (x, y), 0 if empty.
func (p *Painter) Get(x, y float64) uint32 {
	sx, sy := p.CellX(x, y)-p.OX, p.CellY(x, y)-p.OY
	if sx < 0 || sy < 0 || sx >= p.W || sy >= p.H {
		return 0
	}
	return p.C[sy*p.W+sx]
}

// Px draws one opaque pixel.
func (p *Painter) Px(x, y float64, c uint32) { p.Put(p.CellX(x, y), p.CellY(x, y), c, 1) }

// Dot draws one pixel at alpha a (0..1), faded by ordered dither; nothing at all when a is 0 or less.
func (p *Painter) Dot(x, y float64, c uint32, a float64) {
	if a > 0 {
		p.Put(p.CellX(x, y), p.CellY(x, y), c, a)
	}
}

// Light adds k of color c's light to one pixel.
func (p *Painter) Light(x, y float64, c uint32, k float64) {
	p.Add(p.CellX(x, y), p.CellY(x, y), c, k)
}

// Each visits every frame pixel whose center falls in the local box [x0, x1] x [y0, y1], with that center in local space: fn(lx, ly, sx, sy). It is the basis of every filled shape, so shapes mirror and rotate exactly.
func (p *Painter) Each(x0, y0, x1, y1 float64, fn func(lx, ly float64, sx, sy int)) {
	ax, bx, ay, by := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for k := range 4 {
		x, y := x0, y0
		if k&1 != 0 {
			x = x1
		}
		if k&2 != 0 {
			y = y1
		}
		sx, sy := p.ToX(x, y), p.ToY(x, y)
		if sx < ax {
			ax = sx
		}
		if sx > bx {
			bx = sx
		}
		if sy < ay {
			ay = sy
		}
		if sy > by {
			by = sy
		}
	}
	sx0, sx1 := math.Max(math.Floor(ax), float64(p.OX)), math.Min(math.Ceil(bx), float64(p.OX+p.W-1))
	sy0, sy1 := math.Max(math.Floor(ay), float64(p.OY)), math.Min(math.Ceil(by), float64(p.OY+p.H-1))
	if math.IsNaN(sx0) || math.IsNaN(sx1) || math.IsNaN(sy0) || math.IsNaN(sy1) {
		return
	}
	for sy := int(sy0); sy <= int(sy1); sy++ {
		for sx := int(sx0); sx <= int(sx1); sx++ {
			cx, cy := float64(sx)+0.5, float64(sy)+0.5
			fn(p.FromX(cx, cy), p.FromY(cx, cy), sx, sy)
		}
	}
}

// Shade runs a pixel shader over a local box: fn(x, y, out) returns a color, or sets out.C, out.A, out.L and out.K (Out says what each is). Returning 0 with out.C left 0 leaves the pixel alone.
func (p *Painter) Shade(x0, y0, x1, y1 float64, fn func(x, y float64, o *Out) uint32) {
	var o Out
	p.Each(x0, y0, x1, y1, func(lx, ly float64, sx, sy int) {
		o = Out{A: 1}
		c := fn(lx, ly, &o)
		if c == 0 {
			c = o.C
		}
		if c != 0 {
			p.Put(sx, sy, c, o.A)
		}
		if o.K > 0 {
			l := o.L
			if l == 0 {
				l = c
			}
			p.Add(sx, sy, l, o.K)
		}
	})
}

// Line draws a 1 px line.
func (p *Painter) Line(x0, y0, x1, y1 float64, c uint32, a float64) {
	p.line(x0, y0, x1, y1, c, a, nil)
}

// LineFn draws a 1 px line whose color and alpha are fn(u): u 0 at the start to 1 at the end.
func (p *Painter) LineFn(x0, y0, x1, y1 float64, fn func(u float64) (uint32, float64)) {
	p.line(x0, y0, x1, y1, 0, 0, fn)
}

func (p *Painter) line(x0, y0, x1, y1 float64, c uint32, a float64, fn func(u float64) (uint32, float64)) {
	sx, sy := p.CellX(x0, y0), p.CellY(x0, y0)
	ex, ey := p.CellX(x1, y1), p.CellY(x1, y1)
	dx, dy := absInt(ex-sx), -absInt(ey-sy)
	stepx, stepy := -1, -1
	if sx < ex {
		stepx = 1
	}
	if sy < ey {
		stepy = 1
	}
	n := max(dx, -dy)
	if n == 0 {
		n = 1
	}
	err, i := dx+dy, 0
	for {
		u := float64(i) / float64(n)
		i++
		if fn != nil {
			cc, aa := fn(u)
			p.Put(sx, sy, cc, aa)
		} else {
			p.Put(sx, sy, c, a)
		}
		if sx == ex && sy == ey {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			sx += stepx
		}
		if e2 <= dx {
			err += dx
			sy += stepy
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Polyline draws lines through a list of points.
func (p *Painter) Polyline(pts [][2]float64, c uint32, a float64) {
	for i := 1; i < len(pts); i++ {
		p.Line(pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1], c, a)
	}
}

// PolylineFn draws lines through a list of points, each colored by fn as LineFn colors it.
func (p *Painter) PolylineFn(pts [][2]float64, fn func(u float64) (uint32, float64)) {
	for i := 1; i < len(pts); i++ {
		p.LineFn(pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1], fn)
	}
}

// Beam draws a thick segment with round ends.
func (p *Painter) Beam(x0, y0, x1, y1, width float64, c uint32, a float64) {
	p.beam(x0, y0, x1, y1, width, c, a, nil)
}

// BeamFn draws a thick segment with round ends whose color and alpha are fn(u, v): u along it, v across it (0 at the center, 1 at the edge).
func (p *Painter) BeamFn(x0, y0, x1, y1, width float64, fn func(u, v float64) (uint32, float64)) {
	p.beam(x0, y0, x1, y1, width, 0, 0, fn)
}

func (p *Painter) beam(x0, y0, x1, y1, width float64, c uint32, a float64, fn func(u, v float64) (uint32, float64)) {
	r, dx, dy := width/2, x1-x0, y1-y0
	L2 := dx*dx + dy*dy
	if L2 == 0 {
		L2 = 1e-9
	}
	p.Shade(math.Min(x0, x1)-r, math.Min(y0, y1)-r, math.Max(x0, x1)+r, math.Max(y0, y1)+r, func(x, y float64, o *Out) uint32 {
		u := Clamp(((x-x0)*dx + (y-y0)*dy) / L2)
		px, py := x0+dx*u-x, y0+dy*u-y
		d2 := px*px + py*py
		if d2 > r*r {
			return 0
		}
		if fn == nil {
			o.C, o.A = c, a
			return 0
		}
		o.C, o.A = fn(u, math.Sqrt(d2)/r)
		return 0
	})
}

// Circle draws a filled disc.
func (p *Painter) Circle(cx, cy, r float64, c uint32, a float64) {
	if r < 0.6 {
		p.Dot(cx, cy, c, a)
		return
	}
	p.circle(cx, cy, r, c, a, nil)
}

// CircleFn draws a filled disc whose color and alpha are fn(d, angle): d 0 at the center to 1 at the rim.
func (p *Painter) CircleFn(cx, cy, r float64, fn func(d, ang float64) (uint32, float64)) {
	if r < 0.6 {
		c, a := fn(0, 0)
		p.Dot(cx, cy, c, a)
		return
	}
	p.circle(cx, cy, r, 0, 0, fn)
}

func (p *Painter) circle(cx, cy, r float64, c uint32, a float64, fn func(d, ang float64) (uint32, float64)) {
	p.Shade(cx-r, cy-r, cx+r, cy+r, func(x, y float64, o *Out) uint32 {
		dx, dy := x-cx, y-cy
		d2 := dx*dx + dy*dy
		if d2 > r*r {
			return 0
		}
		if fn == nil {
			o.C, o.A = c, a
			return 0
		}
		o.C, o.A = fn(math.Sqrt(d2)/r, math.Atan2(dy, dx))
		return 0
	})
}

// Ellipse draws a filled ellipse with radii rx and ry; nothing when either is 0 or less.
func (p *Painter) Ellipse(cx, cy, rx, ry float64, c uint32, a float64) {
	p.ellipse(cx, cy, rx, ry, c, a, nil)
}

// EllipseFn draws a filled ellipse whose color and alpha are fn(d, angle): d 0 at the center to 1 at the rim.
func (p *Painter) EllipseFn(cx, cy, rx, ry float64, fn func(d, ang float64) (uint32, float64)) {
	p.ellipse(cx, cy, rx, ry, 0, 0, fn)
}

func (p *Painter) ellipse(cx, cy, rx, ry float64, c uint32, a float64, fn func(d, ang float64) (uint32, float64)) {
	if rx <= 0 || ry <= 0 {
		return
	}
	p.Shade(cx-rx, cy-ry, cx+rx, cy+ry, func(x, y float64, o *Out) uint32 {
		dx, dy := (x-cx)/rx, (y-cy)/ry
		d2 := dx*dx + dy*dy
		if d2 > 1 {
			return 0
		}
		if fn == nil {
			o.C, o.A = c, a
			return 0
		}
		o.C, o.A = fn(math.Sqrt(d2), math.Atan2(dy, dx))
		return 0
	})
}

// Ring draws a ring from radius r - w to r.
func (p *Painter) Ring(cx, cy, r, w float64, c uint32, a float64) {
	p.ring(cx, cy, r, w, c, a, nil)
}

// RingFn draws a ring from radius r - w to r whose color and alpha are fn(v, angle): v 0 at the inner edge to 1 at the outer.
func (p *Painter) RingFn(cx, cy, r, w float64, fn func(v, ang float64) (uint32, float64)) {
	p.ring(cx, cy, r, w, 0, 0, fn)
}

func (p *Painter) ring(cx, cy, r, w float64, c uint32, a float64, fn func(v, ang float64) (uint32, float64)) {
	r0 := math.Max(0, r-w)
	p.Shade(cx-r, cy-r, cx+r, cy+r, func(x, y float64, o *Out) uint32 {
		dx, dy := x-cx, y-cy
		d2 := dx*dx + dy*dy
		if d2 > r*r || d2 < r0*r0 {
			return 0
		}
		if fn == nil {
			o.C, o.A = c, a
			return 0
		}
		v := 1.0
		if w > 0 {
			den := r - r0
			if den == 0 {
				den = 1
			}
			v = (math.Sqrt(d2) - r0) / den
		}
		o.C, o.A = fn(v, math.Atan2(dy, dx))
		return 0
	})
}

// Arc draws the band of a ring from angle a0 to a1 (radians, a1 > a0), radius r - w to r: the slash and smear shape.
func (p *Painter) Arc(cx, cy, r, w, a0, a1 float64, c uint32, a float64) {
	p.arc(cx, cy, r, w, nil, a0, a1, c, a, nil)
}

// ArcFn draws the band Arc draws, its color and alpha fn(u, v): u 0 at a0 to 1 at a1, v 0 at the inner edge to 1 at the outer.
func (p *Painter) ArcFn(cx, cy, r, w, a0, a1 float64, fn func(u, v float64) (uint32, float64)) {
	p.arc(cx, cy, r, w, nil, a0, a1, 0, 0, fn)
}

// ArcTaperFn draws the band Arc draws with its width a function of u, for a crescent that tapers at both ends, colored by fn as ArcFn colors it.
func (p *Painter) ArcTaperFn(cx, cy, r float64, w func(u float64) float64, a0, a1 float64, fn func(u, v float64) (uint32, float64)) {
	p.arc(cx, cy, r, 0, w, a0, a1, 0, 0, fn)
}

func (p *Painter) arc(cx, cy, r, w float64, wf func(u float64) float64, a0, a1 float64, c uint32, a float64, fn func(u, v float64) (uint32, float64)) {
	span := a1 - a0
	p.Shade(cx-r, cy-r, cx+r, cy+r, func(x, y float64, o *Out) uint32 {
		dx, dy := x-cx, y-cy
		d := math.Sqrt(dx*dx + dy*dy)
		if d > r {
			return 0
		}
		ang := math.Atan2(dy, dx) - a0
		ang -= math.Floor(ang/(math.Pi*2)) * math.Pi * 2
		if ang > span {
			return 0
		}
		u := ang / span
		ww := w
		if wf != nil {
			ww = wf(u)
		}
		if ww <= 0 || d < r-ww {
			return 0
		}
		if fn == nil {
			o.C, o.A = c, a
			return 0
		}
		o.C, o.A = fn(u, (d-(r-ww))/ww)
		return 0
	})
}

// Rect draws a filled rectangle.
func (p *Painter) Rect(x, y, w, h float64, c uint32, a float64) {
	if p.SN == 0 {
		X0, Y0, X1, Y1 := Round(x), Round(y), Round(x+w), Round(y+h)
		for yy := Y0; yy < Y1; yy++ {
			for xx := X0; xx < X1; xx++ {
				p.Put(p.CellX(xx, yy), p.CellY(xx, yy), c, a)
			}
		}
		return
	}
	p.Shade(x, y, x+w, y+h, func(lx, ly float64, o *Out) uint32 {
		if lx < x || ly < y || lx >= x+w || ly >= y+h {
			return 0
		}
		o.C, o.A = c, a
		return 0
	})
}

// Poly draws a filled polygon from points (even-odd rule).
func (p *Painter) Poly(pts [][2]float64, c uint32, a float64) { p.poly(pts, c, a, nil) }

// PolyFn draws a filled polygon whose color and alpha are fn(x, y) of the local point.
func (p *Painter) PolyFn(pts [][2]float64, fn func(x, y float64) (uint32, float64)) {
	p.poly(pts, 0, 0, fn)
}

func (p *Painter) poly(pts [][2]float64, c uint32, a float64, fn func(x, y float64) (uint32, float64)) {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range pts {
		x, y := q[0], q[1]
		if x < x0 {
			x0 = x
		}
		if x > x1 {
			x1 = x
		}
		if y < y0 {
			y0 = y
		}
		if y > y1 {
			y1 = y
		}
	}
	n := len(pts)
	p.Shade(x0, y0, x1, y1, func(x, y float64, o *Out) uint32 {
		inside := false
		for i, j := 0, n-1; i < n; j, i = i, i+1 {
			xi, yi, xj, yj := pts[i][0], pts[i][1], pts[j][0], pts[j][1]
			if (yi > y) != (yj > y) && x < ((xj-xi)*(y-yi))/(yj-yi)+xi {
				inside = !inside
			}
		}
		if !inside {
			return 0
		}
		if fn == nil {
			o.C, o.A = c, a
			return 0
		}
		o.C, o.A = fn(x, y)
		return 0
	})
}

// Tri draws a filled triangle.
func (p *Painter) Tri(x0, y0, x1, y1, x2, y2 float64, c uint32, a float64) {
	p.Poly([][2]float64{{x0, y0}, {x1, y1}, {x2, y2}}, c, a)
}

// Glow adds a radial glow to the light buffer: k at the center, falling to 0 at radius r.
func (p *Painter) Glow(x, y, r float64, c uint32, k float64) {
	if r < 1 || k <= 0.004 {
		return
	}
	cx, cy := p.ToX(x, y), p.ToY(x, y)
	x0, x1, y0, y1 := int(math.Floor(cx-r)), int(math.Ceil(cx+r)), int(math.Floor(cy-r)), int(math.Ceil(cy+r))
	for sy := y0; sy <= y1; sy++ {
		dy := (float64(sy) + 0.5 - cy) / r
		for sx := x0; sx <= x1; sx++ {
			dx := (float64(sx) + 0.5 - cx) / r
			d := dx*dx + dy*dy
			if d >= 1 {
				continue
			}
			f := 1 - math.Sqrt(d)
			p.Add(sx, sy, c, f*f*k)
		}
	}
}

// GlowLine adds a glow around a segment (a soft beam of light), k on the segment falling to 0 at distance r.
func (p *Painter) GlowLine(x0, y0, x1, y1, r float64, c uint32, k float64) {
	if r < 1 || k <= 0.004 {
		return
	}
	ax, ay, bx, by := p.ToX(x0, y0), p.ToY(x0, y0), p.ToX(x1, y1), p.ToY(x1, y1)
	dx, dy := bx-ax, by-ay
	L2 := dx*dx + dy*dy
	if L2 == 0 {
		L2 = 1e-9
	}
	X0, X1 := int(math.Floor(math.Min(ax, bx)-r)), int(math.Ceil(math.Max(ax, bx)+r))
	Y0, Y1 := int(math.Floor(math.Min(ay, by)-r)), int(math.Ceil(math.Max(ay, by)+r))
	for sy := Y0; sy <= Y1; sy++ {
		for sx := X0; sx <= X1; sx++ {
			px, py := float64(sx)+0.5, float64(sy)+0.5
			u := Clamp(((px-ax)*dx + (py-ay)*dy) / L2)
			ex, ey := ax+dx*u-px, ay+dy*u-py
			d := math.Sqrt(ex*ex+ey*ey) / r
			if d >= 1 {
				continue
			}
			f := 1 - d
			p.Add(sx, sy, c, f*f*k)
		}
	}
}

// GlowRing adds a glow hugging a circle of radius R (a shockwave's light), falling off over width r.
func (p *Painter) GlowRing(x, y, R, r float64, c uint32, k float64) {
	if r < 1 || k <= 0.004 {
		return
	}
	cx, cy, E := p.ToX(x, y), p.ToY(x, y), R+r
	for sy := int(math.Floor(cy - E)); sy <= int(math.Ceil(cy+E)); sy++ {
		for sx := int(math.Floor(cx - E)); sx <= int(math.Ceil(cx+E)); sx++ {
			d := math.Abs(Hypot(float64(sx)+0.5-cx, float64(sy)+0.5-cy)-R) / r
			if d >= 1 {
				continue
			}
			f := 1 - d
			p.Add(sx, sy, c, f*f*k)
		}
	}
}

// SpriteOpts are how Sprite draws: Scale (0 is 1), Flip (mirror left-right), FlipY (upside down) and Tint (draw every pixel in one color; 0 draws each in its own).
type SpriteOpts struct {
	Scale       float64
	Flip, FlipY bool
	Tint        uint32
}

// Sprite draws a little sprite from strings, rows like "..##.." and ".#oo#.", whose top-left is at (x, y) and whose alpha is a: pal maps characters to colors, and "." and " " are empty, as is a character pal leaves out.
func (p *Painter) Sprite(rows []string, pal map[rune]uint32, x, y, a float64, o SpriteOpts) {
	k := o.Scale
	if k == 0 {
		k = 1
	}
	W := 0
	if len(rows) > 0 {
		W = utf8.RuneCountInString(rows[0])
	}
	for r, row := range rows {
		q := -1
		for _, ch := range row {
			q++
			if ch == '.' || ch == ' ' {
				continue
			}
			c := o.Tint
			if c == 0 {
				c = pal[ch]
			}
			if c == 0 {
				continue
			}
			col, rw := float64(q), float64(r)
			if o.Flip {
				col = float64(W - 1 - q)
			}
			if o.FlipY {
				rw = float64(len(rows) - 1 - r)
			}
			if k == 1 {
				p.Dot(x+col, y+rw, c, a)
			} else {
				p.Rect(x+col*k, y+rw*k, k, k, c, a)
			}
		}
	}
}

// Align is where text sits on its x.
type Align uint8

const (
	// AlignLeft starts the text at x.
	AlignLeft Align = iota
	// AlignCenter centers the text on x.
	AlignCenter
	// AlignRight ends the text at x.
	AlignRight
)

// TextOpts are how Text draws: Font ("mini", 3x5, when empty, or "big", 5x7), Scale (0 is 1), Align, and an Outline and a Shadow color (0 for none).
type TextOpts struct {
	Font    string
	Scale   float64
	Align   Align
	Outline uint32
	Shadow  uint32
}

func (o TextOpts) font() string {
	if o.Font == "" {
		return "mini"
	}
	return o.Font
}

func (o TextOpts) scale() float64 {
	if o.Scale == 0 {
		return 1
	}
	return o.Scale
}

// Text draws pixel text in color c at alpha a; (x, y) is the top of the text, at its left, center or right by o.Align.
func (p *Painter) Text(str string, x, y float64, c uint32, a float64, o TextOpts) {
	p.text(str, x, y, c, nil, a, o)
}

// TextFn draws pixel text as Text does, each font pixel colored fn(gx, gy) by its place in the unscaled text.
func (p *Painter) TextFn(str string, x, y float64, fn func(gx, gy int) uint32, a float64, o TextOpts) {
	p.text(str, x, y, 0, fn, a, o)
}

func (p *Painter) text(str string, x, y float64, c uint32, fn func(gx, gy int) uint32, a float64, o TextOpts) {
	font, k := o.font(), o.scale()
	w := float64(TextWidth(str, font)) * k
	left := x
	switch o.Align {
	case AlignCenter:
		left = x - w/2
	case AlignRight:
		left = x - w
	}
	left, top := Round(left), Round(y)
	if o.Outline != 0 {
		rasterText(str, font, func(gx, gy int) {
			p.Rect(left+float64(gx)*k-1, top+float64(gy)*k-1, k+2, k+2, o.Outline, a)
		})
	}
	if o.Shadow != 0 {
		rasterText(str, font, func(gx, gy int) {
			p.Rect(left+float64(gx)*k+1, top+float64(gy)*k+1, k, k, o.Shadow, a)
		})
	}
	rasterText(str, font, func(gx, gy int) {
		cc := c
		if fn != nil {
			cc = fn(gx, gy)
		}
		p.Rect(left+float64(gx)*k, top+float64(gy)*k, k, k, cc, a)
	})
}

// Measure returns the size of text in pixels: its width and height.
func (p *Painter) Measure(str string, o TextOpts) (w, h float64) {
	k := o.scale()
	return float64(TextWidth(str, o.font())) * k, float64(Fonts[o.font()].H) * k
}

// Mask is a w x h stencil: a pixel is in it where Data is not 0.
type Mask struct {
	W, H int
	Data []uint8
}

func (m *Mask) at(mx, my int) bool {
	return mx >= 0 && my >= 0 && mx < m.W && my < m.H && m.Data[my*m.W+mx] != 0
}

// Mask fills a mask with its top-left at (x, y).
func (p *Painter) Mask(m *Mask, x, y float64, c uint32, a float64) {
	for my := 0; my < m.H; my++ {
		for mx := 0; mx < m.W; mx++ {
			if m.Data[my*m.W+mx] != 0 {
				p.Dot(x+float64(mx), y+float64(my), c, a)
			}
		}
	}
}

// MaskFn fills a mask with its top-left at (x, y), each pixel's color and alpha fn(mx, my).
func (p *Painter) MaskFn(m *Mask, x, y float64, fn func(mx, my int) (uint32, float64)) {
	for my := 0; my < m.H; my++ {
		for mx := 0; mx < m.W; mx++ {
			if m.Data[my*m.W+mx] != 0 {
				c, a := fn(mx, my)
				p.Dot(x+float64(mx), y+float64(my), c, a)
			}
		}
	}
}

// Outline draws the 1 px outline just outside a mask.
func (p *Painter) Outline(m *Mask, x, y float64, c uint32, a float64) {
	p.outline(m, x, y, c, a, nil)
}

// OutlineFn draws the 1 px outline just outside a mask, each pixel's color and alpha fn(mx, my) (which may be -1 or the mask's size, just outside it).
func (p *Painter) OutlineFn(m *Mask, x, y float64, fn func(mx, my int) (uint32, float64)) {
	p.outline(m, x, y, 0, 0, fn)
}

func (p *Painter) outline(m *Mask, x, y float64, c uint32, a float64, fn func(mx, my int) (uint32, float64)) {
	for my := -1; my <= m.H; my++ {
		for mx := -1; mx <= m.W; mx++ {
			if !m.at(mx, my) && (m.at(mx-1, my) || m.at(mx+1, my) || m.at(mx, my-1) || m.at(mx, my+1)) {
				if fn != nil {
					c, a = fn(mx, my)
				}
				p.Dot(x+float64(mx), y+float64(my), c, a)
			}
		}
	}
}
