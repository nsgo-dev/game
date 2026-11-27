// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/tractor-beam.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source black hole (https://www.spritefusion.com/pixel-art-vfx/black-hole) for rocks torn out of the floor in dust, hauled up spinning, and matter drawn in as streaks, MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// TractorBeam is a hovering pod projecting a cone of light to the floor: rocks tear loose in puffs of dust and spiral up spinning into the lens while rings of light climb the beam. It is registered as "scifi/tractor-beam".
var TractorBeam = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Tractor beam",
		Desc:     "A hovering pod projects a cone of light to the floor; rocks tear loose in puffs of dust and spiral up spinning into the lens while rings of light climb the beam.",
		Tags:     []string{"scifi", "beam", "tractor", "lift", "abduct", "debris", "ground", "loop"},
		Size:     [2]int{120, 132},
		Origin:   [2]int{60, 114},
		Duration: tbDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#06161a", "#0b2e33", "#11505a", "#1a7f84", "#2bb5a8", "#7ef0c8", "#d6fff0", "#ffffff")},
			{Name: "hull", Default: pixelfx.Palette("#0d0f17", "#1c2030", "#2e3448", "#4a5470", "#7a86a8", "#b8c4e0")},
			{Name: "stone", Default: pixelfx.Palette("#1a1622", "#2e2838", "#4a4258", "#6d6480", "#9a90aa")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	New: newTractorBeam,
}

func init() { pixelfx.Register("scifi/tractor-beam", TractorBeam) }

const (
	tbDuration = 3.0
	tbLens     = -84.0
	tbTopW     = 5.5
	tbBotW     = 32.0
)

var (
	tbPop   = 0.1
	tbHover = 0.32
	tbRise  = 1.4
	tbLife  = tbPop + tbHover + tbRise
)

func tbHalf(y float64) float64 { return pixelfx.Lerp(tbBotW, tbTopW, pixelfx.Clamp(y/tbLens)) }

// tbHaul returns [x, y, depth (> 0 on the near side)] for something hauled up the beam at progress k (0 on the floor, 1 in the lens).
func tbHaul(r0, th0, dir, turns, k float64) (float64, float64, float64) {
	s := math.Pow(pixelfx.Clamp(k), 1.8)
	y := pixelfx.Lerp(-2, tbLens+5, s)
	th := th0 + dir*tau*turns*s
	rr := r0 * (tbHalf(y) / tbBotW) * (1 - 0.5*s)
	return math.Cos(th) * rr, y, math.Sin(th)
}

type tbRock struct {
	id, born, r0, th0, dir, s, a0, va float64
}

type tbRockState struct {
	x, y, z, px, py, size, a, lift float64
}

func tbRockAt(r tbRock, age float64) tbRockState {
	lift := pixelfx.Clamp((age - tbPop - tbHover) / tbRise)
	if lift > 0 {
		x, y, z := tbHaul(r.r0, r.th0, r.dir, 1.3, lift)
		x1, y1, _ := tbHaul(r.r0, r.th0, r.dir, 1.3, lift-0.07)
		return tbRockState{x: x, y: y, z: z, px: x1, py: y1, size: r.s * (1 - 0.6*math.Pow(lift, 2.5)), a: r.a0 + r.va*age*(0.3+lift*2), lift: lift}
	}
	var y, wob float64
	if age < tbPop {
		y = pixelfx.Lerp(3, -2, age/tbPop)
	} else {
		y = -2 - 2*pixelfx.Smooth(0, tbHover, age-tbPop)
		wob = math.Sin(age*55+r.th0*9) * 0.7
	}
	return tbRockState{x: math.Cos(r.th0)*r.r0 + wob, y: y, z: 1, size: r.s, a: r.a0 + r.va*age*0.3, lift: 0}
}

// tbRockDraw draws a tumbling chunk of stone: an uneven outline, lit from above by the lens, shaded underneath.
func tbRockDraw(p *pixelfx.Painter, q tbRockState, r tbRock, S, C []uint32, back bool) {
	const n = 6
	R := math.Max(0.9, q.size)
	ca, sa := math.Cos(q.a), math.Sin(q.a)
	lit := pixelfx.Mix(S[4], C[6], 0.3+0.6*q.lift)
	rad := func(ang float64) float64 {
		f := pixelfx.Wrap((ang/tau)*n, n)
		i := math.Floor(f)
		k := f - i
		return R * (0.75 + 0.35*pixelfx.Lerp(pixelfx.Rand(r.id, i), pixelfx.Rand(r.id, math.Mod(i+1, n)), k))
	}
	// a faint trail of its rush up the beam
	if q.lift > 0.15 && !back {
		p.LineFn(q.px, q.py, q.x, q.y, func(u float64) (uint32, float64) {
			if u > 0.6 {
				return C[5], 0.7 * u
			}
			return C[3], 0.7 * u
		})
		trailCol := pixelfx.Mix(S[3], C[5], 0.4)
		for i := 1.0; i <= 3; i++ {
			p.Disc(pixelfx.Lerp(q.x, q.px, i/3), pixelfx.Lerp(q.y, q.py, i/3)+1, R*(0.5+0.25*i), trailCol, 0.22*(1-i/4)*q.lift)
		}
	}
	p.Shade(q.x-R-2, q.y-R-2, q.x+R+2, q.y+R+2, func(x, y float64, out *pixelfx.Out) uint32 {
		dx, dy := x-q.x, y-q.y
		u, v := dx*ca+dy*sa, -dx*sa+dy*ca
		d, e := pixelfx.Hypot(u, v), rad(math.Atan2(v, u))
		if d > e+0.8 {
			return 0
		}
		switch {
		case d > e:
			out.C = S[0]
		case back:
			if dy < -0.3*R {
				out.C = S[3]
			} else {
				out.C = S[1]
			}
		case dy < -0.45*R:
			if q.lift > 0.35 && d > e-1 {
				out.C = C[6]
			} else {
				out.C = lit
			}
		case dy < -0.1*R:
			if dx < 0 {
				out.C = S[4]
			} else {
				out.C = S[3]
			}
		case dy > 0.35*R:
			out.C = S[1]
		default:
			out.C = S[2]
		}
		return 0
	})
	if !back && q.lift > 0 {
		p.Glow(q.x, q.y-R, 5+R, C[5], 0.25*q.lift)
	}
}

// tbPod draws the hovering pod: a cockpit dome on a wide hull, a rim of running lights, the lens underneath.
func tbPod(p *pixelfx.Painter, t float64, C, H []uint32, y0 float64) {
	y := pixelfx.Round(y0)
	p.Shade(-21, y-12, 21, y+6, func(x, yy float64, out *pixelfx.Out) uint32 {
		dy := yy - y
		// the cockpit: a small glassy dome, a hot glint top left
		cx, cy := x/8.5, (dy+5)/6
		if cy < 0 && cx*cx+cy*cy <= 1 {
			d, l := cx*cx+cy*cy, -cx*0.6-cy*0.8
			switch {
			case d > 0.8:
				out.C = H[0]
			case l > 0.85:
				out.C = pixelfx.White
			case l > 0.6:
				out.C = C[6]
			case l > 0.2:
				out.C = C[4]
			default:
				out.C = C[3]
			}
			return 0
		}
		// the hull: a flat lens shape, lit on its upper face, a dark seam, the rim
		hx, hy := x/20, (dy+0.5)/4.5
		if dy <= 0 && hx*hx+hy*hy <= 1 {
			d, l := hx*hx+hy*hy, -hx*0.55-hy*0.7
			switch {
			case d > 0.88:
				out.C = H[0]
			case dy == -2:
				out.C = H[1]
			case l > 0.55:
				out.C = H[5]
			case l > 0.25:
				out.C = H[4]
			case l > -0.1:
				out.C = H[3]
			default:
				out.C = H[2]
			}
			return 0
		}
		// the underside, tapering in toward the lens housing, lit from below by its own beam
		w := 19.5 - dy*2.4
		if dy > 0 && dy <= 5 && math.Abs(x) <= w {
			switch {
			case math.Abs(x) > w-1:
				out.C = H[0]
			case dy == 1:
				out.C = H[2]
			case dy >= 4 && math.Abs(x) < 9:
				out.C = C[3]
			default:
				out.C = H[1]
			}
		}
		return 0
	})
	// running lights chase round the rim
	for i := range 6 {
		lx := -15 + float64(i)*6
		k := 0.5 + 0.5*math.Cos(tau*(4*(t/tbDuration)-float64(i)/6))
		c := C[2]
		if k > 0.7 {
			c = pixelfx.White
		} else if k > 0.35 {
			c = C[5]
		}
		p.Px(lx, y+1, c)
		if k > 0.5 {
			p.Glow(lx, y+1, 5, C[5], 0.35*k)
		}
	}
	// the lens: a white-hot eye in a ring of light
	p.EllipseFn(0, y+5.5, 7.5, 2.2, func(d, _ float64) (uint32, float64) {
		switch {
		case d < 0.45:
			return pixelfx.White, 1
		case d < 0.75:
			return C[6], 1
		case d < 0.9:
			return C[5], 1
		}
		return C[3], 1
	})
}

// tbHoop draws a ring of light round the beam at height y: a hard line in front, a fainter one for the far side.
func tbHoop(p *pixelfx.Painter, y, rx float64, c uint32, back bool) {
	ry := rx * 0.17
	n := int(math.Ceil(rx * 7))
	for i := range n {
		g := (float64(i) / float64(n)) * tau
		s := math.Sin(g)
		if (s >= 0) == back {
			continue
		}
		p.Px(math.Cos(g)*rx, y+s*ry, c)
	}
}

func newTractorBeam(o *pixelfx.Options) pixelfx.Instance {
	C, H, S, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("hull"), 6), pixelfx.Fit(o.Ramp("stone"), 5), o.Seed()
	rn := pixelfx.NewRng(seed)
	// rocks torn up one after another, from spots left and right of the middle
	rocks := make([]tbRock, 7)
	for i := range 7 {
		fi := float64(i)
		born := (fi/7)*tbDuration + rn.Range(-0.06, 0.06)
		r0 := rn.Range(8, 25)
		baseTh := pi
		if i%2 != 0 {
			baseTh = 0
		}
		th0 := baseTh + rn.Range(-0.45, 0.45)
		dir := rn.Sign()
		s := rn.Range(2.4, 4.4)
		a0 := rn.Range(0, tau)
		va := rn.Range(-7, 7)
		rocks[i] = tbRock{id: fi + seed*31, born: born, r0: r0, th0: th0, dir: dir, s: s, a0: a0, va: va}
	}
	g := newGhosts(tbDuration, ghostOpts{
		ground: zeroGround,
		hasBox: true,
		box:    [4]float64{-58, -112, 58, 14},
	})
	dustCol := pixelfx.Mix(S[3], C[4], 0.3)
	// each rock leaves the floor in a puff of dust and a spray of grit
	for _, r := range rocks {
		x := math.Cos(r.th0) * r.r0
		for range 5 {
			px, vx, vy := x+rn.Range(-3, 3), rn.Range(-34, 34), -rn.Range(8, 26)
			life, size := rn.Range(0.5, 0.9), rn.Range(2.2, 3.6)
			g.add(r.born+0.02, pixelfx.Dust, px, -1, vx, vy, life, dustCol, size, 0)
		}
		for range 6 {
			px, vx, vy := x+rn.Range(-2, 2), rn.Range(-45, 45), -rn.Range(60, 150)
			life := rn.Range(0.5, 0.9)
			col := pixelfx.Pick(rn, []uint32{S[2], S[3], S[4]})
			size := 1.0
			if rn.Chance(0.3) {
				size = 2
			}
			g.add(r.born+0.02, pixelfx.Grit, px, -2, vx, vy, life, col, size, 0)
		}
	}
	// dust always swirling round the foot of the beam
	swirlCol := pixelfx.Mix(S[3], C[5], 0.3)
	for range 16 {
		s := rn.Sign()
		born := rn.Range(0, tbDuration)
		x, y := s*rn.Range(14, 34), -rn.Range(0, 3)
		vx, vy := -s*rn.Range(6, 22), -rn.Range(4, 14)
		life, size := rn.Range(0.9, 1.5), rn.Range(2.6, 4.4)
		g.add(born, pixelfx.Dust, x, y, vx, vy, life, swirlCol, size, 0)
	}

	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			// the pool of light where the beam meets the floor, and the far half of its rim
			for y := -6.0; y <= 6; y++ {
				for x := -tbBotW; x <= tbBotW; x++ {
					d := pixelfx.Hypot((x+0.5)/tbBotW, (y+0.5)/5.5)
					if d <= 1 {
						c := C[5]
						if d < 0.35 {
							c = C[6]
						}
						p.Blend(x+0.5, y+0.5, c, 0.2+0.55*math.Pow(1-d, 1.5))
					}
				}
			}
			tbHoop(p, 0, tbBotW+1, C[5], true)
			g.at(t).Draw(p, pixelfx.LayerBack)
			// rocks on the far side of the beam, seen through it
			for _, r := range rocks {
				age := since(t, r.born, tbDuration)
				if age >= tbLife {
					continue
				}
				q := tbRockAt(r, age)
				if q.z < 0 && q.lift > 0 {
					tbRockDraw(p, q, r, S, C, true)
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := t / tbDuration
			podY := tbLens - 5 + 1.5*math.Sin(tau*2*ph)
			// the beam: a glassy cone, white-hot at the lens, hard bright edges with a soft fringe, light drifting in it
			for y := math.Ceil(tbLens + 3); y < 0; y++ {
				w := tbHalf(y)
				v := (y - tbLens) / -tbLens
				pulse := 0.0
				if pixelfx.Wrap(v*3.2+ph*6, 1) < 0.08 {
					pulse = 1
				}
				for x := math.Floor(-w) - 1; x <= math.Ceil(w); x++ {
					q := math.Abs(x+0.5) / w
					if q > 1+1.2/w {
						continue
					}
					switch {
					case q > 1:
						p.Dot(x, y, C[4], 0.6-0.3*v)
					case q > 1-1/w:
						c := C[5]
						if v < 0.2 {
							c = pixelfx.White
						} else if v < 0.45 {
							c = C[6]
						}
						p.Px(x, y, c)
					default:
						curtain := 0.0
						for s := range 4 {
							fs := float64(s)
							if math.Abs((x+0.5)/w-(pixelfx.Rand(seed, fs, 7)-0.5)*1.4-0.12*math.Sin(tau*ph+fs*1.7)) < 0.045 {
								curtain = 1
							}
						}
						core := (1 - q) * (1 - q) * math.Pow(1-v, 1.6)
						a := 0.08 + 0.22*math.Pow(q, 4) + 0.6*core + 0.12*pulse + 0.18*curtain
						c := C[4]
						if core > 0.45 {
							c = pixelfx.White
						} else if curtain != 0 || pulse != 0 || v < 0.35 {
							c = C[5]
						}
						p.Blend(x+0.5, y+0.5, c, pixelfx.Clamp(a))
					}
				}
				if pixelfx.ToInt32(y)&3 == 0 {
					for _, s := range [2]float64{-1, 1} {
						p.Glow(s*w, y, 8, C[4], 0.18)
					}
				}
			}
			// rings climbing the beam, quickening and narrowing with it
			for i := range 5 {
				k := since(t, (float64(i)/5)*tbDuration, tbDuration) / 1.1
				if k >= 1 {
					continue
				}
				y := pixelfx.Lerp(0, tbLens+5, pixelfx.Ease.InQuad(k))
				w := tbHalf(y) + 1
				tbHoop(p, y, w, C[3], true)
				fc := C[6]
				if k < 0.12 {
					fc = pixelfx.White
				}
				tbHoop(p, y, w, fc, false)
				p.Glow(0, y+w*0.17, w+4, C[5], 0.12)
			}
			// light streaks drawn up the beam
			pixelfx.Cycle(t, tbDuration, 22, seed+1, func(r *pixelfx.Rng) float64 { return r.Range(0.6, 1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				y := pixelfx.Lerp(-2, tbLens+5, k*k)
				w := tbHalf(y)
				x := r.Range(-0.85, 0.85) * w
				L := 2 + 6*k
				a := math.Min(1, (1-k)*3)
				p.LineFn(x, y, x, y+L, func(u float64) (uint32, float64) {
					if u < 0.3 {
						return pixelfx.White, a
					}
					return C[5], a
				})
			})
			// dust drawn up in soft puffs, thinning as it goes
			pixelfx.Cycle(t, tbDuration, 9, seed+3, func(r *pixelfx.Rng) float64 { return r.Range(1.4, 2) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				x, y, _ := tbHaul(r.Range(8, 26), r.Range(0, tau), r.Sign(), 0.8, k)
				p.Disc(x, y, pixelfx.Lerp(4, 1.5, k), pixelfx.Mix(S[3], C[5], 0.25+0.5*k), 0.4*math.Min(1, k*6)*(1-k))
			})
			// grit and motes of light hauled up in a quick spiral, streaking as they go
			pixelfx.Cycle(t, tbDuration, 46, seed+2, func(r *pixelfx.Rng) float64 { return r.Range(0.8, 1.3) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				r0, th0, dir, turns, mote := r.Range(6, 30), r.Range(0, tau), r.Sign(), r.Range(1, 1.8), r.Chance(0.45)
				x, y, z := tbHaul(r0, th0, dir, turns, k)
				backK := 0.06
				if mote {
					backK = 0.13
				}
				x1, y1, _ := tbHaul(r0, th0, dir, turns, k-backK)
				var c uint32
				if mote {
					if r.Chance(0.5) {
						c = pixelfx.White
					} else {
						c = C[6]
					}
				} else {
					c = pixelfx.Pick(r, []uint32{S[3], S[4], S[4]})
				}
				tailCol := S[2]
				if mote {
					tailCol = C[4]
				}
				headCol := c
				if z <= 0 {
					headCol = pixelfx.Scale(c, 0.7)
				}
				p.LineFn(x1, y1, x, y, func(u float64) (uint32, float64) {
					if u > 0.5 {
						return headCol, 1
					}
					return tailCol, 1
				})
				if mote {
					p.Px(x, y, pixelfx.White)
					p.Glow(x, y, 4, C[5], 0.45)
				}
			})
			// twinkles caught in the light
			pixelfx.Cycle(t, tbDuration, 8, seed+4, pixelfx.Fixed(0.5), func(age, life float64, r *pixelfx.Rng, _ int) {
				k := age / life
				x, y, _ := tbHaul(r.Range(4, 24), r.Range(0, tau), r.Sign(), 0.6, r.Range(0.1, 0.7)+k*0.15)
				arm := pixelfx.Round(2 * math.Sin(pi*k))
				if arm > 0 {
					pixelfx.Sparkle(p, x, y, arm, C[6], C[5], 0.4)
				}
			})
			// the rocks on the near side and those still on the floor
			for _, r := range rocks {
				age := since(t, r.born, tbDuration)
				if age >= tbLife {
					continue
				}
				q := tbRockAt(r, age)
				if q.z >= 0 || q.lift <= 0 {
					tbRockDraw(p, q, r, S, C, false)
				}
				// swallowed: a glint at the lens
				if age > tbLife-0.1 {
					pixelfx.Sparkle(p, q.x, tbLens+3, 5, C[6], C[5], 0.5)
				}
			}
			for _, r := range rocks {
				flash(p, t, r.born+tbLife-0.08, 0, tbLens+3, 30, C[5], 0.7, 0.28)
			}
			g.at(t).Draw(p, pixelfx.LayerFront)
			tbHoop(p, 0, tbBotW+1, pixelfx.White, false)
			tbHoop(p, 1, tbBotW, C[5], false)
			// light: the lens, the beam's body, the pool on the floor
			tbPod(p, t, C, H, podY)
			fl := 0.85 + 0.15*math.Sin(tau*6*ph)
			p.Glow(0, tbLens+6, 36, C[5], 0.7*fl)
			p.Glow(0, tbLens+6, 12, pixelfx.White, 0.7*fl)
			for y := tbLens + 16; y < -8; y += 14 {
				p.Glow(0, y, tbHalf(y)+10, C[4], 0.16)
			}
			for _, x := range [3]float64{-20, 0, 20} {
				p.Glow(x, -2, 22, C[4], 0.26)
			}
		},
	}
}
