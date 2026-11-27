// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the noise bake and the branching bolt from Sprite Fusion's open-source fog and lightning (https://www.spritefusion.com/pixel-art-vfx/fog), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"
	"sync"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// pi and tau as float64 variables so expressions combining them with constants round step by step as JavaScript's do.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

var bayerTable = func() [16]float64 {
	var out [16]float64
	for i, v := range [16]float64{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5} {
		out[i] = (v + 0.5) / 16
	}
	return out
}()

// bayer returns the ordered-dither threshold of a local pixel (0..1): a > bayer(x, y) draws it.
func bayer(x, y float64) float64 {
	return bayerTable[((int(math.Floor(y))&3)<<2)|(int(math.Floor(x))&3)]
}

// rect is the canvas in local coordinates: x0 <= x < x1, y0 <= y < y1.
type rect struct {
	x0, y0, x1, y1, w, h int
}

func box(meta *pixelfx.Meta) rect {
	W, H := meta.Size[0], meta.Size[1]
	OX, OY := meta.Origin[0], meta.Origin[1]
	return rect{x0: -OX, y0: -OY, x1: W - OX, y1: H - OY, w: W, h: H}
}

// clipper wraps a Painter to drop color, blends and light outside the effect's canvas, exact under flip and rotation.
type clipper struct {
	p                  *pixelfx.Painter
	b                  rect
	sx0, sx1, sy0, sy1 int
	rot                bool
}

func clip(p *pixelfx.Painter, meta *pixelfx.Meta) *clipper {
	B := box(meta)
	q := &clipper{p: p, b: B, rot: p.SN != 0}
	if !q.rot {
		a, b := p.ToX(float64(B.x0), 0), p.ToX(float64(B.x1), 0)
		q.sx0 = int(pixelfx.Round(math.Min(a, b)))
		q.sx1 = int(pixelfx.Round(math.Max(a, b)))
		q.sy0 = int(pixelfx.Round(p.ToY(0, float64(B.y0))))
		q.sy1 = int(pixelfx.Round(p.ToY(0, float64(B.y1))))
	}
	return q
}

func (q *clipper) ok(sx, sy int) bool {
	if !q.rot {
		return sx >= q.sx0 && sx < q.sx1 && sy >= q.sy0 && sy < q.sy1
	}
	lx := q.p.FromX(float64(sx)+0.5, float64(sy)+0.5)
	ly := q.p.FromY(float64(sx)+0.5, float64(sy)+0.5)
	return lx >= float64(q.b.x0) && lx < float64(q.b.x1) && ly >= float64(q.b.y0) && ly < float64(q.b.y1)
}

func (q *clipper) put(sx, sy int, c uint32, a float64) {
	if q.ok(sx, sy) {
		q.p.Put(sx, sy, c, a)
	}
}

func (q *clipper) add(sx, sy int, c uint32, k float64) {
	if q.ok(sx, sy) {
		q.p.Add(sx, sy, c, k)
	}
}

func (q *clipper) blendPx(sx, sy int, c uint32, a float64) {
	if q.ok(sx, sy) {
		q.p.BlendPx(sx, sy, c, a)
	}
}

func (q *clipper) px(x, y float64, c uint32) {
	q.put(q.p.CellX(x, y), q.p.CellY(x, y), c, 1)
}

func (q *clipper) dot(x, y float64, c uint32, a float64) {
	if a > 0 {
		q.put(q.p.CellX(x, y), q.p.CellY(x, y), c, a)
	}
}

func (q *clipper) blend(x, y float64, c uint32, a float64) {
	q.blendPx(q.p.CellX(x, y), q.p.CellY(x, y), c, a)
}

func (q *clipper) light(x, y float64, c uint32, k float64) {
	q.add(q.p.CellX(x, y), q.p.CellY(x, y), c, k)
}

// glow is the painter's glow with its box cut to the canvas first, in local space as pixelfx's weather/_lib.js does.
func (q *clipper) glow(x, y, r float64, c uint32, k float64) {
	if r < 1 || k <= 0.004 {
		return
	}
	x0 := max(q.b.x0, int(math.Floor(x-r)))
	x1 := min(q.b.x1-1, int(math.Ceil(x+r)))
	y0 := max(q.b.y0, int(math.Floor(y-r)))
	y1 := min(q.b.y1-1, int(math.Ceil(y+r)))
	for py := y0; py <= y1; py++ {
		dy := (float64(py) + 0.5 - y) / r
		for px := x0; px <= x1; px++ {
			dx := (float64(px) + 0.5 - x) / r
			d := dx*dx + dy*dy
			if d >= 1 {
				continue
			}
			f := 1 - math.Sqrt(d)
			q.light(float64(px), float64(py), c, f*f*k)
		}
	}
}

func (q *clipper) line(x0, y0, x1, y1 float64, c uint32, a float64) {
	q.drawLine(x0, y0, x1, y1, c, a, nil)
}

func (q *clipper) lineFn(x0, y0, x1, y1 float64, fn func(u float64) (uint32, float64)) {
	q.drawLine(x0, y0, x1, y1, 0, 0, fn)
}

func (q *clipper) drawLine(x0, y0, x1, y1 float64, c uint32, a float64, fn func(u float64) (uint32, float64)) {
	sx, sy := q.p.CellX(x0, y0), q.p.CellY(x0, y0)
	ex, ey := q.p.CellX(x1, y1), q.p.CellY(x1, y1)
	dx := ex - sx
	if dx < 0 {
		dx = -dx
	}
	dy := ey - sy
	if dy > 0 {
		dy = -dy
	}
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
			q.put(sx, sy, cc, aa)
		} else {
			q.put(sx, sy, c, a)
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

func (q *clipper) polyline(pts [][2]float64, c uint32, a float64) {
	for i := 1; i < len(pts); i++ {
		q.line(pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1], c, a)
	}
}

func (q *clipper) shade(x0, y0, x1, y1 float64, fn func(x, y float64, o *pixelfx.Out) uint32) {
	var o pixelfx.Out
	q.p.Each(x0, y0, x1, y1, func(lx, ly float64, sx, sy int) {
		o = pixelfx.Out{A: 1}
		c := fn(lx, ly, &o)
		if c == 0 {
			c = o.C
		}
		if c != 0 {
			q.put(sx, sy, c, o.A)
		}
		if o.K > 0 {
			l := o.L
			if l == 0 {
				l = c
			}
			q.add(sx, sy, l, o.K)
		}
	})
}

func (q *clipper) circle(cx, cy, r float64, c uint32, a float64) {
	if r < 0.6 {
		q.dot(cx, cy, c, a)
		return
	}
	q.shade(cx-r, cy-r, cx+r, cy+r, func(x, y float64, o *pixelfx.Out) uint32 {
		dx, dy := x-cx, y-cy
		if dx*dx+dy*dy > r*r {
			return 0
		}
		o.C, o.A = c, a
		return 0
	})
}

func (q *clipper) disc(cx, cy, r float64, c uint32, a float64) {
	if r < 0.5 {
		q.blend(cx, cy, c, a)
		return
	}
	q.p.Each(cx-r, cy-r, cx+r, cy+r, func(x, y float64, sx, sy int) {
		dx, dy := x-cx, y-cy
		if dx*dx+dy*dy > r*r {
			return
		}
		q.blendPx(sx, sy, c, a)
	})
}

var (
	sparkleCross = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	sparkleDiag  = [4][2]float64{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
)

// sparkle draws pixelfx.Sparkle through a clipped painter so its pixels and glow stay clipped and match weather/_lib.js's glow.
func sparkle(q *clipper, x, y, arm float64, c, tip uint32, k float64) {
	n := pixelfx.Round(arm)
	if n < 1 {
		return
	}
	X, Y := math.Floor(x), math.Floor(y)
	for d := 1.0; d <= n; d++ {
		cc := c
		if d > n*0.6 {
			cc = tip
		}
		for _, off := range sparkleCross {
			q.px(X+off[0]*d, Y+off[1]*d, cc)
		}
	}
	if n >= 3 {
		for _, off := range sparkleDiag {
			q.px(X+off[0], Y+off[1], c)
		}
	}
	if n >= 6 {
		for _, off := range sparkleDiag {
			q.px(X+off[0]*2, Y+off[1]*2, tip)
		}
	}
	q.px(X, Y, pixelfx.White)
	q.glow(x, y, 4+n*2, c, k)
}

// shader is the minimal interface shared by clipper and pixelRecorder for static scatter/litter caching.
type shader interface {
	shade(x0, y0, x1, y1 float64, fn func(x, y float64, o *pixelfx.Out) uint32)
}

type recordedPixel struct {
	x, y float64
	c    uint32
	a    float64
}

type pixelRecorder struct {
	px []recordedPixel
}

func (rec *pixelRecorder) shade(x0, y0, x1, y1 float64, fn func(x, y float64, o *pixelfx.Out) uint32) {
	var o pixelfx.Out
	for y := math.Floor(y0); y <= math.Ceil(y1); y++ {
		for x := math.Floor(x0); x <= math.Ceil(x1); x++ {
			o = pixelfx.Out{A: 1}
			c := fn(x+0.5, y+0.5, &o)
			if c == 0 {
				c = o.C
			}
			if c != 0 {
				rec.px = append(rec.px, recordedPixel{x: x + 0.5, y: y + 0.5, c: c, a: o.A})
			}
		}
	}
}

type passOut struct {
	c uint32
	a float64
	b bool
	l uint32
	k float64
}

// pass is a per-pixel shader over the canvas rows y0..y1 (local, clipped to the canvas).
func pass(p *pixelfx.Painter, meta *pixelfx.Meta, y0, y1 float64, fn func(x, y float64, out *passOut)) {
	B := box(meta)
	iy0 := max(B.y0, int(math.Floor(y0)))
	iy1 := min(B.y1-1, int(math.Ceil(y1)))
	if iy0 > iy1 {
		return
	}
	var o passOut
	if p.SN != 0 {
		q := clip(p, meta)
		for y := iy0; y <= iy1; y++ {
			for x := B.x0; x < B.x1; x++ {
				o = passOut{a: 1}
				fn(float64(x)+0.5, float64(y)+0.5, &o)
				if o.c != 0 {
					if o.b {
						q.blend(float64(x), float64(y), o.c, o.a)
					} else {
						q.dot(float64(x), float64(y), o.c, o.a)
					}
				}
				if o.k > 0 {
					l := o.l
					if l == 0 {
						l = o.c
					}
					q.light(float64(x), float64(y), l, o.k)
				}
			}
		}
		return
	}
	C, L, W := p.C, p.L, p.W
	fade, lk, fx, tx := p.Fade, p.LightK*p.Fade, p.FX, p.TX
	for y := iy0; y <= iy1; y++ {
		sy := int(math.Floor(p.TY + float64(y) + 0.5))
		ry := sy - p.OY
		if ry < 0 || ry >= p.H {
			continue
		}
		row := ry * W
		by := (sy & 3) << 2
		sxf := tx + (float64(B.x0)+0.5)*fx
		for x := B.x0; x < B.x1; x, sxf = x+1, sxf+fx {
			sx := int(math.Floor(sxf))
			rx := sx - p.OX
			if rx < 0 || rx >= W {
				continue
			}
			o = passOut{a: 1}
			fn(float64(x)+0.5, float64(y)+0.5, &o)
			i := row + rx
			if o.c != 0 {
				a := o.a * fade
				if o.b {
					if a > 0.004 {
						d := C[i]
						da := float64(d>>24) / 255
						oa := a + da*(1-a)
						k := a / oa
						j := 1 - k
						c := o.c
						r := uint32(pixelfx.ToInt32(float64(c&255)*k+float64(d&255)*j) & 255)
						g := uint32(pixelfx.ToInt32(float64((c>>8)&255)*k+float64((d>>8)&255)*j) & 255)
						b := uint32(pixelfx.ToInt32(float64((c>>16)&255)*k+float64((d>>16)&255)*j) & 255)
						C[i] = uint32(pixelfx.ToInt32(oa*255)&255)<<24 | b<<16 | g<<8 | r
					}
				} else if a >= 1 || a > bayerTable[by|(sx&3)] {
					C[i] = o.c
				}
			}
			if o.k > 0 {
				k := o.k * lk
				if k > 0.004 {
					c := o.l
					if c == 0 {
						c = o.c
					}
					d := L[i]
					r := uint32(pixelfx.ToInt32(math.Min(255, float64(d&255)+float64(c&255)*k)))
					g := uint32(pixelfx.ToInt32(math.Min(255, float64((d>>8)&255)+float64((c>>8)&255)*k)))
					b := uint32(pixelfx.ToInt32(math.Min(255, float64((d>>16)&255)+float64((c>>16)&255)*k)))
					L[i] = 255<<24 | b<<16 | g<<8 | r
				}
			}
		}
	}
}

type rowBuf struct {
	w int
	c []uint32
	a []float32
	b []uint8
	l []uint32
	k []float32
}

func (R *rowBuf) ensure(w int) {
	if R.w == w {
		return
	}
	R.w = w
	R.c = make([]uint32, w)
	R.a = make([]float32, w)
	R.b = make([]uint8, w)
	R.l = make([]uint32, w)
	R.k = make([]float32, w)
}

func (R *rowBuf) fillC(c uint32) {
	for i := range R.c {
		R.c[i] = c
	}
}

func (R *rowBuf) fillA(a float32) {
	for i := range R.a {
		R.a[i] = a
	}
}

func (R *rowBuf) fillB(b uint8) {
	for i := range R.b {
		R.b[i] = b
	}
}

// rows is the fast whole-sky shader: fn(y, R) fills one canvas row at a time into R.
func rows(p *pixelfx.Painter, meta *pixelfx.Meta, R *rowBuf, y0, y1 float64, fn func(y float64, R *rowBuf)) {
	B := box(meta)
	w := B.w
	R.ensure(w)
	iy0 := max(B.y0, int(math.Floor(y0)))
	iy1 := min(B.y1-1, int(math.Ceil(y1)))
	rot := p.SN != 0
	var q *clipper
	if rot {
		q = clip(p, meta)
	}
	C, L, W := p.C, p.L, p.W
	fade, lk, fx := p.Fade, p.LightK*p.Fade, p.FX
	for y := iy0; y <= iy1; y++ {
		clear(R.c)
		clear(R.k)
		for i := range R.a {
			R.a[i] = 1
		}
		clear(R.b)
		fn(float64(y)+0.5, R)
		if rot {
			for i := 0; i < w; i++ {
				if R.c[i] != 0 {
					if R.b[i] != 0 {
						q.blend(float64(B.x0+i), float64(y), R.c[i], float64(R.a[i]))
					} else {
						q.dot(float64(B.x0+i), float64(y), R.c[i], float64(R.a[i]))
					}
				}
				if R.k[i] > 0 {
					lc := R.l[i]
					if lc == 0 {
						lc = R.c[i]
					}
					q.light(float64(B.x0+i), float64(y), lc, float64(R.k[i]))
				}
			}
			continue
		}
		sy := int(math.Floor(p.TY + float64(y) + 0.5))
		ry := sy - p.OY
		if ry < 0 || ry >= p.H {
			continue
		}
		row := ry * W
		by := (sy & 3) << 2
		sxf := p.TX + (float64(B.x0)+0.5)*fx
		for i := 0; i < w; i, sxf = i+1, sxf+fx {
			c, kk := R.c[i], float64(R.k[i])
			if c == 0 && !(kk > 0) {
				continue
			}
			sx := int(math.Floor(sxf))
			rx := sx - p.OX
			if rx < 0 || rx >= W {
				continue
			}
			j := row + rx
			if c != 0 {
				a := float64(R.a[i]) * fade
				if R.b[i] != 0 {
					if a > 0.004 {
						d := C[j]
						da := float64(d>>24) / 255
						oa := a + da*(1-a)
						k := a / oa
						m := 1 - k
						r := uint32(pixelfx.ToInt32(float64(c&255)*k+float64(d&255)*m) & 255)
						g := uint32(pixelfx.ToInt32(float64((c>>8)&255)*k+float64((d>>8)&255)*m) & 255)
						b := uint32(pixelfx.ToInt32(float64((c>>16)&255)*k+float64((d>>16)&255)*m) & 255)
						C[j] = uint32(pixelfx.ToInt32(oa*255)&255)<<24 | b<<16 | g<<8 | r
					}
				} else if a >= 1 || a > bayerTable[by|(sx&3)] {
					C[j] = c
				}
			}
			if kk > 0 {
				k := kk * lk
				if k > 0.004 {
					lc := R.l[i]
					if lc == 0 {
						lc = c
					}
					d := L[j]
					r := uint32(pixelfx.ToInt32(math.Min(255, float64(d&255)+float64(lc&255)*k)))
					g := uint32(pixelfx.ToInt32(math.Min(255, float64((d>>8)&255)+float64((lc>>8)&255)*k)))
					b := uint32(pixelfx.ToInt32(math.Min(255, float64((d>>16)&255)+float64((lc>>16)&255)*k)))
					L[j] = 255<<24 | b<<16 | g<<8 | r
				}
			}
		}
	}
}

// noiseField is a baked tiling fractal noise of w x h float32 cells.
type noiseField struct {
	w, h int
	d    []float32
}

type fieldKey struct {
	seed   float64
	w, h   int
	ncells int
	cells  [4]int
}

var (
	fieldsMu    sync.RWMutex
	fieldsCache = map[fieldKey]*noiseField{}
)

var (
	defaultCells = []int{4, 8, 16, 32}
	cells4_8_16  = []int{4, 8, 16}
	cells8_16_32 = []int{8, 16, 32}
)

// field returns a baked tiling fractal noise of w x h cells (powers of two), octaves of cells lattice squares across.
func field(seed float64, w, h int, cells []int) *noiseField {
	if len(cells) == 0 {
		cells = defaultCells
	}
	key := fieldKey{seed: seed, w: w, h: h, ncells: len(cells)}
	copy(key.cells[:], cells)
	fieldsMu.RLock()
	F := fieldsCache[key]
	fieldsMu.RUnlock()
	if F != nil {
		return F
	}
	d := make([]float32, w*h)
	amp, total := 0.5, 0.0
	for _, n := range cells {
		nf := float64(n)
		cs := float64(w) / nf
		nRows := math.Max(1, pixelfx.Round(float64(h)/cs))
		at := func(a, b float64) float64 {
			return pixelfx.Rand(pixelfx.Wrap(a, nf), pixelfx.Wrap(b, nRows), seed, nf)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				gx := float64(x) / cs
				gy := (float64(y) / float64(h)) * nRows
				ix, iy := math.Floor(gx), math.Floor(gy)
				u, v := gx-ix, gy-iy
				su, sv := u*u*(3-2*u), v*v*(3-2*v)
				a00, a10 := at(ix, iy), at(ix+1, iy)
				a01, a11 := at(ix, iy+1), at(ix+1, iy+1)
				top := a00 + (a10-a00)*su
				bot := a01 + (a11-a01)*su
				d[y*w+x] = float32(float64(d[y*w+x]) + (top+(bot-top)*sv)*amp)
			}
		}
		total += amp
		amp *= 0.5
	}
	for i := range d {
		d[i] = float32(float64(d[i]) / total)
	}
	F = &noiseField{w: w, h: h, d: d}
	fieldsMu.Lock()
	if existing := fieldsCache[key]; existing != nil {
		F = existing
	} else {
		fieldsCache[key] = F
	}
	fieldsMu.Unlock()
	return F
}

// sample returns F at (u, v) in its own cells, wrapping smoothly between cells.
func sample(F *noiseField, u, v float64) float64 {
	xf, yf := math.Floor(u), math.Floor(v)
	x, y := int(xf), int(yf)
	fx, fy := u-xf, v-yf
	m, n := F.w-1, F.h-1
	D := F.d
	x0, x1 := x&m, (x+1)&m
	y0, y1 := (y&n)*F.w, ((y+1)&n)*F.w
	a, b, c, e := float64(D[y0+x0]), float64(D[y0+x1]), float64(D[y1+x0]), float64(D[y1+x1])
	return a + (b-a)*fx + (c-a+(a-b-c+e)*fx)*fy
}

// trips runs falling particles for a loop of D seconds: n slots, each falling m times a loop (m in [m0, m1]).
func trips(t, D float64, n int, seed float64, m0, m1 int, fn func(u, k float64, m, i int)) {
	for i := 0; i < n; i++ {
		m := m0 + int(math.Floor(pixelfx.Rand(seed, float64(i), 72)*float64(m1-m0+1)))
		s := (t/D)*float64(m) + pixelfx.Rand(seed, float64(i), 71)
		k := math.Floor(s)
		fn(s-k, k, m, i)
	}
}

type boltPath struct {
	pts   [][2]float64
	w     float64
	depth int
}

// tree builds a branching lightning path from (x0, y0) to (x1, y1) by midpoint displacement.
func tree(R *pixelfx.Rng, x0, y0, x1, y1, jag, forks, w float64, depth int) []boltPath {
	var out []boltPath
	L := pixelfx.Hypot(x1-x0, y1-y0)
	pts := [][2]float64{{x0, y0}, {x1, y1}}
	off := L * jag
	for float64(len(pts)) < 2+L/3 {
		nxt := make([][2]float64, 0, len(pts)*2-1)
		nxt = append(nxt, pts[0])
		for i := 0; i < len(pts)-1; i++ {
			ax, ay := pts[i][0], pts[i][1]
			bx, by := pts[i+1][0], pts[i+1][1]
			l := pixelfx.Hypot(bx-ax, by-ay)
			if l == 0 {
				l = 1
			}
			o := R.Range(-off, off)
			nxt = append(nxt, [2]float64{(ax+bx)/2 + (-(by-ay)/l)*o, (ay+by)/2 + ((bx-ax)/l)*o}, [2]float64{bx, by})
		}
		pts = nxt
		off *= 0.52
	}
	out = append(out, boltPath{pts: pts, w: w, depth: depth})
	if depth < 2 {
		for i := 2; i < len(pts)-3; i++ {
			if R.Chance(forks / float64(1+depth) / math.Max(4, float64(len(pts))/6)) {
				ax, ay := pts[i][0], pts[i][1]
				j := min(len(pts)-1, i+3)
				bx, by := pts[j][0], pts[j][1]
				sgn := R.Sign()
				da := R.Range(0.35, 0.95)
				a := math.Atan2(by-ay, bx-ax) + sgn*da
				lf := R.Range(0.25, 0.55)
				l := (L - (float64(i)/float64(len(pts)))*L) * lf
				out = append(out, tree(R, ax, ay, ax+math.Cos(a)*l, ay+math.Sin(a)*l, jag*1.1, forks, w*0.55, depth+1)...)
			}
		}
	}
	return out
}

type splashColors struct {
	hot, cool, white uint32
}

// splash draws a raindrop's splash on the floor, age seconds after it lands at (x, z).
func splash(q *clipper, x, z, age, size float64, c splashColors, h float64) {
	L := 0.3 + 0.12*size
	u := age / L
	if u >= 1 {
		return
	}
	if age < 0.07 {
		q.px(x, z-1, c.hot)
		if size > 0.3 {
			dy := 0.0
			if age > 0.035 {
				dy = 1
			}
			q.px(x-1, z-1-dy, c.hot)
			q.px(x+1, z-1-dy, c.hot)
		}
		if size > 0.65 && age > 0.03 {
			q.px(x-2, z-2, c.cool)
			q.px(x+2, z-2, c.cool)
		}
	}
	rx := 1 + (1.2+3.6*size)*math.Sqrt(u)
	ry := math.Max(0.5, rx*0.26)
	n := int(math.Ceil(rx * 6.5))
	for j := 0; j < n; j++ {
		a := (float64(j) / float64(n)) * tau
		s := math.Sin(a)
		if s < -0.25 {
			continue
		}
		col := c.cool
		if u < 0.3 && s > 0 {
			col = c.hot
		}
		var alpha float64
		if u < 0.45 {
			if s > 0.2 {
				alpha = 1
			} else {
				alpha = 0.7
			}
		} else {
			if s > 0.2 {
				alpha = (1 - u) * 1.6
			} else {
				alpha = (1 - u) * 1.6 * 0.6
			}
		}
		q.dot(x+math.Cos(a)*rx, z+s*ry, col, alpha)
	}
	nd := 2
	if size > 0.55 {
		nd = 3
	}
	H := math.Floor(h * 1e6)
	for d := 0; d < nd; d++ {
		vx := (pixelfx.Rand(H, float64(d), 1) - 0.5) * 2 * (25 + 35*size)
		vy := -(55 + pixelfx.Rand(H, float64(d), 2)*70*(0.5+size))
		y := z - 1 + vy*age + 475*age*age
		if y > z-0.5 || age > 0.34 {
			continue
		}
		xx := x + vx*age
		col := c.cool
		if age < 0.12 {
			col = c.hot
		}
		q.px(xx, y, col)
		if vy+950*age < -40 {
			q.dot(xx-vx*0.012, y+1, c.cool, 0.7)
		}
	}
}

// crown draws a crisp splash on a floor line, age seconds after a drop lands at (x, z). Returns false once it's over.
func crown(q *clipper, x, z, age, size float64, c splashColors, h float64) bool {
	L := 0.22 + 0.14*size
	u := age / L
	X, Z := pixelfx.Round(x), pixelfx.Round(z)
	if u >= 1 {
		return false
	}
	if age < 0.1 {
		up := 2
		if age < 0.033 {
			up = 1
		} else if age >= 0.067 && size > 0.5 {
			up = 3
		}
		col := c.hot
		if age < 0.05 {
			col = c.white
		}
		q.px(X, Z-1, col)
		lim := 1
		if size > 0.5 {
			lim = 3
		}
		for d := 1; d <= min(up, lim); d++ {
			dc := c.cool
			if d == 1 {
				dc = c.hot
			}
			dy := 0.0
			if d > 1 {
				dy = float64(d - 1)
			}
			q.px(X-float64(d), Z-1-dy, dc)
			q.px(X+float64(d), Z-1-dy, dc)
		}
	}
	if u > 0.15 {
		w := int(pixelfx.Round(1 + (1.5+3.5*size)*math.Sqrt(u)))
		gap := -1
		if u > 0.55 {
			gap = max(0, w-2)
		}
		for d := -w; d <= w; d++ {
			ad := d
			if ad < 0 {
				ad = -ad
			}
			if ad > gap {
				col := c.cool
				if ad >= w-1 && u < 0.6 {
					col = c.hot
				}
				q.px(X+float64(d), Z, col)
			}
		}
	}
	nd := 1
	if size > 0.5 {
		nd = 2
	}
	H := math.Floor(h * 1e6)
	for d := 0; d < nd; d++ {
		vx := (pixelfx.Rand(H, float64(d), 1) - 0.5) * 2 * (20 + 30*size)
		vy := -(50 + pixelfx.Rand(H, float64(d), 2)*60*(0.4+size))
		y := Z - 1 + vy*age + 475*age*age
		if y < Z-0.5 && age < 0.3 {
			col := c.hot
			if age < 0.1 {
				col = c.white
			}
			q.px(x+vx*age, y, col)
		}
	}
	return true
}
