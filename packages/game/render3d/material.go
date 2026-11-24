package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// MaterialGroup is the bind group index reserved for metallic-roughness materials alongside [CameraGroup] (group 0). [GPUMaterial.Bind] binds at this slot, and [MaterialBindingsWGSL] / [MaterialWGSL] declare every material binding there.
const MaterialGroup = 1

// MaterialUniformSize is the byte size of the 16-byte-aligned uniform block [MaterialUniforms.AppendUniformData] and [MaterialUniforms.Encode] write, matching struct MaterialUniforms in [MaterialBindingsWGSL].
const MaterialUniformSize = 64

// Binding indices inside [MaterialGroup] declared by [MaterialBindGroupLayout] and [MaterialBindingsWGSL].
const (
	MaterialUniformBinding                  uint32 = 0
	MaterialBaseColorTextureBinding         uint32 = 1
	MaterialBaseColorSamplerBinding         uint32 = 2
	MaterialMetallicRoughnessTextureBinding uint32 = 3
	MaterialMetallicRoughnessSamplerBinding uint32 = 4
	MaterialNormalTextureBinding            uint32 = 5
	MaterialNormalSamplerBinding            uint32 = 6
	MaterialOcclusionTextureBinding         uint32 = 7
	MaterialOcclusionSamplerBinding         uint32 = 8
	MaterialEmissiveTextureBinding          uint32 = 9
	MaterialEmissiveSamplerBinding          uint32 = 10
)

// minPBRRoughness clamps effective glTF metallic-roughness values away from zero so GGX microfacet specular evaluation remains finite and numerically stable even on mirror surfaces.
const minPBRRoughness float32 = 0.045

// ErrInvalidMaterial is wrapped by every error [Material.Validate], [NewGPUMaterial], [GPUMaterial.Update], and [NewMaterialPipeline] return for malformed material parameters or invalid texture/sampler handles.
var ErrInvalidMaterial = errors.New("render3d: invalid material")

// AlphaMode controls how a [Material]'s alpha channel is interpreted during rendering, matching the glTF 2.0 specification.
type AlphaMode uint8

const (
	// AlphaOpaque ignores alpha and outputs an opaque surface (alpha = 1.0) with depth writes enabled.
	AlphaOpaque AlphaMode = iota
	// AlphaMask discards fragments whose effective base-color alpha is less than [Material.AlphaCutoff], outputting alpha = 1.0 for surviving fragments with depth writes enabled.
	AlphaMask
	// AlphaBlend composites fragments over the color target using straight alpha blending ([gpu.BlendAlpha]) with depth writes disabled.
	AlphaBlend
)

// String returns the canonical lowercase name of m.
func (m AlphaMode) String() string {
	switch m {
	case AlphaOpaque:
		return "opaque"
	case AlphaMask:
		return "mask"
	case AlphaBlend:
		return "blend"
	default:
		return "unknown"
	}
}

// BlendMode converts m into the corresponding render-queue [BlendMode] for [DrawItem.Blend].
func (m AlphaMode) BlendMode() BlendMode {
	switch m {
	case AlphaMask:
		return BlendAlphaMask
	case AlphaBlend:
		return BlendAlphaBlend
	default:
		return BlendOpaque
	}
}

func validAlphaMode(m AlphaMode) bool {
	return m == AlphaOpaque || m == AlphaMask || m == AlphaBlend
}

// Material describes a glTF 2.0 metallic-roughness PBR surface ready to bind on a [gpu.Device] through [NewGPUMaterial] or evaluate on the CPU through [EvaluateMaterial].
//
// Start from [DefaultMaterial] for glTF 2.0 specification defaults (white base color, metallic 1, roughness 1, normal scale 1, occlusion strength 1, alpha cutoff 0.5). Any omitted texture or sampler falls back to the 1x1 neutral defaults in [MaterialDefaults] so every material shares one bind group layout ([MaterialGroup]) with zero pipeline permutations.
//
// Texture channel conventions match glTF 2.0:
//   - BaseColorTexture: sRGB RGBA8 color + linear alpha, multiplied by BaseColorFactor (linear RGBA).
//   - MetallicRoughnessTexture: linear RGBA8 where G = roughness and B = metallic, multiplied by RoughnessFactor and MetallicFactor.
//   - NormalTexture: linear RGBA8 tangent-space normal map (+X right/+U, +Y up/-V, +Z outward), with XY scaled by NormalScale.
//   - OcclusionTexture: linear R (or RGBA8) ambient occlusion factor blended by OcclusionStrength.
//   - EmissiveTexture: sRGB RGBA8 self-emission multiplied by EmissiveFactor (linear RGB).
type Material struct {
	BaseColorFactor  [4]float32
	BaseColorTexture *GPUTexture
	BaseColorSampler *GPUSampler

	MetallicFactor           float32
	RoughnessFactor          float32
	MetallicRoughnessTexture *GPUTexture
	MetallicRoughnessSampler *GPUSampler

	NormalTexture *GPUTexture
	NormalSampler *GPUSampler
	NormalScale   float32

	OcclusionTexture  *GPUTexture
	OcclusionSampler  *GPUSampler
	OcclusionStrength float32

	EmissiveFactor  [3]float32
	EmissiveTexture *GPUTexture
	EmissiveSampler *GPUSampler

	AlphaMode   AlphaMode
	AlphaCutoff float32
	DoubleSided bool
}

// MetallicRoughnessMaterial is an explicit alias for [Material] naming the glTF 2.0 metallic-roughness workflow.
type MetallicRoughnessMaterial = Material

// DefaultMaterial returns a [Material] populated with glTF 2.0 default factors: linear white base color [1, 1, 1, 1], metallic 1.0, roughness 1.0, normal scale 1.0, occlusion strength 1.0, emissive [0, 0, 0], [AlphaOpaque], and alpha cutoff 0.5.
func DefaultMaterial() Material {
	return Material{
		BaseColorFactor:   [4]float32{1, 1, 1, 1},
		MetallicFactor:    1,
		RoughnessFactor:   1,
		NormalScale:       1,
		OcclusionStrength: 1,
		AlphaMode:         AlphaOpaque,
		AlphaCutoff:       0.5,
	}
}

// Validate checks that every numeric factor is finite and within its valid glTF 2.0 range, that AlphaMode is recognized, and that every non-nil GPU texture or sampler handle is currently [GPUTexture.Valid] / [GPUSampler.Valid]. Every error wraps [ErrInvalidMaterial].
func (m Material) Validate() error {
	for i, c := range m.BaseColorFactor {
		if !finite(c) || c < 0 || c > 1 {
			return fmt.Errorf("%w: BaseColorFactor[%d] = %v must be finite in [0, 1]", ErrInvalidMaterial, i, c)
		}
	}
	if !finite(m.MetallicFactor) || m.MetallicFactor < 0 || m.MetallicFactor > 1 {
		return fmt.Errorf("%w: MetallicFactor = %v must be finite in [0, 1]", ErrInvalidMaterial, m.MetallicFactor)
	}
	if !finite(m.RoughnessFactor) || m.RoughnessFactor < 0 || m.RoughnessFactor > 1 {
		return fmt.Errorf("%w: RoughnessFactor = %v must be finite in [0, 1]", ErrInvalidMaterial, m.RoughnessFactor)
	}
	if !finite(m.NormalScale) || m.NormalScale < 0 {
		return fmt.Errorf("%w: NormalScale = %v must be finite and >= 0", ErrInvalidMaterial, m.NormalScale)
	}
	if !finite(m.OcclusionStrength) || m.OcclusionStrength < 0 || m.OcclusionStrength > 1 {
		return fmt.Errorf("%w: OcclusionStrength = %v must be finite in [0, 1]", ErrInvalidMaterial, m.OcclusionStrength)
	}
	for i, c := range m.EmissiveFactor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: EmissiveFactor[%d] = %v must be finite and >= 0", ErrInvalidMaterial, i, c)
		}
	}
	if !validAlphaMode(m.AlphaMode) {
		return fmt.Errorf("%w: invalid AlphaMode %d", ErrInvalidMaterial, m.AlphaMode)
	}
	if !finite(m.AlphaCutoff) || m.AlphaCutoff < 0 || m.AlphaCutoff > 1 {
		return fmt.Errorf("%w: AlphaCutoff = %v must be finite in [0, 1]", ErrInvalidMaterial, m.AlphaCutoff)
	}
	if m.BaseColorTexture != nil && !m.BaseColorTexture.Valid() {
		return fmt.Errorf("%w: BaseColorTexture is not valid", ErrInvalidMaterial)
	}
	if m.BaseColorSampler != nil && !m.BaseColorSampler.Valid() {
		return fmt.Errorf("%w: BaseColorSampler is not valid", ErrInvalidMaterial)
	}
	if m.MetallicRoughnessTexture != nil && !m.MetallicRoughnessTexture.Valid() {
		return fmt.Errorf("%w: MetallicRoughnessTexture is not valid", ErrInvalidMaterial)
	}
	if m.MetallicRoughnessSampler != nil && !m.MetallicRoughnessSampler.Valid() {
		return fmt.Errorf("%w: MetallicRoughnessSampler is not valid", ErrInvalidMaterial)
	}
	if m.NormalTexture != nil && !m.NormalTexture.Valid() {
		return fmt.Errorf("%w: NormalTexture is not valid", ErrInvalidMaterial)
	}
	if m.NormalSampler != nil && !m.NormalSampler.Valid() {
		return fmt.Errorf("%w: NormalSampler is not valid", ErrInvalidMaterial)
	}
	if m.OcclusionTexture != nil && !m.OcclusionTexture.Valid() {
		return fmt.Errorf("%w: OcclusionTexture is not valid", ErrInvalidMaterial)
	}
	if m.OcclusionSampler != nil && !m.OcclusionSampler.Valid() {
		return fmt.Errorf("%w: OcclusionSampler is not valid", ErrInvalidMaterial)
	}
	if m.EmissiveTexture != nil && !m.EmissiveTexture.Valid() {
		return fmt.Errorf("%w: EmissiveTexture is not valid", ErrInvalidMaterial)
	}
	if m.EmissiveSampler != nil && !m.EmissiveSampler.Valid() {
		return fmt.Errorf("%w: EmissiveSampler is not valid", ErrInvalidMaterial)
	}
	return nil
}

// MaterialUniforms holds the 64-byte uniform payload encoded for struct MaterialUniforms in [MaterialBindingsWGSL].
type MaterialUniforms struct {
	BaseColorFactor   [4]float32
	EmissiveFactor    [3]float32
	NormalScale       float32
	MetallicFactor    float32
	RoughnessFactor   float32
	OcclusionStrength float32
	AlphaCutoff       float32
	AlphaMode         AlphaMode
	DoubleSided       bool
	HasNormalMap      bool
	HasEmissiveMap    bool
}

// Uniforms packs m's scalar parameters and texture-presence flags into [MaterialUniforms].
func (m Material) Uniforms() MaterialUniforms {
	return MaterialUniforms{
		BaseColorFactor:   m.BaseColorFactor,
		EmissiveFactor:    m.EmissiveFactor,
		NormalScale:       m.NormalScale,
		MetallicFactor:    m.MetallicFactor,
		RoughnessFactor:   m.RoughnessFactor,
		OcclusionStrength: m.OcclusionStrength,
		AlphaCutoff:       m.AlphaCutoff,
		AlphaMode:         m.AlphaMode,
		DoubleSided:       m.DoubleSided,
		HasNormalMap:      m.NormalTexture != nil,
		HasEmissiveMap:    m.EmissiveTexture != nil,
	}
}

// AppendUniformData appends m's 64-byte little-endian uniform block to dst without allocating when dst has at least [MaterialUniformSize] bytes of spare capacity.
func (m Material) AppendUniformData(dst []byte) []byte {
	return m.Uniforms().AppendUniformData(dst)
}

// AppendUniformData appends u's 64-byte little-endian uniform block to dst and returns the extended slice. It allocates only when dst lacks capacity for [MaterialUniformSize] additional bytes.
func (u MaterialUniforms) AppendUniformData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, MaterialUniformSize)
	u.Encode(dst[start : start+MaterialUniformSize])
	return dst
}

// Encode writes u into dst[:MaterialUniformSize] in the 16-byte-aligned little-endian layout declared by [MaterialBindingsWGSL]. It panics if len(dst) < MaterialUniformSize.
func (u *MaterialUniforms) Encode(dst []byte) []byte {
	if len(dst) < MaterialUniformSize {
		panic("render3d: MaterialUniforms.Encode: dst shorter than MaterialUniformSize")
	}
	dst = dst[:MaterialUniformSize]
	putF32 := func(off int, v float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(v))
	}
	boolF32 := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	// vec4 base_color_factor (offset 0..15)
	putF32(0, u.BaseColorFactor[0])
	putF32(4, u.BaseColorFactor[1])
	putF32(8, u.BaseColorFactor[2])
	putF32(12, u.BaseColorFactor[3])
	// vec4 emissive_factor (offset 16..31): rgb = emissive, w = normal_scale
	putF32(16, u.EmissiveFactor[0])
	putF32(20, u.EmissiveFactor[1])
	putF32(24, u.EmissiveFactor[2])
	putF32(28, u.NormalScale)
	// vec4 params (offset 32..47): metallic, roughness, occlusion_strength, alpha_cutoff
	putF32(32, u.MetallicFactor)
	putF32(36, u.RoughnessFactor)
	putF32(40, u.OcclusionStrength)
	putF32(44, u.AlphaCutoff)
	// vec4 flags (offset 48..63): alpha_mode, double_sided, has_normal_map, has_emissive_map
	putF32(48, float32(u.AlphaMode))
	putF32(52, boolF32(u.DoubleSided))
	putF32(56, boolF32(u.HasNormalMap))
	putF32(60, boolF32(u.HasEmissiveMap))
	return dst
}

// MaterialBindGroupLayout creates the uniform [MaterialGroup] bind group layout shared by every [GPUMaterial]: one fragment uniform buffer at binding 0 followed by five sampled 2D texture/sampler pairs (base color, metallic-roughness, normal, occlusion, emissive) at bindings 1..10.
func MaterialBindGroupLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.material",
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
		},
	})
}

// MaterialDefaultsOptions configures [NewMaterialDefaults].
type MaterialDefaultsOptions struct {
	// Label prefixes the debug labels of the fallback textures, sampler, and bind group layout.
	Label string
	// Retained keeps CPU shadow copies of the fallback textures and sampler so retained [GPUMaterial] instances recover automatically after a GPU device loss.
	Retained bool
}

// MaterialDefaults owns the shared 1x1 fallback textures, default linear-repeat sampler, and [MaterialGroup] bind group layout used by [NewGPUMaterial] whenever a [Material] omits optional maps or samplers. Create one per [gpu.Device] and share it across all materials on that device.
type MaterialDefaults struct {
	layout      *gpu.BindGroupLayout
	whiteSRGB   *GPUTexture
	whiteLinear *GPUTexture
	flatNormal  *GPUTexture
	black       *GPUTexture
	sampler     *GPUSampler
}

// NewMaterialDefaults allocates the shared [MaterialGroup] layout, default linear-repeat sampler, and four 1x1 fallback textures on dev, owned by cx:
//   - WhiteSRGB: (255, 255, 255, 255) RGBA8 sRGB for missing base color maps.
//   - WhiteLinear: (255, 255, 255, 255) RGBA8 unorm for missing metallic-roughness and occlusion maps.
//   - FlatNormal: (128, 128, 255, 255) RGBA8 unorm for missing tangent-space normal maps.
//   - Black: (0, 0, 0, 255) RGBA8 sRGB for missing emissive maps.
func NewMaterialDefaults(cx *core.Scope, dev *gpu.Device, opts MaterialDefaultsOptions) (*MaterialDefaults, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	prefix := opts.Label
	if prefix == "" {
		prefix = "render3d.material.default"
	}
	d := &MaterialDefaults{}
	cleanup := func(err error) (*MaterialDefaults, error) {
		_ = d.Close()
		return nil, err
	}
	var err error
	d.layout, err = MaterialBindGroupLayout(cx, dev)
	if err != nil {
		return cleanup(err)
	}
	d.whiteSRGB, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8UnormSRGB,
		Pixels: []byte{255, 255, 255, 255},
	}, TextureUpload{Label: prefix + ".white_srgb", SRGB: true, Retained: opts.Retained})
	if err != nil {
		return cleanup(err)
	}
	d.whiteLinear, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm,
		Pixels: []byte{255, 255, 255, 255},
	}, TextureUpload{Label: prefix + ".white_linear", Retained: opts.Retained})
	if err != nil {
		return cleanup(err)
	}
	d.flatNormal, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm,
		Pixels: []byte{128, 128, 255, 255},
	}, TextureUpload{Label: prefix + ".flat_normal", Retained: opts.Retained})
	if err != nil {
		return cleanup(err)
	}
	d.black, err = UploadTexturePixels(cx, dev, PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8UnormSRGB,
		Pixels: []byte{0, 0, 0, 255},
	}, TextureUpload{Label: prefix + ".black", SRGB: true, Retained: opts.Retained})
	if err != nil {
		return cleanup(err)
	}
	sampOpts := SamplerLinearRepeat()
	sampOpts.Label = prefix + ".sampler"
	sampOpts.Retained = opts.Retained
	d.sampler, err = NewGPUSampler(cx, dev, sampOpts)
	if err != nil {
		return cleanup(err)
	}
	return d, nil
}

// Layout returns the [MaterialGroup] bind group layout shared by every material created with d.
func (d *MaterialDefaults) Layout() *gpu.BindGroupLayout {
	if d == nil {
		return nil
	}
	return d.layout
}

// WhiteSRGB returns the 1x1 sRGB white fallback texture (255, 255, 255, 255).
func (d *MaterialDefaults) WhiteSRGB() *GPUTexture {
	if d == nil {
		return nil
	}
	return d.whiteSRGB
}

// WhiteLinear returns the 1x1 linear white fallback texture (255, 255, 255, 255).
func (d *MaterialDefaults) WhiteLinear() *GPUTexture {
	if d == nil {
		return nil
	}
	return d.whiteLinear
}

// FlatNormal returns the 1x1 linear tangent-space flat normal fallback texture (128, 128, 255, 255).
func (d *MaterialDefaults) FlatNormal() *GPUTexture {
	if d == nil {
		return nil
	}
	return d.flatNormal
}

// Black returns the 1x1 sRGB black fallback texture (0, 0, 0, 255).
func (d *MaterialDefaults) Black() *GPUTexture {
	if d == nil {
		return nil
	}
	return d.black
}

// Sampler returns the default linear-repeat sampler used when a material slot omits a sampler.
func (d *MaterialDefaults) Sampler() *GPUSampler {
	if d == nil {
		return nil
	}
	return d.sampler
}

// ByteSize returns the total GPU memory held by the four 1x1 fallback textures.
func (d *MaterialDefaults) ByteSize() uint64 {
	if d == nil {
		return 0
	}
	return d.whiteSRGB.ByteSize() + d.whiteLinear.ByteSize() + d.flatNormal.ByteSize() + d.black.ByteSize()
}

// Valid reports whether all fallback resources and the bind group layout are open on a live device.
func (d *MaterialDefaults) Valid() bool {
	return d != nil &&
		d.layout != nil && d.layout.Valid() &&
		d.whiteSRGB.Valid() &&
		d.whiteLinear.Valid() &&
		d.flatNormal.Valid() &&
		d.black.Valid() &&
		d.sampler.Valid()
}

// Close releases the fallback textures, sampler, and bind group layout now rather than waiting for scope disposal. Safe to call on nil or more than once.
func (d *MaterialDefaults) Close() error {
	if d == nil {
		return nil
	}
	var err error
	if d.sampler != nil {
		err = errors.Join(err, d.sampler.Close())
	}
	if d.black != nil {
		err = errors.Join(err, d.black.Close())
	}
	if d.flatNormal != nil {
		err = errors.Join(err, d.flatNormal.Close())
	}
	if d.whiteLinear != nil {
		err = errors.Join(err, d.whiteLinear.Close())
	}
	if d.whiteSRGB != nil {
		err = errors.Join(err, d.whiteSRGB.Close())
	}
	if d.layout != nil {
		err = errors.Join(err, d.layout.Close())
	}
	return err
}

// MaterialUpload configures [NewGPUMaterial].
type MaterialUpload struct {
	// Label names the uniform buffer and bind group in GPU capture tools.
	Label string
	// Retained keeps a CPU shadow copy of the uniform buffer so NSGo restores it (and its bind group, when the defaults and any bound textures/samplers are also retained) across a GPU device loss.
	Retained bool
}

// GPUMaterial is a scope-owned metallic-roughness material bound to [MaterialGroup]: one uniform buffer storing [MaterialUniforms] plus one [gpu.BindGroup] wiring the uniform buffer and all five texture/sampler slots (using [MaterialDefaults] for omitted maps).
//
// Like [GPUTexture], GPUMaterial does not cache a [*gpu.Device] pointer so retained materials remain updatable through [GPUMaterial.Update] after a device loss and restore.
type GPUMaterial struct {
	uniforms *gpu.Buffer
	group    *gpu.BindGroup
	material Material
	scratch  [MaterialUniformSize]byte
}

// NewGPUMaterial validates m and creates its uniform buffer and [MaterialGroup] bind group on dev, owned by cx. Omitted textures and samplers fall back to defaults. Invalid parameters or handles return an error wrapping [ErrInvalidMaterial] before any GPU object is created.
func NewGPUMaterial(cx *core.Scope, dev *gpu.Device, defaults *MaterialDefaults, m Material, opts MaterialUpload) (*GPUMaterial, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !defaults.Valid() {
		return nil, fmt.Errorf("%w: material defaults are not valid", ErrInvalidMaterial)
	}

	label := opts.Label
	if label == "" {
		label = "render3d.material"
	}

	var initBytes [MaterialUniformSize]byte
	u := m.Uniforms()
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
			{Binding: MaterialUniformBinding, Buffer: buf, Size: MaterialUniformSize},
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
		},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}

	return &GPUMaterial{
		uniforms: buf,
		group:    bg,
		material: m,
	}, nil
}

// Update validates m and writes its updated uniform parameters into g's uniform buffer without reallocating the bind group.
//
// Texture and sampler pointers must match those g was created with (to rebind textures or samplers, create a new [GPUMaterial]). Pass the active [*gpu.Device] so retained materials continue updating cleanly after a device loss and restore.
func (g *GPUMaterial) Update(dev *gpu.Device, m Material) error {
	if !g.Valid() {
		return fmt.Errorf("%w: material is not valid", ErrInvalidMaterial)
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if !sameMaterialBindings(g.material, m) {
		return fmt.Errorf("%w: Update cannot change texture or sampler bindings; create a new GPUMaterial", ErrInvalidMaterial)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	u := m.Uniforms()
	dev.WriteBuffer(g.uniforms, 0, u.Encode(g.scratch[:]))
	g.material = m
	return nil
}

func sameMaterialBindings(a, b Material) bool {
	return a.BaseColorTexture == b.BaseColorTexture &&
		a.BaseColorSampler == b.BaseColorSampler &&
		a.MetallicRoughnessTexture == b.MetallicRoughnessTexture &&
		a.MetallicRoughnessSampler == b.MetallicRoughnessSampler &&
		a.NormalTexture == b.NormalTexture &&
		a.NormalSampler == b.NormalSampler &&
		a.OcclusionTexture == b.OcclusionTexture &&
		a.OcclusionSampler == b.OcclusionSampler &&
		a.EmissiveTexture == b.EmissiveTexture &&
		a.EmissiveSampler == b.EmissiveSampler
}

// Bind sets g's bind group at [MaterialGroup] (group 1) on pass. It records nothing when g is not [GPUMaterial.Valid], matching [GPUMesh.Bind].
func (g *GPUMaterial) Bind(pass *gpu.RenderPass) {
	if !g.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(MaterialGroup, g.group)
}

// BindGroup returns the underlying [gpu.BindGroup], or nil for a nil receiver.
func (g *GPUMaterial) BindGroup() *gpu.BindGroup {
	if g == nil {
		return nil
	}
	return g.group
}

// UniformBuffer returns the underlying uniform [*gpu.Buffer], or nil for a nil receiver.
func (g *GPUMaterial) UniformBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.uniforms
}

// Material returns the latest [Material] parameters uploaded to g, or the zero value for a nil receiver.
func (g *GPUMaterial) Material() Material {
	if g == nil {
		return Material{}
	}
	return g.material
}

// AlphaMode returns g's current [AlphaMode].
func (g *GPUMaterial) AlphaMode() AlphaMode {
	if g == nil {
		return AlphaOpaque
	}
	return g.material.AlphaMode
}

// DoubleSided reports whether g renders both front and back faces.
func (g *GPUMaterial) DoubleSided() bool {
	return g != nil && g.material.DoubleSided
}

// ByteSize returns the GPU buffer memory owned by g's uniform buffer (for [RenderStats] and [Budget] accounting). Bound textures are accounted for separately through [GPUTexture.ByteSize].
func (g *GPUMaterial) ByteSize() uint64 {
	if g == nil || g.uniforms == nil {
		return 0
	}
	return g.uniforms.Size()
}

// Valid reports whether g's uniform buffer and bind group are open on a live device.
func (g *GPUMaterial) Valid() bool {
	return g != nil && g.uniforms != nil && g.uniforms.Valid() && g.group != nil && g.group.Valid()
}

// Close releases g's bind group and uniform buffer now instead of waiting for scope disposal. Safe to call on nil or more than once.
func (g *GPUMaterial) Close() error {
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

// MaterialPipelineOptions configures [NewMaterialPipeline].
type MaterialPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing (and depth writes for opaque/alpha-mask modes) when non-zero.
	DepthFormat gpu.TextureFormat
	// AlphaMode selects blending and depth-write state: AlphaOpaque and AlphaMask write depth without color blending; AlphaBlend uses straight alpha blending ([gpu.BlendAlpha]) with depth writes disabled.
	AlphaMode AlphaMode
	// DoubleSided disables backface culling ([gpu.CullNone]); when false, back faces are culled ([gpu.CullBack]).
	DoubleSided bool
}

// NewMaterialPipeline compiles [MaterialWGSL] and builds a render pipeline using [MeshVertexLayout] at vertex slot 0, camLayout at [CameraGroup] (group 0), and matLayout at [MaterialGroup] (group 1).
func NewMaterialPipeline(cx *core.Scope, dev *gpu.Device, camLayout, matLayout *gpu.BindGroupLayout, opts MaterialPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if matLayout == nil || !matLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid material bind group layout"}
	}
	if !validAlphaMode(opts.AlphaMode) {
		return nil, fmt.Errorf("%w: invalid AlphaMode %d", ErrInvalidMaterial, opts.AlphaMode)
	}
	mod, err := gpu.CompileWGSL(MaterialWGSL)
	if err != nil {
		return nil, err
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	target := gpu.ColorTargetState{Format: colorFmt}
	if opts.AlphaMode == AlphaBlend {
		target.Blend = gpu.BlendAlpha()
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	label := opts.Label
	if label == "" {
		label = "render3d.material." + opts.AlphaMode.String()
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{CameraGroup: camLayout, MaterialGroup: matLayout},
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

// MaterialBindingsWGSL declares [MaterialGroup] (group 1) uniform/texture/sampler bindings together with portable glTF 2.0 metallic-roughness Cook-Torrance GGX shading and cotangent-frame normal-mapping helpers. Prepend [CameraWGSL] and MaterialBindingsWGSL to build custom lighting shaders that share [GPUMaterial].
const MaterialBindingsWGSL = `struct MaterialUniforms {
	base_color_factor: vec4<f32>,
	emissive_factor: vec4<f32>,
	params: vec4<f32>,
	flags: vec4<f32>,
}

@group(1) @binding(0) var<uniform> material: MaterialUniforms;
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

fn pbr_pow5(x: f32) -> f32 {
	let c = clamp(x, 0.0, 1.0);
	let c2 = c * c;
	return c2 * c2 * c;
}

// material_perturb_normal builds an API-independent tangent frame from precomputed screen-space derivatives (Cramer's rule on dp1/dp2/duv1/duv2) and falls back to a deterministic orthonormal frame from geom_n when derivatives or UVs are degenerate (such as meshes without UVs or software rendering).
fn material_perturb_normal(
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

// pbr_metallic_roughness_direct evaluates glTF 2.0 metallic-roughness Cook-Torrance GGX (Trowbridge-Reitz D, height-correlated Smith-GGX Visibility, Schlick Fresnel, energy-conserving Lambertian diffuse) for one directional light.
fn pbr_metallic_roughness_direct(
	n: vec3<f32>,
	v: vec3<f32>,
	l: vec3<f32>,
	light_color: vec3<f32>,
	base_color: vec3<f32>,
	metallic: f32,
	roughness: f32,
) -> vec3<f32> {
	let ndl = max(dot(n, l), 0.0);
	if (ndl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ndv = max(dot(n, v), 1e-4);
	let h = normalize(v + l);
	let ndh = max(dot(n, h), 0.0);
	let vdh = max(dot(v, h), 0.0);

	let alpha = roughness * roughness;
	let alpha2 = alpha * alpha;
	let f0 = mix(vec3<f32>(0.04, 0.04, 0.04), base_color, metallic);

	let pi = 3.14159265359;
	let denom = ndh * ndh * (alpha2 - 1.0) + 1.0;
	let d_term = alpha2 / max(pi * denom * denom, 1e-7);

	let ggx_v = ndl * sqrt(ndv * ndv * (1.0 - alpha2) + alpha2);
	let ggx_l = ndv * sqrt(ndl * ndl * (1.0 - alpha2) + alpha2);
	let vis = 0.5 / max(ggx_v + ggx_l, 1e-5);

	let f_term = f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * pbr_pow5(1.0 - vdh);
	let spec = f_term * (d_term * vis);
	let diff = (vec3<f32>(1.0, 1.0, 1.0) - f_term) * (1.0 - metallic) * base_color * (1.0 / pi);
	return (diff + spec) * light_color * ndl;
}

// pbr_metallic_roughness_ambient evaluates an analytic hemisphere diffuse + roughness-filtered specular ambient contribution scaled by ambient occlusion.
fn pbr_metallic_roughness_ambient(
	n: vec3<f32>,
	v: vec3<f32>,
	sky_color: vec3<f32>,
	ground_color: vec3<f32>,
	base_color: vec3<f32>,
	metallic: f32,
	roughness: f32,
	occlusion: f32,
) -> vec3<f32> {
	let ndv = max(dot(n, v), 1e-4);
	let f0 = mix(vec3<f32>(0.04, 0.04, 0.04), base_color, metallic);
	let f_max = max(vec3<f32>(1.0 - roughness, 1.0 - roughness, 1.0 - roughness), f0);
	let f_amb = f0 + (f_max - f0) * pbr_pow5(1.0 - ndv);

	let sky_t = clamp(n.y * 0.5 + 0.5, 0.0, 1.0);
	let amb_env = mix(ground_color, sky_color, sky_t);
	let amb_diff = (vec3<f32>(1.0, 1.0, 1.0) - f_amb) * (1.0 - metallic) * base_color * amb_env;

	let refl = reflect(-v, n);
	let refl_t = clamp(refl.y * 0.5 + 0.5, 0.0, 1.0);
	let refl_env = mix(ground_color, sky_color, mix(refl_t, 0.5, roughness * roughness));
	let amb_spec = refl_env * f_amb * (1.0 - 0.5 * roughness);

	return (amb_diff + amb_spec) * occlusion;
}

fn material_tonemap_srgb(linear_rgb: vec3<f32>) -> vec3<f32> {
	let c = max(linear_rgb, vec3<f32>(0.0, 0.0, 0.0));
	let mapped = c / (c + vec3<f32>(1.0, 1.0, 1.0));
	return pow(clamp(mapped, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0)), vec3<f32>(1.0 / 2.2, 1.0 / 2.2, 1.0 / 2.2));
}
`

// MaterialWGSL is the complete portable WGSL shader module compiled by [NewMaterialPipeline], combining [CameraWGSL] at [CameraGroup] (group 0), [MaterialBindingsWGSL] at [MaterialGroup] (group 1), [MeshVertexLayout] inputs, tangent-space normal mapping, glTF 2.0 metallic-roughness Cook-Torrance GGX shading under [DefaultMaterialLight], alpha masking/blending, and double-sided normal flipping.
const MaterialWGSL = CameraWGSL + MaterialBindingsWGSL + `
struct MaterialVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct MaterialVertexOut {
	@builtin(position) clip_pos: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: MaterialVertexIn) -> MaterialVertexOut {
	var out: MaterialVertexOut;
	out.clip_pos = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.world_pos = in.pos;
	out.world_normal = in.normal;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: MaterialVertexOut, @builtin(front_facing) front_facing: bool) -> @location(0) vec4<f32> {
	// Sample every map and compute screen-space derivatives in uniform control flow before any alpha-mask discard.
	let base_sample = textureSample(base_color_tex, base_color_samp, in.uv);
	let mr_sample = textureSample(metallic_roughness_tex, metallic_roughness_samp, in.uv);
	let norm_sample = textureSample(normal_tex, normal_samp, in.uv);
	let occ_sample = textureSample(occlusion_tex, occlusion_samp, in.uv);
	let emis_sample = textureSample(emissive_tex, emissive_samp, in.uv);
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
		n = material_perturb_normal(geom_n, dp1, dp2, duv1, duv2, norm_sample.xyz, material.emissive_factor.w);
	}

	let metallic = clamp(material.params.x * mr_sample.b, 0.0, 1.0);
	let roughness = clamp(material.params.y * mr_sample.g, 0.045, 1.0);
	let occlusion = mix(1.0, occ_sample.r, material.params.z);

	var emissive = material.emissive_factor.rgb;
	if (material.flags.w > 0.5) {
		emissive = emissive * emis_sample.rgb;
	}

	let v = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let l = normalize(vec3<f32>(0.45, 0.72, 0.53));
	let light_color = vec3<f32>(2.6, 2.5, 2.35);
	let sky_color = vec3<f32>(0.34, 0.42, 0.54);
	let ground_color = vec3<f32>(0.14, 0.12, 0.10);

	let direct = pbr_metallic_roughness_direct(n, v, l, light_color, base_color.rgb, metallic, roughness);
	let ambient = pbr_metallic_roughness_ambient(n, v, sky_color, ground_color, base_color.rgb, metallic, roughness, occlusion);
	let linear_rgb = direct + ambient + emissive;
	let srgb_rgb = material_tonemap_srgb(linear_rgb);

	var out_alpha = 1.0;
	if (material.flags.x > 1.5) {
		out_alpha = base_color.a;
	}
	return vec4<f32>(srgb_rgb, out_alpha);
}
`

// MaterialLight defines the directional key light and hemisphere ambient environment shared by [MaterialWGSL] and [EvaluateMaterial].
type MaterialLight struct {
	Direction   math3d.Vec3
	Color       math3d.Vec3
	SkyColor    math3d.Vec3
	GroundColor math3d.Vec3
}

// DefaultMaterialLight returns the directional key light and hemisphere ambient colors built into [MaterialWGSL].
func DefaultMaterialLight() MaterialLight {
	return MaterialLight{
		Direction:   math3d.Vec3{X: 0.45, Y: 0.72, Z: 0.53}.Norm(),
		Color:       math3d.Vec3{X: 2.6, Y: 2.5, Z: 2.35},
		SkyColor:    math3d.Vec3{X: 0.34, Y: 0.42, Z: 0.54},
		GroundColor: math3d.Vec3{X: 0.14, Y: 0.12, Z: 0.10},
	}
}

// MaterialEvalInput provides per-fragment inputs to [EvaluateMaterial], the pure-Go CPU reference evaluator matching [MaterialWGSL].
type MaterialEvalInput struct {
	Material Material

	BaseColorSample            [4]float32
	HasBaseColorSample         bool
	MetallicSample             float32 // glTF metallic-roughness texture B channel
	RoughnessSample            float32 // glTF metallic-roughness texture G channel
	HasMetallicRoughnessSample bool
	NormalSample               [3]float32 // UNORM [0, 1] tangent-space normal sample
	HasNormalSample            bool
	OcclusionSample            float32 // glTF occlusion texture R channel
	HasOcclusionSample         bool
	EmissiveSample             [3]float32 // linear RGB emissive texture sample
	HasEmissiveSample          bool

	Normal      math3d.Vec3   // geometric world-space normal
	Tangent     math3d.Vec3   // optional world-space tangent (+U); derived from Normal when zero
	Bitangent   math3d.Vec3   // optional world-space bitangent (+Y normal map / -V); derived from Normal x Tangent when zero
	ViewDir     math3d.Vec3   // vector from surface toward camera
	FrontFacing bool          // true for front-facing fragments; flips Normal when false and Material.DoubleSided is true
	Light       MaterialLight // zero value uses [DefaultMaterialLight]
}

// MaterialEvalResult holds the CPU reference shading output produced by [EvaluateMaterial].
type MaterialEvalResult struct {
	Linear    math3d.Vec4 // linear RGBA radiance before tone mapping and sRGB encoding
	SRGB      math3d.Vec4 // Reinhard tone-mapped and sRGB-encoded RGBA matching [MaterialWGSL]
	Normal    math3d.Vec3 // final world-space shading normal after normal mapping
	Metallic  float32     // effective clamped metallic in [0, 1]
	Roughness float32     // effective clamped roughness in [0.045, 1]
	Discarded bool        // true when AlphaMode == AlphaMask and effective alpha < AlphaCutoff
}

// EvaluateMaterial evaluates the glTF 2.0 metallic-roughness Cook-Torrance GGX shading model on the CPU, matching [MaterialWGSL] so tests and tools can verify GPU renders against an exact pure-Go reference.
func EvaluateMaterial(in MaterialEvalInput) MaterialEvalResult {
	m := in.Material
	baseSample := [4]float32{1, 1, 1, 1}
	if in.HasBaseColorSample {
		baseSample = in.BaseColorSample
	}
	baseColor := math3d.Vec3{
		X: m.BaseColorFactor[0] * baseSample[0],
		Y: m.BaseColorFactor[1] * baseSample[1],
		Z: m.BaseColorFactor[2] * baseSample[2],
	}
	baseAlpha := m.BaseColorFactor[3] * baseSample[3]

	if m.AlphaMode == AlphaMask && baseAlpha < m.AlphaCutoff {
		return MaterialEvalResult{Discarded: true}
	}

	geomN := in.Normal.Norm()
	if m.DoubleSided && !in.FrontFacing {
		geomN = geomN.Neg()
	}

	n := geomN
	if in.HasNormalSample || m.NormalTexture != nil {
		normSample := [3]float32{0.5, 0.5, 1}
		if in.HasNormalSample {
			normSample = in.NormalSample
		}
		tVec, bVec := resolveTangentBasis(geomN, in.Tangent, in.Bitangent)
		nx := (normSample[0]*2 - 1) * m.NormalScale
		ny := (normSample[1]*2 - 1) * m.NormalScale
		nz := normSample[2]*2 - 1
		nTS := math3d.Vec3{X: nx, Y: ny, Z: nz}.Norm()
		n = tVec.Scale(nTS.X).Add(bVec.Scale(nTS.Y)).Add(geomN.Scale(nTS.Z)).Norm()
	}

	metSample, roughSample := float32(1), float32(1)
	if in.HasMetallicRoughnessSample {
		metSample = in.MetallicSample
		roughSample = in.RoughnessSample
	}
	metallic := clampF32(m.MetallicFactor*metSample, 0, 1)
	roughness := clampF32(m.RoughnessFactor*roughSample, minPBRRoughness, 1)

	occSample := float32(1)
	if in.HasOcclusionSample {
		occSample = in.OcclusionSample
	}
	occlusion := 1*(1-m.OcclusionStrength) + occSample*m.OcclusionStrength

	emissive := math3d.Vec3{X: m.EmissiveFactor[0], Y: m.EmissiveFactor[1], Z: m.EmissiveFactor[2]}
	if in.HasEmissiveSample {
		emissive = math3d.Vec3{
			X: emissive.X * in.EmissiveSample[0],
			Y: emissive.Y * in.EmissiveSample[1],
			Z: emissive.Z * in.EmissiveSample[2],
		}
	}

	light := in.Light
	if light == (MaterialLight{}) {
		light = DefaultMaterialLight()
	}
	lDir := light.Direction.Norm()
	vDir := in.ViewDir.Norm()

	direct := evalPBRDirect(n, vDir, lDir, light.Color, baseColor, metallic, roughness)
	ambient := evalPBRAmbient(n, vDir, light.SkyColor, light.GroundColor, baseColor, metallic, roughness, occlusion)
	linearRGB := direct.Add(ambient).Add(emissive)
	srgbRGB := evalTonemapSRGB(linearRGB)

	outAlpha := float32(1)
	if m.AlphaMode == AlphaBlend {
		outAlpha = baseAlpha
	}
	return MaterialEvalResult{
		Linear:    math3d.Vec4{X: linearRGB.X, Y: linearRGB.Y, Z: linearRGB.Z, W: outAlpha},
		SRGB:      math3d.Vec4{X: srgbRGB.X, Y: srgbRGB.Y, Z: srgbRGB.Z, W: outAlpha},
		Normal:    n,
		Metallic:  metallic,
		Roughness: roughness,
	}
}

func resolveTangentBasis(geomN, tangent, bitangent math3d.Vec3) (t, b math3d.Vec3) {
	if tangent.LenSq() > 1e-10 {
		tOrtho := tangent.Sub(geomN.Scale(geomN.Dot(tangent)))
		if tOrtho.LenSq() > 1e-10 {
			t = tOrtho.Norm()
			if bitangent.LenSq() > 1e-10 {
				bOrtho := bitangent.Sub(geomN.Scale(geomN.Dot(bitangent)))
				if bOrtho.LenSq() > 1e-10 {
					b = bOrtho.Norm()
					return t, b
				}
			}
			b = geomN.Cross(t)
			return t, b
		}
	}
	upRef := math3d.Vec3{Y: 1}
	if geomN.Y > 0.99 {
		upRef = math3d.Vec3{Z: -1}
	} else if geomN.Y < -0.99 {
		upRef = math3d.Vec3{Z: 1}
	}
	t = upRef.Cross(geomN).Norm()
	b = geomN.Cross(t)
	return t, b
}

func evalPBRDirect(n, v, l, lightColor, baseColor math3d.Vec3, metallic, roughness float32) math3d.Vec3 {
	ndl := max(n.Dot(l), 0)
	if ndl <= 0 {
		return math3d.Vec3{}
	}
	ndv := max(n.Dot(v), 1e-4)
	h := v.Add(l).Norm()
	ndh := max(n.Dot(h), 0)
	vdh := max(v.Dot(h), 0)

	alpha := roughness * roughness
	alpha2 := alpha * alpha
	f0 := mixVec3(math3d.Vec3{X: 0.04, Y: 0.04, Z: 0.04}, baseColor, metallic)

	denom := ndh*ndh*(alpha2-1) + 1
	dTerm := alpha2 / max(float32(math.Pi)*denom*denom, 1e-7)

	ggxV := ndl * float32(math.Sqrt(float64(ndv*ndv*(1-alpha2)+alpha2)))
	ggxL := ndv * float32(math.Sqrt(float64(ndl*ndl*(1-alpha2)+alpha2)))
	vis := 0.5 / max(ggxV+ggxL, 1e-5)

	p5 := pow5F32(1 - vdh)
	fTerm := math3d.Vec3{
		X: f0.X + (1-f0.X)*p5,
		Y: f0.Y + (1-f0.Y)*p5,
		Z: f0.Z + (1-f0.Z)*p5,
	}
	spec := fTerm.Scale(dTerm * vis)
	invPi := float32(1.0 / math.Pi)
	diff := math3d.Vec3{
		X: (1 - fTerm.X) * (1 - metallic) * baseColor.X * invPi,
		Y: (1 - fTerm.Y) * (1 - metallic) * baseColor.Y * invPi,
		Z: (1 - fTerm.Z) * (1 - metallic) * baseColor.Z * invPi,
	}
	sum := diff.Add(spec)
	return math3d.Vec3{
		X: sum.X * lightColor.X * ndl,
		Y: sum.Y * lightColor.Y * ndl,
		Z: sum.Z * lightColor.Z * ndl,
	}
}

func evalPBRAmbient(n, v, skyColor, groundColor, baseColor math3d.Vec3, metallic, roughness, occlusion float32) math3d.Vec3 {
	ndv := max(n.Dot(v), 1e-4)
	f0 := mixVec3(math3d.Vec3{X: 0.04, Y: 0.04, Z: 0.04}, baseColor, metallic)
	fMax := math3d.Vec3{
		X: max(1-roughness, f0.X),
		Y: max(1-roughness, f0.Y),
		Z: max(1-roughness, f0.Z),
	}
	p5 := pow5F32(1 - ndv)
	fAmb := math3d.Vec3{
		X: f0.X + (fMax.X-f0.X)*p5,
		Y: f0.Y + (fMax.Y-f0.Y)*p5,
		Z: f0.Z + (fMax.Z-f0.Z)*p5,
	}

	skyT := clampF32(n.Y*0.5+0.5, 0, 1)
	ambEnv := mixVec3(groundColor, skyColor, skyT)
	ambDiff := math3d.Vec3{
		X: (1 - fAmb.X) * (1 - metallic) * baseColor.X * ambEnv.X,
		Y: (1 - fAmb.Y) * (1 - metallic) * baseColor.Y * ambEnv.Y,
		Z: (1 - fAmb.Z) * (1 - metallic) * baseColor.Z * ambEnv.Z,
	}

	// reflect(-v, n) = -v - 2*dot(-v, n)*n = 2*dot(v, n)*n - v
	refl := n.Scale(2 * n.Dot(v)).Sub(v)
	reflT := clampF32(refl.Y*0.5+0.5, 0, 1)
	reflMix := reflT*(1-roughness*roughness) + 0.5*(roughness*roughness)
	reflEnv := mixVec3(groundColor, skyColor, reflMix)
	specScale := 1 - 0.5*roughness
	ambSpec := math3d.Vec3{
		X: reflEnv.X * fAmb.X * specScale,
		Y: reflEnv.Y * fAmb.Y * specScale,
		Z: reflEnv.Z * fAmb.Z * specScale,
	}
	return ambDiff.Add(ambSpec).Scale(occlusion)
}

func evalTonemapSRGB(c math3d.Vec3) math3d.Vec3 {
	ch := func(v float32) float32 {
		if v <= 0 {
			return 0
		}
		mapped := clampF32(v/(v+1), 0, 1)
		return float32(math.Pow(float64(mapped), 1.0/2.2))
	}
	return math3d.Vec3{X: ch(c.X), Y: ch(c.Y), Z: ch(c.Z)}
}

func pow5F32(x float32) float32 {
	c := clampF32(x, 0, 1)
	c2 := c * c
	return c2 * c2 * c
}

func clampF32(x, lo, hi float32) float32 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func mixVec3(a, b math3d.Vec3, t float32) math3d.Vec3 {
	return math3d.Vec3{
		X: a.X*(1-t) + b.X*t,
		Y: a.Y*(1-t) + b.Y*t,
		Z: a.Z*(1-t) + b.Z*t,
	}
}
