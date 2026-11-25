package editor

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// EditorSettingsDialogView renders the per-user Editor Settings modal dialog with
// General (language and appearance) and Shortcuts (searchable, scope-aware rebinding) pages,
// styled strictly with Spec tokens from docs/design/editor/Spec.dc.html and Settings.dc.html.
// Changes apply live without a restart and persist to Editor.SettingsPath when set.
func EditorSettingsDialogView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})
	tabMemo := core.NewMemo(cx, func() EditorSettingsTab {
		_ = rev.Get()
		return ed.EditorSettingsTab()
	})
	customCountMemo := core.NewMemo(cx, func() int {
		_ = rev.Get()
		return ed.CustomShortcutCount()
	})
	query := core.NewSignal(cx, "")
	capturing := core.NewSignal(cx, "")
	conflict := core.NewSignal[*shortcutConflictState](cx, nil)

	// Clear transient capture/conflict state when closing the dialog, leaving the Shortcuts page, or entering Play mode.
	cx.Effect(func() {
		_ = rev.Get()
		if !ed.EditorSettingsOpen() || ed.EditorSettingsTab() != EditorSettingsTabShortcuts || ed.Playing() {
			if capturing.Get() != "" {
				capturing.Set("")
			}
			if conflict.Get() != nil {
				conflict.Set(nil)
			}
		}
	})

	settingRow := func(testID, labelKey, helpKey string, control core.Node) core.Node {
		return core.View{
			TestID: testID,
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            16,
				Padding:        core.Symmetric(12, 0),
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    specLine,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 3},
					Children: []core.Node{
						core.Text{
							ContentFunc: func() string {
								_ = langMemo.Get()
								return ed.T(labelKey)
							},
							Style: core.Style{FontSize: 13, FontWeight: 600, Color: specTextStrong},
						},
						core.Text{
							ContentFunc: func() string {
								_ = langMemo.Get()
								return ed.T(helpKey)
							},
							Style: core.Style{FontSize: 11.5, Color: specTextMuted},
						},
					},
				},
				control,
			},
		}
	}

	generalPage := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = langMemo.Get()

		langToggle := func(testID, text string, lang Language) core.Node {
			return ui.ToggleButton{
				TestID: testID,
				Text:   text,
				On: func() bool {
					_ = rev.Get()
					return ed.Language() == lang
				},
				OnChange: func(on bool) {
					if on {
						ed.report(ed.SetLanguage(lang))
					}
				},
			}
		}

		appearanceToggle := func(testID, text string, appearance Appearance) core.Node {
			return ui.ToggleButton{
				TestID: testID,
				Text:   text,
				On: func() bool {
					_ = rev.Get()
					return ed.Appearance() == appearance
				},
				OnChange: func(on bool) {
					if on {
						ed.report(ed.SetAppearance(appearance))
					}
				},
			}
		}

		return core.View{
			TestID: "editor-settings-general-page",
			Style:  core.Style{Gap: 4},
			Children: []core.Node{
				settingRow(
					"editor-settings-language",
					"settings.language",
					"settings.languageHelp",
					ui.ToggleGroup{
						TestID: "editor-settings-language-group",
						Label:  ed.T("settings.language"),
						Children: []core.Node{
							langToggle("editor-settings-lang-system", ed.T("settings.system"), LanguageSystem),
							langToggle("editor-settings-lang-en", ed.T("settings.langEnglish"), LanguageEnglish),
							langToggle("editor-settings-lang-zh", ed.T("settings.langChinese"), LanguageChinese),
						},
					},
				),
				settingRow(
					"editor-settings-appearance",
					"settings.appearance",
					"settings.appearanceHelp",
					ui.ToggleGroup{
						TestID: "editor-settings-appearance-group",
						Label:  ed.T("settings.appearance"),
						Children: []core.Node{
							appearanceToggle("editor-settings-appearance-system", ed.T("settings.system"), AppearanceSystem),
							appearanceToggle("editor-settings-appearance-light", ed.T("settings.light"), AppearanceLight),
							appearanceToggle("editor-settings-appearance-dark", ed.T("settings.dark"), AppearanceDark),
						},
					},
				),
			},
		}
	})

	body := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = langMemo.Get()
		activeTab := tabMemo.Get()

		navItem := func(testID, label string, showCustomCount bool, tab EditorSettingsTab) core.Node {
			selected := activeTab == tab
			bg := specBg
			fg := specTextSecondary
			borderCol := specBg
			if selected {
				bg = specRaised
				fg = specTextStrong
				borderCol = specLineRaised
			}
			btn := ui.Button{
				TestID:  testID,
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Color:   func() core.Color { return bg },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      fg,
				},
				Style: core.Style{
					Width:        core.Pct(100),
					Height:       core.Pt(32),
					CornerRadius: 8,
					BorderWidth:  1,
					BorderColor:  borderCol,
					FocusVisible: specRowFocusRingStyle,
				},
				OnPress: func() {
					ed.SetEditorSettingsTab(tab)
				},
			}
			if showCustomCount {
				btn.TextFunc = func() string {
					if n := customCountMemo.Get(); n > 0 {
						return fmt.Sprintf("%s · %d", label, n)
					}
					return label
				}
			} else {
				btn.Text = label
			}
			return btn
		}

		pageTitle := ed.T("settings.general")
		pageSub := ed.T("settings.generalHelp")
		var pageContent core.Node = generalPage
		if activeTab == EditorSettingsTabShortcuts {
			pageTitle = ed.T("settings.shortcuts")
			pageSub = ed.T("settings.shortcutsHelp")
			pageContent = shortcutsSettingsPageView(cx, ed, rev, query, capturing, conflict)
		}

		return core.View{
			Style: core.Style{
				Direction:    core.Row,
				Width:        core.Pt(780),
				Height:       core.Pt(520),
				Background:   specPanel,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
				CornerRadius: 18,
				Overflow:     core.Hidden,
			},
			Children: []core.Node{
				// Left navigation rail (Settings.dc.html: EDITOR · THIS COMPUTER)
				core.View{
					TestID: "editor-settings-sidebar",
					Style: core.Style{
						Width:       core.Pt(196),
						Background:  specBg,
						Borders:     core.Edges{Right: 1},
						BorderColor: specLine,
						Padding:     core.Symmetric(18, 12),
						Gap:         4,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        8,
								Padding:    core.Edges{Left: 8, Bottom: 12},
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(18),
										Height:       core.Pt(18),
										CornerRadius: 5,
										Background:   specAccent,
									},
								},
								core.Text{
									Content: ed.T("settings.title"),
									Style:   core.Style{FontSize: 13.5, FontWeight: 600, Color: specTextStrong},
								},
							},
						},
						core.Text{
							Content: ed.T("settings.sectionEditor"),
							Style: core.Style{
								FontSize:      10.5,
								FontWeight:    600,
								LetterSpacing: 0.6,
								Color:         specTextMuted,
								Padding:       core.Edges{Left: 8, Top: 4, Bottom: 4},
							},
						},
						navItem("editor-settings-tab-general", ed.T("settings.general"), false, EditorSettingsTabGeneral),
						navItem("editor-settings-tab-shortcuts", ed.T("settings.shortcuts"), true, EditorSettingsTabShortcuts),
					},
				},
				// Right content pane
				core.View{
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						Padding:    core.Symmetric(20, 24),
						Gap:        16,
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
									Style: core.Style{Gap: 3, FlexGrow: 1, FlexShrink: 1},
									Children: []core.Node{
										core.Text{
											Content: pageTitle,
											Style:   core.Style{FontSize: 16, FontWeight: 600, Color: specTextStrong},
										},
										core.Text{
											Content: pageSub,
											Style:   core.Style{FontSize: 12, Color: specTextMuted},
										},
									},
								},
								core.View{
									Style: core.Style{
										Height:         core.Pt(24),
										Padding:        core.Symmetric(0, 10),
										CornerRadius:   12,
										BorderWidth:    1,
										BorderColor:    specLineRaised,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("settings.thisComputer"),
											Style:   core.Style{FontSize: 11, Color: specTextMuted},
										},
									},
								},
							},
						},
						core.View{
							Style:    core.Style{FlexGrow: 1, MinHeight: core.Pt(0)},
							Children: []core.Node{pageContent},
						},
						core.View{
							Style: core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyEnd,
								Padding:        core.Edges{Top: 12},
								Borders:        core.Edges{Top: 1},
								BorderColor:    specLine,
							},
							Children: []core.Node{
								ui.Button{
									TestID:  "editor-settings-close",
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
										ed.SetEditorSettingsOpen(false)
									},
								},
							},
						},
					},
				},
			},
		}
	})

	return ui.Dialog{
		TestID: "editor-settings-dialog",
		Bare:   true,
		Open: func() bool {
			_ = rev.Get()
			return ed.EditorSettingsOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			ed.SetEditorSettingsOpen(open)
		},
		LabelFunc: func() string {
			_ = langMemo.Get()
			return ed.T("settings.title")
		},
		Style: core.Style{
			CornerRadius: 18,
			Background:   specPanel,
		},
		Children: []core.Node{body},
	}
}
