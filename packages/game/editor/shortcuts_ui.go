package editor

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

type shortcutConflictState struct {
	cmdID         string
	shortcut      string
	conflictID    string
	conflictLabel string
	scope         string
}

// shortcutsSettingsPageView renders the Settings › Shortcuts page inside EditorSettingsDialogView,
// styled strictly with Spec color tokens from docs/design/editor/Spec.dc.html.
func shortcutsSettingsPageView(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	query *core.Signal[string],
	capturing *core.Signal[string],
	conflict *core.Signal[*shortcutConflictState],
) core.Node {
	captureRef := &core.Ref{}
	uiKeyMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.shortcutsUIStateKey()
	})

	// Keep ed.shortcutCaptureActive synchronized with the local capture signal and move focus onto
	// the capture target immediately when recording begins.
	cx.Effect(func() {
		id := capturing.Get()
		ed.shortcutCaptureActive = id != ""
		if id != "" {
			captureRef.Focus()
		}
	})
	// Cancel capture if the captured command is no longer present in the filtered/active command list.
	cx.Effect(func() {
		_ = uiKeyMemo.Get()
		q := query.Get()
		id := capturing.Get()
		if id == "" {
			return
		}
		visible := false
		for _, cmd := range ed.FilterShortcutCommands(q, cx.Env().OS()) {
			if cmd.ID == id {
				visible = true
				break
			}
		}
		if !visible {
			capturing.Set("")
		}
	})
	cx.Cleanup(func() {
		ed.shortcutCaptureActive = false
	})

	startCapture := func(cmdID string) {
		conflict.Set(nil)
		capturing.Set(cmdID)
	}
	cancelCapture := func() {
		capturing.Set("")
		conflict.Set(nil)
	}

	handleCaptureKey := func(cmdID string, ev core.KeyEvent) bool {
		if strings.EqualFold(ev.Key, "escape") || strings.EqualFold(ev.Key, "esc") {
			cancelCapture()
			return true
		}
		sc, ok := ShortcutFromKeyEvent(ev, cx.Env().OS())
		if !ok {
			// Ignore bare modifier presses while keeping focus on the capture box.
			return true
		}
		if err := validateRebindableShortcut(sc); err != nil {
			cancelCapture()
			ed.report(err)
			return true
		}
		if cid, clabel, hasConflict := ed.ShortcutConflict(cmdID, sc); hasConflict {
			_, scope, _, _ := ed.defaultCommandInfo(cmdID)
			capturing.Set("")
			conflict.Set(&shortcutConflictState{
				cmdID:         cmdID,
				shortcut:      sc,
				conflictID:    cid,
				conflictLabel: clabel,
				scope:         scope,
			})
			return true
		}
		capturing.Set("")
		conflict.Set(nil)
		ed.report(ed.RebindShortcut(cmdID, sc))
		return true
	}

	toolbar := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					ui.Input{
						TestID: "shortcuts-search",
						Label:  ed.T("settings.shortcutsSearchLabel"),
						Placeholder: func() string {
							return ed.T("settings.shortcutsSearchPlaceholder")
						}(),
						Value: query.Get,
						OnChange: func(v string) {
							if capturing.Get() != "" {
								cancelCapture()
							}
							query.Set(v)
						},
						Style: core.Style{
							Height:       core.Pt(32),
							Background:   specRaised,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
							CornerRadius: 8,
							Color:        specText,
							FontSize:     12.5,
						},
					},
				},
			},
			ui.Button{
				TestID:  "shortcuts-reset-all",
				Text:    ed.T("settings.shortcutsResetAll"),
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					_ = uiKeyMemo.Get()
					return !ed.HasCustomShortcuts()
				},
				Color: func() core.Color { return specRaised },
				TextStyle: core.Style{
					FontSize:   12,
					FontWeight: 500,
					Color:      specText,
				},
				Style: core.Style{
					Height:       core.Pt(32),
					CornerRadius: 8,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					cancelCapture()
					ed.report(ed.ResetAllShortcuts())
				},
			},
		},
	}

	rows := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = uiKeyMemo.Get()
		q := query.Get()
		capID := capturing.Get()
		conf := conflict.Get()
		targetOS := cx.Env().OS()

		cmds := ed.FilterShortcutCommands(q, targetOS)
		if len(cmds) == 0 {
			return core.View{
				TestID: "shortcuts-empty",
				Style: core.Style{
					Padding:    core.Symmetric(24, 12),
					AlignItems: core.AlignCenter,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("settings.shortcutsEmpty"),
						Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
					},
				},
			}
		}

		items := make([]core.Node, 0, len(cmds))
		for _, cmd := range cmds {
			c := cmd
			isCapturing := capID == c.ID
			isCustom := ed.ShortcutOverridden(c.ID)
			scopeText := ed.CommandScopeLabel(c.Scope)

			keyText := ed.T("settings.shortcutsUnbound")
			keyColor := specTextMuted
			if c.Shortcut != "" {
				keyText = FormatShortcut(c.Shortcut, targetOS)
				keyColor = specTextStrong
			}

			var keyControl core.Node
			if isCapturing {
				keyControl = core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
					Children: []core.Node{
						core.View{
							TestID:    "shortcut-capture-" + c.ID,
							Label:     ed.T("settings.shortcutsCapturePrompt"),
							Ref:       captureRef,
							Focusable: func() bool { return true },
							OnBlur: func() {
								if capturing.Get() == c.ID {
									capturing.Set("")
								}
							},
							OnKeyDown: func(ev core.KeyEvent) bool {
								return handleCaptureKey(c.ID, ev)
							},
							Style: core.Style{
								Height:         core.Pt(28),
								MinWidth:       core.Pt(112),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   6,
								Background:     specRaised,
								BorderWidth:    1,
								BorderColor:    specAccent,
								OutlineWidth:   2,
								OutlineColor:   specAccent,
								OutlineOffset:  2,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: ed.T("settings.shortcutsCapturePrompt"),
									Style: core.Style{
										FontSize:   11.5,
										FontWeight: 600,
										FontFamily: core.FontMonospace,
										Color:      specTextStrong,
									},
								},
							},
						},
						ui.Button{
							TestID:  "shortcut-capture-cancel",
							Text:    ed.T("settings.shortcutsCaptureCancel"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonGhost,
							Color:   func() core.Color { return specLineRaised },
							TextStyle: core.Style{
								FontSize: 11.5,
								Color:    specText,
							},
							Style: core.Style{
								Height:       core.Pt(28),
								CornerRadius: 6,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: cancelCapture,
						},
					},
				}
			} else {
				borderCol := specLineRaised
				if isCustom {
					borderCol = specChanged
				}
				keyControl = core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
					Children: func() []core.Node {
						nodes := []core.Node{
							ui.Button{
								TestID:  "shortcut-bind-" + c.ID,
								Text:    keyText,
								Label:   c.Label + " · " + keyText,
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return specRaised },
								TextStyle: core.Style{
									FontSize:   11.5,
									FontWeight: 500,
									FontFamily: core.FontMonospace,
									Color:      keyColor,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									MinWidth:     core.Pt(72),
									CornerRadius: 6,
									BorderWidth:  1,
									BorderColor:  borderCol,
									FocusVisible: specFocusRingStyle,
								},
								OnPress: func() {
									startCapture(c.ID)
								},
							},
						}
						if isCustom {
							nodes = append(nodes, ui.Button{
								TestID:  "shortcut-reset-" + c.ID,
								Text:    ed.T("settings.shortcutsReset"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return specPanel },
								TextStyle: core.Style{
									FontSize: 11.5,
									Color:    specTextSecondary,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 6,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
									FocusVisible: specFocusRingStyle,
								},
								OnPress: func() {
									cancelCapture()
									ed.report(ed.ResetShortcut(c.ID))
								},
							})
						}
						return nodes
					}(),
				}
			}

			labelChildren := []core.Node{
				core.Text{
					Content: c.Label,
					Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: specTextStrong},
				},
			}
			if isCustom {
				labelChildren = append(labelChildren, core.View{
					Style: core.Style{
						Padding:      core.Symmetric(1, 6),
						CornerRadius: 6,
						Background:   specWarningFill,
						BorderWidth:  1,
						BorderColor:  specWarningEdge,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("settings.shortcutsCustom"),
							Style:   core.Style{FontSize: 10.5, FontWeight: 600, Color: specChanged},
						},
					},
				})
			}

			rowChildren := []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            12,
						MinHeight:      core.Pt(36),
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        8,
								FlexGrow:   1,
								FlexShrink: 1,
								MinWidth:   core.Pt(0),
							},
							Children: labelChildren,
						},
						core.View{
							TestID: "shortcut-scope-" + c.ID,
							Style: core.Style{
								Height:         core.Pt(22),
								Padding:        core.Symmetric(0, 8),
								CornerRadius:   6,
								Background:     specRaised,
								BorderWidth:    1,
								BorderColor:    specLineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: scopeText,
									Style:   core.Style{FontSize: 11, Color: specTextSecondary},
								},
							},
						},
						keyControl,
					},
				},
			}

			if conf != nil && conf.cmdID == c.ID {
				confCopy := *conf
				rowChildren = append(rowChildren, core.View{
					TestID: "shortcut-conflict-" + c.ID,
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            10,
						Padding:        core.Symmetric(8, 12),
						CornerRadius:   8,
						Background:     specWarningFill,
						BorderWidth:    1,
						BorderColor:    specWarningEdge,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("settings.shortcutsConflict",
								l10n.Param("key", FormatShortcut(confCopy.shortcut, targetOS)),
								l10n.Param("other", confCopy.conflictLabel),
								l10n.Param("scope", ed.CommandScopeLabel(confCopy.scope)),
							),
							Style: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      specWarning,
								FlexGrow:   1,
								FlexShrink: 1,
							},
						},
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
							Children: []core.Node{
								ui.Button{
									TestID:  "shortcut-conflict-swap",
									Text:    ed.T("settings.shortcutsSwap"),
									Size:    ui.SizeSmall,
									Variant: ui.ButtonGhost,
									Color:   func() core.Color { return specAccent },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 600,
										Color:      specBg, // Spec contrast table: #101216 on #3B7BFF (4.88:1 AA).
									},
									Style: core.Style{
										Height:       core.Pt(28),
										CornerRadius: 8,
										FocusVisible: specFocusRingStyle,
									},
									OnPress: func() {
										conflict.Set(nil)
										capturing.Set("")
										ed.report(ed.SwapShortcut(confCopy.cmdID, confCopy.shortcut))
									},
								},
								ui.Button{
									TestID:  "shortcut-conflict-cancel",
									Text:    ed.T("settings.shortcutsCaptureCancel"),
									Size:    ui.SizeSmall,
									Variant: ui.ButtonGhost,
									Color:   func() core.Color { return specLineRaised },
									TextStyle: core.Style{
										FontSize: 12,
										Color:    specText,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										CornerRadius: 8,
										FocusVisible: specFocusRingStyle,
									},
									OnPress: cancelCapture,
								},
							},
						},
					},
				})
			}

			items = append(items, core.View{
				TestID: "shortcut-row-" + c.ID,
				Style: core.Style{
					Gap:         6,
					Padding:     core.Symmetric(6, 4),
					Borders:     core.Edges{Bottom: 1},
					BorderColor: specLine,
				},
				Children: rowChildren,
			})
		}

		return core.View{
			Style:    core.Style{Gap: 0},
			Children: items,
		}
	})

	return core.View{
		TestID: "editor-settings-shortcuts-page",
		Style:  core.Style{Gap: 12, FlexGrow: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{
			toolbar,
			ui.ScrollView{
				TestID:    "shortcuts-list",
				EdgeColor: func() core.Color { return specPanel },
				Style: core.Style{
					Height: core.Pt(332),
				},
				Children: []core.Node{rows},
			},
		},
	}
}

// ShortcutsSheetView renders the ⌘/ Keyboard Shortcuts reference sheet grouped by keyboard scope,
// styled strictly with Spec color tokens from docs/design/editor/Spec.dc.html.
func ShortcutsSheetView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})
	uiKeyMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.shortcutsUIStateKey()
	})

	content := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = uiKeyMemo.Get()
		_ = langMemo.Get()
		targetOS := cx.Env().OS()
		apple := targetOS == core.OSMacOS || targetOS == core.OSIOS
		subtitle := ed.T("shortcutsSheet.subtitleOther")
		if apple {
			subtitle = ed.T("shortcutsSheet.subtitleMac")
		}

		// Group bound commands by scope in deterministic display order.
		byScope := make(map[string][]EditorCommand)
		var extraScopes []string
		for _, cmd := range ed.Commands() {
			if cmd.Shortcut == "" {
				continue
			}
			sc := normalizeCommandScope(cmd.Scope)
			byScope[sc] = append(byScope[sc], cmd)
			if !slices.Contains(scopeDisplayOrder, sc) && !slices.Contains(extraScopes, sc) {
				extraScopes = append(extraScopes, sc)
			}
		}
		slices.Sort(extraScopes)
		orderedScopes := append(slices.Clone(scopeDisplayOrder), extraScopes...)

		var cards []core.Node
		for _, sc := range orderedScopes {
			cmds := byScope[sc]
			if len(cmds) == 0 {
				continue
			}
			rowNodes := make([]core.Node, 0, len(cmds)+1)
			rowNodes = append(rowNodes, core.Text{
				Content: strings.ToUpper(ed.CommandScopeLabel(sc)),
				Style: core.Style{
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.6,
					Color:         specTextMuted,
					Padding:       core.Edges{Bottom: 6},
				},
			})
			for _, cmd := range cmds {
				keyDisplay := FormatShortcut(cmd.Shortcut, targetOS)
				if len(cmd.AlternateShortcuts) > 0 {
					for _, alt := range cmd.AlternateShortcuts {
						keyDisplay += "  " + FormatShortcut(alt, targetOS)
					}
				}
				rowNodes = append(rowNodes, core.View{
					TestID: "shortcuts-sheet-row-" + cmd.ID,
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        12,
						MinHeight:  core.Pt(28),
					},
					Children: []core.Node{
						core.Text{
							TestID:  "shortcuts-sheet-key-" + cmd.ID,
							Content: keyDisplay,
							Style: core.Style{
								Width:      core.Pt(112),
								FontSize:   12,
								FontWeight: 500,
								FontFamily: core.FontMonospace,
								Color:      specTextStrong,
							},
						},
						core.Text{
							Content: cmd.Label,
							Style: core.Style{
								FontSize:   12.5,
								Color:      specText,
								FlexGrow:   1,
								FlexShrink: 1,
							},
						},
					},
				})
			}
			cards = append(cards, core.View{
				TestID: "shortcuts-sheet-scope-" + sc,
				Style: core.Style{
					Background:   specPanel,
					BorderWidth:  1,
					BorderColor:  specLine,
					CornerRadius: 12,
					Padding:      core.Symmetric(14, 18),
					Gap:          2,
				},
				Children: rowNodes,
			})
		}

		sheetKey := FormatShortcut(ed.EffectiveShortcut(CmdShortcutsSheet), targetOS)

		return core.View{
			Style: core.Style{
				Background:   specBg,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
				CornerRadius: 18,
				Width:        core.Pt(720),
				Padding:      core.Symmetric(20, 24),
				Gap:          16,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            12,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{Gap: 4},
							Children: []core.Node{
								core.Text{
									Content: ed.T("shortcutsSheet.title"),
									Style:   core.Style{FontSize: 16, FontWeight: 600, Color: specTextStrong},
								},
								core.Text{
									Content: subtitle,
									Style:   core.Style{FontSize: 12, Color: specTextMuted},
								},
							},
						},
						core.View{
							Style: core.Style{
								Height:         core.Pt(24),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   12,
								Background:     specRaised,
								BorderWidth:    1,
								BorderColor:    specLineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: sheetKey,
									Style: core.Style{
										FontSize:   11.5,
										FontFamily: core.FontMonospace,
										Color:      specTextSecondary,
									},
								},
							},
						},
					},
				},
				ui.ScrollView{
					TestID:    "shortcuts-sheet-list",
					EdgeColor: func() core.Color { return specBg },
					Style:     core.Style{Height: core.Pt(380)},
					ContentStyle: core.Style{
						Gap: 12,
					},
					Children: cards,
				},
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Padding:        core.Edges{Top: 12},
						Borders:        core.Edges{Top: 1},
						BorderColor:    specLine,
					},
					Children: []core.Node{
						ui.Button{
							TestID:  "shortcuts-sheet-customize",
							Text:    ed.T("shortcutsSheet.customize"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonGhost,
							Color:   func() core.Color { return specRaised },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      specText,
							},
							Style: core.Style{
								Height:       core.Pt(30),
								CornerRadius: 8,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								ed.SetShortcutsSheetOpen(false)
								ed.OpenShortcutsSettings()
							},
						},
						ui.Button{
							TestID:  "shortcuts-sheet-close",
							Text:    ed.T("settings.done"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonGhost,
							Color:   func() core.Color { return specTextStrong },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 600,
								Color:      specBg,
							},
							Style: core.Style{
								Height:       core.Pt(30),
								CornerRadius: 8,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								ed.SetShortcutsSheetOpen(false)
							},
						},
					},
				},
			},
		}
	})

	return ui.Dialog{
		TestID: "editor-shortcuts-sheet",
		Bare:   true,
		Open: func() bool {
			_ = rev.Get()
			return ed.ShortcutsSheetOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			ed.SetShortcutsSheetOpen(open)
		},
		LabelFunc: func() string {
			_ = langMemo.Get()
			return ed.T("shortcutsSheet.title")
		},
		Style: core.Style{
			CornerRadius: 18,
			Background:   specBg,
		},
		Children: []core.Node{content},
	}
}
