// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/frost-nova.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package ice

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	novaHigh = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(0), pixelfx.FX.Is(-7), pixelfx.FY.Is(-44), pixelfx.BX.Is(8), pixelfx.BY.Is(-44), pixelfx.FFX.Is(-5), pixelfx.FBX.Is(6))
	novaSlam = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(8), pixelfx.Lean.Is(3), pixelfx.FX.Is(-9), pixelfx.FY.Is(-8), pixelfx.BX.Is(11), pixelfx.BY.Is(-8), pixelfx.FFX.Is(-8), pixelfx.FBX.Is(9))
	novaEase = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-17), pixelfx.BX.Is(9), pixelfx.BY.Is(-17), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5))
	novaDust = pixelfx.Hex("#d8f2ff")
)

// FrostNova spirals frost into a spinning snowflake, then a wave blooms three rings of ice crystals round the caster; snow drifts, frost creeps, and the ice melts back into the floor. It is registered as "ice/frost-nova".
var FrostNova = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Frost nova",
		Desc:     "Frost spirals into a spinning snowflake, then a wave blooms three rings of ice crystals round the caster; snow drifts, frost creeps, and the ice melts back into the floor.",
		Tags:     []string{"ice", "nova", "aoe", "frost", "magic", "one-shot"},
		Size:     [2]int{288, 192},
		Origin:   [2]int{144, 150},
		Duration: novaDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "reach", Default: pixelfx.Num(104)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{76},
		Poses: []pixelfx.Key{
			{T: -0.34, Pose: pixelfx.Ready},
			{T: -0.04, Pose: novaHigh.With(pixelfx.Crouch.Is(2), pixelfx.FY.Is(-36), pixelfx.BY.Is(-36))},
			{T: 0.36, Pose: novaHigh},
			{T: 0.42, Pose: novaSlam, How: "snap"},
			{T: 0.9, Pose: novaSlam.With(pixelfx.Crouch.Is(7))},
			{T: 1.9, Pose: novaSlam.With(pixelfx.Crouch.Is(6))},
			{T: 2.6, Pose: novaEase},
			{T: 3.4, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: 0.42, Stop: 0.1, Shake: 0.26, Amp: pixelfx.Val(2), NoTarget: true},
			{T: 0.66, Push: pixelfx.Val(0.9), Straw: pixelfx.Val(6), Y: pixelfx.Val(-10), Skin: "ice", Shiver: 1.7},
			{T: 2.9, Push: pixelfx.Val(0.2), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: 0.42, X: 10, N: 5, Dir: 1, C: novaDust}, {T: 0.42, X: -10, N: 5, Dir: -1, C: novaDust}},
	},
	New: newFrostNova,
}

func init() { pixelfx.Register("ice/frost-nova", FrostNova) }

var (
	novaDuration                                         = 3.4
	novaCharge, novaSpread, novaHold, novaMelt, novaCore = 0.42, 0.62, 1.95, 0.7, -24.0
	// three rings of crystals: tall ones hugging the caster, smaller ones further out
	novaRings = [3]novaRing{
		{q: [2]float64{0.15, 0.24}, h: [2]float64{24, 32}, w: [2]float64{10, 13}, n: 2, kids: 2},
		{q: [2]float64{0.36, 0.52}, h: [2]float64{13, 19}, w: [2]float64{7, 9}, n: 3, kids: 1},
		{q: [2]float64{0.62, 0.9}, h: [2]float64{5, 10}, w: [2]float64{4, 6}, n: 4, kids: 0},
	}
)

// novaRing is a ring of crystals: where it lies (a share of the reach), its crystals' heights and widths, how many on each side and how many smaller crystals each has at its foot.
type novaRing struct {
	q, h, w [2]float64
	n, kids int
}

// novaPart is one crystal of a site.
type novaPart struct{ dx, h, w, lean float64 }

// novaSite is a cluster of crystals the wave raises.
type novaSite struct {
	x, d, h float64
	back    bool
	t0      float64
	up      bool
	parts   []novaPart
	phase   float64
}

// novaFrost draws frost on the floor: a white crust, little tufts, a dithered bloom under the surface.
func novaFrost(p *pixelfx.Painter, C []uint32, x0, x1, reach, a, edge float64) {
	W := pixelfx.White
	for x := math.Floor(x0); x <= math.Ceil(x1); x++ {
		d := math.Abs(x + 0.5)
		q := d / reach
		k := pixelfx.Clamp((a*1.15 - q) / 0.15)
		if k <= 0 {
			continue
		}
		h, fresh := pixelfx.Rand(x, 17), edge > 0 && d > edge-3
		tuft := 0.0
		if h < 0.45 {
			tuft = pixelfx.Round((1 + pixelfx.Rand(x, 5)*3) * (1 - q*0.6))
		}
		D := (4 + pixelfx.Rand(x, 11)*4) * (1 - q*0.5)
		p.Dot(x, 0, pick(fresh || h < 0.25, W, C[6]), k)
		for j := 1.0; j <= tuft; j++ {
			p.Dot(x, -j, pick(j == tuft, W, C[5]), k)
		}
		if tuft > 1 && pixelfx.Rand(x, 9) < 0.5 {
			p.Dot(x+pick(pixelfx.Rand(x, 3) < 0.5, -1.0, 1), -tuft+1, C[6], k)
		}
		for j := 1.0; j <= D; j++ {
			v := j / D
			var c uint32
			switch {
			case pixelfx.Rand(x*31+j, 7) < 0.06:
				c = W
			case v < 0.25:
				c = C[6]
			case v < 0.55:
				c = C[5]
			default:
				c = C[4]
			}
			p.Dot(x, j, c, k*(1.1-v)*pick(fresh, 1.3, 1))
		}
	}
}

// novaSnowflake draws the snowflake the cold gathers into: six arms with side branches, spinning, pulsing.
func novaSnowflake(p *pixelfx.Painter, C []uint32, x, y, L, spin, k float64) {
	if L < 0.8 {
		return
	}
	for i := range 6 {
		a := spin + (float64(i)*tau)/6
		ca, sa := math.Cos(a), math.Sin(a)
		for d := 0.0; d <= L; d += 0.5 {
			p.Dot(x+ca*d, y+sa*d, pick(d < L*0.4, pixelfx.White, C[6]), k+0.2)
		}
		if L > 3 {
			for _, s := range [2]float64{-1, 1} {
				bx, by, ba := x+ca*L*0.6, y+sa*L*0.6, a+s*0.9
				for d := 0.0; d <= L*0.35; d += 0.5 {
					p.Dot(bx+math.Cos(ba)*d, by+math.Sin(ba)*d, C[5], k)
				}
			}
		}
	}
	p.Px(x, y, pixelfx.White)
	p.Glow(x, y, 10+L*3, C[5], 0.35+0.5*k)
}

func newFrostNova(o *pixelfx.Options) pixelfx.Instance {
	C, REACH := pixelfx.Fit(o.Palette(), 8), o.Num("reach")
	DIM := []uint32{C[0], C[0], C[1], C[2], C[3], C[4], C[5], C[6]}
	R := pixelfx.NewRng(o.Seed())
	ps, bits, lights := newSim(R, C)
	W := pixelfx.White
	CHARGE, SPREAD, HOLD, MELT, CORE := novaCharge, novaSpread, novaHold, novaMelt, novaCore
	front := func(t float64) float64 {
		if t < CHARGE {
			return 0
		}
		return REACH * (1 - math.Pow(1-pixelfx.Clamp((t-CHARGE)/SPREAD), 3))
	}
	reachedAt := func(d float64) float64 { return CHARGE + SPREAD*(1-math.Cbrt(1-math.Min(1, d/REACH))) }
	// lay out the crystals: both sides, three rings, alternating back and front rows
	var sites []*novaSite
	for _, side := range [2]float64{-1, 1} {
		for ri, ring := range novaRings {
			for i := 0; i < ring.n; i++ {
				q := ring.q[0] + ((ring.q[1]-ring.q[0])*(float64(i)+R.Range(0.2, 0.8)))/float64(ring.n)
				h := R.Range(ring.h[0], ring.h[1])
				w := R.Range(ring.w[0], ring.w[1])
				parts := []novaPart{{dx: 0, h: h, w: w, lean: side * h * R.Range(0.1, 0.28)}}
				for k := 0; k < ring.kids; k++ {
					s := pick(k%2 != 0, -side, side)
					kh := h * R.Range(0.4, 0.62)
					dx := s * R.Range(0.3, 0.5) * w
					parts = append([]novaPart{{dx: dx, h: kh, w: w * 0.62, lean: s * kh * R.Range(0.35, 0.6)}}, parts...)
				}
				back := (i+ri+pick(side > 0, 1, 0))%2 == 1
				t0 := reachedAt(q*REACH) + R.Range(0, 0.03)
				sites = append(sites, &novaSite{x: side * q * REACH, d: q * REACH, h: h, back: back, t0: t0, parts: parts, phase: R.Next()})
			}
		}
	}
	t, released := 0.0, false
	sink := func(tt float64) float64 { return pixelfx.Smooth(HOLD, HOLD+MELT, tt) }

	drawCrystals := func(p *pixelfx.Painter, back bool) {
		m := sink(t)
		for _, s := range sites {
			if !s.up || s.back != back || m >= 1 {
				continue
			}
			g, white := pixelfx.OutBack(pixelfx.Clamp((t-s.t0)/0.1), 2.6), t < s.t0+0.03
			sw := math.Mod((t-s.t0)*0.7+s.phase*3, 1.6) / 0.5
			for _, q := range s.parts {
				pixelfx.Crystal(p, s.x+q.dx, pick(back, -1.0, 0), q.h*g*(1-m), q.w*(1-m*0.3), q.lean*g*(1-m), pick(back, DIM, C), 1, white, pick(sw <= 1, sw, -1))
			}
		}
	}
	ring := func(p *pixelfx.Painter, back bool) {
		if t < CHARGE {
			return
		}
		u := pixelfx.Clamp((t - CHARGE) / (SPREAD + 0.15))
		if u >= 1 {
			return
		}
		pixelfx.Band(p, 0, 0, front(t)+2, 0.16, math.Max(2, 10*math.Pow(1-u, 0.7)), pick(u < 0.75, 1, (1-u)/0.25), C, func(g float64) bool { return (math.Sin(g) < 0) == back }, t-CHARGE < 0.1)
		if !back {
			for _, s := range [2]float64{-1, 1} {
				p.Glow(s*(front(t)+2), -2, 22, C[5], 0.55*(1-u))
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < CHARGE-0.06 {
				// the cold gathers: streaks spiral in to the core, vapour slides in along the floor
				if R.Chance(0.9) {
					a, D, T := R.Range(0, tau), R.Range(26, 58), R.Range(0.2, 0.3)
					v := (D * 3) / (math.Exp(3*T) - 1)
					x, y := math.Cos(a)*D, math.Min(CORE+math.Sin(a)*D*0.8, -2)
					l := pixelfx.Hypot(x, y-CORE)
					if l == 0 {
						l = 1
					}
					ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, ((CORE-y)/l)*v, T, pick(R.Chance(0.5), C[6], C[5]), 1, 0, 0)
				}
				if R.Chance(0.14) {
					s := R.Sign()
					ps.Spawn(pixelfx.Vapour, s*R.Range(28, 60), -R.Range(0, 2), -s*R.Range(35, 70), -R.Range(0, 3), R.Range(0.35, 0.5), C[6], R.Range(2, 3), 0, 0)
				}
			}
			if t >= CHARGE && !released {
				released = true
				lights.Add(0, CORE, 84, C[4], 0.8, 0.55)
				lights.Add(0, CORE, 45, W, 0.7, 0.12)
				for range 22 {
					a, sp := R.Range(-pi, 0.25), R.Range(110, 280)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: R.Range(-3, 3), Y: CORE, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 0.7, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.9, 1.5), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1.5, 3.5), C: pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})})
				}
				for range 14 {
					a, sp := R.Range(-pi, 0), R.Range(40, 160)
					ps.Spawn(pixelfx.Flake, 0, CORE, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.2, 1.8), pick(R.Chance(0.6), W, C[6]), R.Range(0.5, 1.6), 0, 0)
				}
			}
			fr := front(t)
			if t > CHARGE && t < CHARGE+SPREAD {
				for _, s := range [2]float64{-1, 1} {
					ps.Spawn(pixelfx.Vapour, s*fr+R.Range(-2, 2), -R.Range(0, 5), s*R.Range(15, 75), -R.Range(2, 15), R.Range(0.6, 1), pick(R.Chance(0.7), C[6], C[5]), R.Range(3, 5.5)*(1-0.4*(fr/REACH)), 0, 0)
					if R.Chance(0.45) {
						ps.Spawn(pixelfx.Flake, s*fr, -2, s*R.Range(10, 45), -R.Range(30, 85), R.Range(1.1, 1.9), pick(R.Chance(0.5), W, C[6]), R.Range(0.5, 1.2), 0, 0)
					}
				}
			}
			for _, s := range sites {
				if s.up || t < s.t0 {
					continue
				}
				s.up = true
				// each crystal punches up with a spray of shards, a puff and a glint
				for i := 0.0; i < 2+pixelfx.Round(s.h/8); i++ {
					a := -pi/2 + R.Range(-1.3, 1.3)
					sp := R.Range(60, 200) * (0.5 + s.h/40)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: s.x + R.Range(-3, 3), Y: 0, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 0.8, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.9, 1.6), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1.5, 3), C: pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})})
				}
				ps.Spawn(pixelfx.Vapour, s.x, -1, R.Range(-30, 30), -R.Range(2, 8), R.Range(0.5, 0.9), C[6], R.Range(2.5, 4), 0, 0)
				if s.h > 10 {
					ps.Spawn(pixelfx.StarKind, s.x+R.Range(-3, 3), -R.Range(4, s.h*0.8), 0, -3, R.Range(0.3, 0.6), C[6], R.Range(1, 2.4), 0, 0)
				}
			}
			// while it holds: glints run over the crystals, snow drifts down
			if t > CHARGE+0.2 && t < HOLD+0.2 {
				if R.Chance(0.5) {
					if s := pixelfx.Pick(R, sites); s.up {
						ps.Spawn(pixelfx.StarKind, s.x+R.Range(-3, 3), -R.Range(2, s.h*0.9), 0, -3, R.Range(0.3, 0.6), pick(R.Chance(0.6), C[6], W), R.Range(0.6, 2.2), 0, 0)
					}
				}
				if t < HOLD-0.3 && R.Chance(0.16) {
					ps.Spawn(pixelfx.Flake, R.Range(-1, 1)*fr, -R.Range(40, 90), R.Range(-5, 5), R.Range(3, 10), R.Range(1.1, 1.5), pick(R.Chance(0.5), W, C[6]), R.Range(0.5, 1.4), 0, 0)
				}
			}
			// melting: drips run off the crystals, vapour lifts off the floor
			if t > HOLD && t < HOLD+MELT {
				if R.Chance(0.5) {
					s := pixelfx.Pick(R, sites)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Drip, X: s.x + R.Range(-3, 3), Y: -R.Range(3, s.h*0.6*(1-sink(t))), VY: 10, G: 0.6, Life: 1, C: C[5]})
				}
				if R.Chance(0.25) {
					ps.Spawn(pixelfx.Vapour, R.Range(-1, 1)*REACH*0.8, -R.Range(0, 3), R.Range(-10, 10), -R.Range(6, 16), R.Range(0.6, 1), C[5], R.Range(3, 5), 0, 0)
				}
			}
			ps.Update(dt)
			bits.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			out, fr := 1-pixelfx.Smooth(HOLD+0.2, HOLD+MELT+0.6, t), front(t)
			if out > 0 && t > CHARGE {
				novaFrost(p, C, -math.Max(12, fr+1), math.Max(12, fr+1), REACH, out, fr)
			}
			ring(p, true)
			drawCrystals(p, true)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, false)
			drawCrystals(p, false)
			if t < CHARGE {
				k := t / CHARGE
				beat := 1 + 0.25*math.Exp(-math.Pow(t-CHARGE*0.55, 2)/0.002) + 0.3*math.Exp(-math.Pow(t-CHARGE*0.85, 2)/0.002)
				novaSnowflake(p, C, 0, CORE, (2+5.5*k*k)*beat, t*4, k)
			} else if t < CHARGE+0.22 {
				a := (t - CHARGE) / 0.22
				pixelfx.Flare(p, 0, CORE, 36*math.Pow(1-a, 1.5), 1-a, C)
				p.Glow(0, CORE, 46, W, 0.6*(1-a))
			}
			// the frosted floor gives off a cold light while the ice stands
			if t > CHARGE {
				k, fr := (1-pixelfx.Smooth(HOLD, HOLD+MELT+0.3, t))*(0.85+0.15*math.Sin(t*2.4)), front(t)
				for i := -3.0; i <= 3; i++ {
					p.Glow((i/3.5)*fr, -4, 26, C[4], 0.16*k)
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
