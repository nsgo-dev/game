// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/countdown.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Countdown slams 3, 2, 1 into a burning-fuse timer ring that throws sparks, then bursts into GO! with a shockwave and rays. It is registered as "ui/countdown".
var Countdown = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Countdown",
		Desc:     "3, 2, 1 slam into a timer ring that drains like a burning fuse, throwing sparks; then GO! bursts out as the ring blows into a shockwave with rays.",
		Tags:     []string{"ui", "countdown", "start", "timer", "text", "one-shot"},
		Size:     [2]int{152, 136},
		Origin:   [2]int{76, 68},
		Duration: cdDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a1030", "#142a66", "#1f4fae", "#2f86e6", "#5cc4ff", "#a8e8ff", "#e6fbff", "#ffffff")},
			{Name: "go", Default: pixelfx.Palette("#0b2410", "#16501c", "#22882a", "#3cc23a", "#7ee84c", "#c4ff8a", "#f0ffd6", "#ffffff")},
			{Name: "from", Default: pixelfx.Num(3)},
			{Name: "word", Default: pixelfx.Text("GO!")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newCountdown,
}

func init() { pixelfx.Register("ui/countdown", Countdown) }

var (
	cdDuration = 3.1
	cdStep     = 0.62
	cdR        = 30.0
	cdTop      = -pi / 2
)

func drawTimer(p *pixelfx.Painter, C []uint32, x, y, f float64, flash bool, t float64) float64 {
	p.RingFn(x, y, cdR+4, 8, func(v, _ float64) (uint32, float64) {
		if v < 0.13 || v > 0.87 {
			return C[0], 1
		}
		return C[1], 1
	})
	for j := range 40 {
		an := (float64(j)/40)*tau + t*0.6
		col := C[2]
		if j%5 == 0 {
			col = C[4]
		}
		p.Dot(x+math.Cos(an)*(cdR-6), y+math.Sin(an)*(cdR-6), col, 1)
	}
	head := cdTop + tau*(1-f)
	for i := range 12 {
		an := cdTop + (float64(i)*tau)/12
		lit := an >= head-1e-6 || f >= 0.999
		near := math.Abs(an-head) < 0.2 && f < 0.999
		ca, sa := math.Cos(an), math.Sin(an)
		col := C[2]
		if flash || near {
			col = pixelfx.White
		} else if lit {
			col = C[5]
		}
		p.Line(x+ca*(cdR+6), y+sa*(cdR+6), x+ca*(cdR+8), y+sa*(cdR+8), col, 1)
	}
	if f <= 0.002 {
		return head
	}
	p.ArcFn(x, y, cdR+2, 5, head, cdTop+tau, func(u, v float64) (uint32, float64) {
		d := u * tau * f
		if flash || d < 0.1 {
			return pixelfx.White, 1
		}
		if d < 0.32 {
			if v > 0.5 {
				return pixelfx.White, 1
			}
			return C[6], 1
		}
		if v > 0.75 {
			return C[6], 1
		}
		if v > 0.4 {
			return C[5], 1
		}
		if v > 0.15 {
			return C[4], 1
		}
		return C[3], 1
	})
	for an := head + 0.25; an < cdTop+tau; an += 0.55 {
		p.Glow(x+math.Cos(an)*(cdR-0.5), y+math.Sin(an)*(cdR-0.5), 9, C[4], 0.12)
	}
	hx, hy := x+math.Cos(head)*(cdR-0.5), y+math.Sin(head)*(cdR-0.5)
	p.Glow(hx, hy, 14, C[5], 0.5)
	if !flash {
		pixelfx.Sparkle(p, hx, hy, 2+1.5*(0.5+0.5*math.Sin(t*40)), C[6], C[5], 0.4)
	}
	return head
}

type cdRay struct {
	a, L float64
}

func newCountdown(o *pixelfx.Options) pixelfx.Instance {
	C, G := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("go"), 8)
	Rg := pixelfx.NewRng(o.Seed())
	n := int(pixelfx.ClampTo(pixelfx.Round(o.Num("from")), 1, 3))
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: Rg, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	gs := pixelfx.NewParticles(pixelfx.SimOpts{Rng: Rg, Sparks: []uint32{G[7], G[6], G[5], G[3]}})
	lights := &pixelfx.Lights{}
	GO := float64(n) * cdStep
	OUT := GO + 0.78
	end := cdDuration - 0.05
	digits := make([]wordLayout, n)
	for i := range digits {
		digits[i] = spell(strconv.Itoa(n-i), "big")
	}
	word := spell(o.Text("word"), "big")
	rays := make([]cdRay, 18)
	for i := range rays {
		a := (float64(i)/18)*tau + Rg.Range(-0.06, 0.06)
		var L float64
		if i%2 != 0 {
			L = Rg.Range(10, 16)
		} else {
			L = Rg.Range(22, 30)
		}
		rays[i] = cdRay{a: a, L: L}
	}
	sizeAt := func(u, base, big float64) float64 {
		return base + big*math.Exp(-u*26)*math.Cos(u*38)
	}
	reach := func(v, d float64) float64 {
		if v <= d*2.2 {
			return 9
		}
		return -math.Log(1-(d*2.2)/v) / 2.2
	}
	t, beat, went, popped := 0.0, -1, false, false

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			i := int(math.Floor(t / cdStep))
			if t < GO && i != beat {
				beat = i
				lights.Add(0, 0, 54, C[5], 0.6, 0.35)
				for j := range 14 {
					an := (float64(j)/14)*tau + Rg.Range(-0.1, 0.1)
					sp := Rg.Range(90, 200)
					ps.Spawn(pixelfx.Spark, math.Cos(an)*(cdR+2), math.Sin(an)*(cdR+2), math.Cos(an)*sp, math.Sin(an)*sp, math.Min(Rg.Range(0.12, 0.3), reach(sp, 26)), pixelfx.White, 1, 0, 0)
				}
			}
			if t < GO {
				k := math.Mod(t, cdStep) / cdStep
				f := 1 - pixelfx.Seg(k, 0.06, 0.97)
				head := cdTop + tau*(1-f)
				if f > 0.01 && f < 0.99 && Rg.Chance(0.7) {
					hx, hy := math.Cos(head)*cdR, math.Sin(head)*cdR
					tx, ty := -math.Sin(head), math.Cos(head)
					out := Rg.Range(-0.4, 1)
					sp := Rg.Range(60, 150)
					ps.Spawn(pixelfx.Spark, hx, hy, (-tx*0.8+math.Cos(head)*out)*sp, (-ty*0.8+math.Sin(head)*out)*sp-20, Rg.Range(0.12, 0.28), pixelfx.White, 1, 0, 0)
					if Rg.Chance(0.15) {
						vx := math.Cos(head) * Rg.Range(5, 20)
						vy := math.Sin(head) * Rg.Range(5, 20)
						ps.Spawn(pixelfx.Ember, hx, hy, vx, vy, Rg.Range(0.4, 0.8), C[5], 1, 0, 0)
					}
				}
			}
			if !went && t >= GO {
				went = true
				lights.Add(0, 0, 70, pixelfx.White, 0.9, 0.18)
				lights.Add(0, 0, 64, G[5], 0.7, 0.8)
				for range 36 {
					an := Rg.Range(0, tau)
					sp := Rg.Range(120, 300)
					gs.Spawn(pixelfx.Spark, math.Cos(an)*cdR, math.Sin(an)*cdR, math.Cos(an)*sp, math.Sin(an)*sp-40, math.Min(Rg.Range(0.18, 0.36), reach(sp, 24)), pixelfx.White, 1, 0, 0)
				}
				for range 18 {
					an := Rg.Range(0, tau)
					sp := Rg.Range(30, 80)
					r := Rg.Range(10, 28)
					life := Rg.Range(0.5, 1)
					col := pixelfx.Pick(Rg, []uint32{G[7], G[6], G[5]})
					size := 0.6
					if Rg.Chance(0.4) {
						size = Rg.Range(1.5, 2.5)
					}
					gs.Spawn(pixelfx.StarKind, math.Cos(an)*r, math.Sin(an)*r, math.Cos(an)*sp, math.Sin(an)*sp-10, life, col, size, 0, 0)
				}
			}
			if t > GO+0.2 && t < OUT-0.1 && Rg.Chance(0.12) {
				an := Rg.Range(0, tau)
				r := Rg.Range(30, 46)
				vy := -Rg.Range(4, 12)
				life := Rg.Range(0.3, 0.6)
				col := pixelfx.Pick(Rg, []uint32{G[7], G[6]})
				size := 0.6
				if Rg.Chance(0.3) {
					size = 1.6
				}
				gs.Spawn(pixelfx.StarKind, math.Cos(an)*r, math.Sin(an)*r*0.8, 0, vy, life, col, size, 0, 0)
			}
			if !popped && t >= OUT+0.05 {
				popped = true
				lights.Add(0, 0, 40, G[6], 0.6, 0.25)
				for j := range 14 {
					an := (float64(j)/14)*tau + Rg.Range(-0.15, 0.15)
					sp := Rg.Range(60, 130)
					life := math.Min(Rg.Range(0.3, 0.45), end-t)
					col := pixelfx.Pick(Rg, []uint32{G[7], G[6], G[5]})
					size := 0.6
					if j%3 == 0 {
						size = Rg.Range(1.3, 2)
					}
					gs.Spawn(pixelfx.StarKind, math.Cos(an)*12, math.Sin(an)*8, math.Cos(an)*sp, math.Sin(an)*sp*0.8-10, life, col, size, 0, 0)
				}
			}
			ps.Update(dt)
			gs.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < GO {
				i := int(math.Floor(t / cdStep))
				u := t - float64(i)*cdStep
				k := u / cdStep
				sx, sy := shake(t, 3, float64(i)*cdStep, 0.22, o.Seed()+float64(i), 32)
				f := 1 - pixelfx.Seg(k, 0.06, 0.97)
				flash := u < 0.07
				rk := u / 0.3
				if rk < 1 {
					col := C[5]
					if rk < 0.3 {
						col = pixelfx.White
					}
					p.Ring(sx, sy, cdR+6+22*pixelfx.Ease.OutCubic(rk), 1.5*(1-rk)+1, col, 1.3*(1-rk))
				}
				drawTimer(p, C, sx, sy, f, flash, t)
				if u < 0.035 {
					p.Circle(sx, sy, cdR-3, pixelfx.White, 1)
				} else if u < 0.07 {
					p.Ring(sx, sy, cdR-3, 3, C[6], 1)
				}
				s := sizeAt(u, 5, 2)
				d := digits[i]
				W, H := float64(d.w)*s, 7*s
				x0, y0 := sx-W/2, sy-H/2
				fl := 0.0
				if u < 0.035 {
					fl = 1
				} else if u < 0.07 {
					fl = 0.4
				}
				var items []wordItem
				for _, L := range d.letters {
					if L.g != nil {
						items = append(items, wordItem{g: L.g, x: x0 + float64(L.x)*s, y: y0, sx: s, sy: s, a: 1, flash: fl})
					}
				}
				drawWord(p, items, wordStyle{
					fill: []uint32{C[4], C[5], C[6], C[7]}, edge: pixelfx.White, under: C[3], outline: C[0], ow: 2, shadow: pixelfx.Scale(C[0], 0.5),
				})
				extra := 0.0
				if u < 0.12 {
					extra = 0.6 * (1 - u/0.12)
				}
				p.Glow(sx, sy, 30, C[4], 0.22+extra)
				p.Glow(sx, sy, cdR+8, C[3], 0.2*(1-k)+0.08)
			} else {
				u := t - GO
				sx, sy := shake(t, 4, GO, 0.3, o.Seed()+9, 32)
				bk := u / 0.4
				if bk < 1 {
					r := cdR + 4 + 30*pixelfx.Ease.OutCubic(bk)
					w := math.Max(1, 7*(1-bk))
					p.RingFn(0, 0, r, w, func(v, _ float64) (uint32, float64) {
						col := G[6]
						if bk < 0.2 || v > 0.6 {
							col = pixelfx.White
						}
						return col, 1.4 * (1 - bk)
					})
					p.GlowRing(0, 0, r-w/2, 8, G[5], 0.5*(1-bk))
				}
				ik := (u - 0.05) / 0.3
				if ik > 0 && ik < 1 {
					p.Ring(0, 0, cdR+22*pixelfx.Ease.OutCubic(ik), 1, G[5], 1.2*(1-ik))
				}
				rk := pixelfx.Ease.OutExpo(pixelfx.Clamp(u/0.18)) * (1 - pixelfx.Ease.InQuad(pixelfx.Seg(t, OUT-0.25, OUT+0.05)))
				for _, ry := range rays {
					if rk <= 0.02 {
						break
					}
					an := ry.a + u*0.35
					ca, sa := math.Cos(an), math.Sin(an)
					r0 := 1/math.Sqrt((ca/40)*(ca/40)+(sa/23)*(sa/23)) + 3*rk
					r1 := r0 + ry.L*0.7*rk
					p.LineFn(ca*r0, sa*r0, ca*r1, sa*r1, func(q float64) (uint32, float64) {
						col := G[5]
						if q < 0.3 {
							col = pixelfx.White
						} else if q < 0.65 {
							col = G[6]
						}
						a := (1.3 * (1 - q)) / 0.4
						if q < 0.6 {
							a = 1.3
						}
						return col, a
					})
				}
				pu := (t - OUT) / 0.06
				if pu < 1 {
					s := sizeAt(u, 5, 3.5)
					fl := 0.0
					if u < 0.035 {
						fl = 1
					} else if u < 0.07 {
						fl = 0.4
					}
					throb := math.Max(0, math.Sin((u-0.25)*9)) * pixelfx.Smooth(0.25, 0.35, u)
					if pu > 0 {
						s = 5 * (1 + 0.25*pu)
						fl = 1
					}
					W, H := float64(word.w)*s, 7*s
					x0, y0 := sx-W/2, sy-H/2
					var items []wordItem
					for _, L := range word.letters {
						if L.g != nil {
							items = append(items, wordItem{g: L.g, x: x0 + float64(L.x)*s, y: y0, sx: s, sy: s, slant: 1, a: 1, flash: fl})
						}
					}
					sh := pixelfx.Seg(u, 0.32, 0.56)
					hasShine := sh > 0 && sh < 1
					shine := 0.0
					if hasShine {
						shine = x0 - 8 + (W+22)*pixelfx.Ease.InOutSine(sh)
					}
					drawWord(p, items, wordStyle{
						fill: []uint32{G[4], G[5], G[6], G[7]}, edge: pixelfx.White, under: G[3], outline: G[0], ow: 2, shadow: pixelfx.Scale(G[0], 0.5),
						hasShine: hasShine, shine: shine, hasShineY: true, shineY: y0, glint: G[6],
					})
					extra := 0.0
					if u < 0.12 {
						extra = 0.7 * (1 - u/0.12)
					}
					p.Glow(sx, sy, 46, G[5], 0.25+0.15*throb+extra)
				} else {
					k := (t - OUT - 0.06) / 0.22
					if k > 0 && k < 1 {
						col := G[6]
						if k < 0.4 {
							col = pixelfx.White
						}
						p.Ring(0, 0, 8+26*pixelfx.Ease.OutCubic(k), 1, col, 1.3*(1-k))
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			gs.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
