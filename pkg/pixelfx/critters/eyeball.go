// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/critters/eyeball.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package critters

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Eyeball is a bloodshot eyeball floating in its fleshy lids: its glowing iris snaps from one look to the next, it blinks, and its nerve sways behind it as it bobs. It is registered as "critters/eyeball".
var Eyeball = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Eyeball",
		Desc:     "A bloodshot eyeball floats in its fleshy lids: its glowing iris snaps from one look to the next, it blinks, and its nerve sways behind it as it bobs.",
		Tags:     []string{"critter", "eyeball", "eye", "monster", "spooky", "float", "loop"},
		Size:     [2]int{96, 108},
		Origin:   [2]int{48, 100},
		Duration: 3.2,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#2a1420", "#6a3040", "#b07078", "#dcbcb4", "#f4e9de", "#ffffff")},
			{Name: "flesh", Default: pixelfx.Palette("#1e0c16", "#4a1a2c", "#7c2c40", "#b04a58", "#e07a80")},
			{Name: "iris", Default: pixelfx.Palette("#0c2a14", "#1f6a2a", "#3fae3a", "#9be05a", "#e8ffb0")},
			{Name: "hover", Default: pixelfx.Num(48)},
			{Name: "seed", Default: pixelfx.Num(1)},
		},
	},
	New: newEyeball,
}

func init() { pixelfx.Register("critters/eyeball", Eyeball) }

var (
	eyeD, eyeR   = 3.2, 13.0
	eyeVein      = pixelfx.Hex("#c42a3a")
	eyeShadowCol = pixelfx.Hex("#0d0a12")
)

// eyeLooks is where it looks: it snaps between targets with a little overshoot, holds, snaps on.
var eyeLooks = [6][3]float64{{0, 0, 0}, {0.55, 4.2, -1.2}, {1.15, -4.4, 0.8}, {1.95, 1.2, 3.4}, {2.5, -2, -3.4}, {3.0, 0, 0}}

func eyeLook(t float64) (x, y float64) {
	n := len(eyeLooks)
	i := n - 1
	for i > 0 && t < eyeLooks[i][0] {
		i--
	}
	t0, x1, y1 := eyeLooks[i][0], eyeLooks[i][1], eyeLooks[i][2]
	prev := eyeLooks[(i+n-1)%n]
	u := pixelfx.OutBack(pixelfx.Clamp((t-t0)/0.08), 2.2)
	return pixelfx.Lerp(prev[1], x1, u), pixelfx.Lerp(prev[2], y1, u)
}

// eyeLid is the lids: open at rest, a quick blink before it looks down.
func eyeLid(t float64) float64 {
	if t > 1.74 && t < 1.92 {
		return math.Sin(pi * ((t - 1.74) / 0.18))
	}
	return 0
}

func eyeAt(t, hover float64) (x, y float64) {
	a := (t / eyeD) * tau
	return 1.5 * math.Sin(a), -hover - 3*math.Sin(2*a+0.4)
}

// eyeVeins are branching red threads creeping in from the rim, fixed to the eyeball's surface.
func eyeVeins(seed float64) [7][5][2]float64 {
	var out [7][5][2]float64
	r := pixelfx.RngOf(seed, 77)
	for i := range out {
		a, d := (float64(i)/7)*tau+r.Range(-0.3, 0.3), eyeR-0.5
		out[i][0] = [2]float64{math.Cos(a) * d, math.Sin(a) * d}
		for k := 1; k <= 4; k++ {
			a += r.Range(-0.35, 0.35)
			d -= r.Range(1.2, 2.1)
			out[i][k] = [2]float64{math.Cos(a) * d, math.Sin(a) * d}
		}
	}
	return out
}

func drawEyeGrid(g *grid, C, Fl, I []uint32, t float64, V *[7][5][2]float64) {
	R := eyeR
	g.clear()
	lx, ly := eyeLook(t)
	L := eyeLid(t)
	lit := func(x, y, k float64) bool { return pixelfx.Hypot(-4+(x+4)/k, -4.5+(y+4.5)/k) <= R }
	// the sclera: a wet sphere lit from the top-left, pinker in its shadow
	g.shade(-R, -R, R, R, func(x, y float64, _ *gridOut) uint32 {
		if pixelfx.Hypot(x, y) > R {
			return 0
		}
		switch {
		case !(pixelfx.Hypot(x+2, y+2.2) <= R):
			return C[2]
		case lit(x, y, 0.5):
			return C[5]
		case lit(x, y, 0.8):
			return C[4]
		}
		return C[3]
	})
	// veins ride the eyeball as it turns
	for _, pts := range V {
		var sh [5][2]float64
		for k, q := range pts {
			x, y := q[0], q[1]
			sh[k] = [2]float64{x + lx*0.35*(1-pixelfx.Hypot(x, y)/R), y + ly*0.35*(1-pixelfx.Hypot(x, y)/R)}
		}
		for k := 1; k < len(sh); k++ {
			c := eyeVein
			if k >= 2 {
				c = pixelfx.Mix(eyeVein, C[3], 0.45)
			}
			g.line(sh[k-1][0], sh[k-1][1], sh[k][0], sh[k][1], c)
		}
	}
	// the iris, foreshortened as it turns away, a dark ring, a green that brightens toward the pupil
	fx, fy := 1-0.32*math.Pow(math.Abs(lx)/R, 1.4)*3, 1-0.32*math.Pow(math.Abs(ly)/R, 1.4)*3
	ir, pr := 6.4, 2+0.4*math.Sin((t/eyeD)*tau*2)
	g.shade(lx-ir-1, ly-ir-1, lx+ir+1, ly+ir+1, func(x, y float64, _ *gridOut) uint32 {
		dx, dy := (x-lx)/(ir*fx), (y-ly)/(ir*fy)
		d := pixelfx.Hypot(dx, dy)
		if d > 1 || pixelfx.Hypot(x, y) > R-0.5 {
			return 0
		}
		if pixelfx.Hypot((x-lx)/fx, (y-ly)/fy) < pr {
			return C[0]
		}
		if d > 0.86 {
			return I[0]
		}
		ang := math.Atan2(dy, dx)
		streak := math.Sin(ang*9+d*4) > 0.5
		switch {
		case d < 0.5:
			if streak {
				return I[4]
			}
			return I[3]
		case d < 0.7:
			if streak {
				return I[3]
			}
			return I[2]
		}
		if streak {
			return I[2]
		}
		return I[1]
	})
	// a fixed glint from the light, whichever way it looks
	if L < 0.5 {
		g.put(-4, -5, C[5])
		g.put(-3, -5, C[5])
		g.put(-4, -4, C[5])
		g.put(1, -2, C[5])
	}
	// the lids: fleshy shells over the top and bottom, lit like the ball they wrap, the upper one sweeping down to blink
	up, dn := pixelfx.Lerp(-R+3, R*0.42, L), pixelfx.Lerp(R-2.2, R*0.42, L)
	flit := func(x, y, k float64) bool { return pixelfx.Hypot(-4+(x+4)/k, -5+(y+5)/k) <= R+0.6 }
	g.shade(-R-1, -R-1, R+1, R+1, func(x, y float64, _ *gridOut) uint32 {
		if pixelfx.Hypot(x, y) > R+0.6 {
			return 0
		}
		ue, de := up+1.8*math.Pow(x/R, 2), dn-1.4*math.Pow(x/R, 2)
		var tone uint32
		switch {
		case !(pixelfx.Hypot(x+2, y+2.2) <= R+0.6):
			tone = Fl[1]
		case flit(x, y, 0.55):
			tone = Fl[4]
		case flit(x, y, 0.85):
			tone = Fl[3]
		default:
			tone = Fl[2]
		}
		if y < ue {
			if y > ue-1.2 {
				return Fl[0]
			}
			return tone
		}
		if y > de {
			if y < de+1.1 {
				return Fl[0]
			}
			if tone == Fl[4] {
				return Fl[3]
			}
			return tone
		}
		return 0
	})
	g.outline(C[0], 1)
}

func newEyeball(o *pixelfx.Options) pixelfx.Instance {
	C, Fl, I := pixelfx.Fit(o.Palette(), 6), pixelfx.Fit(o.Ramp("flesh"), 5), pixelfx.Fit(o.Ramp("iris"), 5)
	hover, seed := o.Num("hover"), o.Seed()
	V := eyeVeins(seed)
	g, nerve := newGrid(-18, -18, 36, 38), newGrid(-20, -70, 40, 64)
	R := eyeR
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, eyeD, seed)
			sx, _ := eyeAt(t, hover)
			shadow(p, sx, 0, 10, 1.5, eyeShadowCol, 0.5)
			for x := -26.0; x <= 26; x += 6 {
				p.Glow(sx+x, -1, 18, I[2], 0.15*(1-math.Abs(x)/34))
			}
		},
		DrawFunc: func(p *pixelfx.Painter, time float64) {
			t := shift(time, eyeD, seed)
			sx, sy := eyeAt(t, hover)
			// the optic nerve hangs from the back, trailing the bob a beat behind: one tube, lit down its left side
			ng := nerve.clear()
			for s := 0.0; s <= 20; s += 0.5 {
				bx, by := eyeAt(t-s*0.035, hover)
				sw, r := 2.6*math.Pow(s/20, 1.3)*math.Sin((t/eyeD)*tau*2-s*0.16), pixelfx.Lerp(2, 0.6, s/20)
				cx, cy := bx+1+sw, by+R-2+s
				ng.shade(cx-r-1, cy-1, cx+r+1, cy+1, func(x, y float64, _ *gridOut) uint32 {
					v := (x - cx) / math.Max(0.6, r)
					if math.Abs(v) > 1 || math.Abs(y-cy) > 0.5 {
						return 0
					}
					if v < -0.35 {
						return Fl[4]
					}
					if v < 0.4 {
						return Fl[3]
					}
					return Fl[2]
				})
			}
			ng.outline(Fl[0], 1)
			ng.draw(p, 1, 0, 0)
			drawEyeGrid(g, C, Fl, I, t, &V)
			g.draw(p, 1, sx, sy)
			// the iris glows, an eerie halo round the whole eye, motes circling it
			lx, ly := eyeLook(t)
			snap := math.Inf(-1)
			for _, l := range eyeLooks {
				snap = math.Max(snap, math.Max(0, 1-math.Abs(pixelfx.Wrap(t-l[0]+eyeD/2, eyeD)-eyeD/2)/0.2))
			}
			open := 1 - eyeLid(t)
			p.Glow(sx+lx, sy+ly, 15, I[3], (0.5+0.3*snap)*open)
			p.Glow(sx+lx, sy+ly, 5, I[4], 0.4*open)
			p.Glow(sx, sy, 50, I[1], 0.3)
			// motes of green light wheel round it on a tilted orbit, trailing short streaks
			for i := 0; i < 8; i++ {
				sp := -1.0
				if i%2 != 0 {
					sp = 1
				}
				if i%3 == 0 {
					sp *= 2
				}
				a0, rr := (float64(i)*tau)/8, 19+float64(i%3)*2
				var prev [2]float64
				hasPrev := false
				for k := 5.0; k >= 0; k-- {
					a := (t/eyeD)*tau*sp + a0 - sp*k*0.035
					z := math.Sin(a)
					x, y := sx+math.Cos(a)*rr, sy+z*5-math.Cos(a)*3
					if z < 0 && pixelfx.Hypot(x-sx, y-sy) < R+1 {
						hasPrev = false
						continue
					}
					if hasPrev && k > 0 {
						p.Line(prev[0], prev[1], x, y, I[2], 0.4+(5-k)*0.12)
					}
					if k == 0 {
						c := I[3]
						if z > 0 {
							c = I[4]
						}
						p.Px(x, y, c)
						p.Glow(x, y, 5, I[3], 0.45)
					}
					prev, hasPrev = [2]float64{x, y}, true
				}
			}
		},
	}
}
