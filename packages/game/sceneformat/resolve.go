package sceneformat

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
)

// SceneResolver loads the scene files that a scene instances (ObjectDoc.Scene) or inherits (SceneDoc.Inherits). An AssetResolver passed to Build may implement it, and FSScenes reads scene files from a file system.
type SceneResolver interface {
	// Scene returns the scene document at path, a slash-separated path from the project root. Resolve validates what it returns and never modifies it, so a resolver may return the same document for every request of one path.
	Scene(path string) (*SceneDoc, error)
}

// FSScenes is a SceneResolver that reads scene files from FS and parses them with Parse.
type FSScenes struct {
	// FS holds the project's files, with paths relative to the project root.
	FS fs.FS
}

// Scene reads the file at path from s.FS and parses it.
func (s FSScenes) Scene(path string) (*SceneDoc, error) {
	if s.FS == nil {
		return nil, errors.New("sceneformat: FSScenes has a nil file system")
	}
	data, err := fs.ReadFile(s.FS, path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// MaxSceneNesting is how many scene files deep Resolve follows instancing and inheritance: a scene that instances or inherits another is one level, and a scene that one instances in turn is two. Resolve refuses a deeper chain.
const MaxSceneNesting = 64

// MaxResolvedObjects is the most objects Resolve creates for one scene, counting each copy of an instanced or inherited object. It stops scenes that instance each other many times over from growing without bound.
const MaxResolvedObjects = 1 << 17

// RefersToScenes reports whether doc instances or inherits another scene file, so that Resolve must load them before doc can be spawned (SpawnEntities) or built into 3D nodes (BuildNodes3D). Build resolves such a scene itself.
func (doc *SceneDoc) RefersToScenes() bool {
	if doc == nil {
		return false
	}
	if doc.Inherits != "" {
		return true
	}
	for li := range doc.Layers {
		found := false
		WalkObjects(doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			found = o.Scene != ""
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// Resolve validates doc and returns a copy of it with every scene it instances or inherits loaded through scenes and merged in, so that the copy refers to no other scene file. An instance object gets the instanced scene's objects, with its overrides applied, as its first children, their IDs prefixed by its own ID and a slash ("enemy_1/body"). A scene that inherits another becomes a copy of its base with its own fields, overrides, layer overrides and layers applied, as SceneDoc.Inherits describes. Connections come along: an instanced scene's connections, with the objects they name prefixed like its objects' IDs, in the order the instances appear, then the scene's own; an inheriting scene keeps its base's connections and adds its own after them. Scenes referred to in turn are resolved the same way, each file loaded once per call.
//
// Resolve refuses a cycle of scenes that instance or inherit each other, a chain deeper than MaxSceneNesting files, a result of more than MaxResolvedObjects objects, a scene file that fails to load or validate, an instanced scene with a tilemap, an override that names no object or layer of the scene it changes, a layer that extends a base layer and sets more than its objects, and a result that does not validate, such as one whose instanced IDs collide with others. It never modifies doc or the documents scenes returns, and scenes may be nil when doc refers to no other scene. The result has doc's version.
func Resolve(doc *SceneDoc, scenes SceneResolver) (*SceneDoc, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	r := &sceneResolver{scenes: scenes, done: make(map[string]*SceneDoc)}
	return r.resolve(doc, "")
}

// sceneResolver carries one Resolve call's state: the files resolved so far, the chain of files being resolved, and the objects created.
type sceneResolver struct {
	scenes  SceneResolver
	done    map[string]*SceneDoc
	stack   []string
	objects int
}

// file returns the resolved scene at path, loading and resolving it on first use. The result is shared by every later use and must be cloned before it is changed.
func (r *sceneResolver) file(path string) (*SceneDoc, error) {
	if d, ok := r.done[path]; ok {
		return d, nil
	}
	if i := slices.Index(r.stack, path); i >= 0 {
		chain := append(slices.Clone(r.stack[i:]), path)
		return nil, fmt.Errorf("sceneformat: scene cycle %s", strings.Join(quoteAll(chain), " -> "))
	}
	if len(r.stack) >= MaxSceneNesting {
		return nil, fmt.Errorf("sceneformat: scene %q nests scene files deeper than %d levels", path, MaxSceneNesting)
	}
	if r.scenes == nil {
		return nil, fmt.Errorf("sceneformat: scene %q requires a SceneResolver", path)
	}
	raw, err := r.scenes.Scene(path)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: load scene %q: %w", path, err)
	}
	if raw == nil {
		return nil, fmt.Errorf("sceneformat: resolver returned nil scene for %q", path)
	}
	if err := Validate(raw); err != nil {
		return nil, fmt.Errorf("sceneformat: scene %q: %w", path, err)
	}
	r.stack = append(r.stack, path)
	res, err := r.resolve(raw, path)
	r.stack = r.stack[:len(r.stack)-1]
	if err != nil {
		return nil, err
	}
	r.done[path] = res
	return res, nil
}

// resolve returns the self-contained copy of the validated doc; path names doc's file in errors, or is empty for the scene passed to Resolve.
func (r *sceneResolver) resolve(doc *SceneDoc, path string) (*SceneDoc, error) {
	own := &SceneDoc{
		Version:         doc.Version,
		Name:            doc.Name,
		NavMesh:         doc.NavMesh,
		VirtualWidth:    doc.VirtualWidth,
		VirtualHeight:   doc.VirtualHeight,
		BackgroundColor: doc.BackgroundColor,
		Camera:          cloneCamera(doc.Camera),
		Tilemaps:        cloneTilemaps(doc.Tilemaps),
	}
	if doc.Layers != nil {
		own.Layers = make([]LayerDoc, len(doc.Layers))
	}
	var conns []ConnectionDoc
	for i := range doc.Layers {
		l := &doc.Layers[i]
		own.Layers[i] = cloneLayerFields(l)
		objs, err := r.expandObjects(l.Objects, &conns)
		if err != nil {
			return nil, err
		}
		own.Layers[i].Objects = objs
	}
	own.Connections = append(conns, cloneConnections(doc.Connections)...)

	out := own
	if doc.Inherits != "" {
		var err error
		if out, err = r.inherit(doc, own); err != nil {
			return nil, err
		}
	}
	if err := Validate(out); err != nil {
		if path == "" {
			return nil, fmt.Errorf("sceneformat: resolved scene: %w", err)
		}
		return nil, fmt.Errorf("sceneformat: scene %q resolves to an invalid scene: %w", path, err)
	}
	return out, nil
}

// inherit merges doc, whose own layers are already expanded into own, onto a copy of its resolved base scene.
func (r *sceneResolver) inherit(doc, own *SceneDoc) (*SceneDoc, error) {
	base, err := r.file(doc.Inherits)
	if err != nil {
		return nil, err
	}
	n := 0
	for li := range base.Layers {
		n += countObjects(base.Layers[li].Objects)
	}
	if err := r.spend(n); err != nil {
		return nil, err
	}
	out := base.Clone()
	out.Version = doc.Version
	out.UID = doc.UID
	if own.Name != "" {
		out.Name = own.Name
	}
	if own.NavMesh != "" {
		out.NavMesh = own.NavMesh
	}
	if own.VirtualWidth != 0 || own.VirtualHeight != 0 {
		out.VirtualWidth, out.VirtualHeight = own.VirtualWidth, own.VirtualHeight
	}
	if own.BackgroundColor != "" {
		out.BackgroundColor = own.BackgroundColor
	}
	if own.Camera != nil {
		out.Camera = own.Camera
	}
	if len(own.Tilemaps) > 0 {
		if out.Tilemaps == nil {
			out.Tilemaps = make(map[string]*TilemapDoc, len(own.Tilemaps))
		}
		maps.Copy(out.Tilemaps, own.Tilemaps)
	}

	for _, name := range slices.Sorted(maps.Keys(doc.LayerOverrides)) {
		li := layerIndex(out.Layers, name)
		if li < 0 {
			return nil, fmt.Errorf("sceneformat: layerOverrides names layer %q, which base scene %q does not hold", name, doc.Inherits)
		}
		lo := doc.LayerOverrides[name]
		lo.applyTo(&out.Layers[li])
	}
	if len(doc.Overrides) > 0 {
		byID := make(map[string]*ObjectDoc)
		for li := range out.Layers {
			indexObjects(out.Layers[li].Objects, byID)
		}
		for _, id := range slices.Sorted(maps.Keys(doc.Overrides)) {
			o, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("sceneformat: overrides names object %q, which base scene %q does not hold", id, doc.Inherits)
			}
			ov := doc.Overrides[id]
			ov.applyTo(o)
		}
	}

	baseLayers := len(out.Layers)
	for i := range own.Layers {
		l := &own.Layers[i]
		li := layerIndex(out.Layers[:baseLayers], l.Name)
		if li < 0 {
			out.Layers = append(out.Layers, *l)
			continue
		}
		if l.Space != "" || l.Order != 0 || l.Visible != nil || l.Tilemap != nil {
			return nil, fmt.Errorf("sceneformat: layer %q extends the layer of base scene %q and may only add objects; change the layer under layerOverrides", l.Name, doc.Inherits)
		}
		out.Layers[li].Objects = append(out.Layers[li].Objects, l.Objects...)
	}
	out.Connections = append(out.Connections, own.Connections...)
	return out, nil
}

// expandObjects returns copies of objs with every instance expanded, or nil for no objects, and appends the connections of the scenes they instance to conns.
func (r *sceneResolver) expandObjects(objs []ObjectDoc, conns *[]ConnectionDoc) ([]ObjectDoc, error) {
	if objs == nil {
		return nil, nil
	}
	out := make([]ObjectDoc, len(objs))
	for i := range objs {
		o, err := r.expandObject(&objs[i], conns)
		if err != nil {
			return nil, err
		}
		out[i] = o
	}
	return out, nil
}

// expandObject returns a copy of o that instances nothing: the objects of o's instanced scene, overridden and with prefixed IDs, followed by o's own children expanded in turn. It appends the instanced scene's connections, with the same prefix on the objects they name, to conns, followed by those of scenes o's children instance.
func (r *sceneResolver) expandObject(o *ObjectDoc, conns *[]ConnectionDoc) (ObjectDoc, error) {
	if err := r.spend(1); err != nil {
		return ObjectDoc{}, err
	}
	out := cloneObjectFields(o)
	out.Scene = ""
	out.Overrides = nil

	var kids []ObjectDoc
	if o.Scene != "" {
		inst, err := r.file(o.Scene)
		if err != nil {
			return ObjectDoc{}, err
		}
		n := 0
		for li := range inst.Layers {
			l := &inst.Layers[li]
			if l.Tilemap != nil {
				return ObjectDoc{}, fmt.Errorf("sceneformat: object %q instances scene %q, whose layer %q holds a tilemap", o.ID, o.Scene, l.Name)
			}
			n += countObjects(l.Objects)
		}
		if err := r.spend(n); err != nil {
			return ObjectDoc{}, err
		}
		for li := range inst.Layers {
			for j := range inst.Layers[li].Objects {
				kids = append(kids, inst.Layers[li].Objects[j].Clone())
			}
		}
		if len(o.Overrides) > 0 {
			byID := make(map[string]*ObjectDoc, n)
			indexObjects(kids, byID)
			for _, id := range slices.Sorted(maps.Keys(o.Overrides)) {
				t, ok := byID[id]
				if !ok {
					return ObjectDoc{}, fmt.Errorf("sceneformat: object %q overrides %q, which scene %q does not hold", o.ID, id, o.Scene)
				}
				ov := o.Overrides[id]
				ov.applyTo(t)
			}
		}
		WalkObjects(kids, func(c, _ *ObjectDoc) bool {
			c.ID = o.ID + "/" + c.ID
			return true
		})
		for _, c := range cloneConnections(inst.Connections) {
			c.From = o.ID + "/" + c.From
			c.To = o.ID + "/" + c.To
			*conns = append(*conns, c)
		}
	}
	for i := range o.Children {
		c, err := r.expandObject(&o.Children[i], conns)
		if err != nil {
			return ObjectDoc{}, err
		}
		kids = append(kids, c)
	}
	out.Children = kids
	return out, nil
}

// spend counts n more created objects against MaxResolvedObjects.
func (r *sceneResolver) spend(n int) error {
	r.objects += n
	if r.objects > MaxResolvedObjects {
		return fmt.Errorf("sceneformat: resolving the scene creates more than %d objects", MaxResolvedObjects)
	}
	return nil
}

// validateSceneRefs checks the scene-level fields version 2 added.
func validateSceneRefs(doc *SceneDoc) error {
	if doc.Version < 2 {
		field := ""
		switch {
		case doc.Inherits != "":
			field = "inherits"
		case len(doc.LayerOverrides) > 0:
			field = "layerOverrides"
		case len(doc.Overrides) > 0:
			field = "overrides"
		}
		if field != "" {
			return fieldErr([]string{field}, "sceneformat: inherits, overrides and layerOverrides need version 2, not %d", doc.Version)
		}
		return nil
	}
	if doc.Inherits != "" && !validFilePath(doc.Inherits) {
		return fieldErr([]string{"inherits"}, "sceneformat: inherits %q must be a slash-separated path relative to the project root", doc.Inherits)
	}
	if doc.Inherits == "" && (len(doc.Overrides) > 0 || len(doc.LayerOverrides) > 0) {
		field := "overrides"
		if len(doc.LayerOverrides) > 0 {
			field = "layerOverrides"
		}
		return fieldErr([]string{field}, "sceneformat: overrides and layerOverrides require inherits")
	}
	for _, name := range slices.Sorted(maps.Keys(doc.LayerOverrides)) {
		if name == "" {
			return fieldErr([]string{"layerOverrides"}, "sceneformat: layerOverrides contains an empty layer name")
		}
		lp := []string{"layerOverrides", name}
		lo := doc.LayerOverrides[name]
		switch lo.Space {
		case "", SpaceWorld, SpaceScreen:
		default:
			return fieldErr(at(lp, "space"), "sceneformat: layerOverrides %q has invalid space %q (want %q or %q)", name, lo.Space, SpaceWorld, SpaceScreen)
		}
		if lo.Tilemap != nil {
			if err := validateTilemap(at(lp, "tilemap"), fmt.Sprintf("layerOverrides %q tilemap", name), lo.Tilemap, true, doc.Tilemaps, false); err != nil {
				return err
			}
		}
	}
	return validateOverrides([]string{"overrides"}, "overrides", doc.Overrides)
}

// validateInstance checks the instancing fields version 2 added to an object found at path in the document.
func validateInstance(version int, path []string, layer string, o *ObjectDoc) error {
	if o.Scene == "" && len(o.Overrides) == 0 {
		return nil
	}
	where := fmt.Sprintf("layer %q object %q", layer, o.ID)
	if version < 2 {
		field := "scene"
		if o.Scene == "" {
			field = "overrides"
		}
		return fieldErr(at(path, field), "sceneformat: %s: scene and overrides need version 2, not %d", where, version)
	}
	if o.Scene == "" {
		return fieldErr(at(path, "overrides"), "sceneformat: %s: overrides require scene", where)
	}
	if !validFilePath(o.Scene) {
		return fieldErr(at(path, "scene"), "sceneformat: %s scene %q must be a slash-separated path relative to the project root", where, o.Scene)
	}
	return validateOverrides(at(path, "overrides"), where+" overrides", o.Overrides)
}

// validateOverrides checks every override's values; path leads to the map in the document and where names it in errors.
func validateOverrides(path []string, where string, ovs map[string]ObjectOverride) error {
	for _, id := range slices.Sorted(maps.Keys(ovs)) {
		if id == "" {
			return fieldErr(path, "sceneformat: %s contains an empty object id", where)
		}
		ov := ovs[id]
		var probe ObjectDoc
		ov.applyTo(&probe)
		if err := validateObject(at(path, id), fmt.Sprintf("%s %q", where, id), &probe); err != nil {
			return err
		}
	}
	return nil
}

// applyTo sets every field of o that ov sets.
func (ov *ObjectOverride) applyTo(o *ObjectDoc) {
	setValue(&o.Name, ov.Name)
	setValue(&o.Prefab, ov.Prefab)
	setValue(&o.X, ov.X)
	setValue(&o.Y, ov.Y)
	setValue(&o.PosZ, ov.PosZ)
	setValue(&o.OriginX, ov.OriginX)
	setValue(&o.OriginY, ov.OriginY)
	setValue(&o.Rotation, ov.Rotation)
	setValue(&o.RotX, ov.RotX)
	setValue(&o.RotY, ov.RotY)
	setValue(&o.RotZ, ov.RotZ)
	setPointer(&o.ScaleX, ov.ScaleX)
	setPointer(&o.ScaleY, ov.ScaleY)
	setPointer(&o.ScaleZ, ov.ScaleZ)
	setValue(&o.Depth, ov.Depth)
	setValue(&o.Mesh, ov.Mesh)
	setValue(&o.Model, ov.Model)
	setValue(&o.ModelNode, ov.ModelNode)
	setValue(&o.ModelMesh, ov.ModelMesh)
	setValue(&o.FlipX, ov.FlipX)
	setValue(&o.FlipY, ov.FlipY)
	setPointer(&o.Alpha, ov.Alpha)
	setValue(&o.Z, ov.Z)
	setPointer(&o.Sprite, ov.Sprite)
	setValue(&o.Material, ov.Material)
	o.Components = mergeAnyMap(o.Components, ov.Components)
	o.Properties = mergeAnyMap(o.Properties, ov.Properties)
}

// applyTo sets every field of l that lo sets.
func (lo *LayerOverride) applyTo(l *LayerDoc) {
	if lo.Space != "" {
		l.Space = lo.Space
	}
	setValue(&l.Order, lo.Order)
	setPointer(&l.Visible, lo.Visible)
	if lo.Tilemap != nil {
		l.Tilemap = cloneTilemap(lo.Tilemap)
	}
}

func setValue[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

func setPointer[T any](dst **T, v *T) {
	if v != nil {
		c := *v
		*dst = &c
	}
}

// mergeAnyMap returns dst with deep copies of src's entries set over its own, allocating dst when it is nil and src is not empty.
func mergeAnyMap(dst, src map[string]any) map[string]any {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		dst[k] = cloneAny(v)
	}
	return dst
}

// indexObjects records every object of objs and its descendants in byID.
func indexObjects(objs []ObjectDoc, byID map[string]*ObjectDoc) {
	WalkObjects(objs, func(o, _ *ObjectDoc) bool {
		byID[o.ID] = o
		return true
	})
}

func countObjects(objs []ObjectDoc) int {
	n := 0
	WalkObjects(objs, func(_, _ *ObjectDoc) bool {
		n++
		return true
	})
	return n
}

func layerIndex(layers []LayerDoc, name string) int {
	return slices.IndexFunc(layers, func(l LayerDoc) bool { return l.Name == name })
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// Clone returns a deep copy of doc: its camera, tilemaps, layers, objects, overrides, connections and maps are all new, so changing the copy never changes doc.
func (doc *SceneDoc) Clone() *SceneDoc {
	if doc == nil {
		return nil
	}
	out := *doc
	out.Camera = cloneCamera(doc.Camera)
	out.Tilemaps = cloneTilemaps(doc.Tilemaps)
	if doc.LayerOverrides != nil {
		out.LayerOverrides = make(map[string]LayerOverride, len(doc.LayerOverrides))
		for k, lo := range doc.LayerOverrides {
			c := lo
			c.Order = clonePtr(lo.Order)
			c.Visible = clonePtr(lo.Visible)
			c.Tilemap = cloneTilemap(lo.Tilemap)
			out.LayerOverrides[k] = c
		}
	}
	out.Overrides = cloneOverrides(doc.Overrides)
	out.Connections = cloneConnections(doc.Connections)
	if doc.Layers != nil {
		out.Layers = make([]LayerDoc, len(doc.Layers))
		for i := range doc.Layers {
			l := &doc.Layers[i]
			out.Layers[i] = cloneLayerFields(l)
			if l.Objects != nil {
				out.Layers[i].Objects = make([]ObjectDoc, len(l.Objects))
				for j := range l.Objects {
					out.Layers[i].Objects[j] = l.Objects[j].Clone()
				}
			}
		}
	}
	return &out
}

// Clone returns a deep copy of o: its children, overrides, sprite, optional values and maps are all new, so changing the copy never changes o.
func (o *ObjectDoc) Clone() ObjectDoc {
	out := cloneObjectFields(o)
	if o.Children != nil {
		out.Children = make([]ObjectDoc, len(o.Children))
		for i := range o.Children {
			out.Children[i] = o.Children[i].Clone()
		}
	}
	return out
}

// cloneObjectFields deep copies o's own fields, overrides included, and leaves its children nil.
func cloneObjectFields(o *ObjectDoc) ObjectDoc {
	out := *o
	out.ScaleX = clonePtr(o.ScaleX)
	out.ScaleY = clonePtr(o.ScaleY)
	out.ScaleZ = clonePtr(o.ScaleZ)
	out.Alpha = clonePtr(o.Alpha)
	out.Sprite = clonePtr(o.Sprite)
	out.Components = cloneAnyMap(o.Components)
	out.Properties = cloneAnyMap(o.Properties)
	out.Overrides = cloneOverrides(o.Overrides)
	out.Children = nil
	return out
}

func cloneOverrides(ovs map[string]ObjectOverride) map[string]ObjectOverride {
	if ovs == nil {
		return nil
	}
	out := make(map[string]ObjectOverride, len(ovs))
	for k, ov := range ovs {
		c := ov
		c.Name = clonePtr(ov.Name)
		c.Prefab = clonePtr(ov.Prefab)
		c.X = clonePtr(ov.X)
		c.Y = clonePtr(ov.Y)
		c.PosZ = clonePtr(ov.PosZ)
		c.OriginX = clonePtr(ov.OriginX)
		c.OriginY = clonePtr(ov.OriginY)
		c.Rotation = clonePtr(ov.Rotation)
		c.RotX = clonePtr(ov.RotX)
		c.RotY = clonePtr(ov.RotY)
		c.RotZ = clonePtr(ov.RotZ)
		c.ScaleX = clonePtr(ov.ScaleX)
		c.ScaleY = clonePtr(ov.ScaleY)
		c.ScaleZ = clonePtr(ov.ScaleZ)
		c.Depth = clonePtr(ov.Depth)
		c.Mesh = clonePtr(ov.Mesh)
		c.Model = clonePtr(ov.Model)
		c.ModelNode = clonePtr(ov.ModelNode)
		c.ModelMesh = clonePtr(ov.ModelMesh)
		c.FlipX = clonePtr(ov.FlipX)
		c.FlipY = clonePtr(ov.FlipY)
		c.Alpha = clonePtr(ov.Alpha)
		c.Z = clonePtr(ov.Z)
		c.Sprite = clonePtr(ov.Sprite)
		c.Material = clonePtr(ov.Material)
		c.Components = cloneAnyMap(ov.Components)
		c.Properties = cloneAnyMap(ov.Properties)
		out[k] = c
	}
	return out
}

// cloneLayerFields deep copies l's own fields and leaves its objects nil.
func cloneLayerFields(l *LayerDoc) LayerDoc {
	return LayerDoc{
		Name:    l.Name,
		Space:   l.Space,
		Order:   l.Order,
		Visible: clonePtr(l.Visible),
		Tilemap: cloneTilemap(l.Tilemap),
	}
}

func cloneCamera(c *CameraDoc) *CameraDoc {
	if c == nil {
		return nil
	}
	out := *c
	out.Zoom = clonePtr(c.Zoom)
	out.Bounds = clonePtr(c.Bounds)
	return &out
}

func cloneTilemaps(tms map[string]*TilemapDoc) map[string]*TilemapDoc {
	if tms == nil {
		return nil
	}
	out := make(map[string]*TilemapDoc, len(tms))
	for k, tm := range tms {
		out[k] = cloneTilemap(tm)
	}
	return out
}

func cloneTilemap(tm *TilemapDoc) *TilemapDoc {
	if tm == nil {
		return nil
	}
	out := *tm
	out.Data = slices.Clone(tm.Data)
	return &out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneAny(v)
	}
	return out
}

// cloneAny deep copies the maps and lists YAML decodes into, and returns any other value as it is.
func cloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneAnyMap(x)
	case map[any]any:
		out := make(map[any]any, len(x))
		for k, e := range x {
			out[k] = cloneAny(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneAny(x[i])
		}
		return out
	case []string:
		return slices.Clone(x)
	default:
		return v
	}
}
