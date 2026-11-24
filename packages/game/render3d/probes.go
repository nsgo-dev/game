package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidProbe is wrapped by errors returned when probe geometry, cubemap
// data, light-probe grid dimensions, or shader bindings are malformed.
var ErrInvalidProbe = errors.New("render3d: invalid probe")

const (
	// ProbeGroup is the bind group index [ProbeRenderer] and [ProbeWGSL] use for
	// probe uniforms, reflection cubemaps, sky cubemap, and sampler alongside
	// [CameraGroup] at group 0.
	ProbeGroup = 1

	// ProbeCubeFaceCount is the six canonical faces of a cubemap
	// (+X, -X, +Y, -Y, +Z, -Z).
	ProbeCubeFaceCount = 6

	// ProbeUniformSize is the byte size of the ProbeData uniform block that
	// [ProbeUniform.Encode] writes and [ProbeWGSL] declares (1 mat4 + 18 vec4).
	ProbeUniformSize = 64 + 18*16
)

// SH9 cosine-lobe normalization and orthonormal real spherical harmonic constants
// (Ramamoorthi & Hanrahan 2001).
const (
	shY0     = 0.28209479177387814 // 1/2 * sqrt(1/pi)
	shY1     = 0.4886025119029199  // 1/2 * sqrt(3/pi)
	shY2xy   = 1.0925484305920792  // 1/2 * sqrt(15/pi)
	shY2z2   = 0.31539156525252005 // 1/4 * sqrt(5/pi)
	shY2x2y2 = 0.5462742152960396  // 1/4 * sqrt(15/pi)

	// Normalized cosine-lobe weights A_l / pi: A0/pi = 1, A1/pi = 2/3, A2/pi = 1/4.
	shCos0 = 1.0
	shCos1 = 2.0 / 3.0
	shCos2 = 0.25
)

// ProbeWGSL is the portable WGSL shader used by [ProbeRenderer]. It combines
// [CameraWGSL] at group 0 with [ProbeGroup] (group 1) to shade meshes with:
//   - Diffuse indirect lighting from interpolated L2 [SH9] light-probe coefficients.
//   - Specular reflections blending up to two local parallax-corrected reflection
//     probe cubemaps over a global sky cubemap using roughness-driven mip levels
//     and Schlick Fresnel.
//
// The shader avoids exp/exp2/log2 so it runs identically on NSGo's software GPU
// device as well as Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, and GLES 3.0.
const ProbeWGSL = CameraWGSL + `
struct ProbeData {
	model: mat4x4<f32>,
	albedo: vec4<f32>,
	material: vec4<f32>,
	probe0_pos: vec4<f32>,
	probe0_min: vec4<f32>,
	probe0_max: vec4<f32>,
	probe1_pos: vec4<f32>,
	probe1_min: vec4<f32>,
	probe1_max: vec4<f32>,
	sky_params: vec4<f32>,
	sh0: vec4<f32>,
	sh1: vec4<f32>,
	sh2: vec4<f32>,
	sh3: vec4<f32>,
	sh4: vec4<f32>,
	sh5: vec4<f32>,
	sh6: vec4<f32>,
	sh7: vec4<f32>,
	sh8: vec4<f32>,
}

@group(1) @binding(0) var<uniform> probe: ProbeData;
@group(1) @binding(1) var probe0_cube: texture_cube<f32>;
@group(1) @binding(2) var probe1_cube: texture_cube<f32>;
@group(1) @binding(3) var sky_cube: texture_cube<f32>;
@group(1) @binding(4) var probe_samp: sampler;

struct ProbeVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct ProbeVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world: vec3<f32>,
	@location(1) normal: vec3<f32>,
}

@vertex
fn vs_main(in: ProbeVertexIn) -> ProbeVertexOut {
	var out: ProbeVertexOut;
	let wpos = probe.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * wpos;
	out.world = wpos.xyz;
	out.normal = (probe.model * vec4<f32>(in.normal, 0.0)).xyz;
	return out;
}

fn slab_tmax(pos: f32, dir: f32, bmin: f32, bmax: f32) -> f32 {
	if (abs(dir) < 1e-6) {
		return 1e30;
	}
	let t1 = (bmin - pos) / dir;
	let t2 = (bmax - pos) / dir;
	return max(t1, t2);
}

fn box_project_dir(world_pos: vec3<f32>, refl_dir: vec3<f32>, probe_pos: vec3<f32>, box_min: vec3<f32>, box_max: vec3<f32>) -> vec3<f32> {
	let dir = normalize(refl_dir);
	let tx = slab_tmax(world_pos.x, dir.x, box_min.x, box_max.x);
	let ty = slab_tmax(world_pos.y, dir.y, box_min.y, box_max.y);
	let tz = slab_tmax(world_pos.z, dir.z, box_min.z, box_max.z);
	let dist = min(min(tx, ty), tz);
	if (dist <= 0.0 || dist > 1e29) {
		return dir;
	}
	let hit_pos = world_pos + dir * dist;
	let offset = hit_pos - probe_pos;
	if (dot(offset, offset) < 1e-12) {
		return dir;
	}
	return normalize(offset);
}

fn eval_sh9_diffuse(n: vec3<f32>) -> vec3<f32> {
	var c = probe.sh0.xyz * 0.28209479;
	c = c + probe.sh1.xyz * (0.32573501 * n.y);
	c = c + probe.sh2.xyz * (0.32573501 * n.z);
	c = c + probe.sh3.xyz * (0.32573501 * n.x);
	c = c + probe.sh4.xyz * (0.27313711 * n.x * n.y);
	c = c + probe.sh5.xyz * (0.27313711 * n.y * n.z);
	c = c + probe.sh6.xyz * (0.07884789 * (3.0 * n.z * n.z - 1.0));
	c = c + probe.sh7.xyz * (0.27313711 * n.x * n.z);
	c = c + probe.sh8.xyz * (0.13656855 * (n.x * n.x - n.y * n.y));
	return max(c, vec3<f32>(0.0, 0.0, 0.0));
}

@fragment
fn fs_main(in: ProbeVertexOut) -> @location(0) vec4<f32> {
	let n = normalize(in.normal);
	let v = normalize(camera.eye.xyz - in.world * camera.eye.w);
	let refl = reflect(-v, n);

	let roughness = clamp(probe.material.x, 0.0, 1.0);
	let metallic = clamp(probe.material.y, 0.0, 1.0);
	let f0_scalar = clamp(probe.material.z, 0.0, 1.0);
	let mip = roughness * probe.material.w;

	var dir0 = refl;
	if (probe.probe0_pos.w > 0.5) {
		dir0 = box_project_dir(in.world, refl, probe.probe0_pos.xyz, probe.probe0_min.xyz, probe.probe0_max.xyz);
	}
	var dir1 = refl;
	if (probe.probe1_pos.w > 0.5) {
		dir1 = box_project_dir(in.world, refl, probe.probe1_pos.xyz, probe.probe1_min.xyz, probe.probe1_max.xyz);
	}

	let spec0 = textureSampleLevel(probe0_cube, probe_samp, dir0, mip).rgb * probe.probe0_min.w;
	let spec1 = textureSampleLevel(probe1_cube, probe_samp, dir1, mip).rgb * probe.probe1_min.w;
	let spec_sky = textureSampleLevel(sky_cube, probe_samp, refl, mip).rgb * probe.sky_params.xyz;
	let spec_env = spec0 * probe.probe0_max.w + spec1 * probe.probe1_max.w + spec_sky * probe.sky_params.w;

	let ndv = max(dot(n, v), 0.0);
	let om = 1.0 - ndv;
	let om2 = om * om;
	let fresnel_pow = om2 * om2 * om;
	let f0 = mix(vec3<f32>(f0_scalar, f0_scalar, f0_scalar), probe.albedo.rgb, metallic);
	let smooth_cap = vec3<f32>(1.0 - roughness, 1.0 - roughness, 1.0 - roughness);
	let max_f = max(smooth_cap, f0);
	let ks = f0 + (max_f - f0) * fresnel_pow;
	let kd = (vec3<f32>(1.0, 1.0, 1.0) - ks) * (1.0 - metallic);

	let diffuse = kd * probe.albedo.rgb * eval_sh9_diffuse(n) * probe.sh0.w;
	let specular = ks * spec_env;
	let color = clamp(diffuse + specular, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	return vec4<f32>(color, probe.albedo.a);
}
`

// -----------------------------------------------------------------------------
// Cubemap Face Orientation & ProbeCubemap
// -----------------------------------------------------------------------------

// ProbeFaceViewProj builds the right-handed 90-degree FOV view and [0, 1]-depth
// projection matrices for rendering one cubemap face from pos. The right-handed
// orientations preserve counter-clockwise triangle winding so backface culling
// works unchanged; when reading back a WebGPU render target (top-down rows) to
// upload into a cubemap face, flip the rows vertically ([BakeReflectionProbeGPU]
// handles this automatically).
func ProbeFaceViewProj(pos math3d.Vec3, face gpu.CubeFace, near, far float32) (view, proj math3d.Mat4) {
	var target, up math3d.Vec3
	switch face {
	case gpu.CubeFacePosX:
		target = math3d.Vec3{X: 1}
		up = math3d.Vec3{Y: -1}
	case gpu.CubeFaceNegX:
		target = math3d.Vec3{X: -1}
		up = math3d.Vec3{Y: -1}
	case gpu.CubeFacePosY:
		target = math3d.Vec3{Y: 1}
		up = math3d.Vec3{Z: 1}
	case gpu.CubeFaceNegY:
		target = math3d.Vec3{Y: -1}
		up = math3d.Vec3{Z: -1}
	case gpu.CubeFacePosZ:
		target = math3d.Vec3{Z: 1}
		up = math3d.Vec3{Y: -1}
	case gpu.CubeFaceNegZ:
		target = math3d.Vec3{Z: -1}
		up = math3d.Vec3{Y: -1}
	default:
		target = math3d.Vec3{X: 1}
		up = math3d.Vec3{Y: -1}
	}
	if near <= 0 {
		near = 0.05
	}
	if far <= near {
		far = near + 100
	}
	view = math3d.LookAt(pos, pos.Add(target), up)
	proj = math3d.Perspective(math.Pi/2, 1, near, far)
	return view, proj
}

// ProbeFaceCamera returns a [CameraUniform] configured to render one cubemap
// face from pos with a 90-degree field of view.
func ProbeFaceCamera(pos math3d.Vec3, face gpu.CubeFace, near, far float32) CameraUniform {
	view, proj := ProbeFaceViewProj(pos, face, near, far)
	return CameraUniform{
		View:     view,
		Proj:     proj,
		ViewProj: proj.Mul(view),
		Eye:      pos.Vec4(1),
	}
}

// ProbeCubemapUpload configures [UploadProbeCubemap], [UploadProbeCubemapRGBA],
// [UploadProbeCubemapLinear], and [BakeReflectionProbe].
type ProbeCubemapUpload struct {
	Label        string
	SRGB         bool   // decode sRGB to linear when sampling on the GPU
	Mipmaps      bool   // build CPU box-filtered mip chain for all 6 faces
	MaxMipLevels uint32 // 0 generates the full chain down to 1x1
	Retained     bool   // preserve cubemap contents across device loss
}

// ProbeCubemap holds a 6-face [gpu.DimCube] texture uploaded to an NSGo
// [gpu.Device] together with a linear RGB CPU mirror for deterministic sampling
// and [SH9] light-probe baking.
type ProbeCubemap struct {
	tex       *gpu.Texture
	size      uint32
	mipLevels uint32
	format    gpu.TextureFormat
	linear    [ProbeCubeFaceCount][]float32 // 3 float32 (R,G,B) per texel
}

// UploadProbeCubemap converts six square [image.Image] faces in canonical
// cubemap order (+X, -X, +Y, -Y, +Z, -Z) and uploads them as a [ProbeCubemap].
func UploadProbeCubemap(cx *core.Scope, dev *gpu.Device, faces [ProbeCubeFaceCount]image.Image, opts ProbeCubemapUpload) (*ProbeCubemap, error) {
	var raw [ProbeCubeFaceCount][]byte
	var size int
	for f := range ProbeCubeFaceCount {
		buf, err := PixelBufferFromImage(faces[f])
		if err != nil {
			return nil, fmt.Errorf("%w: cubemap face %d: %v", ErrInvalidProbe, f, err)
		}
		if buf.Width != buf.Height {
			return nil, fmt.Errorf("%w: cubemap face %d dimensions %dx%d must be square", ErrInvalidProbe, f, buf.Width, buf.Height)
		}
		if f == 0 {
			size = buf.Width
		} else if buf.Width != size {
			return nil, fmt.Errorf("%w: cubemap face %d size %d does not match face 0 size %d", ErrInvalidProbe, f, buf.Width, size)
		}
		raw[f] = buf.Pixels
	}
	return UploadProbeCubemapRGBA(cx, dev, size, raw, opts)
}

// UploadProbeCubemapRGBA uploads six tightly packed RGBA8 face buffers
// (each size*size*4 bytes) into a [gpu.DimCube] [ProbeCubemap].
func UploadProbeCubemapRGBA(cx *core.Scope, dev *gpu.Device, size int, faces [ProbeCubeFaceCount][]byte, opts ProbeCubemapUpload) (*ProbeCubemap, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: cubemap size %d must be positive", ErrInvalidProbe, size)
	}
	need := size * size * 4
	for f := range ProbeCubeFaceCount {
		if len(faces[f]) < need {
			return nil, fmt.Errorf("%w: cubemap face %d has %d bytes, need %d", ErrInvalidProbe, f, len(faces[f]), need)
		}
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	format := gpu.FormatRGBA8Unorm
	if opts.SRGB {
		format = gpu.FormatRGBA8UnormSRGB
	}
	uSize := uint32(size)
	mipLevels := uint32(1)
	if opts.Mipmaps {
		mipLevels = gpu.MaxMipLevels(uSize, uSize)
		if opts.MaxMipLevels > 0 && opts.MaxMipLevels < mipLevels {
			mipLevels = opts.MaxMipLevels
		}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.probe_cubemap"
	}
	tex, err := dev.CreateTexture(cx, gpu.TextureDesc{
		Label:     label,
		Dimension: gpu.DimCube,
		Width:     uSize,
		Height:    uSize,
		Layers:    ProbeCubeFaceCount,
		MipLevels: mipLevels,
		Format:    format,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst | gpu.TextureCopySrc,
		Retained:  opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	var linear [ProbeCubeFaceCount][]float32
	for f := range ProbeCubeFaceCount {
		uploadCubeFaceLevels(dev, tex, gpu.CubeFace(f), faces[f][:need], size, format, mipLevels)
		linear[f] = rgbaBytesToLinearRGB(faces[f][:need], opts.SRGB)
	}
	return &ProbeCubemap{
		tex:       tex,
		size:      uSize,
		mipLevels: mipLevels,
		format:    format,
		linear:    linear,
	}, nil
}

// UploadProbeCubemapLinear encodes six linear RGB float32 face buffers
// (each size*size*3 float32s) and uploads them as a [ProbeCubemap], keeping the
// exact linear float32 values in CPU memory for [ProbeCubemap.SampleCPU] and
// [ProbeCubemap.BakeSH9].
func UploadProbeCubemapLinear(cx *core.Scope, dev *gpu.Device, size int, faces [ProbeCubeFaceCount][]float32, opts ProbeCubemapUpload) (*ProbeCubemap, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: cubemap size %d must be positive", ErrInvalidProbe, size)
	}
	need := size * size * 3
	var rgba [ProbeCubeFaceCount][]byte
	var linCopy [ProbeCubeFaceCount][]float32
	for f := range ProbeCubeFaceCount {
		if len(faces[f]) < need {
			return nil, fmt.Errorf("%w: linear cubemap face %d has %d floats, need %d", ErrInvalidProbe, f, len(faces[f]), need)
		}
		linCopy[f] = make([]float32, need)
		copy(linCopy[f], faces[f][:need])
		rgba[f] = linearRGBToRGBABytes(faces[f][:need], opts.SRGB)
	}
	cm, err := UploadProbeCubemapRGBA(cx, dev, size, rgba, opts)
	if err != nil {
		return nil, err
	}
	cm.linear = linCopy
	return cm, nil
}

// NewSolidProbeCubemap creates a [ProbeCubemap] where each of the six faces is
// filled with a constant linear RGB color in canonical face order (+X, -X, +Y,
// -Y, +Z, -Z).
func NewSolidProbeCubemap(cx *core.Scope, dev *gpu.Device, faceColors [ProbeCubeFaceCount][3]float32, size int, opts ProbeCubemapUpload) (*ProbeCubemap, error) {
	if size <= 0 {
		size = 4
	}
	var faces [ProbeCubeFaceCount][]float32
	n := size * size
	for f := range ProbeCubeFaceCount {
		buf := make([]float32, n*3)
		c := faceColors[f]
		for i := range n {
			buf[i*3+0] = c[0]
			buf[i*3+1] = c[1]
			buf[i*3+2] = c[2]
		}
		faces[f] = buf
	}
	return UploadProbeCubemapLinear(cx, dev, size, faces, opts)
}

// UpdateFaceRGBA replaces a single cubemap face on dev and regenerates its mip
// levels when the cubemap was created with mipmaps.
func (c *ProbeCubemap) UpdateFaceRGBA(dev *gpu.Device, face gpu.CubeFace, rgba []byte) error {
	if c == nil || !c.Valid() {
		return fmt.Errorf("%w: cubemap is not valid", ErrInvalidProbe)
	}
	if face >= ProbeCubeFaceCount {
		return fmt.Errorf("%w: invalid cubemap face %d", ErrInvalidProbe, face)
	}
	need := int(c.size) * int(c.size) * 4
	if len(rgba) < need {
		return fmt.Errorf("%w: face buffer has %d bytes, need %d", ErrInvalidProbe, len(rgba), need)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	uploadCubeFaceLevels(dev, c.tex, face, rgba[:need], int(c.size), c.format, c.mipLevels)
	c.linear[face] = rgbaBytesToLinearRGB(rgba[:need], c.format.IsSRGB())
	return nil
}

// Texture returns the underlying [gpu.DimCube] texture, or nil for a nil receiver.
func (c *ProbeCubemap) Texture() *gpu.Texture {
	if c == nil {
		return nil
	}
	return c.tex
}

// Size returns the width/height of each cubemap face at mip level 0.
func (c *ProbeCubemap) Size() uint32 {
	if c == nil {
		return 0
	}
	return c.size
}

// MipLevels returns the number of mip levels uploaded across each face.
func (c *ProbeCubemap) MipLevels() uint32 {
	if c == nil {
		return 0
	}
	return c.mipLevels
}

// Format returns the GPU texture format of the cubemap.
func (c *ProbeCubemap) Format() gpu.TextureFormat {
	if c == nil {
		return gpu.FormatUndefined
	}
	return c.format
}

// ByteSize returns the total GPU memory occupied by all 6 faces and mip levels.
func (c *ProbeCubemap) ByteSize() uint64 {
	if c == nil || c.size == 0 {
		return 0
	}
	var perFace uint64
	for lvl := range c.mipLevels {
		mw, mh := gpu.MipSize(c.size, c.size, lvl)
		perFace += c.format.ByteSize(mw, mh)
	}
	return perFace * ProbeCubeFaceCount
}

// Valid reports whether the cubemap texture is open on a live GPU device.
func (c *ProbeCubemap) Valid() bool {
	return c != nil && c.tex.Valid()
}

// Close releases the underlying [gpu.Texture]. Safe to call on nil or multiple times.
func (c *ProbeCubemap) Close() error {
	if c == nil || c.tex == nil {
		return nil
	}
	return c.tex.Close()
}

// LinearFaces returns the CPU linear RGB face buffers ([6][]float32, 3 floats per texel).
func (c *ProbeCubemap) LinearFaces() [ProbeCubeFaceCount][]float32 {
	if c == nil {
		return [ProbeCubeFaceCount][]float32{}
	}
	return c.linear
}

// SampleCPU samples the cubemap's linear CPU face data along dir with bilinear
// filtering, matching GPU cubemap face orientation.
func (c *ProbeCubemap) SampleCPU(dir math3d.Vec3) [3]float32 {
	if c == nil || c.size == 0 {
		return [3]float32{}
	}
	face, u, v := CubeDirectionToFaceUV(dir)
	pix := c.linear[face]
	s := int(c.size)
	if len(pix) < s*s*3 {
		return [3]float32{}
	}
	fx := float64(u)*float64(s) - 0.5
	fy := float64(v)*float64(s) - 0.5
	x0 := probeClampInt(int(math.Floor(fx)), 0, s-1)
	y0 := probeClampInt(int(math.Floor(fy)), 0, s-1)
	x1 := probeClampInt(x0+1, 0, s-1)
	y1 := probeClampInt(y0+1, 0, s-1)
	tx := float32(math.Max(0, math.Min(1, fx-math.Floor(fx))))
	ty := float32(math.Max(0, math.Min(1, fy-math.Floor(fy))))

	read := func(x, y int) [3]float32 {
		i := (y*s + x) * 3
		return [3]float32{pix[i], pix[i+1], pix[i+2]}
	}
	c00 := read(x0, y0)
	c10 := read(x1, y0)
	c01 := read(x0, y1)
	c11 := read(x1, y1)
	var out [3]float32
	for ch := range 3 {
		top := c00[ch]*(1-tx) + c10[ch]*tx
		bot := c01[ch]*(1-tx) + c11[ch]*tx
		out[ch] = top*(1-ty) + bot*ty
	}
	return out
}

// BakeSH9 integrates the cubemap's linear CPU face data into L2 [SH9]
// spherical-harmonic coefficients.
func (c *ProbeCubemap) BakeSH9() SH9 {
	if c == nil || c.size == 0 {
		return SH9{}
	}
	return BakeSH9FromCubemap(c.linear, int(c.size))
}

func uploadCubeFaceLevels(dev *gpu.Device, tex *gpu.Texture, face gpu.CubeFace, rgba []byte, size int, format gpu.TextureFormat, mipLevels uint32) {
	if mipLevels <= 1 {
		dev.WriteTextureMip(tex, 0, face, rgba, uint32(size*4))
		return
	}
	cur := rgba
	curSize := size
	dev.WriteTextureMip(tex, 0, face, cur, uint32(curSize*4))
	srgb := format.IsSRGB()
	for lvl := uint32(1); lvl < mipLevels; lvl++ {
		nextSize := max(1, curSize>>1)
		cur = downsamplePixelsBox(cur, curSize, curSize, nextSize, nextSize, 4, srgb)
		curSize = nextSize
		dev.WriteTextureMip(tex, lvl, face, cur, uint32(curSize*4))
	}
}

func rgbaBytesToLinearRGB(rgba []byte, srgb bool) []float32 {
	n := len(rgba) / 4
	out := make([]float32, n*3)
	if srgb {
		for i := range n {
			out[i*3+0] = float32(srgbToLinearLUT[rgba[i*4+0]])
			out[i*3+1] = float32(srgbToLinearLUT[rgba[i*4+1]])
			out[i*3+2] = float32(srgbToLinearLUT[rgba[i*4+2]])
		}
	} else {
		const inv255 = 1.0 / 255.0
		for i := range n {
			out[i*3+0] = float32(rgba[i*4+0]) * inv255
			out[i*3+1] = float32(rgba[i*4+1]) * inv255
			out[i*3+2] = float32(rgba[i*4+2]) * inv255
		}
	}
	return out
}

func linearRGBToRGBABytes(lin []float32, srgb bool) []byte {
	n := len(lin) / 3
	out := make([]byte, n*4)
	if srgb {
		for i := range n {
			out[i*4+0] = linearToSRGBByte(float64(lin[i*3+0]))
			out[i*4+1] = linearToSRGBByte(float64(lin[i*3+1]))
			out[i*4+2] = linearToSRGBByte(float64(lin[i*3+2]))
			out[i*4+3] = 255
		}
	} else {
		for i := range n {
			out[i*4+0] = clampByte(int(math.Round(float64(math3d.Clamp(lin[i*3+0], 0, 1)) * 255.0)))
			out[i*4+1] = clampByte(int(math.Round(float64(math3d.Clamp(lin[i*3+1], 0, 1)) * 255.0)))
			out[i*4+2] = clampByte(int(math.Round(float64(math3d.Clamp(lin[i*3+2], 0, 1)) * 255.0)))
			out[i*4+3] = 255
		}
	}
	return out
}

func flipRGBARows(src []byte, size int) []byte {
	rowBytes := size * 4
	dst := make([]byte, len(src))
	for y := range size {
		sy := size - 1 - y
		copy(dst[y*rowBytes:(y+1)*rowBytes], src[sy*rowBytes:(sy+1)*rowBytes])
	}
	return dst
}

// -----------------------------------------------------------------------------
// Reflection Probes, Box Projection & Blending
// -----------------------------------------------------------------------------

// ReflectionProbe describes a local cubemap reflection volume with optional
// parallax-corrected box projection and smooth edge fade.
type ReflectionProbe struct {
	ID            string
	Position      math3d.Vec3
	BoxMin        math3d.Vec3
	BoxMax        math3d.Vec3
	BlendDistance float32 // inward fade width from the box boundary; 0 = sharp box boundary
	Priority      int     // higher priority takes precedence when probes overlap
	BoxProjection bool    // apply parallax-corrected ray-AABB projection
	Intensity     float32 // reflection multiplier; 0 defaults to 1.0 (use Disabled to turn off)
	Cubemap       *ProbeCubemap
	Dirty         bool // needs re-bake on request
	Baked         bool // cubemap has been baked or populated
	Disabled      bool // exclude probe from selection without removing it
}

// Validate checks that p's position, bounding box, blend distance, and intensity
// are finite and well-formed. Every error wraps [ErrInvalidProbe].
func (p *ReflectionProbe) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: nil reflection probe", ErrInvalidProbe)
	}
	if !finite3(p.Position) {
		return fmt.Errorf("%w: probe %q position is not finite", ErrInvalidProbe, p.ID)
	}
	if !finite3(p.BoxMin) || !finite3(p.BoxMax) {
		return fmt.Errorf("%w: probe %q box bounds are not finite", ErrInvalidProbe, p.ID)
	}
	if p.BoxMin.X > p.BoxMax.X || p.BoxMin.Y > p.BoxMax.Y || p.BoxMin.Z > p.BoxMax.Z {
		return fmt.Errorf("%w: probe %q box min exceeds max", ErrInvalidProbe, p.ID)
	}
	if !finite(p.BlendDistance) || p.BlendDistance < 0 {
		return fmt.Errorf("%w: probe %q blend distance %v must be finite and non-negative", ErrInvalidProbe, p.ID, p.BlendDistance)
	}
	if !finite(p.Intensity) || p.Intensity < 0 {
		return fmt.Errorf("%w: probe %q intensity %v must be finite and non-negative", ErrInvalidProbe, p.ID, p.Intensity)
	}
	return nil
}

// EffectiveIntensity returns the reflection multiplier for p, treating a zero
// Intensity as 1.0 (and returning 0 when Disabled).
func (p *ReflectionProbe) EffectiveIntensity() float32 {
	if p == nil || p.Disabled {
		return 0
	}
	if p.Intensity == 0 {
		return 1
	}
	if p.Intensity < 0 {
		return 0
	}
	return p.Intensity
}

// BoxVolume returns the volume of p's influence AABB.
func (p *ReflectionProbe) BoxVolume() float32 {
	if p == nil {
		return 0
	}
	dx := p.BoxMax.X - p.BoxMin.X
	dy := p.BoxMax.Y - p.BoxMin.Y
	dz := p.BoxMax.Z - p.BoxMin.Z
	if dx <= 0 || dy <= 0 || dz <= 0 {
		return 0
	}
	return dx * dy * dz
}

// Influence returns the smooth influence weight in [0, 1] of p at worldPos.
// Points outside [BoxMin, BoxMax] return 0. Points farther than BlendDistance
// inside every face return 1; points within BlendDistance of a face fade with
// cubic smoothstep.
func (p *ReflectionProbe) Influence(worldPos math3d.Vec3) float32 {
	if p == nil || p.Disabled {
		return 0
	}
	if p.BoxMin.X > p.BoxMax.X || p.BoxMin.Y > p.BoxMax.Y || p.BoxMin.Z > p.BoxMax.Z {
		return 0
	}
	dx := min(worldPos.X-p.BoxMin.X, p.BoxMax.X-worldPos.X)
	dy := min(worldPos.Y-p.BoxMin.Y, p.BoxMax.Y-worldPos.Y)
	dz := min(worldPos.Z-p.BoxMin.Z, p.BoxMax.Z-worldPos.Z)
	minDist := min(dx, min(dy, dz))
	if p.BlendDistance <= 0 {
		if minDist >= 0 {
			return 1
		}
		return 0
	}
	if minDist <= 0 {
		return 0
	}
	if minDist >= p.BlendDistance {
		return 1
	}
	t := minDist / p.BlendDistance
	return t * t * (3 - 2*t)
}

// BoxProjectDirection computes the parallax-corrected cubemap lookup direction
// for a reflection ray originating at worldPos along reflDir inside the box
// [boxMin, boxMax] around probePos.
func BoxProjectDirection(worldPos, reflDir, probePos, boxMin, boxMax math3d.Vec3) math3d.Vec3 {
	dir := reflDir.Norm()
	if dir.LenSq() == 0 {
		return math3d.Vec3Zero()
	}
	slabTMax := func(pos, d, bmin, bmax float32) float32 {
		if math.Abs(float64(d)) < 1e-6 {
			return 1e30
		}
		t1 := (bmin - pos) / d
		t2 := (bmax - pos) / d
		return max(t1, t2)
	}
	tx := slabTMax(worldPos.X, dir.X, boxMin.X, boxMax.X)
	ty := slabTMax(worldPos.Y, dir.Y, boxMin.Y, boxMax.Y)
	tz := slabTMax(worldPos.Z, dir.Z, boxMin.Z, boxMax.Z)
	dist := min(tx, min(ty, tz))
	if dist <= 0 || dist > 1e29 || !finite(dist) {
		return dir
	}
	hitPos := worldPos.Add(dir.Scale(dist))
	offset := hitPos.Sub(probePos)
	if offset.LenSq() < 1e-12 {
		return dir
	}
	return offset.Norm()
}

// ProbeBlendResult holds up to two active local reflection probes selected for
// a world position together with normalized blend weights (Weight0 + Weight1 +
// SkyWeight == 1).
type ProbeBlendResult struct {
	Primary   *ReflectionProbe
	Secondary *ReflectionProbe
	Weight0   float32
	Weight1   float32
	SkyWeight float32
}

// EvaluateCPU evaluates the blended specular reflection color on the CPU at
// worldPos along reflDir, using parallax box projection for probes with
// BoxProjection set and falling back to skyCubemap (or skyColor when skyCubemap
// is nil) with SkyWeight.
func (r ProbeBlendResult) EvaluateCPU(worldPos, reflDir math3d.Vec3, skyCubemap *ProbeCubemap, skyColor [3]float32) [3]float32 {
	dir := reflDir.Norm()
	var out [3]float32
	if r.Primary != nil && r.Weight0 > 0 && r.Primary.Cubemap != nil {
		d0 := dir
		if r.Primary.BoxProjection {
			d0 = BoxProjectDirection(worldPos, dir, r.Primary.Position, r.Primary.BoxMin, r.Primary.BoxMax)
		}
		c0 := r.Primary.Cubemap.SampleCPU(d0)
		s0 := r.Weight0 * r.Primary.EffectiveIntensity()
		out[0] += c0[0] * s0
		out[1] += c0[1] * s0
		out[2] += c0[2] * s0
	}
	if r.Secondary != nil && r.Weight1 > 0 && r.Secondary.Cubemap != nil {
		d1 := dir
		if r.Secondary.BoxProjection {
			d1 = BoxProjectDirection(worldPos, dir, r.Secondary.Position, r.Secondary.BoxMin, r.Secondary.BoxMax)
		}
		c1 := r.Secondary.Cubemap.SampleCPU(d1)
		s1 := r.Weight1 * r.Secondary.EffectiveIntensity()
		out[0] += c1[0] * s1
		out[1] += c1[1] * s1
		out[2] += c1[2] * s1
	}
	if r.SkyWeight > 0 {
		sky := skyColor
		if skyCubemap != nil {
			sc := skyCubemap.SampleCPU(dir)
			if skyColor == ([3]float32{}) {
				sky = sc
			} else {
				sky = [3]float32{sc[0] * skyColor[0], sc[1] * skyColor[1], sc[2] * skyColor[2]}
			}
		}
		out[0] += sky[0] * r.SkyWeight
		out[1] += sky[1] * r.SkyWeight
		out[2] += sky[2] * r.SkyWeight
	}
	return out
}

// ReflectionProbeSet manages a collection of local [ReflectionProbe] volumes
// and selects up to two overlapping probes to blend over the global sky
// environment.
type ReflectionProbeSet struct {
	Probes []*ReflectionProbe
}

// Add appends probes to the set.
func (s *ReflectionProbeSet) Add(probes ...*ReflectionProbe) {
	s.Probes = append(s.Probes, probes...)
}

// DirtyProbes returns every enabled probe currently marked Dirty or not yet Baked.
func (s *ReflectionProbeSet) DirtyProbes() []*ReflectionProbe {
	if s == nil {
		return nil
	}
	var out []*ReflectionProbe
	for _, p := range s.Probes {
		if p == nil || p.Disabled {
			continue
		}
		if p.Dirty || !p.Baked {
			out = append(out, p)
		}
	}
	return out
}

// SelectReflectionProbes selects up to two overlapping reflection probes for
// worldPos, ordered by priority, influence, and box locality, and computes
// continuous blend weights (Weight0, Weight1, SkyWeight) summing to 1.
func (s *ReflectionProbeSet) SelectReflectionProbes(worldPos math3d.Vec3) ProbeBlendResult {
	if s == nil || len(s.Probes) == 0 {
		return ProbeBlendResult{SkyWeight: 1}
	}
	type candidate struct {
		probe *ReflectionProbe
		inf   float32
		vol   float32
		idx   int
	}
	var cands []candidate
	for i, p := range s.Probes {
		if p == nil || p.Disabled {
			continue
		}
		inf := p.Influence(worldPos)
		if inf <= 0 {
			continue
		}
		cands = append(cands, candidate{
			probe: p,
			inf:   inf,
			vol:   p.BoxVolume(),
			idx:   i,
		})
	}
	if len(cands) == 0 {
		return ProbeBlendResult{SkyWeight: 1}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].probe.Priority != cands[j].probe.Priority {
			return cands[i].probe.Priority > cands[j].probe.Priority
		}
		if cands[i].inf != cands[j].inf {
			return cands[i].inf > cands[j].inf
		}
		if cands[i].vol != cands[j].vol {
			return cands[i].vol < cands[j].vol
		}
		return cands[i].idx < cands[j].idx
	})

	p0, inf0 := cands[0].probe, cands[0].inf
	if len(cands) == 1 {
		return ProbeBlendResult{
			Primary:   p0,
			Weight0:   inf0,
			SkyWeight: max(float32(0), 1-inf0),
		}
	}

	p1, inf1 := cands[1].probe, cands[1].inf
	var w0, w1, sky float32
	if p0.Priority > p1.Priority {
		w0 = inf0
		w1 = (1 - w0) * inf1
		sky = max(float32(0), 1-w0-w1)
	} else {
		sum := inf0 + inf1
		if sum <= 1 {
			w0 = inf0
			w1 = inf1
			sky = max(float32(0), 1-sum)
		} else {
			w0 = inf0 / sum
			w1 = inf1 / sum
			sky = 0
		}
	}
	return ProbeBlendResult{
		Primary:   p0,
		Secondary: p1,
		Weight0:   w0,
		Weight1:   w1,
		SkyWeight: sky,
	}
}

// BakeReflectionProbeCPU samples a 6-face linear RGB cubemap of resolution
// size×size from probe.Position using sample(origin, dir).
func BakeReflectionProbeCPU(probe *ReflectionProbe, size int, sample func(origin, dir math3d.Vec3) [3]float32) ([ProbeCubeFaceCount][]float32, error) {
	var faces [ProbeCubeFaceCount][]float32
	if err := probe.Validate(); err != nil {
		return faces, err
	}
	if size <= 0 {
		return faces, fmt.Errorf("%w: bake size %d must be positive", ErrInvalidProbe, size)
	}
	if sample == nil {
		return faces, fmt.Errorf("%w: nil bake sampler function", ErrInvalidProbe)
	}
	invSize := 1.0 / float32(size)
	for f := range ProbeCubeFaceCount {
		buf := make([]float32, size*size*3)
		for y := range size {
			v := (float32(y) + 0.5) * invSize
			for x := range size {
				u := (float32(x) + 0.5) * invSize
				dir := CubeFaceDirection(gpu.CubeFace(f), u, v)
				c := sample(probe.Position, dir)
				i := (y*size + x) * 3
				buf[i+0] = c[0]
				buf[i+1] = c[1]
				buf[i+2] = c[2]
			}
		}
		faces[f] = buf
	}
	return faces, nil
}

// BakeReflectionProbe bakes probe on request using sample(origin, dir), uploads
// the resulting [ProbeCubemap] to dev, attaches it to probe.Cubemap, and marks
// probe Baked = true and Dirty = false.
func BakeReflectionProbe(cx *core.Scope, dev *gpu.Device, probe *ReflectionProbe, size int, opts ProbeCubemapUpload, sample func(origin, dir math3d.Vec3) [3]float32) error {
	faces, err := BakeReflectionProbeCPU(probe, size, sample)
	if err != nil {
		return err
	}
	if opts.Label == "" && probe.ID != "" {
		opts.Label = "probe:" + probe.ID
	}
	cm, err := UploadProbeCubemapLinear(cx, dev, size, faces, opts)
	if err != nil {
		return err
	}
	probe.Cubemap = cm
	probe.Baked = true
	probe.Dirty = false
	return nil
}

// BakeReflectionProbeGPU bakes probe on request by rendering all 6 cubemap faces
// on dev into a temporary size×size offscreen render target via renderFace,
// reading back each face, flipping rows to canonical cubemap orientation, and
// uploading the resulting [ProbeCubemap] (including mipmaps when requested).
func BakeReflectionProbeGPU(
	cx *core.Scope,
	dev *gpu.Device,
	probe *ReflectionProbe,
	size int,
	near, far float32,
	clearColor core.Color,
	opts ProbeCubemapUpload,
	renderFace func(pass *gpu.RenderPass, face gpu.CubeFace, cam CameraUniform),
) error {
	if err := probe.Validate(); err != nil {
		return err
	}
	if size <= 0 {
		return fmt.Errorf("%w: bake size %d must be positive", ErrInvalidProbe, size)
	}
	if renderFace == nil {
		return fmt.Errorf("%w: nil renderFace callback", ErrInvalidProbe)
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	rt, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "render3d.probe_bake_rt",
		Width:       uint32(size),
		Height:      uint32(size),
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()

	var facesRGBA [ProbeCubeFaceCount][]byte
	for f := range ProbeCubeFaceCount {
		face := gpu.CubeFace(f)
		cam := ProbeFaceCamera(probe.Position, face, near, far)

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			Label: "render3d.probe_bake_face",
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    rt.Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: clearColor,
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      rt.Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		renderFace(pass, face, cam)
		pass.End()
		dev.Submit(enc.Finish())

		var readBytes []byte
		var readErr error
		dev.ReadTexture(rt.Color, gpu.TextureRegion{
			Width:  uint32(size),
			Height: uint32(size),
		}, func(rgba []byte, err error) {
			if err != nil {
				readErr = err
				return
			}
			readBytes = make([]byte, len(rgba))
			copy(readBytes, rgba)
		})
		dev.Submit(dev.BeginEncoder().Finish())
		dev.WaitIdle()
		if readErr != nil {
			return readErr
		}
		if len(readBytes) < size*size*4 {
			return fmt.Errorf("%w: face %d readback returned %d bytes, want %d", ErrInvalidProbe, f, len(readBytes), size*size*4)
		}
		facesRGBA[f] = flipRGBARows(readBytes[:size*size*4], size)
	}

	if opts.Label == "" && probe.ID != "" {
		opts.Label = "probe:" + probe.ID
	}
	cm, err := UploadProbeCubemapRGBA(cx, dev, size, facesRGBA, opts)
	if err != nil {
		return err
	}
	probe.Cubemap = cm
	probe.Baked = true
	probe.Dirty = false
	return nil
}

// -----------------------------------------------------------------------------
// L2 Spherical Harmonics (SH9) & Light Probes
// -----------------------------------------------------------------------------

// SH9 holds 9-band (L0, L1, L2) real spherical-harmonic radiance coefficients
// in linear RGB.
type SH9 [9][3]float32

// SH9Basis evaluates the 9 orthonormal real spherical harmonic basis functions
// Y_0..Y_8 at unit direction dir.
func SH9Basis(dir math3d.Vec3) [9]float32 {
	d := dir.Norm()
	x, y, z := d.X, d.Y, d.Z
	return [9]float32{
		shY0,
		shY1 * y,
		shY1 * z,
		shY1 * x,
		shY2xy * x * y,
		shY2xy * y * z,
		shY2z2 * (3*z*z - 1),
		shY2xy * x * z,
		shY2x2y2 * (x*x - y*y),
	}
}

// ProjectDirectionSH9 projects a directional radiance sample along dir onto the
// 9 real SH basis functions. To integrate a continuous environment over the
// sphere, scale each sample by its solid angle dOmega and sum with [SH9.Add].
func ProjectDirectionSH9(dir math3d.Vec3, radiance [3]float32) SH9 {
	basis := SH9Basis(dir)
	var sh SH9
	for i := range 9 {
		b := basis[i]
		sh[i][0] = radiance[0] * b
		sh[i][1] = radiance[1] * b
		sh[i][2] = radiance[2] * b
	}
	return sh
}

// Add returns the component-wise sum s + other.
func (s SH9) Add(other SH9) SH9 {
	var out SH9
	for i := range 9 {
		out[i][0] = s[i][0] + other[i][0]
		out[i][1] = s[i][1] + other[i][1]
		out[i][2] = s[i][2] + other[i][2]
	}
	return out
}

// Scale returns s multiplied by scalar k.
func (s SH9) Scale(k float32) SH9 {
	var out SH9
	for i := range 9 {
		out[i][0] = s[i][0] * k
		out[i][1] = s[i][1] * k
		out[i][2] = s[i][2] * k
	}
	return out
}

// Lerp linearly interpolates between s (t=0) and other (t=1).
func (s SH9) Lerp(other SH9, t float32) SH9 {
	u := 1 - t
	var out SH9
	for i := range 9 {
		out[i][0] = s[i][0]*u + other[i][0]*t
		out[i][1] = s[i][1]*u + other[i][1]*t
		out[i][2] = s[i][2]*u + other[i][2]*t
	}
	return out
}

// EvaluateSH9 evaluates the normalized Lambertian cosine-convolved diffuse
// irradiance E(normal)/pi from s in linear RGB, clamped to non-negative values.
// Multiply directly by a surface's linear diffuse albedo.
func EvaluateSH9(s SH9, normal math3d.Vec3) [3]float32 {
	basis := SH9Basis(normal)
	weights := [9]float32{
		shCos0,
		shCos1, shCos1, shCos1,
		shCos2, shCos2, shCos2, shCos2, shCos2,
	}
	var out [3]float32
	for i := range 9 {
		w := basis[i] * weights[i]
		out[0] += s[i][0] * w
		out[1] += s[i][1] * w
		out[2] += s[i][2] * w
	}
	if out[0] < 0 {
		out[0] = 0
	}
	if out[1] < 0 {
		out[1] = 0
	}
	if out[2] < 0 {
		out[2] = 0
	}
	return out
}

// EvaluateDiffuse returns [EvaluateSH9](s, normal).
func (s SH9) EvaluateDiffuse(normal math3d.Vec3) [3]float32 {
	return EvaluateSH9(s, normal)
}

// EvaluateIrradiance returns the physical cosine-weighted irradiance E(normal)
// (= pi * EvaluateDiffuse(normal)).
func (s SH9) EvaluateIrradiance(normal math3d.Vec3) [3]float32 {
	d := EvaluateSH9(s, normal)
	return [3]float32{d[0] * math.Pi, d[1] * math.Pi, d[2] * math.Pi}
}

// cubeTexelAreaElement returns the exact solid angle of the cube-face rectangle
// [0, x] x [0, y] on the unit cube face z = 1 (Ignacio Castaño / AMD CubeMapGen).
func cubeTexelAreaElement(x, y float64) float64 {
	return math.Atan2(x*y, math.Sqrt(x*x+y*y+1))
}

// cubeTexelSolidAngle returns the exact solid angle dOmega of texel (px, py) on
// a cubemap face of resolution size×size. Summing across all 6*size*size texels
// equals exactly 4*pi.
func cubeTexelSolidAngle(px, py, size int) float64 {
	inv := 2.0 / float64(size)
	x0 := float64(px)*inv - 1.0
	x1 := float64(px+1)*inv - 1.0
	y0 := float64(py)*inv - 1.0
	y1 := float64(py+1)*inv - 1.0
	return cubeTexelAreaElement(x1, y1) -
		cubeTexelAreaElement(x0, y1) -
		cubeTexelAreaElement(x1, y0) +
		cubeTexelAreaElement(x0, y0)
}

// BakeSH9FromCubemap integrates a 6-face linear RGB cubemap (each slice holding
// size*size*3 float32 values in canonical face order +X,-X,+Y,-Y,+Z,-Z) using
// exact per-texel solid-angle weighting to produce baked L2 [SH9] coefficients.
func BakeSH9FromCubemap(faces [ProbeCubeFaceCount][]float32, size int) SH9 {
	if size <= 0 {
		return SH9{}
	}
	need := size * size * 3
	var accum [9][3]float64
	invSize := 1.0 / float32(size)
	for f := range ProbeCubeFaceCount {
		pix := faces[f]
		if len(pix) < need {
			continue
		}
		for y := range size {
			v := (float32(y) + 0.5) * invSize
			for x := range size {
				u := (float32(x) + 0.5) * invSize
				dir := CubeFaceDirection(gpu.CubeFace(f), u, v)
				dOmega := cubeTexelSolidAngle(x, y, size)
				basis := SH9Basis(dir)
				off := (y*size + x) * 3
				r := float64(pix[off+0]) * dOmega
				g := float64(pix[off+1]) * dOmega
				b := float64(pix[off+2]) * dOmega
				for k := range 9 {
					bk := float64(basis[k])
					accum[k][0] += r * bk
					accum[k][1] += g * bk
					accum[k][2] += b * bk
				}
			}
		}
	}
	var sh SH9
	for k := range 9 {
		sh[k][0] = float32(accum[k][0])
		sh[k][1] = float32(accum[k][1])
		sh[k][2] = float32(accum[k][2])
	}
	return sh
}

// BakeSH9FromFunc integrates an environment function sample(dir) over a
// size×size cubemap grid using exact solid-angle weights to produce L2 [SH9]
// coefficients without allocating intermediate face buffers.
func BakeSH9FromFunc(size int, sample func(dir math3d.Vec3) [3]float32) SH9 {
	if size <= 0 || sample == nil {
		return SH9{}
	}
	var accum [9][3]float64
	invSize := 1.0 / float32(size)
	for f := range ProbeCubeFaceCount {
		for y := range size {
			v := (float32(y) + 0.5) * invSize
			for x := range size {
				u := (float32(x) + 0.5) * invSize
				dir := CubeFaceDirection(gpu.CubeFace(f), u, v)
				dOmega := cubeTexelSolidAngle(x, y, size)
				basis := SH9Basis(dir)
				c := sample(dir)
				r := float64(c[0]) * dOmega
				g := float64(c[1]) * dOmega
				b := float64(c[2]) * dOmega
				for k := range 9 {
					bk := float64(basis[k])
					accum[k][0] += r * bk
					accum[k][1] += g * bk
					accum[k][2] += b * bk
				}
			}
		}
	}
	var sh SH9
	for k := range 9 {
		sh[k][0] = float32(accum[k][0])
		sh[k][1] = float32(accum[k][1])
		sh[k][2] = float32(accum[k][2])
	}
	return sh
}

// LightProbe stores baked L2 [SH9] indirect lighting at a single world position.
// Radius optionally bounds the probe's influence in a scattered [LightProbeVolume]
// (<= 0 means global inverse-distance weighting).
type LightProbe struct {
	Position math3d.Vec3
	SH       SH9
	Radius   float32
}

// LightProbeGrid stores a regular 3D lattice of [SH9] light probes covering
// Bounds and provides O(1) trilinear interpolation for dynamic objects.
type LightProbeGrid struct {
	Bounds math3d.AABB
	Counts [3]int
	Probes []SH9
}

// NewLightProbeGrid allocates a regular nx×ny×nz [LightProbeGrid] covering bounds.
func NewLightProbeGrid(bounds math3d.AABB, nx, ny, nz int) (*LightProbeGrid, error) {
	if bounds.Empty() || !finite3(bounds.Min) || !finite3(bounds.Max) {
		return nil, fmt.Errorf("%w: light probe grid bounds are invalid", ErrInvalidProbe)
	}
	if nx <= 0 || ny <= 0 || nz <= 0 {
		return nil, fmt.Errorf("%w: light probe grid counts %dx%dx%d must be positive", ErrInvalidProbe, nx, ny, nz)
	}
	return &LightProbeGrid{
		Bounds: bounds,
		Counts: [3]int{nx, ny, nz},
		Probes: make([]SH9, nx*ny*nz),
	}, nil
}

func (g *LightProbeGrid) index(ix, iy, iz int) int {
	return (iz*g.Counts[1]+iy)*g.Counts[0] + ix
}

// ProbePosition returns the world position of grid probe (ix, iy, iz).
func (g *LightProbeGrid) ProbePosition(ix, iy, iz int) math3d.Vec3 {
	axisPos := func(i, n int, lo, hi float32) float32 {
		if n <= 1 {
			return 0.5 * (lo + hi)
		}
		t := float32(i) / float32(n-1)
		return lo + (hi-lo)*t
	}
	return math3d.Vec3{
		X: axisPos(ix, g.Counts[0], g.Bounds.Min.X, g.Bounds.Max.X),
		Y: axisPos(iy, g.Counts[1], g.Bounds.Min.Y, g.Bounds.Max.Y),
		Z: axisPos(iz, g.Counts[2], g.Bounds.Min.Z, g.Bounds.Max.Z),
	}
}

// Set writes sh to grid cell (ix, iy, iz). Out-of-range coordinates are ignored.
func (g *LightProbeGrid) Set(ix, iy, iz int, sh SH9) {
	if g == nil || ix < 0 || iy < 0 || iz < 0 || ix >= g.Counts[0] || iy >= g.Counts[1] || iz >= g.Counts[2] {
		return
	}
	g.Probes[g.index(ix, iy, iz)] = sh
}

// At returns the [SH9] coefficients stored at grid cell (ix, iy, iz), clamping
// coordinates into range.
func (g *LightProbeGrid) At(ix, iy, iz int) SH9 {
	if g == nil || len(g.Probes) == 0 {
		return SH9{}
	}
	ix = probeClampInt(ix, 0, g.Counts[0]-1)
	iy = probeClampInt(iy, 0, g.Counts[1]-1)
	iz = probeClampInt(iz, 0, g.Counts[2]-1)
	return g.Probes[g.index(ix, iy, iz)]
}

// Bake populates every probe in g by integrating sample(probePos, dir) over a
// cubemap of resolution size×size per probe.
func (g *LightProbeGrid) Bake(size int, sample func(pos, dir math3d.Vec3) [3]float32) {
	if g == nil || size <= 0 || sample == nil {
		return
	}
	for iz := range g.Counts[2] {
		for iy := range g.Counts[1] {
			for ix := range g.Counts[0] {
				pos := g.ProbePosition(ix, iy, iz)
				sh := BakeSH9FromFunc(size, func(dir math3d.Vec3) [3]float32 {
					return sample(pos, dir)
				})
				g.Set(ix, iy, iz, sh)
			}
		}
	}
}

// Sample trilinearly interpolates [SH9] coefficients at worldPos, clamping
// positions outside Bounds to the boundary cell.
func (g *LightProbeGrid) Sample(worldPos math3d.Vec3) SH9 {
	if g == nil || len(g.Probes) == 0 {
		return SH9{}
	}
	axisCoord := func(p, lo, hi float32, n int) (i0, i1 int, t float32) {
		if n <= 1 || hi <= lo {
			return 0, 0, 0
		}
		u := math3d.Clamp((p-lo)/(hi-lo), 0, 1) * float32(n-1)
		i0 = probeClampInt(int(math.Floor(float64(u))), 0, n-1)
		i1 = min(i0+1, n-1)
		t = u - float32(i0)
		return i0, i1, t
	}
	x0, x1, tx := axisCoord(worldPos.X, g.Bounds.Min.X, g.Bounds.Max.X, g.Counts[0])
	y0, y1, ty := axisCoord(worldPos.Y, g.Bounds.Min.Y, g.Bounds.Max.Y, g.Counts[1])
	z0, z1, tz := axisCoord(worldPos.Z, g.Bounds.Min.Z, g.Bounds.Max.Z, g.Counts[2])

	c000 := g.At(x0, y0, z0)
	c100 := g.At(x1, y0, z0)
	c010 := g.At(x0, y1, z0)
	c110 := g.At(x1, y1, z0)
	c001 := g.At(x0, y0, z1)
	c101 := g.At(x1, y0, z1)
	c011 := g.At(x0, y1, z1)
	c111 := g.At(x1, y1, z1)

	c00 := c000.Lerp(c100, tx)
	c10 := c010.Lerp(c110, tx)
	c01 := c001.Lerp(c101, tx)
	c11 := c011.Lerp(c111, tx)

	c0 := c00.Lerp(c10, ty)
	c1 := c01.Lerp(c11, ty)
	return c0.Lerp(c1, tz)
}

// LightProbeVolume combines an optional regular [LightProbeGrid] and/or
// scattered [LightProbe] points (plus an optional FallbackSH) so dynamic
// objects pick up smoothly interpolated baked indirect lighting anywhere in a
// scene.
type LightProbeVolume struct {
	Grid       *LightProbeGrid
	Probes     []LightProbe
	FallbackSH SH9
}

// Add appends scattered probes to the volume.
func (v *LightProbeVolume) Add(probes ...LightProbe) {
	v.Probes = append(v.Probes, probes...)
}

// SampleLightProbes returns the interpolated [SH9] lighting at worldPos.
// Regular grid probes use trilinear interpolation; scattered probes use smooth
// inverse-distance weighting (respecting per-probe Radius when positive) and
// blend seamlessly into Grid or FallbackSH outside finite probe radii.
func (v *LightProbeVolume) SampleLightProbes(worldPos math3d.Vec3) SH9 {
	if v == nil {
		return SH9{}
	}
	baseSH := v.FallbackSH
	if v.Grid != nil {
		baseSH = v.Grid.Sample(worldPos)
	}
	if len(v.Probes) == 0 {
		return baseSH
	}

	var accum SH9
	var weightSum float32
	var maxCoverage float32
	hasUnbounded := false

	for i := range v.Probes {
		p := &v.Probes[i]
		distSq := worldPos.DistSq(p.Position)
		if distSq < 1e-10 {
			return p.SH
		}
		if p.Radius > 0 {
			dist := float32(math.Sqrt(float64(distSq)))
			if dist >= p.Radius {
				continue
			}
			u := 1 - dist/p.Radius
			falloff := u * u * (3 - 2*u)
			if falloff > maxCoverage {
				maxCoverage = falloff
			}
			w := falloff / (distSq + 1e-4)
			accum = accum.Add(p.SH.Scale(w))
			weightSum += w
		} else {
			hasUnbounded = true
			w := 1.0 / (distSq + 1e-4)
			accum = accum.Add(p.SH.Scale(w))
			weightSum += w
		}
	}

	if weightSum <= 0 {
		return baseSH
	}
	scattered := accum.Scale(1.0 / weightSum)
	if hasUnbounded || maxCoverage >= 1 {
		return scattered
	}
	return baseSH.Lerp(scattered, maxCoverage)
}

// -----------------------------------------------------------------------------
// GPU Probe Uniform & ProbeRenderer
// -----------------------------------------------------------------------------

// ProbeMaterial configures surface reflectance for [ProbeUniform] and
// [ProbeRenderer].
type ProbeMaterial struct {
	Albedo           [3]float32 // linear RGB diffuse/metallic base color (default white when zero? No: explicit RGB, Alpha 0 -> 1)
	Alpha            float32    // 0 defaults to 1.0
	Roughness        float32    // [0..1]
	Metallic         float32    // [0..1]
	F0               float32    // dielectric base reflectance (e.g. 0.04; 0 = none)
	DiffuseIntensity float32    // SH9 diffuse multiplier; 0 defaults to 1.0
	SkyTint          [3]float32 // linear RGB sky multiplier; all zeros defaults to (1, 1, 1)
}

// ProbeUniform is the per-object uniform block read by [ProbeWGSL] at
// @group(1) @binding(0).
type ProbeUniform struct {
	Model          math3d.Mat4
	Material       ProbeMaterial
	Blend          ProbeBlendResult
	LightSH        SH9
	MaxReflectMips float32
}

// Encode writes u into dst in the little-endian layout [ProbeWGSL] declares and
// returns dst[:ProbeUniformSize]. It panics if len(dst) < ProbeUniformSize.
func (u *ProbeUniform) Encode(dst []byte) []byte {
	if len(dst) < ProbeUniformSize {
		panic("render3d: ProbeUniform.Encode: dst shorter than ProbeUniformSize")
	}
	dst = dst[:ProbeUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	put4 := func(x, y, z, w float32) {
		put(x)
		put(y)
		put(z)
		put(w)
	}

	model := u.Model
	if model == (math3d.Mat4{}) {
		model = math3d.Mat4Identity()
	}
	for _, f := range model {
		put(f)
	}

	alpha := u.Material.Alpha
	if alpha == 0 {
		alpha = 1
	}
	put4(u.Material.Albedo[0], u.Material.Albedo[1], u.Material.Albedo[2], alpha)
	put4(
		math3d.Clamp(u.Material.Roughness, 0, 1),
		math3d.Clamp(u.Material.Metallic, 0, 1),
		math3d.Clamp(u.Material.F0, 0, 1),
		max(float32(0), u.MaxReflectMips),
	)

	encodeProbe := func(p *ReflectionProbe, w float32) {
		if p == nil || w <= 0 {
			put4(0, 0, 0, 0)
			put4(0, 0, 0, 0)
			put4(0, 0, 0, 0)
			return
		}
		var boxProj float32
		if p.BoxProjection {
			boxProj = 1
		}
		put4(p.Position.X, p.Position.Y, p.Position.Z, boxProj)
		put4(p.BoxMin.X, p.BoxMin.Y, p.BoxMin.Z, p.EffectiveIntensity())
		put4(p.BoxMax.X, p.BoxMax.Y, p.BoxMax.Z, w)
	}
	encodeProbe(u.Blend.Primary, u.Blend.Weight0)
	encodeProbe(u.Blend.Secondary, u.Blend.Weight1)

	skyTint := u.Material.SkyTint
	if skyTint == ([3]float32{}) {
		skyTint = [3]float32{1, 1, 1}
	}
	skyW := u.Blend.SkyWeight
	if u.Blend.Primary == nil && u.Blend.Secondary == nil && skyW == 0 {
		skyW = 1
	}
	put4(skyTint[0], skyTint[1], skyTint[2], skyW)

	diffInt := u.Material.DiffuseIntensity
	if diffInt == 0 {
		diffInt = 1
	}
	put4(u.LightSH[0][0], u.LightSH[0][1], u.LightSH[0][2], diffInt)
	for i := 1; i < 9; i++ {
		put4(u.LightSH[i][0], u.LightSH[i][1], u.LightSH[i][2], 0)
	}
	return dst
}

// ProbeRenderer compiles [ProbeWGSL] and manages the pipeline, sampler, and
// fallback cubemap required to render meshes lit by [ReflectionProbeSet] and
// [LightProbeVolume] on an NSGo [gpu.Device].
type ProbeRenderer struct {
	dev          *gpu.Device
	shader       *gpu.ShaderModule
	probeLayout  *gpu.BindGroupLayout
	pipeline     *gpu.Pipeline
	sampler      *GPUSampler
	fallbackCube *ProbeCubemap
	depthFormat  gpu.TextureFormat
}

// ProbeRendererOptions configures [NewProbeRenderer].
type ProbeRendererOptions struct {
	// CameraLayout is the bind group layout at [CameraGroup] (from [CameraBuffer.Layout]).
	CameraLayout *gpu.BindGroupLayout
	// ColorFormat defaults to dev.SurfaceFormat() when FormatUndefined.
	ColorFormat gpu.TextureFormat
	// DepthFormat defaults to gpu.FormatDepth24Plus when FormatUndefined.
	// Pass gpu.FormatUndefined with DisableDepth: true to render without depth.
	DepthFormat  gpu.TextureFormat
	DisableDepth bool
	CullMode     gpu.CullMode
	Retained     bool
}

// NewProbeRenderer creates a [ProbeRenderer] on dev scoped to cx.
func NewProbeRenderer(cx *core.Scope, dev *gpu.Device, opts ProbeRendererOptions) (*ProbeRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.CameraLayout == nil || !opts.CameraLayout.Valid() {
		return nil, fmt.Errorf("%w: valid CameraLayout is required", ErrInvalidProbe)
	}
	shader, err := gpu.CompileWGSL(ProbeWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile probe WGSL: %w", err)
	}
	probeLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.probe",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		return nil, err
	}
	sampOpts := SamplerLinearClamp()
	sampOpts.Label = "render3d.probe.sampler"
	sampOpts.Retained = opts.Retained
	samp, err := NewGPUSampler(cx, dev, sampOpts)
	if err != nil {
		_ = probeLayout.Close()
		return nil, err
	}
	white := [3]float32{1, 1, 1}
	fallbackCube, err := NewSolidProbeCubemap(cx, dev, [ProbeCubeFaceCount][3]float32{
		white, white, white, white, white, white,
	}, 2, ProbeCubemapUpload{
		Label:    "render3d.probe.fallback_cube",
		Retained: opts.Retained,
	})
	if err != nil {
		_ = samp.Close()
		_ = probeLayout.Close()
		return nil, err
	}

	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = dev.SurfaceFormat()
	}
	var depthState *gpu.DepthStencilState
	depthFmt := opts.DepthFormat
	if !opts.DisableDepth {
		if depthFmt == gpu.FormatUndefined {
			depthFmt = gpu.FormatDepth24Plus
		}
		depthState = &gpu.DepthStencilState{
			Format:       depthFmt,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}

	layouts := make([]*gpu.BindGroupLayout, ProbeGroup+1)
	layouts[CameraGroup] = opts.CameraLayout
	layouts[ProbeGroup] = probeLayout
	pipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:         "render3d.probe",
		Vertex:        gpu.ShaderStage{Module: shader, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: shader, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  opts.CullMode,
		},
		DepthStencil: depthState,
		Targets:      []gpu.ColorTargetState{{Format: colorFormat}},
	})
	if err != nil {
		_ = fallbackCube.Close()
		_ = samp.Close()
		_ = probeLayout.Close()
		return nil, err
	}

	return &ProbeRenderer{
		dev:          dev,
		shader:       shader,
		probeLayout:  probeLayout,
		pipeline:     pipe,
		sampler:      samp,
		fallbackCube: fallbackCube,
		depthFormat:  depthFmt,
	}, nil
}

// Pipeline returns the compiled probe rendering pipeline.
func (r *ProbeRenderer) Pipeline() *gpu.Pipeline { return r.pipeline }

// Layout returns the [ProbeGroup] bind group layout.
func (r *ProbeRenderer) Layout() *gpu.BindGroupLayout { return r.probeLayout }

// FallbackCubemap returns the default white cubemap bound when a probe slot or
// sky cubemap is absent.
func (r *ProbeRenderer) FallbackCubemap() *ProbeCubemap { return r.fallbackCube }

// Close releases resources owned by r.
func (r *ProbeRenderer) Close() error {
	if r == nil {
		return nil
	}
	var first error
	for _, c := range []interface{ Close() error }{r.pipeline, r.fallbackCube, r.sampler, r.probeLayout} {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ProbeBinding holds a per-object uniform buffer and bind group for drawing one
// object with [ProbeRenderer]. Creating one ProbeBinding per rendered object
// lets multiple objects with distinct probe blends and [SH9] coefficients draw
// in the same render pass.
type ProbeBinding struct {
	renderer *ProbeRenderer
	cx       *core.Scope
	buf      *gpu.Buffer
	group    *gpu.BindGroup
	cube0    *gpu.Texture
	cube1    *gpu.Texture
	sky      *gpu.Texture
	scratch  [ProbeUniformSize]byte
}

// NewBinding creates a [ProbeBinding] scoped to cx (nil for caller-managed Close).
func (r *ProbeRenderer) NewBinding(cx *core.Scope) (*ProbeBinding, error) {
	if r == nil || r.dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil probe renderer"}
	}
	buf, err := r.dev.CreateBuffer(cx, gpu.BufferDesc{
		Label: "render3d.probe.uniform",
		Size:  ProbeUniformSize,
		Usage: gpu.UsageUniform | gpu.UsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	return &ProbeBinding{
		renderer: r,
		cx:       cx,
		buf:      buf,
	}, nil
}

// Update writes u into the uniform buffer and ensures the bind group binds the
// cubemaps referenced by u.Blend and sky (falling back to the renderer's white
// fallback cubemap whenever a cubemap is nil).
func (b *ProbeBinding) Update(u ProbeUniform, sky *ProbeCubemap) error {
	if b == nil || b.renderer == nil || !b.buf.Valid() {
		return fmt.Errorf("%w: probe binding is not valid", ErrInvalidProbe)
	}
	pickTex := func(cm *ProbeCubemap) (*gpu.Texture, uint32) {
		if cm != nil && cm.Valid() {
			return cm.Texture(), cm.MipLevels()
		}
		return b.renderer.fallbackCube.Texture(), b.renderer.fallbackCube.MipLevels()
	}
	var c0, c1 *ProbeCubemap
	if u.Blend.Primary != nil {
		c0 = u.Blend.Primary.Cubemap
	}
	if u.Blend.Secondary != nil {
		c1 = u.Blend.Secondary.Cubemap
	}
	tex0, mips0 := pickTex(c0)
	tex1, mips1 := pickTex(c1)
	texSky, mipsSky := pickTex(sky)

	if u.MaxReflectMips == 0 {
		maxMips := max(mips0, max(mips1, mipsSky))
		if maxMips > 1 {
			u.MaxReflectMips = float32(maxMips - 1)
		}
	}

	b.renderer.dev.WriteBuffer(b.buf, 0, u.Encode(b.scratch[:]))

	if b.group == nil || !b.group.Valid() || b.cube0 != tex0 || b.cube1 != tex1 || b.sky != texSky {
		if b.group != nil {
			_ = b.group.Close()
			b.group = nil
		}
		bg, err := b.renderer.dev.CreateBindGroupDesc(b.cx, gpu.BindGroupDesc{
			Label:  "render3d.probe",
			Layout: b.renderer.probeLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: b.buf, Size: ProbeUniformSize},
				{Binding: 1, Texture: tex0},
				{Binding: 2, Texture: tex1},
				{Binding: 3, Texture: texSky},
				{Binding: 4, Sampler: b.renderer.sampler.Sampler()},
			},
		})
		if err != nil {
			return err
		}
		b.group = bg
		b.cube0 = tex0
		b.cube1 = tex1
		b.sky = texSky
	}
	return nil
}

// BindGroup returns the underlying [gpu.BindGroup] for [ProbeGroup].
func (b *ProbeBinding) BindGroup() *gpu.BindGroup {
	if b == nil {
		return nil
	}
	return b.group
}

// Close releases the bind group and uniform buffer.
func (b *ProbeBinding) Close() error {
	if b == nil {
		return nil
	}
	var first error
	if b.group != nil {
		if err := b.group.Close(); err != nil && first == nil {
			first = err
		}
		b.group = nil
	}
	if b.buf != nil {
		if err := b.buf.Close(); err != nil && first == nil {
			first = err
		}
		b.buf = nil
	}
	return first
}

// Draw sets the probe pipeline, camera bind group, and object probe bind group
// on pass, and draws mesh.
func (r *ProbeRenderer) Draw(pass *gpu.RenderPass, cam *CameraBuffer, binding *ProbeBinding, mesh *GPUMesh) {
	if r == nil || pass == nil || cam == nil || binding == nil || binding.group == nil || !mesh.Valid() {
		return
	}
	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(CameraGroup, cam.BindGroup())
	pass.SetBindGroup(ProbeGroup, binding.group)
	mesh.Draw(pass)
}

func probeClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
