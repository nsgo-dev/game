package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ExtendedMaterialUniformSize is the byte size of the 16-byte-aligned uniform block [ExtendedMaterialUniforms.AppendUniformData] and [ExtendedMaterialUniforms.Encode] write, matching struct ExtendedMaterialUniforms in [ExtendedMaterialBindingsWGSL] (64 bytes of [MaterialUniforms] plus 32 bytes of clearcoat, transmission, and IOR parameters).
const ExtendedMaterialUniformSize = 96

// Binding indices inside [MaterialGroup] (group 1) added by [ExtendedMaterialBindGroupLayout] after the base bindings 0..10 of [MaterialBindGroupLayout].
const (
	ExtendedMaterialClearcoatTextureBinding          uint32 = 11
	ExtendedMaterialClearcoatSamplerBinding          uint32 = 12
	ExtendedMaterialClearcoatRoughnessTextureBinding uint32 = 13
	ExtendedMaterialClearcoatRoughnessSamplerBinding uint32 = 14
	ExtendedMaterialClearcoatNormalTextureBinding    uint32 = 15
	ExtendedMaterialClearcoatNormalSamplerBinding    uint32 = 16
	ExtendedMaterialTransmissionTextureBinding       uint32 = 17
	ExtendedMaterialTransmissionSamplerBinding       uint32 = 18
)

// Bind group slots and uniform size for extended PBR pipelines ([NewExtendedMaterialPipeline]) and per-frame transmission background passes ([TransmissionBackground]).
const (
	// ExtendedLightGroup is the bind group slot [NewExtendedMaterialPipeline] assigns to [GPULightBuffer] when [ExtendedMaterialPipelineOptions].LightLayout is set, after [CameraGroup] (0) and [MaterialGroup] (1).
	ExtendedLightGroup = 2

	// TransmissionBackgroundGroup is the bind group slot [NewExtendedMaterialPipeline] assigns to [TransmissionBackground] when paired with [ExtendedMaterialPipelineOptions].LightLayout (groups 0..3: Camera, Material, Light, TransmissionBackground).
	TransmissionBackgroundGroup = 3

	// MaterialTransmissionBackgroundGroup is the bind group slot [NewExtendedMaterialPipeline] assigns to [TransmissionBackground] when [ExtendedMaterialPipelineOptions].LightLayout is nil (groups 0..2: Camera, Material, TransmissionBackground), mirroring [MaterialFogGroup].
	MaterialTransmissionBackgroundGroup = 2

	// TransmissionBackgroundUniformSize is the byte size of TransmissionBackgroundParams in [TransmissionBackgroundWGSL] (two vec4<f32> vectors = 32 bytes).
	TransmissionBackgroundUniformSize = 32
)

// ExtendedMaterialUniforms holds the 96-byte uniform payload encoded for struct ExtendedMaterialUniforms in [ExtendedMaterialBindingsWGSL].
type ExtendedMaterialUniforms struct {
	MaterialUniforms
	ClearcoatFactor          float32
	ClearcoatRoughnessFactor float32
	ClearcoatNormalScale     float32
	TransmissionFactor       float32
	IOR                      float32
	HasClearcoatNormalMap    bool
	HasClearcoatMap          bool
	HasTransmissionMap       bool
}

// ExtendedUniforms packs m's scalar parameters and texture-presence flags (including KHR_materials_clearcoat, KHR_materials_transmission, and IOR) into [ExtendedMaterialUniforms].
func (m Material) ExtendedUniforms() ExtendedMaterialUniforms {
	return ExtendedMaterialUniforms{
		MaterialUniforms:         m.Uniforms(),
		ClearcoatFactor:          m.ClearcoatFactor,
		ClearcoatRoughnessFactor: m.ClearcoatRoughnessFactor,
		ClearcoatNormalScale:     m.ClearcoatNormalScale,
		TransmissionFactor:       m.TransmissionFactor,
		IOR:                      m.EffectiveIOR(),
		HasClearcoatNormalMap:    m.ClearcoatNormalTexture != nil,
		HasClearcoatMap:          m.ClearcoatTexture != nil,
		HasTransmissionMap:       m.TransmissionTexture != nil,
	}
}

// AppendUniformData appends u's 96-byte little-endian uniform block to dst and returns the extended slice. It allocates only when dst lacks capacity for [ExtendedMaterialUniformSize] additional bytes.
func (u ExtendedMaterialUniforms) AppendUniformData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, ExtendedMaterialUniformSize)
	u.Encode(dst[start : start+ExtendedMaterialUniformSize])
	return dst
}

// Encode writes u into dst[:ExtendedMaterialUniformSize] in the 16-byte-aligned little-endian layout declared by [ExtendedMaterialBindingsWGSL]. It panics if len(dst) < ExtendedMaterialUniformSize.
func (u *ExtendedMaterialUniforms) Encode(dst []byte) []byte {
	if len(dst) < ExtendedMaterialUniformSize {
		panic("render3d: ExtendedMaterialUniforms.Encode: dst shorter than ExtendedMaterialUniformSize")
	}
	dst = dst[:ExtendedMaterialUniformSize]
	u.MaterialUniforms.Encode(dst[:MaterialUniformSize])
	putF32 := func(off int, v float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
	}
	boolF32 := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	ior := u.IOR
	if ior == 0 {
		ior = DefaultMaterialIOR
	}
	// vec4 ext_params (offset 64..79): clearcoat_factor, clearcoat_roughness_factor, clearcoat_normal_scale, transmission_factor
	putF32(64, u.ClearcoatFactor)
	putF32(68, u.ClearcoatRoughnessFactor)
	putF32(72, u.ClearcoatNormalScale)
	putF32(76, u.TransmissionFactor)
	// vec4 ext_flags (offset 80..95): ior, has_clearcoat_normal_map, has_clearcoat_map, has_transmission_map
	putF32(80, ior)
	putF32(84, boolF32(u.HasClearcoatNormalMap))
	putF32(88, boolF32(u.HasClearcoatMap))
	putF32(92, boolF32(u.HasTransmissionMap))
	return dst
}

// ExtendedMaterialBindGroupLayout creates the [MaterialGroup] bind group layout shared by every [GPUExtendedMaterial]: the uniform buffer at binding 0, the five base metallic-roughness texture/sampler pairs at bindings 1..10, and four extension texture/sampler pairs (clearcoat, clearcoat roughness, clearcoat normal, transmission) at bindings 11..18.
func ExtendedMaterialBindGroupLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.material.extended",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: MaterialUniformBinding, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: MaterialBaseColorTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: MaterialBaseColorSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: MaterialMetallicRoughnessTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: MaterialMetallicRoughnessSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: MaterialNormalTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: MaterialNormalSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: MaterialOcclusionTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: MaterialOcclusionSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: MaterialEmissiveTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: MaterialEmissiveSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: ExtendedMaterialClearcoatTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: ExtendedMaterialClearcoatSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: ExtendedMaterialClearcoatRoughnessTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: ExtendedMaterialClearcoatRoughnessSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: ExtendedMaterialClearcoatNormalTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: ExtendedMaterialClearcoatNormalSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: ExtendedMaterialTransmissionTextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: ExtendedMaterialTransmissionSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// ExtendedMaterialDefaults owns the extended [MaterialGroup] bind group layout (19 entries) and the shared 1x1 fallback textures and default linear-repeat sampler used by [NewGPUExtendedMaterial], while keeping plain [MaterialDefaults] unchanged for ordinary [GPUMaterial] pipelines.
type ExtendedMaterialDefaults struct {
	base      *MaterialDefaults
	extLayout *gpu.BindGroupLayout
}

// NewExtendedMaterialDefaults allocates an [ExtendedMaterialDefaults] on dev owned by cx, sharing the 1x1 fallback textures (white sRGB, white linear, flat normal, black) and linear-repeat sampler of [MaterialDefaults] alongside [ExtendedMaterialBindGroupLayout].
func NewExtendedMaterialDefaults(cx *core.Scope, dev *gpu.Device, opts MaterialDefaultsOptions) (*ExtendedMaterialDefaults, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	base, err := NewMaterialDefaults(cx, dev, opts)
	if err != nil {
		return nil, err
	}
	extLayout, err := ExtendedMaterialBindGroupLayout(cx, dev)
	if err != nil {
		_ = base.Close()
		return nil, err
	}
	return &ExtendedMaterialDefaults{
		base:      base,
		extLayout: extLayout,
	}, nil
}

// Layout returns the extended [MaterialGroup] bind group layout for [NewExtendedMaterialPipeline].
func (d *ExtendedMaterialDefaults) Layout() *gpu.BindGroupLayout {
	if d == nil {
		return nil
	}
	return d.extLayout
}

// Base returns the underlying lean [MaterialDefaults] for ordinary [GPUMaterial] and [GPUUnlitMaterial] instances.
func (d *ExtendedMaterialDefaults) Base() *MaterialDefaults {
	if d == nil {
		return nil
	}
	return d.base
}

// WhiteSRGB returns the shared 1x1 sRGB white fallback texture.
func (d *ExtendedMaterialDefaults) WhiteSRGB() *GPUTexture { return d.base.WhiteSRGB() }

// WhiteLinear returns the shared 1x1 linear white fallback texture.
func (d *ExtendedMaterialDefaults) WhiteLinear() *GPUTexture { return d.base.WhiteLinear() }

// FlatNormal returns the shared 1x1 linear flat tangent-space normal fallback texture.
func (d *ExtendedMaterialDefaults) FlatNormal() *GPUTexture { return d.base.FlatNormal() }

// Black returns the shared 1x1 sRGB black fallback texture.
func (d *ExtendedMaterialDefaults) Black() *GPUTexture { return d.base.Black() }

// Sampler returns the shared default linear-repeat sampler.
func (d *ExtendedMaterialDefaults) Sampler() *GPUSampler { return d.base.Sampler() }

// ByteSize returns the GPU texture memory held by the fallback textures.
func (d *ExtendedMaterialDefaults) ByteSize() uint64 { return d.base.ByteSize() }

// Valid reports whether the extended layout and shared fallback textures/sampler are open on a live device.
func (d *ExtendedMaterialDefaults) Valid() bool {
	return d != nil &&
		d.extLayout != nil && d.extLayout.Valid() &&
		d.base != nil &&
		d.base.WhiteSRGB().Valid() &&
		d.base.WhiteLinear().Valid() &&
		d.base.FlatNormal().Valid() &&
		d.base.Black().Valid() &&
		d.base.Sampler().Valid()
}

// Close releases the extended layout and underlying fallback resources. Safe to call on nil or more than once.
func (d *ExtendedMaterialDefaults) Close() error {
	if d == nil {
		return nil
	}
	var err error
	if d.extLayout != nil {
		err = errors.Join(err, d.extLayout.Close())
	}
	if d.base != nil {
		err = errors.Join(err, d.base.Close())
	}
	return err
}

// GPUExtendedMaterial is a scope-owned extended PBR material bound at [MaterialGroup] (group 1) supporting base metallic-roughness maps plus KHR_materials_emissive_strength (folded into EmissiveFactor), KHR_materials_clearcoat (factor, roughness, normal map), and KHR_materials_transmission (factor, map, IOR).
//
// Like [GPUMaterial], GPUExtendedMaterial does not cache a [*gpu.Device] pointer so retained materials remain updatable via [GPUExtendedMaterial.Update] after a device loss and restore. The opaque scene background texture for transmission is NOT baked into GPUExtendedMaterial; bind [TransmissionBackground] once per frame/pass instead.
type GPUExtendedMaterial struct {
	uniforms *gpu.Buffer
	group    *gpu.BindGroup
	material Material
	scratch  [ExtendedMaterialUniformSize]byte
}

// NewGPUExtendedMaterial validates m and creates its 96-byte uniform buffer and extended [MaterialGroup] bind group on dev, owned by cx. Omitted textures and samplers fall back to defaults.
func NewGPUExtendedMaterial(cx *core.Scope, dev *gpu.Device, defaults *ExtendedMaterialDefaults, m Material, opts MaterialUpload) (*GPUExtendedMaterial, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !defaults.Valid() {
		return nil, fmt.Errorf("%w: extended material defaults are not valid", ErrInvalidMaterial)
	}

	label := opts.Label
	if label == "" {
		label = "render3d.material.extended"
	}

	var initBytes [ExtendedMaterialUniformSize]byte
	u := m.ExtendedUniforms()
	u.Encode(initBytes[:])

	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniforms",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     initBytes[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	pickTex := func(t, fallback *GPUTexture) *gpu.Texture {
		if t != nil {
			return t.Texture()
		}
		return fallback.Texture()
	}
	pickSamp := func(s *GPUSampler) *gpu.Sampler {
		if s != nil {
			return s.Sampler()
		}
		return defaults.Sampler().Sampler()
	}

	bg, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: defaults.Layout(),
		Entries: []gpu.BindGroupEntry{
			{Binding: MaterialUniformBinding, Buffer: buf, Size: ExtendedMaterialUniformSize},
			{Binding: MaterialBaseColorTextureBinding, Texture: pickTex(m.BaseColorTexture, defaults.WhiteSRGB())},
			{Binding: MaterialBaseColorSamplerBinding, Sampler: pickSamp(m.BaseColorSampler)},
			{Binding: MaterialMetallicRoughnessTextureBinding, Texture: pickTex(m.MetallicRoughnessTexture, defaults.WhiteLinear())},
			{Binding: MaterialMetallicRoughnessSamplerBinding, Sampler: pickSamp(m.MetallicRoughnessSampler)},
			{Binding: MaterialNormalTextureBinding, Texture: pickTex(m.NormalTexture, defaults.FlatNormal())},
			{Binding: MaterialNormalSamplerBinding, Sampler: pickSamp(m.NormalSampler)},
			{Binding: MaterialOcclusionTextureBinding, Texture: pickTex(m.OcclusionTexture, defaults.WhiteLinear())},
			{Binding: MaterialOcclusionSamplerBinding, Sampler: pickSamp(m.OcclusionSampler)},
			{Binding: MaterialEmissiveTextureBinding, Texture: pickTex(m.EmissiveTexture, defaults.Black())},
			{Binding: MaterialEmissiveSamplerBinding, Sampler: pickSamp(m.EmissiveSampler)},
			{Binding: ExtendedMaterialClearcoatTextureBinding, Texture: pickTex(m.ClearcoatTexture, defaults.WhiteLinear())},
			{Binding: ExtendedMaterialClearcoatSamplerBinding, Sampler: pickSamp(m.ClearcoatSampler)},
			{Binding: ExtendedMaterialClearcoatRoughnessTextureBinding, Texture: pickTex(m.ClearcoatRoughnessTexture, defaults.WhiteLinear())},
			{Binding: ExtendedMaterialClearcoatRoughnessSamplerBinding, Sampler: pickSamp(m.ClearcoatRoughnessSampler)},
			{Binding: ExtendedMaterialClearcoatNormalTextureBinding, Texture: pickTex(m.ClearcoatNormalTexture, defaults.FlatNormal())},
			{Binding: ExtendedMaterialClearcoatNormalSamplerBinding, Sampler: pickSamp(m.ClearcoatNormalSampler)},
			{Binding: ExtendedMaterialTransmissionTextureBinding, Texture: pickTex(m.TransmissionTexture, defaults.WhiteLinear())},
			{Binding: ExtendedMaterialTransmissionSamplerBinding, Sampler: pickSamp(m.TransmissionSampler)},
		},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}

	return &GPUExtendedMaterial{
		uniforms: buf,
		group:    bg,
		material: m,
	}, nil
}

func sameExtendedMaterialBindings(a, b Material) bool {
	return sameMaterialBindings(a, b) &&
		a.ClearcoatTexture == b.ClearcoatTexture &&
		a.ClearcoatSampler == b.ClearcoatSampler &&
		a.ClearcoatRoughnessTexture == b.ClearcoatRoughnessTexture &&
		a.ClearcoatRoughnessSampler == b.ClearcoatRoughnessSampler &&
		a.ClearcoatNormalTexture == b.ClearcoatNormalTexture &&
		a.ClearcoatNormalSampler == b.ClearcoatNormalSampler &&
		a.TransmissionTexture == b.TransmissionTexture &&
		a.TransmissionSampler == b.TransmissionSampler
}

// Update validates m and writes its updated 96-byte uniform parameters into g's uniform buffer in place without rebuilding the bind group.
func (g *GPUExtendedMaterial) Update(dev *gpu.Device, m Material) error {
	if !g.Valid() {
		return fmt.Errorf("%w: extended material is not valid", ErrInvalidMaterial)
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if !sameExtendedMaterialBindings(g.material, m) {
		return fmt.Errorf("%w: Update cannot change texture or sampler bindings; create a new GPUExtendedMaterial", ErrInvalidMaterial)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	u := m.ExtendedUniforms()
	dev.WriteBuffer(g.uniforms, 0, u.Encode(g.scratch[:]))
	g.material = m
	return nil
}

// Bind sets g's bind group at [MaterialGroup] (group 1) on pass.
func (g *GPUExtendedMaterial) Bind(pass *gpu.RenderPass) {
	if !g.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(MaterialGroup, g.group)
}

// BindGroup returns the underlying [gpu.BindGroup], or nil for a nil receiver.
func (g *GPUExtendedMaterial) BindGroup() *gpu.BindGroup {
	if g == nil {
		return nil
	}
	return g.group
}

// UniformBuffer returns the underlying uniform [*gpu.Buffer], or nil for a nil receiver.
func (g *GPUExtendedMaterial) UniformBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.uniforms
}

// Material returns the latest [Material] parameters uploaded to g.
func (g *GPUExtendedMaterial) Material() Material {
	if g == nil {
		return Material{}
	}
	return g.material
}

// AlphaMode returns g's current [AlphaMode].
func (g *GPUExtendedMaterial) AlphaMode() AlphaMode {
	if g == nil {
		return AlphaOpaque
	}
	return g.material.AlphaMode
}

// DoubleSided reports whether g renders both front and back faces.
func (g *GPUExtendedMaterial) DoubleSided() bool {
	return g != nil && g.material.DoubleSided
}

// HasClearcoat reports whether g has a non-zero clearcoat factor.
func (g *GPUExtendedMaterial) HasClearcoat() bool {
	return g != nil && g.material.ClearcoatFactor > 0
}

// HasTransmission reports whether g has a non-zero transmission factor.
func (g *GPUExtendedMaterial) HasTransmission() bool {
	return g != nil && g.material.TransmissionFactor > 0
}

// ByteSize returns the GPU buffer memory owned by g's uniform buffer.
func (g *GPUExtendedMaterial) ByteSize() uint64 {
	if g == nil || g.uniforms == nil {
		return 0
	}
	return g.uniforms.Size()
}

// Valid reports whether g's uniform buffer and bind group are open on a live device.
func (g *GPUExtendedMaterial) Valid() bool {
	return g != nil && g.uniforms != nil && g.uniforms.Valid() && g.group != nil && g.group.Valid()
}

// Close releases g's bind group and uniform buffer. Safe to call on nil or more than once.
func (g *GPUExtendedMaterial) Close() error {
	if g == nil {
		return nil
	}
	var err error
	if g.group != nil {
		err = errors.Join(err, g.group.Close())
	}
	if g.uniforms != nil {
		err = errors.Join(err, g.uniforms.Close())
	}
	return err
}

// TransmissionBackgroundOptions configures [NewTransmissionBackground].
type TransmissionBackgroundOptions struct {
	// Label prefixes debug labels on GPU objects. Defaults to "render3d.transmission_bg".
	Label string
	// Retained keeps CPU shadow copies of the uniform buffer, fallback texture, and sampler across GPU device loss.
	Retained bool
	// SRGB indicates that the bound opaque scene color texture holds sRGB-encoded values in a non-sRGB texture format (such as FormatRGBA8Unorm), so the shader decodes it to linear space before tinting. Leave false for linear HDR targets (FormatRGBA16Float) or hardware-decoded sRGB textures (FormatRGBA8UnormSRGB).
	SRGB bool
}

// TransmissionBackground owns the per-frame / per-pass bind group exposing the opaque scene color texture to transmissive materials without baking the background texture into each material's bind group.
//
// In the shader, screen-space sample UVs are derived from frag_pos.xy / vec2<f32>(textureDimensions(transmission_bg_tex)), scaled and clamped by the 32-byte parameter uniform so standard resizes and [DynamicResolutionTarget] sub-viewports work seamlessly with one bind group update per frame or resize.
type TransmissionBackground struct {
	cx          *core.Scope
	label       string
	layout      *gpu.BindGroupLayout
	paramBuf    *gpu.Buffer
	sampler     *gpu.Sampler
	fallbackTex *GPUTexture
	group       *gpu.BindGroup
	boundTex    *gpu.Texture
	scratch     [TransmissionBackgroundUniformSize]byte
}

// NewTransmissionBackground allocates the per-frame transmission background bind group layout, uniform buffer, linear clamp-to-edge sampler, 1x1 black fallback texture, and initial bind group on dev, owned by cx.
func NewTransmissionBackground(cx *core.Scope, dev *gpu.Device, opts TransmissionBackgroundOptions) (*TransmissionBackground, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.transmission_bg"
	}
	tb := &TransmissionBackground{
		cx:    cx,
		label: label,
	}
	cleanup := func(err error) (*TransmissionBackground, error) {
		_ = tb.Close()
		return nil, err
	}

	var err error
	tb.layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return cleanup(err)
	}

	encodeTransmissionBGParams(tb.scratch[:], 1, 1, 0, 0, 1, 1, opts.SRGB)
	tb.paramBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".params",
		Size:     TransmissionBackgroundUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     tb.scratch[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	tb.sampler, err = dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        label + ".sampler",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	tb.fallbackTex, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm,
		Pixels: []byte{0, 0, 0, 255},
	}, TextureUpload{Label: label + ".fallback", Retained: opts.Retained})
	if err != nil {
		return cleanup(err)
	}

	tb.boundTex = tb.fallbackTex.Texture()
	tb.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: tb.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: tb.paramBuf, Size: TransmissionBackgroundUniformSize},
			{Binding: 1, Texture: tb.boundTex},
			{Binding: 2, Sampler: tb.sampler},
		},
	})
	if err != nil {
		return cleanup(err)
	}
	return tb, nil
}

func encodeTransmissionBGParams(dst []byte, scaleX, scaleY, minU, minV, maxU, maxV float32, srgb bool) {
	putF32 := func(off int, v float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
	}
	var srgbFlag float32
	if srgb {
		srgbFlag = 1
	}
	putF32(0, scaleX)
	putF32(4, scaleY)
	putF32(8, minU)
	putF32(12, minV)
	putF32(16, maxU)
	putF32(20, maxV)
	putF32(24, srgbFlag)
	putF32(28, 0)
}

// SetTexture binds the opaque scene color texture for the current frame/pass with full-texture UVs derived automatically from frag_pos.xy / vec2<f32>(textureDimensions(bg_tex)). Passing tex == nil binds the internal 1x1 black fallback. Only this single per-frame bind group is recreated when tex changes; no material bind groups are modified.
func (tb *TransmissionBackground) SetTexture(dev *gpu.Device, tex *gpu.Texture, srgb bool) error {
	return tb.setTextureWithParams(dev, tex, 1, 1, 0, 0, 1, 1, srgb)
}

// SetViewport binds tex and configures screen-to-background UV scaling and half-texel clamping for a transmissive pass rendering into (viewportW, viewportH) while sampling an opaque scene rendered into the top-left [0..activeW, 0..activeH] sub-rectangle of a (texW, texH) backing texture (such as a [DynamicResolutionTarget]).
func (tb *TransmissionBackground) SetViewport(dev *gpu.Device, tex *gpu.Texture, viewportW, viewportH, activeW, activeH, texW, texH int, srgb bool) error {
	if viewportW <= 0 || viewportH <= 0 || activeW <= 0 || activeH <= 0 || texW <= 0 || texH <= 0 {
		return fmt.Errorf("%w: transmission background dimensions must be positive", ErrInvalidMaterial)
	}
	scaleX := float32(activeW) / float32(viewportW)
	scaleY := float32(activeH) / float32(viewportH)
	minU := 0.5 / float32(texW)
	minV := 0.5 / float32(texH)
	maxU := (float32(activeW) - 0.5) / float32(texW)
	maxV := (float32(activeH) - 0.5) / float32(texH)
	if maxU < minU {
		maxU = minU
	}
	if maxV < minV {
		maxV = minV
	}
	return tb.setTextureWithParams(dev, tex, scaleX, scaleY, minU, minV, maxU, maxV, srgb)
}

// SetDynamicResolution configures tb to sample an opaque scene snapshot captured from target (or target.ColorTexture() when snapshot is nil) for a transmissive pass drawn either at target's active scaled viewport (viewportW == 0 || viewportH == 0) or at a custom viewport (such as native resolution).
func (tb *TransmissionBackground) SetDynamicResolution(dev *gpu.Device, target *DynamicResolutionTarget, snapshot *gpu.Texture, viewportW, viewportH int, srgb bool) error {
	if target == nil {
		return fmt.Errorf("%w: nil DynamicResolutionTarget", ErrInvalidMaterial)
	}
	tex := snapshot
	if tex == nil {
		tex = target.ColorTexture()
	}
	if tex == nil || !tex.Valid() {
		return fmt.Errorf("%w: dynamic resolution target has no valid color texture", ErrInvalidMaterial)
	}
	allocW, allocH := target.AllocatedSize()
	scaledW, scaledH := target.ScaledSize()
	if scaledW <= 0 || scaledH <= 0 {
		scaledW, scaledH = allocW, allocH
	}
	if viewportW <= 0 {
		viewportW = scaledW
	}
	if viewportH <= 0 {
		viewportH = scaledH
	}
	return tb.SetViewport(dev, tex, viewportW, viewportH, scaledW, scaledH, allocW, allocH, srgb)
}

func (tb *TransmissionBackground) setTextureWithParams(dev *gpu.Device, tex *gpu.Texture, scaleX, scaleY, minU, minV, maxU, maxV float32, srgb bool) error {
	if tb == nil || tb.layout == nil || !tb.layout.Valid() || tb.paramBuf == nil || !tb.paramBuf.Valid() || tb.sampler == nil || !tb.sampler.Valid() {
		return fmt.Errorf("%w: transmission background is not valid", ErrInvalidMaterial)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	targetTex := tex
	if targetTex == nil {
		if !tb.fallbackTex.Valid() {
			return fmt.Errorf("%w: transmission background fallback texture is not valid", ErrInvalidMaterial)
		}
		targetTex = tb.fallbackTex.Texture()
	} else if !targetTex.Valid() {
		return fmt.Errorf("%w: transmission background texture is not valid", ErrInvalidMaterial)
	}

	encodeTransmissionBGParams(tb.scratch[:], scaleX, scaleY, minU, minV, maxU, maxV, srgb)
	dev.WriteBuffer(tb.paramBuf, 0, tb.scratch[:])

	if targetTex != tb.boundTex || tb.group == nil || !tb.group.Valid() {
		newGroup, err := dev.CreateBindGroupDesc(tb.cx, gpu.BindGroupDesc{
			Label:  tb.label,
			Layout: tb.layout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: tb.paramBuf, Size: TransmissionBackgroundUniformSize},
				{Binding: 1, Texture: targetTex},
				{Binding: 2, Sampler: tb.sampler},
			},
		})
		if err != nil {
			return err
		}
		if tb.group != nil {
			_ = tb.group.Close()
		}
		tb.group = newGroup
		tb.boundTex = targetTex
	}
	return nil
}

// Layout returns the bind group layout for [ExtendedMaterialPipelineOptions].TransmissionLayout.
func (tb *TransmissionBackground) Layout() *gpu.BindGroupLayout {
	if tb == nil {
		return nil
	}
	return tb.layout
}

// BindGroup returns the current frame/pass bind group.
func (tb *TransmissionBackground) BindGroup() *gpu.BindGroup {
	if tb == nil {
		return nil
	}
	return tb.group
}

// Bind sets tb's bind group at [TransmissionBackgroundGroup] (group 3) on pass.
func (tb *TransmissionBackground) Bind(pass *gpu.RenderPass) {
	tb.BindAt(pass, TransmissionBackgroundGroup)
}

// BindAt sets tb's bind group at group on pass (use [MaterialTransmissionBackgroundGroup] = 2 when drawing with a default-light extended material pipeline without [ExtendedMaterialPipelineOptions].LightLayout, or [TransmissionBackgroundGroup] = 3 when paired with [GPULightBuffer]).
func (tb *TransmissionBackground) BindAt(pass *gpu.RenderPass, group uint32) {
	if !tb.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(group, tb.group)
}

// Valid reports whether tb's layout, uniform buffer, sampler, fallback texture, and bind group are live.
func (tb *TransmissionBackground) Valid() bool {
	return tb != nil &&
		tb.layout != nil && tb.layout.Valid() &&
		tb.paramBuf != nil && tb.paramBuf.Valid() &&
		tb.sampler != nil && tb.sampler.Valid() &&
		tb.fallbackTex.Valid() &&
		tb.group != nil && tb.group.Valid()
}

// Close releases all GPU resources owned by tb. Safe to call on nil or more than once.
func (tb *TransmissionBackground) Close() error {
	if tb == nil {
		return nil
	}
	var err error
	if tb.group != nil {
		err = errors.Join(err, tb.group.Close())
	}
	if tb.fallbackTex != nil {
		err = errors.Join(err, tb.fallbackTex.Close())
	}
	if tb.sampler != nil {
		err = errors.Join(err, tb.sampler.Close())
	}
	if tb.paramBuf != nil {
		err = errors.Join(err, tb.paramBuf.Close())
	}
	if tb.layout != nil {
		err = errors.Join(err, tb.layout.Close())
	}
	tb.boundTex = nil
	return err
}

// ExtendedMaterialPipelineOptions configures [NewExtendedMaterialPipeline].
type ExtendedMaterialPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing (and depth writes for opaque/alpha-mask modes) when non-zero.
	DepthFormat gpu.TextureFormat
	// AlphaMode selects blending and depth-write state, matching [MaterialPipelineOptions].AlphaMode.
	AlphaMode AlphaMode
	// DoubleSided disables backface culling ([gpu.CullNone]); when false, back faces are culled ([gpu.CullBack]).
	DoubleSided bool
	// LightLayout, when set, binds a [GPULightBuffer] at [ExtendedLightGroup] (group 2) so extended materials are shaded by the scene's [LightRig] (ambient + directional + point + spot lights) with full clearcoat and transmission evaluation. When nil, the built-in [DefaultMaterialLight] is used.
	LightLayout *gpu.BindGroupLayout
	// TransmissionLayout, when set, binds [TransmissionBackground] at [TransmissionBackgroundGroup] (group 3 when LightLayout is set) or [MaterialTransmissionBackgroundGroup] (group 2 when LightLayout is nil) so transmissive surfaces sample the opaque scene color pass.
	TransmissionLayout *gpu.BindGroupLayout
	// LinearOutput skips tone mapping and sRGB encoding and writes unclamped linear radiance for an [HDRTarget] resolved by [ToneMapPass] (and optional [BloomPass] for HDR emissives).
	LinearOutput bool
}

// NewExtendedMaterialPipeline compiles the extended PBR shader variant matching opts and creates a render pipeline taking [MeshVertexLayout] at vertex slot 0, camLayout at [CameraGroup] (0), and matLayout ([ExtendedMaterialDefaults.Layout]) at [MaterialGroup] (1), with optional [GPULightBuffer] at [ExtendedLightGroup] (2) and/or [TransmissionBackground] at group 2 or [TransmissionBackgroundGroup] (3).
func NewExtendedMaterialPipeline(cx *core.Scope, dev *gpu.Device, camLayout, matLayout *gpu.BindGroupLayout, opts ExtendedMaterialPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if matLayout == nil || !matLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid extended material bind group layout"}
	}
	if !validAlphaMode(opts.AlphaMode) {
		return nil, fmt.Errorf("%w: invalid AlphaMode %d", ErrInvalidMaterial, opts.AlphaMode)
	}
	if opts.LightLayout != nil && !opts.LightLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid light bind group layout"}
	}
	if opts.TransmissionLayout != nil && !opts.TransmissionLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid transmission background bind group layout"}
	}

	layouts := []*gpu.BindGroupLayout{CameraGroup: camLayout, MaterialGroup: matLayout}
	var src string
	switch {
	case opts.LightLayout != nil && opts.TransmissionLayout != nil:
		layouts = append(layouts, opts.LightLayout, opts.TransmissionLayout)
		src = ExtendedMaterialLitTransmissionWGSL
		if opts.LinearOutput {
			src = extendedMaterialLitTransmissionLinearWGSL
		}
	case opts.LightLayout != nil:
		layouts = append(layouts, opts.LightLayout)
		src = ExtendedMaterialLitWGSL
		if opts.LinearOutput {
			src = extendedMaterialLitLinearWGSL
		}
	case opts.TransmissionLayout != nil:
		layouts = append(layouts, opts.TransmissionLayout)
		src = ExtendedMaterialTransmissionWGSL
		if opts.LinearOutput {
			src = extendedMaterialTransmissionLinearWGSL
		}
	default:
		src = ExtendedMaterialWGSL
		if opts.LinearOutput {
			src = extendedMaterialLinearWGSL
		}
	}

	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return nil, err
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	target := gpu.ColorTargetState{Format: colorFmt, WriteMask: gpu.ColorWriteAll}
	if opts.AlphaMode == AlphaBlend {
		target.Blend = gpu.BlendAlpha()
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	label := opts.Label
	if label == "" {
		label = "render3d.material.extended." + opts.AlphaMode.String()
		if opts.LightLayout != nil {
			label += ".lit"
		}
		if opts.TransmissionLayout != nil {
			label += ".transmission"
		}
		if opts.LinearOutput {
			label += ".linear"
		}
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: cull},
		Targets:       []gpu.ColorTargetState{target},
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   opts.AlphaMode != AlphaBlend,
			DepthCompare: gpu.CompareLess,
		}
	}
	return dev.CreatePipeline(cx, desc)
}

// TransmissionBackgroundWGSL declares the per-frame transmission background bindings at [TransmissionBackgroundGroup] (group 3) and the helper ext_sample_transmission_bg(frag_xy, roughness) -> vec3<f32>. Use [TransmissionBackgroundWGSLForGroup] to place it at another bind group (such as [MaterialTransmissionBackgroundGroup] = 2).
const TransmissionBackgroundWGSL = `struct TransmissionBackgroundParams {
	uv_scale_min: vec4<f32>,
	uv_max_flags: vec4<f32>,
}

@group(3) @binding(0) var<uniform> transmission_bg: TransmissionBackgroundParams;
@group(3) @binding(1) var transmission_bg_tex: texture_2d<f32>;
@group(3) @binding(2) var transmission_bg_samp: sampler;

fn ext_srgb_to_linear_ch(c: f32) -> f32 {
	let x = clamp(c, 0.0, 1.0);
	if (x <= 0.04045) {
		return x / 12.92;
	}
	return pow((x + 0.055) / 1.055, 2.4);
}

fn ext_decode_bg(rgb: vec3<f32>) -> vec3<f32> {
	if (transmission_bg.uv_max_flags.z > 0.5) {
		return vec3<f32>(
			ext_srgb_to_linear_ch(rgb.x),
			ext_srgb_to_linear_ch(rgb.y),
			ext_srgb_to_linear_ch(rgb.z),
		);
	}
	return rgb;
}

fn ext_sample_transmission_bg(frag_xy: vec2<f32>, roughness: f32) -> vec3<f32> {
	let dims = textureDimensions(transmission_bg_tex);
	let bg_dims = vec2<f32>(max(f32(dims.x), 1.0), max(f32(dims.y), 1.0));
	let uv_min = transmission_bg.uv_scale_min.zw;
	let uv_max = transmission_bg.uv_max_flags.xy;
	let base_uv = clamp((frag_xy / bg_dims) * transmission_bg.uv_scale_min.xy, uv_min, uv_max);
	let c0 = ext_decode_bg(textureSampleLevel(transmission_bg_tex, transmission_bg_samp, base_uv, 0.0).rgb);
	if (roughness <= 0.08) {
		return c0;
	}
	let r2 = roughness * roughness;
	let step_uv = (vec2<f32>(1.0, 1.0) / bg_dims) * (r2 * 3.0);
	let c1 = ext_decode_bg(textureSampleLevel(transmission_bg_tex, transmission_bg_samp, clamp(base_uv + vec2<f32>( step_uv.x, 0.0), uv_min, uv_max), 0.0).rgb);
	let c2 = ext_decode_bg(textureSampleLevel(transmission_bg_tex, transmission_bg_samp, clamp(base_uv + vec2<f32>(-step_uv.x, 0.0), uv_min, uv_max), 0.0).rgb);
	let c3 = ext_decode_bg(textureSampleLevel(transmission_bg_tex, transmission_bg_samp, clamp(base_uv + vec2<f32>(0.0,  step_uv.y), uv_min, uv_max), 0.0).rgb);
	let c4 = ext_decode_bg(textureSampleLevel(transmission_bg_tex, transmission_bg_samp, clamp(base_uv + vec2<f32>(0.0, -step_uv.y), uv_min, uv_max), 0.0).rgb);
	return (c0 * 2.0 + c1 + c2 + c3 + c4) * (1.0 / 6.0);
}
`

// TransmissionBackgroundWGSLForGroup returns [TransmissionBackgroundWGSL] with its bindings placed at @group(group) instead of [TransmissionBackgroundGroup] (3).
func TransmissionBackgroundWGSLForGroup(group uint32) string {
	return strings.ReplaceAll(TransmissionBackgroundWGSL, "@group(3)", fmt.Sprintf("@group(%d)", group))
}

// ExtendedMaterialBindingsWGSL declares [MaterialGroup] (group 1) bindings 0..18 for [GPUExtendedMaterial] together with portable glTF 2.0 metallic-roughness + KHR_materials_clearcoat + KHR_materials_transmission shading helpers and the UV-aligned Cramer cotangent-frame normal perturbation helper (reused for both base and clearcoat normal maps).
const ExtendedMaterialBindingsWGSL = `struct ExtendedMaterialUniforms {
	base_color_factor: vec4<f32>,
	emissive_factor: vec4<f32>,
	params: vec4<f32>,
	flags: vec4<f32>,
	ext_params: vec4<f32>,
	ext_flags: vec4<f32>,
}

@group(1) @binding(0) var<uniform> material: ExtendedMaterialUniforms;
@group(1) @binding(1) var base_color_tex: texture_2d<f32>;
@group(1) @binding(2) var base_color_samp: sampler;
@group(1) @binding(3) var metallic_roughness_tex: texture_2d<f32>;
@group(1) @binding(4) var metallic_roughness_samp: sampler;
@group(1) @binding(5) var normal_tex: texture_2d<f32>;
@group(1) @binding(6) var normal_samp: sampler;
@group(1) @binding(7) var occlusion_tex: texture_2d<f32>;
@group(1) @binding(8) var occlusion_samp: sampler;
@group(1) @binding(9) var emissive_tex: texture_2d<f32>;
@group(1) @binding(10) var emissive_samp: sampler;
@group(1) @binding(11) var clearcoat_tex: texture_2d<f32>;
@group(1) @binding(12) var clearcoat_samp: sampler;
@group(1) @binding(13) var clearcoat_roughness_tex: texture_2d<f32>;
@group(1) @binding(14) var clearcoat_roughness_samp: sampler;
@group(1) @binding(15) var clearcoat_normal_tex: texture_2d<f32>;
@group(1) @binding(16) var clearcoat_normal_samp: sampler;
@group(1) @binding(17) var transmission_tex: texture_2d<f32>;
@group(1) @binding(18) var transmission_samp: sampler;

fn ext_pow5(x: f32) -> f32 {
	let c = clamp(x, 0.0, 1.0);
	let c2 = c * c;
	return c2 * c2 * c;
}

fn ext_dielectric_f0(ior: f32) -> f32 {
	if (ior <= 0.0) {
		return 0.04;
	}
	let r = (ior - 1.0) / (ior + 1.0);
	return r * r;
}

// ext_perturb_normal builds the same API-independent Cramer's-rule cotangent frame as material_perturb_normal (with deterministic orthonormal fallback when screen-space derivatives or UVs are degenerate) and is shared by both base and clearcoat normal maps.
fn ext_perturb_normal(
	geom_n: vec3<f32>,
	dp1: vec3<f32>,
	dp2: vec3<f32>,
	duv1: vec2<f32>,
	duv2: vec2<f32>,
	normal_sample: vec3<f32>,
	normal_scale: f32,
) -> vec3<f32> {
	var up_ref = vec3<f32>(0.0, 1.0, 0.0);
	if (geom_n.y > 0.99) {
		up_ref = vec3<f32>(0.0, 0.0, -1.0);
	} else if (geom_n.y < -0.99) {
		up_ref = vec3<f32>(0.0, 0.0, 1.0);
	}
	var t_vec = normalize(cross(up_ref, geom_n));
	var b_vec = cross(geom_n, t_vec);

	let det = duv1.x * duv2.y - duv1.y * duv2.x;
	if (abs(det) > 1e-10) {
		let inv_det = 1.0 / det;
		let t_raw = (dp1 * duv2.y - dp2 * duv1.y) * inv_det;
		let b_dv = (dp2 * duv1.x - dp1 * duv2.x) * inv_det;
		let t_ortho = t_raw - geom_n * dot(geom_n, t_raw);
		let b_raw = -b_dv;
		let b_ortho = b_raw - geom_n * dot(geom_n, b_raw);
		if (dot(t_ortho, t_ortho) > 1e-10 && dot(b_ortho, b_ortho) > 1e-10) {
			t_vec = normalize(t_ortho);
			b_vec = normalize(b_ortho);
		}
	}

	let n_unpacked = normal_sample * 2.0 - vec3<f32>(1.0, 1.0, 1.0);
	let n_ts = normalize(vec3<f32>(n_unpacked.x * normal_scale, n_unpacked.y * normal_scale, n_unpacked.z));
	return normalize(t_vec * n_ts.x + b_vec * n_ts.y + geom_n * n_ts.z);
}

fn ext_pbr_direct(
	n: vec3<f32>,
	v: vec3<f32>,
	l: vec3<f32>,
	light_color: vec3<f32>,
	base_color: vec3<f32>,
	metallic: f32,
	roughness: f32,
	f0_dielectric: f32,
	transmission: f32,
) -> vec3<f32> {
	let ndl = max(dot(n, l), 0.0);
	if (ndl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ndv = max(dot(n, v), 1e-4);
	let h_raw = v + l;
	let h_len2 = dot(h_raw, h_raw);
	let f0 = mix(vec3<f32>(f0_dielectric, f0_dielectric, f0_dielectric), base_color, metallic);
	let pi = 3.14159265359;
	if (h_len2 <= 1e-8) {
		let diff_only = (vec3<f32>(1.0, 1.0, 1.0) - f0) * ((1.0 - metallic) * (1.0 - transmission) / pi) * base_color;
		return diff_only * light_color * ndl;
	}
	let h = h_raw / sqrt(h_len2);
	let ndh = max(dot(n, h), 0.0);
	let vdh = max(dot(v, h), 0.0);

	let alpha = roughness * roughness;
	let alpha2 = alpha * alpha;
	let denom = ndh * ndh * (alpha2 - 1.0) + 1.0;
	let d_term = alpha2 / max(pi * denom * denom, 1e-7);

	let ggx_v = ndl * sqrt(ndv * ndv * (1.0 - alpha2) + alpha2);
	let ggx_l = ndv * sqrt(ndl * ndl * (1.0 - alpha2) + alpha2);
	let vis = 0.5 / max(ggx_v + ggx_l, 1e-5);

	let f_term = f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * ext_pow5(1.0 - vdh);
	let spec = f_term * (d_term * vis);
	let diff = (vec3<f32>(1.0, 1.0, 1.0) - f_term) * ((1.0 - metallic) * (1.0 - transmission) / pi) * base_color;
	return (diff + spec) * light_color * ndl;
}

fn ext_clearcoat_direct(
	nc: vec3<f32>,
	v: vec3<f32>,
	l: vec3<f32>,
	light_color: vec3<f32>,
	cc_roughness: f32,
) -> vec3<f32> {
	let ndl = max(dot(nc, l), 0.0);
	if (ndl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ndv = max(dot(nc, v), 1e-4);
	let h_raw = v + l;
	let h_len2 = dot(h_raw, h_raw);
	if (h_len2 <= 1e-8) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let h = h_raw / sqrt(h_len2);
	let ndh = max(dot(nc, h), 0.0);
	let vdh = max(dot(v, h), 0.0);

	let alpha = cc_roughness * cc_roughness;
	let alpha2 = alpha * alpha;
	let pi = 3.14159265359;
	let denom = ndh * ndh * (alpha2 - 1.0) + 1.0;
	let d_term = alpha2 / max(pi * denom * denom, 1e-7);

	let ggx_v = ndl * sqrt(ndv * ndv * (1.0 - alpha2) + alpha2);
	let ggx_l = ndv * sqrt(ndl * ndl * (1.0 - alpha2) + alpha2);
	let vis = 0.5 / max(ggx_v + ggx_l, 1e-5);

	let f_term = 0.04 + (1.0 - 0.04) * ext_pow5(1.0 - vdh);
	return light_color * (f_term * d_term * vis * ndl);
}

fn ext_pbr_ambient(
	n: vec3<f32>,
	v: vec3<f32>,
	sky_color: vec3<f32>,
	ground_color: vec3<f32>,
	base_color: vec3<f32>,
	metallic: f32,
	roughness: f32,
	occlusion: f32,
	f0_dielectric: f32,
	transmission: f32,
) -> vec3<f32> {
	let ndv = max(dot(n, v), 1e-4);
	let f0 = mix(vec3<f32>(f0_dielectric, f0_dielectric, f0_dielectric), base_color, metallic);
	let f_max = max(vec3<f32>(1.0 - roughness, 1.0 - roughness, 1.0 - roughness), f0);
	let f_amb = f0 + (f_max - f0) * ext_pow5(1.0 - ndv);

	let sky_t = clamp(n.y * 0.5 + 0.5, 0.0, 1.0);
	let amb_env = mix(ground_color, sky_color, sky_t);
	let amb_diff = (vec3<f32>(1.0, 1.0, 1.0) - f_amb) * ((1.0 - metallic) * (1.0 - transmission)) * base_color * amb_env;

	let refl = reflect(-v, n);
	let refl_t = clamp(refl.y * 0.5 + 0.5, 0.0, 1.0);
	let refl_env = mix(ground_color, sky_color, mix(refl_t, 0.5, roughness * roughness));
	let amb_spec = refl_env * f_amb * (1.0 - 0.5 * roughness);

	return (amb_diff + amb_spec) * occlusion;
}

fn ext_clearcoat_ambient(
	nc: vec3<f32>,
	v: vec3<f32>,
	sky_color: vec3<f32>,
	ground_color: vec3<f32>,
	cc_roughness: f32,
	occlusion: f32,
) -> vec3<f32> {
	let ndv = max(dot(nc, v), 1e-4);
	let f0 = 0.04;
	let f_max = max(1.0 - cc_roughness, f0);
	let f_amb = f0 + (f_max - f0) * ext_pow5(1.0 - ndv);

	let refl = reflect(-v, nc);
	let refl_t = clamp(refl.y * 0.5 + 0.5, 0.0, 1.0);
	let refl_env = mix(ground_color, sky_color, mix(refl_t, 0.5, cc_roughness * cc_roughness));
	return refl_env * (f_amb * (1.0 - 0.5 * cc_roughness) * occlusion);
}

fn ext_transmission_term(
	n: vec3<f32>,
	v: vec3<f32>,
	base_color: vec3<f32>,
	metallic: f32,
	f0_dielectric: f32,
	transmission: f32,
	bg_linear: vec3<f32>,
) -> vec3<f32> {
	if (transmission <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ndv = max(dot(n, v), 1e-4);
	let f0 = mix(vec3<f32>(f0_dielectric, f0_dielectric, f0_dielectric), base_color, metallic);
	let f_view = f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * ext_pow5(1.0 - ndv);
	return (vec3<f32>(1.0, 1.0, 1.0) - f_view) * ((1.0 - metallic) * transmission) * base_color * bg_linear;
}

fn ext_tonemap_srgb(linear_rgb: vec3<f32>) -> vec3<f32> {
	let c = max(linear_rgb, vec3<f32>(0.0, 0.0, 0.0));
	let mapped = c / (c + vec3<f32>(1.0, 1.0, 1.0));
	return pow(clamp(mapped, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0)), vec3<f32>(1.0 / 2.2, 1.0 / 2.2, 1.0 / 2.2));
}
`

const extFragmentPreludeWGSL = `
@fragment
fn fs_main(in: MaterialVertexOut, @builtin(front_facing) front_facing: bool) -> @location(0) vec4<f32> {
	// Sample all nine material maps and compute screen-space derivatives in uniform control flow before any alpha-mask discard.
	let base_sample = textureSample(base_color_tex, base_color_samp, in.uv);
	let mr_sample = textureSample(metallic_roughness_tex, metallic_roughness_samp, in.uv);
	let norm_sample = textureSample(normal_tex, normal_samp, in.uv);
	let occ_sample = textureSample(occlusion_tex, occlusion_samp, in.uv);
	let emis_sample = textureSample(emissive_tex, emissive_samp, in.uv);
	let cc_sample = textureSample(clearcoat_tex, clearcoat_samp, in.uv);
	let cc_rough_sample = textureSample(clearcoat_roughness_tex, clearcoat_roughness_samp, in.uv);
	let cc_norm_sample = textureSample(clearcoat_normal_tex, clearcoat_normal_samp, in.uv);
	let tr_sample = textureSample(transmission_tex, transmission_samp, in.uv);
	let dp1 = dpdx(in.world_pos);
	let dp2 = dpdy(in.world_pos);
	let duv1 = dpdx(in.uv);
	let duv2 = dpdy(in.uv);

	let base_color = material.base_color_factor * base_sample;
	if (material.flags.x > 0.5 && material.flags.x < 1.5 && base_color.a < material.params.w) {
		discard;
	}

	var geom_n = normalize(in.world_normal);
	if (material.flags.y > 0.5 && !front_facing) {
		geom_n = -geom_n;
	}

	var n = geom_n;
	if (material.flags.z > 0.5) {
		n = ext_perturb_normal(geom_n, dp1, dp2, duv1, duv2, norm_sample.xyz, material.emissive_factor.w);
	}

	var nc = geom_n;
	if (material.ext_flags.y > 0.5) {
		nc = ext_perturb_normal(geom_n, dp1, dp2, duv1, duv2, cc_norm_sample.xyz, material.ext_params.z);
	}

	let metallic = clamp(material.params.x * mr_sample.b, 0.0, 1.0);
	let roughness = clamp(material.params.y * mr_sample.g, 0.045, 1.0);
	let occlusion = mix(1.0, occ_sample.r, material.params.z);
	let f0_dielectric = ext_dielectric_f0(material.ext_flags.x);
	let clearcoat = clamp(material.ext_params.x * cc_sample.r, 0.0, 1.0);
	let cc_roughness = clamp(material.ext_params.y * cc_rough_sample.g, 0.045, 1.0);
	let transmission = clamp(material.ext_params.w * tr_sample.r, 0.0, 1.0);

	var emissive = material.emissive_factor.rgb;
	if (material.flags.w > 0.5) {
		emissive = emissive * emis_sample.rgb;
	}

	let v = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
`

const extDefaultLightingBodyWGSL = `
	let l = normalize(vec3<f32>(0.45, 0.72, 0.53));
	let light_color = vec3<f32>(2.6, 2.5, 2.35);
	let sky_color = vec3<f32>(0.34, 0.42, 0.54);
	let ground_color = vec3<f32>(0.14, 0.12, 0.10);

	let direct = ext_pbr_direct(n, v, l, light_color, base_color.rgb, metallic, roughness, f0_dielectric, transmission);
	let ambient = ext_pbr_ambient(n, v, sky_color, ground_color, base_color.rgb, metallic, roughness, occlusion, f0_dielectric, transmission);
	var linear_rgb = direct + ambient + emissive + transmitted;
	if (clearcoat > 0.0) {
		let ncdv = max(dot(nc, v), 1e-4);
		let f_cc_view = 0.04 + (1.0 - 0.04) * ext_pow5(1.0 - ncdv);
		let cc_direct = ext_clearcoat_direct(nc, v, l, light_color, cc_roughness);
		let cc_amb = ext_clearcoat_ambient(nc, v, sky_color, ground_color, cc_roughness, occlusion);
		linear_rgb = linear_rgb * (1.0 - clearcoat * f_cc_view) + (cc_direct + cc_amb) * clearcoat;
	}
`

const extRigLightingBodyWGSL = `
	var base_lit = lights.ambient.xyz * base_color.rgb * (1.0 - transmission) * occlusion + transmitted;
	var cc_lit = vec3<f32>(0.0, 0.0, 0.0);

	let dir_count = lights.counts.x;
	for (var di: u32 = 0u; di < 4u; di = di + 1u) {
		if (di >= dir_count) {
			break;
		}
		let dl = lights.directional[di];
		let l = dl.to_light.xyz;
		base_lit = base_lit + ext_pbr_direct(n, v, l, dl.color.xyz, base_color.rgb, metallic, roughness, f0_dielectric, transmission);
		if (clearcoat > 0.0) {
			cc_lit = cc_lit + ext_clearcoat_direct(nc, v, l, dl.color.xyz, cc_roughness);
		}
	}
	let pt_count = lights.counts.y;
	for (var pi_idx: u32 = 0u; pi_idx < 16u; pi_idx = pi_idx + 1u) {
		if (pi_idx >= pt_count) {
			break;
		}
		let pl = lights.point[pi_idx];
		let to_light = pl.position_range.xyz - in.world_pos;
		let dist_sq = dot(to_light, to_light);
		let atten = point_light_attenuation(dist_sq, pl.position_range.w);
		if (atten > 0.0 && dist_sq > 1e-12) {
			let l = to_light / sqrt(dist_sq);
			let rad = pl.color.xyz * atten;
			base_lit = base_lit + ext_pbr_direct(n, v, l, rad, base_color.rgb, metallic, roughness, f0_dielectric, transmission);
			if (clearcoat > 0.0) {
				cc_lit = cc_lit + ext_clearcoat_direct(nc, v, l, rad, cc_roughness);
			}
		}
	}
	let spot_count = lights.counts.z;
	for (var si: u32 = 0u; si < 8u; si = si + 1u) {
		if (si >= spot_count) {
			break;
		}
		let sl = lights.spot[si];
		let to_light = sl.position_range.xyz - in.world_pos;
		let dist_sq = dot(to_light, to_light);
		if (dist_sq > 1e-12) {
			let l = to_light / sqrt(dist_sq);
			let atten = point_light_attenuation(dist_sq, sl.position_range.w) * spot_cone_attenuation(sl.direction_scale.xyz, l, sl.direction_scale.w, sl.color_offset.w);
			if (atten > 0.0) {
				let rad = sl.color_offset.xyz * atten;
				base_lit = base_lit + ext_pbr_direct(n, v, l, rad, base_color.rgb, metallic, roughness, f0_dielectric, transmission);
				if (clearcoat > 0.0) {
					cc_lit = cc_lit + ext_clearcoat_direct(nc, v, l, rad, cc_roughness);
				}
			}
		}
	}

	var linear_rgb = base_lit;
	if (clearcoat > 0.0) {
		let ncdv = max(dot(nc, v), 1e-4);
		let f_cc_view = 0.04 + (1.0 - 0.04) * ext_pow5(1.0 - ncdv);
		let cc_amb = lights.ambient.xyz * (f_cc_view * occlusion);
		linear_rgb = base_lit * (1.0 - clearcoat * f_cc_view) + (cc_lit + cc_amb) * clearcoat;
	}
	linear_rgb = linear_rgb + emissive;
`

const extNoBGWGSL = `	let transmitted = vec3<f32>(0.0, 0.0, 0.0);
`

const extWithBGWGSL = `	let bg_linear = ext_sample_transmission_bg(in.clip_pos.xy, roughness);
	let transmitted = ext_transmission_term(n, v, base_color.rgb, metallic, f0_dielectric, transmission, bg_linear);
`

// ExtendedMaterialWGSL is the default-light extended PBR shader compiled by [NewExtendedMaterialPipeline] when neither LightLayout nor TransmissionLayout is set (groups 0: Camera, 1: ExtendedMaterial).
const ExtendedMaterialWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + materialVertexStageWGSL + extFragmentPreludeWGSL + extNoBGWGSL + extDefaultLightingBodyWGSL + `	let srgb_rgb = ext_tonemap_srgb(linear_rgb);
` + materialStagesTailWGSL

const extendedMaterialLinearWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + materialVertexStageWGSL + extFragmentPreludeWGSL + extNoBGWGSL + extDefaultLightingBodyWGSL + `	let srgb_rgb = linear_rgb;
` + materialStagesTailWGSL

// ExtendedMaterialTransmissionWGSL is the default-light extended PBR shader with a per-frame [TransmissionBackground] at [MaterialTransmissionBackgroundGroup] (group 2).
var ExtendedMaterialTransmissionWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + TransmissionBackgroundWGSLForGroup(MaterialTransmissionBackgroundGroup) + materialVertexStageWGSL + extFragmentPreludeWGSL + extWithBGWGSL + extDefaultLightingBodyWGSL + `	let srgb_rgb = ext_tonemap_srgb(linear_rgb);
` + materialStagesTailWGSL

var extendedMaterialTransmissionLinearWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + TransmissionBackgroundWGSLForGroup(MaterialTransmissionBackgroundGroup) + materialVertexStageWGSL + extFragmentPreludeWGSL + extWithBGWGSL + extDefaultLightingBodyWGSL + `	let srgb_rgb = linear_rgb;
` + materialStagesTailWGSL

// ExtendedMaterialLitWGSL is the [LightRig]-lit extended PBR shader compiled by [NewExtendedMaterialPipeline] when LightLayout is set (groups 0: Camera, 1: ExtendedMaterial, 2: LightRig).
var ExtendedMaterialLitWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + LightWGSLForGroup(ExtendedLightGroup) + materialVertexStageWGSL + extFragmentPreludeWGSL + extNoBGWGSL + extRigLightingBodyWGSL + `	let srgb_rgb = linear_to_srgb(linear_rgb);
` + materialStagesTailWGSL

var extendedMaterialLitLinearWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + LightWGSLForGroup(ExtendedLightGroup) + materialVertexStageWGSL + extFragmentPreludeWGSL + extNoBGWGSL + extRigLightingBodyWGSL + `	let srgb_rgb = linear_rgb;
` + materialStagesTailWGSL

// ExtendedMaterialLitTransmissionWGSL is the full [LightRig]-lit extended PBR shader with per-frame [TransmissionBackground] at [TransmissionBackgroundGroup] (groups 0: Camera, 1: ExtendedMaterial, 2: LightRig, 3: TransmissionBackground).
var ExtendedMaterialLitTransmissionWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + LightWGSLForGroup(ExtendedLightGroup) + TransmissionBackgroundWGSL + materialVertexStageWGSL + extFragmentPreludeWGSL + extWithBGWGSL + extRigLightingBodyWGSL + `	let srgb_rgb = linear_to_srgb(linear_rgb);
` + materialStagesTailWGSL

var extendedMaterialLitTransmissionLinearWGSL = CameraWGSL + ExtendedMaterialBindingsWGSL + LightWGSLForGroup(ExtendedLightGroup) + TransmissionBackgroundWGSL + materialVertexStageWGSL + extFragmentPreludeWGSL + extWithBGWGSL + extRigLightingBodyWGSL + `	let srgb_rgb = linear_rgb;
` + materialStagesTailWGSL
