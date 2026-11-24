package main

import (
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	clearSkyHex = 0x0D1322

	// baseColorGroup is the bind group of the base-color texture and sampler, the first slot after render3d's camera (0), light (render3d.LightGroup) and per-draw PBR (render3d.PBRDrawGroup) groups.
	baseColorGroup = render3d.PBRDrawGroup + 1
)

// lanternShaderWGSL is render3d's LitPBRWGSL with a base-color texture: it reads the camera (group 0), the scene's render3d.LightRig (render3d.LightGroup) and a render3d.PBRDrawUniform per draw (render3d.PBRDrawGroup), samples the sRGB base-color texture at baseColorGroup, shades with render3d's shade_pbr, adds the material's emissive, blends towards the sky beyond 9.5 m and encodes the result to sRGB. The fog color is clearSkyHex in linear light.
const lanternShaderWGSL = render3d.CameraWGSL + render3d.LightWGSL + `
struct PBRDrawUniform {
	model: mat4x4<f32>,
	normal_matrix: mat4x4<f32>,
	base_color: vec4<f32>,
	pbr_params: vec4<f32>,
	emissive: vec4<f32>,
}

@group(2) @binding(0) var<uniform> pbr_draw: PBRDrawUniform;
@group(3) @binding(0) var base_tex: texture_2d<f32>;
@group(3) @binding(1) var base_samp: sampler;

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
	let world = pbr_draw.model * vec4<f32>(in.pos, 1.0);
	out.clip = camera.view_proj * world;
	out.world_pos = world.xyz;
	out.world_normal = (pbr_draw.normal_matrix * vec4<f32>(in.normal, 0.0)).xyz;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let texel = textureSample(base_tex, base_samp, in.uv);
	let base_color = pbr_draw.base_color.rgb * texel.rgb;
	let to_eye = camera.eye.xyz - in.world_pos * camera.eye.w;
	let lit = shade_pbr(
		in.world_pos,
		in.world_normal,
		normalize(to_eye),
		base_color,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
	) + pbr_draw.emissive.rgb;
	let fog_color = vec3<f32>(0.00402, 0.00651, 0.01600);
	let fog_t = clamp((length(to_eye) - 9.5) / 22.0, 0.0, 0.55);
	return vec4<f32>(linear_to_srgb(mix(lit, fog_color, fog_t)), 1.0);
}
`

type gpuPrimitive struct {
	mesh       *render3d.GPUMesh
	materialID int
}

type gpuDrawable struct {
	nodeIdx  int
	prim     gpuPrimitive
	texGroup *gpu.BindGroup
	draw     *render3d.PBRDrawBuffer
	material render3d.PBRMaterial
}

// Renderer owns the GPU buffers, textures, samplers, bind groups and render pipeline for the glTF scene. Lights go through render3d.GPULightBuffer and materials through render3d.PBRDrawBuffer, so the shading is render3d's metallic-roughness PBR.
type Renderer struct {
	dev *gpu.Device

	camBuf     *render3d.CameraBuffer
	lights     *render3d.GPULightBuffer
	drawLayout *gpu.BindGroupLayout
	texLayout  *gpu.BindGroupLayout
	pipe       *gpu.Pipeline

	sampler   *render3d.GPUSampler
	whiteTex  *render3d.GPUTexture
	whiteBG   *gpu.BindGroup
	textures  []*render3d.GPUTexture
	texGroups []*gpu.BindGroup
	meshes    [][]gpuPrimitive
	drawables []gpuDrawable

	ready bool
}

// Setup uploads the glTF meshes, embedded textures, samplers, uniform buffers and render pipeline onto dev. It reuses valid retained resources when called again after a GPU device restore.
func (r *Renderer) Setup(cx *core.Scope, dev *gpu.Device, s *Scene) error {
	if dev == nil {
		return fmt.Errorf("lantern3d: nil gpu device")
	}
	r.dev = dev
	doc := s.Doc

	// Camera uniform buffer (render3d.CameraGroup).
	if r.camBuf == nil || !r.camBuf.Buffer().Valid() {
		cb, err := render3d.NewCameraBuffer(cx, dev)
		if err != nil {
			return fmt.Errorf("camera buffer: %w", err)
		}
		r.camBuf = cb
	}

	// Scene lights (render3d.LightGroup).
	if !r.lights.Valid() {
		lb, err := render3d.NewLightBuffer(cx, dev, s.Lights, render3d.LightBufferOptions{
			Label:    "lantern3d.lights",
			Retained: true,
		})
		if err != nil {
			return fmt.Errorf("light buffer: %w", err)
		}
		r.lights = lb
	}

	// Per-draw transform and material layout (render3d.PBRDrawGroup), shared by every drawable.
	if r.drawLayout == nil || !r.drawLayout.Valid() {
		dl, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label: "lantern3d.pbr_draw",
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageVertex | gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
		if err != nil {
			return fmt.Errorf("draw layout: %w", err)
		}
		r.drawLayout = dl
	}

	// Base-color texture and sampler layout (baseColorGroup).
	if r.texLayout == nil || !r.texLayout.Valid() {
		tl, err := render3d.TextureBindingLayout(cx, dev)
		if err != nil {
			return fmt.Errorf("texture layout: %w", err)
		}
		r.texLayout = tl
	}

	// Shared sampler, white fallback texture and the embedded glTF PNG textures. Base color is sRGB, so the textures are uploaded as sRGB and sampled (and mipmapped) in linear light.
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
			SRGB:     true,
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
					SRGB:     true,
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

	// glTF meshes as retained GPUMesh handles.
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

	// Per-node drawables with their PBR draw buffers.
	if err := r.buildDrawables(cx, dev, doc); err != nil {
		return err
	}

	// The forward lit pipeline.
	if r.pipe == nil || !r.pipe.Valid() {
		mod, err := gpu.CompileWGSL(lanternShaderWGSL)
		if err != nil {
			return fmt.Errorf("compile wgsl: %w", err)
		}
		layouts := make([]*gpu.BindGroupLayout, baseColorGroup+1)
		layouts[render3d.CameraGroup] = r.camBuf.Layout()
		layouts[render3d.LightGroup] = r.lights.Layout()
		layouts[render3d.PBRDrawGroup] = r.drawLayout
		layouts[baseColorGroup] = r.texLayout
		pipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
			Label:         "lantern3d.lit_pbr",
			Layouts:       layouts,
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

// pbrMaterial converts a glTF metallic-roughness material into render3d's per-draw PBR parameters. A primitive without a material gets glTF's default material: white, fully metallic and fully rough.
func pbrMaterial(doc *gltf.Document, materialID int) render3d.PBRMaterial {
	if materialID < 0 || materialID >= len(doc.Materials) {
		return render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Alpha: 1, Metallic: 1, Roughness: 1}
	}
	m := doc.Materials[materialID]
	return render3d.PBRMaterial{
		BaseColor: math3d.Vec3{X: m.BaseColorFactor.X, Y: m.BaseColorFactor.Y, Z: m.BaseColorFactor.Z},
		Alpha:     m.BaseColorFactor.W,
		Metallic:  m.MetallicFactor,
		Roughness: m.RoughnessFactor,
		Emissive:  m.EmissiveFactor,
	}
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
				r.drawables = append(r.drawables, gpuDrawable{
					nodeIdx:  ni,
					prim:     gp,
					material: pbrMaterial(doc, gp.materialID),
				})
			}
		}
	}

	for i := range r.drawables {
		d := &r.drawables[i]
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

		if !d.draw.Valid() {
			world := doc.Nodes[d.nodeIdx].WorldMatrix
			db, err := render3d.NewPBRDrawBuffer(cx, dev, r.drawLayout, render3d.NewPBRDrawUniform(world, d.material), true)
			if err != nil {
				return fmt.Errorf("draw buffer %d: %w", i, err)
			}
			d.draw = db
		}
	}
	return nil
}

// Ready reports whether the GPU resources are initialized and valid.
func (r *Renderer) Ready() bool {
	return r != nil && r.ready && r.pipe != nil && r.pipe.Valid() && r.camBuf != nil && r.camBuf.Buffer().Valid() && r.lights.Valid()
}

// Draw uploads the frame's camera, lights and per-node transforms and renders every glTF mesh into frame. It returns an error when the scene's lights fail render3d's validation.
func (r *Renderer) Draw(frame *gpu.Frame, s *Scene, cam scene3d.Camera) error {
	if !r.Ready() || frame == nil || s == nil {
		return nil
	}
	if w, h := frame.Size(); w > 0 && h > 0 {
		cam.SetViewport(float32(w), float32(h))
	}

	dev := frame.Device()
	r.camBuf.WriteCamera(&cam)
	if err := r.lights.Update(dev, s.Lights); err != nil {
		return fmt.Errorf("update lights: %w", err)
	}
	for i := range r.drawables {
		d := &r.drawables[i]
		d.draw.Write(dev, render3d.NewPBRDrawUniform(s.Doc.Nodes[d.nodeIdx].WorldMatrix, d.material))
	}

	pass := frame.BeginRenderPass(gpu.RenderPassDesc{
		Label: "lantern3d.forward",
	})
	pass.ClearColor(core.RGB(clearSkyHex))
	pass.ClearDepth(1)
	pass.SetPipeline(r.pipe)
	pass.SetBindGroup(render3d.CameraGroup, r.camBuf.BindGroup())
	r.lights.Bind(pass)

	for i := range r.drawables {
		d := &r.drawables[i]
		if !d.prim.mesh.Valid() || !d.draw.Valid() || !d.texGroup.Valid() {
			continue
		}
		pass.SetBindGroup(render3d.PBRDrawGroup, d.draw.BindGroup())
		pass.SetBindGroup(baseColorGroup, d.texGroup)
		d.prim.mesh.Draw(pass)
	}

	pass.End()
	frame.Submit()
	return nil
}
