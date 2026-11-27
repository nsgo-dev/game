// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/hammer-smash.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rocks, grit, rolling dust and floor ring from Sprite Fusion's open-source explosion, and the dome from his ice nova (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	// both hands low on the shaft, the head resting on the floor in front; it comes up, over and down
	hammerRest = pixelfx.Ready.With(pixelfx.Two.On(), pixelfx.SA.Is(0.62), pixelfx.FX.Is(11), pixelfx.FY.Is(-21), pixelfx.Crouch.Is(3)).Hold("hammer")
	hammerTop  = hammerRest.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(0), pixelfx.Lean.Is(-3), pixelfx.FX.Is(3), pixelfx.FY.Is(-41), pixelfx.SA.Is(-2.1), pixelfx.FFX.Is(8), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(-5), pixelfx.ToeB.On())
	hammerSlam = hammerRest.With(pixelfx.X.Is(-1), pixelfx.Crouch.Is(7), pixelfx.Lean.Is(6), pixelfx.FX.Is(14), pixelfx.FY.Is(-19), pixelfx.SA.Is(0.62), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10))
)

// HammerSmash is a hammer that drops in front in a wedge of motion: a white slam with rays, a dome of force, a dent with cracks, rocks thrown up that bounce and settle, and dust. It is registered as "melee/hammer-smash".
var HammerSmash = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Hammer smash",
		Desc:     "A hammer drops in front in a wedge of motion: a white slam with rays, a dome of force, a dent with cracks, rocks thrown up that bounce and settle, and dust.",
		Tags:     []string{"melee", "smash", "hammer", "heavy", "ground", "shockwave", "debris", "combat", "directional", "one-shot"},
		Size:     [2]int{208, 112},
		Origin:   [2]int{80, 92},
		Duration: 2.0,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#141834", "#242c5c", "#38479a", "#5a72d0", "#8fa8f0", "#c4d4ff", "#eef3ff", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{66},
		Poses: []pixelfx.Key{
			{T: -0.72, Pose: hammerRest.With(pixelfx.X.Is(-3))},
			{T: -0.44, Pose: hammerRest.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-1), pixelfx.FX.Is(16), pixelfx.FY.Is(-28), pixelfx.SA.Is(-0.9))},
			{T: -0.2, Pose: hammerTop},
			{T: -0.03, Pose: hammerTop.With(pixelfx.Y.Is(-2))},
			{T: 0.06, Pose: hammerRest.With(pixelfx.X.Is(0), pixelfx.Y.Is(-3), pixelfx.Crouch.Is(-1), pixelfx.Lean.Is(2), pixelfx.FX.Is(13), pixelfx.FY.Is(-41), pixelfx.SA.Is(-1.2), pixelfx.FFX.Is(11), pixelfx.FFY.Is(-8), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.11, Pose: hammerRest.With(pixelfx.X.Is(0), pixelfx.Y.Is(-1), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(5), pixelfx.FX.Is(19), pixelfx.FY.Is(-30), pixelfx.SA.Is(-0.15), pixelfx.FFX.Is(13), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.15, Pose: hammerSlam, How: "snap"},
			{T: 0.62, Pose: hammerSlam.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(5))},
			{T: 1.02, Pose: hammerRest.With(pixelfx.X.Is(-2), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(2), pixelfx.FX.Is(14), pixelfx.FY.Is(-24), pixelfx.SA.Is(0.3))},
			{T: 1.4, Pose: hammerRest.With(pixelfx.X.Is(-3))},
		},
		Hits: []pixelfx.Hit{{T: 0.15, Stop: 0.2, Shake: 0.34, Amp: pixelfx.Val(3), Push: pixelfx.Val(3), Straw: pixelfx.Val(26), Y: pixelfx.Val(-8)}},
		Dust: []pixelfx.DustKick{{T: 0.15, X: 10, N: 8}, {T: 0.15, X: -12, N: 5, Dir: -1}},
	},
	New: newHammerSmash,
}

func init() { pixelfx.Register("melee/hammer-smash", HammerSmash) }

var (
	soot     = pixelfx.Hex("#0d0a12")
	hotRamp  = pixelfx.Ramp("#fff3a6", "#ffc23d", "#ff8a2a", "#c9281f")
	slamDrop = 0.15 // the hammer comes over and down, gathering speed
)

// hammerHead returns how far along its path the hammer head is at t, gathering speed.
func hammerHead(t float64) float64 {
	x := pixelfx.Clamp((t + 0.03) / (slamDrop + 0.03))
	return 0.28*x + 0.72*x*x
}

// pickBit returns one of list chosen by R, or nil when it is empty (drawing a number either way, as pixelfx's pick does).
func pickBit(R *pixelfx.Rng, list []pixelfx.Bit) *pixelfx.Bit {
	i := int(math.Floor(R.Next() * float64(len(list))))
	if i >= len(list) {
		return nil
	}
	return &list[i]
}

// hitFloor returns where on the floor (its x) a swing's rim reaches it, and how far along the swing that is.
func hitFloor(A *swing, r float64) (U1, x float64) {
	for {
		if _, y := onSwing(A, A.R, U1); !(y < -0.5 && U1 < 4) {
			break
		}
		U1 += 0.005
	}
	x, _ = onSwing(A, r, U1)
	return U1, x
}

// ray is a ray of light shooting up out of a slam: its angle and length.
type ray struct{ a, L float64 }

func newHammerSmash(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	n, st := len(C), stone(o.Color("stone"))
	R, ps, lights := newSim(o.Seed(), nil)
	bits, chips := pixelfx.NewBits(floor), pixelfx.NewBits(floor)
	// the hammer head's path: round the shoulders, from overhead down to the floor in front
	A := swing{cx: 5, cy: -25, R: 32, sy: 1, rot: 0, a0: -2.25, dir: 1}
	HW := 13.0
	U1, X := hitFloor(&A, A.R-HW/2)
	lines := cracks(R, X, 5, 24, 6, 0)
	rays := make([]ray, 0, 7)
	for i, d := range []float64{-0.95, -0.6, -0.3, -0.05, 0.22, 0.5, 0.85} {
		a := -pi/2 + d + R.Range(-0.06, 0.06)
		L := 12.0
		switch i {
		case 3:
			L = 34
		case 2, 4:
			L = 26
		case 1, 5:
			L = 18
		}
		rays = append(rays, ray{a: a, L: L * R.Range(0.9, 1.1)})
	}
	t, hit := 0.0, false

	// the smear: the hammer head's path as a comet, a hard white leading face, tapering to a hair behind it, cooling along the taper (edges first), with speed lines trailing along the arc
	Rm := A.R - HW/2
	wedge := func(p *pixelfx.Painter) {
		h := hammerHead(t)
		U, after := U1*h, pixelfx.Smooth(slamDrop, slamDrop+0.07, t)
		lagU := math.Max(0, U-(0.3+1.05*h))
		T := lagU + (U-lagU)*after*0.8
		if U-T < 0.03 || after >= 1 {
			return
		}
		length := (U - T) * Rm
		smear(p, &A, T, U, HW+1, func(u, r, _ float64, out *pixelfx.Out) {
			lead := (U - u) * Rm
			f, taper := lead/length, 1.0
			if lead >= 3 {
				taper = math.Pow(1-(lead-3)/math.Max(1, length-3), 0.85)
			}
			w := HW * taper * (1 - after*0.5)
			off := (r - Rm) / (w/2 + 0.01)
			if w < 0.6 || math.Abs(off) > 1 {
				return
			}
			ci := 0.0
			if lead >= 2 {
				ci = pixelfx.Round(1 + f*3.4 + math.Abs(off)*1.4 - either(off > 0.55, 1.0, 0) + after*3)
			}
			if ci > float64(n-3) {
				return
			}
			out.C = C[n-1-int(math.Max(0, ci))]
		}, 0, 1)
		// speed lines trail the head along the arc, dotted out at their ends
		if after < 0.6 {
			for _, sl := range [...][3]float64{{-HW/2 - 2, 3, 26}, {-HW/2 - 5, 9, 16}, {HW/2 + 2, 2, 30}, {HW/2 + 5, 8, 18}, {-1, length + 2, 14}, {3, length + 4, 9}} {
				dr, l0, L := sl[0], sl[1], sl[2]
				r := Rm + dr
				for d := l0; d < l0+L*(1-after); d += 0.5 {
					u, k := U-d/r, (d-l0)/L
					if u < 0 {
						break
					}
					x, y := onSwing(&A, r, u)
					c := C[4]
					if k < 0.25 {
						c = C[6]
					} else if k < 0.6 {
						c = C[5]
					}
					p.Dot(x, y, c, (1-k)*(1-after))
				}
			}
		}
		hx, hy := onSwing(&A, Rm, U)
		p.Glow(hx, hy, 34, C[4], 0.5*(1-pixelfx.Smooth(slamDrop-0.03, slamDrop, t)))
	}
	ringCols := []uint32{st.body, C[3], C[5], C[7]}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit && t >= slamDrop {
				hit = true
				lights.Add(X, -5, 16, pixelfx.White, 0.35, 0.08)
				lights.Add(X, -10, 64, C[4], 0.42, 0.6)
				// rocks thrown up out of the dent: they tumble, land, bounce and settle
				for range 15 {
					s := R.Sign()
					b := pixelfx.Bit{Kind: pixelfx.Chunk, G: 1, Drag: 0.35, Bounce: 0.38}
					b.X = X + s*R.Range(0, 5)
					b.Y = -2
					b.VX = s * R.Range(20, 105)
					b.VY = -R.Range(110, 300)
					b.Life = R.Range(1.25, 1.65)
					b.A = R.Range(0, pixelfx.Tau)
					b.VA = R.Range(-16, 16)
					b.S = pixelfx.Pick(R, []float64{1, 1, 2, 2, 3})
					bits.Add(b)
				}
				// hot chips struck off the stone: they bounce, settle and cool through the ramp
				for range 11 {
					a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(90, 230)
					b := pixelfx.Bit{Kind: pixelfx.Shard, Y: -2, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 0.9, Drag: 0.5, Bounce: 0.35, S: 1}
					b.X = X + R.Range(-4, 4)
					b.Life = R.Range(0.9, 1.5)
					chips.Add(b)
				}
				for range 40 {
					a, sp := -pi/2+R.Range(-1.1, 1.1), R.Range(50, 210)
					ps.Spawn(pixelfx.Grit, X+R.Range(-4, 4), -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.9, 1.5), pixelfx.Pick(R, []uint32{st.light, st.body, st.body, st.shade}), either(R.Chance(0.4), 2.0, 1), 0, 0)
				}
				// force motes skid out along the floor, sparks off the stone
				for i := range 14 {
					s, sp := either(i%2 != 0, -1.0, 1), R.Range(120, 260)
					ps.Spawn(pixelfx.Mote, X+s*6, -R.Range(1, 4), s*sp, -R.Range(0, 30), R.Range(0.18, 0.34), either(R.Chance(0.5), C[6], C[5]), 0.8, R.Range(0.02, 0.05), 0)
				}
				for range 24 {
					a, sp := -pi/2+R.Range(-1.4, 1.4), R.Range(150, 380)
					ps.Spawn(pixelfx.Spark, X+math.Cos(a)*6, -2+math.Sin(a)*4, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.12, 0.42), 0, 1, R.Range(0.035, 0.08), 0)
				}
				// dust rolls out both ways along the floor, and a puff lifts off the dent
				for i := range 20 {
					s := either(i%2 != 0, -1.0, 1)
					ps.Spawn(pixelfx.Smoke, X+s*R.Range(2, 10), -R.Range(2, 7), s*R.Range(40, 125), -R.Range(2, 14), R.Range(0.95, 1.5), st.dust, R.Range(6, 9), R.Range(0.02, 0.08), pixelfx.Big)
				}
				for range 7 {
					ps.Spawn(pixelfx.Smoke, X+R.Range(-6, 6), -R.Range(3, 9), R.Range(-20, 20), -R.Range(30, 80), R.Range(1.0, 1.4), st.dust, R.Range(5, 7.5), R.Range(0.04, 0.14), pixelfx.Big)
				}
				for range 8 {
					ps.Spawn(pixelfx.Dust, X+R.Range(-6, 6), -R.Range(1, 4), R.Range(-70, 70), -R.Range(8, 30), R.Range(0.35, 0.7), st.dust, R.Range(2, 3.5), 0, 0)
				}
			}
			bits.Update(dt)
			chips.Update(dt)
			// settled rocks catch the light now and then
			if hit && t > slamDrop+0.4 && t < slamDrop+1.2 && R.Chance(0.08) {
				if b := pickBit(R, bits.List); b != nil && b.Rest {
					ps.Spawn(pixelfx.StarKind, b.X+R.Range(-1, 1), b.Y-1.5-b.S, 0, -2, R.Range(0.25, 0.45), either(R.Chance(0.5), hotRamp[0], pixelfx.White), R.Range(0.6, 1.6), 0, 0)
				}
			}
			// rocks that land kick up a little dust
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 3 {
						ps.Spawn(pixelfx.Dust, b.X+R.Range(-1.5, 1.5), -0.5, R.Sign()*R.Range(8, 35), -R.Range(2, 14), R.Range(0.3, 0.6), st.dust, R.Range(1.3, 2.6), 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if q := t - slamDrop; q >= 0 {
				// the dent: the floor's top knocked down into a shallow pit, its surface lit at the bottom, chipped lips
				fade := 1 - pixelfx.Smooth(1.3, 1.7, q)
				for x := -11.0; x <= 11; x++ {
					u, d := x/10, 0.0
					if math.Abs(x) <= 10 {
						d = pixelfx.Round(3.2*(1-u*u) + (pixelfx.Rand(x, 4)-0.5)*0.8)
					}
					for y := 0.0; y < d; y++ {
						p.Dot(X+x, y, either(y == 0, soot, st.dark), fade)
					}
					if d > 0 {
						p.Dot(X+x, d, either(math.Abs(x) < 7, st.light, st.body), fade)
					}
					if math.Abs(x) >= 8 {
						p.Dot(X+x, -1, either(math.Abs(x) >= 10, st.body, st.light), fade)
					}
					if math.Abs(x) == 9 && pixelfx.Rand(x, 7) < 0.7 {
						p.Dot(X+x, -2, st.body, fade)
					}
				}
				// the floor ring's far half
				if k := q / 0.5; k < 1 {
					pixelfx.Band(p, X, 0.5, 6+50*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 2*(1-k)), 1.15-k, ringCols, func(g float64) bool { return math.Sin(g) < 0 }, false)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			q := t - slamDrop
			wedge(p)
			if q >= 0 {
				// cracks split out of the dent: a white flash, then dark lines that fade with the dent
				fade := 1 - pixelfx.Smooth(1.3, 1.7, q)
				for _, c := range lines {
					reach := 30 * pixelfx.Ease.OutCubic(pixelfx.Clamp((q-c.at*0.06)/0.08)) * either(c.fork, 0.3, 1)
					if reach > 0 {
						along(c.pts, reach, func(x0, y0, x1, y1, _ float64) {
							if q >= 0.12 {
								p.Line(x0, y0+3, x1, y1+3, st.shade, fade)
							}
							col := soot
							if q < 0.05 {
								col = pixelfx.White
							} else if q < 0.12 {
								col = C[5]
							}
							p.Line(x0, y0+2, x1, y1+2, col, fade)
						})
					}
				}
				// the near half of the floor ring
				if k := q / 0.5; k < 1 {
					pixelfx.Band(p, X, 0.5, 6+50*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 2*(1-k)), 1.15-k, ringCols, func(g float64) bool { return math.Sin(g) >= 0 }, false)
				}
				// the dome of force swells off the floor, thins to dots and fades
				if d := (q - 0.045) / 0.4; d >= 0 && d < 1 {
					// solid steps through the shell (white rim, two blues inside); it dissolves as a whole at the end
					r, th, a := 12+30*pixelfx.Ease.OutCubic(d), math.Max(1, 3.6*math.Pow(1-d, 0.8)), 1.0
					if d >= 0.6 {
						a = (1 - d) / 0.4
					}
					p.Shade(X-r-1, -r-1, X+r+1, 0, func(x, y float64, out *pixelfx.Out) uint32 {
						e := r - pixelfx.Hypot(x-X, y)
						if e < 0 || e > th || y > -0.5 {
							return 0
						}
						v := e / th
						switch {
						case d < 0.1:
							out.C = either(v < 0.6, pixelfx.White, C[6])
						case v < 0.34:
							out.C = either(d < 0.45, pixelfx.White, C[6])
						case v < 0.67:
							out.C = C[5]
						default:
							out.C = C[4]
						}
						out.A = a
						return 0
					})
					for i := range 7 {
						g := -pi * (0.08 + (float64(i)/6)*0.84)
						p.Glow(X+math.Cos(g)*r, math.Sin(g)*r, 14, C[4], 0.36*(1-d))
					}
				}
				// rays of light shoot up out of the slam
				if q < 0.2 {
					for _, ry := range rays {
						d1, d0 := ry.L*pixelfx.Ease.OutCubic(math.Min(1, q/0.05)), ry.L*pixelfx.Smooth(0.04, 0.2, q)
						ca, sa := math.Cos(ry.a), math.Sin(ry.a)
						if d1-d0 < 1 {
							continue
						}
						p.LineFn(X+ca*(2+d0), -1+sa*(2+d0), X+ca*d1, -1+sa*d1, func(u float64) (uint32, float64) {
							switch {
							case u < 0.5:
								return pixelfx.White, 1
							case u < 0.8:
								return C[6], 1
							}
							return C[5], 1
						})
						p.GlowLine(X+ca*d0, -1+sa*d0, X+ca*d1, -1+sa*d1, 4, C[5], 0.18*(1-q/0.2))
					}
				}
				// the white slam: a flat flash on the floor for two frames
				if q < 0.07 {
					hot := q < 0.035
					rx, ry := either(hot, 16.0, 12), either(hot, 3.0, 2)
					p.Ellipse(X, 0, rx+2, ry+1, either(hot, C[6], C[5]), 1)
					p.Ellipse(X, 0, rx, ry, either(hot, pixelfx.White, C[7]), 1)
				}
				if q < 0.2 {
					pop := math.Exp(-q / 0.05)
					p.Glow(X, -5, 26, C[5], 0.3*pop)
				}
				// the struck stone stays warm for a moment
				p.Glow(X, -1, 16, hotRamp[2], 0.28*math.Exp(-q/0.35))
			}
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			for _, b := range chips.List {
				k := b.Life / b.Max
				c := hotRamp[3]
				switch {
				case k > 0.7:
					c = hotRamp[0]
				case k > 0.45:
					c = hotRamp[1]
				case k > 0.25:
					c = hotRamp[2]
				}
				p.Dot(b.X, b.Y, c, math.Min(1, k*4))
				if !b.Rest {
					p.Dot(b.X-b.VX*0.012, b.Y-b.VY*0.012, hotRamp[3], math.Min(1, k*4))
				}
				p.Glow(b.X, b.Y, 4, c, 0.35*k)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
