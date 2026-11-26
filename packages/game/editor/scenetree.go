package editor

import (
	"slices"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// sceneTreeKey is a row of the scene tree: a layer when id is empty, else the object id.
type sceneTreeKey struct {
	layer string
	id    string
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
		doc := ed.resolvedDoc()
		if doc == nil {
			return nil
		}
		if k.id == "" {
			for i := range doc.Layers {
				if doc.Layers[i].Name == k.layer {
					objs := doc.Layers[i].Objects
					out := make([]any, len(objs))
					for j := range objs {
						out[j] = sceneTreeKey{layer: k.layer, id: objs[j].ID}
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
		out := make([]any, len(loc.obj.Children))
		for i := range loc.obj.Children {
			out[i] = sceneTreeKey{layer: doc.Layers[loc.layer].Name, id: loc.obj.Children[i].ID}
		}
		return out
	}
	roots := func() []any {
		_ = rev.Get()
		doc := ed.resolvedDoc()
		if doc == nil {
			return nil
		}
		out := make([]any, len(doc.Layers))
		for i := range doc.Layers {
			out[i] = sceneTreeKey{layer: doc.Layers[i].Name}
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
	syncTreeToEditor := func() {
		ids := ed.SelectedIDs()
		if len(ids) == 0 {
			selection.Clear()
			selection.SetCursor(nil)
			return
		}
		keys := make([]any, 0, len(ids))
		for _, id := range ids {
			if k := keyOf(id); k.id != "" {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			selection.Clear()
			selection.SetCursor(nil)
			return
		}
		selection.Set(keys...)
		if pk := keyOf(ed.SelectedID); pk.id != "" {
			selection.SetCursor(pk)
		}
	}
	cx.Effect(func() {
		cur, _ := selection.Cursor().(sceneTreeKey)
		keys := selection.Keys()
		chosen := selection.Has(cur)
		cx.Untrack(func() {
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
					selection.Set(cur)
				}
				ed.ClearSelection()
				ed.SetActiveLayer(cur.layer)
				return
			}
			objIDs := make([]string, 0, len(keys))
			for _, raw := range keys {
				if tk, ok := raw.(sceneTreeKey); ok && tk.id != "" && !slices.Contains(objIDs, tk.id) {
					objIDs = append(objIDs, tk.id)
				}
			}
			if len(objIDs) > 0 {
				primary := objIDs[len(objIDs)-1]
				if cur.id != "" && chosen && slices.Contains(objIDs, cur.id) {
					primary = cur.id
				}
				if ed.SelectedID != primary || !sameIDSet(ed.SelectedIDs(), objIDs) {
					ed.SelectObjectsWithPrimary(objIDs, primary)
				}
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
			for _, id := range ids {
				k := keyOf(id)
				if k.id == "" {
					continue
				}
				wantKeys = append(wantKeys, k)
				path := ed.resolvedObjectPath(k.id)
				setOpen(sceneTreeKey{layer: k.layer}, true)
				for _, a := range path[:max(len(path)-1, 0)] {
					setOpen(sceneTreeKey{layer: k.layer, id: a.ID}, true)
				}
			}
			if len(wantKeys) == 0 {
				return
			}
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
		if k.id == "" || ed.Playing() {
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
			return objectRow(cx, ed, rev, k, editing, hint, startEdit, setOpen, renamed)
		})
	}

	return core.View{
		TestID: "scene-tree-panel",
		Style: core.Style{
			Width:       core.Pt(220),
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			Background:  colors.Inset(),
			Borders:     core.Edges{Right: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(8),
			Gap:         6,
		},
		Children: []core.Node{
			ui.HStack{
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
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 4},
						Children: []core.Node{
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
								Style:       core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
							}}},
							core.Text{
								TestID:  "scene-tree-hint",
								Content: ed.T("sceneTree.hint"),
								Style:   core.Style{FontSize: 10, Color: colors.Muted()},
							},
						},
					}
				},
			},
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
	s := core.Style{Borders: core.Edges{Top: 2, Bottom: 2}, BorderColor: colors.Inset()}
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
	colors := ui.ColorsOf(cx)
	setHint := func(on bool) { hint.Set(treeHint{key: k, place: DropInto, on: on}) }
	return dragdrop.Target{
		View: core.View{
			TestID:    "scene-tree-layer-" + k.layer,
			Style:     core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, Gap: 6},
			StyleFunc: func() core.Style { return dropStyle(cx, hint, k) },
			Children: []core.Node{
				core.Text{
					Content: k.layer,
					Style:   core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
				},
				core.Text{
					ContentFunc: func() string {
						_ = rev.Get()
						if ed.ActiveLayer == k.layer {
							return ed.T("sceneTree.active")
						}
						return ""
					},
					Style: core.Style{FontSize: 10, Color: colors.Muted()},
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
			return ui.Input{
				TestID:       testID,
				Label:        labelText,
				DefaultValue: text,
				AutoFocus:    true,
				OnChange:     func(s string) { text = s },
				OnSubmit:     func(s string) { text = s; commit() },
				OnBlur:       commit,
				Style:        core.Style{FlexGrow: 1, MinWidth: core.Pt(0), Height: core.Pt(22)},
			}
		},
		Else: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, FlexGrow: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
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
						Style: core.Style{FontSize: 12, Color: colors.Text(), FlexShrink: 1},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							if pathFlagged(ed.hidden, ed.resolvedObjectPath(id)) || ed.ObjectOrigin(id) != ObjectLocal {
								return core.Style{Color: colors.Muted()}
							}
							return core.Style{}
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
						ContentFunc: func() string {
							_ = rev.Get()
							if obj, _ := ed.FindResolvedObject(id); obj != nil && obj.Name != "" && obj.Name != id {
								return id
							}
							return ""
						},
						Style: core.Style{FontSize: 10, Color: colors.Muted(), FlexShrink: 1},
					},
				},
			}
		},
	}

	toggle := func(testID string, on func() bool, set func(bool) error, onIcon, offIcon func() *core.SVG, onLabel, offLabel string) core.Node {
		return ui.IconButton{
			TestID:   testID,
			Size:     20,
			IconSize: 13,
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
		TestID:    "scene-tree-row-" + id,
		Style:     core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, MinWidth: core.Pt(0), Gap: 2},
		StyleFunc: func() core.Style { return dropStyle(cx, hint, k) },
		Children: []core.Node{
			label,
			toggle("scene-tree-eye-"+id, hidden, func(on bool) error { return ed.SetObjectHidden(id, on) }, lucide.EyeOff, lucide.Eye, ed.T("sceneTree.showNamed", l10n.Param("id", id)), ed.T("sceneTree.hideNamed", l10n.Param("id", id))),
			toggle("scene-tree-lock-"+id, locked, func(on bool) error { return ed.SetObjectLocked(id, on) }, lucide.Lock, lucide.LockOpen, ed.T("sceneTree.unlockNamed", l10n.Param("id", id)), ed.T("sceneTree.lockNamed", l10n.Param("id", id))),
		},
	}

	editsOff := func() bool { _ = rev.Get(); return ed.Playing() }
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
		Style:  core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
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
				Style: core.Style{Direction: core.Row, FlexGrow: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{dragdrop.Source{
					View: core.View{
						Style:    core.Style{Direction: core.Row, FlexGrow: 1, MinWidth: core.Pt(0)},
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
