package render3d

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidTexture is wrapped by every error [PixelBuffer.Validate], [PixelBufferFromImage], [UploadTexture], [UploadTexturePixels], [GPUTexture.UpdateImage], [GPUTexture.UpdatePixels] and [BindTexture] return for malformed texture data or an invalid texture handle.
var ErrInvalidTexture = errors.New("render3d: invalid texture")

// ErrInvalidSampler is wrapped by every error [SamplerOptions.Validate], [NewGPUSampler] and [BindTexture] return for malformed sampler settings or an invalid sampler handle.
var ErrInvalidSampler = errors.New("render3d: invalid sampler")

var srgbToLinearLUT [256]float64

func init() {
	for i := range 256 {
		s := float64(i) / 255.0
		if s <= 0.04045 {
			srgbToLinearLUT[i] = s / 12.92
		} else {
			srgbToLinearLUT[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
}

// PixelBuffer holds uncompressed 2D pixel data on the CPU, ready to upload to a GPU view with [UploadTexturePixels].
//
// Width and Height must be positive. Format is one of [gpu.FormatRGBA8Unorm], [gpu.FormatRGBA8UnormSRGB], [gpu.FormatRG8Unorm], [gpu.FormatR8Unorm], or [gpu.FormatUndefined] (which selects RGBA8 linear or sRGB per [TextureUpload.SRGB]). Stride is the byte distance between consecutive rows; when zero, rows are assumed tightly packed (Width * BytesPerPixel).
type PixelBuffer struct {
	Width  int
	Height int
	Stride int
	Format gpu.TextureFormat
	Pixels []byte
}

// BytesPerPixel returns the byte width of one pixel for b.Format, or zero for an unsupported format. [gpu.FormatUndefined] defaults to 4 (RGBA8).
func (b PixelBuffer) BytesPerPixel() int {
	return pixelBytesPerPixel(b.Format)
}

func pixelBytesPerPixel(format gpu.TextureFormat) int {
	switch format {
	case gpu.FormatUndefined, gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB:
		return 4
	case gpu.FormatRG8Unorm:
		return 2
	case gpu.FormatR8Unorm:
		return 1
	default:
		return 0
	}
}

// TightSize returns the byte length of tightly packed rows for b (Width * Height * BytesPerPixel), or zero when b has non-positive dimensions or an unsupported format.
func (b PixelBuffer) TightSize() int {
	bpp := b.BytesPerPixel()
	if b.Width <= 0 || b.Height <= 0 || bpp == 0 {
		return 0
	}
	return b.Width * b.Height * bpp
}

// Validate reports whether b describes a valid uncompressed 2D pixel buffer: positive dimensions within uint32 range, a supported 8-bit format, a non-negative stride at least one row wide, and enough bytes in Pixels for every row. Every error wraps [ErrInvalidTexture].
func (b PixelBuffer) Validate() error {
	if b.Width <= 0 || b.Height <= 0 {
		return fmt.Errorf("%w: dimensions %dx%d must be positive", ErrInvalidTexture, b.Width, b.Height)
	}
	if uint64(b.Width) > math.MaxUint32 || uint64(b.Height) > math.MaxUint32 {
		return fmt.Errorf("%w: dimensions %dx%d exceed uint32 range", ErrInvalidTexture, b.Width, b.Height)
	}
	bpp := b.BytesPerPixel()
	if bpp == 0 {
		return fmt.Errorf("%w: unsupported pixel format %s", ErrInvalidTexture, b.Format)
	}
	rowBytes := uint64(b.Width) * uint64(bpp)
	stride := rowBytes
	if b.Stride < 0 {
		return fmt.Errorf("%w: negative stride %d", ErrInvalidTexture, b.Stride)
	}
	if b.Stride > 0 {
		if uint64(b.Stride) < rowBytes {
			return fmt.Errorf("%w: stride %d is smaller than row width %d", ErrInvalidTexture, b.Stride, rowBytes)
		}
		stride = uint64(b.Stride)
	}
	// Overflow-safe check for (Height-1)*stride + rowBytes <= len(Pixels).
	avail := uint64(len(b.Pixels))
	if avail < rowBytes {
		return fmt.Errorf("%w: pixel buffer has %d bytes, need at least %d", ErrInvalidTexture, len(b.Pixels), rowBytes)
	}
	if b.Height > 1 {
		rem := avail - rowBytes
		if uint64(b.Height-1) > rem/stride {
			need := uint64(b.Height-1)*stride + rowBytes
			return fmt.Errorf("%w: pixel buffer has %d bytes, need %d for %dx%d stride %d", ErrInvalidTexture, len(b.Pixels), need, b.Width, b.Height, stride)
		}
	}
	return nil
}

// PixelBufferFromImage converts any [image.Image] (including sub-images with a non-zero Bounds().Min) into a tightly packed straight-alpha RGBA8 [PixelBuffer]. When img is already an [*image.NRGBA], its channel bytes are copied verbatim even for pixels with zero alpha, preserving packed data maps. Every error wraps [ErrInvalidTexture].
func PixelBufferFromImage(img image.Image) (PixelBuffer, error) {
	if img == nil {
		return PixelBuffer{}, fmt.Errorf("%w: nil image", ErrInvalidTexture)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return PixelBuffer{}, fmt.Errorf("%w: empty image bounds %v", ErrInvalidTexture, bounds)
	}
	if uint64(w) > math.MaxUint32 || uint64(h) > math.MaxUint32 {
		return PixelBuffer{}, fmt.Errorf("%w: image dimensions %dx%d exceed uint32 range", ErrInvalidTexture, w, h)
	}
	rowBytes := w * 4
	pix := make([]byte, rowBytes*h)
	if src, ok := img.(*image.NRGBA); ok {
		for y := range h {
			off := src.PixOffset(bounds.Min.X, bounds.Min.Y+y)
			copy(pix[y*rowBytes:(y+1)*rowBytes], src.Pix[off:off+rowBytes])
		}
	} else {
		dst := &image.NRGBA{
			Pix:    pix,
			Stride: rowBytes,
			Rect:   image.Rect(0, 0, w, h),
		}
		draw.Draw(dst, dst.Rect, img, bounds.Min, draw.Src)
	}
	return PixelBuffer{
		Width:  w,
		Height: h,
		Stride: rowBytes,
		Format: gpu.FormatRGBA8Unorm,
		Pixels: pix,
	}, nil
}

// TextureUpload configures [UploadTexture] and [UploadTexturePixels].
type TextureUpload struct {
	// Label names the texture in GPU capture tools.
	Label string
	// SRGB stores RGBA8 channels in [gpu.FormatRGBA8UnormSRGB] so the GPU decodes RGB to linear light when sampling. Set it for color textures (base color / albedo, emissive); leave it false for data textures (normal, metallic-roughness, occlusion).
	SRGB bool
	// Mipmaps builds a CPU mipmap chain (box-filtered in linear light when SRGB is true) and uploads every level, so mipmapping works on every GPU backend including those without GPU mipmap generation.
	Mipmaps bool
	// MaxMipLevels caps the mipmap chain when Mipmaps is true; zero generates the full chain down to 1x1.
	MaxMipLevels uint32
	// Retained keeps a CPU copy of the texture and its uploaded mip levels so NSGo recreates them after a device loss (see [gpu.TextureDesc].Retained).
	Retained bool
}

// GPUTexture is a 2D texture uploaded to an NSGo [gpu.Device]. Its underlying [gpu.Texture] belongs to the [core.Scope] passed at upload and is released when that scope is disposed or when [GPUTexture.Close] is called. Use it from the engine goroutine, like the [gpu.Device] it came from.
type GPUTexture struct {
	tex       *gpu.Texture
	width     uint32
	height    uint32
	mipLevels uint32
	format    gpu.TextureFormat
}

// UploadTexture converts img to straight-alpha RGBA8 and uploads it to dev, scoped to cx. An invalid image returns an error wrapping [ErrInvalidTexture] before anything is created; a device error (such as [gpu.ErrDeviceLost] or dimensions over [gpu.Limits].MaxTextureDimension2D) is returned as the [gpu.Error] NSGo reports.
func UploadTexture(cx *core.Scope, dev *gpu.Device, img image.Image, opts TextureUpload) (*GPUTexture, error) {
	buf, err := PixelBufferFromImage(img)
	if err != nil {
		return nil, err
	}
	return UploadTexturePixels(cx, dev, buf, opts)
}

// UploadTexturePixels validates buf and uploads it to dev as a sampled 2D texture, scoped to cx. When opts.Mipmaps is true, every level of the mipmap chain is generated on the CPU (in linear light for sRGB textures) and uploaded with [gpu.Device.WriteTextureMip].
func UploadTexturePixels(cx *core.Scope, dev *gpu.Device, buf PixelBuffer, opts TextureUpload) (*GPUTexture, error) {
	if err := buf.Validate(); err != nil {
		return nil, err
	}
	format, err := resolveTextureFormat(buf.Format, opts.SRGB)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	w, h := uint32(buf.Width), uint32(buf.Height)
	mipLevels := uint32(1)
	if opts.Mipmaps {
		mipLevels = gpu.MaxMipLevels(w, h)
		if opts.MaxMipLevels > 0 && opts.MaxMipLevels < mipLevels {
			mipLevels = opts.MaxMipLevels
		}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.texture"
	}
	tex, err := dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label,
		Dimension: gpu.Dim2D,
		Width:     w,
		Height:    h,
		MipLevels: mipLevels,
		Format:    format,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	uploadTextureLevels(dev, tex, buf, format, mipLevels)
	return &GPUTexture{
		tex:       tex,
		width:     w,
		height:    h,
		mipLevels: mipLevels,
		format:    format,
	}, nil
}

// UpdateImage replaces t's pixels from img (and regenerates its mip chain when t has more than one mip level). img must have the same dimensions as t, and t must have an RGBA8 format.
func (t *GPUTexture) UpdateImage(dev *gpu.Device, img image.Image) error {
	buf, err := PixelBufferFromImage(img)
	if err != nil {
		return err
	}
	return t.UpdatePixels(dev, buf)
}

// UpdatePixels replaces t's pixels from buf (and regenerates its mip chain when t has more than one mip level). buf must match t's dimensions and pixel format (passing [gpu.FormatUndefined] or [gpu.FormatRGBA8Unorm] is accepted for both linear and sRGB RGBA8 textures).
func (t *GPUTexture) UpdatePixels(dev *gpu.Device, buf PixelBuffer) error {
	if t == nil || !t.Valid() {
		return fmt.Errorf("%w: texture is not valid", ErrInvalidTexture)
	}
	if err := buf.Validate(); err != nil {
		return err
	}
	if uint32(buf.Width) != t.width || uint32(buf.Height) != t.height {
		return fmt.Errorf("%w: update size %dx%d does not match texture %dx%d", ErrInvalidTexture, buf.Width, buf.Height, t.width, t.height)
	}
	if !compatibleUpdateFormat(t.format, buf.Format) {
		return fmt.Errorf("%w: update format %s does not match texture format %s", ErrInvalidTexture, buf.Format, t.format)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	uploadTextureLevels(dev, t.tex, buf, t.format, t.mipLevels)
	return nil
}

// Texture returns the underlying [gpu.Texture], or nil for a nil receiver.
func (t *GPUTexture) Texture() *gpu.Texture {
	if t == nil {
		return nil
	}
	return t.tex
}

// Width returns the base level width in texels, or zero for a nil receiver.
func (t *GPUTexture) Width() uint32 {
	if t == nil {
		return 0
	}
	return t.width
}

// Height returns the base level height in texels, or zero for a nil receiver.
func (t *GPUTexture) Height() uint32 {
	if t == nil {
		return 0
	}
	return t.height
}

// MipLevels returns the number of mipmap levels uploaded (1 without mipmaps), or zero for a nil receiver.
func (t *GPUTexture) MipLevels() uint32 {
	if t == nil {
		return 0
	}
	return t.mipLevels
}

// Format returns the GPU texture format, or [gpu.FormatUndefined] for a nil receiver.
func (t *GPUTexture) Format() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.format
}

// SRGB reports whether the texture decodes sRGB RGB channels to linear light on sample.
func (t *GPUTexture) SRGB() bool {
	return t != nil && t.format.IsSRGB()
}

// ByteSize returns the total GPU memory across all uploaded mipmap levels, for [RenderStats] and [Budget] accounting. It is zero for a nil texture.
func (t *GPUTexture) ByteSize() uint64 {
	if t == nil || t.width == 0 || t.height == 0 {
		return 0
	}
	var total uint64
	for lvl := range t.mipLevels {
		mw, mh := gpu.MipSize(t.width, t.height, lvl)
		total += t.format.ByteSize(mw, mh)
	}
	return total
}

// Valid reports whether the texture is open and belongs to a live device.
func (t *GPUTexture) Valid() bool {
	return t != nil && t.tex.Valid()
}

// Close releases the underlying [gpu.Texture] now rather than when its scope is disposed. It is safe to call on a nil receiver or more than once.
func (t *GPUTexture) Close() error {
	if t == nil || t.tex == nil {
		return nil
	}
	return t.tex.Close()
}

// SamplerOptions configures texture filtering and coordinate wrapping for [NewGPUSampler].
//
// Its zero value uses nearest filtering and clamp-to-edge wrapping (matching [gpu.SamplerDesc]). Use [SamplerLinearRepeat], [SamplerLinearClamp], [SamplerLinearMirror], [SamplerNearestRepeat] or [SamplerNearestClamp] for common presets.
type SamplerOptions struct {
	Label         string
	MinFilter     gpu.FilterMode
	MagFilter     gpu.FilterMode
	MipmapFilter  gpu.FilterMode
	AddressU      gpu.AddressMode
	AddressV      gpu.AddressMode
	AddressW      gpu.AddressMode
	MinLOD        float32
	MaxLOD        float32 // 0 defaults to 32 (unbounded)
	MaxAnisotropy uint16  // 0 or 1 disables anisotropic filtering; >1 requires linear min, mag and mipmap filters (WebGPU rule)
	Retained      bool
}

// SamplerLinearRepeat returns trilinear filtering with repeating U/V/W coordinates, suited to tiled 3D surfaces.
func SamplerLinearRepeat() SamplerOptions {
	return SamplerOptions{
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressRepeat,
		AddressV:     gpu.AddressRepeat,
		AddressW:     gpu.AddressRepeat,
	}
}

// SamplerLinearClamp returns trilinear filtering with clamp-to-edge wrapping, suited to non-repeating textures and UI/decals.
func SamplerLinearClamp() SamplerOptions {
	return SamplerOptions{
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
	}
}

// SamplerLinearMirror returns trilinear filtering with mirrored-repeat wrapping on U/V/W.
func SamplerLinearMirror() SamplerOptions {
	return SamplerOptions{
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressMirrorRepeat,
		AddressV:     gpu.AddressMirrorRepeat,
		AddressW:     gpu.AddressMirrorRepeat,
	}
}

// SamplerNearestRepeat returns nearest-neighbor filtering with repeating U/V/W coordinates, suited to tiled pixel-art surfaces.
func SamplerNearestRepeat() SamplerOptions {
	return SamplerOptions{
		MinFilter:    gpu.FilterNearest,
		MagFilter:    gpu.FilterNearest,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressRepeat,
		AddressV:     gpu.AddressRepeat,
		AddressW:     gpu.AddressRepeat,
	}
}

// SamplerNearestClamp returns nearest-neighbor filtering with clamp-to-edge wrapping, suited to crisp pixel-art sprites and lookup tables.
func SamplerNearestClamp() SamplerOptions {
	return SamplerOptions{
		MinFilter:    gpu.FilterNearest,
		MagFilter:    gpu.FilterNearest,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
	}
}

// Validate checks that every filter mode, address mode, LOD bound and anisotropy setting is valid across all NSGo GPU backends (including WebGPU's requirement that MaxAnisotropy > 1 uses linear min, mag and mipmap filtering). Every error wraps [ErrInvalidSampler].
func (o SamplerOptions) Validate() error {
	if !validFilterMode(o.MinFilter) {
		return fmt.Errorf("%w: invalid min filter %d", ErrInvalidSampler, o.MinFilter)
	}
	if !validFilterMode(o.MagFilter) {
		return fmt.Errorf("%w: invalid mag filter %d", ErrInvalidSampler, o.MagFilter)
	}
	if !validFilterMode(o.MipmapFilter) {
		return fmt.Errorf("%w: invalid mipmap filter %d", ErrInvalidSampler, o.MipmapFilter)
	}
	if !validAddressMode(o.AddressU) {
		return fmt.Errorf("%w: invalid address mode U %d", ErrInvalidSampler, o.AddressU)
	}
	if !validAddressMode(o.AddressV) {
		return fmt.Errorf("%w: invalid address mode V %d", ErrInvalidSampler, o.AddressV)
	}
	if !validAddressMode(o.AddressW) {
		return fmt.Errorf("%w: invalid address mode W %d", ErrInvalidSampler, o.AddressW)
	}
	if !finite(o.MinLOD) || o.MinLOD < 0 {
		return fmt.Errorf("%w: MinLOD %v must be finite and non-negative", ErrInvalidSampler, o.MinLOD)
	}
	if !finite(o.MaxLOD) || o.MaxLOD < 0 {
		return fmt.Errorf("%w: MaxLOD %v must be finite and non-negative", ErrInvalidSampler, o.MaxLOD)
	}
	maxLOD := o.MaxLOD
	if maxLOD == 0 {
		maxLOD = 32
	}
	if maxLOD < o.MinLOD {
		return fmt.Errorf("%w: MaxLOD (%g) must be >= MinLOD (%g)", ErrInvalidSampler, maxLOD, o.MinLOD)
	}
	if o.MaxAnisotropy > 1 {
		if o.MinFilter != gpu.FilterLinear || o.MagFilter != gpu.FilterLinear || o.MipmapFilter != gpu.FilterLinear {
			return fmt.Errorf("%w: MaxAnisotropy %d requires linear min, mag and mipmap filters", ErrInvalidSampler, o.MaxAnisotropy)
		}
	}
	return nil
}

// Desc converts o into a [gpu.SamplerDesc], applying defaults (MaxLOD 0 -> 32, MaxAnisotropy 0 -> 1).
func (o SamplerOptions) Desc() gpu.SamplerDesc {
	maxLOD := o.MaxLOD
	if maxLOD == 0 {
		maxLOD = 32
	}
	maxAniso := o.MaxAnisotropy
	if maxAniso == 0 {
		maxAniso = 1
	}
	return gpu.SamplerDesc{
		Label:         o.Label,
		MinFilter:     o.MinFilter,
		MagFilter:     o.MagFilter,
		MipmapFilter:  o.MipmapFilter,
		AddressU:      o.AddressU,
		AddressV:      o.AddressV,
		AddressW:      o.AddressW,
		MinLOD:        o.MinLOD,
		MaxLOD:        maxLOD,
		MaxAnisotropy: maxAniso,
		Retained:      o.Retained,
	}
}

// GPUSampler is a sampler allocated on an NSGo [gpu.Device]. Its underlying [gpu.Sampler] belongs to the [core.Scope] passed to [NewGPUSampler] and is released when that scope is disposed or when [GPUSampler.Close] is called.
type GPUSampler struct {
	sampler *gpu.Sampler
	opts    SamplerOptions
}

// NewGPUSampler validates opts and creates a [GPUSampler] on dev, scoped to cx. Invalid options return an error wrapping [ErrInvalidSampler] before anything is created; a nil or lost device returns [gpu.ErrDeviceLost].
func NewGPUSampler(cx *core.Scope, dev *gpu.Device, opts SamplerOptions) (*GPUSampler, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	desc := opts.Desc()
	if desc.Label == "" {
		desc.Label = "render3d.sampler"
	}
	samp, err := dev.CreateSampler(cx, desc)
	if err != nil {
		return nil, err
	}
	normalized := opts
	normalized.MaxLOD = desc.MaxLOD
	normalized.MaxAnisotropy = desc.MaxAnisotropy
	return &GPUSampler{
		sampler: samp,
		opts:    normalized,
	}, nil
}

// Sampler returns the underlying [gpu.Sampler], or nil for a nil receiver.
func (s *GPUSampler) Sampler() *gpu.Sampler {
	if s == nil {
		return nil
	}
	return s.sampler
}

// Options returns the normalized [SamplerOptions] used to create s, or the zero value for a nil receiver.
func (s *GPUSampler) Options() SamplerOptions {
	if s == nil {
		return SamplerOptions{}
	}
	return s.opts
}

// Valid reports whether the sampler is open and belongs to a live device.
func (s *GPUSampler) Valid() bool {
	return s != nil && s.sampler.Valid()
}

// Close releases the underlying [gpu.Sampler] now rather than when its scope is disposed. It is safe to call on a nil receiver or more than once.
func (s *GPUSampler) Close() error {
	if s == nil || s.sampler == nil {
		return nil
	}
	return s.sampler.Close()
}

// TextureBindingLayout creates a bind group layout with a fragment-visible sampled 2D texture at binding 0 and its sampler at binding 1. Place it at whichever group slot your pipeline reserves for textures (for example group 1 alongside [CameraGroup] at group 0).
func TextureBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.texture",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// BindTexture creates a bind group matching [TextureBindingLayout], binding tex at binding 0 and samp at binding 1. When both tex and samp were created with Retained set, NSGo automatically restores the bind group after a device loss.
func BindTexture(cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, tex *GPUTexture, samp *GPUSampler) (*gpu.BindGroup, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if layout == nil || !layout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid texture bind group layout"}
	}
	if !tex.Valid() {
		return nil, fmt.Errorf("%w: texture is not valid", ErrInvalidTexture)
	}
	if !samp.Valid() {
		return nil, fmt.Errorf("%w: sampler is not valid", ErrInvalidSampler)
	}
	return dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  "render3d.texture",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: tex.tex},
			{Binding: 1, Sampler: samp.sampler},
		},
	})
}

func resolveTextureFormat(bufFormat gpu.TextureFormat, srgb bool) (gpu.TextureFormat, error) {
	switch bufFormat {
	case gpu.FormatUndefined:
		if srgb {
			return gpu.FormatRGBA8UnormSRGB, nil
		}
		return gpu.FormatRGBA8Unorm, nil
	case gpu.FormatRGBA8Unorm:
		if srgb {
			return gpu.FormatRGBA8UnormSRGB, nil
		}
		return gpu.FormatRGBA8Unorm, nil
	case gpu.FormatRGBA8UnormSRGB:
		return gpu.FormatRGBA8UnormSRGB, nil
	case gpu.FormatRG8Unorm, gpu.FormatR8Unorm:
		if srgb {
			return gpu.FormatUndefined, fmt.Errorf("%w: sRGB requires RGBA8 format, got %s", ErrInvalidTexture, bufFormat)
		}
		return bufFormat, nil
	default:
		return gpu.FormatUndefined, fmt.Errorf("%w: unsupported pixel format %s", ErrInvalidTexture, bufFormat)
	}
}

func compatibleUpdateFormat(texFormat, bufFormat gpu.TextureFormat) bool {
	if bufFormat == texFormat {
		return true
	}
	if texFormat == gpu.FormatRGBA8Unorm || texFormat == gpu.FormatRGBA8UnormSRGB {
		return bufFormat == gpu.FormatUndefined || bufFormat == gpu.FormatRGBA8Unorm || bufFormat == gpu.FormatRGBA8UnormSRGB
	}
	return false
}

func validFilterMode(m gpu.FilterMode) bool {
	return m == gpu.FilterNearest || m == gpu.FilterLinear
}

func validAddressMode(m gpu.AddressMode) bool {
	return m == gpu.AddressClampToEdge || m == gpu.AddressRepeat || m == gpu.AddressMirrorRepeat
}

func uploadTextureLevels(dev *gpu.Device, tex *gpu.Texture, buf PixelBuffer, format gpu.TextureFormat, mipLevels uint32) {
	bpp := pixelBytesPerPixel(format)
	w, h := buf.Width, buf.Height
	rowBytes := w * bpp
	stride := buf.Stride
	if stride == 0 {
		stride = rowBytes
	}

	if mipLevels <= 1 {
		dev.WriteTextureMip(tex, 0, 0, buf.Pixels, uint32(stride))
		return
	}

	// Pack level 0 tightly once so every subsequent mip level downsamples contiguous rows.
	cur := packPixelRows(buf.Pixels, w, h, bpp, stride)
	dev.WriteTextureMip(tex, 0, 0, cur, uint32(rowBytes))

	srgb := format.IsSRGB()
	curW, curH := w, h
	for lvl := uint32(1); lvl < mipLevels; lvl++ {
		nextW := max(1, curW>>1)
		nextH := max(1, curH>>1)
		cur = downsamplePixelsBox(cur, curW, curH, nextW, nextH, bpp, srgb)
		curW, curH = nextW, nextH
		dev.WriteTextureMip(tex, lvl, 0, cur, uint32(curW*bpp))
	}
}

func packPixelRows(src []byte, w, h, bpp, stride int) []byte {
	rowBytes := w * bpp
	if stride == rowBytes {
		return src[:rowBytes*h]
	}
	out := make([]byte, rowBytes*h)
	for y := range h {
		copy(out[y*rowBytes:(y+1)*rowBytes], src[y*stride:y*stride+rowBytes])
	}
	return out
}

// downsamplePixelsBox downsamples tightly packed 8-bit channels (bpp 1, 2 or 4) into dstW x dstH using an area-weighted box filter. When srgb is true (RGBA8), RGB channels are averaged in linear light and re-encoded to sRGB while alpha is averaged linearly.
func downsamplePixelsBox(src []byte, srcW, srcH, dstW, dstH, bpp int, srgb bool) []byte {
	dst := make([]byte, dstW*dstH*bpp)
	srcRow := srcW * bpp
	dstRow := dstW * bpp

	// Fast path for even dimensions: exact 2x2 box average.
	if srcW == dstW*2 && srcH == dstH*2 {
		for dy := range dstH {
			sy0 := dy * 2 * srcRow
			sy1 := sy0 + srcRow
			dOff := dy * dstRow
			for dx := range dstW {
				sx0 := dx * 2 * bpp
				sx1 := sx0 + bpp
				i00 := sy0 + sx0
				i10 := sy0 + sx1
				i01 := sy1 + sx0
				i11 := sy1 + sx1
				out := dOff + dx*bpp
				if srgb && bpp == 4 {
					for ch := range 3 {
						lin := (srgbToLinearLUT[src[i00+ch]] + srgbToLinearLUT[src[i10+ch]] + srgbToLinearLUT[src[i01+ch]] + srgbToLinearLUT[src[i11+ch]]) * 0.25
						dst[out+ch] = linearToSRGBByte(lin)
					}
					sumA := int(src[i00+3]) + int(src[i10+3]) + int(src[i01+3]) + int(src[i11+3])
					dst[out+3] = byte((sumA + 2) / 4)
				} else {
					for ch := range bpp {
						sum := int(src[i00+ch]) + int(src[i10+ch]) + int(src[i01+ch]) + int(src[i11+ch])
						dst[out+ch] = byte((sum + 2) / 4)
					}
				}
			}
		}
		return dst
	}

	// General area-weighted box filter for odd or 1-texel-tall/wide dimensions.
	scaleX := float64(srcW) / float64(dstW)
	scaleY := float64(srcH) / float64(dstH)
	for dy := range dstH {
		sy0 := float64(dy) * scaleY
		sy1 := float64(dy+1) * scaleY
		iy0 := int(math.Floor(sy0))
		iy1 := min(srcH-1, int(math.Ceil(sy1))-1)

		for dx := range dstW {
			sx0 := float64(dx) * scaleX
			sx1 := float64(dx+1) * scaleX
			ix0 := int(math.Floor(sx0))
			ix1 := min(srcW-1, int(math.Ceil(sx1))-1)

			var sum [4]float64
			var sumW float64
			for sy := iy0; sy <= iy1; sy++ {
				wy := min(float64(sy+1), sy1) - max(float64(sy), sy0)
				if wy <= 0 {
					continue
				}
				rOff := sy * srcRow
				for sx := ix0; sx <= ix1; sx++ {
					wx := min(float64(sx+1), sx1) - max(float64(sx), sx0)
					if wx <= 0 {
						continue
					}
					w := wx * wy
					off := rOff + sx*bpp
					if srgb && bpp == 4 {
						sum[0] += srgbToLinearLUT[src[off+0]] * w
						sum[1] += srgbToLinearLUT[src[off+1]] * w
						sum[2] += srgbToLinearLUT[src[off+2]] * w
						sum[3] += float64(src[off+3]) * w
					} else {
						for ch := range bpp {
							sum[ch] += float64(src[off+ch]) * w
						}
					}
					sumW += w
				}
			}
			if sumW <= 0 {
				continue
			}
			invW := 1.0 / sumW
			out := dy*dstRow + dx*bpp
			if srgb && bpp == 4 {
				dst[out+0] = linearToSRGBByte(sum[0] * invW)
				dst[out+1] = linearToSRGBByte(sum[1] * invW)
				dst[out+2] = linearToSRGBByte(sum[2] * invW)
				dst[out+3] = clampByte(int(math.Round(sum[3] * invW)))
			} else {
				for ch := range bpp {
					dst[out+ch] = clampByte(int(math.Round(sum[ch] * invW)))
				}
			}
		}
	}
	return dst
}

func linearToSRGBByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	var s float64
	if v <= 0.0031308 {
		s = v * 12.92
	} else {
		s = 1.055*math.Pow(v, 1.0/2.4) - 0.055
	}
	return clampByte(int(math.Round(s * 255.0)))
}

func clampByte(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
