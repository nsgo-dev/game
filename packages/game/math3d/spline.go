package math3d

import "math"

// CurveFrame is an orthonormal right-handed reference frame attached to a 3D curve.
//
// Following math3d conventions, Tangent points forward along the curve (local -Z),
// Up points along local +Y, and Right points along local +X (Right = Tangent × Up).
// The basis matrix columns [Right, Up, -Tangent] form a proper right-handed rotation
// with determinant +1.
type CurveFrame struct {
	Position Vec3
	Tangent  Vec3
	Up       Vec3
	Right    Vec3
}

// Rotation returns the unit quaternion orienting local +X to Right, local +Y to Up,
// and local -Z (Vec3Forward) to Tangent.
func (f CurveFrame) Rotation() Quat {
	if f.Tangent.LenSq() < 1e-12 && f.Up.LenSq() < 1e-12 && f.Right.LenSq() < 1e-12 {
		return QuatIdentity()
	}
	return QuatFromMat3(Mat3FromCols(f.Right, f.Up, f.Tangent.Neg()))
}

// Transform returns the rigid TRS transform placing an object at Position oriented
// along the frame (with unit scale).
func (f CurveFrame) Transform() Transform {
	return Transform{
		Pos:   f.Position,
		Rot:   f.Rotation(),
		Scale: Vec3One(),
	}
}

// Mat4 returns the column-major 4x4 rigid transform matrix for the frame,
// equivalent to f.Transform().ToMat4().
func (f CurveFrame) Mat4() Mat4 {
	return Mat4{
		f.Right.X, f.Right.Y, f.Right.Z, 0,
		f.Up.X, f.Up.Y, f.Up.Z, 0,
		-f.Tangent.X, -f.Tangent.Y, -f.Tangent.Z, 0,
		f.Position.X, f.Position.Y, f.Position.Z, 1,
	}
}

// Curve3D is a parameterized 3D curve supporting arc-length reparameterization
// and smooth Rotation-Minimizing Frames (RMF).
//
// Parameter t lies in [0, 1]. Open curves clamp out-of-range t and dist values;
// closed curves wrap out-of-range values around the loop.
type Curve3D interface {
	// Point evaluates the 3D position at parameter t in [0, 1].
	Point(t float32) Vec3
	// Tangent evaluates the unit forward tangent vector at parameter t in [0, 1].
	Tangent(t float32) Vec3
	// Length returns the total arc length of the curve.
	Length() float32
	// DistanceToT maps an arc-length distance in [0, Length()] to parameter t in [0, 1].
	DistanceToT(dist float32) float32
	// PointAtDistance evaluates the 3D position at arc-length distance dist.
	PointAtDistance(dist float32) Vec3
	// TangentAtDistance evaluates the unit forward tangent at arc-length distance dist.
	TangentAtDistance(dist float32) Vec3
	// FrameAt evaluates a smooth Rotation-Minimizing Frame at parameter t in [0, 1],
	// initialized at t=0 with worldUp (or Vec3Up if worldUp is zero).
	FrameAt(t float32, worldUp Vec3) CurveFrame
	// FrameAtDistance evaluates the Rotation-Minimizing Frame at arc-length distance dist
	// using default world up (Vec3Up).
	FrameAtDistance(dist float32) CurveFrame
	// Closed reports whether the curve forms a closed loop.
	Closed() bool
}

// Compile-time interface checks.
var (
	_ Curve3D = (*QuadraticBezier3D)(nil)
	_ Curve3D = (*CubicBezier3D)(nil)
	_ Curve3D = (*CubicHermite3D)(nil)
	_ Curve3D = (*HermiteSpline3D)(nil)
	_ Curve3D = (*CatmullRom3D)(nil)
)

// 5-point Gauss-Legendre quadrature nodes and weights on [-1, 1].
// Exact for polynomials up to degree 9.
var (
	gl5Nodes = [5]float64{
		-0.9061798459386640,
		-0.5384693101056831,
		0.0,
		0.5384693101056831,
		0.9061798459386640,
	}
	gl5Weights = [5]float64{
		0.23692688505618908,
		0.47862867049936647,
		0.5688888888888889,
		0.47862867049936647,
		0.23692688505618908,
	}
)

// cubicPoly3D represents a 3D polynomial segment P(u) = ((a*u + b)*u + c)*u + d for u in [0, 1].
// Quadratic curves are represented with a = 0.
type cubicPoly3D struct {
	a, b, c, d Vec3
}

func (p cubicPoly3D) point(u float32) Vec3 {
	return Vec3{
		X: ((p.a.X*u+p.b.X)*u+p.c.X)*u + p.d.X,
		Y: ((p.a.Y*u+p.b.Y)*u+p.c.Y)*u + p.d.Y,
		Z: ((p.a.Z*u+p.b.Z)*u+p.c.Z)*u + p.d.Z,
	}
}

func (p cubicPoly3D) deriv(u float32) Vec3 {
	return Vec3{
		X: (3*p.a.X*u+2*p.b.X)*u + p.c.X,
		Y: (3*p.a.Y*u+2*p.b.Y)*u + p.c.Y,
		Z: (3*p.a.Z*u+2*p.b.Z)*u + p.c.Z,
	}
}

func (p cubicPoly3D) speed64(u float64) float64 {
	dx := (3*float64(p.a.X)*u+2*float64(p.b.X))*u + float64(p.c.X)
	dy := (3*float64(p.a.Y)*u+2*float64(p.b.Y))*u + float64(p.c.Y)
	dz := (3*float64(p.a.Z)*u+2*float64(p.b.Z))*u + float64(p.c.Z)
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func integrateCubic(seg cubicPoly3D, u0, u1 float64) float64 {
	mid := 0.5 * (u0 + u1)
	half := 0.5 * (u1 - u0)
	var sum float64
	for i := range 5 {
		sum += gl5Weights[i] * seg.speed64(mid+half*gl5Nodes[i])
	}
	return half * sum
}

func cubicFromHermite(p0, m0, p1, m1 Vec3) cubicPoly3D {
	p0x, p0y, p0z := float64(p0.X), float64(p0.Y), float64(p0.Z)
	m0x, m0y, m0z := float64(m0.X), float64(m0.Y), float64(m0.Z)
	p1x, p1y, p1z := float64(p1.X), float64(p1.Y), float64(p1.Z)
	m1x, m1y, m1z := float64(m1.X), float64(m1.Y), float64(m1.Z)
	return cubicPoly3D{
		a: Vec3{
			X: float32(2*p0x - 2*p1x + m0x + m1x),
			Y: float32(2*p0y - 2*p1y + m0y + m1y),
			Z: float32(2*p0z - 2*p1z + m0z + m1z),
		},
		b: Vec3{
			X: float32(-3*p0x + 3*p1x - 2*m0x - m1x),
			Y: float32(-3*p0y + 3*p1y - 2*m0y - m1y),
			Z: float32(-3*p0z + 3*p1z - 2*m0z - m1z),
		},
		c: m0,
		d: p0,
	}
}

func cubicFromBezier(p0, p1, p2, p3 Vec3) cubicPoly3D {
	p0x, p0y, p0z := float64(p0.X), float64(p0.Y), float64(p0.Z)
	p1x, p1y, p1z := float64(p1.X), float64(p1.Y), float64(p1.Z)
	p2x, p2y, p2z := float64(p2.X), float64(p2.Y), float64(p2.Z)
	p3x, p3y, p3z := float64(p3.X), float64(p3.Y), float64(p3.Z)
	return cubicPoly3D{
		a: Vec3{
			X: float32(-p0x + 3*p1x - 3*p2x + p3x),
			Y: float32(-p0y + 3*p1y - 3*p2y + p3y),
			Z: float32(-p0z + 3*p1z - 3*p2z + p3z),
		},
		b: Vec3{
			X: float32(3*p0x - 6*p1x + 3*p2x),
			Y: float32(3*p0y - 6*p1y + 3*p2y),
			Z: float32(3*p0z - 6*p1z + 3*p2z),
		},
		c: Vec3{
			X: float32(-3*p0x + 3*p1x),
			Y: float32(-3*p0y + 3*p1y),
			Z: float32(-3*p0z + 3*p1z),
		},
		d: p0,
	}
}

func cubicFromQuadBezier(p0, p1, p2 Vec3) cubicPoly3D {
	p0x, p0y, p0z := float64(p0.X), float64(p0.Y), float64(p0.Z)
	p1x, p1y, p1z := float64(p1.X), float64(p1.Y), float64(p1.Z)
	p2x, p2y, p2z := float64(p2.X), float64(p2.Y), float64(p2.Z)
	return cubicPoly3D{
		b: Vec3{
			X: float32(p0x - 2*p1x + p2x),
			Y: float32(p0y - 2*p1y + p2y),
			Z: float32(p0z - 2*p1z + p2z),
		},
		c: Vec3{
			X: float32(2 * (p1x - p0x)),
			Y: float32(2 * (p1y - p0y)),
			Z: float32(2 * (p1z - p0z)),
		},
		d: p0,
	}
}

// curveSample stores a precomputed arc-length and Rotation-Minimizing Frame sample.
type curveSample struct {
	t   float32
	s   float32
	pos Vec3
	tan Vec3
	up  Vec3
}

// splineCore implements piecewise-cubic evaluation, Gauss-Legendre arc-length LUT
// with Newton-Raphson inversion, and Wang et al. Double Reflection RMF transport.
type splineCore struct {
	segments        []cubicPoly3D
	samplesPerSeg   int
	table           []curveSample
	length          float32
	closed          bool
	twistCorrection float32
	fallbackPoint   Vec3
}

func newSplineCore(segments []cubicPoly3D, closed bool, fallbackPoint Vec3) splineCore {
	core := splineCore{
		segments:      segments,
		closed:        closed,
		fallbackPoint: fallbackPoint,
	}
	core.buildTable()
	return core
}

func (c *splineCore) buildTable() {
	nSeg := len(c.segments)
	if nSeg == 0 {
		return
	}
	if nSeg == 1 {
		c.samplesPerSeg = 64
	} else {
		c.samplesPerSeg = 32
	}
	totalSteps := nSeg * c.samplesPerSeg
	table := make([]curveSample, totalSteps+1)

	var cumLen float64
	table[0] = curveSample{
		t:   0,
		s:   0,
		pos: c.segments[0].point(0),
		tan: c.tangentNorm(0),
	}

	invSub := 1.0 / float64(c.samplesPerSeg)
	invTotal := 1.0 / float64(totalSteps)

	for segIdx := range nSeg {
		seg := c.segments[segIdx]
		for sub := 1; sub <= c.samplesPerSeg; sub++ {
			idx := segIdx*c.samplesPerSeg + sub
			u0 := float64(sub-1) * invSub
			u1 := float64(sub) * invSub
			cumLen += integrateCubic(seg, u0, u1)
			t := float32(float64(idx) * invTotal)
			if idx == totalSteps {
				t = 1
			}
			table[idx] = curveSample{
				t:   t,
				s:   float32(cumLen),
				pos: seg.point(float32(u1)),
				tan: c.tangentNorm(t),
			}
		}
	}

	c.length = float32(cumLen)
	c.table = table

	if c.length <= 0 || table[0].tan.LenSq() < 1e-12 {
		return
	}

	// Build Rotation-Minimizing Frames via Double Reflection parallel transport.
	table[0].up = orthoUp(table[0].tan, Vec3Up())
	for i := range totalSteps {
		if table[i+1].tan.LenSq() < 1e-12 {
			table[i+1].tan = table[i].tan
		}
		table[i+1].up = doubleReflectionStep(
			table[i].pos, table[i].tan, table[i].up,
			table[i+1].pos, table[i+1].tan,
		)
	}

	// For closed curves, compute holonomy twist around the loop so FrameAt(1) matches FrameAt(0).
	if c.closed {
		t0 := table[0].tan
		u0 := table[0].up
		r0 := t0.Cross(u0)
		uEnd := table[totalSteps].up
		uEnd = uEnd.Sub(t0.Scale(t0.Dot(uEnd)))
		if uEnd.LenSq() > 1e-12 {
			uEnd = uEnd.Norm()
			cosPhi := float64(Clamp(uEnd.Dot(u0), -1, 1))
			sinPhi := float64(uEnd.Dot(r0))
			c.twistCorrection = -float32(math.Atan2(sinPhi, cosPhi))
		}
	}
}

func (c *splineCore) normalizeT(t float32) float32 {
	if c.closed {
		if t < 0 || t > 1 {
			t = float32(float64(t) - math.Floor(float64(t)))
			if t >= 1 {
				t = 0
			}
		}
		return t
	}
	return Clamp(t, 0, 1)
}

func (c *splineCore) locateSegment(t float32) (int, float32) {
	n := len(c.segments)
	if n == 1 {
		return 0, t
	}
	if t <= 0 {
		return 0, 0
	}
	if t >= 1 {
		return n - 1, 1
	}
	scaled := t * float32(n)
	seg := int(scaled)
	if seg >= n {
		return n - 1, 1
	}
	return seg, scaled - float32(seg)
}

func (c *splineCore) pointNorm(t float32) Vec3 {
	if len(c.segments) == 0 {
		return c.fallbackPoint
	}
	seg, u := c.locateSegment(t)
	return c.segments[seg].point(u)
}

func (c *splineCore) derivNorm(t float32) Vec3 {
	if len(c.segments) == 0 {
		return Vec3Zero()
	}
	seg, u := c.locateSegment(t)
	d := c.segments[seg].deriv(u)
	if len(c.segments) > 1 {
		return d.Scale(float32(len(c.segments)))
	}
	return d
}

func (c *splineCore) tangentNorm(t float32) Vec3 {
	if len(c.segments) == 0 {
		return Vec3Zero()
	}
	d := c.derivNorm(t)
	if d.LenSq() > 1e-12 {
		return d.Norm()
	}
	const step = 1e-3
	if t+step <= 1 {
		if d2 := c.derivNorm(t + step); d2.LenSq() > 1e-12 {
			return d2.Norm()
		}
	}
	if t-step >= 0 {
		if d2 := c.derivNorm(t - step); d2.LenSq() > 1e-12 {
			return d2.Norm()
		}
	}
	t0 := Clamp(t-step, 0, 1)
	t1 := Clamp(t+step, 0, 1)
	if chord := c.pointNorm(t1).Sub(c.pointNorm(t0)); chord.LenSq() > 1e-12 {
		return chord.Norm()
	}
	return Vec3Zero()
}

func (c *splineCore) point(t float32) Vec3 {
	if len(c.segments) == 0 {
		return c.fallbackPoint
	}
	return c.pointNorm(c.normalizeT(t))
}

func (c *splineCore) derivative(t float32) Vec3 {
	if len(c.segments) == 0 {
		return Vec3Zero()
	}
	return c.derivNorm(c.normalizeT(t))
}

func (c *splineCore) tangent(t float32) Vec3 {
	if len(c.segments) == 0 {
		return Vec3Zero()
	}
	return c.tangentNorm(c.normalizeT(t))
}

func (c *splineCore) distanceToT(dist float32) float32 {
	if len(c.segments) == 0 || c.length <= 0 {
		return 0
	}
	if c.closed {
		if dist == c.length {
			return 1
		}
		if dist < 0 || dist > c.length {
			d64 := math.Mod(float64(dist), float64(c.length))
			if d64 < 0 {
				d64 += float64(c.length)
			}
			dist = float32(d64)
		}
		if dist <= 0 {
			return 0
		}
		if dist >= c.length {
			return 1
		}
	} else {
		if dist <= 0 {
			return 0
		}
		if dist >= c.length {
			return 1
		}
	}

	// Binary search for the bracketing interval [lo, lo+1] in the LUT.
	lo, hi := 0, len(c.table)-1
	for lo+1 < hi {
		mid := int(uint(lo+hi) >> 1)
		if c.table[mid].s <= dist {
			lo = mid
		} else {
			hi = mid
		}
	}

	s0 := float64(c.table[lo].s)
	s1 := float64(c.table[lo+1].s)
	ds := s1 - s0

	segIdx := lo / c.samplesPerSeg
	sub := lo - segIdx*c.samplesPerSeg
	invSub := 1.0 / float64(c.samplesPerSeg)
	u0 := float64(sub) * invSub
	u1 := float64(sub+1) * invSub

	if ds <= 1e-12 {
		return float32((float64(segIdx) + u0) / float64(len(c.segments)))
	}

	targetDelta := float64(dist) - s0
	u := u0 + (u1-u0)*(targetDelta/ds)
	uLo, uHi := u0, u1
	seg := c.segments[segIdx]

	tol := 1e-6 * float64(c.length)
	if tol < 1e-9 {
		tol = 1e-9
	}

	// Safeguarded Newton-Raphson refinement using 5-point Gauss-Legendre integration.
	for range 6 {
		curDelta := integrateCubic(seg, u0, u)
		err := curDelta - targetDelta
		if math.Abs(err) <= tol {
			break
		}
		if err < 0 {
			uLo = u
		} else {
			uHi = u
		}
		speed := seg.speed64(u)
		var nextU float64
		if speed > 1e-9 {
			nextU = u - err/speed
		} else {
			nextU = 0.5 * (uLo + uHi)
		}
		if nextU <= uLo || nextU >= uHi {
			nextU = 0.5 * (uLo + uHi)
		}
		if math.Abs(nextU-u) < 1e-7 {
			u = nextU
			break
		}
		u = nextU
	}

	t := float32((float64(segIdx) + u) / float64(len(c.segments)))
	return Clamp(t, 0, 1)
}

func (c *splineCore) frameAt(t float32, worldUp Vec3) CurveFrame {
	if len(c.segments) == 0 {
		up := orthoUp(Vec3Forward(), worldUp)
		return CurveFrame{
			Position: c.fallbackPoint,
			Tangent:  Vec3Forward(),
			Up:       up,
			Right:    Vec3Forward().Cross(up),
		}
	}
	t = c.normalizeT(t)
	pos := c.pointNorm(t)
	tan := c.tangentNorm(t)
	if tan.LenSq() < 1e-12 || c.length <= 0 {
		up := orthoUp(Vec3Forward(), worldUp)
		return CurveFrame{
			Position: pos,
			Tangent:  Vec3Forward(),
			Up:       up,
			Right:    Vec3Forward().Cross(up),
		}
	}

	totalSteps := len(c.table) - 1
	scaled := t * float32(totalSteps)
	k := int(scaled)
	if k >= totalSteps {
		k = totalSteps
	}
	if k < 0 {
		k = 0
	}

	sample := c.table[k]
	var u Vec3
	if k == totalSteps || scaled == float32(k) {
		u = sample.up.Sub(tan.Scale(tan.Dot(sample.up)))
		if u.LenSq() < 1e-12 {
			u = orthoUp(tan, sample.up)
		} else {
			u = u.Norm()
		}
	} else {
		u = doubleReflectionStep(sample.pos, sample.tan, sample.up, pos, tan)
	}
	r := tan.Cross(u)

	// Because Double Reflection parallel transport is linear and angle-preserving
	// between normal planes, changing the initial worldUp at t=0 is exact via a
	// constant 2D rotation in the local (Up, Right) normal plane.
	cRot, sRot := float32(1), float32(0)
	if worldUp.LenSq() > 1e-12 {
		baseU0 := c.table[0].up
		baseR0 := c.table[0].tan.Cross(baseU0)
		wantU0 := orthoUp(c.table[0].tan, worldUp)
		cRot = wantU0.Dot(baseU0)
		sRot = wantU0.Dot(baseR0)
	}

	if c.closed && c.twistCorrection != 0 {
		sTw, cTw := math.Sincos(float64(c.twistCorrection * t))
		ct, st := float32(cTw), float32(sTw)
		cRot, sRot = cRot*ct-sRot*st, sRot*ct+cRot*st
	}

	if cRot != 1 || sRot != 0 {
		u = u.Scale(cRot).Add(r.Scale(sRot)).Norm()
		r = tan.Cross(u)
	}

	return CurveFrame{
		Position: pos,
		Tangent:  tan,
		Up:       u,
		Right:    r,
	}
}

// orthoUp returns a unit vector orthogonal to unit tangent tan, aligned as closely
// as possible with up (defaulting to Vec3Up if up is zero, or a deterministic
// fallback axis when tan is parallel to up).
func orthoUp(tan, up Vec3) Vec3 {
	if up.LenSq() < 1e-12 {
		up = Vec3Up()
	}
	proj := up.Sub(tan.Scale(up.Dot(tan)))
	if proj.LenSq() < 1e-10 {
		fallback := Vec3{0, 0, 1}
		if absf(tan.Z) > 0.9 {
			fallback = Vec3{1, 0, 0}
		}
		proj = fallback.Sub(tan.Scale(fallback.Dot(tan)))
	}
	return proj.Norm()
}

// doubleReflectionStep advances a Rotation-Minimizing Frame up vector u0 from
// (x0, t0) to (x1, t1) using the Double Reflection method (Wang et al. 2008).
func doubleReflectionStep(x0, t0, u0, x1, t1 Vec3) Vec3 {
	v1 := x1.Sub(x0)
	c1 := v1.LenSq()
	var u1 Vec3
	if c1 < 1e-12 {
		u1 = shortestArcTransport(t0, t1, u0)
	} else {
		invC1 := 2 / c1
		uL := u0.Sub(v1.Scale(invC1 * v1.Dot(u0)))
		tL := t0.Sub(v1.Scale(invC1 * v1.Dot(t0)))

		v2 := t1.Sub(tL)
		c2 := v2.LenSq()
		if c2 < 1e-12 {
			u1 = uL
		} else {
			u1 = uL.Sub(v2.Scale((2 / c2) * v2.Dot(uL)))
		}
	}
	u1 = u1.Sub(t1.Scale(t1.Dot(u1)))
	if u1.LenSq() < 1e-12 {
		return orthoUp(t1, u0)
	}
	return u1.Norm()
}

func shortestArcTransport(t0, t1, u0 Vec3) Vec3 {
	dot := t0.Dot(t1)
	if dot > 0.999999 {
		return u0
	}
	axis := t0.Cross(t1)
	if axis.LenSq() < 1e-12 {
		return u0
	}
	q := Quat{X: axis.X, Y: axis.Y, Z: axis.Z, W: 1 + dot}.Norm()
	return q.RotateVec3(u0)
}

// QuadraticBezier3D is a 3D quadratic Bézier curve defined by three control points.
type QuadraticBezier3D struct {
	core splineCore
}

// NewQuadraticBezier3D constructs a 3D quadratic Bézier curve from p0 through
// control point p1 to p2, precomputing its Gauss-Legendre arc-length LUT and
// Rotation-Minimizing Frames.
func NewQuadraticBezier3D(p0, p1, p2 Vec3) *QuadraticBezier3D {
	segs := []cubicPoly3D{cubicFromQuadBezier(p0, p1, p2)}
	return &QuadraticBezier3D{core: newSplineCore(segs, false, p0)}
}

// Point evaluates the curve position at parameter t in [0, 1].
func (c *QuadraticBezier3D) Point(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(t)
}

// Derivative evaluates the unnormalized first derivative dP/dt at parameter t in [0, 1].
func (c *QuadraticBezier3D) Derivative(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.derivative(t)
}

// Tangent evaluates the unit tangent vector at parameter t in [0, 1].
func (c *QuadraticBezier3D) Tangent(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(t)
}

// Length returns the total arc length of the curve.
func (c *QuadraticBezier3D) Length() float32 {
	if c == nil {
		return 0
	}
	return c.core.length
}

// DistanceToT maps an arc-length distance in [0, Length()] to parameter t in [0, 1].
func (c *QuadraticBezier3D) DistanceToT(dist float32) float32 {
	if c == nil {
		return 0
	}
	return c.core.distanceToT(dist)
}

// PointAtDistance evaluates the curve position at arc-length distance dist.
func (c *QuadraticBezier3D) PointAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(c.core.distanceToT(dist))
}

// TangentAtDistance evaluates the unit tangent vector at arc-length distance dist.
func (c *QuadraticBezier3D) TangentAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(c.core.distanceToT(dist))
}

// FrameAt evaluates the Rotation-Minimizing Frame at parameter t in [0, 1],
// initialized with worldUp at t=0.
func (c *QuadraticBezier3D) FrameAt(t float32, worldUp Vec3) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(t, worldUp)
}

// FrameAtDistance evaluates the Rotation-Minimizing Frame at arc-length distance dist.
func (c *QuadraticBezier3D) FrameAtDistance(dist float32) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(c.core.distanceToT(dist), Vec3Up())
}

// Closed reports false because a single quadratic Bézier segment is open.
func (c *QuadraticBezier3D) Closed() bool { return false }

// CubicBezier3D is a 3D cubic Bézier curve defined by four control points.
type CubicBezier3D struct {
	core splineCore
}

// NewCubicBezier3D constructs a 3D cubic Bézier curve from p0 to p3 with control
// handles p1 and p2, precomputing its Gauss-Legendre arc-length LUT and
// Rotation-Minimizing Frames.
func NewCubicBezier3D(p0, p1, p2, p3 Vec3) *CubicBezier3D {
	segs := []cubicPoly3D{cubicFromBezier(p0, p1, p2, p3)}
	return &CubicBezier3D{core: newSplineCore(segs, false, p0)}
}

// Point evaluates the curve position at parameter t in [0, 1].
func (c *CubicBezier3D) Point(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(t)
}

// Derivative evaluates the unnormalized first derivative dP/dt at parameter t in [0, 1].
func (c *CubicBezier3D) Derivative(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.derivative(t)
}

// Tangent evaluates the unit tangent vector at parameter t in [0, 1].
func (c *CubicBezier3D) Tangent(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(t)
}

// Length returns the total arc length of the curve.
func (c *CubicBezier3D) Length() float32 {
	if c == nil {
		return 0
	}
	return c.core.length
}

// DistanceToT maps an arc-length distance in [0, Length()] to parameter t in [0, 1].
func (c *CubicBezier3D) DistanceToT(dist float32) float32 {
	if c == nil {
		return 0
	}
	return c.core.distanceToT(dist)
}

// PointAtDistance evaluates the curve position at arc-length distance dist.
func (c *CubicBezier3D) PointAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(c.core.distanceToT(dist))
}

// TangentAtDistance evaluates the unit tangent vector at arc-length distance dist.
func (c *CubicBezier3D) TangentAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(c.core.distanceToT(dist))
}

// FrameAt evaluates the Rotation-Minimizing Frame at parameter t in [0, 1],
// initialized with worldUp at t=0.
func (c *CubicBezier3D) FrameAt(t float32, worldUp Vec3) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(t, worldUp)
}

// FrameAtDistance evaluates the Rotation-Minimizing Frame at arc-length distance dist.
func (c *CubicBezier3D) FrameAtDistance(dist float32) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(c.core.distanceToT(dist), Vec3Up())
}

// Closed reports false because a single cubic Bézier segment is open.
func (c *CubicBezier3D) Closed() bool { return false }

// CubicHermite3D is a single-segment 3D cubic Hermite curve defined by start and
// end positions and their parametric endpoint tangents.
type CubicHermite3D struct {
	core splineCore
}

// NewCubicHermite3D constructs a 3D cubic Hermite curve starting at p0 with
// derivative m0 and ending at p1 with derivative m1, precomputing its
// Gauss-Legendre arc-length LUT and Rotation-Minimizing Frames.
func NewCubicHermite3D(p0, m0, p1, m1 Vec3) *CubicHermite3D {
	segs := []cubicPoly3D{cubicFromHermite(p0, m0, p1, m1)}
	return &CubicHermite3D{core: newSplineCore(segs, false, p0)}
}

// Point evaluates the curve position at parameter t in [0, 1].
func (c *CubicHermite3D) Point(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(t)
}

// Derivative evaluates the unnormalized first derivative dP/dt at parameter t in [0, 1].
// At t=0 this equals m0; at t=1 this equals m1.
func (c *CubicHermite3D) Derivative(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.derivative(t)
}

// Tangent evaluates the unit tangent vector at parameter t in [0, 1].
func (c *CubicHermite3D) Tangent(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(t)
}

// Length returns the total arc length of the curve.
func (c *CubicHermite3D) Length() float32 {
	if c == nil {
		return 0
	}
	return c.core.length
}

// DistanceToT maps an arc-length distance in [0, Length()] to parameter t in [0, 1].
func (c *CubicHermite3D) DistanceToT(dist float32) float32 {
	if c == nil {
		return 0
	}
	return c.core.distanceToT(dist)
}

// PointAtDistance evaluates the curve position at arc-length distance dist.
func (c *CubicHermite3D) PointAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(c.core.distanceToT(dist))
}

// TangentAtDistance evaluates the unit tangent vector at arc-length distance dist.
func (c *CubicHermite3D) TangentAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(c.core.distanceToT(dist))
}

// FrameAt evaluates the Rotation-Minimizing Frame at parameter t in [0, 1],
// initialized with worldUp at t=0.
func (c *CubicHermite3D) FrameAt(t float32, worldUp Vec3) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(t, worldUp)
}

// FrameAtDistance evaluates the Rotation-Minimizing Frame at arc-length distance dist.
func (c *CubicHermite3D) FrameAtDistance(dist float32) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(c.core.distanceToT(dist), Vec3Up())
}

// Closed reports false because a single cubic Hermite segment is open.
func (c *CubicHermite3D) Closed() bool { return false }

// HermiteSpline3D is a C1-continuous multi-segment 3D cubic Hermite spline.
type HermiteSpline3D struct {
	core splineCore
}

// NewHermiteSpline3D constructs a multi-segment cubic Hermite spline interpolating
// points with corresponding per-point tangents.
//
// When closed is false, len(points)-1 segments connect points[0] through points[n-1].
// When closed is true, len(points) segments form a closed loop back to points[0].
// Missing tangent entries (if len(tangents) < len(points)) default to Vec3Zero.
// If len(points) < 2, an empty zero-length spline is returned.
func NewHermiteSpline3D(points, tangents []Vec3, closed bool) *HermiteSpline3D {
	n := len(points)
	if n == 0 {
		return &HermiteSpline3D{core: newSplineCore(nil, closed, Vec3Zero())}
	}
	if n == 1 {
		return &HermiteSpline3D{core: newSplineCore(nil, closed, points[0])}
	}

	tangentAt := func(i int) Vec3 {
		if i < len(tangents) {
			return tangents[i]
		}
		return Vec3Zero()
	}

	nSeg := n - 1
	if closed {
		nSeg = n
	}
	segs := make([]cubicPoly3D, nSeg)
	for i := range nSeg {
		j := (i + 1) % n
		segs[i] = cubicFromHermite(points[i], tangentAt(i), points[j], tangentAt(j))
	}
	return &HermiteSpline3D{core: newSplineCore(segs, closed, points[0])}
}

// SegmentCount returns the number of cubic segments in the spline.
func (c *HermiteSpline3D) SegmentCount() int {
	if c == nil {
		return 0
	}
	return len(c.core.segments)
}

// Point evaluates the spline position at parameter t in [0, 1].
func (c *HermiteSpline3D) Point(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(t)
}

// Derivative evaluates the unnormalized first derivative dP/dt at parameter t in [0, 1].
func (c *HermiteSpline3D) Derivative(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.derivative(t)
}

// Tangent evaluates the unit tangent vector at parameter t in [0, 1].
func (c *HermiteSpline3D) Tangent(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(t)
}

// Length returns the total arc length of the spline.
func (c *HermiteSpline3D) Length() float32 {
	if c == nil {
		return 0
	}
	return c.core.length
}

// DistanceToT maps an arc-length distance to parameter t in [0, 1].
func (c *HermiteSpline3D) DistanceToT(dist float32) float32 {
	if c == nil {
		return 0
	}
	return c.core.distanceToT(dist)
}

// PointAtDistance evaluates the spline position at arc-length distance dist.
func (c *HermiteSpline3D) PointAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(c.core.distanceToT(dist))
}

// TangentAtDistance evaluates the unit tangent vector at arc-length distance dist.
func (c *HermiteSpline3D) TangentAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(c.core.distanceToT(dist))
}

// FrameAt evaluates the Rotation-Minimizing Frame at parameter t in [0, 1],
// initialized with worldUp at t=0.
func (c *HermiteSpline3D) FrameAt(t float32, worldUp Vec3) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(t, worldUp)
}

// FrameAtDistance evaluates the Rotation-Minimizing Frame at arc-length distance dist.
func (c *HermiteSpline3D) FrameAtDistance(dist float32) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(c.core.distanceToT(dist), Vec3Up())
}

// Closed reports whether the spline forms a closed loop.
func (c *HermiteSpline3D) Closed() bool {
	if c == nil {
		return false
	}
	return c.core.closed
}

// CatmullRom3D is a multi-segment interpolating 3D Catmull-Rom spline with
// configurable Barry-Goldman knot parameterization (alpha in [0, 1]).
//
// Parameter t in [0, 1] maps uniformly across segments so control point i lies at
// t = float32(i) / float32(SegmentCount()).
type CatmullRom3D struct {
	core  splineCore
	alpha float32
}

// NewCatmullRom3D constructs a centripetal (alpha = 0.5) 3D Catmull-Rom spline
// interpolating all points. Centripetal parameterization prevents cusps and
// self-intersections within segments.
func NewCatmullRom3D(points []Vec3, closed bool) *CatmullRom3D {
	return NewCatmullRom3DAlpha(points, 0.5, closed)
}

// NewCatmullRom3DAlpha constructs a 3D Catmull-Rom spline interpolating all points
// with knot parameterization exponent alpha clamped to [0, 1]:
//   - alpha = 0.0: uniform
//   - alpha = 0.5: centripetal (default)
//   - alpha = 1.0: chordal
//
// Open splines synthesize reflected ghost endpoints so the spline starts at
// points[0] and ends at points[len(points)-1]. Closed splines wrap seamlessly
// through all points. If len(points) < 2, an empty zero-length spline is returned.
func NewCatmullRom3DAlpha(points []Vec3, alpha float32, closed bool) *CatmullRom3D {
	alpha = Clamp(alpha, 0, 1)
	n := len(points)
	if n == 0 {
		return &CatmullRom3D{core: newSplineCore(nil, closed, Vec3Zero()), alpha: alpha}
	}
	if n == 1 {
		return &CatmullRom3D{core: newSplineCore(nil, closed, points[0]), alpha: alpha}
	}

	nSeg := n - 1
	if closed {
		nSeg = n
	}

	pointAt := func(i int) Vec3 {
		if closed {
			idx := ((i % n) + n) % n
			return points[idx]
		}
		if i < 0 {
			// Reflected ghost start point: P_{-1} = 2*P_0 - P_1.
			return points[0].Scale(2).Sub(points[1])
		}
		if i >= n {
			// Reflected ghost end point: P_n = 2*P_{n-1} - P_{n-2}.
			return points[n-1].Scale(2).Sub(points[n-2])
		}
		return points[i]
	}

	segs := make([]cubicPoly3D, nSeg)
	for i := range nSeg {
		p0 := pointAt(i - 1)
		p1 := pointAt(i)
		p2 := pointAt(i + 1)
		p3 := pointAt(i + 2)
		segs[i] = cubicFromCatmullRom(p0, p1, p2, p3, float64(alpha))
	}

	return &CatmullRom3D{
		core:  newSplineCore(segs, closed, points[0]),
		alpha: alpha,
	}
}

// cubicFromCatmullRom converts a generalized Barry-Goldman Catmull-Rom segment
// interpolating p1 -> p2 with neighbors (p0, p3) and knot exponent alpha into
// its equivalent cubic Hermite polynomial on u in [0, 1].
func cubicFromCatmullRom(p0, p1, p2, p3 Vec3, alpha float64) cubicPoly3D {
	d1 := float64(p2.Sub(p1).Len())
	if d1 < 1e-6 {
		return cubicPoly3D{d: p1}
	}
	d0 := float64(p1.Sub(p0).Len())
	d2 := float64(p3.Sub(p2).Len())

	dt0 := knotInterval(d0, alpha)
	dt1 := knotInterval(d1, alpha)
	dt2 := knotInterval(d2, alpha)

	var m1, m2 Vec3
	if dt0 < 1e-6 {
		m1 = p2.Sub(p1)
	} else {
		// m1 = dt1 * [(dt1/(dt0+dt1))*(p1-p0)/dt0 + (dt0/(dt0+dt1))*(p2-p1)/dt1]
		w0 := float32((dt1 * dt1) / (dt0 * (dt0 + dt1)))
		w1 := float32(dt0 / (dt0 + dt1))
		m1 = p1.Sub(p0).Scale(w0).Add(p2.Sub(p1).Scale(w1))
	}

	if dt2 < 1e-6 {
		m2 = p2.Sub(p1)
	} else {
		// m2 = dt1 * [(dt2/(dt1+dt2))*(p2-p1)/dt1 + (dt1/(dt1+dt2))*(p3-p2)/dt2]
		w1 := float32(dt2 / (dt1 + dt2))
		w2 := float32((dt1 * dt1) / (dt2 * (dt1 + dt2)))
		m2 = p2.Sub(p1).Scale(w1).Add(p3.Sub(p2).Scale(w2))
	}

	return cubicFromHermite(p1, m1, p2, m2)
}

func knotInterval(dist, alpha float64) float64 {
	if dist < 1e-6 {
		return 0
	}
	if alpha == 0 {
		return 1
	}
	if alpha == 0.5 {
		return math.Sqrt(dist)
	}
	if alpha == 1 {
		return dist
	}
	return math.Pow(dist, alpha)
}

// Alpha returns the knot parameterization exponent in [0, 1].
func (c *CatmullRom3D) Alpha() float32 {
	if c == nil {
		return 0
	}
	return c.alpha
}

// SegmentCount returns the number of cubic segments in the spline.
func (c *CatmullRom3D) SegmentCount() int {
	if c == nil {
		return 0
	}
	return len(c.core.segments)
}

// Point evaluates the spline position at parameter t in [0, 1].
func (c *CatmullRom3D) Point(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(t)
}

// Derivative evaluates the unnormalized first derivative dP/dt at parameter t in [0, 1].
func (c *CatmullRom3D) Derivative(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.derivative(t)
}

// Tangent evaluates the unit tangent vector at parameter t in [0, 1].
func (c *CatmullRom3D) Tangent(t float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(t)
}

// Length returns the total arc length of the spline.
func (c *CatmullRom3D) Length() float32 {
	if c == nil {
		return 0
	}
	return c.core.length
}

// DistanceToT maps an arc-length distance to parameter t in [0, 1].
func (c *CatmullRom3D) DistanceToT(dist float32) float32 {
	if c == nil {
		return 0
	}
	return c.core.distanceToT(dist)
}

// PointAtDistance evaluates the spline position at arc-length distance dist.
func (c *CatmullRom3D) PointAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.point(c.core.distanceToT(dist))
}

// TangentAtDistance evaluates the unit tangent vector at arc-length distance dist.
func (c *CatmullRom3D) TangentAtDistance(dist float32) Vec3 {
	if c == nil {
		return Vec3Zero()
	}
	return c.core.tangent(c.core.distanceToT(dist))
}

// FrameAt evaluates the Rotation-Minimizing Frame at parameter t in [0, 1],
// initialized with worldUp at t=0.
func (c *CatmullRom3D) FrameAt(t float32, worldUp Vec3) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(t, worldUp)
}

// FrameAtDistance evaluates the Rotation-Minimizing Frame at arc-length distance dist.
func (c *CatmullRom3D) FrameAtDistance(dist float32) CurveFrame {
	if c == nil {
		return CurveFrame{Tangent: Vec3Forward(), Up: Vec3Up(), Right: Vec3Right()}
	}
	return c.core.frameAt(c.core.distanceToT(dist), Vec3Up())
}

// Closed reports whether the spline forms a closed loop.
func (c *CatmullRom3D) Closed() bool {
	if c == nil {
		return false
	}
	return c.core.closed
}
