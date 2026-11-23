package gltf

import (
	"encoding/json"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/gltf/internal/meshopt"
)

// The two names of the meshopt buffer view compression extension. KHR_meshopt_compression is the ratified successor of EXT_meshopt_compression: the same JSON with a newer attribute codec version and the COLOR filter.
const (
	extMeshoptEXT = "EXT_meshopt_compression"
	extMeshoptKHR = "KHR_meshopt_compression"
)

type rawMeshoptBufferView struct {
	Buffer     *int   `json:"buffer"`
	ByteOffset int    `json:"byteOffset"`
	ByteLength *int   `json:"byteLength"`
	ByteStride *int   `json:"byteStride"`
	Count      *int   `json:"count"`
	Mode       string `json:"mode"`
	Filter     string `json:"filter"`
}

type rawMeshoptBuffer struct {
	Fallback bool `json:"fallback"`
}

// meshoptView is a validated compressed bufferView: the source byte range and how to decode it.
type meshoptView struct {
	ext    string
	source []byte
	stride int
	count  int
	mode   string
	filter string
}

// meshoptExtension returns the name and payload of the meshopt extension on a bufferView or buffer, preferring the KHR name when both are present.
func meshoptExtension(exts map[string]json.RawMessage) (string, json.RawMessage, bool) {
	for _, name := range []string{extMeshoptKHR, extMeshoptEXT} {
		if payload, ok := exts[name]; ok {
			return name, payload, true
		}
	}
	return "", nil, false
}

// meshoptTargets returns, for each buffer index, whether a meshopt-compressed bufferView decodes into it. Malformed payloads are reported later by decodeMeshoptViews, so they are skipped here.
func meshoptTargets(views []rawBufferView) map[int]bool {
	targets := make(map[int]bool)
	for _, rv := range views {
		if _, _, ok := meshoptExtension(rv.Extensions); ok {
			targets[rv.Buffer] = true
		}
	}
	return targets
}

// isMeshoptFallback reports whether a buffer is tagged as a meshopt fallback buffer, which a loader that decodes the extension never needs to fetch.
func isMeshoptFallback(rb rawBuffer) (bool, error) {
	name, payload, ok := meshoptExtension(rb.Extensions)
	if !ok {
		return false, nil
	}
	var ext rawMeshoptBuffer
	if err := json.Unmarshal(payload, &ext); err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return ext.Fallback, nil
}

// decodeMeshoptViews decompresses every meshopt-compressed bufferView into the bytes of the buffer that bufferView names, so accessors, sparse data and images read the decoded data unchanged. Buffers are copied before the first write so the caller's input is never modified.
func (d *Document) decodeMeshoptViews(raws []rawBufferView, placeholders map[int]bool, decodedBytes *uint64) error {
	cloned := make(map[int]bool)
	for i, rv := range raws {
		_, _, ok := meshoptExtension(rv.Extensions)
		if !ok {
			if placeholders[rv.Buffer] {
				return fmt.Errorf("gltf: bufferView %d reads meshopt fallback buffer %d without decompressing it", i, rv.Buffer)
			}
			continue
		}
		view, err := d.parseMeshoptView(i, rv, placeholders)
		if err != nil {
			return err
		}
		if err := chargeDecodedBytes(decodedBytes, uint64(rv.ByteLength)); err != nil {
			return err
		}
		if !placeholders[rv.Buffer] && !cloned[rv.Buffer] {
			buf := &d.Buffers[rv.Buffer]
			buf.Data = append([]byte(nil), buf.Data...)
			cloned[rv.Buffer] = true
		}
		dst := d.Buffers[rv.Buffer].Data[rv.ByteOffset : rv.ByteOffset+rv.ByteLength]
		if err := view.decode(dst); err != nil {
			return fmt.Errorf("gltf: bufferView %d %s: %w", i, view.ext, err)
		}
	}
	return nil
}

func (d *Document) parseMeshoptView(i int, rv rawBufferView, placeholders map[int]bool) (meshoptView, error) {
	name, payload, _ := meshoptExtension(rv.Extensions)
	var ext rawMeshoptBufferView
	if err := json.Unmarshal(payload, &ext); err != nil {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d parse %s: %w", i, name, err)
	}
	if ext.Buffer == nil || ext.ByteLength == nil || ext.ByteStride == nil || ext.Count == nil || ext.Mode == "" {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s needs buffer, byteLength, byteStride, count and mode", i, name)
	}
	src, length, stride, count := *ext.Buffer, *ext.ByteLength, *ext.ByteStride, *ext.Count
	if src < 0 || src >= len(d.Buffers) {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s references invalid buffer %d", i, name, src)
	}
	if placeholders[src] {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s reads compressed data from fallback buffer %d", i, name, src)
	}
	if ext.ByteOffset < 0 || length < 0 || stride <= 0 || count < 0 {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s has a negative offset, length or count, or a non-positive byteStride", i, name)
	}
	srcData := d.Buffers[src].Data
	if uint64(ext.ByteOffset) > uint64(len(srcData)) || uint64(length) > uint64(len(srcData))-uint64(ext.ByteOffset) {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s exceeds buffer %d bounds", i, name, src)
	}
	if uint64(stride)*uint64(count) != uint64(rv.ByteLength) {
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d byteLength %d is not %s byteStride %d times count %d", i, rv.ByteLength, name, stride, count)
	}
	filter := ext.Filter
	if filter == "" {
		filter = "NONE"
	}
	switch ext.Mode {
	case "ATTRIBUTES":
		if stride%4 != 0 || stride > 256 {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s ATTRIBUTES byteStride %d is not a multiple of 4 between 4 and 256", i, name, stride)
		}
	case "TRIANGLES", "INDICES":
		if stride != 2 && stride != 4 {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s %s byteStride %d is not 2 or 4", i, name, ext.Mode, stride)
		}
		if ext.Mode == "TRIANGLES" && count%3 != 0 {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s TRIANGLES count %d is not a multiple of 3", i, name, count)
		}
		if filter != "NONE" {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s %s does not take filter %q", i, name, ext.Mode, filter)
		}
	default:
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s has unknown mode %q", i, name, ext.Mode)
	}
	switch filter {
	case "NONE":
	case "OCTAHEDRAL":
		if stride != 4 && stride != 8 {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s OCTAHEDRAL byteStride %d is not 4 or 8", i, name, stride)
		}
	case "QUATERNION":
		if stride != 8 {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s QUATERNION byteStride %d is not 8", i, name, stride)
		}
	case "EXPONENTIAL":
		// ATTRIBUTES already requires a multiple of 4.
	case "COLOR":
		if name == extMeshoptEXT {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d filter COLOR needs %s, not %s", i, extMeshoptKHR, extMeshoptEXT)
		}
		if stride != 4 && stride != 8 {
			return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s COLOR byteStride %d is not 4 or 8", i, name, stride)
		}
	default:
		return meshoptView{}, fmt.Errorf("gltf: bufferView %d %s has unknown filter %q", i, name, filter)
	}
	return meshoptView{
		ext:    name,
		source: srcData[ext.ByteOffset : ext.ByteOffset+length],
		stride: stride,
		count:  count,
		mode:   ext.Mode,
		filter: filter,
	}, nil
}

func (v meshoptView) decode(dst []byte) error {
	switch v.mode {
	case "TRIANGLES":
		return meshopt.DecodeIndexBuffer(dst, v.count, v.stride, v.source)
	case "INDICES":
		return meshopt.DecodeIndexSequence(dst, v.count, v.stride, v.source)
	}
	// EXT_meshopt_compression defines only version 0 of the attribute codec; version 1 came with KHR_meshopt_compression.
	if v.ext == extMeshoptEXT && len(v.source) > 0 && v.source[0]&0x0f != 0 {
		return fmt.Errorf("attribute codec version %d needs %s, not %s", v.source[0]&0x0f, extMeshoptKHR, extMeshoptEXT)
	}
	if err := meshopt.DecodeVertexBuffer(dst, v.count, v.stride, v.source); err != nil {
		return err
	}
	switch v.filter {
	case "OCTAHEDRAL":
		return meshopt.DecodeFilterOct(dst, v.count, v.stride)
	case "QUATERNION":
		return meshopt.DecodeFilterQuat(dst, v.count, v.stride)
	case "EXPONENTIAL":
		return meshopt.DecodeFilterExp(dst, v.count, v.stride)
	case "COLOR":
		return meshopt.DecodeFilterColor(dst, v.count, v.stride)
	}
	return nil
}
