// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/star-iris.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez, for the twinkles (spinning glints at the tips and motes shed as it turns).

package transitions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// StarIris is a spinning five-point star iris screen transition: a gold-rimmed star spins shut onto the middle of the screen shedding motes, closes on a glint, and spins open again from a flash. It is registered as "transitions/star-iris".
var StarIris = &pixelfx.Effect{
	Meta: screenMeta(
		"Star iris",
		"A five-point star iris spins shut on the middle of the screen, its gold rim lit and its tips twinkling and shedding motes; it closes on a glint and spins open again from a flash.",
		[]string{"iris", "star", "gold", "wipe"},
		2.2,
		pixelfx.Option{Name: "palette", Default: pixelfx.RampOf(pixelfx.Ramps["gold"])},
		pixelfx.Option{Name: "focus", Default: pixelfx.Nums(160, 96)},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newStarIris,
}

func init() { pixelfx.Register("transitions/star-iris", StarIris) }

var (
	starD     = 2.2
	starClose = 0.98
	starOpen0 = 1.16
	starOpen1 = 2.0
	starRMax  = 430.0
	starInner = 0.46
	starRW    = 4.0
)

type starState struct{ r, a float64 }

func starAt(t float64) starState {
	if t < starClose {
		return starState{
			r: starRMax * math.Pow(1-pixelfx.Ease.InOutQuad(t/starClose), 1.3),
			a: -pi/2 + pixelfx.Ease.InOutCubic(t/starClose)*1.6*tau,
		}
	}
	if t < starOpen0 {
		return starState{}
	}
	u := pixelfx.Clamp((t - starOpen0) / (starOpen1 - starOpen0))
	return starState{
		r: starRMax * math.Pow(pixelfx.Ease.InOutCubic(u), 1.15),
		a: -pi/2 - pixelfx.Ease.InOutCubic(u)*1.6*tau,
	}
}

func starPoints(fx, fy, r, a float64) [10][2]float64 {
	var pts [10][2]float64
	for i := range 10 {
		rr := r
		if i%2 != 0 {
			rr = r * starInner
		}
		an := a + (float64(i)*pi)/5
		pts[i] = [2]float64{fx + math.Cos(an)*rr, fy + math.Sin(an)*rr}
	}
	return pts
}

func newStarIris(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	R := pixelfx.NewRng(o.Seed())
	fx, fy := 160.0, 96.0
	if focus := o.Numbers("focus"); len(focus) >= 2 {
		fx, fy = focus[0], focus[1]
	}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}
	t := 0.0
	shut, opened := false, false
	prev := starAt(0)
	end := starD - 0.05
	xs := make([]float64, 0, 10)

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			s := starAt(t)
			pts := starPoints(fx, fy, s.r, s.a)
			spin := (s.a - prev.a) / dt
			if s.r > 2 {
				for i := 0; i < 10; i += 2 {
					x, y := pts[i][0], pts[i][1]
					if x < 0 || y < 0 || x >= W || y >= H || !R.Chance(0.35) {
						continue
					}
					an := s.a + (float64(i)*pi)/5
					v := spin * s.r * 0.25
					kind := pixelfx.Mote
					if R.Chance(0.6) {
						kind = pixelfx.StarKind
					}
					life := math.Min(R.Range(0.25, 0.55), end-t)
					col := C[5]
					if R.Chance(0.5) {
						col = C[6]
					}
					sz := R.Range(0.6, 1.8)
					ps.Spawn(kind, x, y, -math.Sin(an)*v+math.Cos(an)*20, math.Cos(an)*v+math.Sin(an)*20, life, col, sz, 0, 0)
				}
			}
			if !shut && t >= starClose {
				shut = true
				lights.Add(fx, fy, 60, C[5], 0.7, 0.3)
			}
			if !opened && t >= starOpen0 {
				opened = true
				lights.Add(fx, fy, 120, C[5], 0.7, 0.4)
				lights.Add(fx, fy, 40, pixelfx.White, 0.8, 0.12)
				for range 24 {
					a := R.Range(0, tau)
					v := R.Range(60, 200)
					life := R.Range(0.3, 0.6)
					col := C[6]
					if R.Chance(0.5) {
						col = pixelfx.White
					}
					sz := R.Range(0.6, 2)
					ps.Spawn(pixelfx.StarKind, fx, fy, math.Cos(a)*v, math.Sin(a)*v, life, col, sz, 0, 0)
				}
			}
			prev = s
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p0 *pixelfx.Painter, _ float64) {
			p := clipTo(p0)
			s := starAt(t)
			if t >= starClose && t < starOpen0 {
				rect(p0, 0, 0, W, H, ink)
			} else if s.r < starRMax-2 {
				pts := starPoints(fx, fy, s.r, s.a)
				for y := range H {
					yc := float64(y) + 0.5
					xs = xs[:0]
					for i, j := 0, 9; i < 10; j, i = i, i+1 {
						xi, yi := pts[i][0], pts[i][1]
						xj, yj := pts[j][0], pts[j][1]
						if (yi > yc) != (yj > yc) {
							xs = append(xs, xi+((yc-yi)*(xj-xi))/(yj-yi))
						}
					}
					slices.Sort(xs)
					x := 0.0
					for k := 0; k+1 < len(xs); k += 2 {
						a := pixelfx.ClampTo(pixelfx.Round(xs[k]), 0, W)
						if a > x {
							rect(p0, x, float64(y), a-x, 1, ink)
						}
						x = math.Max(x, pixelfx.ClampTo(pixelfx.Round(xs[k+1]), 0, W))
					}
					if x < W {
						rect(p0, x, float64(y), W-x, 1, ink)
					}
				}
				for i, j := 0, 9; i < 10; j, i = i, i+1 {
					ax, ay := pts[j][0], pts[j][1]
					bx, by := pts[i][0], pts[i][1]
					L := pixelfx.Hypot(bx-ax, by-ay)
					if L < 0.5 {
						continue
					}
					ux, uy := (bx-ax)/L, (by-ay)/L
					nx, ny := -uy, ux
					lit := nx*-0.6+ny*-0.8 > 0.1
					for sx := 0.0; sx <= L; sx += 0.5 {
						x0, y0 := ax+ux*sx, ay+uy*sx
						if x0 < -starRW || y0 < -starRW || x0 > W+starRW || y0 > H+starRW {
							continue
						}
						for d := 0.0; d < starRW; d += 0.5 {
							var c uint32
							switch {
							case d < 1:
								if lit {
									c = pixelfx.White
								} else {
									c = C[6]
								}
							case d < 2:
								c = C[5]
							case d < 3:
								c = C[4]
							default:
								c = C[3]
							}
							p.Px(x0+nx*d, y0+ny*d, c)
						}
						if math.Mod(sx, 2) == 0 {
							p.Light(x0, y0, C[5], 0.5)
						}
					}
				}
				for i := 0; i < 10; i += 2 {
					x, y := pts[i][0], pts[i][1]
					if x < 2 || y < 2 || x > W-2 || y > H-2 {
						continue
					}
					glint(p, x, y, 5+3*math.Sin(t*13+float64(i)), t*2+float64(i), C[6], C[5], 1)
				}
				if s.r < 140 {
					p.Glow(fx, fy, s.r*0.7+8, C[5], 0.35*(1-s.r/140))
				}
			}
			if u := t - starClose; u > -0.05 && u < 0.2 {
				glint(p, fx, fy, 14*(1-math.Abs(u)/0.2), u*4, C[6], C[5], 1)
			}
			if v := t - starOpen0; v >= 0 && v < 0.16 {
				glint(p, fx, fy, 18*(1-v/0.16), 0.3-v*5, C[6], C[5], 1)
				p.Glow(fx, fy, 50, pixelfx.White, 0.5*(1-v/0.16))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
