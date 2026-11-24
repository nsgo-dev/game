package ktx2

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestUASTCPartitionTablesMatchAnchors(t *testing.T) {
	firstIndex := func(pat [16]uint8, subset uint8) uint8 {
		for i, s := range pat {
			if s == subset {
				return uint8(i)
			}
		}
		return 255
	}
	for i, pat := range uastcPat24916 {
		for s := uint8(0); s < 2; s++ {
			if got := firstIndex(pat, s); got != uastcAnchors24916[i][s] {
				t.Errorf("pat24916[%d] subset %d first=%d, want anchor %d", i, s, got, uastcAnchors24916[i][s])
			}
		}
	}
	for i, pat := range uastcPat7 {
		for s := uint8(0); s < 2; s++ {
			if got := firstIndex(pat, s); got != uastcAnchors7[i][s] {
				t.Errorf("pat7[%d] subset %d first=%d, want anchor %d", i, s, got, uastcAnchors7[i][s])
			}
		}
	}
	for i, pat := range uastcPat3 {
		for s := uint8(0); s < 3; s++ {
			if got := firstIndex(pat, s); got != uastcAnchors3[i][s] {
				t.Errorf("pat3[%d] subset %d first=%d, want anchor %d", i, s, got, uastcAnchors3[i][s])
			}
		}
	}
}

func TestUASTCDecodeAllModesAndErrorBlocks(t *testing.T) {
	// Verify every mode 0..18 decodes cleanly (all-zero endpoints/weights after mode prefix) and produces expected opaque/alpha behavior without panicking or hitting error magenta.
	for mode := range 19 {
		var blk [16]byte
		info := uastcModes[mode]
		writeBitsLE128(blk[:], 0, int(info.huffLen), uint32(info.huffCode))
		if mode == 8 {
			writeBitsLE128(blk[:], 5, 8, 10)
			writeBitsLE128(blk[:], 13, 8, 20)
			writeBitsLE128(blk[:], 21, 8, 30)
			writeBitsLE128(blk[:], 29, 8, 40)
		}
		var out [16][4]uint8
		decodeUASTCBlock(blk[:], false, &out)
		if mode == 8 {
			if out[0] != [4]uint8{10, 20, 30, 40} {
				t.Errorf("mode 8 got %v, want [10 20 30 40]", out[0])
			}
			continue
		}
		// All-zero endpoints & weights decode to (0,0,0,255) for RGB modes and (0,0,0,0) for RGBA/LA modes.
		wantA := uint8(255)
		if info.comps != 3 {
			wantA = 0
		}
		want := [4]uint8{0, 0, 0, wantA}
		for i, px := range out {
			if px != want {
				t.Errorf("mode %d pixel %d = %v, want %v", mode, i, px, want)
				break
			}
		}
	}

	// Reserved mode 19 must decode to Khronos error color (255, 0, 255, 255).
	var blk19 [16]byte
	writeBitsLE128(blk19[:], 0, 7, uint32(uastcModes[19].huffCode))
	var errOut [16][4]uint8
	decodeUASTCBlock(blk19[:], false, &errOut)
	if errOut[0] != [4]uint8{255, 0, 255, 255} {
		t.Errorf("mode 19 got %v, want error magenta [255 0 255 255]", errOut[0])
	}

	// Invalid PAT index in mode 3 (>= 11) must decode to error magenta.
	var badPat [16]byte
	writeBitsLE128(badPat[:], 0, 5, uint32(uastcModes[3].huffCode))
	writeBitsLE128(badPat[:], 20, 4, 15) // PAT=15 > 10
	decodeUASTCBlock(badPat[:], false, &errOut)
	if errOut[0] != [4]uint8{255, 0, 255, 255} {
		t.Errorf("invalid PAT got %v, want error magenta [255 0 255 255]", errOut[0])
	}

	// Mode 15 (LA Direct, 8-bit L & A endpoints, 4-bit weights): verify L->RGB replication and alpha interpolation.
	var blk15 [16]byte
	writeBitsLE128(blk15[:], 0, 7, uint32(uastcModes[15].huffCode))
	// Config ends at bit 30; LL0=20, LH0=220, AL0=40, AH0=240.
	writeBitsLE128(blk15[:], 30, 8, 20)
	writeBitsLE128(blk15[:], 38, 8, 220)
	writeBitsLE128(blk15[:], 46, 8, 40)
	writeBitsLE128(blk15[:], 54, 8, 240)
	// Texel 0 anchor weight (3b) = 0 -> (20,20,20,40); Texel 1 weight (4b) = 15 -> (220,220,220,240).
	writeBitsLE128(blk15[:], 62, 3, 0)
	writeBitsLE128(blk15[:], 65, 4, 15)
	var out15 [16][4]uint8
	decodeUASTCBlock(blk15[:], false, &out15)
	if out15[0] != [4]uint8{20, 20, 20, 40} || out15[1] != [4]uint8{220, 220, 220, 240} {
		t.Errorf("mode 15 decoded [0]=%v [1]=%v, want [20 20 20 40] and [220 220 220 240]", out15[0], out15[1])
	}
}

func TestZstdRawRLESkippableAndBounds(t *testing.T) {
	// Build a skippable frame followed by a single-segment frame containing a raw block + an RLE block.
	var buf bytes.Buffer
	// Skippable frame (magic 0x184D2A50, size 4, payload "SKIP")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(zstdSkippableMagicLo))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.WriteString("SKIP")

	// Standard Zstd frame: single-segment (0x20), FCS=1 byte (9 bytes total: 4 raw + 5 RLE).
	_ = binary.Write(&buf, binary.LittleEndian, uint32(zstdMagic))
	buf.WriteByte(0x20) // Single_Segment_Flag=1, FCS_Flag=0 (1-byte FCS)
	buf.WriteByte(9)    // FCS = 9

	// Block 1: Raw (type=0), last=0, size=4 -> (4<<3) | (0<<1) | 0 = 32
	bh1 := uint32(4 << 3)
	buf.Write([]byte{byte(bh1), byte(bh1 >> 8), byte(bh1 >> 16)})
	buf.WriteString("KTX2")

	// Block 2: RLE (type=1), last=1, size=5 -> (5<<3) | (1<<1) | 1 = 43
	bh2 := uint32((5 << 3) | (1 << 1) | 1)
	buf.Write([]byte{byte(bh2), byte(bh2 >> 8), byte(bh2 >> 16)})
	buf.WriteByte('!')

	got, err := decompressZstd(buf.Bytes(), 64)
	if err != nil {
		t.Fatalf("decompressZstd failed: %v", err)
	}
	if string(got) != "KTX2!!!!!" {
		t.Fatalf("got %q, want %q", got, "KTX2!!!!!")
	}

	// Enforce maxOut limit (zip-bomb protection).
	if _, err := decompressZstd(buf.Bytes(), 4); err == nil {
		t.Fatal("expected error when output exceeds maxOut")
	}
}
