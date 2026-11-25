// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/warp-out.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx follows Sprite Fusion's open-source blink (https://www.spritefusion.com/pixel-art-vfx/movement) for the breakup (outline flickering on, white hero frame, body bursting into bits of light), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	warpOutSet    = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(3), pixelfx.FX.Is(-9), pixelfx.FY.Is(-15), pixelfx.BX.Is(10), pixelfx.BY.Is(-15), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	warpOutRaised = warpOutSet.With(pixelfx.Crouch.Is(0), pixelfx.FX.Is(-11), pixelfx.FY.Is(-29), pixelfx.BX.Is(12), pixelfx.BY.Is(-29))
)

// WarpOut flickers the hero's outline on, flashes them white and breaks them into light from the head down, streaming up a beam that lifts off the floor and pinches shut. It is registered as "scifi/warp-out".
var WarpOut = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Warp out",
		Desc:     "The hero's outline flickers on, they flash white and break into light from the head down, streaming up a beam that lifts off the floor and pinches shut.",
		Tags:     []string{"scifi", "teleport", "warp", "beam", "despawn", "leave", "one-shot"},
		Size:     [2]int{128, 168},
		Origin:   [2]int{64, 128},
		Duration: warpOutDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0b1030", "#152a63", "#1d4f9e", "#2582d2", "#36b9ee", "#8ae8ff", "#d4fbff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: -0.3, Pose: pixelfx.Ready},
			{T: 0.1, Pose: warpOutSet},
			{T: 0.42, Pose: warpOutRaised, How: "snap"},
			{T: 2.6, Pose: warpOutRaised},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t < warpOutFlash {
			return 1
		}
		return 0
	},
	New: newWarpOut,
}

func init() { pixelfx.Register("scifi/warp-out", WarpOut) }

const (
	warpOutDuration = 2.6
	warpOutTop      = -128.0
)

// the beats: it charges, flashes white, breaks up into the beam, the beam lifts away
var (
	warpOutFlash  = 0.42
	warpOutBreak0 = 0.5
	warpOutBreak1 = 1.1
	warpOutLift   = 1.3
	warpOutGone   = 1.62
)

type warpOutBit struct {
	x, y, x0, y0, vx, vy, tr, j float64
	c                           uint32
	free, rim                   bool
}

func newWarpOut(o *pixelfx.Options) pixelfx.Instance {
	C, R, A := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Actor
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[4]}})
	lights := &pixelfx.Lights{}

	// the body: every pixel, its own color, whether it's on the edge, when it lets go (head first)
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < A.W && y < A.H && A.Mask.Data[y*A.W+x] != 0 }
	var bits []warpOutBit
	var edge [][2]float64
	for my := -1; my <= A.H; my++ {
		for mx := -1; mx <= A.W; mx++ {
			if on(mx, my) {
				x, y := A.X+float64(mx), A.Y+float64(my)
				tr := warpOutBreak0 + (float64(my)/float64(A.H))*(warpOutBreak1-warpOutBreak0-0.12) + R.Range(0, 0.12)
				rim := !on(mx-1, my) || !on(mx+1, my) || !on(mx, my-1) || !on(mx, my+1)
				c := A.Pixels[my*A.W+mx]
				if c == 0 {
					c = C[3]
				}
				bits = append(bits, warpOutBit{x: x, y: y, x0: x, y0: y, tr: tr, c: c, j: R.Next(), rim: rim})
			} else if on(mx-1, my) || on(mx+1, my) || on(mx, my-1) || on(mx, my+1) {
				edge = append(edge, [2]float64{A.X + float64(mx), A.Y + float64(my)})
			}
		}
	}

	t, flashed, lifted := 0.0, false, false
	// the beam's half width and how far its foot has lifted off the floor
	width := func() float64 {
		if t < warpOutBreak0 {
			return 0
		}
		if t < warpOutLift {
			return 2 + 7*pixelfx.Ease.OutCubic(pixelfx.Clamp((t-warpOutBreak0)/0.35))
		}
		return pixelfx.Lerp(9, 0.6, pixelfx.Ease.InCubic(pixelfx.Clamp((t-warpOutLift)/(warpOutGone-warpOutLift))))
	}
	foot := func() float64 {
		if t < warpOutLift {
			return 0
		}
		return pixelfx.Lerp(0, warpOutTop, pixelfx.Ease.InQuad(pixelfx.Clamp((t-warpOutLift)/(warpOutGone-warpOutLift))))
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// charging: motes gather to the body, the floor target closes in
			if t < warpOutFlash-0.05 && R.Chance(0.7) {
				a, D, T := R.Range(0, tau), R.Range(26, 48), R.Range(0.16, 0.26)
				v, cy := (D*3)/(math.Exp(3*T)-1), -19.0
				c := C[5]
				if R.Chance(0.5) {
					c = C[6]
				}
				ps.Spawn(pixelfx.Streak, math.Cos(a)*D, cy+math.Sin(a)*D*0.8, -math.Cos(a)*v, -math.Sin(a)*v*0.8, T, c, 1, 0, 0)
			}
			if t >= warpOutFlash && !flashed {
				flashed = true
				lights.Add(0, -20, 80, C[4], 0.9, 0.5)
				lights.Add(0, -20, 30, pixelfx.White, 0.8, 0.12)
				for range 14 {
					x, y, vx, vy := R.Range(-14, 14), -R.Range(2, 40), R.Range(-20, 20), -R.Range(10, 40)
					life := R.Range(0.3, 0.6)
					c := C[6]
					if R.Chance(0.5) {
						c = pixelfx.White
					}
					size := R.Range(0.8, 1.8)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, size, 0, 0)
				}
			}
			// the body lets go bit by bit: a little burst outward, then hauled up the beam, drawn in to its middle
			for i := len(bits) - 1; i >= 0; i-- {
				b := &bits[i]
				if t < b.tr {
					continue
				}
				if !b.free {
					b.free = true
					ox, oy := b.x+0.5, b.y+19
					b.vx = ox*R.Range(1.5, 4) + R.Range(-10, 10)
					b.vy = oy*R.Range(0.5, 1.5) - R.Range(10, 40)
				}
				b.vy -= 560 * dt
				b.vx += (-(b.x+0.5)*14 - b.vx*3) * dt
				b.x += b.vx * dt
				b.y += b.vy * dt
				if b.y < warpOutTop+2 {
					bits = slices.Delete(bits, i, i+1)
				}
			}
			if t >= warpOutLift && !lifted {
				lifted = true
				lights.Add(0, -2, 40, C[4], 0.7, 0.3)
				for range 22 {
					a, sp := -pi/2+R.Range(-1.4, 1.4), R.Range(80, 220)
					x, life := R.Range(-3, 3), R.Range(0.2, 0.55)
					ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
				}
				for range 16 {
					x, y, vx, vy := R.Range(-10, 10), -R.Range(4, 70), R.Range(-6, 6), -R.Range(10, 30)
					life := R.Range(0.5, 0.95)
					c := C[6]
					if R.Chance(0.4) {
						c = pixelfx.White
					}
					size, delay := R.Range(0.6, 1.6), R.Range(0, 0.3)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, size, delay, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// the floor target: a ring closing in on the feet, then the beam's foot glowing
			k := pixelfx.Clamp(t / warpOutFlash)
			fade := 1 - pixelfx.Smooth(warpOutLift, warpOutLift+0.6, t)
			if fade > 0 {
				pixelfx.Band(p, 0, 0, pixelfx.Lerp(30, 14, pixelfx.Ease.OutCubic(k)), 0.16, 1.5, (0.4+0.6*k)*fade, []uint32{C[3], C[4], C[5], C[6]}, func(g float64) bool { return math.Sin(g) < 0 }, false)
			}
			// the beam: thin bright walls, a glassy inside, from its foot to the sky
			w, f := width(), foot()
			if w > 0.3 && t < warpOutGone {
				for y := math.Floor(warpOutTop); y < f; y++ {
					if y >= f && t >= warpOutLift {
						break
					}
					for x := math.Floor(-w) - 1; x <= math.Ceil(w); x++ {
						q := math.Abs(x+0.5) / w
						if q > 1+1/w {
							continue
						}
						switch {
						case w < 2.2:
							c := C[6]
							if q < 0.7 {
								c = pixelfx.White
							}
							p.Px(x, y, c)
						case q > 1:
							p.Dot(x, y, C[4], 0.5)
						case q > 1-1.2/w:
							p.Px(x, y, C[6])
						default:
							p.Blend(x+0.5, y+0.5, C[5], 0.12+0.25*q*q)
						}
					}
					if pixelfx.ToInt32(y)&7 == 0 {
						p.Glow(0, y, math.Min(w*2.5+6, y-warpOutTop+2), C[4], 0.28)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			k := pixelfx.Clamp(t / warpOutFlash)
			fade := 1 - pixelfx.Smooth(warpOutLift, warpOutLift+0.6, t)
			if fade > 0 {
				pixelfx.Band(p, 0, 0, pixelfx.Lerp(30, 14, pixelfx.Ease.OutCubic(k)), 0.16, 1.5, (0.4+0.6*k)*fade, []uint32{C[3], C[5], C[6], pixelfx.White}, func(g float64) bool { return math.Sin(g) >= 0 }, false)
			}
			// charging: the outline flickers on round the hero, denser as it builds
			if t < warpOutFlash {
				fr := math.Floor(t * 24)
				for _, pt := range edge {
					x, y := pt[0], pt[1]
					if pixelfx.Rand(x, y, fr) < 0.25+0.7*k {
						c := C[5]
						if k > 0.7 && pixelfx.Rand(x, y, fr, 1) < 0.5 {
							c = pixelfx.White
						}
						p.Px(x, y, c)
					}
				}
				p.Glow(0, -20, 26, C[4], 0.4*k)
			}
			// the body, still held together: white in the flash, then cooling to the beam's color as it waits its turn
			for i := range bits {
				b := &bits[i]
				if b.free || t < warpOutFlash {
					continue
				}
				a := t - warpOutFlash
				scan := pixelfx.Wrap(b.y+math.Floor(t*40), 4) == 0
				var c uint32
				switch {
				case a < 0.07:
					c = pixelfx.White
				case a < 0.16 || b.rim:
					c = C[6]
				case scan:
					c = pixelfx.Mix(b.c, C[6], 0.6)
				default:
					c = pixelfx.Mix(b.c, C[5], 0.45)
				}
				if b.tr-t < 0.06 {
					c = pixelfx.White
				}
				p.Px(b.x, b.y, c)
			}
			// the bits on their way up: streaks, longer the faster they go, cooling as they climb
			for i := range bits {
				b := &bits[i]
				if !b.free {
					continue
				}
				sp := math.Abs(b.vy)
				L := math.Min(8, sp*0.02)
				u := pixelfx.Clamp((b.y - warpOutTop) / (b.y0 - warpOutTop))
				head := C[5]
				if u > 0.75 {
					head = pixelfx.White
				} else if u > 0.4 {
					head = C[6]
				}
				// one in three draws a streak, the rest a single spark, so the stream reads as light, not static
				if L >= 1 && b.j < 0.33 {
					p.LineFn(b.x, b.y, b.x-b.vx*0.01, b.y+L, func(s float64) (uint32, float64) {
						switch {
						case s < 0.25:
							return head, 1
						case s < 0.6:
							return C[5], 1
						}
						return C[3], 1
					})
				} else if b.j < 0.75 || u > 0.6 {
					c := head
					if b.j > 0.9 {
						c = pixelfx.White
					}
					p.Px(b.x, b.y, c)
				}
			}
			if t >= warpOutFlash && t < warpOutFlash+0.12 {
				pixelfx.Sparkle(p, 0, -20, 10*(1-(t-warpOutFlash)/0.12), C[6], C[5], 0.6)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
