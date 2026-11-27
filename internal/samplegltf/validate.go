package samplegltf

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// UnpackGLB parses and validates a binary glTF 2.0 container (header, chunks,
// padding, and JSON/BIN payloads).
func UnpackGLB(data []byte) (*Document, []byte, error) {
	if len(data) < 12 {
		return nil, nil, fmt.Errorf("glb too short: %d bytes", len(data))
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	version := binary.LittleEndian.Uint32(data[4:8])
	length := binary.LittleEndian.Uint32(data[8:12])
	if magic != GLBMagic {
		return nil, nil, fmt.Errorf("bad glb magic: 0x%08x", magic)
	}
	if version != GLBVersion {
		return nil, nil, fmt.Errorf("bad glb version: %d", version)
	}
	if int(length) != len(data) {
		return nil, nil, fmt.Errorf("glb length header %d != file length %d", length, len(data))
	}
	if length%4 != 0 {
		return nil, nil, fmt.Errorf("glb length %d is not 4-byte aligned", length)
	}

	offset := 12
	var doc *Document
	var bin []byte

	chunkIdx := 0
	for offset < len(data) {
		if offset+8 > len(data) {
			return nil, nil, fmt.Errorf("truncated chunk header at %d", offset)
		}
		chunkLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		chunkType := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		offset += 8
		if chunkLen%4 != 0 {
			return nil, nil, fmt.Errorf("chunk %d length %d is not 4-byte aligned", chunkIdx, chunkLen)
		}
		if offset+chunkLen > len(data) {
			return nil, nil, fmt.Errorf("chunk %d overflows file (%d + %d > %d)", chunkIdx, offset, chunkLen, len(data))
		}
		payload := data[offset : offset+chunkLen]
		offset += chunkLen

		switch chunkIdx {
		case 0:
			if chunkType != ChunkTypeJSON {
				return nil, nil, fmt.Errorf("first glb chunk must be JSON, got 0x%08x", chunkType)
			}
			var parsed Document
			if err := json.Unmarshal(payload, &parsed); err != nil {
				return nil, nil, fmt.Errorf("parse glb json chunk: %w", err)
			}
			doc = &parsed
		case 1:
			if chunkType != ChunkTypeBIN {
				return nil, nil, fmt.Errorf("second glb chunk must be BIN, got 0x%08x", chunkType)
			}
			bin = payload
		default:
			return nil, nil, fmt.Errorf("unexpected extra glb chunk %d (type 0x%08x)", chunkIdx, chunkType)
		}
		chunkIdx++
	}
	if doc == nil {
		return nil, nil, fmt.Errorf("glb missing JSON chunk")
	}
	if len(doc.Buffers) > 0 && doc.Buffers[0].URI == "" {
		if len(bin) < doc.Buffers[0].ByteLength {
			return nil, nil, fmt.Errorf("glb BIN chunk %d < buffers[0].byteLength %d", len(bin), doc.Buffers[0].ByteLength)
		}
		for i := doc.Buffers[0].ByteLength; i < len(bin); i++ {
			if bin[i] != 0 {
				return nil, nil, fmt.Errorf("glb BIN padding byte at %d is 0x%02x, want 0x00", i, bin[i])
			}
		}
		bin = bin[:doc.Buffers[0].ByteLength]
	}
	return doc, bin, nil
}

// ParseEmbeddedGLTF parses a glTF 2.0 JSON file whose single buffer uses a
// base64 data URI.
func ParseEmbeddedGLTF(data []byte) (*Document, []byte, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse gltf json: %w", err)
	}
	if len(doc.Buffers) != 1 {
		return nil, nil, fmt.Errorf("expected 1 embedded buffer, got %d", len(doc.Buffers))
	}
	const prefix = "data:application/octet-stream;base64,"
	uri := doc.Buffers[0].URI
	if !strings.HasPrefix(uri, prefix) {
		return nil, nil, fmt.Errorf("buffer URI missing base64 data prefix")
	}
	bin, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		return nil, nil, fmt.Errorf("decode embedded buffer base64: %w", err)
	}
	if len(bin) != doc.Buffers[0].ByteLength {
		return nil, nil, fmt.Errorf("decoded buffer length %d != byteLength %d", len(bin), doc.Buffers[0].ByteLength)
	}
	return &doc, bin, nil
}

// ValidateDocument verifies glTF 2.0 schema bounds, geometry winding, normals,
// skinning joints/weights/IBMs, and animation samplers/events.
func ValidateDocument(doc *Document, bin []byte) error {
	if doc.Asset.Version != "2.0" {
		return fmt.Errorf("asset.version = %q, want 2.0", doc.Asset.Version)
	}
	if len(doc.Buffers) != 1 {
		return fmt.Errorf("buffers count = %d, want 1", len(doc.Buffers))
	}
	if len(bin) < doc.Buffers[0].ByteLength {
		return fmt.Errorf("buffer byteLength %d exceeds binary payload %d", doc.Buffers[0].ByteLength, len(bin))
	}
	if doc.Scene != nil {
		if *doc.Scene < 0 || *doc.Scene >= len(doc.Scenes) {
			return fmt.Errorf("scene index %d out of range", *doc.Scene)
		}
	}
	for si, sc := range doc.Scenes {
		for _, ni := range sc.Nodes {
			if ni < 0 || ni >= len(doc.Nodes) {
				return fmt.Errorf("scene[%d] node %d out of range", si, ni)
			}
		}
	}
	for ni, n := range doc.Nodes {
		for _, ci := range n.Children {
			if ci < 0 || ci >= len(doc.Nodes) {
				return fmt.Errorf("node[%d] child %d out of range", ni, ci)
			}
		}
		if n.Mesh != nil && (*n.Mesh < 0 || *n.Mesh >= len(doc.Meshes)) {
			return fmt.Errorf("node[%d] mesh %d out of range", ni, *n.Mesh)
		}
		if n.Skin != nil && (*n.Skin < 0 || *n.Skin >= len(doc.Skins)) {
			return fmt.Errorf("node[%d] skin %d out of range", ni, *n.Skin)
		}
	}
	for bvi, bv := range doc.BufferViews {
		if bv.Buffer != 0 {
			return fmt.Errorf("bufferView[%d] buffer %d out of range", bvi, bv.Buffer)
		}
		if bv.ByteOffset < 0 || bv.ByteLength <= 0 || bv.ByteOffset+bv.ByteLength > doc.Buffers[0].ByteLength {
			return fmt.Errorf("bufferView[%d] range [%d, %d) exceeds buffer length %d", bvi, bv.ByteOffset, bv.ByteOffset+bv.ByteLength, doc.Buffers[0].ByteLength)
		}
	}
	for ai, acc := range doc.Accessors {
		if acc.BufferView == nil || *acc.BufferView < 0 || *acc.BufferView >= len(doc.BufferViews) {
			return fmt.Errorf("accessor[%d] invalid bufferView", ai)
		}
		bv := doc.BufferViews[*acc.BufferView]
		compSize, err := componentByteSize(acc.ComponentType)
		if err != nil {
			return fmt.Errorf("accessor[%d]: %w", ai, err)
		}
		elems, err := typeComponentCount(acc.Type)
		if err != nil {
			return fmt.Errorf("accessor[%d]: %w", ai, err)
		}
		if (bv.ByteOffset+acc.ByteOffset)%compSize != 0 {
			return fmt.Errorf("accessor[%d] offset %d not aligned to component size %d", ai, bv.ByteOffset+acc.ByteOffset, compSize)
		}
		need := acc.ByteOffset + acc.Count*elems*compSize
		if acc.Count <= 0 || need > bv.ByteLength {
			return fmt.Errorf("accessor[%d] needs %d bytes, bufferView has %d", ai, need, bv.ByteLength)
		}
	}

	// Validate meshes and triangle winding.
	for mi, mesh := range doc.Meshes {
		for pi, prim := range mesh.Primitives {
			posIdx, ok := prim.Attributes["POSITION"]
			if !ok {
				return fmt.Errorf("mesh[%d].primitive[%d] missing POSITION", mi, pi)
			}
			positions, err := readVec3(doc, bin, posIdx)
			if err != nil {
				return fmt.Errorf("mesh[%d].primitive[%d] POSITION: %w", mi, pi, err)
			}
			posAcc := doc.Accessors[posIdx]
			if len(posAcc.Min) != 3 || len(posAcc.Max) != 3 {
				return fmt.Errorf("mesh[%d].primitive[%d] POSITION missing min/max", mi, pi)
			}
			gotMin, gotMax := vec3Bounds(positions)
			for axis := 0; axis < 3; axis++ {
				if math.Abs(posAcc.Min[axis]-gotMin[axis]) > 1e-5 || math.Abs(posAcc.Max[axis]-gotMax[axis]) > 1e-5 {
					return fmt.Errorf("mesh[%d].primitive[%d] POSITION bounds mismatch on axis %d", mi, pi, axis)
				}
			}

			var normals [][3]float32
			if normIdx, hasNorm := prim.Attributes["NORMAL"]; hasNorm {
				normals, err = readVec3(doc, bin, normIdx)
				if err != nil {
					return fmt.Errorf("mesh[%d].primitive[%d] NORMAL: %w", mi, pi, err)
				}
				if len(normals) != len(positions) {
					return fmt.Errorf("mesh[%d].primitive[%d] NORMAL count %d != POSITION %d", mi, pi, len(normals), len(positions))
				}
				for vi, n := range normals {
					mag := math.Sqrt(float64(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]))
					if math.Abs(mag-1) > 1e-4 {
						return fmt.Errorf("mesh[%d].primitive[%d] normal[%d] length %f != 1", mi, pi, vi, mag)
					}
				}
			}

			if prim.Indices != nil {
				indices, err := readUint16(doc, bin, *prim.Indices)
				if err != nil {
					return fmt.Errorf("mesh[%d].primitive[%d] indices: %w", mi, pi, err)
				}
				if len(indices)%3 != 0 {
					return fmt.Errorf("mesh[%d].primitive[%d] index count %d not multiple of 3", mi, pi, len(indices))
				}
				for ti := 0; ti < len(indices); ti += 3 {
					i0, i1, i2 := int(indices[ti]), int(indices[ti+1]), int(indices[ti+2])
					if i0 >= len(positions) || i1 >= len(positions) || i2 >= len(positions) {
						return fmt.Errorf("mesh[%d].primitive[%d] triangle %d index out of range", mi, pi, ti/3)
					}
					if len(normals) > 0 {
						e1 := sub3(positions[i1], positions[i0])
						e2 := sub3(positions[i2], positions[i0])
						c := cross3(e1, e2)
						if dot3(c, normals[i0]) <= 0 {
							return fmt.Errorf("mesh[%d].primitive[%d] triangle %d is not CCW outward relative to normal", mi, pi, ti/3)
						}
					}
				}
			}

			// Validate skinning attributes if present.
			jIdx, hasJoints := prim.Attributes["JOINTS_0"]
			wIdx, hasWeights := prim.Attributes["WEIGHTS_0"]
			if hasJoints != hasWeights {
				return fmt.Errorf("mesh[%d].primitive[%d] must define both JOINTS_0 and WEIGHTS_0", mi, pi)
			}
			if hasJoints {
				joints, err := readU8Vec4(doc, bin, jIdx)
				if err != nil {
					return fmt.Errorf("mesh[%d].primitive[%d] JOINTS_0: %w", mi, pi, err)
				}
				weights, err := readVec4(doc, bin, wIdx)
				if err != nil {
					return fmt.Errorf("mesh[%d].primitive[%d] WEIGHTS_0: %w", mi, pi, err)
				}
				if len(joints) != len(positions) || len(weights) != len(positions) {
					return fmt.Errorf("mesh[%d].primitive[%d] skinning attribute count mismatch", mi, pi)
				}
				maxJoint := 0
				for _, sk := range doc.Skins {
					if len(sk.Joints) > maxJoint {
						maxJoint = len(sk.Joints)
					}
				}
				for vi := range positions {
					sum := float64(0)
					for k := 0; k < 4; k++ {
						w := float64(weights[vi][k])
						if w < 0 || w > 1 || math.IsNaN(w) {
							return fmt.Errorf("mesh[%d].primitive[%d] vertex %d weight %d invalid: %f", mi, pi, vi, k, w)
						}
						if w > 0 && int(joints[vi][k]) >= maxJoint {
							return fmt.Errorf("mesh[%d].primitive[%d] vertex %d joint index %d out of range", mi, pi, vi, joints[vi][k])
						}
						sum += w
					}
					if math.Abs(sum-1) > 1e-4 {
						return fmt.Errorf("mesh[%d].primitive[%d] vertex %d weight sum %f != 1", mi, pi, vi, sum)
					}
				}
			}
		}
	}

	// Validate skins and verify worldBind * IBM == Identity for every joint.
	worldMats := computeBindGlobals(doc)
	for si, sk := range doc.Skins {
		if len(sk.Joints) == 0 {
			return fmt.Errorf("skin[%d] has no joints", si)
		}
		if sk.Skeleton != nil && (*sk.Skeleton < 0 || *sk.Skeleton >= len(doc.Nodes)) {
			return fmt.Errorf("skin[%d] skeleton %d out of range", si, *sk.Skeleton)
		}
		if sk.InverseBindMatrices == nil {
			return fmt.Errorf("skin[%d] missing inverseBindMatrices", si)
		}
		ibms, err := readMat4(doc, bin, *sk.InverseBindMatrices)
		if err != nil {
			return fmt.Errorf("skin[%d] inverseBindMatrices: %w", si, err)
		}
		if len(ibms) != len(sk.Joints) {
			return fmt.Errorf("skin[%d] IBM count %d != joint count %d", si, len(ibms), len(sk.Joints))
		}
		for ji, nodeIdx := range sk.Joints {
			if nodeIdx < 0 || nodeIdx >= len(doc.Nodes) {
				return fmt.Errorf("skin[%d] joint[%d] node %d out of range", si, ji, nodeIdx)
			}
			prod := mulMat4(worldMats[nodeIdx], ibms[ji])
			if !isIdentityMat4(prod, 1e-5) {
				return fmt.Errorf("skin[%d] joint[%d] (node %d) worldBind * IBM != Identity: %v", si, ji, nodeIdx, prod)
			}
		}
	}

	// Validate animations, samplers, quaternions, and extras.events.
	for ai, anim := range doc.Animations {
		minClip := math.Inf(1)
		maxClip := math.Inf(-1)
		for si, samp := range anim.Samplers {
			times, err := readFloat32(doc, bin, samp.Input)
			if err != nil {
				return fmt.Errorf("animation[%d].sampler[%d] input: %w", ai, si, err)
			}
			tAcc := doc.Accessors[samp.Input]
			if len(tAcc.Min) != 1 || len(tAcc.Max) != 1 {
				return fmt.Errorf("animation[%d].sampler[%d] input missing min/max", ai, si)
			}
			for ti := 1; ti < len(times); ti++ {
				if times[ti] <= times[ti-1] {
					return fmt.Errorf("animation[%d].sampler[%d] timestamps not strictly increasing", ai, si)
				}
			}
			if math.Abs(tAcc.Min[0]-float64(times[0])) > 1e-5 || math.Abs(tAcc.Max[0]-float64(times[len(times)-1])) > 1e-5 {
				return fmt.Errorf("animation[%d].sampler[%d] input min/max mismatch", ai, si)
			}
			if tAcc.Min[0] < minClip {
				minClip = tAcc.Min[0]
			}
			if tAcc.Max[0] > maxClip {
				maxClip = tAcc.Max[0]
			}
			outAcc := doc.Accessors[samp.Output]
			if outAcc.Count != len(times) {
				return fmt.Errorf("animation[%d].sampler[%d] output count %d != input count %d", ai, si, outAcc.Count, len(times))
			}
		}
		for ci, ch := range anim.Channels {
			if ch.Sampler < 0 || ch.Sampler >= len(anim.Samplers) {
				return fmt.Errorf("animation[%d].channel[%d] sampler %d out of range", ai, ci, ch.Sampler)
			}
			if ch.Target.Node == nil || *ch.Target.Node < 0 || *ch.Target.Node >= len(doc.Nodes) {
				return fmt.Errorf("animation[%d].channel[%d] invalid target node", ai, ci)
			}
			if ch.Target.Path == "rotation" {
				quats, err := readVec4(doc, bin, anim.Samplers[ch.Sampler].Output)
				if err != nil {
					return fmt.Errorf("animation[%d].channel[%d] rotation output: %w", ai, ci, err)
				}
				for qi, q := range quats {
					mag := math.Sqrt(float64(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3]))
					if math.Abs(mag-1) > 1e-4 {
						return fmt.Errorf("animation[%d].channel[%d] quat[%d] length %f != 1", ai, ci, qi, mag)
					}
				}
			}
		}
		if anim.Extras != nil {
			for ei, ev := range anim.Extras.Events {
				if ev.Name == "" {
					return fmt.Errorf("animation[%d].extras.events[%d] empty name", ai, ei)
				}
				if ev.Time < minClip-1e-5 || ev.Time > maxClip+1e-5 {
					return fmt.Errorf("animation[%d].extras.events[%d] time %f outside [%f, %f]", ai, ei, ev.Time, minClip, maxClip)
				}
			}
		}
	}

	return nil
}

func componentByteSize(ct int) (int, error) {
	switch ct {
	case ComponentUnsignedByte:
		return 1, nil
	case ComponentUnsignedShort:
		return 2, nil
	case ComponentFloat:
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported componentType %d", ct)
	}
}

func typeComponentCount(t string) (int, error) {
	switch t {
	case "SCALAR":
		return 1, nil
	case "VEC2":
		return 2, nil
	case "VEC3":
		return 3, nil
	case "VEC4":
		return 4, nil
	case "MAT4":
		return 16, nil
	default:
		return 0, fmt.Errorf("unsupported accessor type %q", t)
	}
}

func accessorSlice(doc *Document, bin []byte, idx int) (Accessor, []byte, error) {
	if idx < 0 || idx >= len(doc.Accessors) {
		return Accessor{}, nil, fmt.Errorf("accessor %d out of range", idx)
	}
	acc := doc.Accessors[idx]
	bv := doc.BufferViews[*acc.BufferView]
	start := bv.ByteOffset + acc.ByteOffset
	return acc, bin[start : bv.ByteOffset+bv.ByteLength], nil
}

func readFloat32(doc *Document, bin []byte, idx int) ([]float32, error) {
	acc, raw, err := accessorSlice(doc, bin, idx)
	if err != nil {
		return nil, err
	}
	if acc.ComponentType != ComponentFloat || acc.Type != "SCALAR" {
		return nil, fmt.Errorf("accessor %d is %s/%d, want SCALAR/FLOAT", idx, acc.Type, acc.ComponentType)
	}
	out := make([]float32, acc.Count)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4 : i*4+4]))
	}
	return out, nil
}

func readUint16(doc *Document, bin []byte, idx int) ([]uint16, error) {
	acc, raw, err := accessorSlice(doc, bin, idx)
	if err != nil {
		return nil, err
	}
	if acc.ComponentType != ComponentUnsignedShort || acc.Type != "SCALAR" {
		return nil, fmt.Errorf("accessor %d is %s/%d, want SCALAR/UNSIGNED_SHORT", idx, acc.Type, acc.ComponentType)
	}
	out := make([]uint16, acc.Count)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
	}
	return out, nil
}

func readVec3(doc *Document, bin []byte, idx int) ([][3]float32, error) {
	acc, raw, err := accessorSlice(doc, bin, idx)
	if err != nil {
		return nil, err
	}
	if acc.ComponentType != ComponentFloat || acc.Type != "VEC3" {
		return nil, fmt.Errorf("accessor %d is %s/%d, want VEC3/FLOAT", idx, acc.Type, acc.ComponentType)
	}
	out := make([][3]float32, acc.Count)
	for i := range out {
		off := i * 12
		out[i] = [3]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4])),
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off+4 : off+8])),
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off+8 : off+12])),
		}
	}
	return out, nil
}

func readVec4(doc *Document, bin []byte, idx int) ([][4]float32, error) {
	acc, raw, err := accessorSlice(doc, bin, idx)
	if err != nil {
		return nil, err
	}
	if acc.ComponentType != ComponentFloat || acc.Type != "VEC4" {
		return nil, fmt.Errorf("accessor %d is %s/%d, want VEC4/FLOAT", idx, acc.Type, acc.ComponentType)
	}
	out := make([][4]float32, acc.Count)
	for i := range out {
		off := i * 16
		out[i] = [4]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4])),
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off+4 : off+8])),
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off+8 : off+12])),
			math.Float32frombits(binary.LittleEndian.Uint32(raw[off+12 : off+16])),
		}
	}
	return out, nil
}

func readU8Vec4(doc *Document, bin []byte, idx int) ([][4]uint8, error) {
	acc, raw, err := accessorSlice(doc, bin, idx)
	if err != nil {
		return nil, err
	}
	if acc.ComponentType != ComponentUnsignedByte || acc.Type != "VEC4" {
		return nil, fmt.Errorf("accessor %d is %s/%d, want VEC4/UNSIGNED_BYTE", idx, acc.Type, acc.ComponentType)
	}
	out := make([][4]uint8, acc.Count)
	for i := range out {
		off := i * 4
		out[i] = [4]uint8{raw[off], raw[off+1], raw[off+2], raw[off+3]}
	}
	return out, nil
}

func readMat4(doc *Document, bin []byte, idx int) ([][16]float32, error) {
	acc, raw, err := accessorSlice(doc, bin, idx)
	if err != nil {
		return nil, err
	}
	if acc.ComponentType != ComponentFloat || acc.Type != "MAT4" {
		return nil, fmt.Errorf("accessor %d is %s/%d, want MAT4/FLOAT", idx, acc.Type, acc.ComponentType)
	}
	out := make([][16]float32, acc.Count)
	for i := range out {
		off := i * 64
		for k := 0; k < 16; k++ {
			out[i][k] = math.Float32frombits(binary.LittleEndian.Uint32(raw[off+k*4 : off+k*4+4]))
		}
	}
	return out, nil
}

func sub3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func cross3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func dot3(a, b [3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func identityMat4() [16]float32 {
	return [16]float32{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// mulMat4 multiplies two column-major 4x4 matrices (a * b).
func mulMat4(a, b [16]float32) [16]float32 {
	var out [16]float32
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			var sum float32
			for k := 0; k < 4; k++ {
				sum += a[k*4+row] * b[col*4+k]
			}
			out[col*4+row] = sum
		}
	}
	return out
}

func nodeLocalMat4(n Node) [16]float32 {
	m := identityMat4()
	if n.Scale != nil {
		m[0] = float32(n.Scale[0])
		m[5] = float32(n.Scale[1])
		m[10] = float32(n.Scale[2])
	}
	if n.Rotation != nil {
		x, y, z, w := float32(n.Rotation[0]), float32(n.Rotation[1]), float32(n.Rotation[2]), float32(n.Rotation[3])
		r := [16]float32{
			1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
			2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
			2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
			0, 0, 0, 1,
		}
		m = mulMat4(r, m)
	}
	if n.Translation != nil {
		m[12] = float32(n.Translation[0])
		m[13] = float32(n.Translation[1])
		m[14] = float32(n.Translation[2])
	}
	return m
}

func computeBindGlobals(doc *Document) [][16]float32 {
	globals := make([][16]float32, len(doc.Nodes))
	hasParent := make([]bool, len(doc.Nodes))
	for _, n := range doc.Nodes {
		for _, c := range n.Children {
			if c >= 0 && c < len(doc.Nodes) {
				hasParent[c] = true
			}
		}
	}
	var walk func(idx int, parent [16]float32)
	walk = func(idx int, parent [16]float32) {
		g := mulMat4(parent, nodeLocalMat4(doc.Nodes[idx]))
		globals[idx] = g
		for _, c := range doc.Nodes[idx].Children {
			walk(c, g)
		}
	}
	for i := range doc.Nodes {
		if !hasParent[i] {
			walk(i, identityMat4())
		}
	}
	return globals
}

func isIdentityMat4(m [16]float32, eps float64) bool {
	id := identityMat4()
	for i := range m {
		if math.Abs(float64(m[i]-id[i])) > eps {
			return false
		}
	}
	return true
}
