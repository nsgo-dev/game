package sceneformat

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
)

// errUnresolved refuses a scene that still refers to other scene files where only a self-contained one can be used.
var errUnresolved = errors.New("sceneformat: the scene instances or inherits other scene files; Resolve it first")

// ObjectRef is the component SpawnEntities attaches to every entity it spawns, naming the scene object the entity came from.
type ObjectRef struct {
	// ID is the object's unique scene ID.
	ID string
	// Layer is the name of the layer the object belongs to.
	Layer string
}

// SpawnOptions configures SpawnEntitiesWith.
type SpawnOptions struct {
	// Scenes, when non-nil and doc refers to other scene files, resolves doc with Resolve before spawning. When nil, SpawnEntitiesWith refuses an unresolved scene just as SpawnEntities does.
	Scenes SceneResolver
	// Events, when non-nil, validates the resolved scene's connections against it and binds them into SpawnedScene.Events before spawning any entities, so a connection error leaves w untouched.
	Events *EventRegistry
}

// SpawnedScene is the result of SpawnEntitiesWith: the resolved document, the spawned entity map, and the bound EventBus when SpawnOptions.Events was set.
type SpawnedScene struct {
	// Doc is the validated (and resolved) SceneDoc spawned into World.
	Doc *SceneDoc
	// World is the entities.World the entities were spawned into.
	World *entities.World
	// Entities maps every object ID in Doc, children included, to its spawned entity.
	Entities map[string]entities.Entity
	// Events is the EventBus wired from Doc.Connections when SpawnOptions.Events was set, or nil otherwise.
	Events *EventBus
}

// SpawnEntitiesWith spawns doc into w as SpawnEntities does, optionally resolving referenced scenes (SpawnOptions.Scenes) and binding declared event connections (SpawnOptions.Events). It validates doc and its connections (and checks that every connected handler has a non-nil Func) before spawning any entities, so a failure leaves w unchanged.
func SpawnEntitiesWith(doc *SceneDoc, w *entities.World, opts SpawnOptions) (*SpawnedScene, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	if doc.RefersToScenes() {
		if opts.Scenes == nil {
			return nil, errUnresolved
		}
		resolved, err := Resolve(doc, opts.Scenes)
		if err != nil {
			return nil, err
		}
		doc = resolved
	}
	if w == nil {
		return nil, errors.New("sceneformat: nil entity world")
	}
	if w.Disposed() {
		return nil, fmt.Errorf("sceneformat: spawn entities: %w", entities.ErrWorldDisposed)
	}
	if opts.Events != nil {
		if _, err := opts.Events.Bind(doc); err != nil {
			return nil, err
		}
	}
	spawned, err := SpawnEntities(doc, w)
	if err != nil {
		return nil, err
	}
	out := &SpawnedScene{
		Doc:      doc,
		World:    w,
		Entities: spawned,
	}
	if opts.Events != nil {
		bus, err := opts.Events.BindEntities(doc, w, spawned)
		if err != nil {
			return nil, err
		}
		out.Events = bus
	}
	return out, nil
}

// SpawnEntities validates doc and spawns one entity per object into w, children included, linked into w's 2D transform hierarchy (entities.Hierarchy2D) the way the objects nest in the document. Each entity's local entities.Transform2D holds the object's X, Y, Rotation, ScaleX and ScaleY, and each carries an ObjectRef component. The top-level objects of a hidden layer are spawned hidden, so their subtrees are not world-visible. Entities are spawned in depth-first document order, each parent before its children, and the result maps every object ID to its entity.
//
// SpawnEntities returns an error and spawns nothing when doc is invalid, still instances or inherits other scene files (pass it through Resolve first), or w is nil or disposed.
func SpawnEntities(doc *SceneDoc, w *entities.World) (map[string]entities.Entity, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	if doc.RefersToScenes() {
		return nil, errUnresolved
	}
	if w == nil {
		return nil, errors.New("sceneformat: nil entity world")
	}
	if w.Disposed() {
		return nil, fmt.Errorf("sceneformat: spawn entities: %w", entities.ErrWorldDisposed)
	}
	h := w.Hierarchy2D()
	refs := entities.Component[ObjectRef](w)
	spawned := make(map[string]entities.Entity)
	var linkErr error
	for li := range doc.Layers {
		l := &doc.Layers[li]
		hidden := !l.IsVisible()
		WalkObjects(l.Objects, func(o, parent *ObjectDoc) bool {
			e := w.Spawn()
			spawned[o.ID] = e
			h.SetLocal(e, entities.Transform2D{
				X:        o.X,
				Y:        o.Y,
				Rotation: o.Rotation,
				ScaleX:   o.EffectiveScaleX(),
				ScaleY:   o.EffectiveScaleY(),
				Hidden:   hidden && parent == nil,
			})
			refs.Set(e, ObjectRef{ID: o.ID, Layer: l.Name})
			if parent != nil {
				// Both entities were just spawned alive and the child is new, so SetParent cannot fail with a cycle or a dead entity; it is checked anyway so a change in entities cannot pass silently.
				if err := h.SetParent(e, spawned[parent.ID], false); err != nil {
					linkErr = fmt.Errorf("sceneformat: layer %q object %q: parent %q: %w", l.Name, o.ID, parent.ID, err)
					return false
				}
			}
			return true
		})
		if linkErr != nil {
			return nil, linkErr
		}
	}
	return spawned, nil
}

// Nodes3D is the 3D node tree BuildNodes3D builds from a scene.
type Nodes3D struct {
	// Layers maps each layer name to its group node, a child of the parent given to BuildNodes3D with an identity transform and the layer's visibility.
	Layers map[string]*scene3d.Node
	// Objects maps each object ID, children included, to its node.
	Objects map[string]*scene3d.Node
}

// BuildNodes3D validates doc and builds its layers and objects into a 3D node tree under parent, such as a scene3d.Scene's Root. Each layer becomes a group node named after the layer, in document order, and each object a node named by its ID under its layer's node or its parent object's node, children in document order. An object's local transform is its ObjectLocalTransform3D, and its node's UserData is the *ObjectDoc it came from (a pointer into doc).
//
// BuildNodes3D returns an error and leaves parent unchanged when doc is invalid, still instances or inherits other scene files (pass it through Resolve first), or parent is nil.
func BuildNodes3D(doc *SceneDoc, parent *scene3d.Node) (*Nodes3D, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	if doc.RefersToScenes() {
		return nil, errUnresolved
	}
	if parent == nil {
		return nil, scene3d.ErrNilNode
	}
	out := &Nodes3D{
		Layers:  make(map[string]*scene3d.Node, len(doc.Layers)),
		Objects: make(map[string]*scene3d.Node),
	}
	layerNodes := make([]*scene3d.Node, 0, len(doc.Layers))
	for li := range doc.Layers {
		l := &doc.Layers[li]
		ln := scene3d.NewNode(l.Name)
		ln.Visible = l.IsVisible()
		out.Layers[l.Name] = ln
		layerNodes = append(layerNodes, ln)
		var linkErr error
		WalkObjects(l.Objects, func(o, po *ObjectDoc) bool {
			n := scene3d.NewNode(o.ID)
			n.Local = ObjectLocalTransform3D(o)
			n.UserData = o
			out.Objects[o.ID] = n
			under := ln
			if po != nil {
				under = out.Objects[po.ID]
			}
			if err := under.AddChild(n); err != nil {
				linkErr = fmt.Errorf("sceneformat: layer %q object %q: %w", l.Name, o.ID, err)
				return false
			}
			return true
		})
		if linkErr != nil {
			return nil, linkErr
		}
	}
	// Attach the finished layer subtrees last so a failure above leaves parent untouched.
	for _, ln := range layerNodes {
		if err := parent.AddChild(ln); err != nil {
			for _, attached := range layerNodes {
				if attached == ln {
					break
				}
				parent.RemoveChild(attached)
			}
			return nil, fmt.Errorf("sceneformat: layer %q: %w", ln.Name, err)
		}
	}
	return out, nil
}

// ObjectLocalTransform3D returns o's 3D transform relative to its parent (or to its layer for a top-level object): position (X, Y, PosZ), rotation RotX then RotY then RotZ about fixed axes (the quaternion Rz * Ry * Rx, matching the editor's 3D viewport), and scale (ScaleX, ScaleY, ScaleZ).
func ObjectLocalTransform3D(o *ObjectDoc) math3d.Transform {
	if o == nil {
		return math3d.TransformIdentity()
	}
	rot := math3d.QuatIdentity()
	if o.RotX != 0 || o.RotY != 0 || o.RotZ != 0 {
		rx := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, o.RotX)
		ry := math3d.QuatFromAxisAngle(math3d.Vec3{Y: 1}, o.RotY)
		rz := math3d.QuatFromAxisAngle(math3d.Vec3{Z: 1}, o.RotZ)
		rot = rz.Mul(ry).Mul(rx)
	}
	return math3d.Transform{
		Pos:   math3d.Vec3{X: o.X, Y: o.Y, Z: o.PosZ},
		Rot:   rot,
		Scale: math3d.Vec3{X: o.EffectiveScaleX(), Y: o.EffectiveScaleY(), Z: o.EffectiveScaleZ()},
	}
}
