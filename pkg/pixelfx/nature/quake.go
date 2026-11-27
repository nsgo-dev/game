// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/quake.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the ground's part (grit thrown up that bounces and settles, stones that tumble and kick up dust where they land, dust rolling out along the floor and climbing) from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Quake is three ground jolts that tear a glowing rift along the floor, heave up slabs and shower pebbles and dust. It is registered as "nature/quake".
var Quake = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Quake",
		Desc:     "The ground jolts three times: a rift tears out along the floor glowing deep inside, slabs of floor heave up and drop back askew, pebbles hop and dust bursts out of the split.",
		Tags:     []string{"nature", "earth", "quake", "rift", "aoe", "ground", "one-shot"},
		Size:     [2]int{288, 104},
		Origin:   [2]int{144, 88},
		Duration: quakeDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e0c14", "#1a1724", "#211d2d", "#2b2639", "#3a3450", "#4a4263", "#6a6088", "#8e86a8")},
			{Name: "glow", Default: pixelfx.Palette("#4a1a0c", "#a8401a", "#ff8a2a", "#ffd27a", "#fff4d0")},
			{Name: "reach", Default: pixelfx.Num(118)},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	New: newQuake,
}

func init() { pixelfx.Register("nature/quake", Quake) }

const quakeDuration = 3.1

var (
	quakeJolts = [3]float64{0.06, 0.5, 0.92}
	quakeClose = 2.2
)

type quakeSlab struct {
	x, y, vy, a, va float64
	rest            bool
	s, side         float64
	pts             [][2]float64
}

func newQuake(o *pixelfx.Options) pixelfx.Instance {
	S := pixelfx.Fit(o.Palette(), 8)
	G := pixelfx.Fit(o.Ramp("glow"), 5)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	REACH := o.Num("reach")
	DUST := pixelfx.Mix(S[6], pixelfx.Hex("#8c8494"), 0.4)
	DUST2 := pixelfx.Mix(S[5], pixelfx.Hex("#6c6474"), 0.35)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{G[4], G[3], G[2], G[1]}})
	bits := pixelfx.NewBits(floor)
	lights := &pixelfx.Lights{}
	// the rift: a jagged line out both ways, its depth into the floor at each step
	var rift [][2]float64
	for x := -REACH; x <= REACH; x += R.Range(2.5, 5) {
		rift = append(rift, [2]float64{x, R.Int(0, 2)})
	}
	var slabs []quakeSlab
	for x := -REACH + 8; x < REACH-6; x += R.Range(12, 17) {
		sx := x + R.Range(-2, 2)
		ss := R.Range(4.5, 6)
		side := R.Sign()
		slabs = append(slabs, quakeSlab{
			x: sx, rest: true, s: ss, side: side,
			pts: [][2]float64{{-1, -0.38}, {1, -0.42}, {1.02, 0.4}, {-0.98, 0.38}},
		})
	}
	t := 0.0
	j := 0
	open := func() float64 {
		return pixelfx.Ease.OutCubic(pixelfx.Clamp((t-quakeJolts[0])/0.45)) * REACH * (0.55 + 0.45*pixelfx.Smooth(quakeJolts[1], quakeJolts[1]+0.3, t))
	}
	gap := func(x float64) float64 {
		return (1 - pixelfx.Smooth(quakeClose, quakeClose+0.5, t)) * (1 + 2.6*(1-math.Abs(x)/REACH)) * (0.6 + 0.4*pixelfx.Smooth(quakeJolts[1], quakeJolts[2]+0.2, t))
	}
	heat := func() float64 {
		return pixelfx.Smooth(0, 0.3, t) * (1 - pixelfx.Smooth(1.4, quakeClose+0.3, t)) * (0.8 + 0.2*math.Sin(t*17))
	}
	shake := func() float64 {
		s := 0.0
		for i, tj := range quakeJolts {
			if t > tj {
				s += math.Exp(-(t-tj)*9) * math.Sin((t-tj)*80) * (1.6 - float64(i)*0.3)
			}
		}
		return s
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			for j < len(quakeJolts) && t >= quakeJolts[j] {
				k := 1 - float64(j)*0.2
				L := open() + 10
				j++
				lights.Add(0, -2, 60, G[2], 0.5*k, 0.5)
				lights.Add(0, -2, 26, pixelfx.White, 0.4*k, 0.07)
				// the floor heaves: slabs along the split jump and twist, pebbles and stones hop, dust bursts out
				for i := range slabs {
					s := &slabs[i]
					d := math.Abs(s.x) / REACH
					if math.Abs(s.x) > L || R.Chance(0.25+d*0.3) {
						continue
					}
					s.rest = false
					s.vy = -R.Range(70, 170) * k * (1 - d*0.5)
					s.va = s.side * R.Range(3, 8)
				}
				for i := 0.0; i < 70*k; i++ {
					x := R.Range(-L, L)
					a := -pi/2 + R.Range(-0.6, 0.6)
					sp := R.Range(40, 200) * k
					life := R.Range(0.6, 1.2)
					c := pixelfx.Pick(R, []uint32{S[7], S[6], S[5], S[4]})
					sz := 1.0
					if R.Chance(0.35) {
						sz = 2
					}
					ps.Spawn(pixelfx.Grit, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, c, sz, 0, 0)
				}
				for i := 0.0; i < 8*k; i++ {
					x := R.Range(-L, L) * 0.8
					a := -pi/2 + R.Range(-0.9, 0.9)
					sp := R.Range(100, 230) * k
					life := math.Min(R.Range(1.1, 1.6), quakeDuration-0.1-t)
					ang := R.Range(0, tau)
					va := R.Range(-14, 14)
					sz := R.Range(1.6, 3.2)
					pts := stoneShape(R, 0)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: -2, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 1, Drag: 0.5, Bounce: 0.3, Life: life, A: ang, VA: va, S: sz, Data: pts})
				}
				for i := 0.0; i < 14*k; i++ {
					x := R.Range(-L, L) * 0.9
					py := -R.Range(1, 4)
					vx := R.Range(-40, 40)
					vy := -R.Range(15, 55) * k
					life := R.Range(1, 1.7)
					c := DUST2
					if R.Chance(0.5) {
						c = DUST
					}
					sz := R.Range(4, 7) * k
					delay := R.Range(0, 0.15)
					ps.Spawn(pixelfx.Smoke, x, py, vx, vy, life, c, sz, delay, pixelfx.Big)
				}
				for i := 0.0; i < 10*k; i++ {
					x := R.Range(-L, L) * 0.7
					a := -pi/2 + R.Range(-1, 1)
					sp := R.Range(120, 300)
					life := R.Range(0.12, 0.3)
					ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
				}
			}
			// between jolts the split keeps breathing grit and wisps of dust, embers float up out of it
			if t > quakeJolts[0] && t < quakeClose && R.Chance(0.3*f) {
				x := R.Range(-1, 1) * open()
				vx := R.Range(-20, 20)
				vy := -R.Range(20, 70)
				life := R.Range(0.5, 0.8)
				c := pixelfx.Pick(R, []uint32{S[6], S[5]})
				ps.Spawn(pixelfx.Grit, x, -1, vx, vy, life, c, 1, 0, 0)
				if R.Chance(0.3) {
					evx := R.Range(-10, 10)
					evy := -R.Range(20, 45)
					elife := math.Min(R.Range(0.6, 1.2), quakeDuration-0.1-t)
					ps.Spawn(pixelfx.Ember, x, -1, evx, evy, elife, G[3], 1, 0, 0)
				}
				if R.Chance(0.25) {
					dvx := R.Range(-15, 15)
					dvy := -R.Range(4, 14)
					dlife := R.Range(0.5, 0.8)
					dsz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, x, -1, dvx, dvy, dlife, DUST2, dsz, 0, 0)
				}
			}
			for i := range slabs {
				s := &slabs[i]
				if s.rest {
					continue
				}
				s.vy += 950 * dt
				s.y += s.vy * dt
				s.a += s.va * dt
				if s.y >= 0 && s.vy > 0 {
					s.y = 0
					s.a = pixelfx.ClampTo(s.a, -0.45, 0.45)
					if s.vy > 160 {
						s.vy *= -0.25
						s.va *= -0.4
					} else {
						s.rest = true
						for range 3 {
							px := s.x + R.Range(-5, 5)
							vx := R.Sign() * R.Range(10, 40)
							vy := -R.Range(2, 12)
							life := R.Range(0.4, 0.7)
							sz := R.Range(1.5, 3)
							ps.Spawn(pixelfx.Dust, px, -0.5, vx, vy, life, DUST2, sz, 0, 0)
						}
					}
				}
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					vx := R.Sign() * R.Range(8, 30)
					vy := -R.Range(2, 10)
					life := math.Min(R.Range(0.3, 0.6), quakeDuration-0.1-t)
					sz := R.Range(1.3, 2.4)
					ps.Spawn(pixelfx.Dust, b.X, -0.5, vx, vy, life, DUST2, sz, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the rift: lit along its upper lip, dark inside with a deep glow, opening out from the middle
			L, hk, jx := open(), heat(), shake()
			fade := 1 - pixelfx.Smooth(quakeClose+0.3, quakeClose+0.7, t)
			if t > quakeJolts[0] {
				for i := 1; i < len(rift); i++ {
					x0, d0 := rift[i-1][0], rift[i-1][1]
					x1, d1 := rift[i][0], rift[i][1]
					if math.Abs(x0) > L {
						continue
					}
					for x := math.Ceil(x0); x < x1; x++ {
						u := (x - x0) / (x1 - x0)
						d := pixelfx.Round(d0 + (d1-d0)*u)
						w := gap(x)
						X := x + jx
						if w < 0.4 {
							p.Dot(X, d, S[1], fade)
							continue
						}
						p.Dot(X, d-1, S[6], fade)
						deep := pixelfx.Rand(math.Floor(x/3), 41) + (w-1.5)*0.35
						hot := hk * pixelfx.ClampTo(deep-0.35, 0, 1) * 1.6
						for y := 0.0; y < w; y++ {
							col := S[0]
							if y > w/2-1 && hot > 0.25 {
								if hot > 0.9 && pixelfx.Rand(x, math.Floor(t*12)) < 0.6 {
									col = G[3]
								} else {
									col = G[2]
								}
							} else if y > w/2-1 && hot > 0.08 {
								col = G[1]
							}
							p.Dot(X, d+y, col, fade)
						}
						p.Dot(X, d+math.Ceil(w), S[2], fade)
					}
					if i%3 == 0 && hk > 0 {
						p.Glow(x1+jx, d1+1, 9, G[2], 0.35*hk*(1-math.Abs(x1)/(REACH*1.2)))
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// slabs of floor heaved up out of it, tilted, sinking back as it closes
			jx := shake()
			sink := pixelfx.Smooth(quakeClose, quakeClose+0.6, t)
			for _, s := range slabs {
				if !(s.rest && s.a == 0 && s.y == 0) && sink < 1 {
					drawRock(p, s.x+jx, s.y-1.5+sink*4, s.s, s.a, s.pts, S, 1-sink, false)
				}
			}
			for i := range bits.List {
				b := &bits.List[i]
				drawRock(p, b.X, b.Y, b.S, b.A, b.Data.([][2]float64), S, math.Min(1, b.Life/0.4), false)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
