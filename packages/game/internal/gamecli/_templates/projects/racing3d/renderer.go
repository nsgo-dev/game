package main

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// skyColor is the color the frame is cleared to, behind everything.
const skyColor = 0x9CC7E6

// renderer draws the race on the GPU view with render3d's lit PBR pipeline: one camera buffer, one light buffer, the meshes sceneMeshes builds and a per-draw buffer for every part. Every resource is retained, so NSGo recreates them after a device loss.
type renderer struct {
	meshes []render3d.Mesh
	static []part

	cam     *render3d.CameraBuffer
	lights  *render3d.GPULightBuffer
	layout  *gpu.BindGroupLayout
	pipe    *gpu.Pipeline
	gpuMesh []*render3d.GPUMesh
	// staticDraws hold the static parts' uniforms and carDraws each car's, the rivals', the player's and then the ghost's.
	staticDraws []*render3d.PBRDrawBuffer
	carDraws    [][carParts]*render3d.PBRDrawBuffer
}

// newRenderer prepares to draw meshes and the static parts.
func newRenderer(meshes []render3d.Mesh, static []part) *renderer {
	return &renderer{meshes: meshes, static: static}
}

// ready reports whether the GPU resources exist and are valid.
func (r *renderer) ready() bool {
	return r.pipe != nil && r.pipe.Valid() && r.cam != nil && r.cam.Buffer().Valid() && r.lights.Valid()
}

// setup creates the GPU resources on dev for cars cars and a ghost, unless they are already there and valid, as they stay after NSGo restores a lost device.
func (r *renderer) setup(cx *core.Scope, dev *gpu.Device, cars int) error {
	if r.ready() && len(r.carDraws) == cars+1 {
		return nil
	}
	cam, err := render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		return fmt.Errorf("camera buffer: %w", err)
	}
	lights, err := render3d.NewLightBuffer(cx, dev, sceneLights(), render3d.LightBufferOptions{Label: "racing.lights", Retained: true})
	if err != nil {
		return fmt.Errorf("light buffer: %w", err)
	}
	gm := make([]*render3d.GPUMesh, len(r.meshes))
	for i, m := range r.meshes {
		if gm[i], err = render3d.UploadMesh(cx, dev, m, render3d.MeshUpload{Label: fmt.Sprintf("racing.mesh.%d", i), Retained: true}); err != nil {
			return fmt.Errorf("mesh %d: %w", i, err)
		}
	}
	var layout *gpu.BindGroupLayout
	draw := func(model math3d.Mat4, mat render3d.PBRMaterial) (*render3d.PBRDrawBuffer, error) {
		b, err := render3d.NewPBRDrawBuffer(cx, dev, layout, render3d.NewPBRDrawUniform(model, mat), true)
		if err == nil && layout == nil {
			layout = b.Layout()
		}
		return b, err
	}
	statics := make([]*render3d.PBRDrawBuffer, len(r.static))
	for i, p := range r.static {
		if statics[i], err = draw(p.model, p.material); err != nil {
			return fmt.Errorf("draw buffer %d: %w", i, err)
		}
	}
	carDraws := make([][carParts]*render3d.PBRDrawBuffer, cars+1)
	for i := range carDraws {
		for k := range carParts {
			if carDraws[i][k], err = draw(math3d.Mat4Identity(), render3d.PBRMaterial{}); err != nil {
				return fmt.Errorf("car %d draw buffer %d: %w", i, k, err)
			}
		}
	}
	pipe, err := render3d.NewLitPBRPipeline(cx, dev, cam.Layout(), lights.Layout(), layout, gpu.FormatDepth24Plus)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	r.cam, r.lights, r.gpuMesh, r.layout, r.staticDraws, r.carDraws, r.pipe = cam, lights, gm, layout, statics, carDraws, pipe
	return nil
}

// draw renders the race into frame from cam, the cars alpha of the way into the next step.
func (r *renderer) draw(frame *gpu.Frame, race *Race, cam scene3d.Camera, alpha float32) {
	if !r.ready() || len(r.carDraws) != len(race.cars)+1 {
		return
	}
	dev := frame.Device()
	if w, h := frame.Size(); w > 0 && h > 0 {
		cam.SetViewport(float32(w), float32(h))
	}
	r.cam.WriteCamera(&cam)

	pass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "racing.forward"})
	pass.ClearColor(core.RGB(skyColor))
	pass.ClearDepth(1)
	pass.SetPipeline(r.pipe)
	pass.SetBindGroup(render3d.CameraGroup, r.cam.BindGroup())
	r.lights.Bind(pass)
	for i, p := range r.static {
		r.drawPart(pass, p.mesh, r.staticDraws[i])
	}
	drawCar := func(slot int, models [carParts]math3d.Mat4, mats [carParts]render3d.PBRMaterial) {
		for k := range carParts {
			r.carDraws[slot][k].Write(dev, render3d.NewPBRDrawUniform(models[k], mats[k]))
			mesh := meshCube
			if k >= 2 {
				mesh = meshWheel
			}
			r.drawPart(pass, mesh, r.carDraws[slot][k])
		}
	}
	for i, c := range race.cars {
		drawCar(i, carModels(c, alpha), carMaterials(c.color, false))
	}
	if pos, rot, ok := race.ghostPose(alpha); ok {
		models := ghostModels(race.player, fromPhys(pos), fromPhysQuat(rot))
		drawCar(len(race.cars), models, carMaterials(math3d.Vec3{}, true))
	}
	pass.End()
	frame.Submit()
}

func (r *renderer) drawPart(pass *gpu.RenderPass, mesh meshID, buf *render3d.PBRDrawBuffer) {
	if m := r.gpuMesh[mesh]; m.Valid() && buf.Valid() {
		pass.SetBindGroup(render3d.PBRDrawGroup, buf.BindGroup())
		m.Draw(pass)
	}
}

// ghostModels places the player's car's parts at the ghost's pose: the ghost records only the chassis, so it borrows the wheels of the player's car where they sit on it now.
func ghostModels(player *car, pos math3d.Vec3, rot math3d.Quat) [carParts]math3d.Mat4 {
	out := carModels(player, 1)
	cur, curRot := fromPhys(player.v.Body.Pos), fromPhysQuat(player.v.Body.Rot)
	inv, _ := math3d.TRS(cur, curRot, math3d.Vec3One()).Inverse()
	to := math3d.TRS(pos, rot, math3d.Vec3One()).Mul(inv)
	for k := range out {
		out[k] = to.Mul(out[k])
	}
	return out
}
