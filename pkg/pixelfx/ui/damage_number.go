// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/damage-number.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// DamageNumber punches a red "-27" out of a hit spark, arcs up, lands with a squash and a hop, then fades as it lifts away. It is registered as "ui/damage-number".
var DamageNumber = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Damage number",
		Desc:     "A red -27 punches out of a hit spark, white for a frame, arcs up, lands with a squash and a little hop, then fades as it lifts away.",
		Tags:     []string{"ui", "damage", "number", "combat", "text", "hit", "one-shot"},
		Size:     [2]int{76, 68},
		Origin:   [2]int{30, 52},
		Duration: dmgDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0508", "#4a0b12", "#8c1420", "#d42a2e", "#ff5a4a", "#ff9a86", "#ffd8cc", "#ffffff")},
			{Name: "text", Default: pixelfx.Text("-27")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newDamageNumber,
}

func init() { pixelfx.Register("ui/damage-number", DamageNumber) }

var (
	dmgDuration = 1.05
	dmgLand     = -8.0
	dmgFade     = 0.84
)

func newDamageNumber(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	word := spell(o.Text("text"), "big")
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[4], C[2]}})
	lights := &pixelfx.Lights{}
	nx, ny, nvx, nvy, nsq, nhops := 0.0, dmgLand, 34.0, -175.0, 0.0, 0
	spikes := make([][2]float64, 9)
	for i := range spikes {
		spikes[i] = [2]float64{(float64(i)/9)*tau + R.Range(-0.2, 0.2), R.Range(8, 14)}
	}
	t, hit := 0.0, false

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit {
				hit = true
				lights.Add(0, -4, 30, C[5], 0.7, 0.25)
				lights.Add(0, -4, 14, pixelfx.White, 0.6, 0.08)
				for i := range 9 {
					an := R.Range(-pi*0.95, -0.05)
					if i%2 != 0 {
						an += pi * 0.08
					}
					sp := R.Range(90, 200)
					ps.Spawn(pixelfx.Spark, 0, -4, math.Cos(an)*sp, math.Sin(an)*sp, R.Range(0.1, 0.25), pixelfx.White, 1, 0, 0)
				}
			}
			if nhops < 2 {
				nvy += 900 * dt
				nvx -= nvx * 2 * dt
				nx += nvx * dt
				ny += nvy * dt
				if ny >= dmgLand && nvy > 0 {
					ny = dmgLand
					nsq = 1
					nhops++
					if nhops < 2 {
						nvy = -nvy * 0.36
					} else {
						nvy = 0
					}
					if nhops == 1 {
						for _, s := range [2]float64{-1, 1} {
							py := dmgLand + R.Range(-2, 1)
							vx := s * R.Range(15, 35)
							vy := -R.Range(5, 20)
							life := R.Range(0.2, 0.35)
							size := R.Range(0.5, 1.2)
							ps.Spawn(pixelfx.StarKind, nx+s*14, py, vx, vy, life, C[6], size, 0, 0)
						}
					}
				}
			}
			nsq = math.Max(0, nsq-dt/0.09)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < 0.07 {
				big := t < 0.035
				for i, spk := range spikes {
					an, L := spk[0], spk[1]
					l := L * 1.3
					if big {
						l = L
					}
					ca, sa := math.Cos(an), math.Sin(an)
					if big {
						p.Line(-sa, -4+ca, ca*l*0.7-sa, -4+sa*l*0.7+ca, C[4], 1)
						p.Line(sa, -4-ca, ca*l*0.7+sa, -4+sa*l*0.7-ca, C[4], 1)
					}
					d0 := l * 0.45
					col := C[5]
					if big {
						d0 = 0
						col = pixelfx.White
					} else if i%2 != 0 {
						col = C[6]
					}
					p.Line(ca*d0, -4+sa*d0, ca*l, -4+sa*l, col, 1)
				}
				if big {
					p.Circle(0, -4, 3.2, pixelfx.White, 1)
				}
			} else if t < 0.16 {
				pixelfx.Sparkle(p, 0, -4, 5*(1-(t-0.07)/0.09), pixelfx.White, C[5], 0.6)
			}
			rk := t / 0.22
			if rk < 1 {
				col := C[5]
				if rk < 0.4 {
					col = pixelfx.White
				}
				p.Ring(0, -4, 3+10*pixelfx.Ease.OutCubic(rk), math.Max(1, 2*(1-rk)), col, 1.2-rk)
			}
			ps.Draw(p, pixelfx.LayerBoth)

			out := pixelfx.Seg(t, dmgFade, dmgDuration-0.04)
			if out < 1 {
				s := 2 + 1.4*math.Exp(-t*22)*math.Cos(t*30)
				sx, sy := s, s
				if nsq > 0 {
					sx = s * (1 + 0.18*nsq)
					sy = s * (1 - 0.2*nsq)
				} else if nhops < 2 && math.Abs(nvy) > 120 {
					sx = s * 0.92
					sy = s * 1.1
				}
				W, H := float64(word.w)*sx, 7*sy
				x0, y0 := nx-W/2, ny-H-6*pixelfx.Ease.InQuad(out)
				flash := 0.0
				if t < 0.035 {
					flash = 1
				} else if t < 0.07 {
					flash = 0.4
				}
				var items []wordItem
				for _, L := range word.letters {
					if L.g != nil {
						items = append(items, wordItem{g: L.g, x: x0 + float64(L.x)*sx, y: y0, sx: sx, sy: sy, flash: flash, a: 1 - out})
					}
				}
				drawWord(p, items, wordStyle{
					fill: []uint32{C[3], C[3], C[4], C[5]}, edge: C[6], under: C[2], outline: C[0], shadow: pixelfx.Scale(C[0], 0.5),
				})
				extra := 0.0
				if t < 0.1 {
					extra = 0.4 * (1 - t/0.1)
				}
				p.Glow(nx, y0+H/2, 18, C[4], 0.3*(1-out)+extra)
			}
			lights.Draw(p)
		},
	}
}
