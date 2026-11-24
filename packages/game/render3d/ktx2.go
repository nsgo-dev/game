package render3d

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
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
}

// LoadKTX2 uploads the 2D texture in data, a KTX2 file such as game assets and game tex write, to dev, scoped to cx, and returns it with the sampler to draw it with: opts.Sampler with its filters set from the file's [KTX2FilterKey], as [KTX2Sampler] sets them, so an image's import settings decide whether it is drawn sharp or smooth. Every mip level the file holds is uploaded as it is, in its sRGB or linear format. A block-compressed level (BC1, BC3 or ETC2) is uploaded compressed when dev supports its format ([gpu.Device.SupportsCompressedFormat]) and otherwise decoded to RGBA8 on the CPU, so the texture loads on every backend. A file that is not a valid uncompressed-container 2D KTX2 texture in RGBA8, BC1, BC3 or ETC2, or whose filter is unknown, returns an error wrapping [ErrInvalidTexture] before anything is created; a device error is returned as the [gpu.Error] NSGo reports.
func LoadKTX2(cx *core.Scope, dev *gpu.Device, data []byte, opts KTX2Upload) (*GPUTexture, SamplerOptions, error) {
	sampler, err := KTX2Sampler(data, opts.Sampler)
	if err != nil {
		return nil, SamplerOptions{}, err
	}
	if err := ktx2.Validate(data); err != nil {
		return nil, SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
	}
	h, levels, err := ktx2.DecodeHeader(data)
	if err != nil {
		return nil, SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
	}
	if h.PixelDepth > 1 || h.LayerCount > 1 || h.FaceCount != 1 {
		return nil, SamplerOptions{}, fmt.Errorf("%w: KTX2 texture is not 2D (depth %d, layers %d, faces %d)", ErrInvalidTexture, h.PixelDepth, h.LayerCount, h.FaceCount)
	}
	if h.SupercompressionScheme != ktx2.SupercompressionNone {
		return nil, SamplerOptions{}, fmt.Errorf("%w: KTX2 supercompression scheme %d is not supported", ErrInvalidTexture, h.SupercompressionScheme)
	}
	kf, srgb, ok := ktx2.FormatFromVk(h.VkFormat)
	if !ok {
		return nil, SamplerOptions{}, fmt.Errorf("%w: KTX2 vkFormat %d is not RGBA8, BC1, BC3 or ETC2", ErrInvalidTexture, h.VkFormat)
	}
	if dev == nil {
		return nil, SamplerOptions{}, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	format, native := ktx2GPUFormat(kf, srgb)
	if native && format.IsCompressed() && !dev.SupportsCompressedFormat(format) {
		native = false
	}
	if !native {
		format = gpu.FormatRGBA8Unorm
		if srgb {
			format = gpu.FormatRGBA8UnormSRGB
		}
	}
	w, ht := h.PixelWidth, h.PixelHeight
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
	for lvl := range levels {
		if native {
			raw, lw, _, err := ktx2.ReadLevel(data, lvl)
			if err != nil {
				_ = tex.Close()
				return nil, SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
			}
			bw, _ := format.BlockSize()
			blocks := (uint32(lw) + bw - 1) / bw
			dev.WriteTextureMip(tex, uint32(lvl), 0, raw, blocks*format.BytesPerBlock())
			continue
		}
		img, err := ktx2.DecodeRGBA(data, lvl)
		if err != nil {
			_ = tex.Close()
			return nil, SamplerOptions{}, fmt.Errorf("%w: %w", ErrInvalidTexture, err)
		}
		dev.WriteTextureMip(tex, uint32(lvl), 0, img.Pix, uint32(img.Stride))
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

// ktx2GPUFormat returns the GPU format a KTX2 format uploads as without decoding, and false when NSGo has none, as for sRGB ETC2.
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
	case f == ktx2.FormatETC2 && !srgb:
		return gpu.FormatETC2RGB8Unorm, true
	}
	return gpu.FormatUndefined, false
}
