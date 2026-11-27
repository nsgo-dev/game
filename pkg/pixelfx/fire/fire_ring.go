// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/fire/fire-ring.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the flame-puff, smoke and spark recipe and the floor ring from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), and the tongue from its fire (https://www.spritefusion.com/pixel-art-vfx/fire), MIT License, Copyright (c) 2026 Hugo Duprez.

package fire

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// FireRing is a white flash at the feet, then flames race outward along the floor in a ring, flaring as the front passes and burning down behind it over a smouldering scorch. A character acts it out, slamming its hands down. It is registered as "fire/fire-ring".
var FireRing = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fire ring",
		Desc:     "A white flash at the feet, then flames race outward along the floor in a ring, flaring as the front passes and burning down behind it over a smouldering scorch.",
		Tags:     []string{"fire", "ring", "nova", "aoe", "spell", "ground", "one-shot"},
		Size:     [2]int{224, 128},
		Origin:   [2]int{112, 92},
		Duration: ringDuration,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#8a1f17", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "reach", Default: pixelfx.Num(92)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{70},
		Poses: []pixelfx.Key{
			{T: -0.32, Pose: pixelfx.Ready},
			{T: 0.02, Pose: ringHigh},
			{T: 0.27, Pose: ringHigh.With(pixelfx.Y.Is(-1))},
			{T: 0.32, Pose: ringSlam, How: "snap"},
			{T: 0.85, Pose: ringSlam.With(pixelfx.Crouch.Is(7))},
			{T: 1.5, Pose: ringEase},
			{T: 2.4, Pose: pixelfx.Ready},
			{T: 3.9, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: 0.32, Stop: 0.1, Shake: 0.28, Amp: pixelfx.Val(3), NoTarget: true},
			{T: 0.6, Push: pixelfx.Val(1.2), Straw: pixelfx.Val(10), Y: pixelfx.Val(-8), Skin: "char"},
			{T: 3.5, Push: pixelfx.Val(0), Straw: pixelfx.Val(0), Flash: pixelfx.Val(0), Skin: pixelfx.SkinNone},
		},
		Dust: []pixelfx.DustKick{{T: 0.32, X: 10, N: 6, Dir: 1}, {T: 0.32, X: -10, N: 6, Dir: -1}},
	},
	New: newFireRing,
}

func init() { pixelfx.Register("fire/fire-ring", FireRing) }

const ringDuration = 3.9

var (
	ringHigh = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(0), pixelfx.FX.Is(-7), pixelfx.FY.Is(-44), pixelfx.BX.Is(8), pixelfx.BY.Is(-44), pixelfx.FFX.Is(-5), pixelfx.FBX.Is(6))
	ringSlam = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(8), pixelfx.Lean.Is(3), pixelfx.FX.Is(-9), pixelfx.FY.Is(-8), pixelfx.BX.Is(11), pixelfx.BY.Is(-8), pixelfx.FFX.Is(-8), pixelfx.FBX.Is(9))
	ringEase = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-17), pixelfx.BX.Is(9), pixelfx.BY.Is(-17), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5))
)

// ringSY squashes the ring onto the floor; ringCharge is when it goes off and ringSpread how long its front takes to run out. They are variables so sums with them round as pixelfx's do.
var ringSY, ringCharge, ringSpread = 0.2, 0.32, 0.75

// emberLayers are the dying flames: deep orange and red, no white heart.
var emberLayers = []layer{{1, 1, 7}, {0.7, 0.8, 6}, {0.4, 0.55, 5}}

// ringMaxReach is the largest reach the fire ring draws, in pixels.
const ringMaxReach = 256

// ringSite is one flame of the ring: where it stands, whether it is on the far half, when the front lights it, how long it burns, its height and half-width and its noise seed.
type ringSite struct {
	x, y              float64
	back              bool
	t0, life, h, w, n float64
}

func newFireRing(o *pixelfx.Options) pixelfx.Instance {
	F := pixelfx.Fit(o.Palette(), 9)
	// A deliberate departure from pixelfx: reach is capped at ringMaxReach, since the flames and the scorched floor grow with its square and a huge reach would exhaust memory and time, while the ring is already off the 224 by 128 canvas well before the cap.
	H, REACH, R, S := hotFirst(F), math.Min(o.Num("reach"), ringMaxReach), pixelfx.NewRng(o.Seed()), o.Seed()*7
	D, SY, CHARGE, SPREAD := float64(ringDuration), ringSY, ringCharge, ringSpread
	floor := func(float64) float64 { return 0 }
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{H[1], H[2], H[4], H[5]}}), &pixelfx.Lights{}
	GLOW := pixelfx.Mix(pixelfx.Mix(H[3], H[4], 0.5), pixelfx.White, 0.25)
	front := func(t float64) float64 { return REACH * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-CHARGE)/SPREAD)) }
	reachedAt := func(d float64) float64 { return CHARGE + SPREAD*(1-math.Cbrt(1-math.Min(1, d/REACH))) }
	// the flames: rings of tongues on the floor, each lit as the front reaches it, flaring then burning down
	var sites []ringSite
	for r := 10.0; r <= REACH; r += 9 {
		per := tau * math.Sqrt((r*r+math.Pow(r*SY, 2))/2)
		n := math.Max(5, pixelfx.Round(per/8.5))
		for k := 0.0; k < n; k++ {
			a := ((k + R.Range(0.15, 0.85)) / n) * tau
			rr := r + R.Range(-3, 3)
			q := math.Min(1, rr/REACH)
			// near the middle they flash and go out as the front passes; the rim, where it stops, burns on and down
			s := ringSite{x: math.Cos(a) * rr, y: math.Sin(a) * rr * SY, back: math.Sin(a) < 0}
			s.t0 = reachedAt(rr) + R.Range(0, 0.04)
			s.life = (0.3 + 1.0*q*q) * R.Range(0.85, 1.2)
			s.h = R.Range(9, 15) * (0.6 + 0.55*q)
			s.w = R.Range(1.8, 2.7) * (0.75 + 0.3*q)
			s.n = R.Int(0, 1e5)
			sites = append(sites, s)
		}
	}
	slices.SortStableFunc(sites, func(a, b ringSite) int { return cmpAsc(a.y, b.y) })
	t, burst := 0.0, false

	NZ := func(rate, seed, off float64) float64 { return pixelfx.Noise(t*rate+off, seed) }
	flames := func(p *pixelfx.Painter, back bool) {
		for i := range sites {
			s := &sites[i]
			if s.back != back {
				continue
			}
			age := t - s.t0
			if age < 0 || age > s.life {
				continue
			}
			u, rise := age/s.life, pixelfx.OutBack(pixelfx.Clamp(age/0.08), 2)
			fade := 1 - math.Pow(math.Max(0, (u-0.1)/0.9), 1.3)
			h := s.h * rise * fade * (0.85 + 0.15*pixelfx.Noise(t*3.3, s.n)) * (1 + 0.5*math.Max(0, pixelfx.Noise(t*2.6, s.n+70)-0.55))
			if h < 1 {
				continue
			}
			// fresh flames white-hot, then cooling as they burn down; the far ones a step dimmer
			L := emberLayers
			switch {
			case u < 0.3 && back:
				L = backLayers
			case u < 0.3:
				L = layers
			case u < 0.65:
				L = backLayers
			}
			flameTongue(p, s.x, s.y, h, s.w*(0.6+0.4*fade), NZ(2.2, s.n+40, 0)*1.5-s.x*0.02, NZ, s.n, H, L, 0.5, 1.1)
		}
	}
	// the scorch: a translucent stain of soot where the fire has crossed, sparse embers cooling in it
	scorch := func(p *pixelfx.Painter) {
		fr, end := front(t), 1-pixelfx.Smooth(D-1.2, D-0.15, t)
		if fr < 1 || end <= 0 {
			return
		}
		for y := math.Floor(-fr * SY); y <= math.Ceil(fr*SY); y++ {
			for x := math.Floor(-fr); x <= math.Ceil(fr); x++ {
				d := pixelfx.Hypot(x+0.5, (y+0.5)/SY)
				if d > fr {
					continue
				}
				since, h := t-reachedAt(d), pixelfx.Rand(x, y, S)
				hot := 1 - since/(1.2+h*0.9)
				if hot > 0 && h < 0.03+0.06*hot {
					c := H[6]
					switch {
					case hot > 0.7:
						c = H[3]
					case hot > 0.45:
						c = H[4]
					case hot > 0.2:
						c = H[5]
					}
					p.Px(x, y, c)
					p.Light(x, y, H[4], 0.3*hot)
					continue
				}
				c := soot2
				if h < 0.5 {
					c = soot
				}
				p.Blend(x, y, c, math.Min(1, since*3)*(0.55-(d/REACH)*0.25)*end)
			}
		}
	}
	ringRamp := []uint32{H[6], H[4], H[2], H[1]}
	ring := func(p *pixelfx.Painter, back bool) {
		k := (t - CHARGE) / (SPREAD + 0.1)
		if k < 0 || k >= 1 {
			return
		}
		a := 1.0
		if k >= 0.75 {
			a = (1 - k) / 0.25
		}
		pixelfx.Band(p, 0, 0, front(t)+1.5, SY, math.Max(1.5, 6*(1-k)), a, ringRamp, func(g float64) bool { return (math.Sin(g) < 0) == back }, k < 0.12)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the charge: streaks of fire spiral in to the feet, heat building there
			if t < CHARGE-0.05 && R.Chance(0.9*dt*60) {
				a, d, T := R.Range(0, tau), R.Range(26, 54), R.Range(0.18, 0.26)
				v := (d * 3) / (math.Exp(3*T) - 1)
				x, y := math.Cos(a)*d, math.Min(-2, -6+math.Sin(a)*d*0.45)
				l := pixelfx.Hypot(x, y+4)
				if l == 0 {
					l = 1
				}
				c := H[3]
				if R.Chance(0.5) {
					c = H[2]
				}
				ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, ((-4-y)/l)*v, T, c, 1, 0, 0)
			}
			if t < CHARGE && R.Chance(0.5*dt*60*(t/CHARGE)) {
				ps.Spawn(pixelfx.Fire, R.Range(-4, 4), -1, R.Range(-8, 8), -R.Range(20, 40), R.Range(0.12, 0.24), 0, R.Range(1, 2.2), 0, 0)
			}
			if !burst && t >= CHARGE {
				burst = true
				lights.Add(0, -18, 64, warm, 0.85, 0.5)
				lights.Add(0, -6, 30, pixelfx.White, 0.8, 0.08)
				for range 40 {
					a, sp := R.Range(0, tau), R.Range(80, 220)
					ps.Spawn(pixelfx.Spark, math.Cos(a)*3, -2, math.Cos(a)*sp, math.Sin(a)*sp*0.4-R.Range(40, 120), R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
				}
			}
			// fire shed off the burning ring: puffs that rise and some cool into smoke, and embers
			if t > CHARGE && t < CHARGE+SPREAD+1.2 {
				for range 3 {
					if !R.Chance(0.75 * dt * 60) {
						continue
					}
					// A deliberate departure from pixelfx, which throws here when reach is under 10 and there are no flames: shed nothing instead. The check comes after the chance roll, so the draws at any other reach are unchanged.
					if len(sites) == 0 {
						continue
					}
					s := pixelfx.Pick(R, sites)
					age := t - s.t0
					if age < 0 || age > s.life*0.7 {
						continue
					}
					var back pixelfx.Flag
					if s.back {
						back = pixelfx.Back
					}
					x, y, vx, vy, life, size := s.x+R.Range(-1, 1), s.y-s.h*0.6, R.Range(-5, 5), -R.Range(20, 40), R.Range(0.2, 0.4), R.Range(1.2, 2.4)
					flag := back
					if R.Chance(0.5) {
						flag = pixelfx.Smokes | back
					}
					ps.Spawn(pixelfx.Fire, x, y, vx, vy, life, 0, size, 0, flag)
					if R.Chance(0.12) {
						ps.Spawn(pixelfx.Ember, s.x, s.y-s.h*0.5, R.Range(-15, 15), -R.Range(30, 70), R.Range(0.6, 1.3), H[3], 1, 0, back)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			scorch(p)
			ring(p, true)
			flames(p, true)
			simDraw(p, ps, H, pixelfx.LayerBack)
			// light on the floor running with the front
			fr, k := front(t), 0.0
			if t >= CHARGE {
				k = 1 - pixelfx.Smooth(CHARGE+SPREAD, CHARGE+SPREAD+1.4, t)
			}
			if k > 0 {
				for i := range 16 {
					a := (float64(i) / 16) * tau
					p.Glow(math.Cos(a)*fr, math.Sin(a)*fr*SY-3, 16, GLOW, 0.2*k)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ring(p, false)
			flames(p, false)
			simDraw(p, ps, H, pixelfx.LayerFront)
			// the charge glowing at the feet, then the white flash as it goes
			if t < CHARGE {
				p.Glow(0, -3, 8+22*(t/CHARGE), GLOW, 0.6*(t/CHARGE))
			}
			if t >= CHARGE && t < CHARGE+0.1 {
				k := 1 - (t-CHARGE)/0.1
				p.Ellipse(0, -1, 14*(0.6+0.4*k), 4, H[2], 1)
				c := H[1]
				if k > 0.5 {
					c = pixelfx.White
				}
				p.Ellipse(0, -1, 9*(0.6+0.4*k), 2.6, c, 1)
			}
			if k := pixelfx.Smooth(CHARGE-0.05, CHARGE, t) * (1 - pixelfx.Smooth(CHARGE+SPREAD*0.5, CHARGE+SPREAD+1.2, t)); k > 0 {
				p.Glow(0, -6, 92, GLOW, 0.3*k)
			}
			lights.Draw(p)
		},
	}
}
