// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/butterfly.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Butterfly is an iridescent butterfly wandering a loop in bursts of quick wingbeats and open-winged glides, its veined, spotted wings shedding a twinkling trail of glowing dust. It is registered as "critters/butterfly".
var Butterfly = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Butterfly",
		Desc:     "An iridescent butterfly wanders a loop in bursts of quick wingbeats and open-winged glides, its veined, spotted wings shedding a twinkling trail of glowing dust.",
		Tags:     []string{"critter", "butterfly", "insect", "flying", "magic", "glow", "loop"},
		Size:     [2]int{128, 88},
		Origin:   [2]int{64, 44},
		Duration: 3.2,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0b0f24", "#16224f", "#1f3f8a", "#2f74c8", "#4fb4f0", "#a0e6ff", "#ffffff")},
			{Name: "spots", Default: pixelfx.Palette("#3a1a08", "#c8641a", "#ffb43c", "#fff0b0")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newButterfly,
}

func init() { pixelfx.Register("critters/butterfly", Butterfly) }

var butterflyD = 3.2

// The right wings, unfolded, in px from the wing root (y forward is negative).
var (
	butterflyFore = [][2]float64{{0, -1}, {3, -6}, {8, -11}, {13, -12.5}, {16.5, -10.5}, {16.5, -6}, {13, -1.5}, {7, 1}, {2, 1.5}}
	butterflyHind = [][2]float64{{0, 1}, {6, 1.5}, {11, 4}, {12.5, 8.5}, {10.5, 12.5}, {6.5, 13.5}, {3, 11}, {0.5, 6}}
)

// butterflyBursts are the bursts of wingbeats (rising); between them it glides (sinking), wandering round a loop.
var butterflyBursts = [2][2]float64{{0, 0.95}, {1.5, 2.55}}

func butterflyFlapping(t float64) bool {
	for _, b := range butterflyBursts {
		if t >= b[0] && t < b[1] {
			return true
		}
	}
	return false
}

// butterflyFold is the wing's fold, 1 flat open .. 0 edge-on: quick beats while flapping, held open while gliding.
func butterflyFold(t float64) float64 {
	t = pixelfx.Wrap(t, butterflyD)
	for _, b := range butterflyBursts {
		a, e := b[0], b[1]
		if t >= a && t < e {
			n := pixelfx.Round((e - a) * 6.3)
			ph := ((t - a) / (e - a)) * n
			return 0.14 + 0.86*(0.5+0.5*math.Cos(ph*tau))
		}
	}
	return 1
}

func butterflyAt(t float64) (x, y float64) {
	t = pixelfx.Wrap(t, butterflyD)
	a := (t / butterflyD) * tau
	// a lift while it beats, a sink while it glides, so the path bobs in steps
	lift := 0.0
	for _, b := range butterflyBursts {
		lift += -3 * math.Sin(pi*pixelfx.Clamp((t-b[0])/(b[1]-b[0])))
	}
	return 30*math.Sin(a) + 6*math.Sin(3*a), 12*math.Sin(2*a+0.4) + lift
}

func edgeDist(pts [][2]float64, x, y float64) float64 {
	m := math.Inf(1)
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		ax, ay, bx, by := pts[j][0], pts[j][1], pts[i][0], pts[i][1]
		dx, dy := bx-ax, by-ay
		L2 := dx*dx + dy*dy
		if L2 == 0 {
			L2 = 1
		}
		u := pixelfx.Clamp(((x-ax)*dx + (y-ay)*dy) / L2)
		m = math.Min(m, pixelfx.Hypot(ax+dx*u-x, ay+dy*u-y))
	}
	return m
}

var (
	butterflySpots     = [4][2]float64{{13.2, -10.2}, {15.2, -8.4}, {14.6, -5.6}, {11.6, -11.4}}
	butterflyForeVeins = [3]float64{-1.2, -0.75, -0.3}
	butterflyHindVeins = [3]float64{0.5, 1.0, 1.45}
)

// butterflyWingColor is a wing's pattern at an unfolded point: dark at the root, iridescent blue brightest along the leading edge, thin dark veins, a black apex dotted white on the forewing, a gold eyespot on the hindwing.
func butterflyWingColor(C, Sp []uint32, hind bool, u, v float64) uint32 {
	pts := butterflyFore
	if hind {
		pts = butterflyHind
	}
	e, r, ang := edgeDist(pts, u, v), pixelfx.Hypot(u, v), math.Atan2(v, u)
	if e < 1 {
		return C[0]
	}
	if !hind {
		if u+v*-0.45 > 14.2 || (r > 12.5 && v < -4) {
			for _, s := range butterflySpots {
				if pixelfx.Hypot(u-s[0], v-s[1]) < 0.9 {
					return C[6]
				}
			}
			return C[0]
		}
	} else {
		if e < 2 && r > 8 {
			if math.Sin(ang*10) > 0.5 {
				return C[5]
			}
			return C[1]
		}
		d := pixelfx.Hypot(u-7.6, v-9.4)
		if d < 0.7 {
			return C[6]
		}
		if d < 1.4 {
			return C[0]
		}
		if d < 2.2 {
			return Sp[2]
		}
	}
	veins := butterflyForeVeins
	if hind {
		veins = butterflyHindVeins
	}
	for _, va := range veins {
		if math.Abs(ang-va)*r < 0.45 && r > 3.5 {
			return C[1]
		}
	}
	if r < 3 {
		return C[2]
	}
	if r < 6 {
		return C[3]
	}
	if !hind && e < 2.4 && v < -3 {
		return C[5]
	}
	return C[4]
}

func drawButterflyGrid(g *grid, C, Sp []uint32, w float64) {
	g.clear()
	for _, s := range [2]float64{-1, 1} {
		for _, hind := range [2]bool{true, false} {
			pts := butterflyFore
			if hind {
				pts = butterflyHind
			}
			g.shade(-20, -15, 20, 16, func(x, y float64, _ *gridOut) uint32 {
				if pixelfx.Sign(x+0.001) != s {
					return 0
				}
				u, v := (math.Abs(x)-1)/w, y
				if u < 0 || !inPoly(pts, u, v) {
					return 0
				}
				c := butterflyWingColor(C, Sp, hind, u, v)
				// folding up, the far side of each wing turns away from the light
				if w < 0.5 && c != C[0] {
					c = pixelfx.Mix(c, C[1], (0.5-w)*1.6)
				}
				return c
			})
		}
	}
	// the body: a fuzzy thorax, a tapering abdomen, a round head, curled antennae with bright tips
	g.ellipse(0, 0.5, 1.6, 2.6, func(nx, ny float64) uint32 {
		if nx+ny < -0.4 {
			return C[3]
		}
		return C[1]
	}, 1)
	for y := 3.0; y < 10; y++ {
		if y < 6 {
			g.put(-1, y, C[2])
		} else {
			g.put(-1, y, C[1])
		}
		if y < 8 {
			g.put(0, y, C[1])
		}
	}
	g.put(-1, -3, C[3])
	g.put(0, -3, C[2])
	g.put(-1, -4, C[2])
	g.put(0, -4, C[1])
	for _, s := range [2]float64{-1, 1} {
		tx, x0 := s*4-1, -1.0
		if s > 0 {
			tx, x0 = s*4, 0
		}
		g.line(x0, -5, tx, -9.5, C[2])
		g.put(tx, -9.5, C[6])
	}
	g.outline(C[0], 1)
}

func newButterfly(o *pixelfx.Options) pixelfx.Instance {
	C, Sp, seed := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("spots"), 4), o.Seed()
	g := newGrid(-22, -20, 44, 40)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, time float64) {
		t := shift(time, butterflyD, seed)
		sx, sy := butterflyAt(t)
		w := butterflyFold(t)
		// a trail of glowing dust shaken off the wings, drifting down and twinkling out
		pixelfx.Cycle(t, butterflyD, 56, seed*7+1, func(r *pixelfx.Rng) float64 { return r.Range(1, 1.8) }, func(age, L float64, r *pixelfx.Rng, i int) {
			born := pixelfx.Wrap(t-age, butterflyD)
			bx, by := butterflyAt(born)
			k, fi := age/L, float64(i)
			if !butterflyFlapping(born) && r.Chance(0.45) {
				return
			}
			x := bx + r.Range(-9, 9) + math.Sin(age*3+fi)*1.5
			y := by + r.Range(-4, 8) + 7*age + 3*age*age
			tw, a := 0.5+0.5*math.Sin(age*11+fi*2.1), math.Min(1, k*8)*(1-k)
			var c uint32
			switch {
			case tw > 0.7:
				c = C[6]
			case r.Chance(0.3):
				c = Sp[2]
			default:
				c = C[5]
			}
			p.Dot(x, y, c, a*1.2)
			p.Glow(x, y, 6, C[4], 0.5*a*tw)
			if i%9 == 0 && tw > 0.85 && k < 0.5 {
				pixelfx.Sparkle(p, x, y, 2, C[6], C[5], 0.3*a)
			}
		})
		drawButterflyGrid(g, C, Sp, w)
		g.draw(p, 1, sx, sy)
		// the wings shine: a soft blue light round it, brighter when they open
		p.Glow(sx, sy, 38, C[4], 0.16+0.14*w)
		p.Glow(sx, sy, 12, C[5], 0.12+0.1*w)
	}}
}
