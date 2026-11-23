package editor

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// EditorView returns the root NSGo UI node for ed, composing:
//   - Top toolbar (scene title, tool selector, Undo/Redo, Play/Stop, Save)
//   - Left Asset Browser panel
//   - Center interactive Canvas viewport
//   - Right Property Inspector panel
//   - Bottom status bar
func EditorView(cx *core.Scope, ed *Editor) core.Node {
	ed.AttachScope(cx)
	rev := core.NewSignal(cx, ed.Revision())
	unsub := ed.Subscribe(func() {
		rev.Set(ed.Revision())
	})
	cx.Cleanup(func() {
		unsub()
		if ed.Playing() {
			_ = ed.Stop()
		}
		if ed.scope == cx {
			ed.scope = nil
		}
	})

	colors := ui.ColorsOf(cx)

	return core.View{
		TestID: "scene-editor",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: colors.Surface(),
		},
		Children: []core.Node{
			toolbarView(cx, ed, rev),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					AssetBrowserView(cx, ed, rev),
					ViewportView(cx, ed, rev),
					InspectorView(cx, ed, rev),
				},
			},
			statusBarView(cx, ed, rev),
		},
	}
}

func toolbarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	notPlaying := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	toolBtn := func(testID, label string, tool Tool) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: notPlaying,
			On: func() bool {
				_ = rev.Get()
				return ed.Tool == tool
			},
			OnChange: func(on bool) {
				if on {
					ed.SetTool(tool)
				}
			},
		}
	}

	viewModeBtn := func(testID, label string, mode ViewMode) core.Node {
		return ui.ToggleButton{
			TestID: testID,
			Text:   label,
			On: func() bool {
				_ = rev.Get()
				return ed.ViewMode == mode
			},
			OnChange: func(on bool) {
				if on {
					ed.SetViewMode(mode)
				}
			},
		}
	}

	gizmoBtn := func(testID, label string, mode GizmoMode3D) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: notPlaying,
			On: func() bool {
				_ = rev.Get()
				return ed.GizmoMode == mode
			},
			OnChange: func(on bool) {
				if on {
					ed.SetGizmoMode3D(mode)
				}
			},
		}
	}

	return core.View{
		TestID: "editor-toolbar",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(8, 12),
			Gap:            10,
			Background:     colors.Raised(),
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    colors.Line(),
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID: "toolbar-scene-name",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.Doc != nil && ed.Doc.Name != "" {
								return ed.Doc.Name
							}
							return "Untitled Scene"
						},
						Style: core.Style{
							FontSize:   14,
							FontWeight: 700,
							Color:      colors.Text(),
						},
					},
					ui.Toolbar{
						Label: "Editor Tools",
						Children: []core.Node{
							ui.ToolbarGroup{
								Label: "View Mode",
								Children: []core.Node{
									viewModeBtn("view-mode-2d", "2D", ViewMode2D),
									viewModeBtn("view-mode-3d", "3D", ViewMode3D),
								},
							},
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.ViewMode == ViewMode3D
								},
								Then: func(cx *core.Scope) core.Node {
									return ui.ToolbarGroup{
										Label: "3D Gizmo",
										Children: []core.Node{
											gizmoBtn("gizmo-translate", "Translate", Gizmo3DTranslate),
											gizmoBtn("gizmo-rotate", "Rotate", Gizmo3DRotate),
											gizmoBtn("gizmo-scale", "Scale", Gizmo3DScale),
										},
									}
								},
								Else: func(cx *core.Scope) core.Node {
									return ui.ToolbarGroup{
										Label: "Tools",
										Children: []core.Node{
											toolBtn("tool-select", "Select", ToolSelect),
											toolBtn("tool-move", "Move", ToolMove),
											toolBtn("tool-place", "Place", ToolPlace),
											toolBtn("tool-paint", "Paint", ToolPaintTile),
											toolBtn("tool-erase", "Erase", ToolEraseTile),
											toolBtn("tool-fill", "Fill", ToolFillTile),
										},
									}
								},
							},
						},
					},
				},
			},
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					ui.Button{
						TestID:  "toolbar-undo",
						Text:    "Undo",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || !ed.History.CanUndo()
						},
						OnPress: func() {
							ed.Undo()
						},
					},
					ui.Button{
						TestID:  "toolbar-redo",
						Text:    "Redo",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || !ed.History.CanRedo()
						},
						OnPress: func() {
							ed.Redo()
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.Playing()
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "toolbar-play",
								Text:    "Stop",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonPrimary,
								OnPress: func() {
									_ = ed.Stop()
								},
							}
						},
						Else: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "toolbar-play",
								Text:    "Play",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonPrimary,
								OnPress: func() {
									_ = ed.Play()
								},
							}
						},
					},
					ui.Button{
						TestID:   "toolbar-save",
						Text:     "Save",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonOutline,
						Disabled: notPlaying,
						OnPress: func() {
							data, err := ed.SaveYAML()
							if err != nil {
								ed.Status = "Save error: " + err.Error()
								ed.notify()
								return
							}
							if ed.OnSave != nil {
								if err := ed.OnSave(data); err != nil {
									ed.Status = "Save error: " + err.Error()
									ed.notify()
									return
								}
							} else if ed.FilePath != "" {
								if err := ed.SaveFile(ed.FilePath); err != nil {
									ed.Status = "Save error: " + err.Error()
									ed.notify()
									return
								}
							} else {
								// Nothing was asked to persist the scene: say so
								// instead of reporting a save that never happened.
								ed.Status = "Save error: no save path; open or save the scene to a file first"
								ed.notify()
								return
							}
							// An edited project file is saved with the scene; an unedited one is left as written, comments included.
							if ed.ProjectDirty() {
								if err := ed.SaveProject(); err != nil {
									ed.Status = "Save error: " + err.Error()
									ed.notify()
								}
							}
						},
					},
				},
			},
		},
	}
}

func statusBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "editor-status",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(4, 12),
			Background:     colors.Raised(),
			Borders:        core.Edges{Top: 1},
			BorderColor:    colors.Line(),
		},
		Children: []core.Node{
			core.Text{
				ContentFunc: func() string {
					_ = rev.Get()
					mode := "Edit"
					if ed.Playing() {
						mode = "Play"
					}
					if ed.ViewMode == ViewMode3D {
						return fmt.Sprintf("Mode: %s | View: 3D (%s) | Layer: %s", mode, ed.GizmoMode.String(), ed.ActiveLayer)
					}
					return fmt.Sprintf("Mode: %s | View: 2D | Tool: %s | Layer: %s", mode, ed.Tool.String(), ed.ActiveLayer)
				},
				Style: core.Style{
					FontSize: 11,
					Color:    colors.Muted(),
				},
			},
			core.Text{
				ContentFunc: func() string {
					_ = rev.Get()
					if ed.BuildErr != nil {
						return "Build error: " + ed.BuildErr.Error()
					}
					return ed.Status
				},
				Style: core.Style{
					FontSize: 11,
					Color:    colors.Muted(),
				},
			},
		},
	}
}
