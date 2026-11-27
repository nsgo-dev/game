// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/falling-leaves.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the falling leaf swing, tilt, flip and litter from Sprite Fusion's open-source falling leaves (https://www.spritefusion.com/pixel-art-vfx/falling-leaves), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FallingLeaves is autumn leaves swinging down through the air, tilting and flipping to show their pale backs, riding a gust, landing flat in the litter and fading. It is registered as "weather/falling-leaves".
var FallingLeaves = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Falling leaves",
		Desc:     "Autumn leaves swing down through the air, tilting and flipping to show their pale backs, ride a gust, land flat in the litter and fade.",
		Tags:     []string{"weather", "leaves", "autumn", "fall", "wind", "nature", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: leavesDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1d0c", "#8a3a14", "#c8621e", "#f0982e", "#ffcf72", "#fff0c4")},
			{Name: "reds", Default: pixelfx.Palette("#40101a", "#7c1c22", "#b8302a", "#e85a3c", "#ff9a72", "#ffd8c4")},
			{Name: "golds", Default: pixelfx.Palette("#3e300c", "#7a5c14", "#b8901e", "#e8c23a", "#fff09a", "#fffbe0")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newFallingLeaves,
}

func init() { pixelfx.Register("weather/falling-leaves", FallingLeaves) }

const (
	leavesDuration = 10
	leavesFloor    = 1.0
)

var (
	leavesDuskTop  = pixelfx.Hex("#0e0a12")
	leavesDuskLow  = pixelfx.Hex("#140c14")
	leavesBack1    = pixelfx.Hex("#9c8a78")
	leavesBack2    = pixelfx.Hex("#c8b49c")
	leavesBack3    = pixelfx.Hex("#e0d0b8")
	leavesBackVein = pixelfx.Hex("#e8dcc8")
)

func leavesGust(t float64) float64 {
	return 16*math.Sin((tau*t)/leavesDuration-1.2) + 5*math.Sin((tau*3*t)/leavesDuration)
}

func leavesHc(th float64) float64 {
	k := math.Floor((th + pi/2) / pi)
	return 2*k + 1 + math.Sin(th-k*pi)
}

func leafShape(shape int, u, v float64) float64 {
	switch shape {
	case 0:
		if math.Abs(u) >= 1 {
			return 9
		}
		return (v * v) / (0.36 * (1 - u*u) * (1.15 - 0.3*u))
	case 1:
		r := pixelfx.Hypot(u, v*1.1)
		a := math.Atan2(v, u)
		return r / (0.62 + 0.38*math.Pow(math.Abs(math.Cos(2.5*a)), 1.5))
	default:
		if math.Abs(u) >= 1 {
			return 9
		}
		return (v * v) / (0.16 * (1 - u*u))
	}
}

func drawLeaf(q shader, x, y, size, ang, flip float64, shape int, col []uint32, a float64) {
	back := flip < -0.25
	fs := 0.35 + 0.65*math.Abs(flip)
	c, s := math.Cos(ang), math.Sin(ang)
	L, Wd, ext := size, size*0.9, size+1.5
	face := [3]uint32{col[2], col[3], col[4]}
	vein := col[1]
	if back {
		face = [3]uint32{
			pixelfx.Mix(col[1], leavesBack1, 0.2),
			pixelfx.Mix(col[2], leavesBack2, 0.22),
			pixelfx.Mix(col[3], leavesBack3, 0.22),
		}
		vein = pixelfx.Mix(col[2], leavesBackVein, 0.5)
	}
	rim := col[0]
	q.shade(x-ext, y-ext, x+ext, y+ext, func(px, py float64, out *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		lu := (dx*c + dy*s) / L
		lv := (-dx*s + dy*c) / (Wd * fs)
		d := leafShape(shape, lu, lv)
		if lu < -1.25 || lu > 1.1 {
			return 0
		}
		if lu < -0.92 && math.Abs(lv*Wd*fs) < 0.5 {
			out.C = rim
			out.A = a
			return 0
		}
		if d > 1.32 {
			return 0
		}
		out.A = a
		if d > 1 {
			out.C = rim
		} else if math.Abs(lv*Wd*fs) < 0.45 && lu > -0.9 && lu < 0.75 {
			out.C = vein
		} else if lv < -0.15 {
			out.C = face[2]
		} else if lv < 0.35 {
			out.C = face[1]
		} else {
			out.C = face[0]
		}
		return 0
	})
}

type leavesInst struct {
	S      float64
	R3     [3][]uint32
	litter []recordedPixel
	rb     rowBuf
}

func newFallingLeaves(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	R3 := [3][]uint32{
		pixelfx.Fit(o.Palette(), 6),
		pixelfx.Fit(o.Ramp("reds"), 6),
		pixelfx.Fit(o.Ramp("golds"), 6),
	}
	var rec pixelRecorder
	dim := make([]uint32, 6)
	for i := 0; i < 64; i++ {
		fi := float64(i)
		x := -164 + pixelfx.Rand(S, fi, 91)*328
		y := leavesFloor - 2 + math.Floor(pixelfx.Rand(S, fi, 92)*4)*2
		col := R3[int(math.Floor(pixelfx.Rand(S, fi, 93)*3))]
		for k, cc := range col {
			dim[k] = pixelfx.Mix(cc, col[0], 0.12)
		}
		drawLeaf(&rec, x, y, 3.6+pixelfx.Rand(S, fi, 94)*1.8, (pixelfx.Rand(S, fi, 95)-0.5)*0.7, 0.45+pixelfx.Rand(S, fi, 96)*0.25, int(math.Floor(pixelfx.Rand(S, fi, 97)*3)), dim, 1)
	}
	return &leavesInst{S: S, R3: R3, litter: rec.px}
}

func (fl *leavesInst) Update(float64) {}

func (fl *leavesInst) DrawBack(p *pixelfx.Painter, t float64) {
	q := clip(p, &FallingLeaves.Meta)
	low := pixelfx.Mix(leavesDuskLow, fl.R3[0][0], 0.3)
	rows(p, &FallingLeaves.Meta, &fl.rb, -170, 9, func(y float64, R *rowBuf) {
		k := (y + 170) / 180
		R.fillC(pixelfx.Mix(leavesDuskTop, low, k*k))
		R.fillA(float32(0.32 + 0.1*k))
		R.fillB(1)
	})
	for _, px := range fl.litter {
		q.dot(px.x, px.y, px.c, px.a)
	}
	fl.leaves(q, t, false)
}

func (fl *leavesInst) Draw(p *pixelfx.Painter, t float64) {
	fl.leaves(clip(p, &FallingLeaves.Meta), t, true)
}

func (fl *leavesInst) leaves(q *clipper, t float64, front bool) {
	S, R3 := fl.S, fl.R3
	trips(t, leavesDuration, 62, S*19, 2, 3, func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		fi := float64(i)
		R := func(n float64) float64 { return pixelfx.Rand(S*19, fi, j, n) }
		near := R(1) < 0.35
		if near != front {
			return
		}
		dur := leavesDuration / float64(m)
		const air = 0.72
		tf := dur * air
		a := math.Min(u*dur, tf)
		rest := u*dur - tf
		w := 1.7 + R(2)*1.2
		ph0 := R(3) * tau
		ampBase, gk, sizeBase := 8.0, 0.7, 4.2
		if near {
			ampBase, gk, sizeBase = 14, 1, 5.8
		}
		amp := ampBase + R(4)*10
		x0 := -180 + R(5)*340
		const y0 = -186.0
		path := func(aa float64) float64 {
			return 0.45*aa + (0.9*(leavesHc(w*aa+ph0)-leavesHc(ph0)))/w
		}
		vy := (leavesFloor - y0) / path(tf)
		born := t - u*dur
		xAt := func(aa float64) float64 {
			return x0 + 7*aa + math.Sin(w*aa+ph0)*amp + (leavesGust(born+aa)-leavesGust(born))*gk
		}
		size := sizeBase + R(6)*1.6
		shape := int(math.Floor(R(7) * 3))
		col := R3[int(math.Floor(R(8)*3))]
		if rest > 0 {
			fade := 1 - rest/(dur-tf)
			drawLeaf(q, xAt(tf), leavesFloor-1, size, (R(9)-0.5)*0.5, 0.42, shape, col, fade)
			return
		}
		x := xAt(a)
		y := y0 + vy*path(a)
		tiltOff := 0.3
		if near {
			tiltOff = 0
		}
		tilt := math.Cos(w*a+ph0)*0.75 + tiltOff
		flipRate := 1.2
		if R(11) < 0.3 {
			flipRate = 5
		}
		flip := math.Cos(R(10)*tau + a*flipRate)
		drawLeaf(q, x, y, size, tilt, flip, shape, col, 1)
		if flip > 0.86 {
			fk := (flip - 0.86) / 0.14
			hx := x - math.Cos(tilt)*size*0.3
			hy := y - math.Sin(tilt)*size*0.3 - 1
			q.px(hx, hy, col[5])
			q.glow(hx, hy, 5+size*0.5, col[4], 0.22*fk)
		}
	})
}
