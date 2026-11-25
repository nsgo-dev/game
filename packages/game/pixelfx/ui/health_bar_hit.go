// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/health-bar-hit.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// HealthBarHit shows a segmented health bar taking a hit: it jolts and dips, the lost chunk flashes white and sheds chips, holds, then drains away behind a sparking edge. It is registered as "ui/health-bar-hit".
var HealthBarHit = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Health bar hit",
		Desc:     "A segmented health bar takes a hit: it jolts and dips, the lost chunk flashes white and sheds chips, holds, then drains away behind a sparking edge.",
		Tags:     []string{"ui", "health", "hud", "damage", "bar", "one-shot"},
		Size:     [2]int{160, 64},
		Origin:   [2]int{80, 30},
		Duration: hbDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0508", "#4a0b12", "#8c1420", "#d42a2e", "#ff5a4a", "#ff9a86", "#ffd8cc", "#ffffff")},
			{Name: "from", Default: pixelfx.Num(0.8)},
			{Name: "to", Default: pixelfx.Num(0.45)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newHealthBarHit,
}

func init() { pixelfx.Register("ui/health-bar-hit", HealthBarHit) }

var (
	hbDuration = 2.0
	hbHit      = 0.2
	hbDrain    = 0.62
	hbDrained  = 1.05
	hbFold     = 1.62
	hbBX       = -48.0
	hbBW       = 104.0
	hbBY       = -4.0
	hbBH       = 8.0
	hbCells    = 10
	hbInk      = panelCols{
		outline: pixelfx.Hex("#0d0a14"),
		rim:     pixelfx.Hex("#4a4060"),
		top:     pixelfx.Hex("#2a2338"),
		body:    pixelfx.Hex("#1e1929"),
		low:     pixelfx.Hex("#141019"),
	}
	hbPaleMix = pixelfx.Hex("#ffe9a8")
	hbTrackBg = pixelfx.Hex("#0d0a14")
	hbEmptyHi = pixelfx.Hex("#2a2338")
	hbEmptyLo = pixelfx.Hex("#191424")
)

func newHealthBarHit(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	from := pixelfx.Clamp(o.Num("from"))
	to := pixelfx.Clamp(math.Min(o.Num("to"), from))
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	bits := pixelfx.NewBits(nil)
	lights := &pixelfx.Lights{}
	PALE := pixelfx.Mix(C[6], hbPaleMix, 0.5)
	pal := heartPal(C)
	palWhite := map[rune]uint32{'k': 0, 'w': pixelfx.White, 'h': pixelfx.White, 'o': pixelfx.White, 'm': pixelfx.White, 'd': C[6]}
	t, hit := 0.0, false
	ghostAt := func(t float64) float64 {
		if t < hbDrain {
			return from
		}
		return from - (from-to)*pixelfx.Ease.InOutQuad(pixelfx.Seg(t, hbDrain, hbDrained))
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit && t >= hbHit {
				hit = true
				lights.Add(hbBX+hbBW*(to+from)/2, hbBY+4, 50, C[5], 0.7, 0.3)
				lights.Add(hbBX+hbBW*(to+from)/2, hbBY+4, 24, pixelfx.White, 0.6, 0.08)
				for range 12 {
					x := hbBX + hbBW*R.Range(to, from)
					an := -pi/2 + R.Range(-1.2, 1.2)
					sp := R.Range(50, 130)
					py := hbBY + R.Range(1, 7)
					life := R.Range(0.35, 0.6)
					a := R.Range(0, tau)
					va := R.Range(-20, 20)
					s := R.Range(1.5, 3)
					c := pixelfx.Pick(R, []uint32{C[7], C[6], C[5], C[4]})
					bits.Add(pixelfx.Bit{
						Kind: pixelfx.Shard, X: x, Y: py,
						VX: math.Cos(an) * sp, VY: math.Sin(an) * sp,
						G: 0.45, Drag: 1.2, Life: life, A: a, VA: va, S: s, C: c,
					})
				}
				for range 10 {
					an := R.Range(0, tau)
					sp := R.Range(60, 160)
					px := hbBX + hbBW*R.Range(to, from)
					life := R.Range(0.12, 0.26)
					ps.Spawn(pixelfx.Spark, px, hbBY+4, math.Cos(an)*sp, math.Sin(an)*sp*0.6-30, life, pixelfx.White, 1, 0, 0)
				}
			}
			if t > hbDrain && t < hbDrained && R.Chance(0.5) {
				x := hbBX + hbBW*ghostAt(t)
				py := hbBY + R.Range(1, 7)
				vx := R.Range(-20, 60)
				vy := -R.Range(20, 70)
				life := R.Range(0.1, 0.22)
				ps.Spawn(pixelfx.Spark, x, py, vx, vy, life, pixelfx.White, 1, 0, 0)
			}
			bits.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			jx, jy := shake(t, 3, hbHit, 0.28, o.Seed(), 32)
			dip := 0.0
			if t > hbHit {
				dip = pixelfx.Round(3 * math.Exp(-(t-hbHit)*10) * math.Cos((t-hbHit)*24))
			}
			open := pixelfx.OutBack(pixelfx.Seg(t, 0, 0.14), 2) * (1 - pixelfx.InBack(pixelfx.Seg(t, hbFold, hbFold+0.18), 2))
			if open > 0.04 {
				ox, oy := jx, jy+dip
				h := 18 * open
				y0 := -9 + (18-h)/2
				col := hbInk
				if t >= hbHit && t < hbHit+0.07 {
					col.outline = C[4]
					col.rim = C[6]
				}
				panel(p, hbBX-22+ox, y0+oy, hbBW+26, h, col, panelOpts{r: math.Min(5, h/2), a: 1})
				if open > 0.75 {
					val, ghost := from, from
					if t >= hbHit {
						val = to
						ghost = ghostAt(t)
					}
					white := t >= hbHit && t < hbHit+0.07
					p.Rect(hbBX+ox, hbBY+oy, hbBW, hbBH, hbTrackBg, 1)
					p.Shade(hbBX+ox, hbBY+oy, hbBX+hbBW+ox, hbBY+hbBH+oy, func(x, y float64, _ *pixelfx.Out) uint32 {
						u := (x - hbBX - ox) / hbBW
						v := y - hbBY - oy
						if u < val {
							if v < 1 {
								return C[6]
							}
							if v < 2 {
								return C[5]
							}
							if v < 5 {
								return C[4]
							}
							if v < 7 {
								return C[3]
							}
							return C[2]
						}
						if u < ghost {
							if white || v < 1 {
								return pixelfx.White
							}
							if v < 6 {
								return PALE
							}
							return C[5]
						}
						if v < 1 {
							return hbEmptyHi
						}
						return hbEmptyLo
					})
					for i := 1; i < hbCells; i++ {
						p.Rect(hbBX+ox+pixelfx.Round((hbBW*float64(i))/float64(hbCells)), hbBY+oy, 1, hbBH, hbTrackBg, 1)
					}
					if t > hbHit {
						p.Rect(hbBX+ox+pixelfx.Round(hbBW*val)-1, hbBY+oy, 1, hbBH, C[7], 1)
					}
					if t > hbDrain && t < hbDrained {
						gx := hbBX + ox + hbBW*ghost
						p.Rect(gx-1, hbBY+oy-1, 2, hbBH+2, pixelfx.White, 1)
						p.Glow(gx, hbBY+oy+4, 10, C[5], 0.6)
					}
					p.Glow(hbBX+ox+hbBW*val*0.5, hbBY+oy+4, hbBW*0.4, C[3], 0.12)
					hk := t - hbHit
					hop := 0.0
					if hk > 0 {
						hop = pixelfx.Round(-3 * math.Exp(-hk*12) * math.Abs(math.Cos(hk*22)))
					}
					p.Sprite(heartRows, pal, hbBX-17+ox, oy-6+hop, 1, pixelfx.SpriteOpts{})
					if hk > 0 && hk < 0.07 {
						p.Sprite(heartRows, palWhite, hbBX-17+ox, oy-6+hop, 1, pixelfx.SpriteOpts{})
					}
					extra := 0.0
					if hk > 0 && hk < 0.2 {
						extra = 0.4
					}
					p.Glow(hbBX-11+ox, oy, 10, C[4], 0.25+extra)
					if t < hbHit && math.Mod(t*2.5, 1) < 0.3 {
						pixelfx.Sparkle(p, hbBX-14+ox, oy-3, 2, C[7], C[6], 0.3)
					}
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
