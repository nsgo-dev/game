package ktx2_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"os/exec"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

func TestUASTCEncodeDecodePSNRAndModes(t *testing.T) {
	t.Run("solid color lossless Mode 8", func(t *testing.T) {
		solid := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		want := color.NRGBA{R: 42, G: 187, B: 99, A: 213}
		for y := range 8 {
			for x := range 8 {
				solid.SetNRGBA(x, y, want)
			}
		}
		raw, err := ktx2.Encode(solid, ktx2.Options{Format: ktx2.FormatUASTC, Mipmaps: true})
		if err != nil {
			t.Fatalf("Encode UASTC: %v", err)
		}
		tex, err := ktx2.Decode(raw)
		if err != nil {
			t.Fatalf("Decode UASTC: %v", err)
		}
		if tex.Header.VkFormat != ktx2.VkFormatUndefined {
			t.Fatalf("VkFormat = %d, want 0 (Undefined)", tex.Header.VkFormat)
		}
		if tex.DFD.ColorModel != ktx2.DFDModelUASTC {
			t.Fatalf("DFD ColorModel = %d, want %d", tex.DFD.ColorModel, ktx2.DFDModelUASTC)
		}
		if tex.Format != ktx2.FormatUASTC || !tex.HasAlpha || len(tex.Levels) != 4 {
			t.Fatalf("unexpected decoded UASTC metadata: format=%s alpha=%v levels=%d", tex.Format, tex.HasAlpha, len(tex.Levels))
		}
		dec, err := tex.DecodeLevelRGBA(0)
		if err != nil {
			t.Fatalf("DecodeLevelRGBA: %v", err)
		}
		for y := range 8 {
			for x := range 8 {
				if got := dec.NRGBAAt(x, y); got != want {
					t.Fatalf("pixel (%d,%d) = %v, want exact %v", x, y, got, want)
				}
			}
		}
	})

	t.Run("opaque gradient high PSNR and RGB DFD channel", func(t *testing.T) {
		opaque := makeGradientImage(32, 32, false)
		raw, err := ktx2.Encode(opaque, ktx2.Options{Format: ktx2.FormatUASTC, Mipmaps: true, SRGB: true})
		if err != nil {
			t.Fatal(err)
		}
		tex, err := ktx2.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !tex.SRGB || tex.HasAlpha {
			t.Fatalf("expected sRGB=true HasAlpha=false, got sRGB=%v HasAlpha=%v", tex.SRGB, tex.HasAlpha)
		}
		dec, err := tex.DecodeLevelRGBA(0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(opaque, dec, false); psnr < 33.0 {
			t.Fatalf("UASTC opaque 2D gradient PSNR = %.2f dB, want >= 33.0 dB", psnr)
		}

		// On a 1D color ramp (collinear per 4x4 block), UASTC Mode 5/18 achieves > 45 dB.
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
		rawRamp, err := ktx2.Encode(ramp, ktx2.Options{Format: ktx2.FormatUASTC})
		if err != nil {
			t.Fatal(err)
		}
		decRamp, err := ktx2.DecodeRGBA(rawRamp, 0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(ramp, decRamp, false); psnr < 45.0 {
			t.Fatalf("UASTC 1D ramp PSNR = %.2f dB, want >= 45.0 dB", psnr)
		}
	})

	t.Run("alpha gradient high PSNR", func(t *testing.T) {
		alphaImg := makeGradientImage(32, 32, true)
		raw, err := ktx2.Encode(alphaImg, ktx2.Options{Format: ktx2.FormatUASTC, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		dec, err := ktx2.DecodeRGBA(raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(alphaImg, dec, true); psnr < 33.5 {
			t.Fatalf("UASTC RGBA 2D gradient PSNR = %.2f dB, want >= 33.5 dB", psnr)
		}
	})
}

func TestBC7ETC2RGBAAndASTC4x4RoundTrip(t *testing.T) {
	opaque := makeGradientImage(32, 32, false)
	withAlpha := makeGradientImage(32, 32, true)

	t.Run("BC7 Mode 6 high PSNR", func(t *testing.T) {
		raw, err := ktx2.Encode(withAlpha, ktx2.Options{Format: ktx2.FormatBC7, Mipmaps: true, SRGB: true})
		if err != nil {
			t.Fatal(err)
		}
		tex, err := ktx2.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if tex.Format != ktx2.FormatBC7 || !tex.SRGB || len(tex.Levels) != 6 {
			t.Fatalf("unexpected BC7 metadata: %+v", tex.Header)
		}
		dec, err := tex.DecodeLevelRGBA(0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(withAlpha, dec, true); psnr < 34.0 {
			t.Fatalf("BC7 RGBA 2D gradient PSNR = %.2f dB, want >= 34.0 dB", psnr)
		}
	})

	t.Run("ETC2 RGBA8 high PSNR", func(t *testing.T) {
		raw, err := ktx2.Encode(withAlpha, ktx2.Options{Format: ktx2.FormatETC2RGBA, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		tex, err := ktx2.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if tex.Format != ktx2.FormatETC2RGBA || !tex.HasAlpha {
			t.Fatalf("unexpected ETC2 RGBA metadata: format=%s alpha=%v", tex.Format, tex.HasAlpha)
		}
		dec, err := tex.DecodeLevelRGBA(0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(withAlpha, dec, true); psnr < 36.0 {
			t.Fatalf("ETC2 RGBA8 gradient PSNR = %.2f dB, want >= 36.0 dB", psnr)
		}
	})

	t.Run("ASTC 4x4 Void-Extent solid and CEM 8/12 gradients", func(t *testing.T) {
		solid := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		want := color.NRGBA{R: 25, G: 210, B: 145, A: 200}
		for y := range 8 {
			for x := range 8 {
				solid.SetNRGBA(x, y, want)
			}
		}
		rawSolid, err := ktx2.Encode(solid, ktx2.Options{Format: ktx2.FormatASTC4x4})
		if err != nil {
			t.Fatal(err)
		}
		decSolid, err := ktx2.DecodeRGBA(rawSolid, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := decSolid.NRGBAAt(3, 5); got != want {
			t.Fatalf("ASTC solid pixel = %v, want %v", got, want)
		}

		rawOpaque, err := ktx2.Encode(opaque, ktx2.Options{Format: ktx2.FormatASTC4x4, Mipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		decOpaque, err := ktx2.DecodeRGBA(rawOpaque, 0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(opaque, decOpaque, false); psnr < 33.0 {
			t.Fatalf("ASTC opaque 2D gradient PSNR = %.2f dB, want >= 33.0 dB", psnr)
		}

		rawAlpha, err := ktx2.Encode(withAlpha, ktx2.Options{Format: ktx2.FormatASTC4x4, SRGB: true})
		if err != nil {
			t.Fatal(err)
		}
		decAlpha, err := ktx2.DecodeRGBA(rawAlpha, 0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(withAlpha, decAlpha, true); psnr < 33.5 {
			t.Fatalf("ASTC alpha 2D gradient PSNR = %.2f dB, want >= 33.5 dB", psnr)
		}
	})
}

func TestTranscodeMatrixAllFormats(t *testing.T) {
	img := makeGradientImage(16, 16, true)
	sources := []ktx2.Format{
		ktx2.FormatRGBA8,
		ktx2.FormatBC1,
		ktx2.FormatBC3,
		ktx2.FormatBC7,
		ktx2.FormatETC2,
		ktx2.FormatETC2RGBA,
		ktx2.FormatASTC4x4,
		ktx2.FormatUASTC,
	}
	targets := []ktx2.Format{
		ktx2.FormatRGBA8,
		ktx2.FormatBC1,
		ktx2.FormatBC3,
		ktx2.FormatBC7,
		ktx2.FormatETC2,
		ktx2.FormatETC2RGBA,
		ktx2.FormatASTC4x4,
	}

	for _, srcFmt := range sources {
		raw, err := ktx2.Encode(img, ktx2.Options{Format: srcFmt, Mipmaps: true})
		if err != nil {
			t.Fatalf("Encode(%s): %v", srcFmt, err)
		}
		tex, err := ktx2.Decode(raw)
		if err != nil {
			t.Fatalf("Decode(%s): %v", srcFmt, err)
		}
		for _, dstFmt := range targets {
			levels, err := tex.Transcode(dstFmt)
			if err != nil {
				t.Fatalf("Transcode(%s -> %s): %v", srcFmt, dstFmt, err)
			}
			if len(levels) != 5 {
				t.Fatalf("Transcode(%s -> %s) levels = %d, want 5", srcFmt, dstFmt, len(levels))
			}
			for lvl, lv := range levels {
				wantSize := dstFmt.LevelByteSize(lv.Width, lv.Height)
				if len(lv.Data) != wantSize {
					t.Fatalf("Transcode(%s -> %s) level %d size = %d, want %d", srcFmt, dstFmt, lvl, len(lv.Data), wantSize)
				}
			}
		}
	}
}

func TestSupercompressionZLIBAndZstd(t *testing.T) {
	img := makeGradientImage(32, 32, true)

	t.Run("ZLIB supercompression round-trip", func(t *testing.T) {
		raw, err := ktx2.Encode(img, ktx2.Options{
			Format:           ktx2.FormatUASTC,
			Mipmaps:          true,
			Supercompression: ktx2.SupercompressionZLIB,
		})
		if err != nil {
			t.Fatalf("Encode ZLIB: %v", err)
		}
		tex, err := ktx2.Decode(raw)
		if err != nil {
			t.Fatalf("Decode ZLIB: %v", err)
		}
		if tex.Header.SupercompressionScheme != ktx2.SupercompressionZLIB {
			t.Fatalf("scheme = %d, want ZLIB", tex.Header.SupercompressionScheme)
		}
		dec, err := tex.DecodeLevelRGBA(0)
		if err != nil {
			t.Fatal(err)
		}
		if psnr := computePSNR(img, dec, true); psnr < 33.5 {
			t.Fatalf("ZLIB UASTC PSNR = %.2f dB, want >= 33.5 dB", psnr)
		}
	})

	t.Run("Zstandard supercompression real zstd CLI frames", func(t *testing.T) {
		zstdBin, err := exec.LookPath("zstd")
		if err != nil {
			t.Skip("zstd CLI not installed")
		}
		// Build an uncompressed UASTC KTX2, then re-pack its mip levels with real zstd CLI at levels 1, 3, and 19.
		baseKTX2, err := ktx2.Encode(makeGradientImage(64, 64, true), ktx2.Options{
			Format:  ktx2.FormatUASTC,
			Mipmaps: true,
			SRGB:    true,
		})
		if err != nil {
			t.Fatal(err)
		}
		baseTex, err := ktx2.Decode(baseKTX2)
		if err != nil {
			t.Fatal(err)
		}

		for _, levelFlag := range []string{"-1", "-3", "-19"} {
			t.Run("zstd"+levelFlag, func(t *testing.T) {
				zstdLevels := make([][]byte, len(baseTex.Levels))
				for i, lv := range baseTex.Levels {
					cmd := exec.Command(zstdBin, levelFlag, "-c", "--no-check")
					cmd.Stdin = bytes.NewReader(lv.Data)
					out, err := cmd.Output()
					if err != nil {
						t.Fatalf("zstd %s level %d: %v", levelFlag, i, err)
					}
					zstdLevels[i] = out
				}
				zstdKTX2 := repackKTX2Supercompressed(t, baseKTX2, ktx2.SupercompressionZstandard, zstdLevels)
				decTex, err := ktx2.Decode(zstdKTX2)
				if err != nil {
					t.Fatalf("Decode zstd%s KTX2: %v", levelFlag, err)
				}
				if decTex.Format != ktx2.FormatUASTC || !decTex.SRGB || len(decTex.Levels) != len(baseTex.Levels) {
					t.Fatalf("unexpected decoded zstd KTX2 metadata")
				}
				for i, lv := range decTex.Levels {
					if !bytes.Equal(lv.Data, baseTex.Levels[i].Data) {
						t.Fatalf("zstd%s level %d mismatch (%d vs %d bytes)", levelFlag, i, len(lv.Data), len(baseTex.Levels[i].Data))
					}
				}
			})
		}

		// Also verify zstd with content checksum enabled (default zstd behavior).
		t.Run("zstd-with-checksum", func(t *testing.T) {
			zstdLevels := make([][]byte, len(baseTex.Levels))
			for i, lv := range baseTex.Levels {
				cmd := exec.Command(zstdBin, "-3", "-c")
				cmd.Stdin = bytes.NewReader(lv.Data)
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("zstd checksum level %d: %v", i, err)
				}
				zstdLevels[i] = out
			}
			zstdKTX2 := repackKTX2Supercompressed(t, baseKTX2, ktx2.SupercompressionZstandard, zstdLevels)
			decTex, err := ktx2.Decode(zstdKTX2)
			if err != nil {
				t.Fatalf("Decode zstd checksum KTX2: %v", err)
			}
			for i, lv := range decTex.Levels {
				if !bytes.Equal(lv.Data, baseTex.Levels[i].Data) {
					t.Fatalf("zstd checksum level %d mismatch", i)
				}
			}
		})
	})
}

func TestBasisLZETC1SDescriptorInspection(t *testing.T) {
	// Synthesize a valid KTX2 container with VkFormat=0, DFD colorModel=ETC1S, and BasisLZ supercompression.
	dfd := make([]byte, 4+24+16)
	binary.LittleEndian.PutUint32(dfd[0:4], uint32(len(dfd)))
	binary.LittleEndian.PutUint32(dfd[4:8], 0)
	binary.LittleEndian.PutUint32(dfd[8:12], ktx2.DFDVersionNumber|(uint32(24+16)<<16))
	dfd[12] = ktx2.DFDModelETC1S
	dfd[13] = ktx2.DFDPrimariesBT709
	dfd[14] = ktx2.DFDTransferSRGB
	dfd[16], dfd[17] = 3, 3
	dfd[20] = 8
	dfd[30] = 63
	dfd[31] = ktx2.DFDChannelETC1SRGB

	sgd := []byte("basis-global-data")
	lvlPayload := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	dfdOff := uint32(ktx2.HeaderSize + ktx2.LevelIndexEntrySize)
	sgdOff := uint64(dfdOff) + uint64(len(dfd))
	lvlOff := sgdOff + uint64(len(sgd))
	buf := make([]byte, lvlOff+uint64(len(lvlPayload)))
	copy(buf[0:12], ktx2.Identifier[:])
	binary.LittleEndian.PutUint32(buf[12:16], ktx2.VkFormatUndefined)
	binary.LittleEndian.PutUint32(buf[16:20], 1)
	binary.LittleEndian.PutUint32(buf[20:24], 8)
	binary.LittleEndian.PutUint32(buf[24:28], 8)
	binary.LittleEndian.PutUint32(buf[36:40], 1)
	binary.LittleEndian.PutUint32(buf[40:44], 1)
	binary.LittleEndian.PutUint32(buf[44:48], ktx2.SupercompressionBasisLZ)
	binary.LittleEndian.PutUint32(buf[48:52], dfdOff)
	binary.LittleEndian.PutUint32(buf[52:56], uint32(len(dfd)))
	binary.LittleEndian.PutUint64(buf[64:72], sgdOff)
	binary.LittleEndian.PutUint64(buf[72:80], uint64(len(sgd)))
	binary.LittleEndian.PutUint64(buf[80:88], lvlOff)
	binary.LittleEndian.PutUint64(buf[88:96], uint64(len(lvlPayload)))
	binary.LittleEndian.PutUint64(buf[96:104], 0)
	copy(buf[dfdOff:], dfd)
	copy(buf[sgdOff:], sgd)
	copy(buf[lvlOff:], lvlPayload)

	tex, err := ktx2.Decode(buf)
	if err != nil {
		t.Fatalf("Decode ETC1S container: %v", err)
	}
	if tex.Format != ktx2.FormatETC1S || !tex.SRGB || !bytes.Equal(tex.SGD, sgd) {
		t.Fatalf("unexpected ETC1S metadata: format=%s srgb=%v sgd=%q", tex.Format, tex.SRGB, tex.SGD)
	}
	if _, err := tex.Transcode(ktx2.FormatRGBA8); err == nil {
		t.Fatal("expected clear unsupported error when transcoding ETC1S")
	}
}

func TestMalformedAdversarialKTX2NeverPanics(t *testing.T) {
	base, err := ktx2.Encode(makeGradientImage(8, 8, true), ktx2.Options{Format: ktx2.FormatUASTC, Mipmaps: true})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"zero_width", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[20:24], 0); return b }},
		{"huge_width", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[20:24], 1<<30); return b }},
		{"huge_level_count", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[40:44], 99999); return b }},
		{"depth_3d", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[28:32], 4); return b }},
		{"layer_array", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[32:36], 4); return b }},
		{"cubemap_faces", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[36:40], 6); return b }},
		{"unknown_supercomp", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[44:48], 99); return b }},
		{"overflow_dfd_offset", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[48:52], 0xFFFFFFFF); return b }},
		{"overflow_lvl_offset", func(b []byte) []byte { binary.LittleEndian.PutUint64(b[80:88], ^uint64(0)); return b }},
		{"overflow_lvl_len", func(b []byte) []byte { binary.LittleEndian.PutUint64(b[88:96], ^uint64(0)); return b }},
		{"bogus_zstd_payload", func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[44:48], ktx2.SupercompressionZstandard)
			return b
		}},
		{"bogus_zlib_payload", func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[44:48], ktx2.SupercompressionZLIB)
			return b
		}},
		{"truncated_mid_header", func(b []byte) []byte { return b[:42] }},
		{"truncated_mid_levels", func(b []byte) []byte { return b[:ktx2.HeaderSize+10] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := append([]byte(nil), base...)
			mut := tc.mutate(cp)
			if _, err := ktx2.Decode(mut); err == nil {
				t.Errorf("%s: expected error, got nil", tc.name)
			}
		})
	}
}

func repackKTX2Supercompressed(t *testing.T, base []byte, scheme uint32, compressedLevels [][]byte) []byte {
	t.Helper()
	h, baseLevels, err := ktx2.DecodeHeader(base)
	if err != nil {
		t.Fatal(err)
	}
	dfd := base[h.DFDByteOffset : h.DFDByteOffset+h.DFDByteLength]
	var kvd []byte
	if h.KVDByteLength > 0 {
		kvd = base[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength]
	}

	lvlCount := len(compressedLevels)
	dfdOff := uint64(ktx2.HeaderSize + lvlCount*ktx2.LevelIndexEntrySize)
	kvdOff := dfdOff + uint64(len(dfd))
	if len(kvd) == 0 {
		kvdOff = 0
	}
	cursor := dfdOff + uint64(len(dfd)) + uint64(len(kvd))
	entries := make([]ktx2.LevelIndex, lvlCount)
	for lvl := lvlCount - 1; lvl >= 0; lvl-- {
		n := uint64(len(compressedLevels[lvl]))
		entries[lvl] = ktx2.LevelIndex{
			ByteOffset:             cursor,
			ByteLength:             n,
			UncompressedByteLength: baseLevels[lvl].UncompressedByteLength,
		}
		cursor += n
	}

	out := make([]byte, cursor)
	copy(out[:ktx2.HeaderSize], base[:ktx2.HeaderSize])
	binary.LittleEndian.PutUint32(out[44:48], scheme)
	binary.LittleEndian.PutUint32(out[48:52], uint32(dfdOff))
	binary.LittleEndian.PutUint32(out[52:56], uint32(len(dfd)))
	binary.LittleEndian.PutUint32(out[56:60], uint32(kvdOff))
	binary.LittleEndian.PutUint32(out[60:64], uint32(len(kvd)))
	for i, e := range entries {
		off := ktx2.HeaderSize + i*ktx2.LevelIndexEntrySize
		binary.LittleEndian.PutUint64(out[off+0:off+8], e.ByteOffset)
		binary.LittleEndian.PutUint64(out[off+8:off+16], e.ByteLength)
		binary.LittleEndian.PutUint64(out[off+16:off+24], e.UncompressedByteLength)
	}
	copy(out[dfdOff:], dfd)
	if len(kvd) > 0 {
		copy(out[kvdOff:], kvd)
	}
	for lvl := lvlCount - 1; lvl >= 0; lvl-- {
		copy(out[entries[lvl].ByteOffset:], compressedLevels[lvl])
	}
	return out
}

func FuzzDecodeKTX2(f *testing.F) {
	img := makeGradientImage(8, 8, true)
	for _, fm := range []ktx2.Format{
		ktx2.FormatRGBA8, ktx2.FormatBC1, ktx2.FormatBC3, ktx2.FormatBC7,
		ktx2.FormatETC2, ktx2.FormatETC2RGBA, ktx2.FormatASTC4x4, ktx2.FormatUASTC,
	} {
		if raw, err := ktx2.Encode(img, ktx2.Options{Format: fm, Mipmaps: true}); err == nil {
			f.Add(raw)
		}
	}
	if rawZ, err := ktx2.Encode(img, ktx2.Options{Format: ktx2.FormatUASTC, Supercompression: ktx2.SupercompressionZLIB}); err == nil {
		f.Add(rawZ)
	}
	f.Add([]byte{})
	f.Add(ktx2.Identifier[:])
	f.Add(make([]byte, ktx2.HeaderSize))

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = ktx2.Validate(data)
		tex, err := ktx2.Decode(data)
		if err != nil {
			return
		}
		_, _ = tex.DecodeLevelRGBA(0)
		_, _ = tex.Transcode(ktx2.FormatRGBA8)
	})
}
