// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/grasping-hands.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// GraspingHands bursts shadow arms up out of a dark pool with a splash, clenches their hands into claws, strains, and drags them back under. It is registered as "shadow/grasping-hands".
var GraspingHands = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Grasping hands",
		Desc:     "Shadow arms burst up out of a dark pool with a splash, clench their hands into claws, strain, and are dragged back under, fingers clawing at the air.",
		Tags:     []string{"shadow", "hands", "arms", "grab", "ground", "dark", "magic", "one-shot"},
		Size:     [2]int{200, 116},
		Origin:   [2]int{100, 100},
		Duration: 3.1,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newGraspingHands,
}

func init() { pixelfx.Register("shadow/grasping-hands", GraspingHands) }

const (
	ghRX   = 42.0
	ghOpen = 0.32
	ghShut = 2.35
	ghLX   = -0.6
	ghLY   = -0.8
)

type ghSnap struct {
	x, y, t0 float64
}

type ghArm struct {
	x, y                    float64
	back                    bool
	L, W                    float64
	lean, bend, curve, curl float64
	t0, td, ph              float64
	up, shut, down          bool
	snap                    *ghSnap
}

type ghPt struct {
	x, y, th, w, u float64
}

type ghSplat struct {
	x, t0, s float64
}

type ghLight struct {
	x, y, r, k, life, max float64
}

// ghGrip returns how closed the hand is: opens as it rises, snaps shut into a claw, splays as it's dragged down.
func ghGrip(A *ghArm, t float64) float64 {
	shut := pixelfx.OutBack(pixelfx.Clamp((t-A.t0-0.3)/0.12), 2.2)
	splay := pixelfx.Smooth(A.td, A.td+0.12, t)
	return shut*(1-splay) - 0.35*splay
}

// ghArmSpine returns an arm's spine at t: it rises out of the pool along its own shape, bent at the elbow, swaying.
func ghArmSpine(A *ghArm, t float64) []ghPt {
	e := pixelfx.Clamp((t - A.t0) / 0.22)
	d := pixelfx.Clamp((t - A.td) / 0.34)
	if e <= 0 || d >= 1 {
		return nil
	}
	rise := pixelfx.OutBack(e, 1.6) * (1 - pixelfx.Ease.InQuad(d))
	hidden := (1 - rise) * A.L
	grip := ghGrip(A, t)
	strain := 0.0
	if t > A.t0+0.5 && t < A.td {
		strain = math.Sin(t*47+A.ph) * 0.03
	}
	elbow := A.bend*(0.3+0.7*grip)*(1-d) + strain
	lean := A.lean + 0.06*math.Sin(t*2.2+A.ph) + d*A.lean*0.6
	th := -pi/2 + lean
	x, y := A.x-math.Cos(th)*hidden, A.y-math.Sin(th)*hidden
	n := int(math.Ceil(A.L / 0.5))
	pts := make([]ghPt, 0, n+1)
	denom := 0.07 * math.Sqrt(pi) * A.L
	for i := 0; i <= n; i++ {
		s := float64(i) * 0.5
		u := s / A.L
		pts = append(pts, ghPt{x: x, y: y, th: th, w: A.W*(1-0.5*u) + 0.4, u: u})
		du := (u - 0.52) / 0.07
		th += 0.5 * (elbow*math.Exp(-(du*du))/denom + A.curve/A.L)
		x += math.Cos(th) * 0.5
		y += math.Sin(th) * 0.5
	}
	return pts
}

// ghDrawArm draws the arm in cross-sections, lit like the tendrils: a near-black body, a bright edge toward the light.
func ghDrawArm(p *pixelfx.Painter, A *ghArm, pts []ghPt, C []uint32) {
	surf := A.y
	for _, q := range pts {
		if q.y > surf {
			continue
		}
		nx, ny := -math.Sin(q.th), math.Cos(q.th)
		ks := -1.0
		if nx*ghLX+ny*ghLY >= 0 {
			ks = 1
		}
		for v := -q.w; v <= q.w; v += 0.5 {
			x, y := q.x+nx*v, q.y+ny*v
			u := (v / q.w) * ks
			if y <= surf {
				c := C[0]
				if u > 0.55 {
					c = C[2]
				} else if u > -0.45 {
					c = C[1]
				}
				p.Px(x, y, c)
			}
		}
	}
	for _, q := range pts {
		if q.y > surf {
			continue
		}
		nx, ny := -math.Sin(q.th), math.Cos(q.th)
		f := nx*ghLX + ny*ghLY
		ks := -1.0
		if f >= 0 {
			ks = 1
		}
		l := ks * q.w
		if q.y-ny*l <= surf {
			p.Px(q.x-nx*l, q.y-ny*l, C[3])
		}
		if q.y+ny*l <= surf {
			p.Px(q.x+nx*l, q.y+ny*l, pick(math.Abs(f) > 0.8, C[7], C[6]))
		}
	}
	for _, q := range pts {
		if q.y <= surf {
			p.Line(q.x-q.w-1.5, surf, q.x+q.w+1.5, surf, C[5], 1)
			break
		}
	}
}

// ghDrawHand draws a hand on the arm's end: a palm, four fingers and a thumb, each two bones with a lit edge and a pale claw.
func ghDrawHand(p *pixelfx.Painter, A *ghArm, pts []ghPt, C []uint32, t float64) {
	tip := pts[len(pts)-1]
	if tip.y > A.y-2 {
		return
	}
	th := tip.th
	dx, dy := math.Cos(th), math.Sin(th)
	nx, ny := -dy, dx
	g, side := ghGrip(A, t), A.curl
	cx, cy := tip.x+dx*2.6, tip.y+dy*2.6
	p.Shade(cx-5, cy-5, cx+5, cy+5, func(x, y float64, _ *pixelfx.Out) uint32 {
		a := (x-cx)*dx + (y-cy)*dy
		b := (x-cx)*nx + (y-cy)*ny
		d := (a/3)*(a/3) + (b/2.7)*(b/2.7)
		if d > 1.25 {
			return 0
		}
		if d > 1 {
			return pick(b*side < 0, C[6], C[3])
		}
		return pick(b*side < -1, C[2], C[1])
	})
	bone := func(x0, y0, ang, ln float64, last bool) (float64, float64) {
		x1, y1 := x0+math.Cos(ang)*ln, y0+math.Sin(ang)*ln
		ox, oy := pixelfx.Round(-math.Sin(ang)*side*-1), pixelfx.Round(math.Cos(ang)*side*-1)
		p.Line(x0, y0, x1, y1, C[1], 1)
		p.Line(x0+ox, y0+oy, x1+ox, y1+oy, C[5], 1)
		if last {
			p.Px(x1, y1, pixelfx.White)
			p.Px(x1+math.Cos(ang), y1+math.Sin(ang), C[7])
		}
		return x1, y1
	}
	spread, curl := 1-0.65*math.Max(0, g), g*1.2
	for i, off := range [4]float64{-2.2, -0.75, 0.75, 2.2} {
		bx, by := cx+dx*2.6+nx*off*0.9, cy+dy*2.6+ny*off*0.9
		ln := 3.2
		if i == 0 || i == 3 {
			ln = 2.6
		}
		a0 := th + off*0.2*spread
		jx, jy := bone(bx, by, a0+curl*side*0.6, ln, false)
		bone(jx, jy, a0+curl*side*1.5, ln*0.85, true)
	}
	jx, jy := bone(cx-nx*side*2.4, cy-ny*side*2.4, th-side*(1-0.6*g), 2.4, false)
	bone(jx, jy, th-side*(0.6-1.4*g), 2, true)
	p.Glow(cx, cy, 8, C[5], 0.18+0.3*math.Max(0, g))
}

func newGraspingHands(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	DIM := []uint32{C[0], C[0], C[1], C[1], C[3], C[4], C[5], C[6]}
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}, Haze: C[4]})
	pf := newPuffs(puffsOpts{
		rng:   R,
		flame: []uint32{C[7], C[6], C[5], C[4], C[3], C[2], C[1], C[1], C[0]},
		soot:  []uint32{pixelfx.Mix(C[3], C[2], 0.55), pixelfx.Mix(C[2], C[1], 0.5)},
		cap:   pixelfx.Mix(C[4], C[2], 0.5),
		rim:   []uint32{C[6], C[5], C[4]},
		floor: floor,
	})
	var ripples []poolRipple
	var splats []ghSplat
	var lights []ghLight
	t, acc := 0.0, 0.0
	rx := func(tt float64) float64 {
		return ghRX * pixelfx.Ease.OutCubic(pixelfx.Clamp(tt/ghOpen)) * (1 - pixelfx.Ease.InCubic(pixelfx.Clamp((tt-ghShut)/0.45)))
	}
	dr := newDrops(dropsOpts{
		onLand: func(x, _, _, s float64) {
			if math.Abs(x) < rx(t)*0.95 {
				ripples = append(ripples, poolRipple{x: x, t0: t, life: 0.5, w: 0.6})
			} else {
				splats = append(splats, ghSplat{x: x, t0: t, s: s})
			}
		},
	})
	// four arms: two at the back, taller and dimmer, two in front; elbows bending toward the middle
	lay := [4][3]float64{{-20, -2.5, 1}, {17, -2.5, -1}, {-30, 2.5, 1}, {28, 2.5, -1}}
	t0Order := [4]float64{1, 3, 0, 2}
	tdOrder := [4]float64{2, 0, 3, 1}
	arms := make([]ghArm, 4)
	for i, l := range lay {
		ax, ay, toward := l[0], l[1], l[2]
		back := ay < 0
		x := ax + R.Range(-3, 3)
		var L, W float64
		if back {
			L = R.Range(44, 52)
			W = R.Range(3.2, 3.8)
		} else {
			L = R.Range(36, 44)
			W = R.Range(3.6, 4.2)
		}
		lean := -toward * R.Range(0.16, 0.3)
		bend := toward * R.Range(0.35, 0.6)
		curve := toward * R.Range(0.05, 0.15)
		t0 := 0.3 + t0Order[i]*0.06 + R.Range(0, 0.03)
		td := 1.45 + tdOrder[i]*0.09 + R.Range(0, 0.05)
		ph := R.Range(0, tau)
		arms[i] = ghArm{x: x, y: ay, back: back, L: L, W: W, lean: lean, bend: bend, curve: curve, curl: toward, t0: t0, td: td, ph: ph}
	}
	moteCols := []uint32{C[7], C[6], C[5]}

	splash := func(x, y float64, n int, k float64) {
		for range n {
			dr.add(x+R.Range(-2, 2), y-1, R.Range(-70, 70)*k, -R.Range(90, 220)*k, pick(R.Chance(0.4), 2.0, 1.0))
		}
		ripples = append(ripples, poolRipple{x: x, t0: t, life: 0.7, w: 1})
		lights = append(lights, ghLight{x: x, y: y - 7, r: 30, k: 0.55 * k, life: 0.25, max: 0.25})
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range arms {
				A := &arms[i]
				if !A.up && t >= A.t0 {
					A.up = true
					splash(A.x, A.y, 12, 1)
				}
				if !A.shut && t >= A.t0+0.36 {
					// the clench: a snap of light at the fist, a puff of dark breath, a few sparks
					A.shut = true
					pts := ghArmSpine(A, t)
					if pts != nil {
						q := pts[len(pts)-1]
						lights = append(lights, ghLight{x: q.x, y: q.y, r: 22, k: 0.7, life: 0.2, max: 0.2})
						A.snap = &ghSnap{x: q.x + math.Cos(q.th)*4, y: q.y + math.Sin(q.th)*4, t0: t}
						for range 6 {
							a, sp := R.Range(0, tau), R.Range(60, 130)
							ps.Spawn(pixelfx.Spark, A.snap.x, A.snap.y, math.Cos(a)*sp, math.Sin(a)*sp-20, R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
						}
						pf.spawn(pkSoot, A.snap.x, A.snap.y, 0, -12, 0.6, 3.5, 0, false)
					}
				}
				if !A.down && t >= A.td+0.28 {
					A.down = true
					splash(A.x, A.y, 8, 0.7)
				}
			}
			r := rx(t)
			if r > 8 && t < ghShut-0.4 {
				for acc += dt * 8; acc > 1; acc-- {
					pf.spawn(pkSoot, R.Range(-0.8, 0.8)*r, -R.Range(1, 4), R.Range(-8, 8), -R.Range(14, 28), R.Range(0.6, 0.85), R.Range(3, 6), 0, false)
				}
				if R.Chance(dt * 12) {
					ps.Spawn(pixelfx.Mote, R.Range(-0.8, 0.8)*r, -2, R.Range(-8, 8), -R.Range(30, 70), R.Range(0.5, 0.9), pixelfx.Pick(R, moteCols), R.Range(0.6, 1), 0, 0)
				}
				if R.Chance(dt * 6) {
					ps.Spawn(pixelfx.Vapour, R.Range(-0.7, 0.7)*r, -R.Range(2, 8), R.Range(-6, 6), -R.Range(10, 22), R.Range(0.6, 0.9), pick(R.Chance(0.5), C[3], C[4]), R.Range(3, 6), 0, 0)
				}
				if R.Chance(dt * 3) {
					ripples = append(ripples, poolRipple{x: R.Range(-0.7, 0.7) * r, t0: t, life: 0.6, w: 0.5})
				}
			}
			for i := len(ripples) - 1; i >= 0; i-- {
				if t-ripples[i].t0 > ripples[i].life {
					ripples = slices.Delete(ripples, i, i+1)
				}
			}
			for i := len(splats) - 1; i >= 0; i-- {
				if t-splats[i].t0 > 0.6 {
					splats = slices.Delete(splats, i, i+1)
				}
			}
			for i := len(lights) - 1; i >= 0; i-- {
				lights[i].life -= dt
				if lights[i].life <= 0 {
					lights = slices.Delete(lights, i, i+1)
				}
			}
			dr.update(dt)
			pf.update(dt)
			ps.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			r := rx(t)
			k := r / ghRX
			floorPool(p, C, r, t, ripples)
			if k > 0 {
				p.Glow(0, -16, 80, C[4], 0.2*k)
				p.Glow(0, -8, r*1.7+12, C[4], 0.3*k)
				p.Glow(0, 0, r*1.15, C[5], 0.16*k)
			}
			pf.draw(p, pkSoot, &puffLight{x: 0, y: 6, k: k, reach: 70})
			ps.Draw(p, pixelfx.LayerBack)
			for i := range arms {
				A := &arms[i]
				if A.back {
					if pts := ghArmSpine(A, t); pts != nil {
						ghDrawArm(p, A, pts, DIM)
						ghDrawHand(p, A, pts, DIM, t)
					}
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			for i := range arms {
				A := &arms[i]
				if !A.back {
					if pts := ghArmSpine(A, t); pts != nil {
						ghDrawArm(p, A, pts, C)
						ghDrawHand(p, A, pts, C, t)
					}
				}
			}
			for i := range arms {
				A := &arms[i]
				if A.snap != nil && t-A.snap.t0 < 0.22 {
					u := (t - A.snap.t0) / 0.22
					c := C[3]
					if u < 0.4 {
						c = C[6]
					} else if u < 0.7 {
						c = C[5]
					}
					ringFast(p, A.snap.x, A.snap.y, 2+5*pixelfx.Ease.OutCubic(u), 1, c, 1)
				}
			}
			dr.draw(p, C[1], C[5])
			for _, s := range splats {
				a := 1 - (t-s.t0)/0.6
				p.Dot(s.x, -0.5, C[1], a*1.5)
				p.Dot(s.x+1, -0.5, C[2], a)
				p.Dot(s.x, -1.5, C[5], a)
			}
			ps.Draw(p, pixelfx.LayerFront)
			for _, l := range lights {
				q := l.life / l.max
				p.Glow(l.x, l.y, l.r*(0.6+0.4*q), C[5], l.k*q*q)
			}
		},
	}
}
