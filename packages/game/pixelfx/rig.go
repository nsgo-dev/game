// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/rig.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import (
	"math"
	"sync"
)

// The character who acts: a real skeleton in 3D, drawn down to pixels the way Dead Cells makes its sprites. Bones have fixed lengths and hang from each other, pelvis to spine to chest to shoulders, so the body can twist, bend and turn round, and depth decides what's in front. A pose only says where the hands and feet should be and how the body is carrying itself; the solver works out every joint, and the renderer shades it into a few flat tones with one clean ink line.
//
// Coordinates: x right, y up, z towards the viewer, in pixels, the feet at the origin. Poses are in screen terms (y negative is up), as effects are. The character faces +x with his right side towards the viewer, so the right hand is the near one.
//
// A rendered pose also knows, for each of its pixels, which pixel of the standing character it came from (Src). That is what lets an effect drawn on the standing body (frost, poison, a scan line) follow the body wherever it goes.

type vec [3]float64
type mat [9]float64

func vadd(a, b vec) vec         { return vec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func vsub(a, b vec) vec         { return vec{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func vmul(a vec, s float64) vec { return vec{a[0] * s, a[1] * s, a[2] * s} }
func vdot(a, b vec) float64     { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func vlen(a vec) float64        { return Hypot3(a[0], a[1], a[2]) }
func vcross(a, b vec) vec {
	return vec{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }
func unit(a vec) vec {
	l := vlen(a)
	if l == 0 {
		l = 1e-6
	}
	return vmul(a, 1/l)
}

func rotY(t float64) mat {
	c, s := math.Cos(t), math.Sin(t)
	return mat{c, 0, s, 0, 1, 0, -s, 0, c}
}

func rotZ(t float64) mat {
	c, s := math.Cos(t), math.Sin(t)
	return mat{c, -s, 0, s, c, 0, 0, 0, 1}
}

func mm(A, B mat) mat {
	var o mat
	for r := range 3 {
		for c := range 3 {
			o[r*3+c] = A[r*3]*B[c] + A[r*3+1]*B[3+c] + A[r*3+2]*B[6+c]
		}
	}
	return o
}

func mv(M mat, v vec) vec {
	return vec{M[0]*v[0] + M[1]*v[1] + M[2]*v[2], M[3]*v[0] + M[4]*v[1] + M[5]*v[2], M[6]*v[0] + M[7]*v[1] + M[8]*v[2]}
}

func tv(M mat, v vec) vec {
	return vec{M[0]*v[0] + M[3]*v[1] + M[6]*v[2], M[1]*v[0] + M[4]*v[1] + M[7]*v[2], M[2]*v[0] + M[5]*v[1] + M[8]*v[2]}
}

// deg is a variable, not a constant, so expressions with it round as pixelfx's do instead of being folded exactly at compile time.
var deg = pi / 180

// The body's measurements, in pixels.
var meas = struct {
	pelvis, spine1, spine2, shoulderUp, shoulderOut, neckUp, headUp, hipDown, hipOut, thigh, shin, upper, fore, turn float64
}{19.7, 2.8, 4.6, 2.5, 5.9, 4.0, 3.1, 1.1, 2.8, 8.6, 8.3, 7.6, 7.4, 36 * deg}

var (
	rigPal                   = DummyPal
	rigInk, rigSeam, rigMark = rigPal['k'], rigPal['J'], rigPal['m']
	shell                    = [4]uint32{rigPal['A'], rigPal['B'], rigPal['C'], rigPal['D']}
	coreTones                = [4]uint32{rigPal['E'], rigPal['F'], rigPal['G'], rigPal['G']}
)

const restW, restH = 24, 38

type ikResult struct{ joint, end, u, v vec }

func ik(a, target vec, L1, L2 float64, pole vec) ikResult {
	to := vsub(target, a)
	d := vlen(to)
	reach := L1 + L2 - 0.02
	if d > reach {
		to = vmul(to, reach/d)
		d = reach
	}
	u := unit(to)
	along := (L1*L1 - L2*L2 + d*d) / (2 * d)
	h := math.Sqrt(math.Max(0, L1*L1-along*along))
	v := vsub(pole, vmul(u, vdot(pole, u)))
	if vlen(v) < 1e-4 {
		v = unit(vcross(u, vec{0, 0, 1}))
	} else {
		v = unit(v)
	}
	return ikResult{joint: vadd(a, vadd(vmul(u, along), vmul(v, h))), end: vadd(a, to), u: u, v: v}
}

type bone struct {
	t vec
	R mat
}

// a frame along a bone: its local x runs down the bone
func boneFrame(a, b, pole vec) bone {
	x := unit(vsub(b, a))
	y := vsub(pole, vmul(x, vdot(pole, x)))
	if vlen(y) < 1e-4 {
		y = unit(vcross(x, vec{0, 0, 1}))
	} else {
		y = unit(y)
	}
	z := vcross(x, y)
	return bone{a, mat{x[0], y[0], z[0], x[1], y[1], z[1], x[2], y[2], z[2]}}
}

// The frames of a solved pose, in pixelfx's order.
const (
	fPelvis = iota
	fWaist
	fChest
	fHead
	fNeck
	fUpperN
	fForeN
	fUpperF
	fForeF
	fThighN
	fShinN
	fThighF
	fShinF
	fFootN
	fFootF
	numFrames
)

type solved struct {
	B                      [numFrames]bone
	W, OW                  *Weapon
	q                      Pose
	axis, side             vec
	armN, armF, legN, legF ikResult
	sN, sF, hipN, hipF     vec
	turn                   float64
}

func solve(pose Pose) *solved {
	q := Stand.Merge(pose)
	lean := q.v[Lean]
	var W, OW *Weapon
	if q.weapon != "" {
		W = Weapons[q.weapon]
	}
	if q.offWeapon != "" {
		OW = Weapons[q.offWeapon]
	}
	sa, wl := q.Or(SA, 0), q.Or(WL, 1)
	axisZ := math.Sqrt(math.Max(0, 1-wl*wl)) * q.Or(WZ, 1)
	axis, side := vec{math.Cos(sa) * wl, -math.Sin(sa) * wl, axisZ}, vec{-math.Sin(sa), -math.Cos(sa), 0}
	two := q.on(Two) && W != nil
	nearZ := 4.5
	if two {
		nearZ = 3
	}
	nearZ = q.Or(FZ, nearZ)
	// how the chest is turned follows the hands: a hand driving forward brings its shoulder round; one drawn back opens him up
	fwd, farFwd := (q.v[FX]-Stand.v[FX])/24, 0.0
	if !two {
		farFwd = (q.v[BX] - Stand.v[BX]) / 22
	}
	var turn float64
	if fwd >= 0 {
		turn = meas.turn + -44*deg*math.Min(1, fwd)
	} else {
		turn = meas.turn + 28*deg*math.Min(1, -fwd*1.8)
	}
	extra := 0.0
	if farFwd > 0 {
		extra = 34 * deg * math.Min(1, farFwd)
	}
	turn += extra + q.Or(Twist, 0)*4*deg
	if q.Has(Turn) {
		turn = q.v[Turn]
	}
	hipTurn, tilt := meas.turn+(turn-meas.turn)*0.45, math.Atan2(lean, 13)
	Rp, Rc := mm(rotZ(-tilt*0.25), rotY(-hipTurn)), mm(rotZ(-tilt), rotY(-turn))
	// feet first: the pelvis sinks if that's what it takes for both legs to reach them
	ankleN, ankleF := vec{q.v[FFX], -q.v[FFY] - 1.8, q.Or(FZZ, 2.4)}, vec{q.v[FBX], -q.v[FBY] - 1.8, q.Or(BZZ, -2.4)}
	pelvis := vec{lean * 0.2, meas.pelvis - q.v[Crouch], 0}
	hipAt := func(s float64) vec { return vadd(pelvis, mv(Rp, vec{0, -meas.hipDown, s * meas.hipOut})) }
	maxLeg := (meas.thigh + meas.shin) * 0.995
	for _, sa := range [2]struct {
		s float64
		a vec
	}{{1, ankleN}, {-1, ankleF}} {
		h := hipAt(sa.s)
		dx, dz := h[0]-sa.a[0], h[2]-sa.a[2]
		flat := dx*dx + dz*dz
		if flat < maxLeg*maxLeg {
			top := sa.a[1] + math.Sqrt(maxLeg*maxLeg-flat)
			if h[1] > top {
				pelvis = vec{pelvis[0], pelvis[1] - (h[1] - top), pelvis[2]}
			}
		}
	}
	waist := vadd(pelvis, vec{math.Sin(tilt*0.45) * meas.spine1, math.Cos(tilt*0.45) * meas.spine1, 0})
	chest := vadd(waist, vec{math.Sin(tilt) * meas.spine2, math.Cos(tilt) * meas.spine2, 0})
	neck := vadd(chest, mv(Rc, vec{0, meas.neckUp, 0}))
	// the head keeps looking where he's going: it takes only part of the lean and part of the turn
	headTurn := meas.turn*0.9 + (turn-meas.turn)*0.35
	nod, roll := tilt*0.35+q.Or(HY, 0)*7*deg, q.Or(HX, 0)*7*deg
	Rh := mm(rotZ(-nod-roll), rotY(-headTurn))
	head := vadd(neck, mv(Rh, vec{0.15, meas.headUp, 0}))
	// the arms: shoulders reach a little towards a far hand
	pick := func(s float64, target vec) vec {
		base := vadd(chest, mv(Rc, vec{0, meas.shoulderUp, s * meas.shoulderOut}))
		to := vsub(target, base)
		d := vlen(to)
		return vadd(base, vmul(unit(to), math.Min(1.8, math.Max(0, d-11)*0.35)))
	}
	nearT := vec{q.v[FX], -q.v[FY], nearZ}
	var farT vec
	if two {
		w2 := W.two()
		farT = vec{q.v[FX] + axis[0]*w2, -q.v[FY] + axis[1]*w2, nearZ + axis[2]*w2}
	} else {
		bz := -4.5
		if q.on(ArmsFront) {
			bz = 5.5
		}
		farT = vec{q.v[BX], -q.v[BY], q.Or(BZ, bz)}
	}
	sN, sF := pick(1, nearT), pick(-1, farT)
	pole := func(s float64, T, shoulder vec, hold *vec) vec {
		// elbows hang down and back; overhead they go out and forward
		up := clamp01((T[1] - shoulder[1] + 2) / 10)
		base := vec{-0.55 + up*1.1, -1 + up*0.9, s * (0.45 + up*0.5)}
		if hold == nil {
			return base
		}
		// holding something, the forearm wants to meet the handle side on: try round the circle, keep the best
		u := unit(vsub(T, shoulder))
		best, score := base, math.Inf(1)
		for i := range 16 {
			a := (float64(i) / 16) * math.Pi * 2
			x := unit(vcross(u, vec{0.3, 1, 0.2}))
			y := vcross(u, x)
			v := vadd(vmul(x, math.Cos(a)), vmul(y, math.Sin(a)))
			A := ik(shoulder, T, meas.upper, meas.fore, v)
			f := unit(vsub(A.end, A.joint))
			cost := math.Abs(vdot(f, *hold))*0.7 + (1-vdot(unit(base), v))*0.55
			if cost < score {
				score, best = cost, v
			}
		}
		return best
	}
	var holdN, holdF *vec
	if W != nil {
		h := axis
		if W.Across {
			h = side
		}
		holdN = &h
	}
	switch {
	case two:
		h := axis
		if W.Across {
			h = side
		}
		holdF = &h
	case OW != nil:
		sb := q.Or(SB, math.Pi)
		h := vec{math.Cos(sb), -math.Sin(sb), 0}
		holdF = &h
	}
	armN := ik(sN, nearT, meas.upper, meas.fore, pole(1, nearT, sN, holdN))
	armF := ik(sF, farT, meas.upper, meas.fore, pole(-1, farT, sF, holdF))
	// the legs: knees forward, a little out
	kneePole := func(s float64) vec { return mv(Rp, vec{1, 0, s * 0.35}) }
	hipN, hipF := hipAt(1), hipAt(-1)
	legN := ik(hipN, ankleN, meas.thigh, meas.shin, kneePole(1))
	legF := ik(hipF, ankleF, meas.thigh, meas.shin, kneePole(-1))
	footDir := func(s float64) vec {
		t := hipTurn*0.6 + s*8*deg
		return vec{math.Cos(t), 0, math.Sin(t)}
	}
	S := &solved{W: W, OW: OW, q: q, axis: axis, side: side, armN: armN, armF: armF, legN: legN, legF: legF, sN: sN, sF: sF, hipN: hipN, hipF: hipF, turn: turn}
	S.B[fPelvis] = bone{pelvis, Rp}
	S.B[fWaist] = bone{waist, mm(rotZ(-tilt*0.6), rotY(-(turn+hipTurn)/2))}
	S.B[fChest] = bone{chest, Rc}
	S.B[fHead] = bone{head, Rh}
	S.B[fNeck] = bone{neck, Rh}
	S.B[fUpperN] = boneFrame(sN, armN.joint, armN.v)
	S.B[fForeN] = boneFrame(armN.joint, armN.end, armN.v)
	S.B[fUpperF] = boneFrame(sF, armF.joint, armF.v)
	S.B[fForeF] = boneFrame(armF.joint, armF.end, armF.v)
	S.B[fThighN] = boneFrame(hipN, legN.joint, legN.v)
	S.B[fShinN] = boneFrame(legN.joint, legN.end, legN.v)
	S.B[fThighF] = boneFrame(hipF, legF.joint, legF.v)
	S.B[fShinF] = boneFrame(legF.joint, legF.end, legF.v)
	toe := func(on bool) float64 {
		if on {
			return -55 * deg
		}
		return 0
	}
	fN, fF := footDir(1), footDir(-1)
	S.B[fFootN] = bone{legN.end, mm(rotY(-math.Atan2(fN[2], fN[0])), rotZ(toe(q.on(ToeF))))}
	S.B[fFootF] = bone{legF.end, mm(rotY(-math.Atan2(fF[2], fF[0])), rotZ(toe(q.on(ToeB))))}
	return S
}

// ---- drawing: every part goes through one depth buffer

// the scratch grid, big enough for a raised scythe; feet at (gridOX, gridOY)
const gridW, gridH, gridOX, gridOY = 300, 240, 150, 180

var rigLight = unit(vec{-0.55, 0.72, 0.5})

// Parts: each has its own number, so edges between them can be seen.
const (
	partPelvis = iota + 1
	partWaist
	partChest
	partNeck
	partHead
	partFace
	partUpperN
	partElbowN
	partForeN
	partHandN
	partUpperF
	partElbowF
	partForeF
	partHandF
	partThighN
	partKneeN
	partShinN
	partFootN
	partThighF
	partKneeF
	partShinF
	partFootF
	partWeapon
	partOffWeapon
)

var partNames = [...]string{"", "pelvis", "waist", "chest", "neck", "head", "face", "upperN", "elbowN", "foreN", "handN", "upperF", "elbowF", "foreF", "handF", "thighN", "kneeN", "shinN", "footN", "thighF", "kneeF", "shinF", "footF", "weapon", "offWeapon"}

// renderer is the rig's scratch: a depth buffer and what was drawn where, reused by every render under its lock.
type renderer struct {
	mu                   sync.Mutex
	zb, tone             []float32
	part, mat, fr        []int8
	col                  []uint32
	touched              []int
	tones                []int8
	marks                []uint32
	ringFrom             []int32
	clipBody, clipWeapon bool
	curFrame             int8
	onlyPart             int8
}

var rigScratch = &renderer{}

func (r *renderer) init() {
	if r.zb != nil {
		return
	}
	n := gridW * gridH
	r.zb, r.tone = make([]float32, n), make([]float32, n)
	r.part, r.mat, r.fr = make([]int8, n), make([]int8, n), make([]int8, n)
	r.col = make([]uint32, n)
	r.tones = make([]int8, n)
	r.marks = make([]uint32, n)
	r.ringFrom = make([]int32, n)
	for i := range r.tones {
		r.tones[i] = -1
		r.ringFrom[i] = -1
	}
}

func (r *renderer) plot(i int, z, tone float64, part, mt int8, color uint32) {
	if r.onlyPart != 0 && part != r.onlyPart {
		return
	}
	if i < 0 || i >= len(r.part) {
		return
	}
	if r.part[i] == 0 {
		r.touched = append(r.touched, i)
	} else if z <= float64(r.zb[i]) {
		return
	}
	r.zb[i] = float32(z)
	r.tone[i] = float32(tone)
	r.part[i] = part
	r.mat[i] = mt
	r.fr[i] = r.curFrame
	r.col[i] = color
}

func (r *renderer) sphere(c vec, rad float64, part, mt int8) {
	x0, x1, y0, y1 := math.Floor(c[0]-rad), math.Ceil(c[0]+rad), math.Floor(c[1]-rad), math.Ceil(c[1]+rad)
	for wy := y0; wy <= y1; wy++ {
		for wx := x0; wx <= x1; wx++ {
			dx, dy := wx-c[0], wy-c[1]
			d2 := dx*dx + dy*dy
			if d2 > rad*rad || (r.clipBody && wy < 1.5) {
				continue
			}
			dz := math.Sqrt(rad*rad - d2)
			n := vec{dx / rad, dy / rad, dz / rad}
			r.plot((gridOY-int(wy))*gridW+(gridOX+int(wx)), c[2]+dz, vdot(n, rigLight), part, mt, 0)
		}
	}
}

func (r *renderer) sweep(a, b vec, ra, rb float64, part, mt int8, bulge float64) {
	n := math.Max(2, math.Ceil(vlen(vsub(b, a))/0.4))
	for k := 0.0; k <= n; k++ {
		u := k / n
		r.sphere(vadd(a, vmul(vsub(b, a), u)), ra+(rb-ra)*u+bulge*math.Sin(u*math.Pi), part, mt)
	}
}

// an ellipsoid with radii rr along the axes of frame R, seen along z
func (r *renderer) ellipsoid(c vec, R mat, rr vec, part, mt int8) {
	big := math.Max(rr[0], math.Max(rr[1], rr[2]))
	x0, x1, y0, y1 := math.Floor(c[0]-big), math.Ceil(c[0]+big), math.Floor(c[1]-big), math.Ceil(c[1]+big)
	D := vec{1 / (rr[0] * rr[0]), 1 / (rr[1] * rr[1]), 1 / (rr[2] * rr[2])}
	// A = R diag(D) R^T
	var A mat
	for i := range 3 {
		for j := range 3 {
			A[i*3+j] = R[i*3]*D[0]*R[j*3] + R[i*3+1]*D[1]*R[j*3+1] + R[i*3+2]*D[2]*R[j*3+2]
		}
	}
	for wy := y0; wy <= y1; wy++ {
		for wx := x0; wx <= x1; wx++ {
			if r.clipBody && wy < 1.5 {
				continue
			}
			dx, dy := wx-c[0], wy-c[1]
			qa, qb := A[8], 2*(A[2]*dx+A[5]*dy)
			qc := A[0]*dx*dx + 2*A[1]*dx*dy + A[4]*dy*dy - 1
			disc := qb*qb - 4*qa*qc
			if disc < 0 {
				continue
			}
			dz := (-qb + math.Sqrt(disc)) / (2 * qa)
			n := unit(vec{A[0]*dx + A[1]*dy + A[2]*dz, A[3]*dx + A[4]*dy + A[5]*dz, A[6]*dx + A[7]*dy + A[8]*dz})
			r.plot((gridOY-int(wy))*gridW+(gridOX+int(wx)), c[2]+dz, vdot(n, rigLight), part, mt, 0)
		}
	}
}

func (r *renderer) drawBody(S *solved) {
	B := &S.B
	r.curFrame = fPelvis
	r.ellipsoid(vadd(B[fPelvis].t, mv(B[fPelvis].R, vec{0, -0.2, 0})), B[fPelvis].R, vec{2.9, 2.6, 4.2}, partPelvis, 0)
	r.curFrame = fWaist
	r.sweep(B[fPelvis].t, B[fChest].t, 3, 3.3, partWaist, 1, 0)
	r.curFrame = fChest
	r.ellipsoid(vadd(B[fChest].t, mv(B[fChest].R, vec{0.2, 0.9, 0})), B[fChest].R, vec{3.4, 4.2, 5.5}, partChest, 0)
	r.curFrame = fNeck
	r.sweep(B[fNeck].t, vadd(B[fNeck].t, mv(B[fHead].R, vec{0, 1.6, 0})), 1.25, 1.15, partNeck, 1, 0)
	r.curFrame = fHead
	r.ellipsoid(B[fHead].t, B[fHead].R, vec{3, 3.3, 2.8}, partHead, 0)
	r.ellipsoid(vadd(B[fHead].t, mv(B[fHead].R, vec{0.85, -0.45, 0})), B[fHead].R, vec{2.4, 2.7, 2.35}, partFace, 0)
	arm := func(A ikResult, sh vec, up, el, fo, ha int8, fu, ff int8) {
		r.curFrame = fu
		r.sphere(sh, 2.5, up, 0)
		r.sweep(sh, A.joint, 2.1, 1.6, up, 0, 0)
		r.sphere(A.joint, 1.45, el, 1)
		r.curFrame = ff
		r.sweep(A.joint, A.end, 1.6, 1.2, fo, 0, 0.15)
		r.sphere(A.end, 1.9, ha, 0)
	}
	arm(S.armN, S.sN, partUpperN, partElbowN, partForeN, partHandN, fUpperN, fForeN)
	arm(S.armF, S.sF, partUpperF, partElbowF, partForeF, partHandF, fUpperF, fForeF)
	leg := func(L ikResult, hip vec, th, kn, sh, fo int8, ft, fs, ff int8) {
		r.curFrame = ft
		r.sweep(hip, L.joint, 3.2, 2.2, th, 0, 0)
		r.sphere(L.joint, 1.9, kn, 1)
		r.curFrame = fs
		r.sweep(L.joint, L.end, 2.15, 1.4, sh, 0, 0.4)
		r.curFrame = ff
		R, a := B[ff].R, B[ff].t
		r.sweep(vadd(a, mv(R, vec{-1.5, -0.6, 0})), vadd(a, mv(R, vec{4.6, -1.1, 0})), 1.7, 1.25, fo, 0, 0)
	}
	leg(S.legN, S.hipN, partThighN, partKneeN, partShinN, partFootN, fThighN, fShinN, fFootN)
	leg(S.legF, S.hipF, partThighF, partKneeF, partShinF, partFootF, fThighF, fShinF, fFootF)
}

// ---- what they hold: drawn along the handle from the fist, through the same depth buffer

// Hand draws a held weapon along its handle from the fist: Put(i, k, c, lift) plots color c i px along the handle, k px across it and lift px off its plane, and At(i, k) returns that point.
type Hand struct {
	r                *renderer
	h, axis, side, n vec
	part             int8
}

// Put plots color c i px along the handle from the fist, k px across it and lift px out of its plane.
func (T *Hand) Put(i, k float64, c uint32, lift float64) {
	x := T.h[0] + T.axis[0]*i + T.side[0]*k + T.n[0]*lift
	y := T.h[1] + T.axis[1]*i + T.side[1]*k + T.n[1]*lift
	z := T.h[2] + T.axis[2]*i + T.side[2]*k + T.n[2]*lift
	wx, wy := int(Round(x)), int(Round(y))
	if T.r.clipWeapon && wy < 0 {
		return
	}
	idx := (gridOY-wy)*gridW + (gridOX + wx)
	if idx < 0 || idx >= gridW*gridH {
		return
	}
	T.r.curFrame = -1
	T.r.plot(idx, z+0.05, 2, T.part, 2, c)
}

// At returns the point i px along the handle and k px across it.
func (T *Hand) At(i, k float64) [3]float64 {
	return [3]float64{T.h[0] + T.axis[0]*i + T.side[0]*k, T.h[1] + T.axis[1]*i + T.side[1]*k, T.h[2] + T.axis[2]*i + T.side[2]*k}
}

// line plots a straight run of color c from A to B (a bow's string).
func (T *Hand) line(A, B vec, c uint32) {
	n := math.Max(2, math.Ceil(vlen(vsub(B, A))/0.4))
	for k := 0.0; k <= n; k++ {
		P := vadd(A, vmul(vsub(B, A), k/n))
		wx, wy := int(Round(P[0])), int(Round(P[1]))
		T.r.curFrame = -1
		T.r.plot((gridOY-wy)*gridW+(gridOX+wx), P[2]+0.05, 2, partWeapon, 2, c)
	}
}

func (r *renderer) along(H, axis, side vec, part int8) *Hand {
	return &Hand{r: r, h: H, axis: axis, side: side, n: vcross(axis, side), part: part}
}

// Weapon is something the character can hold: Two is where the other hand goes on it when both do (none when HasTwo is false), Across lays the handle across the weapon's length, and Draw draws it with the pose and the far hand's place.
type Weapon struct {
	Two    float64
	HasTwo bool
	Across bool
	Draw   func(T *Hand, q Pose, off [3]float64)
}

func (w *Weapon) two() float64 {
	if !w.HasTwo {
		return math.NaN()
	}
	return w.Two
}

var steel = func() map[string]uint32 {
	m := map[string]string{"hi": "#ffffff", "a": "#e8edf6", "b": "#a4afc6", "d": "#68748f", "gold": "#ffd84a", "goldD": "#b8862a", "grip": "#7a3a2a", "gripD": "#4a2018", "wood": "#8a5a32", "woodD": "#5a3820", "dark": "#3a3448", "darkD": "#262033", "olive": "#58663f", "oliveA": "#7d8c58", "oliveD": "#3a4529"}
	out := make(map[string]uint32, len(m))
	for k, v := range m {
		out[k] = Hex(v)
	}
	return out
}()

func blade(T *Hand, from, length, w float64) {
	for i := from; i <= from+length+0.01; i += 0.4 {
		r := i - from
		wd := w
		if r > length-5 {
			wd = math.Max(0, ((length-r)/5)*w)
		}
		for k := -wd; k <= wd+0.01; k += 0.4 {
			c := steel["a"]
			if k < -w*0.4 {
				c = steel["hi"]
			} else if k > w*0.4 {
				c = steel["d"]
			}
			T.Put(i, k, c, 0)
		}
	}
}

func haft(T *Hand, from, to float64, a, d uint32, r float64) {
	for i := from; i <= to; i += 0.4 {
		for k := -r; k <= r+0.01; k += 0.4 {
			c := a
			if k > 0.1 {
				c = d
			}
			T.Put(i, k, c, 0)
		}
	}
}

func pick2(cond bool, a, b uint32) uint32 {
	if cond {
		return a
	}
	return b
}

// Weapons are what the character can hold, by name: the fist is at 0 along each.
var Weapons = map[string]*Weapon{
	"sword": {Two: -4.4, HasTwo: true, Draw: func(T *Hand, q Pose, _ [3]float64) {
		end := -2.6
		if q.on(Two) {
			end = -6.5
		}
		haft(T, end, 2.2, steel["grip"], steel["gripD"], 0.8)
		for k := -1.4; k <= 1.4; k += 0.4 {
			T.Put(end-0.4, k, steel["gold"], 0)
		}
		for k := -4.6; k <= 4.6; k += 0.4 {
			T.Put(2.6, k, pick2(math.Abs(k) > 3.8, steel["goldD"], steel["gold"]), 0)
			T.Put(3, k, steel["goldD"], 0)
		}
		blade(T, 3.2, 25, 2.2)
	}},
	"greatsword": {Two: -4.8, HasTwo: true, Draw: func(T *Hand, _ Pose, _ [3]float64) {
		haft(T, -7.2, 2.2, steel["grip"], steel["gripD"], 0.8)
		for k := -1.8; k <= 1.8; k += 0.4 {
			T.Put(-7.6, k, steel["gold"], 0)
		}
		for k := -6.6; k <= 6.6; k += 0.4 {
			T.Put(2.6, k, pick2(math.Abs(k) > 5.6, steel["goldD"], steel["gold"]), 0)
			T.Put(3, k, steel["goldD"], 0)
		}
		blade(T, 3.2, 31, 3)
		for i := 4.0; i <= 28; i += 0.4 {
			T.Put(i, 0, steel["b"], 0)
		}
	}},
	"dagger": {Draw: func(T *Hand, _ Pose, _ [3]float64) {
		haft(T, -2.4, 1.8, steel["grip"], steel["gripD"], 0.8)
		for k := -2.6; k <= 2.6; k += 0.4 {
			T.Put(2.2, k, steel["goldD"], 0)
		}
		blade(T, 2.6, 10, 1.3)
	}},
	"spear": {Two: -12, HasTwo: true, Draw: func(T *Hand, _ Pose, _ [3]float64) {
		haft(T, -18, 36, steel["wood"], steel["woodD"], 0.5)
		blade(T, 36, 11, 2)
		for k := -2.4; k <= 2.4; k += 0.4 {
			T.Put(36, k, steel["goldD"], 0)
		}
	}},
	"hammer": {Two: -7, HasTwo: true, Draw: func(T *Hand, _ Pose, _ [3]float64) {
		for _, lift := range [3]float64{-0.8, 0, 0.8} {
			for i := -10.0; i <= 18; i += 0.4 {
				for k := -0.8; k <= 0.81; k += 0.4 {
					T.Put(i, k, pick2(k > 0.1, steel["woodD"], steel["wood"]), lift)
				}
			}
		}
		// the head is a block: its two faces, and the steel between them
		for i := 18.0; i <= 28; i += 0.4 {
			for k := -9.0; k <= 9; k += 0.4 {
				var face uint32
				switch {
				case math.Abs(k) > 7.4:
					face = pick2(k < 0, steel["a"], steel["d"])
				case i < 20:
					face = steel["hi"]
				case i > 26.4:
					face = steel["d"]
				case math.Abs(k) < 2:
					face = steel["gold"]
				default:
					face = steel["b"]
				}
				T.Put(i, k, face, 4.5)
				T.Put(i, k, face, -4.5)
				if i < 18.4 || i > 27.6 || math.Abs(k) > 8.6 {
					for l := -4.0; l <= 4; l += 0.4 {
						var c uint32
						switch {
						case k < 0:
							c = steel["a"]
						case i > 27.6:
							c = steel["d"]
						default:
							c = steel["b"]
						}
						T.Put(i, k, c, l)
					}
				}
			}
		}
	}},
	"scythe": {Two: -11, HasTwo: true, Draw: func(T *Hand, _ Pose, _ [3]float64) {
		haft(T, -16, 31, steel["dark"], steel["darkD"], 0.5)
		for j := 0.0; j <= 28; j += 0.4 {
			u := j / 28
			w, b := 3.2*(1-u)+0.3, 31.5-u*u*12
			for k := -w; k <= w; k += 0.4 {
				var c uint32
				switch {
				case k > w*0.3:
					c = steel["hi"]
				case k < -w*0.4:
					c = steel["d"]
				default:
					c = steel["b"]
				}
				T.Put(b+k, j+0.8, c, 0)
			}
		}
	}},
	"claw": {Draw: func(T *Hand, _ Pose, _ [3]float64) {
		for _, q0 := range [3]float64{-2.4, 0, 2.4} {
			for i := 2.0; i <= 9.5; i += 0.4 {
				u := (i - 2) / 7.5
				k := q0 + u*u*1.8
				T.Put(i, k, pick2(u > 0.55, steel["hi"], steel["a"]), 0)
				if u < 0.6 {
					T.Put(i, k+0.8, steel["d"], 0)
				}
			}
		}
	}},
	"bow": {Across: true, Draw: func(T *Hand, q Pose, off [3]float64) {
		for u := -1.0; u <= 1.001; u += 0.025 {
			k, i := u*17, 6*(1-u*u)-1
			T.Put(i, k, pick2(math.Abs(u) > 0.85, steel["goldD"], steel["wood"]), 0)
			T.Put(i+0.8, k, steel["woodD"], 0)
		}
		tipA, tipB := vec(T.At(-1, -17)), vec(T.At(-1, 17))
		mid := vec(T.At(-1, 0))
		if q.on(Pull) {
			mid = vec(off)
		}
		T.line(tipA, mid, steel["a"])
		T.line(tipB, mid, steel["a"])
		if q.on(Nocked) {
			T.line(mid, vec(T.At(13, 0)), steel["wood"])
			for i := 9.0; i <= 13; i += 0.4 {
				for k := -(13 - i) * 0.45; k <= (13-i)*0.45; k += 0.4 {
					T.Put(i, k, steel["hi"], 0)
				}
			}
		}
	}},
	"pistol": {Across: true, Draw: func(T *Hand, _ Pose, _ [3]float64) {
		for i := -1.0; i <= 9.5; i += 0.4 {
			for k := -4.2; k <= -2.4; k += 0.4 {
				var c uint32
				switch {
				case k < -3.6:
					c = steel["a"]
				case k > -2.9:
					c = steel["d"]
				default:
					c = steel["b"]
				}
				T.Put(i, k, c, 0)
			}
		}
		for i := -1.2; i <= 1.2; i += 0.4 {
			for k := -2.4; k <= 2.4; k += 0.4 {
				T.Put(i, k, steel["gripD"], 0)
			}
		}
		T.Put(9, -4.6, steel["hi"], 0)
	}},
	"launcher": {Two: -9, HasTwo: true, Draw: func(T *Hand, _ Pose, _ [3]float64) {
		for i := -14.0; i <= 22; i += 0.4 {
			for k := -6.0; k <= -1; k += 0.4 {
				var c uint32
				switch {
				case k < -4.8:
					c = steel["oliveA"]
				case k > -2.2:
					c = steel["oliveD"]
				case math.Abs(i-3) < 1.4:
					c = steel["gold"]
				default:
					c = steel["olive"]
				}
				T.Put(i, k, c, 0)
			}
		}
		for k := -6.8; k <= -0.2; k += 0.4 {
			T.Put(22.4, k, steel["b"], 0)
			T.Put(-14.4, k, steel["d"], 0)
		}
	}},
	"handle": {Draw: func(T *Hand, _ Pose, _ [3]float64) {
		haft(T, -2.6, 6, steel["grip"], steel["gripD"], 0.8)
		for k := -1.6; k <= 1.6; k += 0.4 {
			T.Put(-3, k, steel["gold"], 0)
			T.Put(6.4, k, steel["gold"], 0)
		}
	}},
}

// Posed is a pose drawn down to pixels, in local space with the feet at (0, 0): pixel i is at (Xs[i], Ys[i]) in Color, came from pixel Src[i] of the standing 24 x 38 character (-1 for none), and is ink (outline) when Ink[i] is 1. X0, Y0, X1 and Y1 bound it, and Z is the body's mean depth.
type Posed struct {
	N              int
	Xs, Ys         []int16
	Color          []uint32
	Src            []int16
	Ink            []uint8
	X0, Y0, X1, Y1 int
	Z              float64
}

// ---- flat tones, seams, and one clean ink line
func (r *renderer) finish(S *solved, rest *[numFrames]bone) *Posed {
	dim := func(part int8) int {
		if (part >= partUpperF && part <= partHandF) || (part >= partThighF && part <= partFootF) {
			return 1
		}
		return 0
	}
	toneOf := func(v float64, part int8) int8 {
		t := 3
		switch {
		case v > 0.8:
			t = 0
		case v > 0.42:
			t = 1
		case v > 0.08:
			t = 2
		}
		return int8(min(3, t+dim(part)))
	}
	for _, i := range r.touched {
		r.tones[i] = toneOf(float64(r.tone[i]), r.part[i])
	}
	// a lone pixel of a tone, with no neighbour like it on the same part, takes its neighbours' tone
	n := len(r.part)
	for _, i := range r.touched {
		if r.mat[i] == 2 {
			continue
		}
		t := r.tones[i]
		var votes [4]int
		same, any := false, false
		for _, d := range [4]int{-1, 1, -gridW, gridW} {
			j := i + d
			if j < 0 || j >= n || r.part[j] != r.part[i] || r.tones[j] < 0 {
				continue
			}
			u := r.tones[j]
			votes[u]++
			any = true
			if u == t {
				same = true
			}
		}
		if !same && any {
			best := 0
			for v := 1; v < 4; v++ {
				if votes[v] > votes[best] {
					best = v
				}
			}
			r.tones[i] = int8(best)
		}
	}
	for _, i := range r.touched {
		if r.mat[i] == 2 {
			continue
		}
		if r.mat[i] == 1 {
			r.col[i] = coreTones[r.tones[i]]
		} else {
			r.col[i] = shell[r.tones[i]]
		}
	}
	// where two parts meet: a soft seam if they touch, ink if one stands in front of the other
	var marked []int
	for _, i := range r.touched {
		for _, d := range [2]int{1, gridW} {
			j := i + d
			if j >= n || r.part[j] == 0 || r.part[j] == r.part[i] {
				continue
			}
			far := j
			if r.zb[i] < r.zb[j] {
				far = i
			}
			dz := math.Abs(float64(r.zb[i]) - float64(r.zb[j]))
			steelEdge := r.mat[i] == 2 || r.mat[j] == 2
			if r.mat[far] == 2 && dz < 1 {
				continue
			}
			if steelEdge || dz > 2.2 {
				if r.marks[far] == 0 {
					marked = append(marked, far)
				}
				r.marks[far] = rigInk
			} else if r.marks[far] == 0 {
				marked = append(marked, far)
				r.marks[far] = rigSeam
			}
		}
	}
	for _, i := range marked {
		r.col[i] = r.marks[i]
		r.marks[i] = 0
	}
	// the mark on his chest, when it's facing us
	B := &S.B
	chestAt := vadd(B[fChest].t, mv(B[fChest].R, vec{3, 1.6, -1.8}))
	if mv(B[fChest].R, vec{1, 0, 0})[2] > 0.15 {
		i := (gridOY-int(Round(chestAt[1])))*gridW + (gridOX + int(Round(chestAt[0])))
		if i >= 0 && i < n && r.part[i] == partChest {
			r.col[i] = rigMark
		}
	}
	// the outline, and everything into a small list of pixels
	list := r.touched
	var ring []int
	for _, i := range list {
		for _, d := range [4]int{-1, 1, -gridW, gridW} {
			j := i + d
			if j >= 0 && j < n && r.part[j] == 0 && r.ringFrom[j] < 0 {
				r.ringFrom[j] = int32(i)
				ring = append(ring, j)
			}
		}
	}
	total := len(list) + len(ring)
	out := &Posed{N: total, Xs: make([]int16, total), Ys: make([]int16, total), Color: make([]uint32, total), Src: make([]int16, total), Ink: make([]uint8, total)}
	srcFor := func(i int) int16 {
		f := r.fr[i]
		if f < 0 || rest == nil {
			return -1
		}
		A, R0 := B[f], rest[f]
		wx, wy := float64(i%gridW-gridOX), float64(gridOY-i/gridW)
		P := vec{wx, wy, float64(r.zb[i])}
		lp := tv(A.R, vsub(P, A.t))
		rp := vadd(R0.t, mv(R0.R, lp))
		c, row := int(Round(rp[0]))+12, 38-int(Round(rp[1]))
		if c < 0 || row < 0 || c >= restW || row >= restH {
			return -1
		}
		return int16(row*restW + c)
	}
	k, x0, y0, x1, y1 := 0, 1000000000, 1000000000, -1000000000, -1000000000
	push := func(i int, c uint32, s int16, isInk bool) {
		x, y := i%gridW-gridOX, i/gridW-gridOY
		out.Xs[k], out.Ys[k], out.Color[k], out.Src[k] = int16(x), int16(y), c, s
		if isInk {
			out.Ink[k] = 1
		}
		x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
		k++
	}
	for _, i := range list {
		push(i, r.col[i], srcFor(i), r.col[i] == rigInk)
	}
	for _, j := range ring {
		push(j, rigInk, srcFor(int(r.ringFrom[j])), true)
		r.ringFrom[j] = -1
	}
	zs := 0.0
	for _, i := range list {
		zs += float64(r.zb[i])
	}
	out.X0, out.Y0, out.X1, out.Y1 = x0, y0, x1, y1
	if len(list) > 0 {
		out.Z = zs / float64(len(list))
	}
	for _, i := range list {
		r.part[i] = 0
		r.tones[i] = -1
	}
	r.touched = r.touched[:0]
	return out
}

func turnAll(S *solved, a float64) {
	// the whole figure turned about the vertical through the feet
	R := rotY(-a)
	for i := range S.B {
		S.B[i].t = mv(R, S.B[i].t)
		S.B[i].R = mm(R, S.B[i].R)
	}
	for _, A := range [4]*ikResult{&S.armN, &S.armF, &S.legN, &S.legF} {
		A.joint, A.end = mv(R, A.joint), mv(R, A.end)
	}
	S.sN, S.sF, S.hipN, S.hipF = mv(R, S.sN), mv(R, S.sF), mv(R, S.hipN), mv(R, S.hipF)
	S.axis, S.side = mv(R, S.axis), mv(R, S.side)
}

func wholeTurn(q Pose) float64 {
	w := q.Or(Yaw, 0)
	if q.Has(Face) && q.v[Face] == -1 {
		w += math.Pi
	}
	return w
}

var restFrames = solve(Pose{}).B

// render draws pose into the scratch and returns its pixels; the caller holds r.mu.
func (r *renderer) render(pose Pose) *Posed {
	r.init()
	S := solve(pose)
	q := S.q
	whole := wholeTurn(q)
	if whole != 0 {
		turnAll(S, whole)
	}
	air := q.Or(Y, 0) < -0.5
	r.clipBody, r.clipWeapon = !air, !air
	r.drawBody(S)
	r.clipBody = false
	if S.W != nil {
		S.W.Draw(r.along(S.armN.end, S.axis, S.side, partWeapon), q, S.armF.end)
	}
	if S.OW != nil {
		sb := q.Or(SB, math.Pi)
		ax, sd := vec{math.Cos(sb), -math.Sin(sb), 0}, vec{-math.Sin(sb), -math.Cos(sb), 0}
		if whole != 0 {
			R := rotY(-whole)
			ax, sd = mv(R, ax), mv(R, sd)
		}
		S.OW.Draw(r.along(S.armF.end, ax, sd, partOffWeapon), q, S.armF.end)
	}
	r.clipWeapon = false
	return r.finish(S, &restFrames)
}

// rigCacheLimit is how many poses a Rig keeps before it starts over, as pixelfx's does.
const rigCacheLimit = 6000

// Rig draws poses down to pixels and keeps the ones it drew, keyed as pixelfx keys them: every key rounded to a pixel (angles to a twentieth of a radian), so poses that round alike share the first one's pixels. An Act and ActBounds take one, so an effect acted out draws what pixelfx draws for it in a fresh process. The zero Rig is ready to use; a Rig is not safe for concurrent use.
type Rig struct {
	cache map[poseCacheKey]*Posed
}

// NewRig returns a Rig with nothing drawn yet.
func NewRig() *Rig { return &Rig{} }

// Posed returns the pixels of a pose, in local space with the feet at (0, 0).
func (g *Rig) Posed(pose Pose) *Posed {
	key := keyOf(pose)
	if r, ok := g.cache[key]; ok {
		return r
	}
	if g.cache == nil || len(g.cache) > rigCacheLimit {
		g.cache = map[poseCacheKey]*Posed{}
	}
	rigScratch.mu.Lock()
	r := rigScratch.render(pose)
	rigScratch.mu.Unlock()
	g.cache[key] = r
	return r
}

// Shortfall returns how far short of where the pose wants them the hands fall, in px: arms don't stretch.
func Shortfall(pose Pose) float64 {
	S := solve(pose)
	q := S.q
	near := vlen(vsub(S.armN.end, vec{q.v[FX], -q.v[FY], S.armN.end[2]}))
	far := 0.0
	if !q.on(Two) {
		far = vlen(vsub(S.armF.end, vec{q.v[BX], -q.v[BY], S.armF.end[2]}))
	}
	return math.Max(near, far)
}

// Skeleton is the skeleton of a pose, in the same local space as Rig.Posed: x right, y down, feet at (0, 0), z toward the viewer, the whole figure turned by its yaw. Joints are by name, Bones join two joints, and Handles are what a pose moves (hands and feet); everything else is solved from them.
type Skeleton struct {
	Joints  map[string][3]float64
	Bones   [][2]string
	Handles []string
}

// SkeletonOf returns the skeleton of a pose.
func SkeletonOf(pose Pose) Skeleton {
	S := solve(pose)
	if whole := wholeTurn(S.q); whole != 0 {
		turnAll(S, whole)
	}
	B := &S.B
	at := func(f bone, v vec) vec { return vadd(f.t, mv(f.R, v)) }
	J := map[string]vec{
		"pelvis": B[fPelvis].t, "waist": B[fWaist].t, "chest": B[fChest].t, "neck": B[fNeck].t, "head": B[fHead].t, "crown": at(B[fHead], vec{0, 3.3, 0}),
		"shoulderN": S.sN, "elbowN": S.armN.joint, "handN": S.armN.end, "shoulderF": S.sF, "elbowF": S.armF.joint, "handF": S.armF.end,
		"hipN": S.hipN, "kneeN": S.legN.joint, "ankleN": S.legN.end, "toeN": at(B[fFootN], vec{4.6, -1.1, 0}), "hipF": S.hipF, "kneeF": S.legF.joint, "ankleF": S.legF.end, "toeF": at(B[fFootF], vec{4.6, -1.1, 0}),
	}
	joints := make(map[string][3]float64, len(J))
	for k, v := range J {
		joints[k] = [3]float64{v[0], -v[1], v[2]}
	}
	bones := [][2]string{{"pelvis", "waist"}, {"waist", "chest"}, {"chest", "neck"}, {"neck", "head"}, {"head", "crown"}, {"chest", "shoulderN"}, {"shoulderN", "elbowN"}, {"elbowN", "handN"}, {"chest", "shoulderF"}, {"shoulderF", "elbowF"}, {"elbowF", "handF"}, {"pelvis", "hipN"}, {"hipN", "kneeN"}, {"kneeN", "ankleN"}, {"ankleN", "toeN"}, {"pelvis", "hipF"}, {"hipF", "kneeF"}, {"kneeF", "ankleF"}, {"ankleF", "toeF"}}
	return Skeleton{Joints: joints, Bones: bones, Handles: []string{"handN", "handF", "ankleN", "ankleF"}}
}

// PosedPart is one part of a figure taken apart: its name and number, its pixels as Posed holds them, and where its middle is (CX, CY) and its mean depth Z.
type PosedPart struct {
	Name   string
	Part   int
	N      int
	Xs, Ys []int16
	Color  []uint32
	CX, CY float64
	Z      float64
}

// PosedParts returns the figure taken apart: each part drawn alone, with its own outline, and where its middle is. For exploded views.
func PosedParts(pose Pose) []PosedPart {
	var out []PosedPart
	rigScratch.mu.Lock()
	defer rigScratch.mu.Unlock()
	for part := int8(partPelvis); part <= partOffWeapon; part++ {
		rigScratch.onlyPart = part
		r := rigScratch.render(pose)
		rigScratch.onlyPart = 0
		if r.N == 0 {
			continue
		}
		cx, cy := 0.0, 0.0
		for i := range r.N {
			cx += float64(r.Xs[i])
			cy += float64(r.Ys[i])
		}
		out = append(out, PosedPart{Name: partNames[part], Part: int(part), N: r.N, Xs: r.Xs, Ys: r.Ys, Color: r.Color, CX: cx / float64(r.N), CY: cy / float64(r.N), Z: r.Z})
	}
	return out
}
