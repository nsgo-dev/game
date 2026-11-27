// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/hologram.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package scifi

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Hologram is a floor projector throwing a cone of light holding a turning wireframe model: scanlines roll through it, a data ring circles it, and it flickers, tears and jitters. It is registered as "scifi/hologram".
var Hologram = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Hologram",
		Desc:     "A floor projector throws a cone of light holding a turning wireframe model: scanlines roll through it, a data ring circles it, and it flickers, tears and jitters.",
		Tags:     []string{"scifi", "hologram", "projector", "wireframe", "display", "glitch", "ground", "loop"},
		Size:     [2]int{112, 104},
		Origin:   [2]int{56, 92},
		Duration: holoDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#050c1e", "#0a1f48", "#123b80", "#1d64b8", "#2e98e8", "#6cd0ff", "#c8f0ff", "#ffffff")},
			{Name: "metal", Default: pixelfx.Palette("#0e0d14", "#1e1c27", "#312e3e", "#4b4759", "#6f6a82", "#9d98b0")},
			{Name: "split", Default: pixelfx.Color("#ff4fd8")},
			{Name: "seed", Default: pixelfx.Num(21)},
		},
	},
	New: newHologram,
}

func init() { pixelfx.Register("scifi/hologram", Hologram) }

const (
	holoDuration = 4.0
	holoOY       = -50.0
	holoRad      = 16.0
	holoLens     = -6.0
)

// the model: a tall six-sided crystal, a point at each end and a ring of six round its waist
var holoVerts = func() [8][3]float64 {
	var v [8][3]float64
	v[0] = [3]float64{0, 1.45, 0}
	v[1] = [3]float64{0, -1.45, 0}
	for i := range 6 {
		a := (float64(i) / 6) * tau
		v[2+i] = [3]float64{math.Cos(a) * 0.9, 0.12, math.Sin(a) * 0.9}
	}
	return v
}()

var holoFaces = func() [12][3]int {
	var f [12][3]int
	for i := range 6 {
		f[i*2] = [3]int{0, 2 + i, 2 + (i+1)%6}
		f[i*2+1] = [3]int{1, 2 + (i+1)%6, 2 + i}
	}
	return f
}()

var holoEdges = func() [18][2]int {
	var e [18][2]int
	for i := range 6 {
		e[i*3] = [2]int{0, 2 + i}
		e[i*3+1] = [2]int{1, 2 + i}
		e[i*3+2] = [2]int{2 + i, 2 + (i+1)%6}
	}
	return e
}()

// when it glitches: [start, length, how hard]
var holoGlitches = [3][3]float64{
	{0.92, 0.12, 1},
	{2.55, 0.2, 1.4},
	{3.42, 0.06, 0.7},
}

type holoTear struct{ y0, h, dx float64 }

func newHologram(o *pixelfx.Options) pixelfx.Instance {
	C, H, SPLIT, S := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("metal"), 6), o.Color("split"), o.Seed()

	return &pixelfx.Funcs{
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph, frame := t/holoDuration, math.Floor(t*30)
			gl := 0.0
			for _, g := range holoGlitches {
				if t >= g[0] && t < g[0]+g[1] {
					gl = math.Max(gl, g[2])
				}
			}
			flick := gl > 0 && pixelfx.Rand(S, frame) < 0.5
			// where it sits: a slow bob, a jolt when it glitches
			cx := 0.0
			if gl != 0 {
				if pixelfx.Rand(S, frame, 2) < 0.5 {
					cx = -2 * gl
				} else {
					cx = 2 * gl
				}
			}
			cy := holoOY + 1.5*math.Sin(tau*ph*2)
			// the tears this frame: bands of rows knocked sideways
			var tears [2]holoTear
			nTears := 0
			if gl != 0 {
				nTears = 2
				for i := range 2 {
					fi := float64(i)
					tears[i] = holoTear{
						y0: math.Floor(cy - holoRad + pixelfx.Rand(S, frame, fi)*holoRad*2),
						h:  2 + math.Floor(pixelfx.Rand(S, frame, fi, 1)*4),
						dx: pixelfx.Round((pixelfx.Rand(S, frame, fi, 2) - 0.5) * 10 * gl),
					}
				}
			}
			tearAt := func(y float64) float64 {
				for i := range nTears {
					tr := tears[i]
					if y >= tr.y0 && y < tr.y0+tr.h {
						return tr.dx
					}
				}
				return 0
			}
			// every pixel of the hologram goes through here: interlaced, torn, split
			put := func(x, y float64, c uint32, a float64) {
				yy := math.Floor(y)
				if flick && pixelfx.ToInt32(yy)&1 != 0 {
					return
				}
				dx := tearAt(yy)
				if dx != 0 {
					p.Dot(x+dx, y, SPLIT, a*0.8)
					p.Dot(x+dx+pixelfx.Sign(dx), y, c, a)
					return
				}
				p.Dot(x, y, c, a)
			}

			// the projector: a steel drum on the floor with a lens on top and lights round it
			p.Shade(-16, -8, 16, 2, func(x, y float64, out *pixelfx.Out) uint32 {
				top := (x/15)*(x/15)+((y+5)/3)*((y+5)/3) <= 1
				side := math.Abs(x) <= 15 && y > -5 && y <= 0
				if !top && !side {
					return 0
				}
				if top {
					switch {
					case (x/15)*(x/15)+((y+5)/3)*((y+5)/3) > 0.8:
						out.C = H[0]
					case y < -6:
						out.C = H[5]
					default:
						out.C = H[3]
					}
				} else {
					switch {
					case math.Abs(x) > 14 || y == 0:
						out.C = H[0]
					case y == -4:
						out.C = H[4]
					case x < -6:
						out.C = H[3]
					case x > 6:
						out.C = H[1]
					default:
						out.C = H[2]
					}
				}
				return 0
			})
			p.EllipseFn(0, holoLens+0.5, 5.5, 1.6, func(d, _ float64) (uint32, float64) {
				switch {
				case d < 0.4:
					return pixelfx.White, 1
				case d < 0.8:
					return C[6], 1
				}
				return C[4], 1
			})
			for i := range 4 {
				k := 0.5 + 0.5*math.Cos(tau*(2*ph-float64(i)/4))
				c := C[3]
				if k > 0.7 {
					c = C[6]
				}
				p.Px(-9+float64(i)*6, -2, c)
			}
			// the cone of light up to the model: thin, fading as it rises, scanlines climbing it
			top := cy + holoRad*1.3
			for y := math.Ceil(top); y < holoLens; y++ {
				v := (y - holoLens) / (top - holoLens)
				w := pixelfx.Lerp(4, holoRad+4, v)
				lineOn := pixelfx.Wrap(y+t*24, 5) < 1
				for x := math.Floor(-w); x <= math.Ceil(w); x++ {
					q := math.Abs(x+0.5) / w
					if q > 1 {
						continue
					}
					if q > 1-1/w {
						c := C[4]
						if v < 0.5 {
							c = C[5]
						}
						put(x, y, c, 1)
					} else {
						c, extra := C[4], 0.0
						if lineOn {
							c, extra = C[5], 0.06
						}
						p.Blend(x+0.5, y+0.5, c, (0.05+0.12*(1-v))*(1-0.6*q*q)+extra)
					}
				}
			}
			p.Glow(0, holoLens, 12, C[5], 0.6)
			// the stage the model stands on: a thin ring where the cone ends
			for i := range 56 {
				a := (float64(i)/56)*tau - ph*tau
				x, y := math.Cos(a)*(holoRad+4), top+math.Sin(a)*3
				if i%7 < 5 {
					if math.Sin(a) > 0 {
						put(x, y, C[6], 1)
					} else {
						put(x, y, C[4], 0.7)
					}
				}
			}
			p.GlowLine(-holoRad-4, top, holoRad+4, top, 4, C[4], 0.2)
			// data bits rising through the cone
			pixelfx.Cycle(t, holoDuration, 18, S+1, func(r *pixelfx.Rng) float64 { return r.Range(0.8, 1.4) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				y := pixelfx.Lerp(holoLens-2, top, k)
				w := pixelfx.Lerp(4, holoRad+2, k)
				x := r.Range(-0.8, 0.8) * w
				c := C[5]
				if r.Chance(0.3) {
					c = pixelfx.White
				}
				put(x, y, c, math.Min(1, (1-k)*2))
			})

			// the model: turning once a loop, tilted toward us
			spin, tilt := tau*ph, 0.32
			cs, ss := math.Cos(spin), math.Sin(spin)
			ct, st := math.Cos(tilt), math.Sin(tilt)
			var P3 [8][3]float64
			for i, v := range holoVerts {
				x1 := v[0]*cs + v[2]*ss
				z1 := -v[0]*ss + v[2]*cs
				y2 := v[1]*ct - z1*st
				z2 := v[1]*st + z1*ct
				P3[i] = [3]float64{cx + x1*holoRad, cy - y2*holoRad, z2}
			}
			// a scan bar climbing through it, twice a loop
			scanY := pixelfx.Lerp(cy+holoRad*1.5, cy-holoRad*1.5, math.Mod(ph*2, 1)/0.8)
			// the faces that face us, filled with scanlines
			var tri [3][2]float64
			for _, f := range holoFaces {
				a, b, c := P3[f[0]], P3[f[1]], P3[f[2]]
				nz := (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
				if nz >= 0 {
					continue
				}
				shade := pixelfx.Clamp(0.3 + 0.7*((a[2]+b[2]+c[2])/3+0.5))
				tri[0] = [2]float64{a[0], a[1]}
				tri[1] = [2]float64{b[0], b[1]}
				tri[2] = [2]float64{c[0], c[1]}
				p.PolyFn(tri[:], func(_, y float64) (uint32, float64) {
					col := C[3]
					if math.Abs(y-scanY) < 2 {
						col = C[6]
					} else if shade > 0.6 {
						col = C[4]
					}
					yy := pixelfx.ToInt32(math.Floor(y))
					if (yy%3 == 0 && !(flick && yy&1 != 0)) || math.Abs(y-scanY) < 1.5 {
						return col, 1
					}
					return col, 0
				})
			}
			// the edges: the far ones dim, the near ones bright, hot where the scan bar crosses them
			type edge3 struct {
				a, b [3]float64
				z    float64
			}
			var edges [18]edge3
			for i, e := range holoEdges {
				a, b := P3[e[0]], P3[e[1]]
				edges[i] = edge3{a: a, b: b, z: (a[2] + b[2]) / 2}
			}
			slices.SortStableFunc(edges[:], func(a, b edge3) int { return cmp.Compare(a.z, b.z) })
			for _, e := range edges {
				a, b, z := e.a, e.b, e.z
				L := pixelfx.Hypot(b[0]-a[0], b[1]-a[1])
				for d := 0.0; d <= L; d += 0.5 {
					x, y := pixelfx.Lerp(a[0], b[0], d/L), pixelfx.Lerp(a[1], b[1], d/L)
					hot := math.Abs(y-scanY) < 1.5
					if z < -0.15 {
						c := C[3]
						if hot {
							c = C[5]
						}
						put(x, y, c, 0.7)
					} else {
						c := C[5]
						if hot {
							c = pixelfx.White
						} else if z > 0.4 {
							c = C[6]
						}
						put(x, y, c, 1)
					}
				}
			}
			for _, v := range P3 {
				if v[2] < -0.15 {
					continue
				}
				put(v[0], v[1], pixelfx.White, 1)
				p.Glow(v[0], v[1], 4, C[5], 0.3)
			}
			// a dashed data ring round its waist, its far half behind
			for i := range 64 {
				a := (float64(i)/64)*tau + ph*tau*2
				x, y := cx+math.Cos(a)*(holoRad+9), cy+3+math.Sin(a)*4
				if (i/3)%3 == 2 {
					continue
				}
				if math.Sin(a) > 0 {
					c := C[5]
					if i%9 == 0 {
						c = pixelfx.White
					}
					put(x, y, c, 1)
				} else {
					put(x, y, C[3], 0.6)
				}
			}
			// light: the model glows, the scan bar glows, a flicker drops the light too
			lk := 1.0
			if flick {
				lk = 0.5
			}
			p.Glow(cx, cy, holoRad*2.2, C[4], 0.36*lk)
			p.Glow(cx, cy-holoRad*0.6, holoRad, C[5], 0.15*lk)
			if math.Abs(scanY-cy) < holoRad*1.4 {
				p.GlowLine(cx-holoRad, scanY, cx+holoRad, scanY, 5, C[5], 0.3*lk)
			}
			p.Glow(0, -2, 22, C[4], 0.2)
		},
	}
}
