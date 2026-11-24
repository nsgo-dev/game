package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidCubemap is wrapped by every error returned for malformed cubemap inputs, invalid prefilter parameters, or invalid BRDF LUT dimensions.
var ErrInvalidCubemap = errors.New("render3d: invalid cubemap")

// ErrInvalidIBL is wrapped by every error [IBLUniform.Validate], [NewGPUIBLBuffer] and [GPUIBLBuffer.Update] return for malformed image-based lighting parameters.
var ErrInvalidIBL = errors.New("render3d: invalid image-based lighting")

const (
	// SkyboxUniformSize is the byte size of the skybox uniform block written by [SkyboxUniform.Encode] and declared by [SkyboxWGSL].
	SkyboxUniformSize = 64 + 16

	// IBLGroup is the bind group index [IBLWGSL] and [LitPBRIBLWGSL] declare for image-based lighting ([GPUIBLBuffer.BindGroup]), after [CameraGroup], [LightGroup] and [PBRDrawGroup]. [FogGroup] uses the same index, so a lit pipeline has either IBL or fog. Use [IBLWGSLForGroup] when a custom shader binds the [GPUIBLBuffer] at another group.
	IBLGroup = 3

	// IBLUniformSize is the byte size of the IBL uniform block written by [IBLUniform.Encode] and declared by [IBLWGSL]: nine vec4<f32> L2 spherical harmonic irradiance coefficients followed by intensity, max_lod, and two padding floats.
	IBLUniformSize = 9*16 + 16
)

// SkyboxWGSL renders a cubemap as an infinitely distant background using a single fullscreen triangle driven by @builtin(vertex_index) (zero vertex buffers). It emits depth z = 0.99999994 (1 - 2^-24, the largest float32 below 1.0), so a pipeline with [gpu.CompareLessEqual] and DepthWrite disabled draws the skybox after opaque geometry without shading occluded pixels: only geometry whose depth is within 2^-24 of the far plane, below the precision of a 24-bit depth buffer, is painted over. Emitting exactly 1.0 would be the textbook choice, but NSGo's software rasterizer rejects fragments whose interpolated depth rounds above 1.0, which leaves clear-color holes in the sky (nsgo-dev/nsgo#2344).
const SkyboxWGSL = `struct SkyboxParams {
	inv_view_proj: mat4x4<f32>,
	exposure: f32,
	lod: f32,
	_pad0: f32,
	_pad1: f32,
}

@group(0) @binding(0) var<uniform> skybox: SkyboxParams;
@group(0) @binding(1) var skybox_tex: texture_cube<f32>;
@group(0) @binding(2) var skybox_sampler: sampler;

struct SkyboxVertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) dir: vec3<f32>,
}

@vertex
fn vs_skybox(@builtin(vertex_index) vid: u32) -> SkyboxVertexOut {
	var ndc = vec2<f32>(-1.0, -1.0);
	if (vid == 1u) {
		ndc = vec2<f32>(3.0, -1.0);
	} else if (vid == 2u) {
		ndc = vec2<f32>(-1.0, 3.0);
	}
	let world = skybox.inv_view_proj * vec4<f32>(ndc, 1.0, 1.0);
	var out: SkyboxVertexOut;
	out.pos = vec4<f32>(ndc, 0.99999994, 1.0);
	out.dir = world.xyz;
	return out;
}

@fragment
fn fs_skybox(in: SkyboxVertexOut) -> @location(0) vec4<f32> {
	let dir = normalize(in.dir);
	let sampled = textureSampleLevel(skybox_tex, skybox_sampler, dir, skybox.lod);
	return vec4<f32>(sampled.rgb * skybox.exposure, 1.0);
}
`

// iblWGSLStruct declares the IBL uniform block layout [IBLUniform.Encode] writes.
const iblWGSLStruct = `struct IBLUniform {
	sh: array<vec4<f32>, 9>,
	intensity: f32,
	max_lod: f32,
	_pad0: f32,
	_pad1: f32,
}

`

// iblWGSLDecl3 declares the IBL bindings at [IBLGroup].
const iblWGSLDecl3 = iblWGSLStruct + `@group(3) @binding(0) var<uniform> ibl: IBLUniform;
@group(3) @binding(1) var ibl_prefilter: texture_cube<f32>;
@group(3) @binding(2) var ibl_sampler: sampler;

`

// iblWGSLFuncs holds the IBL helper functions, which read the bindings the declarations before them make.
const iblWGSLFuncs = `fn evaluate_irradiance_sh(n: vec3<f32>) -> vec3<f32> {
	let d = normalize(n);
	var e = ibl.sh[0].xyz * 0.28209479;
	e = e + ibl.sh[1].xyz * (0.48860251 * d.y);
	e = e + ibl.sh[2].xyz * (0.48860251 * d.z);
	e = e + ibl.sh[3].xyz * (0.48860251 * d.x);
	e = e + ibl.sh[4].xyz * (1.09254843 * d.x * d.y);
	e = e + ibl.sh[5].xyz * (1.09254843 * d.y * d.z);
	e = e + ibl.sh[6].xyz * (0.31539157 * (3.0 * d.z * d.z - 1.0));
	e = e + ibl.sh[7].xyz * (1.09254843 * d.x * d.z);
	e = e + ibl.sh[8].xyz * (0.54627422 * (d.x * d.x - d.y * d.y));
	return max(e, vec3<f32>(0.0));
}

fn env_brdf_approx(f0: vec3<f32>, roughness: f32, n_dot_v: f32) -> vec3<f32> {
	let c0 = vec4<f32>(-1.0, -0.0275, -0.572, 0.022);
	let c1 = vec4<f32>(1.0, 0.0425, 1.04, -0.04);
	let r = roughness * c0 + c1;
	// pow(2.0, x) instead of exp2(x): NSGo v0.3.0's software GPU VM has no exp, exp2, log or log2 and evaluates them to zero (nsgo-dev/nsgo#2334).
	let a004 = min(r.x * r.x, pow(2.0, -9.28 * n_dot_v)) * r.x + r.y;
	let ab = vec2<f32>(-1.04, 1.04) * a004 + r.zw;
	return f0 * ab.x + vec3<f32>(ab.y);
}

fn fresnel_schlick_roughness(cos_theta: f32, f0: vec3<f32>, roughness: f32) -> vec3<f32> {
	let m = max(vec3<f32>(1.0 - roughness), f0);
	return f0 + (m - f0) * pow(clamp(1.0 - cos_theta, 0.0, 1.0), 5.0);
}

fn evaluate_ibl(normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32, ao: f32) -> vec3<f32> {
	let n = normalize(normal);
	let v = normalize(view_dir);
	let m = clamp(metallic, 0.0, 1.0);
	let rough = clamp(roughness, 0.0, 1.0);
	let n_dot_v = max(dot(n, v), 0.0);
	let f0 = mix(vec3<f32>(0.04), base_color, m);
	let f = fresnel_schlick_roughness(n_dot_v, f0, rough);
	let k_d = (vec3<f32>(1.0) - f) * (1.0 - m);
	let irradiance = evaluate_irradiance_sh(n);
	let diffuse = k_d * base_color * (irradiance * 0.31830989);
	let r = reflect(-v, n);
	let prefiltered = textureSampleLevel(ibl_prefilter, ibl_sampler, r, rough * ibl.max_lod).rgb;
	let brdf = env_brdf_approx(f0, rough, n_dot_v);
	let specular = prefiltered * brdf;
	return (diffuse + specular) * (ao * ibl.intensity);
}
`

// IBLWGSL declares the IBL uniform block, prefiltered environment cubemap, and sampler at [IBLGroup] (@group(3)), together with evaluate_irradiance_sh, env_brdf_approx, fresnel_schlick_roughness, and evaluate_ibl. Prepend it to a PBR fragment shader whose pipeline binds [GPUIBLBuffer.BindGroup] at [IBLGroup]; [LitPBRIBLWGSL] does this for the repository's lit PBR shader.
const IBLWGSL = iblWGSLDecl3 + iblWGSLFuncs

// IBLWGSLForGroup returns the same WGSL declarations and helper functions as [IBLWGSL] with the uniform, cubemap, and sampler placed at the given @group index.
func IBLWGSLForGroup(group uint32) string {
	return iblWGSLStruct + fmt.Sprintf(`@group(%d) @binding(0) var<uniform> ibl: IBLUniform;
@group(%d) @binding(1) var ibl_prefilter: texture_cube<f32>;
@group(%d) @binding(2) var ibl_sampler: sampler;

`, group, group, group) + iblWGSLFuncs
}

// Spherical harmonic basis constants (orthonormal real L0..L2 basis).
const (
	shBasisC0 = 0.28209479177387814 // 1 / (2 * sqrt(pi))
	shBasisC1 = 0.4886025119029199  // sqrt(3 / (4 * pi))
	shBasisC2 = 1.0925484305920792  // 0.5 * sqrt(15 / pi)
	shBasisC3 = 0.31539156525252005 // 0.25 * sqrt(5 / pi)
	shBasisC4 = 0.5462742152960396  // 0.25 * sqrt(15 / pi)

	// Cosine-lobe irradiance convolution factors (Ramamoorthi & Hanrahan 2001).
	shConvA0 = math.Pi
	shConvA1 = 2.0 * math.Pi / 3.0
	shConvA2 = math.Pi / 4.0
)

// CubeFaceDirection returns the unit 3D direction corresponding to face-local coordinates (u, v) in [0, 1] on face ([gpu.CubeFacePosX]..[gpu.CubeFaceNegZ]) under the canonical WebGPU / Metal / Direct3D / OpenGL cubemap conventions.
func CubeFaceDirection(face gpu.CubeFace, u, v float32) math3d.Vec3 {
	s := 2*float64(u) - 1
	t := 2*float64(v) - 1
	var x, y, z float64
	switch face {
	case gpu.CubeFacePosX:
		x, y, z = 1, -t, -s
	case gpu.CubeFaceNegX:
		x, y, z = -1, -t, s
	case gpu.CubeFacePosY:
		x, y, z = s, 1, t
	case gpu.CubeFaceNegY:
		x, y, z = s, -1, -t
	case gpu.CubeFacePosZ:
		x, y, z = s, -t, 1
	case gpu.CubeFaceNegZ:
		x, y, z = -s, -t, -1
	default:
		x, y, z = 1, -t, -s
	}
	inv := 1.0 / math.Sqrt(x*x+y*y+z*z)
	return math3d.Vec3{
		X: float32(x * inv),
		Y: float32(y * inv),
		Z: float32(z * inv),
	}
}

// CubeDirectionToFaceUV maps a 3D direction to its cubemap face index ([gpu.CubeFacePosX]..[gpu.CubeFaceNegZ]) and face-local [0, 1] coordinates, matching the NSGo GPU cubemap sampler convention.
func CubeDirectionToFaceUV(dir math3d.Vec3) (face gpu.CubeFace, u, v float32) {
	x, y, z := float64(dir.X), float64(dir.Y), float64(dir.Z)
	ax, ay, az := math.Abs(x), math.Abs(y), math.Abs(z)
	var sc, tc, ma float64
	switch {
	case ax >= ay && ax >= az:
		ma = ax
		if x >= 0 {
			face = gpu.CubeFacePosX
			sc, tc = -z, -y
		} else {
			face = gpu.CubeFaceNegX
			sc, tc = z, -y
		}
	case ay >= ax && ay >= az:
		ma = ay
		if y >= 0 {
			face = gpu.CubeFacePosY
			sc, tc = x, z
		} else {
			face = gpu.CubeFaceNegY
			sc, tc = x, -z
		}
	default:
		ma = az
		if z >= 0 {
			face = gpu.CubeFacePosZ
			sc, tc = x, -y
		} else {
			face = gpu.CubeFaceNegZ
			sc, tc = -x, -y
		}
	}
	if ma < 1e-12 {
		return gpu.CubeFacePosX, 0.5, 0.5
	}
	return face, float32(0.5 * (sc/ma + 1.0)), float32(0.5 * (tc/ma + 1.0))
}

// Cubemap holds a 6-face square cubemap on the CPU in canonical WebGPU / Metal / Direct3D / OpenGL order ([gpu.CubeFacePosX]..[gpu.CubeFaceNegZ]), ready to upload with [UploadCubemap] or process into IBL data with [ComputeIrradianceSH] and [PrefilterEnvironment].
//
// Faces holds mip level 0: six non-nil square RGBA images of equal dimensions. Mips optionally holds additional precomputed mip levels starting at level 1 (so Mips[0] is mip level 1 of edge max(Size>>1, 1)), such as GGX-prefiltered specular environment levels. When SRGB is true, texel RGB channels are sRGB-encoded and CPU integration decodes to linear before filtering.
//
// Texels are 8 bits per channel, so a Cubemap holds a low dynamic range environment: radiance above 1 is clamped, here and in the faces [PrefilterEnvironment] and [IrradianceSH.ToCubemap] write, which flattens bright sun or sky highlights in image-based lighting. High dynamic range environments are not supported yet.
type Cubemap struct {
	Faces [6]*image.RGBA
	Mips  [][6]*image.RGBA
	SRGB  bool
}

// Validate checks that c is non-nil, every base face is non-nil and square with matching positive dimensions, and every optional mip level has the exact expected dimensions down to 1x1. Every error wraps [ErrInvalidCubemap].
func (c *Cubemap) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: nil cubemap", ErrInvalidCubemap)
	}
	var size int
	for face := range 6 {
		img := c.Faces[face]
		if img == nil {
			return fmt.Errorf("%w: face %d is nil", ErrInvalidCubemap, face)
		}
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		if w <= 0 || h <= 0 {
			return fmt.Errorf("%w: face %d has empty bounds %v", ErrInvalidCubemap, face, b)
		}
		if w != h {
			return fmt.Errorf("%w: face %d must be square, got %dx%d", ErrInvalidCubemap, face, w, h)
		}
		if face == 0 {
			size = w
		} else if w != size {
			return fmt.Errorf("%w: face %d size %d does not match face 0 size %d", ErrInvalidCubemap, face, w, size)
		}
	}
	totalMips := 1 + len(c.Mips)
	maxMips := int(gpu.MaxMipLevels(uint32(size), uint32(size)))
	if totalMips > maxMips {
		return fmt.Errorf("%w: %d mip levels exceed maximum %d for size %d", ErrInvalidCubemap, totalMips, maxMips, size)
	}
	for m, level := range c.Mips {
		wantW, wantH := gpu.MipSize(uint32(size), uint32(size), uint32(m+1))
		for face := range 6 {
			img := level[face]
			if img == nil {
				return fmt.Errorf("%w: mip %d face %d is nil", ErrInvalidCubemap, m+1, face)
			}
			b := img.Bounds()
			if uint32(b.Dx()) != wantW || uint32(b.Dy()) != wantH {
				return fmt.Errorf("%w: mip %d face %d is %dx%d, want %dx%d", ErrInvalidCubemap, m+1, face, b.Dx(), b.Dy(), wantW, wantH)
			}
		}
	}
	return nil
}

// Size returns the edge length in texels of mip level 0, or 0 if invalid.
func (c *Cubemap) Size() int {
	if c == nil || c.Faces[0] == nil {
		return 0
	}
	return c.Faces[0].Bounds().Dx()
}

// MipLevels returns the total number of mip levels stored in c (1 when Mips is empty), or 0 if c has no base face.
func (c *Cubemap) MipLevels() int {
	if c == nil || c.Faces[0] == nil {
		return 0
	}
	return 1 + len(c.Mips)
}

func (c *Cubemap) faceAtMip(mip int, face gpu.CubeFace) *image.RGBA {
	if mip <= 0 {
		return c.Faces[face]
	}
	idx := mip - 1
	if idx >= len(c.Mips) {
		idx = len(c.Mips) - 1
	}
	if idx < 0 {
		return c.Faces[face]
	}
	return c.Mips[idx][face]
}

// Sample bilinearly samples mip level 0 of c in direction dir and returns linear RGB values.
func (c *Cubemap) Sample(dir math3d.Vec3) [3]float32 {
	if c == nil || c.Faces[0] == nil {
		return [3]float32{}
	}
	face, u, v := CubeDirectionToFaceUV(dir)
	return sampleRGBAFace(c.Faces[face], u, v, c.SRGB)
}

// SampleLOD trilinearly samples c at mip level lod (clamped to [0, MipLevels-1]) in direction dir and returns linear RGB values.
func (c *Cubemap) SampleLOD(dir math3d.Vec3, lod float32) [3]float32 {
	if c == nil || c.Faces[0] == nil {
		return [3]float32{}
	}
	face, u, v := CubeDirectionToFaceUV(dir)
	maxMip := len(c.Mips)
	if maxMip == 0 || lod <= 0 || math.IsNaN(float64(lod)) {
		return sampleRGBAFace(c.Faces[face], u, v, c.SRGB)
	}
	fl := float64(lod)
	if fl >= float64(maxMip) {
		return sampleRGBAFace(c.faceAtMip(maxMip, face), u, v, c.SRGB)
	}
	lo := int(math.Floor(fl))
	hi := min(lo+1, maxMip)
	frac := float32(fl - float64(lo))
	c0 := sampleRGBAFace(c.faceAtMip(lo, face), u, v, c.SRGB)
	if lo == hi || frac <= 0 {
		return c0
	}
	c1 := sampleRGBAFace(c.faceAtMip(hi, face), u, v, c.SRGB)
	return [3]float32{
		c0[0] + (c1[0]-c0[0])*frac,
		c0[1] + (c1[1]-c0[1])*frac,
		c0[2] + (c1[2]-c0[2])*frac,
	}
}

// CubemapFromImages copies six square [image.Image] faces (+X, -X, +Y, -Y, +Z, -Z) into a normalized [Cubemap]. Every face must be non-nil, square, and of identical resolution.
func CubemapFromImages(faces [6]image.Image, srgb bool) (*Cubemap, error) {
	var size int
	out := &Cubemap{SRGB: srgb}
	for i, src := range faces {
		if src == nil {
			return nil, fmt.Errorf("%w: face %d is nil", ErrInvalidCubemap, i)
		}
		b := src.Bounds()
		w, h := b.Dx(), b.Dy()
		if w <= 0 || h <= 0 {
			return nil, fmt.Errorf("%w: face %d has empty bounds %v", ErrInvalidCubemap, i, b)
		}
		if w != h {
			return nil, fmt.Errorf("%w: face %d must be square, got %dx%d", ErrInvalidCubemap, i, w, h)
		}
		if i == 0 {
			size = w
		} else if w != size {
			return nil, fmt.Errorf("%w: face %d size %d does not match face 0 size %d", ErrInvalidCubemap, i, w, size)
		}
		out.Faces[i] = imageToRGBA(src)
	}
	return out, nil
}

// CubemapFromEquirectangular projects a latitude-longitude (equirectangular) panorama into a 6-face cubemap of resolution faceSize x faceSize per face using seamless horizontal wrapping and bilinear filtering.
//
// Longitude u = 0.5 + atan2(z, x) / (2*pi) maps +X to u = 0.5 and +Z to u = 0.75; latitude v = acos(y) / pi maps +Y (zenith) to the top row (v = 0) and -Y (nadir) to the bottom row (v = 1). Texels are copied unchanged, and srgb marks them as sRGB-encoded, as in [CubemapFromImages]: pass true for a typical PNG or JPEG panorama.
func CubemapFromEquirectangular(img image.Image, faceSize int, srgb bool) (*Cubemap, error) {
	if img == nil {
		return nil, fmt.Errorf("%w: nil equirectangular image", ErrInvalidCubemap)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%w: empty equirectangular image bounds %v", ErrInvalidCubemap, b)
	}
	if faceSize <= 0 {
		return nil, fmt.Errorf("%w: faceSize %d must be positive", ErrInvalidCubemap, faceSize)
	}
	src := imageToRGBA(img)
	cm := &Cubemap{SRGB: srgb}
	invSize := 1.0 / float64(faceSize)
	for face := range gpu.CubeFace(6) {
		dst := image.NewRGBA(image.Rect(0, 0, faceSize, faceSize))
		for py := range faceSize {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range faceSize {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				phi := math.Atan2(float64(d.Z), float64(d.X))
				theta := math.Acos(clamp64(float64(d.Y), -1, 1))
				eu := 0.5 + phi/(2*math.Pi)
				ev := theta / math.Pi
				r, g, bl, a := sampleEquirectBilinear(src, w, h, eu, ev)
				off := dst.PixOffset(px, py)
				dst.Pix[off+0] = r
				dst.Pix[off+1] = g
				dst.Pix[off+2] = bl
				dst.Pix[off+3] = a
			}
		}
		cm.Faces[face] = dst
	}
	return cm, nil
}

// GradientSkyCubemap builds a procedural 3-band sky cubemap interpolating smoothly from horizon to zenith across the upper hemisphere (y >= 0) and from horizon to ground across the lower hemisphere (y < 0). Colors are linear RGB values; they must be finite and non-negative, and the RGBA8 faces clamp them to [0, 1].
func GradientSkyCubemap(zenith, horizon, ground [3]float32, faceSize int) (*Cubemap, error) {
	if faceSize <= 0 {
		return nil, fmt.Errorf("%w: faceSize %d must be positive", ErrInvalidCubemap, faceSize)
	}
	for _, c := range [...][3]float32{zenith, horizon, ground} {
		if !finite(c[0]) || !finite(c[1]) || !finite(c[2]) || c[0] < 0 || c[1] < 0 || c[2] < 0 {
			return nil, fmt.Errorf("%w: gradient sky colors must be finite and non-negative", ErrInvalidCubemap)
		}
	}
	cm := &Cubemap{}
	invSize := 1.0 / float64(faceSize)
	for face := range gpu.CubeFace(6) {
		dst := image.NewRGBA(image.Rect(0, 0, faceSize, faceSize))
		for py := range faceSize {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range faceSize {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				var col [3]float64
				if d.Y >= 0 {
					t := math.Pow(clamp64(float64(d.Y), 0, 1), 0.6)
					col = lerpRGB64(horizon, zenith, t)
				} else {
					t := math.Pow(clamp64(float64(-d.Y), 0, 1), 0.45)
					col = lerpRGB64(horizon, ground, t)
				}
				off := dst.PixOffset(px, py)
				dst.Pix[off+0] = floatToByte(col[0])
				dst.Pix[off+1] = floatToByte(col[1])
				dst.Pix[off+2] = floatToByte(col[2])
				dst.Pix[off+3] = 255
			}
		}
		cm.Faces[face] = dst
	}
	return cm, nil
}

// CubemapUpload configures [UploadCubemap].
type CubemapUpload struct {
	// Label names the GPU texture and sampler in GPU capture tools.
	Label string
	// Format overrides the GPU texture format. When [gpu.FormatUndefined], [gpu.FormatRGBA8UnormSRGB] is selected when cm.SRGB is true and [gpu.FormatRGBA8Unorm] otherwise.
	Format gpu.TextureFormat
	// GenerateMips builds a full 2x2 box-filtered mip chain on the CPU before uploading when cm only carries mip level 0. Ignored when cm already has precomputed Mips (such as from [PrefilterEnvironment]).
	GenerateMips bool
	// Retained keeps CPU shadow copies of the texture and sampler so NSGo recreates them automatically after a device loss.
	Retained bool
}

// GPUCubemap is a 6-face cubemap texture and its trilinear clamp-to-edge sampler uploaded to an NSGo [gpu.Device]. Its resources belong to the [core.Scope] passed to [UploadCubemap] and are released when that scope is disposed or when [GPUCubemap.Close] is called.
type GPUCubemap struct {
	tex       *gpu.Texture
	sampler   *gpu.Sampler
	size      uint32
	mipLevels uint32
	format    gpu.TextureFormat
}

// UploadCubemap validates cm and uploads all six faces and their mip chain to dev, scoped to cx. A malformed cubemap returns an error wrapping [ErrInvalidCubemap] before creating any GPU resources.
func UploadCubemap(cx *core.Scope, dev *gpu.Device, cm *Cubemap, opts CubemapUpload) (*GPUCubemap, error) {
	if err := cm.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	format := opts.Format
	if format == gpu.FormatUndefined {
		if cm.SRGB {
			format = gpu.FormatRGBA8UnormSRGB
		} else {
			format = gpu.FormatRGBA8Unorm
		}
	}
	if format != gpu.FormatRGBA8Unorm && format != gpu.FormatRGBA8UnormSRGB {
		return nil, fmt.Errorf("%w: unsupported upload format %s (expected RGBA8Unorm or RGBA8UnormSRGB)", ErrInvalidCubemap, format)
	}

	size := uint32(cm.Size())
	var levels [][6]*image.RGBA
	if len(cm.Mips) > 0 {
		levels = make([][6]*image.RGBA, 1+len(cm.Mips))
		levels[0] = cm.Faces
		copy(levels[1:], cm.Mips)
	} else if opts.GenerateMips {
		var perFace [6][]*image.RGBA
		for face := range 6 {
			perFace[face] = gpu.GenerateMipChainRGBA(cm.Faces[face], 0)
		}
		nMips := len(perFace[0])
		levels = make([][6]*image.RGBA, nMips)
		for lvl := range nMips {
			for face := range 6 {
				levels[lvl][face] = perFace[face][lvl]
			}
		}
	} else {
		levels = [][6]*image.RGBA{cm.Faces}
	}
	mipLevels := uint32(len(levels))

	texLabel := opts.Label
	if texLabel == "" {
		texLabel = "render3d.cubemap"
	}
	tex, err := dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     texLabel,
		Dimension: gpu.DimCube,
		Width:     size,
		Height:    size,
		Layers:    6,
		MipLevels: mipLevels,
		Format:    format,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	for lvl, faces := range levels {
		for face := range gpu.CubeFace(6) {
			img := faces[face]
			if img.Bounds().Min != (image.Point{}) {
				img = imageToRGBA(img)
			}
			dev.WriteCubeFaceRGBA(tex, face, uint32(lvl), img)
		}
	}
	samp, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:        texLabel + " sampler",
		MinFilter:    gpu.FilterLinear,
		MagFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Retained:     opts.Retained,
	})
	if err != nil {
		_ = tex.Close()
		return nil, err
	}
	return &GPUCubemap{
		tex:       tex,
		sampler:   samp,
		size:      size,
		mipLevels: mipLevels,
		format:    format,
	}, nil
}

// Texture returns the underlying [gpu.DimCube] texture.
func (g *GPUCubemap) Texture() *gpu.Texture {
	if g == nil {
		return nil
	}
	return g.tex
}

// Sampler returns the trilinear clamp-to-edge sampler created for the cubemap.
func (g *GPUCubemap) Sampler() *gpu.Sampler {
	if g == nil {
		return nil
	}
	return g.sampler
}

// Size returns the base mip edge length in texels.
func (g *GPUCubemap) Size() uint32 {
	if g == nil {
		return 0
	}
	return g.size
}

// MipLevels returns the number of uploaded mip levels.
func (g *GPUCubemap) MipLevels() uint32 {
	if g == nil {
		return 0
	}
	return g.mipLevels
}

// MaxLOD returns float32(MipLevels - 1), the maximum valid LOD index for textureSampleLevel.
func (g *GPUCubemap) MaxLOD() float32 {
	if g == nil || g.mipLevels <= 1 {
		return 0
	}
	return float32(g.mipLevels - 1)
}

// Format returns the GPU texture format.
func (g *GPUCubemap) Format() gpu.TextureFormat {
	if g == nil {
		return gpu.FormatUndefined
	}
	return g.format
}

// ByteSize returns the GPU memory occupied by all six faces and all mip levels.
func (g *GPUCubemap) ByteSize() uint64 {
	if g == nil || g.size == 0 || g.mipLevels == 0 {
		return 0
	}
	var total uint64
	for lvl := range g.mipLevels {
		mw, mh := gpu.MipSize(g.size, g.size, lvl)
		total += 6 * g.format.ByteSize(mw, mh)
	}
	return total
}

// Valid reports whether both the texture and sampler are open and belong to a live device.
func (g *GPUCubemap) Valid() bool {
	return g != nil && g.tex.Valid() && g.sampler.Valid()
}

// Close releases the sampler and texture immediately. Safe to call more than once.
func (g *GPUCubemap) Close() error {
	if g == nil {
		return nil
	}
	var err error
	if g.sampler != nil {
		err = errors.Join(err, g.sampler.Close())
	}
	if g.tex != nil {
		err = errors.Join(err, g.tex.Close())
	}
	return err
}

// SkyboxUniform is the per-frame data [SkyboxWGSL] reads: the inverse of Proj * ViewRotation (camera view with translation stripped) plus exposure and mip LOD.
type SkyboxUniform struct {
	InvViewProj math3d.Mat4
	Exposure    float32
	LOD         float32
}

// NewSkyboxUniform builds a [SkyboxUniform] from cam's current rotation and projection with Exposure = 1 and LOD = 0. Camera translation is removed so the skybox stays infinitely far away as the viewer moves.
func NewSkyboxUniform(cam *scene3d.Camera) SkyboxUniform {
	if cam == nil {
		return SkyboxUniform{InvViewProj: math3d.Mat4Identity(), Exposure: 1}
	}
	return NewSkyboxUniformFromMatrices(cam.ViewMatrix(), cam.ProjMatrix())
}

// NewSkyboxUniformFromMatrices builds a [SkyboxUniform] from raw view and projection matrices with translation stripped from view, Exposure = 1, and LOD = 0.
func NewSkyboxUniformFromMatrices(view, proj math3d.Mat4) SkyboxUniform {
	viewRot := view
	viewRot[12], viewRot[13], viewRot[14], viewRot[15] = 0, 0, 0, 1
	rotVP := proj.Mul(viewRot)
	inv, ok := rotVP.Inverse()
	if !ok {
		inv = math3d.Mat4Identity()
	}
	return SkyboxUniform{
		InvViewProj: inv,
		Exposure:    1,
		LOD:         0,
	}
}

// Encode writes u into dst as the little-endian [SkyboxUniformSize]-byte block declared by [SkyboxWGSL] and returns dst[:SkyboxUniformSize]. It panics if len(dst) < SkyboxUniformSize.
func (u *SkyboxUniform) Encode(dst []byte) []byte {
	if len(dst) < SkyboxUniformSize {
		panic("render3d: SkyboxUniform.Encode: dst shorter than SkyboxUniformSize")
	}
	dst = dst[:SkyboxUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	for _, f := range u.InvViewProj {
		put(f)
	}
	put(u.Exposure)
	put(u.LOD)
	put(0)
	put(0)
	return dst
}

// SkyboxPipelineDesc configures [NewSkyboxPipeline].
type SkyboxPipelineDesc struct {
	// Label names the GPU pipeline, uniform buffer, and layout in capture tools.
	Label string
	// ColorFormat is the render target color format; defaults to dev.SurfaceFormat() when [gpu.FormatUndefined].
	ColorFormat gpu.TextureFormat
	// DepthFormat is the depth attachment format (for example [gpu.FormatDepth24Plus]). Leave [gpu.FormatUndefined] for color-only passes. When set, the pipeline uses [gpu.CompareLessEqual] with DepthWrite disabled and [SkyboxWGSL] emits depth 1 - 2^-24, so the skybox drawn after opaque geometry stays behind everything nearer than the far plane.
	DepthFormat gpu.TextureFormat
	// SampleCount is the MSAA sample count (0 or 1 for non-MSAA, 4 for 4x MSAA).
	SampleCount int
	// Retained opts the pipeline and uniform buffer into automatic recreation after a GPU device loss.
	Retained bool
}

// SkyboxPipeline renders any [GPUCubemap] as a fullscreen background skybox without a vertex buffer. Create it once in OnReady, bind a cubemap with [SkyboxPipeline.BindCubemap], update the view each frame with [SkyboxPipeline.WriteCamera] or [SkyboxPipeline.Write], and record the draw with [SkyboxPipeline.Draw].
type SkyboxPipeline struct {
	dev     *gpu.Device
	pipe    *gpu.Pipeline
	layout  *gpu.BindGroupLayout
	uniform *gpu.Buffer
	scratch [SkyboxUniformSize]byte
}

// NewSkyboxPipeline compiles [SkyboxWGSL] and creates the skybox render pipeline, bind group layout, and uniform buffer on dev, scoped to cx.
func NewSkyboxPipeline(cx *core.Scope, dev *gpu.Device, desc SkyboxPipelineDesc) (*SkyboxPipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := desc.Label
	if label == "" {
		label = "render3d.skybox"
	}
	colorFmt := desc.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	mod, err := gpu.CompileWGSL(SkyboxWGSL)
	if err != nil {
		return nil, err
	}
	initUniform := SkyboxUniform{InvViewProj: math3d.Mat4Identity(), Exposure: 1}
	var initBytes [SkyboxUniformSize]byte
	uBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " uniform",
		Size:     SkyboxUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     initUniform.Encode(initBytes[:]),
		Retained: desc.Retained,
	})
	if err != nil {
		return nil, err
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = uBuf.Close()
		return nil, err
	}
	pDesc := gpu.PipelineDesc{
		Label:       label,
		Vertex:      gpu.ShaderStage{Module: mod, EntryPoint: "vs_skybox"},
		Fragment:    gpu.ShaderStage{Module: mod, EntryPoint: "fs_skybox"},
		Layouts:     []*gpu.BindGroupLayout{layout},
		Primitive:   gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullNone},
		Targets:     []gpu.ColorTargetState{{Format: colorFmt, WriteMask: gpu.ColorWriteAll}},
		SampleCount: desc.SampleCount,
		Retained:    desc.Retained,
	}
	if desc.DepthFormat != gpu.FormatUndefined {
		pDesc.DepthStencil = &gpu.DepthStencilState{
			Format:       desc.DepthFormat,
			DepthWrite:   false,
			DepthCompare: gpu.CompareLessEqual,
		}
	}
	pipe, err := dev.CreatePipeline(cx, pDesc)
	if err != nil {
		_ = layout.Close()
		_ = uBuf.Close()
		return nil, err
	}
	return &SkyboxPipeline{
		dev:     dev,
		pipe:    pipe,
		layout:  layout,
		uniform: uBuf,
	}, nil
}

// BindCubemap creates a [gpu.BindGroup] pairing the pipeline's uniform buffer with cube's texture and sampler. When both the pipeline and cube were created with Retained: true, NSGo restores this bind group automatically after a device loss.
func (s *SkyboxPipeline) BindCubemap(cx *core.Scope, cube *GPUCubemap) (*gpu.BindGroup, error) {
	if s == nil || !s.Valid() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: skybox pipeline is not valid"}
	}
	if !cube.Valid() {
		return nil, fmt.Errorf("%w: cubemap is nil or closed", ErrInvalidCubemap)
	}
	return s.dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  "render3d.skybox group",
		Layout: s.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: s.uniform, Size: SkyboxUniformSize},
			{Binding: 1, Texture: cube.tex},
			{Binding: 2, Sampler: cube.sampler},
		},
	})
}

// SetDevice updates the device handle used by [SkyboxPipeline.Write] and [SkyboxPipeline.BindCubemap] after a retained pipeline is restored on a new device in OnReady.
func (s *SkyboxPipeline) SetDevice(dev *gpu.Device) {
	if s != nil && dev != nil {
		s.dev = dev
	}
}

// Write uploads u to the skybox uniform buffer. It encodes into storage the SkyboxPipeline owns, so the only copy made is the one gpu.Device.WriteBuffer takes of the bytes it queues.
func (s *SkyboxPipeline) Write(u SkyboxUniform) {
	if s == nil || s.dev == nil || !s.uniform.Valid() {
		return
	}
	s.dev.WriteBuffer(s.uniform, 0, u.Encode(s.scratch[:]))
}

// WriteCamera uploads cam's rotation and projection to the skybox uniform buffer with Exposure = 1 and LOD = 0.
func (s *SkyboxPipeline) WriteCamera(cam *scene3d.Camera) {
	s.Write(NewSkyboxUniform(cam))
}

// Draw binds the skybox pipeline and cubemap bind group and issues the single fullscreen-triangle draw (3 vertices, no vertex buffer). It records nothing if either the pipeline or group is invalid.
func (s *SkyboxPipeline) Draw(pass *gpu.RenderPass, group *gpu.BindGroup) {
	if pass == nil || !s.Valid() || group == nil || !group.Valid() {
		return
	}
	pass.SetPipeline(s.pipe)
	pass.SetBindGroup(0, group)
	pass.Draw(3, 0)
}

// Pipeline returns the underlying [gpu.Pipeline].
func (s *SkyboxPipeline) Pipeline() *gpu.Pipeline {
	if s == nil {
		return nil
	}
	return s.pipe
}

// Layout returns the skybox [gpu.BindGroupLayout].
func (s *SkyboxPipeline) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.layout
}

// Valid reports whether the pipeline, layout, and uniform buffer are open and belong to a live device.
func (s *SkyboxPipeline) Valid() bool {
	return s != nil && s.pipe.Valid() && s.layout.Valid() && s.uniform.Valid()
}

// Close releases the pipeline, layout, and uniform buffer. Safe to call more than once.
func (s *SkyboxPipeline) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.pipe != nil {
		err = errors.Join(err, s.pipe.Close())
	}
	if s.layout != nil {
		err = errors.Join(err, s.layout.Close())
	}
	if s.uniform != nil {
		err = errors.Join(err, s.uniform.Close())
	}
	return err
}

// IrradianceSH stores the 9 pre-convolved L2 real spherical harmonic RGB coefficients representing diffuse cosine-weighted irradiance E(n) = integral L(w) * max(n·w, 0) dw (Ramamoorthi & Hanrahan 2001).
//
// For a uniform environment with linear radiance C, [IrradianceSH.Evaluate] returns pi * C at every normal; Lambertian diffuse shading multiplies by albedo / pi.
type IrradianceSH struct {
	Coeff [9][3]float32
}

// ComputeIrradianceSH projects env's base mip level onto L2 spherical harmonics using exact per-texel cubemap solid angles (which sum to exactly 4*pi across all six faces) and applies the Lambertian cosine-lobe convolution weights (pi, 2*pi/3, pi/4).
func ComputeIrradianceSH(env *Cubemap) (IrradianceSH, error) {
	if err := env.Validate(); err != nil {
		return IrradianceSH{}, err
	}
	size := env.Size()
	invSize := 1.0 / float64(size)
	var accum [9][3]float64
	for face := range gpu.CubeFace(6) {
		img := env.Faces[face]
		b := img.Bounds()
		for py := range size {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range size {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				dw := cubemapTexelSolidAngle(px, py, size)
				off := img.PixOffset(b.Min.X+px, b.Min.Y+py)
				r, g, bl := decodeTexelRGB(img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], env.SRGB)

				x, y, z := float64(d.X), float64(d.Y), float64(d.Z)
				basis := [9]float64{
					shBasisC0,
					shBasisC1 * y,
					shBasisC1 * z,
					shBasisC1 * x,
					shBasisC2 * x * y,
					shBasisC2 * y * z,
					shBasisC3 * (3*z*z - 1),
					shBasisC2 * x * z,
					shBasisC4 * (x*x - y*y),
				}
				for k := range 9 {
					w := basis[k] * dw
					accum[k][0] += r * w
					accum[k][1] += g * w
					accum[k][2] += bl * w
				}
			}
		}
	}
	conv := [9]float64{
		shConvA0,
		shConvA1, shConvA1, shConvA1,
		shConvA2, shConvA2, shConvA2, shConvA2, shConvA2,
	}
	var out IrradianceSH
	for k := range 9 {
		for ch := range 3 {
			out.Coeff[k][ch] = float32(accum[k][ch] * conv[k])
		}
	}
	return out, nil
}

// Evaluate evaluates the diffuse irradiance E(normal) from sh, matching [IBLWGSL]'s evaluate_irradiance_sh. Negative values from high-contrast ringing are clamped to zero.
func (sh IrradianceSH) Evaluate(normal math3d.Vec3) [3]float32 {
	l := normal.Len()
	if l < 1e-12 {
		return [3]float32{}
	}
	d := normal.Scale(1 / l)
	x, y, z := float64(d.X), float64(d.Y), float64(d.Z)
	basis := [9]float64{
		shBasisC0,
		shBasisC1 * y,
		shBasisC1 * z,
		shBasisC1 * x,
		shBasisC2 * x * y,
		shBasisC2 * y * z,
		shBasisC3 * (3*z*z - 1),
		shBasisC2 * x * z,
		shBasisC4 * (x*x - y*y),
	}
	var res [3]float64
	for k := range 9 {
		bk := basis[k]
		res[0] += float64(sh.Coeff[k][0]) * bk
		res[1] += float64(sh.Coeff[k][1]) * bk
		res[2] += float64(sh.Coeff[k][2]) * bk
	}
	return [3]float32{
		float32(max(res[0], 0)),
		float32(max(res[1], 0)),
		float32(max(res[2], 0)),
	}
}

// ToCubemap renders sh into a low-resolution diffuse radiance cubemap storing E(n) / pi per texel (so a uniform [0, 1] environment maps back into [0, 1] without clipping).
func (sh IrradianceSH) ToCubemap(faceSize int) (*Cubemap, error) {
	if faceSize <= 0 {
		return nil, fmt.Errorf("%w: faceSize %d must be positive", ErrInvalidCubemap, faceSize)
	}
	cm := &Cubemap{}
	invSize := 1.0 / float64(faceSize)
	const invPi = 1.0 / math.Pi
	for face := range gpu.CubeFace(6) {
		dst := image.NewRGBA(image.Rect(0, 0, faceSize, faceSize))
		for py := range faceSize {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range faceSize {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				irr := sh.Evaluate(d)
				off := dst.PixOffset(px, py)
				dst.Pix[off+0] = floatToByte(float64(irr[0]) * invPi)
				dst.Pix[off+1] = floatToByte(float64(irr[1]) * invPi)
				dst.Pix[off+2] = floatToByte(float64(irr[2]) * invPi)
				dst.Pix[off+3] = 255
			}
		}
		cm.Faces[face] = dst
	}
	return cm, nil
}

// PrefilterOptions configures [PrefilterEnvironment].
type PrefilterOptions struct {
	// BaseSize is the resolution of mip level 0 (roughness = 0). Defaults to env.Size() when <= 0.
	BaseSize int
	// MipLevels is the number of roughness mip levels to generate, spanning roughness 0.0 (mip 0) through 1.0 (mip MipLevels-1). Defaults to min(MaxMipLevels(BaseSize, BaseSize), 5) when <= 0.
	MipLevels int
	// SampleCount is the number of Hammersley GGX importance samples per texel on roughness > 0 levels. Defaults to 64 when <= 0.
	SampleCount int
}

// PrefilterEnvironment builds a split-sum GGX prefiltered specular cubemap mip chain from env (Karis 2013). Mip level 0 corresponds to roughness 0 (mirror reflection) and mip level MipLevels-1 corresponds to roughness 1. The process is deterministic (Hammersley quasi-Monte-Carlo) and uses filtered source mip lookups to suppress fireflies.
func PrefilterEnvironment(env *Cubemap, opts PrefilterOptions) (*Cubemap, error) {
	if err := env.Validate(); err != nil {
		return nil, err
	}
	baseSize := opts.BaseSize
	if baseSize <= 0 {
		baseSize = env.Size()
	}
	maxMips := int(gpu.MaxMipLevels(uint32(baseSize), uint32(baseSize)))
	mipLevels := opts.MipLevels
	if mipLevels <= 0 {
		mipLevels = min(maxMips, 5)
	}
	if mipLevels > maxMips {
		return nil, fmt.Errorf("%w: requested %d prefilter mip levels exceeds maximum %d for base size %d", ErrInvalidCubemap, mipLevels, maxMips, baseSize)
	}
	sampleCount := opts.SampleCount
	if sampleCount <= 0 {
		sampleCount = 64
	}

	// Build an internal linear float32 mip pyramid of the source environment so GGX importance samples can use solid-angle filtered LOD lookups.
	srcPyr := buildLinearCubemapPyramid(env)
	srcBaseSize := float64(env.Size())
	saTexel := 4.0 * math.Pi / (6.0 * srcBaseSize * srcBaseSize)

	out := &Cubemap{SRGB: env.SRGB}
	if mipLevels > 1 {
		out.Mips = make([][6]*image.RGBA, mipLevels-1)
	}

	for lvl := range mipLevels {
		mwU, _ := gpu.MipSize(uint32(baseSize), uint32(baseSize), uint32(lvl))
		mw := int(mwU)
		invW := 1.0 / float64(mw)
		var roughness float64
		if mipLevels > 1 {
			roughness = float64(lvl) / float64(mipLevels-1)
		}
		var faces [6]*image.RGBA
		for face := range gpu.CubeFace(6) {
			dst := image.NewRGBA(image.Rect(0, 0, mw, mw))
			for py := range mw {
				v := float32((float64(py) + 0.5) * invW)
				for px := range mw {
					u := float32((float64(px) + 0.5) * invW)
					n := CubeFaceDirection(face, u, v)
					var col [3]float64
					if lvl == 0 || roughness <= 1e-4 {
						col = srcPyr.sampleLOD(n, 0)
					} else {
						col = prefilterTexelGGX(srcPyr, n, roughness, sampleCount, saTexel)
					}
					off := dst.PixOffset(px, py)
					dst.Pix[off+0] = encodeTexelByte(col[0], env.SRGB)
					dst.Pix[off+1] = encodeTexelByte(col[1], env.SRGB)
					dst.Pix[off+2] = encodeTexelByte(col[2], env.SRGB)
					dst.Pix[off+3] = 255
				}
			}
			faces[face] = dst
		}
		if lvl == 0 {
			out.Faces = faces
		} else {
			out.Mips[lvl-1] = faces
		}
	}
	return out, nil
}

// GenerateBRDFLUT generates a square 2D split-sum GGX BRDF integration lookup table (NoV along X in [0, 1], roughness along Y in [0, 1]), storing the Fresnel scale in R and bias in G using 128 Hammersley samples per texel.
func GenerateBRDFLUT(size int) (*image.RGBA, error) {
	return GenerateBRDFLUTWithSamples(size, 128)
}

// GenerateBRDFLUTWithSamples generates a 2D split-sum GGX BRDF integration lookup table with an explicit Hammersley sample count per texel.
func GenerateBRDFLUTWithSamples(size, sampleCount int) (*image.RGBA, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: BRDF LUT size %d must be positive", ErrInvalidCubemap, size)
	}
	if sampleCount <= 0 {
		sampleCount = 128
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	invSize := 1.0 / float64(size)
	for py := range size {
		roughness := clamp64((float64(py)+0.5)*invSize, 1e-3, 1.0)
		for px := range size {
			nDotV := clamp64((float64(px)+0.5)*invSize, 1e-3, 1.0)
			scale, bias := integrateBRDFTexel(nDotV, roughness, sampleCount)
			off := img.PixOffset(px, py)
			img.Pix[off+0] = floatToByte(scale)
			img.Pix[off+1] = floatToByte(bias)
			img.Pix[off+2] = 0
			img.Pix[off+3] = 255
		}
	}
	return img, nil
}

// EnvBRDFApprox evaluates the Karis (2014) analytical split-sum GGX BRDF approximation, matching [IBLWGSL]'s env_brdf_approx.
func EnvBRDFApprox(f0 [3]float32, roughness, nDotV float32) [3]float32 {
	r := float64(roughness)
	nov := float64(nDotV)
	rx := r*(-1.0) + 1.0
	ry := r*(-0.0275) + 0.0425
	rz := r*(-0.572) + 1.04
	rw := r*(0.022) - 0.04
	a004 := math.Min(rx*rx, math.Exp2(-9.28*nov))*rx + ry
	ax := -1.04*a004 + rz
	ay := 1.04*a004 + rw
	return [3]float32{
		float32(float64(f0[0])*ax + ay),
		float32(float64(f0[1])*ax + ay),
		float32(float64(f0[2])*ax + ay),
	}
}

// EvaluateIBL evaluates the exact CPU reference for [IBLWGSL]'s evaluate_ibl function: L2 spherical harmonic diffuse irradiance combined with split-sum GGX prefiltered specular reflection using [EnvBRDFApprox]. A NaN or infinite metallic, roughness, ao or intensity counts as 0, and a negative intensity as 0, so the result stays finite.
func EvaluateIBL(
	sh IrradianceSH,
	prefilter *Cubemap,
	intensity float32,
	normal, viewDir math3d.Vec3,
	baseColor [3]float32,
	metallic, roughness, ao float32,
) [3]float32 {
	metallic, roughness, ao, intensity = finiteOrZero(metallic), finiteOrZero(roughness), finiteOrZero(ao), max(finiteOrZero(intensity), 0)
	n := normal.Norm()
	v := viewDir.Norm()
	m := float32(clamp64(float64(metallic), 0, 1))
	rough := float32(clamp64(float64(roughness), 0, 1))
	nDotV := max(n.Dot(v), 0)
	f0 := [3]float32{
		0.04 + (baseColor[0]-0.04)*m,
		0.04 + (baseColor[1]-0.04)*m,
		0.04 + (baseColor[2]-0.04)*m,
	}
	f := fresnelSchlickRoughness(nDotV, f0, rough)
	kD := [3]float32{
		(1 - f[0]) * (1 - m),
		(1 - f[1]) * (1 - m),
		(1 - f[2]) * (1 - m),
	}
	irr := sh.Evaluate(n)
	const invPi = float32(1.0 / math.Pi)
	diffuse := [3]float32{
		kD[0] * baseColor[0] * irr[0] * invPi,
		kD[1] * baseColor[1] * irr[1] * invPi,
		kD[2] * baseColor[2] * irr[2] * invPi,
	}
	// reflect(-v, n) = 2 * dot(n, v) * n - v
	rDir := n.Scale(2 * n.Dot(v)).Sub(v)
	var prefiltered [3]float32
	if prefilter != nil {
		maxLOD := float32(max(prefilter.MipLevels()-1, 0))
		prefiltered = prefilter.SampleLOD(rDir, rough*maxLOD)
	}
	brdf := EnvBRDFApprox(f0, rough, nDotV)
	specular := [3]float32{
		prefiltered[0] * brdf[0],
		prefiltered[1] * brdf[1],
		prefiltered[2] * brdf[2],
	}
	scale := ao * intensity
	return [3]float32{
		(diffuse[0] + specular[0]) * scale,
		(diffuse[1] + specular[1]) * scale,
		(diffuse[2] + specular[2]) * scale,
	}
}

// IBLUniform holds the diffuse L2 spherical harmonic irradiance coefficients and specular prefilter parameters bound by [GPUIBLBuffer] and read by [IBLWGSL].
type IBLUniform struct {
	SH        IrradianceSH
	Intensity float32
	MaxLOD    float32
}

// NewIBLUniform populates an [IBLUniform] from sh, prefilter's maximum mip LOD, and intensity.
func NewIBLUniform(sh IrradianceSH, prefilter *GPUCubemap, intensity float32) IBLUniform {
	var maxLOD float32
	if prefilter != nil {
		maxLOD = prefilter.MaxLOD()
	}
	return IBLUniform{
		SH:        sh,
		Intensity: intensity,
		MaxLOD:    maxLOD,
	}
}

// Validate reports whether every spherical harmonic coefficient is finite and Intensity and MaxLOD are finite and non-negative. Every error wraps [ErrInvalidIBL].
func (u IBLUniform) Validate() error {
	for k := range 9 {
		for ch := range 3 {
			if !finite(u.SH.Coeff[k][ch]) {
				return fmt.Errorf("%w: spherical harmonic coefficient %d channel %d is not finite", ErrInvalidIBL, k, ch)
			}
		}
	}
	if !finite(u.Intensity) || u.Intensity < 0 {
		return fmt.Errorf("%w: intensity must be finite and non-negative", ErrInvalidIBL)
	}
	if !finite(u.MaxLOD) || u.MaxLOD < 0 {
		return fmt.Errorf("%w: max LOD must be finite and non-negative", ErrInvalidIBL)
	}
	return nil
}

// Encode writes u into dst as the little-endian [IBLUniformSize]-byte block matching [IBLWGSL] and returns dst[:IBLUniformSize]. It panics if len(dst) < IBLUniformSize.
func (u *IBLUniform) Encode(dst []byte) []byte {
	if len(dst) < IBLUniformSize {
		panic("render3d: IBLUniform.Encode: dst shorter than IBLUniformSize")
	}
	dst = dst[:IBLUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	for k := range 9 {
		put(u.SH.Coeff[k][0])
		put(u.SH.Coeff[k][1])
		put(u.SH.Coeff[k][2])
		put(0)
	}
	put(u.Intensity)
	put(u.MaxLOD)
	put(0)
	put(0)
	return dst
}

// IBLBufferOptions configures [NewGPUIBLBuffer].
type IBLBufferOptions struct {
	// Label names the buffer, bind group layout, and bind group in GPU capture tools.
	Label string
	// Retained keeps a CPU shadow copy so NSGo recreates the uniform buffer, layout, and bind group with their latest data after a device loss. The prefiltered cubemap must be retained too.
	Retained bool
}

// GPUIBLBuffer holds the uniform buffer, bind group layout, and bind group that expose diffuse L2 spherical harmonic irradiance and a prefiltered specular cubemap to shaders that prepend [IBLWGSL] (or [IBLWGSLForGroup]).
//
// Create it once when the device is ready, call [GPUIBLBuffer.Update] when the environment or intensity changes, and bind it at [IBLGroup] for [NewLitPBRIBLPipeline] ([GPUIBLBuffer.Bind]) or at the group a custom shader chose with [IBLWGSLForGroup]. Its resources belong to the scope passed to [NewGPUIBLBuffer] and are released with it, or earlier by Close.
type GPUIBLBuffer struct {
	dev     *gpu.Device
	buf     *gpu.Buffer
	layout  *gpu.BindGroupLayout
	group   *gpu.BindGroup
	scratch [IBLUniformSize]byte
}

// NewGPUIBLBuffer validates initial and creates the IBL uniform buffer, bind group layout, and bind group on dev, owned by cx (nil for resources closed manually), binding the uniform at binding 0, prefilter's cubemap at binding 1, and its sampler at binding 2. An invalid uniform returns an error wrapping [ErrInvalidIBL], and a nil or closed prefilter one wrapping [ErrInvalidCubemap], before any GPU resource is created.
func NewGPUIBLBuffer(cx *core.Scope, dev *gpu.Device, prefilter *GPUCubemap, initial IBLUniform, opts IBLBufferOptions) (*GPUIBLBuffer, error) {
	if err := initial.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !prefilter.Valid() {
		return nil, fmt.Errorf("%w: prefilter cubemap is nil or closed", ErrInvalidCubemap)
	}
	label := opts.Label
	if label == "" {
		label = "render3d.ibl"
	}
	b := &GPUIBLBuffer{dev: dev}
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Size:     IBLUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     initial.Encode(b.scratch[:]),
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label,
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label,
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: buf, Size: IBLUniformSize},
			{Binding: 1, Texture: prefilter.tex},
			{Binding: 2, Sampler: prefilter.sampler},
		},
	})
	if err != nil {
		_ = layout.Close()
		_ = buf.Close()
		return nil, err
	}
	b.buf = buf
	b.layout = layout
	b.group = group
	return b, nil
}

// Update validates u and writes it to the uniform buffer; the only copy made is the one gpu.Device.WriteBuffer takes of the bytes it queues. Pass the active [gpu.Device] (for example frame.Device()) so a retained buffer keeps accepting updates after a device loss and restore; passing nil reuses the last device.
func (b *GPUIBLBuffer) Update(dev *gpu.Device, u IBLUniform) error {
	if err := u.Validate(); err != nil {
		return err
	}
	if !b.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: IBL buffer is not valid"}
	}
	if dev != nil {
		b.dev = dev
	}
	if b.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	b.dev.WriteBuffer(b.buf, 0, u.Encode(b.scratch[:]))
	return nil
}

// Buffer returns the underlying uniform buffer, or nil when b is nil.
func (b *GPUIBLBuffer) Buffer() *gpu.Buffer {
	if b == nil {
		return nil
	}
	return b.buf
}

// Layout returns the bind group layout a pipeline lists at [IBLGroup] (or the group chosen with [IBLWGSLForGroup]).
func (b *GPUIBLBuffer) Layout() *gpu.BindGroupLayout {
	if b == nil {
		return nil
	}
	return b.layout
}

// BindGroup returns the bind group a render pass sets at [IBLGroup] (or the group chosen with [IBLWGSLForGroup]).
func (b *GPUIBLBuffer) BindGroup() *gpu.BindGroup {
	if b == nil {
		return nil
	}
	return b.group
}

// Bind sets b's bind group at [IBLGroup] on pass. It records nothing when b is not [GPUIBLBuffer.Valid].
func (b *GPUIBLBuffer) Bind(pass *gpu.RenderPass) {
	if !b.Valid() {
		return
	}
	pass.SetBindGroup(IBLGroup, b.group)
}

// ByteSize returns the GPU memory held by the IBL uniform buffer (for [RenderStats] and [Budget] accounting), or zero for a nil buffer. The prefiltered cubemap is counted by its own [GPUCubemap.ByteSize].
func (b *GPUIBLBuffer) ByteSize() uint64 {
	if b == nil || b.buf == nil {
		return 0
	}
	return b.buf.Size()
}

// Valid reports whether the uniform buffer, layout, and bind group are open and belong to a live device.
func (b *GPUIBLBuffer) Valid() bool {
	return b != nil && b.buf.Valid() && b.layout.Valid() && b.group.Valid()
}

// Close releases the bind group, layout, and uniform buffer now instead of waiting for their owning scope. It is safe to call on nil or more than once.
func (b *GPUIBLBuffer) Close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.group != nil {
		err = errors.Join(err, b.group.Close())
	}
	if b.layout != nil {
		err = errors.Join(err, b.layout.Close())
	}
	if b.buf != nil {
		err = errors.Join(err, b.buf.Close())
	}
	return err
}

// LitPBRIBLWGSL is [LitPBRWGSL] with image-based lighting: the same vertex stage, bind groups [CameraGroup], [LightGroup] and [PBRDrawGroup], and direct lighting, plus a [GPUIBLBuffer] at [IBLGroup]. The fragment stage adds [IBLWGSL]'s evaluate_ibl (diffuse spherical harmonic irradiance and prefiltered specular, with ambient occlusion 1) to the directly lit color before encoding to sRGB, so [ShadePoint] plus [EvaluateIBL] reproduces it on the CPU. Set the [LightRig]'s ambient light to zero when the environment provides the ambient term. Use [NewLitPBRIBLPipeline] to create a pipeline from it.
//
// IBL and fog both use bind group 3 ([IBLGroup] and [FogGroup]), the last of the four groups every backend guarantees, so this module has no fog.
const LitPBRIBLWGSL = litPBRPreludeWGSL + "\n" + IBLWGSL + litPBRFragmentHeadWGSL + `	let ibl_lit = lit + evaluate_ibl(in.world_normal, view_dir, pbr_draw.base_color.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y, 1.0);
	return vec4<f32>(linear_to_srgb(ibl_lit), pbr_draw.base_color.a);
}
`

// litPBRIBLLinearWGSL is [LitPBRIBLWGSL] writing the linear color without sRGB encoding, compiled by [NewLitPBRPipelineWithOptions] when [LitPBRPipelineOptions].LinearOutput and IBLLayout are set.
const litPBRIBLLinearWGSL = litPBRPreludeWGSL + "\n" + IBLWGSL + litPBRFragmentHeadWGSL + `	let ibl_lit = lit + evaluate_ibl(in.world_normal, view_dir, pbr_draw.base_color.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y, 1.0);
	return vec4<f32>(ibl_lit, pbr_draw.base_color.a);
}
`

// NewLitPBRIBLPipeline compiles [LitPBRIBLWGSL] and creates a pipeline configured like [NewLitPBRPipeline] with iblLayout ([GPUIBLBuffer.Layout]) added at [IBLGroup].
func NewLitPBRIBLPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, iblLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	layouts := make([]*gpu.BindGroupLayout, IBLGroup+1)
	layouts[CameraGroup] = camLayout
	layouts[LightGroup] = lightLayout
	layouts[PBRDrawGroup] = drawLayout
	layouts[IBLGroup] = iblLayout
	return newLitPBRPipeline(cx, dev, "render3d.lit_pbr_ibl", LitPBRIBLWGSL, layouts, MeshVertexLayout(), dev.SurfaceFormat(), depthFormat)
}

// --- Internal math and sampling helpers ---

func cubemapAreaElement(x, y float64) float64 {
	return math.Atan2(x*y, math.Sqrt(x*x+y*y+1.0))
}

// cubemapTexelSolidAngle returns the exact analytical solid angle subtended by texel (px, py) on one face of a size x size cubemap. Summing across all 6 * size * size texels yields 4*pi to floating-point precision.
func cubemapTexelSolidAngle(px, py, size int) float64 {
	inv := 1.0 / float64(size)
	u0 := 2.0*float64(px)*inv - 1.0
	v0 := 2.0*float64(py)*inv - 1.0
	u1 := 2.0*float64(px+1)*inv - 1.0
	v1 := 2.0*float64(py+1)*inv - 1.0
	return cubemapAreaElement(u0, v0) - cubemapAreaElement(u0, v1) - cubemapAreaElement(u1, v0) + cubemapAreaElement(u1, v1)
}

func srgbToLinear(c float64) float64 {
	c = clamp64(c, 0, 1)
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSRGB(c float64) float64 {
	c = clamp64(c, 0, 1)
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

func decodeTexelRGB(r, g, b byte, srgb bool) (float64, float64, float64) {
	fr := float64(r) / 255.0
	fg := float64(g) / 255.0
	fb := float64(b) / 255.0
	if srgb {
		return srgbToLinear(fr), srgbToLinear(fg), srgbToLinear(fb)
	}
	return fr, fg, fb
}

func encodeTexelByte(v float64, srgb bool) byte {
	if srgb {
		v = linearToSRGB(v)
	}
	return floatToByte(v)
}

func floatToByte(v float64) byte {
	v = clamp64(v, 0, 1)
	return byte(math.Round(v * 255.0))
}

// finiteOrZero returns v, or 0 when v is NaN or infinite.
func finiteOrZero(v float32) float32 {
	if !finite(v) {
		return 0
	}
	return v
}

func clamp64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lerpRGB64(a, b [3]float32, t float64) [3]float64 {
	return [3]float64{
		float64(a[0]) + (float64(b[0])-float64(a[0]))*t,
		float64(a[1]) + (float64(b[1])-float64(a[1]))*t,
		float64(a[2]) + (float64(b[2])-float64(a[2]))*t,
	}
}

func imageToRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if rgba, ok := src.(*image.RGBA); ok {
		for y := range h {
			sOff := rgba.PixOffset(b.Min.X, b.Min.Y+y)
			dOff := dst.PixOffset(0, y)
			copy(dst.Pix[dOff:dOff+w*4], rgba.Pix[sOff:sOff+w*4])
		}
		return dst
	}
	if nrgba, ok := src.(*image.NRGBA); ok {
		for y := range h {
			sOff := nrgba.PixOffset(b.Min.X, b.Min.Y+y)
			dOff := dst.PixOffset(0, y)
			copy(dst.Pix[dOff:dOff+w*4], nrgba.Pix[sOff:sOff+w*4])
		}
		return dst
	}
	for y := range h {
		for x := range w {
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = c.R
			dst.Pix[off+1] = c.G
			dst.Pix[off+2] = c.B
			dst.Pix[off+3] = c.A
		}
	}
	return dst
}

func sampleRGBAFace(img *image.RGBA, u, v float32, srgb bool) [3]float32 {
	if img == nil {
		return [3]float32{}
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return [3]float32{}
	}
	fx := float64(u)*float64(w) - 0.5
	fy := float64(v)*float64(h) - 0.5
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	tx := fx - float64(x0)
	ty := fy - float64(y0)
	x1 := clampInt(x0+1, 0, w-1)
	y1 := clampInt(y0+1, 0, h-1)
	x0 = clampInt(x0, 0, w-1)
	y0 = clampInt(y0, 0, h-1)

	get := func(x, y int) (float64, float64, float64) {
		off := img.PixOffset(b.Min.X+x, b.Min.Y+y)
		return decodeTexelRGB(img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], srgb)
	}
	r00, g00, b00 := get(x0, y0)
	r10, g10, b10 := get(x1, y0)
	r01, g01, b01 := get(x0, y1)
	r11, g11, b11 := get(x1, y1)

	w00 := (1 - tx) * (1 - ty)
	w10 := tx * (1 - ty)
	w01 := (1 - tx) * ty
	w11 := tx * ty
	return [3]float32{
		float32(r00*w00 + r10*w10 + r01*w01 + r11*w11),
		float32(g00*w00 + g10*w10 + g01*w01 + g11*w11),
		float32(b00*w00 + b10*w10 + b01*w01 + b11*w11),
	}
}

func sampleEquirectBilinear(img *image.RGBA, w, h int, u, v float64) (byte, byte, byte, byte) {
	u = u - math.Floor(u)
	v = clamp64(v, 0, 1)
	fx := u*float64(w) - 0.5
	fy := v*float64(h) - 0.5
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	tx := fx - float64(x0)
	ty := fy - float64(y0)

	wrapX := func(x int) int {
		r := x % w
		if r < 0 {
			r += w
		}
		return r
	}
	sx0, sx1 := wrapX(x0), wrapX(x0+1)
	sy0, sy1 := clampInt(y0, 0, h-1), clampInt(y0+1, 0, h-1)

	i00 := img.PixOffset(sx0, sy0)
	i10 := img.PixOffset(sx1, sy0)
	i01 := img.PixOffset(sx0, sy1)
	i11 := img.PixOffset(sx1, sy1)

	w00 := (1 - tx) * (1 - ty)
	w10 := tx * (1 - ty)
	w01 := (1 - tx) * ty
	w11 := tx * ty

	var out [4]byte
	for ch := range 4 {
		val := float64(img.Pix[i00+ch])*w00 +
			float64(img.Pix[i10+ch])*w10 +
			float64(img.Pix[i01+ch])*w01 +
			float64(img.Pix[i11+ch])*w11
		out[ch] = byte(clamp64(math.Round(val), 0, 255))
	}
	return out[0], out[1], out[2], out[3]
}

type linearCubeLevel struct {
	size  int
	faces [6][][3]float64
}

type linearCubePyramid struct {
	levels []linearCubeLevel
}

func buildLinearCubemapPyramid(env *Cubemap) *linearCubePyramid {
	size := env.Size()
	nMips := int(gpu.MaxMipLevels(uint32(size), uint32(size)))
	levels := make([]linearCubeLevel, nMips)

	// Level 0: decode base cubemap faces to linear float64.
	lvl0 := linearCubeLevel{size: size}
	for face := range 6 {
		img := env.Faces[face]
		b := img.Bounds()
		pix := make([][3]float64, size*size)
		for y := range size {
			for x := range size {
				off := img.PixOffset(b.Min.X+x, b.Min.Y+y)
				r, g, bl := decodeTexelRGB(img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], env.SRGB)
				pix[y*size+x] = [3]float64{r, g, bl}
			}
		}
		lvl0.faces[face] = pix
	}
	levels[0] = lvl0

	// Box-filter down to 1x1 in linear space.
	for l := 1; l < nMips; l++ {
		prev := levels[l-1]
		sw := prev.size
		dw := max(sw/2, 1)
		cur := linearCubeLevel{size: dw}
		for face := range 6 {
			src := prev.faces[face]
			dst := make([][3]float64, dw*dw)
			for dy := range dw {
				sy0 := min(dy*2, sw-1)
				sy1 := min(sy0+1, sw-1)
				for dx := range dw {
					sx0 := min(dx*2, sw-1)
					sx1 := min(sx0+1, sw-1)
					c00 := src[sy0*sw+sx0]
					c10 := src[sy0*sw+sx1]
					c01 := src[sy1*sw+sx0]
					c11 := src[sy1*sw+sx1]
					dst[dy*dw+dx] = [3]float64{
						0.25 * (c00[0] + c10[0] + c01[0] + c11[0]),
						0.25 * (c00[1] + c10[1] + c01[1] + c11[1]),
						0.25 * (c00[2] + c10[2] + c01[2] + c11[2]),
					}
				}
			}
			cur.faces[face] = dst
		}
		levels[l] = cur
	}
	return &linearCubePyramid{levels: levels}
}

func (p *linearCubePyramid) sampleLevel(lvl int, face gpu.CubeFace, u, v float32) [3]float64 {
	lv := p.levels[lvl]
	w := lv.size
	fx := float64(u)*float64(w) - 0.5
	fy := float64(v)*float64(w) - 0.5
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	tx := fx - float64(x0)
	ty := fy - float64(y0)
	x1 := clampInt(x0+1, 0, w-1)
	y1 := clampInt(y0+1, 0, w-1)
	x0 = clampInt(x0, 0, w-1)
	y0 = clampInt(y0, 0, w-1)
	pix := lv.faces[face]
	c00 := pix[y0*w+x0]
	c10 := pix[y0*w+x1]
	c01 := pix[y1*w+x0]
	c11 := pix[y1*w+x1]
	w00 := (1 - tx) * (1 - ty)
	w10 := tx * (1 - ty)
	w01 := (1 - tx) * ty
	w11 := tx * ty
	return [3]float64{
		c00[0]*w00 + c10[0]*w10 + c01[0]*w01 + c11[0]*w11,
		c00[1]*w00 + c10[1]*w10 + c01[1]*w01 + c11[1]*w11,
		c00[2]*w00 + c10[2]*w10 + c01[2]*w01 + c11[2]*w11,
	}
}

func (p *linearCubePyramid) sampleLOD(dir math3d.Vec3, lod float64) [3]float64 {
	face, u, v := CubeDirectionToFaceUV(dir)
	maxLvl := len(p.levels) - 1
	if lod <= 0 || maxLvl == 0 {
		return p.sampleLevel(0, face, u, v)
	}
	if lod >= float64(maxLvl) {
		return p.sampleLevel(maxLvl, face, u, v)
	}
	lo := int(math.Floor(lod))
	hi := min(lo+1, maxLvl)
	frac := lod - float64(lo)
	c0 := p.sampleLevel(lo, face, u, v)
	if lo == hi || frac <= 0 {
		return c0
	}
	c1 := p.sampleLevel(hi, face, u, v)
	return [3]float64{
		c0[0] + (c1[0]-c0[0])*frac,
		c0[1] + (c1[1]-c0[1])*frac,
		c0[2] + (c1[2]-c0[2])*frac,
	}
}

func radicalInverseVdC(bits uint32) float64 {
	bits = (bits << 16) | (bits >> 16)
	bits = ((bits & 0x55555555) << 1) | ((bits & 0xAAAAAAAA) >> 1)
	bits = ((bits & 0x33333333) << 2) | ((bits & 0xCCCCCCCC) >> 2)
	bits = ((bits & 0x0F0F0F0F) << 4) | ((bits & 0xF0F0F0F0) >> 4)
	bits = ((bits & 0x00FF00FF) << 8) | ((bits & 0xFF00FF00) >> 8)
	return float64(bits) * 2.3283064365386963e-10 // 1 / 2^32
}

func hammersley2D(i, n int) (float64, float64) {
	return float64(i) / float64(n), radicalInverseVdC(uint32(i))
}

func importanceSampleGGX(xi1, xi2, roughness float64, n math3d.Vec3) math3d.Vec3 {
	a := roughness * roughness
	phi := 2.0 * math.Pi * xi1
	cosTheta := math.Sqrt((1.0 - xi2) / (1.0 + (a*a-1.0)*xi2))
	sinTheta := math.Sqrt(max(1.0-cosTheta*cosTheta, 0.0))

	hx := math.Cos(phi) * sinTheta
	hy := math.Sin(phi) * sinTheta
	hz := cosTheta

	var up math3d.Vec3
	if math.Abs(float64(n.Z)) < 0.999 {
		up = math3d.Vec3{Z: 1}
	} else {
		up = math3d.Vec3{X: 1}
	}
	tangent := up.Cross(n).Norm()
	bitangent := n.Cross(tangent)

	h := tangent.Scale(float32(hx)).Add(bitangent.Scale(float32(hy))).Add(n.Scale(float32(hz)))
	return h.Norm()
}

func distributionGGX(nDotH, roughness float64) float64 {
	a := roughness * roughness
	a2 := a * a
	denom := nDotH*nDotH*(a2-1.0) + 1.0
	denom = math.Pi * denom * denom
	if denom < 1e-12 {
		return 0
	}
	return a2 / denom
}

func prefilterTexelGGX(src *linearCubePyramid, n math3d.Vec3, roughness float64, sampleCount int, saTexel float64) [3]float64 {
	v := n
	var accum [3]float64
	var totalWeight float64
	for i := range sampleCount {
		xi1, xi2 := hammersley2D(i, sampleCount)
		h := importanceSampleGGX(xi1, xi2, roughness, n)
		vDotH := float64(v.Dot(h))
		l := h.Scale(float32(2.0 * vDotH)).Sub(v).Norm()
		nDotL := float64(n.Dot(l))
		if nDotL <= 0 {
			continue
		}
		nDotH := max(float64(n.Dot(h)), 0.0)
		hDotV := max(vDotH, 0.0)
		d := distributionGGX(nDotH, roughness)
		pdf := d*nDotH/(4.0*hDotV) + 1e-4
		saSample := 1.0 / (float64(sampleCount)*pdf + 1e-4)
		lod := 0.5 * math.Log2(saSample/saTexel)
		if lod < 0 || math.IsNaN(lod) {
			lod = 0
		}
		c := src.sampleLOD(l, lod)
		accum[0] += c[0] * nDotL
		accum[1] += c[1] * nDotL
		accum[2] += c[2] * nDotL
		totalWeight += nDotL
	}
	if totalWeight <= 1e-12 {
		return src.sampleLOD(n, 0)
	}
	invW := 1.0 / totalWeight
	return [3]float64{accum[0] * invW, accum[1] * invW, accum[2] * invW}
}

func geometrySchlickGGXIBL(nDotV, roughness float64) float64 {
	k := (roughness * roughness) * 0.5
	return nDotV / (nDotV*(1.0-k) + k)
}

func geometrySmithIBL(nDotV, nDotL, roughness float64) float64 {
	return geometrySchlickGGXIBL(nDotV, roughness) * geometrySchlickGGXIBL(nDotL, roughness)
}

func integrateBRDFTexel(nDotV, roughness float64, sampleCount int) (scale, bias float64) {
	vx := math.Sqrt(max(1.0-nDotV*nDotV, 0.0))
	vz := nDotV
	v := math3d.Vec3{X: float32(vx), Z: float32(vz)}
	n := math3d.Vec3{Z: 1}
	var a, b float64
	for i := range sampleCount {
		xi1, xi2 := hammersley2D(i, sampleCount)
		h := importanceSampleGGX(xi1, xi2, roughness, n)
		vDotH := float64(v.Dot(h))
		l := h.Scale(float32(2.0 * vDotH)).Sub(v).Norm()
		nDotL := max(float64(l.Z), 0.0)
		nDotH := max(float64(h.Z), 0.0)
		hDotV := max(vDotH, 0.0)
		if nDotL > 0 && nDotH > 1e-6 {
			g := geometrySmithIBL(nDotV, nDotL, roughness)
			gVis := (g * hDotV) / (nDotH * nDotV)
			fc := math.Pow(1.0-hDotV, 5.0)
			a += (1.0 - fc) * gVis
			b += fc * gVis
		}
	}
	inv := 1.0 / float64(sampleCount)
	return clamp64(a*inv, 0, 1), clamp64(b*inv, 0, 1)
}

func fresnelSchlickRoughness(cosTheta float32, f0 [3]float32, roughness float32) [3]float32 {
	t := float32(math.Pow(clamp64(1.0-float64(cosTheta), 0, 1), 5.0))
	oneMinusR := 1 - roughness
	return [3]float32{
		f0[0] + (max(oneMinusR, f0[0])-f0[0])*t,
		f0[1] + (max(oneMinusR, f0[1])-f0[1])*t,
		f0[2] + (max(oneMinusR, f0[2])-f0[2])*t,
	}
}
