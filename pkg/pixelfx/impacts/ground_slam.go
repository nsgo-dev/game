// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/ground-slam.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rocks, grit, rolling floor dust and dust column from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// GroundSlam is something landing hard: the floor flashes and cracks, two crescent shock waves race out along it, a dust wave and a tall dust cloud rise, rocks erupt, bounce and settle. It is registered as "impacts/ground-slam".
var GroundSlam = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Ground slam",
		Desc:     "Something lands hard: the floor flashes and cracks, two crescent shock waves race out along it, a dust wave and a tall dust cloud rise, rocks erupt, bounce and settle.",
		Tags:     []string{"impact", "ground", "slam", "shockwave", "quake", "rocks", "dust", "one-shot"},
		Size:     [2]int{288, 208},
		Origin:   [2]int{144, 164},
		Duration: slamDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1712", "#5e2c1c", "#a4502a", "#e8893a", "#ffc764", "#fff0b4", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "reach", Default: pixelfx.Num(104)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newGroundSlam,
}

func init() { pixelfx.Register("impacts/ground-slam", GroundSlam) }

const slamDuration = 3.2

// slamWave is how long the shock fronts take to run out, in seconds; a variable so sums with it round as pixelfx's do.
var slamWave = 0.6

// crater draws the dent: the floor's edge caves into a shallow dish, its bottom glowing while hot, lips thrown up either side.
func crater(p *pixelfx.Painter, st stoneCols, hot float64, C []uint32, k float64) {
	for x := -12.0; x <= 11; x++ {
		q := math.Abs(x+0.5) / 12
		dep := pixelfx.Round(2.6 * (1 - q*q))
		if q < 0.84 {
			for y := 0.0; y < dep; y++ {
				p.Dot(x, y, soot, k)
			}
			c := st.lip
			if hot > 0.5 {
				c = C[5]
			} else if hot > 0.2 {
				c = C[4]
			}
			p.Dot(x, dep, c, k)
			continue
		}
		c := st.lip
		if q > 0.93 {
			c = st.light
		}
		p.Dot(x, -1, c, k)
		if q < 0.9 {
			p.Dot(x, -2, st.light, k*0.8)
		}
	}
}

type slamEmber struct{ x, y, d, life, ph float64 }

func newGroundSlam(o *pixelfx.Options) pixelfx.Instance {
	C, st, REACH := pixelfx.Fit(o.Palette(), 7), stoneOf(o.Color("stone")), o.Num("reach")
	R, D := pixelfx.NewRng(o.Seed()), float64(slamDuration)
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[6], C[5], C[4], C[3]}})
	bits, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	HOT := [4]uint32{pixelfx.White, C[5], C[4], C[3]}
	dustLit := pixelfx.Mix(st.dust, pixelfx.White, 0.2)
	cracks := makeCracks(R, 10, crackOpts{len0: 22, len1: 58, dive: 0.55, maxY: 22, forks: 0.7, x00: 2, x01: 9})
	front := func(t float64) float64 { return 7 + (REACH-7)*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-0.015)/slamWave)) }
	heat := func(t float64) float64 { return math.Pow(pixelfx.Clamp(1-t/0.8), 1.4) }
	// a front's height and thickness
	crest := func(u float64) (float64, float64) { return 6 + 28*math.Pow(1-u, 0.75), 1.5 + 5*math.Pow(1-u, 0.9) }
	// the flash: a flat half-star hugging the floor, long rays along it, short ones up
	burst := make([]spike, 0, 7)
	burst = append(burst, spike{a: -0.07, len: R.Range(34, 40), w: 1.9})
	burst = append(burst, spike{a: pi + 0.07, len: R.Range(34, 40), w: 1.9})
	for _, s := range [2]struct{ base, jit, l0, l1, w float64 }{{-0.42, 0.08, 15, 20, 1.5}, {-pi + 0.42, 0.08, 15, 20, 1.5}} {
		a := s.base + R.Range(-s.jit, s.jit)
		burst = append(burst, spike{a: a, len: R.Range(s.l0, s.l1), w: s.w})
	}
	for _, s := range [3]struct{ base, jit, l0, l1, w float64 }{{-pi / 2, 0.15, 18, 24, 1.7}, {-1.05, 0.1, 10, 14, 1.2}, {-pi + 1.05, 0.1, 10, 14, 1.2}} {
		a := s.base + R.Range(-s.jit, s.jit)
		burst = append(burst, spike{a: a, len: R.Range(s.l0, s.l1), w: s.w})
	}
	// embers caught in the cracks, flickering as they cool
	var embers []slamEmber
	for _, c := range cracks {
		for _, q := range c.pts {
			if q.d > 3 && q.d < 32 && R.Chance(0.25) {
				life := R.Range(0.8, 2.2)
				embers = append(embers, slamEmber{q.x, q.y, q.d, life, R.Range(0, pixelfx.Tau)})
			}
		}
	}

	// contact: light floods out, sparks fly off the stone and skitter along the floor
	lights.Add(0, -12, 96, C[4], 0.6, 0.6)
	lights.Add(0, -4, 50, pixelfx.White, 0.9, 0.14)
	for i := range 56 {
		low, s := i < 14, R.Sign()
		var a, sp, life, delay float64
		switch {
		case low && s > 0:
			a = -R.Range(0.04, 0.6)
		case low:
			a = pi + R.Range(0.04, 0.6)
		default:
			a = -pi/2 + R.Range(-1.15, 1.15)
		}
		if low {
			sp = R.Range(150, 380)
		} else {
			sp = R.Range(200, 500)
		}
		x := R.Range(-4, 4)
		if low {
			life = R.Range(0.2, 0.5)
		} else {
			life = R.Range(0.25, 0.65)
		}
		if !low {
			delay = R.Range(0, 0.05)
		}
		ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, delay, 0)
	}
	// rocks erupt from the dent: three heavy ones that barely clear it, the rest thrown high
	for i := range 20 {
		s := 4.0
		if i >= 3 {
			s = R.Int(1, 3)
		}
		a := -pi/2 + R.Range(-0.5, 0.5)
		var sp float64
		if s > 3 {
			sp = R.Range(110, 190)
		} else {
			sp = R.Range(170, 380)
			if s > 2 {
				sp *= 0.8
			} else {
				sp *= 1
			}
		}
		var b pixelfx.Bit
		b.Kind = pixelfx.Chunk
		b.X = R.Range(-7, 7)
		b.Y = -2
		b.VX, b.VY = math.Cos(a)*sp, math.Sin(a)*sp
		b.G, b.Drag, b.Bounce = 1, 0.4, 0.38
		b.Life = R.Range(1.9, 2.7)
		b.A = R.Range(0, pixelfx.Tau)
		b.VA = R.Range(-14, 14)
		b.S = s
		b.Pts = rockShape(R, 6)
		bits.Add(b)
	}
	// grit: a fan of floor bits that bounce once and settle
	gritCols := []uint32{st.light, st.body, st.body, st.shade, st.shade, st.lip}
	for range 110 {
		u := R.Range(-1, 1)
		a, sp := -pi/2+u*0.65, R.Range(110, 380)*(1-0.35*math.Abs(u))
		life := R.Range(1, 2.2)
		c := pixelfx.Pick(R, gritCols)
		size := 1.0
		if R.Chance(0.35) {
			size = 2
		}
		ps.Spawn(pixelfx.Grit, u*6, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, c, size, 0, 0)
	}
	// the dust cloud: a column that shoots up and spreads at the top, lit on top, and a heavy bank at its foot
	for i := range 28 {
		core, s := i < 14, R.Sign()
		var x, vx, vy, size float64
		if core {
			x = R.Range(-5, 5)
		} else {
			x = s * R.Range(4, 12)
		}
		y := -R.Range(2, 10)
		if core {
			vx = R.Range(-12, 12)
		} else {
			vx = s * R.Range(14, 38)
		}
		vy = -R.Range(50, 130)
		if core {
			vy *= 1
		} else {
			vy *= R.Range(0.3, 0.6)
		}
		life := R.Range(1.9, 2.7)
		c := st.dust
		if R.Chance(0.35) {
			c = dustLit
		}
		if core {
			size = R.Range(4.5, 9)
		} else {
			size = R.Range(4, 7.5)
		}
		ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, c, size, R.Range(0.02, 0.22), pixelfx.Big)
	}
	for range 10 {
		s := R.Sign()
		ps.Spawn(pixelfx.Smoke, s*R.Range(2, 14), -R.Range(2, 6), s*R.Range(20, 70), -R.Range(4, 20), R.Range(1.4, 2.3), st.dust, R.Range(6, 9), R.Range(0, 0.08), pixelfx.Big)
	}
	// fine dust settling out of the cloud while it hangs
	for range 26 {
		x, y, vx, vy, life := R.Range(-22, 22), -R.Range(14, 80), R.Range(-12, 12), R.Range(-10, 4), R.Range(1.2, 2)
		c := dustLit
		if R.Chance(0.5) {
			c = st.lip
		}
		ps.Spawn(pixelfx.Flake, x, y, vx, vy, life, c, R.Range(0.5, 1.2), R.Range(0.25, 1), 0)
	}

	t := 0.0
	acc := [2]float64{R.Next(), R.Next()}
	ring := func(p *pixelfx.Painter, back bool) {
		u := pixelfx.Clamp((t - 0.015) / (slamWave + 0.1))
		if t < 0.015 || u >= 0.9 {
			return
		}
		k := (0.9 - u) / 0.35
		if u < 0.55 {
			k = 1.1
		}
		ramp := []uint32{st.body, st.light, C[4], C[5]}
		if u < 0.35 {
			ramp = []uint32{C[3], C[4], C[5], C[6]}
		}
		pixelfx.Band(p, 0, 0.5, front(t)+1, 0.16, math.Max(1, 4*math.Pow(1-u, 0.8)), k, ramp, func(g float64) bool { return (math.Sin(g) < 0) == back }, t < 0.09)
	}
	fronts := func(p *pixelfx.Painter) {
		u := pixelfx.Clamp((t - 0.03) / (slamWave + 0.16))
		if t < 0.03 || u >= 1 {
			return
		}
		fr := front(t)
		h, w := crest(u)
		k := 1.0
		if u >= 0.7 {
			k = (1 - u) / 0.3
		}
		var cols [3]uint32
		switch {
		case u < 0.14:
			cols = [3]uint32{pixelfx.White, C[6], C[5]}
		case u < 0.34:
			cols = [3]uint32{C[6], C[5], C[4]}
		case u < 0.55:
			cols = [3]uint32{C[5], C[4], st.lip}
		default:
			cols = [3]uint32{st.lip, st.light, st.dust}
		}
		for _, s := range [2]float64{-1, 1} {
			frontCrescent(p, s*fr, s, h, w, cols, k)
			p.Glow(s*fr, -h*0.3, 8+h*0.7, C[4], 0.5*(1-u)*k)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// dust kicked up behind the racing fronts, and spray off their crests
			if u := pixelfx.Clamp((t - 0.015) / slamWave); u > 0 && u < 1 {
				fr := front(t)
				h, _ := crest(u)
				for k := range 2 {
					s := -1.0
					if k != 0 {
						s = 1
					}
					for acc[k] += dt * 120 * (1 - u*0.7); acc[k] > 1; acc[k]-- {
						x := s * (fr - R.Range(1, 7))
						y := -R.Range(0.5, 3)
						vx := s * R.Range(15, 70) * (1 - u)
						vy := -R.Range(2, 12)
						life := R.Range(1, 2.1)
						c := st.dust
						if R.Chance(0.3) {
							c = dustLit
						}
						ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, c, R.Range(2.5, 6.5)*(1-0.4*u), 0, pixelfx.Big)
						if R.Chance(0.5) {
							x, y, vx, vy, life := s*(fr-R.Range(0, 3)), -R.Range(0.3, 0.9)*h, s*R.Range(-15, 25), -R.Range(8, 35), R.Range(0.3, 0.6)
							c := st.dust
							if R.Chance(0.5) {
								c = dustLit
							}
							ps.Spawn(pixelfx.Dust, x, y, vx, vy, life, c, R.Range(1.2, 2.4), 0, 0)
						}
						if R.Chance(0.4) {
							vx, vy, life := s*R.Range(10, 60), -R.Range(40, 140)*(1-u), R.Range(0.5, 1)
							ps.Spawn(pixelfx.Grit, s*fr, -1, vx, vy, life, pixelfx.Pick(R, []uint32{st.light, st.body, st.lip}), 1, 0, 0)
						}
						if u < 0.45 && R.Chance(0.2) {
							ps.Spawn(pixelfx.Spark, s*fr, -1, s*R.Range(60, 200), -R.Range(30, 160), R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
						}
					}
				}
			}
			for i := range bits.List {
				bits.List[i].PVY = bits.List[i].VY
			}
			bits.Update(dt)
			// rocks that land hard kick up dust
			for i := range bits.List {
				b := &bits.List[i]
				if b.PVY > 70 && b.VY <= 0 {
					n := 2
					if b.PVY > 200 {
						n = 3
					}
					for range n {
						x := b.X + R.Range(-1.5, 1.5)
						vx := R.Sign() * R.Range(8, 35)
						ps.Spawn(pixelfx.Dust, x, -0.5, vx, -R.Range(2, 16), R.Range(0.3, 0.65), st.dust, R.Range(1.3, 2.4)*(0.7+b.S*0.25), 0, 0)
					}
					b.PVY = 0
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			fade, H := 1-pixelfx.Smooth(D-0.9, D-0.15, t), heat(t)
			if t > 0.02 {
				crater(p, st, H, C, fade)
			}
			drawCracks(p, cracks, func(c *crack) float64 { return c.len * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-c.t0)/0.1)) }, func(d float64) float64 { return H * pixelfx.Clamp(1.2-d/40) }, HOT, soot, st.light, fade)
			for _, e := range embers {
				k := 1 - (t-0.1)/e.life
				if t < 0.1 || k <= 0 {
					continue
				}
				f := 0.6 + 0.4*math.Sin(t*23+e.ph)
				c := C[3]
				if k*f > 0.55 {
					c = C[5]
				} else if k*f > 0.25 {
					c = C[4]
				}
				p.Px(e.x, e.y, c)
				p.Glow(e.x, e.y, 4, C[4], 0.35*k*f)
			}
			ring(p, true)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, false)
			fronts(p)
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			ps.Draw(p, pixelfx.LayerFront)
			if t < 0.035 {
				p.EllipseFn(0, 0, 30, 4.5, func(d, _ float64) (uint32, float64) {
					switch {
					case d > 0.8:
						return C[4], 1
					case d > 0.5:
						return C[6], 1
					}
					return pixelfx.White, 1
				})
				star(p, 0, -1, burst, 1, 1, [4]uint32{C[4], C[6], pixelfx.White, pixelfx.White}, 1, 2.4)
			} else if t < 0.07 {
				star(p, 0, -1, burst, 1.25, 0.5, [4]uint32{C[3], C[5], C[6], pixelfx.White}, 1, 2.4)
			}
			// the floor flashes along its length, then the dent glows while the cracks are hot
			if t < 0.14 {
				p.GlowLine(-80, 0, 80, 0, 10, C[5], 0.55*(1-t/0.14))
			}
			if H := heat(t); H > 0 {
				p.Glow(0, 1, 46, C[4], 0.35*H)
			}
			lights.Draw(p)
		},
	}
}
