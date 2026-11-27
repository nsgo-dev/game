// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/torch.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tongue, lick, flame-puff, smoke and ember recipe from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Torch is a wall torch on an iron bracket: a licking flame leaping off a pitch-soaked wrap, licks and puffs torn off its tips, smoke, embers, a flickering light on the wall. It is registered as "fire/torch".
var Torch = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Torch",
		Desc:     "A wall torch on an iron bracket: a licking flame leaping off a pitch-soaked wrap, licks and puffs torn off its tips, smoke, embers, a flickering light on the wall.",
		Tags:     []string{"fire", "torch", "prop", "light", "wall", "dungeon", "loop"},
		Size:     [2]int{72, 120},
		Origin:   [2]int{36, 94},
		Duration: torchDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "wood", Default: pixelfx.Palette("#0e080a", "#24130f", "#3b2216", "#5a3520", "#8a5a34", "#b9884f")},
			{Name: "iron", Default: pixelfx.Palette("#0d0b12", "#1e1b26", "#363242", "#5b5566", "#8a8396")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newTorch,
}

func init() { pixelfx.Register("fire/torch", Torch) }

const (
	torchDuration = 2
	torchBase     = -16
)

// torchTongues are the flame: x, y from the wrap's top, height share, half-width, back row.
var torchTongues = []tongue{
	{-1.9, -1, 0.74, 2.0, true},
	{2.1, -1, 0.7, 2.0, true},
	{-3.4, 0.5, 0.42, 1.6, false},
	{3.6, 0.5, 0.46, 1.6, false},
	{0, 0, 1, 2.9, false},
}

// torchPlate draws the bracket: a bevelled iron plate on the wall with two rivets.
func torchPlate(p *pixelfx.Painter, I, H []uint32, fl float64) {
	p.Shade(-5.5, -5.5, 5.5, 11.5, func(x, y float64, _ *pixelfx.Out) uint32 {
		if x < -5 || x > 5 || y < -5 || y > 11 {
			return I[0]
		}
		if math.Abs(x) < 0.6 && (math.Abs(y+3.5) < 0.6 || math.Abs(y-9.5) < 0.6) {
			return pixelfx.Mix(I[4], H[3], 0.35*fl)
		}
		if y < -4 || x < -4 {
			return pixelfx.Mix(I[3], H[4], 0.3*fl)
		}
		if y > 10 || x > 4 {
			return I[1]
		}
		if y < 2 {
			return I[2]
		}
		return I[1]
	})
}

// torchHandle draws the wooden handle.
func torchHandle(p *pixelfx.Painter, W []uint32) {
	p.Shade(-3, -12, 3, 15, func(x, y float64, _ *pixelfx.Out) uint32 {
		half := 1.3 + pixelfx.Clamp((14-y)/26)*0.9
		if math.Abs(x) > half+0.5 || y > 14.5 {
			return 0
		}
		if math.Abs(x) > half-0.5 || y > 13.5 {
			return W[0]
		}
		g := pixelfx.Rand(math.Floor(y/3), pixelfx.Round(x), 9)
		if x < -0.3 {
			if g < 0.3 {
				return W[2]
			}
			return W[4]
		}
		if g < 0.4 {
			return W[1]
		}
		return W[2]
	})
}

// torchRing draws the ring round the handle.
func torchRing(p *pixelfx.Painter, I, H []uint32, fl float64) {
	p.Rect(-3, -2, 7, 4, I[0], 1)
	p.Rect(-2, -1, 5, 1, pixelfx.Mix(I[4], H[3], 0.3*fl), 1)
	p.Rect(-2, 0, 5, 1, I[2], 1)
}

// torchWrap draws the head: rags bound with cord and soaked in pitch, flaring into a cup at the top, charred where it burns, with embers breathing in the folds.
func torchWrap(p *pixelfx.Painter, W, I, H []uint32, t, D float64) {
	p.Shade(-6, torchBase-2, 6, -9, func(x, y float64, out *pixelfx.Out) uint32 {
		v := (y - (torchBase - 1.5)) / 7.5
		half := 4.6 - v*1.9
		if math.Abs(x) > half+0.5 || y < torchBase-1.5 || y > -9.5 {
			return 0
		}
		if math.Abs(x) > half-0.5 || y > -10.5 {
			return W[0]
		}
		cord := math.Mod(math.Floor(y-x*0.6+40), 3) == 0
		h := pixelfx.Rand(math.Floor(x), math.Floor(y), 31)
		if v < 0.45 {
			if h < 0.42 {
				g := 0.5 + 0.5*math.Sin((t/D)*math.Pi*4+h*40)
				out.K = 0.3 * g
				switch {
				case g > 0.66:
					return H[2]
				case g > 0.33:
					return H[4]
				}
				return H[5]
			}
			if h < 0.75 {
				return W[0]
			}
			return W[1]
		}
		if cord {
			return I[2]
		}
		switch {
		case x < -1:
			return W[3]
		case x < 1.5:
			return W[2]
		}
		return W[1]
	})
}

func newTorch(o *pixelfx.Options) pixelfx.Instance {
	D := float64(torchDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, W, I, S := hotFirst(F), pixelfx.Fit(o.Ramp("wood"), 6), pixelfx.Fit(o.Ramp("iron"), 5), o.Seed()
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		fl0 := 0.74 + 0.16*NL(t, 7.3, S+1, 0) + 0.1*NL(t, 19, S+2, 0)
		torchPlate(p, I, H, fl0)
		torchHandle(p, W)
		torchRing(p, I, H, fl0)
		burn(p, t, torchTongues, burnOpts{
			H: H, D: D, NL: NL, S: S, X: 0, Y: torchBase, Height: 20, Lean: 0.12, Spread: 4,
			Puffs: 10, Licks: 5, Smoke: 7, SmokeSize: 2.3, SmokeLife: [2]float64{1.3, 2.1}, Embers: 7, GlowR: 48, Glow: 1.25, Wind: 2,
			Mid: func(float64) { torchWrap(p, W, I, H, t, D) },
		})
	}}
}
