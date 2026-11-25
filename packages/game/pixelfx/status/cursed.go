// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/cursed.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the soft lit smoke puffs and the shaded, outlined skull from Sprite Fusion's open-source poison (https://www.spritefusion.com/pixel-art-vfx/poison), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const (
	cursedDuration = 2.4
	cursedIn       = 0.5
	cursedHold     = 0.75
	cursedOut      = 1.85
	cursedGone     = 2.1
)

var (
	cursedHunch = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(3), pixelfx.HY.Is(1), pixelfx.FX.Is(-7), pixelfx.FY.Is(-33), pixelfx.BX.Is(8), pixelfx.BY.Is(-33), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	cursedSkull = bitmap([]string{
		"...#####...",
		".#########.",
		"###########",
		"##eee#eee##",
		"##eEe#eEe##",
		"##eee#eee##",
		"#####.#####",
		".####.####.",
		"..#######..",
		"..#.#.#.#..",
		"...#####...",
	}, 1, false)
	cursedTrailSpots = [4]struct {
		q    float64
		side float64
	}{
		{0.12, -1}, {0.1, 1}, {0.5, -1}, {0.48, 1},
	}
)

// Cursed is hexed: violet smoke rises off the body in winding trails, a dark stain creeps up from the feet with a glowing edge, the eyes burn, a skull glitches in overhead. It is registered as "status/cursed".
var Cursed = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Cursed",
		Desc:     "Hexed: violet smoke rises off the body in winding trails, a dark stain creeps up from the feet with a glowing edge, the eyes burn, a skull glitches in overhead.",
		Tags:     []string{"status", "curse", "hex", "dark", "debuff", "shadow", "loop"},
		Size:     [2]int{64, 84},
		Origin:   [2]int{32, 76},
		Duration: cursedDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c0612", "#1f0f2e", "#3a1a55", "#5e2a85", "#8a3fc0", "#b86ef0", "#e0b0ff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: cursedHunch},
			{T: 0.6, Pose: cursedHunch.With(pixelfx.Crouch.Is(5))},
			{T: 1.2, Pose: cursedHunch.With(pixelfx.Lean.Is(2))},
			{T: 1.8, Pose: cursedHunch.With(pixelfx.Crouch.Is(5))},
			{T: 2.4, Pose: cursedHunch},
		},
	},
	New: newCursed,
}

func init() { pixelfx.Register("status/cursed", Cursed) }

func cursedTide(t float64) float64 {
	return 0.3 + 0.12*math.Sin((t/cursedDuration)*tau)
}

func cursedReach(x, t, seed float64) float64 {
	return cursedTide(t) + 0.13*pixelfx.TileFbm(x*0.35, (t/cursedDuration)*4, 0, 4, seed, 2)
}

func cursedTrails(p *pixelfx.Painter, C []uint32, a *pixelfx.Actor, S *actorShape, t float64, front bool) {
	for j, sp := range cursedTrailSpots {
		if (j%2 == 0) != front {
			continue
		}
		my := int(pixelfx.Round(float64(S.h) * sp.q))
		l, r := 0, 0
		if my >= 0 && my < len(S.rows) {
			l, r = S.rows[my][0], S.rows[my][1]
		}
		x0 := a.X + float64(r+1)
		if sp.side < 0 {
			x0 = a.X + float64(l)
		}
		y0 := a.Y + float64(my)
		life := 1.6
		n := 18
		for i := n - 1; i >= 0; i-- {
			age := pixelfx.Wrap(t-(float64(i)/float64(n))*cursedDuration-float64(j)*0.37, cursedDuration)
			tb := t - age
			u := age / life
			if u >= 1 {
				continue
			}
			sway := math.Sin((tb/cursedDuration)*tau*2+float64(j)*1.7) * 4 * math.Min(1, u*1.6)
			x := x0 + sp.side*u*7 + sway + (pixelfx.Rand(float64(i), float64(j), 71)-0.5)*2.5*u
			y := y0 - age*19 - 5*u*u
			rad := (1.3 + 3.6*(1-math.Pow(1-u, 3))) * (0.7 + 0.6*pixelfx.Rand(float64(i), float64(j), 72))
			kf := 0.62
			if front {
				kf = 0.5
			}
			k := pixelfx.Smooth(0, 0.12, u) * (1 - pixelfx.Smooth(0.45, 1, u)) * kf
			base := pixelfx.Mix(C[4], C[3], 0.4)
			if pixelfx.Rand(float64(i), float64(j), 70) < 0.5 {
				base = C[4]
			}
			c := pixelfx.Mix(base, C[1], u*0.85)
			p.Disc(x, y, rad*1.08, c, k*0.4)
			p.Disc(x+rad*0.05, y+rad*0.08, rad*0.76, c, k*0.55)
			p.Disc(x-rad*0.22, y-rad*0.3, rad*0.48, pixelfx.Mix(c, C[6], 0.25), k*0.38*(1-u*0.6))
			if i%3 == 0 {
				p.Glow(x, y, rad*1.6, C[4], 0.08*k)
			}
		}
	}
}

func newCursed(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	H := float64(S.h)
	skullPaint := map[byte]uint32{'e': C[0], 'E': C[7]}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			fl, fr := S.rows[S.h-1][0], S.rows[S.h-1][1]
			cx := a.X + float64(fl+fr+1)/2
			rx := float64(fr-fl)/2 + 7 + 2*math.Sin((t/cursedDuration)*tau)
			for x := math.Floor(cx - rx - 1); x <= cx+rx+1; x++ {
				q := math.Abs(x+0.5-cx) / rx
				n := pixelfx.Rand(x, 31)
				if q > 1+n*0.12 {
					continue
				}
				col := C[2]
				if q > 0.8 {
					col = C[4]
				} else if n < 0.2 {
					col = C[3]
				}
				p.Px(x, 0, col)
				if q < 0.85 {
					p.Px(x, 1, C[1])
				}
				if q < 0.55 && n < 0.7 {
					p.Px(x, 2, C[1])
				}
			}
			p.Shade(cx-rx-6, -4, cx+rx+6, 5, func(x, y float64, out *pixelfx.Out) uint32 {
				d := pixelfx.Hypot((x-cx)/(rx+6), y/4)
				if d < 1 {
					out.L = C[4]
					out.K = 0.2 * (1 - d) * (1 - d)
				}
				return 0
			})
			cursedTrails(p, C, a, S, t, false)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			for _, pt := range S.body {
				mx, my := pt[0], pt[1]
				c := colorAt(a, mx, my)
				up := 1 - (float64(my)+0.5)/H
				lv := cursedReach(float64(mx), t, seed)
				if up > lv+0.035 {
					continue
				}
				x, y := a.X+float64(mx), a.Y+float64(my)
				if up > lv-0.02 {
					p.Px(x, y, C[6])
				} else if up > lv-0.06 {
					p.Px(x, y, C[4])
				} else {
					p.Px(x, y, pixelfx.Mix(C[1], C[3], pixelfx.Clamp(pixelfx.Luma(c)*1.6)))
				}
			}
			for mx := 0; mx < S.w; mx++ {
				lv := cursedReach(float64(mx), t, seed)
				my := int(math.Floor((1 - lv) * H))
				if S.on(mx, my) {
					p.Glow(a.X+float64(mx), a.Y+float64(my), 4, C[5], 0.14)
				}
			}
			for _, e := range E {
				for _, pt := range e.px {
					col := C[5]
					if pt[1] == e.y0 {
						col = C[7]
					}
					p.Px(a.X+float64(pt[0]), a.Y+float64(pt[1]), col)
				}
				p.Glow(a.X+float64(e.x0+e.x1+1)/2, a.Y+float64(e.y0+e.y1+1)/2, 5, C[5], 0.4)
			}
			cursedTrails(p, C, a, S, t, true)
			if t >= cursedIn && t < cursedGone {
				glitch := 0.0
				if t < cursedHold {
					glitch = 1 - (t-cursedIn)/(cursedHold-cursedIn)
				} else if t > cursedOut {
					glitch = (t - cursedOut) / (cursedGone - cursedOut)
				}
				f := math.Floor(t * 30)
				on := glitch < 0.15 || pixelfx.Rand(f, 3) > glitch*0.6
				x := 0.0
				y := a.Y - 12 + math.Sin((t/cursedDuration)*tau*2)*1.2
				if on {
					tear := func(dx, dy int) byte {
						row := dy + 6
						shift := 0
						if glitch > 0 && pixelfx.Rand(f, float64(row>>1), 7) < glitch*0.8 {
							shift = int(pixelfx.Round((pixelfx.Rand(f, float64(row), 9) - 0.5) * 6 * glitch))
						}
						return cursedSkull(dx-shift, dy)
					}
					if glitch > 0 {
						icon(p, x-2, y, 7, tear, [4]uint32{0, C[2], C[2], C[3]}, 0.7*glitch, nil)
						icon(p, x+2, y, 7, tear, [4]uint32{0, C[6], C[6], C[6]}, 0.5*glitch, nil)
					}
					icon(p, x, y, 8, tear, [4]uint32{C[0], C[4], C[6], C[7]}, 1, skullPaint)
					p.Glow(x-3, y-1, 5, C[5], 0.6)
					p.Glow(x+3, y-1, 5, C[5], 0.6)
					p.Glow(x, y, 16, C[4], 0.24+0.3*glitch)
				}
				if glitch > 0.1 {
					for k := 0; k < 3; k++ {
						ly := y - 5 + math.Floor(pixelfx.Rand(f, float64(k), 11)*11)
						lx := pixelfx.Round((pixelfx.Rand(f, float64(k), 12) - 0.5) * 10)
						ln := 3 + math.Floor(pixelfx.Rand(f, float64(k), 13)*8)
						col := C[4]
						if k%2 != 0 {
							col = C[6]
						}
						p.Line(lx-ln/2, ly, lx+ln/2, ly, col, glitch)
					}
				}
			}
			p.Glow(0, a.Y+H*(1-cursedTide(t)), 24, C[4], 0.2)
			p.Glow(0, a.Y+H*0.45, 30, C[3], 0.12)
		},
	}
}
