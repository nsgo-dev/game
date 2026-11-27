// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rebuilt pickups helpers (floor shadows, star glints, coins, gems and light pools) from Sprite Fusion's open-source pickup and sparkle (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// pi and tau are π and 2π as float64 variables so expressions combining them with other constants round step by step as JavaScript's do.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

var pickupInk = pixelfx.Hex("#0b0814")

// layer is a scratch layer: draw a sprite into it with its own painter (same local coordinates as the effect), give it a dark outline, then stamp it onto the frame.
type layer struct {
	f   *pixelfx.Frame
	q   *pixelfx.Painter
	tmp []uint32
}

func newLayer(w, h int) *layer {
	f := pixelfx.NewFrame(w, h, 0, 0)
	return &layer{
		f:   f,
		q:   f.Painter(),
		tmp: make([]uint32, w*h),
	}
}

// begin clears the layer and puts its top-left at local (x0, y0).
func (l *layer) begin(x0, y0 float64) *pixelfx.Painter {
	f := l.f
	f.OX = int(math.Floor(x0))
	f.OY = int(math.Floor(y0))
	clear(f.Color)
	clear(f.Light)
	return l.q.Reset().Place(0, 0, false, 0)
}

func (l *layer) idx(x, y float64) int {
	f := l.f
	i, j := int(math.Floor(x))-f.OX, int(math.Floor(y))-f.OY
	if i < 0 || j < 0 || i >= f.W || j >= f.H {
		return -1
	}
	return j*f.W + i
}

func (l *layer) get(x, y float64) uint32 {
	i := l.idx(x, y)
	if i < 0 {
		return 0
	}
	return l.f.Color[i]
}

func (l *layer) set(x, y float64, c uint32) {
	if i := l.idx(x, y); i >= 0 {
		l.f.Color[i] = c
	}
}

// fill fills each pixel from fn(x, y) at the pixel's center (x + 0.5, y + 0.5): return a color or 0.
func (l *layer) fill(x0, y0, x1, y1 float64, fn func(x, y float64) uint32) {
	for y := math.Floor(y0); y <= y1; y++ {
		for x := math.Floor(x0); x <= x1; x++ {
			if c := fn(x+0.5, y+0.5); c != 0 {
				l.set(x, y, c)
			}
		}
	}
}

// outline draws a 1 px outline around everything drawn in the layer.
func (l *layer) outline(c uint32) *layer {
	f := l.f
	C, T, w, h := f.Color, l.tmp, f.W, f.H
	clear(T)
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			k := j*w + i
			if C[k] != 0 {
				continue
			}
			if (i > 0 && C[k-1] != 0) || (i < w-1 && C[k+1] != 0) || (j > 0 && C[k-w] != 0) || (j < h-1 && C[k+w] != 0) {
				T[k] = c
			}
		}
	}
	for k := 0; k < w*h; k++ {
		if T[k] != 0 {
			C[k] = T[k]
		}
	}
	return l
}

// draw stamps the layer onto p, offset by (dx, dy), at alpha a (dithered).
func (l *layer) draw(p *pixelfx.Painter, dx, dy, a float64) *layer {
	f := l.f
	C := f.Color
	for k, c := range C {
		if c == 0 {
			continue
		}
		x := float64((k%f.W)+f.OX) + dx
		y := float64((k/f.W)+f.OY) + dy
		p.Dot(x, y, c, a)
	}
	return l
}

// pillow fills a puffy shape into layer L from a signed distance sdf(x, y) in px (negative inside).
func pillow(L *layer, x0, y0, x1, y1 float64, sdf func(x, y float64) float64, color func(b, e, x, y float64) uint32, bevel float64) {
	light := [3]float64{-0.5, -0.6, 0.62}
	L.fill(x0, y0, x1, y1, func(x, y float64) uint32 {
		d := sdf(x, y)
		if d > 0 {
			return 0
		}
		e := -d
		gx, gy := sdf(x+0.5, y)-sdf(x-0.5, y), sdf(x, y+0.5)-sdf(x, y-0.5)
		gl := pixelfx.Hypot(gx, gy)
		if gl == 0 {
			gl = 1
		}
		gx /= gl
		gy /= gl
		k := math.Max(0, 1-e/bevel) * 1.25
		sn, cs := math.Sin(k), math.Cos(k)
		return color(gx*sn*light[0]+gy*sn*light[1]+cs*light[2], e, x, y)
	})
}

// heartSdf returns the signed distance (px, roughly) to a heart of half-width w centered at (cx, cy).
func heartSdf(x, y, cx, cy, w, h float64) float64 {
	S := w / 1.14
	X, Y := (x-cx)/S, -(y-cy)/(h/1.14)+0.12
	q := X*X + Y*Y - 1
	f := q*q*q - X*X*Y*Y*Y
	gx := 6*X*q*q - 2*X*Y*Y*Y
	gy := 6*Y*q*q - 3*X*X*Y*Y
	return (f / math.Max(pixelfx.Hypot(gx, gy), 0.6)) * S
}

// blobShadow draws a soft floor shadow at x, half-width rx, strength k, on floor y.
func blobShadow(p *pixelfx.Painter, x, rx, k, y float64) {
	if rx < 0.8 || k <= 0 {
		return
	}
	for i := math.Floor(x - rx); i <= math.Ceil(x+rx); i++ {
		u := math.Abs(i+0.5-x) / rx
		if u > 1 {
			continue
		}
		a := k * (1 - u*u)
		p.Dot(i, y, pickupInk, a*1.1)
		if u < 0.6 {
			p.Dot(i, y+1, pickupInk, a*0.6)
		}
	}
}

// glint draws a four-point star glint: arms of arm px in c, dimmer tips, a white heart, its own light.
func glint(p *pixelfx.Painter, x, y, arm float64, c uint32, k float64) {
	tip := pixelfx.Scale(c, 0.8)
	n, X, Y := pixelfx.Round(arm), math.Floor(x), math.Floor(y)
	if n < 0 {
		return
	}
	for d := 1.0; d <= n; d++ {
		cc := c
		if d == n && n > 1 {
			cc = tip
		}
		p.Px(X+d, Y, cc)
		p.Px(X-d, Y, cc)
		p.Px(X, Y+d, cc)
		p.Px(X, Y-d, cc)
	}
	center := c
	if n > 0 {
		center = pixelfx.White
	}
	p.Px(X, Y, center)
	p.Glow(X+0.5, Y+0.5, 3+n*2.5, c, k)
}

// pool adds additive light in an ellipse (a pool of light on the floor): k at the center falling off to the rim.
func pool(p *pixelfx.Painter, x, y, rx, ry float64, c uint32, k float64) {
	if k <= 0.004 || rx < 1 {
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

// coin draws a coin of radius r tumbling: its face turns by spin about an axis at screen angle ax.
func coin(L *layer, p *pixelfx.Painter, x, y, r, ax, spin float64, C []uint32, white bool, a float64, snap bool) {
	X, Y := x, y
	if snap {
		X, Y = pixelfx.Round(x), pixelfx.Round(y)
	}
	cs, sn := math.Cos(spin), math.Sin(spin)
	b := math.Max(0.5, r*math.Abs(cs))
	csSign := 1.0
	if cs < 0 {
		csSign = -1
	}
	th := 1.2 * sn * csSign
	ca, sa := math.Cos(ax), math.Sin(ax)
	face, R := math.Abs(cs), math.Ceil(r)+2
	L.begin(math.Floor(X)-R, math.Floor(Y)-R)
	L.fill(math.Floor(X)-R, math.Floor(Y)-R, math.Floor(X)+R, math.Floor(Y)+R, func(px, py float64) uint32 {
		dx, dy := px-X, py-Y
		u, w := dx*ca+dy*sa, -dx*sa+dy*ca
		ef, er := pixelfx.Hypot(u/r, w/b), pixelfx.Hypot(u/r, (w-th)/b)
		if white && (ef <= 1 || er <= 1) {
			return pixelfx.White
		}
		if ef <= 1 {
			lit := -(dx*0.62 + dy*0.78) / math.Max(1, pixelfx.Hypot(dx, dy))
			dim := 0
			if face < 0.45 {
				dim = 1
			}
			if b < 1.1 {
				if lit > 0 {
					return C[6]
				}
				return C[4]
			}
			if ef > 0.72 {
				idx := 3
				if lit > 0.3 {
					idx = 6
				} else if lit > -0.35 {
					idx = 5
				}
				return C[idx-dim]
			}
			if b > 2.2 && ef > 0.42 {
				idx := 5
				if lit > 0.2 {
					idx = 4
				}
				return C[idx-dim]
			}
			if face > 0.6 && lit > 0.5 {
				return C[7]
			}
			return C[5-dim]
		}
		if er <= 1 {
			if math.Abs(w-th) < math.Abs(w) {
				return C[2]
			}
			return C[3]
		}
		return 0
	})
	L.outline(C[1]).draw(p, 0, 0, a)
}

var gemletNormals = [6][2]float64{{0, -1}, {-0.7, -0.7}, {0.7, -0.7}, {-0.6, 0.8}, {0, 1}, {0.6, 0.8}}

// gemlet draws a small cut gem of radius r turned to angle ang (radians, on screen).
func gemlet(L *layer, p *pixelfx.Painter, x, y, r, ang float64, G []uint32, white bool, a float64) {
	X, Y, ca, sa, n, R := pixelfx.Round(x), pixelfx.Round(y), math.Cos(ang), math.Sin(ang), len(G), math.Ceil(r*1.4)+2
	ct, tw, pv := r*0.75, r*0.5, r*1.3
	L.begin(X-R, Y-R)
	L.fill(X-R, Y-R, X+R-1, Y+R-1, func(px, py float64) uint32 {
		dx, dy := px-X, py-Y
		u, w := dx*ca+dy*sa, -dx*sa+dy*ca
		var facet int
		if w < 0 {
			if w < -ct || math.Abs(u) > r-((-w)/ct)*(r-tw) {
				return 0
			}
			if w < -ct*0.45 && math.Abs(u) < tw*0.95 {
				facet = 0
			} else if u < 0 {
				facet = 1
			} else {
				facet = 2
			}
		} else {
			if w > pv || math.Abs(u) > r*(1-w/pv) {
				return 0
			}
			if math.Abs(u) < 0.5 {
				facet = 4
			} else if u < 0 {
				facet = 3
			} else {
				facet = 5
			}
		}
		if white {
			return pixelfx.White
		}
		N := gemletNormals[facet]
		nx, ny := N[0]*ca-N[1]*sa, N[0]*sa+N[1]*ca
		l := -(nx*0.62 + ny*0.78)
		if facet == 0 && l > 0.3 && u < 0 {
			return pixelfx.White
		}
		switch {
		case l > 0.6:
			return G[n-1]
		case l > 0.2:
			return G[n-2]
		case l > -0.2:
			return G[n-3]
		case l > -0.6:
			return G[n-4]
		default:
			return G[n-5]
		}
	})
	L.outline(G[0]).draw(p, 0, 0, a)
}

type gemPlane struct {
	n    [3]float64
	d    float64
	kind int
}

type gemSolid struct {
	planes         []gemPlane
	r, top, bottom float64
}

// brilliant builds a round brilliant cut gem solid.
func brilliant(r, crown, table, pavilion, girdle float64, n int) gemSolid {
	planes := []gemPlane{{n: [3]float64{0, 1, 0}, d: crown + girdle/2, kind: 0}}
	cs, ps := math.Atan2(crown, r*(1-table)), math.Atan2(pavilion, r)
	for i := 0; i < n*2; i++ {
		a := (float64(i) * pi) / float64(n)
		half := i%2 != 0
		c, pv := cs, ps
		if half {
			c = math.Min(1.45, cs+0.22)
			pv = ps - 0.1
		}
		cmul, ckind := 1.0, 1
		if half {
			cmul, ckind = 1.012, 2
		}
		planes = append(planes, gemPlane{
			n:    [3]float64{math.Sin(c) * math.Cos(a), math.Cos(c), math.Sin(c) * math.Sin(a)},
			d:    r*math.Sin(c)*cmul + (girdle/2)*math.Cos(c),
			kind: ckind,
		})
		planes = append(planes, gemPlane{
			n:    [3]float64{math.Cos(a), 0, math.Sin(a)},
			d:    r,
			kind: 3,
		})
		pmul, pkind := 1.0, 4
		if half {
			pmul, pkind = 1.03, 5
		}
		planes = append(planes, gemPlane{
			n:    [3]float64{math.Sin(pv) * math.Cos(a), -math.Cos(pv), math.Sin(pv) * math.Sin(a)},
			d:    r*math.Sin(pv)*pmul + (girdle/2)*math.Cos(pv),
			kind: pkind,
		})
	}
	return gemSolid{planes: planes, r: r, top: crown + girdle/2, bottom: pavilion + girdle/2}
}

type drawGemOpts struct {
	tilt  float64
	white bool
	a     float64
	edges bool
	gain  float64
}

type gemHighlight struct {
	x, y, k float64
}

// drawGem draws gem g turned by spin about its upright axis, its center at (x, y), seen from a little above (tilt), into layer L with a dark outline.
func drawGem(L *layer, p *pixelfx.Painter, g gemSolid, x, y, spin float64, C []uint32, o drawGemOpts) gemHighlight {
	cs, sn, ct, st := math.Cos(spin), math.Sin(spin), math.Cos(o.tilt), math.Sin(o.tilt)
	P := make([]gemPlane, len(g.planes))
	for i, q := range g.planes {
		P[i] = gemPlane{
			n:    [3]float64{q.n[0]*cs - q.n[2]*sn, q.n[1], q.n[0]*sn + q.n[2]*cs},
			d:    q.d,
			kind: q.kind,
		}
	}
	Lx, Ly, Lz := -0.55, 0.72, 0.42
	D := [3]float64{0, -st, -ct}
	V := [3]float64{0, st, ct}
	Hl := pixelfx.Hypot3(Lx+V[0], Ly+V[1], Lz+V[2])
	H := [3]float64{(Lx + V[0]) / Hl, (Ly + V[1]) / Hl, (Lz + V[2]) / Hl}
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	R, top, bot := math.Ceil(g.r)+2, math.Ceil(g.top+2), math.Ceil(g.bottom+2)
	best, bx, by := -1.0, X, Y
	ids := make(map[int]int)
	L.begin(X-R, Y-top)
	L.fill(X-R, Y-top, X+R, Y+bot, func(px, py float64) uint32 {
		sx, sy := px-X, -(py - Y)
		orig := [3]float64{sx, sy * ct, -sy * st}
		t0, t1 := -1e9, 1e9
		hitIdx := -1
		for idx := range P {
			q := &P[idx]
			nd := q.n[0]*D[0] + q.n[1]*D[1] + q.n[2]*D[2]
			no := q.n[0]*orig[0] + q.n[1]*orig[1] + q.n[2]*orig[2]
			if math.Abs(nd) < 1e-6 {
				if no > q.d {
					return 0
				}
				continue
			}
			tt := (q.d - no) / nd
			if nd < 0 {
				if tt > t0 {
					t0 = tt
					hitIdx = idx
				}
			} else if tt < t1 {
				t1 = tt
			}
		}
		if hitIdx < 0 || t0 > t1 {
			return 0
		}
		if o.white {
			return pixelfx.White
		}
		hit := &P[hitIdx]
		ids[(int(math.Floor(px))+500)*1000+int(math.Floor(py))+500] = hitIdx
		n := hit.n
		pav := hit.kind == 4 || hit.kind == 5 || hit.kind == 8
		m := n
		if pav {
			m = [3]float64{n[0], -n[1] * 0.6, n[2]}
		}
		ml := pixelfx.Hypot3(m[0], m[1], m[2])
		lam := math.Max(0, (m[0]*Lx+m[1]*Ly+m[2]*Lz)/ml)
		spec := math.Pow(math.Max(0, (m[0]*H[0]+m[1]*H[1]+m[2]*H[2])/ml), 18)
		var b float64
		switch {
		case pav:
			b = 0.02 + 0.72*math.Pow(lam, 2.4)
		case hit.kind == 3:
			b = 0.12 + 0.4*lam
		default:
			b = 0.04 + 0.66*math.Pow(lam, 1.8)
		}
		switch hit.kind {
		case 6:
			b = 0.16 + 0.66*math.Pow(math.Max(0, (n[0]*Lx+n[2]*Lz)/pixelfx.Hypot(Lx, Lz)), 1.3)
		case 7:
			b = 0.22 + 0.62*math.Pow(lam, 1.2)
		case 9:
			b = 0.62 + 0.25*lam
		case 10:
			b = 0.3 + 0.5*lam + (pixelfx.Rand(math.Floor(px-X), math.Floor(py-Y), 43)-0.5)*0.08
		}
		if hit.kind != 9 {
			b *= o.gain
		}
		if hit.kind == 0 {
			b -= 0.04
		}
		s := b + spec*0.6
		if s > best {
			best, bx, by = s, px, py
		}
		if spec > 0.8 {
			if pav {
				return C[6]
			}
			return pixelfx.White
		}
		return C[int(math.Max(1, math.Min(6, pixelfx.Round(1+s*5.6))))]
	})
	if !o.white && o.edges {
		type edit struct {
			x, y float64
			c    uint32
		}
		var out []edit
		for key, id := range ids {
			kx := key / 1000
			ky := key - kx*1000
			ex, ey := float64(kx-500), float64(ky-500)
			r, rok := ids[key+1000]
			d, dok := ids[key+1]
			if (rok && r != id) || (dok && d != id) {
				c := L.get(ex, ey)
				i := slices.Index(C, c)
				kind := P[id].kind
				pav := kind == 4 || kind == 5 || kind == 8
				if i > 0 {
					var nc uint32
					if pav {
						nc = C[max(1, i-1)]
					} else {
						nc = C[min(7, i+1)]
					}
					out = append(out, edit{ex, ey, nc})
				}
			}
		}
		for _, e := range out {
			L.set(e.x, e.y, e.c)
		}
	}
	L.outline(C[0]).draw(p, 0, 0, o.a)
	return gemHighlight{x: bx, y: by, k: best}
}

// coin3d draws a thick coin turning on its upright axis and returns the flash strength 0..1.
func coin3d(L *layer, p *pixelfx.Painter, x, y, r, th, theta float64, C []uint32, a float64, white bool) float64 {
	ct, st := math.Cos(theta), math.Sin(theta)
	sg := 1.0
	if ct < 0 {
		sg = -1
	}
	act, ast := math.Abs(ct), math.Abs(st)
	xc, nfx := (th/2)*st*sg, st*sg
	Lx, Ly, Lz := -0.55, 0.45, 0.7
	Ln := pixelfx.Hypot3(Lx, Ly, Lz)
	lam := (nfx*Lx + act*Lz) / Ln
	Hx, Hz := Lx/Ln, Lz/Ln+1
	Hn := pixelfx.Hypot(Hx, Hz)
	base := int(math.Max(2, math.Min(5, pixelfx.Round(2+lam*3.2))))
	X, Y, B := pixelfx.Round(x), pixelfx.Round(y), math.Ceil(r+th)+2
	flash := 0.0
	stSign := 1.0
	if st < 0 {
		stSign = -1
	}
	L.begin(X-B, Y-B)
	L.fill(X-B, Y-B, X+B, Y+B, func(px, py float64) uint32 {
		sx, sy := px-X, -(py - Y)
		if math.Abs(sy) > r {
			return 0
		}
		syR := sy / r
		s := math.Sqrt(1 - syR*syR)
		hw, xr, rw := r*s*act, -stSign*s*r*ct, (th/2)*ast
		if white && (math.Abs(sx-xc) <= hw || math.Abs(sx-xr) <= rw+0.05) {
			return pixelfx.White
		}
		if math.Abs(sx-xc) <= hw && act > 0.08 {
			fx, fy := (sx-xc)/act, sy
			d := pixelfx.Hypot(fx, fy) / r
			tx := nfx + (fx/r)*0.3*act
			sp := math.Pow(math.Max(0, (tx*Hx+act*Hz)/Hn/pixelfx.Hypot(tx, act)), 130)
			flash = math.Max(flash, sp)
			lit := (-fx*0.6 + fy*0.8) / math.Max(0.5, pixelfx.Hypot(fx, fy))
			var k int
			if d > 0.76 {
				off := 0
				switch {
				case lit > 0.75:
					off = 2
				case lit > 0.3:
					off = 1
				case lit < -0.35:
					off = -1
				}
				k = base + off
			} else {
				q := (math.Abs(fx) + math.Abs(fy)) / (r * 0.52)
				switch {
				case q >= 1:
					k = base - 1
				case q < 0.45:
					k = base + 1
				case fx <= 0 && fy >= 0:
					k = base + 1
				case fx > 0 && fy < 0:
					k = base - 2
				default:
					k = base
				}
			}
			if sp > 0.5 {
				if sp > 0.8 {
					return pixelfx.White
				}
				return C[min(7, k+2)]
			}
			return C[max(2, min(6, k))]
		}
		if math.Abs(sx-xr) <= rw+0.05 {
			cphi := s
			if xr < 0 {
				cphi = -s
			}
			n := [3]float64{cphi * ct, sy / r, -cphi * st}
			l := (n[0]*Lx + n[1]*Ly + n[2]*Lz) / Ln
			groove := int(math.Floor((math.Asin(sy/r)+pi)*9)) % 2
			edge := math.Abs(sx-xr) > rw-0.6 && pixelfx.Sign(sx-xr) == pixelfx.Sign(xc-xr)
			k := int(pixelfx.Round(2.6+l*2.6)) - groove
			if edge {
				k++
			}
			return C[max(1, min(6, k))]
		}
		return 0
	})
	L.outline(C[1]).draw(p, 0, 0, a)
	return flash
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
