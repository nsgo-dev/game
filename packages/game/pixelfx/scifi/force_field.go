// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/force-field.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the hit recipe (tracer from the right, flash and ripple over a hex shell mapped by longitude and latitude, sparks glancing off) from Sprite Fusion's open-source shield (https://www.spritefusion.com/pixel-art-vfx/shield), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	ffHold = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1), pixelfx.FX.Is(-9), pixelfx.FY.Is(-38), pixelfx.BX.Is(10), pixelfx.BY.Is(-38), pixelfx.FFX.Is(-5), pixelfx.FBX.Is(6))
	ffHit  = ffHold.With(pixelfx.X.Is(-2), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(-3))
)

// ForceField is a glassy dome of hex plates over the hero: plates glint, tracers from the right slam into it, a ripple and cracks of light race across, and sparks glance off. It is registered as "scifi/force-field".
var ForceField = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Force field",
		Desc:     "A glassy dome of hex plates over the hero: plates glint, tracers from the right slam into it, a ripple and cracks of light race across, sparks glance off.",
		Tags:     []string{"scifi", "shield", "barrier", "dome", "hex", "hit", "defense", "loop"},
		Size:     [2]int{184, 104},
		Origin:   [2]int{76, 84},
		Duration: ffDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0b1030", "#152a63", "#1d4f9e", "#2582d2", "#36b9ee", "#8ae8ff", "#d4fbff", "#ffffff")},
			{Name: "sparks", Default: pixelfx.Palette("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")},
			{Name: "radius", Default: pixelfx.Num(44)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: 0, Pose: ffHold},
			{T: 0.42, Pose: ffHold},
			{T: 0.42, Pose: ffHit, How: "snap"},
			{T: 0.7, Pose: ffHold.With(pixelfx.X.Is(-1))},
			{T: 1.1, Pose: ffHold},
			{T: 1.5, Pose: ffHold},
			{T: 1.5, Pose: ffHit.With(pixelfx.Crouch.Is(4), pixelfx.Lean.Is(-2)), How: "snap"},
			{T: 1.75, Pose: ffHold},
			{T: 2.4, Pose: ffHold},
		},
		Hits: []pixelfx.Hit{
			{T: 0.42, Stop: 0.07, Shake: 0.16, Amp: pixelfx.Val(2)},
			{T: 1.5, Stop: 0.03, Shake: 0.08, Amp: pixelfx.Val(1)},
		},
	},
	New: newForceField,
}

func init() { pixelfx.Register("scifi/force-field", ForceField) }

const (
	ffDuration = 2.4
	ffG        = 6.3
	ffKY       = 1.12
	ffRightW   = 184 - 76
)

var (
	ffS3    = math.Sqrt(3)
	ffSA    = 0.13
	ffCA    = math.Sqrt(1 - ffSA*ffSA)
	ffSmoke = pixelfx.Hex("#5b5f78")
)

type ffShot struct {
	t, lon, lat float64
	big         bool
	N           [3]float64
}

var ffShots = func() []ffShot {
	base := []ffShot{
		{t: 0.42, lon: 0.66, lat: 0.62, big: true},
		{t: 1.5, lon: 0.95, lat: 0.26, big: false},
	}
	for i := range base {
		s := &base[i]
		s.N = [3]float64{math.Cos(s.lat) * math.Sin(s.lon), math.Sin(s.lat), math.Cos(s.lat) * math.Cos(s.lon)}
	}
	return base
}()

type ffCell struct {
	q, r              float64
	hx, hy, e, corner float64
}

func ffHexCell(gx, gy float64) ffCell {
	fq, fr := (ffS3/3)*gx-gy/3, (2.0/3)*gy
	fs := -fq - fr
	q, r, s := pixelfx.Round(fq), pixelfx.Round(fr), pixelfx.Round(fs)
	dq, dr, ds := math.Abs(q-fq), math.Abs(r-fr), math.Abs(s-fs)
	if dq > dr && dq > ds {
		q = -r - s
	} else if dr > ds {
		r = -q - s
	}
	hx, hy := ffS3*(q+r/2), 1.5*r
	dx, dy := gx-hx, gy-hy
	ax := math.Abs(dx)
	e := math.Max(ax, math.Max(math.Abs(ax/2+(dy*ffS3)/2), math.Abs(ax/2-(dy*ffS3)/2))) / (ffS3 / 2)
	k := pixelfx.Round((math.Atan2(dy, dx) - pi/6) / (pi / 3))
	ca := pi/6 + (k*pi)/3
	return ffCell{q: q, r: r, hx: hx, hy: hy, e: e, corner: pixelfx.Hypot(dx-math.Cos(ca), dy-math.Sin(ca))}
}

func ffProject(R, lon, lat float64) (float64, float64, float64) {
	X, Y, Z := math.Cos(lat)*math.Sin(lon), math.Sin(lat), math.Cos(lat)*math.Cos(lon)
	return R * X, -R * (Y*ffCA - Z*ffSA), Y*ffSA + Z*ffCA
}

type ffPix struct {
	x, y        int
	X, Y, Z     float64
	lon, lat    float64
	q, r        float64
	id          int
	e, corner   float64
	rim, sil    float64
	seam, bevel bool
}

type ffShell struct {
	near, far []ffPix
}

func buildFFShell(R float64) ffShell {
	N := int(math.Ceil(R)) + 1
	W := 2*N + 1
	maxY := int(math.Ceil(R*ffSA)) + 1
	ids := [2]map[int]int{map[int]int{}, map[int]int{}}
	var near, far []ffPix
	for y := -N; y <= maxY; y++ {
		for x := -N; x <= N; x++ {
			u, vy := (float64(x)+0.5)/R, -(float64(y)+0.5)/R
			q := u*u + vy*vy
			if q > 1 {
				continue
			}
			vz := math.Sqrt(1 - q)
			for _, s := range [2]float64{1, -1} {
				Y, Z := vy*ffCA+s*vz*ffSA, -vy*ffSA+s*vz*ffCA
				if Y < 0 {
					continue
				}
				lon, lat := math.Atan2(u, Z), math.Asin(math.Min(1, Y))
				c := ffHexCell(lon*ffG, lat*ffG*ffKY)
				id := int(c.q)*1000 + int(c.r)
				px := ffPix{
					x: x, y: y, X: u, Y: Y, Z: Z, lon: lon, lat: lat,
					q: c.q, r: c.r, id: id, e: c.e, corner: c.corner,
					rim: 1 - vz, sil: (1 - math.Sqrt(q)) * R,
				}
				if s > 0 {
					near = append(near, px)
					ids[0][(y+N)*W+x+N] = id
				} else {
					far = append(far, px)
					ids[1][(y+N)*W+x+N] = id
				}
			}
		}
	}
	mark := func(list []ffPix, m map[int]int) {
		for i := range list {
			q := &list[i]
			r, okR := m[(q.y+N)*W+q.x+1+N]
			d, okD := m[(q.y+1+N)*W+q.x+N]
			l, okL := m[(q.y+N)*W+q.x-1+N]
			u, okU := m[(q.y-1+N)*W+q.x+N]
			q.seam = (okR && r != q.id) || (okD && d != q.id)
			q.bevel = !q.seam && ((okL && l != q.id) || (okU && u != q.id))
		}
	}
	mark(near, ids[0])
	mark(far, ids[1])
	return ffShell{near: near, far: far}
}

type ffCrackPt struct{ x, y, f, u float64 }

func buildFFCracks(R, seed float64, s ffShot) [][]ffCrackPt {
	rn := pixelfx.NewRng(seed*13 + 5)
	var out [][]ffCrackPt
	toScreen := func(path [][2]float64, u0, u1 float64) []ffCrackPt {
		var pts []ffCrackPt
		for i, pt := range path {
			lon, lat := pt[0], pt[1]
			if lat < 0.03 || lat > 1.52 || math.Abs(lon) > 1.5 {
				break
			}
			sx, sy, f := ffProject(R, lon, lat)
			pts = append(pts, ffCrackPt{x: sx, y: sy, f: f, u: u0 + ((u1-u0)*float64(i))/float64(len(path)-1)})
		}
		return pts
	}
	for i := range 5 {
		a := (float64(i)/5)*tau + rn.Range(-0.35, 0.35)
		L := rn.Range(0.42, 0.8)
		path := pixelfx.Bolt(rn, s.lon, s.lat, s.lon+(math.Cos(a)*L)/math.Cos(s.lat), s.lat+math.Sin(a)*L, 0.2, 4)
		out = append(out, toScreen(path, 0, 1))
		for _, fk := range pixelfx.Forks(rn, path, pixelfx.ForkOpts{Count: int(rn.Int(0, 2)), Len: L * 0.5, Jag: 0.25, Spread: 0.9}) {
			j := -1
			for idx, q := range path {
				if q[0] == fk[0][0] && q[1] == fk[0][1] {
					j = idx
					break
				}
			}
			u0 := math.Max(0, float64(j)) / float64(len(path)-1)
			out = append(out, toScreen(fk, u0, u0+0.5))
		}
	}
	return out
}

func ffGlints(S, q, r float64) bool { return pixelfx.Rand(S, q, r, 3) < 0.2 }

func ffGlintAt(S, q, r float64) float64 {
	g := pixelfx.Rand(S, q, r) * 1.7
	if g < 1.4 {
		return 1 + g
	}
	return g - 1.4
}

func buildFFGhosts(seed float64, sparks, C []uint32, R float64) *ghosts {
	rn := pixelfx.NewRng(seed)
	g := newGhosts(ffDuration, ghostOpts{
		ground: zeroGround,
		sparks: sparks,
		hasBox: true,
		box:    [4]float64{-70, -80, ffRightW - 6, 14},
	})
	for _, s := range ffShots {
		hx, hy, _ := ffProject(R, s.lon, s.lat)
		n := 0.55
		if s.big {
			n = 1
		}
		// glancing off the curve: up and away to the right, the way the shot was going when it was turned
		baseA := -0.55
		if s.big {
			baseA = -0.9
		}
		for i := 0; float64(i) < 24*n; i++ {
			a := baseA + rn.Range(-0.7, 0.65)
			sp := rn.Range(100, 280) * (0.7 + 0.3*n)
			born := s.t + rn.Range(0, 0.02)
			life := rn.Range(0.25, 0.75)
			g.add(born, pixelfx.Spark, hx, hy, math.Cos(a)*sp, math.Sin(a)*sp-30, life, pixelfx.White, 1, 0)
		}
		for i := 0; float64(i) < 7*n; i++ {
			a := -0.9 + rn.Range(-1.2, 1.2)
			sp := rn.Range(30, 90)
			born := s.t + rn.Range(0, 0.05)
			life := rn.Range(0.3, 0.6)
			col := pixelfx.Pick(rn, []uint32{C[5], C[6], C[4]})
			g.add(born, pixelfx.Mote, hx, hy, math.Cos(a)*sp, math.Sin(a)*sp-15, life, col, 0.7, 0)
		}
		for i := 0; float64(i) < 3*n; i++ {
			x := hx + rn.Range(-6, 6)
			y := hy + rn.Range(-5, 5)
			life := rn.Range(0.3, 0.5)
			size := rn.Range(1, 2)
			g.add(s.t+0.04+float64(i)*0.07, pixelfx.StarKind, x, y, 0, -4, life, C[6], size, 0)
		}
		for i := 0; float64(i) < 3*n; i++ {
			x := hx + 2 + rn.Range(-2, 2)
			vx := rn.Range(8, 22)
			vy := rn.Range(-14, -4)
			life := rn.Range(0.9, 1.3)
			size := rn.Range(2.2, 3.4)
			g.add(s.t+0.03+float64(i)*0.04, pixelfx.Smoke, x, hy-1, vx, vy, life, ffSmoke, size, 0)
		}
	}
	// a star on each plate as it glints
	for r := 0.0; r < 12; r++ {
		for q := -14.0; q <= 14; q++ {
			if !ffGlints(seed, q, r) {
				continue
			}
			lon, lat := (ffS3*(q+r/2))/ffG, (1.5*r)/(ffG*ffKY)
			x, y, f := ffProject(R, lon, lat)
			if f < 0.25 || lat < 0.15 || lat > 1.45 || math.Abs(lon) > 1.3 {
				continue
			}
			g.add(ffGlintAt(seed, q, r)+0.03, pixelfx.StarKind, x, y, 0, -3, 0.4, C[6], 1.4+pixelfx.Rand(q, r, 9), 0)
		}
	}
	return g
}

func newForceField(o *pixelfx.Options) pixelfx.Instance {
	C, SP, R, S := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("sparks"), 4), o.Num("radius"), o.Seed()
	sh := buildFFShell(R)
	cracks := buildFFCracks(R, S, ffShots[0])
	g := buildFFGhosts(S, SP, C, R)

	drawShell := func(p *pixelfx.Painter, t float64, near bool) {
		ph := t / ffDuration
		var ages [2]float64
		for i, s := range ffShots {
			ages[i] = since(t, s.t, ffDuration)
		}
		// the scan: a band of light climbing the dome once a loop
		sk := pixelfx.Clamp((ph - 0.72) / 0.26)
		scanLat, scanK := -0.2+sk*2, math.Sin(pi*sk)
		list := sh.far
		if near {
			list = sh.near
		}
		for i := range list {
			q := &list[i]
			ff := q.rim * q.rim
			// the ripples: a front racing out over the sphere from each hit, the plates behind it still humming
			rp, hot := 0.0, 0.0
			for si, s := range ffShots {
				age := ages[si]
				if age >= 1 {
					continue
				}
				d := math.Acos(pixelfx.ClampTo(q.X*s.N[0]+q.Y*s.N[1]+q.Z*s.N[2], -1, 1))
				f := 0.06 + 3.1*age
				scale := 0.7
				if s.big {
					scale = 1
				}
				k := (1 - age) * scale
				z := (d - f) / 0.15
				v1 := math.Exp(-(z * z)) * k
				v2 := 0.0
				if d < f {
					v2 = 0.22 * k * k * (1 - d/f)
				}
				rp = math.Max(rp, math.Max(v1, v2))
				hr := 0.17
				if s.big {
					hr = 0.3
				}
				if age < 0.55 && d < hr {
					if age < 0.07 && d < hr*0.75 {
						hot = math.Max(hot, 2)
					} else {
						hot = math.Max(hot, math.Pow(1-age/0.55, 1.5)*(1.1-(0.6*d)/hr))
					}
				}
			}
			// plates glint now and then
			gl := 0.0
			if ffGlints(S, q.q, q.r) {
				a := since(t, ffGlintAt(S, q.q, q.r), ffDuration)
				if a < 0.45 {
					gl = 1 - a/0.45
				}
			}
			sd := (q.lat - scanLat) / 0.09
			sc := scanK * math.Exp(-(sd * sd))
			fx, fy := float64(q.x), float64(q.y)
			if !near {
				// the far side, seen through the glass: dim seams, a breath of tint
				if q.seam {
					c := C[3]
					if rp > 0.3 {
						c = C[5]
					}
					p.Blend(fx+0.5, fy+0.5, c, pixelfx.Clamp(0.18+0.25*ff+0.6*rp+0.3*sc))
				} else {
					p.Blend(fx+0.5, fy+0.5, C[2], 0.04+0.06*ff+0.15*rp)
				}
				continue
			}
			if q.sil < 1.15 {
				// the rim: a hard bright edge, whiter where the light catches it
				lit := -q.X*0.55 + q.Y*0.85
				c := C[5]
				if rp > 0.4 || lit > 0.8 {
					c = pixelfx.White
				} else if lit > 0.3 {
					c = C[6]
				}
				p.Px(fx, fy, c)
				p.Light(fx, fy, C[4], 0.3)
				continue
			}
			if hot >= 2 {
				// the hero frame: the struck plates go white
				c := pixelfx.White
				if q.seam {
					c = C[6]
				}
				p.Px(fx, fy, c)
				continue
			}
			if q.seam {
				corner := 0.0
				if q.corner < 0.32 {
					corner = 0.18
				}
				I := 0.12 + 0.5*ff + 0.08*math.Sin(q.lon*2.3+q.lat*3-ph*tau) + corner + 0.9*gl + 0.7*sc + 1.3*rp + hot
				c := C[4]
				switch {
				case gl > 0.6 || rp > 0.55 || hot > 0.5:
					c = pixelfx.White
				case I > 0.8:
					c = C[6]
				case I > 0.55:
					c = C[5]
				}
				if I > 0.36 {
					p.Px(fx, fy, c)
				} else {
					p.Blend(fx+0.5, fy+0.5, c, 0.3+I)
				}
				p.Light(fx, fy, C[4], 0.04+0.08*I)
			} else {
				// the glass: thin face-on, thicker toward the rim, a lit top-left edge on every plate
				bevel := 0.0
				if q.bevel {
					bevel = 0.12 + 0.2*ff
				}
				a := 0.05 + 0.34*ff*q.rim + 0.1*math.Pow(q.e, 4) + bevel + 0.35*gl + 0.1*sc + 0.3*rp + 0.6*hot
				c := C[3]
				switch {
				case hot > 0.6:
					c = pixelfx.White
				case hot > 0.3 || gl > 0.5:
					c = C[6]
				case rp > 0.4 || q.bevel:
					c = C[5]
				case ff > 0.5:
					c = C[4]
				}
				p.Blend(fx+0.5, fy+0.5, c, pixelfx.Clamp(a))
				if rp > 0.1 || hot > 0 || gl > 0 {
					p.Light(fx, fy, C[4], 0.06*(rp+hot+gl))
				}
			}
		}
	}

	// the dome's foot: a ring on the floor with emitters round it, chasing
	drawFoot := func(p *pixelfx.Painter, t float64, near bool) {
		keep := func(g float64) bool { return (math.Sin(g) >= 0) == near }
		pixelfx.Band(p, 0, 0, R+1, ffSA, 2, 1, []uint32{C[1], C[3], C[5], C[6]}, keep, false)
		for i := range 10 {
			ang := (float64(i)/10)*tau + 0.31
			if !keep(ang) {
				continue
			}
			x, y := math.Cos(ang)*(R-0.5), math.Sin(ang)*R*ffSA
			k := 0.5 + 0.5*math.Cos(tau*(2*(t/ffDuration)-float64(i)/10))
			p.Px(x-1, y, C[2])
			p.Px(x+1, y, C[2])
			c := C[5]
			if k > 0.6 {
				c = pixelfx.White
			}
			p.Px(x, y, c)
			gk := 0.15
			if near {
				gk = 0.25
			}
			p.Glow(x, y, 6+4*k, C[4], gk*(0.4+k))
		}
	}

	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			for x := -R; x <= R; x += R / 3 {
				p.Glow(x, -1, 22, C[3], 0.1)
			}
			drawFoot(p, t, false)
			drawShell(p, t, false)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			drawShell(p, t, true)
			drawFoot(p, t, true)
			for _, s := range ffShots {
				age := since(t, s.t, ffDuration)
				hx, hy, _ := ffProject(R, s.lon, s.lat)
				n := 0.6
				if s.big {
					n = 1
				}
				// the fractures: race out from the hit white-hot, then cool down the ramp and fade
				if s.big && age < 0.9 {
					fade, reach := 1-pixelfx.Smooth(0.35, 0.9, age), pixelfx.Clamp(age/0.09)
					for pass := range 2 {
						for _, cr := range cracks {
							for i := 1; i < len(cr); i++ {
								p0, p1 := cr[i-1], cr[i]
								if p1.u > reach || p0.f < 0.05 || p1.f < 0.05 {
									continue
								}
								k := fade * (1.2 - p1.u*0.4)
								if pass == 0 {
									if age < 0.32 {
										c := C[5]
										if age < 0.12 {
											c = C[6]
										}
										for _, off := range [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
											p.Line(p0.x+off[0], p0.y+off[1], p1.x+off[0], p1.y+off[1], c, k*(1-age/0.32))
										}
									}
									continue
								}
								c := C[5]
								if age < 0.12+p1.u*0.08 {
									c = pixelfx.White
								} else if age < 0.3+p1.u*0.1 {
									c = C[6]
								}
								p.Line(p0.x, p0.y, p1.x, p1.y, c, k)
								if i%3 == 0 {
									p.Glow(p1.x, p1.y, 6, C[4], 0.35*fade*(1-p1.u*0.5))
								}
							}
						}
					}
				}
				// the tracer, in from the right: a streak that comes out of the edge, its tail never past where it entered
				tk := (t - (s.t - 0.1)) / 0.1
				if tk >= 0 && tk < 1 {
					sx, sy := float64(ffRightW-3), hy-12
					L := pixelfx.Hypot(sx-hx, sy-hy)
					at := func(d float64) (float64, float64) {
						dd := math.Max(0, d)
						return sx + ((hx-sx)*dd)/L, sy + ((hy-sy)*dd)/L
					}
					h := L * tk
					x, y := at(h)
					x2, y2 := at(h - 2)
					x7, y7 := at(h - 7)
					x16, y16 := at(h - 16)
					p.Line(x16, y16, x7, y7, SP[3], 1)
					p.Line(x7, y7, x2, y2, SP[1], 1)
					p.Line(x2, y2, x, y, SP[0], 1)
					p.Px(x, y, pixelfx.White)
					p.Glow(x, y, 9, SP[1], 0.55)
				}
				// the impact: a white star, light that flares and dies
				if age < 0.14 {
					pixelfx.Sparkle(p, hx, hy, (7*(1-age/0.14)+1)*n, C[6], C[5], 0.4)
				}
				flash(p, t, s.t, hx, hy, 56*n, C[4], 0.45, 0.45)
				flash(p, t, s.t, hx, hy, 12, pixelfx.White, 0.5*n, 0.08)
				flash(p, t, s.t, hx+3, hy, 22, SP[1], 0.4*n, 0.15)
			}
			// the dome's own light, and what glances off
			a0 := since(t, ffShots[0].t, ffDuration)
			p.Glow(0, -R*0.45, R*1.3, C[3], 0.11+0.1*math.Max(0, 1-a0*2))
			g.at(t).Draw(p, pixelfx.LayerBoth)
		},
	}
}
