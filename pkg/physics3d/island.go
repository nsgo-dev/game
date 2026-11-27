package physics3d

import "math"

// Sleep defaults for World, in meters, radians, and seconds.
const (
	// DefaultSleepLinearThreshold is the linear speed in units per second below
	// which a dynamic body counts as resting.
	DefaultSleepLinearThreshold = 0.05
	// DefaultSleepAngularThreshold is the angular speed in radians per second
	// below which a dynamic body counts as resting.
	DefaultSleepAngularThreshold = 0.05
	// DefaultTimeToSleep is how many seconds every body of an island must rest
	// before the island sleeps.
	DefaultTimeToSleep = 0.4
)

// SetAllowSleep sets whether b may fall asleep when resting, waking b if allow
// is false. Use this on a zero-initialized Body literal before AddBody when
// disabling sleep (NewDynamicBody defaults AllowSleep to true).
func (b *Body) SetAllowSleep(allow bool) {
	if b == nil {
		return
	}
	b.AllowSleep = allow
	b.allowSleepSet = true
	if !allow && b.Sleeping {
		b.Wake()
	}
}

func (b *Body) isFixed() bool {
	return b.Static || b.Kinematic
}

func (b *Body) awakeDynamic() bool {
	return b != nil && !b.Static && !b.Kinematic && !b.Sleeping
}

func (b *Body) allowsSleep() bool {
	return b != nil && (b.AllowSleep || !b.allowSleepSet)
}

func (b *Body) movedSinceStep() bool {
	return b != nil && b.havePose && (b.Pos != b.lastPos || b.Rot != b.lastRot)
}

func (b *Body) movingKinematic() bool {
	return b != nil && b.Kinematic && !b.Static && (b.Vel != (Vec3{}) || b.AngVel != (Vec3{}))
}

func (b *Body) movedKinematic() bool {
	return b != nil && b.Kinematic && !b.Static && b.movedSinceStep()
}

func (b *Body) wakeIfSleeping() {
	if b != nil && !b.Static && !b.Kinematic && b.Sleeping {
		b.Wake()
	}
}

// canWakeOther reports whether b wakes a sleeping dynamic body on contact:
// any awake dynamic body (at any speed), any moving or moved kinematic body,
// any moved static body, or any moving/moved active area trigger.
func (b *Body) canWakeOther() bool {
	if b == nil {
		return false
	}
	if b.IsTrigger {
		return b.Area.Active() && (b.movedSinceStep() || b.Vel != (Vec3{}) || b.AngVel != (Vec3{}))
	}
	return b.awakeDynamic() || b.movingKinematic() || (b.isFixed() && b.movedSinceStep())
}

// effectiveSleepThresholds returns w's active sleep thresholds, falling back to
// the package defaults whenever a configured value is <= 0, NaN, or infinite.
func (w *World) effectiveSleepThresholds() (linTol, angTol, timeToSleep float32) {
	linTol = w.SleepLinearThreshold
	if !(linTol > 0) || math.IsInf(float64(linTol), 0) {
		linTol = DefaultSleepLinearThreshold
	}
	angTol = w.SleepAngularThreshold
	if !(angTol > 0) || math.IsInf(float64(angTol), 0) {
		angTol = DefaultSleepAngularThreshold
	}
	timeToSleep = w.TimeToSleep
	if !(timeToSleep > 0) || math.IsInf(float64(timeToSleep), 0) {
		timeToSleep = DefaultTimeToSleep
	}
	return linTol, angTol, timeToSleep
}

// stampBodies refreshes each body's transient index in w.bodies for O(1)
// union-find lookup without allocations.
func (w *World) stampBodies() {
	w.islandStamp++
	for i, b := range w.bodies {
		if b != nil {
			b.islandStamp = w.islandStamp
			b.islandIdx = i
		}
	}
}

func (w *World) inWorld(b *Body) bool {
	return b != nil && b.islandStamp == w.islandStamp && b.islandIdx < len(w.bodies) && w.bodies[b.islandIdx] == b
}

func (w *World) resetIslandParent() []int {
	n := len(w.bodies)
	if cap(w.islandParent) < n {
		w.islandParent = make([]int, n)
	} else {
		w.islandParent = w.islandParent[:n]
	}
	for i := range n {
		w.islandParent[i] = i
	}
	return w.islandParent
}

func (w *World) resetIslandAwake() []uint8 {
	n := len(w.bodies)
	if cap(w.islandAwake) < n {
		w.islandAwake = make([]uint8, n)
	} else {
		w.islandAwake = w.islandAwake[:n]
		clear(w.islandAwake)
	}
	return w.islandAwake
}

func islandFindRoot(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}
	return i
}

// islandUnion merges the islands of a and b when both are non-trigger dynamic
// bodies in w.bodies. Static, kinematic, and trigger bodies never link islands.
// The smaller body index always becomes the root so the partition is
// deterministic and independent of edge arrival order.
func (w *World) islandUnion(parent []int, a, b *Body) {
	if !w.inWorld(a) || !w.inWorld(b) || a.isFixed() || b.isFixed() || a.IsTrigger || b.IsTrigger {
		return
	}
	ra := islandFindRoot(parent, a.islandIdx)
	rb := islandFindRoot(parent, b.islandIdx)
	if ra < rb {
		parent[rb] = ra
	} else if rb < ra {
		parent[ra] = rb
	}
}

// hasSleepingDynamic reports whether w currently holds any sleeping dynamic body.
func (w *World) hasSleepingDynamic() bool {
	for _, b := range w.bodies {
		if b != nil && !b.Static && !b.Kinematic && b.Sleeping {
			return true
		}
	}
	return false
}

// wakeIslands wakes every sleeping dynamic body connected to an awake dynamic
// body through solid active contacts or joints.
func (w *World) wakeIslands() {
	if !w.hasSleepingDynamic() {
		return
	}
	w.stampBodies()
	parent := w.resetIslandParent()
	for _, rec := range w.activeContacts {
		if rec.isTrigger {
			continue
		}
		w.islandUnion(parent, rec.a, rec.b)
	}
	for _, j := range w.joints {
		if j == nil {
			continue
		}
		a, b := j.Bodies()
		w.islandUnion(parent, a, b)
	}
	awake := w.resetIslandAwake()
	for i, b := range w.bodies {
		if b.awakeDynamic() && !b.IsTrigger {
			awake[islandFindRoot(parent, i)] = 1
		}
	}
	for i, b := range w.bodies {
		if b != nil && !b.Static && !b.Kinematic && b.Sleeping && awake[islandFindRoot(parent, i)] != 0 {
			b.Wake()
		}
	}
}

// wakeIslandsOf wakes every sleeping dynamic body connected to any body in
// seeds through solid active contacts or joints, without waking unrelated
// sleeping islands.
func (w *World) wakeIslandsOf(seeds []*Body) {
	if len(seeds) == 0 || !w.hasSleepingDynamic() {
		return
	}
	w.stampBodies()
	parent := w.resetIslandParent()
	for _, rec := range w.activeContacts {
		if rec.isTrigger {
			continue
		}
		w.islandUnion(parent, rec.a, rec.b)
	}
	for _, j := range w.joints {
		if j == nil {
			continue
		}
		a, b := j.Bodies()
		w.islandUnion(parent, a, b)
	}
	awake := w.resetIslandAwake()
	for _, b := range seeds {
		if w.inWorld(b) && !b.isFixed() && !b.IsTrigger {
			awake[islandFindRoot(parent, b.islandIdx)] = 1
		}
	}
	for i, b := range w.bodies {
		if b != nil && !b.Static && !b.Kinematic && b.Sleeping && awake[islandFindRoot(parent, i)] != 0 {
			b.Wake()
		}
	}
}

// wakeDisturbed wakes every sleeping body disturbed since the last step
// (DisableSleep, AllowSleep disabled, direct velocity or pose edits, moved or
// removed static/kinematic contact or joint partners, voxel changes, or active
// joint motors/drives) and propagates wakeups across contact and joint islands.
func (w *World) wakeDisturbed(dtSec float32) {
	w.stampBodies()
	anySleeping := false
	for _, b := range w.bodies {
		if b == nil || b.Static || b.Kinematic || !b.Sleeping {
			continue
		}
		if w.DisableSleep || !b.allowsSleep() || b.Vel != (Vec3{}) || b.AngVel != (Vec3{}) || b.movedSinceStep() {
			b.Wake()
			continue
		}
		anySleeping = true
	}

	if anySleeping {
		for _, rec := range w.activeContacts {
			a, b := rec.a, rec.b
			inA, inB := w.inWorld(a), w.inWorld(b)
			if rec.isTrigger {
				if inA && inB {
					if a.IsTrigger && a.Area.Active() && (a.movedSinceStep() || a.Vel != (Vec3{}) || a.AngVel != (Vec3{})) {
						b.wakeIfSleeping()
					}
					if b.IsTrigger && b.Area.Active() && (b.movedSinceStep() || b.Vel != (Vec3{}) || b.AngVel != (Vec3{})) {
						a.wakeIfSleeping()
					}
				}
				continue
			}
			switch {
			case !inA && inB:
				b.wakeIfSleeping()
			case inA && !inB:
				a.wakeIfSleeping()
			case inA && inB:
				if a.isFixed() && (a.movedSinceStep() || a.movingKinematic()) {
					b.wakeIfSleeping()
				}
				if b.isFixed() && (b.movedSinceStep() || b.movingKinematic()) {
					a.wakeIfSleeping()
				}
			}
		}
		for _, j := range w.joints {
			if j == nil {
				continue
			}
			a, b := j.Bodies()
			if a == nil || b == nil {
				continue
			}
			inA, inB := w.inWorld(a), w.inWorld(b)
			switch {
			case !inA && inB:
				if a.havePose {
					b.wakeIfSleeping()
				}
			case inA && !inB:
				if b.havePose {
					a.wakeIfSleeping()
				}
			case inA && inB:
				if a.isFixed() && (a.movedSinceStep() || a.movingKinematic()) {
					b.wakeIfSleeping()
				}
				if b.isFixed() && (b.movedSinceStep() || b.movingKinematic()) {
					a.wakeIfSleeping()
				}
			}
		}
		w.wakeForVoxels()
	}

	// Prepare joints before velocity integration so active motors/springs wake
	// their bodies and islands.
	for _, j := range w.joints {
		j.Prepare(dtSec)
	}

	w.wakeIslands()
}

// wakeTouched wakes every sleeping dynamic body touched by an awake dynamic
// body (at any speed), a moving or moved kinematic body, a moved static body,
// or a moving/moved active area trigger among pairs, propagating each wakeup
// across islands until no more bodies wake. Because this runs before velocity
// integration and narrowphase contact collection, an awake body is never solved
// against a sleeping one.
func (w *World) wakeTouched(pairs []bpBodyPair, jointIgnored map[pairKey]struct{}) {
	if !w.hasSleepingDynamic() {
		return
	}
	for {
		woke := false
		for _, pair := range pairs {
			pA := &w.bp.proxies[w.bp.bodyProxies[pair.i]]
			pB := &w.bp.proxies[w.bp.bodyProxies[pair.j]]
			a, b := pA.body, pB.body
			if w.isPairIgnored(a.ID, b.ID) {
				continue
			}
			if jointIgnored != nil {
				pairK, _ := canonicalPairKey(a.ID, b.ID)
				if _, ok := jointIgnored[pairK]; ok {
					continue
				}
			}

			aCanWake := a.canWakeOther()
			bCanWake := b.canWakeOther()
			aSleeper := !a.Static && !a.Kinematic && !a.IsTrigger && a.Sleeping
			bSleeper := !b.Static && !b.Kinematic && !b.IsTrigger && b.Sleeping
			if !(aSleeper && bCanWake) && !(bSleeper && aCanWake) {
				continue
			}
			if !aabbOverlap(pA.worldBounds, pB.worldBounds) {
				continue
			}

			if a.Shape.Kind != ShapeCompound && b.Shape.Kind != ShapeCompound {
				if a.IsTrigger || b.IsTrigger {
					if aSleeper && !(b.IsTrigger && b.Area.Active() && (b.movedSinceStep() || b.Vel != (Vec3{}) || b.AngVel != (Vec3{}))) {
						continue
					}
					if bSleeper && !(a.IsTrigger && a.Area.Active() && (a.movedSinceStep() || a.Vel != (Vec3{}) || a.AngVel != (Vec3{}))) {
						continue
					}
				}
				if !CanCollide(a, b) {
					continue
				}
				if TestShapes(pA.worldShape, pB.worldShape).Hit {
					if aSleeper && bCanWake {
						a.Wake()
						woke = true
					}
					if bSleeper && aCanWake {
						b.Wake()
						woke = true
					}
				}
				continue
			}

			countA := a.childCount()
			countB := b.childCount()
			if countA == 0 || countB == 0 {
				continue
			}
			hitWake := false
			for ia := 0; ia < countA && !hitWake; ia++ {
				ci := -1
				shapeA := pA.worldShape
				if a.Shape.Kind == ShapeCompound {
					ci = ia
					shapeA = pA.worldShape.Children[ia].LocalShape()
				}
				layerA, maskA, trigA := a.childEffective(ci)
				boundsA := shapeA.Bounds()
				for ib := 0; ib < countB; ib++ {
					cj := -1
					shapeB := pB.worldShape
					if b.Shape.Kind == ShapeCompound {
						cj = ib
						shapeB = pB.worldShape.Children[ib].LocalShape()
					}
					layerB, maskB, trigB := b.childEffective(cj)
					isTrig := trigA || trigB
					if isTrig {
						if aSleeper && !(b.IsTrigger && b.Area.Active() && (b.movedSinceStep() || b.Vel != (Vec3{}) || b.AngVel != (Vec3{}))) {
							continue
						}
						if bSleeper && !(a.IsTrigger && a.Area.Active() && (a.movedSinceStep() || a.Vel != (Vec3{}) || a.AngVel != (Vec3{}))) {
							continue
						}
					}
					if !canCollideMasks(layerA, maskA, layerB, maskB) {
						continue
					}
					if !aabbOverlap(boundsA, shapeB.Bounds()) {
						continue
					}
					var c Contact
					if isTrig {
						c = TestShapes(shapeA, shapeB)
					} else {
						c = testShapesSolid(shapeA, shapeB)
					}
					if c.Hit {
						hitWake = true
						break
					}
				}
			}
			if hitWake {
				if aSleeper && bCanWake {
					a.Wake()
					woke = true
				}
				if bSleeper && aCanWake {
					b.Wake()
					woke = true
				}
			}
		}
		if !woke {
			break
		}
		w.wakeIslands()
	}
}

func isDrivenJoint(j Joint3D) bool {
	switch jt := j.(type) {
	case *HingeJoint:
		return jt != nil && jt.MotorMaxTorque > 0 && abs32(jt.MotorTargetVel) > 1e-6
	case *SliderJoint:
		return jt != nil && jt.MotorMaxForce > 0 && abs32(jt.MotorTargetVel) > 1e-6
	case *SixDOFJoint:
		if jt == nil {
			return false
		}
		for k := range 3 {
			if jt.Linear[k].Drive == DriveVelocity && abs32(jt.Linear[k].TargetVel) > 1e-6 {
				return true
			}
			if jt.Angular[k].Drive == DriveVelocity && abs32(jt.Angular[k].TargetVel) > 1e-6 {
				return true
			}
		}
	}
	return false
}

// updateSleep advances each awake dynamic body's rest timer and puts to sleep
// every contact/joint island whose bodies all allow sleep and have rested under
// the sleep thresholds for TimeToSleep seconds.
func (w *World) updateSleep(contacts []solverContact, prevPos []Vec3, dtSec float32) {
	if w.DisableSleep {
		for _, pid := range w.bp.dynamicList {
			b := w.bp.proxies[pid].body
			if b.awakeDynamic() {
				b.sleepTimer = 0
			}
		}
		return
	}

	linTol, angTol, timeToSleep := w.effectiveSleepThresholds()
	linTolSq := float32(linTol * linTol)
	angTolSq := float32(angTol * angTol)
	invDtSq := 1 / (dtSec * dtSec)

	candidate := false
	for di, pid := range w.bp.dynamicList {
		b := w.bp.proxies[pid].body
		if b.Static || b.Kinematic || b.Sleeping || b.IsTrigger {
			continue
		}
		if !b.allowsSleep() {
			b.sleepTimer = 0
			continue
		}
		dispSq := b.Pos.Sub(prevPos[di]).LenSq() * invDtSq
		// A body still sunk into a neighbor stays awake: asleep, the solver
		// would skip the pair and freeze that penetration in place.
		if b.Vel.LenSq() <= linTolSq && dispSq <= linTolSq && b.AngVel.LenSq() <= angTolSq && b.residualDepth <= sleepMaxDepth {
			b.sleepTimer += dtSec
			if b.sleepTimer >= timeToSleep {
				candidate = true
			}
		} else {
			b.sleepTimer = 0
		}
	}

	// Bodies driven by an active velocity motor never count as resting.
	for _, j := range w.joints {
		if !isDrivenJoint(j) {
			continue
		}
		a, b := j.Bodies()
		if a.awakeDynamic() {
			a.sleepTimer = 0
		}
		if b.awakeDynamic() {
			b.sleepTimer = 0
		}
	}

	// Bodies touching a moving or moved kinematic body never count as resting.
	for i := range contacts {
		c := &contacts[i]
		if c.b == nil {
			continue
		}
		if (c.a.movingKinematic() || c.a.movedKinematic()) && c.b.awakeDynamic() {
			c.b.sleepTimer = 0
		}
		if (c.b.movingKinematic() || c.b.movedKinematic()) && c.a.awakeDynamic() {
			c.a.sleepTimer = 0
		}
	}

	if !candidate {
		return
	}

	w.stampBodies()
	parent := w.resetIslandParent()
	for i := range contacts {
		c := &contacts[i]
		if c.b != nil {
			w.islandUnion(parent, c.a, c.b)
		}
	}
	for _, j := range w.joints {
		if j == nil {
			continue
		}
		a, b := j.Bodies()
		w.islandUnion(parent, a, b)
	}

	n := len(w.bodies)
	if cap(w.islandRest) < n {
		w.islandRest = make([]float32, n)
	} else {
		w.islandRest = w.islandRest[:n]
	}
	inf := float32(math.Inf(1))
	for i := range n {
		w.islandRest[i] = inf
	}
	for i, b := range w.bodies {
		if b.awakeDynamic() && !b.IsTrigger {
			r := islandFindRoot(parent, i)
			if b.sleepTimer < w.islandRest[r] {
				w.islandRest[r] = b.sleepTimer
			}
		}
	}
	for i, b := range w.bodies {
		if b.awakeDynamic() && !b.IsTrigger && w.islandRest[islandFindRoot(parent, i)] >= timeToSleep {
			b.Sleeping = true
			b.Vel = Vec3{}
			b.AngVel = Vec3{}
		}
	}
}
