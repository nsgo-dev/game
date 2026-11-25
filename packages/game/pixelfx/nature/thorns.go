// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/thorns.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the wave (spikes punching up one after another, each bigger, with overshoot, a white frame, a burst of debris and glints, a line running along the floor ahead of them) from Sprite Fusion's open-source ice spikes (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Thorns is a wave of hooked thorns that punch up one after another from a glowing floor seam, hold, and sink back down. It is registered as "nature/thorns".
var Thorns = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Thorns",
		Desc:     "A seam races along the floor and hooked thorns punch up out of it one after another, each bigger than the last, in bursts of dirt and green light; they hold, then sink.",
		Tags:     []string{"nature", "thorns", "plant", "spikes", "ground", "attack", "one-shot"},
		Size:     [2]int{232, 92},
		Origin:   [2]int{48, 78},
		Duration: thornsDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#160c0e", "#2e1418", "#4e2224", "#743a30", "#9a5a3e", "#c08458", "#e2b484", "#fbe8c8")},
			{Name: "glow", Default: pixelfx.Palette("#123818", "#2f7a2a", "#6cb83a", "#c8f070")},
			{Name: "soil", Default: pixelfx.Palette("#120c0c", "#251917", "#3b2a24", "#553d31", "#715440", "#8f6e52", "#b8966e", "#e6d0a8")},
			{Name: "count", Default: pixelfx.Num(6)},
			{Name: "seed", Default: pixelfx.Num(8)},
		},
	},
	New: newThorns,
}

func init() { pixelfx.Register("nature/thorns", Thorns) }

const thornsDuration = 2.4

var (
	thornsRise = 0.11
	thornsSink = 0.36
)

type thornItem struct {
	x, h, w, lean, hook, at, sink, seed float64
	up, down                            bool
}

// drawThorn draws a hooked hardwood horn tapering from its foot, lit down its left edge, with a back barb.
func drawThorn(p *pixelfx.Painter, s thornItem, h float64, C []uint32, white bool, base, a float64) {
	if h < 1.5 {
		return
	}
	cx := func(u float64) float64 { return s.x + s.lean*u + s.hook*math.Pow(u, 2.4) }
	hw := func(u float64) float64 { return (s.w/2)*math.Pow(1-u, 0.9) + 0.25 }
	inside := func(px, py float64) bool {
		u := (base - (py + 0.5)) / h
		if u < 0 || u > 1 || py+0.5 > 0 {
			return false
		}
		return math.Abs(px+0.5-cx(u)) <= hw(u)
	}
	X0 := math.Floor(s.x - s.w - 6)
	X1 := math.Ceil(s.x + s.w/2 + math.Abs(s.lean) + math.Abs(s.hook) + 2)
	for py := math.Floor(base-h) - 1; py <= math.Min(0, math.Ceil(base)); py++ {
		for px := X0; px <= X1; px++ {
			if !inside(px, py) {
				if py <= 0 && (inside(px-1, py) || inside(px+1, py) || inside(px, py+1) || inside(px, py-1)) {
					p.Dot(px, py, C[0], a)
				}
				continue
			}
			if white {
				p.Px(px, py, pixelfx.White)
				continue
			}
			u := (base - (py + 0.5)) / h
			c := cx(u)
			r := hw(u)
			q := (px + 0.5 - (c - r)) / (2 * r)
			var i int
			switch {
			case q < 0:
				i = 4
			case q < 0.16:
				i = 6
			case q < 0.36:
				i = 5
			case q < 0.46:
				i = 7
			case q < 0.7:
				i = 4
			case q < 0.86:
				i = 3
			default:
				i = 2
			}
			if u > 0.86 {
				if q < 0.6 {
					i = 7
				} else {
					i = 5
				}
			}
			if i < 7 && i > 2 && pixelfx.Rand(px, math.Floor((py+0.5-base)/3), s.seed) < 0.12 {
				i--
			}
			p.Px(px, py, C[i])
		}
	}
	// a barb off its back, raking up and away
	if !white && h > 14 {
		u0, u1 := 0.34, 0.46
		y0, y1 := base-u0*h, base-u1*h
		e0, e1 := cx(u0)-hw(u0), cx(u1)-hw(u1)
		L := s.w * 0.45
		if y0 <= 0 {
			p.Tri(e0+0.5, y0, e1+0.5, y1, e1-L, y1-L*0.7, C[3], 1)
			p.Line(e1, y1, e1-L, y1-L*0.7, C[6], 1)
			p.Px(e1-L, y1-L*0.7, C[7])
			p.Line(e0, y0+1, e1-L+1, y1-L*0.7+1, C[0], 1)
		}
	}
}

func newThorns(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	G := pixelfx.Fit(o.Ramp("glow"), 4)
	S := pixelfx.Fit(o.Ramp("soil"), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	n := int(math.Max(2, o.Num("count")))
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{G[3], G[2], G[1], G[0]}})
	lights := &pixelfx.Lights{}
	var chips []barkChip
	list := make([]thornItem, n)
	for i := range list {
		k := float64(i) / float64(n-1)
		h := 12 + 36*k + R.Range(-1.5, 1.5)
		x := 10 + float64(i)*(104/float64(n-1)) + R.Range(-3, 3)
		lean := R.Range(-1, 1.5) + k*2
		hook := h * R.Range(0.14, 0.22)
		seed := R.Int(1, 999)
		list[i] = thornItem{x: x, h: h, w: 6 + 9*k, lean: lean, hook: hook, at: 0.12 + float64(i)*0.075, sink: 1.45 + float64(i)*0.06, seed: seed}
	}
	reach := list[n-1].x + 8
	t := 0.0
	chip := func(x, sp float64, nn int) {
		for range nn {
			a := -pi/2 + R.Range(-1.1, 1.1)
			v := R.Range(60, 210) * sp
			cx := x + R.Range(-3, 3)
			ang := R.Range(0, tau)
			va := R.Range(-15, 15)
			sz := R.Range(1.3, 2.6) * math.Sqrt(sp)
			pts := stoneShape(R, 0)
			life := math.Min(R.Range(0.7, 1.1), thornsDuration-0.05-t)
			chips = append(chips, barkChip{x: cx, y: -1, vx: math.Cos(a) * v, vy: math.Sin(a) * v, a: ang, va: va, s: sz, pts: pts, life: life})
		}
	}
	grown := func(s thornItem) float64 {
		if t < s.at {
			return 0
		}
		return pixelfx.OutBack(pixelfx.Clamp((t-s.at)/thornsRise), 2.4)
	}
	sunk := func(s thornItem) float64 { return pixelfx.Smooth(s.sink, s.sink+thornsSink, t) }
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			for idx := range list {
				s := &list[idx]
				k := s.h / 48
				if !s.up && t >= s.at {
					s.up = true
					lights.Add(s.x, -s.h*0.5, 18+18*k, pixelfx.White, 0.45, 0.08)
					lights.Add(s.x, -s.h*0.4, 26+26*k, G[2], 0.45, 0.4)
					chip(s.x, 0.6+0.6*k, 3+int(pixelfx.Round(5*k)))
					for i := 0.0; i < 10+18*k; i++ {
						a := -pi/2 + R.Range(-1, 1)
						v := R.Range(60, 230) * (0.6 + 0.6*k)
						px := s.x + R.Range(-s.w/3, s.w/3)
						life := R.Range(0.6, 1.1)
						col := pixelfx.Pick(R, []uint32{S[5], S[4], S[3]})
						sz := 1.0
						if R.Chance(0.3) {
							sz = 2
						}
						ps.Spawn(pixelfx.Grit, px, -1, math.Cos(a)*v, math.Sin(a)*v, life, col, sz, 0, 0)
					}
					for i := 0.0; i < 3+5*k; i++ {
						px := s.x + R.Range(-4, 4)
						py := -R.Range(1, 4)
						vx := R.Sign() * R.Range(20, 80)
						vy := -R.Range(5, 25)
						life := R.Range(0.7, 1.2)
						sz := R.Range(3, 5.5) * (0.6 + 0.5*k)
						ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, S[5], sz, 0, 0)
					}
					for i := 0.0; i < 2+3*k; i++ {
						px := s.x + s.hook*0.8 + R.Range(-3, 3)
						py := -s.h * R.Range(0.5, 1)
						vx := R.Range(-10, 10)
						vy := -R.Range(5, 20)
						life := R.Range(0.3, 0.6)
						sz := R.Range(0.8, 2)
						delay := R.Range(0, 0.12)
						ps.Spawn(pixelfx.StarKind, px, py, vx, vy, life, G[3], sz, delay, 0)
					}
					for range 4 {
						px := s.x + R.Range(-3, 3)
						py := -R.Range(2, s.h*0.6)
						vx := R.Range(-20, 20)
						vy := -R.Range(30, 70)
						life := R.Range(0.4, 0.8)
						ps.Spawn(pixelfx.Mote, px, py, vx, vy, life, G[3], 0.7, 0, 0)
					}
				}
				if !s.down && t >= s.sink {
					s.down = true
					chip(s.x, 0.35, 2+int(pixelfx.Round(2*k)))
					for i := 0.0; i < 3+3*k; i++ {
						px := s.x + R.Range(-s.w/2, s.w/2)
						py := -R.Range(0, 3)
						vx := R.Sign() * R.Range(15, 50)
						vy := -R.Range(2, 10)
						life := math.Min(R.Range(0.4, 0.7), thornsDuration-0.05-t)
						sz := R.Range(2, 4)
						ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, S[5], sz, 0, 0)
					}
				}
				// while they stand, glints run up their lit edges
				if s.up && !s.down && t > s.at+0.2 && R.Chance(0.05*f) {
					py := -s.h * R.Range(0.3, 0.8)
					life := R.Range(0.25, 0.45)
					sz := R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, s.x+s.lean*0.5-s.w*0.15, py, 0, -3, life, C[7], sz, 0, 0)
				}
			}
			for i := len(chips) - 1; i >= 0; i-- {
				c := &chips[i]
				c.life -= dt
				if c.life <= 0 {
					chips = append(chips[:i], chips[i+1:]...)
					continue
				}
				c.vy += 950 * dt
				c.x += c.vx * dt
				c.y += c.vy * dt
				c.a += c.va * dt
				if c.y > 0 && c.vy > 0 {
					if c.vy > 120 {
						c.y = 0
						c.vy *= -0.3
						c.vx *= 0.6
						c.va *= 0.5
					} else {
						c.y = 0
						c.vy, c.vx, c.va = 0, 0, 0
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the seam racing ahead along the floor: a dark split with a green glow in it, closing up again at the end
			run := pixelfx.Ease.OutCubic(pixelfx.Clamp((t-0.04)/0.5)) * reach
			fade := 1 - pixelfx.Smooth(1.9, 2.3, t)
			for x := -2.0; x <= run; x++ {
				hot := pixelfx.Clamp(1-(run-x)/18) * (1 - pixelfx.Smooth(0.6, 1.2, t))
				h := pixelfx.Rand(x, 31)
				col := S[0]
				if hot > 0.4 {
					col = G[3]
				} else if hot > 0.1 {
					col = G[2]
				}
				p.Dot(x, 0, col, fade)
				if h < 0.5 {
					p.Dot(x, 1, S[1], fade*0.8)
				}
				if h < 0.15 {
					p.Dot(x, -1, S[4], fade)
				}
				if hot > 0.3 && int(x)&3 == 0 {
					p.Glow(x, 0, 6, G[2], 0.3*hot)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			for _, s := range list {
				g, k := grown(s), sunk(s)
				if g <= 0 || k >= 1 {
					continue
				}
				drawThorn(p, s, s.h*g, C, t-s.at < 0.035, k*s.h*1.05, 1)
				if t-s.at < 0.3 {
					p.Glow(s.x, -s.h*0.5*g, s.h*0.6+6, G[2], 0.35*(1-(t-s.at)/0.3))
				}
				// a collar of leaves round its foot, unfurling as it comes up and folding away as it sinks
				lk := pixelfx.Clamp((t-s.at-0.04)/0.18) * (1 - k)
				lw := s.w * 0.5
				if lk > 0.05 {
					for _, lf := range [3][3]float64{{-1, -2.5, 1}, {1, -0.55, 0.9}, {-1, -1.9, 0.7}} {
						sd, a0, sz := lf[0], lf[1], lf[2]
						la := a0 + sd*0.25*(1-lk) + math.Sin(t*4+s.x)*0.06
						L := (5 + s.w*0.45) * sz * lk
						drawLeaf(p, s.x+sd*lw+math.Cos(la)*L*0.5, -1+math.Sin(la)*L*0.5, L, 1.4+s.w*0.1*sz, la, 0.35, [4]uint32{G[0], G[1], G[2], G[3]}, 1)
					}
				}
				p.Glow(s.x, -2, s.w+6, G[2], 0.18*lk)
			}
			for _, c := range chips {
				drawRock(p, c.x, c.y, c.s, c.a, c.pts, S, math.Min(1, c.life/0.25), false)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
