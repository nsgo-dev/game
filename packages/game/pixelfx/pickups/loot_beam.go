// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/loot-beam.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the column recipe (ramp bands out from a white core, drifting strands, dithered edges, light every few rows) from Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// LootBeam is a dropped sword in a tall pillar of rarity light: a white core thinning to the sky, rings pulsing across the floor, motes streaming up, a glint racing up the shaft. It is registered as "pickups/loot-beam".
var LootBeam = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Loot beam",
		Desc:     "A dropped sword in a tall pillar of rarity light: a white core thinning to the sky, rings pulsing across the floor, motes streaming up, a glint racing up the shaft.",
		Tags:     []string{"pickup", "loot", "beam", "rarity", "drop", "item", "loop"},
		Size:     [2]int{96, 176},
		Origin:   [2]int{48, 162},
		Duration: lootBeamDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#140a24", "#2c1250", "#4b1d86", "#7330c0", "#a050f0", "#c890ff", "#ecd4ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newLootBeam,
}

func init() { pixelfx.Register("pickups/loot-beam", LootBeam) }

const (
	lootBeamDuration = 2.4
	lootH            = 150
	lootOX           = 4
)

var (
	lootRings   = [2]float64{0.08, 0.58}
	lootSteel   = pixelfx.Ramp("#151726", "#2c3149", "#4f5775", "#8a93b3", "#d6dcef", "#ffffff")
	lootGold    = pixelfx.Ramp("#3a2008", "#7a4a10", "#b8801c", "#e8b432", "#ffe27a", "#fff6cc")
	lootLeather = pixelfx.Ramp("#1e0f0e", "#3e2016", "#633620")
	lootInk     = pixelfx.Hex("#0d0b14")
)

func lootSince(ph float64, k int) float64 {
	return pixelfx.Wrap(ph-lootRings[k], 1)
}

func lootAxis(x float64) float64 {
	return math.Floor(-4 + (x+4)/7 + 0.5)
}

func drawLootSword(L *layer, p *pixelfx.Painter, C []uint32, ph float64) {
	edgeLit := func(x int) uint32 {
		if math.Abs(float64(x+lootOX)-0.5) < 6 {
			return C[6]
		}
		return pixelfx.Mix(lootSteel[4], C[6], 0.45)
	}
	L.begin(-16, -12)
	set := func(x int, y float64, c uint32) { L.set(float64(x+lootOX), y, c) }
	for x := -11; x <= 10; x++ {
		a := lootAxis(float64(x))
		switch {
		case x <= -9:
			m := x + 10
			if m == 0 {
				set(x, a-1, C[6])
				set(x, a, pixelfx.White)
				set(x, a+1, C[4])
			} else {
				set(x, a, pick(m < 0, C[5], C[3]))
			}
		case x <= -5:
			set(x, a, lootLeather[pick(x%2 != 0, 2, 1)])
			set(x, a+1, lootLeather[pick(x%2 != 0, 1, 0)])
		case x <= -3:
			for y := a - 3; y <= a+3; y++ {
				end := y == a-3 || y == a+3
				var col uint32
				switch {
				case y == a:
					col = pick(x == -4, C[6], C[4])
				case x == -4:
					if end {
						col = lootGold[3]
					} else {
						col = pixelfx.Mix(lootGold[4], C[6], 0.3)
					}
				case end:
					col = lootGold[0]
				default:
					col = lootGold[1]
				}
				set(x, y, col)
			}
		default:
			tip := x - 8
			if tip < 1 {
				set(x, a-1, edgeLit(x))
			}
			if tip < 2 {
				col := pixelfx.Mix(lootSteel[3], C[4], 0.2)
				if tip == 1 {
					col = edgeLit(x)
				}
				set(x, a, col)
			}
			set(x, a+1, pick(tip == 2, lootSteel[2], lootSteel[0]))
		}
	}
	L.outline(lootInk).draw(p, 0, 0, 1)
	g := (ph - 0.72) / 0.16
	if g > 0 && g < 1 {
		x := -1 + 9*pixelfx.Ease.InOutSine(g)
		glint(p, x+lootOX, lootAxis(pixelfx.Round(x))-1, 2.6*math.Sin(g*pi), pixelfx.White, 0.5)
	}
}

func drawLootRing(p *pixelfx.Painter, C []uint32, a float64, back bool) {
	k := a / 1.1
	if k >= 1 {
		return
	}
	r := 3 + 24*pixelfx.Ease.OutCubic(k)
	th := math.Max(1, 3.2*(1-k))
	pixelfx.Band(p, 0.5, 0, r, 0.2, th, math.Min(1, 1.25*(1-k)), []uint32{C[2], C[4], C[5], C[6]}, func(g float64) bool {
		return (math.Sin(g) < 0) == back
	}, k < 0.12)
	if !back {
		for _, s := range [2]float64{-1, 1} {
			p.Glow(0.5+s*r, -1, 8, C[5], 0.35*(1-k))
		}
	}
}

func drawLootColumn(p *pixelfx.Painter, C []uint32, t, T float64) {
	ph := t / T
	beat := 0.0
	for k := range len(lootRings) {
		s := lootSince(ph, k) * T
		b1 := 0.0
		if s < 0.5 {
			b1 = math.Exp(-s * 7)
		}
		beat = math.Max(beat, math.Max(b1, math.Exp(-((T-s)*30))))
	}
	gq := (ph - 0.22) / 0.42
	gh := -100.0
	if gq > 0 && gq < 1 {
		gh = 4 + (lootH-4)*pixelfx.Ease.InQuad(gq)
	}
	shimmer := math.Floor(ph * 24)
	for y := -1; y >= -lootH; y-- {
		fy := float64(y)
		h := -fy - 0.5
		u := h / lootH
		fade := 1 - pixelfx.Smooth(0.25, 1, u)
		w := (4.2*(1-0.45*u) + 3.2*math.Exp(-h/7)) * (1 + 0.2*beat)
		seg := math.Floor(h/3 + pixelfx.Rand(math.Floor(h/9), 5)*2)
		flick := math.Floor(ph * 12)
		if u > 0.66 && pixelfx.Rand(seg, flick, 7) > 1.75-u*1.65 {
			continue
		}
		thread := u > 0.58 && pixelfx.Rand(seg, flick, 9) < (u-0.58)*3
		climb := 0
		if pixelfx.Wrap(h/30-ph*3, 1) < 0.1 {
			climb = 1
		}
		trail := 0.0
		if gh-h > 0 && gh-h < 18 {
			trail = 1 - (gh-h)/18
		}
		X := int(math.Ceil(w * 1.6))
		if thread {
			X = 0
		}
		for x := -X; x <= X; x++ {
			fx := float64(x)
			q := math.Abs(fx) / w
			cw := 0.2 + 0.18*(1-pixelfx.Smooth(0, 0.5, u))
			var lvl int
			switch {
			case q < cw:
				lvl = 0
			case q < 0.47:
				lvl = 1
			case q < 0.72:
				lvl = 2
			case q < 1:
				lvl = 3
			default:
				lvl = 4
			}
			for s := range 3 {
				fs := float64(s)
				sx := (fs-1)*w*0.55 + math.Sin(h*0.09+ph*tau*(fs+1)+fs*2)*0.9
				if math.Abs(fx-sx) < 0.5 {
					lvl = max(0, lvl-1)
				}
			}
			if pixelfx.Rand(fy, shimmer, 3) < 0.05 && lvl > 0 {
				lvl--
			}
			if q < 0.72 {
				tOff := 0
				if trail > 0.3 {
					tOff = 1
				}
				lvl = max(0, lvl-climb-tOff)
			}
			a := 1.0
			if lvl > 2 {
				a = ((1.55 - q) / 0.75) * fade * 1.2
			}
			if a <= 0 {
				continue
			}
			var col uint32
			switch {
			case thread:
				col = pick(u > 0.85, C[6], pixelfx.White)
			case lvl == 0:
				col = pick(u > 0.8, C[6], pixelfx.White)
			default:
				col = C[7-lvl]
			}
			p.Dot(fx, fy, col, a)
		}
		near := pixelfx.Smooth(8, 20, h)
		if (y & 3) == 0 {
			p.Glow(0.5, fy, w*2.6+6, C[5], 0.36*near*(0.3+0.7*fade)*(1+0.4*beat))
		}
		if (y & 15) == 0 {
			p.Glow(0.5, fy, 34, C[4], 0.2*near*(0.25+0.75*fade))
		}
	}
	if gh > 0 {
		glint(p, 0, -gh, 3.5*(1-gq*0.5), C[6], 0.6)
		p.Glow(0.5, -gh, 14, C[5], 0.45)
	}
	pixelfx.Flare(p, 0.5, -5, 8+3*beat+math.Sin(ph*tau*4)*0.8, 0.6+0.4*beat, C)
	p.Glow(0.5, -16, 16, C[4], 0.12+0.25*beat)
}

func drawLootMotes(p *pixelfx.Painter, C []uint32, t, T, seed float64) {
	pixelfx.Cycle(t, T, 30, seed, func(g *pixelfx.Rng) float64 { return g.Range(1.0, 2.2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
		inner := r.Chance(0.3)
		side := r.Sign()
		var x0 float64
		if inner {
			x0 = r.Range(-3, 3)
		} else {
			x0 = side * r.Range(4, 15)
		}
		vy := r.Range(22, 52) * pick(inner, 1.5, 1.0)
		x := x0 + math.Sin(age*r.Range(1.5, 3.5)+r.Range(0, tau))*1.6
		y := -r.Range(1, 10) - vy*age
		k := math.Min(1, math.Min(age/0.25, (life-age)/0.5))
		if k <= 0 || y < -lootH {
			return
		}
		if r.Chance(0.22) {
			tw := 0.5 + 0.5*math.Sin(age*r.Range(9, 14)+r.Range(0, tau))
			if tw*k > 0.3 {
				glint(p, x, y, pick(tw > 0.75, 2.0, 1.0), C[6], 0.4*k)
			}
			return
		}
		p.Dot(x, y, pick(inner, pixelfx.White, C[6]), k*1.5)
		p.Dot(x, y+1, pick(inner, C[6], C[4]), k)
		p.Dot(x, y+2, C[3], k*0.6)
		p.Glow(x+0.5, y+0.5, 3.5, C[5], 0.35*k)
	})
}

func newLootBeam(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	T := float64(lootBeamDuration)
	L := newLayer(40, 24)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / T
			pool(p, 0.5, 0, 34, 5, C[4], 0.14)
			p.Shade(-30, -0.5, 31, 6, func(x, y float64, out *pixelfx.Out) uint32 {
				dx, dy := (x-0.5)/30, y/6
				d := dx*dx + dy*dy
				if d >= 1 {
					return 0
				}
				f := 1 - math.Sqrt(d)
				out.L = C[5]
				out.K = 0.4 * f * f
				return 0
			})
			for x := -16; x <= 16; x++ {
				fx := float64(x)
				q := math.Abs(fx) / 16.5
				a := 1.35 - q*1.25
				col0 := C[5]
				if q < 0.3 {
					col0 = pixelfx.White
				} else if q < 0.6 {
					col0 = C[6]
				}
				p.Dot(fx, 0, col0, a)
				if q < 0.75 {
					col1 := C[4]
					if q < 0.3 {
						col1 = C[6]
					} else if q < 0.55 {
						col1 = C[5]
					}
					p.Dot(fx, 1, col1, a*0.8)
				}
				if q < 0.4 {
					p.Dot(fx, 2, C[4], a*0.5)
				}
			}
			for k := range len(lootRings) {
				drawLootRing(p, C, lootSince(ph, k)*T, true)
			}
			drawLootColumn(p, C, t, T)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / T
			blobShadow(p, lootOX-0.5, 12.5, 0.95, 0)
			drawLootSword(L, p, C, ph)
			for k := range len(lootRings) {
				drawLootRing(p, C, lootSince(ph, k)*T, false)
			}
			drawLootMotes(p, C, t, T, seed)
		},
	}
}
