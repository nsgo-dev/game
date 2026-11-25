// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/ricochet.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the glancing star, the tracer that streaks off, the skid on the surface and the spark spray from Sprite Fusion's open-source impact (https://www.spritefusion.com/pixel-art-vfx/impact), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Ricochet is a tracer streaking down onto the floor, glancing off with a white-hot star and skipping away at an angle, leaving a glowing skid while sparks skitter along the ground. It is registered as "impacts/ricochet".
var Ricochet = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ricochet",
		Desc:     "A tracer streaks down onto the floor, glances off with a white-hot star and skips away at an angle, leaving a glowing skid while sparks skitter along the ground.",
		Tags:     []string{"impact", "ricochet", "bullet", "tracer", "sparks", "ground", "directional", "one-shot"},
		Size:     [2]int{232, 84},
		Origin:   [2]int{64, 60},
		Duration: ricochetDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#5b5079")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newRicochet,
}

func init() { pixelfx.Register("impacts/ricochet", Ricochet) }

const ricochetDuration = 1.1

// When the round lands, how fast it flies, and the angles it comes in and goes out at; variables so sums with them round as pixelfx's do.
var (
	ricochetHit   = 0.05
	ricochetSpeed = 760.0
	ricochetIn    = 0.32
	ricochetOut   = -0.21
)

// ricochetTracer draws a tracer: a white head, a hot body cooling to a dim tail, from (hx, hy) back along angle a for L px.
func ricochetTracer(p *pixelfx.Painter, hx, hy, a, L float64, C []uint32, k float64) {
	c, s := math.Cos(a), math.Sin(a)
	for d := 0.0; d <= L; d += 0.5 {
		u := d / L
		var col uint32
		switch {
		case u < 0.12:
			col = pixelfx.White
		case u < 0.45:
			col = C[5]
		case u < 0.75:
			col = C[4]
		default:
			col = C[2]
		}
		al := 1.0
		if u >= 0.45 {
			al = k * (1.25 - u)
		}
		p.Dot(hx-c*d, hy-s*d, col, al)
	}
	p.Glow(hx, hy, 9, C[4], 0.6*k)
}

func newRicochet(o *pixelfx.Options) pixelfx.Instance {
	C, st, R, D := pixelfx.Fit(o.Palette(), 7), stoneOf(o.Color("stone")), pixelfx.NewRng(o.Seed()), float64(ricochetDuration)
	floor := func(float64) float64 { return 0 }
	HIT, SPEED, IN, OUT := ricochetHit, ricochetSpeed, ricochetIn, ricochetOut
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[6], C[5], C[4], C[2]}})
	lights := &pixelfx.Lights{}
	// the star leans hard along the way out, and barely digs into the floor
	sp := spikes(R, 9, OUT, spikeOpts{len0: 6, len1: 9, w0: 1.4, w1: 1.9, fwd: 2.6, back: 0.6, jag: 1, jit: 0.18})
	for i := range sp {
		s := &sp[i]
		if math.Sin(s.a) > 0.2 {
			s.len *= 0.3
		}
		if math.Abs(math.Sin(s.a-OUT)) > 0.6 {
			s.len *= 0.65
		}
	}
	t, hit := 0.0, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit && t >= HIT {
				hit = true
				lights.Add(0, -2, 44, C[4], 0.65, 0.25)
				lights.Add(0, -1, 14, pixelfx.White, 0.6, 0.06)
				// sparks skitter on along the floor, skipping, and a few spray higher
				for i := range 18 {
					low := i < 12
					var a, v float64
					if low {
						a = OUT + R.Range(-0.22, 0.12)
						v = R.Range(260, 520) * 1
					} else {
						a = OUT + R.Range(-0.7, 0.25)
						v = R.Range(260, 520) * 0.7
					}
					x := R.Range(-1, 2)
					life := R.Range(0.15, 0.42)
					if low {
						life *= 1.3
					} else {
						life *= 1
					}
					delay := 0.0
					if low {
						delay = R.Range(0, 0.05)
					}
					ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*v, math.Sin(a)*v, life, pixelfx.White, 1, delay, 0)
				}
				// grit kicked up and a scuff of dust
				for i := range 7 {
					a := -pi/2 + R.Range(-0.2, 0.9)
					v := R.Range(60, 170)
					x := R.Range(0, 4)
					life := R.Range(0.6, 0.95)
					c := pixelfx.Pick(R, []uint32{st.light, st.body, st.lip})
					size := 1.0
					if i < 2 {
						size = 2
					}
					ps.Spawn(pixelfx.Grit, x, -1, math.Cos(a)*v, math.Sin(a)*v, life, c, size, 0, 0)
				}
				for range 4 {
					x, vx, vy, life := R.Range(0, 8), R.Range(20, 70), -R.Range(4, 16), R.Range(0.25, 0.45)
					c := pixelfx.Mix(st.dust, pixelfx.White, R.Range(0.1, 0.3))
					ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, c, R.Range(1.2, 2), 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the skid: a gouge along the floor that glows where the round dragged, cooling from its far end
			if u := t - HIT; u >= 0 {
				heat, fade, L := math.Pow(pixelfx.Clamp(1-u/0.85), 1.2), 1-pixelfx.Smooth(D-0.4, D-0.05, t), 12*math.Min(1, u/0.03)
				for x := -3; float64(x) <= L; x++ {
					fx := float64(x)
					q := (fx + 3) / 15
					h := heat * (1.15 - q*0.6)
					var c uint32
					switch {
					case h > 0.8:
						c = pixelfx.White
					case h > 0.55:
						c = C[5]
					case h > 0.35:
						c = C[4]
					case h > 0.18:
						c = C[3]
					case h > 0.08:
						c = C[2]
					default:
						c = soot
					}
					p.Dot(fx, 0, c, fade)
					if x > -2 && fx < L-2 && x&1 == 0 {
						c := soot
						if h > 0.4 {
							c = C[3]
						}
						p.Dot(fx, 1, c, fade*0.7)
					}
				}
				if heat > 0 {
					p.GlowLine(-3, 0, L, 0, 5, C[3], 0.45*heat)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerFront)
			if t < HIT {
				// incoming: the tracer races down to the floor
				d := (HIT - t) * SPEED
				ricochetTracer(p, -math.Cos(IN)*d, -math.Sin(IN)*d, IN, 22, C, 1)
			} else {
				u := t - HIT
				// glancing off: a stretched star, then the round skips away, its tail stretching out behind it
				if u < 0.03 {
					star(p, 0, -1, sp, 1, 1, [4]uint32{C[2], C[5], C[4], pixelfx.White}, 1, 2)
				} else if u < 0.06 {
					star(p, 0, -1, sp, 1.25, 0.55, [4]uint32{C[1], C[4], C[3], C[5]}, 1, 2)
				}
				if u < 0.2 {
					head, k := 4+u*SPEED, 1-u/0.2
					ricochetTracer(p, math.Cos(OUT)*head, -1+math.Sin(OUT)*head, OUT, math.Min(head-2, 18+u*200), C, k)
				}
				// a thin half ring hugging the floor, swelling and cooling
				if g := (u - 0.02) / 0.2; g >= 0 && g < 1 {
					r := 5 + 12*(1-math.Pow(1-g, 3))
					c := C[3]
					if g < 0.3 {
						c = C[5]
					} else if g < 0.6 {
						c = C[4]
					}
					n := math.Ceil(r * pi * 1.3)
					for j := 0.0; j <= n; j++ {
						a := -pi + (j/n)*pi
						p.Dot(math.Cos(a)*r, -1+math.Sin(a)*r, c, 1.15-g)
					}
				}
				if pop := math.Exp(-u / 0.045); pop > 0.02 {
					p.Glow(0, -2, 22, C[3], 0.75*pop)
					p.Glow(0, -1, 8, pixelfx.White, 0.5*pop)
				}
			}
			lights.Draw(p)
		},
	}
}
