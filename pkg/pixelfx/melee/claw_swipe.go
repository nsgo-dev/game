// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/melee/claw-swipe.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx's shaded smear, its light and the dotted trace follow Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package melee

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	clawReady = pixelfx.Ready.With(pixelfx.SA.Is(-0.4), pixelfx.SB.Is(-0.9), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3), pixelfx.FX.Is(14), pixelfx.FY.Is(-21), pixelfx.BX.Is(8), pixelfx.BY.Is(-25), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-8)).Hold("claw").HoldOff("claw")
	clawHigh  = clawReady.With(pixelfx.X.Is(-6), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-3), pixelfx.FX.Is(3), pixelfx.FY.Is(-41), pixelfx.SA.Is(-1.4), pixelfx.BX.Is(7), pixelfx.BY.Is(-18), pixelfx.SB.Is(-0.3), pixelfx.FFX.Is(10))
)

// ClawSwipe is three claw marks that rake down one after another, white-hot, cooling through orange to red, then close up from both ends and leave a dotted scar, with hot flecks and drips. It is registered as "melee/claw-swipe".
var ClawSwipe = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Claw swipe",
		Desc:     "Three claw marks rake down one after another, white-hot, cooling through orange to red, then close up from both ends and leave a dotted scar, with hot flecks and drips.",
		Tags:     []string{"melee", "claw", "swipe", "beast", "scratch", "combat", "directional", "one-shot"},
		Size:     [2]int{128, 96},
		Origin:   [2]int{36, 84},
		Duration: 0.9,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a0808", "#5c0f12", "#a51d1d", "#e2401f", "#ff8a2a", "#ffd166", "#fff3c4", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3c4", "#ffd166", "#ff8a2a", "#c42a1a")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{58},
		Poses: []pixelfx.Key{
			{T: -0.46, Pose: clawReady.With(pixelfx.X.Is(-6))},
			{T: -0.2, Pose: clawHigh},
			{T: -0.05, Pose: clawHigh.With(pixelfx.Crouch.Is(5))},
			{T: 0, Pose: clawReady.With(pixelfx.X.Is(0), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(4), pixelfx.FX.Is(19), pixelfx.FY.Is(-36), pixelfx.SA.Is(-0.9), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.Ghost.Is(1)), How: "snap"},
			{T: 0.05, Pose: clawReady.With(pixelfx.X.Is(2), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(6), pixelfx.FX.Is(23), pixelfx.FY.Is(-24), pixelfx.SA.Is(0.5), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On()), How: "snap"},
			{T: 0.1, Pose: clawReady.With(pixelfx.X.Is(3), pixelfx.Crouch.Is(7), pixelfx.Lean.Is(7), pixelfx.FX.Is(22), pixelfx.FY.Is(-12), pixelfx.SA.Is(1.2), pixelfx.BX.Is(-7), pixelfx.BY.Is(-24), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10), pixelfx.ToeB.On()), How: "snap"},
			{T: 0.42, Pose: clawReady.With(pixelfx.X.Is(3), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(5), pixelfx.FX.Is(21), pixelfx.FY.Is(-13), pixelfx.SA.Is(1.1), pixelfx.BX.Is(-6), pixelfx.BY.Is(-22), pixelfx.FFX.Is(13), pixelfx.FBX.Is(-10))},
			{T: 0.9, Pose: clawReady.With(pixelfx.X.Is(-6))},
		},
		Hits: []pixelfx.Hit{
			{T: 0.04, Stop: 0.03, Push: pixelfx.Val(0.9), Straw: pixelfx.Val(5), Y: pixelfx.Val(-30), Flash: pixelfx.Val(0.03)},
			{T: 0.08, Stop: 0.03, Push: pixelfx.Val(0.9), Straw: pixelfx.Val(5), Y: pixelfx.Val(-26), Flash: pixelfx.Val(0.03)},
			{T: 0.12, Stop: 0.08, Shake: 0.14, Amp: pixelfx.Val(2), Push: pixelfx.Val(1.4), Straw: pixelfx.Val(9), Y: pixelfx.Val(-18)},
		},
		Dust: []pixelfx.DustKick{{T: 0.1, X: 12, N: 6, Dir: 1}},
	},
	New: newClawSwipe,
}

func init() { pixelfx.Register("melee/claw-swipe", ClawSwipe) }

var (
	// clawRake is how long each mark takes to tear open, clawStagger the gap between marks
	clawRake, clawStagger = 0.05, 0.04
	// then each closes up from both ends, from clawClose0 over clawClose
	clawClose0, clawClose = 0.16, 0.3
)

// clawMark is one claw mark: its swing, when it starts, how wide it is, whether it has torn open, and where it ends.
type clawMark struct {
	s    swing
	t0   float64
	W    float64
	open bool
	end  travelAt
}

func newClawSwipe(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	n := len(C)
	Rg, ps, lights := newSim(o.Seed(), o.Ramp("sparks"))
	drips := pixelfx.NewBits(floor)
	// three marks raking down and back, each a little lower and further out than the last
	marks := make([]clawMark, 3)
	for i := range marks {
		fi := float64(i)
		s := through(31+fi*8, -23+fi*2, 1.84, 1, 40, 0.84-fi*0.05, 0.5)
		marks[i] = clawMark{s: s, t0: fi * clawStagger, W: 4.2 - fi*0.4, end: travel(&s, s.span)}
	}
	t := 0.0
	heatAt := func(m *clawMark) float64 { return pixelfx.Clamp(1 - (t-m.t0)/0.42) }
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range marks {
				m := &marks[i]
				if m.open || t < m.t0+clawRake*0.7 {
					continue
				}
				m.open = true
				lights.Add(m.end.x, m.end.y-6, 34, C[4], 0.45, 0.3)
				// hot flecks thrown on along the rake, and drips running off the wound
				for range 7 {
					a, sp := math.Atan2(m.end.dy, m.end.dx)+Rg.Range(-0.5, 0.5), Rg.Range(110, 260)
					ps.Spawn(pixelfx.Spark, m.end.x, m.end.y, math.Cos(a)*sp, math.Sin(a)*sp, Rg.Range(0.12, 0.32), pixelfx.White, 1, 0, 0)
				}
				for range 5 {
					x, y := onSwing(&m.s, m.s.R, m.s.span*Rg.Range(0.15, 0.9))
					ps.Spawn(pixelfx.Mote, x, y, Rg.Range(-60, -10), Rg.Range(-30, 30), Rg.Range(0.2, 0.4), either(Rg.Chance(0.5), C[5], C[4]), 0.5, 0, 0)
				}
				for range 2 {
					x, y := onSwing(&m.s, m.s.R-1, m.s.span*Rg.Range(0.35, 0.75))
					drips.Add(pixelfx.Bit{Kind: pixelfx.Drip, X: x, Y: y, VX: Rg.Range(-8, 8), VY: Rg.Range(10, 40), G: 0.55, Life: 1.2, C: C[3]})
				}
			}
			drips.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			for i := range marks {
				m := &marks[i]
				u := t - m.t0
				if u < 0 {
					continue
				}
				S := m.s.span
				head, c := S*pixelfx.Ease.OutExpo(pixelfx.Clamp(u/clawRake)), pixelfx.Smooth(clawClose0, clawClose0+clawClose, u)
				lo, hi := (S/2)*c, math.Min(head, S-(S/2)*c)
				heat := heatAt(m)
				if hi-lo > 0.02 {
					// a lens: sharp at both ends, thickest in the middle; white-hot at the core, cooling outward and with time
					smear(p, &m.s, lo, hi, m.W, func(uu, r, _ float64, out *pixelfx.Out) {
						v := uu / S
						w := m.W * math.Pow(math.Sin(pi*v), 1.15) * (1 - 0.55*c)
						off := math.Abs(r-(m.s.R-m.W/2)) / (w/2 + 0.01)
						if w < 0.5 || off > 1 {
							return
						}
						lag := 0.0
						if head < S {
							lag = (S - uu) / S
						}
						ci := pixelfx.Round((1-heat)*5.2 + off*1.6 + lag*0.5)
						out.C = C[n-1-int(pixelfx.ClampTo(ci, 0, float64(n-2)))]
					}, 0, 2)
					glowAlong(p, &m.s, m.s.R-m.W/2, lo, hi, 11, C[4], constK(0.42*heat*heat), 0, 0.2)
				}
				// where it has closed, a dotted red scar lingers and fades
				if k := 0.8 * (1 - pixelfx.Clamp((u-clawClose0-clawClose*0.5)/0.4)); c > 0.05 && k > 0 {
					trace(p, &m.s, m.s.R-m.W/2, 0.04*S, lo, C[2], constK(k), 0, 0.6)
					trace(p, &m.s, m.s.R-m.W/2, S-lo, 0.96*S, C[2], constK(k), 0, 0.6)
				}
				// the claw tip glints as it tears in
				if g := u - clawRake*0.4; g > 0 && g < 0.16 {
					pixelfx.Sparkle(p, m.end.x, m.end.y, 5*pixelfx.Ease.OutBack(math.Min(1, g/0.04))*(1-g/0.16), C[6], C[4], 0.4)
				}
			}
			drips.Draw(p)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
