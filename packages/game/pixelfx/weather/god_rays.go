// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/god-rays.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the banded light from Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// GodRays is shafts of light slanting down from high on the left, breathing and swaying, pooling on the floor, as drifting dust motes light up whenever they cross a beam. It is registered as "weather/god-rays".
var GodRays = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "God rays",
		Desc:     "Shafts of light slant down from high on the left, breathing and swaying, pooling on the floor, as drifting dust motes light up whenever they cross a beam.",
		Tags:     []string{"weather", "god rays", "light", "sunbeam", "dust", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: godRaysDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a1208", "#3a2810", "#7a5a24", "#c89a44", "#f2d27a", "#fff2c4", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newGodRays,
}

func init() { pixelfx.Register("weather/god-rays", GodRays) }

const (
	godRaysDuration = 10
	godRaysW        = 320
	godRaysSX       = -230.0
	godRaysSY       = -300.0
)

var (
	godRaysAX = math.Cos(0.94)
	godRaysAY = math.Sin(0.94)
	godRaysNX = -godRaysAY
	godRaysNY = godRaysAX
)

type godRayShaftSpec struct {
	c, h, k, b, sw float64
}

var godRayShafts = [6]godRayShaftSpec{
	{c: -0.27, h: 0.03, k: 0.65, b: 1, sw: 0.006},
	{c: -0.17, h: 0.022, k: 0.55, b: 2, sw: 0.005},
	{c: -0.07, h: 0.05, k: 1, b: 1, sw: 0.008},
	{c: 0.03, h: 0.018, k: 0.6, b: 3, sw: 0.004},
	{c: 0.12, h: 0.042, k: 0.85, b: 2, sw: 0.007},
	{c: 0.23, h: 0.026, k: 0.6, b: 1, sw: 0.005},
}

type godRayShaft struct {
	c, h, k float64
}

type godRaysInst struct {
	S   float64
	C   []uint32
	L   [7]uint32
	str [512]float32
	rb  rowBuf
}

func newGodRays(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	var L [7]uint32
	for i := range 7 {
		L[i] = pixelfx.Mix(C[0], C[i], 0.75)
	}
	return &godRaysInst{S: o.Seed(), C: C, L: L}
}

func (gr *godRaysInst) Update(float64) {}

func (gr *godRaysInst) shafts(t float64) [6]godRayShaft {
	S := gr.S
	for i := 0; i < 512; i++ {
		a := float64(i) / 512
		gr.str[i] = float32(0.5 + 0.5*math.Sin(a*190+math.Sin((tau*t)/godRaysDuration)*2+S)*math.Sin(a*77-(tau*t)/godRaysDuration+S*2))
	}
	var out [6]godRayShaft
	for i, s := range godRayShafts {
		fi := float64(i)
		out[i] = godRayShaft{
			c: s.c + s.sw*math.Sin((tau*t)/godRaysDuration+fi*1.3),
			h: s.h * (1 + 0.15*math.Sin((tau*s.b*t)/godRaysDuration+fi)),
			k: s.k * (0.72 + 0.28*math.Sin((tau*s.b*t)/godRaysDuration+fi*2.1)),
		}
	}
	return out
}

func (gr *godRaysInst) lightAt(x, y float64, sh *[6]godRayShaft) float64 {
	dx, dy := x-godRaysSX, y-godRaysSY
	r := dx*godRaysAX + dy*godRaysAY
	ang := (dx*godRaysNX + dy*godRaysNY) / r
	e := 0.0
	for i := range sh {
		s := &sh[i]
		q := math.Abs(ang-s.c) / s.h
		if q < 1 {
			e += s.k * math.Pow(1-q*q, 1.5)
		}
	}
	if e <= 0 {
		return 0
	}
	si := int(pixelfx.ClampTo(math.Floor((ang+0.36)*700), 0, 511))
	strand := float64(gr.str[si])
	fall := pixelfx.Clamp(1.25 - (r-230)/420)
	return e * fall * (0.7 + 0.45*strand)
}

func (gr *godRaysInst) DrawBack(p *pixelfx.Painter, t float64) {
	q := clip(p, &GodRays.Meta)
	C, L := gr.C, gr.L
	sh := gr.shafts(t)
	var sc, si, sk [6]float32
	for j, s := range sh {
		sc[j] = float32(s.c)
		si[j] = float32(1 / s.h)
		sk[j] = float32(s.k)
	}
	rows(p, &GodRays.Meta, &gr.rb, -170, 9, func(y float64, R *rowBuf) {
		floor := y > 0
		dy := y - godRaysSY
		air, boost := float32(0.66), 1.0
		if floor {
			air, boost = 0.8, 1.6
		}
		r := (-160+0.5-godRaysSX)*godRaysAX + dy*godRaysAY
		nn := (-160+0.5-godRaysSX)*godRaysNX + dy*godRaysNY
		R.fillA(air)
		R.fillB(1)
		R.fillC(C[0])
		for i := 0; i < godRaysW; i, r, nn = i+1, r+godRaysAX, nn+godRaysNX {
			ang := nn / r
			e := 0.0
			for j := range 6 {
				q2 := (ang - float64(sc[j])) * float64(si[j])
				if q2 > -1 && q2 < 1 {
					w := 1 - q2*q2
					e += float64(sk[j]) * w * math.Sqrt(w)
				}
			}
			if e <= 0 {
				continue
			}
			fall := 1.35 - (r-200)/330
			if fall > 1 {
				fall = 1
			} else if fall < 0 {
				fall = 0
			}
			idx := int(pixelfx.ToInt32((ang + 0.36) * 700))
			if idx < 0 {
				idx = 0
			} else if idx > 511 {
				idx = 511
			}
			e *= fall * (0.7 + 0.45*float64(gr.str[idx])) * boost
			if e < 0.06 {
				continue
			}
			lv := 2
			switch {
			case e > 1.05:
				lv = 6
			case e > 0.78:
				lv = 5
			case e > 0.52:
				lv = 4
			case e > 0.3:
				lv = 3
			}
			if floor {
				R.c[i] = C[lv]
				R.a[i] = 0.85
				R.k[i] = float32(e * 0.32)
			} else {
				R.c[i] = L[lv]
				R.a[i] = float32(math.Min(0.78, 0.12+e*0.5))
				R.k[i] = float32(e * 0.26)
			}
			R.l[i] = C[4]
		}
	})
	q.glow(-168, -178, 130, C[3], 0.3)
	q.glow(-168, -178, 50, C[5], 0.35)
	gr.motes(q, t, &sh, false)
}

func (gr *godRaysInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &GodRays.Meta)
	sh := gr.shafts(t)
	gr.motes(q, t, &sh, true)
}

func (gr *godRaysInst) motes(q *clipper, t float64, sh *[6]godRayShaft, front bool) {
	S, C := gr.S, gr.C
	start, end := 50, 160
	if front {
		start, end = 0, 50
	}
	for i := start; i < end; i++ {
		fi := float64(i)
		R := func(k float64) float64 { return pixelfx.Rand(S, fi, k) }
		n1 := 1 + math.Floor(R(1)*2)
		n2 := 1 + math.Floor(R(2)*2)
		x := -170 + R(3)*340 + (8+R(4)*14)*math.Sin((tau*n1*t)/godRaysDuration+R(5)*tau)
		y := -170 + R(6)*172 + (6+R(7)*10)*math.Sin((tau*n2*t)/godRaysDuration+R(8)*tau) + 4*math.Sin((tau*3*t)/godRaysDuration+fi)
		e := gr.lightAt(x, y, sh)
		if e < 0.12 {
			continue
		}
		big := front && R(9) < 0.5
		k := math.Min(1, e)
		col := C[4]
		if k > 0.6 {
			col = pixelfx.White
		} else if k > 0.3 {
			col = C[5]
		}
		q.px(x, y, col)
		if big && k > 0.5 {
			q.px(x+1, y, C[4])
			q.px(x, y+1, C[4])
		}
		rad := 4.0
		if big {
			rad = 6
		}
		q.glow(x, y, rad, C[4], 0.4*k)
	}
}
