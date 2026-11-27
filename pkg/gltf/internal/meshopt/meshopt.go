// Package meshopt decodes the meshoptimizer bitstreams used by the EXT_meshopt_compression and KHR_meshopt_compression glTF extensions in pure Go: the attribute codec (versions 0 and 1), the triangle and index sequence codecs, and the octahedral, quaternion, exponential and color filters.
//
// It is a port, modified for Go, of the scalar reference decoder in meshoptimizer 0.25 (https://github.com/zeux/meshoptimizer, Copyright (c) 2016-2025 Arseny Kapoulkine, MIT License; see the LICENSE file in this directory), and rejects truncated or malformed streams with an error instead of reading out of bounds.
package meshopt

import (
	"encoding/binary"
	"errors"
	"math"
)

// Errors returned for streams that cannot be decoded.
var (
	// ErrHeader reports an unknown header byte or unsupported codec version.
	ErrHeader = errors.New("meshopt: invalid header or unsupported version")
	// ErrData reports a truncated stream, trailing bytes or a malformed block.
	ErrData = errors.New("meshopt: malformed or truncated data")
	// ErrLayout reports an element size or count the codec cannot represent.
	ErrLayout = errors.New("meshopt: invalid element size or count")
)

const (
	vertexHeader      = 0xa0
	indexHeader       = 0xe0
	sequenceHeader    = 0xd0
	maxVertexVersion  = 1
	maxIndexVersion   = 1
	blockSizeBytes    = 8192
	blockMaxSize      = 256
	byteGroupSize     = 16
	byteGroupDecodeLm = 24
	tailMinSizeV0     = 32
	tailMinSizeV1     = 24
)

var (
	bitsV0 = [4]int{0, 2, 4, 8}
	bitsV1 = [5]int{0, 1, 2, 4, 8}
)

func vertexBlockSize(vertexSize int) int {
	r := (blockSizeBytes / vertexSize) &^ (byteGroupSize - 1)
	return min(r, blockMaxSize)
}

// DecodeVertexBuffer decodes count elements of size bytes (a multiple of 4, at most 256) from the attribute codec stream data into dst, which must hold count*size bytes.
func DecodeVertexBuffer(dst []byte, count, size int, data []byte) error {
	if size <= 0 || size > 256 || size%4 != 0 || count < 0 || len(dst) < count*size {
		return ErrLayout
	}
	if len(data) < 1 {
		return ErrData
	}
	header := data[0]
	if header&0xf0 != vertexHeader {
		return ErrHeader
	}
	version := int(header & 0x0f)
	if version > maxVertexVersion {
		return ErrHeader
	}
	tailSize := size
	tailMin := tailMinSizeV0
	if version == 1 {
		tailSize += size / 4
		tailMin = tailMinSizeV1
	}
	tailPad := max(tailSize, tailMin)
	body := data[1:]
	if len(body) < tailPad {
		return ErrData
	}
	tail := body[len(body)-tailSize:]
	var last [256]byte
	copy(last[:size], tail[:size])
	var channels []byte
	if version == 1 {
		channels = tail[size:]
	}
	end := len(body) - tailPad
	pos := 0
	blockSize := vertexBlockSize(size)
	for offset := 0; offset < count; {
		n := min(blockSize, count-offset)
		var err error
		pos, err = decodeVertexBlock(body, pos, dst[offset*size:(offset+n)*size], n, size, &last, channels, version)
		if err != nil {
			return err
		}
		offset += n
	}
	if pos != end {
		return ErrData
	}
	return nil
}

func decodeVertexBlock(data []byte, pos int, out []byte, count, size int, last *[256]byte, channels []byte, version int) (int, error) {
	var buf [blockMaxSize * 4]byte
	var transposed [blockSizeBytes]byte
	aligned := (count + byteGroupSize - 1) &^ (byteGroupSize - 1)
	controlSize := 0
	if version == 1 {
		controlSize = size / 4
	}
	if len(data)-pos < controlSize {
		return 0, ErrData
	}
	control := data[pos : pos+controlSize]
	pos += controlSize
	for k := 0; k < size; k += 4 {
		ctrlByte := byte(0)
		if version == 1 {
			ctrlByte = control[k/4]
		}
		for j := 0; j < 4; j++ {
			ctrl := int(ctrlByte>>(2*j)) & 3
			dst := buf[j*count : j*count+aligned]
			switch ctrl {
			case 3:
				if len(data)-pos < count {
					return 0, ErrData
				}
				copy(dst[:count], data[pos:pos+count])
				pos += count
			case 2:
				clear(dst[:count])
			default:
				var bits []int
				if version == 0 {
					bits = bitsV0[:]
				} else {
					bits = bitsV1[ctrl : ctrl+4]
				}
				var err error
				if pos, err = decodeBytes(data, pos, dst, bits); err != nil {
					return 0, err
				}
			}
		}
		channel := 0
		if version == 1 {
			channel = int(channels[k/4])
		}
		switch channel & 3 {
		case 0:
			for c := 0; c < 4; c++ {
				p := last[k+c]
				for i := 0; i < count; i++ {
					v := buf[c*count+i]
					p += (v >> 1) ^ -(v & 1)
					transposed[i*size+k+c] = p
				}
			}
		case 1:
			for c := 0; c < 4; c += 2 {
				p := uint16(last[k+c]) | uint16(last[k+c+1])<<8
				for i := 0; i < count; i++ {
					v := uint16(buf[c*count+i]) | uint16(buf[(c+1)*count+i])<<8
					p += (v >> 1) ^ -(v & 1)
					transposed[i*size+k+c] = byte(p)
					transposed[i*size+k+c+1] = byte(p >> 8)
				}
			}
		case 2:
			rot := uint((32 - (channel >> 4)) & 31)
			p := binary.LittleEndian.Uint32(last[k:])
			for i := 0; i < count; i++ {
				v := uint32(buf[i]) | uint32(buf[count+i])<<8 | uint32(buf[2*count+i])<<16 | uint32(buf[3*count+i])<<24
				p ^= v<<rot | v>>((32-rot)&31)
				binary.LittleEndian.PutUint32(transposed[i*size+k:], p)
			}
		default:
			return 0, ErrData
		}
	}
	copy(out, transposed[:count*size])
	copy(last[:size], transposed[(count-1)*size:count*size])
	return pos, nil
}

// decodeBytes decodes len(dst)/16 byte groups whose 2-bit modes select entries of bits.
func decodeBytes(data []byte, pos int, dst []byte, bits []int) (int, error) {
	groups := len(dst) / byteGroupSize
	headerSize := (groups + 3) / 4
	if len(data)-pos < headerSize {
		return 0, ErrData
	}
	header := data[pos : pos+headerSize]
	pos += headerSize
	for g := 0; g < groups; g++ {
		if len(data)-pos < byteGroupDecodeLm {
			return 0, ErrData
		}
		mode := int(header[g/4]>>(2*(g%4))) & 3
		pos = decodeBytesGroup(data, pos, dst[g*byteGroupSize:(g+1)*byteGroupSize], bits[mode])
	}
	return pos, nil
}

// decodeBytesGroup decodes one group of 16 bytes; the caller guarantees byteGroupDecodeLm readable bytes, which covers the largest group.
func decodeBytesGroup(data []byte, pos int, dst []byte, bits int) int {
	switch bits {
	case 0:
		clear(dst)
		return pos
	case 8:
		copy(dst, data[pos:pos+byteGroupSize])
		return pos + byteGroupSize
	}
	sentinel := byte(1<<bits - 1)
	perByte := 8 / bits
	headerBytes := byteGroupSize / perByte
	extra := pos + headerBytes
	for i := 0; i < byteGroupSize; i++ {
		b := data[pos+i/perByte]
		var enc byte
		if bits == 1 {
			// 1-bit groups store their values least significant bit first.
			enc = (b >> (i % 8)) & 1
		} else {
			enc = (b >> (8 - bits*(i%perByte+1))) & sentinel
		}
		if enc == sentinel {
			dst[i] = data[extra]
			extra++
		} else {
			dst[i] = enc
		}
	}
	return extra
}

func writeIndex(dst []byte, i int, size int, v uint32) {
	if size == 2 {
		binary.LittleEndian.PutUint16(dst[2*i:], uint16(v))
	} else {
		binary.LittleEndian.PutUint32(dst[4*i:], v)
	}
}

func decodeVByte(data []byte, pos int) (uint32, int) {
	lead := data[pos]
	pos++
	if lead < 128 {
		return uint32(lead), pos
	}
	result := uint32(lead & 127)
	shift := uint32(7)
	for i := 0; i < 4; i++ {
		group := data[pos]
		pos++
		result |= uint32(group&127) << shift
		shift += 7
		if group < 128 {
			break
		}
	}
	return result, pos
}

func decodeIndexDelta(data []byte, pos int, last uint32) (uint32, int) {
	v, pos := decodeVByte(data, pos)
	d := (v >> 1) ^ -(v & 1)
	return last + d, pos
}

// DecodeIndexBuffer decodes count triangle-list indices of size bytes (2 or 4) from the triangle codec stream data into dst.
func DecodeIndexBuffer(dst []byte, count, size int, data []byte) error {
	if count < 0 || count%3 != 0 || (size != 2 && size != 4) || len(dst) < count*size {
		return ErrLayout
	}
	if len(data) < 1+count/3+16 {
		return ErrData
	}
	if data[0]&0xf0 != indexHeader {
		return ErrHeader
	}
	version := int(data[0] & 0x0f)
	if version > maxIndexVersion {
		return ErrHeader
	}
	var edgeFifo [16][2]uint32
	var vertexFifo [16]uint32
	for i := range edgeFifo {
		edgeFifo[i] = [2]uint32{math.MaxUint32, math.MaxUint32}
		vertexFifo[i] = math.MaxUint32
	}
	edgeOff, vertexOff := 0, 0
	pushEdge := func(a, b uint32) {
		edgeFifo[edgeOff] = [2]uint32{a, b}
		edgeOff = (edgeOff + 1) & 15
	}
	pushVertex := func(v uint32, cond bool) {
		vertexFifo[vertexOff] = v
		if cond {
			vertexOff = (vertexOff + 1) & 15
		}
	}
	var next, last uint32
	fecmax := 15
	if version >= 1 {
		fecmax = 13
	}
	code := 1
	pos := 1 + count/3
	safeEnd := len(data) - 16
	codeaux := data[safeEnd:]
	for i := 0; i < count; i += 3 {
		if pos > safeEnd {
			return ErrData
		}
		codetri := data[code]
		code++
		if codetri < 0xf0 {
			fe := int(codetri >> 4)
			e := edgeFifo[(edgeOff-1-fe)&15]
			a, b := e[0], e[1]
			var c uint32
			fec := int(codetri & 15)
			if fec < fecmax {
				if fec == 0 {
					c = next
					next++
				} else {
					c = vertexFifo[(vertexOff-1-fec)&15]
				}
				pushVertex(c, fec == 0)
			} else {
				if fec != 15 {
					c = last + uint32(fec-(fec^3))
				} else {
					c, pos = decodeIndexDelta(data, pos, last)
				}
				last = c
				pushVertex(c, true)
			}
			pushEdge(c, b)
			pushEdge(a, c)
			writeIndex(dst, i, size, a)
			writeIndex(dst, i+1, size, b)
			writeIndex(dst, i+2, size, c)
			continue
		}
		if codetri < 0xfe {
			aux := codeaux[codetri&15]
			feb := int(aux >> 4)
			fec := int(aux & 15)
			a := next
			next++
			b := vertexFifo[(vertexOff-feb)&15]
			if feb == 0 {
				b = next
				next++
			}
			c := vertexFifo[(vertexOff-fec)&15]
			if fec == 0 {
				c = next
				next++
			}
			writeIndex(dst, i, size, a)
			writeIndex(dst, i+1, size, b)
			writeIndex(dst, i+2, size, c)
			pushVertex(a, true)
			pushVertex(b, feb == 0)
			pushVertex(c, fec == 0)
			pushEdge(b, a)
			pushEdge(c, b)
			pushEdge(a, c)
			continue
		}
		aux := data[pos]
		pos++
		fea := 15
		if codetri == 0xfe {
			fea = 0
		}
		feb := int(aux >> 4)
		fec := int(aux & 15)
		if aux == 0 {
			next = 0
		}
		var a, b, c uint32
		if fea == 0 {
			a = next
			next++
		}
		if feb == 0 {
			b = next
			next++
		} else {
			b = vertexFifo[(vertexOff-feb)&15]
		}
		if fec == 0 {
			c = next
			next++
		} else {
			c = vertexFifo[(vertexOff-fec)&15]
		}
		if fea == 15 {
			a, pos = decodeIndexDelta(data, pos, last)
			last = a
		}
		if feb == 15 {
			b, pos = decodeIndexDelta(data, pos, last)
			last = b
		}
		if fec == 15 {
			c, pos = decodeIndexDelta(data, pos, last)
			last = c
		}
		writeIndex(dst, i, size, a)
		writeIndex(dst, i+1, size, b)
		writeIndex(dst, i+2, size, c)
		pushVertex(a, true)
		pushVertex(b, feb == 0 || feb == 15)
		pushVertex(c, fec == 0 || fec == 15)
		pushEdge(b, a)
		pushEdge(c, b)
		pushEdge(a, c)
	}
	if pos != safeEnd {
		return ErrData
	}
	return nil
}

// DecodeIndexSequence decodes count indices of size bytes (2 or 4) from the index sequence codec stream data into dst.
func DecodeIndexSequence(dst []byte, count, size int, data []byte) error {
	if count < 0 || (size != 2 && size != 4) || len(dst) < count*size {
		return ErrLayout
	}
	if len(data) < 1+count+4 {
		return ErrData
	}
	if data[0]&0xf0 != sequenceHeader {
		return ErrHeader
	}
	if int(data[0]&0x0f) > maxIndexVersion {
		return ErrHeader
	}
	pos := 1
	safeEnd := len(data) - 4
	var last [2]uint32
	for i := 0; i < count; i++ {
		if pos >= safeEnd {
			return ErrData
		}
		var v uint32
		v, pos = decodeVByte(data, pos)
		current := v & 1
		v >>= 1
		d := (v >> 1) ^ -(v & 1)
		index := last[current] + d
		last[current] = index
		writeIndex(dst, i, size, index)
	}
	if pos != safeEnd {
		return ErrData
	}
	return nil
}
