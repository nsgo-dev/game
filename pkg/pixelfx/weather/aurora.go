// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/aurora.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the banded light from Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Aurora is rippling curtains of green-to-violet light over a starfield: folding hems burn bright, rays shimmer up into violet, and the whole sky slowly breathes. It is registered as "weather/aurora".
var Aurora = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Aurora",
		Desc:     "Rippling curtains of green-to-violet light over a starfield: folding hems burn bright, rays shimmer up into violet, and the whole sky slowly breathes.",
		Tags:     []string{"weather", "aurora", "northern lights", "sky", "night", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 180},
		Duration: auroraDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#081a1e", "#0d3a36", "#13664e", "#1fa06a", "#3fd88e", "#9af7bf", "#e6fff0")},
			{Name: "upper", Default: pixelfx.Palette("#120b2a", "#26164f", "#41208a", "#6a34c0", "#9c62e8", "#d2b4ff", "#f6eeff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newAurora,
}

func init() { pixelfx.Register("weather/aurora", Aurora) }

const (
	auroraDuration = 12
	auroraW        = 320
	auroraH        = 180
)

var auroraProf = func() [201]float32 {
	var out [201]float32
	for i := range out {
		v := float64(i) / 200
		if v < 0.035 {
			out[i] = 2.6
		} else if v < 0.22 {
			out[i] = float32(1.15 - v)
		} else {
			out[i] = float32(math.Pow(1-v, 1.5) * 0.9)
		}
	}
	return out
}()

type auroraCurtainSpec struct {
	y, slope, amp, lam, fold, flam, height, k, s0, s1, rip float64
	c                                                      [3]float64
	hue                                                    float64
}

var auroraCurtains = []auroraCurtainSpec{
	{y: -132, slope: -0.08, amp: 8, lam: 210, fold: 14, flam: 150, height: 40, k: 0.6, s0: -210, s1: 210, rip: 1.6, c: [3]float64{1, 1, 2}, hue: 0.35},
	{y: -88, slope: 0.22, amp: 13, lam: 250, fold: 19, flam: 165, height: 82, k: 1.5, s0: -195, s1: 180, rip: 2.4, c: [3]float64{1, 2, 3}, hue: 0},
	{y: -38, slope: -0.05, amp: 5, lam: 150, fold: 10, flam: 110, height: 20, k: 0.45, s0: -210, s1: 210, rip: 1.4, c: [3]float64{2, 1, 3}, hue: 0},
}

type auroraRay struct {
	x, hem, h, k float64
}

func auroraCyc(t, c, ph float64) float64 {
	return (tau*c*t)/auroraDuration + ph
}

type auroraInst struct {
	S        float64
	G, U, GM []uint32
	F        *noiseField
	I, HV    []float32
	BL, TMP  []float32
	rays     []auroraRay
	rb       rowBuf
}

func newAurora(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	G := pixelfx.Fit(o.Palette(), 7)
	U := pixelfx.Fit(o.Ramp("upper"), 7)
	GM := make([]uint32, 7)
	for i := range 7 {
		GM[i] = pixelfx.Mix(G[i], U[i], 0.5)
	}
	return &auroraInst{
		S:   S,
		G:   G,
		U:   U,
		GM:  GM,
		F:   field(S*5+1, 256, 128, cells8_16_32),
		I:   make([]float32, auroraW*auroraH),
		HV:  make([]float32, auroraW*auroraH),
		BL:  make([]float32, auroraW*auroraH),
		TMP: make([]float32, auroraW*auroraH),
	}
}

func (a *auroraInst) Update(float64)                     {}
func (a *auroraInst) DrawBack(*pixelfx.Painter, float64) {}

func (a *auroraInst) blur(src, dst []float32, r int) {
	n := float64(2*r + 1)
	for y := 0; y < auroraH; y++ {
		acc := 0.0
		o := y * auroraW
		for x := -r; x < auroraW+r; x++ {
			if x+r < auroraW {
				acc += float64(src[o+min(auroraW-1, x+r)])
			}
			if x-r-1 >= 0 {
				acc -= float64(src[o+x-r-1])
			}
			if x >= 0 && x < auroraW {
				a.TMP[o+x] = float32(acc / n)
			}
		}
	}
	for x := 0; x < auroraW; x++ {
		acc := 0.0
		for y := -r; y < auroraH+r; y++ {
			if y+r < auroraH {
				acc += float64(a.TMP[min(auroraH-1, y+r)*auroraW+x])
			}
			if y-r-1 >= 0 {
				acc -= float64(a.TMP[(y-r-1)*auroraW+x])
			}
			if y >= 0 && y < auroraH {
				dst[y*auroraW+x] = float32(acc / n)
			}
		}
	}
}

func (a *auroraInst) curtain(c auroraCurtainSpec, n int, t, breath float64) {
	const X0, Y0 = -160.0, -180.0
	nf := float64(n)
	fast := (256 * 2 * t) / auroraDuration
	slow := (256 * t) / auroraDuration
	p1, p2, lastX := 0.0, 0.0, -999.0
	for s := c.s0; s < c.s1; s += 0.5 {
		x := s + c.fold*math.Sin((tau*s)/c.flam+auroraCyc(t, c.c[0], nf*2)) + 5*math.Sin((tau*s)/64+auroraCyc(t, c.c[2], nf))
		if x < X0-2 || x >= X0+auroraW+1 {
			continue
		}
		hem := c.y + c.slope*s + c.amp*math.Sin((tau*s)/c.lam+auroraCyc(t, c.c[1], nf*1.7)) + c.rip*math.Sin((tau*s)/23-auroraCyc(t, 7, nf))
		env := pixelfx.Smooth(c.s0, c.s0+60, s) * (1 - pixelfx.Smooth(c.s1-70, c.s1, s))
		r1 := pixelfx.Clamp((sample(a.F, s*5-fast+nf*40, 10+nf*30) - 0.38) * 3.2)
		r2 := pixelfx.Clamp((sample(a.F, s*4.3+fast*0.7+nf*90, 50+nf*13) - 0.35) * 3)
		patch := sample(a.F, s*0.7+slow+nf*17, 80+nf*9)
		k := c.k * env * breath * (0.2 + 0.8*r1) * (0.3 + 1.4*math.Max(0, patch-0.3))
		if k < 0.01 {
			continue
		}
		h := c.height * (0.45 + 0.75*r2) * (0.75 + 0.5*patch)
		xi := int(math.Floor(x - X0))
		fr := x - X0 - float64(xi)
		if p1 > 0.75 && p1 >= p2 && p1 >= r1 && math.Abs(x-lastX) > 2 && k > 0.25 {
			a.rays = append(a.rays, auroraRay{x: x, hem: hem - 1, h: h * 0.9, k: math.Min(0.85, k*0.9)})
			lastX = x
		}
		p2 = p1
		p1 = r1
		if xi < -1 || xi >= auroraW {
			continue
		}
		top := max(0, int(math.Floor(hem-h-Y0)))
		bot := min(auroraH-1, int(math.Floor(hem-Y0))+3)
		inv := 1 / h
		k0, k1 := k*0.5*(1-fr), k*0.5*fr
		v := (hem - Y0 - 0.5 - float64(top)) * inv
		for yi := top; yi <= bot; yi, v = yi+1, v-inv {
			var pr float64
			if v >= 0 {
				pr = float64(auroraProf[min(200, int(pixelfx.ToInt32(v*200)))])
			} else {
				pr = math.Max(0, 0.45+v*h*0.16)
			}
			if pr <= 0 {
				continue
			}
			o := yi*auroraW + xi
			hv := v + c.hue
			if hv < 0 {
				hv = 0
			} else if hv > 1 {
				hv = 1
			}
			if xi >= 0 {
				a.I[o] = float32(float64(a.I[o]) + k0*pr)
				a.HV[o] = float32(float64(a.HV[o]) + k0*pr*hv)
			}
			if xi+1 < auroraW {
				a.I[o+1] = float32(float64(a.I[o+1]) + k1*pr)
				a.HV[o+1] = float32(float64(a.HV[o+1]) + k1*pr*hv)
			}
		}
	}
}

func (a *auroraInst) stars(q *clipper, t float64) {
	S, U := a.S, a.U
	for i := 0; i < 150; i++ {
		fi := float64(i)
		x := -160 + pixelfx.Rand(S, fi, 1)*320
		y := -180 + pixelfx.Rand(S, fi, 2)*175
		r := pixelfx.Rand(S, fi, 3)
		c := 1 + math.Floor(pixelfx.Rand(S, fi, 4)*4)
		tw := 0.5 + 0.5*math.Sin(auroraCyc(t, c, pixelfx.Rand(S, fi, 5)*tau))
		var col uint32
		if r < 0.6 {
			col = pixelfx.Mix(U[3], pixelfx.White, 0.2)
		} else if r < 0.9 {
			col = pixelfx.Mix(U[5], pixelfx.White, 0.5)
		} else {
			col = pixelfx.White
		}
		if r < 0.6 {
			q.dot(x, y, col, 0.35+0.5*tw)
		} else if r < 0.95 {
			q.px(x, y, col)
			q.glow(x+0.5, y+0.5, 3, U[5], 0.25*tw)
		} else {
			sparkle(q, x, y, 1+pixelfx.Round(tw*2), pixelfx.Mix(U[5], pixelfx.White, 0.6), U[4], 0.35*tw)
		}
	}
}

func (a *auroraInst) Draw(p *pixelfx.Painter, t float64) {
	q := clip(p, &Aurora.Meta)
	B := box(&Aurora.Meta)
	G, U, GM := a.G, a.U, a.GM
	a.stars(q, t)
	clear(a.I)
	clear(a.HV)
	breath := 0.75 + 0.25*math.Sin(auroraCyc(t, 1, -1.2))
	a.rays = a.rays[:0]
	for n, c := range auroraCurtains {
		a.curtain(c, n, t, breath)
	}
	a.blur(a.I, a.BL, 5)
	rows(p, &Aurora.Meta, &a.rb, float64(B.y0), -12, func(y float64, R *rowBuf) {
		iy := int(math.Floor(y))
		base := (iy - B.y0) * auroraW
		by := (iy & 3) << 2
		for xi := 0; xi < auroraW; xi++ {
			i := base + xi
			raw, bl := float64(a.I[i]), float64(a.BL[i])
			k := 0.0
			if bl > 0.015 {
				k = math.Min(0.4, bl*0.6)
			}
			if raw >= 0.03 {
				e := (raw * 1.5) / (1 + raw*1.5)
				v := float64(a.HV[i]) / raw
				th := 0.0
				if e > 0.45 {
					th = bayerTable[by|(xi&3)] * 0.06
				}
				lv := 1
				switch {
				case e > 0.86+th:
					lv = 6
				case e > 0.72+th:
					lv = 5
				case e > 0.56+th:
					lv = 4
				case e > 0.4+th:
					lv = 3
				case e > 0.24:
					lv = 2
				}
				if v > 0.35 {
					lv = min(lv, 5)
				}
				var c uint32
				if v < 0.3 {
					c = G[lv]
				} else if v < 0.5 {
					c = GM[lv]
				} else {
					c = U[lv]
				}
				if lv >= 3 {
					R.c[xi] = c
					R.a[xi] = float32(pixelfx.Clamp((e - 0.3) * 2.2))
					if lv < 5 {
						R.b[xi] = 1
					} else {
						R.b[xi] = 0
					}
				}
				if lv >= 3 {
					if v < 0.4 {
						R.l[xi] = G[4]
					} else {
						R.l[xi] = U[4]
					}
					k += e * 0.28
				} else {
					switch c {
					case G[lv]:
						R.l[xi] = G[4]
					case U[lv]:
						R.l[xi] = U[4]
					default:
						R.l[xi] = GM[4]
					}
					k += e * 0.72
				}
			} else {
				R.l[xi] = G[4]
			}
			R.k[xi] = float32(k)
		}
	})
	mix5 := pixelfx.Mix(G[5], U[5], 0.5)
	for _, r := range a.rays {
		n := int(pixelfx.Round(r.h))
		for j := 1; j < n; j++ {
			u := float64(j) / float64(n)
			col := U[5]
			if u < 0.35 {
				col = G[5]
			} else if u < 0.6 {
				col = mix5
			}
			q.blend(r.x, r.hem-float64(j), col, r.k*math.Pow(1-u, 1.3))
		}
		q.px(r.x, r.hem, G[6])
	}
}
