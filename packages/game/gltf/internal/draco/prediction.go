package draco

import (
	"errors"
	"math"
	"math/bits"
)

// This file ports Draco's prediction schemes and their decoding transforms for bitstream 2.2.

const (
	predictionNone                       = -2
	predictionDifference                 = 0
	predictionParallelogram              = 1
	predictionMultiParallelogram         = 2
	predictionTexCoordsDeprecated        = 3
	predictionConstrainedMultiParallelog = 4
	predictionTexCoordsPortable          = 5
	predictionGeometricNormal            = 6
	numPredictionSchemes                 = 7
)

const (
	transformNone                 = -1
	transformDelta                = 0
	transformWrap                 = 1
	transformOctahedron           = 2
	transformOctahedronCanonical  = 3
	numPredictionTransformTypes   = 4
	maxConstrainedParallelograms  = 4
	attributeTypePosition         = 0
	positionComponentsForPredicts = 3
)

var errPrediction = errors.New("draco: invalid prediction data")

// transform is a PredictionScheme*DecodingTransform.
type transform interface {
	init(numComponents int)
	decodeData(b *buffer) bool
	correctionsPositive() bool
	original(pred, corr, out []int32)
	quantizationBits() int
}

// wrapTransform is PredictionSchemeWrapDecodingTransform<int32_t>.
type wrapTransform struct {
	numComponents int
	minValue      int32
	maxValue      int32
	maxDif        int32
	clamped       []int32
}

func (t *wrapTransform) init(n int) {
	t.numComponents = n
	t.clamped = make([]int32, n)
}

func (t *wrapTransform) decodeData(b *buffer) bool {
	minV, ok1 := b.i32()
	maxV, ok2 := b.i32()
	if !ok1 || !ok2 || minV > maxV {
		return false
	}
	t.minValue, t.maxValue = minV, maxV
	dif := int64(maxV) - int64(minV)
	if dif < 0 || dif >= math.MaxInt32 {
		return false
	}
	t.maxDif = 1 + int32(dif)
	return true
}

func (t *wrapTransform) correctionsPositive() bool { return false }
func (t *wrapTransform) quantizationBits() int     { return -1 }

func (t *wrapTransform) original(pred, corr, out []int32) {
	for i := 0; i < t.numComponents; i++ {
		p := pred[i]
		if p > t.maxValue {
			p = t.maxValue
		} else if p < t.minValue {
			p = t.minValue
		}
		t.clamped[i] = p
	}
	for i := 0; i < t.numComponents; i++ {
		v := int32(uint32(t.clamped[i]) + uint32(corr[i]))
		if v > t.maxValue {
			v -= t.maxDif
		} else if v < t.minValue {
			v += t.maxDif
		}
		out[i] = v
	}
}

// octahedronToolBox is draco::OctahedronToolBox.
type octahedronToolBox struct {
	quantizationBits  int32
	maxQuantizedValue int32
	maxValue          int32
	dequantScale      float32
	centerValue       int32
}

func (o *octahedronToolBox) setQuantizationBits(q int32) bool {
	if q < 2 || q > 30 {
		return false
	}
	o.quantizationBits = q
	o.maxQuantizedValue = int32((uint32(1) << uint32(q)) - 1)
	o.maxValue = o.maxQuantizedValue - 1
	o.dequantScale = 2 / float32(o.maxValue)
	o.centerValue = o.maxValue / 2
	return true
}

func (o *octahedronToolBox) initialized() bool { return o.quantizationBits != 0 }

func absI32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (o *octahedronToolBox) canonicalize(s, t int32) (int32, int32) {
	mv, cv := o.maxValue, o.centerValue
	switch {
	case (s == 0 && t == 0) || (s == 0 && t == mv) || (s == mv && t == 0):
		s, t = mv, mv
	case s == 0 && t > cv:
		t = cv - (t - cv)
	case s == mv && t < cv:
		t = cv + (cv - t)
	case t == mv && s < cv:
		s = cv + (cv - s)
	case t == 0 && s > cv:
		s = cv - (s - cv)
	}
	return s, t
}

func (o *octahedronToolBox) integerVectorToOctahedral(v [3]int32) (int32, int32) {
	var s, t int32
	if v[0] >= 0 {
		s = v[1] + o.centerValue
		t = v[2] + o.centerValue
	} else {
		if v[1] < 0 {
			s = absI32(v[2])
		} else {
			s = o.maxValue - absI32(v[2])
		}
		if v[2] < 0 {
			t = absI32(v[1])
		} else {
			t = o.maxValue - absI32(v[1])
		}
	}
	return o.canonicalize(s, t)
}

func (o *octahedronToolBox) canonicalizeIntegerVector(v *[3]int32) {
	absSum := int64(absI32(v[0])) + int64(absI32(v[1])) + int64(absI32(v[2]))
	if absSum == 0 {
		v[0] = o.centerValue
		return
	}
	v[0] = int32(int64(v[0]) * int64(o.centerValue) / absSum)
	v[1] = int32(int64(v[1]) * int64(o.centerValue) / absSum)
	if v[2] >= 0 {
		v[2] = o.centerValue - absI32(v[0]) - absI32(v[1])
	} else {
		v[2] = -(o.centerValue - absI32(v[0]) - absI32(v[1]))
	}
}

func (o *octahedronToolBox) isInDiamond(s, t int32) bool {
	return uint32(absI32(s))+uint32(absI32(t)) <= uint32(o.centerValue)
}

func (o *octahedronToolBox) invertDiamond(s, t *int32) {
	var signS, signT int32
	switch {
	case *s >= 0 && *t >= 0:
		signS, signT = 1, 1
	case *s <= 0 && *t <= 0:
		signS, signT = -1, -1
	default:
		signS, signT = -1, -1
		if *s > 0 {
			signS = 1
		}
		if *t > 0 {
			signT = 1
		}
	}
	cornerS := uint32(signS * o.centerValue)
	cornerT := uint32(signT * o.centerValue)
	us := uint32(*s)
	ut := uint32(*t)
	us = us + us - cornerS
	ut = ut + ut - cornerT
	if signS*signT >= 0 {
		us, ut = -ut, -us
	} else {
		us, ut = ut, us
	}
	us += cornerS
	ut += cornerT
	*s = int32(us) / 2
	*t = int32(ut) / 2
}

func (o *octahedronToolBox) modMax(x int32) int32 {
	if x > o.centerValue {
		return x - o.maxQuantizedValue
	}
	if x < -o.centerValue {
		return x + o.maxQuantizedValue
	}
	return x
}

// unitVector is QuantizedOctahedralCoordsToUnitVector; explicit float32 conversions keep every product rounded like the reference.
func (o *octahedronToolBox) unitVector(inS, inT int32) [3]float32 {
	y := float32(float32(inS)*o.dequantScale) - 1
	z := float32(float32(inT)*o.dequantScale) - 1
	x := 1 - float32(math.Abs(float64(y))) - float32(math.Abs(float64(z)))
	xOffset := -x
	if xOffset < 0 {
		xOffset = 0
	}
	if y < 0 {
		y += xOffset
	} else {
		y -= xOffset
	}
	if z < 0 {
		z += xOffset
	} else {
		z -= xOffset
	}
	norm2 := float32(x*x) + float32(y*y) + float32(z*z)
	if norm2 < 1e-6 {
		return [3]float32{}
	}
	d := 1 / float32(math.Sqrt(float64(norm2)))
	return [3]float32{float32(x * d), float32(y * d), float32(z * d)}
}

// octahedronTransform covers both PredictionSchemeNormalOctahedron(Canonicalized)DecodingTransform.
type octahedronTransform struct {
	tb           octahedronToolBox
	canonicalize bool
}

func (t *octahedronTransform) init(int) {}

func (t *octahedronTransform) decodeData(b *buffer) bool {
	maxQ, ok := b.i32()
	if !ok {
		return false
	}
	if t.canonicalize {
		if _, ok := b.i32(); !ok { // center value, unused
			return false
		}
	}
	if maxQ%2 == 0 {
		return false
	}
	q := int32(bits.Len32(uint32(maxQ)))
	return t.tb.setQuantizationBits(q)
}

func (t *octahedronTransform) correctionsPositive() bool { return true }
func (t *octahedronTransform) quantizationBits() int     { return int(t.tb.quantizationBits) }

func rotationCount(p [2]int32) int32 {
	switch {
	case p[0] == 0:
		if p[1] == 0 {
			return 0
		} else if p[1] > 0 {
			return 3
		}
		return 1
	case p[0] > 0:
		if p[1] >= 0 {
			return 2
		}
		return 1
	default:
		if p[1] <= 0 {
			return 0
		}
		return 3
	}
}

func rotatePoint(p [2]int32, n int32) [2]int32 {
	switch n {
	case 1:
		return [2]int32{p[1], -p[0]}
	case 2:
		return [2]int32{-p[0], -p[1]}
	case 3:
		return [2]int32{-p[1], p[0]}
	}
	return p
}

func (t *octahedronTransform) original(pred, corr, out []int32) {
	tb := &t.tb
	c := tb.centerValue
	if t.canonicalize {
		p := [2]int32{pred[0] - c, pred[1] - c}
		inDiamond := tb.isInDiamond(p[0], p[1])
		if !inDiamond {
			tb.invertDiamond(&p[0], &p[1])
		}
		bottomLeft := (p[0] == 0 && p[1] == 0) || (p[0] < 0 && p[1] <= 0)
		rot := rotationCount(p)
		if !bottomLeft {
			p = rotatePoint(p, rot)
		}
		o := [2]int32{tb.modMax(int32(uint32(p[0]) + uint32(corr[0]))), tb.modMax(int32(uint32(p[1]) + uint32(corr[1])))}
		if !bottomLeft {
			o = rotatePoint(o, (4-rot)%4)
		}
		if !inDiamond {
			tb.invertDiamond(&o[0], &o[1])
		}
		out[0] = o[0] + c
		out[1] = o[1] + c
		return
	}
	p := [2]int32{int32(uint32(pred[0]) - uint32(c)), int32(uint32(pred[1]) - uint32(c))}
	inDiamond := tb.isInDiamond(p[0], p[1])
	if !inDiamond {
		tb.invertDiamond(&p[0], &p[1])
	}
	o := [2]int32{int32(uint32(p[0]) + uint32(corr[0])), int32(uint32(p[1]) + uint32(corr[1]))}
	o[0] = tb.modMax(o[0])
	o[1] = tb.modMax(o[1])
	if !inDiamond {
		tb.invertDiamond(&o[0], &o[1])
	}
	out[0] = int32(uint32(o[0]) + uint32(c))
	out[1] = int32(uint32(o[1]) + uint32(c))
}

// predictionScheme is PredictionSchemeTypedDecoderInterface<int32_t>.
type predictionScheme interface {
	usesPositionParent() bool
	setParent(pos *pointAttribute) error
	decodePredictionData(b *buffer) bool
	correctionsPositive() bool
	computeOriginal(data []int32, numComponents int, pointIDs []int32) error
}

// deltaScheme is PredictionSchemeDeltaDecoder.
type deltaScheme struct{ t transform }

func (s *deltaScheme) usesPositionParent() bool            { return false }
func (s *deltaScheme) setParent(*pointAttribute) error     { return errPrediction }
func (s *deltaScheme) decodePredictionData(b *buffer) bool { return s.t.decodeData(b) }
func (s *deltaScheme) correctionsPositive() bool           { return s.t.correctionsPositive() }

func (s *deltaScheme) computeOriginal(data []int32, nc int, _ []int32) error {
	s.t.init(nc)
	if len(data) < nc {
		return errPrediction
	}
	zero := make([]int32, nc)
	s.t.original(zero, data[:nc], data[:nc])
	for i := nc; i+nc <= len(data); i += nc {
		s.t.original(data[i-nc:i], data[i:i+nc], data[i:i+nc])
	}
	return nil
}

// meshData is MeshPredictionSchemeData.
type meshData struct {
	table        connectivity
	vertexToData []int32
	dataToCorner []int32
}

func (m *meshData) entry(c int32) (int, bool) {
	v := m.table.vertex(c)
	if v < 0 || int(v) >= len(m.vertexToData) {
		return 0, false
	}
	return int(m.vertexToData[v]), true
}

func (m *meshData) checkSize(data []int32, nc int) error {
	if nc <= 0 || len(m.dataToCorner)*nc > len(data) {
		return errPrediction
	}
	return nil
}

// parallelogramPrediction is ComputeParallelogramPrediction.
func (m *meshData) parallelogramPrediction(dataID int, ci int32, data []int32, nc int, out []int32) (bool, error) {
	oci := m.table.opposite(ci)
	if oci == invalidIndex {
		return false, nil
	}
	vOpp, ok1 := m.entry(oci)
	vNext, ok2 := m.entry(next(oci))
	vPrev, ok3 := m.entry(previous(oci))
	if !ok1 || !ok2 || !ok3 {
		return false, errPrediction
	}
	if vOpp < dataID && vNext < dataID && vPrev < dataID {
		for c := 0; c < nc; c++ {
			r := int64(data[vNext*nc+c]) + int64(data[vPrev*nc+c]) - int64(data[vOpp*nc+c])
			out[c] = int32(r)
		}
		return true, nil
	}
	return false, nil
}

type parallelogramScheme struct {
	t transform
	m meshData
}

func (s *parallelogramScheme) usesPositionParent() bool            { return false }
func (s *parallelogramScheme) setParent(*pointAttribute) error     { return errPrediction }
func (s *parallelogramScheme) decodePredictionData(b *buffer) bool { return s.t.decodeData(b) }
func (s *parallelogramScheme) correctionsPositive() bool           { return s.t.correctionsPositive() }

func (s *parallelogramScheme) computeOriginal(data []int32, nc int, _ []int32) error {
	s.t.init(nc)
	if err := s.m.checkSize(data, nc); err != nil {
		return err
	}
	pred := make([]int32, nc)
	s.t.original(pred, data[:nc], data[:nc])
	for p := 1; p < len(s.m.dataToCorner); p++ {
		dst := data[p*nc : (p+1)*nc]
		ok, err := s.m.parallelogramPrediction(p, s.m.dataToCorner[p], data, nc, pred)
		if err != nil {
			return err
		}
		if ok {
			s.t.original(pred, dst, dst)
		} else {
			s.t.original(data[(p-1)*nc:p*nc], dst, dst)
		}
	}
	return nil
}

// multiParallelogramScheme is the legacy MeshPredictionSchemeMultiParallelogramDecoder.
type multiParallelogramScheme struct {
	t transform
	m meshData
}

func (s *multiParallelogramScheme) usesPositionParent() bool            { return false }
func (s *multiParallelogramScheme) setParent(*pointAttribute) error     { return errPrediction }
func (s *multiParallelogramScheme) decodePredictionData(b *buffer) bool { return s.t.decodeData(b) }
func (s *multiParallelogramScheme) correctionsPositive() bool           { return s.t.correctionsPositive() }

func (s *multiParallelogramScheme) computeOriginal(data []int32, nc int, _ []int32) error {
	s.t.init(nc)
	if err := s.m.checkSize(data, nc); err != nil {
		return err
	}
	pred := make([]int32, nc)
	para := make([]int32, nc)
	s.t.original(pred, data[:nc], data[:nc])
	limit := s.m.table.numCorners() + 1
	for p := 1; p < len(s.m.dataToCorner); p++ {
		start := s.m.dataToCorner[p]
		corner := start
		count := 0
		clear(pred)
		for steps := 0; corner != invalidIndex; steps++ {
			if steps > limit {
				return errPrediction
			}
			ok, err := s.m.parallelogramPrediction(p, corner, data, nc, para)
			if err != nil {
				return err
			}
			if ok {
				for c := range pred {
					pred[c] = int32(uint32(pred[c]) + uint32(para[c]))
				}
				count++
			}
			corner = swingRight(s.m.table, corner)
			if corner == start {
				corner = invalidIndex
			}
		}
		dst := data[p*nc : (p+1)*nc]
		if count == 0 {
			s.t.original(data[(p-1)*nc:p*nc], dst, dst)
			continue
		}
		for c := range pred {
			pred[c] /= int32(count)
		}
		s.t.original(pred, dst, dst)
	}
	return nil
}

// constrainedScheme is MeshPredictionSchemeConstrainedMultiParallelogramDecoder.
type constrainedScheme struct {
	t      transform
	m      meshData
	crease [maxConstrainedParallelograms][]bool
}

func (s *constrainedScheme) usesPositionParent() bool        { return false }
func (s *constrainedScheme) setParent(*pointAttribute) error { return errPrediction }
func (s *constrainedScheme) correctionsPositive() bool       { return s.t.correctionsPositive() }

func (s *constrainedScheme) decodePredictionData(b *buffer) bool {
	for i := range s.crease {
		n, ok := b.varU32()
		if !ok || uint64(n) > uint64(s.m.table.numCorners()) {
			return false
		}
		if n > 0 {
			s.crease[i] = make([]bool, n)
			var dec bitDecoder
			if !dec.startDecoding(b) {
				return false
			}
			for j := range s.crease[i] {
				s.crease[i][j] = dec.decodeBit()
			}
		}
	}
	return s.t.decodeData(b)
}

func (s *constrainedScheme) computeOriginal(data []int32, nc int, _ []int32) error {
	s.t.init(nc)
	if err := s.m.checkSize(data, nc); err != nil {
		return err
	}
	var preds [maxConstrainedParallelograms][]int32
	for i := range preds {
		preds[i] = make([]int32, nc)
	}
	s.t.original(preds[0], data[:nc], data[:nc])
	var creasePos [maxConstrainedParallelograms]int
	multi := make([]int32, nc)
	limit := s.m.table.numCorners() + 1
	for p := 1; p < len(s.m.dataToCorner); p++ {
		start := s.m.dataToCorner[p]
		corner := start
		count := 0
		firstPass := true
		for steps := 0; corner != invalidIndex; steps++ {
			if steps > limit {
				return errPrediction
			}
			ok, err := s.m.parallelogramPrediction(p, corner, data, nc, preds[count])
			if err != nil {
				return err
			}
			if ok {
				count++
				if count == maxConstrainedParallelograms {
					break
				}
			}
			if firstPass {
				corner = swingLeft(s.m.table, corner)
			} else {
				corner = swingRight(s.m.table, corner)
			}
			if corner == start {
				break
			}
			if corner == invalidIndex && firstPass {
				firstPass = false
				corner = swingRight(s.m.table, start)
			}
		}
		used := 0
		if count > 0 {
			clear(multi)
			for i := 0; i < count; i++ {
				ctx := count - 1
				pos := creasePos[ctx]
				creasePos[ctx]++
				if pos >= len(s.crease[ctx]) {
					return errPrediction
				}
				if !s.crease[ctx][pos] {
					used++
					for j := range multi {
						multi[j] = int32(uint32(multi[j]) + uint32(preds[i][j]))
					}
				}
			}
		}
		dst := data[p*nc : (p+1)*nc]
		if used == 0 {
			s.t.original(data[(p-1)*nc:p*nc], dst, dst)
			continue
		}
		for c := range multi {
			multi[c] /= int32(used)
		}
		s.t.original(multi, dst, dst)
	}
	return nil
}

// positionSource reads quantized parent positions for the predictors that need them.
type positionSource struct {
	pos      *pointAttribute
	pointIDs []int32
}

func (ps *positionSource) forEntry(entry int) ([3]int64, bool) {
	if entry < 0 || entry >= len(ps.pointIDs) {
		return [3]int64{}, false
	}
	port := ps.pos.portable
	idx, ok := ps.pos.valueIndex(int(ps.pointIDs[entry]))
	if !ok || port == nil || idx >= port.numEntries {
		return [3]int64{}, false
	}
	v := port.values[idx*3 : idx*3+3]
	return [3]int64{int64(v[0]), int64(v[1]), int64(v[2])}, true
}

func setPositionParent(dst **pointAttribute, pos *pointAttribute) error {
	if pos == nil || pos.attType != attributeTypePosition || pos.portable == nil || pos.portable.numComponents != positionComponentsForPredicts {
		return errPrediction
	}
	*dst = pos
	return nil
}

// texCoordsScheme is MeshPredictionSchemeTexCoordsPortableDecoder with its predictor.
type texCoordsScheme struct {
	t            transform
	m            meshData
	pos          *pointAttribute
	orientations []bool
}

func (s *texCoordsScheme) usesPositionParent() bool { return true }
func (s *texCoordsScheme) setParent(pos *pointAttribute) error {
	return setPositionParent(&s.pos, pos)
}
func (s *texCoordsScheme) correctionsPositive() bool { return s.t.correctionsPositive() }

func (s *texCoordsScheme) decodePredictionData(b *buffer) bool {
	// One orientation is consumed per predicted entry, so a valid stream never holds more than the table has corners. The remaining byte count is no bound: a skewed rABS stream packs many bits into each byte.
	n, ok := b.i32()
	if !ok || n < 0 || int64(n) > int64(s.m.table.numCorners()) {
		return false
	}
	s.orientations = make([]bool, n)
	last := true
	var dec bitDecoder
	if !dec.startDecoding(b) {
		return false
	}
	for i := range s.orientations {
		if !dec.decodeBit() {
			last = !last
		}
		s.orientations[i] = last
	}
	return s.t.decodeData(b)
}

func (s *texCoordsScheme) computeOriginal(data []int32, nc int, pointIDs []int32) error {
	if nc != 2 || s.pos == nil {
		return errPrediction
	}
	s.t.init(nc)
	if err := s.m.checkSize(data, nc); err != nil {
		return err
	}
	ps := positionSource{pos: s.pos, pointIDs: pointIDs}
	var pred [2]int32
	for p := 0; p < len(s.m.dataToCorner); p++ {
		if err := s.predict(&ps, s.m.dataToCorner[p], data, p, &pred); err != nil {
			return err
		}
		dst := data[p*2 : p*2+2]
		s.t.original(pred[:], dst, dst)
	}
	return nil
}

func (s *texCoordsScheme) predict(ps *positionSource, corner int32, data []int32, dataID int, pred *[2]int32) error {
	nextData, ok1 := s.m.entry(next(corner))
	prevData, ok2 := s.m.entry(previous(corner))
	if !ok1 || !ok2 {
		return errPrediction
	}
	if prevData < dataID && nextData < dataID {
		nUV := [2]int64{int64(data[nextData*2]), int64(data[nextData*2+1])}
		pUV := [2]int64{int64(data[prevData*2]), int64(data[prevData*2+1])}
		if pUV == nUV {
			pred[0], pred[1] = int32(pUV[0]), int32(pUV[1])
			return nil
		}
		tipPos, ok1 := ps.forEntry(dataID)
		nextPos, ok2 := ps.forEntry(nextData)
		prevPos, ok3 := ps.forEntry(prevData)
		if !ok1 || !ok2 || !ok3 {
			return errPrediction
		}
		pn := [3]int64{prevPos[0] - nextPos[0], prevPos[1] - nextPos[1], prevPos[2] - nextPos[2]}
		pnNorm2 := uint64(pn[0]*pn[0] + pn[1]*pn[1] + pn[2]*pn[2])
		if pnNorm2 != 0 {
			cn := [3]int64{tipPos[0] - nextPos[0], tipPos[1] - nextPos[1], tipPos[2] - nextPos[2]}
			cnDotPn := pn[0]*cn[0] + pn[1]*cn[1] + pn[2]*cn[2]
			pnUV := [2]int64{pUV[0] - nUV[0], pUV[1] - nUV[1]}
			nUVAbsMax := max(absI64(nUV[0]), absI64(nUV[1]))
			if uint64(nUVAbsMax) > uint64(math.MaxInt64)/pnNorm2 {
				return errPrediction
			}
			pnUVAbsMax := max(absI64(pnUV[0]), absI64(pnUV[1]))
			if pnUVAbsMax == 0 || absI64(cnDotPn) > math.MaxInt64/pnUVAbsMax {
				return errPrediction
			}
			n2 := int64(pnNorm2)
			xUV := [2]int64{nUV[0]*n2 + cnDotPn*pnUV[0], nUV[1]*n2 + cnDotPn*pnUV[1]}
			pnAbsMax := max(absI64(pn[0]), absI64(pn[1]), absI64(pn[2]))
			if pnAbsMax == 0 || absI64(cnDotPn) > math.MaxInt64/pnAbsMax {
				return errPrediction
			}
			xPos := [3]int64{nextPos[0] + cnDotPn*pn[0]/n2, nextPos[1] + cnDotPn*pn[1]/n2, nextPos[2] + cnDotPn*pn[2]/n2}
			cx := [3]int64{tipPos[0] - xPos[0], tipPos[1] - xPos[1], tipPos[2] - xPos[2]}
			cxNorm2 := uint64(cx[0]*cx[0] + cx[1]*cx[1] + cx[2]*cx[2])
			cxUV := [2]int64{pnUV[1], -pnUV[0]}
			normSquared := int64(intSqrt(cxNorm2 * pnNorm2))
			cxUV[0] *= normSquared
			cxUV[1] *= normSquared
			if len(s.orientations) == 0 {
				return errPrediction
			}
			orientation := s.orientations[len(s.orientations)-1]
			s.orientations = s.orientations[:len(s.orientations)-1]
			var uv [2]int64
			if orientation {
				uv = [2]int64{int64(uint64(xUV[0])+uint64(cxUV[0])) / n2, int64(uint64(xUV[1])+uint64(cxUV[1])) / n2}
			} else {
				uv = [2]int64{int64(uint64(xUV[0])-uint64(cxUV[0])) / n2, int64(uint64(xUV[1])-uint64(cxUV[1])) / n2}
			}
			pred[0], pred[1] = int32(uv[0]), int32(uv[1])
			return nil
		}
	}
	offset := 0
	if prevData < dataID {
		offset = prevData * 2
	}
	if nextData < dataID {
		offset = nextData * 2
	} else {
		if dataID > 0 {
			offset = (dataID - 1) * 2
		} else {
			pred[0], pred[1] = 0, 0
			return nil
		}
	}
	if offset < 0 || offset+2 > len(data) {
		return errPrediction
	}
	pred[0], pred[1] = data[offset], data[offset+1]
	return nil
}

// intSqrt is draco::IntSqrt.
func intSqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	act := n
	root := uint64(1)
	for act >= 2 {
		root *= 2
		act /= 4
	}
	for {
		root = (root + n/root) / 2
		if root*root <= n {
			break
		}
	}
	return root
}

// geometricNormalScheme is MeshPredictionSchemeGeometricNormalDecoder with the triangle-area predictor.
type geometricNormalScheme struct {
	t    *octahedronTransform
	m    meshData
	pos  *pointAttribute
	tb   octahedronToolBox
	flip bitDecoder
}

func (s *geometricNormalScheme) usesPositionParent() bool { return true }
func (s *geometricNormalScheme) setParent(pos *pointAttribute) error {
	return setPositionParent(&s.pos, pos)
}
func (s *geometricNormalScheme) correctionsPositive() bool { return s.t.correctionsPositive() }

func (s *geometricNormalScheme) decodePredictionData(b *buffer) bool {
	if !s.t.decodeData(b) {
		return false
	}
	return s.flip.startDecoding(b)
}

func (s *geometricNormalScheme) computeOriginal(data []int32, nc int, pointIDs []int32) error {
	if nc != 2 || s.pos == nil {
		return errPrediction
	}
	if !s.tb.setQuantizationBits(int32(s.t.quantizationBits())) {
		return errPrediction
	}
	if err := s.m.checkSize(data, nc); err != nil {
		return err
	}
	ps := positionSource{pos: s.pos, pointIDs: pointIDs}
	limit := s.m.table.numCorners() + 1
	var octPred [2]int32
	for dataID := 0; dataID < len(s.m.dataToCorner); dataID++ {
		normal, err := s.predict(&ps, s.m.dataToCorner[dataID], limit)
		if err != nil {
			return err
		}
		s.tb.canonicalizeIntegerVector(&normal)
		if s.flip.decodeBit() {
			normal = [3]int32{-normal[0], -normal[1], -normal[2]}
		}
		octPred[0], octPred[1] = s.tb.integerVectorToOctahedral(normal)
		dst := data[dataID*2 : dataID*2+2]
		s.t.original(octPred[:], dst, dst)
	}
	return nil
}

func (s *geometricNormalScheme) positionForCorner(ps *positionSource, c int32) ([3]int64, bool) {
	entry, ok := s.m.entry(c)
	if !ok {
		return [3]int64{}, false
	}
	return ps.forEntry(entry)
}

func (s *geometricNormalScheme) predict(ps *positionSource, corner int32, limit int) ([3]int32, error) {
	center, ok := s.positionForCorner(ps, corner)
	if !ok {
		return [3]int32{}, errPrediction
	}
	var normal [3]int64
	failed := false
	walked := forEachVertexCorner(s.m.table, corner, limit, func(c int32) bool {
		pNext, ok1 := s.positionForCorner(ps, next(c))
		pPrev, ok2 := s.positionForCorner(ps, previous(c))
		if !ok1 || !ok2 {
			failed = true
			return false
		}
		dn := [3]int64{pNext[0] - center[0], pNext[1] - center[1], pNext[2] - center[2]}
		dp := [3]int64{pPrev[0] - center[0], pPrev[1] - center[1], pPrev[2] - center[2]}
		cross := [3]int64{dn[1]*dp[2] - dn[2]*dp[1], dn[2]*dp[0] - dn[0]*dp[2], dn[0]*dp[1] - dn[1]*dp[0]}
		for i := range normal {
			normal[i] = int64(uint64(normal[i]) + uint64(cross[i]))
		}
		return true
	})
	if failed || !walked {
		return [3]int32{}, errPrediction
	}
	const upperBound = int64(1) << 29
	absSum := int64(0)
	for _, v := range normal {
		a := absI64(v)
		if absSum > math.MaxInt64-a {
			absSum = math.MaxInt64
			break
		}
		absSum += a
	}
	if absSum > upperBound {
		q := absSum / upperBound
		for i := range normal {
			normal[i] /= q
		}
	}
	return [3]int32{int32(normal[0]), int32(normal[1]), int32(normal[2])}, nil
}
