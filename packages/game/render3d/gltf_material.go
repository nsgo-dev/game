package render3d

import (
	"errors"
	"fmt"
	"image"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// AlphaModeFromGLTF maps a glTF 2.0 alpha mode string ("", "OPAQUE", "MASK", or "BLEND")
// to [AlphaMode]. Unknown values return an error wrapping [ErrInvalidMaterial].
func AlphaModeFromGLTF(mode gltf.AlphaMode) (AlphaMode, error) {
	switch mode {
	case "", gltf.AlphaOpaque:
		return AlphaOpaque, nil
	case gltf.AlphaMask:
		return AlphaMask, nil
	case gltf.AlphaBlend:
		return AlphaBlend, nil
	default:
		return AlphaOpaque, fmt.Errorf("%w: unknown glTF alpha mode %q", ErrInvalidMaterial, mode)
	}
}

// SamplerOptionsFromGLTF converts a glTF 2.0 [gltf.Sampler] into [SamplerOptions]
// and reports whether the sampler uses mipmapped minification filtering.
//
// Per the glTF 2.0 specification, omitted wrap modes (0) default to [gpu.AddressRepeat],
// and omitted filter modes (0) default to linear filtering with linear mipmap filtering
// (wantMipmaps = true). Unknown filter or wrap constants return an error wrapping
// [ErrInvalidSampler].
func SamplerOptionsFromGLTF(s gltf.Sampler) (opts SamplerOptions, wantMipmaps bool, err error) {
	opts = SamplerLinearRepeat()
	opts.Label = s.Name

	switch s.MagFilter {
	case 0, gltf.FilterLinear:
		opts.MagFilter = gpu.FilterLinear
	case gltf.FilterNearest:
		opts.MagFilter = gpu.FilterNearest
	default:
		return SamplerOptions{}, false, fmt.Errorf("%w: unsupported glTF magFilter %d", ErrInvalidSampler, s.MagFilter)
	}

	switch s.MinFilter {
	case 0:
		opts.MinFilter = gpu.FilterLinear
		opts.MipmapFilter = gpu.FilterLinear
		wantMipmaps = true
	case gltf.FilterNearest:
		opts.MinFilter = gpu.FilterNearest
		opts.MipmapFilter = gpu.FilterNearest
		wantMipmaps = false
	case gltf.FilterLinear:
		opts.MinFilter = gpu.FilterLinear
		opts.MipmapFilter = gpu.FilterNearest
		wantMipmaps = false
	case gltf.FilterNearestMipmapNearest:
		opts.MinFilter = gpu.FilterNearest
		opts.MipmapFilter = gpu.FilterNearest
		wantMipmaps = true
	case gltf.FilterLinearMipmapNearest:
		opts.MinFilter = gpu.FilterLinear
		opts.MipmapFilter = gpu.FilterNearest
		wantMipmaps = true
	case gltf.FilterNearestMipmapLinear:
		opts.MinFilter = gpu.FilterNearest
		opts.MipmapFilter = gpu.FilterLinear
		wantMipmaps = true
	case gltf.FilterLinearMipmapLinear:
		opts.MinFilter = gpu.FilterLinear
		opts.MipmapFilter = gpu.FilterLinear
		wantMipmaps = true
	default:
		return SamplerOptions{}, false, fmt.Errorf("%w: unsupported glTF minFilter %d", ErrInvalidSampler, s.MinFilter)
	}

	addrU, err := addressModeFromGLTF(s.WrapS, "wrapS")
	if err != nil {
		return SamplerOptions{}, false, err
	}
	addrV, err := addressModeFromGLTF(s.WrapT, "wrapT")
	if err != nil {
		return SamplerOptions{}, false, err
	}
	opts.AddressU = addrU
	opts.AddressV = addrV
	opts.AddressW = gpu.AddressRepeat
	return opts, wantMipmaps, nil
}

func addressModeFromGLTF(wrap gltf.WrapMode, field string) (gpu.AddressMode, error) {
	switch wrap {
	case 0, gltf.WrapRepeat:
		return gpu.AddressRepeat, nil
	case gltf.WrapClampToEdge:
		return gpu.AddressClampToEdge, nil
	case gltf.WrapMirroredRepeat:
		return gpu.AddressMirrorRepeat, nil
	default:
		return gpu.AddressRepeat, fmt.Errorf("%w: unsupported glTF %s %d", ErrInvalidSampler, field, wrap)
	}
}

// GLTFMaterialUpload configures [UploadGLTFMaterials].
type GLTFMaterialUpload struct {
	// Label prefixes debug labels on uploaded textures, samplers, and GPU materials.
	Label string
	// Mipmaps forces CPU mipmap generation on every uploaded glTF texture when true
	// (unless DisableMipmaps is also set). When false, mipmaps are generated for any
	// texture sampled with a mipmapped or unspecified glTF minFilter.
	Mipmaps bool
	// DisableMipmaps disables mipmap generation even when glTF samplers request mipmaps.
	DisableMipmaps bool
	// MaxMipLevels caps generated mipmap chains when mipmaps are enabled; 0 builds the full chain.
	MaxMipLevels uint32
	// MaxAnisotropy sets anisotropic filtering on all-linear glTF samplers (0 or 1 disables it).
	// Samplers that use nearest min, mag, or mipmap filtering keep MaxAnisotropy = 1 to satisfy
	// WebGPU validation rules.
	MaxAnisotropy uint16
	// Retained keeps CPU shadow copies of every uploaded texture, sampler, and material uniform
	// buffer so the returned [GLTFMaterialSet] recovers automatically after a GPU device loss.
	// Pair with [MaterialDefaultsOptions.Retained] on defaults.
	Retained bool
}

// GLTFTextureSlot records the mapped GPU handles and glTF UV metadata for one
// texture slot on an uploaded glTF material.
type GLTFTextureSlot struct {
	Present      bool
	TextureIndex int
	ImageIndex   int
	SamplerIndex int
	TexCoord     int
	Transform    *gltf.TextureTransform
	SRGB         bool
	Texture      *GPUTexture
	Sampler      *GPUSampler
}

// EffectiveTexCoord returns Transform.TexCoord when KHR_texture_transform overrides
// the UV channel, otherwise TexCoord.
func (s GLTFTextureSlot) EffectiveTexCoord() int {
	if s.Transform != nil && s.Transform.TexCoord != nil {
		return *s.Transform.TexCoord
	}
	return s.TexCoord
}

// GLTFMaterialInfo holds the mapped CPU [Material], uploaded [GPUMaterial], and
// glTF-specific metadata (material name, KHR_materials_unlit flag, and per-slot
// UV/transform info) for one entry in [GLTFMaterialSet].
type GLTFMaterialInfo struct {
	Name              string
	Material          Material
	GPU               *GPUMaterial
	Unlit             bool
	BaseColor         GLTFTextureSlot
	MetallicRoughness GLTFTextureSlot
	Normal            GLTFTextureSlot
	Occlusion         GLTFTextureSlot
	Emissive          GLTFTextureSlot
}

type gltfTextureKey struct {
	imageIndex int
	srgb       bool
}

// GLTFMaterialSet owns every [GPUTexture], [GPUSampler], and [GPUMaterial]
// uploaded from a [*gltf.Document] by [UploadGLTFMaterials].
//
// Referenced glTF images are decoded once per image index and uploaded once per
// unique (imageIndex, sRGB) key: color slots (baseColorTexture, emissiveTexture)
// upload as sRGB ([gpu.FormatRGBA8UnormSRGB]) while data slots
// (metallicRoughnessTexture, normalTexture, occlusionTexture) upload as linear
// ([gpu.FormatRGBA8Unorm]). Packed textures shared across linear slots (such as
// occlusion + metallic-roughness) or across multiple materials are uploaded only
// once. Samplers are deduplicated by glTF sampler index.
type GLTFMaterialSet struct {
	doc             *gltf.Document
	materials       []GLTFMaterialInfo
	defaultMaterial *GPUMaterial
	textures        []*GPUTexture
	textureByKey    map[gltfTextureKey]*GPUTexture
	samplers        []*GPUSampler
	samplerByIndex  map[int]*GPUSampler
}

// UploadGLTFMaterials decodes and uploads all textures, samplers, and PBR
// metallic-roughness materials referenced by doc onto dev, owned by cx.
//
// Omitted texture and sampler slots fall back to defaults. A glTF 2.0 default
// material is always created and returned by [GLTFMaterialSet.DefaultMaterial]
// (and by [GLTFMaterialSet.Material](-1)) for primitives with Material == -1.
//
// If any material, sampler, or image fails validation, decoding, or GPU upload,
// every GPU resource allocated during the call is closed before returning the error.
func UploadGLTFMaterials(cx *core.Scope, dev *gpu.Device, defaults *MaterialDefaults, doc *gltf.Document, opts GLTFMaterialUpload) (*GLTFMaterialSet, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !defaults.Valid() {
		return nil, fmt.Errorf("%w: material defaults are not valid", ErrInvalidMaterial)
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: nil glTF document", ErrInvalidMaterial)
	}

	prefix := opts.Label
	if prefix == "" {
		prefix = "render3d.gltf"
	}

	type slotSpec struct {
		name string
		ref  *gltf.TextureRef
		srgb bool
	}
	type plannedSampler struct {
		index       int
		opts        SamplerOptions
		wantMipmaps bool
	}
	type plannedTexture struct {
		key     gltfTextureKey
		mipmaps bool
	}

	samplerOrder := make([]int, 0, len(doc.Samplers))
	samplerPlans := make(map[int]plannedSampler, len(doc.Samplers))
	texOrder := make([]gltfTextureKey, 0, len(doc.Images))
	texPlans := make(map[gltfTextureKey]*plannedTexture, len(doc.Images))

	// Pass 1: validate every material, sampler, and texture reference before
	// decoding images or creating any GPU resources.
	parsedMats := make([]Material, len(doc.Materials))
	for i, gm := range doc.Materials {
		mat, err := mapGLTFMaterialScalars(gm, i)
		if err != nil {
			return nil, err
		}
		if err := mat.Validate(); err != nil {
			return nil, fmt.Errorf("gltf material %d (%s): %w", i, gm.Name, err)
		}
		parsedMats[i] = mat

		slots := [5]slotSpec{
			{"baseColorTexture", gm.BaseColorTexture, true},
			{"metallicRoughnessTexture", gm.MetallicRoughnessTexture, false},
			{"normalTexture", gm.NormalTexture, false},
			{"occlusionTexture", gm.OcclusionTexture, false},
			{"emissiveTexture", gm.EmissiveTexture, true},
		}
		for _, slot := range slots {
			if slot.ref == nil {
				continue
			}
			texIdx := slot.ref.TextureIndex
			if texIdx < 0 || texIdx >= len(doc.Textures) {
				return nil, fmt.Errorf("%w: glTF material %d %s texture index %d out of range [0, %d)",
					ErrInvalidMaterial, i, slot.name, texIdx, len(doc.Textures))
			}
			tex := doc.Textures[texIdx]
			if tex.Source < 0 {
				// glTF 2.0 allows a texture with no image source; fall back to defaults.
				continue
			}
			if tex.Source >= len(doc.Images) {
				return nil, fmt.Errorf("%w: glTF material %d %s texture %d image index %d out of range [0, %d)",
					ErrInvalidMaterial, i, slot.name, texIdx, tex.Source, len(doc.Images))
			}

			wantMipmaps := true // default when texture omits a sampler (glTF 2.0 repeat + linear/mip)
			if tex.Sampler >= 0 {
				if tex.Sampler >= len(doc.Samplers) {
					return nil, fmt.Errorf("%w: glTF material %d %s texture %d sampler index %d out of range [0, %d)",
						ErrInvalidMaterial, i, slot.name, texIdx, tex.Sampler, len(doc.Samplers))
				}
				ps, ok := samplerPlans[tex.Sampler]
				if !ok {
					so, wm, err := SamplerOptionsFromGLTF(doc.Samplers[tex.Sampler])
					if err != nil {
						return nil, fmt.Errorf("gltf sampler %d: %w", tex.Sampler, err)
					}
					if opts.MaxAnisotropy > 1 &&
						so.MinFilter == gpu.FilterLinear &&
						so.MagFilter == gpu.FilterLinear &&
						so.MipmapFilter == gpu.FilterLinear {
						so.MaxAnisotropy = opts.MaxAnisotropy
					}
					so.Retained = opts.Retained
					if so.Label == "" {
						so.Label = fmt.Sprintf("%s.sampler.%d", prefix, tex.Sampler)
					} else {
						so.Label = fmt.Sprintf("%s.sampler.%d.%s", prefix, tex.Sampler, so.Label)
					}
					ps = plannedSampler{index: tex.Sampler, opts: so, wantMipmaps: wm}
					samplerPlans[tex.Sampler] = ps
					samplerOrder = append(samplerOrder, tex.Sampler)
				}
				wantMipmaps = ps.wantMipmaps
			} else if tex.Sampler < -1 {
				return nil, fmt.Errorf("%w: glTF material %d %s texture %d has invalid sampler index %d",
					ErrInvalidMaterial, i, slot.name, texIdx, tex.Sampler)
			}

			useMips := !opts.DisableMipmaps && (opts.Mipmaps || wantMipmaps)
			key := gltfTextureKey{imageIndex: tex.Source, srgb: slot.srgb}
			if tp, ok := texPlans[key]; ok {
				if useMips {
					tp.mipmaps = true
				}
			} else {
				texPlans[key] = &plannedTexture{key: key, mipmaps: useMips}
				texOrder = append(texOrder, key)
			}
		}
	}

	set := &GLTFMaterialSet{
		doc:            doc,
		materials:      make([]GLTFMaterialInfo, len(doc.Materials)),
		textureByKey:   make(map[gltfTextureKey]*GPUTexture, len(texOrder)),
		samplerByIndex: make(map[int]*GPUSampler, len(samplerOrder)),
	}
	cleanup := func(err error) (*GLTFMaterialSet, error) {
		_ = set.Close()
		return nil, err
	}

	// Pass 2: create deduplicated GPU samplers.
	for _, sampIdx := range samplerOrder {
		ps := samplerPlans[sampIdx]
		gs, err := NewGPUSampler(cx, dev, ps.opts)
		if err != nil {
			return cleanup(fmt.Errorf("gltf sampler %d: %w", sampIdx, err))
		}
		set.samplers = append(set.samplers, gs)
		set.samplerByIndex[sampIdx] = gs
	}

	// Pass 3: decode each referenced image once and upload unique (imageIndex, sRGB) textures.
	decoded := make(map[int]image.Image, len(texOrder))
	for _, key := range texOrder {
		tp := texPlans[key]
		img, ok := decoded[key.imageIndex]
		if !ok {
			var err error
			img, err = doc.DecodeImage(key.imageIndex)
			if err != nil {
				return cleanup(fmt.Errorf("%w: glTF image %d: %w", ErrInvalidTexture, key.imageIndex, err))
			}
			decoded[key.imageIndex] = img
		}
		space := "linear"
		if key.srgb {
			space = "srgb"
		}
		texLabel := fmt.Sprintf("%s.image.%d.%s", prefix, key.imageIndex, space)
		if name := doc.Images[key.imageIndex].Name; name != "" {
			texLabel = fmt.Sprintf("%s.%s", texLabel, name)
		}
		gt, err := UploadTexture(cx, dev, img, TextureUpload{
			Label:        texLabel,
			SRGB:         key.srgb,
			Mipmaps:      tp.mipmaps,
			MaxMipLevels: opts.MaxMipLevels,
			Retained:     opts.Retained,
		})
		if err != nil {
			return cleanup(fmt.Errorf("gltf image %d (%s): %w", key.imageIndex, space, err))
		}
		set.textures = append(set.textures, gt)
		set.textureByKey[key] = gt
	}

	// Pass 4: create the default glTF 2.0 material and every document material.
	var err error
	set.defaultMaterial, err = NewGPUMaterial(cx, dev, defaults, DefaultMaterial(), MaterialUpload{
		Label:    prefix + ".material.default",
		Retained: opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	resolveSlot := func(ref *gltf.TextureRef, srgb bool) GLTFTextureSlot {
		if ref == nil {
			return GLTFTextureSlot{TextureIndex: -1, ImageIndex: -1, SamplerIndex: -1, SRGB: srgb}
		}
		tex := doc.Textures[ref.TextureIndex]
		if tex.Source < 0 {
			return GLTFTextureSlot{TextureIndex: ref.TextureIndex, ImageIndex: -1, SamplerIndex: tex.Sampler, SRGB: srgb}
		}
		gt := set.textureByKey[gltfTextureKey{imageIndex: tex.Source, srgb: srgb}]
		var gs *GPUSampler
		if tex.Sampler >= 0 {
			gs = set.samplerByIndex[tex.Sampler]
		}
		var trCopy *gltf.TextureTransform
		if ref.Transform != nil {
			cp := *ref.Transform
			if cp.TexCoord != nil {
				tc := *cp.TexCoord
				cp.TexCoord = &tc
			}
			trCopy = &cp
		}
		effectiveSampler := gs
		if effectiveSampler == nil {
			effectiveSampler = defaults.Sampler()
		}
		return GLTFTextureSlot{
			Present:      true,
			TextureIndex: ref.TextureIndex,
			ImageIndex:   tex.Source,
			SamplerIndex: tex.Sampler,
			TexCoord:     ref.TexCoord,
			Transform:    trCopy,
			SRGB:         srgb,
			Texture:      gt,
			Sampler:      effectiveSampler,
		}
	}

	for i, gm := range doc.Materials {
		mat := parsedMats[i]

		bcSlot := resolveSlot(gm.BaseColorTexture, true)
		if bcSlot.Present {
			mat.BaseColorTexture = bcSlot.Texture
			if bcSlot.SamplerIndex >= 0 {
				mat.BaseColorSampler = set.samplerByIndex[bcSlot.SamplerIndex]
			}
		}

		mrSlot := resolveSlot(gm.MetallicRoughnessTexture, false)
		if mrSlot.Present {
			mat.MetallicRoughnessTexture = mrSlot.Texture
			if mrSlot.SamplerIndex >= 0 {
				mat.MetallicRoughnessSampler = set.samplerByIndex[mrSlot.SamplerIndex]
			}
		}

		normSlot := resolveSlot(gm.NormalTexture, false)
		if normSlot.Present {
			mat.NormalTexture = normSlot.Texture
			if normSlot.SamplerIndex >= 0 {
				mat.NormalSampler = set.samplerByIndex[normSlot.SamplerIndex]
			}
		}

		occSlot := resolveSlot(gm.OcclusionTexture, false)
		if occSlot.Present {
			mat.OcclusionTexture = occSlot.Texture
			if occSlot.SamplerIndex >= 0 {
				mat.OcclusionSampler = set.samplerByIndex[occSlot.SamplerIndex]
			}
		}

		emisSlot := resolveSlot(gm.EmissiveTexture, true)
		if emisSlot.Present {
			mat.EmissiveTexture = emisSlot.Texture
			if emisSlot.SamplerIndex >= 0 {
				mat.EmissiveSampler = set.samplerByIndex[emisSlot.SamplerIndex]
			}
		}

		matLabel := fmt.Sprintf("%s.material.%d", prefix, i)
		if gm.Name != "" {
			matLabel = fmt.Sprintf("%s.%s", matLabel, gm.Name)
		}
		gpuMat, err := NewGPUMaterial(cx, dev, defaults, mat, MaterialUpload{
			Label:    matLabel,
			Retained: opts.Retained,
		})
		if err != nil {
			return cleanup(fmt.Errorf("gltf material %d (%s): %w", i, gm.Name, err))
		}

		set.materials[i] = GLTFMaterialInfo{
			Name:              gm.Name,
			Material:          mat,
			GPU:               gpuMat,
			Unlit:             gm.Unlit,
			BaseColor:         bcSlot,
			MetallicRoughness: mrSlot,
			Normal:            normSlot,
			Occlusion:         occSlot,
			Emissive:          emisSlot,
		}
	}

	return set, nil
}

func mapGLTFMaterialScalars(gm gltf.Material, index int) (Material, error) {
	alphaMode, err := AlphaModeFromGLTF(gm.AlphaMode)
	if err != nil {
		return Material{}, fmt.Errorf("gltf material %d (%s): %w", index, gm.Name, err)
	}
	normalScale := float32(1)
	if gm.NormalTexture != nil {
		normalScale = gm.NormalTexture.Scale
	}
	occlusionStrength := float32(1)
	if gm.OcclusionTexture != nil {
		occlusionStrength = gm.OcclusionTexture.Strength
	}
	emissiveStrength, err := gm.EmissiveStrength()
	if err != nil {
		return Material{}, fmt.Errorf("%w: glTF material %d: %w", ErrInvalidMaterial, index, err)
	}
	emissive := [3]float32{
		gm.EmissiveFactor.X * emissiveStrength,
		gm.EmissiveFactor.Y * emissiveStrength,
		gm.EmissiveFactor.Z * emissiveStrength,
	}
	return Material{
		BaseColorFactor:   [4]float32{gm.BaseColorFactor.X, gm.BaseColorFactor.Y, gm.BaseColorFactor.Z, gm.BaseColorFactor.W},
		MetallicFactor:    gm.MetallicFactor,
		RoughnessFactor:   gm.RoughnessFactor,
		NormalScale:       normalScale,
		OcclusionStrength: occlusionStrength,
		EmissiveFactor:    emissive,
		AlphaMode:         alphaMode,
		AlphaCutoff:       gm.AlphaCutoff,
		DoubleSided:       gm.DoubleSided,
	}, nil
}

// Len returns the number of glTF materials uploaded from the document (excluding the
// built-in glTF default material returned by [GLTFMaterialSet.DefaultMaterial]).
func (s *GLTFMaterialSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.materials)
}

// DefaultMaterial returns the uploaded glTF 2.0 default [GPUMaterial] used when a
// primitive has Material == -1, or nil for a nil receiver.
func (s *GLTFMaterialSet) DefaultMaterial() *GPUMaterial {
	if s == nil {
		return nil
	}
	return s.defaultMaterial
}

// Material returns the uploaded [*GPUMaterial] for glTF material index, or
// [GLTFMaterialSet.DefaultMaterial] when index == -1 (the glTF sentinel for an
// unassigned material). Out-of-range indices and nil receivers return nil.
func (s *GLTFMaterialSet) Material(index int) *GPUMaterial {
	if s == nil {
		return nil
	}
	if index == -1 {
		return s.defaultMaterial
	}
	if index < 0 || index >= len(s.materials) {
		return nil
	}
	return s.materials[index].GPU
}

// Info returns the full [GLTFMaterialInfo] for glTF material index.
func (s *GLTFMaterialSet) Info(index int) (GLTFMaterialInfo, bool) {
	if s == nil || index < 0 || index >= len(s.materials) {
		return GLTFMaterialInfo{}, false
	}
	return s.materials[index], true
}

// MaterialForPrimitive resolves the active material for prim under the
// KHR_materials_variants variantIndex (falling back to prim.Material and then
// [GLTFMaterialSet.DefaultMaterial] when -1).
func (s *GLTFMaterialSet) MaterialForPrimitive(prim gltf.Primitive, variantIndex int) *GPUMaterial {
	if s == nil || s.doc == nil {
		return nil
	}
	return s.Material(s.doc.MaterialForPrimitive(prim, variantIndex))
}

// MaterialForVariant resolves the active material for prim under the named
// KHR_materials_variants variant (falling back to prim.Material and then
// [GLTFMaterialSet.DefaultMaterial] when -1).
func (s *GLTFMaterialSet) MaterialForVariant(prim gltf.Primitive, variantName string) *GPUMaterial {
	if s == nil || s.doc == nil {
		return nil
	}
	return s.Material(s.doc.MaterialForVariant(prim, variantName))
}

// InfoForPrimitive returns the [GLTFMaterialInfo] for prim under variantIndex,
// returning false when the resolved material index is -1 (default material) or out of range.
func (s *GLTFMaterialSet) InfoForPrimitive(prim gltf.Primitive, variantIndex int) (GLTFMaterialInfo, bool) {
	if s == nil || s.doc == nil {
		return GLTFMaterialInfo{}, false
	}
	return s.Info(s.doc.MaterialForPrimitive(prim, variantIndex))
}

// InfoForVariant returns the [GLTFMaterialInfo] for prim under variantName,
// returning false when the resolved material index is -1 (default material) or out of range.
func (s *GLTFMaterialSet) InfoForVariant(prim gltf.Primitive, variantName string) (GLTFMaterialInfo, bool) {
	if s == nil || s.doc == nil {
		return GLTFMaterialInfo{}, false
	}
	return s.Info(s.doc.MaterialForVariant(prim, variantName))
}

// Texture returns the deduplicated [*GPUTexture] uploaded for glTF imageIndex in
// the requested sRGB/linear color space, or nil if that pair was not uploaded.
func (s *GLTFMaterialSet) Texture(imageIndex int, srgb bool) *GPUTexture {
	if s == nil {
		return nil
	}
	return s.textureByKey[gltfTextureKey{imageIndex: imageIndex, srgb: srgb}]
}

// Sampler returns the deduplicated [*GPUSampler] created for glTF samplerIndex,
// or nil if that sampler was not referenced by any uploaded material.
func (s *GLTFMaterialSet) Sampler(samplerIndex int) *GPUSampler {
	if s == nil {
		return nil
	}
	return s.samplerByIndex[samplerIndex]
}

// TextureCount returns the number of unique GPU textures uploaded for the document.
func (s *GLTFMaterialSet) TextureCount() int {
	if s == nil {
		return 0
	}
	return len(s.textures)
}

// SamplerCount returns the number of unique GPU samplers created for the document.
func (s *GLTFMaterialSet) SamplerCount() int {
	if s == nil {
		return 0
	}
	return len(s.samplers)
}

// ByteSize returns the total GPU memory held by all uploaded textures and material
// uniform buffers in s (excluding shared [MaterialDefaults]).
func (s *GLTFMaterialSet) ByteSize() uint64 {
	if s == nil {
		return 0
	}
	var total uint64
	if s.defaultMaterial != nil {
		total += s.defaultMaterial.ByteSize()
	}
	for i := range s.materials {
		total += s.materials[i].GPU.ByteSize()
	}
	for _, t := range s.textures {
		total += t.ByteSize()
	}
	return total
}

// Valid reports whether every material, texture, and sampler owned by s is currently
// open on a live GPU device.
func (s *GLTFMaterialSet) Valid() bool {
	if s == nil || !s.defaultMaterial.Valid() {
		return false
	}
	for i := range s.materials {
		if !s.materials[i].GPU.Valid() {
			return false
		}
	}
	for _, t := range s.textures {
		if !t.Valid() {
			return false
		}
	}
	for _, samp := range s.samplers {
		if !samp.Valid() {
			return false
		}
	}
	return true
}

// Close releases every [GPUMaterial], [GPUSampler], and [GPUTexture] owned by s
// immediately instead of waiting for scope disposal. Safe to call on nil or more than once.
func (s *GLTFMaterialSet) Close() error {
	if s == nil {
		return nil
	}
	var err error
	for i := range s.materials {
		if s.materials[i].GPU != nil {
			err = errors.Join(err, s.materials[i].GPU.Close())
		}
	}
	if s.defaultMaterial != nil {
		err = errors.Join(err, s.defaultMaterial.Close())
	}
	for _, samp := range s.samplers {
		if samp != nil {
			err = errors.Join(err, samp.Close())
		}
	}
	for _, t := range s.textures {
		if t != nil {
			err = errors.Join(err, t.Close())
		}
	}
	return err
}
