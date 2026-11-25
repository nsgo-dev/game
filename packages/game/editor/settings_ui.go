package editor

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// EditorSettingsDialogView renders the per-user Editor Settings modal dialog (language and appearance).
// Changes apply live without a restart and persist to Editor.SettingsPath when set.
func EditorSettingsDialogView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		return cx.Env().Dark()
	})

	settingRow := func(testID, labelKey, helpKey string, control core.Node) core.Node {
		return core.View{
			TestID: testID,
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            16,
				Padding:        core.Symmetric(10, 0),
			},
			StyleFunc: func() core.Style {
				_ = darkMemo.Get()
				return core.Style{
					Borders:     core.Edges{Bottom: 1},
					BorderColor: colors.Line(),
				}
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							ContentFunc: func() string {
								_ = langMemo.Get()
								return ed.T(labelKey)
							},
							Style: core.Style{FontSize: 13, FontWeight: 600},
							StyleFunc: func() core.Style {
								_ = darkMemo.Get()
								return core.Style{Color: colors.Text()}
							},
						},
						core.Text{
							ContentFunc: func() string {
								_ = langMemo.Get()
								return ed.T(helpKey)
							},
							Style: core.Style{FontSize: 11},
							StyleFunc: func() core.Style {
								_ = darkMemo.Get()
								return core.Style{Color: colors.Muted()}
							},
						},
					},
				},
				control,
			},
		}
	}

	controls := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = langMemo.Get()
		_ = darkMemo.Get()

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
			Style: core.Style{Gap: 4},
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

	return ui.Dialog{
		TestID: "editor-settings-dialog",
		Open: func() bool {
			_ = rev.Get()
			return ed.EditorSettingsOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			ed.SetEditorSettingsOpen(open)
		},
		TitleFunc: func() string {
			_ = langMemo.Get()
			return ed.T("settings.title")
		},
		DescriptionFunc: func() string {
			_ = langMemo.Get()
			return ed.T("settings.description")
		},
		Style: core.Style{Width: core.Pt(520)},
		Children: []core.Node{
			controls,
		},
		Footer: []core.Node{
			ui.Button{
				TestID: "editor-settings-close",
				TextFunc: func() string {
					_ = langMemo.Get()
					return ed.T("settings.done")
				},
				Variant: ui.ButtonPrimary,
				OnPress: func() {
					ed.SetEditorSettingsOpen(false)
				},
			},
		},
	}
}
