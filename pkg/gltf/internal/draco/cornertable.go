package draco

// invalidIndex marks a missing corner, vertex or face (kInvalid*Index in the reference).
const invalidIndex int32 = -1

// connectivity is the corner-table view shared by the mesh connectivity (cornerTable) and per-attribute connectivity with seams (attrCornerTable).
type connectivity interface {
	numVertices() int
	numCorners() int
	numFaces() int
	vertex(c int32) int32
	opposite(c int32) int32
	leftMostCorner(v int32) int32
	isOnBoundary(v int32) bool
}

func next(c int32) int32 {
	if c < 0 {
		return invalidIndex
	}
	if (c+1)%3 != 0 {
		return c + 1
	}
	return c - 2
}

func previous(c int32) int32 {
	if c < 0 {
		return invalidIndex
	}
	if c%3 != 0 {
		return c - 1
	}
	return c + 2
}

func swingRight(t connectivity, c int32) int32 { return previous(t.opposite(previous(c))) }
func swingLeft(t connectivity, c int32) int32  { return next(t.opposite(next(c))) }
func rightCorner(t connectivity, c int32) int32 {
	if c < 0 {
		return invalidIndex
	}
	return t.opposite(next(c))
}
func leftCorner(t connectivity, c int32) int32 {
	if c < 0 {
		return invalidIndex
	}
	return t.opposite(previous(c))
}

// cornerTable is draco::CornerTable restricted to what decoding needs.
type cornerTable struct {
	cornerToVertex []int32
	oppositeCorner []int32
	vertexCorners  []int32
}

func newCornerTable(numFaces, numVertices int) *cornerTable {
	t := &cornerTable{
		cornerToVertex: make([]int32, 3*numFaces),
		oppositeCorner: make([]int32, 3*numFaces),
		vertexCorners:  make([]int32, 0, numVertices),
	}
	for i := range t.cornerToVertex {
		t.cornerToVertex[i] = invalidIndex
		t.oppositeCorner[i] = invalidIndex
	}
	return t
}

func (t *cornerTable) numVertices() int { return len(t.vertexCorners) }
func (t *cornerTable) numCorners() int  { return len(t.cornerToVertex) }
func (t *cornerTable) numFaces() int    { return len(t.cornerToVertex) / 3 }

func (t *cornerTable) validCorner(c int32) bool { return c >= 0 && int(c) < len(t.cornerToVertex) }
func (t *cornerTable) validVertex(v int32) bool { return v >= 0 && int(v) < len(t.vertexCorners) }

func (t *cornerTable) vertex(c int32) int32 {
	if !t.validCorner(c) {
		return invalidIndex
	}
	return t.cornerToVertex[c]
}

func (t *cornerTable) opposite(c int32) int32 {
	if !t.validCorner(c) {
		return invalidIndex
	}
	return t.oppositeCorner[c]
}

func (t *cornerTable) leftMostCorner(v int32) int32 {
	if !t.validVertex(v) {
		return invalidIndex
	}
	return t.vertexCorners[v]
}

func (t *cornerTable) isOnBoundary(v int32) bool {
	return swingLeft(t, t.leftMostCorner(v)) == invalidIndex
}

func (t *cornerTable) setOppositeCorners(c0, c1 int32) {
	if t.validCorner(c0) {
		t.oppositeCorner[c0] = c1
	}
	if t.validCorner(c1) {
		t.oppositeCorner[c1] = c0
	}
}

func (t *cornerTable) mapCornerToVertex(c, v int32) {
	if t.validCorner(c) {
		t.cornerToVertex[c] = v
	}
}

func (t *cornerTable) addNewVertex() int32 {
	t.vertexCorners = append(t.vertexCorners, invalidIndex)
	return int32(len(t.vertexCorners) - 1)
}

func (t *cornerTable) setLeftMostCorner(v, c int32) {
	if t.validVertex(v) {
		t.vertexCorners[v] = c
	}
}

func (t *cornerTable) makeVertexIsolated(v int32) { t.setLeftMostCorner(v, invalidIndex) }

// attrCornerTable is draco::MeshAttributeCornerTable: the mesh connectivity split along attribute seams.
type attrCornerTable struct {
	ct                     *cornerTable
	isEdgeOnSeam           []bool
	isVertexOnSeam         []bool
	cornerToVertex         []int32
	vertexToLeftMostCorner []int32
}

func newAttrCornerTable(ct *cornerTable) *attrCornerTable {
	a := &attrCornerTable{
		ct:             ct,
		isEdgeOnSeam:   make([]bool, ct.numCorners()),
		isVertexOnSeam: make([]bool, ct.numVertices()),
		cornerToVertex: make([]int32, ct.numCorners()),
	}
	for i := range a.cornerToVertex {
		a.cornerToVertex[i] = invalidIndex
	}
	return a
}

func (a *attrCornerTable) numVertices() int { return len(a.vertexToLeftMostCorner) }
func (a *attrCornerTable) numCorners() int  { return a.ct.numCorners() }
func (a *attrCornerTable) numFaces() int    { return a.ct.numFaces() }

func (a *attrCornerTable) vertex(c int32) int32 {
	if c < 0 || int(c) >= len(a.cornerToVertex) {
		return invalidIndex
	}
	return a.cornerToVertex[c]
}

func (a *attrCornerTable) opposite(c int32) int32 {
	if c < 0 || int(c) >= len(a.isEdgeOnSeam) || a.isEdgeOnSeam[c] {
		return invalidIndex
	}
	return a.ct.opposite(c)
}

func (a *attrCornerTable) leftMostCorner(v int32) int32 {
	if v < 0 || int(v) >= len(a.vertexToLeftMostCorner) {
		return invalidIndex
	}
	return a.vertexToLeftMostCorner[v]
}

func (a *attrCornerTable) isOnBoundary(v int32) bool {
	c := a.leftMostCorner(v)
	if c == invalidIndex {
		return true
	}
	return swingLeft(a, c) == invalidIndex
}

func (a *attrCornerTable) markVertexOnSeam(v int32) {
	if v >= 0 && int(v) < len(a.isVertexOnSeam) {
		a.isVertexOnSeam[v] = true
	}
}

func (a *attrCornerTable) addSeamEdge(c int32) {
	if c < 0 || int(c) >= len(a.isEdgeOnSeam) {
		return
	}
	a.isEdgeOnSeam[c] = true
	a.markVertexOnSeam(a.ct.vertex(next(c)))
	a.markVertexOnSeam(a.ct.vertex(previous(c)))
	if opp := a.ct.opposite(c); opp != invalidIndex && int(opp) < len(a.isEdgeOnSeam) {
		a.isEdgeOnSeam[opp] = true
		a.markVertexOnSeam(a.ct.vertex(next(opp)))
		a.markVertexOnSeam(a.ct.vertex(previous(opp)))
	}
}

func (a *attrCornerTable) isCornerOnSeam(c int32) bool {
	v := a.ct.vertex(c)
	return v >= 0 && int(v) < len(a.isVertexOnSeam) && a.isVertexOnSeam[v]
}

// recomputeVertices is RecomputeVerticesInternal<false>.
func (a *attrCornerTable) recomputeVertices() bool {
	a.vertexToLeftMostCorner = a.vertexToLeftMostCorner[:0]
	limit := a.ct.numCorners() + 1
	numNew := int32(0)
	for v := int32(0); int(v) < a.ct.numVertices(); v++ {
		c := a.ct.leftMostCorner(v)
		if c == invalidIndex {
			continue
		}
		firstVert := numNew
		numNew++
		firstC := c
		if int(v) < len(a.isVertexOnSeam) && a.isVertexOnSeam[v] {
			actC := swingLeft(a, firstC)
			for steps := 0; actC != invalidIndex; steps++ {
				if steps > limit {
					return false
				}
				firstC = actC
				actC = swingLeft(a, actC)
				if actC == c {
					return false
				}
			}
		}
		if !a.ct.validCorner(firstC) {
			return false
		}
		a.cornerToVertex[firstC] = firstVert
		a.vertexToLeftMostCorner = append(a.vertexToLeftMostCorner, firstC)
		actC := swingRight(a.ct, firstC)
		for steps := 0; actC != invalidIndex && actC != firstC; steps++ {
			if steps > limit || !a.ct.validCorner(actC) {
				return false
			}
			if a.isEdgeOnSeam[next(actC)] {
				firstVert = numNew
				numNew++
				a.vertexToLeftMostCorner = append(a.vertexToLeftMostCorner, actC)
			}
			a.cornerToVertex[actC] = firstVert
			actC = swingRight(a.ct, actC)
		}
	}
	return true
}
