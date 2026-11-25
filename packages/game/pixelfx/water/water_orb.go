// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/water/water-orb.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source energy orb (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez, for the orb's build (a see-through body with a lit rim, an inner current drawn as streaks on a tilted plane, motes on tilted orbits split behind and in front, a pulsing light).

package water

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// WaterOrb is a floating, wobbling sphere of water: a current circles inside it, light gathers on the floor under it, and drips fall and plink. It is registered as "water/water-orb".
var WaterOrb = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Water orb",
		Desc:     "A sphere of water floats and wobbles: a current circles inside it, the floor shows upside down through it, it shines, light gathers on the floor under it, and drips fall and plink.",
		Tags:     []string{"water", "orb", "sphere", "magic", "float", "ambient", "loop"},
		Size:     [2]int{100, 108},
		Origin:   [2]int{50, 100},
		Duration: waterOrbDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: waterPal},
			{Name: "radius", Default: pixelfx.Num(19)},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	New: newWaterOrb,
}

func init() { pixelfx.Register("water/water-orb", WaterOrb) }

const (
	waterOrbDuration = 3
	waterOrbSY       = 0.3
	waterOrbG        = 700
	waterOrbDrip     = 1.5
	waterOrbLet      = 0.5
)

func waterOrbCenter(ph float64) (float64, float64) {
	return 0, -52 + math.Sin(ph*tau)*2
}

func waterOrbSkin(a, R, ph float64) float64 {
	u := math.Mod(ph*waterOrbDuration, waterOrbDrip) / waterOrbDrip
	sag := 0.0
	if u < waterOrbLet {
		sag = math.Pow(math.Max(0, math.Sin(a)), 6) * 2.2 * pixelfx.Smooth(0.1, waterOrbLet, u)
	}
	return R + math.Sin(a*3+ph*tau*2)*0.6 + math.Sin(a*5-ph*tau*3)*0.35 + sag
}

type orbMote struct {
	x, y, z float64
	i       int
}

func waterOrbMotes(ph, R float64) [7]orbMote {
	var out [7]orbMote
	for i := range 7 {
		dir := -1.0
		if i%2 != 0 {
			dir = 1
		}
		a := (float64(i)/7)*tau + ph*tau*dir
		r := R + 5 + float64((i%3)*3)
		tilt := float64(i-3) * 0.22
		x, z := math.Cos(a)*r, math.Sin(a)
		out[i] = orbMote{x: x * math.Cos(tilt), y: x*math.Sin(tilt) + z*r*0.28, z: z, i: i}
	}
	return out
}

func newWaterOrb(o *pixelfx.Options) pixelfx.Instance {
	C, R, seed := pixelfx.Fit(o.Palette(), 8), o.Num("radius"), o.Seed()
	D := float64(waterOrbDuration)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			cx, cy := waterOrbCenter(ph)
			// light gathers on the floor under it, the orb's caustic
			p.Glow(cx, 0, 22, C[5], 0.32)
			p.Shade(cx-9, -2, cx+9, 2, func(x, y float64, out *pixelfx.Out) uint32 {
				dx, dy := x-cx, y*2.5
				d := (dx*dx)/64 + (dy*dy)/9
				if d > 1 {
					return 0
				}
				switch {
				case d < 0.25:
					out.C = pixelfx.White
				case d < 0.6:
					out.C = C[6]
				default:
					out.C = C[5]
				}
				out.A = 1.2 - d
				return 0
			})
			for _, m := range waterOrbMotes(ph, R) {
				if m.z < 0 {
					p.Px(cx+m.x, cy+m.y, C[4])
				}
			}
			p.Glow(cx, cy, R*3.2, C[4], 0.3+0.05*math.Sin(ph*tau*2))
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / D
			cx, cy := waterOrbCenter(ph)
			T := ph * tau
			// the body: see-through water, deeper toward the bottom
			p.Shade(cx-R-4, cy-R-4, cx+R+4, cy+R+6, func(x, y float64, out *pixelfx.Out) uint32 {
				dx, dy := x-cx, y-cy
				d := pixelfx.Hypot(dx, dy)
				a := math.Atan2(dy, dx)
				r := waterOrbSkin(a, R, ph)
				if d > r {
					return 0
				}
				q := d / r
				lit := (-dx - dy) / sqrt2 / math.Max(0.5, d)
				if q > 0.9 {
					switch {
					case lit > 0.35:
						out.C = pixelfx.White
					case lit > -0.2:
						out.C = C[5]
					default:
						out.C = C[3]
					}
					return 0
				}
				// a lens of water: dark on the side the light comes from, the light gathering low on the far side inside
				gather := math.Max(0, (dx+dy)/sqrt2/r) * q
				var c uint32
				switch {
				case gather > 0.45:
					c = C[4]
				case gather > 0.25:
					c = C[3]
				case q > 0.7:
					c = C[2]
				default:
					c = C[1]
				}
				alpha := 0.35 + 0.3*q
				if gather > 0.25 {
					alpha = 0.7
				}
				p.Blend(x, y, c, alpha)
				return 0
			})
			// the current inside: streaks circling on a tilted plane, bright where they pass in front
			for k := 0; k < 3; k++ {
				rr := R * (0.38 + 0.2*float64(k))
				tilt := -0.5 + float64(k)*0.35
				dir := 3.0
				if k%2 != 0 {
					dir = -2
				}
				spin := T*dir + float64(k)*2
				for s := 0.0; s < 1; s += 0.012 {
					a := spin + s*tau
					x, z := math.Cos(a)*rr, math.Sin(a)
					y := x*math.Sin(tilt)*0.6 + z*rr*0.62
					dash := math.Mod(s*3+float64(k)*0.3, 1)
					if dash > 0.62 {
						continue
					}
					c := C[3]
					if z > 0 {
						switch {
						case dash < 0.1:
							c = pixelfx.White
						case dash < 0.4:
							c = C[6]
						default:
							c = C[5]
						}
					}
					p.Px(cx+x*math.Cos(tilt), cy+y, c)
				}
			}
			// bubbles drift up inside it
			pixelfx.Cycle(t, D, 8, seed, func(g *pixelfx.Rng) float64 { return g.Range(1, 1.8) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				x := r.Range(-0.5, 0.5)*R + math.Sin(age*4+r.Range(0, tau))*1.5
				y := R*0.6 - k*R*1.2
				if pixelfx.Hypot(x, y) > R*0.8 {
					return
				}
				c := pixelfx.White
				if k < 0.8 {
					c = C[6]
				}
				p.Px(cx+x, cy+y, c)
				if r.Next() < 0.35 {
					p.Px(cx+x+1, cy+y, C[5])
				}
			})
			// the shine: a bright crescent up on the left, a small refracted glint low on the right
			p.ArcFn(cx, cy, R*0.74, 1.8, pi*1.12, pi*1.4, func(u, v float64) (uint32, float64) {
				if u > 0.25 && u < 0.75 && v > 0.4 {
					return pixelfx.White, 1
				}
				return C[6], 1
			})
			p.Px(cx-R*0.45, cy-R*0.62, pixelfx.White)
			p.Px(cx+R*0.48, cy+R*0.5, C[6])
			p.Px(cx+R*0.48+1, cy+R*0.5, C[5])
			// motes in front
			for _, m := range waterOrbMotes(ph, R) {
				if m.z >= 0 {
					p.Px(cx+m.x, cy+m.y, pixelfx.White)
					off := 1.0
					if m.i%2 != 0 {
						off = -1
					}
					p.Px(cx+m.x-off, cy+m.y, C[5])
					p.Glow(cx+m.x, cy+m.y, 4, C[5], 0.35)
				}
			}
			// drips: one swells on its belly and lets go every DRIP seconds, landing with a plink and a ring
			dt := math.Mod(t, waterOrbDrip) / waterOrbDrip
			fall := math.Mod(t, waterOrbDrip) - waterOrbDrip*waterOrbLet
			y0 := cy + R + 3
			land := math.Sqrt((2 * -y0) / waterOrbG)
			if fall < 0 && dt > 0.2 {
				s := pixelfx.Smooth(0.2, waterOrbLet, dt)
				p.Circle(cx, cy+R+1+s*1.5, 0.8+s*1.2, C[5], 1)
				p.Px(cx-0.5, cy+R+s*1.5, C[6])
			} else if fall >= 0 && fall < land {
				bead(p, cx, y0+0.5*waterOrbG*fall*fall, 0, waterOrbG*fall, 2, C, 1)
			} else if fall >= land {
				since := fall - land
				if since < 0.08 {
					p.Px(cx, 0, pixelfx.White)
				}
				ring(p, cx, 0, 1.5+since*18, waterOrbSY, 1.1*(1-since/0.4), C, ringOpts{n: 1, half: 1, seed: 3, hot: since < 0.05})
			}
			// where the lens brings the light to a point, low on the far side: a bright fleck, pulsing
			fx, fy, pulse := cx+R*0.36, cy+R*0.4, 0.8+0.2*math.Sin(T*4)
			p.Circle(fx, fy, 1.6, C[6], 1)
			p.Px(fx, fy, pixelfx.White)
			p.Glow(fx, fy, 9, C[5], 0.45*pulse)
			p.Glow(cx, cy, R*1.4, C[5], 0.22)
			p.Glow(cx-R*0.4, cy-R*0.5, 6, pixelfx.White, 0.25)
		},
	}
}
