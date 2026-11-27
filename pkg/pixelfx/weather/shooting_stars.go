// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/shooting-stars.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the glints from Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// ShootingStars is meteors burning across a starry sky, white heads trailing gold and rose; the big ones flare and break up, and each leaves a glowing train that twists and fades. It is registered as "weather/shooting-stars".
var ShootingStars = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Shooting stars",
		Desc:     "Meteors burn across a starry sky, white heads trailing gold and rose; the big ones flare and break up, and each leaves a glowing train that twists and fades.",
		Tags:     []string{"weather", "meteor", "shooting star", "night", "sky", "stars", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 180},
		Duration: shootingStarsDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#3a1e5a", "#7a3a8a", "#e0607a", "#ffa86a", "#ffe6a8", "#ffffff")},
			{Name: "sky", Default: pixelfx.Palette("#141428", "#2a2a4a", "#4c4c7c", "#8e8ec4", "#dcdcff")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newShootingStars,
}

func init() { pixelfx.Register("weather/shooting-stars", ShootingStars) }

const (
	shootingStarsDuration = 10
	meteorTrain           = 1.6
)

type meteorSpec struct {
	t0, x, y, a, v, life, s float64
	flare                   bool
}

var meteors = []meteorSpec{
	{t0: 0.5, x: 60, y: -170, a: 2.45, v: 260, life: 0.55, s: 0.6},
	{t0: 1.9, x: -40, y: -176, a: 2.2, v: 220, life: 0.7, s: 0.8},
	{t0: 3.4, x: 150, y: -150, a: 2.62, v: 300, life: 0.45, s: 0.5},
	{t0: 4.6, x: 130, y: -178, a: 2.35, v: 240, life: 1.05, s: 1.4, flare: true},
	{t0: 6.7, x: -90, y: -168, a: 2.05, v: 200, life: 0.6, s: 0.7},
	{t0: 7.9, x: 20, y: -160, a: 2.5, v: 280, life: 0.5, s: 0.6},
	{t0: 9.2, x: -10, y: -176, a: 2.28, v: 230, life: 0.8, s: 1, flare: true},
}

type shootingStarsInst struct {
	S    float64
	C, K []uint32
	revC [5]uint32
	rb   rowBuf
}

func newShootingStars(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 6)
	return &shootingStarsInst{
		S:    o.Seed(),
		C:    C,
		K:    pixelfx.Fit(o.Ramp("sky"), 5),
		revC: [5]uint32{C[5], C[4], C[3], C[2], C[1]},
	}
}

func (ss *shootingStarsInst) Update(float64)                     {}
func (ss *shootingStarsInst) DrawBack(*pixelfx.Painter, float64) {}

func (ss *shootingStarsInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &ShootingStars.Meta)
	ss.sky(p, q, t)
	for i, m := range meteors {
		ss.meteor(q, pixelfx.Wrap(t-m.t0, shootingStarsDuration), m, i)
	}
}

func (ss *shootingStarsInst) sky(p *pixelfx.Painter, q *clipper, t float64) {
	S, K := ss.S, ss.K
	rows(p, &ShootingStars.Meta, &ss.rb, -180, -1, func(y float64, R *rowBuf) {
		fy := math.Floor(y)
		for i := 0; i < 320; i++ {
			x := float64(i-160) + 0.5
			d := math.Abs((y+110)-(x*-0.32)) / 36
			if d >= 1 {
				continue
			}
			k := (1 - d) * (1 - d)
			R.l[i] = K[2]
			R.k[i] = float32(0.1 * k)
			if pixelfx.Rand(float64(i), fy, S, 5) < 0.05*k {
				R.c[i] = K[2]
				R.a[i] = 0.7
			}
		}
	})
	for i := 0; i < 130; i++ {
		fi := float64(i)
		x := -160 + pixelfx.Rand(S, fi, 1)*320
		y := -180 + pixelfx.Rand(S, fi, 2)*178
		r := pixelfx.Rand(S, fi, 3)
		c := 1 + math.Floor(pixelfx.Rand(S, fi, 4)*4)
		tw := 0.5 + 0.5*math.Sin((tau*c*t)/shootingStarsDuration+pixelfx.Rand(S, fi, 5)*tau)
		if r < 0.6 {
			q.dot(x, y, K[3], 0.35+0.5*tw)
		} else if r < 0.94 {
			col := pixelfx.White
			if r < 0.8 {
				col = K[4]
			}
			q.px(x, y, col)
			q.glow(x+0.5, y+0.5, 3, K[3], 0.22*tw)
		} else {
			sparkle(q, x, y, 1+pixelfx.Round(tw*2), K[4], K[3], 0.3*tw)
		}
	}
}

func (ss *shootingStarsInst) meteor(q *clipper, age float64, m meteorSpec, i int) {
	S, C := ss.S, ss.C
	fi := float64(i)
	ca, sa := math.Cos(m.a), math.Sin(m.a)
	flareExtra := 0.0
	if m.flare {
		flareExtra = 0.5
	}
	end := m.life + flareExtra + meteorTrain
	if age > end {
		return
	}
	pos := func(tt float64) (float64, float64) {
		return m.x + ca*m.v*tt, m.y + sa*m.v*tt
	}
	burn := math.Min(age, m.life)
	hx, hy := pos(burn)
	trainK := 1.0
	if age >= m.life {
		trainK = 1 - (age-m.life)/(meteorTrain+flareExtra)
	}
	if trainK > 0 {
		ln := m.v * burn
		nx, ny := -sa, ca
		for d := 0.0; d < ln; d += 1 {
			passed := age - d/m.v
			if passed < 0.08 {
				continue
			}
			g := trainK * math.Min(1, (passed-0.08)*4) * (0.35 + 0.65*math.Sin((pi*d)/ln)) * m.s
			tw := math.Sin(d*0.09+passed*1.7) * math.Min(4, passed*3)
			x := m.x + ca*d + nx*tw + passed*4
			y := m.y + sa*d + ny*tw
			col := C[1]
			if passed < 0.6 {
				col = C[2]
			}
			q.dot(x, y, col, g*0.75)
			if int(d)%6 == 0 {
				q.glow(x, y, 6+passed*4, C[1], 0.12*g)
			}
		}
	}
	if age > m.life+flareExtra {
		return
	}
	u := age / m.life
	k := 0.0
	if age < m.life {
		tail := 1.0
		if u > 0.75 {
			tail = 1 - (u-0.75)/0.25
		}
		k = math.Min(1, u*6) * tail
	}
	if k > 0 {
		L := (24 + 40*m.s) * math.Min(1, u*3)
		for d := 0.0; d <= L; d += 0.5 {
			v := d / L
			x := hx - ca*d
			y := hy - sa*d
			col := pixelfx.White
			if v >= 0.08 {
				col = pixelfx.At(ss.revC[:], v*0.95)
			}
			af := 1.0
			if v >= 0.6 {
				af = (1 - v) / 0.4
			}
			q.dot(x, y, col, k*af)
			if m.s > 0.9 && v < 0.4 {
				sc := C[3]
				if v < 0.15 {
					sc = C[4]
				}
				q.dot(x-sa*0.9, y+ca*0.9, sc, k*(1-v/0.4))
			}
		}
		q.px(hx, hy, pixelfx.White)
		if m.s > 0.9 {
			q.px(hx+1, hy, pixelfx.White)
			q.px(hx, hy+1, C[4])
		}
		q.glow(hx, hy, 8+10*m.s, C[4], 0.45*k)
		q.glow(hx-ca*L*0.3, hy-sa*L*0.3, 10+8*m.s, C[3], 0.2*k)
	}
	if !m.flare || age < m.life*0.82 {
		return
	}
	f := (age - m.life*0.82) / (m.life*0.18 + 0.5)
	fx, fy := pos(m.life * 0.86)
	if f < 0.35 {
		sparkle(q, fx, fy, 9*math.Sin((pi*f)/0.35)+2, C[4], C[3], 0.6)
		q.glow(fx, fy, 40, C[3], 0.5*(1-f/0.35))
	}
	for j := 0; j < 7; j++ {
		fj := float64(j)
		ang := pixelfx.Rand(S, fi, fj, 5) * tau
		r := 5 + pixelfx.Rand(S, fi, fj, 6)*16
		d0 := pixelfx.Rand(S, fi, fj, 7) * 0.3
		g := (f - d0) / 0.45
		if g <= 0 || g >= 1 {
			continue
		}
		sx := fx + math.Cos(ang)*r*(0.6+g*0.5)
		sy := fy + math.Sin(ang)*r*(0.6+g*0.5) - g*3
		arm := pixelfx.Round(2.5 * math.Sin(pi*g))
		if arm >= 1 {
			sc := pixelfx.White
			if j%3 != 0 {
				sc = C[4]
			}
			sparkle(q, sx, sy, arm, sc, C[3], 0.3*(1-g))
		} else {
			q.px(sx, sy, C[4])
		}
	}
	for j := 0; j < 4; j++ {
		fj := float64(j)
		da := (pixelfx.Rand(S, fi, fj, 1) - 0.5) * 0.5
		sp := m.v * (0.5 + pixelfx.Rand(S, fi, fj, 2)*0.4)
		tt := f * (m.life*0.18 + 0.5)
		g := 1 - f
		x := fx + math.Cos(m.a+da)*sp*tt
		y := fy + math.Sin(m.a+da)*sp*tt
		if g <= 0 {
			continue
		}
		q.line(x-math.Cos(m.a+da)*5*g, y-math.Sin(m.a+da)*5*g, x, y, C[3], g)
		col := C[4]
		if g > 0.5 {
			col = pixelfx.White
		}
		q.px(x, y, col)
		q.glow(x, y, 5, C[3], 0.35*g)
	}
}
