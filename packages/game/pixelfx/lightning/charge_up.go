// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/charge-up.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The charge (streaks spiralling in, a core that swells with an accelerating pulse, little arcs spitting off it) and the release (flash, rings, a spray of motes and stars) follow Sprite Fusion's open-source energy charge (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	cuHold  = pixelfx.Ready.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(-2), pixelfx.FX.Is(15), pixelfx.FY.Is(-21), pixelfx.BX.Is(9), pixelfx.BY.Is(-17), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))
	cuBurst = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Y.Is(-2), pixelfx.Crouch.Is(0), pixelfx.Lean.Is(-2), pixelfx.FX.Is(-13), pixelfx.FY.Is(-37), pixelfx.BX.Is(14), pixelfx.BY.Is(-36), pixelfx.FFX.Is(-2), pixelfx.FFY.Is(-6), pixelfx.FBX.Is(3), pixelfx.FBY.Is(-6))
)

// ChargeUp is sparks streaking and arcs whipping in to a crackling core at the caster's hand that swells and pulses faster, then flashes and bursts: bolts every way, a ring, a spray of sparks. It is registered as "lightning/charge-up".
var ChargeUp = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Charge up",
		Desc:     "Sparks streak and arcs whip in to a crackling core at the caster's hand that swells and pulses faster, then it flashes and bursts: bolts every way, a ring, a spray of sparks.",
		Tags:     []string{"lightning", "electric", "charge", "spell", "power-up", "actor", "one-shot"},
		Size:     [2]int{272, 184},
		Origin:   [2]int{124, 148},
		Duration: 2.7,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: append(voltOptions(),
			pixelfx.Option{Name: "charge", Default: pixelfx.Num(1.6)},
			pixelfx.Option{Name: "seed", Default: pixelfx.Num(4)},
		),
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: -0.36, Pose: pixelfx.Ready},
			{T: 0, Pose: cuHold},
			{T: 0.8, Pose: cuHold.With(pixelfx.Crouch.Is(4))},
			{T: 1.4, Pose: cuHold.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(0))},
			{T: 1.59, Pose: cuHold.With(pixelfx.Crouch.Is(7), pixelfx.Lean.Is(0))},
			{T: 1.6, Pose: cuBurst, How: "snap"},
			{T: 1.95, Pose: cuBurst.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(2), pixelfx.FY.Is(-30), pixelfx.BY.Is(-30), pixelfx.FFY.Is(-4), pixelfx.FBY.Is(-4))},
			{T: 2.7, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{{T: 1.6, Stop: 0.14, Shake: 0.3, Amp: pixelfx.Val(3)}},
		Dust: []pixelfx.DustKick{{T: 1.6, X: 10, N: 6, Dir: 1}, {T: 1.6, X: -10, N: 6, Dir: -1}},
	},
	New: newChargeUp,
}

func init() { pixelfx.Register("lightning/charge-up", ChargeUp) }

// the core at the caster's hand
const (
	cuCX = 15
	cuCY = -21
)

// cuBK is a burst bolt's brightness over the frames it lives.
var cuBK = [...]float64{1, 0.7, 0.95, 0.5, 0.8, 0.35}

var cuAsh = pixelfx.Hex("#5b5566")

type cuBolt struct{ x1, y1, t0 float64 }

type cuArc struct{ x, y, t, seed float64 }

func newChargeUp(o *pixelfx.Options) pixelfx.Instance {
	C, seed, REL, A := pixelfx.Fit(o.Palette(), 8), o.Seed(), o.Num("charge"), o.Actor
	R := pixelfx.NewRng(seed)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: o.Ramp("sparks")})
	lights := &pixelfx.Lights{}
	var bolts [8]cuBolt
	for i := range bolts {
		a, L := (float64(i)/8)*tau+R.Range(-0.25, 0.25), R.Range(44, 72)
		bolts[i] = cuBolt{x1: cuCX + math.Cos(a)*L, y1: math.Min(0, cuCY+math.Sin(a)*L), t0: REL + R.Range(0, 0.05)}
	}
	E := actorEdge(A, 0)
	bright := []uint32{C[7], C[6], C[5]}
	var arcs []cuArc
	var beats []float64
	t, phase, released := 0.0, 0.0, false
	level := func() float64 { return pixelfx.Clamp(t / REL) }
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			k := level()
			e := k * k
			if t < REL {
				// energy rushes in: long streaks spiralling in from all round, more and more of them
				for acc := (60 + 360*e) * dt; acc > 0; acc-- {
					if acc < 1 && !R.Chance(acc) {
						break
					}
					a, d, life := R.Range(0, tau), R.Range(30, 72)*(1-0.35*k), R.Range(0.16, 0.26)
					sp := (d / life) * 0.95
					ps.Spawn(pixelfx.Mote, cuCX+math.Cos(a)*d, math.Min(-2, cuCY+math.Sin(a)*d), -math.Cos(a)*sp-math.Sin(a)*sp*0.4, -math.Sin(a)*sp+math.Cos(a)*sp*0.4, life, pixelfx.Pick(R, bright), 1, 0, 0)
				}
				// arcs whip in from the air, more and more of them
				if k > 0.2 && R.Chance((2+16*e)*dt) {
					a, d := R.Range(0, tau), R.Range(20, 40)
					arcs = append(arcs, cuArc{x: cuCX + math.Cos(a)*d, y: math.Min(-3, cuCY+math.Sin(a)*d), t: t, seed: R.Int(0, 1e6)})
				}
				// the pulse quickens
				phase += (1.5 + 11*e) * dt
				if phase >= 1 {
					phase--
					beats = append(beats, t)
				}
				if R.Chance(4 * e * dt) {
					ps.Spawn(pixelfx.StarKind, cuCX+R.Range(-14, 14), cuCY+R.Range(-14, 10), 0, -4, R.Range(0.2, 0.35), C[6], R.Range(1, 2), 0, 0)
				}
			} else if !released {
				released = true
				lights.Add(cuCX, cuCY, 110, C[4], 0.8, 0.5)
				lights.Add(cuCX, cuCY, 28, pixelfx.White, 0.6, 0.08)
				for range 70 {
					a := R.Range(0, tau)
					sp := R.Range(140, 330)
					if math.Sin(a) > 0.4 {
						sp *= 0.55
					}
					kind := pixelfx.Spark
					if math.Sin(a) < -0.2 && R.Chance(0.35) {
						kind = pixelfx.Mote
					}
					ps.Spawn(kind, cuCX, cuCY, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.25, 0.55), pixelfx.Pick(R, bright), R.Range(0.8, 1.4), 0, 0)
				}
				for range 10 {
					a, sp := R.Range(0, tau), R.Range(60, 160)
					life := R.Range(0.4, 0.8)
					c := C[5]
					if R.Chance(0.5) {
						c = C[6]
					}
					ps.Spawn(pixelfx.StarKind, cuCX, cuCY, math.Cos(a)*sp, math.Sin(a)*sp, life, c, R.Range(1.2, 2.4), 0, 0)
				}
				for range 4 {
					ps.Spawn(pixelfx.Smoke, cuCX+R.Range(-5, 5), cuCY+R.Range(-4, 4), R.Range(-12, 12), -R.Range(8, 18), R.Range(0.6, 0.85), pixelfx.Mix(cuAsh, C[4], 0.3), R.Range(3, 5), R.Range(0.05, 0.15), 0)
				}
			}
			beats = slices.DeleteFunc(beats, func(b float64) bool { return !(t-b < 0.22) })
			for i := len(arcs) - 1; i >= 0; i-- {
				if t-arcs[i].t > 0.07 {
					arcs = slices.Delete(arcs, i, i+1)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if k := level(); t < REL {
				p.Glow(cuCX*0.5, -3, 40, C[3], 0.3*k*k)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			k := level()
			e := k * k
			f := int(math.Floor(t*30 + 1e-4))
			if t < REL {
				jit := 0.0
				if k > 0.85 {
					jit = -1
					if f%2 != 0 {
						jit = 1
					}
					jit *= (k - 0.85) / 0.15
				}
				x, y, pr := cuCX+jit, float64(cuCY), 0.5+0.5*math.Sin(phase*tau)
				// the body catches the core's light along its near edge
				for _, ed := range E {
					d := pixelfx.Hypot(ed[0]+0.5-x, ed[1]+0.5-y)
					if d < 6+10*k && e > 0.08 {
						c := C[4]
						if d < 4+6*k && e > 0.4 {
							c = C[6]
						}
						p.Px(ed[0], ed[1], c)
					}
				}
				// rings fall inward to the core on every beat
				for _, b := range beats {
					q := (t - b) / 0.22
					c := C[4]
					if q > 0.6 {
						c = C[6]
					}
					p.Ring(x, y, 4+20*(1-pixelfx.Ease.InQuad(q))*(0.5+k), 1, c, 0.4+0.6*q)
				}
				// arcs whipping in to the core
				for _, a := range arcs {
					crackle(p, zigzag(pixelfx.RngOf(a.seed, float64(f)), a.x, a.y, x, y, 0.35, 0.4), 0.85, C, 0.4, 1)
				}
				// the core: swelling, layered, with arcs racing round its skin
				r := 1.2 + 6.5*e + pr*(0.5+1.2*k)
				p.Circle(x, y, r+1.3, C[2], 1)
				p.Circle(x, y, r, C[4], 1)
				p.Circle(x-r*0.12, y-r*0.12, r*0.7, C[5], 1)
				p.Circle(x, y, r*0.38, pixelfx.White, 1)
				if r > 3 {
					for _, s := range [2]float64{0, pi} {
						for a := 0.0; a < 1.3; a += 0.12 {
							ang, rr := t*10+s+a, r*(0.62+0.1*math.Sin(a*3))
							c := C[6]
							if a < 0.5 {
								c = pixelfx.White
							}
							p.Dot(x+math.Cos(ang)*rr, y+math.Sin(ang)*rr, c, 1)
						}
					}
				}
				if k > 0.35 {
					q := pixelfx.RngOf(seed, 5, float64(f))
					a, l := q.Range(0, tau), q.Range(3, 6)*(0.5+k)
					crackle(p, zigzag(q, x+math.Cos(a)*r, y+math.Sin(a)*r, x+math.Cos(a)*(r+l), y+math.Sin(a)*(r+l), 0.4, 0.3), 0.7, C, 0.3, 1)
				}
				p.Glow(x, y, 12+30*k, C[4], 0.35+0.45*k)
				p.Glow(x, y, 6+6*k, C[6], 0.25+0.3*k)
			} else {
				a := t - REL
				// the release: the caster flashed white, a flare, a white core blown open
				if a < 0.07 {
					c := C[6]
					if a < 0.035 {
						c = pixelfx.White
					}
					p.Mask(A.Mask, A.X, A.Y, c, 1)
				}
				if a < 0.22 {
					q := a / 0.22
					pixelfx.Flare(p, cuCX, cuCY, 46*math.Pow(1-q, 1.4), 1-q, C)
					if q < 0.3 {
						p.Circle(cuCX, cuCY, 12*(1-q*2.5)+1.5, pixelfx.White, 1)
					}
				}
				// bolts every way, struck and flickering
				for i, b := range bolts {
					ba := int(math.Floor((t-b.t0)*30 + 1e-4))
					if t < b.t0 || ba >= len(cuBK) {
						continue
					}
					s := 0.75
					if ba == 0 {
						s = 0.95
					}
					stroke(p, zigzag(pixelfx.RngOf(seed, 3, float64(i), float64(ba>>1)), cuCX, cuCY, b.x1, b.y1, 0.25, 0.75), cuBK[ba], C, s, strokeOpts{hero: ba == 0})
					if b.y1 > -1 {
						p.Glow(b.x1, -3, 18, C[5], 0.5*cuBK[ba])
					}
				}
				// a ring bursting out with a fainter echo inside it
				if q := a / 0.45; q < 1 {
					r := 8 + 80*pixelfx.Ease.OutCubic(q)
					c := C[5]
					if q < 0.25 {
						c = pixelfx.White
					}
					p.RingFn(cuCX, cuCY, r, math.Max(1, 3*(1-q)), func(_, g float64) (uint32, float64) {
						if cuCY+math.Sin(g)*r > -1 {
							return c, 0
						}
						return c, 1.1 - q
					})
					r2 := 6 + 56*pixelfx.Ease.OutCubic(math.Min(1, q*1.3))
					p.RingFn(cuCX, cuCY, r2, 1, func(_, g float64) (uint32, float64) {
						if cuCY+math.Sin(g)*r2 > -1 {
							return C[4], 0
						}
						return C[4], 0.8 - q
					})
					p.Glow(cuCX, cuCY-6, math.Min(r*0.9, 46), C[3], 0.42*(1-q)*(1-q))
				}
				// the charge bleeds off in a few last crackles at the hand
				if a > 0.15 && a < 0.5 {
					qq := pixelfx.RngOf(seed, 8, float64(f))
					ang, l := qq.Range(0, tau), qq.Range(5, 10)
					crackle(p, zigzag(qq, cuCX, cuCY, cuCX+math.Cos(ang)*l, cuCY+math.Sin(ang)*l, 0.45, 0.3), 0.8*(1-(a-0.15)/0.35)+0.15, C, 0.3, 1)
				}
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
