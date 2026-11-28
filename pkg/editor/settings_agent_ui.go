package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// settingsModelsPageView renders Settings › Models and connections matching design/editor/Settings.dc.html
// using exclusively theme.Palette tokens and real accessible controls. All settings on this page are You only
// and stored per computer (API keys strictly in the OS keychain, never in any file).
func settingsModelsPageView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p theme.Palette) core.Node {
	apiKeyEditorOpen := core.NewSignal(cx, false)
	apiKeyDraft := core.NewSignal(cx, "")
	apiKeyErr := core.NewSignal(cx, "")
	routeErr := core.NewSignal(cx, "")

	// Clear any transient API key input when leaving the Models page or closing Settings.
	cx.Effect(func() {
		_ = rev.Get()
		if !ed.SettingsOpen() || ed.SettingsPage() != SettingsPageModels || ed.Playing() {
			if apiKeyDraft.Get() != "" {
				apiKeyDraft.Set("")
			}
			if apiKeyEditorOpen.Get() {
				apiKeyEditorOpen.Set(false)
			}
			if apiKeyErr.Get() != "" {
				apiKeyErr.Set("")
			}
		}
	})

	routeOptions := func() []ui.Option[string] {
		_ = rev.Get()
		primaryName := "Claude Code"
		if c, ok := ed.AgentPrimaryClient(); ok && c.DisplayName != "" {
			primaryName = c.DisplayName
		}
		primary := primaryName + " · default"
		provider := ed.BuiltInAgentProvider()
		opts := []ui.Option[string]{
			{Value: primary, Label: ed.T("settings.models.routeDefault", l10n.Param("name", primaryName))},
			{Value: provider + " · fast", Label: ed.T("settings.models.routeFast", l10n.Param("name", provider))},
			{Value: provider + " · default", Label: ed.T("settings.models.routeDefault", l10n.Param("name", provider))},
			{Value: "Not connected", Label: ed.T("settings.models.notConnected")},
		}
		seen := make(map[string]bool, len(opts)+4)
		for _, o := range opts {
			seen[o.Value] = true
		}
		for _, srv := range ed.AgentMCPServers() {
			if srv.IsEnabled() && srv.Name != DefaultRunningGameMCPServerName {
				lbl := srv.DisplayTitle()
				if !seen[lbl] {
					seen[lbl] = true
					opts = append(opts, ui.Option[string]{Value: lbl, Label: lbl})
				}
			}
		}
		for _, rd := range AgentTaskRoutesDefs() {
			if cur := ed.AgentTaskRoute(rd.Key); cur != "" && !seen[cur] {
				seen[cur] = true
				opts = append(opts, ui.Option[string]{Value: cur, Label: cur})
			}
		}
		return opts
	}

	localCard := core.View{
		TestID: "settings-models-local-card",
		StyleFunc: func() core.Style {
			_ = rev.Get()
			border := p.LineRaised
			var bw core.Points = 1
			if len(ed.AgentClients()) > 0 || ed.AgentMode() == AgentModeLocal {
				border = p.Accent.Fg
				bw = 1.5
			}
			return core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				CornerRadius: 12,
				Padding:      core.All(14),
				Gap:          6,
				Background:   p.Raised,
				BorderWidth:  bw,
				BorderColor:  border,
			}
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
						TestID: "settings-models-local-dot",
						StyleFunc: func() core.Style {
							_ = rev.Get()
							col := p.TextFaint
							switch ed.AgentStatus() {
							case AgentStatusConnected, AgentStatusWorking:
								col = p.Added.Fg
							case AgentStatusDisconnected:
								col = p.Removed.Fg
							case AgentStatusWaitingForYou:
								col = p.Warning.Fg
							}
							return core.Style{
								Width:        core.Pt(8),
								Height:       core.Pt(8),
								CornerRadius: 4,
								Background:   col,
								FlexShrink:   0,
							}
						},
					},
					core.Text{
						TestID: "settings-models-local-name",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.AgentMode() == AgentModeNone {
								return ed.T("settings.models.localNone")
							}
							if c, ok := ed.AgentPrimaryClient(); ok && c.DisplayName != "" {
								return c.DisplayName
							}
							return ed.T("settings.models.localTitle")
						},
						Style: core.Style{
							FlexGrow:   1,
							FontSize:   13,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
				},
			},
			core.Text{
				TestID: "settings-models-local-status",
				ContentFunc: func() string {
					_ = rev.Get()
					if ed.AgentMode() == AgentModeNone {
						return ed.T("settings.models.localNoneDesc")
					}
					if len(ed.AgentClients()) > 0 {
						return ed.T("settings.models.localDefault")
					}
					if ed.AgentStatus() == AgentStatusDisconnected {
						return ed.T("settings.models.localDisconnected")
					}
					var detected []string
					for _, opt := range ed.DetectedLocalAgents() {
						if opt.Kind != "apikey" && opt.Detected {
							detected = append(detected, opt.Name)
						}
					}
					if len(detected) > 0 {
						return ed.T("settings.models.localDetected", l10n.Param("agents", strings.Join(detected, ", ")))
					}
					return ed.T("settings.models.localIdle")
				},
				Style: core.Style{
					FontSize: 12,
					Color:    p.TextFaint,
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, JustifyContent: core.JustifyEnd, Padding: core.Edges{Top: 2}},
				Children: []core.Node{
					core.Text{
						Content: ed.T("settings.policyYouOnly"),
						Style:   core.Style{FontSize: 11, FontWeight: 500, Color: p.TextFaint},
					},
				},
			},
		},
	}

	builtInCard := core.View{
		TestID: "settings-models-builtin-card",
		StyleFunc: func() core.Style {
			_ = rev.Get()
			border := p.LineRaised
			var bw core.Points = 1
			if ed.AgentMode() == AgentModeAPIKey && ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
				border = p.Accent.Fg
				bw = 1.5
			}
			return core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				CornerRadius: 12,
				Padding:      core.All(14),
				Gap:          6,
				Background:   p.Raised,
				BorderWidth:  bw,
				BorderColor:  border,
			}
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
						TestID: "settings-models-builtin-dot",
						StyleFunc: func() core.Style {
							_ = rev.Get()
							col := p.TextFaint
							if ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
								col = p.Added.Fg
							}
							return core.Style{
								Width:        core.Pt(8),
								Height:       core.Pt(8),
								CornerRadius: 4,
								Background:   col,
								FlexShrink:   0,
							}
						},
					},
					core.Text{
						TestID: "settings-models-builtin-name",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.BuiltInAgentProvider()
						},
						Style: core.Style{
							FlexGrow:   1,
							FontSize:   13,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
				},
			},
			core.Text{
				TestID: "settings-models-builtin-status",
				ContentFunc: func() string {
					_ = rev.Get()
					if ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
						return ed.T("settings.models.builtInStored")
					}
					return ed.T("settings.models.builtInMissing")
				},
				Style: core.Style{
					FontSize: 12,
					Color:    p.TextFaint,
				},
			},
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            8,
					Padding:        core.Edges{Top: 2},
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 6},
						Children: []core.Node{
							ui.Button{
								TestID:  "settings-models-apikey-edit",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								TextFunc: func() string {
									_ = rev.Get()
									if ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount) {
										return ed.T("settings.models.apiKeyEdit")
									}
									return ed.T("settings.models.apiKeySet")
								},
								Color:     func() core.Color { return p.Raised },
								TextStyle: core.Style{FontSize: 12, FontWeight: 600, Color: p.TextSecondary},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 8,
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
									FlexShrink:   0,
									FocusVisible: p.FocusRing(2),
								},
								OnPress: func() {
									apiKeyErr.Set("")
									apiKeyDraft.Set("")
									apiKeyEditorOpen.Set(!apiKeyEditorOpen.Get())
								},
							},
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount)
								},
								Then: func(cx *core.Scope) core.Node {
									return settingsSpecButton(p, "settings-models-apikey-remove", ed.T("settings.models.apiKeyRemove"), ed.T("settings.models.apiKeyRemove"), false, nil, func() {
										if err := ed.DeleteAgentAPIKey(DefaultAgentAPIKeyAccount); err != nil {
											apiKeyErr.Set(err.Error())
											return
										}
										apiKeyErr.Set("")
										apiKeyDraft.Set("")
										apiKeyEditorOpen.Set(false)
									})
								},
							},
						},
					},
					core.Text{
						Content: ed.T("settings.policyYouOnly"),
						Style:   core.Style{FontSize: 11, FontWeight: 500, Color: p.TextFaint},
					},
				},
			},
		},
	}

	connectBtn := ui.Button{
		TestID:  "settings-models-connect-btn",
		Variant: ui.ButtonGhost,
		Size:    ui.SizeSmall,
		Flex:    1,
		Text:    ed.T("settings.connectAnother"),
		Label:   ed.T("settings.connectAnother"),
		Color:   func() core.Color { return p.Bg },
		TextStyle: core.Style{
			FontSize:   12.5,
			FontWeight: 500,
			Color:      p.TextMuted,
			TextAlign:  core.TextAlignLeft,
		},
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			MinHeight:    core.Pt(82),
			CornerRadius: 12,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			FocusVisible: p.FocusRing(2),
		},
		OnPress: func() {
			ed.OpenConnectDialog()
		},
	}

	saveAPIKey := func() {
		key := strings.TrimSpace(apiKeyDraft.Get())
		if err := ed.StoreAgentAPIKey(DefaultAgentAPIKeyAccount, key); err != nil {
			apiKeyErr.Set(err.Error())
			return
		}
		apiKeyDraft.Set("")
		apiKeyErr.Set("")
		apiKeyEditorOpen.Set(false)
	}

	apiKeyForm := core.Show{
		When: apiKeyEditorOpen.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "settings-models-apikey-form",
				Style: core.Style{
					CornerRadius: 12,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(14),
					Gap:          10,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
						Children: []core.Node{
							core.Text{
								Content: ed.T("settings.models.apiKeyLabel"),
								Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary},
							},
							settingsPolicyBadge(p, ed, policyYouOnly),
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								Children: []core.Node{
									ui.Input{
										TestID:      "settings-models-apikey-input",
										Label:       ed.T("settings.models.apiKeyLabel"),
										Placeholder: ed.T("settings.models.apiKeyPlaceholder"),
										Masked:      true,
										Value:       apiKeyDraft.Get,
										OnChange: func(v string) {
											apiKeyDraft.Set(v)
											if apiKeyErr.Get() != "" {
												apiKeyErr.Set("")
											}
										},
										OnSubmit: func(string) { saveAPIKey() },
									},
								},
							},
							settingsSpecButton(p, "settings-models-apikey-save", ed.T("settings.models.apiKeySave"), ed.T("settings.models.apiKeySave"), true, nil, saveAPIKey),
							settingsSpecButton(p, "settings-models-apikey-cancel", ed.T("settings.models.apiKeyCancel"), ed.T("settings.models.apiKeyCancel"), false, nil, func() {
								apiKeyDraft.Set("")
								apiKeyErr.Set("")
								apiKeyEditorOpen.Set(false)
							}),
						},
					},
					core.Text{
						Content: ed.T("settings.models.apiKeyHint"),
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
					},
					core.Show{
						When: func() bool { return apiKeyErr.Get() != "" },
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:      "settings-models-apikey-error",
								ContentFunc: apiKeyErr.Get,
								Style:       core.Style{FontSize: 11.5, Color: p.Removed.Fg},
							}
						},
					},
				},
			}
		},
	}

	routeDefs := AgentTaskRoutesDefs()
	routeNodes := make([]core.Node, 0, len(routeDefs))
	for i, rd := range routeDefs {
		rd := rd
		var bottomBorder core.Points = 1
		if i == len(routeDefs)-1 {
			bottomBorder = 0
		}
		routeNodes = append(routeNodes, core.View{
			TestID: "settings-models-route-row-" + rd.Key,
			Style: core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         12,
				Padding:     core.Symmetric(10, 14),
				Borders:     core.Edges{Bottom: bottomBorder},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Width: core.Pt(200), FlexShrink: 0, Gap: 2},
					Children: []core.Node{
						core.Text{
							Content: ed.T(rd.LabelKey),
							Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
						},
						core.Text{
							Content: ed.T(rd.NoteKey),
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
					Children: []core.Node{
						ui.Select[string]{
							TestID:      "settings-models-route-" + rd.Key,
							Label:       ed.T(rd.LabelKey),
							OptionsFunc: routeOptions,
							Value: func() string {
								_ = rev.Get()
								return ed.AgentTaskRoute(rd.Key)
							},
							OnChange: func(v string) {
								if err := ed.SetAgentTaskRoute(rd.Key, v); err != nil {
									routeErr.Set(err.Error())
									return
								}
								routeErr.Set("")
							},
						},
					},
				},
				core.Text{
					Content: ed.T(rd.CostHintKey),
					Style: core.Style{
						Width:      core.Pt(148),
						FlexShrink: 0,
						FontSize:   11.5,
						Color:      p.TextFaint,
					},
				},
				settingsPolicyBadge(p, ed, policyYouOnly),
			},
		})
	}

	return ui.ScrollView{
		TestID: "settings-models-page",
		Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Gap: 16, Padding: core.Edges{Bottom: 16}},
				Children: []core.Node{
					core.View{
						Style:    core.Style{Direction: core.Row, Gap: 12},
						Children: []core.Node{localCard, builtInCard, connectBtn},
					},
					apiKeyForm,
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
						Children: []core.Node{
							core.Text{
								Content: ed.T("settings.models.whoDoesWhat"),
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.66,
									Color:         p.TextFaint,
								},
							},
							settingsPolicyBadge(p, ed, policyYouOnly),
						},
					},
					core.View{
						TestID: "settings-models-routes",
						Style: core.Style{
							CornerRadius: 12,
							Background:   p.Panel,
							BorderWidth:  1,
							BorderColor:  p.Line,
							Overflow:     core.Hidden,
						},
						Children: routeNodes,
					},
					core.Show{
						When: func() bool { return routeErr.Get() != "" },
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:      "settings-models-route-error",
								ContentFunc: routeErr.Get,
								Style:       core.Style{FontSize: 11.5, Color: p.Removed.Fg},
							}
						},
					},
				},
			},
		},
	}
}

// settingsToolsPageView renders Settings › Tools matching design/editor/Settings.dc.html:
// every tool registered on the editor's MCP server with its name, description, and role=switch toggle,
// plus other MCP servers the agent may use (starting with Running game · nsgo dev) and "+ Add a server".
func settingsToolsPageView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p theme.Palette) core.Node {
	toolFilter := core.NewSignal(cx, "")
	addServerOpen := core.NewSignal(cx, false)
	srvName := core.NewSignal(cx, "")
	srvCmd := core.NewSignal(cx, "")
	srvArgs := core.NewSignal(cx, "")
	srvDesc := core.NewSignal(cx, "")
	srvErr := core.NewSignal(cx, "")

	cx.Effect(func() {
		_ = rev.Get()
		if !ed.SettingsOpen() || ed.SettingsPage() != SettingsPageTools || ed.Playing() {
			if addServerOpen.Get() {
				addServerOpen.Set(false)
			}
			if srvErr.Get() != "" {
				srvErr.Set("")
			}
		}
	})

	toolsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		q := strings.ToLower(strings.TrimSpace(toolFilter.Get()))
		tools := ed.RegisteredMCPTools()
		var b strings.Builder
		b.WriteString(q)
		b.WriteByte('|')
		for _, t := range tools {
			b.WriteString(t.Name)
			b.WriteByte(',')
		}
		return b.String()
	})

	serversKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		srvs := ed.AgentMCPServers()
		var b strings.Builder
		for _, s := range srvs {
			fmt.Fprintf(&b, "%s:%s:%v|", s.Name, s.CommandLine(), s.IsEnabled())
		}
		return b.String()
	})

	toolGrid := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = toolsKey.Get()
		q := strings.ToLower(strings.TrimSpace(toolFilter.Get()))
		all := ed.RegisteredMCPTools()
		var filtered []AgentToolInfo
		for _, t := range all {
			if q == "" || strings.Contains(strings.ToLower(t.Name), q) || strings.Contains(strings.ToLower(t.ShortDescription()), q) {
				filtered = append(filtered, t)
			}
		}

		// Pair into 2-column rows matching Settings.dc.html.
		var rows []core.Node
		for i := 0; i < len(filtered); i += 2 {
			rowChildren := make([]core.Node, 0, 2)
			for j := i; j < i+2; j++ {
				if j >= len(filtered) {
					rowChildren = append(rowChildren, core.View{Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)}})
					continue
				}
				t := filtered[j]
				rowChildren = append(rowChildren, core.View{
					TestID: "settings-tool-card-" + t.Name,
					Style: core.Style{
						FlexGrow:     1,
						FlexBasis:    core.Pt(0),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          10,
						Padding:      core.Symmetric(10, 12),
						CornerRadius: 10,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
							Children: []core.Node{
								core.Text{
									Content: t.Name,
									Style: core.Style{
										FontFamily: core.FontMonospace,
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextPrimary,
									},
								},
								core.Text{
									Content: localizedAgentToolDesc(ed, t),
									Style: core.Style{
										FontSize: 11.5,
										Color:    p.TextFaint,
									},
								},
							},
						},
						ui.Switch{
							TestID: "settings-tool-switch-" + t.Name,
							Label:  t.Name,
							Size:   ui.SizeSmall,
							Checked: func() bool {
								_ = rev.Get()
								return ed.IsMCPToolEnabled(t.Name)
							},
							OnChange: func(on bool) {
								ed.report(ed.SetMCPToolEnabled(t.Name, on))
							},
						},
					},
				})
			}
			rows = append(rows, core.View{
				Style:    core.Style{Direction: core.Row, Gap: 8},
				Children: rowChildren,
			})
		}
		return core.View{
			TestID:   "settings-tools-grid",
			Style:    core.Style{Gap: 8},
			Children: rows,
		}
	})

	serversList := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = serversKey.Get()
		srvs := ed.AgentMCPServers()
		rows := make([]core.Node, 0, len(srvs))
		for _, srv := range srvs {
			srv := srv
			rowChildren := []core.Node{
				core.View{
					StyleFunc: func() core.Style {
						_ = rev.Get()
						col := p.TextFaint
						if srv.IsEnabled() {
							col = p.Added.Fg
						}
						return core.Style{
							Width:        core.Pt(8),
							Height:       core.Pt(8),
							CornerRadius: 4,
							Background:   col,
							FlexShrink:   0,
						}
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							Content: localizedMCPServerTitle(ed, srv),
							Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
						},
						core.Text{
							Content: srv.CommandLine(),
							Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11, Color: p.TextFaint},
						},
					},
				},
			}
			if srv.Description != "" {
				rowChildren = append(rowChildren, core.Text{
					Content: localizedMCPServerDesc(ed, srv),
					Style:   core.Style{FontSize: 11.5, Color: p.TextFaint, FlexShrink: 1},
				})
			}
			rowChildren = append(rowChildren, ui.Switch{
				TestID: "settings-mcp-server-switch-" + srv.Name,
				Label:  localizedMCPServerTitle(ed, srv),
				Size:   ui.SizeSmall,
				Checked: func() bool {
					_ = rev.Get()
					for _, cur := range ed.AgentMCPServers() {
						if cur.Name == srv.Name {
							return cur.IsEnabled()
						}
					}
					return false
				},
				OnChange: func(on bool) {
					if err := ed.SetAgentMCPServerEnabled(srv.Name, on); err != nil {
						srvErr.Set(err.Error())
						return
					}
					srvErr.Set("")
				},
			})
			if srv.Name != DefaultRunningGameMCPServerName {
				rowChildren = append(rowChildren, settingsSpecButton(p, "settings-mcp-server-remove-"+srv.Name, ed.T("settings.tools.removeServer"), ed.T("settings.tools.removeServerNamed", l10n.Param("name", srv.Name)), false, nil, func() {
					if err := ed.RemoveAgentMCPServer(srv.Name); err != nil {
						srvErr.Set(err.Error())
						return
					}
					srvErr.Set("")
				}))
			}
			rows = append(rows, core.View{
				TestID: "settings-mcp-server-" + srv.Name,
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          10,
					Padding:      core.Symmetric(10, 12),
					CornerRadius: 10,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: rowChildren,
			})
		}
		return core.View{
			TestID:   "settings-mcp-servers-list",
			Style:    core.Style{Gap: 8},
			Children: rows,
		}
	})

	addServerForm := core.Show{
		When: addServerOpen.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "settings-mcp-server-form",
				Style: core.Style{
					CornerRadius: 10,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(12),
					Gap:          8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 4},
								Children: []core.Node{
									core.Text{Content: ed.T("settings.tools.addName"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
									ui.Input{
										TestID:      "settings-mcp-server-name",
										Label:       ed.T("settings.tools.addName"),
										Placeholder: ed.T("settings.tools.addNamePlaceholder"),
										Value:       srvName.Get,
										OnChange:    srvName.Set,
									},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 4},
								Children: []core.Node{
									core.Text{Content: ed.T("settings.tools.addCommand"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
									ui.Input{
										TestID:      "settings-mcp-server-command",
										Label:       ed.T("settings.tools.addCommand"),
										Placeholder: ed.T("settings.tools.addCommandPlaceholder"),
										Value:       srvCmd.Get,
										OnChange:    srvCmd.Set,
									},
								},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 4},
								Children: []core.Node{
									core.Text{Content: ed.T("settings.tools.addArgs"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
									ui.Input{
										TestID:      "settings-mcp-server-args",
										Label:       ed.T("settings.tools.addArgs"),
										Placeholder: ed.T("settings.tools.addArgsPlaceholder"),
										Value:       srvArgs.Get,
										OnChange:    srvArgs.Set,
									},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 4},
								Children: []core.Node{
									core.Text{Content: ed.T("settings.tools.addDesc"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
									ui.Input{
										TestID:      "settings-mcp-server-desc",
										Label:       ed.T("settings.tools.addDesc"),
										Placeholder: ed.T("settings.tools.addDescPlaceholder"),
										Value:       srvDesc.Get,
										OnChange:    srvDesc.Set,
									},
								},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyEnd, Gap: 8},
						Children: []core.Node{
							settingsSpecButton(p, "settings-mcp-server-cancel", ed.T("settings.tools.addCancel"), ed.T("settings.tools.addCancel"), false, nil, func() {
								srvErr.Set("")
								addServerOpen.Set(false)
							}),
							settingsSpecButton(p, "settings-mcp-server-submit", ed.T("settings.tools.addSubmit"), ed.T("settings.tools.addSubmit"), true, nil, func() {
								on := true
								err := ed.AddAgentMCPServer(AgentMCPServer{
									Name:        srvName.Get(),
									Title:       srvName.Get(),
									Description: srvDesc.Get(),
									Command:     srvCmd.Get(),
									Args:        strings.Fields(srvArgs.Get()),
									Enabled:     &on,
								})
								if err != nil {
									srvErr.Set(err.Error())
									return
								}
								srvName.Set("")
								srvCmd.Set("")
								srvArgs.Set("")
								srvDesc.Set("")
								srvErr.Set("")
								addServerOpen.Set(false)
							}),
						},
					},
				},
			}
		},
	}

	return ui.ScrollView{
		TestID: "settings-tools-page",
		Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Gap: 14, Padding: core.Edges{Bottom: 16}},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 12},
						Children: []core.Node{
							core.Text{
								TestID: "settings-tools-header",
								ContentFunc: func() string {
									_ = rev.Get()
									_, total := ed.EnabledMCPToolCount()
									return ed.T("settings.tools.editorServerHeader", l10n.Param("n", total))
								},
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.66,
									Color:         p.TextFaint,
								},
							},
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
								Children: []core.Node{
									core.View{
										Style: core.Style{Width: core.Pt(220)},
										Children: []core.Node{
											ui.Input{
												TestID:      "settings-tools-filter",
												Label:       ed.T("settings.tools.filterLabel"),
												Placeholder: ed.T("settings.tools.filterPlaceholder"),
												Value:       toolFilter.Get,
												OnChange:    toolFilter.Set,
											},
										},
									},
									settingsPolicyBadge(p, ed, policyYouOnly),
								},
							},
						},
					},
					ui.ScrollView{
						TestID:   "settings-tools-grid-scroll",
						Style:    core.Style{MaxHeight: core.Pt(380)},
						Children: []core.Node{toolGrid},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Padding: core.Edges{Top: 6}},
						Children: []core.Node{
							core.Text{
								Content: ed.T("settings.tools.otherServersHeader"),
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.66,
									Color:         p.TextFaint,
								},
							},
							settingsPolicyBadge(p, ed, policyYouOnly),
						},
					},
					serversList,
					core.Show{
						When: func() bool { return srvErr.Get() != "" },
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:      "settings-mcp-server-error",
								ContentFunc: srvErr.Get,
								Style:       core.Style{FontSize: 11.5, Color: p.Removed.Fg},
							}
						},
					},
					addServerForm,
					ui.Button{
						TestID:  "settings-mcp-server-add-toggle",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Text:    ed.T("settings.tools.addButton"),
						Label:   ed.T("settings.tools.addButton"),
						Color:   func() core.Color { return p.Bg },
						TextStyle: core.Style{
							FontSize:   12.5,
							FontWeight: 500,
							Color:      p.TextMuted,
							TextAlign:  core.TextAlignLeft,
						},
						Style: core.Style{
							Width:        core.Pct(100),
							Height:       core.Pt(38),
							CornerRadius: 10,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							FocusVisible: p.FocusRing(2),
						},
						OnPress: func() {
							srvErr.Set("")
							addServerOpen.Set(!addServerOpen.Get())
						},
					},
				},
			},
		},
	}
}

// settingsUsagePageView renders Settings › Usage and privacy matching design/editor/Settings.dc.html:
// weekly requests vs weekly limit, kept review percentage, local playtest runs, and the four
// WHAT MAY LEAVE THIS COMPUTER switches enforced by the MCP server for every agent.
func settingsUsagePageView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p theme.Palette) core.Node {
	limitDraft := core.NewSignal(cx, strconv.Itoa(ed.WeeklyRequestLimit()))
	limitErr := core.NewSignal(cx, "")
	savedLimit := core.NewMemo(cx, func() int {
		_ = rev.Get()
		return ed.WeeklyRequestLimit()
	})

	// Only sync limitDraft when the persisted weekly limit actually changes, so unrelated
	// editor notifications (autosave, agent ticks, disk watch) never overwrite what the user is typing.
	cx.Effect(func() {
		limitDraft.Set(strconv.Itoa(savedLimit.Get()))
	})

	applyLimit := func() {
		raw := strings.TrimSpace(limitDraft.Get())
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			limitErr.Set(ed.T("settings.usage.limitInvalid"))
			return
		}
		if err := ed.SetWeeklyRequestLimit(n); err != nil {
			limitErr.Set(err.Error())
			return
		}
		limitErr.Set("")
	}

	weekCard := core.View{
		TestID: "settings-usage-week-card",
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(14),
			Gap:          6,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("settings.usage.thisWeekLabel"),
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.Text{
				TestID: "settings-usage-week-value",
				ContentFunc: func() string {
					_ = rev.Get()
					return ed.T("settings.usage.thisWeekValue", l10n.Param("n", ed.AgentUsage().WeeklyRequests))
				},
				Style: core.Style{FontSize: 22, FontWeight: 600, Color: p.TextPrimary},
			},
			core.View{
				Style: core.Style{
					Height:       core.Pt(6),
					CornerRadius: 3,
					Background:   p.LineRaised,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					core.View{
						TestID: "settings-usage-week-bar",
						StyleFunc: func() core.Style {
							_ = rev.Get()
							req := ed.AgentUsage().WeeklyRequests
							lim := ed.WeeklyRequestLimit()
							pct := 0.0
							if lim > 0 {
								pct = min(100.0, float64(req)*100.0/float64(lim))
							}
							fill := p.Accent.Fg
							if pct >= 100 {
								fill = p.Removed.Fg
							} else if pct >= 85 {
								fill = p.Changed.Fg
							}
							return core.Style{
								Width:        core.Pct(float32(pct)),
								Height:       core.Pt(6),
								CornerRadius: 3,
								Background:   fill,
							}
						},
					},
				},
			},
			core.Text{
				TestID: "settings-usage-week-caption",
				ContentFunc: func() string {
					_ = rev.Get()
					lim := ed.WeeklyRequestLimit()
					if lim <= 0 {
						return ed.T("settings.usage.limitNone")
					}
					req := ed.AgentUsage().WeeklyRequests
					pct := (req*100 + lim/2) / lim
					return ed.T("settings.usage.limitPercent", l10n.Param("pct", pct))
				},
				Style: core.Style{FontSize: 11.5, Color: p.TextFaint},
			},
		},
	}

	keptCard := core.View{
		TestID: "settings-usage-kept-card",
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(14),
			Gap:          6,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("settings.usage.keptLabel"),
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.Text{
				TestID: "settings-usage-kept-value",
				ContentFunc: func() string {
					_ = rev.Get()
					if pct, ok := ed.AgentUsage().KeptPercent(); ok {
						return fmt.Sprintf("%d%%", pct)
					}
					return "—"
				},
				Style: core.Style{FontSize: 22, FontWeight: 600, Color: p.TextPrimary},
			},
			core.Text{
				ContentFunc: func() string {
					_ = rev.Get()
					if _, ok := ed.AgentUsage().KeptPercent(); ok {
						return ed.T("settings.usage.keptCaption")
					}
					return ed.T("settings.usage.keptEmpty")
				},
				Style: core.Style{FontSize: 11.5, Color: p.TextFaint},
			},
		},
	}

	playtestsCard := core.View{
		TestID: "settings-usage-playtests-card",
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(14),
			Gap:          6,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("settings.usage.playtestsLabel"),
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.Text{
				TestID: "settings-usage-playtests-value",
				ContentFunc: func() string {
					_ = rev.Get()
					return ed.T("settings.usage.playtestsValue", l10n.Param("n", ed.AgentUsage().PlaytestRuns))
				},
				Style: core.Style{FontSize: 22, FontWeight: 600, Color: p.TextPrimary},
			},
			core.Text{
				Content: ed.T("settings.usage.playtestsCaption"),
				Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
			},
		},
	}

	limitControl := core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
		Children: []core.Node{
			core.View{
				Style: core.Style{Width: core.Pt(112)},
				Children: []core.Node{
					ui.Input{
						TestID:   "settings-usage-limit-input",
						Label:    ed.T("settings.usage.limitLabel"),
						Value:    limitDraft.Get,
						OnChange: limitDraft.Set,
						OnSubmit: func(string) { applyLimit() },
					},
				},
			},
			settingsSpecButton(p, "settings-usage-limit-apply", ed.T("settings.usage.limitApply"), ed.T("settings.usage.limitApply"), false, nil, applyLimit),
		},
	}

	privacyCard := func(testID string, kind PrivacyContentKind, titleKey, helpKey string) core.Node {
		return core.View{
			TestID: testID + "-card",
			Style: core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          12,
				Padding:      core.Symmetric(12, 14),
				CornerRadius: 10,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							Content: ed.T(titleKey),
							Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary},
						},
						core.Text{
							Content: ed.T(helpKey),
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
					},
				},
				ui.Switch{
					TestID: testID,
					Label:  ed.T(titleKey),
					Size:   ui.SizeSmall,
					Checked: func() bool {
						_ = rev.Get()
						return ed.AgentPrivacy().Allows(kind)
					},
					OnChange: func(on bool) {
						ed.report(ed.SetAgentPrivacy(kind, on))
					},
				},
			},
		}
	}

	return ui.ScrollView{
		TestID: "settings-usage-page",
		Style:  core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Gap: 16, Padding: core.Edges{Bottom: 16}},
				Children: []core.Node{
					core.View{
						Style:    core.Style{Direction: core.Row, Gap: 12},
						Children: []core.Node{weekCard, keptCard, playtestsCard},
					},
					settingsRow(
						p,
						ed,
						"settings-usage-limit-row",
						ed.T("settings.usage.limitLabel"),
						ed.T("settings.usage.limitHelp"),
						limitControl,
						policyYouOnly,
						core.Show{
							When: func() bool { return limitErr.Get() != "" },
							Then: func(cx *core.Scope) core.Node {
								return core.Text{
									TestID:      "settings-usage-limit-error",
									ContentFunc: limitErr.Get,
									Style:       core.Style{FontSize: 11.5, Color: p.Removed.Fg},
								}
							},
						},
					),
					core.View{
						Style: core.Style{Gap: 4},
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
								Children: []core.Node{
									core.Text{
										Content: ed.T("settings.privacy.heading"),
										Style: core.Style{
											FontSize:      11,
											FontWeight:    600,
											LetterSpacing: 0.66,
											Color:         p.TextFaint,
										},
									},
									settingsPolicyBadge(p, ed, policyYouOnly),
								},
							},
							core.Text{
								Content: ed.T("settings.privacy.note"),
								Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 12},
						Children: []core.Node{
							privacyCard("settings-privacy-scene-switch", PrivacySceneAndData, "settings.privacy.sceneAndData", "settings.privacy.sceneAndDataHelp"),
							privacyCard("settings-privacy-viewport-switch", PrivacyViewportImages, "settings.privacy.viewportImages", "settings.privacy.viewportImagesHelp"),
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 12},
						Children: []core.Node{
							privacyCard("settings-privacy-go-switch", PrivacyGoSource, "settings.privacy.goSource", "settings.privacy.goSourceHelp"),
							privacyCard("settings-privacy-art-switch", PrivacyArtAndAudio, "settings.privacy.artAndAudio", "settings.privacy.artAndAudioHelp"),
						},
					},
				},
			},
		},
	}
}

func localizedAgentToolDesc(ed *Editor, t AgentToolInfo) string {
	desc := t.ShortDescription()
	switch desc {
	case "Run game check on the project":
		return ed.T("settings.tools.desc.gameCheck")
	case "Delete files":
		return ed.T("settings.tools.desc.deleteFiles")
	case "Headless runs with replays":
		return ed.T("settings.tools.desc.gamePlaytest")
	case "Add, move and change objects; reviewable":
		return ed.T("settings.tools.desc.sceneEdit")
	case "Read any scene, prefab or data file":
		return ed.T("settings.tools.desc.sceneRead")
	case "What you have selected":
		return ed.T("settings.tools.desc.selectionGet")
	case "A picture of the viewport":
		return ed.T("settings.tools.desc.viewportImage")
	default:
		return desc
	}
}

func localizedMCPServerTitle(ed *Editor, srv AgentMCPServer) string {
	title := srv.DisplayTitle()
	if title == "Running game · nsgo dev" {
		return ed.T("settings.tools.runningGameTitle")
	}
	return title
}

func localizedMCPServerDesc(ed *Editor, srv AgentMCPServer) string {
	if srv.Description == "inspect and drive the live game" {
		return ed.T("settings.tools.runningGameDesc")
	}
	return srv.Description
}
