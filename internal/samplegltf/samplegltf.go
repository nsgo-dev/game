// Package samplegltf generates and validates the deterministic glTF 2.0 sample models of the placeholder asset pack (package placeholder's models folder), which the 3D Blender pipeline documentation describes.
package samplegltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// GLB container and glTF 2.0 constants.
const (
	GLBMagic      uint32 = 0x46546C67 // "glTF"
	GLBVersion    uint32 = 2
	ChunkTypeJSON uint32 = 0x4E4F534A // "JSON"
	ChunkTypeBIN  uint32 = 0x004E4942 // "BIN\0"

	ComponentUnsignedByte  = 5121
	ComponentUnsignedShort = 5123
	ComponentFloat         = 5126

	TargetArrayBuffer        = 34962
	TargetElementArrayBuffer = 34963

	ModeTriangles = 4
)

// Document is the subset of the glTF 2.0 JSON schema used by the sample models.
type Document struct {
	Asset       Asset        `json:"asset"`
	Scene       *int         `json:"scene,omitempty"`
	Scenes      []Scene      `json:"scenes,omitempty"`
	Nodes       []Node       `json:"nodes,omitempty"`
	Meshes      []Mesh       `json:"meshes,omitempty"`
	Materials   []Material   `json:"materials,omitempty"`
	Skins       []Skin       `json:"skins,omitempty"`
	Animations  []Animation  `json:"animations,omitempty"`
	Accessors   []Accessor   `json:"accessors,omitempty"`
	BufferViews []BufferView `json:"bufferViews,omitempty"`
	Buffers     []Buffer     `json:"buffers,omitempty"`
}

// Asset identifies the glTF version and generator.
type Asset struct {
	Version   string `json:"version"`
	Generator string `json:"generator,omitempty"`
}

// Scene lists root nodes of a glTF scene.
type Scene struct {
	Name  string `json:"name,omitempty"`
	Nodes []int  `json:"nodes,omitempty"`
}

// Node represents an object or joint in the scene hierarchy.
type Node struct {
	Name        string      `json:"name,omitempty"`
	Children    []int       `json:"children,omitempty"`
	Mesh        *int        `json:"mesh,omitempty"`
	Skin        *int        `json:"skin,omitempty"`
	Translation *[3]float64 `json:"translation,omitempty"`
	Rotation    *[4]float64 `json:"rotation,omitempty"`
	Scale       *[3]float64 `json:"scale,omitempty"`
}

// Mesh groups one or more renderable primitives.
type Mesh struct {
	Name       string      `json:"name,omitempty"`
	Primitives []Primitive `json:"primitives"`
}

// Primitive references vertex attributes, optional indices, and a material.
type Primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices,omitempty"`
	Material   *int           `json:"material,omitempty"`
	Mode       *int           `json:"mode,omitempty"`
}

// Material defines PBR surface properties.
type Material struct {
	Name                 string                `json:"name,omitempty"`
	PBRMetallicRoughness *PBRMetallicRoughness `json:"pbrMetallicRoughness,omitempty"`
	DoubleSided          bool                  `json:"doubleSided"`
}

// PBRMetallicRoughness defines the glTF 2.0 metallic-roughness material model.
type PBRMetallicRoughness struct {
	BaseColorFactor [4]float64 `json:"baseColorFactor"`
	MetallicFactor  float64    `json:"metallicFactor"`
	RoughnessFactor float64    `json:"roughnessFactor"`
}

// Skin binds a mesh to a skeleton joint hierarchy.
type Skin struct {
	Name                string `json:"name,omitempty"`
	InverseBindMatrices *int   `json:"inverseBindMatrices,omitempty"`
	Skeleton            *int   `json:"skeleton,omitempty"`
	Joints              []int  `json:"joints"`
}

// Animation stores keyframe channels, samplers, and NSGo clip events.
type Animation struct {
	Name     string             `json:"name,omitempty"`
	Channels []AnimationChannel `json:"channels"`
	Samplers []AnimationSampler `json:"samplers"`
	Extras   *AnimationExtras   `json:"extras,omitempty"`
}

// AnimationChannel binds a sampler to a node property path.
type AnimationChannel struct {
	Sampler int             `json:"sampler"`
	Target  AnimationTarget `json:"target"`
}

// AnimationTarget identifies the animated node and TRS/weights path.
type AnimationTarget struct {
	Node *int   `json:"node,omitempty"`
	Path string `json:"path"`
}

// AnimationSampler maps keyframe input timestamps to output values.
type AnimationSampler struct {
	Input         int    `json:"input"`
	Interpolation string `json:"interpolation,omitempty"`
	Output        int    `json:"output"`
}

// AnimationExtras holds custom metadata on an animation clip.
type AnimationExtras struct {
	Events []ClipEvent `json:"events,omitempty"`
}

// ClipEvent marks a named gameplay event at a timestamp in seconds.
type ClipEvent struct {
	Time float64 `json:"time"`
	Name string  `json:"name"`
}

// Accessor describes a typed view into a BufferView.
type Accessor struct {
	BufferView    *int      `json:"bufferView,omitempty"`
	ByteOffset    int       `json:"byteOffset,omitempty"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float64 `json:"min,omitempty"`
	Max           []float64 `json:"max,omitempty"`
}

// BufferView slices a byte range out of a Buffer.
type BufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

// Buffer holds binary geometry, skinning, or animation data.
type Buffer struct {
	URI        string `json:"uri,omitempty"`
	ByteLength int    `json:"byteLength"`
}

// WriteSamples writes crate.glb, robot_arm.gltf and gem.glb into dir.
func WriteSamples(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sample dir: %w", err)
	}
	crate, err := GenerateCrateGLB()
	if err != nil {
		return fmt.Errorf("generate crate.glb: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crate.glb"), crate, 0o644); err != nil {
		return fmt.Errorf("write crate.glb: %w", err)
	}
	arm, err := GenerateRobotArmGLTF()
	if err != nil {
		return fmt.Errorf("generate robot_arm.gltf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "robot_arm.gltf"), arm, 0o644); err != nil {
		return fmt.Errorf("write robot_arm.gltf: %w", err)
	}
	gem, err := GenerateGemGLB()
	if err != nil {
		return fmt.Errorf("generate gem.glb: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gem.glb"), gem, 0o644); err != nil {
		return fmt.Errorf("write gem.glb: %w", err)
	}
	return nil
}

// GenerateGemGLB builds a deterministic binary glTF 2.0 (GLB) pickup gem: a flat-shaded hexagonal bipyramid 0.8 m across and 1 m tall, pivot at its bottom point (Y in [0, 1]), with CCW outward faces, one normal per face and a glossy emerald PBR material. It uses only exactly rounded arithmetic (square roots, and products rounded before they are summed), so it writes the same bytes on every architecture.
func GenerateGemGLB() ([]byte, error) {
	const (
		radius  = 0.4
		girdleY = 0.6
		topY    = 1.0
	)
	half := math.Sqrt(3) / 2
	ring := [6][3]float64{
		{radius, girdleY, 0},
		{radius * 0.5, girdleY, float64(radius * half)},
		{-radius * 0.5, girdleY, float64(radius * half)},
		{-radius, girdleY, 0},
		{-radius * 0.5, girdleY, -float64(radius * half)},
		{radius * 0.5, girdleY, -float64(radius * half)},
	}
	top := [3]float64{0, topY, 0}
	bottom := [3]float64{0, 0, 0}
	center := [3]float64{0, girdleY, 0}

	var positions, normals [][3]float32
	var indices []uint16
	addFace := func(a, b, c [3]float64) {
		n := faceNormal(a, b, c)
		centroid := [3]float64{(a[0] + b[0] + c[0]) / 3, (a[1] + b[1] + c[1]) / 3, (a[2] + b[2] + c[2]) / 3}
		out := float64(n[0]*(centroid[0]-center[0])) + float64(n[1]*(centroid[1]-center[1])) + float64(n[2]*(centroid[2]-center[2]))
		if out < 0 {
			b, c = c, b
			n = [3]float64{-n[0], -n[1], -n[2]}
		}
		base := uint16(len(positions))
		for _, v := range [][3]float64{a, b, c} {
			positions = append(positions, [3]float32{float32(v[0]), float32(v[1]), float32(v[2])})
			normals = append(normals, [3]float32{float32(n[0]), float32(n[1]), float32(n[2])})
		}
		indices = append(indices, base, base+1, base+2)
	}
	for i := range ring {
		next := ring[(i+1)%len(ring)]
		addFace(top, ring[i], next)
		addFace(bottom, next, ring[i])
	}

	var bb binBuilder
	posBV := bb.addVec3(positions, TargetArrayBuffer)
	normBV := bb.addVec3(normals, TargetArrayBuffer)
	idxBV := bb.addUint16(indices, TargetElementArrayBuffer)
	bin := bb.bytes()
	posMin, posMax := vec3Bounds(positions)

	doc := &Document{
		Asset:  Asset{Version: "2.0", Generator: "nsgo-game samplegltf"},
		Scene:  intPtr(0),
		Scenes: []Scene{{Name: "Scene", Nodes: []int{0}}},
		Nodes:  []Node{{Name: "Gem", Mesh: intPtr(0)}},
		Meshes: []Mesh{{
			Name: "GemMesh",
			Primitives: []Primitive{{
				Attributes: map[string]int{"POSITION": 0, "NORMAL": 1},
				Indices:    intPtr(2),
				Material:   intPtr(0),
				Mode:       intPtr(ModeTriangles),
			}},
		}},
		Materials: []Material{{
			Name: "GemMaterial",
			PBRMetallicRoughness: &PBRMetallicRoughness{
				BaseColorFactor: [4]float64{0.13, 0.77, 0.45, 1},
				MetallicFactor:  0,
				RoughnessFactor: 0.2,
			},
		}},
		Accessors: []Accessor{
			{BufferView: intPtr(posBV), ComponentType: ComponentFloat, Count: len(positions), Type: "VEC3", Min: posMin, Max: posMax},
			{BufferView: intPtr(normBV), ComponentType: ComponentFloat, Count: len(normals), Type: "VEC3"},
			{BufferView: intPtr(idxBV), ComponentType: ComponentUnsignedShort, Count: len(indices), Type: "SCALAR"},
		},
		BufferViews: bb.views,
		Buffers:     []Buffer{{ByteLength: len(bin)}},
	}
	return PackGLB(doc, bin)
}

// faceNormal returns the unit normal of the triangle a, b, c by the right-hand rule. Each product is converted to float64 before it is summed, which forbids the compiler from fusing it into a multiply-add, so the result is the same on every architecture.
func faceNormal(a, b, c [3]float64) [3]float64 {
	u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	n := [3]float64{
		float64(u[1]*v[2]) - float64(u[2]*v[1]),
		float64(u[2]*v[0]) - float64(u[0]*v[2]),
		float64(u[0]*v[1]) - float64(u[1]*v[0]),
	}
	l := math.Sqrt(float64(n[0]*n[0]) + float64(n[1]*n[1]) + float64(n[2]*n[2]))
	return [3]float64{n[0] / l, n[1] / l, n[2] / l}
}

// GenerateCrateGLB builds a deterministic binary glTF 2.0 (GLB) 1 m cube crate:
// right-handed +X right, +Y up, -Z forward, pivot at bottom center (Y in [0, 1]),
// CCW front faces, outward normals, UVs, and a PBR metallicRoughness material.
func GenerateCrateGLB() ([]byte, error) {
	type face struct {
		normal [3]float32
		verts  [4][3]float32
	}
	faces := []face{
		// +Z (back relative to -Z forward)
		{
			normal: [3]float32{0, 0, 1},
			verts:  [4][3]float32{{-0.5, 0, 0.5}, {0.5, 0, 0.5}, {0.5, 1, 0.5}, {-0.5, 1, 0.5}},
		},
		// -Z (front facing -Z forward)
		{
			normal: [3]float32{0, 0, -1},
			verts:  [4][3]float32{{0.5, 0, -0.5}, {-0.5, 0, -0.5}, {-0.5, 1, -0.5}, {0.5, 1, -0.5}},
		},
		// +X (right)
		{
			normal: [3]float32{1, 0, 0},
			verts:  [4][3]float32{{0.5, 0, 0.5}, {0.5, 0, -0.5}, {0.5, 1, -0.5}, {0.5, 1, 0.5}},
		},
		// -X (left)
		{
			normal: [3]float32{-1, 0, 0},
			verts:  [4][3]float32{{-0.5, 0, -0.5}, {-0.5, 0, 0.5}, {-0.5, 1, 0.5}, {-0.5, 1, -0.5}},
		},
		// +Y (top)
		{
			normal: [3]float32{0, 1, 0},
			verts:  [4][3]float32{{-0.5, 1, 0.5}, {0.5, 1, 0.5}, {0.5, 1, -0.5}, {-0.5, 1, -0.5}},
		},
		// -Y (bottom)
		{
			normal: [3]float32{0, -1, 0},
			verts:  [4][3]float32{{-0.5, 0, -0.5}, {0.5, 0, -0.5}, {0.5, 0, 0.5}, {-0.5, 0, 0.5}},
		},
	}

	uvQuad := [4][2]float32{{0, 1}, {1, 1}, {1, 0}, {0, 0}}

	var positions [][3]float32
	var normals [][3]float32
	var uvs [][2]float32
	var indices []uint16

	for _, f := range faces {
		base := uint16(len(positions))
		for i := 0; i < 4; i++ {
			positions = append(positions, f.verts[i])
			normals = append(normals, f.normal)
			uvs = append(uvs, uvQuad[i])
		}
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}

	var bb binBuilder
	posBV := bb.addVec3(positions, TargetArrayBuffer)
	normBV := bb.addVec3(normals, TargetArrayBuffer)
	uvBV := bb.addVec2(uvs, TargetArrayBuffer)
	idxBV := bb.addUint16(indices, TargetElementArrayBuffer)
	bin := bb.bytes()

	posMin, posMax := vec3Bounds(positions)

	doc := &Document{
		Asset: Asset{
			Version:   "2.0",
			Generator: "nsgo-game samplegltf",
		},
		Scene: intPtr(0),
		Scenes: []Scene{
			{Name: "Scene", Nodes: []int{0}},
		},
		Nodes: []Node{
			{Name: "Crate", Mesh: intPtr(0)},
		},
		Meshes: []Mesh{
			{
				Name: "CrateMesh",
				Primitives: []Primitive{
					{
						Attributes: map[string]int{
							"POSITION":   0,
							"NORMAL":     1,
							"TEXCOORD_0": 2,
						},
						Indices:  intPtr(3),
						Material: intPtr(0),
						Mode:     intPtr(ModeTriangles),
					},
				},
			},
		},
		Materials: []Material{
			{
				Name: "CrateMaterial",
				PBRMetallicRoughness: &PBRMetallicRoughness{
					BaseColorFactor: [4]float64{0.72, 0.52, 0.32, 1},
					MetallicFactor:  0.05,
					RoughnessFactor: 0.78,
				},
				DoubleSided: false,
			},
		},
		Accessors: []Accessor{
			{
				BufferView:    intPtr(posBV),
				ComponentType: ComponentFloat,
				Count:         len(positions),
				Type:          "VEC3",
				Min:           posMin,
				Max:           posMax,
			},
			{
				BufferView:    intPtr(normBV),
				ComponentType: ComponentFloat,
				Count:         len(normals),
				Type:          "VEC3",
			},
			{
				BufferView:    intPtr(uvBV),
				ComponentType: ComponentFloat,
				Count:         len(uvs),
				Type:          "VEC2",
			},
			{
				BufferView:    intPtr(idxBV),
				ComponentType: ComponentUnsignedShort,
				Count:         len(indices),
				Type:          "SCALAR",
			},
		},
		BufferViews: bb.views,
		Buffers: []Buffer{
			{ByteLength: len(bin)},
		},
	}

	return PackGLB(doc, bin)
}

// GenerateRobotArmGLTF builds a deterministic embedded-buffer glTF 2.0 JSON
// file containing a 2-bone skinned and animated robot arm (UpperArm -> Forearm)
// with inverseBindMatrices, JOINTS_0, WEIGHTS_0, a Wave rotation animation, and
// extras.events clip markers.
func GenerateRobotArmGLTF() ([]byte, error) {
	const halfW float32 = 0.125
	const midY float32 = 0.5
	const topY float32 = 1.0

	var positions [][3]float32
	var normals [][3]float32
	var uvs [][2]float32
	var joints [][4]uint8
	var weights [][4]float32
	var indices []uint16

	skinForY := func(y float32) ([4]uint8, [4]float32) {
		switch y {
		case 0:
			return [4]uint8{0, 0, 0, 0}, [4]float32{1, 0, 0, 0}
		case midY:
			return [4]uint8{0, 1, 0, 0}, [4]float32{0.5, 0.5, 0, 0}
		default:
			return [4]uint8{1, 0, 0, 0}, [4]float32{1, 0, 0, 0}
		}
	}

	// Four vertical side faces, each split cleanly at y = 0.5 (the elbow joint)
	// so the shared elbow ring bends smoothly between UpperArm and Forearm.
	type sideSpec struct {
		normal [3]float32
		leftXZ [2]float32
		rghtXZ [2]float32
	}
	sides := []sideSpec{
		// +Z face
		{normal: [3]float32{0, 0, 1}, leftXZ: [2]float32{-halfW, halfW}, rghtXZ: [2]float32{halfW, halfW}},
		// -Z face
		{normal: [3]float32{0, 0, -1}, leftXZ: [2]float32{halfW, -halfW}, rghtXZ: [2]float32{-halfW, -halfW}},
		// +X face
		{normal: [3]float32{1, 0, 0}, leftXZ: [2]float32{halfW, halfW}, rghtXZ: [2]float32{halfW, -halfW}},
		// -X face
		{normal: [3]float32{-1, 0, 0}, leftXZ: [2]float32{-halfW, -halfW}, rghtXZ: [2]float32{-halfW, halfW}},
	}
	rings := []struct {
		y float32
		v float32
	}{
		{y: 0, v: 1},
		{y: midY, v: 0.5},
		{y: topY, v: 0},
	}

	for _, s := range sides {
		base := uint16(len(positions))
		for _, r := range rings {
			j, w := skinForY(r.y)
			positions = append(positions,
				[3]float32{s.leftXZ[0], r.y, s.leftXZ[1]},
				[3]float32{s.rghtXZ[0], r.y, s.rghtXZ[1]},
			)
			normals = append(normals, s.normal, s.normal)
			uvs = append(uvs, [2]float32{0, r.v}, [2]float32{1, r.v})
			joints = append(joints, j, j)
			weights = append(weights, w, w)
		}
		// Lower segment quad: (0,1,3,2), upper segment quad: (2,3,5,4).
		indices = append(indices,
			base+0, base+1, base+3, base+0, base+3, base+2,
			base+2, base+3, base+5, base+2, base+5, base+4,
		)
	}

	// Bottom cap (y = 0, normal -Y, bound to UpperArm).
	addCap := func(normal [3]float32, verts [4][3]float32) {
		base := uint16(len(positions))
		uvQuad := [4][2]float32{{0, 1}, {1, 1}, {1, 0}, {0, 0}}
		for i, v := range verts {
			j, w := skinForY(v[1])
			positions = append(positions, v)
			normals = append(normals, normal)
			uvs = append(uvs, uvQuad[i])
			joints = append(joints, j)
			weights = append(weights, w)
		}
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}
	addCap([3]float32{0, -1, 0}, [4][3]float32{
		{-halfW, 0, -halfW}, {halfW, 0, -halfW}, {halfW, 0, halfW}, {-halfW, 0, halfW},
	})
	addCap([3]float32{0, 1, 0}, [4][3]float32{
		{-halfW, topY, halfW}, {halfW, topY, halfW}, {halfW, topY, -halfW}, {-halfW, topY, -halfW},
	})

	// Inverse bind matrices (column-major):
	// Joint 0 (UpperArm) bind world position = (0, 0, 0)    -> IBM = I
	// Joint 1 (Forearm)  bind world position = (0, 0.5, 0)  -> IBM = T(0, -0.5, 0)
	ibms := [][16]float32{
		{
			1, 0, 0, 0,
			0, 1, 0, 0,
			0, 0, 1, 0,
			0, 0, 0, 1,
		},
		{
			1, 0, 0, 0,
			0, 1, 0, 0,
			0, 0, 1, 0,
			0, -midY, 0, 1,
		},
	}

	// Animation "Wave": 1.0 s clip rotating UpperArm (+15 deg Z) and Forearm (+60 deg Z).
	times := []float32{0, 0.5, 1}
	upperRot := [][4]float32{
		quatZ(0),
		quatZ(15),
		quatZ(0),
	}
	forearmRot := [][4]float32{
		quatZ(0),
		quatZ(60),
		quatZ(0),
	}

	var bb binBuilder
	posBV := bb.addVec3(positions, TargetArrayBuffer)
	normBV := bb.addVec3(normals, TargetArrayBuffer)
	uvBV := bb.addVec2(uvs, TargetArrayBuffer)
	jointBV := bb.addU8Vec4(joints, TargetArrayBuffer)
	weightBV := bb.addVec4(weights, TargetArrayBuffer)
	idxBV := bb.addUint16(indices, TargetElementArrayBuffer)
	// Non-vertex/index bufferViews omit target per the glTF 2.0 specification.
	ibmBV := bb.addMat4(ibms, 0)
	timeBV := bb.addFloat32(times, 0)
	upperRotBV := bb.addVec4(upperRot, 0)
	forearmRotBV := bb.addVec4(forearmRot, 0)
	bin := bb.bytes()

	posMin, posMax := vec3Bounds(positions)
	forearmPos := [3]float64{0, float64(midY), 0}

	doc := &Document{
		Asset: Asset{
			Version:   "2.0",
			Generator: "nsgo-game samplegltf",
		},
		Scene: intPtr(0),
		Scenes: []Scene{
			{Name: "Scene", Nodes: []int{0}},
		},
		Nodes: []Node{
			{Name: "ArmRoot", Children: []int{1, 2}},
			{Name: "ArmMesh", Mesh: intPtr(0), Skin: intPtr(0)},
			{Name: "UpperArm", Children: []int{3}},
			{Name: "Forearm", Translation: &forearmPos},
		},
		Meshes: []Mesh{
			{
				Name: "RobotArmMesh",
				Primitives: []Primitive{
					{
						Attributes: map[string]int{
							"POSITION":   0,
							"NORMAL":     1,
							"TEXCOORD_0": 2,
							"JOINTS_0":   3,
							"WEIGHTS_0":  4,
						},
						Indices:  intPtr(5),
						Material: intPtr(0),
						Mode:     intPtr(ModeTriangles),
					},
				},
			},
		},
		Materials: []Material{
			{
				Name: "ArmMetal",
				PBRMetallicRoughness: &PBRMetallicRoughness{
					BaseColorFactor: [4]float64{0.36, 0.44, 0.52, 1},
					MetallicFactor:  0.75,
					RoughnessFactor: 0.35,
				},
				DoubleSided: false,
			},
		},
		Skins: []Skin{
			{
				Name:                "ArmSkin",
				InverseBindMatrices: intPtr(6),
				Skeleton:            intPtr(2),
				Joints:              []int{2, 3},
			},
		},
		Animations: []Animation{
			{
				Name: "Wave",
				Channels: []AnimationChannel{
					{Sampler: 0, Target: AnimationTarget{Node: intPtr(2), Path: "rotation"}},
					{Sampler: 1, Target: AnimationTarget{Node: intPtr(3), Path: "rotation"}},
				},
				Samplers: []AnimationSampler{
					{Input: 7, Interpolation: "LINEAR", Output: 8},
					{Input: 7, Interpolation: "LINEAR", Output: 9},
				},
				Extras: &AnimationExtras{
					Events: []ClipEvent{
						{Time: 0, Name: "wave_start"},
						{Time: 0.5, Name: "wave_apex"},
						{Time: 1, Name: "wave_end"},
					},
				},
			},
		},
		Accessors: []Accessor{
			{
				BufferView:    intPtr(posBV),
				ComponentType: ComponentFloat,
				Count:         len(positions),
				Type:          "VEC3",
				Min:           posMin,
				Max:           posMax,
			},
			{
				BufferView:    intPtr(normBV),
				ComponentType: ComponentFloat,
				Count:         len(normals),
				Type:          "VEC3",
			},
			{
				BufferView:    intPtr(uvBV),
				ComponentType: ComponentFloat,
				Count:         len(uvs),
				Type:          "VEC2",
			},
			{
				BufferView:    intPtr(jointBV),
				ComponentType: ComponentUnsignedByte,
				Count:         len(joints),
				Type:          "VEC4",
			},
			{
				BufferView:    intPtr(weightBV),
				ComponentType: ComponentFloat,
				Count:         len(weights),
				Type:          "VEC4",
			},
			{
				BufferView:    intPtr(idxBV),
				ComponentType: ComponentUnsignedShort,
				Count:         len(indices),
				Type:          "SCALAR",
			},
			{
				BufferView:    intPtr(ibmBV),
				ComponentType: ComponentFloat,
				Count:         len(ibms),
				Type:          "MAT4",
			},
			{
				BufferView:    intPtr(timeBV),
				ComponentType: ComponentFloat,
				Count:         len(times),
				Type:          "SCALAR",
				Min:           []float64{0},
				Max:           []float64{1},
			},
			{
				BufferView:    intPtr(upperRotBV),
				ComponentType: ComponentFloat,
				Count:         len(upperRot),
				Type:          "VEC4",
			},
			{
				BufferView:    intPtr(forearmRotBV),
				ComponentType: ComponentFloat,
				Count:         len(forearmRot),
				Type:          "VEC4",
			},
		},
		BufferViews: bb.views,
		Buffers: []Buffer{
			{
				URI:        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin),
				ByteLength: len(bin),
			},
		},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal gltf: %w", err)
	}
	return append(out, '\n'), nil
}

// PackGLB encodes doc and bin into a 4-byte-aligned GLB 2.0 container.
func PackGLB(doc *Document, bin []byte) ([]byte, error) {
	jsonRaw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal glb json: %w", err)
	}
	jsonPadded := padBytes(jsonRaw, 0x20)
	binPadded := padBytes(bin, 0x00)

	totalLen := 12 + 8 + len(jsonPadded) + 8 + len(binPadded)
	var out bytes.Buffer
	out.Grow(totalLen)

	// 12-byte GLB header.
	_ = binary.Write(&out, binary.LittleEndian, GLBMagic)
	_ = binary.Write(&out, binary.LittleEndian, GLBVersion)
	_ = binary.Write(&out, binary.LittleEndian, uint32(totalLen))

	// Chunk 0: JSON.
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(jsonPadded)))
	_ = binary.Write(&out, binary.LittleEndian, ChunkTypeJSON)
	out.Write(jsonPadded)

	// Chunk 1: BIN.
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(binPadded)))
	_ = binary.Write(&out, binary.LittleEndian, ChunkTypeBIN)
	out.Write(binPadded)

	return out.Bytes(), nil
}

func padBytes(src []byte, pad byte) []byte {
	rem := len(src) % 4
	if rem == 0 {
		return src
	}
	out := make([]byte, len(src)+(4-rem))
	copy(out, src)
	for i := len(src); i < len(out); i++ {
		out[i] = pad
	}
	return out
}

func intPtr(v int) *int { return &v }

func quatZ(deg float64) [4]float32 {
	half := deg * math.Pi / 360
	return [4]float32{0, 0, float32(math.Sin(half)), float32(math.Cos(half))}
}

func vec3Bounds(vs [][3]float32) (minOut, maxOut []float64) {
	mn := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	mx := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, v := range vs {
		for i := 0; i < 3; i++ {
			f := float64(v[i])
			if f < mn[i] {
				mn[i] = f
			}
			if f > mx[i] {
				mx[i] = f
			}
		}
	}
	return []float64{mn[0], mn[1], mn[2]}, []float64{mx[0], mx[1], mx[2]}
}

// binBuilder appends typed little-endian slices on 4-byte boundaries and records
// their BufferViews.
type binBuilder struct {
	buf   bytes.Buffer
	views []BufferView
}

func (b *binBuilder) align4() {
	for b.buf.Len()%4 != 0 {
		b.buf.WriteByte(0)
	}
}

func (b *binBuilder) record(start, target int) int {
	idx := len(b.views)
	b.views = append(b.views, BufferView{
		Buffer:     0,
		ByteOffset: start,
		ByteLength: b.buf.Len() - start,
		Target:     target,
	})
	return idx
}

func (b *binBuilder) addFloat32(vs []float32, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	return b.record(start, target)
}

func (b *binBuilder) addVec2(vs [][2]float32, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	return b.record(start, target)
}

func (b *binBuilder) addVec3(vs [][3]float32, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	return b.record(start, target)
}

func (b *binBuilder) addVec4(vs [][4]float32, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	return b.record(start, target)
}

func (b *binBuilder) addU8Vec4(vs [][4]uint8, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		b.buf.Write(v[:])
	}
	return b.record(start, target)
}

func (b *binBuilder) addUint16(vs []uint16, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	return b.record(start, target)
}

func (b *binBuilder) addMat4(vs [][16]float32, target int) int {
	b.align4()
	start := b.buf.Len()
	for _, v := range vs {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	return b.record(start, target)
}

func (b *binBuilder) bytes() []byte {
	return b.buf.Bytes()
}
