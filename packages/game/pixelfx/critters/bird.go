// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/bird.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Bird is a songbird flying in place: bursts of wingbeats that lift its body on each downstroke, a glide on spread wings, its tail fanning, a loose feather drifting away. It is registered as "critters/bird".
var Bird = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Bird",
		Desc:     "A songbird flies in place: bursts of wingbeats that lift its body on each downstroke, a glide on spread wings, its tail fanning, a loose feather drifting away.",
		Tags:     []string{"critter", "bird", "flying", "animal", "loop"},
		Size:     [2]int{112, 80},
		Origin:   [2]int{56, 40},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a0e1e", "#18244a", "#24407a", "#3466aa", "#5a94d4", "#9cc8f0", "#eaf6ff")},
			{Name: "breast", Default: pixelfx.Palette("#4a1a0e", "#a8441c", "#e8783a", "#ffb070")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newBird,
}

func init() { pixelfx.Register("critters/bird", Bird) }

var (
	birdD                = 2.4
	birdBeak, birdBeakD  = pixelfx.Hex("#f0b030"), pixelfx.Hex("#a86a18")
	birdFlaps, birdGlide = 4.0, 1.5
	birdShoulder         = [2]float64{1, -2}
)

// The near wing's key poses, points from the shoulder: leading edge to the tip, then the trailing feathers back.
var (
	birdUp    = [8][2]float64{{0, 0}, {-1, -8}, {-5, -15}, {-8, -13.5}, {-10.5, -10.5}, {-11, -7}, {-9, -3}, {-6, 1}}
	birdMid   = [8][2]float64{{0, 0}, {-3, -2.5}, {-9, -2.5}, {-10.5, -0.5}, {-10, 1.2}, {-8.5, 2}, {-7, 2.2}, {-5, 2}}
	birdDown  = [8][2]float64{{0, 0}, {-1, 6}, {-3.5, 12}, {-6, 11}, {-7.5, 8.5}, {-8, 6}, {-7, 3}, {-5, 2}}
	birdFold  = [8][2]float64{{0, 0}, {-3, -4}, {-10, -5.5}, {-11, -4}, {-10, -2.5}, {-8.5, -1}, {-7, 0.5}, {-5, 1.5}}
	birdSpred = [8][2]float64{{0, 0}, {-2, -5}, {-7, -10}, {-10, -9}, {-12, -6.5}, {-11.5, -3.5}, {-9, -1}, {-6, 1}}
)

func birdMix(a, b [8][2]float64, u float64) [8][2]float64 {
	var out [8][2]float64
	for i := range a {
		out[i] = [2]float64{pixelfx.Lerp(a[i][0], b[i][0], u), pixelfx.Lerp(a[i][1], b[i][1], u)}
	}
	return out
}

type birdWing struct {
	pts  [8][2]float64
	down bool
	u    float64
}

// birdWingAt flaps until the glide, then glides on spread wings.
func birdWingAt(t float64) birdWing {
	t = pixelfx.Wrap(t, birdD)
	if t < birdGlide {
		u := math.Mod((t/birdGlide)*birdFlaps, 1)
		// a quick downstroke through the stretched-out middle, a slower upstroke folded close
		if u < 0.4 {
			k := pixelfx.Ease.InOutSine(u / 0.4)
			if k < 0.5 {
				return birdWing{pts: birdMix(birdUp, birdMid, k*2), down: true, u: u}
			}
			return birdWing{pts: birdMix(birdMid, birdDown, k*2-1), down: true, u: u}
		}
		k := pixelfx.Ease.InOutSine((u - 0.4) / 0.6)
		if k < 0.5 {
			return birdWing{pts: birdMix(birdDown, birdFold, k*2), u: u}
		}
		return birdWing{pts: birdMix(birdFold, birdUp, k*2-1), u: u}
	}
	g := (t - birdGlide) / (birdD - birdGlide)
	into, out := pixelfx.Smooth(0, 0.15, g), pixelfx.Smooth(0.85, 1, g)
	return birdWing{pts: birdMix(birdMix(birdUp, birdSpred, into), birdUp, out)}
}

// birdLift is how the body rises as the wings push down, and sinks a touch through the glide.
func birdLift(t float64) float64 {
	t = pixelfx.Wrap(t, birdD)
	y := 0.0
	for k := 0.0; k < 6; k++ {
		if birdWingAt(t - k*0.02).down {
			y -= 0.45
		}
	}
	if t >= birdGlide {
		y += 2.2 * math.Sin(pi*((t-birdGlide)/(birdD-birdGlide)))
	}
	return y + 1.2
}

func birdWingPts(w *birdWing, far bool) [][2]float64 {
	out := make([][2]float64, len(w.pts))
	for i, p := range w.pts {
		if far {
			out[i] = [2]float64{birdShoulder[0] + p[0] - 1, birdShoulder[1] + p[1]*0.85 - 1}
		} else {
			out[i] = [2]float64{birdShoulder[0] + p[0], birdShoulder[1] + p[1]}
		}
	}
	return out
}

func drawBirdGrid(g *grid, C, Br []uint32, t float64, w *birdWing) {
	g.clear()
	// the far wing, behind the body and in shadow
	g.poly(birdWingPts(w, true), C[1], 1)
	// the tail: three feathers fanning as it brakes and lifts
	tf := 0.25 + 0.35*math.Sin((t/birdD)*tau*2)
	for i := 0.0; i < 3; i++ {
		a, L := pi+(i-1)*tf*0.5+0.1, 9-math.Abs(i-1)
		c := C[2]
		if i == 0 {
			c = C[3]
		}
		g.poly([][2]float64{{-7, -2.4}, {-7 + math.Cos(a)*L, -1 + math.Sin(a)*L - 1.2}, {-7 + math.Cos(a)*L, -1 + math.Sin(a)*L + 1.2}, {-7, 0.6}}, c, 1)
	}
	// body and head: a blue back lit from above, a rust breast, a pale belly
	inBody := func(x, y float64) bool {
		return math.Pow((x+1)/8.5, 2)+math.Pow(y/5.2, 2) <= 1 || math.Pow(x-6.5, 2)+math.Pow(y+3, 2) <= math.Pow(4.6, 2)
	}
	g.shade(-10, -9, 12, 6, func(x, y float64, _ *gridOut) uint32 {
		if !inBody(x, y) {
			return 0
		}
		top, under := !inBody(x, y-2.2), !inBody(x+0.5, y+1.6)
		if y > -0.5 && x > -2.5 {
			if under {
				return Br[1]
			}
			if x > 3 && y < 2 {
				return Br[3]
			}
			return Br[2]
		}
		if y > 1.5 {
			if under {
				return C[4]
			}
			return C[5]
		}
		if top {
			if x > 2 {
				return C[5]
			}
			return C[4]
		}
		if y < -3.5 && x > 3 {
			return C[4]
		}
		return C[3]
	})
	// beak, eye with a glint, a dark cap stripe
	g.polyFn([][2]float64{{10.5, -4.5}, {14.5, -3}, {10.5, -2}}, func(_, y float64) uint32 {
		if y < -3 {
			return birdBeak
		}
		return birdBeakD
	}, 1)
	g.put(8, -4, C[0])
	g.put(8, -5, C[6])
	g.put(9, -4, C[0])
	g.line(5, -7, 9, -6.5, C[2])
	// the near wing: lit along its leading edge, the flight feathers splayed at the tips
	wp := birdWingPts(w, false)
	edge := func(x, y float64) float64 {
		m := math.Inf(1)
		for i := 1; i < 3; i++ {
			ax, ay, bx, by := wp[i-1][0], wp[i-1][1], wp[i][0], wp[i][1]
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
	g.polyFn(wp, func(x, y float64) uint32 {
		d := edge(x, y)
		if d < 1.2 {
			return C[5]
		}
		if d < 3 {
			return C[4]
		}
		return C[3]
	}, 1)
	// the flight feathers: dark lines splaying from the wrist to the trailing edge
	for i := 3; i < 7; i++ {
		g.line(wp[1][0], wp[1][1], pixelfx.Lerp(wp[1][0], wp[i][0], 0.92), pixelfx.Lerp(wp[1][1], wp[i][1], 0.92), C[2])
	}
	g.outline(C[0], 1)
}

func newBird(o *pixelfx.Options) pixelfx.Instance {
	C, Br, seed := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("breast"), 4), o.Seed()
	g := newGrid(-26, -26, 52, 46)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, time float64) {
		t := shift(time, birdD, seed)
		w, y := birdWingAt(t), birdLift(t)
		// on the downstroke, a see-through echo of the wing a moment before smears the beat
		if w.down && w.u > 0.1 {
			s := t - 0.05
			ws := birdWingAt(s)
			g.clear()
			g.poly(birdWingPts(&ws, false), C[6], 1)
			g.fillAlpha(0.42)
			g.draw(p, 1, 0, pixelfx.Round(birdLift(s)))
		}
		drawBirdGrid(g, C, Br, t, &w)
		g.draw(p, 1, 0, pixelfx.Round(y))
		// a loose feather drifts off and rocks down out of sight
		if fa := pixelfx.Wrap(t-0.45, birdD); fa < 1.8 {
			fx, fy, rock := -6-14*fa-3*math.Sin(fa*5), 4+9*fa+1.5*math.Cos(fa*5)*fa, math.Sin(fa*5)*0.8
			a := math.Min(1, (1.8-fa)*2)
			for i := -2.0; i <= 2; i++ {
				c := C[4]
				if i < 0 {
					c = C[5]
				}
				p.Dot(fx+i*math.Cos(rock), fy+i*math.Sin(rock)*0.8, c, a)
			}
			p.Dot(fx+2.6*math.Cos(rock), fy+2.6*math.Sin(rock)*0.8, C[3], a)
		}
		// a little moonlight on its back
		p.Glow(2, y-6, 16, C[5], 0.12)
	}}
}
