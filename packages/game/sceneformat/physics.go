package sceneformat

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"go.yaml.in/yaml/v3"
)

// PhysicsComponentVersion is the schema version of the built-in 2D and 3D physics components.
const PhysicsComponentVersion = 1

// Built-in physics component type names in scene YAML files.
const (
	// Body2DComponentName is the built-in 2D physics body component (static, kinematic or dynamic).
	Body2DComponentName = "body2d"
	// Collider2DComponentName is the built-in 2D collision shape component (box or circle).
	Collider2DComponentName = "collider2d"
	// Trigger2DComponentName is the built-in 2D non-solid trigger sensor component (box or circle).
	Trigger2DComponentName = "trigger2d"
	// Body3DComponentName is the built-in 3D physics body component (static, kinematic, dynamic, or trigger).
	Body3DComponentName = "body3d"
	// Collider3DComponentName is the built-in 3D collision shape component (box, sphere, capsule, cylinder, convex, trimesh, or heightfield).
	Collider3DComponentName = "collider3d"
	// Trigger3DComponentName is the built-in 3D non-solid trigger sensor component (box, sphere, capsule, cylinder, or convex).
	Trigger3DComponentName = "trigger3d"
)

// PhysicsMaterialResolver may optionally be implemented by an AssetResolver (such as *resource.Library)
// to load shared .physics.yaml materials named by body2d.material and body3d.material / body3d.physicsMaterial.
type PhysicsMaterialResolver interface {
	PhysicsMaterial(path string) (*resource.PhysicsMaterial, error)
}

var physicsBodyKinds = []string{"static", "kinematic", "dynamic"}
var physicsBody3DKinds = []string{"static", "kinematic", "dynamic", "trigger"}
var physicsShapes2D = []string{"box", "circle"}
var physicsShapes3D = []string{"box", "sphere", "capsule", "cylinder", "convex", "trimesh", "heightfield"}
var physicsTriggerShapes3D = []string{"box", "sphere", "capsule", "cylinder", "convex"}

// PhysicsComponentNames returns the names of all built-in physics component types in canonical order.
func PhysicsComponentNames() []string {
	return []string{
		Body2DComponentName,
		Collider2DComponentName,
		Trigger2DComponentName,
		Body3DComponentName,
		Collider3DComponentName,
		Trigger3DComponentName,
	}
}

// IsPhysicsComponent reports whether name is one of the built-in 2D or 3D physics component types.
func IsPhysicsComponent(name string) bool {
	switch name {
	case Body2DComponentName, Collider2DComponentName, Trigger2DComponentName,
		Body3DComponentName, Collider3DComponentName, Trigger3DComponentName:
		return true
	default:
		return false
	}
}

func isLayeredPhysicsComponent(name string) bool {
	switch name {
	case Collider2DComponentName, Trigger2DComponentName,
		Body3DComponentName, Collider3DComponentName, Trigger3DComponentName:
		return true
	default:
		return false
	}
}

// Body2DComponent returns the ComponentType schema for the built-in "body2d" component.
func Body2DComponent() ComponentType {
	return ComponentType{
		Name: Body2DComponentName,
		Doc:  "2D physics body: static, kinematic, or dynamic rigid body settings.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: PhysicsComponentVersion, Min: Float64(PhysicsComponentVersion), Max: Float64(PhysicsComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "kind", Kind: FieldString, Default: "dynamic", Enum: physicsBodyKinds, Doc: `Body motion type: "static", "kinematic", or "dynamic".`},
			{Name: "mass", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.5, Doc: "Body mass in kilograms for dynamic bodies (0 behaves as static)."},
			{Name: "gravityScale", Kind: FieldFloat, Default: 1.0, Step: 0.1, Doc: "Multiplier applied to 2D world gravity."},
			{Name: "vx", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Initial horizontal velocity in points per second."},
			{Name: "vy", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Initial vertical velocity in points per second."},
			{Name: "friction", Kind: FieldFloat, Default: 0.4, Min: Float64(0), Step: 0.05, Doc: "Coulomb friction coefficient (>= 0)."},
			{Name: "restitution", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Max: Float64(1), Step: 0.05, Doc: "Bounciness in [0, 1]."},
			{Name: "linearDamping", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 0.05, Doc: "Linear velocity damping per second (>= 0)."},
			{Name: "angularDamping", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 0.05, Doc: "Angular velocity damping per second (>= 0)."},
			{Name: "material", Kind: FieldString, Default: "", Doc: "Optional project-relative path to a shared .physics.yaml PhysicsMaterial resource."},
		},
	}
}

// Collider2DComponent returns the ComponentType schema for the built-in "collider2d" component.
func Collider2DComponent() ComponentType {
	return ComponentType{
		Name: Collider2DComponentName,
		Doc:  "2D collision shape (box or circle) with named collision layer and mask.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: PhysicsComponentVersion, Min: Float64(PhysicsComponentVersion), Max: Float64(PhysicsComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "shape", Kind: FieldString, Default: "box", Enum: physicsShapes2D, Doc: `Collider shape: "box" or "circle".`},
			{Name: "width", Kind: FieldFloat, Default: 16.0, Min: Float64(0), Step: 1, Doc: "Box width in points."},
			{Name: "height", Kind: FieldFloat, Default: 16.0, Min: Float64(0), Step: 1, Doc: "Box height in points."},
			{Name: "radius", Kind: FieldFloat, Default: 8.0, Min: Float64(0), Step: 1, Doc: "Circle radius in points."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local horizontal center offset from the object position."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local vertical center offset from the object position."},
			{Name: "layer", Kind: FieldString, Default: "", Doc: "Named collision layer declared in game.yaml physics.layers (empty matches all)."},
			{Name: "mask", Kind: FieldStringList, Doc: "Named collision layers this collider interacts with (empty matches all)."},
			{Name: "isTrigger", Kind: FieldBool, Default: false, Doc: "When true, acts as a non-solid trigger sensor instead of blocking movement."},
		},
		Events: []Event{
			{Name: "enter", Doc: "Emitted when a matching body starts overlapping this trigger."},
			{Name: "stay", Doc: "Emitted each step while a matching body stays overlapping this trigger."},
			{Name: "exit", Doc: "Emitted when a matching body stops overlapping this trigger."},
		},
	}
}

// Trigger2DComponent returns the ComponentType schema for the built-in "trigger2d" component.
func Trigger2DComponent() ComponentType {
	return ComponentType{
		Name: Trigger2DComponentName,
		Doc:  "2D non-solid trigger sensor (box or circle) that fires enter, stay, and exit events.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: PhysicsComponentVersion, Min: Float64(PhysicsComponentVersion), Max: Float64(PhysicsComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "shape", Kind: FieldString, Default: "box", Enum: physicsShapes2D, Doc: `Trigger shape: "box" or "circle".`},
			{Name: "width", Kind: FieldFloat, Default: 16.0, Min: Float64(0), Step: 1, Doc: "Box width in points."},
			{Name: "height", Kind: FieldFloat, Default: 16.0, Min: Float64(0), Step: 1, Doc: "Box height in points."},
			{Name: "radius", Kind: FieldFloat, Default: 8.0, Min: Float64(0), Step: 1, Doc: "Circle radius in points."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local horizontal center offset from the object position."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local vertical center offset from the object position."},
			{Name: "layer", Kind: FieldString, Default: "", Doc: "Named collision layer declared in game.yaml physics.layers (empty matches all)."},
			{Name: "mask", Kind: FieldStringList, Doc: "Named collision layers this trigger detects (empty matches all)."},
		},
		Events: []Event{
			{Name: "enter", Doc: "Emitted when a matching body enters this trigger."},
			{Name: "stay", Doc: "Emitted each step while a matching body stays inside this trigger."},
			{Name: "exit", Doc: "Emitted when a matching body exits this trigger."},
		},
	}
}

// Body3DComponent returns the ComponentType schema for the built-in "body3d" component.
func Body3DComponent() ComponentType {
	return ComponentType{
		Name: Body3DComponentName,
		Doc:  "3D physics body: static, kinematic, dynamic, or trigger body settings with optional layer, mask, and physics material.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: PhysicsComponentVersion, Min: Float64(PhysicsComponentVersion), Max: Float64(PhysicsComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "kind", Kind: FieldString, Default: "dynamic", Enum: physicsBody3DKinds, Doc: `Body motion type: "static", "kinematic", "dynamic", or "trigger".`},
			{Name: "mass", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.5, Doc: "Body mass in kilograms for dynamic bodies (0 behaves as static)."},
			{Name: "gravityScale", Kind: FieldFloat, Default: 1.0, Step: 0.1, Doc: "Multiplier applied to 3D world gravity."},
			{Name: "vx", Kind: FieldFloat, Default: 0.0, Step: 0.5, Doc: "Initial linear velocity along X in units per second."},
			{Name: "vy", Kind: FieldFloat, Default: 0.0, Step: 0.5, Doc: "Initial linear velocity along Y in units per second."},
			{Name: "vz", Kind: FieldFloat, Default: 0.0, Step: 0.5, Doc: "Initial linear velocity along Z in units per second."},
			{Name: "friction", Kind: FieldFloat, Default: 0.5, Min: Float64(0), Step: 0.05, Doc: "Coulomb friction coefficient (>= 0)."},
			{Name: "restitution", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Max: Float64(1), Step: 0.05, Doc: "Bounciness in [0, 1]."},
			{Name: "linearDamping", Kind: FieldFloat, Default: 0.01, Min: Float64(0), Step: 0.01, Doc: "Linear velocity damping per second (>= 0)."},
			{Name: "angularDamping", Kind: FieldFloat, Default: 0.05, Min: Float64(0), Step: 0.01, Doc: "Angular velocity damping per second (>= 0)."},
			{Name: "lockRotation", Kind: FieldBool, Default: false, Doc: "Prevents rotation impulses on dynamic bodies."},
			{Name: "layer", Kind: FieldString, Default: "", Doc: "Named collision layer declared in game.yaml physics.layers (empty falls back to collider3d.layer or all)."},
			{Name: "mask", Kind: FieldStringList, Doc: "Named collision layers this body interacts with (empty falls back to collider3d.mask or all)."},
			{Name: "material", Kind: FieldString, Default: "", Doc: "Optional project-relative path to a shared .physics.yaml PhysicsMaterial resource."},
			{Name: "physicsMaterial", Kind: FieldString, Default: "", Doc: "Optional project-relative path to a shared .physics.yaml PhysicsMaterial resource."},
		},
		Events: []Event{
			{Name: "enter", Doc: "Emitted when kind is trigger and a matching body starts overlapping this body."},
			{Name: "stay", Doc: "Emitted each step while kind is trigger and a matching body stays overlapping this body."},
			{Name: "exit", Doc: "Emitted when kind is trigger and a matching body stops overlapping this body."},
		},
	}
}

// Collider3DComponent returns the ComponentType schema for the built-in "collider3d" component.
func Collider3DComponent() ComponentType {
	return ComponentType{
		Name: Collider3DComponentName,
		Doc:  "3D collision shape (box, sphere, capsule, cylinder, convex, trimesh, or heightfield) with local offset, rotation, layer, and mask.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: PhysicsComponentVersion, Min: Float64(PhysicsComponentVersion), Max: Float64(PhysicsComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "shape", Kind: FieldString, Default: "box", Enum: physicsShapes3D, Doc: `Collider shape: "box", "sphere", "capsule", "cylinder", "convex", "trimesh", or "heightfield".`},
			{Name: "width", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.25, Doc: "Box extent along X."},
			{Name: "height", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.25, Doc: "Box extent along Y, or total capsule or cylinder height."},
			{Name: "depth", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.25, Doc: "Box extent along Z."},
			{Name: "radius", Kind: FieldFloat, Default: 0.5, Min: Float64(0), Step: 0.1, Doc: "Sphere, capsule, or cylinder radius."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 0.25, Doc: "Local X center offset."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 0.25, Doc: "Local Y center offset."},
			{Name: "offsetZ", Kind: FieldFloat, Default: 0.0, Step: 0.25, Doc: "Local Z center offset."},
			{Name: "rotX", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local X rotation in radians."},
			{Name: "rotY", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local Y rotation in radians."},
			{Name: "rotZ", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local Z rotation in radians."},
			{Name: "terrain", Kind: FieldString, Default: "", Doc: "Project-relative path to a *.terrain.yaml file for heightfield colliders."},
			{Name: "maxVertices", Kind: FieldInt, Default: 0, Min: Float64(0), Max: Float64(physics3d.MaxHullVertices), Step: 8, Doc: "Maximum vertex count for convex hull colliders (0 uses physics3d.DefaultHullVertices)."},
			{Name: "layer", Kind: FieldString, Default: "", Doc: "Named collision layer declared in game.yaml physics.layers (empty matches all)."},
			{Name: "mask", Kind: FieldStringList, Doc: "Named collision layers this collider interacts with (empty matches all)."},
			{Name: "isTrigger", Kind: FieldBool, Default: false, Doc: "When true, acts as a non-solid trigger sensor instead of blocking movement."},
		},
		Events: []Event{
			{Name: "enter", Doc: "Emitted when a matching body starts overlapping this trigger."},
			{Name: "stay", Doc: "Emitted each step while a matching body stays overlapping this trigger."},
			{Name: "exit", Doc: "Emitted when a matching body stops overlapping this trigger."},
		},
	}
}

// Trigger3DComponent returns the ComponentType schema for the built-in "trigger3d" component.
func Trigger3DComponent() ComponentType {
	return ComponentType{
		Name: Trigger3DComponentName,
		Doc:  "3D non-solid trigger sensor (box, sphere, capsule, cylinder, or convex) that fires enter, stay, and exit events.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: PhysicsComponentVersion, Min: Float64(PhysicsComponentVersion), Max: Float64(PhysicsComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "shape", Kind: FieldString, Default: "box", Enum: physicsTriggerShapes3D, Doc: `Trigger shape: "box", "sphere", "capsule", "cylinder", or "convex".`},
			{Name: "width", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.25, Doc: "Box extent along X."},
			{Name: "height", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.25, Doc: "Box extent along Y, or total capsule or cylinder height."},
			{Name: "depth", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.25, Doc: "Box extent along Z."},
			{Name: "radius", Kind: FieldFloat, Default: 0.5, Min: Float64(0), Step: 0.1, Doc: "Sphere, capsule, or cylinder radius."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 0.25, Doc: "Local X center offset."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 0.25, Doc: "Local Y center offset."},
			{Name: "offsetZ", Kind: FieldFloat, Default: 0.0, Step: 0.25, Doc: "Local Z center offset."},
			{Name: "rotX", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local X rotation in radians."},
			{Name: "rotY", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local Y rotation in radians."},
			{Name: "rotZ", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local Z rotation in radians."},
			{Name: "maxVertices", Kind: FieldInt, Default: 0, Min: Float64(0), Max: Float64(physics3d.MaxHullVertices), Step: 8, Doc: "Maximum vertex count for convex hull triggers (0 uses physics3d.DefaultHullVertices)."},
			{Name: "layer", Kind: FieldString, Default: "", Doc: "Named collision layer declared in game.yaml physics.layers (empty matches all)."},
			{Name: "mask", Kind: FieldStringList, Doc: "Named collision layers this trigger detects (empty matches all)."},
		},
		Events: []Event{
			{Name: "enter", Doc: "Emitted when a matching body enters this trigger."},
			{Name: "stay", Doc: "Emitted each step while a matching body stays inside this trigger."},
			{Name: "exit", Doc: "Emitted when a matching body exits this trigger."},
		},
	}
}

// RegisterPhysicsComponents registers all six built-in 2D and 3D physics component types on reg.
func RegisterPhysicsComponents(reg *ComponentRegistry) error {
	for _, ct := range []ComponentType{
		Body2DComponent(),
		Collider2DComponent(),
		Trigger2DComponent(),
		Body3DComponent(),
		Collider3DComponent(),
		Trigger3DComponent(),
	} {
		if err := reg.Register(ct); err != nil {
			return err
		}
	}
	return nil
}

// PhysicsComponentRegistry returns a new ComponentRegistry pre-populated with all built-in 2D and 3D physics components.
func PhysicsComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterPhysicsComponents(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinPhysicsReg = PhysicsComponentRegistry()

// PhysicsLayerDef maps one human-readable collision layer name to a bit index in [0, 31].
type PhysicsLayerDef struct {
	Name string
	Bit  int
}

// PhysicsLayerTable resolves human-readable collision layer names (such as "player", "walls", "coins")
// to 32-bit layer bitmasks used by physics and physics3d.
type PhysicsLayerTable struct {
	bits  map[string]uint
	names []string
}

// NewPhysicsLayerTable builds a validated collision layer table from defs.
// It refuses empty names, duplicate names, bits outside [0, 31], and duplicate bit indices.
func NewPhysicsLayerTable(defs []PhysicsLayerDef) (*PhysicsLayerTable, error) {
	bits := make(map[string]uint, len(defs))
	usedBits := make(map[int]string, len(defs))
	names := make([]string, 0, len(defs))
	for i, d := range defs {
		if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Name) != d.Name {
			return nil, fmt.Errorf("sceneformat: physics layer %d has invalid name %q", i, d.Name)
		}
		if _, dup := bits[d.Name]; dup {
			return nil, fmt.Errorf("sceneformat: duplicate physics layer %q", d.Name)
		}
		if d.Bit < 0 || d.Bit > 31 {
			return nil, fmt.Errorf("sceneformat: physics layer %q bit %d must be in [0, 31]", d.Name, d.Bit)
		}
		if prev, dup := usedBits[d.Bit]; dup {
			return nil, fmt.Errorf("sceneformat: physics layer %q reuses bit %d (already assigned to %q)", d.Name, d.Bit, prev)
		}
		bits[d.Name] = uint(d.Bit)
		usedBits[d.Bit] = d.Name
		names = append(names, d.Name)
	}
	slices.Sort(names)
	return &PhysicsLayerTable{bits: bits, names: names}, nil
}

// MustPhysicsLayerTable is NewPhysicsLayerTable for tests and setup code; it panics on error.
func MustPhysicsLayerTable(defs []PhysicsLayerDef) *PhysicsLayerTable {
	t, err := NewPhysicsLayerTable(defs)
	if err != nil {
		panic(err)
	}
	return t
}

// Bit returns the bit index in [0, 31] assigned to name and whether name is declared.
func (t *PhysicsLayerTable) Bit(name string) (uint, bool) {
	if t == nil {
		return 0, false
	}
	b, ok := t.bits[name]
	return b, ok
}

// Names returns all declared layer names in ascending order.
func (t *PhysicsLayerTable) Names() []string {
	if t == nil {
		return nil
	}
	return slices.Clone(t.names)
}

func (t *PhysicsLayerTable) expectedList() string {
	if t == nil || len(t.names) == 0 {
		return "a layer declared under physics.layers in game.yaml"
	}
	return "one of " + strings.Join(t.names, ", ")
}

// Mask2D resolves names into a 2D physics.LayerMask. Empty names returns 0 (which defaults to LayerAll).
func (t *PhysicsLayerTable) Mask2D(names ...string) (physics.LayerMask, error) {
	var mask physics.LayerMask
	for _, n := range names {
		if n == "" {
			continue
		}
		bit, ok := t.Bit(n)
		if !ok {
			return 0, fmt.Errorf("unknown physics layer %q (expected %s)", n, t.expectedList())
		}
		mask |= physics.LayerBit(bit)
	}
	return mask, nil
}

// Mask3D resolves names into a 3D physics3d.LayerMask. Empty names returns 0 (which defaults to LayerAll).
func (t *PhysicsLayerTable) Mask3D(names ...string) (physics3d.LayerMask, error) {
	var mask physics3d.LayerMask
	for _, n := range names {
		if n == "" {
			continue
		}
		bit, ok := t.Bit(n)
		if !ok {
			return 0, fmt.Errorf("unknown physics layer %q (expected %s)", n, t.expectedList())
		}
		mask |= physics3d.LayerBit(bit)
	}
	return mask, nil
}

const fixPhysicsLayer = "check the layer name spelling or declare it under physics.layers in game.yaml"

func unknownLayerErr(line, col int, field, objID, compName, layerName string, inMask bool, layers *PhysicsLayerTable) *loaderr.Error {
	problem := fmt.Sprintf("sceneformat: object %q component %q: unknown physics layer %q", objID, compName, layerName)
	if inMask {
		problem = fmt.Sprintf("sceneformat: object %q component %q: unknown physics layer %q in mask", objID, compName, layerName)
	}
	return &loaderr.Error{
		Line:     line,
		Column:   col,
		Field:    field,
		Problem:  problem,
		Expected: layers.expectedList(),
		Fix:      fixPhysicsLayer,
	}
}

// ValidatePhysicsLayers checks every named collision layer and mask in built-in physics components of doc
// against layers. Unknown or misspelled layer names return a *loaderr.Error naming the field path.
// Prefer ParseWith with ParseOptions.PhysicsLayers when loading files so errors also carry exact 1-based line and column numbers.
func ValidatePhysicsLayers(doc *SceneDoc, layers *PhysicsLayerTable) error {
	if doc == nil || layers == nil {
		return nil
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Overrides)) {
		if err := validateComponentsPhysicsLayers(doc.Overrides[id].Components, id, loaderr.Path("overrides", id, "components"), layers); err != nil {
			return err
		}
	}
	for li := range doc.Layers {
		if err := validateObjectsPhysicsLayers(doc.Layers[li].Objects, loaderr.Path("layers", li, "objects"), layers); err != nil {
			return err
		}
	}
	return nil
}

func validateObjectsPhysicsLayers(objs []ObjectDoc, base string, layers *PhysicsLayerTable) error {
	for oi := range objs {
		o := &objs[oi]
		op := loaderr.Join(base, oi)
		if err := validateComponentsPhysicsLayers(o.Components, o.ID, loaderr.Join(op, "components"), layers); err != nil {
			return err
		}
		for _, id := range slices.Sorted(maps.Keys(o.Overrides)) {
			if err := validateComponentsPhysicsLayers(o.Overrides[id].Components, o.ID+"/"+id, loaderr.Join(op, "overrides", id, "components"), layers); err != nil {
				return err
			}
		}
		if err := validateObjectsPhysicsLayers(o.Children, loaderr.Join(op, "children"), layers); err != nil {
			return err
		}
	}
	return nil
}

func validateComponentsPhysicsLayers(comps map[string]any, objID, base string, layers *PhysicsLayerTable) error {
	if len(comps) == 0 {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(comps)) {
		if !isLayeredPhysicsComponent(name) {
			continue
		}
		m, ok := comps[name].(map[string]any)
		if !ok {
			continue
		}
		if rawLayer, has := m["layer"]; has {
			if s, ok := rawLayer.(string); ok && s != "" {
				if _, ok := layers.Bit(s); !ok {
					return unknownLayerErr(0, 0, loaderr.Join(base, name, "layer"), objID, name, s, false, layers)
				}
			}
		}
		if rawMask, has := m["mask"]; has {
			switch xs := rawMask.(type) {
			case []string:
				for i, s := range xs {
					if s == "" {
						continue
					}
					if _, ok := layers.Bit(s); !ok {
						return unknownLayerErr(0, 0, loaderr.Join(base, name, "mask", i), objID, name, s, true, layers)
					}
				}
			case []any:
				for i, elem := range xs {
					if s, ok := elem.(string); ok && s != "" {
						if _, ok := layers.Bit(s); !ok {
							return unknownLayerErr(0, 0, loaderr.Join(base, name, "mask", i), objID, name, s, true, layers)
						}
					}
				}
			}
		}
	}
	return nil
}

// checkPhysicsLayerNodes walks the YAML AST alongside doc so an unknown physics layer name
// reports the exact 1-based line and column of the offending YAML value node.
func checkPhysicsLayerNodes(root *yaml.Node, doc *SceneDoc, layers *PhysicsLayerTable) error {
	n := resolveAlias(root)
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = resolveAlias(n.Content[0])
	}
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	if ovs := mappingValue(n, "overrides"); ovs != nil && ovs.Kind == yaml.MappingNode {
		if err := checkOverridesPhysicsLayerNodes(ovs, doc.Overrides, "", "overrides", layers); err != nil {
			return err
		}
	}
	layersNode := mappingValue(n, "layers")
	if layersNode == nil || layersNode.Kind != yaml.SequenceNode {
		return nil
	}
	for li, ln := range layersNode.Content {
		if li >= len(doc.Layers) {
			break
		}
		if err := checkObjectsPhysicsLayerNodes(mappingValue(resolveAlias(ln), "objects"), doc.Layers[li].Objects, loaderr.Path("layers", li, "objects"), layers); err != nil {
			return err
		}
	}
	return nil
}

func checkOverridesPhysicsLayerNodes(ovsNode *yaml.Node, ovs map[string]ObjectOverride, prefix, base string, layers *PhysicsLayerTable) error {
	for i := 0; i+1 < len(ovsNode.Content); i += 2 {
		k, v := ovsNode.Content[i], resolveAlias(ovsNode.Content[i+1])
		if k.Tag == "!!merge" {
			continue
		}
		id := k.Value
		ov, ok := ovs[id]
		if !ok {
			continue
		}
		fullID := id
		if prefix != "" {
			fullID = prefix + "/" + id
		}
		if comps := mappingValue(v, "components"); comps != nil && comps.Kind == yaml.MappingNode {
			if err := checkComponentMapPhysicsLayerNodes(comps, ov.Components, fullID, loaderr.Join(base, id, "components"), layers); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkObjectsPhysicsLayerNodes(seq *yaml.Node, objs []ObjectDoc, base string, layers *PhysicsLayerTable) error {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	for oi, on := range seq.Content {
		if oi >= len(objs) {
			break
		}
		obj := &objs[oi]
		on = resolveAlias(on)
		op := loaderr.Join(base, oi)
		if comps := mappingValue(on, "components"); comps != nil && comps.Kind == yaml.MappingNode {
			if err := checkComponentMapPhysicsLayerNodes(comps, obj.Components, obj.ID, loaderr.Join(op, "components"), layers); err != nil {
				return err
			}
		}
		if ovs := mappingValue(on, "overrides"); ovs != nil && ovs.Kind == yaml.MappingNode {
			if err := checkOverridesPhysicsLayerNodes(ovs, obj.Overrides, obj.ID, loaderr.Join(op, "overrides"), layers); err != nil {
				return err
			}
		}
		if err := checkObjectsPhysicsLayerNodes(mappingValue(on, "children"), obj.Children, loaderr.Join(op, "children"), layers); err != nil {
			return err
		}
	}
	return nil
}

func checkComponentMapPhysicsLayerNodes(compsNode *yaml.Node, comps map[string]any, objID, base string, layers *PhysicsLayerTable) error {
	for i := 0; i+1 < len(compsNode.Content); i += 2 {
		k, v := compsNode.Content[i], resolveAlias(compsNode.Content[i+1])
		if k.Tag == "!!merge" {
			continue
		}
		name := k.Value
		if !isLayeredPhysicsComponent(name) || v == nil || v.Kind != yaml.MappingNode {
			continue
		}
		m, ok := comps[name].(map[string]any)
		if !ok {
			continue
		}
		for j := 0; j+1 < len(v.Content); j += 2 {
			fk, fv := v.Content[j], resolveAlias(v.Content[j+1])
			if fk.Tag == "!!merge" {
				continue
			}
			switch fk.Value {
			case "layer":
				if s, ok := m["layer"].(string); ok && s != "" {
					if _, exists := layers.Bit(s); !exists {
						return unknownLayerErr(fv.Line, fv.Column, loaderr.Join(base, name, "layer"), objID, name, s, false, layers)
					}
				}
			case "mask":
				if fv.Kind == yaml.SequenceNode {
					for idx, item := range fv.Content {
						item = resolveAlias(item)
						if item.Kind == yaml.ScalarNode && item.Value != "" {
							if _, exists := layers.Bit(item.Value); !exists {
								return unknownLayerErr(item.Line, item.Column, loaderr.Join(base, name, "mask", idx), objID, name, item.Value, true, layers)
							}
						}
					}
				}
			}
		}
	}
	return nil
}

// PhysicsSettings configures the 2D and 3D physics worlds constructed by BuildPhysics.
type PhysicsSettings struct {
	// HasGravity2D reports whether Gravity2D overrides the default (0, 0) 2D gravity.
	HasGravity2D bool
	// Gravity2D is the 2D gravity vector in points per second squared.
	Gravity2D math2d.Vec2
	// HasGravity3D reports whether Gravity3D overrides physics3d's default (0, -9.81, 0) gravity.
	HasGravity3D bool
	// Gravity3D is the 3D gravity vector in units per second squared.
	Gravity3D physics3d.Vec3
	// SolverIterations2D overrides RigidWorld.SolverIterations when positive.
	SolverIterations2D int
	// SolverIterations3D overrides physics3d.World.SolverIterations when positive.
	SolverIterations3D int
	// Layers maps named collision layers (from game.yaml physics.layers) to bit indices 0..31.
	Layers *PhysicsLayerTable
}

// PhysicsTriggerEventKind identifies enter, stay, or exit transitions on built scene triggers.
type PhysicsTriggerEventKind int

const (
	// TriggerEnter fires on the first step a matching body overlaps a trigger.
	TriggerEnter PhysicsTriggerEventKind = iota
	// TriggerStay fires on each subsequent step the body remains overlapping.
	TriggerStay
	// TriggerExit fires on the first step the body stops overlapping.
	TriggerExit
)

// String returns "enter", "stay", or "exit".
func (k PhysicsTriggerEventKind) String() string {
	switch k {
	case TriggerEnter:
		return "enter"
	case TriggerStay:
		return "stay"
	case TriggerExit:
		return "exit"
	default:
		return "PhysicsTriggerEventKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// PhysicsTriggerEvent records one trigger overlap transition emitted during PhysicsScene.Step.
type PhysicsTriggerEvent struct {
	Kind      PhysicsTriggerEventKind
	TriggerID string
	BodyID    string
	Is3D      bool
}

type physicsBinding2D struct {
	bo          *BuiltObject
	kind        string // "static", "kinematic", "dynamic"
	body        *physics.Body
	rigid       *physics.RigidBody
	offsetX     float32
	offsetY     float32
	isTrigger   bool
	trigger     *physics.Trigger
	triggerComp string
}

type physicsBinding3D struct {
	bo          *BuiltObject
	kind        string // "static", "kinematic", "dynamic"
	body        *physics3d.Body
	isTrigger   bool
	triggerComp string
}

// PhysicsScene holds the 2D and 3D physics worlds constructed from a BuiltScene's built-in physics components.
// Calling Step advances 2D rigid bodies, 2D kinematic bodies, 2D triggers, and 3D bodies/triggers deterministically,
// synchronizing simulated transforms back into the BuiltScene's objects so sprites and 3D wireframes move live.
type PhysicsScene struct {
	// Scene is the BuiltScene this PhysicsScene drives.
	Scene *BuiltScene
	// World2D holds all 2D bodies (dynamic mirrors, kinematic, static) and 2D triggers for queries and trigger callbacks.
	World2D *physics.World
	// Rigid2D simulates 2D dynamic and static rigid bodies with impulses, friction, and restitution.
	Rigid2D *physics.RigidWorld
	// World3D simulates 3D static, kinematic, dynamic bodies, and 3D triggers.
	World3D *physics3d.World

	// Bodies2D maps object IDs to their 2D query/kinematic Body.
	Bodies2D map[string]*physics.Body
	// RigidBodies2D maps object IDs to their 2D RigidBody.
	RigidBodies2D map[string]*physics.RigidBody
	// Triggers2D maps object IDs to their 2D Trigger.
	Triggers2D map[string]*physics.Trigger
	// Bodies3D maps object IDs to their 3D Body (including 3D triggers with IsTrigger == true).
	Bodies3D map[string]*physics3d.Body
	// TriMeshes3D maps object IDs to their static 3D triangle-mesh colliders.
	TriMeshes3D map[string]*physics3d.TriMeshCollider
	// Heightfields3D maps object IDs to their static 3D terrain heightfield colliders.
	Heightfields3D map[string]*physics3d.HeightfieldCollider

	// OnTrigger is called deterministically for every 2D and 3D trigger enter/stay/exit transition during Step.
	OnTrigger func(PhysicsTriggerEvent)
	// Signals, when non-nil, receives enter/stay/exit events emitted from trigger objects during Step.
	Signals *Signals

	bindings2D    []*physicsBinding2D
	bindings3D    []*physicsBinding3D
	bodyID3D      map[int]string
	triggerComp3D map[int]string
	triggerEvents []PhysicsTriggerEvent
}

// HasPhysics reports whether ps contains at least one 2D or 3D body, collider, or trigger.
func (ps *PhysicsScene) HasPhysics() bool {
	if ps == nil {
		return false
	}
	return len(ps.bindings2D) > 0 || len(ps.bindings3D) > 0 || len(ps.TriMeshes3D) > 0 || len(ps.Heightfields3D) > 0
}

// TriggerEvents returns the trigger events produced by the most recent Step call.
func (ps *PhysicsScene) TriggerEvents() []PhysicsTriggerEvent {
	if ps == nil {
		return nil
	}
	return ps.triggerEvents
}

// BuildPhysics constructs 2D and 3D physics worlds from scene's built-in physics components
// (body2d, collider2d, trigger2d, body3d, collider3d, trigger3d).
// settings may be nil (using default gravity, default solver iterations, and no named layers).
// resolver may be nil unless a body component specifies a shared .physics.yaml material path.
func BuildPhysics(scene *BuiltScene, settings *PhysicsSettings, resolver AssetResolver) (*PhysicsScene, error) {
	if scene == nil || scene.Doc == nil {
		return nil, errors.New("sceneformat: BuildPhysics requires a non-nil BuiltScene")
	}

	var layers *PhysicsLayerTable
	w2 := &physics.World{}
	rw2 := &physics.RigidWorld{}
	w3 := physics3d.NewWorld()

	if settings != nil {
		layers = settings.Layers
		if settings.HasGravity2D {
			w2.GravityX = settings.Gravity2D.X
			w2.GravityY = settings.Gravity2D.Y
			rw2.GravityX = settings.Gravity2D.X
			rw2.GravityY = settings.Gravity2D.Y
		}
		if settings.HasGravity3D {
			w3.Gravity = settings.Gravity3D
		}
		if settings.SolverIterations2D > 0 {
			rw2.SolverIterations = settings.SolverIterations2D
		}
		if settings.SolverIterations3D > 0 {
			w3.SolverIterations = settings.SolverIterations3D
		}
	}

	ps := &PhysicsScene{
		Scene:          scene,
		World2D:        w2,
		Rigid2D:        rw2,
		World3D:        w3,
		Bodies2D:       make(map[string]*physics.Body),
		RigidBodies2D:  make(map[string]*physics.RigidBody),
		Triggers2D:     make(map[string]*physics.Trigger),
		Bodies3D:       make(map[string]*physics3d.Body),
		TriMeshes3D:    make(map[string]*physics3d.TriMeshCollider),
		Heightfields3D: make(map[string]*physics3d.HeightfieldCollider),
		bodyID3D:       make(map[int]string),
		triggerComp3D:  make(map[int]string),
	}

	assetCache := newPhysics3DAssetCache(resolver)
	pmCache := make(map[string]*resource.PhysicsMaterial)
	getPhysicsMat := func(matPath string) (*resource.PhysicsMaterial, error) {
		if m, ok := pmCache[matPath]; ok {
			return m, nil
		}
		pmr, ok := resolver.(PhysicsMaterialResolver)
		if !ok {
			return nil, fmt.Errorf("sceneformat: physics material %q requires a PhysicsMaterialResolver", matPath)
		}
		m, err := pmr.PhysicsMaterial(matPath)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: resolve physics material %q: %w", matPath, err)
		}
		if m == nil {
			return nil, fmt.Errorf("sceneformat: resolver returned nil physics material for %q", matPath)
		}
		pmCache[matPath] = m
		return m, nil
	}

	var walk func(objs []*BuiltObject, parentWorld3D math3d.Transform) error
	walk = func(objs []*BuiltObject, parentWorld3D math3d.Transform) error {
		for _, bo := range objs {
			if bo == nil || bo.Doc == nil {
				continue
			}
			world3D := composeTransform3D(parentWorld3D, ObjectLocalTransform3D(bo.Doc))
			if err := ps.buildObject2D(bo, layers, getPhysicsMat); err != nil {
				return err
			}
			if err := ps.buildObject3D(bo, world3D, layers, getPhysicsMat, assetCache); err != nil {
				return err
			}
			if len(bo.Children) > 0 {
				if err := walk(bo.Children, world3D); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, bl := range scene.Layers {
		if err := walk(bl.Objects, math3d.TransformIdentity()); err != nil {
			return nil, err
		}
	}

	return ps, nil
}

func (ps *PhysicsScene) buildObject2D(
	bo *BuiltObject,
	layers *PhysicsLayerTable,
	getPhysicsMat func(string) (*resource.PhysicsMaterial, error),
) error {
	o := bo.Doc
	rawBody, hasBody := o.Components[Body2DComponentName]
	rawCol, hasCol := o.Components[Collider2DComponentName]
	rawTrig, hasTrig := o.Components[Trigger2DComponentName]
	if !hasBody && !hasCol && !hasTrig {
		return nil
	}

	wx, wy, wrot, sx, sy, ok := decomposeTRS(bo.World)
	if !ok {
		wx, wy = o.X, o.Y
		wrot = o.Rotation
		sx, sy = o.EffectiveScaleX(), o.EffectiveScaleY()
	}
	asx := float32(math.Abs(float64(sx)))
	asy := float32(math.Abs(float64(sy)))
	if asx == 0 {
		asx = 1
	}
	if asy == 0 {
		asy = 1
	}

	// Handle trigger2d first (an object may carry trigger2d independently, or collider2d with isTrigger).
	if hasTrig {
		resolved, err := resolvePhysicsComponent(o.ID, Trigger2DComponentName, rawTrig)
		if err != nil {
			return err
		}
		shape, offX, offY, err := buildShape2D(o.ID, Trigger2DComponentName, resolved, wx, wy, asx, asy)
		if err != nil {
			return err
		}
		layerMask, maskBits, err := resolveLayerMask2D(o.ID, Trigger2DComponentName, resolved, layers)
		if err != nil {
			return err
		}
		tr := ps.makeTrigger2D(o.ID, Trigger2DComponentName, shape, layerMask, maskBits)
		ps.World2D.AddTrigger(tr)
		ps.Rigid2D.Triggers = append(ps.Rigid2D.Triggers, tr)
		ps.Triggers2D[o.ID] = tr
		ps.bindings2D = append(ps.bindings2D, &physicsBinding2D{
			bo:          bo,
			offsetX:     offX,
			offsetY:     offY,
			isTrigger:   true,
			trigger:     tr,
			triggerComp: Trigger2DComponentName,
		})
	}

	if !hasBody && !hasCol {
		return nil
	}

	// Resolve collider2d (or default box if only body2d is provided).
	var colResolved map[string]any
	if hasCol {
		var err error
		colResolved, err = resolvePhysicsComponent(o.ID, Collider2DComponentName, rawCol)
		if err != nil {
			return err
		}
	} else {
		colResolved, _ = builtinPhysicsReg.Resolve(Collider2DComponentName, nil)
	}

	shape, offX, offY, err := buildShape2D(o.ID, Collider2DComponentName, colResolved, wx, wy, asx, asy)
	if err != nil {
		return err
	}
	layerMask, maskBits, err := resolveLayerMask2D(o.ID, Collider2DComponentName, colResolved, layers)
	if err != nil {
		return err
	}

	if hasCol && colResolved["isTrigger"].(bool) && !hasBody {
		tr := ps.makeTrigger2D(o.ID, Collider2DComponentName, shape, layerMask, maskBits)
		ps.World2D.AddTrigger(tr)
		ps.Rigid2D.Triggers = append(ps.Rigid2D.Triggers, tr)
		ps.Triggers2D[o.ID] = tr
		ps.bindings2D = append(ps.bindings2D, &physicsBinding2D{
			bo:          bo,
			offsetX:     offX,
			offsetY:     offY,
			isTrigger:   true,
			trigger:     tr,
			triggerComp: Collider2DComponentName,
		})
		return nil
	}

	// Resolve body2d (default static if only collider2d is present).
	kind := "static"
	var bodyResolved map[string]any
	var rawBodyMap map[string]any
	if hasBody {
		var err error
		bodyResolved, err = resolvePhysicsComponent(o.ID, Body2DComponentName, rawBody)
		if err != nil {
			return err
		}
		rawBodyMap, _ = rawBody.(map[string]any)
		kind = bodyResolved["kind"].(string)
	} else {
		bodyResolved, _ = builtinPhysicsReg.Resolve(Body2DComponentName, map[string]any{"kind": "static"})
	}

	cx, cy := wx+offX, wy+offY
	mass := float32(bodyResolved["mass"].(float64))
	if kind != "dynamic" {
		mass = 0
	}

	var rb *physics.RigidBody
	switch shape.Kind {
	case physics.ShapeCircle:
		rb = physics.NewRigidCircle(cx, cy, shape.Circle.R, mass)
	default:
		rb = physics.NewRigidBox(cx, cy, shape.Box.W, shape.Box.H, mass)
	}
	rb.Tag = o.ID
	rb.Angle = wrot
	rb.Layer = layerMask
	rb.Mask = maskBits
	rb.UserData = bo

	if matPath, _ := bodyResolved["material"].(string); matPath != "" {
		pm, err := getPhysicsMat(matPath)
		if err != nil {
			return fmt.Errorf("sceneformat: object %q component %q: %w", o.ID, Body2DComponentName, err)
		}
		pm.ApplyTo(rb)
	}
	applyBody2DMaterialFields(rb, bodyResolved, rawBodyMap)

	if kind == "dynamic" {
		rb.VX = float32(bodyResolved["vx"].(float64))
		rb.VY = float32(bodyResolved["vy"].(float64))
		rb.SetGravityScale(float32(bodyResolved["gravityScale"].(float64)))
	}

	b := &physics.Body{
		Tag:      o.ID,
		Shape:    shape,
		Layer:    layerMask,
		Mask:     maskBits,
		UserData: bo,
	}
	if kind == "kinematic" || kind == "dynamic" {
		b.VX = float32(bodyResolved["vx"].(float64))
		b.VY = float32(bodyResolved["vy"].(float64))
	}

	ps.World2D.Add(b)
	ps.Rigid2D.AddBody(rb)
	ps.Bodies2D[o.ID] = b
	ps.RigidBodies2D[o.ID] = rb
	ps.bindings2D = append(ps.bindings2D, &physicsBinding2D{
		bo:      bo,
		kind:    kind,
		body:    b,
		rigid:   rb,
		offsetX: offX,
		offsetY: offY,
	})
	return nil
}

func applyBody2DMaterialFields(rb *physics.RigidBody, resolved, raw map[string]any) {
	hasMat := resolved["material"].(string) != ""
	if !hasMat || hasKey(raw, "friction") {
		rb.Friction = float32(resolved["friction"].(float64))
	}
	if !hasMat || hasKey(raw, "restitution") {
		rb.Restitution = float32(resolved["restitution"].(float64))
	}
	if !hasMat || hasKey(raw, "linearDamping") {
		rb.LinearDamping = float32(resolved["linearDamping"].(float64))
	}
	if !hasMat || hasKey(raw, "angularDamping") {
		rb.AngularDamping = float32(resolved["angularDamping"].(float64))
	}
}

func (ps *PhysicsScene) makeTrigger2D(objID, compName string, shape physics.Shape, layer, mask physics.LayerMask) *physics.Trigger {
	emit := func(kind PhysicsTriggerEventKind, b *physics.Body) {
		bodyID := ""
		if b != nil {
			bodyID = b.Tag
		}
		ev := PhysicsTriggerEvent{
			Kind:      kind,
			TriggerID: objID,
			BodyID:    bodyID,
			Is3D:      false,
		}
		ps.triggerEvents = append(ps.triggerEvents, ev)
		if ps.OnTrigger != nil {
			ps.OnTrigger(ev)
		}
		if ps.Signals != nil {
			_ = ps.Signals.Emit(objID, compName+"."+kind.String(), ev)
		}
	}
	return &physics.Trigger{
		Tag:     objID,
		Shape:   shape,
		Layer:   layer,
		Mask:    mask,
		OnEnter: func(b *physics.Body) { emit(TriggerEnter, b) },
		OnStay:  func(b *physics.Body) { emit(TriggerStay, b) },
		OnExit:  func(b *physics.Body) { emit(TriggerExit, b) },
	}
}

func (ps *PhysicsScene) buildObject3D(
	bo *BuiltObject,
	world math3d.Transform,
	layers *PhysicsLayerTable,
	getPhysicsMat func(string) (*resource.PhysicsMaterial, error),
	assetCache *physics3DAssetCache,
) error {
	o := bo.Doc
	rawBody, hasBody := o.Components[Body3DComponentName]
	rawCol, hasCol := o.Components[Collider3DComponentName]
	rawTrig, hasTrig := o.Components[Trigger3DComponentName]
	if !hasBody && !hasCol && !hasTrig {
		return nil
	}

	pos := physics3d.Vec3{X: world.Pos.X, Y: world.Pos.Y, Z: world.Pos.Z}
	rot := physics3d.Quat{X: world.Rot.X, Y: world.Rot.Y, Z: world.Rot.Z, W: world.Rot.W}
	sx := float32(math.Abs(float64(world.Scale.X)))
	sy := float32(math.Abs(float64(world.Scale.Y)))
	sz := float32(math.Abs(float64(world.Scale.Z)))
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}

	if hasTrig {
		resolved, err := resolvePhysicsComponent(o.ID, Trigger3DComponentName, rawTrig)
		if err != nil {
			return err
		}
		shape, err := buildShape3D(o, Trigger3DComponentName, resolved, sx, sy, sz, assetCache)
		if err != nil {
			return err
		}
		layerMask, maskBits, err := resolveLayerMask3D(o.ID, Trigger3DComponentName, resolved, layers)
		if err != nil {
			return err
		}
		tb := physics3d.NewStaticBody(shape, pos)
		tb.Tag = o.ID
		tb.Rot = rot
		tb.Layer = layerMask
		tb.Mask = maskBits
		tb.IsTrigger = true
		stored := ps.World3D.AddBody(tb)
		ps.Bodies3D[o.ID] = stored
		ps.bodyID3D[stored.ID] = o.ID
		ps.triggerComp3D[stored.ID] = Trigger3DComponentName
		ps.bindings3D = append(ps.bindings3D, &physicsBinding3D{
			bo:          bo,
			kind:        "static",
			body:        stored,
			isTrigger:   true,
			triggerComp: Trigger3DComponentName,
		})
	}

	if !hasBody && !hasCol {
		return nil
	}

	var colResolved map[string]any
	if hasCol {
		var err error
		colResolved, err = resolvePhysicsComponent(o.ID, Collider3DComponentName, rawCol)
		if err != nil {
			return err
		}
	} else {
		colResolved, _ = builtinPhysicsReg.Resolve(Collider3DComponentName, nil)
	}

	kind := "static"
	var bodyResolved map[string]any
	var rawBodyMap map[string]any
	if hasBody {
		var err error
		bodyResolved, err = resolvePhysicsComponent(o.ID, Body3DComponentName, rawBody)
		if err != nil {
			return err
		}
		rawBodyMap, _ = rawBody.(map[string]any)
		kind = bodyResolved["kind"].(string)
	} else {
		bodyResolved, _ = builtinPhysicsReg.Resolve(Body3DComponentName, map[string]any{"kind": "static"})
	}

	matPath, err := body3DMaterialPath(o.ID, bodyResolved)
	if err != nil {
		return err
	}
	var physMat *resource.PhysicsMaterial
	if matPath != "" {
		physMat, err = getPhysicsMat(matPath)
		if err != nil {
			return fmt.Errorf("sceneformat: object %q component %q: %w", o.ID, Body3DComponentName, err)
		}
	}

	layerMask, maskBits, err := resolveBodyAndColliderLayerMask3D(o.ID, colResolved, bodyResolved, layers)
	if err != nil {
		return err
	}
	isTrigger := (hasCol && colResolved["isTrigger"].(bool)) || kind == "trigger"

	shapeKind := colResolved["shape"].(string)
	if shapeKind == "trimesh" || shapeKind == "heightfield" {
		if kind != "static" || isTrigger {
			return fmt.Errorf("sceneformat: object %q component %q: %s collider must be static and non-trigger", o.ID, Collider3DComponentName, shapeKind)
		}
		friction := float32(bodyResolved["friction"].(float64))
		restitution := float32(bodyResolved["restitution"].(float64))
		if physMat != nil {
			if !hasKey(rawBodyMap, "friction") && physMat.Friction != nil {
				friction = *physMat.Friction
			}
			if !hasKey(rawBodyMap, "restitution") && physMat.Restitution != nil {
				restitution = *physMat.Restitution
			}
		}
		if shapeKind == "trimesh" {
			tm, err := buildTriMesh3D(o, Collider3DComponentName, colResolved, world, assetCache)
			if err != nil {
				return err
			}
			tm.Layer = layerMask
			tm.Mask = maskBits
			tm.Friction = friction
			tm.Restitution = restitution
			stored := ps.World3D.AddTriMesh(tm)
			ps.TriMeshes3D[o.ID] = stored
			return nil
		}
		hf, err := buildHeightfield3D(o, Collider3DComponentName, colResolved, world, assetCache)
		if err != nil {
			return err
		}
		hf.Layer = layerMask
		hf.Mask = maskBits
		hf.Friction = friction
		hf.Restitution = restitution
		stored := ps.World3D.AddHeightfield(hf)
		ps.Heightfields3D[o.ID] = stored
		return nil
	}

	shape, err := buildShape3D(o, Collider3DComponentName, colResolved, sx, sy, sz, assetCache)
	if err != nil {
		return err
	}

	var b physics3d.Body
	switch kind {
	case "dynamic":
		b = physics3d.NewDynamicBody(shape, pos, float32(bodyResolved["mass"].(float64)))
		b.SetGravityScale(float32(bodyResolved["gravityScale"].(float64)))
		b.LockRotation = bodyResolved["lockRotation"].(bool)
	case "kinematic":
		b = physics3d.Body{
			Shape:     shape,
			Pos:       pos,
			Rot:       rot,
			Kinematic: true,
			Friction:  0.5,
		}
	default: // "static" or "trigger"
		b = physics3d.NewStaticBody(shape, pos)
	}
	b.Tag = o.ID
	b.Rot = rot
	b.Layer = layerMask
	b.Mask = maskBits
	b.IsTrigger = isTrigger
	if kind == "dynamic" || kind == "kinematic" {
		b.Vel = physics3d.Vec3{
			X: float32(bodyResolved["vx"].(float64)),
			Y: float32(bodyResolved["vy"].(float64)),
			Z: float32(bodyResolved["vz"].(float64)),
		}
	}

	if physMat != nil {
		physMat.ApplyTo3D(&b)
	}
	applyBody3DMaterialFields(&b, bodyResolved, rawBodyMap, physMat != nil)

	stored := ps.World3D.AddBody(b)
	ps.Bodies3D[o.ID] = stored
	ps.bodyID3D[stored.ID] = o.ID
	trigComp := Collider3DComponentName
	if kind == "trigger" && !(hasCol && colResolved["isTrigger"].(bool)) {
		trigComp = Body3DComponentName
	}
	if isTrigger {
		ps.triggerComp3D[stored.ID] = trigComp
	}
	ps.bindings3D = append(ps.bindings3D, &physicsBinding3D{
		bo:          bo,
		kind:        kind,
		body:        stored,
		isTrigger:   isTrigger,
		triggerComp: trigComp,
	})
	return nil
}

func body3DMaterialPath(objID string, resolved map[string]any) (string, error) {
	matPath, _ := resolved["material"].(string)
	pmPath, _ := resolved["physicsMaterial"].(string)
	if matPath != "" && pmPath != "" && matPath != pmPath {
		return "", &ComponentError{
			Object:    objID,
			Component: Body3DComponentName,
			Field:     "physicsMaterial",
			Msg:       fmt.Sprintf("cannot set both material (%q) and physicsMaterial (%q) to different paths", matPath, pmPath),
		}
	}
	if pmPath != "" {
		return pmPath, nil
	}
	return matPath, nil
}

func resolveBodyAndColliderLayerMask3D(objID string, colResolved, bodyResolved map[string]any, layers *PhysicsLayerTable) (physics3d.LayerMask, physics3d.LayerMask, error) {
	colLayer, _ := colResolved["layer"].(string)
	colMask, _ := colResolved["mask"].([]string)
	bodyLayer, _ := bodyResolved["layer"].(string)
	bodyMask, _ := bodyResolved["mask"].([]string)

	layerComp := Collider3DComponentName
	layerName := colLayer
	if layerName == "" && bodyLayer != "" {
		layerComp = Body3DComponentName
		layerName = bodyLayer
	}
	maskComp := Collider3DComponentName
	maskNames := colMask
	if len(maskNames) == 0 && len(bodyMask) > 0 {
		maskComp = Body3DComponentName
		maskNames = bodyMask
	}

	var layer physics3d.LayerMask
	if layerName != "" {
		bit, ok := layers.Bit(layerName)
		if !ok {
			return 0, 0, unknownLayerErr(0, 0, loaderr.Path("components", layerComp, "layer"), objID, layerComp, layerName, false, layers)
		}
		layer = physics3d.LayerBit(bit)
	}
	var mask physics3d.LayerMask
	for i, n := range maskNames {
		if n == "" {
			continue
		}
		bit, ok := layers.Bit(n)
		if !ok {
			return 0, 0, unknownLayerErr(0, 0, loaderr.Path("components", maskComp, "mask", i), objID, maskComp, n, true, layers)
		}
		mask |= physics3d.LayerBit(bit)
	}
	return layer, mask, nil
}

func applyBody3DMaterialFields(b *physics3d.Body, resolved, raw map[string]any, hasMat bool) {
	if !hasMat || hasKey(raw, "friction") {
		b.Friction = float32(resolved["friction"].(float64))
	}
	if !hasMat || hasKey(raw, "restitution") {
		b.Restitution = float32(resolved["restitution"].(float64))
	}
	if !hasMat || hasKey(raw, "linearDamping") {
		b.LinearDamping = float32(resolved["linearDamping"].(float64))
	}
	if !hasMat || hasKey(raw, "angularDamping") {
		b.AngularDamping = float32(resolved["angularDamping"].(float64))
	}
}

func hasKey(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func resolvePhysicsComponent(objID, compName string, raw any) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, &ComponentError{
			Object:    objID,
			Component: compName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := builtinPhysicsReg.Resolve(compName, m)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.Object = objID
		}
		return nil, err
	}
	return resolved, nil
}

func buildShape2D(objID, compName string, resolved map[string]any, wx, wy, asx, asy float32) (physics.Shape, float32, float32, error) {
	shapeKind := resolved["shape"].(string)
	offX := float32(resolved["offsetX"].(float64)) * asx
	offY := float32(resolved["offsetY"].(float64)) * asy
	cx, cy := wx+offX, wy+offY

	switch shapeKind {
	case "circle":
		r := float32(resolved["radius"].(float64)) * max(asx, asy)
		if r <= 0 {
			return physics.Shape{}, 0, 0, &ComponentError{Object: objID, Component: compName, Field: "radius", Msg: "radius must be positive for circle shape"}
		}
		return physics.CircleShape(physics.Circle{X: cx, Y: cy, R: r}), offX, offY, nil
	default: // "box"
		w := float32(resolved["width"].(float64)) * asx
		h := float32(resolved["height"].(float64)) * asy
		if w <= 0 || h <= 0 {
			return physics.Shape{}, 0, 0, &ComponentError{Object: objID, Component: compName, Field: "width", Msg: "width and height must be positive for box shape"}
		}
		return physics.BoxShape(physics.Box{X: cx - w*0.5, Y: cy - h*0.5, W: w, H: h}), offX, offY, nil
	}
}

func buildShape3D(o *ObjectDoc, compName string, resolved map[string]any, sx, sy, sz float32, cache *physics3DAssetCache) (physics3d.Shape, error) {
	objID := ""
	if o != nil {
		objID = o.ID
	}
	shapeKind := resolved["shape"].(string)
	center := physics3d.Vec3{
		X: float32(resolved["offsetX"].(float64)) * sx,
		Y: float32(resolved["offsetY"].(float64)) * sy,
		Z: float32(resolved["offsetZ"].(float64)) * sz,
	}
	qLocal := colliderLocalRot3D(resolved)
	switch shapeKind {
	case "sphere":
		r := float32(resolved["radius"].(float64)) * max(sx, max(sy, sz))
		if r <= 0 {
			return physics3d.Shape{}, &ComponentError{Object: objID, Component: compName, Field: "radius", Msg: "radius must be positive for sphere shape"}
		}
		return physics3d.SphereShape(physics3d.Sphere{Center: center, Radius: r}), nil
	case "capsule":
		r := float32(resolved["radius"].(float64)) * max(sx, sz)
		h := float32(resolved["height"].(float64)) * sy
		if r <= 0 {
			return physics3d.Shape{}, &ComponentError{Object: objID, Component: compName, Field: "radius", Msg: "radius must be positive for capsule shape"}
		}
		capShape := physics3d.VerticalCapsule(physics3d.Vec3{}, r, h)
		capShape.A = rotateLocalVec3D(qLocal, capShape.A).Add(center)
		capShape.B = rotateLocalVec3D(qLocal, capShape.B).Add(center)
		return physics3d.CapsuleShape(capShape), nil
	case "cylinder":
		r := float32(resolved["radius"].(float64)) * max(sx, sz)
		h := float32(resolved["height"].(float64)) * sy
		if r <= 0 {
			return physics3d.Shape{}, &ComponentError{Object: objID, Component: compName, Field: "radius", Msg: "radius must be positive for cylinder shape"}
		}
		if h <= 0 {
			return physics3d.Shape{}, &ComponentError{Object: objID, Component: compName, Field: "height", Msg: "height must be positive for cylinder shape"}
		}
		cyl := physics3d.VerticalCylinder(physics3d.Vec3{}, r, h)
		cyl.A = rotateLocalVec3D(qLocal, cyl.A).Add(center)
		cyl.B = rotateLocalVec3D(qLocal, cyl.B).Add(center)
		return physics3d.CylinderShape(cyl), nil
	case "convex":
		return buildConvexHull3D(o, compName, resolved, sx, sy, sz, center, qLocal, cache)
	default: // "box"
		w := float32(resolved["width"].(float64)) * sx
		h := float32(resolved["height"].(float64)) * sy
		d := float32(resolved["depth"].(float64)) * sz
		if w <= 0 || h <= 0 || d <= 0 {
			return physics3d.Shape{}, &ComponentError{Object: objID, Component: compName, Field: "width", Msg: "width, height, and depth must be positive for box shape"}
		}
		return physics3d.Shape{
			Kind: physics3d.ShapeBox,
			Box: physics3d.Box{
				Center:      center,
				HalfExtents: physics3d.Vec3{X: w * 0.5, Y: h * 0.5, Z: d * 0.5},
			},
			Rot: qLocal,
		}, nil
	}
}

func resolveLayerMask2D(objID, compName string, resolved map[string]any, layers *PhysicsLayerTable) (physics.LayerMask, physics.LayerMask, error) {
	layerName := resolved["layer"].(string)
	maskNames, _ := resolved["mask"].([]string)

	var layer physics.LayerMask
	if layerName != "" {
		bit, ok := layers.Bit(layerName)
		if !ok {
			return 0, 0, unknownLayerErr(0, 0, loaderr.Path("components", compName, "layer"), objID, compName, layerName, false, layers)
		}
		layer = physics.LayerBit(bit)
	}
	var mask physics.LayerMask
	for i, n := range maskNames {
		if n == "" {
			continue
		}
		bit, ok := layers.Bit(n)
		if !ok {
			return 0, 0, unknownLayerErr(0, 0, loaderr.Path("components", compName, "mask", i), objID, compName, n, true, layers)
		}
		mask |= physics.LayerBit(bit)
	}
	return layer, mask, nil
}

func resolveLayerMask3D(objID, compName string, resolved map[string]any, layers *PhysicsLayerTable) (physics3d.LayerMask, physics3d.LayerMask, error) {
	layerName := resolved["layer"].(string)
	maskNames, _ := resolved["mask"].([]string)

	var layer physics3d.LayerMask
	if layerName != "" {
		bit, ok := layers.Bit(layerName)
		if !ok {
			return 0, 0, unknownLayerErr(0, 0, loaderr.Path("components", compName, "layer"), objID, compName, layerName, false, layers)
		}
		layer = physics3d.LayerBit(bit)
	}
	var mask physics3d.LayerMask
	for i, n := range maskNames {
		if n == "" {
			continue
		}
		bit, ok := layers.Bit(n)
		if !ok {
			return 0, 0, unknownLayerErr(0, 0, loaderr.Path("components", compName, "mask", i), objID, compName, n, true, layers)
		}
		mask |= physics3d.LayerBit(bit)
	}
	return layer, mask, nil
}

// Step advances the 2D and 3D physics simulations by dt, resolves collisions and triggers,
// and writes updated positions and rotations back onto the BuiltScene's objects.
func (ps *PhysicsScene) Step(dt time.Duration) {
	if ps == nil || dt <= 0 {
		return
	}
	ps.triggerEvents = ps.triggerEvents[:0]

	// 1. Step 2D kinematic bodies against solid 2D bodies.
	dtSec := float32(dt.Seconds())
	for _, b2 := range ps.bindings2D {
		if b2.isTrigger || b2.kind != "kinematic" || b2.body == nil {
			continue
		}
		ps.stepKinematic2D(b2, dtSec)
	}

	// 2. Step 2D rigid bodies and sync poses back to query bodies and BuiltObjects.
	if len(ps.Rigid2D.Bodies) > 0 {
		ps.Rigid2D.Step(dt)
		for _, b2 := range ps.bindings2D {
			if b2.isTrigger || b2.kind != "dynamic" || b2.rigid == nil {
				continue
			}
			syncShape2DToCenter(b2.body, b2.rigid.X, b2.rigid.Y)
			b2.body.VX = b2.rigid.VX
			b2.body.VY = b2.rigid.VY
			syncBuiltObject2D(b2.bo, b2.rigid.X-b2.offsetX, b2.rigid.Y-b2.offsetY, b2.rigid.Angle)
		}
	}

	// 3. Evaluate 2D triggers against updated 2D body shapes.
	if len(ps.World2D.Triggers) > 0 {
		ps.World2D.StepTriggers()
	}

	// 4. Step 3D kinematic bodies and 3D rigid world.
	if len(ps.bindings3D) > 0 {
		for _, b3 := range ps.bindings3D {
			if b3.isTrigger || b3.kind != "kinematic" || b3.body == nil {
				continue
			}
			ps.stepKinematic3D(b3, dtSec)
		}
		ps.World3D.Step(dt)
		for _, b3 := range ps.bindings3D {
			if b3.isTrigger || b3.body == nil {
				continue
			}
			if b3.kind == "dynamic" || b3.kind == "kinematic" {
				syncBuiltObject3D(b3.bo, b3.body.Pos, b3.body.Rot)
			}
		}
		for _, ev := range ps.World3D.ContactEvents() {
			if !ev.IsTrigger || ev.BodyA == nil || ev.BodyB == nil {
				continue
			}
			kind := TriggerEnter
			switch ev.Kind {
			case physics3d.ContactStay:
				kind = TriggerStay
			case physics3d.ContactExit:
				kind = TriggerExit
			}
			ps.emit3DTriggerPair(kind, ev.BodyA, ev.BodyB)
		}
	}
}

func (ps *PhysicsScene) emit3DTriggerPair(kind PhysicsTriggerEventKind, a, b *physics3d.Body) {
	dispatch := func(trig, other *physics3d.Body) {
		trigID := ps.bodyID3D[trig.ID]
		otherID := ps.bodyID3D[other.ID]
		ev := PhysicsTriggerEvent{
			Kind:      kind,
			TriggerID: trigID,
			BodyID:    otherID,
			Is3D:      true,
		}
		ps.triggerEvents = append(ps.triggerEvents, ev)
		if ps.OnTrigger != nil {
			ps.OnTrigger(ev)
		}
		if ps.Signals != nil {
			if comp := ps.triggerComp3D[trig.ID]; comp != "" {
				_ = ps.Signals.Emit(trigID, comp+"."+kind.String(), ev)
			}
		}
	}
	if a.IsTrigger {
		dispatch(a, b)
	}
	if b.IsTrigger {
		dispatch(b, a)
	}
}

func (ps *PhysicsScene) stepKinematic2D(b2 *physicsBinding2D, dtSec float32) {
	b := b2.body
	dx := b.VX * dtSec
	dy := b.VY * dtSec
	if dx == 0 && dy == 0 {
		return
	}
	bounds := b.Shape.Bounds()
	minExtent := min(bounds.W, bounds.H)
	steps := 1
	if minExtent > 0 {
		if sx := int(math.Ceil(math.Abs(float64(dx)) / float64(minExtent*0.5))); sx > steps {
			steps = sx
		}
		if sy := int(math.Ceil(math.Abs(float64(dy)) / float64(minExtent*0.5))); sy > steps {
			steps = sy
		}
	}
	stepDX := dx / float32(steps)
	stepDY := dy / float32(steps)

	for range steps {
		if stepDX != 0 {
			b.Translate(stepDX, 0)
			for _, other := range ps.World2D.Bodies {
				if other == nil || other == b || !physics.CanCollide(b, other) {
					continue
				}
				if c := physics.TestShapes(b.Shape, other.Shape); c.Hit {
					b.Translate(c.NormalX*c.Depth, 0)
					if (stepDX > 0 && c.NormalX < 0) || (stepDX < 0 && c.NormalX > 0) {
						b.VX = 0
						stepDX = 0
					}
				}
			}
		}
		if stepDY != 0 {
			b.Translate(0, stepDY)
			for _, other := range ps.World2D.Bodies {
				if other == nil || other == b || !physics.CanCollide(b, other) {
					continue
				}
				if c := physics.TestShapes(b.Shape, other.Shape); c.Hit {
					b.Translate(0, c.NormalY*c.Depth)
					if (stepDY > 0 && c.NormalY < 0) || (stepDY < 0 && c.NormalY > 0) {
						b.VY = 0
						stepDY = 0
					}
				}
			}
		}
	}

	cx, cy := shapeCenter2D(b.Shape)
	if b2.rigid != nil {
		b2.rigid.X = cx
		b2.rigid.Y = cy
		syncRigidShape2D(b2.rigid)
	}
	wrot := b2.bo.Doc.Rotation
	if _, _, dr, _, _, ok := decomposeTRS(b2.bo.World); ok {
		wrot = dr
	}
	syncBuiltObject2D(b2.bo, cx-b2.offsetX, cy-b2.offsetY, wrot)
}

func (ps *PhysicsScene) stepKinematic3D(b3 *physicsBinding3D, dtSec float32) {
	b := b3.body
	delta := b.Vel.Scale(dtSec)
	if delta.LenSq() == 0 {
		return
	}
	b.Pos = b.Pos.Add(delta)
	pushOut := func(c physics3d.Contact) {
		if !c.Hit {
			return
		}
		b.Pos = b.Pos.Add(c.Normal.Scale(c.Depth))
		if vn := b.Vel.Dot(c.Normal); vn < 0 {
			b.Vel = b.Vel.Sub(c.Normal.Scale(vn))
		}
	}
	for _, other := range ps.World3D.Bodies() {
		if other == nil || other == b || other.IsTrigger || !physics3d.CanCollide(b, other) {
			continue
		}
		pushOut(physics3d.TestShapes(b.WorldShape(), other.WorldShape()))
	}
	for _, tm := range ps.World3D.TriMeshes() {
		if tm == nil || !layersMatch3D(b.Layer, b.Mask, tm.Layer, tm.Mask) {
			continue
		}
		pushOut(tm.TestShape(b.WorldShape()))
	}
	for _, hf := range ps.World3D.Heightfields() {
		if hf == nil || !layersMatch3D(b.Layer, b.Mask, hf.Layer, hf.Mask) {
			continue
		}
		pushOut(hf.TestShape(b.WorldShape()))
	}
}

func layersMatch3D(aLayer, aMask, bLayer, bMask physics3d.LayerMask) bool {
	if aLayer == 0 {
		aLayer = physics3d.LayerAll
	}
	if aMask == 0 {
		aMask = physics3d.LayerAll
	}
	if bLayer == 0 {
		bLayer = physics3d.LayerAll
	}
	if bMask == 0 {
		bMask = physics3d.LayerAll
	}
	return (aLayer&bMask) != 0 && (bLayer&aMask) != 0
}

func shapeCenter2D(s physics.Shape) (float32, float32) {
	switch s.Kind {
	case physics.ShapeCircle:
		return s.Circle.X, s.Circle.Y
	case physics.ShapeBox:
		return s.Box.Center()
	default:
		return 0, 0
	}
}

func syncShape2DToCenter(b *physics.Body, cx, cy float32) {
	if b == nil {
		return
	}
	switch b.Shape.Kind {
	case physics.ShapeBox:
		b.Shape.Box.X = cx - b.Shape.Box.W*0.5
		b.Shape.Box.Y = cy - b.Shape.Box.H*0.5
	case physics.ShapeCircle:
		b.Shape.Circle.X = cx
		b.Shape.Circle.Y = cy
	}
}

func syncRigidShape2D(rb *physics.RigidBody) {
	switch rb.Shape.Kind {
	case physics.ShapeBox:
		rb.Shape.Box.X = rb.X - rb.Shape.Box.W*0.5
		rb.Shape.Box.Y = rb.Y - rb.Shape.Box.H*0.5
	case physics.ShapeCircle:
		rb.Shape.Circle.X = rb.X
		rb.Shape.Circle.Y = rb.Y
	}
}

func syncBuiltObject2D(bo *BuiltObject, wx, wy, wrot float32) {
	if bo == nil || bo.Doc == nil {
		return
	}
	if bo.Parent == nil {
		bo.Doc.X = wx
		bo.Doc.Y = wy
		bo.Doc.Rotation = wrot
	} else {
		// Convert world position and rotation back into parent's local space.
		if inv, ok := bo.Parent.World.Invert(); ok {
			lp := inv.TransformPoint(math2d.Vec2{X: wx, Y: wy})
			bo.Doc.X = lp.X
			bo.Doc.Y = lp.Y
		} else {
			bo.Doc.X = wx
			bo.Doc.Y = wy
		}
		if _, _, prot, psx, psy, ok := decomposeTRS(bo.Parent.World); ok {
			if psx*psy < 0 {
				bo.Doc.Rotation = prot - wrot
			} else {
				bo.Doc.Rotation = wrot - prot
			}
		} else {
			bo.Doc.Rotation = wrot
		}
	}
	bo.Sync()
}

func builtObjectWorldTransform3D(bo *BuiltObject) math3d.Transform {
	if bo == nil || bo.Doc == nil {
		return math3d.TransformIdentity()
	}
	local := ObjectLocalTransform3D(bo.Doc)
	if bo.Parent == nil {
		return local
	}
	return composeTransform3D(builtObjectWorldTransform3D(bo.Parent), local)
}

func syncBuiltObject3D(bo *BuiltObject, pos physics3d.Vec3, rot physics3d.Quat) {
	if bo == nil || bo.Doc == nil {
		return
	}
	localPos := pos
	localRot := rot
	if bo.Parent != nil {
		parentWorld := builtObjectWorldTransform3D(bo.Parent)
		parentPos := physics3d.Vec3{X: parentWorld.Pos.X, Y: parentWorld.Pos.Y, Z: parentWorld.Pos.Z}
		parentRot := physics3d.Quat{X: parentWorld.Rot.X, Y: parentWorld.Rot.Y, Z: parentWorld.Rot.Z, W: parentWorld.Rot.W}
		invParentRot := parentRot.Conjugate().Norm()
		rel := invParentRot.RotateVec(pos.Sub(parentPos))
		if parentWorld.Scale.X != 0 {
			rel.X /= parentWorld.Scale.X
		}
		if parentWorld.Scale.Y != 0 {
			rel.Y /= parentWorld.Scale.Y
		}
		if parentWorld.Scale.Z != 0 {
			rel.Z /= parentWorld.Scale.Z
		}
		localPos = rel
		localRot = invParentRot.Mul(rot).Norm()
	}
	bo.Doc.X = localPos.X
	bo.Doc.Y = localPos.Y
	bo.Doc.PosZ = localPos.Z
	rx, ry, rz := quatToEulerXYZ(localRot)
	bo.Doc.RotX = rx
	bo.Doc.RotY = ry
	bo.Doc.RotZ = rz
	bo.Sync()
}

func composeTransform3D(parent, local math3d.Transform) math3d.Transform {
	scaled := physics3d.Vec3{
		X: local.Pos.X * parent.Scale.X,
		Y: local.Pos.Y * parent.Scale.Y,
		Z: local.Pos.Z * parent.Scale.Z,
	}
	pq := physics3d.Quat{X: parent.Rot.X, Y: parent.Rot.Y, Z: parent.Rot.Z, W: parent.Rot.W}
	rotated := pq.RotateVec(scaled)
	return math3d.Transform{
		Pos: math3d.Vec3{
			X: parent.Pos.X + rotated.X,
			Y: parent.Pos.Y + rotated.Y,
			Z: parent.Pos.Z + rotated.Z,
		},
		Rot: parent.Rot.Mul(local.Rot).Norm(),
		Scale: math3d.Vec3{
			X: parent.Scale.X * local.Scale.X,
			Y: parent.Scale.Y * local.Scale.Y,
			Z: parent.Scale.Z * local.Scale.Z,
		},
	}
}

// quatToEulerXYZ converts a unit quaternion representing Rz * Ry * Rx into Euler angles (rx, ry, rz).
func quatToEulerXYZ(q physics3d.Quat) (rx, ry, rz float32) {
	x, y, z, w := float64(q.X), float64(q.Y), float64(q.Z), float64(q.W)
	// Rz * Ry * Rx rotation matrix entries:
	sinp := 2 * (w*y - z*x)
	if sinp >= 1 {
		ry = float32(math.Pi / 2)
	} else if sinp <= -1 {
		ry = float32(-math.Pi / 2)
	} else {
		ry = float32(math.Asin(sinp))
	}
	rx = float32(math.Atan2(2*(w*x+y*z), 1-2*(x*x+y*y)))
	rz = float32(math.Atan2(2*(w*z+x*y), 1-2*(y*y+z*z)))
	return rx, ry, rz
}

// PhysicsDebugShape2D describes a 2D collider or trigger outline extracted from an ObjectDoc for editor debug drawing.
type PhysicsDebugShape2D struct {
	Shape     physics.Shape
	Angle     float32
	IsTrigger bool
	BodyKind  string // "static", "kinematic", "dynamic", or "" for trigger-only
}

// PhysicsDebugShape3D describes a 3D collider or trigger wireframe extracted from an ObjectDoc for editor debug drawing.
type PhysicsDebugShape3D struct {
	Shape       physics3d.Shape
	TriMesh     *physics3d.TriMeshCollider
	Heightfield *physics3d.HeightfieldCollider
	IsTrigger   bool
	BodyKind    string
}

// ExtractPhysicsDebug2D extracts any 2D collider and trigger shapes from o at its current transform for debug drawing.
func ExtractPhysicsDebug2D(o *ObjectDoc, world *math2d.Affine) []PhysicsDebugShape2D {
	if o == nil || len(o.Components) == 0 {
		return nil
	}
	wx, wy, wrot, sx, sy := o.X, o.Y, o.Rotation, o.EffectiveScaleX(), o.EffectiveScaleY()
	if world != nil {
		if dx, dy, dr, dsx, dsy, ok := decomposeTRS(*world); ok {
			wx, wy, wrot, sx, sy = dx, dy, dr, dsx, dsy
		}
	}
	asx := float32(math.Abs(float64(sx)))
	asy := float32(math.Abs(float64(sy)))
	if asx == 0 {
		asx = 1
	}
	if asy == 0 {
		asy = 1
	}

	bodyKind := "static"
	if rawBody, ok := o.Components[Body2DComponentName]; ok {
		if res, err := resolvePhysicsComponent(o.ID, Body2DComponentName, rawBody); err == nil {
			bodyKind = res["kind"].(string)
		}
	}

	var out []PhysicsDebugShape2D
	if rawCol, ok := o.Components[Collider2DComponentName]; ok {
		if res, err := resolvePhysicsComponent(o.ID, Collider2DComponentName, rawCol); err == nil {
			if sh, _, _, err := buildShape2D(o.ID, Collider2DComponentName, res, wx, wy, asx, asy); err == nil {
				out = append(out, PhysicsDebugShape2D{
					Shape:     sh,
					Angle:     wrot,
					IsTrigger: res["isTrigger"].(bool),
					BodyKind:  bodyKind,
				})
			}
		}
	}
	if rawTrig, ok := o.Components[Trigger2DComponentName]; ok {
		if res, err := resolvePhysicsComponent(o.ID, Trigger2DComponentName, rawTrig); err == nil {
			if sh, _, _, err := buildShape2D(o.ID, Trigger2DComponentName, res, wx, wy, asx, asy); err == nil {
				out = append(out, PhysicsDebugShape2D{
					Shape:     sh,
					Angle:     wrot,
					IsTrigger: true,
				})
			}
		}
	}
	return out
}

// ExtractPhysicsDebug3D extracts any 3D collider and trigger shapes from o using its local transform.
func ExtractPhysicsDebug3D(o *ObjectDoc) []PhysicsDebugShape3D {
	if o == nil {
		return nil
	}
	return ExtractPhysicsDebug3DAt(o, ObjectLocalTransform3D(o))
}

// ExtractPhysicsDebug3DAt extracts any 3D collider and trigger shapes from o at the given composed world transform.
func ExtractPhysicsDebug3DAt(o *ObjectDoc, world math3d.Transform) []PhysicsDebugShape3D {
	return ExtractPhysicsDebug3DAtWithResolver(o, world, nil)
}

// ExtractPhysicsDebug3DAtWithResolver extracts any 3D collider and trigger shapes from o at the given composed world transform,
// using resolver (when non-nil) to load glTF models for convex/trimesh colliders and terrains for heightfield colliders.
func ExtractPhysicsDebug3DAtWithResolver(o *ObjectDoc, world math3d.Transform, resolver AssetResolver) []PhysicsDebugShape3D {
	if o == nil || len(o.Components) == 0 {
		return nil
	}
	pos := physics3d.Vec3{X: world.Pos.X, Y: world.Pos.Y, Z: world.Pos.Z}
	rot := physics3d.Quat{X: world.Rot.X, Y: world.Rot.Y, Z: world.Rot.Z, W: world.Rot.W}
	sx := float32(math.Abs(float64(world.Scale.X)))
	sy := float32(math.Abs(float64(world.Scale.Y)))
	sz := float32(math.Abs(float64(world.Scale.Z)))
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}

	bodyKind := "static"
	if rawBody, ok := o.Components[Body3DComponentName]; ok {
		if res, err := resolvePhysicsComponent(o.ID, Body3DComponentName, rawBody); err == nil {
			bodyKind = res["kind"].(string)
		}
	}

	cache := newPhysics3DAssetCache(resolver)
	var out []PhysicsDebugShape3D
	if rawCol, ok := o.Components[Collider3DComponentName]; ok {
		if res, err := resolvePhysicsComponent(o.ID, Collider3DComponentName, rawCol); err == nil {
			isTrig := res["isTrigger"].(bool) || bodyKind == "trigger"
			switch res["shape"].(string) {
			case "trimesh":
				if tm, err := buildTriMesh3D(o, Collider3DComponentName, res, world, cache); err == nil {
					out = append(out, PhysicsDebugShape3D{
						TriMesh:   tm,
						IsTrigger: isTrig,
						BodyKind:  bodyKind,
					})
				}
			case "heightfield":
				if hf, err := buildHeightfield3D(o, Collider3DComponentName, res, world, cache); err == nil {
					out = append(out, PhysicsDebugShape3D{
						Heightfield: hf,
						IsTrigger:   isTrig,
						BodyKind:    bodyKind,
					})
				}
			default:
				if sh, err := buildShape3D(o, Collider3DComponentName, res, sx, sy, sz, cache); err == nil {
					out = append(out, PhysicsDebugShape3D{
						Shape:     sh.Transformed(pos, rot),
						IsTrigger: isTrig,
						BodyKind:  bodyKind,
					})
				}
			}
		}
	}
	if rawTrig, ok := o.Components[Trigger3DComponentName]; ok {
		if res, err := resolvePhysicsComponent(o.ID, Trigger3DComponentName, rawTrig); err == nil {
			if sh, err := buildShape3D(o, Trigger3DComponentName, res, sx, sy, sz, cache); err == nil {
				out = append(out, PhysicsDebugShape3D{
					Shape:     sh.Transformed(pos, rot),
					IsTrigger: true,
				})
			}
		}
	}
	return out
}
