// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/snow.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the snow depth layers, gusts and settling drift from Sprite Fusion's open-source snow (https://www.spritefusion.com/pixel-art-vfx/snow), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Snow is snow at three depths in a gusty breeze: far specks, drifting flakes, big near crystals that spin and glow; they settle on a sparkling drift and melt away. It is registered as "weather/snow".
var Snow = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Snow",
		Desc:     "Snow at three depths in a gusty breeze: far specks, drifting flakes, big near crystals that spin and glow; they settle on a sparkling drift and melt away.",
		Tags:     []string{"weather", "snow", "winter", "cold", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: snowDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c1020", "#161d36", "#24304f", "#3a4a72", "#5f74a3", "#93abd6", "#cfe0fa", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newSnow,
}

func init() { pixelfx.Register("weather/snow", Snow) }

const snowDuration = 12

func snowBank(x float64) float64 {
	return -3 + 1.6*math.Sin(x*0.045+1.3) + 1.1*math.Sin(x*0.12+0.4) + 0.6*math.Sin(x*0.31+2)
}

func snowGust(t float64) float64 {
	return 10*math.Sin((tau*t)/snowDuration+0.6) + 4*math.Sin((tau*2*t)/snowDuration+2.1)
}

type snowLayer struct {
	n    int
	m    [2]int
	air  float64
	sway [2]float64
	wind float64
	gust float64
}

var snowLayers = [3]snowLayer{
	{n: 230, m: [2]int{1, 2}, air: 1, sway: [2]float64{1, 3}, wind: 3, gust: 0.4},
	{n: 140, m: [2]int{2, 3}, air: 0.8, sway: [2]float64{3, 7}, wind: 6, gust: 0.7},
	{n: 24, m: [2]int{2, 3}, air: 0.74, sway: [2]float64{8, 15}, wind: 9, gust: 1},
}

var (
	snowAxis = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	snowDiag = [4][2]int{{1, 1}, {-1, -1}, {1, -1}, {-1, 1}}
)

type snowInst struct {
	S float64
	C []uint32
}

func newSnow(o *pixelfx.Options) pixelfx.Instance {
	return &snowInst{S: o.Seed(), C: pixelfx.Fit(o.Palette(), 8)}
}

func (sn *snowInst) Update(float64) {}

func (sn *snowInst) DrawBack(p *pixelfx.Painter, t float64) {
	q := clip(p, &Snow.Meta)
	haze := pixelfx.Mix(sn.C[1], sn.C[2], 0.4)
	pass(p, &Snow.Meta, -170, 9, func(_, y float64, out *passOut) {
		out.c = haze
		out.a = 0.26 + 0.22*pixelfx.Smooth(-170, 0, y)
		out.b = true
	})
	sn.layer(q, t, 0)
	sn.drift(p, q, t)
	sn.layer(q, t, 1)
}

func (sn *snowInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &Snow.Meta)
	sn.layer(q, t, 2)
}

func (sn *snowInst) layer(q *clipper, t float64, L int) {
	Ly := snowLayers[L]
	S, C := sn.S, sn.C
	sd := S*13 + float64(L)
	trips(t, snowDuration, Ly.n, sd, Ly.m[0], Ly.m[1], func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		dur := snowDuration / float64(m)
		fi := float64(i)
		R := func(n float64) float64 { return pixelfx.Rand(sd, fi, j, n) }
		tf := dur * Ly.air
		age := math.Min(u*dur, tf)
		sw := Ly.sway[0] + R(3)*(Ly.sway[1]-Ly.sway[0])
		f := 0.25 + R(4)*0.35
		ph := R(5) * tau
		xAt := func(a float64) float64 {
			return -175 + R(2)*350 + Ly.wind*a + (snowGust(t-u*dur+a)-snowGust(t-u*dur))*Ly.gust + sw*math.Sin(tau*f*a+ph)
		}
		x, xl := xAt(age), xAt(tf)
		const y0 = -176.0
		var y1 float64
		if L != 0 {
			y1 = snowBank(xl) - 0.5
		} else {
			y1 = -110 + R(1)*106
		}
		y := y0 + (y1-y0)*(age/tf)
		rest := u*dur - tf
		if L == 0 {
			a := math.Min(1, (1-u)*6)
			col := C[5]
			if R(6) < 0.5 {
				col = C[4]
			}
			q.dot(x, y, col, a)
			return
		}
		fade := 1.0
		if rest > 0 {
			fade = 1 - rest/(dur-tf)
		}
		if L == 1 {
			big := R(6) < 0.3
			if rest > 0 {
				q.dot(xl, y1, C[7], fade*0.9)
				return
			}
			head := C[7]
			if big {
				head = pixelfx.White
			}
			q.px(x, y, head)
			if big {
				for _, d := range snowAxis {
					q.px(x+float64(d[0]), y+float64(d[1]), C[5])
				}
			} else {
				q.px(x+1, y, C[6])
				q.px(x, y+1, C[6])
				q.px(x+1, y+1, C[5])
			}
			return
		}
		r := 5
		r6 := R(6)
		if r6 < 0.4 {
			r = 3
		} else if r6 < 0.8 {
			r = 4
		}
		spin := 1.5 + R(7)*2.5
		if rest > 0 {
			q.dot(xl-1, y1, C[6], fade)
			q.dot(xl, y1, pixelfx.White, fade)
			q.dot(xl+1, y1, C[6], fade)
			q.glow(xl, y1, 5, C[5], 0.25*fade)
			return
		}
		turn := int(math.Floor(age*spin+R(8)*2)) % 2
		snowCrystal(q, x, y, r, turn != 0, C)
	})
}

func snowCrystal(q *clipper, x, y float64, r int, turn bool, C []uint32) {
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	long, short := snowAxis, snowDiag
	R := r
	if turn {
		long, short = snowDiag, snowAxis
		R = r - 1
	}
	for _, dir := range long {
		dx, dy := dir[0], dir[1]
		for d := 1; d <= R; d++ {
			col := C[6]
			if d == R {
				col = pixelfx.White
			}
			q.px(X+float64(dx*d), Y+float64(dy*d), col)
		}
		var nubs [2]int
		nn := 0
		if turn {
			if R >= 3 {
				nubs[0] = R - 1
				nn = 1
			}
		} else if R >= 5 {
			nubs[0], nubs[1] = R-1, R-2
			nn = 2
		} else if R >= 4 {
			nubs[0] = R - 1
			nn = 1
		}
		for k := 0; k < nn; k++ {
			n := nubs[k]
			if turn {
				oy := -dy
				q.px(X+float64(dx*n), Y+float64(dy*n+oy), C[4])
				q.px(X+float64(dx*n), Y+float64(dy*n), C[4])
			} else {
				ox, oy := dy, -dx
				q.px(X+float64(dx*n+ox), Y+float64(dy*n+oy), C[4])
				q.px(X+float64(dx*n-ox), Y+float64(dy*n-oy), C[4])
			}
		}
	}
	shortMax := max(1, R/2)
	if turn {
		shortMax = 1
	}
	for _, dir := range short {
		dx, dy := dir[0], dir[1]
		for d := 1; d <= shortMax; d++ {
			col := C[5]
			if d == 1 {
				col = C[6]
			}
			q.px(X+float64(dx*d), Y+float64(dy*d), col)
		}
	}
	q.px(X, Y, pixelfx.White)
	q.glow(X+0.5, Y+0.5, float64(r)*2.5+5, C[5], 0.13)
	q.light(X, Y, C[6], 0.4)
}

func (sn *snowInst) drift(p *pixelfx.Painter, q *clipper, t float64) {
	S, C := sn.S, sn.C
	pass(p, &Snow.Meta, -7, 9, func(x, y float64, out *passOut) {
		top := snowBank(x)
		d := y - top
		if d < 0 {
			return
		}
		switch {
		case d < 1:
			out.c = pixelfx.White
			out.l = C[6]
			out.k = 0.12
		case d < 2:
			out.c = C[7]
		case d < 3.5:
			out.c = C[6]
		case d < 6:
			out.c = C[5]
		default:
			out.c = C[4]
		}
	})
	for i := 0; i < 18; i++ {
		fi := float64(i)
		x := -156 + pixelfx.Rand(S, fi, 81)*312
		c := 1 + math.Floor(pixelfx.Rand(S, fi, 82)*3)
		tw := math.Sin((tau*c*t)/snowDuration + pixelfx.Rand(S, fi, 83)*tau)
		if tw < 0.55 {
			continue
		}
		y := math.Ceil(snowBank(x)) + math.Floor(pixelfx.Rand(S, fi, 84)*3)
		k := (tw - 0.55) / 0.45
		q.px(x, y, pixelfx.White)
		if k > 0.5 {
			q.px(x+1, y, C[6])
			q.px(x-1, y, C[6])
			q.px(x, y-1, C[6])
		}
		q.glow(x+0.5, y+0.5, 6, C[6], 0.35*k)
	}
}
