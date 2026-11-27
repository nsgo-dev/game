// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/heal.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	healHurt = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.8), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3), pixelfx.HY.Is(1), pixelfx.FX.Is(1), pixelfx.FY.Is(-21), pixelfx.BX.Is(7), pixelfx.BY.Is(-15), pixelfx.FFX.Is(4), pixelfx.FBX.Is(-3))
	healUp   = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-13), pixelfx.FY.Is(-36), pixelfx.BX.Is(14), pixelfx.BY.Is(-36), pixelfx.HY.Is(-1), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
)

// Heal is petals of light blooming under the feet, a hoop of light rising up the body restoring its colour and leaving sparkles, and motes spiralling up to pop into tiny hearts. It is registered as "holy/heal".
var Heal = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Heal",
		Desc:     "Petals of light bloom under the feet, a hoop of light rises up the body restoring its colour and leaving sparkles, and motes spiral up and pop into tiny hearts.",
		Tags:     []string{"holy", "heal", "restore", "buff", "petals", "hearts", "character", "one-shot"},
		Size:     [2]int{80, 104},
		Origin:   [2]int{40, 92},
		Duration: 1.9,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#3d2027", "#7d3f35", "#c97a4a", "#f2b46a", "#ffd994", "#fff0cf", "#fffaf2", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.3, Pose: healHurt},
			{T: 0.16, Pose: healHurt.With(pixelfx.Crouch.Is(6))},
			{T: 0.5, Pose: healHurt.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1), pixelfx.HY.Is(0))},
			{T: 0.78, Pose: healUp},
			{T: 1.2, Pose: healUp.With(pixelfx.Crouch.Is(0), pixelfx.FY.Is(-30), pixelfx.BY.Is(-30), pixelfx.HY.Is(0))},
			{T: 1.9, Pose: pixelfx.Ready},
		},
	},
	New: newHeal,
}

func init() { pixelfx.Register("holy/heal", Heal) }

const healNP = 8

var healSY, healPR, healLift0, healLift1, healTopY = 0.3, 16.0, 0.16, 0.74, -43.0

var healHeart = [4]string{".#.#.", "#####", ".###.", "..#.."}

// healDrawHeart draws a tiny heart of light: cream body, a white glint, a warm rim.
func healDrawHeart(p *pixelfx.Painter, x, y float64, C []uint32, a float64) {
	X, Y := pixelfx.Round(x)-2, pixelfx.Round(y)-2
	at := func(i, j int) bool { return j >= 0 && j < 4 && i >= 0 && i < 5 && healHeart[j][i] == '#' }
	for j := -1; j <= 4; j++ {
		for i := -1; i <= 5; i++ {
			fx, fy := X+float64(i), Y+float64(j)
			if at(i, j) {
				c := C[6]
				if i == 1 && j == 1 {
					c = pixelfx.White
				} else if j >= 2 {
					c = C[5]
				}
				p.Dot(fx, fy, c, a)
			} else if at(i-1, j) || at(i+1, j) || at(i, j-1) || at(i, j+1) {
				p.Dot(fx, fy, C[3], a*0.9)
			}
		}
	}
	p.Glow(x, y, 6, C[5], 0.4*a)
}

type healPetal struct{ a, t0, w float64 }

// healMote is a mote riding a spiral up round the body from its start t0, angle th0, radius r0 and height y0, turning w radians a second and rising v px a second for life seconds, then popping into a heart (kind 1) or a sparkle (kind 0).
type healMote struct {
	t0, th0, r0, y0, w, v, life float64
	kind                        int
}

func newHeal(o *pixelfx.Options) pixelfx.Instance {
	C, A, R, W := pixelfx.Fit(o.Palette(), 8), o.Actor, pixelfx.NewRng(o.Seed()), pixelfx.White
	S := shapeOf(A)
	SY, PR, LIFT0, LIFT1, TOPY := healSY, healPR, healLift0, healLift1, healTopY
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{W, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	rows := edgeRows(S)
	// petals, each blooming on its own beat round the ring
	var petals [healNP]healPetal
	for k := range healNP {
		petals[k] = healPetal{a: (float64(k)/healNP)*tau + 0.2, t0: 0.02 + float64((k*3)%healNP)*0.035, w: R.Range(4.2, 5.2)}
	}
	// motes riding a spiral up round the body: from the petal tips as they open, and from the hoop when it bursts
	var motes []healMote
	for k := range healNP {
		w := -4.2
		if k%2 != 0 {
			w = 4.2
		}
		v, life := R.Range(46, 58), R.Range(0.75, 0.95)
		motes = append(motes, healMote{t0: 0.2 + float64(k)*0.05, th0: petals[k].a, r0: PR * 0.9, y0: -1, w: w, v: v, life: life, kind: k % 2})
	}
	for k := range 8 {
		w := -3.6
		if k%2 != 0 {
			w = 3.6
		}
		t0, r0, y0 := LIFT1+R.Range(0, 0.06), R.Range(12, 18), TOPY+R.Range(-2, 4)
		v, life := R.Range(14, 34), R.Range(0.35, 0.6)
		motes = append(motes, healMote{t0: t0, th0: (float64(k) / 8) * tau, r0: r0, y0: y0, w: w, v: v, life: life, kind: (k + 1) % 2})
	}
	t, burst, lastRow := 0.0, false, 1.0

	hoopY := func(tt float64) float64 {
		return pixelfx.Lerp(0, TOPY, pixelfx.Ease.InOutSine(pixelfx.Seg(tt, LIFT0, LIFT1)))
	}
	hoopR := func(tt float64) float64 { return pixelfx.Lerp(PR, 12, pixelfx.Seg(tt, LIFT0, LIFT1)) }

	bloom := func(k int, tt float64) float64 {
		pe := petals[k]
		fk := float64(k)
		return pixelfx.OutBack(pixelfx.Seg(tt, pe.t0, pe.t0+0.3), 1.8) * (1 - pixelfx.InBack(pixelfx.Seg(tt, 1.25+fk*0.02, 1.6+fk*0.02), 1.4))
	}

	// a petal of a lotus of light round the feet: rooted on a ring on the floor, leaning out as it opens, foreshortened as it turns away; a lit rim, a white vein, a translucent warm body, a white tip
	petal := func(p *pixelfx.Painter, k int, back bool) {
		pe := petals[k]
		g := pe.a + t*0.4
		isNear, b := math.Sin(g) >= 0, bloom(k, t)
		if isNear == back || b <= 0.02 {
			return
		}
		ca, sa := math.Cos(g), math.Sin(g)
		rb := 6.0
		rt := rb + 9*math.Min(1.2, b)
		h := 9 * b
		if k%2 != 0 {
			h = 7 * b
		}
		bx, by, tx, ty := ca*rb, sa*rb*SY, ca*rt, sa*rt*SY-h
		L := pixelfx.Hypot(tx-bx, ty-by)
		if L < 1.5 {
			return
		}
		ux, uy := (tx-bx)/L, (ty-by)/L
		Wd := pe.w * math.Min(1, b) * (0.35 + 0.65*math.Abs(sa))
		pad := Wd + 1
		p.Each(math.Min(bx, tx)-pad, math.Min(by, ty)-pad, math.Max(bx, tx)+pad, math.Max(by, ty)+pad, func(x, y float64, sx, sy int) {
			dx, dy := x-bx, y-by
			u := (dx*ux + dy*uy) / L
			if u < 0 || u > 1 {
				return
			}
			hw := Wd*math.Pow(math.Sin(pi*math.Pow(u, 0.6)), 0.7) + 0.2
			v := math.Abs(-dx*uy+dy*ux) / hw
			if v > 1 {
				return
			}
			switch {
			case u > 0.9:
				p.Put(sx, sy, W, 1)
			case v > 0.7:
				c := C[6]
				if u < 0.3 {
					c = C[3]
				} else if u < 0.7 {
					c = C[5]
				}
				p.Put(sx, sy, c, 1)
			case v < 0.2 && u > 0.15 && u < 0.85:
				c, a := C[6], 0.85
				if u < 0.55 {
					c = C[5]
				}
				if back {
					a = 0.75
				}
				p.Put(sx, sy, c, a)
			default:
				c, a := C[4], 0.45
				if u < 0.45 {
					c = C[3]
				}
				if back {
					a = 0.6
				}
				p.BlendPx(sx, sy, c, a)
			}
		})
		p.Glow(tx, ty, 5, C[5], 0.35*math.Min(1, b))
	}

	// the hoop of light rising up the body: far half behind, near half in front, a soft glow round it
	hoop := func(p *pixelfx.Painter, back bool) {
		if t < LIFT0 || t > LIFT1+0.12 {
			return
		}
		y, r := hoopY(t), hoopR(t)
		if t > LIFT1 {
			r *= 1 - (t-LIFT1)/0.12
		}
		keep := halfOf(back)
		a := 1.0
		if back {
			a = 0.8
		}
		ovalFn(p, 0, y, r, r*SY, func(g float64) (uint32, float64) {
			if math.Sin(g) > 0.5 {
				return W, a
			}
			return C[6], a
		}, keep)
		if !back {
			oval(p, 0, y+1, r-0.5, r*SY, C[4], 1, keep)
			for i := -2; i <= 2; i++ {
				p.Glow(float64(i)*r*0.45, y, 8, C[5], 0.32)
			}
		}
	}

	// the spiralling motes, each popping into a heart or a sparkle at the end of its ride
	riders := func(p *pixelfx.Painter, back bool) {
		for i := range motes {
			m := &motes[i]
			a := t - m.t0
			if a < 0 {
				continue
			}
			at := func(aa float64) (float64, float64, float64) {
				th, r := m.th0+m.w*aa, m.r0*(1-0.45*(aa/m.life))
				return math.Cos(th) * r, m.y0 - m.v*aa, math.Sin(th)
			}
			if a < m.life {
				x, y, z := at(a)
				if (z < 0) != back {
					continue
				}
				for j := range 7 {
					aj := a - float64(j)*0.03
					if aj < 0 {
						break
					}
					tx, ty, tz := at(aj)
					if (tz < 0) != back {
						continue
					}
					c := C[5]
					if j == 0 {
						c = W
					} else if j < 3 {
						c = C[6]
					}
					k := 1.0
					if back {
						k = 0.6
					}
					p.Dot(tx, ty, c, (1-float64(j)/7)*k)
				}
				g := 0.4
				if back {
					g = 0.2
				}
				p.Glow(x, y, 4, C[5], g)
				continue
			}
			// the pop: a quick sparkle, then a heart (or a star) that floats up and fades
			u := (a - m.life) / 0.36
			if u >= 1 || back {
				continue
			}
			x, y, _ := at(m.life)
			k := 1 - pixelfx.Smooth(0.5, 1, u)
			switch {
			case u < 0.12:
				pixelfx.Sparkle(p, x, y, 3, W, C[6], 0.5)
			case m.kind != 0:
				healDrawHeart(p, x, y-u*6, C, k)
			default:
				pixelfx.Sparkle(p, x, y-u*5, 1+2*k*(0.7+0.3*math.Sin(a*30)), W, C[5], 0.45*k)
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the hoop leaves sparkles on the body as it passes each row
			if t > LIFT0 && t < LIFT1 {
				Y := pixelfx.Round(hoopY(t))
				for y := lastRow; y > Y; y-- {
					xs, ok := rows[y]
					if !ok || !R.Chance(0.45) {
						continue
					}
					px, _ := pick(R, xs)
					x := px + R.Range(-1, 1)*2
					vx, vy, life := R.Range(-4, 4), -R.Range(4, 12), R.Range(0.3, 0.55)
					col, size := chance(R, 0.5, W, C[6]), R.Range(0.6, 1.6)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
				}
				lastRow = math.Min(lastRow, Y)
			}
			if !burst && t >= LIFT1 {
				burst = true
				lights.Add(0, TOPY, 26, C[5], 0.55, 0.3)
				lights.Add(0, TOPY, 10, W, 0.45, 0.08)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a warm pool under the flower
			pool := pixelfx.Smooth(0, 0.2, t) * (1 - pixelfx.Smooth(1.3, 1.75, t))
			if pool > 0 {
				p.Glow(0, -2, 30, C[4], 0.3*pool)
			}
			for k := range healNP {
				petal(p, k, true)
			}
			hoop(p, true)
			ps.Draw(p, pixelfx.LayerBack)
			riders(p, true)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// drained of colour above the hoop, restored below it with a warm glow at the line
			drain := pixelfx.Smooth(0, 0.08, t)
			if t < LIFT1+0.35 {
				Y := 1.0
				if t >= LIFT0 {
					Y = hoopY(t)
				}
				done := t >= LIFT1
				recolor(p, A, func(c uint32, _, y float64) uint32 {
					d := y - Y
					if !done && d < -0.5 {
						l := pixelfx.Luma(c) * 0.88
						return pixelfx.Mix(c, pixelfx.RGB(l*236, l*226, l*222), 0.68*drain)
					}
					if !done && d < 1 {
						return W
					}
					if !done && d < 2.5 {
						return C[6]
					}
					var k float64
					if done {
						k = 0.45 * (1 - pixelfx.Seg(t, LIFT1, LIFT1+0.35))
					} else {
						k = 0.5 * math.Exp(-(d-2.5)/5)
					}
					if k > 0.05 {
						return pixelfx.Mix(c, C[5], k)
					}
					return 0
				}, 1)
				if t > LIFT0 && !done {
					p.Glow(0, Y, 18, C[5], 0.35)
				}
			}
			for k := range healNP {
				petal(p, k, false)
			}
			hoop(p, false)
			if t >= LIFT1 && t < LIFT1+0.25 {
				pixelfx.Sparkle(p, 0, TOPY, 8*(1-(t-LIFT1)/0.25), W, C[5], 0.6)
			}
			riders(p, false)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
