package draco

// This file ports Draco's entropy coders: the rANS symbol decoder used by DecodeSymbols and the binary rABS decoder (RAnsBitDecoder).

const (
	ansIOBase     = 256
	ansP8LBase    = 4096
	maxSymbolBits = 18
)

// ransPrecisionBits mirrors ComputeRAnsPrecisionFromUniqueSymbolsBitLength.
func ransPrecisionBits(symbolBits int) uint {
	p := (3 * symbolBits) / 2
	if p < 12 {
		p = 12
	}
	if p > 20 {
		p = 20
	}
	return uint(p)
}

type ransSym struct {
	prob, cum uint32
}

// ransSymbolDecoder is RAnsSymbolDecoder<N>: a probability table followed by a rANS stream read from its end.
type ransSymbolDecoder struct {
	precision  uint32
	lBase      uint32
	numSymbols uint32
	lut        []uint32
	syms       []ransSym

	buf    []byte
	offset int
	state  uint32
}

func newRansSymbolDecoder(symbolBits int) *ransSymbolDecoder {
	precision := uint32(1) << ransPrecisionBits(symbolBits)
	return &ransSymbolDecoder{precision: precision, lBase: precision * 4}
}

// create reads the probability table.
func (d *ransSymbolDecoder) create(b *buffer) bool {
	n, ok := b.varU32()
	if !ok {
		return false
	}
	if uint64(n)/64 > uint64(b.remaining()) {
		return false
	}
	d.numSymbols = n
	if n == 0 {
		return true
	}
	probs := make([]uint32, n)
	for i := uint32(0); i < n; i++ {
		data, ok := b.u8()
		if !ok {
			return false
		}
		token := data & 3
		if token == 3 {
			offset := uint32(data >> 2)
			if uint64(i)+uint64(offset) >= uint64(n) {
				return false
			}
			// Zero-probability run of offset+1 symbols; probs is already zeroed.
			i += offset
			continue
		}
		prob := uint32(data >> 2)
		for e := 0; e < int(token); e++ {
			eb, ok := b.u8()
			if !ok {
				return false
			}
			prob |= uint32(eb) << (8*(e+1) - 2)
		}
		probs[i] = prob
	}
	return d.buildLUT(probs)
}

func (d *ransSymbolDecoder) buildLUT(probs []uint32) bool {
	d.lut = make([]uint32, d.precision)
	d.syms = make([]ransSym, len(probs))
	var cum uint64
	act := uint64(0)
	for i, p := range probs {
		d.syms[i] = ransSym{prob: p, cum: uint32(cum)}
		cum += uint64(p)
		if cum > uint64(d.precision) {
			return false
		}
		for j := act; j < cum; j++ {
			d.lut[j] = uint32(i)
		}
		act = cum
	}
	return cum == uint64(d.precision)
}

// startDecoding consumes the encoded rANS bytes from b and initialises the state from their end.
func (d *ransSymbolDecoder) startDecoding(b *buffer) bool {
	size, ok := b.varU64()
	if !ok || size > uint64(b.remaining()) {
		return false
	}
	data, _ := b.bytes(int(size))
	offset := len(data)
	if offset < 1 {
		return false
	}
	d.buf = data
	switch data[offset-1] >> 6 {
	case 0:
		d.offset = offset - 1
		d.state = uint32(data[offset-1] & 0x3f)
	case 1:
		if offset < 2 {
			return false
		}
		d.offset = offset - 2
		d.state = (uint32(data[offset-2]) | uint32(data[offset-1])<<8) & 0x3fff
	case 2:
		if offset < 3 {
			return false
		}
		d.offset = offset - 3
		d.state = (uint32(data[offset-3]) | uint32(data[offset-2])<<8 | uint32(data[offset-1])<<16) & 0x3fffff
	default:
		if offset < 4 {
			return false
		}
		d.offset = offset - 4
		d.state = (uint32(data[offset-4]) | uint32(data[offset-3])<<8 | uint32(data[offset-2])<<16 | uint32(data[offset-1])<<24) & 0x3fffffff
	}
	d.state += d.lBase
	return d.state < d.lBase*ansIOBase
}

func (d *ransSymbolDecoder) decode() uint32 {
	for d.state < d.lBase && d.offset > 0 {
		d.offset--
		d.state = d.state*ansIOBase + uint32(d.buf[d.offset])
	}
	quo := d.state / d.precision
	rem := d.state % d.precision
	sym := d.lut[rem]
	s := d.syms[sym]
	d.state = quo*s.prob + rem - s.cum
	return sym
}

// bitDecoder is RAnsBitDecoder: a binary rABS stream with a fixed probability of zero.
type bitDecoder struct {
	probZero uint8
	buf      []byte
	offset   int
	state    uint32
}

func (d *bitDecoder) startDecoding(b *buffer) bool {
	*d = bitDecoder{}
	p, ok := b.u8()
	if !ok {
		return false
	}
	d.probZero = p
	size, ok := b.varU32()
	if !ok || uint64(size) > uint64(b.remaining()) {
		return false
	}
	data, _ := b.bytes(int(size))
	offset := len(data)
	if offset < 1 {
		return false
	}
	d.buf = data
	switch data[offset-1] >> 6 {
	case 0:
		d.offset = offset - 1
		d.state = uint32(data[offset-1] & 0x3f)
	case 1:
		if offset < 2 {
			return false
		}
		d.offset = offset - 2
		d.state = (uint32(data[offset-2]) | uint32(data[offset-1])<<8) & 0x3fff
	case 2:
		if offset < 3 {
			return false
		}
		d.offset = offset - 3
		d.state = (uint32(data[offset-3]) | uint32(data[offset-2])<<8 | uint32(data[offset-1])<<16) & 0x3fffff
	default:
		return false
	}
	d.state += ansP8LBase
	return d.state < ansP8LBase*ansIOBase
}

// decodeBit is rabs_desc_read. A decoder that was never started yields zeros.
func (d *bitDecoder) decodeBit() bool {
	// AnsP8 is a uint8, so a zero probability of zero wraps p to 0 exactly as in the reference.
	p := uint32(uint8(256 - uint32(d.probZero)))
	if d.state < ansP8LBase && d.offset > 0 {
		d.offset--
		d.state = d.state*ansIOBase + uint32(d.buf[d.offset])
	}
	x := d.state
	quot := x / 256
	rem := x % 256
	xn := quot * p
	val := rem < p
	if val {
		d.state = xn + rem
	} else {
		d.state = x - xn - p
	}
	return val
}

// decodeSymbols is DecodeSymbols: tagged or raw rANS coded unsigned values.
func decodeSymbols(numValues, numComponents int, b *buffer, out []uint32) bool {
	if numValues == 0 {
		return true
	}
	if numValues < 0 || numValues > len(out) || numComponents <= 0 {
		return false
	}
	scheme, ok := b.u8()
	if !ok {
		return false
	}
	switch scheme {
	case 0:
		return decodeTaggedSymbols(numValues, numComponents, b, out)
	case 1:
		return decodeRawSymbols(numValues, b, out)
	}
	return false
}

func decodeTaggedSymbols(numValues, numComponents int, b *buffer, out []uint32) bool {
	tags := newRansSymbolDecoder(5)
	if !tags.create(b) || !tags.startDecoding(b) {
		return false
	}
	if tags.numSymbols == 0 {
		return false
	}
	b.startBitDecoding(false)
	valueID := 0
	for i := 0; i < numValues; i += numComponents {
		bitLength := tags.decode()
		for j := 0; j < numComponents; j++ {
			v, ok := b.bits(bitLength)
			if !ok || valueID >= len(out) {
				b.endBitDecoding()
				return false
			}
			out[valueID] = v
			valueID++
		}
	}
	b.endBitDecoding()
	return true
}

func decodeRawSymbols(numValues int, b *buffer, out []uint32) bool {
	maxBitLength, ok := b.u8()
	if !ok || maxBitLength < 1 || maxBitLength > maxSymbolBits {
		return false
	}
	dec := newRansSymbolDecoder(int(maxBitLength))
	if !dec.create(b) {
		return false
	}
	if dec.numSymbols == 0 {
		return false
	}
	if !dec.startDecoding(b) {
		return false
	}
	for i := 0; i < numValues; i++ {
		out[i] = dec.decode()
	}
	return true
}
