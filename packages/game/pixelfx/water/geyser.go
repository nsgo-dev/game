// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/geyser.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source holy-light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez, for the column (a banded white core, strands running through it, light down its length) and the slam with its floor ring.

package water

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Geyser is a pool that heaves and burbles faster and faster, then blasts a white column of water skyward with a churning foam head; it rains back down in drops and steam, slumps and sputters out. It is registered as "water/geyser".
var Geyser = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Geyser",
		Desc:     "A pool heaves and burbles faster and faster, then blasts a white column of water skyward with a churning foam head; it rains back down in drops and steam, slumps and sputters out.",
		Tags:     []string{"water", "geyser", "eruption", "steam", "column", "nature", "one-shot"},
		Size:     [2]int{184, 168},
		Origin:   [2]int{92, 146},
		Duration: 5,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "height", Default: pixelfx.Num(100)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newGeyser,
}

func init() { pixelfx.Register("water/geyser", Geyser) }

const (
	geyserSY = 0.22
	geyserG  = 820
)

// geyserSpurts are small spurts while it builds, and the sputters after: [time, height].
var (
	geyserErupt   = 1.15
	geyserFall    = 2.6
	geyserGone    = 3.3
	geyserSpurts  = [6][2]float64{{0.46, 7}, {0.72, 12}, {0.92, 9}, {1.04, 15}, {geyserGone + 0.15, 15}, {geyserGone + 0.55, 8}}
	geyserPopDirs = [5][2]float64{{1, 0}, {-1, 0}, {0.7, -0.7}, {-0.7, -0.7}, {0, -1}}
)

type waterPlink struct {
	x, y, s, age float64
}

type geyserPop struct {
	x, y, age, r float64
}

func newGeyser(o *pixelfx.Options) pixelfx.Instance {
	C, R, HMAX, S := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Num("height"), o.Seed()
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R})
	ms := &mist{}
	lights := &pixelfx.Lights{}
	rings := newRipples(geyserSY)
	var plinks []waterPlink
	var pops []geyserPop
	V0 := math.Sqrt(2 * geyserG * HMAX)
	t := 0.0
	poolR := func(tt float64) float64 {
		return (12 + 7*pixelfx.Smooth(0, 1, tt) + 15*pixelfx.Ease.OutCubic(pixelfx.Clamp((tt-geyserErupt)/1.2))) * (1 - 0.25*pixelfx.Smooth(geyserGone, 4.6, tt))
	}
	poolK := func(tt float64) float64 {
		return pixelfx.Smooth(0, 0.2, tt) * (1 - pixelfx.Smooth(4.0, 4.85, tt))
	}
	var ds *drops
	ds = newDrops(geyserSY, geyserG, 0.35, func(d *drop) {
		inPool := pixelfx.Hypot(d.x, d.z) < poolR(t)
		r1Base, rk := 1.5, 0.7
		if inPool {
			r1Base, rk = 2.5, 0.95
		}
		rings.add(d.x, d.z, 0.6, r1Base+d.s*1.6, 0.32+d.s*0.1, rippleOpts{n: 1, k: rk, hot: 0.05})
		plinks = append(plinks, waterPlink{x: d.x, y: d.z * geyserSY, s: d.s, age: 0})
		if d.s > 1 && !d.small {
			for range 2 {
				a, v := R.Range(0, tau), R.Range(8, 28)
				ds.add(drop{x: d.x, z: d.z, h: 0.5, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: R.Range(45, 95), s: 1, small: true, life: 1})
			}
		}
	})
	// how tall the column stands: a fast blast, surging while it holds, slumping when the pressure goes
	top := func(tt float64) float64 {
		if tt < geyserErupt || tt > geyserGone {
			return 0
		}
		return HMAX * pixelfx.Ease.OutCubic(pixelfx.Clamp((tt-geyserErupt)/0.3)) * (0.93 + 0.07*pixelfx.Noise(tt*2.2, S)) * (1 - pixelfx.Ease.InQuad(pixelfx.Clamp((tt-geyserFall)/(geyserGone-geyserFall))))
	}
	head := func(tt float64) float64 {
		// Force float64 rounding of Smooth before subtracting from 1 so arm64 FMA does not fuse 1 - t*t*(3-2*t) into a tiny nonzero positive value at t = 3.05.
		s := float64(pixelfx.Smooth(geyserFall+0.1, geyserFall+0.45, tt))
		return pixelfx.Smooth(geyserErupt+0.08, geyserErupt+0.3, tt) * (1 - s)
	}
	spurt := func(tt float64) float64 {
		h := 0.0
		for _, sp := range geyserSpurts {
			h = math.Max(h, sp[1]*math.Sin(pi*pixelfx.Clamp((tt-sp[0])/0.2)))
		}
		return h
	}
	done := map[string]bool{}
	once := func(key string, at float64) bool {
		if !done[key] && t >= at {
			done[key] = true
			return true
		}
		return false
	}
	emit := func(rate, dt float64) bool { return R.Chance(math.Min(1, rate*dt)) }

	colRamp := [5]uint32{pixelfx.White, C[6], C[5], C[4], C[3]}
	// column draws the column: a white core banded out to blue edges, shadowed on the right; 1 px strands whose streaks run up in jet time.
	column := func(p *pixelfx.Painter, H, thin float64) {
		if H < 1 {
			return
		}
		jetTime := func(h float64) float64 {
			return (V0 - math.Sqrt(math.Max(0, V0*V0-2*geyserG*math.Min(h, HMAX*0.96)))) / geyserG
		}
		p.Shade(-22, -H-2, 22, 0, func(x, y float64, out *pixelfx.Out) uint32 {
			h := -y
			v := h / H
			sway := pixelfx.Noise(t*1.6+h*0.025, S+5) * 2.5 * v
			ragY := 7.0
			if x < sway {
				ragY = 3
			}
			rag := pixelfx.Noise2(h*0.2-t*11, ragY, S+6) * (0.5 + 1.8*v)
			w := (3.6+7.5*math.Pow(v, 1.8))*thin + rag
			q := (x - sway) / math.Max(1, w)
			aq := math.Abs(q)
			if aq > 1 {
				return 0
			}
			X := math.Floor(x)
			streak := pixelfx.Noise2(X*1.618+0.3, (jetTime(h)-t)*14, S)
			body := pixelfx.Noise2(math.Floor((x+40)/2)*2.3, (jetTime(h)-t)*5, S+1)
			lvl := 3
			switch {
			case aq < 0.42:
				lvl = 0
			case aq < 0.7:
				lvl = 1
			case aq < 0.9:
				lvl = 2
			}
			if q > 0.38 {
				lvl++
			}
			if streak > 0.28 {
				lvl--
			}
			if body < -0.4 {
				lvl++
			}
			out.C = colRamp[int(pixelfx.ClampTo(float64(lvl), 0, 4))]
			return 0
		})
		p.GlowLine(0, 0, 0, -H, 16+8*thin, C[5], 0.12*thin)
	}

	backBillowRamp := []uint32{C[0], C[1], C[2], C[3], C[3], C[4], C[5], C[5]}
	// foamHead draws the head: water boiling over at the top and falling back round the column in an umbrella of streaks, foam billows churning in it, shadowed underneath, lit on top.
	foamHead := func(p *pixelfx.Painter, H, k float64, back bool) {
		if k <= 0.01 || H < 4 {
			return
		}
		if back {
			// the umbrella: streaks pouring off the rim of the head, running down and thinning out
			for i := 0; i < 16; i++ {
				side := -1.0
				if i%2 != 0 {
					side = 1
				}
				x0 := side * (5 + 9*pixelfx.Rand(float64(i), S+20)) * k
				ln := (14 + 26*pixelfx.Rand(float64(i), S+21)) * k
				speed := 70 + 50*pixelfx.Rand(float64(i), S+22)
				for j := 0; j < 3; j++ {
					f := math.Mod(t*speed/ln+float64(j)/3+pixelfx.Rand(float64(i), S+23), 1)
					y0 := -H + 2 + f*ln
					x := x0 + side*f*f*6
					L := 1 + 4*f*(1-f)*2
					if f < 0.85 {
						c := C[4]
						if f < 0.4 {
							c = C[6]
						} else if f < 0.7 {
							c = C[5]
						}
						p.Line(x, y0, x+side*0.6, y0+L, c, 1)
					}
				}
			}
		}
		for i := 0; i < 12; i++ {
			dir := -1.1
			if i%2 != 0 {
				dir = 1.3
			}
			a := (float64(i)/12)*tau + t*dir
			z := math.Sin(a)
			if (z < 0) != back {
				continue
			}
			rho := (3 + 7*pixelfx.Rand(float64(i), S)) * k
			x := math.Cos(a)*rho*1.25 + pixelfx.Noise(t*3+float64(i), S+8)*2.5
			y := -H + z*rho*0.45 + (rho/10)*4 - 4*(1-rho/(10*k)) + pixelfx.Noise(t*3+float64(i)+50, S+9)*2.5
			r := (2 + 2.6*pixelfx.Rand(float64(i), S+2) + 0.7*math.Sin(t*7+float64(i))) * k
			if back {
				billow(p, x, y, r, backBillowRamp, 1)
			} else {
				p.Circle(x+r*0.15, y+r*0.25, r, C[4], 1)
				bc := C[6]
				if rho > 6*k {
					bc = C[5]
				}
				p.Circle(x, y, r*0.82, bc, 1)
				p.Circle(x-r*0.25, y-r*0.3, r*0.5, pixelfx.White, 1)
			}
		}
		if !back {
			p.Glow(0, -H-2, 38, C[5], 0.34*k)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			H, hk, k := top(t), head(t), t/geyserErupt
			// building: bubbles burst on the heaving mound, faster and faster, steam starts to lift
			if t < geyserErupt && emit(1.5+9*k*k, dt) {
				a, r := R.Range(0, tau), R.Range(0, 7)
				x, z := math.Cos(a)*r, math.Sin(a)*r
				py := z*geyserSY - R.Range(1, 3)
				pr := R.Range(1, 2)
				pops = append(pops, geyserPop{x: x, y: py, age: 0, r: pr})
				rings.add(x, z, 1, R.Range(5, 9), 0.55, rippleOpts{n: 1, k: 0.85})
				// Note: JS re-evaluates R.int(1, 3) on every loop condition check.
				for i := 0.0; i < R.Int(1, 3); i++ {
					vx := R.Range(-25, 25)
					vz := R.Range(-15, 15)
					vh := R.Range(50, 120)
					ds.add(drop{x: x, z: z, h: 2, vx: vx, vz: vz, vh: vh, s: 1, small: true, life: 1})
				}
			}
			if t < geyserErupt && emit(3+7*k, dt) {
				mx := R.Range(-8, 8)
				my := -R.Range(1, 5)
				mvx := R.Range(-10, 10)
				mvy := -R.Range(6, 18)
				mr1 := R.Range(5, 9)
				mlife := R.Range(0.9, 1.5)
				ms.add(mx, my, mvx, mvy, 2, mr1, mlife, C[6], 0.22, 0)
			}
			for _, sp := range geyserSpurts {
				s0, hs := sp[0], sp[1]
				if once(fmt.Sprintf("spurt%g", s0), s0+0.08) {
					n := 3 + int(pixelfx.Round(hs/4))
					for range n {
						a, v := R.Range(0, tau), R.Range(10, 40)
						vh := R.Range(20, 90)
						s := 1.0
						if R.Chance(0.3) {
							s = 2
						}
						ds.add(drop{x: 0, z: 0, h: hs * 0.8, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: vh, s: s})
					}
				}
			}
			if once("erupt", geyserErupt) {
				// the blast: light, a ring thrown across the ground, water flung high, steam rolling out
				lights.Add(0, -26, 64, C[5], 0.85, 0.6)
				lights.Add(0, -4, 28, pixelfx.White, 0.75, 0.1)
				rings.add(0, 0, 6, 40, 1.2, rippleOpts{n: 2, gap: 3, k: 1.2, hot: 0.12, glint: 1})
				for range 44 {
					a, v := R.Range(0, tau), R.Range(30, 120)
					h := R.Range(2, 10)
					vh := R.Range(180, 400)
					var s float64
					if R.Chance(0.15) {
						s = 3
					} else if R.Chance(0.35) {
						s = 2
					} else {
						s = 1
					}
					delay := R.Range(0, 0.12)
					ds.add(drop{x: math.Cos(a) * 3, z: math.Sin(a) * 3, h: h, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: vh, s: s, delay: delay})
				}
				for i := range 12 {
					s := -1.0
					if i%2 != 0 {
						s = 1
					}
					mx := s * R.Range(3, 9)
					my := -R.Range(1, 6)
					mvx := s * R.Range(30, 80)
					mvy := -R.Range(4, 18)
					mr1 := R.Range(9, 14)
					mlife := R.Range(1.2, 1.9)
					mc := pixelfx.White
					if R.Chance(0.5) {
						mc = C[6]
					}
					mdelay := R.Range(0, 0.15)
					ms.add(mx, my, mvx, mvy, 3, mr1, mlife, mc, 0.3, mdelay)
				}
				for range 14 {
					sx := R.Range(-16, 16)
					sy := -R.Range(8, 80)
					svx := R.Range(-10, 10)
					slife := R.Range(0.3, 0.7)
					sc := C[6]
					if R.Chance(0.5) {
						sc = pixelfx.White
					}
					ssize := R.Range(1, 2.6)
					sdelay := R.Range(0.05, 0.5)
					ps.Spawn(pixelfx.StarKind, sx, sy, svx, -6, slife, sc, ssize, sdelay, 0)
				}
			}
			// holding: the head sheds drops that rain back down, spray hangs off the column, steam pours off the top
			if hk > 0 {
				if emit(60*hk, dt) {
					a, v := R.Range(0, tau), R.Range(10, 60)
					dx := R.Range(-9, 9)
					dz := R.Range(-4, 4)
					dh := H + R.Range(-8, 3)
					dvh := R.Range(-40, 60)
					var s float64
					if R.Chance(0.08) {
						s = 3
					} else if R.Chance(0.3) {
						s = 2
					} else {
						s = 1
					}
					ds.add(drop{x: dx, z: dz, h: dh, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: dvh, s: s})
				}
				if emit(9*hk, dt) {
					mx := R.Range(-8, 8)
					my := -H + R.Range(-6, 2)
					mvx := R.Range(-34, 34)
					mvy := -R.Range(14, 34)
					mr1 := R.Range(8, 13)
					mlife := R.Range(0.9, 1.4)
					mc := pixelfx.White
					if R.Chance(0.6) {
						mc = C[6]
					}
					ms.add(mx, my, mvx, mvy, 3, mr1, mlife, mc, 0.2, 0)
				}
				if emit(5*hk, dt) {
					mx := R.Sign() * R.Range(3, 9)
					my := -R.Range(1, 6)
					mvx := R.Sign() * R.Range(20, 50)
					mvy := -R.Range(2, 10)
					mr1 := R.Range(8, 12)
					mlife := R.Range(1, 1.6)
					ms.add(mx, my, mvx, mvy, 3, mr1, mlife, C[6], 0.22, 0)
				}
				if emit(40*hk, dt) {
					s, hh := R.Sign(), R.Range(8, H*0.95)
					fvx := s * R.Range(10, 45)
					fvy := R.Range(-20, 10)
					flife := R.Range(0.6, 1.2)
					fc := C[6]
					if R.Chance(0.5) {
						fc = pixelfx.White
					}
					fsize := R.Range(0.6, 1.3)
					ps.Spawn(pixelfx.Flake, s*(3+6*math.Pow(hh/H, 1.8)), -hh, fvx, fvy, flife, fc, fsize, 0, 0)
				}
				if emit(4*hk, dt) {
					rx := R.Range(-2, 2)
					rz := R.Range(-2, 2)
					r1 := poolR(t) * R.Range(0.7, 1)
					rings.add(rx, rz, 4, r1, 0.9, rippleOpts{n: 1, k: 0.8})
				}
				if emit(7*hk, dt) {
					sx := R.Range(-14, 14)
					sy := -R.Range(10, H+8)
					svx := R.Range(-6, 6)
					slife := R.Range(0.25, 0.55)
					sc := C[6]
					if R.Chance(0.4) {
						sc = pixelfx.White
					}
					ssize := R.Range(0.8, 2.2)
					ps.Spawn(pixelfx.StarKind, sx, sy, svx, -4, slife, sc, ssize, 0, 0)
				}
			}
			if once("collapse", geyserFall+0.12) {
				// the head falls apart: a heavy rain of drops all at once
				for range 40 {
					a, v := R.Range(0, tau), R.Range(15, 80)
					dx := R.Range(-6, 6)
					dz := R.Range(-3, 3)
					dh := top(t) + R.Range(-8, 4)
					dvh := R.Range(-40, 60)
					var s float64
					if R.Chance(0.12) {
						s = 3
					} else if R.Chance(0.35) {
						s = 2
					} else {
						s = 1
					}
					delay := R.Range(0, 0.25)
					ds.add(drop{x: dx, z: dz, h: dh, vx: math.Cos(a) * v, vz: math.Sin(a) * v, vh: dvh, s: s, delay: delay})
				}
				for range 6 {
					mx := R.Range(-8, 8)
					my := -top(t) + R.Range(-6, 6)
					mvx := R.Range(-34, 34)
					mvy := -R.Range(10, 26)
					mr1 := R.Range(9, 13)
					mlife := R.Range(0.8, 1.2)
					ms.add(mx, my, mvx, mvy, 4, mr1, mlife, C[6], 0.18, 0)
				}
			}
			for i := len(pops) - 1; i >= 0; i-- {
				pops[i].age += dt
				if pops[i].age > 0.12 {
					pops = append(pops[:i], pops[i+1:]...)
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
			// the pool: dark see-through water, a lit far rim
			pr, pk := poolR(t), poolK(t)
			if pk > 0 {
				p.Each(-pr, -pr*geyserSY, pr, pr*geyserSY, func(x, y float64, sx, sy int) {
					dx, dy := x/pr, y/(pr*geyserSY)
					d := dx*dx + dy*dy
					if d > 1 {
						return
					}
					c := C[1]
					if d < 0.15 {
						c = C[2]
					}
					p.BlendPx(sx, sy, c, 0.6*pk*math.Min(1, (1-d)*4))
					if y < 0 && d > 0.78 && d < 0.95 {
						p.BlendPx(sx, sy, C[4], 0.7*pk)
					}
				})
			}
			rings.draw(p, C, -1)
			u := (t - geyserErupt) / 0.4
			if u > 0 && u < 1 {
				ring(p, 0, 0.5, 6+42*pixelfx.Ease.OutCubic(u), geyserSY, 1.5*(1-u), C, ringOpts{n: 3, gap: 1.6 + 2*u, half: -1, hot: u < 0.3, seed: 9})
			}
			foamHead(p, top(t), head(t), true)
			ds.draw(p, C, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			H, hk, k := top(t), head(t), pixelfx.Clamp(t/geyserErupt)
			thin := pixelfx.Smooth(geyserErupt, geyserErupt+0.12, t) * (1 - 0.55*pixelfx.Smooth(geyserFall, geyserGone, t))
			rings.draw(p, C, 1)
			u := (t - geyserErupt) / 0.4
			if u > 0 && u < 1 {
				ring(p, 0, 0.5, 6+42*pixelfx.Ease.OutCubic(u), geyserSY, 1.5*(1-u), C, ringOpts{n: 3, gap: 1.6 + 2*u, half: 1, hot: u < 0.3, seed: 9})
			}
			// foam rides the pool round the foot of the column
			fk := pixelfx.Smooth(geyserErupt, geyserErupt+0.3, t) * (1 - pixelfx.Smooth(geyserGone-0.3, 4.4, t))
			if fk > 0 {
				lace(p, 0, 0, 8+10*fk, (8+10*fk)*geyserSY+0.5, fk, C, t, S)
			}
			// the heaving mound while it builds, beating faster
			if t < geyserErupt+0.05 {
				beat := math.Pow(math.Max(0, math.Sin(t*(8+18*k*k))), 3)
				rd, hd := 4+3.5*k+1.5*beat, 1.5+3*k+3*beat
				p.Shade(-rd-1, -hd-1, rd+1, 0.5, func(x, y float64, out *pixelfx.Out) uint32 {
					dx, dy := x/rd, y/hd
					d := dx*dx + dy*dy
					if d > 1 || y > 0.5 {
						return 0
					}
					// water heaving up: a white rim where it faces the light, a tinted body, shadow low on the right
					lit, rim := -dx*0.7-dy*0.7, d > 0.68
					if rim {
						switch {
						case lit > 0.3:
							out.C = pixelfx.White
						case lit > -0.15:
							out.C = C[6]
						default:
							out.C = C[4]
						}
					} else {
						switch {
						case lit > 0.55:
							out.C = C[6]
						case lit > 0.12:
							out.C = C[5]
						case lit > -0.3:
							out.C = C[4]
						default:
							out.C = C[3]
						}
					}
					return 0
				})
				p.Px(-rd*0.35, -hd*0.6, pixelfx.White)
				p.Glow(0, -5, 16+16*k+8*beat, C[5], 0.06+0.12*k+0.12*beat)
			}
			// spurts while it builds and sputters as it dies: a short white jet with a bead on top
			sp := spurt(t)
			if sp > 1 && H < 1 {
				p.Shade(-2, -sp-1, 2, 0, func(x, y float64, out *pixelfx.Out) uint32 {
					q := x / (1 + 0.8*(1+y/sp))
					if math.Abs(q) > 1 {
						return 0
					}
					switch {
					case q < -0.3:
						out.C = C[6]
					case q < 0.35:
						out.C = pixelfx.White
					default:
						out.C = C[5]
					}
					return 0
				})
				p.Circle(0, -sp, 1.3, C[6], 1)
				p.Px(0, -sp-1, pixelfx.White)
				p.GlowLine(0, 0, 0, -sp, 8, C[5], 0.3)
			}
			column(p, H, thin)
			// the foot: billows boil out round it
			if hk > 0 || (H > 1 && t < geyserGone) {
				for i := 0; i < 7; i++ {
					a := (float64(i)/7)*tau + t*2.1
					r := 4 + 2*math.Sin(t*5+float64(i)*1.7)
					x, y := math.Cos(a)*r*1.3, math.Sin(a)*r*geyserSY-2
					billow(p, x, y, (1.6+1.2*pixelfx.Rand(float64(i), S+4))*math.Max(hk, thin*0.6), C, 1)
				}
			}
			foamHead(p, H, hk, false)
			for _, q := range pops {
				g := q.age / 0.12
				for _, d := range geyserPopDirs {
					p.Dot(q.x+d[0]*(q.r+3*g), q.y+d[1]*(q.r+3*g), C[6], 1-g)
				}
			}
			for _, q := range plinks {
				g := q.age / 0.14
				if g < 0.5 {
					p.Px(q.x, q.y, pixelfx.White)
				}
				if q.s > 1 {
					c := C[6]
					if g < 0.4 {
						c = pixelfx.White
					}
					p.Line(q.x, q.y-1, q.x, q.y-1-3*(1-g), c, 1)
				}
				p.Glow(q.x, q.y, 4, C[5], 0.25*(1-g))
			}
			ds.draw(p, C, pixelfx.LayerFront)
			// the blast's first two frames: a white burst at the vent
			f := (t - geyserErupt) / 0.07
			if f >= 0 && f < 1 {
				p.Circle(0, -4, 7*(1-f*0.4), pixelfx.White, 1)
				p.EllipseFn(0, 0, 12, 2.6, func(d, _ float64) (uint32, float64) {
					if d < 0.6 {
						return pixelfx.White, 1
					}
					return C[6], 1
				})
			}
			ps.Draw(p, pixelfx.LayerBoth)
			ms.draw(p, C[5])
			lights.Draw(p)
			// light: the column and its head, the pool round its foot
			if H > 1 {
				p.GlowLine(0, 0, 0, -H, 26, C[4], 0.2*thin)
			}
			pre := 0.0
			if t < geyserErupt {
				pre = 0.3 * k
			}
			p.Glow(0, -2, 46, C[4], 0.24*math.Max(hk, pre))
			p.Glow(0, 2, 60, C[3], 0.12*poolK(t))
		},
	}
}
