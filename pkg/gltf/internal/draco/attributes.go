package draco

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// This file ports the sequential attribute decoders, their controller and the point sequencers.

// Draco data types (draco::DataType).
const (
	dtInvalid = iota
	dtInt8
	dtUint8
	dtInt16
	dtUint16
	dtInt32
	dtUint32
	dtInt64
	dtUint64
	dtFloat32
	dtFloat64
	dtBool
	dtTypesCount
)

func dataTypeLength(dt uint8) int {
	switch dt {
	case dtInt8, dtUint8, dtBool:
		return 1
	case dtInt16, dtUint16:
		return 2
	case dtInt32, dtUint32, dtFloat32:
		return 4
	case dtInt64, dtUint64, dtFloat64:
		return 8
	}
	return 0
}

// maxNamedAttributeTypes accepts the transcoder attribute types (tangent, material, joints, weights) as well as the classic five.
const maxNamedAttributeTypes = 9

// Sequential attribute decoder kinds (SequentialAttributeEncoderType).
const (
	seqGeneric = iota
	seqInteger
	seqQuantization
	seqNormals
)

var errAttribute = errors.New("draco: invalid attribute data")

// portable holds the integer values an attribute decoder predicts on, before dequantization or octahedral decoding.
type portable struct {
	values        []int32
	numComponents int
	numEntries    int
}

type pointAttribute struct {
	attType       uint8
	dataType      uint8
	numComponents int
	normalized    bool
	uniqueID      uint32

	numValues int
	values    []float64
	mapping   []int32
	portable  *portable
}

// valueIndex maps a point to its attribute value index.
func (a *pointAttribute) valueIndex(point int) (int, bool) {
	if a.mapping == nil {
		return point, point >= 0 && point < a.numValues
	}
	if point < 0 || point >= len(a.mapping) {
		return 0, false
	}
	v := a.mapping[point]
	return int(v), v >= 0 && int(v) < a.numValues
}

// sequencer is PointsSequencer.
type sequencer interface {
	generate() ([]int32, error)
	updateMapping(a *pointAttribute) error
}

type linearSequencer struct{ numPoints int }

func (s *linearSequencer) generate() ([]int32, error) {
	ids := make([]int32, s.numPoints)
	for i := range ids {
		ids[i] = int32(i)
	}
	return ids, nil
}

func (s *linearSequencer) updateMapping(a *pointAttribute) error {
	a.mapping = nil
	return nil
}

// traversalSequencer is MeshTraversalSequencer with a depth-first or max-prediction-degree traverser and the MeshAttributeIndicesEncodingObserver.
type traversalSequencer struct {
	d         *decoder
	table     connectivity
	enc       *encodingData
	maxDegree bool

	faceVisited   []bool
	vertexVisited []bool
	points        []int32
	degree        []int32
	stacks        [3][]int32
	bestPriority  int
}

func (s *traversalSequencer) generate() ([]int32, error) {
	s.faceVisited = make([]bool, s.table.numFaces())
	s.vertexVisited = make([]bool, s.table.numVertices())
	s.points = make([]int32, 0, s.table.numVertices())
	if s.maxDegree {
		s.degree = make([]int32, s.table.numVertices())
	}
	for f := 0; f < s.table.numFaces(); f++ {
		var err error
		if s.maxDegree {
			err = s.traverseMaxDegree(int32(3 * f))
		} else {
			err = s.traverseDepthFirst(int32(3 * f))
		}
		if err != nil {
			return nil, err
		}
	}
	return s.points, nil
}

func (s *traversalSequencer) isFaceVisited(c int32) bool {
	if c < 0 {
		return true
	}
	f := int(c / 3)
	return f >= len(s.faceVisited) || s.faceVisited[f]
}

func (s *traversalSequencer) visitVertex(v, c int32) error {
	if v < 0 || int(v) >= len(s.vertexVisited) {
		return errAttribute
	}
	if s.vertexVisited[v] {
		return nil
	}
	s.vertexVisited[v] = true
	if c < 0 || int(c) >= len(s.d.faces) || int(v) >= len(s.enc.vertexToEncoded) {
		return errAttribute
	}
	s.points = append(s.points, int32(s.d.faces[c]))
	s.enc.encodedToCorner = append(s.enc.encodedToCorner, c)
	s.enc.vertexToEncoded[v] = s.enc.numValues
	s.enc.numValues++
	return nil
}

func (s *traversalSequencer) vertexVisitedAt(v int32) bool {
	return v >= 0 && int(v) < len(s.vertexVisited) && s.vertexVisited[v]
}

// traverseDepthFirst is DepthFirstTraverser::TraverseFromCorner.
func (s *traversalSequencer) traverseDepthFirst(corner int32) error {
	if s.isFaceVisited(corner) {
		return nil
	}
	t := s.table
	stack := []int32{corner}
	nextVert := t.vertex(next(corner))
	prevVert := t.vertex(previous(corner))
	if nextVert == invalidIndex || prevVert == invalidIndex {
		return errAttribute
	}
	if err := s.visitVertex(nextVert, next(corner)); err != nil {
		return err
	}
	if err := s.visitVertex(prevVert, previous(corner)); err != nil {
		return err
	}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		if c == invalidIndex || s.isFaceVisited(c) {
			stack = stack[:len(stack)-1]
			continue
		}
		for {
			if c < 0 || int(c/3) >= len(s.faceVisited) {
				return errAttribute
			}
			s.faceVisited[c/3] = true
			v := t.vertex(c)
			if v == invalidIndex {
				return errAttribute
			}
			if !s.vertexVisitedAt(v) {
				onBoundary := t.isOnBoundary(v)
				if err := s.visitVertex(v, c); err != nil {
					return err
				}
				if !onBoundary {
					c = rightCorner(t, c)
					continue
				}
			}
			r := rightCorner(t, c)
			l := leftCorner(t, c)
			if s.isFaceVisited(r) {
				if s.isFaceVisited(l) {
					stack = stack[:len(stack)-1]
					break
				}
				c = l
			} else {
				if s.isFaceVisited(l) {
					c = r
				} else {
					stack[len(stack)-1] = l
					stack = append(stack, r)
					break
				}
			}
		}
	}
	return nil
}

// traverseMaxDegree is MaxPredictionDegreeTraverser::TraverseFromCorner.
func (s *traversalSequencer) traverseMaxDegree(corner int32) error {
	if len(s.degree) == 0 {
		return nil
	}
	t := s.table
	s.stacks[0] = append(s.stacks[0], corner)
	s.bestPriority = 0
	for _, c := range []int32{next(corner), previous(corner), corner} {
		if err := s.visitVertex(t.vertex(c), c); err != nil {
			return err
		}
	}
	for {
		c := s.popCorner()
		if c == invalidIndex {
			return nil
		}
		if s.isFaceVisited(c) {
			continue
		}
		for {
			if c < 0 || int(c/3) >= len(s.faceVisited) {
				return errAttribute
			}
			s.faceVisited[c/3] = true
			if err := s.visitVertex(t.vertex(c), c); err != nil {
				return err
			}
			r := rightCorner(t, c)
			l := leftCorner(t, c)
			rightVisited := s.isFaceVisited(r)
			leftVisited := s.isFaceVisited(l)
			if !leftVisited {
				p := s.priority(l)
				if rightVisited && p <= s.bestPriority {
					c = l
					continue
				}
				s.pushCorner(l, p)
			}
			if !rightVisited {
				p := s.priority(r)
				if p <= s.bestPriority {
					c = r
					continue
				}
				s.pushCorner(r, p)
			}
			break
		}
	}
}

func (s *traversalSequencer) popCorner() int32 {
	for i := s.bestPriority; i < len(s.stacks); i++ {
		if n := len(s.stacks[i]); n > 0 {
			c := s.stacks[i][n-1]
			s.stacks[i] = s.stacks[i][:n-1]
			s.bestPriority = i
			return c
		}
	}
	return invalidIndex
}

func (s *traversalSequencer) pushCorner(c int32, p int) {
	s.stacks[p] = append(s.stacks[p], c)
	if p < s.bestPriority {
		s.bestPriority = p
	}
}

func (s *traversalSequencer) priority(c int32) int {
	tip := s.table.vertex(c)
	p := 0
	if tip >= 0 && int(tip) < len(s.degree) && !s.vertexVisitedAt(tip) {
		s.degree[tip]++
		if s.degree[tip] > 1 {
			p = 1
		} else {
			p = 2
		}
	}
	return min(p, len(s.stacks)-1)
}

func (s *traversalSequencer) updateMapping(a *pointAttribute) error {
	numPoints := s.d.numPoints
	a.mapping = make([]int32, numPoints)
	for i := range a.mapping {
		a.mapping[i] = invalidIndex
	}
	for c, point := range s.d.faces {
		v := s.table.vertex(int32(c))
		if v == invalidIndex || int(v) >= len(s.enc.vertexToEncoded) {
			return errAttribute
		}
		entry := s.enc.vertexToEncoded[v]
		if int(point) >= numPoints || int(entry) >= numPoints {
			return errAttribute
		}
		a.mapping[point] = entry
	}
	return nil
}

// attributesDecoder is SequentialAttributeDecodersController.
type attributesDecoder struct {
	seq      sequencer
	attIDs   []int
	decoders []*sequentialDecoder
	pointIDs []int32
}

// decodeHeader is DecodeAttributesDecoderData.
func (ad *attributesDecoder) decodeHeader(d *decoder) error {
	b := d.buf
	n, ok := b.varU32()
	if !ok || n == 0 || uint64(n) > 5*uint64(b.remaining()) {
		return errAttribute
	}
	ad.attIDs = make([]int, n)
	for i := range ad.attIDs {
		attType, ok1 := b.u8()
		dataType, ok2 := b.u8()
		numComponents, ok3 := b.u8()
		normalized, ok4 := b.u8()
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return errAttribute
		}
		if attType >= maxNamedAttributeTypes || dataType == dtInvalid || dataType >= dtTypesCount || numComponents == 0 {
			return errAttribute
		}
		uniqueID, ok := b.varU32()
		if !ok {
			return errAttribute
		}
		d.attributes = append(d.attributes, &pointAttribute{
			attType:       attType,
			dataType:      dataType,
			numComponents: int(numComponents),
			normalized:    normalized > 0,
			uniqueID:      uniqueID,
		})
		ad.attIDs[i] = len(d.attributes) - 1
	}
	ad.decoders = make([]*sequentialDecoder, n)
	for i := range ad.decoders {
		kind, ok := b.u8()
		if !ok {
			return errAttribute
		}
		att := d.attributes[ad.attIDs[i]]
		switch kind {
		case seqGeneric, seqInteger:
		case seqQuantization:
			if att.dataType != dtFloat32 {
				return errAttribute
			}
		case seqNormals:
			if att.numComponents != 3 || att.dataType != dtFloat32 {
				return errAttribute
			}
		default:
			return fmt.Errorf("draco: unknown attribute decoder %d", kind)
		}
		ad.decoders[i] = &sequentialDecoder{d: d, kind: int(kind), attID: ad.attIDs[i], att: att}
	}
	return nil
}

func (ad *attributesDecoder) decode(d *decoder) error {
	ids, err := ad.seq.generate()
	if err != nil {
		return err
	}
	if len(ids) > d.limits.MaxPoints {
		return errAttribute
	}
	ad.pointIDs = ids
	// Bound every per-value allocation before making any: the point mapping, and the portable values, entropy symbols and decoded values of each attribute. The rANS bit readers return zeros past the end of the stream, so the stream's size does not bound these.
	var need uint64
	for _, sd := range ad.decoders {
		need += attrBytesPerPoint*uint64(d.numPoints) + attrBytesPerValue*uint64(len(ids))*uint64(sd.att.numComponents)
	}
	if err := d.charge(need); err != nil {
		return err
	}
	for _, id := range ad.attIDs {
		if err := ad.seq.updateMapping(d.attributes[id]); err != nil {
			return err
		}
	}
	for _, sd := range ad.decoders {
		if err := sd.decodePortable(ad.pointIDs); err != nil {
			return err
		}
	}
	for _, sd := range ad.decoders {
		if err := sd.decodeTransformData(); err != nil {
			return err
		}
	}
	for _, sd := range ad.decoders {
		if err := sd.storeValues(); err != nil {
			return err
		}
	}
	return nil
}

// attrBytesPerPoint covers an attribute's point-to-value mapping; attrBytesPerValue covers, per component of each value, the portable int32, the entropy symbol decoded into it, the float64 kept, and a byte of prediction state.
const (
	attrBytesPerPoint = 4
	attrBytesPerValue = 4 + 4 + 8 + 1
)

// sequentialDecoder covers SequentialAttributeDecoder and its integer, quantization and normal subclasses.
type sequentialDecoder struct {
	d     *decoder
	kind  int
	attID int
	att   *pointAttribute
	pred  predictionScheme

	minValues []float32
	rng       float32
	bits      int
}

func (sd *sequentialDecoder) numValueComponents() int {
	if sd.kind == seqNormals {
		return 2
	}
	return sd.att.numComponents
}

func (sd *sequentialDecoder) decodePortable(pointIDs []int32) error {
	att := sd.att
	att.numValues = len(pointIDs)
	if sd.kind == seqGeneric {
		return sd.decodeGeneric(len(pointIDs))
	}
	b := sd.d.buf
	method, ok := b.i8()
	if !ok || method < predictionNone || method >= numPredictionSchemes {
		return errAttribute
	}
	if method != predictionNone {
		tt, ok := b.i8()
		if !ok || tt < transformNone || tt >= numPredictionTransformTypes {
			return errAttribute
		}
		pred, err := sd.d.createPredictionScheme(int(method), int(tt), sd.attID, sd.kind == seqNormals)
		if err != nil {
			return err
		}
		sd.pred = pred
	}
	if sd.pred != nil && sd.pred.usesPositionParent() {
		if err := sd.pred.setParent(sd.d.namedAttribute(attributeTypePosition)); err != nil {
			return err
		}
	}
	return sd.decodeIntegerValues(pointIDs)
}

func (sd *sequentialDecoder) decodeGeneric(numValues int) error {
	att := sd.att
	size := dataTypeLength(att.dataType) * att.numComponents
	if size == 0 || uint64(numValues)*uint64(size) > uint64(sd.d.buf.remaining()) {
		return errAttribute
	}
	att.values = make([]float64, numValues*att.numComponents)
	for i := range att.values {
		raw, _ := sd.d.buf.bytes(dataTypeLength(att.dataType))
		att.values[i] = rawValue(att.dataType, raw)
	}
	return nil
}

func rawValue(dt uint8, b []byte) float64 {
	switch dt {
	case dtInt8:
		return float64(int8(b[0]))
	case dtUint8, dtBool:
		return float64(b[0])
	case dtInt16:
		return float64(int16(binary.LittleEndian.Uint16(b)))
	case dtUint16:
		return float64(binary.LittleEndian.Uint16(b))
	case dtInt32:
		return float64(int32(binary.LittleEndian.Uint32(b)))
	case dtUint32:
		return float64(binary.LittleEndian.Uint32(b))
	case dtInt64:
		return float64(int64(binary.LittleEndian.Uint64(b)))
	case dtUint64:
		return float64(binary.LittleEndian.Uint64(b))
	case dtFloat32:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case dtFloat64:
		return math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	return 0
}

func (sd *sequentialDecoder) decodeIntegerValues(pointIDs []int32) error {
	nc := sd.numValueComponents()
	if nc <= 0 {
		return errAttribute
	}
	numEntries := len(pointIDs)
	numValues := numEntries * nc
	port := &portable{values: make([]int32, numValues), numComponents: nc, numEntries: numEntries}
	sd.att.portable = port
	b := sd.d.buf
	compressed, ok := b.u8()
	if !ok {
		return errAttribute
	}
	if compressed > 0 {
		symbols := make([]uint32, numValues)
		if !decodeSymbols(numValues, nc, b, symbols) {
			return errAttribute
		}
		for i, s := range symbols {
			port.values[i] = int32(s)
		}
	} else {
		numBytes, ok := b.u8()
		if !ok || numBytes > 4 || uint64(numBytes)*uint64(numValues) > uint64(b.remaining()) {
			return errAttribute
		}
		for i := range port.values {
			raw, _ := b.bytes(int(numBytes))
			var v uint32
			for k, c := range raw {
				v |= uint32(c) << (8 * uint(k))
			}
			port.values[i] = int32(v)
		}
	}
	if numValues > 0 && (sd.pred == nil || !sd.pred.correctionsPositive()) {
		for i, v := range port.values {
			u := uint32(v)
			if u&1 == 0 {
				port.values[i] = int32(u >> 1)
			} else {
				port.values[i] = -int32(u>>1) - 1
			}
		}
	}
	if sd.pred != nil {
		if !sd.pred.decodePredictionData(b) {
			return errAttribute
		}
		if numValues > 0 {
			if err := sd.pred.computeOriginal(port.values, nc, pointIDs); err != nil {
				return err
			}
		}
	}
	return nil
}

func (sd *sequentialDecoder) decodeTransformData() error {
	b := sd.d.buf
	switch sd.kind {
	case seqQuantization:
		sd.minValues = make([]float32, sd.att.numComponents)
		for i := range sd.minValues {
			v, ok := b.f32()
			if !ok {
				return errAttribute
			}
			sd.minValues[i] = v
		}
		rng, ok := b.f32()
		if !ok {
			return errAttribute
		}
		bits, ok := b.u8()
		if !ok || bits < 1 || bits > 30 {
			return errAttribute
		}
		sd.rng, sd.bits = rng, int(bits)
	case seqNormals:
		bits, ok := b.u8()
		if !ok {
			return errAttribute
		}
		sd.bits = int(bits)
	}
	return nil
}

func (sd *sequentialDecoder) storeValues() error {
	att := sd.att
	nc := att.numComponents
	switch sd.kind {
	case seqGeneric:
		return nil
	case seqInteger:
		port := att.portable
		att.values = make([]float64, len(port.values))
		for i, v := range port.values {
			switch att.dataType {
			case dtUint8:
				att.values[i] = float64(uint8(v))
			case dtInt8:
				att.values[i] = float64(int8(v))
			case dtUint16:
				att.values[i] = float64(uint16(v))
			case dtInt16:
				att.values[i] = float64(int16(v))
			case dtUint32:
				att.values[i] = float64(uint32(v))
			case dtInt32:
				att.values[i] = float64(v)
			default:
				return errAttribute
			}
		}
	case seqQuantization:
		maxQ := int32((uint32(1) << uint32(sd.bits)) - 1)
		if maxQ <= 0 {
			return errAttribute
		}
		delta := sd.rng / float32(maxQ)
		port := att.portable
		att.values = make([]float64, att.numValues*nc)
		for i := range att.values {
			q := float32(float32(port.values[i]) * delta)
			att.values[i] = float64(q + sd.minValues[i%nc])
		}
	case seqNormals:
		var tb octahedronToolBox
		if !tb.setQuantizationBits(int32(sd.bits)) {
			return errAttribute
		}
		port := att.portable
		att.values = make([]float64, att.numValues*3)
		for i := 0; i < att.numValues; i++ {
			v := tb.unitVector(port.values[2*i], port.values[2*i+1])
			att.values[3*i] = float64(v[0])
			att.values[3*i+1] = float64(v[1])
			att.values[3*i+2] = float64(v[2])
		}
	}
	return nil
}
