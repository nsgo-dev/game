package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolObjectSetField sets a single inspector field, custom property, or component field on an object through Editor methods.
	ToolObjectSetField = "object_set_field"
	// ToolObjectLocateYAML locates an object's exact 1-based line in the open scene YAML and optionally opens it via Editor.OnOpenFileAtLine.
	ToolObjectLocateYAML = "object_locate_yaml"
)

const objectSetFieldSchema = `{"type":"object","additionalProperties":false,"required":["field","value"],"properties":{` +
	`"id":{"type":"string","description":"Object ID to edit; defaults to the currently selected object"},` +
	`"component":{"type":"string","description":"When set, edit this component's field on the object"},` +
	`"property":{"type":"boolean","description":"When true, edit a custom property key on the object"},` +
	`"field":{"type":"string","description":"Object field name (e.g. x, y, rotation, scaleX, z, alpha, sprite, material, model, gridMap, particles, occluder, light, camera), component field name, or custom property key"},` +
	`"value":{"description":"New field value"},` +
	`"label":{"type":"string","description":"Optional undo step label"}` +
	`}}`

const objectLocateYAMLSchema = `{"type":"object","additionalProperties":false,"properties":{` +
	`"id":{"type":"string","description":"Object ID to locate; defaults to the currently selected object (empty returns line 1)"},` +
	`"open":{"type":"boolean","description":"When true, also open the scene file at that line via Editor.OnOpenFileAtLine"}` +
	`}}`

// ObjectFieldResult is returned by object_set_field.
type ObjectFieldResult struct {
	File      string `json:"file"`
	Object    string `json:"object"`
	Component string `json:"component,omitempty"`
	Property  bool   `json:"property,omitempty"`
	Field     string `json:"field"`
	Undo      string `json:"undo,omitempty"`
	Revision  uint64 `json:"revision"`
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
			Description: "Set one inspector field, component field, or custom property on an object (defaulting to the selected object) through the exact same Editor methods as the inspector, recorded as one undo step.",
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
	committed := false
	if !c.onEditor {
		holdTarget := args.ID
		if holdTarget == "" {
			_ = c.Editor(ctx, func(ed *editor.Editor) error {
				holdTarget = ed.SelectedID
				return nil
			})
		}
		if holdTarget != "" {
			rollback, err := c.s.acquireObjectHolds(ctx, c.taskID, []string{holdTarget})
			if err != nil {
				return nil, err
			}
			defer func() {
				if !committed {
					rollback()
				}
			}()
		}
	}
	var res ObjectFieldResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		root := projectRoot(ed)
		name := sceneName(ed, root)
		res.File = name
		if ed.Playing() {
			return &EditError{File: name, Problem: "the scene is playing in the editor, which refuses edits", Fix: "stop playing first"}
		}
		if holdErr := sketchHoldEditError(ed, name); holdErr != nil {
			return holdErr
		}
		targetID := args.ID
		if targetID == "" {
			targetID = ed.SelectedID
		}
		if targetID == "" {
			return &EditError{File: name, Field: args.Field, Problem: "no object id was provided and no object is selected", Fix: "pass id or select an object first"}
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
			switch {
			case args.Property:
				v, err := scalarValue(args.Value)
				if err != nil {
					return &EditError{File: name, Object: targetID, Field: args.Field, Problem: err.Error(), Fix: "give the property a string, finite number, or boolean"}
				}
				if err := ed.SetProperty(targetID, args.Field, v); err != nil {
					return editError(name, 1, editArgs{Op: "set_property", ID: targetID, Key: args.Field}, err)
				}
				return nil
			case args.Component != "":
				if err := setComponentFields(ed, targetID, args.Component, map[string]json.RawMessage{args.Field: args.Value}); err != nil {
					return editError(name, 1, editArgs{Op: "set", ID: targetID, Component: args.Component}, err)
				}
				return nil
			default:
				if err := setObjectFields(ed, targetID, map[string]json.RawMessage{args.Field: args.Value}); err != nil {
					return editError(name, 1, editArgs{Op: "set", ID: targetID}, err)
				}
				return nil
			}
		})
		if err != nil {
			return err
		}
		if ed.SketchApproved() {
			ed.RecordSketchReviewEdits(label, nil, []string{targetID}, nil)
		}
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
		res.Undo = ed.History.UndoLabel()
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
	return &Result{
		Text:       fmt.Sprintf("Updated %s in %s (undo %q).", target, res.File, res.Undo),
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
