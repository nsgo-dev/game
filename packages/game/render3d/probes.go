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

// ErrInvalidProbe is wrapped by errors returned when reflection or light probe geometry, grid dimensions, bake options, or probe IBL uniforms are malformed.
var ErrInvalidProbe = errors.New("render3d: invalid probe")

const (
	// ProbeCubeFaceCount is the six canonical faces of a cubemap (+X, -X, +Y, -Y, +Z, -Z).
	ProbeCubeFaceCount = 6

	// ProbeIBLUniformSize is the byte size of the ProbeIBLUniform block written by [ProbeIBLUniform.Encode] and declared by [ProbeIBLWGSL]: nine vec4<f32> L2 spherical harmonic irradiance coefficients followed by six probe geometry/weight vec4<f32>s and one global parameter vec4<f32> (256 bytes).
	ProbeIBLUniformSize = 9*16 + 7*16
)

// probeIBLWGSLStruct declares the std140/WGSL uniform block layout [ProbeIBLUniform.Encode] writes.
const probeIBLWGSLStruct = `struct ProbeIBLUniform {
	sh: array<vec4<f32>, 9>,
	probe0_pos: vec4<f32>,
	probe0_min: vec4<f32>,
	probe0_max: vec4<f32>,
	probe1_pos: vec4<f32>,
	probe1_min: vec4<f32>,
	probe1_max: vec4<f32>,
	params: vec4<f32>,
}

`

// probeIBLWGSLDecl3 declares the probe IBL bindings at [IBLGroup] (@group(3)).
const probeIBLWGSLDecl3 = probeIBLWGSLStruct + `@group(3) @binding(0) var<uniform> probe_ibl: ProbeIBLUniform;
@group(3) @binding(1) var probe_ibl_cube0: texture_cube<f32>;
@group(3) @binding(2) var probe_ibl_cube1: texture_cube<f32>;
@group(3) @binding(3) var probe_ibl_sky: texture_cube<f32>;
@group(3) @binding(4) var probe_ibl_sampler: sampler;

`

// probeIBLWGSLFuncs holds the portable WGSL probe IBL functions (no exp, exp2, log, or log2 so NSGo's software GPU VM and all hardware backends evaluate identically).
const probeIBLWGSLFuncs = `fn probe_slab_tmax(pos: f32, dir: f32, bmin: f32, bmax: f32) -> f32 {
	if (abs(dir) < 1e-6) {
		return 1e30;
	}
	let t1 = (bmin - pos) / dir;
	let t2 = (bmax - pos) / dir;
	return max(t1, t2);
}

fn box_project_dir(world_pos: vec3<f32>, refl_dir: vec3<f32>, probe_pos: vec3<f32>, box_min: vec3<f32>, box_max: vec3<f32>) -> vec3<f32> {
	let dir = normalize(refl_dir);
	let tx = probe_slab_tmax(world_pos.x, dir.x, box_min.x, box_max.x);
	let ty = probe_slab_tmax(world_pos.y, dir.y, box_min.y, box_max.y);
	let tz = probe_slab_tmax(world_pos.z, dir.z, box_min.z, box_max.z);
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

fn evaluate_probe_irradiance_sh(n: vec3<f32>) -> vec3<f32> {
	let d = normalize(n);
	var e = probe_ibl.sh[0].xyz * 0.28209479;
	e = e + probe_ibl.sh[1].xyz * (0.48860251 * d.y);
	e = e + probe_ibl.sh[2].xyz * (0.48860251 * d.z);
	e = e + probe_ibl.sh[3].xyz * (0.48860251 * d.x);
	e = e + probe_ibl.sh[4].xyz * (1.09254843 * d.x * d.y);
	e = e + probe_ibl.sh[5].xyz * (1.09254843 * d.y * d.z);
	e = e + probe_ibl.sh[6].xyz * (0.31539157 * (3.0 * d.z * d.z - 1.0));
	e = e + probe_ibl.sh[7].xyz * (1.09254843 * d.x * d.z);
	e = e + probe_ibl.sh[8].xyz * (0.54627422 * (d.x * d.x - d.y * d.y));
	return max(e, vec3<f32>(0.0));
}

fn probe_env_brdf_approx(f0: vec3<f32>, roughness: f32, n_dot_v: f32) -> vec3<f32> {
	let c0 = vec4<f32>(-1.0, -0.0275, -0.572, 0.022);
	let c1 = vec4<f32>(1.0, 0.0425, 1.04, -0.04);
	let r = roughness * c0 + c1;
	// pow(2.0, x) instead of exp2(x): NSGo v0.3.0's software GPU VM has no exp, exp2, log or log2 and evaluates them to zero (nsgo-dev/nsgo#2334).
	let a004 = min(r.x * r.x, pow(2.0, -9.28 * n_dot_v)) * r.x + r.y;
	let ab = vec2<f32>(-1.04, 1.04) * a004 + r.zw;
	return f0 * ab.x + vec3<f32>(ab.y);
}

fn probe_fresnel_schlick_roughness(cos_theta: f32, f0: vec3<f32>, roughness: f32) -> vec3<f32> {
	let m = max(vec3<f32>(1.0 - roughness), f0);
	return f0 + (m - f0) * pow(clamp(1.0 - cos_theta, 0.0, 1.0), 5.0);
}

fn sample_probe_specular(world_pos: vec3<f32>, refl_dir: vec3<f32>, roughness: f32) -> vec3<f32> {
	let r = normalize(refl_dir);
	var d0 = r;
	if (probe_ibl.probe0_min.w > 0.5) {
		d0 = box_project_dir(world_pos, r, probe_ibl.probe0_pos.xyz, probe_ibl.probe0_min.xyz, probe_ibl.probe0_max.xyz);
	}
	var d1 = r;
	if (probe_ibl.probe1_min.w > 0.5) {
		d1 = box_project_dir(world_pos, r, probe_ibl.probe1_pos.xyz, probe_ibl.probe1_min.xyz, probe_ibl.probe1_max.xyz);
	}
	let c0 = textureSampleLevel(probe_ibl_cube0, probe_ibl_sampler, d0, roughness * probe_ibl.probe0_max.w).rgb;
	let c1 = textureSampleLevel(probe_ibl_cube1, probe_ibl_sampler, d1, roughness * probe_ibl.probe1_max.w).rgb;
	let cs = textureSampleLevel(probe_ibl_sky, probe_ibl_sampler, r, roughness * probe_ibl.params.z).rgb;
	return c0 * probe_ibl.probe0_pos.w + c1 * probe_ibl.probe1_pos.w + cs * probe_ibl.params.y;
}

fn evaluate_probe_ibl(world_pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32, ao: f32) -> vec3<f32> {
	let n = normalize(normal);
	let v = normalize(view_dir);
	let m = clamp(metallic, 0.0, 1.0);
	let rough = clamp(roughness, 0.0, 1.0);
	let n_dot_v = max(dot(n, v), 0.0);
	let f0 = mix(vec3<f32>(0.04), base_color, m);
	let f = probe_fresnel_schlick_roughness(n_dot_v, f0, rough);
	let k_d = (vec3<f32>(1.0) - f) * (1.0 - m);
	let irradiance = evaluate_probe_irradiance_sh(n);
	let diffuse = k_d * base_color * (irradiance * 0.31830989);
	let r = reflect(-v, n);
	let prefiltered = sample_probe_specular(world_pos, r, rough);
	let brdf = probe_env_brdf_approx(f0, rough, n_dot_v);
	let specular = prefiltered * brdf;
	return (diffuse + specular) * (ao * probe_ibl.params.x);
}
`

// ProbeIBLWGSL declares the reflection-probe and light-probe IBL uniform block, two local reflection cubemaps, sky cubemap, and trilinear sampler at [IBLGroup] (@group(3)), together with box_project_dir, evaluate_probe_irradiance_sh, sample_probe_specular, and evaluate_probe_ibl.
//
// Prepend it to a PBR fragment shader whose pipeline binds [GPUProbeIBLBuffer.BindGroup] at [IBLGroup]; [LitPBRProbeIBLWGSL] does this for [LitPBRWGSL]. Use [ProbeIBLWGSLForGroup] when a custom shader places the bindings at another group.
const ProbeIBLWGSL = probeIBLWGSLDecl3 + probeIBLWGSLFuncs

// ProbeIBLWGSLForGroup returns the same WGSL declarations and helper functions as [ProbeIBLWGSL] with the uniform, cubemaps, and sampler placed at the given @group index.
func ProbeIBLWGSLForGroup(group uint32) string {
	return probeIBLWGSLStruct + fmt.Sprintf(`@group(%d) @binding(0) var<uniform> probe_ibl: ProbeIBLUniform;
@group(%d) @binding(1) var probe_ibl_cube0: texture_cube<f32>;
@group(%d) @binding(2) var probe_ibl_cube1: texture_cube<f32>;
@group(%d) @binding(3) var probe_ibl_sky: texture_cube<f32>;
@group(%d) @binding(4) var probe_ibl_sampler: sampler;

`, group, group, group, group, group) + probeIBLWGSLFuncs
}

// LitPBRProbeIBLWGSL is [LitPBRWGSL] extended with local reflection probes and interpolated light-probe irradiance at [IBLGroup] (group 3). It keeps the exact vertex stage (including the inverse-transpose normal matrix), [LightRig] direct lighting, emissive term, and IEC 61966-2-1 sRGB output encode of [LitPBRWGSL] and [LitPBRIBLWGSL], adding evaluate_probe_ibl before linear_to_srgb so [ShadePoint] plus [EvaluateProbeIBL] reproduces it on the CPU.
//
// Set the [LightRig]'s ambient light to zero when probes or the sky supply the ambient term. Use [NewLitPBRProbeIBLPipeline] or [NewLitPBRProbeIBLPipelineWithOptions] to build a pipeline from it.
const LitPBRProbeIBLWGSL = litPBRPreludeWGSL + "\n" + ProbeIBLWGSL + litPBRFragmentHeadWGSL + `	let ibl_lit = lit + evaluate_probe_ibl(in.world_pos, in.world_normal, view_dir, pbr_draw.base_color.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y, 1.0);
	return vec4<f32>(linear_to_srgb(ibl_lit), pbr_draw.base_color.a);
}
`

// litPBRProbeIBLLinearWGSL is [LitPBRProbeIBLWGSL] writing unclamped linear radiance without sRGB encoding, for drawing into an [HDRTarget] resolved by [ToneMapPass].
const litPBRProbeIBLLinearWGSL = litPBRPreludeWGSL + "\n" + ProbeIBLWGSL + litPBRFragmentHeadWGSL + `	let ibl_lit = lit + evaluate_probe_ibl(in.world_pos, in.world_normal, view_dir, pbr_draw.base_color.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y, 1.0);
	return vec4<f32>(ibl_lit, pbr_draw.base_color.a);
}
`

// --- IrradianceSH arithmetic and functional baking helpers ---

// Add returns the component-wise sum sh + other.
func (sh IrradianceSH) Add(other IrradianceSH) IrradianceSH {
	var out IrradianceSH
	for k := range 9 {
		out.Coeff[k][0] = sh.Coeff[k][0] + other.Coeff[k][0]
		out.Coeff[k][1] = sh.Coeff[k][1] + other.Coeff[k][1]
		out.Coeff[k][2] = sh.Coeff[k][2] + other.Coeff[k][2]
	}
	return out
}

// Scale returns sh multiplied by scalar k.
func (sh IrradianceSH) Scale(k float32) IrradianceSH {
	var out IrradianceSH
	for kIdx := range 9 {
		out.Coeff[kIdx][0] = sh.Coeff[kIdx][0] * k
		out.Coeff[kIdx][1] = sh.Coeff[kIdx][1] * k
		out.Coeff[kIdx][2] = sh.Coeff[kIdx][2] * k
	}
	return out
}

// Lerp linearly interpolates between sh (t = 0) and other (t = 1).
func (sh IrradianceSH) Lerp(other IrradianceSH, t float32) IrradianceSH {
	var out IrradianceSH
	u := 1 - t
	for k := range 9 {
		out.Coeff[k][0] = sh.Coeff[k][0]*u + other.Coeff[k][0]*t
		out.Coeff[k][1] = sh.Coeff[k][1]*u + other.Coeff[k][1]*t
		out.Coeff[k][2] = sh.Coeff[k][2]*u + other.Coeff[k][2]*t
	}
	return out
}

// ProjectDirectionIrradianceSH projects a directional radiance sample along dir onto the cosine-convolved L2 spherical harmonic basis used by [IrradianceSH]. Scale each sample by its solid angle dOmega and sum with [IrradianceSH.Add] to integrate an arbitrary directional environment.
func ProjectDirectionIrradianceSH(dir math3d.Vec3, radiance [3]float32) IrradianceSH {
	l := dir.Len()
	if l < 1e-12 || !finite3(dir) {
		return IrradianceSH{}
	}
	d := dir.Scale(1 / l)
	x, y, z := float64(d.X), float64(d.Y), float64(d.Z)
	basis := [9]float64{
		shBasisC0 * shConvA0,
		shBasisC1 * y * shConvA1,
		shBasisC1 * z * shConvA1,
		shBasisC1 * x * shConvA1,
		shBasisC2 * x * y * shConvA2,
		shBasisC2 * y * z * shConvA2,
		shBasisC3 * (3*z*z - 1) * shConvA2,
		shBasisC2 * x * z * shConvA2,
		shBasisC4 * (x*x - y*y) * shConvA2,
	}
	var out IrradianceSH
	r, g, b := float64(radiance[0]), float64(radiance[1]), float64(radiance[2])
	for k := range 9 {
		bk := basis[k]
		out.Coeff[k][0] = float32(r * bk)
		out.Coeff[k][1] = float32(g * bk)
		out.Coeff[k][2] = float32(b * bk)
	}
	return out
}

// ComputeIrradianceSHFromFunc integrates linear RGB radiance returned by sample(dir) over a size x size cubemap using exact per-texel solid angles and cosine-lobe convolution weights, matching [ComputeIrradianceSH] without clamping radiance to 8-bit LDR.
func ComputeIrradianceSHFromFunc(size int, sample func(dir math3d.Vec3) [3]float32) (IrradianceSH, error) {
	if size <= 0 {
		return IrradianceSH{}, fmt.Errorf("%w: SH integration size %d must be positive", ErrInvalidProbe, size)
	}
	if sample == nil {
		return IrradianceSH{}, fmt.Errorf("%w: nil SH sampler function", ErrInvalidProbe)
	}
	invSize := 1.0 / float64(size)
	var accum [9][3]float64
	for face := range gpu.CubeFace(ProbeCubeFaceCount) {
		for py := range size {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range size {
				u := float32((float64(px) + 0.5) * invSize)
				d := CubeFaceDirection(face, u, v)
				dw := cubemapTexelSolidAngle(px, py, size)
				col := sample(d)
				r, g, bl := float64(col[0]), float64(col[1]), float64(col[2])

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

// --- Cubemap Face Cameras ---

// ProbeFaceViewProj builds the right-handed 90-degree FOV view and [0, 1]-depth projection matrices for rendering one cubemap face from pos. The orientations preserve counter-clockwise triangle winding so backface culling works unchanged; when reading back a render target (top-down rows) to upload into a cubemap face, flip the rows vertically ([BakeReflectionProbeGPU] handles this automatically).
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

// ProbeFaceCamera returns a [CameraUniform] configured to render one cubemap face from pos with a 90-degree field of view.
func ProbeFaceCamera(pos math3d.Vec3, face gpu.CubeFace, near, far float32) CameraUniform {
	view, proj := ProbeFaceViewProj(pos, face, near, far)
	return CameraUniform{
		View:     view,
		Proj:     proj,
		ViewProj: proj.Mul(view),
		Eye:      pos.Vec4(1),
	}
}

// --- Reflection Probes, Box Projection & Selection ---

// ReflectionProbe describes a local cubemap reflection volume with optional parallax-corrected box projection and smooth edge fade.
//
// Reflection probe bakes (Cubemap, Prefiltered, and GPUCubemap) are runtime-only data generated on load or on request rather than persisted in text scene files. Call [ReflectionProbe.Close] when releasing a probe whose GPUCubemap is manually scoped; re-baking via [BakeReflectionProbe] or [BakeReflectionProbeGPU] automatically closes the previous GPUCubemap before replacing it.
type ReflectionProbe struct {
	ID            string
	Position      math3d.Vec3
	BoxMin        math3d.Vec3
	BoxMax        math3d.Vec3
	BlendDistance float32 // inward fade width from the box boundary; 0 = sharp box boundary
	Priority      int     // higher priority takes precedence when probes overlap
	BoxProjection bool    // apply parallax-corrected ray-AABB projection
	Intensity     float32 // reflection multiplier; 0 defaults to 1.0 (use Disabled to turn off)
	Cubemap       *Cubemap
	Prefiltered   *Cubemap
	GPUCubemap    *GPUCubemap
	Dirty         bool // needs re-bake on request; dirty probes are skipped during selection
	Baked         bool // cubemap has been baked or populated; unbaked probes are skipped during selection
	Disabled      bool // exclude probe from selection without removing it
}

// Validate checks that p's position, bounding box, blend distance, and intensity are finite and well-formed. Every error wraps [ErrInvalidProbe].
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

// Ready reports whether p is enabled, baked, and clean (not Dirty).
func (p *ReflectionProbe) Ready() bool {
	return p != nil && !p.Disabled && p.Baked && !p.Dirty
}

func (p *ReflectionProbe) cpuCubemap() *Cubemap {
	if !p.Ready() {
		return nil
	}
	if p.Prefiltered != nil {
		return p.Prefiltered
	}
	return p.Cubemap
}

func (p *ReflectionProbe) gpuCubemap() *GPUCubemap {
	if !p.Ready() || !p.GPUCubemap.Valid() {
		return nil
	}
	return p.GPUCubemap
}

// EffectiveIntensity returns the reflection multiplier for p, treating a zero Intensity as 1.0 and returning 0 when p is nil, disabled, negative, or non-finite.
func (p *ReflectionProbe) EffectiveIntensity() float32 {
	if p == nil || p.Disabled || !finite(p.Intensity) || p.Intensity < 0 {
		return 0
	}
	if p.Intensity == 0 {
		return 1
	}
	return p.Intensity
}

// BoxVolume returns the volume of p's influence AABB, or 0 for a nil, inverted, or non-finite box.
func (p *ReflectionProbe) BoxVolume() float32 {
	if p == nil || !finite3(p.BoxMin) || !finite3(p.BoxMax) {
		return 0
	}
	dx := p.BoxMax.X - p.BoxMin.X
	dy := p.BoxMax.Y - p.BoxMin.Y
	dz := p.BoxMax.Z - p.BoxMin.Z
	if dx <= 0 || dy <= 0 || dz <= 0 || !finite(dx) || !finite(dy) || !finite(dz) {
		return 0
	}
	vol := dx * dy * dz
	if !finite(vol) {
		return 0
	}
	return vol
}

// Influence returns the smooth influence weight in [0, 1] of p at worldPos. Points outside [BoxMin, BoxMax] or with non-finite coordinates return 0 (never NaN). Points farther than BlendDistance inside every face return 1; points within BlendDistance of a face fade with cubic smoothstep.
func (p *ReflectionProbe) Influence(worldPos math3d.Vec3) float32 {
	if p == nil || p.Disabled {
		return 0
	}
	if !finite3(worldPos) || !finite3(p.BoxMin) || !finite3(p.BoxMax) || !finite(p.BlendDistance) || p.BlendDistance < 0 {
		return 0
	}
	if p.BoxMin.X > p.BoxMax.X || p.BoxMin.Y > p.BoxMax.Y || p.BoxMin.Z > p.BoxMax.Z {
		return 0
	}
	dx := min(worldPos.X-p.BoxMin.X, p.BoxMax.X-worldPos.X)
	dy := min(worldPos.Y-p.BoxMin.Y, p.BoxMax.Y-worldPos.Y)
	dz := min(worldPos.Z-p.BoxMin.Z, p.BoxMax.Z-worldPos.Z)
	minDist := min(dx, min(dy, dz))
	if !finite(minDist) {
		return 0
	}
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

// Close releases p's GPUCubemap if present and marks p unbaked. Safe to call on nil or more than once.
func (p *ReflectionProbe) Close() error {
	if p == nil {
		return nil
	}
	var err error
	if p.GPUCubemap != nil {
		err = p.GPUCubemap.Close()
		p.GPUCubemap = nil
	}
	p.Baked = false
	return err
}

// BoxProjectDirection computes the parallax-corrected cubemap lookup direction for a reflection ray originating at worldPos along reflDir inside the box [boxMin, boxMax] around probePos. Non-finite inputs never produce NaN.
func BoxProjectDirection(worldPos, reflDir, probePos, boxMin, boxMax math3d.Vec3) math3d.Vec3 {
	if !finite3(reflDir) {
		return math3d.Vec3Zero()
	}
	dir := reflDir.Norm()
	if dir.LenSq() == 0 || !finite3(dir) {
		return math3d.Vec3Zero()
	}
	if !finite3(worldPos) || !finite3(probePos) || !finite3(boxMin) || !finite3(boxMax) {
		return dir
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
	if !finite3(offset) || offset.LenSq() < 1e-12 {
		return dir
	}
	return offset.Norm()
}

// ProbeBlendResult holds up to two active local reflection probes selected for a world position together with normalized blend weights (Weight0 + Weight1 + SkyWeight == 1).
type ProbeBlendResult struct {
	Primary   *ReflectionProbe
	Secondary *ReflectionProbe
	Weight0   float32
	Weight1   float32
	SkyWeight float32
}

// EvaluateSpecular evaluates the blended specular prefiltered environment color on the CPU at worldPos along reflDir for roughness in [0, 1], matching sample_probe_specular in [ProbeIBLWGSL]. Unbaked, dirty, disabled, or nil-cubemap probes and a nil skyPrefilter all contribute black (0, 0, 0), matching the GPU fallback cubemap identically.
func (r ProbeBlendResult) EvaluateSpecular(worldPos, reflDir math3d.Vec3, roughness float32, skyPrefilter *Cubemap) [3]float32 {
	if !finite3(reflDir) || reflDir.LenSq() < 1e-12 {
		return [3]float32{}
	}
	dir := reflDir.Norm()
	rough := float32(clamp64(float64(finiteOrZero(roughness)), 0, 1))
	var out [3]float32
	if cm0 := r.Primary.cpuCubemap(); cm0 != nil && finite(r.Weight0) && r.Weight0 > 0 {
		d0 := dir
		if r.Primary.BoxProjection {
			d0 = BoxProjectDirection(worldPos, dir, r.Primary.Position, r.Primary.BoxMin, r.Primary.BoxMax)
		}
		maxLOD0 := float32(max(cm0.MipLevels()-1, 0))
		c0 := cm0.SampleLOD(d0, rough*maxLOD0)
		s0 := r.Weight0 * r.Primary.EffectiveIntensity()
		out[0] += c0[0] * s0
		out[1] += c0[1] * s0
		out[2] += c0[2] * s0
	}
	if cm1 := r.Secondary.cpuCubemap(); cm1 != nil && finite(r.Weight1) && r.Weight1 > 0 {
		d1 := dir
		if r.Secondary.BoxProjection {
			d1 = BoxProjectDirection(worldPos, dir, r.Secondary.Position, r.Secondary.BoxMin, r.Secondary.BoxMax)
		}
		maxLOD1 := float32(max(cm1.MipLevels()-1, 0))
		c1 := cm1.SampleLOD(d1, rough*maxLOD1)
		s1 := r.Weight1 * r.Secondary.EffectiveIntensity()
		out[0] += c1[0] * s1
		out[1] += c1[1] * s1
		out[2] += c1[2] * s1
	}
	if skyPrefilter != nil && finite(r.SkyWeight) && r.SkyWeight > 0 {
		maxLODS := float32(max(skyPrefilter.MipLevels()-1, 0))
		cs := skyPrefilter.SampleLOD(dir, rough*maxLODS)
		out[0] += cs[0] * r.SkyWeight
		out[1] += cs[1] * r.SkyWeight
		out[2] += cs[2] * r.SkyWeight
	}
	return out
}

// ReflectionProbeSet manages a collection of local [ReflectionProbe] volumes and selects up to two overlapping ready probes to blend over the global sky environment.
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

// SelectReflectionProbes selects up to two active baked, non-dirty reflection probes affecting worldPos and computes smooth normalized blend weights (Weight0, Weight1, SkyWeight) summing to 1. Unbaked, dirty, and disabled probes are skipped so objects fall back smoothly to ready probes or the global sky.
func (s *ReflectionProbeSet) SelectReflectionProbes(worldPos math3d.Vec3) ProbeBlendResult {
	if s == nil || len(s.Probes) == 0 || !finite3(worldPos) {
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
		if !p.Ready() {
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

// Close closes every probe's GPUCubemap in the set. Safe to call on nil or more than once.
func (s *ReflectionProbeSet) Close() error {
	if s == nil {
		return nil
	}
	var err error
	for _, p := range s.Probes {
		if p != nil {
			err = errors.Join(err, p.Close())
		}
	}
	return err
}

// --- Probe Baking (CPU & Asynchronous GPU) ---

// ProbeBakeOptions configures [BakeReflectionProbe] and [BakeReflectionProbeGPU].
//
// Reflection probe bakes are runtime-only data generated on load or on request rather than persisted in text scene files.
//
// For [BakeReflectionProbeGPU], each temporary offscreen face render target uses [gpu.FormatRGBA8Unorm] when SRGB is false or [gpu.FormatRGBA8UnormSRGB] when SRGB is true (unless Upload.Format explicitly selects one of those two formats), paired with [gpu.FormatDepth24Plus] for depth. Match the color format of any pipeline drawn inside renderFace (for example, [gpu.FormatRGBA8Unorm] for sRGB-encoding shaders such as [LitPBRWGSL], or [gpu.FormatRGBA8UnormSRGB] for linear-output shaders).
type ProbeBakeOptions struct {
	// Size is the face edge length in texels (must be positive).
	Size int
	// Near and Far set the 90-degree cubemap face projection clip planes for [BakeReflectionProbeGPU]. Non-positive Near defaults to 0.05; Far <= Near defaults to Near + 100.
	Near float32
	Far  float32
	// ClearColor is the background clear color for each face pass in [BakeReflectionProbeGPU].
	ClearColor core.Color
	// SRGB marks the baked cubemap as sRGB-encoded: [BakeReflectionProbeGPU] renders into [gpu.FormatRGBA8UnormSRGB] targets and [UploadCubemap] uploads [gpu.FormatRGBA8UnormSRGB] so sampling decodes to linear light. When false, linear [gpu.FormatRGBA8Unorm] is used.
	SRGB bool
	// Prefilter configures GGX specular roughness mip prefiltering via [PrefilterEnvironment]. Zero fields use [PrefilterEnvironment]'s defaults unless SkipPrefilter is true.
	Prefilter PrefilterOptions
	// SkipPrefilter skips GGX prefiltering and uploads the captured mip-0 cubemap directly (with box mip generation controlled by Upload.GenerateMips).
	SkipPrefilter bool
	// Upload configures [UploadCubemap] for the final [GPUCubemap] (Label, Format, GenerateMips, Retained).
	Upload CubemapUpload
}

func resolveProbeBakeFormat(opts ProbeBakeOptions) (gpu.TextureFormat, bool, error) {
	switch opts.Upload.Format {
	case gpu.FormatUndefined:
		if opts.SRGB {
			return gpu.FormatRGBA8UnormSRGB, true, nil
		}
		return gpu.FormatRGBA8Unorm, false, nil
	case gpu.FormatRGBA8Unorm:
		return gpu.FormatRGBA8Unorm, false, nil
	case gpu.FormatRGBA8UnormSRGB:
		return gpu.FormatRGBA8UnormSRGB, true, nil
	default:
		return gpu.FormatUndefined, false, fmt.Errorf("%w: unsupported probe bake format %s (expected RGBA8Unorm or RGBA8UnormSRGB)", ErrInvalidCubemap, opts.Upload.Format)
	}
}

// BakeReflectionProbeCPU samples a 6-face [Cubemap] of resolution size x size from probe.Position using sample(origin, dir), which returns linear RGB radiance in [0, 1]. When srgb is true, texels are sRGB-encoded and Cubemap.SRGB is set.
func BakeReflectionProbeCPU(probe *ReflectionProbe, size int, srgb bool, sample func(origin, dir math3d.Vec3) [3]float32) (*Cubemap, error) {
	if err := probe.Validate(); err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, fmt.Errorf("%w: bake size %d must be positive", ErrInvalidProbe, size)
	}
	if sample == nil {
		return nil, fmt.Errorf("%w: nil bake sampler function", ErrInvalidProbe)
	}
	cm := &Cubemap{SRGB: srgb}
	invSize := 1.0 / float64(size)
	for face := range gpu.CubeFace(ProbeCubeFaceCount) {
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		for py := range size {
			v := float32((float64(py) + 0.5) * invSize)
			for px := range size {
				u := float32((float64(px) + 0.5) * invSize)
				dir := CubeFaceDirection(face, u, v)
				c := sample(probe.Position, dir)
				off := dst.PixOffset(px, py)
				dst.Pix[off+0] = encodeTexelByte(float64(c[0]), srgb)
				dst.Pix[off+1] = encodeTexelByte(float64(c[1]), srgb)
				dst.Pix[off+2] = encodeTexelByte(float64(c[2]), srgb)
				dst.Pix[off+3] = 255
			}
		}
		cm.Faces[face] = dst
	}
	return cm, nil
}

func finalizeProbeBake(cx *core.Scope, dev *gpu.Device, probe *ReflectionProbe, raw *Cubemap, opts ProbeBakeOptions, colorFmt gpu.TextureFormat) error {
	var toUpload *Cubemap
	var prefiltered *Cubemap
	if opts.SkipPrefilter {
		toUpload = raw
	} else {
		var err error
		prefiltered, err = PrefilterEnvironment(raw, opts.Prefilter)
		if err != nil {
			return err
		}
		toUpload = prefiltered
	}
	uploadOpts := opts.Upload
	uploadOpts.Format = colorFmt
	if uploadOpts.Label == "" && probe.ID != "" {
		uploadOpts.Label = "probe:" + probe.ID
	}
	newGPU, err := UploadCubemap(cx, dev, toUpload, uploadOpts)
	if err != nil {
		return err
	}
	oldGPU := probe.GPUCubemap
	probe.Cubemap = raw
	probe.Prefiltered = prefiltered
	probe.GPUCubemap = newGPU
	probe.Baked = true
	probe.Dirty = false
	if oldGPU != nil && oldGPU != newGPU {
		_ = oldGPU.Close()
	}
	return nil
}

// BakeReflectionProbe bakes probe on request using sample(origin, dir), prefilters roughness mips with [PrefilterEnvironment] (unless opts.SkipPrefilter is set), uploads the resulting [GPUCubemap] to dev, closes any previous probe.GPUCubemap to prevent leaks, and marks probe Baked = true and Dirty = false.
func BakeReflectionProbe(cx *core.Scope, dev *gpu.Device, probe *ReflectionProbe, opts ProbeBakeOptions, sample func(origin, dir math3d.Vec3) [3]float32) error {
	colorFmt, srgb, err := resolveProbeBakeFormat(opts)
	if err != nil {
		return err
	}
	raw, err := BakeReflectionProbeCPU(probe, opts.Size, srgb, sample)
	if err != nil {
		return err
	}
	return finalizeProbeBake(cx, dev, probe, raw, opts, colorFmt)
}

// BakeReflectionProbeGPU asynchronously bakes probe on request by rendering all six cubemap faces on dev into temporary offscreen render targets via renderFace, queueing [gpu.Device.ReadTexture] for each face, flipping rows to canonical cubemap orientation, prefiltering roughness mips with [PrefilterEnvironment] (unless opts.SkipPrefilter is set), uploading the [GPUCubemap], closing any previous probe.GPUCubemap, and invoking onDone on the engine goroutine once all six readbacks complete.
//
// Temporary offscreen render targets use [gpu.FormatRGBA8Unorm] (when opts.SRGB is false) or [gpu.FormatRGBA8UnormSRGB] (when opts.SRGB is true) paired with [gpu.FormatDepth24Plus]. They are closed automatically once all six readbacks finish. Because [gpu.Device.ReadTexture] is asynchronous inside a live [gpu.View], callers must wait for onDone rather than assuming probe.GPUCubemap is populated immediately upon return.
func BakeReflectionProbeGPU(
	cx *core.Scope,
	dev *gpu.Device,
	probe *ReflectionProbe,
	opts ProbeBakeOptions,
	renderFace func(pass *gpu.RenderPass, face gpu.CubeFace, cam CameraUniform),
	onDone func(error),
) {
	finish := func(err error) {
		if onDone != nil {
			onDone(err)
		}
	}
	if err := probe.Validate(); err != nil {
		finish(err)
		return
	}
	if opts.Size <= 0 {
		finish(fmt.Errorf("%w: bake size %d must be positive", ErrInvalidProbe, opts.Size))
		return
	}
	if renderFace == nil {
		finish(fmt.Errorf("%w: nil renderFace callback", ErrInvalidProbe))
		return
	}
	if dev == nil {
		finish(gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"})
		return
	}
	colorFmt, srgb, err := resolveProbeBakeFormat(opts)
	if err != nil {
		finish(err)
		return
	}

	size := opts.Size
	var rts [ProbeCubeFaceCount]*gpu.RenderTarget
	closeRTs := func() {
		for i := range rts {
			if rts[i] != nil {
				_ = rts[i].Close()
				rts[i] = nil
			}
		}
	}

	for f := range ProbeCubeFaceCount {
		rt, rtErr := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
			Label:       fmt.Sprintf("render3d.probe_bake_rt_%d", f),
			Width:       uint32(size),
			Height:      uint32(size),
			ColorFormat: colorFmt,
			DepthFormat: gpu.FormatDepth24Plus,
		})
		if rtErr != nil {
			closeRTs()
			finish(rtErr)
			return
		}
		rts[f] = rt
	}

	for f := range ProbeCubeFaceCount {
		face := gpu.CubeFace(f)
		cam := ProbeFaceCamera(probe.Position, face, opts.Near, opts.Far)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			Label: "render3d.probe_bake_face",
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    rts[f].Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: opts.ClearColor,
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      rts[f].Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		renderFace(pass, face, cam)
		pass.End()
		dev.Submit(enc.Finish())
	}

	wantBytes := size * size * 4
	var (
		faces     [ProbeCubeFaceCount]*image.RGBA
		firstErr  error
		remaining = ProbeCubeFaceCount
	)
	for f := range ProbeCubeFaceCount {
		faceIdx := f
		dev.ReadTexture(rts[faceIdx].Color, gpu.TextureRegion{
			Width:  uint32(size),
			Height: uint32(size),
		}, func(rgba []byte, readErr error) {
			if readErr != nil {
				if firstErr == nil {
					firstErr = readErr
				}
			} else if len(rgba) < wantBytes {
				if firstErr == nil {
					firstErr = fmt.Errorf("%w: face %d readback returned %d bytes, want %d", ErrInvalidProbe, faceIdx, len(rgba), wantBytes)
				}
			} else {
				faces[faceIdx] = flippedRGBAToImage(rgba[:wantBytes], size)
			}
			remaining--
			if remaining > 0 {
				return
			}
			closeRTs()
			if firstErr != nil {
				finish(firstErr)
				return
			}
			raw := &Cubemap{
				Faces: faces,
				SRGB:  srgb,
			}
			finish(finalizeProbeBake(cx, dev, probe, raw, opts, colorFmt))
		})
	}
}

func flippedRGBAToImage(src []byte, size int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	rowBytes := size * 4
	for y := range size {
		sy := size - 1 - y
		copy(dst.Pix[y*rowBytes:(y+1)*rowBytes], src[sy*rowBytes:(sy+1)*rowBytes])
	}
	return dst
}

// --- Light Probes (IrradianceSH), Grid & Volume ---

// LightProbe stores baked L2 diffuse spherical harmonic irradiance ([IrradianceSH]) at a world-space sample point for shading dynamic objects.
type LightProbe struct {
	ID       string
	Position math3d.Vec3
	Radius   float32 // optional influence radius; <= 0 means unbounded inverse-distance blending
	SH       IrradianceSH
	Dirty    bool
	Baked    bool
}

// Validate checks that p's position, radius, and SH coefficients are finite and non-negative where required. Every error wraps [ErrInvalidProbe].
func (p LightProbe) Validate() error {
	if !finite3(p.Position) {
		return fmt.Errorf("%w: light probe %q position is not finite", ErrInvalidProbe, p.ID)
	}
	if !finite(p.Radius) || p.Radius < 0 {
		return fmt.Errorf("%w: light probe %q radius %v must be finite and non-negative", ErrInvalidProbe, p.ID, p.Radius)
	}
	for k := range 9 {
		for ch := range 3 {
			if !finite(p.SH.Coeff[k][ch]) {
				return fmt.Errorf("%w: light probe %q SH coefficient %d channel %d is not finite", ErrInvalidProbe, p.ID, k, ch)
			}
		}
	}
	return nil
}

// LightProbeGrid is a regular 3D axis-aligned lattice of L2 [IrradianceSH] light probes interpolated trilinearly at runtime.
//
// Light probe bakes are runtime-only data generated on load or on request rather than persisted in text scene files.
type LightProbeGrid struct {
	Bounds math3d.AABB
	Counts [3]int         // probe counts along X, Y, Z (each >= 1)
	Probes []IrradianceSH // flattened (iz*ny + iy)*nx + ix
}

// NewLightProbeGrid allocates a zeroed nx x ny x nz [LightProbeGrid] covering bounds.
func NewLightProbeGrid(bounds math3d.AABB, nx, ny, nz int) (*LightProbeGrid, error) {
	if bounds.Empty() || !finite3(bounds.Min) || !finite3(bounds.Max) {
		return nil, fmt.Errorf("%w: light probe grid bounds are invalid", ErrInvalidProbe)
	}
	if nx <= 0 || ny <= 0 || nz <= 0 {
		return nil, fmt.Errorf("%w: light probe grid counts %dx%dx%d must be positive", ErrInvalidProbe, nx, ny, nz)
	}
	total, ok := probeGridCellCount(nx, ny, nz)
	if !ok {
		return nil, fmt.Errorf("%w: light probe grid counts %dx%dx%d overflow", ErrInvalidProbe, nx, ny, nz)
	}
	return &LightProbeGrid{
		Bounds: bounds,
		Counts: [3]int{nx, ny, nz},
		Probes: make([]IrradianceSH, total),
	}, nil
}

func probeGridCellCount(nx, ny, nz int) (int, bool) {
	if nx <= 0 || ny <= 0 || nz <= 0 {
		return 0, false
	}
	if nx > math.MaxInt/ny {
		return 0, false
	}
	nxy := nx * ny
	if nxy > math.MaxInt/nz {
		return 0, false
	}
	return nxy * nz, true
}

func (g *LightProbeGrid) validStorage() bool {
	if g == nil {
		return false
	}
	total, ok := probeGridCellCount(g.Counts[0], g.Counts[1], g.Counts[2])
	return ok && len(g.Probes) >= total
}

// Validate checks that g has finite non-empty bounds, positive counts, and len(Probes) == Counts[0]*Counts[1]*Counts[2]. Every error wraps [ErrInvalidProbe].
func (g *LightProbeGrid) Validate() error {
	if g == nil {
		return fmt.Errorf("%w: nil light probe grid", ErrInvalidProbe)
	}
	if g.Bounds.Empty() || !finite3(g.Bounds.Min) || !finite3(g.Bounds.Max) {
		return fmt.Errorf("%w: light probe grid bounds are invalid", ErrInvalidProbe)
	}
	total, ok := probeGridCellCount(g.Counts[0], g.Counts[1], g.Counts[2])
	if !ok {
		return fmt.Errorf("%w: light probe grid counts %dx%dx%d are invalid", ErrInvalidProbe, g.Counts[0], g.Counts[1], g.Counts[2])
	}
	if len(g.Probes) != total {
		return fmt.Errorf("%w: light probe grid has %d probes, want %d for %dx%dx%d", ErrInvalidProbe, len(g.Probes), total, g.Counts[0], g.Counts[1], g.Counts[2])
	}
	return nil
}

func (g *LightProbeGrid) index(ix, iy, iz int) int {
	return (iz*g.Counts[1]+iy)*g.Counts[0] + ix
}

// ProbePosition returns the world position of grid probe (ix, iy, iz), or the zero vector if g is nil.
func (g *LightProbeGrid) ProbePosition(ix, iy, iz int) math3d.Vec3 {
	if g == nil {
		return math3d.Vec3Zero()
	}
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

// Set writes sh to grid cell (ix, iy, iz). Out-of-range coordinates or a grid whose Probes slice is smaller than Counts are ignored safely without panicking.
func (g *LightProbeGrid) Set(ix, iy, iz int, sh IrradianceSH) {
	if !g.validStorage() {
		return
	}
	if ix < 0 || iy < 0 || iz < 0 || ix >= g.Counts[0] || iy >= g.Counts[1] || iz >= g.Counts[2] {
		return
	}
	g.Probes[g.index(ix, iy, iz)] = sh
}

// At returns the [IrradianceSH] coefficients stored at grid cell (ix, iy, iz), clamping coordinates into range. Returns a zero [IrradianceSH] without panicking when g is nil, has non-positive Counts, or has fewer entries in Probes than Counts requires.
func (g *LightProbeGrid) At(ix, iy, iz int) IrradianceSH {
	if !g.validStorage() {
		return IrradianceSH{}
	}
	ix = clampInt(ix, 0, g.Counts[0]-1)
	iy = clampInt(iy, 0, g.Counts[1]-1)
	iz = clampInt(iz, 0, g.Counts[2]-1)
	return g.Probes[g.index(ix, iy, iz)]
}

// Bake populates every probe in g by integrating sample(probePos, dir) over a cubemap of resolution size x size per probe via [ComputeIrradianceSHFromFunc].
func (g *LightProbeGrid) Bake(size int, sample func(pos, dir math3d.Vec3) [3]float32) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if size <= 0 {
		return fmt.Errorf("%w: light probe bake size %d must be positive", ErrInvalidProbe, size)
	}
	if sample == nil {
		return fmt.Errorf("%w: nil light probe bake sampler", ErrInvalidProbe)
	}
	for iz := range g.Counts[2] {
		for iy := range g.Counts[1] {
			for ix := range g.Counts[0] {
				pos := g.ProbePosition(ix, iy, iz)
				sh, err := ComputeIrradianceSHFromFunc(size, func(dir math3d.Vec3) [3]float32 {
					return sample(pos, dir)
				})
				if err != nil {
					return err
				}
				g.Set(ix, iy, iz, sh)
			}
		}
	}
	return nil
}

// Sample trilinearly interpolates [IrradianceSH] coefficients at worldPos, clamping positions outside Bounds to the boundary cell. Returns a zero [IrradianceSH] without panicking when g has invalid storage or worldPos is non-finite.
func (g *LightProbeGrid) Sample(worldPos math3d.Vec3) IrradianceSH {
	if !g.validStorage() || !finite3(worldPos) || !finite3(g.Bounds.Min) || !finite3(g.Bounds.Max) {
		return IrradianceSH{}
	}
	axisCoord := func(p, lo, hi float32, n int) (i0, i1 int, t float32) {
		if n <= 1 || hi <= lo {
			return 0, 0, 0
		}
		u := math3d.Clamp((p-lo)/(hi-lo), 0, 1) * float32(n-1)
		i0 = clampInt(int(math.Floor(float64(u))), 0, n-1)
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

// LightProbeVolume combines an optional regular [LightProbeGrid] and/or scattered [LightProbe] points (plus an optional FallbackSH) so dynamic objects pick up smoothly interpolated baked indirect lighting anywhere in a scene.
//
// Light probe volumes are runtime-only data generated on load or on request rather than persisted in text scene files.
type LightProbeVolume struct {
	Grid       *LightProbeGrid
	Probes     []LightProbe
	FallbackSH IrradianceSH
}

// Add appends scattered probes to the volume.
func (v *LightProbeVolume) Add(probes ...LightProbe) {
	v.Probes = append(v.Probes, probes...)
}

// SampleLightProbes returns the interpolated [IrradianceSH] lighting at worldPos. Regular grid probes use trilinear interpolation; scattered probes use smooth inverse-distance weighting (respecting per-probe Radius when positive) and blend seamlessly into Grid or FallbackSH outside finite probe radii. Non-finite worldPos safely returns FallbackSH.
func (v *LightProbeVolume) SampleLightProbes(worldPos math3d.Vec3) IrradianceSH {
	if v == nil {
		return IrradianceSH{}
	}
	if !finite3(worldPos) {
		return v.FallbackSH
	}
	baseSH := v.FallbackSH
	if v.Grid != nil && v.Grid.validStorage() {
		baseSH = v.Grid.Sample(worldPos)
	}
	if len(v.Probes) == 0 {
		return baseSH
	}

	var accum IrradianceSH
	var weightSum float32
	var maxCoverage float32
	hasUnbounded := false

	for i := range v.Probes {
		p := &v.Probes[i]
		if !finite3(p.Position) || !finite(p.Radius) {
			continue
		}
		distSq := worldPos.DistSq(p.Position)
		if !finite(distSq) {
			continue
		}
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

// --- CPU Reference Evaluator ---

// EvaluateProbeIBL evaluates the exact CPU reference for evaluate_probe_ibl in [ProbeIBLWGSL]: L2 spherical harmonic diffuse irradiance combined with parallax-corrected, blended specular reflections across up to two local reflection probes and an optional global sky cubemap.
//
// Add the result to [ShadePoint] and apply [LinearToSRGB] to reproduce [LitPBRProbeIBLWGSL] pixel-for-pixel. Unbaked, dirty, disabled, or nil-cubemap probes and a nil skyPrefilter all contribute black (0, 0, 0), matching [GPUProbeIBLBuffer] identically. Non-finite metallic, roughness, ao, or intensity values count as 0.
func EvaluateProbeIBL(
	sh IrradianceSH,
	blend ProbeBlendResult,
	skyPrefilter *Cubemap,
	intensity float32,
	worldPos, normal, viewDir math3d.Vec3,
	baseColor [3]float32,
	metallic, roughness, ao float32,
) [3]float32 {
	metallic, roughness, ao, intensity = finiteOrZero(metallic), finiteOrZero(roughness), finiteOrZero(ao), max(finiteOrZero(intensity), 0)
	if !finite3(normal) || !finite3(viewDir) || normal.LenSq() < 1e-12 || viewDir.LenSq() < 1e-12 {
		return [3]float32{}
	}
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
	prefiltered := blend.EvaluateSpecular(worldPos, rDir, rough, skyPrefilter)
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

// --- Probe IBL Uniform, GPU Buffer & Lit PBR Pipeline ---

// ProbeIBLUniform holds the per-object diffuse L2 spherical harmonic irradiance and blended reflection-probe parameters bound by [GPUProbeIBLBuffer] at [IBLGroup] and read by [ProbeIBLWGSL].
type ProbeIBLUniform struct {
	SH IrradianceSH

	Probe0Pos           math3d.Vec3
	Probe0BoxMin        math3d.Vec3
	Probe0BoxMax        math3d.Vec3
	Probe0Weight        float32
	Probe0MaxLOD        float32
	Probe0BoxProjection bool

	Probe1Pos           math3d.Vec3
	Probe1BoxMin        math3d.Vec3
	Probe1BoxMax        math3d.Vec3
	Probe1Weight        float32
	Probe1MaxLOD        float32
	Probe1BoxProjection bool

	Intensity float32
	SkyWeight float32
	SkyMaxLOD float32
}

// NewProbeIBLUniform builds a [ProbeIBLUniform] from interpolated light-probe irradiance sh, reflection probe blend result blend, optional sky prefiltered cubemap sky, and overall IBL intensity.
//
// Unbaked, dirty, disabled, or invalid-GPUCubemap probes receive zero weight, and a nil or invalid sky receives zero SkyWeight and SkyMaxLOD, matching [EvaluateProbeIBL] identically.
func NewProbeIBLUniform(sh IrradianceSH, blend ProbeBlendResult, sky *GPUCubemap, intensity float32) ProbeIBLUniform {
	u := ProbeIBLUniform{
		SH:        sh,
		Intensity: intensity,
	}
	if g0 := blend.Primary.gpuCubemap(); g0 != nil && blend.Weight0 > 0 {
		u.Probe0Pos = blend.Primary.Position
		u.Probe0BoxMin = blend.Primary.BoxMin
		u.Probe0BoxMax = blend.Primary.BoxMax
		u.Probe0Weight = blend.Weight0 * blend.Primary.EffectiveIntensity()
		u.Probe0MaxLOD = g0.MaxLOD()
		u.Probe0BoxProjection = blend.Primary.BoxProjection
	}
	if g1 := blend.Secondary.gpuCubemap(); g1 != nil && blend.Weight1 > 0 {
		u.Probe1Pos = blend.Secondary.Position
		u.Probe1BoxMin = blend.Secondary.BoxMin
		u.Probe1BoxMax = blend.Secondary.BoxMax
		u.Probe1Weight = blend.Weight1 * blend.Secondary.EffectiveIntensity()
		u.Probe1MaxLOD = g1.MaxLOD()
		u.Probe1BoxProjection = blend.Secondary.BoxProjection
	}
	if sky != nil && sky.Valid() && blend.SkyWeight > 0 {
		u.SkyWeight = blend.SkyWeight
		u.SkyMaxLOD = sky.MaxLOD()
	}
	return u
}

// Validate checks that every spherical harmonic coefficient, probe coordinate, weight, LOD, and intensity is finite and non-negative where required. Every error wraps [ErrInvalidProbe].
func (u ProbeIBLUniform) Validate() error {
	for k := range 9 {
		for ch := range 3 {
			if !finite(u.SH.Coeff[k][ch]) {
				return fmt.Errorf("%w: probe IBL SH coefficient %d channel %d is not finite", ErrInvalidProbe, k, ch)
			}
		}
	}
	if !finite3(u.Probe0Pos) || !finite3(u.Probe0BoxMin) || !finite3(u.Probe0BoxMax) {
		return fmt.Errorf("%w: probe0 geometry is not finite", ErrInvalidProbe)
	}
	if !finite3(u.Probe1Pos) || !finite3(u.Probe1BoxMin) || !finite3(u.Probe1BoxMax) {
		return fmt.Errorf("%w: probe1 geometry is not finite", ErrInvalidProbe)
	}
	for _, field := range []struct {
		name string
		val  float32
	}{
		{"probe0 weight", u.Probe0Weight},
		{"probe0 max LOD", u.Probe0MaxLOD},
		{"probe1 weight", u.Probe1Weight},
		{"probe1 max LOD", u.Probe1MaxLOD},
		{"intensity", u.Intensity},
		{"sky weight", u.SkyWeight},
		{"sky max LOD", u.SkyMaxLOD},
	} {
		if !finite(field.val) || field.val < 0 {
			return fmt.Errorf("%w: %s %v must be finite and non-negative", ErrInvalidProbe, field.name, field.val)
		}
	}
	return nil
}

// Encode writes u into dst as the little-endian [ProbeIBLUniformSize]-byte block declared by [ProbeIBLWGSL] and returns dst[:ProbeIBLUniformSize] without allocating. It panics if len(dst) < ProbeIBLUniformSize.
func (u *ProbeIBLUniform) Encode(dst []byte) []byte {
	if len(dst) < ProbeIBLUniformSize {
		panic("render3d: ProbeIBLUniform.Encode: dst shorter than ProbeIBLUniformSize")
	}
	dst = dst[:ProbeIBLUniformSize]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	boolF32 := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	for k := range 9 {
		put(u.SH.Coeff[k][0])
		put(u.SH.Coeff[k][1])
		put(u.SH.Coeff[k][2])
		put(0)
	}
	put(u.Probe0Pos.X)
	put(u.Probe0Pos.Y)
	put(u.Probe0Pos.Z)
	put(u.Probe0Weight)
	put(u.Probe0BoxMin.X)
	put(u.Probe0BoxMin.Y)
	put(u.Probe0BoxMin.Z)
	put(boolF32(u.Probe0BoxProjection))
	put(u.Probe0BoxMax.X)
	put(u.Probe0BoxMax.Y)
	put(u.Probe0BoxMax.Z)
	put(u.Probe0MaxLOD)

	put(u.Probe1Pos.X)
	put(u.Probe1Pos.Y)
	put(u.Probe1Pos.Z)
	put(u.Probe1Weight)
	put(u.Probe1BoxMin.X)
	put(u.Probe1BoxMin.Y)
	put(u.Probe1BoxMin.Z)
	put(boolF32(u.Probe1BoxProjection))
	put(u.Probe1BoxMax.X)
	put(u.Probe1BoxMax.Y)
	put(u.Probe1BoxMax.Z)
	put(u.Probe1MaxLOD)

	put(u.Intensity)
	put(u.SkyWeight)
	put(u.SkyMaxLOD)
	put(0)
	return dst
}

// ProbeIBLBindGroupLayout creates the [IBLGroup] bind group layout shared by [GPUProbeIBLBuffer] and [LitPBRProbeIBLWGSL]: binding 0 uniform buffer, bindings 1..3 cube textures (probe0, probe1, sky), and binding 4 trilinear sampler.
func ProbeIBLBindGroupLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.probe_ibl",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
}

// ProbeIBLBufferOptions configures [NewGPUProbeIBLBuffer].
type ProbeIBLBufferOptions struct {
	// Label names the uniform buffer, fallback black cubemap, bind group layout, and bind group in GPU capture tools.
	Label string
	// Layout optionally reuses a shared [ProbeIBLBindGroupLayout] across multiple per-object [GPUProbeIBLBuffer] instances so they share one pipeline. When nil, the buffer creates and owns its own layout.
	Layout *gpu.BindGroupLayout
	// Retained keeps CPU shadow copies of the uniform buffer and black fallback cubemap so NSGo recreates them across a GPU device loss.
	Retained bool
}

// GPUProbeIBLBuffer holds a per-object [ProbeIBLUniform] buffer, 1x1 black fallback cubemap, and [IBLGroup] bind group wiring two local reflection probe cubemaps and a sky cubemap into [ProbeIBLWGSL] / [LitPBRProbeIBLWGSL].
//
// Any omitted, unbaked, dirty, or nil cubemap slot is bound to the 1x1 black (0, 0, 0, 255) fallback cubemap (never white), guaranteeing exact CPU/GPU parity with [EvaluateProbeIBL]. Calling [GPUProbeIBLBuffer.Update] writes only the uniform buffer without recreating the bind group whenever the three bound cubemap textures are unchanged.
type GPUProbeIBLBuffer struct {
	cx            *core.Scope
	dev           *gpu.Device
	label         string
	retained      bool
	buf           *gpu.Buffer
	layout        *gpu.BindGroupLayout
	ownedLayout   bool
	fallbackBlack *GPUCubemap
	group         *gpu.BindGroup
	boundTex0     *gpu.Texture
	boundTex1     *gpu.Texture
	boundSky      *gpu.Texture
	scratch       [ProbeIBLUniformSize]byte
}

func newBlackFallbackCubemap(cx *core.Scope, dev *gpu.Device, label string, retained bool) (*GPUCubemap, error) {
	cm := &Cubemap{}
	for face := range ProbeCubeFaceCount {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 0, 0, 0, 255
		cm.Faces[face] = img
	}
	return UploadCubemap(cx, dev, cm, CubemapUpload{
		Label:    label + ".black_fallback",
		Format:   gpu.FormatRGBA8Unorm,
		Retained: retained,
	})
}

// NewGPUProbeIBLBuffer allocates a [GPUProbeIBLBuffer] on dev, owned by cx (nil for manual Close), initially bound to a zero uniform and the 1x1 black fallback cubemap across all three cube slots.
func NewGPUProbeIBLBuffer(cx *core.Scope, dev *gpu.Device, opts ProbeIBLBufferOptions) (*GPUProbeIBLBuffer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.probe_ibl"
	}
	b := &GPUProbeIBLBuffer{
		cx:       cx,
		dev:      dev,
		label:    label,
		retained: opts.Retained,
	}
	var zero ProbeIBLUniform
	zero.Encode(b.scratch[:])

	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".uniform",
		Size:     ProbeIBLUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     b.scratch[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}

	layout := opts.Layout
	ownedLayout := false
	if layout != nil {
		if !layout.Valid() {
			_ = buf.Close()
			return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid probe IBL bind group layout"}
		}
	} else {
		layout, err = ProbeIBLBindGroupLayout(cx, dev)
		if err != nil {
			_ = buf.Close()
			return nil, err
		}
		ownedLayout = true
	}

	black, err := newBlackFallbackCubemap(cx, dev, label, opts.Retained)
	if err != nil {
		if ownedLayout {
			_ = layout.Close()
		}
		_ = buf.Close()
		return nil, err
	}

	b.buf = buf
	b.layout = layout
	b.ownedLayout = ownedLayout
	b.fallbackBlack = black

	if err := b.rebindTextures(dev, black.Texture(), black.Texture(), black.Texture()); err != nil {
		_ = b.Close()
		return nil, err
	}
	return b, nil
}

func (b *GPUProbeIBLBuffer) rebindTextures(dev *gpu.Device, tex0, tex1, texSky *gpu.Texture) error {
	if b.group != nil && b.group.Valid() && b.boundTex0 == tex0 && b.boundTex1 == tex1 && b.boundSky == texSky {
		return nil
	}
	newGroup, err := dev.CreateBindGroupDesc(b.cx, gpu.BindGroupDesc{
		Label:  b.label,
		Layout: b.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: b.buf, Size: ProbeIBLUniformSize},
			{Binding: 1, Texture: tex0},
			{Binding: 2, Texture: tex1},
			{Binding: 3, Texture: texSky},
			{Binding: 4, Sampler: b.fallbackBlack.Sampler()},
		},
	})
	if err != nil {
		return err
	}
	old := b.group
	b.group = newGroup
	b.boundTex0 = tex0
	b.boundTex1 = tex1
	b.boundSky = texSky
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// Update validates u, writes it to the uniform buffer, and updates the bound cubemaps when any of probe0, probe1, or sky changes. Any nil or invalid cubemap binds the 1x1 black fallback cubemap so GPU sampling returns (0, 0, 0). Pass the active [*gpu.Device] (or nil to reuse the last device).
func (b *GPUProbeIBLBuffer) Update(dev *gpu.Device, u ProbeIBLUniform, probe0, probe1, sky *GPUCubemap) error {
	if err := u.Validate(); err != nil {
		return err
	}
	if b == nil || !b.buf.Valid() || !b.layout.Valid() || !b.fallbackBlack.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: probe IBL buffer is not valid"}
	}
	target := dev
	if target == nil {
		target = b.dev
	} else {
		b.dev = dev
	}
	if target == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	pickTex := func(c *GPUCubemap) *gpu.Texture {
		if c != nil && c.Valid() {
			return c.Texture()
		}
		return b.fallbackBlack.Texture()
	}
	if err := b.rebindTextures(target, pickTex(probe0), pickTex(probe1), pickTex(sky)); err != nil {
		return err
	}
	target.WriteBuffer(b.buf, 0, u.Encode(b.scratch[:]))
	return nil
}

// UpdateBlend builds a [ProbeIBLUniform] from sh, blend, sky, and intensity and uploads it to b, binding blend's ready primary and secondary probe cubemaps (or the black fallback when nil/unbaked/dirty) and sky.
func (b *GPUProbeIBLBuffer) UpdateBlend(dev *gpu.Device, sh IrradianceSH, blend ProbeBlendResult, sky *GPUCubemap, intensity float32) error {
	u := NewProbeIBLUniform(sh, blend, sky, intensity)
	return b.Update(dev, u, blend.Primary.gpuCubemap(), blend.Secondary.gpuCubemap(), sky)
}

// UpdateForPoint selects reflection probes from reflectionProbes and samples diffuse [IrradianceSH] from lightProbes at worldPos, then updates b with the blended probe IBL state.
func (b *GPUProbeIBLBuffer) UpdateForPoint(
	dev *gpu.Device,
	worldPos math3d.Vec3,
	reflectionProbes *ReflectionProbeSet,
	lightProbes *LightProbeVolume,
	sky *GPUCubemap,
	intensity float32,
) error {
	blend := reflectionProbes.SelectReflectionProbes(worldPos)
	sh := lightProbes.SampleLightProbes(worldPos)
	return b.UpdateBlend(dev, sh, blend, sky, intensity)
}

// Bind sets b's bind group at [IBLGroup] (group 3) on pass. Records nothing when b is not valid.
func (b *GPUProbeIBLBuffer) Bind(pass *gpu.RenderPass) {
	if pass == nil || !b.Valid() {
		return
	}
	pass.SetBindGroup(IBLGroup, b.group)
}

// Buffer returns the underlying uniform buffer, or nil for a nil receiver.
func (b *GPUProbeIBLBuffer) Buffer() *gpu.Buffer {
	if b == nil {
		return nil
	}
	return b.buf
}

// Layout returns the [IBLGroup] bind group layout for pipelines using [ProbeIBLWGSL] or [LitPBRProbeIBLWGSL].
func (b *GPUProbeIBLBuffer) Layout() *gpu.BindGroupLayout {
	if b == nil {
		return nil
	}
	return b.layout
}

// BindGroup returns the current [gpu.BindGroup] for [IBLGroup], or nil for a nil receiver.
func (b *GPUProbeIBLBuffer) BindGroup() *gpu.BindGroup {
	if b == nil {
		return nil
	}
	return b.group
}

// ByteSize returns the GPU memory held by b's uniform buffer and 1x1 black fallback cubemap.
func (b *GPUProbeIBLBuffer) ByteSize() uint64 {
	if b == nil {
		return 0
	}
	var total uint64
	if b.buf != nil {
		total += b.buf.Size()
	}
	if b.fallbackBlack != nil {
		total += b.fallbackBlack.ByteSize()
	}
	return total
}

// Valid reports whether the uniform buffer, layout, fallback cubemap, and bind group are open on a live device.
func (b *GPUProbeIBLBuffer) Valid() bool {
	return b != nil && b.buf.Valid() && b.layout.Valid() && b.fallbackBlack.Valid() && b.group.Valid()
}

// Close releases the bind group, fallback cubemap, owned layout (when not shared), and uniform buffer. Safe to call on nil or more than once.
func (b *GPUProbeIBLBuffer) Close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.group != nil {
		err = errors.Join(err, b.group.Close())
	}
	if b.fallbackBlack != nil {
		err = errors.Join(err, b.fallbackBlack.Close())
	}
	if b.ownedLayout && b.layout != nil {
		err = errors.Join(err, b.layout.Close())
	}
	if b.buf != nil {
		err = errors.Join(err, b.buf.Close())
	}
	return err
}

// ProbeIBLPipelineOptions configures [NewLitPBRProbeIBLPipelineWithOptions].
type ProbeIBLPipelineOptions struct {
	// Label names the pipeline in GPU capture tools; empty picks render3d.lit_pbr_probe_ibl (with .linear for LinearOutput).
	Label string
	// ColorFormat sets the color attachment format; [gpu.FormatUndefined] defaults to dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DepthFormat enables depth testing and writes when not [gpu.FormatUndefined].
	DepthFormat gpu.TextureFormat
	// LinearOutput writes unclamped linear radiance without sRGB encoding for drawing into an [HDRTarget] resolved by [ToneMapPass].
	LinearOutput bool
}

// NewLitPBRProbeIBLPipeline compiles [LitPBRProbeIBLWGSL] and creates a back-face-culled lit PBR pipeline configured like [NewLitPBRIBLPipeline] with probeIBLLayout ([GPUProbeIBLBuffer.Layout] or [ProbeIBLBindGroupLayout]) at [IBLGroup] (group 3).
func NewLitPBRProbeIBLPipeline(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, probeIBLLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	return NewLitPBRProbeIBLPipelineWithOptions(cx, dev, camLayout, lightLayout, drawLayout, probeIBLLayout, ProbeIBLPipelineOptions{
		DepthFormat: depthFormat,
	})
}

// NewLitPBRProbeIBLPipelineWithOptions compiles [LitPBRProbeIBLWGSL] (or its linear HDR variant when opts.LinearOutput is set) and creates a lit PBR pipeline with probeIBLLayout at [IBLGroup] (group 3).
func NewLitPBRProbeIBLPipelineWithOptions(cx *core.Scope, dev *gpu.Device, camLayout, lightLayout, drawLayout, probeIBLLayout *gpu.BindGroupLayout, opts ProbeIBLPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if lightLayout == nil || !lightLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid light bind group layout"}
	}
	if drawLayout == nil || !drawLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid PBR draw bind group layout"}
	}
	if probeIBLLayout == nil || !probeIBLLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid probe IBL bind group layout"}
	}
	label := opts.Label
	src := LitPBRProbeIBLWGSL
	if opts.LinearOutput {
		src = litPBRProbeIBLLinearWGSL
	}
	if label == "" {
		label = "render3d.lit_pbr_probe_ibl"
		if opts.LinearOutput {
			label += ".linear"
		}
	}
	colorFmt := opts.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	layouts := make([]*gpu.BindGroupLayout, IBLGroup+1)
	layouts[CameraGroup] = camLayout
	layouts[LightGroup] = lightLayout
	layouts[PBRDrawGroup] = drawLayout
	layouts[IBLGroup] = probeIBLLayout
	return newLitPBRPipeline(cx, dev, label, src, layouts, MeshVertexLayout(), colorFmt, opts.DepthFormat)
}
