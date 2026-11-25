// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/rain-splash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source rain (https://www.spritefusion.com/pixel-art-vfx/rain), MIT License, Copyright (c) 2026 Hugo Duprez, for the streaks falling in (a dim body, a bright head).

package water

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// RainSplash is a low close-up on one puddle in the rain: big drops land in crowns that throw beads and a rebound jet, rings spread and interfere, a lamp's reflection wobbles on the water. It is registered as "water/rain-splash".
var RainSplash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Rain splash",
		Desc:     "A low close-up on one puddle in the rain: big drops land in crowns that throw beads and a rebound jet, rings spread and interfere, a lamp's reflection wobbles on the water.",
		Tags:     []string{"water", "rain", "splash", "puddle", "close-up", "ambient", "loop"},
		Size:     [2]int{176, 80},
		Origin:   [2]int{88, 50},
		Duration: rainSplashDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newRainSplash,
}

func init() { pixelfx.Register("water/rain-splash", RainSplash) }

const (
	rainSplashDuration = 2.4
	rainSplashSY       = 0.3
	rainSplashRX       = 92
	rainSplashG        = 640
	rainSplashN        = 8
	rainSplashLamp     = 34
)

type rainDrop struct {
	x, z, t0, H, J, spin float64
	i                    int
}

type rainRing struct {
	rainDrop
	a, R, k float64
}

func makeRainDrops(S float64) [rainSplashN]rainDrop {
	var out [rainSplashN]rainDrop
	for i := range rainSplashN {
		fi := float64(i)
		out[i] = rainDrop{
			x:    (pixelfx.Rand(fi, S) - 0.5) * 120,
			z:    (pixelfx.Rand(fi, S+1) - 0.5) * 50,
			t0:   ((fi + 0.25*pixelfx.Rand(fi, S+2)) / rainSplashN) * rainSplashDuration,
			H:    10 + 4*pixelfx.Rand(fi, S+3),
			J:    7 + 4*pixelfx.Rand(fi, S+4),
			spin: pixelfx.Rand(fi, S+5) * tau,
			i:    i,
		}
	}
	return out
}

func rainAgeOf(t, t0 float64) float64 { return pixelfx.Wrap(t-t0, rainSplashDuration) }

func rainRingR(a float64) float64 { return 3 + 34*math.Pow(a/1.8, 0.7) }

func rainRingK(a float64) float64 {
	if a >= 1.8 {
		return 0
	}
	return 1.3 * math.Pow(1-a/1.8, 1.3) * math.Min(1, a/0.06)
}

func rainCrownH(a, H float64) float64 {
	if a >= 0.3 {
		return 0
	}
	return H * math.Pow(math.Sin(pi*pixelfx.Clamp(a/0.3)), 0.8)
}

func rainCrownR(a float64) float64 { return 1.5 + 4.5*math.Sqrt(pixelfx.Clamp(a/0.3)) }

func rainJetH(a, J float64) float64 {
	if a > 0.26 && a < 0.56 {
		return J * math.Sin((pi*(a-0.26))/0.3)
	}
	return 0
}

// rainField returns the water's height at a point: every ring's swell summed, so where two cross they add and cancel.
func rainField(x, z float64, list []rainRing) float64 {
	h := 0.0
	for i := range list {
		q := &list[i]
		dx := x - q.x
		if dx > q.R+10 || dx < -q.R-10 {
			continue
		}
		d := pixelfx.Hypot(dx, z-q.z) - q.R
		w := 3.6 + q.a*3
		if d > 2*w || d < -3*w {
			continue
		}
		dw := d / w
		h += q.k * math.Cos(d*1.1) * math.Exp(-(dw * dw))
	}
	return h
}

// rainCrown draws a crown: a ring of fingers thrown up round the drop's hole, a bead on each tip until they tear away.
func rainCrown(p *pixelfx.Painter, C []uint32, q rainDrop, a float64, back bool) {
	h := rainCrownH(a, q.H)
	if h < 1 {
		return
	}
	r0, cy := rainCrownR(a), q.z*rainSplashSY
	for f := 0; f < 10; f++ {
		ang := q.spin + (float64(f)/10)*tau
		s := math.Sin(ang)
		if (s < 0) != back {
			continue
		}
		tip := h * (0.75 + 0.25*pixelfx.Rand(float64(q.i), float64(f), 7))
		lean := 0.35 * tip
		x0, y0 := q.x+math.Cos(ang)*r0, cy+s*r0*rainSplashSY
		x1, y1 := q.x+math.Cos(ang)*(r0+lean), cy+s*(r0+lean)*rainSplashSY-tip
		p.LineFn(x0, y0, x1, y1, func(u float64) (uint32, float64) {
			if back {
				if u > 0.7 {
					return C[5], 1
				}
				return C[3], 1
			}
			if u > 0.75 {
				return pixelfx.White, 1
			}
			if u > 0.35 {
				return C[6], 1
			}
			return C[5], 1
		})
		if a > 0.08 && a < 0.17 {
			c := pixelfx.White
			if back {
				c = C[5]
			}
			p.Px(x1, y1-1, c)
		}
	}
	if !back {
		p.Glow(q.x, cy-h*0.6, 6+h*0.6, C[5], 0.3*(h/q.H))
	}
}

func newRainSplash(o *pixelfx.Options) pixelfx.Instance {
	C, S := pixelfx.Fit(o.Palette(), 8), o.Seed()
	D := float64(rainSplashDuration)
	allDrops := makeRainDrops(S)
	ry := float64(rainSplashRX) * rainSplashSY
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			var list [rainSplashN]rainRing
			nList := 0
			for _, q := range allDrops {
				a := rainAgeOf(t, q.t0)
				if a < 1.8 {
					list[nList] = rainRing{rainDrop: q, a: a, R: rainRingR(a), k: rainRingK(a)}
					nList++
				}
			}
			var act [rainSplashN]rainRing
			for Y := math.Floor(-ry); Y < ry; Y++ {
				y := Y + 0.5
				z := y / rainSplashSY
				nAct := 0
				for i := 0; i < nList; i++ {
					if math.Abs(z-list[i].z) <= list[i].R+12 {
						act[nAct] = list[i]
						nAct++
					}
				}
				far := (y + ry) / (2 * ry)
				// the lamp's reflection on this row: a dash of light, its width shimmering, pushed about by the swell
				dashW := (1.5 + far*4) * (0.35 + 0.65*(0.5+0.5*math.Sin(Y*1.73+ph*tau*3+math.Sin(Y*0.61)*3)))
				hPrev := 0.0
				for X := -88; X < 88; X++ {
					x := float64(X) + 0.5
					dx, dy := x/rainSplashRX, y/ry
					d := dx*dx + dy*dy
					if d > 1 {
						continue
					}
					edge := math.Min(1, (1-d)*6)
					// the puddle: dark see-through water, the far part holding a little of the sky
					p.Blend(x, y, C[0], (0.62+0.2*far)*edge)
					if far < 0.45 {
						p.Blend(x, y, C[2], 0.3*(1-far/0.45)*edge)
					}
					// its swell: every ring summed, so crests catch the light, troughs go dark, and crossings beat
					h := 0.0
					if nAct > 0 {
						h = rainField(x, z, act[:nAct])
					}
					hx := h - hPrev
					hPrev = h
					if h > 0.24 {
						c := C[4]
						if h > 0.75 {
							c = C[5]
						}
						p.Blend(x, y, c, math.Min(0.95, (h-0.24)*2.2)*edge)
					} else if h < -0.4 {
						p.Blend(x, y, C[0], math.Min(0.85, (-h-0.4)*1.8)*edge)
					}
					if x < rainSplashLamp-14 || x > rainSplashLamp+14 {
						continue
					}
					lx := math.Abs(x - rainSplashLamp - hx*9 - math.Sin(y*0.8+ph*tau*2)*1.4)
					if lx < dashW {
						k := (1 - lx/dashW) * edge
						c := C[5]
						if k > 0.72 {
							c = pixelfx.White
						} else if k > 0.35 {
							c = C[6]
						}
						p.Px(float64(X), Y, c)
						p.Light(x, y, C[5], 0.2*k)
					}
				}
			}
			// the far rim of the puddle catches the light
			for X := -82; X < 82; X++ {
				x := float64(X) + 0.5
				dx := x / rainSplashRX
				y := -math.Sqrt(math.Max(0, 1-dx*dx)) * ry
				c := C[3]
				if math.Abs(x-rainSplashLamp) < 10 {
					c = C[6]
				}
				p.Px(float64(X), y+0.5, c)
			}
			// the back halves of the crowns, behind their own front walls
			for _, q := range allDrops {
				rainCrown(p, C, q, rainAgeOf(t, q.t0), true)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			for _, q := range allDrops {
				a, cy := rainAgeOf(t, q.t0), q.z*rainSplashSY
				// falling in: a streak dropping into the frame just before it lands
				pre := D - a
				if pre < 0.14 && cy-pre*520-10 > -48 {
					y := cy - pre*520
					p.Line(q.x, y-10, q.x, y-4, C[4], 1)
					p.Line(q.x, y-4, q.x, y, C[6], 1)
				}
				// the hit: a white fleck and a flash of light, then the crown
				if a < 0.05 {
					p.Px(q.x, cy, pixelfx.White)
					p.Glow(q.x, cy-2, 12, C[5], 0.5*(1-a/0.05))
				}
				rainCrown(p, C, q, a, false)
				// beads torn off the crown's tips arc out and fall back, each with a fleck and a little ring
				if a > 0.16 && a < 0.8 {
					for f := 0; f < 10; f += 2 {
						ang := q.spin + (float64(f)/10)*tau
						tip := rainCrownH(0.16, q.H) * (0.75 + 0.25*pixelfx.Rand(float64(q.i), float64(f), 7))
						r0 := rainCrownR(0.16) + tip*0.35
						v := 22 + 18*pixelfx.Rand(float64(q.i), float64(f), 8)
						vh := 40 + 30*pixelfx.Rand(float64(q.i), float64(f), 9)
						tt := a - 0.16
						hh := tip + vh*tt - 0.5*rainSplashG*tt*tt
						bx := q.x + math.Cos(ang)*(r0+v*tt)
						bz := q.z + math.Sin(ang)*(r0+v*tt)
						by := bz * rainSplashSY
						if hh > 0 {
							s := 1.0
							if pixelfx.Rand(float64(q.i), float64(f), 10) < 0.4 {
								s = 2
							}
							bead(p, bx, by-hh, math.Cos(ang)*v, -(vh-rainSplashG*tt)+math.Sin(ang)*v*rainSplashSY, s, C, 1)
						} else {
							land := (vh + math.Sqrt(vh*vh+2*rainSplashG*tip)) / rainSplashG
							since := tt - land
							if since < 0.05 {
								p.Px(bx, by, pixelfx.White)
							}
							if since < 0.35 {
								ring(p, bx, by, 1+since*12, rainSplashSY, 1-since/0.35, C, ringOpts{n: 1, half: 1, seed: float64(q.i*10 + f)})
							}
						}
					}
				}
				// the rebound jet, with a bead that pinches off its top and drops back
				jh := rainJetH(a, q.J)
				if jh > 0.8 {
					p.LineFn(q.x, cy-1, q.x, cy-jh, func(u float64) (uint32, float64) {
						if u > 0.6 {
							return pixelfx.White, 1
						}
						return C[6], 1
					})
					p.Px(q.x+1, cy-1, C[5])
					if a < 0.41 {
						p.Circle(q.x, cy-jh-0.5, 1.2, C[6], 1)
					}
					p.Glow(q.x, cy-jh*0.6, 6, C[5], 0.3)
				}
				ba := a - 0.41
				bh := q.J + 1 + 24*ba - 0.5*rainSplashG*ba*ba
				if ba > 0 && bh > 0 {
					p.Circle(q.x, cy-bh, 1.1, C[6], 1)
					p.Px(q.x, cy-bh-1, pixelfx.White)
				}
			}
			// the near halves of the main rings, crisp, so each splash has its own edge in the swell
			for _, q := range allDrops {
				a := rainAgeOf(t, q.t0)
				if a < 1.8 {
					ring(p, q.x, q.z*rainSplashSY, rainRingR(a), rainSplashSY, 0.75*rainRingK(a), C, ringOpts{n: 1, half: 1, hot: a < 0.1, seed: float64(q.i), wob: 0.5 + a})
				}
			}
			// finer rain dimpling the water, and a few streaks falling past
			pixelfx.Cycle(t, D, 16, S+20, pixelfx.Fixed(0.5), func(age, _ float64, r *pixelfx.Rng, _ int) {
				x := r.Range(-80, 80)
				z := r.Range(-26, 26)
				y := z * rainSplashSY
				s := age - 0.1
				if s < 0 {
					yy := y - (0.1-age)*600
					if yy-6 < -48 {
						return
					}
					p.LineFn(x, yy-6, x, yy, func(u float64) (uint32, float64) {
						if u > 0.6 {
							return C[5], 1
						}
						return C[3], 1
					})
				} else if s < 0.4 && (x/rainSplashRX)*(x/rainSplashRX)+(y/ry)*(y/ry) < 0.9 {
					if s < 0.04 {
						p.Px(x, y, C[6])
					}
					ring(p, x, y, 1+s*9, rainSplashSY, 0.8*(1-s/0.4), C, ringOpts{n: 1, half: 1, seed: 40})
				}
			})
			// the lamp's glow on the water
			p.Glow(rainSplashLamp, -6, 30, C[4], 0.16)
		},
	}
}
