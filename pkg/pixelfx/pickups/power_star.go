// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/power-star.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rays and the white-hearted glints from Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// PowerStar is a fat bevelled star rocking side to side and swelling on each beat, rays of light turning slowly behind it, sparks spinning off its points. It is registered as "pickups/power-star".
var PowerStar = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Power star",
		Desc:     "A fat bevelled star rocks side to side and swells on each beat, rays of light turning slowly behind it, sparks spinning off its points.",
		Tags:     []string{"pickup", "star", "power-up", "invincible", "bonus", "loop"},
		Size:     [2]int{88, 84},
		Origin:   [2]int{44, 76},
		Duration: powerStarDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1604", "#5e3208", "#a05c0c", "#e09414", "#ffc626", "#ffe866", "#fff8c4", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newPowerStar,
}

func init() { pixelfx.Register("pickups/power-star", PowerStar) }

const (
	powerStarDuration = 1.6
	powerStarHover    = 25.0
	powerStarRO       = 11.5
	powerStarRI       = 5.2
	powerStarRays     = 10
)

var (
	powerStarInk   = pixelfx.Hex("#1a0e04")
	powerStarBeats = [2]float64{0.25, 0.75}
	powerStarLA    = math.Atan2(-0.8, -0.6)
)

func powerStarSwell(ph float64) float64 {
	s := 0.0
	for _, b := range powerStarBeats {
		u := pixelfx.Wrap(ph-b, 1)
		if 1-u*2.5 > 0 {
			s += math.Exp(-u * 14)
		}
	}
	return s
}

func drawPowerStar(L *layer, p *pixelfx.Painter, C []uint32, cx, cy, rot, sc float64) {
	L.begin(math.Floor(cx)-21, math.Floor(cy)-21)
	L.fill(math.Floor(cx)-21, math.Floor(cy)-21, math.Floor(cx)+20, math.Floor(cy)+20, func(px, py float64) uint32 {
		dx, dy := (px-cx)/sc, (py-cy)/sc
		r := pixelfx.Hypot(dx, dy)
		if r > powerStarRO+0.5 {
			return 0
		}
		a := pixelfx.Wrap(math.Atan2(dy, dx)-rot+pi/2, tau)
		k := math.Floor(a / (pi / 5))
		half := int(k) % 2
		tip := (math.Floor(k/2) + float64(half)) * ((2 * pi) / 5)
		tA := tip - pi/2 + rot
		iA := tA + pick(half != 0, -1.0, 1.0)*(pi/5)
		tx, ty := math.Cos(tA)*powerStarRO, math.Sin(tA)*powerStarRO
		ix, iy := math.Cos(iA)*powerStarRI, math.Sin(iA)*powerStarRI
		ex, ey := ix-tx, iy-ty
		side := (dx-tx)*ey - (dy-ty)*ex
		cside := -tx*ey + ty*ex
		if side*cside < 0 {
			return 0
		}
		n := tA + pick(half != 0, -1.0, 1.0)*(pi/2)
		l := math.Cos(n - powerStarLA)
		ridge := math.Abs(math.Sin(math.Atan2(dy, dx)-tA))*r < 0.6
		if r < 1.2 {
			return pixelfx.White
		}
		toLight := math.Cos(tA - powerStarLA)
		if ridge {
			if toLight > 0.3 {
				return pixelfx.White
			}
			if toLight > -0.4 {
				return C[6]
			}
			return C[5]
		}
		switch {
		case l > 0.6:
			return C[6]
		case l > 0.15:
			return C[5]
		case l > -0.35:
			return C[4]
		case l > -0.75:
			return C[3]
		default:
			return C[2]
		}
	})
	L.outline(powerStarInk).draw(p, 0, 0, 1)
}

func newPowerStar(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(powerStarDuration)
	L := newLayer(44, 44)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y := -powerStarHover - 1.5*math.Sin(ph*tau*2)
			sw := powerStarSwell(ph)
			blobShadow(p, 0, 9+2*sw, 0.7, 0)
			spin := ph * ((tau / powerStarRays) * 2)
			for i := range powerStarRays {
				a := spin + (float64(i)*tau)/powerStarRays
				ln := pick(i%2 != 0, 22.0, 34.0) * (1 + 0.18*sw)
				ca, sa := math.Cos(a), math.Sin(a)
				w := pick(i%2 != 0, 0.06, 0.09)
				p.Shade(-ln-2, y-ln-2, ln+2, y+ln+2, func(x, yy float64, out *pixelfx.Out) uint32 {
					rx, ry := x, yy-y
					d := rx*ca + ry*sa
					if d < 8 || d > ln {
						return 0
					}
					q, hw := math.Abs(rx*sa-ry*ca), 0.5+d*w
					if q > hw+1.5 {
						return 0
					}
					out.L = pick(q < hw*0.5, C[6], C[5])
					edge := 1.0
					if q >= hw {
						edge = 1 - (q-hw)/1.5
					}
					out.K = (0.32 + 0.22*sw) * edge * math.Pow(1-d/ln, 1.2) * math.Min(1, (d-8)/4)
					return 0
				})
				if i%2 == 0 {
					d0 := powerStarRO + 3
					floorLim := math.Inf(1)
					if sa > 0.05 {
						floorLim = (-2 - y) / sa
					}
					d1 := math.Min(ln*0.88, floorLim)
					if d1 > d0 {
						p.LineFn(ca*d0, y+sa*d0, ca*d1, y+sa*d1, func(u float64) (uint32, float64) {
							fade := 1.6 - u*1.4
							if u < 0.5 {
								fade = 1
							}
							return pick(u < 0.35, C[6], C[5]), fade * (0.75 + 0.35*sw)
						})
					}
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			y := -powerStarHover - 1.5*math.Sin(ph*tau*2)
			sw := powerStarSwell(ph)
			rot, sc := 0.22*math.Sin(ph*tau), 1+0.13*sw
			p.GlowRing(0, y, powerStarRO*sc+3, 13, C[4], 0.32+0.25*sw)
			p.Glow(0, y, 6, C[6], 0.12+0.2*sw)
			drawPowerStar(L, p, C, 0, y, rot, sc)
			for _, b := range powerStarBeats {
				age := pixelfx.Wrap(ph-b, 1) * D
				if age > 0.45 {
					continue
				}
				for i := range 5 {
					fi := float64(i)
					a := rot + (fi*tau)/5 - pi/2
					sp := 70 + pixelfx.Rand(fi, b*10, 3)*30
					tan := 35.0
					vx, vy := math.Cos(a)*sp-math.Sin(a)*tan, math.Sin(a)*sp+math.Cos(a)*tan
					x, yy := pixelfx.Fly(math.Cos(a)*powerStarRO*sc, y+math.Sin(a)*powerStarRO*sc, vx, vy, age, 60, 4)
					k := 1 - age/0.45
					glint(p, x, yy, pixelfx.Round(2*k), pick(k > 0.5, pixelfx.White, C[6]), 0.45*k)
				}
			}
			f := math.Floor(ph * 24)
			for i := range 18 {
				fi := float64(i)
				if pixelfx.Rand(f+fi*3, fi, seed) > 0.32 {
					continue
				}
				a := pixelfx.Rand(fi, 1, seed) * tau
				d := 13 + pixelfx.Rand(fi, 2, seed)*20
				sy := y + math.Sin(a)*d*0.85
				if sy < -2 {
					p.Px(math.Cos(a)*d, sy, pick(pixelfx.Rand(f, fi, 5) < 0.5, pixelfx.White, C[6]))
				}
			}
			for i := range 4 {
				fi := float64(i)
				life := 0.3
				age := pixelfx.Wrap(t-((fi+0.3)/4)*D, D)
				if age > life {
					continue
				}
				a := pixelfx.Rand(fi, 7, seed) * tau
				d := 15 + pixelfx.Rand(fi, 8, seed)*12
				k := math.Sin((age / life) * pi)
				glint(p, math.Cos(a)*d, y+math.Sin(a)*d*0.85, pixelfx.Round(2.5*k), pick(i%2 != 0, C[6], pixelfx.White), 0.4*k)
			}
			g := math.Mod(ph+0.1, 0.5) / 0.5
			if g < 0.14 {
				glint(p, -2+rot*4, y-6, 3*math.Sin((g/0.14)*pi), pixelfx.White, 0.5)
			}
		},
	}
}
