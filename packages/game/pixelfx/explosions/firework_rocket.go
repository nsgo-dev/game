// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/firework-rocket.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The flare, the twinkling glitter and the crackle are adapted from Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// FireworkRocket is a rocket that fizzes off the ground on a sparkling gold trail, coasts, and bursts into a palm: heavy gold fronds arc out, droop on glittering trails and crackle at the tips. It is registered as "explosions/firework-rocket".
var FireworkRocket = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Firework rocket",
		Desc:     "A rocket fizzes off the ground on a sparkling gold trail, coasts, and bursts into a palm: heavy gold fronds arc out, droop on glittering trails and crackle at the tips.",
		Tags:     []string{"firework", "rocket", "celebration", "sparkle", "sky", "one-shot"},
		Size:     [2]int{rocketW, rocketH},
		Origin:   [2]int{rocketOX, rocketOY},
		Duration: 3.8,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1406", "#6a3a0c", "#b8701a", "#f0a830", "#ffd860", "#fff2b0", "#ffffff")},
			{Name: "tips", Default: pixelfx.Palette("#0c2a3a", "#1a5a78", "#36a2c8", "#7cd8f0", "#c8f4ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	New: newFireworkRocket,
}

const rocketW, rocketH, rocketOX, rocketOY = 300, 300, 150, 288

func init() { pixelfx.Register("explosions/firework-rocket", FireworkRocket) }

// variables, so sums of them round as pixelfx's do
var rocketFly, rocketHigh, rocketDrag, rocketG, rocketTrail = 1.05, 192.0, 1.35, 105.0, 1.15

var (
	rocketSmoke = pixelfx.Hex("#5a5468")
	rocketStick = pixelfx.Hex("#5a4030")
	rocketTube  = pixelfx.Hex("#b8303a")
	rocketLit   = pixelfx.Hex("#e86070")
	rocketCap   = pixelfx.Hex("#d8d0c0")
)

type frond struct {
	x, y, vx, vy, age, life float64
	pts                     []float64 // x, y, t triples, oldest first
	acc, seed               float64
}

func newFireworkRocket(o *pixelfx.Options) pixelfx.Instance {
	C, T, R := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("tips"), 6), pixelfx.NewRng(o.Seed())
	const OX, OY, W, H = rocketOX, rocketOY, rocketW, rocketH
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[6], C[5], C[4], C[3]}}), &pixelfx.Lights{}
	var fronds []frond
	var pops []pop
	at := func(t float64) (float64, float64) {
		return math.Sin(t*8) * 1.6 * (t / rocketFly), -8 - rocketHigh*pixelfx.Ease.OutQuad(pixelfx.Clamp(t/rocketFly))
	}
	t, burst, bx, by, acc := 0.0, false, 0.0, 0.0, 0.0
	lights.Add(0, -14, 24, C[4], 0.7, 0.35)
	// ignition: a puff of smoke and a spray of sparks off the ground
	for range 18 {
		a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(60, 180)
		ps.Spawn(pixelfx.Spark, 0, -2, math.Cos(a)*sp, math.Sin(a)*sp*0.6, R.Range(0.15, 0.4), pixelfx.White, 1, 0, 0)
	}
	for range 5 {
		ps.Spawn(pixelfx.Vapour, R.Range(-3, 3), -2, R.Range(-20, 20), -R.Range(2, 10), R.Range(0.6, 1), rocketSmoke, R.Range(3, 5), 0, 0)
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < rocketFly {
				// the climb: a hot tail spitting gold sparks, glitter left hanging, a thread of smoke
				x, y := at(t)
				x2, y2 := at(math.Max(0, t-0.02))
				vy, vx := (y-y2)/0.02, (x-x2)/0.02
				for acc += dt * 90; acc > 1; acc-- {
					a, sp := pi/2+R.Range(-0.45, 0.45), R.Range(30, 110)
					ps.Spawn(pixelfx.Spark, x, y+4, math.Cos(a)*sp+vx*0.2, math.Sin(a)*sp+vy*0.15, R.Range(0.15, 0.4), pixelfx.White, 1, 0, 0)
					if R.Chance(0.25) {
						ps.Spawn(pixelfx.StarKind, x+R.Range(-1.5, 1.5), y+R.Range(2, 6), R.Range(-6, 6), R.Range(5, 20), R.Range(0.4, 0.9), pick(R.Chance(0.5), C[5], pixelfx.White), pick(R.Chance(0.15), 1.2, 0.5), 0, 0)
					}
					if R.Chance(0.18) {
						ps.Spawn(pixelfx.Vapour, x, y+5, R.Range(-4, 4), R.Range(4, 12), R.Range(0.6, 1.1), fireworkSmoke, R.Range(1.2, 2.2), 0, 0)
					}
				}
			} else if !burst {
				burst = true
				bx, by = at(rocketFly)
				lights.Add(bx, by, 82, C[4], 0.8, 1.6)
				lights.Add(bx, by, 40, pixelfx.White, 0.6, 0.08)
				// the palm: heavy comets thrown up and out, a few low ones to the sides
				for i := range 9 {
					fi := float64(i)
					a, sp := -pi/2+((fi-4)/4)*1.4+R.Range(-0.1, 0.1), R.Range(180, 210)*(1-0.12*math.Abs(fi-4)/4)
					var f frond
					f.x, f.y, f.vx, f.vy = bx, by, math.Cos(a)*sp, math.Sin(a)*sp
					f.life = R.Range(1.8, 2.15)
					f.acc = R.Next()
					f.seed = R.Range(0, 100)
					fronds = append(fronds, f)
				}
				for range 6 {
					ps.Spawn(pixelfx.Vapour, bx+R.Range(-4, 4), by+R.Range(-4, 4), R.Range(-20, 20), R.Range(-15, 15), R.Range(1.8, 2.6), fireworkSmoke, R.Range(6, 10), 0, 0)
				}
				for range 30 {
					a, sp := R.Range(0, tau), R.Range(40, 160)
					ps.Spawn(pixelfx.Spark, bx, by, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
				}
			}
			for i := len(fronds) - 1; i >= 0; i-- {
				f := &fronds[i]
				f.age += dt
				k := f.age / f.life
				if k >= 1 {
					for range 4 {
						var q pop
						q.x = f.x + R.Range(-5, 5)
						q.y = f.y + R.Range(-5, 5)
						q.t0 = t + R.Range(0, 0.2)
						q.life = R.Range(0.06, 0.12)
						q.c = pick(R.Chance(0.5), pixelfx.White, T[3])
						pops = append(pops, q)
					}
					fronds = slices.Delete(fronds, i, i+1)
					continue
				}
				f.vx -= f.vx * rocketDrag * dt
				f.vy -= f.vy*rocketDrag*dt - rocketG*dt
				f.x += f.vx * dt
				f.y += f.vy * dt
				f.pts = append(f.pts, f.x, f.y, t)
				for len(f.pts) > 3 && t-f.pts[2] > rocketTrail {
					f.pts = f.pts[3:]
				}
				// glitter shed all along: specks that hang, twinkle and drift down
				for f.acc += dt * 34; f.acc > 1; f.acc-- {
					ps.Spawn(pixelfx.StarKind, f.x+R.Range(-1, 1), f.y+R.Range(-1, 1), f.vx*0.1+R.Range(-5, 5), R.Range(0, 14), R.Range(0.5, 1.1), pick(R.Chance(0.6), C[5], pixelfx.White), rangeOr(R, 0.12, 1, 1.5, 0.5), 0, 0)
				}
			}
			pops = slices.DeleteFunc(pops, func(q pop) bool { return t > q.t0+q.life })
			ps.Update(dt)
			cull(ps, -OX+2, -OY+2, W-OX-2, H-OY-2)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < rocketFly {
				// the rocket: a little tube on a stick, a white-hot tail flickering under it
				x, y := at(t)
				p.Line(x, y+4, x, y+12, rocketStick, 1)
				p.Rect(x-1, y-3, 3, 7, rocketTube, 1)
				p.Px(x-1, y-3, rocketLit)
				p.Px(x, y-4, rocketCap)
				fl := 2 + pick(math.Sin(t*70) > 0, 1.0, 0)
				p.LineFn(x, y+4, x, y+4+fl, func(u float64) (uint32, float64) { return pick(u < 0.4, pixelfx.White, C[5]), 1 })
				p.Glow(x, y+5, 9, C[4], 0.6)
			}
			for i := range fronds {
				f := &fronds[i]
				k, P2 := f.age/f.life, f.pts
				n := len(P2) / 3
				// the frond: a thick gold trail, hot near the head, cooling and dithering out behind
				for j := n - 1; j > 0; j-- {
					u := (t - P2[j*3+2]) / rocketTrail
					if u >= 1 {
						break
					}
					var c uint32
					switch {
					case u < 0.08:
						c = C[6]
					case u < 0.25:
						c = C[5]
					case u < 0.5:
						c = C[4]
					case u < 0.75:
						c = C[3]
					default:
						c = C[2]
					}
					a := math.Pow(1-u, 0.6) * (1.25 - k*0.45)
					p.Line(P2[j*3], P2[j*3+1], P2[j*3-3], P2[j*3-2], c, a)
					if u < 0.7 {
						c2 := C[2]
						if u < 0.25 {
							c2 = C[5]
						} else if u < 0.5 {
							c2 = C[3]
						}
						p.Line(P2[j*3]+1, P2[j*3+1], P2[j*3-3]+1, P2[j*3-2], c2, a*0.85)
					}
					if u < 0.35 {
						p.Line(P2[j*3], P2[j*3+1]+1, P2[j*3-3], P2[j*3-2]+1, C[3], a*0.6)
					}
				}
				if k > 0.8 && math.Sin(t*55+f.seed) < 0 {
					continue
				}
				p.Rect(f.x-0.5, f.y-0.5, 2, 2, pick(k < 0.5, pixelfx.White, C[6]), 1)
				if k < 0.6 {
					for _, d := range [][2]float64{{-1, 0}, {2, 0}, {0, -1}, {0, 2}} {
						p.Px(f.x+d[0], f.y+d[1], C[5])
					}
				}
				p.Glow(f.x, f.y, 10, C[4], 0.6*(1-k*0.5))
			}
			for _, q := range pops {
				u := (t - q.t0) / q.life
				if u < 0 || u > 1 {
					continue
				}
				pixelfx.Sparkle(p, q.x, q.y, 2.8*math.Sin(pi*u), q.c, T[2], 0.5)
			}
			// the burst: a white heart for two frames, a flare turning
			if b := t - rocketFly; b >= 0 && b < 0.26 {
				u := b / 0.26
				L := 30 * math.Pow(1-u, 1.3)
				for i := range 8 {
					diag := i%2 == 1
					an := u*0.5 + (float64(i)*tau)/8
					ln := pick(diag, L*0.45, L)
					ca, sa := math.Cos(an), math.Sin(an)
					for d := 0.0; d <= ln; d += 0.5 {
						v := d / ln
						c := C[4]
						if v < 0.3 {
							c = pixelfx.White
						} else if v < 0.65 {
							c = C[5]
						}
						a := 1.0
						if v >= 0.7 {
							a = 1 - (v-0.7)/0.35
						}
						p.Dot(bx+ca*d, by+sa*d, c, a)
					}
				}
				if b < 0.07 {
					p.Circle(bx, by, 2.5, pixelfx.White, 1)
				}
				p.Glow(bx, by, 4+L*1.2, C[4], 0.35)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
