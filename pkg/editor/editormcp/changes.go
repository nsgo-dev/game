package editormcp

import (
	"bytes"
	"encoding/json"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/editor"
)

// fieldSpec names one field an edit touches: an object field, a component field (component set), or a custom property (property true).
type fieldSpec struct {
	id, component string
	property      bool
	field         string
	// renameTo is the object's ID after the edit when the edit sets the object's own id, so the value after the edit is read from the renamed object.
	renameTo string
}

// editFieldSpecs lists the fields a scene_edit edit touches, sorted by field name within the edit. Ops that add, remove or move objects between parents and layers report no field changes.
func editFieldSpecs(e editArgs) []fieldSpec {
	var out []fieldSpec
	switch e.Op {
	case "set", "add_component":
		rename := ""
		if e.Op == "set" && e.Component == "" {
			rename = renamedID(e.Fields["id"])
		}
		for _, key := range sortedKeys(e.Fields) {
			out = append(out, fieldSpec{id: e.ID, component: e.Component, field: key, renameTo: rename})
		}
	case "move":
		out = append(out, fieldSpec{id: e.ID, field: "x"}, fieldSpec{id: e.ID, field: "y"})
	case "set_property", "remove_property":
		out = append(out, fieldSpec{id: e.ID, property: true, field: e.Key})
	}
	return out
}

// renamedID returns the string an edit sets the object's id to, or "" when raw is not a JSON string.
func renamedID(raw json.RawMessage) string {
	var id string
	if len(raw) == 0 || json.Unmarshal(raw, &id) != nil {
		return ""
	}
	return id
}

// pendingChange is a field's value read before an edit, waiting for the value after it.
type pendingChange struct {
	spec   fieldSpec
	before any
}

// captureEditChanges reads the value of every field e touches before e applies.
func captureEditChanges(ed *editor.Editor, e editArgs) []pendingChange {
	specs := editFieldSpecs(e)
	out := make([]pendingChange, 0, len(specs))
	for _, spec := range specs {
		out = append(out, pendingChange{spec: spec, before: editorFieldValue(ed, spec.id, spec.component, spec.property, spec.field)})
	}
	return out
}

// finishEditChanges reads the fields captureEditChanges read again after the edit applied and reports each transition.
func finishEditChanges(ed *editor.Editor, pending []pendingChange) []FieldChange {
	out := make([]FieldChange, 0, len(pending))
	for _, p := range pending {
		out = append(out, makeFieldChange(ed, p.spec, p.before))
	}
	return out
}

// makeFieldChange reads spec's value after an edit and reports it with before, whether the two differ, and where an inherited or instanced object's value was recorded.
func makeFieldChange(ed *editor.Editor, spec fieldSpec, before any) FieldChange {
	id := spec.id
	if spec.renameTo != "" {
		id = spec.renameTo
	}
	after := editorFieldValue(ed, id, spec.component, spec.property, spec.field)
	change := FieldChange{Object: spec.id, Component: spec.component, Property: spec.property, Field: spec.field, Before: before, After: after, Changed: !sameJSON(before, after)}
	if info := ed.ObjectOverrideInfo(id); info.Origin != editor.ObjectLocal {
		change.Override = &OverrideLocation{Origin: info.Origin.String(), Scene: info.SourceScene, Instance: info.InstanceID, Key: info.Key}
	}
	return change
}

// editorFieldValue reads one field of the object objectID in the open scene or, for an instanced or inherited object, the resolved scene as a JSON value, or nil when the object, component or field is absent. Converting the value copies pointer, map and slice values, so a value read before an edit is not changed by the edit.
func editorFieldValue(ed *editor.Editor, objectID, component string, property bool, field string) any {
	obj, _ := ed.FindResolvedObject(objectID)
	if obj == nil {
		return nil
	}
	var v any
	switch {
	case property:
		v = obj.Properties[field]
	case component != "":
		fields, _ := obj.Components[component].(map[string]any)
		v = fields[field]
	default:
		switch field {
		case "id":
			v = obj.ID
		case "name":
			v = obj.Name
		case "x":
			v = obj.X
		case "y":
			v = obj.Y
		case "z":
			v = obj.Z
		case "rotation":
			v = obj.Rotation
		case "scaleX":
			v = obj.ScaleX
		case "scaleY":
			v = obj.ScaleY
		case "scaleZ":
			v = obj.ScaleZ
		case "alpha":
			v = obj.Alpha
		case "originX":
			v = obj.OriginX
		case "originY":
			v = obj.OriginY
		case "flipX":
			v = obj.FlipX
		case "flipY":
			v = obj.FlipY
		case "posZ":
			v = obj.PosZ
		case "rotX":
			v = obj.RotX
		case "rotY":
			v = obj.RotY
		case "rotZ":
			v = obj.RotZ
		case "depth":
			v = obj.Depth
		case "sprite":
			v = obj.Sprite
		case "material":
			v = obj.Material
		case "particles":
			v = obj.Particles
		case "mesh":
			v = obj.Mesh
		case "model":
			v = obj.Model
		case "gridMap":
			v = obj.GridMap
		case "occluder":
			v = obj.Occluder
		case "light":
			v = obj.Light
		case "camera":
			v = obj.Camera
		}
	}
	return jsonValue(v)
}

// jsonValue converts v to the JSON value a client reads: struct fields take their scene-file (YAML) keys, as scene_edit takes them, nil pointers become nil, and numbers become float64.
func jsonValue(v any) any {
	doc, err := yaml.Marshal(v)
	if err != nil {
		return nil
	}
	var plain any
	if err := yaml.Unmarshal(doc, &plain); err != nil {
		return nil
	}
	raw, err := json.Marshal(plain)
	if err != nil {
		return nil
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// sameJSON reports whether a and b encode to the same JSON.
func sameJSON(a, b any) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(left, right)
}
