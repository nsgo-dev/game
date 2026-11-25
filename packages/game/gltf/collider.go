package gltf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// ColliderKind identifies the physics collision shape declared on a glTF node
// through naming conventions or extras.collider metadata.
type ColliderKind string

const (
	// ColliderNone indicates that a node carries no collider annotation.
	ColliderNone ColliderKind = ""
	// ColliderTriMesh builds a static triangle-mesh collider from the node's mesh.
	ColliderTriMesh ColliderKind = "trimesh"
	// ColliderBox builds a box collider from explicit extras dimensions or the
	// node mesh's local bounds.
	ColliderBox ColliderKind = "box"
	// ColliderSphere builds a sphere collider from explicit extras dimensions or
	// the node mesh's local bounds.
	ColliderSphere ColliderKind = "sphere"
	// ColliderCapsule builds a capsule collider from explicit extras dimensions
	// or the node mesh's local bounds.
	ColliderCapsule ColliderKind = "capsule"
	// ColliderConvex marks a convex hull collider (falling back to a triangle
	// mesh collider until native convex hulls are available).
	ColliderConvex ColliderKind = "convex"
)

// NodeCollider describes the collider metadata resolved from a glTF node's name
// suffix and/or extras.collider object.
type NodeCollider struct {
	// NodeIndex is the node's index in Document.Nodes (-1 when parsed from a
	// standalone Node outside a Document).
	NodeIndex int
	// NodeName is the original Node.Name.
	NodeName string
	// BaseName is Node.Name with any recognized collider suffix removed.
	BaseName string
	// MeshIndex is Node.Mesh (-1 if the node has no mesh).
	MeshIndex int
	// Kind is the resolved collider shape kind.
	Kind ColliderKind
	// Draw reports whether the node's mesh should still be rendered visually.
	// It is false for *-colonly suffixes, extras.collider.draw == false,
	// extras.collider.only == true, extras.collider.visible == false, or nodes
	// without a mesh.
	Draw bool
	// IsTrigger marks a sensor/trigger volume rather than a solid obstacle.
	IsTrigger bool
	// Tag is the resolved physics tag (extras.collider.tag when provided,
	// otherwise BaseName, falling back to NodeName).
	Tag string
	// WorldMatrix is the node's world transform matrix (identity when unset).
	WorldMatrix math3d.Mat4

	// Optional local-space shape overrides supplied via extras.collider.
	HasCenter      bool
	Center         math3d.Vec3
	HasHalfExtents bool
	HalfExtents    math3d.Vec3
	HasRadius      bool
	Radius         float32
	HasHeight      bool
	Height         float32

	// Optional physics material and layer filter overrides from extras.collider.
	HasFriction    bool
	Friction       float32
	HasRestitution bool
	Restitution    float32
	HasLayer       bool
	Layer          uint32
	HasMask        bool
	Mask           uint32
}

type colliderSuffixSpec struct {
	suffix string
	kind   ColliderKind
	draw   bool
}

// Ordered longest-first so specialized suffixes (e.g. "-boxcolonly") match
// before shorter ones ("-colonly", "-col").
var colliderSuffixes = []colliderSuffixSpec{
	{"-capsulecolonly", ColliderCapsule, false},
	{"-spherecolonly", ColliderSphere, false},
	{"-convcolonly", ColliderConvex, false},
	{"-boxcolonly", ColliderBox, false},
	{"-capsulecol", ColliderCapsule, true},
	{"-spherecol", ColliderSphere, true},
	{"-convcol", ColliderConvex, true},
	{"-boxcol", ColliderBox, true},
	{"-colonly", ColliderTriMesh, false},
	{"-col", ColliderTriMesh, true},
}

// matchColliderSuffix checks whether name ends with a Godot-style collider
// suffix (case-insensitive), tolerating Blender's trailing ".001" duplicate
// numbering (e.g. "Wall-colonly.001").
func matchColliderSuffix(name string) (baseName string, kind ColliderKind, draw bool, ok bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ColliderNone, true, false
	}

	candidate := trimmed
	if dot := strings.LastIndexByte(trimmed, '.'); dot > 0 && dot+1 < len(trimmed) {
		allDigits := true
		for i := dot + 1; i < len(trimmed); i++ {
			if trimmed[i] < '0' || trimmed[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			candidate = trimmed[:dot]
		}
	}

	lower := strings.ToLower(candidate)
	for _, spec := range colliderSuffixes {
		if strings.HasSuffix(lower, spec.suffix) {
			return candidate[:len(candidate)-len(spec.suffix)], spec.kind, spec.draw, true
		}
	}
	return trimmed, ColliderNone, true, false
}

type rawColliderExtras struct {
	Collider json.RawMessage `json:"collider"`
	Draw     *bool           `json:"draw"`
}

type rawColliderSpec struct {
	Shape        string    `json:"shape"`
	Type         string    `json:"type"`
	Draw         *bool     `json:"draw"`
	Visible      *bool     `json:"visible"`
	Only         *bool     `json:"only"`
	Trigger      *bool     `json:"trigger"`
	IsTrigger    *bool     `json:"isTrigger"`
	Tag          string    `json:"tag"`
	Center       []float32 `json:"center"`
	HalfExtents  []float32 `json:"halfExtents"`
	HalfExtents2 []float32 `json:"half_extents"`
	Size         []float32 `json:"size"`
	Radius       *float32  `json:"radius"`
	Height       *float32  `json:"height"`
	Friction     *float32  `json:"friction"`
	Restitution  *float32  `json:"restitution"`
	Layer        *uint32   `json:"layer"`
	Mask         *uint32   `json:"mask"`
}

func parseColliderKindString(s string) (ColliderKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return ColliderNone, nil
	case "trimesh", "mesh", "concave":
		return ColliderTriMesh, nil
	case "box", "aabb", "obb":
		return ColliderBox, nil
	case "sphere", "ball":
		return ColliderSphere, nil
	case "capsule":
		return ColliderCapsule, nil
	case "convex", "convexhull", "convex_hull":
		return ColliderConvex, nil
	default:
		return ColliderNone, fmt.Errorf("gltf: unknown extras.collider shape %q", s)
	}
}

func parseVec3Slice(vals []float32, field string) (math3d.Vec3, error) {
	if len(vals) != 3 {
		return math3d.Vec3{}, fmt.Errorf("gltf: extras.collider.%s must have 3 elements, got %d", field, len(vals))
	}
	for i, v := range vals {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return math3d.Vec3{}, fmt.Errorf("gltf: extras.collider.%s[%d] must be finite", field, i)
		}
	}
	return math3d.Vec3{X: vals[0], Y: vals[1], Z: vals[2]}, nil
}

func isZeroMat4(m math3d.Mat4) bool {
	return m == (math3d.Mat4{})
}

func nodeEffectiveWorldMatrix(n Node) math3d.Mat4 {
	if !isZeroMat4(n.WorldMatrix) {
		return n.WorldMatrix
	}
	if !isZeroMat4(n.LocalMatrix) {
		return n.LocalMatrix
	}
	if n.Translation != (math3d.Vec3{}) || n.Rotation != (math3d.Quat{}) || n.Scale != (math3d.Vec3{}) {
		scale := n.Scale
		if scale == (math3d.Vec3{}) {
			scale = math3d.Vec3One()
		}
		rot := n.Rotation
		if rot == (math3d.Quat{}) {
			rot = math3d.QuatIdentity()
		}
		return math3d.TRS(n.Translation, rot, scale)
	}
	return math3d.Mat4Identity()
}

// ParseNodeCollider inspects a glTF node's name suffix and extras.collider
// metadata to determine whether it defines a physics collider and whether its
// mesh should be drawn visually.
//
// Recognized Godot-style node name suffixes (case-insensitive, with optional
// trailing Blender ".001" numbering):
//   - -col / -colonly: triangle mesh collider (drawn / collider-only)
//   - -boxcol / -boxcolonly: box collider (drawn / collider-only)
//   - -spherecol / -spherecolonly: sphere collider (drawn / collider-only)
//   - -capsulecol / -capsulecolonly: capsule collider (drawn / collider-only)
//   - -convcol / -convcolonly: convex collider (drawn / collider-only)
//
// Recognized node extras metadata:
//   - {"collider": "box"|"sphere"|"capsule"|"trimesh"|"convex"|"none"}
//   - {"collider": true|false}
//   - {"collider": {"shape"|"type": ..., "draw": bool, "only": bool, "trigger": bool,
//     "tag": string, "center": [x,y,z], "halfExtents"|"size": [x,y,z],
//     "radius": r, "height": h, "friction": f, "restitution": r,
//     "layer": u32, "mask": u32}}
//   - optional top-level {"draw": false} paired with a shorthand collider.
//
// When extras.collider is present it takes precedence over name-suffix defaults.
// It returns (info, true, nil) when the node defines a collider, (zero, false, nil)
// when the node is not a collider, or an error when extras.collider is malformed.
func ParseNodeCollider(node Node) (NodeCollider, bool, error) {
	baseName, kind, draw, hasSuffix := matchColliderSuffix(node.Name)
	info := NodeCollider{
		NodeIndex:   -1,
		NodeName:    node.Name,
		BaseName:    baseName,
		MeshIndex:   node.Mesh,
		Kind:        kind,
		Draw:        draw,
		WorldMatrix: nodeEffectiveWorldMatrix(node),
	}

	hasCollider := hasSuffix

	trimmedExtras := bytes.TrimSpace(node.Extras)
	if len(trimmedExtras) > 0 && trimmedExtras[0] == '{' {
		var top rawColliderExtras
		if err := json.Unmarshal(trimmedExtras, &top); err != nil {
			return NodeCollider{}, false, fmt.Errorf("gltf: parse node %q extras: %w", node.Name, err)
		}
		if top.Draw != nil {
			info.Draw = *top.Draw
		}
		rawCol := bytes.TrimSpace(top.Collider)
		if len(rawCol) > 0 && !bytes.Equal(rawCol, []byte("null")) {
			switch rawCol[0] {
			case 't', 'f':
				var enabled bool
				if err := json.Unmarshal(rawCol, &enabled); err != nil {
					return NodeCollider{}, false, fmt.Errorf("gltf: node %q extras.collider: %w", node.Name, err)
				}
				if !enabled {
					return NodeCollider{}, false, nil
				}
				if info.Kind == ColliderNone {
					info.Kind = ColliderTriMesh
				}
				hasCollider = true
			case '"':
				var shapeStr string
				if err := json.Unmarshal(rawCol, &shapeStr); err != nil {
					return NodeCollider{}, false, fmt.Errorf("gltf: node %q extras.collider: %w", node.Name, err)
				}
				parsedKind, err := parseColliderKindString(shapeStr)
				if err != nil {
					return NodeCollider{}, false, fmt.Errorf("gltf: node %q: %w", node.Name, err)
				}
				if parsedKind == ColliderNone {
					return NodeCollider{}, false, nil
				}
				info.Kind = parsedKind
				hasCollider = true
			case '{':
				var spec rawColliderSpec
				if err := json.Unmarshal(rawCol, &spec); err != nil {
					return NodeCollider{}, false, fmt.Errorf("gltf: node %q extras.collider: %w", node.Name, err)
				}
				shapeName := spec.Shape
				if shapeName == "" {
					shapeName = spec.Type
				}
				if shapeName != "" {
					parsedKind, err := parseColliderKindString(shapeName)
					if err != nil {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: %w", node.Name, err)
					}
					if parsedKind == ColliderNone {
						return NodeCollider{}, false, nil
					}
					info.Kind = parsedKind
				} else if info.Kind == ColliderNone {
					info.Kind = ColliderTriMesh
				}
				hasCollider = true

				if spec.Only != nil {
					info.Draw = !*spec.Only
				}
				if spec.Visible != nil {
					info.Draw = *spec.Visible
				}
				if spec.Draw != nil {
					info.Draw = *spec.Draw
				}
				if spec.Trigger != nil {
					info.IsTrigger = *spec.Trigger
				}
				if spec.IsTrigger != nil {
					info.IsTrigger = *spec.IsTrigger
				}
				if spec.Tag != "" {
					info.Tag = spec.Tag
				}
				if len(spec.Center) > 0 {
					v, err := parseVec3Slice(spec.Center, "center")
					if err != nil {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: %w", node.Name, err)
					}
					info.Center = v
					info.HasCenter = true
				}
				halfExt := spec.HalfExtents
				if len(halfExt) == 0 {
					halfExt = spec.HalfExtents2
				}
				if len(halfExt) > 0 {
					v, err := parseVec3Slice(halfExt, "halfExtents")
					if err != nil {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: %w", node.Name, err)
					}
					if v.X < 0 || v.Y < 0 || v.Z < 0 {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: extras.collider.halfExtents must be non-negative", node.Name)
					}
					info.HalfExtents = v
					info.HasHalfExtents = true
				} else if len(spec.Size) > 0 {
					v, err := parseVec3Slice(spec.Size, "size")
					if err != nil {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: %w", node.Name, err)
					}
					if v.X < 0 || v.Y < 0 || v.Z < 0 {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: extras.collider.size must be non-negative", node.Name)
					}
					info.HalfExtents = v.Scale(0.5)
					info.HasHalfExtents = true
				}
				if spec.Radius != nil {
					r := *spec.Radius
					if math.IsNaN(float64(r)) || math.IsInf(float64(r), 0) || r <= 0 {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: extras.collider.radius must be finite and > 0", node.Name)
					}
					info.Radius = r
					info.HasRadius = true
				}
				if spec.Height != nil {
					h := *spec.Height
					if math.IsNaN(float64(h)) || math.IsInf(float64(h), 0) || h <= 0 {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: extras.collider.height must be finite and > 0", node.Name)
					}
					info.Height = h
					info.HasHeight = true
				}
				if spec.Friction != nil {
					f := *spec.Friction
					if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) || f < 0 {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: extras.collider.friction must be finite and >= 0", node.Name)
					}
					info.Friction = f
					info.HasFriction = true
				}
				if spec.Restitution != nil {
					r := *spec.Restitution
					if math.IsNaN(float64(r)) || math.IsInf(float64(r), 0) || r < 0 || r > 1 {
						return NodeCollider{}, false, fmt.Errorf("gltf: node %q: extras.collider.restitution must be in [0, 1]", node.Name)
					}
					info.Restitution = r
					info.HasRestitution = true
				}
				if spec.Layer != nil {
					info.Layer = *spec.Layer
					info.HasLayer = true
				}
				if spec.Mask != nil {
					info.Mask = *spec.Mask
					info.HasMask = true
				}
			default:
				return NodeCollider{}, false, fmt.Errorf("gltf: node %q extras.collider must be a string, boolean, or object", node.Name)
			}
		}
	}

	if !hasCollider || info.Kind == ColliderNone {
		return NodeCollider{}, false, nil
	}

	if info.MeshIndex < 0 {
		info.Draw = false
	}
	if info.Tag == "" {
		if info.BaseName != "" {
			info.Tag = info.BaseName
		} else {
			info.Tag = info.NodeName
		}
	}
	return info, true, nil
}

// IsDrawable reports whether n should be rendered visually.
// It returns false when n has no mesh (Mesh < 0) or when n is marked as a
// collider-only / non-drawn node via a *-colonly name suffix or extras
// ("draw": false, "collider": {"draw": false | "only": true | "visible": false}).
func (n Node) IsDrawable() bool {
	if n.Mesh < 0 {
		return false
	}
	if info, ok, err := ParseNodeCollider(n); err == nil && ok {
		return info.Draw
	}
	// Check standalone extras.draw == false even for non-collider nodes.
	trimmedExtras := bytes.TrimSpace(n.Extras)
	if len(trimmedExtras) > 0 && trimmedExtras[0] == '{' {
		var top rawColliderExtras
		if err := json.Unmarshal(trimmedExtras, &top); err == nil && top.Draw != nil {
			return *top.Draw
		}
	}
	return true
}

func (d *Document) sceneNodeIndices(sceneIdx int) []int {
	if d == nil || len(d.Nodes) == 0 {
		return nil
	}
	if sceneIdx >= 0 && sceneIdx < len(d.Scenes) {
		visited := make([]bool, len(d.Nodes))
		out := make([]int, 0, len(d.Nodes))
		var walk func(idx int)
		walk = func(idx int) {
			if idx < 0 || idx >= len(d.Nodes) || visited[idx] {
				return
			}
			visited[idx] = true
			out = append(out, idx)
			for _, child := range d.Nodes[idx].Children {
				walk(child)
			}
		}
		for _, root := range d.Scenes[sceneIdx].Roots {
			walk(root)
		}
		return out
	}
	out := make([]int, len(d.Nodes))
	for i := range d.Nodes {
		out[i] = i
	}
	return out
}

func (d *Document) defaultSceneIndex() int {
	if d == nil || len(d.Scenes) == 0 {
		return -1
	}
	if d.Scene >= 0 && d.Scene < len(d.Scenes) {
		return d.Scene
	}
	return 0
}

// DrawableNodes returns the deterministic slice of node indices in the
// document's default scene (or all nodes when no scenes are defined) that
// reference a valid mesh and should be drawn visually, excluding collider-only
// nodes such as *-colonly and extras.collider.draw == false.
func (d *Document) DrawableNodes() []int {
	return d.SceneDrawableNodes(d.defaultSceneIndex())
}

// SceneDrawableNodes returns the deterministic slice of node indices in
// d.Scenes[sceneIdx] (or all nodes when sceneIdx < 0) that reference a valid
// mesh and should be drawn visually, excluding collider-only nodes.
func (d *Document) SceneDrawableNodes(sceneIdx int) []int {
	if d == nil {
		return nil
	}
	indices := d.sceneNodeIndices(sceneIdx)
	var out []int
	for _, ni := range indices {
		n := d.Nodes[ni]
		if n.Mesh < 0 || n.Mesh >= len(d.Meshes) {
			continue
		}
		if !n.IsDrawable() {
			continue
		}
		out = append(out, ni)
	}
	return out
}

// ColliderNodes returns all collider annotations found on nodes in the
// document's default scene (or all nodes when no scenes are defined), in
// deterministic scene traversal order.
func (d *Document) ColliderNodes() ([]NodeCollider, error) {
	return d.SceneColliderNodes(d.defaultSceneIndex())
}

// SceneColliderNodes returns all collider annotations found on nodes in
// d.Scenes[sceneIdx] (or all nodes when sceneIdx < 0), in deterministic scene
// traversal order.
func (d *Document) SceneColliderNodes(sceneIdx int) ([]NodeCollider, error) {
	if d == nil {
		return nil, nil
	}
	indices := d.sceneNodeIndices(sceneIdx)
	var out []NodeCollider
	for _, ni := range indices {
		info, ok, err := ParseNodeCollider(d.Nodes[ni])
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		info.NodeIndex = ni
		out = append(out, info)
	}
	return out, nil
}
