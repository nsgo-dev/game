package editor

import (
	"errors"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// WorkspaceState classifies the high-level state of the main editor workspace
// so every region (header, left rail, viewport, right panel, status bar) can
// render a designed empty, loading, error, or active editing state.
type WorkspaceState uint8

const (
	// WorkspaceReady means an editable scene is open and ready.
	WorkspaceReady WorkspaceState = iota
	// WorkspaceNoProject means no project and no real scene have been opened yet.
	WorkspaceNoProject
	// WorkspaceNoScene means a project is open, but no scene is currently open.
	WorkspaceNoScene
	// WorkspaceLoading means a project is currently being loaded.
	WorkspaceLoading
	// WorkspaceSceneError means opening a scene failed (missing file, malformed YAML, etc.).
	WorkspaceSceneError
)

// SceneOpenError captures the structured details of a failed scene open
// (covering missing files and YAML/schema errors from open_errors_test.go).
type SceneOpenError struct {
	Path     string
	Line     int
	Column   int
	Message  string
	Fix      string
	Err      error
	openPath string
}

// Inline Lucide stroke icons (1.8 px stroke at 15 px, currentColor, decorative / aria-hidden in core.Icon).
var (
	mainIconBrandMark = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 19 12 5l7 14"/></svg>`))
	mainIconSelect    = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M5 3l14 8-6 2-2 6z"/></svg>`))
	mainIconMove      = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v18M3 12h18M9 6l3-3 3 3M9 18l3 3 3-3M6 9l-3 3 3 3M18 9l3 3-3 3"/></svg>`))
	mainIconRotate    = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 12a8 8 0 1 1-3-6.2"/><path d="M20 4v5h-5"/></svg>`))
	mainIconScale     = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="10" width="10" height="10" rx="1.5"/><path d="M13 4h7v7M20 4l-8 8"/></svg>`))
	mainIconPan       = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V5.5a1.5 1.5 0 0 1 3 0V12M11 11.5v-7a1.5 1.5 0 0 1 3 0V12M14 12V6.5a1.5 1.5 0 0 1 3 0V13M8 13l-1.6-1.6a1.6 1.6 0 0 0-2.3 2.3L8 18c1.3 1.6 3 2.5 5 2.5h1a5 5 0 0 0 5-5V9.5a1.5 1.5 0 0 0-3 0"/></svg>`))
	mainIconHistory   = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7"/><path d="M3 4v5h5"/><path d="M12 8v4l3 2"/></svg>`))
	mainIconSearch    = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>`))
	mainIconPlay      = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M6 4l14 8-14 8z"/></svg>`))
	mainIconStop      = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor" stroke="none"><rect x="5" y="5" width="14" height="14" rx="2"/></svg>`))
)

func (e *Editor) recordSceneOpenError(filePath string, err error) {
	if e == nil || err == nil {
		return
	}
	se := &SceneOpenError{
		Path:     filePath,
		Message:  err.Error(),
		Err:      err,
		openPath: filePath,
	}
	var le *loaderr.Error
	if errors.As(err, &le) && le != nil {
		if le.File != "" {
			se.Path = le.File
		}
		se.Line = le.Line
		se.Column = le.Column
		if le.Problem != "" {
			se.Message = le.Problem
		}
		se.Fix = le.Fix
	}
	if se.Fix == "" {
		se.Fix = "Check that the scene file exists and contains valid YAML."
	}
	e.projectLoading = false
	e.projectLoadingPath = ""
	e.sceneOpenErr = se
	e.Status = "Failed to open " + filepath.Base(se.Path)
	e.notify()
}

// SceneOpenError returns a copy of the active scene-open error state, or nil when none.
func (e *Editor) SceneOpenError() *SceneOpenError {
	if e == nil || e.sceneOpenErr == nil {
		return nil
	}
	cp := *e.sceneOpenErr
	return &cp
}

// ClearSceneOpenError dismisses the active scene-open error state.
func (e *Editor) ClearSceneOpenError() {
	if e == nil || e.sceneOpenErr == nil {
		return
	}
	e.sceneOpenErr = nil
	e.notify()
}

// RetryOpenScene retries opening the scene recorded in SceneOpenError.
func (e *Editor) RetryOpenScene() error {
	if e == nil || e.sceneOpenErr == nil {
		return nil
	}
	p := e.sceneOpenErr.openPath
	if p == "" {
		p = e.sceneOpenErr.Path
	}
	if p == "" {
		e.ClearSceneOpenError()
		return nil
	}
	if e.Project != nil && e.ProjectPath != "" && !filepath.IsAbs(p) {
		return e.OpenScene(p)
	}
	return e.OpenFile(p)
}

// ProjectLoading reports whether the editor is currently loading a project.
func (e *Editor) ProjectLoading() bool {
	return e != nil && e.projectLoading
}

// ProjectLoadingPath returns the project path currently being loaded, if any.
func (e *Editor) ProjectLoadingPath() string {
	if e == nil {
		return ""
	}
	return e.projectLoadingPath
}

// SetProjectLoading sets the project-loading state so every region displays its designed loading state.
func (e *Editor) SetProjectLoading(loading bool, projectPath string) {
	if e == nil {
		return
	}
	if !loading {
		projectPath = ""
	}
	if e.projectLoading == loading && e.projectLoadingPath == projectPath {
		return
	}
	e.projectLoading = loading
	e.projectLoadingPath = projectPath
	e.notify()
}

func (e *Editor) isActiveTabPlaceholder() bool {
	if e == nil {
		return false
	}
	if e.activeTab >= 0 && e.activeTab < len(e.tabs) {
		return e.tabs[e.activeTab].placeholder
	}
	return false
}

// WorkspaceState returns the active high-level state of the editor workspace.
func (e *Editor) WorkspaceState() WorkspaceState {
	if e == nil {
		return WorkspaceNoProject
	}
	if e.projectLoading {
		return WorkspaceLoading
	}
	if e.sceneOpenErr != nil {
		return WorkspaceSceneError
	}
	if e.isActiveTabPlaceholder() && e.FilePath == "" && e.ScenePath == "" && !e.SceneDirty() && e.Doc != nil && len(e.Doc.Layers) == 1 && len(e.Doc.Layers[0].Objects) == 0 {
		if e.Project != nil && e.ProjectPath != "" {
			return WorkspaceNoScene
		}
		return WorkspaceNoProject
	}
	if e.Doc == nil {
		if e.Project != nil && e.ProjectPath != "" {
			return WorkspaceNoScene
		}
		return WorkspaceNoProject
	}
	return WorkspaceReady
}

// workspaceStateKey returns a digest of WorkspaceState plus loading/error details and
// resolved language so reactive memos re-render when one scene-open error is replaced
// by another or when the editor language changes.
func (e *Editor) workspaceStateKey() string {
	ws := e.WorkspaceState()
	lang := e.ResolvedLanguage()
	switch ws {
	case WorkspaceLoading:
		return fmt.Sprintf("loading|%s|%s", lang, e.projectLoadingPath)
	case WorkspaceSceneError:
		if se := e.sceneOpenErr; se != nil {
			return fmt.Sprintf("error|%s|%s|%d|%d|%s|%s", lang, se.Path, se.Line, se.Column, se.Message, se.Fix)
		}
		return "error|" + lang
	default:
		return fmt.Sprintf("state|%s|%d", lang, ws)
	}
}

// LayersPanelOpen reports whether the docked Layers panel is open.
func (e *Editor) LayersPanelOpen() bool { return e != nil && e.layersPanelOpen }

// SetLayersPanelOpen opens or closes the docked Layers panel.
func (e *Editor) SetLayersPanelOpen(open bool) {
	if e == nil || e.layersPanelOpen == open {
		return
	}
	e.layersPanelOpen = open
	e.notify()
}

// FileBrowserOpen reports whether the full Project Files browser panel is open.
func (e *Editor) FileBrowserOpen() bool { return e != nil && e.fileBrowserOpen }

// SetFileBrowserOpen opens or closes the full Project Files browser panel.
func (e *Editor) SetFileBrowserOpen(open bool) {
	if e == nil || e.fileBrowserOpen == open {
		return
	}
	e.fileBrowserOpen = open
	e.notify()
}

// AssetBrowserOpen reports whether the Asset Browser panel is open.
func (e *Editor) AssetBrowserOpen() bool { return e != nil && e.assetBrowserOpen }

// SetAssetBrowserOpen opens or closes the Asset Browser panel.
func (e *Editor) SetAssetBrowserOpen(open bool) {
	if e == nil || e.assetBrowserOpen == open {
		return
	}
	e.assetBrowserOpen = open
	e.notify()
}

// LibraryPanelOpen reports whether the Asset Pack Library panel is open.
func (e *Editor) LibraryPanelOpen() bool { return e != nil && e.libraryPanelOpen }

// SetLibraryPanelOpen opens or closes the Asset Pack Library panel.
func (e *Editor) SetLibraryPanelOpen(open bool) {
	if e == nil || e.libraryPanelOpen == open {
		return
	}
	e.libraryPanelOpen = open
	e.notify()
}

// ProjectDisplayName returns the human-readable project name for the header breadcrumb.
func (e *Editor) ProjectDisplayName() string {
	if e == nil {
		return ""
	}
	if e.Project != nil {
		if name := strings.TrimSpace(e.Project.Name); name != "" {
			return name
		}
	}
	if e.ProjectPath != "" {
		dir := filepath.Dir(e.ProjectPath)
		if base := filepath.Base(dir); base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return ""
}

// SceneBreadcrumbName returns a concise scene label for the header breadcrumb.
func (e *Editor) SceneBreadcrumbName() string {
	if e == nil {
		return ""
	}
	if e.projectLoading {
		return e.T("workspace.loadingShort")
	}
	if e.sceneOpenErr != nil && e.sceneOpenErr.Path != "" {
		return stripSceneExt(filepath.Base(e.sceneOpenErr.Path))
	}
	if e.WorkspaceState() == WorkspaceNoScene {
		return e.T("workspace.noSceneTitle")
	}
	if e.ScenePath != "" {
		return stripSceneExt(path.Base(e.ScenePath))
	}
	if e.FilePath != "" {
		return stripSceneExt(filepath.Base(e.FilePath))
	}
	if e.Doc != nil && strings.TrimSpace(e.Doc.Name) != "" {
		return strings.TrimSpace(e.Doc.Name)
	}
	return e.T("scene.untitledScene")
}

func stripSceneExt(base string) string {
	for _, suf := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(base, suf) && len(base) > len(suf) {
			return strings.TrimSuffix(base, suf)
		}
	}
	return base
}

// SceneNodeCount returns the total number of nodes (layers + all nested objects) in the open scene.
func (e *Editor) SceneNodeCount() int {
	if e == nil {
		return 0
	}
	doc := e.resolvedDoc()
	if doc == nil {
		return 0
	}
	total := 0
	var countObjs func(objs []sceneformat.ObjectDoc)
	countObjs = func(objs []sceneformat.ObjectDoc) {
		for i := range objs {
			total++
			countObjs(objs[i].Children)
		}
	}
	for i := range doc.Layers {
		total++
		countObjs(doc.Layers[i].Objects)
	}
	return total
}

// SceneUsedPrefabs returns the sorted, deduplicated project-relative paths of
// prefabs and instanced scenes referenced by objects in the open scene.
func (e *Editor) SceneUsedPrefabs() []string {
	if e == nil {
		return nil
	}
	doc := e.resolvedDoc()
	if doc == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	var walk func(objs []sceneformat.ObjectDoc)
	walk = func(objs []sceneformat.ObjectDoc) {
		for i := range objs {
			o := &objs[i]
			for _, raw := range []string{o.Prefab, o.Scene} {
				if p := strings.TrimSpace(raw); p != "" {
					if _, ok := seen[p]; !ok {
						seen[p] = struct{}{}
						out = append(out, p)
					}
				}
			}
			walk(o.Children)
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Objects)
	}
	slices.Sort(out)
	return out
}

// SetProposedNewObjectIDs marks object IDs proposed by a pending review so the scene rail displays a NEW tag.
func (e *Editor) SetProposedNewObjectIDs(ids []string) {
	if e == nil {
		return
	}
	if len(ids) == 0 {
		if len(e.proposedNewObjects) == 0 {
			return
		}
		e.proposedNewObjects = nil
		e.notify()
		return
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			m[id] = true
		}
	}
	e.proposedNewObjects = m
	e.notify()
}

// IsObjectProposedNew reports whether object id is proposed as a new object by a pending review.
func (e *Editor) IsObjectProposedNew(id string) bool {
	if e == nil || id == "" {
		return false
	}
	if e.proposedNewObjects[id] {
		return true
	}
	if e.agentReview.open {
		for _, c := range e.agentReview.changes {
			if c.Kind != ReviewChangeAdded || c.Reverted || c.Kept {
				continue
			}
			if c.ObjectID == id || c.ID == id || c.Title == id {
				return true
			}
		}
	}
	return false
}

// SceneRailObjectKind returns the concise Spec kind tag for an object row in the scene rail
// (e.g. "prefab", "sprite", "trigger", "group", "mesh", "light", "camera", "node").
func (e *Editor) SceneRailObjectKind(id string) string {
	if e == nil {
		return "node"
	}
	obj, _ := e.FindResolvedObject(id)
	if obj == nil {
		return "node"
	}
	if obj.Prefab != "" || obj.Scene != "" {
		return "prefab"
	}
	if obj.Camera != nil {
		return "camera"
	}
	if obj.Light != nil {
		return "light"
	}
	if obj.Model != "" || obj.Mesh != "" {
		return "mesh"
	}
	if len(obj.Children) > 0 && obj.Sprite == nil {
		return "group"
	}
	var hasCamera, hasLight, hasTrigger bool
	for name := range obj.Components {
		switch strings.ToLower(name) {
		case "camera", "camera2d", "camera3d":
			hasCamera = true
		case "light", "light2d", "light3d":
			hasLight = true
		case "trigger", "trigger2d", "trigger3d":
			hasTrigger = true
		}
	}
	if hasCamera {
		return "camera"
	}
	if hasLight {
		return "light"
	}
	if hasTrigger {
		return "trigger"
	}
	if obj.Sprite != nil {
		return "sprite"
	}
	return "node"
}

// LayerKindLabel returns the concise Spec kind tag for a layer row in the scene rail
// ("tiles", "screen", or "layer").
func (e *Editor) LayerKindLabel(layerName string) string {
	if e == nil {
		return "layer"
	}
	doc := e.resolvedDoc()
	if doc == nil {
		return "layer"
	}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.Name != layerName {
			continue
		}
		if l.Tilemap != nil {
			return "tiles"
		}
		if l.EffectiveSpace() == sceneformat.SpaceScreen {
			return "screen"
		}
		return "layer"
	}
	return "layer"
}

// ObjectKindColor maps an object's kind or pending-new state to a SpecPalette token color.
func (e *Editor) ObjectKindColor(p SpecPalette, id string) core.Color {
	if e.IsObjectProposedNew(id) {
		return p.Added.Fg
	}
	switch e.SceneRailObjectKind(id) {
	case "prefab":
		return p.Accent.Fg
	case "sprite":
		return p.Changed.Fg
	case "trigger":
		return p.Warning.Fg
	case "group":
		return p.Agent.Fg
	case "mesh", "light", "camera":
		return p.Accent.Fg
	default:
		return p.TextFaint
	}
}

// LayerKindColor maps a layer's kind to a SpecPalette token color.
func (e *Editor) LayerKindColor(p SpecPalette, layerName string) core.Color {
	switch e.LayerKindLabel(layerName) {
	case "tiles":
		return p.Accent.Fg
	default:
		return p.TextFaint
	}
}

func (e *Editor) activeHeaderTool() Tool {
	if e == nil {
		return ToolSelect
	}
	if e.ViewMode == ViewMode3D {
		if e.Tool == ToolPan {
			return ToolPan
		}
		if e.Tool == ToolSelect {
			return ToolSelect
		}
		switch e.GizmoMode {
		case Gizmo3DRotate:
			return ToolRotate
		case Gizmo3DScale:
			return ToolScale
		default:
			return ToolMove
		}
	}
	return e.Tool
}

// mainHeaderView renders the 44 px AI-first header matching docs/design/editor/Main.dc.html
// and docs/design/editor/Spec.dc.html.
func mainHeaderView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dirty *core.Memo[unsavedState], mode *core.Signal[pathMode], target *core.Signal[string]) core.Node {
	p := ed.SpecPalette(cx)
	notPlaying := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	noProject := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil || ed.ProjectPath == ""
	}

	fileBtnRef := &core.Ref{}
	selectToolRef := &core.Ref{}
	historyBtnRef := &core.Ref{}
	paletteBtnRef := &core.Ref{}
	playBtnRef := &core.Ref{}
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionHeader, fileBtnRef, nil)
		ed.focus.ctrl.registerCandidate(cx, RegionHeader, selectToolRef, func() bool { return !ed.Playing() })
		ed.focus.ctrl.registerCandidate(cx, RegionHeader, historyBtnRef, func() bool { return !ed.Playing() })
		ed.focus.ctrl.registerCandidate(cx, RegionHeader, paletteBtnRef, nil)
		ed.focus.ctrl.registerCandidate(cx, RegionHeader, playBtnRef, nil)
	}

	shortcutTip := func(name, cmdID string) string {
		if sc := ed.EffectiveShortcut(cmdID); sc != "" {
			return specShortcutTip(name, FormatShortcut(sc, cx.Env().OS()))
		}
		return name
	}

	type headerToolDef struct {
		testID string
		cmdID  string
		label  string
		tool   Tool
		svg    *core.SVG
		ref    *core.Ref
	}
	toolDefs := []headerToolDef{
		{testID: "tool-select", cmdID: CmdToolSelect, label: ed.T("toolbar.select"), tool: ToolSelect, svg: mainIconSelect, ref: selectToolRef},
		{testID: "tool-move", cmdID: CmdToolMove, label: ed.T("toolbar.move"), tool: ToolMove, svg: mainIconMove},
		{testID: "tool-rotate", cmdID: CmdToolRotate, label: ed.T("toolbar.rotate"), tool: ToolRotate, svg: mainIconRotate},
		{testID: "tool-scale", cmdID: CmdToolScale, label: ed.T("toolbar.scale"), tool: ToolScale, svg: mainIconScale},
		{testID: "tool-pan", cmdID: CmdToolPan, label: ed.T("toolbar.pan"), tool: ToolPan, svg: mainIconPan},
	}

	toolRadios := make([]core.Node, 0, len(toolDefs))
	for _, td := range toolDefs {
		d := td
		toolRadios = append(toolRadios, base.Radio{
			Ref:      d.ref,
			TestID:   d.testID,
			Label:    d.label,
			Disabled: notPlaying,
			Tooltip:  specTooltipNode(p, shortcutTip(d.label, d.cmdID)),
			Checked: func() bool {
				_ = rev.Get()
				return ed.activeHeaderTool() == d.tool
			},
			OnChange: func(checked bool) {
				if !checked {
					return
				}
				ed.SetTool(d.tool)
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				cur := ed.SpecPalette(cx)
				bg := core.Transparent
				if ed.activeHeaderTool() == d.tool {
					bg = cur.LineRaised
				}
				return core.Style{
					Width:          core.Pt(32),
					Height:         core.Pt(specRowHeight),
					MinWidth:       core.Pt(specRowHeight),
					MinHeight:      core.Pt(specRowHeight),
					CornerRadius:   specRadiusControl,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     bg,
					Cursor:         core.CursorPointer,
					Hover:          &core.Style{Background: cur.LineRaised},
					Disabled:       &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
					FocusVisible:   cur.FocusRing(2),
				}
			},
			Children: []core.Node{
				core.Icon{
					SVG: d.svg,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						cur := ed.SpecPalette(cx)
						col := cur.TextFaint
						if ed.activeHeaderTool() == d.tool {
							col = cur.TextPrimary
						}
						return core.Style{
							Width:      core.Pt(specIconSize),
							Height:     core.Pt(specIconSize),
							Color:      col,
							FlexShrink: 0,
						}
					},
				},
			},
		})
	}

	viewModeRadio := func(testID, label string, vm ViewMode) core.Node {
		return base.Radio{
			TestID: testID,
			Label:  label,
			Checked: func() bool {
				_ = rev.Get()
				return ed.ViewMode == vm
			},
			OnChange: func(checked bool) {
				if checked {
					ed.SetViewMode(vm)
				}
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				cur := ed.SpecPalette(cx)
				bg := core.Transparent
				if ed.ViewMode == vm {
					bg = cur.LineRaised
				}
				return core.Style{
					Height:         core.Pt(specRowHeight),
					MinWidth:       core.Pt(32),
					MinHeight:      core.Pt(specRowHeight),
					Padding:        core.Symmetric(0, specSpace12),
					CornerRadius:   specRadiusControl,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     bg,
					Cursor:         core.CursorPointer,
					Hover:          &core.Style{Background: cur.LineRaised},
					FocusVisible:   cur.FocusRing(2),
				}
			},
			Children: []core.Node{
				core.Text{
					Content:  label,
					MaxLines: 1,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						cur := ed.SpecPalette(cx)
						col := cur.TextFaint
						if ed.ViewMode == vm {
							col = cur.TextPrimary
						}
						return core.Style{
							FontSize:   12.5,
							FontWeight: core.FontWeightMedium,
							Color:      col,
						}
					},
				},
			},
		}
	}

	headerBaseStyle := core.Style{
		Direction:   core.Row,
		AlignItems:  core.AlignCenter,
		Height:      core.Pt(specHeaderHeight),
		FlexShrink:  0,
		Gap:         specSpace12,
		Padding:     core.Symmetric(0, specSpace12),
		Background:  p.Bg,
		Borders:     core.Edges{Bottom: 1},
		BorderColor: p.Line,
	}

	return core.View{
		Ref:    ed.focus.ctrl.rootRef(RegionHeader),
		TestID: "editor-toolbar",
		Role:   core.RoleRegion,
		Label:  ed.T("region.header"),
		Focusable: func() bool {
			return ed.focus.ctrl != nil && ed.focus.ctrl.fallbackRegion.Get() == RegionHeader
		},
		OnFocusIn: func() {
			if ed.focus.ctrl != nil {
				ed.focus.ctrl.onRegionFocusIn(RegionHeader)
			}
		},
		OnFocusOut: func() {
			if ed.focus.ctrl != nil {
				ed.focus.ctrl.onRegionFocusOut(RegionHeader)
			}
		},
		StyleFunc: func() core.Style {
			cur := ed.SpecPalette(cx)
			st := headerBaseStyle
			st.Background = cur.Bg
			st.BorderColor = cur.Line
			if ed.focus.ctrl == nil {
				return st
			}
			return ed.focus.ctrl.regionOutlineStyle(RegionHeader, st)
		},
		Children: []core.Node{
			// Left: brand mark + project / scene breadcrumb (with File menu) + unsaved dot.
			core.View{
				TestID: "header-breadcrumb",
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
				Children: []core.Node{
					core.View{
						TestID: "header-brand-mark",
						StyleFunc: func() core.Style {
							cur := ed.SpecPalette(cx)
							return core.Style{
								Width:          core.Pt(22),
								Height:         core.Pt(22),
								CornerRadius:   specRadiusControl,
								Background:     cur.Accent.Fg,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FlexShrink:     0,
							}
						},
						Children: []core.Node{
							core.Icon{
								SVG: mainIconBrandMark,
								StyleFunc: func() core.Style {
									return core.Style{
										Width:  core.Pt(12),
										Height: core.Pt(12),
										Color:  ed.SpecPalette(cx).Accent.OnFill,
									}
								},
							},
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ProjectDisplayName() != ""
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								Style: core.Style{
									Direction:  core.Row,
									AlignItems: core.AlignCenter,
									Gap:        specSpace8,
									FlexShrink: 1,
									MinWidth:   core.Pt(0),
								},
								Children: []core.Node{
									core.Text{
										TestID: "toolbar-project-name",
										ContentFunc: func() string {
											_ = rev.Get()
											return ed.ProjectDisplayName()
										},
										MaxLines:      1,
										EllipsizeMode: core.EllipsizeTail,
										StyleFunc: func() core.Style {
											return core.Style{
												FontSize:   12.5,
												FontWeight: core.FontWeightSemibold,
												Color:      ed.SpecPalette(cx).TextPrimary,
											}
										},
									},
									core.Text{
										Content: "/",
										StyleFunc: func() core.Style {
											return core.Style{
												FontSize: 12.5,
												Color:    ed.SpecPalette(cx).TextFaint,
											}
										},
									},
								},
							}
						},
					},
					ui.Menu{
						TestID: "toolbar-file",
						Label:  ed.T("toolbar.fileMenuLabel"),
						Trigger: ui.Button{
							Ref:   fileBtnRef,
							Label: ed.T("toolbar.fileMenuLabel"),
							TextFunc: func() string {
								_ = rev.Get()
								return ed.SceneBreadcrumbName()
							},
							Size:    ui.SizeSmall,
							Variant: ui.ButtonGhost,
							TextStyle: core.Style{
								FontSize: 12.5,
								Color:    p.TextMuted,
							},
							Style: core.Style{
								Height:       core.Pt(specRowHeight),
								MinHeight:    core.Pt(specRowHeight),
								MinWidth:     core.Pt(specRowHeight),
								Padding:      core.Symmetric(0, specSpace8),
								CornerRadius: specRadiusControl,
								MaxWidth:     core.Pt(220),
								FocusVisible: p.FocusRing(2),
							},
						},
						Children: []core.Node{
							ui.MenuItem{
								TestID:   "toolbar-new",
								Text:     ed.T("toolbar.newScene"),
								Disabled: noProject,
								OnSelect: func() {
									dir := "scenes"
									if ed.ScenePath != "" {
										dir = path.Dir(ed.ScenePath)
									}
									target.Set(path.Join(dir, "new.scene.yaml"))
									mode.Set(pathNewScene)
								},
							},
							ui.MenuItem{
								TestID:   "toolbar-menu-save",
								Text:     ed.T("toolbar.save"),
								Shortcut: FormatShortcut(ed.EffectiveShortcut(CmdSave), cx.Env().OS()),
								Disabled: notPlaying,
								OnSelect: func() { ed.saveFromUI() },
							},
							ui.MenuItem{
								TestID:   "toolbar-save-as",
								Text:     ed.T("toolbar.saveAs"),
								Disabled: noProject,
								OnSelect: func() {
									sp := ed.ScenePath
									if sp == "" {
										sp = "scenes/untitled.scene.yaml"
									}
									target.Set(sp)
									mode.Set(pathSaveAs)
								},
							},
							ui.MenuItem{
								TestID: "toolbar-revert",
								Text:   ed.T("toolbar.revert"),
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Playing() || ed.FilePath == "" || !dirty.Get().scene
								},
								OnSelect: func() { ed.report(ed.RevertScene()) },
							},
							ui.MenuSeparator{},
							ui.MenuItem{
								TestID: "toolbar-open-instanced-scene",
								Text:   ed.T("menu.openInstancedScene"),
								Disabled: func() bool {
									return noProject() || !ed.selectedObjectInstancesScene()
								},
								OnSelect: func() { ed.report(ed.OpenInstancedScene(ed.SelectedID)) },
							},
							ui.MenuItem{
								TestID: "toolbar-open-base-scene",
								Text:   ed.T("menu.openBaseScene"),
								Disabled: func() bool {
									return noProject() || ed.Doc == nil || ed.Doc.Inherits == ""
								},
								OnSelect: func() { ed.report(ed.OpenBaseScene()) },
							},
							ui.MenuSeparator{},
							ui.MenuItem{
								TestID:   "toolbar-menu-settings",
								Text:     ed.T("settings.title"),
								Shortcut: FormatShortcut(ed.EffectiveShortcut(CmdProjectSettings), cx.Env().OS()),
								Disabled: notPlaying,
								OnSelect: func() {
									if ed.Project != nil {
										ed.SetProjectSettingsOpen(true)
									} else {
										ed.ToggleEditorSettings()
									}
								},
							},
							ui.MenuItem{
								TestID:   "toolbar-close-tab",
								Text:     ed.T("toolbar.closeTab"),
								Disabled: notPlaying,
								OnSelect: func() { ed.report(ed.RequestCloseTab(ed.ActiveTab())) },
							},
						},
					},
					core.View{
						Style: core.Style{
							Width:          core.Pt(10),
							Height:         core.Pt(10),
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return dirty.Get().either() || ed.SaveFailed()
								},
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: "toolbar-unsaved",
										LabelFunc: func() string {
											_ = rev.Get()
											if ed.SaveFailed() {
												return ed.T("toolbar.unsavedFailedLabel")
											}
											return ed.T("toolbar.unsavedLabel")
										},
										StyleFunc: func() core.Style {
											_ = rev.Get()
											cur := ed.SpecPalette(cx)
											bg := cur.Changed.Fg
											if ed.SaveFailed() {
												bg = cur.Problem().Fg
											}
											return core.Style{
												Width:        core.Pt(6),
												Height:       core.Pt(6),
												CornerRadius: 3,
												Background:   bg,
											}
										},
									}
								},
							},
						},
					},
				},
			},
			// Flexible spacer before centered tool & mode controls.
			core.View{Style: core.Style{FlexGrow: 1}},
			// Center: Tool radio group (Select V, Move W, Rotate E, Scale R, Pan Space) + 2D / 3D radio group.
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace12,
					FlexShrink: 0,
				},
				Children: []core.Node{
					base.RadioGroup{
						TestID: "header-tool-group",
						Label:  ed.T("toolbar.group.tools"),
						StyleFunc: func() core.Style {
							cur := ed.SpecPalette(cx)
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          2,
								Padding:      core.All(3),
								CornerRadius: specRadiusButton,
								Background:   cur.Raised,
								BorderWidth:  1,
								BorderColor:  cur.LineRaised,
								FlexShrink:   0,
							}
						},
						Children: toolRadios,
					},
					base.RadioGroup{
						TestID: "header-view-mode-group",
						Label:  ed.T("toolbar.group.viewMode"),
						StyleFunc: func() core.Style {
							cur := ed.SpecPalette(cx)
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          2,
								Padding:      core.All(3),
								CornerRadius: specRadiusButton,
								Background:   cur.Raised,
								BorderWidth:  1,
								BorderColor:  cur.LineRaised,
								FlexShrink:   0,
							}
						},
						Children: []core.Node{
							viewModeRadio("view-mode-2d", "2D", ViewMode2D),
							viewModeRadio("view-mode-3d", "3D", ViewMode3D),
						},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						modeMemo := core.NewMemo(cx, func() ViewMode {
							_ = rev.Get()
							return ed.ViewMode
						})
						return core.Dynamic(func(cx *core.Scope) core.Node {
							return pluginToolsView(cx, ed, rev, notPlaying, modeMemo.Get())
						})
					}),
				},
			},
			// Flexible spacer before trailing actions.
			core.View{Style: core.Style{FlexGrow: 1}},
			// Live agent state pill (#1013): hidden when idle, shows working / waiting / pending review Keep & Revert.
			toolbarAgentPillView(cx, ed, rev),
			// Right: History (⌘Y), Search ⌘K, Play (⌘P) as the one primary button.
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
					FlexShrink: 0,
				},
				Children: []core.Node{
					base.Button{
						Ref:      historyBtnRef,
						TestID:   "toolbar-history",
						Label:    ed.T("toolbar.historyLabel"),
						Tooltip:  specTooltipNode(p, shortcutTip(ed.T("toolbar.historyLabel"), CmdHistoryPanel)),
						Disabled: notPlaying,
						StyleFunc: func() core.Style {
							_ = rev.Get()
							cur := ed.SpecPalette(cx)
							bg := cur.Raised
							if ed.HistoryPanelOpen() {
								bg = cur.LineRaised
							}
							return core.Style{
								Width:          core.Pt(specButtonHeight),
								Height:         core.Pt(specButtonHeight),
								MinWidth:       core.Pt(specRowHeight),
								MinHeight:      core.Pt(specRowHeight),
								CornerRadius:   specRadiusButton,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Background:     bg,
								BorderWidth:    1,
								BorderColor:    cur.LineRaised,
								Cursor:         core.CursorPointer,
								Hover:          &core.Style{Background: cur.LineRaised},
								Disabled:       &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault},
								FocusVisible:   cur.FocusRing(2),
							}
						},
						OnPress: func(core.PressEvent) {
							ed.ToggleHistoryPanel()
						},
						Children: []core.Node{
							core.Icon{
								SVG: mainIconHistory,
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									col := cur.TextFaint
									if ed.HistoryPanelOpen() {
										col = cur.TextPrimary
									}
									return core.Style{
										Width:  core.Pt(specIconSize),
										Height: core.Pt(specIconSize),
										Color:  col,
									}
								},
							},
						},
					},
					base.Button{
						Ref:     paletteBtnRef,
						TestID:  "toolbar-palette",
						Label:   ed.T("toolbar.searchLabel"),
						Tooltip: specTooltipNode(p, shortcutTip(ed.T("toolbar.search"), CmdCommandPalette)),
						StyleFunc: func() core.Style {
							cur := ed.SpecPalette(cx)
							return core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Height:         core.Pt(specButtonHeight),
								MinHeight:      core.Pt(specRowHeight),
								MinWidth:       core.Pt(specRowHeight),
								Padding:        core.Symmetric(0, specSpace12),
								Gap:            specSpace8,
								CornerRadius:   specRadiusButton,
								Background:     cur.Raised,
								BorderWidth:    1,
								BorderColor:    cur.LineRaised,
								Cursor:         core.CursorPointer,
								Hover:          &core.Style{Background: cur.LineRaised},
								FocusVisible:   cur.FocusRing(2),
							}
						},
						OnPress: func(core.PressEvent) {
							if ed.focus.ctrl != nil {
								ed.focus.ctrl.recordDialogOpener(paletteBtnRef)
							}
							ed.ToggleCommandPalette()
						},
						Children: []core.Node{
							core.Icon{
								SVG: mainIconSearch,
								StyleFunc: func() core.Style {
									return core.Style{
										Width:  core.Pt(specIconSize),
										Height: core.Pt(specIconSize),
										Color:  ed.SpecPalette(cx).TextFaint,
									}
								},
							},
							core.Text{
								Content:  ed.T("toolbar.search"),
								MaxLines: 1,
								StyleFunc: func() core.Style {
									return core.Style{
										FontSize: 12.5,
										Color:    ed.SpecPalette(cx).TextFaint,
									}
								},
							},
							core.Text{
								TestID: "toolbar-search-shortcut",
								ContentFunc: func() string {
									_ = rev.Get()
									return FormatShortcut(ed.EffectiveShortcut(CmdCommandPalette), cx.Env().OS())
								},
								MaxLines: 1,
								StyleFunc: func() core.Style {
									return core.Style{
										FontFamily: core.FontMonospace,
										FontSize:   11.5,
										Color:      ed.SpecPalette(cx).TextFaint,
									}
								},
							},
						},
					},
					base.Button{
						Ref:    playBtnRef,
						TestID: "toolbar-play",
						LabelFunc: func() string {
							_ = rev.Get()
							if ed.Playing() {
								return ed.T("toolbar.stop")
							}
							return ed.T("toolbar.play")
						},
						Tooltip: specTooltipNode(p, shortcutTip(ed.T("toolbar.play"), CmdPlayStop)),
						StyleFunc: func() core.Style {
							cur := ed.SpecPalette(cx)
							return core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Height:         core.Pt(specButtonHeight),
								MinHeight:      core.Pt(specRowHeight),
								MinWidth:       core.Pt(specRowHeight),
								Padding:        core.Symmetric(0, specSpace12),
								Gap:            specSpace8,
								CornerRadius:   specRadiusButton,
								Background:     cur.TextPrimary,
								Cursor:         core.CursorPointer,
								Hover:          &core.Style{Background: specPrimaryHover(cur)},
								FocusVisible:   cur.FocusRing(2),
							}
						},
						OnPress: func(core.PressEvent) {
							if ed.Playing() {
								_ = ed.Stop()
							} else {
								_ = ed.Play()
							}
						},
						Children: []core.Node{
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.Playing()
								},
								Then: func(cx *core.Scope) core.Node {
									return core.Icon{
										SVG: mainIconStop,
										StyleFunc: func() core.Style {
											return core.Style{
												Width:  core.Pt(10),
												Height: core.Pt(10),
												Color:  ed.SpecPalette(cx).OnTextPrimary,
											}
										},
									}
								},
								Else: func(cx *core.Scope) core.Node {
									return core.Icon{
										SVG: mainIconPlay,
										StyleFunc: func() core.Style {
											return core.Style{
												Width:  core.Pt(10),
												Height: core.Pt(10),
												Color:  ed.SpecPalette(cx).OnTextPrimary,
											}
										},
									}
								},
							},
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.Playing() {
										return ed.T("toolbar.stop")
									}
									return ed.T("toolbar.play")
								},
								MaxLines: 1,
								StyleFunc: func() core.Style {
									return core.Style{
										FontSize:   12.5,
										FontWeight: core.FontWeightSemibold,
										Color:      ed.SpecPalette(cx).OnTextPrimary,
									}
								},
							},
						},
					},
				},
			},
		},
	}
}

// leftRailSceneView renders the 232 pt AI-first left rail matching Main.dc.html:
// Scene / Assets switch, SCENE · N nodes (or designed empty/loading/error state),
// and FILES listing the open scene, prefabs it uses, and game.yaml.
func leftRailSceneView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	wsMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.workspaceStateKey()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	filesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		b.WriteString(ed.ResolvedLanguage())
		b.WriteByte('|')
		b.WriteString(ed.ScenePath)
		b.WriteByte('|')
		b.WriteString(ed.FilePath)
		b.WriteByte('|')
		b.WriteString(ed.ProjectPath)
		b.WriteByte('|')
		for _, pf := range ed.SceneUsedPrefabs() {
			b.WriteString(pf)
			b.WriteByte(',')
		}
		if darkMemo.Get() {
			b.WriteString("|D")
		} else {
			b.WriteString("|L")
		}
		return b.String()
	})

	return core.View{
		TestID: "editor-scene-column",
		StyleFunc: func() core.Style {
			cur := ed.SpecPalette(cx)
			return core.Style{
				Width:       core.Pt(232),
				FlexShrink:  0,
				Background:  cur.Bg,
				Borders:     core.Edges{Right: 1},
				BorderColor: cur.Line,
				MinHeight:   core.Pt(0),
				Overflow:    core.Hidden,
			}
		},
		Children: []core.Node{
			LeftRailSwitchView(cx, ed, rev),
			// SCENE · N nodes header
			core.View{
				TestID: "scene-rail-header",
				Style: core.Style{
					Height:         core.Pt(36),
					FlexShrink:     0,
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(0, specSpace12),
				},
				Children: []core.Node{
					core.Text{
						TestID:  "scene-rail-title",
						Content: strings.ToUpper(ed.T("sceneTree.sectionTitle")),
						StyleFunc: func() core.Style {
							return core.Style{
								FontSize:      11,
								FontWeight:    core.FontWeightSemibold,
								LetterSpacing: 0.66,
								Color:         ed.SpecPalette(cx).TextFaint,
							}
						},
					},
					core.Text{
						TestID: "scene-rail-node-count",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.WorkspaceState() != WorkspaceReady {
								return ""
							}
							n := ed.SceneNodeCount()
							if n == 1 {
								return ed.T("sceneTree.nodeCountOne")
							}
							return ed.T("sceneTree.nodeCount", l10n.Param("count", n))
						},
						StyleFunc: func() core.Style {
							return core.Style{
								FontSize:   11,
								FontWeight: core.FontWeightMedium,
								Color:      ed.SpecPalette(cx).TextFaint,
							}
						},
					},
				},
			},
			// Scene tree or designed empty/loading/error state.
			core.View{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinHeight: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = wsMemo.Get()
						ws := ed.WorkspaceState()
						p := SpecPaletteFor(darkMemo.Get())
						switch ws {
						case WorkspaceLoading:
							return core.View{
								Style: core.Style{Padding: core.All(specSpace12)},
								Children: []core.Node{
									specLoadingState(p, "left-rail-loading", ed.T("workspace.loadingProject")),
								},
							}
						case WorkspaceSceneError:
							return core.View{
								Style: core.Style{Padding: core.All(specSpace12), Gap: specSpace8},
								Children: []core.Node{
									specStatusBanner(p, "left-rail-scene-error", false, ed.T("workspace.sceneErrorShort"), nil, nil),
									specPanelButton(p, specPanelButtonSpec{
										TestID:     "left-rail-retry-open",
										Text:       ed.T("workspace.retry"),
										Variant:    specBtnQuiet,
										Compact:    true,
										InsetFocus: true,
										OnPress:    func() { _ = ed.RetryOpenScene() },
									}),
								},
							}
						case WorkspaceNoProject:
							return core.View{
								Style: core.Style{Padding: core.All(specSpace12)},
								Children: []core.Node{
									specEmptyState(p, "left-rail-empty-no-project", lucide.FolderOpen, p.TextFaint,
										ed.T("workspace.noProjectTitle"),
										ed.T("workspace.noProjectRailHint"),
										nil),
								},
							}
						case WorkspaceNoScene:
							return core.View{
								Style: core.Style{Padding: core.All(specSpace12)},
								Children: []core.Node{
									specEmptyState(p, "left-rail-empty-no-scene", lucide.FileCode, p.TextFaint,
										ed.T("workspace.noSceneTitle"),
										ed.T("workspace.noSceneRailHint"),
										nil),
								},
							}
						default:
							return SceneTreeView(cx, ed, rev)
						}
					}),
				},
			},
			// Bottom FILES section: open scene, prefabs it uses, game.yaml.
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = filesKey.Get()
				p := SpecPaletteFor(darkMemo.Get())
				type railFileEntry struct {
					testID string
					path   string
					dot    core.Color
					onOpen func()
				}
				var entries []railFileEntry
				if ed.ScenePath != "" {
					sp := ed.ScenePath
					entries = append(entries, railFileEntry{
						testID: "rail-file-scene",
						path:   sp,
						dot:    p.Accent.Fg,
						onOpen: func() { ed.report(ed.OpenProjectFile(sp)) },
					})
				} else if ed.FilePath != "" {
					fp := filepath.Base(ed.FilePath)
					entries = append(entries, railFileEntry{
						testID: "rail-file-scene",
						path:   fp,
						dot:    p.Accent.Fg,
						onOpen: func() {},
					})
				}
				for i, pref := range ed.SceneUsedPrefabs() {
					pf := pref
					entries = append(entries, railFileEntry{
						testID: fmt.Sprintf("rail-file-prefab-%d", i),
						path:   pf,
						dot:    p.Agent.Fg,
						onOpen: func() {
							if ed.Project != nil {
								ed.report(ed.OpenProjectFile(pf))
							}
						},
					})
				}
				if ed.Project != nil && ed.ProjectPath != "" {
					entries = append(entries, railFileEntry{
						testID: "rail-file-project",
						path:   project.FileName,
						dot:    p.TextFaint,
						onOpen: func() { ed.report(ed.OpenProjectFile(project.FileName)) },
					})
				}

				rows := []core.Node{
					core.Text{
						TestID:  "left-rail-files-title",
						Content: strings.ToUpper(ed.T("files.sectionTitle")),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.66,
							Color:         p.TextFaint,
							Padding:       core.Symmetric(0, specSpace4),
						},
					},
				}
				if len(entries) == 0 {
					rows = append(rows, core.Text{
						TestID:  "left-rail-files-empty",
						Content: ed.T("files.railEmpty"),
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextFaint,
							Padding:  core.Symmetric(specSpace4, specSpace8),
						},
					})
				} else {
					fileItems := make([]core.Node, 0, len(entries))
					for _, ent := range entries {
						e := ent
						fileItems = append(fileItems, base.Button{
							TestID: e.testID,
							Label:  e.path,
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Height:       core.Pt(specRowHeight),
								MinHeight:    core.Pt(specRowHeight),
								Width:        core.Pct(100),
								Padding:      core.Symmetric(0, specSpace8),
								Gap:          specSpace8,
								CornerRadius: specRadiusControl,
								Background:   core.Transparent,
								Cursor:       core.CursorPointer,
								Hover:        &core.Style{Background: p.Raised},
								FocusVisible: p.FocusRing(-2),
							},
							OnPress: func(core.PressEvent) {
								if !ed.Playing() {
									e.onOpen()
								}
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 2,
										Background:   e.dot,
										FlexShrink:   0,
									},
								},
								core.Text{
									Content:       e.path,
									MaxLines:      1,
									EllipsizeMode: core.EllipsizeMiddle,
									Style: core.Style{
										FontSize:   12,
										Color:      p.TextMuted,
										FlexGrow:   1,
										FlexShrink: 1,
									},
								},
							},
						})
					}
					rows = append(rows, core.Scroll{
						Style: core.Style{
							MaxHeight: core.Pt(160),
							Width:     core.Pct(100),
						},
						Children: []core.Node{
							core.View{
								Style:    core.Style{Gap: specSpace4},
								Children: fileItems,
							},
						},
					})
				}
				return core.View{
					TestID: "left-rail-files",
					Style: core.Style{
						FlexShrink:  0,
						Padding:     core.All(specSpace8),
						Gap:         specSpace4,
						Borders:     core.Edges{Top: 1},
						BorderColor: p.Line,
					},
					Children: rows,
				}
			}),
		},
	}
}

// viewportCameraCaptionView renders the "Camera · W × H" caption positioned above the
// 2D camera frame matching docs/design/editor/Main.dc.html.
func viewportCameraCaptionView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.ViewMode == ViewMode2D && !ed.Playing() && ed.WorkspaceState() == WorkspaceReady
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID:        "viewport-camera-caption",
				PointerEvents: core.PointerEventsNone,
				ContentFunc: func() string {
					_ = rev.Get()
					vw, vh := ed.ViewportSize()
					return fmt.Sprintf("Camera · %d × %d", int(math.Round(float64(vw))), int(math.Round(float64(vh))))
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					cur := ed.SpecPalette(cx)
					cx0, cy0 := ed.ScreenToCanvas(0, 0)
					cw, ch := ed.CanvasSize()
					left := core.Points(cx0)
					top := core.Points(cy0) - 20
					if cw <= 0 || ch <= 0 || left < 12 || left > core.Points(cw)-140 || top < 8 || top > core.Points(ch)-56 {
						left = 16
						top = 12
					}
					return core.Style{
						Position: core.Absolute,
						Left:     core.Pt(left),
						Top:      core.Pt(top),
						FontSize: 11,
						Color:    cur.TextFaint,
					}
				},
			}
		},
	}
}

// viewportWorkspaceStateOverlay renders designed empty, loading, and error states over the viewport
// whenever WorkspaceState() != WorkspaceReady so a missing or malformed scene never shows a blank viewport.
func viewportWorkspaceStateOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	wsMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Playing() {
			return "playing"
		}
		return ed.workspaceStateKey()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		if wsMemo.Get() == "playing" {
			return nil
		}
		ws := ed.WorkspaceState()
		if ws == WorkspaceReady {
			return nil
		}
		p := SpecPaletteFor(darkMemo.Get())
		var card core.Node
		switch ws {
		case WorkspaceLoading:
			detail := ed.ProjectLoadingPath()
			children := []core.Node{
				specLoadingState(p, "viewport-loading-card", ed.T("workspace.loadingProject")),
			}
			if detail != "" {
				children = append(children, core.Text{
					TestID:  "viewport-loading-path",
					Content: detail,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextFaint,
					},
				})
			}
			card = core.View{
				TestID: "viewport-loading-state",
				Role:   core.RoleStatus,
				Label:  ed.T("workspace.loadingProject"),
				Style: core.Style{
					Width:        core.Pt(420),
					MaxWidth:     core.Pct(90),
					Padding:      core.All(specSpace24),
					Gap:          specSpace12,
					CornerRadius: specRadiusCard,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: children,
			}

		case WorkspaceSceneError:
			se := ed.SceneOpenError()
			loc := ""
			msg := ed.T("workspace.sceneErrorTitle")
			fix := ""
			if se != nil {
				loc = se.Path
				if se.Line > 0 {
					if se.Column > 0 {
						loc = fmt.Sprintf("%s:%d:%d", se.Path, se.Line, se.Column)
					} else {
						loc = fmt.Sprintf("%s:%d", se.Path, se.Line)
					}
				}
				if se.Message != "" {
					msg = se.Message
				}
				fix = se.Fix
			}
			errChildren := []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        specSpace8,
					},
					Children: []core.Node{
						specIcon(lucide.TriangleAlert, 18, p.Problem().Fg),
						core.Text{
							TestID:  "viewport-error-title",
							Content: ed.T("workspace.sceneErrorTitle"),
							Style: core.Style{
								FontSize:   14,
								FontWeight: core.FontWeightSemibold,
								Color:      p.TextPrimary,
							},
						},
					},
				},
			}
			if loc != "" {
				errChildren = append(errChildren, core.Text{
					TestID:  "viewport-error-path",
					Content: loc,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.Problem().Fg,
					},
				})
			}
			errChildren = append(errChildren, core.Text{
				TestID:  "viewport-error-message",
				Content: msg,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			})
			if fix != "" {
				errChildren = append(errChildren, core.Text{
					TestID:  "viewport-error-fix",
					Content: fix,
					Style: core.Style{
						FontSize: 12,
						Color:    p.TextFaint,
					},
				})
			}
			actions := []core.Node{
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "viewport-error-retry",
					Text:    ed.T("workspace.retry"),
					Variant: specBtnPrimary,
					OnPress: func() { _ = ed.RetryOpenScene() },
				}),
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "viewport-error-dismiss",
					Text:    ed.T("workspace.dismiss"),
					Variant: specBtnQuiet,
					OnPress: func() { ed.ClearSceneOpenError() },
				}),
			}
			if ed.Project != nil {
				actions = append(actions, specPanelButton(p, specPanelButtonSpec{
					TestID:  "viewport-error-open-palette",
					Text:    ed.T("workspace.openAnotherScene"),
					Variant: specBtnSecondary,
					OnPress: func() { ed.OpenCommandPalette() },
				}))
			}
			errChildren = append(errChildren, core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
					Margin:     core.Edges{Top: specSpace4},
				},
				Children: actions,
			})
			card = core.View{
				TestID: "viewport-error-state",
				Role:   core.RoleAlert,
				Label:  ed.T("workspace.sceneErrorTitle"),
				Style: core.Style{
					Width:        core.Pt(480),
					MaxWidth:     core.Pct(90),
					Padding:      core.All(specSpace24),
					Gap:          specSpace12,
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.Problem().Edge,
				},
				Children: errChildren,
			}

		case WorkspaceNoProject:
			card = core.View{
				TestID: "viewport-empty-no-project",
				Role:   core.RoleRegion,
				Label:  ed.T("workspace.noProjectTitle"),
				Style: core.Style{
					Width:        core.Pt(420),
					MaxWidth:     core.Pct(90),
					Padding:      core.All(specSpace24),
					Gap:          specSpace12,
					CornerRadius: specRadiusCard,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
						Children: []core.Node{
							specIcon(lucide.FolderOpen, 18, p.TextMuted),
							core.Text{
								Content: ed.T("workspace.noProjectTitle"),
								Style: core.Style{
									FontSize:   14,
									FontWeight: core.FontWeightSemibold,
									Color:      p.TextPrimary,
								},
							},
						},
					},
					core.Text{
						Content: ed.T("workspace.noProjectDesc"),
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextFaint,
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
						Children: []core.Node{
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "viewport-empty-open-project",
								Text:    ed.T("workspace.openProject"),
								Variant: specBtnPrimary,
								OnPress: func() { ed.report(ed.BrowseAndOpenProject()) },
							}),
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "viewport-empty-start-screen",
								Text:    ed.T("workspace.startScreen"),
								Variant: specBtnQuiet,
								OnPress: func() { ed.SetStartScreenOpen(true) },
							}),
						},
					},
				},
			}

		case WorkspaceNoScene:
			card = core.View{
				TestID: "viewport-empty-no-scene",
				Role:   core.RoleRegion,
				Label:  ed.T("workspace.noSceneTitle"),
				Style: core.Style{
					Width:        core.Pt(420),
					MaxWidth:     core.Pct(90),
					Padding:      core.All(specSpace24),
					Gap:          specSpace12,
					CornerRadius: specRadiusCard,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
						Children: []core.Node{
							specIcon(lucide.FileCode, 18, p.TextMuted),
							core.Text{
								Content: ed.T("workspace.noSceneTitle"),
								Style: core.Style{
									FontSize:   14,
									FontWeight: core.FontWeightSemibold,
									Color:      p.TextPrimary,
								},
							},
						},
					},
					core.Text{
						Content: ed.T("workspace.noSceneDesc"),
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextFaint,
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
						Children: []core.Node{
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "viewport-empty-search-files",
								Text:    ed.T("workspace.searchFiles"),
								Variant: specBtnPrimary,
								OnPress: func() { ed.OpenCommandPalette() },
							}),
						},
					},
				},
			}
		}

		return core.View{
			TestID: "viewport-state-overlay",
			Style: core.Style{
				Position:       core.Absolute,
				Top:            core.Pt(0),
				Right:          core.Pt(0),
				Bottom:         core.Pt(0),
				Left:           core.Pt(0),
				Background:     p.Viewport,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Padding:        core.All(specSpace24),
			},
			Children: []core.Node{card},
		}
	})
}

// inspectorWorkspaceStateView renders designed empty, loading, or error states inside the Inspector tab
// when WorkspaceState() != WorkspaceReady. Returns nil when WorkspaceReady.
func inspectorWorkspaceStateView(cx *core.Scope, ed *Editor, ws WorkspaceState) core.Node {
	if ws == WorkspaceReady {
		return nil
	}
	p := ed.SpecPalette(cx)
	var body core.Node
	switch ws {
	case WorkspaceLoading:
		body = specLoadingState(p, "inspector-loading-state", ed.T("workspace.loadingProject"))
	case WorkspaceSceneError:
		se := ed.SceneOpenError()
		detail := ed.T("workspace.sceneErrorShort")
		if se != nil && se.Path != "" {
			detail = se.Path
		}
		body = specStatusBanner(p, "inspector-error-state", false, detail, nil, nil)
	case WorkspaceNoProject:
		body = specEmptyState(p, "inspector-empty-no-project", lucide.FolderOpen, p.TextFaint,
			ed.T("workspace.noProjectTitle"),
			ed.T("workspace.noProjectInspectorHint"),
			nil)
	case WorkspaceNoScene:
		body = specEmptyState(p, "inspector-empty-no-scene", lucide.FileCode, p.TextFaint,
			ed.T("workspace.noSceneTitle"),
			ed.T("workspace.noSceneInspectorHint"),
			nil)
	}
	return core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Padding:    core.All(specSpace16),
			Background: p.Panel,
		},
		Children: []core.Node{body},
	}
}

// mainStatusBarView renders the 32 px Spec status bar in muted text on bg:
// active layer, live pointer coordinates, current tool on the left;
// problem count (opens Problems), Output, and Debugger on the right.
func mainStatusBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	pointerRev := core.NewSignal(cx, ed.PointerRevision())
	unsubPtr := ed.SubscribePointer(func() {
		pointerRev.Set(ed.PointerRevision())
	})
	cx.Cleanup(unsubPtr)

	layerBtnRef := &core.Ref{}
	problemsBtnRef := &core.Ref{}
	outputBtnRef := &core.Ref{}
	debuggerBtnRef := &core.Ref{}
	changesBtnRef := &core.Ref{}
	conflictsBtnRef := &core.Ref{}
	playtestBtnRef := &core.Ref{}
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, layerBtnRef, nil)
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, problemsBtnRef, func() bool { return !ed.Playing() })
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, outputBtnRef, nil)
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, debuggerBtnRef, nil)
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, changesBtnRef, func() bool {
			return ed.Project != nil && ed.ProjectPath != "" && !ed.Playing()
		})
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, conflictsBtnRef, func() bool {
			return len(ed.DetectedMergeConflicts()) > 0
		})
		ed.focus.ctrl.registerCandidate(cx, RegionStatusBar, playtestBtnRef, func() bool {
			return ed.ActivePlaytestReport() != nil
		})
	}

	statusBtnStyle := func(active bool) core.Style {
		cur := ed.SpecPalette(cx)
		bg := core.Transparent
		if active {
			bg = cur.Raised
		}
		return core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Height:         core.Pt(specRowHeight),
			MinHeight:      core.Pt(specRowHeight),
			MinWidth:       core.Pt(specRowHeight),
			Padding:        core.Symmetric(0, specSpace8),
			Gap:            specSpace4,
			CornerRadius:   specRadiusControl,
			Background:     bg,
			Cursor:         core.CursorPointer,
			Hover:          &core.Style{Background: cur.Raised},
			FocusVisible:   cur.FocusRing(-2),
		}
	}

	return core.View{
		Ref:    ed.focus.ctrl.rootRef(RegionStatusBar),
		TestID: "editor-status",
		Role:   core.RoleRegion,
		Label:  ed.T("region.statusBar"),
		Focusable: func() bool {
			if ed.focus.ctrl == nil {
				return false
			}
			return !ed.focus.ctrl.hasEnabledCandidate(RegionStatusBar) || ed.focus.ctrl.fallbackRegion.Get() == RegionStatusBar
		},
		OnFocusIn: func() {
			if ed.focus.ctrl != nil {
				ed.focus.ctrl.onRegionFocusIn(RegionStatusBar)
			}
		},
		OnFocusOut: func() {
			if ed.focus.ctrl != nil {
				ed.focus.ctrl.onRegionFocusOut(RegionStatusBar)
			}
		},
		StyleFunc: func() core.Style {
			cur := ed.SpecPalette(cx)
			baseStyle := core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Height:      core.Pt(32),
				FlexShrink:  0,
				Gap:         specSpace12,
				Padding:     core.Symmetric(0, specSpace12),
				Background:  cur.Bg,
				Borders:     core.Edges{Top: 1},
				BorderColor: cur.Line,
			}
			if ed.focus.ctrl == nil {
				return baseStyle
			}
			return ed.focus.ctrl.regionOutlineStyle(RegionStatusBar, baseStyle)
		},
		Children: []core.Node{
			// Left: active layer, pointer coordinates, current tool (muted text on bg).
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace12,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
				Children: []core.Node{
					base.Button{
						Ref:    layerBtnRef,
						TestID: "status-active-layer",
						LabelFunc: func() string {
							_ = rev.Get()
							return ed.T("status.activeLayerLabel", l10n.Param("layer", ed.activeLayerReadout()))
						},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							return statusBtnStyle(ed.LayersPanelOpen())
						},
						OnPress: func(core.PressEvent) {
							ed.SetLayersPanelOpen(!ed.LayersPanelOpen())
						},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									switch ed.WorkspaceState() {
									case WorkspaceLoading:
										return ed.T("workspace.loadingShort")
									case WorkspaceSceneError:
										return ed.T("workspace.sceneErrorShort")
									case WorkspaceNoProject:
										return ed.T("workspace.noProjectTitle")
									case WorkspaceNoScene:
										return ed.T("workspace.noSceneTitle")
									default:
										return ed.T("status.layerReadout", l10n.Param("layer", ed.activeLayerReadout()))
									}
								},
								MaxLines: 1,
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									col := cur.TextFaint
									if ed.LayersPanelOpen() {
										col = cur.TextPrimary
									} else if ed.WorkspaceState() == WorkspaceSceneError {
										col = cur.Problem().Fg
									}
									return core.Style{
										FontSize: 11.5,
										Color:    col,
									}
								},
							},
						},
					},
					core.Text{
						TestID: "status-pointer-coords",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.WorkspaceState() != WorkspaceReady || (!ed.Playing() && ed.ViewMode == ViewMode2D && ed.SketchActive()) {
								return ""
							}
							_ = pointerRev.Get()
							if ed.ViewMode == ViewMode2D {
								if wx, wy, ok := ed.ViewportPointer2DWorld(); ok {
									return fmt.Sprintf("%d, %d", int(math.Round(float64(wx))), int(math.Round(float64(wy))))
								}
							}
							return "—, —"
						},
						MaxLines: 1,
						StyleFunc: func() core.Style {
							return core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   11.5,
								Color:      ed.SpecPalette(cx).TextFaint,
							}
						},
					},
					core.Text{
						TestID: "editor-status-mode",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.WorkspaceState() != WorkspaceReady {
								return ""
							}
							if ed.ViewMode == ViewMode3D {
								if gt := ed.GridMapTool(); gt != GridMapToolNone {
									return fmt.Sprintf("%s · %s · Floor %d · %d/24", ed.viewStatus3D(), gt.String(), ed.GridMapFloor(), ed.GridMapOrientation())
								}
								space := ed.T("gizmo.spaceGlobal")
								if ed.GizmoSpace3D == GizmoSpaceLocal {
									space = ed.T("gizmo.spaceLocal")
								}
								s := fmt.Sprintf("%s · %s · %s", ed.viewStatus3D(), ed.localizedGizmoName(ed.GizmoMode), space)
								if ed.Tool == ToolPlugin && ed.activePluginToolSupports3D() {
									s += " · " + ed.toolLabel()
								}
								return s
							}
							if !ed.Playing() && ed.SketchActive() {
								_ = pointerRev.Get()
								sx, sy := ed.SketchStatusSceneCoords()
								return fmt.Sprintf("Sketch layer · scene %d, %d · Sketch · %s",
									int(math.Round(float64(sx))),
									int(math.Round(float64(sy))),
									ed.SketchTool().String(),
								)
							}
							tool := ed.toolLabel()
							switch ed.Tool {
							case ToolPaintTile, ToolFillTile, ToolRectTile, ToolLineTile:
								if tile, ok := ed.TilePalette().Tile(ed.ActiveTileID); ok {
									tool += ed.T("status.tileSuffix", l10n.Param("tile", tile.Number))
								}
							case ToolTerrain:
								if tp := ed.TilePalette(); tp.ActiveTerrain != "" {
									tool += ed.T("status.terrainSuffix", l10n.Param("terrain", tp.ActiveTerrain))
								}
							case ToolStampTile:
								if stamp, ok := ed.TileStamp(); ok {
									tool += fmt.Sprintf(" | Stamp: %dx%d", stamp.Width, stamp.Height)
								} else if tile, ok := ed.TilePalette().Tile(ed.ActiveTileID); ok {
									tool += ed.T("status.tileSuffix", l10n.Param("tile", tile.Number))
								}
							case ToolRandomTile:
								tool += fmt.Sprintf(" | Random: %d tiles (seed %d)", len(ed.effectiveRandomPool()), ed.RandomBrushSeed())
							}
							return tool
						},
						MaxLines: 1,
						StyleFunc: func() core.Style {
							return core.Style{
								FontSize: 11.5,
								Color:    ed.SpecPalette(cx).TextFaint,
							}
						},
					},
					core.Text{
						TestID: "editor-status-message",
						ContentFunc: func() string {
							_ = rev.Get()
							if delta := ed.GizmoDragDelta3D(); delta != "" {
								return delta
							}
							if ed.BuildErr != nil {
								return ed.T("status.buildError", l10n.Param("error", ed.BuildErr.Error()))
							}
							if !ed.Playing() && ed.ViewMode == ViewMode2D && ed.SketchActive() {
								return ed.T("sketch.statusHint")
							}
							if ed.Status == "" || ed.Status == "Ready" {
								return ""
							}
							return ed.Status
						},
						MaxLines:      1,
						EllipsizeMode: core.EllipsizeTail,
						StyleFunc: func() core.Style {
							return core.Style{
								FontSize: 11.5,
								Color:    ed.SpecPalette(cx).TextFaint,
								MaxWidth: core.Pt(240),
							}
						},
					},
				},
			},
			// Flexible spacer.
			core.View{Style: core.Style{FlexGrow: 1}},
			// Right: transient indicators + Problems + Output + Debugger.
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace8,
					FlexShrink: 0,
				},
				Children: []core.Node{
					saveStatusBarView(cx, ed, rev),
					tasksStatusIndicatorView(cx, ed, rev),
					remoteStatusView(cx, ed, rev),
					agentStatusBarActivityView(cx, ed, rev),
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.Project != nil && ed.ProjectPath != "" && ed.ChangesCount() > 0
						},
						Then: func(cx *core.Scope) core.Node {
							return base.Button{
								Ref:    changesBtnRef,
								TestID: "status-changes",
								LabelFunc: func() string {
									_ = rev.Get()
									return ed.T("status.changesCount", l10n.Param("count", ed.ChangesCount()))
								},
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Playing()
								},
								StyleFunc: func() core.Style { return statusBtnStyle(false) },
								OnPress: func(core.PressEvent) {
									ed.report(ed.OpenChangesPanel())
								},
								Children: []core.Node{
									core.Icon{
										SVG: lucide.GitBranch(),
										StyleFunc: func() core.Style {
											return core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: ed.SpecPalette(cx).TextFaint, FlexShrink: 0}
										},
									},
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											return ed.T("status.changesCount", l10n.Param("count", ed.ChangesCount()))
										},
										StyleFunc: func() core.Style {
											return core.Style{FontSize: 11.5, Color: ed.SpecPalette(cx).TextFaint}
										},
									},
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return len(ed.DetectedMergeConflicts()) > 0
						},
						Then: func(cx *core.Scope) core.Node {
							return base.Button{
								Ref:    conflictsBtnRef,
								TestID: "status-merge-conflicts",
								Label:  ed.T("status.mergeConflictsLabel"),
								StyleFunc: func() core.Style {
									cur := ed.SpecPalette(cx)
									st := statusBtnStyle(false)
									st.Background = cur.Warning.Fill
									st.BorderWidth = 1
									st.BorderColor = cur.Warning.Edge
									return st
								},
								OnPress: func(core.PressEvent) {
									ed.report(ed.OpenMergeConflict(""))
								},
								Children: []core.Node{
									core.Icon{
										SVG: lucide.GitMerge(),
										StyleFunc: func() core.Style {
											return core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: ed.SpecPalette(cx).Warning.Fg, FlexShrink: 0}
										},
									},
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											n := len(ed.DetectedMergeConflicts())
											if n == 1 {
												return "1 scene conflict"
											}
											return fmt.Sprintf("%d scene conflicts", n)
										},
										StyleFunc: func() core.Style {
											return core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: ed.SpecPalette(cx).Warning.Fg}
										},
									},
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ActivePlaytestReport() != nil
						},
						Then: func(cx *core.Scope) core.Node {
							return base.Button{
								Ref:    playtestBtnRef,
								TestID: "status-playtest-report",
								Label:  "Playtest Report",
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									st := statusBtnStyle(false)
									st.BorderWidth = 1
									st.BorderColor = cur.LineRaised
									if rep := ed.ActivePlaytestReport(); rep != nil && len(rep.MissedMetrics()) > 0 {
										st.Background = cur.Removed.Fill
									} else {
										st.Background = cur.Added.Fill
									}
									return st
								},
								OnPress: func(core.PressEvent) {
									ed.SetPlaytestReportOpen(true)
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											rep := ed.ActivePlaytestReport()
											if rep == nil {
												return "Playtest"
											}
											if missed := len(rep.MissedMetrics()); missed > 0 {
												return fmt.Sprintf("Playtest · %d missed", missed)
											}
											return "Playtest · met"
										},
										StyleFunc: func() core.Style {
											return core.Style{FontSize: 11.5, FontWeight: core.FontWeightSemibold, Color: ed.SpecPalette(cx).TextSecondary}
										},
									},
								},
							}
						},
					},
					// Problem count button (always present, green dot + "No problems" or red alert + count).
					base.Button{
						Ref:    problemsBtnRef,
						TestID: "status-problems-pill",
						Label:  ed.T("problems.openPanelLabel"),
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing()
						},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							cur := ed.SpecPalette(cx)
							st := statusBtnStyle(ed.ProblemAgentPanelOpen())
							st.Gap = 6
							if ed.ProblemCount() > 0 {
								st.Background = cur.Problem().Fill
								st.BorderWidth = 1
								st.BorderColor = cur.Problem().Edge
							}
							return st
						},
						OnPress: func(core.PressEvent) {
							if ed.Playing() || ed.StartScreenOpen() || ed.MergeConflictOpen() {
								return
							}
							ed.SetProblemAgentPanelOpen(!ed.ProblemAgentPanelOpen())
						},
						Children: []core.Node{
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.ProblemCount() > 0
								},
								Then: func(cx *core.Scope) core.Node {
									return problemAlertIconFunc(func() core.Color { return ed.SpecPalette(cx).Problem().Fg }, 12)
								},
								Else: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: "status-problems-ok-dot",
										StyleFunc: func() core.Style {
											return core.Style{
												Width:        core.Pt(6),
												Height:       core.Pt(6),
												CornerRadius: 3,
												Background:   ed.SpecPalette(cx).Added.Fg,
												FlexShrink:   0,
											}
										},
									}
								},
							},
							core.Text{
								TestID: "status-problems",
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.ProblemCount() == 0 {
										return ed.T("problems.none")
									}
									return ed.problemsStatusText()
								},
								MaxLines: 1,
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									if ed.ProblemCount() > 0 {
										return core.Style{
											FontSize:   11.5,
											FontWeight: core.FontWeightSemibold,
											Color:      cur.Problem().Fg,
										}
									}
									return core.Style{
										FontSize: 11.5,
										Color:    cur.TextFaint,
									}
								},
							},
						},
					},
					// Output button.
					base.Button{
						Ref:    outputBtnRef,
						TestID: "status-output",
						Label:  ed.T("toolbar.outputLabel"),
						StyleFunc: func() core.Style {
							_ = rev.Get()
							return statusBtnStyle(ed.OutputPanelOpen())
						},
						OnPress: func(core.PressEvent) {
							ed.SetOutputPanelOpen(!ed.OutputPanelOpen())
						},
						Children: []core.Node{
							core.Text{
								Content:  ed.T("toolbar.output"),
								MaxLines: 1,
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									col := cur.TextFaint
									if ed.OutputPanelOpen() {
										col = cur.TextPrimary
									}
									return core.Style{
										FontSize: 11.5,
										Color:    col,
									}
								},
							},
						},
					},
					// Debugger button.
					base.Button{
						Ref:    debuggerBtnRef,
						TestID: "status-debugger",
						Label:  ed.T("status.debuggerLabel"),
						StyleFunc: func() core.Style {
							_ = rev.Get()
							return statusBtnStyle(ed.ProfilerOpen())
						},
						OnPress: func(core.PressEvent) {
							ed.SetProfilerOpen(!ed.ProfilerOpen())
						},
						Children: []core.Node{
							core.Text{
								Content:  ed.T("status.debugger"),
								MaxLines: 1,
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									col := cur.TextFaint
									if ed.ProfilerOpen() {
										col = cur.TextPrimary
									}
									return core.Style{
										FontSize: 11.5,
										Color:    col,
									}
								},
							},
						},
					},
				},
			},
		},
	}
}
