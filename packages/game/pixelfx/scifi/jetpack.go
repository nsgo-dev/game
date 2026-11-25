// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/jetpack.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire) for the exhaust (flame puffs cooling down the fire ramp into smoke, warm light over the smoke), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Jetpack is twin thrusters firing straight down: white-hot cores with shock diamonds inside ragged flames, puffs that cool into smoke billowing below, lit by the fire. It is registered as "scifi/jetpack".
var Jetpack = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Jetpack",
		Desc:     "Twin thrusters fire straight down: white-hot cores with shock diamonds inside ragged flames, puffs that cool into smoke billowing below, lit by the fire.",
		Tags:     []string{"scifi", "jetpack", "thrust", "rocket", "fire", "smoke", "flight", "loop"},
		Size:     [2]int{96, 116},
		Origin:   [2]int{48, 30},
		Duration: jetDuration,
		Loop:     true,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0e12", "#5c1818", "#a02818", "#e0501a", "#ff8a2a", "#ffc23d", "#fff3a6", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#0e0d14", "#1e1c27", "#312e3e", "#4b4759", "#6f6a82", "#9d98b0")},
			{Name: "length", Default: pixelfx.Num(26)},
			{Name: "seed", Default: pixelfx.Num(30)},
		},
	},
	New: newJetpack,
}

func init() { pixelfx.Register("scifi/jetpack", Jetpack) }

const (
	jetDuration = 2.0
	jetGap      = 8.0
	jetFR       = 30
)

var jetNF = int(pixelfx.Round(jetDuration * jetFR))

// jetPackDraw draws the pack: twin tanks with rounded caps, a spine between them, belled nozzles glowing hot inside.
func jetPackDraw(p *pixelfx.Painter, H, F []uint32, dy float64) {
	for _, s := range [2]float64{-1, 1} {
		cx := s * jetGap
		p.Shade(cx-5, -24+dy, cx+5, -3+dy, func(x, y float64, out *pixelfx.Out) uint32 {
			u := (x - cx) / 4.5
			top := -19 + dy
			v := 0.0
			if y < top {
				v = (y - top) / 4.5
			}
			if u*u+v*v > 1 {
				return 0
			}
			l := -u*0.8 - v*0.6
			switch {
			case u*u+v*v > 0.78:
				out.C = H[0]
			case math.Abs(y-(-12+dy)) < 0.6:
				out.C = H[1]
			case l > 0.55:
				out.C = H[5]
			case l > 0.15:
				out.C = H[4]
			case l > -0.35:
				out.C = H[3]
			default:
				out.C = H[2]
			}
			return 0
		})
		// the nozzle bell, wider at its mouth, its inside lit by the flame
		for y := -3.0; y <= 0; y++ {
			w := 2 + (y+3)*0.7
			for x := math.Floor(cx - w); x <= math.Ceil(cx+w); x++ {
				inner := math.Abs(x+0.5-cx) < w-1.2 && y >= -1
				var c uint32
				switch {
				case inner:
					if y == 0 {
						c = F[6]
					} else {
						c = F[4]
					}
				case math.Abs(x+0.5-cx) > w-0.6:
					c = H[0]
				case x < cx:
					c = H[3]
				default:
					c = H[1]
				}
				p.Px(x, y+dy, c)
			}
		}
	}
	for y := -20.0; y <= -6; y++ {
		p.Px(-1, y+dy, H[1])
		p.Px(0, y+dy, H[2])
		p.Px(1, y+dy, H[0])
	}
	// a status light on the spine
	p.Px(0, -16+dy, F[5])
}

func newJetpack(o *pixelfx.Options) pixelfx.Instance {
	F, H, L0, S := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("metal"), 6), o.Num("length"), o.Seed()
	rn := pixelfx.NewRng(S)
	g := newGhosts(jetDuration, ghostOpts{
		sparks: []uint32{pixelfx.White, F[6], F[5], F[4]},
		hasBox: true,
		box:    [4]float64{-46, -28, 46, 84},
	})
	for _, s := range [2]float64{-1, 1} {
		// flame shed off the tip, cooling down the ramp into smoke that's driven down, slows and billows
		for range 26 {
			born := rn.Range(0, jetDuration)
			x := s*jetGap + rn.Range(-1.5, 1.5)
			y := L0 * rn.Range(0.55, 0.85)
			vx, vy := rn.Range(-10, 10), rn.Range(70, 130)
			life, size := rn.Range(0.22, 0.4), rn.Range(2.2, 3.4)
			flag := pixelfx.Smokes
			if rn.Chance(0.5) {
				flag |= pixelfx.Big
			}
			g.add(born, pixelfx.Fire, x, y, vx, vy, life, 0, size, flag)
		}
		for range 10 {
			born := rn.Range(0, jetDuration)
			x := s*jetGap + rn.Range(-3, 3)
			y := L0 + rn.Range(2, 8)
			vx, vy := s*rn.Range(0, 14), rn.Range(40, 80)
			life, size := rn.Range(1.1, 1.6), rn.Range(3, 5)
			g.add(born, pixelfx.Smoke, x, y, vx, vy, life, 0, size, pixelfx.Big)
		}
		// sparks spat out of the exhaust, embers drifting off it
		for range 10 {
			born := rn.Range(0, jetDuration)
			x := s*jetGap + rn.Range(-1, 1)
			y := L0 * rn.Range(0.3, 0.7)
			vx, vy := s*rn.Range(-40, 60), rn.Range(80, 200)
			life := rn.Range(0.2, 0.45)
			g.add(born, pixelfx.Spark, x, y, vx, vy, life, pixelfx.White, 1, 0)
		}
		for range 5 {
			born := rn.Range(0, jetDuration)
			x := s*jetGap + rn.Range(-2, 2)
			y := L0 * rn.Range(0.6, 1)
			vx, vy := s*rn.Range(10, 40), rn.Range(20, 60)
			life, size := rn.Range(0.6, 1.1), rn.Range(0.8, 1.6)
			g.add(born, pixelfx.Ember, x, y, vx, vy, life, F[4], size, 0)
		}
	}

	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / jetDuration
			f := float64(int(math.Floor(t*jetFR)) % jetNF)
			dy := pixelfx.Round(math.Sin(tau*12*ph) * 0.6)
			ps := g.at(t)
			// smoke and spent flame behind, then the plumes
			ps.Draw(p, pixelfx.LayerBack)
			for _, s := range [2]float64{-1, 1} {
				cx := s * jetGap
				if pixelfx.Rand(S, f, s) < 0.15 {
					cx += s * 0.5
				}
				L := L0 * (0.92 + 0.08*math.Sin(tau*7*ph+s) + 0.06*(pixelfx.Rand(S, f, s, 1)-0.5))
				seedOff := 0.0
				if s > 0 {
					seedOff = 9
				}
				// the flame: a cone of heat torn ragged by noise streaming down it, white at its heart, red at its edges
				p.Shade(cx-7, dy, cx+7, dy+L+4, func(x, y float64, out *pixelfx.Out) uint32 {
					v := (y - dy) / L
					bump := 0.0
					if v < 1 {
						bump = 1.4 * math.Sin(v*pi)
					}
					w := pixelfx.Lerp(3.4, 0.6, v) + bump
					n := pixelfx.TileFbm((x-cx)/3+s*5, (y-dy)/4-ph*20, 0, 10, S+seedOff, 2)
					q := math.Abs(x-cx)/w + n*0.45
					if v > 1.15 || q > 1.2 {
						return 0
					}
					heat := (1-v)*1.15 - q*0.9 + n*0.25
					// a dark red rim round the flame, so it reads as a shape and not a smear
					if q > 0.95 {
						if heat > 0.2 {
							out.C = F[3]
						} else {
							out.C = F[2]
						}
						if heat < -0.3 {
							out.A = 0.5
						}
						return 0
					}
					switch {
					case heat > 0.82:
						out.C = pixelfx.White
					case heat > 0.62:
						out.C = F[6]
					case heat > 0.42:
						out.C = F[5]
					case heat > 0.22:
						out.C = F[4]
					case heat > 0.02:
						out.C = F[3]
					default:
						out.C = F[2]
					}
					return 0
				})
				// shock diamonds: bright knots strung down the core, breathing
				for i := range 3 {
					fi := float64(i)
					y := pixelfx.Round(dy + 5 + fi*6.5 + math.Sin(tau*4*ph+fi)*0.5)
					r := pixelfx.Round(2.2 - fi*0.45 + 0.4*math.Sin(tau*8*ph+fi*2))
					if y > dy+L*0.75 || r < 1 {
						break
					}
					edge := F[5]
					if i == 0 {
						edge = F[6]
					}
					for yy := -r; yy <= r; yy++ {
						for xx := -r; xx <= r; xx++ {
							d := math.Abs(xx) + math.Abs(yy)
							if d <= r {
								c := edge
								if d < r {
									c = pixelfx.White
								}
								p.Px(cx+xx, y+yy, c)
							}
						}
					}
					p.Glow(cx, y, 6, F[6], 0.35)
				}
				// light: hot at the nozzle, warm down the plume
				p.Glow(cx, dy+2, 9, pixelfx.White, 0.45)
				p.Glow(cx, dy+L*0.45, L*0.8, F[4], 0.35)
			}
			jetPackDraw(p, H, F, dy)
			ps.Draw(p, pixelfx.LayerFront)
			// the fire lights the smoke below it, and its own surroundings
			p.Glow(0, L0+14, 34, F[4], 0.16)
			p.Glow(0, L0*0.4, 30, F[5], 0.18)
		},
	}
}
