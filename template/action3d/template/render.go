package main

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// skyColor is the color behind a level whose scene sets no backgroundColor.
var skyColor = core.RGB(0x1e293b)

// The camera's lens.
const (
	cameraFOV  = 0.95
	cameraNear = 0.1
	cameraFar  = 120
)

// drawItem is one box to draw: where it is and what it looks like.
type drawItem struct {
	model    math3d.Mat4
	material render3d.PBRMaterial
}

// scene lists what to draw at alpha, the fraction of a fixed step since the last one, which smooths actors and the camera between steps on a display faster than the game's rate.
func (g *game) scene(alpha float32, dst []drawItem) []drawItem {
	t := float32(g.elapsed.Seconds()) + alpha/stepRate
	for _, b := range g.level.blocks {
		dst = append(dst, drawItem{model: boxModel(b.center, b.half), material: matte(b.color)})
	}
	if sink := float32(g.doorSink) / float32(doorOpenTime); sink < 1 {
		d := g.level.door
		center := d.center.Sub(math3d.Vec3{Y: 2 * d.half.Y * sink})
		dst = append(dst, drawItem{model: boxModel(center, d.half), material: matte(d.color)})
	}
	if !g.hasKey {
		bob := math3d.Vec3{Y: 0.12 * sin(t*2.4)}
		spin := math3d.RotateY(t * 1.8)
		at := math3d.Translate(g.level.key.Add(bob)).Mul(spin)
		gold := shiny(g.level.keyColor)
		dst = append(dst,
			drawItem{model: at.Mul(math3d.Scale(math3d.Vec3{X: 0.12, Y: 0.6, Z: 0.12})), material: gold},
			drawItem{model: at.Mul(math3d.Translate(math3d.Vec3{Y: 0.38})).Mul(math3d.Scale(math3d.Vec3{X: 0.36, Y: 0.26, Z: 0.1})), material: gold},
			drawItem{model: at.Mul(math3d.Translate(math3d.Vec3{X: 0.12, Y: -0.2})).Mul(math3d.Scale(math3d.Vec3{X: 0.2, Y: 0.08, Z: 0.1})), material: gold},
		)
	}
	glow := float32(0.4)
	if g.doorOpen {
		glow = 1.6
	}
	crystal := matte(g.level.goalColor)
	crystal.Emissive = crystal.BaseColor.Scale(glow)
	crystal.Roughness = 0.2
	at := math3d.Translate(g.level.goal.Add(math3d.Vec3{Y: 0.9 + 0.1*sin(t*1.5)})).Mul(math3d.RotateY(t * 0.9)).Mul(math3d.RotateX(math.Pi / 4)).Mul(math3d.RotateZ(math.Pi / 4))
	dst = append(dst, drawItem{model: at.Mul(math3d.Scale(math3d.Vec3{X: 0.6, Y: 0.6, Z: 0.6})), material: crystal})

	dst = g.actorParts(g.hero, alpha, t, dst)
	for _, e := range g.enemies {
		dst = g.actorParts(e, alpha, t, dst)
	}
	return dst
}

// actorParts appends the boxes of a's rig, posed and placed between its last two steps by alpha. An actor just hit blinks while it cannot be hit again.
func (g *game) actorParts(a *actor, alpha, t float32, dst []drawItem) []drawItem {
	if a.grace > 0 && !a.down && int(t*12)%2 == 0 {
		return dst
	}
	pos := a.prevPos.Lerp(a.pos, alpha)
	yaw := a.prevYaw + wrapAngle(a.yaw-a.prevYaw)*alpha
	model := math3d.Translate(pos).Mul(math3d.RotateY(yaw))
	g.partsBuf = g.rig.parts(g.partsBuf[:0], model, a.anim.pose)
	for i, m := range g.partsBuf {
		mat := matte(a.paint[g.rig.joints[i].Paint])
		if a.down {
			mat.BaseColor = mat.BaseColor.Scale(0.45)
		}
		dst = append(dst, drawItem{model: m, material: mat})
	}
	return dst
}

// camera is the orbit camera following the hero, interpolated by alpha, for a view of aspect width over height. It moves in front of any wall between it and the hero.
func (g *game) camera(alpha, aspect float32) scene3d.Camera {
	target := g.hero.prevPos.Lerp(g.hero.pos, alpha).Add(math3d.Vec3{Y: cameraLift})
	yaw := g.prevCamYaw + wrapAngle(g.camYaw-g.prevCamYaw)*alpha
	orbit := scene3d.NewOrbitController(target, cameraDistance)
	orbit.SetAngles(yaw, cameraPitch)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{}, cameraFOV, aspect, cameraNear, cameraFar)
	orbit.Apply(cam)
	if hit, ok := g.world.RaycastFirst(p3(target), p3(cam.Pos)); ok {
		orbit.SetDistance(max(0.5, hit.Distance-0.3))
		orbit.Apply(cam)
	}
	return *cam
}

// lights are the level's lamps with a low moon and a dim fill.
func (g *game) lights() render3d.LightRig {
	rig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.55, Y: 0.62, Z: 0.85}, Intensity: 0.35},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: -0.4, Y: -1, Z: -0.55}.Norm(), Color: math3d.Vec3{X: 0.85, Y: 0.9, Z: 1}, Intensity: 1.6}},
	}
	for i, l := range g.level.lamps {
		if i == render3d.MaxPointLights {
			break
		}
		rig.Point = append(rig.Point, render3d.PointLight{Position: l.pos, Color: linear(l.color), Intensity: l.intensity, Range: l.reach})
	}
	return rig
}

// renderer owns the GPU resources that draw a game: one cube mesh every box is drawn with, the camera and light buffers, a per-draw buffer for each box and render3d's lit PBR pipeline.
type renderer struct {
	cube   *render3d.GPUMesh
	camera *render3d.CameraBuffer
	lights *render3d.GPULightBuffer
	draws  []*render3d.PBRDrawBuffer
	pipe   *gpu.Pipeline
	items  []drawItem
	// frames counts the frames drawn, for tests.
	frames int
}

// setup creates the GPU resources on dev, owned by cx, for g's level.
func (r *renderer) setup(cx *core.Scope, dev *gpu.Device, g *game) error {
	*r = renderer{}
	cube, err := render3d.UploadMesh(cx, dev, cubeMesh(), render3d.MeshUpload{Label: "action3d.cube", Retained: true})
	if err != nil {
		return fmt.Errorf("cube mesh: %w", err)
	}
	cam, err := render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		return fmt.Errorf("camera buffer: %w", err)
	}
	lights, err := render3d.NewLightBuffer(cx, dev, g.lights(), render3d.LightBufferOptions{Label: "action3d.lights", Retained: true})
	if err != nil {
		return fmt.Errorf("light buffer: %w", err)
	}
	first, err := render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.PBRDrawUniform{}, true)
	if err != nil {
		return fmt.Errorf("draw buffer: %w", err)
	}
	pipe, err := render3d.NewLitPBRPipeline(cx, dev, cam.Layout(), lights.Layout(), first.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	*r = renderer{cube: cube, camera: cam, lights: lights, draws: []*render3d.PBRDrawBuffer{first}, pipe: pipe}
	// One draw buffer per box the level can show at once: the blocks, the door, the key's three, the goal and every actor's rig.
	need := len(g.level.blocks) + 5 + (1+len(g.enemies))*len(g.rig.joints)
	for len(r.draws) < need {
		d, err := render3d.NewPBRDrawBuffer(cx, dev, first.Layout(), render3d.PBRDrawUniform{}, true)
		if err != nil {
			return fmt.Errorf("draw buffer: %w", err)
		}
		r.draws = append(r.draws, d)
	}
	return nil
}

// ready reports whether setup succeeded and the resources are still live.
func (r *renderer) ready() bool { return r.pipe != nil && r.pipe.Valid() }

// draw renders g at alpha into frame.
func (r *renderer) draw(frame *gpu.Frame, g *game, alpha float32) error {
	if !r.ready() {
		return nil
	}
	dev := frame.Device()
	w, h := frame.Size()
	if w <= 0 || h <= 0 {
		return nil
	}
	cam := g.camera(alpha, float32(w)/float32(h))
	r.camera.WriteCamera(&cam)
	if err := r.lights.Update(dev, g.lights()); err != nil {
		return fmt.Errorf("lights: %w", err)
	}
	r.items = g.scene(alpha, r.items[:0])
	n := min(len(r.items), len(r.draws))
	for i := range n {
		r.draws[i].Write(dev, render3d.NewPBRDrawUniform(r.items[i].model, r.items[i].material))
	}

	pass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "action3d.forward"})
	pass.ClearColor(g.level.sky)
	pass.ClearDepth(1)
	pass.SetPipeline(r.pipe)
	pass.SetBindGroup(render3d.CameraGroup, r.camera.BindGroup())
	r.lights.Bind(pass)
	for i := range n {
		pass.SetBindGroup(render3d.PBRDrawGroup, r.draws[i].BindGroup())
		r.cube.Draw(pass)
	}
	pass.End()
	frame.Submit()
	r.frames++
	return nil
}

// cubeMesh is a unit cube centered on the origin, four vertices per face so each face has its own normal, wound counter-clockwise seen from outside.
func cubeMesh() render3d.Mesh {
	var m render3d.Mesh
	faces := []struct{ n, u, v math3d.Vec3 }{
		{n: math3d.Vec3{X: 1}, u: math3d.Vec3{Z: -1}, v: math3d.Vec3{Y: 1}},
		{n: math3d.Vec3{X: -1}, u: math3d.Vec3{Z: 1}, v: math3d.Vec3{Y: 1}},
		{n: math3d.Vec3{Y: 1}, u: math3d.Vec3{X: 1}, v: math3d.Vec3{Z: -1}},
		{n: math3d.Vec3{Y: -1}, u: math3d.Vec3{X: 1}, v: math3d.Vec3{Z: 1}},
		{n: math3d.Vec3{Z: 1}, u: math3d.Vec3{X: 1}, v: math3d.Vec3{Y: 1}},
		{n: math3d.Vec3{Z: -1}, u: math3d.Vec3{X: -1}, v: math3d.Vec3{Y: 1}},
	}
	for _, f := range faces {
		base := uint32(len(m.Positions))
		for _, c := range [][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			p := f.n.Add(f.u.Scale(c[0])).Add(f.v.Scale(c[1])).Scale(0.5)
			m.Positions = append(m.Positions, p)
			m.Normals = append(m.Normals, f.n)
			m.UVs = append(m.UVs, math3d.Vec2{X: (c[0] + 1) / 2, Y: (1 - c[1]) / 2})
		}
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

// boxModel places the unit cube as the box centered on center with half extents half.
func boxModel(center, half math3d.Vec3) math3d.Mat4 {
	return math3d.Translate(center).Mul(math3d.Scale(half.Scale(2)))
}

// matte is a rough, non-metallic surface of color c.
func matte(c core.Color) render3d.PBRMaterial {
	return render3d.PBRMaterial{BaseColor: linear(c), Alpha: 1, Roughness: 0.85}
}

// shiny is a polished metal of color c that glows a little, so it catches the eye in the dark.
func shiny(c core.Color) render3d.PBRMaterial {
	m := render3d.PBRMaterial{BaseColor: linear(c), Alpha: 1, Metallic: 1, Roughness: 0.3}
	m.Emissive = m.BaseColor.Scale(0.35)
	return m
}

// linear converts a CSS color to linear-light RGB.
func linear(c core.Color) math3d.Vec3 {
	r, g, b, _ := c.Components()
	return render3d.SRGBToLinear(math3d.Vec3{X: float32(r), Y: float32(g), Z: float32(b)})
}

// palette parses the colors of p's slots, which the actors file's validation has already accepted.
func palette(p Paint) map[string]core.Color {
	out := map[string]core.Color{}
	for slot, css := range map[string]string{paintBody: p.Body, paintTrim: p.Trim, paintSkin: p.Skin} {
		out[slot], _ = core.ParseColor(css)
	}
	return out
}
