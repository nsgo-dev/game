package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	clearSkyHex = 0x0D1322

	// Uniform block sizes matching lanternShaderWGSL.
	lightsUniformSize = 9 * 16      // 144 bytes (9 vec4<f32>)
	objectUniformSize = 2*64 + 2*16 // 160 bytes (2 mat4x4 + 2 vec4)
)

// lanternShaderWGSL renders textured lit glTF meshes with a directional moon
// key light, an orbiting warm point light, an inner shrine lantern glow,
// hemisphere ambient, and linear distance fog.
//
// Portability: uses only basic arithmetic, dot/normalize/max/min/clamp/mix,
// and textureSample (no exp, exp2, log2, pow, or compute/storage buffers), so
// it runs identically across WebGPU, Metal, Vulkan, D3D12, WebGL 2, GLES 3.0,
// and NSGo's software GPU renderer.
const lanternShaderWGSL = render3d.CameraWGSL + `
@group(1) @binding(0) var base_tex: texture_2d<f32>;
@group(1) @binding(1) var base_samp: sampler;

struct SceneLights {
	key_dir_intensity: vec4<f32>,
	key_color: vec4<f32>,
	point_pos_range: vec4<f32>,
	point_color_intensity: vec4<f32>,
	inner_pos_range: vec4<f32>,
	inner_color_intensity: vec4<f32>,
	sky_ambient: vec4<f32>,
	ground_ambient: vec4<f32>,
	fog_color_range: vec4<f32>,
}

@group(2) @binding(0) var<uniform> lights: SceneLights;

struct ObjectUniform {
	model: mat4x4<f32>,
	normal_mat: mat4x4<f32>,
	base_color: vec4<f32>,
	emissive_spec: vec4<f32>,
}

@group(3) @binding(0) var<uniform> object: ObjectUniform;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let world = object.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (object.normal_mat * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv = in.uv;
	return out;
}

fn spec16(ndh: f32) -> f32 {
	let s2 = ndh * ndh;
	let s4 = s2 * s2;
	let s8 = s4 * s4;
	return s8 * s8;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let texel = textureSample(base_tex, base_samp, in.uv);
	let albedo = texel.rgb * object.base_color.rgb;
	let n = normalize(in.world_normal);
	let to_eye = camera.eye.xyz - in.world_pos * camera.eye.w;
	let view_dist = length(to_eye);
	let v = normalize(to_eye);
	let spec_strength = object.emissive_spec.w;

	// Hemisphere ambient (twilight sky above, warm courtyard bounce below).
	let hemi = 0.5 + 0.5 * n.y;
	var lit = albedo * mix(lights.ground_ambient.rgb, lights.sky_ambient.rgb, hemi);

	// Directional key light (cool moonlight).
	let kl = normalize(lights.key_dir_intensity.xyz);
	let k_ndl = max(dot(n, kl), 0.0);
	let kh = normalize(kl + v);
	let k_spec = spec16(max(dot(n, kh), 0.0)) * spec_strength;
	lit = lit + (albedo * k_ndl + vec3<f32>(k_spec)) * lights.key_color.rgb * lights.key_dir_intensity.w;

	// Orbiting warm point light (light wisp).
	let p_vec = lights.point_pos_range.xyz - in.world_pos;
	let p_dist2 = dot(p_vec, p_vec);
	let p_dist = sqrt(max(p_dist2, 0.0001));
	let p_dir = p_vec / p_dist;
	let p_falloff = clamp(1.0 - p_dist / lights.point_pos_range.w, 0.0, 1.0);
	let p_atten = (p_falloff * p_falloff) / (1.0 + 0.22 * p_dist2);
	let p_ndl = max(dot(n, p_dir), 0.0);
	let ph = normalize(p_dir + v);
	let p_spec = spec16(max(dot(n, ph), 0.0)) * spec_strength * 1.25;
	lit = lit + (albedo * p_ndl + vec3<f32>(p_spec)) * lights.point_color_intensity.rgb * (lights.point_color_intensity.w * p_atten);

	// Inner lantern shrine glow.
	let i_vec = lights.inner_pos_range.xyz - in.world_pos;
	let i_dist2 = dot(i_vec, i_vec);
	let i_dist = sqrt(max(i_dist2, 0.0001));
	let i_dir = i_vec / i_dist;
	let i_falloff = clamp(1.0 - i_dist / lights.inner_pos_range.w, 0.0, 1.0);
	let i_atten = (i_falloff * i_falloff) / (1.0 + 0.35 * i_dist2);
	let i_ndl = max(dot(n, i_dir), 0.0);
	lit = lit + albedo * i_ndl * lights.inner_color_intensity.rgb * (lights.inner_color_intensity.w * i_atten);

	// Emissive contribution (glowing lantern core, crystal accents, orbiting wisp).
	lit = lit + object.emissive_spec.rgb;

	// Linear horizon distance blend (no exp/exp2).
	let fog_t = clamp((view_dist - lights.fog_color_range.w) / 22.0, 0.0, 0.55);
	lit = mix(lit, lights.fog_color_range.rgb, fog_t);

	// Soft Reinhard highlight compression.
	let mapped = lit / (vec3<f32>(1.0) + lit * 0.35);
	return vec4<f32>(clamp(mapped, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);
}
`

type gpuPrimitive struct {
	mesh       *render3d.GPUMesh
	materialID int
}

type gpuDrawable struct {
	nodeIdx    int
	prim       gpuPrimitive
	texGroup   *gpu.BindGroup
	objBuf     *gpu.Buffer
	objGroup   *gpu.BindGroup
	baseColor  math3d.Vec4
	emissive   math3d.Vec3
	specWeight float32
}

// Renderer owns the GPU buffers, textures, samplers, bind groups, and render
// pipeline for the glTF scene.
type Renderer struct {
	dev *gpu.Device

	camBuf    *render3d.CameraBuffer
	texLayout *gpu.BindGroupLayout
	lightsLay *gpu.BindGroupLayout
	objLayout *gpu.BindGroupLayout
	pipe      *gpu.Pipeline

	lightsBuf   *gpu.Buffer
	lightsGroup *gpu.BindGroup

	sampler   *render3d.GPUSampler
	whiteTex  *render3d.GPUTexture
	whiteBG   *gpu.BindGroup
	textures  []*render3d.GPUTexture
	texGroups []*gpu.BindGroup
	meshes    [][]gpuPrimitive
	drawables []gpuDrawable

	lightsScratch [lightsUniformSize]byte
	objectScratch [objectUniformSize]byte

	ready bool
}

// Setup uploads the glTF meshes, embedded textures, samplers, uniform buffers,
// and render pipeline onto dev. It reuses valid retained resources when called
// again after a GPU device restore.
func (r *Renderer) Setup(cx *core.Scope, dev *gpu.Device, s *Scene) error {
	if dev == nil {
		return fmt.Errorf("lantern3d: nil gpu device")
	}
	r.dev = dev
	doc := s.Doc

	// 1. Camera uniform buffer (group 0).
	if r.camBuf == nil || !r.camBuf.Buffer().Valid() {
		cb, err := render3d.NewCameraBuffer(cx, dev)
		if err != nil {
			return fmt.Errorf("camera buffer: %w", err)
		}
		r.camBuf = cb
	}

	// 2. Texture & sampler bind group layout (group 1).
	if r.texLayout == nil || !r.texLayout.Valid() {
		tl, err := render3d.TextureBindingLayout(cx, dev)
		if err != nil {
			return fmt.Errorf("texture layout: %w", err)
		}
		r.texLayout = tl
	}

	// 3. Shared sampler + white fallback texture + embedded glTF PNG textures.
	if r.sampler == nil || !r.sampler.Valid() {
		sOpts := render3d.SamplerLinearRepeat()
		sOpts.Label = "lantern3d.linear_repeat"
		sOpts.Retained = true
		samp, err := render3d.NewGPUSampler(cx, dev, sOpts)
		if err != nil {
			return fmt.Errorf("sampler: %w", err)
		}
		r.sampler = samp
	}

	if r.whiteTex == nil || !r.whiteTex.Valid() {
		white := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		white.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		wt, err := render3d.UploadTexture(cx, dev, white, render3d.TextureUpload{
			Label:    "lantern3d.white",
			Retained: true,
		})
		if err != nil {
			return fmt.Errorf("white texture: %w", err)
		}
		r.whiteTex = wt
	}
	if r.whiteBG == nil || !r.whiteBG.Valid() {
		bg, err := render3d.BindTexture(cx, dev, r.texLayout, r.whiteTex, r.sampler)
		if err != nil {
			return fmt.Errorf("white bind group: %w", err)
		}
		r.whiteBG = bg
	}

	if len(r.textures) != len(doc.Textures) {
		r.textures = make([]*render3d.GPUTexture, len(doc.Textures))
		r.texGroups = make([]*gpu.BindGroup, len(doc.Textures))
	}
	for i, gt := range doc.Textures {
		if r.textures[i] == nil || !r.textures[i].Valid() {
			if gt.Source < 0 || gt.Source >= len(doc.Images) {
				r.textures[i] = r.whiteTex
			} else {
				img, err := doc.DecodeImage(gt.Source)
				if err != nil {
					return fmt.Errorf("decode gltf image %d: %w", gt.Source, err)
				}
				label := doc.Images[gt.Source].Name
				if label == "" {
					label = fmt.Sprintf("lantern3d.tex.%d", i)
				}
				tex, err := render3d.UploadTexture(cx, dev, img, render3d.TextureUpload{
					Label:    label,
					Mipmaps:  true,
					Retained: true,
				})
				if err != nil {
					return fmt.Errorf("upload gltf texture %d: %w", i, err)
				}
				r.textures[i] = tex
			}
		}
		if r.texGroups[i] == nil || !r.texGroups[i].Valid() {
			bg, err := render3d.BindTexture(cx, dev, r.texLayout, r.textures[i], r.sampler)
			if err != nil {
				return fmt.Errorf("bind gltf texture %d: %w", i, err)
			}
			r.texGroups[i] = bg
		}
	}

	// 4. Scene lights uniform buffer & layout (group 2).
	if r.lightsLay == nil || !r.lightsLay.Valid() {
		ll, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label: "lantern3d.lights",
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
		if err != nil {
			return fmt.Errorf("lights layout: %w", err)
		}
		r.lightsLay = ll
	}
	if r.lightsBuf == nil || !r.lightsBuf.Valid() {
		lb, err := dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    "lantern3d.lights",
			Size:     lightsUniformSize,
			Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
			Retained: true,
		})
		if err != nil {
			return fmt.Errorf("lights buffer: %w", err)
		}
		r.lightsBuf = lb
	}
	if r.lightsGroup == nil || !r.lightsGroup.Valid() {
		lg, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
			Label:   "lantern3d.lights",
			Layout:  r.lightsLay,
			Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.lightsBuf, Size: lightsUniformSize}},
		})
		if err != nil {
			return fmt.Errorf("lights bind group: %w", err)
		}
		r.lightsGroup = lg
	}

	// 5. Per-drawable object uniform layout (group 3).
	if r.objLayout == nil || !r.objLayout.Valid() {
		ol, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label: "lantern3d.object",
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageVertex | gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
		if err != nil {
			return fmt.Errorf("object layout: %w", err)
		}
		r.objLayout = ol
	}

	// 6. Upload glTF meshes as Retained GPUMesh handles.
	if len(r.meshes) != len(doc.Meshes) {
		r.meshes = make([][]gpuPrimitive, len(doc.Meshes))
	}
	for mi, gm := range doc.Meshes {
		if len(r.meshes[mi]) != len(gm.Primitives) {
			r.meshes[mi] = make([]gpuPrimitive, len(gm.Primitives))
		}
		for pi, prim := range gm.Primitives {
			if prim.Mode != gltf.ModeTriangles {
				continue
			}
			if r.meshes[mi][pi].mesh == nil || !r.meshes[mi][pi].mesh.Valid() {
				uploaded, err := render3d.UploadMesh(cx, dev, render3d.Mesh{
					Positions: prim.Positions,
					Normals:   prim.Normals,
					UVs:       prim.TexCoords0,
					Indices:   prim.Indices,
				}, render3d.MeshUpload{
					Label:    fmt.Sprintf("%s[%d]", gm.Name, pi),
					Retained: true,
				})
				if err != nil {
					return fmt.Errorf("upload mesh %q[%d]: %w", gm.Name, pi, err)
				}
				r.meshes[mi][pi] = gpuPrimitive{
					mesh:       uploaded,
					materialID: prim.Material,
				}
			}
		}
	}

	// 7. Build or refresh per-node drawable entries & uniform buffers.
	if err := r.buildDrawables(cx, dev, doc); err != nil {
		return err
	}

	// 8. Compile WGSL and create the forward lit render pipeline.
	if r.pipe == nil || !r.pipe.Valid() {
		mod, err := gpu.CompileWGSL(lanternShaderWGSL)
		if err != nil {
			return fmt.Errorf("compile wgsl: %w", err)
		}
		pipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
			Label: "lantern3d.lit",
			Layouts: []*gpu.BindGroupLayout{
				render3d.CameraGroup: r.camBuf.Layout(),
				1:                    r.texLayout,
				2:                    r.lightsLay,
				3:                    r.objLayout,
			},
			Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
			Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
			VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
			Primitive: gpu.PrimitiveState{
				Topology:  gpu.TopologyTriangleList,
				FrontFace: gpu.FrontCCW,
				CullMode:  gpu.CullBack,
			},
			DepthStencil: &gpu.DepthStencilState{
				Format:       gpu.FormatDepth24Plus,
				DepthWrite:   true,
				DepthCompare: gpu.CompareLess,
			},
			Targets: []gpu.ColorTargetState{{
				Format:    dev.SurfaceFormat(),
				WriteMask: gpu.ColorWriteAll,
			}},
		})
		if err != nil {
			return fmt.Errorf("create pipeline: %w", err)
		}
		r.pipe = pipe
	}

	r.ready = true
	return nil
}

func (r *Renderer) buildDrawables(cx *core.Scope, dev *gpu.Device, doc *gltf.Document) error {
	expected := 0
	for i := range doc.Nodes {
		mi := doc.Nodes[i].Mesh
		if mi >= 0 && mi < len(r.meshes) {
			for _, gp := range r.meshes[mi] {
				if gp.mesh != nil {
					expected++
				}
			}
		}
	}

	if len(r.drawables) != expected {
		r.drawables = make([]gpuDrawable, 0, expected)
		for ni := range doc.Nodes {
			mi := doc.Nodes[ni].Mesh
			if mi < 0 || mi >= len(r.meshes) {
				continue
			}
			for _, gp := range r.meshes[mi] {
				if gp.mesh == nil {
					continue
				}
				d := gpuDrawable{
					nodeIdx:    ni,
					prim:       gp,
					baseColor:  math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1},
					specWeight: 0.25,
				}
				if gp.materialID >= 0 && gp.materialID < len(doc.Materials) {
					mat := doc.Materials[gp.materialID]
					d.baseColor = mat.BaseColorFactor
					d.emissive = mat.EmissiveFactor
					// Smoother / more metallic surfaces get stronger specular highlight.
					d.specWeight = clamp32((1.0-mat.RoughnessFactor)*0.65+mat.MetallicFactor*0.35, 0.05, 0.9)
				}
				r.drawables = append(r.drawables, d)
			}
		}
	}

	for i := range r.drawables {
		d := &r.drawables[i]
		// Resolve material texture bind group.
		texBG := r.whiteBG
		if d.prim.materialID >= 0 && d.prim.materialID < len(doc.Materials) {
			mat := doc.Materials[d.prim.materialID]
			if mat.BaseColorTexture != nil {
				ti := mat.BaseColorTexture.TextureIndex
				if ti >= 0 && ti < len(r.texGroups) && r.texGroups[ti] != nil {
					texBG = r.texGroups[ti]
				}
			}
		}
		d.texGroup = texBG

		if d.objBuf == nil || !d.objBuf.Valid() {
			buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
				Label:    fmt.Sprintf("lantern3d.obj.%d", i),
				Size:     objectUniformSize,
				Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
				Retained: true,
			})
			if err != nil {
				return fmt.Errorf("object buffer %d: %w", i, err)
			}
			d.objBuf = buf
		}
		if d.objGroup == nil || !d.objGroup.Valid() {
			bg, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
				Label:   fmt.Sprintf("lantern3d.obj.%d", i),
				Layout:  r.objLayout,
				Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: d.objBuf, Size: objectUniformSize}},
			})
			if err != nil {
				return fmt.Errorf("object bind group %d: %w", i, err)
			}
			d.objGroup = bg
		}
	}
	return nil
}

// Ready reports whether the GPU resources are initialized and valid.
func (r *Renderer) Ready() bool {
	return r != nil && r.ready && r.pipe != nil && r.pipe.Valid() && r.camBuf != nil && r.camBuf.Buffer().Valid()
}

// Draw uploads per-frame camera, light, and transform uniforms and renders all
// glTF meshes into frame.
func (r *Renderer) Draw(frame *gpu.Frame, s *Scene, cam scene3d.Camera) {
	if !r.Ready() || frame == nil || s == nil {
		return
	}
	if w, h := frame.Size(); w > 0 && h > 0 {
		cam.SetViewport(float32(w), float32(h))
	}

	r.camBuf.WriteCamera(&cam)
	r.encodeLights(s.Lights)
	r.dev.WriteBuffer(r.lightsBuf, 0, r.lightsScratch[:])

	for i := range r.drawables {
		d := &r.drawables[i]
		world := s.Doc.Nodes[d.nodeIdx].WorldMatrix
		r.encodeObject(world, d.baseColor, d.emissive, d.specWeight)
		r.dev.WriteBuffer(d.objBuf, 0, r.objectScratch[:])
	}

	pass := frame.BeginRenderPass(gpu.RenderPassDesc{
		Label: "lantern3d.forward",
	})
	pass.ClearColor(core.RGB(clearSkyHex))
	pass.ClearDepth(1)
	pass.SetPipeline(r.pipe)
	pass.SetBindGroup(render3d.CameraGroup, r.camBuf.BindGroup())
	pass.SetBindGroup(2, r.lightsGroup)

	for i := range r.drawables {
		d := &r.drawables[i]
		if !d.prim.mesh.Valid() || !d.objGroup.Valid() || !d.texGroup.Valid() {
			continue
		}
		pass.SetBindGroup(1, d.texGroup)
		pass.SetBindGroup(3, d.objGroup)
		d.prim.mesh.Draw(pass)
	}

	pass.End()
	frame.Submit()
}

func (r *Renderer) encodeLights(l LightRig) {
	dst := r.lightsScratch[:]
	off := 0
	putVec4 := func(x, y, z, w float32) {
		binary.LittleEndian.PutUint32(dst[off+0:], math.Float32bits(x))
		binary.LittleEndian.PutUint32(dst[off+4:], math.Float32bits(y))
		binary.LittleEndian.PutUint32(dst[off+8:], math.Float32bits(z))
		binary.LittleEndian.PutUint32(dst[off+12:], math.Float32bits(w))
		off += 16
	}
	putVec4(l.KeyDir.X, l.KeyDir.Y, l.KeyDir.Z, l.KeyIntensity)
	putVec4(l.KeyColor.X, l.KeyColor.Y, l.KeyColor.Z, 1)
	putVec4(l.PointPos.X, l.PointPos.Y, l.PointPos.Z, l.PointRange)
	putVec4(l.PointColor.X, l.PointColor.Y, l.PointColor.Z, l.PointIntensity)
	putVec4(l.InnerPos.X, l.InnerPos.Y, l.InnerPos.Z, 5.5)
	putVec4(l.InnerColor.X, l.InnerColor.Y, l.InnerColor.Z, l.InnerIntensity)
	putVec4(l.SkyAmbient.X, l.SkyAmbient.Y, l.SkyAmbient.Z, 1)
	putVec4(l.GroundAmbient.X, l.GroundAmbient.Y, l.GroundAmbient.Z, 1)
	putVec4(l.FogColor.X, l.FogColor.Y, l.FogColor.Z, 9.5)
}

func (r *Renderer) encodeObject(model math3d.Mat4, base math3d.Vec4, emissive math3d.Vec3, spec float32) {
	dst := r.objectScratch[:]
	off := 0
	put := func(f float32) {
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
		off += 4
	}
	for _, f := range model {
		put(f)
	}
	// Normal matrix = inverse transpose of upper 3x3 (zero translation).
	nmat := model
	nmat[12], nmat[13], nmat[14], nmat[15] = 0, 0, 0, 1
	if inv, ok := nmat.Inverse(); ok {
		nmat = inv.Transpose()
	}
	for _, f := range nmat {
		put(f)
	}
	put(base.X)
	put(base.Y)
	put(base.Z)
	put(base.W)
	put(emissive.X)
	put(emissive.Y)
	put(emissive.Z)
	put(spec)
}

func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
