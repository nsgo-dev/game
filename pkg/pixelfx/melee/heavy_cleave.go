// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/heavy-cleave.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent and its cooling ramp from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), and the impact star, dust, grit and embers from his impact and explosion (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	cleaveRest  = pixelfx.Ready.With(pixelfx.Two.On(), pixelfx.SA.Is(-1), pixelfx.FX.Is(11), pixelfx.FY.Is(-21)).Hold("greatsword")
	cleaveOver  = cleaveRest.With(pixelfx.X.Is(-4), pixelfx.Crouch.Is(2), pixelfx.Lean.Is(-2), pixelfx.FX.Is(1), pixelfx.FY.Is(-42), pixelfx.SA.Is(-2.45), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-7))
	cleaveFloor = cleaveRest.With(pixelfx.X.Is(0), pixelfx.Crouch.Is(7), pixelfx.Lean.Is(6), pixelfx.FX.Is(17), pixelfx.FY.Is(-21), pixelfx.SA.Is(0.72), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// HeavyCleave is a glint over the head, then a thick ember arc that accelerates over the top into the floor: a white slam, glowing cracks, dust rolling both ways, grit and rising embers. It is registered as "melee/heavy-cleave".
var HeavyCleave = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Heavy cleave",
		Desc:     "A glint over the head, then a thick ember arc accelerates over the top into the floor: a white slam, glowing cracks, dust rolling both ways, grit and rising embers.",
		Tags:     []string{"melee", "slash", "cleave", "axe", "heavy", "ground", "fire", "combat", "directional", "one-shot"},
		Size:     [2]int{208, 116},
		Origin:   [2]int{68, 96},
		Duration: 1.9,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0c10", "#5c1414", "#a3241a", "#e0481c", "#ff8a2a", "#ffc23d", "#fff0a0", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{66},
		Poses: []pixelfx.Key{
			{T: -0.42, Pose: cleaveRest.With(pixelfx.X.Is(-4))},
			{T: -0.3, Pose: cleaveRest.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.FX.Is(13), pixelfx.FY.Is(-29), pixelfx.SA.Is(-1.3))},
			{T: -0.12, Pose: cleaveOver.With(pixelfx.Crouch.Is(4))},
			{T: 0.2, Pose: cleaveOver.With(pixelfx.Crouch.Is(3))},
			{T: 0.27, Pose: cleaveRest.With(pixelfx.X.Is(-1), pixelfx.Y.Is(-2), pixelfx.Crouch.Is(0), pixelfx.Lean.Is(2), pixelfx.FX.Is(11), pixelfx.FY.Is(-42), pixelfx.SA.Is(-1.45), pixelfx.FFX.Is(10), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.31, Pose: cleaveRest.With(pixelfx.X.Is(0), pixelfx.Y.Is(-1), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(4), pixelfx.FX.Is(18), pixelfx.FY.Is(-33), pixelfx.SA.Is(-0.45), pixelfx.FFX.Is(12), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.335, Pose: cleaveFloor, How: "snap"},
			{T: 0.85, Pose: cleaveFloor.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(5))},
			{T: 1.3, Pose: cleaveRest.With(pixelfx.X.Is(-2), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(2), pixelfx.FX.Is(15), pixelfx.FY.Is(-23), pixelfx.SA.Is(0.1))},
			{T: 1.7, Pose: cleaveRest.With(pixelfx.X.Is(-4))},
		},
		Hits: []pixelfx.Hit{{T: 0.335, Stop: 0.2, Shake: 0.34, Amp: pixelfx.Val(3), Push: pixelfx.Val(3.2), Straw: pixelfx.Val(28), Y: pixelfx.Val(-8)}},
		Dust: []pixelfx.DustKick{{T: 0.335, X: 18, N: 8, Dir: 1}, {T: 0.335, X: -12, N: 5, Dir: -1}},
	},
	New: newHeavyCleave,
}

func init() { pixelfx.Register("melee/heavy-cleave", HeavyCleave) }

var (
	cleaveRaise = 0.2   // the glint over the head
	cleaveSwing = 0.135 // the arc accelerates over the top
	cleaveHit   = cleaveRaise + cleaveSwing
	cleaveHold  = 0.05
	cleaveTrail = 0.22
)

// cleaveHead returns the fraction of the arc the edge has made at t.
func cleaveHead(t float64) float64 {
	return pixelfx.Ease.InQuad(pixelfx.Clamp((t - cleaveRaise) / cleaveSwing))
}

// cleavePassedAt returns when the edge passed fraction f of the arc.
func cleavePassedAt(f float64) float64 { return cleaveRaise + cleaveSwing*math.Sqrt(pixelfx.Clamp(f)) }

func newHeavyCleave(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	n, st := len(C), stone(o.Color("stone"))
	R, ps, lights := newSim(o.Seed(), []uint32{C[6], C[5], C[4], C[3]})
	bits := pixelfx.NewBits(floor)
	// the arc: from behind the head, over the top, down into the floor in front
	A := swing{cx: 3, cy: -22, R: 37, sy: 0.88, rot: 0.32, a0: -2.45, dir: 1}
	U1, X := hitFloor(&A, A.R-2)
	E := A
	E.R = A.R * 0.76
	W, WE := 16.0, 5.0
	gx, gy := onSwing(&A, A.R-3, 0)
	lines := cracks(R, X, 4, 40, 7, 0.4)
	// the impact star: long spikes along the floor, shorter ones up
	spikes := make([]spike, 0, 7)
	for i, a := range []float64{-pi, -2.7, -2.15, -1.62, -1.05, -0.45, 0} {
		var s spike
		s.a = a + R.Range(-0.08, 0.08)
		l := 9.0
		switch i {
		case 0, 6:
			l = 20
		case 3:
			l = 15
		}
		s.len = l + R.Range(0, 4)
		s.w = 1.6 + R.Range(0, 0.6)
		spikes = append(spikes, s)
	}
	shards := make([]flyingSliver, 0, 4)
	for _, a := range []float64{-2.5, -1.9, -1.2, -0.6} {
		var s flyingSliver
		s.a = a + R.Range(-0.15, 0.15)
		s.r = 8 + R.Range(0, 3)
		s.v = R.Range(80, 130)
		s.arc = R.Range(0.35, 0.6)
		shards = append(shards, s)
	}
	t, hit := 0.0, false
	gone0, gone1 := cleaveHit+cleaveHold, cleaveHit+cleaveHold+cleaveTrail

	arc := func(p *pixelfx.Painter, s *swing, w0, lag float64, rim bool) {
		tt := t - lag
		U, T := U1*cleaveHead(tt), U1*pixelfx.Smooth(gone0, gone1, tt)
		if U < 0.02 || T >= U-0.02 {
			return
		}
		gone := pixelfx.Smooth(gone0, gone1, tt)
		shrink := 1 - 0.75*gone
		smear(p, s, T, U, w0, func(u, r, _ float64, out *pixelfx.Out) {
			age, v := math.Max(0, tt-cleavePassedAt(u/U1)), (u-T)/(U-T)
			w := w0 * shrink * math.Pow(math.Sin(pi*math.Pow(v, 2.2)), 0.6)
			if w < 0.4 {
				return
			}
			q := (s.R - r) / w
			if q < 0 || q > 1 {
				return
			}
			ci := 0.0
			if !(rim && q < 0.22 && age < 0.12) {
				ci = pixelfx.Round(q*float64(n-1)*0.62 + age*7 + math.Max(0, tt-cleaveHit)*10 - v*0.8 + either(rim, 0.7, 1.7))
			}
			out.C = C[n-1-int(pixelfx.ClampTo(ci, 0, float64(n-1)))]
			out.A = math.Min(1, v/0.35) * (1.2 - gone)
		}, 0, 1)
		if rim {
			glowAlong(p, s, s.R-w0*0.4, T, U, 7+w0*0.5, C[4], func(u float64) float64 {
				return 0.26 * math.Exp(-math.Max(0, tt-cleavePassedAt(u/U1))/0.2) * ((u - T) / (U - T))
			}, 0, 0.35)
		}
	}
	ringCols := []uint32{st.body, st.light, C[4], C[6]}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// heat gathers to the raised blade
			if t < cleaveRaise-0.04 && R.Chance(0.35) {
				a, D, T := R.Range(0, pixelfx.Tau), R.Range(14, 26), R.Range(0.14, 0.2)
				v := (D * 3) / (math.Exp(3*T) - 1)
				ps.Spawn(pixelfx.Streak, gx+math.Cos(a)*D, gy+math.Sin(a)*D, -math.Cos(a)*v, -math.Sin(a)*v, T, either(R.Chance(0.5), C[5], C[4]), 1, 0, 0)
			}
			// embers shed off the edge as it comes over
			if t > cleaveRaise && t < cleaveHit && R.Chance(0.7) {
				u := U1 * cleaveHead(t) * R.Range(0.6, 1)
				x, y := onSwing(&A, A.R-R.Range(0, 6), u)
				ps.Spawn(pixelfx.Ember, x, y, R.Range(-20, 20), R.Range(-40, -10), R.Range(0.5, 1.1), C[4], R.Range(1, 2), 0, 0)
			}
			if !hit && t >= cleaveHit {
				hit = true
				lights.Add(X, -6, 26, pixelfx.White, 0.9, 0.1)
				lights.Add(X, -12, 64, C[4], 0.5, 0.8)
				// sparks fan up off the floor, most of them forward
				for range 26 {
					a, sp := -pi/2+R.Range(-1.2, 1.35), R.Range(140, 360)
					ps.Spawn(pixelfx.Spark, X, -1, math.Cos(a)*sp, math.Sin(a)*sp-40, R.Range(0.15, 0.5), pixelfx.White, 1, 0, 0)
				}
				// dust rolls out both ways along the floor
				for i := range 20 {
					s := either(i%2 != 0, -1.0, 1)
					ps.Spawn(pixelfx.Smoke, X+s*R.Range(0, 8), -R.Range(2, 6), s*R.Range(45, 135), -R.Range(2, 12), R.Range(1.0, 1.45), st.dust, R.Range(5, 8.5), R.Range(0, 0.07), pixelfx.Big)
				}
				for range 4 {
					ps.Spawn(pixelfx.Smoke, X+R.Range(-4, 4), -R.Range(3, 8), R.Range(-15, 15), -R.Range(40, 90), R.Range(1.0, 1.45), st.dust, R.Range(4, 6.5), R.Range(0.02, 0.1), pixelfx.Big)
				}
				for range 8 {
					ps.Spawn(pixelfx.Dust, X+R.Range(-4, 4), -R.Range(1, 4), R.Range(-60, 60), -R.Range(8, 30), R.Range(0.35, 0.7), st.dust, R.Range(2, 3.5), 0, 0)
				}
				// grit and a few stones thrown up, forward
				for range 44 {
					a, sp := -pi/2+R.Range(-0.7, 1.0), R.Range(60, 230)
					ps.Spawn(pixelfx.Grit, X+R.Range(-3, 3), -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.0, 1.5), pixelfx.Pick(R, []uint32{st.light, st.body, st.body, st.shade}), either(R.Chance(0.35), 2.0, 1), 0, 0)
				}
				for range 6 {
					b := pixelfx.Bit{Kind: pixelfx.Chunk, Y: -2, G: 1, Drag: 0.4, Bounce: 0.3}
					b.X = X + R.Range(-3, 3)
					b.VX = R.Range(-50, 110)
					b.VY = -R.Range(120, 260)
					b.Life = R.Range(1.15, 1.5)
					b.A = R.Range(0, pixelfx.Tau)
					b.VA = R.Range(-14, 14)
					b.S = R.Int(1, 2)
					bits.Add(b)
				}
				// embers lift off the cut
				for range 16 {
					ps.Spawn(pixelfx.Ember, X+R.Range(-8, 8), -R.Range(1, 6), R.Range(-50, 60), -R.Range(30, 110), R.Range(0.7, 1.5), C[4], R.Range(1, 2), 0, 0)
				}
			}
			// the cracks breathe embers while they're hot
			if hit && t < cleaveHit+0.6 && R.Chance(0.12) {
				c := pixelfx.Pick(R, lines)
				pt := pixelfx.Pick(R, c.pts)
				ps.Spawn(pixelfx.Ember, pt[0], pt[1]-1, R.Range(-12, 12), -R.Range(20, 50), R.Range(0.5, 0.9), C[4], 1, 0, 0)
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 2 {
						ps.Spawn(pixelfx.Dust, b.X+R.Range(-1.5, 1.5), -0.5, R.Sign()*R.Range(8, 30), -R.Range(2, 12), R.Range(0.3, 0.6), st.dust, R.Range(1.3, 2.4), 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if q := t - cleaveHit; q >= 0 {
				// the floor under the blade: white hot, cooling through the ramp to soot
				fade := 1 - pixelfx.Smooth(1.0, 1.5, q)
				for y := 0.0; y <= 2; y++ {
					for x := -9.0; x <= 9; x++ {
						d := pixelfx.Hypot((x+0.5)/9, (y+0.5)/3.5)
						if d > 1 {
							continue
						}
						T := math.Pow(pixelfx.Clamp(1-q/0.8), 1.4)*(1.2-d) - pixelfx.Rand(x, y, 3)*0.3
						if T > 0.08 {
							p.Dot(X+x, y, C[int(pixelfx.ClampTo(pixelfx.Round(T*9), 1, 7))], fade)
						} else {
							p.Dot(X+x, y, either(d < 0.5, st.dark, st.shade), either(d < 0.9-pixelfx.Rand(x, y, 9)*0.3, 1, 1-d)*fade)
						}
					}
				}
				// the shock runs out along the floor: the far half behind the fighter
				if k := q / 0.45; k < 1 {
					pixelfx.Band(p, X, 0.5, 6+46*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 3*(1-k)), 1.15-k, ringCols, func(g float64) bool { return math.Sin(g) < 0 }, false)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			q := t - cleaveHit
			// the glint over the head: the raised blade catching the light
			if t < cleaveRaise+0.03 {
				g := pixelfx.Ease.OutBack(pixelfx.Clamp(t/0.1)) * (1 + 0.15*math.Sin(t*60)) * either(t > cleaveRaise, 1-(t-cleaveRaise)/0.03, 1)
				pixelfx.Sparkle(p, gx, gy, 1+7*g, C[6], C[4], 0.5)
				p.Glow(gx, gy, 22, C[4], 0.3*g)
			}
			if q >= 0 {
				// cracks race out from the cut and split down into the floor: white hot, cooling down the ramp to dark
				heat, fade := math.Pow(pixelfx.Clamp(1-q/1.2), 1.2), 1-pixelfx.Smooth(1.15, 1.5, q)
				for _, c := range lines {
					reach := 40 * pixelfx.Ease.OutCubic(pixelfx.Clamp((q-c.at*0.1)/0.13)) * either(c.fork, 0.3, 1)
					if reach <= 0 {
						continue
					}
					along(c.pts, reach, func(x0, y0, x1, y1, d float64) {
						h := heat*(1-d/46) - either(c.fork, 0.12, 0)
						col := st.dark
						if h > 0.72 {
							col = pixelfx.White
						} else if h > 0.08 {
							col = C[int(pixelfx.ClampTo(pixelfx.Round(h*8), 1, 6))]
						}
						if y1 > 0.5 {
							p.Line(x0+1, y0, x1+1, y1, st.dark, fade)
						}
						p.Line(x0, y0, x1, y1, col, fade)
						if h > 0.25 && pixelfx.ToInt32(d)%6 < 2 {
							p.Glow(x1, y1, 6, C[4], 0.2*h*fade)
						}
					})
				}
				if heat > 0 {
					p.Glow(X, -2, 34, C[4], 0.32*heat*fade)
				}
				// the near half of the shock ring
				if k := q / 0.45; k < 1 {
					pixelfx.Band(p, X, 0.5, 6+46*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 3*(1-k)), 1.15-k, ringCols, func(g float64) bool { return math.Sin(g) >= 0 }, false)
				}
			}
			// the arc and its echo, then a dotted trace of embers where it has been
			arc(p, &E, WE, 0.02, false)
			arc(p, &A, W, 0, true)
			tail, th := U1*pixelfx.Smooth(gone0, gone1, t), 0.8*(1-pixelfx.Clamp((t-cleaveHit-cleaveHold)/(cleaveTrail+0.25)))
			if tail > 0.02 && th > 0 {
				trace(p, &A, A.R-0.5, 0, tail, C[3], func(u float64) float64 { return th * (0.4 + 0.6*u/U1) }, 0, 0.6)
			}
			if t > cleaveRaise && t < cleaveHit+0.05 {
				x, y := onSwing(&A, A.R-5, U1*cleaveHead(t))
				p.Glow(x, y, 40, C[4], 0.5)
			}
			// the white slam: an impact star flat on the floor, then slivers flying off and a hot pool
			if q >= 0 && q < 0.035 {
				star(p, X, -1, spikes, 1, 1, pixelfx.White, C[6], C[7], C[5])
			} else if q >= 0.035 && q < 0.07 {
				star(p, X, -1, spikes, 1.25, 0.55, C[7], C[6], C[6], C[4])
			}
			if q >= 0.05 && q < 0.16 {
				k := (q - 0.05) / 0.11
				for _, s := range shards {
					sliver(p, X, -1, s.a, s.r+s.v*(q-0.05), s.arc*(1-0.4*k), sliverCol(C, k), 1-k)
				}
			}
			if q >= 0 && q < 0.2 {
				pop := math.Exp(-q / 0.05)
				p.Glow(X, -6, 30, C[5], 0.8*pop)
				p.Glow(X, -2, 10, pixelfx.White, 0.7*pop)
			}
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
