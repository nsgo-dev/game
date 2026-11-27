// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/shatter-death.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the faceted ice shell, the cracks, and the shards, mist and glints of the burst from Sprite Fusion's open-source ice (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	shatterDuration = 3.3
	shatterFear     = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Lean.Is(-2), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-9), pixelfx.FY.Is(-31), pixelfx.BX.Is(9), pixelfx.BY.Is(-31), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	shatterLock     = 0.5
	shatterCrack    = 1.05
	shatterBurst    = 1.4
)

// ShatterDeath races frost up the body and seals it in faceted ice, sweeps a glint across it, spreads cracks from the chest, then bursts it into its own facets that tumble, bounce and melt. It is registered as "spawn/shatter-death".
var ShatterDeath = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Shatter death",
		Desc:     "Frost races up the body and seals it in faceted ice, a glint sweeps it, cracks spread from the chest, then it bursts into its own facets that tumble, bounce and melt.",
		Tags:     []string{"spawn", "death", "ice", "freeze", "shatter", "shards", "one-shot"},
		Size:     [2]int{172, 96},
		Origin:   [2]int{84, 84},
		Duration: shatterDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e1636", "#1a2f66", "#2557a3", "#3488d6", "#5fc0f0", "#a2e6ff", "#dcf8ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(14)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.2, Pose: pixelfx.Ready},
			{T: 0.08, Pose: shatterFear, How: "snap"},
			{T: 0.3, Pose: shatterFear.With(pixelfx.Lean.Is(-3), pixelfx.FY.Is(-34))},
			{T: 0.5, Pose: shatterFear},
			{T: 3.3, Pose: shatterFear},
		},
		Hits: []pixelfx.Hit{
			{T: 0.5, Stop: 0.02, Shake: 0.06, Amp: pixelfx.Val(1)},
			{T: 1.4, Stop: 0.09, Shake: 0.26, Amp: pixelfx.Val(3)},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < shatterBurst {
			return 1
		}
		return 0
	},
	New: newShatterDeath,
}

func init() { pixelfx.Register("spawn/shatter-death", ShatterDeath) }

type shatterSpike struct {
	x, base, h, w, lean, at float64
}

type shatterKey struct{ x, y int }

type shatterPiece struct {
	id                  float64
	x, y, vx, vy, a, va float64
	r                   float64
	pxMap               map[shatterKey]uint32
	life, max           float64
	rest                bool
	n                   int
}

type shatterRime struct {
	x, w, t0 float64
}

func newShatterDeath(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	body := bodyOf(o.Actor, C)
	W, H, X0, Y0 := body.w+2, body.h+1, body.x-1, body.y-1
	// the ice: the body grown by a pixel (not below its feet), cut into facets round a few seeds
	ice := make([]int16, W*H)
	for i := range ice {
		ice[i] = -1
	}
	col, glass, solid := make([]uint32, W*H), make([]uint32, W*H), make([]uint8, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			mx, my := x-1, y-1
			if body.on(mx, my) || (my < body.h-1 && (body.on(mx-1, my) || body.on(mx+1, my) || body.on(mx, my-1) || body.on(mx, my+1))) {
				ice[y*W+x] = 0
			}
		}
	}
	seeds := make([][2]float64, 0, 22)
	for range 22 {
		var x, y float64
		tries := 0
		for {
			x, y = R.Range(0, float64(W)), R.Range(0, float64(H))
			if ice[int(math.Floor(y))*W+int(math.Floor(x))] >= 0 {
				break
			}
			tries++
			if tries >= 40 {
				break
			}
		}
		seeds = append(seeds, [2]float64{x, y})
	}
	at := func(x, y int) bool { return x >= 0 && y >= 0 && x < W && y < H && ice[y*W+x] >= 0 }
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			i := y*W + x
			if ice[i] < 0 {
				continue
			}
			d1, d2, k := 1e9, 1e9, 0
			for j, sd := range seeds {
				d := pixelfx.Hypot(float64(x)+0.5-sd[0], (float64(y)+0.5-sd[1])*0.85)
				if d < d1 {
					d2, d1, k = d1, d, j
				} else if d < d2 {
					d2 = d
				}
			}
			ice[i] = int16(k)
			// each facet catches the light by where it faces: up and left bright, down and right deep
			sx, sy := seeds[k][0], seeds[k][1]
			lit := pixelfx.Clamp(0.55 + 0.35*(pixelfx.Rand(float64(k), 41)-0.5) - 0.5*((sx-float64(W)/2)/float64(W)) - 0.55*((sy-float64(H)/2)/float64(H)))
			var own uint32
			if bc := body.cell(x-1, y-1); bc != nil {
				own = bc.c
			}
			// under the ice: the body's own colours, gone cold; over it: a see-through facet catching the light
			if own != 0 {
				col[i] = pixelfx.Mix(own, C[4], 0.36)
			} else {
				col[i] = C[3]
			}
			glass[i] = C[3+int(math.Min(2, math.Floor(lit*3)))]
			var sc uint32
			if d2-d1 < 0.42 && own == 0 {
				if lit > 0.5 {
					sc = C[6]
				} else {
					sc = C[5]
				}
			} else if d2-d1 < 0.42 {
				col[i] = pixelfx.Mix(col[i], C[6], 0.45)
			}
			if !at(x-1, y) || !at(x, y-1) {
				sc = C[6] // lit outer rim, top and left
			}
			if !at(x+1, y) || (!at(x, y+1) && y < H-1) {
				sc = C[3] // shaded rim, bottom and right
			}
			if sc != 0 {
				col[i] = sc
				solid[i] = 1
			}
		}
	}
	// rime spikes where the ice pushes out: on the shoulders, the head and the feet
	var spikes []shatterSpike
	for range 7 {
		x, y0 := int(R.Int(1, float64(W-2))), int(R.Int(2, float64(H-3)))
		y := y0
		for y > 0 && at(x, y-1) {
			y--
		}
		if at(x, y) {
			h, w, lean, atT := R.Range(3, 6), R.Range(2.5, 3.5), R.Range(-2.5, 2.5), shatterLock+R.Range(-0.02, 0.08)
			spikes = append(spikes, shatterSpike{x: X0 + float64(x) + 0.5, base: Y0 + float64(y) + 1, h: h, w: w, lean: lean, at: atT})
		}
	}
	for _, sd := range [2]float64{-1, 1} {
		for i := range 2 {
			var h, x, w float64
			if i != 0 {
				h = R.Range(5, 7)
				x = sd * R.Range(11, 13)
				w = 3.5
			} else {
				h = R.Range(8, 11)
				x = sd * R.Range(7, 9)
				w = 4.5
			}
			lean := sd * h * R.Range(0.25, 0.45)
			atT := R.Range(0.08, 0.2)
			spikes = append(spikes, shatterSpike{x: x, base: 0, h: h, w: w, lean: lean, at: atT})
		}
	}
	// cracks from the chest
	IX, IY := pixelfx.Round(float64(W)*0.48), pixelfx.Round(float64(H)*0.42)
	var cracks [][][2]float64
	var walk func(x, y, a, length float64)
	walk = func(x, y, a, length float64) {
		pts := [][2]float64{{x, y}}
		for s := 0.0; s < length; s++ {
			a += R.Range(-0.5, 0.5)
			x += math.Cos(a) * R.Range(1.4, 2.4)
			y += math.Sin(a) * R.Range(1.4, 2.4)
			if !at(int(math.Floor(x)), int(math.Floor(y))) {
				break
			}
			pts = append(pts, [2]float64{x, y})
			if len(pts) > 3 && len(cracks) < 18 && R.Chance(0.15) {
				walk(x, y, a+R.Sign()*R.Range(0.6, 1.1), length/2)
			}
		}
		cracks = append(cracks, pts)
	}
	for i := range 7 {
		walk(IX, IY, (float64(i)/7)*tau+R.Range(-0.3, 0.3), 20)
	}

	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}, Haze: C[5]})
	bits, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	var pieces []shatterPiece
	var rime []shatterRime
	t, locked, cracked, burst := 0.0, false, false, false
	level := func(tt float64) float64 {
		return float64(H) * (1 - pixelfx.Ease.OutCubic(pixelfx.Clamp(tt/shatterLock)))
	}
	dur := shatterDuration
	shardCols := []uint32{C[7], C[6], C[5]}
	starCols := []uint32{pixelfx.White, C[6]}

	// the burst: every facet flies as one piece of ice, carrying its own pixels
	shatter := func() {
		type cellPx struct {
			x, y int
			c    uint32
		}
		cells := make([][]cellPx, len(seeds))
		var order []int
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				k := int(ice[y*W+x])
				if k < 0 {
					continue
				}
				if len(cells[k]) == 0 {
					order = append(order, k)
				}
				i := y*W + x
				c := col[i]
				if solid[i] == 0 {
					c = pixelfx.Mix(col[i], glass[i], 0.3)
				}
				cells[k] = append(cells[k], cellPx{x: x, y: y, c: c})
			}
		}
		for _, k := range order {
			px := cells[k]
			cx, cy := 0.0, 0.0
			for _, q := range px {
				cx += float64(q.x) + 0.5
				cy += float64(q.y) + 0.5
			}
			cx /= float64(len(px))
			cy /= float64(len(px))
			pm := make(map[shatterKey]uint32, len(px))
			rMax := 0.0
			for _, q := range px {
				if d := pixelfx.Hypot(float64(q.x)+0.5-cx, float64(q.y)+0.5-cy); d > rMax {
					rMax = d
				}
				pm[shatterKey{int(pixelfx.Round(float64(q.x) + 0.5 - cx)), int(pixelfx.Round(float64(q.y) + 0.5 - cy))}] = q.c
			}
			dx, dy := cx-IX, cy-IY
			d := pixelfx.Hypot(dx, dy)
			if d == 0 {
				d = 1
			}
			sp := R.Range(55, 115)
			life := R.Range(1.3, 1.85)
			vx := (dx/d)*sp + R.Range(-18, 18)
			vy := (dy/d)*sp*0.6 - R.Range(110, 220)
			va := R.Range(-9, 9)
			pieces = append(pieces, shatterPiece{
				id: float64(len(pieces) + 1), x: X0 + cx, y: Y0 + cy, vx: vx, vy: vy, a: 0, va: va,
				r: rMax + 1, pxMap: pm, life: life, max: life, rest: false, n: len(px),
			})
		}
		for range 64 {
			a, sp := -pi/2+R.Range(-1.45, 1.45), R.Range(60, 180)
			x, y := X0+IX+R.Range(-6, 6), Y0+IY+R.Range(-8, 8)
			life, ang, va, s, c := R.Range(0.9, 1.6), R.Range(0, tau), R.Range(-20, 20), R.Range(1.5, 3.2), pixelfx.Pick(R, shardCols)
			bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x, Y: y, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 0.85, Drag: 0.6, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, C: c})
		}
		for range 14 {
			x, y, vx, vy, life := R.Range(-10, 10), -R.Range(1, 20), R.Sign()*R.Range(20, 80), -R.Range(4, 20), R.Range(0.6, 1.1)
			c := C[5]
			if R.Chance(0.5) {
				c = C[6]
			}
			size := R.Range(2.5, 4.5)
			ps.Spawn(pixelfx.Vapour, x, y, vx, vy, life, c, size, 0, 0)
		}
		for range 16 {
			x, y, vx, vy, life := R.Range(-16, 16), -R.Range(4, 38), R.Range(-40, 40), -R.Range(10, 60), R.Range(0.4, 1)
			c, size := pixelfx.Pick(R, starCols), R.Range(1, 2.4)
			ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, size, 0, 0)
		}
		lights.Add(0, Y0+IY, 46, pixelfx.White, 0.75, 0.14)
		lights.Add(0, Y0+IY, 70, C[4], 0.6, 0.5)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the cold climbs: rime sparkles at the frost line, vapour rolls off the feet
			if t < shatterLock {
				y := Y0 + level(t)
				if R.Chance(0.7) {
					x, vx, vy, life := R.Range(-10, 10), R.Range(-12, 12), -R.Range(0, 18), R.Range(0.25, 0.5)
					c := C[6]
					if R.Chance(0.5) {
						c = pixelfx.White
					}
					size := R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, size, 0, 0)
				}
				if R.Chance(0.25) {
					x, vx, vy, life, size := R.Range(-8, 8), R.Sign()*R.Range(15, 50), -R.Range(2, 10), R.Range(0.6, 1), R.Range(2, 3.5)
					ps.Spawn(pixelfx.Vapour, x, -1, vx, vy, life, C[6], size, 0, 0)
				}
			}
			if !locked && t >= shatterLock {
				locked = true
				lights.Add(0, -20, 54, C[4], 0.6, 0.3)
				for range 6 {
					x, y, life, c, size := R.Range(-12, 12), -R.Range(4, 36), R.Range(0.35, 0.7), pixelfx.Pick(R, starCols), R.Range(1.2, 2.4)
					ps.Spawn(pixelfx.StarKind, x, y, 0, -4, life, c, size, 0, 0)
				}
			}
			if locked && t < shatterCrack && R.Chance(0.08) {
				x, y, life, c, size := R.Range(-11, 11), -R.Range(2, 36), R.Range(0.3, 0.6), pixelfx.Pick(R, starCols), R.Range(0.8, 2)
				ps.Spawn(pixelfx.StarKind, x, y, 0, -3, life, c, size, 0, 0)
			}
			if !cracked && t >= shatterCrack {
				cracked = true
				for range 5 {
					a, sp := R.Range(0, tau), R.Range(60, 150)
					life, ang, va, s := R.Range(0.5, 0.9), R.Range(0, tau), R.Range(-20, 20), R.Range(1, 2)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: X0 + IX, Y: Y0 + IY, VX: math.Cos(a) * sp, VY: math.Sin(a)*sp - 60, G: 0.8, Drag: 0.6, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, C: C[6]})
				}
				lights.Add(0, Y0+IY, 24, pixelfx.White, 0.7, 0.1)
			}
			if !burst && t >= shatterBurst {
				burst = true
				shatter()
			}
			// the pieces: tumble, bounce, skid to a stop, melt
			for i := len(pieces) - 1; i >= 0; i-- {
				q := &pieces[i]
				q.life -= dt
				if q.life <= 0 {
					pieces = append(pieces[:i], pieces[i+1:]...)
					continue
				}
				if q.rest {
					if t < dur-0.9 && R.Chance(0.02) {
						vx, vy, life, size := R.Range(-4, 4), -R.Range(4, 10), R.Range(0.5, 0.8), R.Range(1.5, 2.5)
						ps.Spawn(pixelfx.Vapour, q.x, q.y-1, vx, vy, life, C[5], size, 0, 0)
					}
					continue
				}
				q.vy += 900 * dt
				q.vx -= q.vx * 0.5 * dt
				q.x += q.vx * dt
				q.y += q.vy * dt
				q.a += q.va * dt
				g := -q.r * 0.55
				if q.y > g && q.vy > 0 {
					q.y = g
					if q.vy > 90 {
						rime = append(rime, shatterRime{x: q.x, w: 2 + q.r*0.6, t0: t})
						q.vy *= -0.32
						q.vx *= 0.65
						q.va = q.va*0.5 + R.Range(-4, 4)
						if q.n > 8 {
							ps.Spawn(pixelfx.StarKind, q.x, q.y, 0, -6, 0.3, pixelfx.White, 1.2, 0, 0)
						}
						for range 2 {
							dx, vx, vy, life, size := R.Range(-2, 2), R.Sign()*R.Range(10, 30), -R.Range(2, 8), R.Range(0.3, 0.5), R.Range(1.2, 2)
							ps.Spawn(pixelfx.Dust, q.x+dx, -0.5, vx, vy, life, C[5], size, 0, 0)
						}
					} else if math.Abs(q.vx) > 12 {
						q.vy = 0
						q.vx *= 0.9
						q.va *= 0.85
					} else {
						q.rest = true
						q.vx, q.vy = 0, 0
					}
				}
			}
			bits.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// frost on the floor under the feet, spreading as the ice locks, gone as the pieces melt
			k := pixelfx.Smooth(0.1, shatterLock, t) * (1 - pixelfx.Smooth(shatterBurst+0.6, shatterBurst+1.6, t))
			reach := 6 + 12*pixelfx.Smooth(0, shatterLock+0.3, t)
			if k > 0 {
				for x := math.Floor(-reach); x <= math.Ceil(reach); x++ {
					h, e := pixelfx.Rand(x, 17), 1-math.Abs(x)/(reach+1)
					c := C[6]
					if h < 0.3 {
						c = pixelfx.White
					}
					p.Dot(x, 0, c, k*math.Min(1, e*2))
					if h < 0.5 {
						p.Dot(x, 1, C[5], k*e)
					}
					if h < 0.35 && e > 0.3 {
						p.Dot(x, -1, C[6], k*e)
					}
				}
				p.Glow(0, -2, 26, C[4], 0.25*k)
			}
			// rime left where the pieces hit the floor
			for _, r := range rime {
				a := pixelfx.Clamp((t-r.t0)/0.08) * (1 - pixelfx.Smooth(dur-1, dur-0.15, t))
				for x := math.Floor(r.x - r.w); x <= r.x+r.w; x++ {
					h := pixelfx.Rand(x, 23)
					if h < 0.7 {
						c := C[6]
						if h < 0.25 {
							c = pixelfx.White
						}
						p.Dot(x, 0, c, a)
					}
					if h < 0.3 {
						p.Dot(x, 1, C[5], a*0.7)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if !burst {
				// the ice body: everything below the frost line, a white rime edge at the line itself
				lv := level(t)
				shake := 0.0
				if t > shatterCrack && t < shatterBurst {
					sgn := -1.0
					if int(math.Floor(t*50))%2 != 0 {
						sgn = 1
					}
					amp := 0.5
					if t > shatterBurst-0.15 {
						amp = 1
					}
					shake = sgn * amp
				}
				glint := -1.0
				if t > shatterLock {
					glint = math.Mod((t-shatterLock)*0.9, 1.6)
				}
				lvFloor := int(math.Floor(lv))
				for y := 0; y < H; y++ {
					if y < lvFloor {
						continue
					}
					for x := 0; x < W; x++ {
						i := y*W + x
						if ice[i] < 0 {
							continue
						}
						var c uint32
						switch {
						case y == lvFloor && t < shatterLock:
							if pixelfx.Rand(float64(x), 5) < 0.6 {
								c = pixelfx.White
							} else {
								c = C[6]
							}
						case t >= shatterLock && t < shatterLock+0.05:
							c = pixelfx.White
						default:
							c = col[i]
						}
						// a glint runs diagonally across the ice
						g := math.Abs((float64(x)+float64(H-y)*0.6)/(float64(W)+float64(H)*0.6) - glint)
						on := glint >= 0 && glint <= 1
						if on && g < 0.035 {
							c = pixelfx.White
						} else if on && g < 0.07 && c != C[3] {
							c = C[6]
						}
						p.Px(X0+float64(x)+shake, Y0+float64(y), c)
						if solid[i] == 0 && c == col[i] {
							p.Blend(X0+float64(x)+shake, Y0+float64(y), glass[i], 0.3)
						}
					}
				}
				// the dark outline that holds the ice together against the floor and the dark
				for y := max(0, lvFloor-1); y <= H; y++ {
					for x := -1; x <= W; x++ {
						if !at(x, y) && (at(x-1, y) || at(x+1, y) || (at(x, y+1) && float64(y+1) >= lv) || at(x, y-1)) && y < H {
							p.Px(X0+float64(x)+shake, Y0+float64(y), C[0])
						}
					}
				}
				for _, s := range spikes {
					g := pixelfx.OutBack(pixelfx.Clamp((t-s.at)/0.12), 2.4)
					if g > 0 {
						pixelfx.Crystal(p, s.x+shake, s.base, s.h*g, s.w, s.lean*g, C, 1, t < s.at+0.04, -1)
					}
				}
				if cracked {
					k := pixelfx.Clamp((t - shatterCrack) / 0.25)
					for _, pts := range cracks {
						n := int(math.Floor(float64(len(pts)) * k))
						for i := 0; i < n; i++ {
							x, y := pts[i][0], pts[i][1]
							fresh := i > n-3 && t-shatterCrack < 0.3
							c := C[7]
							if fresh {
								c = pixelfx.White
							}
							p.Px(X0+x+shake, Y0+y, c)
							if !fresh {
								p.Px(X0+x+shake, Y0+y+1, C[1])
							}
						}
					}
				}
				// cold light from the ice, a flash as it locks
				p.Glow(0, -18, 30, C[4], 0.18*pixelfx.Smooth(0, shatterLock, t))
				if t > shatterLock && t < shatterLock+0.18 {
					pixelfx.Sparkle(p, -4, Y0+8, pixelfx.Round(8*(1-(t-shatterLock)/0.18)), pixelfx.White, C[5], 0.6)
				}
			} else if t < shatterBurst+0.08 {
				// the burst frame
				k := (t - shatterBurst) / 0.08
				p.Circle(X0+IX, Y0+IY, 10*(1-k)+3, pixelfx.White, 1)
			}
			// the pieces: each drawn back from its own little map, so they turn without holes
			for i := range pieces {
				q := &pieces[i]
				ca, sa, fade, r := math.Cos(q.a), math.Sin(q.a), math.Min(1, q.life/0.5), math.Ceil(q.r)
				for py := -r; py <= r; py++ {
					for px := -r; px <= r; px++ {
						ux, uy := int(pixelfx.Round(px*ca+py*sa)), int(pixelfx.Round(-px*sa+py*ca))
						c := q.pxMap[shatterKey{ux, uy}]
						// a melting piece loses pixels at random rather than dithering away
						if c != 0 && pixelfx.Rand(float64(ux), float64(uy), q.id) < fade {
							col := c
							if q.max-q.life < 0.05 {
								col = pixelfx.White
							}
							p.Px(math.Floor(q.x)+px, math.Floor(q.y)+py, col)
						}
					}
				}
				if q.n > 6 {
					p.Glow(q.x, q.y, 6, C[5], 0.2*fade)
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
