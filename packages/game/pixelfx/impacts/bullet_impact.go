// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/bullet-impact.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the open-sided star, the spark cone off the surface, the chips, dust and smoke from Sprite Fusion's open-source impact (https://www.spritefusion.com/pixel-art-vfx/impact), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// BulletImpact is a bullet hitting a wall on the right: a hot half-star, sparks sprayed back in a cone, a puff of grit, chips that fall and bounce, a pock mark cooling to soot. It is registered as "impacts/bullet-impact".
var BulletImpact = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Bullet impact",
		Desc:     "A bullet hits a wall on the right: a hot half-star, sparks sprayed back in a cone, a puff of grit, chips that fall and bounce, a pock mark cooling to soot.",
		Tags:     []string{"impact", "bullet", "gun", "wall", "sparks", "debris", "directional", "one-shot"},
		Size:     [2]int{152, 100},
		Origin:   [2]int{116, 84},
		Duration: bulletDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#5b5079")},
			{Name: "height", Default: pixelfx.Num(24)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newBulletImpact,
}

func init() { pixelfx.Register("impacts/bullet-impact", BulletImpact) }

const bulletDuration = 1.4

// bulletRim is one pixel of the pock mark: where it is, and whether it is the hole (false) or its rim (true).
type bulletRim struct {
	x, y float64
	rim  bool
}

func newBulletImpact(o *pixelfx.Options) pixelfx.Instance {
	C, st, R, X, Y := pixelfx.Fit(o.Palette(), 7), stoneOf(o.Color("stone")), pixelfx.NewRng(o.Seed()), 0.0, -o.Num("height")
	floor, D := func(float64) float64 { return 0 }, float64(bulletDuration)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[6], C[5], C[4], C[2]}})
	lights := &pixelfx.Lights{}
	// the wall's face looks left (N); the bullet came in level, a touch downward, and the spray bounces back off it
	N, IN := pi, 0.08
	B := pi - IN
	open := func(a float64) bool { return math.Cos(a-N) > -0.25 }
	sp := spikes(R, 9, B, spikeOpts{len0: 7, len1: 10.5, w0: 1.6, w1: 2.1, fwd: 1.8, back: 0.4, jag: 1, jit: 0.18})
	for i := range sp {
		if math.Cos(sp[i].a-N) < -0.2 {
			sp[i].len *= 0.3
		}
	}
	// the pock mark: a ragged hole with a rim that glows and cools
	var rim []bulletRim
	for y := -2.0; y <= 2; y++ {
		for x := 0.0; x <= 3; x++ {
			d := pixelfx.Hypot((x-1)/1.6, y/2.1) + pixelfx.Rand(x, y, o.Seed())*0.35
			if d < 0.75 {
				rim = append(rim, bulletRim{x, y, false})
			} else if d < 1.25 {
				rim = append(rim, bulletRim{x, y, true})
			}
		}
	}
	var cracks [3][4]float64
	for i := range cracks {
		a, L := R.Range(-1.2, 1.2), R.Range(2, 4.5)
		cracks[i] = [4]float64{math.Cos(a)*2.2 + 1, math.Sin(a) * 2.2, math.Cos(a)*(2.2+L) + 1, math.Sin(a) * (2.2 + L)}
	}

	lights.Add(X-4, Y, 40, C[4], 0.6, 0.22)
	lights.Add(X-2, Y, 14, pixelfx.White, 0.6, 0.06)
	// sparks bounce back off the wall in a cone
	for i := range 18 {
		a := B + R.Range(-1, 1)*0.8
		v := R.Range(170, 450)
		life := R.Range(0.12, 0.4)
		delay := 0.0
		if i > 13 {
			delay = R.Range(0.01, 0.04)
		}
		ps.Spawn(pixelfx.Spark, X-1, Y, math.Cos(a)*v, math.Sin(a)*v-30, life, pixelfx.White, 1, delay, 0)
	}
	// chips of the wall: flung out, falling, bouncing once and settling
	for i := range 8 {
		a := N + R.Range(-0.8, 0.6)
		v := R.Range(60, 190)
		y := Y + R.Range(-1, 1)
		vy := math.Sin(a)*v - R.Range(20, 70)
		life := R.Range(0.9, 1.3)
		c := pixelfx.Pick(R, []uint32{st.light, st.body, st.body, st.lip})
		size := 1.0
		if i < 3 {
			size = 2
		}
		ps.Spawn(pixelfx.Grit, X-1, y, math.Cos(a)*v, vy, life, c, size, 0, 0)
	}
	// a puff of grit dust and a wisp of smoke drifting off
	for range 9 {
		a := N + R.Range(-1.1, 1.1)
		v := R.Range(15, 60)
		y := Y + R.Range(-1.5, 1.5)
		life := R.Range(0.3, 0.55)
		c := pixelfx.Mix(st.dust, pixelfx.White, 0.25)
		if R.Chance(0.5) {
			c = st.dust
		}
		ps.Spawn(pixelfx.Dust, X-2, y, math.Cos(a)*v, math.Sin(a)*v, life, c, R.Range(1.2, 2.3), 0, 0)
	}
	{
		vx, vy, life := R.Range(-10, -4), R.Range(-16, -8), R.Range(0.8, 1.2)
		ps.Spawn(pixelfx.Smoke, X-4, Y, vx, vy, life, st.dust, R.Range(1.8, 2.4), 0.05, 0)
	}

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the pock mark: white-hot, cooling through the ramp to a dark rim, fading at the very end
			heat, fade := math.Pow(pixelfx.Clamp(1-t/0.9), 1.3), 1-pixelfx.Smooth(D-0.45, D-0.1, t)
			if fade > 0 {
				for _, c := range cracks {
					col, a := st.shade, fade*0.85
					if heat > 0.5 {
						col, a = C[3], fade*1
					}
					p.Line(X+c[0], Y+c[1], X+c[2], Y+c[3], col, a)
				}
				for _, q := range rim {
					h := heat * 1.15
					if q.rim {
						h = heat * 0.85
					}
					var c uint32
					switch {
					case !q.rim && h < 0.75:
						c = soot
					case h > 0.85:
						c = pixelfx.White
					case h > 0.6:
						c = C[5]
					case h > 0.4:
						c = C[4]
					case h > 0.22:
						c = C[3]
					case h > 0.1:
						c = C[2]
					default:
						c = st.shade
					}
					p.Dot(X+q.x, Y+q.y, c, fade)
				}
				if heat > 0 {
					p.Glow(X+1, Y, 6+6*heat, C[3], 0.5*heat)
				}
			}
			// a half ring opening away from the wall
			if u := (t - 0.03) / 0.18; u >= 0 && u < 1 {
				r := 5 + 12*pixelfx.Ease.OutCubic(u)
				c := C[3]
				if u < 0.3 {
					c = C[5]
				} else if u < 0.6 {
					c = C[4]
				}
				n := math.Ceil(r * pixelfx.Tau * 1.3)
				for j := 0.0; j < n; j++ {
					g := (j / n) * pixelfx.Tau
					if open(g) {
						p.Dot(X+math.Cos(g)*r, Y+math.Sin(g)*r, c, 1.15-u)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			if t < 0.03 {
				star(p, X, Y, sp, 1, 1, [4]uint32{C[2], C[5], C[4], pixelfx.White}, 1, 2.2)
			} else if t < 0.06 {
				star(p, X, Y, sp, 1.25, 0.55, [4]uint32{C[1], C[4], C[3], C[5]}, 1, 2.2)
			}
			if pop := math.Exp(-t / 0.045); pop > 0.02 {
				p.Glow(X-2, Y, 22, C[3], 0.6*pop)
				p.Glow(X-1, Y, 8, pixelfx.White, 0.5*pop)
				// light grazing the wall's face
				p.GlowLine(X+1, Y-16, X+1, Y+16, 4, C[4], 0.4*pop)
			}
			lights.Draw(p)
		},
	}
}
