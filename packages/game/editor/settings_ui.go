package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

type settingsAgentPolicy uint8

const (
	policyYouOnly settingsAgentPolicy = iota
	policyAgentMayChange
)

// SettingsView renders the unified Settings screen matching docs/design/editor/Settings.dc.html
// and strictly using SpecPalette color, spacing, radius, type, and contrast tokens in both Dark
// and Light appearances. It hosts the "PROJECT · game.yaml" pages (Project, Input map, Audio buses,
// Export) alongside "EDITOR · THIS COMPUTER" (General, Shortcuts), keeping every control reachable
// with Tab and focus rings.
func SettingsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type settingsViewKey struct {
		open bool
		dark bool
	}
	key := core.NewMemo(cx, func() settingsViewKey {
		_ = rev.Get()
		return settingsViewKey{
			open: ed.SettingsOpen() && !ed.Playing(),
			dark: cx.Dark(),
		}
	})
	bindViewFocusRestore(cx, ed, func() bool {
		return key.Get().open
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if !key.Get().open {
			return nil
		}
		return settingsShellView(cx, ed, rev)
	})
}

// EditorSettingsDialogView renders the per-user Editor Settings view when mounted standalone.
// EditorView mounts SettingsView instead, which hosts the General and Shortcuts pages alongside the Project pages.
func EditorSettingsDialogView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return SettingsView(cx, ed, rev)
}

func settingsShellView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	accentTint := ui.Alpha(p.Accent.Fg, 0.14)
	accentEdge := ui.Alpha(p.Accent.Fg, 0.45)
	activeNav := ui.Alpha(p.Accent.Fg, 0.16)
	focusRing := p.FocusRing(2)

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

	// Clear transient shortcut capture/conflict state when closing Settings, leaving Shortcuts, or entering Play mode.
	cx.Effect(func() {
		_ = rev.Get()
		if !ed.SettingsOpen() || ed.SettingsPage() != SettingsPageShortcuts || ed.Playing() {
			if shortcutsCapturing.Get() != "" {
				shortcutsCapturing.Set("")
			}
			if shortcutsConflict.Get() != nil {
				shortcutsConflict.Set(nil)
			}
		}
	})

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
				bg := p.Panel
				if active {
					bg = activeNav
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
							return activeNav
						}
						return p.Panel
					},
					TextStyle: core.Style{
						FontSize:   12.5,
						FontWeight: 500,
						Color:      p.TextSecondary,
						FlexGrow:   1,
						TextAlign:  core.TextAlignLeft,
					},
					Style: core.Style{
						Height:       core.Pt(32),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 8),
						FocusVisible: focusRing,
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
								c := p.TextFaint
								if warn {
									c = p.Changed.Fg
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
			Background:  p.Panel,
			Borders:     core.Edges{Right: 1},
			BorderColor: p.Line,
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
							Background:   p.Accent.Fg,
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
							Color:      p.TextPrimary,
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
									Color:      p.TextFaint,
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
							settingsSectionLabelDynamic(p, func() string {
								_ = langMemo.Get()
								return ed.T("settings.sectionProject")
							}),
							navRow(SettingsPageProject, "settings.page.project", "Project", func() (string, bool) {
								_ = rev.Get()
								_ = langMemo.Get()
								if ed.ProjectDirty() {
									return ed.T("settings.metaUnsaved"), true
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
								_ = langMemo.Get()
								if warn := ed.ExportNavWarningMeta(); warn != "" {
									return warn, true
								}
								if n := len(ed.ExportPresets()); n > 0 {
									return ed.T("settings.metaReady"), false
								}
								return "", false
							}, noProject),
						},
					}
				},
			},
			// AGENT · permissions.yaml group (always available; persists to permissions.yaml when a project is open)
			core.View{
				TestID: "settings-nav-group-permissions",
				Style:  core.Style{Gap: 4, Padding: core.Edges{Top: 12}},
				Children: []core.Node{
					settingsSectionLabelDynamic(p, func() string {
						_ = langMemo.Get()
						return ed.T("permissions.sectionAgent")
					}),
					navRow(SettingsPagePermissions, "permissions.title", "Permissions", func() (string, bool) {
						_ = rev.Get()
						_ = langMemo.Get()
						perms := ed.Permissions()
						meta := ed.localizedProfileName(perms.Profile)
						if perms.IsModified() {
							return meta + " · " + ed.T("permissions.changed"), true
						}
						return meta, false
					}, nil),
				},
			},
			// EDITOR · THIS COMPUTER group
			core.View{
				TestID: "settings-nav-group-editor",
				Style:  core.Style{Gap: 4, Padding: core.Edges{Top: 12}},
				Children: []core.Node{
					settingsSectionLabelDynamic(p, func() string {
						_ = langMemo.Get()
						return ed.T("settings.sectionEditor")
					}),
					navRow(SettingsPageGeneral, "settings.page.general", "General", nil, nil),
					core.View{
						TestID: "settings-nav-row-shortcuts",
						StyleFunc: func() core.Style {
							active := activePage.Get() == SettingsPageShortcuts
							bg := p.Panel
							if active {
								bg = activeNav
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
								Color: func() core.Color {
									if activePage.Get() == SettingsPageShortcuts {
										return activeNav
									}
									return p.Panel
								},
								TextStyle: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      p.TextSecondary,
									FlexGrow:   1,
									TextAlign:  core.TextAlignLeft,
								},
								Style: core.Style{
									Height:       core.Pt(32),
									CornerRadius: 8,
									Padding:      core.Symmetric(0, 8),
									FocusVisible: focusRing,
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
				Color: func() core.Color { return p.Raised },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      p.TextSecondary,
				},
				Style: core.Style{
					Width:        core.Pct(100),
					Height:       core.Pt(32),
					CornerRadius: 8,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FocusVisible: focusRing,
				},
				OnPress: func() {
					ed.CloseSettings()
				},
			},
		},
	}

	pageMeta := func(page SettingsPage) (title, subtitle, scope string, isProject bool) {
		switch page {
		case SettingsPageProject:
			return ed.T("settings.page.project"), ed.T("settings.page.projectSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPageInput:
			return ed.T("settings.page.input"), ed.T("settings.page.inputSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPageAudio:
			return ed.T("settings.page.audio"), ed.T("settings.page.audioSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPageExport:
			return ed.T("settings.page.export"), ed.T("settings.page.exportSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPagePermissions:
			return ed.T("permissions.title"), ed.T("permissions.subtitle"), PermissionsFileName, true
		case SettingsPageShortcuts:
			return ed.T("settings.shortcuts"), ed.T("settings.shortcutsHelp"), ed.T("settings.scopeComputer"), false
		default:
			return ed.T("settings.page.general"), ed.T("settings.page.generalSubtitle"), ed.T("settings.scopeComputer"), false
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
			BorderColor: p.Line,
			Background:  p.Bg,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					ui.Input{
						TestID:      "settings-search",
						Label:       ed.T("settings.searchLabel"),
						Placeholder: ed.T("settings.searchPlaceholder"),
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
					return settingsSpecButton(p, "settings-search-clear", ed.T("settings.searchClear"), ed.T("settings.searchClear"), false, nil, func() {
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
							Color:      p.TextPrimary,
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
							Color:    p.TextFaint,
						},
					},
				},
			},
			core.View{
				TestID: "settings-page-scope",
				StyleFunc: func() core.Style {
					_, _, _, isProj := pageMeta(activePage.Get())
					bg := p.Raised
					border := p.LineRaised
					if isProj {
						bg = accentTint
						border = accentEdge
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
							fg := p.TextFaint
							if isProj {
								fg = p.TextPrimary
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

	editorSettingsDoneFooter := func() core.Node {
		return core.View{
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
					Color: func() core.Color { return p.Accent.Fg },
					TextStyle: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.Accent.OnFill,
					},
					Style: core.Style{
						Height:       core.Pt(32),
						CornerRadius: 8,
						FocusVisible: focusRing,
					},
					OnPress: func() {
						ed.SetEditorSettingsOpen(false)
					},
				},
			},
		}
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
					return settingsNoProjectView(p, ed)
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
				case SettingsPagePermissions:
					return permissionsSettingsPageBody(cx, ed, rev)
				case SettingsPageShortcuts:
					return core.View{
						TestID: "editor-settings-shortcuts-wrap",
						Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Overflow: core.Hidden},
						Children: []core.Node{
							shortcutsSettingsPageView(cx, ed, rev, shortcutsQuery, shortcutsCapturing, shortcutsConflict),
							editorSettingsDoneFooter(),
						},
					}
				default:
					return editorSettingsGeneralPage(cx, ed, rev, p, editorSettingsDoneFooter())
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
			Background: p.Bg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			sidebar,
			core.View{
				Style: core.Style{
					FlexGrow:   1,
					FlexBasis:  core.Pt(0),
					Background: p.Bg,
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
	if strings.Contains(q, "shortcut") || strings.Contains(q, "keymap") || strings.Contains(q, "hotkey") || strings.Contains(q, "rebind") || strings.Contains(q, "快捷键") {
		return SettingsPageShortcuts, true
	}
	if strings.Contains(q, "permission") || strings.Contains(q, "mcp") || strings.Contains(q, "agent") || strings.Contains(q, "rule") || strings.Contains(q, "cautious") || strings.Contains(q, "balanced") || strings.Contains(q, "autonomous") || strings.Contains(q, "权限") {
		return SettingsPagePermissions, true
	}
	if hasProject {
		switch {
		case strings.Contains(q, "input") || strings.Contains(q, "key") || strings.Contains(q, "gamepad") || strings.Contains(q, "touch") || strings.Contains(q, "action") || strings.Contains(q, "输入") || strings.Contains(q, "手柄"):
			return SettingsPageInput, true
		case strings.Contains(q, "audio") || strings.Contains(q, "bus") || strings.Contains(q, "mixer") || strings.Contains(q, "sound") || strings.Contains(q, "music") || strings.Contains(q, "音频") || strings.Contains(q, "总线") || strings.Contains(q, "混音"):
			return SettingsPageAudio, true
		case strings.Contains(q, "export") || strings.Contains(q, "build") || strings.Contains(q, "release") || strings.Contains(q, "ios") || strings.Contains(q, "macos") || strings.Contains(q, "web") || strings.Contains(q, "导出") || strings.Contains(q, "构建") || strings.Contains(q, "发布"):
			return SettingsPageExport, true
		case strings.Contains(q, "project") || strings.Contains(q, "name") || strings.Contains(q, "window") || strings.Contains(q, "scene") || strings.Contains(q, "plugin") || strings.Contains(q, "physics") || strings.Contains(q, "项目") || strings.Contains(q, "窗口") || strings.Contains(q, "场景") || strings.Contains(q, "物理") || strings.Contains(q, "插件"):
			return SettingsPageProject, true
		}
	}
	if strings.Contains(q, "lang") || strings.Contains(q, "appear") || strings.Contains(q, "dark") || strings.Contains(q, "light") || strings.Contains(q, "general") || strings.Contains(q, "agent") || strings.Contains(q, "model") || strings.Contains(q, "mcp") || strings.Contains(q, "connect") || strings.Contains(q, "语言") || strings.Contains(q, "外观") || strings.Contains(q, "深色") || strings.Contains(q, "浅色") || strings.Contains(q, "通用") || strings.Contains(q, "智能体") || strings.Contains(q, "模型") || strings.Contains(q, "连接") {
		return SettingsPageGeneral, true
	}
	return "", false
}

func settingsNoProjectView(p SpecPalette, ed *Editor) core.Node {
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
				Content: ed.T("settings.noProjectTitle"),
				Style:   core.Style{FontSize: 14, FontWeight: 600, Color: p.TextPrimary},
			},
			core.Text{
				Content: ed.T("settings.noProjectBody"),
				Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
			},
			settingsSpecButton(p, "settings-go-general", ed.T("settings.goGeneral"), ed.T("settings.goGeneral"), false, nil, func() {
				ed.OpenSettingsPage(SettingsPageGeneral)
			}),
		},
	}
}

func editorSettingsGeneralPage(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, footer core.Node) core.Node {
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
				curPal := ed.SpecPalette(cx)

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
							curPal,
							ed,
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
							curPal,
							ed,
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
						settingsRow(
							curPal,
							ed,
							"editor-settings-autosave",
							ed.T("settings.autoSave"),
							ed.T("settings.autoSaveHelp"),
							ui.Switch{
								TestID: "editor-settings-autosave-switch",
								Label:  ed.T("settings.autoSave"),
								Size:   ui.SizeSmall,
								Checked: func() bool {
									_ = rev.Get()
									return ed.AutoSave()
								},
								OnChange: func(on bool) {
									ed.report(ed.SetAutoSave(on))
								},
							},
							policyYouOnly,
							nil,
						),
						settingsRow(
							curPal,
							ed,
							"editor-settings-tools",
							ed.T("settings.tools"),
							ed.T("settings.toolsHelp"),
							core.Text{
								TestID: "editor-settings-tools-list",
								ContentFunc: func() string {
									_ = rev.Get()
									servers := ed.AssetGenerators()
									if len(servers) == 0 {
										return ed.T("settings.toolsNone")
									}
									names := make([]string, 0, len(servers))
									for _, s := range servers {
										names = append(names, s.Config.Name+" ("+string(s.Config.Kind)+")")
									}
									return strings.Join(names, ", ")
								},
								Style: core.Style{FontSize: 11.5, Color: curPal.TextMuted},
							},
							policyYouOnly,
							nil,
						),
						editorSettingsModelsSection(cx, ed, rev),
					},
				}
			}),
			footer,
		},
	}
}

func settingsSectionLabelDynamic(p SpecPalette, textFunc func() string) core.Node {
	return core.Text{
		ContentFunc: textFunc,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.66,
			Color:         p.TextFaint,
			Padding:       core.Edges{Top: 4, Right: 8, Bottom: 4, Left: 8},
		},
	}
}

func settingsPolicyBadge(p SpecPalette, ed *Editor, policy settingsAgentPolicy) core.Node {
	text := "You only"
	color := p.TextFaint
	if policy == policyAgentMayChange {
		text = "Agent may change"
		color = p.Agent.Fg
	}
	if ed != nil {
		if policy == policyAgentMayChange {
			text = ed.T("settings.policyAgent")
		} else {
			text = ed.T("settings.policyYouOnly")
		}
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
func settingsRow(p SpecPalette, ed *Editor, testID, label, help string, control core.Node, policy settingsAgentPolicy, errNode core.Node) core.Node {
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
							Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary},
						},
						core.Text{
							Content: help,
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
					},
				},
				core.View{
					Style:    core.Style{FlexShrink: 0},
					Children: []core.Node{control},
				},
				settingsPolicyBadge(p, ed, policy),
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
			BorderColor: p.Line,
		},
		Children: children,
	}
}

// settingsSpecButton builds a real accessible ui.Button styled strictly with SpecPalette tokens and focus ring.
func settingsSpecButton(p SpecPalette, testID, text, label string, primary bool, disabled func() bool, onPress func()) core.Node {
	bg := p.Raised
	fg := p.TextSecondary
	var borderW core.Points = 1
	borderColor := p.LineRaised
	if primary {
		bg = p.Accent.Fg
		fg = p.Accent.OnFill
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
			FocusVisible: p.FocusRing(2),
		},
		OnPress: onPress,
	}
}

// editorSettingsModelsSection renders the "Models and connections" summary inside EditorSettingsDialogView
// matching docs/design/editor/Settings.dc.html, listing connected agents and "+ Connect another agent or model".
func editorSettingsModelsSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := SpecPaletteFor(darkMemo.Get())
		return core.View{
			TestID: "editor-settings-models",
			Style: core.Style{
				Padding: core.Edges{Top: 12},
				Gap:     8,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("settings.modelsTitle"),
					Style: core.Style{
						FontSize:   13,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       10,
					},
					Children: []core.Node{
						core.View{
							TestID: "editor-settings-connected-agent",
							Style: core.Style{
								FlexGrow:     1,
								FlexBasis:    core.Pt(0),
								CornerRadius: 10,
								Padding:      core.All(12),
								Gap:          4,
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Direction:  core.Row,
										AlignItems: core.AlignCenter,
										Gap:        8,
									},
									Children: []core.Node{
										core.View{
											StyleFunc: func() core.Style {
												_ = rev.Get()
												col := p.TextFaint
												if ed.AgentStatus() == AgentStatusConnected || ed.AgentStatus() == AgentStatusWorking {
													col = p.Added.Fg
												} else if ed.AgentStatus() == AgentStatusDisconnected {
													col = p.Removed.Fg
												}
												return core.Style{
													Width:        core.Pt(8),
													Height:       core.Pt(8),
													CornerRadius: 4,
													Background:   col,
												}
											},
										},
										core.Text{
											ContentFunc: func() string {
												_ = rev.Get()
												if ed.AgentMode() == AgentModeNone {
													return "No agent"
												}
												if c, ok := ed.AgentPrimaryClient(); ok {
													return c.DisplayName
												}
												return "Local MCP agent"
											},
											Style: core.Style{
												FontSize:   12.5,
												FontWeight: 500,
												Color:      p.TextPrimary,
											},
										},
									},
								},
								core.Text{
									ContentFunc: func() string {
										_ = rev.Get()
										if ed.AgentMode() == AgentModeNone {
											return "Agent panel stays closed"
										}
										if len(ed.AgentClients()) > 0 {
											return "On this computer · MCP · default"
										}
										return "Not connected yet"
									},
									Style: core.Style{
										FontSize: 11.5,
										Color:    p.TextFaint,
									},
								},
							},
						},
						core.View{
							TestID: "editor-settings-connect-agent",
							Role:   core.RoleButton,
							Label:  ed.T("settings.connectAnother"),
							Style: core.Style{
								FlexGrow:       1,
								FlexBasis:      core.Pt(0),
								CornerRadius:   10,
								Padding:        core.All(12),
								BorderWidth:    1,
								BorderColor:    p.LineRaised,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.CloseSettings()
								ed.OpenConnectDialog()
							},
							Children: []core.Node{
								core.Text{
									Content: ed.T("settings.connectAnother"),
									Style: core.Style{
										FontSize: 12,
										Color:    p.TextMuted,
									},
								},
							},
						},
					},
				},
			},
		}
	})
}
