package perf

import "github.com/nsgo-dev/nsgo/core"

const (
	defaultQuadtreeMaxItems = 8
	defaultQuadtreeMaxDepth = 8
)

type qtNode struct {
	bounds   core.Bounds
	children [4]int32
	items    []int32
	depth    int
	divided  bool
}

type qtEntry[ID comparable] struct {
	id      ID
	bounds  core.Bounds
	nodeIdx int32
	pos     int32
	alive   bool
}

// Quadtree is an adaptive 2D spatial index that recursively partitions a world
// rectangle into four quadrants.
//
// Each item is placed in the deepest quadrant that completely contains its
// bounds, so straddling and out-of-world items remain in their parent (or root)
// without duplication. Internal nodes are stored in a flat pooled slice so
// Clear + Insert + Query runs with 0 allocations in steady state.
type Quadtree[ID comparable] struct {
	world    core.Bounds
	maxItems int
	maxDepth int

	nodes     []qtNode
	usedNodes int

	idToIndex   map[ID]int32
	entries     []qtEntry[ID]
	freeEntries []int32
	count       int
}

// NewQuadtree returns a Quadtree covering world. When a leaf exceeds maxItems
// and its depth is below maxDepth, it splits into four equal quadrants.
// Non-positive maxItems or maxDepth default to 8.
func NewQuadtree[ID comparable](world core.Bounds, maxItems, maxDepth int) *Quadtree[ID] {
	if maxItems <= 0 {
		maxItems = defaultQuadtreeMaxItems
	}
	if maxDepth <= 0 {
		maxDepth = defaultQuadtreeMaxDepth
	}
	qt := &Quadtree[ID]{
		world:     world,
		maxItems:  maxItems,
		maxDepth:  maxDepth,
		nodes:     make([]qtNode, 1, 16),
		usedNodes: 1,
		idToIndex: make(map[ID]int32),
	}
	qt.nodes[0] = qtNode{
		bounds:   world,
		children: [4]int32{-1, -1, -1, -1},
		items:    make([]int32, 0, maxItems),
		depth:    0,
	}
	return qt
}

// Len returns the number of items currently stored in the quadtree.
func (qt *Quadtree[ID]) Len() int { return qt.count }

// Clear removes all items and collapses all subdivisions while keeping pooled
// node and slice capacities for zero-allocation reuse.
func (qt *Quadtree[ID]) Clear() {
	for i := 0; i < qt.usedNodes; i++ {
		qt.nodes[i].items = qt.nodes[i].items[:0]
		qt.nodes[i].divided = false
		qt.nodes[i].children = [4]int32{-1, -1, -1, -1}
	}
	qt.nodes[0].bounds = qt.world
	qt.nodes[0].depth = 0
	qt.usedNodes = 1

	clear(qt.idToIndex)
	clear(qt.entries)
	qt.entries = qt.entries[:0]
	qt.freeEntries = qt.freeEntries[:0]
	qt.count = 0
}

// Insert adds id with bounds to the quadtree. If id is already present, its
// bounds are replaced.
func (qt *Quadtree[ID]) Insert(id ID, bounds core.Bounds) {
	if _, exists := qt.idToIndex[id]; exists {
		qt.Remove(id)
	}

	var eIdx int32
	if n := len(qt.freeEntries); n > 0 {
		eIdx = qt.freeEntries[n-1]
		qt.freeEntries = qt.freeEntries[:n-1]
	} else {
		eIdx = int32(len(qt.entries))
		qt.entries = append(qt.entries, qtEntry[ID]{})
	}

	qt.entries[eIdx] = qtEntry[ID]{
		id:      id,
		bounds:  bounds,
		nodeIdx: -1,
		pos:     -1,
		alive:   true,
	}
	qt.idToIndex[id] = eIdx
	qt.count++

	if !validNonEmptyBounds(bounds) {
		return
	}
	qt.insertInto(0, eIdx)
}

// Remove deletes id from the quadtree in O(1) time. Removing an absent id is a
// no-op.
func (qt *Quadtree[ID]) Remove(id ID) {
	eIdx, exists := qt.idToIndex[id]
	if !exists {
		return
	}
	e := &qt.entries[eIdx]
	if e.nodeIdx >= 0 {
		n := &qt.nodes[e.nodeIdx]
		lastIdx := len(n.items) - 1
		pos := int(e.pos)
		if pos != lastIdx {
			moved := n.items[lastIdx]
			n.items[pos] = moved
			qt.entries[moved].pos = e.pos
		}
		n.items = n.items[:lastIdx]
	}
	var zeroID ID
	*e = qtEntry[ID]{id: zeroID, nodeIdx: -1, pos: -1}
	delete(qt.idToIndex, id)
	qt.freeEntries = append(qt.freeEntries, eIdx)
	qt.count--
}

// Update changes the bounds associated with id, inserting it if absent. When
// the updated bounds stay inside the same leaf quadrant, Update runs in O(1).
func (qt *Quadtree[ID]) Update(id ID, bounds core.Bounds) {
	eIdx, exists := qt.idToIndex[id]
	if !exists {
		qt.Insert(id, bounds)
		return
	}
	e := &qt.entries[eIdx]
	if e.nodeIdx >= 0 && validNonEmptyBounds(bounds) &&
		!qt.nodes[e.nodeIdx].divided &&
		containsBounds(qt.nodes[e.nodeIdx].bounds, bounds) {
		e.bounds = bounds
		return
	}
	qt.Remove(id)
	qt.Insert(id, bounds)
}

// Query appends all IDs whose bounds intersect area to out and returns the
// extended slice. Pass out[:0] to reuse a buffer with 0 allocations.
func (qt *Quadtree[ID]) Query(area core.Bounds, out []ID) []ID {
	if qt.count == 0 || !validNonEmptyBounds(area) {
		return out
	}
	return qt.queryNode(0, area, out)
}

// QueryPoint appends all IDs whose bounds contain p to out and returns the
// extended slice.
func (qt *Quadtree[ID]) QueryPoint(p core.Point, out []ID) []ID {
	if qt.count == 0 || !isFinitePoint(p) {
		return out
	}
	nodeIdx := int32(0)
	for {
		n := &qt.nodes[nodeIdx]
		for _, eIdx := range n.items {
			if qt.entries[eIdx].bounds.Contains(p) {
				out = append(out, qt.entries[eIdx].id)
			}
		}
		if !n.divided {
			return out
		}
		next := int32(-1)
		for _, c := range n.children {
			if qt.nodes[c].bounds.Contains(p) {
				next = c
				break
			}
		}
		if next < 0 {
			return out
		}
		nodeIdx = next
	}
}

func (qt *Quadtree[ID]) queryNode(nodeIdx int32, area core.Bounds, out []ID) []ID {
	n := &qt.nodes[nodeIdx]
	for _, eIdx := range n.items {
		if Intersects(qt.entries[eIdx].bounds, area) {
			out = append(out, qt.entries[eIdx].id)
		}
	}
	if n.divided {
		for _, c := range n.children {
			if Intersects(qt.nodes[c].bounds, area) {
				out = qt.queryNode(c, area, out)
			}
		}
	}
	return out
}

func (qt *Quadtree[ID]) insertInto(nodeIdx, eIdx int32) {
	b := qt.entries[eIdx].bounds
	for {
		if qt.nodes[nodeIdx].divided {
			if c := qt.childIndex(nodeIdx, b); c >= 0 {
				nodeIdx = c
				continue
			}
		}
		pos := int32(len(qt.nodes[nodeIdx].items))
		qt.nodes[nodeIdx].items = append(qt.nodes[nodeIdx].items, eIdx)
		qt.entries[eIdx].nodeIdx = nodeIdx
		qt.entries[eIdx].pos = pos

		if !qt.nodes[nodeIdx].divided &&
			len(qt.nodes[nodeIdx].items) > qt.maxItems &&
			qt.nodes[nodeIdx].depth < qt.maxDepth {
			qt.subdivide(nodeIdx)
		}
		return
	}
}

func (qt *Quadtree[ID]) subdivide(nodeIdx int32) {
	b := qt.nodes[nodeIdx].bounds
	hw := b.Size.Width * 0.5
	hh := b.Size.Height * 0.5
	if hw <= 0 || hh <= 0 {
		return
	}
	midX := b.Origin.X + hw
	midY := b.Origin.Y + hh
	rightW := (b.Origin.X + b.Size.Width) - midX
	bottomH := (b.Origin.Y + b.Size.Height) - midY
	if rightW <= 0 || bottomH <= 0 {
		return
	}

	childDepth := qt.nodes[nodeIdx].depth + 1
	nw := qt.allocNode(core.Bounds{Origin: core.Point{X: b.Origin.X, Y: b.Origin.Y}, Size: core.Size{Width: hw, Height: hh}}, childDepth)
	ne := qt.allocNode(core.Bounds{Origin: core.Point{X: midX, Y: b.Origin.Y}, Size: core.Size{Width: rightW, Height: hh}}, childDepth)
	sw := qt.allocNode(core.Bounds{Origin: core.Point{X: b.Origin.X, Y: midY}, Size: core.Size{Width: hw, Height: bottomH}}, childDepth)
	se := qt.allocNode(core.Bounds{Origin: core.Point{X: midX, Y: midY}, Size: core.Size{Width: rightW, Height: bottomH}}, childDepth)

	qt.nodes[nodeIdx].children = [4]int32{nw, ne, sw, se}
	qt.nodes[nodeIdx].divided = true

	items := qt.nodes[nodeIdx].items
	kept := 0
	for i := 0; i < len(items); i++ {
		itemIdx := items[i]
		if c := qt.childIndex(nodeIdx, qt.entries[itemIdx].bounds); c >= 0 {
			cPos := int32(len(qt.nodes[c].items))
			qt.nodes[c].items = append(qt.nodes[c].items, itemIdx)
			qt.entries[itemIdx].nodeIdx = c
			qt.entries[itemIdx].pos = cPos
		} else {
			items[kept] = itemIdx
			qt.entries[itemIdx].pos = int32(kept)
			kept++
		}
	}
	qt.nodes[nodeIdx].items = items[:kept]

	if childDepth < qt.maxDepth {
		for _, c := range [4]int32{nw, ne, sw, se} {
			if len(qt.nodes[c].items) > qt.maxItems {
				qt.subdivide(c)
			}
		}
	}
}

func (qt *Quadtree[ID]) allocNode(bounds core.Bounds, depth int) int32 {
	idx := qt.usedNodes
	qt.usedNodes++
	if idx < len(qt.nodes) {
		n := &qt.nodes[idx]
		n.bounds = bounds
		n.children = [4]int32{-1, -1, -1, -1}
		n.items = n.items[:0]
		n.depth = depth
		n.divided = false
		return int32(idx)
	}
	qt.nodes = append(qt.nodes, qtNode{
		bounds:   bounds,
		children: [4]int32{-1, -1, -1, -1},
		items:    make([]int32, 0, qt.maxItems),
		depth:    depth,
	})
	return int32(idx)
}

// childIndex returns the child node index that completely contains b, or -1 if
// b straddles a split axis or extends outside nodeIdx's bounds.
func (qt *Quadtree[ID]) childIndex(nodeIdx int32, b core.Bounds) int32 {
	for _, c := range qt.nodes[nodeIdx].children {
		if containsBounds(qt.nodes[c].bounds, b) {
			return c
		}
	}
	return -1
}

func containsBounds(outer, inner core.Bounds) bool {
	return inner.Origin.X >= outer.Origin.X &&
		inner.Origin.Y >= outer.Origin.Y &&
		inner.Origin.X+inner.Size.Width <= outer.Origin.X+outer.Size.Width &&
		inner.Origin.Y+inner.Size.Height <= outer.Origin.Y+outer.Size.Height
}
