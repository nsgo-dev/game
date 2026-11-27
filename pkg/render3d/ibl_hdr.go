package render3d

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// MaterialIBLGroup is the bind group index [MaterialIBLWGSL] and [MaterialIBLLinearWGSL] reserve for image-based lighting ([GPUIBLBuffer.BindGroup]), after [CameraGroup] (group 0) and [MaterialGroup] (group 1).
const MaterialIBLGroup = 2

// SkyboxToneMapWGSL is [SkyboxWGSL] with Reinhard tone mapping and gamma 2.2 encoding applied after exposure, matching [MaterialWGSL] and [MaterialIBLWGSL] so an HDR [GPUCubemap] draws consistently directly to an LDR render target. [NewSkyboxPipeline] compiles it when [SkyboxPipelineDesc].ToneMap is true.
const SkyboxToneMapWGSL = `struct SkyboxParams {
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
	out.pos = vec4<f32>(ndc, 1.0, 1.0);
	out.dir = world.xyz;
	return out;
}

@fragment
fn fs_skybox(in: SkyboxVertexOut) -> @location(0) vec4<f32> {
	let dir = normalize(in.dir);
	let sampled = textureSampleLevel(skybox_tex, skybox_sampler, dir, skybox.lod);
	let c = max(sampled.rgb * skybox.exposure, vec3<f32>(0.0, 0.0, 0.0));
	let mapped = c / (c + vec3<f32>(1.0, 1.0, 1.0));
	let srgb = pow(clamp(mapped, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0)), vec3<f32>(1.0 / 2.2, 1.0 / 2.2, 1.0 / 2.2));
	return vec4<f32>(srgb, 1.0);
}
`

// materialIBLDeclWGSL binds the IBL uniform, prefiltered cubemap, and sampler at [MaterialIBLGroup] (group 2).
const materialIBLDeclWGSL = iblWGSLStruct + `@group(2) @binding(0) var<uniform> ibl: IBLUniform;
@group(2) @binding(1) var ibl_prefilter: texture_cube<f32>;
@group(2) @binding(2) var ibl_sampler: sampler;

` + iblWGSLFuncs

// materialIBLFragmentHeadWGSL samples all five glTF 2.0 metallic-roughness maps (base color, metallic-roughness, tangent-space normal, occlusion, emissive) and shades them with [IBLWGSL]'s evaluate_ibl plus self-emission.
const materialIBLFragmentHeadWGSL = `
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
	let ibl_rgb = evaluate_ibl(n, v, base_color.rgb, metallic, roughness, occlusion);
	let linear_rgb = ibl_rgb + emissive;
`

// MaterialIBLWGSL is the textured glTF 2.0 metallic-roughness PBR shader driven by image-based lighting: [CameraWGSL] at [CameraGroup] (group 0), [MaterialBindingsWGSL] at [MaterialGroup] (group 1), and a [GPUIBLBuffer] at [MaterialIBLGroup] (group 2). It evaluates all five glTF maps (base color, metallic-roughness, tangent-space normal, occlusion, emissive), alpha masking/blending, double-sided normal flipping, and split-sum IBL (L2 SH diffuse irradiance + GGX prefiltered specular), followed by Reinhard tone mapping and gamma 2.2 encoding. Use [NewMaterialIBLPipeline] to create a pipeline from it.
const MaterialIBLWGSL = CameraWGSL + MaterialBindingsWGSL + "\n" + materialIBLDeclWGSL + materialVertexStageWGSL + materialIBLFragmentHeadWGSL + `	let srgb_rgb = material_tonemap_srgb(linear_rgb);
` + materialStagesTailWGSL

// MaterialIBLLinearWGSL is [MaterialIBLWGSL] writing unclamped linear HDR radiance without tone mapping or gamma encoding, compiled by [NewMaterialIBLPipeline] when [MaterialIBLPipelineOptions].LinearOutput is set for rendering into an [HDRTarget] resolved by [ToneMapPass].
const MaterialIBLLinearWGSL = CameraWGSL + MaterialBindingsWGSL + "\n" + materialIBLDeclWGSL + materialVertexStageWGSL + materialIBLFragmentHeadWGSL + `	let srgb_rgb = linear_rgb;
` + materialStagesTailWGSL

// MaterialIBLPipelineOptions configures [NewMaterialIBLPipeline].
type MaterialIBLPipelineOptions struct {
	// Label names the pipeline in GPU capture tools.
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat(). Use [HDRTarget.Format] with LinearOutput: true when rendering into an [HDRTarget].
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing (and depth writes for opaque/alpha-mask modes) when non-zero.
	DepthFormat gpu.TextureFormat
	// AlphaMode selects blending and depth-write state: AlphaOpaque and AlphaMask write depth without color blending; AlphaBlend uses straight alpha blending ([gpu.BlendAlpha]) with depth writes disabled.
	AlphaMode AlphaMode
	// DoubleSided disables backface culling ([gpu.CullNone]); when false, back faces are culled ([gpu.CullBack]).
	DoubleSided bool
	// SampleCount sets the MSAA sample count (0 or 1 for non-MSAA, 4 for 4x MSAA).
	SampleCount int
	// LinearOutput compiles [MaterialIBLLinearWGSL] instead of [MaterialIBLWGSL], emitting unclamped linear HDR radiance for an [HDRTarget] resolved by [ToneMapPass].
	LinearOutput bool
	// Retained opts the pipeline into automatic recreation after a GPU device loss.
	Retained bool
}

// NewMaterialIBLPipeline compiles [MaterialIBLWGSL] (or [MaterialIBLLinearWGSL] when opts.LinearOutput is true) and builds a render pipeline using [MeshVertexLayout] at vertex slot 0, camLayout at [CameraGroup] (group 0), matLayout at [MaterialGroup] (group 1), and iblLayout ([GPUIBLBuffer.Layout]) at [MaterialIBLGroup] (group 2).
func NewMaterialIBLPipeline(cx *core.Scope, dev *gpu.Device, camLayout, matLayout, iblLayout *gpu.BindGroupLayout, opts MaterialIBLPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if matLayout == nil || !matLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid material bind group layout"}
	}
	if iblLayout == nil || !iblLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid IBL bind group layout"}
	}
	if !validAlphaMode(opts.AlphaMode) {
		return nil, fmt.Errorf("%w: invalid AlphaMode %d", ErrInvalidMaterial, opts.AlphaMode)
	}
	src := MaterialIBLWGSL
	if opts.LinearOutput {
		src = MaterialIBLLinearWGSL
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
		label = "render3d.material_ibl." + opts.AlphaMode.String()
		if opts.LinearOutput {
			label += ".linear"
		}
	}
	layouts := []*gpu.BindGroupLayout{
		CameraGroup:      camLayout,
		MaterialGroup:    matLayout,
		MaterialIBLGroup: iblLayout,
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: cull},
		Targets:       []gpu.ColorTargetState{target},
		SampleCount:   opts.SampleCount,
		Retained:      opts.Retained,
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

// EvaluateMaterialIBL evaluates the exact pure-Go CPU reference matching [MaterialIBLWGSL] (and [MaterialIBLLinearWGSL] via [MaterialEvalResult].Linear): glTF 2.0 metallic-roughness texture factors, tangent-space normal perturbation, occlusion, emissive, and split-sum HDR image-based lighting via [EvaluateIBLHDR].
func EvaluateMaterialIBL(in MaterialEvalInput, sh IrradianceSH, prefilter *HDRCubemap, intensity float32) MaterialEvalResult {
	m := in.Material
	baseSample := [4]float32{1, 1, 1, 1}
	if in.HasBaseColorSample {
		baseSample = in.BaseColorSample
	}
	baseColor := [3]float32{
		m.BaseColorFactor[0] * baseSample[0],
		m.BaseColorFactor[1] * baseSample[1],
		m.BaseColorFactor[2] * baseSample[2],
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

	ibl := EvaluateIBLHDR(sh, prefilter, intensity, n, in.ViewDir, baseColor, metallic, roughness, occlusion)
	linearRGB := math3d.Vec3{
		X: ibl[0] + emissive.X,
		Y: ibl[1] + emissive.Y,
		Z: ibl[2] + emissive.Z,
	}
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

// --- Radiance .hdr (RGBE) linear floating-point images ---

const (
	maxHDRImageDim    = 16384
	maxHDRImagePixels = 64 * 1024 * 1024
)

// HDRImage holds a 2D linear floating-point RGB image (such as a decoded Radiance .hdr equirectangular environment map) in row-major order, top row first.
type HDRImage struct {
	Width  int
	Height int
	RGB    [][3]float32
}

// NewHDRImage allocates a zeroed linear RGB HDRImage of dimensions width x height. Non-positive dimensions return an error wrapping [ErrInvalidCubemap].
func NewHDRImage(width, height int) (*HDRImage, error) {
	if width <= 0 || height <= 0 || width > maxHDRImageDim || height > maxHDRImageDim || int64(width)*int64(height) > maxHDRImagePixels {
		return nil, fmt.Errorf("%w: invalid HDR image dimensions %dx%d", ErrInvalidCubemap, width, height)
	}
	return &HDRImage{
		Width:  width,
		Height: height,
		RGB:    make([][3]float32, width*height),
	}, nil
}

// Validate checks that img is non-nil, has positive dimensions matching len(RGB), and contains only finite non-negative linear radiance values. Every error wraps [ErrInvalidCubemap].
func (img *HDRImage) Validate() error {
	if img == nil {
		return fmt.Errorf("%w: nil HDR image", ErrInvalidCubemap)
	}
	if img.Width <= 0 || img.Height <= 0 || img.Width > maxHDRImageDim || img.Height > maxHDRImageDim || int64(img.Width)*int64(img.Height) > maxHDRImagePixels {
		return fmt.Errorf("%w: invalid HDR image dimensions %dx%d", ErrInvalidCubemap, img.Width, img.Height)
	}
	if len(img.RGB) != img.Width*img.Height {
		return fmt.Errorf("%w: HDR image pixel count %d does not match %dx%d", ErrInvalidCubemap, len(img.RGB), img.Width, img.Height)
	}
	for i, c := range img.RGB {
		if !finite(c[0]) || !finite(c[1]) || !finite(c[2]) || c[0] < 0 || c[1] < 0 || c[2] < 0 {
			return fmt.Errorf("%w: HDR image pixel %d has invalid radiance %v", ErrInvalidCubemap, i, c)
		}
	}
	return nil
}

// At returns the linear RGB radiance at (x, y), or zero when out of bounds.
func (img *HDRImage) At(x, y int) [3]float32 {
	if img == nil || x < 0 || x >= img.Width || y < 0 || y >= img.Height || len(img.RGB) < img.Width*img.Height {
		return [3]float32{}
	}
	return img.RGB[y*img.Width+x]
}

// Set writes the linear RGB radiance at (x, y) when in bounds.
func (img *HDRImage) Set(x, y int, rgb [3]float32) {
	if img == nil || x < 0 || x >= img.Width || y < 0 || y >= img.Height || len(img.RGB) < img.Width*img.Height {
		return
	}
	img.RGB[y*img.Width+x] = rgb
}

// DecodeHDR decodes a Radiance RGBE (.hdr / .pic) image stream into linear floating-point RGB radiance values. It supports standard adaptive RLE, old-style RLE, and uncompressed scanlines, as well as the standard Radiance EXPOSURE header and +/-Y +/-X scanline orientations. Malformed streams return an error wrapping [ErrInvalidCubemap].
func DecodeHDR(r io.Reader) (*HDRImage, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: nil HDR reader", ErrInvalidCubemap)
	}
	br := bufio.NewReader(r)

	firstLine, err := readHDRLine(br)
	if err != nil {
		return nil, fmt.Errorf("%w: read HDR signature: %w", ErrInvalidCubemap, err)
	}
	if !strings.HasPrefix(firstLine, "#?") {
		return nil, fmt.Errorf("%w: missing Radiance #? header signature", ErrInvalidCubemap)
	}

	exposure := 1.0
	hasFormat := false
	for {
		line, err := readHDRLine(br)
		if err != nil {
			return nil, fmt.Errorf("%w: read HDR header: %w", ErrInvalidCubemap, err)
		}
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if val, ok := strings.CutPrefix(line, "FORMAT="); ok {
			val = strings.TrimSpace(val)
			if val != "32-bit_rle_rgbe" {
				return nil, fmt.Errorf("%w: unsupported Radiance HDR format %q", ErrInvalidCubemap, val)
			}
			hasFormat = true
		} else if val, ok := strings.CutPrefix(line, "EXPOSURE="); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && f > 0 && !math.IsInf(f, 0) {
				exposure *= f
			}
		}
	}
	if !hasFormat {
		return nil, fmt.Errorf("%w: missing FORMAT=32-bit_rle_rgbe in HDR header", ErrInvalidCubemap)
	}

	resLine, err := readHDRLine(br)
	if err != nil {
		return nil, fmt.Errorf("%w: read HDR resolution line: %w", ErrInvalidCubemap, err)
	}
	fields := strings.Fields(resLine)
	if len(fields) != 4 {
		return nil, fmt.Errorf("%w: invalid HDR resolution line %q", ErrInvalidCubemap, resLine)
	}
	var width, height int
	var flipY, flipX bool
	switch fields[0] {
	case "-Y":
		flipY = false
	case "+Y":
		flipY = true
	default:
		return nil, fmt.Errorf("%w: unsupported HDR Y orientation %q", ErrInvalidCubemap, fields[0])
	}
	hVal, err := strconv.Atoi(fields[1])
	if err != nil || hVal <= 0 {
		return nil, fmt.Errorf("%w: invalid HDR height %q", ErrInvalidCubemap, fields[1])
	}
	switch fields[2] {
	case "+X":
		flipX = false
	case "-X":
		flipX = true
	default:
		return nil, fmt.Errorf("%w: unsupported HDR X orientation %q", ErrInvalidCubemap, fields[2])
	}
	wVal, err := strconv.Atoi(fields[3])
	if err != nil || wVal <= 0 {
		return nil, fmt.Errorf("%w: invalid HDR width %q", ErrInvalidCubemap, fields[3])
	}
	width, height = wVal, hVal

	img, err := NewHDRImage(width, height)
	if err != nil {
		return nil, err
	}

	scan := make([]byte, width*4)
	invExposure := 1.0 / exposure
	for row := range height {
		if err := readHDRScanline(br, scan, width); err != nil {
			return nil, fmt.Errorf("%w: scanline %d: %w", ErrInvalidCubemap, row, err)
		}
		dstY := row
		if flipY {
			dstY = height - 1 - row
		}
		rowOff := dstY * width
		for col := range width {
			dstX := col
			if flipX {
				dstX = width - 1 - col
			}
			rB := scan[col*4+0]
			gB := scan[col*4+1]
			bB := scan[col*4+2]
			eB := scan[col*4+3]
			img.RGB[rowOff+dstX] = rgbeToLinearRGB(rB, gB, bB, eB, invExposure)
		}
	}
	return img, nil
}

// EncodeHDR writes img in Radiance RGBE (.hdr) format (using adaptive RLE for widths in [8, 32767] and uncompressed scanlines otherwise). Invalid images return an error wrapping [ErrInvalidCubemap].
func EncodeHDR(w io.Writer, img *HDRImage) error {
	if w == nil {
		return fmt.Errorf("%w: nil HDR writer", ErrInvalidCubemap)
	}
	if err := img.Validate(); err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if _, err := fmt.Fprintf(bw, "#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y %d +X %d\n", img.Height, img.Width); err != nil {
		return err
	}
	useRLE := img.Width >= 8 && img.Width <= 32767
	if !useRLE {
		var pix [4]byte
		for y := range img.Height {
			rowOff := y * img.Width
			for x := range img.Width {
				pix = linearRGBToRGBE(img.RGB[rowOff+x])
				if _, err := bw.Write(pix[:]); err != nil {
					return err
				}
			}
		}
		return bw.Flush()
	}

	planes := [4][]byte{
		make([]byte, img.Width),
		make([]byte, img.Width),
		make([]byte, img.Width),
		make([]byte, img.Width),
	}
	var hdr [4]byte
	hdr[0] = 2
	hdr[1] = 2
	hdr[2] = byte(img.Width >> 8)
	hdr[3] = byte(img.Width & 0xFF)
	for y := range img.Height {
		rowOff := y * img.Width
		for x := range img.Width {
			p := linearRGBToRGBE(img.RGB[rowOff+x])
			planes[0][x] = p[0]
			planes[1][x] = p[1]
			planes[2][x] = p[2]
			planes[3][x] = p[3]
		}
		if _, err := bw.Write(hdr[:]); err != nil {
			return err
		}
		for ch := range 4 {
			if err := writeHDRRLEChannel(bw, planes[ch]); err != nil {
				return err
			}
		}
	}
	return bw.Flush()
}

func readHDRLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func readHDRScanline(br *bufio.Reader, dst []byte, width int) error {
	var head [4]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return err
	}
	// New-style adaptive RLE: 0x02 0x02 <hi> <lo> where <hi> has high bit clear.
	if width >= 8 && width <= 32767 && head[0] == 2 && head[1] == 2 && (head[2]&0x80) == 0 {
		scanW := (int(head[2]) << 8) | int(head[3])
		if scanW != width {
			return fmt.Errorf("RLE scanline width %d does not match header width %d", scanW, width)
		}
		for ch := range 4 {
			x := 0
			for x < width {
				code, err := br.ReadByte()
				if err != nil {
					return err
				}
				if code > 128 {
					count := int(code) - 128
					if count == 0 || x+count > width {
						return fmt.Errorf("invalid RLE run length %d at x=%d (width %d)", count, x, width)
					}
					val, err := br.ReadByte()
					if err != nil {
						return err
					}
					for range count {
						dst[x*4+ch] = val
						x++
					}
				} else {
					count := int(code)
					if count == 0 || x+count > width {
						return fmt.Errorf("invalid RLE literal length %d at x=%d (width %d)", count, x, width)
					}
					for range count {
						val, err := br.ReadByte()
						if err != nil {
							return err
						}
						dst[x*4+ch] = val
						x++
					}
				}
			}
		}
		return nil
	}

	// Uncompressed or old-style RLE scanline.
	copy(dst[0:4], head[:])
	x := 1
	rleShift := uint(0)
	for x < width {
		var p [4]byte
		if _, err := io.ReadFull(br, p[:]); err != nil {
			return err
		}
		if p[0] == 1 && p[1] == 1 && p[2] == 1 {
			count := int(p[3]) << rleShift
			if count <= 0 || x+count > width {
				return fmt.Errorf("invalid old-style RLE run length %d at x=%d (width %d)", count, x, width)
			}
			prev := dst[(x-1)*4 : x*4]
			for range count {
				copy(dst[x*4:x*4+4], prev)
				x++
			}
			rleShift += 8
		} else {
			copy(dst[x*4:x*4+4], p[:])
			x++
			rleShift = 0
		}
	}
	return nil
}

func writeHDRRLEChannel(bw *bufio.Writer, data []byte) error {
	n := len(data)
	i := 0
	for i < n {
		// Check for a run of >= 4 identical bytes.
		runLen := 1
		for i+runLen < n && runLen < 127 && data[i+runLen] == data[i] {
			runLen++
		}
		if runLen >= 4 {
			if err := bw.WriteByte(byte(128 + runLen)); err != nil {
				return err
			}
			if err := bw.WriteByte(data[i]); err != nil {
				return err
			}
			i += runLen
			continue
		}
		// Accumulate literal bytes until a run of >= 4 or 128 bytes.
		litStart := i
		i++
		for i < n && i-litStart < 128 {
			if i+3 < n && data[i] == data[i+1] && data[i] == data[i+2] && data[i] == data[i+3] {
				break
			}
			i++
		}
		litLen := i - litStart
		if err := bw.WriteByte(byte(litLen)); err != nil {
			return err
		}
		if _, err := bw.Write(data[litStart:i]); err != nil {
			return err
		}
	}
	return nil
}

func rgbeToLinearRGB(r, g, b, e byte, invExposure float64) [3]float32 {
	if e == 0 {
		return [3]float32{}
	}
	scale := math.Ldexp(1.0, int(e)-(128+8)) * invExposure
	return [3]float32{
		float32(float64(r) * scale),
		float32(float64(g) * scale),
		float32(float64(b) * scale),
	}
}

func linearRGBToRGBE(c [3]float32) [4]byte {
	r := max(float64(c[0]), 0)
	g := max(float64(c[1]), 0)
	b := max(float64(c[2]), 0)
	v := max(r, max(g, b))
	if v < 1e-32 {
		return [4]byte{}
	}
	m, exp := math.Frexp(v)
	scale := m * 256.0 / v
	eb := exp + 128
	if eb < 1 {
		return [4]byte{}
	}
	if eb > 255 {
		return [4]byte{255, 255, 255, 255}
	}
	clampU8 := func(x float64) byte {
		if x <= 0 {
			return 0
		}
		if x >= 255 {
			return 255
		}
		return byte(x)
	}
	return [4]byte{
		clampU8(r * scale),
		clampU8(g * scale),
		clampU8(b * scale),
		byte(eb),
	}
}

// --- Linear Float32 HDR Cubemap ---

// HDRCubemap holds a 6-face square linear floating-point cubemap on the CPU in canonical WebGPU / Metal / Direct3D / OpenGL order ([gpu.CubeFacePosX]..[gpu.CubeFaceNegZ]), preserving high dynamic range radiance above 1.0 without clamping.
//
// Faces holds mip level 0: six slices of Size*Size linear RGB radiance triples in row-major order. Mips optionally holds precomputed mip levels starting at level 1 (such as GGX-prefiltered HDR specular levels from [PrefilterEnvironmentHDR]). Upload to the GPU with [UploadCubemapHDR] (preferring [gpu.FormatRGBA16Float]).
type HDRCubemap struct {
	Size  int
	Faces [6][][3]float32
	Mips  [][6][][3]float32
}

// NewHDRCubemap allocates a zeroed linear float32 [HDRCubemap] with face edge length size.
func NewHDRCubemap(size int) (*HDRCubemap, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: HDR cubemap size %d must be positive", ErrInvalidCubemap, size)
	}
	cm := &HDRCubemap{Size: size}
	n := size * size
	for face := range 6 {
		cm.Faces[face] = make([][3]float32, n)
	}
	return cm, nil
}

// Validate checks that c is non-nil, Size is positive, every base face has Size*Size finite non-negative linear RGB texels, and every optional mip level has the exact expected dimensions down to 1x1. Every error wraps [ErrInvalidCubemap].
func (c *HDRCubemap) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: nil HDR cubemap", ErrInvalidCubemap)
	}
	if c.Size <= 0 {
		return fmt.Errorf("%w: HDR cubemap size %d must be positive", ErrInvalidCubemap, c.Size)
	}
	baseCount := c.Size * c.Size
	for face := range 6 {
		pix := c.Faces[face]
		if len(pix) != baseCount {
			return fmt.Errorf("%w: HDR face %d length %d does not match %dx%d", ErrInvalidCubemap, face, len(pix), c.Size, c.Size)
		}
		for i, rgb := range pix {
			if !finite(rgb[0]) || !finite(rgb[1]) || !finite(rgb[2]) || rgb[0] < 0 || rgb[1] < 0 || rgb[2] < 0 {
				return fmt.Errorf("%w: HDR face %d texel %d has invalid radiance %v", ErrInvalidCubemap, face, i, rgb)
			}
		}
	}
	totalMips := 1 + len(c.Mips)
	maxMips := int(gpu.MaxMipLevels(uint32(c.Size), uint32(c.Size)))
	if totalMips > maxMips {
		return fmt.Errorf("%w: %d HDR mip levels exceed maximum %d for size %d", ErrInvalidCubemap, totalMips, maxMips, c.Size)
	}
	for m, level := range c.Mips {
		mw, _ := gpu.MipSize(uint32(c.Size), uint32(c.Size), uint32(m+1))
		wantCount := int(mw * mw)
		for face := range 6 {
			pix := level[face]
			if len(pix) != wantCount {
				return fmt.Errorf("%w: HDR mip %d face %d length %d, want %d (%dx%d)", ErrInvalidCubemap, m+1, face, len(pix), wantCount, mw, mw)
			}
			for i, rgb := range pix {
				if !finite(rgb[0]) || !finite(rgb[1]) || !finite(rgb[2]) || rgb[0] < 0 || rgb[1] < 0 || rgb[2] < 0 {
					return fmt.Errorf("%w: HDR mip %d face %d texel %d has invalid radiance %v", ErrInvalidCubemap, m+1, face, i, rgb)
				}
			}
		}
	}
	return nil
}

// MipLevels returns the total number of mip levels stored in c (1 when Mips is empty), or 0 if invalid.
func (c *HDRCubemap) MipLevels() int {
	if c == nil || c.Size <= 0 {
		return 0
	}
	return 1 + len(c.Mips)
}

// At returns the linear RGB radiance at mip 0 texel (x, y) on face, or zero when out of bounds.
func (c *HDRCubemap) At(face gpu.CubeFace, x, y int) [3]float32 {
	if c == nil || face >= 6 || x < 0 || x >= c.Size || y < 0 || y >= c.Size || len(c.Faces[face]) < c.Size*c.Size {
		return [3]float32{}
	}
	return c.Faces[face][y*c.Size+x]
}

// Set writes the linear RGB radiance at mip 0 texel (x, y) on face when in bounds.
func (c *HDRCubemap) Set(face gpu.CubeFace, x, y int, rgb [3]float32) {
	if c == nil || face >= 6 || x < 0 || x >= c.Size || y < 0 || y >= c.Size || len(c.Faces[face]) < c.Size*c.Size {
		return
	}
	c.Faces[face][y*c.Size+x] = rgb
}

func (c *HDRCubemap) faceAtMip(mip int, face gpu.CubeFace) ([][3]float32, int) {
	if mip <= 0 || len(c.Mips) == 0 {
		return c.Faces[face], c.Size
	}
	idx := min(mip-1, len(c.Mips)-1)
	mw, _ := gpu.MipSize(uint32(c.Size), uint32(c.Size), uint32(idx+1))
	return c.Mips[idx][face], int(mw)
}

// Sample bilinearly samples mip level 0 of c in direction dir and returns unclamped linear RGB radiance.
func (c *HDRCubemap) Sample(dir math3d.Vec3) [3]float32 {
	if c == nil || c.Size <= 0 {
		return [3]float32{}
	}
	face, u, v := CubeDirectionToFaceUV(dir)
	return sampleHDRFace(c.Faces[face], c.Size, u, v)
}

// SampleLOD trilinearly samples c at mip level lod (clamped to [0, MipLevels-1]) in direction dir and returns unclamped linear RGB radiance.
func (c *HDRCubemap) SampleLOD(dir math3d.Vec3, lod float32) [3]float32 {
	if c == nil || c.Size <= 0 {
		return [3]float32{}
	}
	face, u, v := CubeDirectionToFaceUV(dir)
	maxMip := len(c.Mips)
	if maxMip == 0 || lod <= 0 || math.IsNaN(float64(lod)) {
		return sampleHDRFace(c.Faces[face], c.Size, u, v)
	}
	fl := float64(lod)
	if fl >= float64(maxMip) {
		pix, sz := c.faceAtMip(maxMip, face)
		return sampleHDRFace(pix, sz, u, v)
	}
	lo := int(math.Floor(fl))
	hi := min(lo+1, maxMip)
	frac := float32(fl - float64(lo))
	p0, sz0 := c.faceAtMip(lo, face)
	c0 := sampleHDRFace(p0, sz0, u, v)
	if lo == hi || frac <= 0 {
		return c0
	}
	p1, sz1 := c.faceAtMip(hi, face)
	c1 := sampleHDRFace(p1, sz1, u, v)
	return [3]float32{
		c0[0] + (c1[0]-c0[0])*frac,
		c0[1] + (c1[1]-c0[1])*frac,
		c0[2] + (c1[2]-c0[2])*frac,
	}
}

// HDRCubemapFromCubemap promotes an LDR RGBA8 [*Cubemap] (decoding sRGB when cm.SRGB is true) into a linear float32 [*HDRCubemap], preserving any precomputed mip levels.
func HDRCubemapFromCubemap(cm *Cubemap) (*HDRCubemap, error) {
	if err := cm.Validate(); err != nil {
		return nil, err
	}
	size := cm.Size()
	out := &HDRCubemap{Size: size}
	convertLevel := func(src [6]*image.RGBA, sz int) [6][][3]float32 {
		var dst [6][][3]float32
		for face := range 6 {
			img := src[face]
			b := img.Bounds()
			pix := make([][3]float32, sz*sz)
			for y := range sz {
				for x := range sz {
					off := img.PixOffset(b.Min.X+x, b.Min.Y+y)
					r, g, bl := decodeTexelRGB(img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], cm.SRGB)
					pix[y*sz+x] = [3]float32{float32(r), float32(g), float32(bl)}
				}
			}
			dst[face] = pix
		}
		return dst
	}
	out.Faces = convertLevel(cm.Faces, size)
	if len(cm.Mips) > 0 {
		out.Mips = make([][6][][3]float32, len(cm.Mips))
		for m, lvl := range cm.Mips {
			mw, _ := gpu.MipSize(uint32(size), uint32(size), uint32(m+1))
			out.Mips[m] = convertLevel(lvl, int(mw))
		}
	}
	return out, nil
}

// GradientSkyHDRCubemap builds a procedural 3-band linear HDR sky cubemap interpolating smoothly from horizon to zenith across the upper hemisphere (y >= 0) and from horizon to ground across the lower hemisphere (y < 0). Unlike [GradientSkyCubemap], linear RGB values above 1.0 are preserved without clamping.
func GradientSkyHDRCubemap(zenith, horizon, ground [3]float32, faceSize int) (*HDRCubemap, error) {
	if faceSize <= 0 {
		return nil, fmt.Errorf("%w: faceSize %d must be positive", ErrInvalidCubemap, faceSize)
	}
	for _, c := range [...][3]float32{zenith, horizon, ground} {
		if !finite(c[0]) || !finite(c[1]) || !finite(c[2]) || c[0] < 0 || c[1] < 0 || c[2] < 0 {
			return nil, fmt.Errorf("%w: gradient sky colors must be finite and non-negative", ErrInvalidCubemap)
		}
	}
	cm, err := NewHDRCubemap(faceSize)
	if err != nil {
		return nil, err
	}
	invSize := 1.0 / float64(faceSize)
	for face := range gpu.CubeFace(6) {
		dst := cm.Faces[face]
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
				dst[py*faceSize+px] = [3]float32{float32(col[0]), float32(col[1]), float32(col[2])}
			}
		}
	}
	return cm, nil
}

// CubemapFromHDREquirectangular projects a linear floating-point latitude-longitude (equirectangular) [HDRImage] into a 6-face [HDRCubemap] of resolution faceSize x faceSize per face using seamless horizontal wrapping and bilinear filtering in linear light, preserving HDR radiance above 1.0.
//
// Longitude u = 0.5 + atan2(z, x) / (2*pi) maps +X to u = 0.5 and +Z to u = 0.75; latitude v = acos(y) / pi maps +Y (zenith) to the top row (v = 0) and -Y (nadir) to the bottom row (v = 1).
func CubemapFromHDREquirectangular(img *HDRImage, faceSize int) (*HDRCubemap, error) {
	if err := img.Validate(); err != nil {
		return nil, err
	}
	if faceSize <= 0 {
		return nil, fmt.Errorf("%w: faceSize %d must be positive", ErrInvalidCubemap, faceSize)
	}
	cm, err := NewHDRCubemap(faceSize)
	if err != nil {
		return nil, err
	}
	invSize := 1.0 / float64(faceSize)
	for face := range gpu.CubeFace(6) {
		dst := cm.Faces[face]
		for py := range faceSize {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range faceSize {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				phi := math.Atan2(float64(d.Z), float64(d.X))
				theta := math.Acos(clamp64(float64(d.Y), -1, 1))
				eu := 0.5 + phi/(2*math.Pi)
				ev := theta / math.Pi
				dst[py*faceSize+px] = sampleEquirectHDRBilinear(img, eu, ev)
			}
		}
	}
	return cm, nil
}

// CubemapFromHDR decodes a Radiance .hdr (RGBE) equirectangular panorama from r and projects it into a 6-face linear float32 [HDRCubemap] of resolution faceSize x faceSize per face.
func CubemapFromHDR(r io.Reader, faceSize int) (*HDRCubemap, error) {
	if faceSize <= 0 {
		return nil, fmt.Errorf("%w: faceSize %d must be positive", ErrInvalidCubemap, faceSize)
	}
	img, err := DecodeHDR(r)
	if err != nil {
		return nil, err
	}
	return CubemapFromHDREquirectangular(img, faceSize)
}

// ComputeIrradianceSHHDR projects env's base mip level onto L2 spherical harmonics using exact analytical per-texel cubemap solid angles (summing to 4*pi across all six faces) and applies the Lambertian cosine-lobe convolution weights (pi, 2*pi/3, pi/4), preserving HDR radiance above 1.0.
func ComputeIrradianceSHHDR(env *HDRCubemap) (IrradianceSH, error) {
	if err := env.Validate(); err != nil {
		return IrradianceSH{}, err
	}
	size := env.Size
	invSize := 1.0 / float64(size)
	var accum [9][3]float64
	for face := range gpu.CubeFace(6) {
		pix := env.Faces[face]
		for py := range size {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range size {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				dw := cubemapTexelSolidAngle(px, py, size)
				rgb := pix[py*size+px]
				r, g, bl := float64(rgb[0]), float64(rgb[1]), float64(rgb[2])

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

// PrefilterEnvironmentHDR builds a split-sum GGX prefiltered specular [HDRCubemap] mip chain from env (Karis 2013), preserving high dynamic range radiance above 1.0 across all roughness levels. Mip level 0 corresponds to roughness 0 (mirror reflection) and mip level MipLevels-1 corresponds to roughness 1.
func PrefilterEnvironmentHDR(env *HDRCubemap, opts PrefilterOptions) (*HDRCubemap, error) {
	if err := env.Validate(); err != nil {
		return nil, err
	}
	baseSize := opts.BaseSize
	if baseSize <= 0 {
		baseSize = env.Size
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

	srcPyr := buildLinearHDRCubemapPyramid(env)
	srcBaseSize := float64(env.Size)
	saTexel := 4.0 * math.Pi / (6.0 * srcBaseSize * srcBaseSize)

	out := &HDRCubemap{Size: baseSize}
	if mipLevels > 1 {
		out.Mips = make([][6][][3]float32, mipLevels-1)
	}

	for lvl := range mipLevels {
		mwU, _ := gpu.MipSize(uint32(baseSize), uint32(baseSize), uint32(lvl))
		mw := int(mwU)
		invW := 1.0 / float64(mw)
		var roughness float64
		if mipLevels > 1 {
			roughness = float64(lvl) / float64(mipLevels-1)
		}
		var faces [6][][3]float32
		for face := range gpu.CubeFace(6) {
			dst := make([][3]float32, mw*mw)
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
					dst[py*mw+px] = [3]float32{
						float32(max(col[0], 0)),
						float32(max(col[1], 0)),
						float32(max(col[2], 0)),
					}
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

// EvaluateIBLHDR evaluates the exact CPU reference for [IBLWGSL]'s evaluate_ibl function using an unclamped linear [*HDRCubemap] prefiltered specular environment.
func EvaluateIBLHDR(
	sh IrradianceSH,
	prefilter *HDRCubemap,
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

// UploadCubemapHDR validates cm and uploads all six faces and their mip chain to dev as a linear floating-point [GPUCubemap], scoped to cx.
//
// When opts.Format is [gpu.FormatUndefined], [gpu.FormatRGBA16Float] is used whenever dev supports it (preserving HDR radiance up to 65504.0 per channel), falling back to [gpu.FormatRGBA8Unorm] only on devices that lack half-float texture support. Explicit opts.Format may select [gpu.FormatRGBA16Float], [gpu.FormatRGBA32Float] (when supported by dev), or [gpu.FormatRGBA8Unorm]. When opts.GenerateMips is true and cm has no precomputed Mips, a linear-light 2x2 box-filtered mip chain is built on the CPU before uploading.
func UploadCubemapHDR(cx *core.Scope, dev *gpu.Device, cm *HDRCubemap, opts CubemapUpload) (*GPUCubemap, error) {
	if err := cm.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	format := opts.Format
	if format == gpu.FormatUndefined {
		if dev.SupportsFormat(gpu.FormatRGBA16Float) {
			format = gpu.FormatRGBA16Float
		} else {
			format = gpu.FormatRGBA8Unorm
		}
	}
	switch format {
	case gpu.FormatRGBA16Float, gpu.FormatRGBA32Float:
		if !dev.SupportsFormat(format) {
			return nil, fmt.Errorf("%w: upload format %s is not supported by this device", ErrInvalidCubemap, format)
		}
	case gpu.FormatRGBA8Unorm:
	default:
		return nil, fmt.Errorf("%w: unsupported HDR cubemap upload format %s (expected RGBA16Float, RGBA32Float, or RGBA8Unorm)", ErrInvalidCubemap, format)
	}

	size := uint32(cm.Size)
	type hdrLevel struct {
		size  int
		faces [6][][3]float32
	}
	var levels []hdrLevel
	if len(cm.Mips) > 0 {
		levels = make([]hdrLevel, 1+len(cm.Mips))
		levels[0] = hdrLevel{size: cm.Size, faces: cm.Faces}
		for m, lvl := range cm.Mips {
			mw, _ := gpu.MipSize(size, size, uint32(m+1))
			levels[m+1] = hdrLevel{size: int(mw), faces: lvl}
		}
	} else if opts.GenerateMips {
		pyr := buildLinearHDRCubemapPyramid(cm)
		levels = make([]hdrLevel, len(pyr.levels))
		for l, pl := range pyr.levels {
			var faces [6][][3]float32
			for face := range 6 {
				src := pl.faces[face]
				dst := make([][3]float32, len(src))
				for i, c := range src {
					dst[i] = [3]float32{float32(c[0]), float32(c[1]), float32(c[2])}
				}
				faces[face] = dst
			}
			levels[l] = hdrLevel{size: pl.size, faces: faces}
		}
	} else {
		levels = []hdrLevel{{size: cm.Size, faces: cm.Faces}}
	}
	mipLevels := uint32(len(levels))

	texLabel := opts.Label
	if texLabel == "" {
		texLabel = "render3d.cubemap_hdr"
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
	for lvl, lv := range levels {
		for face := range gpu.CubeFace(6) {
			data, bytesPerRow := encodeHDRFaceBytes(lv.faces[face], lv.size, format)
			dev.WriteTextureMip(tex, uint32(lvl), uint32(face), data, bytesPerRow)
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

// --- Internal HDR sampling, pyramid, and float16 packing helpers ---

func sampleHDRFace(pix [][3]float32, size int, u, v float32) [3]float32 {
	if size <= 0 || len(pix) < size*size {
		return [3]float32{}
	}
	fx := float64(u)*float64(size) - 0.5
	fy := float64(v)*float64(size) - 0.5
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	tx := fx - float64(x0)
	ty := fy - float64(y0)
	x1 := clampInt(x0+1, 0, size-1)
	y1 := clampInt(y0+1, 0, size-1)
	x0 = clampInt(x0, 0, size-1)
	y0 = clampInt(y0, 0, size-1)

	c00 := pix[y0*size+x0]
	c10 := pix[y0*size+x1]
	c01 := pix[y1*size+x0]
	c11 := pix[y1*size+x1]

	w00 := (1 - tx) * (1 - ty)
	w10 := tx * (1 - ty)
	w01 := (1 - tx) * ty
	w11 := tx * ty
	return [3]float32{
		float32(float64(c00[0])*w00 + float64(c10[0])*w10 + float64(c01[0])*w01 + float64(c11[0])*w11),
		float32(float64(c00[1])*w00 + float64(c10[1])*w10 + float64(c01[1])*w01 + float64(c11[1])*w11),
		float32(float64(c00[2])*w00 + float64(c10[2])*w10 + float64(c01[2])*w01 + float64(c11[2])*w11),
	}
}

func sampleEquirectHDRBilinear(img *HDRImage, u, v float64) [3]float32 {
	w, h := img.Width, img.Height
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

	c00 := img.RGB[sy0*w+sx0]
	c10 := img.RGB[sy0*w+sx1]
	c01 := img.RGB[sy1*w+sx0]
	c11 := img.RGB[sy1*w+sx1]

	w00 := (1 - tx) * (1 - ty)
	w10 := tx * (1 - ty)
	w01 := (1 - tx) * ty
	w11 := tx * ty
	return [3]float32{
		float32(float64(c00[0])*w00 + float64(c10[0])*w10 + float64(c01[0])*w01 + float64(c11[0])*w11),
		float32(float64(c00[1])*w00 + float64(c10[1])*w10 + float64(c01[1])*w01 + float64(c11[1])*w11),
		float32(float64(c00[2])*w00 + float64(c10[2])*w10 + float64(c01[2])*w01 + float64(c11[2])*w11),
	}
}

func buildLinearHDRCubemapPyramid(env *HDRCubemap) *linearCubePyramid {
	size := env.Size
	nMips := int(gpu.MaxMipLevels(uint32(size), uint32(size)))
	levels := make([]linearCubeLevel, nMips)

	lvl0 := linearCubeLevel{size: size}
	for face := range 6 {
		src := env.Faces[face]
		pix := make([][3]float64, size*size)
		for i, c := range src {
			pix[i] = [3]float64{float64(c[0]), float64(c[1]), float64(c[2])}
		}
		lvl0.faces[face] = pix
	}
	levels[0] = lvl0

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

func encodeHDRFaceBytes(pix [][3]float32, size int, format gpu.TextureFormat) ([]byte, uint32) {
	switch format {
	case gpu.FormatRGBA16Float:
		const oneHalf uint16 = 0x3C00 // 1.0 in IEEE 754 binary16
		bytesPerRow := uint32(size * 8)
		out := make([]byte, size*size*8)
		for i, c := range pix {
			off := i * 8
			binary.LittleEndian.PutUint16(out[off+0:], float32ToHalfFinite(c[0]))
			binary.LittleEndian.PutUint16(out[off+2:], float32ToHalfFinite(c[1]))
			binary.LittleEndian.PutUint16(out[off+4:], float32ToHalfFinite(c[2]))
			binary.LittleEndian.PutUint16(out[off+6:], oneHalf)
		}
		return out, bytesPerRow
	case gpu.FormatRGBA32Float:
		oneBits := math.Float32bits(1.0)
		bytesPerRow := uint32(size * 16)
		out := make([]byte, size*size*16)
		for i, c := range pix {
			off := i * 16
			binary.LittleEndian.PutUint32(out[off+0:], math.Float32bits(max(c[0], 0)))
			binary.LittleEndian.PutUint32(out[off+4:], math.Float32bits(max(c[1], 0)))
			binary.LittleEndian.PutUint32(out[off+8:], math.Float32bits(max(c[2], 0)))
			binary.LittleEndian.PutUint32(out[off+12:], oneBits)
		}
		return out, bytesPerRow
	default: // FormatRGBA8Unorm fallback
		bytesPerRow := uint32(size * 4)
		out := make([]byte, size*size*4)
		for i, c := range pix {
			off := i * 4
			out[off+0] = floatToByte(float64(c[0]))
			out[off+1] = floatToByte(float64(c[1]))
			out[off+2] = floatToByte(float64(c[2]))
			out[off+3] = 255
		}
		return out, bytesPerRow
	}
}

// float32ToHalfFinite converts a non-negative linear radiance float32 to IEEE 754 binary16 with round-to-nearest-even, clamping NaN/negative to 0 and values above maxHDRChannel (65504) to the maximum finite half-float (0x7BFF) so shaders never sample Inf or NaN.
func float32ToHalfFinite(f float32) uint16 {
	if !(f > 0) {
		return 0
	}
	if f >= maxHDRChannel {
		return 0x7BFF
	}
	bits := math.Float32bits(f)
	exp := int((bits>>23)&0xFF) - 127 + 15
	mant := bits & 0x007FFFFF

	if exp <= 0 {
		if exp < -10 {
			return 0
		}
		mant |= 0x00800000
		shift := uint(14 - exp)
		half := uint16(mant >> shift)
		roundBit := uint32(1) << (shift - 1)
		if (mant&roundBit) != 0 && ((mant&(roundBit-1)) != 0 || (half&1) != 0) {
			half++
		}
		return half
	}
	if exp >= 31 {
		return 0x7BFF
	}
	half := uint16((exp << 10) | int(mant>>13))
	if (mant&0x1000) != 0 && ((mant&0x0FFF) != 0 || (half&1) != 0) {
		half++
		if half >= 0x7C00 {
			return 0x7BFF
		}
	}
	return half
}
