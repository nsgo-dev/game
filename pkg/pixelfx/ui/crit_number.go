// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/crit-number.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CritNumber slams "CRIT 128!" in huge and white over a jagged starburst with a hard shake and speed lines, cools to fire, holds with a shine, then pops into sparks. It is registered as "ui/crit-number".
var CritNumber = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Crit number",
		Desc:     "CRIT 128! slams in huge and white over a jagged starburst with a hard shake and speed lines, cools to fire, holds with a shine, then pops into sparks.",
		Tags:     []string{"ui", "critical", "damage", "number", "combat", "text", "one-shot"},
		Size:     [2]int{152, 120},
		Origin:   [2]int{76, 60},
		Duration: critDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0710", "#5e0f16", "#a3191c", "#e0401c", "#ff7b22", "#ffb93b", "#ffe68a", "#ffffff")},
			{Name: "text", Default: pixelfx.Text("128!")},
			{Name: "label", Default: pixelfx.Text("CRIT")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newCritNumber,
}

func init() { pixelfx.Register("ui/crit-number", CritNumber) }

var (
	critDuration = 1.5
	critFold     = 0.92
	critTagOut   = 0.98
	critPop      = 1.06
	critSX       = 1.3
	critNS       = 4.0
	critNY       = 5.0
)

type critTip struct {
	length, dip float64
}

type critLine struct {
	a, r, length float64
}

func drawStarburst(p *pixelfx.Painter, C []uint32, cx, cy, R, rot float64, tips []critTip, heat, boil, a float64) {
	if R < 2 || a <= 0 {
		return
	}
	n := len(tips)
	V := make([][3]float64, n*2)
	for i := range n * 2 {
		an := rot + (float64(i)*pi)/float64(n)
		var r float64
		if i%2 != 0 {
			r = R * (0.6 + tips[i>>1].dip)
		} else {
			r = R * (tips[i>>1].length * (1 + 0.14*(pixelfx.Rand(boil, float64(i))-0.5)))
		}
		V[i] = [3]float64{math.Cos(an) * r, math.Sin(an) * r, an}
	}
	step := pi / float64(n)
	edge := func(dx, dy float64) float64 {
		ang := math.Atan2(dy, dx) - rot
		ang -= math.Floor(ang/tau) * tau
		i := min(len(V)-1, int(math.Floor(ang/step)))
		x0, y0 := V[i][0], V[i][1]
		x1, y1 := V[(i+1)%len(V)][0], V[(i+1)%len(V)][1]
		ex, ey := x1-x0, y1-y0
		den := dx*ey - dy*ex
		if math.Abs(den) < 1e-9 {
			return pixelfx.Hypot(x0, y0)
		}
		return (x0*ey - y0*ex) / den
	}
	M := R*1.15 + 2
	ramp := [6]uint32{pixelfx.White, C[6], C[5], C[4], C[3], C[2]}
	p.Shade(cx-M*critSX, cy-M, cx+M*critSX, cy+M, func(x, y float64, o *pixelfx.Out) uint32 {
		dx, dy := (x-cx)/critSX, y-cy
		d := pixelfx.Hypot(dx, dy)
		if d < 0.01 {
			return pixelfx.White
		}
		L := edge(dx/d, dy/d)
		gap := (L - d) * (1 + 0.3*math.Abs(dx/d))
		q := d / L
		if gap < 0 {
			return 0
		}
		o.A = a
		if heat > 0.85 {
			if gap < 1 {
				return C[6]
			}
			return pixelfx.White
		}
		if gap < 1 {
			return C[1]
		}
		if gap < 2.2 {
			if heat > 0.3 {
				return pixelfx.White
			}
			return C[6]
		}
		if gap < 3.4 {
			if heat > 0.3 {
				return C[6]
			}
			return C[5]
		}
		if gap >= 6 && gap < 7 {
			if heat > 0.5 {
				return pixelfx.White
			}
			return C[4]
		}
		an := math.Atan2(dy, dx) - rot
		sp := math.Abs(math.Mod((an/tau)*float64(n)+0.5, 1)-0.5) * (tau / float64(n)) * d
		if sp < 0.6 && q > 0.25 {
			if heat > 0.5 {
				return pixelfx.White
			}
			if q > 0.6 {
				return C[5]
			}
			return C[4]
		}
		base := 5.0
		if q > 0.72 {
			base = 3
		} else if q > 0.45 {
			base = 4
		}
		i := int(pixelfx.Round(base - heat*5))
		if i <= 0 {
			return pixelfx.White
		}
		return ramp[i]
	})
}

func drawCritTag(p *pixelfx.Painter, c0, c2, c3, c4 uint32, x, y, w, h, lean float64) {
	p.Shade(x-1, y-1, x+w+lean+1, y+h+1, func(px, py float64, _ *pixelfx.Out) uint32 {
		lx := px - x - lean*((y+h-py)/h)
		if py < y || py > y+h || lx < 0 || lx > w {
			return 0
		}
		if lx < 1 || lx > w-1 || py < y+1 || py > y+h-1 {
			return c0
		}
		if py < y+2 {
			return c4
		}
		if py < y+h*0.55 {
			return c3
		}
		return c2
	})
}

func drawCritSpeedLines(p *pixelfx.Painter, C []uint32, t float64, lines []critLine) {
	k := t / 0.24
	if k <= 0 || k >= 1 {
		return
	}
	for _, l := range lines {
		r0 := l.r * (1 + 0.5*pixelfx.Ease.OutExpo(k))
		r1 := r0 + l.length*(1-k) + 2
		ca, sa := math.Cos(l.a)*1.2, math.Sin(l.a)*0.92
		p.LineFn(ca*r0, sa*r0, ca*r1, sa*r1, func(u float64) (uint32, float64) {
			if u > 0.5 || k < 0.3 {
				return pixelfx.White, 1
			}
			return C[6], 1
		})
	}
}

func newCritNumber(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	SHADOW := pixelfx.Scale(C[0], 0.5)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	bits := pixelfx.NewBits(nil)
	lights := &pixelfx.Lights{}
	num, lab := spell(o.Text("text"), "big"), spell(o.Text("label"), "mini")
	tips := make([]critTip, 13)
	for i := range tips {
		tips[i] = critTip{length: R.Range(0.86, 1.12), dip: R.Range(-0.06, 0.06)}
	}
	lines := make([]critLine, 18)
	for i := range lines {
		lines[i] = critLine{a: (float64(i)/18)*tau + R.Range(-0.1, 0.1), r: R.Range(31, 34), length: R.Range(8, 15)}
	}
	reach := func(v, d float64) float64 {
		if v <= d*2.2 {
			return 9
		}
		return -math.Log(1-(d*2.2)/v) / 2.2
	}
	end := critDuration - 0.05
	t, slammed, stamped, popped, tagGone := 0.0, false, false, false, false
	spawnStar := func(x, y, vx, vy, life float64, c uint32, size float64) {
		ps.Spawn(pixelfx.StarKind, x, y, vx, vy, math.Min(life, end-t), c, size, 0, 0)
	}
	sizeAt := func(t float64) float64 {
		return critNS + 3*math.Exp(-t*26)*math.Cos(t*38)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !slammed {
				slammed = true
				lights.Add(0, 0, 70, pixelfx.White, 0.9, 0.16)
				lights.Add(0, 0, 60, C[4], 0.45, 0.5)
				for range 30 {
					an := R.Range(0, tau)
					sp := R.Range(140, 300)
					ps.Spawn(pixelfx.Spark, math.Cos(an)*8, math.Sin(an)*6, math.Cos(an)*sp*1.15, math.Sin(an)*sp-40, math.Min(R.Range(0.12, 0.32), reach(sp*1.15, 40)), pixelfx.White, 1, 0, 0)
				}
				for range 10 {
					an := R.Range(0, tau)
					sp := R.Range(90, 170)
					life := math.Min(R.Range(0.3, 0.5), end-t)
					a := R.Range(0, tau)
					va := R.Range(-24, 24)
					s := R.Range(2, 3.5)
					c := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					bits.Add(pixelfx.Bit{
						Kind: pixelfx.Shard, X: math.Cos(an) * 10, Y: math.Sin(an) * 7,
						VX: math.Cos(an) * sp * 1.15, VY: math.Sin(an)*sp - 60,
						G: 0.35, Drag: 1.6, Life: life, A: a, VA: va, S: s, C: c,
					})
				}
			}
			if !stamped && t >= 0.09 {
				stamped = true
				lights.Add(-4, -20, 30, C[6], 0.5, 0.2)
				for range 6 {
					px := -4 + R.Range(-16, 16)
					py := -20 + R.Range(-4, 4)
					vx := R.Range(-40, 40)
					vy := R.Range(-30, 10)
					life := R.Range(0.2, 0.4)
					size := R.Range(0.5, 1.4)
					ps.Spawn(pixelfx.StarKind, px, py, vx, vy, life, C[6], size, 0, 0)
				}
			}
			if !tagGone && t >= critTagOut {
				tagGone = true
				for i := range 6 {
					px := -10 + R.Range(-16, 16)
					py := -14 + R.Range(-5, 5)
					vx := R.Range(-50, 50)
					vy := -R.Range(10, 60)
					life := R.Range(0.25, 0.4)
					col := pixelfx.Pick(R, []uint32{C[7], C[6]})
					size := 0.6
					if i == 0 {
						size = 1.6
					}
					spawnStar(px, py, vx, vy, life, col, size)
				}
			}
			if t > 0.3 && t < critFold-0.1 && R.Chance(0.07) {
				an := R.Range(0, tau)
				sp := R.Range(70, 150)
				ps.Spawn(pixelfx.Spark, math.Cos(an)*30*critSX, math.Sin(an)*30, math.Cos(an)*sp, math.Sin(an)*sp-30, math.Min(R.Range(0.12, 0.26), reach(sp, 14)), pixelfx.White, 1, 0, 0)
			}
			if t > 0.12 && t < critFold && R.Chance(0.16) {
				an := R.Range(0, tau)
				r := R.Range(14, 28)
				vx := R.Range(-10, 10)
				vy := -R.Range(15, 40)
				life := math.Min(R.Range(0.5, 0.9), end-t)
				size := R.Range(1, 2)
				ps.Spawn(pixelfx.Ember, math.Cos(an)*r*critSX, math.Sin(an)*r, vx, vy, life, C[5], size, 0, 0)
			}
			if !popped && t >= critPop+0.05 {
				popped = true
				lights.Add(0, 0, 44, C[6], 0.7, 0.25)
				for i := range 14 {
					an := (float64(i)/14)*tau + R.Range(-0.15, 0.15)
					sp := R.Range(60, 130)
					life := math.Min(R.Range(0.3, 0.42), end-t)
					col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					size := 0.6
					if i%3 == 0 {
						size = R.Range(1.3, 2.2)
					}
					ps.Spawn(pixelfx.StarKind, math.Cos(an)*10*critSX, math.Sin(an)*7, math.Cos(an)*sp*critSX, math.Sin(an)*sp-10, life, col, size, 0, 0)
				}
				for range 8 {
					an := R.Range(0, tau)
					sp := R.Range(120, 240)
					life := math.Min(math.Min(R.Range(0.15, 0.3), end-t), reach(sp, 40))
					ps.Spawn(pixelfx.Spark, math.Cos(an)*6, math.Sin(an)*4, math.Cos(an)*sp, math.Sin(an)*sp-30, life, pixelfx.White, 1, 0, 0)
				}
			}
			ps.Update(dt)
			bits.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			sx, sy := shake(t, 4, 0, 0.32, o.Seed(), 32)
			lx, ly := shake(t, 2, 0.09, 0.15, o.Seed()+3, 32)
			grow := 1.25
			if t >= 0.04 {
				grow = 1 + 0.1*math.Exp(-(t-0.04)*14)
			}
			fold := 1 - pixelfx.InBack(pixelfx.Seg(t, critFold-0.12, critFold), 2.4)
			heat := 1.0
			if t >= 0.035 {
				heat = pixelfx.Clamp(1 - (t-0.035)/0.12)
			}
			breathe := 1 + 0.04*math.Sin(t*16)
			if fold > 0.02 {
				boil := 0.0
				if t >= 0.2 {
					boil = math.Floor(t * 10)
				}
				drawStarburst(p, C, sx, sy, 31*grow*fold*breathe, -pi/2+0.25*t, tips, heat, boil, 1)
			}
			drawCritSpeedLines(p, C, t, lines)
			rk := t / 0.26
			rr := 30 + 22*pixelfx.Ease.OutCubic(rk)
			if rk > 0 && rk < 1 {
				col := C[6]
				if rk < 0.35 {
					col = pixelfx.White
				}
				p.EllipseFn(0, 0, rr*critSX, rr, func(d, _ float64) (uint32, float64) {
					if d > 1-1.6/rr {
						return col, 1.2 - rk
					}
					return col, 0
				})
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)

			s := sizeAt(t)
			sxN, syN := s, s
			flash := 0.0
			if t < 0.035 {
				flash = 1
			} else if t < 0.07 {
				flash = 0.5
			}
			pu := (t - critPop) / 0.05
			if pu >= 1 {
				k := (t - critPop - 0.05) / 0.22
				if k > 0 && k < 1 {
					r := 6 + 22*pixelfx.Ease.OutCubic(k)
					col := C[6]
					if k < 0.4 {
						col = pixelfx.White
					}
					p.EllipseFn(0, 0, r*critSX, r, func(d, _ float64) (uint32, float64) {
						if d > 1-1.5/(6+22*pixelfx.Ease.OutCubic(k)) {
							return col, 1.3 - k
						}
						return col, 0
					})
				}
			} else {
				sq := pixelfx.Seg(t, critPop-0.07, critPop)
				if sq > 0 && pu <= 0 {
					sxN = critNS * (1 + 0.14*sq)
					syN = critNS * (1 - 0.12*sq)
				}
				if pu > 0 {
					sxN = critNS * (1 + 0.25*pu)
					syN = sxN
					flash = 1
				}
				W, H := float64(num.w)*sxN, 7*syN
				x0 := sx - W/2 - 3
				y0 := sy + critNY - H/2 - 3*pixelfx.Smooth(0.15, critFold, t)
				var items []wordItem
				for _, L := range num.letters {
					if L.g != nil {
						items = append(items, wordItem{g: L.g, x: x0 + float64(L.x)*sxN, y: y0, sx: sxN, sy: syN, slant: 1, a: 1, flash: flash})
					}
				}
				sh := pixelfx.Seg(t, 0.36, 0.6)
				hasShine := sh > 0 && sh < 1
				shine := 0.0
				if hasShine {
					shine = x0 - 10 + (W+26)*pixelfx.Ease.InOutSine(sh)
				}
				drawWord(p, items, wordStyle{
					fill: []uint32{C[5], C[6], C[6], C[7]}, edge: pixelfx.White, under: C[4], outline: C[0], ow: 2, shadow: SHADOW,
					hasShine: hasShine, shine: shine, hasShineY: true, shineY: y0, glint: C[5],
				})
				if hasShine && shine > x0 && shine < x0+W {
					p.GlowLine(shine, y0, shine+0.6*H, y0+H, 8, C[6], 0.3)
				}
				lk := t - 0.06
				lo := pixelfx.Seg(t, critTagOut-0.05, critTagOut)
				if lk > 0 && t < critTagOut {
					ls := 2 * (1 + 0.15*lo)
					if lk < 0.035 {
						ls = 3
					} else if lk < 0.07 {
						ls = 1.8
					}
					LW, lh := float64(lab.w)*ls, 5*ls
					lxo, lyo := sx+lx-8-LW/2, y0-3-lh+ly-2*lo
					if lo > 0.5 {
						drawCritTag(p, pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White, lxo-4, lyo-3, LW+8, lh+5, 3)
					} else {
						drawCritTag(p, C[0], C[2], C[3], C[4], lxo-4, lyo-3, LW+8, lh+5, 3)
					}
					lfl := 0.0
					if lk < 0.035 {
						lfl = 1
					}
					var lit []wordItem
					for _, L := range lab.letters {
						if L.g != nil {
							lit = append(lit, wordItem{g: L.g, x: lxo + 1 + float64(L.x)*ls, y: lyo, sx: ls, sy: ls, slant: 0.5, a: 1, flash: lfl})
						}
					}
					drawWord(p, lit, wordStyle{
						fill: []uint32{C[6], C[7], C[7]}, edge: pixelfx.White, under: C[5], outline: 0, shadow: C[1],
					})
				}
				g := (t - 0.6) / 0.24
				if g > 0 && g < 1 {
					pixelfx.Sparkle(p, x0+W-2, y0+1, 6*math.Sin(g*pi), C[6], C[5], 0.6)
				}
			}
			hold := pixelfx.Smooth(0, 0.1, t) * (1 - pixelfx.Smooth(critFold-0.1, critPop+0.1, t))
			p.Glow(sx, sy, 56, C[4], 0.14*hold*(1+0.2*math.Sin(t*16)))
			if t < 0.12 {
				p.Glow(0, 0, 64, pixelfx.White, 0.8*(1-t/0.12))
			}
			lights.Draw(p)
		},
	}
}
