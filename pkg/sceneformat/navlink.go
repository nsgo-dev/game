package sceneformat

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

// NavLinkComponentName is the canonical scene YAML component key for authored off-mesh navigation links
// (jumps, ledge drops, ladders, doors, and teleporters).
const NavLinkComponentName = "nav_link"

// NavLinkComponent returns the ComponentType schema for authoring off-mesh navigation links
// in scene YAML files (for both 2D pathfind.NavMesh and 3D world3d.NavMesh / navbake3d).
//
// The link's Start and End are expressed in the object's local space (start defaults to the object
// origin (0, 0, 0), end to (endX, endY, endZ)) and transformed by the object's composed world
// transform when extracted, so moving, rotating, or instancing a scene or prefab carries both
// endpoints together.
//
// Register it on a ComponentRegistry via RegisterNavLink so ParseWith validates nav_link fields
// and rejects unknown keys, wrong types, or out-of-range values.
func NavLinkComponent() ComponentType {
	return ComponentType{
		Name: NavLinkComponentName,
		Doc:  "Point-to-point off-mesh navigation link (jump, drop, ladder, or teleport) in the object's local space.",
		Fields: []Field{
			{
				Name:    "startX",
				Kind:    FieldFloat,
				Default: 0.0,
				Step:    0.25,
				Doc:     "Local X offset of the link start point from the object origin.",
			},
			{
				Name:    "startY",
				Kind:    FieldFloat,
				Default: 0.0,
				Step:    0.25,
				Doc:     "Local Y offset of the link start point from the object origin.",
			},
			{
				Name:    "startZ",
				Kind:    FieldFloat,
				Default: 0.0,
				Step:    0.25,
				Doc:     "Local Z offset of the link start point from the object origin (3D).",
			},
			{
				Name:    "endX",
				Kind:    FieldFloat,
				Default: 0.0,
				Step:    0.25,
				Doc:     "Local X offset of the link end point from the object origin.",
			},
			{
				Name:    "endY",
				Kind:    FieldFloat,
				Default: 0.0,
				Step:    0.25,
				Doc:     "Local Y offset of the link end point from the object origin.",
			},
			{
				Name:    "endZ",
				Kind:    FieldFloat,
				Default: 0.0,
				Step:    0.25,
				Doc:     "Local Z offset of the link end point from the object origin (3D).",
			},
			{
				Name:    "radius",
				Kind:    FieldFloat,
				Default: 0.0,
				Min:     Float64(0),
				Step:    0.25,
				Doc:     "Endpoint snap and agent entry tolerance radius in world units (>= 0).",
			},
			{
				Name:    "bidirectional",
				Kind:    FieldBool,
				Default: false,
				Doc:     "When true, allows traversal in both directions (two-way); when false, one-way start to end.",
			},
			{
				Name:    "cost",
				Kind:    FieldFloat,
				Default: 0.0,
				Min:     Float64(0),
				Step:    0.5,
				Doc:     "Traversal cost override; 0 uses Euclidean distance between Start and End.",
			},
			{
				Name:    "area",
				Kind:    FieldInt,
				Default: 0,
				Min:     Float64(0),
				Max:     Float64(255),
				Step:    1,
				Doc:     "Navigation area ID (pathfind.NavArea, 0..255).",
			},
			{
				Name:    "flags",
				Kind:    FieldInt,
				Default: 0,
				Min:     Float64(0),
				Max:     Float64(math.MaxUint32),
				Step:    1,
				Doc:     "Optional custom link flag bits (NavLinkOffMesh is always added automatically).",
			},
			{
				Name:    "linkID",
				Kind:    FieldInt,
				Default: 0,
				Step:    1,
				Doc:     "Optional game-defined integer link identifier.",
			},
			{
				Name:    "userData",
				Kind:    FieldString,
				Default: "",
				Doc:     `Optional game-defined metadata tag (such as "jump", "drop", "ladder", or "teleport").`,
			},
		},
	}
}

// RegisterNavLink registers the built-in "nav_link" component type on reg.
func RegisterNavLink(reg *ComponentRegistry) error {
	return reg.Register(NavLinkComponent())
}

var builtinNavLinkReg = func() *ComponentRegistry {
	reg := NewComponentRegistry()
	reg.MustRegister(NavLinkComponent())
	return reg
}()

type decodedNavLink struct {
	startX, startY, startZ float32
	endX, endY, endZ       float32
	radius                 float32
	bidirectional          bool
	cost                   float32
	area                   pathfind.NavArea
	flags                  uint32
	id                     int
	userData               any
}

func decodeNavLink(objectID string, raw any) (decodedNavLink, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return decodedNavLink{}, &ComponentError{
			Object:    objectID,
			Component: NavLinkComponentName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := builtinNavLinkReg.Resolve(NavLinkComponentName, m)
	if err != nil {
		if ce, ok := err.(*ComponentError); ok {
			ce.Object = objectID
		}
		return decodedNavLink{}, err
	}
	var ud any
	if s, _ := resolved["userData"].(string); s != "" {
		ud = s
	}
	return decodedNavLink{
		startX:        float32(resolved["startX"].(float64)),
		startY:        float32(resolved["startY"].(float64)),
		startZ:        float32(resolved["startZ"].(float64)),
		endX:          float32(resolved["endX"].(float64)),
		endY:          float32(resolved["endY"].(float64)),
		endZ:          float32(resolved["endZ"].(float64)),
		radius:        float32(resolved["radius"].(float64)),
		bidirectional: resolved["bidirectional"].(bool),
		cost:          float32(resolved["cost"].(float64)),
		area:          pathfind.NavArea(resolved["area"].(int)),
		flags:         uint32(resolved["flags"].(int)),
		id:            resolved["linkID"].(int),
		userData:      ud,
	}, nil
}

// ExtractNavLinks2D extracts all authored "nav_link" components from doc in deterministic
// document order (including nested child objects, composing parent 2D transforms) as 2D
// pathfind.NavOffMeshLink values ready for pathfind.NavMesh.AddOffMeshLink.
func ExtractNavLinks2D(doc *SceneDoc) ([]pathfind.NavOffMeshLink, error) {
	if doc == nil {
		return nil, nil
	}
	var out []pathfind.NavOffMeshLink

	var visit func(objs []ObjectDoc, parentAffine math2d.Affine) error
	visit = func(objs []ObjectDoc, parentAffine math2d.Affine) error {
		for i := range objs {
			o := &objs[i]
			world := parentAffine.Mul(objectLocalAffine(o))
			if raw, ok := o.Components[NavLinkComponentName]; ok {
				d, err := decodeNavLink(o.ID, raw)
				if err != nil {
					return err
				}
				start := world.TransformPoint(math2d.V2(d.startX, d.startY))
				end := world.TransformPoint(math2d.V2(d.endX, d.endY))
				out = append(out, pathfind.NavOffMeshLink{
					Start:         core.Point{X: core.Points(start.X), Y: core.Points(start.Y)},
					End:           core.Point{X: core.Points(end.X), Y: core.Points(end.Y)},
					Radius:        d.radius,
					Bidirectional: d.bidirectional,
					Flags:         d.flags,
					ID:            d.id,
					Cost:          d.cost,
					Area:          d.area,
					UserData:      d.userData,
				})
			}
			if len(o.Children) > 0 {
				if err := visit(o.Children, world); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for li := range doc.Layers {
		if err := visit(doc.Layers[li].Objects, math2d.Identity()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ExtractBuiltNavLinks2D extracts all authored "nav_link" components from a resolved BuiltScene as 2D links.
func ExtractBuiltNavLinks2D(scene *BuiltScene) ([]pathfind.NavOffMeshLink, error) {
	if scene == nil || scene.Doc == nil {
		return nil, nil
	}
	return ExtractNavLinks2D(scene.Doc)
}

// ExtractNavLinks3D extracts all authored "nav_link" components from doc in deterministic
// document order (including nested child objects, composing full 3D object transforms) as 3D
// world3d.NavOffMeshLink values ready for world3d.NavMesh.AddOffMeshLink or navbake3d.
func ExtractNavLinks3D(doc *SceneDoc) ([]world3d.NavOffMeshLink, error) {
	if doc == nil {
		return nil, nil
	}
	var out []world3d.NavOffMeshLink

	var visit func(objs []ObjectDoc, parentWorld math3d.Mat4) error
	visit = func(objs []ObjectDoc, parentWorld math3d.Mat4) error {
		for i := range objs {
			o := &objs[i]
			tr := ObjectLocalTransform3D(o)
			world := parentWorld.Mul(math3d.TRS(tr.Pos, tr.Rot, tr.Scale))
			if raw, ok := o.Components[NavLinkComponentName]; ok {
				d, err := decodeNavLink(o.ID, raw)
				if err != nil {
					return err
				}
				start := world.TransformPoint(math3d.Vec3{X: d.startX, Y: d.startY, Z: d.startZ})
				end := world.TransformPoint(math3d.Vec3{X: d.endX, Y: d.endY, Z: d.endZ})
				out = append(out, world3d.NavOffMeshLink{
					Start:         start,
					End:           end,
					Radius:        d.radius,
					Bidirectional: d.bidirectional,
					Flags:         d.flags,
					ID:            d.id,
					Cost:          d.cost,
					Area:          d.area,
					UserData:      d.userData,
				})
			}
			if len(o.Children) > 0 {
				if err := visit(o.Children, world); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for li := range doc.Layers {
		if err := visit(doc.Layers[li].Objects, math3d.Mat4Identity()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ExtractBuiltNavLinks3D extracts all authored "nav_link" components from a resolved BuiltScene as 3D links.
func ExtractBuiltNavLinks3D(scene *BuiltScene) ([]world3d.NavOffMeshLink, error) {
	if scene == nil || scene.Doc == nil {
		return nil, nil
	}
	return ExtractNavLinks3D(scene.Doc)
}
