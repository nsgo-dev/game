// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/black-hole.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the dithered tone ramp and the brighter, bluer approaching side from Sprite Fusion's open-source black-hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), MIT License, Copyright (c) 2026 Hugo Duprez.

package shadow

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// BlackHole is a tilted disk of banded rings turning round a black hole, its far side bent up over the top and under the bottom; debris spirals in, stretches and is swallowed. It is registered as "shadow/black-hole".
var BlackHole = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Black hole",
		Desc:     "A tilted disk of banded rings turns round a black hole, its far side bent up over the top and under the bottom; debris spirals in, stretches and is swallowed.",
		Tags:     []string{"shadow", "black-hole", "void", "space", "gravity", "dark", "magic", "loop"},
		Size:     [2]int{184, 132},
		Origin:   [2]int{92, 124},
		Duration: 4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "rock", Default: pixelfx.Palette("#120d1c", "#2e2440", "#544468", "#857399")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newBlackHole,
}

func init() { pixelfx.Register("shadow/black-hole", BlackHole) }

// the hole's centre, its shadow, how hard it bends light; the disk's tilt (screen angle, and how open it is)
// light bent over the top left the far side's upper face, so the arch sees the disk more face-on (bhSiArch)
const (
	bhY      = -70.0
	bhRS     = 13.5
	bhE      = 17.5
	bhSI     = 0.25
	bhSiArch = 0.46
	bhTilt   = -0.14
	bhD      = 4.0
	bhROut   = 60.0
)

type bhBand struct {
	r0, r1, n, hot, k float64
}

var (
	bhCT    = math.Cos(bhTilt)
	bhST    = math.Sin(bhTilt)
	bhBands = []bhBand{
		{r0: 18, r1: 24, n: 5, hot: 1.35, k: 0.3},
		{r0: 26, r1: 34, n: 3, hot: 0.92, k: 1.9},
		{r0: 35.5, r1: 46, n: 2, hot: 0.64, k: 3.7},
		{r0: 47.5, r1: 60, n: 1, hot: 0.42, k: 5.2},
	}
	bhBayer = func() [16]float64 {
		var out [16]float64
		for i, v := range [16]float64{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5} {
			out[i] = (v + 0.5) / 16
		}
		return out
	}()
	bhIce = pixelfx.Hex("#e4f7ff")
)

type bhDiskSample struct {
	I, dop float64
	ok     bool
}

// bhDisk returns the brightness of the disk at a point of its plane (X across, Z away from us), banded, streaked, turning.
func bhDisk(X, Z, t float64) bhDiskSample {
	rho := math.Sqrt(X*X + Z*Z)
	for _, b := range bhBands {
		if rho < b.r0 || rho > b.r1 {
			continue
		}
		w := (rho - b.r0) / (b.r1 - b.r0)
		ph := math.Atan2(Z, X) - (tau*b.n*t)/bhD
		streak := 0.62 + 0.22*math.Cos(7*ph+rho*0.8+b.k) + 0.16*math.Cos(13*ph-rho*1.6+b.k*2)
		lane := 0.72 + 0.28*math.Cos(rho*2.3+b.k)
		edge := pixelfx.Smooth(0, 0.18, w) * (1 - pixelfx.Smooth(0.78, 1, w))
		return bhDiskSample{I: b.hot * streak * lane * (0.35 + 0.65*edge), dop: -X / rho, ok: true}
	}
	return bhDiskSample{}
}

// bhHole draws the hole and its disk, ray by ray: the near half straight in front, the far half seen through the bend (above the hole, and as a thin second image below it), the shadow, and the photon ring at its edge.
func bhHole(p *pixelfx.Painter, C []uint32, t, pulse float64) {
	ramp := [8]uint32{C[1], C[2], C[3], C[4], C[5], C[6], C[7], pixelfx.White}
	const n = len(ramp)
	const W, H = bhROut + 2, 42.0
	p.Shade(-W, bhY-H, W, bhY+H, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x, y-bhY
		u, v := dx*bhCT+dy*bhST, -dx*bhST+dy*bhCT
		r := math.Sqrt(u*u + v*v)
		var s bhDiskSample
		if v > 0 {
			s = bhDisk(u, -v/bhSI, t)
		}
		if !s.ok && r > bhRS {
			k := 1 - (bhE*bhE)/(r*r)
			bu, bv := u*k, v*k
			si := bhSI
			if k > 0 {
				si = bhSiArch
			}
			if -bv/si > 0 {
				s = bhDisk(bu, -bv/si, t)
			}
		}
		if !s.ok || s.I < 0.06 {
			if r < bhRS-0.4 {
				return C[0]
			}
			if r < bhRS+0.7 {
				a := math.Atan2(v, u)
				f := 0.5 + 0.5*math.Cos(3*a+(tau*2*t)/bhD) + pulse*0.4
				out.K = 0.3
				return pick(f > 0.55, pixelfx.White, C[7])
			}
			return 0
		}
		// tone: dithered between ramp steps; the side turning toward us brighter and icier
		I := s.I * 1.3 * (1 + 0.55*s.dop) * (1 + 0.25*pulse)
		f := pixelfx.Clamp(I) * float64(n-1)
		i := int(math.Floor(f))
		bi := ((pixelfx.ToInt32(math.Floor(y)) & 3) << 2) | (pixelfx.ToInt32(math.Floor(x)) & 3)
		if f-float64(i) > bhBayer[bi] {
			i = min(n-1, i+1)
		}
		if i == 0 && I < 0.12 {
			return 0
		}
		c := ramp[i]
		if s.dop > 0.35 && i >= n-3 {
			c = pixelfx.Mix(c, bhIce, math.Min(1, (s.dop-0.35)*2))
		}
		if s.dop < -0.4 && i >= 2 && i < n-2 {
			c = pixelfx.Mix(c, C[3], 0.35)
		}
		if i >= n-4 && (pixelfx.ToInt32(math.Floor(x)+math.Floor(y))&1) == 0 {
			out.K = 0.3 * math.Min(1, I-0.35)
		}
		return c
	})
}

func newBlackHole(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	K := pixelfx.Fit(o.Ramp("rock"), 4)
	S := o.Seed()
	moteCols := []uint32{pixelfx.White, C[7], C[6], C[5]}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			p.Glow(0, -1, 44, C[4], 0.12)
			p.Glow(0, bhY, 80, C[4], 0.32)
			p.Glow(0, bhY, 46, pixelfx.Mix(C[6], C[5], 0.5), 0.45)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			// a gulp as debris is swallowed, twice a loop
			pulse := math.Pow(math.Max(0, math.Cos((tau*2*t)/bhD)), 12)
			// pebbles torn off the floor, rising to join the disk
			pixelfx.Cycle(t, bhD, 10, S+3, func(r *pixelfx.Rng) float64 { return r.Range(1.6, 2.6) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				x0 := r.Range(-60, 60)
				u := math.Pow(age/L, 1.5)
				sx := x0
				if sx == 0 {
					sx = 1
				}
				tx := pixelfx.Sign(sx) * r.Range(40, 56)
				ty := bhY + r.Range(4, 9)
				x := x0 + (tx-x0)*u
				y := -1 + (ty+1)*u - math.Sin(pi*u)*10
				p.Px(x, y, pick(u > 0.6, K[3], K[2]))
				if age < 0.25 {
					p.Disc(x0, -1.5, 2+age*8, K[2], 0.4*(1-age/0.25))
				}
			})
			bhHole(p, C, t, pulse)
			// motes streaking in from all round, turning with the disk as they fall
			pixelfx.Cycle(t, bhD, 40, S+7, func(r *pixelfx.Rng) float64 { return r.Range(0.5, 0.9) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				a0 := r.Range(0, tau)
				d0 := r.Range(48, 76)
				q := math.Pow(age/L, 1.6)
				d := bhRS + 2 + (d0-bhRS-2)*(1-q)
				a := a0 + 0.9*q
				x, y := math.Cos(a)*d*1.15, bhY+math.Sin(a)*d*0.75
				q2 := math.Pow(math.Max(0, age-0.09)/L, 1.6)
				d2 := bhRS + 2 + (d0-bhRS-2)*(1-q2)
				a2 := a0 + 0.9*q2
				if d < bhRS+3 || y > -3 || y < -120 || bhY+math.Sin(a2)*d2*0.75 < -120 || math.Abs(x) > 90 {
					return
				}
				col := pixelfx.Pick(r, moteCols)
				al := math.Min(1, age*6)
				p.LineFn(math.Cos(a2)*d2*1.15, bhY+math.Sin(a2)*d2*0.75, x, y, func(u float64) (uint32, float64) {
					return pick(u > 0.6, col, C[4]), al
				})
				p.Glow(x, y, 3, col, 0.3)
			})
			// debris spiralling in: lumps of rock lit on the side toward the hole, stretched into hot streaks as they fall, gone in a flash at the photon ring; behind the hole they're hidden by it
			pixelfx.Cycle(t, bhD, 22, S, func(r *pixelfx.Rng) float64 { return r.Range(1.4, 2.4) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				ph0 := r.Range(0, tau)
				rho0 := r.Range(58, 80)
				hgt := r.Range(-8, 8)
				size := r.Range(1.6, 2.8)
				type pt struct{ x, y, Z, rho float64 }
				at := func(aa float64) pt {
					q := pixelfx.Clamp(aa / L)
					rho := bhRS + 1 + (rho0-bhRS-1)*(1-math.Pow(q, 1.5))
					ph := ph0 + 2.4*math.Pow(q, 1.6)*(1+1.5*q)
					X, Z := math.Cos(ph)*rho, math.Sin(ph)*rho
					sx, sy := X, -Z*bhSI+hgt*(1-q)
					return pt{x: sx*bhCT - sy*bhST, y: bhY + sx*bhST + sy*bhCT, Z: Z, rho: rho}
				}
				a, b := at(age), at(age-0.04)
				if a.Z > 0 && pixelfx.Hypot(a.x, a.y-bhY) < bhRS+1 {
					return
				}
				if a.rho < bhRS+3 {
					p.Px(a.x, a.y, pixelfx.White)
					p.Glow(a.x, a.y, 6, C[6], 0.6)
					return
				}
				heat := 1 - pixelfx.Smooth(16, 34, a.rho)
				ln := 1 + heat*7
				if heat > 0.05 {
					dx, dy := a.x-b.x, a.y-b.y
					l := pixelfx.Hypot(dx, dy)
					if l == 0 {
						l = 1
					}
					p.LineFn(a.x-(dx/l)*ln, a.y-(dy/l)*ln, a.x, a.y, func(u float64) (uint32, float64) {
						switch {
						case u > 0.7:
							return pixelfx.White, 1
						case u > 0.35:
							return C[6], 1
						}
						return C[4], 1
					})
					p.Glow(a.x, a.y, 4, C[6], 0.4*heat)
				} else {
					hd := pixelfx.Hypot(a.x, a.y-bhY)
					if hd == 0 {
						hd = 1
					}
					lx, ly := -a.x/hd, -(a.y-bhY)/hd
					// a lump of rock: a fading wake along its orbit, a dark body, its face toward the hole lit violet
					c, d := at(age-0.09), at(age-0.15)
					p.Px(b.x, b.y, C[3])
					p.Dot(c.x, c.y, C[3], 0.7)
					p.Dot(d.x, d.y, C[2], 0.5)
					s2 := size > 2.2
					p.Px(a.x, a.y, K[2])
					p.Px(a.x+1, a.y, K[1])
					p.Px(a.x, a.y+1, K[1])
					if s2 {
						p.Px(a.x+1, a.y+1, K[0])
						p.Px(a.x-1, a.y, K[1])
						p.Px(a.x, a.y-1, K[2])
					}
					p.Px(a.x+pixelfx.Round(lx*1.4), a.y+pixelfx.Round(ly*1.4), pick(s2, C[6], C[5]))
					p.Glow(a.x, a.y, 4, C[5], 0.15)
				}
			})
			halo(p, 0, bhY, bhRS-0.4, bhRS+4, C[6], 0.35+0.3*pulse)
			rLim := bhRS - 0.6
			unlight(p, -bhRS, bhY-bhRS, bhRS, bhY+bhRS, func(x, y float64) bool {
				dx, dy := x, y-bhY
				v, u := -dx*bhST+dy*bhCT, dx*bhCT+dy*bhST
				return u*u+v*v < rLim*rLim && !(v > 0 && bhDisk(u, -v/bhSI, t).ok)
			}, 0)
		},
	}
}
