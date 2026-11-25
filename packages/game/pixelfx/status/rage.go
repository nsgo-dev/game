// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/rage.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the shaded, outlined anger mark and the embers from Sprite Fusion's open-source heal and fire (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const (
	rageDuration = 1.2
	rageLub      = 0.05
	rageDub      = 0.29
	rageG        = 760.0
)

var (
	rageSeethe = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(4), pixelfx.FX.Is(-13), pixelfx.FY.Is(-19), pixelfx.BX.Is(13), pixelfx.BY.Is(-19), pixelfx.FFX.Is(-8), pixelfx.FBX.Is(9))
	rageThud   = rageSeethe.With(pixelfx.Crouch.Is(6), pixelfx.FY.Is(-23), pixelfx.BY.Is(-23))
	rageMark   = [2]func(dx, dy int) byte{
		bitmap([]string{"..#.#..", "..#.#..", "##...##", ".......", "##...##", "..#.#..", "..#.#.."}, 1, false),
		bitmap([]string{"...#.#...", "...#.#...", "..##.##..", "###...###", ".........", "###...###", "..##.##..", "...#.#...", "...#.#..."}, 1, false),
	}
)

// Rage is seething: the body judders on every heartbeat, an anger mark throbs at the temple, eyes burn, steam jets off the head, stress lines flash out and the floor cracks hot underfoot. It is registered as "status/rage".
var Rage = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Rage",
		Desc:     "Seething: the body judders on every heartbeat, an anger mark throbs at the temple, eyes burn, steam jets off the head, stress lines flash out and the floor cracks hot underfoot.",
		Tags:     []string{"status", "rage", "berserk", "anger", "buff", "loop"},
		Size:     [2]int{80, 80},
		Origin:   [2]int{40, 70},
		Duration: rageDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#1a0208", "#4a0612", "#8a0c1c", "#c81828", "#ff3040", "#ff7a7a", "#ffc8c8", "#ffffff")},
			{Name: "heat", Default: pixelfx.Palette("#5a0a0a", "#c8281a", "#ff7a2a", "#ffd27a")},
			{Name: "steam", Default: pixelfx.Color("#f2e8ee")},
			{Name: "seed", Default: pixelfx.Num(6)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: rageSeethe},
			{T: 0.05, Pose: rageThud, How: "snap"},
			{T: 0.17, Pose: rageSeethe, How: "snap"},
			{T: 0.29, Pose: rageThud.With(pixelfx.Crouch.Is(5)), How: "snap"},
			{T: 0.4, Pose: rageSeethe, How: "snap"},
			{T: 1.2, Pose: rageSeethe},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if rageShaking(t) {
			return 0
		}
		return 1
	},
	New: newRage,
}

func init() { pixelfx.Register("status/rage", Rage) }

func ragePulse(t, at, w float64) float64 {
	d := (math.Mod(math.Mod(t-at, rageDuration)+rageDuration+rageDuration/2, rageDuration) - rageDuration/2) / w
	return math.Exp(-(d * d))
}

func rageBeat(t float64) float64 {
	return ragePulse(t, rageLub, 0.06) + 0.65*ragePulse(t, rageDub, 0.06)
}

func rageSince(t, at float64) float64 {
	return pixelfx.Wrap(t-at, rageDuration)
}

func rageShaking(t float64) bool {
	return rageSince(t, rageLub-0.02) < 0.13 || rageSince(t, rageDub-0.02) < 0.09
}

func rageCracksOf(a *pixelfx.Actor, S *actorShape, seed float64) [][][2]float64 {
	fl, fr := S.rows[S.h-1][0], S.rows[S.h-1][1]
	R := pixelfx.NewRng(seed*31 + 7)
	var out [][][2]float64
	starts := [4]struct {
		x0, dir float64
	}{
		{a.X + float64(fl+2), -1},
		{a.X + float64(fr-1), 1},
		{a.X + float64(fl+fr)/2, -1},
		{a.X + float64(fl+fr)/2 + 1, 1},
	}
	for _, st := range starts {
		x, y := st.x0, 0.5
		pts := [][2]float64{{x, y}}
		n := int(R.Int(5, 9))
		for i := 0; i < n; i++ {
			x += st.dir * R.Range(1.2, 2.4)
			y = pixelfx.ClampTo(y+R.Range(-0.9, 0.9), 0, 2.5)
			pts = append(pts, [2]float64{x, y})
			if i > 1 && R.Chance(0.3) {
				bx := x + st.dir*R.Range(1, 2)
				by := pixelfx.ClampTo(y+R.Sign()*R.Range(0.6, 1.4), 0, 3)
				out = append(out, [][2]float64{{x, y}, {bx, by}})
			}
		}
		out = append(out, pts)
	}
	return out
}

func newRage(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	Hh := pixelfx.Fit(o.Ramp("heat"), 4)
	steam := o.Color("steam")
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	cracks := rageCracksOf(a, S, seed)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			b := rageBeat(t)
			stamp := math.Exp(-rageSince(t, rageLub) / 0.18)
			pixelfx.Cycle(t, rageDuration, 9, seed+2, func(r *pixelfx.Rng) float64 { return r.Range(0.7, 1.1) }, func(age, life float64, r *pixelfx.Rng, _ int) {
				u := age / life
				x0 := r.Range(-16, 16)
				y := a.Y + r.Range(10, 40) - age*r.Range(14, 22)
				ln := r.Range(6, 11)
				ph := r.Range(0, tau)
				k := math.Sin(pi*u) * 0.55
				for d := 0.0; d < ln; d++ {
					col := C[1]
					if int(d)%3 != 0 {
						col = C[2]
					}
					p.Dot(x0-ln/2+d, y+pixelfx.Round(math.Sin(d*0.9+ph+age*12)*0.8), col, k*(1-math.Abs(d/ln-0.5)*1.4))
				}
			})
			p.Shade(-30, -4, 30, 8, func(x, y float64, out *pixelfx.Out) uint32 {
				d := pixelfx.Hypot(x/28, (y-0.5)/4.5)
				if d < 1 {
					out.L = C[3]
					out.K = (0.2 + 0.3*b) * (1 - d) * (1 - d)
				}
				return 0
			})
			heat := 0.25 + 0.75*stamp
			for _, pts := range cracks {
				for i := 1; i < len(pts); i++ {
					x0, y0 := pts[i-1][0], pts[i-1][1]
					x1, y1 := pts[i][0], pts[i][1]
					far := float64(i) / float64(len(pts))
					col := Hh[1]
					if heat*(1-far*0.6) > 0.6 {
						col = Hh[3]
					} else if heat*(1-far*0.5) > 0.35 {
						col = Hh[2]
					}
					p.Line(x0, y0, x1, y1, col, 1)
					if i%2 != 0 {
						p.Glow(x1, y1, 4, Hh[2], 0.25*heat*(1-far*0.5))
					}
				}
			}
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			b := rageBeat(t)
			shake := rageShaking(t)
			dx := 0.0
			if shake {
				if int(math.Floor(t*60))%2 != 0 {
					dx = 1
				} else {
					dx = -1
				}
			}
			dy := 0.0
			if shake && rageSince(t, rageLub-0.02) < 0.06 {
				dy = -1
			}
			for _, pt := range S.body {
				mx, my := pt[0], pt[1]
				c := colorAt(a, mx, my)
				l := pixelfx.Luma(c)
				skin := l > 0.55 && (c&255) > ((c>>16)&255)+30
				if skin {
					p.Px(a.X+float64(mx)+dx, a.Y+float64(my)+dy, pixelfx.Mix(c, C[4], 0.22+0.3*b))
				} else if shake {
					p.Px(a.X+float64(mx)+dx, a.Y+float64(my)+dy, c)
				}
			}
			for _, e := range E {
				ex, ey := a.X+dx, a.Y+dy
				inner := -1
				if float64(e.x0+e.x1)/2 < float64(a.W)/2 {
					inner = 1
				}
				for _, pt := range e.px {
					col := C[4]
					if pt[1] == e.y0 {
						col = C[7]
					} else if pt[1] == e.y0+1 {
						col = C[6]
					}
					p.Px(ex+float64(pt[0]), ey+float64(pt[1]), col)
				}
				xi, xo := e.x0, e.x1
				if inner > 0 {
					xi, xo = e.x1, e.x0
				}
				p.Px(ex+float64(xo-inner), ey+float64(e.y0-2), 0xff1f1726)
				p.Px(ex+float64(xo), ey+float64(e.y0-2), 0xff1f1726)
				p.Px(ex+float64(xi), ey+float64(e.y0-1), 0xff1f1726)
				p.Glow(ex+float64(e.x0+e.x1+1)/2, ey+float64(e.y0+1), 5, C[5], 0.35+0.35*b)
			}
			big := ragePulse(t, rageLub+0.02, 0.05) > 0.35 || ragePulse(t, rageDub+0.02, 0.04) > 0.6
			mx := a.X + float64(a.W)*0.86 + dx
			my := a.Y + 3 + dy
			mk := rageMark[0]
			gl := 2.0
			if big {
				mk = rageMark[1]
				gl = 3
			}
			icon(p, mx, my, 5, mk, [4]uint32{C[0], C[3], C[4], C[5]}, 1, nil)
			p.Px(mx-gl, my-gl, C[6])
			p.Glow(mx, my, 8, C[4], 0.25+0.4*b)
			for _, jet := range [2]struct {
				at    float64
				n     int
				power float64
			}{{rageLub + 0.04, 7, 1}, {rageDub + 0.04, 4, 0.75}} {
				for _, side := range [2]float64{-1, 1} {
					for i := 0; i < jet.n; i++ {
						age := rageSince(t, jet.at+float64(i)*0.016)
						life := 0.22 + 0.12*pixelfx.Rand(float64(i), side, 3)
						if age >= life {
							continue
						}
						u := age / life
						ang := -pi/2 + side*(0.5+0.2*pixelfx.Rand(float64(i), side, 5))
						sp := (90 + 40*pixelfx.Rand(float64(i), side, 7)) * jet.power
						tr := travel(sp, 0, 7, age)
						x := side*float64(a.W)*0.36 + math.Cos(ang)*tr
						y := a.Y + 3 + math.Sin(ang)*tr - 8*age
						rad := (1 + 3.2*math.Sqrt(u)) * (0.8 + 0.4*pixelfx.Rand(float64(i), side, 9))
						k := math.Min(1, u*12) * math.Pow(1-u, 2)
						p.Disc(x, y, rad, steam, 0.7*k)
						p.Disc(x-rad*0.2, y-rad*0.25, rad*0.6, 0xffffffff, 0.6*k)
						if u < 0.3 {
							p.Dot(x, y, 0xffffffff, 1-u/0.3)
						}
					}
				}
			}
			for _, burst := range [2]struct {
				at float64
				n  int
				ln float64
			}{{rageLub, 8, 7}, {rageDub, 6, 5}} {
				age := rageSince(t, burst.at)
				if age > 0.2 {
					continue
				}
				k := age / 0.2
				for i := 0; i < burst.n; i++ {
					ang := -pi/2 + ((float64(i)+0.5)/float64(burst.n)-0.5)*4.4
					r0 := 15 + 6*k
					ca, sa := math.Cos(ang), math.Sin(ang)
					cx, cy := 0.0, a.Y+12
					var pts [4][2]float64
					for j := 0; j <= 3; j++ {
						r := r0 + (float64(j)/3)*burst.ln
						off := -0.4
						if j%2 != 0 {
							off = 1.3
						}
						pts[j] = [2]float64{cx + ca*r - sa*off, cy + sa*r + ca*off}
					}
					p.PolylineFn(pts[:], func(u float64) (uint32, float64) {
						col := C[4]
						if u > 0.66 {
							col = C[7]
						} else if u > 0.33 {
							col = C[5]
						}
						return col, 1.2 - k
					})
					p.GlowLine(pts[0][0], pts[0][1], pts[3][0], pts[3][1], 3, C[4], 0.3*(1-k))
				}
			}
			st := rageSince(t, rageLub)
			for i := 0; i < 10; i++ {
				life := 0.5 + 0.5*pixelfx.Rand(float64(i), 11)
				age := st - 0.02*pixelfx.Rand(float64(i), 12)
				if age < 0 || age > life {
					continue
				}
				x := (pixelfx.Rand(float64(i), 13)-0.5)*30 + math.Sin(age*9+float64(i))*1.5*age
				y := -1 + travel(-(40+50*pixelfx.Rand(float64(i), 14)), -20, 1.6, age)
				ember(p, x, y, age/life, [3]uint32{Hh[3], Hh[2], Hh[1]}, age+float64(i), 1)
			}
			for i := 0; i < 12; i++ {
				vx := (pixelfx.Rand(float64(i), 21) - 0.5) * 90
				vy := -(40 + 70*pixelfx.Rand(float64(i), 22))
				tt := st
				x := (pixelfx.Rand(float64(i), 23)-0.5)*18 + vx*tt
				y := -1 + vy*tt + 0.5*rageG*tt*tt
				if tt > 0.6 || y > 0 {
					continue
				}
				col := uint32(0xff6a5e72)
				if i%3 != 0 {
					col = C[2]
				}
				p.Px(x, y, col)
			}
			p.Glow(0, a.Y+10, 26, C[4], 0.12+0.4*b)
			p.Glow(0, a.Y+22, 34, C[3], 0.08+0.18*b)
		},
	}
}
