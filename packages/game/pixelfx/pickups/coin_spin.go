// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/coin-spin.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the hover, floor shadow and glints from Sprite Fusion's open-source pickup (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// CoinSpin is a thick gold coin turning on its edge: an embossed star face, a reeded rim showing as it goes edge-on, a flash sweeping the face, bobbing over its shadow. It is registered as "pickups/coin-spin".
var CoinSpin = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Coin spin",
		Desc:     "A thick gold coin turning on its edge: an embossed star face, a reeded rim showing as it goes edge-on, a flash sweeping the face, bobbing over its shadow.",
		Tags:     []string{"pickup", "coin", "gold", "money", "loop"},
		Size:     [2]int{56, 56},
		Origin:   [2]int{28, 48},
		Duration: coinSpinDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1e1206", "#4a2c08", "#86520c", "#c48412", "#f0b424", "#ffe066", "#fff6c4", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newCoinSpin,
}

func init() { pixelfx.Register("pickups/coin-spin", CoinSpin) }

const (
	coinSpinDuration = 1.6
	coinSpinR        = 7.0
	coinSpinTH       = 3.0
	coinSpinHover    = 17.0
)

func newCoinSpin(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	T := float64(coinSpinDuration)
	L := newLayer(32, 32)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / T
			th := ph * tau
			y := -coinSpinHover - 2*math.Sin(ph*tau)
			blobShadow(p, 0, 2.5+4.5*math.Abs(math.Cos(th))+1.5*math.Abs(math.Sin(th)), 0.85-(-y-coinSpinHover+2)*0.06, 0)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / T
			th := ph * tau
			y := -coinSpinHover - 2*math.Sin(ph*tau)
			facing := math.Abs(math.Cos(th))
			p.Glow(0, y, 20, C[4], 0.3+0.12*facing)
			flash := coin3d(L, p, 0, y, coinSpinR, coinSpinTH, th, C, 1, false)
			p.Glow(-1, y-1, 12, C[6], 0.35*flash)
			if flash > 0.3 {
				glint(p, -4, y-4, 1+3.2*flash, C[6], 0.5*flash)
			}
			for i := range 5 {
				fi := float64(i)
				life := 0.35
				age := pixelfx.Wrap(t-(fi/5)*T-pixelfx.Rand(fi, seed, 3)*0.08, T)
				if age > life {
					continue
				}
				a := pixelfx.Rand(fi, seed, 1)*tau + fi*2.1
				d := 10 + pixelfx.Rand(fi, seed, 2)*5
				k := math.Sin((age / life) * pi)
				glint(p, math.Cos(a)*d, y+math.Sin(a)*d*0.85-age*8, pixelfx.Round(2*k), pick(i%2 != 0, C[6], pixelfx.White), 0.35*k)
			}
		},
	}
}
