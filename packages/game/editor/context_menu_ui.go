package editor

import (
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type objectContextMenuOpts struct {
	includeTreeActions bool
	treeKey            sceneTreeKey
	onRename           func()
	onChangeID         func()
	onReparented       func(layer, parentID string)
}

// objectContextMenuItems builds the unified object context menu items shared by the 2D viewport,
// 3D viewport, keyboard (Shift+F10 / ContextMenu key), and Scene Tree rows.
// Any item whose underlying feature does not apply to the target object (for example Select same prefab
// on a plain object, Find usages when no project is open, or Move under when no valid candidate parents exist)
// is omitted rather than shown disabled. Foreign (inherited / instanced-child) objects expose their
// override actions (Open Source Scene, Revert Overrides) and omit or disable local-only structural edits.
func objectContextMenuItems(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], id string, opts objectContextMenuOpts) []core.Node {
	if ed == nil || ed.Doc == nil || id == "" {
		return nil
	}
	obj, layerName, _, isForeign := ed.resolveContextObject(id)
	if obj == nil || layerName == "" {
		return nil
	}
	p := ed.SpecPalette(cx)
	displayName := contextObjectDisplayName(obj)
	subtitle := ed.ObjectContextSubtitle(id)
	editsOff := func() bool { _ = rev.Get(); return ed.Playing() || ed.AgentTouchingObject(id) }
	structOff := func() bool { return editsOff() || isForeign }
	hidden := func() bool { _ = rev.Get(); return ed.ObjectHidden(id) }
	locked := func() bool { _ = rev.Get(); return ed.ObjectLocked(id) }

	// Header: object display name + subtitle with kind and layer, styled using active Spec palette tokens.
	header := core.View{
		TestID: "context-menu-header",
		Style: core.Style{
			Padding: core.Edges{Top: 6, Right: 8, Bottom: 6, Left: 8},
			Gap:     2,
		},
		Children: []core.Node{
			core.Text{
				TestID:   "context-menu-title",
				Content:  displayName,
				MaxLines: 1,
				Style: core.Style{
					FontSize:   12,
					FontWeight: core.FontWeightSemibold,
					Color:      p.TextPrimary,
				},
			},
			core.Text{
				TestID:   "context-menu-subtitle",
				Content:  subtitle,
				MaxLines: 1,
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextMuted,
				},
			},
		},
	}

	pasteAction := func() {
		_, err := ed.Paste()
		ed.report(err)
	}

	targetIDs := func() []string {
		if ed.IsObjectSelected(id) {
			if sel := ed.SelectedIDs(); len(sel) > 0 {
				return sel
			}
		}
		return []string{id}
	}

	renameAction := func() {
		if opts.onRename != nil {
			opts.onRename()
			return
		}
		ed.report(ed.RequestRenameObject(id))
	}

	copyTestID := "context-menu-copy-yaml"
	pasteTestID := "context-menu-paste"
	dupTestID := "context-menu-duplicate"
	groupTestID := "context-menu-group"
	renameTestID := "context-menu-rename"
	hideTestID := "context-menu-hide"
	lockTestID := "context-menu-lock"
	findUsagesTestID := "context-menu-find-usages"
	openSceneTestID := "context-menu-open-scene"
	makeLocalTestID := "context-menu-make-local"
	applyOverridesTestID := "context-menu-apply-overrides"
	revertOverridesTestID := "context-menu-revert-overrides"
	openSourceTestID := "context-menu-open-source"
	if opts.includeTreeActions {
		copyTestID = "scene-tree-copy-" + id
		pasteTestID = "scene-tree-paste-" + id
		dupTestID = "scene-tree-duplicate-" + id
		groupTestID = "scene-tree-group-" + id
		renameTestID = "scene-tree-rename-" + id
		hideTestID = "scene-tree-hide-" + id
		lockTestID = "scene-tree-lock-item-" + id
		openSceneTestID = "scene-tree-open-scene-" + id
		makeLocalTestID = "scene-tree-make-local-" + id
		applyOverridesTestID = "scene-tree-apply-overrides-" + id
		revertOverridesTestID = "scene-tree-revert-overrides-" + id
		openSourceTestID = "scene-tree-open-source-" + id
		if obj.Scene != "" || (obj.Prefab != "" && isPathLikePrefab(obj.Prefab)) {
			findUsagesTestID = "scene-tree-find-usages-" + id
		}
	}

	items := []core.Node{
		header,
		ui.MenuSeparator{},
	}

	// Scene instance and inherited/foreign object override actions (#1184).
	if !isForeign && obj.Scene != "" {
		items = append(items,
			ui.MenuItem{
				TestID:   openSceneTestID,
				Text:     ed.T("overrides.openInstancedScene"),
				Disabled: func() bool { return editsOff() || ed.Project == nil || ed.ProjectPath == "" },
				OnSelect: func() { ed.report(ed.OpenInstancedScene(id)) },
			},
			ui.MenuItem{
				TestID:   makeLocalTestID,
				Text:     ed.T("overrides.makeLocal"),
				Disabled: editsOff,
				OnSelect: func() { ed.report(ed.MakeInstanceLocal(id)) },
			},
			ui.MenuItem{
				TestID: applyOverridesTestID,
				Text:   ed.T("overrides.applyToSource"),
				Disabled: func() bool {
					return editsOff() || ed.Project == nil || ed.ProjectPath == "" || ed.InstanceOverrideCount(id) == 0
				},
				OnSelect: func() { ed.report(ed.ApplyOverridesToSource(id)) },
			},
			ui.MenuItem{
				TestID:   revertOverridesTestID,
				Text:     ed.T("overrides.revertAll"),
				Disabled: func() bool { return editsOff() || ed.InstanceOverrideCount(id) == 0 },
				OnSelect: func() { ed.report(ed.RevertObjectOverrides(id)) },
			},
			ui.MenuSeparator{},
		)
	} else if isForeign {
		items = append(items,
			ui.MenuItem{
				TestID:   openSourceTestID,
				Text:     ed.T("overrides.openSourceScene"),
				Disabled: func() bool { return editsOff() || ed.Project == nil || ed.ProjectPath == "" },
				OnSelect: func() { ed.report(ed.OpenSourceScene(id)) },
			},
			ui.MenuItem{
				TestID:   revertOverridesTestID,
				Text:     ed.T("overrides.revertAll"),
				Disabled: func() bool { return editsOff() || !ed.ObjectHasAnyOverride(id) },
				OnSelect: func() { ed.report(ed.RevertObjectOverrides(id)) },
			},
			ui.MenuSeparator{},
		)
	}

	items = append(items, ui.MenuItem{
		TestID:   copyTestID,
		Text:     ed.T("contextMenu.copyYAML"),
		Icon:     lucide.Copy,
		Shortcut: ed.EffectiveShortcut(CmdCopy),
		OnSelect: func() {
			if ids := targetIDs(); len(ids) > 1 && ed.allSelectedObjectsAreLocal() {
				_, err := ed.CopyObjects(ids...)
				ed.report(err)
				return
			}
			_, _, err := ed.CopyObjectYAML(id)
			ed.report(err)
		},
	})

	if !isForeign || opts.includeTreeActions {
		items = append(items,
			ui.MenuItem{
				TestID:   pasteTestID,
				Text:     ed.T("contextMenu.paste"),
				Icon:     lucide.ClipboardPaste,
				Shortcut: ed.EffectiveShortcut(CmdPaste),
				Disabled: editsOff,
				OnSelect: pasteAction,
			},
			ui.MenuItem{
				TestID:   dupTestID,
				Text:     ed.T("contextMenu.duplicate"),
				Icon:     lucide.CopyPlus,
				Shortcut: ed.EffectiveShortcut(CmdDuplicate),
				Disabled: structOff,
				OnSelect: func() {
					_, err := ed.DuplicateObjects(targetIDs()...)
					ed.report(err)
				},
			},
			ui.MenuItem{
				TestID:   groupTestID,
				Text:     ed.T("contextMenu.group"),
				Icon:     lucide.FolderPlus,
				Shortcut: ed.EffectiveShortcut(CmdGroup),
				Disabled: structOff,
				OnSelect: func() {
					gid, err := ed.GroupObjects(targetIDs()...)
					if err != nil {
						ed.report(err)
						return
					}
					if opts.onReparented != nil && gid != "" {
						layer := layerName
						if gloc := ed.lookupObject(gid).loc; gloc.obj != nil && gloc.layer >= 0 {
							layer = ed.Doc.Layers[gloc.layer].Name
						}
						opts.onReparented(layer, gid)
					}
				},
			},
		)
	}

	// Move under submenu: omit when there are no candidate targets (including foreign objects).
	if !isForeign {
		candidates := ed.MoveUnderCandidates(id)
		if len(candidates) > 0 {
			subItems := make([]core.Node, 0, len(candidates))
			for _, c := range candidates {
				cand := c
				dotColor := p.Accent.Fg
				if cand.IsLayerRoot {
					dotColor = p.TextMuted
				} else if cand.Locked {
					dotColor = p.Warning.Fg
				} else if cand.ChildCount > 0 {
					dotColor = p.Changed.Fg
				}
				itemTestID := "context-menu-move-under-" + cand.ID
				if cand.IsLayerRoot {
					itemTestID = "context-menu-move-under-layer-" + cand.Layer
				}
				subItems = append(subItems, ui.MenuItem{
					TestID: itemTestID,
					Text:   cand.Name,
					Leading: core.View{
						Style: core.Style{
							Width:        core.Pt(7),
							Height:       core.Pt(7),
							CornerRadius: 2,
							Background:   dotColor,
							FlexShrink:   0,
						},
					},
					Trailing: core.Text{
						Content:  cand.Note,
						MaxLines: 1,
						Style: core.Style{
							FontSize: 11,
							Color:    p.TextMuted,
							Margin:   core.Edges{Left: 12},
						},
					},
					Disabled: func() bool {
						return editsOff() || cand.Current || cand.Locked
					},
					OnSelect: func() {
						if err := ed.MoveObjectUnder(id, cand.ID); err != nil {
							ed.report(err)
							return
						}
						if opts.onReparented != nil && cand.ID != "" {
							opts.onReparented(cand.Layer, cand.ID)
						}
					},
				})
			}
			items = append(items, ui.Submenu{
				TestID:   "context-menu-move-under",
				Text:     ed.T("contextMenu.moveUnder"),
				Icon:     lucide.CornerDownRight,
				Back:     ed.T("contextMenu.back"),
				Disabled: editsOff,
				Children: subItems,
			})
		}
	}

	// Select same prefab: omit when the object has neither Prefab nor instanced Scene.
	if obj.Prefab != "" || obj.Scene != "" {
		items = append(items, ui.MenuItem{
			TestID: "context-menu-select-same-prefab",
			Text:   ed.T("contextMenu.selectSamePrefab"),
			Icon:   lucide.Box,
			OnSelect: func() {
				_, err := ed.SelectSamePrefab(id)
				ed.report(err)
			},
		})
	}

	// Hide / Show & Lock / Unlock.
	hideIcon := lucide.EyeOff
	if ed.ObjectHidden(id) {
		hideIcon = lucide.Eye
	}
	lockIcon := lucide.Lock
	if ed.ObjectLocked(id) {
		lockIcon = lucide.LockOpen
	}
	items = append(items,
		ui.MenuSeparator{},
		ui.MenuItem{
			TestID:   hideTestID,
			Icon:     hideIcon,
			Shortcut: "h",
			TextFunc: func() string {
				if hidden() {
					return ed.T("contextMenu.show")
				}
				return ed.T("contextMenu.hide")
			},
			OnSelect: func() {
				ed.report(ed.SetObjectHidden(id, !ed.ObjectHidden(id)))
			},
		},
		ui.MenuItem{
			TestID: lockTestID,
			Icon:   lockIcon,
			TextFunc: func() string {
				if locked() {
					return ed.T("contextMenu.unlock")
				}
				return ed.T("contextMenu.lock")
			},
			OnSelect: func() {
				ed.report(ed.SetObjectLocked(id, !ed.ObjectLocked(id)))
			},
		},
		ui.MenuSeparator{},
	)

	// Find usages: omit when no project is open (project search/references requires a project).
	if ed.Project != nil && ed.ProjectPath != "" {
		items = append(items, ui.MenuItem{
			TestID:   findUsagesTestID,
			Text:     ed.T("contextMenu.findUsages"),
			Icon:     lucide.Search,
			Shortcut: ed.EffectiveShortcut(CmdFindUsages),
			OnSelect: func() {
				_, err := ed.ShowObjectUsages(id)
				ed.report(err)
			},
		})
	}

	if !isForeign || opts.includeTreeActions {
		items = append(items,
			ui.MenuItem{
				TestID:   renameTestID,
				Text:     ed.T("contextMenu.rename"),
				Icon:     lucide.Pencil,
				Shortcut: "f2",
				Disabled: structOff,
				OnSelect: renameAction,
			},
			ui.MenuSeparator{},
		)
	}

	items = append(items,
		ui.MenuItem{
			TestID:   "context-menu-ask-agent",
			Text:     ed.T("contextMenu.askAgent"),
			Shortcut: ed.EffectiveShortcut(CmdAskAgent),
			Leading:  ui.Icon(lucide.Sparkles, 14, func() core.Color { return p.Agent.Fg }),
			OnSelect: func() {
				_, err := ed.AskAgentAboutObject(id)
				ed.report(err)
			},
		},
	)

	// Scene Tree rows also preserve their existing hierarchy, layer, ID, and delete items.
	if opts.includeTreeActions {
		siblings := func() (index, count int, prev, parent string) {
			cur := ed.lookupObject(id).loc
			if cur.obj == nil {
				return -1, 0, "", ""
			}
			objs := ed.Doc.Layers[cur.layer].Objects
			if cur.parent != nil {
				objs = cur.parent.Children
				parent = cur.parent.ID
			}
			if cur.index > 0 {
				prev = objs[cur.index-1].ID
			}
			return cur.index, len(objs), prev, parent
		}

		hasChildren := func() bool {
			cur := ed.lookupObject(id).loc
			return cur.obj != nil && len(cur.obj.Children) > 0
		}

		items = append(items,
			ui.MenuSeparator{},
			ui.MenuItem{
				TestID:   "scene-tree-cut-" + id,
				Text:     ed.T("sceneTree.cut"),
				Shortcut: ed.EffectiveShortcut(CmdCut),
				Disabled: structOff,
				OnSelect: func() {
					_, err := ed.CutObjects(targetIDs()...)
					ed.report(err)
				},
			},
			ui.MenuItem{
				TestID:   "scene-tree-paste-into-" + id,
				Text:     ed.T("sceneTree.pasteInto"),
				Shortcut: ed.EffectiveShortcut(CmdPasteInto),
				Disabled: structOff,
				OnSelect: func() {
					if ids, err := ed.PasteInto(id); err == nil && len(ids) > 0 && opts.onReparented != nil {
						opts.onReparented(opts.treeKey.layer, id)
					}
				},
			},
			ui.MenuItem{
				TestID:   "scene-tree-ungroup-" + id,
				Text:     ed.T("sceneTree.ungroup"),
				Shortcut: ed.EffectiveShortcut(CmdUngroup),
				Disabled: func() bool { return structOff() || !hasChildren() },
				OnSelect: func() {
					_, err := ed.UngroupObject(id)
					ed.report(err)
				},
			},
			ui.MenuItem{
				TestID:   "scene-tree-change-id-" + id,
				Text:     ed.T("sceneTree.changeID"),
				Disabled: structOff,
				OnSelect: func() {
					if opts.onChangeID != nil {
						opts.onChangeID()
					}
				},
			},
			ui.MenuItem{
				TestID: "scene-tree-up-" + id,
				Text:   ed.T("sceneTree.moveUp"),
				Disabled: func() bool {
					i, _, _, _ := siblings()
					return structOff() || i <= 0
				},
				OnSelect: func() {
					if i, _, _, _ := siblings(); i > 0 {
						ed.report(ed.ReorderObject(id, i-1))
					}
				},
			},
			ui.MenuItem{
				TestID: "scene-tree-down-" + id,
				Text:   ed.T("sceneTree.moveDown"),
				Disabled: func() bool {
					i, n, _, _ := siblings()
					return structOff() || i < 0 || i >= n-1
				},
				OnSelect: func() {
					if i, n, _, _ := siblings(); i >= 0 && i < n-1 {
						ed.report(ed.ReorderObject(id, i+1))
					}
				},
			},
			ui.MenuItem{
				TestID: "scene-tree-indent-" + id,
				Text:   ed.T("sceneTree.indent"),
				Disabled: func() bool {
					_, _, prev, _ := siblings()
					return structOff() || prev == ""
				},
				OnSelect: func() {
					if _, _, prev, _ := siblings(); prev != "" {
						if err := ed.ReparentObject(id, prev, -1); err != nil {
							ed.report(err)
							return
						}
						if opts.onReparented != nil {
							opts.onReparented(opts.treeKey.layer, prev)
						}
					}
				},
			},
			ui.MenuItem{
				TestID: "scene-tree-outdent-" + id,
				Text:   ed.T("sceneTree.outdent"),
				Disabled: func() bool {
					_, _, _, parent := siblings()
					return structOff() || parent == ""
				},
				OnSelect: func() {
					if _, _, _, parent := siblings(); parent != "" {
						gp := ""
						if ploc := ed.lookupObject(parent).loc; ploc.parent != nil {
							gp = ploc.parent.ID
						}
						ed.report(ed.ReparentObject(id, gp, -1))
					}
				},
			},
		)
		if ed.Doc != nil && len(ed.Doc.Layers) > 1 {
			items = append(items, ui.MenuSeparator{})
			for li := range ed.Doc.Layers {
				name := ed.Doc.Layers[li].Name
				items = append(items, ui.MenuItem{
					TestID: "scene-tree-layer-" + id + "-" + name,
					Text:   ed.T("sceneTree.moveToLayer", l10n.Param("name", name)),
					Disabled: func() bool {
						cur := ed.lookupObject(id).loc
						return structOff() || cur.obj == nil || (ed.Doc.Layers[cur.layer].Name == name && cur.parent == nil)
					},
					OnSelect: func() { ed.report(ed.MoveObjectToLayer(id, name, -1)) },
				})
			}
		}
		items = append(items,
			ui.MenuSeparator{},
			ui.MenuItem{
				TestID:   "scene-tree-delete-" + id,
				Text:     ed.T("sceneTree.delete"),
				Shortcut: ed.EffectiveShortcut(CmdDelete),
				Variant:  ui.MenuItemDanger,
				Disabled: structOff,
				OnSelect: func() {
					if ed.IsSelected(id) && ed.SelectionCount() > 1 {
						ed.report(ed.DeleteSelected())
						return
					}
					ed.report(ed.DeleteObject(id))
				},
			},
		)
	}

	return items
}

// bindObjectContextMenuKeys wires keyboard triggers for the unified object context menu (Shift+F10 and
// the Menu/ContextMenu key) and object context menu shortcuts that do not clash with built-in commands.
func bindObjectContextMenuKeys(cx *core.Scope, ed *Editor) {
	inScene := func() bool {
		return ed != nil && !ed.shortcutCaptureActive && !ed.contextMenuBlocked()
	}
	openMenu := func() {
		if inScene() && ed.SelectedID != "" {
			ed.OpenObjectContextMenu(ed.SelectedID)
		}
	}
	opts := core.KeyOptions{When: core.KeyNotEditing}
	cx.OnKeyWith("shift+f10", opts, openMenu)
	cx.OnKeyWith("contextmenu", opts, openMenu)
	cx.OnKeyWith("h", opts, func() {
		if inScene() && ed.SelectedID != "" {
			_, err := ed.ToggleObjectHidden(ed.SelectedID)
			ed.report(err)
		}
	})
}
