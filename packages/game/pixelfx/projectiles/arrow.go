// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/arrow.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Arrow is an arrow in flight: a steel broadhead catching the light, a wooden shaft, fletching fluttering in the wind, streaks of air peeling off its head and swirling off its feathers. It is registered as "projectiles/arrow".
var Arrow = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Arrow",
		Desc:     "An arrow in flight: a steel broadhead catching the light, a wooden shaft, fletching fluttering in the wind, streaks of air peeling off its head and swirling off its feathers.",
		Tags:     []string{"projectile", "arrow", "bow", "medieval", "directional", "loop"},
		Size:     [2]int{112, 32},
		Origin:   [2]int{92, 16},
		Duration: arrowDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#7a1f2c", "#b8323c", "#e8e2d6", "#ffffff")},
			{Name: "wood", Default: pixelfx.Palette("#3a2414", "#6e4422", "#a8723c", "#d8a868")},
			{Name: "steel", Default: pixelfx.Palette("#2e3648", "#5a6680", "#9aa6ba", "#dfe7f2")},
			{Name: "wind", Default: pixelfx.Color("#b8c8e8")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newArrow,
}

func init() { pixelfx.Register("projectiles/arrow", Arrow) }

const (
	arrowDuration = 0.6
	arrowOriginX  = 92
)

var (
	arrowHead   = []string{"..LL....", "..sLLL..", "bbsSSSSW", "..dDDD..", "..DD...."}
	arrowFletch = [3][]string{
		{"AAa.........", ".aAAa.......", "..aaAAa.....", "nkkkkkkkkkkk", "..cCCc......", ".cCCc.......", "CCc........."},
		{".AAa........", "..aAAa......", "..aaAAAa....", "nkkkkkkkkkkk", "..cCCCc.....", "..cCCc......", ".CCc........"},
		{"AAAa........", "aAAa........", ".aaAAa......", "nkkkkkkkkkkk", "..ccCc......", "cCCc........", "CCc........."},
	}
	arrowWindShade = pixelfx.Hex("#2a2540")
)

func newArrow(o *pixelfx.Options) pixelfx.Instance {
	PL, W, ST := pixelfx.Fit(o.Palette(), 4), pixelfx.Fit(o.Ramp("wood"), 4), pixelfx.Fit(o.Ramp("steel"), 4)
	wind, S, D := o.Color("wind"), o.Seed(), float64(arrowDuration)
	left, LEN := float64(-arrowOriginX+2), 36.0
	fletchPal := map[rune]uint32{'a': PL[2], 'A': PL[3], 'c': PL[0], 'C': PL[1], 'k': W[1], 'n': W[0]}
	headPal := map[rune]uint32{'L': ST[3], 's': ST[2], 'S': ST[2], 'd': ST[0], 'D': ST[1], 'b': W[0], 'W': pixelfx.White}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		fr, ph := math.Floor(t*30+1e-6), t/D

		// streaks of air peeling off the head and the fletching, stretching back and thinning
		beat(t, D, 9, S, func(r *pixelfx.Rng) float64 { return r.Range(0.14, 0.26) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			top := pick(r.Chance(0.5), -1.0, 1.0)
			from := pick(r.Chance(0.6), -3.0, -LEN+6)
			x0 := from - age*r.Range(160, 240)
			ln := r.Range(8, 16) * (0.5 + k)
			y := 0.5 + top*(r.Range(3, 5)+k*r.Range(1, 2.5))
			if x0-ln < left {
				return
			}
			c := wind
			if k >= 0.4 {
				c = pixelfx.Mix(wind, arrowWindShade, (k-0.4)*1.2)
			}
			p.LineFn(x0, y, x0-ln, y, func(u float64) (uint32, float64) {
				cc := c
				if u < 0.25 && k < 0.4 {
					cc = pixelfx.Mix(wind, pixelfx.White, 0.5)
				}
				a := 1.0
				if u >= 0.65 {
					a = (1 - u) / 0.35
				}
				return cc, a
			})
		})
		// little eddies spinning off the feathers
		beat(t, D, 6, S+3, func(r *pixelfx.Rng) float64 { return r.Range(0.15, 0.25) }, func(age, life float64, r *pixelfx.Rng, _ int) {
			k := age / life
			cx := -LEN - 2 - age*r.Range(100, 150)
			cy := 0.5 + r.Sign()*r.Range(2, 4)
			a := age*30 + r.Range(0, tau)
			rr := 1.2 + k*1.5
			if cx < left+4 {
				return
			}
			for i := range 4 {
				fi := float64(i)
				p.Dot(cx+math.Cos(a+fi*0.5)*rr, cy+math.Sin(a+fi*0.5)*rr, wind, (1-k)*(1-fi*0.2))
			}
		})

		// the shaft with a lit top line
		p.Line(-LEN+11, 0, -7, 0, W[2], 1)
		for x := -LEN + 14; x < -8; x += 6 {
			p.Px(x, 0, W[3])
		}
		// the fletching flutters through its poses, faster than the frame rate can quite catch
		pose := arrowFletch[int(math.Floor(pixelfx.Rand(fr, S)*3))]
		p.Sprite(pose, fletchPal, -LEN, -3, 1, pixelfx.SpriteOpts{})
		// the binding and the broadhead: a lit upper bevel, a shaded lower one, a white point
		p.Sprite(arrowHead, headPal, -7, -2, 1, pixelfx.SpriteOpts{})
		// a glint runs down the blade now and then
		g := math.Mod(ph*2, 1)
		if g < 0.25 {
			pixelfx.Sparkle(p, -5+g*16, -1, 1+pixelfx.Round(2*math.Sin((g/0.25)*pi)), pixelfx.White, ST[3], 0.25)
		}
	}}
}
