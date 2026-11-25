// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/firework-burst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The flare, the twinkling crosses and the glitter are adapted from Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// FireworkBurst is a shell that climbs, slows and bursts into a sphere of coloured trailing stars round a gold core; they droop, crackle into white pops and fade, leaving glitter. It is registered as "explosions/firework-burst".
var FireworkBurst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Firework burst",
		Desc:     "A shell climbs, slows and bursts into a sphere of coloured trailing stars round a gold core; they droop, crackle into white pops and fade, leaving glitter.",
		Tags:     []string{"firework", "celebration", "sparkle", "sky", "burst", "one-shot"},
		Size:     [2]int{240, 248},
		Origin:   [2]int{120, 100},
		Duration: 3.6,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#3d0a33", "#701560", "#b0228f", "#f53cd1", "#ff86e6", "#ffd2f6", "#ffffff")},
			{Name: "core", Default: pixelfx.Palette("#4a2c08", "#86520c", "#c48412", "#f0b424", "#ffe066", "#fff6c4", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newFireworkBurst,
}

func init() { pixelfx.Register("explosions/firework-burst", FireworkBurst) }

// variables, so sums of them round as pixelfx's do
var burstRise, burstY0, burstDrag, burstG, burstTrail, burstTurn = 0.42, 72.0, 2.6, 88.0, 0.34, 0.22

var fireworkSmoke = pixelfx.Hex("#4a4458")

type burstStar struct {
	x, y, vx, vy, life, age float64
	ramp, first             []uint32
	big                     bool
	pts                     []float64 // x, y, t triples, oldest first
	acc, seed               float64
}

// pop is a white crackle where a star burns out.
type pop struct {
	x, y, t0, life float64
	c              uint32
}

func newFireworkBurst(o *pixelfx.Options) pixelfx.Instance {
	C, K, R := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("core"), 7), pixelfx.NewRng(o.Seed())
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{K[6], K[5], K[4], K[3]}}), &pixelfx.Lights{}
	var stars []burstStar
	var pops []pop
	t, burst := 0.0, false

	// a shell of stars thrown over a sphere: seen side-on, the ones flying at us or away look slower; the outer stars burn gold for a beat, then turn to their color
	shell := func(n int, speed float64, ramp []uint32, life [2]float64, first []uint32) {
		for range n {
			z, a := R.Range(-1, 1), R.Range(0, tau)
			s, v := math.Sqrt(1-z*z), speed*R.Range(0.93, 1.05)
			var st burstStar
			st.vx, st.vy = math.Cos(a)*s*v, math.Sin(a)*s*v
			st.life = R.Range(life[0], life[1])
			st.ramp, st.first = ramp, first
			st.big = R.Chance(0.2)
			st.acc = R.Next()
			st.seed = R.Range(0, 100)
			stars = append(stars, st)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the shell climbs, slowing, shedding a fizz of gold sparks
			if t < burstRise {
				y := burstY0 * (1 - pixelfx.Ease.OutCubic(t/burstRise))
				if R.Chance(50 * dt) {
					ps.Spawn(pixelfx.Spark, R.Range(-0.5, 0.5), y+2, R.Range(-25, 25), R.Range(20, 70), R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(14 * dt) {
					ps.Spawn(pixelfx.Vapour, R.Range(-1, 1), y+3, R.Range(-3, 3), R.Range(5, 15), R.Range(0.5, 0.8), fireworkSmoke, R.Range(1.5, 2.5), 0, 0)
				}
			}
			if !burst && t >= burstRise {
				burst = true
				lights.Add(0, 0, 110, C[4], 0.6, 1.5)
				lights.Add(0, 0, 30, pixelfx.White, 0.5, 0.07)
				shell(66, 236, C, [2]float64{1.35, 1.9}, K)
				shell(26, 120, K, [2]float64{1.0, 1.4}, K)
				// the burst charge's smoke, drifting where the shell broke
				for range 7 {
					a, d := R.Range(0, tau), R.Range(0, 8)
					ps.Spawn(pixelfx.Vapour, math.Cos(a)*d, math.Sin(a)*d, math.Cos(a)*R.Range(10, 30), math.Sin(a)*R.Range(10, 30), R.Range(2.2, 3), fireworkSmoke, R.Range(6, 11), 0, 0)
				}
				for range 24 {
					a, sp := R.Range(0, tau), R.Range(40, 140)
					ps.Spawn(pixelfx.Spark, 0, 0, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
				}
			}
			for i := len(stars) - 1; i >= 0; i-- {
				s := &stars[i]
				s.age += dt
				k := s.age / s.life
				if k >= 1 {
					// the star crackles out in a few white pops
					for j := 0; j < 2+pick(s.big, 1, 0); j++ {
						var q pop
						q.x = s.x + R.Range(-3, 3)
						q.y = s.y + R.Range(-3, 3)
						q.t0 = t + R.Range(0, 0.18)
						q.life = R.Range(0.06, 0.12)
						q.c = pick(R.Chance(0.6), pixelfx.White, s.ramp[5])
						pops = append(pops, q)
					}
					stars = slices.Delete(stars, i, i+1)
					continue
				}
				s.vx -= s.vx * burstDrag * dt
				s.vy -= s.vy*burstDrag*dt - burstG*dt
				s.x += s.vx * dt
				s.y += s.vy * dt
				s.pts = append(s.pts, s.x, s.y, t)
				for len(s.pts) > 3 && t-s.pts[2] > burstTrail {
					s.pts = s.pts[3:]
				}
				// glitter shed along the way: specks that hang, twinkle and fall
				for s.acc += dt * pick(k < 0.25, 4.0, 11); s.acc > 1; s.acc-- {
					ps.Spawn(pixelfx.StarKind, s.x, s.y, s.vx*0.15+R.Range(-6, 6), s.vy*0.15+R.Range(0, 10), R.Range(0.35, 0.8), pick(R.Chance(0.5), K[5], pixelfx.White), rangeOr(R, 0.15, 1, 1.6, 0.5), 0, 0)
				}
			}
			pops = slices.DeleteFunc(pops, func(q pop) bool { return t > q.t0+q.life })
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < burstRise {
				// the shell: a hot dot with a short tail
				u := t / burstRise
				y := burstY0 * (1 - pixelfx.Ease.OutCubic(u))
				p.LineFn(0, y, 0, y+3+6*(1-u), func(q float64) (uint32, float64) { return pick(q < 0.3, K[5], K[3]), 1 })
				p.Px(0, y, pixelfx.White)
				p.Glow(0, y, 7, K[4], 0.5)
			}
			for i := range stars {
				s := &stars[i]
				k, P2 := s.age/s.life, s.pts
				n := len(P2) / 3
				// the trail: a 1 px line from the head back along where it has been, cooling and dithering out; the part laid while the star burned gold stays gold
				for j := n - 1; j > 0; j-- {
					u := (t - P2[j*3+2]) / burstTrail
					if u >= 1 {
						break
					}
					ramp := pick(s.age-(t-P2[j*3+2]) < burstTurn, s.first, s.ramp)
					c := ramp[3]
					if u < 0.3 {
						c = ramp[5]
					} else if u < 0.6 {
						c = ramp[4]
					}
					p.Line(P2[j*3], P2[j*3+1], P2[j*3-3], P2[j*3-2], c, math.Pow(1-u, 0.8)*(1.3-k*0.6))
				}
				// the head: white-hot, then its color; late on it flickers before it crackles out
				if k > 0.72 && math.Sin(t*60+s.seed) < -0.2 {
					continue
				}
				// white only on the burst frame; the glow comes in as the stars spread, so the heart never blooms into a blob
				ramp := pick(s.age < burstTurn, s.first, s.ramp)
				head := ramp[5]
				switch {
				case s.age < 0.04:
					head = pixelfx.White
				case k < 0.55:
					head = ramp[pick(s.age < burstTurn, 5, 6)]
				}
				p.Px(s.x, s.y, head)
				if s.big && k < 0.6 && s.age > 0.06 {
					for _, d := range [][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						p.Px(s.x+d[0], s.y+d[1], ramp[5])
					}
				}
				p.Glow(s.x, s.y, pick(s.big, 7.0, 5), ramp[4], pick(s.big, 0.7, 0.5)*(1-k*0.6)*math.Min(1, s.age/0.2))
			}
			for _, q := range pops {
				u := (t - q.t0) / q.life
				if u < 0 || u > 1 {
					continue
				}
				pixelfx.Sparkle(p, q.x, q.y, 2.6*math.Sin(pi*u), q.c, pixelfx.Mix(q.c, C[3], 0.5), 0.5)
			}
			// the burst: a flare and a white heart, the sky lit up behind it
			if b := t - burstRise; b >= 0 && b < 0.28 {
				u := b / 0.28
				L := 32 * math.Pow(1-u, 1.3)
				flareRays(p, 0, 0, L, u*0.6, C, K)
				if b < 0.07 {
					p.Circle(0, 0, 2.6-b*20, pixelfx.White, 1)
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}

// flareRays draws an 8-ray flare: long rays on the axes with a white core and tinted ends, short gold diagonals, turning slowly.
func flareRays(p *pixelfx.Painter, x, y, L, ang float64, C, K []uint32) {
	if L < 1 {
		return
	}
	for i := range 8 {
		diag := i%2 == 1
		an := ang + (float64(i)*tau)/8
		ln := pick(diag, L*0.42, L)
		ca, sa := math.Cos(an), math.Sin(an)
		for d := 0.0; d <= ln; d += 0.5 {
			u := d / ln
			var c uint32
			switch {
			case diag:
				c = pick(u < 0.5, K[5], K[4])
			case u < 0.3:
				c = pixelfx.White
			case u < 0.65:
				c = C[5]
			default:
				c = C[4]
			}
			a := 1.0
			if u >= 0.7 {
				a = 1 - (u-0.7)/0.35
			}
			p.Dot(x+ca*d, y+sa*d, c, a)
		}
	}
	p.Glow(x, y, 4+L*1.2, C[4], 0.3)
	p.Glow(x, y, 2+L*0.3, pixelfx.White, 0.25)
}
