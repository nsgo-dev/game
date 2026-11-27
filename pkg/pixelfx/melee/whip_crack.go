// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/whip-crack.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx's pop star and slivers follow Sprite Fusion's open-source impact, and the sparks his slash (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	whipStance = pixelfx.Ready.With(pixelfx.X.Is(-7), pixelfx.SA.Is(-0.3), pixelfx.FX.Is(13), pixelfx.FY.Is(-21), pixelfx.BX.Is(7), pixelfx.BY.Is(-18)).Hold("handle")
	whipCock   = whipStance.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-3), pixelfx.FX.Is(-6), pixelfx.FY.Is(-38), pixelfx.SA.Is(-2.2), pixelfx.BX.Is(9), pixelfx.BY.Is(-23), pixelfx.ArmsFront.On(), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))
	whipCrack  = whipStance.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(5), pixelfx.FX.Is(18), pixelfx.FY.Is(-21), pixelfx.SA.Is(0), pixelfx.BX.Is(-8), pixelfx.BY.Is(-23), pixelfx.FFX.Is(12), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// WhipCrack is a whip that rolls out to the right as a loop running down its length, faster and faster, until the tip flips over and cracks: a white pop, a ring of air, sparks and a puff of smoke. It is registered as "melee/whip-crack".
var WhipCrack = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Whip crack",
		Desc:     "A whip rolls out to the right as a loop running down its length, faster and faster, until the tip flips over and cracks: a white pop, a ring of air, sparks and a puff of smoke.",
		Tags:     []string{"melee", "whip", "crack", "lash", "combat", "directional", "one-shot"},
		Size:     [2]int{192, 80},
		Origin:   [2]int{30, 68},
		Duration: 0.85,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1a10", "#5c3a1e", "#9c6a34", "#d8a45a", "#ffd890", "#fff4d6", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "length", Default: pixelfx.Num(76)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{94},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: whipStance},
			{T: -0.22, Pose: whipCock},
			{T: 0.02, Pose: whipCock.With(pixelfx.Lean.Is(-4))},
			{T: 0.06, Pose: whipCrack.With(pixelfx.FX.Is(15), pixelfx.FY.Is(-34), pixelfx.SA.Is(-0.7), pixelfx.Lean.Is(2), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.12, Pose: whipCrack, How: "snap"},
			{T: 0.21, Pose: whipCrack},
			{T: 0.26, Pose: whipCrack.With(pixelfx.FX.Is(15), pixelfx.FY.Is(-27), pixelfx.SA.Is(-0.3), pixelfx.Lean.Is(2), pixelfx.Crouch.Is(3)), How: "snap"},
			{T: 0.44, Pose: whipCrack.With(pixelfx.FX.Is(17), pixelfx.FY.Is(-22), pixelfx.SA.Is(0.1), pixelfx.Lean.Is(3), pixelfx.Crouch.Is(3))},
			{T: 0.92, Pose: whipStance},
		},
		Hits: []pixelfx.Hit{{T: 0.21, Stop: 0.1, Shake: 0.14, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.4), Straw: pixelfx.Val(14), Y: pixelfx.Val(-28)}},
		Dust: []pixelfx.DustKick{{T: 0.06, X: 2, N: 4, Dir: 1}},
	},
	New: newWhipCrack,
}

func init() { pixelfx.Register("melee/whip-crack", WhipCrack) }

var (
	// the loop runs out from whipRoll0 over whipRoll; the tip cracks when it gets there
	whipRoll0, whipRoll = 0.02, 0.19
	whipCrackAt         = whipRoll0 + whipRoll
	whipRecoil          = 0.16
)

// tipAt is where the whip's tip was at time t.
type tipAt struct {
	p [2]float64
	t float64
}

func newWhipCrack(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	L, HX, HY := sizeOf(o, "length"), 11.0, -21.0
	TX, TY := HX+L, HY-1
	// how far down the whip the loop has run: it starts a third of the way out, cocked, and accelerates
	lam := func(t float64) float64 {
		return L * (0.34 + 0.66*pixelfx.Ease.InQuad(pixelfx.Clamp((t-whipRoll0)/whipRoll)))
	}
	recoil0, recoil1 := whipCrackAt+0.04, whipCrackAt+0.04+whipRecoil
	// the whip as points, handle to tip: straight out to the loop, round the loop, folded back under it
	shape := func(t float64) [][2]float64 {
		l := lam(t)
		r := 6.5 - 3.5*(l/L)
		recoil := pixelfx.Smooth(recoil0, recoil1, t)
		length := L * (1 - recoil*0.85)
		var pts [][2]float64
		for s := 0.0; s <= length; s++ {
			var x, y float64
			switch {
			case s <= l:
				x, y = HX+s, HY-math.Sin((pi*s)/math.Max(l, 1))*2.5*(1-l/L)
			case s <= l+pi*r:
				th := (s - l) / r
				x, y = HX+l+math.Sin(th)*r, HY+r-math.Cos(th)*r
			default:
				x, y = HX+l-(s-l-pi*r), HY+2*r
			}
			// after the crack it sags as it's pulled back in
			if recoil > 0 {
				y += math.Sin((pi*s)/L) * 6 * recoil
			}
			pts = append(pts, [2]float64{x, y})
		}
		return pts
	}
	spikes := make([]spike, 0, 8)
	for i, a := range []float64{0, 0.8, 1.6, 2.4, 3.14, 3.9, 4.7, 5.5} {
		var s spike
		s.a = a + Rg.Range(-0.15, 0.15)
		l := 9.0
		if i == 0 {
			l = 13
		} else if i%2 != 0 {
			l = 6
		}
		s.len = l + Rg.Range(0, 3)
		s.w = 1.4 + Rg.Range(0, 0.5)
		spikes = append(spikes, s)
	}
	slivers := newSlivers(Rg, []float64{-1.2, -0.3, 0.5, 1.3}, 4, 6, 80, 120)
	t, cracked := 0.0, false
	var tipTrail []tipAt
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t > whipCrackAt-0.05 && t < whipCrackAt+0.01 {
				pts := shape(t)
				tipTrail = append(tipTrail, tipAt{p: pts[len(pts)-1], t: t})
			}
			for i := len(tipTrail) - 1; i >= 0; i-- {
				if t-tipTrail[i].t > 0.06 {
					tipTrail = append(tipTrail[:i], tipTrail[i+1:]...)
				}
			}
			if !cracked && t >= whipCrackAt {
				cracked = true
				lights.Add(TX, TY, 46, C[4], 0.6, 0.3)
				lights.Add(TX, TY, 14, pixelfx.White, 0.8, 0.07)
				for range 16 {
					a, sp := Rg.Range(-1.3, 1.2), Rg.Range(130, 300)
					ps.Spawn(pixelfx.Spark, TX, TY, math.Cos(a)*sp, math.Sin(a)*sp-40, Rg.Range(0.12, 0.36), pixelfx.White, 1, 0, 0)
				}
				for range 3 {
					ps.Spawn(pixelfx.Smoke, TX+Rg.Range(-2, 3), TY+Rg.Range(-2, 2), Rg.Range(10, 40), -Rg.Range(6, 18), Rg.Range(0.5, 0.8), C[2], Rg.Range(2.5, 4), Rg.Range(0.02, 0.06), 0)
				}
				for range 4 {
					ps.Spawn(pixelfx.StarKind, TX+Rg.Range(-8, 10), TY+Rg.Range(-8, 8), 0, -3, Rg.Range(0.2, 0.4), either(Rg.Chance(0.5), C[5], pixelfx.White), Rg.Range(0.8, 1.8), Rg.Range(0.05, 0.2), 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < recoil1 {
				fade := 1 - pixelfx.Smooth(recoil1-0.06, recoil1, t)
				// afterimages of the whip a beat and two beats back: the loop's motion
				if t < whipCrackAt {
					for _, ai := range [...]struct {
						lag float64
						c   uint32
						a   float64
					}{{0.03, C[1], 0.5}, {0.015, C[2], 0.7}} {
						old := shape(math.Max(0, t-ai.lag))
						for i := 1; i < len(old); i += 2 {
							p.Dot(old[i][0], old[i][1], ai.c, ai.a)
						}
					}
				}
				// the whip: thicker and lit near the handle, a single line to the tip
				pts := shape(t)
				n, l := len(pts), lam(t)
				r := 6.5 - 3.5*(l/L)
				for i := 1; i < n; i++ {
					x0, y0, x1, y1 := pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1]
					u, s := float64(i)/float64(n), float64(i)
					// the loop's leading half is moving fastest: it runs hot
					inLoop := t < whipCrackAt && s > l && s < l+pi*r
					lead := inLoop && math.Abs(s-l-(pi*r)/2) < r*0.9
					c := C[3]
					switch {
					case lead:
						c = pixelfx.White
					case inLoop:
						c = C[5]
					case u < 0.5:
						c = C[4]
					}
					p.Line(x0, y0, x1, y1, c, fade)
					if u < 0.4 {
						p.Line(x0, y0+1, x1, y1+1, C[2], fade)
					}
				}
				// the running loop is the fastest part: it shines
				if t < whipCrackAt {
					p.Glow(HX+l+r, HY+r, 12, C[4], 0.4)
				}
			}
			// the tip's path just before the crack: a white-hot streak
			for i := 1; i < len(tipTrail); i++ {
				k := 1 - (t-tipTrail[i].t)/0.06
				a, b := tipTrail[i-1].p, tipTrail[i].p
				p.Line(a[0], a[1], b[0], b[1], either(k > 0.6, pixelfx.White, C[5]), k)
			}
			// the crack: a white pop, a ring of air, slivers
			q := t - whipCrackAt
			if q >= 0 && q < 0.035 {
				star(p, TX, TY, spikes, 1, 1, pixelfx.White, C[5], C[6], C[4])
			} else if q >= 0.035 && q < 0.07 {
				star(p, TX, TY, spikes, 1.3, 0.5, C[6], C[5], C[5], C[3])
			}
			if q >= 0.02 && q < 0.2 {
				k := (q - 0.02) / 0.18
				r, th, col := 4+16*pixelfx.Ease.OutCubic(k), math.Max(1, 2.2*(1-k)), ringCol(C, k)
				for a := 0.0; a < pixelfx.Tau; a += 0.6 / r {
					for o := 0.0; o < th; o += 0.5 {
						p.Dot(TX+math.Cos(a)*(r-o), TY+math.Sin(a)*(r-o)*0.8, col, 1.15-k)
					}
				}
				p.Glow(TX, TY, r+6, C[4], 0.25*(1-k))
			}
			if q >= 0.04 && q < 0.16 {
				k := (q - 0.04) / 0.12
				for _, s := range slivers {
					sliver(p, TX, TY, s.a, s.r+s.v*(q-0.04), s.arc*(1-0.4*k), either(k < 0.4, C[6], C[5]), 1-k)
				}
			}
			if q >= 0 && q < 0.15 {
				p.Glow(TX, TY, 26, C[5], 0.7*math.Exp(-q/0.04))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
