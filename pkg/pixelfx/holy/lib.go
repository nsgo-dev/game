// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the plus sign's lit and shaded bevel from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the holy effects: gold, green and ash ramps, the character's silhouette and recolors, floor ovals, feathers of light (drawn, and simulated as they rock and sink) and plus signs.

// pi and tau as float64 variables so expressions combining them with other constants round step by step as JavaScript's do, instead of being folded at compile time.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// goldHex is holy gold, dark to bright: a plum shadow, rust, amber, gold, pale gold, cream, white. It is the default palette of most holy effects.
var goldHex = []string{"#3b1d2a", "#7a3a2c", "#c4652e", "#f0a040", "#ffd25e", "#fff0a0", "#fffbe0", "#ffffff"}

// greenHex is healing green, warm: deep pine, leaf, spring green, a yellow-green highlight, cream. It is the default palette of holy/heal-aoe.
var greenHex = []string{"#0e2a24", "#14472f", "#1d6e3a", "#2f9a45", "#5cc654", "#a0e46c", "#dcffb4", "#ffffff"}

// ashHex is the colour of the fallen, a cool grey-violet, dark to light. It is the default ash of holy/resurrection.
var ashHex = []string{"#16131c", "#26222f", "#3a3546", "#524c5e", "#6e6878", "#8c8796"}

func goldPalette() pixelfx.Value  { return pixelfx.Palette(goldHex...) }
func greenPalette() pixelfx.Value { return pixelfx.Palette(greenHex...) }
func ashPalette() pixelfx.Value   { return pixelfx.Palette(ashHex...) }

// bounded holds an option's value v to [lo, hi], taking NaN to lo, so a negative, infinite or NaN value set by a caller cannot stall an effect's loops; values the effect was made for pass unchanged.
func bounded(v, lo, hi float64) float64 {
	if !(v >= lo) {
		return lo
	}
	return math.Min(v, hi)
}

// ---- the character

// shape is the actor's silhouette as local points: edge (its own rim pixels), out (the 1 px ring just outside it), and top and bottom (y of the first row and one past the last).
type shape struct {
	a           *pixelfx.Actor
	edge, out   [][2]float64
	top, bottom float64
}

// inside reports whether the mask pixel (i, j) is part of the actor.
func (s *shape) inside(i, j int) bool {
	m := s.a.Mask
	return i >= 0 && j >= 0 && i < m.W && j < m.H && m.Data[j*m.W+i] > 0
}

// at reports whether the local point (x, y) is inside the actor.
func (s *shape) at(x, y float64) bool {
	return s.inside(int(math.Floor(x)-s.a.X), int(math.Floor(y)-s.a.Y))
}

// shapeOf returns the actor's silhouette.
func shapeOf(a *pixelfx.Actor) *shape {
	m := a.Mask
	s := &shape{a: a, top: a.Y, bottom: a.Y + float64(m.H)}
	in := func(i, j int) int {
		if s.inside(i, j) {
			return 1
		}
		return 0
	}
	for j := -1; j <= m.H; j++ {
		for i := -1; i <= m.W; i++ {
			nb := in(i-1, j) + in(i+1, j) + in(i, j-1) + in(i, j+1)
			pt := [2]float64{a.X + float64(i), a.Y + float64(j)}
			if s.inside(i, j) {
				if nb < 4 {
					s.edge = append(s.edge, pt)
				}
			} else if nb > 0 {
				s.out = append(s.out, pt)
			}
		}
	}
	return s
}

// edgeRows returns the x of the silhouette's rim pixels on each row, keyed by the row's y.
func edgeRows(s *shape) map[float64][]float64 {
	rows := map[float64][]float64{}
	for _, e := range s.edge {
		rows[e[1]] = append(rows[e[1]], e[0])
	}
	return rows
}

// recolor draws the actor's own pixels recolored at alpha a: fn(c, x, y) gives the new color for its pixel c at (x, y), or 0 to leave that pixel alone (the character shows through). An actor without pixels draws nothing.
func recolor(p *pixelfx.Painter, A *pixelfx.Actor, fn func(c uint32, x, y float64) uint32, a float64) {
	m := A.Mask
	if len(A.Pixels) == 0 {
		return
	}
	for j := range m.H {
		for i := range m.W {
			c := A.Pixels[j*m.W+i]
			if c == 0 {
				continue
			}
			x, y := A.X+float64(i), A.Y+float64(j)
			if n := fn(c, x, y); n != 0 {
				p.Dot(x, y, n, a)
			}
		}
	}
}

// toneOf maps a color onto the ramp R by its brightness (a tint that keeps the character's shading).
func toneOf(R []uint32, c uint32, lift float64) uint32 {
	n := float64(len(R))
	return R[int(pixelfx.ClampTo(math.Floor((pixelfx.Luma(c)*1.1+lift)*n), 0, n-1))]
}

// ---- floor and ovals

// ovalFn draws a 1 px oval (a ring on a tilted plane) centred on (cx, cy) with radii rx and ry, its color and alpha fn(g) of the angle g round it (sin(g) > 0 is the near side); keep(g), when not nil, picks the part to draw.
func ovalFn(p *pixelfx.Painter, cx, cy, rx, ry float64, fn func(g float64) (uint32, float64), keep func(g float64) bool) {
	if rx < 0.5 {
		return
	}
	n := int(math.Ceil(math.Max(rx, ry)*tau*1.5)) + 6
	for j := range n {
		g := (float64(j) / float64(n)) * tau
		if keep != nil && !keep(g) {
			continue
		}
		c, a := fn(g)
		p.Dot(cx+math.Cos(g)*rx, cy+math.Sin(g)*ry, c, a)
	}
}

// oval draws ovalFn's oval in one color c at alpha a.
func oval(p *pixelfx.Painter, cx, cy, rx, ry float64, c uint32, a float64, keep func(g float64) bool) {
	ovalFn(p, cx, cy, rx, ry, func(float64) (uint32, float64) { return c, a }, keep)
}

// far keeps the far half of an oval, near the near half.
func far(g float64) bool  { return math.Sin(g) < 0 }
func near(g float64) bool { return math.Sin(g) >= 0 }

// halfOf returns far when back, else near.
func halfOf(back bool) func(g float64) bool {
	if back {
		return far
	}
	return near
}

// ---- feathers

// feather draws a feather of light from its quill at (x, y) pointing along ang, len px long, with a rounded tip; the trailing vane, wid px wide, falls on side (1: clockwise of the shaft, -1: the other way), the leading vane is under half that. R is a ramp, dark to bright (6+ steps), a the alpha, and hot 0..1 whitens it (a flash). A white shaft, a lit leading rim, a warm dark trailing edge that parts it from the next feather, a body that cools from cream at the quill to gold, and a tip that thins into light.
func feather(p *pixelfx.Painter, x, y, ang, length, wid float64, R []uint32, a, side, hot, quill float64) {
	if length < 1.5 || a <= 0.02 {
		return
	}
	n := len(R)
	ca, sa := math.Cos(ang), math.Sin(ang)
	x1, y1, pad, h := x+ca*length, y+sa*length, wid+1.2, hot > 0.5
	edge, lead, B := R[n-5], R[n-2], [3]uint32{R[n-2], R[n-3], R[n-4]}
	if h {
		edge, lead, B = R[n-3], pixelfx.White, [3]uint32{pixelfx.White, pixelfx.White, R[n-2]}
	}
	p.Each(math.Min(x, x1)-pad, math.Min(y, y1)-pad, math.Max(x, x1)+pad, math.Max(y, y1)+pad, func(px, py float64, sx, sy int) {
		dx, dy := px-x, py-y
		u := (dx*ca + dy*sa) / length
		if u < 0 || u > 1.05 {
			return
		}
		v := (-dx*sa + dy*ca) * side
		av := math.Abs(v)
		w := wid * math.Sqrt(pixelfx.Clamp((u-quill)/0.22)) * math.Sqrt(math.Max(0, 1-math.Pow(math.Max(0, (u-0.55)/0.47), 2)))
		lim := w
		if v < 0 {
			lim = w * 0.42
		}
		fade := 1.0
		if u >= 0.72 {
			fade = 1 - (u-0.72)*1.6
		}
		if av < 0.5 && u < 0.9 {
			if u < 0.5 {
				p.Put(sx, sy, pixelfx.White, a)
			} else {
				p.Put(sx, sy, B[0], a)
			}
			return
		}
		if av > lim+0.05 || w < 0.5 {
			return
		}
		if lim-av < 1 {
			switch {
			case v <= 0:
				p.Put(sx, sy, lead, a)
			case u > 0.85:
				p.Put(sx, sy, B[2], a*fade)
			default:
				p.Put(sx, sy, edge, a*fade)
			}
			return
		}
		if u < 0.72 {
			if u < 0.38 {
				p.Put(sx, sy, B[0], a)
			} else {
				p.Put(sx, sy, B[1], a)
			}
			return
		}
		p.BlendPx(sx, sy, B[2], a*fade)
	})
}

// looseFeather is one of a feathers' feathers: its centre (x, y), velocity, angle a, length and width, life left (of max), the delay before it appears, age, whether it is drawn behind the character, and its flutter: which side its vane falls on, the phase, rate (w) and size (amp) of its swing, how fast it sinks, its spin (va) and the tilt it rests at.
type looseFeather struct {
	x, y, vx, vy, a  float64
	len, wid         float64
	life, max, delay float64
	age              float64
	back, landed     bool
	side, ph, w, amp float64
	sink, va, tilt   float64
}

// feathers are loose feathers that are thrown, brake hard, then rock like a pendulum as they sink, tilting with each swing and sinking fastest through the bottom of it; on the floor (at y floor) they lie flat and fade.
type feathers struct {
	R     *pixelfx.Rng
	floor float64
	list  []looseFeather
}

func newFeathers(R *pixelfx.Rng, floor float64) *feathers { return &feathers{R: R, floor: floor} }

// add adds f, given its x, y, vx, vy, a, len (7 when 0), wid (2 when 0), life, delay and back. Its flutter is drawn from the generator first, as pixelfx draws it, and f's side, amp and sink win over the drawn ones when they are not 0.
func (fs *feathers) add(f looseFeather) {
	R := fs.R
	side, ph, w, amp, sink, va, tilt := R.Sign(), R.Range(0, tau), R.Range(3.2, 4.6), R.Range(6, 11), R.Range(14, 24), R.Range(-9, 9), R.Range(-0.35, 0.35)
	if f.side == 0 {
		f.side = side
	}
	if f.amp == 0 {
		f.amp = amp
	}
	if f.sink == 0 {
		f.sink = sink
	}
	if f.len == 0 {
		f.len = 7
	}
	if f.wid == 0 {
		f.wid = 2
	}
	f.ph, f.w, f.va, f.tilt = ph, w, va, tilt
	f.max, f.age, f.landed = f.life, 0, false
	fs.list = append(fs.list, f)
}

func (fs *feathers) count() int { return len(fs.list) }

func (fs *feathers) update(dt float64) {
	for i := len(fs.list) - 1; i >= 0; i-- {
		f := &fs.list[i]
		if f.delay > 0 {
			f.delay -= dt
			continue
		}
		f.life -= dt
		if f.life <= 0 {
			fs.list = append(fs.list[:i], fs.list[i+1:]...)
			continue
		}
		f.age += dt
		if f.landed {
			f.a += (pixelfx.Round(f.a/pi)*pi - f.a) * math.Min(1, dt*10)
			continue
		}
		// the throw: drag kills it fast while it spins
		f.vx -= f.vx * 3.4 * dt
		f.vy -= f.vy * 3.4 * dt
		s, ph := pixelfx.Smooth(0.1, 0.6, f.age), f.age*f.w+f.ph
		c := math.Cos(ph)
		// the flutter: a swing across, a sink that's fastest through the bottom of each swing
		f.x += (f.vx + math.Sin(ph)*f.amp*s*0.9) * dt
		f.y += (f.vy + f.sink*s*(c*c*1.5-0.2)) * dt
		// it lies along its swing, tipping up at each end
		rest := pixelfx.Round(f.a/pi)*pi + f.tilt + math.Sin(ph)*0.6
		f.a += f.va*dt*(1-s) + (rest-f.a)*math.Min(1, dt*5*s)
		if f.y >= fs.floor-0.5 {
			f.y = fs.floor - 0.5
			f.landed = true
			f.life = math.Min(f.life, 0.6)
		}
	}
}

// draw draws the feathers of layer (LayerBoth for all of them) with the ramp R.
func (fs *feathers) draw(p *pixelfx.Painter, R []uint32, layer pixelfx.Layer) {
	for i := range fs.list {
		f := &fs.list[i]
		if f.delay > 0 || (layer == pixelfx.LayerBack && !f.back) || (layer == pixelfx.LayerFront && f.back) {
			continue
		}
		k, a := f.life/f.max, math.Min(1, math.Min(f.life/0.45, f.age/0.06))
		ca, sa := math.Cos(f.a), math.Sin(f.a)
		hot := 0.0
		if f.age < 0.06 {
			hot = 1
		}
		feather(p, f.x-(ca*f.len)/2, f.y-(sa*f.len)/2, f.a, f.len, f.wid, R, a, f.side, hot, 0.2)
		p.Glow(f.x, f.y, 4+f.len*0.5, R[len(R)-3], 0.22*a*(0.4+0.6*k))
	}
}

// ---- plus signs

// plus draws a plus sign of light centred on (x, y): arm length L (1..4) and thickness th (1 or 3): a white heart and spine, colored arms lit on the top and left, shaded on the bottom and right. R is a ramp, dark to bright, and glint whitens the tips of the top and left arms.
func plus(p *pixelfx.Painter, x, y float64, L, th int, R []uint32, a float64, glint bool) {
	n := len(R)
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	w := 0
	if th > 1 {
		w = 1
	}
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	ins := func(i, j int) bool { return (abs(i) <= w && abs(j) <= L) || (abs(j) <= w && abs(i) <= L) }
	for j := -L - 1; j <= L+1; j++ {
		for i := -L - 1; i <= L+1; i++ {
			fx, fy := X+float64(i), Y+float64(j)
			if !ins(i, j) {
				if w != 0 && (ins(i-1, j) || ins(i+1, j) || ins(i, j-1) || ins(i, j+1)) {
					p.Dot(fx, fy, R[max(0, n-6)], a*0.9)
				}
				continue
			}
			var c uint32
			switch {
			case w == 0:
				switch {
				case i == 0 && j == 0:
					c = pixelfx.White
				case abs(i)+abs(j) >= L:
					c = R[n-3]
				default:
					c = R[n-2]
				}
			case i == 0 && j == 0:
				c = pixelfx.White
			case i == 0 || j == 0:
				if abs(i)+abs(j) < L {
					c = R[n-2]
				} else {
					c = R[n-3]
				}
			case i < 0 || j < 0:
				c = R[n-3]
			default:
				c = R[n-5]
			}
			if glint && ((i == 0 && j == -L) || (i == -L && j == 0)) {
				c = pixelfx.White
			}
			p.Dot(fx, fy, c, a)
		}
	}
}

// pick returns one of list chosen by R, drawing from R even when list is empty (as pixelfx's pick does), and false then.
func pick[T any](R *pixelfx.Rng, list []T) (T, bool) {
	i := int(math.Floor(R.Next() * float64(len(list))))
	if i >= len(list) {
		var zero T
		return zero, false
	}
	return list[i], true
}

// chance returns a when R draws under q, else b.
func chance[T any](R *pixelfx.Rng, q float64, a, b T) T {
	if R.Chance(q) {
		return a
	}
	return b
}

// wrapPi returns how far angle a is from 0, folded into -π..π.
func wrapPi(a float64) float64 { return math.Mod(math.Mod(a+pi, tau)+tau, tau) - pi }
