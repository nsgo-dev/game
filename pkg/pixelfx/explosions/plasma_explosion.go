// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/plasma-explosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The release (flash, rings, flare, light) and the arcs are adapted from Sprite Fusion's open-source energy (https://www.spritefusion.com/pixel-art-vfx/energy), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// PlasmaExplosion is a blue-violet energy blast with no smoke: a white flash, a ball of plasma swelling into a thinning shell, shock rings in and out of depth, lightning arcs crackling off it. It is registered as "explosions/plasma-explosion".
var PlasmaExplosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Plasma explosion",
		Desc:     "A blue-violet energy blast with no smoke: a white flash, a ball of plasma swelling into a thinning shell, shock rings in and out of depth, lightning arcs crackling off it.",
		Tags:     []string{"explosion", "plasma", "energy", "electric", "magic", "sci-fi", "one-shot"},
		Size:     [2]int{plasmaW, plasmaH},
		Origin:   [2]int{plasmaOX, plasmaOY},
		Duration: 1.7,
		FPS:      30,
		BG:       "dark",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a0828", "#1a1c6e", "#2c3cc0", "#4a7cf0", "#7cc0ff", "#c8ecff", "#ffffff")},
			{Name: "accent", Default: pixelfx.Palette("#1c0a30", "#4a1a8c", "#8a3ce0", "#c48cff", "#f0d8ff")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	New: newPlasmaExplosion,
}

const plasmaW, plasmaH, plasmaOX, plasmaOY = 224, 200, 112, 100

func init() { pixelfx.Register("explosions/plasma-explosion", PlasmaExplosion) }

var plasmaBall = 0.55

func newPlasmaExplosion(o *pixelfx.Options) pixelfx.Instance {
	C, V, R := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("accent"), 5), pixelfx.NewRng(o.Seed())
	const OX, OY, W, H = plasmaOX, plasmaOY, plasmaW, plasmaH
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Sparks: []uint32{C[6], C[5], C[4], V[3]}}), &pixelfx.Lights{}
	lights.Add(0, 0, 96, C[4], 1, 0.6)
	lights.Add(0, 0, 40, pixelfx.White, 0.9, 0.1)
	lights.Add(0, 0, 70, V[2], 0.5, 1.1)
	for range 46 {
		a, sp := R.Range(0, tau), R.Range(140, 360)
		ps.Spawn(pixelfx.Mote, math.Cos(a)*4, math.Sin(a)*4, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.2, 0.42), pixelfx.Pick(R, []uint32{C[6], C[5], C[4], V[3]}), R.Range(0.7, 1.1), 0, 0)
	}
	for range 26 {
		a, sp := R.Range(0, tau), R.Range(40, 140)
		ps.Spawn(pixelfx.Flake, math.Cos(a)*10, math.Sin(a)*10, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.8, 1.5), pixelfx.Pick(R, []uint32{C[5], V[3], V[4]}), R.Range(0.6, 1.6), 0, 0)
	}
	seed, t := o.Seed(), 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the shell sheds sparkles and motes as it thins
			if t > 0.12 && t < 0.9 && R.Chance(30*dt) {
				a, r := R.Range(0, tau), plasmaShell(t)
				ps.Spawn(pixelfx.StarKind, math.Cos(a)*r, math.Sin(a)*r, math.Cos(a)*R.Range(10, 40), math.Sin(a)*R.Range(10, 40), R.Range(0.3, 0.6), pick(R.Chance(0.5), C[5], V[4]), R.Range(0.5, 1.8), 0, 0)
			}
			ps.Update(dt)
			cull(ps, -OX+3, -OY+3, W-OX-3, H-OY-3)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the tilted ring's far half, behind the ball
			plasmaTilted(p, t, C, V, true)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the ball: a white heart, cyan and blue bands, a violet rim boiling at the edge; it swells, then hollows out
			if t < plasmaBall {
				u, r := t/plasmaBall, plasmaShell(t)
				hollow, boil := pixelfx.Smooth(0.35, 1, u), math.Floor(t*20)
				p.Shade(-r-3, -r-3, r+3, r+3, func(x, y float64, out *pixelfx.Out) uint32 {
					d, a := pixelfx.Hypot(x, y), math.Atan2(y, x)
					edge := r * (1 + 0.1*pixelfx.Noise(a*3+boil*0.7, 3))
					if d > edge {
						return 0
					}
					v := d / edge
					if v < hollow*0.92 {
						return 0
					}
					// turbulent bands swirling round the heart, and bright filaments twisting through them
					n := pixelfx.Noise(a*4+d*0.25+boil*0.5, 5)
					w, fil := v+n*0.14, math.Abs(math.Sin(a*5+d*0.32-t*14+n*2.5)) < 0.1
					out.K = 0.25 * (1 - u)
					switch {
					case v > 0.9:
						return V[2]
					case fil && v > 0.3:
						return pick(u < 0.5, pixelfx.White, C[5])
					case w < 0.32-u*0.25:
						return pixelfx.White
					case w < 0.52:
						return C[5]
					case w < 0.72:
						return C[4]
					case w < 0.86:
						return pick(u < 0.5, C[3], V[3])
					}
					return V[2]
				})
				p.Glow(0, 0, r*1.6+6, C[4], 0.6*(1-u))
			}
			if t < 0.07 {
				p.Circle(0, 0, 9-t*60, pixelfx.White, 1)
			}
			if t < 0.25 {
				pixelfx.Flare(p, 0, 0, 52*math.Pow(1-t/0.25, 1.3), 1-t/0.25, C)
			}
			// shock rings: a fast bright one, a slower violet one
			if u1 := t / 0.42; u1 < 1 {
				p.Ring(0, 0, 8+84*pixelfx.Ease.OutCubic(u1), math.Max(1, 3*(1-u1)), pick(u1 < 0.25, pixelfx.White, C[5]), 1.15-u1)
				p.GlowRing(0, 0, 8+84*pixelfx.Ease.OutCubic(u1), 5, C[4], 0.35*(1-u1))
			}
			if u2 := (t - 0.06) / 0.6; u2 > 0 && u2 < 1 {
				p.Ring(0, 0, 6+60*pixelfx.Ease.OutCubic(u2), math.Max(1, 2*(1-u2)), V[3], 1.1-u2)
			}
			// arcs: lightning crackling out of the heart, re-struck every few frames, shorter as the charge runs out
			left, strike := 1-pixelfx.Smooth(0.15, 0.85, t), math.Floor(t*18)
			if left > 0 {
				r := pixelfx.RngOf(seed, strike, 17)
				n := 2 + pixelfx.Round(4*left)
				for i := 0; float64(i) < n; i++ {
					a, L := r.Range(0, tau), r.Range(26, 58)*(0.45+0.55*left)
					path := pixelfx.Bolt(r, 0, 0, math.Cos(a)*L, math.Sin(a)*L, 0.28, 4)
					plasmaArc(p, path, C, V, 0.8*left)
					if r.Chance(0.5) {
						for _, f := range pixelfx.Forks(r, path, pixelfx.ForkOpts{Count: 1, Len: 14, Jag: 0.35, Spread: 0.9}) {
							plasmaArc(p, f, C, V, 0.5*left)
						}
					}
				}
				// and little arcs crawling over the shell
				rs := plasmaShell(t)
				for range 3 {
					a := r.Range(0, tau)
					b := a + r.Range(0.3, 0.6)
					plasmaArc(p, pixelfx.Bolt(r, math.Cos(a)*rs, math.Sin(a)*rs, math.Cos(b)*rs, math.Sin(b)*rs, 0.4, 3), C, V, 0.6*left)
				}
			}
			plasmaTilted(p, t, C, V, false)
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}

// plasmaShell returns the ball's radius: punched out fast, then easing.
func plasmaShell(t float64) float64 {
	return 5 + 30*pixelfx.Ease.OutCubic(pixelfx.Clamp(t/plasmaBall))
}

// plasmaTilted draws a ring on a tilted plane racing out round the blast: far half behind, near half in front.
func plasmaTilted(p *pixelfx.Painter, t float64, C, V []uint32, back bool) {
	u := (t - 0.03) / 0.5
	if u <= 0 || u >= 1 {
		return
	}
	pixelfx.Band(p, 0, 4, 12+86*pixelfx.Ease.OutCubic(u), 0.28, math.Max(1, 4*(1-u)), 1.15-u, []uint32{V[1], V[2], C[4], C[5]}, func(g float64) bool { return (math.Sin(g) < 0) == back }, u < 0.15)
}

// plasmaArc draws one arc: a soft glow along it, a cyan halo line, a white core.
func plasmaArc(p *pixelfx.Painter, path [][2]float64, C, V []uint32, k float64) {
	if k <= 0.05 {
		return
	}
	for i := 1; i < len(path); i++ {
		ax, ay, bx, by := path[i-1][0], path[i-1][1], path[i][0], path[i][1]
		p.GlowLine(ax, ay, bx, by, 5, V[2], 0.18*k)
		p.Line(ax+1, ay, bx+1, by, C[4], k*0.8)
		p.Line(ax, ay, bx, by, pick(float64(i) < float64(len(path))*0.6, pixelfx.White, C[5]), math.Min(1, k*1.4))
	}
}
