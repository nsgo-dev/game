// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/holy/resurrection.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the lit floor line from Sprite Fusion's open-source heal (https://www.spritefusion.com/pixel-art-vfx/heal), MIT License, Copyright (c) 2026 Hugo Duprez.

package holy

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

var (
	resDown  = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(8), pixelfx.Lean.Is(6), pixelfx.HY.Is(2), pixelfx.FX.Is(-4), pixelfx.FY.Is(-10), pixelfx.BX.Is(9), pixelfx.BY.Is(-10), pixelfx.FFX.Is(8), pixelfx.FBX.Is(-7))
	resRisen = pixelfx.Pose{}.With(pixelfx.Turn.Is(1.15), pixelfx.Crouch.Is(-1), pixelfx.FX.Is(-15), pixelfx.FY.Is(-33), pixelfx.BX.Is(16), pixelfx.BY.Is(-33), pixelfx.HY.Is(-1), pixelfx.FFX.Is(-3), pixelfx.FFY.Is(-5), pixelfx.FBX.Is(4), pixelfx.FBY.Is(-5))
	resKneel = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-17), pixelfx.BX.Is(9), pixelfx.BY.Is(-17))
)

// Resurrection is light pouring down on the fallen while life climbs back up the body; wings of light unfold, a halo forms, one great wingbeat, then the wings scatter into feathers. It is registered as "holy/resurrection".
var Resurrection = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Resurrection",
		Desc:     "Light pours down on the fallen and life climbs back up the body; wings of light unfold, a halo forms, one great wingbeat, then the wings scatter into feathers.",
		Tags:     []string{"holy", "revive", "resurrect", "wings", "halo", "light", "character", "one-shot"},
		Size:     [2]int{192, 160},
		Origin:   [2]int{96, 140},
		Duration: 4.8,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: goldPalette()},
			{Name: "ash", Default: ashPalette()},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Follow: pixelfx.Val(0.6),
		Poses: []pixelfx.Key{
			{T: 0, Pose: resDown},
			{T: 0.5, Pose: resDown},
			{T: 0.85, Pose: resDown.With(pixelfx.Crouch.Is(6), pixelfx.Lean.Is(4), pixelfx.HY.Is(1))},
			{T: 1.15, Pose: resKneel},
			{T: 1.5, Pose: resRisen.With(pixelfx.Y.Is(-2))},
			{T: 1.95, Pose: resRisen.With(pixelfx.Y.Is(-3))},
			{T: 2.1, Pose: resRisen.With(pixelfx.Y.Is(-2), pixelfx.Crouch.Is(1), pixelfx.FY.Is(-37), pixelfx.BY.Is(-37))},
			{T: 2.3, Pose: resRisen.With(pixelfx.Y.Is(-9), pixelfx.FX.Is(-15), pixelfx.FY.Is(-38), pixelfx.BX.Is(16), pixelfx.BY.Is(-38))},
			{T: 2.95, Pose: resRisen.With(pixelfx.Y.Is(-6))},
			{T: 3.8, Pose: resKneel.With(pixelfx.Y.Is(0))},
			{T: 4.3, Pose: pixelfx.Ready},
			{T: 4.8, Pose: pixelfx.Ready},
		},
		Dust: []pixelfx.DustKick{{T: 3.8, X: 0, N: 5}},
	},
	New: newResurrection,
}

func init() { pixelfx.Register("holy/resurrection", Resurrection) }

var (
	resTop = -140.0
	// the story: the light pours, life climbs the body, wings and halo, the beat, the wings come apart
	resPour, resCL0, resCL1, resW0, resH0, resH1 = 0.34, 0.5, 1.15, 1.1, 1.24, 1.5
	resB0, resB1, resB2, resB3, resSC            = 1.95, 2.25, 2.37, 2.8, 2.95
	resHX, resHY, resHRX, resHRY                 = 0.0, -47.0, 9.5, 2.6
)

// The right wing (the left mirrors it), from its shoulder: the arm folded and spread, then each feather.
var (
	resShoulder = [2]float64{4, -20}
	resArmOpen  = [4][2]float64{{0, 0}, {7, -9}, {15, -19}, {20, -27}}
	resArmFold  = [4][2]float64{{0, 0}, {2, -2}, {3, -3}, {4, -3}}
	// resQuills are [where along the arm 0..1, angle spread, length, width, kind]: kind 0 a primary, 1 a secondary, 2 a covert.
	resQuills = [][5]float64{
		{0.14, 1.35, 11, 2.6, 1}, {0.26, 1.12, 14, 2.8, 1}, {0.38, 0.88, 17, 3, 1}, {0.5, 0.62, 19, 3.1, 1}, {0.62, 0.35, 21, 3.2, 1}, {0.72, 0.08, 23, 3.3, 1},
		{0.8, -0.2, 26, 3.4, 0}, {0.85, -0.45, 29, 3.5, 0}, {0.9, -0.72, 31, 3.6, 0}, {0.95, -1, 30, 3.6, 0}, {1, -1.3, 26, 3.4, 0},
		{0.22, 1.2, 7, 2.2, 2}, {0.34, 0.98, 8, 2.3, 2}, {0.46, 0.75, 9, 2.4, 2}, {0.58, 0.5, 10, 2.5, 2}, {0.7, 0.22, 10, 2.5, 2}, {0.81, -0.1, 11, 2.6, 2}, {0.92, -0.5, 11, 2.6, 2},
	}
	resFlex = [3]float64{1, 0.5, 0.3}
)

// resAlong returns the point s (0..1) of the way along the polyline pts.
func resAlong(pts [4][2]float64, s float64) [2]float64 {
	L := 0.0
	var seglen [3]float64
	for i := 1; i < len(pts); i++ {
		seglen[i-1] = pixelfx.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
		L += seglen[i-1]
	}
	d := s * L
	for i := range seglen {
		if d <= seglen[i] || i == len(seglen)-1 {
			f := 0.0
			if seglen[i] != 0 {
				f = pixelfx.Clamp(d / seglen[i])
			}
			return [2]float64{pixelfx.Lerp(pts[i][0], pts[i+1][0], f), pixelfx.Lerp(pts[i][1], pts[i+1][1], f)}
		}
		d -= seglen[i]
	}
	return pts[len(pts)-1]
}

// resBeat is the wings' turn at t: a slow raise, a hard fast downstroke, a recovery that overshoots a touch.
func resBeat(t float64) float64 {
	switch {
	case t < resB0:
		return 0
	case t < resB1:
		return -0.34 * pixelfx.Ease.InOutSine(pixelfx.Seg(t, resB0, resB1))
	case t < resB2:
		return pixelfx.Lerp(-0.34, 0.66, pixelfx.Ease.InQuad(pixelfx.Seg(t, resB1, resB2)))
	}
	return 0.66 * (1 - pixelfx.OutBack(pixelfx.Seg(t, resB2, resB3), 1.3))
}

type resRay struct{ o, w, b, f, ph float64 }

type resMote struct{ q, y, v, amp, ph, f float64 }

// resQuill is one feather of a wing: where along the arm it sits (s), its spread angle, length, width and kind, the angle it lies at folded, when it opens (after the wing starts to), when it comes off each wing (right, left) and whether it has.
type resQuill struct {
	s, ang, len, wid float64
	kind             int
	fold, open       float64
	off              [2]float64
	gone             [2]bool
}

// resFeather is one feather of a wing as posed: its quill's index and quill, its base (x, y), angle, length, width, alpha and whether it is flashing white.
type resFeather struct {
	i        int
	q        *resQuill
	x, y     float64
	ang, len float64
	wid, a   float64
	hot      float64
}

func newResurrection(o *pixelfx.Options) pixelfx.Instance {
	C, AS, A, R, W := pixelfx.Fit(o.Palette(), 8), pixelfx.Fit(o.Ramp("ash"), 6), o.Actor, pixelfx.NewRng(o.Seed()), pixelfx.White
	S := shapeOf(A)
	TOP, POUR, CL0, CL1, W0, H0, H1 := resTop, resPour, resCL0, resCL1, resW0, resH0, resH1
	B1, B2, SC := resB1, resB2, resSC
	HX, HY, HRX, HRY := resHX, resHY, resHRX, resHRY
	WING := []uint32{C[2], C[3], C[4], C[5], C[6], W}
	ps := pixelfx.NewParticles(pixelfx.SimOpts{Rng: R, Ground: func(float64) float64 { return 0 }, Sparks: []uint32{W, C[6], C[5], C[3]}, Haze: C[4]})
	lights, loose := &pixelfx.Lights{}, newFeathers(R, 0)
	rows := edgeRows(S)
	// the shaft of light: a few rays of their own width and shimmer
	rays := make([]resRay, 6)
	for i := range rays {
		o := -0.75 + float64(i)*0.3 + R.Range(-0.08, 0.08)
		w, b, f, ph := R.Range(0.1, 0.2), R.Range(0.55, 1), R.Range(1.5, 3.5), R.Range(0, tau)
		rays[i] = resRay{o, w, b, f, ph}
	}
	cores := slices.Clone(rays)
	slices.SortStableFunc(cores, func(a, b resRay) int { return cmp.Compare(b.b, a.b) })
	cores = cores[:2]
	motes := make([]resMote, 34)
	for i := range motes {
		q, y, v, amp, ph, f := R.Range(-1, 1), R.Range(0, 1), R.Range(4, 11), R.Range(0.6, 2.4), R.Range(0, tau), R.Range(0.6, 1.4)
		motes[i] = resMote{q, y, v, amp, ph, f}
	}
	// each feather opens on its own beat, outer ones last, and comes off at its own time, tips first
	reach := make([]float64, len(resQuills))
	farthest := math.Inf(-1)
	for i, q := range resQuills {
		b := resAlong(resArmOpen, q[0])
		reach[i] = pixelfx.Hypot(b[0]+math.Cos(q[1])*q[2], b[1]+math.Sin(q[1])*q[2])
		farthest = math.Max(farthest, reach[i])
	}
	quills := make([]resQuill, len(resQuills))
	for i, q := range resQuills {
		off := func() float64 { return SC + (1-reach[i]/farthest)*0.2 + R.Range(0, 0.04) }
		s, kind := q[0], int(q[4])
		var open float64
		switch kind {
		case 0:
			open = 0.06 + (s-0.8)*0.45
		case 1:
			open = s * 0.06
		default:
			open = 0.03 + s*0.05
		}
		off0 := off()
		off1 := off()
		quills[i] = resQuill{s: s, ang: q[1], len: q[2], wid: q[3], kind: kind, fold: 1.45 + float64(i%6)*0.04, open: open, off: [2]float64{off0, off1}}
	}
	// profile is a Float32Array in pixelfx: each value is rounded to float32 as it is stored
	var profile [121]float32
	// prof reads profile at i; out of range it is NaN, as pixelfx reads undefined there
	prof := func(i int) float64 {
		if i < 0 || i >= len(profile) {
			return math.NaN()
		}
		return float64(profile[i])
	}
	t, poured, flashed, formed, beaten := 0.0, false, false, false, false

	climbY := func(tt float64) float64 {
		return pixelfx.Lerp(1, S.top-2, pixelfx.Ease.InOutSine(pixelfx.Seg(tt, CL0, CL1)))
	}
	shaftK := func(tt float64) float64 { return pixelfx.Smooth(0, 0.12, tt) * (1 - pixelfx.Smooth(3.0, 3.75, tt)) }
	shaftW := func(tt float64) float64 {
		return pixelfx.Lerp(0.4, 1, pixelfx.Ease.OutCubic(pixelfx.Seg(tt, 0, 0.6))) * pixelfx.Lerp(1, 0.12, pixelfx.Ease.InCubic(pixelfx.Seg(tt, 2.9, 3.6)))
	}
	halfW := func(y, tt float64) float64 { return pixelfx.Lerp(8, 25, (y-TOP)/-TOP) * shaftW(tt) }

	// wing returns the pose of every feather of one wing at time tt (quill base, angle, length, width, alpha, hot) and its arm
	wing := func(tt, side float64) ([]resFeather, [4][2]float64) {
		oa, rot := pixelfx.OutBack(pixelfx.Seg(tt, W0, W0+0.4), 1.6), resBeat(tt)
		om := (resBeat(tt) - resBeat(tt-0.03)) / 0.03
		var arm [4][2]float64
		for i, f := range resArmFold {
			arm[i] = [2]float64{pixelfx.Lerp(f[0], resArmOpen[i][0], oa), pixelfx.Lerp(f[1], resArmOpen[i][1], oa)}
		}
		cr, sr := math.Cos(rot), math.Sin(rot)
		place := func(pt [2]float64) [2]float64 {
			x, y := pt[0], pt[1]
			return [2]float64{side * (resShoulder[0] + x*cr - y*sr), resShoulder[1] + x*sr + y*cr}
		}
		a0 := pixelfx.Smooth(W0, W0+0.1, tt)
		si := 1
		if side > 0 {
			si = 0
		}
		var out []resFeather
		for i := range quills {
			q := &quills[i]
			if tt >= q.off[si]+0.05 {
				continue
			}
			of := pixelfx.OutBack(pixelfx.Seg(tt, W0+q.open, W0+q.open+0.42), 2)
			b := place(resAlong(arm, q.s))
			ang := pixelfx.Lerp(q.fold, q.ang, of) + rot - pixelfx.ClampTo(om*0.045*resFlex[q.kind], -0.5, 0.5)
			if side <= 0 {
				ang = pi - ang
			}
			hot := 0.0
			if tt < W0+q.open+0.07 || tt >= q.off[si] {
				hot = 1
			}
			out = append(out, resFeather{i: i, q: q, x: b[0], y: b[1], ang: ang, len: q.len * pixelfx.Lerp(0.3, 1, of), wid: q.wid, a: a0, hot: hot})
		}
		for i := range arm {
			arm[i] = place(arm[i])
		}
		return out, arm
	}
	kindOf := func(fs []resFeather, kind int) []resFeather {
		var out []resFeather
		for _, f := range fs {
			if f.q.kind == kind {
				out = append(out, f)
			}
		}
		return out
	}

	drawWing := func(p *pixelfx.Painter, side float64) {
		out, arm := wing(t, side)
		if len(out) == 0 {
			return
		}
		order := slices.Clone(out)
		slices.SortStableFunc(order, func(a, b resFeather) int { return cmp.Compare(b.q.ang, a.q.ang) })
		// the lowest feathers first, so each one above lays its warm trailing edge over the next; coverts on top
		for _, f := range order {
			if f.q.kind != 2 {
				feather(p, f.x, f.y, f.ang, f.len, f.wid, WING, f.a, side, f.hot, 0.02)
			}
		}
		for _, f := range order {
			if f.q.kind == 2 {
				feather(p, f.x, f.y, f.ang, f.len, f.wid, WING, f.a, side, f.hot, 0.02)
			}
		}
		// the leading edge: a white bone of light with a gold underside
		k := float64(len(out)) / float64(len(quills))
		if k > 0.5 {
			under := make([][2]float64, len(arm))
			for i, pt := range arm {
				under[i] = [2]float64{pt[0], pt[1] + 1}
			}
			p.Polyline(under, C[5], out[0].a)
			p.Polyline(arm[:], W, out[0].a)
		}
		p.Glow(side*28, -36, 38, C[4], 0.26*out[0].a*k)
		g := math.Mod((t-W0-0.45)*1.3, 1)
		if k > 0.9 && t > W0+0.45 && g < 0.8 {
			gp := resAlong(arm, g/0.8)
			pixelfx.Sparkle(p, gp[0], gp[1], 2+math.Sin((g/0.8)*pi)*2, W, C[6], 0.4)
		}
		p.GlowLine(arm[0][0], arm[0][1], arm[3][0], arm[3][1], 7, C[5], 0.25*out[0].a*k)
		// the tips of the primaries leave trails of light as they come down
		if t > B1+0.03 && t < B2+0.14 {
			for _, i := range [3]int{6, 8, 10} {
				var pts [][2]float64
				for j := range 10 {
					tt := math.Max(B1+0.01, t-float64(j)*0.012)
					fs, _ := wing(tt, side)
					for _, w := range fs {
						if w.i == i {
							pts = append(pts, [2]float64{w.x + math.Cos(w.ang)*(w.len-1), w.y + math.Sin(w.ang)*(w.len-1)})
							break
						}
					}
				}
				fade := 1 - pixelfx.Smooth(B2, B2+0.14, t)
				for j := 1; j < len(pts); j++ {
					c := C[4]
					if j < 3 {
						c = W
					} else if j < 6 {
						c = C[6]
					}
					p.Line(pts[j-1][0], pts[j-1][1], pts[j][0], pts[j][1], c, fade*(1.1-float64(j)/10))
				}
			}
		}
	}

	shaft := func(p *pixelfx.Painter) {
		k := shaftK(t)
		if k <= 0 {
			return
		}
		for i := range profile {
			q := float64(i-60) / 40
			I := 0.0
			for _, r := range rays {
				d := (q - r.o) / r.w
				I += r.b * math.Exp(-d*d) * (0.72 + 0.28*math.Sin(t*r.f+r.ph))
			}
			profile[i] = float32((I*0.6 + 0.35*math.Max(0, 1-q*q)) * pixelfx.Smooth(1.25, 0.7, math.Abs(q)))
		}
		front := pixelfx.Lerp(TOP, 1, pixelfx.Ease.InQuad(pixelfx.Seg(t, 0, POUR)))
		w0 := halfW(0, t) * 1.3
		p.Each(-w0-1, TOP, w0+1, math.Min(front, 0.5), func(x, y float64, sx, sy int) {
			w := halfW(y, t)
			q := x / w
			if q < -1.5 || q > 1.5 {
				return
			}
			I := prof(int(pixelfx.Round(q*40))+60) * k * (0.75 + 0.25*pixelfx.Smooth(TOP, 0, y)) * pixelfx.Smooth(front, front-10, y)
			if I <= 0.01 {
				return
			}
			p.BlendPx(sx, sy, C[6], I*0.24)
			p.Add(sx, sy, C[4], I*0.3)
		})
		// the brightest rays show as fine lines, light running down them
		for _, r := range cores {
			sh := 0.75 + 0.25*math.Sin(t*r.f+r.ph)
			for y := TOP; y < math.Min(front, 0); y++ {
				pulse := 0.5 + 0.5*math.Sin(y*0.13-t*11+r.ph)
				c := C[6]
				if pulse > 0.8 {
					c = W
				}
				p.Dot(r.o*halfW(y, t), y, c, k*sh*(0.15+0.55*pulse)*pixelfx.Smooth(front, front-10, y))
			}
		}
		// the head of the pour
		if t < POUR+0.05 && front > TOP+14 {
			fy := math.Min(front, 0)
			p.Glow(0, fy, 14, C[5], 0.7*k)
			for dy := -6.0; dy <= 0; dy++ {
				c := C[6]
				if dy > -3 {
					c = W
				}
				p.Dot(0, fy+dy, c, 1+dy/7)
			}
		}
		// dust motes lit where they cross a ray
		for _, m := range motes {
			y := TOP + math.Mod(math.Mod(m.y*-TOP+t*m.v, -TOP)+-TOP, -TOP)
			if y > front {
				continue
			}
			w := halfW(y, t)
			x := m.q*w + math.Sin(t*m.f+m.ph)*m.amp
			I := prof(int(pixelfx.Round((x/w)*40))+60) * k
			if I < 0.25 {
				continue
			}
			c := C[5]
			if I > 0.75 {
				c = W
			} else if I > 0.5 {
				c = C[6]
			}
			p.Dot(x, y, c, math.Min(1, I*1.4))
			p.Light(x, y, C[5], I*0.5)
		}
	}

	pool := func(p *pixelfx.Painter, back bool) {
		k, w := shaftK(t)*pixelfx.Smooth(POUR-0.05, POUR+0.1, t), halfW(0, t)*1.25
		if k <= 0 {
			return
		}
		if back {
			p.Glow(0, -2, w*1.6, C[4], 0.3*k)
			return
		}
		// the floor catches the light: a bright line, and a pool fading into the stone in front
		for y := 0; y <= 3; y++ {
			for x := math.Floor(-w); x <= w; x++ {
				u := pixelfx.Hypot(x/w, float64(y)/4)
				if u >= 1 {
					continue
				}
				a := k * ((1 - u) * 0.9)
				if y == 0 {
					a = k * (math.Sqrt(1-u) * 1.2)
				}
				var c uint32
				switch {
				case u < 0.3 && y != 0:
					c = C[5]
				case u < 0.3:
					c = W
				case u < 0.6:
					c = C[5]
				case u < 0.85:
					c = C[4]
				default:
					c = C[3]
				}
				p.Dot(x, float64(y), c, a)
			}
		}
	}

	halo := func(p *pixelfx.Painter) {
		form, k := pixelfx.Ease.OutCubic(pixelfx.Seg(t, H0, H1)), 1-pixelfx.Smooth(3.35, 3.9, t)
		if form <= 0 || k <= 0 {
			return
		}
		white, gl := t >= H1 && t < H1+0.06, (t-H1)*4.2+pi/2
		const n = 96
		for j := range n {
			g := (float64(j) / n) * tau
			fromFront := math.Abs(wrapPi(g - pi/2))
			if fromFront > form*pi+0.02 {
				continue
			}
			x, y, isNear := HX+math.Cos(g)*HRX*(0.9+0.1*form), HY+math.Sin(g)*HRY, math.Sin(g) >= 0
			dg := math.Abs(wrapPi(g - gl))
			hot := white || dg < 0.22 || fromFront > form*pi-0.3
			if isNear {
				c1, c2 := C[6], C[4]
				if hot {
					c1, c2 = W, C[6]
				}
				p.Dot(x, y, c1, k)
				p.Dot(x, y+1, c2, k)
			} else {
				c := C[5]
				if hot {
					c = C[6]
				}
				p.Dot(x, y, c, k*0.9)
			}
		}
		p.Glow(HX, HY, 20, C[4], 0.4*k*form)
		p.Glow(HX, HY+1, 9, W, 0.2*k*form)
		gx, gy := HX+math.Cos(gl)*HRX, HY+math.Sin(gl)*HRY
		if t > H1 && math.Sin(gl) > 0.2 {
			pixelfx.Sparkle(p, gx, gy+0.5, 2+2*math.Sin(gl), C[6], C[4], 0.35*k)
		}
		if t >= H1 && t < H1+0.3 {
			pixelfx.Sparkle(p, HX, HY+HRY+0.5, 7*(1-(t-H1)/0.3), W, C[5], 0.6)
		}
	}

	body := func(p *pixelfx.Painter) {
		if t >= CL1+0.6 {
			return
		}
		Ly := climbY(t)
		if t < CL1 {
			recolor(p, A, func(c uint32, _, y float64) uint32 {
				d := y - Ly
				switch {
				case d < -0.5:
					return toneOf(AS, c, 0)
				case d < 0.5:
					return W
				case d < 1.5:
					return C[6]
				}
				if k := math.Exp(-(d-1.5)/4) * 0.7; k > 0.08 {
					return pixelfx.Mix(c, C[5], k)
				}
				return 0
			}, 1)
			// the outline catches the light where the glow has been
			for _, pt := range S.out {
				x, y := pt[0], pt[1]
				d := y - Ly
				if d < -0.5 || d > 12 {
					continue
				}
				c, a := C[4], 1.1-d/12
				if d < 1 {
					c, a = W, 1
				} else if d < 5 {
					c = C[5]
				}
				p.Dot(x, y, c, a)
			}
			if t > CL0 {
				p.Glow(0, Ly, 26, C[4], 0.5)
				p.Glow(0, Ly, 8, W, 0.3)
				p.Glow(0, -18, 36, C[5], 0.16*pixelfx.Seg(t, CL0, CL1))
			}
			return
		}
		// the hero frame: the body flashes white, then cools through gold back to itself
		u := t - CL1
		recolor(p, A, func(c uint32, _, _ float64) uint32 {
			switch {
			case u < 0.06:
				return W
			case u < 0.45:
				return pixelfx.Mix(c, C[5], 0.55*math.Pow(1-u/0.45, 2))
			}
			return 0
		}, 1)
		if u < 0.25 {
			c := C[5]
			if u < 0.08 {
				c = W
			}
			for _, pt := range S.out {
				p.Dot(pt[0], pt[1], c, 1-u/0.25)
			}
		}
	}

	sides := [2]float64{-1, 1}

	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// light streaks pour down the shaft, faster as they fall
			if t > 0.05 && t < 2.9 && R.Chance(0.16) {
				q, y := R.Range(-0.9, 0.9), TOP+R.Range(8, 24)
				v0 := R.Range(50, 90)
				life := math.Log(1+(3*(-y-2))/v0) / 3
				ps.Spawn(pixelfx.Streak, q*halfW(y, t), y, q*2, v0, life, chance(R, 0.5, C[6], C[5]), 1, 0, 0)
			}
			if t > 0.3 && t < 2.8 && R.Chance(0.05) {
				x := R.Range(-1, 1) * halfW(-30, t)
				y, vx, vy, life := -R.Range(4, 90), R.Range(-3, 3), R.Range(-4, 6), R.Range(0.4, 0.8)
				col, size := chance(R, 0.5, C[6], W), R.Range(0.6, 1.8)
				ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
			}
			if !poured && t >= POUR {
				poured = true
				lights.Add(0, -12, 42, C[5], 0.9, 0.4)
				lights.Add(0, -6, 16, W, 0.6, 0.1)
				for range 14 {
					x, y, vx, vy, life := R.Range(-18, 18), -R.Range(0, 6), R.Range(-20, 20), -R.Range(10, 50), R.Range(0.5, 1.1)
					col, size := chance(R, 0.5, W, C[6]), R.Range(0.6, 2.2)
					ps.Spawn(pixelfx.StarKind, x, y, vx, vy, life, col, size, 0, 0)
				}
				for range 10 {
					a, sp := -pi/2+R.Range(-1.2, 1.2), R.Range(60, 150)
					x, life := R.Range(-3, 3), R.Range(0.25, 0.5)
					ps.Spawn(pixelfx.Spark, x, -1, math.Cos(a)*sp, math.Sin(a)*sp, life, W, 1, 0, 0)
				}
			}
			// life climbs the body: bits of light peel off the rim at the front
			if t > CL0 && t < CL1 {
				Ly := pixelfx.Round(climbY(t))
				for _, y := range [2]float64{Ly, Ly + 1} {
					xs, ok := rows[y]
					if !ok || !R.Chance(0.35) {
						continue
					}
					x, _ := pick(R, xs)
					s := pixelfx.Sign(x + 0.5)
					kind := chance(R, 0.6, pixelfx.Mote, pixelfx.StarKind)
					vx, vy, life := s*R.Range(8, 30), -R.Range(15, 45), R.Range(0.35, 0.7)
					col, size := chance(R, 0.5, C[6], C[5]), R.Range(0.5, 1.1)
					ps.Spawn(kind, x+s, y, vx, vy, life, col, size, 0, 0)
				}
			}
			if !flashed && t >= CL1 {
				flashed = true
				lights.Add(0, -24, 64, C[5], 0.7, 0.5)
				lights.Add(0, -30, 28, W, 0.7, 0.1)
				for range 18 {
					a, sp := R.Range(0, tau), R.Range(30, 110)
					life, col, size := R.Range(0.4, 0.9), chance(R, 0.5, W, C[6]), R.Range(0.6, 2)
					ps.Spawn(pixelfx.StarKind, math.Cos(a)*6, -24+math.Sin(a)*12, math.Cos(a)*sp, math.Sin(a)*sp*0.7-20, life, col, size, 0, 0)
				}
			}
			// motes spiral in to make the halo
			if t > H0-0.2 && t < H0+0.1 && R.Chance(0.6) {
				g, D := R.Range(0, tau), R.Range(14, 24)
				x, y := HX+math.Cos(g)*D, HY+math.Sin(g)*D*0.5
				tx, ty := HX+math.Cos(g+0.6)*HRX, HY+math.Sin(g+0.6)*HRY
				T := R.Range(0.16, 0.24)
				l := pixelfx.Hypot(tx-x, ty-y)
				if l == 0 {
					l = 1
				}
				v := (l * 3) / (math.Exp(3*T) - 1)
				ps.Spawn(pixelfx.Streak, x, y, ((tx-x)/l)*v, ((ty-y)/l)*v, T, chance(R, 0.5, C[6], C[5]), 1, 0, 0)
			}
			if !formed && t >= H1 {
				formed = true
				lights.Add(HX, HY, 30, C[6], 0.8, 0.3)
			}
			// the wings burst open with a scatter of light
			if t > W0 && t < W0+0.35 && R.Chance(0.5) {
				for _, side := range sides {
					fs, _ := wing(t, side)
					if f, ok := pick(R, fs); ok {
						vx, vy, life := side*R.Range(5, 25), -R.Range(5, 25), R.Range(0.3, 0.6)
						col, size := chance(R, 0.5, W, C[6]), R.Range(0.6, 1.6)
						ps.Spawn(pixelfx.StarKind, f.x+math.Cos(f.ang)*f.len, f.y+math.Sin(f.ang)*f.len, vx, vy, life, col, size, 0, 0)
					}
				}
			}
			// while the wings hold, light twinkles on the tips and motes lift off them
			if t > W0+0.45 && t < SC {
				for _, side := range sides {
					if R.Chance(0.045) {
						fs, _ := wing(t, side)
						if f, ok := pick(R, kindOf(fs, 0)); ok {
							vy, life, col, size := -R.Range(2, 8), R.Range(0.3, 0.6), chance(R, 0.5, W, C[6]), R.Range(1, 2.4)
							ps.Spawn(pixelfx.StarKind, f.x+math.Cos(f.ang)*(f.len-2), f.y+math.Sin(f.ang)*(f.len-2), 0, vy, life, col, size, 0, 0)
						}
					}
					if R.Chance(0.03) {
						fs, _ := wing(t, side)
						if f, ok := pick(R, fs); ok {
							vx, vy, life := R.Range(-8, 8), -R.Range(8, 25), R.Range(0.6, 1.1)
							ps.Spawn(pixelfx.Ember, f.x+math.Cos(f.ang)*f.len*0.7, f.y+math.Sin(f.ang)*f.len*0.7, vx, vy, life, C[5], 1, 0, 0)
						}
					}
				}
			}
			// the downstroke: a gust along the floor, light flung off the tips, a few feathers shaken loose
			if !beaten && t >= B2-0.03 {
				beaten = true
				for _, side := range sides {
					lights.Add(side*30, -34, 46, C[5], 0.6, 0.4)
					// the gust skims out along the floor as streaks of lit dust
					for range 7 {
						x, y, vx, vy, life := side*R.Range(8, 22), -R.Range(1, 4), side*R.Range(90, 160), -R.Range(0, 8), R.Range(0.25, 0.45)
						col, size := chance(R, 0.5, C[6], C[5]), R.Range(0.6, 1)
						ps.Spawn(pixelfx.Mote, x, y, vx, vy, life, col, size, 0, 0)
					}
					fs, _ := wing(t, side)
					for _, f := range fs {
						if f.q.kind == 0 {
							tx, ty := f.x+math.Cos(f.ang)*f.len, f.y+math.Sin(f.ang)*f.len
							vx, vy, life := side*R.Range(30, 90), R.Range(60, 160), R.Range(0.3, 0.6)
							ps.Spawn(pixelfx.Spark, tx, ty, vx, vy, life, W, 1, 0, 0)
						}
					}
					for range 3 {
						fs, _ := wing(t, side)
						if f, ok := pick(R, kindOf(fs, 1)); ok {
							vx, vy, fl, fw, life := side*R.Range(20, 70), R.Range(10, 50), R.Range(9, 12), R.Range(2.2, 2.8), R.Range(1.2, 1.6)
							loose.add(looseFeather{x: f.x + math.Cos(f.ang)*f.len*0.6, y: f.y + math.Sin(f.ang)*f.len*0.6, vx: vx, vy: vy, a: f.ang, len: fl, wid: fw, life: life, side: side})
						}
					}
				}
			}
			// the wings come apart, tips first: each feather flashes and breaks into small ones and motes
			for _, side := range sides {
				si := 1
				if side > 0 {
					si = 0
				}
				for qi := range quills {
					q := &quills[qi]
					if q.gone[si] || t < q.off[si] {
						continue
					}
					q.gone[si] = true
					fs, _ := wing(q.off[si]-0.001, side)
					var f *resFeather
					for i := range fs {
						if fs[i].q == q {
							f = &fs[i]
							break
						}
					}
					if f == nil {
						continue
					}
					ca, sa := math.Cos(f.ang), math.Sin(f.ang)
					n := 1
					if q.kind == 2 {
						n = 0
					}
					for range n {
						u := 0.55
						x, y := f.x+ca*f.len*u, f.y+sa*f.len*u
						ox, oy := x-side*4, y+20
						l := pixelfx.Hypot(ox, oy)
						if l == 0 {
							l = 1
						}
						sp := R.Range(60, 150)
						vx := (ox/l)*sp + R.Range(-15, 15)
						vy := (oy/l)*sp*0.5 - R.Range(20, 50)
						a, fl, fw, life, back := f.ang+R.Range(-0.4, 0.4), R.Range(9, 12), R.Range(2.2, 2.8), R.Range(1, 1.45), R.Chance(0.35)
						loose.add(looseFeather{x: x, y: y, vx: vx, vy: vy, a: a, len: fl, wid: fw, life: life, side: side, back: back})
					}
					for range 2 {
						x := f.x + ca*f.len*R.Range(0.3, 1)
						y := f.y + sa*f.len*R.Range(0.3, 1)
						vx, vy, life := R.Range(-15, 15), -R.Range(10, 40), R.Range(0.6, 1.1)
						ps.Spawn(pixelfx.Ember, x, y, vx, vy, life, C[5], 1, 0, 0)
					}
					if R.Chance(0.6) {
						vy, life, size := -R.Range(5, 15), R.Range(0.3, 0.6), R.Range(1, 2)
						ps.Spawn(pixelfx.StarKind, f.x+ca*f.len, f.y+sa*f.len, 0, vy, life, W, size, 0, 0)
					}
				}
			}
			// the halo melts into rising motes
			if t > 3.35 && t < 3.75 && R.Chance(0.4) {
				g := R.Range(0, tau)
				vx, vy, life := R.Range(-6, 6), -R.Range(10, 25), R.Range(0.5, 0.9)
				ps.Spawn(pixelfx.Ember, HX+math.Cos(g)*HRX, HY+math.Sin(g)*HRY, vx, vy, life, C[5], 1, 0, 0)
			}
			ps.Update(dt)
			lights.Update(dt)
			loose.update(dt)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			shaft(p)
			pool(p, true)
			if t > POUR && t < POUR+0.4 {
				u := pixelfx.Seg(t, POUR, POUR+0.4)
				pixelfx.Band(p, 0, 0, 4+32*pixelfx.Ease.OutCubic(u), 0.18, 2.5*(1-u)+0.5, 1.1-u, C[3:], far, t < POUR+0.06)
			}
			for _, side := range sides {
				drawWing(p, side)
			}
			loose.draw(p, WING, pixelfx.LayerBack)
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			pool(p, false)
			if t > POUR && t < POUR+0.4 {
				u := pixelfx.Seg(t, POUR, POUR+0.4)
				pixelfx.Band(p, 0, 0, 4+32*pixelfx.Ease.OutCubic(u), 0.18, 2.5*(1-u)+0.5, 1.1-u, C[3:], near, t < POUR+0.06)
			}
			body(p)
			// the downstroke sends a ripple of light out along the floor
			if t > B2-0.03 && t < B2+0.45 {
				u := pixelfx.Seg(t, B2-0.03, B2+0.45)
				fx := 10 + 70*pixelfx.Ease.OutCubic(u)
				for _, s := range sides {
					for d := range 12 {
						fd := float64(d)
						c := C[4]
						if d < 2 {
							c = W
						} else if d < 5 {
							c = C[6]
						}
						p.Dot(s*(fx-fd), 0, c, (1-u)*(1-fd/12)*1.3)
					}
				}
			}
			// the wings light the character from behind
			if wk := pixelfx.Smooth(W0, W0+0.2, t) * (1 - pixelfx.Smooth(SC, SC+0.3, t)); wk > 0 {
				for _, pt := range S.out {
					if pt[1] < -8 {
						p.Light(pt[0], pt[1], C[5], 0.55*wk)
					}
				}
				p.Glow(0, -34, 76, C[4], 0.14*wk)
			}
			halo(p)
			if t >= CL1 && t < CL1+0.22 {
				pixelfx.Flare(p, 0, S.top-1, 22*(1-(t-CL1)/0.22), 1-(t-CL1)/0.22, C[3:])
			}
			loose.draw(p, WING, pixelfx.LayerFront)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
