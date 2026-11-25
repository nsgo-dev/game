// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/fireflies.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the fireflies and swaying lit grass from Sprite Fusion's open-source fireflies (https://www.spritefusion.com/pixel-art-vfx/fireflies), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Fireflies is fireflies wandering over a meadow at night, each glowing up and fading on its own rhythm, trailing light and lighting the grass tips beneath them. It is registered as "weather/fireflies".
var Fireflies = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fireflies",
		Desc:     "Fireflies wander over a meadow at night, each glowing up and fading on its own rhythm, trailing light and lighting the grass tips beneath them.",
		Tags:     []string{"weather", "fireflies", "night", "glow", "meadow", "nature", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: firefliesDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a3a10", "#5a7a1a", "#9ad02a", "#d6ff5a", "#f4ffb8", "#ffffff")},
			{Name: "grass", Default: pixelfx.Palette("#070f0b", "#0c1a12", "#13281a", "#1d3a24", "#2b5232", "#3f6e40")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newFireflies,
}

func init() { pixelfx.Register("weather/fireflies", Fireflies) }

const firefliesDuration = 12

func firefliesCyc(t, n, ph float64) float64 {
	return (tau*n*t)/firefliesDuration + ph
}

type fireflyState struct {
	i        int
	depth    int
	k        float64
	cx, cy   float64
	ax, ay   float64
	n1, n2   float64
	ph1, ph2 float64
	glow     float64
}

func (f *fireflyState) pos(tt float64) (float64, float64) {
	fi := float64(f.i)
	return f.cx + f.ax*math.Sin(firefliesCyc(tt, f.n1, f.ph1)) + 10*f.k*pixelfx.LoopNoise(tt/firefliesDuration, 1.5, fi*3),
		f.cy + f.ay*math.Sin(firefliesCyc(tt, f.n2, f.ph2)) + 6*f.k*pixelfx.LoopNoise(tt/firefliesDuration, 1.5, fi*3+1)
}

func fireflyPulse(v float64) float64 {
	if v < 0.06 {
		return v / 0.06
	}
	if v < 0.14 {
		return 1
	}
	if v < 0.4 {
		return 1 - (v-0.14)/0.26
	}
	return 0
}

func makeFly(S float64, i int, t float64) fireflyState {
	fi := float64(i)
	R := func(n float64) float64 { return pixelfx.Rand(S, fi, n) }
	depth := 2
	if i < 14 {
		depth = 0
	} else if i < 26 {
		depth = 1
	}
	k := [3]float64{0.5, 0.8, 1}[depth]
	cySpan := 130.0
	if depth == 2 {
		cySpan = 70
	}
	cx := -150 + R(1)*300
	cy := -16 - R(2)*cySpan
	ax := (14 + R(3)*26) * k
	ay := (8 + R(4)*14) * k
	n1 := 1 + math.Floor(R(5)*2)
	n2 := 1 + math.Floor(R(6)*3)
	per := firefliesDuration / (3 + math.Floor(R(9)*4))
	u := pixelfx.Wrap(t/per+R(10), 1)
	two := R(11) < 0.35
	glow := fireflyPulse(u)
	if two {
		glow = math.Max(glow, fireflyPulse(u-0.22)*0.8)
	}
	return fireflyState{
		i:     i,
		depth: depth,
		k:     k,
		cx:    cx,
		cy:    cy,
		ax:    ax,
		ay:    ay,
		n1:    n1,
		n2:    n2,
		ph1:   R(7) * tau,
		ph2:   R(8) * tau,
		glow:  glow,
	}
}

var fireflyGlintOffsets = [4][2]float64{{0, -2}, {0, 2}, {-2, 0}, {2, 0}}

type firefliesInst struct {
	S     float64
	C, G  []uint32
	flies [32]fireflyState
	rb    rowBuf
}

func newFireflies(o *pixelfx.Options) pixelfx.Instance {
	return &firefliesInst{
		S: o.Seed(),
		C: pixelfx.Fit(o.Palette(), 6),
		G: pixelfx.Fit(o.Ramp("grass"), 6),
	}
}

func (ff *firefliesInst) Update(float64) {}

func (ff *firefliesInst) DrawBack(p *pixelfx.Painter, t float64) {
	q := clip(p, &Fireflies.Meta)
	rows(p, &Fireflies.Meta, &ff.rb, -170, 9, func(y float64, R *rowBuf) {
		k := (y + 170) / 180
		R.fillC(pixelfx.Mix(ff.G[0], ff.G[1], k))
		R.fillA(float32(0.48 + 0.14*k))
		R.fillB(1)
	})
	for i := range ff.flies {
		ff.flies[i] = makeFly(ff.S, i, t)
	}
	ff.grass(q, t, false)
	for i := range ff.flies {
		if ff.flies[i].depth < 2 {
			ff.drawFly(q, t, &ff.flies[i])
		}
	}
}

func (ff *firefliesInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &Fireflies.Meta)
	for i := range ff.flies {
		ff.flies[i] = makeFly(ff.S, i, t)
	}
	ff.grass(q, t, true)
	for i := range ff.flies {
		if ff.flies[i].depth == 2 {
			ff.drawFly(q, t, &ff.flies[i])
		}
	}
}

func (ff *firefliesInst) drawFly(q *clipper, t float64, f *fireflyState) {
	C := ff.C
	x, y := f.pos(t)
	g := f.glow
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	if f.depth == 0 {
		if g < 0.08 {
			return
		}
		col := C[3]
		if g > 0.6 {
			col = C[4]
		}
		q.dot(X, Y, col, math.Min(1, g*1.6))
		q.glow(x, y, 6, C[2], 0.45*g)
		return
	}
	if g > 0.3 {
		for j := 1; j <= 4; j++ {
			tx, ty := f.pos(t - float64(j)*0.05)
			q.dot(tx, ty, C[2], (g-0.3)*(1-float64(j)/5)*1.3)
		}
	}
	if f.depth == 1 {
		col := C[1]
		if g > 0.5 {
			col = C[5]
		} else if g > 0.15 {
			col = C[3]
		}
		q.px(X, Y, col)
		if g > 0.4 {
			q.px(X, Y+1, C[2])
		}
		q.glow(x, y, 9, C[2], 0.5*g)
		q.glow(x, y, 3, C[4], 0.5*g)
		return
	}
	nx, _ := f.pos(t + 0.05)
	dir := -1.0
	if nx >= x {
		dir = 1
	}
	beat := math.Mod(math.Floor(t*30), 2)
	wingCol := pixelfx.Mix(C[1], pixelfx.White, 0.4)
	q.px(X+dir, Y, C[0])
	q.px(X+dir*2, Y, C[0])
	q.dot(X, Y-1-beat, wingCol, 0.7)
	q.dot(X+dir, Y-1-beat, wingCol, 0.7)
	midCol := C[1]
	if g > 0.3 {
		midCol = C[5]
	}
	q.px(X, Y, midCol)
	tailCol := C[1]
	if g > 0.5 {
		tailCol = C[4]
	} else if g > 0.15 {
		tailCol = C[3]
	}
	q.px(X-dir, Y, tailCol)
	if g > 0.6 {
		q.px(X-dir, Y+1, C[3])
	}
	if g > 0.7 {
		for _, off := range fireflyGlintOffsets {
			q.dot(X-dir+off[0], Y+off[1], C[3], (g-0.7)*3)
		}
	}
	q.glow(x, y, 28, C[2], 0.45*g)
	q.glow(x, y, 10, C[4], 0.5*g)
	q.glow(x, y, 4, C[5], 0.5*g)
}

func (ff *firefliesInst) grass(q *clipper, t float64, front bool) {
	type litFly struct {
		x, y, g float64
	}
	var lit [32]litFly
	nLit := 0
	for i := range ff.flies {
		f := &ff.flies[i]
		if f.glow > 0.25 && f.depth > 0 {
			fx, fy := f.pos(t)
			lit[nLit] = litFly{fx, fy, f.glow}
			nLit++
		}
	}
	S, G, C := ff.S, ff.G, ff.C
	n := 230
	sdOff := 0.0
	hBase, hSpan, baseY, toneSpan := 5.0, 11.0, 2.0, 2.0
	if front {
		n = 150
		sdOff = 1
		hBase, hSpan, baseY, toneSpan = 4, 8, 9, 3
	}
	sd := S*3 + sdOff
	step := 324 / float64(n)
	for i := 0; i < n; i++ {
		fi := float64(i)
		R := func(k float64) float64 { return pixelfx.Rand(sd, fi, k) }
		x := -162 + (fi+R(1))*step
		h := hBase + R(2)*hSpan
		if R(3) < 0.1 {
			h += 6
		}
		base := baseY - R(4)*3
		sway := (1.2+R(5))*math.Sin(firefliesCyc(t, 2, -x*0.03)+R(6)) + 0.6*math.Sin(firefliesCyc(t, 5, x*0.07))
		tone := 1 + int(math.Floor(R(7)*toneSpan))
		tipLight := 0.0
		for j := 0; j < nLit; j++ {
			d := pixelfx.Hypot(lit[j].x-(x+sway), lit[j].y-(base-h))
			if d < 28 {
				tipLight = math.Max(tipLight, lit[j].g*(1-d/28))
			}
		}
		for r := 0.0; r < h; r++ {
			u := r / h
			var c uint32
			if u > 0.8 && tipLight > 0.2 {
				if tipLight > 0.55 && u > 0.9 {
					c = C[3]
				} else {
					c = C[2]
				}
			} else if u > 0.7 {
				c = G[min(5, tone+1)]
			} else {
				c = G[tone]
			}
			q.px(x+sway*u*u, base-r, c)
		}
		if R(8) < 0.07 {
			sx, sy := x+sway, base-h
			headCol := G[5]
			if tipLight > 0.2 {
				headCol = C[3]
			}
			q.px(sx, sy-1, headCol)
			q.px(sx-1, sy, G[4])
			q.px(sx+1, sy, G[4])
		}
	}
}
