// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/explosions/dirt-explosion.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The earth recipe (grit thrown in a fan, rocks that bounce and kick up dust, a dust column and dust rolling out along the floor) is adapted from Sprite Fusion's open-source explosion (https://www.spritefusion.com/pixel-art-vfx/explosion), MIT License, Copyright (c) 2026 Hugo Duprez.

package explosions

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// DirtExplosion is a buried charge going off: a flash under the floor, a V of dirt clods and grit thrown high, a brown dust plume that billows and settles, and a crater left behind. It is registered as "explosions/dirt-explosion".
var DirtExplosion = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Dirt explosion",
		Desc:     "A buried charge goes off: a flash under the floor, a V of dirt clods and grit thrown high, a brown dust plume that billows and settles, and a crater left behind.",
		Tags:     []string{"explosion", "dirt", "earth", "dust", "debris", "ground", "one-shot"},
		Size:     [2]int{dirtW, dirtH},
		Origin:   [2]int{dirtOX, dirtOY},
		Duration: dirtDuration,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1c1610", "#33281c", "#4f3f2c", "#6e5a40", "#927a58", "#b89f78", "#dcc8a2")},
			{Name: "fire", Default: pixelfx.Palette("#4a1712", "#c9281f", "#f2561d", "#ff8a2a", "#ffc23d", "#ffe26b", "#fff3a6", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	New: newDirtExplosion,
}

const dirtW, dirtH, dirtOX, dirtOY = 260, 240, 130, 206

func init() { pixelfx.Register("explosions/dirt-explosion", DirtExplosion) }

// a variable, so sums with it round as pixelfx's do
var dirtDuration = 3.6

func newDirtExplosion(o *pixelfx.Options) pixelfx.Instance {
	E, F, R := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("fire"), 8), pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	const W, H, OX, OY = dirtW, dirtH, dirtOX, dirtOY
	const X0, X1, Y0, Y1 = -OX + 3, W - OX - 3, -OY + 3, H - OY - 3
	clod := stoneCols{light: E[5], body: E[3], shade: E[2]}
	DUST, SOIL, DARK := pixelfx.Mix(E[4], pixelfx.RGB(150, 140, 150), 0.2), pixelfx.Mix(E[3], pixelfx.RGB(90, 80, 90), 0.2), pixelfx.Mix(E[2], pixelfx.RGB(60, 52, 60), 0.3)
	ps, lights := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{F[6], F[5], F[3], F[2]}}), &pixelfx.Lights{}
	clods := pixelfx.NewBits(floor)
	t := 0.0

	lights.Add(0, -10, 56, F[4], 0.7, 0.22)
	lights.Add(0, -4, 26, pixelfx.White, 0.6, 0.06)
	// the charge: a few sparks out of the hole
	for range 14 {
		a, sp := -pi/2+R.Range(-0.7, 0.7), R.Range(160, 320)
		ps.Spawn(pixelfx.Spark, 0, -2, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(0.15, 0.35), pixelfx.White, 1, 0, 0)
	}
	// the fountain: grit in a V, clods thrown high, a few pebbles
	for range 300 {
		side := R.Sign()
		a := -pi/2 + side*R.Range(0.05, 0.62)
		sp := R.Range(150, 470) * (1 - 0.3*math.Abs(a+pi/2))
		ps.Spawn(pixelfx.Grit, R.Range(-4, 4), -1, math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.2, 2.2), pixelfx.Pick(R, []uint32{E[1], E[2], E[2], E[3], E[3], E[4], E[5]}), pick(R.Chance(0.45), 2.0, 1), R.Range(0, 0.06), 0)
	}
	for i := range 30 {
		side := R.Sign()
		a, sp := -pi/2+side*R.Range(0.1, 0.6), R.Range(200, 430)
		var b pixelfx.Bit
		b.Kind = pixelfx.Chunk
		b.X = R.Range(-5, 5)
		b.Y = -2
		b.VX, b.VY = math.Cos(a)*sp, math.Sin(a)*sp
		b.G, b.Drag, b.Bounce = 1, 0.35, 0.25
		b.Life = R.Range(2.2, 3)
		b.A = R.Range(0, tau)
		b.VA = R.Range(-12, 12)
		if i < 12 {
			b.S = R.Range(2.6, 3.8)
		} else {
			b.S = R.Range(1.4, 2.4)
		}
		clods.Add(b)
	}
	// the dust: a plume punched straight up, two lobes riding the V, and a skirt rolling out along the floor
	for range 22 {
		ps.Spawn(pixelfx.Smoke, R.Range(-7, 7), -R.Range(2, 12), R.Range(-25, 25), -R.Range(70, 180), R.Range(1.8, 2.6), pick(R.Chance(0.6), SOIL, DUST), R.Range(9, 14), R.Range(0, 0.14), pixelfx.Big)
	}
	for range 14 {
		side := R.Sign()
		a, sp := -pi/2+side*R.Range(0.35, 0.6), R.Range(110, 210)
		ps.Spawn(pixelfx.Smoke, side*R.Range(2, 8), -R.Range(2, 8), math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1.6, 2.4), pick(R.Chance(0.5), SOIL, DUST), R.Range(8, 12), R.Range(0.02, 0.1), pixelfx.Big)
	}
	for range 16 {
		s := R.Sign()
		ps.Spawn(pixelfx.Smoke, s*R.Range(4, 16), -R.Range(1, 5), s*R.Range(70, 200), -R.Range(2, 12), R.Range(1.4, 2.2), DUST, R.Range(6, 9), R.Range(0.02, 0.1), pixelfx.Big)
	}
	// dark soil thrown up with it, heavier and lower
	for range 8 {
		ps.Spawn(pixelfx.Smoke, R.Range(-8, 8), -R.Range(2, 8), R.Range(-40, 40), -R.Range(40, 110), R.Range(1.2, 1.8), DARK, R.Range(6, 9), R.Range(0, 0.05), 0)
	}

	band := func(p *pixelfx.Painter, keep func(float64) bool) {
		if k := t / 0.45; k < 1 {
			pixelfx.Band(p, 0, 0.5, 90*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 3*(1-k)), 1.1-k, []uint32{E[2], E[4], E[5], E[6]}, keep, false)
		}
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			for i := range clods.List {
				clods.List[i].PVY = clods.List[i].VY
			}
			clods.Update(dt)
			// clods thud down: a puff of dust and a spray of crumbs where they land, the big ones break up (Dusted marks a clod that has thudded)
			for i := len(clods.List) - 1; i >= 0; i-- {
				b := &clods.List[i]
				if b.X < X0+4 || b.X > X1-4 {
					clods.List = slices.Delete(clods.List, i, i+1)
					continue
				}
				landed := (b.PVY > 0 && b.VY <= 0) || (b.Rest && !b.Dusted)
				if !landed || b.PVY < 80 {
					if b.Rest {
						b.Dusted = true
					}
					continue
				}
				b.Dusted = b.Rest
				for j := 0; float64(j) < 2+pixelfx.Round(b.S); j++ {
					ps.Spawn(pixelfx.Dust, b.X+R.Range(-2, 2), -0.5, R.Sign()*R.Range(15, 55), -R.Range(4, 22), R.Range(0.35, 0.7), DUST, R.Range(1.6, 1.2+b.S), 0, 0)
				}
				for j := 0; float64(j) < pixelfx.Round(b.S*3); j++ {
					ps.Spawn(pixelfx.Grit, b.X, -1, R.Range(-70, 70), -R.Range(40, 140), R.Range(0.8, 1.4), pixelfx.Pick(R, []uint32{E[2], E[3], E[4]}), 1, 0, 0)
				}
				if b.S > 2.2 && b.PVY > 200 {
					b.S *= 0.6
				}
			}
			ps.Update(dt)
			cull(ps, X0, Y0, X1, Y1)
			fadeEdges(ps, X0, X1, 24, Y1)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			crater(p, E, 0, 28, pixelfx.Ease.OutCubic(math.Min(1, t/0.12)), 1-pixelfx.Smooth(dirtDuration-0.7, dirtDuration-0.1, t))
			band(p, farHalf)
			drawDirt(p, ps, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			band(p, nearHalf)
			// the flash out of the ground: a squat dome of white-hot air for a frame or two
			if t < 0.09 {
				dome(p, F, 0, 15*(1-t*3), 1-t/0.09)
			}
			for i := range clods.List {
				rock(p, &clods.List[i], clod, F)
			}
			drawDirt(p, ps, pixelfx.LayerFront)
			pool(p, 0, 1, 60, 10, F[3], 0.5*math.Max(0, 1-t/0.3))
			lights.Draw(p)
		},
	}
}
