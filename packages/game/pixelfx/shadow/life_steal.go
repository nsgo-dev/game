// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/life-steal.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	lsGrasp = pixelfx.Ready.With(
		pixelfx.Crouch.Is(3), pixelfx.Lean.Is(3), pixelfx.FX.Is(17), pixelfx.FY.Is(-28),
		pixelfx.BX.Is(-6), pixelfx.BY.Is(-21), pixelfx.FFX.Is(10), pixelfx.FBX.Is(-9),
	)
	lsFed = pixelfx.Pose{}.With(
		pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(0), pixelfx.Lean.Is(-3), pixelfx.HY.Is(-1),
		pixelfx.FX.Is(-13), pixelfx.FY.Is(-37), pixelfx.BX.Is(14), pixelfx.BY.Is(-37),
		pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4),
	)
)

// LifeSteal strikes a target on the right so it bleeds red orbs, hangs a moment, then whips them home as comet streaks to flood the caster crimson. It is registered as "shadow/life-steal".
var LifeSteal = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Life steal",
		Desc:     "A target on the right is struck and bleeds red orbs; they hang a moment, then whip home as comet streaks and flood the caster crimson.",
		Tags:     []string{"shadow", "life-steal", "drain", "blood", "heal", "directional", "magic", "one-shot"},
		Size:     [2]int{184, 100},
		Origin:   [2]int{36, 92},
		Duration: 2.2,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#14040a", "#3a0a14", "#6a101c", "#a01824", "#d42a30", "#ff5a50", "#ff9a8a", "#ffe6de")},
			{Name: "dark", Default: pixelfx.Color("#2a1040")},
			{Name: "seed", Default: pixelfx.Num(11)},
		},
	},
	Act: &pixelfx.Act{
		OnBody:  true,
		Follow:  pixelfx.Val(0),
		Targets: []float64{104},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: pixelfx.Ready},
			{T: -0.2, Pose: pixelfx.Ready.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-2), pixelfx.FX.Is(3), pixelfx.FY.Is(-27), pixelfx.BX.Is(8), pixelfx.BY.Is(-20))},
			{T: 0, Pose: lsGrasp, How: "snap"},
			{T: 0.4, Pose: lsGrasp},
			{T: 0.7, Pose: lsGrasp.With(pixelfx.FX.Is(13), pixelfx.FY.Is(-26), pixelfx.Lean.Is(1))},
			{T: 0.8, Pose: lsFed.With(pixelfx.Crouch.Is(2)), How: "snap"},
			{T: 1.22, Pose: lsFed},
			{T: 1.7, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-11), pixelfx.FY.Is(-26), pixelfx.BX.Is(12), pixelfx.BY.Is(-26), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 2.2, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: 0.01, Stop: 0.07, Shake: 0.12, Amp: pixelfx.Val(1), Push: pixelfx.Val(1.6), Straw: pixelfx.Val(12), Skin: "red", Y: pixelfx.Val(-24)},
			{T: 1.6, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
	},
	New: newLifeSteal,
}

func init() { pixelfx.Register("shadow/life-steal", LifeSteal) }

// the target point on the right, the caster's heart; when the orbs turn for home
const (
	lsTX = 104.0
	lsTY = -24.0
	lsHX = 1.0
	lsHY = -20.0
	lsN  = 7
)

type lsOrb struct {
	x, y, vx, vy, home, at, size float64
	trail                        [][2]float64
	done                         bool
}

type lsStain struct {
	x, t0 float64
}

type lsLight struct {
	x, y, r, k, life, max float64
	c                     uint32
}

func newLifeSteal(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	A := o.Actor
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[7], C[6], C[5]}})
	var stains []lsStain
	var lights []lsLight
	t, flush := 0.0, 0.0
	hit := false
	dr := newDrops(dropsOpts{
		onLand: func(x, _, _, _ float64) {
			stains = append(stains, lsStain{x: x, t0: t})
		},
	})
	orbs := make([]lsOrb, lsN)
	for i := range lsN {
		a := -pi/2 + (float64(i)-float64(lsN-1)/2)*0.4 + R.Range(-0.12, 0.12)
		sp := R.Range(100, 170)
		home := 0.36 + float64(i)*0.05 + R.Range(0, 0.04)
		sz := R.Range(1.6, 2.4)
		orbs[i] = lsOrb{x: lsTX, y: lsTY, vx: math.Cos(a)*sp + 10, vy: math.Sin(a) * sp, home: home, size: sz}
	}
	addLight := func(x, y, r float64, c uint32, k, life float64) {
		lights = append(lights, lsLight{x: x, y: y, r: r, c: c, k: k, life: life, max: life})
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit {
				hit = true
				addLight(lsTX, lsTY, 54, C[5], 1, 0.35)
				addLight(lsTX, lsTY, 18, pixelfx.White, 1, 0.08)
				for range 14 {
					dr.add(lsTX+R.Range(-3, 3), lsTY+R.Range(-3, 3), R.Range(-40, 90), R.Range(-140, -20), pick(R.Chance(0.4), 2.0, 1.0))
				}
				for range 12 {
					a, sp := R.Range(0, tau), R.Range(80, 170)
					ps.Spawn(pixelfx.Spark, lsTX, lsTY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
				}
			}
			for i := range orbs {
				b := &orbs[i]
				if b.done {
					continue
				}
				if t < b.home {
					// burst out, slowing to a hover
					b.vx -= b.vx * 5 * dt
					b.vy -= b.vy * 5 * dt
				} else {
					// whip home: steer hard at the caster's heart, faster and faster
					dx, dy := lsHX-b.x, lsHY-b.y
					d := pixelfx.Hypot(dx, dy)
					if d == 0 {
						d = 1
					}
					pull := 2600 * math.Min(1, (t-b.home)*3)
					b.vx += (dx / d) * pull * dt
					b.vy += (dy / d) * pull * dt
					b.vx -= b.vx * 2.2 * dt
					b.vy -= b.vy * 2.2 * dt
					if d < 6 {
						b.done = true
						b.at = t
						flush = math.Min(1.5, flush+0.45)
						addLight(lsHX, lsHY, 30, C[5], 0.8, 0.25)
						for range 6 {
							a, sp := R.Range(0, tau), R.Range(40, 100)
							ps.Spawn(pixelfx.Spark, lsHX, lsHY, math.Cos(a)*sp, math.Sin(a)*sp-20, R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
						}
						for range 3 {
							ps.Spawn(pixelfx.Ember, lsHX+R.Range(-8, 8), lsHY+R.Range(-6, 14), R.Range(-10, 10), -R.Range(15, 35), R.Range(0.6, 1.1), C[6], 1, 0, 0)
						}
					}
				}
				b.x += b.vx * dt
				b.y += b.vy * dt
				b.trail = slices.Insert(b.trail, 0, [2]float64{b.x, b.y})
				if len(b.trail) > 26 {
					b.trail = b.trail[:26]
				}
			}
			flush -= flush * 2.6 * dt
			for i := len(lights) - 1; i >= 0; i-- {
				lights[i].life -= dt
				if lights[i].life <= 0 {
					lights = slices.Delete(lights, i, i+1)
				}
			}
			for i := len(stains) - 1; i >= 0; i-- {
				if t-stains[i].t0 > 0.8 {
					stains = slices.Delete(stains, i, i+1)
				}
			}
			dr.update(dt)
			ps.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the wound: a dark red bloom where the target was struck, thinning away
			k := 1 - pixelfx.Smooth(0.1, 0.9, t)
			if k > 0 {
				p.Disc(lsTX, lsTY, 6+t*10, C[2], 0.5*k)
				p.Glow(lsTX, lsTY, 30, C[4], 0.35*k)
			}
			for _, s := range stains {
				p.Dot(s.x, -0.5, C[3], 1-(t-s.t0)/0.8)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the caster flushes crimson as the orbs land: its own colours pushed toward blood-red, a hot outline
			fk := math.Min(1, flush) * (1 - pixelfx.Smooth(1.5, 2.05, t))
			if A != nil && fk > 0.03 {
				for my := 0; my < A.H; my++ {
					for mx := 0; mx < A.W; mx++ {
						c0 := A.Pixels[my*A.W+mx]
						if c0 == 0 {
							continue
						}
						l := pixelfx.Luma(c0)
						c := C[3]
						switch {
						case l > 0.6:
							c = C[6]
						case l > 0.4:
							c = C[5]
						case l > 0.2:
							c = C[4]
						}
						p.Blend(A.X+float64(mx), A.Y+float64(my), c, 0.45*fk)
					}
				}
				if fk > 0.12 {
					oc := C[3]
					if fk > 0.6 {
						oc = C[6]
					} else if fk > 0.3 {
						oc = C[5]
					}
					p.Outline(A.Mask, A.X, A.Y, oc, 1)
				}
				p.Glow(lsHX, lsHY, 28, C[5], 0.45*fk)
			}
			// the hit on the target
			if t < 0.12 {
				pixelfx.Sparkle(p, lsTX, lsTY, 10*(1-t/0.12), C[7], C[5], 0.6)
			}
			ru := t / 0.28
			if ru < 1 {
				ringFast(p, lsTX, lsTY, 4+16*pixelfx.Ease.OutCubic(ru), math.Max(1, 2*(1-ru)), pick(ru < 0.4, C[7], C[5]), 1)
			}
			dr.draw(p, C[3], C[6])
			// the orbs: comet tails tapering from a hot head through blood-red to a dark wine thread
			for i := range orbs {
				b := &orbs[i]
				if b.done && t-b.at > 0.2 {
					continue
				}
				// the tail is swallowed after the head: it shortens rather than fades
				n := len(b.trail)
				if b.done {
					n = int(math.Floor(float64(len(b.trail)) * (1 - (t-b.at)/0.2)))
				}
				for j := n - 1; j >= 1; j-- {
					u := float64(j) / 25
					x0, y0 := b.trail[j-1][0], b.trail[j-1][1]
					x1, y1 := b.trail[j][0], b.trail[j][1]
					lc := C[2]
					switch {
					case u < 0.15:
						lc = C[7]
					case u < 0.35:
						lc = C[6]
					case u < 0.6:
						lc = C[5]
					case u < 0.8:
						lc = C[4]
					}
					p.Line(x1, y1, x0, y0, lc, 1)
					if j < 7 {
						p.Disc(x1, y1, (b.size+0.4)*(1-float64(j)/7), pick(u < 0.12, C[6], C[5]), 0.8)
					}
					if j%6 == 3 {
						p.Glow(x1, y1, 6, C[5], 0.2*(1-u))
					}
				}
				if b.done {
					continue
				}
				pulse := 1.0
				if t < b.home {
					pulse = 0.8 + 0.2*math.Sin(t*30+b.size*9)
				}
				p.Circle(b.x, b.y, b.size+0.9, C[4], 1)
				p.Circle(b.x, b.y, b.size*pulse, C[6], 1)
				p.Px(b.x-0.5, b.y-0.5, pixelfx.White)
				p.Glow(b.x, b.y, 9, C[5], 0.5)
			}
			ps.Draw(p, pixelfx.LayerBoth)
			for _, l := range lights {
				q := l.life / l.max
				p.Glow(l.x, l.y, l.r*(0.6+0.4*q), l.c, l.k*q*q)
			}
		},
	}
}
