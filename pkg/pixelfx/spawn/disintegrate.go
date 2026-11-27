// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/disintegrate.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flickering outline, the white silhouette flash and the body's own pixels flying off as bits of light from Sprite Fusion's open-source movement (https://www.spritefusion.com/pixel-art-vfx/movement), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	disintegrateAgony  = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(0), pixelfx.HY.Is(-1), pixelfx.FX.Is(-14), pixelfx.FY.Is(-36), pixelfx.BX.Is(15), pixelfx.BY.Is(-36), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	disintegrateFlash  = 0.42
	disintegrateWhiteT = 0.07
	disintegrateP0     = disintegrateFlash + disintegrateWhiteT
	disintegratePeel   = 1.2
	disintegrateCoreY  = -20.0
)

// Disintegrate crackles the character with light, flashes white, then peels it away from the head down: each pixel flares white-hot and streams off on the wind as a cooling mote of light. It is registered as "spawn/disintegrate".
var Disintegrate = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Disintegrate",
		Desc:     "The character crackles with light, flashes white, then peels away from the head down: each pixel flares white-hot and streams off on the wind as a cooling mote of light.",
		Tags:     []string{"spawn", "death", "disintegrate", "dissolve", "pixels", "energy", "light", "one-shot"},
		Size:     [2]int{176, 100},
		Origin:   [2]int{48, 86},
		Duration: 3.4,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#120a26", "#2a1658", "#3d2f9c", "#3f68d4", "#3fa8ec", "#86e0ff", "#d6f8ff", "#ffffff")},
			{Name: "accent", Default: pixelfx.Palette("#7a3ae4", "#b682ff", "#ecd2ff")},
			{Name: "wind", Default: pixelfx.Num(1)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.2, Pose: pixelfx.Ready},
			{T: 0.05, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(3), pixelfx.FX.Is(-9), pixelfx.FY.Is(-16), pixelfx.BX.Is(10), pixelfx.BY.Is(-16), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 0.3, Pose: disintegrateAgony},
			{T: 3.4, Pose: disintegrateAgony},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < disintegrateFlash-0.1 {
			return 1
		}
		return 0
	},
	New: newDisintegrate,
}

func init() { pixelfx.Register("spawn/disintegrate", Disintegrate) }

type disintegrateMote struct {
	x, y, vx, vy, life, max float64
	c                       uint32
	kind                    int
	lift                    float64
	cool                    [4]uint32
	own, rest               bool
	seed                    float64
}

func newDisintegrate(o *pixelfx.Options) pixelfx.Instance {
	C, A, R, W := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("accent"), 3), pixelfx.NewRng(o.Seed()), o.Num("wind")
	floor := func(float64) float64 { return 0 }
	// what a mote cools through after its white flare: the main ramp, or the accent for variety
	COOL := [3][4]uint32{
		{C[6], C[5], C[5], C[4]},
		{A[2], A[1], A[1], A[0]},
		{pixelfx.White, C[6], C[5], C[5]},
	}
	body := bodyOf(o.Actor, C)
	rim := rimOf(body)
	top, H := body.y, float64(body.h)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	lights := &pixelfx.Lights{}
	// when each pixel lets go: row by row from the crown, a ragged front, the rim a beat before the middle
	for _, b := range body.list {
		u := (b.y - top) / H
		edgeOff := 0.0
		if b.edge {
			edgeOff = 0.03
		}
		b.key = u + 0.14*(pixelfx.Rand(float64(b.mx), float64(b.my), 3)-0.5) + 0.05*pixelfx.Noise(float64(b.mx)*0.45, 7) - edgeOff
		b.gone = false
	}
	front := func(tt float64) float64 {
		return -0.06 + 1.14*pixelfx.Ease.InOutSine(pixelfx.Clamp((tt-disintegrateP0)/disintegratePeel))
	}
	var motes []disintegrateMote
	// the wind: a steady push with gusts, and a slow swirl so neighbouring motes travel in sheets
	gust := func(tt float64) float64 { return 0.7 + 0.5*(pixelfx.Noise(tt*1.6, 11)*0.5+0.5) }
	t, flashed := 0.0, false
	starCols := []uint32{pixelfx.White, C[6], C[5]}

	letGo := func(b *bodyCell) {
		b.gone = true
		lo := b.y > -9 && R.Chance(0.22)
		kind := 0
		switch {
		case lo:
			kind = 2
		case R.Chance(0.42):
			kind = 1
		case R.Chance(0.07):
			kind = 3
		}
		if kind == 3 {
			vx := R.Range(-6, 14) * W
			vy := R.Range(-26, -8)
			life := R.Range(0.6, 1.1)
			col := pixelfx.White
			if R.Chance(0.5) {
				col = C[6]
			}
			size := R.Range(1, 2.2)
			ps.Spawn(pixelfx.StarKind, b.x, b.y, vx, vy, life, col, size, 0, 0)
			return
		}
		var life float64
		switch kind {
		case 2:
			life = R.Range(0.7, 1.2)
		case 1:
			life = R.Range(0.25, 0.5)
		default:
			life = R.Range(0.9, 1.7)
		}
		// a pop off the body, outward from its middle and up, before the wind takes it
		out := (b.x + 0.5) * R.Range(2, 6)
		vxMul := 1.0
		if kind == 2 {
			vxMul = 0.4
		}
		vx := (out + R.Range(-6, 16)) * vxMul
		var vy float64
		if kind == 2 {
			vy = R.Range(-30, -8)
		} else {
			vy = R.Range(-46, -8)
		}
		lift := R.Range(0.4, 1.4)
		coolIdx := 0
		if R.Chance(0.3) {
			coolIdx = 1
		} else if R.Chance(0.3) {
			coolIdx = 2
		}
		own := R.Chance(0.2)
		seed := R.Range(0, 100)
		motes = append(motes, disintegrateMote{
			x: b.x + 0.5, y: b.y + 0.5, vx: vx, vy: vy,
			life: life, max: life, c: b.c, kind: kind, lift: lift, cool: COOL[coolIdx], own: own, rest: false, seed: seed,
		})
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the charge: light gathers into the body in streaks
			if t < disintegrateFlash-0.05 && R.Chance(0.55) {
				a, D, T := R.Range(0, tau), R.Range(22, 44), R.Range(0.16, 0.26)
				v := (D * 3) / (math.Exp(3*T) - 1)
				x := math.Cos(a) * D
				y := math.Min(disintegrateCoreY+math.Sin(a)*D*0.9, -3)
				l := pixelfx.Hypot(x, y-disintegrateCoreY)
				if l == 0 {
					l = 1
				}
				col := C[5]
				if R.Chance(0.5) {
					col = C[6]
				}
				ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, ((disintegrateCoreY-y)/l)*v, T, col, 1, 0, 0)
			}
			if !flashed && t >= disintegrateFlash {
				flashed = true
				lights.Add(0, disintegrateCoreY, 64, C[4], 0.85, 0.6)
				lights.Add(0, disintegrateCoreY, 30, pixelfx.White, 0.8, 0.14)
				for range 14 {
					a, sp := R.Range(0, tau), R.Range(50, 150)
					x, y := R.Range(-6, 6), disintegrateCoreY+R.Range(-10, 10)
					life, col, size := R.Range(0.35, 0.8), pixelfx.Pick(R, starCols), R.Range(0.6, 1.8)
					ps.Spawn(pixelfx.StarKind, x, y, math.Cos(a)*sp, math.Sin(a)*sp*0.7-25, life, col, size, 0, 0)
				}
			}
			// the peel front eats the body
			if t >= disintegrateP0 {
				f := front(t)
				for _, b := range body.list {
					if !b.gone && b.key < f {
						letGo(b)
					}
				}
			}
			// the wind drops once the body is gone, and the last motes hang in the air
			g := gust(t) * (1 - 0.55*pixelfx.Smooth(disintegrateP0+disintegratePeel, disintegrateP0+disintegratePeel+1.2, t))
			for i := len(motes) - 1; i >= 0; i-- {
				m := &motes[i]
				m.life -= dt
				if m.life <= 0 {
					motes = append(motes[:i], motes[i+1:]...)
					continue
				}
				if m.kind == 2 {
					// heavier ash: drifts a little, falls, settles on the floor and glows out
					if m.rest {
						continue
					}
					m.vx += (W*30*g - m.vx*1.2) * dt
					m.vy += 260*dt - m.vy*1.2*dt
					m.x += m.vx * dt
					ny := m.y + m.vy*dt
					if m.vy > 0 && ny >= -0.5 {
						if m.vy > 50 {
							m.vy *= -0.35
							m.vx *= 0.6
							m.y = -0.6
						} else {
							m.rest = true
							m.y = -0.5
						}
					} else {
						m.y = ny
					}
					continue
				}
				age := m.max - m.life
				k := math.Min(1, age/0.15)
				fx := W * (100*g + 30*pixelfx.Noise2(m.x*0.07, m.y*0.07-t*0.8, 3)) * k
				fy := (-30*m.lift + 42*pixelfx.Noise2(m.x*0.06+40, m.y*0.06-t*1.1, 5)) * k
				drag := 1.5
				if m.kind == 1 {
					drag = 3
				}
				m.vx += (fx - m.vx*drag) * dt
				m.vy += (fy - m.vy*drag) * dt
				m.x += m.vx * dt
				m.y += m.vy * dt
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the floor ring from the flash, far half
			k := (t - disintegrateFlash) / 0.5
			if k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0, 6+30*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 3.5*(1-k)), 1.15-k, C, func(g float64) bool { return math.Sin(g) < 0 }, k < 0.12)
			}
			// a cold pool on the floor while the body burns away
			pool := pixelfx.Smooth(0.1, disintegrateFlash, t) * (1 - pixelfx.Smooth(disintegrateP0+disintegratePeel*0.8, disintegrateP0+disintegratePeel+0.6, t))
			if pool > 0 {
				p.Glow(0, -1, 30, C[4], 0.22*pool)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f := front(t)
			// charge: a crackling outline that thickens, the rim of the body catching light
			if t < disintegrateFlash {
				k, fr := t/disintegrateFlash, math.Floor(t*30)
				for _, c := range rim {
					h := pixelfx.Rand(float64(c.mx*61+c.my), fr, 7)
					if h < 0.12+0.62*k*k {
						col := C[5]
						if k > 0.72 && pixelfx.Rand(float64(c.mx), float64(c.my), fr) < 0.5 {
							col = pixelfx.White
						} else if h < 0.2 {
							col = C[6]
						}
						p.Px(c.x, c.y, col)
					}
				}
				p.Glow(0, disintegrateCoreY, 26, C[4], 0.42*k)
				p.Glow(0, disintegrateCoreY, 12, C[6], 0.25*k*k)
			}
			// the body: drawn by us from the shudder on, flashing white, then charged, glowing hot at the front
			if t >= disintegrateFlash-0.1 {
				shake := 0.0
				if t < disintegrateFlash {
					if int(math.Floor(t*60))%2 != 0 {
						shake = 0.6
					} else {
						shake = -0.6
					}
				}
				white := t >= disintegrateFlash && t < disintegrateP0
				for _, b := range body.list {
					if b.gone {
						continue
					}
					var c uint32
					if white {
						if b.edge {
							c = C[6]
						} else {
							c = pixelfx.White
						}
					} else if t < disintegrateFlash {
						if b.edge {
							c = pixelfx.Mix(b.c, C[6], 0.7)
						} else {
							c = pixelfx.Mix(b.c, C[5], 0.25)
						}
					} else {
						d := b.key - f
						above := !body.on(b.mx, b.my-1) || body.cell(b.mx, b.my-1).gone
						switch {
						case above && d < 0.06:
							c = pixelfx.White
						case d < 0.03:
							c = pixelfx.Mix(b.c, C[6], 0.7)
						case d < 0.1:
							c = pixelfx.Mix(b.c, C[5], 0.4)
						case b.edge:
							c = pixelfx.Mix(b.c, C[5], 0.5)
						default:
							c = pixelfx.Mix(b.c, C[4], 0.16)
						}
					}
					p.Px(b.x+shake, b.y, c)
				}
				if white {
					p.Glow(0, disintegrateCoreY, 34, C[5], 0.7)
					pixelfx.Sparkle(p, 0, disintegrateCoreY-3, 9, C[6], C[4], 0.5)
				}
			}
			// light rides the peel front down the body
			if t >= disintegrateP0 && f < 1.05 {
				fy, k := top+pixelfx.Clamp(f)*H, 1-pixelfx.Smooth(0.85, 1.05, f)
				p.GlowLine(-10, fy, 10, fy, 8, C[5], 0.32*k)
				p.Glow(0, fy, 22, C[4], 0.2*k)
			}
			// the floor ring, near half
			k := (t - disintegrateFlash) / 0.5
			if k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0, 6+30*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 3.5*(1-k)), 1.15-k, C, func(g float64) bool { return math.Sin(g) >= 0 }, k < 0.12)
			}
			// the motes: flare white, cool down the ramp, leave a short tail, dither out
			for i := range motes {
				m := &motes[i]
				age, k2 := m.max-m.life, m.life/m.max
				a := 1.0
				if k2 < 0.35 {
					a = k2 / 0.35
				}
				var c uint32
				switch {
				case age < 0.034:
					c = pixelfx.Mix(m.c, pixelfx.White, 0.55)
				case age < 0.06:
					c = pixelfx.White
				case age < 0.15:
					c = m.cool[0]
				default:
					u := (age - 0.15) / math.Max(0.05, m.max-0.15)
					cc := m.cool[1+int(math.Min(2, math.Floor(u*3)))]
					if m.own && u < 0.5 {
						c = pixelfx.Mix(m.c, cc, 0.55)
					} else {
						c = cc
					}
					if m.kind == 0 && math.Sin(t*21+m.seed) > 0.8 {
						c = m.cool[0]
					}
				}
				if m.kind == 2 {
					p.Dot(m.x, m.y, c, a)
					p.Glow(m.x, m.y, 3, C[5], 0.3*k2)
					continue
				}
				sp := pixelfx.Hypot(m.vx, m.vy)
				spDiv := sp
				if spDiv == 0 {
					spDiv = 1
				}
				ux, uy := m.vx/spDiv, m.vy/spDiv
				if sp > 30 && m.kind == 0 {
					p.Dot(m.x-ux*1.2, m.y-uy*1.2, pixelfx.Scale(c, 0.6), a*0.85)
				}
				if sp > 85 && m.kind == 0 {
					p.Dot(m.x-ux*2.4, m.y-uy*2.4, pixelfx.Scale(c, 0.4), a*0.6)
				}
				p.Dot(m.x, m.y, c, a)
				if k2 > 0.15 {
					gk := 0.11
					if m.kind == 1 {
						gk = 0.16
					}
					p.Glow(m.x, m.y, 2.5, c, gk*k2)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
