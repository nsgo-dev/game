// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/transitions/blinds.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez, for the dust drifting in the light and the light squeezing through.

package transitions

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Blinds is a venetian-blinds screen transition: slats tilt shut top to bottom with warm light and dust squeezing through the closing gaps, then tilt open and fade. It is registered as "transitions/blinds".
var Blinds = &pixelfx.Effect{
	Meta: screenMeta(
		"Blinds",
		"Slats catch the light edge-on, then tilt shut top to bottom, lit rims glinting as warm light squeezes through the closing gaps with dust in it; then they tilt open and fade.",
		[]string{"blinds", "slats", "light", "wipe"},
		2.0,
		pixelfx.Option{Name: "palette", Default: pixelfx.Palette("#1a120c", "#33241a", "#5a4030", "#8a6848", "#c09a6a", "#e8cc98", "#fff0cc", "#ffffff")},
		pixelfx.Option{Name: "seed", Default: pixelfx.Num(1)},
	),
	New: newBlinds,
}

func init() { pixelfx.Register("transitions/blinds", Blinds) }

const blindsN = 12

var (
	blindsD     = 2.0
	blindsSH    = float64(H) / blindsN
	blindsShut0 = 0.08
	blindsOpen0 = 1.02
	blindsStep  = 0.038
	blindsTurn  = 0.42
	blindsOStep = 0.03
	blindsOTurn = 0.4
	blindsFade0 = 1.72
	blindsFade1 = 1.94
)

// blindsAngle returns how far slat i has turned: 0 edge-on, π/2 flat and shut (a little past while it settles).
func blindsAngle(i int, t float64) float64 {
	if t < blindsOpen0 {
		return (pi / 2) * pixelfx.OutBack(pixelfx.Clamp((t-blindsShut0-float64(i)*blindsStep)/blindsTurn), 1.6)
	}
	return (pi / 2) * (1 - pixelfx.Ease.InOutCubic(pixelfx.Clamp((t-blindsOpen0-float64(i)*blindsOStep)/blindsOTurn)))
}

func newBlinds(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	ink := o.Color("color")
	R := pixelfx.NewRng(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	t := 0.0
	end := blindsD - 0.05
	cross := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// dust drifts in the light coming through
			if t < blindsFade0 && R.Chance(0.8) {
				i := int(R.Int(0, blindsN-1))
				th := blindsAngle(i, t)
				if th > 0.2 && th < 1.45 {
					x := R.Range(0, W)
					y := float64(i+1)*blindsSH + R.Range(-2, 2)
					vx := R.Range(-8, 8)
					vy := -R.Range(2, 8)
					life := math.Min(R.Range(0.5, 1), end-t)
					ps.Spawn(pixelfx.Ember, x, y, vx, vy, life, 0, 1, 0, 0)
				}
			}
			ps.Update(dt)
		},
		DrawFunc: func(p0 *pixelfx.Painter, _ float64) {
			p := clipTo(p0)
			var open [H]uint8
			for i := range open {
				open[i] = 1
			}
			for i := range blindsN {
				h := math.Max(1, (blindsSH+1)*math.Abs(math.Sin(blindsAngle(i, t))))
				yc := (float64(i) + 0.5) * blindsSH
				y0 := int(math.Max(0, math.Ceil(yc-h/2-0.5)))
				y1 := int(math.Min(H-1, math.Floor(yc+h/2-0.5)))
				for y := y0; y <= y1; y++ {
					open[y] = 0
				}
			}
			// the dust is behind the slats: it only shows (and only shines) in the gaps
			for qi := range ps.List {
				q := &ps.List[qi]
				y := int(math.Floor(q.Y))
				k := q.Life / q.Max
				if q.Delay > 0 || y < 0 || y >= H || open[y] == 0 || q.X < 0 || q.X >= W {
					continue
				}
				tw := math.Mod(q.Seed, 1) < 0.3 && math.Sin(t*14+q.Seed) > 0
				col := C[5]
				if tw || k > 0.5 {
					col = pixelfx.White
				}
				p0.Px(q.X, float64(y), col)
				if tw {
					for _, d := range cross {
						ny := y + d[1]
						if ny >= 0 && ny < H && open[ny] != 0 {
							p.Px(q.X+float64(d[0]), float64(ny), C[6])
						}
					}
				}
				lk := 0.4
				if tw {
					lk = 0.7
				}
				p0.Light(q.X, float64(y), C[6], lk*k)
			}
			fade := 1 - pixelfx.Clamp((t-blindsFade0)/(blindsFade1-blindsFade0))
			appear := pixelfx.Clamp(t / blindsShut0)
			for i := range blindsN {
				th := blindsAngle(i, t)
				s, c := math.Sin(th), math.Cos(th)
				yc := (float64(i) + 0.5) * blindsSH
				h := math.Max(1, (blindsSH+1)*math.Abs(s))
				y0 := int(math.Max(0, math.Ceil(yc-h/2-0.5)))
				y1 := int(math.Min(H-1, math.Floor(yc+h/2-0.5)))
				under := th > pi/2
				if h < 2.5 {
					k := math.Min(appear, fade)
					if k <= 0 {
						continue
					}
					yy := pixelfx.Round(yc - 0.5)
					col := C[2]
					if k > 0.66 {
						col = C[6]
					} else if k > 0.33 {
						col = C[4]
					}
					rect(p0, 0, yy, W, 1, col)
					if h >= 1.5 && yy+1 < H {
						c2 := C[1]
						if k > 0.5 {
							c2 = C[3]
						}
						rect(p0, 0, yy+1, W, 1, c2)
					}
					for x := 0; x < W; x += 2 {
						p0.Light(float64(x), yy, C[5], 0.18*k)
					}
					continue
				}
				lit := c
				if under {
					lit = 0
				}
				for y := y0; y <= y1; y++ {
					r := y - y0
					last := y == y1
					v := float64(r) / math.Max(1, float64(y1-y0))
					f := lit * (1 - v*0.8)
					var col uint32
					switch {
					case r == 0:
						switch {
						case lit > 0.55:
							col = pixelfx.White
						case lit > 0.3:
							col = C[6]
						case lit > 0.12:
							col = C[4]
						default:
							col = ink
						}
					case r == 1:
						switch {
						case lit > 0.45:
							col = C[5]
						case lit > 0.2:
							col = C[3]
						default:
							col = ink
						}
					case last:
						col = ink
					default:
						switch {
						case f > 0.55:
							col = C[3]
						case f > 0.35:
							col = C[2]
						case f > 0.15:
							col = C[1]
						default:
							col = ink
						}
					}
					rect(p0, 0, float64(y), W, 1, col)
					if r == 0 && lit > 0.12 {
						for x := 0; x < W; x += 2 {
							p0.Light(float64(x), float64(y), C[5], 0.35*lit)
						}
					}
				}
				gap := blindsSH - h
				if gap > 0.5 && th > 0.12 && !under {
					k := (0.2 + 0.45*math.Pow(1-gap/blindsSH, 1.5)) * math.Min(1, th/0.4)
					gy := y1 + 1
					endY := min(H, gy+int(math.Ceil(gap)))
					for y := gy; y < endY; y++ {
						v := (float64(y-gy) + 0.5) / gap
						kk := k * (1 - math.Abs(v-0.5)*1.2)
						if kk > 0.01 {
							for x := range W {
								p0.Light(float64(x), float64(y), C[6], kk)
								p0.Light(float64(x), float64(y), C[4], kk*0.5)
							}
						}
					}
				}
				var u float64
				if t < blindsOpen0 {
					u = pixelfx.Clamp((t - blindsShut0 - float64(i)*blindsStep) / blindsTurn)
				} else {
					u = pixelfx.Clamp((t - blindsOpen0 - float64(i)*blindsOStep) / blindsOTurn)
				}
				if lit > 0.3 && u > 0.05 && u < 0.95 {
					gx := W * (1 - u)
					if t < blindsOpen0 {
						gx = W * u
					}
					glint(p, gx, float64(y0), 6*math.Sin(pi*u), u*3, C[6], C[5], 1)
				}
			}
		},
	}
}
