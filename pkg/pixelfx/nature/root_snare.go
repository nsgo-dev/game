// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/root-snare.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the hold-then-break (it closes round the character, holds with glints, cracks, then bursts into tumbling pieces, mist and light) from Sprite Fusion's open-source ice freeze (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var rootSnareGrab = pixelfx.Pose{}.With(
	pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(3),
	pixelfx.FX.Is(-12), pixelfx.FY.Is(-34),
	pixelfx.BX.Is(12), pixelfx.BY.Is(-28),
	pixelfx.FFX.Is(-2), pixelfx.FBX.Is(3),
)

// RootSnare is roots that burst from a cracking floor, spiral round a character's legs, cinch tight, hold, then crumble into bark and dust. It is registered as "nature/root-snare".
var RootSnare = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Root snare",
		Desc:     "The floor cracks and roots burst up round the character, spiralling round its legs; they cinch tight with a snap, hold, then crack and crumble into bark and dust.",
		Tags:     []string{"nature", "roots", "plant", "snare", "root", "status", "actor", "one-shot"},
		Size:     [2]int{128, 96},
		Origin:   [2]int{64, 78},
		Duration: rootSnareDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#140c08", "#2a1a10", "#46301e", "#64462a", "#86623a", "#a8824e", "#ccaa70", "#ecd6a0")},
			{Name: "glow", Default: pixelfx.Palette("#123818", "#2f7a2a", "#6cb83a", "#c8f070")},
			{Name: "seed", Default: pixelfx.Num(7)},
		},
	},
	Act: &pixelfx.Act{
		Poses: []pixelfx.Key{
			{T: -0.24, Pose: pixelfx.Ready.With(pixelfx.FFX.Is(-2), pixelfx.FBX.Is(3))},
			{T: 0.3, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Lean.Is(-1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-10), pixelfx.FY.Is(-24), pixelfx.BX.Is(11), pixelfx.BY.Is(-24), pixelfx.FFX.Is(-2), pixelfx.FBX.Is(3))},
			{T: 0.62, Pose: rootSnareGrab, How: "snap"},
			{T: 0.86, Pose: rootSnareGrab.With(pixelfx.Lean.Is(-3), pixelfx.FY.Is(-37), pixelfx.BY.Is(-27))},
			{T: 1.1, Pose: rootSnareGrab.With(pixelfx.Lean.Is(3), pixelfx.FY.Is(-27), pixelfx.BY.Is(-37))},
			{T: 1.34, Pose: rootSnareGrab.With(pixelfx.Lean.Is(-3), pixelfx.FY.Is(-37), pixelfx.BY.Is(-27))},
			{T: 1.58, Pose: rootSnareGrab.With(pixelfx.Lean.Is(3), pixelfx.FY.Is(-27), pixelfx.BY.Is(-37))},
			{T: 1.75, Pose: rootSnareGrab.With(pixelfx.Crouch.Is(6))},
			{T: 1.8, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1.1), pixelfx.Crouch.Is(-1), pixelfx.Y.Is(-3), pixelfx.FX.Is(-14), pixelfx.FY.Is(-36), pixelfx.BX.Is(15), pixelfx.BY.Is(-36), pixelfx.FFX.Is(-3), pixelfx.FFY.Is(-6), pixelfx.FBX.Is(4), pixelfx.FBY.Is(-6)), How: "snap"},
			{T: 2.05, Pose: pixelfx.Ready.With(pixelfx.Y.Is(0), pixelfx.Crouch.Is(5))},
			{T: 2.7, Pose: pixelfx.Ready.With(pixelfx.FFX.Is(-2), pixelfx.FBX.Is(3))},
		},
		Hits: []pixelfx.Hit{
			{T: 0.62, Stop: 0.05, Shake: 0.1, Amp: pixelfx.Val(1)},
			{T: 1.75, Stop: 0.04, Shake: 0.14, Amp: pixelfx.Val(2)},
		},
	},
	New: newRootSnare,
}

func init() { pixelfx.Register("nature/root-snare", RootSnare) }

const (
	rootSnareDuration = 2.7
	rootSnareSY       = 0.34
	rootSnareNP       = 48
)

var (
	rootSnareCinch = 0.62
	rootSnareBreak = 1.75
)

type snareRoot struct {
	th0, dir, turns, r0, leg, h, at float64
	buds                            [2]float64
	burst                           bool
}

type snarePt struct {
	x, y, z, s float64
}

type barkChip struct {
	x, y, vx, vy, a, va, s float64
	pts                    [][2]float64
	life                   float64
}

// rootPath computes one root's spiral path from outside the feet round the legs and up.
func rootPath(r snareRoot, cinch float64) [rootSnareNP + 1]snarePt {
	var pts [rootSnareNP + 1]snarePt
	for i := 0; i <= rootSnareNP; i++ {
		s := float64(i) / rootSnareNP
		th := r.th0 + r.dir*tau*r.turns*s
		rad := pixelfx.Lerp(r.r0, r.leg-cinch*1.6, pixelfx.Ease.OutCubic(math.Min(1, s*3.5)))
		pts[i] = snarePt{x: math.Cos(th) * rad, y: -r.h*math.Pow(s, 1.05) + math.Sin(th)*rad*rootSnareSY, z: math.Sin(th), s: s}
	}
	return pts
}

// drawRoot draws a stroke of bark along the path: lit on its upper side, dark beneath, a ridge of grain, rootlet hairs.
func drawRoot(p *pixelfx.Painter, pts *[rootSnareNP + 1]snarePt, g float64, C []uint32, back bool, a float64) {
	n := int(math.Floor(rootSnareNP * g))
	dim := 0
	if back {
		dim = 1
	}
	for i := 0; i < n; i++ {
		A, Bp := pts[i], pts[i+1]
		front := (A.z+Bp.z)/2 >= 0
		if front == back {
			continue
		}
		dx, dy := Bp.x-A.x, Bp.y-A.y
		L := pixelfx.Hypot(dx, dy)
		if L == 0 {
			L = 1
		}
		nx, ny := -dy/L, dx/L
		up := -1.0
		if nx*-0.6+ny*-0.8 > 0 {
			up = 1
		}
		w := (1.4+3.6*math.Pow(1-A.s, 0.8))/2 + 0.35
		y0 := math.Floor(math.Min(A.y, Bp.y) - w - 1)
		y1 := math.Ceil(math.Max(A.y, Bp.y) + w + 1)
		x0 := math.Floor(math.Min(A.x, Bp.x) - w - 1)
		x1 := math.Ceil(math.Max(A.x, Bp.x) + w + 1)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				if y > 0 {
					continue
				}
				px, py := x+0.5-A.x, y+0.5-A.y
				u := (px*dx + py*dy) / (L * L)
				if u < -0.2 || u > 1.2 {
					continue
				}
				d := px*nx + py*ny
				if math.Abs(d) > w {
					continue
				}
				side := pixelfx.Sign(d) * up
				q := math.Abs(d) / w
				var i2 int
				switch {
				case q > 0.72:
					if side > 0 {
						i2 = 7
					} else {
						i2 = 1
					}
				case side > 0:
					if q > 0.3 {
						i2 = 6
					} else {
						i2 = 5
					}
				default:
					if q > 0.35 {
						i2 = 2
					} else {
						i2 = 3
					}
				}
				if q < 0.25 && pixelfx.Rand(pixelfx.Round(A.s*40), 3) < 0.4 {
					i2 = 4
				}
				p.Dot(x, y, C[max(1, i2-dim)], a)
			}
		}
		// rootlet hairs off its underside now and then
		if i%7 == 3 && w > 1.2 {
			p.Dot(A.x-nx*up*(w+1), A.y-ny*up*(w+1), C[2-dim], a)
		}
	}
}

func newRootSnare(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	G := pixelfx.Fit(o.Ramp("glow"), 4)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{G[3], G[2], G[1], G[0]}})
	lights := &pixelfx.Lights{}
	var chips []barkChip
	roots := make([]snareRoot, 5)
	for i := range roots {
		dir := -1.0
		if i%2 != 0 {
			dir = 1
		}
		th0 := (float64(i)/5)*tau + R.Range(-0.3, 0.3)
		turns := R.Range(1.1, 1.6)
		r0 := R.Range(14, 19)
		leg := R.Range(8, 10)
		h := R.Range(13, 21)
		at := 0.16 + float64(i)*0.07 + R.Range(0, 0.04)
		b0 := R.Range(0.35, 0.55)
		b1 := R.Range(0.6, 0.85)
		roots[i] = snareRoot{th0: th0, dir: dir, turns: turns, r0: r0, leg: leg, h: h, at: at, buds: [2]float64{b0, b1}}
	}
	cracks := [][][2]float64{
		floorCrack(R, -1, R.Range(18, 26)),
		floorCrack(R, 1, R.Range(18, 26)),
	}
	t := 0.0
	cinched, broken := false, false
	grow := func(r snareRoot) float64 { return pixelfx.Ease.OutCubic(pixelfx.Clamp((t - r.at) / 0.32)) }
	squeeze := func() float64 {
		return pixelfx.Smooth(rootSnareCinch-0.03, rootSnareCinch+0.05, t) * (1 - 0.4*pixelfx.Smooth(rootSnareCinch+0.05, rootSnareCinch+0.25, t))
	}
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			if t < 0.4 && R.Chance(0.3*f) {
				sd := R.Sign()
				px := sd * R.Range(4, 22)
				vx := R.Range(-20, 20)
				vy := -R.Range(30, 90)
				life := R.Range(0.5, 0.9)
				c := pixelfx.Pick(R, []uint32{C[5], C[4]})
				ps.Spawn(pixelfx.Grit, px, -1, vx, vy, life, c, 1, 0, 0)
			}
			for i := range roots {
				r := &roots[i]
				if !r.burst && t >= r.at {
					r.burst = true
					x := math.Cos(r.th0) * r.r0
					for range 4 {
						vx := R.Range(-60, 60)
						vy := -R.Range(60, 160)
						a := R.Range(0, tau)
						va := R.Range(-15, 15)
						s := R.Range(1.2, 2)
						pts := stoneShape(R, 0)
						life := R.Range(0.6, 0.9)
						chips = append(chips, barkChip{x: x, y: -1, vx: vx, vy: vy, a: a, va: va, s: s, pts: pts, life: life})
					}
					for range 3 {
						vx := R.Range(-25, 25)
						vy := -R.Range(4, 18)
						life := R.Range(0.4, 0.8)
						sz := R.Range(2, 3.5)
						ps.Spawn(pixelfx.Dust, x, -1, vx, vy, life, C[5], sz, 0, 0)
					}
					lights.Add(x, -4, 20, G[2], 0.35, 0.3)
				}
			}
			if !cinched && t >= rootSnareCinch {
				cinched = true
				lights.Add(0, -10, 40, G[3], 0.5, 0.3)
				lights.Add(0, -10, 16, pixelfx.White, 0.4, 0.06)
				for range 8 {
					px := R.Range(-12, 12)
					py := -R.Range(0, 4)
					vx := R.Sign() * R.Range(20, 60)
					vy := -R.Range(4, 14)
					life := R.Range(0.5, 0.9)
					sz := R.Range(2, 3.5)
					ps.Spawn(pixelfx.Dust, px, py, vx, vy, life, C[5], sz, 0, 0)
				}
				for range 6 {
					px := R.Range(-9, 9)
					py := -R.Range(4, 20)
					life := R.Range(0.25, 0.5)
					sz := R.Range(0.8, 1.8)
					delay := R.Range(0, 0.1)
					ps.Spawn(pixelfx.StarKind, px, py, 0, -4, life, G[3], sz, delay, 0)
				}
			}
			// while it holds, motes of green rise off the roots
			if t > rootSnareCinch && t < rootSnareBreak && R.Chance(0.06*f) {
				px := R.Range(-9, 9)
				py := -R.Range(3, 18)
				vx := R.Range(-6, 6)
				vy := -R.Range(14, 30)
				life := R.Range(0.5, 0.9)
				ps.Spawn(pixelfx.Mote, px, py, vx, vy, life, G[3], 0.6, 0, 0)
			}
			if !broken && t >= rootSnareBreak {
				broken = true
				lights.Add(0, -10, 36, G[2], 0.4, 0.25)
				// every root crumbles into bark chunks and splinters along its length
				for _, r := range roots {
					pts := rootPath(r, 1)
					for i := 2; i < rootSnareNP; i += 4 {
						q := pts[i]
						w := 1.4 + 3.6*math.Pow(1-q.s, 0.8)
						sgn := pixelfx.Sign(q.x)
						if sgn == 0 {
							sgn = 1
						}
						vx := sgn*R.Range(25, 85) + q.x*3
						vy := -R.Range(50, 150)
						a := R.Range(0, tau)
						va := R.Range(-10, 10)
						s := pixelfx.ClampTo(w*0.75, 1.5, 3)
						shape := stoneShape(R, 0)
						life := math.Min(R.Range(0.7, 0.95), rootSnareDuration-0.05-t)
						chips = append(chips, barkChip{x: q.x, y: math.Min(q.y, -1), vx: vx, vy: vy, a: a, va: va, s: s, pts: shape, life: life})
						if i%10 == 2 {
							gvx := R.Range(-40, 40)
							gvy := -R.Range(20, 80)
							glife := math.Min(R.Range(0.6, 0.9), rootSnareDuration-0.05-t)
							col := pixelfx.Pick(R, []uint32{C[4], C[3], C[2]})
							sz := 1.0
							if R.Chance(0.3) {
								sz = 2
							}
							ps.Spawn(pixelfx.Grit, q.x, math.Min(q.y, -1), gvx, gvy, glife, col, sz, 0, 0)
						}
					}
				}
				for range 10 {
					px := R.Range(-12, 12)
					py := -R.Range(2, 16)
					vx := R.Range(-25, 25)
					vy := -R.Range(5, 20)
					life := math.Min(R.Range(0.6, 0.9), rootSnareDuration-0.05-t)
					sz := R.Range(3, 5)
					ps.Spawn(pixelfx.Smoke, px, py, vx, vy, life, C[5], sz, 0, 0)
				}
			}
			for i := len(chips) - 1; i >= 0; i-- {
				c := &chips[i]
				c.life -= dt
				if c.life <= 0 {
					chips = append(chips[:i], chips[i+1:]...)
					continue
				}
				c.vy += 950 * dt
				c.x += c.vx * dt
				c.y += c.vy * dt
				c.a += c.va * dt
				if c.y > 0 && c.vy > 0 {
					if c.vy > 110 {
						c.y = 0
						c.vy *= -0.3
						c.vx *= 0.5
					} else {
						c.y = 0
						c.vy, c.vx, c.va = 0, 0, 0
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			// cracks run out from the feet, a mossy light seeping up from them
			open := pixelfx.Ease.OutCubic(pixelfx.Clamp(t / 0.25))
			hot := pixelfx.Smooth(0, 0.3, t) * (1 - pixelfx.Smooth(0.5, 1.4, t))
			fade := 1 - pixelfx.Smooth(rootSnareDuration-0.6, rootSnareDuration-0.15, t)
			for _, c := range cracks {
				m := max(2, int(math.Ceil(float64(len(c))*open)))
				for k := 1; k < m; k++ {
					x0, y0 := c[k-1][0], c[k-1][1]
					x1, y1 := c[k][0], c[k][1]
					p.Line(x0, y0+1, x1, y1+1, C[0], fade)
					col := C[1]
					if hot > 0.5 {
						col = G[3]
					} else if hot > 0.2 {
						col = G[2]
					}
					p.Line(x0, y0, x1, y1, col, fade)
					if k%2 == 0 {
						p.Glow(x1, y1, 6, G[2], 0.3*hot)
					}
				}
			}
			hold := pixelfx.Smooth(0.3, rootSnareCinch, t) * (1 - pixelfx.Smooth(rootSnareBreak, rootSnareBreak+0.3, t)) * (0.8 + 0.2*math.Sin(t*8))
			if hold > 0 {
				p.Glow(0, -12, 30, G[2], 0.3*hold)
				p.Glow(0, -1, 22, G[3], 0.16*hold)
			}
			if t < rootSnareBreak {
				sq := squeeze()
				for _, r := range roots {
					if t > r.at {
						pts := rootPath(r, sq)
						drawRoot(p, &pts, grow(r), C, true, 1)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < rootSnareBreak {
				sq := squeeze()
				for _, r := range roots {
					if t <= r.at {
						continue
					}
					pts := rootPath(r, sq)
					g := grow(r)
					drawRoot(p, &pts, g, C, false, 1)
					// buds open into small leaves once it has hold of them
					for _, b := range r.buds {
						q := pts[int(math.Floor(b*rootSnareNP))]
						lk := pixelfx.Clamp((t - rootSnareCinch - 0.1 - b*0.2) / 0.25)
						if b > g || lk <= 0 || q.z < 0 {
							continue
						}
						drawLeaf(p, q.x+2, q.y-2, 5*lk, 1.6, -0.6+math.Sin(t*5+b*9)*0.2, 0.3, [4]uint32{G[0], G[1], G[2], G[3]}, 1)
					}
				}
				// the snap as they cinch
				e := t - rootSnareCinch
				if e > 0 && e < 0.12 {
					for _, pt := range [3][2]float64{{-8, -6}, {8, -10}, {-7, -15}} {
						pixelfx.Sparkle(p, pt[0], pt[1], 5*(1-e/0.12), C[7], G[3], 0.4)
					}
				}
				// the white flash just before they go
				w := rootSnareBreak - t
				if w > 0 && w < 0.05 {
					for _, r := range roots {
						pts := rootPath(r, 1)
						for _, q := range pts {
							if q.z >= 0 {
								p.Px(q.x, q.y, pixelfx.White)
							}
						}
					}
				}
			}
			BARK := []uint32{C[0], C[1], C[2], C[2], C[3], C[4], C[5]}
			for _, c := range chips {
				drawRock(p, c.x, c.y, c.s, c.a, c.pts, BARK, math.Min(1, c.life/0.25), false)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
