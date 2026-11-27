// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/heavy-landing.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the rocks, grit and dust from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	landingFall = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.8), pixelfx.Y.Is(-46), pixelfx.Crouch.Is(-1), pixelfx.Lean.Is(-1), pixelfx.FX.Is(-9), pixelfx.FY.Is(-42), pixelfx.BX.Is(10), pixelfx.BY.Is(-42), pixelfx.FFX.Is(6), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(-3), pixelfx.FBY.Is(-3), pixelfx.ToeB.On(), pixelfx.Ghost.Is(1))
	landingHero = pixelfx.Pose{}.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(8), pixelfx.Lean.Is(5), pixelfx.FX.Is(13), pixelfx.FY.Is(-8), pixelfx.BX.Is(-6), pixelfx.BY.Is(-23), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-10))
)

// HeavyLanding is a character landing hard: the floor dents and cracks under their feet, two rings of dust roll out along the floor one after the other, and pebbles hop away. It is registered as "impacts/heavy-landing".
var HeavyLanding = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Heavy landing",
		Desc:     "A character lands hard: the floor dents and cracks under their feet, two rings of dust roll out along the floor one after the other, and pebbles hop away.",
		Tags:     []string{"impact", "landing", "ground", "stomp", "dust", "rocks", "actor", "one-shot"},
		Size:     [2]int{176, 80},
		Origin:   [2]int{88, 60},
		Duration: landingDuration,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1c1a24", "#2f2a3a", "#4a4258", "#6b6280", "#9088a4", "#bdb6cc", "#e6e1ee")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "seed", Default: pixelfx.Num(14)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: -0.26, Pose: landingFall},
			{T: -0.01, Pose: landingFall.With(pixelfx.Y.Is(-6)), How: "in"},
			{T: 0, Pose: landingHero, How: "snap"},
			{T: 0.55, Pose: landingHero.With(pixelfx.Crouch.Is(7))},
			{T: 0.95, Pose: landingHero.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(3), pixelfx.FX.Is(13), pixelfx.FY.Is(-15), pixelfx.BX.Is(-5), pixelfx.BY.Is(-20))},
			{T: 1.3, Pose: pixelfx.Ready},
			{T: 1.6, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{{T: 0.005, Stop: 0.13, Shake: 0.32, Amp: pixelfx.Val(3)}},
	},
	New: newHeavyLanding,
}

func init() { pixelfx.Register("impacts/heavy-landing", HeavyLanding) }

const landingDuration = 1.6

// landingDent draws the dent: the floor's edge pressed into a shallow dish, scuffed lips either side.
func landingDent(p *pixelfx.Painter, st stoneCols, k float64) {
	for x := -10.0; x <= 9; x++ {
		q := math.Abs(x+0.5) / 10
		dep := pixelfx.Round(1.8 * (1 - q*q))
		if q < 0.82 {
			for y := 0.0; y < dep; y++ {
				p.Dot(x, y, soot, k)
			}
			p.Dot(x, dep, st.lip, k)
			continue
		}
		c := st.lip
		if q > 0.92 {
			c = st.light
		}
		p.Dot(x, -1, c, k)
	}
}

// landingRing is one of the two dust rings: when it starts, how long it rolls, how far, how thick, how dense, and whether it has a lit leading edge.
type landingRing struct {
	t0, dur, r1, w, k0 float64
	edge               bool
}

var landingRings = [2]landingRing{{0.01, 0.45, 58, 4.5, 0.75, true}, {0.12, 0.85, 42, 6, 0.8, false}}

func newHeavyLanding(o *pixelfx.Options) pixelfx.Instance {
	C, st, R, D := pixelfx.Fit(o.Palette(), 7), stoneOf(o.Color("stone")), pixelfx.NewRng(o.Seed()), float64(landingDuration)
	floor := func(float64) float64 { return 0 }
	// the rings' far halves go behind the character, the near halves and all the debris in front
	back := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor})
	front := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor})
	bits := pixelfx.NewBits(floor)
	cracks := makeCracks(R, 6, crackOpts{len0: 8, len1: 20, dive: 0.7, maxY: 12, forks: 0.4, x00: 2, x01: 6})
	DUST := pixelfx.Mix(st.dust, C[5], 0.4)
	LIT := pixelfx.Mix(DUST, pixelfx.White, 0.3)
	PH := R.Range(0, pixelfx.Tau)
	// puffs riding each ring out along the floor
	ring := func(n int, v0, v1, size0, size1, delay float64) {
		for i := range n {
			g := (float64(i)/float64(n))*pixelfx.Tau + R.Range(-0.1, 0.1)
			v := R.Range(v0, v1)
			ps := front
			if math.Sin(g) < 0 {
				ps = back
			}
			vy := math.Sin(g)*v*0.16 - R.Range(2, 10)
			life := R.Range(0.45, 0.8)
			c := DUST
			if R.Chance(0.45) {
				c = LIT
			}
			size := R.Range(size0, size1)
			ps.Spawn(pixelfx.Dust, math.Cos(g)*4, math.Sin(g)*0.7-1, math.Cos(g)*v, vy, life, c, size, delay+R.Range(0, 0.03), 0)
		}
	}
	ring(20, 140, 190, 2.2, 3.4, 0.01)
	ring(18, 70, 110, 3.4, 5.4, 0.12)
	// and a low cloud at the feet that rises a little and hangs
	for i := range 12 {
		s, up := R.Sign(), i < 6
		ps := back
		if i%2 != 0 {
			ps = front
		}
		x := s * R.Range(2, 10)
		y := -R.Range(1, 4)
		vx := s * R.Range(10, 35)
		if up {
			vx *= 0.6
		} else {
			vx *= 1
		}
		var vy float64
		if up {
			vy = -R.Range(25, 60)
		} else {
			vy = -R.Range(4, 12)
		}
		life := R.Range(0.55, 0.9)
		c := DUST
		if R.Chance(0.5) {
			c = LIT
		}
		size := R.Range(3, 5)
		ps.Spawn(pixelfx.Dust, x, y, vx, vy, life, c, size, R.Range(0.02, 0.1), 0)
	}
	// pebbles that hop away along the floor, and grit
	for range 12 {
		s, v := R.Sign(), R.Range(50, 130)
		var b pixelfx.Bit
		b.Kind = pixelfx.Chunk
		b.X, b.Y = s*R.Range(3, 8), -1
		b.VX, b.VY = s*v, -R.Range(140, 230)
		b.G, b.Drag, b.Bounce = 1, 0.5, 0.55
		b.Life = R.Range(1, 1.45)
		b.A = R.Range(0, pixelfx.Tau)
		b.VA = s * R.Range(8, 16)
		b.S = 1
		if R.Chance(0.3) {
			b.S = 2
		}
		b.Pts = rockShape(R, 6)
		bits.Add(b)
	}
	for range 24 {
		s := R.Sign()
		a := -pi/2 + s*R.Range(0.3, 1.2)
		v := R.Range(60, 170)
		x := s * R.Range(2, 7)
		life := R.Range(0.6, 1.1)
		c := pixelfx.Pick(R, []uint32{st.light, st.body, st.lip})
		size := 1.0
		if R.Chance(0.3) {
			size = 2
		}
		front.Spawn(pixelfx.Grit, x, -1, math.Cos(a)*v, math.Sin(a)*v, life, c, size, 0, 0)
	}

	t := 0.0
	// the two dust rings themselves: soft bands on the floor, the second slower, lumpier and later
	rings := func(p *pixelfx.Painter, far bool) {
		half := 1.0
		if far {
			half = -1
		}
		for _, rg := range landingRings {
			g := (t - rg.t0) / rg.dur
			if g <= 0 || g >= 1 {
				continue
			}
			r := 6 + rg.r1*pixelfx.Ease.OutCubic(g)
			dustRing(p, 0, 0, r, rg.w*(1+g), rg.k0*math.Pow(1-g, 1.2)*math.Min(1, g/0.06), DUST, dustOpts{sy: 0.2, ph: PH + rg.t0*10, brk: g * 0.8, lit: 0.35, maxY: math.Inf(1), half: half})
			// the fast ring's leading edge: a crisp lit band on the floor
			if rg.edge && g < 0.75 {
				pixelfx.Band(p, 0, 0.5, r+2, 0.2, math.Max(1, 2.5*(1-g)), 1.1-g/0.75, []uint32{st.body, st.light, LIT, pixelfx.White}, func(a float64) bool { return (math.Sin(a) < 0) == far }, false)
			}
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range bits.List {
				bits.List[i].PVY = bits.List[i].VY
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.PVY > 80 && b.VY <= 0 {
					vx := R.Sign() * R.Range(6, 20)
					vy := -R.Range(2, 10)
					life := R.Range(0.25, 0.45)
					front.Spawn(pixelfx.Dust, b.X, -0.5, vx, vy, life, DUST, R.Range(1, 1.8), 0, 0)
					b.PVY = 0
				}
			}
			back.Update(dt)
			front.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			fade := 1 - pixelfx.Smooth(D-0.5, D-0.1, t)
			landingDent(p, st, fade)
			white := [4]uint32{pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White}
			drawCracks(p, cracks, func(c *crack) float64 { return c.len * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-c.t0)/0.08)) }, func(float64) float64 { return 0 }, white, soot, st.light, fade)
			rings(p, true)
			back.Draw(p, pixelfx.LayerBoth)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			rings(p, false)
			front.Draw(p, pixelfx.LayerBoth)
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			// the thud: a flat streak of light along the floor for a frame or two
			if t < 0.035 {
				p.EllipseFn(0, 0, 15, 2.5, func(d, _ float64) (uint32, float64) {
					if d < 0.55 {
						return pixelfx.White, 1
					}
					return C[6], 1
				})
			} else if t < 0.07 {
				for x := -22.0; x <= 22; x++ {
					c := C[5]
					if math.Abs(x) < 10 {
						c = pixelfx.White
					}
					p.Dot(x, 0, c, 1-math.Abs(x)/24)
				}
			}
			if t < 0.12 {
				p.GlowLine(-20, 0, 20, 0, 8, C[6], 0.45*(1-t/0.12))
			}
		},
	}
}
