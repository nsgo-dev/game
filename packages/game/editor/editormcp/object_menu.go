package editormcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// Object context-menu tool names exposed alongside scene_edit and selection_get.
const (
	// ToolObjectCopyYAML serializes an object as canonical sceneformat YAML and copies it to the clipboard.
	ToolObjectCopyYAML = "object_copy_yaml"
	// ToolObjectPasteYAML pastes canonical sceneformat object YAML into the open scene in one undo step.
	ToolObjectPasteYAML = "object_paste_yaml"
	// ToolObjectDuplicate clones an object with fresh unique IDs in one undo step.
	ToolObjectDuplicate = "object_duplicate"
	// ToolObjectGroup wraps one or more objects under a new parent group object in one undo step.
	ToolObjectGroup = "object_group"
	// ToolObjectMoveUnder reparents an object under another object (or lists valid candidate parents) in one undo step.
	ToolObjectMoveUnder = "object_move_under"
	// ToolObjectSelectSamePrefab selects every object in the open scene sharing the same prefab or instanced scene.
	ToolObjectSelectSamePrefab = "object_select_same_prefab"
	// ToolObjectVisibility sets an object's session-only hidden and/or locked state in the viewports.
	ToolObjectVisibility = "object_visibility"
	// ToolObjectFindUsages finds references or project search matches for an object across the open project.
	ToolObjectFindUsages = "object_find_usages"
)

const (
	objectCopyYAMLSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"id":{"type":"string","description":"Object ID to copy as canonical sceneformat YAML; defaults to the selected object"}}}`

	objectPasteYAMLSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"yaml":{"type":"string","description":"Canonical sceneformat YAML to paste; when omitted, pastes the editor's current object clipboard"},` +
		`"layer":{"type":"string","description":"Optional target layer name; defaults to the selected object's layer or active layer"}}}`

	objectDuplicateSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"id":{"type":"string","description":"Object ID to duplicate; defaults to the selected object"}}}`

	objectGroupSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"ids":{"type":"array","items":{"type":"string"},"description":"Object IDs to group under a new parent; defaults to the current selection"},` +
		`"name":{"type":"string","description":"Optional display name for the created group object"}}}`

	objectMoveUnderSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"id":{"type":"string","description":"Object ID to reparent; defaults to the selected object"},` +
		`"parent":{"type":"string","description":"Target parent object ID, or empty string to move to the top level of its layer"},` +
		`"listCandidates":{"type":"boolean","description":"When true, lists valid candidate parents without modifying the scene"}}}`

	objectSelectSamePrefabSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"id":{"type":"string","description":"Object ID whose prefab or instanced scene to match; defaults to the selected object"}}}`

	objectVisibilitySchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"id":{"type":"string","description":"Object ID to update; defaults to the selected object"},` +
		`"hidden":{"type":"boolean","description":"Hide or show the object in the 2D and 3D viewports for this session"},` +
		`"locked":{"type":"boolean","description":"Lock or unlock the object against viewport picking and manipulation for this session"}}}`

	objectFindUsagesSchema = `{"type":"object","additionalProperties":false,"properties":{` +
		`"id":{"type":"string","description":"Object ID whose usages to search across the open project; defaults to the selected object"}}}`
)

func objectMenuTools() []Tool {
	return []Tool{
		{
			Name:        ToolObjectCopyYAML,
			Title:       "Copy object as YAML",
			Description: "Serialize an object in the open scene as canonical sceneformat YAML, copy it to the editor clipboard, and show the copy toast.",
			InputSchema: []byte(objectCopyYAMLSchema),
			Action:      ActionRead,
			Handler:     objectCopyYAML,
		},
		{
			Name:        ToolObjectPasteYAML,
			Title:       "Paste object YAML",
			Description: "Parse canonical sceneformat object YAML (or the editor's current object clipboard) and place a copy with fresh unique IDs as one undo step.",
			InputSchema: []byte(objectPasteYAMLSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     objectPasteYAML,
		},
		{
			Name:        ToolObjectDuplicate,
			Title:       "Duplicate object",
			Description: "Clone an object (and its children) on the same layer with fresh unique IDs as one undo step.",
			InputSchema: []byte(objectDuplicateSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     objectDuplicate,
		},
		{
			Name:        ToolObjectGroup,
			Title:       "Group objects",
			Description: "Create a new parent group object and reparent the given objects (or current selection) under it in one undo step, preserving world transforms.",
			InputSchema: []byte(objectGroupSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     objectGroup,
		},
		{
			Name:        ToolObjectMoveUnder,
			Title:       "Move object under parent",
			Description: "Reparent an object under another object (or to the top level of its layer when parent is empty) in one undo step while preserving world transform, or list valid candidate parents when listCandidates is true.",
			InputSchema: []byte(objectMoveUnderSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     objectMoveUnder,
		},
		{
			Name:        ToolObjectSelectSamePrefab,
			Title:       "Select same prefab",
			Description: "Select all objects in the open scene sharing the same prefab or instanced scene reference as the target object.",
			InputSchema: []byte(objectSelectSamePrefabSchema),
			Action:      ActionRead,
			Handler:     objectSelectSamePrefab,
		},
		{
			Name:        ToolObjectVisibility,
			Title:       "Hide or lock object",
			Description: "Set an object's session-only hidden and/or locked state in the editor viewports without modifying the scene file.",
			InputSchema: []byte(objectVisibilitySchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     objectVisibility,
		},
		{
			Name:        ToolObjectFindUsages,
			Title:       "Find object usages",
			Description: "Find references or project-wide search matches for an object's instanced scene, prefab, or ID in the open project.",
			InputSchema: []byte(objectFindUsagesSchema),
			Action:      ActionRead,
			Handler:     objectFindUsages,
		},
	}
}

// ObjectCopyYAMLResult is returned by object_copy_yaml.
type ObjectCopyYAMLResult struct {
	ID    string `json:"id"`
	YAML  string `json:"yaml"`
	Lines int    `json:"lines"`
	Toast string `json:"toast"`
}

func objectCopyYAML(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_copy_yaml: %w", err)
	}
	var out ObjectCopyYAMLResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		id := args.ID
		if id == "" {
			id = ed.SelectedID
		}
		text, lines, err := ed.CopyObjectYAML(id)
		if err != nil {
			return err
		}
		out = ObjectCopyYAMLResult{
			ID:    id,
			YAML:  text,
			Lines: lines,
			Toast: ed.ToastMessage(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_copy_yaml: %w", err)
	}
	return &Result{
		Text:       out.Toast + "\n" + out.YAML,
		Structured: out,
	}, nil
}

// ObjectPasteYAMLResult is returned by object_paste_yaml.
type ObjectPasteYAMLResult struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

func objectPasteYAML(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		YAML  string `json:"yaml"`
		Layer string `json:"layer"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_paste_yaml: %w", err)
	}
	var out ObjectPasteYAMLResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		var (
			id  string
			err error
		)
		if strings.TrimSpace(args.YAML) != "" {
			id, err = ed.PasteObjectYAML(args.YAML, args.Layer)
		} else {
			id, err = ed.PasteObject()
		}
		if err != nil {
			return err
		}
		out = ObjectPasteYAMLResult{ID: id, Revision: ed.Revision()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_paste_yaml: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Pasted object %s.", out.ID),
		Structured: out,
	}, nil
}

// ObjectDuplicateResult is returned by object_duplicate.
type ObjectDuplicateResult struct {
	SourceID string `json:"sourceId"`
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

func objectDuplicate(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_duplicate: %w", err)
	}
	var out ObjectDuplicateResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		src := args.ID
		if src == "" {
			src = ed.SelectedID
		}
		id, err := ed.DuplicateObject(src)
		if err != nil {
			return err
		}
		out = ObjectDuplicateResult{SourceID: src, ID: id, Revision: ed.Revision()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_duplicate: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Duplicated %s as %s.", out.SourceID, out.ID),
		Structured: out,
	}, nil
}

// ObjectGroupResult is returned by object_group.
type ObjectGroupResult struct {
	GroupID  string   `json:"groupId"`
	Children []string `json:"children"`
	Revision uint64   `json:"revision"`
}

func objectGroup(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		IDs  []string `json:"ids"`
		Name string   `json:"name"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_group: %w", err)
	}
	var out ObjectGroupResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		ids := args.IDs
		if len(ids) == 0 {
			ids = ed.SelectedIDs()
		}
		var (
			gid string
			err error
		)
		if name := strings.TrimSpace(args.Name); name != "" {
			err = ed.History.Group("Group "+name, func() error {
				id, gErr := ed.GroupObjects(ids...)
				if gErr != nil {
					return gErr
				}
				gid = id
				return ed.SetObjectName(gid, name)
			})
		} else {
			gid, err = ed.GroupObjects(ids...)
		}
		if err != nil {
			return err
		}
		out = ObjectGroupResult{GroupID: gid, Children: ids, Revision: ed.Revision()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_group: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Grouped %d object(s) under %s.", len(out.Children), out.GroupID),
		Structured: out,
	}, nil
}

// ObjectMoveUnderResult is returned by object_move_under.
type ObjectMoveUnderResult struct {
	ID         string                      `json:"id"`
	Parent     string                      `json:"parent,omitempty"`
	Candidates []editor.MoveUnderCandidate `json:"candidates,omitempty"`
	Undo       string                      `json:"undo,omitempty"`
	Revision   uint64                      `json:"revision"`
}

func objectMoveUnder(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID             string `json:"id"`
		Parent         string `json:"parent"`
		ListCandidates bool   `json:"listCandidates"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_move_under: %w", err)
	}
	var out ObjectMoveUnderResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		id := args.ID
		if id == "" {
			id = ed.SelectedID
		}
		if args.ListCandidates {
			cands := ed.MoveUnderCandidates(id)
			out = ObjectMoveUnderResult{ID: id, Candidates: cands, Revision: ed.Revision()}
			return nil
		}
		if err := ed.MoveObjectUnder(id, args.Parent); err != nil {
			return err
		}
		out = ObjectMoveUnderResult{
			ID:       id,
			Parent:   args.Parent,
			Undo:     ed.History.UndoLabel(),
			Revision: ed.Revision(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_move_under: %w", err)
	}
	if args.ListCandidates {
		return &Result{
			Text:       fmt.Sprintf("%d candidate parent(s) for %s.", len(out.Candidates), out.ID),
			Structured: out,
		}, nil
	}
	target := out.Parent
	if target == "" {
		target = "layer top level"
	}
	return &Result{
		Text:       fmt.Sprintf("Moved %s under %s (one undo step: %q).", out.ID, target, out.Undo),
		Structured: out,
	}, nil
}

// ObjectSelectSamePrefabResult is returned by object_select_same_prefab.
type ObjectSelectSamePrefabResult struct {
	SelectedIDs []string `json:"selectedIds"`
	Count       int      `json:"count"`
}

func objectSelectSamePrefab(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_select_same_prefab: %w", err)
	}
	var out ObjectSelectSamePrefabResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		ids, err := ed.SelectSamePrefab(args.ID)
		if err != nil {
			return err
		}
		out = ObjectSelectSamePrefabResult{SelectedIDs: ids, Count: len(ids)}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_select_same_prefab: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Selected %d object(s): %s.", out.Count, strings.Join(out.SelectedIDs, ", ")),
		Structured: out,
	}, nil
}

// ObjectVisibilityResult is returned by object_visibility.
type ObjectVisibilityResult struct {
	ID     string `json:"id"`
	Hidden bool   `json:"hidden"`
	Locked bool   `json:"locked"`
}

func objectVisibility(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID     string `json:"id"`
		Hidden *bool  `json:"hidden"`
		Locked *bool  `json:"locked"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_visibility: %w", err)
	}
	var out ObjectVisibilityResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		id := args.ID
		if id == "" {
			id = ed.SelectedID
		}
		if obj, _ := ed.FindResolvedObject(id); obj == nil {
			return fmt.Errorf("object %q not found", id)
		}
		if args.Hidden != nil {
			if err := ed.SetObjectHidden(id, *args.Hidden); err != nil {
				return err
			}
		}
		if args.Locked != nil {
			if err := ed.SetObjectLocked(id, *args.Locked); err != nil {
				return err
			}
		}
		out = ObjectVisibilityResult{
			ID:     id,
			Hidden: ed.ObjectHidden(id),
			Locked: ed.ObjectLocked(id),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_visibility: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Object %s: hidden=%v, locked=%v.", out.ID, out.Hidden, out.Locked),
		Structured: out,
	}, nil
}

func objectFindUsages(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("object_find_usages: %w", err)
	}
	var out editor.ObjectUsagesResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		res, err := ed.ShowObjectUsages(args.ID)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("object_find_usages: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Found %d usage(s) of %s (%s).", out.Count, out.Target, out.Kind),
		Structured: out,
	}, nil
}
