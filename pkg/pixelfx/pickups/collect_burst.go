// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/pickups/collect-burst.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the pop (a white flash, a ring snapping out, star sparks with short trails) from Sprite Fusion's open-source pickup (https://www.spritefusion.com/pixel-art-vfx/pickup), MIT License, Copyright (c) 2026 Hugo Duprez.

package pickups

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CollectBurst is the moment of pickup: a white flash and a cross of light, a thin ring snapping out ahead of a softer one, twinkles thrown wide on curving paths that brake, hang and wink out. It is registered as "pickups/collect-burst".
var CollectBurst = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Collect burst",
		Desc:     "The moment of pickup: a white flash and a cross of light, a thin ring snapping out ahead of a softer one, twinkles thrown wide on curving paths that brake, hang and wink out.",
		Tags:     []string{"pickup", "collect", "burst", "sparkle", "reward", "one-shot"},
		Size:     [2]int{112, 108},
		Origin:   [2]int{56, 94},
		Duration: 1.35,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0516", "#3d0a33", "#701560", "#b0228f", "#f53cd1", "#ff86e6", "#ffd2f6", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newCollectBurst,
}

func init() { pixelfx.Register("pickups/collect-burst", CollectBurst) }

const collectBurstCY = -30.0

var collectBurstArms = [4][3]float64{{1, 0, 1}, {-1, 0, 1}, {0, 1, 0.6}, {0, -1, 0.6}}

type burstStar struct {
	x, y, a, sp, turn, life, age, size float64
	tr                                 [][2]float64
	c                                  uint32
	ph                                 float64
}

type burstMote struct{ x, y, vy, life, d, w float64 }

func newCollectBurst(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	lights := &pixelfx.Lights{}
	lights.Add(0, collectBurstCY, 36, C[5], 0.85, 0.32)
	lights.Add(0, collectBurstCY, 14, pixelfx.White, 0.75, 0.08)

	stars := make([]burstStar, 14)
	for i := range stars {
		a := (float64(i)/14)*tau + R.Range(-0.2, 0.2)
		sp := R.Range(145, 225) * pick(i%3 == 0, 1.12, 0.85)
		turn := pick(i%2 != 0, 1.0, -1.0) * R.Range(2.5, 5)
		life := R.Range(0.75, 1.15)
		var size float64
		if i%3 == 0 {
			size = 2.6
		} else {
			size = R.Range(1.2, 2)
		}
		var col uint32
		switch {
		case i%4 == 0:
			col = pixelfx.White
		case i%2 != 0:
			col = C[6]
		default:
			col = C[5]
		}
		ph := R.Range(0, tau)
		stars[i] = burstStar{x: 0, y: collectBurstCY, a: a, sp: sp, turn: turn, life: life, size: size, c: col, ph: ph}
	}

	motes := make([]burstMote, 7)
	for i := range motes {
		motes[i] = burstMote{
			x:    R.Range(-8, 8),
			y:    collectBurstCY + R.Range(-4, 6),
			vy:   -R.Range(12, 30),
			life: R.Range(0.5, 0.9),
			d:    R.Range(0.05, 0.25),
			w:    R.Range(0, tau),
		}
	}

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range stars {
				s := &stars[i]
				if s.age > s.life {
					continue
				}
				s.age += dt
				s.tr = append([][2]float64{{s.x, s.y}}, s.tr...)
				if len(s.tr) > 4 {
					s.tr = s.tr[:4]
				}
				s.a += s.turn * dt * math.Max(0, 1-s.age*1.5)
				s.sp -= s.sp * 4.3 * dt
				s.x += math.Cos(s.a) * s.sp * dt
				s.y += math.Sin(s.a)*s.sp*dt + 6*s.age*dt
				if s.y > -2 {
					s.y = -2
					s.a = -math.Abs(s.a)
					s.sp *= 0.5
				}
			}
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < 0.07 {
				p.Circle(0, collectBurstCY, 5.5*(1-t/0.07)+1, pixelfx.White, 1)
			}
			if t < 0.16 {
				k := 1 - t/0.16
				L := 26 * pixelfx.Ease.OutCubic(math.Min(1, t/0.03+0.4)) * k
				for _, arm := range collectBurstArms {
					dx, dy, l := arm[0], arm[1], arm[2]
					p.LineFn(0, collectBurstCY, dx*L*l, collectBurstCY+dy*L*l, func(u float64) (uint32, float64) {
						col := C[5]
						if u < 0.3 {
							col = pixelfx.White
						} else if u < 0.65 {
							col = C[6]
						}
						fade := (1 - u) * 2
						if u < 0.5 {
							fade = 1
						}
						return col, fade * (0.5 + k)
					})
				}
				p.Glow(0, collectBurstCY, 20*k+4, C[5], 0.6*k)
			}
			if u1 := t / 0.32; u1 < 1 {
				r := 3 + 23*pixelfx.Ease.OutExpo(u1)
				p.Ring(0, collectBurstCY, r, 1, pick(u1 < 0.3, pixelfx.White, C[6]), 1.3-u1*1.2)
				p.GlowRing(0, collectBurstCY, r, 4, C[5], 0.4*(1-u1))
			}
			if u2 := (t - 0.04) / 0.5; u2 > 0 && u2 < 1 {
				r := 2 + 16*pixelfx.Ease.OutCubic(u2)
				p.Ring(0, collectBurstCY, r, math.Max(1, 2.6*(1-u2)), C[5], 1-u2)
			}
			for i := range stars {
				s := &stars[i]
				if s.age > s.life {
					continue
				}
				k := 1 - s.age/s.life
				tw := 0.65 + 0.35*math.Sin(s.age*26+s.ph)
				for j, pt := range s.tr {
					if j > 0 {
						p.Dot(pt[0], pt[1], pick(j < 2, C[5], C[4]), (1-float64(j)/4)*k*1.4)
					}
				}
				if k < 0.3 && math.Sin(s.age*40+s.ph) < 0 {
					continue
				}
				glint(p, s.x, s.y, pixelfx.Round(s.size*tw*math.Min(1, k*2.5)), s.c, 0.45*k)
			}
			for _, m := range motes {
				a := t - m.d
				if a < 0 || a > m.life {
					continue
				}
				k := 1 - a/m.life
				x, y := m.x+math.Sin(a*6+m.w)*1.5, m.y+m.vy*a
				p.Dot(x, y, pick(k > 0.5, C[6], C[5]), k*1.5)
				p.Glow(x+0.5, y+0.5, 3, C[5], 0.3*k)
			}
			lights.Draw(p)
		},
	}
}
