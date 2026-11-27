// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/hex-sigil.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// HexSigil traces a cursed sigil in the air with a burning pen, pops its glyphs in one by one, then flares white and crumbles into falling embers. It is registered as "shadow/hex-sigil".
var HexSigil = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Hex sigil",
		Desc:     "A burning pen traces a cursed sigil in the air, its glyphs pop in one by one, then it flares white and crumbles into falling embers.",
		Tags:     []string{"shadow", "hex", "sigil", "rune", "curse", "magic", "circle", "one-shot"},
		Size:     [2]int{168, 128},
		Origin:   [2]int{84, 112},
		Duration: 4.2,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	New: newHexSigil,
}

func init() { pixelfx.Register("shadow/hex-sigil", HexSigil) }

// the sigil's centre and radii; the beats: traced, glyphs pop, holds, flares, crumbles
const (
	hexCY    = -50.0
	hexRO    = 28.0
	hexRI    = 22.0
	hexRC    = 6.0
	hexGlyph = 1.62
	hexFlare = 2.6
	hexBreak = 2.74
)

type hexPt struct {
	x, y, t float64
}

type hexStrokes struct {
	pts []hexPt
	end float64
}

// buildHexStrokes returns the strokes, in the order the pen draws them, as points with the moment the pen passes each.
func buildHexStrokes() hexStrokes {
	circle := func(r, a0 float64) [][2]float64 {
		n := int(math.Ceil(r * tau))
		out := make([][2]float64, n+1)
		for i := 0; i <= n; i++ {
			a := a0 + (float64(i)/float64(n))*tau
			out[i] = [2]float64{math.Cos(a) * r, hexCY + math.Sin(a)*r}
		}
		return out
	}
	paths := make([][][2]float64, 0, 5)
	paths = append(paths, circle(hexRO, -pi/2))
	// a seven-pointed star, drawn in one stroke, three points round each time
	star := make([][2]float64, 0, 8)
	for i := 0; i <= 7; i++ {
		a := -pi/2 + float64((i*3)%7)*(tau/7)
		star = append(star, [2]float64{math.Cos(a) * hexRI, hexCY + math.Sin(a)*hexRI})
	}
	paths = append(paths, star)
	paths = append(paths, circle(hexRI, -pi/2))
	paths = append(paths, circle(hexRC, pi/2))
	paths = append(paths, [][2]float64{{0, hexCY - 3.5}, {2, hexCY}, {0, hexCY + 3.5}, {-2, hexCY}, {0, hexCY - 3.5}})

	// sample every half pixel, timing the pen along it: the outer circle fast, the star with more weight
	speeds := [5]float64{420, 700, 550, 380, 300}
	var pts []hexPt
	t := 0.12
	for k, path := range paths {
		for i := 1; i < len(path); i++ {
			x0, y0 := path[i-1][0], path[i-1][1]
			x1, y1 := path[i][0], path[i][1]
			L := pixelfx.Hypot(x1-x0, y1-y0)
			for d := 0.0; d < L; d += 0.5 {
				pts = append(pts, hexPt{x: x0 + ((x1-x0)*d)/L, y: y0 + ((y1-y0)*d)/L, t: t + d/speeds[k]})
			}
			t += L / speeds[k]
		}
		t += 0.04
	}
	return hexStrokes{pts: pts, end: t}
}

type hexSeg struct {
	ax, ay, bx, by float64
}

type hexGlyphItem struct {
	x, y, t float64
	segs    []hexSeg
	popped  bool
}

// buildHexGlyphs builds seven small glyphs: a spine and two or three branches on a 3x3 grid of nodes.
func buildHexGlyphs(seed float64) []hexGlyphItem {
	out := make([]hexGlyphItem, 7)
	rr := (hexRO + hexRI) / 2
	for i := range 7 {
		r := pixelfx.RngOf(seed, float64(i), 31)
		segs := []hexSeg{{1, 0, 1, 2}}
		// the condition draws again on every pass, as pixelfx's loop does
		for k := 0; ; k++ {
			n := 2
			if r.Next() < 0.5 {
				n = 3
			}
			if k >= n {
				break
			}
			ax, ay := r.Int(0, 2), r.Int(0, 2)
			bx := math.Max(0, math.Min(2, ax+r.Int(-1, 1)))
			by := math.Max(0, math.Min(2, ay+r.Int(-1, 1)))
			if ax != bx || ay != by {
				segs = append(segs, hexSeg{ax, ay, bx, by})
			}
		}
		ang := -pi/2 + (float64(i)*tau)/7 + pi/7
		out[i] = hexGlyphItem{x: math.Cos(ang) * rr, y: hexCY + math.Sin(ang)*rr, segs: segs, t: hexGlyph + float64(i)*0.07}
	}
	return out
}

type hexLight struct {
	x, y, r, k, life, max float64
}

func newHexSigil(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	S := buildHexStrokes()
	G := buildHexGlyphs(o.Seed())
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[7], C[6], C[5]}})
	embers := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[7], C[6], C[5], C[4]}})
	pf := newPuffs(puffsOpts{
		rng:   R,
		flame: []uint32{pixelfx.White, C[7], C[6], C[5], C[4], C[3], C[2], C[1], C[0]},
		soot:  []uint32{pixelfx.Mix(C[3], C[2], 0.5), pixelfx.Mix(C[2], C[1], 0.5)},
		cap:   pixelfx.Mix(C[4], C[2], 0.5),
		rim:   []uint32{C[6], C[5], C[4]},
		floor: floor,
	})
	bits := pixelfx.NewBits(floor)
	var lights []hexLight
	t, piIdx := 0.0, 0
	flared, broke := false, false
	shardCols := []uint32{C[7], C[6], C[6], C[5]}
	pen := func() hexPt { return S.pts[min(len(S.pts)-1, piIdx)] }

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the pen: it spits sparks and drips burning embers as it writes
			for piIdx < len(S.pts)-1 && S.pts[piIdx+1].t <= t {
				piIdx++
			}
			if t > 0.12 && t < S.end {
				q := pen()
				if R.Chance(dt * 70) {
					ps.Spawn(pixelfx.Spark, q.x, q.y, R.Range(-70, 70), R.Range(-100, 20), R.Range(0.12, 0.35), pixelfx.White, 1, 0, 0)
				}
				if R.Chance(dt * 10) {
					embers.Spawn(pixelfx.Ember, q.x, q.y, R.Range(-12, 12), R.Range(5, 25), R.Range(0.5, 0.9), pixelfx.White, 1, 0, 0)
				}
			}
			// power drawn into the sigil while it's written and held: streaks racing in to its heart
			if t > 0.2 && t < hexFlare-0.05 && R.Chance(dt*(8+14*pixelfx.Smooth(0.2, hexFlare, t))) {
				a, D0, T := R.Range(0, tau), R.Range(40, 60), R.Range(0.22, 0.32)
				v := (D0 * 3) / (math.Exp(3*T) - 1)
				ps.Spawn(pixelfx.Streak, math.Cos(a)*D0, hexCY+math.Sin(a)*D0, -math.Cos(a)*v, -math.Sin(a)*v, T, pick(R.Chance(0.5), C[7], C[6]), 1, 0, 0)
			}
			for i := range G {
				g := &G[i]
				if !g.popped && t >= g.t {
					g.popped = true
					lights = append(lights, hexLight{x: g.x, y: g.y, r: 14, k: 0.7, life: 0.2, max: 0.2})
					for range 4 {
						ps.Spawn(pixelfx.Spark, g.x, g.y, R.Range(-60, 60), R.Range(-70, 20), R.Range(0.1, 0.25), pixelfx.White, 1, 0, 0)
					}
				}
			}
			if t >= hexFlare && !flared {
				flared = true
				lights = append(lights,
					hexLight{x: 0, y: hexCY, r: 96, k: 1.4, life: 0.45, max: 0.45},
					hexLight{x: 0, y: hexCY, r: 44, k: 1, life: 0.14, max: 0.14},
				)
				for range 24 {
					a, sp := R.Range(0, tau), R.Range(60, 130)
					ps.Spawn(pixelfx.Spark, math.Cos(a)*13, hexCY+math.Sin(a)*13, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.2, 0.45), pixelfx.White, 1, 0, 0)
				}
			}
			if t >= hexBreak && !broke {
				// it crumbles: every stroke breaks into burning fragments that tumble down and bounce
				broke = true
				// pieces drop nearly straight at first, so the sigil's shape lingers as it falls apart
				for i := 0; i < len(S.pts)-1; i += 7 {
					q, n := S.pts[i], S.pts[i+1]
					a := math.Atan2(n.y-q.y, n.x-q.x)
					vx := R.Range(-8, 8)
					vy := R.Range(-12, 6)
					g := R.Range(0.22, 0.45)
					life := R.Range(0.8, 1.2)
					va := R.Range(-3, 3)
					s := R.Range(2.5, 3.5)
					c := pixelfx.Pick(R, shardCols)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: q.x, Y: q.y, VX: vx, VY: vy, G: g, Drag: 0.6, Bounce: 0.25, Life: life, A: a, VA: va, S: s, C: c})
					if i%27 == 0 {
						embers.Spawn(pixelfx.Ember, q.x, q.y, R.Range(-10, 10), -R.Range(10, 40), R.Range(0.6, 1.1), pixelfx.White, 1, 0, 0)
					}
				}
				for _, g := range G {
					pf.spawn(pkSoot, g.x, g.y, R.Range(-6, 6), -R.Range(8, 18), R.Range(0.7, 1.1), R.Range(3, 5), 0, false)
				}
				for range 8 {
					pf.spawn(pkSoot, R.Range(-20, 20), hexCY+R.Range(-20, 20), R.Range(-8, 8), -R.Range(6, 16), R.Range(0.8, 1.2), R.Range(3, 6), R.Range(0, 0.15), false)
				}
			}
			for i := len(lights) - 1; i >= 0; i-- {
				lights[i].life -= dt
				if lights[i].life <= 0 {
					lights = slices.Delete(lights, i, i+1)
				}
			}
			bits.Update(dt)
			ps.Update(dt)
			embers.Update(dt)
			pf.update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			lk := 0.0
			if broke {
				lk = 0.3
			}
			pf.draw(p, pkSoot, &puffLight{x: 0, y: hexCY, k: lk, reach: 50})
			var k float64
			if t < hexBreak {
				k = pixelfx.Smooth(0.1, 0.6, t)
			} else {
				k = 1 - pixelfx.Smooth(hexBreak, hexBreak+0.6, t)
			}
			if k > 0 {
				p.Glow(0, hexCY, 52, C[4], 0.3*k)
			}
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if !broke {
				// the traced lines: white where the pen just passed, cooling to violet; pulsing as it holds; white in the flare
				hold := 0.0
				if t > S.end {
					hold = 0.5 + 0.5*math.Sin((t-S.end)*9)
				}
				white := t >= hexFlare
				for _, q := range S.pts {
					if q.t > t {
						break
					}
					age := t - q.t
					c := C[5]
					switch {
					case white || age < 0.05:
						c = pixelfx.White
					case age < 0.15:
						c = C[7]
					case age < 0.35 || hold > 0.75:
						c = C[6]
					}
					p.Px(q.x, q.y, c)
				}
				if white {
					for _, q := range S.pts {
						if pixelfx.ToInt32(q.x*2)%3 == 0 {
							p.Light(q.x, q.y, C[6], 0.5)
						}
					}
				}
				// glyphs: each pops in white and settles
				for _, g := range G {
					if t < g.t {
						continue
					}
					age := t - g.t
					col := C[6]
					switch {
					case white || age < 0.06:
						col = pixelfx.White
					case age < 0.2 || hold > 0.75:
						col = C[7]
					}
					for _, seg := range g.segs {
						p.Line(g.x-2+seg.ax*2, g.y-2+seg.ay*2, g.x-2+seg.bx*2, g.y-2+seg.by*2, col, 1)
					}
					if age < 0.18 {
						ringFast(p, g.x, g.y, 2+4*(age/0.18), 1, pick(age < 0.09, C[7], C[5]), 1)
					}
					p.Glow(g.x, g.y, 6, C[5], 0.25)
				}
				// the pen: a white-hot point with a little cross of light
				if t > 0.12 && t < S.end+0.05 {
					q := pen()
					pixelfx.Sparkle(p, q.x, q.y, 3+math.Sin(t*60), C[7], C[6], 0.6)
					p.Glow(q.x, q.y, 16, C[6], 0.55)
				}
			}
			// the flare: a burst ring as it goes white
			fu, bu := (t-hexFlare)/0.35, (t-hexFlare)/0.22
			if bu > 0 && bu < 1 {
				pixelfx.Sparkle(p, 0, hexCY, 18*(1-bu), C[7], C[5], 0.9*(1-bu))
			}
			if fu > 0 && fu < 1 {
				c := C[4]
				if fu < 0.4 {
					c = pixelfx.White
				} else if fu < 0.7 {
					c = C[6]
				}
				ringFast(p, 0, hexCY, hexRO+3+12*pixelfx.Ease.OutCubic(fu), math.Max(1, 3*(1-fu)), c, 1)
			}
			bits.Draw(p)
			pf.draw(p, pkFlame, nil)
			ps.Draw(p, pixelfx.LayerBoth)
			embers.Draw(p, pixelfx.LayerBoth)
			for _, l := range lights {
				q := l.life / l.max
				p.Glow(l.x, l.y, l.r*(0.6+0.4*q), pick(l.k > 1, C[5], C[6]), l.k*q*q)
			}
		},
	}
}
