package main

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/placeholder"
	"github.com/nsgo-dev/game/pkg/render3d"
)

// meshID names one of the meshes the scene draws.
type meshID int

const (
	meshCube meshID = iota
	meshWheel
	meshGround
	meshRoad
	meshCurbRed
	meshCurbWhite
	meshStartLine
	// meshCrate is the first of the crate model's primitives; the rest follow it.
	meshCrate
)

// part is one thing the renderer draws: a mesh, where it is and what it is made of.
type part struct {
	mesh     meshID
	model    math3d.Mat4
	material render3d.PBRMaterial
}

// sceneMeshes builds every mesh the scene draws on the CPU, indexed by meshID, and returns the static parts that never move: the ground, the road, its curbs, the start line, the checkpoint posts and the crates along the road.
func sceneMeshes(c *course) ([]render3d.Mesh, []part, error) {
	half := c.track.Width / 2
	meshes := []render3d.Mesh{
		meshCube:      cubeMesh(),
		meshWheel:     wheelMesh(),
		meshGround:    groundMesh(groundHalf),
		meshRoad:      stripMesh(c, [][2]float32{{-half, half}}, 0.02, -1),
		meshCurbRed:   stripMesh(c, [][2]float32{{-half - 0.9, -half}, {half, half + 0.9}}, 0.03, 0),
		meshCurbWhite: stripMesh(c, [][2]float32{{-half - 0.9, -half}, {half, half + 0.9}}, 0.03, 1),
	}
	// The start line: a white band across the road, a couple of meters deep.
	line := c.at(c.track.Start)
	var lb meshBuilder
	a, b := line.pos.Sub(line.right.Scale(half)), line.pos.Add(line.right.Scale(half))
	depth := line.forward.Scale(1)
	lift := math3d.Vec3{Y: 0.04}
	lb.quad(a.Sub(depth).Add(lift), b.Sub(depth).Add(lift), b.Add(depth).Add(lift), a.Add(depth).Add(lift), math3d.Vec3Up())
	meshes = append(meshes, lb.m)

	crate, err := placeholder.Model("crate.glb")
	if err != nil {
		return nil, nil, fmt.Errorf("load crate: %w", err)
	}
	var crateParts []part
	for _, n := range crate.Nodes {
		if n.Mesh < 0 {
			continue
		}
		for _, prim := range crate.Meshes[n.Mesh].Primitives {
			if prim.Mode != gltf.ModeTriangles {
				continue
			}
			crateParts = append(crateParts, part{mesh: meshID(len(meshes)), model: n.WorldMatrix})
			meshes = append(meshes, render3d.Mesh{Positions: prim.Positions, Normals: prim.Normals, Indices: prim.Indices})
		}
	}

	parts := []part{
		{mesh: meshGround, model: math3d.Mat4Identity(), material: matte(0.16, 0.36, 0.14)},
		{mesh: meshRoad, model: math3d.Mat4Identity(), material: matte(0.09, 0.09, 0.1)},
		{mesh: meshCurbRed, model: math3d.Mat4Identity(), material: matte(0.75, 0.08, 0.06)},
		{mesh: meshCurbWhite, model: math3d.Mat4Identity(), material: matte(0.85, 0.85, 0.85)},
		{mesh: meshStartLine, model: math3d.Mat4Identity(), material: matte(0.9, 0.9, 0.9)},
	}
	// A pair of posts marks every checkpoint, taller and checkered at the start line.
	for i := range c.track.Checkpoints {
		s := c.at(c.track.Start + float32(i)*c.length/float32(c.track.Checkpoints))
		rot := fromPhysQuat(heading(s.forward))
		height, mat := float32(2.4), matte(0.95, 0.65, 0.1)
		if i == 0 {
			height, mat = 4, matte(0.9, 0.9, 0.9)
			// The banner across the start.
			parts = append(parts, part{mesh: meshCube, model: math3d.TRS(s.pos.Add(math3d.Vec3{Y: height}), rot, math3d.Vec3{X: c.track.Width + 3, Y: 0.8, Z: 0.3}), material: matte(0.12, 0.12, 0.14)})
		}
		for _, side := range []float32{-1, 1} {
			p := s.pos.Add(s.right.Scale(side * (half + 1.5))).Add(math3d.Vec3{Y: height / 2})
			parts = append(parts, part{mesh: meshCube, model: math3d.TRS(p, rot, math3d.Vec3{X: 0.4, Y: height, Z: 0.4}), material: mat})
		}
	}
	// Stacks of crates at ten places around the loop, on the outside of the bend, where a car running wide would meet them.
	for i := 0; i < len(c.samples); i += len(c.samples) / 10 {
		s := c.samples[i]
		side := float32(1)
		if next := c.samples[c.wrap(i+4)].forward; s.forward.Cross(next).Y < 0 {
			side = -1
		}
		base := s.pos.Add(s.right.Scale(side * (half + 4)))
		for k, off := range []math3d.Vec3{{}, {X: 1.1}, {X: 0.55, Y: 1}} {
			at := math3d.TRS(base.Add(s.right.Scale(off.X)).Add(math3d.Vec3{Y: off.Y}), fromPhysQuat(heading(s.forward)), math3d.Vec3One())
			for _, cp := range crateParts {
				parts = append(parts, part{mesh: cp.mesh, model: at.Mul(cp.model), material: matte(0.55-0.05*float32(k), 0.36, 0.18)})
			}
		}
	}
	return meshes, parts, nil
}

// carParts are the parts of one car: its body, cabin and wheels.
const carParts = 6

// carModels returns the model matrices of c's parts alpha of the way from its previous step to its last: the body, the cabin, then its first four wheels.
func carModels(c *car, alpha float32) [carParts]math3d.Mat4 {
	pos, rot := carPose(c, alpha)
	cfg := c.v.Config()
	chassis := math3d.TRS(pos, rot, math3d.Vec3One())
	he := cfg.ChassisHalfExtents
	off := math3d.Vec3{X: cfg.ChassisOffset[0], Y: cfg.ChassisOffset[1], Z: cfg.ChassisOffset[2]}
	var out [carParts]math3d.Mat4
	out[0] = chassis.Mul(math3d.TRS(off, math3d.QuatIdentity(), math3d.Vec3{X: 2 * he[0], Y: 2 * he[1], Z: 2 * he[2]}))
	out[1] = chassis.Mul(math3d.TRS(off.Add(math3d.Vec3{Y: he[1] + 0.22, Z: 0.25}), math3d.QuatIdentity(), math3d.Vec3{X: 1.5 * he[0], Y: 0.45, Z: he[2]}))
	// The wheels move with the last step's suspension and steering, shifted by how far the chassis has been interpolated back.
	shift := pos.Sub(fromPhys(c.v.Body.Pos))
	for i := range min(4, len(c.v.Wheels)) {
		wp, wr := c.v.WheelTransform(i)
		r := c.v.Wheels[i].Config.Radius
		out[2+i] = math3d.TRS(fromPhys(wp).Add(shift), fromPhysQuat(wr), math3d.Vec3{X: 0.3, Y: r, Z: r})
	}
	return out
}

// carMaterials returns the materials of a car's parts in paint color: glossy paint, a dark cabin and rubber wheels. A ghost glows instead.
func carMaterials(color math3d.Vec3, ghost bool) [carParts]render3d.PBRMaterial {
	paint := render3d.PBRMaterial{BaseColor: color, Metallic: 0.3, Roughness: 0.35}
	cabin := matte(0.05, 0.06, 0.08)
	tire := matte(0.03, 0.03, 0.03)
	if ghost {
		glow := math3d.Vec3{X: 0.35, Y: 0.75, Z: 0.95}
		paint = render3d.PBRMaterial{BaseColor: glow.Scale(0.3), Roughness: 1, Emissive: glow.Scale(0.6)}
		cabin, tire = paint, paint
	}
	return [carParts]render3d.PBRMaterial{paint, cabin, tire, tire, tire, tire}
}

// matte is a rough, non-metallic material of linear color r, g, b.
func matte(r, g, b float32) render3d.PBRMaterial {
	return render3d.PBRMaterial{BaseColor: math3d.Vec3{X: r, Y: g, Z: b}, Roughness: 0.9}
}

// sceneLights are the scene's lights: a low afternoon sun and a sky fill.
func sceneLights() render3d.LightRig {
	return render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.35, Y: 0.42, Z: 0.55}, Intensity: 0.6},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.4, Y: -0.75, Z: -0.5},
			Color:     math3d.Vec3{X: 1, Y: 0.93, Z: 0.8},
			Intensity: 3,
		}},
	}
}
