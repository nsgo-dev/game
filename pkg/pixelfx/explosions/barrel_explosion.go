// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/barrel-explosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The blast (fire puffs that cool into smoke, sparks, embers, burning brands that land and keep burning, grit, scorch, rings) is adapted from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// BarrelExplosion is an explosive barrel that shudders, its lid bulging and spitting flame, then blows: a fireball column, staves and the lid flying, burning fuel splashed over the floor. It is registered as "explosions/barrel-explosion".
var BarrelExplosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Barrel explosion",
		Desc:     "An explosive barrel shudders, its lid bulging and spitting flame, then blows: a fireball column, staves and the lid flying, burning fuel splashed over the floor.",
		Tags:     []string{"explosion", "barrel", "fire", "smoke", "debris", "ground", "one-shot"},
		Size:     [2]int{barrelW, barrelH},
		Origin:   [2]int{barrelOX, barrelOY},
		Duration: barrelDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "barrel", Default: pixelfx.Palette("#1c0a0e", "#4a1218", "#7c1a1e", "#b02a26", "#d8483a", "#f08a70")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newBarrelExplosion,
}

const barrelW, barrelH, barrelOX, barrelOY = 300, 240, 150, 204

func init() { pixelfx.Register("explosions/barrel-explosion", BarrelExplosion) }

// variables, so sums of them round as pixelfx's do
var barrelDuration, barrelBlow, barrelBW, barrelBH = 5.2, 0.8, 8.0, 22.0

var (
	barrelOil   = pixelfx.Hex("#221a22")
	barrelSoot  = pixelfx.Hex("#1f1a24")
	barrelPaint = []uint32{pixelfx.Hex("#e8d8b0"), pixelfx.Hex("#b8a888"), pixelfx.Hex("#6e6050")}
)

// barrelLid is the lid's flight.
type barrelLid struct {
	x, y, vx, vy, a, va float64
	on, rest            bool
}

// fuelPool is a splash of burning fuel on the floor.
type fuelPool struct{ x, w float64 }

func newBarrelExplosion(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 8)
	FR := fireRamp(F)
	HR, C, R := heatRamp(FR), pixelfx.Fit(o.Ramp("barrel"), 6), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	const W, H, OX, OY = barrelW, barrelH, barrelOX, barrelOY
	const X0, X1, Y0, Y1 = -OX + 3, W - OX - 3, -OY + 3, H - OY - 3
	BH, D := barrelBH, barrelDuration
	var blazes *pixelfx.Blazes
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[6], F[5], F[3], F[2]}, OnLand: func(x, y float64) {
		blazes.Add(x, y-0.25, R.Range(1.2, 2.2), R.Range(0.6, 1))
	}})
	blazes = pixelfx.NewBlazes(ps)
	lights, staves := &pixelfx.Lights{}, pixelfx.NewBits(floor)
	lid := barrelLid{y: -BH}
	// the fuel splashed over the floor: a puddle and the spots that keep burning in it
	var pools []fuelPool
	t, blown := 0.0, false
	ringCols := []uint32{groundRing[0], groundRing[1], F[3], F[6]}

	blow := func() {
		blown = true
		y := -BH * 0.5
		lights.Add(0, y-20, 90, pixelfx.Mix(F[3], F[6], 0.45), 0.85, 0.9)
		lights.Add(0, y, 44, pixelfx.White, 0.7, 0.09)
		// the fireball: bursting out, then a column of fire driven up out of the drum
		for range 5 {
			ps.Spawn(pixelfx.Fire, R.Range(-3, 3), y+R.Range(-3, 3), R.Range(-20, 20), R.Range(-40, -10), R.Range(0.18, 0.28), 0, R.Range(9, 12), 0, 0)
		}
		for range 30 {
			a, d, sp := R.Range(-pi-0.15, 0.15), R.Range(0, 8), R.Range(50, 190)
			ps.Spawn(pixelfx.Fire, math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*sp, math.Sin(a)*sp*0.8-40, R.Range(0.55, 1.05), 0, R.Range(5, 10), 0, chance(R, 0.5, pixelfx.Smokes|pixelfx.Big))
		}
		for range 12 {
			ps.Spawn(pixelfx.Fire, R.Range(-6, 6), y-R.Range(0, 10), R.Range(-14, 14), -R.Range(170, 300), R.Range(0.7, 1.15), 0, R.Range(6, 10), R.Range(0, 0.12), chance(R, 0.7, pixelfx.Smokes|pixelfx.Big))
		}
		// oily black smoke
		for range 12 {
			a, d := R.Range(-pi, 0), R.Range(4, 16)
			ps.Spawn(pixelfx.Smoke, math.Cos(a)*d, y+math.Sin(a)*d, math.Cos(a)*30, -R.Range(30, 90), R.Range(1.6, 2.6), barrelSoot, R.Range(8, 13), R.Range(0.1, 0.4), pixelfx.Big)
		}
		for range 90 {
			a, sp := R.Range(0, tau), R.Range(160, 400)
			ps.Spawn(pixelfx.Spark, 0, y, math.Cos(a)*sp, math.Sin(a)*sp-70, R.Range(0.2, 0.6), pixelfx.White, 1, 0, 0)
		}
		for range 40 {
			a, sp := R.Range(0, tau), R.Range(30, 140)
			ps.Spawn(pixelfx.Ember, math.Cos(a)*8, y+math.Sin(a)*8, math.Cos(a)*sp, math.Sin(a)*sp-70, R.Range(1.2, 2.6), F[3], 1, 0, 0)
		}
		// burning fuel flung out in arcs: it lands and keeps burning
		for range 14 {
			a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(120, 300)
			ps.Spawn(pixelfx.Brand, R.Range(-4, 4), y, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.8, 1.4), pixelfx.White, 1, 0, 0)
		}
		// the staves: the drum's plates torn off and thrown out, tumbling (each one's heat in its Data)
		for i := range 9 {
			side := pick(i%2 != 0, 1.0, -1)
			a, sp := -pi/2+side*R.Range(0.25, 1.15), R.Range(170, 330)
			var b pixelfx.Bit
			b.Kind = pixelfx.Chunk
			b.X = side * R.Range(2, 6)
			b.Y = -R.Range(4, BH-4)
			b.VX, b.VY = math.Cos(a)*sp, math.Sin(a)*sp
			b.G, b.Drag, b.Bounce = 1, 0.5, 0.35
			b.Life = R.Range(3, 3.8)
			b.A = R.Range(0, tau)
			b.VA = R.Range(-16, 16)
			b.S = R.Range(9, 13)
			b.Data = R.Range(0.4, 0.9)
			staves.Add(b)
		}
		// the lid, flung high and spinning
		lid.on, lid.x, lid.y = true, 0, -BH
		lid.vx = R.Range(-40, 40)
		lid.vy = -R.Range(430, 480)
		lid.a = 0
		lid.va = R.Sign() * R.Range(10, 14)
		// the fuel on the floor, and the floor's own dust and grit
		for range 7 {
			x := R.Range(-34, 34)
			pools = append(pools, fuelPool{x, R.Range(5, 10)})
			blazes.Add(x, -0.25, R.Range(2.2, 3), R.Range(0.8, 1.3))
		}
		for range 12 {
			s := R.Sign()
			ps.Spawn(pixelfx.Smoke, s*R.Range(0, 10), -R.Range(1, 4), s*R.Range(80, 220), -R.Range(2, 12), R.Range(1.1, 1.8), groundDust, R.Range(6, 9), R.Range(0, 0.05), pixelfx.Big)
		}
		for range 60 {
			u := R.Range(-1, 1)
			a, sp := -pi/2+u*0.7, R.Range(120, 330)*(1-0.4*math.Abs(u))
			ps.Spawn(pixelfx.Grit, u*6, -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.1, 1.9), pixelfx.Pick(R, groundGrit), pick(R.Chance(0.4), 2.0, 1), 0, 0)
		}
	}

	band := func(p *pixelfx.Painter, keep func(float64) bool) {
		if k := (t - barrelBlow) / 0.45; k < 1 {
			pixelfx.Band(p, 0, 0.5, 100*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 4.5*(1-k)), 1.15-k, ringCols, keep, false)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !blown {
				// the drum strains: flame and smoke spitting from the seam under the lid, quicker as it goes
				k := t / barrelBlow
				if R.Chance((6 + 30*k*k) * dt) {
					s := R.Sign()
					ps.Spawn(pixelfx.Fire, s*R.Range(4, 7), -BH+1, s*R.Range(20, 60), -R.Range(20, 60), R.Range(0.15, 0.3), 0, R.Range(1.5, 3), 0, 0)
					ps.Spawn(pixelfx.Spark, s*6, -BH+1, s*R.Range(40, 140), -R.Range(20, 120), R.Range(0.1, 0.25), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(10 * dt) {
					ps.Spawn(pixelfx.Smoke, R.Range(-5, 5), -BH, R.Range(-8, 8), -R.Range(15, 35), R.Range(0.8, 1.3), 0, R.Range(2.5, 4), 0, 0)
				}
				if t >= barrelBlow {
					blow()
				}
			}
			if lid.on && !lid.rest {
				lid.vy += 950 * dt
				lid.vx -= lid.vx * 0.4 * dt
				lid.x += lid.vx * dt
				lid.y += lid.vy * dt
				lid.a += lid.va * dt
				if lid.y >= -1 && lid.vy > 0 {
					// a clank: sparks and dust where it lands, then it bounces flat and settles
					for range 6 {
						ps.Spawn(pixelfx.Spark, lid.x, -1, R.Range(-90, 90), -R.Range(40, 140), R.Range(0.1, 0.25), pixelfx.White, 1, 0, 0)
					}
					for range 5 {
						ps.Spawn(pixelfx.Dust, lid.x+R.Range(-4, 4), -0.5, R.Sign()*R.Range(15, 60), -R.Range(4, 18), R.Range(0.35, 0.7), groundDust, R.Range(1.8, 3), 0, 0)
					}
					lid.y = -1
					if lid.vy > 160 {
						lid.vy *= -0.3
						lid.vx *= 0.5
						lid.va *= 0.5
					} else {
						lid.rest, lid.a = true, pi/2
					}
				}
			}
			staves.Update(dt)
			for i := len(staves.List) - 1; i >= 0; i-- {
				b := &staves.List[i]
				if b.X < X0+8 || b.X > X1-8 {
					staves.List = slices.Delete(staves.List, i, i+1)
				} else if h := heatOf(b); h > 0 {
					b.Data = math.Max(0, h-dt*0.35)
				}
			}
			blazes.Update(dt)
			ps.Update(dt)
			cull(ps, X0, Y0, X1, Y1)
			fadeEdges(ps, X0, X1, 24, Y1)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if blown {
				u := t - barrelBlow
				out := 1 - pixelfx.Smooth(D-barrelBlow-1, D-barrelBlow-0.2, u)
				scorch(p, HR, u, 30, 2.2, out, 3)
				// the spilled fuel: a dark slick on the floor, lit by its own fire, shrinking as it burns off
				if left := 1 - pixelfx.Smooth(1.6, 3.4, u); left > 0 {
					for _, q := range pools {
						for x := math.Floor(-q.w * left); x <= math.Ceil(q.w*left); x++ {
							e := math.Abs(x) / (q.w*left + 0.01)
							c := barrelOil
							if pixelfx.Rand(math.Floor(q.x+x), math.Floor(t*12)) < 0.25 {
								c = F[4]
							}
							p.Dot(q.x+x, 1, c, out*pick(e < 0.8, 1.0, 0.6))
							if e < 0.6 {
								p.Dot(q.x+x, 2, barrelOil, out*0.8)
							}
						}
					}
				}
				band(p, farHalf)
			}
			drawPs(p, ps, pixelfx.LayerBack, FR)
			blazes.DrawBack(p)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if !blown {
				// the barrel, shaking harder, its lid bulging
				k := t / barrelBlow
				jx, jy := math.Sin(t*70)*k*k*1.4, math.Abs(math.Sin(t*53))*k*k*0.8
				drawBarrel(p, C, jx, -jy, k)
				p.Glow(0, -BH, 10+8*k, F[4], 0.25+0.35*k*k*(0.6+0.4*math.Sin(t*40)))
			} else {
				band(p, nearHalf)
			}
			drawPs(p, ps, pixelfx.LayerFront, FR)
			for i := range staves.List {
				stave(p, &staves.List[i], C, FR)
			}
			if lid.on {
				drawLid(p, C, &lid, math.Min(1, (D-t)/0.4))
			}
			if blown {
				u := t - barrelBlow
				if u < 0.08 {
					k2 := 1 - u/0.08
					r, y := 18*(0.85+0.15*k2), -BH*0.5
					p.Circle(0, y, r+1.4, F[2], 1)
					p.Circle(0, y, r, pick(k2 > 0.45, F[6], F[5]), 1)
					p.Circle(0, y, r*0.6, pick(k2 > 0.45, pixelfx.White, F[6]), 1)
				}
				if u < 0.22 {
					pixelfx.Flare(p, 0, -BH*0.5, 54*math.Pow(1-u/0.22, 1.4), 1-u/0.22, F)
				}
				// the burning fuel lights the floor
				pool(p, 0, 1, 90, 14, F[3], 0.5*math.Max(0, 1-u/0.7)+0.22*(1-pixelfx.Smooth(1.6, 3.4, u)))
			}
			lights.Draw(p)
		},
	}
}

// drawBarrel draws a steel drum standing on the floor at x: red, two ribs, a lit left side, a dark right side, an outline, a pale band; the lid bulges with k.
func drawBarrel(p *pixelfx.Painter, C []uint32, x, y, k float64) {
	BW, BH := barrelBW, barrelBH
	bulge := pixelfx.Round(k * k * 2)
	p.Shade(x-BW-1, y-BH-3, x+BW+1, y+0.5, func(px, py float64, _ *pixelfx.Out) uint32 {
		dx, v := (px-x)/BW, (y-py)/BH // dx -1..1 across, v 0 at the floor .. 1 at the top
		if math.Abs(dx) > 1.1 || v < 0 || v > 1.05 {
			return 0
		}
		if math.Abs(dx) > 0.92 || v > 0.97 || v < 0.04 {
			return outline
		}
		rib := math.Abs(v-0.33) < 0.035 || math.Abs(v-0.67) < 0.035
		ribLit := math.Abs(v-0.31) < 0.03 || math.Abs(v-0.65) < 0.03
		band := v > 0.44 && v < 0.56
		lit := -dx // light from the left
		var i int
		switch {
		case lit > 0.55:
			i = 5
		case lit > 0.15:
			i = 4
		case lit > -0.35:
			i = 3
		case lit > -0.75:
			i = 2
		default:
			i = 1
		}
		if band {
			switch {
			case lit > 0.3:
				return barrelPaint[0]
			case lit > -0.4:
				return barrelPaint[1]
			}
			return barrelPaint[2]
		}
		if rib {
			i = max(1, i-2)
		} else if ribLit {
			i = min(5, i+1)
		}
		return C[i]
	})
	// the lid: a dark ellipse with a lit rim, pushed up as the drum swells
	p.EllipseFn(x, y-BH-bulge*0.5, BW-0.5, 2.2+bulge*0.4, func(d, a float64) (uint32, float64) {
		if d > 0.7 {
			return pick(math.Sin(a) < 0, C[4], C[2]), 1
		}
		return C[1], 1
	})
}

// stave draws a curved plate torn from the drum, its painted face and its dark inside showing as it turns; hot edges glow.
func stave(p *pixelfx.Painter, b *pixelfx.Bit, C, FR []uint32) {
	fade, ca, sa, L := math.Min(1, b.Life/0.5), math.Cos(b.A), math.Sin(b.A), b.S
	face, heat := math.Cos(b.A*1.7) > 0, heatOf(b)
	for d := -L / 2; d <= L/2; d += 0.5 {
		q := 2 * d / L
		bow := (1 - q*q) * 1.2
		x, y := b.X+ca*d-sa*bow, b.Y+sa*d+ca*bow
		hot := heat > 0.3 && math.Abs(d) > L/2-1
		p.Dot(x+sa, y-ca, outline, fade)
		c := pick(face, C[4], C[1])
		if hot {
			c = FR[pick(heat > 0.6, 3, 5)]
		}
		p.Dot(x, y, c, fade)
		p.Dot(x-sa, y+ca, pick(face, C[2], outline), fade)
	}
	if heat > 0.3 {
		p.Glow(b.X, b.Y, 6, FR[4], 0.25*heat*fade)
	}
}

// drawLid draws the lid: a disc turning as it flies (an ellipse whose width follows its spin), lying flat once it lands.
func drawLid(p *pixelfx.Painter, C []uint32, lid *barrelLid, a float64) {
	w, h := math.Max(0.8, math.Abs(math.Cos(lid.a))*7.5), math.Max(1.2, math.Abs(math.Sin(lid.a))*7.5)
	rx, ry, dy := w, h, 0.0
	if lid.rest {
		rx, ry, dy = 7.5, 1.6, 0.5
	}
	p.Ellipse(lid.x, lid.y-dy, rx+0.8, ry+0.8, outline, a)
	p.EllipseFn(lid.x, lid.y-dy, rx, ry, func(d, an float64) (uint32, float64) {
		if d > 0.65 {
			return pick(math.Sin(an) < 0, C[4], C[2]), a
		}
		return C[3], a
	})
}
