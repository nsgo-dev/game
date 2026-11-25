// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/blizzard.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the wind-driven snow streaks and gusts from Sprite Fusion's open-source snow (https://www.spritefusion.com/pixel-art-vfx/snow), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Blizzard is snow driven almost flat by a gale: streaks at three depths race past, gusts throw translucent sheets of snow across the sky and spindrift streams off the drifts. It is registered as "weather/blizzard".
var Blizzard = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Blizzard",
		Desc:     "Snow driven almost flat by a gale: streaks at three depths race past, gusts throw translucent sheets of snow across the sky and spindrift streams off the drifts.",
		Tags:     []string{"weather", "blizzard", "snow", "wind", "storm", "winter", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: blizzardDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0d111f", "#182035", "#27324d", "#3e4d6e", "#64779c", "#9cb0d0", "#d8e4f6", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newBlizzard,
}

func init() { pixelfx.Register("weather/blizzard", Blizzard) }

const blizzardDuration = 8

func blizzardGustAt(x, t float64) float64 {
	return 0.5 + 0.5*math.Sin(tau*((x+160)/400-(2*t)/blizzardDuration)+0.8)
}

func blizzardBank(x float64) float64 {
	return -2 + 1.2*math.Sin(x*0.05+2) + 0.8*math.Sin(x*0.15+1)
}

type blizzardLayer struct {
	n    int
	m    [2]int
	len  [2]float64
	drop [2]float64
	col  [3]int
	near bool
}

var blizzardLayers = []blizzardLayer{
	{n: 240, m: [2]int{3, 4}, len: [2]float64{2, 4}, drop: [2]float64{10, 40}, col: [3]int{3, 4, 5}},
	{n: 130, m: [2]int{4, 5}, len: [2]float64{5, 8}, drop: [2]float64{20, 60}, col: [3]int{4, 5, 6}},
	{n: 34, m: [2]int{6, 7}, len: [2]float64{10, 16}, drop: [2]float64{30, 70}, col: [3]int{4, 6, 7}, near: true},
}

type blizzardInst struct {
	S      float64
	C      []uint32
	F0, F1 *noiseField
	FG     *noiseField
	gx, wv [320]float32
	rb     rowBuf
}

func newBlizzard(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	return &blizzardInst{
		S:  S,
		C:  pixelfx.Fit(o.Palette(), 8),
		F0: field(S*3+7, 256, 128, nil),
		F1: field(S*3+8, 256, 128, nil),
		FG: field(S*5+1, 256, 128, nil),
	}
}

func (b *blizzardInst) Update(float64) {}

func (b *blizzardInst) DrawBack(p *pixelfx.Painter, t float64) {
	q := clip(p, &Blizzard.Meta)
	b.sheets(p, t, 0)
	b.streaks(q, t, 0)
	b.streaks(q, t, 1)
	b.ground(p, q, t)
}

func (b *blizzardInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &Blizzard.Meta)
	b.sheets(p, t, 1)
	b.streaks(q, t, 2)
}

func (b *blizzardInst) streaks(q *clipper, t float64, L int) {
	Ly := blizzardLayers[L]
	sd := b.S*17 + float64(L)
	C := b.C
	c0, c1, c2 := C[Ly.col[0]], C[Ly.col[1]], C[Ly.col[2]]
	trips(t, blizzardDuration, Ly.n, sd, Ly.m[0], Ly.m[1], func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		fi := float64(i)
		R := func(n float64) float64 { return pixelfx.Rand(sd, fi, j, n) }
		drop := Ly.drop[0] + R(3)*(Ly.drop[1]-Ly.drop[0])
		x := -190 + 380*u
		y := -178 + R(1)*182 + drop*u + 3*math.Sin(u*tau*(1+R(5))+R(6)*6)
		if y > blizzardBank(x)-1 {
			return
		}
		g := blizzardGustAt(x, t)
		if !Ly.near && R(7) > 0.45+0.75*g {
			return
		}
		ln := (Ly.len[0] + R(2)*(Ly.len[1]-Ly.len[0])) * (0.7 + 0.6*g)
		dur := blizzardDuration / float64(m)
		dx, dy := 380/dur, drop/dur
		l := pixelfx.Hypot(dx, dy)
		ux, uy := dx/l, dy/l
		q.lineFn(x-ux*ln, y-uy*ln, x-ux*ln*0.45, y-uy*ln*0.45, func(v float64) (uint32, float64) {
			return c0, 0.3 + 0.7*v
		})
		q.line(x-ux*ln*0.45, y-uy*ln*0.45, x-ux, y-uy, c1, 1)
		head := c2
		if g > 0.6 && Ly.near {
			head = pixelfx.White
		}
		q.px(x, y, head)
		if Ly.near {
			q.glow(x, y, 5, C[5], 0.12+0.12*g)
		}
	})
}

func (b *blizzardInst) sheets(p *pixelfx.Painter, t float64, near int) {
	C := b.C
	F := b.F0
	mult, c, y0, sy, th := 2.0, C[5], -170.0, 3.2, 0.54
	if near != 0 {
		F = b.F1
		mult, c, y0, sy, th = 3.0, C[6], -110.0, 4.5, 0.58
	}
	shift := (256 * mult * t) / blizzardDuration
	haze := pixelfx.Mix(C[2], C[3], 0.5)
	for i := 0; i < 320; i++ {
		b.gx[i] = float32(blizzardGustAt(float64(i-160)+0.5, t))
		b.wv[i] = float32(5 * math.Sin(float64(i-160)*0.021-(tau*t)/blizzardDuration+float64(near)*2))
	}
	rows(p, &Blizzard.Meta, &b.rb, y0, 4, func(y float64, R *rowBuf) {
		fade := 1.0
		if near != 0 {
			fade = pixelfx.Smooth(-110, -40, y)
		}
		for i := 0; i < 320; i++ {
			g := float64(b.gx[i])
			n := sample(F, float64(i-160)*0.16-shift, (y+float64(b.wv[i]))*sy+40)
			d := (n - th) * 2.2 * (0.35 + g) * fade
			if near != 0 {
				if d <= 0.02 {
					continue
				}
				R.c[i] = c
				R.a[i] = float32(math.Min(0.5, d))
			} else {
				if d > 0.08 {
					R.c[i] = c
				} else {
					R.c[i] = haze
				}
				R.a[i] = float32(0.3 + 0.12*g + math.Min(0.42, math.Max(0, d)))
			}
			R.b[i] = 1
			if d > 0.05 {
				R.l[i] = C[5]
				R.k[i] = float32(d * 0.08)
			}
		}
	})
}

func (b *blizzardInst) ground(p *pixelfx.Painter, q *clipper, t float64) {
	C, S := b.C, b.S
	F := b.FG
	shift := (256 * 4 * t) / blizzardDuration
	for i := 0; i < 320; i++ {
		x := float64(i-160) + 0.5
		b.gx[i] = float32(blizzardGustAt(x, t))
		b.wv[i] = float32(blizzardBank(x))
	}
	rows(p, &Blizzard.Meta, &b.rb, -16, 9, func(y float64, R *rowBuf) {
		for i := 0; i < 320; i++ {
			x := float64(i-160) + 0.5
			top := float64(b.wv[i])
			d := y - top
			if d >= 0 {
				switch {
				case d < 1:
					R.c[i] = pixelfx.White
					R.l[i] = C[6]
					R.k[i] = 0.1
				case d < 2:
					R.c[i] = C[6]
				case d < 4:
					R.c[i] = C[5]
				default:
					R.c[i] = C[4]
				}
				continue
			}
			if y < -14 {
				continue
			}
			n := sample(F, x*0.5-shift, y*3+9)
			s := (n - 0.5) * 3 * pixelfx.Smooth(-14, -1, y) * (0.4 + float64(b.gx[i]))
			if s > 0.02 {
				R.c[i] = C[6]
				R.a[i] = float32(math.Min(0.55, s))
				R.b[i] = 1
			}
		}
	})
	trips(t, blizzardDuration, 40, S*23, 8, 10, func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		fi := float64(i)
		x := -175 + 350*u
		y := blizzardBank(x) - 1 - pixelfx.Rand(S, fi, j, 1)*6*math.Abs(math.Sin(u*9+fi))
		col := C[6]
		if pixelfx.Rand(S, fi, j, 2) < 0.5 {
			col = pixelfx.White
		}
		q.px(x, y, col)
		q.px(x-1, y, C[5])
	})
}
