// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/snake.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Snake is a snake slithering along the floor, head raised: humps rise and roll back down its patterned body to the tail, dust puffs where it touches down, its forked tongue flicks. It is registered as "critters/snake".
var Snake = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Snake",
		Desc:     "A snake slithers along the floor, head raised: humps rise and roll back down its patterned body to the tail, dust puffs where it touches down, its forked tongue flicks.",
		Tags:     []string{"critter", "snake", "reptile", "ground", "loop"},
		Size:     [2]int{136, 56},
		Origin:   [2]int{68, 48},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#081410", "#123a1e", "#1d5e2a", "#2f8a36", "#5cb848", "#a8e070", "#e8ffc0")},
			{Name: "belly", Default: pixelfx.Palette("#4a3a14", "#b08a34", "#f0d878")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newSnake,
}

func init() { pixelfx.Register("critters/snake", Snake) }

var (
	snakeD, snakeHead, snakeLen = 2.4, 30.0, 70.0
	snakeTongue, snakeDust      = pixelfx.Hex("#e8304a"), pixelfx.Hex("#8a7f92")
	snakeK, snakeW              = tau / 30, (tau * 2) / snakeD
	snakeFlicks                 = [2]float64{0.3, 1.5}
)

// snakeLift is the body's centre height u px behind the head: humps rolling back toward the tail, the neck raised to the head.
func snakeLift(u, t float64) float64 {
	wave := math.Pow(math.Max(0, math.Sin(snakeK*u-snakeW*t)), 0.85)
	env := pixelfx.Smooth(8, 18, u) * (1 - pixelfx.Smooth(snakeLen-14, snakeLen-2, u))
	neck := 0.0
	if u < 12 {
		neck = 6 * math.Pow(1-u/12, 2)
	}
	return 7*wave*env + neck
}

func snakeRadius(u float64) float64 {
	if u < 6 {
		return 3.3
	}
	return pixelfx.Lerp(3.4, 0.6, math.Pow((u-6)/(snakeLen-6), 1.3))
}

// snakeBob is the head's bob with the rhythm.
func snakeBob(t float64) float64 { return 0.8 * math.Sin(snakeW*t) }

// snakeTongueOut is how far the tongue flicks: two bursts of three a loop.
func snakeTongueOut(t float64) float64 {
	for _, t0 := range snakeFlicks {
		if k := (t - t0) / 0.5; k >= 0 && k < 1 {
			return math.Pow(math.Max(0, math.Sin(k*pi*3)), 0.6)
		}
	}
	return 0
}

func drawSnakeGrid(g *grid, C, B []uint32, t float64) {
	g.clear()
	hb := snakeBob(t)
	// the body, column by column from the tail to the neck: lit along its back, a gold belly, dark diamonds, glints on the humps
	for u := snakeLen; u >= 2; u -= 0.5 {
		x, r0 := snakeHead-u, snakeRadius(u)
		cy := -r0 - snakeLift(u, t)
		if u < 12 {
			cy += hb * (1 - u/12)
		}
		hump := snakeLift(u, t)
		// keep the body as thick across its slope as along the floor
		slope := snakeLift(u+0.5, t) - snakeLift(u-0.5, t)
		r := r0 * math.Sqrt(1+slope*slope)
		for y := math.Floor(cy - r); y <= math.Ceil(cy+r); y++ {
			v := (y + 0.5 - cy) / r
			if math.Abs(v) > 1 {
				continue
			}
			var c uint32
			switch {
			case v < -0.5:
				c = C[4]
			case v < 0.1:
				c = C[3]
			case v < 0.45:
				c = C[2]
			case v < 0.8:
				c = B[1]
			default:
				c = B[0]
			}
			dm := math.Abs(math.Mod(u+2, 7)-3.5)/3.5 + math.Abs(v+0.25)/0.55
			if dm < 0.85 && v < 0.35 && u > 4 {
				c = C[2]
				if dm < 0.5 {
					c = C[1]
				}
			}
			if v < -0.62 && hump > 4.5 && dm >= 0.85 {
				c = C[5]
				if hump > 6 {
					c = C[6]
				}
			}
			if v > 0.45 && v < 0.8 && math.Mod(math.Floor(u), 3) == 0 {
				c = B[2]
			}
			g.put(x, y, c)
		}
	}
	// the head: a blunt wedge raised off the floor, lit on top, an eye with a slit pupil, a nostril, a mouth line
	hx, hy := snakeHead+2, -9.5+hb
	g.shade(hx-5, hy-4, hx+6, hy+4, func(x, y float64, _ *gridOut) uint32 {
		rx := 4.6
		if x > hx {
			rx = 5.6
		}
		dx, dy := (x-hx)/rx, (y-hy)/(3.6-math.Max(0, x-hx)*0.18)
		if dx*dx+dy*dy > 1 {
			return 0
		}
		if dy > 0.55 {
			return B[1]
		}
		if dy < -0.3 {
			if dx < 0.3 {
				return C[5]
			}
			return C[4]
		}
		return C[3]
	})
	g.line(hx-1, hy+1.5, hx+5, hy+1.2, C[1])
	g.put(hx+1, hy-2, C[0])
	g.put(hx+2, hy-2, B[2])
	g.put(hx+1, hy-1, B[2])
	g.put(hx+2, hy-1, C[0])
	g.put(hx+1, hy-3, C[6])
	g.put(hx+5, hy-1, C[1])
	g.outline(C[0], 1)
}

func newSnake(o *pixelfx.Options) pixelfx.Instance {
	C, B, seed := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("belly"), 3), o.Seed()
	g, sw := newGrid(-44, -24, 88, 26), newSwarm()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, snakeD, seed)
			// a shadow along its length, softer under the humps
			for u := 2.0; u <= snakeLen; u += 2 {
				h, r := snakeLift(u, t), snakeRadius(u)
				shadow(p, snakeHead-u, -0.2, 2.2+r*0.4, 1.2, C[0], 0.5*(1-pixelfx.Clamp(h/10))+0.1)
			}
			shadow(p, snakeHead+2, -0.2, 5, 1.2, C[0], 0.35)
		},
		DrawFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, snakeD, seed)
			hb := snakeBob(t)
			// the tongue darts out, forked, and flicks back
			e, tx, ty := snakeTongueOut(t), snakeHead+7.5, -8.2+hb
			if e > 0.05 {
				L, wig := 7*e, math.Sin(t*60)*0.6*e
				p.Line(tx, ty, tx+L, ty+wig, snakeTongue, 1)
				if L > 3 {
					p.Px(tx+L+1, ty+wig-1, snakeTongue)
					p.Px(tx+L+1, ty+wig+1, snakeTongue)
				}
			}
			drawSnakeGrid(g, C, B, t)
			g.draw(p, 1, 0, 0)
			// dust scuffed along the floor where each hump presses back down, drifting off low and thin
			sw.begin(t)
			pixelfx.Cycle(t, snakeD, 26, seed*7+1, func(r *pixelfx.Rng) float64 { return r.Range(0.6, 1.1) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				born, m := pixelfx.Wrap(t-age, snakeD), r.Int(0, 2)
				u := (pi + snakeW*born + m*tau) / snakeK
				if u < 18 || u > snakeLen-10 {
					return
				}
				x := snakeHead - u + r.Range(-2, 2) - r.Range(6, 16)*age
				sw.add(pixelfx.Smoke, x, -0.5-age*r.Range(3, 8), 0, 0, age, L, snakeDust, r.Range(1.8, 3.4), 0, 0)
			})
			sw.draw(p)
			// a glint in its eye
			p.Glow(snakeHead+3.5, -11+hb, 4, B[2], 0.35)
		},
	}
}
