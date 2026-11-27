// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/spawn/respawn-beam.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the beam's slam, strands, rings and the sparks thrown along the floor from Sprite Fusion's open-source holy-light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package spawn

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	respawnSet    = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(4), pixelfx.FX.Is(-9), pixelfx.FY.Is(-14), pixelfx.BX.Is(10), pixelfx.BY.Is(-14), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	respawnSlam   = 0.32
	respawnWrite0 = respawnSlam + 0.08
	respawnWrite  = 0.95
	respawnDone   = respawnWrite0 + respawnWrite
	respawnLift   = respawnDone + 0.12
	respawnDrop   = 0.07
)

// RespawnBeam drops a guide line, slams a pillar of light onto the spot, and climbs a scan bar from the feet writing the character back in row by row, white-hot lines cooling to its colors. It is registered as "spawn/respawn-beam".
var RespawnBeam = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Respawn beam",
		Desc:     "A guide line drops, a pillar of light slams onto the spot, and a scan bar climbs from the feet writing the character back in row by row, white-hot lines cooling to its colors.",
		Tags:     []string{"spawn", "respawn", "beam", "teleport", "scanline", "light", "one-shot"},
		Size:     [2]int{128, 150},
		Origin:   [2]int{64, 136},
		Duration: 2.5,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#061e1b", "#0c3e36", "#136a5a", "#1f9c80", "#45d1a8", "#a8f5d8", "#ffffff")},
			{Name: "width", Default: pixelfx.Num(15)},
			{Name: "seed", Default: pixelfx.Num(12)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: 0, Pose: respawnSet},
			{T: 1.4, Pose: respawnSet},
			{T: 1.72, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-13), pixelfx.FY.Is(-36), pixelfx.BX.Is(14), pixelfx.BY.Is(-36), pixelfx.HY.Is(-1), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 2.1, Pose: pixelfx.Ready},
			{T: 2.5, Pose: pixelfx.Ready},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < respawnDone+0.04 {
			return 0
		}
		return 1
	},
	New: newRespawnBeam,
}

func init() { pixelfx.Register("spawn/respawn-beam", RespawnBeam) }

type respawnStrand struct {
	o, f, w float64
}

func newRespawnBeam(o *pixelfx.Options) pixelfx.Instance {
	C, R, BW := pixelfx.Fit(o.Palette(), 7), pixelfx.NewRng(o.Seed()), o.Num("width")
	floor := func(float64) float64 { return 0 }
	body := bodyOf(o.Actor, C)
	rim := rimOf(body)
	top, H := body.y, float64(body.h)
	const topY = -136.0
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[5], C[4], C[3]}})
	lights := &pixelfx.Lights{}

	// strands of brighter light running down the beam, each at its own offset and shimmer
	var strands [6]respawnStrand
	for i := range strands {
		fi := float64(i)
		strands[i] = respawnStrand{o: pixelfx.Rand(fi, 1)*1.6 - 0.8, f: 1 + pixelfx.Rand(fi, 2)*3, w: pixelfx.Rand(fi, 3)}
	}
	// the scan bar: where it is (local y), climbing from the feet to the crown
	scanY := func(tt float64) float64 {
		return -pixelfx.Ease.InOutSine(pixelfx.Clamp((tt-respawnWrite0)/respawnWrite)) * (H + 2)
	}
	// when each row is written, and its glitch: some rows jump sideways for a frame or two as they land
	rowT := func(my int) float64 {
		return respawnWrite0 + (respawnWrite*math.Acos(1-2*pixelfx.Clamp((H-float64(my))/(H+2))))/pi
	}
	t, slammed, finished := 0.0, false, false
	width := func(tt float64) float64 {
		switch {
		case tt < respawnSlam-respawnDrop:
			return 0
		case tt < respawnSlam:
			return BW * 0.6
		case tt < respawnLift:
			return BW * (1 + 0.4*math.Exp(-(tt-respawnSlam)*10)) * (0.92 + 0.08*math.Sin(tt*40))
		default:
			return BW * (1 - pixelfx.Ease.OutCubic(pixelfx.Clamp((tt-respawnLift)/0.3)))
		}
	}
	// the beam lifts away bottom first once the character is written
	bottom := func(tt float64) float64 {
		if tt < respawnLift {
			return 0
		}
		return -150 * pixelfx.Ease.InCubic(pixelfx.Clamp((tt-respawnLift)/0.4))
	}
	starCols1 := []uint32{pixelfx.White, C[5]}
	starCols2 := []uint32{pixelfx.White, C[5], C[5], C[4]}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// the guide: motes drawn in to the spot along the floor
			if t < respawnSlam && R.Chance(0.5) {
				s, D, T := R.Sign(), R.Range(18, 40), R.Range(0.15, 0.25)
				v := (D * 3) / (math.Exp(3*T) - 1)
				y := -R.Range(0, 3)
				col := C[4]
				if R.Chance(0.5) {
					col = C[5]
				}
				ps.Spawn(pixelfx.Streak, s*D, y, -s*v, 0, T, col, 1, 0, 0)
			}
			if !slammed && t >= respawnSlam {
				slammed = true
				lights.Add(0, -28, 64, C[4], 1, 0.5)
				lights.Add(0, -52, 80, C[5], 1.1, 0.4)
				lights.Add(0, -9, 24, pixelfx.White, 0.9, 0.12)
				for range 40 {
					s, sp := R.Sign(), R.Range(60, 135)
					x, vy, life := s*R.Range(0, 6), -R.Range(20, 140), R.Range(0.25, 0.7)
					ps.Spawn(pixelfx.Spark, x, -1, s*sp, vy, life, pixelfx.White, 1, 0, 0)
				}
				for range 12 {
					x, y, vx, vy := R.Range(-BW, BW), -R.Range(2, 40), R.Range(-20, 20), -R.Range(20, 70)
					life, col, size := R.Range(0.5, 1), pixelfx.Pick(R, starCols1), R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
				}
				for range 8 {
					s := R.Sign()
					x, vx, vy, life, size := s*R.Range(4, 10), s*R.Range(30, 80), -R.Range(3, 10), R.Range(0.4, 0.7), R.Range(1.5, 3)
					ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, C[2], size, 0, 0)
				}
			}
			// the scan bar spits sparks sideways as it climbs
			if t > respawnWrite0 && t < respawnDone && R.Chance(0.45) {
				y, s := scanY(t), R.Sign()
				x, vx, vy, life := s*R.Range(6, 11), s*R.Range(40, 140), R.Range(-60, 10), R.Range(0.15, 0.35)
				ps.Spawn(pixelfx.Spark, x, y, vx, vy, life, pixelfx.White, 1, 0, 0)
			}
			// motes rise inside the beam while it stands
			if t > respawnSlam && t < respawnLift+0.2 && R.Chance(0.6) {
				x := R.Range(-1.3, 1.3) * BW
				y := -R.Range(0, 90)
				vx := R.Range(-6, 6)
				vy := -R.Range(30, 90)
				life := R.Range(0.4, 0.9)
				col := pixelfx.Pick(R, starCols2)
				size := 0.6
				if R.Chance(0.3) {
					size = R.Range(1, 1.8)
				}
				ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
			}
			if !finished && t >= respawnDone {
				finished = true
				lights.Add(0, top+H/2, 40, C[5], 0.7, 0.3)
				for range 14 {
					c := pixelfx.Pick(R, rim)
					vx, vy, life, col, size := R.Range(-25, 25), -R.Range(10, 50), R.Range(0.4, 0.9), pixelfx.Pick(R, starCols1), R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, c.x, c.y, vx, vy, life, col, size, 0, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			w, b := width(t), bottom(t)
			// the guide line before the slam, a marker on the floor
			if t < respawnSlam {
				k := pixelfx.Smooth(0, respawnSlam*0.7, t) * (1 - pixelfx.Smooth(respawnSlam-respawnDrop, respawnSlam, t)*0.5)
				odd := float64(pixelfx.ToInt32(t*60) % 2)
				for y := topY; y < -1; y += 2 {
					p.Dot(0, y+odd, C[5], k*(0.35+0.65*((y-topY)/-topY)))
				}
				pixelfx.Band(p, 0, 0, 10-4*k, 0.2, 1, k, C, func(g float64) bool { return math.Sin(g) < 0 }, false)
				p.Glow(0, -2, 16, C[4], 0.4*k)
			}
			if w <= 0.2 {
				return
			}
			// the beam: a see-through column, hard bright edges, shimmering strands; its light hugs the edges so whatever it writes inside stays readable
			yEnd := -1 + b
			if t < respawnSlam {
				yEnd = topY + (-1-topY)*pixelfx.Ease.InCubic(pixelfx.Clamp((t-respawnSlam+respawnDrop)/respawnDrop))
			}
			writing := pixelfx.Smooth(respawnWrite0-0.05, respawnWrite0+0.1, t) * (1 - pixelfx.Smooth(respawnDone, respawnLift, t))
			fill := 0.2 - 0.1*writing
			fr := float64(pixelfx.ToInt32(t * 30))
			core := 1 - pixelfx.Smooth(respawnSlam+0.06, respawnWrite0+0.08, t)
			x0, x1 := math.Floor(-w), math.Ceil(w)
			for y := topY; y <= yEnd; y++ {
				for x := x0; x <= x1; x++ {
					u := (x + 0.5) / w
					q := math.Abs(u)
					if q > 1 {
						continue
					}
					if q > 0.86 {
						col := C[5]
						if q > 0.94 && pixelfx.Rand(x, y, fr) < 0.7 {
							col = pixelfx.White
						}
						p.Dot(x, y, col, 1)
						continue
					}
					if q < 0.7*core {
						// the white-hot core, with strands of colour running down it
						hot := q < 0.35*core
						for _, s := range strands {
							if math.Abs(u-s.o*0.6-0.05*math.Sin(t*s.f*3+y*0.05)) < 0.04 {
								hot = false
							}
						}
						col := C[5]
						if hot {
							col = pixelfx.White
						} else if q < 0.5*core {
							col = C[6]
						}
						p.Px(x, y, col)
						continue
					}
					a, c := fill*(1.2-q), C[4]
					for _, s := range strands {
						if math.Abs(u-s.o-0.06*math.Sin(t*s.f+s.o*7)) < 0.045+0.03*s.w {
							a, c = 0.55-0.25*writing, C[5]
						}
					}
					p.Blend(x, y, c, a)
				}
			}
			if yEnd > topY+24 {
				for _, s := range [2]float64{-1, 1} {
					p.GlowLine(s*w, topY+24, s*w, yEnd, 6, C[4], 0.3)
				}
			}
			p.Glow(0, -5, w*1.5+6, C[5], 0.45)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			w := width(t)
			sk := (t - respawnSlam) / 0.45
			if sk > 0 && sk < 1 {
				rx, ry := 14+18*pixelfx.Ease.OutCubic(sk), 3+1.5*sk
				p.EllipseFn(0, 0.5, rx, ry, func(d, _ float64) (uint32, float64) {
					col := C[5]
					if d < 0.4 {
						col = pixelfx.White
					} else if d < 0.7 {
						col = C[6]
					}
					return col, (1 - sk) * 1.4 * (1 - d*d)
				})
				p.Glow(0, -2, rx*1.3, C[5], 0.5*(1-sk))
			}
			// the floor: a hot spot and rings at the slam
			for i := range 2 {
				k := (t - respawnSlam - 0.08*float64(i)) / 0.5
				if k > 0 && k < 1 {
					spread := 34.0
					if i != 0 {
						spread = 22
					}
					pixelfx.Band(p, 0, 0, 8+spread*pixelfx.Ease.OutCubic(k), 0.2, math.Max(1, 3*(1-k)), 1.15-k, C, nil, k < 0.15)
				}
			}
			// the character, written in from the feet: each row lands white, then cools to its own colors
			if t >= respawnWrite0 && t < respawnDone+0.05 {
				sy, fr := scanY(t), math.Floor(t*30)
				for _, b := range body.list {
					t0 := rowT(b.my)
					age := t - t0
					if age < 0 {
						continue
					}
					glitch := 0.0
					if age < 0.12 && pixelfx.Rand(float64(b.my), fr, 9) < 0.3 {
						if pixelfx.Rand(float64(b.my), fr, 4) < 0.5 {
							glitch = -2
						} else {
							glitch = 2
						}
					}
					var c uint32
					switch {
					case age < 0.05:
						c = pixelfx.White
					case age < 0.14:
						c = C[5]
					case age < 0.3:
						c = pixelfx.Mix(b.c, C[5], 0.5*(1-(age-0.14)/0.16))
					default:
						c = b.c
					}
					if b.edge && age < 0.3 {
						c = C[6]
					}
					p.Px(b.x+glitch, b.y, c)
				}
				// the scan bar itself: a bright line across the beam and a light riding it
				if t < respawnDone {
					p.LineFn(-w-2, sy, w+2, sy, func(u float64) (uint32, float64) {
						if math.Abs(u-0.5) < 0.35 {
							return pixelfx.White, 1
						}
						return C[5], 1
					})
					p.Line(-w, sy+1, w, sy+1, C[4], 0.6)
					p.GlowLine(-w, sy, w, sy, 6, C[5], 0.6)
				}
			}
			// done: a flash round the finished character
			fk := (t - respawnDone) / 0.25
			if fk > 0 && fk < 1 {
				col := C[5]
				if fk < 0.4 {
					col = pixelfx.White
				}
				for _, c := range rim {
					p.Dot(c.x, c.y, col, 1.2-fk)
				}
				pixelfx.Sparkle(p, 6, top+6, pixelfx.Round(6*(1-fk)), pixelfx.White, C[5], 0.5)
			}
			// the beam's front edges over the character while it stands
			if w > 0.2 && t >= respawnSlam && t-respawnSlam < 0.1 {
				p.Glow(0, -30, 56, pixelfx.White, 0.6*(1-(t-respawnSlam)/0.1))
			}
			ps.Draw(p, pixelfx.LayerBoth)
			lights.Draw(p)
		},
	}
}
