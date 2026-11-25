package render3d

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Bind group slot, binding indices, limits, and uniform byte size for splat-map
// multi-layer PBR terrain materials ([TerrainMaterial], [GPUTerrainMaterial],
// [TerrainMaterialWGSL], and [NewTerrainMaterialPipeline]).
const (
	// TerrainMaterialGroup is the bind group index reserved for [GPUTerrainMaterial]
	// alongside [CameraGroup] (group 0) and [LightGroup] (group 1). It occupies
	// group 2 (matching [PBRDrawGroup]), leaving group 3 free for [ShadowGroup],
	// [CSMGroup], or [FogGroup].
	TerrainMaterialGroup = PBRDrawGroup

	// MaxTerrainLayers is the maximum number of PBR material layers blended by a
	// [TerrainMaterial] (4 RGBA channels per splat map × up to 2 splat maps).
	MaxTerrainLayers = 8

	// TerrainMaterialUniformSize is the byte size of struct TerrainMaterialUniforms
	// in [TerrainMaterialBindingsWGSL]: model (64) + normal_matrix (64) + params (16)
	// + triplanar_params (16) + 8 × TerrainLayerUniform (8 × 48 = 384) = 544 bytes.
	TerrainMaterialUniformSize = 64 + 64 + 16 + 16 + MaxTerrainLayers*48

	// Binding indices inside [TerrainMaterialGroup] declared by
	// [TerrainMaterialBindGroupLayout] and [TerrainMaterialBindingsWGSL].
	TerrainUniformBinding       uint32 = 0
	TerrainSplat0TextureBinding uint32 = 1
	TerrainSplat1TextureBinding uint32 = 2
	TerrainSplatSamplerBinding  uint32 = 3
	TerrainAlbedoArrayBinding   uint32 = 4
	TerrainNormalArrayBinding   uint32 = 5
	TerrainMRHeightArrayBinding uint32 = 6
	TerrainLayerSamplerBinding  uint32 = 7
)

// Default terrain layer and triplanar constants.
const (
	DefaultTerrainRoughness      float32 = 0.85
	DefaultTerrainTriplanarSlope float32 = 0.70
	DefaultTerrainTriplanarBlend float32 = 0.15
	DefaultTerrainTriplanarSharp float32 = 4.0
	DefaultTerrainTriplanarScale float32 = 0.25
)

// ErrInvalidTerrainMaterial is wrapped by validation and construction errors
// returned by [TerrainMaterial], [NewGPUTerrainMaterial], and
// [NewTerrainMaterialPipeline].
var ErrInvalidTerrainMaterial = errors.New("render3d: invalid terrain material")

// TerrainLayer describes one PBR surface layer inside a [TerrainMaterial].
//
// Texture channel conventions match glTF 2.0 plus an optional grayscale height
// map:
//   - AlbedoImage: sRGB color texture multiplied by BaseColorFactor (defaults to solid white).
//   - NormalImage: linear tangent-space normal map (+X right/+U, +Y up/-V, +Z out; defaults to flat (128,128,255)).
//   - MetallicRoughnessImage: linear texture where G = roughness and B = metallic (defaults to white).
//   - HeightImage: linear grayscale height map used for height-based blending (defaults to 0.5).
type TerrainLayer struct {
	Name                   string
	BaseColorFactor        [3]float32
	AlbedoImage            image.Image
	NormalImage            image.Image
	MetallicRoughnessImage image.Image
	HeightImage            image.Image
	RoughnessFactor        float32
	MetallicFactor         float32
	NormalScale            float32
	UVScale                math3d.Vec2
	HeightBias             float32
}

// DefaultTerrainLayer returns a [TerrainLayer] initialized with natural
// dielectric outdoor terrain defaults: white linear tint, roughness 0.85,
// metallic 0, normal scale 1, UV scale (1, 1), and height bias 0.
func DefaultTerrainLayer(name string) TerrainLayer {
	return TerrainLayer{
		Name:            name,
		BaseColorFactor: [3]float32{1, 1, 1},
		RoughnessFactor: DefaultTerrainRoughness,
		MetallicFactor:  0,
		NormalScale:     1,
		UVScale:         math3d.Vec2{X: 1, Y: 1},
		HeightBias:      0,
	}
}

func (l TerrainLayer) effectiveUVScale() math3d.Vec2 {
	sx := l.UVScale.X
	if sx == 0 {
		sx = 1
	}
	sy := l.UVScale.Y
	if sy == 0 {
		sy = 1
	}
	return math3d.Vec2{X: sx, Y: sy}
}

// TerrainTriplanar configures slope-driven triplanar projection on steep
// terrain faces to eliminate UV stretching on cliffs.
type TerrainTriplanar struct {
	// Enabled activates triplanar projection when abs(world_normal.y) drops below SlopeThreshold.
	Enabled bool
	// SlopeThreshold is the world normal Y threshold in [0, 1] below which triplanar mapping blends in (0 defaults to 0.70, ~45°).
	SlopeThreshold float32
	// SlopeBlend is the transition width in [0, 1] over which planar UVs cross-fade into triplanar UVs (0 defaults to 0.15).
	SlopeBlend float32
	// Sharpness controls triplanar axis blend sharpness (> 0; 0 defaults to 4.0).
	Sharpness float32
	// Scale is the world-space repeat rate before per-layer UVScale (> 0; 0 defaults to 0.25).
	Scale float32
}

// DefaultTerrainTriplanar returns recommended triplanar projection settings for
// cliffs steeper than ~45°.
func DefaultTerrainTriplanar() TerrainTriplanar {
	return TerrainTriplanar{
		Enabled:        true,
		SlopeThreshold: DefaultTerrainTriplanarSlope,
		SlopeBlend:     DefaultTerrainTriplanarBlend,
		Sharpness:      DefaultTerrainTriplanarSharp,
		Scale:          DefaultTerrainTriplanarScale,
	}
}

func (tp TerrainTriplanar) normalized() TerrainTriplanar {
	out := tp
	if out.SlopeThreshold == 0 {
		out.SlopeThreshold = DefaultTerrainTriplanarSlope
	}
	if out.SlopeBlend == 0 {
		out.SlopeBlend = DefaultTerrainTriplanarBlend
	}
	if out.Sharpness == 0 {
		out.Sharpness = DefaultTerrainTriplanarSharp
	}
	if out.Scale == 0 {
		out.Scale = DefaultTerrainTriplanarScale
	}
	return out
}

// TerrainMaterial describes a splat-map-driven multi-layer PBR terrain surface
// supporting up to 8 layers, optional height-based layer blending, and optional
// triplanar projection on steep slopes.
type TerrainMaterial struct {
	// Model transforms terrain vertices into world space; the zero matrix defaults to identity.
	Model math3d.Mat4
	// Layers defines 1 to 8 PBR terrain material layers in splat-channel order:
	// layers 0..3 read SplatMap0 (R, G, B, A); layers 4..7 read SplatMap1 (R, G, B, A).
	Layers []TerrainLayer
	// SplatMap0 provides RGBA weights for layers 0..3. When nil, defaults to 1x1 (255, 0, 0, 0).
	SplatMap0 image.Image
	// SplatMap1 provides RGBA weights for layers 4..7. Required when len(Layers) > 4 unless nil defaults to 1x1 (0, 0, 0, 0).
	SplatMap1 image.Image
	// HeightBlendContrast controls height-based layer blending in [0, 1]: 0 uses
	// linear splat blending; higher values produce sharper transitions driven by
	// each layer's HeightImage and HeightBias.
	HeightBlendContrast float32
	// Triplanar configures slope-driven triplanar projection on steep terrain faces.
	Triplanar TerrainTriplanar
	// LayerTextureSize optionally sets the square texel width and height of the
	// GPU 2D texture arrays backing the layer maps. When 0, the size is inferred
	// from the first non-nil layer image (or 1 when all layers are solid colors).
	// Any layer image with different dimensions is bilinearly resampled to match.
	LayerTextureSize int
}

// Validate checks that every field of m is finite and within valid ranges.
// Every error wraps [ErrInvalidTerrainMaterial].
func (m TerrainMaterial) Validate() error {
	if len(m.Layers) == 0 || len(m.Layers) > MaxTerrainLayers {
		return fmt.Errorf("%w: layer count %d must be in [1, %d]", ErrInvalidTerrainMaterial, len(m.Layers), MaxTerrainLayers)
	}
	if m.Model != (math3d.Mat4{}) && !finiteMat4(m.Model) {
		return fmt.Errorf("%w: Model matrix contains non-finite values", ErrInvalidTerrainMaterial)
	}
	if !finite(m.HeightBlendContrast) || m.HeightBlendContrast < 0 || m.HeightBlendContrast > 1 {
		return fmt.Errorf("%w: HeightBlendContrast = %v must be finite in [0, 1]", ErrInvalidTerrainMaterial, m.HeightBlendContrast)
	}
	if !finite(m.Triplanar.SlopeThreshold) || m.Triplanar.SlopeThreshold < 0 || m.Triplanar.SlopeThreshold > 1 {
		return fmt.Errorf("%w: Triplanar.SlopeThreshold = %v must be finite in [0, 1]", ErrInvalidTerrainMaterial, m.Triplanar.SlopeThreshold)
	}
	if !finite(m.Triplanar.SlopeBlend) || m.Triplanar.SlopeBlend < 0 || m.Triplanar.SlopeBlend > 1 {
		return fmt.Errorf("%w: Triplanar.SlopeBlend = %v must be finite in [0, 1]", ErrInvalidTerrainMaterial, m.Triplanar.SlopeBlend)
	}
	if m.Triplanar.Sharpness != 0 && (!finite(m.Triplanar.Sharpness) || m.Triplanar.Sharpness <= 0) {
		return fmt.Errorf("%w: Triplanar.Sharpness = %v must be finite and > 0", ErrInvalidTerrainMaterial, m.Triplanar.Sharpness)
	}
	if m.Triplanar.Scale != 0 && (!finite(m.Triplanar.Scale) || m.Triplanar.Scale <= 0) {
		return fmt.Errorf("%w: Triplanar.Scale = %v must be finite and > 0", ErrInvalidTerrainMaterial, m.Triplanar.Scale)
	}
	if m.LayerTextureSize < 0 {
		return fmt.Errorf("%w: LayerTextureSize = %d must be >= 0", ErrInvalidTerrainMaterial, m.LayerTextureSize)
	}
	if m.SplatMap0 != nil {
		b := m.SplatMap0.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			return fmt.Errorf("%w: SplatMap0 has empty bounds %v", ErrInvalidTerrainMaterial, b)
		}
	}
	if m.SplatMap1 != nil {
		b := m.SplatMap1.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			return fmt.Errorf("%w: SplatMap1 has empty bounds %v", ErrInvalidTerrainMaterial, b)
		}
	}
	for i, l := range m.Layers {
		for c, v := range l.BaseColorFactor {
			if !finite(v) || v < 0 || v > 1 {
				return fmt.Errorf("%w: Layers[%d].BaseColorFactor[%d] = %v must be finite in [0, 1]", ErrInvalidTerrainMaterial, i, c, v)
			}
		}
		if !finite(l.RoughnessFactor) || l.RoughnessFactor < 0 || l.RoughnessFactor > 1 {
			return fmt.Errorf("%w: Layers[%d].RoughnessFactor = %v must be finite in [0, 1]", ErrInvalidTerrainMaterial, i, l.RoughnessFactor)
		}
		if !finite(l.MetallicFactor) || l.MetallicFactor < 0 || l.MetallicFactor > 1 {
			return fmt.Errorf("%w: Layers[%d].MetallicFactor = %v must be finite in [0, 1]", ErrInvalidTerrainMaterial, i, l.MetallicFactor)
		}
		if !finite(l.NormalScale) {
			return fmt.Errorf("%w: Layers[%d].NormalScale = %v must be finite", ErrInvalidTerrainMaterial, i, l.NormalScale)
		}
		if l.UVScale.X != 0 && (!finite(l.UVScale.X) || l.UVScale.X <= 0) {
			return fmt.Errorf("%w: Layers[%d].UVScale.X = %v must be finite and > 0", ErrInvalidTerrainMaterial, i, l.UVScale.X)
		}
		if l.UVScale.Y != 0 && (!finite(l.UVScale.Y) || l.UVScale.Y <= 0) {
			return fmt.Errorf("%w: Layers[%d].UVScale.Y = %v must be finite and > 0", ErrInvalidTerrainMaterial, i, l.UVScale.Y)
		}
		if !finite(l.HeightBias) || l.HeightBias < -1 || l.HeightBias > 1 {
			return fmt.Errorf("%w: Layers[%d].HeightBias = %v must be finite in [-1, 1]", ErrInvalidTerrainMaterial, i, l.HeightBias)
		}
		for kind, img := range map[string]image.Image{
			"AlbedoImage":            l.AlbedoImage,
			"NormalImage":            l.NormalImage,
			"MetallicRoughnessImage": l.MetallicRoughnessImage,
			"HeightImage":            l.HeightImage,
		} {
			if img != nil {
				b := img.Bounds()
				if b.Dx() <= 0 || b.Dy() <= 0 {
					return fmt.Errorf("%w: Layers[%d].%s has empty bounds %v", ErrInvalidTerrainMaterial, i, kind, b)
				}
			}
		}
	}
	return nil
}

func terrainNormalMatrix(model math3d.Mat4) math3d.Mat4 {
	if inv, ok := model.Inverse(); ok {
		nm := inv.Transpose()
		nm[3], nm[7], nm[11] = 0, 0, 0
		nm[12], nm[13], nm[14] = 0, 0, 0
		nm[15] = 1
		return nm
	}
	nm := model
	nm[3], nm[7], nm[11] = 0, 0, 0
	nm[12], nm[13], nm[14] = 0, 0, 0
	nm[15] = 1
	return nm
}

// EncodeUniforms encodes m's uniform block into dst[:TerrainMaterialUniformSize]
// in the exact 16-byte-aligned layout declared by [TerrainMaterialBindingsWGSL].
// It panics if len(dst) < TerrainMaterialUniformSize.
func (m TerrainMaterial) EncodeUniforms(dst []byte) []byte {
	if len(dst) < TerrainMaterialUniformSize {
		panic("render3d: TerrainMaterial.EncodeUniforms: dst shorter than TerrainMaterialUniformSize")
	}
	dst = dst[:TerrainMaterialUniformSize]
	clear(dst)

	model := m.Model
	if model == (math3d.Mat4{}) {
		model = math3d.Mat4Identity()
	}
	nm := terrainNormalMatrix(model)
	putMat4(dst[0:64], &model)
	putMat4(dst[64:128], &nm)

	tp := m.Triplanar.normalized()
	layerCount := min(len(m.Layers), MaxTerrainLayers)
	putF32(dst[128:132], float32(layerCount))
	putF32(dst[132:136], clamp01f(m.HeightBlendContrast))
	putF32(dst[136:140], clamp01f(tp.SlopeThreshold))
	putF32(dst[140:144], clamp01f(tp.SlopeBlend))

	triEnabled := float32(0)
	if tp.Enabled {
		triEnabled = 1
	}
	hasSplat1 := float32(0)
	if layerCount > 4 {
		hasSplat1 = 1
	}
	putF32(dst[144:148], triEnabled)
	putF32(dst[148:152], tp.Sharpness)
	putF32(dst[152:156], tp.Scale)
	putF32(dst[156:160], hasSplat1)

	for i := range layerCount {
		l := m.Layers[i]
		off := 160 + i*48
		putF32(dst[off+0:off+4], l.BaseColorFactor[0])
		putF32(dst[off+4:off+8], l.BaseColorFactor[1])
		putF32(dst[off+8:off+12], l.BaseColorFactor[2])
		putF32(dst[off+12:off+16], clamp01f(l.MetallicFactor))

		hasNorm := float32(0)
		if l.NormalImage != nil {
			hasNorm = 1
		}
		putF32(dst[off+16:off+20], clamp01f(l.RoughnessFactor))
		putF32(dst[off+20:off+24], l.NormalScale)
		putF32(dst[off+24:off+28], clampF32(l.HeightBias, -1, 1))
		putF32(dst[off+28:off+32], hasNorm)

		uv := l.effectiveUVScale()
		hasHeight := float32(0)
		if l.HeightImage != nil {
			hasHeight = 1
		}
		putF32(dst[off+32:off+36], uv.X)
		putF32(dst[off+36:off+40], uv.Y)
		putF32(dst[off+40:off+44], hasHeight)
		putF32(dst[off+44:off+48], 0)
	}
	return dst
}

// AppendUniformData appends m's 544-byte uniform block to dst without
// allocating when dst has sufficient spare capacity.
func (m TerrainMaterial) AppendUniformData(dst []byte) []byte {
	start := len(dst)
	dst = grow(dst, TerrainMaterialUniformSize)
	m.EncodeUniforms(dst[start : start+TerrainMaterialUniformSize])
	return dst
}

// TerrainMaterialBindGroupLayout creates the [TerrainMaterialGroup] (group 2)
// bind group layout shared by every [GPUTerrainMaterial]:
//   - binding 0: vertex/fragment uniform buffer ([TerrainMaterialUniforms])
//   - binding 1: splat0 texture_2d<f32> (layers 0..3 RGBA weights)
//   - binding 2: splat1 texture_2d<f32> (layers 4..7 RGBA weights)
//   - binding 3: splat sampler (linear clamp-to-edge)
//   - binding 4: albedo texture_2d_array<f32> (sRGB RGBA8)
//   - binding 5: normal texture_2d_array<f32> (linear RGBA8)
//   - binding 6: metallic-roughness-height texture_2d_array<f32> (R=height, G=roughness, B=metallic)
//   - binding 7: layer sampler (linear repeat)
func TerrainMaterialBindGroupLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.terrain_material",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: TerrainUniformBinding, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: TerrainSplat0TextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: TerrainSplat1TextureBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: TerrainSplatSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: TerrainAlbedoArrayBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: TerrainNormalArrayBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: TerrainMRHeightArrayBinding, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: TerrainLayerSamplerBinding, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// TerrainMaterialUpload configures [NewGPUTerrainMaterial].
type TerrainMaterialUpload struct {
	// Label prefixes GPU resource debug labels.
	Label string
	// Mipmaps generates CPU mip chains for the layer texture arrays.
	Mipmaps bool
	// Retained keeps CPU shadow copies of every GPU buffer, texture, sampler, and bind group so NSGo restores them across device loss.
	Retained bool
}

// GPUTerrainMaterial owns the uniform buffer, splat textures, 2D texture arrays,
// samplers, bind group layout, and bind group for rendering a splat-blended
// multi-layer PBR terrain at [TerrainMaterialGroup] (group 2).
type GPUTerrainMaterial struct {
	layout      *gpu.BindGroupLayout
	uniforms    *gpu.Buffer
	splat0      *GPUTexture
	splat1      *GPUTexture
	splatSamp   *GPUSampler
	albedoArr   *gpu.Texture
	normalArr   *gpu.Texture
	mrHeightArr *gpu.Texture
	layerSamp   *GPUSampler
	group       *gpu.BindGroup
	material    TerrainMaterial
	arrWidth    uint32
	arrHeight   uint32
	arrLayers   uint32
	arrMips     uint32
	scratch     [TerrainMaterialUniformSize]byte
}

func resolveTerrainArraySize(m TerrainMaterial) (int, int) {
	if m.LayerTextureSize > 0 {
		return m.LayerTextureSize, m.LayerTextureSize
	}
	for _, l := range m.Layers {
		for _, img := range []image.Image{l.AlbedoImage, l.NormalImage, l.MetallicRoughnessImage, l.HeightImage} {
			if img != nil {
				b := img.Bounds()
				if b.Dx() > 0 && b.Dy() > 0 {
					return b.Dx(), b.Dy()
				}
			}
		}
	}
	return 1, 1
}

func resampleImageNRGBA(img image.Image, dstW, dstH int) []byte {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	src := make([]byte, srcW*srcH*4)
	if nrgba, ok := img.(*image.NRGBA); ok {
		rowBytes := srcW * 4
		for y := range srcH {
			off := nrgba.PixOffset(b.Min.X, b.Min.Y+y)
			copy(src[y*rowBytes:(y+1)*rowBytes], nrgba.Pix[off:off+rowBytes])
		}
	} else {
		for y := range srcH {
			for x := range srcW {
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				i := (y*srcW + x) * 4
				src[i], src[i+1], src[i+2], src[i+3] = c.R, c.G, c.B, c.A
			}
		}
	}
	if srcW == dstW && srcH == dstH {
		return src
	}
	dst := make([]byte, dstW*dstH*4)
	for y := range dstH {
		fy := (float64(y)+0.5)/float64(dstH)*float64(srcH) - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		iy0 := min(max(y0, 0), srcH-1)
		iy1 := min(max(y0+1, 0), srcH-1)
		for x := range dstW {
			fx := (float64(x)+0.5)/float64(dstW)*float64(srcW) - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			ix0 := min(max(x0, 0), srcW-1)
			ix1 := min(max(x0+1, 0), srcW-1)
			i00 := (iy0*srcW + ix0) * 4
			i10 := (iy0*srcW + ix1) * 4
			i01 := (iy1*srcW + ix0) * 4
			i11 := (iy1*srcW + ix1) * 4
			di := (y*dstW + x) * 4
			for c := range 4 {
				top := float64(src[i00+c])*(1-tx) + float64(src[i10+c])*tx
				bot := float64(src[i01+c])*(1-tx) + float64(src[i11+c])*tx
				v := top*(1-ty) + bot*ty
				dst[di+c] = uint8(math.Round(min(max(v, 0), 255)))
			}
		}
	}
	return dst
}

func solidRGBA8(w, h int, r, g, b, a uint8) []byte {
	pix := make([]byte, w*h*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i] = r
		pix[i+1] = g
		pix[i+2] = b
		pix[i+3] = a
	}
	return pix
}

func packMRHeightLayer(mrImg, heightImg image.Image, w, h int) []byte {
	out := make([]byte, w*h*4)
	var mrPix, hPix []byte
	if mrImg != nil {
		mrPix = resampleImageNRGBA(mrImg, w, h)
	}
	if heightImg != nil {
		hPix = resampleImageNRGBA(heightImg, w, h)
	}
	for i := 0; i < len(out); i += 4 {
		// R = height (default 128 -> ~0.5)
		heightByte := uint8(128)
		if hPix != nil {
			// Luminance from RGB so grayscale and RGB images both work identically.
			lum := (299*uint32(hPix[i]) + 587*uint32(hPix[i+1]) + 114*uint32(hPix[i+2]) + 500) / 1000
			heightByte = uint8(min(lum, 255))
		}
		roughByte := uint8(255)
		metalByte := uint8(255)
		if mrPix != nil {
			roughByte = mrPix[i+1]
			metalByte = mrPix[i+2]
		}
		out[i] = heightByte
		out[i+1] = roughByte
		out[i+2] = metalByte
		out[i+3] = 255
	}
	return out
}

func uploadArrayLayerWithMips(dev *gpu.Device, tex *gpu.Texture, layer uint32, basePix []byte, w, h int, mipLevels uint32, srgb bool) {
	curPix := basePix
	curW, curH := w, h
	for mip := range mipLevels {
		dev.WriteTextureMip(tex, mip, layer, curPix, uint32(curW*4))
		if mip+1 < mipLevels {
			nextW := max(curW/2, 1)
			nextH := max(curH/2, 1)
			curPix = downsamplePixelsBox(curPix, curW, curH, nextW, nextH, 4, srgb)
			curW, curH = nextW, nextH
		}
	}
}

// NewGPUTerrainMaterial validates m and uploads its uniform buffer, splat map
// textures, per-layer 2D texture arrays (albedo, normal, and packed
// height/roughness/metallic), samplers, and [TerrainMaterialGroup] bind group
// onto dev, owned by cx.
func NewGPUTerrainMaterial(cx *core.Scope, dev *gpu.Device, m TerrainMaterial, opts TerrainMaterialUpload) (*GPUTerrainMaterial, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.terrain_material"
	}

	g := &GPUTerrainMaterial{material: m}
	cleanup := func(err error) (*GPUTerrainMaterial, error) {
		_ = g.Close()
		return nil, err
	}

	var err error
	g.layout, err = TerrainMaterialBindGroupLayout(cx, dev)
	if err != nil {
		return cleanup(err)
	}

	m.EncodeUniforms(g.scratch[:])
	g.uniforms, err = dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniforms",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     g.scratch[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	uploadSplat := func(img image.Image, defRGBA [4]byte, suffix string) (*GPUTexture, error) {
		if img != nil {
			return UploadTexture(cx, dev, img, TextureUpload{
				Label:    label + suffix,
				SRGB:     false,
				Mipmaps:  false,
				Retained: opts.Retained,
			})
		}
		return UploadTexturePixels(cx, dev, PixelBuffer{
			Width:  1,
			Height: 1,
			Format: gpu.FormatRGBA8Unorm,
			Pixels: defRGBA[:],
		}, TextureUpload{
			Label:    label + suffix,
			SRGB:     false,
			Mipmaps:  false,
			Retained: opts.Retained,
		})
	}

	g.splat0, err = uploadSplat(m.SplatMap0, [4]byte{255, 0, 0, 0}, ".splat0")
	if err != nil {
		return cleanup(err)
	}
	g.splat1, err = uploadSplat(m.SplatMap1, [4]byte{0, 0, 0, 0}, ".splat1")
	if err != nil {
		return cleanup(err)
	}

	splatSampOpts := SamplerLinearClamp()
	splatSampOpts.Label = label + ".splat_sampler"
	splatSampOpts.Retained = opts.Retained
	g.splatSamp, err = NewGPUSampler(cx, dev, splatSampOpts)
	if err != nil {
		return cleanup(err)
	}

	layerSampOpts := SamplerLinearRepeat()
	layerSampOpts.Label = label + ".layer_sampler"
	layerSampOpts.Retained = opts.Retained
	g.layerSamp, err = NewGPUSampler(cx, dev, layerSampOpts)
	if err != nil {
		return cleanup(err)
	}

	arrW, arrH := resolveTerrainArraySize(m)
	layerCount := uint32(len(m.Layers))
	mipLevels := uint32(1)
	if opts.Mipmaps {
		mipLevels = gpu.MaxMipLevels(uint32(arrW), uint32(arrH))
	}
	g.arrWidth = uint32(arrW)
	g.arrHeight = uint32(arrH)
	g.arrLayers = layerCount
	g.arrMips = mipLevels

	g.albedoArr, err = dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label + ".albedo_array",
		Dimension: gpu.Dim2DArray,
		Width:     uint32(arrW),
		Height:    uint32(arrH),
		Layers:    layerCount,
		MipLevels: mipLevels,
		Format:    gpu.FormatRGBA8UnormSRGB,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}
	g.normalArr, err = dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label + ".normal_array",
		Dimension: gpu.Dim2DArray,
		Width:     uint32(arrW),
		Height:    uint32(arrH),
		Layers:    layerCount,
		MipLevels: mipLevels,
		Format:    gpu.FormatRGBA8Unorm,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}
	g.mrHeightArr, err = dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label + ".mr_height_array",
		Dimension: gpu.Dim2DArray,
		Width:     uint32(arrW),
		Height:    uint32(arrH),
		Layers:    layerCount,
		MipLevels: mipLevels,
		Format:    gpu.FormatRGBA8Unorm,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return cleanup(err)
	}

	for i, l := range m.Layers {
		var albedoPix []byte
		if l.AlbedoImage != nil {
			albedoPix = resampleImageNRGBA(l.AlbedoImage, arrW, arrH)
		} else {
			albedoPix = solidRGBA8(arrW, arrH, 255, 255, 255, 255)
		}
		uploadArrayLayerWithMips(dev, g.albedoArr, uint32(i), albedoPix, arrW, arrH, mipLevels, true)

		var normPix []byte
		if l.NormalImage != nil {
			normPix = resampleImageNRGBA(l.NormalImage, arrW, arrH)
		} else {
			normPix = solidRGBA8(arrW, arrH, 128, 128, 255, 255)
		}
		uploadArrayLayerWithMips(dev, g.normalArr, uint32(i), normPix, arrW, arrH, mipLevels, false)

		mrhPix := packMRHeightLayer(l.MetallicRoughnessImage, l.HeightImage, arrW, arrH)
		uploadArrayLayerWithMips(dev, g.mrHeightArr, uint32(i), mrhPix, arrW, arrH, mipLevels, false)
	}

	g.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: g.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: TerrainUniformBinding, Buffer: g.uniforms, Size: TerrainMaterialUniformSize},
			{Binding: TerrainSplat0TextureBinding, Texture: g.splat0.Texture()},
			{Binding: TerrainSplat1TextureBinding, Texture: g.splat1.Texture()},
			{Binding: TerrainSplatSamplerBinding, Sampler: g.splatSamp.Sampler()},
			{Binding: TerrainAlbedoArrayBinding, Texture: g.albedoArr},
			{Binding: TerrainNormalArrayBinding, Texture: g.normalArr},
			{Binding: TerrainMRHeightArrayBinding, Texture: g.mrHeightArr},
			{Binding: TerrainLayerSamplerBinding, Sampler: g.layerSamp.Sampler()},
		},
	})
	if err != nil {
		return cleanup(err)
	}

	return g, nil
}

// UpdateUniforms validates m and updates g's uniform buffer in place (model
// transform, height blend contrast, triplanar parameters, and per-layer scalar
// factors / UV scales) without reallocating textures or bind groups. The layer
// count must match the count g was created with.
func (g *GPUTerrainMaterial) UpdateUniforms(dev *gpu.Device, m TerrainMaterial) error {
	if !g.Valid() {
		return fmt.Errorf("%w: terrain material is not valid", ErrInvalidTerrainMaterial)
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if uint32(len(m.Layers)) != g.arrLayers {
		return fmt.Errorf("%w: UpdateUniforms layer count %d does not match created count %d", ErrInvalidTerrainMaterial, len(m.Layers), g.arrLayers)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	m.EncodeUniforms(g.scratch[:])
	dev.WriteBuffer(g.uniforms, 0, g.scratch[:])
	g.material = m
	return nil
}

// UpdateSplatMaps uploads new RGBA splat map image data into g's splat textures
// in place (e.g., after interactive terrain painting). Non-nil images must
// match the dimensions of the existing splat texture.
func (g *GPUTerrainMaterial) UpdateSplatMaps(dev *gpu.Device, splat0, splat1 image.Image) error {
	if !g.Valid() {
		return fmt.Errorf("%w: terrain material is not valid", ErrInvalidTerrainMaterial)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if splat0 != nil {
		if err := g.splat0.UpdateImage(dev, splat0); err != nil {
			return fmt.Errorf("%w: splat0: %v", ErrInvalidTerrainMaterial, err)
		}
		g.material.SplatMap0 = splat0
	}
	if splat1 != nil {
		if err := g.splat1.UpdateImage(dev, splat1); err != nil {
			return fmt.Errorf("%w: splat1: %v", ErrInvalidTerrainMaterial, err)
		}
		g.material.SplatMap1 = splat1
	}
	return nil
}

// Bind binds g's bind group at [TerrainMaterialGroup] (group 2) on pass.
func (g *GPUTerrainMaterial) Bind(pass *gpu.RenderPass) {
	if !g.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(TerrainMaterialGroup, g.group)
}

// Layout returns g's [TerrainMaterialGroup] bind group layout, or nil for a nil receiver.
func (g *GPUTerrainMaterial) Layout() *gpu.BindGroupLayout {
	if g == nil {
		return nil
	}
	return g.layout
}

// BindGroup returns g's [gpu.BindGroup], or nil for a nil receiver.
func (g *GPUTerrainMaterial) BindGroup() *gpu.BindGroup {
	if g == nil {
		return nil
	}
	return g.group
}

// UniformBuffer returns g's uniform [*gpu.Buffer], or nil for a nil receiver.
func (g *GPUTerrainMaterial) UniformBuffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.uniforms
}

// LayerCount returns the number of material layers uploaded into g.
func (g *GPUTerrainMaterial) LayerCount() int {
	if g == nil {
		return 0
	}
	return int(g.arrLayers)
}

// ByteSize returns the approximate GPU memory owned by g (uniform buffer,
// splat maps, and all 3 layer texture arrays including mip chains).
func (g *GPUTerrainMaterial) ByteSize() uint64 {
	if g == nil {
		return 0
	}
	var total uint64
	if g.uniforms != nil {
		total += g.uniforms.Size()
	}
	total += g.splat0.ByteSize() + g.splat1.ByteSize()
	// 3 RGBA8 2D array textures of identical dimensions and mip chain.
	var perArr uint64
	w, h := uint64(g.arrWidth), uint64(g.arrHeight)
	for range g.arrMips {
		perArr += w * h * 4 * uint64(g.arrLayers)
		w = max(w/2, 1)
		h = max(h/2, 1)
	}
	total += perArr * 3
	return total
}

// Valid reports whether every GPU resource owned by g is open on a live device.
func (g *GPUTerrainMaterial) Valid() bool {
	return g != nil &&
		g.layout != nil && g.layout.Valid() &&
		g.uniforms != nil && g.uniforms.Valid() &&
		g.splat0.Valid() && g.splat1.Valid() &&
		g.splatSamp.Valid() && g.layerSamp.Valid() &&
		g.albedoArr != nil && g.albedoArr.Valid() &&
		g.normalArr != nil && g.normalArr.Valid() &&
		g.mrHeightArr != nil && g.mrHeightArr.Valid() &&
		g.group != nil && g.group.Valid()
}

// Close releases every GPU resource owned by g now rather than waiting for
// scope disposal. Safe to call on nil or more than once.
func (g *GPUTerrainMaterial) Close() error {
	if g == nil {
		return nil
	}
	var err error
	if g.group != nil {
		err = errors.Join(err, g.group.Close())
	}
	if g.mrHeightArr != nil {
		err = errors.Join(err, g.mrHeightArr.Close())
	}
	if g.normalArr != nil {
		err = errors.Join(err, g.normalArr.Close())
	}
	if g.albedoArr != nil {
		err = errors.Join(err, g.albedoArr.Close())
	}
	if g.layerSamp != nil {
		err = errors.Join(err, g.layerSamp.Close())
	}
	if g.splatSamp != nil {
		err = errors.Join(err, g.splatSamp.Close())
	}
	if g.splat1 != nil {
		err = errors.Join(err, g.splat1.Close())
	}
	if g.splat0 != nil {
		err = errors.Join(err, g.splat0.Close())
	}
	if g.uniforms != nil {
		err = errors.Join(err, g.uniforms.Close())
	}
	if g.layout != nil {
		err = errors.Join(err, g.layout.Close())
	}
	return err
}

// TerrainPipelineOptions configures [NewTerrainMaterialPipeline].
type TerrainPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing and depth writes when non-zero.
	DepthFormat gpu.TextureFormat
	// DoubleSided disables back-face culling when true.
	DoubleSided bool
	// ShadowLayout, when set, compiles [TerrainMaterialShadowWGSL] with a [DirectionalShadowMap] at [ShadowGroup] (group 3).
	ShadowLayout *gpu.BindGroupLayout
	// CSMLayout, when set, compiles [TerrainMaterialCSMWGSL] with a [CascadedShadowMap] at [CSMGroup] (group 3).
	CSMLayout *gpu.BindGroupLayout
	// FogLayout, when set, compiles [TerrainMaterialFogWGSL] with a [GPUFogBuffer] at [FogGroup] (group 3).
	FogLayout *gpu.BindGroupLayout
	// LinearOutput skips sRGB encoding and writes linear radiance directly, for drawing into an [HDRTarget] resolved by [ToneMapPass].
	LinearOutput bool
}

// NewTerrainMaterialPipeline compiles the portable WGSL terrain shader matching
// opts and builds a render pipeline using [MeshVertexLayout] at vertex slot 0,
// camLayout at [CameraGroup] (group 0), lightLayout at [LightGroup] (group 1),
// terrainLayout at [TerrainMaterialGroup] (group 2), and an optional shadow,
// CSM, or fog layout at group 3.
func NewTerrainMaterialPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, terrainLayout *gpu.BindGroupLayout, opts TerrainPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if lightLayout == nil || !lightLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid light bind group layout"}
	}
	if terrainLayout == nil || !terrainLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid terrain material bind group layout"}
	}
	group3Count := 0
	if opts.ShadowLayout != nil {
		group3Count++
	}
	if opts.CSMLayout != nil {
		group3Count++
	}
	if opts.FogLayout != nil {
		group3Count++
	}
	if group3Count > 1 {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: ShadowLayout, CSMLayout, and FogLayout share bind group 3; set at most one"}
	}

	layouts := []*gpu.BindGroupLayout{
		CameraGroup:          camLayout,
		LightGroup:           lightLayout,
		TerrainMaterialGroup: terrainLayout,
	}
	src := TerrainMaterialWGSL
	if opts.LinearOutput {
		src = terrainMaterialLinearWGSL
	}

	suffix := ""
	switch {
	case opts.ShadowLayout != nil:
		if !opts.ShadowLayout.Valid() {
			return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid shadow bind group layout"}
		}
		layouts = append(layouts, opts.ShadowLayout)
		src = TerrainMaterialShadowWGSL
		if opts.LinearOutput {
			src = terrainMaterialShadowLinearWGSL
		}
		suffix = ".shadow"
	case opts.CSMLayout != nil:
		if !opts.CSMLayout.Valid() {
			return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid CSM bind group layout"}
		}
		layouts = append(layouts, opts.CSMLayout)
		src = TerrainMaterialCSMWGSL
		if opts.LinearOutput {
			src = terrainMaterialCSMLinearWGSL
		}
		suffix = ".csm"
	case opts.FogLayout != nil:
		if !opts.FogLayout.Valid() {
			return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid fog bind group layout"}
		}
		layouts = append(layouts, opts.FogLayout)
		src = TerrainMaterialFogWGSL
		if opts.LinearOutput {
			src = terrainMaterialFogLinearWGSL
		}
		suffix = ".fog"
	}

	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return nil, err
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	label := opts.Label
	if label == "" {
		label = "render3d.terrain_material" + suffix
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
		Targets:       []gpu.ColorTargetState{{Format: colorFmt}},
	}
	if opts.DepthFormat != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       opts.DepthFormat,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	return dev.CreatePipeline(cx, desc)
}

// TerrainMaterialBindingsWGSL declares the [TerrainMaterialGroup] (@group(2))
// uniform buffer, splat maps, 2D texture arrays, samplers, and portable helper
// functions (with every helper declared before its callers for MSL/WGSL
// compatibility).
const TerrainMaterialBindingsWGSL = `struct TerrainLayerUniform {
	color_metallic: vec4<f32>,
	pbr_params: vec4<f32>,
	uv_params: vec4<f32>,
}

struct TerrainMaterialUniforms {
	model: mat4x4<f32>,
	normal_matrix: mat4x4<f32>,
	params: vec4<f32>,
	triplanar_params: vec4<f32>,
	layers: array<TerrainLayerUniform, 8>,
}

@group(2) @binding(0) var<uniform> terrain: TerrainMaterialUniforms;
@group(2) @binding(1) var terrain_splat0_tex: texture_2d<f32>;
@group(2) @binding(2) var terrain_splat1_tex: texture_2d<f32>;
@group(2) @binding(3) var terrain_splat_samp: sampler;
@group(2) @binding(4) var terrain_albedo_arr: texture_2d_array<f32>;
@group(2) @binding(5) var terrain_normal_arr: texture_2d_array<f32>;
@group(2) @binding(6) var terrain_mrh_arr: texture_2d_array<f32>;
@group(2) @binding(7) var terrain_layer_samp: sampler;

fn terrain_splat_weight(s0: vec4<f32>, s1: vec4<f32>, idx: u32) -> f32 {
	if (idx == 0u) {
		return s0.x;
	}
	if (idx == 1u) {
		return s0.y;
	}
	if (idx == 2u) {
		return s0.z;
	}
	if (idx == 3u) {
		return s0.w;
	}
	if (idx == 4u) {
		return s1.x;
	}
	if (idx == 5u) {
		return s1.y;
	}
	if (idx == 6u) {
		return s1.z;
	}
	return s1.w;
}

fn terrain_set_channel(v: vec4<f32>, ch: u32, val: f32) -> vec4<f32> {
	if (ch == 0u) {
		return vec4<f32>(val, v.y, v.z, v.w);
	}
	if (ch == 1u) {
		return vec4<f32>(v.x, val, v.z, v.w);
	}
	if (ch == 2u) {
		return vec4<f32>(v.x, v.y, val, v.w);
	}
	return vec4<f32>(v.x, v.y, v.z, val);
}

fn terrain_triplanar_factor(geom_n: vec3<f32>, enabled: f32, threshold: f32, blend_w: f32) -> f32 {
	if (enabled <= 0.5) {
		return 0.0;
	}
	let up = clamp(abs(geom_n.y), 0.0, 1.0);
	let half_b = max(blend_w, 0.001) * 0.5;
	let lo = clamp(threshold - half_b, 0.0, 1.0);
	let hi = clamp(threshold + half_b, lo + 0.001, 1.0);
	let t = clamp((hi - up) / (hi - lo), 0.0, 1.0);
	return t * t * (3.0 - 2.0 * t);
}

fn terrain_triplanar_weights(geom_n: vec3<f32>, sharpness: f32) -> vec3<f32> {
	let an = max(abs(geom_n), vec3<f32>(0.0001, 0.0001, 0.0001));
	let sh = max(sharpness, 0.5);
	let pw = pow(an, vec3<f32>(sh, sh, sh));
	let sum = max(pw.x + pw.y + pw.z, 1e-6);
	return pw / sum;
}

fn terrain_perturb_normal(
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

fn terrain_triplanar_normal(
	geom_n: vec3<f32>,
	nx_sample: vec3<f32>,
	ny_sample: vec3<f32>,
	nz_sample: vec3<f32>,
	normal_scale: f32,
	tw: vec3<f32>,
) -> vec3<f32> {
	let ux = nx_sample * 2.0 - vec3<f32>(1.0, 1.0, 1.0);
	let uy = ny_sample * 2.0 - vec3<f32>(1.0, 1.0, 1.0);
	let uz = nz_sample * 2.0 - vec3<f32>(1.0, 1.0, 1.0);

	let n_x = normalize(vec3<f32>(ux.x * normal_scale, ux.y * normal_scale, ux.z));
	let n_y = normalize(vec3<f32>(uy.x * normal_scale, uy.y * normal_scale, uy.z));
	let n_z = normalize(vec3<f32>(uz.x * normal_scale, uz.y * normal_scale, uz.z));

	// Whiteout-style per-axis world normal blending: preserves geom_n exactly on flat maps.
	let wx = normalize(vec3<f32>(geom_n.x * n_x.z, geom_n.y + n_x.y, geom_n.z + n_x.x));
	let wy = normalize(vec3<f32>(geom_n.x + n_y.x, geom_n.y * n_y.z, geom_n.z - n_y.y));
	let wz = normalize(vec3<f32>(geom_n.x + n_z.x, geom_n.y + n_z.y, geom_n.z * n_z.z));
	return normalize(wx * tw.x + wy * tw.y + wz * tw.z);
}
`

const terrainVertexStageWGSL = `
struct TerrainVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct TerrainVertexOut {
	@builtin(position) clip_pos: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: TerrainVertexIn) -> TerrainVertexOut {
	var out: TerrainVertexOut;
	let world_pos = terrain.model * vec4<f32>(in.pos, 1.0);
	out.clip_pos = camera.view_proj * world_pos;
	out.world_pos = world_pos.xyz;
	out.world_normal = (terrain.normal_matrix * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv = in.uv;
	return out;
}
`

// terrainFragmentBlendWGSL opens fs_main and computes the blended PBR surface
// properties (blend_albedo, blend_metallic, blend_roughness, n, view_dir) in
// uniform control flow.
const terrainFragmentBlendWGSL = `
@fragment
fn fs_main(in: TerrainVertexOut) -> @location(0) vec4<f32> {
	let splat0_raw = textureSample(terrain_splat0_tex, terrain_splat_samp, in.uv);
	let splat1_raw = textureSample(terrain_splat1_tex, terrain_splat_samp, in.uv);
	let dp1 = dpdx(in.world_pos);
	let dp2 = dpdy(in.world_pos);
	let duv1 = dpdx(in.uv);
	let duv2 = dpdy(in.uv);

	var geom_n = vec3<f32>(0.0, 1.0, 0.0);
	let gn_len = length(in.world_normal);
	if (gn_len > 1e-6) {
		geom_n = in.world_normal / gn_len;
	}

	let layer_count = u32(clamp(terrain.params.x, 1.0, 8.0));
	let contrast = clamp(terrain.params.y, 0.0, 0.98);
	let tri_enabled = terrain.triplanar_params.x;
	let tri_scale = terrain.triplanar_params.z;
	let tri_factor = terrain_triplanar_factor(geom_n, tri_enabled, terrain.params.z, terrain.params.w);
	let tw = terrain_triplanar_weights(geom_n, terrain.triplanar_params.y);

	var splat1_masked = vec4<f32>(0.0, 0.0, 0.0, 0.0);
	if (terrain.triplanar_params.w > 0.5) {
		splat1_masked = splat1_raw;
	}

	// Normalize raw splat weights across active layers.
	var nw0 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
	var nw1 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
	var w_sum: f32 = 0.0;
	for (var i: u32 = 0u; i < 8u; i = i + 1u) {
		if (i >= layer_count) {
			break;
		}
		let rw = max(terrain_splat_weight(splat0_raw, splat1_masked, i), 0.0);
		w_sum = w_sum + rw;
		if (i < 4u) {
			nw0 = terrain_set_channel(nw0, i, rw);
		} else {
			nw1 = terrain_set_channel(nw1, i - 4u, rw);
		}
	}
	if (w_sum <= 1e-6) {
		nw0 = vec4<f32>(1.0, 0.0, 0.0, 0.0);
		nw1 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
	} else {
		let inv_sum = 1.0 / w_sum;
		nw0 = nw0 * inv_sum;
		nw1 = nw1 * inv_sum;
	}

	var ew0 = nw0;
	var ew1 = nw1;

	// Optional height-based blending (gated only on uniform contrast > 0 so texture sampling stays in uniform control flow).
	if (contrast > 0.0) {
		var sc0 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
		var sc1 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
		var max_sc: f32 = 0.0;
		for (var i: u32 = 0u; i < 8u; i = i + 1u) {
			if (i >= layer_count) {
				break;
			}
			let lyr = terrain.layers[i];
			let lyr_idx = i32(i);
			let uv_scale = lyr.uv_params.xy;
			let planar_uv = in.uv * uv_scale;
			var h_val = textureSample(terrain_mrh_arr, terrain_layer_samp, planar_uv, lyr_idx).x;
			if (tri_enabled > 0.5) {
				let ts = uv_scale * tri_scale;
				let uv_x = vec2<f32>(in.world_pos.z, -in.world_pos.y) * ts;
				let uv_y = vec2<f32>(in.world_pos.x, in.world_pos.z) * ts;
				let uv_z = vec2<f32>(in.world_pos.x, -in.world_pos.y) * ts;
				let hx = textureSample(terrain_mrh_arr, terrain_layer_samp, uv_x, lyr_idx).x;
				let hy = textureSample(terrain_mrh_arr, terrain_layer_samp, uv_y, lyr_idx).x;
				let hz = textureSample(terrain_mrh_arr, terrain_layer_samp, uv_z, lyr_idx).x;
				let h_tri = hx * tw.x + hy * tw.y + hz * tw.z;
				h_val = mix(h_val, h_tri, tri_factor);
			}
			let h_eff = clamp(h_val + lyr.pbr_params.z, 0.0, 1.0);
			let wi = terrain_splat_weight(nw0, nw1, i);
			let sc = wi * (0.5 + h_eff);
			if (sc > max_sc) {
				max_sc = sc;
			}
			if (i < 4u) {
				sc0 = terrain_set_channel(sc0, i, sc);
			} else {
				sc1 = terrain_set_channel(sc1, i - 4u, sc);
			}
		}
		if (max_sc > 1e-7) {
			let thresh = max_sc * contrast;
			var e_sum: f32 = 0.0;
			var tmp0 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
			var tmp1 = vec4<f32>(0.0, 0.0, 0.0, 0.0);
			for (var i: u32 = 0u; i < 8u; i = i + 1u) {
				if (i >= layer_count) {
					break;
				}
				let e = max(terrain_splat_weight(sc0, sc1, i) - thresh, 0.0);
				e_sum = e_sum + e;
				if (i < 4u) {
					tmp0 = terrain_set_channel(tmp0, i, e);
				} else {
					tmp1 = terrain_set_channel(tmp1, i - 4u, e);
				}
			}
			if (e_sum > 1e-7) {
				let inv_e = 1.0 / e_sum;
				ew0 = tmp0 * inv_e;
				ew1 = tmp1 * inv_e;
			}
		}
	}

	var blend_albedo = vec3<f32>(0.0, 0.0, 0.0);
	var blend_normal = vec3<f32>(0.0, 0.0, 0.0);
	var blend_metallic: f32 = 0.0;
	var blend_roughness: f32 = 0.0;

	for (var i: u32 = 0u; i < 8u; i = i + 1u) {
		if (i >= layer_count) {
			break;
		}
		let lyr = terrain.layers[i];
		let lyr_idx = i32(i);
		let uv_scale = lyr.uv_params.xy;
		let planar_uv = in.uv * uv_scale;

		var alb_s = textureSample(terrain_albedo_arr, terrain_layer_samp, planar_uv, lyr_idx).rgb;
		var mrh_s = textureSample(terrain_mrh_arr, terrain_layer_samp, planar_uv, lyr_idx);
		let norm_p_s = textureSample(terrain_normal_arr, terrain_layer_samp, planar_uv, lyr_idx).xyz;

		var lyr_n = geom_n;
		if (lyr.pbr_params.w > 0.5) {
			lyr_n = terrain_perturb_normal(geom_n, dp1, dp2, duv1 * uv_scale, duv2 * uv_scale, norm_p_s, lyr.pbr_params.y);
		}

		if (tri_enabled > 0.5) {
			let ts = uv_scale * tri_scale;
			let uv_x = vec2<f32>(in.world_pos.z, -in.world_pos.y) * ts;
			let uv_y = vec2<f32>(in.world_pos.x, in.world_pos.z) * ts;
			let uv_z = vec2<f32>(in.world_pos.x, -in.world_pos.y) * ts;

			let alb_x = textureSample(terrain_albedo_arr, terrain_layer_samp, uv_x, lyr_idx).rgb;
			let alb_y = textureSample(terrain_albedo_arr, terrain_layer_samp, uv_y, lyr_idx).rgb;
			let alb_z = textureSample(terrain_albedo_arr, terrain_layer_samp, uv_z, lyr_idx).rgb;
			let alb_tri = alb_x * tw.x + alb_y * tw.y + alb_z * tw.z;
			alb_s = mix(alb_s, alb_tri, tri_factor);

			let mrh_x = textureSample(terrain_mrh_arr, terrain_layer_samp, uv_x, lyr_idx);
			let mrh_y = textureSample(terrain_mrh_arr, terrain_layer_samp, uv_y, lyr_idx);
			let mrh_z = textureSample(terrain_mrh_arr, terrain_layer_samp, uv_z, lyr_idx);
			let mrh_tri = mrh_x * tw.x + mrh_y * tw.y + mrh_z * tw.z;
			mrh_s = mix(mrh_s, mrh_tri, tri_factor);

			let nx_s = textureSample(terrain_normal_arr, terrain_layer_samp, uv_x, lyr_idx).xyz;
			let ny_s = textureSample(terrain_normal_arr, terrain_layer_samp, uv_y, lyr_idx).xyz;
			let nz_s = textureSample(terrain_normal_arr, terrain_layer_samp, uv_z, lyr_idx).xyz;
			if (lyr.pbr_params.w > 0.5) {
				let n_tri = terrain_triplanar_normal(geom_n, nx_s, ny_s, nz_s, lyr.pbr_params.y, tw);
				lyr_n = normalize(mix(lyr_n, n_tri, tri_factor));
			}
		}

		let w = terrain_splat_weight(ew0, ew1, i);
		blend_albedo = blend_albedo + (alb_s * lyr.color_metallic.rgb) * w;
		blend_normal = blend_normal + lyr_n * w;
		blend_metallic = blend_metallic + clamp(lyr.color_metallic.w * mrh_s.z, 0.0, 1.0) * w;
		blend_roughness = blend_roughness + clamp(lyr.pbr_params.x * mrh_s.y, 0.045, 1.0) * w;
	}

	var n = geom_n;
	let bn_len = length(blend_normal);
	if (bn_len > 1e-6) {
		n = blend_normal / bn_len;
	}
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
`

const terrainPreludeWGSL = CameraWGSL + LightWGSL + TerrainMaterialBindingsWGSL + terrainVertexStageWGSL

// TerrainMaterialWGSL is the complete portable WGSL shader module for lit
// splat-map multi-layer PBR terrain combining [CameraWGSL] (group 0),
// [LightWGSL] (group 1), and [TerrainMaterialBindingsWGSL] (group 2).
const TerrainMaterialWGSL = terrainPreludeWGSL + terrainFragmentBlendWGSL + `	let lit = shade_pbr(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness);
	return vec4<f32>(linear_to_srgb(lit), 1.0);
}
`

const terrainMaterialLinearWGSL = terrainPreludeWGSL + terrainFragmentBlendWGSL + `	let lit = shade_pbr(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness);
	return vec4<f32>(lit, 1.0);
}
`

// TerrainMaterialShadowWGSL is [TerrainMaterialWGSL] with directional shadow
// mapping via [ShadowWGSL] at [ShadowGroup] (group 3).
const TerrainMaterialShadowWGSL = terrainPreludeWGSL + ShadowWGSL + terrainFragmentBlendWGSL + `	let shadow_vis = sample_directional_shadow(in.world_pos, geom_n);
	let lit = shade_pbr_shadowed(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness, shadow_vis);
	return vec4<f32>(linear_to_srgb(lit), 1.0);
}
`

const terrainMaterialShadowLinearWGSL = terrainPreludeWGSL + ShadowWGSL + terrainFragmentBlendWGSL + `	let shadow_vis = sample_directional_shadow(in.world_pos, geom_n);
	let lit = shade_pbr_shadowed(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness, shadow_vis);
	return vec4<f32>(lit, 1.0);
}
`

// TerrainMaterialCSMWGSL is [TerrainMaterialWGSL] with cascaded directional
// shadows via [CSMWGSL] at [CSMGroup] (group 3).
const TerrainMaterialCSMWGSL = terrainPreludeWGSL + CSMWGSL + terrainFragmentBlendWGSL + `	let view_depth = csm_view_depth(in.world_pos);
	let shadow_vis = csm_shadow_factor(in.world_pos, geom_n, view_depth);
	let lit = shade_pbr_shadowed(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness, shadow_vis) * csm_debug_tint(in.world_pos);
	return vec4<f32>(linear_to_srgb(lit), 1.0);
}
`

const terrainMaterialCSMLinearWGSL = terrainPreludeWGSL + CSMWGSL + terrainFragmentBlendWGSL + `	let view_depth = csm_view_depth(in.world_pos);
	let shadow_vis = csm_shadow_factor(in.world_pos, geom_n, view_depth);
	let lit = shade_pbr_shadowed(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness, shadow_vis) * csm_debug_tint(in.world_pos);
	return vec4<f32>(lit, 1.0);
}
`

// TerrainMaterialFogWGSL is [TerrainMaterialWGSL] with distance and height fog
// via [FogWGSL] at [FogGroup] (group 3).
const TerrainMaterialFogWGSL = terrainPreludeWGSL + FogWGSL + FogBindingWGSL + FogCameraWGSL + terrainFragmentBlendWGSL + `	let lit = shade_pbr(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness);
	let fogged = apply_fog(lit, fog_camera_pos(in.world_pos), in.world_pos, fog);
	return vec4<f32>(linear_to_srgb(fogged), 1.0);
}
`

const terrainMaterialFogLinearWGSL = terrainPreludeWGSL + FogWGSL + FogBindingWGSL + FogCameraWGSL + terrainFragmentBlendWGSL + `	let lit = shade_pbr(in.world_pos, n, view_dir, blend_albedo, blend_metallic, blend_roughness);
	let fogged = apply_fog(lit, fog_camera_pos(in.world_pos), in.world_pos, fog);
	return vec4<f32>(fogged, 1.0);
}
`

// TerrainEvalLayerSample holds per-layer sampled texel values for the pure-Go
// CPU reference evaluator [EvaluateTerrainMaterial].
type TerrainEvalLayerSample struct {
	// AlbedoLinear is the linear RGB base-color sample (defaults to [1, 1, 1] when HasAlbedo is false).
	AlbedoLinear [3]float32
	HasAlbedo    bool
	// MetallicSample is the metallic-roughness texture B channel (defaults to 1 when HasMetallicRoughness is false).
	MetallicSample float32
	// RoughnessSample is the metallic-roughness texture G channel (defaults to 1 when HasMetallicRoughness is false).
	RoughnessSample      float32
	HasMetallicRoughness bool
	// NormalSample is the UNORM [0, 1] tangent-space normal sample (defaults to [0.5, 0.5, 1] when HasNormal is false).
	NormalSample [3]float32
	HasNormal    bool
	// HeightSample is the normalized [0, 1] height-map sample (defaults to 0.5 when HasHeight is false).
	HeightSample float32
	HasHeight    bool
}

// TerrainEvalInput provides per-fragment inputs to [EvaluateTerrainMaterial],
// matching [TerrainMaterialWGSL] and [TerrainMaterialShadowWGSL].
type TerrainEvalInput struct {
	Material TerrainMaterial
	// Splat0 holds the RGBA weights from SplatMap0 (layers 0..3).
	Splat0 [4]float32
	// Splat1 holds the RGBA weights from SplatMap1 (layers 4..7).
	Splat1 [4]float32
	// Samples holds optional per-layer planar samples (indexed by layer index).
	Samples []TerrainEvalLayerSample
	// TriplanarX, TriplanarY, TriplanarZ hold optional per-layer samples along the X, Y, Z projection planes when Triplanar.Enabled is true.
	TriplanarX []TerrainEvalLayerSample
	TriplanarY []TerrainEvalLayerSample
	TriplanarZ []TerrainEvalLayerSample
	// WorldPos is the fragment world-space position.
	WorldPos math3d.Vec3
	// Normal is the geometric world-space normal.
	Normal math3d.Vec3
	// Tangent and Bitangent optionally supply the planar tangent frame; derived from Normal when zero.
	Tangent   math3d.Vec3
	Bitangent math3d.Vec3
	// ViewDir points from WorldPos toward the camera.
	ViewDir math3d.Vec3
	// Rig is the scene [LightRig].
	Rig LightRig
	// ShadowVisibility attenuates directional light 0 in [0, 1] (0 defaults to 1 unless UseShadowVisibility is true).
	ShadowVisibility    float32
	UseShadowVisibility bool
}

// TerrainEvalResult holds the CPU reference output from [EvaluateTerrainMaterial].
type TerrainEvalResult struct {
	Weights         [MaxTerrainLayers]float32
	TriplanarFactor float32
	BaseColor       math3d.Vec3
	Normal          math3d.Vec3
	Metallic        float32
	Roughness       float32
	Linear          math3d.Vec3
	SRGB            math3d.Vec3
}

// EvaluateTerrainTriplanarFactor computes the slope-driven triplanar blend
// factor in [0, 1] matching terrain_triplanar_factor in [TerrainMaterialBindingsWGSL].
func EvaluateTerrainTriplanarFactor(geomN math3d.Vec3, tp TerrainTriplanar) float32 {
	if !tp.Enabled {
		return 0
	}
	tp = tp.normalized()
	if geomN.LenSq() > 1e-12 {
		geomN = geomN.Norm()
	} else {
		geomN = math3d.Vec3Up()
	}
	up := clamp01f(float32(math.Abs(float64(geomN.Y))))
	halfB := max(tp.SlopeBlend, 0.001) * 0.5
	lo := clamp01f(tp.SlopeThreshold - halfB)
	hi := clampF32(tp.SlopeThreshold+halfB, lo+0.001, 1)
	t := clamp01f((hi - up) / (hi - lo))
	return t * t * (3 - 2*t)
}

// EvaluateTerrainTriplanarWeights computes the normalized X/Y/Z axis blend
// weights matching terrain_triplanar_weights in [TerrainMaterialBindingsWGSL].
func EvaluateTerrainTriplanarWeights(geomN math3d.Vec3, sharpness float32) math3d.Vec3 {
	if geomN.LenSq() > 1e-12 {
		geomN = geomN.Norm()
	} else {
		geomN = math3d.Vec3Up()
	}
	sh := float64(max(sharpness, 0.5))
	ax := max(math.Abs(float64(geomN.X)), 0.0001)
	ay := max(math.Abs(float64(geomN.Y)), 0.0001)
	az := max(math.Abs(float64(geomN.Z)), 0.0001)
	px := float32(math.Pow(ax, sh))
	py := float32(math.Pow(ay, sh))
	pz := float32(math.Pow(az, sh))
	sum := max(px+py+pz, 1e-6)
	return math3d.Vec3{X: px / sum, Y: py / sum, Z: pz / sum}
}

func evalLayerSampleOrDefault(samples []TerrainEvalLayerSample, idx int) (albedo [3]float32, metallic, roughness float32, normal [3]float32, height float32) {
	albedo = [3]float32{1, 1, 1}
	metallic, roughness = 1, 1
	normal = [3]float32{0.5, 0.5, 1}
	height = 0.5
	if idx < 0 || idx >= len(samples) {
		return
	}
	s := samples[idx]
	if s.HasAlbedo {
		albedo = s.AlbedoLinear
	}
	if s.HasMetallicRoughness {
		metallic = s.MetallicSample
		roughness = s.RoughnessSample
	}
	if s.HasNormal {
		normal = s.NormalSample
	}
	if s.HasHeight {
		height = s.HeightSample
	}
	return
}

func evalTriplanarNormal(geomN math3d.Vec3, nxS, nyS, nzS [3]float32, scale float32, tw math3d.Vec3) math3d.Vec3 {
	unpack := func(s [3]float32) math3d.Vec3 {
		return math3d.Vec3{
			X: (s[0]*2 - 1) * scale,
			Y: (s[1]*2 - 1) * scale,
			Z: s[2]*2 - 1,
		}.Norm()
	}
	nx := unpack(nxS)
	ny := unpack(nyS)
	nz := unpack(nzS)
	wx := math3d.Vec3{X: geomN.X * nx.Z, Y: geomN.Y + nx.Y, Z: geomN.Z + nx.X}.Norm()
	wy := math3d.Vec3{X: geomN.X + ny.X, Y: geomN.Y * ny.Z, Z: geomN.Z - ny.Y}.Norm()
	wz := math3d.Vec3{X: geomN.X + nz.X, Y: geomN.Y + nz.Y, Z: geomN.Z * nz.Z}.Norm()
	return wx.Scale(tw.X).Add(wy.Scale(tw.Y)).Add(wz.Scale(tw.Z)).Norm()
}

// EvaluateTerrainMaterial is the pure-Go CPU reference matching
// [TerrainMaterialWGSL] and [TerrainMaterialShadowWGSL] so tests can verify
// GPU renders against an exact analytical reference.
func EvaluateTerrainMaterial(in TerrainEvalInput) TerrainEvalResult {
	count := min(len(in.Material.Layers), MaxTerrainLayers)
	if count == 0 {
		return TerrainEvalResult{}
	}
	geomN := math3d.Vec3Up()
	if in.Normal.LenSq() > 1e-12 {
		geomN = in.Normal.Norm()
	}
	tp := in.Material.Triplanar.normalized()
	triFactor := EvaluateTerrainTriplanarFactor(geomN, in.Material.Triplanar)
	tw := EvaluateTerrainTriplanarWeights(geomN, tp.Sharpness)

	var raw [MaxTerrainLayers]float32
	for i := range count {
		if i < 4 {
			raw[i] = max(in.Splat0[i], 0)
		} else {
			raw[i] = max(in.Splat1[i-4], 0)
		}
	}
	var sum float32
	var nw [MaxTerrainLayers]float32
	for i := range count {
		nw[i] = raw[i]
		sum += raw[i]
	}
	if sum <= 1e-6 {
		nw[0] = 1
	} else {
		inv := 1 / sum
		for i := range count {
			nw[i] *= inv
		}
	}

	ew := nw
	contrast := clampF32(in.Material.HeightBlendContrast, 0, 0.98)
	if contrast > 0 {
		var sc [MaxTerrainLayers]float32
		var maxSc float32
		for i := range count {
			_, _, _, _, hp := evalLayerSampleOrDefault(in.Samples, i)
			hVal := hp
			if in.Material.Triplanar.Enabled {
				_, _, _, _, hx := evalLayerSampleOrDefault(in.TriplanarX, i)
				_, _, _, _, hy := evalLayerSampleOrDefault(in.TriplanarY, i)
				_, _, _, _, hz := evalLayerSampleOrDefault(in.TriplanarZ, i)
				hTri := hx*tw.X + hy*tw.Y + hz*tw.Z
				hVal = hVal*(1-triFactor) + hTri*triFactor
			}
			hEff := clamp01f(hVal + in.Material.Layers[i].HeightBias)
			sc[i] = nw[i] * (0.5 + hEff)
			if sc[i] > maxSc {
				maxSc = sc[i]
			}
		}
		if maxSc > 1e-7 {
			thresh := maxSc * contrast
			var eSum float32
			var tmp [MaxTerrainLayers]float32
			for i := range count {
				tmp[i] = max(sc[i]-thresh, 0)
				eSum += tmp[i]
			}
			if eSum > 1e-7 {
				inv := 1 / eSum
				for i := range count {
					ew[i] = tmp[i] * inv
				}
			}
		}
	}

	tVec, bVec := resolveTangentBasis(geomN, in.Tangent, in.Bitangent)
	var blendAlbedo, blendNormal math3d.Vec3
	var blendMetallic, blendRoughness float32

	for i := range count {
		lyr := in.Material.Layers[i]
		albS, metS, roughS, normS, _ := evalLayerSampleOrDefault(in.Samples, i)
		lyrN := geomN
		hasNorm := lyr.NormalImage != nil || (i < len(in.Samples) && in.Samples[i].HasNormal)
		if hasNorm {
			nx := (normS[0]*2 - 1) * lyr.NormalScale
			ny := (normS[1]*2 - 1) * lyr.NormalScale
			nz := normS[2]*2 - 1
			nTS := math3d.Vec3{X: nx, Y: ny, Z: nz}.Norm()
			lyrN = tVec.Scale(nTS.X).Add(bVec.Scale(nTS.Y)).Add(geomN.Scale(nTS.Z)).Norm()
		}

		if in.Material.Triplanar.Enabled {
			ax, mx, rx, nxS, _ := evalLayerSampleOrDefault(in.TriplanarX, i)
			ay, my, ry, nyS, _ := evalLayerSampleOrDefault(in.TriplanarY, i)
			az, mz, rz, nzS, _ := evalLayerSampleOrDefault(in.TriplanarZ, i)
			for c := range 3 {
				triC := ax[c]*tw.X + ay[c]*tw.Y + az[c]*tw.Z
				albS[c] = albS[c]*(1-triFactor) + triC*triFactor
			}
			metTri := mx*tw.X + my*tw.Y + mz*tw.Z
			roughTri := rx*tw.X + ry*tw.Y + rz*tw.Z
			metS = metS*(1-triFactor) + metTri*triFactor
			roughS = roughS*(1-triFactor) + roughTri*triFactor
			if hasNorm {
				nTri := evalTriplanarNormal(geomN, nxS, nyS, nzS, lyr.NormalScale, tw)
				lyrN = mixVec3(lyrN, nTri, triFactor).Norm()
			}
		}

		w := ew[i]
		blendAlbedo = blendAlbedo.Add(math3d.Vec3{
			X: albS[0] * lyr.BaseColorFactor[0],
			Y: albS[1] * lyr.BaseColorFactor[1],
			Z: albS[2] * lyr.BaseColorFactor[2],
		}.Scale(w))
		blendNormal = blendNormal.Add(lyrN.Scale(w))
		blendMetallic += clamp01f(lyr.MetallicFactor*metS) * w
		blendRoughness += clampF32(lyr.RoughnessFactor*roughS, minPerceptualRoughness, 1) * w
	}

	n := geomN
	if blendNormal.LenSq() > 1e-12 {
		n = blendNormal.Norm()
	}
	vis := float32(1)
	if in.UseShadowVisibility {
		vis = clamp01f(in.ShadowVisibility)
	}
	mat := PBRMaterial{
		BaseColor: blendAlbedo,
		Metallic:  blendMetallic,
		Roughness: blendRoughness,
	}
	lit := ShadePointShadowed(in.WorldPos, n, in.ViewDir, mat, in.Rig, vis)
	srgb := LinearToSRGB(lit)
	return TerrainEvalResult{
		Weights:         ew,
		TriplanarFactor: triFactor,
		BaseColor:       blendAlbedo,
		Normal:          n,
		Metallic:        blendMetallic,
		Roughness:       blendRoughness,
		Linear:          lit,
		SRGB:            srgb,
	}
}
