// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/spore-burst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the lumpy cloud (puffs that drift off their push, rise, swell from small to large and thin out, lit caps, motes with light) from Sprite Fusion's open-source poison cloud (https://www.spritefusion.com/pixel-art-vfx/poison), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// SporeBurst is a toadstool that swells, trembles as its gills glow, then squashes and puffs a lumpy cloud of glowing spores. It is registered as "nature/spore-burst".
var SporeBurst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spore burst",
		Desc:     "A toadstool pushes up, swells and trembles as its gills glow, then squashes and puffs a lumpy cloud of glowing spores that billows up and drifts down twinkling.",
		Tags:     []string{"nature", "mushroom", "spores", "poison", "cloud", "ground", "one-shot"},
		Size:     [2]int{212, 132},
		Origin:   [2]int{106, 118},
		Duration: sporeBurstDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#06141c", "#0c2a34", "#11484e", "#17706a", "#22a08a", "#4cd0a8", "#a8f4d0", "#f0fff6")},
			{Name: "cap", Default: pixelfx.Palette("#1a0c1e", "#3a1438", "#62204e", "#8e3462", "#bc5276", "#e88a96")},
			{Name: "stem", Default: pixelfx.Palette("#2a2230", "#5a4e5e", "#9a8e98", "#d8ccd0", "#f6eeee")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newSporeBurst,
}

func init() { pixelfx.Register("nature/spore-burst", SporeBurst) }

const sporeBurstDuration = 3.2

var (
	sporeGrow  = 0.32
	sporePuff  = 0.8
	sporeSink  = 2.15
	sporeSpots = [6][3]float64{
		{-0.45, 0.45, 1.4},
		{0.1, 0.72, 1.8},
		{0.55, 0.38, 1.2},
		{-0.12, 0.25, 1},
		{0.3, 0.12, 0.9},
		{-0.7, 0.16, 0.8},
	}
)

// drawToadstool draws the toadstool at its current squash: a stem with a frilled ring, a domed cap lit from the upper left with pale spots, glowing gills beneath it.
func drawToadstool(p *pixelfx.Painter, K, Sm, Cs []uint32, g, sx, sy, glow, sink float64) {
	H := 17 * g
	cw := 17 * sx * g
	ch := 13 * sy * g
	base := sink
	top := base - H
	if g < 0.05 {
		return
	}
	// stem
	for y := math.Floor(top); y <= math.Min(0, base); y++ {
		for x := -4.0; x <= 4; x++ {
			w := 3 + ((y-top)/H)*1
			if math.Abs(x+0.5) > w {
				continue
			}
			q := (x + 0.5 + w) / (2 * w)
			var col uint32
			switch {
			case math.Abs(x+0.5) > w-0.8:
				col = Sm[1]
			case q < 0.4:
				col = Sm[4]
			case q < 0.7:
				col = Sm[3]
			default:
				col = Sm[2]
			}
			p.Px(x, y, col)
		}
	}
	ring := top + H*0.3
	if ring <= 0 {
		for x := -5.0; x <= 5; x++ {
			dy := 0.0
			if math.Abs(x) > 3 {
				dy = 1
			}
			col := Sm[3]
			if math.Abs(x) > 4 {
				col = Sm[1]
			}
			p.Px(x, ring+dy, col)
		}
	}
	// gills: a glowing underside
	gy := top + 1
	if gy <= 0 {
		for x := math.Floor(-cw); x <= math.Ceil(cw); x++ {
			d := math.Abs(x+0.5) / cw
			if d > 0.95 {
				continue
			}
			odd := int(x)&1 != 0
			col := Sm[3]
			if d < 0.3 {
				col = Cs[0]
			} else if odd {
				col = Sm[2]
			}
			p.Px(x, gy, col)
			if glow > 0.05 {
				gcol := K[5]
				if glow > 0.6 && odd {
					gcol = K[7]
				}
				p.Px(x, gy, gcol)
			}
		}
	}
	if glow > 0.05 {
		p.Glow(0, gy+2, cw*1.5+6, K[5], 0.5*glow)
	}
	// the cap: a dome with a curled lip
	p.Shade(-cw-1, top-ch-1, cw+1, top+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x/cw, (top-y)/ch
		d := dx*dx + dy*dy
		if dy < -0.12 || d > 1.12 || y > 0 {
			return 0
		}
		if d > 1 {
			out.C = Cs[0]
			return 0
		}
		var i int
		switch {
		case dx < -0.35 && dy > 0.45:
			i = 5
		case dx < 0.05 && dy > 0.3:
			i = 4
		case dy < 0.18:
			i = 2
		case dx > 0.55:
			i = 2
		default:
			i = 3
		}
		for _, sp := range sporeSpots {
			ex := (dx - sp[0]) * cw
			ey := (dy - sp[1]) * ch
			if ex*ex+ey*ey*1.6 < sp[2]*sp[2]*g*g {
				if ex < 0 && ey > 0 {
					i = 9
				} else {
					i = 8
				}
			}
		}
		switch i {
		case 9:
			out.C = Sm[4]
		case 8:
			out.C = Sm[3]
		default:
			out.C = Cs[i]
		}
		return 0
	})
	p.Px(-cw*0.45, top-ch*0.8, Cs[5])
}

func newSporeBurst(o *pixelfx.Options) pixelfx.Instance {
	K := pixelfx.Fit(o.Palette(), 8)
	Cs := pixelfx.Fit(o.Ramp("cap"), 6)
	Sm := pixelfx.Fit(o.Ramp("stem"), 5)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{K[7], K[6], K[5], K[4]}, Haze: K[5]})
	gCloud := &gas{}
	lights := &pixelfx.Lights{}
	t := 0.0
	puffed, grown := false, false
	g := func() float64 {
		return pixelfx.OutBack(pixelfx.Clamp(t/sporeGrow), 1.6) * (1 - pixelfx.Ease.InCubic(pixelfx.Clamp((t-sporeSink)/0.45)))
	}
	charge := func() float64 { return pixelfx.Smooth(sporeGrow, sporePuff, t) }
	// squash and stretch: it swells as it charges, squashes flat as it puffs, springs back
	shape := func() (float64, float64) {
		if t < sporePuff {
			k := charge()
			j := math.Sin(t*70) * 0.04 * k
			return 1 + 0.28*k + j, 1 + 0.22*k - j
		}
		e := t - sporePuff
		sp := math.Exp(-e*7) * math.Cos(e*26)
		return 1 + 0.25*sp, 1 - 0.35*sp
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			if !grown {
				grown = true
				for range 10 {
					px := R.Range(-4, 4)
					vx := R.Range(-50, 50)
					vy := -R.Range(40, 120)
					life := R.Range(0.5, 0.8)
					c := pixelfx.Pick(R, []uint32{Sm[1], Sm[2]})
					ps.Spawn(pixelfx.Grit, px, -1, vx, vy, life, c, 1, 0, 0)
				}
				for range 4 {
					px := R.Range(-4, 4)
					vx := R.Sign() * R.Range(10, 40)
					vy := -R.Range(4, 12)
					life := R.Range(0.4, 0.7)
					sz := R.Range(2, 3)
					ps.Spawn(pixelfx.Dust, px, -1, vx, vy, life, Sm[1], sz, 0, 0)
				}
			}
			// charging: spores gather toward the gills
			if t > sporeGrow && t < sporePuff-0.05 && R.Chance(0.25*f) {
				a := R.Range(pi*1.05, pi*1.95)
				d := R.Range(20, 38)
				x := math.Cos(a) * d
				y := -18 + math.Sin(a)*d*0.7
				life := R.Range(0.2, 0.3)
				ps.Spawn(pixelfx.Streak, x, y, -x*3, (-18-y)*3, life, K[6], 1, 0, 0)
			}
			if !puffed && t >= sporePuff {
				puffed = true
				lights.Add(0, -22, 30, pixelfx.White, 0.6, 0.1)
				lights.Add(0, -28, 70, K[5], 0.55, 0.9)
				// the cloud: lumps shoved up and out from under the cap, swelling as they go
				for range 30 {
					a := -pi/2 + R.Range(-1.1, 1.1)
					sp := R.Range(15, 95)
					gx := R.Range(-8, 8)
					gy := -20 + R.Range(-3, 3)
					r0 := R.Range(3, 5)
					r1 := R.Range(8, 14)
					end := t + R.Range(1.6, 2.2)
					rise := R.Range(5, 10)
					gCloud.add(t, gx, gy, math.Cos(a)*sp*1.3, math.Sin(a)*sp-10, r0, r1, end, rise)
				}
				for range 6 {
					sd := R.Sign()
					gx := sd * R.Range(6, 12)
					vx := sd * R.Range(30, 70)
					vy := -R.Range(2, 14)
					r0 := R.Range(2, 3)
					r1 := R.Range(4, 6)
					end := t + R.Range(0.8, 1.1)
					rise := R.Range(4, 8)
					gCloud.add(t, gx, -5, vx, vy, r0, r1, end, rise)
				}
				for range 40 {
					a := -pi/2 + R.Range(-1.4, 1.4)
					sp := R.Range(40, 160)
					mote := R.Chance(0.5)
					k := 0.7
					kind := pixelfx.Flake
					if mote {
						k = 0.35
						kind = pixelfx.Mote
					}
					px := R.Range(-5, 5)
					life := R.Range(1, 2)
					c := pixelfx.Pick(R, []uint32{K[7], K[6], K[6], K[5]})
					sz := R.Range(0.6, 1.6)
					ps.Spawn(kind, px, -20, math.Cos(a)*sp*k, math.Sin(a)*sp*k, life, c, sz, 0, 0)
				}
				for range 6 {
					px := R.Range(-20, 20)
					py := -R.Range(12, 40)
					life := R.Range(0.3, 0.6)
					sz := R.Range(1, 2.4)
					delay := R.Range(0.05, 0.4)
					ps.Spawn(pixelfx.StarKind, px, py, 0, -6, life, K[7], sz, delay, 0)
				}
				for range 8 {
					px := R.Sign() * R.Range(4, 10)
					vx := R.Sign() * R.Range(30, 90)
					vy := -R.Range(2, 10)
					life := R.Range(0.5, 0.9)
					sz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, px, -1, vx, vy, life, K[3], sz, 0, 0)
				}
			}
			// after it: spores keep drifting down out of the cloud, twinkling
			if puffed && t < 2.6 && R.Chance(0.12*f) {
				px := R.Range(-40, 40)
				py := -R.Range(30, 70)
				vx := R.Range(-8, 8)
				vy := R.Range(4, 14)
				life := math.Min(R.Range(0.9, 1.5), sporeBurstDuration-0.05-t)
				c := K[6]
				if R.Chance(0.5) {
					c = K[7]
				}
				sz := R.Range(0.6, 1.5)
				ps.Spawn(pixelfx.Flake, px, py, vx, vy, life, c, sz, 0, 0)
			}
			if t > sporeSink && t < sporeSink+0.4 && R.Chance(0.15*f) {
				px := R.Range(-5, 5)
				vx := R.Sign() * R.Range(10, 30)
				vy := -R.Range(2, 8)
				life := R.Range(0.3, 0.5)
				sz := R.Range(1.5, 2.5)
				ps.Spawn(pixelfx.Dust, px, -1, vx, vy, life, Sm[1], sz, 0, 0)
			}
			gCloud.prune(t)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a ring of glow on the floor round it while it charges and puffs
			k := charge() * (1 - pixelfx.Smooth(sporePuff+0.2, sporePuff+1.2, t))
			if k > 0 {
				for x := -20.0; x <= 20; x++ {
					col := K[4]
					if math.Abs(x) < 10 {
						col = K[5]
					}
					p.Dot(x, 0, col, k*(1-math.Abs(x)/21))
				}
			}
			p.Glow(0, -2, 30, K[4], 0.25*k)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			sx, sy := shape()
			var gl float64
			if t < sporePuff {
				ch := charge()
				gl = ch * (0.75 + 0.25*math.Sin(t*(20+30*ch)))
			} else {
				gl = math.Max(0, 1-(t-sporePuff)*1.5)
			}
			drawToadstool(p, K, Sm, Cs, g(), sx, sy, gl, pixelfx.Ease.InCubic(pixelfx.Clamp((t-sporeSink)/0.45))*20)
			// the cloud: teal lumps lit along their tops, a cool light through them
			gCloud.draw(p, t, K[2], K[4], K[6], K[7], 0.62, K[6])
			// the cloud glows from within: a soft light riding its middle
			if len(gCloud.list) > 0 {
				cx, cy, n := 0.0, 0.0, 0.0
				for _, q := range gCloud.list {
					g2 := gCloud.at(q, t)
					cx += g2.x * g2.a
					cy += g2.y * g2.a
					n += g2.a
				}
				if n > 0.2 {
					p.Glow(cx/n, cy/n, 46, K[5], math.Min(0.32, 0.03*n))
				}
			}
			e := t - sporePuff
			if e > 0 && e < 0.1 {
				pixelfx.Sparkle(p, 0, -20, 10*(1-e/0.1), K[7], K[6], 0.6)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
