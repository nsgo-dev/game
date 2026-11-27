// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/storm.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the branching lightning bolt and driving rain from Sprite Fusion's open-source lightning (https://www.spritefusion.com/pixel-art-vfx/lightning), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

// Storm is driving rain at two depths under a churning cloud deck; a forked bolt strikes in a double flash that lights the sky and the rain, then sheet lightning glows in the clouds. It is registered as "weather/storm".
var Storm = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Storm",
		Desc:     "Driving rain at two depths under a churning cloud deck; a forked bolt strikes in a double flash that lights the sky and the rain, then sheet lightning glows in the clouds.",
		Tags:     []string{"weather", "storm", "rain", "lightning", "thunder", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: stormDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#09080f", "#121120", "#1b1a30", "#262541", "#363a5c", "#4f5d8c", "#7790c6", "#c2d6ff")},
			{Name: "bolt", Default: pixelfx.Palette("#2a1a6e", "#4835c2", "#6a6cff", "#78b8ff", "#c6ebff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newStorm,
}

func init() { pixelfx.Register("weather/storm", Storm) }

const (
	stormDuration = 10
	stormSlant    = 0.42
	stormTLead    = 2.62
	stormT1       = 3.0
	stormT2       = 3.3
	stormTSheet   = 6.4
	stormTFar     = 8.6
	stormBX0      = 62.0
	stormBY0      = -136.0
	stormBX1      = 52.0
	stormBY1      = 3.0
)

var (
	stormF1 = []float64{1, 0.62, 0.95, 0.4, 0.7, 0.22}
	stormF2 = []float64{0.92, 0.5, 0.8, 0.3, 0.12}
)

func stormEnv(t, t0 float64, E []float64) float64 {
	i := int(math.Floor((t-t0)*30 + 1e-3))
	if i >= 0 && i < len(E) {
		return E[i]
	}
	return 0
}

func stormFlashAt(t float64) float64 {
	return math.Max(stormEnv(t, stormT1, stormF1), stormEnv(t, stormT2, stormF2))
}

func stormSheetAt(t float64) float64 {
	a := t - stormTSheet
	if a < 0 || a > 0.9 {
		return 0
	}
	d1 := (a - 0.08) / 0.05
	d2 := (a - 0.3) / 0.09
	p1 := math.Exp(-(d1 * d1))
	p2 := math.Exp(-(d2 * d2))
	tail := 0.0
	if a > 0.3 {
		tail = math.Exp(-(a-0.3)*6) * 0.5
	}
	m := 1.0
	if int(math.Floor(a*30))%3 == 1 {
		m = 0.75
	}
	return math.Min(1, 0.7*p1+p2+tail) * m
}

func stormBlink(a, k float64) float64 {
	if a < 0 || a > 0.3 {
		return 0
	}
	d1 := (a - 0.06) / 0.04
	d2 := (a - 0.18) / 0.05
	return k * (math.Exp(-(d1 * d1)) + 0.65*math.Exp(-(d2*d2)))
}

func stormFarAt(t float64) float64  { return stormBlink(t-stormTFar, 0.45) }
func stormFar2At(t float64) float64 { return stormBlink(t-0.9, 0.3) }
func stormSnap(t float64) float64   { return pixelfx.Round(t*3e4) / 3e4 }

type stormRowSpec struct {
	base, wob float64
	n         int
	rx        [2]float64
	ry, drift float64
	life      [2]float64
	tone      [4]int
	tex       bool
	haze      bool
}

var stormRows = [3]stormRowSpec{
	{base: -161, wob: 3, n: 18, rx: [2]float64{16, 30}, ry: 0.8, drift: 15, life: [2]float64{4, 7}, tone: [4]int{1, 2, 3, 4}, tex: true},
	{base: -150, wob: 3, n: 22, rx: [2]float64{10, 20}, ry: 0.85, drift: 10, life: [2]float64{3.5, 6.5}, tone: [4]int{1, 2, 3, 5}, tex: true},
	{base: -140, wob: 3, n: 26, rx: [2]float64{7, 14}, ry: 0.9, drift: 6, life: [2]float64{3, 6}, tone: [4]int{2, 2, 3, 5}, tex: false, haze: true},
}

func stormSag(x float64) float64 {
	d := (x - 40) / 95
	return 9*math.Exp(-(d*d)) - 3
}

type stormRoll struct {
	x, h, rx float64
}

type stormLightSrc struct {
	sx, sy, R, k float64
}

type stormInst struct {
	S            float64
	C, V         []uint32
	FVeil, FDeck *noiseField
	FFloor, FM   *noiseField
}

func newStorm(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	return &stormInst{
		S:      S,
		C:      pixelfx.Fit(o.Palette(), 8),
		V:      pixelfx.Fit(o.Ramp("bolt"), 6),
		FVeil:  field(S+11, 256, 128, cells4_8_16),
		FDeck:  field(S*3+1, 256, 128, nil),
		FFloor: field(S+3, 256, 128, nil),
		FM:     field(S+4, 256, 128, cells4_8_16),
	}
}

func (st *stormInst) Update(float64) {}

func (st *stormInst) DrawBack(p *pixelfx.Painter, t float64) {
	t = stormSnap(t)
	q := clip(p, &Storm.Meta)
	B := box(&Storm.Meta)
	f, sh, far := stormFlashAt(t), stormSheetAt(t), stormFarAt(t)
	lead := 0.0
	if t > stormTLead && t < stormT1 {
		lead = 0.25 + 0.2*math.Sin(t*70)
	}
	pre := 0.0
	if t > stormTLead-0.5 && t < stormTLead {
		pre = 0.12 * math.Max(0, math.Sin((t-stormTLead)*41))
	}
	var srcs [4]stormLightSrc
	nsrcs := 0
	if f+lead+pre > 0 {
		srcs[nsrcs] = stormLightSrc{stormBX0, stormBY0 - 6, 150, f + lead*0.5 + pre}
		nsrcs++
	}
	if sh > 0 {
		srcs[nsrcs] = stormLightSrc{-96, -146, 120, sh}
		nsrcs++
	}
	if far > 0 {
		srcs[nsrcs] = stormLightSrc{128, -156, 80, far}
		nsrcs++
	}
	if f2 := stormFar2At(t); f2 > 0 {
		srcs[nsrcs] = stormLightSrc{-40, -160, 70, f2}
		nsrcs++
	}
	litAt := func(x, y float64) float64 {
		L := 0.0
		for i := 0; i < nsrcs; i++ {
			s := &srcs[i]
			d := pixelfx.Hypot(x-s.sx, (y-s.sy)*1.5) / s.R
			if d < 1 {
				L += s.k * (1 - d) * (1 - d)
			}
		}
		return math.Min(1, L)
	}
	st.sky(p, f)
	if sh > 0.05 {
		pass(p, &Storm.Meta, -170, 0, func(x, y float64, out *passOut) {
			d := pixelfx.Hypot(x+96, (y+150)*1.3) / 170
			if d < 1 {
				out.l = st.V[2]
				out.k = sh * 0.3 * (1 - d) * (1 - d)
			}
		})
	}
	st.veil(p, t, f)
	st.rain(q, t, f, false)
	if t > stormTLead && t < stormT2+0.7 {
		st.bolt(q, t)
	}
	st.deck(q, B, t, litAt, nsrcs > 0)
	st.crawler(q, t)
	st.floor(p, q, t, f)
	st.strike(q, t)
}

func (st *stormInst) Draw(p *pixelfx.Painter, t float64) {
	t = stormSnap(t)
	q := clip(p, &Storm.Meta)
	st.rain(q, t, stormFlashAt(t), true)
}

func (st *stormInst) sky(p *pixelfx.Painter, f float64) {
	k := f * 0.36
	if k <= 0.01 {
		return
	}
	c := pixelfx.Mix(st.V[2], st.V[3], 0.6)
	mid := (stormBX0 + stormBX1) / 2
	pass(p, &Storm.Meta, -170, 9, func(x, _ float64, out *passOut) {
		d := math.Abs(x-mid) / 230
		out.l = c
		out.k = k * (0.2 + 0.8*math.Pow(math.Max(0, 1-d), 2.2))
	})
}

func (st *stormInst) rolls(t float64) [3][]stormRoll {
	S := st.S
	var out [3][]stormRoll
	for j, Rw := range stormRows {
		fj := float64(j)
		list := make([]stormRoll, 0, Rw.n)
		for i := 0; i < Rw.n; i++ {
			fi := float64(i)
			L := Rw.life[0] + pixelfx.Rand(S, fj, fi, 1)*(Rw.life[1]-Rw.life[0])
			age := pixelfx.Wrap(t-pixelfx.Rand(S, fj, fi, 2)*stormDuration, stormDuration)
			if age >= L {
				continue
			}
			u := age / L
			g := math.Pow(math.Sin(pi*u), 0.6)
			rx := Rw.rx[0] + pixelfx.Rand(S, fj, fi, 3)*(Rw.rx[1]-Rw.rx[0])
			ry := rx * Rw.ry
			x := -200 + pixelfx.Rand(S, fj, fi, 4)*400 + Rw.drift*(age-L/2)
			list = append(list, stormRoll{
				x:  x,
				h:  ry * g * (0.6 + 0.4*pixelfx.Rand(S, fj, fi, 5)),
				rx: rx * (0.8 + 0.2*g),
			})
		}
		out[j] = list
	}
	return out
}

func (st *stormInst) veil(p *pixelfx.Painter, t, f float64) {
	F, C, V := st.FVeil, st.C, st.V
	shift := (256 * 2 * t) / stormDuration
	glow := pixelfx.Mix(C[3], V[3], 0.5)
	pass(p, &Storm.Meta, -170, -0.5, func(x, y float64, out *passOut) {
		n := sample(F, (x-y*stormSlant)*1.9-shift, y*0.16+7)
		dens := pixelfx.Clamp(0.45 + (n-0.5)*2.6)
		if f > 0.05 {
			out.c = pixelfx.Mix(C[1], glow, math.Min(1, f*dens*0.7*math.Max(0.25, 1-math.Abs(x-stormBX1)/220)))
		} else {
			out.c = C[1]
		}
		out.a = 0.4 + 0.38*dens
		out.b = true
	})
}

func (st *stormInst) deck(q *clipper, B rect, t float64, litAt func(x, y float64) float64, lit bool) {
	R := st.rolls(t)
	F, C, V := st.FDeck, st.C, st.V
	drift := (256 * t) / stormDuration
	const n = len(stormRows)
	cs := math.Cos((pi * t) / stormDuration)
	mo := cs * cs
	RAMP := [9]uint32{C[0], C[1], C[2], C[3], C[4], C[5], C[6], V[4], pixelfx.White}
	var top, bot, bulge [n]float32
	for x := B.x0; x < B.x1; x++ {
		cx := float64(x) + 0.5
		low := -170.0
		for j := 0; j < n; j++ {
			Rw := stormRows[j]
			fj := float64(j)
			und := (sample(F, cx*0.55+fj*91, 3)*mo + sample(F, cx*0.55+fj*91, 60)*(1-mo) - 0.5) * 2
			c := Rw.base + stormSag(cx)*(0.4+fj*0.3) + 7*und + Rw.wob*2*(sample(F, cx*1.8+fj*61-drift*(0.5-fj*0.1), 20+fj*17)-0.5)
			bump := 0.0
			for _, b := range R[j] {
				dx := (cx - b.x) / b.rx
				if dx*dx < 1 {
					bump = math.Max(bump, b.h*math.Sqrt(1-dx*dx))
				}
			}
			y1 := c + bump
			top[j] = -999
			bot[j] = float32(y1)
			bulge[j] = float32(bump)
			low = math.Max(low, y1)
		}
		prev, prevBot := -1, -999.0
		for y := B.y0; float64(y) < low; y++ {
			cy := float64(y) + 0.5
			j := 0
			for j < n && !(cy >= float64(top[j]) && cy < float64(bot[j])) {
				j++
			}
			if j >= n {
				prev = -1
				continue
			}
			if j != prev {
				if prev >= 0 && prev < j {
					prevBot = cy
				}
				prev = j
			}
			Rw := stormRows[j]
			d := float64(bot[j]) - cy
			tone := Rw.tone
			th := bayer(float64(x), float64(y))
			bg := float64(bulge[j])
			var i int
			switch {
			case d < 1:
				if bg > 3 {
					i = tone[3]
				} else {
					i = tone[3] - 1
				}
			case cy-prevBot < 1.5 && j > 0:
				i = max(0, tone[0]-1)
			case d < 2+bg*0.25+th*2:
				i = tone[2]
			case d < 6+bg*0.4+th*3:
				i = tone[1]
			default:
				fj := float64(j)
				nn := sample(F, cx*0.9+fj*77-drift*(0.6-fj*0.12), cy*1.7+fj*31)
				i = tone[0]
				if Rw.tex && nn > 0.6 {
					i++
				}
			}
			lk := 0.0
			if lit {
				Lv := litAt(cx, cy)
				if Lv > 0.02 {
					mult := 3.1
					if d < 1 {
						mult = 4.4
					} else if d < 4 {
						mult = 3.8
					}
					i = min(8, i+int(pixelfx.Round(Lv*mult)))
					lk = Lv
				}
			}
			if Rw.haze && lk == 0 && d >= 1 {
				q.blend(float64(x), float64(y), RAMP[i], 0.75)
			} else {
				q.px(float64(x), float64(y), RAMP[max(0, i)])
			}
			if lk > 0.03 {
				lm := 0.22
				if d < 3 {
					lm = 0.4
				}
				q.light(float64(x), float64(y), V[3], lk*lm)
			}
		}
	}
}

func (st *stormInst) rain(q *clipper, t, f float64, near bool) {
	S, C, V := st.S, st.C, st.V
	litAt := func(x float64) int {
		L := f * (0.3 + 0.7*math.Max(0, 1-math.Abs(x-stormBX1)/170))
		if L > 0.45 {
			return 2
		}
		if L > 0.15 {
			return 1
		}
		return 0
	}
	hyp := pixelfx.Hypot(stormSlant, 1)
	ux, uy := stormSlant/hyp, 1/hyp
	if !near {
		BODY := [3]uint32{C[4], C[4], C[5]}
		HEAD := [3]uint32{C[5], C[6], C[6]}
		trips(t, stormDuration, 300, S*7+1, 22, 28, func(u, k float64, m, i int) {
			j := pixelfx.Wrap(k, float64(m))
			fi := float64(i)
			stop := -14 - pixelfx.Rand(S, fi, j, 1)*90
			y := -176 + (stop+176)*u
			x := -170 + pixelfx.Rand(S, fi, j, 2)*360 - (stop-y)*stormSlant
			L := 3 + pixelfx.Rand(S, fi, j, 3)*2
			lit := litAt(x)
			q.line(x-ux*L, y-uy*L, x-ux*L*0.4, y-uy*L*0.4, BODY[lit], 1)
			q.line(x-ux*L*0.4, y-uy*L*0.4, x, y, HEAD[lit], 1)
		})
		return
	}
	BODY := [3]uint32{C[6], C[6], V[4]}
	HEAD := [3]uint32{C[7], pixelfx.White, pixelfx.White}
	TAIL := [3]uint32{C[4], C[5], V[3]}
	COOL := [3]uint32{C[5], C[6], V[4]}
	trips(t, stormDuration, 84, S*7+2, 30, 38, func(u, k float64, m, i int) {
		j := pixelfx.Wrap(k, float64(m))
		fi := float64(i)
		z := 1 + pixelfx.Rand(S, fi, j, 1)*4
		y := -180 + (z+180)*u
		x := -175 + pixelfx.Rand(S, fi, j, 2)*370 - (z-y)*stormSlant
		L := 9 + pixelfx.Rand(S, fi, j, 3)*5
		lit := litAt(x)
		q.line(x-ux*L, y-uy*L, x-ux*L*0.55, y-uy*L*0.55, TAIL[lit], 1)
		q.line(x-ux*L*0.55, y-uy*L*0.55, x-ux, y-uy, BODY[lit], 1)
		q.px(x, y, HEAD[lit])
		dur := stormDuration / float64(m)
		age := u * dur
		if age < 0.4 {
			jp := pixelfx.Wrap(k-1, float64(m))
			zp := 1 + pixelfx.Rand(S, fi, jp, 1)*4
			xp := -175 + pixelfx.Rand(S, fi, jp, 2)*370
			litp := litAt(xp)
			hot := C[7]
			if litp != 0 {
				hot = pixelfx.White
			}
			splash(q, xp, zp, age, 0.25+(zp-1)/5.3, splashColors{hot: hot, cool: COOL[litp]}, pixelfx.Rand(S, fi, jp, 4))
		}
	})
}

func stormChannel(S float64) []boltPath {
	R := pixelfx.NewRng(S*31 + 1)
	P := tree(R, stormBX0, stormBY0, stormBX1, stormBY1, 0.26, 0.18, 1, 0)
	pts := P[0].pts
	side := R.Sign()
	for i, u := range [3]float64{0.2, 0.38, 0.55} {
		k := int(math.Floor(u * float64(len(pts)-1)))
		ax, ay := pts[k][0], pts[k][1]
		k2 := min(len(pts)-1, k+3)
		bx, by := pts[k2][0], pts[k2][1]
		sgn := side
		if i%2 != 0 {
			sgn = -side
		}
		da := R.Range(0.6, 1.0)
		a := math.Atan2(by-ay, bx-ax) + sgn*da
		l := R.Range(36, 56) * (1 - u*0.45)
		P = append(P, tree(R, ax, ay, ax+math.Cos(a)*l, ay+math.Sin(a)*l, 0.3, 0.5, 0.6, 1)...)
	}
	return P
}

func (st *stormInst) bolt(q *clipper, t float64) {
	S, V := st.S, st.V
	P1 := stormChannel(S)
	if t < stormT1 {
		reach := stormBY0 + (stormBY1-stormBY0)*math.Pow(math.Floor(((t-stormTLead)/(stormT1-stormTLead))*9)/9, 1.25)
		for _, bp := range P1 {
			col := V[1]
			if bp.w >= 1 {
				col = V[2]
			}
			for i := 0; i < len(bp.pts)-1; i++ {
				ax, ay := bp.pts[i][0], bp.pts[i][1]
				bx, by := bp.pts[i+1][0], bp.pts[i+1][1]
				if by > reach {
					break
				}
				q.line(ax, ay, bx, by, col, 1)
				if i%4 == 0 {
					q.glow(ax, ay, 6, V[3], 0.22*bp.w)
				}
			}
		}
		return
	}
	t2 := tree(pixelfx.NewRng(S*31+2), stormBX0, stormBY0, stormBX1, stormBY1, 0.3, 0.3, 1, 0)
	P2 := make([]boltPath, 0, 3)
	P2 = append(P2, P1[0])
	for i := 1; i < len(t2) && i-1 < 2; i++ {
		P2 = append(P2, t2[i])
	}
	s := 1
	t0, E, paths := stormT1, stormF1, P1
	if t >= stormT2 {
		s = 2
		t0, E, paths = stormT2, stormF2, P2
	}
	_ = s
	a := t - t0
	k := stormEnv(t, t0, E)
	if k > 0 {
		stormStroke(q, paths, k, V)
	} else if a < 0.5 {
		g := 1 - a/0.5
		for _, bp := range paths {
			if bp.w >= 1 {
				for _, pt := range bp.pts {
					q.dot(pt[0], pt[1], V[1], g*0.9)
				}
			}
		}
	}
}

func (st *stormInst) crawler(q *clipper, t float64) {
	S, V := st.S, st.V
	a := t - stormTSheet - 0.22
	if a < 0 || a > 0.17 {
		return
	}
	P := tree(pixelfx.NewRng(S*7+3), -150, -138, -26, -134, 0.2, 0.7, 1, 0)
	reach := -150 + 130*math.Min(1, (math.Floor(a*30)+1)/3)
	k := 0.5
	if a < 0.1 {
		k = 1
	}
	for _, bp := range P {
		col := V[3]
		if bp.w >= 1 && k > 0.6 {
			col = V[4]
		}
		for i := 0; i < len(bp.pts)-1; i++ {
			ax, ay := bp.pts[i][0], bp.pts[i][1]
			bx, by := bp.pts[i+1][0], bp.pts[i+1][1]
			if ax > reach {
				break
			}
			q.line(ax, ay, bx, by, col, 1)
			if bp.w >= 1 {
				q.dot(ax, ay+1, V[2], k)
			}
			if i%3 == 0 {
				q.glow(ax, ay, 10, V[3], 0.35*k*bp.w)
			}
		}
	}
}

func stormStroke(q *clipper, paths []boltPath, k float64, V []uint32) {
	for passIdx := 0; passIdx < 3; passIdx++ {
		for _, bp := range paths {
			trunk := bp.w >= 1
			if !trunk && (k < 0.4 || (passIdx == 0 && bp.w < 0.5)) {
				continue
			}
			for i := 0; i < len(bp.pts)-1; i++ {
				ax, ay := bp.pts[i][0], bp.pts[i][1]
				bx, by := bp.pts[i+1][0], bp.pts[i+1][1]
				l := math.Max(1, pixelfx.Hypot(bx-ax, by-ay))
				for d := 0.0; d <= l; d += 0.5 {
					x := ax + ((bx-ax)*d)/l
					y := ay + ((by-ay)*d)/l
					switch passIdx {
					case 0:
						r := 1.2
						if trunk {
							if k > 0.6 {
								r = 2.3
							} else {
								r = 1.7
							}
						}
						col := V[1]
						if k > 0.6 {
							col = V[2]
						}
						q.circle(x, y, r, col, 1)
					case 1:
						r, col := 0.6, V[3]
						if trunk {
							col = V[4]
							if k > 0.6 {
								r = 1.4
							} else {
								r = 0.9
							}
						}
						q.circle(x, y, r, col, 1)
					default:
						if trunk {
							col := V[4]
							if k > 0.3 {
								col = pixelfx.White
							}
							q.px(x, y, col)
							if k > 0.85 {
								q.px(x+1, y, pixelfx.White)
							}
						} else if bp.w > 0.4 && k > 0.6 {
							q.px(x, y, V[4])
						}
					}
				}
				if passIdx == 0 && i%3 == 0 {
					gr, gk := 8.0, 0.3
					if trunk {
						gr, gk = 18, 0.5
					}
					q.glow(ax, ay, gr, V[3], gk*k*bp.w)
				}
			}
		}
	}
}

func (st *stormInst) strike(q *clipper, t float64) {
	S, C, V := st.S, st.C, st.V
	a := t - stormT1
	if a < 0 || a > 1.8 {
		return
	}
	k := stormFlashAt(t)
	if k > 0 {
		fr := math.Floor(t*30 + 1e-3)
		Rc := pixelfx.NewRng(S*977 + fr)
		for i := 0; i < 4; i++ {
			side := -1.0
			if i%2 != 0 {
				side = 1
			}
			l := Rc.Range(10, 30) * (0.6 + 0.4*k)
			ey := stormBY1 + Rc.Range(-2, 3)
			for _, bp := range tree(Rc, stormBX1+side, stormBY1-1, stormBX1+side*l, ey, 0.35, 0.5, 0.6, 1) {
				col := V[3]
				if k > 0.6 && bp.w > 0.5 {
					col = V[4]
				}
				q.polyline(bp.pts, col, 1)
				mid := bp.pts[len(bp.pts)>>1]
				q.glow(mid[0], mid[1], 8, V[3], 0.3*k)
			}
		}
		q.circle(stormBX1, stormBY1-1, 2+3*k, V[4], 1)
		q.circle(stormBX1, stormBY1-1, 1+2*k, pixelfx.White, 1)
		q.glow(stormBX1, stormBY1-12, 95, V[2], 0.55*k)
		q.glow(stormBX1, stormBY1-4, 40, V[3], 0.5*k)
		q.glow(stormBX1, stormBY1-2, 14, pixelfx.White, 0.6*k)
	}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{
		Rng:    pixelfx.NewRng(S*13 + 5),
		Ground: func(float64) float64 { return 4 },
		Sparks: []uint32{pixelfx.White, V[4], V[3], V[2]},
		Haze:   V[2],
	})
	R := ps.R
	steam := pixelfx.Mix(C[4], V[3], 0.3)
	burst := func(n int, pow float64) {
		for i := 0; i < n; i++ {
			an := -pi/2 + R.Range(-1.35, 1.35)
			sp := R.Range(110, 420) * pow
			life := R.Range(0.18, 0.6)
			ps.Spawn(pixelfx.Spark, stormBX1, stormBY1-1, math.Cos(an)*sp, math.Sin(an)*sp, life, pixelfx.White, 1, 0, 0)
		}
		for i := 0.0; i < 6*pow; i++ {
			sd := R.Sign()
			px := stormBX1 + sd*R.Range(2, 8)
			py := stormBY1 - R.Range(1, 3)
			vx := sd * R.Range(30, 90)
			vy := -R.Range(4, 16)
			life := R.Range(0.6, 1.2)
			size := R.Range(2.5, 4.5)
			delay := R.Range(0, 0.08)
			ps.Spawn(pixelfx.Vapour, px, py, vx, vy, life, steam, size, delay, 0)
		}
	}
	burst(48, 1)
	second := false
	const dt = 1.0 / 120
	for tt := 0.0; tt+dt <= a; tt += dt {
		if !second && tt >= stormT2-stormT1 {
			second = true
			burst(16, 0.7)
		}
		ps.Update(dt)
	}
	drawStormParticles(q, ps)
	if a < 1.6 {
		f := 1 - a/1.6
		q.glow(stormBX1, stormBY1, 16, V[2], 0.4*f*f)
	}
}

// drawStormParticles renders Spark and Vapour particles through the clipped painter so their lines, discs and glows match weather/_lib.js clip().
func drawStormParticles(q *clipper, ps *pixelfx.Particles) {
	for i := range ps.List {
		pt := &ps.List[i]
		if pt.Delay > 0 {
			continue
		}
		k, x, y := pt.Life/pt.Max, pt.X, pt.Y
		switch pt.K {
		case pixelfx.Spark:
			sp := pixelfx.Hypot(pt.VX, pt.VY)
			if sp == 0 {
				sp = 1
			}
			ux, uy := pt.VX/sp, pt.VY/sp
			S := ps.Sparks
			head := S[2]
			if k > 0.6 {
				head = S[0]
			} else if k > 0.3 {
				head = S[1]
			}
			ln := math.Min(9, 1+sp*0.022)
			tail := S[len(S)-1]
			if k > 0.5 {
				tail = head
			}
			q.line(x-ux*ln, y-uy*ln, x-ux*ln*0.4, y-uy*ln*0.4, tail, 1)
			q.line(x-ux*ln*0.45, y-uy*ln*0.45, x, y, head, 1)
			q.glow(x, y, 3, head, 0.45*k)
		case pixelfx.Vapour:
			r := pt.Size * (0.6 + (1-k)*0.9)
			q.disc(x, y, r, pt.Col, math.Pow(k, 1.3)*0.45)
			if ps.Haze != 0 {
				q.glow(x, y, r*1.8, ps.Haze, 0.16*k)
			}
		}
	}
}

func (st *stormInst) floor(p *pixelfx.Painter, q *clipper, t, f float64) {
	F, M, C, V := st.FFloor, st.FM, st.C, st.V
	wind := (256 * 3 * t) / stormDuration
	pass(p, &Storm.Meta, -14, -0.5, func(x, y float64, out *passOut) {
		m := sample(M, x*1.1-wind, y*4+9) - 0.4
		if m <= 0 {
			return
		}
		if f > 0.1 {
			out.c = pixelfx.Mix(C[4], V[3], f)
		} else {
			out.c = C[4]
		}
		out.a = math.Min(0.3, m*0.9) * (1 + y/14)
		out.b = true
	})
	pass(p, &Storm.Meta, 0, 9, func(x, y float64, out *passOut) {
		pool := func(yy float64) bool { return sample(F, x*1.3+31, yy*9) > 0.6-yy*0.006 }
		wet := pool(y)
		lip := wet && !pool(y-1)
		out.b = true
		out.c = C[1]
		out.a = 0.5
		if y < 1 {
			if sample(F, x*2, 50) > 0.5 {
				out.c = C[4]
			} else {
				out.c = C[3]
			}
			out.a = 1
			out.b = false
		} else if wet {
			d := math.Abs(x-stormBX1) / 150
			if f > 0.06 {
				if (d < 0.25 && f > 0.5) || lip {
					out.c = V[4]
				} else {
					out.c = V[3]
				}
				lm := 1.0
				if lip {
					lm = 1.4
				}
				out.a = pixelfx.Clamp(f * (1.35 - d) * lm)
				out.b = false
			} else {
				if lip {
					out.c = C[5]
					out.a = 1
				} else {
					out.c = C[3]
					out.a = 0.7
				}
			}
			out.l = V[3]
			out.k = f * 0.25 * math.Max(0, 1-d)
		}
	})
	if f > 0.2 {
		fr := math.Floor(t * 30)
		for y := 1; y < 10; y++ {
			fy := float64(y)
			if pixelfx.Rand(fy, fr, 3) < 0.8 {
				col := V[4]
				if y < 4 {
					col = pixelfx.White
				}
				q.dot(stormBX1+math.Sin(fy*1.7+t*40)*1.2, fy, col, f*(1.1-fy/12))
			}
		}
	}
}
