package ktx2_test

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/ktx2"
)

func loadRefPNG(t *testing.T, name string) *image.NRGBA {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "ref", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return ktx2.ToNRGBA(img)
}

func assertImagesMatch(t *testing.T, got, want *image.NRGBA, maxChannelDiff int) {
	t.Helper()
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatalf("size = %v, want %v", got.Bounds().Size(), want.Bounds().Size())
	}
	w, h := want.Bounds().Dx(), want.Bounds().Dy()
	worstDiff := 0
	mismatches := 0
	var firstMsg string
	for y := range h {
		for x := range w {
			g := got.NRGBAAt(x, y)
			r := want.NRGBAAt(x, y)
			diffs := [4]int{
				absInt(int(g.R) - int(r.R)),
				absInt(int(g.G) - int(r.G)),
				absInt(int(g.B) - int(r.B)),
				absInt(int(g.A) - int(r.A)),
			}
			d := max(max(diffs[0], diffs[1]), max(diffs[2], diffs[3]))
			if d > worstDiff {
				worstDiff = d
			}
			if d > maxChannelDiff {
				mismatches++
				if firstMsg == "" {
					firstMsg = "first mismatch at (" + itoa(x) + "," + itoa(y) + "): got=" + rgbaStr(g.R, g.G, g.B, g.A) + " want=" + rgbaStr(r.R, r.G, r.B, r.A)
				}
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d pixels exceeded tolerance %d (worst diff=%d); %s", mismatches, maxChannelDiff, worstDiff, firstMsg)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func rgbaStr(r, g, b, a uint8) string {
	return "{" + itoa(int(r)) + "," + itoa(int(g)) + "," + itoa(int(b)) + "," + itoa(int(a)) + "}"
}

// TestReferenceEncoderFixtures verifies the pure-Go BasisLZ/ETC1S, UASTC (uncompressed and Zstandard-supercompressed),
// BC7, and ASTC 4x4 decoders against reference .ktx2 fixtures and expected RGBA PNGs written by basisu and astcenc.
func TestReferenceEncoderFixtures(t *testing.T) {
	cases := []struct {
		name     string
		ktxFile  string
		pngFile  string
		level    int
		format   ktx2.Format
		srgb     bool
		hasAlpha bool
		super    uint32
		maxDiff  int
	}{
		{
			name:     "etc1s opaque srgb mip0",
			ktxFile:  "etc1s_opaque_srgb.ktx2",
			pngFile:  "etc1s_opaque_srgb.png",
			level:    0,
			format:   ktx2.FormatETC1S,
			srgb:     true,
			hasAlpha: false,
			super:    ktx2.SupercompressionBasisLZ,
			maxDiff:  0,
		},
		{
			name:     "etc1s alpha srgb mip0",
			ktxFile:  "etc1s_alpha_srgb.ktx2",
			pngFile:  "etc1s_alpha_srgb.png",
			level:    0,
			format:   ktx2.FormatETC1S,
			srgb:     true,
			hasAlpha: true,
			super:    ktx2.SupercompressionBasisLZ,
			maxDiff:  0,
		},
		{
			name:     "etc1s alpha srgb mip1",
			ktxFile:  "etc1s_alpha_srgb.ktx2",
			pngFile:  "etc1s_alpha_srgb_mip1.png",
			level:    1,
			format:   ktx2.FormatETC1S,
			srgb:     true,
			hasAlpha: true,
			super:    ktx2.SupercompressionBasisLZ,
			maxDiff:  0,
		},
		{
			name:     "etc1s opaque linear mip0",
			ktxFile:  "etc1s_opaque_linear.ktx2",
			pngFile:  "etc1s_opaque_linear.png",
			level:    0,
			format:   ktx2.FormatETC1S,
			srgb:     false,
			hasAlpha: false,
			super:    ktx2.SupercompressionBasisLZ,
			maxDiff:  0,
		},
		{
			name:     "uastc opaque srgb mip0",
			ktxFile:  "uastc_opaque_srgb.ktx2",
			pngFile:  "uastc_opaque_srgb.png",
			level:    0,
			format:   ktx2.FormatUASTC,
			srgb:     true,
			hasAlpha: false,
			super:    ktx2.SupercompressionNone,
			maxDiff:  1,
		},
		{
			name:     "uastc alpha srgb mip0",
			ktxFile:  "uastc_alpha_srgb.ktx2",
			pngFile:  "uastc_alpha_srgb.png",
			level:    0,
			format:   ktx2.FormatUASTC,
			srgb:     true,
			hasAlpha: true,
			super:    ktx2.SupercompressionNone,
			maxDiff:  1,
		},
		{
			name:     "uastc linear zstd mip0",
			ktxFile:  "uastc_linear_zstd.ktx2",
			pngFile:  "uastc_linear_zstd.png",
			level:    0,
			format:   ktx2.FormatUASTC,
			srgb:     false,
			hasAlpha: false,
			super:    ktx2.SupercompressionZstandard,
			maxDiff:  1,
		},
		{
			name:     "uastc linear zstd mip1",
			ktxFile:  "uastc_linear_zstd.ktx2",
			pngFile:  "uastc_linear_zstd_mip1.png",
			level:    1,
			format:   ktx2.FormatUASTC,
			srgb:     false,
			hasAlpha: false,
			super:    ktx2.SupercompressionZstandard,
			maxDiff:  1,
		},
		{
			name:     "bc7 alpha srgb",
			ktxFile:  "bc7_alpha_srgb.ktx2",
			pngFile:  "bc7_alpha_srgb.png",
			level:    0,
			format:   ktx2.FormatBC7,
			srgb:     true,
			hasAlpha: true,
			super:    ktx2.SupercompressionNone,
			maxDiff:  0,
		},
		{
			name:     "bc7 opaque linear",
			ktxFile:  "bc7_opaque_linear.ktx2",
			pngFile:  "bc7_opaque_linear.png",
			level:    0,
			format:   ktx2.FormatBC7,
			srgb:     false,
			hasAlpha: true,
			super:    ktx2.SupercompressionNone,
			maxDiff:  0,
		},
		{
			name:     "astc4x4 opaque linear",
			ktxFile:  "astc4x4_opaque_linear.ktx2",
			pngFile:  "astc4x4_opaque_linear.png",
			level:    0,
			format:   ktx2.FormatASTC4x4,
			srgb:     false,
			hasAlpha: true,
			super:    ktx2.SupercompressionNone,
			maxDiff:  1,
		},
		{
			name:     "astc4x4 alpha srgb",
			ktxFile:  "astc4x4_alpha_srgb.ktx2",
			pngFile:  "astc4x4_alpha_srgb.png",
			level:    0,
			format:   ktx2.FormatASTC4x4,
			srgb:     true,
			hasAlpha: true,
			super:    ktx2.SupercompressionNone,
			maxDiff:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "ref", tc.ktxFile))
			if err != nil {
				t.Fatal(err)
			}
			tex, err := ktx2.Decode(raw)
			if err != nil {
				t.Fatalf("Decode(%s): %v", tc.ktxFile, err)
			}
			if tex.Format != tc.format || tex.SRGB != tc.srgb || tex.HasAlpha != tc.hasAlpha || tex.Header.SupercompressionScheme != tc.super {
				t.Fatalf("metadata mismatch: format=%s srgb=%v alpha=%v super=%d; want format=%s srgb=%v alpha=%v super=%d",
					tex.Format, tex.SRGB, tex.HasAlpha, tex.Header.SupercompressionScheme,
					tc.format, tc.srgb, tc.hasAlpha, tc.super)
			}
			got, err := tex.DecodeLevelRGBA(tc.level)
			if err != nil {
				t.Fatalf("DecodeLevelRGBA(%d): %v", tc.level, err)
			}
			want := loadRefPNG(t, tc.pngFile)
			assertImagesMatch(t, got, want, tc.maxDiff)
		})
	}
}

// TestETC1STranscodeAllTargets verifies BasisLZ/ETC1S textures transcode cleanly to every GPU block format and RGBA8.
func TestETC1STranscodeAllTargets(t *testing.T) {
	targets := []ktx2.Format{
		ktx2.FormatRGBA8,
		ktx2.FormatBC1,
		ktx2.FormatBC3,
		ktx2.FormatBC7,
		ktx2.FormatETC2,
		ktx2.FormatETC2RGBA,
		ktx2.FormatASTC4x4,
	}
	for _, file := range []string{"etc1s_opaque_srgb.ktx2", "etc1s_alpha_srgb.ktx2", "etc1s_opaque_linear.ktx2"} {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "ref", file))
			if err != nil {
				t.Fatal(err)
			}
			tex, err := ktx2.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, dst := range targets {
				levels, err := tex.Transcode(dst)
				if err != nil {
					t.Fatalf("Transcode(%s -> %s): %v", file, dst, err)
				}
				if len(levels) != len(tex.Levels) {
					t.Fatalf("Transcode(%s -> %s): got %d levels, want %d", file, dst, len(levels), len(tex.Levels))
				}
				for i, lv := range levels {
					if wantSize := dst.LevelByteSize(lv.Width, lv.Height); len(lv.Data) != wantSize {
						t.Fatalf("level %d size = %d, want %d", i, len(lv.Data), wantSize)
					}
				}
			}
		})
	}
}
