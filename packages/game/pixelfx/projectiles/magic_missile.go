// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/projectiles/magic-missile.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source energy bolt (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez, for the gathering motes, star particles and burst flare.

package projectiles

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// MagicMissile gathers motes into an arcane orb at a hand height px above the floor (the origin); it weaves to its target trailing a twisting ribbon and bursts into a star of light. It is registered as "projectiles/magic-missile".
var MagicMissile = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Magic missile",
		Desc:     "Motes gather into an arcane orb at a hand `height` px above the floor (the origin); it weaves to its target trailing a twisting ribbon and bursts into a star of light.",
		Tags:     []string{"projectile", "magic", "arcane", "missile", "spell", "directional", "one-shot"},
		Size:     [2]int{208, 112},
		Origin:   [2]int{24, 104},
		Duration: 1.9,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#140a26", "#2b1252", "#481c88", "#6b2cc0", "#9a48ec", "#c684ff", "#ecc8ff", "#ffffff")},
			{Name: "height", Default: pixelfx.Num(26)},
			{Name: "reach", Default: pixelfx.Num(112)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newMagicMissile,
}

func init() { pixelfx.Register("projectiles/magic-missile", MagicMissile) }

const (
	magicCharge = 0.2
	magicFly    = 0.62
)

type trailPoint struct{ x, y, t float64 }

func newMagicMissile(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	H, X1, R := o.Num("height"), o.Num("reach"), pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	moteCols := []uint32{C[5], C[6], pixelfx.White}
	flyMoteCols := []uint32{C[4], C[5], C[6]}
	starCols := []uint32{C[6], pixelfx.White, C[5]}

	// the path: out of the hand, weaving up and down twice, homing in on the target at the same height
	pos := func(u float64) (float64, float64) {
		return X1*pixelfx.Ease.InOutSine(u) + math.Sin(u*pi)*6, -H + math.Sin(u*tau*1.5)*9*math.Sin(u*pi)
	}
	t, burst := 0.0, false
	var trail []trailPoint
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < magicCharge {
				// motes spiral in to the hand
				if R.Chance(0.8) {
					a, d := R.Range(0, tau), R.Range(12, 26)
					sp := d / 0.14
					ps.Spawn(pixelfx.Mote, math.Cos(a)*d, -H+math.Sin(a)*d, -math.Cos(a)*sp-math.Sin(a)*sp*0.5, -math.Sin(a)*sp+math.Cos(a)*sp*0.5, 0.14, pixelfx.Pick(R, moteCols), 0.8, 0, 0)
				}
			} else if t < magicCharge+magicFly {
				u := (t - magicCharge) / magicFly
				x, y := pos(u)
				trail = append(trail, trailPoint{x: x, y: y, t: t})
				if R.Chance(0.6) {
					ps.Spawn(pixelfx.Mote, x, y, R.Range(-50, 50), R.Range(-50, 50), R.Range(0.2, 0.4), pixelfx.Pick(R, flyMoteCols), 0.7, 0, 0)
				}
				if R.Chance(0.25) {
					ps.Spawn(pixelfx.StarKind, x+R.Range(-3, 3), y+R.Range(-3, 3), R.Range(-10, 10), R.Range(-10, 10), R.Range(0.25, 0.5), C[6], R.Range(0.6, 1.6), 0, 0)
				}
			} else if !burst {
				burst = true
				y := -H
				lights.Add(X1, y, 52, C[4], 0.95, 0.55)
				lights.Add(X1, y, 22, pixelfx.White, 0.8, 0.12)
				for i := range 26 {
					a := R.Range(0, tau)
					sp := R.Range(60, 150) * (1 - 0.65*math.Max(0, math.Sin(a)))
					kind := pixelfx.StarKind
					if i%3 != 0 {
						kind = pixelfx.Mote
					}
					ps.Spawn(kind, X1, y, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.25, 0.45), pixelfx.Pick(R, moteCols), R.Range(0.8, 1.8), 0, 0)
				}
				for range 18 {
					a := R.Range(0, tau)
					sp := R.Range(110, 240) * (1 - 0.5*math.Max(0, math.Sin(a)))
					ps.Spawn(pixelfx.Spark, X1, y, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
				}
				for range 14 {
					a, d := R.Range(0, tau), R.Range(4, 22)
					ps.Spawn(pixelfx.StarKind, X1+math.Cos(a)*d, y+math.Sin(a)*d*0.8, 0, -R.Range(2, 8), R.Range(0.5, 1.1), pixelfx.Pick(R, starCols), R.Range(0.8, 2.6), R.Range(0.05, 0.45), 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the ribbon: a strip that twists as it trails the orb, its bright face and its dark back taking turns, fading from the tail
			now := t
			var keep []trailPoint
			for _, q := range trail {
				if now-q.t < 0.32 {
					keep = append(keep, q)
				}
			}
			for i := 1; i < len(keep); i++ {
				a, b := keep[i-1], keep[i]
				dx, dy := b.x-a.x, b.y-a.y
				l := pixelfx.Hypot(dx, dy)
				if l == 0 {
					l = 1
				}
				nx, ny := -dy/l, dx/l
				for f := 0.0; f < 1; f += 0.5 / l {
					tt := a.t + (b.t-a.t)*f
					k := 1 - (now-tt)/0.32
					tw := math.Sin(tt * 26)
					w := 3.2*k*math.Abs(tw) + 0.4
					face := tw > 0
					x, y := a.x+dx*f, a.y+dy*f
					al := 1.0
					if k <= 0.35 {
						al = k / 0.35
					}
					for s := -w; s <= w; s += 0.5 {
						edge := math.Abs(s) > w-0.8
						var c uint32
						if face {
							if edge {
								c = C[6]
							} else if k > 0.6 {
								c = C[5]
							} else {
								c = C[4]
							}
						} else if edge {
							c = C[4]
						} else {
							c = C[2]
						}
						p.Dot(x+nx*s, y+ny*s, c, al)
					}
				}
				if i%3 == 0 {
					p.Glow(b.x, b.y, 6, C[4], 0.25*(1-(now-b.t)/0.32))
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			// the orb: gathering at the hand, then flying, its heart white, a ring of light turning round it
			if t < magicCharge+magicFly {
				k := 1.0
				var x, y float64
				if t < magicCharge {
					k = t / magicCharge
					x, y = 0, -H
				} else {
					x, y = pos((t - magicCharge) / magicFly)
				}
				r := 1 + 3*pixelfx.Ease.OutBack(k)
				p.Circle(x, y, r+1.4, C[3], 1)
				p.Circle(x, y, r+0.4, C[5], 1)
				p.Circle(x-0.4, y-0.4, r*0.55, pixelfx.White, 1)
				for i := range 3 {
					a := t*14 + (float64(i)*tau)/3
					p.Px(x+math.Cos(a)*(r+3), y+math.Sin(a)*(r+3)*0.6, pick(i != 0, C[6], pixelfx.White))
				}
				p.Glow(x, y, 20, C[4], 0.65*k)
				p.Glow(x, y, 7, C[6], 0.5*k)
			}
			// the burst: a star flare and a ring that open out and fade
			q := t - magicCharge - magicFly
			if q >= 0 && q < 0.5 {
				e := q / 0.5
				if e < 0.5 {
					pixelfx.Flare(p, X1, -H, 26*math.Pow(1-e*2, 1.2)+2, 1-e*2, C)
				}
				if e < 0.12 {
					p.Circle(X1, -H, 6*(1-e/0.12)+1, pixelfx.White, 1)
				}
				p.Ring(X1, -H, 4+20*pixelfx.Ease.OutCubic(e), math.Max(1, 2.5*(1-e)), pick(e < 0.3, C[6], C[5]), math.Min(1, (1-e)*1.5))
				if e < 0.4 {
					pixelfx.Sparkle(p, X1, -H, 7*(1-e/0.4), C[6], C[4], 0.6*(1-e/0.4))
				}
			}
			lights.Draw(p)
		},
	}
}
