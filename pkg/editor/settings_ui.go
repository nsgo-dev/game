package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
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
// and Light appearances. It hosts AGENT (Models and connections, Permissions, Game brief,
// Project rules, Tools, Automations, Usage and privacy), PROJECT · game.yaml (Project, Input map,
// Audio buses, Export), and EDITOR · THIS COMPUTER (General, Viewport, Shortcuts), keeping every
// control reachable with Tab, arrow keys, and focus rings.
func SettingsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type settingsViewKey struct {
		open   bool
		dark   bool
		uiSize InterfaceSize
	}
	key := core.NewMemo(cx, func() settingsViewKey {
		_ = rev.Get()
		return settingsViewKey{
			open:   ed.SettingsOpen() && !ed.Playing(),
			dark:   cx.Dark(),
			uiSize: ed.InterfaceSize(),
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
// EditorView mounts SettingsView instead, which hosts the General, Viewport, and Shortcuts pages alongside the Agent and Project pages.
func EditorSettingsDialogView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return SettingsView(cx, ed, rev)
}

func settingsScaledPt(base, scale float32) core.Points {
	return core.Points(math.Round(float64(base * scale)))
}

func settingsScaledLen(base, scale float32) core.Length {
	return core.Pt(settingsScaledPt(base, scale))
}

func settingsScaledFont(base, scale float32) core.Points {
	return core.Points(math.Round(float64(base*scale*2)) / 2)
}

// settingsTextMatches reports whether every whitespace-separated token in query appears
// case-insensitively in the combined searchable text fields.
func settingsTextMatches(query string, fields ...string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	var b strings.Builder
	for _, f := range fields {
		if f == "" {
			continue
		}
		b.WriteString(strings.ToLower(f))
		b.WriteByte(' ')
	}
	haystack := b.String()
	for _, tok := range strings.Fields(q) {
		if !strings.Contains(haystack, tok) {
			return false
		}
	}
	return true
}

// settingsPageMatchesQuery reports whether page (or any setting row on it) matches query.
func settingsPageMatchesQuery(ed *Editor, page SettingsPage, query string, targetOS core.OS, hasProject bool) bool {
	if page.IsProjectPage() && !hasProject && ed.SettingsPage() != page {
		return false
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return true
	}
	switch page {
	case SettingsPageGeneral:
		if settingsTextMatches(q, ed.T("settings.page.general"), ed.T("settings.page.generalSubtitle"), "general 通用 this computer") {
			return true
		}
		return settingsGeneralLanguageMatches(ed, q) ||
			settingsGeneralAppearanceMatches(ed, q) ||
			settingsGeneralAutoSaveMatches(ed, q) ||
			settingsGeneralInterfaceSizeMatches(ed, q)
	case SettingsPageViewport:
		if settingsTextMatches(q, ed.T("settings.page.viewport"), ed.T("settings.page.viewportSubtitle"), "viewport 视口 this computer") {
			return true
		}
		if settingsViewportSnapMatches(ed, q) ||
			settingsViewportGridMatches(ed, q) ||
			settingsViewportAgentChangesMatches(ed, q) ||
			settingsViewportNav3DMatches(ed, q) {
			return true
		}
		for _, o := range ed.ViewportOverlays() {
			if settingsViewportOverlayMatches(ed, o, q) {
				return true
			}
		}
		return false
	case SettingsPageShortcuts:
		if settingsTextMatches(q, ed.T("settings.shortcuts"), ed.T("settings.shortcutsHelp"), "shortcuts keymap hotkey rebind 快捷键") {
			return true
		}
		return len(ed.FilterShortcutCommands(q, targetOS)) > 0
	case SettingsPageModels:
		return settingsTextMatches(q,
			ed.T("settings.page.models"), ed.T("settings.page.modelsSubtitle"),
			ed.T("settings.models.whoDoesWhat"), ed.T("settings.models.apiKeyLabel"),
			ed.T("settings.connectAnother"),
			"models connections anthropic claude codex mcp keychain api key routing 模型 连接 密钥 智能体",
		)
	case SettingsPageTools:
		return settingsTextMatches(q,
			ed.T("settings.page.tools"), ed.T("settings.page.toolsSubtitle"),
			ed.T("settings.tools.addButton"),
			"tools mcp server delete_file running game generator 工具 服务器",
		)
	case SettingsPageAutomations:
		return settingsTextMatches(q,
			ed.T("settings.page.automations"), ed.T("settings.page.automationsSubtitle"),
			ed.T("settings.scopeAutomations"),
			"automations.yaml automation trigger nightly daily schedule pause checkpoint 自动化 触发 定时",
		)
	case SettingsPageUsage:
		return settingsTextMatches(q,
			ed.T("settings.page.usage"), ed.T("settings.page.usageSubtitle"),
			ed.T("settings.usage.limitLabel"), ed.T("settings.privacy.heading"),
			ed.T("settings.privacy.sceneAndData"), ed.T("settings.privacy.viewportImages"),
			ed.T("settings.privacy.goSource"), ed.T("settings.privacy.artAndAudio"),
			"usage privacy limit quota weekly kept playtest outbound go source 用量 隐私 限额",
		)
	case SettingsPageBrief:
		return settingsTextMatches(q,
			ed.T("settings.brief"), ed.T("settings.briefHelp"),
			ed.T("settings.briefPitch"), ed.T("settings.briefGenre"), ed.T("settings.briefTags"),
			ed.T("settings.briefPlatforms"), ed.T("settings.briefArtDirection"),
			ed.T("settings.briefFeel"), ed.T("settings.briefToneAudio"),
			"brief.yaml pitch genre platforms art direction feel difficulty tone audio 简报",
		)
	case SettingsPageRules:
		return settingsTextMatches(q,
			ed.T("settings.rules"), ed.T("settings.rulesHelp"),
			"agents.md project rules markdown 规则",
		)
	case SettingsPagePermissions:
		return settingsTextMatches(q,
			ed.T("permissions.title"), ed.T("permissions.subtitle"),
			"permissions.yaml cautious balanced autonomous allow ask deny mcp agent 权限",
		)
	case SettingsPageProject:
		return settingsTextMatches(q,
			ed.T("settings.page.project"), ed.T("settings.page.projectSubtitle"),
			"project game.yaml name id window size display physics autoload main scene plugins version 项目 窗口 场景 物理 插件",
		)
	case SettingsPageInput:
		return settingsTextMatches(q,
			ed.T("settings.page.input"), ed.T("settings.page.inputSubtitle"),
			"input map actions keyboard gamepad touch mouse regions axes 输入 按键 手柄 触屏",
		)
	case SettingsPageAudio:
		return settingsTextMatches(q,
			ed.T("settings.page.audio"), ed.T("settings.page.audioSubtitle"),
			"audio buses master music effects voice mixer dsp audition 音频 总线 混音",
		)
	case SettingsPageExport:
		return settingsTextMatches(q,
			ed.T("settings.page.export"), ed.T("settings.page.exportSubtitle"),
			"export presets build release macos windows linux web ios android credits 导出 构建 发布",
		)
	default:
		return false
	}
}

func settingsGeneralLanguageMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.language"), ed.T("settings.languageHelp"), "language english chinese locale 语言 中文 英文")
}

func settingsGeneralAppearanceMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.appearance"), ed.T("settings.appearanceHelp"), "appearance dark light system theme 外观 深色 浅色 主题")
}

func settingsGeneralAutoSaveMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.autoSave"), ed.T("settings.autoSaveHelp"), "save automatically autosave kept changes 2s 自动保存")
}

func settingsGeneralInterfaceSizeMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.interfaceSize"), ed.T("settings.interfaceSizeHelp"), "interface size scale text controls compact default large 界面 尺寸 缩放")
}

func settingsViewportSnapMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.viewportSnap"), ed.T("settings.viewportSnapHelp"), "snap step px angle degrees rotation grid size hold ignore 吸附 步长 角度")
}

func settingsViewportGridMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.viewportGrid"), ed.T("settings.viewportGridHelp"), "grid dots snap step overlay 网格 点阵")
}

func settingsViewportAgentChangesMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.viewportAgentChanges"), ed.T("settings.viewportAgentChangesHelp"), "show agent changes outlines review 审阅 轮廓 智能体更改")
}

func settingsViewportNav3DMatches(ed *Editor, q string) bool {
	return settingsTextMatches(q, ed.T("settings.viewportNav3D"), ed.T("settings.viewportNav3DHelp"), "3d navigation mouse mapping default blender maya orbit pan fly 3D 导航 鼠标")
}

func settingsViewportOverlayMatches(ed *Editor, o ViewportOverlay, q string) bool {
	return settingsTextMatches(q, ed.overlayDisplayName(o), o.ID, ed.T("settings.viewportOverlaysTitle"), ed.T("settings.viewportOverlaysHelp"), "overlay legend 叠加层 图例")
}

func formatSettingsStep(v float32) string {
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}

func settingsShellView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.SpecPalette(cx)
	scale := ed.InterfaceScale()
	accentTint := ui.Alpha(p.Accent.Fg, 0.14)
	accentEdge := ui.Alpha(p.Accent.Fg, 0.45)
	activeNav := ui.Alpha(p.Accent.Fg, 0.16)
	focusRing := p.FocusRing(2)

	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
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

	allNavPages := []SettingsPage{
		SettingsPageModels,
		SettingsPagePermissions,
		SettingsPageBrief,
		SettingsPageRules,
		SettingsPageTools,
		SettingsPageAutomations,
		SettingsPageUsage,
		SettingsPageProject,
		SettingsPageInput,
		SettingsPageAudio,
		SettingsPageExport,
		SettingsPageGeneral,
		SettingsPageViewport,
		SettingsPageShortcuts,
	}
	navRefs := make(map[SettingsPage]*core.Ref, len(allNavPages))
	for _, pg := range allNavPages {
		navRefs[pg] = &core.Ref{}
	}

	pageMatches := func(pg SettingsPage) bool {
		_ = langMemo.Get()
		return settingsPageMatchesQuery(ed, pg, searchQuery.Get(), cx.Env().OS(), hasProject.Get())
	}

	visibleNavPages := func(enabledOnly bool) []SettingsPage {
		projOpen := hasProject.Get()
		q := searchQuery.Get()
		targetOS := cx.Env().OS()
		out := make([]SettingsPage, 0, len(allNavPages))
		for _, pg := range allNavPages {
			if !settingsPageMatchesQuery(ed, pg, q, targetOS, projOpen) {
				continue
			}
			if enabledOnly && pg.IsProjectPage() && !projOpen {
				continue
			}
			out = append(out, pg)
		}
		return out
	}

	var searchOriginPage SettingsPage
	// When typing in search narrows away the currently active page, switch to the first matching page so matching rows show immediately.
	// Remember the page that was active before search started so typing an unmatched natural-language prompt (whose intermediate prefix
	// briefly matched another page) restores the original page context before dispatching to the agent.
	cx.Effect(func() {
		q := strings.TrimSpace(searchQuery.Get())
		_ = langMemo.Get()
		projOpen := hasProject.Get()
		if q == "" {
			searchOriginPage = ""
			return
		}
		cur := ed.SettingsPage()
		if searchOriginPage == "" {
			searchOriginPage = cur
		}
		matches := visibleNavPages(true)
		if len(matches) == 0 {
			if searchOriginPage != "" && cur != searchOriginPage {
				cx.Untrack(func() {
					ed.OpenSettingsPage(searchOriginPage)
				})
			}
			return
		}
		if searchOriginPage != "" && settingsPageMatchesQuery(ed, searchOriginPage, q, cx.Env().OS(), projOpen) {
			if cur != searchOriginPage {
				cx.Untrack(func() {
					ed.OpenSettingsPage(searchOriginPage)
				})
			}
			return
		}
		if settingsPageMatchesQuery(ed, cur, q, cx.Env().OS(), projOpen) {
			return
		}
		cx.Untrack(func() {
			ed.OpenSettingsPage(matches[0])
		})
	})

	// Arrow-key navigation inside the left navigation list (Down/Up/Home/End).
	moveSidebarNav := func(delta, edge int) {
		if ed.shortcutCaptureActive {
			return
		}
		visible := visibleNavPages(true)
		if len(visible) == 0 {
			return
		}
		curIdx := -1
		for i, pg := range visible {
			if ref := navRefs[pg]; ref != nil && ref.HoldsFocus() {
				curIdx = i
				break
			}
		}
		if curIdx < 0 {
			return
		}
		var nextIdx int
		switch edge {
		case -1:
			nextIdx = 0
		case 1:
			nextIdx = len(visible) - 1
		default:
			nextIdx = (curIdx + delta + len(visible)) % len(visible)
		}
		target := visible[nextIdx]
		searchOriginPage = target
		ed.OpenSettingsPage(target)
		if ref := navRefs[target]; ref != nil {
			ref.Focus()
		}
	}
	cx.OnKeyWith("down", core.KeyOptions{When: core.KeyNotEditing}, func() { moveSidebarNav(1, 0) })
	cx.OnKeyWith("up", core.KeyOptions{When: core.KeyNotEditing}, func() { moveSidebarNav(-1, 0) })
	cx.OnKeyWith("home", core.KeyOptions{When: core.KeyNotEditing}, func() { moveSidebarNav(0, -1) })
	cx.OnKeyWith("end", core.KeyOptions{When: core.KeyNotEditing}, func() { moveSidebarNav(0, 1) })

	navRowHeight := settingsScaledLen(32, scale)
	navFontSize := settingsScaledFont(12.5, scale)
	metaFontSize := settingsScaledFont(11, scale)

	navRowWithIDs := func(page SettingsPage, btnTestID, metaTestID, labelKey, fallbackLabel string, metaFunc func() (string, bool), disabled func() bool) core.Node {
		if btnTestID == "" {
			btnTestID = "settings-nav-" + string(page)
		}
		if metaTestID == "" {
			metaTestID = "settings-nav-" + string(page) + "-meta"
		}
		return core.Show{
			When: func() bool {
				return pageMatches(page)
			},
			Then: func(cx *core.Scope) core.Node {
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
							Height:       navRowHeight,
							CornerRadius: 8,
							Background:   bg,
							Gap:          4,
						}
					},
					Children: []core.Node{
						ui.Button{
							Ref:      navRefs[page],
							TestID:   btnTestID,
							Variant:  ui.ButtonGhost,
							Size:     ui.SizeSmall,
							Flex:     1,
							Disabled: disabled,
							TextFunc: func() string {
								_ = langMemo.Get()
								label := fallbackLabel
								if labelKey != "" {
									label = ed.T(labelKey)
								}
								if page == SettingsPageShortcuts {
									_ = rev.Get()
									if n := len(ed.Settings().Shortcuts); n > 0 {
										return fmt.Sprintf("%s · %d", label, n)
									}
								}
								return label
							},
							LabelFunc: func() string {
								_ = langMemo.Get()
								lbl := fallbackLabel
								if labelKey != "" {
									lbl = ed.T(labelKey)
								}
								if page == SettingsPageShortcuts {
									_ = rev.Get()
									if n := len(ed.Settings().Shortcuts); n > 0 {
										return fmt.Sprintf("%s · %d", lbl, n)
									}
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
								FontSize:   navFontSize,
								FontWeight: 500,
								Color:      p.TextSecondary,
								FlexGrow:   1,
								TextAlign:  core.TextAlignLeft,
							},
							Style: core.Style{
								Height:       navRowHeight,
								CornerRadius: 8,
								Padding:      core.Symmetric(0, 8),
								FocusVisible: focusRing,
							},
							OnPress: func() {
								searchOriginPage = page
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
									TestID: metaTestID,
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
											FontSize:   metaFontSize,
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
			},
		}
	}

	navRow := func(page SettingsPage, labelKey, fallbackLabel string, metaFunc func() (string, bool), disabled func() bool) core.Node {
		return navRowWithIDs(page, "", "", labelKey, fallbackLabel, metaFunc, disabled)
	}

	noProject := func() bool {
		return !hasProject.Get()
	}

	anyMatches := func(pages ...SettingsPage) bool {
		for _, pg := range pages {
			if pageMatches(pg) {
				return true
			}
		}
		return false
	}

	sidebar := core.View{
		TestID: "settings-sidebar",
		Role:   core.RoleNavigation,
		Label:  "Settings pages",
		Style: core.Style{
			Width:       settingsScaledLen(248, scale),
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
							FontSize:   settingsScaledFont(13, scale),
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
									FontSize:   settingsScaledFont(12, scale),
									Color:      p.TextFaint,
									FlexShrink: 1,
								},
							}
						},
					},
				},
			},
			// AGENT group (matching docs/design/editor/Settings.dc.html) — first group.
			core.Show{
				When: func() bool {
					return anyMatches(SettingsPageModels, SettingsPagePermissions, SettingsPageBrief, SettingsPageRules, SettingsPageTools, SettingsPageAutomations, SettingsPageUsage)
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "settings-nav-group-agent",
						Style:  core.Style{Gap: 4},
						Children: []core.Node{
							settingsSectionLabelDynamic(p, func() string {
								_ = langMemo.Get()
								return ed.T("settings.sectionAgent")
							}),
							navRow(SettingsPageModels, "settings.page.models", "Models and connections", func() (string, bool) {
								_ = rev.Get()
								_ = langMemo.Get()
								return ed.T("settings.models.metaOn", l10n.Param("n", ed.AgentActiveConnectionCount())), false
							}, nil),
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
							navRowWithIDs(SettingsPageBrief, "editor-settings-tab-brief", "editor-settings-tab-brief-meta", "settings.brief", "Game brief", func() (string, bool) {
								_ = rev.Get()
								_ = langMemo.Get()
								if !ed.BriefExists() {
									return ed.T("settings.briefEmptyBadge"), true
								}
								gaps := len(ed.BriefGaps())
								if gaps == 0 {
									return "", false
								}
								if gaps == 1 {
									return ed.T("settings.briefOneGap"), true
								}
								return ed.T("settings.briefManyGaps", l10n.Param("count", gaps)), true
							}, nil),
							navRowWithIDs(SettingsPageRules, "editor-settings-tab-rules", "editor-settings-tab-rules-meta", "settings.rules", "Project rules", func() (string, bool) {
								_ = rev.Get()
								if n := ed.ProjectRulesCount(); n > 0 {
									return fmt.Sprintf("%d", n), false
								}
								return "", false
							}, nil),
							navRow(SettingsPageTools, "settings.page.tools", "Tools", func() (string, bool) {
								_ = rev.Get()
								en, total := ed.EnabledMCPToolCount()
								return fmt.Sprintf("%d / %d", en, total), false
							}, nil),
							navRow(SettingsPageAutomations, "settings.page.automations", "Automations", func() (string, bool) {
								_ = rev.Get()
								_ = langMemo.Get()
								if ed.AutomationsPausedToday() {
									return ed.T("settings.automations.metaPaused"), true
								}
								if n := ed.EnabledAutomationsCount(); n > 0 {
									return ed.T("settings.automations.metaOn", l10n.Param("count", fmt.Sprintf("%d", n))), false
								}
								return "", false
							}, nil),
							navRow(SettingsPageUsage, "settings.page.usage", "Usage and privacy", nil, nil),
						},
					}
				},
			},
			// PROJECT · game.yaml group — hidden when no project is open or when search matches none of its pages.
			core.Show{
				When: func() bool {
					return (hasProject.Get() || activePage.Get().IsProjectPage()) &&
						anyMatches(SettingsPageProject, SettingsPageInput, SettingsPageAudio, SettingsPageExport)
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "settings-nav-group-project",
						Style:  core.Style{Gap: 4, Padding: core.Edges{Top: 12}},
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
			// EDITOR · THIS COMPUTER group
			core.Show{
				When: func() bool {
					return anyMatches(SettingsPageGeneral, SettingsPageViewport, SettingsPageShortcuts)
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "settings-nav-group-editor",
						Style:  core.Style{Gap: 4, Padding: core.Edges{Top: 12}},
						Children: []core.Node{
							settingsSectionLabelDynamic(p, func() string {
								_ = langMemo.Get()
								return ed.T("settings.sectionEditor")
							}),
							navRow(SettingsPageGeneral, "settings.page.general", "General", func() (string, bool) {
								_ = rev.Get()
								_ = langMemo.Get()
								switch ed.Appearance() {
								case AppearanceDark:
									return ed.T("settings.dark"), false
								case AppearanceLight:
									return ed.T("settings.light"), false
								default:
									return ed.T("settings.system"), false
								}
							}, nil),
							navRow(SettingsPageViewport, "settings.page.viewport", "Viewport", func() (string, bool) {
								_ = rev.Get()
								step := ed.EffectiveGridSize()
								deg := ed.EffectiveRotationSnapDeg()
								return fmt.Sprintf("%s px · %s°", formatSettingsStep(step), formatSettingsStep(deg)), false
							}, nil),
							navRowWithIDs(SettingsPageShortcuts, "editor-settings-tab-shortcuts", "settings-nav-shortcuts-meta", "settings.shortcuts", "Shortcuts", nil, nil),
						},
					}
				},
			},
			core.Show{
				When: func() bool {
					return strings.TrimSpace(searchQuery.Get()) != "" && len(visibleNavPages(false)) == 0
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID: "settings-nav-empty",
						ContentFunc: func() string {
							_ = langMemo.Get()
							return ed.T("settings.navEmpty")
						},
						Style: core.Style{
							FontSize: settingsScaledFont(12, scale),
							Color:    p.TextFaint,
							Padding:  core.Symmetric(8, 8),
						},
					}
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
					FontSize:   navFontSize,
					FontWeight: 500,
					Color:      p.TextSecondary,
				},
				Style: core.Style{
					Width:        core.Pct(100),
					Height:       navRowHeight,
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
		case SettingsPageModels:
			return ed.T("settings.page.models"), ed.T("settings.page.modelsSubtitle"), ed.T("settings.scopeComputer"), false
		case SettingsPageTools:
			return ed.T("settings.page.tools"), ed.T("settings.page.toolsSubtitle"), ed.T("settings.scopeComputer"), false
		case SettingsPageUsage:
			return ed.T("settings.page.usage"), ed.T("settings.page.usageSubtitle"), ed.T("settings.scopeComputer"), false
		case SettingsPageAutomations:
			return ed.T("settings.page.automations"), ed.T("settings.page.automationsSubtitle"), ed.T("settings.scopeAutomations"), true
		case SettingsPageProject:
			return ed.T("settings.page.project"), ed.T("settings.page.projectSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPageInput:
			return ed.T("settings.page.input"), ed.T("settings.page.inputSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPageAudio:
			return ed.T("settings.page.audio"), ed.T("settings.page.audioSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPageExport:
			return ed.T("settings.page.export"), ed.T("settings.page.exportSubtitle"), ed.T("settings.scopeProject"), true
		case SettingsPagePermissions:
			return ed.T("permissions.title"), ed.T("permissions.subtitle"), ed.T("settings.scopeShared"), true
		case SettingsPageBrief:
			return ed.T("settings.brief"), ed.T("settings.briefHelp"), ed.T("settings.scopeBrief"), true
		case SettingsPageRules:
			return ed.T("settings.rules"), ed.T("settings.rulesHelp"), ed.T("settings.scopeRules"), true
		case SettingsPageViewport:
			return ed.T("settings.page.viewport"), ed.T("settings.page.viewportSubtitle"), ed.T("settings.scopeComputer"), false
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
					specInputField(cx, ed, ui.Input{
						TestID:      "settings-search",
						Label:       ed.T("settings.searchLabel"),
						Placeholder: ed.T("settings.searchPlaceholder"),
						Value:       searchQuery.Get,
						OnChange:    searchQuery.Set,
						OnSubmit: func(q string) {
							if matches := visibleNavPages(true); len(matches) > 0 {
								ed.OpenSettingsPage(matches[0])
								return
							}
							if target, ok := matchSettingsPageQuery(q, ed.Project != nil); ok {
								ed.OpenSettingsPage(target)
								return
							}
							if trimmed := strings.TrimSpace(q); trimmed != "" && ed.AgentMode() != AgentModeNone {
								ed.AskAgentFromSettings("search", trimmed)
								searchQuery.Set("")
							}
						},
					}),
				},
			},
			core.Show{
				When: func() bool {
					return strings.TrimSpace(searchQuery.Get()) != ""
				},
				Then: func(cx *core.Scope) core.Node {
					askLabel := func() string {
						_ = langMemo.Get()
						return ed.T("settings.searchAskAgent", l10n.Param("query", strings.TrimSpace(searchQuery.Get())))
					}
					return core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.AgentMode() != AgentModeNone
								},
								Then: func(cx *core.Scope) core.Node {
									return ui.Button{
										TestID:    "settings-search-ask",
										TextFunc:  askLabel,
										LabelFunc: askLabel,
										Variant:   ui.ButtonGhost,
										Size:      ui.SizeSmall,
										Disabled: func() bool {
											_ = rev.Get()
											return ed.Playing() || ed.MergeConflictOpen()
										},
										Color: func() core.Color { return p.Accent.Fg },
										TextStyle: core.Style{
											FontSize:   settingsScaledFont(12, scale),
											FontWeight: 600,
											Color:      p.Accent.OnFill,
										},
										Style: core.Style{
											Height:       settingsScaledLen(28, scale),
											CornerRadius: 8,
											FlexShrink:   0,
											FocusVisible: p.FocusRing(2),
										},
										OnPress: func() {
											if trimmed := strings.TrimSpace(searchQuery.Get()); trimmed != "" {
												ed.AskAgentFromSettings("search", trimmed)
												searchQuery.Set("")
											}
										},
									}
								},
							},
							settingsSpecButton(p, "settings-search-clear", ed.T("settings.searchClear"), ed.T("settings.searchClear"), false, nil, func() {
								searchQuery.Set("")
							}),
						},
					}
				},
			},
		},
	}

	pageHeader := core.Show{
		When: func() bool {
			return strings.TrimSpace(searchQuery.Get()) == "" || len(visibleNavPages(false)) > 0
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
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
									FontSize:   settingsScaledFont(18, scale),
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
									FontSize: settingsScaledFont(12.5, scale),
									Color:    p.TextFaint,
								},
							},
						},
					},
					core.Show{
						When: func() bool {
							pg := activePage.Get()
							return pg == SettingsPagePermissions || pg.IsAgentPage()
						},
						Then: func(*core.Scope) core.Node {
							_ = langMemo.Get()
							return specYouOnlyPill(p, "settings-page-you-only", ed.T("settings.policyYouOnly"))
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
								Height:         settingsScaledLen(24, scale),
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
										FontSize:   settingsScaledFont(11.5, scale),
										FontWeight: 500,
										Color:      fg,
									}
								},
							},
						},
					},
				},
			}
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
						FontSize:   navFontSize,
						FontWeight: 600,
						Color:      p.Accent.OnFill,
					},
					Style: core.Style{
						Height:       navRowHeight,
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

	// Sync global Settings search into shortcutsQuery when searching on Shortcuts so command rows narrow live too.
	cx.Effect(func() {
		q := searchQuery.Get()
		if activePage.Get() == SettingsPageShortcuts {
			shortcutsQuery.Set(q)
		}
	})

	type pageBodyKey struct {
		noMatch  bool
		page     SettingsPage
		projOpen bool
	}
	bodyKey := core.NewMemo(cx, func() pageBodyKey {
		q := strings.TrimSpace(searchQuery.Get())
		if q != "" && len(visibleNavPages(false)) == 0 {
			return pageBodyKey{noMatch: true}
		}
		return pageBodyKey{
			page:     activePage.Get(),
			projOpen: hasProject.Get(),
		}
	})

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
				bk := bodyKey.Get()
				if bk.noMatch {
					return settingsSearchNoMatchView(cx, ed, rev, p, scale, searchQuery, func() {
						searchQuery.Set("")
					})
				}
				page := bk.page
				if page.IsProjectPage() && !bk.projOpen {
					return settingsNoProjectView(p, ed)
				}
				switch page {
				case SettingsPageModels:
					_ = langMemo.Get()
					return settingsModelsPageView(cx, ed, rev, ed.SpecPalette(cx))
				case SettingsPageTools:
					_ = langMemo.Get()
					return settingsToolsPageView(cx, ed, rev, ed.SpecPalette(cx))
				case SettingsPageUsage:
					_ = langMemo.Get()
					return settingsUsagePageView(cx, ed, rev, ed.SpecPalette(cx))
				case SettingsPageAutomations:
					return automationsSettingsPageView(cx, ed, rev)
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
				case SettingsPageBrief:
					return core.View{
						TestID: "editor-settings-brief-wrap",
						Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Overflow: core.Hidden},
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 16, Overflow: core.Hidden},
								Children: []core.Node{
									briefSettingsPageView(cx, ed, rev),
									settingsAgentCardView(cx, ed, rev, EditorSettingsTabBrief, ed.T("settings.brief")),
								},
							},
							editorSettingsDoneFooter(),
						},
					}
				case SettingsPageRules:
					return core.View{
						TestID: "editor-settings-rules-wrap",
						Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Overflow: core.Hidden},
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 16, Overflow: core.Hidden},
								Children: []core.Node{
									rulesSettingsPageView(cx, ed, rev),
									settingsAgentCardView(cx, ed, rev, EditorSettingsTabRules, ed.T("settings.rules")),
								},
							},
							editorSettingsDoneFooter(),
						},
					}
				case SettingsPageViewport:
					return editorSettingsViewportPage(cx, ed, rev, p, scale, searchQuery, editorSettingsDoneFooter())
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
					return editorSettingsGeneralPage(cx, ed, rev, p, scale, searchQuery, editorSettingsDoneFooter())
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

func settingsSearchNoMatchView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, scale float32, searchQuery *core.Signal[string], onClear func()) core.Node {
	trimmedQuery := func() string {
		return strings.TrimSpace(searchQuery.Get())
	}
	sendLabel := func() string {
		return ed.T("settings.searchSendToAgent", l10n.Param("query", trimmedQuery()))
	}
	return core.View{
		TestID: "settings-search-no-match",
		Style: core.Style{
			FlexGrow:       1,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            12,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("settings.searchNoMatchTitle"),
				Style:   core.Style{FontSize: settingsScaledFont(14, scale), FontWeight: 600, Color: p.TextPrimary},
			},
			core.Text{
				ContentFunc: func() string {
					return ed.T("settings.searchNoMatchBody", l10n.Param("query", trimmedQuery()))
				},
				Style: core.Style{FontSize: settingsScaledFont(12.5, scale), Color: p.TextSecondary},
			},
			core.Text{
				Content: ed.T("settings.searchNoMatchHint"),
				Style:   core.Style{FontSize: settingsScaledFont(12, scale), Color: p.TextFaint},
			},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Edges{Top: 4}},
				Children: []core.Node{
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.AgentMode() != AgentModeNone
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:    "settings-search-send-agent",
								TextFunc:  sendLabel,
								LabelFunc: sendLabel,
								Variant:   ui.ButtonGhost,
								Size:      ui.SizeSmall,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Playing() || ed.MergeConflictOpen()
								},
								Color: func() core.Color { return p.Accent.Fg },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      p.Accent.OnFill,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 8,
									FlexShrink:   0,
									FocusVisible: p.FocusRing(2),
								},
								OnPress: func() {
									if q := strings.TrimSpace(searchQuery.GetUntracked()); q != "" {
										ed.AskAgentFromSettings("search", q)
										onClear()
									}
								},
							}
						},
					},
					settingsSpecButton(p, "settings-search-no-match-clear", ed.T("settings.searchClear"), ed.T("settings.searchClear"), false, nil, onClear),
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
	if strings.Contains(q, "automation") || strings.Contains(q, "trigger") || strings.Contains(q, "nightly") || strings.Contains(q, "daily") || strings.Contains(q, "schedule") || strings.Contains(q, "自动化") || strings.Contains(q, "触发") || strings.Contains(q, "定时") {
		return SettingsPageAutomations, true
	}
	if strings.Contains(q, "shortcut") || strings.Contains(q, "keymap") || strings.Contains(q, "hotkey") || strings.Contains(q, "rebind") || strings.Contains(q, "快捷键") {
		return SettingsPageShortcuts, true
	}
	if strings.Contains(q, "viewport") || strings.Contains(q, "snap") || strings.Contains(q, "grid") || strings.Contains(q, "overlay") || strings.Contains(q, "3d nav") || strings.Contains(q, "mouse map") || strings.Contains(q, "视口") || strings.Contains(q, "吸附") || strings.Contains(q, "网格") || strings.Contains(q, "叠加层") {
		return SettingsPageViewport, true
	}
	if strings.Contains(q, "brief") || strings.Contains(q, "pitch") || strings.Contains(q, "简报") {
		return SettingsPageBrief, true
	}
	if strings.Contains(q, "agents.md") || strings.Contains(q, "project rule") || strings.Contains(q, "规则") {
		return SettingsPageRules, true
	}
	if strings.Contains(q, "permission") || strings.Contains(q, "cautious") || strings.Contains(q, "balanced") || strings.Contains(q, "autonomous") || strings.Contains(q, "权限") {
		return SettingsPagePermissions, true
	}
	if strings.Contains(q, "privacy") || strings.Contains(q, "usage") || strings.Contains(q, "limit") || strings.Contains(q, "quota") || strings.Contains(q, "weekly") || strings.Contains(q, "隐私") || strings.Contains(q, "用量") || strings.Contains(q, "限额") {
		return SettingsPageUsage, true
	}
	if strings.Contains(q, "tool") || strings.Contains(q, "mcp") || strings.Contains(q, "server") || strings.Contains(q, "delete_file") || strings.Contains(q, "工具") || strings.Contains(q, "服务器") {
		return SettingsPageTools, true
	}
	if strings.Contains(q, "model") || strings.Contains(q, "connect") || strings.Contains(q, "anthropic") || strings.Contains(q, "claude") || strings.Contains(q, "codex") || strings.Contains(q, "keychain") || strings.Contains(q, "api") || strings.Contains(q, "agent") || strings.Contains(q, "模型") || strings.Contains(q, "连接") || strings.Contains(q, "智能体") {
		return SettingsPageModels, true
	}
	if strings.Contains(q, "rule") {
		return SettingsPageRules, true
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
	if strings.Contains(q, "lang") || strings.Contains(q, "appear") || strings.Contains(q, "dark") || strings.Contains(q, "light") || strings.Contains(q, "general") || strings.Contains(q, "save") || strings.Contains(q, "autosave") || strings.Contains(q, "interface") || strings.Contains(q, "size") || strings.Contains(q, "语言") || strings.Contains(q, "外观") || strings.Contains(q, "深色") || strings.Contains(q, "浅色") || strings.Contains(q, "通用") || strings.Contains(q, "保存") || strings.Contains(q, "界面") {
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

func editorSettingsGeneralPage(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, scale float32, searchQuery *core.Signal[string], footer core.Node) core.Node {
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})

	return core.View{
		TestID: "editor-settings-dialog",
		Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 8, Overflow: core.Hidden},
		Children: []core.Node{
			core.Scroll{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = langMemo.Get()
						q := strings.TrimSpace(searchQuery.Get())
						curPal := ed.SpecPalette(cx)

						// When the query matches the page title/subtitle directly (e.g. "general") without
						// narrowing to a single row, show every row on the page.
						narrowRows := q != "" &&
							(settingsGeneralLanguageMatches(ed, q) ||
								settingsGeneralAppearanceMatches(ed, q) ||
								settingsGeneralAutoSaveMatches(ed, q) ||
								settingsGeneralInterfaceSizeMatches(ed, q))

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

						sizeToggle := func(testID, text string, sz InterfaceSize) core.Node {
							return ui.ToggleButton{
								TestID: testID,
								Text:   text,
								On: func() bool {
									_ = rev.Get()
									return ed.InterfaceSize() == sz
								},
								OnChange: func(on bool) {
									if on {
										ed.report(ed.SetInterfaceSize(sz))
									}
								},
							}
						}

						rows := make([]core.Node, 0, 6)
						if !narrowRows || settingsGeneralLanguageMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
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
							))
						}
						if !narrowRows || settingsGeneralAppearanceMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
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
							))
						}
						if !narrowRows || settingsGeneralAutoSaveMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
								"editor-settings-autosave",
								ed.T("settings.autoSave"),
								ed.T("settings.autoSaveHelp"),
								SpecSwitch{
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
							))
						}
						if !narrowRows || settingsGeneralInterfaceSizeMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
								"editor-settings-interface-size",
								ed.T("settings.interfaceSize"),
								ed.T("settings.interfaceSizeHelp"),
								ui.ToggleGroup{
									TestID: "editor-settings-size-group",
									Label:  ed.T("settings.interfaceSize"),
									Children: []core.Node{
										sizeToggle("editor-settings-size-compact", ed.T("settings.interfaceSizeCompact"), InterfaceSizeCompact),
										sizeToggle("editor-settings-size-default", ed.T("settings.interfaceSizeDefault"), InterfaceSizeDefault),
										sizeToggle("editor-settings-size-large", ed.T("settings.interfaceSizeLarge"), InterfaceSizeLarge),
									},
								},
								policyYouOnly,
								nil,
							))
						}
						return core.View{
							Style:    core.Style{Gap: 4},
							Children: rows,
						}
					}),
				},
			},
			footer,
		},
	}
}

func editorSettingsViewportPage(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, scale float32, searchQuery *core.Signal[string], footer core.Node) core.Node {
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})

	stepDraft := core.NewSignal(cx, formatSettingsStep(ed.EffectiveGridSize()))
	angleDraft := core.NewSignal(cx, formatSettingsStep(ed.EffectiveRotationSnapDeg()))
	snapErr := core.NewSignal(cx, "")
	stepRef := &core.Ref{}
	angleRef := &core.Ref{}

	// Keep snap draft inputs synchronized with live editor snap values when changed externally,
	// without clobbering an in-progress edit while either numeric field holds keyboard focus.
	cx.Effect(func() {
		_ = rev.Get()
		if snapErr.GetUntracked() != "" {
			return
		}
		if !stepRef.HoldsFocus() {
			wantStep := formatSettingsStep(ed.EffectiveGridSize())
			if stepDraft.GetUntracked() != wantStep {
				stepDraft.Set(wantStep)
			}
		}
		if !angleRef.HoldsFocus() {
			wantAngle := formatSettingsStep(ed.EffectiveRotationSnapDeg())
			if angleDraft.GetUntracked() != wantAngle {
				angleDraft.Set(wantAngle)
			}
		}
	})

	applySnapStep := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		v, err := strconv.ParseFloat(trimmed, 32)
		if err != nil || !isFiniteFloat32(float32(v)) || v <= 0 {
			snapErr.Set(ed.T("settings.viewportSnapError"))
			return
		}
		snapErr.Set("")
		ed.SetGridSize(float32(v))
		stepDraft.Set(formatSettingsStep(float32(v)))
	}

	applySnapAngle := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		v, err := strconv.ParseFloat(trimmed, 32)
		if err != nil || !isFiniteFloat32(float32(v)) || v <= 0 {
			snapErr.Set(ed.T("settings.viewportSnapError"))
			return
		}
		snapErr.Set("")
		ed.SetRotationSnapDeg(float32(v))
		angleDraft.Set(formatSettingsStep(float32(v)))
	}

	return core.View{
		TestID: "editor-settings-viewport",
		Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 8, Overflow: core.Hidden},
		Children: []core.Node{
			core.Scroll{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = langMemo.Get()
						q := strings.TrimSpace(searchQuery.Get())
						curPal := ed.SpecPalette(cx)
						overlays := ed.ViewportOverlays()

						anyOverlayMatches := false
						for _, o := range overlays {
							if settingsViewportOverlayMatches(ed, o, q) {
								anyOverlayMatches = true
								break
							}
						}
						narrowRows := q != "" &&
							(settingsViewportSnapMatches(ed, q) ||
								settingsViewportGridMatches(ed, q) ||
								settingsViewportAgentChangesMatches(ed, q) ||
								settingsViewportNav3DMatches(ed, q) ||
								anyOverlayMatches)

						nav3DToggle := func(testID, text string, mode Navigation3D) core.Node {
							return ui.ToggleButton{
								TestID: testID,
								Text:   text,
								On: func() bool {
									_ = rev.Get()
									return ed.Navigation3D() == mode
								},
								OnChange: func(on bool) {
									if on {
										ed.report(ed.SetNavigation3D(mode))
									}
								},
							}
						}

						rows := make([]core.Node, 0, 6+len(overlays))
						if !narrowRows || settingsViewportSnapMatches(ed, q) {
							snapControls := core.View{
								Style: core.Style{
									Direction:  core.Row,
									AlignItems: core.AlignCenter,
									Gap:        8,
								},
								Children: []core.Node{
									SpecSwitch{
										TestID: "editor-settings-snap-switch",
										Label:  ed.T("settings.viewportSnap"),
										Size:   ui.SizeSmall,
										Checked: func() bool {
											_ = rev.Get()
											return ed.SnapToGrid || ed.SnapRotation
										},
										OnChange: func(on bool) {
											s := ed.SnapSettings()
											s.Grid = on
											s.Rotation = on
											ed.SetSnapSettings(s)
										},
									},
									core.View{
										Style: core.Style{Width: settingsScaledLen(64, scale)},
										Children: []core.Node{
											specInputField(cx, ed, ui.Input{
												Ref:    stepRef,
												TestID: "editor-settings-snap-step",
												Label:  ed.T("settings.viewportSnapStepLabel"),
												Value:  stepDraft.Get,
												OnChange: func(v string) {
													stepDraft.Set(v)
													snapErr.Set("")
												},
												OnSubmit: applySnapStep,
												OnBlur: func() {
													applySnapStep(stepDraft.GetUntracked())
												},
												Style: core.Style{
													Height:       settingsScaledLen(28, scale),
													Background:   curPal.Raised,
													BorderWidth:  1,
													BorderColor:  curPal.LineRaised,
													CornerRadius: 6,
													FontFamily:   core.FontMonospace,
													FontSize:     settingsScaledFont(12, scale),
													Color:        curPal.TextSecondary,
												},
											}),
										},
									},
									core.Text{
										Content: "px ·",
										Style:   core.Style{FontSize: settingsScaledFont(11.5, scale), Color: curPal.TextFaint},
									},
									core.View{
										Style: core.Style{Width: settingsScaledLen(56, scale)},
										Children: []core.Node{
											specInputField(cx, ed, ui.Input{
												Ref:    angleRef,
												TestID: "editor-settings-snap-angle",
												Label:  ed.T("settings.viewportSnapAngleLabel"),
												Value:  angleDraft.Get,
												OnChange: func(v string) {
													angleDraft.Set(v)
													snapErr.Set("")
												},
												OnSubmit: applySnapAngle,
												OnBlur: func() {
													applySnapAngle(angleDraft.GetUntracked())
												},
												Style: core.Style{
													Height:       settingsScaledLen(28, scale),
													Background:   curPal.Raised,
													BorderWidth:  1,
													BorderColor:  curPal.LineRaised,
													CornerRadius: 6,
													FontFamily:   core.FontMonospace,
													FontSize:     settingsScaledFont(12, scale),
													Color:        curPal.TextSecondary,
												},
											}),
										},
									},
									core.Text{
										Content: "°",
										Style:   core.Style{FontSize: settingsScaledFont(11.5, scale), Color: curPal.TextFaint},
									},
								},
							}
							errNode := core.Show{
								When: func() bool { return snapErr.Get() != "" },
								Then: func(cx *core.Scope) core.Node {
									return core.Text{
										TestID:      "editor-settings-snap-error",
										ContentFunc: snapErr.Get,
										Style:       core.Style{FontSize: settingsScaledFont(11.5, scale), Color: curPal.Removed.Fg},
									}
								},
							}
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
								"editor-settings-snap",
								ed.T("settings.viewportSnap"),
								ed.T("settings.viewportSnapHelp"),
								snapControls,
								policyYouOnly,
								errNode,
							))
						}
						if !narrowRows || settingsViewportGridMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
								"editor-settings-grid",
								ed.T("settings.viewportGrid"),
								ed.T("settings.viewportGridHelp"),
								SpecSwitch{
									TestID: "editor-settings-grid-switch",
									Label:  ed.T("settings.viewportGrid"),
									Size:   ui.SizeSmall,
									Checked: func() bool {
										_ = rev.Get()
										return ed.ShowGrid
									},
									OnChange: func(on bool) {
										ed.SetShowGrid(on)
									},
								},
								policyYouOnly,
								nil,
							))
						}
						if !narrowRows || settingsViewportAgentChangesMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
								"editor-settings-agent-changes",
								ed.T("settings.viewportAgentChanges"),
								ed.T("settings.viewportAgentChangesHelp"),
								SpecSwitch{
									TestID: "editor-settings-agent-changes-switch",
									Label:  ed.T("settings.viewportAgentChanges"),
									Size:   ui.SizeSmall,
									Checked: func() bool {
										_ = rev.Get()
										return ed.ShowAgentChanges()
									},
									OnChange: func(on bool) {
										ed.report(ed.SetShowAgentChanges(on))
									},
								},
								policyYouOnly,
								nil,
							))
						}
						if !narrowRows || settingsViewportNav3DMatches(ed, q) {
							rows = append(rows, settingsRowScaled(
								curPal, scale, ed,
								"editor-settings-nav3d",
								ed.T("settings.viewportNav3D"),
								ed.T("settings.viewportNav3DHelp"),
								ui.ToggleGroup{
									TestID: "editor-settings-nav3d-group",
									Label:  ed.T("settings.viewportNav3D"),
									Children: []core.Node{
										nav3DToggle("editor-settings-nav3d-default", ed.T("settings.nav3dDefault"), Navigation3DDefault),
										nav3DToggle("editor-settings-nav3d-blender", ed.T("settings.nav3dBlender"), Navigation3DBlender),
										nav3DToggle("editor-settings-nav3d-maya", ed.T("settings.nav3dMaya"), Navigation3DMaya),
									},
								},
								policyYouOnly,
								nil,
							))
						}

						var overlayRows []core.Node
						for _, ov := range overlays {
							o := ov
							if narrowRows && !settingsViewportOverlayMatches(ed, o, q) {
								continue
							}
							overlayRows = append(overlayRows, settingsViewportOverlayRow(curPal, scale, ed, rev, o))
						}
						if len(overlayRows) > 0 {
							sectionChildren := []core.Node{
								core.View{
									Style: core.Style{Padding: core.Edges{Top: 12, Bottom: 4}, Gap: 2},
									Children: []core.Node{
										core.Text{
											Content: ed.T("settings.viewportOverlaysTitle"),
											Style: core.Style{
												FontSize:      settingsScaledFont(11, scale),
												FontWeight:    600,
												LetterSpacing: 0.66,
												Color:         curPal.TextFaint,
											},
										},
										core.Text{
											Content: ed.T("settings.viewportOverlaysHelp"),
											Style:   core.Style{FontSize: settingsScaledFont(11.5, scale), Color: curPal.TextFaint},
										},
									},
								},
							}
							sectionChildren = append(sectionChildren, overlayRows...)
							rows = append(rows, core.View{
								TestID:   "editor-settings-overlays-section",
								Style:    core.Style{Gap: 2},
								Children: sectionChildren,
							})
						}

						return core.View{
							Style:    core.Style{Gap: 4},
							Children: rows,
						}
					}),
				},
			},
			footer,
		},
	}
}

func settingsViewportOverlayRow(p SpecPalette, scale float32, ed *Editor, rev *core.Signal[uint64], o ViewportOverlay) core.Node {
	id := o.ID
	name := ed.overlayDisplayName(o)
	col := o.Color
	if col == 0 {
		col = p.TextMuted
	}
	swatch := core.Canvas{
		Style: core.Style{
			Width:      core.Pt(18),
			Height:     core.Pt(12),
			FlexShrink: 0,
		},
		Draw: func(d *core.Drawing, size core.Size) {
			w, h := float32(size.Width), float32(size.Height)
			d.Save()
			switch o.Swatch {
			case OverlaySwatchBox:
				d.SetStrokeColor(col).SetLineWidth(1.25)
				d.StrokeRect(1, 1, w-2, h-2)
			case OverlaySwatchDashedBox:
				if col == p.Warning.Fg {
					d.SetFillColor(ui.Alpha(p.Warning.Fg, 0.14)).FillRect(1, 1, w-2, h-2)
				}
				d.SetStrokeColor(col).SetLineWidth(1.25).SetLineDash(2, 2)
				d.StrokeRect(1, 1, w-2, h-2)
			case OverlaySwatchLine:
				d.SetStrokeColor(col).SetLineWidth(1.5)
				d.BeginPath().MoveTo(1, h*0.5).LineTo(w-1, h*0.5).Stroke()
			case OverlaySwatchDashedLine:
				d.SetStrokeColor(col).SetLineWidth(1.25).SetLineDash(2, 2)
				d.BeginPath().MoveTo(1, h*0.5).LineTo(w-1, h*0.5).Stroke()
			case OverlaySwatchRegion:
				d.SetFillColor(ui.Alpha(p.TextMuted, 0.25)).FillRect(1, 2, w-2, h-3)
				d.SetStrokeColor(col).SetLineWidth(1.25).SetLineDash(2, 2)
				d.BeginPath().MoveTo(1, 2).LineTo(w-1, 2).Stroke()
			case OverlaySwatchLabel:
				d.SetStrokeColor(col).SetLineWidth(2)
				d.BeginPath().MoveTo(2, h-2).LineTo(w-2, h-2).Stroke()
			}
			d.Restore()
		},
	}

	return core.View{
		TestID: "editor-settings-overlay-row-" + id,
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         16,
			Padding:     core.Symmetric(settingsScaledPt(10, scale), 0),
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					Gap:        10,
				},
				Children: []core.Node{
					swatch,
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
						Children: []core.Node{
							core.Text{
								Content: name,
								Style:   core.Style{FontSize: settingsScaledFont(12.5, scale), FontWeight: 500, Color: p.TextPrimary},
							},
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									if o.Count == nil {
										return o.ID
									}
									n := max(0, o.Count(ed))
									if o.CountUnit != nil {
										return o.CountUnit(ed, n)
									}
									return fmt.Sprintf("%d", n)
								},
								Style: core.Style{FontSize: settingsScaledFont(11, scale), FontFamily: core.FontMonospace, Color: p.TextFaint},
							},
						},
					},
				},
			},
			SpecSwitch{
				TestID: "editor-settings-overlay-" + id,
				Label:  name,
				Size:   ui.SizeSmall,
				Checked: func() bool {
					_ = rev.Get()
					return ed.ViewportOverlayEnabled(id)
				},
				OnChange: func(on bool) {
					ed.report(ed.SetViewportOverlay(id, on))
				},
			},
			settingsPolicyBadge(p, ed, policyYouOnly),
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
	return settingsRowScaled(p, 1.0, ed, testID, label, help, control, policy, errNode)
}

func settingsRowScaled(p SpecPalette, scale float32, ed *Editor, testID, label, help string, control core.Node, policy settingsAgentPolicy, errNode core.Node) core.Node {
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
							Style:   core.Style{FontSize: settingsScaledFont(12.5, scale), FontWeight: 600, Color: p.TextPrimary},
						},
						core.Text{
							Content: help,
							Style:   core.Style{FontSize: settingsScaledFont(11.5, scale), Color: p.TextFaint},
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
			Padding:     core.Symmetric(settingsScaledPt(12, scale), 0),
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
