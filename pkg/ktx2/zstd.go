package ktx2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

// Pure-Go RFC 8878 Zstandard frame decoder and ZLIB supercompression helper for KTX2 level payloads (SupercompressionZstandard = 2, SupercompressionZLIB = 3).

const (
	zstdMagic            uint32 = 0xFD2FB528
	zstdSkippableMagicLo uint32 = 0x184D2A50
	zstdSkippableMagicHi uint32 = 0x184D2A5F
	zstdMaxBlockSize            = 128 * 1024
	zstdMaxHuffLog              = 11
	zstdMaxFSELog               = 9
	zstdMaxWindowSize    uint64 = 128 * 1024 * 1024 // 128 MiB safety cap
)

// Predefined FSE distributions (RFC 8878 §3.1.1.3.2.2).
var zstdPredefLL = []int16{
	4, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 2, 1, 1, 1, 1, 1,
	-1, -1, -1, -1,
}

var zstdPredefOF = []int16{
	1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, -1, -1, -1, -1, -1,
}

var zstdPredefML = []int16{
	1, 4, 3, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1,
	-1, -1, -1, -1, -1,
}

// Sequence baselines and extra-bit counts (RFC 8878 Appendix A).
var zstdLLBase = [36]uint32{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 18, 20, 22, 24, 28, 32, 40, 48, 64, 128, 256, 512, 1024, 2048, 4096,
	8192, 16384, 32768, 65536,
}

var zstdLLBits = [36]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3, 4, 6, 7, 8, 9, 10, 11, 12,
	13, 14, 15, 16,
}

var zstdMLBase = [53]uint32{
	3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18,
	19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34,
	35, 37, 39, 41, 43, 47, 51, 59, 67, 83, 99, 131, 259, 515, 1027, 2051,
	4099, 8195, 16387, 32771, 65539,
}

var zstdMLBits = [53]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 2, 2, 3, 3, 4, 4, 5, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16,
}

type zstdFSEEntry struct {
	sym      uint8
	nbBits   uint8
	baseline uint16
}

type zstdFSETable struct {
	accLog uint8
	table  []zstdFSEEntry
}

type zstdHuffEntry struct {
	sym    uint8
	nbBits uint8
}

type zstdHuffTable struct {
	maxBits uint8
	table   []zstdHuffEntry
}

var (
	zstdPredefLLTable zstdFSETable
	zstdPredefOFTable zstdFSETable
	zstdPredefMLTable zstdFSETable
)

func init() {
	var err error
	if zstdPredefLLTable, err = buildFSETable(zstdPredefLL, 6); err != nil {
		panic(err)
	}
	if zstdPredefOFTable, err = buildFSETable(zstdPredefOF, 5); err != nil {
		panic(err)
	}
	if zstdPredefMLTable, err = buildFSETable(zstdPredefML, 6); err != nil {
		panic(err)
	}
}

func buildFSETable(norm []int16, accLog uint8) (zstdFSETable, error) {
	if accLog > zstdMaxFSELog {
		return zstdFSETable{}, fmt.Errorf("ktx2/zstd: invalid FSE accuracy log %d", accLog)
	}
	tableSize := 1 << accLog
	tableMask := tableSize - 1
	step := (tableSize >> 1) + (tableSize >> 3) + 3

	syms := make([]uint8, tableSize)
	stateNext := make([]uint16, len(norm))

	highThreshold := tableSize - 1
	for s, n := range norm {
		if n == -1 {
			if highThreshold < 0 {
				return zstdFSETable{}, errors.New("ktx2/zstd: FSE low-prob overflow")
			}
			syms[highThreshold] = uint8(s)
			highThreshold--
			stateNext[s] = 1
		} else if n > 0 {
			stateNext[s] = uint16(n)
		}
	}

	pos := 0
	for s, n := range norm {
		if n <= 0 {
			continue
		}
		for range int(n) {
			syms[pos] = uint8(s)
			pos = (pos + step) & tableMask
			for pos > highThreshold {
				pos = (pos + step) & tableMask
			}
		}
	}
	if pos != 0 {
		return zstdFSETable{}, errors.New("ktx2/zstd: FSE distribution sum mismatch")
	}

	tbl := make([]zstdFSEEntry, tableSize)
	for i := range tableSize {
		s := syms[i]
		next := stateNext[s]
		stateNext[s] = next + 1
		nbBits := accLog - uint8(bits.Len16(next)-1)
		baseline := (next << nbBits) - uint16(tableSize)
		tbl[i] = zstdFSEEntry{
			sym:      s,
			nbBits:   nbBits,
			baseline: baseline,
		}
	}
	return zstdFSETable{accLog: accLog, table: tbl}, nil
}

func buildRLEFSETable(sym uint8) zstdFSETable {
	return zstdFSETable{
		accLog: 0,
		table:  []zstdFSEEntry{{sym: sym, nbBits: 0, baseline: 0}},
	}
}

func readFSETable(src []byte, maxSym int, maxLog uint8) (zstdFSETable, int, error) {
	if len(src) == 0 {
		return zstdFSETable{}, 0, errors.New("ktx2/zstd: truncated FSE header")
	}
	var bitStream uint64
	var bitCount uint
	pos := 0
	refill := func() {
		for bitCount <= 56 && pos < len(src) {
			bitStream |= uint64(src[pos]) << bitCount
			bitCount += 8
			pos++
		}
	}
	refill()
	if bitCount < 4 {
		return zstdFSETable{}, 0, errors.New("ktx2/zstd: truncated FSE accuracy log")
	}
	accLog := uint8((bitStream & 0xF) + 5)
	bitStream >>= 4
	bitCount -= 4
	if accLog > maxLog {
		return zstdFSETable{}, 0, fmt.Errorf("ktx2/zstd: FSE accuracy log %d exceeds %d", accLog, maxLog)
	}

	remaining := (1 << accLog) + 1
	threshold := 1 << accLog
	nbBits := int(accLog) + 1
	norm := make([]int16, 0, maxSym+1)
	prevZero := false

	for remaining > 1 && len(norm) <= maxSym {
		if prevZero {
			for {
				refill()
				if bitCount < 2 {
					return zstdFSETable{}, 0, errors.New("ktx2/zstd: truncated FSE zero repeat")
				}
				rep := int(bitStream & 3)
				bitStream >>= 2
				bitCount -= 2
				for range rep {
					if len(norm) > maxSym {
						return zstdFSETable{}, 0, errors.New("ktx2/zstd: FSE symbol overflow")
					}
					norm = append(norm, 0)
				}
				if rep < 3 {
					break
				}
			}
			prevZero = false
			if len(norm) > maxSym {
				break
			}
		}

		maxVal := (2 * threshold) - 1 - remaining
		refill()
		if int(bitCount) < nbBits-1 {
			return zstdFSETable{}, 0, errors.New("ktx2/zstd: truncated FSE probability")
		}
		smallMask := uint64((1 << (nbBits - 1)) - 1)
		val := int(bitStream & smallMask)
		if val < maxVal {
			bitStream >>= uint(nbBits - 1)
			bitCount -= uint(nbBits - 1)
		} else {
			if int(bitCount) < nbBits {
				return zstdFSETable{}, 0, errors.New("ktx2/zstd: truncated FSE probability")
			}
			val = int(bitStream & uint64((1<<nbBits)-1))
			if val >= threshold {
				val -= maxVal
			}
			bitStream >>= uint(nbBits)
			bitCount -= uint(nbBits)
		}

		count := val - 1
		if count == -1 {
			remaining -= 1
		} else {
			remaining -= count
		}
		if remaining < 1 {
			return zstdFSETable{}, 0, errors.New("ktx2/zstd: FSE probability underflow")
		}
		norm = append(norm, int16(count))
		prevZero = count == 0

		for remaining < threshold {
			nbBits--
			threshold >>= 1
		}
	}

	if remaining != 1 {
		return zstdFSETable{}, 0, errors.New("ktx2/zstd: FSE table incomplete")
	}

	// Bytes consumed = pos - full unread bytes remaining in bitStream.
	consumed := pos - int(bitCount/8)
	tbl, err := buildFSETable(norm, accLog)
	if err != nil {
		return zstdFSETable{}, 0, err
	}
	return tbl, consumed, nil
}

// zstdBitReader reads bits backwards from the end of a Zstd bitstream (RFC 8878 §4.2.2).
type zstdBitReader struct {
	src    []byte
	bitPos int // total bits remaining to read from the stream
}

func newZstdBitReader(src []byte) (zstdBitReader, error) {
	if len(src) == 0 {
		return zstdBitReader{}, errors.New("ktx2/zstd: empty bitstream")
	}
	last := src[len(src)-1]
	if last == 0 {
		return zstdBitReader{}, errors.New("ktx2/zstd: missing bitstream sentinel bit")
	}
	// Highest set bit in last byte is the sentinel; valid bits lie strictly below it.
	highBit := bits.Len8(last) - 1 // 0..7
	totalBits := (len(src)-1)*8 + highBit
	return zstdBitReader{src: src, bitPos: totalBits}, nil
}

func (r *zstdBitReader) read(n uint8) (uint32, error) {
	if n == 0 {
		return 0, nil
	}
	if int(n) > r.bitPos {
		return 0, errors.New("ktx2/zstd: bitstream underrun")
	}
	r.bitPos -= int(n)
	return r.extract(r.bitPos, int(n)), nil
}

// readPadded reads n bits, zero-padding past the start of the stream (used by Huffman final symbols).
func (r *zstdBitReader) readPadded(n uint8) uint32 {
	if n == 0 {
		return 0
	}
	if r.bitPos >= int(n) {
		r.bitPos -= int(n)
		return r.extract(r.bitPos, int(n))
	}
	avail := r.bitPos
	r.bitPos = 0
	if avail <= 0 {
		return 0
	}
	return r.extract(0, avail) << uint(int(n)-avail)
}

func (r *zstdBitReader) peekPadded(n uint8) uint32 {
	if n == 0 {
		return 0
	}
	if r.bitPos >= int(n) {
		return r.extract(r.bitPos-int(n), int(n))
	}
	if r.bitPos <= 0 {
		return 0
	}
	return r.extract(0, r.bitPos) << uint(int(n)-r.bitPos)
}

func (r *zstdBitReader) extract(startBit, count int) uint32 {
	var v uint32
	for i := range count {
		bp := startBit + i
		if (r.src[bp>>3]>>(bp&7))&1 != 0 {
			v |= 1 << i
		}
	}
	return v
}

func buildHuffTable(weights []uint8) (zstdHuffTable, error) {
	if len(weights) == 0 || len(weights) > 255 {
		return zstdHuffTable{}, errors.New("ktx2/zstd: invalid Huffman weight count")
	}
	var weightSum uint32
	var rankCount [zstdMaxHuffLog + 2]uint32
	for _, w := range weights {
		if w > zstdMaxHuffLog {
			return zstdHuffTable{}, fmt.Errorf("ktx2/zstd: Huffman weight %d exceeds max", w)
		}
		if w > 0 {
			weightSum += 1 << (w - 1)
			rankCount[w]++
		}
	}
	if weightSum == 0 {
		return zstdHuffTable{}, errors.New("ktx2/zstd: empty Huffman tree")
	}
	maxBits := uint8(bits.Len32(weightSum))
	if maxBits > zstdMaxHuffLog {
		return zstdHuffTable{}, fmt.Errorf("ktx2/zstd: Huffman maxBits %d exceeds %d", maxBits, zstdMaxHuffLog)
	}
	leftOver := (uint32(1) << maxBits) - weightSum
	if leftOver == 0 || (leftOver&(leftOver-1)) != 0 {
		return zstdHuffTable{}, errors.New("ktx2/zstd: invalid Huffman weight sum")
	}
	lastWeight := uint8(bits.TrailingZeros32(leftOver) + 1)
	if lastWeight > maxBits {
		return zstdHuffTable{}, errors.New("ktx2/zstd: invalid last Huffman weight")
	}
	rankCount[lastWeight]++

	allWeights := make([]uint8, len(weights)+1)
	copy(allWeights, weights)
	allWeights[len(weights)] = lastWeight

	var rankStart [zstdMaxHuffLog + 2]uint32
	nextCode := uint32(0)
	for w := uint8(1); w <= maxBits; w++ {
		rankStart[w] = nextCode
		nextCode += rankCount[w] << (w - 1)
	}
	tableSize := uint32(1) << maxBits
	if nextCode != tableSize {
		return zstdHuffTable{}, errors.New("ktx2/zstd: incomplete Huffman code space")
	}

	tbl := make([]zstdHuffEntry, tableSize)
	for sym, w := range allWeights {
		if w == 0 {
			continue
		}
		nbBits := maxBits + 1 - w
		span := uint32(1) << (w - 1)
		start := rankStart[w]
		rankStart[w] += span
		for i := range span {
			tbl[start+i] = zstdHuffEntry{sym: uint8(sym), nbBits: nbBits}
		}
	}
	return zstdHuffTable{maxBits: maxBits, table: tbl}, nil
}

func readHuffTree(src []byte) (zstdHuffTable, int, error) {
	if len(src) == 0 {
		return zstdHuffTable{}, 0, errors.New("ktx2/zstd: truncated Huffman header")
	}
	hdr := int(src[0])
	if hdr >= 128 {
		numWeights := hdr - 127
		byteLen := (numWeights + 1) / 2
		if 1+byteLen > len(src) {
			return zstdHuffTable{}, 0, errors.New("ktx2/zstd: truncated direct Huffman weights")
		}
		weights := make([]uint8, numWeights)
		for i := range numWeights {
			b := src[1+i/2]
			if i%2 == 0 {
				weights[i] = b >> 4
			} else {
				weights[i] = b & 0xF
			}
		}
		tbl, err := buildHuffTable(weights)
		return tbl, 1 + byteLen, err
	}

	compSize := hdr
	if 1+compSize > len(src) {
		return zstdHuffTable{}, 0, errors.New("ktx2/zstd: truncated FSE Huffman weights")
	}
	fseData := src[1 : 1+compSize]
	fseTbl, fseHeaderLen, err := readFSETable(fseData, 12, 6)
	if err != nil {
		return zstdHuffTable{}, 0, err
	}
	if fseHeaderLen >= len(fseData) {
		return zstdHuffTable{}, 0, errors.New("ktx2/zstd: missing FSE Huffman bitstream")
	}
	br, err := newZstdBitReader(fseData[fseHeaderLen:])
	if err != nil {
		return zstdHuffTable{}, 0, err
	}
	state1, err := br.read(fseTbl.accLog)
	if err != nil {
		return zstdHuffTable{}, 0, err
	}
	state2, err := br.read(fseTbl.accLog)
	if err != nil {
		return zstdHuffTable{}, 0, err
	}

	weights := make([]uint8, 0, 255)
	for {
		if len(weights) >= 255 {
			return zstdHuffTable{}, 0, errors.New("ktx2/zstd: too many Huffman weights")
		}
		e1 := fseTbl.table[state1]
		weights = append(weights, e1.sym)
		if br.bitPos < int(e1.nbBits) {
			weights = append(weights, fseTbl.table[state2].sym)
			break
		}
		b1, _ := br.read(e1.nbBits)
		state1 = uint32(e1.baseline) + b1

		if len(weights) >= 255 {
			return zstdHuffTable{}, 0, errors.New("ktx2/zstd: too many Huffman weights")
		}
		e2 := fseTbl.table[state2]
		weights = append(weights, e2.sym)
		if br.bitPos < int(e2.nbBits) {
			weights = append(weights, fseTbl.table[state1].sym)
			break
		}
		b2, _ := br.read(e2.nbBits)
		state2 = uint32(e2.baseline) + b2
	}
	tbl, err := buildHuffTable(weights)
	return tbl, 1 + compSize, err
}

func decodeHuffStream(tbl zstdHuffTable, src []byte, out []byte) error {
	br, err := newZstdBitReader(src)
	if err != nil {
		return err
	}
	for i := range out {
		idx := br.peekPadded(tbl.maxBits)
		e := tbl.table[idx]
		if e.nbBits == 0 || int(e.nbBits) > br.bitPos {
			return errors.New("ktx2/zstd: corrupt Huffman stream")
		}
		br.bitPos -= int(e.nbBits)
		out[i] = e.sym
	}
	if br.bitPos != 0 {
		return errors.New("ktx2/zstd: trailing bits in Huffman stream")
	}
	return nil
}

type zstdFrameState struct {
	rep     [3]uint32
	hasHuff bool
	huff    zstdHuffTable
	hasLL   bool
	llTable zstdFSETable
	hasOF   bool
	ofTable zstdFSETable
	hasML   bool
	mlTable zstdFSETable
}

func (st *zstdFrameState) decodeLiterals(block []byte) ([]byte, int, error) {
	if len(block) == 0 {
		return nil, 0, errors.New("ktx2/zstd: empty compressed block")
	}
	b0 := block[0]
	litType := b0 & 3
	sizeFmt := (b0 >> 2) & 3

	if litType == 0 || litType == 1 {
		// Raw or RLE literals.
		var regenSize int
		var hdrLen int
		switch sizeFmt {
		case 0, 2:
			regenSize = int(b0 >> 3)
			hdrLen = 1
		case 1:
			if len(block) < 2 {
				return nil, 0, errors.New("ktx2/zstd: truncated literal header")
			}
			regenSize = int(b0>>4) | (int(block[1]) << 4)
			hdrLen = 2
		case 3:
			if len(block) < 3 {
				return nil, 0, errors.New("ktx2/zstd: truncated literal header")
			}
			regenSize = int(b0>>4) | (int(block[1]) << 4) | (int(block[2]) << 12)
			hdrLen = 3
		}
		if regenSize > zstdMaxBlockSize {
			return nil, 0, errors.New("ktx2/zstd: literal size exceeds max block size")
		}
		if litType == 0 {
			if hdrLen+regenSize > len(block) {
				return nil, 0, errors.New("ktx2/zstd: truncated raw literals")
			}
			return block[hdrLen : hdrLen+regenSize], hdrLen + regenSize, nil
		}
		// RLE literals.
		if hdrLen+1 > len(block) {
			return nil, 0, errors.New("ktx2/zstd: truncated RLE literal")
		}
		val := block[hdrLen]
		out := make([]byte, regenSize)
		for i := range out {
			out[i] = val
		}
		return out, hdrLen + 1, nil
	}

	// Huffman compressed (2) or treeless repeat (3).
	var regenSize, compSize, hdrLen int
	singleStream := sizeFmt == 0
	switch sizeFmt {
	case 0, 1:
		if len(block) < 3 {
			return nil, 0, errors.New("ktx2/zstd: truncated Huffman literal header")
		}
		v := uint32(block[0]) | (uint32(block[1]) << 8) | (uint32(block[2]) << 16)
		regenSize = int((v >> 4) & 0x3FF)
		compSize = int((v >> 14) & 0x3FF)
		hdrLen = 3
	case 2:
		if len(block) < 4 {
			return nil, 0, errors.New("ktx2/zstd: truncated Huffman literal header")
		}
		v := binary.LittleEndian.Uint32(block[0:4])
		regenSize = int((v >> 4) & 0x3FFF)
		compSize = int((v >> 18) & 0x3FFF)
		hdrLen = 4
	case 3:
		if len(block) < 5 {
			return nil, 0, errors.New("ktx2/zstd: truncated Huffman literal header")
		}
		v := uint64(binary.LittleEndian.Uint32(block[0:4])) | (uint64(block[4]) << 32)
		regenSize = int((v >> 4) & 0x3FFFF)
		compSize = int((v >> 22) & 0x3FFFF)
		hdrLen = 5
	}
	if regenSize > zstdMaxBlockSize || hdrLen+compSize > len(block) {
		return nil, 0, errors.New("ktx2/zstd: invalid Huffman literal size")
	}

	payload := block[hdrLen : hdrLen+compSize]
	if litType == 2 {
		tbl, treeLen, err := readHuffTree(payload)
		if err != nil {
			return nil, 0, err
		}
		st.huff = tbl
		st.hasHuff = true
		payload = payload[treeLen:]
	} else if !st.hasHuff {
		return nil, 0, errors.New("ktx2/zstd: treeless literals without prior Huffman tree")
	}

	out := make([]byte, regenSize)
	if singleStream {
		if err := decodeHuffStream(st.huff, payload, out); err != nil {
			return nil, 0, err
		}
		return out, hdrLen + compSize, nil
	}

	if len(payload) < 6 {
		return nil, 0, errors.New("ktx2/zstd: truncated 4-stream jump table")
	}
	s1 := int(binary.LittleEndian.Uint16(payload[0:2]))
	s2 := int(binary.LittleEndian.Uint16(payload[2:4]))
	s3 := int(binary.LittleEndian.Uint16(payload[4:6]))
	streams := payload[6:]
	if s1+s2+s3 > len(streams) {
		return nil, 0, errors.New("ktx2/zstd: 4-stream jump table out of bounds")
	}
	partLen := (regenSize + 3) / 4
	if partLen*3 > regenSize {
		return nil, 0, errors.New("ktx2/zstd: invalid 4-stream regen size")
	}
	if err := decodeHuffStream(st.huff, streams[:s1], out[0:partLen]); err != nil {
		return nil, 0, err
	}
	if err := decodeHuffStream(st.huff, streams[s1:s1+s2], out[partLen:2*partLen]); err != nil {
		return nil, 0, err
	}
	if err := decodeHuffStream(st.huff, streams[s1+s2:s1+s2+s3], out[2*partLen:3*partLen]); err != nil {
		return nil, 0, err
	}
	if err := decodeHuffStream(st.huff, streams[s1+s2+s3:], out[3*partLen:]); err != nil {
		return nil, 0, err
	}
	return out, hdrLen + compSize, nil
}

func (st *zstdFrameState) decodeBlock(block []byte, out []byte, maxOut uint64) ([]byte, error) {
	lits, pos, err := st.decodeLiterals(block)
	if err != nil {
		return nil, err
	}
	if pos >= len(block) {
		return nil, errors.New("ktx2/zstd: missing sequences section")
	}
	seq := block[pos:]
	var numSeq int
	var sp int
	switch {
	case seq[0] == 0:
		numSeq = 0
		sp = 1
	case seq[0] < 128:
		numSeq = int(seq[0])
		sp = 1
	case seq[0] < 255:
		if len(seq) < 2 {
			return nil, errors.New("ktx2/zstd: truncated sequence count")
		}
		numSeq = ((int(seq[0]) - 128) << 8) + int(seq[1])
		sp = 2
	default:
		if len(seq) < 3 {
			return nil, errors.New("ktx2/zstd: truncated sequence count")
		}
		numSeq = int(seq[1]) + (int(seq[2]) << 8) + 0x7F00
		sp = 3
	}

	if numSeq == 0 {
		if uint64(len(out))+uint64(len(lits)) > maxOut {
			return nil, errors.New("ktx2/zstd: decompressed output exceeds limit")
		}
		return append(out, lits...), nil
	}
	if sp >= len(seq) {
		return nil, errors.New("ktx2/zstd: missing sequence modes byte")
	}
	modes := seq[sp]
	sp++
	if modes&3 != 0 {
		return nil, errors.New("ktx2/zstd: non-zero reserved sequence mode bits")
	}
	llMode := (modes >> 6) & 3
	ofMode := (modes >> 4) & 3
	mlMode := (modes >> 2) & 3

	loadTable := func(mode uint8, predef zstdFSETable, maxSym int, maxLog uint8, dst *zstdFSETable, has *bool) error {
		switch mode {
		case 0:
			*dst = predef
			*has = true
		case 1:
			if sp >= len(seq) {
				return errors.New("ktx2/zstd: truncated RLE sequence symbol")
			}
			sym := seq[sp]
			sp++
			if int(sym) > maxSym {
				return fmt.Errorf("ktx2/zstd: RLE symbol %d exceeds max %d", sym, maxSym)
			}
			*dst = buildRLEFSETable(sym)
			*has = true
		case 2:
			tbl, n, err := readFSETable(seq[sp:], maxSym, maxLog)
			if err != nil {
				return err
			}
			sp += n
			*dst = tbl
			*has = true
		case 3:
			if !*has {
				return errors.New("ktx2/zstd: repeat FSE table without prior table")
			}
		}
		return nil
	}

	if err := loadTable(llMode, zstdPredefLLTable, 35, 9, &st.llTable, &st.hasLL); err != nil {
		return nil, err
	}
	if err := loadTable(ofMode, zstdPredefOFTable, 31, 8, &st.ofTable, &st.hasOF); err != nil {
		return nil, err
	}
	if err := loadTable(mlMode, zstdPredefMLTable, 52, 9, &st.mlTable, &st.hasML); err != nil {
		return nil, err
	}

	if sp >= len(seq) {
		return nil, errors.New("ktx2/zstd: missing sequence bitstream")
	}
	br, err := newZstdBitReader(seq[sp:])
	if err != nil {
		return nil, err
	}

	llState, err := br.read(st.llTable.accLog)
	if err != nil {
		return nil, err
	}
	ofState, err := br.read(st.ofTable.accLog)
	if err != nil {
		return nil, err
	}
	mlState, err := br.read(st.mlTable.accLog)
	if err != nil {
		return nil, err
	}

	litPos := 0
	for i := range numSeq {
		llEntry := st.llTable.table[llState]
		ofEntry := st.ofTable.table[ofState]
		mlEntry := st.mlTable.table[mlState]

		ofCode := ofEntry.sym
		mlCode := mlEntry.sym
		llCode := llEntry.sym
		if llCode > 35 || mlCode > 52 || ofCode > 31 {
			return nil, errors.New("ktx2/zstd: sequence symbol out of range")
		}

		ofExtra, err := br.read(ofCode)
		if err != nil {
			return nil, err
		}
		mlExtra, err := br.read(zstdMLBits[mlCode])
		if err != nil {
			return nil, err
		}
		llExtra, err := br.read(zstdLLBits[llCode])
		if err != nil {
			return nil, err
		}

		ofVal := (uint32(1) << ofCode) + ofExtra
		matchLen := int(zstdMLBase[mlCode] + mlExtra)
		litLen := int(zstdLLBase[llCode] + llExtra)

		if i < numSeq-1 {
			llBits, err := br.read(llEntry.nbBits)
			if err != nil {
				return nil, err
			}
			mlBits, err := br.read(mlEntry.nbBits)
			if err != nil {
				return nil, err
			}
			ofBits, err := br.read(ofEntry.nbBits)
			if err != nil {
				return nil, err
			}
			llState = uint32(llEntry.baseline) + llBits
			mlState = uint32(mlEntry.baseline) + mlBits
			ofState = uint32(ofEntry.baseline) + ofBits
		}

		var actualOffset uint32
		if ofCode > 1 {
			actualOffset = ofVal - 3
			st.rep[2] = st.rep[1]
			st.rep[1] = st.rep[0]
			st.rep[0] = actualOffset
		} else {
			idx := ofVal
			if litLen == 0 {
				idx++
			}
			switch idx {
			case 1:
				actualOffset = st.rep[0]
			case 2:
				actualOffset = st.rep[1]
				st.rep[1] = st.rep[0]
				st.rep[0] = actualOffset
			case 3:
				actualOffset = st.rep[2]
				st.rep[2] = st.rep[1]
				st.rep[1] = st.rep[0]
				st.rep[0] = actualOffset
			case 4:
				if st.rep[0] <= 1 {
					return nil, errors.New("ktx2/zstd: invalid repeat offset")
				}
				actualOffset = st.rep[0] - 1
				st.rep[2] = st.rep[1]
				st.rep[1] = st.rep[0]
				st.rep[0] = actualOffset
			default:
				return nil, errors.New("ktx2/zstd: invalid repeat offset index")
			}
		}

		if litPos+litLen > len(lits) {
			return nil, errors.New("ktx2/zstd: literal length exceeds buffer")
		}
		if uint64(len(out))+uint64(litLen)+uint64(matchLen) > maxOut {
			return nil, errors.New("ktx2/zstd: decompressed output exceeds limit")
		}
		out = append(out, lits[litPos:litPos+litLen]...)
		litPos += litLen

		if actualOffset == 0 || int(actualOffset) > len(out) {
			return nil, errors.New("ktx2/zstd: match offset out of bounds")
		}
		srcPos := len(out) - int(actualOffset)
		for range matchLen {
			out = append(out, out[srcPos])
			srcPos++
		}
	}

	if br.bitPos != 0 {
		return nil, errors.New("ktx2/zstd: trailing bits in sequence bitstream")
	}
	if litPos < len(lits) {
		rem := len(lits) - litPos
		if uint64(len(out))+uint64(rem) > maxOut {
			return nil, errors.New("ktx2/zstd: decompressed output exceeds limit")
		}
		out = append(out, lits[litPos:]...)
	}
	return out, nil
}

// decompressZstd decodes one or more concatenated RFC 8878 Zstandard frames (skipping skippable frames) while enforcing a strict maximum output size.
func decompressZstd(src []byte, maxOut uint64) ([]byte, error) {
	if len(src) < 4 {
		return nil, errors.New("ktx2/zstd: truncated frame")
	}
	var out []byte
	pos := 0
	for pos < len(src) {
		if pos+4 > len(src) {
			return nil, errors.New("ktx2/zstd: truncated frame magic")
		}
		magic := binary.LittleEndian.Uint32(src[pos : pos+4])
		pos += 4

		if magic >= zstdSkippableMagicLo && magic <= zstdSkippableMagicHi {
			if pos+4 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated skippable frame size")
			}
			skip := uint64(binary.LittleEndian.Uint32(src[pos : pos+4]))
			pos += 4
			if skip > uint64(len(src)-pos) {
				return nil, errors.New("ktx2/zstd: skippable frame out of bounds")
			}
			pos += int(skip)
			continue
		}

		if magic != zstdMagic {
			return nil, fmt.Errorf("ktx2/zstd: invalid frame magic 0x%08x", magic)
		}
		if pos >= len(src) {
			return nil, errors.New("ktx2/zstd: truncated frame header descriptor")
		}
		fhd := src[pos]
		pos++
		fcsFlag := (fhd >> 6) & 3
		singleSeg := (fhd>>5)&1 != 0
		if (fhd>>3)&1 != 0 {
			return nil, errors.New("ktx2/zstd: non-zero reserved frame header bit")
		}
		hasChecksum := (fhd>>2)&1 != 0
		dictFlag := fhd & 3

		if !singleSeg {
			if pos >= len(src) {
				return nil, errors.New("ktx2/zstd: truncated window descriptor")
			}
			wd := src[pos]
			pos++
			exp := uint(wd >> 3)
			mant := uint64(wd & 7)
			if exp > 30 {
				return nil, errors.New("ktx2/zstd: window size too large")
			}
			base := uint64(1) << (10 + exp)
			winSize := base + (base>>3)*mant
			if winSize > zstdMaxWindowSize {
				return nil, fmt.Errorf("ktx2/zstd: window size %d exceeds limit", winSize)
			}
		}

		var dictID uint32
		switch dictFlag {
		case 1:
			if pos+1 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated dictionary ID")
			}
			dictID = uint32(src[pos])
			pos++
		case 2:
			if pos+2 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated dictionary ID")
			}
			dictID = uint32(binary.LittleEndian.Uint16(src[pos : pos+2]))
			pos += 2
		case 3:
			if pos+4 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated dictionary ID")
			}
			dictID = binary.LittleEndian.Uint32(src[pos : pos+4])
			pos += 4
		}
		if dictID != 0 {
			return nil, errors.New("ktx2/zstd: custom Zstandard dictionaries are not supported in KTX2")
		}

		var fcs uint64
		hasFCS := false
		switch fcsFlag {
		case 0:
			if singleSeg {
				if pos+1 > len(src) {
					return nil, errors.New("ktx2/zstd: truncated frame content size")
				}
				fcs = uint64(src[pos])
				pos++
				hasFCS = true
			}
		case 1:
			if pos+2 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated frame content size")
			}
			fcs = uint64(binary.LittleEndian.Uint16(src[pos:pos+2])) + 256
			pos += 2
			hasFCS = true
		case 2:
			if pos+4 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated frame content size")
			}
			fcs = uint64(binary.LittleEndian.Uint32(src[pos : pos+4]))
			pos += 4
			hasFCS = true
		case 3:
			if pos+8 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated frame content size")
			}
			fcs = binary.LittleEndian.Uint64(src[pos : pos+8])
			pos += 8
			hasFCS = true
		}
		// Each frame decodes into its own buffer, so a match offset cannot reach back into an earlier frame, and its budget is what the earlier frames left.
		budget := maxOut - uint64(len(out))
		if hasFCS && fcs > budget {
			return nil, fmt.Errorf("ktx2/zstd: declared content size %d exceeds limit %d", fcs, maxOut)
		}
		var frame []byte
		if hasFCS {
			frame = make([]byte, 0, fcs)
		}
		st := zstdFrameState{rep: [3]uint32{1, 4, 8}}
		for {
			if pos+3 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated block header")
			}
			bh := uint32(src[pos]) | (uint32(src[pos+1]) << 8) | (uint32(src[pos+2]) << 16)
			pos += 3
			lastBlock := (bh & 1) != 0
			blockType := (bh >> 1) & 3
			blockSize := int(bh >> 3)
			if blockSize > zstdMaxBlockSize {
				return nil, fmt.Errorf("ktx2/zstd: block size %d exceeds maximum", blockSize)
			}

			switch blockType {
			case 0: // Raw block
				if pos+blockSize > len(src) {
					return nil, errors.New("ktx2/zstd: truncated raw block")
				}
				if uint64(len(frame))+uint64(blockSize) > budget {
					return nil, errors.New("ktx2/zstd: decompressed output exceeds limit")
				}
				frame = append(frame, src[pos:pos+blockSize]...)
				pos += blockSize
			case 1: // RLE block
				if pos+1 > len(src) {
					return nil, errors.New("ktx2/zstd: truncated RLE block")
				}
				val := src[pos]
				pos++
				if uint64(len(frame))+uint64(blockSize) > budget {
					return nil, errors.New("ktx2/zstd: decompressed output exceeds limit")
				}
				for range blockSize {
					frame = append(frame, val)
				}
			case 2: // Compressed block
				if pos+blockSize > len(src) {
					return nil, errors.New("ktx2/zstd: truncated compressed block")
				}
				var err error
				frame, err = st.decodeBlock(src[pos:pos+blockSize], frame, budget)
				if err != nil {
					return nil, err
				}
				pos += blockSize
			default:
				return nil, errors.New("ktx2/zstd: reserved block type 3")
			}

			if lastBlock {
				break
			}
		}

		if hasChecksum {
			if pos+4 > len(src) {
				return nil, errors.New("ktx2/zstd: truncated content checksum")
			}
			want := binary.LittleEndian.Uint32(src[pos : pos+4])
			if got := uint32(xxhash64(frame)); got != want {
				return nil, fmt.Errorf("ktx2/zstd: content checksum %#08x, frame declared %#08x", got, want)
			}
			pos += 4
		}
		if hasFCS && uint64(len(frame)) != fcs {
			return nil, fmt.Errorf("ktx2/zstd: decoded %d bytes, frame header declared %d", len(frame), fcs)
		}
		// The first frame becomes the output itself, so a single-frame level is held once rather than copied into a second buffer of the same size.
		if out == nil {
			out = frame
		} else {
			out = append(out, frame...)
		}
	}
	return out, nil
}

func decompressZLIB(src []byte, maxOut uint64) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("ktx2/zlib: %w", err)
	}
	defer zr.Close()
	limited := io.LimitReader(zr, int64(maxOut)+1)
	out, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("ktx2/zlib: %w", err)
	}
	if uint64(len(out)) > maxOut {
		return nil, fmt.Errorf("ktx2/zlib: decompressed output exceeds limit %d", maxOut)
	}
	return out, nil
}

const (
	xxhPrime1 uint64 = 11400714785074694791
	xxhPrime2 uint64 = 14029467366897019727
	xxhPrime3 uint64 = 1609587929392839161
	xxhPrime4 uint64 = 9650029242287828579
	xxhPrime5 uint64 = 2870177450012600261
)

func xxhRound(acc, input uint64) uint64 {
	acc += input * xxhPrime2
	acc = bits.RotateLeft64(acc, 31)
	return acc * xxhPrime1
}

func xxhMerge(acc, val uint64) uint64 {
	acc ^= xxhRound(0, val)
	return acc*xxhPrime1 + xxhPrime4
}

// xxhash64 returns the XXH64 hash of b with seed 0, whose low 32 bits are a Zstandard frame's content checksum (RFC 8878, section 3.1.1).
func xxhash64(b []byte) uint64 {
	n := len(b)
	var h uint64
	if n >= 32 {
		p1, p2 := xxhPrime1, xxhPrime2
		v1 := p1 + p2
		v2 := p2
		v3 := uint64(0)
		v4 := -p1
		for len(b) >= 32 {
			v1 = xxhRound(v1, binary.LittleEndian.Uint64(b[0:8]))
			v2 = xxhRound(v2, binary.LittleEndian.Uint64(b[8:16]))
			v3 = xxhRound(v3, binary.LittleEndian.Uint64(b[16:24]))
			v4 = xxhRound(v4, binary.LittleEndian.Uint64(b[24:32]))
			b = b[32:]
		}
		h = bits.RotateLeft64(v1, 1) + bits.RotateLeft64(v2, 7) + bits.RotateLeft64(v3, 12) + bits.RotateLeft64(v4, 18)
		h = xxhMerge(h, v1)
		h = xxhMerge(h, v2)
		h = xxhMerge(h, v3)
		h = xxhMerge(h, v4)
	} else {
		h = xxhPrime5
	}
	h += uint64(n)
	for len(b) >= 8 {
		h ^= xxhRound(0, binary.LittleEndian.Uint64(b[:8]))
		h = bits.RotateLeft64(h, 27)*xxhPrime1 + xxhPrime4
		b = b[8:]
	}
	if len(b) >= 4 {
		h ^= uint64(binary.LittleEndian.Uint32(b[:4])) * xxhPrime1
		h = bits.RotateLeft64(h, 23)*xxhPrime2 + xxhPrime3
		b = b[4:]
	}
	for _, c := range b {
		h ^= uint64(c) * xxhPrime5
		h = bits.RotateLeft64(h, 11) * xxhPrime1
	}
	h ^= h >> 33
	h *= xxhPrime2
	h ^= h >> 29
	h *= xxhPrime3
	h ^= h >> 32
	return h
}
