// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/stunned.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the shaded, outlined icons and the twinkles from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

const (
	stunnedDuration = 2.0
	stunnedTurns    = 2.0
	stunnedN        = 5
)

var (
	stunnedDaze  = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.FX.Is(-7), pixelfx.FY.Is(-15), pixelfx.BX.Is(8), pixelfx.BY.Is(-15), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	stunnedStar7 = bitmap([]string{"...#...", "..###..", "#######", ".#####.", "..###..", ".##.##.", ".#...#."}, 1, false)
	stunnedStarF = starShape(0.44)
	stunnedBird  = [2]func(dx, dy int) byte{
		makeStunnedBird(true),
		makeStunnedBird(false),
	}
)

func makeStunnedBird(up bool) func(dx, dy int) byte {
	wing := [][2]int{{-1, 1}, {-1, 2}, {-2, 2}, {-2, 3}, {0, 1}}
	if up {
		wing = [][2]int{{-1, -1}, {-1, -2}, {-2, -2}, {-2, -3}, {-3, -3}, {-3, -4}}
	}
	return func(dx, dy int) byte {
		x, y := float64(dx)+0.5, float64(dy)+0.5
		if dx == 3 && dy == -2 {
			return 'e'
		}
		if (dx == 5 || dx == 4) && dy == -1 {
			if dx == 5 {
				return 'b'
			}
			return '#'
		}
		for _, w := range wing {
			if w[0] == dx && w[1] == dy {
				return 'w'
			}
		}
		a1 := x / 3.3
		b1 := (y - 0.6) / 1.9
		a2 := x - 2.8
		b2 := y + 1.2
		if a1*a1+b1*b1 <= 1 || a2*a2+b2*b2 <= 1.7*1.7 {
			return '#'
		}
		if (dx == -4 && (dy == -1 || dy == 0)) || (dx == -5 && dy == -1) {
			return '#'
		}
		return 0
	}
}

// Stunned is seeing stars: three spinning stars and two little birds circle the head on a tilted ring, ducking behind it and sweeping round in front, trailing glitter. It is registered as "status/stunned".
var Stunned = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Stunned",
		Desc:     "Seeing stars: three spinning stars and two little birds circle the head on a tilted ring, ducking behind it and sweeping round in front, trailing glitter.",
		Tags:     []string{"status", "stun", "dizzy", "daze", "stars", "birds", "debuff", "loop"},
		Size:     [2]int{60, 64},
		Origin:   [2]int{30, 56},
		Duration: stunnedDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#3a1e0c", "#8a4a12", "#d88a1c", "#f5c030", "#ffe36e", "#fff6c8", "#ffffff")},
			{Name: "birds", Default: pixelfx.Palette("#1a1422", "#6a7fae", "#b8c8e8", "#f4f8ff")},
			{Name: "beak", Default: pixelfx.Color("#ffae35")},
			{Name: "seed", Default: pixelfx.Num(2)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: stunnedDaze.With(pixelfx.X.Is(-1), pixelfx.Lean.Is(-3), pixelfx.HX.Is(-1))},
			{T: 0.5, Pose: stunnedDaze.With(pixelfx.X.Is(0), pixelfx.Lean.Is(0), pixelfx.Crouch.Is(2), pixelfx.HY.Is(1), pixelfx.FFX.Is(-2))},
			{T: 1, Pose: stunnedDaze.With(pixelfx.X.Is(1), pixelfx.Lean.Is(3), pixelfx.HX.Is(1))},
			{T: 1.5, Pose: stunnedDaze.With(pixelfx.X.Is(0), pixelfx.Lean.Is(0), pixelfx.Crouch.Is(2), pixelfx.HY.Is(1), pixelfx.FBX.Is(3))},
			{T: 2, Pose: stunnedDaze.With(pixelfx.X.Is(-1), pixelfx.Lean.Is(-3), pixelfx.HX.Is(-1))},
		},
	},
	New: newStunned,
}

func init() { pixelfx.Register("status/stunned", Stunned) }

type stunnedItem struct {
	i          int
	th, x, y   float64
	near       bool
	depth      float64
	bird       bool
	rx, ry, cy float64
}

func stunnedRing(t float64, a *pixelfx.Actor) [stunnedN]stunnedItem {
	cy := a.Y + 2
	rx := float64(a.W) * 0.62
	ry := 4.5
	var out [stunnedN]stunnedItem
	for i := 0; i < stunnedN; i++ {
		th := tau * ((stunnedTurns*t)/stunnedDuration + float64(i)/stunnedN)
		s := math.Sin(th)
		out[i] = stunnedItem{
			i: i, th: th, x: math.Cos(th) * rx, y: cy + s*ry,
			near: s >= 0, depth: 0.5 + 0.5*s, bird: i%2 == 1, rx: rx, ry: ry, cy: cy,
		}
	}
	return out
}

func newStunned(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 7)
	Bd := pixelfx.Fit(o.Ramp("birds"), 4)
	beak := o.Color("beak")
	seed := o.Seed()
	a := o.Actor
	flareRamp := []uint32{C[3], C[4], C[5], C[6]}

	drawLayer := func(p *pixelfx.Painter, t float64, far bool) {
		dim := func(c uint32) uint32 {
			if far {
				return pixelfx.Mix(c, Bd[0], 0.3)
			}
			return c
		}
		birdPaint := map[byte]uint32{'e': Bd[0], 'b': dim(beak), 'w': dim(Bd[1])}
		for _, it := range stunnedRing(t, a) {
			if it.near == far {
				continue
			}
			if !it.bird {
				for k := 1; k <= 12; k++ {
					th := it.th - float64(k)*0.085
					x := math.Cos(th) * it.rx
					y := it.cy - 1 + math.Sin(th)*it.ry
					if (math.Sin(th) >= 0) == far || pixelfx.Rand(float64(it.i), float64(k), math.Floor(((t/stunnedDuration)*stunnedTurns+float64(k)*0.37)*9)) < 0.25 {
						continue
					}
					col := C[4]
					if k < 4 {
						col = C[6]
					} else if k < 8 {
						col = C[5]
					}
					p.Dot(x, y, dim(col), 1.1-float64(k)/13)
					if k < 6 {
						p.Light(x, y, C[4], 0.3*(1-float64(k)/6))
					}
				}
			}
			if it.bird {
				up := int(math.Floor(t*8+float64(it.i)*0.5))%2 == 0
				bob := math.Sin((t/stunnedDuration)*tau*4+float64(it.i)) * 0.7
				f := stunnedBird[1]
				if up {
					f = stunnedBird[0]
				}
				ins := f
				if it.near {
					ins = func(dx, dy int) byte { return f(-dx, dy) }
				}
				icon(p, it.x, it.y-3+bob, 7, ins, [4]uint32{Bd[0], dim(Bd[1]), dim(Bd[2]), dim(Bd[3])}, 1, birdPaint)
			} else {
				spin := (t/stunnedDuration)*tau*3 + float64(it.i)
				R := [4]uint32{C[0], dim(C[3]), dim(C[4]), dim(C[5])}
				if it.depth > 0.55 {
					icon(p, it.x, it.y-1, 7, shaped(stunnedStarF, 4+0.8*it.depth, spin), R, 1, nil)
				} else {
					icon(p, it.x, it.y-1, 5, stunnedStar7, R, 1, nil)
				}
				pc := C[6]
				if far {
					pc = C[5]
				}
				p.Px(it.x, it.y-1, pc)
				p.Glow(it.x, it.y-1, 11, C[4], 0.24+0.3*it.depth)
				p.Glow(it.x, it.y-1, 4, C[6], 0.3*it.depth)
				da := math.Atan2(math.Sin(it.th-pi/2), math.Cos(it.th-pi/2)) / 0.32
				fr := math.Exp(-(da * da))
				if !far && fr > 0.08 {
					pixelfx.Flare(p, it.x, it.y-1, 9*fr, fr, flareRamp)
				}
			}
		}
	}

	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			drawLayer(p, t, true)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			drawLayer(p, t, false)
			pixelfx.Cycle(t, stunnedDuration, 14, seed+3, func(r *pixelfx.Rng) float64 { return r.Range(0.25, 0.5) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				th := r.Range(0, tau)
				u := age / life
				x := math.Cos(th) * float64(a.W) * r.Range(0.45, 0.95)
				y := a.Y + 1 + math.Sin(th)*7 - r.Range(0, 9)
				arm := pixelfx.Round(r.Range(1, 2.6) * math.Sin(pi*u))
				if arm < 1 {
					p.Dot(x, y, C[6], math.Sin(pi*u)*1.4)
				}
				pixelfx.Sparkle(p, x, y, arm, C[5], C[4], 0.35*math.Sin(pi*u))
			})
			p.Glow(0, a.Y+1, 26, C[4], 0.13)
		},
	}
}
