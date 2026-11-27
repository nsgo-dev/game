// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/small-explosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the particle recipe (fire puffs that cool into smoke, sparks, embers, burning brands, grit, rocks, scorch) from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// SmallExplosion is a ground blast: a white flash and shock rings, fire that cools into a rolling smoke column, sparks, burning brands that land and keep burning, tumbling rocks and a glowing scorch. It is registered as "explosions/small-explosion".
var SmallExplosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Explosion",
		Desc:     "A ground blast: a white flash and shock rings, fire that cools into a rolling smoke column, sparks, burning brands that land and keep burning, tumbling rocks and a glowing scorch.",
		Tags:     []string{"explosion", "fire", "smoke", "debris", "impact", "one-shot"},
		Size:     [2]int{300, 240},
		Origin:   [2]int{150, 196},
		Duration: smallDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "size", Default: pixelfx.Num(1)},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	New: newSmallExplosion,
}

func init() { pixelfx.Register("explosions/small-explosion", SmallExplosion) }

const smallDuration = 5.4

var (
	soot, soot2, outline = pixelfx.Hex("#0d0a12"), pixelfx.Hex("#17121d"), pixelfx.Hex("#1a1422")
	warm                 = pixelfx.RGB(255, 176, 96)
)

type stoneCols struct{ light, body, shade, dust uint32 }

func stoneOf(c uint32) stoneCols {
	return stoneCols{
		light: pixelfx.Mix(pixelfx.Scale(c, 1.35), pixelfx.White, 0.08),
		body:  c,
		shade: pixelfx.Scale(c, 0.64),
		dust:  pixelfx.Mix(pixelfx.Scale(c, 1.2), pixelfx.RGB(150, 140, 150), 0.3),
	}
}

// smallScorch draws the floor under the blast: white-hot, cooling through the fire ramp to soot.
func smallScorch(p *pixelfx.Painter, F []uint32, t, rx0, cool, fade float64) {
	if fade <= 0 {
		return
	}
	rx, ry := rx0*math.Min(1, t/0.08), 4.0
	heat := slices.Clone(F)
	slices.Reverse(heat)
	for y := 0.0; y <= 2; y++ {
		for x := math.Floor(-rx); x <= math.Ceil(rx); x++ {
			dx, dy := (x+0.5)/rx, (y+0.5)/ry
			d := math.Sqrt(dx*dx + dy*dy)
			if d > 1 {
				continue
			}
			k := pixelfx.Rand(x*31+y, 3)
			T := math.Pow(pixelfx.Clamp(1-t/cool), 1.4)*(1.25-d) - k*0.35
			if T > 0.06 {
				c := heat[int(math.Min(float64(len(heat)-1), math.Floor((1-math.Min(1, T))*float64(len(heat)-1))))]
				p.Dot(x, y, c, fade)
				if pixelfx.ToInt32(x)&3 == 0 {
					p.Glow(x+0.5, y, 6, c, 0.18*T*fade)
				}
				continue
			}
			c, a := soot2, 1-d
			if d < 0.5 {
				c = soot
			}
			if d < 0.95-k*0.3 {
				a = 1
			}
			p.Dot(x, y, c, a*fade)
		}
	}
}

// smallRock draws a rock: a lit side, a shaded side, an outline, spinning.
func smallRock(p *pixelfx.Painter, b *pixelfx.Bit, st stoneCols) {
	r, fade := 0.6+b.S*0.75, math.Min(1, b.Life/0.5)
	lx, ly := math.Cos(b.A)*r*0.4, math.Sin(b.A)*r*0.4
	p.Shade(b.X-r-1, b.Y-r-1, b.X+r+1, b.Y+r+1, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-b.X, y-b.Y
		d := pixelfx.Hypot(dx*1.15, dy)
		if d > r+0.9 {
			return 0
		}
		lit := dx*lx + dy*ly
		switch {
		case d > r:
			out.C = outline
		case lit < -0.4:
			out.C = st.light
		case lit > 0.6:
			out.C = st.shade
		default:
			out.C = st.body
		}
		out.A = fade
		return 0
	})
}

type flash struct{ r, life, max float64 }

type shockRing struct {
	r0, r1, life, max, thick float64
	c                        uint32
}

func farHalf(g float64) bool  { return math.Sin(g) < 0 }
func nearHalf(g float64) bool { return math.Sin(g) >= 0 }

func newSmallExplosion(o *pixelfx.Options) pixelfx.Instance {
	F, S := pixelfx.Fit(o.Palette(), 8), o.Num("size")
	RAD, POWER, EARTH := 18*S, 0.9*math.Pow(S, 0.8), earth
	st := stoneOf(o.Color("stone"))
	D := float64(smallDuration)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	var blazes *pixelfx.Blazes
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[6], F[5], F[3], F[2]}, OnLand: func(x, y float64) {
		blazes.Add(x, y-0.25, R.Range(0.9, 1.5), R.Range(0.55, 0.85))
	}})
	blazes = pixelfx.NewBlazes(ps)
	rocks, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	Y := -RAD * 0.3
	t := 0.0
	cos, sin := math.Cos, math.Sin

	// the fireball
	flashes := []flash{{RAD * 1.05, 0.06, 0.06}}
	rings := []shockRing{{RAD * 0.45, RAD * 2.7, 0.34, 0.34, 3, F[5]}, {RAD * 0.3, RAD * 1.6, 0.22, 0.22, 1.8, F[3]}}
	lights.Add(0, Y, RAD*3.4, warm, 0.6*POWER, 0.7)
	lights.Add(0, Y, RAD*1.6, pixelfx.White, 0.6, 0.08)
	for range 5 {
		ps.Spawn(pixelfx.Fire, R.Range(-2.5, 2.5), Y+R.Range(-2.5, 2.5), R.Range(-20, 20), R.Range(-40, -10), R.Range(0.18, 0.3), 0, R.Range(RAD*0.4, RAD*0.55), 0, 0)
	}
	for i := 0; float64(i) < pixelfx.Round(28*POWER); i++ {
		a, d, sp := R.Range(0, pixelfx.Tau), R.Range(0, RAD*0.55), R.Range(40, 210)*POWER
		ps.Spawn(pixelfx.Fire, cos(a)*d, Y+sin(a)*d, cos(a)*sp, sin(a)*sp*0.8-40, R.Range(0.55, 1.15), 0, R.Range(RAD*0.22, RAD*0.46), 0, chance(R, 0.45, pixelfx.Smokes|pixelfx.Big))
	}
	for range 5 {
		a, d := R.Range(0, pixelfx.Tau), R.Range(RAD*0.1, RAD*0.5)
		ps.Spawn(pixelfx.Smoke, cos(a)*d, Y+sin(a)*d, cos(a)*30, sin(a)*20-30, R.Range(0.9, 1.5), pixelfx.RGB(34, 29, 38), R.Range(RAD*0.15, RAD*0.26), R.Range(0.06, 0.2), pixelfx.Big)
	}
	for range 5 {
		ps.Spawn(pixelfx.Fire, R.Range(-RAD*0.25, RAD*0.25), Y, R.Range(-12, 12), R.Range(-180, -100), R.Range(0.8, 1.3), 0, R.Range(RAD*0.24, RAD*0.36), R.Range(0, 0.1), chance(R, 0.6, pixelfx.Smokes|pixelfx.Big))
	}
	for i := 0; float64(i) < pixelfx.Round(10*POWER); i++ {
		a, d, sp := R.Range(0, pixelfx.Tau), R.Range(0, RAD*0.7), R.Range(15, 100)
		ps.Spawn(pixelfx.Smoke, cos(a)*d, Y+sin(a)*d, cos(a)*sp, sin(a)*sp*0.6-35, R.Range(1.8, 2.8), 0, R.Range(RAD*0.26, RAD*0.44), R.Range(0.12, 0.45), pixelfx.Big)
	}
	for i := 0; float64(i) < pixelfx.Round(80*POWER); i++ {
		a, sp := R.Range(0, pixelfx.Tau), R.Range(140, 380)*POWER
		ps.Spawn(pixelfx.Spark, 0, Y, cos(a)*sp, sin(a)*sp-70, R.Range(0.18, 0.6), pixelfx.White, 1, 0, 0)
	}
	for i := 0; float64(i) < pixelfx.Round(40*POWER); i++ {
		a, sp := R.Range(0, pixelfx.Tau), R.Range(30, 150)
		ps.Spawn(pixelfx.Ember, cos(a)*RAD*0.4, Y+sin(a)*RAD*0.4, cos(a)*sp, sin(a)*sp-75, R.Range(1.2, 2.6), F[3], 1, 0, 0)
	}
	// the condition draws again on every pass, as pixelfx's loop does
	for i := 0; float64(i) < pixelfx.Round(R.Range(9, 14)*math.Max(0, POWER-0.55)); i++ {
		a, sp := -math.Pi/2+R.Range(-1.35, 1.35), R.Range(170, 330)
		ps.Spawn(pixelfx.Brand, 0, Y, cos(a)*sp, sin(a)*sp, R.Range(0.7, 1.3), pixelfx.White, 1, 0, 0)
	}
	// the ground's part: dust rolling out along the floor, rocks and grit thrown up, a dust column
	for i := 0; float64(i) < pixelfx.Round(RAD*0.5); i++ {
		s := R.Sign()
		ps.Spawn(pixelfx.Smoke, s*R.Range(0, RAD*0.6), -R.Range(1, 5+RAD*0.04), s*R.Range(3, 9.5)*RAD, -R.Range(2, 15), R.Range(1.1, 2.0), st.dust, R.Range(4, 7)+RAD/6, R.Range(0, 0.06), pixelfx.Big)
	}
	for i := 0; float64(i) < 26*math.Min(1.5, S); i++ {
		var b pixelfx.Bit
		b.Kind = pixelfx.Chunk
		b.X = R.Range(-0.4, 0.4) * RAD
		b.Y = -3
		b.VX = R.Range(-9, 9) * RAD * 0.25
		b.VY = -R.Range(7, 16) * RAD * 0.9
		b.G, b.Drag, b.Bounce = 1, 0.4, 0.3
		b.Life = R.Range(1.8, 2.6)
		b.A = R.Range(0, pixelfx.Tau)
		b.VA = R.Range(-16, 16)
		b.S = R.Int(1, 3)
		rocks.Add(b)
	}
	for i := 0; float64(i) < pixelfx.Round(EARTH*RAD*4); i++ {
		u := R.Range(-1, 1)
		a, sp := -math.Pi/2+u*0.5, R.Range(5, 15)*RAD*(1-0.45*math.Abs(u))
		size := 1.0
		ps.Spawn(pixelfx.Grit, u*0.25*RAD, -1, cos(a)*sp, sin(a)*sp, R.Range(1.2, 2.2), pixelfx.Pick(R, []uint32{st.light, st.body, st.body, st.shade}), pick(R.Chance(0.45), 2, size), 0, 0)
	}
	for i := 0; float64(i) < pixelfx.Round(EARTH*12); i++ {
		ps.Spawn(pixelfx.Smoke, R.Range(-0.3, 0.3)*RAD, -R.Range(2, RAD*0.6), R.Range(-15, 15), -R.Range(60, 190)*EARTH*0.6, R.Range(1.6, 2.6), st.dust, R.Range(5, 9)*math.Sqrt(S)+RAD/7, R.Range(0, 0.15), pixelfx.Big)
	}

	band := func(p *pixelfx.Painter, keep func(float64) bool) {
		if k := t / 0.5; k < 1 {
			pixelfx.Band(p, 0, 0.5, 80*S*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, (2+RAD/9)*(1-k)), 1.15-k, []uint32{st.body, st.light, F[3], F[6]}, keep, false)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := len(flashes) - 1; i >= 0; i-- {
				if flashes[i].life -= dt; flashes[i].life <= 0 {
					flashes = slices.Delete(flashes, i, i+1)
				}
			}
			for i := len(rings) - 1; i >= 0; i-- {
				if rings[i].life -= dt; rings[i].life <= 0 {
					rings = slices.Delete(rings, i, i+1)
				}
			}
			rocks.Update(dt)
			// rocks that hit hard kick up dust
			for i := range rocks.List {
				b := &rocks.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 3 {
						x := b.X + R.Range(-1.5, 1.5)
						vx := R.Sign() * R.Range(10, 55) * 0.6
						ps.Spawn(pixelfx.Dust, x, -0.5, vx, -R.Range(2, 22), R.Range(0.3, 0.65), st.dust, R.Range(1.3, 2.8), 0, 0)
					}
				}
			}
			blazes.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			smallScorch(p, F, t, 22*S, 1.2+RAD/14, 1-pixelfx.Smooth(D-1.2, D-0.2, t))
			ps.Draw(p, pixelfx.LayerBack)
			blazes.DrawBack(p)
			band(p, farHalf)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			band(p, nearHalf)
			for i := range rocks.List {
				smallRock(p, &rocks.List[i], st)
			}
			ps.Draw(p, pixelfx.LayerFront)
			for _, f := range flashes {
				k2 := f.life / f.max
				r := f.r * (0.8 + 0.2*k2)
				p.Circle(0, Y, r+1.2, F[2], 1)
				p.Circle(0, Y, r, pick(k2 > 0.45, F[6], F[5]), 1)
				p.Circle(0, Y, r*0.6, pick(k2 > 0.45, pixelfx.White, F[6]), 1)
			}
			for _, r := range rings {
				k2 := r.life / r.max
				e := 1 - math.Pow(k2, 3)
				p.Ring(0, Y, r.r0+(r.r1-r.r0)*e, math.Max(1, r.thick*k2+0.4), r.c, math.Min(1, k2*1.6))
			}
			lights.Draw(p)
		},
	}
}

// earth is how much ground the blast throws up, a variable so products with it round as pixelfx's do.
var earth = 1.6

func chance(R *pixelfx.Rng, p float64, f pixelfx.Flag) pixelfx.Flag {
	if R.Chance(p) {
		return f
	}
	return 0
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
