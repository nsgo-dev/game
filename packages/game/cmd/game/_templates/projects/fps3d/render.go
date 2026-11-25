package main

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	// skyHex is the clear color behind the arena.
	skyHex = 0x101826
	// maxDynamicDraws bounds the per-frame draws of enemies, rounds, tracers, pickups and the weapon.
	maxDynamicDraws = 96
	fieldOfView     = 1.2 // vertical, radians
)

// Surface materials, in linear RGB.
var (
	floorMaterial   = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.18, Y: 0.2, Z: 0.22}, Roughness: 0.9}
	wallMaterial    = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.42, Y: 0.45, Z: 0.5}, Roughness: 0.75}
	crateMaterial   = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.55, Y: 0.38, Z: 0.18}, Roughness: 0.6}
	gunMaterial     = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.12, Y: 0.13, Z: 0.15}, Metallic: 0.8, Roughness: 0.35}
	visorMaterial   = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 1, Y: 0.9, Z: 0.5}, Roughness: 0.3, Emissive: math3d.Vec3{X: 1.6, Y: 1.2, Z: 0.4}}
	tracerMaterial  = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.7, Y: 0.95, Z: 1}, Emissive: math3d.Vec3{X: 1.5, Y: 2.2, Z: 2.6}, Roughness: 1}
	roundMaterial   = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.3, Y: 0.9, Z: 1}, Emissive: math3d.Vec3{X: 0.6, Y: 2, Z: 2.4}, Roughness: 1}
	boltMaterial    = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 1, Y: 0.35, Z: 0.2}, Emissive: math3d.Vec3{X: 2.6, Y: 0.7, Z: 0.3}, Roughness: 1}
	healthMaterial  = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.2, Y: 0.9, Z: 0.35}, Emissive: math3d.Vec3{X: 0.2, Y: 1.2, Z: 0.35}, Roughness: 0.4}
	ammoMaterial    = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 1, Y: 0.75, Z: 0.2}, Emissive: math3d.Vec3{X: 1.2, Y: 0.8, Z: 0.15}, Roughness: 0.4}
	hurtEmissive    = math3d.Vec3{X: 2, Y: 2, Z: 2}
	unitCubeSize    = math3d.Vec3{X: 1, Y: 1, Z: 1}
	worldUp         = math3d.Vec3{Y: 1}
	viewmodelOffset = math3d.Vec3{X: 0.26, Y: -0.24, Z: -0.55}
)

// draw is one box to draw this frame: a world matrix for the unit cube and its material.
type draw struct {
	model    math3d.Mat4
	material render3d.PBRMaterial
}

// staticMesh is the level geometry of one material, baked into one mesh in world space.
type staticMesh struct {
	mesh     render3d.Mesh
	material render3d.PBRMaterial
}

// renderer draws a match with render3d's lit PBR pipeline under one light rig. The level is three baked meshes, the floor tiles, the walls and the crates, drawn once each; everything that moves is the unit cube, scaled and placed per draw and refilled from the match every frame.
type renderer struct {
	cube    *render3d.GPUMesh
	camera  *render3d.CameraBuffer
	lights  *render3d.GPULightBuffer
	pipe    *gpu.Pipeline
	buffers []*render3d.PBRDrawBuffer
	level   []staticMesh
	meshes  []*render3d.GPUMesh
	dynamic []draw
	rig     render3d.LightRig
}

// setup uploads the cube, the level meshes, the camera and light buffers, the pipeline and one draw buffer per level mesh and dynamic draw onto dev. Called again after the device is restored, it keeps what is still valid.
func (r *renderer) setup(cx *core.Scope, dev *gpu.Device, m *match) error {
	if dev == nil {
		return fmt.Errorf("render: no GPU device")
	}
	r.level = levelMeshes(m.arena)
	r.rig = lightRig(r.rig, m)
	var err error
	if !r.cube.Valid() {
		if r.cube, err = render3d.UploadMesh(cx, dev, unitCube(), render3d.MeshUpload{Label: "fps3d.cube", Retained: true}); err != nil {
			return fmt.Errorf("render: cube: %w", err)
		}
	}
	r.meshes = r.meshes[:min(len(r.meshes), len(r.level))]
	for i, l := range r.level {
		if i < len(r.meshes) && r.meshes[i].Valid() {
			continue
		}
		g, err := render3d.UploadMesh(cx, dev, l.mesh, render3d.MeshUpload{Label: "fps3d.level", Retained: true})
		if err != nil {
			return fmt.Errorf("render: level mesh %d: %w", i, err)
		}
		if i < len(r.meshes) {
			r.meshes[i] = g
		} else {
			r.meshes = append(r.meshes, g)
		}
	}
	if r.camera == nil || !r.camera.Buffer().Valid() {
		if r.camera, err = render3d.NewCameraBuffer(cx, dev); err != nil {
			return fmt.Errorf("render: camera: %w", err)
		}
	}
	if !r.lights.Valid() {
		if r.lights, err = render3d.NewLightBuffer(cx, dev, r.rig, render3d.LightBufferOptions{Label: "fps3d.lights", Retained: true}); err != nil {
			return fmt.Errorf("render: lights: %w", err)
		}
	}
	want := len(r.level) + maxDynamicDraws
	for i := range want {
		if i < len(r.buffers) && r.buffers[i].Valid() {
			continue
		}
		var shared *gpu.BindGroupLayout
		if i > 0 {
			shared = r.buffers[0].Layout()
		}
		b, err := render3d.NewPBRDrawBuffer(cx, dev, shared, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), wallMaterial), true)
		if err != nil {
			return fmt.Errorf("render: draw buffer %d: %w", i, err)
		}
		if i < len(r.buffers) {
			r.buffers[i] = b
		} else {
			r.buffers = append(r.buffers, b)
		}
	}
	for i, l := range r.level {
		r.buffers[i].Write(dev, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), l.material))
	}
	if r.pipe == nil || !r.pipe.Valid() {
		if r.pipe, err = render3d.NewLitPBRPipeline(cx, dev, r.camera.Layout(), r.lights.Layout(), r.buffers[0].Layout(), gpu.FormatDepth24Plus); err != nil {
			return fmt.Errorf("render: pipeline: %w", err)
		}
	}
	return nil
}

// ready reports whether setup has uploaded everything a frame needs.
func (r *renderer) ready() bool {
	return r.pipe.Valid() && r.cube.Valid() && r.lights.Valid() && r.camera != nil && r.camera.Buffer().Valid() && len(r.meshes) == len(r.level) && len(r.buffers) == len(r.level)+maxDynamicDraws
}

// frame draws m into frame from the player's eye, alpha of the way from the previous step to the current one.
func (r *renderer) frame(frame *gpu.Frame, m *match, alpha float32) error {
	if !r.ready() {
		return nil
	}
	dev := frame.Device()
	cam := playerCamera(m, alpha)
	if w, h := frame.Size(); w > 0 && h > 0 {
		cam.SetViewport(float32(w), float32(h))
	}
	r.camera.WriteCamera(&cam)
	r.rig = lightRig(r.rig, m)
	if err := r.lights.Update(dev, r.rig); err != nil {
		return fmt.Errorf("render: lights: %w", err)
	}
	r.dynamic = dynamicDraws(r.dynamic[:0], m, cam, alpha)
	n := min(len(r.dynamic), maxDynamicDraws)
	for i, d := range r.dynamic[:n] {
		r.buffers[len(r.level)+i].Write(dev, render3d.NewPBRDrawUniform(d.model, d.material))
	}

	pass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "fps3d.forward"})
	pass.ClearColor(core.RGB(skyHex))
	pass.ClearDepth(1)
	pass.SetPipeline(r.pipe)
	pass.SetBindGroup(render3d.CameraGroup, r.camera.BindGroup())
	r.lights.Bind(pass)
	for i, mesh := range r.meshes {
		pass.SetBindGroup(render3d.PBRDrawGroup, r.buffers[i].BindGroup())
		mesh.Draw(pass)
	}
	for _, b := range r.buffers[len(r.level) : len(r.level)+n] {
		pass.SetBindGroup(render3d.PBRDrawGroup, b.BindGroup())
		r.cube.Draw(pass)
	}
	pass.End()
	frame.Submit()
	return nil
}

// playerCamera returns the camera at the player's eye, interpolated alpha of the way into the current step.
func playerCamera(m *match, alpha float32) scene3d.Camera {
	feet := lerpVec(m.player.prevFeet, m.player.ctrl.Pos, alpha)
	eye := toMath(feet).Add(math3d.Vec3{Y: m.arena.Player.EyeHeight})
	return scene3d.Camera{Pos: eye, Rot: viewRotation(m.player.yaw, m.player.pitch), Projection: scene3d.PerspectiveProjection(fieldOfView, 16.0/9, 0.05, 120)}
}

// viewRotation turns -Z by yaw about +Y and then by pitch about the turned +X.
func viewRotation(yaw, pitch float32) math3d.Quat {
	return math3d.QuatFromAxisAngle(worldUp, yaw).Mul(math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, pitch))
}

// levelMeshes bakes the level into one mesh per material: a floor tile under every open cell, a box on every wall cell and a slightly smaller one on every crate cell. Tiles rather than one floor slab keep every triangle small, so none spans from in front of the camera to behind it. NSGo's headless software GPU drops a triangle with a vertex behind the camera instead of clipping it (https://github.com/nsgo-dev/nsgo/issues/2344), and small tiles keep the floor drawn there as on real GPUs.
func levelMeshes(a *Arena) []staticMesh {
	s := a.CellSize
	var floor, walls, crates []math3d.Mat4
	for r, line := range a.Layout {
		for c := 0; c < len(line); c++ {
			x, z := (float32(c)+0.5)*s, (float32(r)+0.5)*s
			switch line[c] {
			case cellWall:
				walls = append(walls, box(math3d.Vec3{X: x, Y: a.WallHeight / 2, Z: z}, math3d.Vec3{X: s, Y: a.WallHeight, Z: s}))
			case cellCrate:
				inset := s * 0.9
				floor = append(floor, box(math3d.Vec3{X: x, Y: -0.25, Z: z}, math3d.Vec3{X: s, Y: 0.5, Z: s}))
				crates = append(crates, box(math3d.Vec3{X: x, Y: a.CrateHeight / 2, Z: z}, math3d.Vec3{X: inset, Y: a.CrateHeight, Z: inset}))
			default:
				floor = append(floor, box(math3d.Vec3{X: x, Y: -0.25, Z: z}, math3d.Vec3{X: s, Y: 0.5, Z: s}))
			}
		}
	}
	var out []staticMesh
	for _, l := range []struct {
		boxes    []math3d.Mat4
		material render3d.PBRMaterial
	}{{floor, floorMaterial}, {walls, wallMaterial}, {crates, crateMaterial}} {
		if len(l.boxes) > 0 {
			out = append(out, staticMesh{mesh: bakeBoxes(l.boxes), material: l.material})
		}
	}
	return out
}

// bakeBoxes returns one mesh holding the unit cube transformed by each of boxes, which scale and translate without rotating, so the cube's face normals stay as they are.
func bakeBoxes(boxes []math3d.Mat4) render3d.Mesh {
	cube := unitCube()
	var m render3d.Mesh
	for _, b := range boxes {
		base := uint32(len(m.Positions))
		for _, p := range cube.Positions {
			m.Positions = append(m.Positions, b.TransformPoint(p))
		}
		m.Normals = append(m.Normals, cube.Normals...)
		m.UVs = append(m.UVs, cube.UVs...)
		for _, i := range cube.Indices {
			m.Indices = append(m.Indices, base+i)
		}
	}
	return m
}

// dynamicDraws appends the enemies, pickups, rounds, tracers and the player's weapon to dst.
func dynamicDraws(dst []draw, m *match, cam scene3d.Camera, alpha float32) []draw {
	// The weapon sits in view space, kicked back by recoil, and is longer for the launcher. It comes first, so it is never the draw maxDynamicDraws drops.
	length := float32(0.5)
	if m.player.weapons[m.player.weapon].cfg.Kind == WeaponProjectile {
		length = 0.65
	}
	offset := viewmodelOffset.Add(math3d.Vec3{Z: m.player.recoil * 0.08})
	if m.player.reloading > 0 {
		offset.Y -= 0.12
	}
	gun := math3d.TRS(cam.Pos, cam.Rot, unitCubeSize).Mul(box(offset, math3d.Vec3{X: 0.1, Y: 0.12, Z: length}))
	dst = append(dst, draw{model: gun, material: gunMaterial})
	for _, e := range m.enemies {
		feet := toMath(lerpVec(e.prevFeet, e.ctrl.Pos, alpha))
		k := e.kind
		body := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: k.Color[0], Y: k.Color[1], Z: k.Color[2]}, Metallic: 0.3, Roughness: 0.5}
		if e.hurtFlash > 0 {
			body.Emissive = hurtEmissive
		}
		turn := math3d.QuatFromAxisAngle(worldUp, e.yaw)
		dst = append(dst,
			draw{model: math3d.TRS(feet.Add(math3d.Vec3{Y: k.Height / 2}), turn, math3d.Vec3{X: 2 * k.Radius, Y: k.Height, Z: 2 * k.Radius}), material: body},
			draw{model: math3d.TRS(feet.Add(turn.RotateVec3(math3d.Vec3{Y: k.Height * 0.8, Z: -k.Radius})), turn, math3d.Vec3{X: k.Radius * 1.4, Y: k.Height * 0.12, Z: 0.08}), material: visorMaterial},
		)
	}
	spin := math3d.QuatFromAxisAngle(worldUp, m.clock*2)
	for _, p := range m.pickups {
		if p.respawn > 0 {
			continue
		}
		mat := healthMaterial
		if p.kind.Effect == EffectAmmo {
			mat = ammoMaterial
		}
		at := toMath(p.pos).Add(math3d.Vec3{Y: 0.6})
		dst = append(dst, draw{model: math3d.TRS(at, spin, math3d.Vec3{X: 0.45, Y: 0.45, Z: 0.45}), material: mat})
	}
	for _, s := range m.shots {
		mat, size := roundMaterial, float32(0.22)
		if s.fromEnemy {
			mat, size = boltMaterial, 0.16
		}
		dst = append(dst, draw{model: box(toMath(s.pos), math3d.Vec3{X: size, Y: size, Z: size}), material: mat})
	}
	for _, t := range m.tracers {
		from, to := toMath(t.from), toMath(t.to)
		length := to.Dist(from)
		if length < 0.01 {
			// A burst: a bright cube that fades with the tracer.
			size := 1.5 * t.life / (tracerLife * 3)
			dst = append(dst, draw{model: box(from, math3d.Vec3{X: size, Y: size, Z: size}), material: roundMaterial})
			continue
		}
		rot := math3d.LookRotation(to.Sub(from), worldUp)
		dst = append(dst, draw{model: math3d.TRS(from.Lerp(to, 0.5), rot, math3d.Vec3{X: 0.03, Y: 0.03, Z: length}), material: tracerMaterial})
	}
	return dst
}

// keyLight is the arena's one directional light.
var keyLight = render3d.DirectionalLight{Direction: math3d.Vec3{X: -0.35, Y: -1, Z: -0.25}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.85}, Intensity: 2.2}

// lightRig lights the arena with a dim sky fill, the key light, a glow over each spawn and a glow over each pickup that is there. It reuses rig's slices, so a frame allocates nothing.
func lightRig(rig render3d.LightRig, m *match) render3d.LightRig {
	rig.Ambient = render3d.AmbientLight{Color: math3d.Vec3{X: 0.6, Y: 0.7, Z: 0.9}, Intensity: 0.35}
	rig.Directional = append(rig.Directional[:0], keyLight)
	rig.Point = rig.Point[:0]
	for _, s := range m.spawns {
		if len(rig.Point) == render3d.MaxPointLights {
			break
		}
		rig.Point = append(rig.Point, render3d.PointLight{Position: toMath(s.pos).Add(math3d.Vec3{Y: 2}), Color: math3d.Vec3{X: 1, Y: 0.3, Z: 0.2}, Intensity: 6, Range: 7})
	}
	for _, p := range m.pickups {
		if p.respawn > 0 || len(rig.Point) == render3d.MaxPointLights {
			continue
		}
		c := healthMaterial.BaseColor
		if p.kind.Effect == EffectAmmo {
			c = ammoMaterial.BaseColor
		}
		rig.Point = append(rig.Point, render3d.PointLight{Position: toMath(p.pos).Add(math3d.Vec3{Y: 1}), Color: c, Intensity: 4, Range: 5})
	}
	return rig
}

// box returns the world matrix that scales the unit cube to size and centers it at center.
func box(center, size math3d.Vec3) math3d.Mat4 {
	return math3d.Translate(center).Mul(math3d.Scale(size))
}

// unitCube returns a cube of side 1 centered at the origin, with outward normals and counter-clockwise faces.
func unitCube() render3d.Mesh {
	var m render3d.Mesh
	faces := [6]struct{ n, u, v math3d.Vec3 }{
		{n: math3d.Vec3{X: 1}, u: math3d.Vec3{Z: -1}, v: math3d.Vec3{Y: 1}},
		{n: math3d.Vec3{X: -1}, u: math3d.Vec3{Z: 1}, v: math3d.Vec3{Y: 1}},
		{n: math3d.Vec3{Y: 1}, u: math3d.Vec3{X: 1}, v: math3d.Vec3{Z: -1}},
		{n: math3d.Vec3{Y: -1}, u: math3d.Vec3{X: 1}, v: math3d.Vec3{Z: 1}},
		{n: math3d.Vec3{Z: 1}, u: math3d.Vec3{X: 1}, v: math3d.Vec3{Y: 1}},
		{n: math3d.Vec3{Z: -1}, u: math3d.Vec3{X: -1}, v: math3d.Vec3{Y: 1}},
	}
	for _, f := range faces {
		base := uint32(len(m.Positions))
		for _, corner := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			p := f.n.Scale(0.5).Add(f.u.Scale(corner[0] * 0.5)).Add(f.v.Scale(corner[1] * 0.5))
			m.Positions = append(m.Positions, p)
			m.Normals = append(m.Normals, f.n)
			m.UVs = append(m.UVs, math3d.Vec2{X: (corner[0] + 1) / 2, Y: (1 - corner[1]) / 2})
		}
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func toMath(v physics3d.Vec3) math3d.Vec3 { return math3d.Vec3{X: v.X, Y: v.Y, Z: v.Z} }

func lerpVec(a, b physics3d.Vec3, t float32) physics3d.Vec3 { return a.Add(b.Sub(a).Scale(t)) }
