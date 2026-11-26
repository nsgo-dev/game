package editor

import (
	"bytes"
	"fmt"
	"math"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
)

// AgentChangeKind classifies one derived change inside an agent turn.
type AgentChangeKind string

const (
	AgentChangeAdded   AgentChangeKind = "added"
	AgentChangeChanged AgentChangeKind = "changed"
	AgentChangeRemoved AgentChangeKind = "removed"
)

// AgentTurnFileKind identifies the structured format of a file touched by an agent turn.
type AgentTurnFileKind string

const (
	TurnFileScene     AgentTurnFileKind = "scene"
	TurnFilePrefab    AgentTurnFileKind = "prefab"
	TurnFileDataTable AgentTurnFileKind = "datatable"
	TurnFileProject   AgentTurnFileKind = "project"
	TurnFileClip      AgentTurnFileKind = "clip"
	TurnFileOther     AgentTurnFileKind = "other"
)

// AgentTurnChange is one derived structural change inside a pending or finished agent turn.
// Changes are derived from the diff between pre-turn and post-turn state rather than a raw command log.
type AgentTurnChange struct {
	// ID uniquely identifies this change within the turn (for example "scenes/level_01.scene.yaml::obj:slime_01").
	ID string `json:"id"`
	// Kind is "added", "changed", or "removed".
	Kind AgentChangeKind `json:"kind"`
	// Title is the short name of the affected object, record, key, or track.
	Title string `json:"title"`
	// Detail is a concise summary of the change (for example "z 10 → 12" or "patrol").
	Detail string `json:"detail,omitempty"`
	// File is the project-relative slash-separated path of the touched file (or open scene label).
	File string `json:"file"`
	// Line is the 1-based line number in the current YAML (or pre-turn YAML for removed items).
	Line int `json:"line,omitempty"`
	// Reason is the agent's optional "why" explanation when supplied on an MCP edit tool.
	Reason string `json:"reason,omitempty"`
	// ObjectID names the scene/prefab object ID when applicable.
	ObjectID string `json:"objectId,omitempty"`
	// Field names the specific object field, component field, property, or key when applicable.
	Field string `json:"field,omitempty"`
	// Layer names the scene layer when applicable.
	Layer string `json:"layer,omitempty"`
	// Reverted reports whether the person reverted this change while the turn is pending.
	Reverted bool `json:"reverted,omitempty"`
}

func classifyTurnFileKind(rel string, proj *project.File) AgentTurnFileKind {
	lower := strings.ToLower(strings.TrimSpace(rel))
	base := path.Base(lower)
	switch {
	case base == project.FileName:
		return TurnFileProject
	case strings.HasSuffix(lower, ".prefab.yaml") || strings.HasSuffix(lower, ".prefab.yml"):
		return TurnFilePrefab
	case strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml"):
		return TurnFileScene
	case strings.HasSuffix(lower, ".anim.yaml") || strings.HasSuffix(lower, ".anim.yml") ||
		strings.HasSuffix(lower, ".clip.yaml") || strings.HasSuffix(lower, ".clip.yml"):
		return TurnFileClip
	case strings.HasPrefix(lower, "data/") && (strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")):
		return TurnFileDataTable
	case proj != nil && isProjectScene(proj, rel):
		return TurnFileScene
	default:
		return TurnFileOther
	}
}

type turnSceneObj struct {
	obj      sceneformat.ObjectDoc
	layer    string
	parentID string
	index    int
}

func flattenTurnSceneObjects(doc *sceneformat.SceneDoc) (map[string]turnSceneObj, []string) {
	byID := make(map[string]turnSceneObj)
	var order []string
	if doc == nil {
		return byID, order
	}
	var walk func(layer, parentID string, objs []sceneformat.ObjectDoc)
	walk = func(layer, parentID string, objs []sceneformat.ObjectDoc) {
		for i := range objs {
			o := objs[i]
			cp := o.Clone()
			// Children are diffed as their own objects by ID; strip nested children on the per-object record.
			cp.Children = nil
			byID[o.ID] = turnSceneObj{
				obj:      cp,
				layer:    layer,
				parentID: parentID,
				index:    i,
			}
			order = append(order, o.ID)
			if len(o.Children) > 0 {
				walk(layer, o.ID, o.Children)
			}
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Name, "", doc.Layers[i].Objects)
	}
	return byID, order
}

func turnFormatFloat(v float32) string {
	if math.Abs(float64(v)-math.Round(float64(v))) < 1e-5 {
		return strconv.FormatInt(int64(math.Round(float64(v))), 10)
	}
	return strconv.FormatFloat(float64(v), 'g', 5, 32)
}

func formatFloatPtr(p *float32, def float32) string {
	if p == nil {
		return turnFormatFloat(def)
	}
	return turnFormatFloat(*p)
}

func formatAnyScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "none"
	case float32:
		return turnFormatFloat(x)
	case float64:
		if math.Abs(x-math.Round(x)) < 1e-6 {
			return strconv.FormatInt(int64(math.Round(x)), 10)
		}
		return strconv.FormatFloat(x, 'g', 6, 64)
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

func formatSpriteRef(s *sceneformat.SpriteRef) string {
	if s == nil {
		return "none"
	}
	if s.Atlas == "" {
		return s.Frame
	}
	return s.Atlas + ":" + s.Frame
}

func formatParticlesRef(p *sceneformat.ParticlesRef) string {
	if p == nil {
		return "none"
	}
	return p.File
}

func addedObjectDetail(o sceneformat.ObjectDoc, layer string) string {
	if len(o.Components) > 0 {
		keys := make([]string, 0, len(o.Components))
		for k := range o.Components {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return strings.Join(keys, ", ")
	}
	if o.Prefab != "" {
		return strings.TrimSuffix(strings.TrimSuffix(path.Base(o.Prefab), ".prefab.yaml"), ".yaml")
	}
	if o.Scene != "" {
		return strings.TrimSuffix(strings.TrimSuffix(path.Base(o.Scene), ".scene.yaml"), ".yaml")
	}
	if o.Sprite != nil && o.Sprite.Frame != "" {
		return o.Sprite.Frame
	}
	if layer != "" {
		return "layer " + layer
	}
	return ""
}

func sceneObjectChangeID(file, objID string) string {
	return file + "::obj:" + objID
}

func sceneFieldChangeID(file, objID, field string) string {
	return file + "::obj:" + objID + "::field:" + field
}

func sceneMetaChangeID(file, key string) string {
	return file + "::scene:" + key
}

// lookupReason finds the most specific recorded reason for a change.
func lookupReason(reasons map[string]string, keys ...string) string {
	if len(reasons) == 0 {
		return ""
	}
	for _, k := range keys {
		if k != "" {
			if r := strings.TrimSpace(reasons[k]); r != "" {
				return r
			}
		}
	}
	return ""
}

// diffTurnSceneDocs structurally diffs beforeDoc and afterDoc per object ID and field.
func diffTurnSceneDocs(file string, beforeDoc, afterDoc *sceneformat.SceneDoc, beforeYAML, afterYAML []byte, reasons map[string]string, reverted map[string]bool) []AgentTurnChange {
	bObjs, bOrder := flattenTurnSceneObjects(beforeDoc)
	aObjs, aOrder := flattenTurnSceneObjects(afterDoc)

	var changes []AgentTurnChange

	// 1. Added objects (in after order).
	for _, id := range aOrder {
		if _, existed := bObjs[id]; existed {
			continue
		}
		ao := aObjs[id]
		cid := sceneObjectChangeID(file, id)
		line := yamlFindObjectLine(afterYAML, id)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeAdded,
			Title:    id,
			Detail:   addedObjectDetail(ao.obj, ao.layer),
			File:     file,
			Line:     line,
			Reason:   lookupReason(reasons, cid, "obj:"+id, "*"),
			ObjectID: id,
			Layer:    ao.layer,
			Reverted: reverted[cid],
		})
	}

	// 2. Changed objects (per field, component field, property, layer, parent).
	for _, id := range aOrder {
		bo, existed := bObjs[id]
		if !existed {
			continue
		}
		ao := aObjs[id]
		addField := func(field, detail string) {
			cid := sceneFieldChangeID(file, id, field)
			line := yamlFindObjectFieldLine(afterYAML, id, field)
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    id,
				Detail:   detail,
				File:     file,
				Line:     line,
				Reason:   lookupReason(reasons, cid, "obj:"+id+"::"+field, "obj:"+id, "*"),
				ObjectID: id,
				Field:    field,
				Layer:    ao.layer,
				Reverted: reverted[cid],
			})
		}

		b, a := bo.obj, ao.obj
		if b.Name != a.Name {
			addField("name", fmt.Sprintf("name %s → %s", formatAnyScalar(b.Name), formatAnyScalar(a.Name)))
		}
		if b.Prefab != a.Prefab {
			addField("prefab", fmt.Sprintf("prefab %s → %s", formatAnyScalar(b.Prefab), formatAnyScalar(a.Prefab)))
		}
		if b.Scene != a.Scene {
			addField("scene", fmt.Sprintf("scene %s → %s", formatAnyScalar(b.Scene), formatAnyScalar(a.Scene)))
		}
		if b.X != a.X {
			addField("x", fmt.Sprintf("x %s → %s", turnFormatFloat(b.X), turnFormatFloat(a.X)))
		}
		if b.Y != a.Y {
			addField("y", fmt.Sprintf("y %s → %s", turnFormatFloat(b.Y), turnFormatFloat(a.Y)))
		}
		if b.Z != a.Z {
			addField("z", fmt.Sprintf("z %d → %d", b.Z, a.Z))
		}
		if b.PosZ != a.PosZ {
			addField("posZ", fmt.Sprintf("posZ %s → %s", turnFormatFloat(b.PosZ), turnFormatFloat(a.PosZ)))
		}
		if b.OriginX != a.OriginX {
			addField("originX", fmt.Sprintf("originX %s → %s", turnFormatFloat(b.OriginX), turnFormatFloat(a.OriginX)))
		}
		if b.OriginY != a.OriginY {
			addField("originY", fmt.Sprintf("originY %s → %s", turnFormatFloat(b.OriginY), turnFormatFloat(a.OriginY)))
		}
		if b.Rotation != a.Rotation {
			addField("rotation", fmt.Sprintf("rotation %s → %s", turnFormatFloat(b.Rotation), turnFormatFloat(a.Rotation)))
		}
		if b.RotX != a.RotX {
			addField("rotX", fmt.Sprintf("rotX %s → %s", turnFormatFloat(b.RotX), turnFormatFloat(a.RotX)))
		}
		if b.RotY != a.RotY {
			addField("rotY", fmt.Sprintf("rotY %s → %s", turnFormatFloat(b.RotY), turnFormatFloat(a.RotY)))
		}
		if b.RotZ != a.RotZ {
			addField("rotZ", fmt.Sprintf("rotZ %s → %s", turnFormatFloat(b.RotZ), turnFormatFloat(a.RotZ)))
		}
		if !floatPtrEqual(b.ScaleX, a.ScaleX, 1) {
			addField("scaleX", fmt.Sprintf("scaleX %s → %s", formatFloatPtr(b.ScaleX, 1), formatFloatPtr(a.ScaleX, 1)))
		}
		if !floatPtrEqual(b.ScaleY, a.ScaleY, 1) {
			addField("scaleY", fmt.Sprintf("scaleY %s → %s", formatFloatPtr(b.ScaleY, 1), formatFloatPtr(a.ScaleY, 1)))
		}
		if !floatPtrEqual(b.ScaleZ, a.ScaleZ, 1) {
			addField("scaleZ", fmt.Sprintf("scaleZ %s → %s", formatFloatPtr(b.ScaleZ, 1), formatFloatPtr(a.ScaleZ, 1)))
		}
		if b.Depth != a.Depth {
			addField("depth", fmt.Sprintf("depth %s → %s", turnFormatFloat(b.Depth), turnFormatFloat(a.Depth)))
		}
		if !floatPtrEqual(b.Alpha, a.Alpha, 1) {
			addField("alpha", fmt.Sprintf("alpha %s → %s", formatFloatPtr(b.Alpha, 1), formatFloatPtr(a.Alpha, 1)))
		}
		if b.FlipX != a.FlipX {
			addField("flipX", fmt.Sprintf("flipX %v → %v", b.FlipX, a.FlipX))
		}
		if b.FlipY != a.FlipY {
			addField("flipY", fmt.Sprintf("flipY %v → %v", b.FlipY, a.FlipY))
		}
		if !reflect.DeepEqual(b.Sprite, a.Sprite) {
			addField("sprite", fmt.Sprintf("sprite %s → %s", formatSpriteRef(b.Sprite), formatSpriteRef(a.Sprite)))
		}
		if b.Material != a.Material {
			addField("material", fmt.Sprintf("material %s → %s", formatAnyScalar(b.Material), formatAnyScalar(a.Material)))
		}
		if b.Mesh != a.Mesh {
			addField("mesh", fmt.Sprintf("mesh %s → %s", formatAnyScalar(b.Mesh), formatAnyScalar(a.Mesh)))
		}
		if b.Model != a.Model {
			addField("model", fmt.Sprintf("model %s → %s", formatAnyScalar(b.Model), formatAnyScalar(a.Model)))
		}
		if b.GridMap != a.GridMap {
			addField("gridMap", fmt.Sprintf("gridMap %s → %s", formatAnyScalar(b.GridMap), formatAnyScalar(a.GridMap)))
		}
		if !reflect.DeepEqual(b.Particles, a.Particles) {
			addField("particles", fmt.Sprintf("particles %s → %s", formatParticlesRef(b.Particles), formatParticlesRef(a.Particles)))
		}
		if !reflect.DeepEqual(b.Occluder, a.Occluder) {
			addField("occluder", "occluder updated")
		}
		if !reflect.DeepEqual(b.Light, a.Light) {
			addField("light", "light updated")
		}
		if !reflect.DeepEqual(b.Camera, a.Camera) {
			addField("camera", "camera updated")
		}
		if bo.layer != ao.layer {
			addField("layer", fmt.Sprintf("layer %s → %s", bo.layer, ao.layer))
		}
		if bo.parentID != ao.parentID {
			bp := bo.parentID
			if bp == "" {
				bp = "root"
			}
			ap := ao.parentID
			if ap == "" {
				ap = "root"
			}
			addField("parent", fmt.Sprintf("parent %s → %s", bp, ap))
		}

		// Components diff (per component or per component field).
		compNames := unionMapKeys(b.Components, a.Components)
		for _, cn := range compNames {
			bv, bHas := b.Components[cn]
			av, aHas := a.Components[cn]
			switch {
			case !bHas && aHas:
				addField("component:"+cn, "+ component "+cn)
			case bHas && !aHas:
				addField("component:"+cn, "− component "+cn)
			case !reflect.DeepEqual(bv, av):
				bm, bIsMap := bv.(map[string]any)
				am, aIsMap := av.(map[string]any)
				if bIsMap && aIsMap {
					fNames := unionMapKeys(bm, am)
					for _, fn := range fNames {
						bfv, bfHas := bm[fn]
						afv, afHas := am[fn]
						if bfHas == afHas && reflect.DeepEqual(bfv, afv) {
							continue
						}
						switch {
						case !bfHas && afHas:
							addField("component:"+cn+"."+fn, fmt.Sprintf("%s.%s = %s", cn, fn, formatAnyScalar(afv)))
						case bfHas && !afHas:
							addField("component:"+cn+"."+fn, fmt.Sprintf("%s.%s removed", cn, fn))
						default:
							addField("component:"+cn+"."+fn, fmt.Sprintf("%s.%s %s → %s", cn, fn, formatAnyScalar(bfv), formatAnyScalar(afv)))
						}
					}
				} else {
					addField("component:"+cn, fmt.Sprintf("%s %s → %s", cn, formatAnyScalar(bv), formatAnyScalar(av)))
				}
			}
		}

		// Properties diff (per key).
		propNames := unionMapKeys(b.Properties, a.Properties)
		for _, pk := range propNames {
			bv, bHas := b.Properties[pk]
			av, aHas := a.Properties[pk]
			if bHas == aHas && reflect.DeepEqual(bv, av) {
				continue
			}
			switch {
			case !bHas && aHas:
				addField("property:"+pk, fmt.Sprintf("%s = %s", pk, formatAnyScalar(av)))
			case bHas && !aHas:
				addField("property:"+pk, fmt.Sprintf("%s removed", pk))
			default:
				addField("property:"+pk, fmt.Sprintf("%s %s → %s", pk, formatAnyScalar(bv), formatAnyScalar(av)))
			}
		}
	}

	// 3. Removed objects (in before order).
	for _, id := range bOrder {
		if _, still := aObjs[id]; still {
			continue
		}
		bo := bObjs[id]
		cid := sceneObjectChangeID(file, id)
		line := yamlFindObjectLine(beforeYAML, id)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeRemoved,
			Title:    id,
			Detail:   "removed",
			File:     file,
			Line:     line,
			Reason:   lookupReason(reasons, cid, "obj:"+id, "*"),
			ObjectID: id,
			Layer:    bo.layer,
			Reverted: reverted[cid],
		})
	}

	// 4. Scene-level metadata (name, size, background, camera, tilemaps) if changed.
	if beforeDoc != nil && afterDoc != nil {
		if beforeDoc.Name != afterDoc.Name {
			cid := sceneMetaChangeID(file, "name")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    path.Base(file),
				Detail:   fmt.Sprintf("name %s → %s", beforeDoc.Name, afterDoc.Name),
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "name"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "name",
				Reverted: reverted[cid],
			})
		}
		if beforeDoc.VirtualWidth != afterDoc.VirtualWidth || beforeDoc.VirtualHeight != afterDoc.VirtualHeight {
			cid := sceneMetaChangeID(file, "virtualSize")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    path.Base(file),
				Detail:   fmt.Sprintf("size %sx%s → %sx%s", turnFormatFloat(beforeDoc.VirtualWidth), turnFormatFloat(beforeDoc.VirtualHeight), turnFormatFloat(afterDoc.VirtualWidth), turnFormatFloat(afterDoc.VirtualHeight)),
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "virtualWidth"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "virtualSize",
				Reverted: reverted[cid],
			})
		}
		if beforeDoc.BackgroundColor != afterDoc.BackgroundColor {
			cid := sceneMetaChangeID(file, "backgroundColor")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    path.Base(file),
				Detail:   fmt.Sprintf("backgroundColor %s → %s", formatAnyScalar(beforeDoc.BackgroundColor), formatAnyScalar(afterDoc.BackgroundColor)),
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "backgroundColor"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "backgroundColor",
				Reverted: reverted[cid],
			})
		}
		if !reflect.DeepEqual(beforeDoc.Camera, afterDoc.Camera) {
			cid := sceneMetaChangeID(file, "camera")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    path.Base(file),
				Detail:   "camera updated",
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "camera"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "camera",
				Reverted: reverted[cid],
			})
		}
		// Check tilemap edits per layer.
		bLayers := make(map[string]*sceneformat.LayerDoc, len(beforeDoc.Layers))
		for i := range beforeDoc.Layers {
			bLayers[beforeDoc.Layers[i].Name] = &beforeDoc.Layers[i]
		}
		for i := range afterDoc.Layers {
			al := &afterDoc.Layers[i]
			bl := bLayers[al.Name]
			if bl == nil {
				continue
			}
			if !reflect.DeepEqual(bl.Tilemap, al.Tilemap) {
				cid := sceneMetaChangeID(file, "tilemap:"+al.Name)
				changes = append(changes, AgentTurnChange{
					ID:       cid,
					Kind:     AgentChangeChanged,
					Title:    al.Name,
					Detail:   "tilemap updated",
					File:     file,
					Line:     yamlFindLayerLine(afterYAML, al.Name),
					Reason:   lookupReason(reasons, cid, "*"),
					Field:    "tilemap",
					Layer:    al.Name,
					Reverted: reverted[cid],
				})
			}
		}
	}

	return changes
}

func floatPtrEqual(a, b *float32, def float32) bool {
	va, vb := def, def
	if a != nil {
		va = *a
	}
	if b != nil {
		vb = *b
	}
	return va == vb
}

func unionMapKeys(a, b map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []string
	for k := range a {
		seen[k] = struct{}{}
		out = append(out, k)
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// synthesizeSceneDoc computes the effective SceneDoc from (beforeDoc, agentDoc, reverted)
// so that reverting or restoring any subset of changes is exact and lossless.
func synthesizeSceneDoc(file string, beforeDoc, agentDoc *sceneformat.SceneDoc, reverted map[string]bool) *sceneformat.SceneDoc {
	if len(reverted) == 0 {
		return agentDoc.Clone()
	}
	// If every derived change is reverted, return exact beforeDoc clone.
	doc := agentDoc.Clone()
	bObjs, bOrder := flattenTurnSceneObjects(beforeDoc)
	aObjs, aOrder := flattenTurnSceneObjects(agentDoc)

	// 1. Revert added objects by deleting them from doc.
	for _, id := range aOrder {
		if _, existed := bObjs[id]; existed {
			continue
		}
		if reverted[sceneObjectChangeID(file, id)] {
			removeObjectFromSceneDoc(doc, id)
		}
	}

	// 2. Revert removed objects by reinserting them from beforeDoc.
	for _, id := range bOrder {
		if _, still := aObjs[id]; still {
			continue
		}
		if reverted[sceneObjectChangeID(file, id)] {
			bo := bObjs[id]
			insertObjectIntoSceneDoc(doc, bo.obj.Clone(), bo.layer, bo.parentID, bo.index)
		}
	}

	// 3. Revert individual changed fields on surviving objects.
	for _, id := range aOrder {
		bo, existed := bObjs[id]
		if !existed {
			continue
		}
		ao := aObjs[id]
		target := findObjectDocByID(doc, id)
		if target == nil {
			continue
		}
		b := bo.obj
		isRev := func(field string) bool {
			return reverted[sceneFieldChangeID(file, id, field)]
		}
		if isRev("name") {
			target.Name = b.Name
		}
		if isRev("prefab") {
			target.Prefab = b.Prefab
		}
		if isRev("scene") {
			target.Scene = b.Scene
		}
		if isRev("x") {
			target.X = b.X
		}
		if isRev("y") {
			target.Y = b.Y
		}
		if isRev("z") {
			target.Z = b.Z
		}
		if isRev("posZ") {
			target.PosZ = b.PosZ
		}
		if isRev("originX") {
			target.OriginX = b.OriginX
		}
		if isRev("originY") {
			target.OriginY = b.OriginY
		}
		if isRev("rotation") {
			target.Rotation = b.Rotation
		}
		if isRev("rotX") {
			target.RotX = b.RotX
		}
		if isRev("rotY") {
			target.RotY = b.RotY
		}
		if isRev("rotZ") {
			target.RotZ = b.RotZ
		}
		if isRev("scaleX") {
			target.ScaleX = cloneFloat32Ptr(b.ScaleX)
		}
		if isRev("scaleY") {
			target.ScaleY = cloneFloat32Ptr(b.ScaleY)
		}
		if isRev("scaleZ") {
			target.ScaleZ = cloneFloat32Ptr(b.ScaleZ)
		}
		if isRev("depth") {
			target.Depth = b.Depth
		}
		if isRev("alpha") {
			target.Alpha = cloneFloat32Ptr(b.Alpha)
		}
		if isRev("flipX") {
			target.FlipX = b.FlipX
		}
		if isRev("flipY") {
			target.FlipY = b.FlipY
		}
		if isRev("sprite") {
			if b.Sprite != nil {
				cp := *b.Sprite
				target.Sprite = &cp
			} else {
				target.Sprite = nil
			}
		}
		if isRev("material") {
			target.Material = b.Material
		}
		if isRev("mesh") {
			target.Mesh = b.Mesh
		}
		if isRev("model") {
			target.Model = b.Model
		}
		if isRev("gridMap") {
			target.GridMap = b.GridMap
		}
		if isRev("particles") {
			if b.Particles != nil {
				cp := *b.Particles
				target.Particles = &cp
			} else {
				target.Particles = nil
			}
		}
		if isRev("occluder") {
			target.Occluder = b.Clone().Occluder
		}
		if isRev("light") {
			target.Light = b.Clone().Light
		}
		if isRev("camera") {
			target.Camera = b.Clone().Camera
		}

		// Revert component / component-field changes.
		for _, cn := range unionMapKeys(b.Components, ao.obj.Components) {
			if isRev("component:" + cn) {
				bv, bHas := b.Components[cn]
				if bHas {
					if target.Components == nil {
						target.Components = make(map[string]any)
					}
					target.Components[cn] = cloneAny(bv)
				} else if target.Components != nil {
					delete(target.Components, cn)
					if len(target.Components) == 0 {
						target.Components = nil
					}
				}
				continue
			}
			bm, bIsMap := b.Components[cn].(map[string]any)
			am, aIsMap := ao.obj.Components[cn].(map[string]any)
			if bIsMap && aIsMap {
				tm, _ := target.Components[cn].(map[string]any)
				for _, fn := range unionMapKeys(bm, am) {
					if !isRev("component:" + cn + "." + fn) {
						continue
					}
					if tm == nil {
						tm = make(map[string]any)
						if target.Components == nil {
							target.Components = make(map[string]any)
						}
						target.Components[cn] = tm
					}
					if bfv, ok := bm[fn]; ok {
						tm[fn] = cloneAny(bfv)
					} else {
						delete(tm, fn)
					}
				}
			}
		}

		// Revert property changes.
		for _, pk := range unionMapKeys(b.Properties, ao.obj.Properties) {
			if !isRev("property:" + pk) {
				continue
			}
			if bv, ok := b.Properties[pk]; ok {
				if target.Properties == nil {
					target.Properties = make(map[string]any)
				}
				target.Properties[pk] = cloneAny(bv)
			} else if target.Properties != nil {
				delete(target.Properties, pk)
				if len(target.Properties) == 0 {
					target.Properties = nil
				}
			}
		}

		// Revert layer / parent moves.
		if isRev("layer") || isRev("parent") {
			wantLayer := ao.layer
			wantParent := ao.parentID
			wantIndex := ao.index
			if isRev("layer") {
				wantLayer = bo.layer
				wantIndex = bo.index
			}
			if isRev("parent") {
				wantParent = bo.parentID
				wantIndex = bo.index
			}
			moved := target.Clone()
			removeObjectFromSceneDoc(doc, id)
			insertObjectIntoSceneDoc(doc, moved, wantLayer, wantParent, wantIndex)
		}
	}

	// 4. Scene-level metadata reverts.
	if beforeDoc != nil {
		if reverted[sceneMetaChangeID(file, "name")] {
			doc.Name = beforeDoc.Name
		}
		if reverted[sceneMetaChangeID(file, "virtualSize")] {
			doc.VirtualWidth = beforeDoc.VirtualWidth
			doc.VirtualHeight = beforeDoc.VirtualHeight
		}
		if reverted[sceneMetaChangeID(file, "backgroundColor")] {
			doc.BackgroundColor = beforeDoc.BackgroundColor
		}
		if reverted[sceneMetaChangeID(file, "camera")] {
			doc.Camera = beforeDoc.Clone().Camera
		}
		beforeClone := beforeDoc.Clone()
		bLayers := make(map[string]*sceneformat.LayerDoc, len(beforeClone.Layers))
		for i := range beforeClone.Layers {
			bLayers[beforeClone.Layers[i].Name] = &beforeClone.Layers[i]
		}
		for i := range doc.Layers {
			al := &doc.Layers[i]
			if reverted[sceneMetaChangeID(file, "tilemap:"+al.Name)] {
				if bl := bLayers[al.Name]; bl != nil {
					al.Tilemap = bl.Tilemap
				}
			}
		}
	}

	return doc
}

// applySceneDelta applies the structural delta (fromDoc → toDoc) onto targetDoc,
// leaving any objects or fields in targetDoc that did not change between fromDoc and toDoc untouched.
// This lets person edits on untouched objects coexist cleanly with agent edits and reverts.
func applySceneDelta(targetDoc, fromDoc, toDoc *sceneformat.SceneDoc) *sceneformat.SceneDoc {
	if targetDoc == nil {
		return toDoc.Clone()
	}
	if fromDoc == nil {
		return toDoc.Clone()
	}
	// Fast path: if targetDoc equals fromDoc, return toDoc.Clone() directly (exact byte fidelity).
	if targetEnc, err1 := sceneformat.Encode(targetDoc); err1 == nil {
		if fromEnc, err2 := sceneformat.Encode(fromDoc); err2 == nil && bytes.Equal(targetEnc, fromEnc) {
			return toDoc.Clone()
		}
	}
	out := targetDoc.Clone()
	changes := diffTurnSceneDocs("", fromDoc, toDoc, nil, nil, nil, nil)
	if len(changes) == 0 {
		return out
	}
	tObjs, _ := flattenTurnSceneObjects(toDoc)
	for _, ch := range changes {
		switch ch.Kind {
		case AgentChangeAdded:
			if toObj, ok := tObjs[ch.ObjectID]; ok {
				if findObjectDocByID(out, ch.ObjectID) == nil {
					insertObjectIntoSceneDoc(out, toObj.obj.Clone(), toObj.layer, toObj.parentID, toObj.index)
				}
			}
		case AgentChangeRemoved:
			removeObjectFromSceneDoc(out, ch.ObjectID)
		case AgentChangeChanged:
			if ch.ObjectID != "" {
				toObj, ok := tObjs[ch.ObjectID]
				if !ok {
					continue
				}
				target := findObjectDocByID(out, ch.ObjectID)
				if target == nil {
					continue
				}
				applySingleObjectField(out, target, toObj, ch.Field)
			} else {
				switch ch.Field {
				case "name":
					out.Name = toDoc.Name
				case "virtualSize":
					out.VirtualWidth, out.VirtualHeight = toDoc.VirtualWidth, toDoc.VirtualHeight
				case "backgroundColor":
					out.BackgroundColor = toDoc.BackgroundColor
				case "camera":
					out.Camera = toDoc.Clone().Camera
				case "tilemap":
					toClone := toDoc.Clone()
					for i := range toClone.Layers {
						if toClone.Layers[i].Name == ch.Layer {
							for j := range out.Layers {
								if out.Layers[j].Name == ch.Layer {
									out.Layers[j].Tilemap = toClone.Layers[i].Tilemap
								}
							}
						}
					}
				}
			}
		}
	}
	return out
}

func applySingleObjectField(doc *sceneformat.SceneDoc, target *sceneformat.ObjectDoc, src turnSceneObj, field string) {
	s := src.obj
	switch {
	case field == "name":
		target.Name = s.Name
	case field == "prefab":
		target.Prefab = s.Prefab
	case field == "scene":
		target.Scene = s.Scene
	case field == "x":
		target.X = s.X
	case field == "y":
		target.Y = s.Y
	case field == "z":
		target.Z = s.Z
	case field == "posZ":
		target.PosZ = s.PosZ
	case field == "originX":
		target.OriginX = s.OriginX
	case field == "originY":
		target.OriginY = s.OriginY
	case field == "rotation":
		target.Rotation = s.Rotation
	case field == "rotX":
		target.RotX = s.RotX
	case field == "rotY":
		target.RotY = s.RotY
	case field == "rotZ":
		target.RotZ = s.RotZ
	case field == "scaleX":
		target.ScaleX = cloneFloat32Ptr(s.ScaleX)
	case field == "scaleY":
		target.ScaleY = cloneFloat32Ptr(s.ScaleY)
	case field == "scaleZ":
		target.ScaleZ = cloneFloat32Ptr(s.ScaleZ)
	case field == "depth":
		target.Depth = s.Depth
	case field == "alpha":
		target.Alpha = cloneFloat32Ptr(s.Alpha)
	case field == "flipX":
		target.FlipX = s.FlipX
	case field == "flipY":
		target.FlipY = s.FlipY
	case field == "sprite":
		if s.Sprite != nil {
			cp := *s.Sprite
			target.Sprite = &cp
		} else {
			target.Sprite = nil
		}
	case field == "material":
		target.Material = s.Material
	case field == "mesh":
		target.Mesh = s.Mesh
	case field == "model":
		target.Model = s.Model
	case field == "gridMap":
		target.GridMap = s.GridMap
	case field == "particles":
		if s.Particles != nil {
			cp := *s.Particles
			target.Particles = &cp
		} else {
			target.Particles = nil
		}
	case field == "occluder":
		target.Occluder = s.Clone().Occluder
	case field == "light":
		target.Light = s.Clone().Light
	case field == "camera":
		target.Camera = s.Clone().Camera
	case field == "layer" || field == "parent":
		moved := target.Clone()
		removeObjectFromSceneDoc(doc, moved.ID)
		insertObjectIntoSceneDoc(doc, moved, src.layer, src.parentID, src.index)
	case strings.HasPrefix(field, "component:"):
		rest := strings.TrimPrefix(field, "component:")
		if cn, fn, hasDot := strings.Cut(rest, "."); hasDot {
			sm, _ := s.Components[cn].(map[string]any)
			tm, _ := target.Components[cn].(map[string]any)
			if sm != nil {
				if val, ok := sm[fn]; ok {
					if target.Components == nil {
						target.Components = make(map[string]any)
					}
					if tm == nil {
						tm = make(map[string]any)
						target.Components[cn] = tm
					}
					tm[fn] = cloneAny(val)
				} else if tm != nil {
					delete(tm, fn)
				}
			}
		} else {
			if val, ok := s.Components[rest]; ok {
				if target.Components == nil {
					target.Components = make(map[string]any)
				}
				target.Components[rest] = cloneAny(val)
			} else if target.Components != nil {
				delete(target.Components, rest)
				if len(target.Components) == 0 {
					target.Components = nil
				}
			}
		}
	case strings.HasPrefix(field, "property:"):
		pk := strings.TrimPrefix(field, "property:")
		if val, ok := s.Properties[pk]; ok {
			if target.Properties == nil {
				target.Properties = make(map[string]any)
			}
			target.Properties[pk] = cloneAny(val)
		} else if target.Properties != nil {
			delete(target.Properties, pk)
			if len(target.Properties) == 0 {
				target.Properties = nil
			}
		}
	}
}

func findObjectDocByID(doc *sceneformat.SceneDoc, id string) *sceneformat.ObjectDoc {
	if doc == nil {
		return nil
	}
	var walk func(objs []sceneformat.ObjectDoc) *sceneformat.ObjectDoc
	walk = func(objs []sceneformat.ObjectDoc) *sceneformat.ObjectDoc {
		for i := range objs {
			if objs[i].ID == id {
				return &objs[i]
			}
			if found := walk(objs[i].Children); found != nil {
				return found
			}
		}
		return nil
	}
	for i := range doc.Layers {
		if found := walk(doc.Layers[i].Objects); found != nil {
			return found
		}
	}
	return nil
}

func removeObjectFromSceneDoc(doc *sceneformat.SceneDoc, id string) bool {
	if doc == nil {
		return false
	}
	var walk func(objs *[]sceneformat.ObjectDoc) bool
	walk = func(objs *[]sceneformat.ObjectDoc) bool {
		list := *objs
		for i := range list {
			if list[i].ID == id {
				*objs = append(list[:i], list[i+1:]...)
				return true
			}
			if walk(&list[i].Children) {
				return true
			}
		}
		return false
	}
	for i := range doc.Layers {
		if walk(&doc.Layers[i].Objects) {
			return true
		}
	}
	return false
}

func insertObjectIntoSceneDoc(doc *sceneformat.SceneDoc, obj sceneformat.ObjectDoc, layer, parentID string, index int) {
	if doc == nil {
		return
	}
	if parentID != "" {
		if p := findObjectDocByID(doc, parentID); p != nil {
			idx := index
			if idx < 0 || idx > len(p.Children) {
				idx = len(p.Children)
			}
			p.Children = slices.Insert(p.Children, idx, obj)
			return
		}
	}
	li := -1
	for i := range doc.Layers {
		if doc.Layers[i].Name == layer {
			li = i
			break
		}
	}
	if li < 0 {
		if len(doc.Layers) == 0 {
			doc.Layers = append(doc.Layers, sceneformat.LayerDoc{Name: layer, Space: "world"})
		}
		li = 0
	}
	objs := doc.Layers[li].Objects
	idx := index
	if idx < 0 || idx > len(objs) {
		idx = len(objs)
	}
	doc.Layers[li].Objects = slices.Insert(objs, idx, obj)
}

// --- Data Table diff and synthesis ---

func rowRecordKey(r datatable.Row, idx int) string {
	for _, k := range []string{"id", "ID", "key", "name"} {
		if v, ok := r.Get(k); ok && strings.TrimSpace(v.Display()) != "" {
			return v.Display()
		}
	}
	if len(r.Cells) > 0 && strings.TrimSpace(r.Cells[0].Value.Display()) != "" {
		return r.Cells[0].Value.Display()
	}
	return fmt.Sprintf("row_%d", idx+1)
}

func rowChangeID(file, key string) string {
	return file + "::row:" + key
}

func diffDataTableDocs(file string, beforeDoc, afterDoc *datatable.Document, beforeYAML, afterYAML []byte, reasons map[string]string, reverted map[string]bool) []AgentTurnChange {
	bRows := make(map[string]datatable.Row)
	var bOrder []string
	if beforeDoc != nil {
		for i, r := range beforeDoc.Rows() {
			k := rowRecordKey(r, i)
			bRows[k] = r
			bOrder = append(bOrder, k)
		}
	}
	aRows := make(map[string]datatable.Row)
	var aOrder []string
	if afterDoc != nil {
		for i, r := range afterDoc.Rows() {
			k := rowRecordKey(r, i)
			aRows[k] = r
			aOrder = append(aOrder, k)
		}
	}
	var changes []AgentTurnChange
	for _, k := range aOrder {
		if _, existed := bRows[k]; existed {
			continue
		}
		cid := rowChangeID(file, k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeAdded,
			Title:    k,
			Detail:   "record added",
			File:     file,
			Line:     yamlFindRowLine(afterYAML, k),
			Reason:   lookupReason(reasons, cid, "row:"+k, "*"),
			Reverted: reverted[cid],
		})
	}
	for _, k := range aOrder {
		br, existed := bRows[k]
		if !existed {
			continue
		}
		ar := aRows[k]
		var diffs []string
		seen := make(map[string]bool)
		for _, c := range ar.Cells {
			seen[c.Key] = true
			bv, ok := br.Get(c.Key)
			if !ok {
				diffs = append(diffs, fmt.Sprintf("%s = %s", c.Key, c.Value.Display()))
			} else if !bv.Equal(c.Value) {
				diffs = append(diffs, fmt.Sprintf("%s %s → %s", c.Key, bv.Display(), c.Value.Display()))
			}
		}
		for _, c := range br.Cells {
			if !seen[c.Key] {
				diffs = append(diffs, fmt.Sprintf("%s removed", c.Key))
			}
		}
		if len(diffs) == 0 {
			continue
		}
		cid := rowChangeID(file, k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeChanged,
			Title:    k,
			Detail:   strings.Join(diffs, ", "),
			File:     file,
			Line:     yamlFindRowLine(afterYAML, k),
			Reason:   lookupReason(reasons, cid, "row:"+k, "*"),
			Reverted: reverted[cid],
		})
	}
	for _, k := range bOrder {
		if _, still := aRows[k]; still {
			continue
		}
		cid := rowChangeID(file, k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeRemoved,
			Title:    k,
			Detail:   "record removed",
			File:     file,
			Line:     yamlFindRowLine(beforeYAML, k),
			Reason:   lookupReason(reasons, cid, "row:"+k, "*"),
			Reverted: reverted[cid],
		})
	}
	return changes
}

func synthesizeDataTableDoc(file string, beforeDoc, agentDoc *datatable.Document, reverted map[string]bool) *datatable.Document {
	if len(reverted) == 0 {
		return agentDoc.Clone()
	}
	bRows := make(map[string]datatable.Row)
	var bOrder []string
	if beforeDoc != nil {
		for i, r := range beforeDoc.Rows() {
			k := rowRecordKey(r, i)
			bRows[k] = r
			bOrder = append(bOrder, k)
		}
	}
	aRows := make(map[string]datatable.Row)
	var aOrder []string
	if agentDoc != nil {
		for i, r := range agentDoc.Rows() {
			k := rowRecordKey(r, i)
			aRows[k] = r
			aOrder = append(aOrder, k)
		}
	}

	out := datatable.NewDocument()
	addedKeys := make(map[string]bool)
	// Preserve before order for surviving or restored rows, then append new non-reverted rows.
	for _, k := range bOrder {
		cid := rowChangeID(file, k)
		ar, inAfter := aRows[k]
		if !inAfter {
			// Removed by agent: include only if reverted.
			if reverted[cid] {
				_, _ = out.AppendRow(bRows[k].Clone().Cells...)
				addedKeys[k] = true
			}
			continue
		}
		if reverted[cid] {
			_, _ = out.AppendRow(bRows[k].Clone().Cells...)
		} else {
			_, _ = out.AppendRow(ar.Clone().Cells...)
		}
		addedKeys[k] = true
	}
	for _, k := range aOrder {
		if addedKeys[k] {
			continue
		}
		if reverted[rowChangeID(file, k)] {
			continue
		}
		_, _ = out.AppendRow(aRows[k].Clone().Cells...)
	}
	return out
}

// --- Project (game.yaml) / Generic YAML key diff and synthesis ---

func flattenYAMLMap(data []byte) (map[string]string, map[string]int, []string) {
	vals := make(map[string]string)
	lines := make(map[string]int)
	var order []string
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || len(root.Content) == 0 {
		return vals, lines, order
	}
	var walk func(prefix string, n *yaml.Node)
	walk = func(prefix string, n *yaml.Node) {
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) > 0 {
				walk(prefix, n.Content[0])
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				kNode := n.Content[i]
				vNode := n.Content[i+1]
				key := kNode.Value
				if prefix != "" {
					key = prefix + "." + key
				}
				if vNode.Kind == yaml.MappingNode && len(vNode.Content) > 0 {
					walk(key, vNode)
				} else {
					enc, _ := yaml.Marshal(vNode)
					s := strings.TrimSpace(string(enc))
					if len(s) > 64 {
						s = strings.Join(strings.Fields(s), " ")
						if len(s) > 64 {
							s = s[:61] + "..."
						}
					}
					vals[key] = s
					lines[key] = kNode.Line
					order = append(order, key)
				}
			}
		}
	}
	walk("", &root)
	return vals, lines, order
}

func yamlKeyChangeID(file, key string) string {
	return file + "::key:" + key
}

func diffYAMLKeys(file string, beforeYAML, afterYAML []byte, reasons map[string]string, reverted map[string]bool) []AgentTurnChange {
	bVals, bLines, bOrder := flattenYAMLMap(beforeYAML)
	aVals, aLines, aOrder := flattenYAMLMap(afterYAML)
	var changes []AgentTurnChange
	for _, k := range aOrder {
		if _, existed := bVals[k]; existed {
			continue
		}
		cid := yamlKeyChangeID(file, k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeAdded,
			Title:    k,
			Detail:   fmt.Sprintf("%s = %s", k, aVals[k]),
			File:     file,
			Line:     aLines[k],
			Reason:   lookupReason(reasons, cid, "key:"+k, "*"),
			Field:    k,
			Reverted: reverted[cid],
		})
	}
	for _, k := range aOrder {
		bv, existed := bVals[k]
		if !existed || bv == aVals[k] {
			continue
		}
		cid := yamlKeyChangeID(file, k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeChanged,
			Title:    k,
			Detail:   fmt.Sprintf("%s %s → %s", k, bv, aVals[k]),
			File:     file,
			Line:     aLines[k],
			Reason:   lookupReason(reasons, cid, "key:"+k, "*"),
			Field:    k,
			Reverted: reverted[cid],
		})
	}
	for _, k := range bOrder {
		if _, still := aVals[k]; still {
			continue
		}
		cid := yamlKeyChangeID(file, k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeRemoved,
			Title:    k,
			Detail:   "removed",
			File:     file,
			Line:     bLines[k],
			Reason:   lookupReason(reasons, cid, "key:"+k, "*"),
			Field:    k,
			Reverted: reverted[cid],
		})
	}
	return changes
}

func synthesizeYAMLByKeys(file string, beforeYAML, agentYAML []byte, reverted map[string]bool) ([]byte, error) {
	if len(reverted) == 0 {
		return append([]byte(nil), agentYAML...), nil
	}
	var bMap, aMap map[string]any
	_ = yaml.Unmarshal(beforeYAML, &bMap)
	_ = yaml.Unmarshal(agentYAML, &aMap)
	if bMap == nil {
		bMap = map[string]any{}
	}
	if aMap == nil {
		aMap = map[string]any{}
	}
	out := cloneAnyMap(aMap)
	bVals, _, bOrder := flattenYAMLMap(beforeYAML)
	aVals, _, aOrder := flattenYAMLMap(agentYAML)

	for _, k := range aOrder {
		if !reverted[yamlKeyChangeID(file, k)] {
			continue
		}
		if _, existed := bVals[k]; !existed {
			deleteNestedKey(out, strings.Split(k, "."))
		} else if val, ok := getNestedKey(bMap, strings.Split(k, ".")); ok {
			setNestedKey(out, strings.Split(k, "."), cloneAny(val))
		}
	}
	for _, k := range bOrder {
		if _, still := aVals[k]; still {
			continue
		}
		if reverted[yamlKeyChangeID(file, k)] {
			if val, ok := getNestedKey(bMap, strings.Split(k, ".")); ok {
				setNestedKey(out, strings.Split(k, "."), cloneAny(val))
			}
		}
	}
	return yaml.Marshal(out)
}

func synthesizeProjectFile(file string, beforeYAML, agentYAML []byte, reverted map[string]bool) (*project.File, []byte, error) {
	raw, err := synthesizeYAMLByKeys(file, beforeYAML, agentYAML, reverted)
	if err != nil {
		return nil, nil, err
	}
	pf, err := project.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	enc, err := project.Encode(pf)
	if err != nil {
		return nil, nil, err
	}
	return pf, enc, nil
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAny(v)
	}
	return out
}

func getNestedKey(m map[string]any, parts []string) (any, bool) {
	cur := m
	for i, p := range parts {
		v, ok := cur[p]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return v, true
		}
		next, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

func setNestedKey(m map[string]any, parts []string, val any) {
	cur := m
	for i := 0; i < len(parts)-1; i++ {
		p := parts[i]
		next, ok := cur[p].(map[string]any)
		if !ok || next == nil {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = val
}

func deleteNestedKey(m map[string]any, parts []string) {
	if len(parts) == 0 || m == nil {
		return
	}
	if len(parts) == 1 {
		delete(m, parts[0])
		return
	}
	next, ok := m[parts[0]].(map[string]any)
	if !ok {
		return
	}
	deleteNestedKey(next, parts[1:])
	if len(next) == 0 {
		delete(m, parts[0])
	}
}

// --- Animation Clip diff and synthesis ---

func clipTrackKey(tr resource.Track) string {
	if tr.Target != "" {
		return tr.Target + "." + tr.Property
	}
	return tr.Property
}

func clipChangeID(file, suffix string) string {
	return file + "::clip:" + suffix
}

func diffClipDocs(file string, beforeClip, afterClip *resource.Animation, beforeYAML, afterYAML []byte, reasons map[string]string, reverted map[string]bool) []AgentTurnChange {
	var changes []AgentTurnChange
	if beforeClip != nil && afterClip != nil {
		if beforeClip.Name != afterClip.Name {
			cid := clipChangeID(file, "name")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    "name",
				Detail:   fmt.Sprintf("name %s → %s", beforeClip.Name, afterClip.Name),
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "name"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "name",
				Reverted: reverted[cid],
			})
		}
		if beforeClip.Length != afterClip.Length {
			cid := clipChangeID(file, "length")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    "length",
				Detail:   fmt.Sprintf("length %s → %s", turnFormatFloat(beforeClip.Length), turnFormatFloat(afterClip.Length)),
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "length"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "length",
				Reverted: reverted[cid],
			})
		}
		if beforeClip.Loop != afterClip.Loop {
			cid := clipChangeID(file, "loop")
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    "loop",
				Detail:   fmt.Sprintf("loop %v → %v", beforeClip.Loop, afterClip.Loop),
				File:     file,
				Line:     yamlFindTopKeyLine(afterYAML, "loop"),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    "loop",
				Reverted: reverted[cid],
			})
		}
	}
	bTracks := make(map[string]resource.Track)
	var bOrder []string
	if beforeClip != nil {
		for _, tr := range beforeClip.Tracks {
			k := clipTrackKey(tr)
			bTracks[k] = tr
			bOrder = append(bOrder, k)
		}
	}
	aTracks := make(map[string]resource.Track)
	var aOrder []string
	if afterClip != nil {
		for _, tr := range afterClip.Tracks {
			k := clipTrackKey(tr)
			aTracks[k] = tr
			aOrder = append(aOrder, k)
		}
	}
	for _, k := range aOrder {
		tr := aTracks[k]
		cid := clipChangeID(file, "track:"+k)
		btr, existed := bTracks[k]
		if !existed {
			n := len(tr.Keys) + len(tr.Frames)
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeAdded,
				Title:    k,
				Detail:   fmt.Sprintf("%d keys", n),
				File:     file,
				Line:     yamlFindTextLine(afterYAML, "property: "+tr.Property),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    k,
				Reverted: reverted[cid],
			})
			continue
		}
		if !reflect.DeepEqual(btr, tr) {
			bn := len(btr.Keys) + len(btr.Frames)
			an := len(tr.Keys) + len(tr.Frames)
			changes = append(changes, AgentTurnChange{
				ID:       cid,
				Kind:     AgentChangeChanged,
				Title:    k,
				Detail:   fmt.Sprintf("%d → %d keys", bn, an),
				File:     file,
				Line:     yamlFindTextLine(afterYAML, "property: "+tr.Property),
				Reason:   lookupReason(reasons, cid, "*"),
				Field:    k,
				Reverted: reverted[cid],
			})
		}
	}
	for _, k := range bOrder {
		if _, still := aTracks[k]; still {
			continue
		}
		btr := bTracks[k]
		cid := clipChangeID(file, "track:"+k)
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeRemoved,
			Title:    k,
			Detail:   "track removed",
			File:     file,
			Line:     yamlFindTextLine(beforeYAML, "property: "+btr.Property),
			Reason:   lookupReason(reasons, cid, "*"),
			Field:    k,
			Reverted: reverted[cid],
		})
	}
	if beforeClip != nil && afterClip != nil && !reflect.DeepEqual(beforeClip.Events, afterClip.Events) {
		cid := clipChangeID(file, "events")
		changes = append(changes, AgentTurnChange{
			ID:       cid,
			Kind:     AgentChangeChanged,
			Title:    "events",
			Detail:   fmt.Sprintf("%d → %d events", len(beforeClip.Events), len(afterClip.Events)),
			File:     file,
			Line:     yamlFindTopKeyLine(afterYAML, "events"),
			Reason:   lookupReason(reasons, cid, "*"),
			Field:    "events",
			Reverted: reverted[cid],
		})
	}
	return changes
}

func synthesizeClipDoc(file string, beforeClip, agentClip *resource.Animation, reverted map[string]bool) *resource.Animation {
	if len(reverted) == 0 {
		return cloneClip(agentClip)
	}
	out := cloneClip(agentClip)
	if beforeClip == nil {
		return out
	}
	if reverted[clipChangeID(file, "name")] {
		out.Name = beforeClip.Name
	}
	if reverted[clipChangeID(file, "length")] {
		out.Length = beforeClip.Length
	}
	if reverted[clipChangeID(file, "loop")] {
		out.Loop = beforeClip.Loop
	}
	if reverted[clipChangeID(file, "events")] {
		out.Events = slices.Clone(beforeClip.Events)
	}
	bTracks := make(map[string]resource.Track)
	var bOrder []string
	for _, tr := range beforeClip.Tracks {
		k := clipTrackKey(tr)
		bTracks[k] = tr
		bOrder = append(bOrder, k)
	}
	aTracks := make(map[string]resource.Track)
	var aOrder []string
	for _, tr := range agentClip.Tracks {
		k := clipTrackKey(tr)
		aTracks[k] = tr
		aOrder = append(aOrder, k)
	}
	var merged []resource.Track
	seen := make(map[string]bool)
	for _, k := range bOrder {
		cid := clipChangeID(file, "track:"+k)
		atr, inAfter := aTracks[k]
		if !inAfter {
			if reverted[cid] {
				merged = append(merged, cloneTrack(bTracks[k]))
				seen[k] = true
			}
			continue
		}
		if reverted[cid] {
			merged = append(merged, cloneTrack(bTracks[k]))
		} else {
			merged = append(merged, cloneTrack(atr))
		}
		seen[k] = true
	}
	for _, k := range aOrder {
		if seen[k] || reverted[clipChangeID(file, "track:"+k)] {
			continue
		}
		merged = append(merged, cloneTrack(aTracks[k]))
	}
	out.Tracks = merged
	return out
}

func cloneTrack(tr resource.Track) resource.Track {
	cp := tr
	if tr.Keys != nil {
		cp.Keys = slices.Clone(tr.Keys)
	}
	if tr.Frames != nil {
		cp.Frames = slices.Clone(tr.Frames)
	}
	return cp
}

// --- YAML line helpers & LCS line counts ---

func yamlFindObjectLine(data []byte, id string) int {
	if len(data) == 0 || id == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if v, ok := strings.CutPrefix(trimmed, "- id: "); ok {
			if unquoteYAMLScalar(v) == id {
				return i + 1
			}
		}
	}
	return 0
}

func yamlFindObjectFieldLine(data []byte, id, field string) int {
	objLine := yamlFindObjectLine(data, id)
	if objLine == 0 || len(data) == 0 {
		return objLine
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	start := objLine - 1
	trimmedStart := strings.TrimLeft(lines[start], " ")
	indent := len(lines[start]) - len(trimmedStart)

	needle := field
	if rest, ok := strings.CutPrefix(field, "component:"); ok {
		if _, fn, dot := strings.Cut(rest, "."); dot {
			needle = fn
		} else {
			needle = rest
		}
	} else if rest, ok := strings.CutPrefix(field, "property:"); ok {
		needle = rest
	}
	keyPrefix := needle + ":"
	quotedKeyPrefix := `"` + needle + `":`

	for i := start + 1; i < len(lines); i++ {
		rest := strings.TrimLeft(lines[i], " ")
		if rest != "" && len(lines[i])-len(rest) <= indent {
			break
		}
		if strings.HasPrefix(rest, keyPrefix) || strings.HasPrefix(rest, quotedKeyPrefix) {
			return i + 1
		}
	}
	return objLine
}

func yamlFindTopKeyLine(data []byte, key string) int {
	if len(data) == 0 || key == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	prefix := key + ":"
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return i + 1
		}
	}
	return 1
}

func yamlFindLayerLine(data []byte, layerName string) int {
	if len(data) == 0 || layerName == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if v, ok := strings.CutPrefix(trimmed, "- name: "); ok && unquoteYAMLScalar(v) == layerName {
			return i + 1
		}
	}
	return 1
}

func yamlFindRowLine(data []byte, recordKey string) int {
	if len(data) == 0 || recordKey == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"- id: ", "id: ", "- ID: ", "ID: ", "- key: ", "key: ", "- name: ", "name: "} {
			if v, ok := strings.CutPrefix(trimmed, prefix); ok && unquoteYAMLScalar(v) == recordKey {
				return i + 1
			}
		}
	}
	return 1
}

func yamlFindTextLine(data []byte, substr string) int {
	if len(data) == 0 || substr == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i + 1
		}
	}
	return 1
}

func unquoteYAMLScalar(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	}
	if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") && len(v) >= 2 {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// lineDiffCounts returns the exact count of added and removed lines between before and after.
func lineDiffCounts(before, after []byte) (added, removed int) {
	if bytes.Equal(before, after) {
		return 0, 0
	}
	split := func(b []byte) []string {
		if len(b) == 0 {
			return nil
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
	a := split(before)
	b := split(after)
	// Trim common prefix and suffix to keep DP tiny.
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a = a[1:]
		b = b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a = a[:len(a)-1]
		b = b[:len(b)-1]
	}
	n, m := len(a), len(b)
	if n == 0 {
		return m, 0
	}
	if m == 0 {
		return 0, n
	}
	// Guard against huge files with a frequency-bag fallback if n*m > 400_000.
	if n*m > 400000 {
		counts := make(map[string]int, n)
		for _, s := range a {
			counts[s]++
		}
		common := 0
		for _, s := range b {
			if counts[s] > 0 {
				counts[s]--
				common++
			}
		}
		return m - common, n - common
	}
	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] >= curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	lcs := prev[m]
	return m - lcs, n - lcs
}
