package editormcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// sceneEditSchema is scene_edit's input schema.
const sceneEditSchema = `{"type":"object","additionalProperties":false,"required":["edits"],"properties":{` +
	`"file":{"type":"string","description":"The scene or prefab to edit, by its path from the project root; empty names the scene open in the editor (during an active turn, any scene or prefab in the project may be edited in memory)"},` +
	`"label":{"type":"string","description":"What the edits do, as the undo step names it; derived from the edits when absent"},` +
	`"why":{"type":"string","description":"Optional reason explaining why these edits were made, shown alongside derived changes in turn review"},` +
	`"edits":{"type":"array","minItems":1,"maxItems":500,"items":{"type":"object","additionalProperties":false,"required":["op"],"properties":{` +
	`"op":{"type":"string","enum":["add","remove","move","set","add_component","remove_component","set_property","remove_property","reparent","move_to_layer"]},` +
	`"id":{"type":"string","description":"The object the edit changes"},` +
	`"object":{"type":"object","description":"add: the object as the scene file writes it"},` +
	`"layer":{"type":"string","description":"add: the layer to place on, the active layer when absent; move_to_layer: the layer to move to"},` +
	`"parent":{"type":"string","description":"add, reparent: the object to put it under; reparent without it moves the object to the top of its layer"},` +
	`"index":{"type":"integer","description":"reparent, move_to_layer: where among its new siblings, last when absent or out of range"},` +
	`"x":{"type":"number"},"y":{"type":"number"},` +
	`"component":{"type":"string","description":"set, add_component, remove_component: the component's type name"},` +
	`"fields":{"type":"object","description":"set: the fields to change; add_component: field values to set after adding"},` +
	`"key":{"type":"string","description":"set_property, remove_property: the property's key"},` +
	`"value":{"description":"set_property: a string, number or boolean"},` +
	`"why":{"type":"string","description":"Optional reason for this edit, shown in turn review"}` +
	`}}}}}`

// sceneEditArgs are scene_edit's arguments.
type sceneEditArgs struct {
	File  string     `json:"file"`
	Label string     `json:"label"`
	Why   string     `json:"why"`
	Edits []editArgs `json:"edits"`
}

// editArgs are one edit of a scene_edit call.
type editArgs struct {
	Op        string                     `json:"op"`
	ID        string                     `json:"id"`
	Object    json.RawMessage            `json:"object"`
	Layer     string                     `json:"layer"`
	Parent    *string                    `json:"parent"`
	Index     *int                       `json:"index"`
	X         *float64                   `json:"x"`
	Y         *float64                   `json:"y"`
	Component string                     `json:"component"`
	Fields    map[string]json.RawMessage `json:"fields"`
	Key       string                     `json:"key"`
	Value     json.RawMessage            `json:"value"`
	Why       string                     `json:"why"`
}

// EditError is a scene_edit the editor refused. Nothing of the call applied: the open scene, its undo history and its file on disk are as they were.
type EditError struct {
	// File is the scene the call edits, by its path from the project root.
	File string `json:"file"`
	// Edit is the 1-based index of the refused edit among the call's edits, or 0 for the call as a whole.
	Edit int `json:"edit,omitempty"`
	// Op is the refused edit's op.
	Op string `json:"op,omitempty"`
	// Object is the ID of the object the edit names, or empty.
	Object string `json:"object,omitempty"`
	// Component is the component the edit names, or empty.
	Component string `json:"component,omitempty"`
	// Field is the field or property the editor refused, or empty.
	Field string `json:"field,omitempty"`
	// Problem says what is wrong.
	Problem string `json:"problem"`
	// Fix says what to change for the edit to apply.
	Fix string `json:"fix"`
}

// Error formats e as "scene_edit: file: edit N (op) on object "id" component "c" field "f": problem; fix. Nothing was changed."
func (e *EditError) Error() string {
	var b strings.Builder
	b.WriteString("scene_edit: ")
	b.WriteString(e.File)
	if e.Edit > 0 {
		fmt.Fprintf(&b, ": edit %d (%s)", e.Edit, e.Op)
	}
	if e.Object != "" {
		fmt.Fprintf(&b, " on object %q", e.Object)
	}
	if e.Component != "" {
		fmt.Fprintf(&b, " component %q", e.Component)
	}
	if e.Field != "" {
		fmt.Fprintf(&b, " field %q", e.Field)
	}
	b.WriteString(": ")
	b.WriteString(e.Problem)
	if e.Fix != "" {
		b.WriteString("; ")
		b.WriteString(e.Fix)
	}
	b.WriteString(". Nothing was changed.")
	return b.String()
}

// EditResult is what scene_edit reports once its edits applied.
type EditResult struct {
	// File is the scene edited.
	File string `json:"file"`
	// Undo is the undo step's label, which one Undo takes back.
	Undo string `json:"undo"`
	// Objects are the IDs of the objects the edits added, in order.
	Objects []string `json:"added,omitempty"`
	// Revision is the editor's revision after the edits.
	Revision uint64 `json:"revision"`
	// Changes are the fields the edits set, one per field in edit order, each read back from the resolved scene before and after its edit. add, remove, reparent and move_to_layer edits report none.
	Changes []FieldChange `json:"changes,omitempty"`
}

// objectFields are the object fields set changes, in the order replies list them.
var objectFields = []string{"id", "name", "x", "y", "z", "rotation", "scaleX", "scaleY", "alpha", "originX", "originY", "flipX", "flipY", "sprite", "material", "particles", "posZ", "rotX", "rotY", "rotZ", "scaleZ", "depth", "mesh", "model", "gridMap", "occluder", "light", "camera"}

// sceneEdit runs scene_edit.
func sceneEdit(ctx context.Context, c *Call) (*Result, error) {
	var args sceneEditArgs
	if err := c.Decode(&args); err != nil {
		return nil, &EditError{File: "the open scene", Problem: err.Error(), Fix: "pass {\"edits\": [{\"op\": ..., ...}]} with the fields the tool's schema lists"}
	}
	committed := false
	if !c.onEditor {
		targets := targetObjectIDs(args.Edits)
		rollback, err := c.s.acquireObjectHolds(ctx, c.taskID, targets)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !committed {
				rollback()
			}
		}()
	}
	var (
		res    EditResult
		inTurn bool
	)
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		root := projectRoot(ed)
		name := sceneName(ed, root)
		targetFile := ""
		if args.File != "" {
			clean, err := cleanProjectPath(args.File)
			if err != nil {
				return &EditError{File: args.File, Problem: err.Error(), Fix: "name a scene or prefab inside the project, such as " + name}
			}
			if clean != scenePath(ed, root) {
				if !ed.AgentTurnWorking() {
					return &EditError{File: clean, Problem: "scene_edit edits the scene open in the editor, which is " + name, Fix: "open " + clean + " in the editor first, or leave file empty to edit " + name}
				}
				targetFile = clean
				name = clean
			}
		}
		res.File = name
		if len(args.Edits) == 0 {
			return &EditError{File: name, Problem: "no edits", Fix: "list at least one edit in edits"}
		}
		if ed.Playing() {
			return &EditError{File: name, Problem: "the scene is playing in the editor, which refuses edits", Fix: "ask the person to stop playing, then edit"}
		}
		if holdErr := sketchHoldEditError(ed, name); holdErr != nil {
			return holdErr
		}
		if c.taskID != "" {
			c.s.captureTaskBeforeYAML(c.taskID, ed)
		}
		if ed.AgentTurnWorking() {
			inTurn = true
			reasons := buildEditReasons(args.Why, args.Edits)
			return ed.RunAgentSceneEdit(targetFile, reasons, func(target *editor.Editor) error {
				for i, e := range args.Edits {
					before := captureEditChanges(target, e)
					id, err := applyEdit(target, e)
					if err != nil {
						return editError(name, i+1, e, err)
					}
					if id != "" {
						res.Objects = append(res.Objects, id)
						if w := strings.TrimSpace(e.Why); w != "" {
							reasons["obj:"+id] = w
						}
					}
					res.Changes = append(res.Changes, finishEditChanges(target, before)...)
				}
				return nil
			})
		}
		label := args.Label
		if label == "" {
			label = editLabel(args.Edits)
		}
		openedTurn := !ed.AgentTurnPending()
		if openedTurn {
			ed.BeginAgentTurn(label)
			defer ed.DiscardEmptyAgentTurn()
		}
		touched := touchedEditIDs(args.Edits)
		activity := stepActivityLabel(args.Label, args.Edits)
		if !c.s.explicitStep {
			ed.BeginAgentStep(touched, activity)
			defer func() {
				if !c.s.explicitStep {
					ed.EndAgentStep()
				}
			}()
		}
		title := strings.TrimSpace(strings.TrimPrefix(label, "Agent:"))
		if title == "" {
			title = label
		}
		var editedIDs, changedIDs, removedIDs []string
		err := ed.RecordAgentTurn(editor.AgentTurnOptions{
			Title:       title,
			UndoLabel:   label,
			ChangeCount: len(args.Edits),
			KeptCount:   len(args.Edits),
		}, func() error {
			for i, e := range args.Edits {
				before := captureEditChanges(ed, e)
				id, err := applyEdit(ed, e)
				if err != nil {
					return editError(name, i+1, e, err)
				}
				if id != "" {
					res.Objects = append(res.Objects, id)
					editedIDs = append(editedIDs, id)
				} else if e.Op == "remove" && e.ID != "" {
					editedIDs = append(editedIDs, e.ID)
					removedIDs = append(removedIDs, e.ID)
				} else if e.ID != "" {
					editedIDs = append(editedIDs, e.ID)
					changedIDs = append(changedIDs, e.ID)
				}
				res.Changes = append(res.Changes, finishEditChanges(ed, before)...)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(editedIDs) > 0 {
			ed.NoteAgentEditedObjects(editedIDs, []string{scenePath(ed, root)}, label)
		}
		if ed.SketchApproved() {
			ed.RecordSketchReviewEdits(label, res.Objects, changedIDs, removedIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	committed = true
	if c.taskID != "" {
		c.s.recordTaskEdits(c.taskID, res.File, args.Edits, res.Objects)
	}
	err = c.Editor(ctx, func(ed *editor.Editor) error {
		if !inTurn {
			res.Undo = ed.History.UndoLabel()
		}
		res.Revision = ed.Revision()
		return nil
	})
	if err != nil {
		return nil, err
	}
	var text string
	if inTurn {
		if len(args.Edits) == 1 {
			text = fmt.Sprintf("Recorded 1 edit to %s in the active agent turn; it stays unsaved until kept.", res.File)
		} else {
			text = fmt.Sprintf("Recorded %d edits to %s in the active agent turn; they stay unsaved until kept.", len(args.Edits), res.File)
		}
	} else {
		text = fmt.Sprintf("Applied %d edits to %s as one undo step, %q; they are not saved.", len(args.Edits), res.File, res.Undo)
		if len(args.Edits) == 1 {
			text = fmt.Sprintf("Applied 1 edit to %s as one undo step, %q; it is not saved.", res.File, res.Undo)
		}
	}
	if len(res.Objects) > 0 {
		text += " Added " + strings.Join(res.Objects, ", ") + "."
	}
	return &Result{Text: text, Structured: res}, nil
}

func buildEditReasons(topWhy string, edits []editArgs) map[string]string {
	reasons := map[string]string{}
	if w := strings.TrimSpace(topWhy); w != "" {
		reasons["*"] = w
	}
	for _, e := range edits {
		w := strings.TrimSpace(e.Why)
		if w == "" {
			continue
		}
		id := e.ID
		if e.Op == "add" && id == "" {
			var obj struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(e.Object, &obj)
			id = obj.ID
		}
		if id == "" {
			continue
		}
		reasons["obj:"+id] = w
		if e.Op == "set" {
			if e.Component != "" {
				for fn := range e.Fields {
					reasons["obj:"+id+"::component:"+e.Component+"."+fn] = w
				}
			} else {
				for fn := range e.Fields {
					reasons["obj:"+id+"::"+fn] = w
				}
			}
		} else if e.Op == "set_property" || e.Op == "remove_property" {
			reasons["obj:"+id+"::property:"+e.Key] = w
		} else if e.Op == "add_component" || e.Op == "remove_component" {
			reasons["obj:"+id+"::component:"+e.Component] = w
		}
	}
	return reasons
}

// editLabel names the undo step of edits that came without a label.
func editLabel(edits []editArgs) string {
	if len(edits) == 1 {
		e := edits[0]
		target := e.ID
		if e.Op == "add" {
			var obj struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(e.Object, &obj)
			target = obj.ID
		}
		if target != "" {
			return "Agent: " + strings.ReplaceAll(e.Op, "_", " ") + " " + target
		}
		return "Agent: " + strings.ReplaceAll(e.Op, "_", " ")
	}
	return fmt.Sprintf("Agent: %d scene edits", len(edits))
}

// touchedEditIDs returns the unique object IDs named by edits, preserving first-seen order.
func touchedEditIDs(edits []editArgs) []string {
	var out []string
	seen := make(map[string]bool, len(edits))
	addID := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, e := range edits {
		if e.ID != "" {
			addID(e.ID)
		}
		if e.Op == "add" && len(e.Object) > 0 {
			var obj struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(e.Object, &obj)
			addID(obj.ID)
		}
	}
	return out
}

// stepActivityLabel formats a concise present-participle pill label for an active agent step
// (for example "placing slime_02", "moving knight", or "editing knight").
func stepActivityLabel(label string, edits []editArgs) string {
	if s := strings.TrimSpace(label); s != "" {
		return s
	}
	if len(edits) == 1 {
		e := edits[0]
		target := e.ID
		if e.Op == "add" {
			var obj struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			_ = json.Unmarshal(e.Object, &obj)
			if obj.ID != "" {
				target = obj.ID
			} else {
				target = obj.Name
			}
		}
		verb := "editing"
		switch e.Op {
		case "add":
			verb = "placing"
		case "move":
			verb = "moving"
		case "remove":
			verb = "removing"
		case "reparent", "move_to_layer":
			verb = "moving"
		}
		if target != "" {
			return verb + " " + target
		}
		return verb + " scene"
	}
	return fmt.Sprintf("editing %d objects", len(edits))
}

// refusal is an edit's refusal before it reaches an editor method: what is wrong, the field it is about, and the fix.
type refusal struct {
	field, problem, fix string
}

func (r *refusal) Error() string { return r.problem }

// refuse returns a refusal about field.
func refuse(field, problem, fix string) error {
	return &refusal{field: field, problem: problem, fix: fix}
}

// applyEdit applies e through ed's methods and returns the ID of an object it added.
func applyEdit(ed *editor.Editor, e editArgs) (string, error) {
	need := func(name, value string) error {
		if value == "" {
			return refuse("", e.Op+" needs "+name, "add \""+name+"\" to the edit")
		}
		return nil
	}
	if !slices.Contains(editOps, e.Op) {
		return "", refuse("", "unknown op "+quote(e.Op), "use "+opList())
	}
	if e.Op != "add" {
		if err := need("id", e.ID); err != nil {
			return "", err
		}
		if obj, _, _ := ed.FindObject(e.ID); obj == nil {
			return "", refuse("", "the open scene has no object "+quote(e.ID), "use an ID scene_read or selection_get shows")
		}
	}
	switch e.Op {
	case "add":
		return addObject(ed, e)
	case "remove":
		return "", ed.DeleteObject(e.ID)
	case "move":
		if e.X == nil || e.Y == nil {
			return "", refuse("", "move needs x and y", "add \"x\" and \"y\" to the edit")
		}
		return "", ed.MoveObject(e.ID, float32(*e.X), float32(*e.Y))
	case "set":
		if len(e.Fields) == 0 {
			return "", refuse("", "set needs fields", "add \"fields\": {\"name\": value} to the edit")
		}
		if e.Component != "" {
			return "", setComponentFields(ed, e.ID, e.Component, e.Fields)
		}
		return "", setObjectFields(ed, e.ID, e.Fields)
	case "add_component":
		if err := need("component", e.Component); err != nil {
			return "", err
		}
		if err := ed.AddComponent(e.ID, e.Component); err != nil {
			return "", err
		}
		return "", setComponentFields(ed, e.ID, e.Component, e.Fields)
	case "remove_component":
		if err := need("component", e.Component); err != nil {
			return "", err
		}
		if obj, _, _ := ed.FindObject(e.ID); obj != nil {
			if _, ok := obj.Components[e.Component]; !ok {
				return "", refuse("", "the object has no component "+quote(e.Component), "name one of its components: "+listKeys(obj.Components))
			}
		}
		return "", ed.RemoveComponent(e.ID, e.Component)
	case "set_property":
		if err := need("key", e.Key); err != nil {
			return "", err
		}
		v, err := scalarValue(e.Value)
		if err != nil {
			fix := "give the property a string, number or boolean value"
			if errors.Is(err, errNotFinite) {
				fix = "give " + e.Key + " a finite number"
			}
			return "", refuse(e.Key, err.Error(), fix)
		}
		return "", ed.SetProperty(e.ID, e.Key, v)
	case "remove_property":
		if err := need("key", e.Key); err != nil {
			return "", err
		}
		if obj, _, _ := ed.FindObject(e.ID); obj != nil {
			if _, ok := obj.Properties[e.Key]; !ok {
				return "", refuse(e.Key, "the object has no property "+quote(e.Key), "name one of its properties: "+listKeys(obj.Properties))
			}
		}
		return "", ed.DeleteProperty(e.ID, e.Key)
	case "reparent":
		index := -1
		if e.Index != nil {
			index = *e.Index
		}
		parent := ""
		if e.Parent != nil {
			parent = *e.Parent
		}
		return "", ed.ReparentObject(e.ID, parent, index)
	case "move_to_layer":
		if err := need("layer", e.Layer); err != nil {
			return "", err
		}
		index := -1
		if e.Index != nil {
			index = *e.Index
		}
		return "", ed.MoveObjectToLayer(e.ID, e.Layer, index)
	default:
		return "", refuse("", "unknown op "+quote(e.Op), "use "+opList())
	}
}

// editOps are the ops scene_edit applies, in the order its description lists them.
var editOps = []string{"add", "remove", "move", "set", "add_component", "remove_component", "set_property", "remove_property", "reparent", "move_to_layer"}

// opList is editOps as a sentence, "add, remove, … or move_to_layer".
func opList() string {
	return strings.Join(editOps[:len(editOps)-1], ", ") + " or " + editOps[len(editOps)-1]
}

// addObject places e.Object, read through the scene format as the file would hold it, on e.Layer, and under e.Parent when it names one.
func addObject(ed *editor.Editor, e editArgs) (string, error) {
	if len(bytes.TrimSpace(e.Object)) == 0 || string(bytes.TrimSpace(e.Object)) == "null" {
		return "", refuse("", "add needs object", "add \"object\": {\"id\": ..., \"x\": ..., \"y\": ...} written as in the scene file")
	}
	obj, err := parseObject(ed, e.Object)
	if err != nil {
		return "", err
	}
	layer := e.Layer
	if layer != "" && !hasLayer(ed.Doc, layer) {
		return "", refuse("layer", "the open scene has no layer "+quote(layer), "name one of its layers: "+strings.Join(layerNames(ed.Doc), ", "))
	}
	id, err := ed.PlaceObject(layer, obj)
	if err != nil {
		return "", err
	}
	if e.Parent != nil && *e.Parent != "" {
		if err := ed.ReparentObject(id, *e.Parent, -1); err != nil {
			return "", err
		}
	}
	return id, nil
}

// parseObject reads raw, an object as the scene file writes it in JSON, through the scene format's own reader with the editor's component registry, so a field the format does not have, a component the registry does not know and a value of the wrong kind are refused as opening the file would refuse them.
func parseObject(ed *editor.Editor, raw json.RawMessage) (sceneformat.ObjectDoc, error) {
	doc := fmt.Appendf(nil, `{"version": %d, "name": "scene_edit", "layers": [{"name": "scene_edit", "objects": [`, sceneformat.CurrentVersion)
	doc = append(doc, raw...)
	doc = append(doc, "]}]}"...)
	parsed, err := sceneformat.ParseWith(doc, sceneformat.ParseOptions{Components: ed.ComponentRegistry()})
	if err != nil {
		if ce, ok := errors.AsType[*sceneformat.ComponentError](err); ok {
			ce.Line, ce.Column = 0, 0
			return sceneformat.ObjectDoc{}, ce
		}
		if le, ok := errors.AsType[*loaderr.Error](err); ok {
			field := strings.TrimPrefix(le.Field, "layers[0].objects[0]")
			field = strings.TrimPrefix(field, ".")
			if name, ok := strings.CutPrefix(le.Problem, "unknown field "); ok {
				// The decoder names the mapping that holds a field it does not know, not the field.
				field = yamlScalar(strings.Fields(name)[0])
			}
			problem := le.Problem
			if le.Expected != "" {
				problem += "; expected " + le.Expected
			}
			return sceneformat.ObjectDoc{}, refuse(field, problem, "write the object as the scene file does: docs/en/scene.md lists its fields")
		}
		return sceneformat.ObjectDoc{}, refuse("", err.Error(), "write the object as the scene file does: docs/en/scene.md lists its fields")
	}
	objs := parsed.Layers[0].Objects
	if len(objs) != 1 {
		return sceneformat.ObjectDoc{}, refuse("", "object must be one JSON object", "pass a single object")
	}
	return objs[0], nil
}

// setComponentFields sets the fields of the component the object id has, in the order of their names.
func setComponentFields(ed *editor.Editor, id, component string, fields map[string]json.RawMessage) error {
	if len(fields) == 0 {
		return nil
	}
	if obj, _, _ := ed.FindObject(id); obj != nil {
		if _, ok := obj.Components[component]; !ok {
			fix := "add it first with add_component"
			if len(obj.Components) > 0 {
				fix += ", or name one of its components: " + listKeys(obj.Components)
			}
			return refuse("", "the object has no component "+quote(component), fix)
		}
	}
	for _, name := range sortedKeys(fields) {
		v, err := fieldValue(fields[name])
		if err != nil {
			return &sceneformat.ComponentError{Object: id, Component: component, Field: name, Msg: err.Error()}
		}
		if err := ed.SetComponentField(id, component, name, v); err != nil {
			return err
		}
	}
	return nil
}

// setObjectFields sets the object fields of id, in objectFields order, through the editor's setter of each.
func setObjectFields(ed *editor.Editor, id string, fields map[string]json.RawMessage) error {
	for _, name := range sortedKeys(fields) {
		if !slices.Contains(objectFields, name) {
			fix := "set one of " + strings.Join(objectFields, ", ")
			switch name {
			case "components":
				fix = "change a component with set and component, or add one with add_component"
			case "properties":
				fix = "change a property with set_property"
			case "children":
				fix = "add a child with add and parent, or move one with reparent"
			}
			return refuse(name, "set cannot change "+quote(name), fix)
		}
	}
	// obj is the object as the step running now finds it, refetched before each step below.
	var obj *sceneformat.ObjectDoc
	num := func(name string, cur float32) (float32, bool, error) {
		raw, ok := fields[name]
		if !ok {
			return cur, false, nil
		}
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, true, refuse(name, "must be a number", "give "+name+" a finite number")
		}
		return float32(f), true, nil
	}
	str := func(name string) (string, bool, error) {
		raw, ok := fields[name]
		if !ok {
			return "", false, nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", true, refuse(name, "must be a string", "give "+name+" a string")
		}
		return s, true, nil
	}
	boolean := func(name string, cur bool) (bool, bool, error) {
		raw, ok := fields[name]
		if !ok {
			return cur, false, nil
		}
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return false, true, refuse(name, "must be true or false", "give "+name+" a boolean")
		}
		return b, true, nil
	}
	orOne := func(p *float32) float32 {
		if p == nil {
			return 1
		}
		return *p
	}
	// Each step reads the object afresh, as the loop below refetches it, since an earlier step may have replaced or changed it.
	steps := []func() error{
		func() error {
			if s, ok, err := str("name"); err != nil || !ok {
				return err
			} else {
				return wrapField("name", ed.SetObjectName(id, s))
			}
		},
		func() error {
			x, okX, err := num("x", obj.X)
			if err != nil {
				return err
			}
			y, okY, err := num("y", obj.Y)
			if err != nil || !okX && !okY {
				return err
			}
			return wrapField("x", ed.MoveObject(id, x, y))
		},
		func() error {
			raw, ok := fields["z"]
			if !ok {
				return nil
			}
			var z int
			if err := json.Unmarshal(raw, &z); err != nil {
				return refuse("z", "must be a whole number", "give z an integer")
			}
			return wrapField("z", ed.SetObjectZ(id, z))
		},
		func() error {
			r, ok, err := num("rotation", obj.Rotation)
			if err != nil || !ok {
				return err
			}
			return wrapField("rotation", ed.RotateObject(id, r))
		},
		func() error {
			sx, okX, err := num("scaleX", orOne(obj.ScaleX))
			if err != nil {
				return err
			}
			sy, okY, err := num("scaleY", orOne(obj.ScaleY))
			if err != nil || !okX && !okY {
				return err
			}
			return wrapField("scaleX", ed.ScaleObject(id, sx, sy))
		},
		func() error {
			a, ok, err := num("alpha", orOne(obj.Alpha))
			if err != nil || !ok {
				return err
			}
			return wrapField("alpha", ed.SetObjectAlpha(id, a))
		},
		func() error {
			ox, okX, err := num("originX", obj.OriginX)
			if err != nil {
				return err
			}
			oy, okY, err := num("originY", obj.OriginY)
			if err != nil || !okX && !okY {
				return err
			}
			return wrapField("originX", ed.SetObjectOrigin(id, ox, oy))
		},
		func() error {
			fx, okX, err := boolean("flipX", obj.FlipX)
			if err != nil {
				return err
			}
			fy, okY, err := boolean("flipY", obj.FlipY)
			if err != nil || !okX && !okY {
				return err
			}
			return wrapField("flipX", ed.SetObjectFlip(id, fx, fy))
		},
		func() error {
			raw, ok := fields["sprite"]
			if !ok {
				return nil
			}
			var ref *sceneformat.SpriteRef
			if string(bytes.TrimSpace(raw)) != "null" {
				var v struct {
					Atlas string `json:"atlas"`
					Frame string `json:"frame"`
				}
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&v); err != nil {
					return refuse("sprite", "must be {\"atlas\": ..., \"frame\": ...} or null", "give sprite an atlas and a frame")
				}
				ref = &sceneformat.SpriteRef{Atlas: v.Atlas, Frame: v.Frame}
			}
			return wrapField("sprite", ed.SetObjectSprite(id, ref))
		},
		func() error {
			if s, ok, err := str("material"); err != nil || !ok {
				return err
			} else {
				return wrapField("material", ed.SetObjectMaterial(id, s))
			}
		},
		func() error {
			raw, ok := fields["particles"]
			if !ok {
				return nil
			}
			var ref *sceneformat.ParticlesRef
			if string(bytes.TrimSpace(raw)) != "null" {
				var v struct {
					File  string `json:"file"`
					Atlas string `json:"atlas"`
				}
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&v); err != nil {
					return refuse("particles", "must be {\"file\": ..., \"atlas\"?: ...} or null", "give particles a file path or null")
				}
				ref = &sceneformat.ParticlesRef{File: v.File, Atlas: v.Atlas}
			}
			return wrapField("particles", ed.SetObjectParticles(id, ref))
		},
		func() error {
			z, ok, err := num("posZ", obj.PosZ)
			if err != nil || !ok {
				return err
			}
			return wrapField("posZ", ed.MoveObject3D(id, obj.X, obj.Y, z))
		},
		func() error {
			rx, okX, err := num("rotX", obj.RotX)
			if err != nil {
				return err
			}
			ry, okY, err := num("rotY", obj.RotY)
			if err != nil {
				return err
			}
			rz, okZ, err := num("rotZ", obj.RotZ)
			if err != nil || !okX && !okY && !okZ {
				return err
			}
			return wrapField("rotX", ed.RotateObject3D(id, rx, ry, rz))
		},
		func() error {
			sz, ok, err := num("scaleZ", orOne(obj.ScaleZ))
			if err != nil || !ok {
				return err
			}
			return wrapField("scaleZ", ed.ScaleObject3D(id, orOne(obj.ScaleX), orOne(obj.ScaleY), sz))
		},
		func() error {
			d, ok, err := num("depth", obj.Depth)
			if err != nil || !ok {
				return err
			}
			return wrapField("depth", ed.SetObjectDepth(id, d))
		},
		func() error {
			if s, ok, err := str("mesh"); err != nil || !ok {
				return err
			} else {
				return wrapField("mesh", ed.SetObjectMesh(id, s))
			}
		},
		func() error {
			if s, ok, err := str("model"); err != nil || !ok {
				return err
			} else {
				return wrapField("model", ed.SetObjectModel(id, s))
			}
		},
		func() error {
			if s, ok, err := str("gridMap"); err != nil || !ok {
				return err
			} else {
				return wrapField("gridMap", ed.SetObjectGridMap(id, s))
			}
		},
		func() error {
			raw, ok := fields["occluder"]
			if !ok {
				return nil
			}
			var occ *sceneformat.OccluderDoc
			if string(bytes.TrimSpace(raw)) != "null" {
				var v sceneformat.OccluderDoc
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&v); err != nil {
					return refuse("occluder", "must be an occluder object or null", "give occluder {\"mode\": \"model\"|\"box\", ...} or null")
				}
				occ = &v
			}
			return wrapField("occluder", ed.SetObjectOccluder(id, occ))
		},
		func() error {
			raw, ok := fields["light"]
			if !ok {
				return nil
			}
			var l *sceneformat.LightDoc
			if string(bytes.TrimSpace(raw)) != "null" {
				var v sceneformat.LightDoc
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&v); err != nil {
					return refuse("light", "must be a 3D light object or null", "give light {\"kind\": \"point\"|\"directional\"|\"spot\", ...} or null")
				}
				l = &v
			}
			return wrapField("light", ed.SetObjectLight(id, l))
		},
		func() error {
			raw, ok := fields["camera"]
			if !ok {
				return nil
			}
			var cam *sceneformat.Camera3DDoc
			if string(bytes.TrimSpace(raw)) != "null" {
				var v sceneformat.Camera3DDoc
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&v); err != nil {
					return refuse("camera", "must be a 3D camera object or null", "give camera {\"projection\": \"perspective\"|\"orthographic\", ...} or null")
				}
				cam = &v
			}
			return wrapField("camera", ed.SetObjectCamera3D(id, cam))
		},
		// The id changes last, so the steps above find the object by the ID the edit names.
		func() error {
			if s, ok, err := str("id"); err != nil || !ok {
				return err
			} else {
				return wrapField("id", ed.SetObjectID(id, s))
			}
		},
	}
	for _, step := range steps {
		if obj, _, _ = ed.FindObject(id); obj == nil {
			return refuse("", "the open scene has no object "+quote(id), "use an ID scene_read or selection_get shows")
		}
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// fieldError is an editor method's refusal of a change to an object field.
type fieldError struct {
	field string
	err   error
}

func (e *fieldError) Error() string { return e.err.Error() }
func (e *fieldError) Unwrap() error { return e.err }

// wrapField names the field an editor method refused, or returns nil.
func wrapField(field string, err error) error {
	if err == nil {
		return nil
	}
	return &fieldError{field: field, err: err}
}

// editError turns the refusal of edit n into an EditError naming the file, the edit, its object and field and how to fix it.
func editError(file string, n int, e editArgs, err error) *EditError {
	out := &EditError{File: file, Edit: n, Op: e.Op, Object: e.ID, Component: e.Component}
	if e.Op == "add" {
		var obj struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(e.Object, &obj)
		out.Object = obj.ID
	}
	if e.Op == "set_property" || e.Op == "remove_property" {
		out.Field = e.Key
	}
	switch {
	case errors.As(err, new(*refusal)):
		r, _ := errors.AsType[*refusal](err)
		if r.field != "" {
			out.Field = r.field
		}
		out.Problem, out.Fix = r.problem, r.fix
	case errors.As(err, new(*sceneformat.ComponentError)):
		ce, _ := errors.AsType[*sceneformat.ComponentError](err)
		if ce.Object != "" {
			out.Object = ce.Object
		}
		out.Component, out.Field, out.Problem = ce.Component, ce.Field, ce.Msg
		out.Fix = "use a component and fields the project registers, as the message lists them"
	case errors.Is(err, editor.ErrPlaying):
		out.Problem, out.Fix = "the scene is playing in the editor, which refuses edits", "ask the person to stop playing, then edit"
	default:
		if fe, ok := errors.AsType[*fieldError](err); ok {
			out.Field = fe.field
		}
		out.Problem = strings.TrimPrefix(err.Error(), "editor: ")
		out.Fix = "change the edit so the editor accepts it"
	}
	return out
}

// fieldValue decodes a component field's JSON value: a whole number as int, any other number as float64, and strings, booleans, lists and maps as JSON decodes them. It refuses a number out of float64's range, such as 1e400, which would otherwise become an infinity.
func fieldValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("not a JSON value: %v", err)
	}
	return normalizeNumbers(v)
}

// errNotFinite is normalizeNumbers' refusal of a number float64 cannot hold.
var errNotFinite = errors.New("must be a finite number")

// normalizeNumbers replaces each json.Number in v with an int when it is whole and fits, else a float64, and refuses one that is out of float64's range.
func normalizeNumbers(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil && i >= math.MinInt && i <= math.MaxInt {
			return int(i), nil
		}
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%s %w", x, errNotFinite)
		}
		return f, nil
	case []any:
		for i := range x {
			n, err := normalizeNumbers(x[i])
			if err != nil {
				return nil, err
			}
			x[i] = n
		}
		return x, nil
	case map[string]any:
		for k := range x {
			n, err := normalizeNumbers(x[k])
			if err != nil {
				return nil, err
			}
			x[k] = n
		}
		return x, nil
	}
	return v, nil
}

// scalarValue decodes a property's value, which must be a string, a number or a boolean.
func scalarValue(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("set_property needs value")
	}
	v, err := fieldValue(raw)
	if err != nil {
		return nil, err
	}
	switch v.(type) {
	case string, int, float64, bool:
		return v, nil
	}
	return nil, errors.New("a property's value must be a string, number or boolean")
}

// hasLayer reports whether doc has a layer called name.
func hasLayer(doc *sceneformat.SceneDoc, name string) bool {
	return slices.Contains(layerNames(doc), name)
}

// layerNames are doc's layers' names in order.
func layerNames(doc *sceneformat.SceneDoc) []string {
	if doc == nil {
		return nil
	}
	names := make([]string, 0, len(doc.Layers))
	for _, l := range doc.Layers {
		names = append(names, l.Name)
	}
	return names
}

// sortedKeys are m's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// listKeys lists m's keys in order, or says there are none.
func listKeys[V any](m map[string]V) string {
	if len(m) == 0 {
		return "it has none"
	}
	return strings.Join(sortedKeys(m), ", ")
}

// quote is s in double quotes.
func quote(s string) string { return fmt.Sprintf("%q", s) }
