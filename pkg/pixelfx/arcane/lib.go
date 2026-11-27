// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The dithered ramp (tone) is adapted from Sprite Fusion's open-source black hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), and the arc drawing from its energy (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Shared by the arcane effects: a dithered light ramp, closed-form motes and streaks for loops, crackling arcs, floor rings and an original rune alphabet.

// pi and tau as float64 variables so expressions combining them with constants round step by step as JavaScript's do.
var (
	pi  = math.Pi
	tau = pixelfx.Tau
)

// arcanePalette returns pixelfx's arcane ramp, every effect's default palette.
func arcanePalette() pixelfx.Value { return pixelfx.RampOf(pixelfx.Ramps["arcane"]) }

// zeroGround is the floor at y = 0 every simulated effect here bounces on.
func zeroGround(float64) float64 { return 0 }

// pick returns a when cond holds and b otherwise: JavaScript's cond ? a : b for colors.
func pick(cond bool, a, b uint32) uint32 {
	if cond {
		return a
	}
	return b
}

// pickF returns a when cond holds and b otherwise, for numbers.
func pickF(cond bool, a, b float64) float64 {
	if cond {
		return a
	}
	return b
}

// or1 returns x, or 1 when x is 0 or NaN: JavaScript's x || 1.
func or1(x float64) float64 {
	if x == 0 || math.IsNaN(x) {
		return 1
	}
	return x
}

// alpha returns an alpha function that is a everywhere.
func alpha(a float64) func(float64) float64 { return func(float64) float64 { return a } }

var bayerTable = func() [16]float64 {
	var out [16]float64
	for i, v := range [16]float64{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5} {
		out[i] = (v + 0.5) / 16
	}
	return out
}()

// cross are the four neighbours of a pixel.
var cross = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// bayer returns the 4x4 ordered-dither threshold (0..1) of the pixel cell holding (x, y).
func bayer(x, y float64) float64 {
	return bayerTable[((pixelfx.ToInt32(math.Floor(y))&3)<<2)|(pixelfx.ToInt32(math.Floor(x))&3)]
}

// tone maps light intensity I (0..1) to a step of ramp R (dark to bright), dithering between neighbouring steps so a gradient reads smooth at 1x; below lo it thins out through the dither and returns 0 (draw nothing). sharp (0..1) narrows the dithered zone between two steps, so broad areas stay clean and only edges dither.
func tone(R []uint32, I, x, y, lo, sharp float64) uint32 {
	if math.IsNaN(I) {
		return 0
	}
	b := bayer(x, y)
	if I < lo && b > I/lo {
		return 0
	}
	n := float64(len(R) - 1)
	f := pixelfx.Clamp(I) * n
	i := math.Floor(f)
	q := f - i
	if sharp > 0 {
		q = pixelfx.Clamp((f-i-0.5)/(1-sharp) + 0.5)
	}
	if q > b {
		i = math.Min(n, i+1)
	}
	return R[int(i)]
}

// mote draws a drifting mote at (x, y) moving (vx, vy) px/s, like the simulation's Mote: a bright head, a darker tail, light. pixelfx's defaults are size 1, k 1 and glow 0.4.
func mote(p *pixelfx.Painter, x, y, vx, vy float64, c uint32, size, k, glow float64) {
	sp := or1(pixelfx.Hypot(vx, vy))
	ux, uy := vx/sp, vy/sp
	l := math.Min(16, 0.5+sp*0.026) * size
	a := 1.0
	if k < 0.35 {
		a = k / 0.35
	}
	if l > 1.2 {
		p.Line(x-ux*l, y-uy*l, x-ux*l*0.35, y-uy*l*0.35, pixelfx.Scale(c, 0.55), a)
	}
	p.Line(x-ux*l*0.4, y-uy*l*0.4, x, y, c, a)
	if glow > 0 {
		p.Glow(x, y, 2.5+size, c, glow*k)
	}
}

// streak draws energy rushing in, like the simulation's Streak: a white tip, a colored body, a dim dotted tail. pixelfx's defaults are a 1, maxLen 7 and glow 0.4.
func streak(p *pixelfx.Painter, x, y, vx, vy float64, c uint32, a, maxLen, glow float64) {
	sp := or1(pixelfx.Hypot(vx, vy))
	ux, uy := vx/sp, vy/sp
	L := pixelfx.ClampTo(sp/30, 1, maxLen)
	for d := L; d >= 0; d -= 0.5 {
		col, k := pixelfx.Scale(c, 0.6), 1.2-d/L
		if d < L*0.5 {
			col, k = c, 1
		}
		if d < 1 {
			col = pixelfx.White
		}
		p.Dot(x-ux*d, y-uy*d, col, k*a)
	}
	if glow > 0 {
		p.Glow(x, y, 4, c, glow*a)
	}
}

// arcPath is one path of an arc: its points and its weight (1 the main path, less a fork).
type arcPath struct {
	pts [][2]float64
	w   float64
}

// arcOpts shape an arc: how jagged it is, how many forks it may grow and how long they are (pixelfx's defaults are 0.3, 1 and 0.5).
type arcOpts struct {
	jag, forkLen float64
	forks        int
}

// arc returns a crackling arc from (x0, y0) to (x1, y1): a jagged main path plus forks. r is a generator, so re-seed it every couple of frames for flicker.
func arc(r *pixelfx.Rng, x0, y0, x1, y1 float64, o arcOpts) []arcPath {
	L := pixelfx.Hypot(x1-x0, y1-y0)
	depth := int(math.Max(1, math.Min(5, pixelfx.Round(math.Log2(L/1.5)))))
	main := pixelfx.Bolt(r, x0, y0, x1, y1, o.jag, depth)
	out := []arcPath{{pts: main, w: 1}}
	for k := 0; k < o.forks && len(main) > 3; k++ {
		if r.Next() > 0.7 {
			continue
		}
		i := 1 + int(math.Floor(r.Next()*float64(len(main)-2)))
		x, y := main[i][0], main[i][1]
		px, py := main[i-1][0], main[i-1][1]
		side := 1.0
		if r.Next() < 0.5 {
			side = -1
		}
		a := math.Atan2(y-py, x-px) + side*(0.4+r.Next()*0.6)
		l := L * o.forkLen * (0.4 + 0.6*r.Next()) * (1 - float64(i)/float64(len(main)))
		if l > 1.5 {
			out = append(out, arcPath{pts: pixelfx.Bolt(r, x, y, x+math.Cos(a)*l, y+math.Sin(a)*l, o.jag*1.1, max(1, depth-2)), w: 0.55})
		}
	}
	return out
}

// arcColors are how drawArc colors an arc: its core, the body beside it, the halo round it while it's fresh, and its light.
type arcColors struct {
	core, body, halo, glow uint32
}

// drawArc draws arc paths the way Sprite Fusion draws its bolts: a dithered halo while it's fresh, a body beside the core, a white core, light along the path. k is 1 fresh to 0 gone (pixelfx's default is 1), glowK how much light (0.5).
func drawArc(p *pixelfx.Painter, paths []arcPath, cols arcColors, k, glowK float64) {
	steps := func(fn func(x, y float64, path *arcPath, i int, first bool)) {
		for j := range paths {
			path := &paths[j]
			for i := 0; i < len(path.pts)-1; i++ {
				ax, ay := path.pts[i][0], path.pts[i][1]
				bx, by := path.pts[i+1][0], path.pts[i+1][1]
				l := math.Max(1, pixelfx.Hypot(bx-ax, by-ay))
				for d := 0.0; d <= l; d += 0.5 {
					fn(ax+((bx-ax)*d)/l, ay+((by-ay)*d)/l, path, i, d == 0)
				}
			}
		}
	}
	if k > 0.45 {
		steps(func(x, y float64, path *arcPath, _ int, _ bool) {
			if path.w >= 1 {
				for _, d := range cross {
					p.Dot(x+d[0]*1.5, y+d[1]*1.5, cols.halo, (k-0.45)*1.6)
				}
			}
		})
	}
	if k > 0.25 {
		steps(func(x, y float64, path *arcPath, _ int, _ bool) {
			if path.w > 0.4 {
				p.Dot(x+1, y, cols.body, 1)
				p.Dot(x, y+1, cols.body, 1)
			}
		})
	}
	steps(func(x, y float64, path *arcPath, i int, first bool) {
		p.Px(x, y, pick(path.w >= 1 && k > 0.3, cols.core, cols.body))
		if first && i%3 == 0 {
			p.Glow(x, y, pickF(path.w >= 1, 9, 5)*(0.5+k*0.5), cols.glow, glowK*k*path.w)
		}
	})
}

// floorRing draws a ring lying on the floor (or any tilted plane): centre (cx, cy), radius r, squashed by sy, 1 px thick at any size. c and a are functions of the angle g around it (sin(g) > 0 is the near side, towards the viewer); keep(g), when not nil, picks which part to draw: sin(g) < 0 is the far half, for the back layer.
func floorRing(p *pixelfx.Painter, cx, cy, r, sy float64, c func(g float64) uint32, a func(g float64) float64, keep func(g float64) bool) {
	if r < 0.5 {
		return
	}
	ry := r * sy
	p.Shade(cx-r-1, cy-ry-1, cx+r+1, cy+ry+1, func(x, y float64, o *pixelfx.Out) uint32 {
		nx, ny := (x-cx)/r, (y-cy)/math.Max(0.3, ry)
		d := pixelfx.Hypot(nx, ny)
		if d < 1e-6 {
			return 0
		}
		g := pixelfx.Hypot(nx/r, ny/math.Max(0.3, ry)) / d
		if math.Abs(d-1)/g > 0.5 {
			return 0
		}
		ang := math.Atan2(ny, nx)
		if keep != nil && !keep(ang) {
			return 0
		}
		o.C = c(ang)
		o.A = a(ang)
		return 0
	})
}

// glyphs is an original rune alphabet: angular 3 x 5 glyphs that stay readable standing upright at 1x.
var glyphs = [][]string{
	{".#.", "#.#", ".#.", ".#.", "#.#"},
	{"#.#", "#.#", ".#.", "#.#", "#.#"},
	{"##.", "#.#", "##.", "#..", "#.."},
	{"#..", "#.#", "###", "#.#", "..#"},
	{".##", "#..", ".#.", "..#", "##."},
	{"###", ".#.", "###", ".#.", ".#."},
	{"#.#", "###", "#.#", "..#", "..#"},
	{".#.", ".##", ".#.", "##.", ".#."},
}

// glyph draws rune i standing on (x, y): its bottom row sits on y, centred on x. c is its body color, a its alpha (dithered), under an optional darker color (0 for none) drawn a pixel lower so the glyph has a little thickness.
func glyph(p *pixelfx.Painter, i int, x, y float64, c uint32, a float64, under uint32) {
	n := len(glyphs)
	g := glyphs[((i%n)+n)%n]
	X, Y := pixelfx.Round(x)-1, pixelfx.Round(y)-float64(len(g))+1
	if under != 0 {
		p.Sprite(g, nil, X, Y+1, a, pixelfx.SpriteOpts{Tint: under})
	}
	p.Sprite(g, nil, X, Y, a, pixelfx.SpriteOpts{Tint: c})
}

// sigils are larger original sigils, 5 x 7, for runes that fly or hover on their own.
var sigils = [][]string{
	{"..#..", ".#.#.", "#.#.#", "..#..", ".###.", "..#..", ".#.#."},
	{"#...#", ".#.#.", "..#..", ".###.", "..#..", ".#.#.", "#...#"},
	{"#....", "##...", "#.#..", "#..#.", "#.#..", "##...", "#...."},
	{"..#..", "#.#.#", ".###.", "..#..", "..#..", ".#.#.", "#...#"},
	{"#.#.#", "#.#.#", "#####", "..#..", "..#..", "..#..", ".###."},
	{"#...#", "##.##", "#.#.#", "#...#", "#.#.#", "##.##", "#...#"},
	{"..#..", ".#.#.", "#...#", ".#.#.", "..#..", "..#..", "..#.."},
	{"#..#.", "#.#..", "##...", "#....", "##...", "#.#..", "#..#."},
}

// sigil draws sigil i centred on (x, y) in color c, ringed by a 1 px edge in a darker color (0 for none) so it reads against anything.
func sigil(p *pixelfx.Painter, i int, x, y float64, c uint32, a float64, edge uint32) {
	n := len(sigils)
	g := sigils[((i%n)+n)%n]
	X, Y := pixelfx.Round(x)-2, pixelfx.Round(y)-3
	if edge != 0 {
		for _, d := range cross {
			p.Sprite(g, nil, X+d[0], Y+d[1], a, pixelfx.SpriteOpts{Tint: edge})
		}
	}
	p.Sprite(g, nil, X, Y, a, pixelfx.SpriteOpts{Tint: c})
}

// shades returns a character's silhouette shaded by how light each pixel is: 1 its dark outline, 2 mid, 3 light (0 empty).
func shades(A *pixelfx.Actor) []uint8 {
	s := make([]uint8, A.W*A.H)
	for i := range s {
		if A.Mask.Data[i] == 0 {
			continue
		}
		if A.Pixels == nil {
			s[i] = 2
			continue
		}
		switch l := pixelfx.Luma(A.Pixels[i]); {
		case l < 0.24:
			s[i] = 1
		case l < 0.62:
			s[i] = 2
		default:
			s[i] = 3
		}
	}
	return s
}

// squeezed draws silhouette A squeezed to sx of its width about its middle and shifted by dx: body color on its pixels, edge color on its dark outline pixels. sh is shades(A). The blink's squeeze, adapted from Sprite Fusion.
func squeezed(p *pixelfx.Painter, A *pixelfx.Actor, sh []uint8, sx float64, body, edge uint32, a, dx float64) {
	if sx <= 0.02 {
		return
	}
	cx, half := A.X+float64(A.W)/2, float64(A.W)/2+1
	for y := 0; y < A.H; y++ {
		for x := math.Floor(cx - half*sx); x <= math.Ceil(cx+half*sx); x++ {
			i := pixelfx.Round(cx+(x+0.5-cx)/sx-0.5) - A.X
			var v uint8
			if i >= 0 && i < float64(A.W) {
				v = sh[y*A.W+int(i)]
			}
			if v != 0 {
				p.Dot(x+dx, A.Y+float64(y), pick(v == 1, edge, body), a)
			}
		}
	}
}
