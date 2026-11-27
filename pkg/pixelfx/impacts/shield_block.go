// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/shield-block.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts mapping a lattice onto a dome by longitude and latitude, the ripple measured over its surface, the rim light and the glancing sparks from Sprite Fusion's open-source shield (https://www.spritefusion.com/pixel-art-vfx/shield), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var shieldBrace = pixelfx.Ready.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(2), pixelfx.FX.Is(13), pixelfx.FY.Is(-28), pixelfx.BX.Is(6), pixelfx.BY.Is(-21), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-9))

// ShieldBlock is a blow caught on a half-dome of energy in front of the character: its diamond lattice flashes, a ripple runs over it, sparks glance off, then it blinks out cell by cell. It is registered as "impacts/shield-block".
var ShieldBlock = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Shield block",
		Desc:     "A blow is caught on a half-dome of energy in front of the character: its diamond lattice flashes, a ripple runs over it, sparks glance off, then it blinks out cell by cell.",
		Tags:     []string{"impact", "block", "shield", "barrier", "energy", "ripple", "actor", "one-shot"},
		Size:     [2]int{148, 104},
		Origin:   [2]int{40, 88},
		Duration: 1.1,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#151a3d", "#212f6b", "#2c58aa", "#3a8bd8", "#62c8f4", "#a6ecff", "#dcfbff", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: -0.36, Pose: pixelfx.Ready},
			{T: -0.1, Pose: shieldBrace},
			{T: 0, Pose: shieldBrace.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(-2), pixelfx.FX.Is(10), pixelfx.FY.Is(-26)), How: "snap"},
			{T: 0.18, Pose: shieldBrace.With(pixelfx.X.Is(-4), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(0))},
			{T: 0.65, Pose: shieldBrace.With(pixelfx.X.Is(-3))},
			{T: 1.1, Pose: pixelfx.Ready.With(pixelfx.X.Is(0))},
		},
		Hits: []pixelfx.Hit{{T: 0.005, Stop: 0.09, Shake: 0.2, Amp: pixelfx.Val(2)}},
		Dust: []pixelfx.DustKick{{T: 0.01, X: -10, N: 6, Dir: -1}},
	},
	New: newShieldBlock,
}

func init() { pixelfx.Register("impacts/shield-block", ShieldBlock) }

// The dome's flat face, its depth and height, and the lattice's density; where the blow lands on it, and when it starts to blink out. Variables, so sums with them round as pixelfx's do.
var (
	shieldCX, shieldRX, shieldRY, shieldGrid = 3.0, 28.0, 44.0, 3.4
	shieldHitLon, shieldHitLat, shieldGone   = 1.18, 0.5, 0.5
)

// shieldOnDome returns a point on the dome's surface (lon round from facing us, lat up from the floor) in local space.
func shieldOnDome(lon, lat float64) (float64, float64) {
	return shieldCX + shieldRX*math.Cos(lat)*math.Sin(lon), -shieldRY * math.Sin(lat)
}

func newShieldBlock(o *pixelfx.Options) pixelfx.Instance {
	C, S, R := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("sparks"), 4), pixelfx.NewRng(o.Seed())
	CX, RX, RY, GRID, GONE := shieldCX, shieldRX, shieldRY, shieldGrid, shieldGone
	hitLon, hitLat := shieldHitLon, shieldHitLat
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: S})
	lights := &pixelfx.Lights{}
	hx, hy := shieldOnDome(hitLon, hitLat)
	sp := spikes(R, 8, 0.2, spikeOpts{len0: 5, len1: 8, w0: 1.4, w1: 1.9, fwd: 1.2, back: 0.3, jag: 1, jit: 0.18})

	lights.Add(hx, hy, 40, S[1], 0.7, 0.2)
	lights.Add(hx, hy, 12, pixelfx.White, 0.7, 0.06)
	// sparks glance off the dome: out and along its surface, mostly upward
	nA := math.Atan2(-math.Sin(hitLat)/RY, (math.Cos(hitLat)*math.Sin(hitLon))/RX) // the outward normal, roughly
	for i := range 14 {
		a := nA + R.Range(-1.2, 0.9)
		v := R.Range(130, 300)
		life := R.Range(0.15, 0.42)
		delay := 0.0
		if i > 10 {
			delay = R.Range(0.01, 0.04)
		}
		ps.Spawn(pixelfx.Spark, hx+1, hy, math.Cos(a)*v, math.Sin(a)*v-30, life, pixelfx.White, 1, delay, 0)
	}

	t := 0.0
	ripples := [2][2]float64{{0, 1}, {0.12, 0.55}}
	// the dome, front (back false) or the far side seen through it (back true): a glassy fill, a diamond lattice, a bright rim where it turns away, a sheen sweeping up it
	dome := func(p *pixelfx.Painter, back bool) {
		fade := 1 - pixelfx.Smooth(GONE+0.35, GONE+0.55, t)
		if fade <= 0 {
			return
		}
		for y := -RY - 1; y < 0; y++ {
			for x := CX; x <= CX+RX+1; x++ {
				u, v := (x+0.5-CX)/RX, -(y+0.5)/RY
				q := u*u + v*v
				if q > 1 || u < 0 || v < 0 {
					continue
				}
				z := math.Sqrt(1 - q)
				if back {
					z *= -1
				} else {
					z *= 1
				}
				lon, lat := math.Atan2(u, z), math.Asin(pixelfx.Clamp(v))
				// the lattice, and which cell this is: each cell keeps its own clock for blinking out
				a, b := (lon+lat)*GRID, (lon-lat)*GRID
				fa, fb := a-math.Floor(a), b-math.Floor(b)
				cell := pixelfx.Rand(math.Floor(a), math.Floor(b), 31)
				line := math.Min(math.Min(fa, 1-fa), math.Min(fb, 1-fb)) < 0.13
				gone := GONE + cell*0.35
				if t > gone {
					continue
				}
				// how far over the surface from the blow, and the two ripples running out from it
				d := math.Acos(pixelfx.ClampTo(math.Cos(lat)*math.Cos(hitLat)*math.Cos(lon-hitLon)+math.Sin(lat)*math.Sin(hitLat), -1, 1))
				hit := 0.0
				for _, rp := range ripples {
					t0, k0 := rp[0], rp[1]
					k := t - t0
					front := k * 3.6
					if k < 0 || k > 0.55 {
						continue
					}
					if math.Abs(d-front) < 0.2 {
						hit = math.Max(hit, k0*(1-k/0.55))
					} else if d < front {
						hit = math.Max(hit, 0.3*k0*(1-k/0.55))
					}
				}
				rim, flash, blink := 1-math.Abs(z), t < 0.05, t > gone-0.06
				sheen := math.Abs(math.Mod(math.Mod(lat*0.8-lon*0.3-t*1.6, 1)+1, 1)-0.5) < 0.05
				if back {
					if line {
						p.Dot(x, y, C[3], 0.5*fade)
					}
					continue
				}
				switch {
				case q > 0.9:
					c := C[6]
					if flash || hit > 0.5 {
						c = pixelfx.White
					}
					p.Dot(x, y, c, fade)
				case line:
					var c uint32
					switch {
					case flash || blink || hit > 0.5:
						c = pixelfx.White
					case hit > 0.2 || sheen:
						c = C[6]
					case rim > 0.7:
						c = C[5]
					case rim > 0.35:
						c = C[4]
					default:
						c = C[3]
					}
					p.Dot(x, y, c, fade)
				default:
					c := C[3]
					if flash || hit > 0.35 {
						c = C[6]
					}
					base := 0.1 + 0.25*rim*rim
					if flash {
						base = 0.35
					}
					p.Blend(x, y, c, fade*math.Min(0.85, base+hit*0.65))
				}
				if (int(x)*7+int(y)*3)&15 == 0 {
					p.Light(x, y, C[4], 0.15+0.5*hit)
				}
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// as cells blink out, little motes of light lift off
			if t > GONE && t < GONE+0.38 && R.Chance(0.5) {
				lon, lat := R.Range(0, pi/2), R.Range(0.05, 1.4)
				x, y := shieldOnDome(lon, lat)
				vx, vy, life := R.Range(-10, 10), -R.Range(10, 40), R.Range(0.2, 0.36)
				c := C[5]
				if R.Chance(0.5) {
					c = C[6]
				}
				ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, R.Range(0.6, 1.4), 0, 0)
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			dome(p, true)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			dome(p, false)
			// where the dome meets the floor
			if fade := 1 - pixelfx.Smooth(GONE+0.3, GONE+0.55, t); fade > 0 {
				c := C[5]
				if t < 0.05 {
					c = pixelfx.White
				}
				for x := CX; x <= CX+RX; x++ {
					p.Dot(x, 0, c, fade*(0.6+0.4*((x-CX)/RX)))
				}
			}
			// the blow: a hot star where it lands, then the sparks glancing off
			if t < 0.03 {
				star(p, hx, hy, sp, 1, 1, [4]uint32{S[3], S[0], S[1], pixelfx.White}, 1, 2)
			} else if t < 0.06 {
				star(p, hx, hy, sp, 1.25, 0.55, [4]uint32{S[3], S[1], S[2], S[0]}, 1, 2)
			}
			ps.Draw(p, pixelfx.LayerBoth)
			p.Glow(CX+RX*0.5, -RY*0.45, RY*0.9, C[4], 0.2*(1-pixelfx.Smooth(GONE, GONE+0.55, t))+0.35*math.Exp(-t/0.08))
			lights.Draw(p)
		},
	}
}
