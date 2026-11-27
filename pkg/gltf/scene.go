package gltf

import (
	"encoding/json"
	"fmt"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// Scene represents a glTF 2.0 scene with its root node indices.
type Scene struct {
	Name   string
	Roots  []int
	Extras json.RawMessage
}

// Node represents a node in the glTF 2.0 scene graph.
// Index fields (Parent, Mesh, Skin, Camera) are -1 when unset.
type Node struct {
	Name        string
	Parent      int
	Children    []int
	Translation math3d.Vec3
	Rotation    math3d.Quat
	Scale       math3d.Vec3
	LocalMatrix math3d.Mat4
	WorldMatrix math3d.Mat4
	// HasMatrix reports whether this node's transform is governed directly by
	// LocalMatrix (authored via the glTF "matrix" property) rather than TRS.
	// When false, ComputeWorldMatrices recomputes LocalMatrix from Translation,
	// Rotation, and Scale.
	HasMatrix bool
	// MatrixNotTRS reports that the authored matrix has shear or another part a translation, rotation and scale cannot express. Translation then holds the matrix's translation, Rotation and Scale stay identity, and LocalMatrix is the only exact transform (#546).
	MatrixNotTRS bool
	Mesh         int
	Skin         int
	Camera       int
	Weights      []float32
	Extras       json.RawMessage
	Extensions   map[string]json.RawMessage
}

// Transform returns the node's local TRS transform.
func (n *Node) Transform() math3d.Transform {
	return math3d.Transform{
		Pos:   n.Translation,
		Rot:   n.Rotation,
		Scale: n.Scale,
	}
}

// SetTransform updates the node's local TRS properties, clears HasMatrix, and
// refreshes LocalMatrix.
func (n *Node) SetTransform(t math3d.Transform) {
	n.Translation = t.Pos
	n.Rotation = t.Rot
	n.Scale = t.Scale
	n.HasMatrix = false
	n.MatrixNotTRS = false
	n.LocalMatrix = t.ToMat4()
}

// CameraType identifies the projection model of a glTF camera.
type CameraType string

const (
	CameraPerspective  CameraType = "perspective"
	CameraOrthographic CameraType = "orthographic"
)

// Camera describes a glTF 2.0 perspective or orthographic camera.
type Camera struct {
	Name        string
	Type        CameraType
	AspectRatio float32 // perspective only; 0 means unspecified
	YFOV        float32 // perspective vertical field of view in radians
	XMag        float32 // orthographic horizontal magnification
	YMag        float32 // orthographic vertical magnification
	ZNear       float32
	ZFar        float32 // perspective: 0 means infinite far plane
	Extras      json.RawMessage
}

// ComputeWorldMatrices validates the node hierarchy (rejecting out-of-range
// child indices, multiple parents, and cycles), updates LocalMatrix from TRS
// for non-matrix nodes, and computes exact parent-to-child WorldMatrix values
// for every node in the document.
func (d *Document) ComputeWorldMatrices() error {
	n := len(d.Nodes)
	if n == 0 {
		return nil
	}

	for i := range d.Nodes {
		d.Nodes[i].Parent = -1
		if !d.Nodes[i].HasMatrix {
			d.Nodes[i].LocalMatrix = math3d.TRS(
				d.Nodes[i].Translation,
				d.Nodes[i].Rotation,
				d.Nodes[i].Scale,
			)
		}
	}

	for parentIdx := range d.Nodes {
		for _, childIdx := range d.Nodes[parentIdx].Children {
			if childIdx < 0 || childIdx >= n {
				return fmt.Errorf("gltf: node %d references out-of-range child %d", parentIdx, childIdx)
			}
			if childIdx == parentIdx {
				return fmt.Errorf("gltf: node %d is a child of itself (cycle)", parentIdx)
			}
			if d.Nodes[childIdx].Parent != -1 {
				return fmt.Errorf("gltf: node %d has multiple parents (%d and %d)", childIdx, d.Nodes[childIdx].Parent, parentIdx)
			}
			d.Nodes[childIdx].Parent = parentIdx
		}
	}

	// 0 = unvisited, 1 = visiting, 2 = visited
	state := make([]uint8, n)

	var visit func(idx int, parentWorld math3d.Mat4) error
	visit = func(idx int, parentWorld math3d.Mat4) error {
		switch state[idx] {
		case 1:
			return fmt.Errorf("gltf: cycle detected involving node %d", idx)
		case 2:
			return fmt.Errorf("gltf: node %d visited more than once", idx)
		}
		state[idx] = 1
		world := parentWorld.Mul(d.Nodes[idx].LocalMatrix)
		d.Nodes[idx].WorldMatrix = world
		for _, childIdx := range d.Nodes[idx].Children {
			if err := visit(childIdx, world); err != nil {
				return err
			}
		}
		state[idx] = 2
		return nil
	}

	identity := math3d.Mat4Identity()
	for i := range d.Nodes {
		if d.Nodes[i].Parent == -1 {
			if err := visit(i, identity); err != nil {
				return err
			}
		}
	}

	// Any node not reachable from a root belongs to a disconnected cycle.
	for i, s := range state {
		if s != 2 {
			return fmt.Errorf("gltf: cycle detected in node hierarchy at node %d", i)
		}
	}

	return nil
}
