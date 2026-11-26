// Package qrcode implements a deterministic, self-contained pure-Go ISO/IEC 18004
// QR Code Model 2 encoder (versions 1–10, byte mode, error correction levels L/M/Q/H).
// It has zero external dependencies and compiles across desktop, mobile, and web targets.
package qrcode

import (
	"errors"
	"fmt"
	"math"
)

// Level selects the ISO/IEC 18004 error correction level.
type Level int

const (
	// Low recovers up to ~7% codeword errors.
	Low Level = iota
	// Medium recovers up to ~15% codeword errors.
	Medium
	// Quartile recovers up to ~25% codeword errors.
	Quartile
	// High recovers up to ~30% codeword errors.
	High
)

// String returns the standard single-letter error correction code ("L", "M", "Q", or "H").
func (l Level) String() string {
	switch l {
	case Low:
		return "L"
	case Medium:
		return "M"
	case Quartile:
		return "Q"
	case High:
		return "H"
	default:
		return "?"
	}
}

func (l Level) formatBits() uint16 {
	switch l {
	case Low:
		return 0b01
	case Medium:
		return 0b00
	case Quartile:
		return 0b11
	case High:
		return 0b10
	default:
		return 0b00
	}
}

// ErrEmptyInput is returned when Encode receives an empty payload.
var ErrEmptyInput = errors.New("qrcode: input must not be empty")

// ErrPayloadTooLarge is returned when the input exceeds version 10 capacity for the chosen level.
var ErrPayloadTooLarge = errors.New("qrcode: input exceeds maximum capacity")

// Code holds a finished QR Code symbol matrix.
type Code struct {
	// Version is the ISO/IEC 18004 symbol version (1..10).
	Version int
	// Level is the error correction level used to encode the symbol.
	Level Level
	// Mask is the chosen data mask pattern (0..7).
	Mask int
	// Size is the module width and height (4*Version + 17).
	Size int
	// modules stores row-major booleans where true is a dark module.
	modules []bool
}

// Black reports whether the module at column x, row y is dark.
// Coordinates outside [0, Size) return false (light quiet zone).
func (c *Code) Black(x, y int) bool {
	if c == nil || x < 0 || y < 0 || x >= c.Size || y >= c.Size {
		return false
	}
	return c.modules[y*c.Size+x]
}

// QuietZone returns the standard 4-module light border width recommended by ISO/IEC 18004.
func (c *Code) QuietZone() int {
	return 4
}

// Matrix returns a defensive Size×Size copy of the symbol's module grid (indexed [y][x]).
func (c *Code) Matrix() [][]bool {
	if c == nil || c.Size <= 0 {
		return nil
	}
	out := make([][]bool, c.Size)
	for y := range c.Size {
		row := make([]bool, c.Size)
		copy(row, c.modules[y*c.Size:(y+1)*c.Size])
		out[y] = row
	}
	return out
}

// Encode encodes text as a deterministic ISO/IEC 18004 QR Code Model 2 symbol in byte mode
// at the smallest version (1..10) that fits text at error correction level.
func Encode(text string, level Level) (*Code, error) {
	if len(text) == 0 {
		return nil, ErrEmptyInput
	}
	if level < Low || level > High {
		return nil, fmt.Errorf("qrcode: invalid error correction level %d", level)
	}
	data := []byte(text)
	ver, spec, ok := pickVersion(len(data), level)
	if !ok {
		return nil, fmt.Errorf("%w: %d bytes at level %s", ErrPayloadTooLarge, len(data), level)
	}

	codewords := buildDataCodewords(data, ver, spec.totalDataCodewords())
	stream := interleaveWithECC(codewords, spec)

	size := 4*ver + 17
	base := make([]bool, size*size)
	funcMask := make([]bool, size*size)

	placeFunctionPatterns(base, funcMask, ver, size)
	placeDataBits(base, funcMask, stream, size)

	bestMask := 0
	bestPenalty := math.MaxInt
	var bestModules []bool
	for mask := range 8 {
		cand := make([]bool, len(base))
		copy(cand, base)
		applyMask(cand, funcMask, size, mask)
		placeFormatInfo(cand, size, level, mask)
		if ver >= 7 {
			placeVersionInfo(cand, size, ver)
		}
		pen := evaluatePenalty(cand, size)
		if pen < bestPenalty {
			bestPenalty = pen
			bestMask = mask
			bestModules = cand
		}
	}

	return &Code{
		Version: ver,
		Level:   level,
		Mask:    bestMask,
		Size:    size,
		modules: bestModules,
	}, nil
}

type rsSpec struct {
	ecPerBlock int
	g1Blocks   int
	g1Data     int
	g2Blocks   int
	g2Data     int
}

func (s rsSpec) totalDataCodewords() int {
	return s.g1Blocks*s.g1Data + s.g2Blocks*s.g2Data
}

// ISO/IEC 18004 Table 9 RS block parameters for versions 1..10, indexed [version][level].
var versionSpecs = [11][4]rsSpec{
	{}, // version 0 unused
	// Version 1 (26 codewords)
	{
		Low:      {ecPerBlock: 7, g1Blocks: 1, g1Data: 19},
		Medium:   {ecPerBlock: 10, g1Blocks: 1, g1Data: 16},
		Quartile: {ecPerBlock: 13, g1Blocks: 1, g1Data: 13},
		High:     {ecPerBlock: 17, g1Blocks: 1, g1Data: 9},
	},
	// Version 2 (44 codewords)
	{
		Low:      {ecPerBlock: 10, g1Blocks: 1, g1Data: 34},
		Medium:   {ecPerBlock: 16, g1Blocks: 1, g1Data: 28},
		Quartile: {ecPerBlock: 22, g1Blocks: 1, g1Data: 22},
		High:     {ecPerBlock: 28, g1Blocks: 1, g1Data: 16},
	},
	// Version 3 (70 codewords)
	{
		Low:      {ecPerBlock: 15, g1Blocks: 1, g1Data: 55},
		Medium:   {ecPerBlock: 26, g1Blocks: 1, g1Data: 44},
		Quartile: {ecPerBlock: 18, g1Blocks: 2, g1Data: 17},
		High:     {ecPerBlock: 22, g1Blocks: 2, g1Data: 13},
	},
	// Version 4 (100 codewords)
	{
		Low:      {ecPerBlock: 20, g1Blocks: 1, g1Data: 80},
		Medium:   {ecPerBlock: 18, g1Blocks: 2, g1Data: 32},
		Quartile: {ecPerBlock: 26, g1Blocks: 2, g1Data: 24},
		High:     {ecPerBlock: 16, g1Blocks: 4, g1Data: 9},
	},
	// Version 5 (134 codewords)
	{
		Low:      {ecPerBlock: 26, g1Blocks: 1, g1Data: 108},
		Medium:   {ecPerBlock: 24, g1Blocks: 2, g1Data: 43},
		Quartile: {ecPerBlock: 18, g1Blocks: 2, g1Data: 15, g2Blocks: 2, g2Data: 16},
		High:     {ecPerBlock: 22, g1Blocks: 2, g1Data: 11, g2Blocks: 2, g2Data: 12},
	},
	// Version 6 (172 codewords)
	{
		Low:      {ecPerBlock: 18, g1Blocks: 2, g1Data: 68},
		Medium:   {ecPerBlock: 16, g1Blocks: 4, g1Data: 27},
		Quartile: {ecPerBlock: 24, g1Blocks: 4, g1Data: 19},
		High:     {ecPerBlock: 28, g1Blocks: 4, g1Data: 15},
	},
	// Version 7 (196 codewords)
	{
		Low:      {ecPerBlock: 20, g1Blocks: 2, g1Data: 78},
		Medium:   {ecPerBlock: 18, g1Blocks: 4, g1Data: 31},
		Quartile: {ecPerBlock: 18, g1Blocks: 2, g1Data: 14, g2Blocks: 4, g2Data: 15},
		High:     {ecPerBlock: 26, g1Blocks: 4, g1Data: 13, g2Blocks: 1, g2Data: 14},
	},
	// Version 8 (242 codewords)
	{
		Low:      {ecPerBlock: 24, g1Blocks: 2, g1Data: 97},
		Medium:   {ecPerBlock: 22, g1Blocks: 2, g1Data: 38, g2Blocks: 2, g2Data: 39},
		Quartile: {ecPerBlock: 22, g1Blocks: 4, g1Data: 18, g2Blocks: 2, g2Data: 19},
		High:     {ecPerBlock: 26, g1Blocks: 4, g1Data: 14, g2Blocks: 2, g2Data: 15},
	},
	// Version 9 (292 codewords)
	{
		Low:      {ecPerBlock: 30, g1Blocks: 2, g1Data: 116},
		Medium:   {ecPerBlock: 22, g1Blocks: 3, g1Data: 36, g2Blocks: 2, g2Data: 37},
		Quartile: {ecPerBlock: 20, g1Blocks: 4, g1Data: 16, g2Blocks: 4, g2Data: 17},
		High:     {ecPerBlock: 24, g1Blocks: 4, g1Data: 12, g2Blocks: 4, g2Data: 13},
	},
	// Version 10 (346 codewords)
	{
		Low:      {ecPerBlock: 18, g1Blocks: 2, g1Data: 68, g2Blocks: 2, g2Data: 69},
		Medium:   {ecPerBlock: 26, g1Blocks: 4, g1Data: 43, g2Blocks: 1, g2Data: 44},
		Quartile: {ecPerBlock: 24, g1Blocks: 6, g1Data: 19, g2Blocks: 2, g2Data: 20},
		High:     {ecPerBlock: 28, g1Blocks: 6, g1Data: 15, g2Blocks: 2, g2Data: 16},
	},
}

// Alignment pattern center coordinates for versions 1..10 (ISO/IEC 18004 Annex E).
var alignmentCenters = [11][]int{
	nil,
	nil,         // v1
	{6, 18},     // v2
	{6, 22},     // v3
	{6, 26},     // v4
	{6, 30},     // v5
	{6, 34},     // v6
	{6, 22, 38}, // v7
	{6, 24, 42}, // v8
	{6, 26, 46}, // v9
	{6, 28, 50}, // v10
}

func byteCountBits(version int) int {
	if version <= 9 {
		return 8
	}
	return 16
}

func pickVersion(dataLen int, level Level) (int, rsSpec, bool) {
	for ver := 1; ver <= 10; ver++ {
		spec := versionSpecs[ver][level]
		neededBits := 4 + byteCountBits(ver) + dataLen*8
		if neededBits <= spec.totalDataCodewords()*8 {
			return ver, spec, true
		}
	}
	return 0, rsSpec{}, false
}

type bitBuffer struct {
	bits []byte // each entry is 0 or 1
}

func (b *bitBuffer) appendBits(val uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		b.bits = append(b.bits, byte((val>>i)&1))
	}
}

func buildDataCodewords(data []byte, version int, totalDataBytes int) []byte {
	var bb bitBuffer
	// Byte mode indicator: 0100.
	bb.appendBits(0b0100, 4)
	bb.appendBits(uint32(len(data)), byteCountBits(version))
	for _, ch := range data {
		bb.appendBits(uint32(ch), 8)
	}

	capBits := totalDataBytes * 8
	// Up to 4 zero terminator bits.
	term := min(4, capBits-len(bb.bits))
	if term > 0 {
		bb.appendBits(0, term)
	}
	// Pad to next byte boundary.
	if rem := len(bb.bits) % 8; rem != 0 {
		bb.appendBits(0, 8-rem)
	}

	out := make([]byte, 0, totalDataBytes)
	for i := 0; i < len(bb.bits); i += 8 {
		var b byte
		for j := range 8 {
			b = (b << 1) | bb.bits[i+j]
		}
		out = append(out, b)
	}
	// Pad alternating 0xEC, 0x11 codewords to reach totalDataBytes.
	pad := [2]byte{0xEC, 0x11}
	for i := 0; len(out) < totalDataBytes; i++ {
		out = append(out, pad[i&1])
	}
	return out
}

// GF(256) arithmetic with primitive polynomial 0x11D (x^8 + x^4 + x^3 + x^2 + 1).
var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := range 255 {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func rsGeneratorPoly(degree int) []byte {
	poly := []byte{1}
	for i := range degree {
		next := make([]byte, len(poly)+1)
		factor := gfExp[i]
		for j, coeff := range poly {
			next[j] ^= coeff
			next[j+1] ^= gfMul(coeff, factor)
		}
		poly = next
	}
	return poly
}

func rsEncodeBlock(data []byte, ecCount int) []byte {
	gen := rsGeneratorPoly(ecCount)
	rem := make([]byte, ecCount)
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[ecCount-1] = 0
		if factor != 0 {
			for j := range ecCount {
				rem[j] ^= gfMul(gen[j+1], factor)
			}
		}
	}
	return rem
}

func interleaveWithECC(data []byte, spec rsSpec) []byte {
	numBlocks := spec.g1Blocks + spec.g2Blocks
	dataBlocks := make([][]byte, 0, numBlocks)
	ecBlocks := make([][]byte, 0, numBlocks)

	off := 0
	for range spec.g1Blocks {
		blk := data[off : off+spec.g1Data]
		off += spec.g1Data
		dataBlocks = append(dataBlocks, blk)
		ecBlocks = append(ecBlocks, rsEncodeBlock(blk, spec.ecPerBlock))
	}
	for range spec.g2Blocks {
		blk := data[off : off+spec.g2Data]
		off += spec.g2Data
		dataBlocks = append(dataBlocks, blk)
		ecBlocks = append(ecBlocks, rsEncodeBlock(blk, spec.ecPerBlock))
	}

	maxDataLen := spec.g1Data
	if spec.g2Data > maxDataLen {
		maxDataLen = spec.g2Data
	}

	out := make([]byte, 0, len(data)+numBlocks*spec.ecPerBlock)
	for i := range maxDataLen {
		for _, blk := range dataBlocks {
			if i < len(blk) {
				out = append(out, blk[i])
			}
		}
	}
	for i := range spec.ecPerBlock {
		for _, blk := range ecBlocks {
			out = append(out, blk[i])
		}
	}
	return out
}

func setFunc(modules, funcMask []bool, size, x, y int, dark bool) {
	if x < 0 || y < 0 || x >= size || y >= size {
		return
	}
	idx := y*size + x
	modules[idx] = dark
	funcMask[idx] = true
}

func placeFunctionPatterns(modules, funcMask []bool, ver, size int) {
	// 1. Finder patterns (7×7) + 1-module separators.
	placeFinder := func(ox, oy int) {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				x, y := ox+dx, oy+dy
				if x < 0 || y < 0 || x >= size || y >= size {
					continue
				}
				dark := false
				if dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6 {
					dist := max(absInt(dx-3), absInt(dy-3))
					dark = dist != 2
				}
				setFunc(modules, funcMask, size, x, y, dark)
			}
		}
	}
	placeFinder(0, 0)
	placeFinder(size-7, 0)
	placeFinder(0, size-7)

	// 2. Timing patterns on row 6 and column 6.
	for i := 8; i < size-8; i++ {
		dark := i%2 == 0
		setFunc(modules, funcMask, size, i, 6, dark)
		setFunc(modules, funcMask, size, 6, i, dark)
	}

	// 3. Alignment patterns (5×5) for version >= 2.
	centers := alignmentCenters[ver]
	for _, cy := range centers {
		for _, cx := range centers {
			// Skip alignment centers that overlap any of the 3 finder patterns.
			if (cx <= 8 && cy <= 8) || (cx >= size-9 && cy <= 8) || (cx <= 8 && cy >= size-9) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					dist := max(absInt(dx), absInt(dy))
					setFunc(modules, funcMask, size, cx+dx, cy+dy, dist != 1)
				}
			}
		}
	}

	// 4. Fixed dark module at (8, 4*ver+9).
	setFunc(modules, funcMask, size, 8, 4*ver+9, true)

	// 5. Reserve format information modules.
	for i := range 9 {
		if i != 6 {
			setFunc(modules, funcMask, size, i, 8, false)
			setFunc(modules, funcMask, size, 8, i, false)
		}
	}
	for i := range 8 {
		setFunc(modules, funcMask, size, size-1-i, 8, false)
	}
	for i := range 7 {
		setFunc(modules, funcMask, size, 8, size-1-i, false)
	}

	// 6. Reserve version information modules for version >= 7.
	if ver >= 7 {
		for i := range 6 {
			for j := range 3 {
				setFunc(modules, funcMask, size, size-11+j, i, false)
				setFunc(modules, funcMask, size, i, size-11+j, false)
			}
		}
	}
}

func placeDataBits(modules, funcMask []bool, codewords []byte, size int) {
	bitIdx := 0
	totalBits := len(codewords) * 8
	upward := true

	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5 // skip vertical timing pattern column
		}
		for step := range size {
			y := step
			if upward {
				y = size - 1 - step
			}
			for dx := range 2 {
				x := right - dx
				idx := y*size + x
				if funcMask[idx] {
					continue
				}
				dark := false
				if bitIdx < totalBits {
					dark = ((codewords[bitIdx>>3] >> (7 - uint(bitIdx&7))) & 1) != 0
					bitIdx++
				}
				modules[idx] = dark
			}
		}
		upward = !upward
	}
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (y+x)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (y+x)%3 == 0
	case 4:
		return ((y/2)+(x/3))%2 == 0
	case 5:
		return ((y*x)%2)+((y*x)%3) == 0
	case 6:
		return (((y*x)%2)+((y*x)%3))%2 == 0
	case 7:
		return (((y+x)%2)+((y*x)%3))%2 == 0
	default:
		return false
	}
}

func applyMask(modules, funcMask []bool, size, mask int) {
	for y := range size {
		rowOff := y * size
		for x := range size {
			idx := rowOff + x
			if !funcMask[idx] && maskBit(mask, x, y) {
				modules[idx] = !modules[idx]
			}
		}
	}
}

func formatInfoBits(level Level, mask int) uint16 {
	data := (level.formatBits() << 3) | uint16(mask&0x7)
	rem := data << 10
	for i := 14; i >= 10; i-- {
		if (rem & (1 << i)) != 0 {
			rem ^= 0x537 << (i - 10)
		}
	}
	return ((data << 10) | rem) ^ 0x5412
}

func placeFormatInfo(modules []bool, size int, level Level, mask int) {
	bits := formatInfoBits(level, mask)
	bit := func(pos int) bool {
		return ((bits >> pos) & 1) != 0
	}

	// Copy 1 around top-left finder: bit 14 (MSB) at (0,8) .. bit 0 (LSB) at (8,0).
	coords1 := [15][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8},
		{7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
	}
	// Copy 2 split between top-right row 8 and bottom-left col 8.
	coords2 := [15][2]int{
		{size - 1, 8}, {size - 2, 8}, {size - 3, 8}, {size - 4, 8},
		{size - 5, 8}, {size - 6, 8}, {size - 7, 8}, {size - 8, 8},
		{8, size - 7}, {8, size - 6}, {8, size - 5}, {8, size - 4},
		{8, size - 3}, {8, size - 2}, {8, size - 1},
	}
	for i := range 15 {
		v := bit(i)
		c1 := coords1[i]
		modules[c1[1]*size+c1[0]] = v
		c2 := coords2[i]
		modules[c2[1]*size+c2[0]] = v
	}
}

func versionInfoBits(ver int) uint32 {
	data := uint32(ver)
	rem := data << 12
	for i := 17; i >= 12; i-- {
		if (rem & (1 << i)) != 0 {
			rem ^= 0x1F25 << (i - 12)
		}
	}
	return (data << 12) | rem
}

func placeVersionInfo(modules []bool, size, ver int) {
	bits := versionInfoBits(ver)
	for i := range 18 {
		dark := ((bits >> i) & 1) != 0
		a := i / 3
		b := size - 11 + (i % 3)
		// Top-right 3×6 block and bottom-left 6×3 block.
		modules[a*size+b] = dark
		modules[b*size+a] = dark
	}
}

// evaluatePenalty computes the ISO/IEC 18004 Section 7.8.3.1 mask penalty score (N1+N2+N3+N4).
func evaluatePenalty(modules []bool, size int) int {
	penalty := 0

	// N1: 5+ consecutive same-colored modules in rows and columns.
	for y := range size {
		runColor := modules[y*size]
		runLen := 1
		for x := 1; x < size; x++ {
			c := modules[y*size+x]
			if c == runColor {
				runLen++
			} else {
				if runLen >= 5 {
					penalty += 3 + (runLen - 5)
				}
				runColor = c
				runLen = 1
			}
		}
		if runLen >= 5 {
			penalty += 3 + (runLen - 5)
		}
	}
	for x := range size {
		runColor := modules[x]
		runLen := 1
		for y := 1; y < size; y++ {
			c := modules[y*size+x]
			if c == runColor {
				runLen++
			} else {
				if runLen >= 5 {
					penalty += 3 + (runLen - 5)
				}
				runColor = c
				runLen = 1
			}
		}
		if runLen >= 5 {
			penalty += 3 + (runLen - 5)
		}
	}

	// N2: 2×2 blocks of same-colored modules (+3 each).
	for y := range size - 1 {
		for x := range size - 1 {
			c := modules[y*size+x]
			if c == modules[y*size+x+1] && c == modules[(y+1)*size+x] && c == modules[(y+1)*size+x+1] {
				penalty += 3
			}
		}
	}

	// N3: finder-like 1:1:3:1:1 patterns preceded or followed by 4 light modules (+40 each).
	pat1 := [11]bool{true, false, true, true, true, false, true, false, false, false, false}
	pat2 := [11]bool{false, false, false, false, true, false, true, true, true, false, true}
	for y := range size {
		for x := 0; x <= size-11; x++ {
			m1, m2 := true, true
			for k := range 11 {
				v := modules[y*size+x+k]
				if v != pat1[k] {
					m1 = false
				}
				if v != pat2[k] {
					m2 = false
				}
			}
			if m1 || m2 {
				penalty += 40
			}
		}
	}
	for x := range size {
		for y := 0; y <= size-11; y++ {
			m1, m2 := true, true
			for k := range 11 {
				v := modules[(y+k)*size+x]
				if v != pat1[k] {
					m1 = false
				}
				if v != pat2[k] {
					m2 = false
				}
			}
			if m1 || m2 {
				penalty += 40
			}
		}
	}

	// N4: dark module ratio deviation from 50% in 5% steps (+10 per step).
	darkCount := 0
	for _, m := range modules {
		if m {
			darkCount++
		}
	}
	total := size * size
	// k = floor(|20*dark - 10*total| / total)
	diff := absInt(20*darkCount - 10*total)
	penalty += (diff / total) * 10

	return penalty
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
