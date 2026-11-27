package render3d_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/nsgo-dev/game/pkg/render3d"
)

// issue1552HeaderHex is the exact 52-byte Radiance .hdr header from issue #1552
// (#?RADIANCE / FORMAT=32-bit_rle_rgbe / -Y 4096 +X 16384) with no pixel payload.
const issue1552HeaderHex = "233f52414449414e43450a464f524d41543d33322d6269745f726c655f726762650a0a2d592034303936202b582031363338340a"

// opaqueHDRReader hides Len(), Size(), Stat(), and Seek() so DecodeHDR exercises
// its pure streaming path in addition to the known-size pre-check path.
type opaqueHDRReader struct {
	r io.Reader
}

func (o opaqueHDRReader) Read(p []byte) (int, error) {
	return o.r.Read(p)
}

func measureDecodeHDRAlloc(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	if after.TotalAlloc < before.TotalAlloc {
		return 0
	}
	return after.TotalAlloc - before.TotalAlloc
}

// buildAdaptiveRLEScanline builds one valid adaptive-RLE scanline of solid RGBE pixels.
func buildAdaptiveRLEScanline(width int, rgbe [4]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{2, 2, byte(width >> 8), byte(width & 0xFF)})
	for ch := range 4 {
		rem := width
		for rem > 0 {
			run := min(rem, 127)
			buf.WriteByte(byte(128 + run))
			buf.WriteByte(rgbe[ch])
			rem -= run
		}
	}
	return buf.Bytes()
}

// buildOldRLEScanline builds one valid old-style RLE scanline of solid RGBE pixels.
func buildOldRLEScanline(width int, rgbe [4]byte) []byte {
	var buf bytes.Buffer
	buf.Write(rgbe[:])
	rem := width - 1
	for rem > 0 {
		buf.Write([]byte{1, 1, 1, byte(rem & 0xFF)})
		rem >>= 8
	}
	return buf.Bytes()
}

func TestDecodeHDRNoPreallocationOOM(t *testing.T) {
	raw52, err := hex.DecodeString(issue1552HeaderHex)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	if len(raw52) != 52 {
		t.Fatalf("reproducer length = %d, want 52", len(raw52))
	}

	tmpPath := filepath.Join(t.TempDir(), "truncated.hdr")
	if err := os.WriteFile(tmpPath, raw52, 0o600); err != nil {
		t.Fatalf("write temp hdr: %v", err)
	}

	cases := []struct {
		name     string
		open     func() (io.Reader, func())
		maxAlloc uint64
	}{
		{
			name: "52-byte bytes.Reader",
			open: func() (io.Reader, func()) {
				return bytes.NewReader(raw52), func() {}
			},
			maxAlloc: 1 << 20,
		},
		{
			name: "52-byte os.File",
			open: func() (io.Reader, func()) {
				f, err := os.Open(tmpPath)
				if err != nil {
					t.Fatalf("open temp hdr: %v", err)
				}
				return f, func() { _ = f.Close() }
			},
			maxAlloc: 1 << 20,
		},
		{
			name: "52-byte opaque streaming reader",
			open: func() (io.Reader, func()) {
				return opaqueHDRReader{bytes.NewReader(raw52)}, func() {}
			},
			maxAlloc: 1 << 20,
		},
		{
			name: "52-byte OneByteReader",
			open: func() (io.Reader, func()) {
				return iotest.OneByteReader(bytes.NewReader(raw52)), func() {}
			},
			maxAlloc: 1 << 20,
		},
		{
			name: "52-byte +Y flipped header",
			open: func() (io.Reader, func()) {
				flipped := strings.Replace(string(raw52), "-Y 4096 +X 16384", "+Y 4096 -X 16384", 1)
				return opaqueHDRReader{strings.NewReader(flipped)}, func() {}
			},
			maxAlloc: 1 << 20,
		},
		{
			name: "truncated after 1 adaptive-RLE scanline (bytes.Reader)",
			open: func() (io.Reader, func()) {
				payload := append(slices.Clone(raw52), buildAdaptiveRLEScanline(16384, [4]byte{128, 128, 128, 129})...)
				return bytes.NewReader(payload), func() {}
			},
			maxAlloc: 1 << 20,
		},
		{
			name: "truncated after 1 adaptive-RLE scanline (opaque reader)",
			open: func() (io.Reader, func()) {
				payload := append(slices.Clone(raw52), buildAdaptiveRLEScanline(16384, [4]byte{128, 128, 128, 129})...)
				return opaqueHDRReader{bytes.NewReader(payload)}, func() {}
			},
			maxAlloc: 2 << 20,
		},
		{
			name: "old-style RLE decompression bomb (4096x16384)",
			open: func() (io.Reader, func()) {
				row := buildOldRLEScanline(16384, [4]byte{128, 128, 128, 129})
				var buf bytes.Buffer
				buf.Write(raw52)
				for range 4096 {
					buf.Write(row)
				}
				return bytes.NewReader(buf.Bytes()), func() {}
			},
			maxAlloc: 8 << 20,
		},
		{
			name: "old-style RLE decompression bomb (opaque reader)",
			open: func() (io.Reader, func()) {
				row := buildOldRLEScanline(16384, [4]byte{128, 128, 128, 129})
				var buf bytes.Buffer
				buf.Write(raw52)
				for range 4096 {
					buf.Write(row)
				}
				return opaqueHDRReader{bytes.NewReader(buf.Bytes())}, func() {}
			},
			maxAlloc: 8 << 20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, cleanup := tc.open()
			defer cleanup()
			var gotErr error
			grew := measureDecodeHDRAlloc(func() {
				_, gotErr = render3d.DecodeHDR(r)
			})
			if !errors.Is(gotErr, render3d.ErrInvalidCubemap) {
				t.Fatalf("DecodeHDR err = %v, want ErrInvalidCubemap", gotErr)
			}
			if grew > tc.maxAlloc {
				t.Fatalf("DecodeHDR allocated %d bytes (> limit %d bytes)", grew, tc.maxAlloc)
			}
		})
	}
}

func TestDecodeHDRLimitsAndEdgeCases(t *testing.T) {
	badCases := []struct {
		name string
		data []byte
	}{
		{
			name: "height exceeds maxHDRImageDim",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 16385 +X 1\n\x00\x00\x00\x00"),
		},
		{
			name: "width exceeds maxHDRImageDim",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 16385\n\x00\x00\x00\x00"),
		},
		{
			name: "pixel count exceeds maxHDRImagePixels",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 16384 +X 16384\n\x00\x00\x00\x00"),
		},
		{
			name: "height integer overflow",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 99999999999999999999999 +X 1\n"),
		},
		{
			name: "width integer overflow",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 99999999999999999999999\n"),
		},
		{
			name: "zero height",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 0 +X 1\n"),
		},
		{
			name: "negative width",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X -4\n"),
		},
		{
			name: "unterminated oversized header line",
			data: append([]byte("#?RADIANCE\n#"), bytes.Repeat([]byte("a"), 10000)...),
		},
		{
			name: "oversized header comment block",
			data: append([]byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n"), bytes.Repeat([]byte("# comment line padding\n"), 4000)...),
		},
		{
			name: "exposure underflows to zero",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\nEXPOSURE=1e-308\nEXPOSURE=1e-308\n\n-Y 1 +X 1\n\x80\x80\x80\x80"),
		},
		{
			name: "exposure overflows to infinity",
			data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\nEXPOSURE=1e308\nEXPOSURE=1e308\n\n-Y 1 +X 1\n\x80\x80\x80\x80"),
		},
		{
			name: "old-style RLE shift overflow",
			data: append(
				[]byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 20\n"),
				128, 128, 128, 128,
				1, 1, 1, 1,
				1, 1, 1, 1,
				1, 1, 1, 1,
				1, 1, 1, 1,
			),
		},
		{
			name: "adaptive RLE scanline width mismatch",
			data: append(
				[]byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 8\n"),
				2, 2, 0, 9,
				136, 0, 136, 0, 136, 0, 136, 0,
			),
		},
		{
			name: "adaptive RLE zero literal count",
			data: append(
				[]byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 8\n"),
				2, 2, 0, 8,
				0, 0, 0, 0, 0, 0, 0, 0,
			),
		},
		{
			name: "adaptive RLE run overflows width",
			data: append(
				[]byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 8\n"),
				2, 2, 0, 8,
				137, 0, 136, 0, 136, 0, 136, 0,
			),
		},
	}

	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := render3d.DecodeHDR(bytes.NewReader(tc.data)); !errors.Is(err, render3d.ErrInvalidCubemap) {
				t.Errorf("bytes.Reader err = %v, want ErrInvalidCubemap", err)
			}
			if _, err := render3d.DecodeHDR(opaqueHDRReader{bytes.NewReader(tc.data)}); !errors.Is(err, render3d.ErrInvalidCubemap) {
				t.Errorf("opaque reader err = %v, want ErrInvalidCubemap", err)
			}
		})
	}
}

func TestDecodeHDROrientationsEncodingsAndStreamingParity(t *testing.T) {
	// 1. All 4 orientations (-Y/+Y x +X/-X) with adaptive RLE (width=12, height=5),
	//    verifying exact pixel placement and identical output across sized and streaming readers.
	const w, h = 12, 5
	orientations := []struct {
		yTag, xTag   string
		flipY, flipX bool
	}{
		{"-Y", "+X", false, false},
		{"+Y", "+X", true, false},
		{"-Y", "-X", false, true},
		{"+Y", "-X", true, true},
	}
	for _, o := range orientations {
		t.Run(o.yTag+"_"+o.xTag, func(t *testing.T) {
			var buf bytes.Buffer
			buf.WriteString("#?RADIANCE\r\n# CRLF comment\r\nEXPOSURE=0.5\r\nEXPOSURE=4.0\r\nFORMAT=32-bit_rle_rgbe\r\n\r\n")
			buf.WriteString(o.yTag + " 5 " + o.xTag + " 12\r\n")
			// Write 5 distinct rows: row y has pixel (x) with R=(x+1)*16, G=(y+1)*32, B=64, E=130.
			for row := range h {
				buf.Write([]byte{2, 2, 0, w})
				var planes [4][w]byte
				for col := range w {
					planes[0][col] = byte((col + 1) * 16)
					planes[1][col] = byte((row + 1) * 32)
					planes[2][col] = 64
					planes[3][col] = 130
				}
				for ch := range 4 {
					buf.WriteByte(byte(w)) // literal run of w bytes
					buf.Write(planes[ch][:])
				}
			}

			raw := buf.Bytes()
			imgSized, err := render3d.DecodeHDR(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("DecodeHDR sized: %v", err)
			}
			imgStream, err := render3d.DecodeHDR(opaqueHDRReader{bytes.NewReader(raw)})
			if err != nil {
				t.Fatalf("DecodeHDR stream: %v", err)
			}
			imgOneByte, err := render3d.DecodeHDR(iotest.OneByteReader(bytes.NewReader(raw)))
			if err != nil {
				t.Fatalf("DecodeHDR OneByteReader: %v", err)
			}
			if err := imgSized.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !slices.Equal(imgSized.RGB, imgStream.RGB) || !slices.Equal(imgSized.RGB, imgOneByte.RGB) {
				t.Fatal("sized, opaque streaming, and OneByteReader outputs differ")
			}

			// Exposure product = 0.5 * 4.0 = 2.0 => invExposure = 0.5.
			// Scale = 2^(130-136) * 0.5 = 2^-7 = 1/128.
			scale := math.Ldexp(1.0, 130-(128+8)) * 0.5
			for row := range h {
				dstY := row
				if o.flipY {
					dstY = h - 1 - row
				}
				for col := range w {
					dstX := col
					if o.flipX {
						dstX = w - 1 - col
					}
					want := [3]float32{
						float32(float64((col+1)*16) * scale),
						float32(float64((row+1)*32) * scale),
						float32(64.0 * scale),
					}
					if got := imgSized.At(dstX, dstY); got != want {
						t.Fatalf("pixel (%d,%d) = %v, want %v", dstX, dstY, got, want)
					}
				}
			}
		})
	}

	// 2. Old-style RLE with width > 256 (exercises multi-packet rleShift = 0, 8) and +Y flip.
	t.Run("old_style_rle_wide", func(t *testing.T) {
		const width, height = 300, 3
		var buf bytes.Buffer
		buf.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n+Y 3 +X 300\n")
		rowRGBE := [height][4]byte{
			{128, 64, 32, 130}, // stream row 0 -> dstY = 2
			{64, 128, 32, 130}, // stream row 1 -> dstY = 1
			{32, 64, 128, 130}, // stream row 2 -> dstY = 0
		}
		for row := range height {
			buf.Write(buildOldRLEScanline(width, rowRGBE[row]))
		}
		raw := buf.Bytes()
		img, err := render3d.DecodeHDR(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("DecodeHDR old-style RLE: %v", err)
		}
		if err := img.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		imgStream, err := render3d.DecodeHDR(opaqueHDRReader{bytes.NewReader(raw)})
		if err != nil {
			t.Fatalf("DecodeHDR old-style RLE stream: %v", err)
		}
		if !slices.Equal(img.RGB, imgStream.RGB) {
			t.Fatal("old-style RLE sized vs streaming mismatch")
		}
		// dstY=0 came from stream row 2: (32, 64, 128, 130) => (0.5, 1.0, 2.0).
		if got := img.At(299, 0); got != ([3]float32{0.5, 1.0, 2.0}) {
			t.Fatalf("At(299,0) = %v, want [0.5 1 2]", got)
		}
		// dstY=2 came from stream row 0: (128, 64, 32, 130) => (2.0, 1.0, 0.5).
		if got := img.At(0, 2); got != ([3]float32{2.0, 1.0, 0.5}) {
			t.Fatalf("At(0,2) = %v, want [2 1 0.5]", got)
		}
	})

	// 3. Extreme small EXPOSURE clamps overflowed float32 radiance to MaxFloat32 so Validate stays true.
	t.Run("extreme_exposure_stays_finite", func(t *testing.T) {
		raw := []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\nEXPOSURE=1e-100\n\n-Y 1 +X 1\n\xff\xff\xff\xff")
		img, err := render3d.DecodeHDR(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("DecodeHDR extreme exposure: %v", err)
		}
		if err := img.Validate(); err != nil {
			t.Fatalf("Validate failed on decoded image: %v", err)
		}
		if got := img.At(0, 0); got != ([3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}) {
			t.Fatalf("At(0,0) = %v, want MaxFloat32", got)
		}
	})
}

func FuzzDecodeHDR(f *testing.F) {
	// Seed 1: exact 52-byte OOM reproducer from #1552.
	if raw52, err := hex.DecodeString(issue1552HeaderHex); err == nil {
		f.Add(raw52)
	}
	// Seed 2: empty and minimal malformed inputs.
	f.Add([]byte{})
	f.Add([]byte("#?RADIANCE\n"))
	// Seed 3: valid adaptive RLE image encoded by EncodeHDR.
	if img, err := render3d.NewHDRImage(16, 4); err == nil {
		for y := range img.Height {
			for x := range img.Width {
				img.Set(x, y, [3]float32{float32(x) * 0.5, float32(y) * 2.0, 1.25})
			}
		}
		var buf bytes.Buffer
		if err := render3d.EncodeHDR(&buf, img); err == nil {
			f.Add(buf.Bytes())
		}
	}
	// Seed 4: valid uncompressed image (width < 8).
	if small, err := render3d.NewHDRImage(4, 2); err == nil {
		small.Set(0, 0, [3]float32{4.0, 2.0, 1.0})
		var buf bytes.Buffer
		if err := render3d.EncodeHDR(&buf, small); err == nil {
			f.Add(buf.Bytes())
		}
	}
	// Seed 5: valid old-style RLE image with +Y -X orientation and EXPOSURE.
	{
		var buf bytes.Buffer
		buf.WriteString("#?RADIANCE\nEXPOSURE=2.0\nFORMAT=32-bit_rle_rgbe\n\n+Y 2 -X 16\n")
		buf.Write(buildOldRLEScanline(16, [4]byte{128, 64, 32, 130}))
		buf.Write(buildOldRLEScanline(16, [4]byte{64, 128, 32, 131}))
		f.Add(buf.Bytes())
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var imgSized *render3d.HDRImage
		var errSized error
		grew := measureDecodeHDRAlloc(func() {
			imgSized, errSized = render3d.DecodeHDR(bytes.NewReader(data))
		})
		// Allocation bound: even worst-case valid adaptive RLE (190.5x) with slice doubling
		// stays strictly within 1024x the input plus an 8 MiB floor.
		maxAlloc := uint64(len(data))*1024 + 8<<20
		if grew > maxAlloc {
			t.Fatalf("DecodeHDR allocated %d bytes for %d-byte input (limit %d)", grew, len(data), maxAlloc)
		}

		imgStream, errStream := render3d.DecodeHDR(opaqueHDRReader{bytes.NewReader(data)})
		if (errSized == nil) != (errStream == nil) {
			t.Fatalf("sized err = %v, stream err = %v (must agree)", errSized, errStream)
		}
		if errSized != nil {
			if !errors.Is(errSized, render3d.ErrInvalidCubemap) {
				t.Fatalf("sized err %v does not wrap ErrInvalidCubemap", errSized)
			}
			if !errors.Is(errStream, render3d.ErrInvalidCubemap) {
				t.Fatalf("stream err %v does not wrap ErrInvalidCubemap", errStream)
			}
			return
		}
		if err := imgSized.Validate(); err != nil {
			t.Fatalf("decoded image failed Validate: %v", err)
		}
		if imgSized.Width != imgStream.Width || imgSized.Height != imgStream.Height || !slices.Equal(imgSized.RGB, imgStream.RGB) {
			t.Fatalf("sized (%dx%d) and stream (%dx%d) decoded images differ", imgSized.Width, imgSized.Height, imgStream.Width, imgStream.Height)
		}
	})
}
