// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/zap-hit.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The arc's halo-body-core inking, its flicker and the light that strobes as it dies follow Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// ZapHit is a contact zap: an arc snaps in from the left, the hit point overloads into a knot of tiny crackling zigzags, a fern of burn marks glows outward, sparks jink away and ozone puffs. It is registered as "lightning/zap-hit".
var ZapHit = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Zap hit",
		Desc:     "A contact zap: an arc snaps in from the left, the hit point overloads into a knot of tiny crackling zigzags, a fern of burn marks glows outward, sparks jink away, ozone puffs.",
		Tags:     []string{"lightning", "electric", "hit", "impact", "combat", "directional", "one-shot"},
		Size:     [2]int{144, 112},
		Origin:   [2]int{76, 56},
		Duration: 0.9,
		Loop:     false,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(volt...)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newZapHit,
}

func init() { pixelfx.Register("lightning/zap-hit", ZapHit) }

// zhSnap is when the arc lands, a variable so sums with it round as JavaScript's do.
var zhSnap = 0.05

var zhAsh = pixelfx.Hex("#6a6680")

// zhFern grows the burn the current leaves on what it hit: a Lichtenberg fern, main fronds radiating, side fronds alternating.
func zhFern(R *pixelfx.Rng) []branch {
	var out []branch
	var grow func(x, y, a, n, w, d float64, depth int)
	grow = func(x, y, a, n, w, d float64, depth int) {
		pts := []branchPoint{{x, y, d}}
		side := R.Sign()
		for s := 0.0; s < n; s += 1.4 {
			a += R.Range(-0.3, 0.3)
			x += math.Cos(a) * 1.4
			y += math.Sin(a) * 1.4
			d += 1.4
			pts = append(pts, branchPoint{x, y, d})
			if depth < 2 && s > 2 && R.Chance(0.24) {
				side = -side
				grow(x, y, a+side*R.Range(0.55, 1.05), (n-s)*R.Range(0.3, 0.55), w*0.6, d, depth+1)
			}
		}
		out = append(out, branch{pts: pts, w: w})
	}
	for i := range 6 {
		grow(0, 0, (float64(i)/6)*tau+R.Range(-0.35, 0.35), R.Range(18, 32), 1, 0, 0)
	}
	return out
}

// zhSpark is a spark that jinks: it flies out and jerks to a new heading every few hundredths of a second.
type zhSpark struct {
	x, y, vx, vy, life, age, next float64
	trail                         [][2]float64
}

func newZapHit(o *pixelfx.Options) pixelfx.Instance {
	C, seed := pixelfx.Fit(o.Palette(), 8), o.Seed()
	R := pixelfx.NewRng(seed)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R})
	lights := &pixelfx.Lights{}
	tree := zhFern(R)
	Y0 := R.Range(-14, 6)
	kinked := make([]zhSpark, 16)
	for i := range kinked {
		a := R.Range(-1.2, 1.2)
		if !R.Chance(0.7) {
			a += pi
		}
		sp := R.Range(130, 250)
		kinked[i] = zhSpark{vx: math.Cos(a) * sp, vy: math.Sin(a)*sp - 40, life: R.Range(0.2, 0.36), age: -zhSnap, next: R.Range(0.02, 0.05)}
	}
	held, landed := zhSnap+0.034, zhSnap+0.07
	t, hit := 0.0, false
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !hit && t >= zhSnap {
				hit = true
				lights.Add(0, 0, 60, C[4], 0.7, 0.3)
				lights.Add(0, 0, 14, pixelfx.White, 0.5, 0.07)
				for range 3 {
					ps.Spawn(pixelfx.Smoke, R.Range(-3, 5), R.Range(-4, 2), R.Range(-5, 8), -R.Range(8, 16), R.Range(0.55, 0.75), pixelfx.Mix(zhAsh, C[4], 0.3), R.Range(2.5, 3.8), R.Range(0.08, 0.16), 0)
				}
			}
			for i := range kinked {
				s := &kinked[i]
				if s.age += dt; s.age < 0 || s.age >= s.life {
					continue
				}
				if s.next -= dt; s.next <= 0 {
					s.next = R.Range(0.025, 0.055)
					sp := pixelfx.Hypot(s.vx, s.vy)
					a := math.Atan2(s.vy, s.vx) + R.Sign()*R.Range(0.6, 1.3)
					s.vx = math.Cos(a) * sp
					s.vy = math.Sin(a) * sp
					s.trail = append(s.trail, [2]float64{s.x, s.y})
					if len(s.trail) > 4 {
						s.trail = s.trail[1:]
					}
				}
				s.vx -= s.vx * 3 * dt
				s.vy -= s.vy*3*dt - 140*dt
				s.x += s.vx * dt
				s.y += s.vy * dt
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the fern burns outward from the hit, glowing white, cooling through the palette, then fading
			if t < zhSnap {
				return
			}
			// it cools by stepping down the palette, and only the last embers dither away
			cool, fade := pixelfx.Smooth(0.45, 0.85, t), 1-pixelfx.Smooth(0.78, 0.88, t)
			for _, br := range tree {
				for i := 1; i < len(br.pts); i++ {
					a, b := br.pts[i-1], br.pts[i]
					age := t - zhSnap - b.d/260
					if age < 0 {
						break
					}
					k := math.Exp(-age/0.22) - cool*0.3
					var c uint32
					switch {
					case k > 0.75:
						c = pixelfx.White
					case k > 0.45:
						c = C[6]
					case k > 0.25:
						c = C[5]
					case k > 0.1:
						c = C[4]
					case k > -0.05:
						c = C[2]
					default:
						c = C[1]
					}
					p.Line(a.x, a.y, b.x, b.y, c, fade)
					if br.w >= 1 && b.d < 9 {
						c2 := C[2]
						if k > 0.45 {
							c2 = C[5]
						}
						p.Line(a.x+1, a.y, b.x+1, b.y, c2, fade)
					}
					if i&3 == 0 && k > 0 {
						p.Glow(b.x, b.y, 5, C[5], 0.35*k*fade)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f := int(math.Floor(t*30 + 1e-4))
			// the arc snaps in from the left in two frames, holds and flickers, then lets go
			if t < 0.32 {
				r := pixelfx.RngOf(seed, 2, float64(f))
				b := zigzag(r, -66, Y0, 0, 0, 0.22, 0.35)
				u := pixelfx.Clamp(t / zhSnap)
				var k float64
				switch {
				case t < zhSnap:
					k = 1
				case t < 0.2 && f%2 != 0:
					k = 0.7
				case t < 0.2:
					k = 1
				default:
					k = 0.55 * (1 - (t-0.2)/0.12)
				}
				drawn := b
				if u < 1 {
					drawn = b[:1]
				}
				s := 0.7
				if t < held {
					s = 0.95
				}
				stroke(p, drawn, k, C, s, strokeOpts{upTo: pixelfx.Val(u), hero: t >= zhSnap && t < held})
				if u < 1 {
					tp := b[0].pts
					h := tp[int(math.Floor(u*float64(len(tp)-1)))]
					p.Circle(h[0], h[1], 2, pixelfx.White, 1)
					p.Glow(h[0], h[1], 14, C[5], 0.6)
				}
			}
			// the hit point overloads: a tight knot of tiny zigzags re-struck every frame round a white heart
			if t >= zhSnap && t < 0.45 {
				a := (t - zhSnap) / 0.4
				r := pixelfx.RngOf(seed, 5, float64(f))
				R0, m := 8*(1-a*0.6), int(pixelfx.Round(11*(1-a*0.5)))
				for range m {
					ang, d0 := r.Range(0, tau), r.Range(0.5, R0*0.5)
					ang2, d1 := ang+r.Range(-1.2, 1.2), r.Range(R0*0.55, R0)
					crackle(p, zigzag(r, math.Cos(ang)*d0, math.Sin(ang)*d0, math.Cos(ang2)*d1, math.Sin(ang2)*d1, 0.5, 0), 0.75*(1-a)+0.25, C, 0.25, 1)
				}
				p.Circle(0, 0, 3*(1-a)+1, C[5], 1)
				p.Circle(0, 0, 2*(1-a)+0.5, pixelfx.White, 1)
				p.Glow(0, 0, 20, C[4], 0.55*(1-a))
				if t < landed {
					p.Glow(0, 0, 9, pixelfx.White, 0.4)
				}
			}
			// the sparks: a bright head, the zigzag behind it cooling
			for _, s := range kinked {
				if s.age < 0 || s.age >= s.life {
					continue
				}
				k := 1 - s.age/s.life
				path := append(append(make([][2]float64, 0, len(s.trail)+1), s.trail...), [2]float64{s.x, s.y})
				for i := 0; i < len(path)-1; i++ {
					var c uint32
					switch {
					case i == len(path)-2 && k > 0.5:
						c = pixelfx.White
					case i == len(path)-2:
						c = C[6]
					case k > 0.5:
						c = C[5]
					default:
						c = C[4]
					}
					p.Line(path[i][0], path[i][1], path[i+1][0], path[i+1][1], c, 0.6+0.4*(float64(i)/float64(len(path))))
				}
				c := C[5]
				if k > 0.4 {
					c = pixelfx.White
				}
				p.Px(s.x, s.y, c)
				p.Glow(s.x, s.y, 4, C[5], 0.4*k)
			}
			// the light strobes as it dies
			if t >= zhSnap {
				strobe := 1.0
				if f%2 != 0 {
					strobe = 0.7
				}
				p.Glow(0, 0, 40*math.Max(0, 1-t/0.85), C[3], 0.45*math.Exp(-(t-zhSnap)*5)*strobe)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
