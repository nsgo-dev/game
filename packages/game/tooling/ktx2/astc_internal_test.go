package ktx2

import (
	"image"
	"testing"
)

// TestDecodeASTC4x4ModesAndRefusals verifies that the full CPU ASTC 4x4 LDR decoder
// decodes valid single-partition, multi-partition, dual-plane, and BISE-coded LDR blocks,
// while rejecting illegal block modes, 4-partition dual-plane blocks, invalid void-extent
// coordinates, and HDR endpoint/void-extent modes.
func TestDecodeASTC4x4ModesAndRefusals(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	own := encodeASTC4x4(img)
	if _, err := decodeASTC4x4(own, 4, 4, false); err != nil {
		t.Fatalf("encoder's own block: %v", err)
	}

	// Valid multi-partition, dual-plane, and BISE-quantized LDR blocks must decode without error.
	for name, setup := range map[string]func(b []byte){
		"two partitions shared cem8": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x042) // 4x4 grid, 2-bit weights (32 bits)
			writeBitsLE128(b, 11, 2, 1)     // 2 partitions
			writeBitsLE128(b, 13, 10, 17)   // partition seed
			writeBitsLE128(b, 23, 6, 8<<2)  // shared CEM 8 (12 endpoint ints in 67 bits -> QUANT_24 BISE)
		},
		"four partitions shared cem0": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x042) // 4x4 grid, 2-bit weights (32 bits)
			writeBitsLE128(b, 11, 2, 3)     // 4 partitions
			writeBitsLE128(b, 13, 10, 5)    // partition seed
			writeBitsLE128(b, 23, 6, 0<<2)  // shared CEM 0 (8 luminance ints in 67 bits -> QUANT_256)
		},
		"cem 12 with 3-bit weights (BISE endpoints)": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x053) // 4x4 grid, 3-bit weights (48 bits)
			writeBitsLE128(b, 11, 2, 0)     // 1 partition
			writeBitsLE128(b, 13, 4, 12)    // CEM 12 (8 ints in 63 bits -> QUANT_192 BISE)
		},
		"dual plane 4x4 2-bit weights": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x442) // dual plane, 4x4 grid, 2-bit weights (64 bits)
			writeBitsLE128(b, 11, 2, 0)     // 1 partition
			writeBitsLE128(b, 13, 4, 8)     // CEM 8 (6 ints in 45 bits -> QUANT_160 BISE)
		},
		"decimated 3x3 weight grid": func(b []byte) {
			// bm: bits 0..1=2, bit 9=1 (H=1 -> weightQuant=8=QUANT_16, 9*4=36 bits), bits 2..3=3, bit 8=1 -> wx=3 (bit7=1), wy=3 (bits5..6=1)
			bm := uint32(2 | (3 << 2) | (1 << 5) | (1 << 7) | (1 << 8) | (1 << 9))
			writeBitsLE128(b, 0, 11, bm)
			writeBitsLE128(b, 11, 2, 0)
			writeBitsLE128(b, 13, 4, 8)
		},
	} {
		b := make([]byte, 16)
		setup(b)
		if _, err := decodeASTC4x4(b, 4, 4, false); err != nil {
			t.Errorf("%s: unexpected decode error: %v", name, err)
		}
	}

	// Illegal and HDR blocks must be refused.
	for name, setup := range map[string]func(b []byte){
		"hdr void extent": func(b []byte) {
			b[0], b[1] = 0xFC, 0xFF
		},
		"invalid void extent reserved bits": func(b []byte) {
			b[0], b[1] = 0xFC, 0xF1 // bits 10..11 != 3
		},
		"invalid void extent coordinates": func(b []byte) {
			b[0], b[1] = 0xFC, 0xFD
			// lowS=100, highS=50 (lowS >= highS, not all 1s)
			writeBitsLE128(b, 12, 13, 100)
			writeBitsLE128(b, 25, 13, 50)
			writeBitsLE128(b, 38, 13, 0)
			writeBitsLE128(b, 51, 13, 100)
		},
		"reserved block mode": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0)
		},
		"weight grid larger than 4x4": func(b []byte) {
			// bm with wx=5, wy=2 (illegal for 4x4 footprint)
			writeBitsLE128(b, 0, 11, 0x082)
		},
		"four partitions with dual plane": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x442)
			writeBitsLE128(b, 11, 2, 3)
		},
		"hdr cem 11": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x042)
			writeBitsLE128(b, 11, 2, 0)
			writeBitsLE128(b, 13, 4, 11)
		},
	} {
		b := make([]byte, 16)
		setup(b)
		if _, err := decodeASTC4x4(b, 4, 4, false); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
}

func TestASTCAllLDRColorEndpointModes(t *testing.T) {
	ldrCEMs := []int{0, 1, 4, 5, 6, 8, 9, 10, 12, 13}
	for _, cem := range ldrCEMs {
		b := make([]byte, 16)
		writeBitsLE128(b, 0, 11, 0x042) // 4x4 grid, 2-bit weights
		writeBitsLE128(b, 11, 2, 0)     // 1 partition
		writeBitsLE128(b, 13, 4, uint32(cem))
		for i := range 8 {
			writeBitsLE128(b, 17+i*8, 8, uint32(20+i*25))
		}
		if _, err := decodeASTC4x4(b, 4, 4, false); err != nil {
			t.Errorf("LDR CEM %d failed: %v", cem, err)
		}
	}
	hdrCEMs := []int{2, 3, 7, 11, 14, 15}
	for _, cem := range hdrCEMs {
		b := make([]byte, 16)
		writeBitsLE128(b, 0, 11, 0x042)
		writeBitsLE128(b, 11, 2, 0)
		writeBitsLE128(b, 13, 4, uint32(cem))
		if _, err := decodeASTC4x4(b, 4, 4, false); err == nil {
			t.Errorf("HDR CEM %d unexpectedly succeeded in LDR decoder", cem)
		}
	}
}
