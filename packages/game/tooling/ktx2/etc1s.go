package ktx2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

// Pure-Go Khronos BasisLZ / ETC1S supercompression and slice decoder
// implementing KTX2 v2.0 §3.9 and Khronos Data Format Specification §22.

const (
	basisHuffMaxCodeSize         = 16
	basisHuffFastLookupBits      = 10
	basisHuffFastLookupSize      = 1 << basisHuffFastLookupBits
	basisHuffMaxSymsLog2         = 14
	basisHuffMaxSyms             = 1 << basisHuffMaxSymsLog2
	basisHuffTotalCodeLenCodes   = 21
	basisHuffSmallZeroRunCode    = 17
	basisHuffBigZeroRunCode      = 18
	basisHuffSmallRepeatCode     = 19
	basisHuffBigRepeatCode       = 20
	basisHuffSmallZeroRunMin     = 3
	basisHuffSmallZeroRunBits    = 3
	basisHuffBigZeroRunMin       = 11
	basisHuffBigZeroRunBits      = 7
	basisHuffSmallRepeatMin      = 3
	basisHuffSmallRepeatBits     = 2
	basisHuffBigRepeatMin        = 7
	basisHuffBigRepeatBits       = 7
	basisEndpointPredRepeatSym   = 256
	basisEndpointPredMinRepeat   = 3
	basisEndpointPredCountVLCBit = 4
	basisSelectorRLECountThresh  = 3
	basisSelectorRLECountTotal   = 64
	basisColor5Pal0PrevHi        = 9
	basisColor5Pal1PrevHi        = 21
	basisImageFlagPFrame         = 2
	basisSGDHeaderSize           = 20
	basisSGDImageDescSize        = 20
)

var basisHuffSortedCodeLenCodes = [basisHuffTotalCodeLenCodes]uint8{
	basisHuffSmallZeroRunCode, basisHuffBigZeroRunCode,
	basisHuffSmallRepeatCode, basisHuffBigRepeatCode,
	0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15, 16,
}

// ETC1S intensity modifier table ordered by Basis Universal 2-bit selector value (0..3 -> -large, -small, +small, +large).
var etc1sModifierTable = [8][4]int{
	{-8, -2, 2, 8},
	{-17, -5, 5, 17},
	{-29, -9, 9, 29},
	{-42, -13, 13, 42},
	{-60, -18, 18, 60},
	{-80, -24, 24, 80},
	{-106, -33, 33, 106},
	{-183, -47, 47, 183},
}

type basisLZImageDesc struct {
	imageFlags           uint32
	rgbSliceByteOffset   uint32
	rgbSliceByteLength   uint32
	alphaSliceByteOffset uint32
	alphaSliceByteLength uint32
}

type etc1sEndpoint struct {
	color5 [3]uint8
	inten  uint8
}

type etc1sSelector struct {
	rows [4]uint8 // 4 rows x 4 2-bit selectors (col x is (rows[y] >> (x*2)) & 3)
}

type basisLZGlobalData struct {
	imageDescs             []basisLZImageDesc
	endpoints              []etc1sEndpoint
	selectors              []etc1sSelector
	endpointPredModel      *basisHuffTable
	deltaEndpointModel     *basisHuffTable
	selectorModel          *basisHuffTable
	selectorHistoryRLE     *basisHuffTable
	selectorHistoryBufSize int
}

type basisBitReader struct {
	data    []byte
	pos     int
	bitBuf  uint64
	bitSize uint
}

func newBasisBitReader(data []byte) basisBitReader {
	return basisBitReader{data: data}
}

func (r *basisBitReader) refill(need uint) {
	for r.bitSize < need {
		var b uint64
		if r.pos < len(r.data) {
			b = uint64(r.data[r.pos])
			r.pos++
		}
		r.bitBuf |= b << r.bitSize
		r.bitSize += 8
	}
}

func (r *basisBitReader) readBits(n uint) uint32 {
	if n == 0 {
		return 0
	}
	r.refill(n)
	v := uint32(r.bitBuf & ((uint64(1) << n) - 1))
	r.bitBuf >>= n
	r.bitSize -= n
	return v
}

func (r *basisBitReader) decodeVLC(chunkBits uint) (uint32, error) {
	chunkSize := uint32(1) << chunkBits
	chunkMask := chunkSize - 1
	var v uint32
	var ofs uint
	for {
		s := r.readBits(chunkBits + 1)
		v |= (s & chunkMask) << ofs
		ofs += chunkBits
		if s&chunkSize == 0 {
			return v, nil
		}
		if ofs >= 32 {
			return 0, errors.New("ktx2: BasisLZ VLC overflow")
		}
	}
}

type basisHuffTable struct {
	numSyms int
	lookup  [basisHuffFastLookupSize]int32
	tree    []int32
}

func buildBasisHuffTable(codeSizes []uint8) (*basisHuffTable, error) {
	totalSyms := len(codeSizes)
	if totalSyms == 0 {
		return &basisHuffTable{}, nil
	}

	var count [basisHuffMaxCodeSize + 1]uint32
	for _, sz := range codeSizes {
		if sz > basisHuffMaxCodeSize {
			return nil, fmt.Errorf("ktx2: BasisLZ Huffman code size %d exceeds %d", sz, basisHuffMaxCodeSize)
		}
		count[sz]++
	}

	var nextCode [basisHuffMaxCodeSize + 1]uint32
	var usedSyms, total uint32
	for i := 1; i <= basisHuffMaxCodeSize; i++ {
		usedSyms += count[i]
		nextCode[i] = total
		total = (total + count[i]) << 1
	}
	// Kraft completeness check matching basisu's huffman_decoding_table::init.
	// After i=16, total == 1<<17 when Kraft sum is 1.
	if total != (1<<(basisHuffMaxCodeSize+1)) && usedSyms != 1 {
		return nil, errors.New("ktx2: BasisLZ incomplete or invalid Huffman tree")
	}

	ht := &basisHuffTable{
		numSyms: totalSyms,
		tree:    make([]int32, totalSyms*2),
	}

	treeNext := int32(-1)
	for sym, sz := range codeSizes {
		if sz == 0 {
			continue
		}
		cur := nextCode[sz]
		nextCode[sz]++

		var rev uint32
		for l := sz; l > 0; l-- {
			rev = (rev << 1) | (cur & 1)
			cur >>= 1
		}

		if sz <= basisHuffFastLookupBits {
			entry := int32(uint32(sz)<<16 | uint32(sym))
			step := uint32(1) << sz
			for idx := rev; idx < basisHuffFastLookupSize; idx += step {
				if ht.lookup[idx] != 0 {
					return nil, errors.New("ktx2: BasisLZ overlapping Huffman code")
				}
				ht.lookup[idx] = entry
			}
			continue
		}

		lowIdx := rev & (basisHuffFastLookupSize - 1)
		treeCur := ht.lookup[lowIdx]
		if treeCur == 0 {
			ht.lookup[lowIdx] = treeNext
			treeCur = treeNext
			treeNext -= 2
		} else if treeCur > 0 {
			return nil, errors.New("ktx2: BasisLZ overlapping Huffman prefix")
		}

		rev >>= basisHuffFastLookupBits - 1
		for l := int(sz); l > basisHuffFastLookupBits+1; l-- {
			rev >>= 1
			nodeIdx := int(-treeCur-1) + int(rev&1)
			if nodeIdx < 0 || nodeIdx >= len(ht.tree) {
				return nil, errors.New("ktx2: BasisLZ Huffman tree overflow")
			}
			if ht.tree[nodeIdx] == 0 {
				ht.tree[nodeIdx] = treeNext
				treeCur = treeNext
				treeNext -= 2
			} else if ht.tree[nodeIdx] > 0 {
				return nil, errors.New("ktx2: BasisLZ overlapping Huffman branch")
			} else {
				treeCur = ht.tree[nodeIdx]
			}
		}
		rev >>= 1
		leafIdx := int(-treeCur-1) + int(rev&1)
		if leafIdx < 0 || leafIdx >= len(ht.tree) || ht.tree[leafIdx] != 0 {
			return nil, errors.New("ktx2: BasisLZ Huffman leaf conflict")
		}
		ht.tree[leafIdx] = int32(sym)
	}

	return ht, nil
}

func (r *basisBitReader) decodeHuff(ht *basisHuffTable) (int, error) {
	if ht == nil || ht.numSyms == 0 {
		return 0, errors.New("ktx2: BasisLZ empty Huffman table")
	}
	r.refill(16)
	sym := ht.lookup[r.bitBuf&(basisHuffFastLookupSize-1)]
	var codeLen uint
	if sym > 0 {
		codeLen = uint(sym >> 16)
		sym &= 0xFFFF
	} else if sym < 0 {
		codeLen = basisHuffFastLookupBits
		for sym < 0 {
			if codeLen >= basisHuffMaxCodeSize {
				return 0, errors.New("ktx2: BasisLZ Huffman code exceeds 16 bits")
			}
			idx := int(-sym-1) + int((r.bitBuf>>codeLen)&1)
			codeLen++
			if idx < 0 || idx >= len(ht.tree) {
				return 0, errors.New("ktx2: BasisLZ corrupt Huffman tree lookup")
			}
			sym = ht.tree[idx]
		}
	} else {
		return 0, errors.New("ktx2: BasisLZ invalid Huffman symbol")
	}
	if codeLen == 0 || codeLen > r.bitSize {
		return 0, errors.New("ktx2: BasisLZ invalid Huffman code length")
	}
	r.bitBuf >>= codeLen
	r.bitSize -= codeLen
	return int(sym), nil
}

func (r *basisBitReader) readHuffmanTable() (*basisHuffTable, error) {
	totalUsedSyms := int(r.readBits(basisHuffMaxSymsLog2))
	if totalUsedSyms == 0 {
		return &basisHuffTable{}, nil
	}
	if totalUsedSyms > basisHuffMaxSyms {
		return nil, fmt.Errorf("ktx2: BasisLZ Huffman symbol count %d exceeds %d", totalUsedSyms, basisHuffMaxSyms)
	}

	numCodeLenCodes := int(r.readBits(5))
	if numCodeLenCodes < 1 || numCodeLenCodes > basisHuffTotalCodeLenCodes {
		return nil, fmt.Errorf("ktx2: BasisLZ invalid codelength count %d", numCodeLenCodes)
	}

	var codeLenSizes [basisHuffTotalCodeLenCodes]uint8
	for i := range numCodeLenCodes {
		codeLenSizes[basisHuffSortedCodeLenCodes[i]] = uint8(r.readBits(3))
	}
	codeLenTable, err := buildBasisHuffTable(codeLenSizes[:])
	if err != nil {
		return nil, err
	}
	if codeLenTable.numSyms == 0 {
		return nil, errors.New("ktx2: BasisLZ empty codelength Huffman table")
	}

	codeSizes := make([]uint8, totalUsedSyms)
	cur := 0
	for cur < totalUsedSyms {
		c, err := r.decodeHuff(codeLenTable)
		if err != nil {
			return nil, err
		}
		switch {
		case c <= 16:
			codeSizes[cur] = uint8(c)
			cur++
		case c == basisHuffSmallZeroRunCode:
			cur += int(r.readBits(basisHuffSmallZeroRunBits)) + basisHuffSmallZeroRunMin
		case c == basisHuffBigZeroRunCode:
			cur += int(r.readBits(basisHuffBigZeroRunBits)) + basisHuffBigZeroRunMin
		case c == basisHuffSmallRepeatCode || c == basisHuffBigRepeatCode:
			if cur == 0 {
				return nil, errors.New("ktx2: BasisLZ Huffman repeat with no previous length")
			}
			var run int
			if c == basisHuffSmallRepeatCode {
				run = int(r.readBits(basisHuffSmallRepeatBits)) + basisHuffSmallRepeatMin
			} else {
				run = int(r.readBits(basisHuffBigRepeatBits)) + basisHuffBigRepeatMin
			}
			prev := codeSizes[cur-1]
			if prev == 0 {
				return nil, errors.New("ktx2: BasisLZ Huffman repeat of zero length")
			}
			if cur+run > totalUsedSyms {
				return nil, errors.New("ktx2: BasisLZ Huffman repeat overflows symbol table")
			}
			for range run {
				codeSizes[cur] = prev
				cur++
			}
		default:
			return nil, fmt.Errorf("ktx2: BasisLZ unexpected codelength symbol %d", c)
		}
	}
	if cur != totalUsedSyms {
		return nil, errors.New("ktx2: BasisLZ Huffman code length stream overshoot")
	}
	return buildBasisHuffTable(codeSizes)
}

func parseBasisLZSGD(sgd []byte, levelCount int, hasAlpha bool) (*basisLZGlobalData, error) {
	if len(sgd) < basisSGDHeaderSize {
		return nil, fmt.Errorf("ktx2: BasisLZ SGD truncated (%d bytes)", len(sgd))
	}
	endpointCount := int(binary.LittleEndian.Uint16(sgd[0:2]))
	selectorCount := int(binary.LittleEndian.Uint16(sgd[2:4]))
	endpointsLen := uint64(binary.LittleEndian.Uint32(sgd[4:8]))
	selectorsLen := uint64(binary.LittleEndian.Uint32(sgd[8:12]))
	tablesLen := uint64(binary.LittleEndian.Uint32(sgd[12:16]))
	extendedLen := uint64(binary.LittleEndian.Uint32(sgd[16:20]))

	if endpointCount == 0 || selectorCount == 0 {
		return nil, errors.New("ktx2: BasisLZ SGD endpointCount and selectorCount must be > 0")
	}
	if endpointsLen == 0 || selectorsLen == 0 || tablesLen == 0 {
		return nil, errors.New("ktx2: BasisLZ SGD endpoints, selectors, and tables must be non-empty")
	}
	if endpointCount > basisHuffMaxSyms || selectorCount > basisHuffMaxSyms {
		return nil, fmt.Errorf("ktx2: BasisLZ codebook sizes (%d, %d) exceed maximum %d", endpointCount, selectorCount, basisHuffMaxSyms)
	}

	imageDescBytes := uint64(levelCount) * basisSGDImageDescSize
	needed := uint64(basisSGDHeaderSize) + imageDescBytes + endpointsLen + selectorsLen + tablesLen + extendedLen
	if needed > uint64(len(sgd)) {
		return nil, fmt.Errorf("ktx2: BasisLZ SGD truncated (%d bytes, need %d)", len(sgd), needed)
	}

	descs := make([]basisLZImageDesc, levelCount)
	pos := uint64(basisSGDHeaderSize)
	for i := range levelCount {
		d := basisLZImageDesc{
			imageFlags:           binary.LittleEndian.Uint32(sgd[pos+0 : pos+4]),
			rgbSliceByteOffset:   binary.LittleEndian.Uint32(sgd[pos+4 : pos+8]),
			rgbSliceByteLength:   binary.LittleEndian.Uint32(sgd[pos+8 : pos+12]),
			alphaSliceByteOffset: binary.LittleEndian.Uint32(sgd[pos+12 : pos+16]),
			alphaSliceByteLength: binary.LittleEndian.Uint32(sgd[pos+16 : pos+20]),
		}
		if d.imageFlags&basisImageFlagPFrame != 0 {
			return nil, fmt.Errorf("ktx2: BasisLZ video P-frame level %d is not supported", i)
		}
		if d.rgbSliceByteLength == 0 {
			return nil, fmt.Errorf("ktx2: BasisLZ level %d has empty RGB slice", i)
		}
		if hasAlpha && d.alphaSliceByteLength == 0 {
			return nil, fmt.Errorf("ktx2: BasisLZ level %d has empty alpha slice", i)
		}
		descs[i] = d
		pos += basisSGDImageDescSize
	}

	endpointsData := sgd[pos : pos+endpointsLen]
	pos += endpointsLen
	selectorsData := sgd[pos : pos+selectorsLen]
	pos += selectorsLen
	tablesData := sgd[pos : pos+tablesLen]

	gd := &basisLZGlobalData{
		imageDescs: descs,
	}

	// Decode global slice Huffman tables.
	tbr := newBasisBitReader(tablesData)
	var err error
	if gd.endpointPredModel, err = tbr.readHuffmanTable(); err != nil {
		return nil, fmt.Errorf("ktx2: BasisLZ endpoint pred table: %w", err)
	}
	if gd.endpointPredModel.numSyms == 0 {
		return nil, errors.New("ktx2: BasisLZ empty endpoint pred table")
	}
	if gd.deltaEndpointModel, err = tbr.readHuffmanTable(); err != nil {
		return nil, fmt.Errorf("ktx2: BasisLZ delta endpoint table: %w", err)
	}
	if gd.deltaEndpointModel.numSyms == 0 {
		return nil, errors.New("ktx2: BasisLZ empty delta endpoint table")
	}
	if gd.selectorModel, err = tbr.readHuffmanTable(); err != nil {
		return nil, fmt.Errorf("ktx2: BasisLZ selector table: %w", err)
	}
	if gd.selectorModel.numSyms == 0 {
		return nil, errors.New("ktx2: BasisLZ empty selector table")
	}
	if gd.selectorHistoryRLE, err = tbr.readHuffmanTable(); err != nil {
		return nil, fmt.Errorf("ktx2: BasisLZ selector RLE table: %w", err)
	}
	if gd.selectorHistoryRLE.numSyms == 0 {
		return nil, errors.New("ktx2: BasisLZ empty selector RLE table")
	}
	gd.selectorHistoryBufSize = int(tbr.readBits(13))
	if gd.selectorHistoryBufSize == 0 {
		return nil, errors.New("ktx2: BasisLZ selectorHistoryBufSize must be > 0")
	}

	// Decode color endpoint codebook.
	ebr := newBasisBitReader(endpointsData)
	color5Models := make([]*basisHuffTable, 3)
	for i := range 3 {
		if color5Models[i], err = ebr.readHuffmanTable(); err != nil {
			return nil, fmt.Errorf("ktx2: BasisLZ color5 delta table %d: %w", i, err)
		}
		if color5Models[i].numSyms == 0 {
			return nil, fmt.Errorf("ktx2: BasisLZ empty color5 delta table %d", i)
		}
	}
	intenModel, err := ebr.readHuffmanTable()
	if err != nil {
		return nil, fmt.Errorf("ktx2: BasisLZ intensity delta table: %w", err)
	}
	if intenModel.numSyms == 0 {
		return nil, errors.New("ktx2: BasisLZ empty intensity delta table")
	}
	isGrayscale := ebr.readBits(1) != 0

	gd.endpoints = make([]etc1sEndpoint, endpointCount)
	prevColor := [3]int{16, 16, 16}
	prevInten := 0
	numCh := 3
	if isGrayscale {
		numCh = 1
	}
	for i := range endpointCount {
		intenDelta, err := ebr.decodeHuff(intenModel)
		if err != nil {
			return nil, fmt.Errorf("ktx2: BasisLZ endpoint %d inten: %w", i, err)
		}
		inten := (prevInten + intenDelta) & 7
		gd.endpoints[i].inten = uint8(inten)
		prevInten = inten

		for c := range numCh {
			var model *basisHuffTable
			switch {
			case prevColor[c] <= basisColor5Pal0PrevHi:
				model = color5Models[0]
			case prevColor[c] <= basisColor5Pal1PrevHi:
				model = color5Models[1]
			default:
				model = color5Models[2]
			}
			delta, err := ebr.decodeHuff(model)
			if err != nil {
				return nil, fmt.Errorf("ktx2: BasisLZ endpoint %d color5[%d]: %w", i, c, err)
			}
			v := (prevColor[c] + delta) & 31
			gd.endpoints[i].color5[c] = uint8(v)
			prevColor[c] = v
		}
		if isGrayscale {
			gd.endpoints[i].color5[1] = gd.endpoints[i].color5[0]
			gd.endpoints[i].color5[2] = gd.endpoints[i].color5[0]
		}
	}

	// Decode selector codebook.
	sbr := newBasisBitReader(selectorsData)
	if sbr.readBits(1) != 0 {
		return nil, errors.New("ktx2: BasisLZ global selector codebooks are not supported")
	}
	if sbr.readBits(1) != 0 {
		return nil, errors.New("ktx2: BasisLZ hybrid global selector codebooks are not supported")
	}
	usedRaw := sbr.readBits(1) != 0
	gd.selectors = make([]etc1sSelector, selectorCount)
	if usedRaw {
		for i := range selectorCount {
			for row := range 4 {
				gd.selectors[i].rows[row] = uint8(sbr.readBits(8))
			}
		}
	} else {
		deltaSelModel, err := sbr.readHuffmanTable()
		if err != nil {
			return nil, fmt.Errorf("ktx2: BasisLZ selector delta table: %w", err)
		}
		if selectorCount > 1 && deltaSelModel.numSyms == 0 {
			return nil, errors.New("ktx2: BasisLZ empty selector delta table")
		}
		var prevBytes [4]uint8
		for i := range selectorCount {
			if i == 0 {
				for row := range 4 {
					b := uint8(sbr.readBits(8))
					prevBytes[row] = b
					gd.selectors[i].rows[row] = b
				}
				continue
			}
			for row := range 4 {
				delta, err := sbr.decodeHuff(deltaSelModel)
				if err != nil {
					return nil, fmt.Errorf("ktx2: BasisLZ selector %d row %d: %w", i, row, err)
				}
				b := uint8(delta) ^ prevBytes[row]
				prevBytes[row] = b
				gd.selectors[i].rows[row] = b
			}
		}
	}

	return gd, nil
}

type basisApproxMTF struct {
	vals  []int
	rover int
}

func newBasisApproxMTF(n int) basisApproxMTF {
	return basisApproxMTF{
		vals:  make([]int, n),
		rover: n / 2,
	}
}

func (m *basisApproxMTF) add(v int) {
	m.vals[m.rover] = v
	m.rover++
	if m.rover == len(m.vals) {
		m.rover = len(m.vals) / 2
	}
}

func (m *basisApproxMTF) use(idx int) {
	if idx > 0 {
		m.vals[idx/2], m.vals[idx] = m.vals[idx], m.vals[idx/2]
	}
}

type etc1sBlockRef struct {
	endpointIdx uint16
	selectorIdx uint16
}

func (gd *basisLZGlobalData) decodeSliceBlocks(sliceData []byte, bw, bh int) ([]etc1sBlockRef, error) {
	totalBlocks := bw * bh
	out := make([]etc1sBlockRef, totalBlocks)
	br := newBasisBitReader(sliceData)

	type predState struct {
		endpointIdx uint16
		predBits    uint8
	}
	predRows := [2][]predState{
		make([]predState, bw),
		make([]predState, bw),
	}

	historyBuf := newBasisApproxMTF(gd.selectorHistoryBufSize)
	numEndpoints := len(gd.endpoints)
	numSelectors := len(gd.selectors)
	selectorHistoryFirstSym := numSelectors
	selectorHistoryRLESym := numSelectors + gd.selectorHistoryBufSize

	prevEndpointIdx := 0
	endpointPredRepeatCount := 0
	prevEndpointPredSym := 0
	curPredBits := 0
	curSelectorRLECount := 0

	for by := range bh {
		curRow := by & 1
		otherRow := curRow ^ 1
		for bx := range bw {
			if bx&1 == 0 {
				if by&1 == 0 {
					if endpointPredRepeatCount > 0 {
						endpointPredRepeatCount--
						curPredBits = prevEndpointPredSym
					} else {
						sym, err := br.decodeHuff(gd.endpointPredModel)
						if err != nil {
							return nil, fmt.Errorf("ktx2: BasisLZ block (%d,%d) pred: %w", bx, by, err)
						}
						if sym == basisEndpointPredRepeatSym {
							vlc, err := br.decodeVLC(basisEndpointPredCountVLCBit)
							if err != nil {
								return nil, err
							}
							endpointPredRepeatCount = int(vlc) + basisEndpointPredMinRepeat - 1
							curPredBits = prevEndpointPredSym
						} else {
							curPredBits = sym
							prevEndpointPredSym = sym
						}
					}
					predRows[otherRow][bx].predBits = uint8(curPredBits >> 4)
				} else {
					curPredBits = int(predRows[curRow][bx].predBits)
				}
			}

			pred := curPredBits & 3
			curPredBits >>= 2

			var endpointIdx int
			switch pred {
			case 0: // Left
				if bx == 0 {
					return nil, fmt.Errorf("ktx2: BasisLZ invalid left predictor at (%d,%d)", bx, by)
				}
				endpointIdx = prevEndpointIdx
			case 1: // Upper
				if by == 0 {
					return nil, fmt.Errorf("ktx2: BasisLZ invalid upper predictor at (%d,%d)", bx, by)
				}
				endpointIdx = int(predRows[otherRow][bx].endpointIdx)
			case 2: // Upper-left
				if bx == 0 || by == 0 {
					return nil, fmt.Errorf("ktx2: BasisLZ invalid upper-left predictor at (%d,%d)", bx, by)
				}
				endpointIdx = int(predRows[otherRow][bx-1].endpointIdx)
			default: // Delta
				deltaSym, err := br.decodeHuff(gd.deltaEndpointModel)
				if err != nil {
					return nil, fmt.Errorf("ktx2: BasisLZ block (%d,%d) endpoint delta: %w", bx, by, err)
				}
				endpointIdx = deltaSym + prevEndpointIdx
				if endpointIdx >= numEndpoints {
					endpointIdx -= numEndpoints
				}
			}
			if endpointIdx < 0 || endpointIdx >= numEndpoints {
				return nil, fmt.Errorf("ktx2: BasisLZ endpoint index %d out of range [0,%d)", endpointIdx, numEndpoints)
			}
			predRows[curRow][bx].endpointIdx = uint16(endpointIdx)
			prevEndpointIdx = endpointIdx

			var selectorSym int
			if curSelectorRLECount > 0 {
				curSelectorRLECount--
				selectorSym = selectorHistoryFirstSym
			} else {
				var err error
				selectorSym, err = br.decodeHuff(gd.selectorModel)
				if err != nil {
					return nil, fmt.Errorf("ktx2: BasisLZ block (%d,%d) selector: %w", bx, by, err)
				}
				if selectorSym == selectorHistoryRLESym {
					runSym, err := br.decodeHuff(gd.selectorHistoryRLE)
					if err != nil {
						return nil, fmt.Errorf("ktx2: BasisLZ block (%d,%d) selector RLE: %w", bx, by, err)
					}
					if runSym == basisSelectorRLECountTotal-1 {
						vlc, err := br.decodeVLC(7)
						if err != nil {
							return nil, err
						}
						curSelectorRLECount = int(vlc) + basisSelectorRLECountThresh
					} else {
						curSelectorRLECount = runSym + basisSelectorRLECountThresh
					}
					if curSelectorRLECount > totalBlocks {
						return nil, fmt.Errorf("ktx2: BasisLZ selector RLE count %d exceeds block count %d", curSelectorRLECount, totalBlocks)
					}
					selectorSym = selectorHistoryFirstSym
					curSelectorRLECount--
				}
			}

			var selectorIdx int
			if selectorSym >= selectorHistoryFirstSym {
				histIdx := selectorSym - selectorHistoryFirstSym
				if histIdx < 0 || histIdx >= len(historyBuf.vals) {
					return nil, fmt.Errorf("ktx2: BasisLZ selector history index %d out of range", histIdx)
				}
				selectorIdx = historyBuf.vals[histIdx]
				if histIdx != 0 {
					historyBuf.use(histIdx)
				}
			} else {
				selectorIdx = selectorSym
				historyBuf.add(selectorIdx)
			}
			if selectorIdx < 0 || selectorIdx >= numSelectors {
				return nil, fmt.Errorf("ktx2: BasisLZ selector index %d out of range [0,%d)", selectorIdx, numSelectors)
			}

			out[by*bw+bx] = etc1sBlockRef{
				endpointIdx: uint16(endpointIdx),
				selectorIdx: uint16(selectorIdx),
			}
		}
	}

	if endpointPredRepeatCount != 0 {
		return nil, fmt.Errorf("ktx2: BasisLZ trailing endpoint predictor repeat count %d", endpointPredRepeatCount)
	}
	return out, nil
}

func (t *Texture) decodeETC1SLevelRGBA(level int) (*image.NRGBA, error) {
	gd := t.etc1s
	if gd == nil {
		var err error
		gd, err = parseBasisLZSGD(t.SGD, len(t.Levels), t.HasAlpha)
		if err != nil {
			return nil, err
		}
		t.etc1s = gd
	}
	if level < 0 || level >= len(gd.imageDescs) {
		return nil, fmt.Errorf("ktx2: BasisLZ level %d out of range [0, %d)", level, len(gd.imageDescs))
	}

	lvl := t.Levels[level]
	desc := gd.imageDescs[level]
	if !rangeInBounds(uint64(desc.rgbSliceByteOffset), uint64(desc.rgbSliceByteLength), len(lvl.Data)) {
		return nil, fmt.Errorf("ktx2: BasisLZ level %d RGB slice out of bounds", level)
	}
	rgbSlice := lvl.Data[desc.rgbSliceByteOffset : desc.rgbSliceByteOffset+desc.rgbSliceByteLength]

	bw := (lvl.Width + 3) / 4
	bh := (lvl.Height + 3) / 4
	rgbBlocks, err := gd.decodeSliceBlocks(rgbSlice, bw, bh)
	if err != nil {
		return nil, fmt.Errorf("ktx2: BasisLZ level %d RGB: %w", level, err)
	}

	var alphaBlocks []etc1sBlockRef
	if desc.alphaSliceByteLength > 0 {
		if !rangeInBounds(uint64(desc.alphaSliceByteOffset), uint64(desc.alphaSliceByteLength), len(lvl.Data)) {
			return nil, fmt.Errorf("ktx2: BasisLZ level %d alpha slice out of bounds", level)
		}
		alphaSlice := lvl.Data[desc.alphaSliceByteOffset : desc.alphaSliceByteOffset+desc.alphaSliceByteLength]
		alphaBlocks, err = gd.decodeSliceBlocks(alphaSlice, bw, bh)
		if err != nil {
			return nil, fmt.Errorf("ktx2: BasisLZ level %d alpha: %w", level, err)
		}
	}

	dst := image.NewNRGBA(image.Rect(0, 0, lvl.Width, lvl.Height))
	for by := range bh {
		for bx := range bw {
			ref := rgbBlocks[by*bw+bx]
			ep := gd.endpoints[ref.endpointIdx]
			sel := gd.selectors[ref.selectorIdx]

			baseR := expand5(int(ep.color5[0]))
			baseG := expand5(int(ep.color5[1]))
			baseB := expand5(int(ep.color5[2]))
			mods := etc1sModifierTable[ep.inten]
			var pal [4][3]uint8
			for k := range 4 {
				m := mods[k]
				pal[k] = [3]uint8{
					clampByte(baseR + m),
					clampByte(baseG + m),
					clampByte(baseB + m),
				}
			}

			var alphaPal [4]uint8
			var alphaSel etc1sSelector
			if alphaBlocks != nil {
				aRef := alphaBlocks[by*bw+bx]
				aEp := gd.endpoints[aRef.endpointIdx]
				alphaSel = gd.selectors[aRef.selectorIdx]
				// Khronos Data Format Spec §22 / Basis Universal stores alpha intensity in the green channel of the alpha slice.
				aBaseG := expand5(int(aEp.color5[1]))
				aMods := etc1sModifierTable[aEp.inten]
				for k := range 4 {
					alphaPal[k] = clampByte(aBaseG + aMods[k])
				}
			}

			for dy := range 4 {
				py := by*4 + dy
				if py >= lvl.Height {
					continue
				}
				rowOff := py * dst.Stride
				rgbRow := sel.rows[dy]
				alphaRow := alphaSel.rows[dy]
				for dx := range 4 {
					px := bx*4 + dx
					if px >= lvl.Width {
						continue
					}
					c := pal[(rgbRow>>(dx*2))&3]
					a := uint8(255)
					if alphaBlocks != nil {
						a = alphaPal[(alphaRow>>(dx*2))&3]
					}
					di := rowOff + px*4
					dst.Pix[di+0] = c[0]
					dst.Pix[di+1] = c[1]
					dst.Pix[di+2] = c[2]
					dst.Pix[di+3] = a
				}
			}
		}
	}
	return dst, nil
}
