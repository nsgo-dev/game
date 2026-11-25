// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/combo-counter.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// ComboCounter rockets "x5 COMBO" in on streaming speed lines, slams to a stop with a flash, ring and sparks, pulses while a timer bar drains, then whooshes out. It is registered as "ui/combo-counter".
var ComboCounter = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Combo counter",
		Desc:     "x5 COMBO rockets in on streaming speed lines, smearing, and slams to a stop: a white flash, a ring, sparks; it pulses while a timer bar drains, then whooshes out.",
		Tags:     []string{"ui", "combo", "counter", "combat", "text", "one-shot"},
		Size:     [2]int{208, 100},
		Origin:   [2]int{104, 50},
		Duration: comboDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0516", "#3d0a33", "#701560", "#b0228f", "#f53cd1", "#ff86e6", "#ffd2f6", "#ffffff")},
			{Name: "count", Default: pixelfx.Num(5)},
			{Name: "label", Default: pixelfx.Text("COMBO")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newComboCounter,
}

func init() { pixelfx.Register("ui/combo-counter", ComboCounter) }

var (
	comboDuration = 1.45
	comboHit      = 0.1
	comboBeat     = 0.5
	comboOut      = 1.1
)

func comboSpeedLines(p *pixelfx.Painter, C []uint32, t, k, dir float64) {
	if k <= 0.02 {
		return
	}
	f := math.Floor(t * 30)
	for i := range 18 {
		fi := float64(i)
		if pixelfx.Rand(f, fi, 3) > k {
			continue
		}
		y := pixelfx.Round((pixelfx.Rand(f, fi, 1) - 0.5) * 60)
		L := 16 + pixelfx.Rand(f, fi, 2)*50
		x := (pixelfx.Rand(f, fi, 4) - 0.5) * 190
		x0, x1 := math.Max(-100, x-L/2), math.Min(100, x+L/2)
		p.LineFn(x0, y, x1, y, func(u float64) (uint32, float64) {
			q := 1 - u
			if dir > 0 {
				q = u
			}
			c := C[4]
			if q > 0.75 {
				c = pixelfx.White
			} else if q > 0.35 {
				c = C[6]
			}
			a := 1.0
			if q < 0.25 {
				a = 0.6
			}
			return c, a
		})
	}
}

func newComboCounter(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	SHADOW := pixelfx.Scale(C[0], 0.5)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	lights := &pixelfx.Lights{}
	big := spell("x"+strconv.Itoa(int(pixelfx.Round(o.Num("count")))), "big")
	lab := spell(o.Text("label"), "big")
	const BS, LS, BASE = 4.0, 2.0, 12.0
	W := float64(big.w)*BS + 4 + float64(lab.w)*LS
	X0 := -W / 2
	spikes := make([][2]float64, 10)
	for i := range spikes {
		spikes[i] = [2]float64{(float64(i)/10)*tau + R.Range(-0.15, 0.15), R.Range(14, 24)}
	}
	t, hit, out := 0.0, false, false
	xAt := func(t float64) float64 {
		t = math.Max(0, t)
		if t < comboHit {
			return -32 * math.Pow(1-t/comboHit, 2)
		}
		if t < comboOut {
			return 7 * math.Exp(-(t-comboHit)*18) * math.Sin((t-comboHit)*34)
		}
		u := pixelfx.Seg(t, comboOut, comboOut+0.2)
		if u < 0.3 {
			return -4 * pixelfx.Ease.OutQuad(u/0.3)
		}
		return -4 + 24*pixelfx.Ease.InQuad((u-0.3)/0.7)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit && t >= comboHit {
				hit = true
				lights.Add(0, 0, 70, C[5], 0.7, 0.4)
				lights.Add(-20, 0, 34, pixelfx.White, 0.8, 0.1)
				for range 26 {
					an := R.Range(-1.2, 1.2)
					if R.Chance(0.3) {
						an += pi
					}
					sp := R.Range(120, 300)
					px := R.Range(-20, 20)
					py := R.Range(-8, 8)
					life := R.Range(0.12, 0.3)
					ps.Spawn(pixelfx.Spark, px, py, math.Cos(an)*sp, math.Sin(an)*sp*0.6-20, life, pixelfx.White, 1, 0, 0)
				}
			}
			if t > comboBeat && t < comboOut && R.Chance(0.1) {
				x := X0 + R.Range(0, float64(big.w)*BS)
				y := BASE - R.Range(0, 30)
				vx := R.Range(-10, 10)
				vy := -R.Range(5, 20)
				life := R.Range(0.25, 0.45)
				col := pixelfx.Pick(R, []uint32{C[7], C[6]})
				size := 0.6
				if R.Chance(0.3) {
					size = 1.6
				}
				ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
			}
			if !out && t >= comboOut+0.06 {
				out = true
				for range 10 {
					x := R.Range(-50, 30)
					y := R.Range(-14, 10)
					vx := R.Range(220, 320)
					vy := R.Range(-10, 10)
					life := R.Range(0.06, 0.14)
					col := pixelfx.Pick(R, []uint32{C[7], C[6]})
					ps.Spawn(pixelfx.Mote, x, y, vx, vy, life, col, 1, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			sx, sy := shake(t, 3, comboHit, 0.25, o.Seed(), 32)
			slK := 1.0
			if t >= comboHit && t < comboOut {
				slK = math.Max(0, 1-(t-comboHit)/0.35) * 0.9
			} else if t >= comboOut {
				slK = pixelfx.Seg(t, comboOut+0.04, comboOut+0.12) * (1 - pixelfx.Seg(t, comboOut+0.18, comboOut+0.3))
			}
			dir := 1.0
			if t >= comboOut {
				dir = -1
			}
			comboSpeedLines(p, C, t, slK, dir)

			hk := t - comboHit
			if hk >= 0 && hk < 0.07 {
				cx := X0 + (float64(big.w)*BS)/2
				k := 1.3
				if hk < 0.035 {
					k = 1
				}
				for _, spk := range spikes {
					an, L := spk[0], spk[1]
					ca, sa := math.Cos(an)*1.4, math.Sin(an)
					if k == 1 {
						p.BeamFn(cx, 0, cx+ca*L, sa*L, 3, func(u, _ float64) (uint32, float64) {
							if u < 0.5 {
								return pixelfx.White, 1
							}
							return C[6], 1
						})
					} else {
						p.Line(cx+ca*L*0.5, sa*L*0.5, cx+ca*L*k, sa*L*k, C[5], 1)
					}
				}
			}
			rk := (t - comboHit) / 0.32
			if rk > 0 && rk < 1 {
				r := 20 + 34*pixelfx.Ease.OutCubic(rk)
				col := C[5]
				if rk < 0.3 {
					col = pixelfx.White
				}
				p.EllipseFn(sx, sy, r*1.5, r*0.75, func(d, _ float64) (uint32, float64) {
					if d > 1-1.5/(r*0.75) {
						return col, 1.25 - rk
					}
					return col, 0
				})
			}
			ps.Draw(p, pixelfx.LayerBoth)

			x := xAt(t)
			moving := t < comboHit || t > comboOut+0.06
			stretch := 1.0
			if moving {
				if t < comboHit {
					stretch = 1.22
				} else {
					stretch = 1.12
				}
			}
			dp := (t - comboBeat) / 0.05
			pulse := math.Exp(-(dp * dp))
			flash := 0.0
			if (t >= comboHit && t < comboHit+0.07) || pulse > 0.6 {
				flash = 1
			}
			a := 1.0
			if t > comboOut {
				a = 1 - pixelfx.Seg(t, comboOut+0.08, comboOut+0.2)
			} else if t < 0.035 {
				a = 0.5
			}
			layout := func(dx float64) []wordItem {
				bs := BS * (1 + 0.12*pulse)
				var items []wordItem
				bx := X0 + dx - (float64(big.w)*BS*(stretch-1))/2
				by := BASE - 7*bs
				for _, L := range big.letters {
					if L.g != nil {
						items = append(items, wordItem{g: L.g, x: bx + float64(L.x)*BS*stretch, y: by, sx: BS * stretch, sy: bs, slant: 1, flash: flash, a: a})
					}
				}
				lx := X0 + dx + float64(big.w)*BS*stretch + 4
				for _, L := range lab.letters {
					if L.g != nil {
						items = append(items, wordItem{g: L.g, x: lx + float64(L.x)*LS*stretch, y: BASE - 7*LS, sx: LS * stretch, sy: LS, slant: 0.5, flash: flash, a: a})
					}
				}
				return items
			}
			if moving && a > 0 {
				for _, ghost := range [2]struct {
					lag float64
					c   uint32
				}{{0.03, C[2]}, {0.015, C[3]}} {
					dtLag := -ghost.lag
					if t < comboHit {
						dtLag = ghost.lag
					}
					items := layout(xAt(t-dtLag) + sx)
					for i := range items {
						items[i].flash = 0
					}
					drawWord(p, items, wordStyle{fill: []uint32{ghost.c}, edge: ghost.c, under: ghost.c})
				}
			}
			if a > 0 {
				sh := pixelfx.Seg(t, 0.62, 0.86)
				items := layout(x + sx)
				for i := range items {
					items[i].y += sy
				}
				drawWord(p, items, wordStyle{
					fill: []uint32{C[4], C[5], C[6], C[7]}, edge: pixelfx.White, under: C[3], outline: C[0], ow: 2, shadow: SHADOW,
					hasShine: sh > 0 && sh < 1, shine: X0 - 10 + (W+24)*sh, hasShineY: true, shineY: BASE - 28, glint: C[6],
				})
				bk := pixelfx.Seg(t, comboHit+0.1, comboHit+0.2)
				if t > comboOut {
					bk = 0
				}
				if bk > 0 {
					bw := W * bk
					left := 1 - pixelfx.Seg(t, comboHit+0.2, comboOut)
					bx, by := X0+x+sx, BASE+5+sy
					p.Rect(bx-1, by-1, bw+2, 4, C[0], 1)
					p.Rect(bx, by, bw, 2, C[1], 1)
					if left > 0 {
						p.Rect(bx, by, bw*left, 2, C[5], 1)
						p.Rect(bx, by, bw*left, 1, C[6], 1)
						p.Px(bx+bw*left-1, by, pixelfx.White)
					}
				}
			}
			hitGlow := 0.0
			if t >= comboHit && t < comboHit+0.15 {
				hitGlow = 0.5 * (1 - (t-comboHit)/0.15)
			}
			p.Glow(sx-20, 0, 60, C[4], 0.2*a+hitGlow+0.25*pulse)
			g := (t - 0.86) / 0.22
			if g > 0 && g < 1 {
				pixelfx.Sparkle(p, X0+W-2, BASE-14, 6*math.Sin(g*pi), C[7], C[5], 0.5)
			}
			lights.Draw(p)
		},
	}
}
