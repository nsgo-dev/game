// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame tongue, flame puff, smoke and ember recipes from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.
// pixelfx adapts the shaded, outlined icons from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Shared by the status effects: the character's shape, noise that loops, and the toolkit's flame, smoke and ember particles drawn from their age alone, so loops stay seamless.

// pi and tau as float64 variables so expressions combining them with other constants round step by step as JavaScript's do, instead of being folded at compile time.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// actorShape is the character's shape in mask space (0, 0 = its top-left):
// body ([x, y] of every pixel), up (pixels with nothing above them: where flames stand),
// side ([x, y, dir] of pixels open to the left (-1) or right (1)),
// outline ([x, y, nx, ny]: empty cells touching it, with the outward normal),
// top (each column's first row, -1 if empty) and rows ([left, right] of each row).
type actorShape struct {
	w, h    int
	data    []uint8
	body    [][2]int
	up      [][2]int
	side    [][3]int
	outline [][4]float64
	top     []int
	rows    [][2]int
}

func (s *actorShape) on(x, y int) bool {
	return x >= 0 && y >= 0 && x < s.w && y < s.h && s.data[y*s.w+x] > 0
}

func shapeOf(a *pixelfx.Actor) *actorShape {
	m := a.Mask
	s := &actorShape{
		w:    m.W,
		h:    m.H,
		data: m.Data,
		top:  make([]int, m.W),
		rows: make([][2]int, m.H),
	}
	for x := 0; x < s.w; x++ {
		y := 0
		for y < s.h && !s.on(x, y) {
			y++
		}
		if y < s.h {
			s.top[x] = y
		} else {
			s.top[x] = -1
		}
	}
	for y := 0; y < s.h; y++ {
		l, r := -1, -1
		for x := 0; x < s.w; x++ {
			if !s.on(x, y) {
				continue
			}
			if l < 0 {
				l = x
			}
			r = x
			s.body = append(s.body, [2]int{x, y})
			if !s.on(x, y-1) {
				s.up = append(s.up, [2]int{x, y})
			}
			if !s.on(x-1, y) {
				s.side = append(s.side, [3]int{x, y, -1})
			}
			if !s.on(x+1, y) {
				s.side = append(s.side, [3]int{x, y, 1})
			}
		}
		s.rows[y] = [2]int{l, r}
	}
	for y := -1; y <= s.h; y++ {
		for x := -1; x <= s.w; x++ {
			if s.on(x, y) {
				continue
			}
			nx, ny := 0.0, 0.0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if s.on(x+dx, y+dy) {
						nx -= float64(dx)
						ny -= float64(dy)
					}
				}
			}
			if s.on(x-1, y) || s.on(x+1, y) || s.on(x, y-1) || s.on(x, y+1) {
				l := pixelfx.Hypot(nx, ny)
				if l == 0 {
					l = 1
				}
				s.outline = append(s.outline, [4]float64{float64(x), float64(y), nx / l, ny / l})
			}
		}
	}
	return s
}

// colorAt returns the character's own color at mask pixel (x, y), or a mid grey for a mask without pixels.
func colorAt(a *pixelfx.Actor, x, y int) uint32 {
	if x < 0 || y < 0 || x >= a.Mask.W || y >= a.Mask.H {
		return 0
	}
	idx := y*a.Mask.W + x
	if len(a.Pixels) > 0 {
		return a.Pixels[idx]
	}
	if a.Mask.Data[idx] != 0 {
		return 0xff6b6b6b
	}
	return 0
}

// wnoise is 1D value noise in [-1, 1] whose lattice wraps every P cells.
func wnoise(x, P, seed float64) float64 {
	i := math.Floor(x)
	f := x - i
	u := f * f * (3 - 2*f)
	a := pixelfx.Rand(pixelfx.Wrap(i, P), seed)
	b := pixelfx.Rand(pixelfx.Wrap(i+1, P), seed)
	return (a+(b-a)*u)*2 - 1
}

// tnoise is noise of time t that repeats every D seconds, about f wiggles a second; off slides along it (ripples).
func tnoise(t, D, f, seed, off float64) float64 {
	P := math.Max(2, pixelfx.Round(f*D))
	return wnoise((t/D)*P+off, P, seed)
}

// travel returns how far something has moved after age a, launched at speed v under acceleration g with linear drag d.
func travel(v, g, d, a float64) float64 {
	if d > 0 {
		return (g/d)*a + ((v-g/d)*(1-math.Exp(-d*a)))/d
	}
	return v*a + 0.5*g*a*a
}

// tongueLayer is one layer of a flame tongue: width, height, ramp step (0 brightest).
type tongueLayer struct {
	lw, lh float64
	ri     int
}

// defaultLayers are a tongue's nested layers: a deep rim, orange, yellow, a white heart.
var defaultLayers = []tongueLayer{{1, 1, 6}, {0.76, 0.86, 4}, {0.54, 0.68, 2}, {0.3, 0.42, 0}}

// tongue draws a flame tongue standing on (x, y): h tall, w half-wide at its root, its tip swept sx px sideways, filled in nested layers from F (bright to dark, 9 steps). Rows ripple with noise that loops every D seconds (seed n).
func tongue(p *pixelfx.Painter, x, y, h, w, sx float64, F []uint32, t, D, n float64, layers []tongueLayer) {
	if h < 1 {
		return
	}
	if layers == nil {
		layers = defaultLayers
	}
	for _, l := range layers {
		if l.ri == 0 && h < 4 {
			continue
		}
		H, col := h*l.lh, F[l.ri]
		for r := 0.0; r <= H; r++ {
			u := r / math.Max(1, H)
			half := w*l.lw*math.Pow(1-u, 0.85)*math.Min(1, (r+1.5)/3.5) + tnoise(t, D, 9, n, r*0.45)*0.5
			if half < 0.3 {
				continue
			}
			cx := x + sx*u*u*l.lh + tnoise(t, D, 4, n+7, r*0.2)*u*1.1
			for px, x1 := pixelfx.Round(cx-half), pixelfx.Round(cx+half); px <= x1; px++ {
				p.Px(px, pixelfx.Round(y-r), col)
			}
		}
	}
}

func rampAt(r []uint32, t float64) uint32 {
	return r[int(math.Min(float64(len(r)-1), math.Max(0, math.Floor(t*float64(len(r))))))]
}

// smoke draws a puff of smoke at age u (0..1): three soft discs (a body, a denser core sitting low, a lit cap while young), swelling and thinning, tinted col and walking down the smoke ramp. warm (a color) lights its underside.
func smoke(p *pixelfx.Painter, x, y, size, u float64, col uint32, k float64, warm uint32) {
	r := size * (0.65 + math.Min(1, u*2.5)*0.45 + u*0.4)
	a := math.Min(1, u*12) * math.Min(1, (1-u)*2.4) * 0.58 * k
	if a <= 0.01 {
		return
	}
	var c uint32
	if col != 0 {
		c = pixelfx.Mix(col, rampAt(pixelfx.SmokeRamp, u), u)
	} else {
		c = rampAt(pixelfx.SmokeRamp, u*1.05)
	}
	p.Disc(x, y, r, c, a*0.45)
	p.Disc(x+r*0.05, y+r*0.06, r*0.74, c, a*0.62)
	if warm != 0 && u < 0.6 {
		p.Disc(x+r*0.1, y+r*0.3, r*0.56, pixelfx.Mix(c, warm, 0.5), a*0.7*(1-u/0.6))
	}
	if u < 0.5 {
		p.Disc(x-r*0.22, y-r*0.3, r*0.5, pixelfx.Mix(c, pixelfx.White, 0.12), a*0.4*(1-u*2))
	}
}

// ember draws an ember at age u: hot and flickering, cooling to a dim red that blinks out. E is [hot, warm, cool].
func ember(p *pixelfx.Painter, x, y, u float64, E [3]uint32, ph, k float64) {
	left := 1 - u
	if left < 0.22 && math.Sin(ph*40) < 0 {
		return
	}
	c := E[2]
	if left > 0.5 {
		if math.Sin(ph*22) > -0.2 {
			c = E[0]
		} else {
			c = E[1]
		}
	} else if left > 0.22 {
		c = E[1]
	}
	p.Px(x, y, c)
	p.Glow(x, y, 3, c, 0.32*left*k)
}

// bitmap returns inside(dx, dy) for a shape from a bitmap (rows of strings, "." empty), centred on (0, 0), scaled up by whole k, mirrored if flip.
func bitmap(rows []string, k int, flip bool) func(dx, dy int) byte {
	h, w := len(rows), len(rows[0])
	ox, oy := (w*k)/2, (h*k)/2
	return func(dx, dy int) byte {
		bx := int(math.Floor(float64(dx+ox) / float64(k)))
		by := int(math.Floor(float64(dy+oy) / float64(k)))
		if bx < 0 || by < 0 || bx >= w || by >= h {
			return 0
		}
		if flip {
			bx = w - 1 - bx
		}
		c := rows[by][bx]
		if c == '.' {
			return 0
		}
		return c
	}
}

// icon draws a little shaded pixel icon at (x, y): every cell where inside(dx, dy) is non-zero, within ext of the centre, filled from R = [outline, shadow, body, light] (lit on its top and left edges, shaded on the bottom and right, ringed by the outline) and recoloured where inside returns a character listed in paint. a fades it.
func icon(p *pixelfx.Painter, x, y float64, ext int, inside func(dx, dy int) byte, R [4]uint32, a float64, paint map[byte]uint32) {
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	at := func(dx, dy int) bool { return inside(dx, dy) != 0 }
	for dy := -ext - 1; dy <= ext+1; dy++ {
		for dx := -ext - 1; dx <= ext+1; dx++ {
			ch := inside(dx, dy)
			var c uint32
			if ch != 0 {
				if pc, ok := paint[ch]; ok {
					c = pc
				} else {
					top := !at(dx, dy-1)
					left := !at(dx-1, dy)
					bottom := !at(dx, dy+1)
					right := !at(dx+1, dy)
					if top || left {
						c = R[3]
					} else if bottom || right {
						c = R[1]
					} else {
						c = R[2]
					}
				}
			} else if R[0] != 0 && (at(dx-1, dy) || at(dx+1, dy) || at(dx, dy-1) || at(dx, dy+1)) {
				c = R[0]
			} else {
				continue
			}
			p.Dot(X+float64(dx), Y+float64(dy), c, a)
		}
	}
}

// shaped returns inside(dx, dy) for a shape given in its own unit space by f(u, v), rotated by ang and scaled by s px.
func shaped(f func(u, v float64) bool, s, ang float64) func(dx, dy int) byte {
	c, n := math.Cos(ang), math.Sin(ang)
	return func(dx, dy int) byte {
		x, y := float64(dx)+0.5, float64(dy)+0.5
		if f((x*c+y*n)/s, (-x*n+y*c)/s) {
			return '#'
		}
		return 0
	}
}

// starShape is a five-pointed star in unit space (points at radius 1, inner corners at r0).
func starShape(r0 float64) func(u, v float64) bool {
	return func(u, v float64) bool {
		a := math.Atan2(v, u) + pi/2
		d := pixelfx.Hypot(u, v)
		seg := (2 * pi) / 5
		k := 1 - math.Abs(pixelfx.Wrap(a, seg)-seg/2)/(seg/2)
		return d <= 1-(1-r0)*math.Pow(k, 0.8)
	}
}

// heartShape is a heart in unit space (two lobes and a point, about 2 wide and 2 tall, centred).
func heartShape(u, v float64) bool {
	return (u+0.5)*(u+0.5)+(v+0.32)*(v+0.32) <= 0.55*0.55 ||
		(u-0.5)*(u-0.5)+(v+0.32)*(v+0.32) <= 0.55*0.55 ||
		(v >= -0.32 && v <= 1 && math.Abs(u) <= 1.04*math.Pow(1-(v+0.32)/1.32, 0.75))
}

// eye is one eye detected on the character in mask space, with skin color round it.
type eye struct {
	x0, x1, y0, y1 int
	px             [][2]int
	skin           uint32
}

// eyesOf returns the character's eyes, if it has any we can find: small blobs of very dark pixels (with any white beside them) inside the upper part of the body, surrounded by it, left to right.
func eyesOf(a *pixelfx.Actor, S *actorShape) []eye {
	if len(a.Eyes) > 0 {
		list := make([]eye, len(a.Eyes))
		for i, pt := range a.Eyes {
			x, y := int(pt[0]), int(pt[1])
			list[i] = eye{x0: x, x1: x, y0: y, y1: y + 1, px: [][2]int{{x, y}, {x, y + 1}}, skin: colorAt(a, x, y)}
		}
		return list
	}
	W := S.w
	col := func(x, y int) uint32 {
		if S.on(x, y) {
			return colorAt(a, x, y)
		}
		return 0
	}
	dark := func(x, y int) bool {
		return S.on(x, y) && float64(y) < float64(S.h)*0.4 && pixelfx.Luma(col(x, y)) < 0.15 && S.on(x-1, y) && S.on(x+1, y) && S.on(x, y-1) && S.on(x, y+1)
	}
	seen := map[int]bool{}
	var list []eye
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	neigh := [6][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {2, 0}, {-2, 0}}
	for _, pt := range S.body {
		x0, y0 := pt[0], pt[1]
		if seen[y0*W+x0] || !dark(x0, y0) {
			continue
		}
		var px [][2]int
		q := [][2]int{{x0, y0}}
		seen[y0*W+x0] = true
		for len(q) > 0 {
			cur := q[len(q)-1]
			q = q[:len(q)-1]
			x, y := cur[0], cur[1]
			px = append(px, [2]int{x, y})
			for _, d := range dirs {
				nx, ny := x+d[0], y+d[1]
				k := ny*W + nx
				if seen[k] || !(dark(nx, ny) || (S.on(nx, ny) && pixelfx.Luma(col(nx, ny)) > 0.95)) {
					continue
				}
				seen[k] = true
				q = append(q, [2]int{nx, ny})
			}
		}
		if len(px) < 2 || len(px) > 14 {
			continue
		}
		e := eye{x0: px[0][0], x1: px[0][0], y0: px[0][1], y1: px[0][1], px: px}
		for _, p := range px[1:] {
			e.x0 = min(e.x0, p[0])
			e.x1 = max(e.x1, p[0])
			e.y0 = min(e.y0, p[1])
			e.y1 = max(e.y1, p[1])
		}
		type colorCount struct {
			c uint32
			n int
		}
		var counts []colorCount
		idxOf := map[uint32]int{}
		for _, p := range px {
			x, y := p[0], p[1]
			for _, d := range neigh {
				nx, ny := x+d[0], y+d[1]
				c := col(nx, ny)
				l := pixelfx.Luma(c)
				if c == 0 || !(l > 0.5 && l < 0.95) {
					continue
				}
				inEye := false
				for _, ep := range px {
					if ep[0] == nx && ep[1] == ny {
						inEye = true
						break
					}
				}
				if inEye {
					continue
				}
				if idx, ok := idxOf[c]; ok {
					counts[idx].n++
				} else {
					idxOf[c] = len(counts)
					counts = append(counts, colorCount{c: c, n: 1})
				}
			}
		}
		slices.SortStableFunc(counts, func(p, q colorCount) int { return q.n - p.n })
		if len(counts) > 0 {
			e.skin = counts[0].c
		}
		if e.skin != 0 && e.x1-e.x0 <= 4 && e.y1-e.y0 <= 5 {
			list = append(list, e)
		}
	}
	slices.SortStableFunc(list, func(p, q eye) int { return p.x0 - q.x0 })
	if len(list) > 2 {
		list = nil
	}
	return list
}

// hotFirst returns a ramp reversed (brightest first).
func hotFirst(F []uint32) []uint32 {
	H := slices.Clone(F)
	slices.Reverse(H)
	return H
}
