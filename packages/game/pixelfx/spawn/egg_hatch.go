// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/egg-hatch.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source ice shatter (https://www.spritefusion.com/pixel-art-vfx/ice) for the cracks that open in stages and the burst into tumbling pieces, MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// EggHatch is a speckled egg rocking three times, each rock cracking it further with warm light leaking out, then popping: light pours out of the opening and lit shell halves tumble away. It is registered as "spawn/egg-hatch".
var EggHatch = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Egg hatch",
		Desc:     "A speckled egg rocks three times, each rock cracking it further with warm light leaking out, then pops: light pours out of the opening and lit shell halves tumble away.",
		Tags:     []string{"spawn", "egg", "hatch", "birth", "crack", "shell", "light", "ground", "one-shot"},
		Size:     [2]int{120, 96},
		Origin:   [2]int{60, 84},
		Duration: eggDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1e1826", "#4a3e52", "#857684", "#c4b4ac", "#ece0d2", "#fffaf0")},
			{Name: "glow", Default: pixelfx.Palette("#ff8a2a", "#ffd25a", "#fff3c0")},
			{Name: "speckle", Default: pixelfx.Color("#9a6a52")},
			{Name: "seed", Default: pixelfx.Num(31)},
		},
	},
	New: newEggHatch,
}

func init() { pixelfx.Register("spawn/egg-hatch", EggHatch) }

const (
	eggEW = 16
	eggEH = 20
	eggZY = 11
)

var (
	eggDuration = 3.1
	eggWobbles  = [3][2]float64{{0.3, 0.11}, {0.85, 0.19}, {1.4, 0.28}}
	eggPop      = 1.98
	eggLight    = [3]float64{-0.72, 0.5, 0.48} // from the upper left, a little toward us
)

type eggBaseCell struct {
	x, y       int
	u, v, half float64
}

// the egg in its own pixels: x across from its middle, y up from its base; wider low than high
var eggBase = func() []eggBaseCell {
	var out []eggBaseCell
	for y := 0; y < eggEH; y++ {
		for x := -eggEW / 2; x < eggEW/2; x++ {
			v := (float64(y) + 0.5) / eggEH
			u := (float64(x) + 0.5) / (eggEW / 2)
			s := 2*v - 1
			half := math.Sqrt(math.Max(0, 1-s*s)) * (1.08 - 0.22*v)
			if math.Abs(u) <= half {
				out = append(out, eggBaseCell{x: x, y: y, u: u, v: v, half: half})
			}
		}
	}
	return out
}()

type eggKey struct{ x, y int }

type eggCell struct {
	x, y       int
	u, v, half float64
	c          uint32
	lit        float64
}

type eggPiecePixel struct {
	c    uint32
	edge bool
}

type eggPiece struct {
	id                  float64
	x, y, vx, vy, a, va float64
	r                   float64
	pxMap               map[eggKey]eggPiecePixel
	life, max, born     float64
	rest                bool
}

func newEggHatch(o *pixelfx.Options) pixelfx.Instance {
	C, G, SPK, R := pixelfx.Fit(o.Palette(), 6), pixelfx.Fit(o.Ramp("glow"), 3), o.Color("speckle"), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }

	egg := make([]eggCell, len(eggBase))
	at := make(map[eggKey]*eggCell, len(eggBase))
	for i, b := range eggBase {
		egg[i] = eggCell{x: b.x, y: b.y, u: b.u, v: b.v, half: b.half}
		at[eggKey{b.x, b.y}] = &egg[i]
	}
	has := func(x, y int) bool { _, ok := at[eggKey{x, y}]; return ok }

	// shading: a round shell lit from the upper left, a core shadow, a rim of bounce light on the dark side
	for i := range egg {
		e := &egg[i]
		nx := pixelfx.ClampTo(e.u/math.Max(0.05, e.half), -1, 1)
		ny := (2*e.v - 1) * 0.9
		nz := math.Sqrt(math.Max(0, 1-nx*nx-ny*ny))
		d := nx*eggLight[0] + ny*eggLight[1] + nz*eggLight[2]
		edge := !has(e.x-1, e.y) || !has(e.x+1, e.y) || !has(e.x, e.y-1) || !has(e.x, e.y+1)
		var c uint32
		switch {
		case d > 0.9:
			c = pixelfx.White
		case d > 0.62:
			c = C[5]
		case d > 0.32:
			c = C[4]
		case d > 0.02:
			c = C[3]
		case d > -0.3:
			c = C[2]
		default:
			c = C[1]
		}
		if edge && nx > 0.25 && ny < 0.55 {
			c = C[3] // rim light on the shadow side
		} else if pixelfx.Rand(float64(e.x), float64(e.y), 3) < 0.08 && e.v > 0.12 && !edge {
			if d > 0.2 {
				c = SPK
			} else {
				c = pixelfx.Mix(SPK, C[1], 0.5)
			}
		}
		e.c = c
		e.lit = d
	}

	// cracks: three stages, the last a zigzag right round the egg where the top breaks off
	crack := map[eggKey]int{}
	// a crack: a jagged line, zigging either side of its heading, every step joined pixel to pixel
	walk := func(x, y, a float64, length, s int) {
		px, py := pixelfx.Round(x), pixelfx.Round(y)
		for i := range length {
			sgn := -1.0
			if i%2 != 0 {
				sgn = 1
			}
			side := sgn * R.Range(0.6, 1.3)
			a += R.Range(-0.25, 0.25)
			x += math.Cos(a)*1.8 - math.Sin(a)*side*0.6
			y += math.Sin(a)*1.8 + math.Cos(a)*side*0.6
			nx, ny := pixelfx.Round(x), pixelfx.Round(y)
			n := int(math.Max(math.Abs(nx-px), math.Abs(ny-py)))
			for j := 1; j <= n; j++ {
				k := eggKey{int(pixelfx.Round(px + ((nx-px)*float64(j))/float64(n))), int(pixelfx.Round(py + ((ny-py)*float64(j))/float64(n)))}
				if _, ok := at[k]; !ok {
					return
				}
				if _, ok := crack[k]; !ok {
					crack[k] = s
				}
			}
			px, py = nx, ny
		}
	}
	walk(2, 15, -2.2, 3, 0)
	walk(2, 15, 0.3, 2, 0)
	walk(-2, 13, -0.6, 3, 1)
	walk(-2, 13, 2.7, 3, 1)
	walk(3, 11, 1.9, 2, 1)
	walk(2, 15, 1.5, 2, 1)
	zig := make([]int, 0, eggEW)
	for x := -eggEW / 2; x < eggEW/2; x++ {
		rem := (x + 8) % 3
		y := eggZY - 1
		if rem == 0 {
			y = eggZY + 1
		} else if rem == 1 {
			y = eggZY
		}
		zig = append(zig, y)
		if has(x, y) {
			crack[eggKey{x, y}] = 2
		}
	}
	zigAt := func(x int) int {
		idx := int(pixelfx.ClampTo(float64(x+eggEW/2), 0, float64(len(zig)-1)))
		return zig[idx]
	}
	// how close each pixel of shell is to an open crack, per stage: light shows through the shell round them
	var near [3]map[eggKey]float64
	for s := range 3 {
		var pts [][2]float64
		for k, cs := range crack {
			if cs <= s {
				pts = append(pts, [2]float64{float64(k.x), float64(k.y)})
			}
		}
		m := make(map[eggKey]float64, len(egg))
		for i := range egg {
			e := &egg[i]
			best := 9.0
			for _, pt := range pts {
				d := pixelfx.Hypot(pt[0]-float64(e.x), pt[1]-float64(e.y))
				if d < best {
					best = d
				}
			}
			m[eggKey{e.x, e.y}] = best
		}
		near[s] = m
	}

	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, G[2], G[1], G[0]}})
	lights := &pixelfx.Lights{}
	bits := pixelfx.NewBits(floor)
	var pieces []eggPiece
	t, popped, crumbled, cupA, cupV := 0.0, false, false, 0.0, 0.0
	var done [3]bool
	dur := eggDuration
	crumble := dur - 0.55

	stage := func(tt float64) int {
		switch {
		case tt > eggWobbles[2][0]+0.05:
			return 3
		case tt > eggWobbles[1][0]+0.05:
			return 2
		case tt > eggWobbles[0][0]+0.05:
			return 1
		}
		return 0
	}
	// the rock: a damped swing about the base, each one harder; a shiver just before it pops
	angle := func(tt float64) float64 {
		a := 0.0
		for _, w := range eggWobbles {
			u := tt - w[0]
			if u > 0 && u < 0.55 {
				a += w[1] * math.Sin(u*22) * math.Exp(-u*5)
			}
		}
		if tt > eggPop-0.14 && tt < eggPop {
			a += 0.05 * math.Sin(tt*120)
		}
		return a
	}
	// the light inside: grows with each crack, flashes at the pop, then dies down in the empty cup
	inner := func(tt float64) float64 {
		if tt < eggPop {
			s := float64(stage(tt)) / 3
			return s + 0.25*math.Max(0, math.Sin(tt*9))*s
		}
		return 1.6 * math.Exp(-(tt-eggPop)*2.6)
	}

	// break part of the shell into pieces round a few seeds; each keeps its lit outside and a hot broken edge
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	breakUp := func(keep func(x, y int) bool, n int, v func(cx, cy float64) (float64, float64), life func() float64) {
		seeds := make([][2]float64, n)
		for i := range n {
			seeds[i] = [2]float64{R.Range(-eggEW/2+1, eggEW/2-1), R.Range(0, eggEH)}
		}
		cells := make([][]*eggCell, n)
		var order []int
		for i := range egg {
			e := &egg[i]
			if !keep(e.x, e.y) {
				continue
			}
			best, bd := 0, 1e9
			for j, sd := range seeds {
				d := pixelfx.Hypot(float64(e.x)-sd[0], float64(e.y)-sd[1])
				if d < bd {
					bd, best = d, j
				}
			}
			if len(cells[best]) == 0 {
				order = append(order, best)
			}
			cells[best] = append(cells[best], e)
		}
		for _, idx := range order {
			px := cells[idx]
			cx, cy := 0.0, 0.0
			for _, e := range px {
				cx += float64(e.x) + 0.5
				cy += float64(e.y) + 0.5
			}
			cx /= float64(len(px))
			cy /= float64(len(px))
			mine := make(map[eggKey]struct{}, len(px))
			for _, e := range px {
				mine[eggKey{e.x, e.y}] = struct{}{}
			}
			pm := make(map[eggKey]eggPiecePixel, len(px))
			rMax := 0.0
			for _, e := range px {
				edge := false
				for _, d := range dirs {
					nk := eggKey{e.x + d[0], e.y + d[1]}
					if _, ok := mine[nk]; !ok && has(nk.x, nk.y) {
						edge = true
						break
					}
				}
				// lit by the flash: no piece flies off in shadow
				c := e.c
				if c == C[1] || c == C[2] {
					c = C[3]
				}
				pm[eggKey{int(pixelfx.Round(float64(e.x) + 0.5 - cx)), int(pixelfx.Round(-(float64(e.y) + 0.5 - cy)))}] = eggPiecePixel{c: c, edge: edge}
				if d := pixelfx.Hypot(float64(e.x)+0.5-cx, float64(e.y)+0.5-cy); d > rMax {
					rMax = d
				}
			}
			vx, vy := v(cx, cy)
			l := life()
			va := R.Range(-10, 10)
			pieces = append(pieces, eggPiece{
				id: float64(len(pieces) + 1), x: cx, y: -cy, vx: vx, vy: vy, a: 0, va: va,
				r: rMax + 1, pxMap: pm, life: l, max: l, rest: false, born: t,
			})
		}
	}

	popShardCols := []uint32{C[5], C[5], C[4], G[2]}
	popStarCols := []uint32{pixelfx.White, G[2], G[1]}
	wobbleStarCols := []uint32{pixelfx.White, G[2]}
	moteCols := []uint32{G[2], G[1], pixelfx.White}

	pop := func() {
		popped = true
		breakUp(func(x, y int) bool { return y >= zigAt(x) }, 6,
			func(cx, _ float64) (float64, float64) {
				return cx*R.Range(8, 13) + R.Range(-14, 14), -R.Range(140, 210)
			},
			func() float64 { return R.Range(0.95, 1.15) },
		)
		for range 14 {
			a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(70, 170)
			x, y := R.Range(-5, 5), -eggZY-R.Range(0, 4)
			life, ang, va, s, col := R.Range(0.55, 0.85), R.Range(0, tau), R.Range(-20, 20), R.Range(1.2, 2.4), pixelfx.Pick(R, popShardCols)
			bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x, Y: y, VX: math.Cos(a) * sp, VY: math.Sin(a) * sp, G: 0.9, Drag: 0.6, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, C: col})
		}
		for range 12 {
			a, sp := -pi/2+R.Range(-1.1, 1.1), R.Range(50, 110)
			x, life, col, size := R.Range(-3, 3), R.Range(0.4, 0.9), pixelfx.Pick(R, popStarCols), R.Range(1, 2.2)
			ps.Spawn(pixelfx.StarKind, x, -eggZY-2, math.Cos(a)*sp, math.Sin(a)*sp, life, col, size, 0, 0)
		}
		for range 10 {
			a, sp := -pi/2+R.Range(-1, 1), R.Range(100, 200)
			x, life := R.Range(-3, 3), R.Range(0.2, 0.45)
			ps.Spawn(pixelfx.Spark, x, -eggZY-1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
		}
		// a puff of pale dust at the base, gone in a few frames
		for range 4 {
			s := R.Sign()
			x, vx, vy, life, size := s*R.Range(5, 8), s*R.Range(25, 50), -R.Range(2, 6), R.Range(0.15, 0.25), R.Range(1, 1.8)
			ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, C[5], size, 0, 0)
		}
		lights.Add(0, -eggZY-10, 40, G[1], 0.9, 0.55)
		lights.Add(0, -eggZY-2, 18, pixelfx.White, 0.9, 0.12)
		cupV = R.Sign() * 3
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// each rock opens more crack: a tick of light, a chip or two of shell, a puff at the base
			for i, w := range eggWobbles {
				if !done[i] && t >= w[0]+0.05 {
					done[i] = true
					lx := 2.0
					if i == 1 {
						lx = -3
					}
					fi := float64(i)
					lights.Add(lx, -14+fi, 8+fi*2, G[1], 0.25+fi*0.08, 0.18)
					for range 2 + i*2 {
						x, y, vx, vy := R.Range(-5, 5), -R.Range(8, 17), R.Range(-20, 20), -R.Range(10, 30)
						life, col, size := R.Range(0.25, 0.5), pixelfx.Pick(R, wobbleStarCols), R.Range(0.6, 1.4)
						ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
					}
					for range i + 1 {
						x, y, vx, vy := R.Range(-4, 4), -R.Range(10, 16), R.Range(-50, 50), -R.Range(40, 90)
						life, ang, va, s := R.Range(0.5, 0.8), R.Range(0, tau), R.Range(-20, 20), R.Range(1, 1.6)
						bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x, Y: y, VX: vx, VY: vy, G: 0.9, Drag: 0.5, Bounce: 0.3, Life: life, A: ang, VA: va, S: s, C: C[4]})
					}
					x := R.Sign() * R.Range(5, 7)
					vx := R.Sign() * R.Range(8, 18)
					vy := -R.Range(1, 3)
					life := R.Range(0.15, 0.25)
					size := R.Range(0.8, 1.4)
					ps.Spawn(pixelfx.Dust, x, -0.5, vx, vy, life, C[5], size, 0, 0)
				}
			}
			if !popped && t >= eggPop {
				pop()
			}
			// light keeps welling up out of the empty cup for a moment: something came out of there
			if popped && t < eggPop+0.6 && R.Chance(0.35) {
				x, vx, vy := R.Range(-4, 4), R.Range(-8, 8), -R.Range(40, 80)
				life := math.Min(R.Range(0.4, 0.8), dur-0.1-t)
				col := pixelfx.Pick(R, moteCols)
				ps.Spawn(pixelfx.Mote, x, -eggZY-1, vx, vy, life, col, 0.7, 0, 0)
			}
			if popped && !crumbled && t >= crumble {
				crumbled = true
				breakUp(func(x, y int) bool { return y < zigAt(x) }, 4,
					func(cx, _ float64) (float64, float64) { return cx * R.Range(4, 8), -R.Range(40, 80) },
					func() float64 { return R.Range(0.38, 0.48) },
				)
				for range 3 {
					x := R.Range(-6, 6)
					vx := R.Sign() * R.Range(10, 25)
					vy := -R.Range(2, 6)
					life, size := R.Range(0.15, 0.25), R.Range(1, 1.6)
					ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, C[5], size, 0, 0)
				}
			}
			if popped {
				cupV += (-cupA*90 - cupV*4) * dt
				cupA += cupV * dt
			}
			for i := len(pieces) - 1; i >= 0; i-- {
				q := &pieces[i]
				q.life -= dt
				if q.life <= 0 {
					pieces = append(pieces[:i], pieces[i+1:]...)
					continue
				}
				if q.rest {
					continue
				}
				q.vy += 900 * dt
				q.vx -= q.vx * 0.6 * dt
				q.x += q.vx * dt
				q.y += q.vy * dt
				q.a += q.va * dt
				g := -q.r * 0.45
				if q.y > g && q.vy > 0 {
					q.y = g
					if q.vy > 80 {
						q.vy *= -0.35
						q.vx *= 0.6
						q.va *= 0.5
					} else {
						q.rest = true
						q.vx, q.vy = 0, 0
					}
				}
			}
			bits.Update(dt)
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// a soft contact shadow under the egg, shifting as it rocks
			if !crumbled {
				sx, k := -angle(t)*8, 1.0
				if popped {
					sx, k = 0, 1-pixelfx.Smooth(eggPop, crumble, t)*0.4
				}
				for y := -1.0; y <= 1; y++ {
					for x := -10.0; x <= 10; x++ {
						dx := (x + 0.5 - sx) / 9.5
						dy := (y + 0.5) / 1.8
						d := dx*dx + dy*dy
						if d < 1 {
							p.Blend(x, y, C[0], 0.5*k*(1-d))
						}
					}
				}
			}
			// warm light on the floor as it leaks out
			g := inner(t)
			if g > 0.05 {
				p.Glow(0, -3, 22, G[0], 0.18*math.Min(1.2, g))
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			st := stage(t)
			a := angle(t)
			if popped {
				a = cupA * 0.03
			}
			ca, sa, g := math.Cos(a), math.Sin(a), inner(t)
			if !crumbled {
				p.Shade(-12, -eggEH-4, 12, 1, func(x, y float64, out *pixelfx.Out) uint32 {
					ux, uy := int(math.Floor(x*ca+y*sa)), int(math.Floor(-(-x*sa + y*ca)))
					e := at[eggKey{ux, uy}]
					inside := e != nil && !(popped && uy >= zigAt(ux))
					if !inside {
						// a dark outline holds the egg's shape
						n := false
						for _, d := range dirs {
							f := at[eggKey{ux + d[0], uy + d[1]}]
							if f != nil && !(popped && uy+d[1] >= zigAt(ux+d[0])) {
								n = true
								break
							}
						}
						if n && !(popped && uy >= zigAt(ux) && uy <= zigAt(ux)+1) {
							out.C = C[0]
						}
						return 0
					}
					cs, hasCrack := crack[eggKey{ux, uy}]
					if popped {
						// the cup: its broken rim burns with the light inside, cooling as it fades
						if uy >= zigAt(ux)-1 {
							switch {
							case g > 0.9:
								out.C = pixelfx.White
							case g > 0.4:
								out.C = G[2]
							case g > 0.12:
								out.C = G[1]
							default:
								out.C = C[4]
							}
							out.K = 0.25 * g
							out.L = G[1]
						} else {
							out.C = e.c
						}
						return 0
					}
					if hasCrack && cs < st {
						// an open crack: deep orange light in the gap, flashing white the moment it opens
						fresh := t-eggWobbles[cs][0] < 0.17
						if fresh {
							out.C = pixelfx.White
						} else if pixelfx.Rand(float64(ux), float64(uy), float64(pixelfx.ToInt32(t*20))) < 0.25+0.1*float64(st) {
							out.C = G[1]
						} else {
							out.C = G[0]
						}
						out.K = 0.14 + 0.06*float64(st)
						out.L = G[1]
						return 0
					}
					// the lip of each crack falls into shadow below it, so the gap reads as a gap
					c1, ok1 := crack[eggKey{ux, uy + 1}]
					c2, ok2 := crack[eggKey{ux + 1, uy}]
					if (ok1 && c1 < st) || (ok2 && c2 < st) {
						if e.lit > 0.3 {
							out.C = C[2]
						} else {
							out.C = C[1]
						}
						return 0
					}
					// light shows through the shell in a halo round the cracks, and the whole egg warms a little
					dn := 9.0
					if st > 0 {
						dn = near[st-1][eggKey{ux, uy}]
					}
					k := math.Min(1, g*1.5)
					base := e.c
					if st > 0 && e.c != SPK {
						base = pixelfx.Mix(e.c, G[0], 0.05*float64(st)*k)
					}
					switch {
					case dn < 1.6:
						w := 0.2
						if st >= 2 {
							w = 0.32
						}
						out.C = pixelfx.Mix(base, G[1], w*k)
					case dn < 2.7 && st >= 2:
						out.C = pixelfx.Mix(base, G[1], 0.14*k)
					default:
						out.C = base
					}
					return 0
				})
			}
			// the pop: the opening floods white, and light rays fan up out of it
			q := t - eggPop
			if popped && q < 1.2 && !crumbled && g > 0.22 {
				oy, rx := -float64(eggZY)+0.5, 7.0
				for y := -2.0; y <= 2; y++ {
					for x := -rx; x <= rx; x++ {
						d := pixelfx.Hypot((x+0.5)/rx, (y+0.5)/2.2)
						if d > 1 {
							continue
						}
						if g < 0.6 && d > 0.75 {
							continue
						}
						var col uint32
						switch {
						case g > 1:
							if d < 0.7 {
								col = pixelfx.White
							} else {
								col = G[2]
							}
						case g > 0.6:
							if d < 0.5 {
								col = G[2]
							} else {
								col = G[1]
							}
						default:
							if d < 0.45 {
								col = G[1]
							} else {
								col = G[0]
							}
						}
						p.Px(x, oy+y, col)
					}
				}
				p.Glow(0, oy, 16, G[1], 0.6*g)
				if q < 0.65 {
					rk := q / 0.65
					L := 36 * pixelfx.Ease.OutCubic(pixelfx.Clamp(q/0.1)) * (1 - rk*0.6)
					for i := range 7 {
						an := -pi/2 + float64(i-3)*0.24 + 0.05*math.Sin(t*6+float64(i))
						lenMul, w := 1.0, 2
						if i%2 != 0 {
							lenMul, w = 0.65, 1
						}
						length := L * lenMul
						x1, y1 := math.Cos(an)*length, oy+math.Sin(an)*length
						for j := range w {
							off := float64(j)*0.8 - 0.4
							p.LineFn(off, oy-1, x1+off, y1, func(u float64) (uint32, float64) {
								col := G[1]
								if u < 0.3 {
									col = pixelfx.White
								} else if u < 0.65 {
									col = G[2]
								}
								return col, (1 - u*u) * (1.1 - rk)
							})
						}
						p.GlowLine(0, oy, x1, y1, 5, G[1], 0.35*(1-rk))
					}
				}
				if q < 0.1 {
					p.Glow(0, oy-4, 30, pixelfx.White, 0.6*(1-q/0.1))
				}
				// the light that lingers over the cup
				p.GlowLine(0, oy, 0, oy-26, 8, G[1], 0.25*g)
			}
			// the inside glow through the shell before it pops
			if !popped && g > 0 {
				p.Glow(0, -eggEH*0.55, 7+2*float64(st), G[1], 0.05*g)
			}
			// pieces of shell: their lit outsides, their broken edges hot at first
			for i := range pieces {
				s := &pieces[i]
				c2, s2, f, r, hot := math.Cos(s.a), math.Sin(s.a), math.Min(1, s.life/0.4), math.Ceil(s.r), t-s.born < 0.35
				for py := -r; py <= r; py++ {
					for px := -r; px <= r; px++ {
						ux, uy := int(pixelfx.Round(px*c2+py*s2)), int(pixelfx.Round(-px*s2+py*c2))
						m, ok := s.pxMap[eggKey{ux, uy}]
						if ok && pixelfx.Rand(float64(ux), float64(uy), s.id) < f {
							col := m.c
							if m.edge {
								if hot {
									col = G[2]
								} else {
									col = C[4]
								}
							}
							p.Px(math.Floor(s.x)+px, math.Floor(s.y)+py, col)
						}
					}
				}
				if hot {
					p.Glow(s.x, s.y, 6, G[1], 0.3)
				}
			}
			bits.Draw(p)
			if popped && q < 0.9 {
				pixelfx.Sparkle(p, -3, -eggZY-6, pixelfx.Round(4*(1-q/0.9)), pixelfx.White, G[1], 0.4)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
