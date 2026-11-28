package editormcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/nsgo-dev/game/pkg/editor"
)

type fieldSpec struct {
	id, component string
	property      bool
	field         string
}

func editFieldSpecs(e editArgs) []fieldSpec {
	id := e.ID
	if e.Op == "add" && id == "" {
		var obj struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(e.Object, &obj)
		id = obj.ID
	}
	var out []fieldSpec
	switch e.Op {
	case "set":
		keys := make([]string, 0, len(e.Fields))
		for key := range e.Fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			out = append(out, fieldSpec{id: id, component: e.Component, field: key})
		}
	case "move":
		out = append(out, fieldSpec{id: id, field: "x"}, fieldSpec{id: id, field: "y"})
	case "set_property", "remove_property":
		out = append(out, fieldSpec{id: id, property: true, field: e.Key})
	case "add_component":
		keys := make([]string, 0, len(e.Fields))
		for key := range e.Fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			out = append(out, fieldSpec{id: id, component: e.Component, field: key})
		}
	}
	return out
}

func captureEditChanges(ed *editor.Editor, e editArgs) []FieldChange {
	specs := editFieldSpecs(e)
	out := make([]FieldChange, 0, len(specs))
	for _, spec := range specs {
		out = append(out, FieldChange{Object: spec.id, Component: spec.component, Property: spec.property, Field: spec.field, Before: editorFieldValue(ed, spec.id, spec.component, spec.property, spec.field)})
	}
	return out
}

func finishEditChanges(ed *editor.Editor, before []FieldChange) []FieldChange {
	for i := range before {
		before[i].After = editorFieldValue(ed, before[i].Object, before[i].Component, before[i].Property, before[i].Field)
		before[i].Changed = !reflect.DeepEqual(before[i].Before, before[i].After)
		info := ed.ObjectOverrideInfo(before[i].Object)
		if info.Origin != editor.ObjectLocal {
			before[i].Override = &OverrideLocation{Origin: info.Origin.String(), Scene: info.SourceScene, Instance: info.InstanceID, Key: info.Key}
		}
	}
	return before
}

func (f fieldSpec) String() string {
	if f.property {
		return fmt.Sprintf("%s.%s", f.id, f.field)
	}
	if f.component != "" {
		return fmt.Sprintf("%s.%s.%s", f.id, f.component, f.field)
	}
	return fmt.Sprintf("%s.%s", f.id, f.field)
}
