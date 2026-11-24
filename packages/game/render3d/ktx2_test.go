package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// encodeKTX2 encodes img as game assets does, with filter under the filter key unless it is empty.
func encodeKTX2(t *testing.T, img image.Image, format ktx2.Format, mipmaps, srgb bool, filter string) []byte {
	t.Helper()
	opts := ktx2.Options{Format: format, Mipmaps: mipmaps, SRGB: srgb}
	if filter != "" {
		opts.KVD = map[string]string{importsettings.FilterKey: filter}
	}
	data, err := ktx2.Encode(img, opts)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestKTX2FilterKeyIsTheImportSettingsKey(t *testing.T) {
	if render3d.KTX2FilterKey != importsettings.FilterKey {
		t.Fatalf("KTX2FilterKey = %q, importsettings.FilterKey = %q", render3d.KTX2FilterKey, importsettings.FilterKey)
	}
}

func TestKTX2Sampler(t *testing.T) {
	img := fourColorImage()
	base := render3d.SamplerLinearRepeat()
	base.MaxAnisotropy = 8
	base.Label = "kept"

	got, err := render3d.KTX2Sampler(encodeKTX2(t, img, ktx2.FormatRGBA8, false, false, "nearest"), base)
	if err != nil {
		t.Fatal(err)
	}
	want := render3d.SamplerNearestRepeat()
	want.Label = "kept"
	if got != want {
		t.Fatalf("nearest: %+v, want %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("nearest sampler is invalid: %v", err)
	}

	got, err = render3d.KTX2Sampler(encodeKTX2(t, img, ktx2.FormatRGBA8, false, false, "linear"), render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatal(err)
	}
	if got != render3d.SamplerLinearClamp() {
		t.Fatalf("linear: %+v, want %+v", got, render3d.SamplerLinearClamp())
	}

	got, err = render3d.KTX2Sampler(encodeKTX2(t, img, ktx2.FormatRGBA8, false, false, ""), base)
	if err != nil || got != base {
		t.Fatalf("no filter: %+v, %v; want base unchanged", got, err)
	}

	for name, data := range map[string][]byte{
		"unknown filter": encodeKTX2(t, img, ktx2.FormatRGBA8, false, false, "cubic"),
		"not KTX2":       []byte("not a texture at all, but long enough to hold a header of eighty bytes or so ......"),
		"truncated":      encodeKTX2(t, img, ktx2.FormatRGBA8, false, false, "nearest")[:40],
	} {
		if _, err := render3d.KTX2Sampler(data, base); !errors.Is(err, render3d.ErrInvalidTexture) {
			t.Errorf("%s: error %v, want ErrInvalidTexture", name, err)
		}
	}
}

// drawKTX2 loads data with LoadKTX2 on dev and draws it over the whole size x size target, sampled through the sampler LoadKTX2 returns.
func drawKTX2(t *testing.T, sr *gpu.SoftwareRenderer, data []byte) (*render3d.GPUTexture, *image.RGBA) {
	t.Helper()
	dev := sr.Device()
	layout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatal(err)
	}
	pipe := clipTexturedPipeline(t, nil, dev, layout, "fs_main", gpu.FormatUndefined)
	tex, opts, err := render3d.LoadKTX2(nil, dev, data, render3d.KTX2Upload{Label: "ktx2", Sampler: render3d.SamplerLinearClamp()})
	if err != nil {
		t.Fatalf("LoadKTX2: %v", err)
	}
	samp, err := render3d.NewGPUSampler(nil, dev, opts)
	if err != nil {
		t.Fatal(err)
	}
	bg, err := render3d.BindTexture(nil, dev, layout, tex, samp)
	if err != nil {
		t.Fatal(err)
	}
	gm, err := render3d.UploadMesh(nil, dev, texturedQuadMesh(-1, -1, 1, 1, 0, 0, 1, 1), render3d.MeshUpload{})
	if err != nil {
		t.Fatal(err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}}})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, bg)
	gm.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return tex, sr.Image()
}

// TestLoadKTX2DrawsWithItsFilter holds what an image's filter setting is for: the same texture draws sharp with nearest and blended with linear.
func TestLoadKTX2DrawsWithItsFilter(t *testing.T) {
	const size = 32
	for _, tc := range []struct {
		filter string
		sharp  bool
	}{{"nearest", true}, {"linear", false}} {
		t.Run(tc.filter, func(t *testing.T) {
			sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, encodeKTX2(t, fourColorImage(), ktx2.FormatRGBA8, false, false, tc.filter))
			if tex.Format() != gpu.FormatRGBA8Unorm || tex.Width() != 2 || tex.MipLevels() != 1 {
				t.Fatalf("texture = %s %dx%d, %d levels", tex.Format(), tex.Width(), tex.Height(), tex.MipLevels())
			}
			// Just off the center, nearest picks one texel and linear blends all four.
			c := img.RGBAAt(size/2-2, size/2-2)
			pure := func(v uint8) bool { return v < 8 || v > 247 }
			if sharp := pure(c.R) && pure(c.G) && pure(c.B); sharp != tc.sharp {
				t.Fatalf("pixel near the center = %v; sharp %v, want %v", c, sharp, tc.sharp)
			}
		})
	}
}

func TestLoadKTX2CompressedNativeOrDecoded(t *testing.T) {
	const size = 16
	solid := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range solid.Pix {
		solid.Pix[i] = []uint8{40, 160, 220, 255}[i%4]
	}
	noCompression := func() *gpu.SoftwareRenderer {
		f := gpu.NewSoftwareRenderer(1, 1, gpu.FormatUndefined)
		features := f.Device().Features()
		f.Close()
		features.TextureCompressionBC, features.TextureCompressionETC2, features.TextureCompressionASTC = false, false, false
		return gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, features)
	}
	for _, tc := range []struct {
		name   string
		format ktx2.Format
		srgb   bool
		sr     func() *gpu.SoftwareRenderer
		want   gpu.TextureFormat
		color  color.RGBA
	}{
		{"bc1 native", ktx2.FormatBC1, false, func() *gpu.SoftwareRenderer { return gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined) }, gpu.FormatBC1RGBAUnorm, color.RGBA{R: 40, G: 160, B: 220, A: 255}},
		{"bc3 native srgb", ktx2.FormatBC3, true, func() *gpu.SoftwareRenderer { return gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined) }, gpu.FormatBC3RGBAUnormSRGB, color.RGBA{R: 5, G: 91, B: 181, A: 255}},
		{"etc2 native", ktx2.FormatETC2, false, func() *gpu.SoftwareRenderer { return gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined) }, gpu.FormatETC2RGB8Unorm, color.RGBA{R: 40, G: 160, B: 220, A: 255}},
		{"etc2 srgb decoded", ktx2.FormatETC2, true, func() *gpu.SoftwareRenderer { return gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined) }, gpu.FormatRGBA8UnormSRGB, color.RGBA{R: 5, G: 91, B: 181, A: 255}},
		{"bc1 decoded", ktx2.FormatBC1, false, noCompression, gpu.FormatRGBA8Unorm, color.RGBA{R: 40, G: 160, B: 220, A: 255}},
		{"etc2 decoded", ktx2.FormatETC2, false, noCompression, gpu.FormatRGBA8Unorm, color.RGBA{R: 40, G: 160, B: 220, A: 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := tc.sr()
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, encodeKTX2(t, solid, tc.format, true, tc.srgb, "nearest"))
			if tex.Format() != tc.want || tex.MipLevels() != 4 || tex.SRGB() != tc.srgb {
				t.Fatalf("texture = %s with %d levels, sRGB %v; want %s with 4 levels", tex.Format(), tex.MipLevels(), tex.SRGB(), tc.want)
			}
			// Block compression is lossy, so the color is compared within a few steps.
			got := img.RGBAAt(size/2, size/2)
			near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 10 }
			if !near(got.R, tc.color.R) || !near(got.G, tc.color.G) || !near(got.B, tc.color.B) {
				t.Fatalf("center = %v, want about %v", got, tc.color)
			}
		})
	}
}

func TestLoadKTX2Refuses(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(4, 4, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	good := encodeKTX2(t, fourColorImage(), ktx2.FormatRGBA8, false, false, "")
	// BC1 without alpha samples its punch-through texels as opaque black, which an RGBA BC1 upload would draw transparent, so it is refused.
	bc1RGB := encodeKTX2(t, fourColorImage(), ktx2.FormatBC1, false, false, "")
	binary.LittleEndian.PutUint32(bc1RGB[12:16], 131)
	for name, data := range map[string][]byte{
		"bc1 rgb":   bc1RGB,
		"empty":     nil,
		"truncated": good[:len(good)-2],
		"filter":    encodeKTX2(t, fourColorImage(), ktx2.FormatRGBA8, false, false, "smooth"),
	} {
		if _, _, err := render3d.LoadKTX2(nil, sr.Device(), data, render3d.KTX2Upload{}); !errors.Is(err, render3d.ErrInvalidTexture) {
			t.Errorf("%s: error %v, want ErrInvalidTexture", name, err)
		}
	}
	if _, _, err := render3d.LoadKTX2(nil, nil, good, render3d.KTX2Upload{}); !errors.As(err, new(gpu.Error)) {
		t.Errorf("nil device: error %v, want a gpu.Error", err)
	}
}

// quadrantImage is a size x size image with a different solid color in each quadrant, so every 4x4 block of it is one color and every block codec keeps the colors within a step or two.
func quadrantImage(size int, alpha uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	colors := [4]color.NRGBA{{R: 230, G: 30, B: 30, A: alpha}, {R: 30, G: 200, B: 40, A: alpha}, {R: 40, G: 60, B: 220, A: alpha}, {R: 240, G: 220, B: 30, A: alpha}}
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, colors[(y/(size/2))*2+x/(size/2)])
		}
	}
	return img
}

// features returns the software renderer's default features with only the named block compression families on.
func features(bc, etc2, astc bool) gpu.Features {
	sr := gpu.NewSoftwareRenderer(1, 1, gpu.FormatUndefined)
	f := sr.Device().Features()
	sr.Close()
	f.TextureCompressionBC, f.TextureCompressionETC2, f.TextureCompressionASTC = bc, etc2, astc
	return f
}

// assertSameQuadrants checks that got shows the four quadrant colors of want, within tol per channel, at each quadrant's center.
func assertSameQuadrants(t *testing.T, got, want *image.RGBA, tol uint8) {
	t.Helper()
	b := want.Bounds()
	for _, p := range []image.Point{{b.Dx() / 4, b.Dy() / 4}, {3 * b.Dx() / 4, b.Dy() / 4}, {b.Dx() / 4, 3 * b.Dy() / 4}, {3 * b.Dx() / 4, 3 * b.Dy() / 4}} {
		g, w := got.RGBAAt(p.X, p.Y), want.RGBAAt(p.X, p.Y)
		near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= tol }
		if !near(g.R, w.R) || !near(g.G, w.G) || !near(g.B, w.B) || !near(g.A, w.A) {
			t.Errorf("pixel %v = %v, want about %v", p, g, w)
		}
	}
}

// TestLoadKTX2TranscodesUASTC holds what UASTC is for: one file draws on a BC, an ETC2, an ASTC and an uncompressed device, transcoded to the best block format each supports.
func TestLoadKTX2TranscodesUASTC(t *testing.T) {
	const size = 32
	ref := func(srgb bool) *image.RGBA {
		sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
		t.Cleanup(sr.Close)
		_, img := drawKTX2(t, sr, encodeKTX2(t, quadrantImage(16, 255), ktx2.FormatRGBA8, false, srgb, "nearest"))
		return img
	}
	linear, srgb := ref(false), ref(true)
	for _, tc := range []struct {
		name     string
		features gpu.Features
		srgb     bool
		alpha    uint8
		want     gpu.TextureFormat
	}{
		{"bc opaque", features(true, false, false), false, 255, gpu.FormatBC7RGBAUnorm},
		{"bc srgb", features(true, false, false), true, 255, gpu.FormatBC7RGBAUnormSRGB},
		{"etc2 opaque", features(false, true, false), false, 255, gpu.FormatETC2RGB8Unorm},
		{"etc2 alpha", features(false, true, false), false, 200, gpu.FormatETC2RGBA8Unorm},
		{"etc2 srgb decoded", features(false, true, false), true, 255, gpu.FormatRGBA8UnormSRGB},
		{"astc opaque", features(false, false, true), false, 255, gpu.FormatASTC4x4Unorm},
		{"astc srgb", features(false, false, true), true, 255, gpu.FormatASTC4x4UnormSRGB},
		{"none decoded", features(false, false, false), false, 255, gpu.FormatRGBA8Unorm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, tc.features)
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, encodeKTX2(t, quadrantImage(16, tc.alpha), ktx2.FormatUASTC, true, tc.srgb, "nearest"))
			if tex.Format() != tc.want || tex.MipLevels() != 5 {
				t.Fatalf("texture = %s with %d levels, want %s with 5", tex.Format(), tex.MipLevels(), tc.want)
			}
			if tc.alpha == 255 {
				want := linear
				if tc.srgb {
					want = srgb
				}
				assertSameQuadrants(t, img, want, 4)
			}
		})
	}
}

func TestLoadKTX2BlockFormatsNativeOrDecoded(t *testing.T) {
	const size = 32
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	_, want := drawKTX2(t, sr, encodeKTX2(t, quadrantImage(16, 255), ktx2.FormatRGBA8, false, false, "nearest"))
	for _, tc := range []struct {
		format ktx2.Format
		native gpu.TextureFormat
		f      gpu.Features
	}{
		{ktx2.FormatBC7, gpu.FormatBC7RGBAUnorm, features(true, false, false)},
		{ktx2.FormatETC2RGBA, gpu.FormatETC2RGBA8Unorm, features(false, true, false)},
		{ktx2.FormatASTC4x4, gpu.FormatASTC4x4Unorm, features(false, false, true)},
	} {
		data := encodeKTX2(t, quadrantImage(16, 255), tc.format, true, false, "nearest")
		t.Run(string(tc.format)+" native", func(t *testing.T) {
			sr := gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, tc.f)
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, data)
			if tex.Format() != tc.native {
				t.Fatalf("texture = %s, want %s", tex.Format(), tc.native)
			}
			assertSameQuadrants(t, img, want, 4)
		})
		t.Run(string(tc.format)+" decoded", func(t *testing.T) {
			sr := gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, features(false, false, false))
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, data)
			if tex.Format() != gpu.FormatRGBA8Unorm {
				t.Fatalf("texture = %s, want RGBA8", tex.Format())
			}
			assertSameQuadrants(t, img, want, 4)
		})
	}
}

func TestLoadKTX2ForceRGBA8OddSizeAndZLIB(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(4, 4, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	uastc := encodeKTX2(t, quadrantImage(16, 255), ktx2.FormatUASTC, true, false, "")
	if tex, _, err := render3d.LoadKTX2(nil, dev, uastc, render3d.KTX2Upload{ForceRGBA8: true}); err != nil || tex.Format() != gpu.FormatRGBA8Unorm {
		t.Fatalf("ForceRGBA8: texture %v, error %v; want RGBA8", tex.Format(), err)
	}
	odd := encodeKTX2(t, image.NewNRGBA(image.Rect(0, 0, 13, 7)), ktx2.FormatUASTC, true, false, "")
	if tex, _, err := render3d.LoadKTX2(nil, dev, odd, render3d.KTX2Upload{}); err != nil || tex.Format() != gpu.FormatRGBA8Unorm || tex.MipLevels() != 4 {
		t.Fatalf("13x7 UASTC: texture %v, error %v; want RGBA8 with 4 levels", tex, err)
	}
	zlib, err := ktx2.Encode(quadrantImage(16, 255), ktx2.Options{Format: ktx2.FormatUASTC, Mipmaps: true, Supercompression: ktx2.SupercompressionZLIB})
	if err != nil {
		t.Fatal(err)
	}
	if tex, _, err := render3d.LoadKTX2(nil, dev, zlib, render3d.KTX2Upload{}); err != nil || tex.Format() != gpu.FormatBC7RGBAUnorm || tex.MipLevels() != 5 {
		t.Fatalf("ZLIB UASTC: texture %v, error %v; want BC7 with 5 levels", tex, err)
	}
}

func readRefFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "tooling", "ktx2", "testdata", "ref", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestLoadKTX2TranscodesETC1S verifies that a real basisu BasisLZ/ETC1S KTX2 file loads and draws
// on BC, ETC2, ASTC, and uncompressed devices, including sRGB and alpha-slice ETC1S textures.
func TestLoadKTX2TranscodesETC1S(t *testing.T) {
	const size = 32
	srRef := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
	t.Cleanup(srRef.Close)
	_, wantQuad := drawKTX2(t, srRef, encodeKTX2(t, quadrantImage(16, 255), ktx2.FormatRGBA8, false, false, "nearest"))

	quadETC1S := readRefFixture(t, "etc1s_quad_linear.ktx2")
	for _, tc := range []struct {
		name     string
		features gpu.Features
		want     gpu.TextureFormat
	}{
		{"bc", features(true, false, false), gpu.FormatBC7RGBAUnorm},
		{"etc2", features(false, true, false), gpu.FormatETC2RGB8Unorm},
		{"astc", features(false, false, true), gpu.FormatASTC4x4Unorm},
		{"uncompressed", features(false, false, false), gpu.FormatRGBA8Unorm},
	} {
		t.Run("quad_"+tc.name, func(t *testing.T) {
			sr := gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, tc.features)
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, quadETC1S)
			if tex.Format() != tc.want || tex.MipLevels() != 5 {
				t.Fatalf("texture = %s with %d levels, want %s with 5", tex.Format(), tex.MipLevels(), tc.want)
			}
			assertSameQuadrants(t, img, wantQuad, 6)
		})
	}

	// Also verify basisu sRGB opaque and sRGB alpha-slice ETC1S textures across all profiles.
	for _, tc := range []struct {
		name     string
		fixture  string
		features gpu.Features
		want     gpu.TextureFormat
	}{
		{"opaque_srgb_bc", "etc1s_opaque_srgb.ktx2", features(true, false, false), gpu.FormatBC7RGBAUnormSRGB},
		{"opaque_srgb_etc2_fallback", "etc1s_opaque_srgb.ktx2", features(false, true, false), gpu.FormatRGBA8UnormSRGB},
		{"opaque_srgb_astc", "etc1s_opaque_srgb.ktx2", features(false, false, true), gpu.FormatASTC4x4UnormSRGB},
		{"opaque_srgb_uncompressed", "etc1s_opaque_srgb.ktx2", features(false, false, false), gpu.FormatRGBA8UnormSRGB},
		{"alpha_srgb_bc", "etc1s_alpha_srgb.ktx2", features(true, false, false), gpu.FormatBC7RGBAUnormSRGB},
		{"alpha_srgb_astc", "etc1s_alpha_srgb.ktx2", features(false, false, true), gpu.FormatASTC4x4UnormSRGB},
		{"alpha_srgb_uncompressed", "etc1s_alpha_srgb.ktx2", features(false, false, false), gpu.FormatRGBA8UnormSRGB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, tc.features)
			t.Cleanup(sr.Close)
			tex, _, err := render3d.LoadKTX2(nil, sr.Device(), readRefFixture(t, tc.fixture), render3d.KTX2Upload{Label: tc.name})
			if err != nil {
				t.Fatalf("LoadKTX2(%s): %v", tc.fixture, err)
			}
			if tex.Format() != tc.want || tex.Width() != 32 || tex.Height() != 32 || tex.MipLevels() != 6 {
				t.Fatalf("texture = %s %dx%d %d mips, want %s 32x32 6 mips", tex.Format(), tex.Width(), tex.Height(), tex.MipLevels(), tc.want)
			}
		})
	}
}

// TestLoadKTX2AstcencOnNonASTCDevice verifies that multi-partition, dual-plane astcenc ASTC 4x4 KTX2 files
// decode on the CPU and load cleanly on a device without ASTC support.
func TestLoadKTX2AstcencOnNonASTCDevice(t *testing.T) {
	const size = 32
	for _, tc := range []struct {
		name    string
		fixture string
		want    gpu.TextureFormat
		srgb    bool
	}{
		{"opaque_linear", "astc4x4_opaque_linear.ktx2", gpu.FormatRGBA8Unorm, false},
		{"alpha_srgb", "astc4x4_alpha_srgb.ktx2", gpu.FormatRGBA8UnormSRGB, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := gpu.NewSoftwareRendererWithFeatures(size, size, gpu.FormatUndefined, features(false, false, false))
			t.Cleanup(sr.Close)
			tex, img := drawKTX2(t, sr, readRefFixture(t, tc.fixture))
			if tex.Format() != tc.want || tex.SRGB() != tc.srgb || tex.Width() != 32 || tex.Height() != 32 {
				t.Fatalf("texture = %s sRGB=%v %dx%d, want %s sRGB=%v 32x32", tex.Format(), tex.SRGB(), tex.Width(), tex.Height(), tc.want, tc.srgb)
			}
			// Top-left 8x8 of astc4x4_opaque_linear is solid {42, 187, 99, 255}; verify it actually rendered.
			if !tc.srgb {
				got := img.RGBAAt(4, 4)
				near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 2 }
				if !near(got.R, 42) || !near(got.G, 187) || !near(got.B, 99) {
					t.Fatalf("top-left pixel = %v, want about {42 187 99 255}", got)
				}
			}
		})
	}
}

func FuzzLoadKTX2(f *testing.F) {
	for _, format := range []ktx2.Format{ktx2.FormatUASTC, ktx2.FormatBC7, ktx2.FormatASTC4x4} {
		data, err := ktx2.Encode(quadrantImage(8, 255), ktx2.Options{Format: format, Mipmaps: true})
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, name := range []string{"etc1s_quad_linear.ktx2", "astc4x4_opaque_linear.ktx2"} {
		if raw, err := os.ReadFile(filepath.Join("..", "tooling", "ktx2", "testdata", "ref", name)); err == nil {
			f.Add(raw)
		}
	}
	f.Add([]byte{})
	f.Add(ktx2.Identifier[:])
	sr := gpu.NewSoftwareRendererWithFeatures(4, 4, gpu.FormatUndefined, features(false, false, false))
	f.Cleanup(sr.Close)
	f.Fuzz(func(t *testing.T, data []byte) {
		tex, _, err := render3d.LoadKTX2(nil, sr.Device(), data, render3d.KTX2Upload{})
		if err != nil {
			if !errors.Is(err, render3d.ErrInvalidTexture) && !errors.As(err, new(gpu.Error)) {
				t.Fatalf("error %v wraps neither ErrInvalidTexture nor a gpu.Error", err)
			}
			return
		}
		_ = tex.Close()
	})
}
