// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/campfire.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tongue, lick, flame-puff, smoke and ember recipe from Sprite Fusion's open-source fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Campfire is crossed logs on a bed of coals and a lively fire: tongues that lick and leap, popping sparks, embers drifting up, smoke curling off, a warm pool of light. It is registered as "fire/campfire".
var Campfire = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Campfire",
		Desc:     "Crossed logs on a bed of coals and a lively fire: tongues that lick and leap, popping sparks, embers drifting up, smoke curling off, a warm pool of light.",
		Tags:     []string{"fire", "campfire", "prop", "light", "smoke", "ambient", "loop"},
		Size:     [2]int{128, 128},
		Origin:   [2]int{64, 112},
		Duration: campfireDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "wood", Default: pixelfx.Palette("#0e080a", "#24130f", "#3b2216", "#5a3520", "#8a5a34", "#b9884f")},
			{Name: "height", Default: pixelfx.Num(22)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newCampfire,
}

func init() { pixelfx.Register("fire/campfire", Campfire) }

const (
	campfireDuration = 3
	// campfireCross is where the logs cross, as a share of each log's length.
	campfireCross = 0.56
)

var campfireAsh = [3]uint32{pixelfx.Hex("#2a2530"), pixelfx.Hex("#4a4350"), pixelfx.Hex("#6b6270")}

// campfireLog is a log from its near end (x0, y0) to its far end (x1, y1), r its radius.
type campfireLog struct{ x0, y0, x1, y1, r float64 }

// campfireLogs are the logs, near end first: they cross in an X over the coals, their near ends showing the cut grain and their far ends poking out past the flames.
var campfireLogs = [2]campfireLog{
	{-19, 2, 15, -12, 3.4},
	{19, 2, -15, -12, 3.4},
}

// campfireTongues are the fire: x, base, height share, half-width, back row.
var campfireTongues = []tongue{
	{-7.5, -7, 0.42, 1.8, true},
	{7.5, -7, 0.46, 1.8, true},
	{-3, -9, 0.78, 2.0, true},
	{3.2, -9, 0.82, 2.0, true},
	{-12, -3, 0.14, 1.3, false},
	{12.5, -3, 0.16, 1.3, false},
	{-9.5, -4.5, 0.3, 1.9, false},
	{9.5, -4.5, 0.34, 1.9, false},
	{-5, -6, 0.6, 2.6, false},
	{5.2, -6, 0.66, 2.6, false},
	{0, -7, 1, 3.4, false},
}

// campfireLogDraw draws a log between u0 and u1 along it: a 1 px dark rim, a lit top, grooves running along the grain, a shadowed underside; charred with glowing cracks in the fire.
func campfireLogDraw(p *pixelfx.Painter, L campfireLog, W, H []uint32, u0, u1, t, S float64) {
	const D = campfireDuration
	x0, y0, x1, y1, r := L.x0, L.y0, L.x1, L.y1, L.r
	dx, dy := x1-x0, y1-y0
	ln := pixelfx.Hypot(dx, dy)
	ax, ay := dx/ln, dy/ln
	nx, ny := -ay, ax
	if ny < 0 {
		nx, ny = -nx, -ny
	}
	rim := 1 - 1.05/r
	p.Shade(math.Min(x0, x1)-r-1, math.Min(y0, y1)-r-1, math.Max(x0, x1)+r+1, math.Max(y0, y1)+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dist := (x-x0)*ax + (y-y0)*ay
		u, s := dist/ln, ((x-x0)*nx+(y-y0)*ny)/r
		if u < u0 || u > u1 || math.Abs(s) > 1 {
			return 0
		}
		burning := pixelfx.Clamp(1.2 - math.Abs(x)/8)
		lane := pixelfx.Round(s * r * 0.85)
		run := pixelfx.Rand(math.Floor(dist/3), lane, S)
		if math.Abs(s) > rim || dist > ln-1 {
			if s < 0 && burning > 0.4 {
				return H[7]
			}
			return W[0]
		}
		if burning > 0.35 && lane < 2 {
			// charred: black, with embers breathing in the cracks along the grain
			if run < 0.55 && lane > -2 {
				g := 0.5 + 0.5*math.Sin((t/D)*math.Pi*6+pixelfx.Rand(math.Floor(dist/2), lane, S+1)*6.3)
				out.K = 0.3 * burning * g
				switch {
				case g > 0.72:
					return H[3]
				case g > 0.4:
					return H[4]
				}
				return H[5]
			}
			if lane < -1 {
				return W[1]
			}
			return W[0]
		}
		switch {
		case lane <= -2:
			if run < 0.25 {
				return W[2]
			}
			return pixelfx.Mix(W[3], H[4], 0.45*burning)
		case lane == -1:
			if run < 0.45 {
				return W[1]
			}
			return W[2]
		case lane == 0:
			if run < 0.2 {
				return W[1]
			}
			return W[2]
		}
		if run < 0.4 {
			return W[0]
		}
		return W[1]
	})
	// the near end's cut face: a dark rim, pale sapwood, growth rings round a dark heart
	if u0 <= 0 {
		p.EllipseFn(x0, y0, r*0.62, r+0.2, func(d, _ float64) (uint32, float64) {
			switch {
			case d > 0.78:
				return W[0], 1
			case d > 0.56:
				return W[5], 1
			case d > 0.32:
				return W[4], 1
			}
			return W[3], 1
		})
	}
}

// campfireCoals draws the bed of coals under the logs: coals that breathe in the heat, grey ash round the rim.
func campfireCoals(p *pixelfx.Painter, W, H []uint32, t, S float64) {
	const D = campfireDuration
	p.Shade(-15, -3, 15, 2, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x/14, (y+0.3)/2.2
		d := dx*dx + dy*dy
		if d > 1 {
			return 0
		}
		h := pixelfx.Rand(math.Floor(x/2), math.Floor(y), S+20)
		if d > 0.55 && h < 0.55 {
			switch {
			case h < 0.15:
				return campfireAsh[2]
			case h < 0.32:
				return campfireAsh[1]
			}
			return campfireAsh[0]
		}
		if h > 0.5 {
			g := (0.5 + 0.5*math.Sin((t/D)*math.Pi*4+h*40)) * (1.1 - d)
			out.K = 0.35 * g
			switch {
			case g > 0.7:
				return H[2]
			case g > 0.5:
				return H[3]
			case g > 0.3:
				return H[4]
			}
			return H[5]
		}
		if h > 0.3 {
			return W[1]
		}
		return W[0]
	})
}

type (
	campfireSmoke struct{ x, y, vx, vy, s float64 }
	campfireShed  struct {
		i                              int
		fire, sl, ox, oy, vx, vy, s, f float64
		smokes                         bool
	}
	campfirePuff struct{ x, y, k, s, f float64 }
	campfireLick struct {
		i         int
		vx, vy, s float64
	}
	campfireFlame struct{ x, y, vx, vy, s, f float64 }
	campfirePop   struct{ x, y, n, seed float64 }
	campfireEmber struct{ x, y, vx, vy, s, a, b float64 }
)

func newCampfire(o *pixelfx.Options) pixelfx.Instance {
	D := float64(campfireDuration)
	NL := noiseFor(D)
	F := pixelfx.Fit(o.Palette(), 9)
	H, W, S, HC := hotFirst(F), pixelfx.Fit(o.Ramp("wood"), 6), o.Seed(), o.Num("height")
	GLOW, SMOKE := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25), pixelfx.Mix(smoke1, H[6], 0.25)
	SPARKS, EMB := [4]uint32{H[1], H[2], H[4], H[5]}, [4]uint32{H[1], H[2], H[4], H[6]}
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		R := pixelfx.NewRng(S)
		N := func(rate, seed, off float64) float64 { return NL(t, rate, seed, off) }
		fla := func(tt float64) float64 { return 0.74 + 0.16*NL(tt, 7.3, S+1, 0) + 0.1*NL(tt, 19, S+2, 0) }
		fl := fla(t)

		T := make([]burnTongue, len(campfireTongues))
		for i, tg := range campfireTongues {
			dx := tg.dx + R.Range(-0.4, 0.4)
			h := 3 + HC*tg.h*R.Range(0.92, 1.08)
			w := tg.w * R.Range(0.9, 1.1)
			T[i] = burnTongue{dx: dx, y: tg.dy, h: h, w: w, back: tg.back, env: tg.h, n: (S*7 + float64(i)) * 13}
		}
		lean := func(tt float64) float64 { return NL(tt, 0.67, S+90, 0) * 0.12 }
		height := func(i int, tt float64) float64 {
			h, n := T[i].h, T[i].n
			leap := math.Max(0, NL(tt, 2.6, n+70, 0)-0.55) * 1.4
			return (h + 0.25*h*NL(tt, 3.3, n, 0) + h*leap) * (0.9 + 0.1*fla(tt))
		}
		sweep := func(i int, tt, h float64) float64 {
			return NL(tt, 2.2, T[i].n+40, 0)*(1+h*0.08) + lean(tt)*h*0.9 - pixelfx.ClampTo(T[i].dx*0.3, -2.5, 2.5)
		}
		tip := func(i int, tt float64) (float64, float64) {
			h := height(i, tt)
			return T[i].dx + sweep(i, tt, h)*0.85, T[i].y - h*0.88 + 3
		}
		tongueAt := func(i int) {
			tg := T[i]
			h := height(i, t)
			ls := layers
			switch {
			case tg.back:
				ls = backLayers
			case tg.w > 2.2:
				ls = bigLayers
			}
			flameTongue(p, tg.dx, tg.y, h, tg.w, sweep(i, t, h), N, tg.n, H, ls, 0.5, 1.1+h*0.04)
		}

		// smoke curling off the top, behind everything: each puff keeps the sway the plume had when it left the flames, so the wiggle climbs the plume, and the draught bends it over as it rises
		looped(t, D, 48, S+300, func(r *pixelfx.Rng, _ int) (campfireSmoke, float64) {
			var q campfireSmoke
			q.x = r.Range(-5, 5)
			q.y = -HC*0.8 - r.Range(0, 8)
			q.vx = r.Range(-2, 2)
			q.vy = -r.Range(10, 17)
			life := r.Range(2, 2.9)
			q.s = r.Range(2.4, 3.8)
			return q, life
		}, func(q *campfireSmoke, age, life float64, _ int) {
			x, y, _, _ := drift(q.x, q.y, q.vx, q.vy, age, 4, -20, 1.6, 1)
			smokePuff(p, x+NL(t-age, 0.8, S+60, 0)*(2+3*age), y, 1-age/life, q.s, SMOKE, false, 1)
		})
		// flame puffs shed from the tips: they burn out above the fire, some into smoke
		var puffs []campfirePuff
		looped(t, D, len(T)*24, S+200, func(r *pixelfx.Rng, j int) (campfireShed, float64) {
			q := campfireShed{i: j % len(T)}
			q.fire = r.Range(0.14, 0.3)
			q.smokes = r.Chance(0.45)
			q.sl = r.Range(0.8, 1.4)
			q.ox = r.Range(-1, 1)
			q.oy = r.Range(0, 4)
			q.vx = r.Range(-4, 4)
			q.vy = -r.Range(15, 30)
			q.s = r.Range(0.8, 1.6) * (0.8 + 0.6*T[q.i].env)
			q.f = r.Next()
			life := q.fire
			if q.smokes {
				life = q.fire + 1.4
			}
			return q, life
		}, func(q *campfireShed, age, _ float64, _ int) {
			tx, ty := tip(q.i, t-age)
			x, y, vx, vy := drift(tx+q.ox, ty+q.oy, q.vx, q.vy, math.Min(age, q.fire), 0, -95, 5, 3.2)
			if age < q.fire {
				puffs = append(puffs, campfirePuff{x, y, 1 - age/q.fire, q.s, q.f})
				return
			}
			if age-q.fire > q.sl {
				return
			}
			sx, sy, _, _ := drift(x, y, vx*0.6, vy*0.6-6, age-q.fire, 2, -16, 1.6, 1.2)
			smokePuff(p, sx, sy, 1-(age-q.fire)/q.sl, q.s*0.7, 0, false, 1)
		})

		campfireCoals(p, W, H, t, S)
		for _, L := range campfireLogs {
			campfireLogDraw(p, L, W, H, campfireCross, 1, t, S+11)
		}
		for i := range T {
			if T[i].back {
				tongueAt(i)
			}
		}
		// flames licking up off the burning wood
		looped(t, D, 90, S+210, func(r *pixelfx.Rng, _ int) (campfireFlame, float64) {
			var q campfireFlame
			q.x = r.Range(-10, 10)
			q.y = -r.Range(3, 8)
			q.vx = r.Range(-5, 5)
			q.vy = -r.Range(20, 40)
			life := r.Range(0.16, 0.3)
			q.s = r.Range(1, 1.8)
			q.f = r.Next()
			return q, life
		}, func(q *campfireFlame, age, life float64, _ int) {
			x, y, _, _ := drift(q.x, q.y, q.vx, q.vy, age, 0, -95, 5, 3.2)
			firePuff(p, x, y, 1-age/life, q.s, q.f, H, 0.18, 0, -1)
		})
		for i := range T {
			if !T[i].back {
				tongueAt(i)
			}
		}
		// licks torn off the tips, rising and shrinking
		looped(t, D, len(T)*12, S+220, func(r *pixelfx.Rng, j int) (campfireLick, float64) {
			q := campfireLick{i: j % len(T)}
			q.vx = r.Range(-3, 3)
			q.vy = -r.Range(12, 22)
			q.s = r.Range(0.9, 1.6) * (0.8 + 0.5*T[j%len(T)].env)
			return q, 0.26
		}, func(q *campfireLick, age, life float64, _ int) {
			tx, ty := tip(q.i, t-age)
			lick(p, tx+q.vx*age, ty-2+q.vy*age, 1-age/life, q.s, H)
		})
		for _, q := range puffs {
			firePuff(p, q.x, q.y, q.k, q.s, q.f, H, 0.18, 0, -1)
		}
		for _, L := range campfireLogs {
			campfireLogDraw(p, L, W, H, 0, campfireCross, t, S+11)
		}

		// pops: a crack, a flash, and a fistful of sparks thrown up out of the wood that bounce on the floor
		looped(t, D, 4, S+400, func(r *pixelfx.Rng, _ int) (campfirePop, float64) {
			var q campfirePop
			q.x = r.Range(-9, 9)
			q.y = -r.Range(4, 9)
			q.n = r.Int(3, 5)
			q.seed = r.Int(0, 1e6)
			return q, 0.7
		}, func(q *campfirePop, age, _ float64, _ int) {
			if age < 0.08 {
				p.Px(q.x, q.y, pixelfx.White)
				p.Glow(q.x, q.y, 12, H[1], 0.7*(1-age/0.08))
			}
			r := pixelfx.NewRng(q.seed)
			for range int(q.n) {
				a, sp, life := -pi/2+r.Range(-0.8, 0.8), r.Range(110, 220), r.Range(0.25, 0.6)
				if age >= life {
					continue
				}
				x, y, vx, vy := sparkAt(q.x, q.y, math.Cos(a)*sp, math.Sin(a)*sp, age, 0, 427, 2.2)
				spark(p, x, y, vx, vy, 1-age/life, SPARKS)
			}
		})
		// embers carried up by the heat, wandering and cooling
		looped(t, D, 36, S+500, func(r *pixelfx.Rng, _ int) (campfireEmber, float64) {
			var q campfireEmber
			q.x = r.Range(-7, 7)
			q.y = -r.Range(8, 18)
			q.vx = r.Range(-15, 15)
			q.vy = -r.Range(25, 60)
			life := r.Range(1.1, 2.5)
			if r.Chance(0.15) {
				q.s = 2
			} else {
				q.s = r.Range(1, 1.6)
			}
			q.a = r.Range(0, 6.3)
			q.b = r.Range(0, 6.3)
			return q, life
		}, func(q *campfireEmber, age, life float64, _ int) {
			x, y, _, _ := drift(q.x, q.y, q.vx, q.vy, age, 0, -30, 1.6, 1.6)
			ember(p, x+7*(math.Sin(age*2.3+q.a)-math.Sin(q.a)), y+2*(math.Sin(age*3+q.b)-math.Sin(q.b)), 1-age/life, q.s, EMB, age*40+q.a)
		})

		// light: a big warm room light, a hot heart, a pool on the floor, all breathing with the flames
		br := 0.92 + 0.08*N(0.67, S+3, 0)
		lx := lean(t) * 12
		p.Glow(lx, -8, 74*br*(0.9+0.1*fl), GLOW, 0.3*fl)
		for _, l := range [3][3]float64{{-6, -17, 0.85}, {0, -21, 1}, {6, -17, 0.85}} {
			x, y, k := l[0], l[1], l[2]
			p.Glow(x+lx, y, 46*br, GLOW, 0.3*k*fl)
			p.Glow(x*0.6, y+13, 15, H[2], 0.16*k*fl)
		}
		p.Glow(0, -3, 9, H[0], 0.16*fl)
		floorLight(p, 0, 0.5, 48, 8, GLOW, 0.3*fl)
	}}
}
