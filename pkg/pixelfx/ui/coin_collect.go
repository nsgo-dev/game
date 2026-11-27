// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/coin-collect.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the coin's pop (a white disc, then a ring and stars) and the trails of flying coins from Sprite Fusion's open-source pickup (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package ui

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CoinCollect bursts a coin into a fountain of spinning coins that hang, then zip one by one into a HUD counter that pops and ticks up, glints and folds away. It is registered as "ui/coin-collect".
var CoinCollect = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Coin collect",
		Desc:     "A coin bursts into a fountain of spinning coins that hang, then zip one by one into a HUD counter that pops and ticks up, glints and folds away.",
		Tags:     []string{"ui", "coins", "pickup", "reward", "hud", "counter", "one-shot"},
		Size:     [2]int{176, 116},
		Origin:   [2]int{60, 102},
		Duration: coinDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1405", "#5c3008", "#9a5a0e", "#d48e18", "#f5b92c", "#ffde5e", "#fff4bc", "#ffffff")},
			{Name: "count", Default: pixelfx.Num(10)},
			{Name: "from", Default: pixelfx.Num(12)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newCoinCollect,
}

func init() { pixelfx.Register("ui/coin-collect", CoinCollect) }

var (
	coinDuration = 2.45
	coinPop      = 0.3
	coinFly0     = 0.58
	coinGap      = 0.075
	coinFly      = 0.28
	coinFold     = 2.0
	coinCY       = -16.0
	coinHX       = 50.0
	coinHY       = -88.0
	coinHW       = 60.0
	coinHH       = 22.0
	coinIX       = coinHX + 12
	coinIY       = coinHY + 11
	coinInk      = panelCols{
		outline: pixelfx.Hex("#0d0a14"),
		rim:     pixelfx.Hex("#4a4060"),
		top:     pixelfx.Hex("#2a2338"),
		body:    pixelfx.Hex("#1e1929"),
		low:     pixelfx.Hex("#141019"),
	}
	coinDustCol   = pixelfx.Hex("#5a4f6e")
	coinShadowCol = pixelfx.Hex("#0d0a14")
)

func drawCoin(p *pixelfx.Painter, C []uint32, x, y, r, w float64, white bool) {
	rx, ry := math.Max(0.7, r*w), r
	p.Shade(x-rx-1, y-ry-1, x+rx+1, y+ry+1, func(px, py float64, _ *pixelfx.Out) uint32 {
		u, v := (px-x)/rx, (py-y)/ry
		d := pixelfx.Hypot(u, v)
		if d > 1 {
			return 0
		}
		e := (1 - d) * math.Min(rx*1.6, ry)
		if white {
			if e < 1 {
				return C[6]
			}
			return pixelfx.White
		}
		if e < 1 {
			return C[1]
		}
		if rx < 2 {
			if v < -0.2 {
				return C[6]
			}
			if v < 0.4 {
				return C[4]
			}
			return C[3]
		}
		if e < 2 {
			if u+v < -0.2 {
				return C[6]
			}
			return C[3]
		}
		if math.Abs(px-x) < 0.55 && math.Abs(v) < 0.5 {
			return C[3]
		}
		if px-x >= 0.55 && px-x < 1.55 && math.Abs(v) < 0.5 {
			return C[7]
		}
		l := -(u*0.6 + v*0.8)
		if l > 0.4 {
			return C[7]
		}
		if l > 0.05 {
			return C[6]
		}
		if l > -0.35 {
			return C[5]
		}
		return C[4]
	})
}

type coinItem struct {
	x, y, vx, vy, ph, spin float64
	state                  int
	t0, fx, fy             float64
	trail                  [][2]float64
	bob                    float64
}

func newCoinCollect(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	N := int(pixelfx.ClampTo(pixelfx.Round(o.Num("count")), 1, 12))
	ps := pixelfx.NewParticles(pixelfx.SimOpts{
		Rng:    R,
		Ground: func(float64) float64 { return 0 },
		Sparks: []uint32{C[7], C[6], C[5], C[3]},
	})
	lights := &pixelfx.Lights{}
	end := coinDuration - 0.05
	coins := make([]coinItem, N)
	for i := range coins {
		s := (float64(i)/math.Max(1, float64(N-1)))*2 - 1
		vx := s*R.Range(55, 90) + R.Range(-12, 12)
		vy := -R.Range(170, 285)
		ph := R.Range(0, tau)
		spin := R.Range(9, 14) * R.Sign()
		bob := R.Range(0, tau)
		coins[i] = coinItem{x: 0, y: coinCY, vx: vx, vy: vy, ph: ph, spin: spin, t0: coinFly0 + float64(i)*coinGap, bob: bob}
	}
	t, popped := 0.0, false
	count := int(pixelfx.Round(o.Num("from")))
	bump, flash := 0.0, 0.0

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !popped && t >= coinPop {
				popped = true
				lights.Add(0, coinCY, 46, pixelfx.White, 0.8, 0.14)
				lights.Add(0, coinCY, 48, C[5], 0.6, 0.5)
				for i := range 14 {
					an := (float64(i)/14)*tau + R.Range(-0.2, 0.2)
					sp := R.Range(80, 170)
					ps.Spawn(pixelfx.Spark, math.Cos(an)*3, coinCY+math.Sin(an)*3, math.Cos(an)*sp, math.Sin(an)*sp-30, R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
				}
				for range 4 {
					x := R.Range(-6, 6)
					vx := R.Sign() * R.Range(15, 40)
					vy := -R.Range(2, 8)
					life := R.Range(0.3, 0.5)
					size := R.Range(1.5, 2.5)
					ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, coinDustCol, size, 0, 0)
				}
				for i := range 10 {
					an := (float64(i)/10)*tau + R.Range(-0.2, 0.2)
					sp := R.Range(70, 130)
					down := 1.0
					if math.Sin(an) > 0 {
						down = 0.25
					}
					life := R.Range(0.35, 0.6)
					col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					size := 0.6
					if i%3 == 0 {
						size = R.Range(1.2, 2)
					}
					ps.Spawn(pixelfx.StarKind, math.Cos(an)*6, coinCY+math.Sin(an)*6, math.Cos(an)*sp, math.Sin(an)*sp*down, life, col, size, 0, 0)
				}
			}
			for ci := range coins {
				c := &coins[ci]
				if !popped || c.state == 3 {
					continue
				}
				c.ph += c.spin * dt
				if c.state < 2 {
					if c.state == 0 {
						c.vy += 640 * dt
						c.vx -= c.vx * 2.2 * dt
						if c.vy > 0 {
							c.state = 1
						}
					} else {
						c.vy -= c.vy * 7 * dt
						c.vx -= c.vx * 4 * dt
					}
					c.x += c.vx * dt
					c.y += c.vy * dt
					if t >= c.t0 {
						c.state = 2
						c.fx = c.x
						c.fy = c.y + math.Sin(t*6+c.bob)*1.5
					}
				}
				if c.state == 2 {
					u := pixelfx.Clamp((t - c.t0) / coinFly)
					e := u * u
					mx := c.fx + (coinIX-c.fx)*0.35
					my := math.Min(c.fy, coinIY) - 26
					c.x = (1-e)*(1-e)*c.fx + 2*(1-e)*e*mx + e*e*coinIX
					c.y = (1-e)*(1-e)*c.fy + 2*(1-e)*e*my + e*e*coinIY
					c.trail = append([][2]float64{{c.x, c.y}}, c.trail...)
					if len(c.trail) > 7 {
						c.trail = c.trail[:7]
					}
					if R.Chance(0.25) {
						px := c.x + R.Range(-2, 2)
						py := c.y + R.Range(-2, 2)
						vx := R.Range(-8, 8)
						vy := R.Range(-4, 10)
						life := R.Range(0.2, 0.4)
						col := pixelfx.Pick(R, []uint32{C[7], C[6]})
						ps.Spawn(pixelfx.StarKind, px, py, vx, vy, life, col, 0.6, 0, 0)
					}
					if u >= 1 {
						c.state = 3
						count++
						bump = 1
						flash = 1
						lights.Add(coinIX, coinIY, 26, C[5], 0.6, 0.25)
						for i := range 3 {
							an := R.Range(0, tau)
							sp := R.Range(18, 40)
							life := math.Min(R.Range(0.25, 0.45), end-t)
							col := pixelfx.Pick(R, []uint32{C[7], C[6]})
							size := 0.6
							if i == 0 {
								size = R.Range(1.2, 1.8)
							}
							ps.Spawn(pixelfx.StarKind, coinIX+math.Cos(an)*5, coinIY+math.Sin(an)*5, math.Cos(an)*sp, math.Sin(an)*sp, life, col, size, 0, 0)
						}
					}
				}
			}
			bump = math.Max(0, bump-dt/0.14)
			flash = math.Max(0, flash-dt/0.07)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			k := (t - coinPop) / 0.45
			if k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0, 4+34*pixelfx.Ease.OutCubic(k), 0.22, math.Max(1, 3*(1-k)), 1.15-k, C, func(g float64) bool { return math.Sin(g) < 0 }, false)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			k := (t - coinPop) / 0.45
			if k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0, 4+34*pixelfx.Ease.OutCubic(k), 0.22, math.Max(1, 3*(1-k)), 1.15-k, C, func(g float64) bool { return math.Sin(g) >= 0 }, false)
			}
			if t < coinPop+0.02 {
				y := coinCY + math.Sin(t*9)*1.5
				w := math.Abs(math.Cos(t * 7))
				p.Ellipse(0, 0.5, 6-0.5*math.Sin(t*9), 1.3, coinShadowCol, 0.6)
				for i := range 2 {
					an := t*7 + float64(i)*pi
					ox, oy := math.Cos(an)*11, math.Sin(an)*3
					if math.Sin(an) < 0 {
						p.Px(ox, y+oy, C[4])
					}
				}
				drawCoin(p, C, 0, y, 7, math.Max(0.12, w), t > coinPop-0.04)
				for i := range 2 {
					an := t*7 + float64(i)*pi
					ox, oy := math.Cos(an)*11, math.Sin(an)*3
					if math.Sin(an) >= 0 {
						p.Px(ox, y+oy, C[7])
						p.Glow(ox, y+oy, 4, C[5], 0.4)
					}
				}
				p.Glow(0, y, 20, C[5], 0.45+0.4*pixelfx.Seg(t, coinPop-0.15, coinPop))
				if t > 0.06 && t < 0.2 {
					pixelfx.Sparkle(p, -3, y-3, 3.5*math.Sin(((t-0.06)/0.14)*pi), C[7], C[6], 0.4)
				}
			}
			pk := (t - coinPop) / 0.1
			if pk > 0 && pk < 1 {
				col := C[6]
				if pk < 0.5 {
					col = pixelfx.White
				}
				p.Circle(0, coinCY, 13*(1-pk)+1, col, 1)
			}
			fk := (t - coinPop) / 0.2
			if fk > 0 && fk < 1 {
				pixelfx.Flare(p, 0, coinCY, 26*math.Pow(1-fk, 1.5), 1-fk, C)
			}
			rk := (t - coinPop - 0.03) / 0.35
			if rk > 0 && rk < 1 {
				col := C[6]
				if rk < 0.3 {
					col = pixelfx.White
				}
				p.Ring(0, coinCY, 6+22*pixelfx.Ease.OutCubic(rk), math.Max(1, 2*(1-rk)), col, 1.2-rk)
			}
			for ci := range coins {
				c := &coins[ci]
				if !popped || c.state == 3 {
					continue
				}
				for i := 1; i < len(c.trail); i++ {
					col := C[4]
					if i < 3 {
						col = C[6]
					}
					p.Line(c.trail[i-1][0], c.trail[i-1][1], c.trail[i][0], c.trail[i][1], col, 1.2-float64(i)/7)
				}
				hover := 0.0
				if c.state == 1 {
					hover = math.Sin(t*6+c.bob) * 1.5
				}
				w := math.Abs(math.Cos(c.ph))
				drawCoin(p, C, c.x, c.y+hover, 5, math.Max(0.15, w), false)
				glK := 0.32
				if c.state == 2 {
					glK = 0.5
				}
				p.Glow(c.x, c.y+hover, 10, C[5], glK)
				gl := math.Mod(t*1.7+c.bob, 1)
				if c.state == 1 && gl < 0.12 && w > 0.5 {
					pixelfx.Sparkle(p, c.x-1, c.y+hover-2, 3*math.Sin((gl/0.12)*pi), C[7], C[6], 0.4)
				}
			}
			open := pixelfx.OutBack(pixelfx.Seg(t, 0.06, 0.26), 2.2) * (1 - pixelfx.InBack(pixelfx.Seg(t, coinFold, coinFold+0.2), 2))
			if open > 0.04 {
				h := coinHH * open
				y0 := coinHY + (coinHH-h)/2
				sh := pixelfx.Seg(t, 1.5, 1.76)
				hasShine := sh > 0 && sh < 1
				panel(p, coinHX, y0, coinHW, h, coinInk, panelOpts{
					r: math.Min(4, h/2), a: 1, hasShine: hasShine, shine: coinHX - 6 + (coinHW+16)*sh,
				})
				if open > 0.7 {
					drawCoin(p, C, coinIX, coinIY, 5.5*(1+0.3*bump), 1, flash > 0.5)
					p.Glow(coinIX, coinIY, 14, C[5], 0.25+0.4*bump)
					if bump > 0 {
						col := C[6]
						if bump > 0.6 {
							col = pixelfx.White
						}
						p.Ring(coinIX, coinIY, 7+8*(1-bump), 1, col, bump*1.3)
					}
					s := 2 * (1 + 0.22*bump)
					word := spell("x"+strconv.Itoa(count), "big")
					W, H := float64(word.w)*s, 7*s
					x0, ty := coinIX+9, coinIY-H/2
					fl := 0.0
					if flash > 0.5 {
						fl = 1
					}
					var items []wordItem
					for _, L := range word.letters {
						if L.g != nil {
							items = append(items, wordItem{g: L.g, x: x0 + float64(L.x)*s, y: ty, sx: s, sy: s, a: 1, flash: fl})
						}
					}
					drawWord(p, items, wordStyle{
						fill: []uint32{C[4], C[5], C[6], C[7]}, edge: pixelfx.White, under: C[3], outline: coinInk.outline,
					})
					if bump > 0 {
						p.Glow(x0+W/2, coinIY, 16, C[5], 0.3*bump)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
