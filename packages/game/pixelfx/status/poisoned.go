// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/poisoned.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package status

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

const (
	poisonedDuration = 1.6
	poisonedTick     = 0.8
	poisonedG        = 760.0
)

var poisonedSick = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.8), pixelfx.Crouch.Is(4), pixelfx.Lean.Is(4), pixelfx.HY.Is(1), pixelfx.FX.Is(2), pixelfx.FY.Is(-20), pixelfx.BX.Is(8), pixelfx.BY.Is(-14), pixelfx.FFX.Is(3), pixelfx.FBX.Is(-3))

// Poisoned is sick to the bone: green rolls up the body in waves, dark veins creep from a wound, toxic sweat drips and hisses; every 0.8 s it wobbles, blisters pop and a ring pulses out. It is registered as "status/poisoned".
var Poisoned = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Poisoned",
		Desc:     "Sick to the bone: green rolls up the body in waves, dark veins creep from a wound, toxic sweat drips and hisses; every 0.8 s it wobbles, blisters pop and a ring pulses out.",
		Tags:     []string{"status", "poison", "toxic", "dot", "debuff", "sick", "loop"},
		Size:     [2]int{72, 64},
		Origin:   [2]int{36, 54},
		Duration: poisonedDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0e1d14", "#173320", "#23552e", "#318232", "#55b92f", "#8eea3f", "#cfff63", "#f6ffc2")},
			{Name: "veins", Default: pixelfx.Color("#2c1a3a")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: poisonedSick.With(pixelfx.X.Is(-1), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5))},
			{T: 0.2, Pose: poisonedSick.With(pixelfx.X.Is(0))},
			{T: 0.5, Pose: poisonedSick.With(pixelfx.X.Is(1))},
			{T: 0.8, Pose: poisonedSick.With(pixelfx.X.Is(1), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5))},
			{T: 1, Pose: poisonedSick.With(pixelfx.X.Is(0))},
			{T: 1.3, Pose: poisonedSick.With(pixelfx.X.Is(-1))},
			{T: 1.6, Pose: poisonedSick.With(pixelfx.X.Is(-1), pixelfx.Crouch.Is(5), pixelfx.Lean.Is(5))},
		},
	},
	ActorAlpha: func(float64, *pixelfx.Options) float64 { return 0 },
	New:        newPoisoned,
}

func init() { pixelfx.Register("status/poisoned", Poisoned) }

func poisonedSince(t float64) float64 { return pixelfx.Wrap(t, poisonedTick) }

type veinPt struct {
	x, y int
	u    float64
}

type blister struct {
	x, y int
	r    float64
}

type poisonedBody struct {
	veins    [][]veinPt
	wound    [2]int
	blisters [2][3]blister
	sweat    [][2]int
}

func poisonedBodyOf(a *pixelfx.Actor, S *actorShape, E []eye, seed float64) poisonedBody {
	H, W := S.h, S.w
	R := pixelfx.NewRng(seed*7 + 3)
	wy := int(pixelfx.Round(float64(H) * 0.6))
	l, r := S.rows[wy][0], S.rows[wy][1]
	wx := r - 1
	var veins [][]veinPt
	var walk func(x, y, ang, ln float64, depth int)
	walk = func(x, y, ang, ln float64, depth int) {
		var pts []veinPt
		for s := 0.0; s < ln; s++ {
			ang += R.Range(-0.5, 0.5)
			x += math.Cos(ang)
			y += math.Sin(ang)
			rx, ry := int(pixelfx.Round(x)), int(pixelfx.Round(y))
			if !S.on(rx, ry) {
				break
			}
			pts = append(pts, veinPt{rx, ry, s / ln})
			if depth < 2 && s > 2 && R.Chance(0.16) {
				walk(x, y, ang+R.Sign()*R.Range(0.6, 1.1), ln*0.55, depth+1)
			}
		}
		veins = append(veins, pts)
	}
	for i := 0; i < 4; i++ {
		walk(float64(wx), float64(wy), pi+(float64(i)/4-0.45)*2.2, 11, 0)
	}
	var skin [][2]int
	var blCand [][2]int
	for _, pt := range S.body {
		x, y := pt[0], pt[1]
		if pixelfx.Luma(colorAt(a, x, y)) > 0.55 && S.on(x-1, y) && S.on(x+1, y) {
			skin = append(skin, pt)
		}
		if y > 4 && S.on(x-1, y) && S.on(x+1, y) && S.on(x, y-1) {
			blCand = append(blCand, pt)
		}
	}
	pick := func(list [][2]int) [2]int {
		return list[int(math.Floor(R.Next()*float64(len(list))))]
	}
	var blisters [2][3]blister
	for tick := 0; tick < 2; tick++ {
		for k := 0; k < 3; k++ {
			pt := pick(blCand)
			blisters[tick][k] = blister{x: pt[0], y: pt[1], r: R.Range(1.1, 1.8)}
		}
	}
	var sweat [][2]int
	for _, e := range E {
		sx := e.x1 + 1
		if float64(e.x0) < float64(W)/2 {
			sx = e.x0 - 1
		}
		sweat = append(sweat, [2]int{sx, e.y0 - 1})
	}
	for _, side := range [2]int{-1, 1} {
		for _, pt := range S.body {
			x, y := pt[0], pt[1]
			okSide := x >= r-1
			if side < 0 {
				okSide = x <= l+1
			}
			if float64(y) > float64(H)*0.5 && float64(y) < float64(H)*0.7 && okSide && !S.on(x, y+1) {
				sweat = append(sweat, pt)
				break
			}
		}
	}
	if len(sweat) == 0 {
		if len(skin) > 0 {
			sweat = append(sweat, pick(skin))
		} else {
			sweat = append(sweat, pick(S.body))
		}
	}
	return poisonedBody{veins: veins, wound: [2]int{wx, wy}, blisters: blisters, sweat: sweat}
}

func poisonedOffsets(t, H float64) func(my int) float64 {
	lean := 1.3 * math.Sin((t/poisonedDuration)*tau)
	ph := poisonedSince(t)
	w := 0.0
	if ph < 0.12 {
		w = math.Sin((ph / 0.12) * pi)
	}
	return func(my int) float64 {
		return pixelfx.Round(lean*(1-float64(my)/H) + w*math.Sin(float64(my)*0.42+ph*40))
	}
}

func newPoisoned(o *pixelfx.Options) pixelfx.Instance {
	P := pixelfx.Fit(o.Palette(), 8)
	vein := o.Color("veins")
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	B := poisonedBodyOf(a, S, E, seed)
	H := float64(S.h)
	bandRamp := []uint32{P[3], P[4], P[5], P[6]}
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			ph := poisonedSince(t)
			k := ph / 0.55
			if k < 1 {
				pixelfx.Band(p, 0, 0, 5+22*math.Sqrt(k), 0.18, math.Max(1, 2.5*(1-k)), 1-k, bandRamp, func(g float64) bool { return math.Sin(g) < 0 }, false)
			}
			p.Shade(-28, -3, 28, 6, func(x, y float64, out *pixelfx.Out) uint32 {
				d := pixelfx.Hypot(x/24, (y-0.5)/4)
				if d < 1 {
					out.L = P[4]
					out.K = (0.14 + 0.2*math.Exp(-ph/0.2)) * (1 - d) * (1 - d)
				}
				return 0
			})
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			off := poisonedOffsets(t, H)
			ph := poisonedSince(t)
			tick := int(math.Floor(t/poisonedTick)) % 2
			for _, pt := range S.body {
				mx, my := pt[0], pt[1]
				c := colorAt(a, mx, my)
				l := pixelfx.Luma(c)
				wave := 0.5 + 0.5*math.Sin(tau*((float64(my)/H)*1.6+(t/poisonedDuration)*2))
				sick := P[int(pixelfx.ClampTo(1+pixelfx.Round(l*6.2), 1, 7))]
				k := 0.22 + 0.4*wave*wave
				dst := sick
				if wave > 0.85 && l > 0.4 {
					dst = pixelfx.Mix(sick, P[6], 0.3)
				}
				p.Px(a.X+float64(mx)+off(my), a.Y+float64(my), pixelfx.Mix(c, dst, k))
			}
			grow := pixelfx.Smooth(0, poisonedDuration*0.55, t) * (1 - pixelfx.Smooth(poisonedDuration*0.62, poisonedDuration, t)) * 1.15
			for _, pts := range B.veins {
				for _, pt := range pts {
					if pt.u < grow {
						col := vein
						if pt.u > grow-0.12 {
							col = P[2]
						}
						p.Px(a.X+float64(pt.x)+off(pt.y), a.Y+float64(pt.y), col)
					}
				}
			}
			wx, wy := B.wound[0], B.wound[1]
			p.Px(a.X+float64(wx)+off(wy), a.Y+float64(wy), P[6])
			p.Px(a.X+float64(wx)+off(wy)-1, a.Y+float64(wy), P[3])
			p.Glow(a.X+float64(wx), a.Y+float64(wy), 6, P[5], 0.3)
			for _, bl := range B.blisters[tick] {
				x, y := a.X+float64(bl.x)+off(bl.y), a.Y+float64(bl.y)
				grow2 := pixelfx.Clamp(ph / 0.16)
				pop := ph > 0.38
				if !pop {
					r := bl.r * grow2
					p.Circle(x, y, r+0.6, P[3], 1)
					p.Circle(x-0.2, y-0.2, r, P[5], 1)
					if r > 0.8 {
						p.Px(x-0.5, y-0.8, pixelfx.White)
					}
					p.Glow(x, y, 4, P[5], 0.3*grow2)
				} else if ph < 0.56 {
					k := (ph - 0.38) / 0.18
					for j := 0; j < 6; j++ {
						an := (float64(j)/6)*tau + float64(bl.x)
						d := bl.r + 0.5 + k*3.5
						col := P[5]
						if j%2 != 0 {
							col = P[6]
						}
						p.Dot(x+math.Cos(an)*d, y+math.Sin(an)*d-k*2, col, 1.2-k)
					}
					p.Glow(x, y, 6, P[6], 0.4*(1-k))
				}
			}
			for i, sw := range B.sweat {
				sx, sy := sw[0], sw[1]
				per := poisonedDuration / 2
				age := pixelfx.Wrap(t-float64(i)*0.37, per)
				gather := 0.35
				x := a.X + float64(sx) + 0.5 + off(sy)
				y0 := a.Y + float64(sy)
				if age < gather {
					k := age / gather
					run := pixelfx.Round(k * 2)
					col := P[5]
					if k > 0.5 {
						col = P[6]
					}
					p.Px(x, y0+run, col)
					if k > 0.5 {
						p.Px(x, y0+run-1, P[4])
					}
					continue
				}
				f := age - gather
				fallH := -1 - (y0 + 2)
				tf := math.Sqrt((2 * math.Max(1, fallH)) / poisonedG)
				if f < tf {
					y := y0 + 2 + 0.5*poisonedG*f*f
					p.Line(x, y-math.Min(3, poisonedG*f*0.012), x, y, P[4], 1)
					p.Px(x, y, P[6])
					continue
				}
				h := f - tf
				if h > 0.4 {
					continue
				}
				k := h / 0.4
				lc := P[4]
				if k < 0.4 {
					lc = P[6]
				}
				p.Line(x-1-k*2, -1, x+1+k*2, -1, lc, 1.2-k)
				for j := 0; j < 3; j++ {
					p.Dot(x+float64(j-1)*(2+3*k), -2-float64(j%2)-5*k*(1-k), P[6], 1-k)
				}
				p.Disc(x+math.Sin(k*6)*1.5, -3-k*6, 1+1.5*k, P[5], 0.35*(1-k))
				p.Glow(x, -1.5, 5, P[5], 0.35*(1-k))
			}
			k := ph / 0.55
			if k < 1 {
				pixelfx.Band(p, 0, 0, 5+22*math.Sqrt(k), 0.18, math.Max(1, 2.5*(1-k)), 1-k, bandRamp, func(g float64) bool { return math.Sin(g) >= 0 }, false)
			}
			p.Glow(0, a.Y+H*0.45, 26, P[4], 0.14+0.2*math.Exp(-ph/0.18))
		},
	}
}
