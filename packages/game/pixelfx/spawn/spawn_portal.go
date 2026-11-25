// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/spawn-portal.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the streaks pulled in, the rings and lights at the burst and the collapse pop from Sprite Fusion's open-source black-hole (https://www.spritefusion.com/pixel-art-vfx/black-hole), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// SpawnPortal tears a rift open on the floor and swirls, belches violet smoke and sparks, spits a fountain of wisps up out of itself, then twists shut with a pop. It is registered as "spawn/spawn-portal".
var SpawnPortal = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Spawn portal",
		Desc:     "A rift tears open on the floor and swirls, belches violet smoke and sparks, spits a fountain of wisps up out of itself, then twists shut with a pop.",
		Tags:     []string{"spawn", "portal", "summon", "rift", "arcane", "smoke", "ground", "one-shot"},
		Size:     [2]int{208, 150},
		Origin:   [2]int{104, 122},
		Duration: 3.8,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#12081f", "#2a0f4d", "#481b8c", "#7330d0", "#a35cff", "#cf9bff", "#f1dcff", "#ffffff")},
			{Name: "width", Default: pixelfx.Num(28)},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	New: newSpawnPortal,
}

func init() { pixelfx.Register("spawn/spawn-portal", SpawnPortal) }

var (
	portalOpen  = 0.3
	portalBelch = [2]float64{0.52, 0.76}
	portalBurst = 1.0
	portalShut  = 1.9
	portalPop   = 2.22
	portalSY    = 0.34
	portalVoid  = pixelfx.Hex("#06030b")
	portalFume  = pixelfx.Hex("#3a3046")
	portalStone = pixelfx.Ramp("#1a1424", "#2b2639", "#4a4263", "#6c6488", "#9a91b4")
	portalDust  = pixelfx.Hex("#5a5070")
)

// the rift's size over its life: a tear that snaps open past round, breathes, swells as it shuts, then collapses
func portalRadius(t, RX float64) float64 {
	if t < portalOpen {
		return RX * pixelfx.OutBack(pixelfx.Clamp(t/portalOpen), 2.2)
	}
	if t < portalShut {
		d := t - portalBurst - 0.05
		return RX * (1 + 0.035*math.Sin(t*9) + 0.1*math.Exp(-(d*d)/0.006))
	}
	k := pixelfx.Clamp((t - portalShut) / (portalPop - portalShut))
	if k < 0.35 {
		return RX * (1 + 0.1*math.Sin((k/0.35)*pi*0.5))
	}
	return RX * (1.1 * (1 - math.Pow((k-0.35)/0.65, 1.6)))
}

// how far the swirl has turned: lazy, then whipping round as it shuts
func portalSpinAt(t float64) float64 {
	return t*2.4 + 1.5*pixelfx.Smooth(portalBurst-0.1, portalBurst+0.3, t) + 10*math.Pow(pixelfx.Clamp((t-portalShut)/(portalPop-portalShut)), 2.2)
}

func portalLipAt(th, spin float64) float64 {
	return 1 + 0.05*pixelfx.Noise(th*3+spin*0.6, 5) + 0.035*pixelfx.Noise(th*8-spin*1.3, 9)
}

// the hole: a void, a lit far wall, three arms of light spiralling down into it
func portalHole(p *pixelfx.Painter, C []uint32, rx, spin, heat, wind float64) {
	if rx < 1 {
		return
	}
	ry := rx * portalSY
	p.Shade(-rx-2, -ry-2, rx+2, ry+2, func(x, y float64, out *pixelfx.Out) uint32 {
		u, v := x/rx, y/ry
		q, th := pixelfx.Hypot(u, v), math.Atan2(v, u)
		lip := portalLipAt(th, spin)
		if q > lip {
			return 0
		}
		w := q / lip
		// the far inner wall catches the glow from below; the near one is hidden
		switch {
		case v < 0 && w > 0.72:
			if w > 0.86 {
				out.C = C[2]
			} else {
				out.C = C[1]
			}
		case w > 0.55:
			out.C = C[0]
		default:
			out.C = portalVoid
		}
		return 0
	})
	n := int(math.Max(40, pixelfx.Round(rx*5)))
	for i := range 3 {
		for j := 0; j <= n; j++ {
			q := 0.14 + 0.82*(float64(j)/float64(n))
			th := spin + (float64(i)*tau)/3 - wind*math.Log(q)
			lip := portalLipAt(th, spin)
			x, y := math.Cos(th)*q*lip*rx, math.Sin(th)*q*lip*ry
			var c uint32
			switch {
			case q > 0.8:
				if heat > 1.1 {
					c = pixelfx.White
				} else {
					c = C[6]
				}
			case q > 0.55:
				c = C[5]
			case q > 0.32:
				c = C[4]
			default:
				c = C[3]
			}
			a := 1.0
			if q <= 0.3 {
				a = q / 0.3
			}
			p.Dot(x, y, c, a)
			// a fainter strand riding just inside each arm
			if q > 0.4 && j%2 == 0 {
				t2 := th - 0.32
				x2, y2 := math.Cos(t2)*q*0.94*lip*rx, math.Sin(t2)*q*0.94*lip*ry
				c2 := C[3]
				if q > 0.7 {
					c2 = C[4]
				}
				p.Dot(x2, y2, c2, 0.85)
			}
		}
	}
	p.Glow(0, 0, rx*0.45, C[3], 0.35*heat)
}

// the lip: a ragged ring of white-hot light, a dark cut outside it; far half behind, near half in front
func portalLip(p *pixelfx.Painter, C []uint32, rx, spin, heat float64, near bool) {
	if rx < 1 {
		return
	}
	ry, n := rx*portalSY, int(math.Ceil(rx*7))
	for j := 0; j < n; j++ {
		th := (float64(j) / float64(n)) * tau
		s := math.Sin(th)
		if (s >= 0) != near {
			continue
		}
		l := portalLipAt(th, spin)
		x, y := math.Cos(th)*rx*l, s*ry*l
		f := pixelfx.Noise(th*9+spin*3, 13)
		p.Dot(math.Cos(th)*(rx*l+1.2), s*(ry*l+1), C[1], 1)
		var c uint32
		if near {
			if f > -0.3 || heat > 1.1 {
				c = pixelfx.White
			} else {
				c = C[6]
			}
		} else if f > 0.2 {
			c = C[6]
		} else {
			c = C[5]
		}
		p.Dot(x, y, c, 1)
		if j%6 == 0 {
			gk := 0.2
			if near {
				gk = 0.3
			}
			p.Glow(x, y, 7, C[5], gk*math.Min(1.4, heat))
		}
	}
}

type portalSwirl struct {
	a, r, w, in float64
	c           uint32
}

type portalWisp struct {
	x, y, vx, vy, life, max float64
	tr                      [][2]float64
	acc, ph                 float64
}

type portalRing struct {
	t0, life, r0, r1, th float64
	hot                  bool
}

// a shock ring on the floor plane: far half behind, near half in front
func drawPortalRing(p *pixelfx.Painter, C []uint32, r portalRing, t float64, near bool) {
	k := (t - r.t0) / r.life
	if k < 0 || k >= 1 {
		return
	}
	pixelfx.Band(p, 0, 0, r.r0+(r.r1-r.r0)*pixelfx.Ease.OutCubic(k), portalSY*0.62, math.Max(1, r.th*(1-k)), 1.15-k, C, func(g float64) bool { return (math.Sin(g) >= 0) == near }, r.hot && k < 0.15)
}

func newSpawnPortal(o *pixelfx.Options) pixelfx.Instance {
	C, RX, R := pixelfx.Fit(o.Palette(), 8), o.Num("width"), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	// violet smoke, dulled toward a smoky grey so it reads as smoke and not as glass
	SM := []uint32{pixelfx.Mix(C[4], portalFume, 0.4), pixelfx.Mix(C[3], portalFume, 0.3), pixelfx.Mix(C[5], portalFume, 0.55)}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[3]}})
	rocks, lights := pixelfx.NewBits(floor), &pixelfx.Lights{}
	var swirl []portalSwirl
	var wisps []portalWisp
	var rings []portalRing
	done := map[string]bool{}
	t := 0.0

	swirlCols := []uint32{pixelfx.White, C[6], C[6], C[5]}
	pullCols := []uint32{pixelfx.White, C[6], C[5], C[5], C[4]}
	burstStarCols := []uint32{pixelfx.White, C[6], C[5]}
	popStarCols := []uint32{pixelfx.White, C[6]}
	floorStarCols := []uint32{C[6], C[5], C[5]}

	// a mote riding the swirl on the floor plane, spiralling in
	addSwirl := func(r0 float64) {
		a, w, in, c := R.Range(0, tau), R.Range(2.2, 4), R.Range(0.3, 0.7), pixelfx.Pick(R, swirlCols)
		swirl = append(swirl, portalSwirl{a: a, r: r0, w: w, in: in, c: c})
	}
	// a streak of light sucked in from the air round the rift
	pull := func() {
		a, D, T := R.Range(pi*1.03, pi*1.97), R.Range(40, 86), R.Range(0.24, 0.42)
		x, y := math.Cos(a)*D*1.08, math.Sin(a)*D*0.85
		l := pixelfx.Hypot(x, y)
		v := (0.6 * l) / (1 - math.Exp(-0.6*T))
		c, size := pixelfx.Pick(R, pullCols), R.Range(1, 1.7)
		ps.Spawn(pixelfx.Mote, x, y, (-x/l)*v, (-y/l)*v, T, c, size, 0, 0)
	}
	puff := func(n int, big bool, sp float64) {
		for range n {
			a, d := R.Range(0, tau), R.Range(0, 0.55)*RX
			vx := R.Range(-1, 1) * sp * 0.6
			vy := -R.Range(0.4, 1) * sp
			lifeMul, sizeMul := 0.8, 0.9
			if big {
				lifeMul, sizeMul = 1.15, 1.15
			}
			life := R.Range(1.2, 2.2) * lifeMul
			col := pixelfx.Pick(R, SM)
			size := R.Range(2.8, 4.6) * sizeMul
			var delay float64
			var flag pixelfx.Flag
			if big {
				delay = R.Range(0, 0.3)
				flag = pixelfx.Big
			} else {
				delay = R.Range(0, 0.1)
			}
			ps.Spawn(pixelfx.Smoke, math.Cos(a)*d, math.Sin(a)*d*portalSY-2, vx, vy, life, col, size, delay, flag)
		}
	}
	sparks := func(n int, up, spread float64) {
		for range n {
			a, sp := -pi/2+R.Range(-spread, spread), R.Range(0.5, 1)*up
			x, life := R.Range(-0.5, 0.5)*RX, R.Range(0.35, 0.8)
			ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
		}
	}
	once := func(k string, at float64) bool {
		if !done[k] && t >= at {
			done[k] = true
			return true
		}
		return false
	}

	drawSwirl := func(p *pixelfx.Painter, near bool) {
		rx := portalRadius(t, RX)
		for _, s := range swirl {
			for j := range 3 {
				a := s.a - float64(j)*0.08*(1.4/math.Max(0.3, s.r))
				x, y := math.Cos(a)*s.r*rx, math.Sin(a)*s.r*rx*portalSY
				if (math.Sin(a) >= 0) != near {
					continue
				}
				c, al := s.c, 1.0
				if j != 0 {
					c, al = C[5], 0.9-float64(j)*0.3
				}
				p.Dot(x, y-0.5, c, al)
				if j == 0 {
					p.Glow(x, y, 3, C[5], 0.35)
				}
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the tear pulls dust and light in while it opens, while it's open, and hard while it shuts
			shutting := t > portalShut && t < portalPop
			if (t < portalOpen+0.15 || shutting) && R.Chance(0.55) {
				s, D, T := R.Sign(), R.Range(RX*1.3, RX*2.4), R.Range(0.18, 0.3)
				v := (D * 3) / (math.Exp(3*T) - 1)
				y, vy := -R.Range(0, 3), R.Range(-2, 2)
				col := C[5]
				if R.Chance(0.5) {
					col = C[6]
				}
				ps.Spawn(pixelfx.Streak, s*D, y, -s*v, vy, T, col, 1, 0, 0)
			}
			pullProb := 0.1
			if shutting {
				pullProb = 0.85
			} else if t < portalBurst {
				pullProb = 0.55
			}
			if t > 0.1 && t < portalPop-0.1 && R.Chance(pullProb) {
				pull()
			}
			swirlProb := 0.25
			if shutting {
				swirlProb = 0.5
			}
			if t < portalPop && t > 0.12 && R.Chance(swirlProb) {
				addSwirl(R.Range(1.1, 1.5))
			}
			if once("open", portalOpen*0.7) {
				lights.Add(0, -8, 50, C[4], 0.75, 0.45)
				rings = append(rings, portalRing{t0: t, life: 0.42, r0: RX * 0.8, r1: RX * 1.9, th: 2.5, hot: true})
				for range 8 {
					x := R.Sign() * R.Range(0.7, 1.1) * RX
					vx := R.Sign() * R.Range(10, 45)
					vy := -R.Range(4, 14)
					life, size := R.Range(0.4, 0.7), R.Range(1.5, 3)
					ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, portalDust, size, 0, 0)
				}
			}
			// two small belches before the big one: a burp of smoke and a few sparks
			if once("b0", portalBelch[0]) {
				puff(6, false, 34)
				sparks(8, 210, 0.55)
				lights.Add(0, -6, 44, C[4], 0.6, 0.3)
			}
			if once("b1", portalBelch[1]) {
				puff(8, false, 40)
				sparks(12, 240, 0.6)
				lights.Add(0, -6, 48, C[4], 0.65, 0.3)
			}
			// the burst: something comes up through it
			if once("burst", portalBurst) {
				lights.Add(0, -26, 74, C[4], 1, 0.75)
				lights.Add(0, -10, 40, pixelfx.White, 0.85, 0.14)
				rings = append(rings,
					portalRing{t0: t, life: 0.55, r0: RX * 1.0, r1: RX * 2.5, th: 3.5, hot: true},
					portalRing{t0: t + 0.06, life: 0.45, r0: RX * 0.8, r1: RX * 1.8, th: 2, hot: false},
				)
				puff(18, true, 70)
				sparks(34, 400, 0.8)
				for i := range 5 {
					a, sp := -pi/2+float64(i-2)*0.27+R.Range(-0.08, 0.08), R.Range(185, 235)
					x, life, ph := R.Range(-4, 4), R.Range(0.75, 1.0), R.Range(0, tau)
					wisps = append(wisps, portalWisp{x: x, y: -2, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp, life: life, max: 1, ph: ph})
				}
				for range 12 {
					x := R.Sign() * R.Range(0.75, 1.05) * RX
					vx, vy := R.Range(-100, 100), -R.Range(150, 280)
					life, ang, va, s := R.Range(1.3, 1.9), R.Range(0, tau), R.Range(-14, 14), R.Range(1, 2.4)
					rocks.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: -1, VX: vx, VY: vy, G: 1, Drag: 0.4, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, Ramp: portalStone})
				}
				for range 14 {
					x, y, vx, vy := R.Range(-0.8, 0.8)*RX, -R.Range(4, 34), R.Range(-30, 30), -R.Range(10, 60)
					life, col, size := R.Range(0.5, 1.1), pixelfx.Pick(R, burstStarCols), R.Range(0.8, 2)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
				}
			}
			// while it's open it keeps venting thin smoke and the odd spark
			if t > portalOpen && t < portalShut && R.Chance(0.16) {
				puff(1, false, 22)
			}
			if t > portalOpen && t < portalShut+0.1 && R.Chance(0.06) {
				sparks(1, 160, 0.4)
			}
			// the pop when it closes
			if once("pop", portalPop) {
				lights.Add(0, -4, 50, C[4], 0.95, 0.5)
				lights.Add(0, -2, 28, pixelfx.White, 0.9, 0.12)
				rings = append(rings, portalRing{t0: t, life: 0.35, r0: 2, r1: RX * 1.9, th: 2.5, hot: true})
				for range 18 {
					a, sp, life := R.Range(-pi, 0), R.Range(90, 270), R.Range(0.25, 0.6)
					ps.Spawn(pixelfx.Spark, 0, -1, math.Cos(a)*sp, math.Sin(a)*sp*0.8, life, pixelfx.White, 1, 0, 0)
				}
				for range 9 {
					a, sp, life, col, size := R.Range(0, tau), R.Range(30, 95), R.Range(0.4, 0.9), pixelfx.Pick(R, popStarCols), R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, 0, -2, math.Cos(a)*sp, math.Sin(a)*sp*0.5-20, life, col, size, 0, 0)
				}
				for range 6 {
					x, y, vx, vy, life, size := R.Range(-6, 6), -R.Range(1, 4), R.Range(-34, 34), -R.Range(10, 30), R.Range(0.8, 1.3), R.Range(2.5, 4.5)
					ps.Spawn(pixelfx.Smoke, x, y, vx, vy, life, SM[2], size, 0, 0)
				}
				// what's left on the floor: specks of light that twinkle out where the rift was
				for range 14 {
					x, y, vx, vy := R.Range(-1, 1)*RX*0.9, -R.Range(0.5, 2), R.Range(-4, 4), -R.Range(0, 4)
					life, col, size, delay := R.Range(0.6, 1.4), pixelfx.Pick(R, floorStarCols), R.Range(0.4, 0.9), R.Range(0, 0.25)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, delay, 0)
				}
			}
			for i := len(swirl) - 1; i >= 0; i-- {
				s := &swirl[i]
				k := 1.0
				if shutting {
					k = 2.4
				}
				s.a += s.w * k * dt * (1.4 / math.Max(0.3, s.r))
				s.r -= s.in * k * dt
				if s.r < 0.25 || t > portalPop {
					swirl = append(swirl[:i], swirl[i+1:]...)
				}
			}
			// the wisps: comets with smoky tails that arc up and out, then burst
			for i := len(wisps) - 1; i >= 0; i-- {
				w := &wisps[i]
				w.tr = append([][2]float64{{w.x, w.y}}, w.tr...)
				if len(w.tr) > 12 {
					w.tr = w.tr[:12]
				}
				// a wisp bursts when it runs out or comes down on the floor
				w.life -= dt
				if w.life <= 0 || (w.vy > 0 && w.y > -2) {
					for range 8 {
						a, sp, life := R.Range(0, tau), R.Range(30, 120), R.Range(0.2, 0.5)
						ps.Spawn(pixelfx.Spark, w.x, w.y, math.Cos(a)*sp+w.vx*0.2, math.Sin(a)*sp+w.vy*0.2, life, pixelfx.White, 1, 0, 0)
					}
					ps.Spawn(pixelfx.StarKind, w.x, w.y, 0, -6, 0.45, pixelfx.White, 2.4, 0, 0)
					lights.Add(w.x, w.y, 24, C[5], 0.65, 0.25)
					wisps = append(wisps[:i], wisps[i+1:]...)
					continue
				}
				w.vy += 360 * dt
				w.vx += math.Sin((w.max-w.life)*16+w.ph) * 160 * dt
				w.vx -= w.vx * 1.1 * dt
				w.vy -= w.vy * 1.1 * dt
				w.x += w.vx * dt
				w.y += w.vy * dt
				for w.acc += dt; w.acc > 0.03; w.acc -= 0.03 {
					life, size := R.Range(0.6, 1), R.Range(1.5, 2.4)
					ps.Spawn(pixelfx.Smoke, w.x, w.y, w.vx*0.05, w.vy*0.05-4, life, SM[0], size, 0, 0)
				}
				if R.Chance(0.3) {
					vx, vy, life := -w.vx*0.2+R.Range(-15, 15), -w.vy*0.2+R.Range(-15, 15), R.Range(0.2, 0.4)
					ps.Spawn(pixelfx.Mote, w.x, w.y, vx, vy, life, C[6], 0.6, 0, 0)
				}
			}
			for i := len(rings) - 1; i >= 0; i-- {
				if t-rings[i].t0 > rings[i].life {
					rings = append(rings[:i], rings[i+1:]...)
				}
			}
			rocks.Update(dt)
			for i := range rocks.List {
				b := &rocks.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					for range 2 {
						dx, vx, vy, life, size := R.Range(-1.5, 1.5), R.Sign()*R.Range(6, 30), -R.Range(2, 12), R.Range(0.3, 0.6), R.Range(1.2, 2.4)
						ps.Spawn(pixelfx.Dust, b.X+dx, -0.5, vx, vy, life, portalDust, size, 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			rx, spin := portalRadius(t, RX), portalSpinAt(t)
			db := t - portalBurst
			heat := 0.85 + 0.15*math.Sin(t*7) + 0.6*math.Exp(-(db*db)/0.01)
			// the floor glows violet round the hole
			if t < portalPop {
				p.Glow(0, -4, rx*1.8+10, C[4], 0.32*pixelfx.Clamp(rx/RX))
				// and light wells up out of it into the air
				p.GlowLine(0, -2, 0, -44, rx*0.7+6, C[4], 0.16*pixelfx.Clamp(rx/RX))
			}
			// the slit it tears open from
			if t < portalOpen*0.6 {
				k := t / (portalOpen * 0.6)
				L := RX * 1.1 * pixelfx.Ease.OutCubic(k)
				p.LineFn(-L, 0, L, 0, func(u float64) (uint32, float64) {
					if math.Abs(u-0.5) < 0.3 {
						return pixelfx.White, 1
					}
					return C[6], 1
				})
				p.GlowLine(-L, 0, L, 0, 7, C[5], 0.6)
			}
			if t > portalOpen*0.35 && t < portalPop {
				// the arms wind tighter as it twists shut
				portalHole(p, C, rx, spin, heat+0.4*pixelfx.Smooth(portalShut, portalPop, t), 2.3+2.6*pixelfx.Smooth(portalShut, portalPop-0.05, t))
				portalLip(p, C, rx, spin, heat, false)
			}
			for _, r := range rings {
				drawPortalRing(p, C, r, t, false)
			}
			drawSwirl(p, false)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			rx, spin := portalRadius(t, RX), portalSpinAt(t)
			db := t - portalBurst
			heat := 0.85 + 0.15*math.Sin(t*7) + 0.6*math.Exp(-(db*db)/0.01)
			if t > portalOpen*0.35 && t < portalPop {
				portalLip(p, C, rx, spin, heat, true)
			}
			for _, r := range rings {
				drawPortalRing(p, C, r, t, true)
			}
			drawSwirl(p, true)
			// the burst: a column of light shoots up out of the hole, thins and lifts away
			b := t - portalBurst
			if b > 0 && b < 0.45 {
				k := b / 0.45
				top := -84 * pixelfx.Ease.OutExpo(pixelfx.Clamp(b/0.14))
				bot := -1 - 60*pixelfx.Ease.InQuad(k)
				w0 := RX * 0.55 * (1 - k*0.8)
				p.Shade(-w0-1, top, w0+1, bot, func(x, y float64, out *pixelfx.Out) uint32 {
					u := (y - bot) / (top - bot)
					half := w0 * math.Pow(1-u, 0.8) * (0.85 + 0.15*math.Sin(y*0.7+t*40))
					d := math.Abs(x) / math.Max(0.5, half)
					if d > 1 {
						return 0
					}
					switch {
					case d < 0.35:
						out.C = pixelfx.White
					case d < 0.7:
						out.C = C[6]
					default:
						out.C = C[5]
					}
					return 0
				})
				p.GlowLine(0, bot, 0, top, w0+12, C[5], 0.55*(1-k))
				if b < 0.14 {
					pixelfx.Flare(p, 0, -4, 36*(1-b/0.14), 1-b/0.14, C)
				}
			}
			// the collapse: a white point that pops
			q := t - portalPop
			if q > -0.08 && q < 0.25 {
				armBase := 12.0
				if q < 0 {
					armBase = 4
				}
				pixelfx.Sparkle(p, 0, -1, pixelfx.Round(armBase*(1-pixelfx.Clamp(q/0.25))), pixelfx.White, C[5], 0.7*(1-pixelfx.Clamp(q/0.25)))
			}
			rocks.Draw(p)
			for i := range wisps {
				w := &wisps[i]
				k := w.life / w.max
				for j := 1; j < len(w.tr); j++ {
					x0, y0 := w.tr[j-1][0], w.tr[j-1][1]
					x1, y1 := w.tr[j][0], w.tr[j][1]
					u := float64(j) / float64(len(w.tr))
					col := C[4]
					if u < 0.25 {
						col = pixelfx.White
					} else if u < 0.55 {
						col = C[6]
					}
					p.Line(x0, y0, x1, y1, col, 1.15-u)
					if u < 0.35 {
						p.Line(x0+1, y0, x1+1, y1, C[5], 1)
					}
				}
				p.Circle(w.x, w.y, 2, C[5], 1)
				p.Circle(w.x, w.y, 1.2, C[6], 1)
				p.Px(w.x, w.y, pixelfx.White)
				p.Glow(w.x, w.y, 12, C[5], 0.5*k+0.2)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
