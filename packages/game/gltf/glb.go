package gltf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	glbMagic     uint32 = 0x46546C67 // "glTF"
	glbVersion2  uint32 = 2
	glbChunkJSON uint32 = 0x4E4F534A // "JSON"
	glbChunkBIN  uint32 = 0x004E4942 // "BIN\0"
	glbHeaderLen        = 12
	glbChunkHdr         = 8
)

// isGLB reports whether data begins with the 4-byte binary glTF magic header.
func isGLB(data []byte) bool {
	return len(data) >= 4 && binary.LittleEndian.Uint32(data[:4]) == glbMagic
}

// parseGLB parses a binary glTF 2.0 (.glb) container into its JSON chunk and
// optional BIN buffer chunk. Unknown trailing chunks are skipped per spec.
func parseGLB(data []byte) (jsonChunk []byte, binChunk []byte, err error) {
	if len(data) < glbHeaderLen {
		return nil, nil, errors.New("gltf: truncated GLB header")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != glbMagic {
		return nil, nil, fmt.Errorf("gltf: invalid GLB magic 0x%08x", magic)
	}

	version := binary.LittleEndian.Uint32(data[4:8])
	if version != glbVersion2 {
		return nil, nil, fmt.Errorf("gltf: unsupported GLB version %d", version)
	}

	totalLen := uint64(binary.LittleEndian.Uint32(data[8:12]))
	if totalLen < glbHeaderLen || totalLen > uint64(len(data)) {
		return nil, nil, fmt.Errorf("gltf: invalid GLB length %d (data size %d)", totalLen, len(data))
	}

	offset := uint64(glbHeaderLen)
	chunkIndex := 0
	for offset < totalLen {
		if totalLen-offset < glbChunkHdr {
			return nil, nil, errors.New("gltf: truncated GLB chunk header")
		}
		chunkLen := uint64(binary.LittleEndian.Uint32(data[offset : offset+4]))
		chunkType := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		offset += glbChunkHdr

		if chunkLen%4 != 0 {
			return nil, nil, fmt.Errorf("gltf: GLB chunk %d length %d is not 4-byte aligned", chunkIndex, chunkLen)
		}
		if chunkLen > totalLen-offset {
			return nil, nil, fmt.Errorf("gltf: GLB chunk %d length %d exceeds remaining %d bytes", chunkIndex, chunkLen, totalLen-offset)
		}

		payload := data[offset : offset+chunkLen]
		offset += chunkLen

		if chunkIndex == 0 {
			if chunkType != glbChunkJSON {
				return nil, nil, fmt.Errorf("gltf: first GLB chunk must be JSON (0x%08x), got 0x%08x", glbChunkJSON, chunkType)
			}
			jsonChunk = payload
		} else if chunkIndex == 1 && chunkType == glbChunkBIN {
			binChunk = payload
		}
		// Unknown chunk types (and any extra chunks) are ignored per glTF 2.0 spec.
		chunkIndex++
	}

	if jsonChunk == nil {
		return nil, nil, errors.New("gltf: GLB missing JSON chunk")
	}
	return jsonChunk, binChunk, nil
}
