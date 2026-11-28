package editor

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
	"go.yaml.in/yaml/v3"
)

// maxObjectYAMLBytes caps object YAML payloads pasted from the clipboard or MCP to prevent unbounded allocations.
const maxObjectYAMLBytes = 4 << 20

// AgentPromptRequest describes a prompt draft sent from the editor to an attached agent composer
// (for example "Ask the agent about this" in the object context menu).
type AgentPromptRequest struct {
	ObjectID   string
	ObjectName string
	Layer      string
	ScenePath  string
	Draft      string
}

// MoveUnderCandidate describes one valid target parent in the "Move under" submenu of an object's context menu.
type MoveUnderCandidate struct {
	// ID is the target parent object's ID, or "" for moving to the top level of Layer.
	ID string
	// Name is the target's display name (or ID when Name is empty, or the layer name for a top-level entry).
	Name string
	// Layer is the scene layer containing this target.
	Layer string
	// Note is the secondary hint shown beside the entry (such as "layer Actors", "2 children", "current", or "locked").
	Note string
	// Current reports that this entry is already the object's current parent.
	Current bool
	// Locked reports that this candidate object is locked in the session.
	Locked bool
	// IsLayerRoot reports that choosing this entry moves the object to the top level of Layer.
	IsLayerRoot bool
	// ChildCount is how many direct children the candidate object currently has.
	ChildCount int
	// Depth is the candidate's nesting depth (0 for a layer root, 1 for a top-level object).
	Depth int
}

// ObjectUsagesResult reports project references or search matches found for an object by FindObjectUsages.
type ObjectUsagesResult struct {
	// Target is the scene path, prefab name, or object ID that was searched.
	Target string
	// Kind is "references" when FindReferences ran for a project file path, or "search" when Search ran.
	Kind string
	// References holds matching file references when Kind == "references".
	References []refs.Ref
	// SearchResults holds matching project search hits when Kind == "search".
	SearchResults []SearchResult
	// Count is the total number of usages found.
	Count int
}

type objectContextMenuState struct {
	open            bool
	targetID        string
	renameReqSeq    uint64
	renameReqID     string
	lastAgentPrompt AgentPromptRequest
}

// resolveContextObject returns the object document, its layer name, layer space, and whether it is
// foreign (inherited or from an instanced scene rather than local to e.Doc).
func (e *Editor) resolveContextObject(id string) (obj *sceneformat.ObjectDoc, layerName string, layerSpace sceneformat.Space, isForeign bool) {
	if e == nil || e.Doc == nil || id == "" {
		return nil, "", "", false
	}
	if local, li, _ := e.FindObject(id); local != nil && li >= 0 && li < len(e.Doc.Layers) {
		return local, e.Doc.Layers[li].Name, e.Doc.Layers[li].Space, false
	}
	if resolved, lname := e.FindResolvedObject(id); resolved != nil {
		space := sceneformat.SpaceWorld
		if e.Built != nil && e.Built.Doc != nil {
			for li := range e.Built.Doc.Layers {
				if e.Built.Doc.Layers[li].Name == lname {
					space = e.Built.Doc.Layers[li].Space
					break
				}
			}
		}
		return resolved, lname, space, true
	}
	return nil, "", "", false
}

// contextObjectDisplayName returns the human-readable display name of obj (Name if set, else ID).
func contextObjectDisplayName(obj *sceneformat.ObjectDoc) string {
	if obj == nil {
		return ""
	}
	if obj.Name != "" {
		return obj.Name
	}
	return obj.ID
}

// ObjectKindLabel returns a short human-readable kind label for object id (such as "Sprite",
// "Prefab instance", "Scene instance", "3D Model", "3D Mesh", "Light", "Camera", "Particles", "Group", or "Object").
func (e *Editor) ObjectKindLabel(id string) string {
	if e == nil || e.Doc == nil || id == "" {
		return ""
	}
	obj, _, _, _ := e.resolveContextObject(id)
	if obj == nil {
		return ""
	}
	return e.contextObjectKindLabel(obj)
}

func (e *Editor) contextObjectKindLabel(obj *sceneformat.ObjectDoc) string {
	if obj == nil {
		return ""
	}
	switch {
	case obj.Prefab != "":
		return e.T("contextMenu.kind.prefabInstance")
	case obj.Scene != "":
		return e.T("contextMenu.kind.sceneInstance")
	case obj.Camera != nil:
		return e.T("contextMenu.kind.camera")
	case obj.Light != nil:
		return e.T("contextMenu.kind.light")
	case obj.Particles != nil:
		return e.T("contextMenu.kind.particles")
	case obj.Model != "":
		return e.T("contextMenu.kind.model3d")
	case obj.Mesh != "":
		return e.T("contextMenu.kind.mesh3d")
	case obj.GridMap != "":
		return e.T("contextMenu.kind.gridMap")
	case obj.Sprite != nil:
		return e.T("contextMenu.kind.sprite")
	case len(obj.Children) > 0:
		return e.T("contextMenu.kind.group")
	default:
		return e.T("contextMenu.kind.object")
	}
}

// ObjectContextSubtitle returns the subtitle shown under an object's title in the object context menu,
// such as "Sprite · layer Actors".
func (e *Editor) ObjectContextSubtitle(id string) string {
	if e == nil || e.Doc == nil || id == "" {
		return ""
	}
	obj, layerName, _, _ := e.resolveContextObject(id)
	if obj == nil || layerName == "" {
		return ""
	}
	return e.T("contextMenu.subtitle",
		l10n.Param("kind", e.contextObjectKindLabel(obj)),
		l10n.Param("layer", layerName),
	)
}

// ObjectYAML serializes object id as a canonical single-object sceneformat YAML document that
// sceneformat.Parse parses back to the exact same ObjectDoc. It returns the YAML text and its line count.
func (e *Editor) ObjectYAML(id string) (string, int, error) {
	if e == nil || e.Doc == nil {
		return "", 0, errors.New("editor: no scene open")
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return "", 0, errors.New("editor: no object selected")
	}
	obj, layerName, layerSpace, _ := e.resolveContextObject(id)
	if obj == nil || layerName == "" {
		return "", 0, fmt.Errorf("editor: object %q not found", id)
	}
	ver := e.Doc.Version
	if ver < sceneformat.MinVersion || ver > sceneformat.CurrentVersion {
		ver = sceneformat.CurrentVersion
	}
	snippet := &sceneformat.SceneDoc{
		Version: ver,
		Layers: []sceneformat.LayerDoc{
			{
				Name:    layerName,
				Space:   layerSpace,
				Objects: []sceneformat.ObjectDoc{cloneObjectDoc(*obj)},
			},
		},
	}
	data, err := sceneformat.Encode(snippet)
	if err != nil {
		return "", 0, err
	}
	text := string(data)
	trimmed := strings.TrimSuffix(text, "\n")
	lines := 0
	if trimmed != "" {
		lines = strings.Count(trimmed, "\n") + 1
	}
	return text, lines, nil
}

// CopyObjectYAML serializes object id in canonical sceneformat YAML, writes it to the clipboard
// (via WriteClipboard when set, otherwise the attached scope's system clipboard), caches it for Paste,
// updates Status, and displays the toast "Copied <name> as YAML · N lines".
func (e *Editor) CopyObjectYAML(id string) (string, int, error) {
	if id == "" {
		id = e.SelectedID
	}
	text, lines, err := e.ObjectYAML(id)
	if err != nil {
		return "", 0, err
	}
	obj, _, _, _ := e.resolveContextObject(id)
	name := contextObjectDisplayName(obj)
	e.writeClipboard(text)
	msg := e.T("contextMenu.copiedYAML",
		l10n.Param("name", name),
		l10n.Param("lines", lines),
	)
	e.Status = msg
	e.EmitToastEvent(ToastEvent{
		Kind:    ToastEventCopied,
		ID:      "object-copy-yaml",
		Message: msg,
	})
	return text, lines, nil
}

// CopySelectedYAML copies the currently selected object as canonical sceneformat YAML.
func (e *Editor) CopySelectedYAML() (string, int, error) {
	if e == nil {
		return "", 0, errors.New("editor: nil editor")
	}
	return e.CopyObjectYAML(e.SelectedID)
}

// CanPasteObject reports whether PasteObject has an object YAML payload available from either the
// editor's clipboard cache or an injectable ReadClipboard hook.
func (e *Editor) CanPasteObject() bool {
	if e == nil || e.playing {
		return false
	}
	if e.ReadClipboard != nil {
		var text string
		e.ReadClipboard(func(s string) { text = s })
		if strings.TrimSpace(text) != "" {
			return true
		}
	}
	return strings.TrimSpace(e.clipboardText) != ""
}

// PasteObjectYAML parses yamlText (either a canonical sceneformat SceneDoc as CopyObjectYAML writes,
// or a standalone ObjectDoc mapping) through sceneformat, assigns fresh unique IDs to the object and
// its descendants, and places it on layerName as one undoable step. An empty layerName picks the selected object's layer, else the layer the YAML names when the scene has it (ObjectYAMLLayer), else the active layer; a caller that must not depend on what the person selects passes layerName.
func (e *Editor) PasteObjectYAML(yamlText, layerName string) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: no scene open")
	}
	if e.playing {
		return "", ErrPlaying
	}
	obj, srcLayer, err := e.parsePastedObjectYAML(yamlText)
	if err != nil {
		return "", err
	}

	targetLayer := layerName
	if targetLayer == "" {
		if e.SelectedID != "" {
			if _, li, _ := e.FindObject(e.SelectedID); li >= 0 && li < len(e.Doc.Layers) {
				targetLayer = e.Doc.Layers[li].Name
			}
		}
		if targetLayer == "" && srcLayer != "" && e.layerIndex(srcLayer) >= 0 {
			targetLayer = srcLayer
		}
		if targetLayer == "" {
			targetLayer = e.ActiveLayer
		}
	}

	cloned := cloneObjectDoc(obj)
	e.assignFreshSubtreeIDs(&cloned)
	newID, err := e.PlaceObject(targetLayer, cloned)
	if err != nil {
		return "", err
	}
	e.Status = "Pasted " + newID
	e.notify()
	return newID, nil
}

// PasteObject pastes the object YAML currently on the clipboard (reading from ReadClipboard when set,
// otherwise the cached object clipboard) onto the selected object's layer or the active layer.
func (e *Editor) PasteObject() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	return e.PasteObjectYAML(e.ObjectClipboardText(), "")
}

// ObjectClipboardText returns the object YAML PasteObject pastes: ReadClipboard's text when it is set and not blank, otherwise the cached object clipboard.
func (e *Editor) ObjectClipboardText() string {
	if e == nil {
		return ""
	}
	text := ""
	if e.ReadClipboard != nil {
		e.ReadClipboard(func(clip string) {
			if strings.TrimSpace(clip) != "" {
				text = clip
			}
		})
	}
	if strings.TrimSpace(text) == "" {
		text = e.clipboardText
	}
	return text
}

// ObjectYAMLLayer returns the layer a copied scene YAML (as CopyObjectYAML writes) names for its object when the open scene has a layer of that name, or "" for a bare object mapping or a layer the scene lacks. It refuses yamlText with the error PasteObjectYAML would.
func (e *Editor) ObjectYAMLLayer(yamlText string) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: no scene open")
	}
	_, srcLayer, err := e.parsePastedObjectYAML(yamlText)
	if err != nil {
		return "", err
	}
	if srcLayer == "" || e.layerIndex(srcLayer) < 0 {
		return "", nil
	}
	return srcLayer, nil
}

// parsePastedObjectYAML checks yamlText's size and parses it with parseObjectYAML.
func (e *Editor) parsePastedObjectYAML(yamlText string) (sceneformat.ObjectDoc, string, error) {
	raw := bytes.TrimSpace([]byte(yamlText))
	if len(raw) == 0 {
		return sceneformat.ObjectDoc{}, "", errors.New("editor: clipboard has no object YAML to paste")
	}
	if len(raw) > maxObjectYAMLBytes {
		return sceneformat.ObjectDoc{}, "", fmt.Errorf("editor: pasted object YAML exceeds maximum size (%d bytes)", maxObjectYAMLBytes)
	}
	return e.parseObjectYAML(raw)
}

func (e *Editor) parseObjectYAML(raw []byte) (sceneformat.ObjectDoc, string, error) {
	opts := sceneformat.ParseOptions{
		Components:    e.components,
		PhysicsLayers: e.PhysicsLayers(),
	}
	if doc, err := sceneformat.ParseWith(raw, opts); err == nil {
		for li := range doc.Layers {
			if len(doc.Layers[li].Objects) > 0 {
				return doc.Layers[li].Objects[0], doc.Layers[li].Name, nil
			}
		}
		return sceneformat.ObjectDoc{}, "", errors.New("editor: pasted scene YAML contains no objects")
	} else {
		// Fall back to parsing a bare ObjectDoc mapping by wrapping it inside a minimal SceneDoc
		// so sceneformat still validates every field, component, and unknown key strictly.
		var node yaml.Node
		if uErr := yaml.Unmarshal(raw, &node); uErr != nil || node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
			return sceneformat.ObjectDoc{}, "", err
		}
		item := node.Content[0]
		if item.Kind != yaml.MappingNode {
			return sceneformat.ObjectDoc{}, "", err
		}
		hasID := false
		var bareName string
		for i := 0; i+1 < len(item.Content); i += 2 {
			switch item.Content[i].Value {
			case "id":
				if strings.TrimSpace(item.Content[i+1].Value) != "" {
					hasID = true
				}
			case "name":
				if item.Content[i+1].Kind == yaml.ScalarNode {
					bareName = item.Content[i+1].Value
				}
			}
		}
		if !hasID {
			itemCopy := *item
			itemCopy.Content = append([]*yaml.Node(nil), item.Content...)
			itemCopy.Content = append(itemCopy.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "id"},
				&yaml.Node{Kind: yaml.ScalarNode, Value: sanitizeObjectIDBase(bareName)},
			)
			item = &itemCopy
		}
		wrapped := &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "version"},
				{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", sceneformat.CurrentVersion)},
				{Kind: yaml.ScalarNode, Value: "layers"},
				{
					Kind: yaml.SequenceNode,
					Content: []*yaml.Node{
						{
							Kind: yaml.MappingNode,
							Content: []*yaml.Node{
								{Kind: yaml.ScalarNode, Value: "name"},
								{Kind: yaml.ScalarNode, Value: "paste"},
								{Kind: yaml.ScalarNode, Value: "objects"},
								{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}},
							},
						},
					},
				},
			},
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if eErr := enc.Encode(wrapped); eErr != nil {
			_ = enc.Close()
			return sceneformat.ObjectDoc{}, "", err
		}
		_ = enc.Close()
		doc2, err2 := sceneformat.ParseWith(buf.Bytes(), opts)
		if err2 != nil {
			return sceneformat.ObjectDoc{}, "", err2
		}
		if len(doc2.Layers) == 0 || len(doc2.Layers[0].Objects) == 0 {
			return sceneformat.ObjectDoc{}, "", err
		}
		return doc2.Layers[0].Objects[0], "", nil
	}
}

func sanitizeObjectIDBase(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_-")
	if out == "" {
		return "obj"
	}
	return out
}

func (e *Editor) assignFreshSubtreeIDs(root *sceneformat.ObjectDoc) {
	allocated := make(map[string]struct{})
	var assign func(o *sceneformat.ObjectDoc)
	assign = func(o *sceneformat.ObjectDoc) {
		rawBase := o.ID
		if rawBase == "" {
			rawBase = o.Name
		}
		base := sanitizeObjectIDBase(rawBase)
		candidate := e.generateObjectID(base)
		if _, taken := allocated[candidate]; taken {
			for n := 2; ; n++ {
				try := fmt.Sprintf("%s_%d", base, n)
				if _, used := allocated[try]; !used {
					if obj, _, _ := e.FindObject(try); obj == nil {
						candidate = try
						break
					}
				}
			}
		}
		o.ID = candidate
		allocated[candidate] = struct{}{}
		for i := range o.Children {
			assign(&o.Children[i])
		}
	}
	assign(root)
}

// SelectSamePrefab selects every object in the open scene that shares the same Prefab (or instanced Scene)
// reference as object id (defaulting to SelectedID). Returns an error if the object has neither Prefab nor Scene.
func (e *Editor) SelectSamePrefab(id string) ([]string, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no scene open")
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return nil, errors.New("editor: no object selected")
	}
	obj, _, _, _ := e.resolveContextObject(id)
	if obj == nil {
		return nil, fmt.Errorf("editor: object %q not found", id)
	}
	ref := obj.Prefab
	byScene := false
	if ref == "" && obj.Scene != "" {
		ref = obj.Scene
		byScene = true
	}
	if ref == "" {
		return nil, fmt.Errorf("editor: object %q has no prefab or instanced scene", id)
	}
	searchDoc := e.Doc
	if e.Built != nil && e.Built.Doc != nil {
		searchDoc = e.Built.Doc
	}
	var matched []string
	for li := range searchDoc.Layers {
		sceneformat.WalkObjects(searchDoc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if (!byScene && o.Prefab == ref) || (byScene && o.Scene == ref) {
				matched = append(matched, o.ID)
			}
			return true
		})
	}
	e.SelectObjects(matched)
	selected := e.SelectedIDs()
	e.Status = fmt.Sprintf("Selected %d instances of %s", len(selected), path.Base(ref))
	e.notify()
	return selected, nil
}

// MoveUnderCandidates returns every valid target parent that object id can be moved under via
// MoveObjectUnder / ReparentObject, excluding id itself, its descendants (preventing cycles),
// and any candidate that would exceed sceneformat.MaxObjectDepth. Each entry carries a descriptive
// note (current parent, locked status, child count, or layer).
func (e *Editor) MoveUnderCandidates(id string) []MoveUnderCandidate {
	if e == nil || e.Doc == nil || id == "" {
		return nil
	}
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return nil
	}
	height := subtreeHeight(loc.obj)
	currentParent := ""
	if loc.parent != nil {
		currentParent = loc.parent.ID
	}
	excluded := make(map[string]struct{})
	sceneformat.WalkObjects([]sceneformat.ObjectDoc{*loc.obj}, func(o, _ *sceneformat.ObjectDoc) bool {
		excluded[o.ID] = struct{}{}
		return true
	})

	var out []MoveUnderCandidate
	// If nested under a parent, offer moving back to the top level of its current layer.
	if loc.parent != nil {
		layerName := e.Doc.Layers[loc.layer].Name
		out = append(out, MoveUnderCandidate{
			ID:          "",
			Name:        layerName,
			Layer:       layerName,
			Note:        e.T("contextMenu.topLevelNote"),
			IsLayerRoot: true,
			Depth:       0,
		})
	}
	for li := range e.Doc.Layers {
		layerName := e.Doc.Layers[li].Name
		var walk func(objs []sceneformat.ObjectDoc, depth int)
		walk = func(objs []sceneformat.ObjectDoc, depth int) {
			for i := range objs {
				o := &objs[i]
				if _, skip := excluded[o.ID]; skip {
					continue
				}
				if depth+height <= sceneformat.MaxObjectDepth {
					isCurrent := o.ID == currentParent
					isLocked := e.ObjectLocked(o.ID)
					note := e.moveUnderNote(layerName, len(o.Children), isCurrent, isLocked)
					out = append(out, MoveUnderCandidate{
						ID:         o.ID,
						Name:       contextObjectDisplayName(o),
						Layer:      layerName,
						Note:       note,
						Current:    isCurrent,
						Locked:     isLocked,
						ChildCount: len(o.Children),
						Depth:      depth,
					})
				}
				walk(o.Children, depth+1)
			}
		}
		walk(e.Doc.Layers[li].Objects, 1)
	}
	return out
}

func (e *Editor) moveUnderNote(layer string, childCount int, current, locked bool) string {
	switch {
	case current:
		return e.T("contextMenu.currentParent")
	case locked:
		return e.T("contextMenu.lockedNote")
	case childCount == 1:
		return e.T("contextMenu.oneChildNote", l10n.Param("layer", layer))
	case childCount > 1:
		return e.T("contextMenu.manyChildrenNote", l10n.Param("layer", layer), l10n.Param("count", childCount))
	default:
		return e.T("contextMenu.layerNote", l10n.Param("layer", layer))
	}
}

// MoveObjectUnder reparents object id under parentID (or to the top level of its layer when parentID is "")
// at the end of parentID's children as ONE undoable History step, preserving its world transform.
func (e *Editor) MoveObjectUnder(id, parentID string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no scene open")
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return errors.New("editor: no object selected")
	}
	return e.ReparentObject(id, parentID, -1)
}

// ToggleObjectHidden toggles whether object id is hidden in the 2D and 3D viewports for this session.
func (e *Editor) ToggleObjectHidden(id string) (bool, error) {
	if e == nil {
		return false, errors.New("editor: nil editor")
	}
	if id == "" {
		id = e.SelectedID
	}
	next := !e.ObjectHidden(id)
	if err := e.SetObjectHidden(id, next); err != nil {
		return false, err
	}
	return next, nil
}

// ToggleObjectLocked toggles whether object id is locked against picking and manipulation in the viewports.
func (e *Editor) ToggleObjectLocked(id string) (bool, error) {
	if e == nil {
		return false, errors.New("editor: nil editor")
	}
	if id == "" {
		id = e.SelectedID
	}
	next := !e.ObjectLocked(id)
	if err := e.SetObjectLocked(id, next); err != nil {
		return false, err
	}
	return next, nil
}

// FindObjectUsages searches the open project for usages of object id: references to its instanced Scene
// (when obj.Scene is set), usages of its Prefab (when obj.Prefab is set), or occurrences of its ID.
// Returns ErrNoProject when no project is open.
func (e *Editor) FindObjectUsages(id string) (ObjectUsagesResult, error) {
	if e == nil || e.Doc == nil {
		return ObjectUsagesResult{}, errors.New("editor: no scene open")
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ObjectUsagesResult{}, ErrNoProject
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return ObjectUsagesResult{}, errors.New("editor: no object selected")
	}
	obj, _, _, _ := e.resolveContextObject(id)
	if obj == nil {
		return ObjectUsagesResult{}, fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Scene != "" {
		found, err := e.FindReferences(obj.Scene)
		if err != nil {
			return ObjectUsagesResult{}, err
		}
		return ObjectUsagesResult{
			Target:     obj.Scene,
			Kind:       "references",
			References: found,
			Count:      len(found),
		}, nil
	}
	query := obj.ID
	if obj.Prefab != "" {
		if found, err := e.FindReferences(obj.Prefab); err == nil && len(found) > 0 {
			return ObjectUsagesResult{
				Target:     obj.Prefab,
				Kind:       "references",
				References: found,
				Count:      len(found),
			}, nil
		}
		query = obj.Prefab
	}
	hits, err := e.Search(query)
	if err != nil {
		return ObjectUsagesResult{}, err
	}
	return ObjectUsagesResult{
		Target:        query,
		Kind:          "search",
		SearchResults: hits,
		Count:         len(hits),
	}, nil
}

// ShowObjectUsages runs FindObjectUsages for id and opens the Find Usages review screen (for scene or
// path-valued prefab targets) or the Project Search & References panel with the results.
func (e *Editor) ShowObjectUsages(id string) (ObjectUsagesResult, error) {
	res, err := e.FindObjectUsages(id)
	if err != nil {
		return ObjectUsagesResult{}, err
	}
	if res.Kind == "references" {
		if !e.Playing() {
			if err := e.OpenUsagesScreen(res.Target); err == nil {
				return res, nil
			}
		}
		_, err = e.ShowReferences(res.Target)
		return res, err
	}
	err = e.SetSearchQuery(res.Target)
	return res, err
}

// RequestRenameObject selects object id and asks the Scene Tree panel to start inline renaming of its row.
func (e *Editor) RequestRenameObject(id string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no scene open")
	}
	if e.playing {
		return ErrPlaying
	}
	if id == "" {
		id = e.SelectedID
	}
	if obj, _, _ := e.FindObject(id); obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	e.SelectObject(id)
	e.contextMenu.renameReqID = id
	e.contextMenu.renameReqSeq++
	e.notify()
	return nil
}

// AskAgentAboutObject builds an AgentPromptRequest draft for object id, invokes Editor.OnAskAgent if set,
// stores it as LastAgentPrompt, opens the anchored ⌘L in-place composer for the target (or the current
// multi-selection when id is part of it), updates Status, and returns the request.
func (e *Editor) AskAgentAboutObject(id string) (AgentPromptRequest, error) {
	if e == nil || e.Doc == nil {
		return AgentPromptRequest{}, errors.New("editor: no scene open")
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return AgentPromptRequest{}, errors.New("editor: no object selected")
	}
	obj, layerName, _, _ := e.resolveContextObject(id)
	if obj == nil || layerName == "" {
		return AgentPromptRequest{}, fmt.Errorf("editor: object %q not found", id)
	}
	name := contextObjectDisplayName(obj)
	req := AgentPromptRequest{
		ObjectID:   obj.ID,
		ObjectName: name,
		Layer:      layerName,
		ScenePath:  e.ScenePath,
		Draft:      fmt.Sprintf("About %s: ", name),
	}
	e.contextMenu.lastAgentPrompt = req
	e.Status = "Ask agent: " + req.Draft
	if e.OnAskAgent != nil {
		e.OnAskAgent(req)
	}
	targets := []string{obj.ID}
	if e.IsSelected(obj.ID) {
		if sel := e.SelectedIDs(); len(sel) > 0 {
			targets = sel
		}
	}
	if !e.askInPlaceBlocked() {
		_ = e.openAskInPlaceForTargets(targets)
	} else {
		e.notify()
	}
	return req, nil
}

// LastAgentPrompt returns the most recent AgentPromptRequest initiated by AskAgentAboutObject.
func (e *Editor) LastAgentPrompt() AgentPromptRequest {
	if e == nil {
		return AgentPromptRequest{}
	}
	return e.contextMenu.lastAgentPrompt
}

func (e *Editor) contextMenuBlocked() bool {
	return e == nil || e.Doc == nil || e.playing || e.HistoryPreviewActive() ||
		e.AskInPlaceOpen() ||
		e.StartScreenOpen() || e.ReviewOpen() || e.MergeConflictOpen() || e.PlaytestReportOpen() ||
		e.UsagesScreenOpen() || e.ChangesOpen() || e.HUDLayoutOpen() ||
		e.SettingsOpen() || e.ShortcutsSheetOpen() || e.BuildReleaseScreenOpen() ||
		e.ConnectDialogOpen() || e.AssetGeneratorOpen()
}

// OpenObjectContextMenu opens the viewport/keyboard object context menu targeting object id
// (defaulting to SelectedID). Returns false when playing, blocked by a modal screen, or when id does
// not name an existing local or resolved scene object.
func (e *Editor) OpenObjectContextMenu(id string) bool {
	if e.contextMenuBlocked() {
		return false
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return false
	}
	if obj, _, _, _ := e.resolveContextObject(id); obj == nil {
		return false
	}
	if e.contextMenu.open && e.contextMenu.targetID == id {
		return true
	}
	e.contextMenu.open = true
	e.contextMenu.targetID = id
	e.notify()
	return true
}

// CloseObjectContextMenu closes the viewport/keyboard object context menu if open.
func (e *Editor) CloseObjectContextMenu() {
	if e == nil || (!e.contextMenu.open && e.contextMenu.targetID == "") {
		return
	}
	e.contextMenu.open = false
	e.contextMenu.targetID = ""
	e.notify()
}

// resetContextMenuForProject clears context menu state, pending rename requests, agent prompt draft, and toasts on project/demo switches.
func (e *Editor) resetContextMenuForProject() {
	if e == nil {
		return
	}
	e.contextMenu = objectContextMenuState{}
	e.ClearToasts()
}

// ObjectContextMenuOpen reports whether the viewport/keyboard object context menu is currently open.
func (e *Editor) ObjectContextMenuOpen() bool {
	return e != nil && e.contextMenu.open && e.contextMenu.targetID != ""
}

// ObjectContextMenuTarget returns the object ID currently targeted by the viewport/keyboard object context menu.
func (e *Editor) ObjectContextMenuTarget() string {
	if e == nil || !e.contextMenu.open {
		return ""
	}
	return e.contextMenu.targetID
}

// ToastMessage returns the currently displayed viewport toast message, or "" when hidden.
func (e *Editor) ToastMessage() string {
	if t, ok := e.ActiveToast(); ok {
		return t.Message
	}
	return ""
}

// objectContextMenuDigest returns a narrow deterministic key for memoizing an open object context menu
// so unrelated 60 Hz frame/animation rev ticks do not rebuild menu items or collapse submenus.
func (e *Editor) objectContextMenuDigest(id string, dark bool) string {
	if e == nil || e.Doc == nil || id == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%v|%s|%s|%v|%v|%v|%v|%d|%v|%d|%v|%d|%d|%d|%d",
		id,
		dark,
		e.ResolvedLanguage(),
		e.shortcutsDigest(),
		e.playing,
		e.Project != nil && e.ProjectPath != "",
		e.ObjectHidden(id),
		e.ObjectLocked(id),
		e.ObjectOrigin(id),
		e.ObjectHasAnyOverride(id),
		e.InstanceOverrideCount(id),
		e.IsObjectSelected(id),
		e.SelectionCount(),
		e.scenesRev,
		e.History.Index(),
		e.History.Len(),
	)
	for li := range e.Doc.Layers {
		b.WriteByte('|')
		b.WriteString(e.Doc.Layers[li].Name)
		fmt.Fprintf(&b, ":%d", len(e.Doc.Layers[li].Objects))
	}
	return b.String()
}
