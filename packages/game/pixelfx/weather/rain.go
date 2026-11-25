// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/rain.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rain streaks, splashes and low mist from Sprite Fusion's open-source rain (https://www.spritefusion.com/pixel-art-vfx/rain), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Rain is slanted rain at four depths: far drops fade into the dark, near ones glint and splash, drops ring the puddles and a low mist drifts over the wet floor. It is registered as "weather/rain".
var Rain = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Rain",
		Desc:     "Slanted rain at four depths: far drops fade into the dark, near ones glint and splash, drops ring the puddles and a low mist drifts over the wet floor.",
		Tags:     []string{"weather", "rain", "storm", "water", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: rainDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e1322", "#1a2440", "#283a5e", "#3a5482", "#5674a8", "#84a6d6", "#c0dcf8", "#f2f8ff")},
			{Name: "density", Default: pixelfx.Num(1)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newRain,
}

func init() { pixelfx.Register("weather/rain", Rain) }

const (
	rainDuration = 8
	rainSlant    = -0.2
)

var (
	rainUX = rainSlant / pixelfx.Hypot(rainSlant, 1)
	rainUY = 1 / pixelfx.Hypot(rainSlant, 1)
)

type rainLayer struct {
	n    int
	m    [2]int
	len  float64
	col  [3]int
	hasZ bool
	stop [2]float64
	z    [2]float64
	size [2]float64
	near bool
}

var rainLayers = [4]rainLayer{
	{n: 200, m: [2]int{10, 13}, len: 2, col: [3]int{2, 2, 3}, stop: [2]float64{-80, -6}},
	{n: 150, m: [2]int{14, 17}, len: 4, col: [3]int{2, 3, 4}, stop: [2]float64{-45, -2}},
	{n: 96, m: [2]int{19, 23}, len: 7, col: [3]int{3, 4, 5}, hasZ: true, z: [2]float64{0, 1.4}, size: [2]float64{0.15, 0.4}},
	{n: 42, m: [2]int{25, 29}, len: 13, col: [3]int{3, 5, 7}, hasZ: true, z: [2]float64{2, 6.5}, size: [2]float64{0.6, 1}, near: true},
}

type rainPool struct {
	x0, x1 float64
	y      float64
}

func rainPoolAt(P []rainPool, x, y float64) *rainPool {
	for i := range P {
		p := &P[i]
		if x >= p.x0+2 && x <= p.x1-2 && math.Abs(y-p.y) < 1.5 {
			return p
		}
	}
	return nil
}

type rainInst struct {
	S, density float64
	C          []uint32
	P          [5]rainPool
	G, M       *noiseField
}

func newRain(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	var P [5]rainPool
	for i := range 5 {
		fi := float64(i)
		x := -150 + fi*66 + pixelfx.Rand(S, fi, 61)*30
		w := 16 + pixelfx.Rand(S, fi, 63)*26
		P[i] = rainPool{x0: x, x1: x + w, y: 3 + math.Floor(pixelfx.Rand(S, fi, 62)*3)}
	}
	return &rainInst{
		S:       S,
		density: o.Num("density"),
		C:       pixelfx.Fit(o.Palette(), 8),
		P:       P,
		G:       field(S+21, 256, 128, cells4_8_16),
		M:       field(S+9, 256, 128, defaultCells),
	}
}

func (rn *rainInst) Update(float64) {}

func (rn *rainInst) DrawBack(p *pixelfx.Painter, t float64) {
	t = pixelfx.Round(t*3e4) / 3e4
	q := clip(p, &Rain.Meta)
	haze := pixelfx.Mix(rn.C[0], rn.C[1], 0.6)
	pass(p, &Rain.Meta, -170, -0.5, func(_, y float64, out *passOut) {
		out.c = haze
		out.a = 0.3 + 0.26*pixelfx.Smooth(-170, 0, y)
		out.b = true
	})
	rn.floor(p, q, t)
	for L := 0; L < 3; L++ {
		rn.layer(q, t, L)
	}
	rn.mist(p, t)
}

func (rn *rainInst) Draw(p *pixelfx.Painter, t float64) {
	t = pixelfx.Round(t*3e4) / 3e4
	q := clip(p, &Rain.Meta)
	rn.layer(q, t, 3)
}

func (rn *rainInst) layer(q *clipper, t float64, L int) {
	Ly := rainLayers[L]
	S, C := rn.S, rn.C
	sd := S*11 + float64(L)
	c0, c1, c2 := C[Ly.col[0]], C[Ly.col[1]], C[Ly.col[2]]
	ln := Ly.len
	n := int(pixelfx.Round(float64(Ly.n) * rn.density))
	G := rn.G
	sweep := (256 * t) / rainDuration
	crownCols := splashColors{hot: C[6], cool: C[4], white: pixelfx.White}
	trips(t, rainDuration, n, sd, Ly.m[0], Ly.m[1], func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		fi := float64(i)
		if L < 2 && pixelfx.Rand(sd, fi, j, 7) > 0.35+1.3*(sample(G, (-166+pixelfx.Rand(sd, fi, j, 2)*332)*0.8+sweep, 9+float64(L)*40)-0.32) {
			return
		}
		var end float64
		if Ly.hasZ {
			end = Ly.z[0] + pixelfx.Rand(sd, fi, j, 1)*(Ly.z[1]-Ly.z[0])
		} else {
			end = Ly.stop[0] + pixelfx.Rand(sd, fi, j, 1)*(Ly.stop[1]-Ly.stop[0])
		}
		y0 := -172 - ln
		y := y0 + (end-y0)*u
		x := -166 + pixelfx.Rand(sd, fi, j, 2)*332 - (end-y)*rainSlant
		a := 1.0
		if !Ly.hasZ {
			a = math.Min(1, (1-u)*5)
		}
		if ln <= 2 {
			q.dot(x, y, c2, a)
			q.dot(x-rainUX, y-rainUY, c1, a*0.8)
		} else {
			q.lineFn(x-rainUX*ln, y-rainUY*ln, x-rainUX*ln*0.45, y-rainUY*ln*0.45, func(v float64) (uint32, float64) {
				return c0, a * (0.35 + 0.65*v)
			})
			q.line(x-rainUX*ln*0.45, y-rainUY*ln*0.45, x-rainUX, y-rainUY, c1, a)
			q.dot(x, y, c2, a)
		}
		if Ly.near {
			q.glow(x, y-2, 4, C[5], 0.12)
		}
		if !Ly.hasZ {
			return
		}
		dur := rainDuration / float64(m)
		for back := 1; back < 4; back++ {
			age := u*dur + float64(back-1)*dur
			if age > 0.7 {
				break
			}
			jp := pixelfx.Wrap(k-float64(back), float64(m))
			z := Ly.z[0] + pixelfx.Rand(sd, fi, jp, 1)*(Ly.z[1]-Ly.z[0])
			xp := -166 + pixelfx.Rand(sd, fi, jp, 2)*332
			lim := 0.7
			if Ly.near {
				lim = 1
			}
			if pixelfx.Rand(sd, fi, jp, 5) > lim {
				continue
			}
			size := Ly.size[0] + ((z-Ly.z[0])/(Ly.z[1]-Ly.z[0]))*(Ly.size[1]-Ly.size[0])
			var pool *rainPool
			if Ly.near {
				pool = rainPoolAt(rn.P[:], xp, z)
			}
			if pool != nil {
				rainRings(q, xp, pool, age, C)
			} else if crown(q, xp, z, age, size, crownCols, pixelfx.Rand(sd, fi, jp, 4)) && Ly.near && age < 0.1 {
				q.glow(xp, z-1, 5, C[5], 0.3*(1-age/0.1))
			}
		}
	})
}

func rainRings(q *clipper, x float64, pool *rainPool, age float64, C []uint32) {
	X, y := pixelfx.Round(x), pool.y
	if age < 0.05 {
		q.px(X, y, C[7])
	}
	for r := 0; r < 2; r++ {
		a := age - float64(r)*0.16
		u := a / 0.5
		if u <= 0 || u >= 1 {
			continue
		}
		w := pixelfx.Round(1 + 7*math.Sqrt(u))
		c := C[5]
		if u < 0.4 && r == 0 {
			c = C[6]
		}
		dmax := 1
		if u < 0.6 {
			dmax = 2
		}
		for _, s := range [2]float64{-1, 1} {
			for d := 0; d < dmax; d++ {
				px := X + s*(w-float64(d))
				if px > pool.x0 && px < pool.x1 {
					q.px(px, y, c)
				}
			}
		}
	}
}

func (rn *rainInst) floor(p *pixelfx.Painter, q *clipper, t float64) {
	C := rn.C
	pass(p, &Rain.Meta, 0, 9.5, func(x, y float64, out *passOut) {
		if y < 1 {
			out.c = C[4]
			out.l = C[5]
			out.k = 0.08
			return
		}
		if y < 2 {
			out.c = C[1]
			return
		}
		out.c = C[0]
		out.a = 0.55
		out.b = true
		fy := math.Floor(y)
		for i := range rn.P {
			pl := &rn.P[i]
			if x >= pl.x0 && x < pl.x1 && fy == pl.y {
				if x < pl.x0+2 || x >= pl.x1-2 {
					out.c = C[3]
				} else {
					out.c = C[4]
				}
				out.a = 1
				out.b = false
				out.l = C[5]
				out.k = 0.06
			}
		}
	})
	for i := range rn.P {
		pl := &rn.P[i]
		gx := pixelfx.Round(pl.x0 + 3 + (pl.x1-pl.x0-6)*(0.5+0.5*math.Sin((pi*2*t)/rainDuration+float64(i)*1.9)))
		q.px(gx, pl.y, C[6])
		q.glow(gx, pl.y, 4, C[5], 0.2)
	}
}

func (rn *rainInst) mist(p *pixelfx.Painter, t float64) {
	M, C := rn.M, rn.C
	drift := (256 * t) / rainDuration
	crest := pixelfx.Mix(C[4], C[5], 0.5)
	dens := func(x, y float64) float64 {
		prof := pixelfx.Smooth(-22, -4, y) * (1 - pixelfx.Smooth(7, 10, y))
		return prof * (0.62 + (sample(M, x*0.45+drift, y*4+30)-0.5)*2)
	}
	pass(p, &Rain.Meta, -20, 9, func(x, y float64, out *passOut) {
		d := dens(x, y)
		if d < 0.28 {
			return
		}
		th := bayer(x, y) * 0.06
		out.b = true
		if d > 0.46+th && dens(x, y-1) < 0.43 {
			out.c = crest
			out.a = 0.5
		} else {
			if d > 0.58+th {
				out.c = C[4]
			} else {
				out.c = C[3]
			}
			out.a = pixelfx.Clamp((d-0.28)*1.6) * 0.5
		}
		out.l = C[4]
		out.k = (d - 0.3) * 0.1
	})
}
