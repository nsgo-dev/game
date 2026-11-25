package physics3d

import "github.com/nsgo-dev/game/packages/game/internal/detmath"

// quatFromBasis constructs a unit quaternion whose rotation matrix has columns
// (x, y, z), mapping local (1,0,0)->x, (0,1,0)->y, (0,0,1)->z.
func quatFromBasis(x, y, z Vec3) Quat {
	trace := x.X + y.Y + z.Z
	var q Quat
	if trace > 0 {
		s := sqrt32(trace+1) * 2
		invS := 1 / s
		q = Quat{
			W: 0.25 * s,
			X: (y.Z - z.Y) * invS,
			Y: (z.X - x.Z) * invS,
			Z: (x.Y - y.X) * invS,
		}
	} else if x.X > y.Y && x.X > z.Z {
		s := sqrt32(1+x.X-y.Y-z.Z) * 2
		invS := 1 / s
		q = Quat{
			W: (y.Z - z.Y) * invS,
			X: 0.25 * s,
			Y: (y.X + x.Y) * invS,
			Z: (z.X + x.Z) * invS,
		}
	} else if y.Y > z.Z {
		s := sqrt32(1+y.Y-x.X-z.Z) * 2
		invS := 1 / s
		q = Quat{
			W: (z.X - x.Z) * invS,
			X: (y.X + x.Y) * invS,
			Y: 0.25 * s,
			Z: (z.Y + y.Z) * invS,
		}
	} else {
		s := sqrt32(1+z.Z-x.X-y.Y) * 2
		invS := 1 / s
		q = Quat{
			W: (x.Y - y.X) * invS,
			X: (z.X + x.Z) * invS,
			Y: (z.Y + y.Z) * invS,
			Z: 0.25 * s,
		}
	}
	return q.Norm()
}

// SwingTwistJoint is a 3D ball-and-socket constraint with an asymmetric
// four-quadrant elliptic swing cone and an independent twist range [MinTwist, MaxTwist].
//
// Each body carries an orthonormal frame (TwistAxis, PlaneAxis, TwistAxis x PlaneAxis).
// Swing1 rotates around PlaneAxisA (clamped to [SwingMin1, SwingMax1]),
// Swing2 rotates around TwistAxisA x PlaneAxisA (clamped to [SwingMin2, SwingMax2]),
// and Twist rotates around TwistAxisB (clamped to [MinTwist, MaxTwist]).
type SwingTwistJoint struct {
	BodyA, BodyB *Body
	LocalAnchorA Vec3
	LocalAnchorB Vec3
	TwistAxisA   Vec3 // bone/twist axis in A's local space (defaults to +Y)
	TwistAxisB   Vec3 // bone/twist axis in B's local space (defaults to world-aligned TwistAxisA)
	PlaneAxisA   Vec3 // reference axis perpendicular to TwistAxisA (auto-completed if zero)
	PlaneAxisB   Vec3 // reference axis perpendicular to TwistAxisB (auto-aligned if zero)

	EnableSwingLimits bool
	SwingMin1         float32 // negative/lower swing limit around PlaneAxisA in radians
	SwingMax1         float32 // positive/upper swing limit around PlaneAxisA in radians
	SwingMin2         float32 // negative/lower swing limit around Plane2A in radians
	SwingMax2         float32 // positive/upper swing limit around Plane2A in radians

	EnableTwistLimits bool
	MinTwist          float32 // lower twist limit in radians (in [-pi, pi])
	MaxTwist          float32 // upper twist limit in radians (in [-pi, pi])

	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	refInitialized  bool
	swingImpulse    float32
	twistImpulse    float32
	twistLimitState int // 0 = none, -1 = below min, +1 = above max, 2 = locked
}

// NewSwingTwistJoint creates a SwingTwistJoint locking LocalAnchorA on a to
// LocalAnchorB on b with twistAxis specified in a's local space (defaulting to +Y).
// TwistAxisB and PlaneAxisB are aligned to match A's frame in world space so the
// initial swing and twist angles start at zero.
func NewSwingTwistJoint(a, b *Body, localAnchorA, localAnchorB, twistAxis Vec3) *SwingTwistJoint {
	tA := twistAxis.Norm()
	if tA.LenSq() == 0 {
		tA = Vec3{Y: 1}
	}
	p1A, _ := orthonormalBasis(tA)
	j := &SwingTwistJoint{
		BodyA:        a,
		BodyB:        b,
		LocalAnchorA: localAnchorA,
		LocalAnchorB: localAnchorB,
		TwistAxisA:   tA,
		PlaneAxisA:   p1A,
	}
	j.initReference()
	return j
}

func (j *SwingTwistJoint) collideConnected() bool { return j.CollideConnected }

func (j *SwingTwistJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *SwingTwistJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *SwingTwistJoint) initReference() {
	if j.refInitialized {
		return
	}
	tA := j.TwistAxisA.Norm()
	if tA.LenSq() == 0 {
		tA = Vec3{Y: 1}
		j.TwistAxisA = tA
	}
	p1A := j.PlaneAxisA.Sub(tA.Scale(j.PlaneAxisA.Dot(tA))).Norm()
	if p1A.LenSq() == 0 {
		p1A, _ = orthonormalBasis(tA)
	}
	j.PlaneAxisA = p1A

	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)

	tB := j.TwistAxisB.Norm()
	if tB.LenSq() == 0 {
		worldT := rotA.RotateVec(tA)
		tB = rotB.Conjugate().RotateVec(worldT).Norm()
		if tB.LenSq() == 0 {
			tB = tA
		}
		j.TwistAxisB = tB
	}
	p1B := j.PlaneAxisB.Sub(tB.Scale(j.PlaneAxisB.Dot(tB))).Norm()
	if p1B.LenSq() == 0 {
		worldP1 := rotA.RotateVec(p1A)
		p1B = rotB.Conjugate().RotateVec(worldP1)
		p1B = p1B.Sub(tB.Scale(p1B.Dot(tB))).Norm()
		if p1B.LenSq() == 0 {
			p1B, _ = orthonormalBasis(tB)
		}
		j.PlaneAxisB = p1B
	}
	j.refInitialized = true
}

func (j *SwingTwistJoint) localFrames() (tA, p1A, p2A, tB, p1B, p2B Vec3) {
	j.initReference()
	tA = j.TwistAxisA.Norm()
	if tA.LenSq() == 0 {
		tA = Vec3{Y: 1}
	}
	p1A = j.PlaneAxisA.Sub(tA.Scale(j.PlaneAxisA.Dot(tA))).Norm()
	if p1A.LenSq() == 0 {
		p1A, _ = orthonormalBasis(tA)
	}
	p2A = tA.Cross(p1A).Norm()

	tB = j.TwistAxisB.Norm()
	if tB.LenSq() == 0 {
		tB = tA
	}
	p1B = j.PlaneAxisB.Sub(tB.Scale(j.PlaneAxisB.Dot(tB))).Norm()
	if p1B.LenSq() == 0 {
		p1B, _ = orthonormalBasis(tB)
	}
	p2B = tB.Cross(p1B).Norm()
	return tA, p1A, p2A, tB, p1B, p2B
}

func (j *SwingTwistJoint) decompose() (swing1, swing2, twist float32, worldTB, worldP1A, worldP2A Vec3) {
	tA, p1A, p2A, tB, p1B, p2B := j.localFrames()
	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)

	qFA := rotA.Mul(quatFromBasis(tA, p1A, p2A)).Norm()
	qFB := rotB.Mul(quatFromBasis(tB, p1B, p2B)).Norm()

	worldP1A = rotA.RotateVec(p1A)
	worldP2A = rotA.RotateVec(p2A)
	worldTB = rotB.RotateVec(tB)

	qRel := qFA.Conjugate().Mul(qFB).Norm()
	if qRel.W < 0 {
		qRel = Quat{X: -qRel.X, Y: -qRel.Y, Z: -qRel.Z, W: -qRel.W}
	}

	// Exact swing-twist decomposition around local +X: qRel = qSwing * qTwist.
	var qTwist Quat
	twLenSq := float32(qRel.X*qRel.X) + float32(qRel.W*qRel.W)
	if twLenSq > 1e-12 {
		inv := 1 / sqrt32(twLenSq)
		qTwist = Quat{X: qRel.X * inv, W: qRel.W * inv}
	} else {
		qTwist = QuatIdentity()
	}
	qSwing := qRel.Mul(qTwist.Conjugate()).Norm()
	if qSwing.W < 0 {
		qSwing = Quat{X: -qSwing.X, Y: -qSwing.Y, Z: -qSwing.Z, W: -qSwing.W}
	}

	twist = float32(2 * detmath.Atan2(float64(qTwist.X), float64(qTwist.W)))

	sinHalf := sqrt32(float32(qSwing.Y*qSwing.Y) + float32(qSwing.Z*qSwing.Z))
	if sinHalf > 1e-6 {
		theta := float32(2 * detmath.Atan2(float64(sinHalf), float64(qSwing.W)))
		scale := theta / sinHalf
		swing1 = qSwing.Y * scale
		swing2 = qSwing.Z * scale
	}
	return swing1, swing2, twist, worldTB, worldP1A, worldP2A
}

// SwingAngles returns the current decomposed swing angles (swing1, swing2) in
// radians around PlaneAxisA and TwistAxisA x PlaneAxisA.
func (j *SwingTwistJoint) SwingAngles() (swing1, swing2 float32) {
	s1, s2, _, _, _, _ := j.decompose()
	return s1, s2
}

// TwistAngle returns the current decomposed twist angle in radians in [-pi, pi]
// around the joint's twist axis.
func (j *SwingTwistJoint) TwistAngle() float32 {
	_, _, tw, _, _, _ := j.decompose()
	return tw
}

func (j *SwingTwistJoint) evalSwingLimit(s1, s2 float32) (violated bool, c1, c2, n1, n2 float32, lock1, lock2 bool) {
	min1, max1 := j.SwingMin1, j.SwingMax1
	if max1 < min1 {
		max1 = min1
	}
	min2, max2 := j.SwingMin2, j.SwingMax2
	if max2 < min2 {
		max2 = min2
	}

	var o1, o2 float32
	if min1 > 0 || max1 < 0 {
		o1 = 0.5 * (min1 + max1)
	}
	if min2 > 0 || max2 < 0 {
		o2 = 0.5 * (min2 + max2)
	}
	d1 := s1 - o1
	d2 := s2 - o2

	var r1, r2 float32
	if d1 >= 0 {
		r1 = max1 - o1
	} else {
		r1 = o1 - min1
	}
	if d2 >= 0 {
		r2 = max2 - o2
	} else {
		r2 = o2 - min2
	}

	const eps = 1e-6
	switch {
	case r1 <= eps && r2 <= eps:
		return abs32(d1) > 1e-5 || abs32(d2) > 1e-5, o1, o2, 0, 0, true, true
	case r1 <= eps:
		c1 = o1
		c2 = clamp32(s2, min2, max2)
		lock1 = true
		if s2 < min2-1e-5 {
			n2 = -1
			violated = true
		} else if s2 > max2+1e-5 {
			n2 = 1
			violated = true
		} else if abs32(d1) > 1e-5 {
			violated = true
		}
		return violated, c1, c2, 0, n2, true, false
	case r2 <= eps:
		c1 = clamp32(s1, min1, max1)
		c2 = o2
		lock2 = true
		if s1 < min1-1e-5 {
			n1 = -1
			violated = true
		} else if s1 > max1+1e-5 {
			n1 = 1
			violated = true
		} else if abs32(d2) > 1e-5 {
			violated = true
		}
		return violated, c1, c2, n1, 0, false, true
	default:
		u1 := d1 / r1
		u2 := d2 / r2
		uSq := float32(u1*u1) + float32(u2*u2)
		if uSq <= 1 {
			return false, s1, s2, 0, 0, false, false
		}
		u := sqrt32(uSq)
		c1 = o1 + d1/u
		c2 = o2 + d2/u
		gx := d1 / (r1 * r1)
		gy := d2 / (r2 * r2)
		gLen := sqrt32(float32(gx*gx) + float32(gy*gy))
		if gLen > 1e-12 {
			n1 = gx / gLen
			n2 = gy / gLen
		}
		return true, c1, c2, n1, n2, false, false
	}
}

// Prepare resets per-step impulses and evaluates active twist limit states.
func (j *SwingTwistJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.initReference()
	j.swingImpulse = 0
	j.twistImpulse = 0
	j.twistLimitState = 0

	if j.EnableTwistLimits && j.MinTwist <= j.MaxTwist {
		tw := j.TwistAngle()
		if j.MaxTwist <= j.MinTwist+1e-6 {
			j.twistLimitState = 2
		} else if tw <= j.MinTwist+1e-4 {
			j.twistLimitState = -1
		} else if tw >= j.MaxTwist-1e-4 {
			j.twistLimitState = 1
		}
	}
}

// SolveVelocity solves the 3D socket anchor, asymmetric swing cone, and twist limits.
func (j *SwingTwistJoint) SolveVelocity(dt float32) {
	_ = dt
	j.addLinearImpulse(solvePointAnchorVelocity(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB))

	if !j.EnableSwingLimits && j.twistLimitState == 0 {
		return
	}

	_, _, _, angA, _, invTA := bodyKinematics(j.BodyA)
	_, _, _, angB, _, invTB := bodyKinematics(j.BodyB)
	if invTA.IsZero() && invTB.IsZero() {
		return
	}

	s1, s2, _, worldTB, worldP1A, worldP2A := j.decompose()

	// 1. Asymmetric elliptic swing cone limits.
	if j.EnableSwingLimits {
		violated, _, _, n1, n2, lock1, lock2 := j.evalSwingLimit(s1, s2)
		if lock1 {
			if inv1 := invTA.QuadForm(worldP1A) + invTB.QuadForm(worldP1A); inv1 > 1e-12 {
				_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
				_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
				w1 := angB.Sub(angA).Dot(worldP1A)
				imp := worldP1A.Scale(-w1 / inv1)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			}
		}
		if lock2 {
			if inv2 := invTA.QuadForm(worldP2A) + invTB.QuadForm(worldP2A); inv2 > 1e-12 {
				_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
				_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
				w2 := angB.Sub(angA).Dot(worldP2A)
				imp := worldP2A.Scale(-w2 / inv2)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			}
		}
		if violated && (n1 != 0 || n2 != 0) {
			nW := worldP1A.Scale(n1).Add(worldP2A.Scale(n2)).Norm()
			if nW.LenSq() > 0 {
				if invW := invTA.QuadForm(nW) + invTB.QuadForm(nW); invW > 1e-12 {
					_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
					_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
					wRel := angB.Sub(angA).Dot(nW)
					dLambda := -wRel / invW
					newAcc := j.swingImpulse + dLambda
					if newAcc > 0 {
						newAcc = 0
					}
					applied := newAcc - j.swingImpulse
					j.swingImpulse = newAcc
					if applied != 0 {
						imp := nW.Scale(applied)
						applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
						j.addAngularImpulse(imp)
					}
				}
			}
		}
	}

	// 2. Twist limits around worldTB.
	if j.twistLimitState != 0 {
		if invTw := invTA.QuadForm(worldTB) + invTB.QuadForm(worldTB); invTw > 1e-12 {
			_, _, _, angA, _, _ = bodyKinematics(j.BodyA)
			_, _, _, angB, _, _ = bodyKinematics(j.BodyB)
			wTwist := angB.Sub(angA).Dot(worldTB)
			dLambda := -wTwist / invTw
			if j.twistLimitState == 2 {
				imp := worldTB.Scale(dLambda)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			} else {
				newAcc := j.twistImpulse + dLambda
				if j.twistLimitState < 0 {
					if newAcc < 0 {
						newAcc = 0
					}
				} else {
					if newAcc > 0 {
						newAcc = 0
					}
				}
				applied := newAcc - j.twistImpulse
				j.twistImpulse = newAcc
				if applied != 0 {
					imp := worldTB.Scale(applied)
					applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
					j.addAngularImpulse(imp)
				}
			}
		}
	}
}

// SolvePosition corrects socket anchor drift, asymmetric swing violations, and twist violations.
func (j *SwingTwistJoint) SolvePosition(dt float32) {
	_ = dt
	solvePointAnchorPosition(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB, 0.8)

	if !j.EnableSwingLimits && !j.EnableTwistLimits {
		return
	}
	_, _, _, _, _, invTA := bodyKinematics(j.BodyA)
	_, _, _, _, _, invTB := bodyKinematics(j.BodyB)
	if invTA.IsZero() && invTB.IsZero() {
		return
	}

	// 1. Swing cone correction.
	if j.EnableSwingLimits {
		s1, s2, _, _, worldP1A, worldP2A := j.decompose()
		violated, c1, c2, _, _, _, _ := j.evalSwingLimit(s1, s2)
		if violated {
			corrVec := worldP1A.Scale(c1 - s1).Add(worldP2A.Scale(c2 - s2))
			if corrLen := corrVec.Len(); corrLen > 1e-5 {
				applyAngularPositionCorrection(j.BodyA, j.BodyB, corrVec.Scale(1/corrLen), corrLen*0.8)
			}
		}
	}

	// 2. Twist limit correction.
	if j.EnableTwistLimits && j.MinTwist <= j.MaxTwist {
		_, _, tw, worldTB, _, _ := j.decompose()
		if tw < j.MinTwist-1e-5 {
			applyAngularPositionCorrection(j.BodyA, j.BodyB, worldTB, (j.MinTwist-tw)*0.8)
		} else if tw > j.MaxTwist+1e-5 {
			applyAngularPositionCorrection(j.BodyA, j.BodyB, worldTB, -(tw-j.MaxTwist)*0.8)
		}
	}
}

// AxisMode controls whether one linear or angular degree of freedom on a
// SixDOFJoint is free, locked at 0, or clamped to [Min, Max].
type AxisMode int

const (
	// AxisFree leaves the axis unconstrained (optional springs and drives still apply).
	AxisFree AxisMode = iota
	// AxisLocked rigidly locks the axis at 0.
	AxisLocked
	// AxisLimited clamps the axis to [Min, Max].
	AxisLimited
)

// DriveMode selects how a SixDOFJoint axis drive operates.
type DriveMode int

const (
	// DriveNone disables explicit motor driving (Stiffness > 0 or Damping > 0
	// still acts as a passive spring-damper toward TargetPos).
	DriveNone DriveMode = iota
	// DrivePosition drives the axis toward TargetPos using Stiffness and Damping
	// (or a direct velocity servo when both are 0), clamped by MaxForce when > 0.
	DrivePosition
	// DriveVelocity drives the axis toward TargetVel, clamped by MaxForce when > 0.
	DriveVelocity
)

// AxisConfig configures one linear (X, Y, Z) or angular (X, Y, Z) degree of
// freedom of a SixDOFJoint.
type AxisConfig struct {
	Mode      AxisMode
	Min       float32 // lower limit (meters or radians) when Mode == AxisLimited
	Max       float32 // upper limit (meters or radians) when Mode == AxisLimited
	Drive     DriveMode
	Stiffness float32 // spring stiffness (N/m for linear, N*m/rad for angular)
	Damping   float32 // damping coefficient (N*s/m for linear, N*m*s/rad for angular)
	TargetPos float32 // target position (meters) or angle (radians)
	TargetVel float32 // target velocity (m/s) or angular velocity (rad/s)
	MaxForce  float32 // max drive force (N) or torque (N*m); <= 0 means unlimited
}

type sixDOFAxisState struct {
	limitState   int // 0 = none, -1 = min, +1 = max, 2 = locked at Min
	limitImpulse float32
	driveImpulse float32
	driveGamma   float32
	driveBias    float32
	driveMode    int // 0 = inactive, 1 = spring/position, 2 = direct velocity/servo
	driveVel     float32
}

// SixDOFJoint is a generic 6-degree-of-freedom constraint between BodyA and BodyB
// with independent linear (X, Y, Z) and angular (X, Y, Z) axis modes, limits,
// spring stiffness/damping, velocity or position drives, reaction readouts, and
// optional break thresholds.
type SixDOFJoint struct {
	BodyA, BodyB *Body
	LocalAnchorA Vec3
	LocalAnchorB Vec3
	// LocalFrameA and LocalFrameB orient the constraint's (X, Y, Z) frame in A's
	// and B's local spaces. Zero quaternions default to identity for A and
	// world-aligned with A on initialization for B.
	LocalFrameA Quat
	LocalFrameB Quat

	Linear  [3]AxisConfig // linear X, Y, Z (indices 0, 1, 2)
	Angular [3]AxisConfig // angular X, Y, Z (indices 0, 1, 2)

	BreakForce       float32 // max reaction force in N before breaking (<= 0 disables)
	BreakTorque      float32 // max reaction torque in N*m before breaking (<= 0 disables)
	CollideConnected bool

	jointReaction
	refInitialized  bool
	linState        [3]sixDOFAxisState
	angState        [3]sixDOFAxisState
	singleAxisAngle float32
	singleAxisIdx   int
}

// NewSixDOFJoint creates a SixDOFJoint between a and b at the specified local
// anchor points, aligning LocalFrameB to match LocalFrameA (identity) in world
// space so all six relative coordinates start at zero. All axes default to AxisFree.
func NewSixDOFJoint(a, b *Body, localAnchorA, localAnchorB Vec3) *SixDOFJoint {
	j := &SixDOFJoint{
		BodyA:         a,
		BodyB:         b,
		LocalAnchorA:  localAnchorA,
		LocalAnchorB:  localAnchorB,
		LocalFrameA:   QuatIdentity(),
		singleAxisIdx: -1,
	}
	j.initReference()
	return j
}

func (j *SixDOFJoint) collideConnected() bool { return j.CollideConnected }

func (j *SixDOFJoint) checkBreak() (JointBreakEvent, bool) {
	return j.evalBreak(j, j.BreakForce, j.BreakTorque)
}

// Bodies returns the connected bodies.
func (j *SixDOFJoint) Bodies() (*Body, *Body) { return j.BodyA, j.BodyB }

func (j *SixDOFJoint) initReference() {
	if j.refInitialized {
		return
	}
	if j.LocalFrameA.LenSq() <= 1e-12 {
		j.LocalFrameA = QuatIdentity()
	} else {
		j.LocalFrameA = j.LocalFrameA.Norm()
	}
	if j.LocalFrameB.LenSq() <= 1e-12 {
		_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
		_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
		worldFA := rotA.Mul(j.LocalFrameA).Norm()
		j.LocalFrameB = rotB.Conjugate().Mul(worldFA).Norm()
	} else {
		j.LocalFrameB = j.LocalFrameB.Norm()
	}
	j.refInitialized = true
}

func (j *SixDOFJoint) worldFrames() (qFA, qFB Quat, uA, uB [3]Vec3) {
	j.initReference()
	_, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	_, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	qFA = rotA.Mul(normalizedQuat(j.LocalFrameA)).Norm()
	qFB = rotB.Mul(normalizedQuat(j.LocalFrameB)).Norm()
	uA = [3]Vec3{
		qFA.RotateVec(Vec3{X: 1}),
		qFA.RotateVec(Vec3{Y: 1}),
		qFA.RotateVec(Vec3{Z: 1}),
	}
	uB = [3]Vec3{
		qFB.RotateVec(Vec3{X: 1}),
		qFB.RotateVec(Vec3{Y: 1}),
		qFB.RotateVec(Vec3{Z: 1}),
	}
	return qFA, qFB, uA, uB
}

func (j *SixDOFJoint) singleFreeAngularAxis() int {
	freeIdx := -1
	freeCount := 0
	for k := range 3 {
		if j.Angular[k].Mode != AxisLocked {
			freeIdx = k
			freeCount++
		}
	}
	if freeCount == 1 {
		return freeIdx
	}
	return -1
}

func (j *SixDOFJoint) allLinearLocked() bool {
	return j.Linear[0].Mode == AxisLocked && j.Linear[1].Mode == AxisLocked && j.Linear[2].Mode == AxisLocked
}

func (j *SixDOFJoint) allAngularLocked() bool {
	return j.Angular[0].Mode == AxisLocked && j.Angular[1].Mode == AxisLocked && j.Angular[2].Mode == AxisLocked
}

func (j *SixDOFJoint) rawSingleAxisAngle(axisIdx int, uA, uB [3]Vec3) float32 {
	a1 := uA[axisIdx]
	perpIdx := (axisIdx + 1) % 3
	curA := uA[perpIdx]
	curB := uB[perpIdx]
	curBProj := curB.Sub(a1.Scale(curB.Dot(a1)))
	if curBProj.LenSq() <= 1e-10 {
		return 0
	}
	curBProj = curBProj.Norm()
	sinA := a1.Dot(curA.Cross(curBProj))
	cosA := curA.Dot(curBProj)
	return float32(detmath.Atan2(float64(sinA), float64(cosA)))
}

// LinearPosition returns the current relative anchor displacement along the
// constraint frame's (X, Y, Z) axes in meters.
func (j *SixDOFJoint) LinearPosition() Vec3 {
	posA, rotA, _, _, _, _ := bodyKinematics(j.BodyA)
	posB, rotB, _, _, _, _ := bodyKinematics(j.BodyB)
	_, _, uA, _ := j.worldFrames()
	diff := posB.Add(rotB.RotateVec(j.LocalAnchorB)).Sub(posA.Add(rotA.RotateVec(j.LocalAnchorA)))
	return Vec3{
		X: diff.Dot(uA[0]),
		Y: diff.Dot(uA[1]),
		Z: diff.Dot(uA[2]),
	}
}

// AngularPosition returns the current relative rotation (X, Y, Z) in radians
// in the constraint frame. When exactly one angular axis is unlocked, its
// component is tracked continuously past ±pi.
func (j *SixDOFJoint) AngularPosition() Vec3 {
	qFA, qFB, uA, uB := j.worldFrames()
	if idx := j.singleFreeAngularAxis(); idx >= 0 {
		raw := j.rawSingleAxisAngle(idx, uA, uB)
		ang := raw
		if j.singleAxisIdx == idx {
			ang = j.singleAxisAngle + wrapPi(raw-j.singleAxisAngle)
		}
		var out Vec3
		switch idx {
		case 0:
			out.X = ang
		case 1:
			out.Y = ang
		case 2:
			out.Z = ang
		}
		return out
	}
	qRel := qFA.Conjugate().Mul(qFB).Norm()
	axis, angle := qRel.ToAxisAngle()
	return axis.Scale(angle)
}

func vecComponent(v Vec3, k int) float32 {
	switch k {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

func prepareSixDOFAxis(cfg AxisConfig, coord, dt float32) (st sixDOFAxisState, wake bool) {
	if cfg.Mode == AxisLimited && cfg.Min <= cfg.Max {
		if cfg.Max <= cfg.Min+1e-6 {
			st.limitState = 2
		} else if coord <= cfg.Min+1e-4 {
			st.limitState = -1
		} else if coord >= cfg.Max-1e-4 {
			st.limitState = 1
		}
	}

	if cfg.Mode == AxisLocked || dt <= 0 {
		return st, false
	}

	switch cfg.Drive {
	case DriveVelocity:
		if cfg.Damping > 0 {
			if gamma, _, ok := softSpringParams(0, cfg.Damping, 0, dt); ok {
				st.driveMode = 1
				st.driveGamma = gamma
				st.driveVel = cfg.TargetVel
			}
		} else {
			st.driveMode = 2
			st.driveVel = cfg.TargetVel
		}
		if abs32(cfg.TargetVel) > 1e-6 {
			wake = true
		}
	case DrivePosition:
		err := coord - cfg.TargetPos
		if cfg.Stiffness > 0 || cfg.Damping > 0 {
			if gamma, bias, ok := softSpringParams(cfg.Stiffness, cfg.Damping, err, dt); ok {
				st.driveMode = 1
				st.driveGamma = gamma
				st.driveBias = bias
				st.driveVel = cfg.TargetVel
			}
		} else {
			st.driveMode = 2
			st.driveVel = -err / dt
		}
		if abs32(err) > 1e-4 {
			wake = true
		}
	default:
		// Passive spring-damper when Stiffness > 0 or Damping > 0.
		if cfg.Stiffness > 0 || cfg.Damping > 0 {
			err := coord - cfg.TargetPos
			if gamma, bias, ok := softSpringParams(cfg.Stiffness, cfg.Damping, err, dt); ok {
				st.driveMode = 1
				st.driveGamma = gamma
				st.driveBias = bias
				st.driveVel = cfg.TargetVel
				if cfg.Stiffness > 0 && abs32(err) > 1e-4 {
					wake = true
				}
			}
		}
	}
	return st, wake
}

func solveSixDOFDriveScalar(st *sixDOFAxisState, cfg AxisConfig, vRel, invEffMass, dt float32) float32 {
	if st.driveMode == 0 || invEffMass <= 1e-12 || dt <= 0 {
		return 0
	}
	var dLambda float32
	if st.driveMode == 1 {
		dLambda = -(float32((vRel-st.driveVel)+st.driveBias) + float32(st.driveGamma*st.driveImpulse)) / (invEffMass + st.driveGamma)
	} else {
		dLambda = (st.driveVel - vRel) / invEffMass
	}
	newAcc := st.driveImpulse + dLambda
	if cfg.MaxForce > 0 {
		maxImp := cfg.MaxForce * dt
		newAcc = clamp32(newAcc, -maxImp, maxImp)
	}
	applied := newAcc - st.driveImpulse
	st.driveImpulse = newAcc
	return applied
}

func solveSixDOFLimitScalar(st *sixDOFAxisState, vRel, invEffMass float32) float32 {
	if st.limitState == 0 || invEffMass <= 1e-12 {
		return 0
	}
	dLambda := -vRel / invEffMass
	if st.limitState == 2 {
		return dLambda
	}
	newAcc := st.limitImpulse + dLambda
	if st.limitState < 0 {
		if newAcc < 0 {
			newAcc = 0
		}
	} else {
		if newAcc > 0 {
			newAcc = 0
		}
	}
	applied := newAcc - st.limitImpulse
	st.limitImpulse = newAcc
	return applied
}

// Prepare initializes the constraint frames, wakes driven bodies, and caches
// per-axis spring, drive, and limit states for dt.
func (j *SixDOFJoint) Prepare(dt float32) {
	j.resetStepReaction(dt)
	j.initReference()

	if idx := j.singleFreeAngularAxis(); idx >= 0 {
		_, _, uA, uB := j.worldFrames()
		raw := j.rawSingleAxisAngle(idx, uA, uB)
		if j.singleAxisIdx == idx {
			j.singleAxisAngle = j.singleAxisAngle + wrapPi(raw-j.singleAxisAngle)
		} else {
			j.singleAxisIdx = idx
			j.singleAxisAngle = raw
		}
	} else {
		j.singleAxisIdx = -1
	}

	linPos := j.LinearPosition()
	angPos := j.AngularPosition()

	wake := false
	for k := range 3 {
		st, w := prepareSixDOFAxis(j.Linear[k], vecComponent(linPos, k), dt)
		j.linState[k] = st
		wake = wake || w
	}
	for k := range 3 {
		st, w := prepareSixDOFAxis(j.Angular[k], vecComponent(angPos, k), dt)
		j.angState[k] = st
		wake = wake || w
	}
	if wake {
		wakeIfDynamic(j.BodyA)
		wakeIfDynamic(j.BodyB)
	}
}

// SolveVelocity solves all 6 linear and angular constraint, spring, drive, and limit rows.
func (j *SixDOFJoint) SolveVelocity(dt float32) {
	_, rotA, _, _, invMA, invTA := bodyKinematics(j.BodyA)
	_, rotB, _, _, invMB, invTB := bodyKinematics(j.BodyB)
	if invMA+invMB == 0 && invTA.IsZero() && invTB.IsZero() {
		return
	}

	_, _, uA, _ := j.worldFrames()
	rA := jointAnchorOffset(j.BodyA, rotA, j.LocalAnchorA)
	rB := jointAnchorOffset(j.BodyB, rotB, j.LocalAnchorB)
	invMSum := invMA + invMB

	// 1. Linear 3 DOFs.
	if j.allLinearLocked() {
		j.addLinearImpulse(solvePointAnchorVelocity(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB))
	} else {
		for k := range 3 {
			axis := uA[k]
			rAxa := rA.Cross(axis)
			rBxa := rB.Cross(axis)
			w := float32(invMSum+invTA.QuadForm(rAxa)) + invTB.QuadForm(rBxa)
			if w <= 1e-12 {
				continue
			}
			// Drive / spring first.
			if j.linState[k].driveMode != 0 {
				_, _, velA, angA, _, _ := bodyKinematics(j.BodyA)
				_, _, velB, angB, _, _ := bodyKinematics(j.BodyB)
				vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA))).Dot(axis)
				if applied := solveSixDOFDriveScalar(&j.linState[k], j.Linear[k], vRel, w, dt); applied != 0 {
					imp := axis.Scale(applied)
					applyAnchorImpulse(j.BodyA, j.BodyB, rA, rB, imp)
					j.addLinearImpulse(imp)
				}
			}
			// Lock or limit after drive so hard constraints override drives.
			if j.Linear[k].Mode == AxisLocked {
				_, _, velA, angA, _, _ := bodyKinematics(j.BodyA)
				_, _, velB, angB, _, _ := bodyKinematics(j.BodyB)
				vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA))).Dot(axis)
				imp := axis.Scale(-vRel / w)
				applyAnchorImpulse(j.BodyA, j.BodyB, rA, rB, imp)
				j.addLinearImpulse(imp)
			} else if j.linState[k].limitState != 0 {
				_, _, velA, angA, _, _ := bodyKinematics(j.BodyA)
				_, _, velB, angB, _, _ := bodyKinematics(j.BodyB)
				vRel := velB.Add(angB.Cross(rB)).Sub(velA.Add(angA.Cross(rA))).Dot(axis)
				if applied := solveSixDOFLimitScalar(&j.linState[k], vRel, w); applied != 0 {
					imp := axis.Scale(applied)
					applyAnchorImpulse(j.BodyA, j.BodyB, rA, rB, imp)
					j.addLinearImpulse(imp)
				}
			}
		}
	}

	// 2. Angular 3 DOFs.
	if invTA.IsZero() && invTB.IsZero() {
		return
	}
	if j.allAngularLocked() {
		j.addAngularImpulse(solveFullOrientationVelocity(j.BodyA, j.BodyB))
		return
	}

	if idx := j.singleFreeAngularAxis(); idx >= 0 {
		// Hinge-equivalent single-free-axis solve: lock the 2 perpendicular axes, then solve drive + limits on uA[idx].
		a1 := uA[idx]
		p1 := uA[(idx+1)%3]
		p2 := uA[(idx+2)%3]
		j.addAngularImpulse(solvePerpAngularLockVelocity(j.BodyA, j.BodyB, p1, p2))

		invAxis := invTA.QuadForm(a1) + invTB.QuadForm(a1)
		if invAxis <= 1e-12 {
			return
		}
		if j.angState[idx].driveMode != 0 {
			_, _, _, angA, _, _ := bodyKinematics(j.BodyA)
			_, _, _, angB, _, _ := bodyKinematics(j.BodyB)
			wAxis := angB.Sub(angA).Dot(a1)
			if applied := solveSixDOFDriveScalar(&j.angState[idx], j.Angular[idx], wAxis, invAxis, dt); applied != 0 {
				imp := a1.Scale(applied)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			}
		}
		if j.angState[idx].limitState != 0 {
			_, _, _, angA, _, _ := bodyKinematics(j.BodyA)
			_, _, _, angB, _, _ := bodyKinematics(j.BodyB)
			wAxis := angB.Sub(angA).Dot(a1)
			if applied := solveSixDOFLimitScalar(&j.angState[idx], wAxis, invAxis); applied != 0 {
				imp := a1.Scale(applied)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			}
		}
		return
	}

	for k := range 3 {
		axis := uA[k]
		invAxis := invTA.QuadForm(axis) + invTB.QuadForm(axis)
		if invAxis <= 1e-12 {
			continue
		}
		if j.angState[k].driveMode != 0 {
			_, _, _, angA, _, _ := bodyKinematics(j.BodyA)
			_, _, _, angB, _, _ := bodyKinematics(j.BodyB)
			wAxis := angB.Sub(angA).Dot(axis)
			if applied := solveSixDOFDriveScalar(&j.angState[k], j.Angular[k], wAxis, invAxis, dt); applied != 0 {
				imp := axis.Scale(applied)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			}
		}
		if j.Angular[k].Mode == AxisLocked {
			_, _, _, angA, _, _ := bodyKinematics(j.BodyA)
			_, _, _, angB, _, _ := bodyKinematics(j.BodyB)
			wAxis := angB.Sub(angA).Dot(axis)
			imp := axis.Scale(-wAxis / invAxis)
			applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
			j.addAngularImpulse(imp)
		} else if j.angState[k].limitState != 0 {
			_, _, _, angA, _, _ := bodyKinematics(j.BodyA)
			_, _, _, angB, _, _ := bodyKinematics(j.BodyB)
			wAxis := angB.Sub(angA).Dot(axis)
			if applied := solveSixDOFLimitScalar(&j.angState[k], wAxis, invAxis); applied != 0 {
				imp := axis.Scale(applied)
				applyAngularImpulsePair(j.BodyA, j.BodyB, imp)
				j.addAngularImpulse(imp)
			}
		}
	}
}

// SolvePosition corrects locked and limited linear and angular drift.
func (j *SixDOFJoint) SolvePosition(dt float32) {
	_ = dt
	j.initReference()

	_, _, _, _, invMA, invTA := bodyKinematics(j.BodyA)
	_, _, _, _, invMB, invTB := bodyKinematics(j.BodyB)
	if invMA+invMB == 0 && invTA.IsZero() && invTB.IsZero() {
		return
	}

	// 1. Linear positional correction.
	if j.allLinearLocked() {
		solvePointAnchorPosition(j.BodyA, j.BodyB, j.LocalAnchorA, j.LocalAnchorB, 0.8)
	} else {
		invMSum := invMA + invMB
		for k := range 3 {
			cfg := j.Linear[k]
			if cfg.Mode == AxisFree {
				continue
			}
			posA, rotA, _, _, _, curTA := bodyKinematics(j.BodyA)
			posB, rotB, _, _, _, curTB := bodyKinematics(j.BodyB)
			_, _, uA, _ := j.worldFrames()
			axis := uA[k]
			rA := jointAnchorOffset(j.BodyA, rotA, j.LocalAnchorA)
			rB := jointAnchorOffset(j.BodyB, rotB, j.LocalAnchorB)
			coord := posB.Add(rotB.RotateVec(j.LocalAnchorB)).Sub(posA.Add(rotA.RotateVec(j.LocalAnchorA))).Dot(axis)

			var err float32
			if cfg.Mode == AxisLocked {
				if abs32(coord) > 1e-5 {
					err = coord
				}
			} else if cfg.Mode == AxisLimited && cfg.Min <= cfg.Max {
				if coord < cfg.Min-1e-5 {
					err = coord - cfg.Min
				} else if coord > cfg.Max+1e-5 {
					err = coord - cfg.Max
				}
			}
			if err == 0 {
				continue
			}
			rAxa := rA.Cross(axis)
			rBxa := rB.Cross(axis)
			w := float32(invMSum+curTA.QuadForm(rAxa)) + curTB.QuadForm(rBxa)
			if w <= 1e-12 {
				continue
			}
			corr := axis.Scale(-err * 0.8 / w)
			applyAnchorPositionCorrection(j.BodyA, j.BodyB, rA, rB, corr)
		}
	}

	// 2. Angular positional correction.
	if invTA.IsZero() && invTB.IsZero() {
		return
	}
	if j.allAngularLocked() {
		refRelRot := normalizedQuat(j.LocalFrameA).Mul(normalizedQuat(j.LocalFrameB).Conjugate()).Norm()
		solveFullOrientationPosition(j.BodyA, j.BodyB, refRelRot, 0.8)
		return
	}

	if idx := j.singleFreeAngularAxis(); idx >= 0 {
		_, _, uA, uB := j.worldFrames()
		a1 := uA[idx]
		a2 := uB[idx]
		cross := a2.Cross(a1)
		sinErr := cross.Len()
		if sinErr > 1e-6 {
			cosErr := clamp32(a1.Dot(a2), -1, 1)
			errAngle := float32(detmath.Atan2(float64(sinErr), float64(cosErr)))
			applyAngularPositionCorrection(j.BodyA, j.BodyB, cross.Scale(1/sinErr), errAngle*0.8)
		}
		cfg := j.Angular[idx]
		if cfg.Mode == AxisLimited && cfg.Min <= cfg.Max {
			ang := vecComponent(j.AngularPosition(), idx)
			_, _, uA, _ = j.worldFrames()
			a1 = uA[idx]
			if ang < cfg.Min-1e-5 {
				applyAngularPositionCorrection(j.BodyA, j.BodyB, a1, (cfg.Min-ang)*0.8)
			} else if ang > cfg.Max+1e-5 {
				applyAngularPositionCorrection(j.BodyA, j.BodyB, a1, -(ang-cfg.Max)*0.8)
			}
		}
		return
	}

	for k := range 3 {
		cfg := j.Angular[k]
		if cfg.Mode == AxisFree {
			continue
		}
		angPos := j.AngularPosition()
		ang := vecComponent(angPos, k)
		var delta float32
		if cfg.Mode == AxisLocked {
			if abs32(ang) > 1e-5 {
				delta = -ang
			}
		} else if cfg.Mode == AxisLimited && cfg.Min <= cfg.Max {
			if ang < cfg.Min-1e-5 {
				delta = cfg.Min - ang
			} else if ang > cfg.Max+1e-5 {
				delta = cfg.Max - ang
			}
		}
		if delta != 0 {
			_, _, uA, _ := j.worldFrames()
			applyAngularPositionCorrection(j.BodyA, j.BodyB, uA[k], delta*0.8)
		}
	}
}
