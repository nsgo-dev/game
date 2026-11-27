// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/fish-school.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the caustic light and the bubbles from Sprite Fusion's open-source bubbles (https://www.spritefusion.com/pixel-art-vfx/bubbles), MIT License, Copyright (c) 2026 Hugo Duprez.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FishSchool is a school of fish holding station in two depth rows: bodies wag into swinging tails, one darts ahead now and then, bubbles rise and caustic light ripples over them. It is registered as "critters/fish-school".
var FishSchool = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fish school",
		Desc:     "A school of fish holds station in two depth rows: bodies wag into swinging tails, one darts ahead now and then, bubbles rise and caustic light ripples over them.",
		Tags:     []string{"critter", "fish", "school", "sea", "underwater", "swim", "loop"},
		Size:     [2]int{136, 80},
		Origin:   [2]int{68, 40},
		Duration: 4.8,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a1a2a", "#123552", "#1d5a7e", "#2f8aa8", "#62bccc", "#bfe8e6", "#f4fff8")},
			{Name: "stripe", Default: pixelfx.Palette("#5a1e14", "#d8582a", "#ffa04a")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newFishSchool,
}

func init() { pixelfx.Register("critters/fish-school", FishSchool) }

var (
	fishD                    = 4.8
	fishDorsal0, fishDorsal1 = 0.34, 0.66
)

// fishSpec is one fish of the school: where it holds station, its size, whether it swims in the back row, and its phase.
type fishSpec struct {
	x, y, size float64
	back       bool
	ph         float64
}

var fishSchool = [7]fishSpec{
	{-34, -16, 0.72, true, 0.1}, {2, -20, 0.68, true, 0.55}, {30, -10, 0.7, true, 0.3},
	{-22, 4, 1, false, 0.8}, {12, -4, 1, false, 0.2}, {-40, -2, 0.95, false, 0.45}, {26, 14, 0.96, false, 0.65},
}

// drawFish draws one fish into g, its nose at (0, 0), facing right, its body trailing back to the left.
func drawFish(g *grid, C, Sp []uint32, s, t, ph, dart float64) {
	g.clear()
	beats := 12.0
	if dart > 0 {
		beats += 6
	}
	L, H, w := 15*s, 8*s, (t/fishD)*tau*beats+ph*tau
	amp := (1.3 + 1.2*dart) * s
	bend := func(u float64) float64 { return amp * math.Pow(u, 1.8) * math.Sin(w-4*u) }
	prof := func(u float64) float64 {
		if u < 0.3 {
			return math.Sqrt(u / 0.3)
		}
		return 1 - math.Pow((u-0.3)/0.7, 1.5)*0.72
	}
	hh := func(u float64) float64 { return (H / 2) * prof(u) }
	// body: dark back, lighter flank, silver belly; a stripe down the side; light rippling on its back
	g.shade(-L, -H, 0, H, func(x, y float64, _ *gridOut) uint32 {
		u := -x / L
		dy, h := y-bend(u), hh(u)
		if u < 0 || u > 1 || math.Abs(dy) > h {
			return 0
		}
		v := dy / h
		if v > -0.12 && v < 0.12 && u > 0.22 && u < 0.92 {
			if u < 0.3 || v >= 0 {
				return Sp[1]
			}
			return Sp[2]
		}
		if v < -0.6 {
			if u < 0.5 && v < -0.8 {
				return C[3]
			}
			return C[2]
		}
		if v < -0.12 {
			return C[3]
		}
		if v < 0.5 || v > 0.86 {
			return C[4]
		}
		return C[5]
	})
	// the tail: a forked fin swinging off the end of the wave, lit along its upper lobe
	tb, ta, TL, hb := bend(1), math.Atan(amp*1.8*0.9*math.Cos(w-4))*0.8, 6*s, hh(1)
	rot := func(dx, dy float64) [2]float64 {
		return [2]float64{-(L + dx*math.Cos(ta) - dy*math.Sin(ta)), tb + dx*math.Sin(ta) + dy*math.Cos(ta)}
	}
	g.polyFn([][2]float64{rot(-0.5, -hb), rot(TL, -hb-3.4*s), rot(TL*0.62, 0), rot(TL, hb+3.4*s), rot(-0.5, hb)}, func(_, y float64) uint32 {
		if y < tb-1 {
			return C[4]
		}
		return C[3]
	}, 0.88)
	// the dorsal fin: swept back, ribbed; a pectoral fin paddling, the gill, the eye
	d0, d1 := fishDorsal0, fishDorsal1
	g.polyFn([][2]float64{{-d0 * L, bend(d0) - hh(d0) + 0.5}, {-(d0 + 0.08) * L, bend(d0) - hh(d0) - 3.6*s}, {-d1 * L, bend(d1) - hh(d1) + 0.5}}, func(x, _ float64) uint32 {
		if math.Mod(pixelfx.Round(x), 2) != 0 {
			return C[3]
		}
		return C[4]
	}, 0.85)
	pf := math.Sin(w*0.5) * 1.2
	g.set(-0.34*L, bend(0.34)+1.5*s+pf*0.3, C[4], 0.85, 0)
	g.set(-0.34*L-1, bend(0.34)+2*s+pf*0.6, C[4], 0.85, 0)
	for y := -2.0; y <= 1; y++ {
		g.put(-0.26*L, bend(0.26)+y*s, C[2])
	}
	ex, ey := -0.13*L-1, bend(0.13)-0.25*H
	g.put(ex, ey, C[0])
	g.put(ex+1, ey, C[0])
	g.put(ex, ey+1, C[1])
	g.put(ex+1, ey+1, C[0])
	g.put(ex+1, ey, C[6])
	g.outline(C[0], 1)
}

// fishPlace is where a fish is: holding station in the current, swaying; one darts ahead and drifts back once a loop.
func fishPlace(f *fishSpec, i int, t float64) (x, y, dart float64) {
	a := (t / fishD) * tau
	dt := pixelfx.Wrap(t-math.Mod(float64(i)*0.71*fishD, fishD), fishD)
	d := 0.0
	if i == 4 {
		switch {
		case dt < 0.25:
			d = pixelfx.Ease.OutCubic(dt / 0.25)
		case dt < 1.6:
			d = 1 - pixelfx.Ease.InOutSine((dt-0.25)/1.35)
		}
	}
	if i == 4 && dt < 0.6 {
		dart = 1 - dt/0.6
	}
	return f.x + 3*math.Sin(a+f.ph*tau) + 8*d, f.y + 1.8*math.Sin(2*a+f.ph*5), dart
}

// fishCaustics is the light that comes down through rippling water, as bright crossing lines.
func fishCaustics(p *pixelfx.Painter, C []uint32, t float64) {
	a := (t / fishD) * tau
	for y := -38.0; y < 38; y++ {
		for x := -66.0; x < 66; x++ {
			fall := pixelfx.Clamp(1-(math.Pow(x/64, 2)+math.Pow(y/40, 2))) * (0.6 + 0.4*pixelfx.Clamp(-y/40+0.6))
			if fall <= 0 {
				continue
			}
			u, v := x*0.16+1.9*math.Sin(y*0.12+a), y*0.19+1.9*math.Sin(x*0.1-a)
			n1, sh := math.Sin(u)+math.Sin(v), 0.75+0.25*math.Sin(x*0.05-y*0.04+2*a)
			f := math.Max(0, 1-math.Abs(n1)*5) * fall * sh
			if f > 0.55 {
				p.Light(x, y, C[4], 0.3)
			} else if f > 0.25 {
				p.Light(x, y, C[3], 0.14)
			}
		}
	}
}

var fishShafts = [3][3]float64{{-30, 10, 0}, {14, 7, 2.1}, {44, 12, 4}}

func newFishSchool(o *pixelfx.Options) pixelfx.Instance {
	C, Sp, seed := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("stripe"), 3), o.Seed()
	g := newGrid(-28, -12, 34, 24)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, time float64) {
		t := shift(time, fishD, seed)
		// shafts of light slanting down through the water
		for _, s := range fishShafts {
			sw := math.Sin((t/fishD)*tau+s[2]) * 4
			p.GlowLine(s[0]+sw-12, -40, s[0]+sw+8, 40, s[1], C[4], 0.05)
		}
		// the back row first, dimmed into the water; then the front row
		for _, back := range [2]bool{true, false} {
			for i := range fishSchool {
				f := &fishSchool[i]
				if f.back != back {
					continue
				}
				x, y, dart := fishPlace(f, i, t)
				drawFish(g, C, Sp, f.size, t, f.ph, dart)
				if back {
					g.recolor(func(_, _ float64, c uint32, _ float64, _ uint8) uint32 { return pixelfx.Mix(c, C[1], 0.42) })
				}
				g.draw(p, 1, x, y)
			}
		}
		// bubbles let go from their mouths now and then
		pixelfx.Cycle(t, fishD, 9, seed*13+1, func(r *pixelfx.Rng) float64 { return r.Range(1.6, 2.6) }, func(age, L float64, r *pixelfx.Rng, i int) {
			fi := i % len(fishSchool)
			f := &fishSchool[fi]
			px, py, _ := fishPlace(f, fi, pixelfx.Wrap(t-age, fishD))
			rad := r.Range(0.6, 1.6) * f.size
			ph := r.Next() * tau
			x, y := px+1+math.Sin(age*(4+2/rad)+ph)*(0.5+rad*0.7), py-1-(16+12*rad)*age
			if y < -38 {
				return
			}
			bubble(p, x, y, rad, [4]uint32{C[6], C[5], C[4], C[3]}, math.Min(math.Min(math.Min(1, age*4), (L-age)*3), (y+38)/4), 0)
		})
		fishCaustics(p, C, t)
		p.Glow(0, -8, 74, C[2], 0.2)
		p.Glow(-6, -18, 40, C[3], 0.1)
	}}
}
