// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/acid-splash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the drops that splat where they land from Sprite Fusion's open-source blood (https://www.spritefusion.com/pixel-art-vfx/blood), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// AcidSplash is a gob of acid bursting into a crown and eating a smoking pit into the floor. It is registered as "nature/acid-splash".
var AcidSplash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Acid splash",
		Desc:     "A gob of acid bursts into a crown and spatters; it eats a glowing, smoking pit into the floor and boils down into it, spitting bright beads, pale steam curling off.",
		Tags:     []string{"nature", "acid", "poison", "splash", "liquid", "ground", "one-shot"},
		Size:     [2]int{184, 116},
		Origin:   [2]int{92, 100},
		Duration: acidSplashDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e140a", "#1c3010", "#2c5214", "#457e18", "#6aae1c", "#a0dc2a", "#dcff6a", "#f8ffd0")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newAcidSplash,
}

func init() { pixelfx.Register("nature/acid-splash", AcidSplash) }

const acidSplashDuration = 2.8

var (
	acidHit    = 0.3
	acidY0     = -62.0
	acidV0     = 60.0
	acidSpread = 0.4
	acidDry    = 0.7
	acidGone   = 2.2
	acidW      = 30.0
	acidPit    = 5.0
)

type acidWisp struct {
	x, y, t0, life, rise, length, amp, k, ph, drift float64
}

// drawAcidWisp draws a curl of steam: thin, pale, quick, fading as it goes.
func drawAcidWisp(p *pixelfx.Painter, q acidWisp, t float64, col uint32) {
	age := t - q.t0
	u := age / q.life
	if u < 0 || u >= 1 {
		return
	}
	head := q.rise * age
	tail := math.Max(0, head-q.length)
	fade := pixelfx.Smooth(0, 0.15, u) * (1 - pixelfx.Smooth(0.4, 1, u))
	for h := tail; h <= head; h += 0.5 {
		s := (h - tail) / math.Max(1, head-tail)
		x := q.x + q.amp*math.Sin(h*q.k+q.ph)*(0.2+s) + q.drift*age
		p.Blend(x, q.y-h, col, fade*math.Sin(pi*s)*0.42)
	}
}

type acidSpike struct {
	a, h, r float64
	shed    bool
}

type acidStain struct {
	x, t0, w float64
}

type acidPop struct {
	x, y, t0 float64
}

func newAcidSplash(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	SCORCH := pixelfx.Mix(C[0], pixelfx.Hex("#140f0c"), 0.6)
	BURNT := pixelfx.Mix(C[1], pixelfx.Hex("#2a1a10"), 0.6)
	STEAM := pixelfx.Mix(C[7], pixelfx.Hex("#e8ece6"), 0.75)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}})
	ds := newDrops(floor)
	lights := &pixelfx.Lights{}
	var stains []acidStain
	var wisps []acidWisp
	var pops []acidPop
	spikes := make([]acidSpike, 13)
	for i := range spikes {
		a := (float64(i)/13)*tau + R.Range(-0.18, 0.18)
		h := R.Range(4, 10)
		r := R.Range(0.85, 1.12)
		spikes[i] = acidSpike{a: a, h: h, r: r}
	}
	rag := make([]float64, 80)
	for i := range rag {
		rag[i] = pixelfx.Rand(float64(i), o.Seed(), 5) - 0.5
	}
	t := 0.0
	hit := false
	// the pit it etches: wider as it spreads, deeper as it eats; the acid's level sinks into it as it boils away
	pw := func() float64 { return acidW * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-acidHit)/acidSpread)) * 1.02 }
	pitD := func() float64 { return acidPit * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-acidHit)/1.1)) }
	depth := func(x float64) float64 {
		w := pw()
		d := math.Abs(x) / math.Max(1, w)
		if d >= 1 {
			return -1
		}
		idx := (int(pixelfx.Round(x)) + 200) % 80
		return pitD()*math.Sqrt(1-d*d) + rag[idx]*0.9
	}
	level := func() float64 {
		if t < acidHit {
			return 0
		}
		return -1.6 + (acidPit+1.6)*pixelfx.Ease.InQuad(pixelfx.Smooth(acidDry, acidGone, t))
	}
	width := func() float64 {
		L := level()
		w := pw()
		if L <= 0 {
			return w
		}
		q := L / math.Max(0.1, pitD())
		return w * math.Sqrt(math.Max(0, 1-q*q))
	}
	heat := func() float64 {
		return pixelfx.Smooth(acidHit, acidHit+0.3, t) * (1 - pixelfx.Smooth(acidGone-0.3, acidGone+0.25, t))
	}
	gobY := func() float64 { return acidY0 + acidV0*t + 0.5*950*t*t }
	crownR := func() float64 { return 4 + 11*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-acidHit)/0.16)) }
	crownH := func() float64 { return 5 * math.Sin(pi*pixelfx.Clamp((t-acidHit)/0.26)) }
	spikeK := func() float64 {
		u := pixelfx.Clamp((t - acidHit - 0.015) / 0.26)
		return math.Sin(pi * math.Pow(u, 0.6))
	}
	tipOf := func(s acidSpike) (float64, float64, float64, float64) {
		r := crownR() * s.r
		h := crownH()
		sn := math.Sin(s.a)
		sh := s.h * spikeK()
		bx := math.Cos(s.a) * r
		by := sn*r*0.3 - h*(0.75+0.25*math.Abs(sn))
		return bx, by, bx + math.Cos(s.a)*sh*0.55, by - sh
	}
	steam := func(x, y float64, big bool) {
		lifeScale := 1.0
		if big {
			lifeScale = 1.3
		}
		life := math.Min(R.Range(0.5, 0.9)*lifeScale, acidSplashDuration-0.05-t)
		rise := R.Range(22, 32)
		lenScale := 1.0
		if big {
			lenScale = 1.4
		}
		length := R.Range(5, 10) * lenScale
		amp := R.Range(1, 2.5)
		k := R.Range(0.35, 0.6)
		ph := R.Range(0, tau)
		drift := R.Range(-4, 4)
		wisps = append(wisps, acidWisp{x: x, y: y, t0: t, life: life, rise: rise, length: length, amp: amp, k: k, ph: ph, drift: drift})
	}
	// the crown: a thin cup wall rising round the impact, a bright rim, spikes off the rim with beads on their tips
	crown := func(p *pixelfx.Painter, back bool) {
		if t < acidHit || t > acidHit+0.27 {
			return
		}
		r := crownR()
		h := crownH()
		ry := r * 0.3
		for x := math.Floor(-r); x <= math.Ceil(r); x++ {
			c := (x + 0.5) / r
			if math.Abs(c) > 1 {
				continue
			}
			e := math.Sqrt(1 - c*c)
			yb := ry * e
			if back {
				yb = -ry * e
			}
			hh := h * (0.75 + 0.25*e)
			y0 := math.Floor(yb - hh)
			for y := y0; y <= pixelfx.Round(yb); y++ {
				if y == y0 {
					col := C[6]
					if back {
						col = C[5]
					} else if c < 0.1 {
						col = C[7]
					}
					p.Px(x, y, col)
				} else {
					idx := 4
					if c < -0.25 {
						idx = 5
					} else if c > 0.45 {
						idx = 3
					}
					a := 0.7
					if back {
						idx--
						a = 0.55
					}
					p.Blend(x, y, C[idx], a)
				}
			}
		}
		for _, s := range spikes {
			if (math.Sin(s.a) < 0) != back {
				continue
			}
			bx, by, tx, ty := tipOf(s)
			col1, col2 := C[6], C[5]
			if back {
				col1, col2 = C[4], C[3]
			}
			p.Line(bx, by, tx, ty, col1, 1)
			p.Line(bx+1, by, (bx+tx)/2+0.5, (by+ty)/2, col2, 1)
			if !s.shed {
				tipCol := C[7]
				if back {
					tipCol = C[5]
				}
				p.Px(tx, ty-1, tipCol)
				p.Px(tx+1, ty-1, C[5])
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			_ = width()
			if !hit && t >= acidHit {
				hit = true
				lights.Add(0, -4, 28, pixelfx.White, 0.6, 0.08)
				lights.Add(0, -10, 56, C[5], 0.5, 0.6)
				for range 26 {
					a := -pi/2 + R.Range(-1.35, 1.35)
					sp := R.Range(60, 250)
					x := R.Range(-3, 3)
					sz := 1.0
					if R.Chance(0.3) {
						sz = 2
					}
					ds.add(x, -2, math.Cos(a)*sp, math.Sin(a)*sp, sz, 0)
				}
				for range 10 {
					a := -pi/2 + R.Range(-1.2, 1.2)
					sp := R.Range(120, 260)
					ps.Spawn(pixelfx.Spark, 0, -2, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
				}
				for range 5 {
					sx := R.Range(-10, 10)
					sy := -R.Range(2, 6)
					steam(sx, sy, true)
				}
			}
			// beads break off the crown's spikes as they peak
			if hit {
				for i := range spikes {
					s := &spikes[i]
					if !s.shed && t > acidHit+0.09 {
						s.shed = true
						_, _, tx, ty := tipOf(*s)
						vx := math.Cos(s.a) * R.Range(30, 80)
						vy := -R.Range(50, 130)
						sz := 1.0
						if R.Chance(0.4) {
							sz = 2
						}
						ds.add(tx, ty-1, vx, vy, sz, 0)
					}
				}
			}
			ds.update(dt, func(d drop, _ float64) {
				// a drop that lands eats a little spot into the floor and hisses
				if math.Abs(d.x) > pw() {
					stains = append(stains, acidStain{x: d.x, t0: t, w: d.s*0.8 + R.Range(0.3, 1.2)})
				}
				if t < acidGone {
					steam(d.x, -1, false)
				}
			})
			// the acid sizzles: tiny bright beads spit off it, pops flash on its surface, steam curls off it and the pit
			lv, w2 := level(), width()
			if t > acidHit+0.15 && t < acidGone && w2 > 2 {
				if R.Chance(0.3 * f) {
					x := R.Range(-0.85, 0.85) * w2
					y := math.Min(lv, depth(x)) - 1
					pops = append(pops, acidPop{x: x, y: y, t0: t})
					for j := 0.0; j < R.Int(1, 3); j++ {
						ds.add(x, math.Min(y-1, -1), R.Range(-40, 40), -R.Range(50, 110), 1, 0)
					}
				}
				if R.Chance(0.14 * f) {
					sx := R.Range(-0.9, 0.9) * w2
					big := R.Chance(0.25)
					steam(sx, math.Min(lv, 0)-1, big)
				}
			}
			// the pit keeps smoking a while after the acid is gone
			if t > acidGone-0.2 && t < acidSplashDuration-0.5 && R.Chance(0.08*f) {
				steam(R.Range(-0.7, 0.7)*pw(), 0, false)
			}
			for i := len(pops) - 1; i >= 0; i-- {
				if t-pops[i].t0 > 0.1 {
					pops = append(pops[:i], pops[i+1:]...)
				}
			}
			for i := len(wisps) - 1; i >= 0; i-- {
				if t-wisps[i].t0 > wisps[i].life {
					wisps = append(wisps[:i], wisps[i+1:]...)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			fade := 1 - pixelfx.Smooth(acidSplashDuration-0.6, acidSplashDuration-0.08, t)
			hk, w, lv := heat(), pw(), level()
			// spatter spots etched round it
			for _, s := range stains {
				age := t - s.t0
				k := 1 - pixelfx.Smooth(1, 1.8, age)
				c := BURNT
				switch {
				case age < 0.15:
					c = C[7]
				case age < 0.5:
					c = C[6]
				case age < 0.9:
					c = C[4]
				}
				for x := math.Floor(s.x - s.w); x <= math.Ceil(s.x+s.w); x++ {
					col := c
					if math.Abs(x-s.x) < s.w*0.5 {
						col = SCORCH
					}
					p.Dot(x, 0, col, k*fade)
				}
				if age < 0.5 {
					p.Glow(s.x, -1, 5, C[5], 0.3*(1-age/0.5))
				}
			}
			crown(p, true)
			if t > acidHit && w > 1 {
				// the pit: an etched bowl in the floor, its edge glowing where the acid is still eating, cooling to a burnt lip
				for x := math.Floor(-w) - 1; x <= math.Ceil(w)+1; x++ {
					dp := depth(x + 0.5)
					if dp < 0 {
						continue
					}
					bottom := math.Max(0, pixelfx.Round(dp))
					edge := math.Abs(x+0.5)/w > 0.88
					for y := 0.0; y <= bottom; y++ {
						col := SCORCH
						if y == bottom {
							if hk > 0.5 {
								col = C[6]
							} else if hk > 0.15 {
								col = C[4]
							} else {
								col = BURNT
							}
						}
						p.Dot(x, y, col, fade)
					}
					botCol := SCORCH
					if hk > 0.5 {
						botCol = C[5]
					} else if hk > 0.15 {
						botCol = C[3]
					}
					p.Dot(x, bottom+1, botCol, fade*0.8)
					if edge {
						edgeCol := BURNT
						if hk > 0.4 {
							edgeCol = C[7]
						} else if hk > 0.1 {
							edgeCol = C[5]
						}
						p.Dot(x, -1, edgeCol, fade)
					}
				}
				for _, sd := range [2]float64{-1, 1} {
					p.Glow(sd*w, -1, 8, C[6], 0.45*hk)
				}
				// the acid in it: a bright skin shimmering on top, darker as it deepens, sinking as it boils off
				if t < acidGone {
					for x := math.Floor(-w); x <= math.Ceil(w); x++ {
						dp := depth(x + 0.5)
						if dp < 0 {
							continue
						}
						d := math.Abs(x+0.5) / w
						dome := pixelfx.Round(lv)
						if lv < 0 {
							dome = pixelfx.Round(lv * math.Sqrt(math.Max(0, 1-d*d)))
						}
						bottom := pixelfx.Round(dp)
						if dome > bottom {
							continue
						}
						n := pixelfx.Noise(x*0.25-t*2.4, 3) + pixelfx.Noise(x*0.1+t*1.4, 5)*0.6
						for y := dome; y <= bottom; y++ {
							var col uint32
							switch {
							case y == dome:
								if n > 0.7 {
									col = C[7]
								} else {
									col = C[6]
								}
							case y == dome+1:
								if n > 0.2 {
									col = C[5]
								} else {
									col = C[4]
								}
							case y >= bottom:
								col = C[3]
							default:
								col = C[4]
							}
							p.Px(x, y, col)
						}
					}
				}
				p.Glow(0, -6, w*1.4+10, C[4], 0.3*hk*(0.85+0.15*math.Sin(t*9)))
				p.Glow(0, 1, w*0.9, C[6], 0.2*hk)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the gob: a teardrop stretched by its fall, lit on its upper left, a trail of droplets above it
			if t < acidHit {
				y := gobY()
				v := acidV0 + 950*t
				st := 1 + v*0.0022
				rx := 2.6 / math.Sqrt(st)
				ry := 2.8 * st
				p.Shade(-rx-1, y-ry-4, rx+1, y+ry+1, func(x, yy float64, out *pixelfx.Out) uint32 {
					dx, dy := x/rx, (yy-y)/ry
					tail := dy < 0 && math.Abs(x) < rx*(1+dy*0.9)+0.4 && dy > -1.7
					d := dx*dx + dy*dy
					if d > 1.25 && !tail {
						return 0
					}
					switch {
					case d > 1 && !tail:
						out.C = C[2]
					case dx < -0.2 && dy < -0.1:
						out.C = C[7]
					case dy > 0.45:
						out.C = C[4]
					default:
						out.C = C[5]
					}
					return 0
				})
				p.Px(-1, y-ry*0.3, pixelfx.White)
				for i := 1; i <= 3; i++ {
					p.Dot(0, y-ry-2-float64(i)*4, C[5], 1-float64(i)*0.25)
				}
				p.Glow(0, y, 10, C[5], 0.45)
			}
			crown(p, false)
			// the hero frame: a white burst where it lands
			e := t - acidHit
			if e >= 0 && e < 0.07 {
				k := 1 - e/0.07
				col := C[7]
				if k > 0.5 {
					col = pixelfx.White
				}
				p.Ellipse(0, -2, 6+6*(1-k), 2.5, col, 1)
				pixelfx.Sparkle(p, 0, -4, 8*k, C[7], C[6], 0.6)
			}
			// sizzle: tiny star flashes where beads spit off the surface
			for _, q := range pops {
				k := (t - q.t0) / 0.1
				p.Px(q.x, q.y, pixelfx.White)
				for _, d := range [3][2]float64{{1, 0}, {-1, 0}, {0, -1}} {
					p.Dot(q.x+d[0]*(1+k*2), q.y+d[1]*(1+k*2), C[7], 1-k)
				}
				p.Glow(q.x, q.y, 4, C[6], 0.4*(1-k))
			}
			for _, q := range wisps {
				drawAcidWisp(p, q, t, STEAM)
			}
			ds.draw(p, C)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
