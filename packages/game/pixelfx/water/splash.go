// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/splash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the droplet recipe (shaded beads with tails that fly, land and throw up a little crown of their own) from Sprite Fusion's open-source blood (https://www.spritefusion.com/pixel-art-vfx/blood), MIT License, Copyright (c) 2026 Hugo Duprez.

package water

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Splash drops a stone into still water: a white hit, a crown that tears into droplets arcing out to plink down with rings, a jet that bounces back, foam and drifting mist. It is registered as "water/splash".
var Splash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Splash",
		Desc:     "A stone drops into still water: a white hit, a crown that tears into droplets arcing out to plink down with rings, a jet that bounces back, foam and drifting mist.",
		Tags:     []string{"water", "splash", "impact", "drop", "liquid", "one-shot"},
		Size:     [2]int{176, 100},
		Origin:   [2]int{88, 78},
		Duration: 2.5,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "stone", Default: pixelfx.Bool(true)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newSplash,
}

func init() { pixelfx.Register("water/splash", Splash) }

const (
	splashSY      = 0.22
	splashHit     = 0.16
	splashG       = 820
	splashFingers = 12
	splashJet     = 0.3
	splashTear    = 0.11
)

func splashHeight(u float64) float64 {
	return 19 * pixelfx.Ease.OutCubic(pixelfx.Clamp(u/0.09)) * (1 - pixelfx.Ease.InQuad(pixelfx.Clamp((u-0.12)/0.3)))
}

func splashFoot(u float64) float64 {
	return 3 + 7.5*pixelfx.Ease.OutCubic(pixelfx.Clamp(u/0.45))
}

func splashLip(u float64) float64 {
	return splashFoot(u) + splashHeight(u)*0.42 + 3*pixelfx.Ease.OutCubic(pixelfx.Clamp(u/0.25))
}

func splashJetH(u float64) float64 {
	return 26 * (pixelfx.Ease.OutCubic(pixelfx.Clamp(u/0.12)) - pixelfx.Ease.InQuad(pixelfx.Clamp((u-0.15)/0.28)))
}

type splashSpike struct{ a, L float64 }

type splashBubble struct {
	x, z, t0, r float64
	popped      bool
}

type crownCell struct {
	x, y float64
	c    uint32
	a    float64
	rank int
}

var splashGlugs = [3]struct {
	i  int
	at float64
}{{0, 0.95}, {1, 1.2}, {2, 1.5}}

func newSplash(o *pixelfx.Options) pixelfx.Instance {
	C, R, ST := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), rock()
	seed, showStone := o.Seed(), o.Bool("stone")
	phi := R.Range(0, tau)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R})
	ms := &mist{}
	lights := &pixelfx.Lights{}
	rings := newRipples(splashSY)
	var plinks []waterPlink
	var bubbles []splashBubble
	spikes := make([]splashSpike, splashFingers)
	for k := range splashFingers {
		spikes[k] = splashSpike{a: math.Mod((float64(k)*tau)/splashFingers+phi, tau), L: R.Range(0.2, 0.36)}
	}
	var ds *drops
	ds = newDrops(splashSY, splashG, 0.35, func(d *drop) {
		// every drop that comes down plinks: a white fleck, a ring, and the bigger ones throw up a tiny crown
		rings.add(d.x, d.z, 0.6, 2+d.s*1.8, 0.34+d.s*0.1, rippleOpts{n: 1, k: 0.95, hot: 0.06})
		plinks = append(plinks, waterPlink{x: d.x, y: d.z * splashSY, s: d.s, age: 0})
		if d.s > 1 && !d.small {
			for i := 0.0; i < d.s; i++ {
				a, v := R.Range(0, tau), R.Range(10, 34)
				ds.add(drop{x: d.x, z: d.z, h: 0.5, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: R.Range(55, 110), s: 1, small: true, life: 1})
			}
		}
	})
	t := 0.0
	done := map[string]bool{}
	once := func(key string, at float64) bool {
		if !done[key] && t >= at {
			done[key] = true
			return true
		}
		return false
	}
	scallop := func(a, h float64) float64 {
		return h * (0.86 + 0.14*pixelfx.Noise(math.Cos(a)*2.2+math.Sin(a)*1.3+phi*3, seed))
	}

	// crown draws the crown: a thin sheet from its foot to a rolled lip, with fingers rising off the lip.
	crown := func(p *pixelfx.Painter, back bool) {
		u := t - splashHit
		h := splashHeight(u)
		if u < 0 || h < 0.7 {
			return
		}
		r0, rt, white := splashFoot(u), splashLip(u), u < 0.045
		cells := make(map[int]int)
		var list []crownCell
		put := func(x, y float64, c uint32, a float64, rank int) {
			X, Y := math.Floor(x), math.Floor(y)
			key := (int(Y)+4096)*8192 + int(X) + 4096
			if idx, ok := cells[key]; !ok {
				cells[key] = len(list)
				list = append(list, crownCell{x: X, y: Y, c: c, a: a, rank: rank})
			} else if rank > list[idx].rank {
				list[idx] = crownCell{x: X, y: Y, c: c, a: a, rank: rank}
			}
		}
		n := int(math.Ceil(rt * tau * 1.6))
		for i := 0; i < n; i++ {
			a := (float64(i) / float64(n)) * tau
			s, c := math.Sin(a), math.Cos(a)
			if (s < 0) != back {
				continue
			}
			lane := math.Floor(((a + phi) * rt) / 2.2)
			streak := pixelfx.Rand(lane, 31) < 0.3
			H := scallop(a, h)
			// as it falls, each lane of the sheet gives way from the top at its own moment
			keep := 1 - pixelfx.Clamp((u-0.13-0.14*pixelfx.Rand(lane, 7))/0.1)
			steps := math.Ceil(H*1.4) + 2
			for k := 0.0; k <= steps*keep; k++ {
				v := k / steps
				r := r0 + (rt-r0)*math.Pow(v, 2.2)
				x, y := c*r, s*r*splashSY-H*v
				switch {
				case white:
					put(x, y, pixelfx.White, 1, 3)
				case back:
					col := C[2]
					if v > 0.72 {
						col = C[4]
						if streak {
							col = C[5]
						}
					} else if streak {
						col = C[3]
					}
					put(x, y, col, 1, 1)
				case math.Abs(c) > 0.9:
					col := C[4]
					if c < 0 {
						col = C[6]
						if math.Abs(c) > 0.97 {
							col = pixelfx.White
						}
					}
					put(x, y, col, 1, 2)
				case v < 0.1:
					col := C[6]
					if v < 0.05 {
						col = pixelfx.White
					}
					put(x, y, col, 1, 2)
				case streak:
					col := C[5]
					if c < -0.2 {
						col = C[6]
					}
					put(x, y, col, 1, 1)
				default:
					col := C[4]
					if c < 0 {
						col = C[5]
					}
					put(x, y, col, 0.22, 0)
				}
			}
			// the rolled lip: a hard white line on the near side, softer on the far, gone where the lane broke
			if !white && keep >= 1 {
				x, y := c*rt, s*rt*splashSY-H
				col := C[6]
				if back {
					col = C[5]
				} else if c < 0.45 {
					col = pixelfx.White
				}
				put(x, y, col, 1, 3)
				if !back && c < 0.2 {
					put(x, y+1, C[6], 1, 2)
				}
			}
		}
		for _, q := range list {
			if q.rank == 0 {
				p.Blend(q.x, q.y, q.c, q.a)
			} else {
				p.Px(q.x, q.y, q.c)
			}
		}
		// fingers: spikes running on up and out from the lip, a bead on each until it tears off
		reach := 1 - pixelfx.Smooth(splashTear, splashTear+0.1, u)
		for _, f := range spikes {
			if (math.Sin(f.a) < 0) != back || reach <= 0 {
				continue
			}
			H := scallop(f.a, h)
			L := h * f.L * reach
			x0, y0 := math.Cos(f.a)*rt, math.Sin(f.a)*rt*splashSY-H
			x1, y1 := x0+math.Cos(f.a)*L*0.45, y0-L
			if L >= 1 {
				col := C[6]
				if white {
					col = pixelfx.White
				} else if back {
					col = C[4]
				}
				p.Line(x0, y0, x1, y1, col, 1)
			}
			if u > 0.04 && u < splashTear {
				off, bs := 0.5, 2.0
				if back {
					off, bs = 0, 1
				}
				bead(p, x1-off, y1-1, 0, -1, bs, C, 1)
			}
			if !back && math.Sin(f.a) > 0.3 {
				p.Glow(x0, y0, 6, C[5], 0.22*reach)
			}
		}
	}

	jet := func(p *pixelfx.Painter) {
		u := t - splashHit - splashJet
		h := splashJetH(u)
		if u < 0 || h < 0.8 {
			return
		}
		top := -h
		// a mound of water heaves up under it
		p.EllipseFn(0, 0, 5, 2.4, func(d, a float64) (uint32, float64) {
			if math.Sin(a) > 0 {
				return 0, 1
			}
			if d < 0.5 {
				return pixelfx.White, 1
			}
			if math.Cos(a) < 0 {
				return C[6], 1
			}
			return C[5], 1
		})
		p.Shade(-3, top-2, 3, 0, func(x, y float64, out *pixelfx.Out) uint32 {
			v := pixelfx.Clamp((y - top) / h)
			w := 1.15 + 1.5*v*v*v
			q := x / w
			if math.Abs(q) > 1 {
				return 0
			}
			switch {
			case q < -0.5:
				out.C = C[6]
			case q < 0.3:
				out.C = pixelfx.White
			case q < 0.72:
				out.C = C[5]
			default:
				out.C = C[4]
			}
			return 0
		})
		if u < 0.12 {
			// the bulb at its tip, about to pinch off
			p.Circle(0, top-1, 1.7, C[5], 1)
			p.Circle(-0.3, top-1.3, 1.1, C[6], 1)
			p.Px(-1, top-2, pixelfx.White)
		}
		p.GlowLine(0, -1, 0, top, 7, C[5], 0.3)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if once("hit", splashHit) {
				lights.Add(0, -6, 52, C[5], 0.6, 0.45)
				lights.Add(0, -4, 16, pixelfx.White, 0.5, 0.08)
				rings.add(0, 0, 8, 56, 2.0, rippleOpts{n: 2, gap: 3.4, k: 1.35, hot: 0.16, glint: 1})
				rings.add(0, 0, 5, 38, 1.7, rippleOpts{n: 1, k: 0.9, delay: 0.14})
				// the first sheet of spray, thrown up fast and fine
				for range 20 {
					a, v := R.Range(0, tau), R.Range(50, 125)
					dh := R.Range(1, 6)
					vh := R.Range(160, 270)
					s := 1.0
					if R.Chance(0.25) {
						s = 2
					}
					delay := R.Range(0, 0.04)
					ds.add(drop{x: math.Cos(a) * 3, z: math.Sin(a) * 3, h: dh, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: vh, s: s, delay: delay})
				}
				// mist rolls out low along the water
				for i := range 9 {
					s := -1.0
					if i%2 != 0 {
						s = 1
					}
					x := s * R.Range(4, 12)
					my := -R.Range(1, 7)
					mvx := s * R.Range(25, 70)
					mvy := -R.Range(4, 16)
					mr0 := R.Range(2.5, 4)
					mr1 := R.Range(7, 11)
					mlife := R.Range(0.9, 1.5)
					mc := C[5]
					if R.Chance(0.5) {
						mc = C[6]
					}
					ma := R.Range(0.3, 0.42)
					mdelay := R.Range(0, 0.1)
					ms.add(x, my, mvx, mvy, mr0, mr1, mlife, mc, ma, mdelay)
				}
			}
			if once("spray", splashHit+0.04) {
				for range 20 {
					a := R.Range(0, tau)
					fy := math.Sin(a)*9*splashSY - R.Range(6, 15)
					fvx := math.Cos(a) * R.Range(25, 90)
					fvy := -R.Range(30, 100)
					flife := R.Range(0.7, 1.3)
					fc := C[6]
					if R.Chance(0.5) {
						fc = pixelfx.White
					}
					fsize := R.Range(0.6, 1.5)
					ps.Spawn(pixelfx.Flake, math.Cos(a)*9, fy, fvx, fvy, flife, fc, fsize, 0, 0)
				}
				for range 5 {
					sx := R.Range(-13, 13)
					sy := -R.Range(6, 20)
					slife := R.Range(0.25, 0.55)
					ssize := R.Range(1, 2.4)
					sdelay := R.Range(0, 0.15)
					ps.Spawn(pixelfx.StarKind, sx, sy, 0, -4, slife, C[6], ssize, sdelay, 0)
				}
			}
			if once("tear", splashHit+splashTear) {
				// the beads on the finger tips tear off and arc away
				h, rt := splashHeight(splashTear), splashLip(splashTear)
				for _, f := range spikes {
					a, v, L := f.a+R.Range(-0.08, 0.08), R.Range(32, 88), h*f.L
					vh := R.Range(90, 190)
					s := 2.0
					if R.Chance(0.3) {
						s = 3
					}
					ds.add(drop{x: math.Cos(f.a) * (rt + L*0.45), z: math.Sin(f.a) * rt, h: h + L + 1, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: vh, s: s})
				}
			}
			// as the crown gives way its lip comes apart into drops
			u := t - splashHit
			if u > 0.13 && u < 0.3 && R.Chance(0.3) {
				a, rt, v := R.Range(0, tau), splashLip(u), R.Range(15, 45)
				dh := scallop(a, splashHeight(u)) * R.Range(0.6, 1)
				vh := R.Range(-10, 45)
				s := 1.0
				if R.Chance(0.3) {
					s = 2
				}
				ds.add(drop{x: math.Cos(a) * rt, z: math.Sin(a) * rt, h: dh, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: vh, s: s})
			}
			if once("fall", splashHit+0.3) {
				// the crown falls back, shedding a few heavy drops close in
				for range 8 {
					a, r, v := R.Range(0, tau), splashLip(0.3), R.Range(15, 40)
					dh := R.Range(3, 7)
					vh := R.Range(30, 80)
					s := 1.0
					if R.Chance(0.4) {
						s = 2
					}
					ds.add(drop{x: math.Cos(a) * r, z: math.Sin(a) * r, h: dh, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: vh, s: s})
				}
			}
			if once("bead", splashHit+splashJet+0.12) {
				// the jet's tip pinches off into a bead that hangs, then drops back
				ds.add(drop{x: 0, z: 0.2, h: splashJetH(0.12) + 1, vx: R.Range(-6, 6), vz: 0, vh: 55, s: 3})
				for range 3 {
					mx := R.Range(-3, 3)
					my := -splashJetH(0.12) + R.Range(2, 8)
					mvx := R.Range(-14, 14)
					mvy := -R.Range(4, 12)
					mr1 := R.Range(4, 6)
					mlife := R.Range(0.6, 1)
					ms.add(mx, my, mvx, mvy, 2, mr1, mlife, C[6], 0.3, 0)
				}
			}
			if once("slump", splashHit+splashJet+0.38) {
				rings.add(0, 0, 3, 30, 1.5, rippleOpts{n: 2, gap: 2.6, k: 0.9})
				for range 4 {
					mx := R.Range(-6, 6)
					my := -R.Range(1, 4)
					mvx := R.Range(-30, 30)
					mvy := -R.Range(3, 10)
					mr1 := R.Range(6, 9)
					mlife := R.Range(0.8, 1.2)
					ms.add(mx, my, mvx, mvy, 2.5, mr1, mlife, C[5], 0.28, 0)
				}
			}
			// the stone sinks and lets go of its air: a few bubbles come up and pop
			for _, g := range splashGlugs {
				if once(fmt.Sprintf("glug%d", g.i), splashHit+g.at) {
					bx := R.Range(-5, 5)
					bz := R.Range(-6, 6)
					br := R.Range(1.3, 2.3)
					bubbles = append(bubbles, splashBubble{x: bx, z: bz, t0: t, r: br})
				}
			}
			for i := range bubbles {
				b := &bubbles[i]
				if !b.popped && t-b.t0 > 0.2 {
					b.popped = true
					rings.add(b.x, b.z, 1, 7, 0.6, rippleOpts{n: 1, k: 0.9})
					plinks = append(plinks, waterPlink{x: b.x, y: b.z * splashSY, s: 1, age: 0})
					for range 2 {
						vx := R.Range(-20, 20)
						vz := R.Range(-10, 10)
						vh := R.Range(50, 90)
						ds.add(drop{x: b.x, z: b.z, h: 1, vx: vx, vz: vz, vh: vh, s: 1, small: true, life: 1})
					}
				}
			}
			for i := len(plinks) - 1; i >= 0; i-- {
				plinks[i].age += dt
				if plinks[i].age > 0.14 {
					plinks = append(plinks[:i], plinks[i+1:]...)
				}
			}
			ds.update(dt)
			rings.update(dt)
			ms.update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			u := t - splashHit
			// the cavity the stone punches, dark inside the foot of the crown
			if u > 0 && u < 0.26 {
				r := splashFoot(u) * 0.85 * (1 - pixelfx.Smooth(0.12, 0.26, u))
				p.EllipseFn(0, 0, r, r*splashSY+0.6, func(_, a float64) (uint32, float64) {
					if math.Sin(a) < 0 {
						return C[1], 1
					}
					return C[2], 1
				})
			}
			rings.draw(p, C, -1)
			crown(p, true)
			ds.draw(p, C, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			u := t - splashHit
			rings.draw(p, C, 1)
			if u > 0.2 {
				rx := 4 + 11*pixelfx.Ease.OutCubic(pixelfx.Clamp((u-0.2)/0.7))
				k := pixelfx.Smooth(0.2, 0.32, u) * (1 - pixelfx.Smooth(0.5, 2.0, u)) * 0.9
				lace(p, 0, 0, rx, rx*splashSY+0.5, k, C, t, seed)
			}
			crown(p, false)
			jet(p)
			for _, b := range bubbles {
				g := (t - b.t0) / 0.2
				if g > 1 {
					continue
				}
				r, x, y := b.r*pixelfx.Ease.OutBack(g), b.x, b.z*splashSY
				for a := pi; a <= tau+0.01; a += 0.5 / math.Max(1, r) {
					p.Dot(x+math.Cos(a)*r, y+math.Sin(a)*r, C[6], 1)
				}
				p.Px(x-r*0.4, y-r*0.6, pixelfx.White)
				p.Glow(x, y-1, 4, C[5], 0.25)
			}
			for _, q := range plinks {
				k := q.age / 0.14
				if k < 0.5 {
					p.Px(q.x, q.y, pixelfx.White)
				}
				if q.s > 1 {
					c := C[6]
					if k < 0.4 {
						c = pixelfx.White
					}
					p.Line(q.x, q.y-1, q.x, q.y-1-3*(1-k), c, 1)
				}
				p.Glow(q.x, q.y, 4, C[5], 0.3*(1-k))
			}
			ds.draw(p, C, pixelfx.LayerFront)
			// the hit: a flat white flash on the surface for two frames
			if u >= 0 && u < 0.07 {
				p.EllipseFn(0, 0, 10, 2.4, func(d, _ float64) (uint32, float64) {
					if d < 0.6 {
						return pixelfx.White, 1
					}
					return C[6], 1
				})
				pixelfx.Sparkle(p, 0, -3, 5*(1-u/0.07), C[6], C[5], 0.5)
			}
			// the stone, falling fast with a streak of air off it
			if showStone && t < splashHit {
				v := t / splashHit
				y := -70 + 70*(0.4*v+0.6*v*v)
				p.LineFn(0, y-12, 0, y-3, func(w float64) (uint32, float64) { return ST[4], w * 0.8 })
				pebble(p, 0, y, 2.6, ST)
			}
			ms.draw(p, C[5])
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
			// the disturbed water keeps a cool light on it while it settles
			if u > 0 {
				p.Glow(0, -2, 34, C[4], 0.2*(1-pixelfx.Smooth(0, 1.6, u)))
				p.Glow(0, 2, 64, C[3], 0.16*(1-pixelfx.Smooth(0.1, 2.1, u)))
			}
		},
	}
}
