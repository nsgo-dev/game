// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/achievement.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Achievement is a toast that slides in on speed lines and settles while a gold medal pops onto it in a burst of sparkles, a shine sweeps the whole toast, then it slides away. It is registered as "ui/achievement".
var Achievement = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Achievement",
		Desc:     "A toast slides in on speed lines and settles; a gold medal pops onto it in a burst of sparkles, a shine sweeps the whole toast, then it slides away.",
		Tags:     []string{"ui", "achievement", "toast", "medal", "reward", "text", "one-shot"},
		Size:     [2]int{220, 72},
		Origin:   [2]int{110, 36},
		Duration: achDuration,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1405", "#5c3008", "#9a5a0e", "#d48e18", "#f5b92c", "#ffde5e", "#fff4bc", "#ffffff")},
			{Name: "ribbon", Default: pixelfx.Palette("#0c1430", "#1f3a7a", "#2f66c4", "#5aa0f0", "#a6d4ff")},
			{Name: "title", Default: pixelfx.Text("ACHIEVEMENT")},
			{Name: "text", Default: pixelfx.Text("UNLOCKED")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newAchievement,
}

func init() { pixelfx.Register("ui/achievement", Achievement) }

var (
	achDuration = 2.4
	achPW       = 130.0
	achPH       = 34.0
	achPX       = -achPW / 2
	achPY       = -achPH / 2
	achIn       = 0.2
	achMedal    = 0.26
	achShine    = 0.95
	achOut      = 1.92
	achMX       = achPX + 17
	achMY       = 2.0
	achInk      = panelCols{
		outline: pixelfx.Hex("#0d0a14"),
		rim:     pixelfx.Hex("#5a4f74"),
		top:     pixelfx.Hex("#2a2338"),
		body:    pixelfx.Hex("#211b2d"),
		low:     pixelfx.Hex("#15111c"),
	}
	achSubFill0 = pixelfx.Hex("#9a94b0")
	achSubFill1 = pixelfx.Hex("#d8d4e6")
	achSubUnder = pixelfx.Hex("#6e6886")
)

func drawMedal(p *pixelfx.Painter, G, B []uint32, x, y, s float64, white, hasShine bool, shine float64) {
	r := 10 * s
	for _, sd := range [2]float64{-1, 1} {
		p.PolyFn([][2]float64{
			{x + sd*2*s, y - r + 2},
			{x + sd*7*s, y - r + 2},
			{x + sd*9*s, achPY + 1},
			{x + sd*4*s, achPY + 1},
		}, func(px, _ float64) (uint32, float64) {
			if math.Abs(px-x) < 4.5*s {
				return B[2], 1
			}
			return B[3], 1
		})
		p.Line(x+sd*2*s, y-r+2, x+sd*4*s, achPY+1, B[0], 1)
		p.Line(x+sd*7*s, y-r+2, x+sd*9*s, achPY+1, B[0], 1)
	}
	st := pixelfx.Star(x, y+0.5, 2.8*s, 6.5*s, 5, -pi/2)
	inStar := func(px, py float64) bool {
		inside := false
		for i, j := 0, len(st)-1; i < len(st); j, i = i, i+1 {
			xi, yi, xj, yj := st[i][0], st[i][1], st[j][0], st[j][1]
			if (yi > py) != (yj > py) && px < ((xj-xi)*(py-yi))/(yj-yi)+xi {
				inside = !inside
			}
		}
		return inside
	}
	p.Shade(x-r-1, y-r-1, x+r+1, y+r+1, func(px, py float64, _ *pixelfx.Out) uint32 {
		dx, dy := px-x, py-y
		d := pixelfx.Hypot(dx, dy)
		if d > r {
			return 0
		}
		if white {
			if r-d < 1 {
				return G[6]
			}
			return pixelfx.White
		}
		if r-d < 1 {
			return G[0]
		}
		l := -(dx*0.6 + dy*0.8) / r
		if hasShine && math.Abs(dx-dy*0.6-shine) < 1.2 {
			return pixelfx.White
		}
		if r-d < 2.5 {
			if l > 0.2 {
				return G[6]
			}
			if l > -0.3 {
				return G[4]
			}
			return G[3]
		}
		if inStar(px, py) {
			if !inStar(px+1, py) || !inStar(px-1, py) || !inStar(px, py+1) || !inStar(px, py-1) {
				return G[2]
			}
			if dx+dy < 0 {
				return G[7]
			}
			return G[6]
		}
		if l > 0.4 {
			return G[5]
		}
		if l > -0.2 {
			return G[4]
		}
		return G[3]
	})
}

func newAchievement(o *pixelfx.Options) pixelfx.Instance {
	G, B := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("ribbon"), 5)
	R := pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{G[7], G[6], G[5], G[3]}})
	lights := &pixelfx.Lights{}
	title, sub := spell(o.Text("title"), "mini"), spell(o.Text("text"), "mini")
	TX := achPX + 34
	t, popped := 0.0, false
	end := achDuration - 0.05
	xAt := func(t float64) float64 {
		if t < achIn {
			return 30 * math.Pow(1-t/achIn, 3)
		}
		if t < achOut {
			return -3 * math.Exp(-(t-achIn)*12) * math.Sin((t-achIn)*30)
		}
		if pixelfx.Seg(t, achOut, achOut+0.06) < 1 {
			return -3 * pixelfx.Seg(t, achOut, achOut+0.06)
		}
		return -3 + 33*pixelfx.Ease.InQuad(pixelfx.Seg(t, achOut+0.06, achOut+0.24))
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !popped && t >= achMedal+0.06 {
				popped = true
				lights.Add(achMX, achMY, 36, G[5], 0.7, 0.4)
				for i := range 12 {
					an := (float64(i)/12)*tau + R.Range(-0.2, 0.2)
					sp := R.Range(40, 90)
					life := R.Range(0.25, 0.45)
					col := pixelfx.Pick(R, []uint32{G[7], G[6], G[5]})
					size := 0.6
					if i%3 == 0 {
						size = R.Range(1.2, 1.8)
					}
					ps.Spawn(pixelfx.StarKind, achMX+math.Cos(an)*9, achMY+math.Sin(an)*9, math.Cos(an)*sp, math.Sin(an)*sp*0.6, life, col, size, 0, 0)
				}
			}
			if t > 0.5 && t < achOut && R.Chance(0.05) {
				px := achMX + R.Range(-11, 11)
				py := achMY + R.Range(-11, 9)
				vy := -R.Range(4, 10)
				life := math.Min(R.Range(0.3, 0.5), end-t)
				size := 0.6
				if R.Chance(0.3) {
					size = 1.4
				}
				ps.Spawn(pixelfx.StarKind, px, py, 0, vy, life, G[6], size, 0, 0)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			x := xAt(t)
			out := t > achOut+0.06
			a := 1.0
			if t < 0.05 {
				a = 0.5
			} else if out {
				a = 1 - pixelfx.Seg(t, achOut+0.12, achOut+0.24)
			}
			if a <= 0 {
				return
			}
			moving := t < achIn*0.8 || out
			if moving {
				f := math.Floor(t * 30)
				for i := range 8 {
					fi := float64(i)
					y := achPY + 3 + pixelfx.Rand(f, fi, 1)*(achPH-6)
					L := 10 + pixelfx.Rand(f, fi, 2)*26
					x0 := x + achPW/2 + 2 + pixelfx.Rand(fi, 3)*6
					p.LineFn(x0, y, math.Min(106, x0+L), y, func(u float64) (uint32, float64) {
						c := G[4]
						if u < 0.3 {
							c = pixelfx.White
						} else if u < 0.6 {
							c = G[6]
						}
						return c, a * (1.1 - u)
					})
				}
			}
			sh := pixelfx.Seg(t, achShine, achShine+0.36)
			hasShine := sh > 0 && sh < 1
			shine := 0.0
			if hasShine {
				shine = achPX - 12 + (achPW+30)*pixelfx.Ease.InOutSine(sh)
			}
			panel(p, achPX+x, achPY, achPW, achPH, achInk, panelOpts{r: 4, a: a, hasShine: hasShine, shine: shine + x})
			p.Line(achPX+x+4, achPY+1, achPX+x+achPW-5, achPY+1, G[4], a)
			p.Px(achPX+x+3, achPY+1, G[2])

			var ti []wordItem
			for _, L := range title.letters {
				if L.g != nil {
					ti = append(ti, wordItem{g: L.g, x: TX + x + float64(L.x)*2, y: achPY + 5, sx: 2, sy: 2, a: a})
				}
			}
			drawWord(p, ti, wordStyle{
				fill: []uint32{G[4], G[5], G[6]}, edge: G[7], under: G[3], outline: achInk.outline,
				hasShine: hasShine, shine: shine + x, hasShineY: true, shineY: achPY, glint: G[7],
			})
			var si []wordItem
			for _, L := range sub.letters {
				if L.g != nil {
					si = append(si, wordItem{g: L.g, x: TX + x + float64(L.x)*2, y: achPY + 19, sx: 2, sy: 2, a: a})
				}
			}
			drawWord(p, si, wordStyle{
				fill: []uint32{achSubFill0, achSubFill1, pixelfx.White}, edge: pixelfx.White, under: achSubUnder, outline: achInk.outline,
				hasShine: hasShine, shine: shine + x, hasShineY: true, shineY: achPY,
			})
			mk := t - achMedal
			if mk > 0 {
				s := 1.5
				if mk >= 0.035 {
					s = 1 + 0.35*math.Exp(-mk*16)*math.Cos(mk*28)
				}
				drawMedal(p, G, B, achMX+x, achMY, s, mk < 0.07, hasShine, shine-achMX+4)
				extra := 0.0
				if mk < 0.15 {
					extra = 0.5 * (1 - mk/0.15)
				}
				p.Glow(achMX+x, achMY, 20, G[5], 0.3*a+extra)
				g := math.Mod(t-1.5, 0.9) / 0.9
				if t > 1.5 && g < 0.25 {
					pixelfx.Sparkle(p, achMX+x-4, achMY-4, 4*math.Sin((g/0.25)*pi), G[7], G[6], 0.5)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
