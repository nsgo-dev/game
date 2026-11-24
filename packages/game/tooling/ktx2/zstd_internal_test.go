package ktx2

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures under testdata/zstd were written by the zstd 1.5.7 CLI from payload.bin: level1 and level19 without a content checksum, checksum with one, and twoframes as two concatenated frames holding the payload's two halves. They keep the decoder checked against the reference encoder on machines without the CLI.
func TestDecompressZstdReferenceFrames(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "zstd", "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"level1", "level19", "checksum", "twoframes"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", "zstd", name+".zst"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := decompressZstd(src, uint64(len(payload)))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("decoded %d bytes that differ from the %d-byte payload", len(got), len(payload))
			}
			if _, err := decompressZstd(src, uint64(len(payload)-1)); err == nil {
				t.Fatal("decoding past the output limit succeeded")
			}
		})
	}
	t.Run("bad checksum", func(t *testing.T) {
		src, err := os.ReadFile(filepath.Join("testdata", "zstd", "checksum.zst"))
		if err != nil {
			t.Fatal(err)
		}
		src[len(src)-1] ^= 0xFF
		if _, err := decompressZstd(src, uint64(len(payload))); err == nil {
			t.Fatal("a frame whose content checksum does not match decoded without error")
		}
	})
}

func TestXXHash64(t *testing.T) {
	// Reference values from the xxHash project's XXH64 with seed 0.
	for _, tc := range []struct {
		in   string
		want uint64
	}{
		{"", 0xEF46DB3751D8E999},
		{"a", 0xD24EC4F1A98C6E5B},
		{"abc", 0x44BC2CF5AD770999},
		{"Nobody inspects the spammish repetition", 0xFBCEA83C8A378BF1},
	} {
		if got := xxhash64([]byte(tc.in)); got != tc.want {
			t.Errorf("xxhash64(%q) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}
