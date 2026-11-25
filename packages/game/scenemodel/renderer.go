package scenemodel

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// BaseColorGroup is the bind group of the base color texture and sampler in ShaderWGSL, after render3d's camera (render3d.CameraGroup), lights (render3d.LightGroup) and per-draw uniform (render3d.PBRDrawGroup).
const BaseColorGroup = render3d.PBRDrawGroup + 1

// ShaderWGSL is the shader module Renderer draws with: render3d.LitPBRWGSL's lighting (render3d.LightWGSL's shade_pbr over the scene's render3d.LightRig) with a base color texture. Per draw it reads a render3d.PBRDrawUniform at render3d.PBRDrawGroup: the model and normal matrices, the linear base color factor, metallic in pbr_params.x, roughness in pbr_params.y, the alpha cutoff of a cutout surface in pbr_params.z (0 for none), 1 in pbr_params.w for a double-sided surface, whose back faces it lights with the normal flipped, the emissive color in emissive.rgb, and 1 in emissive.w for a blended surface, whose alpha it writes (1 otherwise). The texture at BaseColorGroup is sampled at the mesh's UVs and multiplies the base color factor.
const ShaderWGSL = shaderPreludeWGSL + `
@fragment
fn fs_main(in: VertexOut, @builtin(front_facing) front_facing: bool) -> @location(0) vec4<f32> {
	// Sample in uniform control flow, before the cutout's discard.
	let texel = textureSample(base_tex, base_samp, in.uv);
	let base = pbr_draw.base_color * texel;
	if (pbr_draw.pbr_params.z > 0.0 && base.a < pbr_draw.pbr_params.z) {
		discard;
	}
	var n = normalize(in.world_normal);
	if (pbr_draw.pbr_params.w > 0.5 && !front_facing) {
		n = -n;
	}
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let lit = shade_pbr(in.world_pos, n, view_dir, base.rgb, pbr_draw.pbr_params.x, pbr_draw.pbr_params.y) + pbr_draw.emissive.rgb;
	var alpha = 1.0;
	if (pbr_draw.emissive.w > 0.5) {
		alpha = base.a;
	}
	return vec4<f32>(linear_to_srgb(lit), alpha);
}
`

// unlitShaderWGSL is ShaderWGSL with a fragment stage that writes the textured base color plus the emissive color and ignores the lights, which ShadingUnlit draws with. It is a module of its own because NSGo's software renderer runs a module's first fragment entry point whatever the pipeline names.
const unlitShaderWGSL = shaderPreludeWGSL + `
@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	// Sample in uniform control flow, before the cutout's discard.
	let texel = textureSample(base_tex, base_samp, in.uv);
	let base = pbr_draw.base_color * texel;
	if (pbr_draw.pbr_params.z > 0.0 && base.a < pbr_draw.pbr_params.z) {
		discard;
	}
	var alpha = 1.0;
	if (pbr_draw.emissive.w > 0.5) {
		alpha = base.a;
	}
	return vec4<f32>(linear_to_srgb(base.rgb + pbr_draw.emissive.rgb), alpha);
}
`

// shaderPreludeWGSL is what ShaderWGSL and unlitShaderWGSL share: the bindings, the vertex stage and render3d's camera and lights.
const shaderPreludeWGSL = render3d.CameraWGSL + render3d.LightWGSL + `
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
`

// RendererOptions configures NewRenderer.
type RendererOptions struct {
	// Label prefixes the debug labels of the GPU resources; empty is "scenemodel".
	Label string
	// ColorFormat is the color attachment's format; gpu.FormatUndefined is the device's surface format.
	ColorFormat gpu.TextureFormat
	// DepthFormat is the depth attachment's format, which every pass the renderer draws in must have; gpu.FormatUndefined is gpu.FormatDepth24Plus.
	DepthFormat gpu.TextureFormat
	// Retained keeps CPU copies of the buffers and textures so NSGo recreates them after a device loss (gpu.BufferDesc.Retained).
	Retained bool
	// DisableOcclusion disables CPU software occlusion culling even when Built holds occluders.
	DisableOcclusion bool
	// OcclusionWidth and OcclusionHeight configure the software render3d.OcclusionBuffer resolution; non-positive values use render3d's defaults (128x72).
	OcclusionWidth, OcclusionHeight int
	// LODHysteresis overrides the relative deadband [0, 0.5] used by render3d.Queue for LOD selection; 0 uses Built.LODHysteresis.
	LODHysteresis float32
}

// Shading says how Renderer shades the parts it draws (Renderer.SetShading).
type Shading uint8

const (
	// ShadingLit lights every part with the render3d.LightRig given to Prepare. It is a Renderer's shading until SetShading changes it.
	ShadingLit Shading = iota
	// ShadingUnlit draws every part at its textured base color plus its emissive color and ignores the lights, as an editor's unlit view shows a scene.
	ShadingUnlit
)

// String returns "lit" or "unlit", or "Shading(n)" for any other value.
func (s Shading) String() string {
	switch s {
	case ShadingLit:
		return "lit"
	case ShadingUnlit:
		return "unlit"
	}
	return fmt.Sprintf("Shading(%d)", uint8(s))
}

// Renderer draws a Built scene's drawables on a gpu.Device: every part's mesh lit by a render3d.LightRig and textured with its base color, culled and ordered by a render3d.Queue (frustum culling, software occlusion culling when Built has occluders, LOD selection with hysteresis, opaque surfaces front to back by pipeline, material and mesh, blended ones back to front). Each frame, call Prepare with the camera and lights before the render pass, then Draw inside it. Its GPU resources belong to the scope given to NewRenderer, or are released by Close. Use it from the engine goroutine, like the device.
type Renderer struct {
	built *Built
	dev   *gpu.Device

	cam        *render3d.CameraBuffer
	lights     *render3d.GPULightBuffer
	drawLayout *gpu.BindGroupLayout
	texLayout  *gpu.BindGroupLayout
	// pipes holds the pipeline of each DrawPipeline ID for each Shading, the unlit ones made on the first SetShading(ShadingUnlit).
	pipes   [2][4]*gpu.Pipeline
	shading Shading
	// cx, label and opts are what the pipelines SetShading makes need.
	cx    *core.Scope
	label string
	opts  RendererOptions

	white    *render3d.GPUTexture
	textures []*render3d.GPUTexture
	samplers []*render3d.GPUSampler
	// meshes holds each mesh by MeshID, and materials each material's texture bind group by MaterialID.
	meshes    []*render3d.GPUMesh
	materials []*gpu.BindGroup
	// draws holds the per-draw uniform buffer of each part, by drawable and part.
	draws [][]*render3d.PBRDrawBuffer

	queue     render3d.Queue
	occ       *render3d.OcclusionBuffer
	occluders []render3d.Occluder
	items     []render3d.DrawItem
	plan      render3d.RenderPlan
}

// NewRenderer uploads every mesh, texture and material of b to dev, scoped to cx, and creates the buffers and pipelines that draw them. b's drawables are fixed from here on; their nodes may move, hide and show.
func NewRenderer(cx *core.Scope, dev *gpu.Device, b *Built, opts RendererOptions) (*Renderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "scenemodel: nil device"}
	}
	if b == nil {
		return nil, errors.New("scenemodel: nil Built")
	}
	r := &Renderer{built: b, dev: dev, cx: cx, opts: opts}
	if err := r.setup(cx, opts); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

// setup creates r's resources.
func (r *Renderer) setup(cx *core.Scope, opts RendererOptions) error {
	dev := r.dev
	label := opts.Label
	if label == "" {
		label = "scenemodel"
	}
	r.label = label
	var err error
	if r.cam, err = render3d.NewCameraBuffer(cx, dev); err != nil {
		return fmt.Errorf("scenemodel: camera buffer: %w", err)
	}
	if r.lights, err = render3d.NewLightBuffer(cx, dev, render3d.LightRig{}, render3d.LightBufferOptions{Label: label + ".lights", Retained: opts.Retained}); err != nil {
		return fmt.Errorf("scenemodel: light buffer: %w", err)
	}
	if r.drawLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label:   label + ".draw",
		Entries: []gpu.BindGroupLayoutEntry{{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform}},
	}); err != nil {
		return fmt.Errorf("scenemodel: draw layout: %w", err)
	}
	if r.texLayout, err = render3d.TextureBindingLayout(cx, dev); err != nil {
		return fmt.Errorf("scenemodel: texture layout: %w", err)
	}
	if err := r.uploadMaterials(cx, label, opts.Retained); err != nil {
		return err
	}
	if err := r.uploadMeshes(cx, label, opts.Retained); err != nil {
		return err
	}
	r.draws = make([][]*render3d.PBRDrawBuffer, len(r.built.Drawables))
	for di, d := range r.built.Drawables {
		r.draws[di] = make([]*render3d.PBRDrawBuffer, len(d.Parts))
		for pi := range d.Parts {
			buf, err := render3d.NewPBRDrawBuffer(cx, dev, r.drawLayout, drawUniform(d, &d.Parts[pi]), opts.Retained)
			if err != nil {
				return fmt.Errorf("scenemodel: object %q draw buffer: %w", d.Object, err)
			}
			r.draws[di][pi] = buf
		}
	}
	if opts.LODHysteresis > 0 {
		r.queue.LODHysteresis = opts.LODHysteresis
	} else {
		r.queue.LODHysteresis = r.built.LODHysteresis
	}
	if !opts.DisableOcclusion && len(r.built.Occluders) > 0 {
		r.occ = render3d.NewOcclusionBuffer(opts.OcclusionWidth, opts.OcclusionHeight)
	}
	return r.createPipelines(ShadingLit)
}

// uploadMaterials uploads every texture the parts use, once each, and a texture bind group per material.
func (r *Renderer) uploadMaterials(cx *core.Scope, label string, retained bool) error {
	dev := r.dev
	white := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	white.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var err error
	if r.white, err = render3d.UploadTexture(cx, dev, white, render3d.TextureUpload{Label: label + ".white", SRGB: true, Retained: retained}); err != nil {
		return fmt.Errorf("scenemodel: white texture: %w", err)
	}
	r.textures = append(r.textures, r.white)
	uploaded := make(map[*Texture]*render3d.GPUTexture)
	samplers := make(map[render3d.SamplerOptions]*render3d.GPUSampler)
	sampler := func(o render3d.SamplerOptions) (*render3d.GPUSampler, error) {
		o.Retained = retained
		if s, ok := samplers[o]; ok {
			return s, nil
		}
		s, err := render3d.NewGPUSampler(cx, dev, o)
		if err != nil {
			return nil, err
		}
		samplers[o] = s
		r.samplers = append(r.samplers, s)
		return s, nil
	}
	defaultSampler, err := sampler(render3d.SamplerLinearRepeat())
	if err != nil {
		return fmt.Errorf("scenemodel: sampler: %w", err)
	}
	r.materials = make([]*gpu.BindGroup, r.built.Materials)
	for _, d := range r.built.Drawables {
		for pi := range d.Parts {
			p := &d.Parts[pi]
			if int(p.MaterialID) >= len(r.materials) {
				return fmt.Errorf("scenemodel: object %q material ID %d is out of range for %d materials", d.Object, p.MaterialID, len(r.materials))
			}
			if r.materials[p.MaterialID] != nil {
				continue
			}
			tex, samp := r.white, defaultSampler
			if t := p.Material.BaseColorTexture; t != nil {
				gt, ok := uploaded[t]
				if !ok {
					gt, err = render3d.UploadTexture(cx, dev, t.Image, render3d.TextureUpload{Label: label + " " + t.Name, SRGB: true, Mipmaps: t.Mipmaps, Retained: retained})
					if err != nil {
						return fmt.Errorf("scenemodel: object %q texture %s: %w", d.Object, t.Name, err)
					}
					uploaded[t] = gt
					r.textures = append(r.textures, gt)
				}
				if samp, err = sampler(t.Sampler); err != nil {
					return fmt.Errorf("scenemodel: object %q texture %s sampler: %w", d.Object, t.Name, err)
				}
				tex = gt
			}
			bg, err := render3d.BindTexture(cx, dev, r.texLayout, tex, samp)
			if err != nil {
				return fmt.Errorf("scenemodel: object %q material bind group: %w", d.Object, err)
			}
			r.materials[p.MaterialID] = bg
		}
	}
	return nil
}

// uploadMeshes uploads every mesh the parts and their LOD levels use, once each.
func (r *Renderer) uploadMeshes(cx *core.Scope, label string, retained bool) error {
	r.meshes = make([]*render3d.GPUMesh, r.built.Meshes)
	upload := func(obj string, id uint32, mesh render3d.Mesh) error {
		if int(id) >= len(r.meshes) {
			return fmt.Errorf("scenemodel: object %q mesh ID %d is out of range for %d meshes", obj, id, len(r.meshes))
		}
		if r.meshes[id] != nil {
			return nil
		}
		m, err := render3d.UploadMesh(cx, r.dev, mesh, render3d.MeshUpload{Label: fmt.Sprintf("%s.mesh.%d", label, id), Retained: retained})
		if err != nil {
			return fmt.Errorf("scenemodel: object %q mesh: %w", obj, err)
		}
		r.meshes[id] = m
		return nil
	}
	for _, d := range r.built.Drawables {
		for pi := range d.Parts {
			p := &d.Parts[pi]
			if err := upload(d.Object, p.MeshID, p.Mesh); err != nil {
				return err
			}
			for li := range p.LODs {
				if err := upload(d.Object, p.LODs[li].MeshID, p.LODs[li].Mesh); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// createPipelines creates the pipeline of each DrawPipeline ID for shading s.
func (r *Renderer) createPipelines(s Shading) error {
	cx, label, opts := r.cx, r.label, r.opts
	src, suffix := ShaderWGSL, ""
	if s == ShadingUnlit {
		src, suffix = unlitShaderWGSL, ".unlit"
	}
	mod, err := gpu.CompileWGSL(src)
	if err != nil {
		return fmt.Errorf("scenemodel: compile %s shader: %w", s, err)
	}
	colorFormat := opts.ColorFormat
	if colorFormat == gpu.FormatUndefined {
		colorFormat = r.dev.SurfaceFormat()
	}
	depthFormat := opts.DepthFormat
	if depthFormat == gpu.FormatUndefined {
		depthFormat = gpu.FormatDepth24Plus
	}
	layouts := make([]*gpu.BindGroupLayout, BaseColorGroup+1)
	layouts[render3d.CameraGroup] = r.cam.Layout()
	layouts[render3d.LightGroup] = r.lights.Layout()
	layouts[render3d.PBRDrawGroup] = r.drawLayout
	layouts[BaseColorGroup] = r.texLayout
	for id := range r.pipes[s] {
		doubleSided, blend := id&1 != 0, id&2 != 0
		cull := gpu.CullBack
		if doubleSided {
			cull = gpu.CullNone
		}
		target := gpu.ColorTargetState{Format: colorFormat, WriteMask: gpu.ColorWriteAll}
		if blend {
			target.Blend = gpu.BlendAlpha()
		}
		pipe, err := r.dev.CreatePipeline(cx, gpu.PipelineDesc{
			Label:         fmt.Sprintf("%s.pipeline.%d%s", label, id, suffix),
			Layouts:       layouts,
			Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
			Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
			VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
			Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: cull},
			DepthStencil:  &gpu.DepthStencilState{Format: depthFormat, DepthWrite: !blend, DepthCompare: gpu.CompareLess},
			Targets:       []gpu.ColorTargetState{target},
		})
		if err != nil {
			return fmt.Errorf("scenemodel: %s pipeline %d: %w", s, id, err)
		}
		r.pipes[s][id] = pipe
	}
	return nil
}

// SetShading makes Draw shade the parts with s from the next frame on. The first change to ShadingUnlit creates its pipelines, under the scope given to NewRenderer, and returns an error when they cannot be created, leaving the shading as it was; so does an unknown Shading or a closed renderer.
func (r *Renderer) SetShading(s Shading) error {
	if r == nil || r.cam == nil {
		return errors.New("scenemodel: SetShading on a closed renderer")
	}
	if s != ShadingLit && s != ShadingUnlit {
		return fmt.Errorf("scenemodel: unknown shading %d", uint8(s))
	}
	if r.pipes[s][0] == nil {
		if err := r.createPipelines(s); err != nil {
			for id, p := range r.pipes[s] {
				if p != nil {
					_ = p.Close()
					r.pipes[s][id] = nil
				}
			}
			return err
		}
	}
	r.shading = s
	return nil
}

// Shading returns how Draw shades the parts (SetShading).
func (r *Renderer) Shading() Shading {
	if r == nil {
		return ShadingLit
	}
	return r.shading
}

// drawUniform returns the per-draw uniform of part p of drawable d at its node's current world transform.
func drawUniform(d *Drawable, p *Part) render3d.PBRDrawUniform {
	m := &p.Material
	u := render3d.NewPBRDrawUniform(d.Node.WorldMatrix(), render3d.PBRMaterial{
		BaseColor: m.BaseColor.XYZ(),
		Metallic:  m.Metallic,
		Roughness: m.Roughness,
		Emissive:  m.Emissive,
	})
	u.BaseColor.W = min(max(m.BaseColor.W, 0), 1)
	if m.AlphaMode == render3d.AlphaMask {
		// A cutoff of 0 keeps every fragment, as glTF's comparison alpha < cutoff does.
		u.PBRParams.Z = max(m.AlphaCutoff, 0)
	}
	if m.DoubleSided {
		u.PBRParams.W = 1
	}
	if m.AlphaMode == render3d.AlphaBlend {
		u.Emissive.W = 1
	}
	return u
}

// Prepare writes cam and lights to their buffers and each visible part's world transform to its draw buffer, and plans the frame with the renderer's render3d.Queue: frustum culling and draw order. Call it once a frame before the render pass that Draw records into. It returns the plan's statistics, and an error when lights fails render3d's validation or the renderer is closed.
func (r *Renderer) Prepare(cam *scene3d.Camera, lights render3d.LightRig) (render3d.RenderStats, error) {
	if r == nil || cam == nil {
		return render3d.RenderStats{}, errors.New("scenemodel: Prepare needs a renderer and a camera")
	}
	if r.cam == nil {
		return render3d.RenderStats{}, errors.New("scenemodel: Prepare on a closed renderer")
	}
	if err := r.lights.Update(r.dev, lights); err != nil {
		return render3d.RenderStats{}, fmt.Errorf("scenemodel: lights: %w", err)
	}
	r.cam.WriteCamera(cam)
	r.items = r.built.AppendDrawItems(r.items[:0])
	for i := range r.items {
		id := r.items[i].ID
		d, p, _ := r.built.Part(id)
		r.draws[int(id>>32)][int(uint32(id))].Write(r.dev, drawUniform(d, p))
	}
	viewProj := cam.ViewProj()
	var occ *render3d.OcclusionBuffer
	if r.occ != nil {
		r.occluders = r.built.AppendOccluders(r.occluders[:0])
		r.occ.Clear()
		if len(r.occluders) > 0 {
			r.occ.Rasterize(viewProj, r.occluders...)
			r.occ.BuildPyramid()
			occ = r.occ
		}
	}
	r.plan = r.queue.BuildWithOcclusion(viewProj, cam.Pos, r.items, occ)
	return r.plan.Stats, nil
}

// Plan returns the render3d.RenderPlan from the most recent Prepare call, allowing callers to inspect selected LOD indices and culling statistics.
func (r *Renderer) Plan() render3d.RenderPlan {
	if r == nil {
		return render3d.RenderPlan{}
	}
	return r.plan
}

// Draw records the parts Prepare planned into pass: the opaque and cutout ones first, then the blended ones back to front. pass must have a color attachment of RendererOptions.ColorFormat and a depth attachment of its DepthFormat. It records nothing once the renderer is closed.
func (r *Renderer) Draw(pass *gpu.RenderPass) {
	if r == nil || pass == nil || r.cam == nil {
		return
	}
	pass.SetBindGroup(render3d.CameraGroup, r.cam.BindGroup())
	r.lights.Bind(pass)
	current := -1
	draw := func(rd render3d.ResolvedDraw) {
		di, pi := int(rd.ID>>32), int(uint32(rd.ID))
		if int(rd.PipelineID) != current {
			current = int(rd.PipelineID)
			pass.SetPipeline(r.pipes[r.shading][current])
		}
		pass.SetBindGroup(render3d.PBRDrawGroup, r.draws[di][pi].BindGroup())
		pass.SetBindGroup(BaseColorGroup, r.materials[rd.MaterialID])
		r.meshes[rd.MeshID].Draw(pass)
	}
	for _, rd := range r.plan.Opaque {
		draw(rd)
	}
	for _, rd := range r.plan.Transparent {
		draw(rd)
	}
}

// Close releases every GPU resource of r now instead of when its scope is disposed. It is safe to call on nil and more than once; after it, Prepare returns an error and Draw records nothing.
func (r *Renderer) Close() error {
	if r == nil {
		return nil
	}
	var err error
	for _, row := range r.draws {
		for _, d := range row {
			err = errors.Join(err, d.Close())
		}
	}
	for _, m := range r.meshes {
		if m != nil {
			err = errors.Join(err, m.Close())
		}
	}
	for _, bg := range r.materials {
		if bg != nil {
			err = errors.Join(err, bg.Close())
		}
	}
	for _, t := range r.textures {
		err = errors.Join(err, t.Close())
	}
	for _, s := range r.samplers {
		err = errors.Join(err, s.Close())
	}
	for _, set := range r.pipes {
		for _, p := range set {
			if p != nil {
				err = errors.Join(err, p.Close())
			}
		}
	}
	for _, l := range []*gpu.BindGroupLayout{r.drawLayout, r.texLayout} {
		if l != nil {
			err = errors.Join(err, l.Close())
		}
	}
	if r.lights != nil {
		err = errors.Join(err, r.lights.Close())
	}
	if r.cam != nil {
		err = errors.Join(err, r.cam.Close())
	}
	*r = Renderer{}
	return err
}
