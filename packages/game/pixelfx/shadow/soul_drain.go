// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/soul-drain.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// SoulDrain tears pale souls loose on the right and drags them down a funnel of streaks into a black crystal vessel, which flares violet as each one lands. It is registered as "shadow/soul-drain".
var SoulDrain = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Soul drain",
		Desc:     "Pale souls are torn loose on the right and dragged down a funnel of streaks into a black crystal vessel, which flares violet as each one lands.",
		Tags:     []string{"shadow", "soul", "drain", "necromancy", "funnel", "directional", "magic", "loop"},
		Size:     [2]int{208, 120},
		Origin:   [2]int{40, 110},
		Duration: 3.2,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "soul", Default: pixelfx.Palette("#2c3f66", "#7d9fd0", "#cfe6ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newSoulDrain,
}

func init() { pixelfx.Register("shadow/soul-drain", SoulDrain) }

// the vessel's centre; the loop; the funnel's streamlines (start points out on the right); four souls a loop
const (
	sdVY     = -50.0
	sdD      = 3.2
	sdLines  = 11
	sdSouls  = 4
	sdTravel = 1.45
)

type sdLine struct {
	sx, sy, c1x, c1y, c2x, c2y float64
}

var sdLN = func() [sdLines]sdLine {
	var out [sdLines]sdLine
	for i := range out {
		fi := float64(i)
		sx := 96 + pixelfx.Rand(fi, 1)*64
		sy := -88 + pixelfx.Rand(fi, 2)*72
		bend := (pixelfx.Rand(fi, 3) - 0.5) * 50
		out[i] = sdLine{sx: sx, sy: sy, c1x: sx * 0.55, c1y: sy + bend, c2x: 22 + pixelfx.Rand(fi, 4)*14, c2y: sdVY + (sy-sdVY)*0.25}
	}
	return out
}()

func sdWrap(x float64) float64 { return pixelfx.Wrap(x, sdD) }

// sdAlong returns a point along streamline L at u (0 at its start, 1 at the vessel), cubic bezier.
func sdAlong(L sdLine, u float64) (float64, float64) {
	v := 1 - u
	a, b, c, d := v*v*v, 3*v*v*u, 3*v*u*u, u*u*u
	return a*L.sx + b*L.c1x + c*L.c2x, a*L.sy + b*L.c1y + c*L.c2y + d*(sdVY-L.sy) + d*L.sy
}

type sdSoul struct {
	born float64
	line int
}

func sdSoulOf(j int) sdSoul {
	fj := float64(j)
	return sdSoul{
		born: (fj + 0.15 + pixelfx.Rand(fj, 7)*0.5) * (sdD / sdSouls),
		line: int(math.Floor(pixelfx.Rand(fj, 8) * sdLines)),
	}
}

func sdLandings(t float64) float64 {
	f := 0.0
	for j := range sdSouls {
		since := sdWrap(t - sdSoulOf(j).born - sdTravel)
		if since < 1 {
			f += math.Exp(-since / 0.14)
		}
	}
	return math.Min(1.4, f)
}

// sdVessel draws the vessel: a black crystal, two facets lit violet from inside, a hard ridge, a heart of stolen light.
func sdVessel(p *pixelfx.Painter, C []uint32, t, f float64) float64 {
	y0 := sdVY + math.Sin((tau*t)/sdD)*1.5
	const W, UP, DN = 9.0, 14.0, 12.0
	p.Shade(-W-1, y0-UP-1, W+1, y0+DN+1, func(x, y float64, _ *pixelfx.Out) uint32 {
		dy := y - y0
		var half float64
		if dy < 0 {
			half = W * (1 + dy/UP)
		} else {
			half = W * (1 - dy/DN)
		}
		if half <= 0 || math.Abs(x) > half+0.6 {
			return 0
		}
		if math.Abs(x) > half-0.4 {
			return pick(f > 0.5, C[6], C[1])
		}
		ridge := math.Abs(x+dy*0.12) < 0.6
		heart := math.Abs(x)+math.Abs(dy-1) < 2.6+f*1.5
		if heart {
			return pick(f > 0.4, pixelfx.White, C[6])
		}
		if ridge {
			return pick(f > 0.3, C[7], C[5])
		}
		if x < 0 {
			if dy < 0 {
				return pick(f > 0.2, C[5], C[4])
			}
			return C[3]
		}
		return pick(dy < 0, C[2], C[1])
	})
	p.Glow(0, y0, 30+14*f, C[5], 0.5+0.5*f)
	p.Glow(0, y0+1, 8, pixelfx.White, 0.25+0.6*f)
	return y0
}

func newSoulDrain(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	S := pixelfx.Fit(o.Ramp("soul"), 4)
	seed := o.Seed()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			// the funnel: streamlines bunching into the vessel, dashes running down them, brighter as they close in
			for i := range sdLines {
				L, ph := sdLN[i], sdWrap(t+float64(i)*0.37)/sdD
				hasPrev := false
				px, py := 0.0, 0.0
				for u := 0.0; u <= 1.0001; u += 0.01 {
					x, y := sdAlong(L, u)
					dash := pixelfx.Wrap(u*5-ph*10+float64(i)*0.3, 1)
					// a faint line the whole way, bright dashes riding it inward, all brighter as it narrows
					if hasPrev {
						p.Line(px, py, x, y, pick(u > 0.6, C[3], C[2]), 0.4+0.6*u)
					}
					if dash < 0.18 {
						c := C[5]
						if u > 0.7 {
							c = C[7]
						} else if u > 0.35 {
							c = C[6]
						}
						p.Px(x, y, c)
					}
					px, py, hasPrev = x, y, true
				}
			}
			p.Glow(10, sdVY, 40, C[4], 0.22)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			f := sdLandings(t)
			// where the souls are torn from: a faint pale haze out on the right
			pixelfx.Cycle(t, sdD, 10, seed, func(r *pixelfx.Rng) float64 { return r.Range(0.8, 1.3) }, func(age, Lf float64, r *pixelfx.Rng, _ int) {
				x := r.Range(100, 150)
				y := r.Range(-80, -20) - age*18
				u := age / Lf
				p.Dot(x, y, S[2], (1-u)*0.8)
				p.Glow(x, y, 4, S[1], 0.2*(1-u))
			})
			// streaks racing down the funnel, faster as they close on the vessel, white at the head
			pixelfx.Cycle(t, sdD, 22, seed+1, func(r *pixelfx.Rng) float64 { return r.Range(0.4, 0.7) }, func(age, Lf float64, r *pixelfx.Rng, _ int) {
				L := sdLN[int(math.Floor(r.Next()*sdLines))]
				u0 := r.Range(0.15, 0.5)
				q := u0 + (1-u0)*math.Pow(age/Lf, 2.2)
				q2 := u0 + (1-u0)*math.Pow(math.Max(0, age-0.06)/Lf, 2.2)
				x, y := sdAlong(L, q)
				tx, ty := sdAlong(L, q2)
				p.LineFn(tx, ty, x, y, func(v float64) (uint32, float64) {
					switch {
					case v > 0.7:
						return pixelfx.White, 1
					case v > 0.35:
						return C[6], 1
					}
					return C[4], 1
				})
				p.Glow(x, y, 4, C[6], 0.35)
			})
			y0 := sdVessel(p, C, t, f)
			// motes circling the vessel, in front and behind it
			for k := range 6 {
				a := (tau*(2*t))/sdD + (float64(k)*tau)/6
				ex, ey, z := math.Cos(a)*15, math.Sin(a)*5-2, math.Sin(a)
				p.Px(ex, y0+ey, pick(z > 0, C[7], C[4]))
				if z > 0 {
					p.Glow(ex, y0+ey, 4, C[6], 0.35)
				}
			}
			// the souls: a pale head with hollow eyes and an open mouth, a wavering tail, dragged faster as they go
			for j := range sdSouls {
				s := sdSoulOf(j)
				age := sdWrap(t - s.born)
				if age > sdTravel {
					continue
				}
				L := sdLN[s.line]
				u := math.Pow(age/sdTravel, 1.8)
				x, y := sdAlong(L, u)
				if age < 0.18 {
					k := 1 - age/0.18
					ringFast(p, L.sx, L.sy, 3+9*(1-k), 1, pick(k > 0.5, S[2], S[1]), 1)
					p.Glow(L.sx, L.sy, 12, S[1], 0.5*k)
				}
				// the tail: a wavering wisp, pale near the head, thinning to violet
				for k := 14; k >= 1; k-- {
					uu := math.Pow(math.Max(0, age-float64(k)*0.03)/sdTravel, 1.8)
					tx, ty := sdAlong(L, uu)
					w := math.Sin(t*12+float64(k)*0.8+float64(j)) * float64(k) * 0.16
					r := math.Max(0.5, 2.6-float64(k)*0.17)
					c := C[5]
					if k < 4 {
						c = S[2]
					} else if k < 9 {
						c = S[1]
					}
					p.Disc(tx, ty+w, r, c, 0.75*(1-float64(k)/15))
				}
				// the head: a pale round face, hollow eyes and an open mouth, a lit rim, stretched as it nears the vessel
				near := pixelfx.Smooth(0.82, 1, u)
				hr := 3.4 - near*1.6
				p.Circle(x, y, hr+0.8, S[1], 1)
				p.Circle(x, y, hr, S[2], 1)
				p.Px(x-1, y-2, S[3])
				p.Px(x-2, y-1, S[3])
				if near < 0.5 {
					p.Px(x-1.5, y, S[0])
					p.Px(x+1.5, y, S[0])
					p.Px(x, y+1.6, S[0])
					p.Px(x, y+2.4, S[0])
				}
				p.Glow(x, y, 10, S[1], 0.5)
			}
			// each landing: a flash in the heart, a ring off the crystal, a glint
			for j := range sdSouls {
				since := sdWrap(t - sdSoulOf(j).born - sdTravel)
				if since < 0.3 {
					k := since / 0.3
					c := C[3]
					if k < 0.4 {
						c = C[7]
					} else if k < 0.7 {
						c = C[5]
					}
					ringFast(p, 0, y0, 6+18*pixelfx.Ease.OutCubic(k), math.Max(1, 2*(1-k)), c, 1)
				}
			}
			halo(p, 0, y0, 4, 22, C[6], 0.25*f)
		},
	}
}
