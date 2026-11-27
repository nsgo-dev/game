package draco

import (
	"encoding/binary"
	"math"
)

// buffer is a forward-only reader over a Draco bitstream with an optional bit-level mode, mirroring draco::DecoderBuffer. It is copied by value where the reference decoder copies its buffers.
type buffer struct {
	data []byte
	pos  int

	bitMode   bool
	bitData   []byte
	bitOffset int
}

func newBuffer(data []byte) buffer { return buffer{data: data} }

// tail returns a new buffer that starts at the current position, like DecoderBuffer::Init(data_head(), remaining_size()).
func (b *buffer) tail() buffer { return buffer{data: b.data[b.pos:]} }

func (b *buffer) remaining() int { return len(b.data) - b.pos }

func (b *buffer) advance(n int) bool {
	if n < 0 || n > b.remaining() {
		return false
	}
	b.pos += n
	return true
}

func (b *buffer) bytes(n int) ([]byte, bool) {
	if n < 0 || n > b.remaining() {
		return nil, false
	}
	out := b.data[b.pos : b.pos+n]
	b.pos += n
	return out, true
}

func (b *buffer) u8() (uint8, bool) {
	if b.remaining() < 1 {
		return 0, false
	}
	v := b.data[b.pos]
	b.pos++
	return v, true
}

func (b *buffer) i8() (int8, bool) {
	v, ok := b.u8()
	return int8(v), ok
}

func (b *buffer) u16() (uint16, bool) {
	if b.remaining() < 2 {
		return 0, false
	}
	v := binary.LittleEndian.Uint16(b.data[b.pos:])
	b.pos += 2
	return v, true
}

func (b *buffer) u32() (uint32, bool) {
	if b.remaining() < 4 {
		return 0, false
	}
	v := binary.LittleEndian.Uint32(b.data[b.pos:])
	b.pos += 4
	return v, true
}

func (b *buffer) i32() (int32, bool) {
	v, ok := b.u32()
	return int32(v), ok
}

func (b *buffer) f32() (float32, bool) {
	v, ok := b.u32()
	return math.Float32frombits(v), ok
}

// varint decodes an unsigned LEB128 value of at most maxBytes bytes; bits beyond the target width are discarded like the reference shifts do.
func (b *buffer) varint(maxBytes int) (uint64, bool) {
	var v uint64
	for i := 0; i < maxBytes; i++ {
		c, ok := b.u8()
		if !ok {
			return 0, false
		}
		v |= uint64(c&0x7f) << (7 * uint(i))
		if c&0x80 == 0 {
			return v, true
		}
	}
	return 0, false
}

func (b *buffer) varU32() (uint32, bool) {
	v, ok := b.varint(5)
	return uint32(v), ok
}

func (b *buffer) varU64() (uint64, bool) { return b.varint(10) }

// startBitDecoding switches to bit mode at the current position, optionally reading a varint size first (returned).
func (b *buffer) startBitDecoding(decodeSize bool) (uint64, bool) {
	var size uint64
	if decodeSize {
		var ok bool
		if size, ok = b.varU64(); !ok {
			return 0, false
		}
	}
	b.bitMode = true
	b.bitData = b.data[b.pos:]
	b.bitOffset = 0
	return size, true
}

// endBitDecoding leaves bit mode and skips the whole bytes the bit reader consumed.
func (b *buffer) endBitDecoding() {
	b.bitMode = false
	b.pos += (b.bitOffset + 7) / 8
}

// bits reads n bits least significant first; bits past the end read as zero without advancing, like the reference BitDecoder.
func (b *buffer) bits(n uint32) (uint32, bool) {
	if !b.bitMode || n > 32 {
		return 0, false
	}
	var v uint32
	for i := uint32(0); i < n; i++ {
		byteOff := b.bitOffset >> 3
		if byteOff >= len(b.bitData) {
			continue
		}
		bit := uint32(b.bitData[byteOff]>>(uint(b.bitOffset)&7)) & 1
		b.bitOffset++
		v |= bit << i
	}
	return v, true
}
