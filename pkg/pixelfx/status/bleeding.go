// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/bleeding.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the beads, drops, drying splats and crowns from Sprite Fusion's open-source blood (https://www.spritefusion.com/pixel-art-vfx/blood), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

const (
	bleedingDuration = 2.4
	bleedingBeat     = bleedingDuration / 2
	bleedingG        = 900.0
)

var (
	bleedingClutch = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.7), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(2), pixelfx.FX.Is(2), pixelfx.FY.Is(-21), pixelfx.BX.Is(8), pixelfx.BY.Is(-14), pixelfx.FFX.Is(3), pixelfx.FBX.Is(-3))
	bleedingBeatP  = bleedingClutch.With(pixelfx.Crouch.Is(5), pixelfx.Lean.Is(4), pixelfx.HY.Is(1))
)

// Bleeding is bleeding out: beads swell at three wounds, run, let go and fall to spatter into a glossy pool that ripples; each heartbeat spurts a little and flashes the body red. It is registered as "status/bleeding".
var Bleeding = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Bleeding",
		Desc:     "Bleeding out: beads swell at three wounds, run, let go and fall to spatter into a glossy pool that ripples; each heartbeat spurts a little and flashes the body red.",
		Tags:     []string{"status", "bleed", "blood", "dot", "debuff", "wound", "loop"},
		Size:     [2]int{60, 56},
		Origin:   [2]int{30, 46},
		Duration: bleedingDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#350711", "#5e0c1b", "#931327", "#c71e33", "#f2384a", "#ff8a7a")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: bleedingBeatP},
			{T: 0.18, Pose: bleedingClutch},
			{T: 1.2, Pose: bleedingClutch},
			{T: 1.2, Pose: bleedingBeatP, How: "snap"},
			{T: 1.38, Pose: bleedingClutch},
			{T: 2.4, Pose: bleedingClutch},
			{T: 2.4, Pose: bleedingBeatP, How: "snap"},
		},
	},
	New: newBleeding,
}

func init() { pixelfx.Register("status/bleeding", Bleeding) }

type wound struct {
	x      float64
	mx     int
	y0, yb float64
	drops  int
	phase  float64
	side   float64
}

func woundsOf(a *pixelfx.Actor, S *actorShape) [3]wound {
	H, W := S.h, S.w
	makeWound := func(mx, my, drops int, phase float64) wound {
		run := 1
		for run < 3 && S.on(mx, my+run+1) {
			run++
		}
		side := 1.0
		if float64(mx) < float64(W)/2 {
			side = -1
		}
		return wound{
			x:     a.X + float64(mx) + 0.5,
			mx:    mx,
			y0:    a.Y + float64(my),
			yb:    a.Y + float64(my+run+1),
			drops: drops,
			phase: phase,
			side:  side,
		}
	}
	edge := func(q float64, side int) (int, int) {
		my := int(pixelfx.Round(float64(H) * q))
		row := S.rows[my]
		if side < 0 {
			return row[0] + 1, my
		}
		return row[1] - 1, my
	}
	fx, fy := edge(0.55, 1)
	for _, pt := range S.body {
		x, y := pt[0], pt[1]
		if float64(y) > float64(H)*0.5 && float64(y) < float64(H)*0.62 && float64(x) > float64(W)*0.55 && float64(x) < float64(W)*0.75 && S.on(x, y+3) {
			fx, fy = x, y
			break
		}
	}
	e0x, e0y := edge(0.57, -1)
	e2x, e2y := edge(0.6, 1)
	return [3]wound{
		makeWound(e0x, e0y, 3, 0),
		makeWound(fx, fy, 2, 0.35),
		makeWound(e2x, e2y, 2, 0.7),
	}
}

// bloodDrop draws a drop in flight: a bead with a tail when it is fast.
func bloodDrop(p *pixelfx.Painter, C []uint32, x, y, v float64, s int) {
	tail := math.Min(5, v*0.012)
	if tail >= 1 {
		col := C[2]
		if s >= 2 {
			col = C[3]
		}
		p.Line(x, y-tail, x, y-1, col, 1)
	}
	if s >= 2 {
		p.Px(x, y, C[5])
		p.Px(x+1, y, C[4])
		p.Px(x, y+1, C[3])
		p.Px(x+1, y+1, C[2])
	} else {
		p.Px(x, y, C[4])
	}
}

// bloodSplat draws a splat where a drop lands: it spreads, throws a crown of droplets, then dries darker and fades.
func bloodSplat(p *pixelfx.Painter, C []uint32, x, ground, age float64, big bool, seed float64) {
	life := bleedingDuration * 0.85
	if age < 0 || age > life {
		return
	}
	spread := pixelfx.Smooth(0, 0.1, age)
	base := 1.4
	if big {
		base = 2.2
	}
	hw := base * (0.5 + 0.5*spread)
	fade := 1 - pixelfx.Smooth(life-0.5, life, age)
	dry := 2
	if age < 0.35 {
		dry = 4
	} else if age < 1 {
		dry = 3
	}
	for xx, x1 := math.Floor(x-hw), math.Ceil(x+hw); xx <= x1; xx++ {
		col := C[dry]
		if math.Abs(xx+0.5-x) > hw*0.75 {
			col = C[dry-1]
		}
		p.Dot(xx, ground-1, col, fade)
	}
	if age < 0.3 {
		p.Px(x-0.5, ground-1, C[5])
	}
	if age < 0.4 {
		n := 2
		if big {
			n = 3
		}
		for j := 0; j < n; j++ {
			dir := -1.0
			if j%2 != 0 {
				dir = 1
			}
			vx := dir * (18 + 24*pixelfx.Rand(seed, float64(j), 1))
			vy := -(40 + 40*pixelfx.Rand(seed, float64(j), 2))
			tt := age
			dx := vx * tt
			dy := vy*tt + 0.5*bleedingG*tt*tt
			if dy <= 0 {
				col := C[4]
				if j != 0 {
					col = C[3]
				}
				p.Px(x+dx, ground-1+dy, col)
			}
		}
	}
}

func newBleeding(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 6)
	a := o.Actor
	S := shapeOf(a)
	W := woundsOf(a, S)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			px := (W[0].x+W[2].x)/2 - 1
			rx := 6.5
			beat := math.Exp(-(pixelfx.Wrap(t, bleedingBeat) / 0.25))
			for x := math.Floor(px - rx - 1); x <= px+rx+1; x++ {
				q := math.Abs(x+0.5-px) / rx
				if q > 1.1 {
					continue
				}
				col := C[1]
				if q > 0.85 {
					col = C[2]
				}
				p.Px(x, -1, col)
				if q < 0.8 {
					c0 := C[0]
					if q < 0.5 {
						c0 = C[1]
					}
					p.Px(x, 0, c0)
				}
				if q < 0.45 {
					p.Px(x, 1, C[0])
				}
			}
			p.Px(px-2, -1, C[4])
			p.Px(px-1, -1, C[3])
			p.Px(px+3, -1, C[3])
			p.Glow(px, -1, 8, C[3], 0.06+0.04*beat)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			ph := pixelfx.Wrap(t, bleedingBeat)
			hurt := math.Max(0, 1-ph/0.12)
			if hurt > 0 {
				for _, pt := range S.body {
					p.Blend(a.X+float64(pt[0]), a.Y+float64(pt[1]), C[4], 0.26*hurt)
				}
			}
			for _, w := range W {
				p.Px(w.x, w.y0-1, C[1])
				p.Px(w.x-w.side, w.y0-1, C[0])
				p.Px(w.x-w.side, w.y0, C[2])
				for y := w.y0; y < w.yb; y++ {
					col := C[2]
					if y == w.y0 {
						col = C[4]
					} else if y >= w.yb-1 {
						col = C[3]
					}
					p.Px(w.x, y, col)
				}
				period := bleedingDuration / float64(w.drops)
				for k := 0; k < w.drops; k++ {
					born := w.phase*period + float64(k)*period
					age := pixelfx.Wrap(t-born, bleedingDuration)
					swell := period * 0.62
					big := (k+w.mx)%2 == 0
					fallH := math.Max(1, -1-w.yb)
					fall := math.Sqrt((2 * fallH) / bleedingG)
					ground := 0.0
					if age < swell {
						u := age / swell
						ln := 3
						if u < 0.35 {
							ln = 1
						} else if u < 0.75 {
							ln = 2
						}
						for j := 0; j < ln; j++ {
							col := C[3]
							if j == ln-1 {
								col = C[2]
							}
							p.Px(w.x, w.yb+float64(j), col)
						}
						if ln == 3 {
							p.Px(w.x-1, w.yb+2, C[1])
						}
						col := C[4]
						if u > 0.5 {
							col = C[5]
						}
						p.Px(w.x, w.yb+float64(ln-2), col)
					} else if age < swell+fall {
						f := age - swell
						y := w.yb + 2 + 0.5*bleedingG*f*f
						s := 1
						if big {
							s = 2
						}
						bloodDrop(p, C, w.x, y, bleedingG*f, s)
					} else {
						bloodSplat(p, C, w.x, ground, age-swell-fall, big, float64(w.mx*31+k))
					}
				}
			}
			w := W[0]
			if ph < 0.5 {
				for j := 0; j < 7; j++ {
					vx := w.side * (18 + 42*pixelfx.Rand(float64(j), 7))
					vy := -(15 + 50*pixelfx.Rand(float64(j), 8))
					tt := ph - float64(j)*0.012
					if tt <= 0 {
						continue
					}
					x := w.x + travel(vx, 0, 1.2, tt)
					y := w.y0 + travel(vy, bleedingG*0.8, 0.6, tt)
					x2 := w.x + travel(vx, 0, 1.2, tt-0.02)
					y2 := w.y0 + travel(vy, bleedingG*0.8, 0.6, tt-0.02)
					if y >= -1 {
						continue
					}
					lc := C[2]
					if j%3 != 0 {
						lc = C[3]
					}
					p.Line(x2, y2, x, y, lc, 1)
					pc := C[4]
					if j%2 != 0 {
						pc = C[5]
					}
					p.Px(x, y, pc)
				}
			}
			p.Glow(w.x, w.y0, 6, C[4], 0.25*hurt)
		},
	}
}
