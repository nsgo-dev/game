package gltf

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/nsgo-dev/game/pkg/gltf/internal/draco"
)

const extDraco = "KHR_draco_mesh_compression"

type rawDracoPrimitive struct {
	BufferView *int              `json:"bufferView"`
	Attributes map[string]uint32 `json:"attributes"`
}

// decodeDracoPrimitive decompresses the KHR_draco_mesh_compression payload of a primitive and fills the accessors its attributes and indices name with the decoded data, so the ordinary accessor readers return it. Per the extension, those accessors' bufferView and byteOffset are ignored. A primitive whose mode is TRIANGLE_STRIP is decoded as the triangle list Draco stores, and the returned mode says so.
func (d *Document) decodeDracoPrimitive(mi, pi int, rp rawPrimitive, mode PrimitiveMode, decodedBytes *uint64) (PrimitiveMode, error) {
	var ext rawDracoPrimitive
	if err := json.Unmarshal(rp.Extensions[extDraco], &ext); err != nil {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d parse %s: %w", mi, pi, extDraco, err)
	}
	if mode != ModeTriangles && mode != ModeTriangleStrip {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s needs mode TRIANGLES or TRIANGLE_STRIP, got %d", mi, pi, extDraco, mode)
	}
	if ext.BufferView == nil || *ext.BufferView < 0 || *ext.BufferView >= len(d.BufferViews) {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s has no valid bufferView", mi, pi, extDraco)
	}
	if len(ext.Attributes) == 0 {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s lists no attributes", mi, pi, extDraco)
	}
	if rp.Indices == nil {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s needs an indices accessor", mi, pi, extDraco)
	}
	idxAcc := *rp.Indices
	if idxAcc < 0 || idxAcc >= len(d.Accessors) {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d invalid indices accessor %d", mi, pi, idxAcc)
	}

	// Decode attributes in name order so errors are deterministic.
	names := make([]string, 0, len(ext.Attributes))
	for name := range ext.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)

	// The accessors declare how much data the stream must produce; bounding the decoder by them keeps a small stream from allocating more than the document admits to.
	numPoints := -1
	for _, name := range names {
		accIdx, ok := rp.Attributes[name]
		if !ok {
			return mode, fmt.Errorf("gltf: mesh %d primitive %d %s attribute %q is not an attribute of the primitive", mi, pi, extDraco, name)
		}
		if accIdx < 0 || accIdx >= len(d.Accessors) {
			return mode, fmt.Errorf("gltf: mesh %d primitive %d attribute %q invalid accessor %d", mi, pi, name, accIdx)
		}
		acc := d.Accessors[accIdx]
		if numPoints >= 0 && acc.Count != numPoints {
			return mode, fmt.Errorf("gltf: mesh %d primitive %d %s attribute accessors disagree on the vertex count", mi, pi, extDraco)
		}
		numPoints = acc.Count
	}
	numIndices := d.Accessors[idxAcc].Count
	if numIndices%3 != 0 {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s indices count %d is not a multiple of 3", mi, pi, extDraco, numIndices)
	}
	if numPoints <= 0 || numIndices <= 0 {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s accessors declare no data", mi, pi, extDraco)
	}

	// Charge the decoder's working memory (corner tables, attribute values and mappings, roughly 32 bytes per index and per vertex) before decoding, since the counts come from the document.
	if err := chargeDecodedBytes(decodedBytes, 32*(uint64(numIndices)+uint64(numPoints))); err != nil {
		return mode, err
	}
	bv := d.BufferViews[*ext.BufferView]
	data := d.Buffers[bv.Buffer].Data[bv.ByteOffset : bv.ByteOffset+bv.ByteLength]
	// The decoder charges what scales with the attributes the stream declares against what is left of the per-load budget, then reports it to be charged here.
	remaining := maxLoadDecodedBytes - *decodedBytes
	if remaining == 0 {
		return mode, errors.New("gltf: decoded geometry exceeds safety limit")
	}
	mesh, err := draco.Decode(data, draco.Limits{MaxFaces: numIndices / 3, MaxPoints: numPoints, MaxBytes: remaining})
	if err != nil {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s: %w", mi, pi, extDraco, err)
	}
	if err := chargeDecodedBytes(decodedBytes, mesh.Bytes); err != nil {
		return mode, err
	}
	if mesh.NumPoints != numPoints || len(mesh.Faces) != numIndices {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s decoded %d vertices and %d indices, accessors declare %d and %d", mi, pi, extDraco, mesh.NumPoints, len(mesh.Faces), numPoints, numIndices)
	}

	for _, name := range names {
		accIdx := rp.Attributes[name]
		attr, ok := mesh.Attribute(ext.Attributes[name])
		if !ok {
			return mode, fmt.Errorf("gltf: mesh %d primitive %d %s has no attribute with id %d for %q", mi, pi, extDraco, ext.Attributes[name], name)
		}
		if err := d.fillDracoAccessor(accIdx, attr, decodedBytes); err != nil {
			return mode, fmt.Errorf("gltf: mesh %d primitive %d %s attribute %q: %w", mi, pi, extDraco, name, err)
		}
	}
	if err := d.fillDracoIndices(idxAcc, mesh.Faces, decodedBytes); err != nil {
		return mode, fmt.Errorf("gltf: mesh %d primitive %d %s indices: %w", mi, pi, extDraco, err)
	}
	return ModeTriangles, nil
}

// fillDracoAccessor stores attr in the accessor's own component type, converting each value the way Draco's glTF decoders do (a plain numeric conversion) and rejecting values the type cannot hold. A floating-point attribute needs a FLOAT accessor: Draco dequantizes it to floats, and truncating those into an integer accessor, normalized or not, would silently lose them.
func (d *Document) fillDracoAccessor(accIdx int, attr draco.Attribute, decodedBytes *uint64) error {
	acc := &d.Accessors[accIdx]
	nComp := acc.Type.Components()
	compSize := acc.ComponentType.ByteSize()
	if compSize == 0 || nComp == 0 || acc.Type == AccessorMat2 || acc.Type == AccessorMat3 {
		return fmt.Errorf("accessor %d has an unsupported type %s/%d", accIdx, acc.Type, acc.ComponentType)
	}
	if attr.IsFloat() && acc.ComponentType != ComponentFloat {
		return fmt.Errorf("decoded floating-point values, accessor %d has integer componentType %d", accIdx, acc.ComponentType)
	}
	if attr.NumComponents() != nComp {
		return fmt.Errorf("decoded %d components per vertex, accessor %d is %s", attr.NumComponents(), accIdx, acc.Type)
	}
	elemSize := nComp * compSize
	if err := chargeDecodedBytes(decodedBytes, uint64(acc.Count)*uint64(elemSize)); err != nil {
		return err
	}
	out := make([]byte, acc.Count*elemSize)
	for p := range acc.Count {
		for c := range nComp {
			v, err := attr.Value(p, c)
			if err != nil {
				return err
			}
			if err := putComponent(out[p*elemSize+c*compSize:], acc.ComponentType, v); err != nil {
				return fmt.Errorf("vertex %d: %w", p, err)
			}
		}
	}
	acc.decoded = out
	return nil
}

func (d *Document) fillDracoIndices(accIdx int, faces []uint32, decodedBytes *uint64) error {
	acc := &d.Accessors[accIdx]
	if acc.Type != AccessorScalar {
		return fmt.Errorf("accessor %d is %s, not SCALAR", accIdx, acc.Type)
	}
	switch acc.ComponentType {
	case ComponentUnsignedByte, ComponentUnsignedShort, ComponentUnsignedInt:
	default:
		return fmt.Errorf("accessor %d has componentType %d, not an unsigned integer", accIdx, acc.ComponentType)
	}
	compSize := acc.ComponentType.ByteSize()
	if err := chargeDecodedBytes(decodedBytes, uint64(len(faces))*uint64(compSize)); err != nil {
		return err
	}
	out := make([]byte, len(faces)*compSize)
	for i, f := range faces {
		if err := putComponent(out[i*compSize:], acc.ComponentType, float64(f)); err != nil {
			return fmt.Errorf("index %d: %w", i, err)
		}
	}
	acc.decoded = out
	return nil
}

// putComponent writes v as one component of type ct. Integer types take v truncated toward zero, as a C++ static_cast does, and reject values outside their range; callers pass integer values to them.
func putComponent(dst []byte, ct ComponentType, v float64) error {
	if ct == ComponentFloat {
		binary.LittleEndian.PutUint32(dst, math.Float32bits(float32(v)))
		return nil
	}
	if math.IsNaN(v) {
		return fmt.Errorf("value NaN does not fit componentType %d", ct)
	}
	t := math.Trunc(v)
	var lo, hi float64
	switch ct {
	case ComponentByte:
		lo, hi = math.MinInt8, math.MaxInt8
	case ComponentUnsignedByte:
		lo, hi = 0, math.MaxUint8
	case ComponentShort:
		lo, hi = math.MinInt16, math.MaxInt16
	case ComponentUnsignedShort:
		lo, hi = 0, math.MaxUint16
	case ComponentUnsignedInt:
		lo, hi = 0, math.MaxUint32
	default:
		return fmt.Errorf("unsupported componentType %d", ct)
	}
	if t < lo || t > hi {
		return fmt.Errorf("value %v does not fit componentType %d", v, ct)
	}
	switch ct {
	case ComponentByte, ComponentUnsignedByte:
		dst[0] = byte(int64(t))
	case ComponentShort, ComponentUnsignedShort:
		binary.LittleEndian.PutUint16(dst, uint16(int64(t)))
	case ComponentUnsignedInt:
		binary.LittleEndian.PutUint32(dst, uint32(int64(t)))
	}
	return nil
}
