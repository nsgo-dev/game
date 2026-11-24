package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Tone mapping uniform layout and bind-group constants (#250).
const (
	// ToneMapGroup is the @group index that [ToneMapWGSL] binds the HDR texture, sampler, and uniform parameters at.
	ToneMapGroup = 0

	// ToneMapUniformSize is the byte size of the uniform block written by [AppendToneMapUniforms] and declared by [ToneMapWGSL]: twelve little-endian float32 values (exposure, inv_gamma, operator_id, padding, then the source region's uv_scale, uv_min and uv_max as vec2 pairs, and two padding floats).
	ToneMapUniformSize = 48
)

// ErrInvalidHDRTarget is wrapped by errors returned when an [HDRTargetDesc] has invalid dimensions or parameters.
var ErrInvalidHDRTarget = errors.New("render3d: invalid HDR target")

// ToneMapOperator selects the HDR-to-LDR tone mapping curve applied by [ApplyToneMap] and [ToneMapPass] (#250).
type ToneMapOperator uint8

const (
	// ToneMapNone applies exposure only and clamps linear RGB to [0, 1] with no highlight roll-off.
	ToneMapNone ToneMapOperator = iota
	// ToneMapReinhard applies per-channel Reinhard compression: c / (1 + c).
	ToneMapReinhard
	// ToneMapACES applies Krzysztof Narkowicz's fitted ACES filmic curve: (c*(2.51*c + 0.03)) / (c*(2.43*c + 0.59) + 0.14).
	ToneMapACES
	// ToneMapNeutral applies the Khronos PBR Neutral tone mapper, preserving mid-tone hues and smoothly desaturating bright highlights toward white.
	ToneMapNeutral
	// ToneMapAgX applies Troy Sobotka's AgX default look (inset matrix, log2 range mapping, 6th-order polynomial sigmoid, outset matrix, and display linearization), avoiding hue skew in bright saturated highlights.
	ToneMapAgX
)

// Valid reports whether op is a known tone mapping operator.
func (op ToneMapOperator) Valid() bool {
	return op <= ToneMapAgX
}

// String returns the lower-case name of op, or "unknown" for out-of-range values.
func (op ToneMapOperator) String() string {
	switch op {
	case ToneMapNone:
		return "none"
	case ToneMapReinhard:
		return "reinhard"
	case ToneMapACES:
		return "aces"
	case ToneMapNeutral:
		return "neutral"
	case ToneMapAgX:
		return "agx"
	default:
		return "unknown"
	}
}

// ToneMapOptions configures exposure, tone mapping, and display gamma encoding for [ApplyToneMap], [AppendToneMapUniforms], and [ToneMapPass] (#250).
type ToneMapOptions struct {
	// Operator selects the tone mapping curve. The zero value is [ToneMapNone]; use [DefaultToneMapOptions] for the recommended [ToneMapACES] preset.
	Operator ToneMapOperator
	// Exposure is the linear exposure multiplier applied before tone mapping. Zero renders black, so a fade to black can interpolate it down to 0; negative, NaN and infinite values are treated as 1.0 (0 EV). Compute it from photographic stops with [ExposureFromEV], and start from [DefaultToneMapOptions] so the zero value of this field does not leave the image black.
	Exposure float32
	// Gamma selects the display transfer function when LinearOutput is false: zero (or non-positive / non-finite) applies the exact IEC 61966-2-1 linear-to-sRGB OETF; a positive finite value (for example 2.2) applies power-law encoding pow(c, 1/Gamma).
	Gamma float32
	// LinearOutput skips display OETF encoding and emits tone-mapped linear RGB in [0, 1]. [ToneMapPass] automatically enables linear output when its target format is sRGB (TargetFormat.IsSRGB()) so GPU hardware encodes linear-to-sRGB exactly once without double gamma.
	LinearOutput bool
}

// DefaultToneMapOptions returns the recommended HDR tone-mapping defaults: [ToneMapACES], exposure 1.0 (0 EV), and exact sRGB encoding (Gamma 0).
func DefaultToneMapOptions() ToneMapOptions {
	return ToneMapOptions{
		Operator: ToneMapACES,
		Exposure: 1.0,
	}
}

// Normalized returns a copy of o with invalid fields canonicalized identically for CPU and GPU evaluation: a negative, NaN or infinite Exposure becomes 1.0 (zero stays black), a non-positive or non-finite Gamma becomes 0 (exact sRGB), and unknown Operator values become [ToneMapNone].
func (o ToneMapOptions) Normalized() ToneMapOptions {
	if !o.Operator.Valid() {
		o.Operator = ToneMapNone
	}
	if !(o.Exposure >= 0) || isInf32(o.Exposure) {
		o.Exposure = 1.0
	}
	if !(o.Gamma > 0) || isInf32(o.Gamma) {
		o.Gamma = 0
	}
	return o
}

// ExposureFromEV converts photographic exposure compensation in stops (EV) to a linear exposure multiplier 2^ev (0 -> 1.0, +1 -> 2.0, -1 -> 0.5). NaN returns 1.0, and ev is clamped to [-126, 126] (including the infinities), so the result is always finite and positive; set [ToneMapOptions].Exposure to 0 for black.
func ExposureFromEV(ev float32) float32 {
	if isNaN32(ev) {
		return 1.0
	}
	if ev < -126 {
		return float32(math.Exp2(-126))
	}
	if ev > 126 {
		return float32(math.Exp2(126))
	}
	return float32(math.Exp2(float64(ev)))
}

// ApplyToneMap evaluates the CPU reference tone-mapping and display-encoding pipeline matching [ToneMapWGSL]:
//  1. Scale linear HDR rgb by normalized Exposure and clamp it to [0, 65504], mapping NaN to 0 (sanitize_hdr in the shader).
//  2. Apply opts.Operator (None, Reinhard, ACES, Khronos PBR Neutral, or AgX).
//  3. Unless opts.LinearOutput is true, encode to display space using exact sRGB (Gamma <= 0) or power-law pow(c, 1/Gamma) (Gamma > 0).
func ApplyToneMap(rgb math3d.Vec3, opts ToneMapOptions) math3d.Vec3 {
	n := opts.Normalized()
	exposed := math3d.Vec3{
		X: sanitizeHDRChannel(rgb.X, n.Exposure),
		Y: sanitizeHDRChannel(rgb.Y, n.Exposure),
		Z: sanitizeHDRChannel(rgb.Z, n.Exposure),
	}

	var mapped math3d.Vec3
	switch n.Operator {
	case ToneMapReinhard:
		mapped = toneMapReinhardCPU(exposed)
	case ToneMapACES:
		mapped = toneMapACESCPU(exposed)
	case ToneMapNeutral:
		mapped = toneMapNeutralCPU(exposed)
	case ToneMapAgX:
		mapped = toneMapAgXCPU(exposed)
	default:
		mapped = math3d.Vec3{
			X: clamp01(exposed.X),
			Y: clamp01(exposed.Y),
			Z: clamp01(exposed.Z),
		}
	}

	if n.LinearOutput {
		return mapped
	}
	if n.Gamma > 0 {
		invGamma := 1.0 / float64(n.Gamma)
		return math3d.Vec3{
			X: safePow01(mapped.X, invGamma),
			Y: safePow01(mapped.Y, invGamma),
			Z: safePow01(mapped.Z, invGamma),
		}
	}
	return LinearToSRGB(mapped)
}

// maxHDRChannel is the largest finite half-float value; [ToneMapWGSL] and [ApplyToneMap] clamp exposed radiance to it so overflowed RGBA16Float highlights and extreme exposures stay finite.
const maxHDRChannel = 65504

// sanitizeHDRChannel mirrors sanitize_hdr in [ToneMapWGSL]: it scales v by exposure, maps NaN and negative results (including +Inf * 0) to 0, and clamps the rest to [maxHDRChannel].
func sanitizeHDRChannel(v, exposure float32) float32 {
	x := v * exposure
	if !(x > 0) {
		return 0
	}
	return min(x, maxHDRChannel)
}

func safePow01(v float32, p float64) float32 {
	c := clamp01(v)
	if c <= 0 {
		return 0
	}
	return float32(math.Pow(float64(c), p))
}

func toneMapReinhardCPU(c math3d.Vec3) math3d.Vec3 {
	reinhard := func(x float32) float32 {
		if !(x > 0) {
			return 0
		}
		if isInf32(x) {
			return 1
		}
		return x / (1 + x)
	}
	return math3d.Vec3{
		X: reinhard(c.X),
		Y: reinhard(c.Y),
		Z: reinhard(c.Z),
	}
}

func toneMapACESCPU(c math3d.Vec3) math3d.Vec3 {
	aces := func(x float32) float32 {
		if !(x > 0) {
			return 0
		}
		if isInf32(x) {
			return 1
		}
		const a, b, k, d, e = float64(2.51), float64(0.03), float64(2.43), float64(0.59), float64(0.14)
		xf := float64(x)
		y := (xf * (a*xf + b)) / (xf*(k*xf+d) + e)
		return clamp01(float32(y))
	}
	return math3d.Vec3{
		X: aces(c.X),
		Y: aces(c.Y),
		Z: aces(c.Z),
	}
}

func toneMapNeutralCPU(c math3d.Vec3) math3d.Vec3 {
	const (
		startCompression = float64(0.76) // 0.8 - 0.04
		desaturation     = float64(0.15)
	)
	if isInf32(c.X) || isInf32(c.Y) || isInf32(c.Z) {
		return math3d.Vec3{X: 1, Y: 1, Z: 1}
	}
	r, g, b := float64(c.X), float64(c.Y), float64(c.Z)
	x := min(r, min(g, b))
	offset := 0.04
	if x < 0.08 {
		offset = x - 6.25*x*x
	}
	r -= offset
	g -= offset
	b -= offset

	peak := max(r, max(g, b))
	if peak < startCompression {
		return math3d.Vec3{
			X: clamp01(float32(r)),
			Y: clamp01(float32(g)),
			Z: clamp01(float32(b)),
		}
	}
	d := 1.0 - startCompression
	newPeak := 1.0 - (d*d)/(peak+d-startCompression)
	scale := newPeak / peak
	r *= scale
	g *= scale
	b *= scale

	desat := 1.0 - 1.0/(desaturation*(peak-newPeak)+1.0)
	lerp := func(a, target, t float64) float32 {
		return clamp01(float32(a*(1-t) + target*t))
	}
	return math3d.Vec3{
		X: lerp(r, newPeak, desat),
		Y: lerp(g, newPeak, desat),
		Z: lerp(b, newPeak, desat),
	}
}

func toneMapAgXCPU(c math3d.Vec3) math3d.Vec3 {
	const (
		agxMinEV  = -12.47393
		agxMaxEV  = 4.026069
		agxRange  = 16.5 // agxMaxEV - agxMinEV
		agxMinLin = 0.0001757761
		agxMaxLin = 16.291573
	)
	// Inputs arrive from sanitizeHDRChannel, finite in [0, maxHDRChannel]; the inset matrix mixes channels before the log encoding clamps, as tonemap_agx does on the GPU.
	clampLin := func(v float32) float64 {
		if !(v > 0) {
			return 0
		}
		return float64(min(v, maxHDRChannel))
	}
	r, g, b := clampLin(c.X), clampLin(c.Y), clampLin(c.Z)
	// AgX inset matrix (linear Rec.709/sRGB -> AgX working space).
	ir := 0.84247906*r + 0.07843360*g + 0.07922375*b
	ig := 0.04232824*r + 0.87846864*g + 0.07916613*b
	ib := 0.04237565*r + 0.07843360*g + 0.87914297*b

	logEncode := func(v float64) float64 {
		if v < agxMinLin {
			v = agxMinLin
		} else if v > agxMaxLin {
			v = agxMaxLin
		}
		t := (math.Log2(v) - agxMinEV) / agxRange
		if t < 0 {
			return 0
		}
		if t > 1 {
			return 1
		}
		return t
	}
	agxPoly := func(x float64) float64 {
		x2 := x * x
		x4 := x2 * x2
		return 15.5*x4*x2 - 40.14*x4*x + 31.96*x4 - 6.868*x2*x + 0.4298*x2 + 0.1191*x - 0.00232
	}

	cr := agxPoly(logEncode(ir))
	cg := agxPoly(logEncode(ig))
	cb := agxPoly(logEncode(ib))

	// AgX outset matrix (AgX display -> Rec.709/sRGB display).
	or := 1.19687901*cr - 0.09802088*cg - 0.09902974*cb
	og := -0.05289685*cr + 1.15190313*cg - 0.09896118*cb
	ob := -0.05297164*cr - 0.09804345*cg + 1.15107367*cb

	// Convert AgX's ~2.2 display-encoded curve back to linear so the pass's configured display OETF (sRGB, custom gamma, or hardware sRGB target) applies uniformly across all operators.
	return math3d.Vec3{
		X: safePow01(float32(or), 2.2),
		Y: safePow01(float32(og), 2.2),
		Z: safePow01(float32(ob), 2.2),
	}
}

// toneMapRegion is the part of the source texture [ToneMapPass] stretches over its target: the fullscreen triangle's uv in [0, 1] is multiplied by scale and clamped to [min, max], as the dynamic resolution upscaler samples its active sub-viewport.
type toneMapRegion struct {
	scaleX, scaleY, minX, minY, maxX, maxY float32
}

// fullToneMapRegion samples the whole source texture.
var fullToneMapRegion = toneMapRegion{scaleX: 1, scaleY: 1, maxX: 1, maxY: 1}

// AppendToneMapUniforms appends the [ToneMapUniformSize] little-endian bytes matching [ToneMapWGSL]'s uniform struct, sampling the whole source texture, to dst and returns the extended slice. It allocates nothing when cap(dst)-len(dst) >= [ToneMapUniformSize].
func AppendToneMapUniforms(dst []byte, opts ToneMapOptions) []byte {
	return appendToneMapUniforms(dst, opts, fullToneMapRegion)
}

func appendToneMapUniforms(dst []byte, opts ToneMapOptions, region toneMapRegion) []byte {
	n := opts.Normalized()
	start := len(dst)
	if cap(dst)-start < ToneMapUniformSize {
		next := make([]byte, start, start+ToneMapUniformSize)
		copy(next, dst)
		dst = next
	}
	dst = dst[:start+ToneMapUniformSize]
	out := dst[start:]

	var invGamma float32
	switch {
	case n.LinearOutput:
		invGamma = -1.0
	case n.Gamma > 0:
		invGamma = 1.0 / n.Gamma
	default:
		invGamma = 0.0
	}

	binary.LittleEndian.PutUint32(out[0:], math.Float32bits(n.Exposure))
	binary.LittleEndian.PutUint32(out[4:], math.Float32bits(invGamma))
	binary.LittleEndian.PutUint32(out[8:], math.Float32bits(float32(n.Operator)))
	binary.LittleEndian.PutUint32(out[12:], 0)
	for i, v := range [...]float32{region.scaleX, region.scaleY, region.minX, region.minY, region.maxX, region.maxY, 0, 0} {
		binary.LittleEndian.PutUint32(out[16+4*i:], math.Float32bits(v))
	}
	return dst
}

// ToneMapWGSL is the full-screen triangle WGSL shader used by [ToneMapPass].
//
// It binds the HDR source texture at @group(0) @binding(0), its sampler at @group(0) @binding(1), and the [ToneMapUniformSize]-byte uniform block written by [AppendToneMapUniforms] at @group(0) @binding(2).
//
// Entry points: vs_main (vertexless full-screen triangle via @builtin(vertex_index), drawn with pass.Draw(3, 0)) and fs_main (exposure, operator dispatch across None/Reinhard/ACES/Neutral/AgX, and exact sRGB or power-law gamma encoding).
//
// All math uses only operations supported across WebGPU, Metal, D3D11, OpenGL 3.3, WebGL 2, OpenGL ES 3.0, and NSGo's software GPU VM (no exp or log2 builtins).
const ToneMapWGSL = `struct ToneMapParams {
	exposure: f32,
	inv_gamma: f32,
	operator_id: f32,
	_pad: f32,
	uv_scale: vec2<f32>,
	uv_min: vec2<f32>,
	uv_max: vec2<f32>,
	_pad2: vec2<f32>,
}

@group(0) @binding(0) var hdr_tex: texture_2d<f32>;
@group(0) @binding(1) var hdr_samp: sampler;
@group(0) @binding(2) var<uniform> params: ToneMapParams;

struct VertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32) -> VertexOut {
	var out: VertexOut;
	var x: f32 = -1.0;
	var y: f32 = -1.0;
	if (vid == 1u) {
		x = 3.0;
	} else if (vid == 2u) {
		y = 3.0;
	}
	out.pos = vec4<f32>(x, y, 0.0, 1.0);
	out.uv = vec2<f32>((x + 1.0) * 0.5, (1.0 - y) * 0.5);
	return out;
}

fn safe_pow_scalar(x: f32, p: f32) -> f32 {
	if (x <= 0.0) {
		return 0.0;
	}
	return pow(x, p);
}

fn linear_to_srgb_scalar(x: f32) -> f32 {
	let c = clamp(x, 0.0, 1.0);
	if (c <= 0.0031308) {
		return c * 12.92;
	}
	return 1.055 * pow(c, 0.41666667) - 0.055;
}

fn tonemap_reinhard(c: vec3<f32>) -> vec3<f32> {
	return c / (vec3<f32>(1.0, 1.0, 1.0) + c);
}

fn tonemap_aces(c: vec3<f32>) -> vec3<f32> {
	let num = c * (2.51 * c + vec3<f32>(0.03, 0.03, 0.03));
	let den = c * (2.43 * c + vec3<f32>(0.59, 0.59, 0.59)) + vec3<f32>(0.14, 0.14, 0.14);
	return clamp(num / den, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
}

fn tonemap_neutral(color: vec3<f32>) -> vec3<f32> {
	let start_compression = 0.76;
	let desaturation = 0.15;
	let x = min(color.x, min(color.y, color.z));
	var offset: f32 = 0.04;
	if (x < 0.08) {
		offset = x - 6.25 * x * x;
	}
	var c = color - vec3<f32>(offset, offset, offset);
	let peak = max(c.x, max(c.y, c.z));
	if (peak < start_compression) {
		return clamp(c, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	}
	let d = 1.0 - start_compression;
	let new_peak = 1.0 - (d * d) / (peak + d - start_compression);
	c = c * (new_peak / peak);
	let g = 1.0 - 1.0 / (desaturation * (peak - new_peak) + 1.0);
	return clamp(mix(c, vec3<f32>(new_peak, new_peak, new_peak), vec3<f32>(g, g, g)), vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
}

// fast_log2_agx computes log2 over AgX's input range with power-of-two range reduction and an atanh series, because NSGo v0.3.0's software GPU has no case for the log2, exp2, log and exp builtins and silently returns 0 for them.
fn fast_log2_agx(v: f32) -> f32 {
	var m = clamp(v, 0.0001757761, 16.291573);
	var e: f32 = 0.0;
	if (m >= 16.0) {
		m = m * 0.0625;
		e = e + 4.0;
	}
	if (m >= 4.0) {
		m = m * 0.25;
		e = e + 2.0;
	}
	if (m >= 2.0) {
		m = m * 0.5;
		e = e + 1.0;
	}
	if (m < 0.00390625) {
		m = m * 256.0;
		e = e - 8.0;
	}
	if (m < 0.0625) {
		m = m * 16.0;
		e = e - 4.0;
	}
	if (m < 0.25) {
		m = m * 4.0;
		e = e - 2.0;
	}
	if (m < 0.5) {
		m = m * 2.0;
		e = e - 1.0;
	}
	if (m < 1.0) {
		m = m * 2.0;
		e = e - 1.0;
	}
	if (m >= 1.41421356) {
		m = m * 0.5;
		e = e + 1.0;
	}
	let t = (m - 1.0) / (m + 1.0);
	let t2 = t * t;
	let poly = 1.0 + t2 * (0.33333334 + t2 * (0.2 + t2 * 0.14285715));
	return e + 2.88539008 * t * poly;
}

fn agx_curve(x: vec3<f32>) -> vec3<f32> {
	let x2 = x * x;
	let x4 = x2 * x2;
	return 15.5 * x4 * x2
		- 40.14 * x4 * x
		+ 31.96 * x4
		- 6.868 * x2 * x
		+ 0.4298 * x2
		+ 0.1191 * x
		- vec3<f32>(0.00232, 0.00232, 0.00232);
}

fn tonemap_agx(c: vec3<f32>) -> vec3<f32> {
	let inset = vec3<f32>(
		0.84247906 * c.x + 0.07843360 * c.y + 0.07922375 * c.z,
		0.04232824 * c.x + 0.87846864 * c.y + 0.07916613 * c.z,
		0.04237565 * c.x + 0.07843360 * c.y + 0.87914297 * c.z,
	);
	let min_ev = -12.47393;
	let inv_range = 0.06060606;
	let lx = clamp((fast_log2_agx(inset.x) - min_ev) * inv_range, 0.0, 1.0);
	let ly = clamp((fast_log2_agx(inset.y) - min_ev) * inv_range, 0.0, 1.0);
	let lz = clamp((fast_log2_agx(inset.z) - min_ev) * inv_range, 0.0, 1.0);
	let curved = agx_curve(vec3<f32>(lx, ly, lz));
	let outset = vec3<f32>(
		1.19687901 * curved.x - 0.09802088 * curved.y - 0.09902974 * curved.z,
		-0.05289685 * curved.x + 1.15190313 * curved.y - 0.09896118 * curved.z,
		-0.05297164 * curved.x - 0.09804345 * curved.y + 1.15107367 * curved.z,
	);
	let clamped = clamp(outset, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	return vec3<f32>(
		safe_pow_scalar(clamped.x, 2.2),
		safe_pow_scalar(clamped.y, 2.2),
		safe_pow_scalar(clamped.z, 2.2),
	);
}

// sanitize_hdr maps NaN and non-positive values to 0 and clamps the rest to the largest finite half float, so overflowed RGBA16Float highlights (+Inf) and extreme exposures never reach the operators as Inf or NaN. It tests NaN with a strict comparison, which is false for NaN under IEEE 754, instead of relying on how max and min treat NaN; NSGo v0.3.0's software VM also treats NaN as equal to every value, so >= and <= would let it through there.
fn sanitize_hdr(x: f32) -> f32 {
	if (x > 0.0) {
		return min(x, 65504.0);
	}
	return 0.0;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let src = textureSample(hdr_tex, hdr_samp, clamp(in.uv * params.uv_scale, params.uv_min, params.uv_max));
	let raw = src.xyz * params.exposure;
	let exposed = vec3<f32>(sanitize_hdr(raw.x), sanitize_hdr(raw.y), sanitize_hdr(raw.z));
	var mapped: vec3<f32>;
	if (params.operator_id < 0.5) {
		mapped = clamp(exposed, vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
	} else if (params.operator_id < 1.5) {
		mapped = tonemap_reinhard(exposed);
	} else if (params.operator_id < 2.5) {
		mapped = tonemap_aces(exposed);
	} else if (params.operator_id < 3.5) {
		mapped = tonemap_neutral(exposed);
	} else {
		mapped = tonemap_agx(exposed);
	}
	if (params.inv_gamma < 0.0) {
		return vec4<f32>(mapped, src.w);
	}
	var display: vec3<f32>;
	if (params.inv_gamma > 0.0) {
		display = vec3<f32>(
			safe_pow_scalar(clamp(mapped.x, 0.0, 1.0), params.inv_gamma),
			safe_pow_scalar(clamp(mapped.y, 0.0, 1.0), params.inv_gamma),
			safe_pow_scalar(clamp(mapped.z, 0.0, 1.0), params.inv_gamma),
		);
	} else {
		display = vec3<f32>(
			linear_to_srgb_scalar(mapped.x),
			linear_to_srgb_scalar(mapped.y),
			linear_to_srgb_scalar(mapped.z),
		);
	}
	return vec4<f32>(display, src.w);
}
`

var compileToneMapShader = sync.OnceValues(func() (*gpu.ShaderModule, error) {
	return gpu.CompileWGSL(ToneMapWGSL)
})

// SupportsHDR reports whether dev supports rendering to and sampling from [gpu.FormatRGBA16Float] textures.
func SupportsHDR(dev *gpu.Device) bool {
	return dev != nil && dev.SupportsFormat(gpu.FormatRGBA16Float)
}

// PreferredHDRFormat returns [gpu.FormatRGBA16Float] when dev supports it, and falls back to [gpu.FormatRGBA8Unorm] otherwise.
func PreferredHDRFormat(dev *gpu.Device) gpu.TextureFormat {
	if SupportsHDR(dev) {
		return gpu.FormatRGBA16Float
	}
	return gpu.FormatRGBA8Unorm
}

// HDRTargetDesc configures [NewHDRTarget].
type HDRTargetDesc struct {
	// Label names the offscreen textures in GPU capture tools.
	Label string
	// Width and Height are the target dimensions in physical pixels. Both must be positive.
	Width  uint32
	Height uint32
	// DepthFormat optionally creates a matching depth/stencil attachment (for example [gpu.FormatDepth24Plus]). Leave [gpu.FormatUndefined] for a color-only HDR target.
	DepthFormat gpu.TextureFormat
	// SampleCount enables MSAA when > 1 (for example 4). If the device does not report [gpu.Features].MSAA4x, it falls back to 1.
	SampleCount int
	// DisableFallback prevents falling back to [gpu.FormatRGBA8Unorm] when [gpu.FormatRGBA16Float] is unsupported, returning [gpu.ErrUnsupported].
	DisableFallback bool
	// Retained opts into automatic recreation after a GPU device loss (see [gpu.RenderTargetDesc].Retained).
	Retained bool
}

// HDRTarget owns an offscreen linear floating-point [gpu.RenderTarget] ([gpu.FormatRGBA16Float] with automatic fallback to [gpu.FormatRGBA8Unorm] on devices without half-float support) for rendering 3D scenes before tone mapping (#250).
type HDRTarget struct {
	cx          *core.Scope
	dev         *gpu.Device
	rt          *gpu.RenderTarget
	format      gpu.TextureFormat
	depthFormat gpu.TextureFormat
	sampleCount int
	desc        HDRTargetDesc
}

// NewHDRTarget allocates an [HDRTarget] on dev scoped to cx (nil for resources the caller closes manually, as in headless tests).
//
// It prefers [gpu.FormatRGBA16Float] when [SupportsHDR](dev) is true and gracefully falls back to [gpu.FormatRGBA8Unorm] otherwise unless desc.DisableFallback is set.
func NewHDRTarget(cx *core.Scope, dev *gpu.Device, desc HDRTargetDesc) (*HDRTarget, error) {
	if desc.Width == 0 || desc.Height == 0 {
		return nil, fmt.Errorf("%w: non-positive size %dx%d", ErrInvalidHDRTarget, desc.Width, desc.Height)
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	t := &HDRTarget{cx: cx, dev: dev, desc: desc}
	if err := t.allocate(dev, desc.Width, desc.Height); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *HDRTarget) allocate(dev *gpu.Device, width, height uint32) error {
	samples := t.desc.SampleCount
	if samples <= 1 || !dev.Features().MSAA4x {
		samples = 1
	}
	label := t.desc.Label
	if label == "" {
		label = "render3d.hdr"
	}

	colorFormat, err := hdrColorFormat(SupportsHDR(dev), t.desc.DisableFallback)
	if err != nil {
		return err
	}

	rt, err := dev.CreateRenderTarget(t.cx, gpu.RenderTargetDesc{
		Label:       label,
		Width:       width,
		Height:      height,
		ColorFormat: colorFormat,
		DepthFormat: t.desc.DepthFormat,
		SampleCount: samples,
		Retained:    t.desc.Retained,
	})
	if retry, ok := hdrFallbackFormat(colorFormat, t.desc.DisableFallback, err); ok {
		colorFormat = retry
		rt, err = dev.CreateRenderTarget(t.cx, gpu.RenderTargetDesc{
			Label:       label,
			Width:       width,
			Height:      height,
			ColorFormat: colorFormat,
			DepthFormat: t.desc.DepthFormat,
			SampleCount: samples,
			Retained:    t.desc.Retained,
		})
	}
	if err != nil {
		return err
	}
	t.dev = dev
	t.rt = rt
	t.format = colorFormat
	t.depthFormat = t.desc.DepthFormat
	t.sampleCount = samples
	t.desc.Width = width
	t.desc.Height = height
	return nil
}

// hdrColorFormat picks [NewHDRTarget]'s color format: [gpu.FormatRGBA16Float] when the device supports it, otherwise [gpu.FormatRGBA8Unorm], or [gpu.ErrUnsupported] when the fallback is disabled.
func hdrColorFormat(supportsHDR, disableFallback bool) (gpu.TextureFormat, error) {
	if supportsHDR {
		return gpu.FormatRGBA16Float, nil
	}
	if disableFallback {
		return gpu.FormatUndefined, gpu.Error{Code: gpu.ErrUnsupported, Message: "render3d: FormatRGBA16Float is unsupported on this device"}
	}
	return gpu.FormatRGBA8Unorm, nil
}

// hdrFallbackFormat reports whether creating a render target in format failed with err only because the device cannot render to RGBA16Float, and the format to retry with.
func hdrFallbackFormat(format gpu.TextureFormat, disableFallback bool, err error) (gpu.TextureFormat, bool) {
	if err == nil || format != gpu.FormatRGBA16Float || disableFallback || !errors.Is(err, gpu.ErrUnsupported) {
		return format, false
	}
	return gpu.FormatRGBA8Unorm, true
}

// RenderTarget returns the underlying [*gpu.RenderTarget].
func (t *HDRTarget) RenderTarget() *gpu.RenderTarget {
	if t == nil {
		return nil
	}
	return t.rt
}

// ColorTexture returns the resolved texture suitable for sampling in [ToneMapPass]: the MSAA resolve texture when multisampled, otherwise the color attachment texture.
func (t *HDRTarget) ColorTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	if t.rt.Resolve != nil {
		return t.rt.Resolve
	}
	return t.rt.Color
}

// ColorAttachmentTexture returns the color texture rendered into by the 3D scene pass (multisampled when SampleCount > 1).
func (t *HDRTarget) ColorAttachmentTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Color
}

// DepthTexture returns the optional depth/stencil texture, or nil when created without a DepthFormat.
func (t *HDRTarget) DepthTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Depth
}

// Format returns the actual color format allocated ([gpu.FormatRGBA16Float] on HDR-capable devices, or [gpu.FormatRGBA8Unorm] when fallen back).
func (t *HDRTarget) Format() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.format
}

// DepthFormat returns the depth/stencil format, or [gpu.FormatUndefined] if none.
func (t *HDRTarget) DepthFormat() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.depthFormat
}

// SampleCount returns the effective MSAA sample count (1 or 4).
func (t *HDRTarget) SampleCount() int {
	if t == nil {
		return 0
	}
	return t.sampleCount
}

// Width returns the target width in physical pixels.
func (t *HDRTarget) Width() uint32 {
	if t == nil || t.rt == nil {
		return 0
	}
	return t.rt.Width
}

// Height returns the target height in physical pixels.
func (t *HDRTarget) Height() uint32 {
	if t == nil || t.rt == nil {
		return 0
	}
	return t.rt.Height
}

// IsHDR reports whether the target uses a floating-point color format capable of storing linear values above 1.0 without clamping.
func (t *HDRTarget) IsHDR() bool {
	return t != nil && t.format.IsFloat()
}

// Valid reports whether the target's GPU textures are open and belong to a live device.
func (t *HDRTarget) Valid() bool {
	return t != nil && t.rt != nil && t.rt.Valid()
}

// ByteSize returns the estimated GPU memory in bytes held by the target's color, resolve, and depth attachments, for [RenderStats] and [Budget] accounting.
func (t *HDRTarget) ByteSize() uint64 {
	if t == nil || t.rt == nil {
		return 0
	}
	w, h := t.rt.Width, t.rt.Height
	samples := uint64(max(t.sampleCount, 1))
	total := t.format.ByteSize(w, h) * samples
	if t.rt.Resolve != nil {
		total += t.format.ByteSize(w, h)
	}
	if t.rt.Depth != nil && t.depthFormat != gpu.FormatUndefined {
		total += t.depthFormat.ByteSize(w, h) * samples
	}
	return total
}

// ColorAttachment returns a [gpu.ColorAttachment] configured to clear and draw into t (automatically wiring ResolveTarget when multisampled).
func (t *HDRTarget) ColorAttachment(clearColor core.Color) gpu.ColorAttachment {
	if t == nil || t.rt == nil {
		return gpu.ColorAttachment{}
	}
	return gpu.ColorAttachment{
		Texture:       t.rt.Color,
		ResolveTarget: t.rt.Resolve,
		LoadOp:        gpu.LoadClear,
		StoreOp:       gpu.StoreStore,
		ClearColor:    clearColor,
	}
}

// RenderPassDesc returns a [gpu.RenderPassDesc] targeting t's color attachment and, when present, its depth attachment.
func (t *HDRTarget) RenderPassDesc(clearColor core.Color, clearDepth float32) gpu.RenderPassDesc {
	if t == nil || t.rt == nil {
		return gpu.RenderPassDesc{}
	}
	desc := gpu.RenderPassDesc{
		Label:            t.desc.Label,
		ColorAttachments: []gpu.ColorAttachment{t.ColorAttachment(clearColor)},
	}
	if t.rt.Depth != nil {
		desc.DepthStencil = &gpu.DepthStencilAttachment{
			Texture:      t.rt.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   clearDepth,
		}
	}
	return desc
}

// Resize recreates t's attachments at width x height when the dimensions differ or the existing target is no longer valid. Pass dev (for example frame.Device()) after a device restore, or nil to reuse the original device.
func (t *HDRTarget) Resize(dev *gpu.Device, width, height uint32) error {
	if t == nil {
		return fmt.Errorf("%w: nil HDRTarget", ErrInvalidHDRTarget)
	}
	if width == 0 || height == 0 {
		return fmt.Errorf("%w: non-positive size %dx%d", ErrInvalidHDRTarget, width, height)
	}
	if dev == nil {
		dev = t.dev
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if t.Valid() && t.rt.Width == width && t.rt.Height == height && t.dev == dev {
		return nil
	}
	oldRT := t.rt
	if err := t.allocate(dev, width, height); err != nil {
		return err
	}
	if oldRT != nil {
		_ = oldRT.Close()
	}
	return nil
}

// Close releases the target's GPU attachments. It is safe to call on nil or more than once.
func (t *HDRTarget) Close() error {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Close()
}

// ToneMapPassDesc configures [NewToneMapPass].
type ToneMapPassDesc struct {
	// Label names the pass's pipeline, sampler, uniform buffer, and bind group in GPU capture tools.
	Label string
	// TargetFormat is the color format of the render pass that [ToneMapPass] draws into. [gpu.FormatUndefined] defaults to dev.SurfaceFormat(). When TargetFormat.IsSRGB() is true, [ToneMapPass] automatically outputs linear tone-mapped RGB so the hardware sRGB target encodes linear-to-sRGB once without double gamma.
	TargetFormat gpu.TextureFormat
	// DepthFormat optionally matches a render pass with a depth attachment; depth testing and depth writes stay disabled.
	DepthFormat gpu.TextureFormat
	// NearestFilter selects [gpu.FilterNearest] instead of [gpu.FilterLinear] when sampling the HDR source texture.
	NearestFilter bool
	// Retained opts the uniform buffer, sampler, bind group layout and pipeline into automatic recreation after a GPU device loss. Bind groups have no retained form; the pass rebuilds its cached one on the next draw once the old one is no longer valid.
	Retained bool
}

// ToneMapPass draws a full-screen triangle that samples a linear HDR texture, applies exposure and a configurable [ToneMapOperator], and encodes to sRGB or power-law gamma for display (#250).
//
// A pass holds one uniform block, and NSGo applies [gpu.Device.WriteBuffer] ahead of the commands of the submission it belongs to, so every draw one ToneMapPass records into a submission uses the options of its last [ToneMapPass.Draw]. To tone-map several views with different options in one frame (split screen, a picture-in-picture camera, a photo-mode preview), create one ToneMapPass per view.
//
// [AppendToneMapUniforms] encodes the options without allocating, and the pass reuses its bind group while the source texture stays the same; each draw still uploads the [ToneMapUniformSize]-byte block with WriteBuffer, which copies it. When created with Retained: true (along with a retained [HDRTarget]), the pass's GPU resources survive a device loss as [ToneMapPassDesc].Retained describes.
type ToneMapPass struct {
	cx           *core.Scope
	dev          *gpu.Device
	targetFormat gpu.TextureFormat
	label        string
	retained     bool

	uniformBuf *gpu.Buffer
	sampler    *gpu.Sampler
	layout     *gpu.BindGroupLayout
	pipeline   *gpu.Pipeline

	boundTex   *gpu.Texture
	boundGroup *gpu.BindGroup
	scratch    [ToneMapUniformSize]byte
}

// NewToneMapPass compiles [ToneMapWGSL] (cached process-wide) and creates the uniform buffer, sampler, bind group layout, and render pipeline on dev scoped to cx (nil for manual closing in headless tests).
func NewToneMapPass(cx *core.Scope, dev *gpu.Device, desc ToneMapPassDesc) (*ToneMapPass, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	mod, err := compileToneMapShader()
	if err != nil {
		return nil, fmt.Errorf("render3d: compile tonemap shader: %w", err)
	}

	targetFmt := desc.TargetFormat
	if targetFmt == gpu.FormatUndefined {
		targetFmt = dev.SurfaceFormat()
	}
	if targetFmt == gpu.FormatUndefined {
		targetFmt = gpu.FormatRGBA8Unorm
	}

	label := desc.Label
	if label == "" {
		label = "render3d.tonemap"
	}

	initUniforms := AppendToneMapUniforms(make([]byte, 0, ToneMapUniformSize), DefaultToneMapOptions())
	uniformBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " uniforms",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     initUniforms,
		Retained: desc.Retained,
	})
	if err != nil {
		return nil, err
	}

	filter := gpu.FilterLinear
	if desc.NearestFilter {
		filter = gpu.FilterNearest
	}
	sampler, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + " sampler",
		MinFilter: filter,
		MagFilter: filter,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		Retained:  desc.Retained,
	})
	if err != nil {
		_ = uniformBuf.Close()
		return nil, err
	}

	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
		},
	})
	if err != nil {
		_ = sampler.Close()
		_ = uniformBuf.Close()
		return nil, err
	}

	pipeDesc := gpu.PipelineDesc{
		Label:    label + " pipeline",
		Vertex:   gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment: gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:  []*gpu.BindGroupLayout{ToneMapGroup: layout},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  gpu.CullNone,
		},
		Targets:  []gpu.ColorTargetState{{Format: targetFmt, WriteMask: gpu.ColorWriteAll}},
		Retained: desc.Retained,
	}
	if desc.DepthFormat != gpu.FormatUndefined {
		pipeDesc.DepthStencil = &gpu.DepthStencilState{
			Format:       desc.DepthFormat,
			DepthWrite:   false,
			DepthCompare: gpu.CompareAlways,
		}
	}
	pipeline, err := dev.CreatePipeline(cx, pipeDesc)
	if err != nil {
		_ = layout.Close()
		_ = sampler.Close()
		_ = uniformBuf.Close()
		return nil, err
	}

	return &ToneMapPass{
		cx:           cx,
		dev:          dev,
		targetFormat: targetFmt,
		label:        label,
		retained:     desc.Retained,
		uniformBuf:   uniformBuf,
		sampler:      sampler,
		layout:       layout,
		pipeline:     pipeline,
	}, nil
}

// Pipeline returns the pass's tone-mapping render pipeline.
func (p *ToneMapPass) Pipeline() *gpu.Pipeline {
	if p == nil {
		return nil
	}
	return p.pipeline
}

// Layout returns the bind group layout at [ToneMapGroup].
func (p *ToneMapPass) Layout() *gpu.BindGroupLayout {
	if p == nil {
		return nil
	}
	return p.layout
}

// Sampler returns the sampler bound for reading the HDR texture.
func (p *ToneMapPass) Sampler() *gpu.Sampler {
	if p == nil {
		return nil
	}
	return p.sampler
}

// UniformBuffer returns the [ToneMapUniformSize]-byte uniform buffer.
func (p *ToneMapPass) UniformBuffer() *gpu.Buffer {
	if p == nil {
		return nil
	}
	return p.uniformBuf
}

// TargetFormat returns the color format the pass renders into.
func (p *ToneMapPass) TargetFormat() gpu.TextureFormat {
	if p == nil {
		return gpu.FormatUndefined
	}
	return p.targetFormat
}

// Valid reports whether the pass's GPU resources are open and live.
func (p *ToneMapPass) Valid() bool {
	return p != nil && p.pipeline.Valid() && p.uniformBuf.Valid() && p.sampler.Valid() && p.layout.Valid()
}

// SetDevice updates the device reference used when a nil device is passed to [ToneMapPass.Write], [ToneMapPass.BindGroupFor], or [ToneMapPass.Draw] (for example from OnReady after a retained device restoration).
func (p *ToneMapPass) SetDevice(dev *gpu.Device) {
	if p != nil && dev != nil {
		p.dev = dev
	}
}

func (p *ToneMapPass) effectiveOptions(opts ToneMapOptions) ToneMapOptions {
	n := opts.Normalized()
	if p.targetFormat.IsSRGB() {
		// Hardware sRGB targets apply linear-to-sRGB automatically on write; emit linear tone-mapped values so gamma is never applied twice.
		n.LinearOutput = true
	}
	return n
}

// Write uploads opts to the pass's uniform buffer without allocating into the caller's heap (encodes into internal scratch storage). Pass dev (such as frame.Device()) after a device restore, or nil to use the pass's stored device.
func (p *ToneMapPass) Write(dev *gpu.Device, opts ToneMapOptions) {
	if !p.Valid() {
		return
	}
	if dev != nil {
		p.dev = dev
	}
	if p.dev == nil {
		return
	}
	p.write(opts, fullToneMapRegion)
}

func (p *ToneMapPass) write(opts ToneMapOptions, region toneMapRegion) {
	data := appendToneMapUniforms(p.scratch[:0], p.effectiveOptions(opts), region)
	p.dev.WriteBuffer(p.uniformBuf, 0, data)
}

// BindGroupFor returns a bind group pairing src with the pass's sampler and uniform buffer, caching it for steady-state reuse when src has not changed.
func (p *ToneMapPass) BindGroupFor(dev *gpu.Device, src *gpu.Texture) (*gpu.BindGroup, error) {
	if !p.Valid() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: tone map pass is not valid"}
	}
	if src == nil || !src.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid source texture"}
	}
	if dev != nil {
		p.dev = dev
	}
	if p.dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if p.boundTex == src && p.boundGroup != nil && p.boundGroup.Valid() {
		return p.boundGroup, nil
	}
	bg, err := p.dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
		Label:  p.label + " bind group",
		Layout: p.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Texture: src},
			{Binding: 1, Sampler: p.sampler},
			{Binding: 2, Buffer: p.uniformBuf, Size: ToneMapUniformSize},
		},
	})
	if err != nil {
		return nil, err
	}
	if p.boundGroup != nil {
		_ = p.boundGroup.Close()
	}
	p.boundTex = src
	p.boundGroup = bg
	return bg, nil
}

// Draw uploads opts, binds src (reusing the cached bind group in steady state), and records the full-screen tone-mapping triangle into pass. The options apply to every draw of this pass in the same submission (see [ToneMapPass]).
func (p *ToneMapPass) Draw(pass *gpu.RenderPass, dev *gpu.Device, src *gpu.Texture, opts ToneMapOptions) error {
	return p.drawRegion(pass, dev, src, opts, fullToneMapRegion)
}

func (p *ToneMapPass) drawRegion(pass *gpu.RenderPass, dev *gpu.Device, src *gpu.Texture, opts ToneMapOptions, region toneMapRegion) error {
	if pass == nil {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: nil render pass"}
	}
	bg, err := p.BindGroupFor(dev, src)
	if err != nil {
		return err
	}
	p.write(opts, region)
	pass.SetPipeline(p.pipeline)
	pass.SetBindGroup(ToneMapGroup, bg)
	pass.Draw(3, 0)
	return nil
}

// DrawTarget is [ToneMapPass.Draw] for an [*HDRTarget], sampling src.ColorTexture().
func (p *ToneMapPass) DrawTarget(pass *gpu.RenderPass, dev *gpu.Device, src *HDRTarget, opts ToneMapOptions) error {
	if src == nil {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: nil HDR target"}
	}
	return p.Draw(pass, dev, src.ColorTexture(), opts)
}

// DrawDynamicResolution tone-maps the active scaled sub-viewport of a [DynamicResolutionTarget] (created with an HDR ColorFormat such as [PreferredHDRFormat]) over the whole of pass, upscaling it with the pass's sampler in the same draw. Call it after [DynamicResolutionTarget.Begin3D]'s pass ends, in place of [DynamicResolutionTarget.Upscale]; the upscaler's sharpen filter is not applied. Like the upscaler, it clamps the sampled coordinates half a texel inside the scaled region so the target's unused texels never bleed in.
func (p *ToneMapPass) DrawDynamicResolution(pass *gpu.RenderPass, dev *gpu.Device, src *DynamicResolutionTarget, opts ToneMapOptions) error {
	if src == nil {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: nil dynamic resolution target"}
	}
	allocW, allocH := src.AllocatedSize()
	scaledW, scaledH := src.ScaledSize()
	if allocW <= 0 || allocH <= 0 || scaledW <= 0 || scaledH <= 0 {
		return fmt.Errorf("%w: Begin3D must be called before DrawDynamicResolution", ErrInvalidDynamicResolution)
	}
	scaledW, scaledH = min(scaledW, allocW), min(scaledH, allocH)
	invW, invH := 1/float32(allocW), 1/float32(allocH)
	region := toneMapRegion{
		scaleX: float32(scaledW) * invW,
		scaleY: float32(scaledH) * invH,
		minX:   0.5 * invW,
		minY:   0.5 * invH,
		maxX:   (float32(scaledW) - 0.5) * invW,
		maxY:   (float32(scaledH) - 0.5) * invH,
	}
	return p.drawRegion(pass, dev, src.ColorTexture(), opts, region)
}

// Close releases the pass's GPU resources. It is safe to call on nil or more than once.
func (p *ToneMapPass) Close() error {
	if p == nil {
		return nil
	}
	var err error
	if p.boundGroup != nil {
		err = errors.Join(err, p.boundGroup.Close())
		p.boundGroup = nil
		p.boundTex = nil
	}
	if p.pipeline != nil {
		err = errors.Join(err, p.pipeline.Close())
	}
	if p.layout != nil {
		err = errors.Join(err, p.layout.Close())
	}
	if p.sampler != nil {
		err = errors.Join(err, p.sampler.Close())
	}
	if p.uniformBuf != nil {
		err = errors.Join(err, p.uniformBuf.Close())
	}
	return err
}
