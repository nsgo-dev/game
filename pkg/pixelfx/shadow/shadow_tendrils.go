// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/shadow-tendrils.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// ShadowTendrils opens a dark pool on the floor; tentacles with lit rims and hooked tips lash up out of it with a splash, writhe, and sink back as it closes. It is registered as "shadow/shadow-tendrils".
var ShadowTendrils = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Shadow tendrils",
		Desc:     "A dark pool opens on the floor; tentacles with lit rims and hooked tips lash up out of it with a splash, writhe, and sink back as it closes.",
		Tags:     []string{"shadow", "tentacle", "tendril", "ground", "dark", "magic", "one-shot"},
		Size:     [2]int{208, 130},
		Origin:   [2]int{104, 106},
		Duration: 3.2,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newShadowTendrils,
}

func init() { pixelfx.Register("shadow/shadow-tendrils", ShadowTendrils) }

const (
	stRX   = 40.0
	stOpen = 0.34
	stShut = 2.5
	stSink = 0.5
	stLX   = -0.6
	stLY   = -0.8
)

type stWhip struct {
	t, a float64
}

type stTentacle struct {
	x, y              float64
	back              bool
	L, W              float64
	lean, curl        float64
	lam, om, ph, A, H float64
	t0, t1            float64
	whip              *stWhip
	up, down          bool
}

type stPt struct {
	x, y, th, w, u, s float64
}

type stSplat struct {
	x, t0, s float64
}

type stLight struct {
	x, y, r, k, life, max float64
}

// stSpine returns a tentacle's spine at t: it rises out of the pool along its own curve, a wave writhes up it, its lean sways and sometimes whips, and its tip curls into a hook; points every 0.5 px with angle and half-width.
func stSpine(T *stTentacle, t float64) []stPt {
	e := pixelfx.Clamp((t - T.t0) / 0.2)
	s1 := pixelfx.Clamp((t - T.t1) / stSink)
	if e <= 0 || s1 >= 1 {
		return nil
	}
	// it lashes up past its length and settles; going, it lifts a little, then slides back under
	lift := 0.07 * math.Sin(pi*pixelfx.Clamp(s1/0.3))
	down := pixelfx.Ease.InOutCubic(pixelfx.Clamp((s1 - 0.12) / 0.88))
	rise := pixelfx.OutBack(e, 1.8) * (1 + lift) * (1 - down)
	hidden := (1 - rise) * T.L
	writhe := T.A * (0.25 + 0.75*pixelfx.Smooth(T.t0+0.08, T.t0+0.5, t))
	hook := (0.04 + T.H*pixelfx.OutBack(pixelfx.Clamp((t-T.t0-0.1)/0.32), 2.5)) * (1 + 0.5*s1) * (1 + 0.18*math.Sin(t*2.3+T.ph))
	lean := T.lean + 0.12*math.Sin(t*1.6+T.ph)
	if T.whip != nil {
		dw := (t - T.whip.t) / 0.14
		lean += T.whip.a * math.Exp(-(dw * dw))
	}
	th := -pi/2 + lean
	x, y := T.x-math.Cos(th)*hidden, T.y-math.Sin(th)*hidden
	n := int(math.Ceil(T.L / 0.5))
	pts := make([]stPt, 0, n+1)
	for i := 0; i <= n; i++ {
		s := float64(i) * 0.5
		u := s / T.L
		pts = append(pts, stPt{x: x, y: y, th: th, w: T.W*math.Pow(1-u, 0.75) + 0.45, u: u, s: s})
		th += 0.5 * (writhe*math.Sin((s/T.lam)*tau-T.om*t+T.ph) + T.curl*hook*pixelfx.Smooth(0.58, 1, u))
		x += math.Cos(th) * 0.5
		y += math.Sin(th) * 0.5
	}
	return pts
}

// stDrawTentacle draws a tentacle in cross-sections: a dark body with a lighter half toward the light, then the rims: bright where it faces the light (white where it faces it squarely), violet on the side the pool lights, suckers inside the hook.
func stDrawTentacle(p *pixelfx.Painter, T *stTentacle, pts []stPt, C []uint32, t float64) {
	surf := T.y
	for _, q := range pts {
		if q.y > surf {
			continue
		}
		nx, ny := -math.Sin(q.th), math.Cos(q.th)
		ks := -1.0
		if nx*stLX+ny*stLY >= 0 {
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
	// a pulse of light runs up it as it lashes, then slower ones while it writhes
	pulse := (t - T.t0) * 150
	slow := math.Mod((t-T.t0)*55, T.L*1.8) - 4
	for _, q := range pts {
		if q.y > surf {
			continue
		}
		nx, ny := -math.Sin(q.th), math.Cos(q.th)
		f := nx*stLX + ny*stLY
		ks := -1.0
		if f >= 0 {
			ks = 1
		}
		lit := ks * q.w
		x0, y0 := q.x+nx*lit, q.y+ny*lit
		x1, y1 := q.x-nx*lit, q.y-ny*lit
		if y1 <= surf {
			p.Px(x1, y1, C[3])
		}
		if y0 <= surf {
			c := C[5]
			switch {
			case math.Abs(f) > 0.8 && q.u > 0.15:
				c = C[7]
			case q.u > 0.85 || math.Abs(f) > 0.3:
				c = C[6]
			}
			p.Px(x0, y0, c)
		}
		if q.u > 0.62 && q.u < 0.93 && math.Mod(q.s, 3) < 0.5 {
			p.Px(q.x+nx*T.curl*q.w*0.45, q.y+ny*T.curl*q.w*0.45, C[4])
		}
		dp := math.Min(math.Abs(q.s-pulse), math.Abs(q.s-slow)*1.6)
		if dp < 2.5 {
			p.Px(q.x, q.y, pick(dp < 1, C[7], C[5]))
		}
	}
	// the tip catches the light, and light runs along it
	tip := pts[len(pts)-1]
	if tip.y <= surf {
		p.Px(tip.x, tip.y, pixelfx.White)
		p.Glow(tip.x, tip.y, 6, C[6], 0.25)
	}
	for i := 0; i < len(pts); i += 8 {
		if pts[i].y < surf {
			p.Glow(pts[i].x, pts[i].y, 9, C[4], 0.1)
		}
	}
	pIdx := min(len(pts)-1, max(0, int(pixelfx.Round(pulse*2))))
	pq := pts[pIdx]
	if pulse < T.L && pq.y < surf {
		p.Glow(pq.x, pq.y, 10, C[6], 0.45)
	}
	// where it breaks the surface: a lit collar of ink
	for _, q := range pts {
		if q.y <= surf {
			w := q.w + 1.5
			p.Line(q.x-w, surf, q.x+w, surf, C[5], 1)
			p.Px(q.x-w-1, surf+0.5, C[3])
			p.Px(q.x+w+1, surf+0.5, C[3])
			break
		}
	}
}

func newShadowTendrils(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	DIM := []uint32{C[0], C[0], C[1], C[1], C[3], C[4], C[5], C[6]}
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
	var splats []stSplat
	var lights []stLight
	t, acc := 0.0, 0.0
	rx := func(tt float64) float64 {
		return stRX * pixelfx.Ease.OutCubic(pixelfx.Clamp(tt/stOpen)) * (1 - pixelfx.Ease.InCubic(pixelfx.Clamp((tt-stShut)/0.45)))
	}
	inPool := func(x, tt float64) bool { return math.Abs(x) < rx(tt)*0.95 }
	dr := newDrops(dropsOpts{
		onLand: func(x, _, _, s float64) {
			if inPool(x, t) {
				ripples = append(ripples, poolRipple{x: x, t0: t, life: 0.5, w: 0.6})
			} else {
				splats = append(splats, stSplat{x: x, t0: t, s: s})
			}
		},
	})
	// seven tentacles in two rows: the back row taller and dimmer, hooks mostly curling inward
	rows := [7][2]float64{{-22, -2.5}, {-8, -3}, {7, -3}, {21, -2.5}, {-28, 2.5}, {-1, 3}, {27, 2.5}}
	order := [7]float64{3, 0, 5, 2, 4, 1, 6}
	last := [7]float64{1, 5, 0, 6, 3, 2, 4}
	tents := make([]stTentacle, 7)
	for i, rw := range rows {
		ax, ay := rw[0], rw[1]
		back := ay < 0
		sx := pixelfx.Sign(ax)
		if sx == 0 {
			sx = 1
		}
		x := ax + R.Range(-3, 3)
		var L, W float64
		if back {
			L = R.Range(46, 62)
			W = R.Range(3.2, 4)
		} else {
			L = R.Range(32, 46)
			W = R.Range(3.8, 4.6)
		}
		lean := sx*(0.06+math.Abs(ax)/120) + R.Range(-0.06, 0.06)
		curl := pick(R.Chance(0.75), -sx, sx)
		lam := R.Range(36, 50)
		om := R.Range(4.5, 7) * R.Sign()
		ph := R.Range(0, tau)
		A := R.Range(0.06, 0.085)
		H := R.Range(0.15, 0.32)
		t0 := 0.3 + order[i]*0.045 + R.Range(0, 0.03)
		t1 := 1.5 + last[i]*0.1 + R.Range(0, 0.05)
		var whip *stWhip
		if R.Chance(0.45) {
			wt := R.Range(0.9, 1.5)
			wa := R.Range(0.28, 0.45) * R.Sign()
			whip = &stWhip{t: wt, a: wa}
		}
		tents[i] = stTentacle{x: x, y: ay, back: back, L: L, W: W, lean: lean, curl: curl, lam: lam, om: om, ph: ph, A: A, H: H, t0: t0, t1: t1, whip: whip}
	}
	moteCols := []uint32{C[7], C[6], C[6], C[5]}

	splash := func(x, y float64, n int, power float64) {
		for range n {
			dr.add(x+R.Range(-2, 2), y-1, R.Range(-70, 70)*power, -R.Range(90, 230)*power, pick(R.Chance(0.4), 2.0, 1.0))
		}
		ripples = append(ripples, poolRipple{x: x, t0: t, life: 0.7, w: 1})
		lights = append(lights, stLight{x: x, y: y - 7, r: 30, k: 0.6 * power, life: 0.25, max: 0.25})
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range tents {
				T := &tents[i]
				if !T.up && t >= T.t0 {
					T.up = true
					splash(T.x, T.y, 12, 1)
					for range 4 {
						ps.Spawn(pixelfx.Spark, T.x, T.y-2, R.Range(-80, 80), -R.Range(120, 260), R.Range(0.2, 0.45), pixelfx.White, 1, 0, 0)
					}
				}
				if !T.down && t >= T.t1+stSink*0.8 {
					T.down = true
					splash(T.x, T.y, 6, 0.6)
				}
				// glints run over the rims while they stand
				if t > T.t0+0.2 && t < T.t1 && R.Chance(dt*1.6) {
					pts := stSpine(T, t)
					if pts != nil {
						q := pts[int(math.Floor(R.Range(0.3, 0.95)*float64(len(pts))))]
						if q.y < T.y {
							ps.Spawn(pixelfx.StarKind, q.x-math.Sin(q.th)*q.w, q.y+math.Cos(q.th)*q.w, 0, -4, R.Range(0.25, 0.5), C[6], R.Range(1, 2.2), 0, 0)
						}
					}
				}
			}
			// the pool breathes out wisps and motes while it's open
			r := rx(t)
			if r > 8 && t < stShut-0.4 {
				for acc += dt * 9; acc > 1; acc-- {
					pf.spawn(pkSoot, R.Range(-0.8, 0.8)*r, -R.Range(1, 4), R.Range(-8, 8), -R.Range(14, 30), R.Range(0.6, 0.85), R.Range(3, 6), 0, false)
				}
				if R.Chance(dt * 16) {
					ps.Spawn(pixelfx.Mote, R.Range(-0.8, 0.8)*r, -2, R.Range(-8, 8), -R.Range(30, 80), R.Range(0.5, 1), pixelfx.Pick(R, moteCols), R.Range(0.6, 1.1), 0, 0)
				}
				if R.Chance(dt * 5) {
					ps.Spawn(pixelfx.Ember, R.Range(-0.8, 0.8)*r, -3, R.Range(-10, 10), -R.Range(20, 45), R.Range(0.6, 1), C[6], 1, 0, 0)
				}
				if R.Chance(dt * 3) {
					ripples = append(ripples, poolRipple{x: R.Range(-0.7, 0.7) * r, t0: t, life: 0.6, w: 0.5})
				}
				// a violet haze hangs over the pool, lit from inside
				if R.Chance(dt * 7) {
					ps.Spawn(pixelfx.Vapour, R.Range(-0.7, 0.7)*r, -R.Range(2, 8), R.Range(-6, 6), -R.Range(10, 22), R.Range(0.6, 0.9), pick(R.Chance(0.5), C[3], C[4]), R.Range(3, 6), 0, 0)
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
			k := r / stRX
			floorPool(p, C, r, t, ripples)
			if k > 0 {
				p.Glow(0, -16, 86, C[4], 0.22*k)
				p.Glow(0, -8, r*1.7+12, C[4], 0.32*k*(0.9+0.1*math.Sin(t*5)))
				p.Glow(0, 0, r*1.15, C[5], 0.18*k)
			}
			pf.draw(p, pkSoot, &puffLight{x: 0, y: 6, k: k, reach: 70})
			ps.Draw(p, pixelfx.LayerBack)
			for i := range tents {
				T := &tents[i]
				if T.back {
					if pts := stSpine(T, t); pts != nil {
						stDrawTentacle(p, T, pts, DIM, t)
					}
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			for i := range tents {
				T := &tents[i]
				if !T.back {
					if pts := stSpine(T, t); pts != nil {
						stDrawTentacle(p, T, pts, C, t)
					}
				}
			}
			dr.draw(p, C[1], C[5])
			for _, s := range splats {
				a := 1 - (t-s.t0)/0.6
				p.Dot(s.x, -0.5, C[1], a*1.5)
				p.Dot(s.x+1, -0.5, C[2], a)
				if s.s > 1 {
					p.Dot(s.x-1, -0.5, C[2], a)
				}
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
