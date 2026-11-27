// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/lightning/tesla-coil.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The arcs (forking bolts re-struck every frame, inked with halo, body, core and light), their flicker and the zaps that strike the floor in a spray of sparks follow Sprite Fusion's open-source lightning and energy orb (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package lightning

import (
	"math"
	"sync"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// TeslaCoil is a copper coil under a steel torus throwing branching arcs that lash out in volleys and now and then strike the floor in a spray of sparks, lighting the metal and the ground. It is registered as "lightning/tesla-coil".
var TeslaCoil = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Tesla coil",
		Desc:     "A copper coil under a steel torus throws branching arcs that lash out in volleys and now and then strike the floor in a spray of sparks, lighting the metal and the ground.",
		Tags:     []string{"lightning", "electric", "tesla", "machine", "prop", "loop"},
		Size:     [2]int{224, 136},
		Origin:   [2]int{112, 120},
		Duration: 2.4,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: append(voltOptions(),
			pixelfx.Option{Name: "copper", Default: pixelfx.Palette("#2a120c", "#5a2814", "#8e4a1e", "#c4772f", "#eaa65a", "#ffd9a0")},
			pixelfx.Option{Name: "steel", Default: pixelfx.Palette("#0f121c", "#1f2433", "#353d52", "#556078", "#7f8ba3", "#b8c3d6", "#f2f6ff")},
			pixelfx.Option{Name: "seed", Default: pixelfx.Num(4)},
		),
	},
	New: newTeslaCoil,
}

func init() { pixelfx.Register("lightning/tesla-coil", TeslaCoil) }

const (
	tcFPS  = 30
	tcN    = 72
	tcP    = float64(tcN) / tcFPS
	tcCTop = -50
	tcCBot = -12
)

// The torus: its height, ring radius, tube radius and squash, variables so sums of them round as JavaScript's do.
var tcTY, tcRR, tcTR, tcSY = -57.0, 12.0, 3.6, 0.45

var (
	tcIron = pixelfx.Ramp("#120f18", "#1e1a27", "#2c2637", "#3f374d", "#5a5068")
	tcAsh  = pixelfx.Hex("#5b5566")
)

// tcFL is an arc's brightness over the frames it lives: struck, flickered, re-struck, gone.
var tcFL = [...]float64{1, 0.7, 0.95, 0.55, 0.85, 0.45, 0.7, 0.35}

var tcZapK = [6]float64{1, 0.75, 1, 0.6, 0.85, 0.4}

type tcArc struct {
	f0, len          int
	side             float64
	phi, th0, th1, L float64
	front            bool
}

type tcStrike struct {
	f0      int
	side, x float64
	phi     float64
}

// tcPlan returns the loop's events, frame by frame: volleys of arcs off the torus, and two zaps to the floor.
func tcPlan(seed float64) ([]tcArc, []tcStrike) {
	r := pixelfx.RngOf(seed, 1)
	var arcs []tcArc
	for _, v := range [6][2]int{{2, 3}, {20, 3}, {30, 1}, {38, 3}, {56, 3}, {66, 1}} {
		// pixelfx draws a number on every test of the loop
		for j := 0; float64(j) < float64(v[1])+r.Int(0, 1); j++ {
			side := -1.0
			if r.Chance(0.5) {
				side = 1
			}
			front, a := r.Chance(0.55), r.Range(0.15, 1.1)
			var phi float64
			switch {
			case side > 0 && front:
				phi = a
			case side > 0:
				phi = -a
			case front:
				phi = pi - a
			default:
				phi = pi + a
			}
			th0 := r.Range(-1.15, 0.35)
			f0 := (v[0] + j*int(r.Int(2, 4))) % tcN
			n := int(r.Int(3, 7))
			th1 := th0 + r.Range(-0.6, 0.6)
			arcs = append(arcs, tcArc{f0: f0, len: n, side: side, phi: phi, front: front, th0: th0, th1: th1, L: r.Range(24, 52)})
		}
	}
	strikes := make([]tcStrike, 0, 2)
	for _, s := range [2]struct {
		f0   int
		side float64
	}{{14, 1}, {47, -1}} {
		phi := pi - 0.4
		if s.side > 0 {
			phi = 0.4
		}
		strikes = append(strikes, tcStrike{f0: s.f0, side: s.side, x: s.side * r.Range(38, 56), phi: phi})
	}
	return arcs, strikes
}

func tcAgeOf(f, f0 int) int { return ((f-f0)%tcN + tcN) % tcN }

// tcRoot returns where an arc at angle phi round the torus roots, lifted by up.
func tcRoot(phi, up float64) (float64, float64) {
	return math.Cos(phi) * (tcRR + tcTR*0.6), tcTY + math.Sin(phi)*tcRR*tcSY - up
}

func tcSide(s float64) int {
	if s > 0 {
		return 1
	}
	return 0
}

// tcCoil draws the coil: an iron plinth, two thick primary turns, the secondary wound in copper wire, catching arc light.
func tcCoil(p *pixelfx.Painter, Cu []uint32, lit [2]float64, C []uint32) {
	n, I := len(Cu), tcIron
	at := func(l float64, d int) uint32 {
		return Cu[int(pixelfx.ClampTo(pixelfx.Round(l*float64(n-1))-float64(d), 0, float64(n-1)))]
	}
	p.Shade(-14, -8, 14, 0, func(x, y float64, o *pixelfx.Out) uint32 {
		if x < -13 || x > 13 || y < -7 {
			return 0
		}
		edge := 1
		if x < -12 {
			edge = 2
		} else if x > 12 {
			edge = 0
		}
		switch {
		case y < -6:
			o.C = I[4]
		case y < -5:
			o.C = I[3]
		case y > -1:
			o.C = I[0]
		case edge+1 > 2:
			o.C = I[2]
		case y < -3:
			o.C = I[edge+1]
		default:
			o.C = I[edge]
		}
		return 0
	})
	for _, rx := range [2]float64{-10, 10} {
		p.Px(rx, -4, I[4])
		p.Px(rx, -3, I[0])
	}
	turn := func(y float64, front bool) {
		for a := 0.0; a < tau; a += 0.04 {
			if (math.Sin(a) >= 0) != front {
				continue
			}
			x, yy := math.Cos(a)*10, y+math.Sin(a)*2.2
			off := -0.2
			if front {
				off = 0.25
			}
			l := 0.5 - 0.4*math.Cos(a) + off
			p.Px(x, yy, at(l, 0))
			p.Px(x, yy+1, at(l, 2))
		}
	}
	turn(-10.5, false)
	turn(-13.5, false)
	// the secondary: wire on every other row, lit from the left, a bright band down the front, arc light on the rims
	p.Shade(-6, tcCTop, 6, tcCBot, func(x, y float64, o *pixelfx.Out) uint32 {
		if x < -5 || x > 5 {
			return 0
		}
		u := (x + 5.5) / 11
		nx := u*2 - 1
		wire := int(math.Floor(y))&1 == 0
		band := 0.0
		if math.Abs(nx+0.35) < 0.12 {
			band = 0.3
		}
		l := 0.62 - 0.55*nx - 0.35*nx*nx + band
		if !wire {
			l -= 0.32
		}
		o.C = at(l, 0)
		if wire && ((nx < -0.75 && lit[0] > 0.25) || (nx > 0.75 && lit[1] > 0.25)) {
			k := lit[1]
			if nx < 0 {
				k = lit[0]
			}
			o.C = C[4]
			if k > 0.6 {
				o.C = C[6]
			}
		}
		return 0
	})
	p.Line(-5, tcCTop-1, 5, tcCTop-1, I[3], 1)
	p.Line(-4, tcCTop-2, 4, tcCTop-2, I[2], 1)
	turn(-10.5, true)
	turn(-13.5, true)
}

// tcTorusMap is the torus, solved once: for every pixel, the normal of the nearest point of a ring tube seen from a little above, in float32 as pixelfx's typed arrays hold it.
type tcTorusMap struct {
	X0, Y0, W, H int
	z, n         []float32
	ca, sa       float64
}

var (
	tcTorusOnce sync.Once
	tcTorus     *tcTorusMap
)

func tcTorusOf() *tcTorusMap {
	tcTorusOnce.Do(func() {
		RR, TR, TY := tcRR, tcTR, tcTY
		sa, ca := tcSY, math.Sqrt(1-tcSY*tcSY)
		X0 := int(-math.Ceil(RR+TR) - 1)
		Y0 := int(math.Floor(TY-RR*sa-TR) - 2)
		W := 2*-X0 + 2
		H := int(math.Ceil(TY+RR*sa+TR)+3) - Y0
		T := &tcTorusMap{X0: X0, Y0: Y0, W: W, H: H, z: make([]float32, W*H), n: make([]float32, W*H*3), ca: ca, sa: sa}
		for i := range T.z {
			T.z[i] = -1e9
		}
		for i := range 480 {
			phi := (float64(i) / 480) * tau
			cp, sp := math.Cos(phi), math.Sin(phi)
			for j := range 96 {
				psi := (float64(j) / 96) * tau
				cs, ss := math.Cos(psi), math.Sin(psi)
				rr := RR + TR*cs
				X, Y, Z := rr*cp, TR*ss, rr*sp
				px, py := int(math.Floor(X))-X0, int(math.Floor(TY-Y*ca+Z*sa))-Y0
				d := Z*ca + Y*sa
				if px < 0 || py < 0 || px >= W || py >= H {
					continue
				}
				k := py*W + px
				if d > float64(T.z[k]) {
					T.z[k] = float32(d)
					T.n[k*3], T.n[k*3+1], T.n[k*3+2] = float32(cs*cp), float32(ss), float32(cs*sp)
				}
			}
		}
		tcTorus = T
	})
	return tcTorus
}

func tcNorm(x, y, z float64) [3]float64 {
	l := pixelfx.Hypot3(x, y, z)
	return [3]float64{x / l, y / l, z / l}
}

var tcKey = tcNorm(-0.55, 0.75, 0.45)

// tcDrawTorus draws the steel shaded by a key light from the upper left, a hard specular, dark inner faces, the arcs' light on the sides.
func tcDrawTorus(p *pixelfx.Painter, St, C []uint32, lit [2]float64) {
	T, m, K := tcTorusOf(), len(St), tcKey
	H := tcNorm(K[0], K[1]+T.sa, K[2]+T.ca)
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < T.W && y < T.H && float64(T.z[y*T.W+x]) > -1e8 }
	for y := range T.H {
		for x := range T.W {
			k := y*T.W + x
			if float64(T.z[k]) < -1e8 {
				continue
			}
			nx, ny, nz := float64(T.n[k*3]), float64(T.n[k*3+1]), float64(T.n[k*3+2])
			dif := math.Max(0, nx*K[0]+ny*K[1]+nz*K[2])
			spec := math.Pow(math.Max(0, nx*H[0]+ny*H[1]+nz*H[2]), 28)
			lx := float64(x+T.X0) + 0.5
			inner := 1.0
			if nx*lx+nz*0 < 0 && math.Abs(lx) < tcRR {
				inner = 0.75
			}
			var c uint32
			if spec > 0.55 {
				c = St[m-1]
			} else {
				c = St[int(pixelfx.ClampTo(math.Floor((0.12+0.8*dif*inner+spec)*(float64(m)-1.01)), 0, float64(m-2)))]
			}
			for _, sl := range [2][2]float64{{-1, lit[0]}, {1, lit[1]}} {
				r := math.Max(0, sl[0]*nx*0.85+ny*0.25+nz*0.45) * math.Min(1, sl[1])
				if r > 0.88 {
					c = C[6]
				} else if r > 0.62 {
					c = pixelfx.Mix(c, C[5], 0.6)
				} else if r > 0.38 {
					c = pixelfx.Mix(c, C[4], 0.3)
				}
			}
			if !on(x, y+1) && ny < 0.3 {
				c = St[0]
			}
			p.Px(float64(x+T.X0), float64(y+T.Y0), c)
		}
	}
}

func newTeslaCoil(o *pixelfx.Options) pixelfx.Instance {
	C, Cu, St, S, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("copper"), 6), pixelfx.Fit(o.Ramp("steel"), 7), o.Ramp("sparks"), o.Seed()
	arcs, strikes := tcPlan(seed)
	var live []tcArc
	return &pixelfx.Funcs{DrawFunc: func(p *pixelfx.Painter, t float64) {
		f := frameOf(t, tcFPS, tcN)
		ff := float64(f)
		var lit [2]float64
		live = live[:0]
		for _, a := range arcs {
			if tcAgeOf(f, a.f0) < a.len {
				live = append(live, a)
			}
		}
		for _, a := range live {
			lit[tcSide(a.side)] += tcFL[tcAgeOf(f, a.f0)] * 0.6
		}
		for _, s := range strikes {
			if tcAgeOf(f, s.f0) < 6 {
				lit[tcSide(s.side)]++
			}
		}
		act := math.Min(1, (lit[0]+lit[1])*0.5)

		// the floor: a cold pool of light under the coil, scorches and smoke where zaps landed
		p.Glow(0, -10, 58, C[3], 0.14+0.25*act)
		p.Ellipse(0, 0.5, 15, 1.6, tcIron[0], 1)
		for _, s := range strikes {
			age := pixelfx.Wrap(t-float64(s.f0)/tcFPS, tcP)
			scorch(p, s.x, 0, 7, math.Pow(pixelfx.Clamp(1-age/1.1), 1.3), 1-pixelfx.Clamp((age-1.4)/0.6))
			for j := range 2 {
				fj := float64(j)
				u := (age - 0.12 - fj*0.18) / 1.2
				if u > 0 && u < 1 {
					dx := -2.0
					if j != 0 {
						dx = 3
					}
					puff(p, s.x+dx+u*5*s.side, -3-u*16-fj*2, 2.6+fj, u, pixelfx.Mix(tcAsh, C[2], 0.3))
				}
			}
		}

		drawArc := func(a tcArc) {
			age := tcAgeOf(f, a.f0)
			u := float64(age) / math.Max(1, float64(a.len-1))
			th := a.th0 + (a.th1-a.th0)*pixelfx.Ease.OutQuad(u)
			ang := th
			if a.side <= 0 {
				ang = pi - th
			}
			x0, y0 := tcRoot(a.phi, 0)
			L := a.L
			if age == 0 {
				L = a.L * 0.75
			}
			r := pixelfx.RngOf(seed, 21, float64(a.f0), ff)
			stroke(p, zigzag(r, x0, y0, x0+math.Cos(ang)*L, y0+math.Sin(ang)*L, 0.32, 0.75), tcFL[age], C, 0.6, strokeOpts{light: pixelfx.Val(0.4)})
			p.Glow(x0, y0, 10, C[5], 0.5*tcFL[age])
		}
		// arcs off the back of the torus go behind the coil
		for _, a := range live {
			if !a.front {
				drawArc(a)
			}
		}
		tcCoil(p, Cu, lit, C)
		tcDrawTorus(p, St, C, lit)
		// corona: a faint violet fuzz crawling over the torus
		rc := pixelfx.RngOf(seed, 9, ff)
		for range 6 {
			phi, d := rc.Range(0, tau), rc.Range(1, 3)
			x, y := math.Cos(phi)*(tcRR+tcTR+d), tcTY+math.Sin(phi)*(tcRR*tcSY+tcTR+d*0.7)
			c := C[3]
			if rc.Chance(0.3) {
				c = C[5]
			}
			p.Px(x, y, c)
			if rc.Chance(0.4) {
				p.Px(x+math.Cos(phi), y+math.Sin(phi)*0.6, C[2])
			}
			p.Glow(x, y, 4, C[3], 0.3)
		}
		// brush discharge: short streamers hiss off the rim every frame
		for range 2 {
			phi := rc.Range(0, tau)
			x0, y0 := math.Cos(phi)*(tcRR+tcTR*0.8), tcTY+math.Sin(phi)*(tcRR*tcSY+tcTR*0.6)
			a := math.Atan2(math.Sin(phi)*0.6-0.5, math.Cos(phi)) + rc.Range(-0.5, 0.5)
			l := rc.Range(4, 10)
			crackle(p, zigzag(rc, x0, y0, x0+math.Cos(a)*l, y0+math.Sin(a)*l, 0.4, 0.4), rc.Range(0.35, 0.65), C, 0.3, 1)
		}
		p.Glow(0, tcTY, 34, C[3], 0.18+0.3*act)
		for _, a := range live {
			if a.front {
				drawArc(a)
			}
		}

		// zaps to the floor: a full bolt, a flash where it lands, sparks thrown and bouncing
		for _, s := range strikes {
			if age := tcAgeOf(f, s.f0); age < 6 {
				k := tcZapK[age]
				x0, y0 := tcRoot(s.phi, -2)
				r := pixelfx.RngOf(seed, 41, float64(s.f0), ff)
				sc := 0.8
				if age == 0 {
					sc = 0.95
				}
				stroke(p, zigzag(r, x0, y0, s.x, 0, 0.22, 0.55), k, C, sc, strokeOpts{hero: age == 0})
				p.Circle(s.x, -1, 3*k+1.5, C[2], 1)
				p.Circle(s.x, -1, 3*k+0.5, C[5], 1)
				p.Circle(s.x, -1, 2*k, pixelfx.White, 1)
				p.Glow(s.x, -10, 40, C[4], 0.55*k)
				p.Glow(s.x, -3, 12, C[6], 0.5*k)
				if age < 2 {
					p.Glow(0, -50, 120, C[3], 0.25*k)
				}
			}
			st := pixelfx.Wrap(t-float64(s.f0)/tcFPS, tcP)
			for j := range 18 {
				r := pixelfx.RngOf(seed, 51, float64(s.f0), float64(j))
				life := r.Range(0.3, 0.65)
				if st >= life {
					continue
				}
				a, sp := -pi/2+r.Range(-1.3, 1.3), r.Range(50, 190)
				x, y, vx, vy := flight(s.x, -1, math.Cos(a)*sp, math.Sin(a)*sp, st, 0)
				spark(p, x, y, vx, vy, 1-st/life, S)
			}
		}
		// sparks spat off the arc tips as they strike
		for _, a := range arcs {
			age := pixelfx.Wrap(t-float64(a.f0)/tcFPS, tcP)
			ang := a.th0
			if a.side <= 0 {
				ang = pi - a.th0
			}
			x0, y0 := tcRoot(a.phi, 0)
			for j := range 3 {
				r := pixelfx.RngOf(seed, 61, float64(a.f0), float64(j))
				life := r.Range(0.15, 0.35)
				if age >= life {
					continue
				}
				tx, ty := x0+math.Cos(ang)*a.L*0.75, y0+math.Sin(ang)*a.L*0.75
				b, sp := ang+r.Range(-0.9, 0.9), r.Range(50, 150)
				x, y, vx, vy := flight(tx, ty, math.Cos(b)*sp, math.Sin(b)*sp, age, 0)
				spark(p, x, y, vx, vy, 1-age/life, S)
			}
		}
	}}
}
