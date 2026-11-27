// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/cartoon-pow.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package explosions

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// CartoonPow is a comic-book hit: a jagged two-tone burst star slams in with a shake and boiling edges, the word (POW! by default) drops in letter by letter, action lines fly, then it all snaps away. It is registered as "explosions/cartoon-pow".
var CartoonPow = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Cartoon POW",
		Desc:     "A comic-book hit: a jagged two-tone burst star slams in with a shake and boiling edges, POW! drops in letter by letter, action lines fly, then it all snaps away.",
		Tags:     []string{"cartoon", "comic", "pow", "hit", "impact", "text", "one-shot"},
		Size:     [2]int{powW, powH},
		Origin:   [2]int{powOX, powOY},
		Duration: 1.05,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0e14", "#6e1a12", "#c8361a", "#f47a24", "#ffc23c", "#fff07a", "#ffffff")},
			{Name: "text", Default: pixelfx.Text("POW!")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newCartoonPow,
}

const powW, powH, powOX, powOY = 184, 144, 92, 72

func init() { pixelfx.Register("explosions/cartoon-pow", CartoonPow) }

const powN = 14

// variables, so sums of them round as pixelfx's do
var powIn, powGo, powEnd = 0.05, 0.66, 0.8

type powSpike struct{ r, j float64 }

type powLine struct{ a, r, L, t0 float64 }

func newCartoonPow(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed())
	INK := C[0]
	const OX, OY, W, H = powOX, powOY, powW, powH
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[6], C[5], C[4], C[3]}})
	// the star's spikes: long and short in turn, each its own length, boiling a little every few frames
	spikes := make([]powSpike, powN*2)
	for i := range spikes {
		if i%2 != 0 {
			spikes[i].r = R.Range(0.5, 0.6)
		} else {
			spikes[i].r = R.Range(0.85, 1.05)
		}
		spikes[i].j = R.Next() * 100
	}
	lines := make([]powLine, 16)
	for i := range lines {
		l := &lines[i]
		l.a = ((float64(i) + R.Range(0.2, 0.8)) / 16) * tau
		l.r = R.Range(0.92, 1.08)
		l.L = R.Range(10, 17)
		l.t0 = R.Range(0, 0.06)
	}
	word := []rune(o.Text("text"))
	t, popped, burst := 0.0, false, false
	rad := make([]float64, len(spikes))
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if !burst && t >= powIn {
				burst = true
				for range 22 {
					a, sp := R.Range(0, tau), R.Range(180, 360)
					ps.Spawn(pixelfx.Spark, math.Cos(a)*14, math.Sin(a)*10, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.12, 0.3), pixelfx.White, 1, 0, 0)
				}
			}
			// it snaps away into a scatter of little stars
			if !popped && t >= powGo {
				popped = true
				for range 14 {
					a, sp := R.Range(0, tau), R.Range(60, 150)
					ps.Spawn(pixelfx.StarKind, math.Cos(a)*26, math.Sin(a)*18, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.25, 0.42), pixelfx.Pick(R, []uint32{C[6], C[5], C[4]}), R.Range(1.2, 2.4), 0, 0)
				}
			}
			ps.Update(dt)
			cull(ps, -OX+3, -OY+3, W-OX-3, H-OY-3)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			k := pixelfx.Clamp(t / powIn)
			// the slam: it punches in past full size, shudders and settles; then it squeezes and snaps away
			var s float64
			switch {
			case t < powIn:
				s = 0.25 + 0.95*pixelfx.Ease.OutCubic(k)
			case t < powGo:
				s = 1 + 0.18*math.Exp(-(t-powIn)*18)*math.Cos((t-powIn)*40)
			default:
				s = 1 - pixelfx.InBack(pixelfx.Clamp((t-powGo)/(powEnd-powGo)), 2)
			}
			shake := 0.0
			if t < 0.3 {
				shake = (1 - t/0.3) * 2.2
			}
			sx, sy := pixelfx.Round(math.Sin(t*97)*shake), pixelfx.Round(math.Cos(t*83)*shake)
			if s > 0.02 {
				// the burst star, in one pass: its edge at each angle runs straight from spike tip to notch. An inked edge, a yellow body with halftone dots on its shaded side, an inked orange heart and a yellow core
				boil, outer, rot, M := math.Floor(t*12), 50*s, -0.1, len(spikes)
				if t > powGo {
					rot += (t - powGo) * 3
				}
				for i, q := range spikes {
					rad[i] = q.r * (1 + (pixelfx.Rand(float64(i), boil, 7)-0.5)*0.08)
				}
				p.Shade(sx-outer-4, sy-outer-4, sx+outer+4, sy+outer+4, func(x, y float64, _ *pixelfx.Out) uint32 {
					dx, dy := x-sx, (y-sy)/0.82
					d := pixelfx.Hypot(dx, dy)
					u := math.Mod(math.Mod((math.Atan2(dy, dx)-rot)/tau, 1)+1, 1) * float64(M)
					i := int(math.Floor(u)) % M
					f := u - math.Floor(u)
					r := rad[i] + (rad[(i+1)%M]-rad[i])*f
					e := d / r
					switch {
					case e > outer+2.5:
						return 0
					case e > outer:
						return INK
					case e > outer*0.66+2:
						fx, fy := int(math.Floor(x)), int(math.Floor(y))
						if dx*0.6+dy*0.8 > outer*0.25 && (fx+fy*2)%4 == 0 && fy%2 == 0 {
							return C[4]
						}
						return C[5]
					case e > outer*0.66:
						return INK
					case e > outer*0.44:
						return C[3]
					}
					return C[4]
				})
				p.Glow(sx, sy, outer*1.3, C[5], 0.3*math.Min(1, s))
			}
			// action lines flying out, and a ring of impact
			for _, q := range lines {
				u := (t - powIn - q.t0) / 0.32
				if u <= 0 || u >= 1 {
					continue
				}
				r0, L, ca, sa := (46+22*pixelfx.Ease.OutCubic(u))*q.r, q.L*0.7*(1-u), math.Cos(q.a), math.Sin(q.a)*0.82
				p.LineFn(ca*r0, sa*r0, ca*(r0+L), sa*(r0+L), func(v float64) (uint32, float64) { return pick(v < 0.5, pixelfx.White, C[5]), 1 })
			}
			if ru := (t - powIn) / 0.24; ru > 0 && ru < 1 {
				p.Ring(0, 0, 30+34*pixelfx.Ease.OutCubic(ru), math.Max(1, 2.5*(1-ru)), pick(ru < 0.4, pixelfx.White, C[5]), 1.1-ru)
			}
			// the word: letters dropping in one after another, inked, with a deep red extrusion, then a squash as it goes
			const scale = 3.0
			big := pixelfx.TextOpts{Font: "big", Scale: scale}
			widths := make([]float64, len(word))
			sum := -scale
			for i, ch := range word {
				widths[i], _ = p.Measure(string(ch), big)
				sum = sum + widths[i] + scale
			}
			x := -sum / 2
			for i, ch := range word {
				fi := float64(i)
				u, away := (t-powIn-0.04-fi*0.05)/0.12, pixelfx.Clamp((t-powGo)/(powEnd-powGo-0.04))
				if u > 0 && away < 1 {
					drop := (1 - pixelfx.OutBack(pixelfx.Clamp(u), 2.4)) * -14
					y := pixelfx.Round(-11 + drop - fi*1.5 + sy + away*6)
					lx, a, str := pixelfx.Round(x+sx), 1-away, string(ch)
					// a thick ink edge, a deep red extrusion, a white face going warm toward its foot
					for d := 4.0; d >= 1; d-- {
						to := big
						if d == 4 {
							to.Outline = INK
						}
						p.Text(str, lx+d, y+d, pick(d >= 3, INK, C[1]), a, to)
					}
					for _, off := range [][2]float64{{-2, 0}, {2, 0}, {0, -2}, {0, 2}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
						p.Text(str, lx+off[0], y+off[1], INK, a, big)
					}
					p.TextFn(str, lx, y, func(_, gy int) uint32 {
						switch {
						case gy < 4:
							return C[6]
						case gy < 6:
							return C[5]
						}
						return C[4]
					}, a, big)
				}
				x += widths[i] + scale
			}
			ps.Draw(p, pixelfx.LayerBoth)
			if t > powIn && t < powIn+0.12 {
				pixelfx.Sparkle(p, sx+34, sy-22, 6*(1-(t-powIn)/0.12), C[6], C[5], 0.6)
			}
		},
	}
}
