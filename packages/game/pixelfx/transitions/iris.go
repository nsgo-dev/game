// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/iris.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source holy-light and sparkle (https://www.spritefusion.com/pixel-art-vfx/sparkle), MIT License, Copyright (c) 2026 Hugo Duprez, for the motes drifting in the light and the glints.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Iris is a circular iris screen transition: a lit iris closes onto a warm spotlight where dust drifts, snaps shut into a glint, then pops open from a flash with sparks. It is registered as "transitions/iris".
var Iris = &pixelfx.Effect{
	Meta: screenMeta(
		"Iris",
		"A lit iris closes onto a warm spotlight where dust drifts, holds as its rim twinkles, snaps shut into a glint, then pops open from a flash with a white-hot rim throwing sparks.",
		[]string{"iris", "circle", "spotlight", "wipe"},
		2.2,
		pixelfx.Option{Name: "palette", Default: pixelfx.Palette("#1c140c", "#3b2a18", "#6e5233", "#a8875a", "#d9bd8a", "#f3e2bd", "#fff7e8", "#ffffff")},
		pixelfx.Option{Name: "focus", Default: pixelfx.Nums(160, 100)},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newIris,
}

func init() { pixelfx.Register("transitions/iris", Iris) }

var (
	irisD     = 2.2
	irisClose = 0.62
	irisSnap  = 0.9
	irisShut  = 0.99
	irisPop   = 1.2
	irisOpen  = 1.75
	irisRS    = 24.0
	irisRW    = 5.0
	irisGlo   = 16.0
)

func newIris(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	R := pixelfx.NewRng(o.Seed())
	fx, fy := 160.0, 100.0
	if focus := o.Numbers("focus"); len(focus) >= 2 {
		fx, fy = focus[0], focus[1]
	}
	R0 := 0.0
	for _, corner := range [4][2]float64{{0, 0}, {W, 0}, {0, H}, {W, H}} {
		if d := pixelfx.Hypot(corner[0]-fx, corner[1]-fy); d > R0 {
			R0 = d
		}
	}
	R0 += irisRW + 2
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}

	radius := func(t float64) float64 {
		switch {
		case t < irisClose:
			return irisRS + (R0-irisRS)*(1-pixelfx.Ease.InOutCubic(t/irisClose))
		case t < irisSnap:
			return irisRS + math.Sin((t-irisClose)*22)*0.8*(1-(t-irisClose)/(irisSnap-irisClose))
		case t < irisShut:
			return irisRS * (1 - pixelfx.Ease.InCubic((t-irisSnap)/(irisShut-irisSnap)))
		case t < irisPop:
			return 0
		default:
			return R0 * pixelfx.Ease.OutQuart(pixelfx.Clamp((t-irisPop)/(irisOpen-irisPop)))
		}
	}

	t := 0.0
	shut, popped := false, false
	prevR := R0
	end := irisD - 0.05

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			r := radius(t)
			if t > irisClose-0.25 && t < irisSnap && R.Chance(0.25) {
				a := R.Range(0, tau)
				d := math.Sqrt(R.Next()) * r * 0.9
				vx := R.Range(-6, 6)
				vy := -R.Range(4, 14)
				life := R.Range(0.4, 0.8)
				ps.Spawn(pixelfx.Ember, fx+math.Cos(a)*d, fy+math.Sin(a)*d, vx, vy, life, 0, 1, 0, 0)
			}
			if t > irisClose-0.1 && t < irisSnap && R.Chance(0.3) {
				a := R.Range(0, tau)
				life := R.Range(0.2, 0.4)
				col := C[6]
				if R.Chance(0.5) {
					col = pixelfx.White
				}
				sz := R.Range(1, 2.4)
				ps.Spawn(pixelfx.StarKind, fx+math.Cos(a)*(r+1), fy+math.Sin(a)*(r+1), 0, 0, life, col, sz, 0, 0)
			}
			if !shut && t >= irisShut {
				shut = true
				lights.Add(fx, fy, 50, C[5], 0.7, 0.3)
			}
			if !popped && t >= irisPop {
				popped = true
				lights.Add(fx, fy, 140, C[5], 0.7, 0.45)
				lights.Add(fx, fy, 50, pixelfx.White, 0.9, 0.12)
				for range 40 {
					a := R.Range(0, tau)
					v := R.Range(150, 420)
					life := R.Range(0.2, 0.5)
					ps.Spawn(pixelfx.Spark, fx+math.Cos(a)*3, fy+math.Sin(a)*3, math.Cos(a)*v, math.Sin(a)*v, life, pixelfx.White, 1, 0, 0)
				}
			}
			if t > irisPop && t < irisOpen-0.1 {
				vr := (r - prevR) / dt
				for range 2 {
					if !R.Chance(0.6) {
						continue
					}
					a := R.Range(0, tau)
					x, y := fx+math.Cos(a)*r, fy+math.Sin(a)*r
					if x < 0 || y < 0 || x >= W || y >= H {
						continue
					}
					life := math.Min(R.Range(0.2, 0.45), end-t)
					col := C[5]
					if R.Chance(0.5) {
						col = C[6]
					}
					ps.Spawn(pixelfx.Mote, x, y, math.Cos(a)*vr*0.35, math.Sin(a)*vr*0.35, life, col, 0.7, 0, 0)
				}
			}
			prevR = r
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p0 *pixelfx.Painter, _ float64) {
			p := clipTo(p0)
			r := radius(t)
			hot := 0.7
			if t >= irisPop {
				hot = 1 - pixelfx.Smooth(irisPop, irisOpen, t)
			} else if t < irisSnap {
				hot = 0.45 + 0.25*pixelfx.Smooth(irisClose-0.2, irisClose, t)
			}
			rw := irisRW
			if t >= irisPop {
				rw += 3 * hot
			}
			if t < irisOpen+0.05 && !(t >= irisShut && t < irisPop && r <= 0) {
				rrw := r + rw
				rrw2 := rrw * rrw
				rg := r - irisGlo
				rg2 := rg * rg
				for y := range H {
					dy := float64(y) + 0.5 - fy
					xo := math.Sqrt(math.Max(0, rrw2-dy*dy))
					if xo <= 0 {
						rect(p0, 0, float64(y), W, 1, ink)
						continue
					}
					a0 := int(pixelfx.ClampTo(math.Floor(fx-xo), 0, W))
					b1 := int(pixelfx.ClampTo(math.Ceil(fx+xo), 0, W))
					if a0 > 0 {
						rect(p0, 0, float64(y), float64(a0), 1, ink)
					}
					if b1 < W {
						rect(p0, float64(b1), float64(y), float64(W-b1), 1, ink)
					}
					g := 0.0
					if r > irisGlo {
						g = math.Sqrt(math.Max(0, rg2-dy*dy))
					}
					for x := a0; x < b1; x++ {
						dx := float64(x) + 0.5 - fx
						if math.Abs(dx) < g {
							continue
						}
						d := pixelfx.Hypot(dx, dy) - r
						if d >= 0 {
							q := d / rw
							lit := 0.5 - 0.5*(dx*0.7+dy*0.7)/(r+d+0.01)
							h := hot * (0.55 + 0.6*lit)
							var col uint32
							switch {
							case q >= 1:
								col = ink
							case q < 0.2:
								if h > 0.5 {
									col = pixelfx.White
								} else if h > 0.3 {
									col = C[6]
								} else {
									col = C[5]
								}
							case q < 0.4:
								if h > 0.38 {
									col = C[6]
								} else {
									col = C[5]
								}
							case q < 0.65:
								if h > 0.3 {
									col = C[5]
								} else {
									col = C[4]
								}
							case q < 0.85:
								col = C[3]
							default:
								col = C[2]
							}
							p0.Px(float64(x), float64(y), col)
							if q < 0.5 {
								p0.Light(float64(x), float64(y), C[5], 0.45*h*(1-q*2))
							}
						} else if d > -irisGlo {
							gm := 1 + d/irisGlo
							p0.Light(float64(x), float64(y), C[5], (0.18+0.3*hot)*gm*gm)
						}
					}
				}
			} else if t >= irisShut && t < irisPop {
				rect(p0, 0, 0, W, H, ink)
			}
			if r > 0 && r < 80 && t < irisShut {
				k := 1 - r/80
				p.Glow(fx, fy, r*1.15+6, C[5], 0.18+0.32*k)
				p.Glow(fx, fy, r*0.6+3, C[6], 0.25*k)
			}
			if t > 0.08 && t < irisSnap {
				a := -2.4 + ((t-0.08)/(irisSnap-0.08))*tau
				gx, gy := fx+math.Cos(a)*(r+1), fy+math.Sin(a)*(r+1)
				if gx > 2 && gy > 2 && gx < W-2 && gy < H-2 {
					glint(p, gx, gy, 7+3*math.Sin(t*9), t*3, C[6], C[5], 0.9)
				}
			}
			if s := t - irisShut; s > -0.06 && s < 0.22 {
				glint(p, fx, fy, 14*(1-math.Abs(s+0.01)/0.23), s*4, C[6], C[5], 1)
			}
			if u := t - irisPop; u >= 0 && u < 0.18 {
				glint(p, fx, fy, 20*(1-u/0.18), 0.4-u*3, C[6], C[5], 1)
				p.Glow(fx, fy, 60, pixelfx.White, 0.6*(1-u/0.18))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
