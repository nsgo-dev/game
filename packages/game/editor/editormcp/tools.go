package editormcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/editor"
	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// The names of the tools this package adds, as the editor's Settings › Tools page lists them.
const (
	// ToolSceneRead reads a scene, prefab, data or project file with line numbers.
	ToolSceneRead = "scene_read"
	// ToolSceneEdit adds, moves, changes and removes objects, components and properties of the open scene.
	ToolSceneEdit = "scene_edit"
	// ToolSelectionGet reports what the person has selected.
	ToolSelectionGet = "selection_get"
	// ToolViewportImage returns a PNG of the viewport.
	ToolViewportImage = "viewport_image"
	// ToolGameCheck runs the Problems panel's checks over the project.
	ToolGameCheck = "game_check"
)

// maxReadBytes is the largest file scene_read reads.
const maxReadBytes = 1 << 20

// defaultReadLines is how many lines scene_read returns when the call names no limit.
const defaultReadLines = 2000

// builtinTools are the tools New adds.
func builtinTools() []Tool {
	return append([]Tool{
		{
			Name:        ToolSceneRead,
			Title:       "Read a file",
			Description: "Read a scene, prefab, data table or other text file of the open project, or game.yaml, as YAML with line numbers. The scene open in the editor and game.yaml are read as the editor holds them while it has edits to them that are not saved, whose line numbers then differ from the file's; otherwise, and for any other file, the file is read from disk, comments included. With no file it reads the open scene.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{"file":{"type":"string","description":"Slash-separated path from the project root, such as scenes/level.scene.yaml; empty for the scene open in the editor"},"offset":{"type":"integer","minimum":1,"description":"First line to return, 1-based; 1 when absent"},"limit":{"type":"integer","minimum":1,"description":"How many lines to return; 2000 when absent"}}}`),
			Action:      ActionRead,
			Handler:     sceneRead,
		},
		{
			Name:  ToolSceneEdit,
			Title: "Edit the open scene",
			Description: "Edit the scene open in the editor through the same commands as the inspector, scene tree and viewport. The edits of one call apply in order as one undo step, and when the editor refuses any of them none applies and the error names the file, the edit, the object, the field and the fix. Edits stay unsaved in the editor until the person saves. Ops: " +
				"add {object, layer?, parent?} places an object written as in the scene file (id, name, x, y, sprite, components, properties, children...), in layer coordinates; " +
				"remove {id}; move {id, x, y}; " +
				"set {id, fields} changes object fields: id, name, x, y, z, rotation, scaleX, scaleY, alpha, originX, originY, flipX, flipY, sprite ({atlas, frame} or null), material, particles ({file, atlas?} or null), posZ, rotX, rotY, rotZ, scaleZ, depth, mesh, model, gridMap, occluder, light, camera; " +
				"set {id, component, fields} changes fields of a component the object has; " +
				"add_component {id, component, fields?}; remove_component {id, component}; " +
				"set_property {id, key, value}; remove_property {id, key}; " +
				"reparent {id, parent?, index?} moves an object under another, or to the top of its layer without parent, keeping where it is drawn; move_to_layer {id, layer, index?}.",
			InputSchema: []byte(sceneEditSchema),
			Action:      ActionEdit,
			Target:      sceneEditTarget,
			Handler:     sceneEdit,
		},
		{
			Name:        ToolSelectionGet,
			Title:       "Get the selection",
			Description: "What the person has selected in the editor: the open scene, the active layer, the selected object with its YAML and line numbers as scene_read shows them, the file selected in the file browser, the active tool and the view mode.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionRead,
			Handler:     selectionGet,
		},
		{
			Name:        ToolViewportImage,
			Title:       "Picture the viewport",
			Description: "A PNG of the editor's viewport as it shows now, at the viewport's size in points times scale: the scene through the editor's own view, with the camera frame, the tile grid, the selection outline and viewport overlays. When viewport_overlays is omitted, the editor's active overlay set is used; pass overlay names (such as collision_shapes, triggers, connections, patrol_paths, navigation, camera_bounds, labels, or active plugin overlays) to override for this image, or [] to draw no overlays. Images that are not loaded in memory are left out, and the reply says how many.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{"scale":{"type":"number","minimum":0.25,"maximum":4,"description":"Pixels per point; 1 when absent"},"viewport_overlays":{"type":"array","items":{"type":"string"},"description":"Viewport overlay names to draw (for example collision_shapes, triggers, connections, patrol_paths, navigation, camera_bounds, labels, or active plugin overlays). Uses the editor's active overlay set when omitted; pass [] for none."}}}`),
			Action:      ActionRead,
			Handler:     viewportImage,
		},
		{
			Name:        ToolGameCheck,
			Title:       "Check the project",
			Description: "Run game check over the open project, as the Problems panel does (assetcheck with the editor's component registry, and project.Check, including open unsaved documents in the editor), and return the visible problems with file, line, column and message.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionRun,
			Target:      gameCheckTarget,
			Handler:     gameCheck,
		},
		{
			Name:        ToolStatesWrite,
			Title:       "Write a sprite state machine",
			Description: "Validate and write a *.states.yaml sprite animation state machine file inside the open project in canonical YAML form, and open it in the editor's state machine graph as an undoable edit.",
			InputSchema: []byte(statesWriteSchema),
			Action:      ActionEdit,
			Target:      statesWriteTarget,
			Handler:     statesWrite,
		},
		{
			Name:        ToolStatesTest,
			Title:       "Test a sprite state machine with scripted input",
			Description: "Run deterministic fixed-step scripted input against a *.states.yaml file (or the state machine open in the editor) and report its visited state trace, transitions and any stuck states, such as \"Played 8 s of scripted input · no stuck states\".",
			InputSchema: []byte(statesTestSchema),
			Action:      ActionRun,
			Target:      statesTestTarget,
			Handler:     statesTest,
		},
		{
			Name:        ToolCutsceneWrite,
			Title:       "Propose or write a cutscene timeline",
			Description: "Validate a *.cutscene.yaml file against cutscene.Parse and open an interactive Before/After review on the cutscene timeline without overwriting disk until kept (or write directly when review is false).",
			InputSchema: []byte(cutsceneWriteSchema),
			Action:      ActionEdit,
			Target:      cutsceneWriteTarget,
			Handler:     cutsceneWrite,
		},
		{
			Name:        ToolCutsceneScrub,
			Title:       "Scrub the cutscene timeline",
			Description: "Open (when file is given) and scrub the cutscene timeline playhead to a timestamp, returning the posed camera, actors, active localized dialogue subtitle and review state.",
			InputSchema: []byte(cutsceneScrubSchema),
			Action:      ActionRead,
			Handler:     cutsceneScrub,
		},
		{
			Name:        ToolTilesetRead,
			Title:       "Read a YAML tileset",
			Description: "Read a *.tileset.yaml tileset document (or the tileset currently open in the editor, or the active layer's tilesetFile) with per-tile names, solid/one-way/custom flags, variant weights, collision shapes, animations, custom properties, and terrain rules.",
			InputSchema: []byte(tilesetReadSchema),
			Action:      ActionRead,
			Handler:     tilesetRead,
		},
		{
			Name:        ToolTilesetEdit,
			Title:       "Edit a YAML tileset",
			Description: "Edit a *.tileset.yaml tileset through the editor's undoable tileset commands in one atomic undo step: set_tile (name, solid, oneWay, flags, weight, shapes, animation, terrain, neighbors), set_property, remove_property, add_terrain, remove_terrain, with optional save.",
			InputSchema: []byte(tilesetEditSchema),
			Action:      ActionEdit,
			Target:      tilesetEditTarget,
			Handler:     tilesetEdit,
		},
	}, append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(playtestTools(), mergeTools()...), refsTools()...), tileTools()...), inspectorTools()...), objectMenuTools()...), releaseTools()...), problemTools()...), playFrozenTools()...), agentTools()...), taskTools()...), contextTools()...), historyTools()...), youOnlyTools()...), reviewTools()...), dataTableReviewTools()...), sketchTools()...), checkpointTools()...), turnTools()...), codeReviewTools()...)...)
}

// projectRoot returns the absolute, symlink-free root of ed's project, or "" without one.
func projectRoot(ed *editor.Editor) string {
	if ed.ProjectPath == "" {
		return ""
	}
	root, err := filepath.Abs(filepath.Dir(ed.ProjectPath))
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root
}

// scenePath returns the open scene's slash-separated path from root, or "" when it has no file or its file is outside root.
func scenePath(ed *editor.Editor, root string) string {
	if ed.FilePath == "" || root == "" {
		return ""
	}
	abs, err := filepath.Abs(ed.FilePath)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// sceneName is how replies name the open scene: its path from the project root, else its file, else "the open scene".
func sceneName(ed *editor.Editor, root string) string {
	if p := scenePath(ed, root); p != "" {
		return p
	}
	if ed.FilePath != "" {
		return ed.FilePath
	}
	return "the open scene"
}

// sceneYAML is the open scene as the editor would save it.
func sceneYAML(ed *editor.Editor) ([]byte, error) {
	return ed.SaveYAML()
}

// unsaved reports whether data, the open scene as the editor would save it, differs from the scene in the file at path: a file that holds the same scene in another layout, such as one written by hand, reads back and writes out as data does, and counts as saved.
func unsaved(data []byte, path string, reg *sceneformat.ComponentRegistry) bool {
	saved, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	if bytes.Equal(saved, data) {
		return false
	}
	doc, err := sceneformat.ParseWith(saved, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		return true
	}
	again, err := sceneformat.Encode(doc)
	return err != nil || !bytes.Equal(again, data)
}

// cleanProjectPath checks that file names a file inside the project, as a relative slash-separated path, and returns it cleaned.
func cleanProjectPath(file string) (string, error) {
	slashed := strings.ReplaceAll(file, `\`, "/")
	if path.IsAbs(slashed) || filepath.IsAbs(file) || filepath.VolumeName(file) != "" {
		return "", fmt.Errorf("%s: name the file by its path from the project root, such as scenes/level.scene.yaml, not an absolute path", file)
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%s: the path leaves the project; name a file inside it, such as scenes/level.scene.yaml", file)
	}
	return clean, nil
}

// sceneReadArgs are scene_read's arguments.
type sceneReadArgs struct {
	File   string `json:"file"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// FileText is a file scene_read read.
type FileText struct {
	// File is the file's slash-separated path from the project root, or the open scene's name when it has no file.
	File string `json:"file"`
	// Source is "editor" when the text is what the editor holds, with its unsaved edits, and "disk" when it is the file on disk, as it is whenever the editor holds no unsaved edits to it.
	Source string `json:"source"`
	// Unsaved reports that the editor holds edits to the file that are not saved.
	Unsaved bool `json:"unsaved"`
	// Lines is how many lines the file has.
	Lines int `json:"lines"`
	// From and To are the first and last line returned, 1-based; both are 0 for an empty file.
	From int `json:"from"`
	To   int `json:"to"`
	// hasFile reports that the text has a file on disk, whose line numbers unsaved edits make differ.
	hasFile bool
}

// sceneRead runs scene_read.
func sceneRead(ctx context.Context, c *Call) (*Result, error) {
	var args sceneReadArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("scene_read: %w", err)
	}
	if args.Offset < 0 || args.Limit < 0 {
		return nil, errors.New("scene_read: offset and limit must be positive")
	}
	var (
		root, rel string
		text      []byte
		info      FileText
		fromDisk  string
		reg       *sceneformat.ComponentRegistry
	)
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		root = projectRoot(ed)
		reg = ed.ComponentRegistry()
		open := scenePath(ed, root)
		switch {
		case args.File == "":
			data, err := sceneYAML(ed)
			if err != nil {
				return err
			}
			text, info.File, info.Source = data, sceneName(ed, root), "editor"
			fromDisk = ed.FilePath
			return nil
		case root == "":
			return fmt.Errorf("scene_read: %s: no project is open in the editor, so only the open scene can be read; call scene_read without a file", args.File)
		}
		clean, err := cleanProjectPath(args.File)
		if err != nil {
			return fmt.Errorf("scene_read: %w", err)
		}
		rel = clean
		switch {
		case clean == open:
			data, err := sceneYAML(ed)
			if err != nil {
				return err
			}
			text, info.File, info.Source = data, clean, "editor"
			fromDisk = ed.FilePath
		case clean == project.FileName && ed.ProjectDirty():
			// An unedited game.yaml is read from disk below, comments and line numbers as the person wrote them and game_check reports them.
			data, err := project.Encode(ed.Project)
			if err != nil {
				return err
			}
			text, info.File, info.Source, info.Unsaved, info.hasFile = data, clean, "editor", true, true
		case clean == project.BriefFileName && ed.BriefDirty():
			data, err := ed.BriefYAML()
			if err != nil {
				return err
			}
			text, info.File, info.Source, info.Unsaved, info.hasFile = data, clean, "editor", true, true
		case clean == editor.ProjectRulesFileName && ed.ProjectRulesDirty():
			text, info.File, info.Source, info.Unsaved, info.hasFile = []byte(ed.ProjectRules()), clean, "editor", true, true
		case ed.TilesetOpen() && ed.TilesetPath() == clean && ed.TilesetDirty():
			data, err := ed.SaveTilesetYAML()
			if err != nil {
				return err
			}
			text, info.File, info.Source, info.Unsaved, info.hasFile = data, clean, "editor", true, true
		default:
			if staged, ok := ed.StagedFileYAML(clean); ok {
				text, info.File, info.Source, info.Unsaved, info.hasFile = staged, clean, "editor", true, true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	switch {
	case info.Source == "" && rel != "":
		data, realRel, err := readProjectFileResolved(root, rel)
		if err != nil {
			return nil, fmt.Errorf("scene_read: %w", err)
		}
		// Enforce privacy on BOTH the requested path and the symlink-resolved target path after binary-file
		// checks so binary files still report "not a text file" while no forbidden Go source, art/audio
		// metadata, or symlinked file content ever leaves this computer when switched off.
		policy, _, perr := c.s.readPrivacyContext(ctx, c.onEditor)
		if perr != nil {
			return nil, perr
		}
		for _, candidate := range []string{rel, realRel} {
			if candidate == "" {
				continue
			}
			kind := editor.ClassifyProjectFilePrivacy(candidate)
			if !policy.Allows(kind) {
				return nil, editor.PrivacyRefusalError(kind, rel)
			}
		}
		text, info.File, info.Source = data, rel, "disk"
	case fromDisk != "":
		info.Unsaved, info.hasFile = unsaved(text, fromDisk, reg), true
		if !info.Unsaved {
			// The editor holds no edits, so the file on disk is the scene: read it, comments and line numbers as game_check reports them.
			if root != "" {
				if real, rerr := filepath.EvalSymlinks(fromDisk); rerr == nil {
					if inside, ierr := filepath.Rel(root, real); ierr == nil && inside != ".." && !strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
						policy, _, perr := c.s.readPrivacyContext(ctx, c.onEditor)
						if perr != nil {
							return nil, perr
						}
						if kind := editor.ClassifyProjectFilePrivacy(filepath.ToSlash(inside)); !policy.Allows(kind) {
							return nil, editor.PrivacyRefusalError(kind, info.File)
						}
					}
				}
			}
			data, err := os.ReadFile(fromDisk)
			if err != nil {
				return nil, fmt.Errorf("scene_read: %w", err)
			}
			text, info.Source = data, "disk"
		}
	case info.Source == "editor" && rel == "":
		info.Unsaved = true
	}
	return numberLines(text, info, args.Offset, args.Limit), nil
}

// readProjectFile reads the text file rel inside root, refusing one a symbolic link takes out of the project, one that is not a regular file, one larger than maxReadBytes and one that is not UTF-8 text.
func readProjectFile(root, rel string) ([]byte, error) {
	data, _, err := readProjectFileResolved(root, rel)
	return data, err
}

// readProjectFileResolved reads the text file rel inside root and returns both the file bytes and the
// symlink-resolved slash-separated path relative to root so privacy checks see the real target file too.
func readProjectFileResolved(root, rel string) ([]byte, string, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("%s: no such file in the project at %s; name a file by its path from the project root, such as scenes/level.scene.yaml", rel, root)
		}
		return nil, "", err
	}
	inside, err := filepath.Rel(root, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("%s: the file links to %s, outside the project; scene_read reads files inside it", rel, real)
	}
	realRel := filepath.ToSlash(inside)
	st, err := os.Stat(real)
	if err != nil {
		return nil, "", err
	}
	if !st.Mode().IsRegular() {
		return nil, "", fmt.Errorf("%s: not a file; name a scene, prefab, data or other text file", rel)
	}
	if st.Size() > maxReadBytes {
		return nil, "", fmt.Errorf("%s: %d bytes is more than the %d scene_read reads", rel, st.Size(), maxReadBytes)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return nil, "", err
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, "", fmt.Errorf("%s: not a text file; scene_read reads YAML and other text files, and viewport_image shows what the scene draws", rel)
	}
	return data, realRel, nil
}

// numberLines returns the lines of text from offset, at most limit of them, each after its 1-based number and a tab, under a line naming the file.
func numberLines(text []byte, info FileText, offset, limit int) *Result {
	var lines []string
	if len(text) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	}
	info.Lines = len(lines)
	if offset == 0 {
		offset = 1
	}
	if limit == 0 {
		limit = defaultReadLines
	}
	from := min(offset, len(lines)+1)
	// limit is clamped before the addition, so a huge limit cannot overflow it.
	to := from + min(limit, len(lines)-from+1) - 1
	var b strings.Builder
	b.WriteString(info.File)
	if info.Source == "editor" {
		b.WriteString(" (as the editor holds it")
		if info.Unsaved {
			b.WriteString(", with unsaved edits")
			if info.hasFile {
				b.WriteString("; its line numbers are not those of the file on disk, which game_check reports")
			}
		}
		b.WriteString(")")
	}
	if len(lines) == 0 {
		b.WriteString(": empty\n")
		return &Result{Text: b.String(), Structured: info}
	}
	if from > to {
		fmt.Fprintf(&b, ": %d lines, none from line %d\n", len(lines), offset)
		return &Result{Text: b.String(), Structured: info}
	}
	info.From, info.To = from, to
	fmt.Fprintf(&b, ": lines %d-%d of %d", from, to, len(lines))
	if to < len(lines) {
		fmt.Fprintf(&b, "; call again with offset %d for the rest", to+1)
	}
	b.WriteString("\n")
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i, strings.TrimSuffix(lines[i-1], "\r"))
	}
	return &Result{Text: b.String(), Structured: info}
}

// Selection is what selection_get reports.
type Selection struct {
	// Scene is the open scene's path from the project root, or its file, or "the open scene" when it has none.
	Scene string `json:"scene"`
	// ActiveLayer is the layer new objects go on.
	ActiveLayer string `json:"activeLayer"`
	// Object is the selected object, or nil when none is selected.
	Object *SelectedObject `json:"object,omitempty"`
	// SelectedIDs lists every currently selected object ID when one or more objects are selected.
	SelectedIDs []string `json:"selectedIds,omitempty"`
	// File is the file selected in the file browser, by its path from the project root, or empty.
	File string `json:"file,omitempty"`
	// Tool is the viewport's active tool, such as "Select".
	Tool string `json:"tool"`
	// ViewMode is "2D" or "3D".
	ViewMode string `json:"viewMode"`
	// Playing reports that the scene plays in the editor, when edits are refused.
	Playing bool `json:"playing,omitempty"`
	// Language is the editor's active UI language ("en" or "zh"), so connected agents can reply in the same language.
	Language string `json:"language,omitempty"`
}

// SelectedObject is the selected object of a Selection.
type SelectedObject struct {
	// ID and Name are the object's ID and display name.
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Layer is the layer the object is on.
	Layer string `json:"layer"`
	// X and Y are where the object sits in its parent's space.
	X float32 `json:"x"`
	Y float32 `json:"y"`
	// Components are the names of its components, sorted.
	Components []string `json:"components,omitempty"`
	// Line is the line of the open scene's YAML, as scene_read numbers it, that starts the object, or 0 when the scene does not hold it itself.
	Line int `json:"line,omitempty"`
	// YAML is the object as the open scene's YAML writes it, with line numbers as scene_read shows them.
	YAML string `json:"yaml,omitempty"`
}

// readSelection is ed's selection.
func readSelection(ed *editor.Editor) Selection {
	root := projectRoot(ed)
	sel := Selection{
		Scene:       sceneName(ed, root),
		ActiveLayer: ed.ActiveLayer,
		SelectedIDs: ed.SelectedIDs(),
		File:        ed.SelectedFile,
		Tool:        ed.Tool.String(),
		ViewMode:    ed.ViewMode.String(),
		Playing:     ed.Playing(),
		Language:    ed.ResolvedLanguage(),
	}
	obj, layer := ed.SelectedObject()
	if obj == nil {
		return sel
	}
	so := &SelectedObject{ID: obj.ID, Name: obj.Name, Layer: layer, X: obj.X, Y: obj.Y}
	for name := range obj.Components {
		so.Components = append(so.Components, name)
	}
	slices.Sort(so.Components)
	if data, err := sceneYAML(ed); err == nil {
		so.Line, so.YAML = objectBlock(data, obj.ID)
	}
	sel.Object = so
	return sel
}

// objectBlock finds the object id in a scene's YAML, as sceneformat.Encode writes it, and returns the line that starts it and its lines numbered as scene_read numbers them, or 0 and "" when the YAML does not hold it.
func objectBlock(data []byte, id string) (int, string) {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		v, ok := strings.CutPrefix(trimmed, "- id: ")
		if !ok || yamlScalar(v) != id {
			continue
		}
		indent := len(line) - len(trimmed)
		end := i + 1
		for end < len(lines) {
			next := lines[end]
			rest := strings.TrimLeft(next, " ")
			if rest != "" && len(next)-len(rest) <= indent {
				break
			}
			end++
		}
		var b strings.Builder
		for j := i; j < end; j++ {
			fmt.Fprintf(&b, "%6d\t%s\n", j+1, lines[j])
		}
		return i + 1, b.String()
	}
	return 0, ""
}

// yamlScalar is the plain, single- or double-quoted YAML scalar v as a string.
func yamlScalar(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(v, `"`):
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	case strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") && len(v) >= 2:
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// selectionGet runs selection_get.
func selectionGet(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("selection_get: %w", err)
	}
	var (
		sel   Selection
		file  string
		scene []byte
		reg   *sceneformat.ComponentRegistry
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		sel = readSelection(ed)
		if sel.Object != nil {
			file, reg = ed.FilePath, ed.ComponentRegistry()
			scene, _ = sceneYAML(ed)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// While the editor holds no unsaved edits, scene_read numbers the file on disk, so the object's lines come from it too when it holds the object in the layout objectBlock finds.
	if file != "" && !unsaved(scene, file, reg) {
		if data, err := os.ReadFile(file); err == nil {
			if line, block := objectBlock(data, sel.Object.ID); line > 0 {
				sel.Object.Line, sel.Object.YAML = line, block
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Scene %s, active layer %s, tool %s, %s view", sel.Scene, sel.ActiveLayer, sel.Tool, sel.ViewMode)
	if sel.Playing {
		b.WriteString(", playing")
	}
	b.WriteString(".\n")
	if sel.File != "" {
		fmt.Fprintf(&b, "File browser: %s selected.\n", sel.File)
	}
	if sel.Object == nil {
		b.WriteString("No object is selected.\n")
	} else {
		fmt.Fprintf(&b, "Selected object %s on layer %s", sel.Object.ID, sel.Object.Layer)
		if sel.Object.Line > 0 {
			fmt.Fprintf(&b, ", at line %d", sel.Object.Line)
		}
		b.WriteString(":\n")
		b.WriteString(sel.Object.YAML)
	}
	return &Result{Text: b.String(), Structured: sel}, nil
}

// ViewportImage is what viewport_image reports beside its PNG.
type ViewportImage struct {
	// Width and Height are the PNG's size in pixels.
	Width  int `json:"width"`
	Height int `json:"height"`
	// View is the editor's 2D view: the world point at the center, the zoom and the rotation.
	View editor.View2D `json:"view"`
	// ViewMode is "2D" or "3D".
	ViewMode string `json:"viewMode"`
	// ViewportOverlays lists the viewport overlay names drawn into this image.
	ViewportOverlays []string `json:"viewportOverlays,omitempty"`
	// SkippedImages counts the images left out because they are not loaded in memory.
	SkippedImages int `json:"skippedImages,omitempty"`
	// SkippedOps counts the drawing operations left out by kind.
	SkippedOps map[string]int `json:"skippedOps,omitempty"`
}

// viewportImage runs viewport_image.
func viewportImage(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Scale            float64   `json:"scale"`
		ViewportOverlays *[]string `json:"viewport_overlays"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("viewport_image: %w", err)
	}
	scale := args.Scale
	if scale == 0 {
		scale = 1
	}
	if !(scale >= 0.25 && scale <= 4) {
		return nil, fmt.Errorf("viewport_image: scale %v must be between 0.25 and 4", args.Scale)
	}
	var (
		ops  []core.DrawOp
		w, h float32
		info ViewportImage
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		w, h = ed.CanvasSize()
		var d core.Drawing
		d.Scale(float32(scale), float32(scale))
		size := core.Size{Width: core.Points(w), Height: core.Points(h)}
		if ed.ViewMode == editor.ViewMode3D {
			if args.ViewportOverlays != nil {
				return fmt.Errorf("viewport_image: viewport_overlays only applies in 2D view mode")
			}
			ed.DrawViewport(&d, size)
		} else if args.ViewportOverlays == nil {
			if !ed.Playing() {
				info.ViewportOverlays = ed.ActiveViewportOverlays()
			}
			ed.DrawViewport(&d, size)
		} else {
			req := *args.ViewportOverlays
			if req == nil {
				req = []string{}
			}
			if err := ed.DrawViewportWithOverlays(&d, size, req); err != nil {
				return fmt.Errorf("viewport_image: %w", err)
			}
			if !ed.Playing() {
				seen := make(map[string]struct{}, len(req))
				for _, id := range req {
					if _, dup := seen[id]; !dup {
						seen[id] = struct{}{}
						info.ViewportOverlays = append(info.ViewportOverlays, id)
					}
				}
				slices.Sort(info.ViewportOverlays)
			}
		}
		ops = d.Ops
		info.View = ed.View2D()
		info.ViewMode = ed.ViewMode.String()
		return nil
	}); err != nil {
		return nil, err
	}
	pw, ph := int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale))
	r, err := gamemcp.RenderPNG(ops, pw, ph, 0)
	if err != nil {
		return nil, fmt.Errorf("viewport_image: %w", err)
	}
	pngBytes := r.PNG
	skippedOps := r.SkippedOps
	if skippedOps["DrawFillText"] > 0 {
		if rendered, ok := renderOverlayTextPNG(pngBytes, ops); ok {
			pngBytes = rendered
			delete(skippedOps, "DrawFillText")
			if len(skippedOps) == 0 {
				skippedOps = nil
			}
		}
	}
	info.Width, info.Height, info.SkippedImages, info.SkippedOps = r.Width, r.Height, r.SkippedImages, skippedOps
	text := fmt.Sprintf("The %s viewport, %d×%d pixels, view centered on (%g, %g) at zoom %g.", info.ViewMode, r.Width, r.Height, info.View.X, info.View.Y, info.View.Zoom)
	if len(info.ViewportOverlays) > 0 {
		text += fmt.Sprintf(" Overlays: %s.", strings.Join(info.ViewportOverlays, ", "))
	}
	if n := r.SkippedImages; n > 0 {
		text += fmt.Sprintf(" %d images are not loaded in memory and are left out.", n)
	}
	skipped := 0
	for _, n := range skippedOps {
		skipped += n
	}
	if skipped > 0 {
		text += fmt.Sprintf(" %d drawing operations it cannot draw are left out.", skipped)
	}
	return &Result{Text: text, Structured: info, PNG: pngBytes}, nil
}

// Problem is one problem game_check reports, as a row of the Problems panel shows it.
type Problem struct {
	// File is the slash-separated path of the file at fault from the project root.
	File string `json:"file"`
	// Line is the 1-based line of the problem, or 0 when it is about the whole file.
	Line int `json:"line,omitempty"`
	// Column is the 1-based column on Line where the scene format knows it, or 0 when unknown.
	Column int `json:"column,omitempty"`
	// Object is the ID of the scene object the problem names or belongs to when known.
	Object string `json:"object,omitempty"`
	// Message says what is wrong.
	Message string `json:"message"`
}

// Check is what game_check reports.
type Check struct {
	// Problems are the visible problems the Problems panel lists, sorted by file, line, column and message.
	Problems []Problem `json:"problems"`
	// Unsaved names the open scene when the editor holds edits to it not yet saved on disk (which game_check checked live).
	Unsaved string `json:"unsaved,omitempty"`
}

// gameCheck runs game_check.
func gameCheck(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("game_check: %w", err)
	}
	var (
		ed    *editor.Editor
		scene []byte
		file  string
		name  string
		reg   *sceneformat.ComponentRegistry
	)
	if err := c.Editor(ctx, func(e *editor.Editor) error {
		if e.Project == nil {
			return errors.New("game_check: no project is open in the editor; open one with go tool game editor <project-dir>")
		}
		c.s.beginBackgroundWork(e, "checking project")
		e.CheckProblems()
		ed, file, name, reg = e, e.FilePath, sceneName(e, projectRoot(e)), e.ComponentRegistry()
		scene, _ = sceneYAML(e)
		return nil
	}); err != nil {
		return nil, err
	}
	defer func() {
		_ = c.Editor(context.Background(), func(e *editor.Editor) error {
			c.s.endBackgroundWork(e)
			return nil
		})
	}()
	waited := make(chan struct{})
	go func() {
		ed.WaitForProblems()
		close(waited)
	}()
	select {
	case <-waited:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ed.ProblemErr(); err != nil {
		return nil, fmt.Errorf("game_check: %w", err)
	}
	found := ed.Problems()
	res := Check{Problems: make([]Problem, 0, len(found))}
	for _, p := range found {
		res.Problems = append(res.Problems, Problem{File: p.File, Line: p.Line, Column: p.Column, Object: p.Object, Message: p.Message})
	}
	if file != "" && unsaved(scene, file, reg) {
		res.Unsaved = name
	}
	var b strings.Builder
	switch len(res.Problems) {
	case 0:
		b.WriteString("No problems.\n")
	case 1:
		b.WriteString("1 problem:\n")
	default:
		fmt.Fprintf(&b, "%d problems:\n", len(res.Problems))
	}
	for _, p := range found {
		b.WriteString(p.Location())
		b.WriteString(": ")
		b.WriteString(p.Message)
		b.WriteByte('\n')
	}
	if res.Unsaved != "" {
		fmt.Fprintf(&b, "%s has edits that are not saved (checked live from the editor).\n", res.Unsaved)
	}
	return &Result{Text: b.String(), Structured: res}, nil
}
