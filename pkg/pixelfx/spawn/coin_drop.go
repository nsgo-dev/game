// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/coin-drop.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source pickup (https://www.spritefusion.com/pixel-art-vfx/pickup) for the coin's spin (squashed toward its edge), glints and burst of light, and the soft smoke of the poof, MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CoinDrop is an enemy going up in a soft poof while gold coins spin out of it, bounce with a glint on each hit, settle on the floor, twinkle, and wink out one by one. It is registered as "spawn/coin-drop".
var CoinDrop = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Coin drop",
		Desc:     "An enemy goes up in a soft poof and gold coins spin out of it, bounce with a glint on each hit, settle on the floor, twinkle, and wink out one by one.",
		Tags:     []string{"spawn", "death", "loot", "coins", "gold", "drop", "pickup", "ground", "one-shot"},
		Size:     [2]int{160, 88},
		Origin:   [2]int{80, 78},
		Duration: 3,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1606", "#5c3208", "#9a5a0c", "#d48c14", "#f5b82a", "#ffe066", "#fff6c4", "#ffffff")},
			{Name: "smoke", Default: pixelfx.Palette("#3a3450", "#6c6688", "#a8a2c0", "#dcd8ea")},
			{Name: "coins", Default: pixelfx.Num(7)},
			{Name: "seed", Default: pixelfx.Num(23)},
		},
	},
	New: newCoinDrop,
}

func init() { pixelfx.Register("spawn/coin-drop", CoinDrop) }

const coinDropCY = -16.0

// an original coin: a rim, a lit face, a raised mark in the middle
var coinDropSprite = []string{"..oooo..", ".ohhlmo.", "ohlmmmdo", "ohmdhmdo", "olmdhmdo", "olmmmmdo", ".oddddo.", "..oooo.."}

// lying flat on the floor, seen from the side
var coinDropFlat = []string{".lhhhl.", "olmmmdo", ".odddo."}

type coinDropBall struct {
	x, y, vx, vy, r, life, max float64
}

type coinDropCoin struct {
	x, y, vx, vy, ph, w float64
	rest, gone          bool
	hits                int
	out, glint, born    float64
	restAt              float64
}

func newCoinDrop(o *pixelfx.Options) pixelfx.Instance {
	C, SM, R := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("smoke"), 4), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	PAL := map[rune]uint32{'o': C[1], 'h': C[6], 'l': C[5], 'm': C[4], 'd': C[3]}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[6], C[5], C[3]}})
	lights := &pixelfx.Lights{}
	var coins []coinDropCoin
	var balls []coinDropBall
	starCols := []uint32{pixelfx.White, C[6], C[5]}

	// the poof the enemy goes up in
	for i := range 10 {
		a := (float64(i)/10)*tau + R.Range(-0.25, 0.25)
		sp := R.Range(60, 100)
		life := R.Range(0.55, 0.85)
		r := R.Range(4, 6)
		balls = append(balls, coinDropBall{
			x: math.Cos(a) * 2, y: coinDropCY + math.Sin(a)*2,
			vx: math.Cos(a) * sp, vy: math.Sin(a)*sp*0.7 - 10,
			r: r, life: life, max: life,
		})
	}
	// the coins: thrown up and out, each spinning at its own rate
	n := o.Num("coins")
	for i := 0.0; i < n; i++ {
		u := 0.0
		if n > 1 {
			u = i/(n-1) - 0.5
		}
		a := -pi/2 + u*1.6 + R.Range(-0.1, 0.1)
		sp := R.Range(230, 300)
		x := R.Range(-3, 3)
		ph := R.Range(0, tau)
		w := R.Range(9, 15) * R.Sign()
		out := 2.15 + i*0.07 + R.Range(0, 0.1)
		glint := R.Range(1.2, 1.9)
		born := R.Range(0.04, 0.12)
		coins = append(coins, coinDropCoin{
			x: x, y: coinDropCY, vx: math.Cos(a) * sp * 0.4, vy: math.Sin(a) * sp,
			ph: ph, w: w, out: out, glint: glint, born: born,
		})
	}
	for range 8 {
		a, sp := R.Range(0, tau), R.Range(50, 120)
		life, col, size := R.Range(0.3, 0.6), pixelfx.Pick(R, starCols), R.Range(0.8, 1.8)
		ps.Spawn(pixelfx.StarKind, 0, coinDropCY, math.Cos(a)*sp, math.Sin(a)*sp-20, life, col, size, 0, 0)
	}
	lights.Add(0, coinDropCY, 44, C[4], 0.7, 0.4)
	lights.Add(0, coinDropCY, 20, pixelfx.White, 0.7, 0.1)
	t := 0.0

	// a coin turning in the air: its face squashed toward the edge, the rim showing when it's side-on
	drawCoin := func(p *pixelfx.Painter, x, y, ph float64) {
		c := math.Cos(ph)
		sx := math.Abs(c)
		if sx < 0.28 {
			for dy := -3.0; dy <= 3; dy++ {
				col := C[5]
				if math.Abs(dy) == 3 {
					col = C[1]
				} else if dy < 0 {
					col = C[6]
				}
				p.Px(x, y+dy, col)
			}
			if sx > 0.12 {
				dx := -1.0
				if c > 0 {
					dx = 1
				}
				for dy := -2.0; dy <= 2; dy++ {
					p.Px(x+dx, y+dy, C[3])
				}
			}
			return
		}
		half := 4 * sx
		for px := math.Floor(x - half); px < math.Ceil(x+half); px++ {
			col := int(math.Floor((px+0.5-x)/sx + 4))
			if col < 0 || col > 7 {
				continue
			}
			if c < 0 {
				col = 7 - col
			}
			for row := range 8 {
				ch := rune(coinDropSprite[row][col])
				if ch != '.' {
					p.Px(px, pixelfx.Round(y-4+float64(row)), PAL[ch])
				}
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := len(balls) - 1; i >= 0; i-- {
				b := &balls[i]
				b.life -= dt
				if b.life <= 0 {
					balls = append(balls[:i], balls[i+1:]...)
					continue
				}
				b.vx -= b.vx * 7 * dt
				b.vy -= b.vy*7*dt + 26*dt
				b.x += b.vx * dt
				b.y += b.vy * dt
			}
			for i := range coins {
				c := &coins[i]
				// a coin winks out with a little burst of gold
				if !c.gone && t >= c.out {
					c.gone = true
					for range 5 {
						a, sp := R.Range(-pi, 0), R.Range(25, 60)
						life, col, size := R.Range(0.3, 0.55), pixelfx.Pick(R, starCols), R.Range(0.7, 1.4)
						ps.Spawn(pixelfx.StarKind, c.x, -3, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
					}
					lights.Add(c.x, -3, 14, C[5], 0.6, 0.2)
				}
				if t < c.born || t > c.out || c.rest {
					continue
				}
				c.vy += 820 * dt
				c.vx -= c.vx * 0.4 * dt
				c.x += c.vx * dt
				c.y += c.vy * dt
				c.ph += c.w * dt
				if c.y >= -1.5 && c.vy > 0 {
					c.y = -1.5
					c.hits++
					// a tink of light and a puff of dust at each hit
					sz := 1.4
					if c.hits == 1 {
						sz = 2.2
					}
					ps.Spawn(pixelfx.StarKind, c.x, -3, 0, -8, 0.25, pixelfx.White, sz, 0, 0)
					for range 2 {
						dx := R.Range(-1, 1)
						vx := R.Sign() * R.Range(8, 24)
						vy := -R.Range(2, 8)
						life := R.Range(0.25, 0.45)
						size := R.Range(1, 1.8)
						ps.Spawn(pixelfx.Dust, c.x+dx, -0.5, vx, vy, life, SM[1], size, 0, 0)
					}
					lights.Add(c.x, -3, 12, C[5], 0.5, 0.15)
					if c.vy > 110 {
						c.vy *= -0.45
						c.vx *= 0.7
						c.w = c.w*0.8 + R.Range(-2, 2)
					} else {
						c.rest = true
						c.vx, c.vy = 0, 0
						c.restAt = t
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a warm gleam on the floor where the coins lie
			for i := range coins {
				c := &coins[i]
				if c.rest && t < c.out {
					p.Glow(c.x, -1, 7, C[4], 0.18)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the poof: soft, top-lit balls, a pop of light in the middle
			for layer := range 3 {
				for i := range balls {
					b := &balls[i]
					age, k := b.max-b.life, b.life/b.max
					shrink := 1.0
					if k < 0.35 {
						shrink = k / 0.35
					}
					r := b.r * (0.6 + 0.6*pixelfx.Ease.OutCubic(pixelfx.Clamp(age/0.2))) * shrink
					if r < 0.6 {
						continue
					}
					switch layer {
					case 0:
						p.Disc(b.x+r*0.15, b.y+r*0.2, r, SM[1], 0.8)
					case 1:
						col := SM[2]
						if b.y > coinDropCY+2 {
							col = SM[1]
						}
						p.Disc(b.x-r*0.06, b.y-r*0.08, r*0.86, col, 0.85)
					case 2:
						if b.y < coinDropCY+2 {
							p.Disc(b.x-r*0.28, b.y-r*0.34, r*0.4, SM[3], 0.85)
						}
					}
				}
			}
			if t < 0.07 {
				k := t / 0.07
				p.Circle(0, coinDropCY, 8+2*k, C[6], 1)
				p.Circle(0, coinDropCY, 6+2*k, pixelfx.White, 1)
				pixelfx.Flare(p, 0, coinDropCY, 16*(1-k*0.5), 1-k, C)
			}
			for i := range coins {
				c := &coins[i]
				if t < c.born || t > c.out+0.12 {
					continue
				}
				if t > c.out {
					// a coin winks out with a last flash and a ring
					k := (t - c.out) / 0.12
					pixelfx.Sparkle(p, c.x, -2, pixelfx.Round(5*(1-k)), pixelfx.White, C[5], 0.5)
					p.Ring(c.x, -3, 2+5*pixelfx.Ease.OutCubic(k), 1, C[6], 1.1-k)
					continue
				}
				if c.rest {
					// flat on the floor; now and then a glint runs over it
					p.Sprite(coinDropFlat, PAL, pixelfx.Round(c.x)-3, -3, 1, pixelfx.SpriteOpts{})
					p.Glow(c.x, -2, 6, C[5], 0.25)
					g := math.Mod(t-c.restAt+c.glint, 0.95) / 0.95
					if g < 0.2 {
						pixelfx.Sparkle(p, c.x-1, -3, pixelfx.Round(3*math.Sin((g/0.2)*pi)), pixelfx.White, C[5], 0.4)
					}
				} else {
					drawCoin(p, c.x, c.y, c.ph)
					p.Glow(c.x, c.y, 8, C[4], 0.3)
					// a glint catches the face as it turns toward us
					if math.Abs(math.Cos(c.ph)) > 0.96 {
						p.Px(c.x-1, c.y-2, pixelfx.White)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
