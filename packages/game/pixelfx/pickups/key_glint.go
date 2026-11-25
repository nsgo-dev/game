// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/key-glint.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the hover, floor shadow and star glints from Sprite Fusion's open-source pickup and sparkle (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// KeyGlint is an ornate silver key hovering and rocking; a glint runs from its bow down the shaft to the teeth and bursts off the tip as falling sparks. It is registered as "pickups/key-glint".
var KeyGlint = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Key glint",
		Desc:     "An ornate silver key hovering and rocking; a glint runs from its bow down the shaft to the teeth and bursts off the tip as falling sparks.",
		Tags:     []string{"pickup", "key", "unlock", "item", "loop"},
		Size:     [2]int{72, 60},
		Origin:   [2]int{36, 54},
		Duration: keyGlintDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e1018", "#222838", "#3c4660", "#5e6d8e", "#8c9cbc", "#bccae2", "#e6eeff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	New: newKeyGlint,
}

func init() { pixelfx.Register("pickups/key-glint", KeyGlint) }

const (
	keyGlintDuration = 2.0
	keyHover         = 19.0
	keyMid           = 8.0
	keyTip           = 17.5
	keyG0            = 0.18
	keyG1            = 0.5
)

func keyPose(ph float64) (y, a float64) {
	return -keyHover - 1.6*math.Sin(ph*tau), 0.42 + 0.11*math.Sin(ph*tau+0.9)
}

func keyToWorld(u, v, cx, cy, a float64) (float64, float64) {
	return cx + (u-keyMid)*math.Cos(a) - v*math.Sin(a), cy + (u-keyMid)*math.Sin(a) + v*math.Cos(a)
}

func keyPart(u, v float64) int {
	rb := pixelfx.Hypot(u, v)
	if rb <= 4.6 && rb >= 2.1 {
		return 1
	}
	if u >= 4.3 && u <= 6 && math.Abs(v) <= 1.9 {
		return 2
	}
	if u >= 6 && u <= keyTip && math.Abs(v) <= 1 {
		return 3
	}
	if u >= 12.6 && u <= keyTip-0.3 && v > 1 && v <= 4.6 && !(u > 14.3 && u < 15.4 && v > 2.7) {
		return 4
	}
	return 0
}

func drawKey(L *layer, p *pixelfx.Painter, C []uint32, cx, cy, a, gu float64) {
	ca, sa := math.Cos(a), math.Sin(a)
	L.begin(math.Floor(cx)-24, math.Floor(cy)-20)
	L.fill(math.Floor(cx)-24, math.Floor(cy)-20, math.Floor(cx)+23, math.Floor(cy)+19, func(px, py float64) uint32 {
		dx, dy := px-cx, py-cy
		u, v := dx*ca+dy*sa+keyMid, -dx*sa+dy*ca
		k := keyPart(u, v)
		if k == 0 {
			return 0
		}
		var ny float64
		if k == 1 {
			rb := pixelfx.Hypot(u, v)
			out := pick(rb > 3.4, 1.0, -1.0)
			wx, wy := (u/rb)*out, (v/rb)*out
			ny = -(((wx*ca - wy*sa) * -0.6) + (wx*sa+wy*ca)*-0.8)
		} else {
			switch {
			case v < -0.35:
				ny = 0.8
			case v > 0.35 && k != 4:
				ny = -0.6
			case k == 4:
				ny = pick(v > 3.6, -0.5, 0.1)
			default:
				ny = 0.2
			}
		}
		var c int
		switch {
		case ny > 0.55:
			c = 6
		case ny > 0.15:
			c = 4
		case ny > -0.3:
			c = 3
		default:
			c = 2
		}
		if k == 2 {
			c = min(7, c+1)
		}
		g := math.Abs(u + v*0.45 - gu)
		if g < 0.8 {
			return pixelfx.White
		}
		if g < 2 {
			c = min(7, c+2)
		}
		return C[c]
	})
	L.outline(C[0]).draw(p, 0, 0, 1)
}

func newKeyGlint(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	T := float64(keyGlintDuration)
	L := newLayer(48, 40)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			y, _ := keyPose(t / T)
			blobShadow(p, 0, 10, 0.8-(-y-keyHover+1.6)*0.06, 0)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / T
			y, a := keyPose(ph)
			q := (ph - keyG0) / (keyG1 - keyG0)
			gu := -100.0
			if q > 0 && q < 1 {
				gu = -5 + (keyTip+7)*pixelfx.Ease.InOutSine(q)
			}
			bx, by := keyToWorld(0, 0, 0, y, a)
			ex, ey := keyToWorld(keyTip, 0, 0, y, a)
			p.Glow(0, y, 24, C[4], 0.3)
			p.GlowLine(bx, by, ex, ey, 7, C[5], 0.16)
			drawKey(L, p, C, 0, y, a, gu)
			for i := range 4 {
				fi := float64(i)
				life := 0.4
				age := pixelfx.Wrap(t-((fi+0.6)/4)*T, T)
				if age > life {
					continue
				}
				an := pixelfx.Rand(fi, seed, 8) * tau
				d := 9 + pixelfx.Rand(fi, seed, 9)*6
				k := math.Sin((age / life) * pi)
				glint(p, math.Cos(an)*d, y+math.Sin(an)*d*0.8-age*6, pixelfx.Round(1.8*k), pick(i%2 != 0, C[6], pixelfx.White), 0.35*k)
			}
			if gu > -6 {
				gx, gy := keyToWorld(pixelfx.ClampTo(gu, 0, keyTip), 0, 0, y, a)
				p.Glow(gx, gy, 10, C[6], 0.45*math.Sin(q*pi))
			}
			tx, ty := keyToWorld(keyTip, 0, 0, y, a)
			at := (ph - keyG1 + 0.02) * T
			if at > 0 && at < 0.3 {
				glint(p, tx, ty, 4.5*(1-at/0.3), C[6], 0.6*(1-at/0.3))
			}
			for i := range 9 {
				fi := float64(i)
				life := 0.32 + pixelfx.Rand(fi, seed, 4)*0.3
				age := at - pixelfx.Rand(fi, seed, 5)*0.05
				if age < 0 || age > life {
					continue
				}
				sp := 25 + pixelfx.Rand(fi, seed, 1)*45
				an := a + (pixelfx.Rand(fi, seed, 2)-0.5)*2.2
				x, yy := pixelfx.Fly(tx, ty, math.Cos(an)*sp, math.Sin(an)*sp-25, age, 380, 2.5)
				vx, vy := pixelfx.FlyV(math.Cos(an)*sp, math.Sin(an)*sp-25, age, 380, 2.5)
				if yy > -0.5 {
					continue
				}
				s := pixelfx.Hypot(vx, vy)
				if s == 0 {
					s = 1
				}
				L2 := math.Min(4, 1+s*0.03)
				k := 1 - age/life
				p.LineFn(x-(vx/s)*L2, yy-(vy/s)*L2, x, yy, func(u float64) (uint32, float64) {
					return pick(u > 0.6, pixelfx.White, C[5]), k * 1.6
				})
				p.Glow(x, yy, 3, C[5], 0.4*k)
			}
		},
	}
}
