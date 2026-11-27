package ktx2_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/ktx2"
)

func makeGradientImage(w, h int, withAlpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			r := uint8((x * 255) / max(1, w-1))
			g := uint8((y * 255) / max(1, h-1))
			b := uint8(255 - ((x+y)*255)/max(1, w+h-2))
			a := uint8(255)
			if withAlpha {
				a = uint8((x*180 + y*75) / max(1, w+h-2))
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return img
}

func computePSNR(a, b *image.NRGBA, includeAlpha bool) float64 {
	w, h := a.Bounds().Dx(), a.Bounds().Dy()
	var mse float64
	channels := 3
	if includeAlpha {
		channels = 4
	}
	for y := range h {
		for x := range w {
			ao := y*a.Stride + x*4
			bo := y*b.Stride + x*4
			for c := range channels {
				d := float64(a.Pix[ao+c]) - float64(b.Pix[bo+c])
				mse += d * d
			}
		}
	}
	mse /= float64(w * h * channels)
	if mse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10((255.0*255.0)/mse)
}

func TestHeaderMagicDFDKVDAndLevelIndex(t *testing.T) {
	src := makeGradientImage(64, 32, true)
	raw, err := ktx2.Encode(src, ktx2.Options{
		Format:  ktx2.FormatBC3,
		Mipmaps: true,
		SRGB:    true,
		KVD:     map[string]string{"GameAsset": "hero_albedo"},
	})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Verify exact 12-byte KTX2 v2.0 magic signature.
	if !bytes.Equal(raw[:12], ktx2.Identifier[:]) {
		t.Fatalf("magic = %x, want %x", raw[:12], ktx2.Identifier)
	}

	if err := ktx2.Validate(raw); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	h, levels, err := ktx2.DecodeHeader(raw)
	if err != nil {
		t.Fatalf("DecodeHeader failed: %v", err)
	}
	if h.VkFormat != ktx2.VkFormatBC3SRGBBlock {
		t.Fatalf("VkFormat = %d, want %d", h.VkFormat, ktx2.VkFormatBC3SRGBBlock)
	}
	if h.TypeSize != 1 || h.PixelWidth != 64 || h.PixelHeight != 32 || h.FaceCount != 1 {
		t.Fatalf("unexpected header fields: %+v", h)
	}
	// 64x32 -> 32x16 -> 16x8 -> 8x4 -> 4x2 -> 2x1 -> 1x1 = 7 levels
	if h.LevelCount != 7 || len(levels) != 7 {
		t.Fatalf("LevelCount = %d (entries=%d), want 7", h.LevelCount, len(levels))
	}

	// Verify smallest-to-largest offset layout in file body.
	for i := 1; i < len(levels); i++ {
		if levels[i].ByteOffset >= levels[i-1].ByteOffset {
			t.Fatalf("expected level %d offset (%d) < level %d offset (%d)", i, levels[i].ByteOffset, i-1, levels[i-1].ByteOffset)
		}
	}

	// Verify DFD fields for BC3 sRGB.
	dfd, err := ktx2.ParseDFD(raw[h.DFDByteOffset : h.DFDByteOffset+h.DFDByteLength])
	if err != nil {
		t.Fatalf("ParseDFD failed: %v", err)
	}
	if dfd.ColorModel != ktx2.DFDModelBC3 || dfd.TransferFunction != ktx2.DFDTransferSRGB {
		t.Fatalf("unexpected DFD model/transfer: %+v", dfd)
	}
	if dfd.TexelBlockDimensions != [4]uint8{3, 3, 0, 0} || dfd.BytesPlane[0] != 16 || len(dfd.Samples) != 2 {
		t.Fatalf("unexpected DFD block layout: %+v", dfd)
	}

	// Verify KVD metadata.
	kvd, err := ktx2.DecodeKVD(raw[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength])
	if err != nil {
		t.Fatalf("DecodeKVD failed: %v", err)
	}
	if string(bytes.TrimRight(kvd["KTXorientation"], "\x00")) != "rd" {
		t.Fatalf("KTXorientation = %q, want rd", kvd["KTXorientation"])
	}
	if string(bytes.TrimRight(kvd["GameAsset"], "\x00")) != "hero_albedo" {
		t.Fatalf("GameAsset = %q, want hero_albedo", kvd["GameAsset"])
	}

	// Verify ReadLevel dimensions for every mip.
	wantDims := [][2]int{{64, 32}, {32, 16}, {16, 8}, {8, 4}, {4, 2}, {2, 1}, {1, 1}}
	for lvl, wd := range wantDims {
		_, w, ht, err := ktx2.ReadLevel(raw, lvl)
		if err != nil {
			t.Fatalf("ReadLevel(%d) failed: %v", lvl, err)
		}
		if w != wd[0] || ht != wd[1] {
			t.Fatalf("level %d dims = %dx%d, want %dx%d", lvl, w, ht, wd[0], wd[1])
		}
	}
}

func TestMipmapGenerationSRGBVsLinearAndOddDimensions(t *testing.T) {
	// 2x2 black & white checkerboard: linear average is 128, sRGB linear-light average is ~188!
	checker := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			v := uint8(0)
			if (x+y)%2 == 0 {
				v = 255
			}
			checker.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}

	linMips := ktx2.GenerateMipmaps(checker, false)
	srgbMips := ktx2.GenerateMipmaps(checker, true)
	if len(linMips) != 3 || len(srgbMips) != 3 {
		t.Fatalf("mip counts = %d/%d, want 3", len(linMips), len(srgbMips))
	}
	linVal := linMips[1].NRGBAAt(0, 0).R
	srgbVal := srgbMips[1].NRGBAAt(0, 0).R
	if linVal < 126 || linVal > 129 {
		t.Fatalf("linear mip value = %d, want ~128", linVal)
	}
	if srgbVal < 185 || srgbVal > 190 {
		t.Fatalf("sRGB linear-light mip value = %d, want ~188", srgbVal)
	}

	// Non-power-of-two odd dimensions: 13x7 -> 6x3 -> 3x1 -> 1x1 (4 levels).
	odd := makeGradientImage(13, 7, false)
	oddMips := ktx2.GenerateMipmaps(odd, false)
	want := [][2]int{{13, 7}, {6, 3}, {3, 1}, {1, 1}}
	if len(oddMips) != len(want) {
		t.Fatalf("odd mip count = %d, want %d", len(oddMips), len(want))
	}
	for i, w := range want {
		b := oddMips[i].Bounds()
		if b.Dx() != w[0] || b.Dy() != w[1] {
			t.Fatalf("odd mip %d = %dx%d, want %dx%d", i, b.Dx(), b.Dy(), w[0], w[1])
		}
	}
}

func TestAllFormatsEncodeDecodePSNRAndRoundTrip(t *testing.T) {
	opaque := makeGradientImage(32, 32, false)
	withAlpha := makeGradientImage(32, 32, true)

	t.Run("RGBA8 lossless round-trip", func(t *testing.T) {
		raw, err := ktx2.Encode(withAlpha, ktx2.Options{Format: ktx2.FormatRGBA8, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := ktx2.Validate(raw); err != nil {
			t.Fatal(err)
		}
		dec, err := ktx2.DecodeRGBA(raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(dec.Pix, withAlpha.Pix) {
			t.Fatal("RGBA8 round-trip was not bit-exact")
		}
	})

	t.Run("BC1 opaque high PSNR and 1-bit punch-through alpha", func(t *testing.T) {
		raw, err := ktx2.Encode(opaque, ktx2.Options{Format: ktx2.FormatBC1, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := ktx2.Validate(raw); err != nil {
			t.Fatal(err)
		}
		dec, err := ktx2.DecodeRGBA(raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		psnr := computePSNR(opaque, dec, false)
		if psnr < 32.0 {
			t.Fatalf("BC1 2D gradient PSNR = %.2f dB, want >= 32.0 dB", psnr)
		}

		// On a 1D color ramp (collinear colors per 4x4 block), BC1 achieves > 42 dB.
		ramp := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := range 32 {
			for x := range 32 {
				tVal := float64(x+y) / 62.0
				ramp.SetNRGBA(x, y, color.NRGBA{
					R: uint8(30 + tVal*200),
					G: uint8(60 + tVal*160),
					B: uint8(220 - tVal*180),
					A: 255,
				})
			}
		}
		rawRamp, err := ktx2.Encode(ramp, ktx2.Options{Format: ktx2.FormatBC1})
		if err != nil {
			t.Fatal(err)
		}
		decRamp, err := ktx2.DecodeRGBA(rawRamp, 0)
		if err != nil {
			t.Fatal(err)
		}
		if rampPSNR := computePSNR(ramp, decRamp, false); rampPSNR < 40.0 {
			t.Fatalf("BC1 1D ramp PSNR = %.2f dB, want >= 40.0 dB", rampPSNR)
		}

		// Verify 1-bit punch-through alpha on a 4x4 block with transparent pixels.
		punch := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := range 8 {
			for x := range 8 {
				if (x+y)%2 == 0 {
					punch.SetNRGBA(x, y, color.NRGBA{R: 200, G: 80, B: 40, A: 255})
				} else {
					punch.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
				}
			}
		}
		rawPunch, err := ktx2.Encode(punch, ktx2.Options{Format: ktx2.FormatBC1})
		if err != nil {
			t.Fatal(err)
		}
		decPunch, err := ktx2.DecodeRGBA(rawPunch, 0)
		if err != nil {
			t.Fatal(err)
		}
		for y := range 8 {
			for x := range 8 {
				gotA := decPunch.NRGBAAt(x, y).A
				wantA := punch.NRGBAAt(x, y).A
				if gotA != wantA {
					t.Fatalf("BC1 punch-through alpha at (%d,%d) = %d, want %d", x, y, gotA, wantA)
				}
			}
		}
	})

	t.Run("BC3 smooth RGB+Alpha high PSNR", func(t *testing.T) {
		raw, err := ktx2.Encode(withAlpha, ktx2.Options{Format: ktx2.FormatBC3, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := ktx2.Validate(raw); err != nil {
			t.Fatal(err)
		}
		dec, err := ktx2.DecodeRGBA(raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		psnr := computePSNR(withAlpha, dec, true)
		if psnr < 33.0 {
			t.Fatalf("BC3 RGBA 2D gradient PSNR = %.2f dB, want >= 33.0 dB", psnr)
		}
		// Decode smallest 1x1 mip as well to verify non-multiple-of-4 handling.
		mip1x1, err := ktx2.DecodeRGBA(raw, 5)
		if err != nil {
			t.Fatal(err)
		}
		if mip1x1.Bounds().Dx() != 1 || mip1x1.Bounds().Dy() != 1 {
			t.Fatalf("mip 5 size = %v, want 1x1", mip1x1.Bounds())
		}
	})

	t.Run("ETC2 RGB8 high PSNR on gradients and sharp edges", func(t *testing.T) {
		raw, err := ktx2.Encode(opaque, ktx2.Options{Format: ktx2.FormatETC2, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := ktx2.Validate(raw); err != nil {
			t.Fatal(err)
		}
		dec, err := ktx2.DecodeRGBA(raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		psnr := computePSNR(opaque, dec, false)
		if psnr < 38.0 {
			t.Fatalf("ETC2 smooth gradient PSNR = %.2f dB, want >= 38.0 dB", psnr)
		}

		// Also test a high-contrast sharp-edge pattern (exercises Individual/Differential subblocks).
		sharp := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := range 16 {
			for x := range 16 {
				if (x/2+y/2)%2 == 0 {
					sharp.SetNRGBA(x, y, color.NRGBA{R: 230, G: 40, B: 60, A: 255})
				} else {
					sharp.SetNRGBA(x, y, color.NRGBA{R: 30, G: 190, B: 220, A: 255})
				}
			}
		}
		rawSharp, err := ktx2.Encode(sharp, ktx2.Options{Format: ktx2.FormatETC2})
		if err != nil {
			t.Fatal(err)
		}
		decSharp, err := ktx2.DecodeRGBA(rawSharp, 0)
		if err != nil {
			t.Fatal(err)
		}
		psnrSharp := computePSNR(sharp, decSharp, false)
		if psnrSharp < 32.0 {
			t.Fatalf("ETC2 sharp-edge PSNR = %.2f dB, want >= 32.0 dB", psnrSharp)
		}
	})
}

func TestValidateRejectsMalformedInputs(t *testing.T) {
	if err := ktx2.Validate([]byte("not a ktx2 file")); err == nil {
		t.Fatal("expected error for invalid magic")
	}
	src := makeGradientImage(8, 8, false)
	valid, err := ktx2.Encode(src, ktx2.Options{Format: ktx2.FormatBC1})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt magic byte.
	badMagic := append([]byte(nil), valid...)
	badMagic[0] = 0x00
	if err := ktx2.Validate(badMagic); err == nil {
		t.Fatal("expected error for corrupted magic")
	}
	// Truncate payload.
	if err := ktx2.Validate(valid[:len(valid)-4]); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}
