// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/ice-crystal.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// A floating pickup that bobs, glints, glows and has motes circling in front and behind follows the recipe in Sprite Fusion's open-source pickup gem (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// IceCrystal is a floating ice crystal turning slowly in its own cold light: its facets brighten and darken as they turn, glints flash on them, frost motes circle it. It is registered as "ice/ice-crystal".
var IceCrystal = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ice crystal",
		Desc:     "A floating ice crystal turning slowly in its own cold light: its facets brighten and darken as they turn, glints flash on them, frost motes circle it.",
		Tags:     []string{"ice", "crystal", "gem", "pickup", "float", "magic", "loop"},
		Size:     [2]int{96, 100},
		Origin:   [2]int{48, 92},
		Duration: crystalDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "height", Default: pixelfx.Num(34)},
			{Name: "seed", Default: pixelfx.Num(21)},
		},
	},
	New: newIceCrystal,
}

func init() { pixelfx.Register("ice/ice-crystal", IceCrystal) }

const crystalDuration = 3

var (
	// crystalTip is how far it leans toward us, so the upper facets show.
	crystalTip = 0.28
	// crystalLight points toward the light: upper left, in front.
	crystalLight = norm3([3]float64{-0.55, -0.75, -0.45})
	// crystalHalf is half way between the light and the eye, where a face mirrors the light toward the viewer.
	crystalHalf = norm3([3]float64{crystalLight[0], crystalLight[1], crystalLight[2] - 1})
)

func norm3(v [3]float64) [3]float64 {
	l := pixelfx.Hypot3(v[0], v[1], v[2])
	if l == 0 {
		l = 1
	}
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}

// crystalFace is a visible face of the crystal: its projected corners, how lit it is, how much it glints and its center.
type crystalFace struct {
	pts       [][2]float64
	lit, spec float64
	cx, cy    float64
}

// crystalFaces returns the visible faces of the crystal: a hexagonal prism with a tall point above and a shorter one below, turned by th, tipped toward the viewer.
func crystalFaces(th, R float64) []crystalFace {
	ct, st := math.Cos(crystalTip), math.Sin(crystalTip)
	P := func(x, y, z float64) [3]float64 { return [3]float64{x, y*ct - z*st, y*st + z*ct} } // tip toward the viewer (z toward us is negative)
	ring := func(y float64) [6][3]float64 {
		var out [6][3]float64
		for k := range 6 {
			out[k] = P(math.Cos(th+(float64(k)*tau)/6)*R, y, math.Sin(th+(float64(k)*tau)/6)*R)
		}
		return out
	}
	top, bot, A, Z := ring(-R*0.45), ring(R*0.45), P(0, -R*2.2, 0), P(0, R*1.7, 0)
	faces := make([][][3]float64, 0, 18)
	for k := range 6 {
		j := (k + 1) % 6
		faces = append(faces, [][3]float64{A, top[j], top[k]}, [][3]float64{top[k], top[j], bot[j], bot[k]}, [][3]float64{bot[k], bot[j], Z})
	}
	var out []crystalFace
	for _, f := range faces {
		a, b, c := f[0], f[1], f[2]
		u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
		v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
		n := norm3([3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]})
		var cx, cy, cz float64
		for _, q := range f {
			cx += q[0]
		}
		for _, q := range f {
			cy += q[1]
		}
		for _, q := range f {
			cz += q[2]
		}
		fl := float64(len(f))
		cx, cy, cz = cx/fl, cy/fl, cz/fl
		if n[0]*cx+n[1]*cy+n[2]*cz < 0 {
			n = [3]float64{-n[0], -n[1], -n[2]} // outward
		}
		if n[2] >= -0.02 {
			continue // facing away
		}
		lit := math.Max(0, n[0]*crystalLight[0]+n[1]*crystalLight[1]+n[2]*crystalLight[2])
		// a glint where the face mirrors the light toward the viewer
		spec := math.Max(0, n[0]*crystalHalf[0]+n[1]*crystalHalf[1]+n[2]*crystalHalf[2])
		pts := make([][2]float64, len(f))
		for i, q := range f {
			pts[i] = [2]float64{q[0], q[1]}
		}
		out = append(out, crystalFace{pts: pts, lit: lit, spec: spec, cx: cx, cy: cy})
	}
	return out
}

// hullOf returns the convex hull of a set of points (monotone chain), for the crystal's silhouette.
func hullOf(pts [][2]float64) [][2]float64 {
	P := slices.Clone(pts)
	slices.SortStableFunc(P, func(a, b [2]float64) int {
		d := a[0] - b[0]
		if d == 0 {
			d = a[1] - b[1]
		}
		switch {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
		return 0
	})
	cross := func(o, a, b [2]float64) float64 { return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0]) }
	var lo, hi [][2]float64
	for _, q := range P {
		for len(lo) >= 2 && cross(lo[len(lo)-2], lo[len(lo)-1], q) <= 0 {
			lo = lo[:len(lo)-1]
		}
		lo = append(lo, q)
	}
	for i := len(P) - 1; i >= 0; i-- {
		q := P[i]
		for len(hi) >= 2 && cross(hi[len(hi)-2], hi[len(hi)-1], q) <= 0 {
			hi = hi[:len(hi)-1]
		}
		hi = append(hi, q)
	}
	return append(slices.Clone(lo[:len(lo)-1]), hi[:len(hi)-1]...)
}

func crystalMotes(p *pixelfx.Painter, C []uint32, t, seed, Y float64, front bool) {
	pixelfx.Cycle(t, crystalDuration, 16, seed, func(g *pixelfx.Rng) float64 { return g.Range(1.6, 2.6) }, func(age, life float64, r *pixelfx.Rng, i int) {
		th := r.Range(0, tau) + age*r.Range(1.6, 2.6)
		R := r.Range(14, 22)
		y := Y + r.Range(-14, 12) - age*r.Range(3, 7)
		s, k := math.Sin(th), age/life
		if (s >= 0) != front {
			return
		}
		x, yy := math.Cos(th)*R, y+s*R*0.3
		a := math.Min(1, age/0.2) * pick(k > 0.75, (1-k)/0.25, 1)
		if front {
			p.Dot(x, yy, pick(i%4 == 0, pixelfx.White, C[6]), a)
			if i%4 == 0 {
				for _, d := range cross4 {
					p.Dot(x+d[0], yy+d[1], C[5], a)
				}
			}
			p.Glow(x, yy, 4, C[5], 0.3*a)
		} else {
			p.Dot(x, yy, C[4], a*0.9)
		}
	})
}

func newIceCrystal(o *pixelfx.Options) pixelfx.Instance {
	C, seed, height := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Num("height")
	pose := func(t float64) (ph, bob, Y float64) {
		ph = t / crystalDuration
		bob = math.Sin(tau*ph) * 2.5
		return ph, bob, -height + bob
	}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph, bob, Y := pose(t)
			// its shadow on the floor, smaller and darker as it dips, and a cold pool of light
			s := 1 - bob/12
			rx := 11 * s
			for x := math.Floor(-rx); x <= rx; x++ {
				f := 1 - (x/rx)*(x/rx)
				p.Blend(x+0.5, 0, C[0], 0.65*f)
				if math.Abs(x) < rx*0.6 {
					p.Blend(x+0.5, 1, C[0], 0.4*f)
				}
			}
			p.Glow(0, -1, 24, C[4], 0.2+0.05*math.Sin(tau*ph*2))
			crystalMotes(p, C, t, seed, Y, false)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph, _, Y := pose(t)
			R := 8.0
			// its own cold light
			p.Glow(0, Y, 34, C[3], 0.26)
			p.Glow(0, Y, 14, C[5], 0.18+0.08*math.Sin(tau*ph*3))
			// a sixth of a turn a loop: a hexagon looks the same after it, so the loop never seams
			faces := crystalFaces((tau/6)*ph, R)
			// a dark rim round the silhouette, then the facets: deep in shadow, pale toward the light, white in a glint
			var all [][2]float64
			for _, f := range faces {
				all = append(all, f.pts...)
			}
			hull := hullOf(all)
			mx, my := 0.0, 0.0
			for _, q := range hull {
				mx += q[0]
				my += q[1]
			}
			mx /= float64(len(hull))
			my /= float64(len(hull))
			rim := make([][2]float64, len(hull))
			for i, q := range hull {
				x, y := q[0], q[1]
				d := pixelfx.Hypot(x-mx, y-my)
				if d == 0 {
					d = 1
				}
				rim[i] = [2]float64{x + ((x-mx)/d)*1.2, y + Y + ((y-my)/d)*1.2}
			}
			p.Poly(rim, C[1], 1)
			shift := func(pts [][2]float64) [][2]float64 {
				out := make([][2]float64, len(pts))
				for i, q := range pts {
					out[i] = [2]float64{q[0], q[1] + Y}
				}
				return out
			}
			for _, f := range faces {
				c := pick(f.spec > 0.985, pixelfx.White, C[clampIdx(pixelfx.Round(1+math.Pow(f.lit, 1.4)*6.6), 1, 7)])
				p.Poly(shift(f.pts), c, 1)
			}
			// edges: pale where two faces meet, a dark rim round the outside, a white ridge on the lit side
			for _, f := range faces {
				for i := range f.pts {
					x0, y0, x1, y1 := f.pts[i][0], f.pts[i][1], f.pts[(i+1)%len(f.pts)][0], f.pts[(i+1)%len(f.pts)][1]
					p.Line(x0, y0+Y, x1, y1+Y, pick(f.lit > 0.6, C[7], pick(f.lit > 0.3, C[6], C[4])), 1)
				}
			}
			// glints: a star flashes on a face as it turns into the light
			for _, f := range faces {
				if f.spec > 0.96 {
					pixelfx.Sparkle(p, f.cx, f.cy+Y, pixelfx.Round(2+(f.spec-0.96)*150), C[6], C[4], 0.5)
				}
			}
			// a glint at the tip now and then
			if g := math.Mod(ph*2, 1); g < 0.12 {
				pixelfx.Sparkle(p, 0, Y-R*2.2*math.Cos(crystalTip), pixelfx.Round(5*math.Sin((g/0.12)*pi)), C[6], C[4], 0.6)
			}
			crystalMotes(p, C, t, seed, Y, true)
		},
	}
}
