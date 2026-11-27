package editor

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// layerDrag is what a row of the layers panel carries while it is dragged.
type layerDrag struct{ name string }

// layerHint is where a dragged layer would land on the row of name: before it when before is set, else after it.
type layerHint struct {
	name   string
	before bool
	on     bool
}

// LayersView renders the layers panel: the open scene's layers in draw order, the first drawn at the top, each with its space, its tilemap, an eye button that shows or hides it (SetLayerVisible, written to the scene file) and a lock button that locks it against the viewports for the session (SetLayerLocked). Pressing a layer's row makes it the active layer, which placing and painting target and the status bar names. The Add button adds a world layer drawn over the others (AddLayer) and makes it active, and the Remove button removes the active layer with its objects (RemoveLayer). Dragging a row above or below another moves the layer there in the draw order (MoveLayer). A row's context menu, or a double press, renames it in place (RenameLayer); the menu also moves it up or down, sets its space (SetLayerSpace), hides, locks or removes it. On the row of a layer that extends one of the base scene's, the eye, renaming, moving, dragging and the space items are off, since that scene owns them. Every edit but the lock goes through History.
func LayersView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	selection := ui.NewSelection(cx, false)
	editing := core.NewSignal(cx, "")
	hint := core.NewSignal(cx, layerHint{})
	editsOff := func() bool { _ = rev.Get(); return ed.Playing() }

	// Every row reads the layers several times per change, so they are listed once per editor revision.
	var cached []LayerInfo
	cachedRev := ^uint64(0)
	layers := func() []LayerInfo {
		_ = rev.Get()
		if r := ed.Revision(); r != cachedRev || cached == nil {
			cached, cachedRev = ed.Layers(), r
		}
		return cached
	}
	roots := func() []any {
		ls := layers()
		out := make([]any, len(ls))
		for i := range ls {
			out[i] = ls[i].Name
		}
		return out
	}

	// A press on a row makes its layer the active one.
	cx.Effect(func() {
		cur, _ := selection.Cursor().(string)
		cx.Untrack(func() {
			if cur != "" && ed.ActiveLayer != cur && (ed.layerIndex(cur) >= 0 || ed.inheritedLayers()[cur]) {
				ed.SetActiveLayer(cur)
			}
		})
	})
	// The active layer's row is the selected one, whichever control changed it.
	cx.Effect(func() {
		_ = rev.Get()
		cx.Untrack(func() {
			cur, _ := selection.Cursor().(string)
			switch {
			case ed.ActiveLayer == "":
				if cur != "" {
					selection.Clear()
				}
			case cur != ed.ActiveLayer:
				selection.Set(ed.ActiveLayer)
			}
		})
	})

	startEdit := func(name string) {
		if ed.Playing() || ed.layerIndex(name) < 0 || ed.inheritedLayers()[name] {
			return
		}
		editing.Set(name)
	}
	add := func() {
		name := ed.newLayerName()
		if err := ed.AddLayer(name, sceneformat.SpaceWorld); err != nil {
			ed.report(err)
			return
		}
		ed.SetActiveLayer(name)
	}

	row := func(cx *core.Scope, item ui.TreeItem) core.Node {
		return core.Dynamic(func(cx *core.Scope) core.Node {
			name, _ := item.Key().(string)
			return layerPanelRow(cx, ed, layers, name, editing, hint, startEdit, editsOff)
		})
	}

	return core.View{
		TestID: "layers-panel",
		Style: dockedStyle(cx, core.Style{
			Height:      core.Pt(200),
			FlexShrink:  0,
			Background:  colors.Surface(),
			Borders:     core.Edges{Top: 1, Right: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(8),
			Gap:         6,
		}),
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2},
				Children: []core.Node{
					core.Text{
						TestID:  "layers-title",
						Content: ed.T("layers.title"),
						Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
					},
					ui.IconButton{
						TestID:   "layers-add",
						Label:    ed.T("layers.add"),
						Icon:     lucide.Plus,
						Size:     22,
						IconSize: 14,
						Disabled: editsOff,
						OnPress:  add,
					},
					ui.IconButton{
						TestID:   "layers-remove",
						Label:    ed.T("layers.remove"),
						Icon:     lucide.Trash,
						Size:     22,
						IconSize: 14,
						Disabled: func() bool { return editsOff() || ed.ActiveLayer == "" },
						OnPress:  func() { ed.report(ed.RemoveLayer(ed.ActiveLayer)) },
					},
				},
			},
			dragdrop.Area{Children: []core.Node{ui.Tree{
				TestID:     "layers-list",
				Label:      ed.T("layers.title"),
				Roots:      roots,
				Children:   func(any) []any { return nil },
				Expanded:   func(any) bool { return false },
				OnActivate: func(key any) { name, _ := key.(string); startEdit(name) },
				RowLabel: func(key any) string {
					name, _ := key.(string)
					return ed.T("layers.rowLabel", l10n.Param("name", name))
				},
				Selection: selection,
				Row:       row,
				Empty: core.Text{
					Content: ed.T("layers.empty"),
					Style:   core.Style{FontSize: 12, Color: colors.Muted(), Padding: core.All(4)},
				},
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					FocusVisible: ed.SpecPalette(cx).FocusRing(-2),
				},
				RowStyle: core.Style{
					FocusVisible: ed.SpecPalette(cx).FocusRing(-2),
				},
			}}},
		},
	}
}

// layerPosition returns the position of the layer name in ls, the layers in draw order, and their number; the position is -1 when ls has no such layer.
func layerPosition(ls []LayerInfo, name string) (int, int) {
	return slices.IndexFunc(ls, func(l LayerInfo) bool { return l.Name == name }), len(ls)
}

func layerPanelRow(cx *core.Scope, ed *Editor, layers func() []LayerInfo, name string, editing *core.Signal[string], hint *core.Signal[layerHint], startEdit func(string), editsOff func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	rowRef := new(core.Ref)
	info := func() LayerInfo {
		ls := layers()
		if i, _ := layerPosition(ls, name); i >= 0 {
			return ls[i]
		}
		return LayerInfo{}
	}
	before := func(ev dragdrop.Event) bool {
		b, ok := rowRef.Bounds()
		return !ok || ev.Position.Y < b.Size.Height/2
	}
	track := func(ev dragdrop.Event) { hint.Set(layerHint{name: name, before: before(ev), on: true}) }

	label := core.Show{
		When: func() bool { return editing.Get() == name },
		Then: func(cx *core.Scope) core.Node {
			text := name
			commit := func() {
				if editing.GetUntracked() != name {
					return
				}
				editing.Set("")
				ed.report(ed.RenameLayer(name, strings.TrimSpace(text)))
			}
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if editing.GetUntracked() == name {
					editing.Set("")
				}
			})
			return specInputField(cx, ed, ui.Input{
				TestID:       "layers-rename-" + name,
				Label:        ed.T("layers.nameLabel"),
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
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					core.Text{
						TestID:  "layers-name-" + name,
						Content: name,
						Style:   core.Style{FontSize: 12, Color: colors.Text()},
						StyleFunc: func() core.Style {
							if l := info(); !l.Visible || l.Locked {
								return core.Style{Color: colors.Muted()}
							}
							if info().Active {
								return core.Style{FontWeight: 700}
							}
							return core.Style{}
						},
					},
					core.Text{
						TestID: "layers-meta-" + name,
						ContentFunc: func() string {
							l := info()
							meta := string(l.Space)
							if l.Tilemap != "" {
								meta += " · " + l.Tilemap
							}
							return meta
						},
						Style: core.Style{FontSize: 10, Color: colors.Muted()},
					},
				},
			}
		},
	}

	toggle := func(testID string, on func() bool, set func(bool) error, onIcon, offIcon func() *core.SVG, onLabel, offLabel string, disabled func() bool) core.Node {
		return ui.IconButton{
			TestID:   testID,
			Size:     20,
			IconSize: 13,
			Icon: func() *core.SVG {
				if on() {
					return onIcon()
				}
				return offIcon()
			},
			LabelFunc: func() string {
				if on() {
					return onLabel
				}
				return offLabel
			},
			Selected: on,
			Disabled: disabled,
			OnPress:  func() { ed.report(set(!on())) },
		}
	}
	hidden := func() bool { return !info().Visible }
	locked := func() bool { return info().Locked }
	setHidden := func(on bool) error { return ed.SetLayerVisible(name, !on) }
	setLocked := func(on bool) error { return ed.SetLayerLocked(name, on) }
	never := func() bool { return false }
	// A layer that extends one of the base scene's has its name and draw order owned by that scene,
	// while visibility, space, parallax, and repeat can be overridden in layerOverrides.
	baseOwned := func() bool { return editsOff() || info().Inherited }

	content := core.View{
		TestID: "layers-row-" + name,
		Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, MinWidth: core.Pt(0), Gap: 2},
		StyleFunc: func() core.Style {
			h := hint.Get()
			s := core.Style{Borders: core.Edges{Top: 2, Bottom: 2}, BorderColor: colors.Surface()}
			if h.on && h.name == name {
				if h.before {
					s.BorderColors = core.EdgeColors{Top: colors.Accent()}
				} else {
					s.BorderColors = core.EdgeColors{Bottom: colors.Accent()}
				}
			}
			return s
		},
		Children: []core.Node{
			label,
			toggle("layers-eye-"+name, hidden, setHidden, lucide.EyeOff, lucide.Eye, ed.T("layers.showNamed", l10n.Param("name", name)), ed.T("layers.hideNamed", l10n.Param("name", name)), editsOff),
			toggle("layers-lock-"+name, locked, setLocked, lucide.Lock, lucide.LockOpen, ed.T("layers.unlockNamed", l10n.Param("name", name)), ed.T("layers.lockNamed", l10n.Param("name", name)), never),
		},
	}

	move := func(delta int) {
		if pos, _ := layerPosition(layers(), name); pos >= 0 {
			ed.report(ed.MoveLayer(name, pos+delta))
		}
	}
	spaceItem := func(space sceneformat.Space, text string) core.Node {
		return ui.MenuItem{
			TestID:   "layers-space-" + string(space) + "-" + name,
			Text:     text,
			Disabled: func() bool { return editsOff() || info().Space == space },
			OnSelect: func() { ed.report(ed.SetLayerSpace(name, space)) },
		}
	}
	items := []core.Node{
		ui.MenuItem{TestID: "layers-inspect-" + name, Text: ed.T("layers.inspectLayer"), OnSelect: func() {
			ed.SelectObject("")
			ed.SetActiveLayer(name)
		}},
		ui.MenuItem{TestID: "layers-rename-item-" + name, Text: ed.T("layers.rename"), Disabled: baseOwned, OnSelect: func() { startEdit(name) }},
		ui.MenuSeparator{},
		ui.MenuItem{TestID: "layers-up-" + name, Text: ed.T("layers.moveUp"),
			Disabled: func() bool { pos, _ := layerPosition(layers(), name); return baseOwned() || pos <= 0 },
			OnSelect: func() { move(-1) }},
		ui.MenuItem{TestID: "layers-down-" + name, Text: ed.T("layers.moveDown"),
			Disabled: func() bool { pos, n := layerPosition(layers(), name); return baseOwned() || pos < 0 || pos >= n-1 },
			OnSelect: func() { move(1) }},
		ui.MenuSeparator{},
		spaceItem(sceneformat.SpaceWorld, ed.T("layers.worldSpace")),
		spaceItem(sceneformat.SpaceScreen, ed.T("layers.screenSpace")),
		ui.MenuSeparator{},
		ui.MenuItem{TestID: "layers-hide-" + name, TextFunc: func() string {
			if hidden() {
				return ed.T("layers.show")
			}
			return ed.T("layers.hide")
		}, Disabled: editsOff, OnSelect: func() { ed.report(setHidden(!hidden())) }},
		ui.MenuItem{TestID: "layers-lock-item-" + name, TextFunc: func() string {
			if locked() {
				return ed.T("layers.unlock")
			}
			return ed.T("layers.lock")
		}, OnSelect: func() { ed.report(setLocked(!locked())) }},
		ui.MenuSeparator{},
		ui.MenuItem{TestID: "layers-revert-overrides-" + name, Text: ed.T("overrides.revertAll"),
			Disabled: func() bool { return editsOff() || !info().Inherited || len(ed.LayerOverrideFields(name)) == 0 },
			OnSelect: func() { ed.report(ed.RevertLayerOverrides(name)) }},
		ui.MenuItem{TestID: "layers-remove-" + name, Text: ed.T("layers.remove"), Variant: ui.MenuItemDanger, Disabled: editsOff,
			OnSelect: func() { ed.report(ed.RemoveLayer(name)) }},
	}

	return ui.ContextMenu{
		TestID: "layers-menu-" + name,
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
						if editsOff() || editing.GetUntracked() == name || ed.inheritedLayers()[name] {
							return nil
						}
						return layerDrag{name: name}
					},
					Preview: func(any) core.Node {
						return core.Text{Content: name, Style: core.Style{FontSize: 12, Color: colors.Text(), Padding: core.Symmetric(2, 6), Background: colors.Raised(), CornerRadius: 4}}
					},
				}},
			},
			Accept: func(data any) bool {
				d, ok := data.(layerDrag)
				return ok && d.name != name
			},
			OnEnter: track,
			OnMove:  track,
			OnLeave: func(dragdrop.Event) { hint.Set(layerHint{}) },
			OnDrop: func(ev dragdrop.Event) {
				hint.Set(layerHint{})
				d, ok := ev.Data.(layerDrag)
				if !ok {
					return
				}
				ed.report(ed.MoveLayer(d.name, dropPosition(ed, d.name, name, before(ev))))
			},
		},
		Children: items,
	}
}

// dropPosition returns the draw-order position MoveLayer takes for the layer dragged to land just before or after the layer target: target's position among the other layers, one more after it.
func dropPosition(ed *Editor, dragged, target string, before bool) int {
	pos := 0
	for _, l := range ed.Layers() {
		if l.Name == dragged {
			continue
		}
		if l.Name == target {
			break
		}
		pos++
	}
	if !before {
		pos++
	}
	return pos
}
