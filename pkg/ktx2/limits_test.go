package ktx2_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"runtime"
	"testing"

	"github.com/nsgo-dev/game/pkg/ktx2"
)

// zstdRLEFrame returns one Zstandard frame without a content size that decodes to n copies of val through 128 KiB RLE blocks, four bytes of input per block.
func zstdRLEFrame(n int, val byte) []byte {
	const block = 128 * 1024
	frame := []byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 17 << 3} // magic, no content size, 128 MiB window
	for n > 0 {
		size := min(n, block)
		n -= size
		bh := uint32(size)<<3 | 1<<1
		if n == 0 {
			bh |= 1
		}
		frame = append(frame, byte(bh), byte(bh>>8), byte(bh>>16), val)
	}
	return frame
}

// hugeZstdBC1 returns a KTX2 file of a few kilobytes holding one 16384x16384 BC1 level, 128 MiB once its Zstandard frame of RLE blocks is decoded and 1 GiB as RGBA.
func hugeZstdBC1(t testing.TB) []byte {
	t.Helper()
	const dim = 16384
	base, err := ktx2.Encode(image.NewNRGBA(image.Rect(0, 0, 4, 4)), ktx2.Options{Format: ktx2.FormatBC1})
	if err != nil {
		t.Fatal(err)
	}
	levelBytes := ktx2.FormatBC1.LevelByteSize(dim, dim)
	data := repackKTX2Supercompressed(t, base, ktx2.SupercompressionZstandard, [][]byte{zstdRLEFrame(levelBytes, 0)})
	binary.LittleEndian.PutUint32(data[20:24], dim)
	binary.LittleEndian.PutUint32(data[24:28], dim)
	binary.LittleEndian.PutUint64(data[ktx2.HeaderSize+16:ktx2.HeaderSize+24], uint64(levelBytes))
	if err := ktx2.Validate(data); err != nil {
		t.Fatalf("crafted file must be structurally valid: %v", err)
	}
	return data
}

func allocatedBytes(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestDecodeRefusesRGBAOverLimit checks that a tiny Zstandard file whose level would decode to a 1 GiB RGBA image is refused: DecodeRGBA refuses it before decompressing anything, and DecodeLevelRGBA and Transcode refuse it before allocating the image.
func TestDecodeRefusesRGBAOverLimit(t *testing.T) {
	data := hugeZstdBC1(t)
	if len(data) > 8*1024 {
		t.Fatalf("crafted file is %d bytes, want a few kilobytes", len(data))
	}

	var err error
	if n := allocatedBytes(func() { _, err = ktx2.DecodeRGBA(data, 0) }); n > 1<<20 {
		t.Errorf("DecodeRGBA allocated %d bytes before refusing, want under 1 MiB", n)
	}
	if err == nil {
		t.Fatal("DecodeRGBA accepted a level that decodes to a 1 GiB RGBA image")
	}

	tex, err := ktx2.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n := allocatedBytes(func() { _, err = tex.DecodeLevelRGBA(0) }); n > 1<<20 {
		t.Errorf("DecodeLevelRGBA allocated %d bytes before refusing, want under 1 MiB", n)
	}
	if err == nil {
		t.Fatal("DecodeLevelRGBA accepted a level that decodes to a 1 GiB RGBA image")
	}
	for _, target := range []ktx2.Format{ktx2.FormatRGBA8, ktx2.FormatBC7} {
		if n := allocatedBytes(func() { _, err = tex.Transcode(target) }); n > 1<<20 {
			t.Errorf("Transcode(%s) allocated %d bytes before refusing, want under 1 MiB", target, n)
		}
		if err == nil {
			t.Fatalf("Transcode(%s) accepted a level that decodes to a 1 GiB RGBA image", target)
		}
	}
	// The level's own payload stays within the per-level limit, so a texture that is uploaded in its block format still loads.
	if lv, err := tex.Transcode(ktx2.FormatBC1); err != nil || len(lv[0].Data) != ktx2.FormatBC1.LevelByteSize(16384, 16384) {
		t.Fatalf("Transcode(BC1) = %v, want the level cloned", err)
	}
}

// TestDecodeAcceptsRGBAAtLimit checks that a level whose RGBA image is exactly at the limit, 8192x8192, still decodes.
func TestDecodeAcceptsRGBAAtLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes a 256 MiB image")
	}
	const dim = 8192
	base, err := ktx2.Encode(image.NewNRGBA(image.Rect(0, 0, 4, 4)), ktx2.Options{Format: ktx2.FormatBC1})
	if err != nil {
		t.Fatal(err)
	}
	levelBytes := ktx2.FormatBC1.LevelByteSize(dim, dim)
	data := repackKTX2Supercompressed(t, base, ktx2.SupercompressionZstandard, [][]byte{zstdRLEFrame(levelBytes, 0)})
	binary.LittleEndian.PutUint32(data[20:24], dim)
	binary.LittleEndian.PutUint32(data[24:28], dim)
	binary.LittleEndian.PutUint64(data[ktx2.HeaderSize+16:ktx2.HeaderSize+24], uint64(levelBytes))
	img, err := ktx2.DecodeRGBA(data, 0)
	if err != nil {
		t.Fatalf("DecodeRGBA at the limit: %v", err)
	}
	if b := img.Bounds(); b.Dx() != dim || b.Dy() != dim {
		t.Fatalf("decoded %v, want %dx%d", b, dim, dim)
	}
}

// TestKVDBinaryAndStringValuesRoundTrip checks that a binary value the KTX2 specification defines keeps its trailing zero bytes, and a string value loses only the terminator buildKVD writes.
func TestKVDBinaryAndStringValuesRoundTrip(t *testing.T) {
	kvd := map[string]string{
		"KTXdxgiFormat__": "\x1c\x00\x00\x00",
		"KTXglFormat":     "\x58\x80\x00\x00\x08\x19\x00\x00\x00\x00\x00\x00",
		"GameAsset":       "hero",
		"Padded":          "tail\x00",
	}
	raw, err := ktx2.Encode(image.NewNRGBA(image.Rect(0, 0, 4, 4)), ktx2.Options{Format: ktx2.FormatRGBA8, KVD: kvd})
	if err != nil {
		t.Fatal(err)
	}
	tex, err := ktx2.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range kvd {
		if got := tex.KVD[k]; !bytes.Equal(got, []byte(want)) {
			t.Errorf("KVD[%q] = %q, want %q", k, got, want)
		}
	}
	if got := string(tex.KVD["KTXwriter"]); got != "nsgo game tex" {
		t.Errorf("KVD[KTXwriter] = %q, want the string without its terminator", got)
	}

	// A binary value another tool wrote, without a terminator, reads back byte for byte.
	entry := append([]byte("KTXdxgiFormat__\x00"), 0x1c, 0, 0, 0)
	buf := binary.LittleEndian.AppendUint32(nil, uint32(len(entry)))
	buf = append(buf, entry...)
	got, err := ktx2.DecodeKVD(buf)
	if err != nil {
		t.Fatal(err)
	}
	if v := got["KTXdxgiFormat__"]; !bytes.Equal(v, []byte{0x1c, 0, 0, 0}) {
		t.Errorf("DecodeKVD binary value = %q, want \"\\x1c\\x00\\x00\\x00\"", v)
	}
}
