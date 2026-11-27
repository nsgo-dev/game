// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/flying-skull.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the fire (nested flame tongues with leaps and flicker, licks, flame puffs cooling to smoke, wandering embers, layered warm light) from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package critters

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FlyingSkull is a cracked bone skull bobbing in the air, jaw chattering, eyes burning, a fire roaring off its crown that sheds flame puffs, embers and smoke and throws warm light. It is registered as "critters/flying-skull".
var FlyingSkull = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Flying skull",
		Desc:     "A cracked bone skull bobs in the air, jaw chattering, eyes burning, a fire roaring off its crown that sheds flame puffs, embers and smoke and throws warm light.",
		Tags:     []string{"critter", "skull", "undead", "fire", "flying", "monster", "loop"},
		Size:     [2]int{96, 132},
		Origin:   [2]int{48, 120},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "bone", Default: pixelfx.Palette("#1a1114", "#3e2b2a", "#6b5144", "#9e8467", "#cdb892", "#efe2bf", "#fffaea")},
			{Name: "hover", Default: pixelfx.Num(40)},
			{Name: "floor", Default: pixelfx.Bool(true)},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newFlyingSkull,
}

func init() { pixelfx.Register("critters/flying-skull", FlyingSkull) }

var (
	skullD         = 2.4
	skullSocket    = pixelfx.Hex("#120a0c")
	skullSmoke     = pixelfx.Hex("#5b5566")
	skullShadowCol = pixelfx.Hex("#0d0a12")
)

type skullState struct{ x, y, vx, vy float64 }

// skullAt is where the skull is at loop time t: a slow figure-eight bob, and its velocity (for the flames to trail).
func skullAt(t, hover float64) skullState {
	a := (t / skullD) * tau
	return skullState{
		x:  3 * math.Sin(a),
		y:  -hover + 3*math.Sin(2*a+0.5),
		vx: (3 * tau * math.Cos(a)) / skullD,
		vy: (6 * tau * math.Cos(2*a+0.5)) / skullD,
	}
}

func skullBurst(t, a, b float64) float64 {
	if t <= a || t >= b {
		return 0
	}
	return math.Pow(math.Sin(pi*((t-a)/(b-a))), 0.5)
}

// skullJaw is the jaw: two bursts of chatter a loop, a wide gape at the end of the second.
func skullJaw(t float64) float64 {
	ch, b1, b2 := math.Max(0, math.Sin((t/skullD)*tau*22)), skullBurst(t, 0.15, 0.75), skullBurst(t, 1.3, 1.95)
	gape := skullBurst(t, 1.95, 2.3)
	return pixelfx.Round(math.Max(ch*2.4*math.Max(b1, b2), gape*3.4))
}

func skullFlick(t float64) float64 {
	return 0.74 + 0.16*lnoise(t, skullD, 18, 0, 1) + 0.1*lnoise(t, skullD, 46, 0, 2)
}

// The skull's shape, centered on (0, 0): a cranium, cheekbones narrowing to the upper jaw.
func inCranium(x, y float64) bool { return math.Pow(x/11, 2)+math.Pow((y+6)/10.5, 2) <= 1 }
func inFace(x, y float64) bool {
	return y >= -3 && y <= 6.5 && math.Abs(x) <= pixelfx.Lerp(10.2, 7.2, (y+3)/9.5)
}
func inSkull(x, y float64) bool    { return inCranium(x, y) || inFace(x, y) }
func skullBrowY(x float64) float64 { return -6.6 + (8.5-math.Abs(x))*0.3 }
func inSocket(x, y float64) bool {
	return math.Pow((math.Abs(x)-5.1)/3.3, 2)+math.Pow((y+2.9)/3.3, 2) <= 1 && y > skullBrowY(x)
}
func skullTopY(x float64) float64 { return -6 - 10.5*math.Sqrt(math.Max(0, 1-math.Pow(x/11, 2))) }

func inJaw(x, y, jy float64) bool {
	v := y - jy
	return v >= 0 && v <= 5.5 && math.Abs(x) <= skullJawHalfW(v)
}

func skullJawHalfW(v float64) float64 {
	hw := 7.4 - v*0.45
	if v > 4 {
		hw -= (v - 4) * 1.2
	}
	return hw
}

var (
	skullNose  = [8][2]float64{{-1, 1}, {0, 1}, {-2, 2}, {-1, 2}, {0, 2}, {1, 2}, {-2, 3}, {1, 3}}
	skullCrack = [8][2]float64{{4, -16}, {4, -15}, {5, -14}, {5, -13}, {4, -12}, {5, -11}, {6, -10}, {6, -9}}
)

func drawSkullGrid(g *grid, B, F []uint32, open float64) {
	warm, rim := pixelfx.Mix(B[6], F[6], 0.45), pixelfx.Mix(B[5], F[5], 0.3)
	lx, ly := -4.0, -15.0
	lit := func(x, y, k float64) bool { return inSkull(lx+(x-lx)/k, ly+(y-ly)/k) }
	pick := func(cond bool, a, b uint32) uint32 {
		if cond {
			return a
		}
		return b
	}
	g.clear()
	g.shade(-12, -17, 12, 8, func(x, y float64, _ *gridOut) uint32 {
		if !inSkull(x, y) {
			return 0
		}
		c := B[3]
		if lit(x, y, 0.45) {
			c = B[5]
		} else if lit(x, y, 0.74) {
			c = B[4]
		}
		ax := math.Abs(x)
		// temples pinch in behind the sockets
		if ax > 8.2 && y > -7 && y < -1 {
			c = pick(x < 0, B[3], B[2])
		}
		if !inSkull(x+2, y+2.2) || (x > 0 && !inSkull(x+2.6, y)) {
			c = B[2]
		}
		if !inSkull(x+1, y+1.2) && y > 2 {
			c = B[1]
		}
		// the crown catches the fire
		if !inSkull(x, y-1.5) {
			c = pick(ax < 7, warm, rim)
		} else if !inSkull(x, y-2.5) && ax < 6 {
			c = rim
		}
		// under the brow, the bone drops into shadow
		if y > skullBrowY(x)-1.2 && y < skullBrowY(x)+0.4 && ax > 1.6 && ax < 8.6 {
			c = pick(x < 0, B[4], B[3])
		}
		// cheekbones
		if ax > 6.2 && ax < 9.2 && y > 0 && y < 2 {
			c = pick(x < 0, B[5], B[3])
		}
		// the upper teeth
		if y > 6 {
			if ax > 6.4 {
				return 0
			}
			cx := math.Floor(x)
			switch {
			case cx == -6 || cx == -3 || cx == 2 || cx == 5:
				c = B[1]
			case y < 7:
				c = pick(x < 0, B[5], B[4])
			case cx == -1 || cx == 0:
				c = B[3]
			default:
				c = B[4]
			}
		}
		return c
	})
	// sockets: dark hollows with an ember glow catching their lower rim
	g.shade(-9, -7, 9, 1, func(x, y float64, _ *gridOut) uint32 {
		if !inSocket(x, y) {
			return 0
		}
		if !inSocket(x, y+1.3) {
			return pixelfx.Mix(B[2], F[4], 0.55)
		}
		return skullSocket
	})
	for _, q := range skullNose {
		g.put(q[0], q[1], pick(q[1] == 3, B[1], skullSocket))
	}
	g.put(-1, 3, B[2])
	g.put(0, 3, B[2])
	for _, q := range skullCrack {
		g.put(q[0], q[1], B[1])
		g.put(q[0]-1, q[1], B[5])
	}
	g.put(7, -7, B[1])
	g.put(8, -6, B[1])
	// the jaw hangs below: rami up the sides, a row of teeth, the chin
	jy := 8 + open
	g.shade(-8, 4, 8, jy+6, func(x, y float64, _ *gridOut) uint32 {
		ax := math.Abs(x)
		if ax > 6.6 && ax < 8.1 && y > 4 && y < jy+2 {
			return pick(x < 0, B[3], B[2])
		}
		if y < jy || y > jy+5.5 {
			return 0
		}
		v := y - jy
		if ax > skullJawHalfW(v) {
			return 0
		}
		if v < 1.5 {
			if ax > 6 {
				return pick(x < 0, B[4], B[3])
			}
			cx := math.Floor(x)
			if cx == -5 || cx == -2 || cx == 1 || cx == 4 {
				return B[1]
			}
			return pick(v < 1, B[5], B[4])
		}
		if !inJaw(x+1.6, y+1.6, jy) {
			return B[1]
		}
		if v < 2.5 {
			return pick(x < 0, B[4], B[3])
		}
		return B[3]
	})
	if open > 0 {
		for y := 8.0; y < jy; y++ {
			for x := -6.0; x <= 5; x++ {
				g.put(x, y, pick(y == jy-1, pixelfx.Mix(skullSocket, F[3], 0.5), skullSocket))
			}
		}
	}
	g.outlineFn(func(_, _, _, ny float64) uint32 { return pick(ny < 0, pixelfx.Mix(B[0], F[1], 0.75), B[0]) }, 1)
}

// skullFlame is one tongue of fire on the crown; hasY tells a tongue up the temples, standing at y, from one on the crown's top.
type skullFlame struct {
	x, y, h, w, n float64
	hasY, front   bool
}

// skullCrown is the tongues of fire on the crown: one every 2 px, tallest in the middle, leaping now and then; two up the temples; a few small ones in front.
func skullCrown(seed float64) []skullFlame {
	var out []skullFlame
	for x := -10.0; x <= 10; x += 2 {
		out = append(out, skullFlame{x: x, h: 13 + 13*(1-math.Pow(x/11, 2)), w: 2.3 + pixelfx.Rand(x+20, seed, 3)*0.8, n: 37 + x*3 + seed*11})
	}
	for _, s := range [2]float64{-1, 1} {
		out = append(out, skullFlame{x: s * 11, y: -5, hasY: true, h: 10, w: 1.8, n: 61 + s*13 + seed*5})
	}
	for x := -5.0; x <= 5; x += 2.5 {
		out = append(out, skullFlame{x: x, h: 5 + 3*pixelfx.Rand(x*4+20, seed, 5), w: 1.6, n: 91 + x*5 + seed*7, front: true})
	}
	// pixelfx sorts with Array.prototype.sort, which is stable
	slices.SortStableFunc(out, func(a, b skullFlame) int {
		d := math.Abs(b.x) - math.Abs(a.x)
		switch {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
		return 0
	})
	return out
}

func skullHeight(f *skullFlame, t, k float64) float64 {
	leap := math.Max(0, lnoise(t, skullD, 6, f.n*3.1, 70)-0.55) * 1.6
	return (f.h + 0.25*f.h*lnoise(t, skullD, 8, f.n*1.7, 5) + f.h*leap) * (0.9 + 0.1*k)
}

type skull struct {
	F, heat, B []uint32
	hover      float64
	floor      bool
	seed       float64
	tongues    []skullFlame
	g          *grid
	sw         *swarm
}

func newFlyingSkull(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 9)
	s := &skull{F: F, heat: reversed(F), B: pixelfx.Fit(o.Ramp("bone"), 7), hover: o.Num("hover"), floor: o.Bool("floor"), seed: o.Seed(), g: newGrid(-14, -20, 28, 40), sw: newSwarm()}
	s.tongues = skullCrown(s.seed)
	return &pixelfx.Funcs{DrawBackFunc: s.drawBack, DrawFunc: s.draw}
}

func (s *skull) draw(p *pixelfx.Painter, time float64) {
	t := shift(time, skullD, s.seed)
	F, heat, B := s.F, s.heat, s.B
	S, k := skullAt(t, s.hover), skullFlick(t)
	lean, stretch := pixelfx.ClampTo(-S.vx/60, -0.8, 0.8), pixelfx.ClampTo(S.vy/90, -0.3, 0.45)
	X, Y := pixelfx.Round(S.x), pixelfx.Round(S.y)
	lick := func(f *skullFlame) float64 {
		return lnoise(t, skullD, 5, f.n, 40)*2 + lean*skullHeight(f, t, k)*0.9 - pixelfx.ClampTo(f.x*0.25, -2, 2)
	}
	// the fire behind: tall tongues off the back of the crown
	for li := 0; li < 4; li++ {
		for i := range s.tongues {
			f := &s.tongues[i]
			if f.front {
				continue
			}
			y := skullTopY(f.x) + 3
			if f.hasY {
				y = f.y
			}
			tongue(p, X+f.x, Y+y, skullHeight(f, t, k)*(1+stretch), f.w*(1-0.25*math.Abs(lean)), lick(f), t, skullD, f.n, heat, li)
		}
	}
	// embers and flame puffs behind it
	s.particles(p, t, true)
	drawSkullGrid(s.g, B, F, skullJaw(t))
	s.g.draw(p, 1, X, Y)
	// the burning eyes
	for _, side := range [2]float64{-1, 1} {
		ex, ey, fl := X+side*5.1-0.5, Y-2.4, 0.75+0.25*lnoise(t, skullD, 30, side*9, 12)
		p.Px(ex, ey, heat[0])
		p.Px(ex+1, ey, heat[1])
		p.Px(ex, ey+1, heat[2])
		p.Px(ex+1, ey+1, heat[3])
		if fl > 0.8 {
			p.Px(ex, ey-1, heat[3])
			p.Px(ex+1, ey-1, heat[5])
		}
		p.Glow(ex+1, ey+1, 6, F[5], 0.45*fl)
		p.Glow(ex+1, ey+0.5, 3, F[7], 0.5*fl)
	}
	// fire in the mouth when it gapes, and a gout of flame breathed out
	open := skullJaw(t)
	if open > 0 {
		p.Glow(X, Y+8+open*0.5, 4+open*2, F[5], 0.25*open)
	}
	for i := 0.0; i < 9; i++ {
		r := pixelfx.RngOf(s.seed, i, 21)
		born := 1.98 + i*0.03
		age, L := pixelfx.Wrap(t-born, skullD), r.Range(0.25, 0.45)
		if age >= L {
			continue
		}
		S0 := skullAt(born, s.hover)
		x, y := pixelfx.Fly(S0.x+r.Range(-3, 3), S0.y+11, r.Range(-22, 22), r.Range(40, 75), age, -70, 3.5)
		firePuff(p, x, y, 1-age/L, r.Range(1.1, 2), r.Next()*100, heat)
	}
	// the front flames, over the crown
	for li := 0; li < 4; li++ {
		for i := range s.tongues {
			f := &s.tongues[i]
			if f.front {
				tongue(p, X+f.x, Y+skullTopY(f.x)+1.5, skullHeight(f, t, k)*(1+stretch), f.w, lick(f)*0.6, t, skullD, f.n, heat, li)
			}
		}
	}
	s.particles(p, t, false)
	// light: a big warm pool, a hot core over the crown, the eyes
	br := 0.92 + 0.08*math.Sin((t/skullD)*tau*2)
	p.Glow(X+lean*4, Y-20, 74*br*(0.9+0.1*k), pixelfx.Mix(F[4], F[5], 0.5), 0.48*k)
	p.Glow(X, Y-21, 24, F[6], 0.3*k)
	p.Glow(X, Y-15, 9, F[8], 0.16*k)
}

// particles draws the flame puffs off the tongue tips that cool into smoke, licks of flame breaking away and embers wandering up; back draws the smoke behind the skull.
func (s *skull) particles(p *pixelfx.Painter, t float64, back bool) {
	F, heat, hover := s.F, s.heat, s.hover
	sw := s.sw.begin(t)
	sw.ps.Sparks = []uint32{F[7], F[6], F[4], F[2]}
	if back {
		// smoke from the tips, born where the skull was
		pixelfx.Cycle(t, skullD, 15, s.seed*7+1, func(r *pixelfx.Rng) float64 { return r.Range(1.7, 2.5) }, func(age, L float64, r *pixelfx.Rng, _ int) {
			S0 := skullAt(pixelfx.Wrap(t-age, skullD), hover)
			x0 := S0.x + r.Range(-6, 6)
			y0 := S0.y - 22 - r.Range(0, 8)
			x, y := pixelfx.Fly(x0, y0, r.Range(-5, 5), -r.Range(16, 26), age, -12, 1.2)
			sw.add(pixelfx.Smoke, x+math.Sin(age*1.3+r.Next()*6)*3*age, y, 0, 0, age, L, pixelfx.Mix(skullSmoke, F[2], 0.3), r.Range(2.8, 4.4), pixelfx.Big, 0)
		})
		sw.draw(p)
		return
	}
	// flame puffs: bright, fast, cooling down the ramp as they rise
	pixelfx.Cycle(t, skullD, 40, s.seed*7+2, func(r *pixelfx.Rng) float64 { return r.Range(0.18, 0.36) }, func(age, L float64, r *pixelfx.Rng, _ int) {
		S0 := skullAt(pixelfx.Wrap(t-age, skullD), hover)
		side := r.Chance(0.3)
		var a float64
		if side {
			a = r.Sign() * r.Range(0.9, 1.45)
		} else {
			a = r.Range(-0.8, 0.8)
		}
		x0 := S0.x + math.Sin(a)*10.5
		y0 := S0.y - 6 - math.Cos(a)*10
		if !side {
			y0 -= r.Range(2, 10)
		}
		x, y := pixelfx.Fly(x0, y0, r.Range(-6, 6)+math.Sin(a)*6, -r.Range(18, 34), age, -95, 3.2)
		firePuff(p, x+math.Sin(age*17+r.Next()*6)*0.8, y, 1-age/L, r.Range(1, 2.2), r.Next()*100, heat)
	})
	// licks: little blobs of flame breaking off the tips
	pixelfx.Cycle(t, skullD, 18, s.seed*7+3, pixelfx.Fixed(0.26), func(age, L float64, r *pixelfx.Rng, _ int) {
		S0 := skullAt(pixelfx.Wrap(t-age, skullD), hover)
		fx := r.Range(-10, 10)
		x0, y0 := S0.x+fx, S0.y+skullTopY(fx)-r.Range(4, 16)*(1-math.Abs(fx)/14)
		kk := 1 - age/L
		x := x0 + r.Range(-3, 3)*age
		y := y0 - r.Range(12, 20)*age
		rr := r.Range(0.9, 1.7) * (0.4 + 0.6*kk)
		c := heat[6]
		if kk > 0.5 {
			c = heat[5]
		}
		p.Circle(x, y, rr+0.6, c, 1)
		if kk > 0.35 {
			c = heat[4]
			if kk > 0.7 {
				c = heat[2]
			}
			p.Circle(x, y-0.3, rr*0.6, c, 1)
		}
	})
	// embers wander up and cool
	pixelfx.Cycle(t, skullD, 24, s.seed*7+4, func(r *pixelfx.Rng) float64 { return r.Range(1, 2.4) }, func(age, L float64, r *pixelfx.Rng, _ int) {
		S0 := skullAt(pixelfx.Wrap(t-age, skullD), hover)
		x0, y0, sd := S0.x+r.Range(-11, 11), S0.y-r.Range(6, 24), r.Next()*100
		x, y := pixelfx.Fly(x0, y0, r.Range(-22, 22), -r.Range(25, 70), age, -20, 1.6)
		size := 1.0
		if r.Chance(0.3) {
			size = 2
		}
		sw.add(pixelfx.Ember, x+math.Sin(age*2.3+sd)*4*math.Min(1, age), y, 0, 0, age, L, F[4], size, 0, sd)
	})
	// sparks pop out of the fire now and then, fall, and skip along the floor
	if s.floor {
		pixelfx.Cycle(t, skullD, 5, s.seed*7+5, func(r *pixelfx.Rng) float64 { return r.Range(0.9, 1.4) }, func(age, L float64, r *pixelfx.Rng, _ int) {
			S0 := skullAt(pixelfx.Wrap(t-age, skullD), hover)
			b := bounce(S0.x+r.Range(-6, 6), S0.y-r.Range(10, 18), r.Range(-28, 28), -r.Range(50, 110), age, 430, -0.5, 0.42)
			vx, vy := b.vx, b.vy
			if b.rest {
				vx, vy = 0.1, 0
			}
			sw.add(pixelfx.Spark, b.x, b.y, vx, vy, age, L, 0, 1, 0, r.Next()*100)
		})
	}
	sw.draw(p)
}

func (s *skull) drawBack(p *pixelfx.Painter, time float64) {
	if !s.floor {
		return
	}
	t := shift(time, skullD, s.seed)
	F, S, k := s.F, skullAt(t, s.hover), skullFlick(t)
	// its shadow on the floor, and the floor lit warm around it
	shadow(p, S.x, 0, 9, 1.6, skullShadowCol, 0.6)
	for x := -30.0; x <= 30; x += 6 {
		p.Glow(S.x+x, -1, 20, pixelfx.Mix(F[4], F[5], 0.5), 0.15*k*(1-math.Abs(x)/40))
	}
}
