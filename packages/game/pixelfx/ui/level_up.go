// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ui/level-up.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the pillar's strands of light and the rings climbing it from Sprite Fusion's open-source holy light (https://www.spritefusion.com/pixel-art-vfx/holy-light), and the streaks rushing in from its power-up (https://www.spritefusion.com/pixel-art-vfx/power-up), MIT License, Copyright (c) 2026 Hugo Duprez.

package ui

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	luCoil = pixelfx.Pose{}.With(
		pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(7), pixelfx.Lean.Is(2),
		pixelfx.FX.Is(-6), pixelfx.FY.Is(-13), pixelfx.BX.Is(7), pixelfx.BY.Is(-13),
		pixelfx.FFX.Is(-5), pixelfx.FBX.Is(6),
	)
	luLeap = pixelfx.Pose{}.With(
		pixelfx.Turn.Is(1.1), pixelfx.Y.Is(-16), pixelfx.Crouch.Is(-1),
		pixelfx.FX.Is(-6), pixelfx.FY.Is(-45), pixelfx.BX.Is(7), pixelfx.BY.Is(-44),
		pixelfx.FFX.Is(-2), pixelfx.FFY.Is(-8), pixelfx.FBX.Is(5), pixelfx.FBY.Is(-3),
		pixelfx.ToeB.On(),
	)
	luPump = pixelfx.Pose{}.With(
		pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(0),
		pixelfx.FX.Is(-6), pixelfx.FY.Is(-45), pixelfx.BX.Is(9), pixelfx.BY.Is(-15),
		pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4),
	)
)

// LevelUp gathers gold at the hero's feet and bursts up as a pillar of light while rings climb it, LEVEL UP! drops in letter by letter, a shine sweeps it and sparkles twinkle. It is registered as "ui/level-up".
var LevelUp = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Level up",
		Desc:     "Gold gathers at the feet and bursts up as a pillar of light round the hero; rings climb it, LEVEL UP! drops in letter by letter, a shine sweeps it, sparkles twinkle.",
		Tags:     []string{"ui", "level", "reward", "text", "light", "one-shot"},
		Size:     [2]int{176, 138},
		Origin:   [2]int{88, 114},
		Duration: luDuration,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1405", "#5c3008", "#9a5a0e", "#d48e18", "#f5b92c", "#ffde5e", "#fff4bc", "#ffffff")},
			{Name: "text", Default: pixelfx.Text("LEVEL UP!")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0),
		Poses: []pixelfx.Key{
			{T: -0.3, Pose: pixelfx.Ready},
			{T: 0.28, Pose: luCoil},
			{T: 0.3, Pose: luLeap, How: "snap"},
			{T: 0.46, Pose: luLeap.With(pixelfx.Y.Is(-19))},
			{T: 0.72, Pose: luLeap.With(pixelfx.Y.Is(0), pixelfx.FFY.Is(-4), pixelfx.FBY.Is(-4)), How: "in"},
			{T: 0.74, Pose: luCoil.With(pixelfx.Crouch.Is(5)), How: "snap"},
			{T: 0.95, Pose: luPump, How: "out"},
			{T: 1.2, Pose: luPump.With(pixelfx.Crouch.Is(1), pixelfx.FY.Is(-43))},
			{T: 1.45, Pose: luPump},
			{T: 2.3, Pose: luPump.With(pixelfx.FY.Is(-41))},
			{T: 2.8, Pose: pixelfx.Ready},
			{T: 3.2, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{{T: 0.28, Stop: 0.05, Shake: 0.14, Amp: pixelfx.Val(2)}},
		Dust: []pixelfx.DustKick{{T: 0.3, X: 0, N: 7}, {T: 0.74, X: 0, N: 6}},
	},
	New: newLevelUp,
}

func init() { pixelfx.Register("ui/level-up", LevelUp) }

type luStrand struct {
	x, sp, per, duty, ph, end float64
	lvl                       int
}

var (
	luDuration = 3.2
	luRel      = 0.28
	luRet      = 1.02
	luGone     = 1.34
	luTop      = 100.0
	luTY       = -68.0
	luDrop     = 0.5
	luStag     = 0.05
	luLanded   = 1.06
	luShine    = 1.24
	luExit     = 2.3
	luStrands  = func() [9]luStrand {
		var out [9]luStrand
		for i := range out {
			fi := float64(i)
			lvl := 2
			if pixelfx.Rand(fi, 6) < 0.35 {
				lvl = 1
			}
			out[i] = luStrand{
				x:    (fi/8-0.5)*1.5 + (pixelfx.Rand(fi, 1)-0.5)*0.16,
				sp:   170 + pixelfx.Rand(fi, 2)*170,
				per:  10 + pixelfx.Rand(fi, 3)*16,
				duty: 0.3 + pixelfx.Rand(fi, 4)*0.35,
				ph:   pixelfx.Rand(fi, 5),
				lvl:  lvl,
				end:  0.5 + pixelfx.Rand(fi, 7)*0.42,
			}
		}
		return out
	}()
)

func luWidthAt(t float64) float64 {
	if t < luRel {
		return 0
	}
	return 12 * (1 + 0.4*math.Exp(-(t-luRel)*10)) * (1 - 0.82*pixelfx.Smooth(luRet-0.12, luRet+0.16, t))
}

func drawLuColumn(p *pixelfx.Painter, C []uint32, t float64) {
	a := t - luRel
	if a < 0 || t > luGone {
		return
	}
	top := -luTop * pixelfx.Ease.OutExpo(pixelfx.Clamp(a/0.12))
	bot := -luTop * pixelfx.Ease.InCubic(pixelfx.Seg(t, luRet, luGone))
	W := luWidthAt(t)
	k := 1 - pixelfx.Smooth(luRet, luGone, t)
	p.GlowLine(0, top, 0, math.Min(bot, -34), W*1.6+6, C[5], 0.2*k+0.06)
	p.GlowLine(0, top, 0, math.Min(bot, -40), 5, pixelfx.White, 0.18*k)
	if a < 0.04 {
		for y := math.Floor(top); y < bot; y++ {
			wr := W * 0.55 * (1 - 0.3*(-y/luTop))
			for x := math.Floor(-wr); x <= math.Ceil(wr); x++ {
				col := C[6]
				if math.Abs(x+0.5) < wr-1.5 {
					col = pixelfx.White
				}
				p.Dot(x, y, col, pixelfx.Clamp((y+luTop)/20)*1.5)
			}
		}
		return
	}
	for y := math.Ceil(top); y < math.Ceil(bot); y++ {
		h := -y / luTop
		botFade := 1.0
		if t >= luRet {
			botFade = pixelfx.Clamp((bot - y) / 10)
		}
		fade := pixelfx.Clamp((y+luTop)/16) * botFade
		head := y-top < 3 && a < 0.16
		wr := W*(1-0.3*h) + 0.9*pixelfx.Noise2(y*0.2, t*12, 3)
		for _, s := range [2]float64{-1, 1} {
			off := 0.0
			if s > 0 {
				off = 0.5
			}
			run := pixelfx.Fract(y*0.06+t*3.4+off) < 0.78
			xw := s * wr
			col := C[5]
			if head {
				col = pixelfx.White
			} else if run {
				col = C[6]
			}
			p.Dot(xw, y, col, fade*1.4)
			outerA := 0.25
			if run {
				outerA = 0.5
			}
			p.Dot(xw+s, y, C[4], fade*outerA)
			if run {
				p.Dot(xw-s, y, C[5], fade*1.2)
			}
		}
		p.Dot(0, y, pixelfx.White, fade*1.6)
		leftCol := C[6]
		if pixelfx.Noise2(y*0.3, t*14, 5) > -0.2 {
			leftCol = pixelfx.White
		}
		p.Dot(-1, y, leftCol, fade*1.3)
		if pixelfx.Noise2(y*0.25, t*9, 8) > 0.25 {
			p.Dot(1, y, C[6], fade*1.2)
		}
		for _, S := range luStrands {
			if h > S.end {
				continue
			}
			ph := pixelfx.Fract((y+t*S.sp)/S.per + S.ph)
			if ph > S.duty {
				continue
			}
			u := ph / S.duty
			col := C[6-S.lvl]
			if head || u < 0.2 {
				col = pixelfx.White
			} else if u < 0.65 {
				col = C[7-S.lvl]
			}
			p.Px(S.x*wr, y, col)
		}
	}
}

func drawLuPlate(p *pixelfx.Painter, C []uint32, t, k float64) {
	if k <= 0.02 {
		return
	}
	rx, ry := 9+9*k+math.Sin(t*9), 2.2
	p.Shade(-rx, -ry, rx, ry, func(x, y float64, o *pixelfx.Out) uint32 {
		dx, dy := x/rx, y/ry
		d := math.Sqrt(dx*dx + dy*dy)
		if d > 1 {
			return 0
		}
		u := d / math.Max(0.05, k)
		if u < 0.3 {
			o.C = pixelfx.White
		} else if u < 0.55 {
			o.C = C[6]
		} else if u < 0.8 {
			o.C = C[5]
		} else {
			o.C = C[4]
		}
		if d < 0.6*k {
			o.A = 1
		} else {
			o.A = k * (1 - d) * 2.2
		}
		return 0
	})
	p.Glow(0, -1, rx*2.2, C[4], 0.35*k)
}

func drawLuRays(p *pixelfx.Painter, C []uint32, t, k, cx, cy float64) {
	if k <= 0.02 {
		return
	}
	for i := range 20 {
		fi := float64(i)
		an := (fi/20)*tau + 0.08
		ca, sa := math.Cos(an), math.Sin(an)
		if math.Abs(sa) < 0.4 {
			continue
		}
		r0 := 1/math.Sqrt((ca/52)*(ca/52)+(sa/12)*(sa/12)) + 2*k
		var length float64
		if i%2 == 0 {
			length = (15 + 7*pixelfx.Rand(fi, 9)) * k
		} else {
			length = (7 + 4*pixelfx.Rand(fi, 8)) * k
		}
		if length < 1 {
			continue
		}
		sh := 0.75 + 0.25*math.Sin(t*7+fi*2.1)
		x0, y0 := cx+ca*r0, cy+sa*r0
		p.LineFn(x0, y0, x0+ca*length, y0+sa*length, func(u float64) (uint32, float64) {
			col := C[5]
			if u < 0.3 {
				col = C[7]
			} else if u < 0.6 {
				col = C[6]
			}
			a := (1.3 * (1 - u)) / 0.45
			if u < 0.55 {
				a = 1.3
			}
			return col, sh * a
		})
	}
}

type luLetter struct {
	g                       *pixelfx.Glyph
	w                       int
	cx, born, pop           float64
	off, v, sq, flash       float64
	on, landed, rest, burst bool
}

type luSat struct {
	x, y, t0, dur, L float64
}

type luRush struct {
	x, y, vx, vy, life, length float64
	c                          uint32
	back                       bool
}

func newLevelUp(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	R := pixelfx.NewRng(o.Seed())
	A := o.Actor
	SHADOW := pixelfx.Scale(C[0], 0.5)
	ps := pixelfx.NewParticles(pixelfx.SimOpts{
		Rng:    R,
		Ground: func(float64) float64 { return 0 },
		Sparks: []uint32{C[7], C[6], C[5], C[3]},
	})
	lights := &pixelfx.Lights{}
	t := 0.0
	spawn := func(k pixelfx.Kind, x, y, vx, vy, life float64, col uint32, size, delay float64, flag pixelfx.Flag) {
		ps.Spawn(k, x, y, vx, vy, math.Min(life, luDuration-0.05-t), col, size, delay, flag)
	}
	word := spell(o.Text("text"), "big")
	BASE := luTY + 14
	X0 := -float64(word.w)
	var lets []luLetter
	for _, L := range word.letters {
		if L.g == nil {
			continue
		}
		i := len(lets)
		lets = append(lets, luLetter{
			g: L.g, w: L.w, cx: X0 + (float64(L.x)+float64(L.w)/2)*2,
			born: luDrop + float64(i)*luStag, pop: luExit + float64(i)*0.04,
			off: -18, v: 60,
		})
	}
	right := X0 + float64(word.w)*2
	sats := make([]luSat, 8)
	for i := range sats {
		s := -1.0
		if i%2 != 0 {
			s = 1
		}
		x := s * R.Range(20, 60)
		var y float64
		if math.Abs(x) < 50 {
			y = luTY + 7 + R.Sign()*R.Range(11, 18)
		} else {
			y = luTY + 7 + R.Range(-14, 14)
		}
		t0 := luShine - 0.1 + float64(i)*0.13 + R.Range(0, 0.05)
		dur := R.Range(0.32, 0.5)
		L := R.Range(3, 6)
		sats[i] = luSat{x: x, y: y, t0: t0, dur: dur, L: L}
	}
	var halos []float64
	released, nextHalo, gather := false, luRel+0.03, 0.0
	var rush []luRush
	drawRush := func(p *pixelfx.Painter, back bool) {
		for _, m := range rush {
			if m.back != back {
				continue
			}
			sp := pixelfx.Hypot(m.vx, m.vy)
			ux, uy, L := m.vx/sp, m.vy/sp, m.length
			p.Line(m.x-ux*L, m.y-uy*L, m.x-ux*L*0.4, m.y-uy*L*0.4, m.c, 1)
			p.Line(m.x-ux*L*0.35, m.y-uy*L*0.35, m.x, m.y, pixelfx.White, 1)
			p.Glow(m.x, m.y, 4, C[5], 0.25)
		}
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			if t < luRel {
				gather += dt * 420
			}
			for ; gather >= 1; gather-- {
				kind := R.Next()
				sp := R.Range(140, 250) * (1 + t*2)
				var x, y, tx, ty float64
				if kind < 0.42 {
					an := R.Range(0, tau)
					D := R.Range(22, 70)
					x, y = math.Cos(an)*D, -1-math.Abs(math.Sin(an))*D*0.1
					tx, ty = math.Cos(an)*3, -1
				} else if kind < 0.84 {
					an := R.Range(0, tau)
					D := R.Range(34, 64)
					x, y = math.Cos(an)*D, math.Min(-3, -20+math.Sin(an)*D*0.85)
					tx, ty = math.Cos(an)*11, -20+math.Sin(an)*14
				} else {
					if t < luRel*0.4 {
						continue
					}
					x, y = R.Range(-9, 9), -1
					tx, ty = x*0.7, -R.Range(26, 44)
				}
				dx, dy := tx-x, ty-y
				l := pixelfx.Hypot(dx, dy)
				if l == 0 {
					l = 1
				}
				ln := R.Range(5, 9)
				col := C[5]
				if R.Chance(0.45) {
					col = C[6]
				}
				back := kind > 0.84 && R.Chance(0.5)
				rush = append(rush, luRush{
					x: x, y: y, vx: (dx / l) * sp, vy: (dy / l) * sp,
					life: math.Min(l/sp, luRel+0.06-t), length: ln, c: col, back: back,
				})
			}
			for i := len(rush) - 1; i >= 0; i-- {
				rush[i].life -= dt
				if rush[i].life <= 0 {
					rush = append(rush[:i], rush[i+1:]...)
				} else {
					rush[i].x += rush[i].vx * dt
					rush[i].y += rush[i].vy * dt
				}
			}
			if t < luRel && R.Chance(0.12) {
				px := R.Range(-30, 30)
				py := -R.Range(0, 3)
				vy := -R.Range(10, 30)
				life := R.Range(0.3, 0.5)
				spawn(pixelfx.StarKind, px, py, 0, vy, life, C[6], 0.6, 0, 0)
			}
			if !released && t >= luRel {
				released = true
				lights.Add(0, -24, 90, C[5], 0.6, 0.7)
				lights.Add(0, -8, 40, pixelfx.White, 0.6, 0.12)
				for i := range 22 {
					an := (float64(i)/22)*tau + R.Range(-0.12, 0.12)
					sp := R.Range(260, 460)
					y0 := -18 + math.Sin(an)*8
					vy := math.Sin(an) * sp * 0.8
					vert := 1.0
					if vy > 0 {
						vert = -y0 / vy
					} else if vy < 0 {
						vert = (y0 + 104) / -vy
					}
					life := math.Min(math.Min(R.Range(0.1, 0.2), vert), 78/math.Abs(math.Cos(an)*sp))
					col := C[6]
					if R.Chance(0.4) {
						col = C[7]
					}
					var flag pixelfx.Flag
					if an > 0.3 && an < pi-0.3 {
						flag = pixelfx.Back
					}
					spawn(pixelfx.Mote, math.Cos(an)*6, y0, math.Cos(an)*sp, vy, life, col, 1, 0, flag)
				}
				for range 26 {
					an := -pi/2 + R.Range(-1.3, 1.3)
					sp := R.Range(120, 290)
					px := R.Range(-5, 5)
					life := R.Range(0.35, 0.8)
					spawn(pixelfx.Spark, px, -2, math.Cos(an)*sp, math.Sin(an)*sp, life, pixelfx.White, 1, 0, 0)
				}
				for range 16 {
					px := R.Range(-10, 10)
					py := R.Range(-36, -4)
					vx := R.Range(-30, 30)
					vy := -R.Range(15, 60)
					life := R.Range(0.6, 1.3)
					col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					var size float64
					if R.Chance(0.35) {
						size = R.Range(1.5, 2.6)
					} else {
						size = R.Range(0.4, 0.9)
					}
					var flag pixelfx.Flag
					if R.Chance(0.4) {
						flag = pixelfx.Back
					}
					spawn(pixelfx.StarKind, px, py, vx, vy, life, col, size, 0, flag)
				}
			}
			W := luWidthAt(t)
			if t >= nextHalo && t < luRet-0.1 {
				halos = append(halos, t)
				nextHalo += 0.16
			}
			if t > luRel && t < luRet+0.1 && R.Chance(0.5) {
				s := R.Sign()
				y := R.Range(-4, -60)
				vy := -R.Range(140, 260)
				x := s * R.Range(0.75, 1.6) * W
				life := math.Min(R.Range(0.16, 0.32), (y+96)/-vy)
				col := C[6]
				if R.Chance(0.5) {
					col = C[7]
				}
				size := R.Range(0.6, 1)
				var flag pixelfx.Flag
				if R.Chance(0.5) {
					flag = pixelfx.Back
				}
				spawn(pixelfx.Mote, x, y, 0, vy, life, col, size, 0, flag)
			}
			if t > luRel && t < 2.4 && R.Chance(0.14*(1-pixelfx.Seg(t, 1.6, 2.4))) {
				px := R.Range(-20, 20)
				py := -R.Range(1, 30)
				vx := R.Range(-15, 15)
				vy := -R.Range(20, 55)
				life := R.Range(0.8, 1.6)
				size := R.Range(1, 2)
				var flag pixelfx.Flag
				if R.Chance(0.4) {
					flag = pixelfx.Back
				}
				spawn(pixelfx.Ember, px, py, vx, vy, life, C[5], size, 0, flag)
			}
			if A != nil && t > luRel+0.1 && t < 2.2 && R.Chance(0.09) {
				m := A.Mask
				for range 12 {
					i, j := int(R.Int(0, float64(m.W-1))), int(R.Int(0, float64(m.H-1)))
					if m.Data[j*m.W+i] == 0 {
						continue
					}
					vx := R.Range(-8, 8)
					vy := -R.Range(14, 34)
					life := R.Range(0.6, 1.1)
					col := pixelfx.Pick(R, []uint32{C[7], C[6], C[5]})
					size := 0.6
					if R.Chance(0.2) {
						size = 1.5
					}
					var flag pixelfx.Flag
					if R.Chance(0.5) {
						flag = pixelfx.Back
					}
					spawn(pixelfx.StarKind, A.X+float64(i)+0.5, A.Y+float64(j)+0.5, vx, vy, life, col, size, 0, flag)
					break
				}
			}
			if t > 1.0 && t < 2.5 && R.Chance(0.13) {
				up := R.Chance(0.6)
				var x, y float64
				if up {
					x = R.Range(-58, 58)
					y = luTY + 7 + R.Range(-16, 16)
				} else {
					x = R.Range(-20, 20)
					y = R.Range(-44, -6)
				}
				vx := R.Range(-6, 6)
				vy := -R.Range(4, 14)
				life := R.Range(0.4, 0.8)
				col := pixelfx.Pick(R, []uint32{C[7], C[6], C[6], C[5]})
				size := 0.6
				if R.Chance(0.3) {
					size = R.Range(1.5, 2.4)
				}
				var flag pixelfx.Flag
				if R.Chance(0.3) {
					flag = pixelfx.Back
				}
				spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, flag)
			}
			for li := range lets {
				L := &lets[li]
				if t < L.born {
					continue
				}
				L.on = true
				if !L.rest {
					L.v += 1700 * dt
					L.off += L.v * dt
					if L.off >= 0 && L.v > 0 {
						L.off = 0
						if L.v > 110 {
							L.sq = 1
							if !L.landed {
								L.landed = true
								L.flash = 1
								lights.Add(L.cx, BASE-7, 18, C[5], 0.55, 0.25)
								for _, s := range [2]float64{-1, 1} {
									py := BASE - R.Range(0, 3)
									vx := s * R.Range(15, 40)
									vy := -R.Range(5, 25)
									life := R.Range(0.25, 0.45)
									size := R.Range(0.5, 1.2)
									spawn(pixelfx.StarKind, L.cx+s*float64(L.w), py, vx, vy, life, C[6], size, 0, 0)
								}
							}
							L.v = -L.v * 0.3
						} else {
							L.v = 0
							L.rest = true
						}
					}
				}
				L.sq = math.Max(0, L.sq-dt/0.1)
				L.flash = math.Max(0, L.flash-dt/0.12)
				if !L.burst && t >= L.pop+0.06 {
					L.burst = true
					for i := range 3 {
						an := R.Range(0, tau)
						sp := R.Range(20, 55)
						px := L.cx + R.Range(-3, 3)
						py := BASE - 7 + R.Range(-4, 4)
						life := R.Range(0.3, 0.6)
						col := pixelfx.Pick(R, []uint32{C[7], C[6]})
						size := 0.6
						if i == 0 {
							size = R.Range(1.2, 2)
						}
						spawn(pixelfx.StarKind, px, py, math.Cos(an)*sp, math.Sin(an)*sp-15, life, col, size, 0, 0)
					}
				}
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			drawLuRays(p, C, t, pixelfx.Ease.OutExpo(pixelfx.Seg(t, luLanded, luLanded+0.2))*(1-pixelfx.Ease.InQuad(pixelfx.Seg(t, luExit-0.15, luExit+0.1))), 0, luTY+7)
			if t < luRel {
				k := pixelfx.Seg(t, 0.02, luRel)
				pixelfx.Band(p, 0, 0, 4+52*(1-pixelfx.Ease.InQuad(k)), 0.2, 1.5, k*1.1, C, func(g float64) bool { return math.Sin(g) < 0 }, false)
			}
			for _, ring := range [2][3]float64{{0, 72, 4}, {0.08, 46, 2.5}} {
				k := (t - luRel - ring[0]) / 0.55
				if k > 0 && k < 1 {
					pixelfx.Band(p, 0, 0, 5+ring[1]*pixelfx.Ease.OutCubic(k), 0.2, math.Max(1, ring[2]*(1-k)), 1.15-k, C, func(g float64) bool { return math.Sin(g) < 0 }, k < 0.15)
				}
			}
			for _, h0 := range halos {
				k := (t - h0) / 0.6
				if k > 0 && k < 1 {
					pixelfx.Band(p, 0, -2-72*pixelfx.Ease.OutQuad(k), (luWidthAt(t)*1.45+4)*(1-0.4*k), 0.3, 1.5, (1-k)*1.4, C, func(g float64) bool { return math.Sin(g) < 0 }, k < 0.12)
				}
			}
			drawLuColumn(p, C, t)
			plateK := 1 - pixelfx.Smooth(luRet-0.1, luGone+0.2, t)
			if t < luRel {
				plateK = 0.55 * pixelfx.Seg(t, 0.05, luRel)
			}
			drawLuPlate(p, C, t, plateK)
			ps.Draw(p, pixelfx.LayerBack)
			drawRush(p, true)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			if t < luRel {
				k := pixelfx.Seg(t, 0.02, luRel)
				pixelfx.Band(p, 0, 0, 4+52*(1-pixelfx.Ease.InQuad(k)), 0.2, 1.5, k*1.1, C, func(g float64) bool { return math.Sin(g) >= 0 }, false)
			}
			for _, ring := range [2][3]float64{{0, 72, 4}, {0.08, 46, 2.5}} {
				k := (t - luRel - ring[0]) / 0.55
				if k > 0 && k < 1 {
					pixelfx.Band(p, 0, 0, 5+ring[1]*pixelfx.Ease.OutCubic(k), 0.2, math.Max(1, ring[2]*(1-k)), 1.15-k, C, func(g float64) bool { return math.Sin(g) >= 0 }, k < 0.15)
				}
			}
			for _, h0 := range halos {
				k := (t - h0) / 0.6
				if k > 0 && k < 1 {
					pixelfx.Band(p, 0, -2-72*pixelfx.Ease.OutQuad(k), (luWidthAt(t)*1.45+4)*(1-0.4*k), 0.3, 1.5, (1-k)*1.4, C, func(g float64) bool { return math.Sin(g) >= 0 }, k < 0.12)
				}
			}
			lit := pixelfx.Smooth(luRel-0.02, luRel+0.04, t) * (1 - pixelfx.Smooth(luRet, 2.5, t))
			tintK := 0.0
			if t >= luRel {
				if t < luRel+0.06 {
					tintK = 1
				} else {
					tintK = math.Exp(-(t - luRel - 0.06) / 0.07)
				}
			}
			tintActor(p, A, tintK, [3]uint32{C[4], C[5], C[6]})
			lightActor(p, A, C[5], 0.1*pixelfx.Seg(t, 0.05, luRel)*(1-lit)+0.06*lit, 0.6*lit, t)
			haloActor(p, A, C[5], 0.85*lit*(1-pixelfx.Smooth(luRet, luGone+0.3, t))+0.3*lit, t)
			if t < luRel {
				k := pixelfx.Seg(t, 0.05, luRel)
				d := t - luRel*0.6
				beat := 1 + 0.4*math.Exp(-(d*d)/0.001)
				pixelfx.Sparkle(p, 0, -2, (1+4*k*k)*beat, C[6], C[5], 0.3+0.4*k)
			} else if t < luRel+0.22 {
				a := (t - luRel) / 0.22
				pixelfx.Flare(p, 0, -4, 28*math.Pow(1-a, 1.5), 1-a, C)
				p.Glow(0, -16, 48, pixelfx.White, 0.3*(1-a))
			}
			ps.Draw(p, pixelfx.LayerFront)
			drawRush(p, false)

			var items []wordItem
			done := (t - luLanded) / 0.12
			for li := range lets {
				L := &lets[li]
				if !L.on {
					continue
				}
				sx, sy, flash := 2.0, 2.0, L.flash
				if !L.landed && L.v > 150 {
					sx, sy = 1.8, 2.3
				}
				if L.sq > 0 {
					sx = 2 * (1 + 0.22*L.sq)
					sy = 2 * (1 - 0.22*L.sq)
				}
				pu := (t - L.pop) / 0.07
				hop := pixelfx.Seg(t, L.pop-0.1, L.pop)
				if pu >= 1 {
					continue
				}
				if hop > 0 && pu <= 0 {
					sx = 2 * (1 - 0.1*hop)
					sy = 2 * (1 + 0.15*hop)
				}
				if pu > 0 {
					sx = 2 * (1 + 0.3*pu)
					sy = sx
					flash = 1
				}
				if done > 0 && done < 1 {
					flash = math.Max(flash, 1-done)
				}
				h, w := 7*sy, float64(L.w)*sx
				items = append(items, wordItem{
					g: L.g, x: L.cx - w/2, y: BASE + L.off - h - 3*pixelfx.Ease.OutQuad(hop),
					sx: sx, sy: sy, a: pixelfx.Clamp((t - L.born) / 0.05), flash: flash,
				})
			}
			sh := pixelfx.Seg(t, luShine, luShine+0.34)
			hasShine := sh > 0 && sh < 1
			shine := 0.0
			if hasShine {
				shine = X0 - 14 + (right-X0+30)*pixelfx.Ease.InOutSine(sh)
			}
			drawWord(p, items, wordStyle{
				fill: []uint32{C[3], C[4], C[5], C[6]}, edge: pixelfx.White, under: C[2], outline: C[0], shadow: SHADOW,
				hasShine: hasShine, shine: shine, hasShineY: true, shineY: luTY, glint: C[6],
			})
			if hasShine && shine > X0 && shine < right {
				p.GlowLine(shine, luTY, shine+8.4, BASE, 9, C[6], 0.3)
			}
			hold := pixelfx.Smooth(0.9, 1.2, t) * (1 - pixelfx.Smooth(luExit, luExit+0.4, t))
			if hold > 0 {
				extra := 0.0
				if done > 0 && done < 1 {
					extra = 0.3 * (1 - done)
				}
				p.Glow(0, luTY+7, 50, C[5], 0.22*hold+extra)
			}
			for li := range lets {
				L := &lets[li]
				u := (t - L.pop - 0.07) / 0.16
				if u > 0 && u < 1 {
					pixelfx.Sparkle(p, L.cx, BASE-7, 6*(1-u)*(1-u), C[6], C[5], 0.5)
				}
			}
			g := (t - luShine - 0.3) / 0.26
			if g > 0 && g < 1 {
				pixelfx.Sparkle(p, right-2, luTY+1, 7*math.Sin(g*pi), C[6], C[5], 0.6)
			}
			for _, s := range sats {
				u := (t - s.t0) / s.dur
				if u > 0 && u < 1 {
					pixelfx.Sparkle(p, s.x, s.y, s.L*math.Pow(math.Sin(u*pi), 0.7), C[6], C[5], 0.5)
				}
			}
			lights.Draw(p)
		},
	}
}
