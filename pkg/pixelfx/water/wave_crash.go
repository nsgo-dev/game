// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/wave-crash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez, for the lip's crescent (a thick arc, white on its outer rim, cooling to deep blue underneath, with light along it).

package water

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// WaveCrash rolls in a swell that steepens, throws its lip forward over a dark barrel, then crashes into a burst of white foam and spray that surges on and fades to lace. It is registered as "water/wave-crash".
var WaveCrash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Wave crash",
		Desc:     "A swell rolls in and steepens, its lip throws forward and curls over a dark barrel, then crashes into a burst of white foam and spray that surges on and fades to lace.",
		Tags:     []string{"water", "wave", "crash", "surf", "foam", "spray", "one-shot"},
		Size:     [2]int{224, 104},
		Origin:   [2]int{112, 86},
		Duration: waveCrashDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newWaveCrash,
}

func init() { pixelfx.Register("water/wave-crash", WaveCrash) }

const (
	waveCrashDuration = 3.2
	waveCrashSY       = 0.22
	waveCrashG        = 820
	waveCrashCrash    = 1.2
	waveCrashLip0     = 0.62
	waveCrashEdge     = 110
)

func waveCrestX(t float64) float64 {
	if t < waveCrashCrash {
		return -80 + 72*pixelfx.Ease.InOutSine(t/waveCrashCrash)
	}
	return -8 + 30*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-waveCrashCrash)/1.4))
}

func waveCrestH(t float64) float64 {
	if t < waveCrashCrash {
		return 5 + 33*pixelfx.Ease.InOutSine(pixelfx.Clamp(t/(waveCrashCrash*0.92)))
	}
	return 38*(1-pixelfx.Ease.OutCubic(pixelfx.Clamp((t-waveCrashCrash)/0.45))) + 5*(1-pixelfx.Smooth(waveCrashCrash, waveCrashCrash+1.6, t))
}

func waveFaceW(t float64) float64 {
	if t < waveCrashCrash {
		return 24 - 16*pixelfx.Smooth(0.3, waveCrashCrash, t)
	}
	return 8 + 14*pixelfx.Smooth(waveCrashCrash, waveCrashCrash+0.6, t)
}

func waveLipK(t float64) float64 {
	return pixelfx.Clamp((t - waveCrashLip0) / (waveCrashCrash - waveCrashLip0))
}

type wavePuff struct {
	x, y, vx, vy, r0, r1, life, max, delay float64
}

type wavePlink struct {
	x, y, age float64
}

type curlCell struct {
	x, y float64
	c    uint32
	q    float64
}

func newWaveCrash(o *pixelfx.Options) pixelfx.Instance {
	C, R, S := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Seed()
	D := float64(waveCrashDuration)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R})
	ms := &mist{}
	lights := &pixelfx.Lights{}
	rings := newRipples(waveCrashSY)
	var puffs []wavePuff
	var plinks []wavePlink
	ds := newDrops(waveCrashSY, waveCrashG, 0.35, func(d *drop) {
		rings.add(d.x, d.z, 0.6, 2+d.s*1.8, 0.3+d.s*0.1, rippleOpts{n: 1, k: 0.9, hot: 0.05})
		plinks = append(plinks, wavePlink{x: d.x, y: d.z * waveCrashSY, age: 0})
	})
	t, crashed := 0.0, false
	height := func(x, tt float64) float64 {
		xc, A := waveCrestX(tt), waveCrestH(tt)
		dx := x - xc
		w := waveFaceW(tt)
		if dx < 0 {
			w = 30
		}
		dw := dx / w
		return A * math.Exp(-(dw * dw))
	}
	// lip returns a point on the lip at s (0 at the crest, 1 at its tip), and its thickness there.
	lip := func(s, tt float64) (float64, float64, float64) {
		k, xc, A := waveLipK(tt), waveCrestX(tt), waveCrestH(tt)
		return xc + 1 + s*30*math.Pow(k, 0.8), -A - 4*s*(1-s)*(1+4*k) + s*s*A*pixelfx.Ease.InQuad(k)*0.98, (11 - 8.5*s) * (0.5 + 0.5*k)
	}
	addPuff := func(x, y, vx, vy, r0, r1, life, delay float64) {
		puffs = append(puffs, wavePuff{x: x, y: y, vx: vx, vy: vy, r0: r0, r1: r1, life: life, max: life, delay: delay})
	}

	// body draws the swell's body, column by column.
	body := func(p *pixelfx.Painter, fade float64) {
		xc, A := waveCrestX(t), waveCrestH(t)
		for X := -waveCrashEdge; X < waveCrashEdge; X++ {
			x := float64(X) + 0.5
			h := height(x, t)
			edge := 1 - pixelfx.Smooth(waveCrashEdge-24, waveCrashEdge, math.Abs(x))
			if h < 0.6 || edge <= 0 {
				continue
			}
			face := x > xc
			dn := (x - xc) / 9
			near := math.Exp(-(dn * dn))
			a := fade * edge * 1.4
			for Y := math.Floor(-h); Y < 0; Y++ {
				y := Y + 0.5
				u, depth := (y+h)/h, y+h
				var c uint32
				if depth < 1 {
					if near > 0.5 && A > 14 && pixelfx.Noise(x*0.6+t*9, S) > -0.35 {
						c = pixelfx.White
					} else if face {
						c = C[5]
					} else {
						c = C[6]
					}
				} else if depth < 2.4 && near > 0.35 && A > 14 {
					if pixelfx.Noise2(x*0.5, y*0.5+t*6, S+1) > -0.1 {
						c = pixelfx.White
					} else {
						c = C[6]
					}
				} else {
					// face and back shade differently; blend the two across the crest so there's no seam
					win := near * (1 - u) * 1.6
					up := pixelfx.Noise2((x*0.9-y*0.35)*0.8, y*0.12+t*5, S+2)
					wind := pixelfx.Noise2(x*0.18-t*2, y*0.9, S+3)
					var fi, bi float64
					switch {
					case up > 0.55 && u < 0.85:
						fi = 6
					case win > 0.8:
						fi = 5
					case win > 0.45 || up > 0.3:
						fi = 4
					case win > 0.2 || up > 0.05:
						fi = 3
					case u < 0.6:
						fi = 2
					default:
						fi = 1
					}
					switch {
					case u < 0.14:
						bi = 4
					case wind > 0.55:
						bi = 4
					case wind > 0.25 || u < 0.32:
						bi = 3
					case u < 0.7:
						bi = 2
					default:
						bi = 1
					}
					c = C[int(pixelfx.Round(bi+(fi-bi)*pixelfx.Smooth(xc-5, xc+5, x)))]
				}
				p.Dot(float64(X), Y, c, a)
			}
		}
	}

	// curl draws the lip: a thick arc thrown off the crest, white along its outer edge, tinted through, dark underneath.
	curl := func(p *pixelfx.Painter) {
		k := waveLipK(t)
		if k <= 0 || t >= waveCrashCrash {
			return
		}
		cells := make(map[int]int)
		var list []curlCell
		for s := 0.0; s <= 1; s += 0.012 {
			x, y, w := lip(s, t)
			x2, y2, _ := lip(math.Min(1, s+0.012), t)
			tx, ty := x2-x, y2-y
			l := pixelfx.Hypot(tx, ty)
			if l == 0 {
				l = 1
			}
			nx, ny := ty/l, -tx/l
			step := 0.5 / math.Max(1, w)
			for q := 0.0; q <= 1; q += step {
				px, py := x+nx*(q-0.5)*w, y+ny*(q-0.5)*w
				key := int(math.Floor(py))*1024 + int(math.Floor(px))
				var c uint32
				switch {
				case q < 0.18:
					c = pixelfx.White
				case q < 0.36:
					if s > 0.8 {
						c = pixelfx.White
					} else {
						c = C[6]
					}
				case q < 0.56:
					c = C[5]
				case q < 0.76:
					c = C[4]
				default:
					c = C[3]
				}
				if idx, ok := cells[key]; !ok {
					cells[key] = len(list)
					list = append(list, curlCell{x: px, y: py, c: c, q: q})
				} else if q < list[idx].q {
					list[idx] = curlCell{x: px, y: py, c: c, q: q}
				}
			}
		}
		for _, c := range list {
			p.Px(c.x, c.y, c.c)
		}
		for s := 0.0; s <= 1; s += 0.125 {
			x, y, w := lip(s, t)
			p.Glow(x, y, 6+w, C[5], 0.24*k)
		}
		// the tip glints as it throws
		tx, ty, _ := lip(1, t)
		g := math.Sin(pi * pixelfx.Clamp((k-0.55)/0.45))
		if g > 0.05 {
			pixelfx.Sparkle(p, tx, ty, 6*g, C[6], C[5], 0.5*g)
		}
		// light caught under the barrel
		bx, by, _ := lip(0.6, t)
		p.Glow(bx-3, by+8, 16, C[4], 0.25*k)
	}

	// sea draws the sea it rolls on: a lit line along the surface, dark water under it, glints sliding along.
	sea := func(p *pixelfx.Painter, fade float64) {
		for X := -waveCrashEdge; X < waveCrashEdge; X++ {
			x := float64(X) + 0.5
			edge := 1 - pixelfx.Smooth(waveCrashEdge-30, waveCrashEdge, math.Abs(x))
			a := fade * edge
			if a <= 0.02 {
				continue
			}
			g := pixelfx.Noise(x*0.3-t*3, S+4)
			c := C[4]
			if g > 0.55 {
				c = C[6]
			}
			p.Dot(float64(X), 0, c, a*1.2)
			p.Blend(x, 1.5, C[2], 0.6*a)
			p.Blend(x, 2.5, C[1], 0.45*a)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			k := waveLipK(t)
			// wind tears spray off the crest; the lip's tip sheds drops as it throws
			if t < waveCrashCrash && waveCrestH(t) > 14 && R.Chance(0.5) {
				xc, A := waveCrestX(t), waveCrestH(t)
				fx := xc + R.Range(-4, 3)
				fy := -A - R.Range(0, 2)
				fvx := -R.Range(20, 60)
				fvy := -R.Range(10, 40)
				flife := R.Range(0.5, 0.9)
				fc := C[6]
				if R.Chance(0.5) {
					fc = pixelfx.White
				}
				fsize := R.Range(0.6, 1.3)
				ps.Spawn(pixelfx.Flake, fx, fy, fvx, fvy, flife, fc, fsize, 0, 0)
			}
			if k > 0.3 && t < waveCrashCrash && R.Chance(0.35) {
				x, y, _ := lip(1, t)
				dz := R.Range(-3, 3)
				vx := R.Range(30, 90)
				vz := R.Range(-10, 10)
				vh := R.Range(-20, 40)
				s := 1.0
				if R.Chance(0.3) {
					s = 2
				}
				ds.add(drop{x: x, z: dz, h: -y, vx: vx, vz: vz, vh: vh, s: s})
			}
			if !crashed && t >= waveCrashCrash {
				crashed = true
				X0, _, _ := lip(1, waveCrashCrash-1e-3)
				lights.Add(X0, -20, 56, C[5], 0.8, 0.55)
				lights.Add(X0, -4, 22, pixelfx.White, 0.6, 0.1)
				for range 46 {
					a, v := R.Range(-0.9, 0.6)*pi, R.Range(30, 120)
					dx := X0 + R.Range(-6, 6)
					dz := R.Range(-4, 4)
					dh := R.Range(2, 8)
					vh := R.Range(120, 320)
					var s float64
					if R.Chance(0.15) {
						s = 3
					} else if R.Chance(0.35) {
						s = 2
					} else {
						s = 1
					}
					delay := R.Range(0, 0.1)
					ds.add(drop{x: dx, z: dz, h: dh, vx: math.Cos(a)*v*0.85 + 18, vz: math.Sin(a) * v * 0.5, vh: vh, s: s, delay: delay})
				}
				// the spit: a plume shot straight up where the lip lands
				for range 14 {
					dx := X0 + R.Range(-3, 3)
					dz := R.Range(-2, 2)
					dh := R.Range(2, 6)
					vx := R.Range(-15, 25)
					vz := R.Range(-8, 8)
					vh := R.Range(220, 300)
					s := 1.0
					if R.Chance(0.3) {
						s = 2
					}
					delay := R.Range(0.04, 0.16)
					ds.add(drop{x: dx, z: dz, h: dh, vx: vx, vz: vz, vh: vh, s: s, delay: delay})
				}
				for range 34 {
					px := X0 + R.Range(-12, 10)
					py := -R.Range(2, 12)
					pvx := R.Range(-40, 130)
					pvy := -R.Range(30, 150)
					pr0 := R.Range(1, 2)
					pr1 := R.Range(1.8, 4.2)
					plife := R.Range(0.4, 0.95)
					pdelay := R.Range(0, 0.14)
					addPuff(px, py, pvx, pvy, pr0, pr1, plife, pdelay)
				}
				// fine spray hangs in the air over the burst
				for range 34 {
					a, v := R.Range(-pi*0.95, -pi*0.05), R.Range(40, 160)
					fx := X0 + R.Range(-8, 8)
					fy := -R.Range(4, 14)
					flife := R.Range(0.6, 1.3)
					fc := C[6]
					if R.Chance(0.5) {
						fc = pixelfx.White
					}
					fsize := R.Range(0.6, 1.4)
					fdelay := R.Range(0, 0.1)
					ps.Spawn(pixelfx.Flake, fx, fy, math.Cos(a)*v+25, math.Sin(a)*v, flife, fc, fsize, fdelay, 0)
				}
				for range 12 {
					mx := X0 + R.Range(-10, 14)
					my := -R.Range(3, 16)
					mvx := R.Range(-20, 70)
					mvy := -R.Range(6, 24)
					mr1 := R.Range(9, 15)
					mlife := R.Range(1.1, 1.7)
					mc := pixelfx.White
					if R.Chance(0.5) {
						mc = C[6]
					}
					mdelay := R.Range(0, 0.2)
					ms.add(mx, my, mvx, mvy, 3, mr1, mlife, mc, 0.26, mdelay)
				}
				for range 10 {
					sx := X0 + R.Range(-20, 24)
					sy := -R.Range(6, 40)
					slife := R.Range(0.3, 0.6)
					ssize := R.Range(1, 2.4)
					sdelay := R.Range(0.05, 0.4)
					ps.Spawn(pixelfx.StarKind, sx, sy, 0, -5, slife, C[6], ssize, sdelay, 0)
				}
				rings.add(X0, 0, 6, 40, 1.2, rippleOpts{n: 2, gap: 3, k: 1.1, hot: 0.12})
			}
			// the whitewater surges on, boiling up foam at its front, then calms
			if crashed && t < waveCrashCrash+1.1 && R.Chance(0.45*(1-(t-waveCrashCrash)/1.1)) {
				x := waveCrestX(t) + R.Range(-6, 14)
				py := -R.Range(1, 4)
				pvx := R.Range(10, 40)
				pvy := -R.Range(10, 40)
				pr0 := R.Range(0.9, 1.8)
				pr1 := R.Range(1.8, 3.6)
				plife := R.Range(0.4, 0.8)
				addPuff(x, py, pvx, pvy, pr0, pr1, plife, 0)
			}
			if crashed && t < waveCrashCrash+0.8 && R.Chance(0.12) {
				mx := waveCrestX(t) + R.Range(-8, 8)
				my := -R.Range(2, 6)
				mvx := R.Range(10, 40)
				mvy := -R.Range(3, 10)
				mr1 := R.Range(7, 11)
				mlife := R.Range(0.8, 1.1)
				ms.add(mx, my, mvx, mvy, 3, mr1, mlife, C[6], 0.2, 0)
			}
			for i := len(puffs) - 1; i >= 0; i-- {
				f := &puffs[i]
				if f.delay > 0 {
					f.delay -= dt
					continue
				}
				f.life -= dt
				if f.life <= 0 {
					puffs = append(puffs[:i], puffs[i+1:]...)
					continue
				}
				f.vx -= f.vx * 2.5 * dt
				f.vy += (160 - f.vy*2.5) * dt
				f.x += f.vx * dt
				f.y = math.Min(f.y+f.vy*dt, -1)
			}
			for i := len(plinks) - 1; i >= 0; i-- {
				plinks[i].age += dt
				if plinks[i].age > 0.12 {
					plinks = append(plinks[:i], plinks[i+1:]...)
				}
			}
			ds.update(dt)
			rings.update(dt)
			ms.update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			fade := pixelfx.Smooth(0, 0.25, t) * (1 - pixelfx.Smooth(D-0.7, D-0.1, t))
			sea(p, fade)
			rings.draw(p, C, -1)
			ds.draw(p, C, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			fade := pixelfx.Smooth(0, 0.25, t) * (1 - pixelfx.Smooth(D-0.7, D-0.1, t))
			body(p, fade)
			curl(p)
			rings.draw(p, C, 1)
			// lace left on the water behind the surge, thinning out
			if crashed {
				k := 1 - pixelfx.Smooth(waveCrashCrash+0.4, D-0.15, t)
				x0, x1 := -20.0, waveCrestX(t)+10
				for X := math.Floor(x0); X < x1; X++ {
					x := X + 0.5
					n := 1 - math.Abs(pixelfx.Noise2(x*0.35, t*0.8, S+7))*4
					if n > 1.15-k*1.1 {
						c := C[6]
						if n > 1.4-k {
							c = pixelfx.White
						}
						p.Px(X, 0, c)
					}
					if n > 1.4-k*0.9 {
						p.Px(X, 1, C[5])
					}
				}
			}
			for _, f := range puffs {
				if f.delay > 0 {
					continue
				}
				g := 1 - f.life/f.max
				r := (f.r0 + (f.r1-f.r0)*pixelfx.Ease.OutCubic(g)) * (1 - pixelfx.Smooth(0.7, 1, g))
				if r > 0.6 {
					billow(p, f.x, f.y, r, C, 1)
				}
			}
			for _, q := range plinks {
				if q.age < 0.06 {
					p.Px(q.x, q.y, pixelfx.White)
				}
				p.Glow(q.x, q.y, 4, C[5], 0.25*(1-q.age/0.12))
			}
			ds.draw(p, C, pixelfx.LayerFront)
			// the crash: a white burst where the lip lands
			f := (t - waveCrashCrash) / 0.08
			if f >= 0 && f < 1 {
				X0, _, _ := lip(1, waveCrashCrash-1e-3)
				p.Circle(X0, -3, 8*(1-f*0.4), pixelfx.White, 1)
				p.EllipseFn(X0, 0, 14, 2.6, func(d, _ float64) (uint32, float64) {
					if d < 0.6 {
						return pixelfx.White, 1
					}
					return C[6], 1
				})
			}
			ps.Draw(p, pixelfx.LayerBoth)
			ms.draw(p, C[5])
			lights.Draw(p)
			// light: the crest catches it while it stands, the whitewater after
			if t < waveCrashCrash {
				s := pixelfx.Smooth(0.2, waveCrashCrash, t)
				p.Glow(waveCrestX(t)+4, -waveCrestH(t)+2, 26, C[4], 0.22*s)
				p.GlowLine(waveCrestX(t)-26, -waveCrestH(t)*0.45, waveCrestX(t), -waveCrestH(t), 10, C[5], 0.14*s)
			} else {
				p.Glow(waveCrestX(t), -10, 34, C[4], 0.26*(1-pixelfx.Smooth(waveCrashCrash, D-0.3, t)))
			}
		},
	}
}
