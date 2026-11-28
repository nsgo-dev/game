package physics

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// jointPushBiasFraction is the fraction of anchor position error closed per
	// substep via pseudo-velocities (zero velocity injection). Over 4 substeps,
	// 0.8 closes 1 - 0.2^4 = 99.84% of nonlinear chord drift with no overshoot.
	jointPushBiasFraction = 0.8
	// jointMaxPushSpeed caps linear joint push-out pseudo-velocity in pixels/s.
	jointMaxPushSpeed = 2000.0
	// jointMaxAngPushSpeed caps angular limit/lock push-out pseudo-velocity in rad/s.
	jointMaxAngPushSpeed = 25.0
)

// anchorJoint2D is implemented by built-in 2D joints whose primary 2-DOF
// point-to-point anchor constraint can be solved directly as part of the O(N)
// spanning-forest tree solver.
type anchorJoint2D interface {
	anchorBodies2D() (a, b *RigidBody, anchorA, anchorB core.Point)
	anchorSoftness2D() (biasRate, gammaScale float64, soft bool)
	// anchorSoftAt2D reports whether the joint's public Softness makes its anchor soft for substep length dt, without reading solver state from an earlier step.
	anchorSoftAt2D(dt float64) bool
	anchorAccImpulse2D() (ix, iy float64)
	addAnchorImpulse2D(px, py float64, recordReaction bool)
}

type jointTreeEdge2D struct {
	jointIdx int
	joint    anchorJoint2D
	bodyA    *RigidBody
	bodyB    *RigidBody
	anchorA  core.Point
	anchorB  core.Point
	nodeA    int // 0 for fixed/static/kinematic, body.index+1 for awake dynamic
	nodeB    int
	soft     bool
}

type jointTreeOrder2D struct {
	edgeIdx    int
	parentNode int // 0 for fixed root, or body.index+1 for dynamic parent
	childNode  int // always > 0 (body.index+1 for dynamic child)
	flipped    bool
}

// sym3_64 is a symmetric 3x3 matrix for 2D articulated inverse spatial inertia.
// Layout: [w00 w01 w02; w01 w11 w12; w02 w12 w22].
type sym3_64 struct {
	w00, w01, w02 float64
	w11, w12, w22 float64
}

// jointTree2D solves spanning forests of 2D bilateral point-anchor constraints
// in O(N) time using exact Woodbury inverse-inertia elimination (Baraff /
// Featherstone impulse form) with zero steady-state allocations.
type jointTree2D struct {
	ufParent    []int
	adjHead     []int
	adjNext     []int
	adjTo       []int
	adjEdge     []int
	bfsQueue    []int
	visited     []bool
	inTree      []bool
	edges       []jointTreeEdge2D
	velOrder    []jointTreeOrder2D
	rigidOrder  []jointTreeOrder2D
	hasSoft     bool
	wMat        []sym3_64
	dVec        [][3]float64
	qVec        [][3]float64
	touched     []int
	touchedMark []int
	touchStamp  int
	lam0        [][2]float64
	gain        [][2][3]float64
	rP          [][2]float64
	rC          [][2]float64
}

func (t *jointTree2D) build(w *RigidWorld, joints []Joint, subH float64) {
	t.edges = t.edges[:0]
	t.velOrder = t.velOrder[:0]
	t.rigidOrder = t.rigidOrder[:0]
	t.hasSoft = false

	nj := len(joints)
	if cap(t.inTree) < nj {
		t.inTree = make([]bool, nj)
	} else {
		t.inTree = t.inTree[:nj]
		clear(t.inTree)
	}
	if nj == 0 || len(w.Bodies) == 0 {
		return
	}

	// Collect eligible bilateral 2-DOF anchor joints whose bodies are in w.
	for ji, j := range joints {
		aj, ok := j.(anchorJoint2D)
		if !ok {
			continue
		}
		a, b, ancA, ancB := aj.anchorBodies2D()
		if a == nil || b == nil || a == b {
			continue
		}
		if !w.inWorld(a) || !w.inWorld(b) {
			continue
		}
		aDyn := a.awakeDynamic()
		bDyn := b.awakeDynamic()
		if !aDyn && !bDyn {
			continue
		}
		nodeA, nodeB := 0, 0
		if aDyn {
			nodeA = a.index + 1
		}
		if bDyn {
			nodeB = b.index + 1
		}
		soft := aj.anchorSoftAt2D(subH)
		if soft {
			t.hasSoft = true
		}
		t.edges = append(t.edges, jointTreeEdge2D{
			jointIdx: ji,
			joint:    aj,
			bodyA:    a,
			bodyB:    b,
			anchorA:  ancA,
			anchorB:  ancB,
			nodeA:    nodeA,
			nodeB:    nodeB,
			soft:     soft,
		})
	}
	if len(t.edges) == 0 {
		return
	}

	nNodes := len(w.Bodies) + 1
	t.velOrder = t.buildOrder(nNodes, false, true, t.velOrder[:0])
	if t.hasSoft {
		t.rigidOrder = t.buildOrder(nNodes, true, false, t.rigidOrder[:0])
	}
}

func (t *jointTree2D) buildOrder(nNodes int, skipSoft, markInTree bool, out []jointTreeOrder2D) []jointTreeOrder2D {
	if cap(t.ufParent) < nNodes {
		t.ufParent = make([]int, nNodes)
	} else {
		t.ufParent = t.ufParent[:nNodes]
	}
	for i := range nNodes {
		t.ufParent[i] = i
	}
	if cap(t.adjHead) < nNodes {
		t.adjHead = make([]int, nNodes)
	} else {
		t.adjHead = t.adjHead[:nNodes]
	}
	for i := range nNodes {
		t.adjHead[i] = -1
	}
	t.adjNext = t.adjNext[:0]
	t.adjTo = t.adjTo[:0]
	t.adjEdge = t.adjEdge[:0]

	addAdj := func(u, v, ei int) {
		idx := len(t.adjTo)
		t.adjTo = append(t.adjTo, v)
		t.adjEdge = append(t.adjEdge, ei)
		t.adjNext = append(t.adjNext, t.adjHead[u])
		t.adjHead[u] = idx
	}

	// Spanning forest via union-find over nodes 0..nBodies (0 = all fixed bodies).
	// Because all fixed bodies map to node 0, every component has at most one fixed
	// root and zero cycles, guaranteeing every eliminated K matrix is strictly SPD.
	treeCount := 0
	for ei := range t.edges {
		e := &t.edges[ei]
		if skipSoft && e.soft {
			continue
		}
		ra := findRoot(t.ufParent, e.nodeA)
		rb := findRoot(t.ufParent, e.nodeB)
		if ra == rb {
			continue
		}
		if ra < rb {
			t.ufParent[rb] = ra
		} else {
			t.ufParent[ra] = rb
		}
		addAdj(e.nodeA, e.nodeB, ei)
		addAdj(e.nodeB, e.nodeA, ei)
		if markInTree {
			t.inTree[e.jointIdx] = true
		}
		treeCount++
	}
	if treeCount == 0 {
		return out
	}

	if cap(t.visited) < nNodes {
		t.visited = make([]bool, nNodes)
	} else {
		t.visited = t.visited[:nNodes]
		clear(t.visited)
	}
	t.bfsQueue = t.bfsQueue[:0]

	// BFS from node 0 first (rooted at static/kinematic anchors), then any
	// remaining free-floating trees rooted at their lowest body index.
	bfsFrom := func(root int) {
		start := len(out)
		t.visited[root] = true
		t.bfsQueue = append(t.bfsQueue[:0], root)
		for head := 0; head < len(t.bfsQueue); head++ {
			u := t.bfsQueue[head]
			for ai := t.adjHead[u]; ai >= 0; ai = t.adjNext[ai] {
				v := t.adjTo[ai]
				if t.visited[v] {
					continue
				}
				t.visited[v] = true
				t.bfsQueue = append(t.bfsQueue, v)
				ei := t.adjEdge[ai]
				e := &t.edges[ei]
				out = append(out, jointTreeOrder2D{
					edgeIdx:    ei,
					parentNode: u,
					childNode:  v,
					flipped:    e.nodeA == v,
				})
			}
		}
		// Reverse this component's BFS edges so leaves come first and root comes last.
		for lo, hi := start, len(out)-1; lo < hi; lo, hi = lo+1, hi-1 {
			out[lo], out[hi] = out[hi], out[lo]
		}
	}

	if t.adjHead[0] >= 0 {
		bfsFrom(0)
	}
	for u := 1; u < nNodes; u++ {
		if t.adjHead[u] >= 0 && !t.visited[u] {
			bfsFrom(u)
		}
	}
	return out
}

type jointTreeSolveMode int

const (
	jointTreeVelocity jointTreeSolveMode = iota
	jointTreePushOut
	jointTreeRelax
)

func (t *jointTree2D) solve(w *RigidWorld, deltas []rigidBodyDelta, mode jointTreeSolveMode, subH float64) {
	order := t.velOrder
	if mode != jointTreeVelocity && t.hasSoft {
		order = t.rigidOrder
	}
	m := len(order)
	if m == 0 {
		return
	}

	nBodies := len(w.Bodies)
	if cap(t.wMat) < nBodies {
		t.wMat = make([]sym3_64, nBodies)
	} else {
		t.wMat = t.wMat[:nBodies]
	}
	if cap(t.dVec) < nBodies {
		t.dVec = make([][3]float64, nBodies)
	} else {
		t.dVec = t.dVec[:nBodies]
	}
	if cap(t.qVec) < nBodies {
		t.qVec = make([][3]float64, nBodies)
	} else {
		t.qVec = t.qVec[:nBodies]
	}
	if cap(t.touchedMark) < nBodies {
		t.touchedMark = make([]int, nBodies)
	} else {
		t.touchedMark = t.touchedMark[:nBodies]
	}
	t.touchStamp++
	if t.touchStamp == 0 {
		clear(t.touchedMark)
		t.touchStamp = 1
	}
	stamp := t.touchStamp
	t.touched = t.touched[:0]

	touchBody := func(bi int) {
		if t.touchedMark[bi] == stamp {
			return
		}
		t.touchedMark[bi] = stamp
		t.touched = append(t.touched, bi)
		b := w.Bodies[bi]
		invM := float64(b.InvMass)
		invI := float64(b.InvInertia)
		t.wMat[bi] = sym3_64{w00: invM, w11: invM, w22: invI}
		t.dVec[bi] = [3]float64{}
		t.qVec[bi] = [3]float64{}
	}

	if cap(t.lam0) < m {
		t.lam0 = make([][2]float64, m)
		t.gain = make([][2][3]float64, m)
		t.rP = make([][2]float64, m)
		t.rC = make([][2]float64, m)
	} else {
		t.lam0 = t.lam0[:m]
		t.gain = t.gain[:m]
		t.rP = t.rP[:m]
		t.rC = t.rC[:m]
	}

	pushBiasRate := 0.0
	if mode == jointTreePushOut && subH > 0 {
		pushBiasRate = jointPushBiasFraction / subH
	}

	// Pass 1: bottom-up exact Woodbury elimination (leaves -> roots).
	for oi := range m {
		ord := order[oi]
		e := &t.edges[ord.edgeIdx]

		bodyP, bodyC := e.bodyA, e.bodyB
		ancP, ancC := e.anchorA, e.anchorB
		if ord.flipped {
			bodyP, bodyC = e.bodyB, e.bodyA
			ancP, ancC = e.anchorB, e.anchorA
		}

		ci := ord.childNode - 1
		touchBody(ci)
		pi := ord.parentNode - 1
		if pi >= 0 {
			touchBody(pi)
		}

		pP := bodyP.LocalToWorld(ancP)
		pC := bodyC.LocalToWorld(ancC)
		rPx := float64(pP.X) - float64(bodyP.X)
		rPy := float64(pP.Y) - float64(bodyP.Y)
		rCx := float64(pC.X) - float64(bodyC.X)
		rCy := float64(pC.Y) - float64(bodyC.Y)
		t.rP[oi] = [2]float64{rPx, rPy}
		t.rC[oi] = [2]float64{rCx, rCy}

		// Child B_c = J_c * W_c and K_c = B_c * J_c^T.
		wc := &t.wMat[ci]
		bc00 := wc.w00 - float64(rCy*wc.w02)
		bc01 := wc.w01 - float64(rCy*wc.w12)
		bc02 := wc.w02 - float64(rCy*wc.w22)
		bc11 := wc.w11 + float64(rCx*wc.w12)
		bc12 := wc.w12 + float64(rCx*wc.w22)

		k00 := bc00 - float64(rCy*bc02)
		k01 := bc01 + float64(rCx*bc02)
		k11 := bc11 + float64(rCx*bc12)

		// Parent B_p = J_p * W_p and K_p = B_p * J_p^T (zero if parent is fixed).
		var bp00, bp01, bp02, bp10, bp11, bp12 float64
		if pi >= 0 {
			wp := &t.wMat[pi]
			bp00 = wp.w00 - float64(rPy*wp.w02)
			bp01 = wp.w01 - float64(rPy*wp.w12)
			bp02 = wp.w02 - float64(rPy*wp.w22)
			bp10 = wp.w01 + float64(rPx*wp.w02)
			bp11 = wp.w11 + float64(rPx*wp.w12)
			bp12 = wp.w12 + float64(rPx*wp.w22)

			k00 += bp00 - float64(rPy*bp02)
			k01 += bp01 + float64(rPx*bp02)
			k11 += bp11 + float64(rPx*bp12)
		}

		// Optional soft joint compliance Gamma = gammaScale * K_local and soft bias.
		var biasX, biasY float64
		var g00, g01, g11 float64
		if mode == jointTreeVelocity && e.soft {
			biasRate, gammaScale, soft := e.joint.anchorSoftness2D()
			if soft {
				errX := float64(pC.X) - float64(pP.X)
				errY := float64(pC.Y) - float64(pP.Y)
				biasX = float64(biasRate * errX)
				biasY = float64(biasRate * errY)

				invMC, invIC := float64(bodyC.InvMass), float64(bodyC.InvInertia)
				invMP, invIP := float64(bodyP.InvMass), float64(bodyP.InvInertia)
				kl00 := float64(invMC+invMP) + float64(float64(invIC*rCy)*rCy) + float64(float64(invIP*rPy)*rPy)
				kl01 := float64(float64(-invIC*rCx)*rCy) - float64(float64(invIP*rPx)*rPy)
				kl11 := float64(invMC+invMP) + float64(float64(invIC*rCx)*rCx) + float64(float64(invIP*rPx)*rPx)
				g00 = float64(gammaScale * kl00)
				g01 = float64(gammaScale * kl01)
				g11 = float64(gammaScale * kl11)
				k00 += g00
				k01 += g01
				k11 += g11
			}
		} else if mode == jointTreePushOut {
			errX := float64(pC.X) - float64(pP.X)
			errY := float64(pC.Y) - float64(pP.Y)
			biasX = float64(pushBiasRate * errX)
			biasY = float64(pushBiasRate * errY)
			speedSq := float64(biasX*biasX) + float64(biasY*biasY)
			if speedSq > float64(jointMaxPushSpeed*jointMaxPushSpeed) {
				scale := jointMaxPushSpeed / math.Sqrt(speedSq)
				biasX *= scale
				biasY *= scale
			}
		}

		det := float64(k00*k11) - float64(k01*k01)
		if det <= 1e-14 {
			t.lam0[oi] = [2]float64{}
			t.gain[oi] = [2][3]float64{}
			continue
		}
		invDet := 1.0 / det
		m00 := k11 * invDet
		m01 := -k01 * invDet
		m11 := k00 * invDet

		// Predicted child and parent anchor velocities.
		var uCx, uCy, uCw float64
		var uPx, uPy, uPw float64
		if mode == jointTreePushOut {
			dC := &deltas[ci]
			uCx = dC.pvx + t.dVec[ci][0]
			uCy = dC.pvy + t.dVec[ci][1]
			uCw = dC.pav + t.dVec[ci][2]
			if pi >= 0 {
				dP := &deltas[pi]
				uPx = dP.pvx + t.dVec[pi][0]
				uPy = dP.pvy + t.dVec[pi][1]
				uPw = dP.pav + t.dVec[pi][2]
			}
		} else {
			uCx = float64(bodyC.VX) + t.dVec[ci][0]
			uCy = float64(bodyC.VY) + t.dVec[ci][1]
			uCw = float64(bodyC.AngularVel) + t.dVec[ci][2]
			if pi >= 0 {
				uPx = float64(bodyP.VX) + t.dVec[pi][0]
				uPy = float64(bodyP.VY) + t.dVec[pi][1]
				uPw = float64(bodyP.AngularVel) + t.dVec[pi][2]
			} else {
				// Fixed kinematic parent can still carry velocity.
				uPx = float64(bodyP.VX)
				uPy = float64(bodyP.VY)
				uPw = float64(bodyP.AngularVel)
			}
		}

		vCx := uCx - float64(uCw*rCy)
		vCy := uCy + float64(uCw*rCx)
		vPx := uPx - float64(uPw*rPy)
		vPy := uPy + float64(uPw*rPx)

		resX := -((vCx - vPx) + biasX)
		resY := -((vCy - vPy) + biasY)
		if g00 != 0 || g11 != 0 {
			accX, accY := e.joint.anchorAccImpulse2D()
			if ord.flipped {
				accX, accY = -accX, -accY
			}
			resX -= float64(g00*accX) + float64(g01*accY)
			resY -= float64(g01*accX) + float64(g11*accY)
		}

		l0x := float64(m00*resX) + float64(m01*resY)
		l0y := float64(m01*resX) + float64(m11*resY)
		t.lam0[oi] = [2]float64{l0x, l0y}

		if pi >= 0 {
			// Gain G_e = K_e^{-1} * B_p (2x3).
			g0_0 := float64(m00*bp00) + float64(m01*bp10)
			g0_1 := float64(m00*bp01) + float64(m01*bp11)
			g0_2 := float64(m00*bp02) + float64(m01*bp12)
			g1_0 := float64(m01*bp00) + float64(m11*bp10)
			g1_1 := float64(m01*bp01) + float64(m11*bp11)
			g1_2 := float64(m01*bp02) + float64(m11*bp12)
			t.gain[oi] = [2][3]float64{
				{g0_0, g0_1, g0_2},
				{g1_0, g1_1, g1_2},
			}

			// d_p -= B_p^T * lam0
			dp := &t.dVec[pi]
			dp[0] -= float64(bp00*l0x) + float64(bp10*l0y)
			dp[1] -= float64(bp01*l0x) + float64(bp11*l0y)
			dp[2] -= float64(bp02*l0x) + float64(bp12*l0y)

			// W_p -= B_p^T * G_e (symmetric 3x3 Woodbury downdate)
			wp := &t.wMat[pi]
			wp.w00 -= float64(bp00*g0_0) + float64(bp10*g1_0)
			wp.w01 -= float64(bp00*g0_1) + float64(bp10*g1_1)
			wp.w02 -= float64(bp00*g0_2) + float64(bp10*g1_2)
			wp.w11 -= float64(bp01*g0_1) + float64(bp11*g1_1)
			wp.w12 -= float64(bp01*g0_2) + float64(bp11*g1_2)
			wp.w22 -= float64(bp02*g0_2) + float64(bp12*g1_2)
		} else {
			t.gain[oi] = [2][3]float64{}
		}
	}

	// Pass 2: top-down back-substitution (roots -> leaves).
	recordReaction := mode == jointTreeVelocity || mode == jointTreeRelax
	for oi := m - 1; oi >= 0; oi-- {
		ord := order[oi]
		ci := ord.childNode - 1
		pi := ord.parentNode - 1

		lx := t.lam0[oi][0]
		ly := t.lam0[oi][1]
		if pi >= 0 {
			g := &t.gain[oi]
			qp := &t.qVec[pi]
			lx += float64(g[0][0]*qp[0]) + float64(g[0][1]*qp[1]) + float64(g[0][2]*qp[2])
			ly += float64(g[1][0]*qp[0]) + float64(g[1][1]*qp[1]) + float64(g[1][2]*qp[2])
		}

		rCx, rCy := t.rC[oi][0], t.rC[oi][1]
		qc := &t.qVec[ci]
		qc[0] += lx
		qc[1] += ly
		qc[2] += float64(rCx*ly) - float64(rCy*lx)

		if pi >= 0 {
			rPx, rPy := t.rP[oi][0], t.rP[oi][1]
			qp := &t.qVec[pi]
			qp[0] -= lx
			qp[1] -= ly
			qp[2] -= float64(rPx*ly) - float64(rPy*lx)
		}

		if recordReaction {
			e := &t.edges[ord.edgeIdx]
			if ord.flipped {
				e.joint.addAnchorImpulse2D(-lx, -ly, true)
			} else {
				e.joint.addAnchorImpulse2D(lx, ly, true)
			}
		}
	}

	// Pass 3: apply accumulated spatial impulses once per touched dynamic body.
	if mode == jointTreePushOut {
		for _, bi := range t.touched {
			b := w.Bodies[bi]
			q := &t.qVec[bi]
			d := &deltas[bi]
			invM := float64(b.InvMass)
			invI := float64(b.InvInertia)
			d.pvx += float64(q[0] * invM)
			d.pvy += float64(q[1] * invM)
			d.pav += float64(q[2] * invI)
		}
	} else {
		for _, bi := range t.touched {
			b := w.Bodies[bi]
			q := &t.qVec[bi]
			invM := float64(b.InvMass)
			invI := float64(b.InvInertia)
			b.VX = float32(float64(b.VX) + float64(q[0]*invM))
			b.VY = float32(float64(b.VY) + float64(q[1]*invM))
			b.AngularVel = float32(float64(b.AngularVel) + float64(q[2]*invI))
		}
	}
}
