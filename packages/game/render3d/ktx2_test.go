package render3d_test

import (
	"errors"
	"image"
	"image/color"
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
	for name, data := range map[string][]byte{
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
