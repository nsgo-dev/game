// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/bat.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Bat is a bat flapping in four snappy wing frames along a figure-eight, its body lifting on each downbeat, moonlight on its wing bones, red eyes glowing and glinting. It is registered as "critters/bat".
var Bat = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Bat",
		Desc:     "A bat flaps in four snappy wing frames along a figure-eight, its body lifting on each downbeat, moonlight on its wing bones, red eyes glowing and glinting.",
		Tags:     []string{"critter", "bat", "night", "flying", "spooky", "loop"},
		Size:     [2]int{112, 80},
		Origin:   [2]int{56, 40},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c0912", "#1f1629", "#382645", "#5b3f66", "#866089", "#b88fb0", "#e8cfe0")},
			{Name: "eyes", Default: pixelfx.Palette("#8a1020", "#ff3b30", "#ffb04a", "#fff0c0")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newBat,
}

func init() { pixelfx.Register("critters/bat", Bat) }

var batD, batFlaps = 2.4, 6.0

// batFrame is one of the four wing frames of the right wing: arm angle and length, three fingers' angles and lengths, the lift.
type batFrame struct {
	arm, al float64
	f, fl   [3]float64
	lift    float64
}

var batFrames = [4]batFrame{
	{arm: -58, al: 8, f: [3]float64{-112, -76, -38}, fl: [3]float64{9, 11, 9}, lift: 1},
	{arm: -18, al: 9, f: [3]float64{-40, -12, 16}, fl: [3]float64{10, 11, 9}, lift: 0},
	{arm: 28, al: 8, f: [3]float64{50, 72, 94}, fl: [3]float64{9, 10, 8}, lift: -2},
	{arm: -42, al: 6, f: [3]float64{-66, -38, -10}, fl: [3]float64{7, 8, 6}, lift: -1},
}

func batRad(d float64) float64 { return (d * pi) / 180 }

// batAt is the figure-eight flight.
func batAt(t float64) (x, y float64) {
	a := (t / batD) * tau
	return 22 * math.Sin(a), 7 * math.Sin(2*a)
}

// batFrameAt is which wing frame the flight is on.
func batFrameAt(t float64) int {
	return int(math.Floor(pixelfx.Wrap((t/batD)*batFlaps, 1)*4)) % 4
}

type batWing struct {
	sh, wr [2]float64
	tips   [3][2]float64
	poly   [][2]float64
}

func batWingPts(F *batFrame, s float64) batWing {
	sh := [2]float64{s * 2.5, -2}
	ar := batRad(F.arm)
	wr := [2]float64{sh[0] + s*math.Cos(ar)*F.al, sh[1] + math.Sin(ar)*F.al}
	var tips [3][2]float64
	for i, d := range F.f {
		tips[i] = [2]float64{wr[0] + s*math.Cos(batRad(d))*F.fl[i], wr[1] + math.Sin(batRad(d))*F.fl[i]}
	}
	pull := func(a, b [2]float64, k float64) [2]float64 {
		return [2]float64{pixelfx.Lerp((a[0]+b[0])/2, wr[0], k), pixelfx.Lerp((a[1]+b[1])/2, wr[1], k)}
	}
	hip := [2]float64{s * 2, 3.5}
	return batWing{sh: sh, wr: wr, tips: tips, poly: [][2]float64{sh, wr, tips[0], pull(tips[0], tips[1], 0.22), tips[1], pull(tips[1], tips[2], 0.22), tips[2], pull(tips[2], hip, 0.38), hip}}
}

func drawBatGrid(g *grid, C, E []uint32, F *batFrame) {
	g.clear()
	// wings: a membrane, darker near the body, lit along the leading edge; finger bones over it
	for _, s := range [2]float64{-1, 1} {
		w := batWingPts(F, s)
		lead := func(x, y float64) float64 {
			ax, ay, bx, by := w.wr[0], w.wr[1], w.tips[0][0], w.tips[0][1]
			dx, dy := bx-ax, by-ay
			L := pixelfx.Hypot(dx, dy)
			if L == 0 {
				L = 1
			}
			return math.Abs(((x-ax)*dy - (y-ay)*dx) / L)
		}
		g.polyFn(w.poly, func(x, y float64) uint32 {
			near := pixelfx.Hypot(x-w.sh[0], y-w.sh[1])
			if lead(x, y) < 1.2 && F.arm < 20 {
				return C[3]
			}
			if near < 4.5 {
				return C[1]
			}
			return C[2]
		}, 1)
		g.line(w.sh[0], w.sh[1], w.wr[0], w.wr[1], C[4])
		for _, tip := range w.tips {
			g.lineFn(w.wr[0], w.wr[1], tip[0], tip[1], func(u float64) uint32 {
				if u < 0.6 {
					return C[4]
				}
				return C[3]
			})
		}
		g.put(w.wr[0], w.wr[1]-1, C[5])
	}
	// the body: fur lit top-left, a paler belly, big ears with pink insides
	g.ellipse(0, 0.5, 3.8, 4.8, func(nx, ny float64) uint32 {
		switch {
		case nx*-0.6+ny*-0.8 > 0.45:
			return C[4]
		case nx*0.6+ny*0.8 > 0.55:
			return C[2]
		case ny > 0.1 && math.Abs(nx) < 0.5:
			return C[4]
		}
		return C[3]
	}, 1)
	for _, s := range [2]float64{-1, 1} {
		c := C[3]
		if s < 0 {
			c = C[4]
		}
		g.poly([][2]float64{{s * 0.6, -3.5}, {s * 3.6, -9.5}, {s * 3.8, -3}}, c, 1)
		g.line(s*2.6, -4, s*3.2, -7.5, C[5])
	}
	// eyes, a snout, two fangs, tucked feet
	g.put(-2, -1, E[1])
	g.put(1, -1, E[1])
	g.put(-1, 0, C[1])
	g.put(0, 0, C[1])
	g.put(-1, 1, C[6])
	g.put(0, 1, C[6])
	g.put(-2, 5, C[3])
	g.put(1, 5, C[3])
	g.outline(C[0], 1)
}

func newBat(o *pixelfx.Options) pixelfx.Instance {
	C, E, seed := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("eyes"), 4), o.Seed()
	g := newGrid(-24, -22, 48, 40)
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, time float64) {
		t := shift(time, batD, seed)
		sx, sy := batAt(t)
		fi := batFrameAt(t)
		F := &batFrames[fi]
		X, Y := pixelfx.Round(sx), pixelfx.Round(sy+F.lift)
		// its glowing eyes leave a fading streak of red light along its flight
		for k := 2.0; k <= 16; k++ {
			ax, ay := batAt(t - (k-1)*0.022)
			bx, by := batAt(t - k*0.022)
			u := 1 - k/17
			for _, ex := range [2]float64{-1.5, 1.5} {
				p.GlowLine(ax+ex, ay-0.5, bx+ex, by-0.5, 2.6, E[1], 0.16*u)
			}
		}
		// moonlit dust drifting in the night air
		pixelfx.Cycle(t, batD, 9, seed*11+1, pixelfx.Fixed(batD), func(age, L float64, r *pixelfx.Rng, _ int) {
			x := r.Range(-50, 50) + math.Sin(age*1.1+r.Next()*6)*3
			y := r.Range(-34, 34) + age*r.Range(-2, 3)
			tw := math.Sin(age*2.6 + r.Next()*6)
			a := math.Min(math.Min(1, age*2), (L-age)*2) * (0.4 + 0.5*tw)
			c := C[5]
			if tw > 0.7 {
				c = C[6]
			}
			p.Dot(x, y, c, a)
			if tw > 0.5 {
				p.Glow(x, y, 3, C[5], 0.2*a)
			}
		})
		// on the downbeat the wings leave a see-through echo of where they just were
		if fi == 2 {
			drawBatGrid(g, C, E, &batFrames[1])
			g.recolor(func(_, _ float64, c uint32, _ float64, _ uint8) uint32 { return pixelfx.Mix(c, C[4], 0.35) })
			g.fillAlpha(0.35)
			g.draw(p, 1, X, Y-batFrames[1].lift+F.lift)
		}
		drawBatGrid(g, C, E, F)
		g.draw(p, 1, X, Y)
		// moonlight catches the top of the head and wings
		p.Glow(X-3, Y-6, 10, C[5], 0.12)
		// the eyes glow, and glint now and then
		for _, ex := range [2]float64{-1.5, 1.5} {
			p.Glow(X+ex, Y-0.5, 7, E[1], 0.6)
			p.Glow(X+ex, Y-0.5, 2.5, E[2], 0.5)
		}
		p.Glow(X, Y, 22, E[0], 0.25)
		for _, g0 := range [2]float64{0.3, 1.5} {
			if k := pixelfx.Wrap(t-g0, batD) / 0.24; k < 1 {
				pixelfx.Sparkle(p, X-2, Y-1, 1+2.5*math.Sin(pi*k), E[3], E[2], 0.4*math.Sin(pi*k))
			}
		}
	}}
}
