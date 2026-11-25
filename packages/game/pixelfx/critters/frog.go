// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/frog.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Frog is a frog puffing its throat, blinking, crouching and leaping to snap a buzzing fly out of the air, then landing with a squash and a puff of dust, gulping and settling. It is registered as "critters/frog".
var Frog = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Frog",
		Desc:     "A frog puffs its throat, blinks, crouches and leaps to snap a buzzing fly out of the air, then lands with a squash and a puff of dust, gulps and settles.",
		Tags:     []string{"critter", "frog", "animal", "hop", "ground", "loop"},
		Size:     [2]int{112, 88},
		Origin:   [2]int{56, 80},
		Duration: 2.6,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#0a160c", "#173a18", "#256024", "#3a8a2e", "#62b440", "#a8e070", "#e8ffc0")},
			{Name: "belly", Default: pixelfx.Palette("#5e5a24", "#bdb45a", "#efe9a8")},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newFrog,
}

func init() { pixelfx.Register("critters/frog", Frog) }

var (
	frogD                                       = 2.6
	frogCrouch, frogUp, frogLand, frogSnap      = 0.95, 1.2, 1.62, 1.4
	frogBack, frogG                             = 1.95, 1000.0
	frogHop                                     = (frogG * math.Pow(frogLand-frogUp, 2)) / 8
	frogEye                                     = pixelfx.Ramp("#3a2a08", "#c89420", "#ffd860")
	frogTongue, frogFly                         = pixelfx.Hex("#e8607a"), pixelfx.Hex("#141018")
	frogWings, frogDust                         = pixelfx.Hex("#c8d0e0"), pixelfx.Hex("#8a7f92")
	frogGrit                                    = [2]uint32{pixelfx.Hex("#9a90a4"), pixelfx.Hex("#6a6074")}
	frogGlintX, frogGlintY, frogMouthY, frogMid = 4.6, -14.4, -7.8, 7.0
)

var (
	frogThroats = [3][2]float64{{0.05, 0.45}, {0.5, 0.85}, {1.75, 1.95}}
	frogSpots   = [4][3]float64{{-5, -10, 1.6}, {0, -11.5, 1.3}, {-8, -7, 1.2}, {3, -12.5, 0.9}}
)

// frogPose is the frog's pose: lift off the floor, nose tilt, squash, hind legs out, throat puffed, eye shut, in the air.
type frogPose struct {
	by, rot, sq, leg, throat float64
	shut, air                bool
}

func frogPoseAt(t float64) frogPose {
	var P frogPose
	age := pixelfx.Wrap(t-frogLand, frogD)
	for _, w := range frogThroats {
		a, b := w[0], w[1]
		if t > a && t < b {
			k := 1.0
			if a > 1 {
				k = 0.6
			}
			P.throat = math.Pow(math.Sin(pi*((t-a)/(b-a))), 0.7) * k
		}
	}
	P.shut = t > 0.88 && t < 0.95
	switch {
	case t >= frogCrouch && t < frogUp:
		u := pixelfx.Ease.InOutSine(pixelfx.Seg(t, frogCrouch, frogUp))
		P.sq, P.rot = -0.18*u, -0.12*u
	case t >= frogUp && t < frogLand:
		q := (t - frogUp) / (frogLand - frogUp)
		P.by = -frogHop * 4 * q * (1 - q)
		P.rot = pixelfx.Lerp(0.42, -0.25, q)
		P.sq = pixelfx.Lerp(0.18, 0.02, math.Min(1, q*2))
		P.leg = 1
		if q >= 0.75 {
			P.leg = 1 - (q-0.75)/0.25
		}
	default:
		P.sq = -0.24 * spring(age, 2.4, 6.5)
		P.rot = -0.08 * math.Exp(-age*6)
	}
	P.air = t >= frogUp && t < frogLand
	return P
}

// frogFlyAt is the fly: buzzing a little loop over the frog, snapped up at the snap, buzzing back in from the side at frogBack; ok is false while it is gone.
func frogFlyAt(t float64) (x, y float64, ok bool) {
	if t > frogSnap && t < frogBack {
		return 0, 0, false
	}
	a := (t / frogD) * tau * 3
	hx, hy := 16+6*math.Sin(a), -40+3*math.Sin(2*a)
	if inn := pixelfx.Wrap(t-frogBack, frogD); inn < 0.45 {
		u := pixelfx.Ease.OutCubic(inn / 0.45)
		return pixelfx.Lerp(-52, hx, u), pixelfx.Lerp(-46, hy, u) + math.Sin(inn*30)*1.5, true
	}
	return hx, hy, true
}

func drawFrogGrid(g *grid, C, Bl []uint32, P *frogPose) {
	g.clear()
	cs, sn, sx, sy := math.Cos(P.rot), math.Sin(P.rot), 1+P.sq*-0.6, 1+P.sq
	// into the frog's own frame: centred on its body, turned with it, squashed with it
	local := func(x, y float64) (float64, float64) {
		dx, dy := x, y+7
		return (dx*cs + dy*sn) / sx, (-dx*sn+dy*cs)/sy - 7
	}
	inBody := func(x, y float64) bool {
		return math.Pow((x+1)/10.5, 2)+math.Pow((y+7)/6, 2) <= 1 || math.Pow((x-6)/6.2, 2)+math.Pow((y+8.6)/4.8, 2) <= 1
	}
	inEye := func(x, y float64) bool { return math.Pow(x-5.5, 2)+math.Pow(y+13.2, 2) <= math.Pow(3.2, 2) }
	// the body and head: lit on top, a pale belly, dark spots on the back
	lit := func(x, y, k float64) bool { return inBody(-4+(x+4)/k, -13+(y+13)/k) }
	g.shade(-15, -22, 15, 1, func(x, y float64, _ *gridOut) uint32 {
		u, v := local(x, y)
		if !inBody(u, v) && !inEye(u, v) {
			return 0
		}
		if inEye(u, v) {
			d := pixelfx.Hypot(u-5.5, v-13.2*-1)
			if P.shut {
				if v < -13.2 {
					return C[4]
				}
				return C[3]
			}
			if math.Abs(v+13) < 0.7 && math.Abs(u-6) < 1.8 {
				return C[0]
			}
			if u < 4.7 && v < -14 {
				return frogEye[2]
			}
			if d < 2.4 {
				return frogEye[1]
			}
			return frogEye[0]
		}
		if v > -5 && u > -6 {
			if v > -3 {
				return Bl[1]
			}
			return Bl[2]
		}
		for _, s := range frogSpots {
			if pixelfx.Hypot(u-s[0], v-s[1]) < s[2] {
				return C[2]
			}
		}
		switch {
		case !inBody(u+2, v+2.4):
			return C[2]
		case lit(u, v, 0.5):
			return C[5]
		case lit(u, v, 0.8):
			return C[4]
		}
		return C[3]
	})
	// hind leg: a thigh folded at the rump, or kicked out straight when it leaps
	var leg [][2]float64
	if P.leg < 0.5 {
		g.ellipse(-7.5, -4.5+(1-sy)*3, 5, 3.6, func(nx, ny float64) uint32 {
			switch {
			case nx*-0.6+ny*-0.8 > 0.35:
				return C[4]
			case ny > 0.4:
				return C[2]
			}
			return C[3]
		}, 1)
		leg = [][2]float64{{-4, -1.5}, {-9, -1}, {-3, -0.5}, {4, -0.5}}
	} else {
		ex, ey := -8-10*P.leg, 4+6*P.leg
		leg = [][2]float64{{-6, -5}, {-10, 0}, {ex, ey}, {ex - 5, ey + 2}}
	}
	// a tapering tube: a thick thigh, a thinner shin, a long foot; lit along its top
	g.tube(leg, func(u float64) float64 { return pixelfx.Lerp(2, 0.7, u) }, func(u, v float64) uint32 {
		switch {
		case v > 0.55:
			return C[2]
		case u > 0.66:
			return C[3]
		}
		return C[4]
	})
	// webbed toes spread at the end of the foot
	fx, fy, px, py := leg[len(leg)-1][0], leg[len(leg)-1][1], leg[len(leg)-2][0], leg[len(leg)-2][1]
	dx, dy := fx-px, fy-py
	l := pixelfx.Hypot(dx, dy)
	if l == 0 {
		l = 1
	}
	for _, k := range [3]float64{-1, 0, 1} {
		g.line(fx, fy, fx+(dx/l)*2-(dy/l)*k*1.6, fy+(dy/l)*2+(dx/l)*k*1.6, C[3])
	}
	// glint in the eye, the mouth line
	if !P.shut {
		gx, gy := frogGlintX, frogGlintY
		g.put(gx*cs-(gy+frogMid)*sn*1, gx*sn+(gy+frogMid)*cs-frogMid, C[6])
	}
	for u := 3.0; u <= 11; u++ {
		v := frogMouthY + (u-3)*0.05
		g.put((u*sx)*cs-((v+7)*sy)*sn, (u*sx)*sn+((v+7)*sy)*cs-7, C[1])
	}
	// the front leg props it up, or tucks in when it leaps
	f1 := [2]float64{8, -0.5}
	if P.air {
		f1 = [2]float64{9, -1}
	}
	g.line(7, -4, f1[0], f1[1], C[3])
	g.put(f1[0]+1, f1[1], C[3])
	g.put(f1[0]-1, f1[1], C[3])
	// the throat sac swells into a glossy bubble under the chin
	if P.throat > 0.05 {
		r := 0.8 + 2.8*P.throat
		cx, cy := 7.0, -5.2+r*0.25
		g.ellipse(cx, cy, r*1.1, r, func(nx, ny float64) uint32 {
			switch {
			case nx*-0.6+ny*-0.8 > 0.55:
				return Bl[2]
			case ny > 0.5:
				return Bl[1]
			}
			return pixelfx.Mix(Bl[2], C[5], 0.4)
		}, 0.9)
		g.put(cx-r*0.4, cy-r*0.45, C[6])
	}
	g.outline(C[0], 1)
}

// frogDusts are the dust bursts: when, how many puffs, how strong.
var frogDusts = [2][3]float64{{frogUp, 7, 0.6}, {frogLand, 18, 1}}

func newFrog(o *pixelfx.Options) pixelfx.Instance {
	C, Bl, seed := pixelfx.Fit(o.Palette(), 7), pixelfx.Fit(o.Ramp("belly"), 3), o.Seed()
	g, sw := newGrid(-22, -40, 44, 44), newSwarm()
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, frogD, seed)
			P := frogPoseAt(t)
			hgt := pixelfx.Clamp(-P.by / frogHop)
			shadow(p, 0, -0.2, 13*(1-0.35*hgt), 1.6, C[0], 0.6-0.3*hgt)
		},
		DrawFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, frogD, seed)
			P := frogPoseAt(t)
			drawFrogGrid(g, C, Bl, &P)
			g.draw(p, 1, 0, pixelfx.Round(P.by))
			// the tongue whips out to the fly and back
			if ts := (t - frogSnap + 0.06) / 0.14; ts > 0 && ts < 1 {
				fx, fy, _ := frogFlyAt(frogSnap - 0.001)
				e, mx, my := math.Sin(pi*ts), 11.0, P.by-8
				x1, y1 := pixelfx.Lerp(mx, fx, e), pixelfx.Lerp(my, fy, e)
				p.Line(mx, my, x1, y1, frogTongue, 1)
				p.Line(mx, my+1, x1, y1+1, pixelfx.Mix(frogTongue, C[0], 0.4), 1)
				p.Circle(x1, y1, 1.4, frogTongue, 1)
			}
			// the fly, buzzing
			if fx, fy, ok := frogFlyAt(t); ok {
				up0, up1 := 0.0, 1.0
				if math.Sin(t*90) > 0 {
					up0, up1 = 1, 0
				}
				p.Px(fx, fy, frogFly)
				p.Px(fx+1, fy, frogFly)
				p.Dot(fx, fy-up0, frogWings, 0.8)
				p.Dot(fx+1, fy-up1, frogWings, 0.8)
				p.Glow(fx, fy, 3, frogWings, 0.15)
			}
			// dust thrown out along the floor as it takes off and lands, and grit bouncing
			sw.begin(t)
			for _, d := range frogDusts {
				t0, n, pow := d[0], int(d[1]), d[2]
				age := pixelfx.Wrap(t-t0, frogD)
				for i := 0; i < n; i++ {
					r := pixelfx.RngOf(seed, float64(i), t0*100)
					L := r.Range(0.6, 1.1) * pow
					if age > L {
						continue
					}
					s, v := r.Sign(), r.Range(24, 70)*pow
					x, y := s*(7+(v/3.5)*(1-math.Exp(-age*3.5))), -1.5-r.Range(1, 5)*age
					sw.add(pixelfx.Smoke, x, y, 0, 0, age, L, frogDust, r.Range(2.2, 4.2)*(0.65+0.35*pow), pixelfx.Big, 0)
				}
				if pow == 1 {
					for i := 0; i < 8; i++ {
						r := pixelfx.RngOf(seed, float64(i), 909)
						b := toss(r.Range(-8, 8), -1, r.Range(-40, 40), -r.Range(30, 70), age, 700, -0.5)
						if age < 0.7 {
							a := 1.0
							if b.landed {
								a = 1.4 - age*2
							}
							p.Dot(b.x, b.y, frogGrit[i%2], a)
						}
					}
				}
			}
			sw.draw(p)
		},
	}
}
