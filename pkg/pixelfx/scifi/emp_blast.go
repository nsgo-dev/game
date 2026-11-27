// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/emp-blast.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source lightning impact (https://www.spritefusion.com/pixel-art-vfx/lightning) for the pop (white disc, fast ring, bursting sparks, crawling arcs), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// EmpBlast is a disc charge that ticks, pops white, and swells into a bubble of rolling scanlines with arcs crawling its skin before strobing, tearing and shorting out into falling dead pixels. It is registered as "scifi/emp-blast".
var EmpBlast = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "EMP blast",
		Desc:     "A disc charge ticks, pops white, and a bubble of rolling scanlines swells out with arcs crawling its skin; it strobes, tears and shorts out into falling dead pixels.",
		Tags:     []string{"scifi", "emp", "pulse", "shockwave", "electric", "grenade", "glitch", "one-shot"},
		Size:     [2]int{152, 116},
		Origin:   [2]int{76, 84},
		Duration: empDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0d0820", "#1c1658", "#2d2aa0", "#3f55e8", "#5a8eff", "#9cc4ff", "#dceaff", "#ffffff")},
			{Name: "fringe", Default: pixelfx.Color("#ff5ad8")},
			{Name: "radius", Default: pixelfx.Num(56)},
			{Name: "seed", Default: pixelfx.Num(33)},
		},
	},
	New: newEmpBlast,
}

func init() { pixelfx.Register("scifi/emp-blast", EmpBlast) }

const empDuration = 3.2

// the beats: it ticks, pops, the bubble swells and holds, strobes, breaks
var (
	empPop    = 0.42
	empGrow   = 0.32
	empStrobe = 1.25
	empBreak  = 1.58
	empSmoke1 = pixelfx.Hex("#3c3a52")
	empSmoke2 = pixelfx.Hex("#2f2c3c")
)

type empArc struct {
	t    float64
	pts  [][2]float64
	life float64
}

type empDead struct {
	x, y, vx, vy, life, max, s, ph float64
	c                              uint32
}

func newEmpBlast(o *pixelfx.Options) pixelfx.Instance {
	C, F, RM, seed := pixelfx.Fit(o.Palette(), 8), o.Color("fringe"), o.Num("radius"), o.Seed()
	R := pixelfx.NewRng(seed)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}, Haze: C[5]})
	lights := &pixelfx.Lights{}
	var arcs []empArc
	var dead []empDead
	var star [][][2]float64
	t, popped, broke, nextArc, starAt := 0.0, false, false, 0.0, -1.0

	radius := func() float64 {
		if t < empPop {
			return 0
		}
		return RM * (0.12 + 0.88*pixelfx.Ease.OutExpo(pixelfx.Clamp((t-empPop)/empGrow))) * (1 + 0.04*pixelfx.Clamp((t-empPop-empGrow)/0.8))
	}
	// which frames the bubble cuts out in while it fails
	off := func() bool {
		return t > empStrobe && t < empBreak && pixelfx.Rand(seed, math.Floor(t*30)) < 0.2+0.5*((t-empStrobe)/(empBreak-empStrobe))
	}

	// the charge: a mote of light just off the floor, swelling, blinking faster, crackling
	charge := func(p *pixelfx.Painter) {
		if t >= empPop {
			return
		}
		k := t / empPop
		r := 1 + 3.2*k*k
		tick := math.Sin(tau*(2*t+10*t*t)) > 0
		p.Circle(0, -7, r+1, C[4], 1)
		c := C[6]
		if tick {
			c = pixelfx.White
		}
		p.Circle(0, -7, r, c, 1)
		f := math.Floor(t * 30)
		rr := pixelfx.NewRng(seed*97 + f)
		n := 1 + int(math.Floor(k*3))
		for range n {
			a, l := rr.Range(0, tau), r+2+rr.Range(2, 6)*k
			strokeArc(p, pixelfx.Bolt(rr, math.Cos(a)*r, -7+math.Sin(a)*r, math.Cos(a)*l, -7+math.Sin(a)*l, 0.4, 2), [3]uint32{C[2], C[5], C[4]}, 0.7, 0.5)
		}
		tk := 0.6
		if tick {
			tk = 1
		}
		p.Glow(0, -7, 8+16*k, C[4], 0.4+0.5*k*tk)
	}

	// the bubble: thin glass, brighter toward its rim, interference lines rolling down it, tears across it
	bubble := func(p *pixelfx.Painter, near bool) {
		r := radius()
		if r <= 1 || t >= empBreak || off() {
			return
		}
		age, slot := t-empPop, math.Floor(t*30)
		roll, ry := age*40, r*0.16
		// the tears: a few rows this frame shifted sideways and burning bright
		tears := map[int]float64{}
		nt := 2
		if t > empStrobe {
			nt += 4
		}
		for i := range nt {
			fi := float64(i)
			row := int(math.Floor(-pixelfx.Rand(seed, slot, fi) * r))
			dir := 1.0
			if pixelfx.Rand(seed, slot, fi, 1) < 0.5 {
				dir = -1
			}
			tears[row] = dir * (2 + math.Floor(pixelfx.Rand(seed, slot, fi, 2)*4))
		}
		for y := math.Floor(-r); y <= math.Ceil(ry); y++ {
			shift := tears[int(y)]
			for x := math.Floor(-r); x <= math.Ceil(r); x++ {
				sx := x - shift
				vy := 0.0
				if y < 0 {
					vy = y + 0.5
				}
				d := pixelfx.Hypot(sx+0.5, vy) / r
				// a split edge just outside the rim, as if the signal's colors had slipped
				if d > 1 {
					if near && y < 0 && d < 1+1.4/r && (int(x)+int(y))%2 == 0 {
						p.Px(x, y, F)
					}
					continue
				}
				// the dome above the floor, the floor disc below it only as its rim
				if y >= 0 {
					e := pixelfx.Hypot((sx+0.5)/r, (y+0.5)/ry)
					if e > 1 || e < 0.92 || (y > 0) != near {
						continue
					}
					c := C[4]
					if e > 0.97 {
						c = C[6]
					}
					p.Px(x, y, c)
					continue
				}
				if !near {
					continue
				}
				rim := math.Pow(d, 6)
				line := pixelfx.Wrap(y-roll, 4) < 1
				band := pixelfx.Wrap(y-roll*0.5, 22) < 4
				switch {
				case d > 0.965:
					c := C[6]
					if shift != 0 || (-y > r*0.55 && sx < 0) {
						c = pixelfx.White
					}
					p.Px(x, y, c)
				case d > 0.93:
					p.Px(x, y, C[4])
				case shift != 0:
					c := C[5]
					if math.Abs(sx) > r*0.8 {
						c = F
					} else if pixelfx.ToInt32(x)&1 != 0 {
						c = C[6]
					}
					p.Px(x, y, c)
				default:
					c := C[3]
					switch {
					case line && band:
						c = C[6]
					case line:
						c = C[5]
					case band:
						c = C[4]
					}
					a := 0.06 + 0.42*rim
					if line {
						a += 0.14 + 0.24*rim
					}
					if band {
						a += 0.16
					}
					p.Blend(x+0.5, y+0.5, c, a)
				}
			}
			if shift != 0 && near {
				p.GlowLine(-r, y, r, y, 4, C[5], 0.3)
			}
		}
		if near {
			for a := 0.15; a < pi-0.1; a += 0.35 {
				p.Glow(math.Cos(a)*r, -math.Sin(a)*r, 8, C[4], 0.12)
			}
			// junk blocks flicker on inside it
			for i := range 3 {
				fi := float64(i)
				k := pixelfx.Rand(seed, slot, fi, 7)
				if k > 0.5 {
					continue
				}
				a := pixelfx.Rand(seed, slot, fi, 8) * pi
				d := pixelfx.Rand(seed, slot, fi, 9) * r * 0.85
				bx, by := pixelfx.Round(math.Cos(a)*d), pixelfx.Round(-math.Sin(a)*d)
				c := C[5]
				if k < 0.2 {
					c = pixelfx.White
				}
				p.Rect(bx, by, 2+math.Floor(k*8), 1+math.Floor(k*4), c, 1)
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// charging: energy streaks in to the puck
			if t < empPop-0.05 && R.Chance(0.6) {
				a, D, T := R.Range(-pi, 0), R.Range(16, 40), R.Range(0.14, 0.22)
				v := (D * 3) / (math.Exp(3*T) - 1)
				c := C[5]
				if R.Chance(0.5) {
					c = C[6]
				}
				ps.Spawn(pixelfx.Streak, math.Cos(a)*D, -5+math.Sin(a)*D*0.7, -math.Cos(a)*v, -math.Sin(a)*v*0.7, T, c, 1, 0, 0)
			}
			// the discharge: a star of thick arcs out of the pop, jittering each frame as it shrinks
			sa, sf := t-empPop, math.Floor(t*30)
			if sa >= 0 && sa < 0.26 && sf != starAt {
				starAt = sf
				var ln float64
				if sa < 0.05 {
					ln = pixelfx.OutBack(sa/0.05, 2)
				} else {
					ln = 1 - (sa-0.05)/0.26
				}
				star = star[:0]
				for i := range 7 {
					fi := float64(i)
					a := -pi + (fi/6)*pi + pixelfx.Rand(seed, fi, 3)*0.3 - 0.15 + R.Range(-0.1, 0.1)
					L := (20 + 16*pixelfx.Rand(seed, fi, 5)) * math.Max(0.15, ln)
					star = append(star, pixelfx.Bolt(R, 0, -7, math.Cos(a)*L, math.Min(-1, -7+math.Sin(a)*L), 0.28, 4))
				}
			}
			if sa >= 0.26 {
				star = star[:0]
			}
			if t >= empPop && !popped {
				popped = true
				for range 16 {
					a, sp := R.Range(-pi, 0), R.Range(50, 150)
					life := R.Range(0.3, 0.6)
					col := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
					ps.Spawn(pixelfx.Mote, 0, -7, math.Cos(a)*sp, math.Sin(a)*sp, life, col, 0.8, 0, 0)
				}
				lights.Add(0, -20, 72, C[4], 0.9, 0.5)
				lights.Add(0, -7, 30, pixelfx.White, 0.9, 0.12)
				for range 60 {
					a, sp := R.Range(-pi, 0), R.Range(90, 220)
					ps.Spawn(pixelfx.Spark, 0, -7, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.6), pixelfx.White, 1, 0, 0)
				}
				for range 6 {
					x, vx, vy := R.Range(-3, 3), R.Range(-12, 12), -R.Range(10, 24)
					life, size, delay := R.Range(1, 1.6), R.Range(2.5, 4), R.Range(0.05, 0.3)
					ps.Spawn(pixelfx.Smoke, x, -4, vx, vy, life, empSmoke1, size, delay, 0)
				}
			}
			// arcs crawl over the bubble's skin, faster as it fails
			if popped && !broke && t >= nextArc {
				if t > empStrobe {
					nextArc = t + R.Range(0.02, 0.05)
				} else {
					nextArc = t + R.Range(0.05, 0.11)
				}
				r := radius()
				a0 := R.Range(0.1, pi-0.1)
				a1 := a0 + R.Sign()*R.Range(0.25, 0.6)
				rr := r * R.Range(0.92, 1)
				pts := pixelfx.Bolt(R, math.Cos(a0)*rr, -math.Sin(a0)*rr, math.Cos(a1)*rr, -math.Max(0.05, math.Sin(a1))*rr, 0.3, 4)
				life := R.Range(0.05, 0.1)
				arcs = append(arcs, empArc{t: t, pts: pts, life: life})
				if R.Chance(0.35) {
					ps.Spawn(pixelfx.Spark, math.Cos(a1)*rr, -math.Max(0.05, math.Sin(a1))*rr, R.Range(-80, 80), R.Range(-90, 10), R.Range(0.15, 0.3), pixelfx.White, 1, 0, 0)
				}
			}
			if t >= empBreak && !broke {
				broke = true
				// it shorts out: the skin bursts into dead pixels, a last crackle, the charge smokes
				r := radius()
				lights.Add(0, -r*0.4, r*1.3, C[5], 0.8, 0.3)
				for range 70 {
					a, d := R.Range(0.05, pi-0.05), r*R.Range(0.6, 1)
					vx, vy := math.Cos(a)*R.Range(5, 25), -R.Range(0, 20)
					life := R.Range(0.4, 1)
					s := 1.0
					if R.Chance(0.3) {
						s = 2
					}
					c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5], C[4]})
					ph := R.Range(0, 9)
					dead = append(dead, empDead{x: math.Cos(a) * d, y: -math.Sin(a) * d, vx: vx, vy: vy, life: life, max: 1, s: s, c: c, ph: ph})
				}
				for range 6 {
					a0 := R.Range(0.1, pi-0.1)
					a1 := a0 + R.Range(-0.5, 0.5)
					pts := pixelfx.Bolt(R, math.Cos(a0)*r, -math.Sin(a0)*r, math.Cos(a1)*r*0.6, -math.Max(0.05, math.Sin(a1))*r*0.6, 0.35, 4)
					arcs = append(arcs, empArc{t: t, pts: pts, life: 0.1})
				}
				for range 8 {
					x, vx, vy := R.Range(-3, 3), R.Range(-6, 6), -R.Range(8, 18)
					life, size, delay := R.Range(0.8, 1.2), R.Range(2, 3.4), R.Range(0, 0.35)
					ps.Spawn(pixelfx.Smoke, x, -4, vx, vy, life, empSmoke2, size, delay, 0)
				}
			}
			// the dead charge fizzes now and then
			if broke && t < empDuration-0.8 && R.Chance(0.05) {
				ps.Spawn(pixelfx.Spark, R.Range(-3, 3), -3, R.Range(-60, 60), -R.Range(40, 120), R.Range(0.15, 0.3), pixelfx.White, 1, 0, 0)
			}
			for i := len(arcs) - 1; i >= 0; i-- {
				if t-arcs[i].t > arcs[i].life {
					arcs = slices.Delete(arcs, i, i+1)
				}
			}
			for i := len(dead) - 1; i >= 0; i-- {
				d := &dead[i]
				if d.life -= dt; d.life <= 0 {
					dead = slices.Delete(dead, i, i+1)
					continue
				}
				d.vy += 60 * dt
				d.x += d.vx * dt
				d.y = math.Min(-1, d.y+d.vy*dt)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			bubble(p, false)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			charge(p)
			bubble(p, true)
			// the pop: a white disc, a ring racing out ahead of the bubble, a flare
			a := t - empPop
			if a >= 0 && a < 0.06 {
				p.Circle(0, -7, 12*(1-a/0.06)+3, C[6], 1)
				p.Circle(0, -7, 9*(1-a/0.06)+1, pixelfx.White, 1)
			}
			if a >= 0 && a < 0.4 {
				k := a / 0.4
				rr := 6 + (RM+14)*pixelfx.Ease.OutCubic(k)
				pixelfx.Band(p, 0, 0, rr, 0.16, math.Max(1, 3*(1-k)), 1.2-k, []uint32{C[3], C[5], C[6], pixelfx.White}, nil, k < 0.3)
			}
			if a >= 0 && a < 0.16 {
				pixelfx.Sparkle(p, 0, -7, 10*(1-a/0.16), C[6], C[5], 0.6)
			}
			for _, b := range star {
				strokeArc(p, b, [3]uint32{C[3], C[6], C[5]}, 1-(t-empPop)/0.3, 1.3)
			}
			for _, c := range arcs {
				strokeArc(p, c.pts, [3]uint32{C[2], C[5], C[4]}, 1-(t-c.t)/(c.life+0.03), 0.8)
			}
			// dead pixels: drifting down, blinking, going out
			for _, d := range dead {
				if math.Sin(t*40+d.ph) < -0.3 && d.life < 0.6 {
					continue
				}
				c := C[3]
				if d.life > 0.5 {
					c = d.c
				}
				p.Rect(d.x, d.y, d.s, d.s, c, 1)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
