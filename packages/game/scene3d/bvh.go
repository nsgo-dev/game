package scene3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

const bvhNull int32 = -1

// BVHHandle uniquely identifies an active leaf entry in a [BVH].
// The zero value is invalid.
type BVHHandle struct {
	index uint32 // 1-based index into BVH.leaves; 0 means invalid
	gen   uint32 // generation counter to reject stale handles
}

// Valid reports whether h is a non-zero handle.
func (h BVHHandle) Valid() bool {
	return h.index != 0
}

// BVHRayHit describes a ray intersection with a BVH leaf's tight AABB.
type BVHRayHit struct {
	Handle   BVHHandle
	ID       uint64
	UserData any
	Distance float32 // Ray parameter t in [0, maxDist] such that hitPoint == ray.At(Distance)
}

// bvhNode is a 48-byte, pointer-free tree node (GC noscan) stored in a flat slice.
type bvhNode struct {
	fatMin  math3d.Vec3 // 12B
	left    int32       // 4B  (bvhNull for leaf)
	fatMax  math3d.Vec3 // 12B
	right   int32       // 4B  (bvhNull for leaf)
	parent  int32       // 4B  (bvhNull for root; next free index when freed)
	height  int32       // 4B  (0 for leaf, -1 when freed)
	leafIdx int32       // 4B  (0-based index into leaves, bvhNull for internal)
	_       int32       // 4B  padding
}

type bvhLeaf struct {
	tight    math3d.AABB
	id       uint64
	userData any
	nodeIdx  int32 // index into nodes, or next free leaf index when freed
	gen      uint32
	alive    bool
}

// BVH is a dynamic 3D AABB Bounding Volume Hierarchy.
//
// It maintains a balanced fat-AABB tree with O(log N) incremental Insert,
// Update, and Remove operations, an optional top-down binned SAH Rebuild for
// optimal cache-ordered query performance, and zero-allocation slice-reusing
// frustum, AABB, sphere, and raycast queries.
//
// Every leaf stores both a fattened AABB (for O(1) small-motion updates and
// subtree pruning) and its exact tight AABB (tested at leaves so queries return
// the exact same hit set as a brute-force linear scan).
type BVH struct {
	nodes            []bvhNode
	leaves           []bvhLeaf
	root             int32
	freeNode         int32
	freeLeaf         int32
	count            int
	lastQueryVisited int
}

// NewBVH returns an empty dynamic BVH ready for insertions and queries.
func NewBVH() *BVH {
	return &BVH{
		root:     bvhNull,
		freeNode: bvhNull,
		freeLeaf: bvhNull,
	}
}

func (b *BVH) initIfZero() {
	if len(b.nodes) == 0 && len(b.leaves) == 0 && b.count == 0 && b.root == 0 {
		b.root = bvhNull
		b.freeNode = bvhNull
		b.freeLeaf = bvhNull
	}
}

// Len returns the number of active entries currently stored in b.
func (b *BVH) Len() int {
	if b == nil {
		return 0
	}
	return b.count
}

// Height returns the height of the BVH tree (0 for empty or single-leaf tree).
func (b *BVH) Height() int {
	if b == nil || b.count == 0 || b.root == bvhNull {
		return 0
	}
	return int(b.nodes[b.root].height)
}

// LastQueryVisited returns the number of BVH tree nodes visited during the
// most recent query (QueryFrustum, QueryAABB, QuerySphere, Raycast, or RaycastAll).
func (b *BVH) LastQueryVisited() int {
	if b == nil {
		return 0
	}
	return b.lastQueryVisited
}

func (b *BVH) lookupLeaf(h BVHHandle) (*bvhLeaf, int32) {
	if b == nil || h.index == 0 {
		return nil, bvhNull
	}
	idx := int32(h.index - 1)
	if int(idx) >= len(b.leaves) {
		return nil, bvhNull
	}
	leaf := &b.leaves[idx]
	if !leaf.alive || leaf.gen != h.gen {
		return nil, bvhNull
	}
	return leaf, idx
}

// Contains reports whether h refers to a live entry in b.
func (b *BVH) Contains(h BVHHandle) bool {
	leaf, _ := b.lookupLeaf(h)
	return leaf != nil
}

// ID returns the caller-provided uint64 identifier associated with h.
func (b *BVH) ID(h BVHHandle) (uint64, bool) {
	leaf, _ := b.lookupLeaf(h)
	if leaf == nil {
		return 0, false
	}
	return leaf.id, true
}

// UserData returns the caller-provided userData payload associated with h.
func (b *BVH) UserData(h BVHHandle) (any, bool) {
	leaf, _ := b.lookupLeaf(h)
	if leaf == nil {
		return nil, false
	}
	return leaf.userData, true
}

// Bounds returns the tight world-space AABB associated with h.
func (b *BVH) Bounds(h BVHHandle) (math3d.AABB, bool) {
	leaf, _ := b.lookupLeaf(h)
	if leaf == nil {
		return math3d.AABBEmpty(), false
	}
	return leaf.tight, true
}

func (b *BVH) allocNode() int32 {
	if b.freeNode != bvhNull {
		idx := b.freeNode
		b.freeNode = b.nodes[idx].parent
		b.nodes[idx] = bvhNode{
			left:    bvhNull,
			right:   bvhNull,
			parent:  bvhNull,
			height:  0,
			leafIdx: bvhNull,
		}
		return idx
	}
	idx := int32(len(b.nodes))
	b.nodes = append(b.nodes, bvhNode{
		left:    bvhNull,
		right:   bvhNull,
		parent:  bvhNull,
		height:  0,
		leafIdx: bvhNull,
	})
	return idx
}

func (b *BVH) freeNodeIdx(idx int32) {
	b.nodes[idx] = bvhNode{
		left:    bvhNull,
		right:   bvhNull,
		parent:  b.freeNode,
		height:  -1,
		leafIdx: bvhNull,
	}
	b.freeNode = idx
}

func (b *BVH) allocLeaf() int32 {
	if b.freeLeaf != bvhNull {
		idx := b.freeLeaf
		b.freeLeaf = b.leaves[idx].nodeIdx
		b.leaves[idx].nodeIdx = bvhNull
		b.leaves[idx].alive = true
		return idx
	}
	idx := int32(len(b.leaves))
	b.leaves = append(b.leaves, bvhLeaf{
		nodeIdx: bvhNull,
		gen:     1,
		alive:   true,
	})
	return idx
}

func (b *BVH) freeLeafIdx(idx int32) {
	leaf := &b.leaves[idx]
	leaf.alive = false
	leaf.userData = nil // release reference for GC
	leaf.gen++
	if leaf.gen == 0 {
		leaf.gen = 1
	}
	leaf.nodeIdx = b.freeLeaf
	b.freeLeaf = idx
}

// Insert adds an entry with the given id, tight bounds, and arbitrary userData
// into b and returns its handle. If bounds is empty, Insert returns an invalid
// zero BVHHandle without modifying b.
func (b *BVH) Insert(id uint64, bounds math3d.AABB, userData any) BVHHandle {
	if b == nil || bounds.Empty() || !bounds.IsFinite() {
		return BVHHandle{}
	}
	b.initIfZero()

	leafIdx := b.allocLeaf()
	nodeIdx := b.allocNode()

	fatMin, fatMax := fattenAABB(bounds, math3d.Vec3{})
	b.nodes[nodeIdx].fatMin = fatMin
	b.nodes[nodeIdx].fatMax = fatMax
	b.nodes[nodeIdx].height = 0
	b.nodes[nodeIdx].leafIdx = leafIdx

	leaf := &b.leaves[leafIdx]
	leaf.tight = bounds
	leaf.id = id
	leaf.userData = userData
	leaf.nodeIdx = nodeIdx

	b.insertLeaf(nodeIdx)
	b.count++

	return BVHHandle{
		index: uint32(leafIdx + 1),
		gen:   leaf.gen,
	}
}

// Update updates the tight bounds for handle h. If newBounds still fits inside
// the leaf's existing fat AABB (and is not excessively shrunk), Update completes
// in O(1) without modifying the tree structure; otherwise the leaf is reinserted
// with motion-predicted fat bounds in O(log N).
//
// Returns true if h was valid and updated, or false if h is invalid/removed or
// newBounds is empty.
func (b *BVH) Update(h BVHHandle, newBounds math3d.AABB) bool {
	if b == nil || newBounds.Empty() || !newBounds.IsFinite() {
		return false
	}
	leaf, _ := b.lookupLeaf(h)
	if leaf == nil {
		return false
	}
	nodeIdx := leaf.nodeIdx
	node := &b.nodes[nodeIdx]

	disp := newBounds.Center().Sub(leaf.tight.Center())
	leaf.tight = newBounds

	// Fast path: new tight bounds still fit inside existing fat AABB.
	if aabbContains(node.fatMin, node.fatMax, newBounds.Min, newBounds.Max) {
		// Re-tighten only if the fat box has become excessively oversized
		// (e.g., object shrank dramatically or stopped after fast motion).
		fatDx := node.fatMax.X - node.fatMin.X
		fatDy := node.fatMax.Y - node.fatMin.Y
		fatDz := node.fatMax.Z - node.fatMin.Z
		curDx := (newBounds.Max.X - newBounds.Min.X) + 0.1
		curDy := (newBounds.Max.Y - newBounds.Min.Y) + 0.1
		curDz := (newBounds.Max.Z - newBounds.Min.Z) + 0.1
		if fatDx <= 4*curDx && fatDy <= 4*curDy && fatDz <= 4*curDz {
			return true
		}
	}

	b.removeLeaf(nodeIdx)
	fatMin, fatMax := fattenAABB(newBounds, disp)
	b.nodes[nodeIdx].fatMin = fatMin
	b.nodes[nodeIdx].fatMax = fatMax
	b.nodes[nodeIdx].height = 0
	b.insertLeaf(nodeIdx)
	return true
}

// Remove deletes the entry identified by h from b. Removing an invalid or
// already-removed handle is a safe no-op.
func (b *BVH) Remove(h BVHHandle) {
	if b == nil || b.count == 0 {
		return
	}
	leaf, leafIdx := b.lookupLeaf(h)
	if leaf == nil {
		return
	}
	nodeIdx := leaf.nodeIdx
	b.removeLeaf(nodeIdx)
	b.freeNodeIdx(nodeIdx)
	b.freeLeafIdx(leafIdx)
	b.count--
}

func (b *BVH) insertLeaf(leafNode int32) {
	if b.root == bvhNull {
		b.root = leafNode
		b.nodes[leafNode].parent = bvhNull
		return
	}

	leafMin := b.nodes[leafNode].fatMin
	leafMax := b.nodes[leafNode].fatMax

	// Greedy SAH descent to pick the best sibling node.
	index := b.root
	for b.nodes[index].left != bvhNull {
		cur := &b.nodes[index]
		left := cur.left
		right := cur.right

		area := aabbHalfArea(cur.fatMin, cur.fatMax)
		combMin, combMax := aabbUnion(cur.fatMin, cur.fatMax, leafMin, leafMax)
		combinedArea := aabbHalfArea(combMin, combMax)

		// Cost of creating a new parent here for cur and leafNode.
		cost := 2.0 * combinedArea
		// Minimum cost of pushing leafNode further down the tree.
		inheritanceCost := 2.0 * (combinedArea - area)

		// Cost of descending into left child.
		leftNode := &b.nodes[left]
		lMin, lMax := aabbUnion(leafMin, leafMax, leftNode.fatMin, leftNode.fatMax)
		var costLeft float32
		if leftNode.left == bvhNull {
			costLeft = aabbHalfArea(lMin, lMax) + inheritanceCost
		} else {
			costLeft = (aabbHalfArea(lMin, lMax) - aabbHalfArea(leftNode.fatMin, leftNode.fatMax)) + inheritanceCost
		}

		// Cost of descending into right child.
		rightNode := &b.nodes[right]
		rMin, rMax := aabbUnion(leafMin, leafMax, rightNode.fatMin, rightNode.fatMax)
		var costRight float32
		if rightNode.left == bvhNull {
			costRight = aabbHalfArea(rMin, rMax) + inheritanceCost
		} else {
			costRight = (aabbHalfArea(rMin, rMax) - aabbHalfArea(rightNode.fatMin, rightNode.fatMax)) + inheritanceCost
		}

		if cost < costLeft && cost < costRight {
			break
		}
		if costLeft < costRight {
			index = left
		} else {
			index = right
		}
	}

	sibling := index
	oldParent := b.nodes[sibling].parent
	newParent := b.allocNode()

	b.nodes[newParent].parent = oldParent
	b.nodes[newParent].fatMin, b.nodes[newParent].fatMax = aabbUnion(
		leafMin, leafMax,
		b.nodes[sibling].fatMin, b.nodes[sibling].fatMax,
	)
	b.nodes[newParent].height = b.nodes[sibling].height + 1
	b.nodes[newParent].left = sibling
	b.nodes[newParent].right = leafNode

	b.nodes[sibling].parent = newParent
	b.nodes[leafNode].parent = newParent

	if oldParent != bvhNull {
		if b.nodes[oldParent].left == sibling {
			b.nodes[oldParent].left = newParent
		} else {
			b.nodes[oldParent].right = newParent
		}
	} else {
		b.root = newParent
	}

	b.refitAndBalance(newParent)
}

func (b *BVH) removeLeaf(leafNode int32) {
	if leafNode == b.root {
		b.root = bvhNull
		b.nodes[leafNode].parent = bvhNull
		return
	}

	parent := b.nodes[leafNode].parent
	grandParent := b.nodes[parent].parent
	var sibling int32
	if b.nodes[parent].left == leafNode {
		sibling = b.nodes[parent].right
	} else {
		sibling = b.nodes[parent].left
	}

	if grandParent != bvhNull {
		if b.nodes[grandParent].left == parent {
			b.nodes[grandParent].left = sibling
		} else {
			b.nodes[grandParent].right = sibling
		}
		b.nodes[sibling].parent = grandParent
		b.freeNodeIdx(parent)
		b.refitAndBalance(grandParent)
	} else {
		b.root = sibling
		b.nodes[sibling].parent = bvhNull
		b.freeNodeIdx(parent)
	}
	b.nodes[leafNode].parent = bvhNull
}

func (b *BVH) refitAndBalance(index int32) {
	for index != bvhNull {
		index = b.balance(index)
		left := b.nodes[index].left
		right := b.nodes[index].right

		l := &b.nodes[left]
		r := &b.nodes[right]
		cur := &b.nodes[index]
		cur.height = 1 + max(l.height, r.height)
		cur.fatMin, cur.fatMax = aabbUnion(l.fatMin, l.fatMax, r.fatMin, r.fatMax)

		index = cur.parent
	}
}

// balance performs a single AVL-style tree rotation at iA if its children's
// heights differ by more than 1, returning the new subtree root index.
func (b *BVH) balance(iA int32) int32 {
	nodes := b.nodes
	a := &nodes[iA]
	if a.left == bvhNull || a.height < 2 {
		return iA
	}

	iB := a.left
	iC := a.right
	nB := &nodes[iB]
	nC := &nodes[iC]

	bal := nC.height - nB.height

	// Rotate C up.
	if bal > 1 {
		iF := nC.left
		iG := nC.right
		nF := &nodes[iF]
		nG := &nodes[iG]

		nC.left = iA
		nC.parent = a.parent
		a.parent = iC

		if nC.parent != bvhNull {
			if nodes[nC.parent].left == iA {
				nodes[nC.parent].left = iC
			} else {
				nodes[nC.parent].right = iC
			}
		} else {
			b.root = iC
		}

		if nF.height > nG.height {
			nC.right = iF
			a.right = iG
			nG.parent = iA
			a.fatMin, a.fatMax = aabbUnion(nB.fatMin, nB.fatMax, nG.fatMin, nG.fatMax)
			nC.fatMin, nC.fatMax = aabbUnion(a.fatMin, a.fatMax, nF.fatMin, nF.fatMax)
			a.height = 1 + max(nB.height, nG.height)
			nC.height = 1 + max(a.height, nF.height)
		} else {
			nC.right = iG
			a.right = iF
			nF.parent = iA
			a.fatMin, a.fatMax = aabbUnion(nB.fatMin, nB.fatMax, nF.fatMin, nF.fatMax)
			nC.fatMin, nC.fatMax = aabbUnion(a.fatMin, a.fatMax, nG.fatMin, nG.fatMax)
			a.height = 1 + max(nB.height, nF.height)
			nC.height = 1 + max(a.height, nG.height)
		}
		return iC
	}

	// Rotate B up.
	if bal < -1 {
		iD := nB.left
		iE := nB.right
		nD := &nodes[iD]
		nE := &nodes[iE]

		nB.left = iA
		nB.parent = a.parent
		a.parent = iB

		if nB.parent != bvhNull {
			if nodes[nB.parent].left == iA {
				nodes[nB.parent].left = iB
			} else {
				nodes[nB.parent].right = iB
			}
		} else {
			b.root = iB
		}

		if nD.height > nE.height {
			nB.right = iD
			a.left = iE
			nE.parent = iA
			a.fatMin, a.fatMax = aabbUnion(nC.fatMin, nC.fatMax, nE.fatMin, nE.fatMax)
			nB.fatMin, nB.fatMax = aabbUnion(a.fatMin, a.fatMax, nD.fatMin, nD.fatMax)
			a.height = 1 + max(nC.height, nE.height)
			nB.height = 1 + max(a.height, nD.height)
		} else {
			nB.right = iE
			a.left = iD
			nD.parent = iA
			a.fatMin, a.fatMax = aabbUnion(nC.fatMin, nC.fatMax, nD.fatMin, nD.fatMax)
			nB.fatMin, nB.fatMax = aabbUnion(a.fatMin, a.fatMax, nE.fatMin, nE.fatMax)
			a.height = 1 + max(nC.height, nD.height)
			nB.height = 1 + max(a.height, nE.height)
		}
		return iB
	}

	return iA
}

type bvhBuildItem struct {
	leafIdx  int32
	fatMin   math3d.Vec3
	fatMax   math3d.Vec3
	centroid math3d.Vec3
}

type sahBin struct {
	minV  math3d.Vec3
	maxV  math3d.Vec3
	count int
}

const sahNumBins = 12

// Rebuild reconstructs the BVH top-down using a 12-bucket Surface Area
// Heuristic (SAH) across all three axes, laying nodes out in cache-friendly
// depth-first pre-order. All existing BVHHandles remain valid.
func (b *BVH) Rebuild() {
	if b == nil {
		return
	}
	b.initIfZero()
	if b.count == 0 {
		b.nodes = b.nodes[:0]
		b.root = bvhNull
		b.freeNode = bvhNull
		return
	}

	items := make([]bvhBuildItem, 0, b.count)
	for i := range b.leaves {
		leaf := &b.leaves[i]
		if !leaf.alive {
			continue
		}
		fatMin, fatMax := fattenAABB(leaf.tight, math3d.Vec3{})
		items = append(items, bvhBuildItem{
			leafIdx:  int32(i),
			fatMin:   fatMin,
			fatMax:   fatMax,
			centroid: fatMin.Add(fatMax).Scale(0.5),
		})
	}

	needNodes := 2*len(items) - 1
	if cap(b.nodes) < needNodes {
		b.nodes = make([]bvhNode, 0, needNodes)
	} else {
		b.nodes = b.nodes[:0]
	}
	b.freeNode = bvhNull
	b.root = b.buildSAH(items, bvhNull)
}

func (b *BVH) buildSAH(items []bvhBuildItem, parent int32) int32 {
	nodeIdx := int32(len(b.nodes))
	b.nodes = append(b.nodes, bvhNode{
		left:    bvhNull,
		right:   bvhNull,
		parent:  parent,
		height:  0,
		leafIdx: bvhNull,
	})

	n := len(items)
	if n == 1 {
		it := &items[0]
		b.nodes[nodeIdx].fatMin = it.fatMin
		b.nodes[nodeIdx].fatMax = it.fatMax
		b.nodes[nodeIdx].leafIdx = it.leafIdx
		b.leaves[it.leafIdx].nodeIdx = nodeIdx
		return nodeIdx
	}
	if n == 2 {
		leftIdx := b.buildSAH(items[:1], nodeIdx)
		rightIdx := b.buildSAH(items[1:], nodeIdx)
		l := &b.nodes[leftIdx]
		r := &b.nodes[rightIdx]
		cur := &b.nodes[nodeIdx]
		cur.left = leftIdx
		cur.right = rightIdx
		cur.fatMin, cur.fatMax = aabbUnion(l.fatMin, l.fatMax, r.fatMin, r.fatMax)
		cur.height = 1
		return nodeIdx
	}

	// Compute centroid bounding box.
	cMin := items[0].centroid
	cMax := cMin
	for i := 1; i < n; i++ {
		c := items[i].centroid
		cMin = cMin.Min(c)
		cMax = cMax.Max(c)
	}

	bestAxis := -1
	bestSplit := 0
	bestCost := float32(math.Inf(1))
	var bestCMin, bestScale float32

	cMinArr := [3]float32{cMin.X, cMin.Y, cMin.Z}
	cMaxArr := [3]float32{cMax.X, cMax.Y, cMax.Z}

	posInf := float32(math.Inf(1))
	negInf := float32(math.Inf(-1))
	emptyMin := math3d.Vec3{X: posInf, Y: posInf, Z: posInf}
	emptyMax := math3d.Vec3{X: negInf, Y: negInf, Z: negInf}

	var bins [sahNumBins]sahBin
	var leftAreas [sahNumBins - 1]float32
	var leftCounts [sahNumBins - 1]int

	for axis := range 3 {
		ext := cMaxArr[axis] - cMinArr[axis]
		if ext <= 1e-6 {
			continue
		}
		for i := range sahNumBins {
			bins[i] = sahBin{minV: emptyMin, maxV: emptyMax}
		}
		scale := float32(sahNumBins) / ext
		axisMin := cMinArr[axis]

		for i := range items {
			it := &items[i]
			var coord float32
			switch axis {
			case 0:
				coord = it.centroid.X
			case 1:
				coord = it.centroid.Y
			default:
				coord = it.centroid.Z
			}
			bi := int((coord - axisMin) * scale)
			if bi < 0 {
				bi = 0
			} else if bi >= sahNumBins {
				bi = sahNumBins - 1
			}
			bin := &bins[bi]
			bin.count++
			bin.minV = bin.minV.Min(it.fatMin)
			bin.maxV = bin.maxV.Max(it.fatMax)
		}

		// Left-to-right prefix sweep.
		lMin, lMax := emptyMin, emptyMax
		lCount := 0
		for i := range sahNumBins - 1 {
			if bins[i].count > 0 {
				lCount += bins[i].count
				lMin = lMin.Min(bins[i].minV)
				lMax = lMax.Max(bins[i].maxV)
			}
			leftCounts[i] = lCount
			if lCount > 0 {
				leftAreas[i] = aabbHalfArea(lMin, lMax)
			} else {
				leftAreas[i] = 0
			}
		}

		// Right-to-left suffix sweep.
		rMin, rMax := emptyMin, emptyMax
		rCount := 0
		for i := sahNumBins - 1; i >= 1; i-- {
			if bins[i].count > 0 {
				rCount += bins[i].count
				rMin = rMin.Min(bins[i].minV)
				rMax = rMax.Max(bins[i].maxV)
			}
			lc := leftCounts[i-1]
			if lc == 0 || rCount == 0 {
				continue
			}
			cost := float32(lc)*leftAreas[i-1] + float32(rCount)*aabbHalfArea(rMin, rMax)
			if cost < bestCost {
				bestCost = cost
				bestAxis = axis
				bestSplit = i
				bestCMin = axisMin
				bestScale = scale
			}
		}
	}

	mid := n / 2
	if bestAxis >= 0 {
		// In-place Hoare partition by best bin split.
		i, j := 0, n-1
		for i <= j {
			var coord float32
			switch bestAxis {
			case 0:
				coord = items[i].centroid.X
			case 1:
				coord = items[i].centroid.Y
			default:
				coord = items[i].centroid.Z
			}
			bi := int((coord - bestCMin) * bestScale)
			if bi < 0 {
				bi = 0
			} else if bi >= sahNumBins {
				bi = sahNumBins - 1
			}
			if bi < bestSplit {
				i++
			} else {
				items[i], items[j] = items[j], items[i]
				j--
			}
		}
		if i > 0 && i < n {
			mid = i
		}
	}

	leftIdx := b.buildSAH(items[:mid], nodeIdx)
	rightIdx := b.buildSAH(items[mid:], nodeIdx)
	l := &b.nodes[leftIdx]
	r := &b.nodes[rightIdx]
	cur := &b.nodes[nodeIdx]
	cur.left = leftIdx
	cur.right = rightIdx
	cur.fatMin, cur.fatMax = aabbUnion(l.fatMin, l.fatMax, r.fatMin, r.fatMax)
	cur.height = 1 + max(l.height, r.height)
	return nodeIdx
}

const (
	frustumOutside   = 0
	frustumIntersect = 1
	frustumInside    = 2
)

func classifyAABBFrustum(f *math3d.Frustum, minV, maxV math3d.Vec3) int {
	result := frustumInside
	for i := range 6 {
		p := &f.Planes[i]
		nx, ny, nz, d := p.Normal.X, p.Normal.Y, p.Normal.Z, p.D

		var px, py, pz float32
		var qx, qy, qz float32
		if nx >= 0 {
			px, qx = maxV.X, minV.X
		} else {
			px, qx = minV.X, maxV.X
		}
		if ny >= 0 {
			py, qy = maxV.Y, minV.Y
		} else {
			py, qy = minV.Y, maxV.Y
		}
		if nz >= 0 {
			pz, qz = maxV.Z, minV.Z
		} else {
			pz, qz = minV.Z, maxV.Z
		}

		if nx*px+ny*py+nz*pz+d < 0 {
			return frustumOutside
		}
		if result == frustumInside && nx*qx+ny*qy+nz*qz+d < 0 {
			result = frustumIntersect
		}
	}
	return result
}

// QueryFrustum appends handles of all entries whose tight AABB intersects
// frustum to out and returns the resulting slice. Subtrees completely outside
// frustum are pruned immediately, and subtrees completely inside frustum are
// collected without redundant per-leaf plane tests.
//
// Pass a reused slice (e.g. buf[:0]) for zero-allocation queries.
func (b *BVH) QueryFrustum(frustum math3d.Frustum, out []BVHHandle) []BVHHandle {
	if b == nil || b.count == 0 || b.root == bvhNull {
		if b != nil {
			b.lastQueryVisited = 0
		}
		return out
	}

	nodes := b.nodes
	leaves := b.leaves
	visited := 0

	// Bit 0 of encoded entry marks a subtree already proven completely inside
	// the frustum so descendants skip plane checks.
	var stack [128]int32
	var overflow []int32
	sp := 1
	stack[0] = b.root << 1

	for sp > 0 || len(overflow) > 0 {
		var entry int32
		if len(overflow) > 0 {
			last := len(overflow) - 1
			entry = overflow[last]
			overflow = overflow[:last]
		} else {
			sp--
			entry = stack[sp]
		}

		idx := entry >> 1
		fullyInside := (entry & 1) != 0
		visited++
		node := &nodes[idx]

		if fullyInside {
			if node.left == bvhNull {
				leafIdx := node.leafIdx
				out = append(out, BVHHandle{index: uint32(leafIdx + 1), gen: leaves[leafIdx].gen})
				continue
			}
			rEntry := (node.right << 1) | 1
			lEntry := (node.left << 1) | 1
			if sp+2 <= len(stack) {
				stack[sp] = rEntry
				stack[sp+1] = lEntry
				sp += 2
			} else {
				overflow = append(overflow, rEntry, lEntry)
			}
			continue
		}

		cls := classifyAABBFrustum(&frustum, node.fatMin, node.fatMax)
		if cls == frustumOutside {
			continue
		}
		if node.left == bvhNull {
			leafIdx := node.leafIdx
			leaf := &leaves[leafIdx]
			if cls == frustumInside || frustum.IntersectsAABB(leaf.tight) {
				out = append(out, BVHHandle{index: uint32(leafIdx + 1), gen: leaf.gen})
			}
			continue
		}

		insideBit := int32(0)
		if cls == frustumInside {
			insideBit = 1
		}
		rEntry := (node.right << 1) | insideBit
		lEntry := (node.left << 1) | insideBit
		if sp+2 <= len(stack) {
			stack[sp] = rEntry
			stack[sp+1] = lEntry
			sp += 2
		} else {
			overflow = append(overflow, rEntry, lEntry)
		}
	}

	b.lastQueryVisited = visited
	return out
}

// QueryAABB appends handles of all entries whose tight AABB overlaps or touches
// region to out and returns the resulting slice.
//
// Pass a reused slice (e.g. buf[:0]) for zero-allocation queries.
func (b *BVH) QueryAABB(region math3d.AABB, out []BVHHandle) []BVHHandle {
	if b == nil || b.count == 0 || b.root == bvhNull || region.Empty() || !region.IsFinite() {
		if b != nil {
			b.lastQueryVisited = 0
		}
		return out
	}

	nodes := b.nodes
	leaves := b.leaves
	rMin, rMax := region.Min, region.Max
	visited := 0

	var stack [128]int32
	var overflow []int32
	sp := 1
	stack[0] = b.root

	for sp > 0 || len(overflow) > 0 {
		var idx int32
		if len(overflow) > 0 {
			last := len(overflow) - 1
			idx = overflow[last]
			overflow = overflow[:last]
		} else {
			sp--
			idx = stack[sp]
		}

		visited++
		node := &nodes[idx]
		if !aabbOverlap(node.fatMin, node.fatMax, rMin, rMax) {
			continue
		}
		if node.left == bvhNull {
			leafIdx := node.leafIdx
			leaf := &leaves[leafIdx]
			if aabbOverlap(leaf.tight.Min, leaf.tight.Max, rMin, rMax) {
				out = append(out, BVHHandle{index: uint32(leafIdx + 1), gen: leaf.gen})
			}
			continue
		}
		if sp+2 <= len(stack) {
			stack[sp] = node.right
			stack[sp+1] = node.left
			sp += 2
		} else {
			overflow = append(overflow, node.right, node.left)
		}
	}

	b.lastQueryVisited = visited
	return out
}

// QuerySphere appends handles of all entries whose tight AABB overlaps or
// touches the sphere (center, radius) to out and returns the resulting slice.
//
// Pass a reused slice (e.g. buf[:0]) for zero-allocation queries.
func (b *BVH) QuerySphere(center math3d.Vec3, radius float32, out []BVHHandle) []BVHHandle {
	if b == nil || b.count == 0 || b.root == bvhNull || radius < 0 {
		if b != nil {
			b.lastQueryVisited = 0
		}
		return out
	}

	nodes := b.nodes
	leaves := b.leaves
	sphere := math3d.Sphere{Center: center, Radius: radius}
	r2 := radius * radius
	visited := 0

	var stack [128]int32
	var overflow []int32
	sp := 1
	stack[0] = b.root

	for sp > 0 || len(overflow) > 0 {
		var idx int32
		if len(overflow) > 0 {
			last := len(overflow) - 1
			idx = overflow[last]
			overflow = overflow[:last]
		} else {
			sp--
			idx = stack[sp]
		}

		visited++
		node := &nodes[idx]
		if !sphereOverlapAABB(center, r2, node.fatMin, node.fatMax) {
			continue
		}
		if node.left == bvhNull {
			leafIdx := node.leafIdx
			leaf := &leaves[leafIdx]
			if sphere.IntersectsAABB(leaf.tight) {
				out = append(out, BVHHandle{index: uint32(leafIdx + 1), gen: leaf.gen})
			}
			continue
		}
		if sp+2 <= len(stack) {
			stack[sp] = node.right
			stack[sp+1] = node.left
			sp += 2
		} else {
			overflow = append(overflow, node.right, node.left)
		}
	}

	b.lastQueryVisited = visited
	return out
}

type fastRay struct {
	ox, oy, oz       float32
	dx, dy, dz       float32
	invX, invY, invZ float32
	zeroDir          bool
}

func makeFastRay(r math3d.Ray) fastRay {
	fr := fastRay{
		ox:      r.Origin.X,
		oy:      r.Origin.Y,
		oz:      r.Origin.Z,
		dx:      r.Dir.X,
		dy:      r.Dir.Y,
		dz:      r.Dir.Z,
		zeroDir: r.Dir == (math3d.Vec3{}),
	}
	if fr.dx != 0 {
		fr.invX = 1 / fr.dx
	}
	if fr.dy != 0 {
		fr.invY = 1 / fr.dy
	}
	if fr.dz != 0 {
		fr.invZ = 1 / fr.dz
	}
	return fr
}

func (r *fastRay) intersectAABB(minV, maxV math3d.Vec3, maxDist float32) (float32, bool) {
	if r.zeroDir {
		if r.ox >= minV.X && r.ox <= maxV.X &&
			r.oy >= minV.Y && r.oy <= maxV.Y &&
			r.oz >= minV.Z && r.oz <= maxV.Z {
			return 0, true
		}
		return 0, false
	}

	t0 := float32(0)
	t1 := maxDist

	if r.dx == 0 {
		if r.ox < minV.X || r.ox > maxV.X {
			return 0, false
		}
	} else {
		near := (minV.X - r.ox) * r.invX
		far := (maxV.X - r.ox) * r.invX
		if near > far {
			near, far = far, near
		}
		if near > t0 {
			t0 = near
		}
		if far < t1 {
			t1 = far
		}
		if t0 > t1 {
			return 0, false
		}
	}

	if r.dy == 0 {
		if r.oy < minV.Y || r.oy > maxV.Y {
			return 0, false
		}
	} else {
		near := (minV.Y - r.oy) * r.invY
		far := (maxV.Y - r.oy) * r.invY
		if near > far {
			near, far = far, near
		}
		if near > t0 {
			t0 = near
		}
		if far < t1 {
			t1 = far
		}
		if t0 > t1 {
			return 0, false
		}
	}

	if r.dz == 0 {
		if r.oz < minV.Z || r.oz > maxV.Z {
			return 0, false
		}
	} else {
		near := (minV.Z - r.oz) * r.invZ
		far := (maxV.Z - r.oz) * r.invZ
		if near > far {
			near, far = far, near
		}
		if near > t0 {
			t0 = near
		}
		if far < t1 {
			t1 = far
		}
		if t0 > t1 {
			return 0, false
		}
	}

	return t0, true
}

type rayStackItem struct {
	idx  int32
	tMin float32
}

// Raycast traverses the BVH in near-to-far child order using slab AABB
// intersection tests and invokes visit for each leaf whose tight AABB is hit at
// parameter hitDist in [0, maxDist]. Traversal stops immediately if visit
// returns false.
func (b *BVH) Raycast(ray math3d.Ray, maxDist float32, visit func(h BVHHandle, hitDist float32) (continueSearch bool)) {
	if visit == nil {
		return
	}
	b.raycastShrink(ray, maxDist, func(h BVHHandle, _ any, hitDist float32) (float32, bool) {
		return maxDist, visit(h, hitDist)
	})
}

// raycastShrink is an internal helper that lets callers (such as
// Scene.RaycastFirst) shrink maxDist dynamically as closer hits are discovered.
func (b *BVH) raycastShrink(ray math3d.Ray, maxDist float32, visit func(h BVHHandle, userData any, hitDist float32) (newMaxDist float32, cont bool)) {
	if b == nil || b.count == 0 || b.root == bvhNull || maxDist < 0 || math.IsNaN(float64(maxDist)) ||
		!ray.Origin.IsFinite() || !ray.Dir.IsFinite() || ray.Dir.LenSq() == 0 {
		if b != nil {
			b.lastQueryVisited = 0
		}
		return
	}

	nodes := b.nodes
	leaves := b.leaves
	fr := makeFastRay(ray)
	visited := 1

	rootT, hit := fr.intersectAABB(nodes[b.root].fatMin, nodes[b.root].fatMax, maxDist)
	if !hit {
		b.lastQueryVisited = visited
		return
	}

	var stack [128]rayStackItem
	var overflow []rayStackItem
	sp := 1
	stack[0] = rayStackItem{idx: b.root, tMin: rootT}

	for sp > 0 || len(overflow) > 0 {
		var item rayStackItem
		if len(overflow) > 0 {
			last := len(overflow) - 1
			item = overflow[last]
			overflow = overflow[:last]
		} else {
			sp--
			item = stack[sp]
		}

		if item.tMin > maxDist {
			continue
		}

		node := &nodes[item.idx]
		if node.left == bvhNull {
			leafIdx := node.leafIdx
			leaf := &leaves[leafIdx]
			tHit, ok := fr.intersectAABB(leaf.tight.Min, leaf.tight.Max, maxDist)
			if !ok {
				continue
			}
			h := BVHHandle{index: uint32(leafIdx + 1), gen: leaf.gen}
			newMax, cont := visit(h, leaf.userData, tHit)
			if newMax < maxDist {
				maxDist = newMax
			}
			if !cont {
				break
			}
			continue
		}

		leftIdx := node.left
		rightIdx := node.right
		visited += 2
		tLeft, hitLeft := fr.intersectAABB(nodes[leftIdx].fatMin, nodes[leftIdx].fatMax, maxDist)
		tRight, hitRight := fr.intersectAABB(nodes[rightIdx].fatMin, nodes[rightIdx].fatMax, maxDist)

		if hitLeft && hitRight {
			// Push farther child first so nearer child is popped first.
			first, second := rayStackItem{idx: leftIdx, tMin: tLeft}, rayStackItem{idx: rightIdx, tMin: tRight}
			if tRight < tLeft {
				first, second = second, first
			}
			if sp+2 <= len(stack) {
				stack[sp] = second
				stack[sp+1] = first
				sp += 2
			} else {
				overflow = append(overflow, second, first)
			}
		} else if hitLeft {
			it := rayStackItem{idx: leftIdx, tMin: tLeft}
			if sp < len(stack) {
				stack[sp] = it
				sp++
			} else {
				overflow = append(overflow, it)
			}
		} else if hitRight {
			it := rayStackItem{idx: rightIdx, tMin: tRight}
			if sp < len(stack) {
				stack[sp] = it
				sp++
			} else {
				overflow = append(overflow, it)
			}
		}
	}

	b.lastQueryVisited = visited
}

// RaycastAll finds all entries whose tight AABB intersects ray within
// [0, maxDist], appends them to out sorted by ascending Distance, and returns
// the resulting slice.
//
// Pass a reused slice (e.g. buf[:0]) for zero-allocation queries.
func (b *BVH) RaycastAll(ray math3d.Ray, maxDist float32, out []BVHRayHit) []BVHRayHit {
	if b == nil || b.count == 0 || b.root == bvhNull || maxDist < 0 || math.IsNaN(float64(maxDist)) {
		if b != nil {
			b.lastQueryVisited = 0
		}
		return out
	}

	nodes := b.nodes
	leaves := b.leaves
	fr := makeFastRay(ray)
	visited := 1

	rootT, hit := fr.intersectAABB(nodes[b.root].fatMin, nodes[b.root].fatMax, maxDist)
	if !hit {
		b.lastQueryVisited = visited
		return out
	}

	start := len(out)
	var stack [128]rayStackItem
	var overflow []rayStackItem
	sp := 1
	stack[0] = rayStackItem{idx: b.root, tMin: rootT}

	for sp > 0 || len(overflow) > 0 {
		var item rayStackItem
		if len(overflow) > 0 {
			last := len(overflow) - 1
			item = overflow[last]
			overflow = overflow[:last]
		} else {
			sp--
			item = stack[sp]
		}

		node := &nodes[item.idx]
		if node.left == bvhNull {
			leafIdx := node.leafIdx
			leaf := &leaves[leafIdx]
			tHit, ok := fr.intersectAABB(leaf.tight.Min, leaf.tight.Max, maxDist)
			if !ok {
				continue
			}
			out = append(out, BVHRayHit{
				Handle:   BVHHandle{index: uint32(leafIdx + 1), gen: leaf.gen},
				ID:       leaf.id,
				UserData: leaf.userData,
				Distance: tHit,
			})
			continue
		}

		leftIdx := node.left
		rightIdx := node.right
		visited += 2
		tLeft, hitLeft := fr.intersectAABB(nodes[leftIdx].fatMin, nodes[leftIdx].fatMax, maxDist)
		tRight, hitRight := fr.intersectAABB(nodes[rightIdx].fatMin, nodes[rightIdx].fatMax, maxDist)

		if hitLeft && hitRight {
			first, second := rayStackItem{idx: leftIdx, tMin: tLeft}, rayStackItem{idx: rightIdx, tMin: tRight}
			if tRight < tLeft {
				first, second = second, first
			}
			if sp+2 <= len(stack) {
				stack[sp] = second
				stack[sp+1] = first
				sp += 2
			} else {
				overflow = append(overflow, second, first)
			}
		} else if hitLeft {
			it := rayStackItem{idx: leftIdx, tMin: tLeft}
			if sp < len(stack) {
				stack[sp] = it
				sp++
			} else {
				overflow = append(overflow, it)
			}
		} else if hitRight {
			it := rayStackItem{idx: rightIdx, tMin: tRight}
			if sp < len(stack) {
				stack[sp] = it
				sp++
			} else {
				overflow = append(overflow, it)
			}
		}
	}

	b.lastQueryVisited = visited

	added := out[start:]
	if len(added) > 1 {
		slices.SortFunc(added, func(a, b BVHRayHit) int {
			if a.Distance < b.Distance {
				return -1
			}
			if a.Distance > b.Distance {
				return 1
			}
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			if a.Handle.index < b.Handle.index {
				return -1
			}
			if a.Handle.index > b.Handle.index {
				return 1
			}
			return 0
		})
	}
	return out
}

func fattenAABB(b math3d.AABB, disp math3d.Vec3) (minV, maxV math3d.Vec3) {
	const baseMargin float32 = 0.05
	const relMargin float32 = 0.05

	px := baseMargin + (b.Max.X-b.Min.X)*relMargin
	py := baseMargin + (b.Max.Y-b.Min.Y)*relMargin
	pz := baseMargin + (b.Max.Z-b.Min.Z)*relMargin

	minV = math3d.Vec3{X: b.Min.X - px, Y: b.Min.Y - py, Z: b.Min.Z - pz}
	maxV = math3d.Vec3{X: b.Max.X + px, Y: b.Max.Y + py, Z: b.Max.Z + pz}

	const dispMult float32 = 2.0
	dx := disp.X * dispMult
	dy := disp.Y * dispMult
	dz := disp.Z * dispMult
	if dx < 0 {
		minV.X += dx
	} else {
		maxV.X += dx
	}
	if dy < 0 {
		minV.Y += dy
	} else {
		maxV.Y += dy
	}
	if dz < 0 {
		minV.Z += dz
	} else {
		maxV.Z += dz
	}
	return minV, maxV
}

func aabbHalfArea(minV, maxV math3d.Vec3) float32 {
	dx := maxV.X - minV.X
	dy := maxV.Y - minV.Y
	dz := maxV.Z - minV.Z
	return dx*dy + dy*dz + dz*dx
}

func aabbUnion(aMin, aMax, bMin, bMax math3d.Vec3) (math3d.Vec3, math3d.Vec3) {
	return math3d.Vec3{
		X: min(aMin.X, bMin.X),
		Y: min(aMin.Y, bMin.Y),
		Z: min(aMin.Z, bMin.Z),
	}, math3d.Vec3{
		X: max(aMax.X, bMax.X),
		Y: max(aMax.Y, bMax.Y),
		Z: max(aMax.Z, bMax.Z),
	}
}

func aabbContains(outerMin, outerMax, innerMin, innerMax math3d.Vec3) bool {
	return outerMin.X <= innerMin.X && outerMin.Y <= innerMin.Y && outerMin.Z <= innerMin.Z &&
		outerMax.X >= innerMax.X && outerMax.Y >= innerMax.Y && outerMax.Z >= innerMax.Z
}

func aabbOverlap(aMin, aMax, bMin, bMax math3d.Vec3) bool {
	return aMin.X <= bMax.X && aMax.X >= bMin.X &&
		aMin.Y <= bMax.Y && aMax.Y >= bMin.Y &&
		aMin.Z <= bMax.Z && aMax.Z >= bMin.Z
}

func sphereOverlapAABB(center math3d.Vec3, radiusSq float32, minV, maxV math3d.Vec3) bool {
	var distSq float32
	if center.X < minV.X {
		d := minV.X - center.X
		distSq += d * d
	} else if center.X > maxV.X {
		d := center.X - maxV.X
		distSq += d * d
	}
	if center.Y < minV.Y {
		d := minV.Y - center.Y
		distSq += d * d
	} else if center.Y > maxV.Y {
		d := center.Y - maxV.Y
		distSq += d * d
	}
	if center.Z < minV.Z {
		d := minV.Z - center.Z
		distSq += d * d
	} else if center.Z > maxV.Z {
		d := center.Z - maxV.Z
		distSq += d * d
	}
	return distSq <= radiusSq
}
