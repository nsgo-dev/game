// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/divine-shield.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package holy

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var shieldBrace = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.7), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1), pixelfx.FX.Is(5), pixelfx.FY.Is(-33), pixelfx.BX.Is(10), pixelfx.BY.Is(-20), pixelfx.FFX.Is(4), pixelfx.FBX.Is(-2))

// DivineShield is a bubble of gold light round the character: a bright rim and a thin gold film, meridians turning like a slow globe, a sheen sweeping across and sparkles winking on it. It is registered as "holy/divine-shield".
var DivineShield = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Divine shield",
		Desc:     "A bubble of gold light round the character: a bright rim and a thin gold film, meridians turning like a slow globe, a sheen sweeping across and sparkles winking on it.",
		Tags:     []string{"holy", "shield", "bubble", "barrier", "buff", "character", "loop"},
		Size:     [2]int{88, 84},
		Origin:   [2]int{44, 68},
		Duration: shieldT,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: goldPalette()},
			{Name: "radius", Default: pixelfx.Num(29)},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: shieldBrace},
			{T: 0.6, Pose: shieldBrace.With(pixelfx.Crouch.Is(4))},
			{T: 1.2, Pose: shieldBrace.With(pixelfx.FY.Is(-35), pixelfx.Crouch.Is(3))},
			{T: 1.8, Pose: shieldBrace.With(pixelfx.Crouch.Is(4))},
			{T: 2.4, Pose: shieldBrace},
		},
	},
	New: newDivineShield,
}

func init() { pixelfx.Register("holy/divine-shield", DivineShield) }

const (
	shieldT = 2.4
	shieldN = 4
)

var shieldCY, shieldTilt, shieldFloorSY = -22.0, 0.34, 0.26

// shieldOnSphere returns a point of the sphere (lat, lon) seen from a little above: screen x, y and depth z (> 0 faces us).
func shieldOnSphere(r, lat, lon float64) (x, y, z float64) {
	X, Y, Z := r*math.Cos(lat)*math.Sin(lon), r*math.Sin(lat), r*math.Cos(lat)*math.Cos(lon)
	return X, shieldCY - (Y*math.Cos(shieldTilt) - Z*math.Sin(shieldTilt)), Y*math.Sin(shieldTilt) + Z*math.Cos(shieldTilt)
}

// shieldSheen is where the sheen, a diagonal band crossing the bubble once a loop then resting, is at t.
func shieldSheen(t float64) float64 {
	return -1.7 + 3.4*pixelfx.Clamp(math.Mod(t/shieldT, 1)/0.55)
}

// shieldDot is one pixel of the lattice: where it is, how far it faces away (f) and whether the sheen lights it.
type shieldDot struct {
	x, y, f float64
	hot     bool
}

type shieldInst struct {
	C    []uint32
	r    float64
	seed float64
	A    *pixelfx.Actor
	S    *shape
	// seen indexes dots, the lattice's pixels, each drawn once, in the order they were first reached.
	seen map[float64]int
	dots []shieldDot
}

func newDivineShield(o *pixelfx.Options) pixelfx.Instance {
	s := &shieldInst{C: pixelfx.Fit(o.Palette(), 8), r: bounded(o.Num("radius"), 22, 256), seed: o.Seed(), A: o.Actor, seen: map[float64]int{}}
	if s.A != nil {
		s.S = shapeOf(s.A)
	}
	return s
}

func (s *shieldInst) Update(float64) {}

func (s *shieldInst) lattice(p *pixelfx.Painter, t float64, back bool) {
	C, r, CY := s.C, s.r, shieldCY
	phi, sw := (t/shieldT)*(pi/shieldN), shieldSheen(t)
	clear(s.seen)
	s.dots = s.dots[:0]
	// gather the lines' pixels first, so each is drawn once
	dot := func(x, y, z float64) {
		if (z < 0) != back {
			return
		}
		key := math.Floor(x)*1000 + math.Floor(y)
		f, q := math.Abs(z)/r, (x-(y-CY))/(r*1.414)
		hot := math.Abs(q-sw) < 0.12
		i, ok := s.seen[key]
		if !ok {
			s.seen[key] = len(s.dots)
			s.dots = append(s.dots, shieldDot{x, y, f, hot})
			return
		}
		if f < s.dots[i].f {
			s.dots[i] = shieldDot{x, y, f, hot || s.dots[i].hot}
		}
	}
	// meridians, turning
	for k := range 2 * shieldN {
		lon := phi + (float64(k)*pi)/shieldN
		for lat := -pi / 2; lat <= pi/2; lat += 0.7 / r {
			dot(shieldOnSphere(r, lat, lon))
		}
	}
	// the equator
	for lon := 0.0; lon < tau; lon += 0.7 / r {
		dot(shieldOnSphere(r, 0, lon))
	}
	// behind: faint and broken. In front: bright where the surface turns away, a translucent wash of gold where it faces us, so the character still shows through
	for _, d := range s.dots {
		switch {
		case back:
			p.Blend(d.x, d.y, C[4], 0.32)
		case d.hot:
			p.Px(d.x, d.y, pixelfx.White)
		case d.f < 0.4:
			p.Px(d.x, d.y, C[6])
		case d.f < 0.7:
			p.Blend(d.x, d.y, C[6], 0.8)
		default:
			p.Blend(d.x, d.y, C[5], 0.55)
		}
	}
}

func (s *shieldInst) film(p *pixelfx.Painter, t float64, back bool) {
	C, r, CY, sw := s.C, s.r, shieldCY, shieldSheen(t)
	p.Each(-r-1, CY-r-1, r+1, CY+r+1, func(x, y float64, sx, sy int) {
		d := pixelfx.Hypot(x, y-CY) / r
		if d >= 1 {
			return
		}
		q := (x - (y - CY)) / (r * 1.414)
		hot := math.Max(0, 1-math.Abs(q-sw)/0.22)
		base, hk := 0.03, 1.0
		if back {
			base, hk = 0.07, 0.3
		}
		a := base + 0.26*math.Pow(d, 6) + 0.2*hot*hk
		c := C[5]
		if hot > 0.5 && !back {
			c = C[6]
		}
		p.BlendPx(sx, sy, c, a)
	})
}

func (s *shieldInst) DrawBack(p *pixelfx.Painter, t float64) {
	C, r, CY := s.C, s.r, shieldCY
	rf := math.Sqrt(r*r - CY*CY)
	s.film(p, t, true)
	s.lattice(p, t, true)
	// where it meets the floor: the far half of a bright seal
	oval(p, 0, 0, rf, rf*shieldFloorSY, C[5], 0.8, far)
	p.Glow(0, CY, r*1.1, C[4], 0.14)
}

func (s *shieldInst) Draw(p *pixelfx.Painter, t float64) {
	C, r, CY, W := s.C, s.r, shieldCY, pixelfx.White
	T := shieldT
	rf := math.Sqrt(r*r - CY*CY)
	// the bubble lights the character from all round
	if s.S != nil {
		for _, pt := range s.S.out {
			p.Light(pt[0], pt[1], C[5], 0.3)
		}
	}
	s.film(p, t, false)
	s.lattice(p, t, false)
	// the rim: bright where the light sits (top left) and where the sheen crosses it
	sw := shieldSheen(t)
	for j, n := 0, int(math.Ceil(r*tau*1.4)); j < n; j++ {
		g := (float64(j) / float64(n)) * tau
		x, y := math.Cos(g)*r, CY+math.Sin(g)*r
		q := (x - (y - CY)) / (r * 1.414)
		lit, hot := math.Cos(g+2.36), math.Abs(q-sw) < 0.15
		c := C[5]
		if hot || lit > 0.75 {
			c = W
		} else if lit > -0.2 {
			c = C[6]
		}
		p.Dot(x, y, c, 1)
		p.Dot(math.Cos(g)*(r-1), CY+math.Sin(g)*(r-1), C[4], 0.35+0.3*lit)
	}
	p.GlowRing(0, CY, r, 6, C[4], 0.32)
	// where the meridians cross the equator, and at the pole they all turn round, the lattice catches the light
	phi := (t / T) * (pi / shieldN)
	for k := range 2 * shieldN {
		x, y, z := shieldOnSphere(r, 0, phi+(float64(k)*pi)/shieldN)
		if z < r*0.15 {
			continue
		}
		p.Px(x, y, W)
		if z > r*0.45 {
			for _, d := range [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				p.Blend(x+d[0], y+d[1], C[6], 0.7)
			}
		}
		p.Glow(x, y, 4, C[5], 0.3)
	}
	px0, py0, _ := shieldOnSphere(r, pi/2, 0)
	pixelfx.Sparkle(p, px0, py0, 2+pixelfx.Round(1.5+1.5*math.Sin((t/T)*tau*2)), W, C[6], 0.45)
	// two glints chase each other round the rim
	for _, off := range [2]float64{0, pi} {
		g := (t/T)*tau + off + 0.8
		pixelfx.Sparkle(p, math.Cos(g)*r, CY+math.Sin(g)*r, 2, W, C[6], 0.4)
	}
	// a bubble's shine: a bright curl up on the left, a dot under it, a faint one low on the right
	for g := 3.55; g < 4.2; g += 0.04 {
		c := C[6]
		if g > 3.7 && g < 4.05 {
			c = W
		}
		p.Px(math.Cos(g)*r*0.74, CY+math.Sin(g)*r*0.74, c)
	}
	g34 := 3.4
	p.Px(math.Cos(g34)*r*0.62, CY+math.Sin(g34)*r*0.62, W)
	for g := 0.45; g < 0.8; g += 0.05 {
		p.Dot(math.Cos(g)*r*0.8, CY+math.Sin(g)*r*0.8, C[5], 0.6)
	}
	// the near half of the floor seal and a pool of light
	ovalFn(p, 0, 0, rf, rf*shieldFloorSY, func(g float64) (uint32, float64) {
		if math.Sin(g) > 0.6 {
			return W, 1
		}
		return C[6], 1
	}, near)
	for i := -2; i <= 2; i++ {
		p.Glow(float64(i)*rf*0.5, 0, 12, C[4], 0.18)
	}
	// sparkles winking on its skin as it turns, motes rising inside
	pixelfx.Cycle(t, T, 9, s.seed, func(g *pixelfx.Rng) float64 { return g.Range(0.35, 0.6) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		lat := R.Range(-1.2, 1.3)
		lon := R.Range(0, tau) + phi
		x, y, z := shieldOnSphere(r, lat, lon)
		k := math.Sin((age / life) * pi)
		if z < 0 {
			return
		}
		pixelfx.Sparkle(p, x, y, 1+2.5*k*R.Range(0.6, 1), W, C[6], 0.45*k)
	})
	pixelfx.Cycle(t, T, 12, s.seed+1, func(g *pixelfx.Rng) float64 { return g.Range(1.2, 2.2) }, func(age, life float64, R *pixelfx.Rng, _ int) {
		x0, u := R.Range(-0.7, 0.7)*r, age/life
		x, y := x0+math.Sin(u*6+R.Range(0, 6))*2, CY+r*0.75-u*r*1.4
		if pixelfx.Hypot(x, y-CY) > r-2 {
			return
		}
		c := C[5]
		if u < 0.5 {
			c = C[6]
		}
		p.Dot(x, y, c, math.Sin(u*pi))
		p.Light(x, y, C[5], 0.4*math.Sin(u*pi))
	})
}
