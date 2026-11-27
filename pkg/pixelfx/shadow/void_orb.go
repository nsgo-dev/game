// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/void-orb.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"
	"sync"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// VoidOrb hangs a black orb in the air with a pale rim crawling round it; a dark vortex winds into it, motes and streaks are pulled in, and light bends round its edge. It is registered as "shadow/void-orb".
var VoidOrb = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Void orb",
		Desc:     "A black orb hangs in the air, a pale rim crawling round it; a dark vortex winds into it, motes and streaks are pulled in, and light bends round its edge.",
		Tags:     []string{"shadow", "void", "orb", "vortex", "dark", "magic", "loop"},
		Size:     [2]int{168, 160},
		Origin:   [2]int{84, 150},
		Duration: 3.2,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newVoidOrb,
}

func init() { pixelfx.Register("shadow/void-orb", VoidOrb) }

// the orb's centre and radius, the vortex's reach, the radius light bends to (Einstein ring), the loop
// matter can't orbit inside voGap: there the dark is empty but for light bent round the orb
const (
	voY    = -62.0
	voR    = 12.0
	voVR   = 48
	voGap  = voR + 9
	voE    = voR + 4.5
	voD    = 3.2
	voArms = 3.0
	voK    = 7.0
)

var (
	voWind  = (tau * 2) / voD
	voGulps = [2]float64{0.2, 0.68}
)

func voWrap(x float64) float64 { return x - math.Floor(x) }

func voGulp(t float64) float64 {
	s := 0.0
	for _, g := range voGulps {
		d := ((voWrap(t/voD-g+0.5) - 0.5) * voD) / 0.07
		s += math.Exp(-(d * d))
	}
	return s
}

type voSource struct {
	cx, cy, rho, n, ph, s, sy float64
}

// specks of light behind the orb on looping paths; we see them through the lens: far off, a speck; passing behind, smeared into an arc hugging the rim, with a faint second image on the far side
var voSources = [7]voSource{
	{cx: 0, cy: 0, rho: 6, n: 1, ph: 0, s: 2.6, sy: 0.75},
	{cx: 1, cy: -1, rho: 8, n: -1, ph: 2.2, s: 2, sy: 0.6},
	{cx: 18, cy: -6, rho: 24, n: 1, ph: 0.4, s: 2.4, sy: 0.7},
	{cx: -26, cy: 10, rho: 30, n: 1, ph: 2.6, s: 2, sy: 0.55},
	{cx: 8, cy: 22, rho: 20, n: 2, ph: 4.1, s: 1.6, sy: 0.8},
	{cx: -12, cy: -26, rho: 34, n: 1, ph: 5.3, s: 1.8, sy: 0.6},
	{cx: 34, cy: 18, rho: 16, n: 2, ph: 1.2, s: 1.4, sy: 0.9},
}

type voTable struct {
	n             int
	r, th, ln, nz []float32
}

var (
	voTabOnce sync.Once
	voTab     *voTable
)

// voTables builds the tables round the orb once: radius, angle, log radius, and lumpy turbulence for the vortex.
func voTables() *voTable {
	voTabOnce.Do(func() {
		n := voVR * 2
		N := n * n
		r := make([]float32, N)
		th := make([]float32, N)
		ln := make([]float32, N)
		nz := make([]float32, N)
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				x, y := float64(i-voVR)+0.5, float64(j-voVR)+0.5
				k := j*n + i
				a := math.Atan2(y, x)
				d := pixelfx.Hypot(x, y)
				l := math.Log(math.Max(1, d))
				r[k] = float32(d)
				th[k] = float32(a)
				ln[k] = float32(l)
				nz[k] = float32(pixelfx.Fbm(math.Cos(a)*2.5+l*2.4, math.Sin(a)*2.5+l*2.4, 17, 3))
			}
		}
		voTab = &voTable{n: n, r: r, th: th, ln: ln, nz: nz}
	})
	return voTab
}

// voVortex draws the vortex: three arms of dark dust winding inward, a dithered veil threaded with thin bright filaments that stream in along them faster than the arms turn, a lit leading edge, all heating to pale violet near the rim.
func voVortex(p *pixelfx.Painter, C []uint32, t, g float64) {
	T := voTables()
	n, wf := T.n, (tau*12)/voD
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			k := j*n + i
			r := float64(T.r[k])
			if r < voGap || r > voVR {
				continue
			}
			th, ln, nz := float64(T.th[k]), float64(T.ln[k]), float64(T.nz[k])
			env := pixelfx.Smooth(voVR, voVR*0.42, r) * pixelfx.Smooth(voGap, voGap+4, r)
			ph := voArms*th + voK*ln + voWind*t + nz*2.6
			cs := math.Cos(ph)
			A := (0.5 + 0.5*cs) * env * (1 + 0.4*g)
			if A < 0.16 {
				continue
			}
			x, y := float64(i-voVR)+0.5, voY+float64(j-voVR)+0.5
			hot := 1 - pixelfx.Smooth(voGap, voGap+16, r)
			fil := math.Cos(12*th + 28*ln + wf*t + nz*1.6)
			// streams break into dashes that run inward with them
			dash := math.Cos(9*ln*2.2 + 2.2*th + wf*0.75*t)
			if fil > 0.86 && A > 0.3 && dash > -0.55 {
				c := C[5]
				if hot > 0.6 {
					c = C[7]
				} else if (hot > 0.25 || fil > 0.97) && dash > 0 {
					c = C[6]
				}
				p.Px(x, y, c)
				p.Light(x, y, c, 0.1+0.2*hot)
				continue
			}
			// the arm's leading edge, where it turns into the light
			if A > 0.36 && A < 0.46 && math.Sin(ph) > 0 {
				p.Px(x, y, pick(hot > 0.4, C[6], C[4]))
				continue
			}
			// the veil: translucent dark dust, denser in the arm's heart
			p.Blend(x, y, pick(A > 0.6, C[3], C[2]), math.Min(0.75, (A-0.16)*1.4))
		}
	}
}

// voLensing draws what the orb's gravity does to light from behind it.
func voLensing(p *pixelfx.Painter, C []uint32, t, g float64) {
	// the Einstein ring itself, faint, brightening as it gulps
	for a, st := 0.0, 0.7/voE; a < tau; a += st {
		f := 0.5 + 0.5*math.Cos(5*a-(tau*2*t)/voD) + 0.5*g
		c := C[3]
		if f > 0.85 {
			c = C[5]
		} else if f > 0.45 {
			c = C[4]
		}
		p.Px(math.Cos(a)*voE, voY+math.Sin(a)*voE, c)
	}
	for _, s := range voSources {
		q := (tau*s.n*t)/voD + s.ph
		sx, sy := s.cx+s.rho*math.Cos(q), s.cy+s.rho*math.Sin(q)*s.sy
		b := math.Max(0.3, pixelfx.Hypot(sx, sy))
		al := math.Atan2(sy, sx)
		root := math.Sqrt(b*b + 4*voE*voE)
		images := [2][3]float64{{(b + root) / 2, al, 1}, {(root - b) / 2, al + pi, 0.55}}
		for _, img := range images {
			r, a0, w := img[0], img[1], img[2]
			if r < voR+1.5 {
				continue
			}
			mag := (r / b) * w
			hw := math.Min(pi, (s.s*1.6)/(2*b))
			step := 0.6 / r
			for a := a0 - hw; a <= a0+hw+1e-6; a += step {
				u := 0.0
				if hw > 0 {
					u = math.Abs(a-a0) / hw
				}
				x, y := math.Cos(a)*r, voY+math.Sin(a)*r
				c := C[5]
				if u < 0.3 && mag > 0.8 {
					c = pixelfx.White
				} else if u < 0.65 {
					c = C[7]
				}
				p.Dot(x, y, c, math.Min(1, mag*1.4)*(1.15-u))
				if mag > 1.2 && u < 0.6 {
					p.Dot(x+math.Cos(a), y+math.Sin(a), C[6], 0.8)
				}
			}
			p.Glow(math.Cos(a0)*r, voY+math.Sin(a0)*r, 4+math.Min(10, hw*r), C[6], math.Min(0.5, 0.18*mag))
		}
	}
}

// voOrb draws the orb: void black, a dull glint and a crescent of reflected light inside, and a pale rim crawling round.
func voOrb(p *pixelfx.Painter, C []uint32, t, g float64) {
	p.Shade(-voR-1, voY-voR-1, voR+1, voY+voR+1, func(x, y float64, _ *pixelfx.Out) uint32 {
		dx, dy := x, y-voY
		d := math.Sqrt(dx*dx + dy*dy)
		if d > voR+0.5 {
			return 0
		}
		a := math.Atan2(dy, dx)
		if d > voR-0.6 {
			f := 0.5*math.Cos(3*a-(tau*2*t)/voD) + 0.3*math.Cos(7*a+(tau*3*t)/voD+1) + 0.25*math.Cos(13*a-(tau*5*t)/voD+2) + g
			switch {
			case f > 0.5:
				return pixelfx.White
			case f > 0.05:
				return C[6]
			case f > -0.4:
				return C[5]
			}
			return C[4]
		}
		if voR-d < 1.4 && math.Cos(a-0.8) > 0.6 {
			return C[2]
		}
		gx, gy := dx+voR*0.42, dy+voR*0.45
		if gx*gx+gy*gy < 1.6 {
			return C[3]
		}
		return C[0]
	})
}

func newVoidOrb(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	S := o.Seed()
	moteCols := []uint32{pixelfx.White, C[7], C[6], C[6], C[5]}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			g := voGulp(t)
			p.Glow(0, -1, 52, C[4], 0.22+0.1*g)
			voVortex(p, C, t, g)
			voLensing(p, C, t, g)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			g := voGulp(t)
			// dust drawn up off the floor, curving into the orb
			pixelfx.Cycle(t, voD, 14, S+2, func(r *pixelfx.Rng) float64 { return r.Range(1.6, 2.4) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				x0 := r.Range(-44, 44)
				u := math.Pow(age/L, 1.7)
				side := pixelfx.Sign(x0)
				if side == 0 {
					side = 1
				}
				ea := pi/2 + side*r.Range(0.3, 1)
				ex, ey := math.Cos(ea)*voR, voY+math.Sin(ea)*voR
				mx, my := x0*0.2, -18-r.Range(0, 12)
				x := (1-u)*(1-u)*x0 + 2*(1-u)*u*mx + u*u*ex
				y := (1-u)*(1-u)*-1 + 2*(1-u)*u*my + u*u*ey
				if age < 0.5 {
					p.Disc(x0, -1.5, 2+age*6, C[3], 0.4*(1-age/0.5))
				}
				c := C[5]
				if u > 0.6 {
					c = C[7]
				} else if u > 0.25 {
					c = C[6]
				}
				p.Dot(x, y, c, math.Min(1, age*4))
				if u > 0.3 {
					p.Glow(x, y, 3, C[5], 0.25)
				}
			})
			// motes swirl in along the arms, faster as they fall, and spark on the rim where they're swallowed
			pixelfx.Cycle(t, voD, 30, S, func(r *pixelfx.Rng) float64 { return r.Range(1.1, 1.8) }, func(age, L float64, r *pixelfx.Rng, _ int) {
				th0 := r.Range(0, tau)
				if math.Sin(th0) > 0.75 {
					th0 = -th0
				}
				r0 := r.Range(30, 64)
				sw := r.Range(1.4, 2.6)
				col := pixelfx.Pick(r, moteCols)
				pos := func(aa float64) (float64, float64, float64) {
					u := pixelfx.Clamp(aa / L)
					k := math.Pow(u, 1.8)
					rr := voR + 1 + (r0-voR-1)*(1-k)
					th := th0 - sw*(k+0.5*u)
					return math.Cos(th) * rr, voY + math.Sin(th)*rr, rr
				}
				x, y, rr := pos(age)
				tx, ty, _ := pos(age - 0.05)
				a := math.Min(1, age*5)
				if rr < voR+3 {
					p.Px(x, y, pixelfx.White)
					p.Glow(x, y, 5, C[6], 0.5)
					return
				}
				p.Line(tx, ty, (tx+x)/2, (ty+y)/2, C[4], a*0.8)
				p.Line((tx+x)/2, (ty+y)/2, x, y, col, a)
				p.Glow(x, y, 3, col, 0.3*a)
			})
			// streaks: long dotted lines shot in from further out, white at the head (never from under the floor)
			pixelfx.Cycle(t, voD, 9, S+1, pixelfx.Fixed(0.42), func(age, L float64, r *pixelfx.Rng, _ int) {
				th := r.Range(0, tau)
				if math.Sin(th) > 0.45 {
					th = -th
				}
				r0 := r.Range(56, 74)
				u := age / L
				k := (math.Exp(3*u) - 1) / (math.Exp(3) - 1)
				rr := r0 - (r0-voR-1)*k
				ln := 2 + 10*k
				ca, sa := math.Cos(th), math.Sin(th)
				for d := 0.0; d <= ln; d += 0.5 {
					c := C[4]
					if d < 1 {
						c = pixelfx.White
					} else if d < ln*0.5 {
						c = C[6]
					}
					al := 1.0
					if d >= ln*0.5 {
						al = 1.2 - d/ln
					}
					p.Dot(ca*(rr+d), voY+sa*(rr+d), c, al)
				}
				p.Glow(ca*rr, voY+sa*rr, 4, C[6], 0.4)
			})
			// a gulp draws a faint ring in after it
			for _, gp := range voGulps {
				u := voWrap(t/voD-gp) / (0.4 / voD)
				if u >= 1 {
					continue
				}
				rr := voR + 2 + (voVR-voR)*(1-pixelfx.Ease.InQuad(u))
				ringFast(p, 0, voY, rr, 1, C[4], 0.35*(1-u))
				ringLight(p, 0, voY, rr, 4, C[5], 0.3*(1-u), nil)
			}
			voOrb(p, C, t, g)
			halo(p, 0, voY, voR-0.5, voR+7, C[6], 0.6+0.3*g)
			halo(p, 0, voY, voR-0.5, voR+46, C[4], 0.38+0.15*g)
			rLim := voR - 0.6
			unlight(p, -voR, voY-voR, voR, voY+voR, func(x, y float64) bool {
				dy := y - voY
				return x*x+dy*dy < rLim*rLim
			}, 0)
		},
	}
}
