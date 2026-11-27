// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/cherry-blossoms.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the fluttering fall, flip and ground fade from Sprite Fusion's open-source falling leaves (https://www.spritefusion.com/pixel-art-vfx/falling-leaves), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CherryBlossoms is pink petals fluttering down on a spring breeze, tumbling in little spirals and flashing as they catch the light, with a few whole blossoms spinning among them. It is registered as "weather/cherry-blossoms".
var CherryBlossoms = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Cherry blossoms",
		Desc:     "Pink petals flutter down on a spring breeze, tumbling in little spirals and flashing as they catch the light, with a few whole blossoms spinning among them.",
		Tags:     []string{"weather", "cherry blossom", "sakura", "petals", "spring", "wind", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: cherryDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1832", "#8e3460", "#d26a94", "#f2a2c0", "#ffd2e2", "#fff4f8")},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	New: newCherryBlossoms,
}

func init() { pixelfx.Register("weather/cherry-blossoms", CherryBlossoms) }

const (
	cherryDuration = 10
	cherryFloor    = 1.0
)

var (
	cherryDuskTop  = pixelfx.Hex("#0e0a14")
	cherryDuskLow  = pixelfx.Hex("#1a0e1a")
	cherryHeartCol = pixelfx.Hex("#ffe08a")
)

func cherryGust(t float64) float64 {
	return 22*math.Sin((tau*t)/cherryDuration+0.4) + 7*math.Sin((tau*2*t)/cherryDuration-1)
}

func drawPetal(q shader, x, y, ln, ang, flip float64, C []uint32, a float64) {
	fs := 0.3 + 0.7*math.Abs(flip)
	back := flip < -0.2
	c, s, e := math.Cos(ang), math.Sin(ang), ln+1
	face := [3]uint32{C[2], C[3], C[4]}
	tip := C[5]
	if back {
		face = [3]uint32{C[1], C[2], C[3]}
		tip = C[3]
	}
	q.shade(x-e, y-e, x+e, y+e, func(px, py float64, out *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		u := (dx*c + dy*s) / ln
		v := (-dx*s + dy*c) / (ln * fs)
		if u < -1 || u > 1 {
			return 0
		}
		hw := 0.55 * math.Sqrt(1-u*u) * (1 + 0.35*u)
		if math.Abs(v) > hw || (u > 0.7 && math.Abs(v) < 0.12) {
			return 0
		}
		switch {
		case u > 0.45 && v < 0.1:
			out.C = tip
		case u < -0.45:
			out.C = face[0]
		case v < 0:
			out.C = face[2]
		default:
			out.C = face[1]
		}
		out.A = a
		return 0
	})
}

type cherryInst struct {
	S       float64
	C       []uint32
	scatter []recordedPixel
	rb      rowBuf
}

func newCherryBlossoms(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	C := pixelfx.Fit(o.Palette(), 6)
	var rec pixelRecorder
	for i := 0; i < 46; i++ {
		fi := float64(i)
		drawPetal(&rec, -164+pixelfx.Rand(S, fi, 71)*328, cherryFloor+math.Floor(pixelfx.Rand(S, fi, 72)*3)*2, 2.6+pixelfx.Rand(S, fi, 73), (pixelfx.Rand(S, fi, 74)-0.5)*1.2, 0.5, C, 1)
	}
	return &cherryInst{S: S, C: C, scatter: rec.px}
}

func (cb *cherryInst) Update(float64) {}

func (cb *cherryInst) DrawBack(p *pixelfx.Painter, t float64) {
	q := clip(p, &CherryBlossoms.Meta)
	low := pixelfx.Mix(cherryDuskLow, cb.C[0], 0.4)
	rows(p, &CherryBlossoms.Meta, &cb.rb, -170, 9, func(y float64, R *rowBuf) {
		k := (y + 170) / 180
		R.fillC(pixelfx.Mix(cherryDuskTop, low, k*k))
		R.fillA(float32(0.3 + 0.12*k))
		R.fillB(1)
	})
	for _, px := range cb.scatter {
		q.dot(px.x, px.y, px.c, px.a)
	}
	cb.petals(q, t, 0)
}

func (cb *cherryInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &CherryBlossoms.Meta)
	cb.petals(q, t, 1)
	cb.blossoms(q, t)
}

func (cb *cherryInst) petals(q *clipper, t float64, near int) {
	n := 90
	if near != 0 {
		n = 54
	}
	sd := cb.S*7 + float64(near)
	C := cb.C
	trips(t, cherryDuration, n, sd, 1, 2, func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		fi := float64(i)
		R := func(c float64) float64 { return pixelfx.Rand(sd, fi, j, c) }
		dur := cherryDuration / float64(m)
		tf := dur * 0.84
		a := math.Min(u*dur, tf)
		rest := u*dur - tf
		born := t - u*dur
		x0 := -200 + R(1)*330
		y0 := -184.0
		floorOff := 0.0
		if near != 0 {
			floorOff = 2
		}
		vy := (cherryFloor + floorOff - y0) / tf
		vx := 9 + R(2)*10
		rhoBase := 2.0
		gk := 0.6
		lenBase := 2.6
		if near != 0 {
			rhoBase = 3
			gk = 1
			lenBase = 3.6
		}
		rho := rhoBase + R(3)*3
		w := 2.2 + R(4)*2.8
		ph := R(5) * tau
		at := func(aa float64) (float64, float64) {
			return x0 + vx*aa + (cherryGust(born+aa)-cherryGust(born))*gk + rho*math.Cos(w*aa+ph),
				y0 + vy*aa + rho*0.6*math.Sin(w*aa+ph)
		}
		ln := lenBase + R(6)*1.1
		if rest > 0 {
			x, y := at(tf)
			fade := 1 - rest/(dur-tf)
			if fade > 0.15 {
				drawPetal(q, x, y, ln*(0.35+0.65*fade), (R(7)-0.5)*1.2, 0.5, C, 1)
			}
			return
		}
		x, y := at(a)
		flip := math.Cos(R(8)*tau + a*(3+R(9)*5))
		ang := R(10)*tau + a*(R(11)-0.5)*3
		drawPetal(q, x, y, ln, ang, flip, C, 1)
		if flip > 0.9 {
			g := (flip - 0.9) / 0.1
			q.px(x, y-1, pixelfx.White)
			gr, gk2 := 5.0, 0.22
			if near != 0 {
				gr, gk2 = 7, 0.32
			}
			q.glow(x, y, gr, C[4], gk2*g)
		}
	})
}

func (cb *cherryInst) blossoms(q *clipper, t float64) {
	S, C := cb.S, cb.C
	trips(t, cherryDuration, 7, S*31, 1, 1, func(u, _ float64, _, i int) {
		fi := float64(i)
		R := func(c float64) float64 { return pixelfx.Rand(S*31, fi, c) }
		y := -190 + (cherryFloor+2+190)*math.Min(1, u/0.9)
		born := t - u*cherryDuration
		if u > 0.9 {
			return
		}
		x := -190 + R(1)*330 + 14*u*cherryDuration*0.1 + (cherryGust(t) - cherryGust(born)) + 6*math.Sin(u*tau*2+R(2)*6)
		rot := R(3)*tau + u*cherryDuration*(R(4)-0.5)*2
		r := 3.6 + R(5)*1.2
		for p := 0; p < 5; p++ {
			an := rot + (float64(p)*tau)/5
			drawPetal(q, x+math.Cos(an)*r*0.75, y+math.Sin(an)*r*0.75, r*0.7, an, 1, C, 1)
		}
		q.px(x, y, cherryHeartCol)
		q.glow(x, y, 8, C[4], 0.2)
	})
}
