package sceneformat

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/scene3d"
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
	// Components validates every object's components, resolves defaults, and invokes each registered ComponentType.Spawn factory on its spawned entity. When nil, SpawnEntitiesWith behaves like SpawnEntities and ignores object components.
	Components *ComponentRegistry
	// Persistence, when non-nil alongside ScenePath, applies persisted per-scene object state
	// (omitting destroyed objects and applying moved transforms and component field overrides)
	// to a copy of the SceneDoc before spawning entities.
	Persistence *save.Persistence
	// ScenePath identifies the scene inside Persistence (such as "scenes/cave.scene.yaml").
	ScenePath string
}

// resolvedSpawn pairs a registered component's Spawn factory with its resolved fields for one scene object.
type resolvedSpawn struct {
	name   string
	spawn  func(w *entities.World, e entities.Entity, fields map[string]any) error
	fields map[string]any
}

// SpawnEntities validates doc and spawns one entity per object into w, children included, linked into w's 2D transform hierarchy (entities.Hierarchy2D) the way the objects nest in the document. Each entity's local entities.Transform2D holds the object's X, Y, Rotation, ScaleX and ScaleY, and each carries an ObjectRef component. The top-level objects of a hidden layer are spawned hidden, so their subtrees are not world-visible. Entities are spawned in depth-first document order, each parent before its children, and the result maps every object ID to its entity.
//
// SpawnEntities returns an error and spawns nothing when doc is invalid, still instances or inherits other scene files (pass it through Resolve first), or w is nil or disposed. It is equivalent to SpawnEntitiesWith(doc, w, SpawnOptions{}).
func SpawnEntities(doc *SceneDoc, w *entities.World) (map[string]entities.Entity, error) {
	return SpawnEntitiesWith(doc, w, SpawnOptions{})
}

// SpawnEntitiesWith spawns doc's objects into w as SpawnEntities does, and when opts.Components is non-nil validates and resolves every object's components against opts.Components before the first entity is spawned, then runs each registered ComponentType.Spawn factory on its entity. Objects are spawned in depth-first document order (each parent before its children), and within each object its components run in ascending component-name order.
//
// If any ComponentType.Spawn returns an error, SpawnEntitiesWith destroys every entity it created so far (leaving pre-existing entities in w untouched) and returns the error naming the layer, object ID and component.
func SpawnEntitiesWith(doc *SceneDoc, w *entities.World, opts SpawnOptions) (map[string]entities.Entity, error) {
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
	if opts.Persistence != nil && opts.ScenePath != "" {
		doc = ApplyPersistence(doc, opts.Persistence, opts.ScenePath)
	}
	var plan map[string][]resolvedSpawn
	if opts.Components != nil {
		var err error
		plan, err = planComponentSpawns(doc, opts.Components)
		if err != nil {
			return nil, err
		}
	}
	h := w.Hierarchy2D()
	refs := entities.Component[ObjectRef](w)
	spawned := make(map[string]entities.Entity)
	var created []entities.Entity
	rollback := func() {
		for i := len(created) - 1; i >= 0; i-- {
			w.Destroy(created[i])
		}
	}
	var spawnErr error
	for li := range doc.Layers {
		l := &doc.Layers[li]
		hidden := !l.IsVisible()
		WalkObjects(l.Objects, func(o, parent *ObjectDoc) bool {
			e := w.Spawn()
			if e == entities.Zero {
				spawnErr = fmt.Errorf("sceneformat: layer %q object %q: %w", l.Name, o.ID, entities.ErrWorldDisposed)
				return false
			}
			spawned[o.ID] = e
			created = append(created, e)
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
				// Both entities were just spawned alive and the child is new, so SetParent cannot fail unless a ComponentType.Spawn destroyed the parent or disposed w.
				if err := h.SetParent(e, spawned[parent.ID], false); err != nil {
					spawnErr = fmt.Errorf("sceneformat: layer %q object %q: parent %q: %w", l.Name, o.ID, parent.ID, err)
					return false
				}
			}
			for _, task := range plan[o.ID] {
				if err := task.spawn(w, e, task.fields); err != nil {
					spawnErr = fmt.Errorf("sceneformat: layer %q object %q component %q: %w", l.Name, o.ID, task.name, err)
					return false
				}
			}
			return true
		})
		if spawnErr != nil {
			rollback()
			return nil, spawnErr
		}
	}
	return spawned, nil
}

// planComponentSpawns checks and resolves every component on every object of doc against reg in depth-first document order with component names sorted per object, returning the Spawn tasks to run once entities are created.
func planComponentSpawns(doc *SceneDoc, reg *ComponentRegistry) (map[string][]resolvedSpawn, error) {
	var plan map[string][]resolvedSpawn
	var firstErr error
	for li := range doc.Layers {
		WalkObjects(doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			if len(o.Components) == 0 {
				return true
			}
			for _, name := range slices.Sorted(maps.Keys(o.Components)) {
				raw := o.Components[name]
				if err := reg.CheckComponent(name, raw); err != nil {
					var ce *ComponentError
					if errors.As(err, &ce) {
						ce.Object = o.ID
					}
					firstErr = err
					return false
				}
				ct := reg.lookup(name)
				if ct == nil || ct.Spawn == nil {
					continue
				}
				data, _ := raw.(map[string]any)
				fields, err := reg.Resolve(name, data)
				if err != nil {
					var ce *ComponentError
					if errors.As(err, &ce) {
						ce.Object = o.ID
					}
					firstErr = err
					return false
				}
				if plan == nil {
					plan = make(map[string][]resolvedSpawn)
				}
				plan[o.ID] = append(plan[o.ID], resolvedSpawn{
					name:   name,
					spawn:  ct.Spawn,
					fields: fields,
				})
			}
			return true
		})
		if firstErr != nil {
			return nil, firstErr
		}
	}
	return plan, nil
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
