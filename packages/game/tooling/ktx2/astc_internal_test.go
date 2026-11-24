package ktx2

import (
	"image"
	"testing"
)

// TestDecodeASTC4x4RefusesUnsupportedBlocks holds that a block outside what encodeASTC4x4 writes, such as a two-partition block or CEM 12 under 3-bit weights, is an error rather than a guessed color, while the encoder's own blocks still decode.
func TestDecodeASTC4x4RefusesUnsupportedBlocks(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	own := encodeASTC4x4(img)
	if _, err := decodeASTC4x4(own, 4, 4, false); err != nil {
		t.Fatalf("encoder's own block: %v", err)
	}
	for name, setup := range map[string]func(b []byte){
		"two partitions": func(b []byte) { writeBitsLE128(b, 0, 11, 0x053); writeBitsLE128(b, 11, 2, 1) },
		"cem 12 with 3-bit weights": func(b []byte) {
			writeBitsLE128(b, 0, 11, 0x053)
			writeBitsLE128(b, 13, 4, 12)
		},
		"dual plane": func(b []byte) { writeBitsLE128(b, 0, 11, 0x442); writeBitsLE128(b, 13, 4, 8) },
		"hdr void extent": func(b []byte) {
			b[0], b[1] = 0xFC, 0xFF
		},
	} {
		b := make([]byte, 16)
		setup(b)
		if _, err := decodeASTC4x4(b, 4, 4, false); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
}
