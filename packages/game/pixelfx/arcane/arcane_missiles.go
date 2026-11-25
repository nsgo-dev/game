// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/arcane/arcane-missiles.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The bolt and its pop (a layered head with spinning rays, a flash disc, a cross, a ring, plasma sparks and stars) are adapted from Sprite Fusion's open-source energy bolt (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package arcane

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// the hand, the target, how many points each ribbon keeps; variables so expressions with them round step by step as JavaScript's do
var (
	missHX    = 15.0
	missHY    = -24.0
	missTX    = 152.0
	missTY    = -22.0
	missTrail = 18
)

var (
	missOpen   = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(4), pixelfx.FX.Is(18), pixelfx.FY.Is(-24), pixelfx.BX.Is(-6), pixelfx.BY.Is(-22), pixelfx.FFX.Is(11), pixelfx.FBX.Is(-9))
	missKick   = missOpen.With(pixelfx.FX.Is(15), pixelfx.FY.Is(-26), pixelfx.Lean.Is(2))
	missGather = pixelfx.Ready.With(pixelfx.X.Is(-3), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-2), pixelfx.FX.Is(6), pixelfx.FY.Is(-24), pixelfx.BX.Is(8), pixelfx.BY.Is(-24), pixelfx.FFX.Is(9), pixelfx.FBX.Is(-7))
)

// missHit is a small hit as one bolt lands.
func missHit(t float64) pixelfx.Hit {
	return pixelfx.Hit{T: t, Stop: 0.02, Push: pixelfx.Val(0.7), Straw: pixelfx.Val(4), Y: pixelfx.Val(-24), Flash: pixelfx.Val(0.03)}
}

// ArcaneMissiles fans a volley of violet bolts out of the hand; they curl round on twisting ribbons and pop on the target one after another. It is registered as "arcane/arcane-missiles".
var ArcaneMissiles = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Arcane missiles",
		Desc:     "A volley of violet bolts fans out of the hand, curls round on twisting ribbons and pops on the target one after another.",
		Tags:     []string{"arcane", "missile", "projectile", "homing", "volley", "magic", "directional", "one-shot"},
		Size:     [2]int{240, 116},
		Origin:   [2]int{26, 88},
		Duration: 1.75,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: arcanePalette()},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		Targets: []float64{152},
		Poses: []pixelfx.Key{
			{T: -0.52, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
			{T: -0.24, Pose: missGather},
			{T: 0, Pose: missGather.With(pixelfx.Crouch.Is(5))},
			{T: 0.05, Pose: missKick, How: "snap"},
			{T: 0.1, Pose: missOpen, How: "snap"},
			{T: 0.15, Pose: missKick, How: "snap"},
			{T: 0.2, Pose: missOpen, How: "snap"},
			{T: 0.25, Pose: missKick, How: "snap"},
			{T: 0.29, Pose: missOpen, How: "snap"},
			{T: 0.33, Pose: missKick, How: "snap"},
			{T: 0.38, Pose: missOpen, How: "snap"},
			{T: 0.43, Pose: missKick, How: "snap"},
			{T: 0.48, Pose: missOpen, How: "snap"},
			{T: 1.2, Pose: missOpen.With(pixelfx.Crouch.Is(3))},
			{T: 1.7, Pose: pixelfx.Ready.With(pixelfx.X.Is(-3))},
		},
		Hits: []pixelfx.Hit{
			missHit(0.77),
			missHit(0.85),
			missHit(0.95),
			missHit(1.08),
			{T: 1.2, Stop: 0.07, Shake: 0.14, Amp: pixelfx.Val(1), Push: pixelfx.Val(1.4), Straw: pixelfx.Val(9), Y: pixelfx.Val(-24)},
		},
	},
	New: newArcaneMissiles,
}

func init() { pixelfx.Register("arcane/arcane-missiles", ArcaneMissiles) }

type missBolt struct {
	t0, x, y, vx, vy, hit, tx, ty, tw, acc float64
	trail                                  [][2]float64
	on                                     bool
}

// missPop is where and when a bolt landed.
type missPop struct{ t, x, y float64 }

func newArcaneMissiles(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	// each bolt leaves the hand on its own heading, then homes in harder and faster until it lands
	fan := [5]float64{-1.05, 0.7, -0.5, 1.1, -1.4}
	bolts := make([]*missBolt, len(fan))
	for i, a := range fan {
		a += R.Range(-0.12, 0.12)
		sp := R.Range(130, 165)
		b := &missBolt{t0: 0.05 + float64(i)*0.1, x: missHX, y: missHY, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp, hit: -1}
		b.tx = missTX + R.Range(-3, 3)
		b.ty = missTY + R.Range(-6, 6)
		b.tw = R.Range(0, tau)
		bolts[i] = b
	}
	var pops []missPop
	t := 0.0

	ribbon := func(p *pixelfx.Painter, b *missBolt) {
		pts, n := b.trail, len(b.trail)
		fade := 1.0
		if b.hit >= 0 {
			fade = 1 - pixelfx.Clamp((t-b.hit)/0.16)
		}
		if n < 2 || fade <= 0 {
			return
		}
		fn := float64(n)
		for i := 0; i < n-1; i++ {
			fi := float64(i)
			x0, y0, x1, y1 := pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]
			L := or1(pixelfx.Hypot(x1-x0, y1-y0))
			nx, ny := -(y1-y0)/L, (x1-x0)/L
			for d := 0.0; d < L; d += 0.5 {
				u := (fi + d/L) / (fn - 1)
				x, y := x0+((x1-x0)*d)/L, y0+((y1-y0)*d)/L
				a := (1 - u) * 1.7 * fade
				// the body: a capsule that tapers down the tail, white along its middle near the head, cooling outward
				w := 2.6*math.Pow(1-u, 0.8) + 0.3
				for q := -w; q <= w; q += 0.5 {
					v := math.Abs(q) / w
					var c uint32
					switch {
					case v < 0.35:
						c = pick(u < 0.25, pixelfx.White, C[6])
					case v < 0.7:
						c = pick(u < 0.4, C[6], C[5])
					default:
						c = pick(u < 0.5, C[5], C[4])
					}
					p.Dot(x+nx*q, y+ny*q, c, a*pickF(v < 0.7, 1, 0.75))
				}
				// two strands twisting round it: bright where they come round the front, dim behind
				for _, s := range [2]float64{0, pi} {
					ph := u*10 - t*26 + b.tw + s
					off := math.Sin(ph) * (w + 1.4)
					if math.Cos(ph) > -0.2 {
						p.Dot(x+nx*off, y+ny*off, pick(math.Cos(ph) > 0.4, pick(u < 0.35, pixelfx.White, C[6]), C[4]), a)
					}
				}
			}
			if i%3 == 0 {
				p.Glow(x0, y0, 9*(1-fi/fn)+3, C[5], 0.3*(1-fi/fn)*fade)
			}
		}
	}

	head := func(p *pixelfx.Painter, b *missBolt) {
		x, y := b.x, b.y
		for i := range 4 {
			fi := float64(i)
			a, l := t*24+(fi*tau)/4+b.tw, 5.5+1.5*math.Sin(t*50+fi)
			p.Line(x+math.Cos(a)*3.2, y+math.Sin(a)*3.2, x+math.Cos(a)*l, y+math.Sin(a)*l, C[6], 1)
		}
		p.Circle(x, y, 3.8, C[4], 1)
		p.Circle(x, y, 3, C[5], 1)
		p.Circle(x, y, 2.1, C[6], 1)
		p.Circle(x, y, 1.2, pixelfx.White, 1)
		p.Glow(x, y, 24, C[5], 0.6)
		p.Glow(x, y, 7, pixelfx.White, 0.4)
	}

	pop := func(p *pixelfx.Painter, q, x, y float64) {
		if q < 0.16 {
			r := 9*(1-q/0.16) + 1.5
			p.Circle(x, y, r+1.5, C[5], 1)
			p.Circle(x, y, r, pixelfx.White, 1)
		}
		if q < 0.4 {
			l := 15*(1-q/0.4) + 3
			for d := -l; d <= l; d++ {
				a := math.Abs(d) / l
				c := C[5]
				if a < 0.3 {
					c = pixelfx.White
				} else if a < 0.65 {
					c = C[6]
				}
				p.Dot(x+d, y, c, 1.15-a)
				p.Dot(x, y+d*0.7, c, 1.15-a)
			}
		}
		rr := 3 + 18*pixelfx.Ease.OutCubic(q)
		p.Ring(x, y, rr+0.5, math.Max(1, 3*(1-q)), pick(q < 0.3, pixelfx.White, C[6]), 1.2-q)
	}

	// the lock: four brackets close on the target and pulse as each bolt lands
	lock := func(p *pixelfx.Painter) {
		k := pixelfx.Smooth(0, 0.22, t) * (1 - pixelfx.Smooth(1.15, 1.45, t))
		if k <= 0 {
			return
		}
		kick := 0.0
		for _, q := range pops {
			d := (t - q.t) / 0.06
			kick = math.Max(kick, math.Exp(-(d * d)))
		}
		r, spin := 9+10*(1-pixelfx.Ease.OutCubic(pixelfx.Smooth(0, 0.22, t)))+3*kick, t*1.5
		for i := range 4 {
			a0 := spin + (float64(i)*tau)/4 - 0.38
			for a := a0; a <= a0+0.76; a += 0.06 {
				p.Dot(missTX+math.Cos(a)*r, missTY+math.Sin(a)*r, pick(kick > 0.4, pixelfx.White, C[5]), k)
			}
		}
		p.Glow(missTX, missTY, 12, C[4], 0.2*k)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for _, b := range bolts {
				if t < b.t0 || b.hit >= 0 {
					continue
				}
				if !b.on {
					b.on = true
					lights.Add(missHX, missHY, 28, C[5], 0.7, 0.14)
					for range 4 {
						vx := math.Cos(math.Atan2(b.vy, b.vx)+R.Range(-0.6, 0.6)) * R.Range(60, 140)
						vy := math.Sin(math.Atan2(b.vy, b.vx)+R.Range(-0.6, 0.6)) * R.Range(60, 140)
						ps.Spawn(pixelfx.Mote, missHX, missHY, vx, vy, R.Range(0.12, 0.25), C[6], 0.6, 0, 0)
					}
				}
				age, dx, dy := t-b.t0, b.tx-b.x, b.ty-b.y
				d := or1(pixelfx.Hypot(dx, dy))
				sp, steer := 150+560*age*age, math.Min(1, (1.4+15*age)*dt)
				b.vx += ((dx/d)*sp - b.vx) * steer
				b.vy += ((dy/d)*sp - b.vy) * steer
				b.x += b.vx * dt
				b.y += b.vy * dt
				b.trail = append([][2]float64{{b.x, b.y}}, b.trail...)
				if len(b.trail) > missTrail {
					b.trail = b.trail[:len(b.trail)-1]
				}
				b.acc += dt
				if b.acc > 0.012 {
					b.acc = 0
					if R.Chance(0.6) {
						vx, vy := -b.vx*0.08+R.Range(-25, 25), -b.vy*0.08+R.Range(-25, 25)
						life := R.Range(0.2, 0.4)
						ps.Spawn(pixelfx.Mote, b.x, b.y, vx, vy, life, pick(R.Chance(0.5), C[6], C[5]), 0.55, 0, 0)
					}
				}
				if d < 3 || age > 1.3 {
					b.hit = t
					pops = append(pops, missPop{t, b.x, b.y})
					lights.Add(b.x, b.y, 70, C[5], 0.9, 0.3)
					lights.Add(b.x, b.y, 24, pixelfx.White, 0.85, 0.09)
					for range 16 {
						a, v := R.Range(0, tau), R.Range(80, 230)
						ps.Spawn(pixelfx.Spark, b.x, b.y, math.Cos(a)*v, math.Sin(a)*v-40, R.Range(0.15, 0.45), pixelfx.White, 1, 0, 0)
					}
					for i := range 7 {
						a, v := R.Range(-pi, 0.3), R.Range(40, 140)
						k := pixelfx.Mote
						if i%2 != 0 {
							k = pixelfx.StarKind
						}
						life := R.Range(0.25, 0.48)
						c := pixelfx.Pick(R, []uint32{pixelfx.White, C[6], C[5]})
						ps.Spawn(k, b.x, b.y, math.Cos(a)*v, math.Sin(a)*v, life, c, R.Range(0.8, 1.6), 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the floor catches the light of each pop
			for _, q := range pops {
				if k := 1 - pixelfx.Clamp((t-q.t)/0.5); k > 0 {
					p.Glow(q.x, -1, 22, C[5], 0.25*k*k)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			lock(p)
			for _, b := range bolts {
				if b.on {
					ribbon(p, b)
				}
			}
			for _, b := range bolts {
				if b.on && b.hit < 0 {
					head(p, b)
				}
			}
			for _, q := range pops {
				if k := (t - q.t) / 0.35; k < 1 {
					pop(p, k, q.x, q.y)
				}
			}
			if t < 0.55 {
				pixelfx.Sparkle(p, missHX, missHY, 3+2*math.Sin(t*40), C[6], C[5], 0.45*(1-t/0.55))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
