// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/warp-in.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the column (bands of the ramp across it, strands, pulses scrolling along it, guide thread, rings riding it) from Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var warpInPose = pixelfx.Pose{}.With(pixelfx.Crouch.Is(7), pixelfx.Lean.Is(3), pixelfx.FX.Is(9), pixelfx.FY.Is(-12), pixelfx.BX.Is(12), pixelfx.BY.Is(-12), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-6))

// WarpIn slams a beam down, sends rings down it, and paints the hero in from head to toe as white-hot pixels cool to their own colors before the beam pinches shut in sparks. It is registered as "scifi/warp-in".
var WarpIn = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Warp in",
		Desc:     "A beam slams down, rings run down it and a scanline paints the hero in from head to toe, white-hot pixels cooling to their own colors; the beam pinches shut in sparks.",
		Tags:     []string{"scifi", "teleport", "warp", "beam", "spawn", "arrive", "one-shot"},
		Size:     [2]int{192, 168},
		Origin:   [2]int{96, 128},
		Duration: warpInDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0b1030", "#152a63", "#1d4f9e", "#2582d2", "#36b9ee", "#8ae8ff", "#d4fbff", "#ffffff")},
			{Name: "width", Default: pixelfx.Num(15)},
			{Name: "seed", Default: pixelfx.Num(3)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: 0, Pose: warpInPose},
			{T: 1.82, Pose: warpInPose},
			{T: 2.05, Pose: pixelfx.Pose{}.With(pixelfx.Crouch.Is(3), pixelfx.FX.Is(8), pixelfx.FY.Is(-19), pixelfx.BX.Is(8), pixelfx.BY.Is(-18), pixelfx.FFX.Is(4), pixelfx.FBX.Is(-3))},
			{T: 2.5, Pose: pixelfx.Ready},
			{T: 3, Pose: pixelfx.Ready},
		},
	},
	ActorAlpha: func(t float64, _ *pixelfx.Options) float64 {
		if t >= warpInDone {
			return 1
		}
		return 0
	},
	New: newWarpIn,
}

func init() { pixelfx.Register("scifi/warp-in", WarpIn) }

const (
	warpInDuration = 3.0
	warpInTop      = -128.0
)

// the beats: the guide thread, the slam, the tube opens, the scan, the cooling, the beam narrows, it snaps shut
var (
	warpInSlam  = 0.34
	warpInOpen  = 0.52
	warpInScan0 = 0.62
	warpInScan1 = 1.4
	warpInCool  = 0.42
	warpInDone  = warpInScan1 + warpInCool
	warpInClose = 1.9
	warpInSnap  = 2.16
)

func warpInScanY(t, y0 float64) float64 {
	return pixelfx.Lerp(y0-2, 1, pixelfx.Ease.InOutSine(pixelfx.Clamp((t-warpInScan0)/(warpInScan1-warpInScan0))))
}

func warpInScanT(y, y0 float64) float64 {
	return warpInScan0 + ((warpInScan1-warpInScan0)*math.Acos(1-2*pixelfx.Clamp((y+0.5-(y0-2))/(3-y0))))/pi
}

func warpInWidthAt(t, W float64) float64 {
	if t < warpInSlam {
		return 0
	}
	if t < warpInClose {
		return W * (1 + 0.45*math.Exp(-(t-warpInSlam)*14) + 0.03*math.Sin(t*31))
	}
	return pixelfx.Lerp(W, 0.7, pixelfx.Ease.OutCubic(pixelfx.Clamp((t-warpInClose)/(warpInSnap-warpInClose))))
}

type warpInBit struct {
	x, y  float64
	c     uint32
	edge  bool
	ts, j float64
}

func newWarpIn(o *pixelfx.Options) pixelfx.Instance {
	C, W, seed, A := pixelfx.Fit(o.Palette(), 8), o.Num("width"), o.Seed(), o.Actor
	R := pixelfx.NewRng(seed)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: zeroGround, Sparks: []uint32{pixelfx.White, C[6], C[5], C[3]}, Haze: C[5]})
	lights := &pixelfx.Lights{}

	// the body: each filled pixel, its own color, whether it's on the outline, and each row's extent
	var body []warpInBit
	rows := map[float64][2]float64{}
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < A.W && y < A.H && A.Mask.Data[y*A.W+x] != 0 }
	for my := range A.H {
		for mx := range A.W {
			i := my*A.W + mx
			if A.Mask.Data[i] == 0 {
				continue
			}
			x, y := A.X+float64(mx), A.Y+float64(my)
			edge := !on(mx-1, my) || !on(mx+1, my) || !on(mx, my-1) || !on(mx, my+1)
			c := A.Pixels[i]
			if c == 0 {
				c = C[2]
			}
			body = append(body, warpInBit{x: x, y: y, c: c, edge: edge, ts: warpInScanT(y, A.Y), j: pixelfx.Rand(seed, float64(mx), float64(my))})
			if r, ok := rows[y]; ok {
				rows[y] = [2]float64{math.Min(r[0], x), math.Max(r[1], x)}
			} else {
				rows[y] = [2]float64{x, x}
			}
		}
	}

	var rings []float64
	t, slammed, snapped, nextRing, lastRow := 0.0, false, false, warpInSlam+0.08, A.Y-3

	column := func(p *pixelfx.Painter) {
		w := warpInWidthAt(t, W)
		if w <= 0.05 || t > warpInSnap+0.14 {
			return
		}
		// it falls from the sky as it lands, and snaps back up into it at the end
		bottom := 0.0
		if t-warpInSlam < 0.06 {
			bottom = pixelfx.Lerp(warpInTop, 0, pixelfx.Ease.InCubic(pixelfx.Clamp((t-warpInSlam)/0.06)*0.5+0.5))
		} else if t > warpInSnap {
			bottom = pixelfx.Lerp(0, warpInTop, pixelfx.Ease.OutExpo((t-warpInSnap)/0.14))
		}
		// the core hollows out from the middle into a tube of light, and the tube's walls close into a thread
		hero := t-warpInSlam < 0.07
		hollow := 0.78 * pixelfx.Smooth(warpInOpen-0.16, warpInOpen, t)
		thin := 1 - pixelfx.Clamp((w-1.5)/(W-1.5))
		for y := math.Floor(warpInTop); y < bottom; y++ {
			pulse := 0
			if pixelfx.Wrap((y-warpInTop)*0.028-t*2.6, 1) < 0.09 {
				pulse = 1
			}
			for x := math.Floor(-w * 1.35); x <= math.Ceil(w*1.35); x++ {
				q := math.Abs(x+0.5) / w
				if q > 1.35 {
					continue
				}
				if hero {
					c := C[4]
					switch {
					case q < 0.55:
						c = pixelfx.White
					case q < 0.8:
						c = C[6]
					case q < 1:
						c = C[5]
					}
					a := 1.0
					if q >= 1 {
						a = (1.35 - q) / 0.35
					}
					p.Dot(x, y, c, a)
					continue
				}
				if w < 2.4 {
					c := C[6]
					if q < 0.7 {
						c = pixelfx.White
					}
					a := 1.0
					if q >= 1 {
						a = (1.35 - q) / 0.35
					}
					p.Dot(x, y, c, a)
					continue
				}
				// strands: thin bright threads drifting inside the beam
				strand := 0
				for s := range 5 {
					fs := float64(s)
					if math.Abs((x+0.5)/w-(pixelfx.Rand(seed, fs)-0.5)*1.3-0.06*math.Sin(t*(1.5+fs)+fs*2)) < 0.05 {
						strand = 1
					}
				}
				switch {
				case q < hollow:
					c := C[5]
					if strand != 0 || pulse != 0 {
						c = C[6]
					}
					p.Blend(x+0.5, y+0.5, c, pixelfx.Clamp(0.12+0.3*q*q+0.3*float64(strand)+0.25*float64(pulse)+0.7*thin))
				case q < 0.78:
					base := 2
					if q < 0.22 {
						base = 0
					} else if q < 0.45 {
						base = 1
					}
					lvl := max(0, base-pulse-strand)
					c := pixelfx.White
					if lvl != 0 {
						c = C[7-lvl]
					}
					p.Px(x, y, c)
				case q < 0.96:
					c := C[6]
					if q > 0.86 && q < 0.93 {
						c = pixelfx.White
					}
					p.Px(x, y, c)
				default:
					c := C[4]
					if q < 1.1 {
						c = C[5]
					}
					p.Dot(x, y, c, (1.35-q)/0.39)
				}
			}
			// light rides the walls, not the middle, so the body being built inside stays readable
			if pixelfx.ToInt32(y)&7 == 0 {
				r := math.Min(w+8, y-warpInTop+2)
				if hollow < 0.4 {
					k := 0.32
					if hero {
						k = 0.55
					}
					p.Glow(0, y, math.Min(w*2.6+6, y-warpInTop+2), C[4], k)
				} else {
					for _, s := range [2]float64{-1, 1} {
						p.Glow(s*(w+3), y, r, C[4], 0.24)
					}
				}
			}
		}
	}

	// a ring riding down the beam: the near half in front, the far half behind
	ring := func(p *pixelfx.Painter, near bool) {
		w := warpInWidthAt(t, W)
		keep := func(g float64) bool { return (math.Sin(g) >= 0) == near }
		for _, t0 := range rings {
			k := (t - t0) / 0.5
			if k < 0 || k >= 1 {
				continue
			}
			y := pixelfx.Lerp(warpInTop+6, -1, pixelfx.Ease.InQuad(k))
			rx := w*1.18 + 1
			c := C[6]
			if k < 0.25 {
				c = pixelfx.White
			}
			pixelfx.Band(p, 0, y, rx, 2.6/rx, 1.5, 1, []uint32{C[3], C[5], c, pixelfx.White}, keep, false)
			if near {
				p.Glow(0, y, rx+6, C[5], 0.25)
			}
		}
	}

	// the floor where the beam stands: a pool of light, ripples where rings land, the slam's shockwave
	floor := func(p *pixelfx.Painter, near bool) {
		var on float64
		if t < warpInSlam {
			on = pixelfx.Smooth(0, warpInSlam, t) * 0.5
		} else {
			on = 1 - pixelfx.Smooth(warpInSnap, warpInSnap+0.5, t)
		}
		if !near && on > 0.01 {
			rx := W * 1.9
			if t >= warpInSlam {
				rx += 2 * math.Sin(t*6)
			}
			ry := 4.0
			for y := -4.0; y <= 4; y++ {
				for x := math.Floor(-rx); x <= math.Ceil(rx); x++ {
					d := pixelfx.Hypot((x+0.5)/rx, (y+0.5)/ry)
					if d > 1 {
						continue
					}
					c := C[4]
					switch {
					case d < 0.35:
						c = C[7]
					case d < 0.6:
						c = C[6]
					case d < 0.85:
						c = C[5]
					}
					p.Dot(x, y, c, on*pixelfx.Clamp((1.05-d)*2.4))
				}
			}
			for _, x := range [3]float64{-rx * 0.6, 0, rx * 0.6} {
				p.Glow(x, -1, rx*1.3, C[4], 0.3*on)
			}
		}
		keep := func(g float64) bool { return (math.Sin(g) >= 0) == near }
		for _, t0 := range rings {
			k := (t - t0 - 0.5) / 0.35
			if k >= 0 && k < 1 {
				pixelfx.Band(p, 0, 0, W*(1.2+1.3*pixelfx.Ease.OutCubic(k)), 0.16, 1.5, 1-k, []uint32{C[3], C[4], C[5], C[6]}, keep, false)
			}
		}
		for _, pair := range [2][2]float64{{0, 1}, {0.08, 0.65}} {
			k := (t - warpInSlam - pair[0]) / 0.55
			if k >= 0 && k < 1 {
				pixelfx.Band(p, 0, 0.5, 92*pair[1]*pixelfx.Ease.OutCubic(k), 0.16, math.Max(1, 5*(1-k)), 1.15-k, []uint32{C[3], C[5], C[6], pixelfx.White}, keep, k < 0.25)
			}
		}
		k := (t - warpInSnap) / 0.45
		if k >= 0 && k < 1 {
			pixelfx.Band(p, 0, 0, 10+26*pixelfx.Ease.OutCubic(k), 0.16, 2*(1-k)+0.5, 1.1-k, []uint32{C[4], C[5], C[6], pixelfx.White}, keep, k < 0.3)
		}
	}

	// the hero, built out of light: the blueprint first, then each row as the scanline crosses it, white-hot and cooling through the beam's color to its own
	hero := func(p *pixelfx.Painter) {
		if t >= warpInDone || t < warpInScan0-0.2 {
			return
		}
		blink := math.Floor(t * 24)
		preAlpha := 0.8 * pixelfx.Smooth(warpInScan0-0.25, warpInScan0-0.05, t)
		for _, b := range body {
			a := t - b.ts
			if a < 0 {
				if b.edge && pixelfx.Rand(b.x, b.y, blink) < 0.6 {
					p.Dot(b.x, b.y, C[1], preAlpha)
				}
				continue
			}
			k := a / warpInCool
			// the newest rows jitter sideways a pixel, as if still arriving
			dx := 0.0
			if a < 0.06 && b.j < 0.3 {
				if b.j < 0.15 {
					dx = -1
				} else {
					dx = 1
				}
			}
			c := b.c
			switch {
			case k < 0.18:
				c = pixelfx.White
			case k < 0.45:
				c = pixelfx.Mix(b.c, C[6], 0.72)
			case k < 0.75:
				c = pixelfx.Mix(b.c, C[5], 0.38)
			}
			p.Px(b.x+dx, b.y, c)
			if k < 0.5 {
				p.Light(b.x+dx, b.y, C[5], 0.22*(1-k/0.5))
			}
		}
	}

	// the scanline: a white rule across the beam, hottest where it crosses the body, sparking at its edges
	scanline := func(p *pixelfx.Painter) {
		if t < warpInScan0 || t > warpInScan1 {
			return
		}
		y := math.Floor(warpInScanY(t, A.Y))
		w := warpInWidthAt(t, W) * 1.5
		r, hasR := rows[y]
		for x := math.Floor(-w); x <= math.Ceil(w); x++ {
			inside := hasR && x >= r[0] && x <= r[1]
			c, a := C[6], 1-math.Abs(x+0.5)/(w+1)
			if inside {
				c, a = pixelfx.White, 1
			}
			p.Dot(x, y, c, a)
		}
		p.GlowLine(-w, y, w, y, 5, C[5], 0.5)
		if hasR {
			for _, x := range r {
				pixelfx.Sparkle(p, x+0.5, y, 2, C[6], C[5], 0.3)
			}
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// gathering: energy streaks in toward the spot, vapour slides along the floor
			if t < warpInSlam-0.04 {
				if R.Chance(0.8) {
					a, D, T := R.Range(-pi*0.95, -pi*0.05), R.Range(30, 64), R.Range(0.18, 0.28)
					v := (D * 3) / (math.Exp(3*T) - 1)
					x, y := math.Cos(a)*D, math.Sin(a)*D*0.8-2
					l := pixelfx.Hypot(x, y+2)
					if l == 0 {
						l = 1
					}
					c := C[5]
					if R.Chance(0.5) {
						c = C[6]
					}
					ps.Spawn(pixelfx.Streak, x, y, (-x/l)*v, (-(y+2)/l)*v, T, c, 1, 0, 0)
				}
				if R.Chance(0.12) {
					s := R.Sign()
					x, y, vx, vy := s*R.Range(30, 56), -R.Range(0, 2), -s*R.Range(40, 70), -R.Range(0, 4)
					life, size := R.Range(0.3, 0.45), R.Range(2, 3)
					ps.Spawn(pixelfx.Vapour, x, y, vx, vy, life, C[5], size, 0, 0)
				}
			}
			if t >= warpInSlam && !slammed {
				slammed = true
				lights.Add(0, -40, 120, C[4], 1, 0.55)
				lights.Add(0, -10, 44, pixelfx.White, 0.9, 0.14)
				for range 34 {
					a := -pi/2 + R.Sign()*R.Range(0.35, 1.45)
					sp := R.Range(110, 270)
					x, life := R.Range(-W, W)*0.6, R.Range(0.25, 0.75)
					ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
				}
				for range 18 {
					s := R.Sign()
					x, y, vx, vy := s*R.Range(4, 18), -R.Range(0, 4), s*R.Range(40, 150), -R.Range(2, 18)
					life := R.Range(0.5, 0.95)
					c := C[5]
					if R.Chance(0.6) {
						c = C[6]
					}
					size := R.Range(3, 6)
					ps.Spawn(pixelfx.Vapour, x, y, vx, vy, life, c, size, 0, 0)
				}
				for range 10 {
					x, y, vx, vy := R.Range(-W, W)*1.3, -R.Range(2, 40), R.Range(-20, 20), -R.Range(10, 40)
					life := R.Range(0.4, 0.8)
					c := C[6]
					if R.Chance(0.5) {
						c = pixelfx.White
					}
					size := R.Range(1, 2.2)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, size, 0, 0)
				}
			}
			if slammed && t < warpInClose {
				// rings ride down, light falls down the beam, glints wink at its walls
				if t >= nextRing {
					rings = append(rings, nextRing)
					nextRing += 0.17
				}
				w := warpInWidthAt(t, W)
				if R.Chance(0.5) {
					x, y, vy, life := R.Range(-0.8, 0.8)*w, warpInTop+R.Range(9, 36), R.Range(60, 110), R.Range(0.3, 0.45)
					c := C[6]
					if R.Chance(0.5) {
						c = pixelfx.White
					}
					ps.Spawn(pixelfx.Streak, x, y, 0, vy, life, c, 1, 0, 0)
				}
				if R.Chance(0.18) {
					x, y, vy := R.Sign()*w*R.Range(0.8, 1.3), -R.Range(4, 70), -R.Range(5, 20)
					life, size := R.Range(0.3, 0.6), R.Range(0.6, 1.8)
					ps.Spawn(pixelfx.StarKind, x, y, 0, vy, life, C[6], size, 0, 0)
				}
				if R.Chance(0.1) {
					s := R.Sign()
					x, y, vx, vy := s*R.Range(8, 16), -R.Range(0, 3), s*R.Range(20, 50), -R.Range(2, 8)
					life, size := R.Range(0.5, 0.8), R.Range(2.5, 4)
					ps.Spawn(pixelfx.Vapour, x, y, vx, vy, life, C[5], size, 0, 0)
				}
			}
			// the scanline spits light off the edges of the body as it crosses
			if t > warpInScan0 && t < warpInScan1 {
				y := math.Floor(warpInScanY(t, A.Y))
				if y != lastRow {
					lastRow = y
					if r, ok := rows[y]; ok && R.Chance(0.7) {
						for _, pair := range [2][2]float64{{r[0], -1}, {r[1] + 1, 1}} {
							if R.Chance(0.6) {
								vx, vy, life := pair[1]*R.Range(40, 90), R.Range(-20, 10), R.Range(0.15, 0.3)
								c := C[6]
								if R.Chance(0.5) {
									c = pixelfx.White
								}
								ps.Spawn(pixelfx.Mote, pair[0], y, vx, vy, life, c, 0.6, 0, 0)
							}
						}
					}
				}
			}
			if t >= warpInDone && t-dt < warpInDone {
				// set: a glint or two on the new body
				for range 6 {
					b := body[int(math.Floor(R.Next()*float64(len(body))))]
					vx, vy, life := R.Range(-6, 6), -R.Range(6, 20), R.Range(0.4, 0.8)
					c := C[6]
					if R.Chance(0.5) {
						c = pixelfx.White
					}
					size := R.Range(1, 2)
					ps.Spawn(pixelfx.StarKind, b.x+0.5, b.y+0.5, vx, vy, life, c, size, 0, 0)
				}
			}
			if t >= warpInSnap && !snapped {
				snapped = true
				lights.Add(0, -1, 32, C[4], 0.5, 0.25)
				lights.Add(0, -1, 10, pixelfx.White, 0.5, 0.08)
				for range 22 {
					a, sp := -pi/2+R.Range(-1.4, 1.4), R.Range(90, 260)
					x, life := R.Range(-2, 2), R.Range(0.2, 0.6)
					ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, pixelfx.White, 1, 0, 0)
				}
				for range 12 {
					x, y, vx, vy := R.Range(-16, 16), -R.Range(0, 46), R.Range(-10, 10), -R.Range(8, 26)
					life := R.Range(0.5, 0.9)
					c := C[6]
					if R.Chance(0.5) {
						c = pixelfx.White
					}
					size := R.Range(0.6, 1.8)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, c, size, 0, 0)
				}
				// where the beam stood, motes keep rising for a while
				for range 18 {
					k := pixelfx.Mote
					if R.Chance(0.6) {
						k = pixelfx.StarKind
					}
					x, y, vx, vy := R.Range(-6, 6), -R.Range(10, 100), R.Range(-6, 6), -R.Range(14, 40)
					life := R.Range(0.5, 0.75)
					var c uint32
					if R.Chance(0.4) {
						c = pixelfx.White
					} else if R.Chance(0.5) {
						c = C[6]
					} else {
						c = C[5]
					}
					size, delay := R.Range(0.5, 1.4), R.Range(0, 0.25)
					ps.Spawn(k, x, y, vx, vy, life, c, size, delay, 0)
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			floor(p, false)
			ring(p, false)
			column(p)
			// the guide thread: a dotted line pricking down to the spot, brightening
			if t < warpInSlam {
				a := pixelfx.Smooth(0, warpInSlam*0.8, t) * 0.8
				head := pixelfx.Lerp(warpInTop, 0, pixelfx.Ease.OutCubic(pixelfx.Clamp(t/(warpInSlam*0.6))))
				for y := warpInTop; y < head; y++ {
					c := C[5]
					if pixelfx.ToInt32(y)&1 == 0 {
						c = C[6]
					}
					p.Dot(0, y, c, a*(0.35+0.65*((y-warpInTop)/-warpInTop)))
				}
				p.Glow(0, head, 10, C[5], 0.5*a)
				// a target on the floor, closing in
				rx := pixelfx.Lerp(34, 12, pixelfx.Ease.OutCubic(t/warpInSlam))
				k := pixelfx.Smooth(0, 0.15, t)
				for i := range 48 {
					g := (float64(i)/48)*tau + t*3
					if (i/4)%2 != 0 {
						continue
					}
					c := C[5]
					if i%4 == 0 {
						c = pixelfx.White
					}
					p.Dot(math.Cos(g)*rx, math.Sin(g)*rx*0.16, c, k)
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			hero(p)
			scanline(p)
			ring(p, true)
			floor(p, true)
			// the set body's outline flares as it lands, then fades
			if t >= warpInDone && t < warpInDone+0.3 {
				p.Outline(A.Mask, A.X, A.Y, C[6], 1-(t-warpInDone)/0.3)
			}
			if t > warpInSlam && t < warpInSlam+0.16 {
				pixelfx.Sparkle(p, 0, -2, 12*(1-(t-warpInSlam)/0.16), C[6], C[5], 0.6)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
