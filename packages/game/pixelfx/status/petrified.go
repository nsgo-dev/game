// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/petrified.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the creeping front, the branching cracks and the burst of the body into chunks, grit and dust from Sprite Fusion's open-source ice freeze and shatter (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const (
	petrifiedDuration = 3.8
	petrifiedCreep    = 0.95
	petrifiedCrack    = 1.75
	petrifiedShatter  = 2.2
)

var (
	petrifiedReach = pixelfx.Pose{}.With(pixelfx.Lean.Is(3), pixelfx.Crouch.Is(2), pixelfx.FX.Is(14), pixelfx.FY.Is(-29), pixelfx.BX.Is(-4), pixelfx.BY.Is(-22), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-7), pixelfx.ToeB.On())
	petrifiedShock = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Lean.Is(-1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-24), pixelfx.BX.Is(9), pixelfx.BY.Is(-24))
)

// Petrified is turned to stone: grey creeps up from the feet behind a crackling edge that spits grit, the statue holds, cracks run through it, and it bursts into tumbling chunks and dust. It is registered as "status/petrified".
var Petrified = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Petrified",
		Desc:     "Turned to stone: grey creeps up from the feet behind a crackling edge that spits grit, the statue holds, cracks run through it, and it bursts into tumbling chunks and dust.",
		Tags:     []string{"status", "stone", "petrify", "petrified", "debuff", "death", "one-shot"},
		Size:     [2]int{120, 74},
		Origin:   [2]int{60, 64},
		Duration: petrifiedDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#16141c", "#2a2733", "#433f4d", "#5f5a6a", "#7d7889", "#a29dab", "#cdc8d2", "#f0edf3")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.16, Pose: pixelfx.Ready},
			{T: 0.08, Pose: petrifiedShock, How: "snap"},
			{T: 0.5, Pose: petrifiedReach},
			{T: 0.95, Pose: petrifiedReach},
			{T: 3.8, Pose: petrifiedReach},
		},
		Hits: []pixelfx.Hit{
			{T: 2.2, Stop: 0.08, Shake: 0.24, Amp: pixelfx.Val(2)},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < petrifiedCreep {
			return 1
		}
		return 0
	},
	New: newPetrified,
}

func init() { pixelfx.Register("status/petrified", Petrified) }

func petrifiedLevel(t float64) float64 {
	if t >= petrifiedCreep {
		return 1.2
	}
	return 1.2*pixelfx.Smooth(0, petrifiedCreep, t) + 0.025*math.Sin((t/petrifiedCreep)*tau*3)
}

func newPetrified(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	H := float64(S.h)
	R := pixelfx.NewRng(seed)
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}})
	bits := pixelfx.NewBits(floor)
	lights := &pixelfx.Lights{}

	type stonePx struct {
		x, y int
		up   float64
		c    uint32
	}
	stone := make([]stonePx, len(S.body))
	for idx, pt := range S.body {
		x, y := pt[0], pt[1]
		tone := pixelfx.Clamp((pixelfx.Luma(colorAt(a, x, y)) - 0.06) / 0.78)
		mottle := pixelfx.TileFbm(float64(x)*0.32, float64(y)*0.32, 0, 0, seed+5, 2)
		i := 1 + int(pixelfx.Round(tone*4.6+mottle*1.1))
		if !S.on(x-1, y) || !S.on(x, y-1) {
			i++
		} else if !S.on(x+1, y) || !S.on(x, y+1) {
			i--
		}
		stone[idx] = stonePx{x: x, y: y, up: 1 - (float64(y)+0.5)/H, c: C[int(pixelfx.ClampTo(float64(i), 1, 6))]}
	}

	var cracks [][][2]float64
	ox := pixelfx.Round(float64(S.w) * 0.55)
	oy := pixelfx.Round(H * 0.42)
	var walk func(x, y, ang, ln float64)
	walk = func(x, y, ang, ln float64) {
		pts := [][2]float64{{x, y}}
		for s := 0.0; s < ln; s++ {
			ang += R.Range(-0.55, 0.55)
			x += math.Cos(ang) * R.Range(1.4, 2.4)
			y += math.Sin(ang) * R.Range(1.4, 2.4)
			if !S.on(int(math.Floor(x)), int(math.Floor(y))) {
				break
			}
			pts = append(pts, [2]float64{x, y})
			if len(pts) > 3 && len(cracks) < 16 && R.Chance(0.14) {
				walk(x, y, ang+R.Sign()*R.Range(0.6, 1.2), ln/2)
			}
		}
		cracks = append(cracks, pts)
	}
	for i := 0; i < 7; i++ {
		walk(ox, oy, (float64(i)/7)*tau+R.Range(-0.3, 0.3), 14)
	}

	var t float64
	var broke, settled bool
	front := func(mx float64) float64 {
		return petrifiedLevel(t) + 0.06*pixelfx.TileFbm(mx*0.4, 0.5, 0, 0, seed, 2)
	}
	gritCols := []uint32{C[4], C[5], C[6]}
	crackCols := []uint32{C[5], C[6]}
	shardCols := []uint32{C[5], C[6], C[7]}
	chunkRamp := []uint32{C[0], C[2], C[3], C[5], C[6]}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < petrifiedCreep {
				rate := 60 * dt
				for n := 0.0; n < rate; n++ {
					if !R.Chance(0.5) {
						continue
					}
					pt := S.body[int(R.Int(0, float64(len(S.body)-1)))]
					mx, my := pt[0], pt[1]
					if math.Abs(1-(float64(my)+0.5)/H-front(float64(mx))) > 0.04 {
						continue
					}
					x := a.X + float64(mx) + 0.5
					y := a.Y + float64(my)
					side := 1.0
					if float64(mx) < float64(S.w)/2 {
						side = -1
					}
					gvx := side * R.Range(15, 60)
					gvy := -R.Range(30, 90)
					glife := R.Range(0.6, 1.1)
					gcol := pixelfx.Pick(R, gritCols)
					gsize := 1.0
					if R.Chance(0.3) {
						gsize = 2
					}
					ps.Spawn(pixelfx.Grit, x, y, gvx, gvy, glife, gcol, gsize, 0, 0)
					if R.Chance(0.3) {
						dvx := side * R.Range(10, 30)
						dvy := -R.Range(4, 14)
						dlife := R.Range(0.4, 0.7)
						dsize := R.Range(1.5, 2.6)
						ps.Spawn(pixelfx.Dust, x+side*2, y, dvx, dvy, dlife, C[4], dsize, 0, 0)
					}
					if R.Chance(0.25) {
						mvx := side * R.Range(40, 90)
						mvy := -R.Range(20, 70)
						mlife := R.Range(0.15, 0.3)
						ps.Spawn(pixelfx.Mote, x, y, mvx, mvy, mlife, C[7], 0.5, 0, 0)
					}
				}
			}
			if !settled && t >= petrifiedCreep {
				settled = true
				lights.Add(0, a.Y+H*0.5, 34, C[6], 0.35, 0.3)
				for i := 0; i < 8; i++ {
					dx := R.Range(-8, 8)
					dy := -R.Range(0, 2)
					vx := R.Sign() * R.Range(20, 50)
					vy := -R.Range(2, 10)
					life := R.Range(0.6, 1)
					size := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, dx, dy, vx, vy, life, C[3], size, 0, 0)
				}
			}
			if t > petrifiedCreep+0.2 && t < petrifiedShatter && R.Chance(1.6*dt) {
				pt := S.body[int(R.Int(0, float64(len(S.body)-1)))]
				mx, my := pt[0], pt[1]
				if !S.on(mx, my+1) {
					vx := R.Range(-8, 8)
					life := R.Range(0.8, 1.2)
					ps.Spawn(pixelfx.Grit, a.X+float64(mx)+0.5, a.Y+float64(my)+1, vx, 0, life, C[3], 1, 0, 0)
				}
			}
			if t >= petrifiedCrack && t < petrifiedShatter && R.Chance(12*dt) {
				pts := pixelfx.Pick(R, cracks)
				pt := pts[int(math.Floor(R.Next()*float64(len(pts))))]
				vx := R.Range(-40, 40)
				vy := -R.Range(30, 80)
				life := R.Range(0.5, 0.9)
				col := pixelfx.Pick(R, crackCols)
				ps.Spawn(pixelfx.Grit, a.X+pt[0], a.Y+pt[1], vx, vy, life, col, 1, 0, 0)
			}
			if !broke && t >= petrifiedShatter {
				broke = true
				lights.Add(0, a.Y+H*0.45, 46, C[6], 0.7, 0.5)
				lights.Add(0, a.Y+H*0.45, 30, pixelfx.White, 0.7, 0.12)
				cx, cy := a.X+ox, a.Y+oy
				for i := 0; i < 18; i++ {
					q := stone[int(R.Int(0, float64(len(stone)-1)))]
					x := a.X + float64(q.x) + 0.5
					y := a.Y + float64(q.y) + 0.5
					dx, dy := x-cx, y-cy
					d := pixelfx.Hypot(dx, dy)
					if d == 0 {
						d = 1
					}
					sp := R.Range(35, 110)
					vy := (dy/d)*sp*0.6 - R.Range(60, 160)
					life := R.Range(1.1, 1.5)
					ang := R.Range(0, tau)
					va := R.Range(-14, 14)
					s := R.Range(1.3, 2.8)
					bits.Add(pixelfx.Bit{
						Kind: pixelfx.Chunk, X: x, Y: y, VX: (dx / d) * sp, VY: vy,
						G: 1, Drag: 0.4, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, Ramp: chunkRamp,
					})
				}
				for _, q := range stone {
					if pixelfx.Rand(float64(q.x), float64(q.y), 9) > 0.22 {
						continue
					}
					x := a.X + float64(q.x) + 0.5
					y := a.Y + float64(q.y) + 0.5
					dx, dy := x-cx, y-cy
					d := pixelfx.Hypot(dx, dy)
					if d == 0 {
						d = 1
					}
					sp := R.Range(40, 130)
					vy := (dy/d)*sp*0.6 - R.Range(40, 140)
					life := R.Range(0.9, 1.4)
					size := 1.0
					if R.Chance(0.35) {
						size = 2
					}
					ps.Spawn(pixelfx.Grit, x, y, (dx/d)*sp, vy, life, q.c, size, 0, 0)
				}
				for i := 0; i < 16; i++ {
					an := -pi/2 + R.Range(-1.4, 1.4)
					sp := R.Range(50, 120)
					sx := cx + R.Range(-5, 5)
					sy := cy + R.Range(-8, 8)
					life := R.Range(0.9, 1.4)
					ang := R.Range(0, tau)
					va := R.Range(-18, 18)
					s := R.Range(1.5, 3)
					col := pixelfx.Pick(R, shardCols)
					bits.Add(pixelfx.Bit{
						Kind: pixelfx.Shard, X: sx, Y: sy, VX: math.Cos(an) * sp, VY: math.Sin(an) * sp,
						G: 0.8, Drag: 0.6, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, C: col,
					})
				}
				for i := 0; i < 6; i++ {
					sx := cx + R.Range(-12, 12)
					sy := cy + R.Range(-10, 14)
					vx := R.Range(-10, 10)
					vy := -R.Range(5, 20)
					life := R.Range(0.4, 0.9)
					size := R.Range(1, 2.2)
					delay := R.Range(0.05, 0.4)
					ps.Spawn(pixelfx.StarKind, sx, sy, vx, vy, life, C[6], size, delay, 0)
				}
				for i := 0; i < 12; i++ {
					side := R.Sign()
					sx := side * R.Range(0, 8)
					sy := -R.Range(1, 6)
					vx := side * R.Range(20, 60)
					vy := -R.Range(4, 16)
					life := R.Range(0.9, 1.4)
					size := R.Range(2.4, 4)
					delay := R.Range(0, 0.08)
					ps.Spawn(pixelfx.Smoke, sx, sy, vx, vy, life, C[4], size, delay, 0)
				}
				for i := 0; i < 6; i++ {
					sx := R.Range(-6, 6)
					sy := a.Y + H*R.Range(0.3, 0.8)
					vx := R.Range(-20, 20)
					vy := -R.Range(10, 30)
					life := R.Range(1, 1.5)
					size := R.Range(3, 4.5)
					delay := R.Range(0, 0.1)
					ps.Spawn(pixelfx.Smoke, sx, sy, vx, vy, life, C[3], size, delay, 0)
				}
			}
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					vx := R.Range(-20, 20)
					vy := -R.Range(2, 10)
					life := R.Range(0.3, 0.6)
					size := R.Range(1.2, 2.2)
					ps.Spawn(pixelfx.Dust, b.X, -0.5, vx, vy, life, C[3], size, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < petrifiedShatter {
				sh := 0.0
				if t >= petrifiedCrack {
					sh = pixelfx.Round(math.Sin(t*90) * math.Min(1, (t-petrifiedCrack)/(petrifiedShatter-petrifiedCrack)) * 1.2)
				}
				white := t > petrifiedShatter-0.035
				f24 := math.Floor(t * 24)
				for _, q := range stone {
					f := front(float64(q.x))
					d := f - q.up
					if d < 0 {
						continue
					}
					x := a.X + float64(q.x) + sh
					y := a.Y + float64(q.y)
					if d < 0.035 && t < petrifiedCreep {
						col := C[6]
						if pixelfx.Rand(float64(q.x), float64(q.y), f24) < 0.5 {
							col = C[7]
						}
						p.Px(x, y, col)
					} else if d < 0.07 && t < petrifiedCreep {
						p.Px(x, y, C[5])
					} else {
						col := q.c
						if white {
							col = pixelfx.White
						}
						p.Px(x, y, col)
					}
				}
				if t < petrifiedCreep {
					for mx := 0; mx < S.w; mx += 2 {
						my := int(math.Floor((1 - front(float64(mx))) * H))
						if S.on(mx, my) {
							p.Glow(a.X+float64(mx), a.Y+float64(my), 5, C[6], 0.18)
						}
					}
				}
				if t >= petrifiedCrack {
					k := pixelfx.Clamp((t - petrifiedCrack) / 0.3)
					for _, pts := range cracks {
						n := int(math.Floor(float64(len(pts)) * k))
						for i := 0; i < n; i++ {
							px := math.Floor(a.X+pts[i][0]) + sh
							py := math.Floor(a.Y + pts[i][1])
							fresh := i > n-3
							col := C[0]
							if fresh {
								col = pixelfx.White
							}
							p.Px(px, py, col)
							if !fresh {
								p.Px(px, py-1, C[6])
							}
						}
					}
					p.Glow(a.X+ox, a.Y+oy, 12, C[6], 0.25*k)
				}
			}
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
