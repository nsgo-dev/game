package qrcode

import (
	"errors"
	"strings"
	"testing"
)

// decodeForTest independently extracts format info, unmasks the QR symbol,
// verifies zero Reed-Solomon syndromes on every block, and decodes the byte-mode payload.
func decodeForTest(t *testing.T, c *Code) string {
	t.Helper()
	size := c.Size
	if size != 4*c.Version+17 {
		t.Fatalf("size = %d, want %d for version %d", size, 4*c.Version+17, c.Version)
	}

	// Verify 3 finder patterns (7×7) and 1-module light separators.
	checkFinder := func(ox, oy int) {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				x, y := ox+dx, oy+dy
				if x < 0 || y < 0 || x >= size || y >= size {
					continue
				}
				want := false
				if dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6 {
					dist := max(absInt(dx-3), absInt(dy-3))
					want = dist != 2
				}
				if got := c.Black(x, y); got != want {
					t.Fatalf("finder (%d,%d) module (%d,%d) = %v, want %v", ox, oy, x, y, got, want)
				}
			}
		}
	}
	checkFinder(0, 0)
	checkFinder(size-7, 0)
	checkFinder(0, size-7)

	// Verify timing patterns on row 6 and column 6.
	for i := 8; i < size-8; i++ {
		want := i%2 == 0
		if c.Black(i, 6) != want || c.Black(6, i) != want {
			t.Fatalf("timing pattern mismatch at index %d", i)
		}
	}

	// Verify fixed dark module.
	if !c.Black(8, 4*c.Version+9) {
		t.Fatalf("dark module at (8,%d) is light", 4*c.Version+9)
	}

	// Extract both copies of format info and verify they match and pass BCH(15,5).
	coords1 := [15][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8},
		{7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
	}
	coords2 := [15][2]int{
		{size - 1, 8}, {size - 2, 8}, {size - 3, 8}, {size - 4, 8},
		{size - 5, 8}, {size - 6, 8}, {size - 7, 8}, {size - 8, 8},
		{8, size - 7}, {8, size - 6}, {8, size - 5}, {8, size - 4},
		{8, size - 3}, {8, size - 2}, {8, size - 1},
	}
	var fmt1, fmt2 uint16
	for i := range 15 {
		if c.Black(coords1[i][0], coords1[i][1]) {
			fmt1 |= 1 << i
		}
		if c.Black(coords2[i][0], coords2[i][1]) {
			fmt2 |= 1 << i
		}
	}
	if fmt1 != fmt2 {
		t.Fatalf("format info copies differ: 0x%04x vs 0x%04x", fmt1, fmt2)
	}
	unmaskedFmt := fmt1 ^ 0x5412
	// Verify BCH(15,5) remainder is 0 with generator 0x537.
	rem := unmaskedFmt
	for i := 14; i >= 10; i-- {
		if (rem & (1 << i)) != 0 {
			rem ^= 0x537 << (i - 10)
		}
	}
	if rem != 0 {
		t.Fatalf("format info BCH check failed: remainder=0x%x (raw=0x%04x)", rem, fmt1)
	}
	decodedMask := int((unmaskedFmt >> 10) & 0x7)
	decodedLevelBits := (unmaskedFmt >> 13) & 0x3
	if decodedMask != c.Mask {
		t.Fatalf("decoded mask = %d, want %d", decodedMask, c.Mask)
	}
	if decodedLevelBits != c.Level.formatBits() {
		t.Fatalf("decoded level bits = %b, want %b", decodedLevelBits, c.Level.formatBits())
	}

	// Verify version info for versions 7..10.
	if c.Version >= 7 {
		var v1, v2 uint32
		for i := range 18 {
			a := i / 3
			b := size - 11 + (i % 3)
			if c.Black(b, a) {
				v1 |= 1 << i
			}
			if c.Black(a, b) {
				v2 |= 1 << i
			}
		}
		if v1 != v2 {
			t.Fatalf("version info copies differ: 0x%05x vs 0x%05x", v1, v2)
		}
		if int(v1>>12) != c.Version {
			t.Fatalf("version info version = %d, want %d", v1>>12, c.Version)
		}
		vrem := v1
		for i := 17; i >= 12; i-- {
			if (vrem & (1 << i)) != 0 {
				vrem ^= 0x1F25 << (i - 12)
			}
		}
		if vrem != 0 {
			t.Fatalf("version info BCH check failed: remainder=0x%x", vrem)
		}
	}

	// Recompute function mask and read unmasked data bits in ISO/IEC 18004 zig-zag order.
	dummy := make([]bool, size*size)
	funcMask := make([]bool, size*size)
	placeFunctionPatterns(dummy, funcMask, c.Version, size)

	spec := versionSpecs[c.Version][c.Level]
	totalCodewords := spec.totalDataCodewords() + (spec.g1Blocks+spec.g2Blocks)*spec.ecPerBlock
	rawBits := make([]byte, 0, totalCodewords*8)
	upward := true
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for step := range size {
			y := step
			if upward {
				y = size - 1 - step
			}
			for dx := range 2 {
				x := right - dx
				if funcMask[y*size+x] {
					continue
				}
				v := c.Black(x, y)
				if maskBit(decodedMask, x, y) {
					v = !v
				}
				if v {
					rawBits = append(rawBits, 1)
				} else {
					rawBits = append(rawBits, 0)
				}
			}
		}
		upward = !upward
	}
	if len(rawBits) < totalCodewords*8 {
		t.Fatalf("read %d data bits, need at least %d", len(rawBits), totalCodewords*8)
	}
	stream := make([]byte, totalCodewords)
	for i := range totalCodewords {
		var b byte
		for j := range 8 {
			b = (b << 1) | rawBits[i*8+j]
		}
		stream[i] = b
	}

	// De-interleave data and EC blocks and verify every RS syndrome is zero.
	numBlocks := spec.g1Blocks + spec.g2Blocks
	dataBlocks := make([][]byte, numBlocks)
	ecBlocks := make([][]byte, numBlocks)
	for i := range spec.g1Blocks {
		dataBlocks[i] = make([]byte, spec.g1Data)
		ecBlocks[i] = make([]byte, spec.ecPerBlock)
	}
	for i := range spec.g2Blocks {
		dataBlocks[spec.g1Blocks+i] = make([]byte, spec.g2Data)
		ecBlocks[spec.g1Blocks+i] = make([]byte, spec.ecPerBlock)
	}
	pos := 0
	maxData := max(spec.g1Data, spec.g2Data)
	for i := range maxData {
		for b := range numBlocks {
			if i < len(dataBlocks[b]) {
				dataBlocks[b][i] = stream[pos]
				pos++
			}
		}
	}
	for i := range spec.ecPerBlock {
		for b := range numBlocks {
			ecBlocks[b][i] = stream[pos]
			pos++
		}
	}

	var dataCodewords []byte
	for b := range numBlocks {
		// Verify RS syndromes: evaluating polynomial (data || ec) at alpha^j for j=0..ecPerBlock-1 must be 0.
		cw := append(append([]byte(nil), dataBlocks[b]...), ecBlocks[b]...)
		for j := range spec.ecPerBlock {
			alpha := gfExp[j]
			var acc byte
			for _, coeff := range cw {
				acc = gfMul(acc, alpha) ^ coeff
			}
			if acc != 0 {
				t.Fatalf("block %d RS syndrome[%d] = 0x%02x, want 0", b, j, acc)
			}
		}
		dataCodewords = append(dataCodewords, dataBlocks[b]...)
	}

	// Decode byte-mode bitstream.
	bitAt := func(i int) uint32 {
		return uint32((dataCodewords[i>>3] >> (7 - uint(i&7))) & 1)
	}
	readBits := func(off, n int) uint32 {
		var v uint32
		for i := range n {
			v = (v << 1) | bitAt(off+i)
		}
		return v
	}
	mode := readBits(0, 4)
	if mode != 0b0100 {
		t.Fatalf("mode = 0b%04b, want 0b0100 (byte mode)", mode)
	}
	cntBits := byteCountBits(c.Version)
	length := int(readBits(4, cntBits))
	off := 4 + cntBits
	out := make([]byte, length)
	for i := range length {
		out[i] = byte(readBits(off+i*8, 8))
	}
	return string(out)
}

func TestEncodeRoundTripAllLevelsAndVersions(t *testing.T) {
	payloads := []string{
		"http://192.168.1.24:8731/",
		"http://10.0.0.42:54321/?play=1&mcp=1",
		"http://172.16.4.99:8080/dungeon-run/index.html",
		"a",
		strings.Repeat("nsgo-lan-qr/", 5), // hits version 3–5
		strings.Repeat("nsgo-lan-qr/", 9), // 108 bytes: hits version 6–10 (including version info)
		strings.Repeat("x", 119),          // max v10 level H capacity
	}
	levels := []Level{Low, Medium, Quartile, High}

	for _, payload := range payloads {
		for _, lvl := range levels {
			code, err := Encode(payload, lvl)
			if err != nil {
				t.Fatalf("Encode(%d bytes, %s): %v", len(payload), lvl, err)
			}
			got := decodeForTest(t, code)
			if got != payload {
				t.Fatalf("roundtrip (%s, v%d) = %q, want %q", lvl, code.Version, got, payload)
			}
			if code.QuietZone() != 4 {
				t.Fatalf("QuietZone() = %d, want 4", code.QuietZone())
			}
			// Out-of-bounds Black() returns false (quiet zone).
			if code.Black(-1, 0) || code.Black(0, -1) || code.Black(code.Size, 0) || code.Black(0, code.Size) {
				t.Fatalf("out-of-bounds Black() must return false")
			}
		}
	}
}

func TestEncodeDeterminismAndMatrixCopy(t *testing.T) {
	url := "http://192.168.1.24:8731/"
	c1, err := Encode(url, Medium)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Encode(url, Medium)
	if err != nil {
		t.Fatal(err)
	}
	if c1.Version != c2.Version || c1.Mask != c2.Mask || c1.Size != c2.Size {
		t.Fatalf("non-deterministic metadata: %+v vs %+v", c1, c2)
	}
	m1 := c1.Matrix()
	m2 := c2.Matrix()
	for y := range c1.Size {
		for x := range c1.Size {
			if m1[y][x] != m2[y][x] {
				t.Fatalf("non-deterministic module at (%d,%d)", x, y)
			}
		}
	}
	// Mutating returned Matrix must not affect Code.
	m1[0][0] = !m1[0][0]
	if c1.Black(0, 0) == m1[0][0] {
		t.Fatal("Matrix() did not return a defensive copy")
	}
}

func TestEncodeErrors(t *testing.T) {
	if _, err := Encode("", Medium); !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("empty input err = %v, want ErrEmptyInput", err)
	}
	if _, err := Encode(strings.Repeat("a", 400), Low); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized input err = %v, want ErrPayloadTooLarge", err)
	}
	if _, err := Encode("hi", Level(99)); err == nil {
		t.Fatal("expected error for invalid EC level")
	}
}
