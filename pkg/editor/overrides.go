package editor

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// ObjectOrigin identifies where an object in the active scene originates.
type ObjectOrigin int

const (
	// ObjectLocal is an object defined directly in Editor.Doc.
	ObjectLocal ObjectOrigin = iota
	// ObjectInstanced is an object brought in by a local scene instance (ObjectDoc.Scene) in Editor.Doc.
	ObjectInstanced
	// ObjectInherited is an object brought in by the scene's base scene (SceneDoc.Inherits), including any instanced objects inside that base scene.
	ObjectInherited
)

// String returns a human-readable label for the object origin.
func (o ObjectOrigin) String() string {
	switch o {
	case ObjectInstanced:
		return "instanced"
	case ObjectInherited:
		return "inherited"
	default:
		return "local"
	}
}

// ObjectOverrideInfo describes an object's origin and where edits to it record overrides.
type ObjectOverrideInfo struct {
	// ID is the object's full ID in the resolved scene (for example "enemy_1/body" or "hero").
	ID string
	// Origin is ObjectLocal, ObjectInstanced, or ObjectInherited.
	Origin ObjectOrigin
	// Layer is the name of the layer holding the object in the resolved scene.
	Layer string
	// InstanceID is the ID of the owning local scene-instancing object in Editor.Doc when Origin is ObjectInstanced, or "" otherwise.
	InstanceID string
	// Key is the override map key: the object's ID inside the instanced scene when Origin is ObjectInstanced, or the full ID when Origin is ObjectInherited.
	Key string
	// SourceScene is the slash-separated path of the scene file that defines this object (instance.Scene or Editor.Doc.Inherits).
	SourceScene string
}

// baseSceneCache caches resolved un-overridden source scenes for one revision of the editor.
type baseSceneCache struct {
	rev       uint64
	scenesRev uint64
	doc       *sceneformat.SceneDoc
	resolved  map[string]*sceneformat.SceneDoc
}

// instanceCountCache caches per-file scene instance counts scanned from disk for one (ProjectPath, filesRev, scenesRev).
type instanceCountCache struct {
	projectPath string
	filesRev    uint64
	scenesRev   uint64
	// perFile maps clean project scene path -> map[referencedScenePath]count.
	perFile   map[string]map[string]int
	diskScans uint64
}

// resolvedDoc returns the scene document as resolved (Built.Doc when valid), falling back to e.Doc.
func (e *Editor) resolvedDoc() *sceneformat.SceneDoc {
	if e == nil {
		return nil
	}
	if e.Built != nil && e.BuildErr == nil && e.Built.Doc != nil {
		return e.Built.Doc
	}
	return e.Doc
}

// FindResolvedObject locates object id in Editor.Doc or, for instanced and inherited objects,
// in the resolved BuiltScene document, returning the ObjectDoc and its layer name.
func (e *Editor) FindResolvedObject(id string) (*sceneformat.ObjectDoc, string) {
	if e == nil || id == "" {
		return nil, ""
	}
	if obj, li, _ := e.FindObject(id); obj != nil {
		return obj, e.Doc.Layers[li].Name
	}
	if e.Built != nil && e.Built.Doc != nil && e.Built.Doc != e.Doc {
		if loc := locateObject(e.Built.Doc, id); loc.obj != nil {
			return loc.obj, e.Built.Doc.Layers[loc.layer].Name
		}
	}
	return nil, ""
}

// resolvedObjectPath returns the ancestor chain from top-level object down to id in the resolved scene,
// falling back to e.Doc.
func (e *Editor) resolvedObjectPath(id string) []*sceneformat.ObjectDoc {
	if e == nil || id == "" {
		return nil
	}
	if doc := e.resolvedDoc(); doc != nil {
		if path := objectPath(doc, id); path != nil {
			return path
		}
	}
	return objectPath(e.Doc, id)
}

// ObjectOverrideInfo returns origin and override routing metadata for object id.
// If id does not exist in either Editor.Doc or the resolved scene, the returned info has an empty ID.
func (e *Editor) ObjectOverrideInfo(id string) ObjectOverrideInfo {
	if e == nil || e.Doc == nil || id == "" {
		return ObjectOverrideInfo{}
	}
	if obj, li, _ := e.FindObject(id); obj != nil {
		return ObjectOverrideInfo{
			ID:          id,
			Origin:      ObjectLocal,
			Layer:       e.Doc.Layers[li].Name,
			SourceScene: obj.Scene,
		}
	}
	if e.Built == nil || e.Built.Doc == nil {
		return ObjectOverrideInfo{}
	}
	loc := locateObject(e.Built.Doc, id)
	if loc.obj == nil {
		return ObjectOverrideInfo{}
	}
	layerName := e.Built.Doc.Layers[loc.layer].Name
	path := objectPath(e.Built.Doc, id)
	for i := len(path) - 2; i >= 0; i-- {
		ancID := path[i].ID
		if localAnc, _, _ := e.FindObject(ancID); localAnc != nil && localAnc.Scene != "" && strings.HasPrefix(id, ancID+"/") {
			return ObjectOverrideInfo{
				ID:          id,
				Origin:      ObjectInstanced,
				Layer:       layerName,
				InstanceID:  ancID,
				Key:         strings.TrimPrefix(id, ancID+"/"),
				SourceScene: localAnc.Scene,
			}
		}
	}
	if e.Doc.Inherits != "" {
		return ObjectOverrideInfo{
			ID:          id,
			Origin:      ObjectInherited,
			Layer:       layerName,
			Key:         id,
			SourceScene: e.Doc.Inherits,
		}
	}
	return ObjectOverrideInfo{}
}

// ObjectOrigin returns whether id is a local, instanced, or inherited object.
func (e *Editor) ObjectOrigin(id string) ObjectOrigin {
	return e.ObjectOverrideInfo(id).Origin
}

// resolveSourceScene loads and resolves scenePath through e.Resolver, cached per editor revision.
func (e *Editor) resolveSourceScene(scenePath string) (*sceneformat.SceneDoc, error) {
	if e == nil || scenePath == "" {
		return nil, errors.New("editor: empty source scene path")
	}
	c := &e.baseScenes
	if c.resolved == nil || c.rev != e.revision || c.scenesRev != e.scenesRev || c.doc != e.Doc {
		*c = baseSceneCache{
			rev:       e.revision,
			scenesRev: e.scenesRev,
			doc:       e.Doc,
			resolved:  make(map[string]*sceneformat.SceneDoc),
		}
	}
	if doc, ok := c.resolved[scenePath]; ok {
		return doc, nil
	}
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		return nil, errNoSceneResolver
	}
	raw, err := scenes.Scene(scenePath)
	if err != nil {
		return nil, err
	}
	res, err := sceneformat.Resolve(raw, scenes)
	if err != nil {
		return nil, err
	}
	c.resolved[scenePath] = res
	return res, nil
}

// baseObjectFor returns the un-overridden ObjectDoc from the source scene for an instanced or inherited object.
func (e *Editor) baseObjectFor(info ObjectOverrideInfo) (*sceneformat.ObjectDoc, error) {
	if info.Origin == ObjectLocal || info.SourceScene == "" {
		return nil, nil
	}
	src, err := e.resolveSourceScene(info.SourceScene)
	if err != nil {
		return nil, err
	}
	loc := locateObject(src, info.Key)
	if loc.obj == nil {
		return nil, fmt.Errorf("editor: source scene %q has no object %q", info.SourceScene, info.Key)
	}
	return loc.obj, nil
}

// baseLayerFor returns the un-overridden LayerDoc from the base scene for an inherited layer.
func (e *Editor) baseLayerFor(layerName string) (*sceneformat.LayerDoc, error) {
	if e == nil || e.Doc == nil || e.Doc.Inherits == "" {
		return nil, nil
	}
	src, err := e.resolveSourceScene(e.Doc.Inherits)
	if err != nil {
		return nil, err
	}
	for i := range src.Layers {
		if src.Layers[i].Name == layerName {
			return &src.Layers[i], nil
		}
	}
	return nil, fmt.Errorf("editor: base scene %q has no layer %q", e.Doc.Inherits, layerName)
}

// checkCandidateScene validates candidate and, if it references other scenes, resolves it
// (serving candidate for e.ScenePath when set so cycles are caught) and validates components,
// physics layers, and connections on the resolved scene as well.
func (e *Editor) checkCandidateScene(candidate *sceneformat.SceneDoc) (*sceneformat.SceneDoc, error) {
	if err := sceneformat.Validate(candidate); err != nil {
		return nil, err
	}
	if e.components != nil {
		if err := sceneformat.ValidateComponents(candidate, e.components); err != nil {
			return nil, err
		}
	}
	layers := e.PhysicsLayers()
	if layers != nil {
		if err := sceneformat.ValidatePhysicsLayers(candidate, layers); err != nil {
			return nil, err
		}
	}
	if !candidate.RefersToScenes() {
		if e.components != nil && len(candidate.Connections) > 0 {
			if err := sceneformat.ValidateConnections(candidate, e.components); err != nil {
				return nil, err
			}
		}
		return candidate, nil
	}
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		return nil, errNoSceneResolver
	}
	var (
		res *sceneformat.SceneDoc
		err error
	)
	if e.ScenePath == "" {
		res, err = sceneformat.Resolve(candidate, scenes)
	} else {
		probe := &sceneformat.SceneDoc{Version: max(candidate.Version, sceneformat.CurrentVersion), Inherits: e.ScenePath}
		res, err = sceneformat.Resolve(probe, editedScene{SceneResolver: scenes, path: e.ScenePath, doc: candidate})
	}
	if err != nil {
		return nil, err
	}
	if err := sceneformat.Validate(res); err != nil {
		return nil, err
	}
	if e.components != nil {
		if err := sceneformat.ValidateComponents(res, e.components); err != nil {
			return nil, err
		}
	}
	if layers != nil {
		if err := sceneformat.ValidatePhysicsLayers(res, layers); err != nil {
			return nil, err
		}
	}
	if e.components != nil && len(res.Connections) > 0 {
		if err := sceneformat.ValidateConnections(res, e.components); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// currentOverrideFor returns a deep copy of the existing ObjectOverride for info, whether it existed, and whether its map was nil.
func (e *Editor) currentOverrideFor(doc *sceneformat.SceneDoc, info ObjectOverrideInfo) (ov sceneformat.ObjectOverride, existed, wasNilMap bool, ok bool) {
	switch info.Origin {
	case ObjectInstanced:
		loc := locateObject(doc, info.InstanceID)
		if loc.obj == nil || loc.obj.Scene == "" {
			return sceneformat.ObjectOverride{}, false, false, false
		}
		wasNilMap = loc.obj.Overrides == nil
		if existing, has := loc.obj.Overrides[info.Key]; has {
			return cloneResolvedObjectOverride(existing), true, wasNilMap, true
		}
		return sceneformat.ObjectOverride{}, false, wasNilMap, true
	case ObjectInherited:
		if doc.Inherits == "" {
			return sceneformat.ObjectOverride{}, false, false, false
		}
		wasNilMap = doc.Overrides == nil
		if existing, has := doc.Overrides[info.Key]; has {
			return cloneResolvedObjectOverride(existing), true, wasNilMap, true
		}
		return sceneformat.ObjectOverride{}, false, wasNilMap, true
	default:
		return sceneformat.ObjectOverride{}, false, false, false
	}
}

// setOverrideOnDoc writes or removes ov for info in doc, pruning empty maps.
func setOverrideOnDoc(doc *sceneformat.SceneDoc, info ObjectOverrideInfo, ov sceneformat.ObjectOverride, existed bool, wasNilMap bool) bool {
	var target *map[string]sceneformat.ObjectOverride
	switch info.Origin {
	case ObjectInstanced:
		loc := locateObject(doc, info.InstanceID)
		if loc.obj == nil {
			return false
		}
		target = &loc.obj.Overrides
	case ObjectInherited:
		target = &doc.Overrides
	default:
		return false
	}
	normalizeObjectOverride(&ov)
	if !existed || isEmptyObjectOverride(ov) {
		if *target != nil {
			delete(*target, info.Key)
			if len(*target) == 0 {
				*target = nil
			}
		}
		if len(*target) == 0 && wasNilMap {
			*target = nil
		}
		return true
	}
	if *target == nil {
		*target = make(map[string]sceneformat.ObjectOverride)
	}
	(*target)[info.Key] = cloneResolvedObjectOverride(ov)
	return true
}

// editObjectOverride applies mutate to the ObjectOverride of an instanced or inherited object,
// verifies that the resulting document validates and resolves cleanly, and executes one undoable command.
func (e *Editor) editObjectOverride(id, label string, minVersion int, mutate func(ov *sceneformat.ObjectOverride, resolved, base *sceneformat.ObjectDoc) error) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	info := e.ObjectOverrideInfo(id)
	if info.Origin == ObjectLocal || info.ID == "" {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if e.BuildErr != nil {
		return fmt.Errorf("editor: cannot override %q while scene fails to resolve: %w", id, e.BuildErr)
	}
	resolved, _ := e.FindResolvedObject(id)
	if resolved == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	base, err := e.baseObjectFor(info)
	if err != nil {
		return err
	}
	oldOv, hadOld, wasNilMap, ok := e.currentOverrideFor(e.Doc, info)
	if !ok {
		return fmt.Errorf("editor: cannot locate override owner for %q", id)
	}
	nextOv := cloneResolvedObjectOverride(oldOv)
	if err := mutate(&nextOv, resolved, base); err != nil {
		return err
	}
	normalizeObjectOverride(&nextOv)
	hasNext := !isEmptyObjectOverride(nextOv)
	if hadOld == hasNext && reflect.DeepEqual(oldOv, nextOv) {
		return nil
	}
	reqVersion := max(2, minVersion)
	if nextOv.Light != nil || nextOv.Camera != nil {
		reqVersion = max(reqVersion, sceneformat.CurrentVersion)
	}
	candidate := e.Doc.Clone()
	if candidate.Version < reqVersion && hasNext {
		candidate.Version = reqVersion
	}
	setOverrideOnDoc(candidate, info, nextOv, hasNext, wasNilMap)
	if _, err := e.checkCandidateScene(candidate); err != nil {
		return err
	}
	e.History.Execute(&objectOverrideCmd{
		ed:          e,
		info:        info,
		label:       label,
		oldOv:       oldOv,
		hadOld:      hadOld,
		wasNilMap:   wasNilMap,
		newOv:       nextOv,
		hasNew:      hasNext,
		prevVersion: e.Doc.Version,
		nextVersion: candidate.Version,
	})
	return nil
}

type objectOverrideCmd struct {
	ed          *Editor
	info        ObjectOverrideInfo
	label       string
	oldOv       sceneformat.ObjectOverride
	hadOld      bool
	wasNilMap   bool
	newOv       sceneformat.ObjectOverride
	hasNew      bool
	prevVersion int
	nextVersion int
}

func (c *objectOverrideCmd) Label() string { return c.label }

func (c *objectOverrideCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	if c.hasNew && c.ed.Doc.Version < c.nextVersion {
		c.ed.Doc.Version = c.nextVersion
	}
	setOverrideOnDoc(c.ed.Doc, c.info, c.newOv, c.hasNew, c.wasNilMap)
	c.ed.Status = c.label
}

func (c *objectOverrideCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	setOverrideOnDoc(c.ed.Doc, c.info, c.oldOv, c.hadOld, c.wasNilMap)
	c.ed.Doc.Version = c.prevVersion
}

// --- Override Inspection & Revert APIs ---

// ObjectOverrideFields returns the sorted list of overridden field keys on objectID
// (such as "x", "y", "name", "sprite", "property:speed", "component:health.hp").
// When objectID is a local scene instance root, it returns all override target keys ("body", ...).
func (e *Editor) ObjectOverrideFields(objectID string) []string {
	if e == nil || e.Doc == nil || objectID == "" {
		return nil
	}
	info := e.ObjectOverrideInfo(objectID)
	if info.Origin == ObjectLocal {
		obj, _, _ := e.FindObject(objectID)
		if obj == nil || len(obj.Overrides) == 0 {
			return nil
		}
		return slices.Sorted(maps.Keys(obj.Overrides))
	}
	ov, existed, _, ok := e.currentOverrideFor(e.Doc, info)
	if !ok || !existed {
		return nil
	}
	base, _ := e.baseObjectFor(info)
	var fields []string
	if ov.Name != nil {
		fields = append(fields, "name")
	}
	if ov.Prefab != nil {
		fields = append(fields, "prefab")
	}
	if ov.X != nil {
		fields = append(fields, "x")
	}
	if ov.Y != nil {
		fields = append(fields, "y")
	}
	if ov.PosZ != nil {
		fields = append(fields, "posZ")
	}
	if ov.OriginX != nil {
		fields = append(fields, "originX")
	}
	if ov.OriginY != nil {
		fields = append(fields, "originY")
	}
	if ov.Rotation != nil {
		fields = append(fields, "rotation")
	}
	if ov.RotX != nil {
		fields = append(fields, "rotX")
	}
	if ov.RotY != nil {
		fields = append(fields, "rotY")
	}
	if ov.RotZ != nil {
		fields = append(fields, "rotZ")
	}
	if ov.ScaleX != nil {
		fields = append(fields, "scaleX")
	}
	if ov.ScaleY != nil {
		fields = append(fields, "scaleY")
	}
	if ov.ScaleZ != nil {
		fields = append(fields, "scaleZ")
	}
	if ov.Depth != nil {
		fields = append(fields, "depth")
	}
	if ov.Mesh != nil {
		fields = append(fields, "mesh")
	}
	if ov.Model != nil {
		fields = append(fields, "model")
	}
	if ov.ModelNode != nil {
		fields = append(fields, "modelNode")
	}
	if ov.ModelMesh != nil {
		fields = append(fields, "modelMesh")
	}
	if ov.GridMap != nil {
		fields = append(fields, "gridMap")
	}
	if ov.Occluder != nil {
		fields = append(fields, "occluder")
	}
	if ov.FlipX != nil {
		fields = append(fields, "flipX")
	}
	if ov.FlipY != nil {
		fields = append(fields, "flipY")
	}
	if ov.Alpha != nil {
		fields = append(fields, "alpha")
	}
	if ov.Z != nil {
		fields = append(fields, "z")
	}
	if ov.Sprite != nil {
		fields = append(fields, "sprite")
	}
	if ov.Material != nil {
		fields = append(fields, "material")
	}
	if ov.Particles != nil {
		fields = append(fields, "particles")
	}
	if ov.Light != nil {
		fields = append(fields, "light")
	}
	if ov.Camera != nil {
		fields = append(fields, "camera")
	}
	for _, k := range sortedMapKeys(ov.Properties) {
		fields = append(fields, "property:"+k)
	}
	for _, cn := range sortedMapKeys(ov.Components) {
		var baseMap map[string]any
		baseHasComp := false
		if base != nil && base.Components != nil {
			if raw, ok := base.Components[cn]; ok {
				baseHasComp = true
				baseMap, _ = raw.(map[string]any)
			}
		}
		if !baseHasComp {
			fields = append(fields, "component:"+cn)
		}
		ovMap, ok := ov.Components[cn].(map[string]any)
		if !ok {
			if baseHasComp {
				fields = append(fields, "component:"+cn)
			}
			continue
		}
		seen := make(map[string]bool)
		for _, fk := range sortedMapKeys(ovMap) {
			seen[fk] = true
			bv, hasBase := baseMap[fk]
			if !baseHasComp || !hasBase || !reflect.DeepEqual(ovMap[fk], bv) {
				fields = append(fields, "component:"+cn+"."+fk)
			}
		}
		for _, fk := range sortedMapKeys(baseMap) {
			if !seen[fk] {
				fields = append(fields, "component:"+cn+"."+fk)
			}
		}
	}
	slices.Sort(fields)
	return fields
}

// HasObjectOverride reports whether fieldKey is currently overridden on objectID.
func (e *Editor) HasObjectOverride(objectID, fieldKey string) bool {
	if e == nil || e.Doc == nil || objectID == "" || fieldKey == "" {
		return false
	}
	info := e.ObjectOverrideInfo(objectID)
	if info.Origin == ObjectLocal {
		return false
	}
	return slices.Contains(e.ObjectOverrideFields(objectID), fieldKey)
}

// ObjectHasAnyOverride reports whether objectID (an instanced/inherited object, or a local scene instance root) has any overrides recorded.
func (e *Editor) ObjectHasAnyOverride(objectID string) bool {
	return len(e.ObjectOverrideFields(objectID)) > 0
}

// InstanceOverrideCount returns the total number of overridden fields across all targets of a local scene instance root (or on the owning instance of an instanced child).
func (e *Editor) InstanceOverrideCount(objectID string) int {
	if e == nil || e.Doc == nil || objectID == "" {
		return 0
	}
	info := e.ObjectOverrideInfo(objectID)
	switch info.Origin {
	case ObjectLocal:
		obj, _, _ := e.FindObject(objectID)
		if obj == nil || obj.Scene == "" {
			return 0
		}
		total := 0
		for key := range obj.Overrides {
			total += len(e.ObjectOverrideFields(obj.ID + "/" + key))
		}
		return total
	case ObjectInstanced:
		return len(e.ObjectOverrideFields(objectID))
	case ObjectInherited:
		return len(e.ObjectOverrideFields(objectID))
	default:
		return 0
	}
}

// ObjectBaseValue returns a short human-readable string of the un-overridden source value for fieldKey on objectID.
func (e *Editor) ObjectBaseValue(objectID, fieldKey string) string {
	info := e.ObjectOverrideInfo(objectID)
	if info.Origin == ObjectLocal {
		return ""
	}
	base, err := e.baseObjectFor(info)
	if err != nil || base == nil {
		return ""
	}
	fmtF32 := func(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }
	switch fieldKey {
	case "name":
		if base.Name == "" {
			return `""`
		}
		return fmt.Sprintf("%q", base.Name)
	case "prefab":
		return fmt.Sprintf("%q", base.Prefab)
	case "x":
		return fmtF32(base.X)
	case "y":
		return fmtF32(base.Y)
	case "posZ":
		return fmtF32(base.PosZ)
	case "originX":
		return fmtF32(base.OriginX)
	case "originY":
		return fmtF32(base.OriginY)
	case "rotation":
		return fmtF32(base.Rotation)
	case "rotX":
		return fmtF32(base.RotX)
	case "rotY":
		return fmtF32(base.RotY)
	case "rotZ":
		return fmtF32(base.RotZ)
	case "scaleX":
		return fmtF32(base.EffectiveScaleX())
	case "scaleY":
		return fmtF32(base.EffectiveScaleY())
	case "scaleZ":
		return fmtF32(base.EffectiveScaleZ())
	case "depth":
		return fmtF32(base.Depth)
	case "mesh":
		if base.Mesh == "" {
			return "none"
		}
		return base.Mesh
	case "model":
		if base.Model == "" {
			return "none"
		}
		return base.Model
	case "gridMap":
		if base.GridMap == "" {
			return "none"
		}
		return base.GridMap
	case "occluder":
		if base.Occluder == nil {
			return "none"
		}
		return string(base.Occluder.Mode)
	case "flipX":
		return strconv.FormatBool(base.FlipX)
	case "flipY":
		return strconv.FormatBool(base.FlipY)
	case "alpha":
		return fmtF32(base.EffectiveAlpha())
	case "z":
		return strconv.Itoa(base.Z)
	case "sprite":
		if base.Sprite == nil {
			return "none"
		}
		return base.Sprite.Atlas + "/" + base.Sprite.Frame
	case "material":
		if base.Material == "" {
			return "none"
		}
		return base.Material
	case "particles":
		if base.Particles == nil {
			return "none"
		}
		return base.Particles.File
	case "light":
		if base.Light == nil {
			return "none"
		}
		return string(base.Light.Kind)
	case "camera":
		if base.Camera == nil {
			return "none"
		}
		return string(base.Camera.EffectiveProjection())
	}
	if propKey, ok := strings.CutPrefix(fieldKey, "property:"); ok {
		if base.Properties != nil {
			if v, exists := base.Properties[propKey]; exists {
				return formatFieldValue(v)
			}
		}
		return "unset"
	}
	if compSpec, ok := strings.CutPrefix(fieldKey, "component:"); ok {
		cn, fk, hasField := strings.Cut(compSpec, ".")
		if !hasField {
			if base.Components != nil {
				if _, exists := base.Components[cn]; exists {
					return cn
				}
			}
			return "unset"
		}
		if base.Components != nil {
			if cm, ok := base.Components[cn].(map[string]any); ok {
				if v, exists := cm[fk]; exists {
					return formatFieldValue(v)
				}
			}
		}
		if ct, ok := e.lookupComponentType(cn); ok {
			for _, f := range ct.Fields {
				if f.Name == fk {
					return formatFieldValue(f.Default)
				}
			}
		}
		return "unset"
	}
	return ""
}

// RevertObjectOverrideField removes one overridden field from objectID's override entry as one undoable step,
// pruning empty maps so the saved YAML stays clean.
func (e *Editor) RevertObjectOverrideField(objectID, fieldKey string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	info := e.ObjectOverrideInfo(objectID)
	if info.Origin == ObjectLocal {
		return fmt.Errorf("editor: object %q is not an instanced or inherited object", objectID)
	}
	return e.editObjectOverride(objectID, "Revert "+fieldKey+" on "+objectID, 2, func(ov *sceneformat.ObjectOverride, _, base *sceneformat.ObjectDoc) error {
		switch fieldKey {
		case "name":
			ov.Name = nil
		case "prefab":
			ov.Prefab = nil
		case "x":
			ov.X = nil
		case "y":
			ov.Y = nil
		case "posZ":
			ov.PosZ = nil
		case "originX":
			ov.OriginX = nil
		case "originY":
			ov.OriginY = nil
		case "rotation":
			ov.Rotation = nil
		case "rotX":
			ov.RotX = nil
		case "rotY":
			ov.RotY = nil
		case "rotZ":
			ov.RotZ = nil
		case "scaleX":
			ov.ScaleX = nil
		case "scaleY":
			ov.ScaleY = nil
		case "scaleZ":
			ov.ScaleZ = nil
		case "depth":
			ov.Depth = nil
		case "mesh":
			ov.Mesh = nil
		case "model":
			ov.Model = nil
			ov.ModelNode = nil
			ov.ModelMesh = nil
		case "modelNode":
			ov.ModelNode = nil
		case "modelMesh":
			ov.ModelMesh = nil
		case "gridMap":
			ov.GridMap = nil
		case "occluder":
			ov.Occluder = nil
		case "flipX":
			ov.FlipX = nil
		case "flipY":
			ov.FlipY = nil
		case "alpha":
			ov.Alpha = nil
		case "z":
			ov.Z = nil
		case "sprite":
			ov.Sprite = nil
		case "material":
			ov.Material = nil
		case "particles":
			ov.Particles = nil
		case "light":
			ov.Light = nil
		case "camera":
			ov.Camera = nil
		default:
			if propKey, ok := strings.CutPrefix(fieldKey, "property:"); ok {
				if ov.Properties != nil {
					delete(ov.Properties, propKey)
				}
				return nil
			}
			if compSpec, ok := strings.CutPrefix(fieldKey, "component:"); ok {
				if ov.Components == nil {
					return nil
				}
				cn, fk, hasField := strings.Cut(compSpec, ".")
				if !hasField {
					delete(ov.Components, cn)
					return nil
				}
				ovMap, ok := ov.Components[cn].(map[string]any)
				if !ok {
					delete(ov.Components, cn)
					return nil
				}
				var baseMap map[string]any
				baseHasComp := false
				if base != nil && base.Components != nil {
					if raw, ok := base.Components[cn]; ok {
						baseHasComp = true
						baseMap, _ = raw.(map[string]any)
					}
				}
				if baseHasComp {
					if bv, exists := baseMap[fk]; exists {
						ovMap[fk] = cloneAny(bv)
					} else {
						delete(ovMap, fk)
					}
					if reflect.DeepEqual(ovMap, baseMap) {
						delete(ov.Components, cn)
					}
				} else {
					delete(ovMap, fk)
					if len(ovMap) == 0 {
						delete(ov.Components, cn)
					}
				}
				return nil
			}
			return fmt.Errorf("editor: unknown override field %q", fieldKey)
		}
		return nil
	})
}

// RevertObjectOverrides removes all overrides for objectID as one undoable step.
// When objectID is a local scene instance root, it removes all entries in its Overrides map.
// When objectID is an instanced child or inherited object, it removes that object's override entry
// (and clears the owning Overrides map when it becomes empty).
func (e *Editor) RevertObjectOverrides(objectID string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if objectID == "" {
		objectID = e.SelectedID
	}
	info := e.ObjectOverrideInfo(objectID)
	if info.Origin == ObjectLocal {
		obj, _, _ := e.FindObject(objectID)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", objectID)
		}
		if obj.Scene == "" {
			return fmt.Errorf("editor: object %q does not instance a scene", objectID)
		}
		if len(obj.Overrides) == 0 {
			return nil
		}
		candidate := e.Doc.Clone()
		loc := locateObject(candidate, objectID)
		loc.obj.Overrides = nil
		if _, err := e.checkCandidateScene(candidate); err != nil {
			return err
		}
		e.History.Execute(&revertInstanceOverridesCmd{
			ed:           e,
			instanceID:   objectID,
			oldOverrides: cloneResolvedObjectOverridesMap(obj.Overrides),
		})
		return nil
	}
	return e.editObjectOverride(objectID, "Revert all overrides on "+objectID, 2, func(ov *sceneformat.ObjectOverride, _, _ *sceneformat.ObjectDoc) error {
		*ov = sceneformat.ObjectOverride{}
		return nil
	})
}

type revertInstanceOverridesCmd struct {
	ed           *Editor
	instanceID   string
	oldOverrides map[string]sceneformat.ObjectOverride
}

func (c *revertInstanceOverridesCmd) Label() string {
	return "Revert all overrides on " + c.instanceID
}

func (c *revertInstanceOverridesCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.instanceID); obj != nil {
		obj.Overrides = nil
		c.ed.Status = "Reverted overrides on " + c.instanceID
	}
}

func (c *revertInstanceOverridesCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.instanceID); obj != nil {
		obj.Overrides = cloneResolvedObjectOverridesMap(c.oldOverrides)
	}
}

// --- Layer Override APIs ---

func (e *Editor) editLayerOverride(layerName, label string, minVersion int, mutate func(lo *sceneformat.LayerOverride, effective, base *sceneformat.LayerDoc) error) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc.Inherits == "" || !e.inheritedLayers()[layerName] {
		return fmt.Errorf("editor: layer %q is not inherited", layerName)
	}
	if e.BuildErr != nil {
		return fmt.Errorf("editor: cannot override layer %q while scene fails to resolve: %w", layerName, e.BuildErr)
	}
	base, err := e.baseLayerFor(layerName)
	if err != nil {
		return err
	}
	var eff *sceneformat.LayerDoc
	for _, l := range e.effectiveLayers() {
		if l.Name == layerName {
			c := l
			eff = &c
			break
		}
	}
	if eff == nil {
		eff = base
	}
	wasNilMap := e.Doc.LayerOverrides == nil
	oldLo, hadOld := e.Doc.LayerOverrides[layerName]
	oldLo = cloneLayerOverride(oldLo)
	nextLo := cloneLayerOverride(oldLo)
	if err := mutate(&nextLo, eff, base); err != nil {
		return err
	}
	hasNext := !isEmptyLayerOverride(nextLo)
	if hadOld == hasNext && reflect.DeepEqual(oldLo, nextLo) {
		return nil
	}
	reqVersion := max(2, minVersion)
	if nextLo.ParallaxX != nil || nextLo.ParallaxY != nil || nextLo.RepeatX != nil || nextLo.RepeatY != nil || nextLo.RepeatWidth != nil || nextLo.RepeatHeight != nil {
		reqVersion = max(reqVersion, 3)
	}
	candidate := e.Doc.Clone()
	if candidate.Version < reqVersion && hasNext {
		candidate.Version = max(reqVersion, sceneformat.CurrentVersion)
	}
	setLayerOverrideOnDoc(candidate, layerName, nextLo, hasNext, wasNilMap)
	if _, err := e.checkCandidateScene(candidate); err != nil {
		return err
	}
	e.History.Execute(&layerOverrideCmd{
		ed:          e,
		layerName:   layerName,
		label:       label,
		oldLo:       oldLo,
		hadOld:      hadOld,
		wasNilMap:   wasNilMap,
		newLo:       nextLo,
		hasNew:      hasNext,
		prevVersion: e.Doc.Version,
		nextVersion: candidate.Version,
	})
	return nil
}

func setLayerOverrideOnDoc(doc *sceneformat.SceneDoc, layerName string, lo sceneformat.LayerOverride, existed, wasNilMap bool) {
	if !existed || isEmptyLayerOverride(lo) {
		if doc.LayerOverrides != nil {
			delete(doc.LayerOverrides, layerName)
			if len(doc.LayerOverrides) == 0 {
				doc.LayerOverrides = nil
			}
		}
		if len(doc.LayerOverrides) == 0 && wasNilMap {
			doc.LayerOverrides = nil
		}
		return
	}
	if doc.LayerOverrides == nil {
		doc.LayerOverrides = make(map[string]sceneformat.LayerOverride)
	}
	doc.LayerOverrides[layerName] = cloneLayerOverride(lo)
}

type layerOverrideCmd struct {
	ed          *Editor
	layerName   string
	label       string
	oldLo       sceneformat.LayerOverride
	hadOld      bool
	wasNilMap   bool
	newLo       sceneformat.LayerOverride
	hasNew      bool
	prevVersion int
	nextVersion int
}

func (c *layerOverrideCmd) Label() string { return c.label }

func (c *layerOverrideCmd) Do() {
	if c.ed.Doc == nil {
		return
	}
	if c.hasNew && c.ed.Doc.Version < c.nextVersion {
		c.ed.Doc.Version = c.nextVersion
	}
	setLayerOverrideOnDoc(c.ed.Doc, c.layerName, c.newLo, c.hasNew, c.wasNilMap)
	c.ed.Status = c.label
}

func (c *layerOverrideCmd) Undo() {
	if c.ed.Doc == nil {
		return
	}
	setLayerOverrideOnDoc(c.ed.Doc, c.layerName, c.oldLo, c.hadOld, c.wasNilMap)
	c.ed.Doc.Version = c.prevVersion
}

// LayerOverrideFields returns the sorted list of overridden fields in SceneDoc.LayerOverrides[layerName].
func (e *Editor) LayerOverrideFields(layerName string) []string {
	if e == nil || e.Doc == nil || layerName == "" {
		return nil
	}
	lo, ok := e.Doc.LayerOverrides[layerName]
	if !ok {
		return nil
	}
	var fields []string
	if lo.Space != "" {
		fields = append(fields, "space")
	}
	if lo.Order != nil {
		fields = append(fields, "order")
	}
	if lo.Visible != nil {
		fields = append(fields, "visible")
	}
	if lo.ParallaxX != nil {
		fields = append(fields, "parallaxX")
	}
	if lo.ParallaxY != nil {
		fields = append(fields, "parallaxY")
	}
	if lo.RepeatX != nil {
		fields = append(fields, "repeatX")
	}
	if lo.RepeatY != nil {
		fields = append(fields, "repeatY")
	}
	if lo.RepeatWidth != nil {
		fields = append(fields, "repeatWidth")
	}
	if lo.RepeatHeight != nil {
		fields = append(fields, "repeatHeight")
	}
	if lo.Tilemap != nil {
		fields = append(fields, "tilemap")
	}
	slices.Sort(fields)
	return fields
}

// HasLayerOverride reports whether fieldKey is overridden on inherited layerName.
func (e *Editor) HasLayerOverride(layerName, fieldKey string) bool {
	return slices.Contains(e.LayerOverrideFields(layerName), fieldKey)
}

// LayerBaseValue returns a short human-readable summary of the un-overridden base value of fieldKey on layerName.
func (e *Editor) LayerBaseValue(layerName, fieldKey string) string {
	base, err := e.baseLayerFor(layerName)
	if err != nil || base == nil {
		return ""
	}
	fmtF32 := func(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }
	switch fieldKey {
	case "space":
		return string(base.EffectiveSpace())
	case "order":
		return strconv.Itoa(base.Order)
	case "visible":
		return strconv.FormatBool(base.IsVisible())
	case "parallaxX":
		return fmtF32(base.EffectiveParallaxX())
	case "parallaxY":
		return fmtF32(base.EffectiveParallaxY())
	case "repeatX":
		return strconv.FormatBool(base.RepeatX)
	case "repeatY":
		return strconv.FormatBool(base.RepeatY)
	case "repeatWidth":
		return fmtF32(base.RepeatWidth)
	case "repeatHeight":
		return fmtF32(base.RepeatHeight)
	case "tilemap":
		return e.describeTilemap(base.Tilemap)
	default:
		return ""
	}
}

// RevertLayerOverrideField removes one overridden field from SceneDoc.LayerOverrides[layerName] as one undoable step.
func (e *Editor) RevertLayerOverrideField(layerName, fieldKey string) error {
	return e.editLayerOverride(layerName, "Revert "+fieldKey+" on layer "+layerName, 2, func(lo *sceneformat.LayerOverride, _, _ *sceneformat.LayerDoc) error {
		switch fieldKey {
		case "space":
			lo.Space = ""
		case "order":
			lo.Order = nil
		case "visible":
			lo.Visible = nil
		case "parallaxX":
			lo.ParallaxX = nil
		case "parallaxY":
			lo.ParallaxY = nil
		case "repeatX":
			lo.RepeatX = nil
		case "repeatY":
			lo.RepeatY = nil
		case "repeatWidth":
			lo.RepeatWidth = nil
		case "repeatHeight":
			lo.RepeatHeight = nil
		case "tilemap":
			lo.Tilemap = nil
		default:
			return fmt.Errorf("editor: unknown layer override field %q", fieldKey)
		}
		return nil
	})
}

// RevertLayerOverrides removes all overrides for layerName from SceneDoc.LayerOverrides as one undoable step.
func (e *Editor) RevertLayerOverrides(layerName string) error {
	return e.editLayerOverride(layerName, "Revert all overrides on layer "+layerName, 2, func(lo *sceneformat.LayerOverride, _, _ *sceneformat.LayerDoc) error {
		*lo = sceneformat.LayerOverride{}
		return nil
	})
}

// --- Make Local ---

// MakeInstanceLocal replaces a scene instance object (objectID, its owning instance if objectID is an instanced child,
// or SelectedID when empty) with a local copy of its resolved objects and connections in one undoable step.
func (e *Editor) MakeInstanceLocal(objectID string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if objectID == "" {
		objectID = e.SelectedID
	}
	if objectID == "" {
		return errors.New("editor: no object selected")
	}
	targetID := objectID
	if info := e.ObjectOverrideInfo(objectID); info.Origin == ObjectInstanced {
		targetID = info.InstanceID
	}
	loc := locateObject(e.Doc, targetID)
	if loc.obj == nil {
		return fmt.Errorf("editor: object %q not found", targetID)
	}
	if loc.obj.Scene == "" {
		return fmt.Errorf("editor: object %q does not instance a scene", targetID)
	}
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		return errNoSceneResolver
	}

	// Expand only this object's Scene (keeping its local Children unflattened).
	probeObj := loc.obj.Clone()
	ownChildren := probeObj.Children
	probeObj.Children = nil
	probe := &sceneformat.SceneDoc{
		Version: max(e.Doc.Version, sceneformat.CurrentVersion),
		Layers:  []sceneformat.LayerDoc{{Name: "probe", Objects: []sceneformat.ObjectDoc{probeObj}}},
	}
	expanded, err := sceneformat.Resolve(probe, scenes)
	if err != nil {
		return err
	}
	localObj := expanded.Layers[0].Objects[0].Clone()
	localObj.Children = append(localObj.Children, ownChildren...)
	var addedConns []sceneformat.ConnectionDoc
	if len(expanded.Connections) > 0 {
		addedConns = make([]sceneformat.ConnectionDoc, len(expanded.Connections))
		for i, conn := range expanded.Connections {
			addedConns[i] = cloneConnectionDoc(conn)
		}
	}

	candidate := e.Doc.Clone()
	candLoc := locateObject(candidate, targetID)
	if candLoc.obj == nil {
		return fmt.Errorf("editor: object %q not found", targetID)
	}
	*candLoc.obj = localObj.Clone()
	for _, conn := range addedConns {
		candidate.Connections = append(candidate.Connections, cloneConnectionDoc(conn))
	}
	if candidate.Version < sceneformat.CurrentVersion && (drawsModel(&localObj) || hasOccluder(&localObj) || hasParticles(&localObj) || usesScene3DFeatures(&localObj)) {
		candidate.Version = sceneformat.CurrentVersion
	}
	if _, err := e.checkCandidateScene(candidate); err != nil {
		return err
	}

	e.CancelDrag2D()
	e.CancelGizmoDrag3D()
	e.History.Execute(&makeInstanceLocalCmd{
		ed:              e,
		targetID:        targetID,
		before:          loc.obj.Clone(),
		after:           localObj,
		addedConns:      addedConns,
		wasNilConns:     e.Doc.Connections == nil,
		prevVersion:     e.Doc.Version,
		nextVersion:     candidate.Version,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: slices.Clone(e.SelectedIDs()),
	})
	return nil
}

type makeInstanceLocalCmd struct {
	ed              *Editor
	targetID        string
	before          sceneformat.ObjectDoc
	after           sceneformat.ObjectDoc
	addedConns      []sceneformat.ConnectionDoc
	wasNilConns     bool
	prevVersion     int
	nextVersion     int
	prevSelected    string
	prevSelectedIDs []string
}

func (c *makeInstanceLocalCmd) Label() string { return "Make " + c.targetID + " local" }

func (c *makeInstanceLocalCmd) Do() {
	loc := locateObject(c.ed.Doc, c.targetID)
	if loc.obj == nil {
		return
	}
	*loc.obj = c.after.Clone()
	for _, conn := range c.addedConns {
		c.ed.Doc.Connections = append(c.ed.Doc.Connections, cloneConnectionDoc(conn))
	}
	if c.ed.Doc.Version < c.nextVersion {
		c.ed.Doc.Version = c.nextVersion
	}
	c.ed.Status = "Made " + c.targetID + " local"
}

func (c *makeInstanceLocalCmd) Undo() {
	loc := locateObject(c.ed.Doc, c.targetID)
	if loc.obj == nil {
		return
	}
	*loc.obj = c.before.Clone()
	if n := len(c.addedConns); n > 0 && len(c.ed.Doc.Connections) >= n {
		c.ed.Doc.Connections = c.ed.Doc.Connections[:len(c.ed.Doc.Connections)-n]
		if len(c.ed.Doc.Connections) == 0 && c.wasNilConns {
			c.ed.Doc.Connections = nil
		}
	}
	c.ed.Doc.Version = c.prevVersion
	c.ed.SelectedID = c.prevSelected
	c.ed.selectedIDs = slices.Clone(c.prevSelectedIDs)
}

// --- Apply Overrides to Source ---

// ApplyOverridesToSource writes the overrides of a scene instance (objectID, its owning instance if objectID is an
// instanced child, or SelectedID when empty) into its source scene file on disk and clears those overrides on the
// instance as one undoable step. Undo restores both the source file on disk and the instance's overrides.
func (e *Editor) ApplyOverridesToSource(objectID string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if objectID == "" {
		objectID = e.SelectedID
	}
	if objectID == "" {
		return errors.New("editor: no object selected")
	}
	targetID := objectID
	if info := e.ObjectOverrideInfo(objectID); info.Origin == ObjectInstanced {
		targetID = info.InstanceID
	}
	inst, _, _ := e.FindObject(targetID)
	if inst == nil {
		return fmt.Errorf("editor: object %q not found", targetID)
	}
	if inst.Scene == "" {
		return fmt.Errorf("editor: object %q does not instance a scene", targetID)
	}
	if len(inst.Overrides) == 0 {
		return fmt.Errorf("editor: instance %q has no overrides to apply", targetID)
	}

	sourceRel, err := normalizeProjectAssetPath(inst.Scene)
	if err != nil {
		return fmt.Errorf("editor: invalid source scene %q: %w", inst.Scene, err)
	}
	if e.ScenePath != "" && sourceRel == e.ScenePath {
		return fmt.Errorf("editor: cannot apply overrides onto the active scene %q", sourceRel)
	}
	rootDir := filepath.Dir(e.ProjectPath)
	sourceDisk := filepath.Join(rootDir, filepath.FromSlash(sourceRel))
	if err := verifyFileWithinRoot(rootDir, sourceDisk, sourceRel); err != nil {
		return err
	}
	if idx := e.findTabByPath(sourceRel, sourceDisk); idx >= 0 && e.TabDirty(idx) {
		return fmt.Errorf("editor: save or discard unsaved edits in %q before applying overrides", sourceRel)
	}

	beforeBytes, err := os.ReadFile(sourceDisk)
	if err != nil {
		return fmt.Errorf("editor: read source scene %q: %w", sourceRel, err)
	}
	sourceDoc, err := sceneformat.ParseWith(beforeBytes, sceneformat.ParseOptions{
		File:          sourceDisk,
		Components:    e.components,
		PhysicsLayers: e.PhysicsLayers(),
	})
	if err != nil {
		return fmt.Errorf("editor: parse source scene %q: %w", sourceRel, err)
	}
	afterDoc := sourceDoc.Clone()
	for _, key := range slices.Sorted(maps.Keys(inst.Overrides)) {
		ov := inst.Overrides[key]
		if err := applyOverrideIntoSourceDoc(afterDoc, key, ov); err != nil {
			return fmt.Errorf("editor: apply override %q to %q: %w", key, sourceRel, err)
		}
	}
	if err := sceneformat.Validate(afterDoc); err != nil {
		return fmt.Errorf("editor: updated source scene %q invalid: %w", sourceRel, err)
	}
	if e.components != nil {
		if err := sceneformat.ValidateComponents(afterDoc, e.components); err != nil {
			return fmt.Errorf("editor: updated source scene %q components invalid: %w", sourceRel, err)
		}
	}
	layers := e.PhysicsLayers()
	if layers != nil {
		if err := sceneformat.ValidatePhysicsLayers(afterDoc, layers); err != nil {
			return fmt.Errorf("editor: updated source scene %q physics layers invalid: %w", sourceRel, err)
		}
	}
	afterBytes, err := sceneformat.Encode(afterDoc)
	if err != nil {
		return fmt.Errorf("editor: encode source scene %q: %w", sourceRel, err)
	}

	// Verify that clearing overrides on the instance resolves cleanly against the updated source scene.
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		return errNoSceneResolver
	}
	candidate := e.Doc.Clone()
	candLoc := locateObject(candidate, targetID)
	candLoc.obj.Overrides = nil
	resWithUpdated := editedScene{SceneResolver: scenes, path: sourceRel, doc: afterDoc}
	var resolved *sceneformat.SceneDoc
	if e.ScenePath != "" {
		probe := &sceneformat.SceneDoc{Version: max(candidate.Version, sceneformat.CurrentVersion), Inherits: e.ScenePath}
		resolved, err = sceneformat.Resolve(probe, editedScene{SceneResolver: resWithUpdated, path: e.ScenePath, doc: candidate})
	} else {
		resolved, err = sceneformat.Resolve(candidate, resWithUpdated)
	}
	if err != nil {
		return err
	}
	if err := sceneformat.Validate(resolved); err != nil {
		return err
	}
	if e.components != nil {
		if err := sceneformat.ValidateComponents(resolved, e.components); err != nil {
			return err
		}
	}
	if layers != nil {
		if err := sceneformat.ValidatePhysicsLayers(resolved, layers); err != nil {
			return err
		}
	}
	if e.components != nil && len(resolved.Connections) > 0 {
		if err := sceneformat.ValidateConnections(resolved, e.components); err != nil {
			return err
		}
	}

	instCount, sceneCount := e.CountSceneInstances(sourceRel)
	cmd := &applyOverridesToSourceCmd{
		ed:           e,
		instanceID:   targetID,
		rootDir:      rootDir,
		sourceRel:    sourceRel,
		sourceDisk:   sourceDisk,
		beforeBytes:  beforeBytes,
		afterBytes:   afterBytes,
		oldOverrides: cloneResolvedObjectOverridesMap(inst.Overrides),
		instCount:    instCount,
		sceneCount:   sceneCount,
	}
	return e.History.execute(cmd)
}

type applyOverridesToSourceCmd struct {
	ed           *Editor
	instanceID   string
	rootDir      string
	sourceRel    string
	sourceDisk   string
	beforeBytes  []byte
	afterBytes   []byte
	oldOverrides map[string]sceneformat.ObjectOverride
	instCount    int
	sceneCount   int
}

func (c *applyOverridesToSourceCmd) Label() string {
	return "Apply overrides from " + c.instanceID + " to " + c.sourceRel
}

func (c *applyOverridesToSourceCmd) Do() {
	if err := c.do(); err != nil {
		c.ed.report(err)
	}
}

func (c *applyOverridesToSourceCmd) Undo() {
	if err := c.undo(); err != nil {
		c.ed.report(err)
	}
}

func (c *applyOverridesToSourceCmd) do() error {
	if c.ed.ScenePath != "" && c.sourceRel == c.ed.ScenePath {
		return fmt.Errorf("editor: cannot apply overrides onto the active scene %q", c.sourceRel)
	}
	if err := verifyFileWithinRoot(c.rootDir, c.sourceDisk, c.sourceRel); err != nil {
		return err
	}
	if idx := c.ed.findTabByPath(c.sourceRel, c.sourceDisk); idx >= 0 && c.ed.TabDirty(idx) {
		return fmt.Errorf("editor: save or discard unsaved edits in %q before applying overrides", c.sourceRel)
	}
	if err := writeFileAtomic(c.sourceDisk, c.afterBytes); err != nil {
		return err
	}
	if obj, _, _ := c.ed.FindObject(c.instanceID); obj != nil {
		obj.Overrides = nil
	}
	c.syncSourceTabs(c.afterBytes)
	c.ed.Status = fmt.Sprintf("Applied overrides to %s (affects %d instances in %d scenes)", c.sourceRel, c.instCount, c.sceneCount)
	return nil
}

func (c *applyOverridesToSourceCmd) undo() error {
	if c.ed.ScenePath != "" && c.sourceRel == c.ed.ScenePath {
		return fmt.Errorf("editor: cannot revert applied overrides on the active scene %q", c.sourceRel)
	}
	if err := verifyFileWithinRoot(c.rootDir, c.sourceDisk, c.sourceRel); err != nil {
		return err
	}
	if idx := c.ed.findTabByPath(c.sourceRel, c.sourceDisk); idx >= 0 && c.ed.TabDirty(idx) {
		return fmt.Errorf("editor: save or discard unsaved edits in %q before reverting applied overrides", c.sourceRel)
	}
	if err := writeFileAtomic(c.sourceDisk, c.beforeBytes); err != nil {
		return err
	}
	if obj, _, _ := c.ed.FindObject(c.instanceID); obj != nil {
		obj.Overrides = cloneResolvedObjectOverridesMap(c.oldOverrides)
	}
	c.syncSourceTabs(c.beforeBytes)
	c.ed.Status = "Reverted applied overrides on " + c.sourceRel
	return nil
}

func (c *applyOverridesToSourceCmd) syncSourceTabs(data []byte) {
	e := c.ed
	if idx := e.findTabByPath(c.sourceRel, c.sourceDisk); idx >= 0 && idx != e.activeTab {
		if doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
			File:          c.sourceDisk,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		}); err == nil {
			t := e.tabs[idx]
			t.doc = doc
			t.history = e.newTabHistory()
			if encoded, err := sceneformat.Encode(doc); err == nil {
				t.sceneSaved = encoded
			} else {
				t.sceneSaved = append([]byte(nil), data...)
			}
			diskPath := t.filePath
			if diskPath == "" {
				diskPath = c.sourceDisk
			}
			t.sceneDisk = makeDiskSnapshot(diskPath, data)
			if e.disk.saveConflict != nil && e.disk.saveConflictTabID == t.id {
				e.disk.saveConflict = nil
				e.disk.saveConflictTabID = 0
			}
			if built, err := sceneformat.Build(doc, e.resolverForScene(t.scenePath)); err == nil {
				built.DrawAllTiles = true
				t.built = built
				t.buildErr = nil
			} else {
				t.buildErr = err
			}
		}
	}
	e.scenesRev++
	e.baseLayers = baseLayerCache{}
	e.baseScenes = baseSceneCache{}
	e.invalidateProjectIndex()
	e.snapshotProjectTree()
	e.rebuildDependentTabs(e.activeTabID())
}

// applyOverrideIntoSourceDoc merges ov for key into sourceDoc (direct object, nested instance override, or inherited override).
func applyOverrideIntoSourceDoc(sourceDoc *sceneformat.SceneDoc, key string, ov sceneformat.ObjectOverride) error {
	if loc := locateObject(sourceDoc, key); loc.obj != nil {
		applyObjectOverrideToDoc(loc.obj, ov)
		bumpVersionForOverride(sourceDoc, ov)
		return nil
	}
	// Check if key belongs to a nested scene instance inside sourceDoc.
	var bestInst *sceneformat.ObjectDoc
	var bestPrefix string
	for li := range sourceDoc.Layers {
		sceneformat.WalkObjects(sourceDoc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.Scene != "" && strings.HasPrefix(key, o.ID+"/") && len(o.ID) > len(bestPrefix) {
				bestInst = o
				bestPrefix = o.ID
			}
			return true
		})
	}
	if bestInst != nil {
		subKey := strings.TrimPrefix(key, bestPrefix+"/")
		if bestInst.Overrides == nil {
			bestInst.Overrides = make(map[string]sceneformat.ObjectOverride)
		}
		merged := mergeObjectOverride(bestInst.Overrides[subKey], ov)
		bestInst.Overrides[subKey] = merged
		bumpVersionForOverride(sourceDoc, ov)
		return nil
	}
	if sourceDoc.Inherits != "" {
		if sourceDoc.Overrides == nil {
			sourceDoc.Overrides = make(map[string]sceneformat.ObjectOverride)
		}
		sourceDoc.Overrides[key] = mergeObjectOverride(sourceDoc.Overrides[key], ov)
		bumpVersionForOverride(sourceDoc, ov)
		return nil
	}
	return fmt.Errorf("object %q not found", key)
}

func bumpVersionForOverride(doc *sceneformat.SceneDoc, ov sceneformat.ObjectOverride) {
	if doc.Version < 2 && (ov.Model != nil || ov.ModelNode != nil || ov.ModelMesh != nil || ov.GridMap != nil || ov.Occluder != nil || ov.Particles != nil) {
		doc.Version = 2
	}
	if doc.Version < sceneformat.CurrentVersion && (ov.Light != nil || ov.Camera != nil) {
		doc.Version = sceneformat.CurrentVersion
	}
}

func applyObjectOverrideToDoc(o *sceneformat.ObjectDoc, ov sceneformat.ObjectOverride) {
	if ov.Name != nil {
		o.Name = *ov.Name
	}
	if ov.Prefab != nil {
		o.Prefab = *ov.Prefab
	}
	if ov.X != nil {
		o.X = *ov.X
	}
	if ov.Y != nil {
		o.Y = *ov.Y
	}
	if ov.PosZ != nil {
		o.PosZ = *ov.PosZ
	}
	if ov.OriginX != nil {
		o.OriginX = *ov.OriginX
	}
	if ov.OriginY != nil {
		o.OriginY = *ov.OriginY
	}
	if ov.Rotation != nil {
		o.Rotation = *ov.Rotation
	}
	if ov.RotX != nil {
		o.RotX = *ov.RotX
	}
	if ov.RotY != nil {
		o.RotY = *ov.RotY
	}
	if ov.RotZ != nil {
		o.RotZ = *ov.RotZ
	}
	if ov.ScaleX != nil {
		o.ScaleX = cloneFloat32Ptr(ov.ScaleX)
	}
	if ov.ScaleY != nil {
		o.ScaleY = cloneFloat32Ptr(ov.ScaleY)
	}
	if ov.ScaleZ != nil {
		o.ScaleZ = cloneFloat32Ptr(ov.ScaleZ)
	}
	if ov.Depth != nil {
		o.Depth = *ov.Depth
	}
	if ov.Mesh != nil {
		o.Mesh = *ov.Mesh
	}
	if ov.Model != nil {
		o.Model = *ov.Model
	}
	if ov.ModelNode != nil {
		o.ModelNode = *ov.ModelNode
	}
	if ov.ModelMesh != nil {
		o.ModelMesh = *ov.ModelMesh
	}
	if ov.GridMap != nil {
		o.GridMap = *ov.GridMap
	}
	if ov.Occluder != nil {
		o.Occluder = cloneOccluderDoc(ov.Occluder)
	}
	if ov.FlipX != nil {
		o.FlipX = *ov.FlipX
	}
	if ov.FlipY != nil {
		o.FlipY = *ov.FlipY
	}
	if ov.Alpha != nil {
		o.Alpha = cloneFloat32Ptr(ov.Alpha)
	}
	if ov.Z != nil {
		o.Z = *ov.Z
	}
	if ov.Sprite != nil {
		o.Sprite = cloneSprite(ov.Sprite)
	}
	if ov.Material != nil {
		o.Material = *ov.Material
	}
	if ov.Particles != nil {
		o.Particles = cloneParticlesRef(ov.Particles)
	}
	if ov.Light != nil {
		o.Light = ov.Light.Clone()
	}
	if ov.Camera != nil {
		o.Camera = ov.Camera.Clone()
	}
	if len(ov.Components) > 0 {
		if o.Components == nil {
			o.Components = make(map[string]any, len(ov.Components))
		}
		for k, v := range ov.Components {
			o.Components[k] = cloneAny(v)
		}
	}
	if len(ov.Properties) > 0 {
		if o.Properties == nil {
			o.Properties = make(map[string]any, len(ov.Properties))
		}
		for k, v := range ov.Properties {
			o.Properties[k] = cloneAny(v)
		}
	}
}

func mergeObjectOverride(dst, src sceneformat.ObjectOverride) sceneformat.ObjectOverride {
	out := cloneResolvedObjectOverride(dst)
	if src.Name != nil {
		out.Name = clonePtr(src.Name)
	}
	if src.Prefab != nil {
		out.Prefab = clonePtr(src.Prefab)
	}
	if src.X != nil {
		out.X = cloneFloat32Ptr(src.X)
	}
	if src.Y != nil {
		out.Y = cloneFloat32Ptr(src.Y)
	}
	if src.PosZ != nil {
		out.PosZ = cloneFloat32Ptr(src.PosZ)
	}
	if src.OriginX != nil {
		out.OriginX = cloneFloat32Ptr(src.OriginX)
	}
	if src.OriginY != nil {
		out.OriginY = cloneFloat32Ptr(src.OriginY)
	}
	if src.Rotation != nil {
		out.Rotation = cloneFloat32Ptr(src.Rotation)
	}
	if src.RotX != nil {
		out.RotX = cloneFloat32Ptr(src.RotX)
	}
	if src.RotY != nil {
		out.RotY = cloneFloat32Ptr(src.RotY)
	}
	if src.RotZ != nil {
		out.RotZ = cloneFloat32Ptr(src.RotZ)
	}
	if src.ScaleX != nil {
		out.ScaleX = cloneFloat32Ptr(src.ScaleX)
	}
	if src.ScaleY != nil {
		out.ScaleY = cloneFloat32Ptr(src.ScaleY)
	}
	if src.ScaleZ != nil {
		out.ScaleZ = cloneFloat32Ptr(src.ScaleZ)
	}
	if src.Depth != nil {
		out.Depth = cloneFloat32Ptr(src.Depth)
	}
	if src.Mesh != nil {
		out.Mesh = clonePtr(src.Mesh)
	}
	if src.Model != nil {
		out.Model = clonePtr(src.Model)
	}
	if src.ModelNode != nil {
		out.ModelNode = clonePtr(src.ModelNode)
	}
	if src.ModelMesh != nil {
		out.ModelMesh = clonePtr(src.ModelMesh)
	}
	if src.GridMap != nil {
		out.GridMap = clonePtr(src.GridMap)
	}
	if src.Occluder != nil {
		out.Occluder = cloneOccluderDoc(src.Occluder)
	}
	if src.FlipX != nil {
		out.FlipX = clonePtr(src.FlipX)
	}
	if src.FlipY != nil {
		out.FlipY = clonePtr(src.FlipY)
	}
	if src.Alpha != nil {
		out.Alpha = cloneFloat32Ptr(src.Alpha)
	}
	if src.Z != nil {
		out.Z = clonePtr(src.Z)
	}
	if src.Sprite != nil {
		out.Sprite = cloneSprite(src.Sprite)
	}
	if src.Material != nil {
		out.Material = clonePtr(src.Material)
	}
	if src.Particles != nil {
		out.Particles = cloneParticlesRef(src.Particles)
	}
	if src.Light != nil {
		out.Light = src.Light.Clone()
	}
	if src.Camera != nil {
		out.Camera = src.Camera.Clone()
	}
	if len(src.Components) > 0 {
		if out.Components == nil {
			out.Components = make(map[string]any, len(src.Components))
		}
		for k, v := range src.Components {
			out.Components[k] = cloneAny(v)
		}
	}
	if len(src.Properties) > 0 {
		if out.Properties == nil {
			out.Properties = make(map[string]any, len(src.Properties))
		}
		for k, v := range src.Properties {
			out.Properties[k] = cloneAny(v)
		}
	}
	return out
}

// --- Instance Counting, Discovery, and Source Navigation ---

// SceneInstances returns the IDs of all local objects in the active scene that instance scenePath, in document order.
func (e *Editor) SceneInstances(scenePath string) []string {
	if e == nil || e.Doc == nil || scenePath == "" {
		return nil
	}
	var ids []string
	for li := range e.Doc.Layers {
		sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.Scene == scenePath {
				ids = append(ids, o.ID)
			}
			return true
		})
	}
	return ids
}

// CountSceneInstances counts how many objects across the open project (or open tabs when no project is open)
// instance scenePath, returning (totalInstances, affectedScenes).
// Disk-backed scene files are scanned once per (ProjectPath, filesRev, scenesRev) and overlaid with all open tabs' live in-memory documents.
func (e *Editor) CountSceneInstances(scenePath string) (instances, scenes int) {
	if e == nil || scenePath == "" {
		return 0, 0
	}
	countDoc := func(doc *sceneformat.SceneDoc) int {
		if doc == nil {
			return 0
		}
		n := 0
		for li := range doc.Layers {
			sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
				if o.Scene == scenePath {
					n++
				}
				return true
			})
		}
		return n
	}
	if e.Project == nil || e.ProjectPath == "" {
		if len(e.tabs) > 0 {
			for i, t := range e.tabs {
				doc := t.doc
				if i == e.activeTab {
					doc = e.Doc
				}
				if n := countDoc(doc); n > 0 {
					instances += n
					scenes++
				}
			}
			return instances, scenes
		}
		if n := countDoc(e.Doc); n > 0 {
			return n, 1
		}
		return 0, 0
	}

	// Rebuild per-file disk cache only when project/files/scenes revisions change.
	c := &e.instanceCounts
	if c.perFile == nil || c.projectPath != e.ProjectPath || c.filesRev != e.filesRev || c.scenesRev != e.scenesRev {
		perFile := make(map[string]map[string]int)
		if files, err := e.ProjectFiles(); err == nil {
			root := filepath.Dir(e.ProjectPath)
			for _, f := range files {
				if f.Dir || !isProjectScene(e.Project, f.Path) {
					continue
				}
				data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
				if err != nil || !bytes.Contains(data, []byte("scene:")) {
					perFile[f.Path] = nil
					continue
				}
				doc, err := sceneformat.Parse(data)
				if err != nil {
					perFile[f.Path] = nil
					continue
				}
				counts := make(map[string]int)
				for li := range doc.Layers {
					sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
						if o.Scene != "" {
							counts[o.Scene]++
						}
						return true
					})
				}
				perFile[f.Path] = counts
			}
		}
		c.projectPath = e.ProjectPath
		c.filesRev = e.filesRev
		c.scenesRev = e.scenesRev
		c.perFile = perFile
		c.diskScans++
	}

	// Overlay live in-memory docs from all open tabs (active and inactive).
	openDocs := make(map[string]*sceneformat.SceneDoc)
	var untitledDocs []*sceneformat.SceneDoc
	if len(e.tabs) > 0 {
		for i, t := range e.tabs {
			doc := t.doc
			sp := t.scenePath
			if i == e.activeTab {
				doc = e.Doc
				sp = e.ScenePath
			}
			if doc == nil {
				continue
			}
			if sp != "" {
				openDocs[sp] = doc
			} else {
				untitledDocs = append(untitledDocs, doc)
			}
		}
	} else if e.Doc != nil {
		if e.ScenePath != "" {
			openDocs[e.ScenePath] = e.Doc
		} else {
			untitledDocs = append(untitledDocs, e.Doc)
		}
	}

	seen := make(map[string]bool, len(openDocs))
	for sp, doc := range openDocs {
		seen[sp] = true
		if n := countDoc(doc); n > 0 {
			instances += n
			scenes++
		}
	}
	for _, doc := range untitledDocs {
		if n := countDoc(doc); n > 0 {
			instances += n
			scenes++
		}
	}
	for filePath, counts := range c.perFile {
		if seen[filePath] {
			continue
		}
		if n := counts[scenePath]; n > 0 {
			instances += n
			scenes++
		}
	}
	return instances, scenes
}

// OpenSourceScene opens the source scene file of objectID (or SelectedID when empty) in a tab:
// for a scene instance root or instanced child it opens the instanced scene, and for an inherited object it opens the base scene.
func (e *Editor) OpenSourceScene(objectID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	id := objectID
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		if e.Doc != nil && e.Doc.Inherits != "" {
			return e.OpenBaseScene()
		}
		return errors.New("editor: no object selected")
	}
	info := e.ObjectOverrideInfo(id)
	switch info.Origin {
	case ObjectLocal:
		if info.SourceScene != "" {
			return e.OpenScene(info.SourceScene)
		}
		return fmt.Errorf("editor: object %q does not instance a scene", id)
	case ObjectInstanced:
		return e.OpenScene(info.SourceScene)
	case ObjectInherited:
		return e.OpenBaseScene()
	default:
		return fmt.Errorf("editor: object %q not found", id)
	}
}

// --- Registered Prefab selection helpers for Asset Browser ---

// PrefabNames returns the names of all registered prefabs in Editor.Prefabs, sorted alphabetically.
func (e *Editor) PrefabNames() []string {
	if e == nil || len(e.Prefabs) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(e.Prefabs))
}

// SelectPrefab marks the named registered prefab as selected in the Asset Browser (clearing SelectedAsset),
// or clears it when name is empty.
func (e *Editor) SelectPrefab(name string) bool {
	if e == nil {
		return false
	}
	if name == "" {
		if e.SelectedPrefab != "" {
			e.SelectedPrefab = ""
			e.notify()
		}
		return true
	}
	if _, ok := e.Prefabs[name]; !ok {
		return false
	}
	e.SelectedPrefab = name
	e.SelectedAsset = nil
	e.Status = "Selected prefab " + name
	e.notify()
	return true
}

// --- Override cloning, normalization, and emptiness helpers ---

func normalizeObjectOverride(ov *sceneformat.ObjectOverride) {
	if ov == nil {
		return
	}
	if len(ov.Components) == 0 {
		ov.Components = nil
	}
	if len(ov.Properties) == 0 {
		ov.Properties = nil
	}
}

func isEmptyObjectOverride(ov sceneformat.ObjectOverride) bool {
	return ov.Name == nil &&
		ov.Prefab == nil &&
		ov.X == nil &&
		ov.Y == nil &&
		ov.PosZ == nil &&
		ov.OriginX == nil &&
		ov.OriginY == nil &&
		ov.Rotation == nil &&
		ov.RotX == nil &&
		ov.RotY == nil &&
		ov.RotZ == nil &&
		ov.ScaleX == nil &&
		ov.ScaleY == nil &&
		ov.ScaleZ == nil &&
		ov.Depth == nil &&
		ov.Mesh == nil &&
		ov.Model == nil &&
		ov.ModelNode == nil &&
		ov.ModelMesh == nil &&
		ov.GridMap == nil &&
		ov.Occluder == nil &&
		ov.FlipX == nil &&
		ov.FlipY == nil &&
		ov.Alpha == nil &&
		ov.Z == nil &&
		ov.Sprite == nil &&
		ov.Material == nil &&
		ov.Particles == nil &&
		ov.Light == nil &&
		ov.Camera == nil &&
		len(ov.Components) == 0 &&
		len(ov.Properties) == 0
}

func isEmptyLayerOverride(lo sceneformat.LayerOverride) bool {
	return lo.Space == "" &&
		lo.Order == nil &&
		lo.Visible == nil &&
		lo.ParallaxX == nil &&
		lo.ParallaxY == nil &&
		lo.RepeatX == nil &&
		lo.RepeatY == nil &&
		lo.RepeatWidth == nil &&
		lo.RepeatHeight == nil &&
		lo.Tilemap == nil
}

func cloneResolvedObjectOverride(ov sceneformat.ObjectOverride) sceneformat.ObjectOverride {
	c := ov
	c.Name = clonePtr(ov.Name)
	c.Prefab = clonePtr(ov.Prefab)
	c.X = cloneFloat32Ptr(ov.X)
	c.Y = cloneFloat32Ptr(ov.Y)
	c.PosZ = cloneFloat32Ptr(ov.PosZ)
	c.OriginX = cloneFloat32Ptr(ov.OriginX)
	c.OriginY = cloneFloat32Ptr(ov.OriginY)
	c.Rotation = cloneFloat32Ptr(ov.Rotation)
	c.RotX = cloneFloat32Ptr(ov.RotX)
	c.RotY = cloneFloat32Ptr(ov.RotY)
	c.RotZ = cloneFloat32Ptr(ov.RotZ)
	c.ScaleX = cloneFloat32Ptr(ov.ScaleX)
	c.ScaleY = cloneFloat32Ptr(ov.ScaleY)
	c.ScaleZ = cloneFloat32Ptr(ov.ScaleZ)
	c.Depth = cloneFloat32Ptr(ov.Depth)
	c.Mesh = clonePtr(ov.Mesh)
	c.Model = clonePtr(ov.Model)
	c.ModelNode = clonePtr(ov.ModelNode)
	c.ModelMesh = clonePtr(ov.ModelMesh)
	c.GridMap = clonePtr(ov.GridMap)
	c.Occluder = cloneOccluderDoc(ov.Occluder)
	c.FlipX = clonePtr(ov.FlipX)
	c.FlipY = clonePtr(ov.FlipY)
	c.Alpha = cloneFloat32Ptr(ov.Alpha)
	c.Z = clonePtr(ov.Z)
	c.Sprite = cloneSprite(ov.Sprite)
	c.Material = clonePtr(ov.Material)
	c.Particles = cloneParticlesRef(ov.Particles)
	c.Light = ov.Light.Clone()
	c.Camera = ov.Camera.Clone()
	c.Components = cloneMap(ov.Components)
	c.Properties = cloneMap(ov.Properties)
	return c
}

func cloneResolvedObjectOverridesMap(ovs map[string]sceneformat.ObjectOverride) map[string]sceneformat.ObjectOverride {
	if ovs == nil {
		return nil
	}
	out := make(map[string]sceneformat.ObjectOverride, len(ovs))
	for k, ov := range ovs {
		out[k] = cloneResolvedObjectOverride(ov)
	}
	return out
}

func cloneLayerOverride(lo sceneformat.LayerOverride) sceneformat.LayerOverride {
	c := lo
	c.Order = clonePtr(lo.Order)
	c.Visible = clonePtr(lo.Visible)
	c.ParallaxX = cloneFloat32Ptr(lo.ParallaxX)
	c.ParallaxY = cloneFloat32Ptr(lo.ParallaxY)
	c.RepeatX = clonePtr(lo.RepeatX)
	c.RepeatY = clonePtr(lo.RepeatY)
	c.RepeatWidth = cloneFloat32Ptr(lo.RepeatWidth)
	c.RepeatHeight = cloneFloat32Ptr(lo.RepeatHeight)
	if lo.Tilemap != nil {
		doc := sceneformat.SceneDoc{Layers: []sceneformat.LayerDoc{{Name: "l", Tilemap: lo.Tilemap}}}
		c.Tilemap = doc.Clone().Layers[0].Tilemap
	}
	return c
}
