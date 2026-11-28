package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
)

const (
	// ToolObjectSetField sets a single inspector field, custom property, or component field on an object through Editor methods.
	ToolObjectSetField = "object_set_field"
	// ToolObjectLocateYAML locates an object's exact 1-based line in the open scene YAML and optionally opens it via Editor.OnOpenFileAtLine.
	ToolObjectLocateYAML = "object_locate_yaml"
)

const objectSetFieldSchema = `{"type":"object","additionalProperties":false,"required":["field","value"],"properties":{` +
	`"id":{"type":"string","description":"Object ID to edit; inside an agent turn, defaults to the one object the person's request pinned when it was sent (agent_turn_get selectionIds), never the live selection; outside a turn it is required"},` +
	`"component":{"type":"string","description":"When set, edit this component's field on the object"},` +
	`"property":{"type":"boolean","description":"When true, edit a custom property key on the object"},` +
	`"field":{"type":"string","description":"Object field name (e.g. x, y, rotation, scaleX, z, alpha, sprite, material, model, gridMap, particles, occluder, light, camera), component field name, or custom property key"},` +
	`"value":{"description":"New field value"},` +
	`"label":{"type":"string","description":"Optional undo step label"},` +
	`"why":{"type":"string","description":"Optional reason for this edit, shown in turn review"}` +
	`}}`

const objectLocateYAMLSchema = `{"type":"object","additionalProperties":false,"properties":{` +
	`"id":{"type":"string","description":"Object ID to locate; defaults to the currently selected object (empty returns line 1)"},` +
	`"open":{"type":"boolean","description":"When true, also open the scene file at that line via Editor.OnOpenFileAtLine"}` +
	`}}`

// ObjectFieldResult is returned by object_set_field.
type ObjectFieldResult struct {
	File      string      `json:"file"`
	Object    string      `json:"object"`
	Component string      `json:"component,omitempty"`
	Property  bool        `json:"property,omitempty"`
	Field     string      `json:"field"`
	Undo      string      `json:"undo,omitempty"`
	Revision  uint64      `json:"revision"`
	Change    FieldChange `json:"change"`
}

// FieldChange describes the value transition made by an edit tool.
type FieldChange struct {
	Object    string            `json:"object"`
	Component string            `json:"component,omitempty"`
	Property  bool              `json:"property,omitempty"`
	Field     string            `json:"field"`
	Before    any               `json:"before"`
	After     any               `json:"after"`
	Changed   bool              `json:"changed"`
	Override  *OverrideLocation `json:"override,omitempty"`
}

// OverrideLocation identifies where an inherited or instanced edit is stored.
type OverrideLocation struct {
	Origin   string `json:"origin"`
	Scene    string `json:"scene,omitempty"`
	Instance string `json:"instance,omitempty"`
	Key      string `json:"key,omitempty"`
}

// ObjectYAMLLocation is returned by object_locate_yaml.
type ObjectYAMLLocation struct {
	File   string `json:"file"`
	Object string `json:"object,omitempty"`
	Line   int    `json:"line"`
	Opened bool   `json:"opened,omitempty"`
	YAML   string `json:"yaml,omitempty"`
}

func inspectorTools() []Tool {
	return []Tool{
		{
			Name:        ToolObjectSetField,
			Title:       "Set an object inspector field",
			Description: "Set one inspector field, component field, or custom property on an object (id, or inside an agent turn the object its request pinned; the live selection is never a write target) through the exact same Editor methods as the inspector, recorded as one undo step.",
			InputSchema: []byte(objectSetFieldSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     objectSetField,
		},
		{
			Name:        ToolObjectLocateYAML,
			Title:       "Locate an object in scene YAML",
			Description: "Return the 1-based line number and YAML snippet of an object (defaulting to the selected object) in the open scene file, matching the inspector's Open YAML action; set open=true to invoke Editor.OnOpenFileAtLine.",
			InputSchema: []byte(objectLocateYAMLSchema),
			Action:      ActionRead,
			Handler:     objectLocateYAML,
		},
	}
}

func objectSetField(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID        string          `json:"id"`
		Component string          `json:"component"`
		Property  bool            `json:"property"`
		Field     string          `json:"field"`
		Value     json.RawMessage `json:"value"`
		Label     string          `json:"label"`
		Why       string          `json:"why"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_set_field: %w", err)
	}
	if args.Field == "" {
		return nil, errors.New("object_set_field: field must not be empty")
	}
	if args.Component != "" && args.Property {
		return nil, errors.New("object_set_field: component and property cannot both be set")
	}
	resolved, err := c.resolveObjectTarget(ctx, nonEmpty(args.ID), true)
	if err != nil {
		return nil, err
	}
	targetID := resolved.ids[0]
	committed := false
	if !c.onEditor {
		rollback, err := c.s.acquireObjectHolds(ctx, c.taskID, resolved.holdIDs())
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
		res    ObjectFieldResult
		inTurn bool
	)
	err = c.Editor(ctx, func(ed *editor.Editor) error {
		root := projectRoot(ed)
		name := sceneName(ed, root)
		res.File = name
		if ed.Playing() {
			return &EditError{File: name, Problem: "the scene is playing in the editor, which refuses edits", Fix: "stop playing first"}
		}
		if holdErr := sketchHoldEditError(ed, name); holdErr != nil {
			return holdErr
		}
		if err := resolved.missing(ed, name); err != nil {
			return err
		}
		if obj, _, _ := ed.FindObject(targetID); obj == nil {
			return &EditError{File: name, Object: targetID, Field: args.Field, Problem: "the open scene has no object " + quote(targetID), Fix: "use an object ID present in the open scene"}
		}
		if c.taskID != "" {
			c.s.captureTaskBeforeYAML(c.taskID, ed)
		}
		res.Object = targetID
		res.Component = args.Component
		res.Property = args.Property
		res.Field = args.Field
		before := editorFieldValue(ed, targetID, args.Component, args.Property, args.Field)

		label := args.Label
		if label == "" {
			switch {
			case args.Component != "":
				label = fmt.Sprintf("Agent: set %s.%s.%s", targetID, args.Component, args.Field)
			case args.Property:
				label = fmt.Sprintf("Agent: set property %s.%s", targetID, args.Field)
			default:
				label = fmt.Sprintf("Agent: set %s.%s", targetID, args.Field)
			}
		}

		applyFn := func(target *editor.Editor) error {
			switch {
			case args.Property:
				v, err := scalarValue(args.Value)
				if err != nil {
					return &EditError{File: name, Object: targetID, Field: args.Field, Problem: err.Error(), Fix: "give the property a string, finite number, or boolean"}
				}
				if err := target.SetProperty(targetID, args.Field, v); err != nil {
					return editError(name, 1, editArgs{Op: "set_property", ID: targetID, Key: args.Field}, err)
				}
				return nil
			case args.Component != "":
				if err := setComponentFields(target, targetID, args.Component, map[string]json.RawMessage{args.Field: args.Value}); err != nil {
					return editError(name, 1, editArgs{Op: "set", ID: targetID, Component: args.Component}, err)
				}
				return nil
			default:
				if err := setObjectFields(target, targetID, map[string]json.RawMessage{args.Field: args.Value}); err != nil {
					return editError(name, 1, editArgs{Op: "set", ID: targetID}, err)
				}
				return nil
			}
		}

		if ed.AgentTurnWorking() {
			inTurn = true
			var reasons map[string]string
			if w := strings.TrimSpace(args.Why); w != "" {
				reasons = map[string]string{"obj:" + targetID: w}
				switch {
				case args.Property:
					reasons["obj:"+targetID+"::property:"+args.Field] = w
				case args.Component != "":
					reasons["obj:"+targetID+"::component:"+args.Component+"."+args.Field] = w
				default:
					reasons["obj:"+targetID+"::"+args.Field] = w
				}
			}
			return ed.RunAgentSceneEdit("", reasons, applyFn)
		}

		openedTurn := !ed.AgentTurnPending()
		if openedTurn {
			ed.BeginAgentTurn(label)
			defer ed.DiscardEmptyAgentTurn()
		}
		autoStep := !c.s.explicitStep
		if autoStep {
			ed.BeginAgentStep([]string{targetID}, "editing "+targetID)
			defer ed.EndAgentStep()
		}

		err := ed.History.Group(label, func() error {
			return applyFn(ed)
		})
		if err != nil {
			return err
		}
		if ed.SketchApproved() {
			ed.RecordSketchReviewEdits(label, nil, []string{targetID}, nil)
		}
		res.Change = makeFieldChange(ed, targetID, args.Component, args.Property, args.Field, before)
		return nil
	})
	if err != nil {
		return nil, err
	}
	committed = true
	if c.taskID != "" {
		c.s.recordTaskObjectFieldEdit(c.taskID, res.File, res.Object, res.Field)
	}
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if !inTurn {
			res.Undo = ed.History.UndoLabel()
		}
		res.Revision = ed.Revision()
		return nil
	}); err != nil {
		return nil, err
	}
	target := res.Object + "." + res.Field
	if res.Component != "" {
		target = res.Object + "." + res.Component + "." + res.Field
	} else if res.Property {
		target = res.Object + ".properties." + res.Field
	}
	text := fmt.Sprintf("Updated %s in %s (undo %q).", target, res.File, res.Undo)
	if inTurn {
		text = fmt.Sprintf("Recorded update to %s in %s in the active agent turn; it stays unsaved until kept.", target, res.File)
	}
	return &Result{
		Text:       text,
		Structured: res,
	}, nil
}

func objectLocateYAML(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID   string `json:"id"`
		Open bool   `json:"open"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_locate_yaml: %w", err)
	}
	var loc ObjectYAMLLocation
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		targetID := args.ID
		if targetID == "" {
			targetID = ed.SelectedID
		}
		var (
			path   string
			line   int
			opened bool
			err    error
		)
		if args.Open {
			path, line, opened, err = ed.OpenObjectYAML(targetID)
			if err != nil {
				if errors.Is(err, editor.ErrPlaying) {
					root := projectRoot(ed)
					return &EditError{File: sceneName(ed, root), Problem: "the scene is playing in the editor, which refuses opening YAML", Fix: "stop playing first"}
				}
				return fmt.Errorf("object_locate_yaml: %w", err)
			}
		} else {
			path, line, err = ed.FindObjectYAMLLine(targetID)
			if err != nil {
				return fmt.Errorf("object_locate_yaml: %w", err)
			}
		}
		loc.File = path
		loc.Object = targetID
		loc.Line = line
		loc.Opened = opened
		if targetID != "" {
			yamlBytes, _ := ed.ReadSceneYAMLForLocate()
			if len(yamlBytes) == 0 {
				yamlBytes, _ = sceneYAML(ed)
			}
			if _, block := objectBlock(yamlBytes, targetID); block != "" {
				loc.YAML = block
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if loc.Object != "" {
		fmt.Fprintf(&b, "%s:%d (%s)\n", loc.File, loc.Line, loc.Object)
	} else {
		fmt.Fprintf(&b, "%s:%d\n", loc.File, loc.Line)
	}
	if loc.YAML != "" {
		b.WriteString(loc.YAML)
	}
	return &Result{Text: b.String(), Structured: loc}, nil
}

func editorFieldValue(ed *editor.Editor, objectID, component string, property bool, field string) any {
	obj, _, _ := ed.FindObject(objectID)
	if obj == nil {
		return nil
	}
	if property {
		return obj.Properties[field]
	}
	if component != "" {
		if raw, ok := obj.Components[component]; ok {
			if fields, ok := raw.(map[string]any); ok {
				return fields[field]
			}
		}
		return nil
	}
	switch field {
	case "id":
		return obj.ID
	case "name":
		return obj.Name
	case "x":
		return obj.X
	case "y":
		return obj.Y
	case "z":
		return obj.Z
	case "rotation":
		return obj.Rotation
	case "scaleX":
		if obj.ScaleX != nil {
			return *obj.ScaleX
		}
		return nil
	case "scaleY":
		if obj.ScaleY != nil {
			return *obj.ScaleY
		}
		return nil
	case "scaleZ":
		if obj.ScaleZ != nil {
			return *obj.ScaleZ
		}
		return nil
	case "alpha":
		if obj.Alpha != nil {
			return *obj.Alpha
		}
		return nil
	case "originX":
		return obj.OriginX
	case "originY":
		return obj.OriginY
	case "flipX":
		return obj.FlipX
	case "flipY":
		return obj.FlipY
	case "posZ":
		return obj.PosZ
	case "rotX":
		return obj.RotX
	case "rotY":
		return obj.RotY
	case "rotZ":
		return obj.RotZ
	case "depth":
		return obj.Depth
	case "sprite":
		return obj.Sprite
	case "material":
		return obj.Material
	case "model":
		return obj.Model
	case "gridMap":
		return obj.GridMap
	case "occluder":
		return obj.Occluder
	case "light":
		return obj.Light
	case "camera":
		return obj.Camera
	default:
		return nil
	}
}

func makeFieldChange(ed *editor.Editor, objectID, component string, property bool, field string, before any) FieldChange {
	after := editorFieldValue(ed, objectID, component, property, field)
	change := FieldChange{Object: objectID, Component: component, Property: property, Field: field, Before: before, After: after, Changed: !sameJSON(before, after)}
	info := ed.ObjectOverrideInfo(objectID)
	if info.Origin != editor.ObjectLocal {
		change.Override = &OverrideLocation{Origin: info.Origin.String(), Scene: info.SourceScene, Instance: info.InstanceID, Key: info.Key}
	}
	return change
}

func sameJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
