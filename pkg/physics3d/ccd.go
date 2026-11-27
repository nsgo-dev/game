package physics3d

import (
	"cmp"
	"math"
	"slices"
)

// Continuous collision detection (#546, #1712).
//
// After positions and orientations are integrated, a dynamic body that is a
// Bullet, or any point of whose core moved farther than half its smallest
// dimension this step (translation plus rotation at its reach), is swept from
// its start pose to its end pose with rotation interpolated. Conservative
// advancement using GJK finds the exact time of impact without a sample cap, so
// arbitrarily fast bodies and rapidly spinning shapes cannot tunnel through
// thin static geometry, triangle meshes, heightfields, or voxel colliders.
//
// Bullets are swept against awake dynamic bodies as well as static, kinematic,
// and sleeping bodies; non-bullet dynamic bodies keep static, kinematic,
// sleeping, and static-collider candidates only. On impact, the body (and a
// moved dynamic target it struck) is placed at the time of impact minus a small
// separation gap, its approach velocity is removed with restitution, any
// sleeping target island is woken, and body-body impacts are recorded for
// ContactEnter event dispatch. Static surfaces the body already rests on at the
// start of the step do not stop the sweep, and neither do coplanar static
// pieces lying wholly behind them, so fast bodies slide cleanly across multi-box,
// triangle-mesh, heightfield, and voxel floors.

const (
	ccdMaxIterations3D  = 64
	ccdBisectSteps3D    = 8
	ccdSpeculativeSep3D = 0.02
	ccdSupportSlopMin3D = 0.005
)

// ccdScratch3D holds per-World reusable slices for continuous collision so
// steady-state sweeps allocate nothing. None of it is simulation state.
type ccdScratch3D struct {
	candBodies []sweepBodyCand
	candTMs    []*TriMeshCollider
	candHFs    []*HeightfieldCollider
	candVoxels []*VoxelCollider
	supports   []ccdSupport3D
}

type sweepBodyCand struct {
	body   *Body
	shape  Shape
	bounds Box
	pos0   Vec3
	rot0   Quat
	moves  bool
}

type ccdSupport3D struct {
	n dv
	d float64
}

type ccdHitKind uint8

const (
	ccdHitBody ccdHitKind = iota + 1
	ccdHitTriMesh
	ccdHitHeightfield
	ccdHitVoxel
)

type ccdHit3D struct {
	found       bool
	t           float64
	n           dv // unit normal pointing from swept body toward target
	point       dv // world-space contact point on swept body surface
	kind        ccdHitKind
	targetID    int
	childA      int
	childB      int
	subIdx      int
	subY        int
	subZ        int
	body        *Body
	targetPos0  Vec3
	targetRot0  Quat
	targetMoves bool
	triMesh     *TriMeshCollider
	heightfield *HeightfieldCollider
	voxel       *VoxelCollider
	tri         Triangle
	hasTri      bool
}

func (h ccdHit3D) betterThan(other ccdHit3D) bool {
	if !other.found {
		return true
	}
	if h.t != other.t {
		return h.t < other.t
	}
	if h.kind != other.kind {
		return h.kind < other.kind
	}
	if h.targetID != other.targetID {
		return h.targetID < other.targetID
	}
	if h.childA != other.childA {
		return h.childA < other.childA
	}
	if h.childB != other.childB {
		return h.childB < other.childB
	}
	if h.subIdx != other.subIdx {
		return h.subIdx < other.subIdx
	}
	if h.subY != other.subY {
		return h.subY < other.subY
	}
	return h.subZ < other.subZ
}

// ccdMotion3D describes a body's linear and angular motion across one step.
type ccdMotion3D struct {
	pos0, pos1 Vec3
	rot0, rot1 Quat
	comLocal   Vec3
	com0, com1 dv
	dCom       dv
	angVel     Vec3
	omega      dv
	angAngle   float64
	dtSec      float32
	hasRot     bool
	hasCOM     bool
}

func makeCCDMotion3D(b *Body, pos0 Vec3, rot0 Quat, dtSec float32) ccdMotion3D {
	r0 := normalizedQuat(rot0)
	r1 := normalizedQuat(b.Rot)
	hasCOM := b.CenterOfMass != (Vec3{})
	var com0v, com1v Vec3
	if hasCOM {
		com0v = pos0.Add(r0.RotateVec(b.CenterOfMass))
		com1v = b.Pos.Add(r1.RotateVec(b.CenterOfMass))
	} else {
		com0v = pos0
		com1v = b.Pos
	}
	com0 := toDV(com0v)
	com1 := toDV(com1v)
	dCom := com1.sub(com0)

	var omega dv
	var angAngle float64
	hasRot := r0 != r1 || b.AngVel.LenSq() > 0
	if b.AngVel.LenSq() > 0 && dtSec > 0 {
		omega = toDV(b.AngVel).scale(float64(dtSec))
		angAngle = omega.length()
	} else if r0 != r1 {
		// Direct orientation change (e.g. kinematic rotation): recover axis-angle from r1 * conj(r0).
		dq := r1.Mul(r0.Conjugate()).Norm()
		axis, angle := dq.ToAxisAngle()
		angAngle = float64(angle)
		omega = toDV(axis).scale(angAngle)
	}
	if angAngle <= 1e-9 {
		hasRot = false
		angAngle = 0
		omega = dv{}
	}
	return ccdMotion3D{
		pos0:     pos0,
		pos1:     b.Pos,
		rot0:     r0,
		rot1:     r1,
		comLocal: b.CenterOfMass,
		com0:     com0,
		com1:     com1,
		dCom:     dCom,
		angVel:   b.AngVel,
		omega:    omega,
		angAngle: angAngle,
		dtSec:    dtSec,
		hasRot:   hasRot,
		hasCOM:   hasCOM,
	}
}

func (m *ccdMotion3D) poseAt(t float64) (Vec3, Quat) {
	if t <= 0 {
		return m.pos0, m.rot0
	}
	if t >= 1 {
		return m.pos1, m.rot1
	}
	if !m.hasRot {
		p := Vec3{
			X: float32(float64(m.pos0.X) + float64((float64(m.pos1.X)-float64(m.pos0.X))*t)),
			Y: float32(float64(m.pos0.Y) + float64((float64(m.pos1.Y)-float64(m.pos0.Y))*t)),
			Z: float32(float64(m.pos0.Z) + float64((float64(m.pos1.Z)-float64(m.pos0.Z))*t)),
		}
		return p, m.rot0
	}
	var rot Quat
	if m.angVel.LenSq() > 0 && m.dtSec > 0 {
		rot = IntegrateQuat(m.rot0, m.angVel, float32(float64(m.dtSec)*t))
	} else {
		rot = m.rot0.Slerp(m.rot1, float32(t))
	}
	comT := Vec3{
		X: float32(m.com0.x + float64(m.dCom.x*t)),
		Y: float32(m.com0.y + float64(m.dCom.y*t)),
		Z: float32(m.com0.z + float64(m.dCom.z*t)),
	}
	if !m.hasCOM {
		return comT, rot
	}
	return comT.Sub(normalizedQuat(rot).RotateVec(m.comLocal)), rot
}

func (m *ccdMotion3D) sweptBounds(shape Shape, boundR float64) Box {
	if !m.hasRot {
		b0 := shapeBoundsAt(shape, m.pos0, m.rot0)
		b1 := shapeBoundsAt(shape, m.pos1, m.rot1)
		return unionBounds(b0, b1)
	}
	r := float32(boundR)
	minV := Vec3{
		X: float32(min(m.com0.x, m.com1.x)) - r,
		Y: float32(min(m.com0.y, m.com1.y)) - r,
		Z: float32(min(m.com0.z, m.com1.z)) - r,
	}
	maxV := Vec3{
		X: float32(max(m.com0.x, m.com1.x)) + r,
		Y: float32(max(m.com0.y, m.com1.y)) + r,
		Z: float32(max(m.com0.z, m.com1.z)) + r,
	}
	return BoxFromMinMax(minV, maxV)
}

// shapeBoundsAt returns the world-space AABB of local shape s placed at (pos, rot)
// without allocating a child slice when s is a compound shape.
func shapeBoundsAt(s Shape, pos Vec3, rot Quat) Box {
	if s.Kind != ShapeCompound {
		return s.Transformed(pos, rot).Bounds()
	}
	var minV, maxV Vec3
	first := true
	for _, c := range s.Children {
		if c.Trigger {
			continue
		}
		cb := c.WorldShape(pos, rot).Bounds()
		cMin, cMax := cb.Min(), cb.Max()
		if first {
			minV, maxV = cMin, cMax
			first = false
		} else {
			minV = Vec3{X: min(minV.X, cMin.X), Y: min(minV.Y, cMin.Y), Z: min(minV.Z, cMin.Z)}
			maxV = Vec3{X: max(maxV.X, cMax.X), Y: max(maxV.Y, cMax.Y), Z: max(maxV.Z, cMax.Z)}
		}
	}
	if first {
		return Box{Center: pos}
	}
	return BoxFromMinMax(minV, maxV)
}

// shapeMinExtent returns half the smallest dimension of s: the radius for spheres
// and capsules, the smallest half-extent for boxes, half the smallest width
// across a face for convex hulls, and the smaller of the radius and half the
// height for cylinders.
func shapeMinExtent(s Shape) float32 {
	switch s.Kind {
	case ShapeSphere:
		return s.Sphere.Radius
	case ShapeCapsule:
		return s.Capsule.Radius
	case ShapeBox:
		return min(s.Box.HalfExtents.X, s.Box.HalfExtents.Y, s.Box.HalfExtents.Z)
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return 0
		}
		return float32(s.Hull.geom.halfWidth)
	case ShapeCylinder:
		return min(s.Cylinder.Radius, s.Cylinder.Height()*0.5)
	case ShapeCompound:
		var best float32
		for _, c := range s.Children {
			if c.Trigger {
				continue
			}
			e := shapeMinExtent(c.Shape)
			if e > 0 && (best == 0 || e < best) {
				best = e
			}
		}
		return best
	default:
		return 0
	}
}

// shapeCoreReach returns the maximum distance from localCOM to any point of s's
// core (excluding rounding margin) and the maximum total radius including margin.
func shapeCoreReach(s Shape, localCOM Vec3) (coreReach, totalRadius float64) {
	switch s.Kind {
	case ShapeSphere:
		c := float64(s.Sphere.Center.Sub(localCOM).Len())
		r := float64(max(s.Sphere.Radius, 0))
		return c, c + r
	case ShapeCapsule:
		da := float64(s.Capsule.A.Sub(localCOM).Len())
		db := float64(s.Capsule.B.Sub(localCOM).Len())
		c := max(da, db)
		r := float64(max(s.Capsule.Radius, 0))
		return c, c + r
	case ShapeBox:
		c := float64(s.Box.Center.Sub(localCOM).Len()) + float64(s.Box.HalfExtents.Len())
		return c, c
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return 0, 0
		}
		c := float64(s.Hull.Center.Sub(localCOM).Len()) + s.Hull.geom.radius
		return c, c
	case ShapeCylinder:
		mid := s.Cylinder.Center()
		half := float64(s.Cylinder.Height()) * 0.5
		rad := float64(max(s.Cylinder.Radius, 0))
		rim := math.Sqrt(float64(half*half) + float64(rad*rad))
		c := float64(mid.Sub(localCOM).Len()) + rim
		return c, c
	case ShapeCompound:
		for _, ch := range s.Children {
			if ch.Trigger {
				continue
			}
			cr, tr := shapeCoreReach(ch.LocalShape(), localCOM)
			if cr > coreReach {
				coreReach = cr
			}
			if tr > totalRadius {
				totalRadius = tr
			}
		}
		return coreReach, totalRadius
	default:
		return 0, 0
	}
}

func ccdTargetSep3D(ext float64) (targetSep, tol float64) {
	targetSep = min(max(float64(ext*0.02), 2e-4), 2e-3)
	tol = float64(targetSep * 0.5)
	return targetSep, tol
}

// sweepFastBody sweeps b from (pos0, rot0) to (b.Pos, b.Rot) against blocking
// colliders using GJK conservative advancement, placing b (and a moved dynamic
// target it struck) at the earliest time of impact minus a small gap and
// removing their approach velocity with restitution.
func (w *World) sweepFastBody(
	b *Body,
	pos0 Vec3,
	rot0 Quat,
	dtSec float32,
	contacts []solverContact,
	currentPairs map[contactKey]activeContactRecord,
	jointIgnored map[pairKey]struct{},
) {
	ext32 := shapeMinExtent(b.Shape)
	if ext32 <= 0 {
		return
	}
	ext := float64(ext32)
	mot := makeCCDMotion3D(b, pos0, rot0, dtSec)
	reach, boundR := shapeCoreReach(b.Shape, b.CenterOfMass)
	linDist := mot.dCom.length()
	motion := linDist + float64(mot.angAngle*reach)
	if !b.Bullet && !(motion > float64(ext*0.5)) {
		return
	}
	if math.IsNaN(motion) || math.IsInf(motion, 0) {
		return
	}

	sweptBounds := mot.sweptBounds(b.Shape, boundR)
	bIsCompound := b.Shape.Kind == ShapeCompound
	w.bp.ensureInit(w)

	candBodies := w.ccd.candBodies[:0]
	candTMs := w.ccd.candTMs[:0]
	candHFs := w.ccd.candHFs[:0]
	candVoxels := w.ccd.candVoxels[:0]

	w.bp.queryAABB(sweptBounds, func(p *bpProxy) bool {
		switch p.kind {
		case bpKindBody:
			o := p.body
			if o == b || o.IsTrigger || !(o.Static || o.Kinematic || o.Sleeping) {
				return true
			}
			if w.isPairIgnored(b.ID, o.ID) {
				return true
			}
			if !bIsCompound && o.Shape.Kind != ShapeCompound && !CanCollide(b, o) {
				return true
			}
			if jointIgnored != nil {
				key, _ := canonicalPairKey(b.ID, o.ID)
				if _, ok := jointIgnored[key]; ok {
					return true
				}
			}
			if !aabbOverlap(sweptBounds, p.worldBounds) {
				return true
			}
			candBodies = append(candBodies, sweepBodyCand{
				body:   o,
				shape:  p.worldShape,
				bounds: p.worldBounds,
				pos0:   o.Pos,
				rot0:   o.Rot,
				moves:  false,
			})
		case bpKindTriMesh:
			tm := p.triMesh
			if (!bIsCompound && !canCollideMasks(b.Layer, b.Mask, tm.Layer, tm.Mask)) || !aabbOverlap(sweptBounds, tm.Bounds) {
				return true
			}
			candTMs = append(candTMs, tm)
		case bpKindHeightfield:
			hf := p.heightfield
			if (!bIsCompound && !canCollideMasks(b.Layer, b.Mask, hf.Layer, hf.Mask)) || !aabbOverlap(sweptBounds, hf.Bounds) {
				return true
			}
			candHFs = append(candHFs, hf)
		}
		return true
	})

	// Bullets are also swept against awake dynamic bodies using their true
	// swept bounds across [prevPose, curPose].
	if b.Bullet {
		for _, pid := range w.bp.dynamicList {
			p := &w.bp.proxies[pid]
			o := p.body
			if o == b || o.IsTrigger || o.Static || o.Kinematic || o.Sleeping {
				continue
			}
			if w.isPairIgnored(b.ID, o.ID) {
				continue
			}
			if !bIsCompound && o.Shape.Kind != ShapeCompound && !CanCollide(b, o) {
				continue
			}
			if jointIgnored != nil {
				key, _ := canonicalPairKey(b.ID, o.ID)
				if _, ok := jointIgnored[key]; ok {
					continue
				}
			}
			oMot := makeCCDMotion3D(o, p.prevPos, p.prevRot, dtSec)
			_, oBoundR := shapeCoreReach(o.Shape, o.CenterOfMass)
			oSwept := oMot.sweptBounds(o.Shape, oBoundR)
			if !aabbOverlap(sweptBounds, oSwept) {
				continue
			}
			oMoves := o.Pos != p.prevPos || o.Rot != p.prevRot || o.AngVel.LenSq() > 0
			candBodies = append(candBodies, sweepBodyCand{
				body:   o,
				shape:  o.WorldShape(),
				bounds: oSwept,
				pos0:   p.prevPos,
				rot0:   p.prevRot,
				moves:  oMoves,
			})
		}
	}

	for _, vc := range w.voxels {
		if vc != nil && (bIsCompound || canCollideMasks(b.Layer, b.Mask, vc.Layer, vc.Mask)) {
			candVoxels = append(candVoxels, vc)
		}
	}

	if len(candBodies) == 0 && len(candTMs) == 0 && len(candHFs) == 0 && len(candVoxels) == 0 {
		w.ccd.candBodies = candBodies[:0]
		w.ccd.candTMs = candTMs[:0]
		w.ccd.candHFs = candHFs[:0]
		w.ccd.candVoxels = candVoxels[:0]
		return
	}

	// Sort candidates deterministically by ID so broadphase tree traversal order
	// never affects tie-breaking or brute-force parity.
	if len(candBodies) > 1 {
		slices.SortFunc(candBodies, func(a, b sweepBodyCand) int {
			return cmp.Compare(a.body.ID, b.body.ID)
		})
	}
	if len(candTMs) > 1 {
		slices.SortFunc(candTMs, func(a, b *TriMeshCollider) int {
			return cmp.Compare(a.ID, b.ID)
		})
	}
	if len(candHFs) > 1 {
		slices.SortFunc(candHFs, func(a, b *HeightfieldCollider) int {
			return cmp.Compare(a.ID, b.ID)
		})
	}

	// Collect static support planes the body already rests on at the start of the
	// step so coplanar tiles, mesh triangles, heightfield cells, or voxels lying
	// wholly behind a support never snag a body sliding fast across seams.
	supports := w.ccd.supports[:0]
	shallowStatic := func(n dv, sep float64) bool {
		angApp := float64(n.cross(mot.omega).length() * reach)
		return mot.dCom.dot(n)+angApp-max(0, sep) <= float64(0.5*ext)
	}
	for i := range contacts {
		sc := &contacts[i]
		var n dv
		if sc.a == b && (sc.b == nil || sc.b.Static) {
			n = toDV(sc.normal).neg()
		} else if sc.b == b && sc.a != nil && sc.a.Static {
			n = toDV(sc.normal)
		} else {
			continue
		}
		if shallowStatic(n, 0) {
			supports = append(supports, ccdSupport3D{n: n, d: toDV(sc.point).dot(n)})
		}
	}
	if !bIsCompound {
		startCvx := toCvx(b.Shape.Transformed(pos0, rot0))
		if startCvx.valid() {
			for i := range candBodies {
				cb := &candBodies[i]
				if !cb.body.Static || cb.body.Shape.Kind == ShapeCompound {
					continue
				}
				oCvx := toCvx(cb.shape)
				if !oCvx.valid() {
					continue
				}
				sep, n, _, _, ok := ccdSeparation3D(&startCvx, &oCvx, 1e-5)
				if ok && sep <= ccdSpeculativeSep3D && shallowStatic(n, sep) {
					supports = append(supports, ccdSupport3D{n: n, d: nearOffset3D(&oCvx, n)})
				}
			}
		}
	}
	supportSlop := max(float64(ext*0.05), ccdSupportSlopMin3D)

	targetSep, _ := ccdTargetSep3D(ext)
	var best ccdHit3D
	best.t = math.Inf(1)

	sweepPiece := func(childIdx int, childShape Shape, childOffset Vec3, childRot Quat, childReach, childExt float64, layerA, maskA LayerMask) {
		cTargetSep, cTol := ccdTargetSep3D(childExt)
		if cTargetSep < targetSep {
			targetSep = cTargetSep
		}
		evalA := func(t float64) cvx {
			p, r := mot.poseAt(t)
			if childIdx < 0 {
				return toCvx(childShape.Transformed(p, r))
			}
			cc := CompoundChild{Shape: childShape, Offset: childOffset, Rot: childRot}
			return toCvx(cc.WorldShape(p, r))
		}
		var childStartShape Shape
		if childIdx < 0 {
			childStartShape = childShape.Transformed(pos0, rot0)
		} else {
			cc := CompoundChild{Shape: childShape, Offset: childOffset, Rot: childRot}
			childStartShape = cc.WorldShape(pos0, rot0)
		}
		childBounds0 := childStartShape.Bounds()
		var childSwept Box
		if !mot.hasRot {
			childSwept = unionBounds(childBounds0, childBounds0.Translated(mot.pos1.Sub(mot.pos0)))
		} else {
			childSwept = sweptBounds
		}

		// 1. Candidate bodies.
		for i := range candBodies {
			cb := &candBodies[i]
			o := cb.body
			if !aabbOverlap(childSwept, cb.bounds) {
				continue
			}
			var oMot ccdMotion3D
			var oReach float64
			if cb.moves {
				oMot = makeCCDMotion3D(o, cb.pos0, cb.rot0, dtSec)
				oReach, _ = shapeCoreReach(o.Shape, o.CenterOfMass)
			}
			testTargetChild := func(childB int, targetLocal Shape, targetOff Vec3, targetRot Quat, targetWorld Shape, layerB, maskB LayerMask) {
				if !canCollideMasks(layerA, maskA, layerB, maskB) {
					return
				}
				if !cb.moves {
					if !aabbOverlap(childSwept, targetWorld.Bounds()) {
						return
					}
					oCvx := toCvx(targetWorld)
					if !oCvx.valid() {
						return
					}
					if o.Static && behindSupport3D(&oCvx, supports, supportSlop) {
						return
					}
					if t, n, pt, hit := sweepConvexVsStatic3D(b.Bullet, evalA, &oCvx, &mot, childReach, childExt, cTargetSep, cTol, best.t); hit {
						cand := ccdHit3D{
							found:    true,
							t:        t,
							n:        n,
							point:    pt,
							kind:     ccdHitBody,
							targetID: o.ID,
							childA:   childIdx,
							childB:   childB,
							body:     o,
						}
						if cand.betterThan(best) {
							best = cand
						}
					}
					return
				}
				evalB := func(t float64) cvx {
					p, r := oMot.poseAt(t)
					if childB < 0 {
						return toCvx(targetLocal.Transformed(p, r))
					}
					cc := CompoundChild{Shape: targetLocal, Offset: targetOff, Rot: targetRot}
					return toCvx(cc.WorldShape(p, r))
				}
				if t, n, pt, hit := sweepConvexVsMoving3D(b.Bullet, evalA, evalB, &mot, &oMot, childReach, oReach, childExt, cTargetSep, cTol, best.t); hit {
					cand := ccdHit3D{
						found:       true,
						t:           t,
						n:           n,
						point:       pt,
						kind:        ccdHitBody,
						targetID:    o.ID,
						childA:      childIdx,
						childB:      childB,
						body:        o,
						targetPos0:  cb.pos0,
						targetRot0:  cb.rot0,
						targetMoves: true,
					}
					if cand.betterThan(best) {
						best = cand
					}
				}
			}

			if o.Shape.Kind != ShapeCompound {
				testTargetChild(-1, o.Shape, Vec3{}, Quat{}, cb.shape, o.Layer, o.Mask)
			} else {
				for cj, oc := range o.Shape.Children {
					if oc.Trigger {
						continue
					}
					layerB, maskB, _ := o.childEffective(cj)
					var tw Shape
					if !cb.moves && cj < len(cb.shape.Children) {
						tw = cb.shape.Children[cj].LocalShape()
					} else if !cb.moves {
						tw = oc.WorldShape(o.Pos, o.Rot)
					}
					testTargetChild(cj, oc.Shape, oc.Offset, oc.Rot, tw, layerB, maskB)
				}
			}
		}

		// 2. Candidate triangle meshes (BVH-accelerated, ray-pruned when translating).
		for _, tm := range candTMs {
			if !canCollideMasks(layerA, maskA, tm.Layer, tm.Mask) || !aabbOverlap(childSwept, tm.Bounds) || len(tm.nodes) == 0 {
				continue
			}
			sweepTriMeshCCD(b.Bullet, tm, evalA, &mot, childBounds0, childSwept, childReach, childExt, cTargetSep, cTol, supports, supportSlop, childIdx, &best)
		}

		// 3. Candidate heightfields.
		for _, hf := range candHFs {
			if !canCollideMasks(layerA, maskA, hf.Layer, hf.Mask) || !aabbOverlap(childSwept, hf.Bounds) {
				continue
			}
			sweepHeightfieldCCD(b.Bullet, hf, evalA, &mot, childBounds0, childSwept, childReach, childExt, cTargetSep, cTol, supports, supportSlop, childIdx, &best)
		}

		// 4. Candidate voxel colliders.
		for _, vc := range candVoxels {
			if !canCollideMasks(layerA, maskA, vc.Layer, vc.Mask) {
				continue
			}
			sweepVoxelCCD(b.Bullet, vc, evalA, &mot, childBounds0, childSwept, childReach, childExt, cTargetSep, cTol, supports, supportSlop, childIdx, &best)
		}
	}

	if !bIsCompound {
		sweepPiece(-1, b.Shape, Vec3{}, Quat{}, reach, ext, b.Layer, b.Mask)
	} else {
		for ci, c := range b.Shape.Children {
			if c.Trigger {
				continue
			}
			ce := shapeMinExtent(c.Shape)
			if ce <= 0 {
				continue
			}
			cr, _ := shapeCoreReach(c.LocalShape(), b.CenterOfMass)
			layerA, maskA, _ := b.childEffective(ci)
			sweepPiece(ci, c.Shape, c.Offset, c.Rot, cr, float64(ce), layerA, maskA)
		}
	}

	// Clear pointer references in scratch slices before returning.
	clear(candBodies)
	clear(candTMs)
	clear(candHFs)
	clear(candVoxels)
	w.ccd.candBodies = candBodies[:0]
	w.ccd.candTMs = candTMs[:0]
	w.ccd.candHFs = candHFs[:0]
	w.ccd.candVoxels = candVoxels[:0]
	w.ccd.supports = supports[:0]

	if !best.found {
		return
	}

	// Place b at the earliest time of impact (already separated by targetSep).
	b.Pos, b.Rot = mot.poseAt(best.t)

	// Wake a sleeping dynamic target, and rewind a moved dynamic target to best.t
	// so both bodies meet cleanly without passing through each other.
	if best.body != nil {
		o := best.body
		if o.Sleeping && !o.Static && !o.Kinematic {
			o.Wake()
			w.wakeIslands()
		} else if best.targetMoves && !o.Static && !o.Kinematic {
			oMot := makeCCDMotion3D(o, best.targetPos0, best.targetRot0, dtSec)
			o.Pos, o.Rot = oMot.poseAt(best.t)
		}
	}

	// Remove approach velocity along the impact normal(s) with restitution.
	w.resolveImpact3D(b, &best, targetSep)

	// Record body-body CCD impacts so OnContact emits ContactEnter even if
	// restitution rebounds the body clear on the same step.
	if best.body != nil && currentPairs != nil {
		key, swapped := canonicalContactKey(b.ID, best.childA, best.body.ID, best.childB)
		if _, exists := currentPairs[key]; !exists {
			evA, evB, evChildA, evChildB := b, best.body, best.childA, best.childB
			// ContactEvent.Normal points from BodyB toward BodyA; best.n points from b toward best.body.
			evNorm := best.n.neg().vec3().Norm()
			if swapped {
				evA, evB, evChildA, evChildB = best.body, b, best.childB, best.childA
				evNorm = evNorm.Neg()
			}
			currentPairs[key] = activeContactRecord{
				a:      evA,
				b:      evB,
				childA: evChildA,
				childB: evChildB,
				point:  best.point.vec3(),
				normal: evNorm,
				depth:  0,
			}
		}
	}
}

// nearOffset3D returns the minimum projection along unit normal n of rounded
// convex shape c (the surface point reaching farthest against n).
func nearOffset3D(c *cvx, n dv) float64 {
	return c.support(n.neg()).dot(n) - c.margin
}

// behindSupport3D reports whether all of c lies on the far side of any support plane.
func behindSupport3D(c *cvx, supports []ccdSupport3D, slop float64) bool {
	for i := range supports {
		s := &supports[i]
		if nearOffset3D(c, s.n) >= s.d-slop {
			return true
		}
	}
	return false
}

// ccdSeparation3D computes the exact surface separation between convex shapes a
// and b using GJK. When separated (or only margins overlapping), ok is true,
// sep = coreDist - a.margin - b.margin, n is the unit normal from a toward b,
// and pa, pb are the closest core points. When the cores overlap, ok is false.
func ccdSeparation3D(a, b *cvx, tol float64) (sep float64, n, pa, pb dv, ok bool) {
	_, pa, pb, overlap := gjk(a, b, tol)
	if overlap {
		return 0, dv{}, dv{}, dv{}, false
	}
	d := pb.sub(pa)
	dist := d.length()
	if !(dist > tol) {
		return 0, dv{}, dv{}, dv{}, false
	}
	n = d.scale(1.0 / dist)
	return dist - a.margin - b.margin, n, pa, pb, true
}

// ccdStartOverlapCheck handles the iter == 0 case where cores already touch or
// overlap at t == 0. A body resting, pivoting, or sliding along a surface is
// left to the regular contacts; a point-blank shot driven deeper than half its
// extent into a barely-touching surface stops at t == 0.
func ccdStartOverlapCheck(a, b *cvx, s simplex, tol float64, motA, motB *ccdMotion3D, extA float64) (n, pt dv, stop bool) {
	pointApproach := func(norm, p dv) float64 {
		vA := motA.dCom.add(motA.omega.cross(p.sub(motA.com0)))
		if motB != nil {
			vB := motB.dCom.add(motB.omega.cross(p.sub(motB.com0)))
			return vA.sub(vB).dot(norm)
		}
		return vA.dot(norm)
	}
	c := testConvex(*a, *b)
	if c.Hit {
		n = toDV(c.Normal).neg()
		if n.lenSq() <= 1e-18 {
			return dv{}, dv{}, false
		}
		if float64(c.Depth) >= extA {
			// Already deeply embedded at start of step: regular contacts own it.
			return dv{}, dv{}, false
		}
		pt = toDV(c.Point)
		if pointApproach(n, pt) <= float64(0.5*extA) {
			return dv{}, dv{}, false
		}
		return n, pt, true
	}
	// Exact kissing contact (zero penetration depth): recover the true surface
	// normal from EPA on the touching simplex rather than centroids, so a box
	// sitting flush on a wide floor gets the floor normal instead of pointing
	// sideways toward the floor's center.
	if r, ok := epa(a, b, s, tol*10); ok && r.n.lenSq() > 1e-18 {
		n = r.n.norm()
		pt = r.pa.add(n.scale(a.margin))
		if pointApproach(n, pt) <= float64(0.5*extA) {
			return dv{}, dv{}, false
		}
		return n, pt, true
	}
	return dv{}, dv{}, false
}

// ccdPointApproach3D returns the displacement along unit normal n of the
// contact point p on body A relative to body B (nil for stationary targets).
func ccdPointApproach3D(motA, motB *ccdMotion3D, p, n dv) float64 {
	vA := motA.dCom.add(motA.omega.cross(p.sub(motA.com0)))
	if motB != nil {
		vB := motB.dCom.add(motB.omega.cross(p.sub(motB.com0)))
		return vA.sub(vB).dot(n)
	}
	return vA.dot(n)
}

// sweepConvexVsStatic3D runs GJK conservative advancement of a moving/rotating
// convex shape against a stationary convex target.
func sweepConvexVsStatic3D(
	isBullet bool,
	evalA func(float64) cvx,
	target *cvx,
	mot *ccdMotion3D,
	reachA, extA, targetSep, tol, maxT float64,
) (hitT float64, hitN, hitPt dv, hit bool) {
	t := 0.0
	tPrev := 0.0
	var lastN, lastPt dv
	havePrev := false
	gjkTol := max(1e-9, min(float64(targetSep*0.1), 1e-5))

	for iter := range ccdMaxIterations3D {
		ca := evalA(t)
		if !ca.valid() {
			return 0, dv{}, dv{}, false
		}
		s, pa, pb, overlap := gjk(&ca, target, gjkTol)
		var sep float64
		var n dv
		ok := false
		if !overlap {
			d := pb.sub(pa)
			dist := d.length()
			if dist > gjkTol {
				n = d.scale(1.0 / dist)
				sep = dist - ca.margin - target.margin
				ok = true
			}
		}
		if !ok {
			if iter == 0 {
				if sn, spt, stop := ccdStartOverlapCheck(&ca, target, s, gjkTol, mot, nil, extA); stop {
					if 0 <= maxT {
						return 0, sn, spt, true
					}
				}
				return 0, dv{}, dv{}, false
			}
			// Numerical overshoot past contact: bisect [tPrev, t] to stay separated.
			if havePrev {
				bt, bn, bpt := bisectStaticCCD(evalA, target, tPrev, t, lastN, lastPt, targetSep, gjkTol)
				if bt <= maxT {
					return bt, bn, bpt, true
				}
			}
			return 0, dv{}, dv{}, false
		}
		angApp := float64(n.cross(mot.omega).length() * reachA)
		approach := mot.dCom.dot(n) + angApp
		pt := pa.add(n.scale(ca.margin))

		if iter == 0 && sep <= targetSep {
			if ccdPointApproach3D(mot, nil, pt, n)-max(0, sep) <= float64(0.5*extA) {
				return 0, dv{}, dv{}, false
			}
			if 0 <= maxT {
				return 0, n, pt, true
			}
			return 0, dv{}, dv{}, false
		}
		if sep <= targetSep+tol {
			if !isBullet && float64((1-t)*approach)-sep <= float64(0.5*extA) {
				return 0, dv{}, dv{}, false
			}
			if sep < 0 && havePrev {
				bt, bn, bpt := bisectStaticCCD(evalA, target, tPrev, t, lastN, lastPt, targetSep, gjkTol)
				if bt <= maxT {
					return bt, bn, bpt, true
				}
				return 0, dv{}, dv{}, false
			}
			if t <= maxT {
				return t, n, pt, true
			}
			return 0, dv{}, dv{}, false
		}
		if iter == ccdMaxIterations3D-1 {
			if sep <= max(targetSep+tol, float64(extA*0.1)) && t <= maxT {
				if !isBullet && float64((1-t)*approach)-sep <= float64(0.5*extA) {
					return 0, dv{}, dv{}, false
				}
				return t, n, pt, true
			}
			return 0, dv{}, dv{}, false
		}
		if approach <= 1e-12 {
			return 0, dv{}, dv{}, false
		}
		tPrev = t
		lastN = n
		lastPt = pt
		havePrev = true
		t += (sep - targetSep) / approach
		if t >= 1 || t > maxT {
			return 0, dv{}, dv{}, false
		}
	}
	return 0, dv{}, dv{}, false
}

func bisectStaticCCD(evalA func(float64) cvx, target *cvx, lo, hi float64, bestN, bestPt dv, targetSep, gjkTol float64) (float64, dv, dv) {
	for range ccdBisectSteps3D {
		mid := float64((lo + hi) * 0.5)
		ca := evalA(mid)
		if !ca.valid() {
			break
		}
		sep, n, pa, _, ok := ccdSeparation3D(&ca, target, gjkTol)
		if ok && sep >= float64(targetSep*0.25) {
			lo = mid
			bestN = n
			bestPt = pa.add(n.scale(ca.margin))
		} else {
			hi = mid
		}
	}
	return lo, bestN, bestPt
}

// sweepConvexVsMoving3D runs GJK conservative advancement between two moving
// and/or rotating convex bodies.
func sweepConvexVsMoving3D(
	isBullet bool,
	evalA, evalB func(float64) cvx,
	motA, motB *ccdMotion3D,
	reachA, reachB, extA, targetSep, tol, maxT float64,
) (hitT float64, hitN, hitPt dv, hit bool) {
	relD := motA.dCom.sub(motB.dCom)
	t := 0.0
	tPrev := 0.0
	var lastN, lastPt dv
	havePrev := false
	gjkTol := max(1e-9, min(float64(targetSep*0.1), 1e-5))

	for iter := range ccdMaxIterations3D {
		ca := evalA(t)
		cb := evalB(t)
		if !ca.valid() || !cb.valid() {
			return 0, dv{}, dv{}, false
		}
		s, pa, pb, overlap := gjk(&ca, &cb, gjkTol)
		var sep float64
		var n dv
		ok := false
		if !overlap {
			d := pb.sub(pa)
			dist := d.length()
			if dist > gjkTol {
				n = d.scale(1.0 / dist)
				sep = dist - ca.margin - cb.margin
				ok = true
			}
		}
		if !ok {
			if iter == 0 {
				if sn, spt, stop := ccdStartOverlapCheck(&ca, &cb, s, gjkTol, motA, motB, extA); stop {
					if 0 <= maxT {
						return 0, sn, spt, true
					}
				}
				return 0, dv{}, dv{}, false
			}
			if havePrev {
				bt, bn, bpt := bisectMovingCCD(evalA, evalB, tPrev, t, lastN, lastPt, targetSep, gjkTol)
				if bt <= maxT {
					return bt, bn, bpt, true
				}
			}
			return 0, dv{}, dv{}, false
		}
		angApp := float64(n.cross(motA.omega).length()*reachA) + float64(n.cross(motB.omega).length()*reachB)
		approach := relD.dot(n) + angApp
		pt := pa.add(n.scale(ca.margin))

		if iter == 0 && sep <= targetSep {
			if ccdPointApproach3D(motA, motB, pt, n)-max(0, sep) <= float64(0.5*extA) {
				return 0, dv{}, dv{}, false
			}
			if 0 <= maxT {
				return 0, n, pt, true
			}
			return 0, dv{}, dv{}, false
		}
		if sep <= targetSep+tol {
			if !isBullet && float64((1-t)*approach)-sep <= float64(0.5*extA) {
				return 0, dv{}, dv{}, false
			}
			if sep < 0 && havePrev {
				bt, bn, bpt := bisectMovingCCD(evalA, evalB, tPrev, t, lastN, lastPt, targetSep, gjkTol)
				if bt <= maxT {
					return bt, bn, bpt, true
				}
				return 0, dv{}, dv{}, false
			}
			if t <= maxT {
				return t, n, pt, true
			}
			return 0, dv{}, dv{}, false
		}
		if iter == ccdMaxIterations3D-1 {
			if sep <= max(targetSep+tol, float64(extA*0.1)) && t <= maxT {
				if !isBullet && float64((1-t)*approach)-sep <= float64(0.5*extA) {
					return 0, dv{}, dv{}, false
				}
				return t, n, pt, true
			}
			return 0, dv{}, dv{}, false
		}
		if approach <= 1e-12 {
			return 0, dv{}, dv{}, false
		}
		tPrev = t
		lastN = n
		lastPt = pt
		havePrev = true
		t += (sep - targetSep) / approach
		if t >= 1 || t > maxT {
			return 0, dv{}, dv{}, false
		}
	}
	return 0, dv{}, dv{}, false
}

func bisectMovingCCD(evalA, evalB func(float64) cvx, lo, hi float64, bestN, bestPt dv, targetSep, gjkTol float64) (float64, dv, dv) {
	for range ccdBisectSteps3D {
		mid := float64((lo + hi) * 0.5)
		ca := evalA(mid)
		cb := evalB(mid)
		if !ca.valid() || !cb.valid() {
			break
		}
		sep, n, pa, _, ok := ccdSeparation3D(&ca, &cb, gjkTol)
		if ok && sep >= float64(targetSep*0.25) {
			lo = mid
			bestN = n
			bestPt = pa.add(n.scale(ca.margin))
		} else {
			hi = mid
		}
	}
	return lo, bestN, bestPt
}

// sweepTriMeshCCD traverses tm's BVH (front-to-back with ray-expanded AABB
// pruning when not rotating) and tests leaf triangles with conservative advancement.
func sweepTriMeshCCD(
	isBullet bool,
	tm *TriMeshCollider,
	evalA func(float64) cvx,
	mot *ccdMotion3D,
	bounds0, swept Box,
	reachA, extA, targetSep, tol float64,
	supports []ccdSupport3D,
	supportSlop float64,
	childA int,
	best *ccdHit3D,
) {
	delta := mot.pos1.Sub(mot.pos0)
	dist := delta.Len()
	useRay := !mot.hasRot && dist > 1e-9
	var dir Vec3
	if useRay {
		dir = delta.Scale(1 / dist)
	}
	he := bounds0.HalfExtents

	nodeOverlap := func(nb Box) (float32, bool) {
		if !useRay {
			return 0, aabbOverlap(swept, nb)
		}
		maxDist := dist
		if best.found && best.t < 1 {
			maxDist = min(dist, float32(float64(dist)*best.t)+float32(targetSep*2)+1e-3)
		}
		expanded := Box{Center: nb.Center, HalfExtents: nb.HalfExtents.Add(he)}
		tMin, _, hit := rayIntersectsBoxDist(bounds0.Center, dir, maxDist, expanded)
		return tMin, hit
	}

	var buf [64]int
	stack := append(buf[:0], 0)
	for len(stack) > 0 {
		node := &tm.nodes[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if node.triCount > 0 {
			for i := 0; i < node.triCount; i++ {
				triIdx := tm.triOrder[node.firstTri+i]
				tri := tm.Triangles[triIdx]
				tb := tri.Bounds()
				if useRay {
					if _, ok := nodeOverlap(tb); !ok {
						continue
					}
				} else if !aabbOverlap(swept, tb) {
					continue
				}
				tc := triCvx(tri)
				if behindSupport3D(&tc, supports, supportSlop) {
					continue
				}
				if t, n, pt, hit := sweepConvexVsStatic3D(isBullet, evalA, &tc, mot, reachA, extA, targetSep, tol, best.t); hit {
					cand := ccdHit3D{
						found:    true,
						t:        t,
						n:        n,
						point:    pt,
						kind:     ccdHitTriMesh,
						targetID: tm.ID,
						childA:   childA,
						childB:   -1,
						subIdx:   triIdx,
						triMesh:  tm,
						tri:      tri,
						hasTri:   true,
					}
					if cand.betterThan(*best) {
						*best = cand
					}
				}
			}
			continue
		}
		tL, hitL := nodeOverlap(tm.nodes[node.left].bounds)
		tR, hitR := nodeOverlap(tm.nodes[node.right].bounds)
		if hitL && hitR {
			if tL <= tR {
				stack = append(stack, node.right, node.left)
			} else {
				stack = append(stack, node.left, node.right)
			}
		} else if hitL {
			stack = append(stack, node.left)
		} else if hitR {
			stack = append(stack, node.right)
		}
	}
}

// sweepHeightfieldCCD sweeps against heightfield triangles overlapped by swept.
func sweepHeightfieldCCD(
	isBullet bool,
	hf *HeightfieldCollider,
	evalA func(float64) cvx,
	mot *ccdMotion3D,
	bounds0, swept Box,
	reachA, extA, targetSep, tol float64,
	supports []ccdSupport3D,
	supportSlop float64,
	childA int,
	best *ccdHit3D,
) {
	delta := mot.pos1.Sub(mot.pos0)
	dist := delta.Len()
	useRay := !mot.hasRot && dist > 1e-9
	var dir Vec3
	if useRay {
		dir = delta.Scale(1 / dist)
	}
	he := bounds0.HalfExtents
	triSeq := 0
	hf.QueryTriangles(swept, func(tri Triangle) {
		seq := triSeq
		triSeq++
		tb := tri.Bounds()
		if useRay {
			maxDist := dist
			if best.found && best.t < 1 {
				maxDist = min(dist, float32(float64(dist)*best.t)+float32(targetSep*2)+1e-3)
			}
			expanded := Box{Center: tb.Center, HalfExtents: tb.HalfExtents.Add(he)}
			if _, _, ok := rayIntersectsBoxDist(bounds0.Center, dir, maxDist, expanded); !ok {
				return
			}
		} else if !aabbOverlap(swept, tb) {
			return
		}
		tc := triCvx(tri)
		if behindSupport3D(&tc, supports, supportSlop) {
			return
		}
		if t, n, pt, hit := sweepConvexVsStatic3D(isBullet, evalA, &tc, mot, reachA, extA, targetSep, tol, best.t); hit {
			cand := ccdHit3D{
				found:       true,
				t:           t,
				n:           n,
				point:       pt,
				kind:        ccdHitHeightfield,
				targetID:    hf.ID,
				childA:      childA,
				childB:      -1,
				subIdx:      seq,
				heightfield: hf,
				tri:         tri,
				hasTri:      true,
			}
			if cand.betterThan(*best) {
				*best = cand
			}
		}
	})
}

// sweepVoxelCCD sweeps against solid voxels along the body's trajectory,
// chunking long linear sweeps so cost stays O(dist/voxelSize) and filtering out
// occluded internal voxel faces so bodies never snag on flat voxel floors.
func sweepVoxelCCD(
	isBullet bool,
	vc *VoxelCollider,
	evalA func(float64) cvx,
	mot *ccdMotion3D,
	bounds0, swept Box,
	reachA, extA, targetSep, tol float64,
	supports []ccdSupport3D,
	supportSlop float64,
	childA int,
	best *ccdHit3D,
) {
	if !vc.valid() {
		return
	}
	g := vc.Grid
	vs := g.VoxelSize()

	testRegion := func(region Box) {
		x0, y0, z0, x1, y1, z1, ok := vc.voxelRange(region)
		if !ok {
			return
		}
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				for x := x0; x <= x1; x++ {
					if !g.IsSolid(x, y, z) {
						continue
					}
					if vc.solidNeighborMask(x, y, z) == voxelFaceAll {
						continue
					}
					vb := vc.voxelBox(x, y, z)
					vcCvx := toCvx(BoxShape(vb))
					if behindSupport3D(&vcCvx, supports, supportSlop) {
						continue
					}
					if t, n, pt, hit := sweepConvexVsStatic3D(isBullet, evalA, &vcCvx, mot, reachA, extA, targetSep, tol, best.t); hit {
						// Outward normal of the voxel block points from voxel toward body (-n).
						if normalOccluded(vc, x, y, z, n.neg().vec3()) {
							continue
						}
						cand := ccdHit3D{
							found:    true,
							t:        t,
							n:        n,
							point:    pt,
							kind:     ccdHitVoxel,
							targetID: vc.ID,
							childA:   childA,
							childB:   -1,
							subIdx:   x,
							subY:     y,
							subZ:     z,
							voxel:    vc,
						}
						if cand.betterThan(*best) {
							*best = cand
						}
					}
				}
			}
		}
	}

	delta := mot.pos1.Sub(mot.pos0)
	dist := delta.Len()
	if mot.hasRot || dist <= max(vs*4, float32(1)) {
		testRegion(swept)
		return
	}

	chunkLen := max(vs*4, float32(1))
	chunks := max(1, int(math.Ceil(float64(dist/chunkLen))))
	for seg := range chunks {
		f0 := float64(seg) / float64(chunks)
		if best.found && best.t < f0 {
			break
		}
		f1 := float64(seg+1) / float64(chunks)
		segStart := bounds0.Translated(delta.Scale(float32(f0)))
		segEnd := bounds0.Translated(delta.Scale(float32(f1)))
		testRegion(unionBounds(segStart, segEnd))
	}
}

// resolveImpact3D removes b's approach velocity into the surface it struck
// (transferring momentum to a dynamic target), applying restitution above
// restitutionVelThreshold and using a multi-point block LCP when a flat face
// meets a planar surface so flat landings stay flat.
func (w *World) resolveImpact3D(b *Body, hit *ccdHit3D, targetSep float64) {
	o := hit.body
	var e float32
	switch {
	case o != nil:
		e = max(b.effectiveRestitution(), o.effectiveRestitution())
	case hit.triMesh != nil:
		e = max(b.effectiveRestitution(), hit.triMesh.Restitution)
	case hit.heightfield != nil:
		e = max(b.effectiveRestitution(), hit.heightfield.Restitution)
	case hit.voxel != nil:
		e = max(b.effectiveRestitution(), hit.voxel.Restitution)
	default:
		e = b.effectiveRestitution()
	}

	outN := hit.n.neg().vec3().Norm()
	if outN.LenSq() == 0 {
		return
	}
	invA := activeInvMass(b)
	invTA := activeWorldInvInertiaMatrix(b)
	var invB float32
	var invTB SymMat3
	if o != nil {
		invB = activeInvMass(o)
		invTB = activeWorldInvInertiaMatrix(o)
	}
	if invA+invB == 0 && invTA.IsZero() && invTB.IsZero() {
		return
	}

	// Gather a contact manifold by shifting b slightly along hit.n so flat faces
	// resolve across all corners simultaneously without tipping torque.
	shift := float32(targetSep) + 0.002
	shiftVec := hit.n.vec3().Norm().Scale(shift)
	shapeA := b.worldChildShape(hit.childA).Translated(shiftVec)
	var mpts []manifoldPoint
	var mNormal Vec3
	if o != nil {
		shapeB := o.worldChildShape(hit.childB)
		if c := testShapesSolid(shapeA, shapeB); c.Hit && c.Normal.Dot(outN) > 0.9 {
			w.manifoldBuf = buildShapeManifold(shapeA, shapeB, c, w.manifoldBuf[:0])
			mpts = w.manifoldBuf
			mNormal = c.Normal
		}
	} else if hit.hasTri {
		var c Contact
		if hit.heightfield != nil {
			c = testShapeHeightfieldTriangleSolid(shapeA, hit.tri)
		} else {
			c = testShapeTriangleSolid(shapeA, hit.tri)
		}
		if c.Hit && c.Normal.Dot(outN) > 0.9 {
			w.manifoldBuf = buildTriangleManifold(shapeA, hit.tri, c, false, w.manifoldBuf[:0])
			mpts = w.manifoldBuf
			mNormal = c.Normal
		}
	}

	comA := b.WorldCenterOfMass()
	var comB Vec3
	if o != nil {
		comB = o.WorldCenterOfMass()
	}

	var normals, rAs, rBs [maxManifoldPoints]Vec3
	var rhs, lam [maxManifoldPoints]float64
	count := 0
	approaching := false

	if len(mpts) > 0 {
		n := mNormal
		for i := 0; i < len(mpts) && count < maxManifoldPoints; i++ {
			pt := mpts[i].point.Sub(shiftVec)
			rA := pt.Sub(comA)
			var rB Vec3
			if o != nil {
				rB = pt.Sub(comB)
			}
			vn := pointVel(b, rA).Sub(pointVel(o, rB)).Dot(n)
			if vn < 0 {
				approaching = true
			}
			var bias float32
			if -vn > restitutionVelThreshold {
				bias = -e * vn
			}
			normals[count] = n
			rAs[count] = rA
			rBs[count] = rB
			rhs[count] = -float64(vn - bias)
			count++
		}
	}
	if count == 0 {
		pt := hit.point.vec3()
		rA := pt.Sub(comA)
		var rB Vec3
		if o != nil {
			rB = pt.Sub(comB)
		}
		vn := pointVel(b, rA).Sub(pointVel(o, rB)).Dot(outN)
		if vn < 0 {
			approaching = true
		}
		var bias float32
		if -vn > restitutionVelThreshold {
			bias = -e * vn
		}
		normals[0] = outN
		rAs[0] = rA
		rBs[0] = rB
		rhs[0] = -float64(vn - bias)
		count = 1
	}
	if !approaching {
		return
	}

	if count >= 2 && solveBlockLCP(count, invA, invTA, invB, invTB, normals[:count], rAs[:count], rBs[:count], rhs[:count], nil, lam[:count]) {
		var totalLin, totalAngA, totalAngB Vec3
		for k := range count {
			if !(lam[k] > 0) || math.IsNaN(lam[k]) || math.IsInf(lam[k], 0) {
				continue
			}
			imp := normals[k].Scale(float32(lam[k]))
			totalLin = totalLin.Add(imp)
			totalAngA = totalAngA.Add(rAs[k].Cross(imp))
			if o != nil {
				totalAngB = totalAngB.Add(rBs[k].Cross(imp))
			}
		}
		if invA > 0 {
			b.Vel = b.Vel.Add(totalLin.Scale(invA))
		}
		if !invTA.IsZero() {
			b.AngVel = b.AngVel.Add(invTA.MulVec(totalAngA))
		}
		if o != nil {
			if invB > 0 {
				o.Vel = o.Vel.Sub(totalLin.Scale(invB))
			}
			if !invTB.IsZero() {
				o.AngVel = o.AngVel.Sub(invTB.MulVec(totalAngB))
			}
		}
		return
	}

	// Single-point (or block-LCP fallback) sequential normal impulse.
	for k := range count {
		if !(rhs[k] > 0) {
			continue
		}
		n := normals[k]
		rA, rB := rAs[k], rBs[k]
		_, hasTA := activeWorldInvInertia(b)
		_, hasTB := activeWorldInvInertia(o)
		var kn float32
		if hasTA || hasTB {
			kn = contactEffectiveMassBodies(b, rA, o, rB, n)
		} else {
			invIA := activeInvInertia(b)
			var invIB float32
			if o != nil {
				invIB = activeInvInertia(o)
			}
			kn = contactEffectiveMass(invA, invIA, rA, invB, invIB, rB, n)
		}
		if kn <= 0 {
			continue
		}
		vn := pointVel(b, rA).Sub(pointVel(o, rB)).Dot(n)
		var bias float32
		if -vn > restitutionVelThreshold {
			bias = -e * vn
		}
		j := -(vn - bias) / kn
		if j <= 0 {
			continue
		}
		imp := n.Scale(j)
		applyContactImpulse(b, rA, imp)
		if o != nil {
			applyContactImpulse(o, rB, imp.Neg())
		}
	}
}
