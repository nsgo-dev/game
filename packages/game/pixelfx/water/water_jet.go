// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/water-jet.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source energy beam (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez, for the stream's build (a white core banded out to darker edges, surges running along it, light down its length, a flare where it leaves the source).

package water

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// WaterJet blasts a pressurised stream right: a tight glassy rod with surges running down it that swells, roughens, bends under its weight and breaks into chunks, spray and mist. It is registered as "water/water-jet".
var WaterJet = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Water jet",
		Desc:     "A pressurised stream blasts right: a tight glassy rod with surges running down it that swells, roughens, bends under its weight and breaks into chunks, spray and mist.",
		Tags:     []string{"water", "jet", "stream", "hose", "spray", "directional", "loop"},
		Size:     [2]int{188, 80},
		Origin:   [2]int{14, 32},
		Duration: waterJetDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "length", Default: pixelfx.Num(118)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newWaterJet,
}

func init() { pixelfx.Register("water/water-jet", WaterJet) }

const (
	waterJetDuration = 1
	waterJetG        = 520
	waterJetSpeed    = 260
)

func waterJetDrop(x, L float64) float64 {
	u := x / L
	return u * u * 9
}

func waterJetThick(x, L float64) float64 {
	return 2.6 + 5.4*math.Pow(x/L, 1.1)
}

func newWaterJet(o *pixelfx.Options) pixelfx.Instance {
	C, L, S := pixelfx.Fit(o.Palette(), 8), o.Num("length"), o.Seed()
	D := float64(waterJetDuration)
	bandCols := [4]uint32{pixelfx.White, C[6], C[5], C[4]}
	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			T := ph * tau
			// the stream, column by column: white down its core, banded out to blue edges, shadowed underneath; streaks race along it and surges bulge through; past halfway it roughens, and its far end tears into chunks with gaps
			for X := 0.0; X <= L+3; X++ {
				x := X + 0.5
				u := x / L
				if u > 1 {
					break
				}
				yc := waterJetDrop(x, L)
				rough := pixelfx.Smooth(0.25, 1, u)
				sd := pixelfx.Wrap(x-ph*waterJetSpeed*2, 130) - 65
				w0 := waterJetThick(x, L) + math.Exp(-(sd*sd)/90)*1.4*(1-u*0.5)
				up := w0 + pixelfx.TileNoise(x*0.18-ph*30, 9, 30, 0, S+1)*2.2*rough
				dn := w0 + pixelfx.TileNoise(x*0.18-ph*30, 3, 30, 0, S+1)*2.2*rough
				for Y := math.Floor(yc - up); Y <= math.Ceil(yc+dn); Y++ {
					y := Y + 0.5
					half := dn
					if y < yc {
						half = up
					}
					q := (y - yc) / math.Max(0.8, half)
					aq := math.Abs(q)
					if aq > 1 {
						continue
					}
					if u > 0.55 && pixelfx.TileNoise(x*0.12-ph*25, (y-yc)*0.25, 25, 0, S+2) < -0.9+1.6*pixelfx.Smooth(0.55, 1, u) {
						continue
					}
					streak := pixelfx.TileNoise(x*0.06-ph*40, math.Floor(y-yc+20)*1.7, 40, 0, S)
					lvl := 3
					switch {
					case aq < 0.2:
						lvl = 0
					case aq < 0.48:
						lvl = 1
					case aq < 0.78:
						lvl = 2
					}
					if streak > 0.35 && lvl > 0 {
						lvl--
					}
					if q > 0.4 && lvl < 3 {
						lvl++
					}
					p.Px(X, Y, bandCols[lvl])
					if lvl == 0 {
						p.Light(x, y, C[5], 0.12)
					}
				}
			}
			// the nozzle flare: a white burst where it leaves, ringed with light
			fl := 0.85 + 0.15*math.Sin(T*6)
			p.Circle(0, 0, 4.4*fl, C[5], 1)
			p.Circle(0, 0, 3.4*fl, C[6], 1)
			p.Circle(0, 0, 2.2*fl, pixelfx.White, 1)
			for a := 0.0; a < tau; a += 0.35 {
				p.Dot(math.Cos(a+T*2)*5.2*fl, math.Sin(a+T*2)*5.2*fl, C[5], 0.6)
			}
			p.Glow(0, 0, 24, C[5], 0.6*fl)
			p.Glow(0, 0, 8, pixelfx.White, 0.4*fl)
			// where it pinches apart: beads strung out along the end of the stream, racing on
			for k := 0; k < 6; k++ {
				x := L*0.82 + math.Mod(float64(k)*13+ph*waterJetSpeed, 78)
				extra := 0.0
				if x > L {
					d := (x - L) / 60
					extra = d * d * 10
				}
				y := waterJetDrop(math.Min(x, L), L) + extra + math.Sin(float64(k)*2.1)*1.5
				if x < L+34 {
					s := 2.0
					if k%3 == 0 {
						s = 3
					}
					bead(p, x, y, waterJetSpeed, 40, s, C, 1)
				}
			}
			// spray: a widening cone of drops shed from the stream and torn off its end, arcing down
			pixelfx.Cycle(t, D, 70, S+3, func(g *pixelfx.Rng) float64 { return g.Range(0.2, 0.42) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				f := math.Sqrt(r.Next())
				x0 := L * (0.3 + 0.7*f)
				y0 := waterJetDrop(x0, L) + r.Range(-1, 1)*waterJetThick(x0, L)
				vx := r.Range(150, 270) * (0.55 + 0.45*f)
				vy := r.Range(-1, 1)*110*f + r.Range(-10, 20)
				x, y, k := x0+vx*age, y0+vy*age+0.5*waterJetG*age*age, age/life
				if x > L+50 || y > 40 || y < -28 || (k > 0.75 && r.Next() < (k-0.75)*4) {
					return
				}
				if r.Next() < 0.45 {
					c := C[6]
					if k < 0.5 {
						c = pixelfx.White
					}
					p.Px(x, y, c)
				} else {
					s := 1.0
					if r.Next() < 0.2 {
						s = 2
					}
					bead(p, x, y, vx, vy+waterJetG*age, s, C, 1)
				}
			})
			// mist blooming off the end
			pixelfx.Cycle(t, D, 12, S+4, func(g *pixelfx.Rng) float64 { return g.Range(0.7, 1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				x := L*r.Range(0.7, 1) + age*r.Range(20, 45)
				y := waterJetDrop(math.Min(x, L), L) + r.Range(-5, 5) + age*r.Range(4, 14)
				rad := r.Range(3, 5) + k*r.Range(5, 9)
				a := r.Range(0.1, 0.18) * math.Min(1, k/0.15) * math.Pow(1-k, 1.5)
				p.DiscFn(x, y, rad, func(d, _ float64) (uint32, float64) { return C[6], a * (1 - d*d) })
			})
			// light: down the core, brightest near the nozzle
			for x := 0.0; x < L; x += 10 {
				p.Glow(x, waterJetDrop(x, L), 14+x*0.1, C[4], 0.26*(1-(x/L)*0.5))
			}
			p.Glow(L*0.9, waterJetDrop(L*0.9, L), 26, C[5], 0.22)
		},
	}
}
