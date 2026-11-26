package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints strictly from docs/design/editor/Spec.dc.html color tokens.
var (
	specSettingsAccentTint = core.RGBA(0x3B7BFF, 0.14)
	specSettingsAccentEdge = core.RGBA(0x3B7BFF, 0.45)
	specSettingsActiveNav  = core.RGBA(0x3B7BFF, 0.16)
	specSettingsHoverNav   = core.RGBA(0xFFFFFF, 0.04)
	specSettingsProblemBg  = core.RGBA(0xFF6B6B, 0.08)
)

type settingsAgentPolicy uint8

const (
	policyYouOnly settingsAgentPolicy = iota
	policyAgentMayChange
)

// SettingsView renders the unified Settings screen matching docs/design/editor/Settings.dc.html
// and strictly using docs/design/editor/Spec.dc.html color, spacing, radius, type, and contrast tokens.
// It hosts the "PROJECT · game.yaml" pages (Project, Input map, Audio buses, Export) alongside
// "EDITOR · THIS COMPUTER" (General), keeping every control reachable with Tab and focus rings.
func SettingsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.SettingsOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return settingsShellView(cx, ed, rev)
		},
	}
}

// EditorSettingsDialogView renders the per-user Editor Settings view when mounted standalone.
// EditorView mounts SettingsView instead, which hosts the General page alongside the Project pages.
func EditorSettingsDialogView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return SettingsView(cx, ed, rev)
}

func settingsShellView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.shortcutCaptureActive {
			return
		}
		if ed.CapturingKey() {
			ed.CancelKeyCapture()
			return
		}
		if ed.SettingsOpen() {
			ed.CloseSettings()
		}
	})

	searchQuery := core.NewSignal(cx, "")
	shortcutsQuery := core.NewSignal(cx, "")
	shortcutsCapturing := core.NewSignal(cx, "")
	shortcutsConflict := core.NewSignal[*shortcutConflictState](cx, nil)

	activePage := core.NewMemo(cx, func() SettingsPage {
		_ = rev.Get()
		return ed.SettingsPage()
	})
	hasProject := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Project != nil
	})
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})

	navRow := func(page SettingsPage, labelKey, fallbackLabel string, metaFunc func() (string, bool), disabled func() bool) core.Node {
		return core.View{
			TestID: "settings-nav-row-" + string(page),
			StyleFunc: func() core.Style {
				active := activePage.Get() == page
				bg := specPanel
				if active {
					bg = specSettingsActiveNav
				}
				return core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(32),
					CornerRadius: 8,
					Background:   bg,
					Gap:          4,
				}
			},
			Children: []core.Node{
				ui.Button{
					TestID:   "settings-nav-" + string(page),
					Variant:  ui.ButtonGhost,
					Size:     ui.SizeSmall,
					Flex:     1,
					Disabled: disabled,
					TextFunc: func() string {
						_ = langMemo.Get()
						if labelKey != "" {
							return ed.T(labelKey)
						}
						return fallbackLabel
					},
					LabelFunc: func() string {
						_ = langMemo.Get()
						lbl := fallbackLabel
						if labelKey != "" {
							lbl = ed.T(labelKey)
						}
						if metaFunc != nil {
							if m, _ := metaFunc(); m != "" {
								return lbl + " · " + m
							}
						}
						return lbl
					},
					Color: func() core.Color {
						if activePage.Get() == page {
							return specSettingsActiveNav
						}
						return specPanel
					},
					TextStyle: core.Style{
						FontSize:   12.5,
						FontWeight: 500,
						Color:      specText,
						FlexGrow:   1,
						TextAlign:  core.TextAlignLeft,
					},
					Style: core.Style{
						Height:       core.Pt(32),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 8),
						FocusVisible: specFocusRingStyle,
					},
					OnPress: func() {
						ed.OpenSettingsPage(page)
					},
				},
				core.Show{
					When: func() bool {
						if metaFunc == nil {
							return false
						}
						m, _ := metaFunc()
						return m != ""
					},
					Then: func(cx *core.Scope) core.Node {
						return core.Text{
							TestID: "settings-nav-" + string(page) + "-meta",
							ContentFunc: func() string {
								m, _ := metaFunc()
								return m
							},
							StyleFunc: func() core.Style {
								_, warn := metaFunc()
								c := specTextMuted
								if warn {
									c = specChanged
								}
								return core.Style{
									FontSize:   11,
									FontWeight: 600,
									Color:      c,
									Padding:    core.Edges{Right: 8},
								}
							},
						}
					},
				},
			},
		}
	}

	noProject := func() bool {
		return !hasProject.Get()
	}

	sidebar := core.View{
		TestID: "settings-sidebar",
		Role:   core.RoleNavigation,
		Label:  "Settings pages",
		Style: core.Style{
			Width:       core.Pt(248),
			FlexShrink:  0,
			Background:  specPanel,
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
			Padding:     core.Symmetric(16, 12),
			Gap:         4,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			// Settings title header matching Settings.dc.html.
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
					Padding:    core.Edges{Left: 8, Right: 8, Bottom: 12},
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(20),
							Height:       core.Pt(20),
							CornerRadius: 6,
							Background:   specAccent,
							FlexShrink:   0,
						},
					},
					core.Text{
						ContentFunc: func() string {
							_ = langMemo.Get()
							return ed.T("settings.navTitle")
						},
						Style: core.Style{
							FontSize:   13,
							FontWeight: 600,
							Color:      specTextStrong,
						},
					},
					core.Show{
						When: hasProject.Get,
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "settings-sidebar-project-name",
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.Project == nil || ed.Project.Name == "" {
										return ""
									}
									return "· " + ed.Project.Name
								},
								Style: core.Style{
									FontSize:   12,
									Color:      specTextMuted,
									FlexShrink: 1,
								},
							}
						},
					},
				},
			},
			// PROJECT · game.yaml group
			core.Show{
				When: func() bool {
					return hasProject.Get() || activePage.Get().IsProjectPage()
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "settings-nav-group-project",
						Style:  core.Style{Gap: 4},
						Children: []core.Node{
							settingsSectionLabel("PROJECT · game.yaml"),
							navRow(SettingsPageProject, "settings.page.project", "Project", func() (string, bool) {
								_ = rev.Get()
								if ed.ProjectDirty() {
									return "Unsaved", true
								}
								return "", false
							}, noProject),
							navRow(SettingsPageInput, "settings.page.input", "Input map", func() (string, bool) {
								_ = rev.Get()
								if n := len(ed.InputActions()); n > 0 {
									return fmt.Sprintf("%d", n), false
								}
								return "", false
							}, noProject),
							navRow(SettingsPageAudio, "settings.page.audio", "Audio buses", func() (string, bool) {
								_ = rev.Get()
								if n := len(ed.AudioBuses()); n > 0 {
									return fmt.Sprintf("%d", n), false
								}
								return "", false
							}, noProject),
							navRow(SettingsPageExport, "settings.page.export", "Export", func() (string, bool) {
								_ = rev.Get()
								if warn := ed.ExportNavWarningMeta(); warn != "" {
									return warn, true
								}
								if n := len(ed.ExportPresets()); n > 0 {
									return "Ready", false
								}
								return "", false
							}, noProject),
						},
					}
				},
			},
			// EDITOR · THIS COMPUTER group
			core.View{
				TestID: "settings-nav-group-editor",
				Style:  core.Style{Gap: 4, Padding: core.Edges{Top: 12}},
				Children: []core.Node{
					settingsSectionLabel("EDITOR · THIS COMPUTER"),
					navRow(SettingsPageGeneral, "settings.page.general", "General", nil, nil),
					core.View{
						TestID: "settings-nav-row-shortcuts",
						StyleFunc: func() core.Style {
							active := activePage.Get() == SettingsPageShortcuts
							bg := specPanel
							if active {
								bg = specSettingsActiveNav
							}
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Height:       core.Pt(32),
								CornerRadius: 8,
								Background:   bg,
								Gap:          4,
							}
						},
						Children: []core.Node{
							ui.Button{
								TestID:  "editor-settings-tab-shortcuts",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Flex:    1,
								TextFunc: func() string {
									_ = rev.Get()
									_ = langMemo.Get()
									label := ed.T("settings.shortcuts")
									if n := len(ed.Settings().Shortcuts); n > 0 {
										return fmt.Sprintf("%s · %d", label, n)
									}
									return label
								},
								TextStyle: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      specText,
								},
								Style: core.Style{
									Height:         core.Pt(32),
									CornerRadius:   8,
									Padding:        core.Symmetric(0, 10),
									JustifyContent: core.JustifyStart,
									FocusVisible:   specFocusRingStyle,
								},
								OnPress: func() {
									ed.OpenSettingsPage(SettingsPageShortcuts)
								},
							},
						},
					},
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:  "settings-close",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				TextFunc: func() string {
					_ = langMemo.Get()
					return ed.T("settings.done")
				},
				Color: func() core.Color { return specRaised },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      specText,
				},
				Style: core.Style{
					Width:        core.Pct(100),
					Height:       core.Pt(32),
					CornerRadius: 8,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.CloseSettings()
				},
			},
		},
	}

	pageMeta := func(p SettingsPage) (title, subtitle, scope string, isProject bool) {
		switch p {
		case SettingsPageProject:
			return ed.T("settings.page.project"), ed.T("settings.page.projectSubtitle"), "Project · game.yaml", true
		case SettingsPageInput:
			return ed.T("settings.page.input"), ed.T("settings.page.inputSubtitle"), "Project · game.yaml", true
		case SettingsPageAudio:
			return ed.T("settings.page.audio"), ed.T("settings.page.audioSubtitle"), "Project · game.yaml", true
		case SettingsPageExport:
			return ed.T("settings.page.export"), ed.T("settings.page.exportSubtitle"), "Project · game.yaml", true
		case SettingsPageShortcuts:
			return ed.T("settings.shortcuts"), ed.T("settings.shortcutsHeaderHint"), "This computer", false
		default:
			return ed.T("settings.page.general"), ed.T("settings.page.generalSubtitle"), "This computer", false
		}
	}

	// Quick search bar at top of Settings matching Settings.dc.html.
	searchBar := core.View{
		TestID: "settings-search-bar",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Padding:     core.Symmetric(12, 24),
			Gap:         12,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
			Background:  specBg,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					ui.Input{
						TestID:      "settings-search",
						Label:       "Search settings",
						Placeholder: "Search settings — project, input, audio, export, language…",
						Value:       searchQuery.Get,
						OnChange:    searchQuery.Set,
						OnSubmit: func(q string) {
							if target, ok := matchSettingsPageQuery(q, ed.Project != nil); ok {
								ed.OpenSettingsPage(target)
							}
						},
					},
				},
			},
			core.Show{
				When: func() bool {
					return strings.TrimSpace(searchQuery.Get()) != ""
				},
				Then: func(cx *core.Scope) core.Node {
					return settingsSpecButton("settings-search-clear", "Clear", "Clear settings search", false, nil, func() {
						searchQuery.Set("")
					})
				},
			},
		},
	}

	pageHeader := core.View{
		TestID: "settings-page-header",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignEnd,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Edges{Top: 16, Right: 24, Bottom: 12, Left: 24},
			Gap:            16,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4},
				Children: []core.Node{
					core.Text{
						TestID: "settings-page-title",
						ContentFunc: func() string {
							_ = langMemo.Get()
							title, _, _, _ := pageMeta(activePage.Get())
							return title
						},
						Style: core.Style{
							FontSize:   18,
							FontWeight: 600,
							Color:      specTextStrong,
						},
					},
					core.Text{
						TestID: "settings-page-subtitle",
						ContentFunc: func() string {
							_ = langMemo.Get()
							_, sub, _, _ := pageMeta(activePage.Get())
							return sub
						},
						Style: core.Style{
							FontSize: 12.5,
							Color:    specTextMuted,
						},
					},
				},
			},
			core.View{
				TestID: "settings-page-scope",
				StyleFunc: func() core.Style {
					_, _, _, isProj := pageMeta(activePage.Get())
					bg := specRaised
					border := specLineRaised
					if isProj {
						bg = specSettingsAccentTint
						border = specSettingsAccentEdge
					}
					return core.Style{
						Height:         core.Pt(24),
						Padding:        core.Symmetric(0, 12),
						CornerRadius:   12,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Background:     bg,
						BorderWidth:    1,
						BorderColor:    border,
						FlexShrink:     0,
					}
				},
				Children: []core.Node{
					core.Text{
						ContentFunc: func() string {
							_ = langMemo.Get()
							_, _, scope, _ := pageMeta(activePage.Get())
							return scope
						},
						StyleFunc: func() core.Style {
							_, _, _, isProj := pageMeta(activePage.Get())
							fg := specTextMuted
							if isProj {
								fg = specTextStrong
							}
							return core.Style{
								FontSize:   11.5,
								FontWeight: 500,
								Color:      fg,
							}
						},
					},
				},
			},
		},
	}

	pageBody := core.View{
		TestID: "settings-page-body",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.Edges{Top: 4, Right: 24, Bottom: 16, Left: 24},
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				page := activePage.Get()
				projOpen := hasProject.Get()
				if page.IsProjectPage() && !projOpen {
					return settingsNoProjectView(ed)
				}
				switch page {
				case SettingsPageProject:
					return projectSettingsPanel(cx, ed, rev)
				case SettingsPageInput:
					return inputMapPanel(cx, ed, rev)
				case SettingsPageAudio:
					return audioMixerPanel(cx, ed, rev)
				case SettingsPageExport:
					return exportPanel(cx, ed, rev)
				case SettingsPageShortcuts:
					return shortcutsSettingsPageView(cx, ed, rev, shortcutsQuery, shortcutsCapturing, shortcutsConflict)
				default:
					return editorSettingsGeneralPage(cx, ed, rev)
				}
			}),
		},
	}

	return core.View{
		TestID: "settings-view",
		Role:   core.RoleRegion,
		Label:  "Settings",
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Right:      core.Pt(0),
			Bottom:     core.Pt(0),
			Left:       core.Pt(0),
			Direction:  core.Row,
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			sidebar,
			core.View{
				Style: core.Style{
					FlexGrow:   1,
					FlexBasis:  core.Pt(0),
					Background: specBg,
					Overflow:   core.Hidden,
				},
				Children: []core.Node{
					searchBar,
					pageHeader,
					pageBody,
				},
			},
		},
	}
}

func matchSettingsPageQuery(q string, hasProject bool) (SettingsPage, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return "", false
	}
	if hasProject {
		switch {
		case strings.Contains(q, "input") || strings.Contains(q, "key") || strings.Contains(q, "gamepad") || strings.Contains(q, "touch") || strings.Contains(q, "action"):
			return SettingsPageInput, true
		case strings.Contains(q, "audio") || strings.Contains(q, "bus") || strings.Contains(q, "mixer") || strings.Contains(q, "sound") || strings.Contains(q, "music"):
			return SettingsPageAudio, true
		case strings.Contains(q, "export") || strings.Contains(q, "build") || strings.Contains(q, "release") || strings.Contains(q, "ios") || strings.Contains(q, "macos") || strings.Contains(q, "web"):
			return SettingsPageExport, true
		case strings.Contains(q, "project") || strings.Contains(q, "name") || strings.Contains(q, "window") || strings.Contains(q, "scene") || strings.Contains(q, "plugin") || strings.Contains(q, "physics"):
			return SettingsPageProject, true
		}
	}
	if strings.Contains(q, "shortcut") || strings.Contains(q, "hotkey") || strings.Contains(q, "rebind") {
		return SettingsPageShortcuts, true
	}
	if strings.Contains(q, "lang") || strings.Contains(q, "appear") || strings.Contains(q, "dark") || strings.Contains(q, "light") || strings.Contains(q, "general") {
		return SettingsPageGeneral, true
	}
	return "", false
}

func settingsNoProjectView(ed *Editor) core.Node {
	return core.View{
		TestID: "settings-no-project",
		Style: core.Style{
			FlexGrow:       1,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            12,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.Text{
				Content: "No project open",
				Style:   core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong},
			},
			core.Text{
				Content: "Open a game.yaml project to configure Project, Input map, Audio buses, and Export settings.",
				Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
			},
			settingsSpecButton("settings-go-general", "Open General Settings", "Switch to General editor settings", false, nil, func() {
				ed.OpenSettingsPage(SettingsPageGeneral)
			}),
		},
	}
}

func editorSettingsGeneralPage(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})

	return core.View{
		TestID: "editor-settings-dialog",
		Style:  core.Style{FlexGrow: 1, Gap: 8},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
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
					Style: core.Style{Gap: 4},
					Children: []core.Node{
						settingsRow(
							"editor-settings-language",
							ed.T("settings.language"),
							ed.T("settings.languageHelp"),
							ui.ToggleGroup{
								TestID: "editor-settings-language-group",
								Label:  ed.T("settings.language"),
								Children: []core.Node{
									langToggle("editor-settings-lang-system", ed.T("settings.system"), LanguageSystem),
									langToggle("editor-settings-lang-en", ed.T("settings.langEnglish"), LanguageEnglish),
									langToggle("editor-settings-lang-zh", ed.T("settings.langChinese"), LanguageChinese),
								},
							},
							policyYouOnly,
							nil,
						),
						settingsRow(
							"editor-settings-appearance",
							ed.T("settings.appearance"),
							ed.T("settings.appearanceHelp"),
							ui.ToggleGroup{
								TestID: "editor-settings-appearance-group",
								Label:  ed.T("settings.appearance"),
								Children: []core.Node{
									appearanceToggle("editor-settings-appearance-system", ed.T("settings.system"), AppearanceSystem),
									appearanceToggle("editor-settings-appearance-light", ed.T("settings.light"), AppearanceLight),
									appearanceToggle("editor-settings-appearance-dark", ed.T("settings.dark"), AppearanceDark),
								},
							},
							policyYouOnly,
							nil,
						),
					},
				}
			}),
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					JustifyContent: core.JustifyEnd,
					Padding:        core.Edges{Top: 12},
				},
				Children: []core.Node{
					ui.Button{
						TestID:  "editor-settings-close",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						TextFunc: func() string {
							_ = langMemo.Get()
							return ed.T("settings.done")
						},
						Color: func() core.Color { return specAccent },
						TextStyle: core.Style{
							FontSize:   12.5,
							FontWeight: 600,
							Color:      specBg, // Spec.dc.html contrast table: #101216 on #3B7BFF (4.88:1 AA).
						},
						Style: core.Style{
							Height:       core.Pt(32),
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
	}
}

func settingsSectionLabel(text string) core.Node {
	return core.Text{
		Content: text,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.66,
			Color:         specTextMuted,
			Padding:       core.Edges{Top: 4, Right: 8, Bottom: 4, Left: 8},
		},
	}
}

func settingsPolicyBadge(policy settingsAgentPolicy) core.Node {
	text := "You only"
	color := specTextMuted
	if policy == policyAgentMayChange {
		text = "Agent may change"
		color = specAgent
	}
	return core.Text{
		Content: text,
		Style: core.Style{
			Width:      core.Pt(116),
			FlexShrink: 0,
			FontSize:   11,
			FontWeight: 500,
			Color:      color,
			TextAlign:  core.TextAlignRight,
		},
	}
}

// settingsRow renders one Settings row in the docs/design/editor/Settings.dc.html layout:
// label + help on the left, value control in the middle/right, "Agent may change" or "You only" policy column on the right, and optional inline error text underneath.
func settingsRow(testID, label, help string, control core.Node, policy settingsAgentPolicy, errNode core.Node) core.Node {
	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        16,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4},
					Children: []core.Node{
						core.Text{
							Content: label,
							Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: specTextStrong},
						},
						core.Text{
							Content: help,
							Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
						},
					},
				},
				core.View{
					Style:    core.Style{FlexShrink: 0},
					Children: []core.Node{control},
				},
				settingsPolicyBadge(policy),
			},
		},
	}
	if errNode != nil {
		children = append(children, errNode)
	}
	return core.View{
		TestID: testID,
		Style: core.Style{
			Padding:     core.Symmetric(12, 0),
			Gap:         4,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: children,
	}
}

// settingsSpecButton builds a real accessible ui.Button styled strictly with Spec.dc.html tokens and focus ring.
func settingsSpecButton(testID, text, label string, primary bool, disabled func() bool, onPress func()) core.Node {
	bg := specRaised
	fg := specText
	var borderW core.Points = 1
	borderColor := specLineRaised
	if primary {
		bg = specAccent
		fg = specBg // Spec.dc.html contrast table: #101216 on #3B7BFF (4.88:1 AA), never #FFFFFF on accent.
		borderW = 0
		borderColor = 0
	}
	return ui.Button{
		TestID:   testID,
		Text:     text,
		Label:    label,
		Variant:  ui.ButtonGhost,
		Size:     ui.SizeSmall,
		Disabled: disabled,
		Color:    func() core.Color { return bg },
		TextStyle: core.Style{
			FontSize:   12,
			FontWeight: 600,
			Color:      fg,
		},
		Style: core.Style{
			Height:       core.Pt(28),
			CornerRadius: 8,
			BorderWidth:  borderW,
			BorderColor:  borderColor,
			FlexShrink:   0,
			FocusVisible: specFocusRingStyle,
		},
		OnPress: onPress,
	}
}
