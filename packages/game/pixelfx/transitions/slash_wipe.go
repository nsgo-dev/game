// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/slash-wipe.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the cut's tapered smear, dotted trace, tip glint, sparks off the end and shavings flung off the line from Sprite Fusion's open-source slash (https://www.spritefusion.com/pixel-art-vfx/slash), MIT License, Copyright (c) 2026 Hugo Duprez.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// SlashWipe is a blade-cut screen transition: a white-hot edge slices across the screen spraying sparks, then a second diagonal cut flashes through and the cover splits apart. It is registered as "transitions/slash-wipe".
var SlashWipe = &pixelfx.Effect{
	Meta: screenMeta(
		"Slash wipe",
		"A white-hot blade edge slices across the screen, light ahead of it and sparks spraying back over the cover; a second cut flashes through and the cover splits apart.",
		[]string{"slash", "blade", "cut", "sparks", "wipe"},
		1.7,
		pixelfx.Option{Name: "palette", Default: pixelfx.Palette("#151a3d", "#212f6b", "#2c58aa", "#3a8bd8", "#62c8f4", "#b8c9e8", "#e8f1ff", "#ffffff")},
		pixelfx.Option{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(2)},
	),
	New: newSlashWipe,
}

func init() { pixelfx.Register("transitions/slash-wipe", SlashWipe) }

var (
	slashD      = 1.7
	slashSL     = 0.55
	slashCS     = 1 / pixelfx.Hypot(1, slashSL)
	slashHalo   = 18.0
	slashT0     = 0.06
	slashSweep  = 0.62
	slashE0     = -(float64(H)/2)*slashSL - 4
	slashE1     = float64(W) + (float64(H)/2)*slashSL + 24
	slashCut    = 0.84
	slashCSW    = 0.07
	slashCHold  = 0.05
	slashCTrail = 0.12
	slashSlip   = slashCut + 0.12
	slashSplit  = slashCut + slashCSW + slashCHold + slashCTrail
)

func slashEdgeAt(t float64) float64 {
	return slashE0 + (slashE1-slashE0)*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-slashT0)/slashSweep))
}

func slashSpeedAt(t float64) float64 {
	if t < slashT0 || t > slashT0+slashSweep {
		return 0
	}
	r := 1 - (t-slashT0)/slashSweep
	return ((slashE1 - slashE0) * 3 * r * r) / slashSweep
}

func slashXAt(e, y float64) float64 {
	return e + (float64(H)/2-y)*slashSL
}

func newSlashWipe(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	R := pixelfx.NewRng(o.Seed())
	n := len(C)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: o.Ramp("sparks")})
	lights := &pixelfx.Lights{}

	th := R.Range(0.42, 0.56)
	ux, uy := math.Cos(th), math.Sin(th)
	nx, ny := uy, -ux
	cx := W/2 + R.Range(-24, 24)
	cy := H/2 + R.Range(-10, 10)
	area := [][2]float64{{-24, -24}, {W + 24, -24}, {W + 24, H + 24}, {-24, H + 24}}
	var ends [2][2]float64
	for idx, s := range [2]float64{-1, 1} {
		lo, hi := 0.0, 400.0
		for range 30 {
			m := (lo + hi) / 2
			x, y := cx+s*ux*m, cy+s*uy*m
			if x >= 0 && x <= W && y >= 0 && y <= H {
				lo = m
			} else {
				hi = m
			}
		}
		ends[idx] = [2]float64{cx + s*ux*lo, cy + s*uy*lo}
	}
	A, B := ends[0], ends[1]
	LC := pixelfx.Hypot(B[0]-A[0], B[1]-A[1])

	type half struct {
		s      float64
		pts    [][2]float64
		mx, my float64
		spin   float64
	}
	var halves [2]half
	for idx, s := range [2]float64{1, -1} {
		pts := halfPlane(area, cx, cy, s*nx, s*ny)
		mx, my := 0.0, 0.0
		for _, q := range pts {
			mx += q[0] / float64(len(pts))
			my += q[1] / float64(len(pts))
		}
		halves[idx] = half{s: s, pts: pts, mx: mx, my: my, spin: s * R.Range(0.5, 0.9)}
	}

	t := 0.0
	acc := 0.0
	entered, tipped, split := false, false, false
	end := slashD - 0.05

	type place struct{ dx, dy, rot, sep float64 }
	placeOf := func(hf *half) place {
		sm := -0.4
		if hf.s > 0 {
			sm = 1
		}
		slip := 5 * pixelfx.Ease.OutCubic(pixelfx.Clamp((t-slashSlip)/0.12)) * sm
		u := math.Max(0, t-slashSplit)
		sep := 30*u + 0.5*1500*u*u
		rot := hf.spin * u * u
		return place{dx: ux*slip + hf.s*nx*sep, dy: uy*slip + hf.s*ny*sep, rot: rot, sep: sep}
	}
	moved := func(hf *half, pl place, q [2]float64) [2]float64 {
		c, s := math.Cos(pl.rot), math.Sin(pl.rot)
		lx, ly := q[0]-hf.mx, q[1]-hf.my
		return [2]float64{hf.mx + lx*c - ly*s + pl.dx, hf.my + lx*s + ly*c + pl.dy}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			sp, e := slashSpeedAt(t), slashEdgeAt(t)
			if !entered && t >= slashT0 {
				entered = true
				lights.Add(4, 4, 60, C[5], 0.6, 0.25)
			}
			for acc += sp * dt * 0.035; acc > 1; acc-- {
				y := R.Range(4, H-4)
				x := slashXAt(e, y) - 1
				a0 := pi + R.Range(-0.1, 0.6)
				cnt := int(R.Int(5, 10))
				if x < 0 || x >= W {
					continue
				}
				for range cnt {
					a := a0 + R.Range(-0.75, 0.75)
					v := R.Range(0.2, 1) * (140 + 0.22*sp)
					sy := y + R.Range(-2, 2)
					life := R.Range(0.15, 0.4)
					ps.Spawn(pixelfx.Spark, x, sy, math.Cos(a)*v, math.Sin(a)*v, life, pixelfx.White, 1, 0, 0)
					if R.Chance(0.4) {
						ml := R.Range(0.2, 0.5)
						col := C[5]
						if R.Chance(0.5) {
							col = C[6]
						}
						ps.Spawn(pixelfx.Mote, x, y, math.Cos(a)*v*0.45, math.Sin(a)*v*0.45-20, ml, col, 0.7, 0, 0)
					}
				}
				lights.Add(x, y, R.Range(30, 46), C[5], 0.35, 0.14)
			}
			if !tipped && t >= slashCut+slashCSW {
				tipped = true
				for range 12 {
					a := math.Atan2(uy, ux) + R.Range(-0.5, 0.5)
					v := R.Range(150, 400)
					life := R.Range(0.12, 0.35)
					ps.Spawn(pixelfx.Spark, B[0]-ux*2, B[1]-uy*2, math.Cos(a)*v, math.Sin(a)*v, life, pixelfx.White, 1, 0, 0)
				}
				for range 18 {
					k := R.Range(0.1, 1)
					x, y := A[0]+(B[0]-A[0])*k, A[1]+(B[1]-A[1])*k
					s := R.Sign()
					v := R.Range(30, 100)
					life := R.Range(0.25, 0.55)
					col := C[4]
					if R.Chance(0.5) {
						col = C[5]
					}
					ps.Spawn(pixelfx.Mote, x, y, s*nx*v+ux*20, s*ny*v-10, life, col, 0.6, 0, 0)
				}
				for i := range 5 {
					k := (float64(i) + 0.5) / 5
					lights.Add(A[0]+(B[0]-A[0])*k, A[1]+(B[1]-A[1])*k, 46, C[5], 0.45, 0.3)
				}
			}
			if !split && t >= slashSplit {
				split = true
				for range 46 {
					k := R.Range(0, 1)
					x, y := A[0]+(B[0]-A[0])*k, A[1]+(B[1]-A[1])*k
					s := R.Sign()
					v := R.Range(80, 320)
					a := math.Atan2(s*ny, s*nx) + R.Range(-0.7, 0.7)
					life := math.Min(R.Range(0.15, 0.45), end-t)
					ps.Spawn(pixelfx.Spark, x, y, math.Cos(a)*v, math.Sin(a)*v, life, pixelfx.White, 1, 0, 0)
				}
				for i := range 6 {
					k := (float64(i) + 0.5) / 6
					lights.Add(A[0]+(B[0]-A[0])*k, A[1]+(B[1]-A[1])*k, 56, C[5], 0.5, 0.35)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p0 *pixelfx.Painter, _ float64) {
			p := clipTo(p0)
			if t < slashCut+slashCSW+slashCHold+0.02 || t < slashSlip {
				e, sp := slashEdgeAt(t), slashSpeedAt(t)
				Ls := pixelfx.ClampTo(sp*0.0065, 1.5, 8)
				echo := pixelfx.Clamp(sp/900) * 0.6
				for y := range H {
					xe := slashXAt(e, float64(y)+0.5)
					ia := int(pixelfx.ClampTo(math.Floor(xe-(Ls+10)/slashCS), 0, W))
					ib := int(pixelfx.ClampTo(math.Ceil(xe+slashHalo/slashCS), 0, W))
					if ia > 0 {
						rect(p0, 0, float64(y), float64(ia), 1, ink)
					}
					for x := ia; x < ib; x++ {
						d := (xe - (float64(x) + 0.5)) * slashCS
						if d < 0 {
							u := 1 + d/slashHalo
							p0.Light(float64(x), float64(y), C[5], 0.32*u*u)
							if d > -4 {
								w := 1 + d/4
								p0.Light(float64(x), float64(y), pixelfx.White, 0.3*w*w)
							}
							continue
						}
						if d < 1 {
							p0.Px(float64(x), float64(y), pixelfx.White)
						} else if d < 2 {
							p0.Px(float64(x), float64(y), C[6])
						} else {
							q := (d - 2) / Ls
							var col uint32
							switch {
							case q < 0.3:
								col = C[5]
							case q < 0.55:
								col = C[4]
							case q < 0.8:
								col = C[3]
							case q < 1:
								col = C[2]
							default:
								col = ink
							}
							p0.Px(float64(x), float64(y), col)
							if echo > 0 && math.Abs(d-Ls-7) < 0.5 && (x+y)%3 == 0 {
								p0.Dot(float64(x), float64(y), C[4], echo)
							}
						}
						if d < Ls+2 {
							p0.Light(float64(x), float64(y), C[5], 0.3*(1-d/(Ls+2)))
						}
					}
				}
			}
			if t >= slashSlip {
				heat := 1 - pixelfx.Clamp((t-slashCut-0.05)/0.5)
				for hi := range halves {
					hf := &halves[hi]
					pl := placeOf(hf)
					pts := make([][2]float64, len(hf.pts))
					for i, q := range hf.pts {
						pts[i] = moved(hf, pl, q)
					}
					a, b := moved(hf, pl, A), moved(hf, pl, B)
					c, sn := math.Cos(pl.rot), math.Sin(pl.rot)
					mx, my := hf.s*(nx*c-ny*sn), hf.s*(nx*sn+ny*c)
					var rim [4]uint32
					if heat > 0.55 {
						rim[0] = pixelfx.White
					} else if heat > 0.25 {
						rim[0] = C[6]
					} else {
						rim[0] = C[5]
					}
					if heat > 0.4 {
						rim[1] = C[6]
					} else {
						rim[1] = C[4]
					}
					if heat > 0.3 {
						rim[2] = C[5]
						rim[3] = C[4]
					} else {
						rim[2] = C[3]
						rim[3] = ink
					}
					deep := pixelfx.ClampTo(1+pl.sep*0.5, 1, 4)
					fillConvexFn(p0, pts, func(x, y float64) uint32 {
						d := (x-a[0])*mx + (y-a[1])*my
						if d < deep {
							if d < 2 {
								p0.Light(x, y, C[5], 0.4*heat*(1-d/2))
							}
							idx := min(3, max(0, int(math.Floor(d))))
							return rim[idx]
						}
						return ink
					}, 1)
					if heat > 0 {
						for i := range 8 {
							k := (float64(i) + 0.5) / 8
							p.Glow(a[0]+(b[0]-a[0])*k, a[1]+(b[1]-a[1])*k, 12+pl.sep*0.15, C[5], 0.3*heat)
						}
					}
				}
			}
			u := t - slashCut
			if u >= 0 && u < slashCSW+slashCHold+slashCTrail+0.25 {
				head := LC * pixelfx.Ease.OutExpo(pixelfx.Clamp(u/slashCSW))
				tail := LC * pixelfx.Smooth(slashCSW+slashCHold, slashCSW+slashCHold+slashCTrail, u)
				age := pixelfx.Clamp(u / (slashCSW + slashCHold + slashCTrail))
				Wc := 3.6 * (1 - (tail/LC)*0.55)
				span := head - tail
				sn := math.Abs(nx)
				if span > 1 && tail < LC-1 && t < slashSplit {
					for y := range H {
						x0 := A[0] + ((float64(y)+0.5-A[1])*ux)/uy
						hw := (Wc + 1) / sn
						xs := max(0, int(math.Floor(x0-hw)))
						xe := min(W, int(math.Ceil(x0+hw)))
						for x := xs; x < xe; x++ {
							px := float64(x) + 0.5 - A[0]
							py := float64(y) + 0.5 - A[1]
							s := px*ux + py*uy
							v := (s - tail) / span
							if v < 0 || v > 1 {
								continue
							}
							w := Wc * math.Pow(math.Sin(pi*math.Pow(v, 2.2)), 0.6)
							if w < 0.35 {
								continue
							}
							q := math.Abs(px*nx+py*ny) / w
							if q > 1 {
								continue
							}
							ci := 0.0
							if !(q < 0.34 && age < 0.5) {
								ci = pixelfx.Round(q*float64(n-1)*0.75 + age*2.4 - v*0.8 + 0.4)
							}
							cIdx := n - 1 - int(pixelfx.ClampTo(ci, 0, float64(n-1)))
							p0.Dot(float64(x), float64(y), C[cIdx], math.Min(1, v/0.4))
							if q < 0.5 {
								p0.Light(float64(x), float64(y), C[5], 0.4*(1-age))
							}
						}
					}
				}
				if tail > 2 && t < slashSplit {
					for s := 0.0; s < tail; s += 1.6 {
						p.Dot(A[0]+ux*s, A[1]+uy*s, C[5], 0.7*(0.4+(0.6*s)/LC))
					}
				}
				if u < 0.3 {
					hx, hy := A[0]+ux*head, A[1]+uy*head
					p.Glow(hx, hy, 46, C[4], 0.55*(1-u/0.3))
				}
				if g := u - slashCSW*0.6; g > 0 && g < 0.24 {
					pixelfx.Sparkle(p, B[0]-ux*3, B[1]-uy*3, 11*pixelfx.Ease.OutBack(math.Min(1, g/0.05))*(1-g/0.24), C[6], C[4], 0.7)
				}
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
