// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/meteor.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the impact's particle recipe (fire puffs that cool into smoke, sparks, embers, burning brands that land and keep burning, rocks, grit, dust, scorch) from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Meteor is a burning rock streaking in from the upper left in a comet of fire and smashing into the floor: a white flash, a fireball, rocks and brands thrown, a glowing crater that cools. It is registered as "fire/meteor".
var Meteor = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Meteor",
		Desc:     "A burning rock streaks in from the upper left in a comet of fire and smashes into the floor: a white flash, a fireball, rocks and brands thrown, a glowing crater that cools.",
		Tags:     []string{"fire", "meteor", "impact", "explosion", "spell", "debris", "one-shot"},
		Size:     [2]int{272, 232},
		Origin:   [2]int{150, 208},
		Duration: meteorDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "stone", Default: pixelfx.Color("#4a3f69")},
			{Name: "rock", Default: pixelfx.Palette("#120e16", "#2a2230", "#463a4a", "#6a5a66")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newMeteor,
}

func init() { pixelfx.Register("fire/meteor", Meteor) }

const meteorDuration = 5

var (
	// meteorHit is when the meteor lands; a variable so sums with it round as pixelfx's do.
	meteorHit = 0.62
	// meteorFrom is where it comes from, relative to where it lands.
	meteorFrom = [2]float64{-112, -142}
	// meteorComet is the comet's tongues of flame streaming back off the rock: length, half-width, offset across.
	meteorComet = [4][3]float64{{36, 7, 0}, {26, 7.5, -2.5}, {26, 7.5, 2.5}, {16, 8, 0}}
)

func newMeteor(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 9)
	H, RK, st, R := hotFirst(F), pixelfx.Fit(o.Ramp("rock"), 4), stoneOf(o.Color("stone")), pixelfx.NewRng(o.Seed())
	D, HIT, S := float64(meteorDuration), meteorHit, o.Seed()*11
	floor := func(float64) float64 { return 0 }
	var blazes *pixelfx.Blazes
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{H[1], H[2], H[4], H[5]}, OnLand: func(x, y float64) {
		blazes.Add(x, y-0.25, R.Range(0.9, 1.6), R.Range(0.5, 0.85))
	}})
	blazes = pixelfx.NewBlazes(ps)
	bits, lights, GLOW := pixelfx.NewBits(floor), &pixelfx.Lights{}, pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25)
	FX, FY := meteorFrom[0], meteorFrom[1]
	ln := pixelfx.Hypot(FX, FY)
	ux, uy := -FX/ln, -FY/ln
	// the flight: speeding up a touch as it falls
	pos := func(tt float64) (float64, float64) {
		k := pixelfx.Ease.InQuad(pixelfx.Clamp(tt/HIT))*0.35 + pixelfx.Clamp(tt/HIT)*0.65
		return FX * (1 - k), FY * (1 - k)
	}
	t, hit := 0.0, false
	lastX, lastY := pos(0)

	// the meteor: a lumpy rock, white-hot on its leading face, cracks glowing over the dark back, an outline
	meteor := func(p *pixelfx.Painter, x, y, R0, cool, alpha float64) {
		p.Shade(x-R0-2, y-R0-2, x+R0+2, y+R0+2, func(px, py float64, out *pixelfx.Out) uint32 {
			out.A = alpha
			dx, dy := px-x, py-y
			d, a := pixelfx.Hypot(dx, dy), math.Atan2(dy, dx)
			rr := R0 * (1 + 0.18*pixelfx.Noise(a*2.2+3, S) + 0.1*pixelfx.Noise(a*5, S+1))
			if d > rr+1 {
				return 0
			}
			if d > rr {
				return outline
			}
			face := (dx*ux + dy*uy) / math.Max(1, d)
			heat := (1 - cool) * (face*0.6 + 0.5 - (d/rr)*0.25)
			if heat > 0.62 {
				switch {
				case heat > 0.85:
					return H[0]
				case heat > 0.74:
					return H[1]
				}
				return H[2]
			}
			if heat > 0.42 {
				return H[4]
			}
			h := pixelfx.Rand(math.Floor(px-x), math.Floor(py-y), S+3)
			if h < 0.22*(1-cool*0.7) {
				if 1-cool > 0.5 {
					return H[4]
				}
				return H[6]
			}
			switch {
			case dx*-0.6+dy*-0.8 > 0:
				return RK[2]
			case d/rr > 0.6:
				return RK[0]
			}
			return RK[1]
		})
	}
	// the crater: hot rock pooled in the dent cooling to soot, a lip of broken floor
	crater := func(p *pixelfx.Painter) {
		if t < HIT {
			return
		}
		a, end := t-HIT, 1-pixelfx.Smooth(D-1, D-0.15, t)
		rx, ry := 36*math.Min(1, a/0.08), 6.0
		for y := -1.0; y <= 3; y++ {
			for x := math.Floor(-rx); x <= math.Ceil(rx); x++ {
				dx, dy := (x+0.5)/rx, (y+0.5)/ry
				d := math.Sqrt(dx*dx + dy*dy)
				if d > 1 {
					continue
				}
				k := pixelfx.Rand(x*31+y, S+5)
				T := math.Pow(pixelfx.Clamp(1-a/2.6), 1.3)*(1.25-d) - k*0.35
				if T > 0.06 {
					c := H[int(math.Min(8, math.Floor((1-math.Min(1, T))*8)))]
					p.Dot(x, y, c, end)
					if pixelfx.ToInt32(x)&3 == 0 {
						p.Glow(x+0.5, y, 7, c, 0.2*T*end)
					}
					continue
				}
				c, al := soot2, 1-d
				if d < 0.55 {
					c = soot
				}
				if d < 0.95-k*0.3 {
					al = 1
				}
				p.Dot(x, y, c, al*end)
			}
		}
		// the lip: broken floor heaped round the edge, lit by the glow inside
		for x := -40.0; x <= 40; x++ {
			e, hh := math.Abs(x)/40, 0.0
			if e >= 0.7 {
				hh = pixelfx.Round(2.4 * math.Sin(((e-0.7)/0.3)*math.Pi) * (0.6 + 0.4*pixelfx.Rand(x, S+7)))
			}
			for j := 0.0; j < hh; j++ {
				c := st.shade
				if j == hh-1 {
					c = pixelfx.Mix(st.light, H[4], 0.5*pixelfx.Clamp(1-a/2))
				}
				p.Dot(x, -1-j, c, end)
			}
			if hh != 0 {
				p.Dot(x, 0, outline, end)
			}
		}
	}
	bandRamp, flareRamp := []uint32{st.body, st.light, H[4], H[1]}, []uint32{H[6], H[4], H[2], H[1]}
	gritCols := []uint32{st.light, st.body, st.body, st.shade}
	chunkRamp := []uint32{outline, st.shade, st.body, st.light, pixelfx.Mix(st.light, pixelfx.White, 0.3)}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			switch {
			case t < HIT:
				// the flight: a comet of fire shed behind, smoke, sparks, embers
				x, y := pos(t)
				vx, vy := (x-lastX)/dt, (y-lastY)/dt
				lastX, lastY = x, y
				for range 3 {
					if !R.Chance(0.8 * dt * 60) {
						continue
					}
					px := x - ux*R.Range(2, 8) + R.Range(-3, 3)
					py := y - uy*R.Range(2, 8) + R.Range(-3, 3)
					pvx, pvy, life, size := vx*0.15+R.Range(-20, 20), vy*0.15+R.Range(-30, 10), R.Range(0.3, 0.6), R.Range(3, 6)
					var flag pixelfx.Flag
					if R.Chance(0.65) {
						flag = pixelfx.Smokes | pixelfx.Big
					}
					ps.Spawn(pixelfx.Fire, px, py, pvx, pvy, life, 0, size, 0, flag)
				}
				if R.Chance(0.6 * dt * 60) {
					ps.Spawn(pixelfx.Spark, x, y, vx*0.35+R.Range(-80, 80), vy*0.35+R.Range(-80, 40), R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(0.3 * dt * 60) {
					pvx, pvy, life := vx*0.1+R.Range(-30, 30), vy*0.1+R.Range(-40, 0), R.Range(0.8, 1.6)
					size := 1.0
					if R.Chance(0.2) {
						size = 2
					}
					ps.Spawn(pixelfx.Ember, x, y, pvx, pvy, life, H[3], size, 0, 0)
				}
			case !hit:
				hit = true
				// the impact: flash, fireball thrown up and forward with the rock's momentum, a column, smoke, sparks
				lights.Add(0, -40, 100, warm, 0.95, 0.9)
				lights.Add(0, -20, 46, pixelfx.White, 0.9, 0.12)
				for range 34 {
					a, sp := -pi/2+R.Range(-1.3, 1.3)+0.25, R.Range(50, 210)
					x, y, life, size := R.Range(-6, 6), -R.Range(2, 10), R.Range(0.5, 1.1), R.Range(5, 9)
					var flag pixelfx.Flag
					if R.Chance(0.5) {
						flag = pixelfx.Smokes | pixelfx.Big
					}
					ps.Spawn(pixelfx.Fire, x, y, math.Cos(a)*sp, math.Sin(a)*sp*0.75-30, life, 0, size, 0, flag)
				}
				for range 9 {
					ps.Spawn(pixelfx.Fire, R.Range(-6, 6), -6, R.Range(-14, 14), -R.Range(130, 220), R.Range(0.8, 1.2), 0, R.Range(6, 8.5), R.Range(0, 0.12), pixelfx.Smokes|pixelfx.Big)
				}
				for range 20 {
					a, d := R.Range(-pi, 0), R.Range(4, 18)
					ps.Spawn(pixelfx.Smoke, math.Cos(a)*d, -R.Range(6, 24), math.Cos(a)*R.Range(10, 40), -R.Range(30, 110), R.Range(1.8, 2.8), 0, R.Range(8, 12), R.Range(0.15, 0.5), pixelfx.Big)
				}
				for range 16 {
					s := R.Sign()
					ps.Spawn(pixelfx.Smoke, s*R.Range(4, 18), -R.Range(1, 5), s*R.Range(60, 130)+10, -R.Range(2, 12), R.Range(1.2, 2.0), st.dust, R.Range(5, 8), R.Range(0, 0.06), pixelfx.Big)
				}
				for range 90 {
					a, sp := -pi/2+R.Range(-1.3, 1.3)+0.15, R.Range(100, 300)
					ps.Spawn(pixelfx.Spark, 0, -4, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.7), pixelfx.White, 1, 0, 0)
				}
				for range 36 {
					a, sp := R.Range(0, tau), R.Range(40, 150)
					ps.Spawn(pixelfx.Ember, math.Cos(a)*8, -8+math.Sin(a)*6, math.Cos(a)*sp, math.Sin(a)*sp-70, R.Range(1.2, 2.6), H[3], 1, 0, 0)
				}
				for range 10 {
					a, sp := -pi/2+R.Range(-1, 1.05), R.Range(130, 240)
					ps.Spawn(pixelfx.Brand, 0, -6, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.7, 1.3), pixelfx.White, 1, 0, 0)
				}
				for range 28 {
					bits.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: R.Range(-8, 8), Y: -3, VX: R.Range(-80, 110), VY: -R.Range(140, 340), G: 1, Drag: 0.4, Bounce: 0.3, Life: R.Range(1.8, 2.8), A: R.Range(0, tau), VA: R.Range(-16, 16), S: R.Int(1, 3), Ramp: chunkRamp})
				}
				for range 80 {
					u := R.Range(-1, 1)
					a, sp := -pi/2+u*0.7+0.15, R.Range(90, 270)*(1-0.4*math.Abs(u))
					life, c := R.Range(1.2, 2.2), pixelfx.Pick(R, gritCols)
					size := 1.0
					if R.Chance(0.45) {
						size = 2
					}
					ps.Spawn(pixelfx.Grit, u*6, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, c, size, 0, 0)
				}
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 3 {
						ps.Spawn(pixelfx.Dust, b.X+R.Range(-1.5, 1.5), -0.5, R.Sign()*R.Range(8, 35), -R.Range(2, 18), R.Range(0.3, 0.6), st.dust, R.Range(1.3, 2.6), 0, 0)
					}
				}
			}
			blazes.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			crater(p)
			simDraw(p, ps, H, pixelfx.LayerBack)
			blazes.DrawBack(p)
			if k := (t - HIT) / 0.5; k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0.5, 110*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 5*(1-k)), 1.15-k, bandRamp, farHalf, false)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if k := (t - HIT) / 0.5; k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0.5, 110*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 5*(1-k)), 1.15-k, bandRamp, nearHalf, k < 0.12)
			}
			for i := range bits.List {
				rock(p, &bits.List[i], st)
			}
			if t < HIT {
				// the comet: tongues of flame streaming back off the rock, the rock, its light; the floor lights up below
				x, y := pos(t)
				NZ := func(rate, seed, off float64) float64 { return pixelfx.Noise(t*rate+off, seed) }
				floorLight(p, 0, 0.5, 30+40*(t/HIT), 7, GLOW, 0.5*math.Pow(t/HIT, 2))
				for i, c := range meteorComet {
					L, w, off := c[0], c[1], c[2]
					n := (S + float64(i)) * 13
					flameBlade(p, x-uy*off, y+ux*off, -ux, -uy, L*(0.85+0.25*NZ(5, n, 0)), w, NZ(3, n+3, 0)*3, NZ, n, H, layers, 0.5, 1.6)
				}
				meteor(p, x, y, 6.5, 0, 1)
				p.Glow(x, y, 46, GLOW, 0.55)
				p.Glow(x, y, 12, H[1], 0.4)
			} else {
				// the core of the meteor sits half-buried in the crater, cooling
				a := t - HIT
				cool, end := pixelfx.Clamp(a/2.8), 1-pixelfx.Smooth(D-1.1, D-0.2, t)
				if end > 0 {
					meteor(p, 0, 0, 5.5, cool, end)
					p.Glow(0, -2, 22, GLOW, 0.4*(1-cool)*end)
				}
			}
			simDraw(p, ps, H, pixelfx.LayerFront)
			// the hero frame: a white-hot flash and a flare of rays
			if f := (t - HIT) / 0.08; f > 0 && f < 1 {
				p.Circle(0, -6, 11*(1-f*0.3)+1.5, H[3], 1)
				c1, c2 := H[2], H[1]
				if f < 0.5 {
					c1, c2 = H[1], pixelfx.White
				}
				p.Circle(0, -6, 11*(1-f*0.3), c1, 1)
				p.Circle(0, -6, 7*(1-f*0.3), c2, 1)
			}
			if fr := (t - HIT) / 0.24; fr > 0 && fr < 1 {
				pixelfx.Flare(p, 0, -6, 44*math.Pow(1-fr, 1.4), 1-fr, flareRamp)
			}
			lights.Draw(p)
		},
	}
}
