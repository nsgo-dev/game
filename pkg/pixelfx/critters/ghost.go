// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/ghost.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Ghost is a sheet ghost bobbing over the floor: its scalloped hem ripples and trails its drift, little arms flutter, it glances about and blinks, motes trickle off its hem. It is registered as "critters/ghost".
var Ghost = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ghost",
		Desc:     "A sheet ghost bobs over the floor: its scalloped hem ripples and trails its drift, little arms flutter, it glances about and blinks, motes trickle off its hem.",
		Tags:     []string{"critter", "ghost", "spirit", "undead", "spooky", "float", "loop"},
		Size:     [2]int{96, 100},
		Origin:   [2]int{48, 90},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#10142a", "#232c52", "#3e4f86", "#6b84bb", "#a3b8e2", "#d8e4ff", "#ffffff")},
			{Name: "hover", Default: pixelfx.Num(14)},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newGhost,
}

func init() { pixelfx.Register("critters/ghost", Ghost) }

var (
	ghostD, ghostR = 2.4, 10.5
	ghostLobes     = 4.0
)

// ghostAt is where the ghost is: a lazy drift side to side and a bob, both looping.
func ghostAt(t, hover float64) (x, y float64) {
	a := (t / ghostD) * tau
	return 4 * math.Sin(a), -hover - 3*math.Sin(2*a+0.8)
}

// ghostHalfW is the sheet's half-width h px above the hem: a dome over a body that flares toward the hem; -1 outside it.
func ghostHalfW(h float64) float64 {
	R := ghostR
	top := 2*R + 9
	if h > top || h < -1 {
		return -1
	}
	if h > top-R {
		return math.Sqrt(math.Max(0, R*R-math.Pow(h-(top-R), 2)))
	}
	return pixelfx.Lerp(R+2.2, R, math.Pow(h/(top-R), 0.7))
}

// drawGhostSheet draws the sheet into g and returns its height.
func drawGhostSheet(g *grid, C []uint32, t, lagX float64) float64 {
	g.clear()
	R, NL := ghostR, ghostLobes
	top, ph, HW := 2*R+9, (t/ghostD)*tau*2, R+2.2
	LW := (2 * HW) / NL
	// the lower sheet trails behind the head as it drifts
	off := func(h float64) float64 { return lagX * math.Pow(1-pixelfx.Clamp(h/top), 1.4) }
	// four rounded lobes along the hem, lifting one after another in a ripple
	lobe := func(X float64) (u, d float64) {
		j := pixelfx.ClampTo(math.Floor((X+HW)/LW), 0, NL-1)
		c := -HW + (j+0.5)*LW
		u = (X - c) / (LW / 2)
		return u, (2.2 + 1.1*math.Sin(ph-j*1.5)) * math.Sqrt(math.Max(0, 1-u*u))
	}
	inside := func(x, y float64) bool {
		h := -y
		X := x - off(h)
		if _, d := lobe(X); h < 0 && -h > d {
			return false
		}
		hw := ghostHalfW(math.Max(0, h))
		return hw >= 0 && math.Abs(X) <= hw
	}
	// little arms: rounded nubs that flutter
	arm := func(x, y float64) bool {
		for _, s := range [2]float64{-1, 1} {
			ax, ay := s*(R+1.2)+off(13), -13+1.6*math.Sin(ph+s*1.3)
			if math.Pow((x-ax)/3, 2)+math.Pow((y-ay)/2.2, 2) <= 1 {
				return true
			}
		}
		return false
	}
	anyOf := func(x, y float64) bool { return inside(x, y) || arm(x, y) }
	lx, ly := -9.0, -top-1
	lit := func(x, y, k float64) bool { return anyOf(lx+(x-lx)/k, ly+(y-ly)/k) }
	g.shade(-19, -top-2, 19, 5, func(x, y float64, o *gridOut) uint32 {
		if !anyOf(x, y) {
			return 0
		}
		h := -y
		X := x - off(h)
		i := 4
		if lit(x, y, 0.62) {
			i = 5
		}
		if !anyOf(x+2.4, y+1.8) {
			i = 3
		}
		if !anyOf(x+1.2, y+1) && X > 2 {
			i = 2
		}
		// the gloss: a thin bright arc just inside the top-left of the dome
		if h > top-R-2 && X < 1 && !anyOf(x-1.5, y-1.5) && anyOf(x-0.6, y-0.6) {
			i = 6
		}
		// the hem: each lobe darker underneath and to its right, a crease where two lobes meet
		if h < 1 {
			if u, d := lobe(X); u > 0.1 || -h > d-1.2 {
				i = min(i, 3)
			}
		}
		// folds: short creases running down into the notches between the lobes, lit on their left
		if h < 7 && h >= -0.5 {
			for j := 1.0; j < NL; j++ {
				cx, w := -HW+j*LW+0.6*math.Sin(ph-j), (7-h)*0.09
				if math.Abs(X-cx) <= w+0.3 {
					i = min(i, 3)
				} else if cx-X > w+0.3 && cx-X < w+1.3 && i == 4 {
					i = 5
				}
			}
		}
		o.a = 0.95
		if h < 2 {
			o.a = 0.86
		}
		return C[i]
	})
	return top
}

func newGhost(o *pixelfx.Options) pixelfx.Instance {
	C, hover, seed := pixelfx.Fit(o.Palette(), 7), o.Num("hover"), o.Seed()
	g := newGrid(-20, -40, 40, 46)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, ghostD, seed)
			sx, sy := ghostAt(t, hover)
			hgt := pixelfx.Clamp((-sy - 8) / 16)
			// its shadow on the floor, and the floor lit cold under it
			shadow(p, sx, 0, 11-3*hgt, 1.6, C[0], 0.55-0.2*hgt)
			for x := -24.0; x <= 24; x += 6 {
				p.Glow(sx+x, -1, 16, C[4], 0.14*(1-math.Abs(x)/34)*(1-0.4*hgt))
			}
		},
		DrawFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, ghostD, seed)
			sx, sy := ghostAt(t, hover)
			lx, _ := ghostAt(t-0.3, hover)
			top := drawGhostSheet(g, C, t, lx-sx)
			// the face: two eyes that glance about and blink, a little mouth that oohs
			w := pixelfx.Wrap(t, ghostD)
			look := 0.0
			switch {
			case w < 0.7:
			case w < 1.3:
				look = 1
			case w < 1.9:
				look = -1
			}
			blink := w > 1.95 && w < 2.03
			ey, mo := -top+ghostR-1, 0.5+0.5*math.Sin((t/ghostD)*tau*2)
			for _, s := range [2]float64{-1, 1} {
				ex := s*4 + look
				if s > 0 {
					ex = s*4 - 1 + look
				}
				if blink {
					g.put(ex, ey+2, C[1])
					g.put(ex+1, ey+2, C[1])
					continue
				}
				for k := 0.0; k < 4; k++ {
					g.put(ex, ey+k, C[0])
					g.put(ex+1, ey+k, C[0])
				}
				gx := ex + 1
				if look < 0 {
					gx = ex
				}
				g.put(gx, ey, C[2])
			}
			my := ey + 6
			g.put(look-1, my, C[1])
			g.put(look, my, C[1])
			if mo > 0.4 {
				g.put(look-1, my+1, C[0])
				g.put(look, my+1, C[0])
			}
			g.outlineFn(func(_, _, _, ny float64) uint32 {
				if ny > 0 {
					return C[2]
				}
				return C[1]
			}, 0.95)
			g.draw(p, 1, sx, sy)
			// motes trickle off the hem and fade as they sink and drift
			pixelfx.Cycle(t, ghostD, 12, seed*3+1, func(r *pixelfx.Rng) float64 { return r.Range(1, 1.7) }, func(age, L float64, r *pixelfx.Rng, i int) {
				s0x, s0y := ghostAt(pixelfx.Wrap(t-age, ghostD), hover)
				k, fi := age/L, float64(i)
				x0, y0 := s0x+r.Range(-10, 10), s0y+1.5
				x, y := x0+math.Sin(age*2.2+fi)*2.4, y0+r.Range(5, 11)*age-3*age*age
				a, tw := math.Min(1, k*6)*math.Min(1, (1-k)*2.2), 0.6+0.4*math.Sin(age*8+fi*1.7)
				c := C[5]
				if tw > 0.8 {
					c = C[6]
				}
				p.Dot(x, y, c, a)
				if r.Chance(0.3) {
					p.Dot(x, y-1, C[4], a*0.7)
				}
				p.Glow(x, y, 7, C[4], 0.42*a*tw)
			})
			// a cold glow round it
			p.Glow(sx, sy-top*0.55, 54, C[3], 0.3)
			p.Glow(sx-3, sy-top*0.72, 14, C[5], 0.12)
		},
	}
}
