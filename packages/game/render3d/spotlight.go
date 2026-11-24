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

const (
	// MaxSpotLights is the maximum number of active spot lights packed into a single [GPUSpotLights] uniform buffer. Using a fixed uniform block rather than a storage buffer keeps spot lighting portable across every NSGo GPU backend, including WebGL 2 and OpenGL ES 3.0.
	MaxSpotLights = 8

	// SpotLightStride is the byte size of one packed spot light in [SpotLightsWGSL] (three vec4<f32> fields, 16-byte aligned).
	SpotLightStride = 48

	// SpotLightUniformSize is the total byte size of the [SpotLightsWGSL] uniform block: a vec4 ambient/count header (16 bytes) followed by [MaxSpotLights] packed lights (400 bytes total).
	SpotLightUniformSize = 16 + MaxSpotLights*SpotLightStride

	// SpotMaterialUniformSize is the total byte size of the [SpotLitWGSL] material uniform block: model matrix (64 bytes), three normal-matrix/PBR vec4s (48 bytes), base color (16 bytes) and emissive (16 bytes) — 144 bytes total.
	SpotMaterialUniformSize = 64 + 3*16 + 16 + 16

	// SpotLightsGroup is the default bind group index [SpotLightsWGSL] and [SpotLitWGSL] reserve for the [GPUSpotLights] uniform buffer alongside [CameraGroup] at group 0.
	SpotLightsGroup = 1

	// SpotMaterialGroup is the bind group index [SpotLitWGSL] reserves for the per-draw [GPUSpotMaterial] uniform buffer.
	SpotMaterialGroup = 2

	spotLightMinRoughness   = 0.045
	spotLightMinDistSq      = 0.0001
	spotLightMinCosDelta    = 0.0001
	spotLightMaxConeRadians = float32(math.Pi / 2)
	spotLightPi             = float32(math.Pi)
	spotLightInvPi          = float32(1.0 / math.Pi)
	spotLightInvGamma       = float32(1.0 / 2.2)
)

// ErrInvalidSpotLight is wrapped by every error [SpotLight.Validate], [SpotLightSet.Validate], [SpotMaterial.Validate], [GPUSpotLights.Update], [NewGPUSpotMaterial] and [GPUSpotMaterial.Update] return for malformed light or material parameters.
var ErrInvalidSpotLight = errors.New("render3d: invalid spot light")

// SpotLightsWGSL declares the spot light uniform block at @group(1) @binding(0) together with portable glTF KHR_lights_punctual attenuation and Cook-Torrance PBR shading helpers.
//
// Every field is vec4<f32> so the layout is identical across WGSL, Metal, Direct3D 11, OpenGL 3.3, WebGL 2, OpenGL ES 3.0 and NSGo's software GPU renderer. All math uses algebraic operations, sqrt and pow (avoiding exp/exp2/log2) for full software-VM parity.
const SpotLightsWGSL = `struct SpotLightGPU {
	pos_range: vec4<f32>,
	dir_scale: vec4<f32>,
	color_offset: vec4<f32>,
}

struct SpotLightBlock {
	ambient_count: vec4<f32>,
	lights: array<SpotLightGPU, 8>,
}

@group(1) @binding(0) var<uniform> spot_lights: SpotLightBlock;

fn spot_distance_attenuation(dist: f32, inv_range: f32) -> f32 {
	let dist_sq = max(dist * dist, 0.0001);
	if (inv_range <= 0.0) {
		return 1.0 / dist_sq;
	}
	let ratio = dist * inv_range;
	let ratio2 = ratio * ratio;
	let w = clamp(1.0 - ratio2 * ratio2, 0.0, 1.0);
	return (w * w) / dist_sq;
}

fn spot_cone_attenuation(spot_dir: vec3<f32>, to_light: vec3<f32>, angle_scale: f32, angle_offset: f32) -> f32 {
	let cd = dot(spot_dir, -to_light);
	let t = clamp(cd * angle_scale + angle_offset, 0.0, 1.0);
	return t * t;
}

fn spot_pbr_contribution(
	n: vec3<f32>,
	v: vec3<f32>,
	l: vec3<f32>,
	radiance: vec3<f32>,
	diffuse_color: vec3<f32>,
	f0: vec3<f32>,
	alpha2: f32,
	ndotv: f32,
) -> vec3<f32> {
	let ndotl = max(dot(n, l), 0.0);
	if (ndotl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let h = normalize(v + l);
	let ndoth = max(dot(n, h), 0.0);
	let vdoth = max(dot(v, h), 0.0);

	let denom_d = ndoth * ndoth * (alpha2 - 1.0) + 1.0;
	let d = alpha2 / (3.14159265 * denom_d * denom_d);

	let ggxv = ndotl * sqrt(ndotv * ndotv * (1.0 - alpha2) + alpha2);
	let ggxl = ndotv * sqrt(ndotl * ndotl * (1.0 - alpha2) + alpha2);
	let vis = 0.5 / max(ggxv + ggxl, 0.0001);

	let om = clamp(1.0 - vdoth, 0.0, 1.0);
	let om2 = om * om;
	let om5 = om2 * om2 * om;
	let f = f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * om5;

	let spec = f * (d * vis);
	let diff = (vec3<f32>(1.0, 1.0, 1.0) - f) * diffuse_color * 0.31830989;
	return (diff + spec) * radiance * ndotl;
}

fn spot_shade_pbr(
	world_pos: vec3<f32>,
	n: vec3<f32>,
	v: vec3<f32>,
	base_color: vec3<f32>,
	metallic: f32,
	roughness: f32,
	emissive: vec3<f32>,
) -> vec3<f32> {
	let m = clamp(metallic, 0.0, 1.0);
	let r = clamp(roughness, 0.045, 1.0);
	let alpha = r * r;
	let alpha2 = alpha * alpha;
	let ndotv = max(dot(n, v), 0.0001);
	let f0 = vec3<f32>(0.04, 0.04, 0.04) * (1.0 - m) + base_color * m;
	let diffuse_color = base_color * (1.0 - m);

	var lo = vec3<f32>(0.0, 0.0, 0.0);
	let count = u32(spot_lights.ambient_count.w);
	for (var i: u32 = 0u; i < 8u; i = i + 1u) {
		if (i >= count) {
			break;
		}
		let light = spot_lights.lights[i];
		let to_light = light.pos_range.xyz - world_pos;
		let dist = length(to_light);
		if (dist > 0.00001) {
			let l = to_light / dist;
			let dist_att = spot_distance_attenuation(dist, light.pos_range.w);
			let cone_att = spot_cone_attenuation(light.dir_scale.xyz, l, light.dir_scale.w, light.color_offset.w);
			let att = dist_att * cone_att;
			if (att > 0.0) {
				let rad = light.color_offset.xyz * att;
				lo = lo + spot_pbr_contribution(n, v, l, rad, diffuse_color, f0, alpha2, ndotv);
			}
		}
	}
	return emissive + spot_lights.ambient_count.xyz * diffuse_color + lo;
}

fn spot_tonemap_srgb(linear_rgb: vec3<f32>) -> vec3<f32> {
	let c = max(linear_rgb, vec3<f32>(0.0, 0.0, 0.0));
	let mapped = c / (c + vec3<f32>(1.0, 1.0, 1.0));
	let gamma = vec3<f32>(0.45454545, 0.45454545, 0.45454545);
	return clamp(pow(mapped, gamma), vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(1.0, 1.0, 1.0));
}
`

// SpotLitWGSL is a complete portable WGSL shader module for drawing [MeshVertexLayout] meshes lit by [GPUSpotLights] with Cook-Torrance metallic-roughness PBR shading and Reinhard + 2.2 gamma display output.
//
// Bindings:
//   - @group(0) @binding(0): [CameraBuffer] ([CameraWGSL])
//   - @group(1) @binding(0): [GPUSpotLights] ([SpotLightsWGSL])
//   - @group(2) @binding(0): [GPUSpotMaterial]
//
// Entry points are "vs_main" and "fs_main".
const SpotLitWGSL = CameraWGSL + "\n" + SpotLightsWGSL + `
struct SpotMaterialBlock {
	model: mat4x4<f32>,
	normal_mat_0: vec4<f32>,
	normal_mat_1: vec4<f32>,
	normal_mat_2: vec4<f32>,
	base_color: vec4<f32>,
	emissive: vec4<f32>,
}

@group(2) @binding(0) var<uniform> spot_mat: SpotMaterialBlock;

struct SpotVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct SpotVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: SpotVertexIn) -> SpotVertexOut {
	var out: SpotVertexOut;
	let world = spot_mat.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = spot_mat.normal_mat_0.xyz * in.normal.x +
		spot_mat.normal_mat_1.xyz * in.normal.y +
		spot_mat.normal_mat_2.xyz * in.normal.z;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: SpotVertexOut) -> @location(0) vec4<f32> {
	let n = normalize(in.world_normal);
	let v = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let linear_rgb = spot_shade_pbr(
		in.world_pos,
		n,
		v,
		spot_mat.base_color.xyz,
		spot_mat.normal_mat_0.w,
		spot_mat.normal_mat_1.w,
		spot_mat.emissive.xyz,
	);
	let srgb = spot_tonemap_srgb(linear_rgb);
	return vec4<f32>(srgb, spot_mat.base_color.w);
}
`

// SpotLight describes a punctual cone-shaped light source following glTF KHR_lights_punctual conventions.
//
// Direction points away from the light along the center of its emission cone (it does not need to be unit length, as packing and CPU evaluation normalize it). Range <= 0 selects infinite range with pure inverse-square falloff; Range > 0 applies the smooth glTF window max(min(1 - (d/Range)^4, 1), 0)^2 / max(d^2, 0.0001). InnerConeAngle and OuterConeAngle are half-angles in radians satisfying 0 <= InnerConeAngle <= OuterConeAngle <= pi/2.
type SpotLight struct {
	Position       math3d.Vec3
	Direction      math3d.Vec3
	Color          [3]float32
	Intensity      float32
	Range          float32
	InnerConeAngle float32
	OuterConeAngle float32
}

// Validate checks that l has finite fields, a non-zero emission direction, non-negative color and intensity, and valid cone half-angles (0 <= InnerConeAngle <= OuterConeAngle <= pi/2). Every error wraps [ErrInvalidSpotLight].
func (l SpotLight) Validate() error {
	if !finite3(l.Position) {
		return fmt.Errorf("%w: position is not finite", ErrInvalidSpotLight)
	}
	if !finite3(l.Direction) {
		return fmt.Errorf("%w: direction is not finite", ErrInvalidSpotLight)
	}
	if l.Direction.LenSq() <= 1e-12 {
		return fmt.Errorf("%w: direction must be non-zero", ErrInvalidSpotLight)
	}
	for i, c := range l.Color {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: color[%d] (%v) must be finite and non-negative", ErrInvalidSpotLight, i, c)
		}
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: intensity (%v) must be finite and non-negative", ErrInvalidSpotLight, l.Intensity)
	}
	if !finite(l.Range) {
		return fmt.Errorf("%w: range (%v) must be finite", ErrInvalidSpotLight, l.Range)
	}
	if !finite(l.InnerConeAngle) || l.InnerConeAngle < 0 {
		return fmt.Errorf("%w: inner cone angle (%v) must be finite and >= 0", ErrInvalidSpotLight, l.InnerConeAngle)
	}
	if !finite(l.OuterConeAngle) || l.OuterConeAngle < 0 || l.OuterConeAngle > spotLightMaxConeRadians {
		return fmt.Errorf("%w: outer cone angle (%v) must be in [0, pi/2]", ErrInvalidSpotLight, l.OuterConeAngle)
	}
	if l.InnerConeAngle > l.OuterConeAngle {
		return fmt.Errorf("%w: inner cone angle (%v) exceeds outer cone angle (%v)", ErrInvalidSpotLight, l.InnerConeAngle, l.OuterConeAngle)
	}
	return nil
}

// ConeParams returns the precomputed glTF KHR_lights_punctual angular attenuation terms (cosInner, cosOuter, angleScale, angleOffset) for l.
func (l SpotLight) ConeParams() (cosInner, cosOuter, angleScale, angleOffset float32) {
	cosInner = float32(math.Cos(float64(l.InnerConeAngle)))
	cosOuter = float32(math.Cos(float64(l.OuterConeAngle)))
	delta := cosInner - cosOuter
	if delta < spotLightMinCosDelta {
		delta = spotLightMinCosDelta
	}
	angleScale = 1.0 / delta
	angleOffset = -cosOuter * angleScale
	return cosInner, cosOuter, angleScale, angleOffset
}

// DistanceAttenuation returns the glTF KHR_lights_punctual distance falloff at distance world units from l.
func (l SpotLight) DistanceAttenuation(distance float32) float32 {
	if distance < 0 {
		distance = -distance
	}
	invRange := float32(0)
	if l.Range > 0 {
		invRange = 1.0 / l.Range
	}
	return spotDistanceAttenuation(distance, invRange)
}

// ConeAttenuation returns the smooth glTF KHR_lights_punctual angular attenuation factor in [0, 1] for a surface point at worldPos. Points inside InnerConeAngle return 1, points between InnerConeAngle and OuterConeAngle fall off smoothly as t^2, and points outside OuterConeAngle or behind the light return 0.
func (l SpotLight) ConeAttenuation(worldPos math3d.Vec3) float32 {
	if l.Direction.LenSq() <= 1e-12 {
		return 0
	}
	toLight := l.Position.Sub(worldPos)
	dist := toLight.Len()
	if dist <= 1e-5 {
		return 0
	}
	dir := l.Direction.Norm()
	lVec := toLight.Scale(1.0 / dist)
	_, _, angleScale, angleOffset := l.ConeParams()
	return spotConeAttenuation(dir, lVec, angleScale, angleOffset)
}

// SpotLightSample is the incident illumination from one [SpotLight] evaluated at a world-space point by [EvaluateSpotLight].
type SpotLightSample struct {
	// Direction is the unit vector pointing from the surface toward the light source (L).
	Direction math3d.Vec3
	// Distance is the Euclidean distance from the surface point to the light position.
	Distance float32
	// DistanceAttenuation is the inverse-square distance falloff with optional smooth range windowing.
	DistanceAttenuation float32
	// ConeAttenuation is the smooth angular cone falloff in [0, 1].
	ConeAttenuation float32
	// Attenuation is DistanceAttenuation * ConeAttenuation.
	Attenuation float32
	// Radiance is the attenuated incident linear RGB radiance (Color * Intensity * Attenuation).
	Radiance [3]float32
}

// EvaluateSpotLight evaluates l at worldPos using the exact same glTF KHR_lights_punctual distance and angular attenuation formulas as [SpotLightsWGSL].
func EvaluateSpotLight(l SpotLight, worldPos math3d.Vec3) SpotLightSample {
	if l.Direction.LenSq() <= 1e-12 {
		return SpotLightSample{}
	}
	toLight := l.Position.Sub(worldPos)
	dist := toLight.Len()
	if dist <= 1e-5 {
		return SpotLightSample{}
	}
	lVec := toLight.Scale(1.0 / dist)
	invRange := float32(0)
	if l.Range > 0 {
		invRange = 1.0 / l.Range
	}
	distAtt := spotDistanceAttenuation(dist, invRange)
	dir := l.Direction.Norm()
	_, _, angleScale, angleOffset := l.ConeParams()
	coneAtt := spotConeAttenuation(dir, lVec, angleScale, angleOffset)
	att := distAtt * coneAtt
	return SpotLightSample{
		Direction:           lVec,
		Distance:            dist,
		DistanceAttenuation: distAtt,
		ConeAttenuation:     coneAtt,
		Attenuation:         att,
		Radiance: [3]float32{
			l.Color[0] * l.Intensity * att,
			l.Color[1] * l.Intensity * att,
			l.Color[2] * l.Intensity * att,
		},
	}
}

// SpotLightSet holds the scene's linear ambient color and up to [MaxSpotLights] active spot lights.
type SpotLightSet struct {
	Ambient [3]float32
	Lights  []SpotLight
}

// Validate checks that Ambient is finite and non-negative, len(Lights) <= [MaxSpotLights], and every light passes [SpotLight.Validate]. Every error wraps [ErrInvalidSpotLight].
func (s SpotLightSet) Validate() error {
	for i, c := range s.Ambient {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: ambient[%d] (%v) must be finite and non-negative", ErrInvalidSpotLight, i, c)
		}
	}
	if len(s.Lights) > MaxSpotLights {
		return fmt.Errorf("%w: %d spot lights exceed maximum %d", ErrInvalidSpotLight, len(s.Lights), MaxSpotLights)
	}
	for i, l := range s.Lights {
		if err := l.Validate(); err != nil {
			return fmt.Errorf("light %d: %w", i, err)
		}
	}
	return nil
}

// AppendSpotLightUniforms appends [SpotLightUniformSize] (400) little-endian bytes for set matching [SpotLightsWGSL] and returns the extended slice. It allocates only when dst lacks capacity for [SpotLightUniformSize] additional bytes. Call [SpotLightSet.Validate] first; lights beyond [MaxSpotLights] are ignored.
func AppendSpotLightUniforms(dst []byte, set SpotLightSet) []byte {
	start := len(dst)
	dst = grow(dst, SpotLightUniformSize)
	out := dst[start:]
	clear(out)

	count := min(len(set.Lights), MaxSpotLights)
	putSpotVec4(out[0:16], set.Ambient[0], set.Ambient[1], set.Ambient[2], float32(count))

	for i := range count {
		l := set.Lights[i]
		invRange := float32(0)
		if l.Range > 0 {
			invRange = 1.0 / l.Range
		}
		dir := l.Direction.Norm()
		_, _, angleScale, angleOffset := l.ConeParams()

		base := 16 + i*SpotLightStride
		putSpotVec4(out[base+0:base+16], l.Position.X, l.Position.Y, l.Position.Z, invRange)
		putSpotVec4(out[base+16:base+32], dir.X, dir.Y, dir.Z, angleScale)
		putSpotVec4(out[base+32:base+48],
			l.Color[0]*l.Intensity,
			l.Color[1]*l.Intensity,
			l.Color[2]*l.Intensity,
			angleOffset,
		)
	}
	return dst
}

// SpotMaterial holds the model transform and metallic-roughness PBR parameters for one draw shaded by [SpotLitWGSL] and [EvaluateSpotPBR].
//
// A zero Model matrix is treated as [math3d.Mat4Identity]. A zero Alpha is packed as 1.0 so opaque materials do not need to set Alpha explicitly.
type SpotMaterial struct {
	Model     math3d.Mat4
	BaseColor [3]float32
	Alpha     float32
	Metallic  float32
	Roughness float32
	Emissive  [3]float32
}

// DefaultSpotMaterial returns an opaque white dielectric PBR material with an identity model matrix and roughness 0.5.
func DefaultSpotMaterial() SpotMaterial {
	return SpotMaterial{
		Model:     math3d.Mat4Identity(),
		BaseColor: [3]float32{1, 1, 1},
		Alpha:     1,
		Metallic:  0,
		Roughness: 0.5,
	}
}

// Validate checks that every field of m is finite and within physically valid bounds (BaseColor and Emissive non-negative, Alpha/Metallic/Roughness in [0, 1]). Every error wraps [ErrInvalidSpotLight].
func (m SpotMaterial) Validate() error {
	for i, f := range m.Model {
		if !finite(f) {
			return fmt.Errorf("%w: model[%d] is not finite", ErrInvalidSpotLight, i)
		}
	}
	for i, c := range m.BaseColor {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: base color[%d] (%v) must be finite and non-negative", ErrInvalidSpotLight, i, c)
		}
	}
	if !finite(m.Alpha) || m.Alpha < 0 || m.Alpha > 1 {
		return fmt.Errorf("%w: alpha (%v) must be in [0, 1]", ErrInvalidSpotLight, m.Alpha)
	}
	if !finite(m.Metallic) || m.Metallic < 0 || m.Metallic > 1 {
		return fmt.Errorf("%w: metallic (%v) must be in [0, 1]", ErrInvalidSpotLight, m.Metallic)
	}
	if !finite(m.Roughness) || m.Roughness < 0 || m.Roughness > 1 {
		return fmt.Errorf("%w: roughness (%v) must be in [0, 1]", ErrInvalidSpotLight, m.Roughness)
	}
	for i, c := range m.Emissive {
		if !finite(c) || c < 0 {
			return fmt.Errorf("%w: emissive[%d] (%v) must be finite and non-negative", ErrInvalidSpotLight, i, c)
		}
	}
	return nil
}

// AppendSpotMaterialUniforms appends [SpotMaterialUniformSize] (144) little-endian bytes for mat matching [SpotLitWGSL] and returns the extended slice. It allocates only when dst lacks capacity for [SpotMaterialUniformSize] additional bytes.
func AppendSpotMaterialUniforms(dst []byte, mat SpotMaterial) []byte {
	start := len(dst)
	dst = grow(dst, SpotMaterialUniformSize)
	out := dst[start:]

	model := mat.Model
	if model == (math3d.Mat4{}) {
		model = math3d.Mat4Identity()
	}
	for i, f := range model {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}

	n0, n1, n2 := spotNormalMatrixColumns(model)
	metallic := clampUnit(mat.Metallic)
	roughness := clampFloat32(mat.Roughness, spotLightMinRoughness, 1)
	alpha := mat.Alpha
	if alpha <= 0 {
		alpha = 1
	}

	putSpotVec4(out[64:80], n0.X, n0.Y, n0.Z, metallic)
	putSpotVec4(out[80:96], n1.X, n1.Y, n1.Z, roughness)
	putSpotVec4(out[96:112], n2.X, n2.Y, n2.Z, 0)
	putSpotVec4(out[112:128], mat.BaseColor[0], mat.BaseColor[1], mat.BaseColor[2], alpha)
	putSpotVec4(out[128:144], mat.Emissive[0], mat.Emissive[1], mat.Emissive[2], 0)
	return dst
}

// EvaluateSpotPBR evaluates the Cook-Torrance metallic-roughness PBR shading model (GGX NDF, height-correlated Smith-GGX visibility, Schlick Fresnel, energy-conserving Lambertian diffuse, ambient and emissive) for a surface point illuminated by set.
//
// It returns linear HDR RGB radiance matching spot_shade_pbr in [SpotLightsWGSL]. Apply [SpotToneMapSRGB] to compare directly against RGBA8Unorm pixels rendered by [SpotLitWGSL].
func EvaluateSpotPBR(worldPos, worldNormal, viewDir math3d.Vec3, mat SpotMaterial, set SpotLightSet) [3]float32 {
	n := worldNormal.Norm()
	v := viewDir.Norm()
	m := clampUnit(mat.Metallic)
	r := clampFloat32(mat.Roughness, spotLightMinRoughness, 1)
	alpha := r * r
	alpha2 := alpha * alpha
	ndotv := max(n.Dot(v), 0.0001)

	f0 := [3]float32{
		0.04*(1-m) + mat.BaseColor[0]*m,
		0.04*(1-m) + mat.BaseColor[1]*m,
		0.04*(1-m) + mat.BaseColor[2]*m,
	}
	diffuseColor := [3]float32{
		mat.BaseColor[0] * (1 - m),
		mat.BaseColor[1] * (1 - m),
		mat.BaseColor[2] * (1 - m),
	}

	var lo [3]float32
	count := min(len(set.Lights), MaxSpotLights)
	for i := range count {
		sample := EvaluateSpotLight(set.Lights[i], worldPos)
		if sample.Attenuation <= 0 {
			continue
		}
		l := sample.Direction
		ndotl := max(n.Dot(l), 0)
		if ndotl <= 0 {
			continue
		}
		h := v.Add(l).Norm()
		ndoth := max(n.Dot(h), 0)
		vdoth := max(v.Dot(h), 0)

		denomD := ndoth*ndoth*(alpha2-1) + 1
		d := alpha2 / (spotLightPi * denomD * denomD)

		ggxv := ndotl * float32(math.Sqrt(float64(ndotv*ndotv*(1-alpha2)+alpha2)))
		ggxl := ndotv * float32(math.Sqrt(float64(ndotl*ndotl*(1-alpha2)+alpha2)))
		vis := 0.5 / max(ggxv+ggxl, 0.0001)

		om := clampUnit(1 - vdoth)
		om2 := om * om
		om5 := om2 * om2 * om

		for ch := range 3 {
			f := f0[ch] + (1-f0[ch])*om5
			spec := f * (d * vis)
			diff := (1 - f) * diffuseColor[ch] * spotLightInvPi
			lo[ch] += (diff + spec) * sample.Radiance[ch] * ndotl
		}
	}

	return [3]float32{
		mat.Emissive[0] + set.Ambient[0]*diffuseColor[0] + lo[0],
		mat.Emissive[1] + set.Ambient[1]*diffuseColor[1] + lo[1],
		mat.Emissive[2] + set.Ambient[2]*diffuseColor[2] + lo[2],
	}
}

// SpotToneMapSRGB applies Reinhard tone mapping (c / (c + 1)) and 2.2 display gamma to linear HDR RGB radiance, matching spot_tonemap_srgb in [SpotLightsWGSL].
func SpotToneMapSRGB(linear [3]float32) [3]float32 {
	var out [3]float32
	for ch := range 3 {
		c := max(linear[ch], 0)
		mapped := c / (c + 1)
		out[ch] = clampUnit(float32(math.Pow(float64(mapped), float64(spotLightInvGamma))))
	}
	return out
}

// GPUSpotLightsOptions configures [NewGPUSpotLights], [NewGPUSpotMaterial] and [NewSpotLitRenderer].
type GPUSpotLightsOptions struct {
	// Label names GPU resources in capture tools.
	Label string
	// Retained keeps a CPU copy of created uniform buffers so NSGo recreates them with their latest contents after a GPU device loss.
	Retained bool
}

// GPUSpotLights holds a [SpotLightSet] uniform buffer, its bind group layout, and its bind group on an NSGo [gpu.Device] for binding at [SpotLightsGroup] (or any group slot matching [SpotLightsWGSL]).
//
// Pass the live [gpu.Device] to [GPUSpotLights.Update] so retained buffers remain updatable after a GPU device loss and restore.
type GPUSpotLights struct {
	buf     *gpu.Buffer
	layout  *gpu.BindGroupLayout
	group   *gpu.BindGroup
	scratch [SpotLightUniformSize]byte
	count   int
}

// NewGPUSpotLights creates a spot-light uniform buffer (initialized to zero lights and zero ambient), its bind group layout and its bind group on dev, scoped to cx.
func NewGPUSpotLights(cx *core.Scope, dev *gpu.Device, opts GPUSpotLightsOptions) (*GPUSpotLights, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.spot_lights"
	}
	var zeros [SpotLightUniformSize]byte
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     zeros[:],
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " group",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  buf,
			Size:    SpotLightUniformSize,
		}},
	})
	if err != nil {
		_ = layout.Close()
		_ = buf.Close()
		return nil, err
	}
	return &GPUSpotLights{
		buf:    buf,
		layout: layout,
		group:  group,
	}, nil
}

// Update validates set and uploads its packed uniform bytes to dev without heap allocations. An invalid set returns an error wrapping [ErrInvalidSpotLight] before touching the GPU; a nil device or invalid receiver returns [gpu.ErrDeviceLost].
func (g *GPUSpotLights) Update(dev *gpu.Device, set SpotLightSet) error {
	if err := set.Validate(); err != nil {
		return err
	}
	if dev == nil || !g.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: spot light buffer is not valid"}
	}
	data := AppendSpotLightUniforms(g.scratch[:0], set)
	dev.WriteBuffer(g.buf, 0, data)
	g.count = len(set.Lights)
	return nil
}

// Buffer returns the underlying uniform buffer, or nil for a nil receiver.
func (g *GPUSpotLights) Buffer() *gpu.Buffer {
	if g == nil {
		return nil
	}
	return g.buf
}

// Layout returns the bind group layout for pipelines using [SpotLightsWGSL], or nil for a nil receiver.
func (g *GPUSpotLights) Layout() *gpu.BindGroupLayout {
	if g == nil {
		return nil
	}
	return g.layout
}

// BindGroup returns the bind group to bind at [SpotLightsGroup], or nil for a nil receiver.
func (g *GPUSpotLights) BindGroup() *gpu.BindGroup {
	if g == nil {
		return nil
	}
	return g.group
}

// Count returns the number of active spot lights uploaded by the most recent [GPUSpotLights.Update].
func (g *GPUSpotLights) Count() int {
	if g == nil {
		return 0
	}
	return g.count
}

// ByteSize returns the GPU uniform buffer size in bytes, or zero for a nil receiver.
func (g *GPUSpotLights) ByteSize() uint64 {
	if g == nil || g.buf == nil {
		return 0
	}
	return g.buf.Size()
}

// Valid reports whether the spot-light uniform buffer, layout and bind group are open and belong to a live device.
func (g *GPUSpotLights) Valid() bool {
	return g != nil && g.buf.Valid() && g.layout.Valid() && g.group.Valid()
}

// Close releases the bind group, layout and uniform buffer now instead of waiting for scope disposal. It is safe to call on a nil receiver or more than once.
func (g *GPUSpotLights) Close() error {
	if g == nil {
		return nil
	}
	var err error
	if g.group != nil {
		err = errors.Join(err, g.group.Close())
	}
	if g.layout != nil {
		err = errors.Join(err, g.layout.Close())
	}
	if g.buf != nil {
		err = errors.Join(err, g.buf.Close())
	}
	return err
}

// SpotMaterialBindingLayout creates the bind group layout for @group(2) of [SpotLitWGSL] (one vertex+fragment uniform buffer at binding 0).
func SpotMaterialBindingLayout(cx *core.Scope, dev *gpu.Device) (*gpu.BindGroupLayout, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	return dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.spot_material layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
}

// GPUSpotMaterial holds one draw's [SpotMaterial] uniform buffer and bind group for @group(2) of [SpotLitWGSL].
//
// Pass the live [gpu.Device] to [GPUSpotMaterial.Update] so retained materials remain updatable after a GPU device loss and restore.
type GPUSpotMaterial struct {
	buf     *gpu.Buffer
	group   *gpu.BindGroup
	scratch [SpotMaterialUniformSize]byte
}

// NewGPUSpotMaterial validates mat and uploads it into a uniform buffer and bind group matching layout (created by [SpotMaterialBindingLayout]).
func NewGPUSpotMaterial(cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, mat SpotMaterial, opts GPUSpotLightsOptions) (*GPUSpotMaterial, error) {
	if err := mat.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if layout == nil || !layout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid spot material bind group layout"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.spot_material"
	}
	var initData [SpotMaterialUniformSize]byte
	packed := AppendSpotMaterialUniforms(initData[:0], mat)
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     packed,
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " group",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  buf,
			Size:    SpotMaterialUniformSize,
		}},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	return &GPUSpotMaterial{
		buf:   buf,
		group: group,
	}, nil
}

// Update validates mat and writes its packed uniform bytes to dev without heap allocations.
func (m *GPUSpotMaterial) Update(dev *gpu.Device, mat SpotMaterial) error {
	if err := mat.Validate(); err != nil {
		return err
	}
	if dev == nil || !m.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: spot material buffer is not valid"}
	}
	data := AppendSpotMaterialUniforms(m.scratch[:0], mat)
	dev.WriteBuffer(m.buf, 0, data)
	return nil
}

// Buffer returns the underlying uniform buffer, or nil for a nil receiver.
func (m *GPUSpotMaterial) Buffer() *gpu.Buffer {
	if m == nil {
		return nil
	}
	return m.buf
}

// BindGroup returns the bind group to bind at [SpotMaterialGroup], or nil for a nil receiver.
func (m *GPUSpotMaterial) BindGroup() *gpu.BindGroup {
	if m == nil {
		return nil
	}
	return m.group
}

// ByteSize returns the GPU uniform buffer size in bytes, or zero for a nil receiver.
func (m *GPUSpotMaterial) ByteSize() uint64 {
	if m == nil || m.buf == nil {
		return 0
	}
	return m.buf.Size()
}

// Valid reports whether m's uniform buffer and bind group are open and belong to a live device.
func (m *GPUSpotMaterial) Valid() bool {
	return m != nil && m.buf.Valid() && m.group.Valid()
}

// Close releases m's bind group and uniform buffer now instead of waiting for scope disposal. It is safe to call on a nil receiver or more than once.
func (m *GPUSpotMaterial) Close() error {
	if m == nil {
		return nil
	}
	var err error
	if m.group != nil {
		err = errors.Join(err, m.group.Close())
	}
	if m.buf != nil {
		err = errors.Join(err, m.buf.Close())
	}
	return err
}

// SpotLitPipelineOptions configures [NewSpotLitPipeline].
type SpotLitPipelineOptions struct {
	Label          string
	CameraLayout   *gpu.BindGroupLayout
	LightsLayout   *gpu.BindGroupLayout
	MaterialLayout *gpu.BindGroupLayout
	DepthFormat    gpu.TextureFormat
	// ColorFormat overrides the target color attachment format; FormatUndefined uses dev.SurfaceFormat().
	ColorFormat gpu.TextureFormat
	// DoubleSided disables back-face culling (gpu.CullNone) when true; false uses gpu.CullBack.
	DoubleSided bool
}

// NewSpotLitPipeline compiles [SpotLitWGSL] and creates a [gpu.Pipeline] for [MeshVertexLayout] meshes lit by [GPUSpotLights].
func NewSpotLitPipeline(cx *core.Scope, dev *gpu.Device, opts SpotLitPipelineOptions) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if opts.CameraLayout == nil || !opts.CameraLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	if opts.LightsLayout == nil || !opts.LightsLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid spot lights bind group layout"}
	}
	if opts.MaterialLayout == nil || !opts.MaterialLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid spot material bind group layout"}
	}
	mod, err := gpu.CompileWGSL(SpotLitWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile spot lit shader: %w", err)
	}
	label := opts.Label
	if label == "" {
		label = "render3d.spot_lit"
	}
	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = dev.SurfaceFormat()
	}
	cull := gpu.CullBack
	if opts.DoubleSided {
		cull = gpu.CullNone
	}
	desc := gpu.PipelineDesc{
		Label:         label,
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{opts.CameraLayout, opts.LightsLayout, opts.MaterialLayout},
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: gpu.FrontCCW,
			CullMode:  cull,
		},
		Targets: []gpu.ColorTargetState{{Format: colorFormat}},
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

// SpotLitRendererOptions configures [NewSpotLitRenderer].
type SpotLitRendererOptions struct {
	Label        string
	CameraLayout *gpu.BindGroupLayout
	DepthFormat  gpu.TextureFormat
	ColorFormat  gpu.TextureFormat
	DoubleSided  bool
	Retained     bool
}

// SpotLitRenderer owns a [GPUSpotLights] uniform buffer, a shared material bind group layout, and the [SpotLitWGSL] render pipeline so callers can light and draw [GPUMesh] instances with minimal boilerplate.
type SpotLitRenderer struct {
	cx       *core.Scope
	dev      *gpu.Device
	label    string
	retained bool
	lights   *GPUSpotLights
	matLay   *gpu.BindGroupLayout
	pipeline *gpu.Pipeline
}

// NewSpotLitRenderer builds a [SpotLitRenderer] on dev scoped to cx, sharing opts.CameraLayout (such as [CameraBuffer.Layout]) at [CameraGroup].
func NewSpotLitRenderer(cx *core.Scope, dev *gpu.Device, opts SpotLitRendererOptions) (*SpotLitRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.spot_lit"
	}
	lights, err := NewGPUSpotLights(cx, dev, GPUSpotLightsOptions{
		Label:    label + " lights",
		Retained: opts.Retained,
	})
	if err != nil {
		return nil, err
	}
	matLay, err := SpotMaterialBindingLayout(cx, dev)
	if err != nil {
		_ = lights.Close()
		return nil, err
	}
	pipe, err := NewSpotLitPipeline(cx, dev, SpotLitPipelineOptions{
		Label:          label,
		CameraLayout:   opts.CameraLayout,
		LightsLayout:   lights.Layout(),
		MaterialLayout: matLay,
		DepthFormat:    opts.DepthFormat,
		ColorFormat:    opts.ColorFormat,
		DoubleSided:    opts.DoubleSided,
	})
	if err != nil {
		_ = matLay.Close()
		_ = lights.Close()
		return nil, err
	}
	return &SpotLitRenderer{
		cx:       cx,
		dev:      dev,
		label:    label,
		retained: opts.Retained,
		lights:   lights,
		matLay:   matLay,
		pipeline: pipe,
	}, nil
}

// Lights returns the renderer's [GPUSpotLights] uniform buffer wrapper.
func (r *SpotLitRenderer) Lights() *GPUSpotLights {
	if r == nil {
		return nil
	}
	return r.lights
}

// Pipeline returns the underlying [SpotLitWGSL] render pipeline.
func (r *SpotLitRenderer) Pipeline() *gpu.Pipeline {
	if r == nil {
		return nil
	}
	return r.pipeline
}

// MaterialLayout returns the bind group layout for @group(2) materials created with [SpotLitRenderer.NewMaterial].
func (r *SpotLitRenderer) MaterialLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.matLay
}

// SetLights validates and uploads set to the renderer's spot light uniform buffer.
func (r *SpotLitRenderer) SetLights(set SpotLightSet) error {
	if r == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil spot lit renderer"}
	}
	return r.lights.Update(r.dev, set)
}

// NewMaterial creates a [GPUSpotMaterial] bound to r's material layout and inheriting r's Retained setting.
func (r *SpotLitRenderer) NewMaterial(mat SpotMaterial) (*GPUSpotMaterial, error) {
	if r == nil || r.dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil spot lit renderer"}
	}
	return NewGPUSpotMaterial(r.cx, r.dev, r.matLay, mat, GPUSpotLightsOptions{
		Label:    r.label + " material",
		Retained: r.retained,
	})
}

// BeginPass sets r's pipeline, binds cameraGroup at [CameraGroup] and binds r's spot light uniform block at [SpotLightsGroup]. It does nothing when r, pass or cameraGroup is invalid.
func (r *SpotLitRenderer) BeginPass(pass *gpu.RenderPass, cameraGroup *gpu.BindGroup) {
	if !r.Valid() || pass == nil || cameraGroup == nil || !cameraGroup.Valid() {
		return
	}
	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(CameraGroup, cameraGroup)
	pass.SetBindGroup(SpotLightsGroup, r.lights.BindGroup())
}

// Draw binds mat at [SpotMaterialGroup] and draws mesh. Call [SpotLitRenderer.BeginPass] once per render pass before calling Draw. It records nothing when mesh or mat is invalid.
func (r *SpotLitRenderer) Draw(pass *gpu.RenderPass, mesh *GPUMesh, mat *GPUSpotMaterial) {
	if !r.Valid() || pass == nil || !mesh.Valid() || !mat.Valid() {
		return
	}
	pass.SetBindGroup(SpotMaterialGroup, mat.BindGroup())
	mesh.Draw(pass)
}

// Valid reports whether r's pipeline, spot lights buffer and material layout are open and belong to a live device.
func (r *SpotLitRenderer) Valid() bool {
	return r != nil && r.pipeline.Valid() && r.lights.Valid() && r.matLay.Valid()
}

// Close releases the pipeline, material layout and spot light resources owned by r. It is safe to call on a nil receiver or more than once.
func (r *SpotLitRenderer) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.pipeline != nil {
		err = errors.Join(err, r.pipeline.Close())
	}
	if r.matLay != nil {
		err = errors.Join(err, r.matLay.Close())
	}
	if r.lights != nil {
		err = errors.Join(err, r.lights.Close())
	}
	return err
}

func spotDistanceAttenuation(dist, invRange float32) float32 {
	distSq := max(dist*dist, spotLightMinDistSq)
	if invRange <= 0 {
		return 1.0 / distSq
	}
	ratio := dist * invRange
	ratio2 := ratio * ratio
	w := clampUnit(1.0 - ratio2*ratio2)
	return (w * w) / distSq
}

func spotConeAttenuation(spotDir, toLight math3d.Vec3, angleScale, angleOffset float32) float32 {
	cd := spotDir.Dot(toLight.Neg())
	t := clampUnit(cd*angleScale + angleOffset)
	return t * t
}

func spotNormalMatrixColumns(m math3d.Mat4) (c0, c1, c2 math3d.Vec3) {
	// Upper-left 3x3 columns of column-major Mat4:
	// col 0 = (m[0], m[1], m[2]), col 1 = (m[4], m[5], m[6]), col 2 = (m[8], m[9], m[10]).
	a00, a10, a20 := m[0], m[1], m[2]
	a01, a11, a21 := m[4], m[5], m[6]
	a02, a12, a22 := m[8], m[9], m[10]

	// Cofactor matrix (which equals det(A) * (A^-1)^T):
	cf00 := a11*a22 - a12*a21
	cf10 := -(a01*a22 - a02*a21)
	cf20 := a01*a12 - a02*a11

	cf01 := -(a10*a22 - a12*a20)
	cf11 := a00*a22 - a02*a20
	cf21 := -(a00*a12 - a02*a10)

	cf02 := a10*a21 - a11*a20
	cf12 := -(a00*a21 - a01*a20)
	cf22 := a00*a11 - a01*a10

	det := a00*cf00 + a01*cf01 + a02*cf02
	if math.Abs(float64(det)) <= 1e-12 {
		return math3d.Vec3{X: a00, Y: a10, Z: a20},
			math3d.Vec3{X: a01, Y: a11, Z: a21},
			math3d.Vec3{X: a02, Y: a12, Z: a22}
	}
	invDet := 1.0 / det
	return math3d.Vec3{X: cf00 * invDet, Y: cf10 * invDet, Z: cf20 * invDet},
		math3d.Vec3{X: cf01 * invDet, Y: cf11 * invDet, Z: cf21 * invDet},
		math3d.Vec3{X: cf02 * invDet, Y: cf12 * invDet, Z: cf22 * invDet}
}

func putSpotVec4(dst []byte, x, y, z, w float32) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(x))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(y))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(z))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(w))
}

func clampUnit(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
