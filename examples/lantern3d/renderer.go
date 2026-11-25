package main

import (
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
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

// gpuDrawable is one part of the level: the node it is drawn at, its mesh, its base color texture (nil for none) and its draw buffer.
type gpuDrawable struct {
	node     *scene3d.Node
	meshID   uint32
	texture  *scenemodel.Texture
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
	textures  map[*scenemodel.Texture]*render3d.GPUTexture
	texGroups map[*scenemodel.Texture]*gpu.BindGroup
	meshes    []*render3d.GPUMesh
	drawables []gpuDrawable

	ready bool
}

// Setup uploads the level's meshes and textures (Scene.Level), samplers, uniform buffers and render pipeline onto dev. It reuses valid retained resources when called again after a GPU device restore.
func (r *Renderer) Setup(cx *core.Scope, dev *gpu.Device, s *Scene) error {
	if dev == nil {
		return fmt.Errorf("lantern3d: nil gpu device")
	}
	r.dev = dev
	level := s.Level

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

	// Shared sampler, white fallback texture and the diorama's PNG textures. Base color is sRGB, so the textures are uploaded as sRGB and sampled (and mipmapped) in linear light.
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

	// The level's base color textures, once each, and their bind groups.
	if r.textures == nil {
		r.textures = make(map[*scenemodel.Texture]*render3d.GPUTexture)
		r.texGroups = make(map[*scenemodel.Texture]*gpu.BindGroup)
	}
	for _, d := range level.Drawables {
		for _, p := range d.Parts {
			t := p.Material.BaseColorTexture
			if t == nil {
				continue
			}
			if gt := r.textures[t]; gt == nil || !gt.Valid() {
				tex, err := render3d.UploadTexture(cx, dev, t.Image, render3d.TextureUpload{
					Label:    "lantern3d " + t.Name,
					SRGB:     true,
					Mipmaps:  true,
					Retained: true,
				})
				if err != nil {
					return fmt.Errorf("upload texture %s: %w", t.Name, err)
				}
				r.textures[t] = tex
			}
			if bg := r.texGroups[t]; bg == nil || !bg.Valid() {
				bg, err := render3d.BindTexture(cx, dev, r.texLayout, r.textures[t], r.sampler)
				if err != nil {
					return fmt.Errorf("bind texture %s: %w", t.Name, err)
				}
				r.texGroups[t] = bg
			}
		}
	}

	// The level's meshes as retained GPUMesh handles, once per scenemodel.Part.MeshID.
	if len(r.meshes) != level.Meshes {
		r.meshes = make([]*render3d.GPUMesh, level.Meshes)
	}
	for _, d := range level.Drawables {
		for _, p := range d.Parts {
			if m := r.meshes[p.MeshID]; m != nil && m.Valid() {
				continue
			}
			uploaded, err := render3d.UploadMesh(cx, dev, p.Mesh, render3d.MeshUpload{
				Label:    fmt.Sprintf("lantern3d.mesh.%d (%s)", p.MeshID, d.Node.Name),
				Retained: true,
			})
			if err != nil {
				return fmt.Errorf("upload mesh of %q: %w", d.Node.Name, err)
			}
			r.meshes[p.MeshID] = uploaded
		}
	}

	// Per-part drawables with their PBR draw buffers.
	if err := r.buildDrawables(cx, dev, level); err != nil {
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

// pbrMaterial converts a part's metallic-roughness material into render3d's per-draw PBR parameters.
func pbrMaterial(m scenemodel.Material) render3d.PBRMaterial {
	return render3d.PBRMaterial{
		BaseColor: m.BaseColor.XYZ(),
		Alpha:     m.BaseColor.W,
		Metallic:  m.Metallic,
		Roughness: m.Roughness,
		Emissive:  m.Emissive,
	}
}

// buildDrawables makes a drawable for every part of the level, once, and its draw buffer, again after a device restore invalidates it.
func (r *Renderer) buildDrawables(cx *core.Scope, dev *gpu.Device, level *scenemodel.Built) error {
	if r.drawables == nil {
		for _, d := range level.Drawables {
			for _, p := range d.Parts {
				r.drawables = append(r.drawables, gpuDrawable{
					node:     d.Node,
					meshID:   p.MeshID,
					texture:  p.Material.BaseColorTexture,
					material: pbrMaterial(p.Material),
				})
			}
		}
	}
	for i := range r.drawables {
		d := &r.drawables[i]
		if !d.draw.Valid() {
			db, err := render3d.NewPBRDrawBuffer(cx, dev, r.drawLayout, render3d.NewPBRDrawUniform(d.node.WorldMatrix(), d.material), true)
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

// Reset clears GPU resource handles when the 3D scene's gpu.View unmounts (for example when switching to the 2D courtyard scene) so returning to 3D uploads fresh resources on the new view scope.
func (r *Renderer) Reset() {
	if r == nil {
		return
	}
	*r = Renderer{}
}

// Draw uploads the frame's camera, lights and per-node transforms and renders every part of the level into frame. It returns an error when the scene's lights fail render3d's validation.
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
		d.draw.Write(dev, render3d.NewPBRDrawUniform(d.node.WorldMatrix(), d.material))
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
		if !d.node.EffectiveVisible() {
			continue
		}
		mesh, texGroup := r.meshes[d.meshID], r.whiteBG
		if d.texture != nil {
			texGroup = r.texGroups[d.texture]
		}
		if !mesh.Valid() || !d.draw.Valid() || !texGroup.Valid() {
			continue
		}
		pass.SetBindGroup(render3d.PBRDrawGroup, d.draw.BindGroup())
		pass.SetBindGroup(baseColorGroup, texGroup)
		mesh.Draw(pass)
	}

	pass.End()
	frame.Submit()
	return nil
}
