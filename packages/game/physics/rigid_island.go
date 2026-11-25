package physics

import (
	"math"
	"slices"
)

// Sleep defaults for RigidWorld, in NSGo's pixel units.
const (
	// DefaultSleepLinearThreshold is the speed, in pixels per second, below which a rigid body counts as resting.
	DefaultSleepLinearThreshold = 2
	// DefaultSleepAngularThreshold is the angular speed, in radians per second, below which a rigid body counts as resting (2 degrees per second, as in Box2D).
	DefaultSleepAngularThreshold = 2 * math.Pi / 180
	// DefaultTimeToSleep is how many seconds every body of an island must rest before the island sleeps.
	DefaultTimeToSleep = 0.5
)

// rigidPose is a body's position and angle.
type rigidPose struct {
	x, y, angle float32
}

// rigidPairKey names one contact manifold across steps: the pair of bodies, in the order the broadphase reports them, and the manifold's index within the pair.
type rigidPairKey struct {
	a, b *RigidBody
	sub  int
}

// rigidCachedPoint is the accumulated normal and friction impulse of one contact point, named by its feature id.
type rigidCachedPoint struct {
	id     uint32
	pn, pt float64
}

// rigidCachedManifold holds the impulses a manifold ended its step with.
type rigidCachedManifold struct {
	key    rigidPairKey
	count  int
	points [2]rigidCachedPoint
}

// rigidProxy is a body's bounding box in the broadphase.
type rigidProxy struct {
	minX, minY, maxX, maxY float64
	index                  int
}

// rigidScratch holds buffers Step reuses so a world in steady state allocates nothing per step. None of it is simulation state.
type rigidScratch struct {
	proxies       []rigidProxy
	wild          []int
	pairs         []uint64
	manifolds     []rigidManifold
	pairManifolds []rigidManifold
	starts        []rigidPose
	statics       []int
	ccdContacts   []ccdContact
	ccdCandidates []ccdCandidate
	ccdSupports   []ccdSupport
	joints        []Joint
	contacts      []rigidCachedManifold
	contactIndex  map[rigidPairKey]int
	parent        []int
	islandRest    []float64
}

// awakeDynamic reports whether b is in the current step and is a dynamic body that is awake.
func (b *RigidBody) awakeDynamic() bool {
	return b != nil && !b.Static && !b.sleeping
}

// wakeIfSleeping wakes b when it is asleep.
func (b *RigidBody) wakeIfSleeping() {
	if b.sleeping {
		b.Wake()
	}
}

// movedSinceStep reports whether b's pose differs from the one it ended its last step with.
func (b *RigidBody) movedSinceStep() bool {
	return b.restValid && (b.X != b.restX || b.Y != b.restY || b.Angle != b.restAngle)
}

// inWorld reports whether b is one of the bodies the current Step of w visits.
func (w *RigidWorld) inWorld(b *RigidBody) bool {
	return b != nil && b.stamp == w.stamp && b.index < len(w.Bodies) && w.Bodies[b.index] == b
}

// broadphase returns, as i<<32 | j sorted ascending, every pair i < j of w.Bodies whose bounding boxes overlap and which may collide: neither shape empty, not both static, and layers that interact. This is the set and order of pairs the brute-force double loop over the bodies would test, found by sorting the boxes along X and sweeping. A body whose box has a NaN coordinate cannot be sorted, so it is tested against every other body with the double loop's box test.
func (w *RigidWorld) broadphase() []uint64 {
	proxies := w.scratch.proxies[:0]
	wild := w.scratch.wild[:0]
	for i, b := range w.Bodies {
		if b == nil || b.Shape.Empty() {
			continue
		}
		minX, minY, maxX, maxY := b.worldAABB()
		if math.IsNaN(minX) || math.IsNaN(minY) || math.IsNaN(maxX) || math.IsNaN(maxY) {
			wild = append(wild, i)
			continue
		}
		proxies = append(proxies, rigidProxy{minX: minX, minY: minY, maxX: maxX, maxY: maxY, index: i})
	}
	// Ties on minX fall back to the body index, so the order is total and the sort gives the same result on every run.
	slices.SortFunc(proxies, func(p, q rigidProxy) int {
		switch {
		case p.minX < q.minX:
			return -1
		case p.minX > q.minX:
			return 1
		}
		return p.index - q.index
	})

	pairs := w.scratch.pairs[:0]
	addPair := func(i, j int) {
		if i > j {
			i, j = j, i
		}
		a, b := w.Bodies[i], w.Bodies[j]
		if (a.Static && b.Static) || !CanCollideRigid(a, b) {
			return
		}
		pairs = append(pairs, uint64(i)<<32|uint64(j))
	}
	for k := range proxies {
		p := &proxies[k]
		for m := k + 1; m < len(proxies); m++ {
			q := &proxies[m]
			if !(q.minX < p.maxX) {
				break
			}
			// The same test the double loop makes, so boxes that only touch are culled alike.
			if p.maxX <= q.minX || q.maxX <= p.minX || p.maxY <= q.minY || q.maxY <= p.minY {
				continue
			}
			addPair(p.index, q.index)
		}
	}
	// A box with a NaN coordinate fails every comparison along that axis, so the test culls it only along the other one.
	culled := func(i, j int) bool {
		aMinX, aMinY, aMaxX, aMaxY := w.Bodies[i].worldAABB()
		bMinX, bMinY, bMaxX, bMaxY := w.Bodies[j].worldAABB()
		return aMaxX <= bMinX || bMaxX <= aMinX || aMaxY <= bMinY || bMaxY <= aMinY
	}
	for wi, i := range wild {
		for _, p := range proxies {
			if !culled(i, p.index) {
				addPair(i, p.index)
			}
		}
		for _, j := range wild[wi+1:] {
			if !culled(i, j) {
				addPair(i, j)
			}
		}
	}
	slices.Sort(pairs)
	w.scratch.proxies, w.scratch.wild, w.scratch.pairs = proxies, wild, pairs
	return pairs
}

// unionFind resets the island forest over len(w.Bodies) bodies.
func (w *RigidWorld) unionFind() []int {
	parent := w.scratch.parent[:0]
	for i := range w.Bodies {
		parent = append(parent, i)
	}
	w.scratch.parent = parent
	return parent
}

func findRoot(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}
	return i
}

// union joins the islands of two bodies of the current step when both are dynamic; static bodies never link islands.
func (w *RigidWorld) union(parent []int, a, b *RigidBody) {
	if !w.inWorld(a) || !w.inWorld(b) || a.Static || b.Static {
		return
	}
	ra, rb := findRoot(parent, a.index), findRoot(parent, b.index)
	// The smaller index becomes the root, so the forest is the same however the links arrive.
	if ra < rb {
		parent[rb] = ra
	} else if rb < ra {
		parent[ra] = rb
	}
}

// wakeDisturbed wakes every sleeping body that something changed since the last step, then the islands they belong to. A sleeping body is disturbed when it was moved, given a velocity or a pending force, when the gravity, damping or fluid of the areas it is in changed, or its gravity scale did, when a body it touched left the world, or when a static body it touched moved. With DisableSleep set, every body wakes.
func (w *RigidWorld) wakeDisturbed(areas []activeAreaEntry) {
	anySleeping := false
	for _, b := range w.Bodies {
		if b == nil || !b.sleeping {
			continue
		}
		if w.DisableSleep || b.movedSinceStep() || b.VX != 0 || b.VY != 0 || b.AngularVel != 0 || b.forceX != 0 || b.forceY != 0 || b.torque != 0 || w.restEnvironment(b, areas) != b.sleepEnv {
			b.Wake()
			continue
		}
		anySleeping = true
	}
	if !anySleeping {
		return
	}
	for i := range w.contacts {
		a, b := w.contacts[i].key.a, w.contacts[i].key.b
		inA, inB := w.inWorld(a), w.inWorld(b)
		switch {
		case !inA && inB:
			b.wakeIfSleeping()
		case inA && !inB:
			a.wakeIfSleeping()
		case inA && inB:
			if a.Static && a.movedSinceStep() {
				b.wakeIfSleeping()
			}
			if b.Static && b.movedSinceStep() {
				a.wakeIfSleeping()
			}
		}
	}
	w.wakeIslands()
}

// wakeIslands wakes the bodies of every joint that drives them (drivenJoint) and every sleeping body linked to an awake dynamic body through last step's contacts or a joint. Those links are the islands the bodies fell asleep in, so waking one body wakes its whole island.
func (w *RigidWorld) wakeIslands() {
	for _, j := range w.Joints {
		if drivenJoint(j) {
			a, b := jointPair(j)
			for _, d := range [2]*RigidBody{a, b} {
				if d != nil {
					d.wakeIfSleeping()
				}
			}
		}
	}
	parent := w.unionFind()
	for i := range w.contacts {
		w.union(parent, w.contacts[i].key.a, w.contacts[i].key.b)
	}
	for _, j := range w.Joints {
		if j != nil {
			a, b := jointPair(j)
			w.union(parent, a, b)
		}
	}
	awake := w.scratch.islandRest[:0]
	for range w.Bodies {
		awake = append(awake, 0)
	}
	w.scratch.islandRest = awake
	for i, b := range w.Bodies {
		if b.awakeDynamic() {
			awake[findRoot(parent, i)] = 1
		}
	}
	for i, b := range w.Bodies {
		if b != nil && b.sleeping && awake[findRoot(parent, i)] == 1 {
			b.Wake()
		}
	}
}

// wakeTouched wakes every sleeping body that an awake dynamic body touches among the candidate pairs, and the islands of those bodies, repeating until no more wake.
func (w *RigidWorld) wakeTouched(pairs []uint64) {
	buf := w.scratch.pairManifolds[:0]
	for {
		woke := false
		for _, key := range pairs {
			a, b := w.Bodies[int(key>>32)], w.Bodies[int(uint32(key))]
			var sleeper *RigidBody
			switch {
			case a.sleeping && b.awakeDynamic():
				sleeper = a
			case b.sleeping && a.awakeDynamic():
				sleeper = b
			default:
				continue
			}
			buf = collideRigidPair(a, b, buf[:0])
			if len(buf) > 0 {
				sleeper.Wake()
				woke = true
			}
		}
		if !woke {
			break
		}
		w.wakeIslands()
	}
	w.scratch.pairManifolds = buf
}

// jointPair returns the bodies joint j connects; a joint holding one body, like a MouseJoint, returns nil for the other.
func jointPair(j Joint) (a, b *RigidBody) {
	if jb, ok := j.(jointBodies); ok {
		return jb.jointBodyPair()
	}
	return nil, nil
}

// drivenJoint reports whether j drives its bodies however still they are: a MouseJoint that pulls with force, or a RevoluteJoint, PrismaticJoint or WheelJoint whose motor is on. Their targets are fields a game may change on any step without telling the world, so the bodies they drive stay awake while the drive is on.
func drivenJoint(j Joint) bool {
	switch j := j.(type) {
	case *MouseJoint:
		return j != nil && j.MaxForce > 0
	case *RevoluteJoint:
		return j != nil && j.EnableMotor && j.MaxMotorTorque > 0
	case *PrismaticJoint:
		return j != nil && j.EnableMotor && j.MaxMotorForce > 0
	case *WheelJoint:
		return j != nil && j.EnableMotor && j.MaxMotorTorque > 0
	}
	return false
}

// awakeJoints returns the joints with at least one awake dynamic body, the ones Step solves.
func (w *RigidWorld) awakeJoints() []Joint {
	joints := w.scratch.joints[:0]
	for _, j := range w.Joints {
		if j == nil {
			continue
		}
		a, b := jointPair(j)
		if a.awakeDynamic() || b.awakeDynamic() {
			joints = append(joints, j)
		}
	}
	w.scratch.joints = joints
	return joints
}

// warmStart starts each contact point from the impulses the same pair of features ended the last step with, scaled to the new step length, and applies them to the bodies, so resting stacks keep their support from one step to the next instead of rebuilding it from zero.
func (w *RigidWorld) warmStart(manifolds []rigidManifold, h float64) {
	if len(w.contacts) == 0 || w.lastDt <= 0 {
		return
	}
	index := w.scratch.contactIndex
	if index == nil {
		index = make(map[rigidPairKey]int, len(w.contacts))
		w.scratch.contactIndex = index
	}
	clear(index)
	for i := range w.contacts {
		index[w.contacts[i].key] = i
	}
	ratio := h / w.lastDt
	for mi := range manifolds {
		m := &manifolds[mi]
		ci, ok := index[rigidPairKey{a: m.a, b: m.b, sub: m.sub}]
		if !ok {
			continue
		}
		cached := &w.contacts[ci]
		tx, ty := -m.ny, m.nx
		for i := range m.count {
			cp := &m.points[i]
			for _, old := range cached.points[:cached.count] {
				if old.id != cp.id {
					continue
				}
				cp.pn = old.pn * ratio
				cp.pt = old.pt * ratio
				px := float64(cp.pn*m.nx) + float64(cp.pt*tx)
				py := float64(cp.pn*m.ny) + float64(cp.pt*ty)
				m.a.applyImpulseRaw(-px, -py, cp.rAx, cp.rAy)
				m.b.applyImpulseRaw(px, py, cp.rBx, cp.rBy)
				break
			}
		}
	}
}

// storeContacts replaces the contact cache with this step's manifolds and their impulses, keeping the entries of pairs that are both asleep or static, which this step did not solve.
func (w *RigidWorld) storeContacts(manifolds []rigidManifold, h float64) {
	next := w.scratch.contacts[:0]
	for mi := range manifolds {
		m := &manifolds[mi]
		c := rigidCachedManifold{key: rigidPairKey{a: m.a, b: m.b, sub: m.sub}, count: m.count}
		for i := range m.count {
			c.points[i] = rigidCachedPoint{id: m.points[i].id, pn: m.points[i].pn, pt: m.points[i].pt}
		}
		next = append(next, c)
	}
	for _, c := range w.contacts {
		a, b := c.key.a, c.key.b
		if w.inWorld(a) && w.inWorld(b) && !a.awakeDynamic() && !b.awakeDynamic() {
			next = append(next, c)
		}
	}
	w.scratch.contacts = w.contacts
	w.contacts = next
	w.lastDt = h
}

// updateSleep advances each awake body's rest time and puts to sleep every island, linked by this step's contacts and joints, whose bodies have all rested for TimeToSleep.
func (w *RigidWorld) updateSleep(manifolds []rigidManifold, joints []Joint, areas []activeAreaEntry, h float64) {
	if w.DisableSleep {
		return
	}
	linTol := float64(w.SleepLinearThreshold)
	if !(linTol > 0) || math.IsInf(linTol, 0) {
		linTol = DefaultSleepLinearThreshold
	}
	angTol := float64(w.SleepAngularThreshold)
	if !(angTol > 0) || math.IsInf(angTol, 0) {
		angTol = DefaultSleepAngularThreshold
	}
	timeToSleep := float64(w.TimeToSleep)
	if !(timeToSleep > 0) || math.IsInf(timeToSleep, 0) {
		timeToSleep = DefaultTimeToSleep
	}
	linTolSq := float64(linTol * linTol)
	angTolSq := float64(angTol * angTol)
	candidate := false
	for _, b := range w.Bodies {
		if !b.awakeDynamic() {
			continue
		}
		vx, vy, av := float64(b.VX), float64(b.VY), float64(b.AngularVel)
		if float64(vx*vx)+float64(vy*vy) > linTolSq || float64(av*av) > angTolSq {
			b.sleepTime = 0
			continue
		}
		b.sleepTime += h
		if b.sleepTime >= timeToSleep {
			candidate = true
		}
	}
	// A body a joint drives never counts as resting: the drive's target can change at any time.
	for _, j := range joints {
		if drivenJoint(j) {
			a, b := jointPair(j)
			for _, d := range [2]*RigidBody{a, b} {
				if d.awakeDynamic() {
					d.sleepTime = 0
				}
			}
		}
	}
	if !candidate {
		return
	}

	parent := w.unionFind()
	for mi := range manifolds {
		w.union(parent, manifolds[mi].a, manifolds[mi].b)
	}
	for _, j := range joints {
		a, b := jointPair(j)
		w.union(parent, a, b)
	}
	// islandRest[root] is the shortest rest time in the island, the time the whole island has rested.
	islandRest := w.scratch.islandRest[:0]
	for range w.Bodies {
		islandRest = append(islandRest, math.Inf(1))
	}
	w.scratch.islandRest = islandRest
	for i, b := range w.Bodies {
		if b.awakeDynamic() {
			r := findRoot(parent, i)
			islandRest[r] = min(islandRest[r], b.sleepTime)
		}
	}
	for i, b := range w.Bodies {
		if b.awakeDynamic() && islandRest[findRoot(parent, i)] >= timeToSleep {
			b.sleeping = true
			b.VX, b.VY, b.AngularVel = 0, 0, 0
			b.sleepEnv = w.restEnvironment(b, areas)
		}
	}
}

// restEnvironment returns the gravity, damping and fluid that the world and its areas apply to b at its current pose, with gravity scaled by b's gravity scale. A sleeping body keeps the one it fell asleep in, and wakes when it changes.
func (w *RigidWorld) restEnvironment(b *RigidBody, areas []activeAreaEntry) AreaEffect {
	eff := w.evaluateRigidBodyAreas(b, areas)
	scale := b.EffectiveGravityScale()
	eff.GravityX *= scale
	eff.GravityY *= scale
	eff.BuoyancyAccelX *= scale
	eff.BuoyancyAccelY *= scale
	return eff
}
