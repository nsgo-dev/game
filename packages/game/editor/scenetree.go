package editor

import (
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
	selection := ui.NewSelection(cx, false)
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
		if ed.Doc == nil {
			return nil
		}
		if k.id == "" {
			li := ed.layerIndex(k.layer)
			if li < 0 {
				return nil
			}
			objs := ed.Doc.Layers[li].Objects
			out := make([]any, len(objs))
			for i := range objs {
				out[i] = sceneTreeKey{layer: k.layer, id: objs[i].ID}
			}
			return out
		}
		loc := ed.lookupObject(k.id).loc
		if loc.obj == nil {
			return nil
		}
		out := make([]any, len(loc.obj.Children))
		for i := range loc.obj.Children {
			out[i] = sceneTreeKey{layer: ed.Doc.Layers[loc.layer].Name, id: loc.obj.Children[i].ID}
		}
		return out
	}
	roots := func() []any {
		_ = rev.Get()
		if ed.Doc == nil {
			return nil
		}
		out := make([]any, len(ed.Doc.Layers))
		for i := range ed.Doc.Layers {
			out[i] = sceneTreeKey{layer: ed.Doc.Layers[i].Name}
		}
		return out
	}
	keyOf := func(id string) sceneTreeKey {
		if _, li, _ := ed.FindObject(id); li >= 0 {
			return sceneTreeKey{layer: ed.Doc.Layers[li].Name, id: id}
		}
		return sceneTreeKey{}
	}

	// A press on a row selects its object in the editor, or makes its layer the active one.
	// While connection target pick mode is active, pressing an object row picks it as the target without changing SelectedID.
	cx.Effect(func() {
		cur, _ := selection.Cursor().(sceneTreeKey)
		cx.Untrack(func() {
			if cur.id != "" && ed.PickingConnectionTarget() {
				ed.finishConnectionPick(cur.id)
				if ed.SelectedID != "" {
					if k := keyOf(ed.SelectedID); k.id != "" {
						selection.Set(k)
					}
				} else {
					selection.Clear()
				}
				return
			}
			switch {
			case cur.id != "":
				if ed.SelectedID != cur.id {
					ed.SelectObject(cur.id)
				}
			case cur.layer != "":
				// A layer's row is selected in place of an object's.
				ed.SelectObject("")
				if ed.ActiveLayer != cur.layer {
					ed.SetActiveLayer(cur.layer)
				}
			}
		})
	})
	// Selecting an object elsewhere, such as in a viewport, selects its row and reveals it.
	cx.Effect(func() {
		_ = rev.Get()
		cx.Untrack(func() {
			cur, _ := selection.Cursor().(sceneTreeKey)
			if ed.SelectedID == "" {
				if cur.id != "" {
					selection.Clear()
				}
				return
			}
			k := keyOf(ed.SelectedID)
			if k.id == "" || cur == k {
				return
			}
			path := objectPath(ed.Doc, k.id)
			setOpen(sceneTreeKey{layer: k.layer}, true)
			for _, a := range path[:len(path)-1] {
				setOpen(sceneTreeKey{layer: k.layer, id: a.ID}, true)
			}
			selection.Set(k)
			if row := shownRow(roots(), children, expanded, k); row >= 0 {
				listRef.ScrollToIndexAt(row, 0.5, 0, false)
			}
		})
	})

	startEdit := func(k sceneTreeKey, f treeField) {
		if k.id == "" || ed.Playing() {
			return
		}
		editing.Set(treeEdit{key: k, field: f})
	}
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
			core.Text{
				Content: "Scene",
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
			},
			dragdrop.Area{Children: []core.Node{ui.Tree{
				TestID:   "scene-tree",
				Label:    "Scene Tree",
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
						startEdit(k, editName)
					}
				},
				RowLabel: func(key any) string {
					k, _ := key.(sceneTreeKey)
					if k.id == "" {
						return "Layer " + k.layer
					}
					return k.id
				},
				ToggleLabel: "Expand",
				Selection:   selection,
				Row:         row,
				Ref:         listRef,
				Style:       core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
			}}},
		},
	}
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
							return "active"
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
		return ok && d.id != id
	}
	track := func(ev dragdrop.Event) {
		hint.Set(treeHint{key: k, place: placeAt(ev), on: true})
	}

	label := core.Show{
		When: func() bool { e := editing.Get(); return e.key == k && e.field != editNone },
		Then: func(cx *core.Scope) core.Node {
			edit := editing.GetUntracked()
			text := ""
			if obj, _, _ := ed.FindObject(id); obj != nil {
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
			testID, labelText := "scene-tree-name-"+id, "Object Name"
			if edit.field == editID {
				testID, labelText = "scene-tree-id-"+id, "Object ID"
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
					core.Text{
						TestID: "scene-tree-label-" + id,
						ContentFunc: func() string {
							_ = rev.Get()
							if obj := ed.lookupObject(id).loc.obj; obj != nil && obj.Name != "" {
								return obj.Name
							}
							return id
						},
						Style: core.Style{FontSize: 12, Color: colors.Text(), FlexShrink: 1},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							if pathFlagged(ed.hidden, ed.lookupObject(id).path) {
								return core.Style{Color: colors.Muted()}
							}
							return core.Style{}
						},
					},
					core.Text{
						ContentFunc: func() string {
							_ = rev.Get()
							if obj := ed.lookupObject(id).loc.obj; obj != nil && obj.Name != "" && obj.Name != id {
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
			toggle("scene-tree-eye-"+id, hidden, func(on bool) error { return ed.SetObjectHidden(id, on) }, lucide.EyeOff, lucide.Eye, "Show "+id, "Hide "+id),
			toggle("scene-tree-lock-"+id, locked, func(on bool) error { return ed.SetObjectLocked(id, on) }, lucide.Lock, lucide.LockOpen, "Unlock "+id, "Lock "+id),
		},
	}

	editsOff := func() bool { _ = rev.Get(); return ed.Playing() }
	siblings := func() (index, count int, prev, parent string) {
		loc := ed.lookupObject(id).loc
		if loc.obj == nil {
			return -1, 0, "", ""
		}
		objs := ed.Doc.Layers[loc.layer].Objects
		if loc.parent != nil {
			objs = loc.parent.Children
			parent = loc.parent.ID
		}
		if loc.index > 0 {
			prev = objs[loc.index-1].ID
		}
		return loc.index, len(objs), prev, parent
	}
	items := []core.Node{
		ui.MenuItem{TestID: "scene-tree-rename-" + id, Text: "Rename", Shortcut: "F2", Disabled: editsOff, OnSelect: func() { startEdit(k, editName) }},
		ui.MenuItem{TestID: "scene-tree-change-id-" + id, Text: "Change ID", Disabled: editsOff, OnSelect: func() { startEdit(k, editID) }},
		ui.MenuSeparator{},
		ui.MenuItem{TestID: "scene-tree-up-" + id, Text: "Move Up",
			Disabled: func() bool { i, _, _, _ := siblings(); return editsOff() || i <= 0 },
			OnSelect: func() {
				if i, _, _, _ := siblings(); i > 0 {
					ed.report(ed.ReorderObject(id, i-1))
				}
			}},
		ui.MenuItem{TestID: "scene-tree-down-" + id, Text: "Move Down",
			Disabled: func() bool { i, n, _, _ := siblings(); return editsOff() || i < 0 || i >= n-1 },
			OnSelect: func() {
				if i, n, _, _ := siblings(); i >= 0 && i < n-1 {
					ed.report(ed.ReorderObject(id, i+1))
				}
			}},
		ui.MenuItem{TestID: "scene-tree-indent-" + id, Text: "Make Child of Previous",
			Disabled: func() bool { _, _, prev, _ := siblings(); return editsOff() || prev == "" },
			OnSelect: func() {
				if _, _, prev, _ := siblings(); prev != "" {
					if err := ed.ReparentObject(id, prev, -1); err != nil {
						ed.report(err)
						return
					}
					setOpen(sceneTreeKey{layer: k.layer, id: prev}, true)
				}
			}},
		ui.MenuItem{TestID: "scene-tree-outdent-" + id, Text: "Move Out of Parent",
			Disabled: func() bool { _, _, _, parent := siblings(); return editsOff() || parent == "" },
			OnSelect: func() {
				_, _, _, parent := siblings()
				if parent == "" {
					return
				}
				ploc := locateObject(ed.Doc, parent)
				grand := ""
				if ploc.parent != nil {
					grand = ploc.parent.ID
				}
				ed.report(ed.ReparentObject(id, grand, ploc.index+1))
			}},
	}
	if ed.Doc != nil && len(ed.Doc.Layers) > 1 {
		items = append(items, ui.MenuSeparator{})
		for i := range ed.Doc.Layers {
			name := ed.Doc.Layers[i].Name
			items = append(items, ui.MenuItem{TestID: "scene-tree-to-layer-" + id + "-" + name, Text: "Move to Layer " + name,
				Disabled: func() bool {
					loc := ed.lookupObject(id).loc
					return editsOff() || loc.obj == nil || ed.Doc.Layers[loc.layer].Name == name && loc.parent == nil
				},
				OnSelect: func() { ed.report(ed.MoveObjectToLayer(id, name, -1)) }})
		}
	}
	items = append(items,
		ui.MenuSeparator{},
		ui.MenuItem{TestID: "scene-tree-hide-" + id, TextFunc: func() string {
			_ = rev.Get()
			if hidden() {
				return "Show in Viewport"
			}
			return "Hide in Viewport"
		}, OnSelect: func() { ed.report(ed.SetObjectHidden(id, !hidden())) }},
		ui.MenuItem{TestID: "scene-tree-lock-item-" + id, TextFunc: func() string {
			_ = rev.Get()
			if locked() {
				return "Unlock"
			}
			return "Lock"
		}, OnSelect: func() { ed.report(ed.SetObjectLocked(id, !locked())) }},
		ui.MenuSeparator{},
		ui.MenuItem{TestID: "scene-tree-delete-" + id, Text: "Delete", Variant: ui.MenuItemDanger, Disabled: editsOff,
			OnSelect: func() { ed.report(ed.DeleteObject(id)) }},
	)

	return ui.ContextMenu{
		TestID: "scene-tree-menu-" + id,
		Style:  core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
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
						if editsOff() || editing.GetUntracked().key == k {
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
		Children: items,
	}
}
