package draco

import (
	"errors"
	"fmt"
)

// This file ports MeshEdgebreakerDecoderImpl and its standard and valence traversal decoders for bitstream 2.2.

const (
	topologyC       = 0
	topologyS       = 1
	topologyL       = 3
	topologyR       = 5
	topologyE       = 7
	topologyInvalid = 9
)

var symbolToTopology = [5]uint32{topologyC, topologyS, topologyL, topologyR, topologyE}

const (
	traversalStandard   = 0
	traversalPredictive = 1
	traversalValence    = 2
)

const (
	meshVertexAttribute = 0
)

const (
	traversalDepthFirst       = 0
	traversalPredictionDegree = 1
)

type traversalDecoder interface {
	init(main *buffer, ct *cornerTable, numVertices, numAttributeData int)
	start() (buffer, bool)
	decodeSymbol() uint32
	newActiveCornerReached(c int32)
	mergeVertices(dest, src int32)
	decodeStartFaceConfiguration() bool
	decodeAttributeSeam(i int) bool
	done()
}

// standardTraversal is MeshEdgebreakerTraversalDecoder.
type standardTraversal struct {
	buf          buffer
	symbolBuf    buffer
	startFace    bitDecoder
	seams        []bitDecoder
	numAttrData  int
	symbolsValid bool
}

func (t *standardTraversal) init(main *buffer, _ *cornerTable, _, numAttributeData int) {
	t.buf = main.tail()
	t.numAttrData = numAttributeData
}

func (t *standardTraversal) start() (buffer, bool) {
	if !t.decodeTraversalSymbols() || !t.decodeStartFaces() || !t.decodeAttributeSeams() {
		return buffer{}, false
	}
	return t.buf, true
}

func (t *standardTraversal) decodeTraversalSymbols() bool {
	t.symbolBuf = t.buf
	size, ok := t.symbolBuf.startBitDecoding(true)
	if !ok {
		return false
	}
	t.symbolsValid = true
	t.buf = t.symbolBuf
	if size > uint64(t.buf.remaining()) {
		return false
	}
	t.buf.advance(int(size))
	return true
}

func (t *standardTraversal) decodeStartFaces() bool { return t.startFace.startDecoding(&t.buf) }

func (t *standardTraversal) decodeAttributeSeams() bool {
	t.seams = make([]bitDecoder, t.numAttrData)
	for i := range t.seams {
		if !t.seams[i].startDecoding(&t.buf) {
			return false
		}
	}
	return true
}

func (t *standardTraversal) decodeSymbol() uint32 {
	symbol, _ := t.symbolBuf.bits(1)
	if symbol == topologyC {
		return symbol
	}
	suffix, _ := t.symbolBuf.bits(2)
	return symbol | suffix<<1
}

func (t *standardTraversal) newActiveCornerReached(int32)       {}
func (t *standardTraversal) mergeVertices(int32, int32)         {}
func (t *standardTraversal) decodeStartFaceConfiguration() bool { return t.startFace.decodeBit() }
func (t *standardTraversal) decodeAttributeSeam(i int) bool     { return t.seams[i].decodeBit() }
func (t *standardTraversal) done() {
	if t.symbolBuf.bitMode {
		t.symbolBuf.endBitDecoding()
	}
}

// valenceTraversal is MeshEdgebreakerTraversalValenceDecoder.
type valenceTraversal struct {
	standardTraversal
	ct             *cornerTable
	numVertices    int
	valences       []int32
	lastSymbol     uint32
	activeContext  int
	contextSymbols [][]uint32
	contextCounter []int
}

const (
	minValence = 2
	maxValence = 7
)

func (t *valenceTraversal) init(main *buffer, ct *cornerTable, numVertices, numAttributeData int) {
	t.standardTraversal.init(main, ct, numVertices, numAttributeData)
	t.ct = ct
	t.numVertices = numVertices
	t.activeContext = -1
	t.lastSymbol = topologyInvalid
}

func (t *valenceTraversal) start() (buffer, bool) {
	if !t.decodeStartFaces() || !t.decodeAttributeSeams() {
		return buffer{}, false
	}
	out := t.buf
	if t.numVertices < 0 {
		return buffer{}, false
	}
	t.valences = make([]int32, t.numVertices)
	n := maxValence - minValence + 1
	t.contextSymbols = make([][]uint32, n)
	t.contextCounter = make([]int, n)
	for i := 0; i < n; i++ {
		num, ok := out.varU32()
		if !ok || uint64(num) > uint64(t.ct.numFaces()) {
			return buffer{}, false
		}
		if num > 0 {
			t.contextSymbols[i] = make([]uint32, num)
			if !decodeSymbols(int(num), 1, &out, t.contextSymbols[i]) {
				return buffer{}, false
			}
			t.contextCounter[i] = int(num)
		}
	}
	return out, true
}

func (t *valenceTraversal) decodeSymbol() uint32 {
	if t.activeContext != -1 {
		t.contextCounter[t.activeContext]--
		counter := t.contextCounter[t.activeContext]
		if counter < 0 {
			return topologyInvalid
		}
		id := t.contextSymbols[t.activeContext][counter]
		if id > 4 {
			return topologyInvalid
		}
		t.lastSymbol = symbolToTopology[id]
	} else {
		t.lastSymbol = topologyE
	}
	return t.lastSymbol
}

func (t *valenceTraversal) addValence(v int32, n int32) {
	if v >= 0 && int(v) < len(t.valences) {
		t.valences[v] += n
	}
}

func (t *valenceTraversal) newActiveCornerReached(c int32) {
	nxt := next(c)
	prv := previous(c)
	switch t.lastSymbol {
	case topologyC, topologyS:
		t.addValence(t.ct.vertex(nxt), 1)
		t.addValence(t.ct.vertex(prv), 1)
	case topologyR:
		t.addValence(t.ct.vertex(c), 1)
		t.addValence(t.ct.vertex(nxt), 1)
		t.addValence(t.ct.vertex(prv), 2)
	case topologyL:
		t.addValence(t.ct.vertex(c), 1)
		t.addValence(t.ct.vertex(nxt), 2)
		t.addValence(t.ct.vertex(prv), 1)
	case topologyE:
		t.addValence(t.ct.vertex(c), 2)
		t.addValence(t.ct.vertex(nxt), 2)
		t.addValence(t.ct.vertex(prv), 2)
	}
	active := int32(0)
	if v := t.ct.vertex(nxt); v >= 0 && int(v) < len(t.valences) {
		active = t.valences[v]
	}
	clamped := min(max(active, minValence), maxValence)
	t.activeContext = int(clamped - minValence)
}

func (t *valenceTraversal) mergeVertices(dest, src int32) {
	if src >= 0 && int(src) < len(t.valences) {
		t.addValence(dest, t.valences[src])
	}
}

type topologySplit struct {
	splitSymbol  uint32
	sourceSymbol uint32
	sourceEdge   uint8
}

// encodingData is MeshAttributeIndicesEncodingData.
type encodingData struct {
	encodedToCorner []int32
	vertexToEncoded []int32
	numValues       int32
}

func (e *encodingData) init(numVertices int) {
	e.vertexToEncoded = make([]int32, numVertices)
	e.encodedToCorner = make([]int32, 0, numVertices)
	e.numValues = 0
}

type attributeData struct {
	decoderID   int
	conn        *attrCornerTable
	connUsed    bool
	enc         encodingData
	seamCorners []int32
}

type edgebreaker struct {
	d             *decoder
	ct            *cornerTable
	traversal     traversalDecoder
	splits        []topologySplit
	isVertHole    []bool
	attrData      []attributeData
	posEnc        encodingData
	posDecoderID  int
	numEncodedVer int
}

func newEdgebreaker(d *decoder, traversalType uint8) (*edgebreaker, error) {
	eb := &edgebreaker{d: d, posDecoderID: -1}
	switch traversalType {
	case traversalStandard:
		eb.traversal = &standardTraversal{}
	case traversalValence:
		eb.traversal = &valenceTraversal{}
	case traversalPredictive:
		return nil, errors.New("draco: deprecated predictive edgebreaker traversal is not supported")
	default:
		return nil, fmt.Errorf("draco: unknown edgebreaker traversal %d", traversalType)
	}
	return eb, nil
}

var errConnectivity = errors.New("draco: invalid edgebreaker connectivity")

// attrDataBytesPerCorner and attrDataBytesPerVertex bound the memory of one attribute connectivity: a seam flag (1), a corner-to-vertex entry (4), a seam corner (4, doubled for slice growth), a left-most corner (4) and two encoding entries (8) per corner, since an attribute connectivity has at most one vertex per corner; and a seam flag per mesh vertex.
const (
	attrDataBytesPerCorner = 1 + 4 + 8 + 4 + 8
	attrDataBytesPerVertex = 1
)

func (eb *edgebreaker) decodeConnectivity() error {
	b := eb.d.buf
	numEncodedVertices, ok := b.varU32()
	if !ok {
		return errConnectivity
	}
	numFaces, ok := b.varU32()
	if !ok {
		return errConnectivity
	}
	if uint64(numFaces) > uint64(eb.d.limits.MaxFaces) {
		return fmt.Errorf("draco: %d faces exceed the limit of %d", numFaces, eb.d.limits.MaxFaces)
	}
	if uint64(numEncodedVertices) > 3*uint64(numFaces) {
		return errConnectivity
	}
	minFaceEdges := 3 * uint64(numFaces) / 2
	nv := uint64(numEncodedVertices)
	if nv*(nv-1)/2 < minFaceEdges {
		return errConnectivity
	}
	numAttributeData, ok := b.u8()
	if !ok {
		return errConnectivity
	}
	numSymbols, ok := b.varU32()
	if !ok || numFaces < numSymbols {
		return errConnectivity
	}
	if uint64(numFaces) > uint64(numSymbols)+uint64(numSymbols)/3 {
		return errConnectivity
	}
	numSplitSymbols, ok := b.varU32()
	if !ok || numSplitSymbols > numSymbols {
		return errConnectivity
	}
	eb.numEncodedVer = int(numEncodedVertices)
	maxVertices := int(numEncodedVertices) + int(numSplitSymbols)
	// Each attribute connectivity holds its own seam flags, seam corners, corner-to-vertex map, left-most corners and encoding maps, every one sized by the corners or vertices of the mesh. Seam bits read past the end of the stream as zero, so nothing in the stream bounds their number; the byte budget does.
	if err := eb.d.charge(uint64(numAttributeData) * (attrDataBytesPerCorner*3*uint64(numFaces) + attrDataBytesPerVertex*uint64(maxVertices))); err != nil {
		return err
	}
	eb.ct = newCornerTable(int(numFaces), maxVertices)
	eb.attrData = make([]attributeData, numAttributeData)
	for i := range eb.attrData {
		eb.attrData[i] = attributeData{decoderID: -1, connUsed: true}
	}
	eb.isVertHole = make([]bool, maxVertices)
	for i := range eb.isVertHole {
		eb.isVertHole[i] = true
	}
	if !eb.decodeTopologySplitEvents(b) {
		return errConnectivity
	}
	eb.traversal.init(b, eb.ct, maxVertices, int(numAttributeData))
	end, ok := eb.traversal.start()
	if !ok {
		return errConnectivity
	}
	numConnectivityVerts, err := eb.decodeSymbols(int(numSymbols))
	if err != nil {
		return err
	}
	*b = end.tail()
	if len(eb.attrData) > 0 {
		for c := int32(0); int(c) < eb.ct.numCorners(); c += 3 {
			eb.decodeAttributeConnectivitiesOnFace(c)
		}
	}
	eb.traversal.done()
	for i := range eb.attrData {
		ad := &eb.attrData[i]
		ad.conn = newAttrCornerTable(eb.ct)
		for _, c := range ad.seamCorners {
			ad.conn.addSeamEdge(c)
		}
		if !ad.conn.recomputeVertices() {
			return errConnectivity
		}
	}
	eb.posEnc.init(eb.ct.numVertices())
	for i := range eb.attrData {
		n := max(eb.attrData[i].conn.numVertices(), eb.ct.numVertices())
		eb.attrData[i].enc.init(n)
	}
	return eb.assignPointsToCorners(numConnectivityVerts)
}

func (eb *edgebreaker) decodeTopologySplitEvents(b *buffer) bool {
	numSplits, ok := b.varU32()
	if !ok {
		return false
	}
	if numSplits == 0 {
		return true
	}
	if uint64(numSplits) > uint64(eb.ct.numFaces()) {
		return false
	}
	eb.splits = make([]topologySplit, numSplits)
	last := uint32(0)
	for i := range eb.splits {
		delta, ok := b.varU32()
		if !ok {
			return false
		}
		source := delta + last
		delta2, ok := b.varU32()
		if !ok || delta2 > source {
			return false
		}
		eb.splits[i] = topologySplit{sourceSymbol: source, splitSymbol: source - delta2}
		last = source
	}
	b.startBitDecoding(false)
	for i := range eb.splits {
		edge, _ := b.bits(1)
		eb.splits[i].sourceEdge = uint8(edge & 1)
	}
	b.endBitDecoding()
	return true
}

// isTopologySplit returns (isSplit, edge, encoderSplitSymbolID).
func (eb *edgebreaker) isTopologySplit(encoderSymbolID int) (bool, uint8, int) {
	if len(eb.splits) == 0 {
		return false, 0, 0
	}
	back := eb.splits[len(eb.splits)-1]
	if back.sourceSymbol > uint32(encoderSymbolID) {
		return true, 0, -1
	}
	if back.sourceSymbol != uint32(encoderSymbolID) {
		return false, 0, 0
	}
	eb.splits = eb.splits[:len(eb.splits)-1]
	return true, back.sourceEdge, int(back.splitSymbol)
}

const (
	leftFaceEdge  = 0
	rightFaceEdge = 1
)

func (eb *edgebreaker) setHole(v int32, hole bool) {
	if v >= 0 && int(v) < len(eb.isVertHole) {
		eb.isVertHole[v] = hole
	}
}

// decodeSymbols is DecodeConnectivity(int num_symbols); it returns the number of connectivity vertices.
func (eb *edgebreaker) decodeSymbols(numSymbols int) (int, error) {
	ct := eb.ct
	var active []int32
	splitActive := map[int]int32{}
	var invalidVertices []int32
	removeInvalid := len(eb.attrData) == 0
	maxVertices := len(eb.isVertHole)
	cornerLimit := ct.numCorners() + 1
	numFaces := 0
	for symbolID := 0; symbolID < numSymbols; symbolID++ {
		if numFaces >= ct.numFaces() {
			return 0, errConnectivity
		}
		face := int32(numFaces)
		numFaces++
		checkSplit := false
		corner := 3 * face
		symbol := eb.traversal.decodeSymbol()
		switch symbol {
		case topologyC:
			if len(active) == 0 {
				return 0, errConnectivity
			}
			cornerA := active[len(active)-1]
			vertexX := ct.vertex(next(cornerA))
			cornerB := next(ct.leftMostCorner(vertexX))
			if cornerA == cornerB || cornerB == invalidIndex {
				return 0, errConnectivity
			}
			if ct.opposite(cornerA) != invalidIndex || ct.opposite(cornerB) != invalidIndex {
				return 0, errConnectivity
			}
			ct.setOppositeCorners(cornerA, corner+1)
			ct.setOppositeCorners(cornerB, corner+2)
			vertAPrev := ct.vertex(previous(cornerA))
			vertBNext := ct.vertex(next(cornerB))
			if vertexX == vertAPrev || vertexX == vertBNext {
				return 0, errConnectivity
			}
			ct.mapCornerToVertex(corner, vertexX)
			ct.mapCornerToVertex(corner+1, vertBNext)
			ct.mapCornerToVertex(corner+2, vertAPrev)
			ct.setLeftMostCorner(vertAPrev, corner+2)
			eb.setHole(vertexX, false)
			active[len(active)-1] = corner
		case topologyR, topologyL:
			if len(active) == 0 {
				return 0, errConnectivity
			}
			cornerA := active[len(active)-1]
			if ct.opposite(cornerA) != invalidIndex {
				return 0, errConnectivity
			}
			var oppCorner, cornerL, cornerR int32
			if symbol == topologyR {
				oppCorner, cornerL, cornerR = corner+2, corner+1, corner
			} else {
				oppCorner, cornerL, cornerR = corner+1, corner, corner+2
			}
			ct.setOppositeCorners(oppCorner, cornerA)
			newVert := ct.addNewVertex()
			if ct.numVertices() > maxVertices {
				return 0, errConnectivity
			}
			ct.mapCornerToVertex(oppCorner, newVert)
			ct.setLeftMostCorner(newVert, oppCorner)
			vertexR := ct.vertex(previous(cornerA))
			ct.mapCornerToVertex(cornerR, vertexR)
			ct.setLeftMostCorner(vertexR, cornerR)
			ct.mapCornerToVertex(cornerL, ct.vertex(next(cornerA)))
			active[len(active)-1] = corner
			checkSplit = true
		case topologyS:
			if len(active) == 0 {
				return 0, errConnectivity
			}
			cornerB := active[len(active)-1]
			active = active[:len(active)-1]
			if c, ok := splitActive[symbolID]; ok {
				active = append(active, c)
			}
			if len(active) == 0 {
				return 0, errConnectivity
			}
			cornerA := active[len(active)-1]
			if cornerA == cornerB {
				return 0, errConnectivity
			}
			if ct.opposite(cornerA) != invalidIndex || ct.opposite(cornerB) != invalidIndex {
				return 0, errConnectivity
			}
			ct.setOppositeCorners(cornerA, corner+2)
			ct.setOppositeCorners(cornerB, corner+1)
			vertexP := ct.vertex(previous(cornerA))
			ct.mapCornerToVertex(corner, vertexP)
			ct.mapCornerToVertex(corner+1, ct.vertex(next(cornerA)))
			vertBPrev := ct.vertex(previous(cornerB))
			ct.mapCornerToVertex(corner+2, vertBPrev)
			ct.setLeftMostCorner(vertBPrev, corner+2)
			cornerN := next(cornerB)
			vertexN := ct.vertex(cornerN)
			eb.traversal.mergeVertices(vertexP, vertexN)
			ct.setLeftMostCorner(vertexP, ct.leftMostCorner(vertexN))
			first := cornerN
			for steps := 0; cornerN != invalidIndex; steps++ {
				if steps > cornerLimit {
					return 0, errConnectivity
				}
				ct.mapCornerToVertex(cornerN, vertexP)
				cornerN = swingLeft(ct, cornerN)
				if cornerN == first {
					return 0, errConnectivity
				}
			}
			ct.makeVertexIsolated(vertexN)
			if removeInvalid {
				invalidVertices = append(invalidVertices, vertexN)
			}
			active[len(active)-1] = corner
		case topologyE:
			first := ct.addNewVertex()
			ct.mapCornerToVertex(corner, first)
			ct.mapCornerToVertex(corner+1, ct.addNewVertex())
			ct.mapCornerToVertex(corner+2, ct.addNewVertex())
			if ct.numVertices() > maxVertices {
				return 0, errConnectivity
			}
			ct.setLeftMostCorner(first, corner)
			ct.setLeftMostCorner(first+1, corner+1)
			ct.setLeftMostCorner(first+2, corner+2)
			active = append(active, corner)
			checkSplit = true
		default:
			return 0, errConnectivity
		}
		eb.traversal.newActiveCornerReached(active[len(active)-1])
		if checkSplit {
			encoderSymbolID := numSymbols - symbolID - 1
			for {
				isSplit, edge, encoderSplitSymbolID := eb.isTopologySplit(encoderSymbolID)
				if !isSplit {
					break
				}
				if encoderSplitSymbolID < 0 {
					return 0, errConnectivity
				}
				top := active[len(active)-1]
				var newActive int32
				if edge == rightFaceEdge {
					newActive = next(top)
				} else {
					newActive = previous(top)
				}
				splitActive[numSymbols-encoderSplitSymbolID-1] = newActive
			}
		}
	}
	if ct.numVertices() > maxVertices {
		return 0, errConnectivity
	}
	for len(active) > 0 {
		corner := active[len(active)-1]
		active = active[:len(active)-1]
		if eb.traversal.decodeStartFaceConfiguration() {
			if numFaces >= ct.numFaces() {
				return 0, errConnectivity
			}
			cornerA := corner
			vertN := ct.vertex(next(cornerA))
			cornerB := next(ct.leftMostCorner(vertN))
			vertX := ct.vertex(next(cornerB))
			cornerC := next(ct.leftMostCorner(vertX))
			if cornerB == invalidIndex || cornerC == invalidIndex {
				return 0, errConnectivity
			}
			if corner == cornerB || corner == cornerC || cornerB == cornerC {
				return 0, errConnectivity
			}
			if ct.opposite(corner) != invalidIndex || ct.opposite(cornerB) != invalidIndex || ct.opposite(cornerC) != invalidIndex {
				return 0, errConnectivity
			}
			vertP := ct.vertex(next(cornerC))
			newCorner := int32(3 * numFaces)
			numFaces++
			ct.setOppositeCorners(newCorner, corner)
			ct.setOppositeCorners(newCorner+1, cornerB)
			ct.setOppositeCorners(newCorner+2, cornerC)
			ct.mapCornerToVertex(newCorner, vertX)
			ct.mapCornerToVertex(newCorner+1, vertP)
			ct.mapCornerToVertex(newCorner+2, vertN)
			for ci := int32(0); ci < 3; ci++ {
				eb.setHole(ct.vertex(newCorner+ci), false)
			}
		}
	}
	if numFaces != ct.numFaces() {
		return 0, errConnectivity
	}
	numVertices := ct.numVertices()
	for _, invalidVert := range invalidVertices {
		srcVert := int32(numVertices - 1)
		for ct.leftMostCorner(srcVert) == invalidIndex {
			numVertices--
			srcVert = int32(numVertices - 1)
			if srcVert < 0 {
				return 0, errConnectivity
			}
		}
		if srcVert < invalidVert {
			continue
		}
		start := ct.leftMostCorner(srcVert)
		ok := forEachVertexCorner(ct, start, cornerLimit, func(c int32) bool {
			if ct.vertex(c) != srcVert {
				return false
			}
			ct.mapCornerToVertex(c, invalidVert)
			return true
		})
		if !ok {
			return 0, errConnectivity
		}
		ct.setLeftMostCorner(invalidVert, ct.leftMostCorner(srcVert))
		ct.makeVertexIsolated(srcVert)
		if int(invalidVert) < len(eb.isVertHole) && int(srcVert) < len(eb.isVertHole) {
			eb.isVertHole[invalidVert] = eb.isVertHole[srcVert]
			eb.isVertHole[srcVert] = false
		}
		numVertices--
	}
	return numVertices, nil
}

// forEachVertexCorner walks the corners around the vertex of start like VertexCornersIterator, stopping with false if fn fails or the walk does not terminate.
func forEachVertexCorner(t connectivity, start int32, limit int, fn func(c int32) bool) bool {
	if start == invalidIndex {
		return true
	}
	c := start
	left := true
	for steps := 0; c != invalidIndex; steps++ {
		if steps > limit {
			return false
		}
		if !fn(c) {
			return false
		}
		if left {
			c = swingLeft(t, c)
			if c == invalidIndex {
				c = swingRight(t, start)
				left = false
			} else if c == start {
				c = invalidIndex
			}
		} else {
			c = swingRight(t, c)
		}
	}
	return true
}

func (eb *edgebreaker) decodeAttributeConnectivitiesOnFace(corner int32) {
	corners := [3]int32{corner, next(corner), previous(corner)}
	srcFace := corner / 3
	for _, c := range corners {
		opp := eb.ct.opposite(c)
		if opp == invalidIndex {
			for i := range eb.attrData {
				eb.attrData[i].seamCorners = append(eb.attrData[i].seamCorners, c)
			}
			continue
		}
		if opp/3 < srcFace {
			continue
		}
		for i := range eb.attrData {
			if eb.traversal.decodeAttributeSeam(i) {
				eb.attrData[i].seamCorners = append(eb.attrData[i].seamCorners, c)
			}
		}
	}
}

func (eb *edgebreaker) assignPointsToCorners(numConnectivityVerts int) error {
	ct := eb.ct
	numFaces := ct.numFaces()
	faces := make([]uint32, 3*numFaces)
	if len(eb.attrData) == 0 {
		for c := 0; c < 3*numFaces; c++ {
			v := ct.vertex(int32(c))
			if v < 0 {
				return errConnectivity
			}
			faces[c] = uint32(v)
		}
		eb.d.faces = faces
		eb.d.numPoints = numConnectivityVerts
		return nil
	}
	limit := ct.numCorners() + 1
	var pointToCorner []int32
	cornerToPoint := make([]int32, ct.numCorners())
	for v := int32(0); int(v) < ct.numVertices(); v++ {
		c := ct.leftMostCorner(v)
		if c == invalidIndex {
			continue
		}
		dedupFirst := c
		if !eb.isVertHole[v] {
			for i := range eb.attrData {
				conn := eb.attrData[i].conn
				if !conn.isCornerOnSeam(c) {
					continue
				}
				vertID := conn.vertex(c)
				actC := swingRight(ct, c)
				seamFound := false
				for steps := 0; actC != c; steps++ {
					if actC == invalidIndex || steps > limit {
						return errConnectivity
					}
					if conn.vertex(actC) != vertID {
						dedupFirst = actC
						seamFound = true
						break
					}
					actC = swingRight(ct, actC)
				}
				if seamFound {
					break
				}
			}
		}
		c = dedupFirst
		cornerToPoint[c] = int32(len(pointToCorner))
		pointToCorner = append(pointToCorner, c)
		prevC := c
		c = swingRight(ct, c)
		for steps := 0; c != invalidIndex && c != dedupFirst; steps++ {
			if steps > limit {
				return errConnectivity
			}
			seam := false
			for i := range eb.attrData {
				conn := eb.attrData[i].conn
				if conn.vertex(c) != conn.vertex(prevC) {
					seam = true
					break
				}
			}
			if seam {
				cornerToPoint[c] = int32(len(pointToCorner))
				pointToCorner = append(pointToCorner, c)
			} else {
				cornerToPoint[c] = cornerToPoint[prevC]
			}
			prevC = c
			c = swingRight(ct, c)
		}
	}
	for c := range faces {
		faces[c] = uint32(cornerToPoint[c])
	}
	eb.d.faces = faces
	eb.d.numPoints = len(pointToCorner)
	return nil
}

// createAttributesDecoder reads the per-decoder header and returns the sequencer that orders its points.
func (eb *edgebreaker) createAttributesDecoder(decoderID int) (sequencer, error) {
	b := eb.d.buf
	attDataID, ok := b.i8()
	if !ok {
		return nil, errConnectivity
	}
	decoderType, ok := b.u8()
	if !ok {
		return nil, errConnectivity
	}
	if attDataID >= 0 {
		if int(attDataID) >= len(eb.attrData) || eb.attrData[attDataID].decoderID >= 0 {
			return nil, errConnectivity
		}
		eb.attrData[attDataID].decoderID = decoderID
	} else {
		if eb.posDecoderID >= 0 {
			return nil, errConnectivity
		}
		eb.posDecoderID = decoderID
	}
	method, ok := b.u8()
	if !ok || method > traversalPredictionDegree {
		return nil, errConnectivity
	}
	if decoderType == meshVertexAttribute {
		enc := &eb.posEnc
		if attDataID >= 0 {
			enc = &eb.attrData[attDataID].enc
			eb.attrData[attDataID].connUsed = false
		}
		return &traversalSequencer{d: eb.d, table: eb.ct, enc: enc, maxDegree: method == traversalPredictionDegree}, nil
	}
	if method != traversalDepthFirst || attDataID < 0 {
		return nil, errConnectivity
	}
	ad := &eb.attrData[attDataID]
	return &traversalSequencer{d: eb.d, table: ad.conn, enc: &ad.enc}, nil
}

// attributeConnectivity is GetAttributeCornerTable followed by the CornerTable fallback of CreateMeshPredictionScheme.
func (eb *edgebreaker) attributeConnectivity(attID int) (connectivity, *encodingData) {
	for i := range eb.attrData {
		ad := &eb.attrData[i]
		if ad.decoderID < 0 || ad.decoderID >= len(eb.d.attrDecoders) {
			continue
		}
		for _, id := range eb.d.attrDecoders[ad.decoderID].attIDs {
			if id == attID {
				if ad.connUsed {
					return ad.conn, &ad.enc
				}
				return eb.ct, &ad.enc
			}
		}
	}
	return eb.ct, &eb.posEnc
}
