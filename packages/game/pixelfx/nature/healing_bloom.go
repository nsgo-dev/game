// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/nature/healing-bloom.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the healing light (a soft column of glow, a lit ring on the floor, motes and bevelled crosses drifting up and glinting) from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package nature

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// HealingBloom is a sprout that pushes up and opens into a glowing golden flower in a flare of healing light, then lifts away as petals and motes. It is registered as "nature/healing-bloom".
var HealingBloom = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Healing bloom",
		Desc:     "A sprout pushes up and unfurls, a bud swells and opens round a glowing golden heart in a flare of healing light; its petals lift away one by one as motes.",
		Tags:     []string{"nature", "heal", "flower", "plant", "buff", "ground", "one-shot"},
		Size:     [2]int{124, 120},
		Origin:   [2]int{62, 108},
		Duration: bloomDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0c1a10", "#173a1a", "#24582a", "#357a34", "#52a03e", "#86c84e", "#c4ec7a", "#f4ffd0")},
			{Name: "petal", Default: pixelfx.Palette("#4a1a3a", "#a8406e", "#ec8ab0", "#ffd8e8")},
			{Name: "heart", Default: pixelfx.Palette("#a8641a", "#f0b42e", "#ffe680", "#fffbe0")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	New: newHealingBloom,
}

func init() { pixelfx.Register("nature/healing-bloom", HealingBloom) }

const (
	bloomDuration = 3.3
	bloomNPet     = 6
)

var (
	bloomStem = 0.42
	bloomBud  = 0.75
	bloomOpen = 1.05
	bloomLift = 1.7
	bloomWilt = 2.4
	bloomH    = 36.0
)

// drawBloomCross draws a bevelled cross of light.
func drawBloomCross(p *pixelfx.Painter, x, y float64, L int, C []uint32, a float64, glint bool) {
	X, Y := pixelfx.Round(x), pixelfx.Round(y)
	ins := func(dx, dy int) bool {
		return (dx == 0 && absInt(dy) <= L) || (dy == 0 && absInt(dx) <= L)
	}
	for dy := -L - 1; dy <= L+1; dy++ {
		for dx := -L - 1; dx <= L+1; dx++ {
			if ins(dx, dy) {
				col := C[5]
				if glint && dx == 0 && dy == 0 {
					col = pixelfx.White
				} else if dy < 0 || dx < 0 {
					col = C[6]
				}
				p.Dot(X+float64(dx), Y+float64(dy), col, a)
			} else if L > 1 && (ins(dx-1, dy) || ins(dx+1, dy) || ins(dx, dy-1) || ins(dx, dy+1)) {
				p.Dot(X+float64(dx), Y+float64(dy), C[2], a*0.8)
			}
		}
	}
	p.Glow(x, y, 5+float64(L)*2, C[6], 0.35*a)
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

type bloomPetal struct {
	az, off, at float64
	gone        bool
}

type bloomFlying struct {
	x, y, vx, vy, a, va, fl, vf, life, max float64
}

type bloomGlyph struct {
	x, y, t0, life, seed float64
	L                    int
}

func newHealingBloom(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	Pt := pixelfx.Fit(o.Ramp("petal"), 4)
	Ht := pixelfx.Fit(o.Ramp("heart"), 4)
	R := pixelfx.NewRng(o.Seed())
	floor := func(float64) float64 { return 0 }
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: floor, Sparks: []uint32{Ht[3], Ht[2], Ht[1], Ht[0]}})
	lights := &pixelfx.Lights{}
	var flying []bloomFlying
	var glyphs []bloomGlyph
	petals := make([]bloomPetal, bloomNPet)
	for i := range petals {
		petals[i] = bloomPetal{
			az:  (float64(i)/bloomNPet)*tau + 0.3,
			off: math.Mod(float64(i)*0.7, 1),
			at:  bloomLift + math.Mod(float64(i)*0.37, 1)*0.55,
		}
	}
	t := 0.0
	popped, sprouted := false, false
	grow := func() float64 {
		return pixelfx.Ease.OutCubic(pixelfx.Clamp(t/bloomStem)) * (1 - pixelfx.Ease.InCubic(pixelfx.Clamp((t-bloomWilt)/0.6)))
	}
	sway := func() float64 { return math.Sin(t*2.6) * 1.4 * pixelfx.Smooth(0.2, 0.6, t) }
	// the stem's points, base to tip: a gentle S that sways
	stem := func() [13][2]float64 {
		g := grow()
		var pts [13][2]float64
		for i := 0; i <= 12; i++ {
			s := float64(i) / 12
			pts[i] = [2]float64{math.Sin(s*3.2)*2.2*s + sway()*s*s, (1-g)*bloomH*1.1 - s*bloomH}
		}
		return pts
	}
	open := func() float64 { return pixelfx.OutBack(pixelfx.Clamp((t-bloomBud)/(bloomOpen-bloomBud)), 1.4) }
	// a petal: from closed round the bud to splayed open round the heart, foreshortened by where it points
	drawPetal := func(p *pixelfx.Painter, q bloomPetal, top [2]float64) {
		op := open()
		e := pixelfx.Lerp(0.15, pi/2-0.2, op)
		dx := math.Sin(e) * math.Cos(q.az)
		dz := math.Sin(e) * math.Sin(q.az)
		dy := -math.Cos(e)
		L := pixelfx.Lerp(8, 12, op)
		length := L * pixelfx.Hypot(dx, dy)
		ang := math.Atan2(dy, dx)
		back := dz < 0
		cy := top[1] - 3
		if length < 1.5 {
			return
		}
		fl := 0.2
		pal := [4]uint32{Pt[0], Pt[1], Pt[2], Pt[3]}
		if back {
			fl = 0.9
			pal = [4]uint32{Pt[0], Pt[0], Pt[1], Pt[2]}
		}
		drawLeaf(p, top[0]+dx*L*0.5, cy+dy*L*0.5, length+1, 3+(1-math.Abs(dz))*0.8, ang, fl, pal, 1)
	}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			f := math.Min(1, dt*120)
			top := stem()[12]
			if !sprouted {
				sprouted = true
				for range 8 {
					px := R.Range(-3, 3)
					vx := R.Range(-40, 40)
					vy := -R.Range(40, 100)
					life := R.Range(0.5, 0.8)
					c := pixelfx.Pick(R, []uint32{C[2], C[3]})
					ps.Spawn(pixelfx.Grit, px, -1, vx, vy, life, c, 1, 0, 0)
				}
				for range 3 {
					px := R.Range(-3, 3)
					vx := R.Sign() * R.Range(10, 30)
					vy := -R.Range(4, 10)
					life := R.Range(0.4, 0.6)
					sz := R.Range(1.5, 2.5)
					ps.Spawn(pixelfx.Dust, px, -1, vx, vy, life, C[3], sz, 0, 0)
				}
			}
			// the bud draws light in as it swells
			if t > bloomStem && t < bloomBud && R.Chance(0.3*f) {
				a := R.Range(0, tau)
				d := R.Range(14, 26)
				x := top[0] + math.Cos(a)*d
				y := top[1] - 3 + math.Sin(a)*d*0.8
				life := R.Range(0.18, 0.26)
				c := C[6]
				if R.Chance(0.5) {
					c = Ht[2]
				}
				ps.Spawn(pixelfx.Streak, x, y, (top[0]-x)*3.4, (top[1]-3-y)*3.4, life, c, 1, 0, 0)
			}
			if !popped && t >= bloomOpen-0.12 {
				popped = true
				lights.Add(top[0], top[1]-3, 22, pixelfx.White, 0.45, 0.08)
				lights.Add(top[0], top[1]-3, 70, Ht[2], 0.5, 0.8)
				for range 16 {
					a := R.Range(0, tau)
					sp := R.Range(40, 120)
					life := R.Range(0.4, 0.8)
					c := C[7]
					if R.Chance(0.5) {
						c = Ht[3]
					}
					ps.Spawn(pixelfx.Mote, top[0], top[1]-3, math.Cos(a)*sp, math.Sin(a)*sp-20, life, c, 0.7, 0, 0)
				}
				for range 5 {
					px := top[0] + R.Range(-10, 10)
					py := top[1] - R.Range(0, 14)
					life := R.Range(0.3, 0.6)
					sz := R.Range(1, 2.4)
					delay := R.Range(0, 0.3)
					ps.Spawn(pixelfx.StarKind, px, py, 0, -6, life, Ht[3], sz, delay, 0)
				}
			}
			// while it blooms: crosses and motes of light drift up round it
			if t > bloomOpen && t < bloomWilt && R.Chance(0.13*f) {
				gx := R.Range(-22, 22)
				gy := -R.Range(2, 36)
				life := math.Min(R.Range(0.7, 1.1), bloomDuration-0.05-t)
				L := 2
				if R.Chance(0.35) {
					L = 3
				}
				seed := R.Next() * 9
				glyphs = append(glyphs, bloomGlyph{x: gx, y: gy, t0: t, life: life, L: L, seed: seed})
			}
			if t > bloomOpen-0.1 && t < bloomWilt && R.Chance(0.35*f) {
				px := R.Range(-20, 20)
				py := -R.Range(0, 8)
				vx := R.Range(-6, 6)
				vy := -R.Range(25, 55)
				life := math.Min(R.Range(0.6, 1.1), bloomDuration-0.05-t)
				c := Ht[2]
				if R.Chance(0.6) {
					c = C[6]
				}
				sz := 1.0
				if R.Chance(0.3) {
					sz = 2
				}
				ps.Spawn(pixelfx.Ember, px, py, vx, vy, life, c, sz, 0, 0)
			}
			for i := len(glyphs) - 1; i >= 0; i-- {
				if t-glyphs[i].t0 > glyphs[i].life {
					glyphs = append(glyphs[:i], glyphs[i+1:]...)
				}
			}
			// petals let go one by one and lift away, turning, until they twinkle out
			for i := range petals {
				q := &petals[i]
				if !q.gone && t >= q.at {
					q.gone = true
					e := pi/2 - 0.15
					dx := math.Sin(e) * math.Cos(q.az)
					L := 10.0
					vx := dx*30 + R.Range(-10, 10)
					vy := -R.Range(25, 45)
					va := R.Range(-5, 5)
					vf := R.Range(5, 9)
					life := math.Min(R.Range(0.75, 1.0), bloomDuration-0.45-t)
					flying = append(flying, bloomFlying{
						x: top[0] + dx*L*0.6, y: top[1] - 3 - math.Cos(e)*L*0.6,
						vx: vx, vy: vy, a: math.Atan2(-math.Cos(e), dx), va: va, fl: 0, vf: vf, life: life, max: life,
					})
				}
			}
			for i := len(flying) - 1; i >= 0; i-- {
				q := &flying[i]
				q.life -= dt
				if q.life <= 0 {
					ps.Spawn(pixelfx.StarKind, q.x, q.y, 0, -8, math.Min(0.35, bloomDuration-0.05-t), Pt[3], 1.8, 0, 0)
					flying = append(flying[:i], flying[i+1:]...)
					continue
				}
				q.vx -= q.vx * 1.5 * dt
				q.vy -= q.vy*0.8*dt + 10*dt
				q.x += q.vx*dt + math.Sin(q.fl)*12*dt
				q.y += q.vy * dt
				q.a += q.va * dt
				q.fl += q.vf * dt
			}
			ps.Update(dt)
			lights.Update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			bloom := pixelfx.Smooth(bloomOpen-0.15, bloomOpen+0.2, t) * (1 - pixelfx.Smooth(bloomWilt-0.2, bloomWilt+0.5, t))
			top := stem()[12]
			// a column of soft healing light, and a lit ring on the floor that blooms out as it opens
			for h := 0.0; h < 70; h += 7 {
				p.Glow(0, -h, 22, C[5], 0.12*bloom*(1-h/80))
			}
			k := pixelfx.Clamp((t - bloomOpen + 0.12) / 0.6)
			if k > 0 && k < 1 {
				pixelfx.Band(p, 0, 0.5, 6+30*pixelfx.Ease.OutCubic(k), 0.18, math.Max(1, 3*(1-k)), 1.1-k, []uint32{C[3], C[5], Ht[2], Ht[3]}, func(g float64) bool { return math.Sin(g) < 0 }, false)
			}
			fw := 6 + 16*pixelfx.Smooth(0, 0.8, t)
			for x := math.Floor(-fw); x <= fw; x++ {
				u := math.Abs(x) / fw
				a := math.Sqrt(1-u) * pixelfx.Smooth(0, 0.4, t) * (1 - pixelfx.Smooth(bloomWilt, bloomWilt+0.6, t))
				col := C[4]
				if u < 0.3 {
					col = C[6]
				} else if u < 0.65 {
					col = C[5]
				}
				p.Dot(x, 0, col, a)
				if int(x)%5 == 0 {
					p.Glow(x, 0, 8, C[5], 0.2*a*(1-u))
				}
			}
			// the far petals sit behind the heart
			if t > bloomBud-0.05 {
				for _, q := range petals {
					if math.Sin(q.az) < 0 && !q.gone {
						drawPetal(p, q, top)
					}
				}
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			pts := stem()
			top := pts[12]
			g := grow()
			if g > 0.02 {
				// the stem: a 2 px shoot, lit down its left side
				for i := 1; i < len(pts); i++ {
					x0, y0 := pts[i-1][0], pts[i-1][1]
					x1, y1 := pts[i][0], pts[i][1]
					if y1 > 0 {
						continue
					}
					p.Line(x0, y0, x1, y1, C[5], 1)
					p.Line(x0+1, y0, x1+1, y1, C[3], 1)
				}
				// leaves unfurl off it
				for _, lf := range [2]struct {
					i     int
					sd, d float64
				}{{4, -1, 0.1}, {7, 1, 0.22}} {
					lk := pixelfx.OutBack(pixelfx.Clamp((t-lf.d-0.12)/0.3), 1.6) * (1 - pixelfx.Smooth(bloomWilt, bloomWilt+0.5, t)*0.4)
					x, y := pts[lf.i][0], pts[lf.i][1]
					if lk <= 0 || y > -1 {
						continue
					}
					la := -0.55 - 0.4*(1-lk)
					if lf.sd < 0 {
						la = -2.6 + 0.4*(1-lk)
					}
					drawLeaf(p, x+math.Cos(la)*6*lk, y+math.Sin(la)*6*lk, 12*lk, 3.4, la, 0.25, [4]uint32{C[1], C[3], C[5], C[6]}, 1)
				}
				// the bud, then the heart once it opens
				if t > bloomStem-0.1 && t < bloomOpen-0.1 {
					b := pixelfx.Clamp((t - bloomStem + 0.1) / (bloomBud - bloomStem))
					w, h := 2+2.5*b, 3.5+5*b
					p.EllipseFn(top[0], top[1]-h*0.6, w, h, func(d, a float64) (uint32, float64) {
						if math.Cos(a) < -0.2 {
							return Pt[2], 1
						}
						if d > 0.7 {
							return Pt[0], 1
						}
						return Pt[1], 1
					})
					p.Glow(top[0], top[1]-h*0.6, 8+8*b, Ht[2], 0.25*b)
				}
			}
			for _, q := range petals {
				if math.Sin(q.az) >= 0 && !q.gone && t > bloomBud-0.05 {
					drawPetal(p, q, top)
				}
			}
			if t > bloomOpen-0.12 && t < bloomWilt+0.3 {
				pulse := 0.85 + 0.15*math.Sin(t*9)
				k := 1 - pixelfx.Smooth(bloomWilt-0.3, bloomWilt+0.3, t)
				cy := top[1] - 4
				p.Circle(top[0], cy, 3.3*k, Ht[1], 1)
				p.Circle(top[0]-0.5, cy-0.5, 2.2*k, Ht[2], 1)
				p.Px(top[0]-1, cy-1, Ht[3])
				p.Glow(top[0], cy, 16*pulse, Ht[2], 0.5*k)
				p.Glow(top[0], cy, 34, C[5], 0.2*k)
			}
			e := t - bloomOpen + 0.12
			if e > 0 && e < 0.16 {
				pixelfx.Sparkle(p, top[0], top[1]-3, 10*(1-e/0.16), Ht[3], Ht[2], 0.6)
			}
			for _, q := range flying {
				drawLeaf(p, q.x, q.y, 6, 2.2, q.a, q.fl, [4]uint32{Pt[0], Pt[1], Pt[2], Pt[3]}, math.Min(1, q.life/0.3))
			}
			for _, q := range glyphs {
				k := (t - q.t0) / q.life
				drawBloomCross(p, q.x+math.Sin(k*5+q.seed)*2, q.y-k*24, q.L, C, math.Sin(pi*k), math.Sin(k*14+q.seed) > 0.8)
			}
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
