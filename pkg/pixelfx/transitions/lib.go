// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts glint() from Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez.

package transitions

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the screen transitions: a 320 x 180 screen with its origin at the top-left. Each one covers the screen in o.color by about 45% of its duration, holds, then uncovers so the last frame is empty.

const (
	// W is the screen width in pixels.
	W = 320
	// H is the screen height in pixels.
	H = 180
)

var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// screenMeta returns the Meta every transition shares, with color and seed defaults overridden by extra options in JavaScript spread order.
func screenMeta(name, desc string, tags []string, duration float64, extra ...pixelfx.Option) pixelfx.Meta {
	allTags := make([]string, 0, len(tags)+3)
	allTags = append(allTags, "transition", "screen")
	allTags = append(allTags, tags...)
	allTags = append(allTags, "one-shot")

	opts := []pixelfx.Option{
		{Name: "color", Default: pixelfx.Color("#0b0a10")},
		{Name: "seed", Default: pixelfx.Num(1)},
	}
	for _, opt := range extra {
		replaced := false
		for i := range opts {
			if opts[i].Name == opt.Name {
				opts[i] = opt
				replaced = true
				break
			}
		}
		if !replaced {
			opts = append(opts, opt)
		}
	}
	return pixelfx.Meta{
		Name:     name,
		Desc:     desc,
		Tags:     allTags,
		Size:     [2]int{W, H},
		Origin:   [2]int{0, 0},
		Duration: duration,
		Loop:     false,
		FPS:      30,
		BG:       "sky",
		Options:  opts,
	}
}

// clipTo returns a painter clipped to the 320 x 180 screen in local space.
func clipTo(p *pixelfx.Painter) *pixelfx.Painter {
	return p.Clipped(0, 0, W, H)
}

func unrotated(p *pixelfx.Painter) bool {
	return p.SN == 0 && p.CS == 1
}

func col0(p *pixelfx.Painter) int {
	if p.FX > 0 {
		return int(math.Floor(p.TX + 0.5))
	}
	return int(math.Floor(p.TX - 0.5))
}

// rect fills an opaque rectangle, mapping each row to the screen once when unrotated.
func rect(p *pixelfx.Painter, x, y, w, h float64, c uint32) {
	if !unrotated(p) {
		p.Rect(x, y, w, h, c, 1)
		return
	}
	X0, X1 := int(pixelfx.Round(x)), int(pixelfx.Round(x+w))
	Y0, Y1 := int(pixelfx.Round(y)), int(pixelfx.Round(y+h))
	fx, b := int(p.FX), col0(p)
	for yy := Y0; yy < Y1; yy++ {
		sy := int(math.Floor(p.TY + float64(yy) + 0.5))
		for xx := X0; xx < X1; xx++ {
			p.Put(b+fx*xx, sy, c, 1)
		}
	}
}

// fillConvex fills a convex polygon row by row, clipped to the screen, with a solid color c at alpha a.
func fillConvex(p *pixelfx.Painter, pts [][2]float64, c uint32, a float64) {
	fillConvexDraw(p, pts, c, nil, a)
}

// fillConvexFn fills a convex polygon row by row, clipped to the screen, colored by fn(x, y) (0 skips the pixel) at alpha a.
func fillConvexFn(p *pixelfx.Painter, pts [][2]float64, fn func(x, y float64) uint32, a float64) {
	fillConvexDraw(p, pts, 0, fn, a)
}

func fillConvexDraw(p *pixelfx.Painter, pts [][2]float64, c uint32, fn func(x, y float64) uint32, a float64) {
	y0, y1 := math.Inf(1), math.Inf(-1)
	for _, q := range pts {
		if q[1] < y0 {
			y0 = q[1]
		}
		if q[1] > y1 {
			y1 = q[1]
		}
	}
	r0 := int(math.Max(0, math.Ceil(y0-0.5)))
	r1 := int(math.Min(H-1, math.Floor(y1-0.5)))
	n := len(pts)
	unrot := unrotated(p)
	fx, b := int(p.FX), col0(p)
	for y := r0; y <= r1; y++ {
		yc := float64(y) + 0.5
		xl, xr := math.Inf(1), math.Inf(-1)
		for i, j := 0, n-1; i < n; j, i = i, i+1 {
			xi, yi := pts[i][0], pts[i][1]
			xj, yj := pts[j][0], pts[j][1]
			if (yi > yc) == (yj > yc) {
				continue
			}
			x := xi + ((yc-yi)*(xj-xi))/(yj-yi)
			if x < xl {
				xl = x
			}
			if x > xr {
				xr = x
			}
		}
		c0 := int(math.Max(0, math.Ceil(xl-0.5)))
		c1 := int(math.Min(W-1, math.Floor(xr-0.5)))
		if unrot {
			sy := int(math.Floor(p.TY + yc))
			for x := c0; x <= c1; x++ {
				col := c
				if fn != nil {
					col = fn(float64(x)+0.5, yc)
				}
				if col != 0 {
					p.Put(b+fx*x, sy, col, a)
				}
			}
		} else {
			for x := c0; x <= c1; x++ {
				col := c
				if fn != nil {
					col = fn(float64(x)+0.5, yc)
				}
				if col != 0 {
					p.Dot(float64(x), float64(y), col, a)
				}
			}
		}
	}
}

// halfPlane returns the part of polygon pts on the side of the line through (cx, cy) that normal (nx, ny) points to.
func halfPlane(pts [][2]float64, cx, cy, nx, ny float64) [][2]float64 {
	side := func(q [2]float64) float64 { return (q[0]-cx)*nx + (q[1]-cy)*ny }
	var out [][2]float64
	for i := range pts {
		a, b := pts[i], pts[(i+1)%len(pts)]
		sa, sb := side(a), side(b)
		if sa >= 0 {
			out = append(out, a)
		}
		if (sa >= 0) != (sb >= 0) {
			k := sa / (sa - sb)
			out = append(out, [2]float64{a[0] + (b[0]-a[0])*k, a[1] + (b[1]-a[1])*k})
		}
	}
	return out
}

// scalarField is a scalar field over the screen with inverse gradient length, vertical gradient share, range, and pixels sorted by value.
type scalarField struct {
	f, g, ny []float32
	lo, hi   float64
	order    []uint32
	sorted   []float32
	ox, oy   float64
}

func (sf *scalarField) below(v float64) int {
	a, b := 0, len(sf.sorted)
	for a < b {
		m := (a + b) >> 1
		if float64(sf.sorted[m]) < v {
			a = m + 1
		} else {
			b = m
		}
	}
	return a
}

// newField builds a scalar field over the screen from fn(x, y) at pixel centers.
func newField(fn func(x, y float64) float64, gmin, gmax float64) *scalarField {
	const n = W * H
	f, g, ny := make([]float32, n), make([]float32, n), make([]float32, n)
	lo, hi := math.Inf(1), math.Inf(-1)
	for y := range H {
		for x := range W {
			v := fn(float64(x)+0.5, float64(y)+0.5)
			f[y*W+x] = float32(v)
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
	}
	for y := range H {
		for x := range W {
			xa, xb := max(0, x-1), min(W-1, x+1)
			ya, yb := max(0, y-1), min(H-1, y+1)
			gx := (float64(f[y*W+xb]) - float64(f[y*W+xa])) / float64(xb-xa)
			gy := (float64(f[yb*W+x]) - float64(f[ya*W+x])) / float64(yb-ya)
			L := pixelfx.Hypot(gx, gy)
			g[y*W+x] = float32(1 / pixelfx.ClampTo(L, gmin, gmax))
			if L > 1e-6 {
				ny[y*W+x] = float32(gy / L)
			}
		}
	}
	order := make([]uint32, n)
	for i := range n {
		order[i] = uint32(i)
	}
	slices.SortStableFunc(order, func(a, b uint32) int {
		return cmp.Compare(f[a], f[b])
	})
	sorted := make([]float32, n)
	for i, idx := range order {
		sorted[i] = f[idx]
	}
	return &scalarField{f: f, g: g, ny: ny, lo: lo, hi: hi, order: order, sorted: sorted}
}

// pixelWriter writes whole screen pixels by index fast when unrotated, else through the painter.
type pixelWriter struct {
	p          *pixelfx.Painter
	fast       bool
	tx, ty, fx float64
	sx, sy, i0 int
}

func newPixelWriter(p *pixelfx.Painter) pixelWriter {
	if p.SN == 0 && p.CS == 1 {
		return pixelWriter{p: p, fast: true, tx: p.TX, ty: p.TY, fx: p.FX}
	}
	return pixelWriter{p: p}
}

func (pw *pixelWriter) at(i int) {
	if pw.fast {
		pw.sx = int(math.Floor(pw.tx + (float64(i%W)+0.5)*pw.fx))
		pw.sy = int(math.Floor(pw.ty + float64(i/W) + 0.5))
		return
	}
	pw.i0 = i
}

func (pw *pixelWriter) put(c uint32, a float64) {
	if pw.fast {
		pw.p.Put(pw.sx, pw.sy, c, a)
		return
	}
	pw.p.Dot(float64(pw.i0%W)+0.5, float64(pw.i0/W)+0.5, c, a)
}

func (pw *pixelWriter) add(c uint32, k float64) {
	if pw.fast {
		pw.p.Add(pw.sx, pw.sy, c, k)
		return
	}
	pw.p.Light(float64(pw.i0%W)+0.5, float64(pw.i0/W)+0.5, c, k)
}

func (pw *pixelWriter) blend(c uint32, a float64) {
	if pw.fast {
		pw.p.BlendPx(pw.sx, pw.sy, c, a)
		return
	}
	pw.p.Blend(float64(pw.i0%W)+0.5, float64(pw.i0/W)+0.5, c, a)
}

// glint draws a spinning 8-ray glint at (x, y).
func glint(p *pixelfx.Painter, x, y, L, ang float64, c1, c2 uint32, a float64) {
	if L < 0.8 || a <= 0 {
		return
	}
	for i := range 8 {
		diag := i%2 == 1
		an := ang + (float64(i)*tau)/8
		length := L
		if diag {
			length = L * 0.42
		}
		ca, sa := math.Cos(an), math.Sin(an)
		for d := 0.0; d <= length; d += 0.5 {
			u := d / length
			var c uint32
			if diag {
				if u < 0.5 {
					c = c1
				} else {
					c = c2
				}
			} else {
				switch {
				case u < 0.3:
					c = pixelfx.White
				case u < 0.65:
					c = c1
				default:
					c = c2
				}
			}
			al := 1.0
			if u >= 0.7 {
				al = 1 - (u-0.7)/0.35
			}
			p.Dot(x+ca*d, y+sa*d, c, a*al)
			if !diag && u < 0.22 && L > 4 {
				p.Dot(x+ca*d-sa, y+sa*d+ca, c1, a)
				p.Dot(x+ca*d+sa, y+sa*d-ca, c1, a)
			}
		}
	}
	p.Dot(x, y, pixelfx.White, a)
	p.Glow(x, y, 4+L*1.4, c1, 0.5*a)
	p.Glow(x, y, 2+L*0.5, pixelfx.White, 0.4*a)
}

// lifeLeft returns the smaller of life and what's left before end.
func lifeLeft(life, t, end float64) float64 {
	return math.Min(life, end-t)
}
