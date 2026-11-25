// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/shadow/curse.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the tint that floods the character and the gas round it from Sprite Fusion's open-source poison aura (https://www.spritefusion.com/pixel-art-vfx/poison), MIT License, Copyright (c) 2026 Hugo Duprez.

package shadow

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var curseCower = pixelfx.Pose{}.With(
	pixelfx.Turn.Is(1), pixelfx.Crouch.Is(6), pixelfx.Lean.Is(2), pixelfx.HY.Is(1),
	pixelfx.FX.Is(-6), pixelfx.FY.Is(-38), pixelfx.BX.Is(7), pixelfx.BY.Is(-38),
	pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5),
)

// Curse gathers wisps over the head, stamps a skull down with a flash, burns its eyes as the curse floods the body violet, then cracks and crumbles. It is registered as "shadow/curse".
var Curse = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Curse",
		Desc:     "Wisps gather over the head, a skull stamps down with a flash, its eyes burn as the curse floods the body violet, then it cracks and crumbles.",
		Tags:     []string{"shadow", "curse", "debuff", "skull", "status", "dark", "magic", "one-shot"},
		Size:     [2]int{112, 124},
		Origin:   [2]int{56, 108},
		Duration: 3.4,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: voidPalette()},
			{Name: "bone", Default: pixelfx.Palette("#170f26", "#4a3a6e", "#7f6ea6", "#b3a4d4", "#e2d8f6")},
			{Name: "seed", Default: pixelfx.Num(9)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.2, Pose: pixelfx.Ready},
			{T: 0.3, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.HY.Is(-1), pixelfx.FX.Is(-9), pixelfx.FY.Is(-19), pixelfx.BX.Is(10), pixelfx.BY.Is(-19), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 0.55, Pose: curseCower, How: "snap"},
			{T: 0.8, Pose: curseCower.With(pixelfx.Crouch.Is(7))},
			{T: 1.2, Pose: curseCower.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(3))},
			{T: 1.72, Pose: curseCower.With(pixelfx.Crouch.Is(7), pixelfx.Lean.Is(2))},
			{T: 2.1, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(3), pixelfx.HY.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-15), pixelfx.BX.Is(9), pixelfx.BY.Is(-15), pixelfx.FFX.Is(-4), pixelfx.FBX.Is(5))},
			{T: 2.9, Pose: pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1), pixelfx.FX.Is(-8), pixelfx.FY.Is(-16), pixelfx.BX.Is(9), pixelfx.BY.Is(-16), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))},
			{T: 3.4, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: 0.55, Stop: 0.09, Shake: 0.2, Amp: pixelfx.Val(2)},
		},
	},
	New: newCurse,
}

func init() { pixelfx.Register("shadow/curse", Curse) }

// where the skull hangs, and the beats: it stamps in, burns, cracks, breaks
const (
	curseSY    = -60.0
	curseStamp = 0.55
	curseCrack = 1.72
	curseBreak = 1.98
	curseSW    = 17
	curseSH    = 18
)

// the skull, drawn by hand (light from the upper left): k outline, d m l h bone dark to bright, e sockets, n nose, t teeth, g the gaps between them
var curseSkull = [curseSH]string{
	"......kkkkk......",
	"....kkhhhhlkk....",
	"...khhhhhhlllk...",
	"..khhhhhllllllk..",
	".khhhhlllllllmmk.",
	".khhhllllllllmmk.",
	"khhllllllllllmmdk",
	"khlldddllldddmmdk",
	"khleeeeelleeeeedk",
	"kleeeeeelleeeeedk",
	"kleeeeeemleeeeedk",
	"kmleeeelmmleeedkk",
	".kmmlllmnnmlmddk.",
	"..kmmlmnnnnmddk..",
	"..kdmlllmmlmddk..",
	"...kgtgtgtgtgk...",
	"...kdgtgtgtgdk...",
	"....kkkkkkkkk....",
}

var curseEyes = [2][2]float64{{-3.5, 1.5}, {3.5, 1.5}}

func inCurseSkull(u, v int) bool {
	return u >= 0 && v >= 0 && u < curseSW && v < curseSH && curseSkull[v][u] != '.'
}

// curseCracks holds jagged paths from the crown down to the sockets and out to the temple, each pixel with its turn to split.
var curseCracks = func() map[int]float64 {
	out := map[int]float64{}
	paths := [][][2]int{
		{{8, 1}, {7, 3}, {8, 5}, {6, 6}, {5, 7}},
		{{9, 1}, {10, 3}, {10, 4}, {12, 6}, {12, 7}},
		{{4, 3}, {3, 5}, {2, 6}},
		{{13, 4}, {14, 6}},
	}
	for _, path := range paths {
		ln := len(path) - 1
		for k := 0; k < ln; k++ {
			x0, y0 := path[k][0], path[k][1]
			x1, y1 := path[k+1][0], path[k+1][1]
			dx, dy := x1-x0, y1-y0
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			steps := max(dx, dy)
			for s := 0; s <= steps; s++ {
				x := int(pixelfx.Round(float64(x0) + (float64(x1-x0)*float64(s))/float64(steps)))
				y := int(pixelfx.Round(float64(y0) + (float64(y1-y0)*float64(s))/float64(steps)))
				key := y*curseSW + x
				if _, ok := out[key]; !ok {
					out[key] = (float64(k) + float64(s)/float64(max(1, steps))) / float64(ln)
				}
			}
		}
	}
	return out
}()

// drawCurseSkull draws the skull at scale s (squashed by sq as it lands), shaken by (ox, oy); white for the stamp's first frames; crack 0..1 splits it from the crown, light leaking out of the splits.
func drawCurseSkull(p *pixelfx.Painter, C, K []uint32, s, sq, ox, oy float64, white bool, crack float64) {
	cx, cy := ox, curseSY+oy
	hw, hh := (float64(curseSW)/2)*s+1, (float64(curseSH)/2)*s*sq+1
	p.Shade(cx-hw, cy-hh, cx+hw, cy+hh, func(x, y float64, _ *pixelfx.Out) uint32 {
		u := int(math.Floor((x-cx)/s + float64(curseSW)/2))
		v := int(math.Floor((y-cy)/(s*sq) + float64(curseSH)/2))
		if !inCurseSkull(u, v) {
			return 0
		}
		ch := curseSkull[v][u]
		if white {
			return pick(ch == 'e' || ch == 'n', C[5], pixelfx.White)
		}
		if ck, ok := curseCracks[v*curseSW+u]; ok && ck < crack {
			return pick(crack > 0.55 && (u+v)%2 != 0, C[6], K[0])
		}
		switch ch {
		case 'k', 'g':
			return K[0]
		case 'd':
			return K[1]
		case 'm':
			return K[2]
		case 'l', 't':
			return K[3]
		case 'h':
			return K[4]
		case 'e', 'n':
			return C[0]
		}
		return 0
	})
}

// curseEyeFire draws a burning socket: tongues of violet flame from a white root, licking up out of the eye.
func curseEyeFire(p *pixelfx.Painter, C []uint32, x, y, t, k float64, i int) {
	if k <= 0 {
		return
	}
	// two thin tongues lick up out of the socket and past the brow, swaying, cooling to violet
	for _, j := range [2]float64{-0.6, 0.7} {
		h := (1.5 + 3*(0.5+0.5*pixelfx.Noise(t*8+j*3.1+float64(i)*7, 4))) * k
		sw := pixelfx.Noise(t*4+j*2+float64(i)*3, 8) * 1.2
		for d := 1.0; d <= h; d += 0.5 {
			u := d / h
			c := C[4]
			switch {
			case u < 0.3:
				c = C[7]
			case u < 0.6:
				c = C[6]
			case u < 0.85:
				c = C[5]
			}
			a := 1.0
			if u >= 0.75 {
				a = (1 - u) * 4
			}
			p.Dot(x+j+sw*u*u, y-d, c, a)
		}
	}
	// the pupil: a hard white point in the dark
	p.Px(x, y, pixelfx.White)
	p.Px(x-1, y, C[6])
	p.Glow(x, y, 8, C[5], 0.5*k)
}

type curseLight struct {
	x, y, r, k, life, max float64
	c                     uint32
}

func newCurse(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	K := pixelfx.Fit(o.Ramp("bone"), 5)
	R := pixelfx.NewRng(o.Seed())
	A := o.Actor
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{pixelfx.White, C[7], C[6], C[5]}, Haze: C[4]})
	pf := newPuffs(puffsOpts{
		rng:   R,
		flame: []uint32{pixelfx.White, C[7], C[6], C[5], C[4], C[3], C[2], C[1], C[0]},
		soot:  []uint32{pixelfx.Mix(C[3], C[2], 0.5), pixelfx.Mix(C[1], C[0], 0.3)},
		cap:   pixelfx.Mix(C[4], C[2], 0.5),
		rim:   []uint32{C[6], C[5], C[4]},
		floor: floor,
	})
	dust := newPuffs(puffsOpts{
		rng:   R,
		flame: []uint32{pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White, pixelfx.White},
		soot:  []uint32{pixelfx.Mix(K[2], K[1], 0.4), pixelfx.Mix(K[1], K[0], 0.5)},
		cap:   K[3],
		rim:   []uint32{K[4], K[3], K[2]},
		floor: floor,
	})
	bits := pixelfx.NewBits(floor)
	var lights []curseLight
	t, acc := 0.0, 0.0
	stamped, broke := false, false
	addLight := func(x, y, r float64, c uint32, k, life float64) {
		lights = append(lights, curseLight{x: x, y: y, r: r, c: c, k: k, life: life, max: life})
	}
	head := -38.0
	if A != nil {
		head = A.Y
	}
	moteCols := []uint32{pixelfx.White, C[7], C[6], C[5]}
	gritCols := []uint32{K[2], K[3], K[3], K[4]}
	boneRamp := []uint32{K[0], K[1], K[2], K[3], K[4]}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// wisps and motes gather over the head, spiralling in
			if t < curseStamp-0.05 {
				k := t / curseStamp
				for acc += dt * (40 + 60*k); acc > 1; acc-- {
					a, D := R.Range(0, tau), R.Range(22, 44)
					ps.Spawn(pixelfx.Mote, math.Cos(a)*D, curseSY+math.Sin(a)*D*0.8, -math.Sin(a)*60, math.Cos(a)*60, 0.8, pixelfx.Pick(R, moteCols), R.Range(0.6, 1), 0, 0)
				}
				if R.Chance(dt * 14) {
					a, D := R.Range(0, tau), R.Range(20, 34)
					pf.spawn(pkSoot, math.Cos(a)*D, curseSY+math.Sin(a)*D*0.7, -math.Sin(a)*40, math.Cos(a)*30, R.Range(0.5, 0.7), R.Range(3, 5.5), 0, false)
				}
				attractParticles(ps, 0, curseSY, 9e5*(0.4+k), 70, 0.6, 2.5, dt, func(q *pixelfx.Particle) float64 {
					if q.K == pixelfx.Mote {
						return 1
					}
					return 0
				}, 300)
				attractPuffs(pf, 0, curseSY, 5e5*(0.4+k), 60, 0.7, 3.5, dt, func(*puff) float64 { return 1 }, 120)
			}
			if t >= curseStamp && !stamped {
				stamped = true
				ps.List = ps.List[:0]
				pf.list = pf.list[:0]
				addLight(0, curseSY, 64, C[5], 1, 0.35)
				addLight(0, curseSY, 30, pixelfx.White, 1, 0.1)
				addLight(0, head+12, 40, C[4], 0.6, 0.5)
				for range 22 {
					a, sp := R.Range(0, tau), R.Range(80, 160)
					ps.Spawn(pixelfx.Spark, math.Cos(a)*6, curseSY+math.Sin(a)*6, math.Cos(a)*sp, math.Sin(a)*sp-30, R.Range(0.2, 0.5), pixelfx.White, 1, 0, 0)
				}
				for range 6 {
					a, sp := R.Range(0, tau), R.Range(20, 45)
					pf.spawn(pkSoot, math.Cos(a)*8, curseSY+math.Sin(a)*6, math.Cos(a)*sp, math.Sin(a)*sp*0.6-10, R.Range(0.6, 0.9), R.Range(2.5, 4.5), 0, false)
				}
			}
			// it burns: embers and thin smoke off the eyes, a violet haze round it, the curse dripping onto the head
			if stamped && !broke {
				for _, e := range curseEyes {
					ex, ey := e[0], e[1]
					if R.Chance(dt * 9) {
						ps.Spawn(pixelfx.Ember, ex, curseSY+ey-3, R.Range(-10, 10), -R.Range(20, 45), R.Range(0.5, 0.9), C[6], 1, 0, 0)
					}
					if R.Chance(dt * 7) {
						pf.spawn(pkSoot, ex+R.Range(-1, 1), curseSY+ey-3, R.Range(-5, 5), -R.Range(14, 26), R.Range(0.5, 0.8), R.Range(1.4, 2.4), 0, false)
					}
				}
				if R.Chance(dt * 8) {
					ps.Spawn(pixelfx.Vapour, R.Range(-7, 7), curseSY+R.Range(-6, 6), R.Range(-12, 12), R.Range(-12, 4), R.Range(0.5, 0.8), pick(R.Chance(0.5), C[3], C[4]), R.Range(5, 8), 0, 0)
				}
				// the curse runs down from the skull into the head
				if R.Chance(dt * 12) {
					ps.Spawn(pixelfx.Mote, R.Range(-4, 4), curseSY+9, R.Range(-6, 6), R.Range(40, 70), R.Range(0.25, 0.4), pick(R.Chance(0.5), C[6], C[5]), 0.8, 0, 0)
				}
				if R.Chance(dt * 10) {
					pf.spawn(pkSoot, R.Range(-3.5, 3.5), curseSY+9, R.Range(-3, 3), R.Range(14, 26), R.Range(0.45, 0.7), R.Range(1.2, 2.2), 0, false)
				}
				if t > curseStamp+0.3 && R.Chance(dt*5) {
					hw, hh := 10.0, 30.0
					if A != nil {
						hw, hh = float64(A.W)/2, float64(A.H-4)
					}
					ps.Spawn(pixelfx.StarKind, R.Range(-1, 1)*hw, head+R.Range(4, hh), 0, -6, R.Range(0.3, 0.5), C[6], R.Range(1, 2), 0, 0)
				}
			}
			if t >= curseBreak && !broke {
				broke = true
				// the skull breaks: chunks of bone tumble and bounce, grit rains, dust and the last of the fire rise
				for range 24 {
					var x, y float64
					for {
						x, y = R.Range(-8.5, 8.5), R.Range(-9, 9)
						if inCurseSkull(int(math.Floor(x+float64(curseSW)/2)), int(math.Floor(y+float64(curseSH)/2))) {
							break
						}
					}
					vx := x*R.Range(2.5, 6) + R.Range(-12, 12)
					vy := R.Range(-70, 10)
					life := R.Range(0.8, 1.2)
					a := R.Range(0, tau)
					va := R.Range(-14, 14)
					s := R.Range(1, 2.4)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Chunk, X: x, Y: curseSY + y, VX: vx, VY: vy, G: 0.85, Drag: 0.4, Bounce: 0.35, Life: life, A: a, VA: va, S: s, Ramp: boneRamp})
				}
				for range 26 {
					ps.Spawn(pixelfx.Grit, R.Range(-7, 7), curseSY+R.Range(-8, 8), R.Range(-32, 32), R.Range(-60, 20), R.Range(0.8, 1.3), pixelfx.Pick(R, gritCols), pick(R.Chance(0.3), 2.0, 1.0), 0, 0)
				}
				for range 7 {
					dust.spawn(pkSoot, R.Range(-7, 7), curseSY+R.Range(-8, 6), R.Range(-25, 25), R.Range(-20, 5), R.Range(0.5, 0.8), R.Range(2, 4), R.Range(0, 0.1), false)
				}
				for _, e := range curseEyes {
					ex, ey := e[0], e[1]
					pf.spawn(pkFlame, ex, curseSY+ey, pixelfx.Sign(ex)*6, -30, 0.5, 2.6, 0, false)
				}
				addLight(0, curseSY, 40, C[5], 0.5, 0.25)
			}
			for i := len(lights) - 1; i >= 0; i-- {
				lights[i].life -= dt
				if lights[i].life <= 0 {
					lights = slices.Delete(lights, i, i+1)
				}
			}
			// chunks that land kick up a little dust
			bits.Update(dt)
			for i := range bits.List {
				b := &bits.List[i]
				if b.Rest && !b.Dusted {
					b.Dusted = true
					dust.spawn(pkSoot, b.X, -1.5, R.Range(-12, 12), -R.Range(4, 10), R.Range(0.35, 0.6), R.Range(1.5, 2.5), 0, false)
				}
			}
			ps.Update(dt)
			pf.update(dt)
			dust.update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			lk := 0.3
			if stamped && !broke {
				lk = 0.8
			}
			pf.draw(p, pkSoot, &puffLight{x: 0, y: curseSY, k: lk, reach: 50})
			dust.draw(p, pkSoot, nil)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			// the curse floods down the body from the head, a bright front ahead of it, then ebbs
			flood := pixelfx.Smooth(curseStamp+0.04, curseStamp+0.42, t)
			ebb := 1 - pixelfx.Smooth(curseBreak+0.15, curseBreak+0.75, t)
			if A != nil && flood > 0 && ebb > 0 {
				k, pulse := ebb, 0.85+0.15*math.Sin(t*9)
				for my := 0; my < A.H; my++ {
					u := float64(my) / float64(A.H)
					if u > flood+0.06 {
						break
					}
					edge := flood < 1 && math.Abs(u-flood) < 0.05
					band := flood >= 1 && math.Abs(u-math.Mod((t-curseStamp)*1.3, 1.4)+0.2) < 0.06
					for mx := 0; mx < A.W; mx++ {
						c0 := A.Pixels[my*A.W+mx]
						if c0 == 0 {
							continue
						}
						l := pixelfx.Luma(c0)
						cc := C[2]
						switch {
						case l > 0.72:
							cc = C[6]
						case l > 0.5:
							cc = C[5]
						case l > 0.33:
							cc = C[4]
						case l > 0.18:
							cc = C[3]
						}
						c, a := cc, 0.6*k*pulse
						if edge {
							c, a = C[7], 0.85
						} else if band {
							c, a = C[6], 0.7*k
						}
						p.Blend(A.X+float64(mx), A.Y+float64(my), c, a)
					}
				}
				// a crawling violet edge round the cursed body
				p.OutlineFn(A.Mask, A.X, A.Y, func(mx, my int) (uint32, float64) {
					c := pick(math.Sin(float64(my)*0.7-t*14+float64(mx)*0.3) > 0.2, C[6], C[4])
					a := 0.0
					if float64(my)/float64(A.H) <= flood {
						a = k
					}
					return c, a
				})
				p.Glow(0, A.Y+float64(A.H)*0.45, 30, C[4], 0.3*k*pulse)
			}
			// the gathering: a dim swirl of light where the skull will be
			if t < curseStamp {
				q := t / curseStamp
				p.Glow(0, curseSY, 14+16*q, C[5], 0.2+0.5*q)
				if q > 0.15 {
					c := C[3]
					if q > 0.7 {
						c = C[5]
					} else if q > 0.4 {
						c = C[4]
					}
					ringFast(p, 0, curseSY, 4+30*(1-pixelfx.Ease.InQuad(q)), 1, c, 1)
				}
			}
			// the skull: it slams down from big and pale, squashes, bobs while it burns, shakes as it cracks
			if t >= curseStamp && t < curseBreak {
				u := (t - curseStamp) / 0.1
				s := 1.0
				if u < 1 {
					s = 1.7 - 0.7*pixelfx.Ease.InQuad(u)
				}
				land := t - curseStamp - 0.1
				sq := 1.0
				if land > 0 {
					sq = 1 - 0.16*math.Exp(-land*18)*math.Cos(land*40)
				}
				shake := 0.0
				if t > curseCrack {
					shake = (t - curseCrack) * 4
				}
				ox := pixelfx.Round(math.Sin(t*61) * shake)
				oy := 0.0
				if land > 0 {
					oy = pixelfx.Round(math.Sin(t*3.4) * 1.2)
				}
				drawCurseSkull(p, C, K, s, sq, ox, oy, t < curseStamp+0.067, pixelfx.Smooth(curseCrack, curseBreak, t))
				burn := pixelfx.Smooth(curseStamp+0.1, curseStamp+0.25, t)
				if u >= 1 {
					ck := burn * (1 + 0.4*pixelfx.Smooth(curseCrack, curseBreak, t))
					for i, e := range curseEyes {
						curseEyeFire(p, C, ox+e[0], curseSY+oy+e[1], t, ck, i)
					}
				}
				if u < 1.2 {
					pixelfx.Sparkle(p, 0, curseSY, 14*(1-u/1.2), C[7], C[5], 0.6)
				}
				p.Glow(0, curseSY, 26, C[4], 0.25)
			}
			// the stamp's shock ring
			ru := (t - curseStamp - 0.08) / 0.3
			if ru > 0 && ru < 1 {
				ringFastFn(p, 0, curseSY+2, 10+26*pixelfx.Ease.OutCubic(ru), math.Max(1, 2.5*(1-ru)), func(v, _ float64) (uint32, float64) {
					return pick(v > 0.5, pixelfx.White, C[6]), 1.2 - ru
				})
			}
			bits.Draw(p)
			pf.draw(p, pkFlame, nil)
			ps.Draw(p, pixelfx.LayerFront)
			for _, l := range lights {
				q := l.life / l.max
				p.Glow(l.x, l.y, l.r*(0.6+0.4*q), l.c, l.k*q*q)
			}
			// bone keeps its own colours under the glow (all but the stamp's white frame)
			if t >= curseStamp+0.067 && t < curseBreak {
				unlight(p, -10, curseSY-10, 10, curseSY+10, func(x, y float64) bool {
					u := int(math.Floor(x + float64(curseSW)/2))
					v := int(math.Floor(y - curseSY + float64(curseSH)/2))
					return inCurseSkull(u, v) && curseSkull[v][u] != 'e'
				}, 0.2)
			}
		},
	}
}
