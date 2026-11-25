// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/hit-spark.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the two-tone spark spray and the light pop from Sprite Fusion's open-source impact (https://www.spritefusion.com/pixel-art-vfx/impact), MIT License, Copyright (c) 2026 Hugo Duprez.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// HitSpark is the everyday hit: a sharp, lopsided cross flare with one long ray along the blow round a white diamond core, then a cone of sparks and hot chips that fall and bounce. It is registered as "impacts/hit-spark".
var HitSpark = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Hit spark",
		Desc:     "The everyday hit: a sharp, lopsided cross flare with one long ray along the blow round a white diamond core, then a cone of sparks and hot chips that fall and bounce.",
		Tags:     []string{"impact", "hit", "spark", "combat", "directional", "one-shot"},
		Size:     [2]int{136, 80},
		Origin:   [2]int{44, 64},
		Duration: 0.5,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "height", Default: pixelfx.Num(20)},
			{Name: "aim", Default: pixelfx.Num(0.1)},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newHitSpark,
}

func init() { pixelfx.Register("impacts/hit-spark", HitSpark) }

// sparkRay draws one ray of the cross: fat at the root, a hair at the tip, white cooling outward; from (0..1) starts it part of the way along.
func sparkRay(p *pixelfx.Painter, x, y, a, L, w float64, C []uint32, from float64) {
	c, s := math.Cos(a), math.Sin(a)
	for d := from * L; d <= L; d += 0.5 {
		u := d / L
		hw := w * math.Pow(1-u, 1.6)
		var col uint32
		switch {
		case u < 0.3:
			col = pixelfx.White
		case u < 0.62:
			col = C[5]
		case u < 0.88:
			col = C[4]
		default:
			col = C[3]
		}
		p.Px(x+c*d, y+s*d, col)
		if hw > 0.45 {
			for o := 0.5; o <= hw; o += 0.5 {
				for _, e := range [2]float64{-o, o} {
					ec := C[4]
					if u < 0.3 && o < 1 {
						ec = C[5]
					}
					p.Px(x+c*d-s*e, y+s*d+c*e, ec)
				}
			}
		}
	}
}

// sparkDiamond draws the core: a crisp white diamond with a pale rim.
func sparkDiamond(p *pixelfx.Painter, x, y, r float64, C []uint32) {
	for dy := -r - 1; dy <= r+1; dy++ {
		for dx := -r - 1; dx <= r+1; dx++ {
			m := math.Abs(dx) + math.Abs(dy)
			if m <= r {
				p.Px(x+dx, y+dy, pixelfx.White)
			} else if m == r+1 {
				p.Px(x+dx, y+dy, C[3])
			}
		}
	}
}

type sparkRayDef struct{ a, L, w float64 }

func newHitSpark(o *pixelfx.Options) pixelfx.Instance {
	C, R, X, Y, A := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed()), 0.0, -o.Num("height"), o.Num("aim")
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{C[6], C[5], C[4], C[2]}})
	lights := &pixelfx.Lights{}
	// the cross: a long thin ray along the blow, a stub behind, two unequal arms across, and a glint between
	tilt := R.Range(-0.08, 0.08)
	rays := make([]sparkRayDef, 0, 5)
	rays = append(rays, sparkRayDef{A + tilt, R.Range(33, 37), 2.2})
	rays = append(rays, sparkRayDef{A + pi + tilt, R.Range(8, 10), 2.2})
	rays = append(rays, sparkRayDef{A - pi/2 + tilt, R.Range(15, 18), 2.6})
	rays = append(rays, sparkRayDef{A + pi/2 + tilt, R.Range(10, 12), 2.3})
	rays = append(rays, sparkRayDef{A - pi/4 + tilt, R.Range(5, 7), 1})

	lights.Add(X, Y, 38, C[4], 0.35, 0.18)
	lights.Add(X+math.Cos(A)*10, Y+math.Sin(A)*10, 22, C[5], 0.45, 0.1)
	lights.Add(X, -2, 26, C[3], 0.22, 0.22)
	// a cone of sparks sprayed on along the blow: they fall, bounce off the floor and die
	for i := range 9 {
		a := A + R.Range(-0.6, 0.6) - 0.15
		v := R.Range(170, 360)
		life := R.Range(0.25, 0.45)
		delay := 0.0
		if i > 6 {
			delay = R.Range(0.01, 0.03)
		}
		ps.Spawn(pixelfx.Spark, X+1, Y, math.Cos(a)*v, math.Sin(a)*v-40, life, pixelfx.White, 1, delay, 0)
	}
	// hot pixel chips knocked off, tumbling down to tick off the floor
	for range 6 {
		a := A + R.Range(-1.1, 0.5) - 0.3
		v := R.Range(70, 170)
		vy := math.Sin(a)*v - R.Range(30, 80)
		life := R.Range(0.35, 0.45)
		ps.Spawn(pixelfx.Grit, X+1, Y, math.Cos(a)*v, vy, life, pixelfx.Pick(R, []uint32{C[5], C[4], C[4], C[3]}), 1, 0, 0)
	}

	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			ps.Draw(p, pixelfx.LayerBoth)
			switch {
			case t < 0.025:
				// the flash: the whole cross round the diamond
				for _, r := range rays {
					sparkRay(p, X, Y, r.a, r.L, r.w, C, 0)
				}
				sparkDiamond(p, X, Y, 4, C)
			case t < 0.058:
				// reaching further and thinning, the diamond still crisp
				for _, r := range rays {
					sparkRay(p, X, Y, r.a, r.L*1.2, r.w*0.55, C, 0.12)
				}
				sparkDiamond(p, X, Y, 3, C)
			case t < 0.092:
				// only the long ray lingers, its root burning away
				r := rays[0]
				sparkRay(p, X, Y, r.a, r.L*1.3, 0.6, []uint32{C[0], C[1], C[2], C[3], C[3], C[4]}, 0.4)
			}
			pop := math.Exp(-t / 0.045)
			if pop > 0.02 {
				p.Glow(X, Y, 22, C[4], 0.3*pop)
				p.Glow(X, Y, 7, pixelfx.White, 0.15*pop)
				p.GlowLine(X, Y, X+math.Cos(A)*26, Y+math.Sin(A)*26, 4, C[5], 0.25*pop)
			}
			lights.Draw(p)
		},
	}
}
