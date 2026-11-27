// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/scythe-reap.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the crescent and trace recipe from Sprite Fusion's open-source slash, and the vapour from his ice and poison (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	// the butt rests on the floor in one hand; both hands take it out to the right, and he turns with it round the front
	scytheRest = pixelfx.Ready.With(pixelfx.X.Is(2), pixelfx.SA.Is(-1.5), pixelfx.FX.Is(11), pixelfx.FY.Is(-22), pixelfx.BX.Is(7), pixelfx.BY.Is(-16)).Hold("scythe")
	scytheOut  = scytheRest.With(pixelfx.Two.On(), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(4), pixelfx.FX.Is(17), pixelfx.FY.Is(-27), pixelfx.SA.Is(-0.45), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-8))
	scytheEnd  = scytheRest.With(pixelfx.Two.On(), pixelfx.X.Is(0), pixelfx.Yaw.Is(3.14), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(4), pixelfx.FX.Is(15), pixelfx.FY.Is(-24), pixelfx.SA.Is(0.3), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10), pixelfx.ToeB.On())
)

// ScytheReap is a slow, huge dark crescent that reaps round the front with a pale ghostly edge; it hangs, then tears away from the tail into curling wisps and cold vapour. It is registered as "melee/scythe-reap".
var ScytheReap = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Scythe reap",
		Desc:     "A slow, huge dark crescent reaps round the front with a pale ghostly edge; it hangs, then tears away from the tail into curling wisps and cold vapour.",
		Tags:     []string{"melee", "scythe", "reap", "dark", "ghost", "death", "slash", "combat", "one-shot"},
		Size:     [2]int{184, 116},
		Origin:   [2]int{84, 98},
		Duration: 2.1,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#07040c", "#140a22", "#24113a", "#3b1d58", "#5a3a82", "#7fd6c0", "#d8fff2", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(13)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{66},
		Poses: []pixelfx.Key{
			{T: -0.56, Pose: scytheRest},
			{T: -0.26, Pose: scytheOut},
			{T: 0.08, Pose: scytheOut.With(pixelfx.Crouch.Is(5))},
			{T: 0.2, Pose: scytheOut.With(pixelfx.Yaw.Is(0.8), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3), pixelfx.FX.Is(16), pixelfx.FY.Is(-23), pixelfx.SA.Is(0.1), pixelfx.Ghost.Is(1))},
			{T: 0.3, Pose: scytheOut.With(pixelfx.X.Is(1), pixelfx.Yaw.Is(1.9), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(3), pixelfx.FX.Is(15), pixelfx.FY.Is(-23), pixelfx.SA.Is(0.2), pixelfx.Ghost.Is(1))},
			{T: 0.42, Pose: scytheEnd},
			{T: 0.92, Pose: scytheEnd.With(pixelfx.Crouch.Is(5))},
			{T: 1.3, Pose: scytheRest.With(pixelfx.X.Is(1), pixelfx.Yaw.Is(1.4), pixelfx.Crouch.Is(4))},
			{T: 1.7, Pose: scytheRest},
		},
		Hits: []pixelfx.Hit{{T: 0.14, Stop: 0.12, Shake: 0.16, Amp: pixelfx.Val(2), Push: pixelfx.Val(2.4), Straw: pixelfx.Val(20), Skin: "shadow"}},
		Dust: []pixelfx.DustKick{{T: 0.3, X: 2, N: 6}},
	},
	New: newScytheReap,
}

func init() { pixelfx.Register("melee/scythe-reap", ScytheReap) }

var (
	scytheWind, scytheSweep, scytheHang, scytheTear = 0.08, 0.34, 0.14, 0.55
	scytheT0                                        = scytheWind
	scytheT1                                        = scytheWind + scytheSweep
	scytheT2                                        = scytheT1 + scytheHang
	scytheT3                                        = scytheT2 + scytheTear
)

// scytheHead returns the fraction of its span the blade has swept at t.
func scytheHead(t float64) float64 {
	return pixelfx.Ease.InOutSine(pixelfx.Clamp((t - scytheT0) / scytheSweep))
}

// wisp is a wisp torn off the crescent: where it is, how it moves, how long it lives and has lived, its sway, the thread it leaves and whether it is behind the character.
type wisp struct {
	x, y, vx, vy, life, max, seed float64
	pts                           [][2]float64
	far                           bool
}

func newScytheReap(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	Rg := pixelfx.NewRng(o.Seed())
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: Rg, Ground: floor, Haze: C[5]}), &pixelfx.Lights{}
	// a wide reap from out on the right, round the front of the fighter, to the left
	A := swing{cx: 8, cy: -33, R: 52, sy: 0.6, k: 0.1, rot: 0.08, a0: -0.5, dir: 1, span: 3.5}
	W := 21.0
	var wisps []wisp
	t, lastTear := 0.0, 0.0
	// how far the tear has eaten in from the tail (0..1 of the span), ragged by a per-pixel threshold
	tear := func() float64 { return pixelfx.Smooth(scytheT2, scytheT3, t) }
	addWisp := func(x, y float64, far bool) {
		w := wisp{x: x, y: y, far: far}
		w.vx = Rg.Range(-12, 12)
		w.vy = -Rg.Range(8, 26)
		w.life = Rg.Range(0.5, 0.9)
		w.seed = Rg.Range(0, 100)
		wisps = append(wisps, w)
	}
	dim := []uint32{C[0], C[0], C[1], C[2], C[3], C[4], C[5], C[6]}

	blade := func(p *pixelfx.Painter, half float64) {
		U, k := A.span*scytheHead(t), tear()
		if U < 0.02 || k >= 1 {
			return
		}
		ramp := either(half < 0, dim, C)
		smear(p, &A, 0, U, W, func(u, r, _ float64, out *pixelfx.Out) {
			v := u / U
			w := W * math.Pow(math.Sin(pi*math.Pow(v, 1.7)), 0.55)
			if w < 0.5 {
				return
			}
			q := (A.R - r) / w
			if q < 0 || q > 1 {
				return
			}
			// torn away from the tail: a ragged front of per-pixel thresholds
			n := pixelfx.Rand(math.Floor(u*40), math.Floor(r), 7)
			if k > 0 && (u/A.span) < k*1.2-n*0.2 {
				return
			}
			// a pale ghostly rim, a dark body that deepens inward, a faint lit seam
			ci := 2
			switch {
			case q < 0.08:
				ci = 7
			case q < 0.16:
				ci = 6
			case q < 0.25:
				ci = 5
			case q < 0.4:
				ci = 4
			case q < 0.62:
				ci = 3
			}
			if t > scytheT1 && q < 0.2 {
				ci--
			}
			out.C = ramp[ci]
			out.A = math.Min(1, v/0.25)
		}, half, 1)
		glowAlong(p, &A, A.R-2, math.Max(0, k*1.2*A.span-0.3), U, 14, C[5], func(u float64) float64 { return 0.3 * (u / U) }, half, 0.25)
	}

	threads := func(p *pixelfx.Painter, back bool) {
		for _, w := range wisps {
			if w.far != back {
				continue
			}
			life := 1 - w.max/w.life
			for i := 1; i < len(w.pts); i++ {
				f := float64(i) / float64(len(w.pts))
				c := C[3]
				if f > 0.75 {
					c = C[6]
				} else if f > 0.4 {
					c = C[5]
				}
				p.Line(w.pts[i-1][0], w.pts[i-1][1], w.pts[i][0], w.pts[i][1], c, life*(0.3+0.7*f))
			}
			if life > 0.5 {
				p.Glow(w.x, w.y, 5, C[5], 0.25*life)
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the cold gathers on the blade before it moves
			if t < scytheWind && Rg.Chance(0.5) {
				x, y := onSwing(&A, A.R-4, 0)
				ps.Spawn(pixelfx.Vapour, x+Rg.Range(-14, 14), y+Rg.Range(-8, 8), Rg.Range(-10, 10), Rg.Range(-6, 6), Rg.Range(0.3, 0.5), C[5], Rg.Range(1.5, 2.5), 0, 0)
			}
			// the edge sheds a little vapour as it goes
			if t > scytheT0 && t < scytheT1 && Rg.Chance(0.5) {
				tr := travel(&A, A.span*scytheHead(t)*Rg.Range(0.75, 1))
				ps.Spawn(pixelfx.Vapour, tr.x, tr.y, tr.dx*20, -Rg.Range(4, 14), Rg.Range(0.5, 0.9), C[6], Rg.Range(2, 3.5), 0, backIf(math.Sin(tr.a) < 0))
			}
			// tearing away: wisps peel off where the crescent is coming apart
			k := tear()
			for u := lastTear; u < k*1.15; u += 0.022 {
				uu := math.Min(1, u) * A.span
				S := A
				S.R = A.R - Rg.Range(0, 8)
				tr := travel(&S, uu)
				far := math.Sin(tr.a) < 0
				addWisp(tr.x, tr.y, far)
				if Rg.Chance(0.5) {
					ps.Spawn(pixelfx.Vapour, tr.x, tr.y, Rg.Range(-10, 10), -Rg.Range(6, 18), Rg.Range(0.55, 0.85), either(Rg.Chance(0.5), C[5], C[4]), Rg.Range(2.5, 4.5), 0, backIf(far))
				}
				if Rg.Chance(0.2) {
					ps.Spawn(pixelfx.StarKind, tr.x+Rg.Range(-3, 3), tr.y+Rg.Range(-3, 3), 0, -6, Rg.Range(0.25, 0.5), either(Rg.Chance(0.5), C[6], pixelfx.White), Rg.Range(0.6, 1.6), 0, backIf(far))
				}
			}
			lastTear = math.Max(lastTear, k*1.15)
			// wisps rise, sway and curl, leaving a fading thread behind them
			for i := len(wisps) - 1; i >= 0; i-- {
				w := &wisps[i]
				w.max += dt
				if w.max > w.life {
					wisps = append(wisps[:i], wisps[i+1:]...)
					continue
				}
				w.vy -= 30 * dt
				w.vx += math.Sin(w.max*9+w.seed) * 120 * dt
				w.vx -= w.vx * 2 * dt
				w.vy -= w.vy * 1.2 * dt
				w.x += w.vx * dt
				w.y += w.vy * dt
				w.pts = append(w.pts, [2]float64{w.x, w.y})
				if len(w.pts) > 14 {
					w.pts = w.pts[1:]
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			blade(p, -1)
			ps.Draw(p, pixelfx.LayerBack)
			threads(p, true)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			blade(p, 1)
			// the blade's point glints cold as it sets off and where it stops
			if t < scytheWind+0.05 {
				x, y := onSwing(&A, A.R-3, 0)
				pixelfx.Sparkle(p, x, y, 6*math.Min(1, t/0.04)*(1-pixelfx.Smooth(scytheWind-0.02, scytheWind+0.05, t)), C[6], C[5], 0.45)
			}
			if g := t - scytheT1 + 0.06; g > 0 && g < 0.3 {
				x, y := onSwing(&A, A.R-3, A.span)
				pixelfx.Sparkle(p, x, y, 9*pixelfx.Ease.OutBack(math.Min(1, g/0.06))*(1-g/0.3), C[6], C[5], 0.55)
			}
			ps.Draw(p, pixelfx.LayerFront)
			threads(p, false)
			lights.Draw(p)
		},
	}
}
