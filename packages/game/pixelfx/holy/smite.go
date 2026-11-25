// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/smite.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Smite is a glint marking the spot, then a bolt of light dropping like a hammer: a white flash, a shockwave over the floor, stone chips flying and cracks that glow and cool. It is registered as "holy/smite".
var Smite = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Smite",
		Desc:     "A glint marks the spot, then a bolt of light drops like a hammer: a white flash, a shockwave over the floor, stone chips flying and cracks that glow and cool.",
		Tags:     []string{"holy", "smite", "strike", "impact", "light", "ground", "one-shot"},
		Size:     [2]int{224, 176},
		Origin:   [2]int{112, 148},
		Duration: 2.8,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(Gold...)},
			{Name: "stone", Default: pixelfx.Color("#5a5066")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newSmite,
}

func init() { pixelfx.Register("holy/smite", Smite) }

var (
	smiteTop, smiteSY, smiteMark, smiteHit, smiteLen = -148.0, 0.38, 0.42, 0.54, 44.0
	smiteOutline                                     = pixelfx.Hex("#1a1422")
)

// smiteChip draws a chip of stone: lit side, shaded side, outline.
func smiteChip(p *pixelfx.Painter, b *pixelfx.Bit, st [4]uint32) {
	r := 0.6 + b.S*0.7
	fade := math.Min(1, b.Life/0.5)
	lx, ly := math.Cos(b.A)*r*0.4, math.Sin(b.A)*r*0.4
	p.Shade(b.X-r-1, b.Y-r-1, b.X+r+1, b.Y+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		d := pixelfx.Hypot(dx*1.2, dy)
		if d > r+0.9 {
			return 0
		}
		lit := dx*lx + dy*ly
		switch {
		case d > r:
			out.C = smiteOutline
		case lit < -0.3:
			out.C = st[3]
		case lit > 0.5:
			out.C = st[1]
		default:
			out.C = st[2]
		}
		out.A = fade
		return 0
	})
}

type smiteCrack struct {
	pts, fork [][2]float64
	L, cool   float64
}

func newSmite(o *pixelfx.Options) pixelfx.Instance {
	C, R, s0, W := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Color("stone"), pixelfx.White
	TOP, SY, MARK, HIT, LEN := smiteTop, smiteSY, smiteMark, smiteHit, smiteLen
	st := [4]uint32{pixelfx.Scale(s0, 0.5), pixelfx.Scale(s0, 0.75), s0, pixelfx.Mix(pixelfx.Scale(s0, 1.4), W, 0.1)}
	dust := pixelfx.Mix(s0, pixelfx.Hex("#8a8094"), 0.4)
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{W, C[6], C[5], C[3]}})
	chips, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	// cracks across the floor, mostly out to the sides and towards us, each its own length
	cracks := make([]smiteCrack, 7)
	for i := range cracks {
		a := 0.12 + (float64(i)/6)*(pi-0.24) + R.Range(-0.12, 0.12)
		L := R.Range(30, 54)
		if math.Abs(math.Sin(a)) > 0.7 {
			L *= 0.6
		}
		pts := pixelfx.Bolt(R, 0, 0, math.Cos(a)*L, math.Sin(a)*L, 0.28, 4)
		for j := range pts {
			pts[j][1] *= SY
		}
		var fork [][2]float64
		if len(pts) > 6 {
			m := pts[int(math.Floor(float64(len(pts))*0.5))]
			x1 := m[0] + math.Cos(a+R.Sign()*0.7)*L*0.35
			y1 := m[1] + math.Sin(a+R.Sign()*0.7)*L*0.35*SY
			fork = pixelfx.Bolt(R, m[0], m[1], x1, y1, 0.3, 3)
		}
		cracks[i] = smiteCrack{pts: pts, fork: fork, L: L, cool: R.Range(1.6, 2.1)}
	}
	t, hit := 0.0, false

	// the hammer of light: a heavy blunt bolt that falls faster and faster
	tipY := func(tt float64) float64 {
		return pixelfx.Lerp(TOP-6, 0, pixelfx.Ease.InCubic(pixelfx.Seg(tt, MARK, HIT)))
	}

	hammer := func(p *pixelfx.Painter) {
		if t < MARK || t >= HIT+0.06 {
			return
		}
		white := t >= HIT
		y1, k := tipY(t), 1.0
		if white {
			y1, k = 0, 1-(t-HIT)/0.06
		}
		y0 := math.Max(TOP+5, y1-LEN*k)
		if y1 < TOP+12 {
			return
		}
		// speed lines above it
		for _, l := range [4][2]float64{{-5, 0.6}, {-2, 0.9}, {2, 0.8}, {5, 0.5}} {
			dx, lk := l[0], l[1]
			if y0 > TOP+2 {
				p.LineFn(dx, math.Max(TOP, y0-70), dx, y0, func(u float64) (uint32, float64) {
					if u > 0.7 {
						return C[6], u * lk
					}
					return C[4], u * lk
				})
			}
		}
		if y1-3 > y0 {
			p.BeamFn(0, y0, 0, y1-3, 9, func(_, v float64) (uint32, float64) {
				switch {
				case white || v < 0.35:
					return W, 1
				case v < 0.7:
					return C[6], 1
				}
				return C[5], 1
			})
		}
		// the head: wider, white hot
		p.BeamFn(0, y1-6, 0, y1-1, 13, func(_, v float64) (uint32, float64) {
			if v < 0.6 || white {
				return W, 1
			}
			return C[6], 1
		})
		p.GlowLine(0, y0+10, 0, y1, 16, C[5], 0.6)
		p.Glow(0, y1-4, math.Min(22, y1-TOP-6), W, 0.5)
	}

	crack := func(p *pixelfx.Painter) {
		if t < HIT {
			return
		}
		grow := pixelfx.Ease.OutCubic(pixelfx.Seg(t, HIT, HIT+0.1))
		for ci := range cracks {
			c := &cracks[ci]
			paths := [][][2]float64{c.pts}
			if c.fork != nil {
				paths = append(paths, c.fork)
			}
			for pathIdx, path := range paths {
				n := max(1, int(pixelfx.Round(float64(len(path)-1)*grow)))
				for i := range n {
					x0, y0, x1, y1 := path[i][0], path[i][1], path[i+1][0], path[i+1][1]
					d := pixelfx.Hypot(x0, y0/SY) / c.L
					heat := pixelfx.Clamp(1 - (t-HIT)/c.cool - d*0.5)
					if heat <= 0 {
						continue
					}
					var col uint32
					switch {
					case heat > 0.85:
						col = W
					case heat > 0.65:
						col = C[6]
					case heat > 0.45:
						col = C[5]
					case heat > 0.3:
						col = C[4]
					case heat > 0.18:
						col = C[3]
					default:
						col = C[2]
					}
					a := 1.0
					if heat < 0.12 {
						a = heat / 0.12
					}
					p.Line(x0, y0, x1, y1, col, a)
					// wide and deep near the heart, a hairline at the tips
					if d < 0.45 && pathIdx == 0 {
						deep := C[2]
						if heat > 0.5 {
							deep = C[4]
						}
						p.Line(x0, y0+1, x1, y1+1, deep, a)
					}
					if i%2 == 0 {
						p.Glow(x0, y0, 10, C[4], 0.45*heat)
					}
				}
			}
		}
		// the crater glows at the heart and cools
		if h := pixelfx.Clamp(1 - (t-HIT)/1.6); h > 0 {
			c := C[3]
			if h > 0.6 {
				c = W
			} else if h > 0.3 {
				c = C[5]
			}
			oval(p, 0, 0, 5, 1.6, c, math.Min(1, h*3), nil)
			p.Glow(0, 0, 18, C[4], 0.45*h)
		}
	}

	// shock draws the half of the shockwave band over the floor that keep picks.
	shock := func(p *pixelfx.Painter, keep func(g float64) bool) {
		if k := pixelfx.Seg(t, HIT, HIT+0.42); t >= HIT && k < 1 {
			pixelfx.Band(p, 0, 0, 6+84*pixelfx.Ease.OutCubic(k), SY*0.8, math.Max(1, 5*(1-k)), 1.15-k, C[3:], keep, k < 0.12)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < MARK && R.Chance(0.25) {
				x, y, vy, life := R.Range(-10, 10), -R.Range(0, 18), -R.Range(4, 12), R.Range(0.2, 0.4)
				col, size := chance(R, 0.5, W, C[6]), R.Range(0.5, 1.2)
				ps.Spawn(pixelfx.StarKind, x, y, 0, vy, life, col, size, 0, 0)
			}
			// sparks shed off the sides of the falling hammer
			if t > MARK && t < HIT && tipY(t) > TOP+50 && R.Chance(0.6) {
				x := R.Sign() * R.Range(4, 7)
				y, vx, vy, life := tipY(t)-R.Range(4, 30), R.Range(-20, 20), -R.Range(10, 40), R.Range(0.15, 0.3)
				ps.Spawn(pixelfx.Mote, x, y, vx, vy, life, C[6], 0.7, 0, 0)
			}
			if !hit && t >= HIT {
				hit = true
				lights.Add(0, -24, 92, C[5], 1, 0.6)
				lights.Add(0, -8, 40, W, 0.9, 0.12)
				for range 60 {
					a, sp := -pi/2+R.Range(-1.25, 1.25), R.Range(110, 280)
					x, life := R.Range(-3, 3), R.Range(0.25, 0.75)
					ps.Spawn(pixelfx.Spark, x, -2, math.Cos(a)*sp, math.Sin(a)*sp, life, W, 1, 0, 0)
				}
				for range 16 {
					a, sp := -pi/2+R.Range(-1, 1), R.Range(60, 160)
					x, life, col, size := R.Range(-4, 4), R.Range(0.4, 0.9), chance(R, 0.5, W, C[6]), R.Range(0.8, 2.4)
					ps.Spawn(pixelfx.StarKind, x, -4, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
				}
				for range 14 {
					a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(90, 230)
					x, life, ang, va, s := R.Range(-5, 5), R.Range(1.2, 1.8), R.Range(0, tau), R.Range(-14, 14), R.Int(1, 3)
					chips.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: -2, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 1, Drag: 0.4, Bounce: 0.35, Life: life, A: ang, VA: va, S: s})
				}
				for range 34 {
					a, sp := -pi/2+R.Range(-1, 1), R.Range(60, 220)
					x, life := R.Range(-4, 4), R.Range(0.9, 1.5)
					col, _ := pick(R, st[1:])
					size := chance(R, 0.4, 2.0, 1.0)
					ps.Spawn(pixelfx.Grit, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
				}
				// dust rolls out along the floor, a little smoke lifts off the crater
				for range 9 {
					s := R.Sign()
					x, y, vx, vy, life := s*R.Range(2, 10), -R.Range(1, 4), s*R.Range(50, 130), -R.Range(2, 12), R.Range(0.7, 1.2)
					size, delay := R.Range(3.5, 6), R.Range(0, 0.06)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, pixelfx.Mix(dust, C[4], 0.18), size, delay, pixelfx.Big)
				}
				for range 4 {
					x, y, vx, vy, life := R.Range(-4, 4), -R.Range(2, 8), R.Range(-8, 8), -R.Range(20, 45), R.Range(1.2, 1.8)
					size, delay := R.Range(4, 6), R.Range(0.05, 0.3)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, pixelfx.Mix(dust, C[5], 0.25), size, delay, 0)
				}
			}
			// the cracks spit embers while they're hot
			if t > HIT && t < HIT+1.3 && R.Chance(0.5) {
				c, _ := pick(R, cracks)
				q, _ := pick(R, c.pts)
				if R.Chance(0.75) {
					vx, vy, life := R.Range(-8, 8), -R.Range(15, 35), R.Range(0.5, 0.9)
					ps.Spawn(pixelfx.Ember, q[0], q[1]-1, vx, vy, life, C[5], 1, 0, 0)
				} else {
					vy, life, col, size := -R.Range(4, 12), R.Range(0.3, 0.6), chance(R, 0.5, W, C[6]), R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, q[0], q[1]-2, 0, vy, life, col, size, 0, 0)
				}
			}
			chips.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			crack(p)
			ps.Draw(p, pixelfx.LayerBack)
			shock(p, far)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the mark: a glint on the floor and a ring closing on it
			if t < HIT {
				u, k, spin := pixelfx.Seg(t, 0, MARK), pixelfx.Smooth(0, 0.08, t), t*3
				rr := pixelfx.Lerp(26, 8, pixelfx.Ease.InOutCubic(u))
				ka := k * (0.65 + 0.35*math.Sin(t*40))
				ovalFn(p, 0, 0, rr, rr*SY, func(g float64) (uint32, float64) {
					if math.Sin(g*4+spin) > 0 {
						return W, ka
					}
					return C[5], ka
				}, nil)
				oval(p, 0, 1, rr, rr*SY, C[3], k*0.6, func(g float64) bool { return math.Sin(g) > 0 })
				L := 3 + 8*pixelfx.OutBack(pixelfx.Clamp(t/0.2), 2)
				if t > MARK {
					L *= 1 + 0.6*pixelfx.Seg(t, MARK, HIT)
				}
				for i := range 4 {
					a, length := spin*0.3+(float64(i)*pi)/2, L
					if i%2 != 0 {
						length = L * 0.6
					}
					p.LineFn(0, -2, math.Cos(a)*length, -2+math.Sin(a)*length, func(v float64) (uint32, float64) {
						if v < 0.4 {
							return W, 1.2 - v
						}
						return C[5], 1.2 - v
					})
				}
				p.Px(0, -2, W)
				p.Glow(0, -2, 14+10*u, C[5], 0.5*k)
				p.GlowLine(0, TOP+6, 0, -2, 5, C[4], 0.12*u)
			}
			hammer(p)
			// the bolt's afterimage: a thread of light from the sky to the mark, thinning out
			if ai := pixelfx.Seg(t, HIT+0.04, HIT+0.55); t > HIT+0.04 && ai < 1 {
				w := 1 + 4*math.Pow(1-ai, 2)
				p.BeamFn(0, TOP+4, 0, -4, w, func(u, v float64) (uint32, float64) {
					a := (0.3 + 0.7*u) * (1 - ai*0.5)
					if v < 0.5 {
						return W, a
					}
					return C[5], a
				})
				p.GlowLine(0, TOP+16, 0, -6, 10, C[5], 0.4*(1-ai))
			}
			shock(p, near)
			// the air shock: a dome of light thrown up over the hit
			if d := pixelfx.Seg(t, HIT, HIT+0.28); t >= HIT && d < 1 {
				r := 8 + 40*pixelfx.Ease.OutCubic(d)
				p.ArcFn(0, 0, r, math.Max(1, 3*(1-d)), pi, tau, func(_, v float64) (uint32, float64) {
					if d < 0.2 || v > 0.6 {
						return W, 1.1 - d
					}
					return C[5], 1.1 - d
				})
				for g := pi * 1.1; g < tau-0.2; g += 0.5 {
					p.Glow(math.Cos(g)*r, math.Sin(g)*r, 8, C[5], 0.3*(1-d))
				}
			}
			// the white hero frame
			if t >= HIT && t < HIT+0.07 {
				p.Circle(0, -6, 16, W, 1)
				p.Circle(0, -6, 19, C[6], 0.6)
			}
			for i := range chips.List {
				smiteChip(p, &chips.List[i], st)
			}
			ps.Draw(p, pixelfx.LayerFront)
			if t >= HIT && t < HIT+0.25 {
				pixelfx.Sparkle(p, 0, -8, 16*(1-(t-HIT)/0.25), W, C[5], 0.7)
			}
			lights.Draw(p)
		},
	}
}
