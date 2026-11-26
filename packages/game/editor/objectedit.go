package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// AddComponent adds the component named component to objectID through History. With a component registry set (SetComponentRegistry), component must be a registered type and is added holding every registered field at its default, so the scene file shows what the component does; it is refused when the added component would declare an event or handler that one of the scene's connections names bare and another of the object's components declares too, since the connection would no longer say which one it means. Without a registry the component is added with no fields. It is refused for an empty name, one with leading or trailing whitespace or a '.', which connections use to qualify an event or handler with its component, and for a component the object already has.
func (e *Editor) AddComponent(objectID, component string) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(objectID); err != nil {
		return err
	}
	if err := checkName("component name", component); err != nil {
		return err
	}
	// A connection names a component's event or handler as "component.name", so a dot in the name would make that ambiguous.
	if strings.Contains(component, ".") {
		return fmt.Errorf("editor: component name %q must not contain '.'", component)
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			resolved, _ := e.FindResolvedObject(objectID)
			if resolved == nil {
				return fmt.Errorf("editor: object %q not found", objectID)
			}
			if _, has := resolved.Components[component]; has {
				return fmt.Errorf("editor: object %q already has component %q", objectID, component)
			}
			value := map[string]any{}
			if e.components != nil {
				t, ok := e.components.Lookup(component)
				if !ok {
					err := e.components.CheckComponent(component, value)
					var ce *sceneformat.ComponentError
					if errors.As(err, &ce) {
						ce.Object = objectID
					}
					return err
				}
				for _, f := range t.Fields {
					value[f.Name] = f.Default
				}
			}
			return e.editObjectOverride(objectID, "Add component "+component, 2, func(ov *sceneformat.ObjectOverride, _, _ *sceneformat.ObjectDoc) error {
				if ov.Components == nil {
					ov.Components = make(map[string]any)
				}
				ov.Components[component] = cloneMap(value)
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if _, has := obj.Components[component]; has {
		return fmt.Errorf("editor: object %q already has component %q", objectID, component)
	}
	value := map[string]any{}
	if e.components != nil {
		t, ok := e.components.Lookup(component)
		if !ok {
			// CheckComponent words the refusal as file loading does, listing the registered types.
			err := e.components.CheckComponent(component, value)
			var ce *sceneformat.ComponentError
			if errors.As(err, &ce) {
				ce.Object = objectID
			}
			return err
		}
		for _, f := range t.Fields {
			value[f.Name] = f.Default
		}
		if err := e.checkConnectionsWith(obj, component, value); err != nil {
			return err
		}
	}
	e.History.Execute(&addComponentCmd{
		ed:          e,
		objectID:    objectID,
		component:   component,
		value:       value,
		wasNilComps: obj.Components == nil,
	})
	return nil
}

// checkName refuses an empty or blank name, and one with leading or trailing whitespace, which would sit beside the trimmed name as a separate key; what says what the name is, such as "property key".
func checkName(what, name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("editor: %s must not be empty", what)
	}
	if trimmed != name {
		return fmt.Errorf("editor: %s %q must not start or end with whitespace", what, name)
	}
	return nil
}

// checkConnectionsWith reports whether the scene's connections still bind against the registry once obj also holds component with value. A scene that instances or inherits others binds its connections only once resolved, the connections of the scenes it refers to included, so it is resolved through the editor's resolver and checked as resolved; it is not checked when the resolver cannot load scene files, or when the scene does not resolve or bind even without the component, a fault the editor reports elsewhere (CheckProblems, Play) and that adding a component neither causes nor fixes.
func (e *Editor) checkConnectionsWith(obj *sceneformat.ObjectDoc, component string, value map[string]any) error {
	resolved := e.Doc.RefersToScenes()
	var scenes sceneformat.SceneResolver
	if resolved {
		var ok bool
		if scenes, ok = e.Resolver.(sceneformat.SceneResolver); !ok {
			return nil
		}
	}
	bind := func() error {
		if !resolved {
			return sceneformat.ValidateConnections(e.Doc, e.components)
		}
		doc, err := sceneformat.Resolve(e.Doc, scenes)
		if err != nil {
			return err
		}
		return sceneformat.ValidateConnections(doc, e.components)
	}
	if !resolved && len(e.Doc.Connections) == 0 {
		return nil
	}
	if resolved && bind() != nil {
		return nil
	}
	if obj.Components == nil {
		obj.Components = map[string]any{component: value}
		defer func() { obj.Components = nil }()
	} else {
		obj.Components[component] = value
		defer delete(obj.Components, component)
	}
	if err := bind(); err != nil {
		return fmt.Errorf("editor: adding component %q to %q would break a connection: %w", component, obj.ID, err)
	}
	return nil
}

// RemoveComponent removes the component named component from objectID through History, together with every connection of the scene that needs it: one from objectID whose event the component declares, or one to objectID whose handler it declares, named either bare ("pressed") or with the component ("button.pressed"). A bare name is attributed to the component through its registered type, so without a component registry, or for a component the registry does not know, only connections that name the component ("button.pressed") are removed. Undo puts the component and those connections back where they were. Removing a component the object does not have is refused.
func (e *Editor) RemoveComponent(objectID, component string) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(objectID); err != nil {
		return err
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Remove component "+component, 2, func(ov *sceneformat.ObjectOverride, resolved, base *sceneformat.ObjectDoc) error {
				if _, has := resolved.Components[component]; !has {
					return fmt.Errorf("editor: object %q has no component %q", objectID, component)
				}
				if base != nil && base.Components != nil {
					if _, onBase := base.Components[component]; onBase {
						return fmt.Errorf("editor: cannot remove inherited component %q on %q; revert its overrides or edit the source scene", component, objectID)
					}
				}
				if ov.Components != nil {
					delete(ov.Components, component)
				}
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	old, has := obj.Components[component]
	if !has {
		return fmt.Errorf("editor: object %q has no component %q", objectID, component)
	}
	e.History.Execute(&removeComponentCmd{
		ed:        e,
		objectID:  objectID,
		component: component,
		oldVal:    cloneAny(old),
	})
	return nil
}

// ComponentConnections returns the indexes, in ascending order, of the scene's connections that need objectID's component: those RemoveComponent removes with it. A connection that names an event or handler bare is listed only when the component's registered type declares it.
func (e *Editor) ComponentConnections(objectID, component string) []int {
	var t *sceneformat.ComponentType
	if ct, ok := e.components.Lookup(component); ok {
		t = &ct
	}
	var out []int
	for i, c := range e.Doc.Connections {
		if (c.From == objectID && namesMember(c.Event, component, t, false)) || (c.To == objectID && namesMember(c.Handler, component, t, true)) {
			out = append(out, i)
		}
	}
	return out
}

// namesMember reports whether name, a connection's event (or, when handler is true, its handler) written bare or as "component.name", resolves to component, whose registered type is t or nil when it is not registered. In a scene whose connections bind, a bare name resolves to component exactly when its type declares it, since no other component of the object may declare it too.
func namesMember(name, component string, t *sceneformat.ComponentType, handler bool) bool {
	if comp, _, qualified := strings.Cut(name, "."); qualified {
		return comp == component
	}
	if t == nil {
		return false
	}
	if handler {
		_, ok := t.Handler(name)
		return ok
	}
	_, ok := t.Event(name)
	return ok
}

// AddProperty adds the custom property key to objectID through History, holding the zero value of kind: false, 0, 0.0 or "". It is refused for an empty or blank key, one with leading or trailing whitespace, a key the object already has and an unknown kind.
func (e *Editor) AddProperty(objectID, key string, kind sceneformat.FieldKind) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := checkName("property key", key); err != nil {
		return err
	}
	var value any
	switch kind {
	case sceneformat.FieldBool:
		value = false
	case sceneformat.FieldInt:
		value = 0
	case sceneformat.FieldFloat:
		value = 0.0
	case sceneformat.FieldString:
		value = ""
	default:
		return fmt.Errorf("editor: unknown property kind %v", kind)
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Add property "+key, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if _, has := resolved.Properties[key]; has {
					return fmt.Errorf("editor: object %q already has property %q", objectID, key)
				}
				if ov.Properties == nil {
					ov.Properties = make(map[string]any)
				}
				ov.Properties[key] = value
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if _, has := obj.Properties[key]; has {
		return fmt.Errorf("editor: object %q already has property %q", objectID, key)
	}
	e.History.Execute(&setPropertyCmd{
		ed:        e,
		objectID:  objectID,
		key:       key,
		label:     "Add property " + key,
		status:    "Added property " + key,
		wasNilMap: obj.Properties == nil,
		newVal:    value,
	})
	return nil
}

// SetObjectSprite binds objectID to the atlas frame ref names, or with a nil ref unbinds it, through History. ref needs both an atlas and a frame; with a Resolver set the atlas must resolve and hold the frame, as the asset browser lists them (Assets).
func (e *Editor) SetObjectSprite(objectID string, ref *sceneformat.SpriteRef) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(objectID); err != nil {
		return err
	}
	if ref != nil {
		if ref.Atlas == "" || ref.Frame == "" {
			return errors.New("editor: object sprite requires both atlas and frame")
		}
		if e.Resolver != nil {
			atlas, err := e.Resolver.Atlas(ref.Atlas)
			if err != nil {
				return fmt.Errorf("editor: resolve atlas %q: %w", ref.Atlas, err)
			}
			if atlas == nil {
				return fmt.Errorf("editor: atlas %q is nil", ref.Atlas)
			}
			if _, ok := atlas.Frames[ref.Frame]; !ok {
				return fmt.Errorf("editor: atlas %q has no frame %q", ref.Atlas, ref.Frame)
			}
		}
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Set sprite of "+objectID, 2, func(ov *sceneformat.ObjectOverride, resolved, base *sceneformat.ObjectDoc) error {
				if spriteEqual(resolved.Sprite, ref) {
					return nil
				}
				if ref == nil {
					if base != nil && base.Sprite != nil {
						return fmt.Errorf("editor: cannot clear inherited sprite on %q; revert override or edit the source scene", objectID)
					}
					ov.Sprite = nil
					return nil
				}
				ov.Sprite = cloneSprite(ref)
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if spriteEqual(obj.Sprite, ref) {
		return nil
	}
	e.History.Execute(&objectFieldCmd{
		ed:    e,
		id:    objectID,
		label: "Set sprite of " + objectID,
		old:   cloneSprite(obj.Sprite),
		new:   cloneSprite(ref),
		apply: func(o *sceneformat.ObjectDoc, v any) { o.Sprite = cloneSprite(v.(*sceneformat.SpriteRef)) },
	})
	return nil
}

// SetObjectOrigin sets objectID's pivot inside its sprite's untrimmed frame (originX and originY) through History. Both must be finite.
func (e *Editor) SetObjectOrigin(objectID string, x, y float32) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(objectID); err != nil {
		return err
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return errors.New("editor: object origin must be finite")
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Set origin of "+objectID, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if x != resolved.OriginX {
					ov.OriginX = sceneformat.Float32(x)
				}
				if y != resolved.OriginY {
					ov.OriginY = sceneformat.Float32(y)
				}
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if obj.OriginX == x && obj.OriginY == y {
		return nil
	}
	e.History.Execute(&objectFieldCmd{
		ed:    e,
		id:    objectID,
		label: "Set origin of " + objectID,
		old:   [2]float32{obj.OriginX, obj.OriginY},
		new:   [2]float32{x, y},
		apply: func(o *sceneformat.ObjectDoc, v any) {
			p := v.([2]float32)
			o.OriginX, o.OriginY = p[0], p[1]
		},
	})
	return nil
}

// SetObjectFlip sets whether objectID's sprite is mirrored horizontally (flipX) and vertically (flipY) inside its frame, through History.
func (e *Editor) SetObjectFlip(objectID string, flipX, flipY bool) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(objectID); err != nil {
		return err
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Flip "+objectID, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if flipX != resolved.FlipX {
					ov.FlipX = sceneformat.Bool(flipX)
				}
				if flipY != resolved.FlipY {
					ov.FlipY = sceneformat.Bool(flipY)
				}
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if obj.FlipX == flipX && obj.FlipY == flipY {
		return nil
	}
	e.History.Execute(&objectFieldCmd{
		ed:    e,
		id:    objectID,
		label: "Flip " + objectID,
		old:   [2]bool{obj.FlipX, obj.FlipY},
		new:   [2]bool{flipX, flipY},
		apply: func(o *sceneformat.ObjectDoc, v any) {
			f := v.([2]bool)
			o.FlipX, o.FlipY = f[0], f[1]
		},
	})
	return nil
}

// SetObjectMaterial names the material resource file objectID's sprite draws with, by its slash-separated path from the project root such as "materials/stone.material.yaml", or with an empty path names none, through History. A named material must load through the editor's Resolver, which must therefore implement sceneformat.MaterialResolver, as sceneformat.Build needs to draw the scene.
func (e *Editor) SetObjectMaterial(objectID, material string) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if err := e.checkObjectNotPausedByPlan(objectID); err != nil {
		return err
	}
	if material != "" && (!fs.ValidPath(material) || material == ".") {
		return fmt.Errorf("editor: material %q must be a slash-separated path relative to the project root", material)
	}
	if material != "" {
		mr, ok := e.Resolver.(sceneformat.MaterialResolver)
		if !ok {
			return errNoMaterialResolver
		}
		m, err := mr.Material(material)
		if err != nil {
			return fmt.Errorf("editor: load material %q: %w", material, err)
		}
		if m == nil {
			return fmt.Errorf("editor: material %q is nil", material)
		}
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Set material of "+objectID, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Material == material {
					return nil
				}
				ov.Material = sceneformat.String(material)
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if obj.Material == material {
		return nil
	}
	e.History.Execute(&objectFieldCmd{
		ed:    e,
		id:    objectID,
		label: "Set material of " + objectID,
		old:   obj.Material,
		new:   material,
		apply: func(o *sceneformat.ObjectDoc, v any) { o.Material = v.(string) },
	})
	return nil
}

var errNoMaterialResolver = errors.New("editor: the resolver cannot load material files (it does not implement sceneformat.MaterialResolver)")

// canLoadMaterials reports whether the editor's Resolver implements sceneformat.MaterialResolver, which SetObjectMaterial needs to name a material and sceneformat.Build needs to draw it. The inspector's Material field offers the project's material files only when it does.
func (e *Editor) canLoadMaterials() bool {
	_, ok := e.Resolver.(sceneformat.MaterialResolver)
	return ok
}

// materialSuffix ends the name of every material resource file.
const materialSuffix = ".material.yaml"

// projectMaterials returns the paths of the open project's material files in the order ProjectFiles lists them, or nil without a project.
func (e *Editor) projectMaterials() []string {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if !f.Dir && strings.HasSuffix(f.Path, materialSuffix) {
			out = append(out, f.Path)
		}
	}
	return out
}

// addableComponents returns the registered component types objectID lacks whose names hold query, ignoring case, in ascending order.
func (e *Editor) addableComponents(objectID, query string) []string {
	obj, _ := e.FindResolvedObject(objectID)
	if obj == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var out []string
	for _, n := range e.components.Names() {
		if _, has := obj.Components[n]; has {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(n), q) {
			out = append(out, n)
		}
	}
	return out
}

func spriteEqual(a, b *sceneformat.SpriteRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func cloneSprite(s *sceneformat.SpriteRef) *sceneformat.SpriteRef {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// objectFieldCmd sets one field, or a pair of fields, of an object: apply writes new on Do and old on Undo. The values are immutable copies, and apply copies them again where they are pointers.
type objectFieldCmd struct {
	ed       *Editor
	id       string
	label    string
	old, new any
	apply    func(o *sceneformat.ObjectDoc, v any)
}

func (c *objectFieldCmd) Label() string { return c.label }

func (c *objectFieldCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		c.apply(obj, c.new)
		c.ed.Status = c.label
	}
}

func (c *objectFieldCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		c.apply(obj, c.old)
	}
}

type addComponentCmd struct {
	ed          *Editor
	objectID    string
	component   string
	value       map[string]any
	wasNilComps bool
}

func (c *addComponentCmd) Label() string { return "Add component " + c.component }

func (c *addComponentCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	obj.Components[c.component] = cloneMap(c.value)
	c.ed.Status = "Added component " + c.component
}

func (c *addComponentCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil || obj.Components == nil {
		return
	}
	delete(obj.Components, c.component)
	if c.wasNilComps && len(obj.Components) == 0 {
		obj.Components = nil
	}
}

type removeComponentCmd struct {
	ed        *Editor
	objectID  string
	component string
	oldVal    any
	// conns are the connections Do removed with the component, each with its index before the removal, in ascending index order.
	conns []removedConnection
}

func (c *removeComponentCmd) Label() string { return "Remove component " + c.component }

func (c *removeComponentCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	// The connections are found before the component goes, while a bare name still resolves to it.
	idx := c.ed.ComponentConnections(c.objectID, c.component)
	c.conns = c.conns[:0]
	for _, i := range idx {
		c.conns = append(c.conns, removedConnection{index: i, conn: c.ed.Doc.Connections[i]})
	}
	if len(idx) > 0 {
		kept := make([]sceneformat.ConnectionDoc, 0, len(c.ed.Doc.Connections)-len(idx))
		for i, conn := range c.ed.Doc.Connections {
			if !slices.Contains(idx, i) {
				kept = append(kept, conn)
			}
		}
		if len(kept) == 0 {
			kept = nil
		}
		c.ed.Doc.Connections = kept
	}
	delete(obj.Components, c.component)
	if len(obj.Components) == 0 {
		obj.Components = nil
	}
	c.ed.Status = "Removed component " + c.component
	if n := len(c.conns); n == 1 {
		c.ed.Status += " and 1 connection"
	} else if n > 1 {
		c.ed.Status += fmt.Sprintf(" and %d connections", n)
	}
}

func (c *removeComponentCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	obj.Components[c.component] = cloneAny(c.oldVal)
	for _, rc := range c.conns {
		at := min(rc.index, len(c.ed.Doc.Connections))
		c.ed.Doc.Connections = slices.Insert(c.ed.Doc.Connections, at, rc.conn)
	}
}
