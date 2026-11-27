package physics3d

import (
	"cmp"
	"slices"
)

const (
	aabbNullNode int32 = -1
	// broadphaseDynamicMargin pads dynamic/kinematic/trigger leaf AABBs so
	// sleeping or slowly moving bodies stay inside their leaf proxy without
	// churning the tree.
	broadphaseDynamicMargin float32 = 0.1
	// broadphaseDisplacementMultiplier expands a re-inserted dynamic leaf in
	// its motion direction so fast-moving bodies avoid re-inserting every step.
	broadphaseDisplacementMultiplier float32 = 2.0
)

type aabbBounds struct {
	min Vec3
	max Vec3
}

// aabbBoundsFromBox converts a center/half-extent Box to a conservative min/max
// AABB expanded by at least margin and a scale-aware epsilon so float32
// rounding in Center +/- HalfExtents can never produce a false negative
// relative to aabbOverlap(Box, Box) or rayIntersectsBoxDist.
func aabbBoundsFromBox(b Box, margin float32) aabbBounds {
	bMin := b.Min()
	bMax := b.Max()
	maxCoord := max(
		abs32(bMin.X), abs32(bMin.Y), abs32(bMin.Z),
		abs32(bMax.X), abs32(bMax.Y), abs32(bMax.Z),
	)
	eps := max(float32(1e-4), float32(maxCoord*1e-5))
	pad := max(margin, eps)
	return aabbBounds{
		min: Vec3{X: bMin.X - pad, Y: bMin.Y - pad, Z: bMin.Z - pad},
		max: Vec3{X: bMax.X + pad, Y: bMax.Y + pad, Z: bMax.Z + pad},
	}
}

func unionAABB(a, b aabbBounds) aabbBounds {
	return aabbBounds{
		min: Vec3{X: min(a.min.X, b.min.X), Y: min(a.min.Y, b.min.Y), Z: min(a.min.Z, b.min.Z)},
		max: Vec3{X: max(a.max.X, b.max.X), Y: max(a.max.Y, b.max.Y), Z: max(a.max.Z, b.max.Z)},
	}
}

func aabbBoundsOverlap(a, b aabbBounds) bool {
	return a.min.X <= b.max.X && a.max.X >= b.min.X &&
		a.min.Y <= b.max.Y && a.max.Y >= b.min.Y &&
		a.min.Z <= b.max.Z && a.max.Z >= b.min.Z
}

func (b aabbBounds) contains(inner aabbBounds) bool {
	return b.min.X <= inner.min.X && b.max.X >= inner.max.X &&
		b.min.Y <= inner.min.Y && b.max.Y >= inner.max.Y &&
		b.min.Z <= inner.min.Z && b.max.Z >= inner.max.Z
}

// surfaceArea returns the surface area of b using FMA-free arithmetic.
func (b aabbBounds) surfaceArea() float32 {
	dx := b.max.X - b.min.X
	dy := b.max.Y - b.min.Y
	dz := b.max.Z - b.min.Z
	return float32(2) * (float32(float32(dx*dy)+float32(dy*dz)) + float32(dz*dx))
}

type aabbNode struct {
	bounds  aabbBounds
	parent  int32 // parent index, or next free node index when height == -1
	child1  int32
	child2  int32
	height  int32 // 0 for leaf, >0 for internal, -1 for free
	payload int32 // proxy index for leaves, -1 for internal/free
}

// aabbTree is a deterministic, FMA-free dynamic AABB tree with SAH insertion
// and AVL tree rotations.
type aabbTree struct {
	root     int32
	nodes    []aabbNode
	freeList int32
}

func (t *aabbTree) reset() {
	t.root = aabbNullNode
	t.nodes = t.nodes[:0]
	t.freeList = aabbNullNode
}

func (t *aabbTree) allocateNode() int32 {
	if len(t.nodes) == 0 {
		t.root = aabbNullNode
		t.freeList = aabbNullNode
	}
	if t.freeList != aabbNullNode {
		id := t.freeList
		t.freeList = t.nodes[id].parent
		t.nodes[id] = aabbNode{
			parent:  aabbNullNode,
			child1:  aabbNullNode,
			child2:  aabbNullNode,
			height:  0,
			payload: -1,
		}
		return id
	}
	id := int32(len(t.nodes))
	t.nodes = append(t.nodes, aabbNode{
		parent:  aabbNullNode,
		child1:  aabbNullNode,
		child2:  aabbNullNode,
		height:  0,
		payload: -1,
	})
	return id
}

func (t *aabbTree) freeNode(id int32) {
	t.nodes[id].parent = t.freeList
	t.nodes[id].child1 = aabbNullNode
	t.nodes[id].child2 = aabbNullNode
	t.nodes[id].height = -1
	t.nodes[id].payload = -1
	t.freeList = id
}

func (t *aabbTree) createProxy(bounds aabbBounds, payload int32) int32 {
	id := t.allocateNode()
	t.nodes[id].bounds = bounds
	t.nodes[id].payload = payload
	t.nodes[id].height = 0
	t.insertLeaf(id)
	return id
}

func (t *aabbTree) destroyProxy(id int32) {
	if id == aabbNullNode {
		return
	}
	t.removeLeaf(id)
	t.freeNode(id)
}

func (t *aabbTree) moveProxy(id int32, tight aabbBounds, margin float32, disp Vec3) bool {
	if t.nodes[id].bounds.contains(tight) {
		return false
	}
	t.removeLeaf(id)

	fat := aabbBounds{
		min: Vec3{X: tight.min.X - margin, Y: tight.min.Y - margin, Z: tight.min.Z - margin},
		max: Vec3{X: tight.max.X + margin, Y: tight.max.Y + margin, Z: tight.max.Z + margin},
	}
	dx := float32(disp.X * broadphaseDisplacementMultiplier)
	if dx < 0 {
		fat.min.X += dx
	} else if dx > 0 {
		fat.max.X += dx
	}
	dy := float32(disp.Y * broadphaseDisplacementMultiplier)
	if dy < 0 {
		fat.min.Y += dy
	} else if dy > 0 {
		fat.max.Y += dy
	}
	dz := float32(disp.Z * broadphaseDisplacementMultiplier)
	if dz < 0 {
		fat.min.Z += dz
	} else if dz > 0 {
		fat.max.Z += dz
	}

	t.nodes[id].bounds = fat
	t.insertLeaf(id)
	return true
}

func (t *aabbTree) insertLeaf(leaf int32) {
	if t.root == aabbNullNode {
		t.root = leaf
		t.nodes[leaf].parent = aabbNullNode
		return
	}

	leafBounds := t.nodes[leaf].bounds
	idx := t.root
	for t.nodes[idx].child1 != aabbNullNode {
		c1 := t.nodes[idx].child1
		c2 := t.nodes[idx].child2

		area := t.nodes[idx].bounds.surfaceArea()
		combined := unionAABB(t.nodes[idx].bounds, leafBounds)
		combinedArea := combined.surfaceArea()

		// Cost of creating a new parent for this node and the new leaf.
		cost := float32(2) * combinedArea
		// Minimum cost of pushing the leaf further down the tree.
		inheritanceCost := float32(2) * (combinedArea - area)

		var cost1 float32
		b1 := unionAABB(leafBounds, t.nodes[c1].bounds)
		if t.nodes[c1].child1 == aabbNullNode {
			cost1 = b1.surfaceArea() + inheritanceCost
		} else {
			oldArea := t.nodes[c1].bounds.surfaceArea()
			newArea := b1.surfaceArea()
			cost1 = (newArea - oldArea) + inheritanceCost
		}

		var cost2 float32
		b2 := unionAABB(leafBounds, t.nodes[c2].bounds)
		if t.nodes[c2].child1 == aabbNullNode {
			cost2 = b2.surfaceArea() + inheritanceCost
		} else {
			oldArea := t.nodes[c2].bounds.surfaceArea()
			newArea := b2.surfaceArea()
			cost2 = (newArea - oldArea) + inheritanceCost
		}

		if cost < cost1 && cost < cost2 {
			break
		}
		if cost1 <= cost2 {
			idx = c1
		} else {
			idx = c2
		}
	}

	sibling := idx
	oldParent := t.nodes[sibling].parent
	newParent := t.allocateNode()
	t.nodes[newParent].parent = oldParent
	t.nodes[newParent].payload = -1
	t.nodes[newParent].bounds = unionAABB(leafBounds, t.nodes[sibling].bounds)
	t.nodes[newParent].height = t.nodes[sibling].height + 1
	t.nodes[newParent].child1 = sibling
	t.nodes[newParent].child2 = leaf
	t.nodes[sibling].parent = newParent
	t.nodes[leaf].parent = newParent

	if oldParent != aabbNullNode {
		if t.nodes[oldParent].child1 == sibling {
			t.nodes[oldParent].child1 = newParent
		} else {
			t.nodes[oldParent].child2 = newParent
		}
	} else {
		t.root = newParent
	}

	// Walk back up the tree fixing heights and AABBs with AVL rotations.
	idx = t.nodes[leaf].parent
	for idx != aabbNullNode {
		idx = t.balance(idx)
		c1 := t.nodes[idx].child1
		c2 := t.nodes[idx].child2
		t.nodes[idx].height = 1 + max(t.nodes[c1].height, t.nodes[c2].height)
		t.nodes[idx].bounds = unionAABB(t.nodes[c1].bounds, t.nodes[c2].bounds)
		idx = t.nodes[idx].parent
	}
}

func (t *aabbTree) removeLeaf(leaf int32) {
	if leaf == t.root {
		t.root = aabbNullNode
		return
	}

	parent := t.nodes[leaf].parent
	grandParent := t.nodes[parent].parent
	sibling := t.nodes[parent].child2
	if t.nodes[parent].child1 != leaf {
		sibling = t.nodes[parent].child1
	}

	if grandParent != aabbNullNode {
		if t.nodes[grandParent].child1 == parent {
			t.nodes[grandParent].child1 = sibling
		} else {
			t.nodes[grandParent].child2 = sibling
		}
		t.nodes[sibling].parent = grandParent
		t.freeNode(parent)

		idx := grandParent
		for idx != aabbNullNode {
			idx = t.balance(idx)
			c1 := t.nodes[idx].child1
			c2 := t.nodes[idx].child2
			t.nodes[idx].bounds = unionAABB(t.nodes[c1].bounds, t.nodes[c2].bounds)
			t.nodes[idx].height = 1 + max(t.nodes[c1].height, t.nodes[c2].height)
			idx = t.nodes[idx].parent
		}
	} else {
		t.root = sibling
		t.nodes[sibling].parent = aabbNullNode
		t.freeNode(parent)
	}
}

// balance performs an AVL height-restoring rotation when node iA is imbalanced
// by more than 1 level, or a Box2D v3 style SAH-minimizing rotation when
// balanced, returning the new subtree root index.
func (t *aabbTree) balance(iA int32) int32 {
	nodes := t.nodes
	a := &nodes[iA]
	if a.child1 == aabbNullNode || a.height < 2 {
		return iA
	}

	iB := a.child1
	iC := a.child2
	b := &nodes[iB]
	c := &nodes[iC]

	bal := c.height - b.height

	// Rotate C up (mandatory when bal > 1).
	if bal > 1 {
		iF := c.child1
		iG := c.child2
		f := &nodes[iF]
		g := &nodes[iG]

		c.child1 = iA
		c.parent = a.parent
		a.parent = iC

		if c.parent != aabbNullNode {
			if nodes[c.parent].child1 == iA {
				nodes[c.parent].child1 = iC
			} else {
				nodes[c.parent].child2 = iC
			}
		} else {
			t.root = iC
		}

		// Pick the child that keeps height balanced; break equal heights by SAH.
		keepF := f.height > g.height
		if f.height == g.height {
			keepF = unionAABB(b.bounds, g.bounds).surfaceArea() < unionAABB(b.bounds, f.bounds).surfaceArea()
		}
		if keepF {
			c.child2 = iF
			a.child2 = iG
			g.parent = iA
			a.bounds = unionAABB(b.bounds, g.bounds)
			c.bounds = unionAABB(a.bounds, f.bounds)
			a.height = 1 + max(b.height, g.height)
			c.height = 1 + max(a.height, f.height)
		} else {
			c.child2 = iG
			a.child2 = iF
			f.parent = iA
			a.bounds = unionAABB(b.bounds, f.bounds)
			c.bounds = unionAABB(a.bounds, g.bounds)
			a.height = 1 + max(b.height, f.height)
			c.height = 1 + max(a.height, g.height)
		}
		return iC
	}

	// Rotate B up (mandatory when bal < -1).
	if bal < -1 {
		iD := b.child1
		iE := b.child2
		d := &nodes[iD]
		e := &nodes[iE]

		b.child1 = iA
		b.parent = a.parent
		a.parent = iB

		if b.parent != aabbNullNode {
			if nodes[b.parent].child1 == iA {
				nodes[b.parent].child1 = iB
			} else {
				nodes[b.parent].child2 = iB
			}
		} else {
			t.root = iB
		}

		keepD := d.height > e.height
		if d.height == e.height {
			keepD = unionAABB(c.bounds, e.bounds).surfaceArea() < unionAABB(c.bounds, d.bounds).surfaceArea()
		}
		if keepD {
			b.child2 = iD
			a.child1 = iE
			e.parent = iA
			a.bounds = unionAABB(c.bounds, e.bounds)
			b.bounds = unionAABB(a.bounds, d.bounds)
			a.height = 1 + max(c.height, e.height)
			b.height = 1 + max(a.height, d.height)
		} else {
			b.child2 = iE
			a.child1 = iD
			d.parent = iA
			a.bounds = unionAABB(c.bounds, d.bounds)
			b.bounds = unionAABB(a.bounds, e.bounds)
			a.height = 1 + max(c.height, d.height)
			b.height = 1 + max(a.height, e.height)
		}
		return iB
	}

	// Height is balanced: check if a height-preserving SAH rotation reduces
	// total surface area (Box2D v3 style).
	bestRot := 0
	bestDelta := float32(-1e-5)

	if c.child1 != aabbNullNode {
		iF := c.child1
		iG := c.child2
		f := &nodes[iF]
		g := &nodes[iG]
		oldC := c.bounds.surfaceArea()
		// Rot 1: swap B with F -> new C holds (B, G) -> actually A holds (B, G), C holds (A, F).
		newAH := 1 + max(b.height, g.height)
		if d := f.height - newAH; d >= -1 && d <= 1 {
			if delta := unionAABB(b.bounds, g.bounds).surfaceArea() - oldC; delta < bestDelta {
				bestDelta = delta
				bestRot = 1
			}
		}
		// Rot 2: A holds (B, F), C holds (A, G).
		newAH = 1 + max(b.height, f.height)
		if d := g.height - newAH; d >= -1 && d <= 1 {
			if delta := unionAABB(b.bounds, f.bounds).surfaceArea() - oldC; delta < bestDelta {
				bestDelta = delta
				bestRot = 2
			}
		}
	}
	if b.child1 != aabbNullNode {
		iD := b.child1
		iE := b.child2
		d := &nodes[iD]
		e := &nodes[iE]
		oldB := b.bounds.surfaceArea()
		// Rot 3: A holds (E, C), B holds (A, D).
		newAH := 1 + max(c.height, e.height)
		if diff := d.height - newAH; diff >= -1 && diff <= 1 {
			if delta := unionAABB(c.bounds, e.bounds).surfaceArea() - oldB; delta < bestDelta {
				bestDelta = delta
				bestRot = 3
			}
		}
		// Rot 4: A holds (D, C), B holds (A, E).
		newAH = 1 + max(c.height, d.height)
		if diff := e.height - newAH; diff >= -1 && diff <= 1 {
			if delta := unionAABB(c.bounds, d.bounds).surfaceArea() - oldB; delta < bestDelta {
				bestDelta = delta
				bestRot = 4
			}
		}
	}

	switch bestRot {
	case 1, 2:
		iF := c.child1
		iG := c.child2
		f := &nodes[iF]
		g := &nodes[iG]
		c.child1 = iA
		c.parent = a.parent
		a.parent = iC
		if c.parent != aabbNullNode {
			if nodes[c.parent].child1 == iA {
				nodes[c.parent].child1 = iC
			} else {
				nodes[c.parent].child2 = iC
			}
		} else {
			t.root = iC
		}
		if bestRot == 1 {
			c.child2 = iF
			a.child2 = iG
			g.parent = iA
			a.bounds = unionAABB(b.bounds, g.bounds)
			c.bounds = unionAABB(a.bounds, f.bounds)
			a.height = 1 + max(b.height, g.height)
			c.height = 1 + max(a.height, f.height)
		} else {
			c.child2 = iG
			a.child2 = iF
			f.parent = iA
			a.bounds = unionAABB(b.bounds, f.bounds)
			c.bounds = unionAABB(a.bounds, g.bounds)
			a.height = 1 + max(b.height, f.height)
			c.height = 1 + max(a.height, g.height)
		}
		return iC
	case 3, 4:
		iD := b.child1
		iE := b.child2
		d := &nodes[iD]
		e := &nodes[iE]
		b.child1 = iA
		b.parent = a.parent
		a.parent = iB
		if b.parent != aabbNullNode {
			if nodes[b.parent].child1 == iA {
				nodes[b.parent].child1 = iB
			} else {
				nodes[b.parent].child2 = iB
			}
		} else {
			t.root = iB
		}
		if bestRot == 3 {
			b.child2 = iD
			a.child1 = iE
			e.parent = iA
			a.bounds = unionAABB(c.bounds, e.bounds)
			b.bounds = unionAABB(a.bounds, d.bounds)
			a.height = 1 + max(c.height, e.height)
			b.height = 1 + max(a.height, d.height)
		} else {
			b.child2 = iE
			a.child1 = iD
			d.parent = iA
			a.bounds = unionAABB(c.bounds, d.bounds)
			b.bounds = unionAABB(a.bounds, e.bounds)
			a.height = 1 + max(c.height, d.height)
			b.height = 1 + max(a.height, e.height)
		}
		return iB
	}

	return iA
}

type topDownLeaf struct {
	bounds   aabbBounds
	centroid Vec3
	payload  int32
}

// buildTopDown constructs a balanced spatial BVH over items using deterministic
// longest-axis median splits. onLeaf is called for each created leaf node.
func (t *aabbTree) buildTopDown(items []topDownLeaf, onLeaf func(payload, nodeID int32)) {
	t.reset()
	if len(items) == 0 {
		return
	}
	wantNodes := 2*len(items) - 1
	if cap(t.nodes) < wantNodes {
		t.nodes = make([]aabbNode, 0, wantNodes)
	}
	var build func(slice []topDownLeaf, parent int32) int32
	build = func(slice []topDownLeaf, parent int32) int32 {
		if len(slice) == 1 {
			id := int32(len(t.nodes))
			t.nodes = append(t.nodes, aabbNode{
				bounds:  slice[0].bounds,
				parent:  parent,
				child1:  aabbNullNode,
				child2:  aabbNullNode,
				height:  0,
				payload: slice[0].payload,
			})
			onLeaf(slice[0].payload, id)
			return id
		}

		cMin := slice[0].centroid
		cMax := slice[0].centroid
		for i := 1; i < len(slice); i++ {
			c := slice[i].centroid
			cMin.X = min(cMin.X, c.X)
			cMin.Y = min(cMin.Y, c.Y)
			cMin.Z = min(cMin.Z, c.Z)
			cMax.X = max(cMax.X, c.X)
			cMax.Y = max(cMax.Y, c.Y)
			cMax.Z = max(cMax.Z, c.Z)
		}
		dx := cMax.X - cMin.X
		dy := cMax.Y - cMin.Y
		dz := cMax.Z - cMin.Z
		axis := 0
		if dy > dx && dy >= dz {
			axis = 1
		} else if dz > dx && dz > dy {
			axis = 2
		}

		switch axis {
		case 0:
			slices.SortFunc(slice, func(a, b topDownLeaf) int {
				if c := cmp.Compare(a.centroid.X, b.centroid.X); c != 0 {
					return c
				}
				return cmp.Compare(a.payload, b.payload)
			})
		case 1:
			slices.SortFunc(slice, func(a, b topDownLeaf) int {
				if c := cmp.Compare(a.centroid.Y, b.centroid.Y); c != 0 {
					return c
				}
				return cmp.Compare(a.payload, b.payload)
			})
		default:
			slices.SortFunc(slice, func(a, b topDownLeaf) int {
				if c := cmp.Compare(a.centroid.Z, b.centroid.Z); c != 0 {
					return c
				}
				return cmp.Compare(a.payload, b.payload)
			})
		}

		id := int32(len(t.nodes))
		t.nodes = append(t.nodes, aabbNode{
			parent:  parent,
			child1:  aabbNullNode,
			child2:  aabbNullNode,
			payload: -1,
		})
		mid := len(slice) / 2
		c1 := build(slice[:mid], id)
		c2 := build(slice[mid:], id)
		t.nodes[id].child1 = c1
		t.nodes[id].child2 = c2
		t.nodes[id].height = 1 + max(t.nodes[c1].height, t.nodes[c2].height)
		t.nodes[id].bounds = unionAABB(t.nodes[c1].bounds, t.nodes[c2].bounds)
		return id
	}
	t.root = build(items, aabbNullNode)
}

func (t *aabbTree) queryAABB(bounds aabbBounds, visit func(payload int32) bool) {
	if len(t.nodes) == 0 || t.root == aabbNullNode {
		return
	}
	nodes := t.nodes
	if !aabbBoundsOverlap(nodes[t.root].bounds, bounds) {
		return
	}
	var stackBuf [64]int32
	stack := append(stackBuf[:0], t.root)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := &nodes[id]
		if n.child1 == aabbNullNode {
			if !visit(n.payload) {
				return
			}
			continue
		}
		c2 := n.child2
		if aabbBoundsOverlap(nodes[c2].bounds, bounds) {
			stack = append(stack, c2)
		}
		c1 := n.child1
		if aabbBoundsOverlap(nodes[c1].bounds, bounds) {
			stack = append(stack, c1)
		}
	}
}

type raySlab struct {
	origin Vec3
	dir    Vec3
	invDir Vec3
	axisOk [3]bool
}

func newRaySlab(origin, delta Vec3) (raySlab, bool) {
	dir := delta.Norm()
	if dir.LenSq() == 0 {
		return raySlab{}, false
	}
	r := raySlab{origin: origin, dir: dir}
	if abs32(dir.X) >= 1e-8 {
		r.axisOk[0] = true
		r.invDir.X = 1 / dir.X
	}
	if abs32(dir.Y) >= 1e-8 {
		r.axisOk[1] = true
		r.invDir.Y = 1 / dir.Y
	}
	if abs32(dir.Z) >= 1e-8 {
		r.axisOk[2] = true
		r.invDir.Z = 1 / dir.Z
	}
	return r, true
}

func (r *raySlab) test(b aabbBounds, maxDist float32) (float32, bool) {
	tMin := float32(0)
	tMax := maxDist

	if r.axisOk[0] {
		t1 := (b.min.X - r.origin.X) * r.invDir.X
		t2 := (b.max.X - r.origin.X) * r.invDir.X
		tMin = max(tMin, min(t1, t2))
		tMax = min(tMax, max(t1, t2))
	} else if r.origin.X < b.min.X || r.origin.X > b.max.X {
		return 0, false
	}

	if r.axisOk[1] {
		t1 := (b.min.Y - r.origin.Y) * r.invDir.Y
		t2 := (b.max.Y - r.origin.Y) * r.invDir.Y
		tMin = max(tMin, min(t1, t2))
		tMax = min(tMax, max(t1, t2))
	} else if r.origin.Y < b.min.Y || r.origin.Y > b.max.Y {
		return 0, false
	}

	if r.axisOk[2] {
		t1 := (b.min.Z - r.origin.Z) * r.invDir.Z
		t2 := (b.max.Z - r.origin.Z) * r.invDir.Z
		tMin = max(tMin, min(t1, t2))
		tMax = min(tMax, max(t1, t2))
	} else if r.origin.Z < b.min.Z || r.origin.Z > b.max.Z {
		return 0, false
	}

	return tMin, tMin <= tMax
}

type rayStackItem struct {
	node int32
	tMin float32
}

// raycastOrdered traverses the tree front-to-back along r, calling visit for
// each leaf intersected within [0, maxDist]. visit returns the updated maxDist
// (allowing closest-hit queries to prune farther subtrees) and whether to
// continue traversal.
func (t *aabbTree) raycastOrdered(r *raySlab, maxDist float32, visit func(payload int32, curMaxDist float32) (float32, bool)) float32 {
	if len(t.nodes) == 0 || t.root == aabbNullNode {
		return maxDist
	}
	nodes := t.nodes
	tRoot, ok := r.test(nodes[t.root].bounds, maxDist)
	if !ok {
		return maxDist
	}
	var stackBuf [64]rayStackItem
	stack := append(stackBuf[:0], rayStackItem{node: t.root, tMin: tRoot})
	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if item.tMin > maxDist {
			continue
		}
		n := &nodes[item.node]
		if n.child1 == aabbNullNode {
			var keep bool
			maxDist, keep = visit(n.payload, maxDist)
			if !keep {
				return maxDist
			}
			continue
		}
		c1 := n.child1
		c2 := n.child2
		t1, hit1 := r.test(nodes[c1].bounds, maxDist)
		t2, hit2 := r.test(nodes[c2].bounds, maxDist)
		if hit1 && hit2 {
			if t1 <= t2 {
				stack = append(stack, rayStackItem{node: c2, tMin: t2}, rayStackItem{node: c1, tMin: t1})
			} else {
				stack = append(stack, rayStackItem{node: c1, tMin: t1}, rayStackItem{node: c2, tMin: t2})
			}
		} else if hit1 {
			stack = append(stack, rayStackItem{node: c1, tMin: t1})
		} else if hit2 {
			stack = append(stack, rayStackItem{node: c2, tMin: t2})
		}
	}
	return maxDist
}

type bpKind uint8

const (
	bpKindNone bpKind = iota
	bpKindBody
	bpKindTriMesh
	bpKindHeightfield
)

type bpProxy struct {
	body        *Body
	triMesh     *TriMeshCollider
	heightfield *HeightfieldCollider
	cachedPos   Vec3
	cachedRot   Quat
	nodeID      int32
	order       int32
	kind        bpKind
	inDynamic   bool
	cachedShape Shape
	worldShape  Shape
	worldBounds Box
	prevPos     Vec3
	prevRot     Quat
}

type bpBodyPair struct {
	i, j int32
}

type bpOrderedProxy struct {
	order int32
	pid   int32
}

// broadphase3D maintains separate deterministic static and dynamic AABB trees
// for a physics3d.World.
//
// Static non-trigger bodies, TriMeshColliders, and HeightfieldColliders reside
// in staticTree (never colliding with each other and never churning during
// steps). Dynamic, kinematic, and trigger bodies reside in dynamicTree with
// fattened AABBs and predictive motion expansion so sleeping and slowly moving
// bodies rarely update the tree.
type broadphase3D struct {
	staticTree  aabbTree
	dynamicTree aabbTree

	proxies     []bpProxy
	freeProxies []int32

	bodyProxies    []int32
	triMeshProxies []int32
	hfProxies      []int32

	// dynamicList holds all body proxy IDs currently in dynamicTree, always
	// kept sorted in ascending w.bodies insertion order.
	dynamicList []int32

	// Reusable per-step scratch buffers.
	pairBuf        []bpBodyPair
	dynPrevPos     []Vec3
	tmOrderHits    []bpOrderedProxy
	hfOrderHits    []bpOrderedProxy
	tmHits         []*TriMeshCollider
	hfHits         []*HeightfieldCollider
	staticBuildBuf []topDownLeaf

	initialized bool
	staticDirty bool
	staticEdits int
	// bruteForce bypasses the AABB trees and scans proxies linearly in insertion
	// order; used only by tests to assert bit-identical parity.
	bruteForce bool
}

func shapesEqual(a, b Shape) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case ShapeBox:
		return a.Box == b.Box && a.Rot == b.Rot
	case ShapeSphere:
		return a.Sphere == b.Sphere
	case ShapeCapsule:
		return a.Capsule == b.Capsule
	case ShapeConvexHull:
		return a.Hull == b.Hull && a.Rot == b.Rot
	case ShapeCylinder:
		return a.Cylinder == b.Cylinder
	case ShapeCompound:
		if len(a.Children) != len(b.Children) {
			return false
		}
		for i := range a.Children {
			ca, cb := a.Children[i], b.Children[i]
			if ca.Offset != cb.Offset || ca.Rot != cb.Rot ||
				ca.Mass != cb.Mass || ca.Density != cb.Density ||
				ca.Trigger != cb.Trigger || ca.Layer != cb.Layer || ca.Mask != cb.Mask ||
				!shapesEqual(ca.Shape, cb.Shape) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (bp *broadphase3D) allocProxy() int32 {
	if n := len(bp.freeProxies); n > 0 {
		pid := bp.freeProxies[n-1]
		bp.freeProxies = bp.freeProxies[:n-1]
		bp.proxies[pid] = bpProxy{nodeID: aabbNullNode}
		return pid
	}
	pid := int32(len(bp.proxies))
	bp.proxies = append(bp.proxies, bpProxy{nodeID: aabbNullNode})
	return pid
}

func (bp *broadphase3D) freeProxy(pid int32) {
	bp.proxies[pid] = bpProxy{nodeID: aabbNullNode}
	bp.freeProxies = append(bp.freeProxies, pid)
}

func (bp *broadphase3D) insertDynamicList(pid int32) {
	ord := bp.proxies[pid].order
	pos, _ := slices.BinarySearchFunc(bp.dynamicList, ord, func(elem int32, target int32) int {
		return cmp.Compare(bp.proxies[elem].order, target)
	})
	bp.dynamicList = slices.Insert(bp.dynamicList, pos, pid)
}

func (bp *broadphase3D) removeDynamicList(pid int32) {
	for i, elem := range bp.dynamicList {
		if elem == pid {
			bp.dynamicList = append(bp.dynamicList[:i], bp.dynamicList[i+1:]...)
			return
		}
	}
}

func (bp *broadphase3D) ensureInit(w *World) {
	if bp.initialized &&
		len(bp.bodyProxies) == len(w.bodies) &&
		len(bp.triMeshProxies) == len(w.triMeshes) &&
		len(bp.hfProxies) == len(w.heightfields) {
		return
	}
	bp.rebuild(w)
}

func (bp *broadphase3D) rebuild(w *World) {
	bp.staticTree.reset()
	bp.dynamicTree.reset()
	bp.proxies = bp.proxies[:0]
	bp.freeProxies = bp.freeProxies[:0]
	bp.bodyProxies = bp.bodyProxies[:0]
	bp.triMeshProxies = bp.triMeshProxies[:0]
	bp.hfProxies = bp.hfProxies[:0]
	bp.dynamicList = bp.dynamicList[:0]
	bp.initialized = true
	bp.staticDirty = true
	bp.staticEdits = 0

	for i, b := range w.bodies {
		bp.insertBodyEntry(b, i)
	}
	for i, tm := range w.triMeshes {
		bp.insertTriMeshEntry(tm, i)
	}
	for i, hf := range w.heightfields {
		bp.insertHeightfieldEntry(hf, i)
	}
}

func (bp *broadphase3D) ensureStaticTree() {
	if !bp.staticDirty {
		return
	}
	items := bp.staticBuildBuf[:0]
	for _, pid := range bp.bodyProxies {
		p := &bp.proxies[pid]
		if p.inDynamic {
			continue
		}
		ab := aabbBoundsFromBox(p.worldBounds, 0)
		items = append(items, topDownLeaf{
			bounds:   ab,
			centroid: p.worldBounds.Center,
			payload:  pid,
		})
	}
	for _, pid := range bp.triMeshProxies {
		p := &bp.proxies[pid]
		if p.triMesh == nil || len(p.triMesh.nodes) == 0 {
			p.nodeID = aabbNullNode
			continue
		}
		ab := aabbBoundsFromBox(p.triMesh.Bounds, 0)
		items = append(items, topDownLeaf{
			bounds:   ab,
			centroid: p.triMesh.Bounds.Center,
			payload:  pid,
		})
	}
	for _, pid := range bp.hfProxies {
		p := &bp.proxies[pid]
		if p.heightfield == nil || !p.heightfield.valid() {
			p.nodeID = aabbNullNode
			continue
		}
		ab := aabbBoundsFromBox(p.heightfield.Bounds, 0)
		items = append(items, topDownLeaf{
			bounds:   ab,
			centroid: p.heightfield.Bounds.Center,
			payload:  pid,
		})
	}
	bp.staticTree.buildTopDown(items, func(pid, nodeID int32) {
		bp.proxies[pid].nodeID = nodeID
	})
	bp.staticBuildBuf = items[:0]
	bp.staticDirty = false
	bp.staticEdits = 0
}

func (bp *broadphase3D) insertBodyEntry(b *Body, order int) {
	pid := bp.allocProxy()
	ws := b.WorldShape()
	wb := ws.Bounds()
	isDyn := bodyIsBroadphaseDynamic(b)
	nodeID := aabbNullNode
	if isDyn {
		nodeID = bp.dynamicTree.createProxy(aabbBoundsFromBox(wb, broadphaseDynamicMargin), pid)
	} else if !bp.staticDirty {
		nodeID = bp.staticTree.createProxy(aabbBoundsFromBox(wb, 0), pid)
		bp.staticEdits++
		if bp.staticEdits >= 64 && bp.staticEdits*4 >= len(bp.bodyProxies)+1 {
			bp.staticDirty = true
		}
	}
	bp.proxies[pid] = bpProxy{
		body:        b,
		cachedPos:   b.Pos,
		cachedRot:   b.Rot,
		nodeID:      nodeID,
		order:       int32(order),
		kind:        bpKindBody,
		inDynamic:   isDyn,
		cachedShape: cloneShape(b.Shape),
		worldShape:  ws,
		worldBounds: wb,
	}
	bp.bodyProxies = append(bp.bodyProxies, pid)
	if isDyn {
		bp.dynamicList = append(bp.dynamicList, pid)
	}
}

func (bp *broadphase3D) addBody(w *World, b *Body) {
	if !bp.initialized || len(bp.bodyProxies) != len(w.bodies)-1 {
		bp.rebuild(w)
		return
	}
	bp.insertBodyEntry(b, len(w.bodies)-1)
}

func (bp *broadphase3D) removeBody(w *World, idx int) {
	if !bp.initialized || len(bp.bodyProxies) != len(w.bodies)+1 {
		bp.rebuild(w)
		return
	}
	pid := bp.bodyProxies[idx]
	p := &bp.proxies[pid]
	if p.inDynamic {
		bp.dynamicTree.destroyProxy(p.nodeID)
		bp.removeDynamicList(pid)
	} else if !bp.staticDirty {
		bp.staticTree.destroyProxy(p.nodeID)
	}
	bp.freeProxy(pid)
	bp.bodyProxies = append(bp.bodyProxies[:idx], bp.bodyProxies[idx+1:]...)
	for k := idx; k < len(bp.bodyProxies); k++ {
		bp.proxies[bp.bodyProxies[k]].order = int32(k)
	}
}

func (bp *broadphase3D) insertTriMeshEntry(tm *TriMeshCollider, order int) {
	pid := bp.allocProxy()
	nodeID := aabbNullNode
	if tm != nil && len(tm.nodes) > 0 && !bp.staticDirty {
		nodeID = bp.staticTree.createProxy(aabbBoundsFromBox(tm.Bounds, 0), pid)
		bp.staticEdits++
	}
	bp.proxies[pid] = bpProxy{
		triMesh: tm,
		nodeID:  nodeID,
		order:   int32(order),
		kind:    bpKindTriMesh,
	}
	bp.triMeshProxies = append(bp.triMeshProxies, pid)
}

func (bp *broadphase3D) addTriMesh(w *World, tm *TriMeshCollider) {
	if !bp.initialized || len(bp.triMeshProxies) != len(w.triMeshes)-1 {
		bp.rebuild(w)
		return
	}
	bp.insertTriMeshEntry(tm, len(w.triMeshes)-1)
}

func (bp *broadphase3D) removeTriMesh(w *World, idx int) {
	if !bp.initialized || len(bp.triMeshProxies) != len(w.triMeshes)+1 {
		bp.rebuild(w)
		return
	}
	pid := bp.triMeshProxies[idx]
	if !bp.staticDirty {
		bp.staticTree.destroyProxy(bp.proxies[pid].nodeID)
	}
	bp.freeProxy(pid)
	bp.triMeshProxies = append(bp.triMeshProxies[:idx], bp.triMeshProxies[idx+1:]...)
	for k := idx; k < len(bp.triMeshProxies); k++ {
		bp.proxies[bp.triMeshProxies[k]].order = int32(k)
	}
}

func (bp *broadphase3D) insertHeightfieldEntry(hf *HeightfieldCollider, order int) {
	pid := bp.allocProxy()
	nodeID := aabbNullNode
	if hf != nil && hf.valid() && !bp.staticDirty {
		nodeID = bp.staticTree.createProxy(aabbBoundsFromBox(hf.Bounds, 0), pid)
		bp.staticEdits++
	}
	bp.proxies[pid] = bpProxy{
		heightfield: hf,
		nodeID:      nodeID,
		order:       int32(order),
		kind:        bpKindHeightfield,
	}
	bp.hfProxies = append(bp.hfProxies, pid)
}

func (bp *broadphase3D) addHeightfield(w *World, hf *HeightfieldCollider) {
	if !bp.initialized || len(bp.hfProxies) != len(w.heightfields)-1 {
		bp.rebuild(w)
		return
	}
	bp.insertHeightfieldEntry(hf, len(w.heightfields)-1)
}

func (bp *broadphase3D) removeHeightfield(w *World, idx int) {
	if !bp.initialized || len(bp.hfProxies) != len(w.heightfields)+1 {
		bp.rebuild(w)
		return
	}
	pid := bp.hfProxies[idx]
	if !bp.staticDirty {
		bp.staticTree.destroyProxy(bp.proxies[pid].nodeID)
	}
	bp.freeProxy(pid)
	bp.hfProxies = append(bp.hfProxies[:idx], bp.hfProxies[idx+1:]...)
	for k := idx; k < len(bp.hfProxies); k++ {
		bp.proxies[bp.hfProxies[k]].order = int32(k)
	}
}

func (bp *broadphase3D) updateBodyProxy(pid int32, disp Vec3) {
	p := &bp.proxies[pid]
	b := p.body
	wantDyn := bodyIsBroadphaseDynamic(b)
	ws := b.WorldShape()
	wb := ws.Bounds()
	p.cachedPos = b.Pos
	p.cachedRot = b.Rot
	p.cachedShape = cloneShape(b.Shape)
	p.worldShape = ws
	p.worldBounds = wb

	if p.inDynamic != wantDyn {
		if p.inDynamic {
			bp.dynamicTree.destroyProxy(p.nodeID)
			bp.removeDynamicList(pid)
			p.inDynamic = false
			if bp.staticDirty {
				p.nodeID = aabbNullNode
			} else {
				p.nodeID = bp.staticTree.createProxy(aabbBoundsFromBox(wb, 0), pid)
			}
		} else {
			if !bp.staticDirty {
				bp.staticTree.destroyProxy(p.nodeID)
			}
			p.inDynamic = true
			p.nodeID = bp.dynamicTree.createProxy(aabbBoundsFromBox(wb, broadphaseDynamicMargin), pid)
			bp.insertDynamicList(pid)
		}
		return
	}

	tight := aabbBoundsFromBox(wb, 0)
	if p.inDynamic {
		bp.dynamicTree.moveProxy(p.nodeID, tight, broadphaseDynamicMargin, disp)
	} else if !bp.staticDirty {
		bp.staticTree.moveProxy(p.nodeID, tight, 0, Vec3{})
	}
}

// syncAllBodies reconciles any external mutations to Pos, Rot, Shape, Static,
// or IsTrigger across all bodies (called once per World.Step). Unchanged static
// bodies perform only a cheap equality check and zero tree updates.
func (bp *broadphase3D) syncAllBodies(w *World) {
	bp.ensureInit(w)
	for i, b := range w.bodies {
		pid := bp.bodyProxies[i]
		p := &bp.proxies[pid]
		wantDyn := bodyIsBroadphaseDynamic(b)
		if p.inDynamic == wantDyn && b.Pos == p.cachedPos && b.Rot == p.cachedRot && shapesEqual(b.Shape, p.cachedShape) {
			continue
		}
		bp.updateBodyProxy(pid, Vec3{})
	}
}

// syncDynamicBodies updates only proxies in dynamicList (dynamic, kinematic,
// and trigger bodies). When prevPos is non-nil (aligned with dynamicList), it
// also refreshes worldShape/worldBounds for all awake dynamic bodies so signed
// zero and subtle pose updates stay bit-exact, and uses per-step displacement
// for predictive AABB expansion.
func (bp *broadphase3D) syncDynamicBodies(w *World, prevPos []Vec3) {
	bp.ensureInit(w)
	// Walk forward and avoid advancing di when a body transitions from dynamic
	// to static and is removed from dynamicList.
	hasPrev := len(prevPos) == len(bp.dynamicList)
	for di := 0; di < len(bp.dynamicList); {
		pid := bp.dynamicList[di]
		p := &bp.proxies[pid]
		b := p.body
		wantDyn := bodyIsBroadphaseDynamic(b)
		if !wantDyn {
			bp.updateBodyProxy(pid, Vec3{})
			if hasPrev {
				prevPos = append(prevPos[:di], prevPos[di+1:]...)
			}
			continue
		}
		if b.Pos == p.cachedPos && b.Rot == p.cachedRot && shapesEqual(b.Shape, p.cachedShape) {
			if hasPrev && !b.Static && !b.Kinematic && !b.Sleeping {
				ws := b.WorldShape()
				p.worldShape = ws
				p.worldBounds = ws.Bounds()
			}
			di++
			continue
		}
		var disp Vec3
		if hasPrev {
			disp = b.Pos.Sub(prevPos[di])
		}
		bp.updateBodyProxy(pid, disp)
		di++
	}
}

// findBodyPairs queries dynamic-vs-dynamic and dynamic-vs-static overlaps and
// returns candidate body index pairs in strictly ascending (i, j) order with
// i < j, matching the exact iteration order of a brute-force double loop.
func (bp *broadphase3D) findBodyPairs() []bpBodyPair {
	bp.pairBuf = bp.pairBuf[:0]
	if bp.bruteForce {
		n := int32(len(bp.bodyProxies))
		for i := int32(0); i < n; i++ {
			a := bp.proxies[bp.bodyProxies[i]].body
			for j := i + 1; j < n; j++ {
				b := bp.proxies[bp.bodyProxies[j]].body
				if a.Static && b.Static && !bodyIsBroadphaseDynamic(a) && !bodyIsBroadphaseDynamic(b) {
					continue
				}
				bp.pairBuf = append(bp.pairBuf, bpBodyPair{i: i, j: j})
			}
		}
		return bp.pairBuf
	}
	bp.ensureStaticTree()
	for _, pid := range bp.dynamicList {
		p := &bp.proxies[pid]
		idxA := p.order
		queryBounds := aabbBoundsFromBox(p.worldBounds, 0)

		bp.dynamicTree.queryAABB(queryBounds, func(otherPID int32) bool {
			idxB := bp.proxies[otherPID].order
			if idxA < idxB {
				bp.pairBuf = append(bp.pairBuf, bpBodyPair{i: idxA, j: idxB})
			}
			return true
		})

		bp.staticTree.queryAABB(queryBounds, func(staticPID int32) bool {
			q := &bp.proxies[staticPID]
			if q.kind != bpKindBody {
				return true
			}
			idxB := q.order
			if idxA < idxB {
				bp.pairBuf = append(bp.pairBuf, bpBodyPair{i: idxA, j: idxB})
			} else {
				bp.pairBuf = append(bp.pairBuf, bpBodyPair{i: idxB, j: idxA})
			}
			return true
		})
	}

	if len(bp.pairBuf) > 1 {
		slices.SortFunc(bp.pairBuf, func(a, b bpBodyPair) int {
			if a.i != b.i {
				return cmp.Compare(a.i, b.i)
			}
			return cmp.Compare(a.j, b.j)
		})
	}
	return bp.pairBuf
}

// queryStaticMeshesAndHeightfields returns all TriMeshColliders and
// HeightfieldColliders in staticTree overlapping bounds, each sorted in
// ascending insertion order. The returned slices are backed by scratch buffers
// reused on the next call.
func (bp *broadphase3D) queryStaticMeshesAndHeightfields(bounds Box) ([]*TriMeshCollider, []*HeightfieldCollider) {
	bp.tmOrderHits = bp.tmOrderHits[:0]
	bp.hfOrderHits = bp.hfOrderHits[:0]
	bp.tmHits = bp.tmHits[:0]
	bp.hfHits = bp.hfHits[:0]

	if bp.bruteForce {
		for _, pid := range bp.triMeshProxies {
			if tm := bp.proxies[pid].triMesh; tm != nil {
				bp.tmHits = append(bp.tmHits, tm)
			}
		}
		for _, pid := range bp.hfProxies {
			if hf := bp.proxies[pid].heightfield; hf != nil {
				bp.hfHits = append(bp.hfHits, hf)
			}
		}
		return bp.tmHits, bp.hfHits
	}

	bp.ensureStaticTree()
	qb := aabbBoundsFromBox(bounds, 0)
	bp.staticTree.queryAABB(qb, func(pid int32) bool {
		p := &bp.proxies[pid]
		switch p.kind {
		case bpKindTriMesh:
			bp.tmOrderHits = append(bp.tmOrderHits, bpOrderedProxy{order: p.order, pid: pid})
		case bpKindHeightfield:
			bp.hfOrderHits = append(bp.hfOrderHits, bpOrderedProxy{order: p.order, pid: pid})
		}
		return true
	})

	if len(bp.tmOrderHits) > 1 {
		slices.SortFunc(bp.tmOrderHits, func(a, b bpOrderedProxy) int {
			return cmp.Compare(a.order, b.order)
		})
	}
	for _, h := range bp.tmOrderHits {
		bp.tmHits = append(bp.tmHits, bp.proxies[h.pid].triMesh)
	}

	if len(bp.hfOrderHits) > 1 {
		slices.SortFunc(bp.hfOrderHits, func(a, b bpOrderedProxy) int {
			return cmp.Compare(a.order, b.order)
		})
	}
	for _, h := range bp.hfOrderHits {
		bp.hfHits = append(bp.hfHits, bp.proxies[h.pid].heightfield)
	}

	return bp.tmHits, bp.hfHits
}

func (bp *broadphase3D) queryAABB(bounds Box, visit func(p *bpProxy) bool) {
	if bp.bruteForce {
		for _, pid := range bp.bodyProxies {
			if !visit(&bp.proxies[pid]) {
				return
			}
		}
		for _, pid := range bp.triMeshProxies {
			if tm := bp.proxies[pid].triMesh; tm != nil && len(tm.nodes) > 0 {
				if !visit(&bp.proxies[pid]) {
					return
				}
			}
		}
		for _, pid := range bp.hfProxies {
			if hf := bp.proxies[pid].heightfield; hf != nil && hf.valid() {
				if !visit(&bp.proxies[pid]) {
					return
				}
			}
		}
		return
	}
	bp.ensureStaticTree()
	qb := aabbBoundsFromBox(bounds, 0)
	keep := true
	bp.staticTree.queryAABB(qb, func(pid int32) bool {
		keep = visit(&bp.proxies[pid])
		return keep
	})
	if !keep {
		return
	}
	bp.dynamicTree.queryAABB(qb, func(pid int32) bool {
		return visit(&bp.proxies[pid])
	})
}

func (bp *broadphase3D) queryDynamicAABB(bounds Box, visit func(p *bpProxy) bool) {
	if bp.bruteForce {
		for _, pid := range bp.dynamicList {
			if !visit(&bp.proxies[pid]) {
				return
			}
		}
		return
	}
	qb := aabbBoundsFromBox(bounds, 0)
	bp.dynamicTree.queryAABB(qb, func(pid int32) bool {
		return visit(&bp.proxies[pid])
	})
}

func (bp *broadphase3D) raycastOrdered(r *raySlab, maxDist float32, visit func(p *bpProxy, curMaxDist float32) (float32, bool)) float32 {
	if bp.bruteForce {
		for _, pid := range bp.bodyProxies {
			var keep bool
			maxDist, keep = visit(&bp.proxies[pid], maxDist)
			if !keep {
				return maxDist
			}
		}
		for _, pid := range bp.triMeshProxies {
			if tm := bp.proxies[pid].triMesh; tm == nil || len(tm.nodes) == 0 {
				continue
			}
			var keep bool
			maxDist, keep = visit(&bp.proxies[pid], maxDist)
			if !keep {
				return maxDist
			}
		}
		for _, pid := range bp.hfProxies {
			if hf := bp.proxies[pid].heightfield; hf == nil || !hf.valid() {
				continue
			}
			var keep bool
			maxDist, keep = visit(&bp.proxies[pid], maxDist)
			if !keep {
				return maxDist
			}
		}
		return maxDist
	}
	bp.ensureStaticTree()
	keep := true
	maxDist = bp.staticTree.raycastOrdered(r, maxDist, func(pid int32, curMax float32) (float32, bool) {
		var nextMax float32
		nextMax, keep = visit(&bp.proxies[pid], curMax)
		return nextMax, keep
	})
	if !keep {
		return maxDist
	}
	return bp.dynamicTree.raycastOrdered(r, maxDist, func(pid int32, curMax float32) (float32, bool) {
		return visit(&bp.proxies[pid], curMax)
	})
}
