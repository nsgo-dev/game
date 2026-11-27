package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// SceneTreeFilter returns the current left-rail Scene Tree search query.
func (e *Editor) SceneTreeFilter() string {
	if e == nil {
		return ""
	}
	return e.sceneTreeFilter
}

// SetSceneTreeFilter updates the left-rail Scene Tree search query.
func (e *Editor) SetSceneTreeFilter(query string) {
	if e == nil || e.sceneTreeFilter == query {
		return
	}
	e.sceneTreeFilter = query
	e.notify()
}

// SceneTreeFilterOpen reports whether the left-rail Scene Tree filter bar is visible.
func (e *Editor) SceneTreeFilterOpen() bool {
	return e != nil && e.sceneTreeFilterOpen
}

// SetSceneTreeFilterOpen shows or hides the left-rail Scene Tree filter bar.
// Closing the filter bar clears any active filter so hidden nodes are not left obscured.
func (e *Editor) SetSceneTreeFilterOpen(open bool) {
	if e == nil {
		return
	}
	changed := e.sceneTreeFilterOpen != open
	e.sceneTreeFilterOpen = open
	if !open && e.sceneTreeFilter != "" {
		e.sceneTreeFilter = ""
		changed = true
	}
	if changed {
		e.notify()
	}
}

// AddSceneRailObject creates a new object of kind ("empty", "trigger", or "light") on the active layer
// at the current 2D view center (or world origin in 3D), selects it, and switches the right rail to Inspector.
func (e *Editor) AddSceneRailObject(kind string) (string, error) {
	if e == nil {
		return "", fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if err := e.lockedLayerErr(); err != nil {
		return "", err
	}
	layerName := e.ActiveLayer
	if layerName == "" && e.Doc != nil && len(e.Doc.Layers) > 0 {
		layerName = e.Doc.Layers[0].Name
	}
	var px, py float32
	if e.ViewMode == ViewMode2D {
		v := e.View2D()
		sx, sy := e.SnapPosition2D(float64(v.X), float64(v.Y))
		px, py = float32(sx), float32(sy)
	}
	obj := sceneformat.ObjectDoc{X: px, Y: py}
	switch kind {
	case "trigger":
		obj.Name = e.T("sceneTreeExtra.defaultTriggerName")
		obj.Components = map[string]any{
			"trigger": map[string]any{},
		}
	case "light":
		obj.Name = e.T("sceneTreeExtra.defaultLightName")
		if e.ViewMode == ViewMode3D {
			obj.Light = &sceneformat.LightDoc{
				Kind:  sceneformat.LightPoint,
				Range: 12,
			}
		} else {
			obj.Components = map[string]any{
				"light2d": map[string]any{
					"radius":    float64(96),
					"intensity": float64(1),
				},
			}
		}
	default:
		obj.Name = e.T("sceneTreeExtra.defaultObjectName")
	}
	id, err := e.PlaceObject(layerName, obj)
	if err != nil {
		return "", err
	}
	e.SelectObject(id)
	e.SetRightRailTab(RightTabInspector)
	return id, nil
}

// AddSceneRailLayer creates a uniquely named world-space layer through History and makes it active.
func (e *Editor) AddSceneRailLayer() (string, error) {
	if e == nil {
		return "", fmt.Errorf("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	existing := make(map[string]bool)
	if doc := e.resolvedDoc(); doc != nil {
		for i := range doc.Layers {
			existing[doc.Layers[i].Name] = true
		}
	}
	name := ""
	for n := 1; ; n++ {
		cand := fmt.Sprintf("layer_%d", n)
		if !existing[cand] {
			name = cand
			break
		}
	}
	if err := e.AddLayer(name, sceneformat.SpaceWorld); err != nil {
		return "", err
	}
	e.SetActiveLayer(name)
	return name, nil
}

func localizedObjectKind(e *Editor, kind string) string {
	switch kind {
	case "prefab":
		return e.T("sceneTreeExtra.objKind.prefab")
	case "camera":
		return e.T("sceneTreeExtra.objKind.camera")
	case "light":
		return e.T("sceneTreeExtra.objKind.light")
	case "mesh":
		return e.T("sceneTreeExtra.objKind.mesh")
	case "group":
		return e.T("sceneTreeExtra.objKind.group")
	case "trigger":
		return e.T("sceneTreeExtra.objKind.trigger")
	case "sprite":
		return e.T("sceneTreeExtra.objKind.sprite")
	default:
		return e.T("sceneTreeExtra.objKind.node")
	}
}

func localizedLayerKind(e *Editor, kind string) string {
	switch kind {
	case "tiles":
		return e.T("sceneTreeExtra.layerKind.tiles")
	case "screen":
		return e.T("sceneTreeExtra.layerKind.screen")
	default:
		return e.T("sceneTreeExtra.layerKind.layer")
	}
}

func (e *Editor) sceneTreeFilterMatchesObject(o *sceneformat.ObjectDoc, q string) bool {
	if q == "" || o == nil {
		return true
	}
	kind := e.SceneRailObjectKind(o.ID)
	if strings.Contains(strings.ToLower(o.ID), q) ||
		strings.Contains(strings.ToLower(o.Name), q) ||
		strings.Contains(strings.ToLower(kind), q) ||
		strings.Contains(strings.ToLower(localizedObjectKind(e, kind)), q) {
		return true
	}
	for i := range o.Children {
		if e.sceneTreeFilterMatchesObject(&o.Children[i], q) {
			return true
		}
	}
	return false
}

func (e *Editor) sceneTreeFilterMatchesLayer(l *sceneformat.LayerDoc, q string) bool {
	if q == "" || l == nil {
		return true
	}
	kind := e.LayerKindLabel(l.Name)
	if strings.Contains(strings.ToLower(l.Name), q) ||
		strings.Contains(strings.ToLower(kind), q) ||
		strings.Contains(strings.ToLower(localizedLayerKind(e, kind)), q) {
		return true
	}
	for i := range l.Objects {
		if e.sceneTreeFilterMatchesObject(&l.Objects[i], q) {
			return true
		}
	}
	return false
}

// sceneTreeKey is a row of the scene tree: a layer when id is empty, else the object id
// (with removed set for objects deleted during a pending agent turn).
type sceneTreeKey struct {
	layer   string
	id      string
	removed bool
}

// sceneTreeDrag is what a row of the scene tree carries while it is dragged.
type sceneTreeDrag struct{ id string }

// treeEdit is the field a row of the scene tree is editing in place, the zero value for none.
type treeEdit struct {
	key   sceneTreeKey
	field treeField
}

type treeField int

const (
	editNone treeField = iota
	editName
	editID
)

// treeHint is where a dragged row would land on the row of key.
type treeHint struct {
	key   sceneTreeKey
	place DropPlace
	on    bool
}

// SceneTreeView renders the scene tree panel: the open scene's layers and, under each, its objects with their children nested, kept in step with the editor's revision. Pressing an object's row selects it, and selecting an object in a viewport selects and reveals its row, expanding the rows above it; pressing a layer's row makes it the active layer. Dragging a row onto another makes it that object's child, or its sibling before or after it when dropped near the row's top or bottom edge (DropObject), and dropping it on a layer's row moves it to the top level of that layer (MoveObjectToLayer); both keep its world transform. A row's context menu renames it, changes its ID, moves it up, down, into the sibling above or out of its parent, to another layer, hides or locks it, or deletes it, and F2 renames the selected object in its row. The eye and lock buttons of a row hide the object in the viewports or lock it against picking for this session (SetObjectHidden, SetObjectLocked). Every edit goes through History.
func SceneTreeView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	selection := ui.NewSelection(cx, true)
	listRef := new(core.Ref)
	// open holds the rows expanded or collapsed by hand; a layer starts expanded and an object collapsed.
	open := map[sceneTreeKey]bool{}
	openRev := core.NewSignal(cx, 0)
	editing := core.NewSignal(cx, treeEdit{})
	hint := core.NewSignal(cx, treeHint{})

	// An object's row stays open by its ID alone, so it does when the object moves to another layer.
	openKey := func(k sceneTreeKey) sceneTreeKey {
		if k.id != "" {
			return sceneTreeKey{id: k.id}
		}
		return k
	}
	expanded := func(k sceneTreeKey) bool {
		_ = openRev.Get()
		_ = rev.Get()
		if strings.TrimSpace(ed.SceneTreeFilter()) != "" {
			return true
		}
		if v, ok := open[openKey(k)]; ok {
			return v
		}
		return k.id == ""
	}
	setOpen := func(k sceneTreeKey, on bool) {
		if core.Untracked(cx, func() bool { return expanded(k) }) == on {
			return
		}
		open[openKey(k)] = on
		openRev.Set(openRev.GetUntracked() + 1)
	}
	// renamed keeps an object's row open as its ID changes.
	renamed := func(oldID, newID string) {
		if v, ok := open[sceneTreeKey{id: oldID}]; ok {
			delete(open, sceneTreeKey{id: oldID})
			open[sceneTreeKey{id: newID}] = v
			openRev.Set(openRev.GetUntracked() + 1)
		}
	}
	children := func(k sceneTreeKey) []any {
		_ = rev.Get()
		if k.removed {
			return nil
		}
		doc := ed.resolvedDoc()
		if doc == nil {
			return nil
		}
		q := strings.ToLower(strings.TrimSpace(ed.SceneTreeFilter()))
		if k.id == "" {
			for i := range doc.Layers {
				if doc.Layers[i].Name == k.layer {
					objs := doc.Layers[i].Objects
					layerKind := ed.LayerKindLabel(doc.Layers[i].Name)
					layerSelfMatches := q == "" ||
						strings.Contains(strings.ToLower(doc.Layers[i].Name), q) ||
						strings.Contains(strings.ToLower(layerKind), q) ||
						strings.Contains(strings.ToLower(localizedLayerKind(ed, layerKind)), q)
					rem := ed.AgentRemovedNodes(k.layer, "")
					out := make([]any, 0, len(objs)+len(rem))
					for j := range objs {
						if layerSelfMatches || ed.sceneTreeFilterMatchesObject(&objs[j], q) {
							out = append(out, sceneTreeKey{layer: k.layer, id: objs[j].ID})
						}
					}
					for _, r := range rem {
						if q == "" || strings.Contains(strings.ToLower(r.ObjectID), q) || strings.Contains(strings.ToLower(r.Name), q) {
							out = append(out, sceneTreeKey{layer: k.layer, id: r.ObjectID, removed: true})
						}
					}
					return out
				}
			}
			return nil
		}
		loc := locateObject(doc, k.id)
		if loc.obj == nil {
			return nil
		}
		layerName := doc.Layers[loc.layer].Name
		rem := ed.AgentRemovedNodes(layerName, k.id)
		out := make([]any, 0, len(loc.obj.Children)+len(rem))
		for i := range loc.obj.Children {
			if ed.sceneTreeFilterMatchesObject(&loc.obj.Children[i], q) {
				out = append(out, sceneTreeKey{layer: layerName, id: loc.obj.Children[i].ID})
			}
		}
		for _, r := range rem {
			if q == "" || strings.Contains(strings.ToLower(r.ObjectID), q) || strings.Contains(strings.ToLower(r.Name), q) {
				out = append(out, sceneTreeKey{layer: layerName, id: r.ObjectID, removed: true})
			}
		}
		return out
	}
	roots := func() []any {
		_ = rev.Get()
		doc := ed.resolvedDoc()
		if doc == nil {
			return nil
		}
		q := strings.ToLower(strings.TrimSpace(ed.SceneTreeFilter()))
		out := make([]any, 0, len(doc.Layers))
		for i := range doc.Layers {
			if ed.sceneTreeFilterMatchesLayer(&doc.Layers[i], q) {
				out = append(out, sceneTreeKey{layer: doc.Layers[i].Name})
			}
		}
		return out
	}
	keyOf := func(id string) sceneTreeKey {
		if _, layerName := ed.FindResolvedObject(id); layerName != "" {
			return sceneTreeKey{layer: layerName, id: id}
		}
		return sceneTreeKey{}
	}

	// A press on a row selects its object(s) in the editor (supporting Shift/Mod multi-selection), or makes its layer the active one.
	// While connection target pick mode is active, pressing an object row picks it as the target without changing SelectedID, and pressing a layer row is ignored so pick mode and SelectedID stay intact.
	var ignorePickKey sceneTreeKey
	var syncedFromEditor bool
	var syncedCursor sceneTreeKey
	var syncedIDs []string
	recordEditorSync := func(cursor sceneTreeKey, ids []string) {
		syncedFromEditor = true
		syncedCursor = cursor
		syncedIDs = slices.Clone(ids)
	}
	syncTreeToEditor := func() {
		ids := ed.SelectedIDs()
		if len(ids) == 0 {
			recordEditorSync(sceneTreeKey{}, nil)
			selection.Clear()
			selection.SetCursor(nil)
			return
		}
		keys := make([]any, 0, len(ids))
		synced := make([]string, 0, len(ids))
		for _, id := range ids {
			if k := keyOf(id); k.id != "" {
				keys = append(keys, k)
				synced = append(synced, id)
			}
		}
		if len(keys) == 0 {
			recordEditorSync(sceneTreeKey{}, nil)
			selection.Clear()
			selection.SetCursor(nil)
			return
		}
		pk := keyOf(ed.SelectedID)
		recordEditorSync(pk, synced)
		selection.Set(keys...)
		if pk.id != "" {
			selection.SetCursor(pk)
		}
	}
	cx.Effect(func() {
		cur, _ := selection.Cursor().(sceneTreeKey)
		keys := selection.Keys()
		chosen := selection.Has(cur)
		cx.Untrack(func() {
			objIDs := make([]string, 0, len(keys))
			for _, raw := range keys {
				if tk, ok := raw.(sceneTreeKey); ok && tk.id != "" && !tk.removed && !slices.Contains(objIDs, tk.id) {
					objIDs = append(objIDs, tk.id)
				}
			}
			if syncedFromEditor {
				matched := cur == syncedCursor && sameIDSet(objIDs, syncedIDs)
				syncedFromEditor = false
				syncedCursor = sceneTreeKey{}
				syncedIDs = nil
				if matched {
					return
				}
			}
			if ed.PickingConnectionTarget() {
				if cur == (sceneTreeKey{}) {
					return
				}
				if cur == ignorePickKey {
					ignorePickKey = sceneTreeKey{}
					return
				}
				ignorePickKey = sceneTreeKey{}
				if cur.id != "" {
					ed.finishConnectionPick(cur.id)
				}
				if ed.SelectedID != "" {
					if k := keyOf(ed.SelectedID); k.id != "" {
						if ed.PickingConnectionTarget() {
							ignorePickKey = k
						}
					}
				}
				syncTreeToEditor()
				return
			}
			ignorePickKey = sceneTreeKey{}
			if cur.layer != "" && cur.id == "" && chosen {
				// A layer's row is selected in place of any object selection.
				if selection.Len() != 1 {
					recordEditorSync(cur, nil)
					selection.Set(cur)
				}
				ed.ClearSelection()
				ed.SetActiveLayer(cur.layer)
				ed.SetRightRailTab(RightTabInspector)
				return
			}
			if len(objIDs) > 0 {
				primary := objIDs[len(objIDs)-1]
				if cur.id != "" && chosen && slices.Contains(objIDs, cur.id) {
					primary = cur.id
				}
				if ed.SelectedID != primary || !sameIDSet(ed.SelectedIDs(), objIDs) {
					ed.SelectObjectsWithPrimary(objIDs, primary)
				}
				ed.SetRightRailTab(RightTabInspector)
				return
			}
			if cur.id != "" && !chosen && ed.SelectionCount() > 0 {
				ed.ClearSelection()
			}
		})
	})
	// Selecting objects elsewhere (viewport click, Shift/Mod click, marquee, undo/redo) selects their rows and reveals them.
	lastTabID := ed.activeTabID()
	cx.Effect(func() {
		_ = rev.Get()
		cx.Untrack(func() {
			if tabID := ed.activeTabID(); tabID != lastTabID {
				lastTabID = tabID
				editing.Set(treeEdit{})
			}
			ids := ed.SelectedIDs()
			if len(ids) == 0 {
				hasObj := false
				for _, raw := range selection.Keys() {
					if tk, ok := raw.(sceneTreeKey); ok && tk.id != "" {
						hasObj = true
						break
					}
				}
				if cur, _ := selection.Cursor().(sceneTreeKey); hasObj || cur.id != "" {
					recordEditorSync(sceneTreeKey{}, nil)
					selection.Clear()
					selection.SetCursor(nil)
				}
				return
			}
			curIDs := make([]string, 0, selection.Len())
			for _, raw := range selection.Keys() {
				if tk, ok := raw.(sceneTreeKey); ok && tk.id != "" {
					curIDs = append(curIDs, tk.id)
				}
			}
			pk := keyOf(ed.SelectedID)
			cur, _ := selection.Cursor().(sceneTreeKey)
			if sameIDSet(curIDs, ids) && cur == pk {
				return
			}
			wantKeys := make([]any, 0, len(ids))
			synced := make([]string, 0, len(ids))
			for _, id := range ids {
				k := keyOf(id)
				if k.id == "" {
					continue
				}
				wantKeys = append(wantKeys, k)
				synced = append(synced, id)
				path := ed.resolvedObjectPath(k.id)
				setOpen(sceneTreeKey{layer: k.layer}, true)
				for _, a := range path[:max(len(path)-1, 0)] {
					setOpen(sceneTreeKey{layer: k.layer, id: a.ID}, true)
				}
			}
			if len(wantKeys) == 0 {
				return
			}
			recordEditorSync(pk, synced)
			selection.Set(wantKeys...)
			if pk.id != "" {
				selection.SetCursor(pk)
				if row := shownRow(roots(), children, expanded, pk); row >= 0 {
					listRef.ScrollToIndexAt(row, 0.5, 0, false)
				}
			}
		})
	})

	startEdit := func(k sceneTreeKey, f treeField) {
		if k.id == "" || k.removed || ed.Playing() || ed.AgentTouchingObject(k.id) {
			return
		}
		editing.Set(treeEdit{key: k, field: f})
	}
	var lastRenameReq uint64
	cx.Effect(func() {
		_ = rev.Get()
		cx.Untrack(func() {
			seq, reqID := ed.contextMenu.renameReqSeq, ed.contextMenu.renameReqID
			if seq != 0 && seq != lastRenameReq && reqID != "" {
				lastRenameReq = seq
				k := keyOf(reqID)
				if k.id != "" {
					startEdit(k, editName)
				}
			}
		})
	})
	cx.OnKey("f2", func() {
		if ed.SelectedID != "" {
			startEdit(keyOf(ed.SelectedID), editName)
		}
	})

	row := func(cx *core.Scope, item ui.TreeItem) core.Node {
		return core.Dynamic(func(cx *core.Scope) core.Node {
			k, _ := item.Key().(sceneTreeKey)
			if k.id == "" {
				return layerRow(cx, ed, rev, k, hint)
			}
			if k.removed {
				return removedObjectRow(cx, ed, rev, k)
			}
			return objectRow(cx, ed, rev, k, editing, hint, startEdit, setOpen, renamed)
		})
	}

	return core.View{
		TestID: "scene-tree-panel",
		StyleFunc: func() core.Style {
			cur := ed.SpecPalette(cx)
			return core.Style{
				FlexGrow:   1,
				FlexBasis:  core.Pt(0),
				MinHeight:  core.Pt(0),
				Background: cur.Bg,
				Padding:    core.Symmetric(0, specSpace4),
				Gap:        specSpace4,
			}
		},
		Children: []core.Node{
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.RemoteConnected() || ed.SceneTreeTab() == SceneTreeRemote
				},
				Then: func(cx *core.Scope) core.Node {
					return ui.HStack{
						Gap: 4,
						Children: []core.Node{
							ui.ToggleButton{
								TestID: "scene-tree-tab-local",
								Text:   ed.T("sceneTree.title"),
								On: func() bool {
									_ = rev.Get()
									return ed.SceneTreeTab() == SceneTreeLocal
								},
								OnChange: func(on bool) {
									if on {
										ed.SetSceneTreeTab(SceneTreeLocal)
									}
								},
							},
							ui.ToggleButton{
								TestID: "scene-tree-tab-remote",
								Text:   ed.T("sceneTree.remote"),
								On: func() bool {
									_ = rev.Get()
									return ed.SceneTreeTab() == SceneTreeRemote
								},
								OnChange: func(on bool) {
									if on {
										ed.SetSceneTreeTab(SceneTreeRemote)
									}
								},
							},
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.RemoteConnected()
								},
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: "scene-tree-remote-live-dot",
										Style: core.Style{
											Width:        core.Pt(7),
											Height:       core.Pt(7),
											CornerRadius: 4,
											Background:   colors.Up(),
										},
									}
								},
							},
						},
					}
				},
			},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.PlannedNewTreeNodeCount() > 0
				},
				Then: func(cx *core.Scope) core.Node {
					darkMemo := core.NewMemo(cx, func() bool {
						_ = rev.Get()
						return ed.SpecPalette(cx).Dark
					})
					return core.Dynamic(func(cx *core.Scope) core.Node {
						spec := SpecPaletteFor(darkMemo.Get())
						return core.Text{
							TestID: "scene-tree-planned-count",
							ContentFunc: func() string {
								_ = rev.Get()
								return ed.T("plan.plannedCount", l10n.Param("count", strconv.Itoa(ed.PlannedNewTreeNodeCount())))
							},
							Style: core.Style{
								FontSize:   10.5,
								FontWeight: 500,
								Color:      spec.Agent.Fg,
							},
						}
					})
				},
			},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.SceneTreeTab() == SceneTreeRemote
				},
				Then: func(cx *core.Scope) core.Node {
					return remoteSceneTreeView(cx, ed, rev)
				},
				Else: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0), Gap: 4},
						Children: []core.Node{
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return strings.TrimSpace(ed.SceneTreeFilter()) != "" && len(roots()) == 0
								},
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: "scene-tree-filter-empty",
										Style: core.Style{
											Padding:    core.Symmetric(specSpace16, specSpace8),
											AlignItems: core.AlignCenter,
										},
										Children: []core.Node{
											core.Text{
												Content: ed.T("sceneTree.noMatches"),
												StyleFunc: func() core.Style {
													return core.Style{
														FontSize: 12,
														Color:    ed.SpecPalette(cx).TextFaint,
													}
												},
											},
										},
									}
								},
							},
							dragdrop.Area{Children: []core.Node{ui.Tree{
								TestID:   "scene-tree",
								Label:    ed.T("sceneTree.label"),
								Roots:    roots,
								Children: func(key any) []any { k, _ := key.(sceneTreeKey); return children(k) },
								Expanded: func(key any) bool { k, _ := key.(sceneTreeKey); return expanded(k) },
								OnExpand: func(key any, on bool) {
									if k, ok := key.(sceneTreeKey); ok {
										setOpen(k, on)
									}
								},
								OnActivate: func(key any) {
									if k, ok := key.(sceneTreeKey); ok {
										if obj, _, _ := ed.FindObject(k.id); obj != nil && obj.Scene != "" && !ed.Playing() && ed.Project != nil && ed.ProjectPath != "" {
											ed.report(ed.OpenInstancedScene(k.id))
											return
										}
										startEdit(k, editName)
									}
								},
								RowLabel: func(key any) string {
									k, _ := key.(sceneTreeKey)
									if k.id == "" {
										return ed.T("sceneTree.layerLabel", l10n.Param("name", k.layer))
									}
									return k.id
								},
								ToggleLabel: ed.T("sceneTree.expand"),
								Selection:   selection,
								Row:         row,
								Ref:         listRef,
								// Inset -2 px ring so the scrolling tree never clips it; selected+focused rows keep both selection fill and inset ring.
								Style: core.Style{
									FlexGrow:     1,
									FlexBasis:    core.Pt(0),
									FocusVisible: ed.SpecPalette(cx).FocusRing(-2),
								},
								RowStyle: core.Style{
									MinHeight:    core.Pt(specRowHeight),
									Height:       core.Pt(specRowHeight),
									CornerRadius: specRadiusControl,
									FocusVisible: ed.SpecPalette(cx).FocusRing(-2),
								},
							}}},
							sceneTreeSketchSectionView(cx, ed, rev),
							sceneTreePlannedNodesView(cx, ed, rev),
						},
					}
				},
			},
			planFilesFooterView(cx, ed, rev),
			playReplayFooterView(cx, ed, rev),
		},
	}
}

func sameIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !slices.Contains(b, id) {
			return false
		}
	}
	return true
}

// shownRow returns the index of k among the rows the tree shows, the children of expanded rows included, or -1.
func shownRow(roots []any, children func(sceneTreeKey) []any, expanded func(sceneTreeKey) bool, k sceneTreeKey) int {
	n := 0
	var find func(keys []any) bool
	find = func(keys []any) bool {
		for _, key := range keys {
			kk, _ := key.(sceneTreeKey)
			if kk == k {
				return true
			}
			n++
			if expanded(kk) && find(children(kk)) {
				return true
			}
		}
		return false
	}
	if find(roots) {
		return n
	}
	return -1
}

// dropStyle draws where a row being dragged would land on the row of k: a line above or below it for a sibling, a tint over it for a child. The edges are always there, in the panel's color while no drop is near, so the row does not move as one comes.
func dropStyle(cx *core.Scope, hint *core.Signal[treeHint], k sceneTreeKey) core.Style {
	colors := ui.ColorsOf(cx)
	h := hint.Get()
	s := core.Style{Borders: core.Edges{Top: 2, Bottom: 2}, BorderColor: colors.Surface()}
	if !h.on || h.key != k {
		return s
	}
	switch h.place {
	case DropBefore:
		s.BorderColors = core.EdgeColors{Top: colors.Accent()}
	case DropAfter:
		s.BorderColors = core.EdgeColors{Bottom: colors.Accent()}
	default:
		s.Background = ui.Alpha(colors.Accent(), 0.2)
	}
	return s
}

func layerRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], k sceneTreeKey, hint *core.Signal[treeHint]) core.Node {
	setHint := func(on bool) { hint.Set(treeHint{key: k, place: DropInto, on: on}) }
	return dragdrop.Target{
		View: core.View{
			TestID:    "scene-tree-layer-" + k.layer,
			Style:     core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, MinWidth: core.Pt(0), Gap: specSpace8},
			StyleFunc: func() core.Style { return dropStyle(cx, hint, k) },
			Children: []core.Node{
				core.View{
					TestID: "scene-tree-layer-dot-" + k.layer,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						return core.Style{
							Width:        core.Pt(7),
							Height:       core.Pt(7),
							CornerRadius: 2,
							Background:   ed.LayerKindColor(ed.SpecPalette(cx), k.layer),
							FlexShrink:   0,
						}
					},
				},
				core.Text{
					Content:       k.layer,
					MaxLines:      1,
					EllipsizeMode: core.EllipsizeTail,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						cur := ed.SpecPalette(cx)
						col := cur.TextMuted
						weight := core.FontWeightMedium
						if ed.ActiveLayer == k.layer {
							col = cur.TextPrimary
							weight = core.FontWeightSemibold
						}
						return core.Style{FontSize: 12.5, FontWeight: weight, Color: col, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)}
					},
				},
				core.Text{
					TestID: "scene-tree-layer-kind-" + k.layer,
					ContentFunc: func() string {
						_ = rev.Get()
						return localizedLayerKind(ed, ed.LayerKindLabel(k.layer))
					},
					StyleFunc: func() core.Style {
						return core.Style{FontSize: 11, Color: ed.SpecPalette(cx).TextFaint, FlexShrink: 0}
					},
				},
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.PlanStepBadgeForNode(k.layer) > 0
					},
					Then: func(cx *core.Scope) core.Node {
						darkMemo := core.NewMemo(cx, func() bool {
							_ = rev.Get()
							return ed.SpecPalette(cx).Dark
						})
						return core.Dynamic(func(cx *core.Scope) core.Node {
							spec := SpecPaletteFor(darkMemo.Get())
							return core.View{
								TestID: "scene-tree-plan-badge-" + k.layer,
								Style: core.Style{
									Width:          core.Pt(16),
									Height:         core.Pt(16),
									CornerRadius:   8,
									Background:     spec.Agent.Fill,
									BorderWidth:    1,
									BorderColor:    spec.Agent.Edge,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FlexShrink:     0,
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											return strconv.Itoa(ed.PlanStepBadgeForNode(k.layer))
										},
										Style: core.Style{
											FontSize:   10,
											FontWeight: 600,
											Color:      spec.Agent.Fg,
										},
									},
								},
							}
						})
					},
				},
			},
		},
		Accept:  func(data any) bool { _, ok := data.(sceneTreeDrag); return ok },
		OnEnter: func(dragdrop.Event) { setHint(true) },
		OnLeave: func(dragdrop.Event) { setHint(false) },
		OnDrop: func(ev dragdrop.Event) {
			setHint(false)
			if d, ok := ev.Data.(sceneTreeDrag); ok {
				ed.report(ed.MoveObjectToLayer(d.id, k.layer, -1))
			}
		},
	}
}

func objectRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], k sceneTreeKey, editing *core.Signal[treeEdit], hint *core.Signal[treeHint], startEdit func(sceneTreeKey, treeField), setOpen func(sceneTreeKey, bool), renamed func(oldID, newID string)) core.Node {
	colors := ui.ColorsOf(cx)
	id := k.id
	rowRef := new(core.Ref)
	placeAt := func(ev dragdrop.Event) DropPlace {
		b, ok := rowRef.Bounds()
		if !ok || b.Size.Height <= 0 {
			return DropInto
		}
		switch y := ev.Position.Y; {
		case y < b.Size.Height/4:
			return DropBefore
		case y > b.Size.Height*3/4:
			return DropAfter
		}
		return DropInto
	}
	accept := func(data any) bool {
		d, ok := data.(sceneTreeDrag)
		return ok && d.id != id && ed.ObjectOrigin(id) == ObjectLocal
	}
	track := func(ev dragdrop.Event) {
		hint.Set(treeHint{key: k, place: placeAt(ev), on: true})
	}
	isForeign := func() bool {
		_ = rev.Get()
		return ed.ObjectOrigin(id) != ObjectLocal
	}

	label := core.Show{
		When: func() bool { e := editing.Get(); return e.key == k && e.field != editNone },
		Then: func(cx *core.Scope) core.Node {
			edit := editing.GetUntracked()
			text := ""
			if obj, _ := ed.FindResolvedObject(id); obj != nil {
				text = obj.Name
				if edit.field == editID {
					text = obj.ID
				}
			}
			commit := func() {
				if editing.GetUntracked() != edit {
					return
				}
				editing.Set(treeEdit{})
				if edit.field == editID {
					if err := ed.SetObjectID(id, text); err != nil {
						ed.report(err)
						return
					}
					renamed(id, text)
					return
				}
				ed.report(ed.SetObjectName(id, text))
			}
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if editing.GetUntracked() == edit {
					editing.Set(treeEdit{})
				}
			})
			testID, labelText := "scene-tree-name-"+id, ed.T("sceneTree.objectName")
			if edit.field == editID {
				testID, labelText = "scene-tree-id-"+id, ed.T("sceneTree.objectID")
			}
			return specInputField(cx, ed, ui.Input{
				TestID:       testID,
				Label:        labelText,
				DefaultValue: text,
				AutoFocus:    true,
				OnChange:     func(s string) { text = s },
				OnSubmit:     func(s string) { text = s; commit() },
				OnBlur:       commit,
				Style:        core.Style{FlexGrow: 1, MinWidth: core.Pt(0), Height: core.Pt(22)},
			})
		},
		Else: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace4, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.View{
						TestID: "scene-tree-dot-" + id,
						StyleFunc: func() core.Style {
							_ = rev.Get()
							return core.Style{
								Width:        core.Pt(7),
								Height:       core.Pt(7),
								CornerRadius: 2,
								Background:   ed.ObjectKindColor(ed.SpecPalette(cx), id),
								FlexShrink:   0,
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ObjectHasAnyOverride(id) || ed.InstanceOverrideCount(id) > 0
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "scene-tree-override-dot-" + id,
								Style: core.Style{
									Width:        core.Pt(6),
									Height:       core.Pt(6),
									CornerRadius: 3,
									Background:   colors.Warn(),
									FlexShrink:   0,
								},
							}
						},
					},
					core.Text{
						TestID: "scene-tree-label-" + id,
						ContentFunc: func() string {
							_ = rev.Get()
							if obj, _ := ed.FindResolvedObject(id); obj != nil && obj.Name != "" {
								return obj.Name
							}
							return id
						},
						MaxLines:      1,
						EllipsizeMode: core.EllipsizeTail,
						Style:         core.Style{FontSize: 12.5, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							cur := ed.SpecPalette(cx)
							if pathFlagged(ed.hidden, ed.resolvedObjectPath(id)) || ed.ObjectOrigin(id) != ObjectLocal {
								return core.Style{Color: cur.TextFaint}
							}
							if ed.IsSelected(id) {
								return core.Style{Color: cur.TextPrimary, FontWeight: core.FontWeightMedium}
							}
							return core.Style{Color: cur.TextMuted}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							if ch, ok := ed.AgentObjectChange(id); ok && (ch.Kind == AgentChangeAdded || ch.Kind == AgentChangeChanged) {
								return false
							}
							return ed.IsObjectProposedNew(id)
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:  "scene-tree-new-" + id,
								Content: ed.T("sceneTreeExtra.badgeNew"),
								StyleFunc: func() core.Style {
									return core.Style{
										FontSize:   10,
										FontWeight: core.FontWeightSemibold,
										Color:      ed.SpecPalette(cx).Added.Fg,
										FlexShrink: 0,
									}
								},
							}
						},
					},
					core.Show{
						When: isForeign,
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "scene-tree-inherited-" + id,
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.ObjectOrigin(id) == ObjectInstanced {
										return ed.T("sceneTree.instancedTag")
									}
									return ed.T("sceneTree.inheritedTag")
								},
								Style: core.Style{FontSize: 9, Color: colors.Muted(), FlexShrink: 0},
							}
						},
					},
					core.Text{
						TestID: "scene-tree-kind-" + id,
						ContentFunc: func() string {
							_ = rev.Get()
							return localizedObjectKind(ed, ed.SceneRailObjectKind(id))
						},
						StyleFunc: func() core.Style {
							return core.Style{FontSize: 11, Color: ed.SpecPalette(cx).TextFaint, FlexShrink: 0}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							_, ok := ed.ObjectTaskBadge(id)
							return ok
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "scene-tree-task-" + id,
								ContentFunc: func() string {
									_ = rev.Get()
									ov, ok := ed.ObjectTaskBadge(id)
									if !ok {
										return ""
									}
									if ov.Holding {
										return ed.T("sceneTreeExtra.taskHolding", l10n.Param("num", ov.TaskNumber))
									}
									return ed.T("sceneTreeExtra.taskEditing", l10n.Param("num", ov.TaskNumber))
								},
								Style: core.Style{
									FontSize:   10.5,
									FontWeight: 500,
									FlexShrink: 0,
								},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									spec := ed.SpecPalette(cx)
									ov, ok := ed.ObjectTaskBadge(id)
									if ok && !ov.Holding {
										return core.Style{Color: spec.Agent.Fg}
									}
									return core.Style{Color: spec.TextFaint}
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.AgentTouchingObject(id)
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "scene-tree-agent-working-" + id,
								Style: core.Style{
									Width:        core.Pt(7),
									Height:       core.Pt(7),
									CornerRadius: 4,
									FlexShrink:   0,
								},
								StyleFunc: func() core.Style {
									return core.Style{Background: ed.SpecPalette(cx).Agent.Fg}
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							ch, ok := ed.AgentObjectChange(id)
							return ok && (ch.Kind == AgentChangeAdded || ch.Kind == AgentChangeChanged)
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								Style: core.Style{
									Padding:      core.Symmetric(1, 5),
									CornerRadius: 4,
									FlexShrink:   0,
								},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									p := ed.SpecPalette(cx)
									if ch, ok := ed.AgentObjectChange(id); ok && ch.Kind == AgentChangeChanged {
										return core.Style{Background: p.Changed.Fill}
									}
									return core.Style{Background: p.Added.Fill}
								},
								Children: []core.Node{
									core.Text{
										TestID: "scene-tree-agent-badge-" + id,
										ContentFunc: func() string {
											_ = rev.Get()
											if ch, ok := ed.AgentObjectChange(id); ok && ch.Kind == AgentChangeChanged {
												return ed.T("agent.treeChanged")
											}
											return ed.T("agent.treeNew")
										},
										Style: core.Style{
											FontSize:   10,
											FontWeight: 600,
										},
										StyleFunc: func() core.Style {
											_ = rev.Get()
											p := ed.SpecPalette(cx)
											if ch, ok := ed.AgentObjectChange(id); ok && ch.Kind == AgentChangeChanged {
												return core.Style{Color: p.Changed.Fg}
											}
											return core.Style{Color: p.Added.Fg}
										},
									},
								},
							}
						},
					},
				},
			}
		},
	}

	toggle := func(testID string, on func() bool, set func(bool) error, onIcon, offIcon func() *core.SVG, onLabel, offLabel string) core.Node {
		return ui.IconButton{
			TestID:   testID,
			Size:     18,
			IconSize: 12,
			Icon: func() *core.SVG {
				_ = rev.Get()
				if on() {
					return onIcon()
				}
				return offIcon()
			},
			LabelFunc: func() string {
				_ = rev.Get()
				if on() {
					return onLabel
				}
				return offLabel
			},
			Selected: func() bool { _ = rev.Get(); return on() },
			OnPress:  func() { ed.report(set(!on())) },
		}
	}
	hidden := func() bool { return ed.ObjectHidden(id) }
	locked := func() bool { return ed.ObjectLocked(id) }

	content := core.View{
		TestID: "scene-tree-row-" + id,
		Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Gap: 2, CornerRadius: 6},
		StyleFunc: func() core.Style {
			_ = rev.Get()
			st := dropStyle(cx, hint, k)
			if st.Background == 0 {
				spec := ed.SpecPalette(cx)
				if ed.ObjectProblemCount(id) > 0 {
					st.Background = spec.Problem().Fill
				} else if ov, ok := ed.ObjectTaskBadge(id); ok && !ov.Holding {
					st.Background = ui.Alpha(spec.Agent.Fg, 0.12)
				}
			}
			return st
		},
		Children: []core.Node{
			label,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.ObjectProblemCount(id) > 0
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "scene-tree-problem-" + id,
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          3,
							Padding:      core.Symmetric(1, 5),
							CornerRadius: 6,
							BorderWidth:  1,
							FlexShrink:   0,
						},
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{
								Background:  p.Problem().Fill,
								BorderColor: p.Problem().Edge,
							}
						},
						Children: []core.Node{
							problemAlertIconFunc(func() core.Color { return ed.SpecPalette(cx).Problem().Fg }, 11),
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									return strconv.Itoa(ed.ObjectProblemCount(id))
								},
								Style: core.Style{
									FontSize:   10.5,
									FontWeight: 600,
								},
								StyleFunc: func() core.Style {
									return core.Style{Color: ed.SpecPalette(cx).Problem().Fg}
								},
							},
						},
					}
				},
			},
			toggle("scene-tree-eye-"+id, hidden, func(on bool) error { return ed.SetObjectHidden(id, on) }, lucide.EyeOff, lucide.Eye, ed.T("sceneTree.showNamed", l10n.Param("id", id)), ed.T("sceneTree.hideNamed", l10n.Param("id", id))),
			toggle("scene-tree-lock-"+id, locked, func(on bool) error { return ed.SetObjectLocked(id, on) }, lucide.Lock, lucide.LockOpen, ed.T("sceneTree.unlockNamed", l10n.Param("id", id)), ed.T("sceneTree.lockNamed", l10n.Param("id", id))),
		},
	}

	editsOff := func() bool { _ = rev.Get(); return ed.Playing() || ed.AgentTouchingObject(id) }
	structOff := func() bool { return editsOff() || isForeign() }
	menuOpen := core.NewSignal(cx, false)
	cx.Effect(func() {
		if !menuOpen.Get() {
			return
		}
		_ = rev.Get()
		if ed.ObjectContextMenuOpen() || ed.contextMenuBlocked() {
			menuOpen.Set(false)
		}
	})
	menuKey := core.NewMemo(cx, func() string {
		if !menuOpen.Get() {
			return ""
		}
		_ = rev.Get()
		return ed.objectContextMenuDigest(id, cx.Dark())
	})

	return ui.ContextMenu{
		TestID: "scene-tree-menu-" + id,
		Style:  core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
		Open:   menuOpen.Get,
		OnOpenChange: func(open bool, _ base.Reason) {
			if open {
				if ed.contextMenuBlocked() {
					return
				}
				ed.CloseObjectContextMenu()
			}
			menuOpen.Set(open)
		},
		Trigger: dragdrop.Target{
			View: core.View{
				Ref:   rowRef,
				Style: core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
				Children: []core.Node{dragdrop.Source{
					View: core.View{
						Style:    core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
						Children: []core.Node{content},
					},
					Data: func() any {
						if structOff() || editing.GetUntracked().key == k {
							return nil
						}
						return sceneTreeDrag{id: id}
					},
					Preview: func(any) core.Node {
						return core.Text{Content: id, Style: core.Style{FontSize: 12, Color: colors.Text(), Padding: core.Symmetric(2, 6), Background: colors.Raised(), CornerRadius: 4}}
					},
				}},
			},
			Accept:  accept,
			OnEnter: track,
			OnMove:  track,
			OnLeave: func(dragdrop.Event) { hint.Set(treeHint{}) },
			OnDrop: func(ev dragdrop.Event) {
				place := placeAt(ev)
				hint.Set(treeHint{})
				d, ok := ev.Data.(sceneTreeDrag)
				if !ok {
					return
				}
				if err := ed.DropObject(d.id, id, place); err != nil {
					ed.report(err)
					return
				}
				if place == DropInto {
					setOpen(k, true)
				}
			},
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if menuKey.Get() == "" {
					return core.Group{}
				}
				return core.Group(objectContextMenuItems(cx, ed, rev, id, objectContextMenuOpts{
					includeTreeActions: true,
					treeKey:            k,
					onRename:           func() { startEdit(k, editName) },
					onChangeID:         func() { startEdit(k, editID) },
					onReparented: func(layer, parentID string) {
						setOpen(sceneTreeKey{layer: layer, id: parentID}, true)
					},
				}))
			}),
		},
	}
}

func removedObjectRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], k sceneTreeKey) core.Node {
	id := k.id
	return core.View{
		TestID: "scene-tree-row-removed-" + id,
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			FlexGrow:   1,
			MinWidth:   core.Pt(0),
			Gap:        6,
			Padding:    core.Symmetric(2, 0),
		},
		Children: []core.Node{
			core.Text{
				TestID: "scene-tree-label-" + id,
				ContentFunc: func() string {
					_ = rev.Get()
					if ch, ok := ed.AgentObjectChange(id); ok && ch.Name != "" {
						return ch.Name
					}
					return id
				},
				Style: core.Style{
					FontSize:       12,
					TextDecoration: core.TextDecorationLineThrough,
					FlexGrow:       1,
					FlexShrink:     1,
					MinWidth:       core.Pt(0),
				},
				StyleFunc: func() core.Style {
					return core.Style{Color: ed.SpecPalette(cx).Removed.Fg}
				},
			},
			core.View{
				Style: core.Style{
					Padding:      core.Symmetric(1, 5),
					CornerRadius: 4,
					FlexShrink:   0,
				},
				StyleFunc: func() core.Style {
					return core.Style{Background: ed.SpecPalette(cx).Removed.Fill}
				},
				Children: []core.Node{
					core.Text{
						TestID: "scene-tree-agent-badge-" + id,
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.T("agent.treeRemoved")
						},
						Style: core.Style{
							FontSize:   10,
							FontWeight: 600,
						},
						StyleFunc: func() core.Style {
							return core.Style{Color: ed.SpecPalette(cx).Removed.Fg}
						},
					},
				},
			},
		},
	}
}
