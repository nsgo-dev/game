// Package draco decodes Draco-compressed triangle meshes (bitstream version 2.2, the version KHR_draco_mesh_compression normatively references) in pure Go.
//
// It is a port of the decoding half of Google's Draco library (Apache-2.0): sequential and edgebreaker (standard and valence) connectivity, the generic, integer, quantization and normal attribute decoders, and the difference, parallelogram, multi-parallelogram, constrained multi-parallelogram, portable texture coordinate and geometric normal prediction schemes with their wrap and octahedron transforms. Every read is bounds checked so malformed input returns an error instead of panicking.
package draco

import (
	"bytes"
	"errors"
	"fmt"
)

// Limits caps the sizes a stream may declare, so a small malicious input cannot force large allocations.
type Limits struct {
	// MaxFaces is the largest triangle count accepted.
	MaxFaces int
	// MaxPoints is the largest point (vertex) count accepted.
	MaxPoints int
}

// DefaultLimits are used when a Limits field is zero or negative.
var DefaultLimits = Limits{MaxFaces: 1 << 22, MaxPoints: 1 << 22}

// Mesh is a decoded Draco triangle mesh. Faces index points; every attribute has one value per point.
type Mesh struct {
	// Faces holds three point indices per triangle.
	Faces []uint32
	// NumPoints is the number of points the faces index.
	NumPoints int

	attributes []*pointAttribute
}

// Attribute is one decoded per-point attribute of a Mesh.
type Attribute struct {
	a *pointAttribute
}

// Attribute returns the attribute with the given unique id, which is what KHR_draco_mesh_compression's attributes map refers to.
func (m *Mesh) Attribute(uniqueID uint32) (Attribute, bool) {
	for _, a := range m.attributes {
		if a.uniqueID == uniqueID {
			return Attribute{a: a}, true
		}
	}
	return Attribute{}, false
}

// NumComponents returns the number of components per value.
func (a Attribute) NumComponents() int { return a.a.numComponents }

// IsFloat reports whether the attribute stores floating-point values.
func (a Attribute) IsFloat() bool { return a.a.dataType == dtFloat32 || a.a.dataType == dtFloat64 }

// Normalized reports whether integer values are meant to be normalized.
func (a Attribute) Normalized() bool { return a.a.normalized }

// Value returns component comp of the value of point. Integer values are returned exactly; float32 values are widened exactly.
func (a Attribute) Value(point, comp int) (float64, error) {
	idx, ok := a.a.valueIndex(point)
	if !ok || comp < 0 || comp >= a.a.numComponents {
		return 0, fmt.Errorf("draco: point %d has no value for attribute %d", point, a.a.uniqueID)
	}
	return a.a.values[idx*a.a.numComponents+comp], nil
}

type decoder struct {
	buf          *buffer
	limits       Limits
	faces        []uint32
	numPoints    int
	attributes   []*pointAttribute
	attrDecoders []*attributesDecoder
	eb           *edgebreaker
}

const (
	encoderPointCloud      = 0
	encoderTriangularMesh  = 1
	methodSequential       = 0
	methodEdgebreaker      = 1
	metadataFlagMask       = 0x8000
	supportedVersionMajor  = 2
	supportedVersionMinor  = 2
	maxMetadataNesting     = 1000
	maxAttributesDecoders  = 255
	connectivityCompressed = 0
)

var errHeader = errors.New("draco: invalid header")

// Decode decodes a Draco triangle mesh. Zero limit fields take DefaultLimits.
func Decode(data []byte, limits Limits) (*Mesh, error) {
	if limits.MaxFaces <= 0 {
		limits.MaxFaces = DefaultLimits.MaxFaces
	}
	if limits.MaxPoints <= 0 {
		limits.MaxPoints = DefaultLimits.MaxPoints
	}
	b := newBuffer(data)
	d := &decoder{buf: &b, limits: limits}
	magic, ok := b.bytes(5)
	if !ok || !bytes.Equal(magic, []byte("DRACO")) {
		return nil, errors.New("draco: not a Draco stream")
	}
	major, ok1 := b.u8()
	minor, ok2 := b.u8()
	encoderType, ok3 := b.u8()
	method, ok4 := b.u8()
	flags, ok5 := b.u16()
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return nil, errHeader
	}
	if major != supportedVersionMajor || minor != supportedVersionMinor {
		return nil, fmt.Errorf("draco: bitstream version %d.%d is not supported (want %d.%d)", major, minor, supportedVersionMajor, supportedVersionMinor)
	}
	if encoderType != encoderTriangularMesh {
		return nil, fmt.Errorf("draco: geometry type %d is not a triangular mesh", encoderType)
	}
	if flags&metadataFlagMask != 0 {
		if err := skipGeometryMetadata(&b); err != nil {
			return nil, err
		}
	}
	switch method {
	case methodSequential:
		if err := d.decodeSequentialConnectivity(); err != nil {
			return nil, err
		}
	case methodEdgebreaker:
		traversal, ok := b.u8()
		if !ok {
			return nil, errHeader
		}
		eb, err := newEdgebreaker(d, traversal)
		if err != nil {
			return nil, err
		}
		d.eb = eb
		if err := eb.decodeConnectivity(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("draco: unknown mesh encoding method %d", method)
	}
	if d.numPoints > limits.MaxPoints {
		return nil, fmt.Errorf("draco: %d points exceed the limit of %d", d.numPoints, limits.MaxPoints)
	}
	if err := d.decodeAttributes(); err != nil {
		return nil, err
	}
	for _, p := range d.faces {
		if int(p) >= d.numPoints {
			return nil, errors.New("draco: face references a missing point")
		}
	}
	return &Mesh{Faces: d.faces, NumPoints: d.numPoints, attributes: d.attributes}, nil
}

func skipGeometryMetadata(b *buffer) error {
	errMeta := errors.New("draco: invalid metadata")
	numAttMetadata, ok := b.varU32()
	if !ok {
		return errMeta
	}
	for i := uint32(0); i < numAttMetadata; i++ {
		if _, ok := b.varU32(); !ok {
			return errMeta
		}
		if err := skipMetadata(b); err != nil {
			return err
		}
	}
	return skipMetadata(b)
}

func skipMetadata(b *buffer) error {
	errMeta := errors.New("draco: invalid metadata")
	type item struct {
		level  int
		parent bool
	}
	stack := []item{{}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if it.parent {
			if it.level > maxMetadataNesting {
				return errMeta
			}
			if !skipName(b) {
				return errMeta
			}
		}
		numEntries, ok := b.varU32()
		if !ok {
			return errMeta
		}
		for i := uint32(0); i < numEntries; i++ {
			if !skipName(b) {
				return errMeta
			}
			size, ok := b.varU32()
			if !ok || size == 0 || !b.advance(int(size)) {
				return errMeta
			}
		}
		numSub, ok := b.varU32()
		if !ok || uint64(numSub) > uint64(b.remaining()) {
			return errMeta
		}
		level := it.level
		if it.parent {
			level++
		}
		for i := uint32(0); i < numSub; i++ {
			stack = append(stack, item{level: level, parent: true})
		}
	}
	return nil
}

func skipName(b *buffer) bool {
	n, ok := b.u8()
	return ok && b.advance(int(n))
}

// decodeSequentialConnectivity is MeshSequentialDecoder::DecodeConnectivity.
func (d *decoder) decodeSequentialConnectivity() error {
	b := d.buf
	errSeq := errors.New("draco: invalid sequential connectivity")
	numFaces, ok1 := b.varU32()
	numPoints, ok2 := b.varU32()
	if !ok1 || !ok2 {
		return errSeq
	}
	if uint64(numFaces) > uint64(d.limits.MaxFaces) {
		return fmt.Errorf("draco: %d faces exceed the limit of %d", numFaces, d.limits.MaxFaces)
	}
	if uint64(numPoints) > uint64(d.limits.MaxPoints) {
		return fmt.Errorf("draco: %d points exceed the limit of %d", numPoints, d.limits.MaxPoints)
	}
	if uint64(numFaces) > uint64(b.remaining())/3 {
		return errSeq
	}
	method, ok := b.u8()
	if !ok {
		return errSeq
	}
	n := 3 * int(numFaces)
	d.faces = make([]uint32, n)
	if method == connectivityCompressed {
		symbols := make([]uint32, n)
		if !decodeSymbols(n, 1, b, symbols) {
			return errSeq
		}
		last := int64(0)
		for i, s := range symbols {
			diff := int64(s >> 1)
			if s&1 != 0 {
				if diff > last {
					return errSeq
				}
				diff = -diff
			} else if diff > (1<<31-1)-last {
				return errSeq
			}
			last += diff
			d.faces[i] = uint32(last)
		}
	} else {
		for i := range d.faces {
			var v uint32
			var ok bool
			switch {
			case numPoints < 256:
				var c uint8
				c, ok = b.u8()
				v = uint32(c)
			case numPoints < 1<<16:
				var c uint16
				c, ok = b.u16()
				v = uint32(c)
			case numPoints < 1<<21:
				v, ok = b.varU32()
			default:
				v, ok = b.u32()
			}
			if !ok {
				return errSeq
			}
			d.faces[i] = v
		}
	}
	d.numPoints = int(numPoints)
	return nil
}

// decodeAttributes is PointCloudDecoder::DecodePointAttributes.
func (d *decoder) decodeAttributes() error {
	b := d.buf
	num, ok := b.u8()
	if !ok {
		return errAttribute
	}
	d.attrDecoders = make([]*attributesDecoder, num)
	for i := range d.attrDecoders {
		var seq sequencer
		if d.eb != nil {
			var err error
			if seq, err = d.eb.createAttributesDecoder(i); err != nil {
				return err
			}
		} else {
			seq = &linearSequencer{numPoints: d.numPoints}
		}
		d.attrDecoders[i] = &attributesDecoder{seq: seq}
	}
	for _, ad := range d.attrDecoders {
		if err := ad.decodeHeader(d); err != nil {
			return err
		}
	}
	for _, ad := range d.attrDecoders {
		if err := ad.decode(d); err != nil {
			return err
		}
	}
	return nil
}

// namedAttribute is GetNamedAttributeId followed by GetPortableAttribute's lookup: the first attribute of the type.
func (d *decoder) namedAttribute(attType uint8) *pointAttribute {
	for _, a := range d.attributes {
		if a.attType == attType {
			return a
		}
	}
	return nil
}

// createPredictionScheme is CreateIntPredictionScheme followed by CreatePredictionSchemeForDecoder. A nil scheme means the values carry no prediction.
func (d *decoder) createPredictionScheme(method, transformType, attID int, isNormal bool) (predictionScheme, error) {
	var t transform
	switch {
	case isNormal && transformType == transformOctahedron:
		t = &octahedronTransform{}
	case isNormal && transformType == transformOctahedronCanonical:
		t = &octahedronTransform{canonicalize: true}
	case !isNormal && transformType == transformWrap:
		t = &wrapTransform{}
	default:
		return nil, nil
	}
	if d.eb != nil && method >= predictionParallelogram {
		table, enc := d.eb.attributeConnectivity(attID)
		m := meshData{table: table, vertexToData: enc.vertexToEncoded, dataToCorner: enc.encodedToCorner}
		oct, isOct := t.(*octahedronTransform)
		if isOct {
			if method == predictionGeometricNormal {
				return &geometricNormalScheme{t: oct, m: m}, nil
			}
		} else {
			switch method {
			case predictionParallelogram:
				return &parallelogramScheme{t: t, m: m}, nil
			case predictionMultiParallelogram:
				return &multiParallelogramScheme{t: t, m: m}, nil
			case predictionConstrainedMultiParallelog:
				return &constrainedScheme{t: t, m: m}, nil
			case predictionTexCoordsPortable:
				return &texCoordsScheme{t: t, m: m}, nil
			case predictionTexCoordsDeprecated:
				return nil, errors.New("draco: deprecated texture coordinate prediction is not supported")
			case predictionGeometricNormal:
				return nil, errors.New("draco: geometric normal prediction requires an octahedron transform")
			}
		}
	}
	return &deltaScheme{t: t}, nil
}
