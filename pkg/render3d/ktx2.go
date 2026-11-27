package render3d

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/tooling/ktx2"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// KTX2FilterKey is the key of a KTX2 file's key/value data that holds the sampling filter the image's import settings ask for, "linear" or "nearest", as game assets and game tex write it from the filter of the image's sidecar.
const KTX2FilterKey = "game.filter"

// KTX2Upload configures [LoadKTX2].
type KTX2Upload struct {
	// Label names the texture in GPU capture tools.
	Label string
	// Retained keeps a CPU copy of every mip level so NSGo recreates the texture after a device loss (see [gpu.TextureDesc].Retained).
	Retained bool
	// Sampler is the sampler the texture is drawn with before the file's filter is applied: its wrapping, LOD bounds and label are kept, and its filters are kept when the file names none.
	Sampler SamplerOptions
	// ForceRGBA8 uploads every level decoded to RGBA8 even when dev supports the file's block format or one a UASTC or ETC1S file could be transcoded to.
	ForceRGBA8 bool
}

// LoadKTX2 uploads the 2D texture in data, a KTX2 file such as game assets and game tex write or a Basis Universal UASTC or BasisLZ/ETC1S file, to dev, scoped to cx, and returns it with the sampler to draw it with: opts.Sampler with its filters set from the file's [KTX2FilterKey], as [KTX2Sampler] sets them, so an image's import settings decide whether it is drawn sharp or smooth.
//
// Every mip level the file holds is uploaded, in its sRGB or linear format, after undoing the file's BasisLZ, Zstandard or ZLIB supercompression. A block-compressed level (BC1, BC3, BC7, ETC2 RGB8 or RGBA8, or ASTC 4x4) is uploaded compressed when dev supports its format ([gpu.Device.SupportsCompressedFormat]) and otherwise decoded to RGBA8 on the CPU, so the texture loads on every backend. A UASTC or BasisLZ/ETC1S level is transcoded to the best block format dev supports, in the order BC7, BC1 for an opaque texture, BC3, ETC2 (linear only) and ASTC 4x4, and decoded to RGBA8 when dev supports none of them or the base level's width or height is not a multiple of 4. Transcoding decodes each level and encodes it again on the calling goroutine, which costs time at load for a large texture.
//
// A file that is not a valid 2D KTX2 texture in one of those formats, or whose filter is unknown, returns an error wrapping [ErrInvalidTexture] before anything is created; a device error is returned as the [gpu.Error] NSGo reports.
func LoadKTX2(cx *core.Scope, dev *gpu.Device, data []byte, opts KTX2Upload) (*GPUTexture, SamplerOptions, error) {
	sampler, err := KTX2Sampler(data, opts.Sampler)
	if err != nil {
		return nil, SamplerOptions{}, err
	}
	t, err := ktx2.Decode(data)
	if err != nil {
		return nil, SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
	}
	if dev == nil {
		return nil, SamplerOptions{}, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	target, format := ktx2Target(dev, t, opts.ForceRGBA8)
	levels, err := t.Transcode(target)
	if err != nil {
		return nil, SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
	}
	w, ht := uint32(t.Width), uint32(t.Height)
	label := opts.Label
	if label == "" {
		label = "render3d.ktx2"
	}
	tex, err := dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label,
		Dimension: gpu.Dim2D,
		Width:     w,
		Height:    ht,
		MipLevels: uint32(len(levels)),
		Format:    format,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return nil, SamplerOptions{}, err
	}
	for lvl, l := range levels {
		bytesPerRow := uint32(l.Width) * 4
		if format.IsCompressed() {
			bw, _ := format.BlockSize()
			bytesPerRow = (uint32(l.Width) + bw - 1) / bw * format.BytesPerBlock()
		}
		dev.WriteTextureMip(tex, uint32(lvl), 0, l.Data, bytesPerRow)
	}
	return &GPUTexture{tex: tex, width: w, height: ht, mipLevels: uint32(len(levels)), format: format}, sampler, nil
}

// KTX2Sampler returns base with its filters set from the [KTX2FilterKey] of the KTX2 file in data: "nearest" sets the min, mag and mipmap filters to [gpu.FilterNearest] and turns anisotropic filtering off, which keeps pixel art sharp, and "linear" sets them to [gpu.FilterLinear]. A file without the key returns base unchanged. A file whose key/value data cannot be read, or whose filter is neither, returns an error wrapping [ErrInvalidTexture].
func KTX2Sampler(data []byte, base SamplerOptions) (SamplerOptions, error) {
	h, _, err := ktx2.DecodeHeader(data)
	if err != nil {
		return SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
	}
	if h.KVDByteLength == 0 {
		return base, nil
	}
	end := uint64(h.KVDByteOffset) + uint64(h.KVDByteLength)
	if end > uint64(len(data)) {
		return SamplerOptions{}, fmt.Errorf("%w: KTX2 key/value data out of bounds", ErrInvalidTexture)
	}
	kvd, err := ktx2.DecodeKVD(data[h.KVDByteOffset:end])
	if err != nil {
		return SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
	}
	v, ok := kvd[KTX2FilterKey]
	if !ok {
		return base, nil
	}
	switch string(v) {
	case "nearest":
		base.MinFilter, base.MagFilter, base.MipmapFilter = gpu.FilterNearest, gpu.FilterNearest, gpu.FilterNearest
		base.MaxAnisotropy = 0
	case "linear":
		base.MinFilter, base.MagFilter, base.MipmapFilter = gpu.FilterLinear, gpu.FilterLinear, gpu.FilterLinear
	default:
		return SamplerOptions{}, fmt.Errorf("%w: KTX2 %s %q must be linear or nearest", ErrInvalidTexture, KTX2FilterKey, v)
	}
	return base, nil
}

// ktx2Target returns the format t's levels are transcoded to and the GPU format they upload as on dev. A block format dev supports uploads as it is, any other block format decodes to RGBA8, and UASTC or BasisLZ/ETC1S transcodes to the first of ktx2UniversalTargets dev supports.
func ktx2Target(dev *gpu.Device, t *ktx2.Texture, forceRGBA8 bool) (ktx2.Format, gpu.TextureFormat) {
	rgba := gpu.FormatRGBA8Unorm
	if t.SRGB {
		rgba = gpu.FormatRGBA8UnormSRGB
	}
	if forceRGBA8 {
		return ktx2.FormatRGBA8, rgba
	}
	if f, ok := ktx2GPUFormat(t.Format, t.SRGB); ok && (!f.IsCompressed() || dev.SupportsCompressedFormat(f)) {
		return t.Format, f
	}
	if (t.Format == ktx2.FormatUASTC || t.Format == ktx2.FormatETC1S) && t.Width%4 == 0 && t.Height%4 == 0 {
		for _, c := range ktx2UniversalTargets {
			if c.srgb == t.SRGB && (c.alpha || !t.HasAlpha) && dev.SupportsCompressedFormat(c.gpu) {
				return c.format, c.gpu
			}
		}
	}
	return ktx2.FormatRGBA8, rgba
}

// ktx2UniversalTargets are the block formats a UASTC or BasisLZ/ETC1S texture transcodes to, best first; a format without alpha is skipped for a texture with alpha.
var ktx2UniversalTargets = []struct {
	format ktx2.Format
	gpu    gpu.TextureFormat
	srgb   bool
	alpha  bool
}{
	{ktx2.FormatBC7, gpu.FormatBC7RGBAUnormSRGB, true, true},
	{ktx2.FormatBC1, gpu.FormatBC1RGBAUnormSRGB, true, false},
	{ktx2.FormatBC3, gpu.FormatBC3RGBAUnormSRGB, true, true},
	{ktx2.FormatASTC4x4, gpu.FormatASTC4x4UnormSRGB, true, true},
	{ktx2.FormatBC7, gpu.FormatBC7RGBAUnorm, false, true},
	{ktx2.FormatBC1, gpu.FormatBC1RGBAUnorm, false, false},
	{ktx2.FormatBC3, gpu.FormatBC3RGBAUnorm, false, true},
	{ktx2.FormatETC2, gpu.FormatETC2RGB8Unorm, false, false},
	{ktx2.FormatETC2RGBA, gpu.FormatETC2RGBA8Unorm, false, true},
	{ktx2.FormatASTC4x4, gpu.FormatASTC4x4Unorm, false, true},
}

// ktx2GPUFormat returns the GPU format a KTX2 format uploads as without decoding, and false when NSGo has none, as for sRGB ETC2 and for UASTC.
func ktx2GPUFormat(f ktx2.Format, srgb bool) (gpu.TextureFormat, bool) {
	switch {
	case f == ktx2.FormatRGBA8 && srgb:
		return gpu.FormatRGBA8UnormSRGB, true
	case f == ktx2.FormatRGBA8:
		return gpu.FormatRGBA8Unorm, true
	case f == ktx2.FormatBC1 && srgb:
		return gpu.FormatBC1RGBAUnormSRGB, true
	case f == ktx2.FormatBC1:
		return gpu.FormatBC1RGBAUnorm, true
	case f == ktx2.FormatBC3 && srgb:
		return gpu.FormatBC3RGBAUnormSRGB, true
	case f == ktx2.FormatBC3:
		return gpu.FormatBC3RGBAUnorm, true
	case f == ktx2.FormatBC7 && srgb:
		return gpu.FormatBC7RGBAUnormSRGB, true
	case f == ktx2.FormatBC7:
		return gpu.FormatBC7RGBAUnorm, true
	case f == ktx2.FormatETC2 && !srgb:
		return gpu.FormatETC2RGB8Unorm, true
	case f == ktx2.FormatETC2RGBA && !srgb:
		return gpu.FormatETC2RGBA8Unorm, true
	case f == ktx2.FormatASTC4x4 && srgb:
		return gpu.FormatASTC4x4UnormSRGB, true
	case f == ktx2.FormatASTC4x4:
		return gpu.FormatASTC4x4Unorm, true
	}
	return gpu.FormatUndefined, false
}
