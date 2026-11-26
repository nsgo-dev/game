package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// RightRailView renders the editor's right sidebar: TasksPanelView when Background Tasks are open,
// InspectorView alone when AgentModeNone is chosen, or the Agent / Inspector / History tab bar
// keeping InspectorView active by default at 272 pt and showing AgentPanelView or AgentHistoryView
// (380 pt) when selected.
func RightRailView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	tasksOpenMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return !ed.Playing() && ed.TasksPanelOpen()
	})
	modeMemo := core.NewMemo(cx, func() AgentMode {
		_ = rev.Get()
		return ed.AgentMode()
	})
	tabMemo := core.NewMemo(cx, func() RightRailTab {
		_ = rev.Get()
		return ed.RightRailTab()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})

	return core.Show{
		When: tasksOpenMemo.Get,
		Then: func(cx *core.Scope) core.Node {
			return TasksPanelView(cx, ed, rev)
		},
		Else: func(cx *core.Scope) core.Node {
			return core.Show{
				When: func() bool {
					return modeMemo.Get() == AgentModeNone
				},
				Then: func(cx *core.Scope) core.Node {
					return InspectorView(cx, ed, rev)
				},
				Else: func(cx *core.Scope) core.Node {
					return core.Show{
						When: func() bool {
							return tabMemo.Get() == RightTabAgent
						},
						Then: func(cx *core.Scope) core.Node {
							return AgentPanelView(cx, ed, rev)
						},
						Else: func(cx *core.Scope) core.Node {
							return core.Show{
								When: func() bool {
									return tabMemo.Get() == RightTabHistory
								},
								Then: func(cx *core.Scope) core.Node {
									return core.Dynamic(func(cx *core.Scope) core.Node {
										p := SpecPaletteFor(darkMemo.Get())
										return core.View{
											TestID: "right-rail-history-wrap",
											Style: core.Style{
												Width:       core.Pt(380),
												FlexShrink:  0,
												Background:  p.Panel,
												Borders:     core.Edges{Left: 1},
												BorderColor: p.Line,
												Overflow:    core.Hidden,
											},
											Children: []core.Node{
												rightRailTabBar(cx, ed, rev, p, true),
												AgentHistoryView(cx, ed, rev),
											},
										}
									})
								},
								Else: func(cx *core.Scope) core.Node {
									return core.Dynamic(func(cx *core.Scope) core.Node {
										p := SpecPaletteFor(darkMemo.Get())
										return core.View{
											TestID: "right-rail-inspector-wrap",
											Style: core.Style{
												Width:       core.Pt(272),
												FlexShrink:  0,
												Background:  p.Panel,
												Borders:     core.Edges{Left: 1},
												BorderColor: p.Line,
											},
											Children: []core.Node{
												rightRailTabBar(cx, ed, rev, p, false),
												core.View{
													Style: core.Style{
														Direction: core.Row,
														FlexGrow:  1,
														FlexBasis: core.Pt(0),
														MinHeight: core.Pt(0),
														Overflow:  core.Hidden,
													},
													Children: []core.Node{InspectorView(cx, ed, rev)},
												},
											},
										}
									})
								},
							}
						},
					}
				},
			}
		},
	}
}

// rightRailTabBar renders the 44 pt Agent / Inspector header bar matching docs/design/editor/Main.dc.html and AgentStates.dc.html.
func rightRailTabBar(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, showFullChip bool) core.Node {
	tabBtn := func(testID, label string, tab RightRailTab) core.Node {
		children := []core.Node{
			core.Text{
				Content: label,
				StyleFunc: func() core.Style {
					_ = rev.Get()
					col := p.TextFaint
					if ed.RightRailTab() == tab {
						col = p.TextPrimary
					}
					return core.Style{
						FontSize:   12.5,
						FontWeight: 500,
						Color:      col,
					}
				},
			},
		}
		if tab == RightTabAgent {
			children = append(children, core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.HasPendingReview() || (ed.HasPendingAgentTurn() && ed.RightRailTab() != RightTabAgent)
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "agent-tab-pending-dot",
						Label:  ed.T("agentTurn.reviewWaiting"),
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Accent.Fg,
						},
					}
				},
			})
			children = append(children, core.Show{
				When: func() bool {
					_ = rev.Get()
					return !ed.HasPendingReview() && ed.DataTableReviewPaused()
				},
				Then: func(*core.Scope) core.Node {
					return core.View{
						TestID: "right-tab-agent-review-dot",
						Label:  ed.T("dtReview.reviewWaiting"),
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Accent.Fg,
						},
					}
				},
			})
		}
		return core.View{
			TestID: testID,
			Role:   core.RoleButton,
			Label:  label,
			StyleFunc: func() core.Style {
				_ = rev.Get()
				active := ed.RightRailTab() == tab
				var bg core.Color
				if active {
					bg = p.Raised
				}
				return core.Style{
					Direction:      core.Row,
					Gap:            6,
					Height:         core.Pt(30),
					Padding:        core.Symmetric(0, 12),
					CornerRadius:   8,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     bg,
					FocusVisible:   p.FocusRing(2),
				}
			},
			OnPress: func(core.PressEvent) {
				ed.SetRightRailTab(tab)
			},
			Children: children,
		}
	}

	var trailing core.Node
	if showFullChip {
		trailing = AgentStatusChipView(cx, ed, rev)
	} else {
		trailing = compactAgentChip(ed, rev, p)
	}

	historyBtn := core.View{
		TestID: "right-rail-tab-history",
		Role:   core.RoleButton,
		Label:  ed.T("threads.history.title"),
		StyleFunc: func() core.Style {
			_ = rev.Get()
			var bg core.Color
			if ed.RightRailTab() == RightTabHistory {
				bg = p.Raised
			}
			return core.Style{
				Width:          core.Pt(26),
				Height:         core.Pt(26),
				FlexShrink:     0,
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     bg,
				FocusVisible:   p.FocusRing(2),
			}
		},
		OnPress: func(core.PressEvent) {
			if ed.RightRailTab() == RightTabHistory {
				ed.SetRightRailTab(RightTabAgent)
			} else {
				ed.SetRightRailTab(RightTabHistory)
			}
		},
		Children: []core.Node{
			specDynamicIcon(func() *core.SVG { return lucide.Clock() }, 14, func() core.Color {
				_ = rev.Get()
				if ed.RightRailTab() == RightTabHistory {
					return p.TextPrimary
				}
				return p.TextMuted
			}),
		},
	}

	return core.View{
		TestID: "right-rail-header",
		Style: core.Style{
			MinHeight:   core.Pt(44),
			FlexShrink:  0,
			Direction:   core.Row,
			Wrap:        core.Wrap,
			AlignItems:  core.AlignCenter,
			Gap:         6,
			Padding:     core.Symmetric(7, 10),
			Background:  p.Panel,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        4,
					FlexShrink: 0,
				},
				Children: []core.Node{
					tabBtn("right-tab-agent", ed.T("agent.tab"), RightTabAgent),
					tabBtn("right-tab-inspector", ed.T("inspector.title"), RightTabInspector),
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			historyBtn,
			trailing,
		},
	}
}

// compactAgentChip renders a compact status dot/button in the Inspector tab header so the author can jump to Agent or Connect.
func compactAgentChip(ed *Editor, rev *core.Signal[uint64], p SpecPalette) core.Node {
	return core.View{
		TestID: "right-rail-agent-pill",
		Role:   core.RoleButton,
		LabelFunc: func() string {
			_ = rev.Get()
			return ed.AgentLiveAnnouncement()
		},
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(24),
			Padding:      core.Symmetric(0, 8),
			Gap:          6,
			CornerRadius: 12,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			FocusVisible: p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if ed.AgentStatus() == AgentStatusNotConnected {
				ed.OpenConnectDialog()
				return
			}
			ed.SetRightRailTab(RightTabAgent)
		},
		Children: []core.Node{
			core.View{
				StyleFunc: func() core.Style {
					_ = rev.Get()
					col := p.Changed.Fg
					switch ed.AgentStatus() {
					case AgentStatusConnected:
						col = p.Added.Fg
					case AgentStatusWorking:
						col = p.Agent.Fg
					case AgentStatusWaitingForYou:
						col = p.Warning.Fg
					case AgentStatusDisconnected, AgentStatusOffline:
						col = p.Removed.Fg
					case AgentStatusLimitReached:
						col = p.Removed.Fg
					case AgentStatusReconnecting:
						col = p.TextFaint
					}
					return core.Style{
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 3,
						Background:   col,
					}
				},
			},
			core.Text{
				ContentFunc: func() string {
					_ = rev.Get()
					switch ed.AgentStatus() {
					case AgentStatusConnected:
						if len(ed.AgentClients()) == 0 && (ed.AgentMode() == AgentModeAPIKey || ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
							return "API"
						}
						return "MCP"
					case AgentStatusWorking:
						return ed.T("agent.status.workingShort")
					case AgentStatusWaitingForYou:
						return ed.T("agent.status.waitingShort")
					case AgentStatusReconnecting:
						return ed.T("agent.status.reconnectingShort")
					case AgentStatusDisconnected:
						return ed.T("agent.status.disconnected")
					case AgentStatusLimitReached:
						return ed.T("agent.status.limitShort")
					case AgentStatusOffline:
						return ed.T("agent.status.offline")
					default:
						return ed.T("agent.connectShort")
					}
				},
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextMuted,
				},
			},
		},
	}
}

// AgentStatusChipView renders the status chip for the Agent panel header matching docs/design/editor/AgentStates.dc.html
// using exclusively Spec.dc.html tokens and announcing state transitions through LiveRegionPolite.
func AgentStatusChipView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	// 1.2 s pulse for the Working dot (600 ms half-period), disabled under ReduceMotion.
	pulseOn := core.NewSignal(cx, true)
	elapsedTick := core.NewSignal(cx, uint64(0))

	statusMemo := core.NewMemo(cx, func() AgentStatus {
		_ = rev.Get()
		return ed.AgentStatus()
	})
	reduceMotion := core.NewMemo(cx, func() bool {
		return cx.Env().ReduceMotion()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})

	cx.Effect(func() {
		st := statusMemo.Get()
		rm := reduceMotion.Get()
		if st == AgentStatusWorking && !rm {
			stopped := false
			var stop func() bool
			var tick func()
			tick = func() {
				if stopped {
					return
				}
				pulseOn.Set(!pulseOn.Get())
				stop = cx.AfterFunc(600*time.Millisecond, tick)
			}
			stop = cx.AfterFunc(600*time.Millisecond, tick)
			cx.Cleanup(func() {
				stopped = true
				if stop != nil {
					stop()
				}
			})
		} else {
			pulseOn.Set(true)
		}
		if st == AgentStatusWaitingForYou {
			stopped := false
			var stop func() bool
			var tick func()
			tick = func() {
				if stopped {
					return
				}
				elapsedTick.Set(elapsedTick.Get() + 1)
				stop = cx.AfterFunc(time.Second, tick)
			}
			stop = cx.AfterFunc(time.Second, tick)
			cx.Cleanup(func() {
				stopped = true
				if stop != nil {
					stop()
				}
			})
		}
	})

	chipContent := core.Dynamic(func(cx *core.Scope) core.Node {
		st := statusMemo.Get()
		p := SpecPaletteFor(darkMemo.Get())
		switch st {
		case AgentStatusConnected:
			pill := core.View{
				TestID: "agent-status-chip",
				Role:   core.RoleButton,
				LabelFunc: func() string {
					_ = rev.Get()
					if len(ed.AgentClients()) == 0 && (ed.AgentMode() == AgentModeAPIKey || ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
						return fmt.Sprintf("Connected · API (%s)", ed.AgentStatusTooltip())
					}
					return fmt.Sprintf("Connected · MCP (%s)", ed.AgentStatusTooltip())
				},
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.OpenConnectDialog()
				},
				Children: []core.Node{
					core.View{
						TestID: "agent-status-dot",
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Added.Fg,
							FlexShrink:   0,
						},
					},
					core.Text{
						TestID: "agent-status-text",
						ContentFunc: func() string {
							_ = rev.Get()
							if len(ed.AgentClients()) == 0 && (ed.AgentMode() == AgentModeAPIKey || ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount)) {
								return "Connected · API"
							}
							return "Connected · MCP"
						},
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextMuted,
						},
					},
				},
			}
			return ui.Tooltip{
				TestID: "agent-status-tooltip",
				TextFunc: func() string {
					_ = rev.Get()
					return ed.AgentStatusTooltip()
				},
				Trigger: pill,
			}

		case AgentStatusWorking:
			pulseHalo := ui.Alpha(p.Agent.Fg, 0.25)
			return core.View{
				TestID: "agent-status-chip",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					Background:   p.Agent.Fill,
					BorderWidth:  1,
					BorderColor:  p.Agent.Edge,
				},
				Children: []core.Node{
					core.View{
						TestID: "agent-status-dot",
						StyleFunc: func() core.Style {
							halo := pulseHalo
							op := float32(1)
							if !reduceMotion.Get() && !pulseOn.Get() {
								op = 0.45
								halo = 0
							}
							return core.Style{
								Width:          core.Pt(10),
								Height:         core.Pt(10),
								CornerRadius:   5,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Background:     halo,
								Opacity:        core.Alpha(op),
								FlexShrink:     0,
							}
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(6),
									Height:       core.Pt(6),
									CornerRadius: 3,
									Background:   p.Agent.Fg,
								},
							},
						},
					},
					core.Text{
						TestID: "agent-status-text",
						ContentFunc: func() string {
							_ = rev.Get()
							step, total := ed.AgentWorkingProgress()
							if step <= 0 {
								step = 1
							}
							if total > 0 {
								return fmt.Sprintf("Working · step %d of %d", step, total)
							}
							return fmt.Sprintf("Working · step %d", step)
						},
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextSecondary,
						},
					},
				},
			}

		case AgentStatusWaitingForYou:
			return core.View{
				TestID: "agent-status-chip",
				Role:   core.RoleButton,
				LabelFunc: func() string {
					_ = rev.Get()
					_ = elapsedTick.Get()
					return fmt.Sprintf("Waiting for you, %s. Focus open request", ed.AgentWaitingElapsed())
				},
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					Background:   p.Warning.Fill,
					BorderWidth:  1,
					BorderColor:  p.Warning.Edge,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.FocusOpenAgentRequest()
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Clock(),
						Style: core.Style{
							Width:      core.Pt(13),
							Height:     core.Pt(13),
							Color:      p.Warning.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID: "agent-status-text",
						ContentFunc: func() string {
							_ = rev.Get()
							_ = elapsedTick.Get()
							return fmt.Sprintf("Waiting for you · %s", ed.AgentWaitingElapsed())
						},
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.Warning.Fg,
						},
					},
				},
			}

		case AgentStatusReconnecting:
			return core.View{
				TestID: "agent-status-chip",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.RefreshCw(),
						Style: core.Style{
							Width:      core.Pt(13),
							Height:     core.Pt(13),
							Color:      p.TextMuted,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID: "agent-status-text",
						ContentFunc: func() string {
							_ = rev.Get()
							attempt, total := ed.AgentReconnectProgress()
							return fmt.Sprintf("Reconnecting · try %d of %d", attempt, total)
						},
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextMuted,
						},
					},
				},
			}

		case AgentStatusDisconnected:
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						TestID: "agent-status-chip",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Height:       core.Pt(24),
							Padding:      core.Symmetric(0, 10),
							Gap:          6,
							CornerRadius: 12,
							Background:   p.Removed.Fill,
							BorderWidth:  1,
							BorderColor:  p.Removed.Edge,
						},
						Children: []core.Node{
							core.SVGView{
								SVG: lucide.Unplug(),
								Style: core.Style{
									Width:      core.Pt(13),
									Height:     core.Pt(13),
									Color:      p.Removed.Fg,
									FlexShrink: 0,
								},
							},
							core.Text{
								TestID:  "agent-status-text",
								Content: "Disconnected",
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.Removed.Fg,
								},
							},
						},
					},
					core.View{
						TestID: "agent-status-reconnect",
						Role:   core.RoleButton,
						Label:  "Reconnect",
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Background:     p.Raised,
							BorderWidth:    1,
							BorderColor:    p.LineRaised,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.ReconnectAgent()
						},
						Children: []core.Node{
							core.Text{
								Content: "Reconnect",
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextSecondary,
								},
							},
						},
					},
					core.View{
						TestID: "agent-status-troubleshoot",
						Role:   core.RoleButton,
						Label:  "Troubleshoot",
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.TroubleshootAgentConnection()
						},
						Children: []core.Node{
							core.Text{
								Content: "Troubleshoot",
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextMuted,
								},
							},
						},
					},
				},
			}

		case AgentStatusLimitReached:
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						TestID: "agent-status-chip",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Height:       core.Pt(24),
							Padding:      core.Symmetric(0, 10),
							Gap:          6,
							CornerRadius: 12,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
						},
						Children: []core.Node{
							core.SVGView{
								SVG: lucide.Gauge(),
								Style: core.Style{
									Width:      core.Pt(13),
									Height:     core.Pt(13),
									Color:      p.Removed.Fg,
									FlexShrink: 0,
								},
							},
							core.Text{
								TestID: "agent-status-text",
								ContentFunc: func() string {
									_ = rev.Get()
									return fmt.Sprintf("Limit reached · resets %s", ed.AgentUsageResetLabel())
								},
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextSecondary,
								},
							},
						},
					},
					ui.Button{
						TestID:  "agent-status-usage",
						Text:    "Usage",
						Label:   "Open Usage and privacy settings",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Color:   func() core.Color { return p.Panel },
						TextStyle: core.Style{
							FontSize:   11.5,
							FontWeight: 500,
							Color:      p.Accent.Fg,
						},
						Style: core.Style{
							Height:       core.Pt(24),
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 6,
							FocusVisible: p.FocusRing(2),
						},
						OnPress: func() {
							ed.OpenSettingsPage(SettingsPageUsage)
						},
					},
				},
			}

		case AgentStatusOffline:
			return core.View{
				TestID: "agent-status-chip",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					Background:   p.Removed.Fill,
					BorderWidth:  1,
					BorderColor:  p.Removed.Edge,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.WifiOff(),
						Style: core.Style{
							Width:      core.Pt(13),
							Height:     core.Pt(13),
							Color:      p.Removed.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID:  "agent-status-text",
						Content: "Disconnected · offline",
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.Removed.Fg,
						},
					},
				},
			}
		default:
			return core.View{
				TestID: "agent-status-chip",
				Role:   core.RoleButton,
				Label:  "Connect an agent",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.OpenConnectDialog()
				},
				Children: []core.Node{
					core.View{
						TestID: "agent-status-dot",
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Changed.Fg,
							FlexShrink:   0,
						},
					},
					core.Text{
						TestID:  "agent-status-text",
						Content: "Connect agent",
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextSecondary,
						},
					},
				},
			}
		}
	})

	return core.View{
		TestID:     "agent-status-live",
		Role:       core.RoleStatus,
		LiveRegion: core.LiveRegionPolite,
		LabelFunc: func() string {
			_ = rev.Get()
			return ed.AgentLiveAnnouncement()
		},
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			FlexShrink: 0,
		},
		Children: []core.Node{chipContent},
	}
}

// AgentPanelView renders the Agent tab panel: 44 pt header with tabs and status chip,
// scene-grouped thread header and switcher popover, scrollable thread of persisted requests /
// live user turns / streamed steps / open requests or empty-thread orientation, and AgentComposerView
// (@-mentions, context chips, hover preview, and context meter).
func AgentPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	openReqRef := new(core.Ref)
	panelRef := new(core.Ref)
	switcherBtnRef := new(core.Ref)
	ed.SetAgentPanelRef(panelRef)
	cx.Cleanup(func() {
		if ed.focus.ctrl != nil && ed.focus.ctrl.agentPanelRef == panelRef {
			ed.SetAgentPanelRef(nil)
		}
	})
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionInspector, switcherBtnRef, func() bool {
			return ed.RightRailTab() == RightTabAgent
		})
	}

	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
		if ed.AgentSwitcherOpen() {
			ed.SetAgentSwitcherOpen(false)
		}
	})

	// Esc cancels "Let me point" viewport picking, or stops an active agent run when not editing text or in a modal.
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.CommandPaletteOpen() || ed.ConnectDialogOpen() || ed.SettingsOpen() || ed.AskInPlaceOpen() || ed.PermissionsDraftOpen() || ed.HistoryPreviewActive() || ed.AgentSwitcherOpen() || ed.MentionPickerOpen() || ed.ContextBreakdownOpen() {
			return
		}
		if ed.AgentPointingActive() {
			ed.CancelAgentPointing()
			return
		}
		if ed.AgentWorking() && !ed.IsTyping() {
			ed.StopAgentRun()
		}
	})
	focusSeqMemo := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.agent.focusRequestSeq
	})
	cx.Effect(func() {
		if focusSeqMemo.Get() > 0 {
			openReqRef.Focus()
		}
	})

	threadKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		fmt.Fprintf(&b, "%v|%s|%s|seq=%d|at=%d|rev=%s:%d|off=%v|lim=%v|q=%d|pt=%s|rv=%s|t=%d|th=%d:%s:%s|ct=%d:%d:%d:%s|",
			ed.SpecPalette(cx).Dark,
			ed.ResolvedLanguage(),
			ed.AgentStatus(),
			ed.AgentTurnSeq(),
			ed.agentTurnRev,
			ed.review.outcome,
			ed.review.uiRev,
			ed.agent.offline,
			ed.AgentWeeklyLimitReached(),
			len(ed.agent.queued),
			ed.agent.pointingTurnID,
			ed.agent.openReviewTurnID,
			len(ed.agent.turns),
			ed.agentThreads.rev,
			ed.agentThreads.activeThreadID,
			ed.agentThreads.expandedRequestID,
			len(ed.agentComposer.turns),
			ed.agentComposer.threadRev,
			len(ed.AgentThreadSummary()),
			ed.AgentThreadID(),
		)
		for _, t := range ed.agent.turns {
			fmt.Fprintf(&b, "%s:%s:%d:%d:%v:%v:%v:%s:%s:%d:%d:%d", t.ID, t.Status, len(t.Steps), len(t.Images), t.Done, t.Stopped, t.SkipPlan, t.ReviewState, t.CheckpointID, len(t.Summary), len(t.Reply), len(t.Error))
			if t.Partial != nil {
				fmt.Fprintf(&b, ":P(%v,%v,%v,%v)", t.Partial.Pointing, t.Partial.Kept, t.Partial.TryingAnother, t.Partial.HasPoint)
			}
			if t.Stuck != nil {
				fmt.Fprintf(&b, ":S(%s,%d)", t.Stuck.Resolution, len(t.Stuck.Failures))
			}
			if t.Lost != nil {
				fmt.Fprintf(&b, ":L(%s,%v,%v,%v)", t.Lost.Kind, t.Lost.Reconnected, t.Lost.AskResumePending, t.Lost.Resumed)
			}
			if t.StoppedState != nil {
				fmt.Fprintf(&b, ":ST(%v,%d)", t.StoppedState.Resumed, t.StoppedState.AtStep)
			}
			if t.Mismatch != nil {
				fmt.Fprintf(&b, ":M(%d,%v)", len(t.Mismatch.Rows), t.Mismatch.AskedWhy)
			}
			if t.Review != nil {
				fmt.Fprintf(&b, ":R(%d,%v,%v)", t.Review.Count(), t.Review.Kept, t.Review.Discarded)
			}
			b.WriteByte('|')
			for _, st := range t.Steps {
				fmt.Fprintf(&b, "%s=%s;", st.ID, st.Status)
			}
			if t.ModelError != nil {
				fmt.Fprintf(&b, "me=%d:%d:%d:%v:%s:%s;", t.ModelError.StatusCode, t.ModelError.RetryAttempt, t.ModelError.RetryTotal, t.ModelError.Retrying, t.ModelError.Headline, t.ModelError.AlternativeAgent)
			}
		}
		for _, q := range ed.agent.queued {
			fmt.Fprintf(&b, "q=%s:%v;", q.ID, q.Offline)
		}
		if ed.agent.openRequest != nil {
			fmt.Fprintf(&b, "req:%s|", ed.agent.openRequest.ID)
		}
		if ed.DataTableReviewPending() {
			fmt.Fprintf(&b, "dtrev:%d:%v", ed.dataTableReviewRev(), ed.DataTableReviewPaused())
		}
		return b.String()
	})

	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})

	tabBar := core.Dynamic(func(cx *core.Scope) core.Node {
		p := SpecPaletteFor(darkMemo.Get())
		return rightRailTabBar(cx, ed, rev, p, true)
	})

	bodyArea := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = threadKey.Get()
		p := ed.SpecPalette(cx)
		turns := ed.AgentTurns()
		composerTurns := ed.AgentThreadTurns()
		summary := ed.AgentThreadSummary()
		openReq := ed.AgentOpenRequest()
		hasReviewCard := ed.HasAgentPanelCard()
		dtPending := ed.DataTableReviewPending()
		queued := ed.AgentQueuedRequests()
		var limitQueued []AgentQueuedRequest
		for _, q := range queued {
			if !q.Offline {
				limitQueued = append(limitQueued, q)
			}
		}
		limitReached := ed.AgentWeeklyLimitReached()
		active := ed.ActiveAgentThread()
		turnSnap, hasPendingTurn := ed.CurrentAgentTurn()
		activeRev := ed.ActiveAgentReview()
		offlineNodes := agentOfflineAndQueueSection(ed, p)

		if active != nil && len(active.Requests) > 0 {
			return agentThreadConversationBody(cx, ed, rev, p, active, turns, openReq, openReqRef)
		}
		ready := ed.ReadyToReviewTurns()
		if summary != "" || len(turns) > 0 || len(composerTurns) > 0 || openReq != nil || len(ready) > 0 || hasReviewCard || dtPending || len(queued) > 0 || limitReached || hasPendingTurn || len(offlineNodes) > 0 || activeRev != nil {
			var items []core.Node
			if len(ready) > 0 {
				items = append(items, agentReadyToReviewBanner(ed, p, len(ready)))
			}
			if limitReached {
				items = append(items, agentLimitReachedCard(ed, p))
				if len(limitQueued) > 0 {
					items = append(items, agentQueuedRequestsView(ed, limitQueued, limitReached, p))
				}
			}
			if summary != "" {
				items = append(items, agentComposerSummaryCard(ed, p, summary))
			}
			for ti, turn := range turns {
				items = append(items, agentTurnView(ed, rev, ti, turn, p))
			}
			if len(composerTurns) > 0 {
				items = append(items, agentComposerThreadNodes(ed, p, composerTurns)...)
			}
			if hasReviewCard {
				items = append(items, agentReviewPanelCards(ed, p, turns)...)
			}
			if dtPending {
				items = append(items, agentPendingDataTableReviewCard(ed, p))
			}
			if hasPendingTurn {
				items = append(items, agentTurnSection(cx, ed, p, turnSnap))
			}
			if len(offlineNodes) > 0 {
				items = append(items, offlineNodes...)
			}
			if activeRev != nil {
				items = append(items, agentTurnReviewDrawer(ed, activeRev, p))
			}
			if openReq != nil && !strings.HasSuffix(openReq.ID, "-stuck") && !strings.HasSuffix(openReq.ID, "-reconnect-ask") {
				items = append(items, agentOpenRequestCard(ed, openReq, openReqRef, p))
			}
			if !limitReached && len(limitQueued) > 0 {
				items = append(items, agentQueuedRequestsView(ed, limitQueued, limitReached, p))
			}
			return ui.ScrollView{
				TestID: "agent-thread-scroll",
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
				},
				Children: []core.Node{
					core.View{
						TestID: "agent-thread-list",
						Style: core.Style{
							Gap:     14,
							Padding: core.All(16),
						},
						Children: items,
					},
				},
			}
		}
		return agentEmptyThreadBody(cx, ed, p)
	})

	return core.View{
		TestID: "agent-panel",
		Ref:    panelRef,
		Role:   core.RoleRegion,
		Label:  "Agent panel",
		StyleFunc: func() core.Style {
			p := SpecPaletteFor(darkMemo.Get())
			return core.Style{
				Width:       core.Pt(380),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				Overflow:    core.Hidden,
			}
		},
		Children: []core.Node{
			tabBar,
			agentThreadHeaderBar(cx, ed, rev, switcherBtnRef),
			core.View{
				TestID: "editor-agent-panel",
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					bodyArea,
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.AgentSwitcherOpen()
						},
						Then: func(cx *core.Scope) core.Node {
							return agentThreadSwitcherPopover(cx, ed, rev)
						},
					},
				},
			},
			AgentComposerView(cx, ed, rev),
		},
	}
}

func agentReadyToReviewBanner(ed *Editor, p SpecPalette, count int) core.Node {
	return core.View{
		TestID: "agent-ready-to-review-banner",
		Role:   core.RoleStatus,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			CornerRadius: 10,
			Background:   p.Accent.Fill,
			BorderWidth:  1,
			BorderColor:  p.Accent.Edge,
			Padding:      core.Symmetric(8, 12),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(8),
					Height:       core.Pt(8),
					CornerRadius: 4,
					Background:   p.Accent.Fg,
					FlexShrink:   0,
				},
			},
			core.Text{
				Content: ed.T("agent.automationReadyBanner", l10n.Param("count", fmt.Sprintf("%d", count))),
				Style: core.Style{
					FontSize:   12,
					FontWeight: 600,
					Color:      p.TextPrimary,
				},
			},
		},
	}
}

func agentEmptyStateCard(ed *Editor, p SpecPalette) core.Node {
	connected := ed.AgentStatus() == AgentStatusConnected
	heading := "Ask the agent to edit this scene"
	body := "Describe what you want to change. Every edit runs through the editor's MCP server so you can see it live and undo it in one step."
	if !connected {
		heading = "Connect an agent to start"
		body = "Connect Claude Code, Codex, or any MCP client on this computer. Manual editing tools keep working in every state."
	}
	children := []core.Node{
		core.Text{
			Content: heading,
			Style: core.Style{
				FontSize:   13,
				FontWeight: 600,
				Color:      p.TextPrimary,
			},
		},
		core.Text{
			Content: body,
			Style: core.Style{
				FontSize: 12.5,
				Color:    p.TextMuted,
			},
		},
	}
	if !connected {
		children = append(children, core.View{
			TestID: "agent-empty-connect",
			Role:   core.RoleButton,
			Label:  "Connect an agent",
			Style: core.Style{
				AlignSelf:      core.AlignStart,
				Height:         core.Pt(30),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     p.TextPrimary,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.OpenConnectDialog()
			},
			Children: []core.Node{
				core.Text{
					Content: "Connect an agent",
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.OnTextPrimary,
					},
				},
			},
		})
	}
	return core.View{
		TestID: "agent-empty-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(14),
			Gap:          8,
		},
		Children: children,
	}
}

func agentTurnView(ed *Editor, rev *core.Signal[uint64], turnIdx int, turn AgentTurn, p SpecPalette) core.Node {
	var children []core.Node
	if turn.Automation {
		stateLabel, stateFg, stateBg, stateEdge := automationReviewBadgeStyle(ed, turn.ReviewState, p)
		headerMeta := ed.T("agent.automationBadge")
		if turn.TriggerSummary != "" {
			headerMeta += " · " + turn.TriggerSummary
		}
		children = append(children, core.View{
			TestID: fmt.Sprintf("agent-turn-automation-%d", turnIdx),
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
			},
			Children: []core.Node{
				core.Text{
					Content: headerMeta,
					Style: core.Style{
						FontSize:   11,
						FontWeight: 600,
						Color:      p.Agent.Fg,
						FlexGrow:   1,
						FlexShrink: 1,
					},
				},
				core.View{
					TestID: fmt.Sprintf("agent-turn-review-state-%d", turnIdx),
					Style: core.Style{
						Height:         core.Pt(20),
						Padding:        core.Symmetric(0, 8),
						CornerRadius:   10,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Background:     stateBg,
						BorderWidth:    1,
						BorderColor:    stateEdge,
						FlexShrink:     0,
					},
					Children: []core.Node{
						core.Text{
							Content: stateLabel,
							Style: core.Style{
								FontSize:   10.5,
								FontWeight: 600,
								Color:      stateFg,
							},
						},
					},
				},
			},
		})
		if turn.CheckpointID != "" {
			children = append(children, core.Text{
				TestID:  fmt.Sprintf("agent-turn-checkpoint-%d", turnIdx),
				Content: ed.T("agent.automationCheckpoint", l10n.Param("id", turn.CheckpointID)),
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextFaint,
				},
			})
		}
	}
	if turn.External {
		label := "Terminal MCP session"
		if turn.AgentName != "" {
			label = fmt.Sprintf("From terminal · %s", turn.AgentName)
		}
		children = append(children, core.Text{
			TestID:  fmt.Sprintf("agent-turn-external-%d", turnIdx),
			Content: label,
			Style: core.Style{
				FontSize:   11,
				FontWeight: 600,
				Color:      p.TextFaint,
			},
		})
	} else if turn.Prompt != "" || len(turn.Images) > 0 {
		var bubbleChildren []core.Node
		if imgRow := agentTurnImagesRowView(turnIdx, turn.Images, p); imgRow != nil {
			bubbleChildren = append(bubbleChildren, imgRow)
		}
		if turn.Prompt != "" {
			bubbleChildren = append(bubbleChildren, core.Text{
				Content: turn.Prompt,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			})
		}
		if chips := agentTurnContextChips(ed, turn, p); len(chips) > 0 {
			bubbleChildren = append(bubbleChildren, core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					Gap:        4,
				},
				Children: chips,
			})
		}
		children = append(children, core.View{
			TestID: fmt.Sprintf("agent-turn-prompt-%d", turnIdx),
			Style: core.Style{
				AlignSelf:    core.AlignEnd,
				MaxWidth:     core.Pt(296),
				Padding:      core.Symmetric(8, 12),
				Gap:          8,
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: bubbleChildren,
		})
	}

	// Stopped card renders DONE and NOT DONE sections directly inside the card; avoid duplicating steps above it.
	if len(turn.Steps) > 0 && turn.StoppedState == nil {
		stepNodes := make([]core.Node, 0, len(turn.Steps))
		for si, step := range turn.Steps {
			stepNodes = append(stepNodes, agentStepRow(turnIdx, si, step, p))
		}
		stepStyle := core.Style{
			Gap: 8,
		}
		if turn.Lost != nil && turn.Lost.Kind == AgentLostConnection && !turn.Lost.Resumed {
			stepStyle.Opacity = core.Alpha(0.75)
		}
		children = append(children, core.View{
			TestID:   fmt.Sprintf("agent-turn-steps-%d", turnIdx),
			Style:    stepStyle,
			Children: stepNodes,
		})
	}

	replyText := turn.Summary
	if replyText == "" {
		replyText = turn.Reply
	}
	if replyText != "" {
		children = append(children, core.View{
			TestID: "agent-turn-reply-" + turn.ID,
			Children: []core.Node{
				core.Text{
					TestID:  fmt.Sprintf("agent-turn-summary-%d", turnIdx),
					Content: replyText,
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextSecondary,
					},
				},
			},
		})
	} else if turn.Source == "in_place" && !turn.Done && !turn.Stopped && turn.Error == "" {
		statusText := ed.T("agentPanel.statusPending")
		if turn.Status == "running" {
			statusText = ed.T("agentPanel.statusRunning")
		}
		children = append(children, core.View{
			TestID: "agent-turn-status-" + turn.ID,
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(7),
						Height:       core.Pt(7),
						CornerRadius: 4,
						Background:   p.Agent.Fg,
						FlexShrink:   0,
					},
				},
				core.Text{
					Content: statusText,
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextFaint,
					},
				},
			},
		})
	}

	if turn.Partial != nil {
		children = append(children, agentPartialSection(ed, turnIdx, turn, p)...)
	}
	if turn.Stuck != nil {
		children = append(children, agentStuckSection(ed, rev, turnIdx, turn, p)...)
	}
	if turn.Lost != nil {
		children = append(children, agentLostSection(ed, turnIdx, turn, p)...)
	}
	if turn.StoppedState != nil {
		children = append(children, agentStoppedSection(ed, turnIdx, turn, p)...)
	} else if turn.Stopped {
		children = append(children, core.Text{
			TestID:  fmt.Sprintf("agent-turn-stopped-%d", turnIdx),
			Content: "Stopped · edits already made stay undoable.",
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}
	if turn.Mismatch != nil {
		children = append(children, agentMismatchSection(ed, turnIdx, turn, p)...)
	}

	if turn.ModelError != nil {
		children = append(children, agentModelErrorCard(ed, turnIdx, turn.ID, turn.ModelError, p))
	} else if turn.Error != "" && turn.Lost == nil {
		children = append(children, core.View{
			TestID: fmt.Sprintf("agent-turn-error-%d", turnIdx),
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Removed.Fill,
				BorderWidth:  1,
				BorderColor:  p.Removed.Edge,
				Padding:      core.All(12),
				Gap:          8,
			},
			Children: []core.Node{
				core.Text{
					Content: turn.Error,
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.Removed.Fg,
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       8,
					},
					Children: []core.Node{
						core.View{
							Role:  core.RoleButton,
							Label: "Troubleshoot",
							Style: core.Style{
								Height:         core.Pt(28),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   8,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Background:     p.Raised,
								BorderWidth:    1,
								BorderColor:    p.LineRaised,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.TroubleshootAgentConnection()
							},
							Children: []core.Node{
								core.Text{
									Content: "Troubleshoot",
									Style: core.Style{
										FontSize: 12,
										Color:    p.TextSecondary,
									},
								},
							},
						},
					},
				},
			},
		})
	}

	if turn.Automation && turn.ReviewState == AgentReviewReady {
		tid := turn.ID
		cpID := turn.CheckpointID
		reviewBtns := []core.Node{
			settingsSpecButton(p, fmt.Sprintf("agent-turn-keep-%d", turnIdx), ed.T("agent.automationKeep"), ed.T("agent.automationKeep"), true, nil, func() {
				ed.report(ed.KeepAgentTurnReview(tid))
			}),
			settingsSpecButton(p, fmt.Sprintf("agent-turn-dismiss-%d", turnIdx), ed.T("agent.automationDismiss"), ed.T("agent.automationDismiss"), false, nil, func() {
				ed.report(ed.DismissAgentTurnReview(tid))
			}),
		}
		if cpID != "" {
			reviewBtns = append(reviewBtns, settingsSpecButton(p, fmt.Sprintf("agent-turn-restore-%d", turnIdx), ed.T("agent.automationRestoreCheckpoint"), ed.T("agent.automationRestoreCheckpoint"), false, nil, func() {
				ed.report(ed.RestoreAutomationCheckpoint(cpID))
			}))
		}
		children = append(children, core.View{
			TestID: fmt.Sprintf("agent-turn-review-actions-%d", turnIdx),
			Style: core.Style{
				Direction: core.Row,
				Wrap:      core.Wrap,
				Gap:       8,
			},
			Children: reviewBtns,
		})
	}

	return core.View{
		TestID: fmt.Sprintf("agent-turn-%d", turnIdx),
		Style: core.Style{
			Gap: 10,
		},
		Children: children,
	}
}

func agentTurnContextChips(ed *Editor, t AgentTurn, p SpecPalette) []core.Node {
	var chips []core.Node
	if len(t.Context.SelectionIDs) > 0 {
		selLabel := strings.Join(t.Context.SelectionNames, ", ")
		if selLabel == "" {
			selLabel = strings.Join(t.Context.SelectionIDs, ", ")
		}
		if len(t.Context.SelectionIDs) > 1 {
			selLabel = fmt.Sprintf("%d · %s", len(t.Context.SelectionIDs), selLabel)
		}
		chips = append(chips, agentContextChip(p, "agent-turn-selection-"+t.ID, p.Added.Fg, selLabel))
	}
	sceneChip := t.Context.ScenePath
	if sceneChip == "" {
		sceneChip = t.Context.SceneName
	}
	if sceneChip != "" {
		chips = append(chips, agentContextChip(p, "agent-turn-scene-"+t.ID, p.Accent.Fg, sceneChip))
	}
	if len(t.Context.ViewportPNG) > 0 {
		imgLabel := ed.T("agentPanel.viewportImage")
		if t.Context.ViewportWidth > 0 && t.Context.ViewportHeight > 0 {
			imgLabel = fmt.Sprintf("%s · %d×%d", imgLabel, t.Context.ViewportWidth, t.Context.ViewportHeight)
		}
		chips = append(chips, agentContextChip(p, "agent-turn-image-"+t.ID, p.TextMuted, imgLabel))
	}
	if t.SkipPlan {
		chips = append(chips, core.View{
			TestID: "agent-turn-skip-plan-" + t.ID,
			Style: core.Style{
				Height:         core.Pt(20),
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   6,
				Background:     p.Agent.Fill,
				BorderWidth:    1,
				BorderColor:    p.Agent.Edge,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content:  ed.T("agentPanel.skipPlanBadge"),
					MaxLines: 1,
					Style: core.Style{
						FontSize:   10.5,
						FontWeight: core.FontWeightMedium,
						Color:      p.Agent.Fg,
					},
				},
			},
		})
	}
	return chips
}

func agentContextChip(p SpecPalette, testID string, dotColor core.Color, label string) core.Node {
	return core.View{
		TestID: testID,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          5,
			Height:       core.Pt(20),
			Padding:      core.Symmetric(0, 6),
			CornerRadius: 6,
			Background:   p.Inset,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(6),
					Height:       core.Pt(6),
					CornerRadius: 2,
					Background:   dotColor,
					FlexShrink:   0,
				},
			},
			core.Text{
				Content:  label,
				MaxLines: 1,
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextMuted,
				},
			},
		},
	}
}

func automationReviewBadgeStyle(ed *Editor, state AgentReviewState, p SpecPalette) (label string, fg, bg, edge core.Color) {
	switch state {
	case AgentReviewRunning:
		return ed.T("agent.automationState.running"), p.Agent.Fg, p.Agent.Fill, p.Agent.Edge
	case AgentReviewNeedsYou:
		return ed.T("agent.automationState.needsYou"), p.Warning.Fg, p.Warning.Fill, p.Warning.Edge
	case AgentReviewReady:
		return ed.T("agent.automationState.ready"), p.Accent.Fg, p.Accent.Fill, p.Accent.Edge
	case AgentReviewKept:
		return ed.T("agent.automationState.kept"), p.Added.Fg, p.Added.Fill, p.Added.Edge
	case AgentReviewDismissed:
		return ed.T("agent.automationState.dismissed"), p.TextFaint, p.Raised, p.LineRaised
	case AgentReviewFailed:
		return ed.T("agent.automationState.failed"), p.Removed.Fg, p.Removed.Fill, p.Removed.Edge
	default:
		return ed.T("agent.automationState.ready"), p.Accent.Fg, p.Accent.Fill, p.Accent.Edge
	}
}

func agentModelErrorCard(ed *Editor, turnIdx int, turnID string, me *AgentModelErrorState, p SpecPalette) core.Node {
	actions := []core.Node{
		ui.Button{
			TestID:  fmt.Sprintf("agent-turn-retry-%d", turnIdx),
			Text:    "Retry now",
			Label:   "Retry now",
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return p.Raised },
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      p.TextSecondary,
			},
			Style: core.Style{
				Height:       core.Pt(30),
				Padding:      core.Symmetric(0, 12),
				CornerRadius: 8,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				FocusVisible: p.FocusRing(2),
			},
			OnPress: func() {
				ed.report(ed.RetryAgentTurn(turnID))
			},
		},
	}
	if me.AlternativeAgent != "" && me.AlternativeRoute != "" && me.AlternativeRoute != AgentRouteNone {
		switchLabel := "Use " + me.AlternativeAgent
		actions = append(actions, ui.Button{
			TestID:  fmt.Sprintf("agent-turn-switch-%d", turnIdx),
			Text:    switchLabel,
			Label:   switchLabel,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return p.Removed.Fill },
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      p.TextMuted,
			},
			Style: core.Style{
				Height:       core.Pt(30),
				Padding:      core.Symmetric(0, 12),
				CornerRadius: 8,
				FocusVisible: p.FocusRing(2),
			},
			OnPress: func() {
				ed.report(ed.SwitchAgentTurnToAlternative(turnID))
			},
		})
	}
	return core.View{
		TestID: fmt.Sprintf("agent-turn-model-error-%d", turnIdx),
		Role:   core.RoleAlert,
		Label:  me.Headline,
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Removed.Fill,
			BorderWidth:  1,
			BorderColor:  p.Removed.Edge,
			Padding:      core.All(12),
			Gap:          8,
		},
		Children: []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("agent-turn-model-error-title-%d", turnIdx),
				Content: me.Headline,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 600,
					Color:      p.Removed.Fg,
				},
			},
			core.Text{
				TestID:  fmt.Sprintf("agent-turn-model-error-body-%d", turnIdx),
				Content: me.Body(),
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			},
			core.View{
				Style: core.Style{
					Direction: core.Row,
					Wrap:      core.Wrap,
					Gap:       8,
				},
				Children: actions,
			},
		},
	}
}

func agentLimitReachedCard(ed *Editor, p SpecPalette) core.Node {
	u := ed.AgentUsage()
	limit := ed.WeeklyRequestLimit()
	return core.View{
		TestID: "agent-limit-card",
		Role:   core.RoleRegion,
		Label:  "Weekly limit reached",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(12),
			Gap:          8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Gauge(),
						Style: core.Style{
							Width:      core.Pt(14),
							Height:     core.Pt(14),
							Color:      p.Removed.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						Content: fmt.Sprintf("Limit reached · resets %s", ed.AgentUsageResetLabel()),
						Style: core.Style{
							FontSize:   13,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
				},
			},
			core.Text{
				Content: fmt.Sprintf("This week's limit (%d of %d requests) is used. Requests wait in the queue until the reset or until you raise the limit; only you can.", u.WeeklyRequests, limit),
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			},
			core.View{
				Style: core.Style{
					Direction: core.Row,
					Gap:       8,
				},
				Children: []core.Node{
					settingsSpecButton(p, "agent-limit-open-usage", "Usage and privacy", "Open Usage and privacy settings", false, nil, func() {
						ed.OpenSettingsPage(SettingsPageUsage)
					}),
				},
			},
		},
	}
}

func agentQueuedRequestsView(ed *Editor, queued []AgentQueuedRequest, limitReached bool, p SpecPalette) core.Node {
	rows := make([]core.Node, 0, len(queued)+2)
	rows = append(rows, core.Text{
		TestID:  "agent-queued-heading",
		Content: fmt.Sprintf("QUEUED · %d", len(queued)),
		Style: core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.7,
			Color:         p.TextFaint,
		},
	})
	for i, q := range queued {
		item := q
		idx := i
		rowChildren := []core.Node{
			core.View{
				Style: core.Style{
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					Gap:        2,
				},
				Children: []core.Node{
					core.Text{
						TestID:  fmt.Sprintf("agent-queued-text-%d", idx),
						Content: item.Prompt,
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextSecondary,
						},
					},
					core.Text{
						Content: fmt.Sprintf("queued %s", item.QueuedAt.Format("15:04")),
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextFaint,
						},
					},
				},
			},
		}
		if !limitReached {
			rowChildren = append(rowChildren, settingsSpecButton(p, fmt.Sprintf("agent-queued-send-%d", idx), "Send", "Send queued request", true, nil, func() {
				ed.report(ed.SendQueuedAgentRequest(item.ID))
			}))
		}
		rowChildren = append(rowChildren, ui.Button{
			TestID:  fmt.Sprintf("agent-queued-remove-%d", idx),
			Label:   fmt.Sprintf("Remove %q from queue", item.Prompt),
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return p.Raised },
			Style: core.Style{
				Width:        core.Pt(28),
				Height:       core.Pt(28),
				CornerRadius: 6,
				FocusVisible: p.FocusRing(2),
			},
			OnPress: func() {
				ed.RemoveQueuedAgentRequest(item.ID)
			},
			Icon: lucide.X,
		})
		rows = append(rows, core.View{
			TestID: fmt.Sprintf("agent-queued-item-%d", idx),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Edges{Top: 8, Right: 8, Bottom: 8, Left: 12},
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: rowChildren,
		})
	}
	rows = append(rows, core.Text{
		Content: "When the limit resets or you raise it, each queued request waits for you to send it.",
		Style: core.Style{
			FontSize: 12,
			Color:    p.TextFaint,
		},
	})
	return core.View{
		TestID: "agent-queued-section",
		Style: core.Style{
			Gap: 8,
		},
		Children: rows,
	}
}

func agentStepRow(turnIdx, stepIdx int, step AgentStep, p SpecPalette) core.Node {
	var badge core.Node
	textCol := p.TextMuted
	switch step.Status {
	case AgentStepDone:
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.Added.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Check(),
					Style: core.Style{
						Width:  core.Pt(10),
						Height: core.Pt(10),
						Color:  p.Added.Fg,
					},
				},
			},
		}
	case AgentStepRunning:
		textCol = p.TextPrimary
		badge = core.View{
			Style: core.Style{
				Width:        core.Pt(16),
				Height:       core.Pt(16),
				CornerRadius: 8,
				BorderWidth:  2,
				BorderColor:  p.Agent.Fg,
				FlexShrink:   0,
			},
		}
	case AgentStepWaiting:
		textCol = p.TextPrimary
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.Warning.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Pause(),
					Style: core.Style{
						Width:  core.Pt(10),
						Height: core.Pt(10),
						Color:  p.Warning.Fg,
					},
				},
			},
		}
	case AgentStepWarning:
		textCol = p.TextSecondary
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.Warning.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.Text{
					Content: "!",
					Style: core.Style{
						FontSize:   10,
						FontWeight: 700,
						Color:      p.Warning.Fg,
					},
				},
			},
		}
	case AgentStepInterrupted:
		textCol = p.TextMuted
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				BorderWidth:    1.5,
				BorderColor:    p.TextFaint,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Pause(),
					Style: core.Style{
						Width:  core.Pt(9),
						Height: core.Pt(9),
						Color:  p.TextFaint,
					},
				},
			},
		}
	case AgentStepFailed:
		textCol = p.TextSecondary
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.Removed.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.X(),
					Style: core.Style{
						Width:  core.Pt(10),
						Height: core.Pt(10),
						Color:  p.Removed.Fg,
					},
				},
			},
		}
	default:
		textCol = p.TextFaint
		badge = core.View{
			Style: core.Style{
				Width:        core.Pt(16),
				Height:       core.Pt(16),
				CornerRadius: 8,
				BorderWidth:  1.5,
				BorderColor:  p.LineRaised,
				FlexShrink:   0,
			},
		}
	}

	label := step.Title
	if label == "" {
		label = step.ToolName
	}
	if step.Detail != "" {
		label = label + " · " + step.Detail
	}
	return core.View{
		TestID: fmt.Sprintf("agent-step-%d-%d", turnIdx, stepIdx),
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
		},
		Children: []core.Node{
			badge,
			core.Text{
				Content: label,
				Style: core.Style{
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					FontSize:   12.5,
					Color:      textCol,
				},
			},
		},
	}
}

func agentOpenRequestCard(ed *Editor, req *AgentOpenRequest, ref *core.Ref, p SpecPalette) core.Node {
	return core.View{
		TestID:    "agent-open-request",
		Ref:       ref,
		Role:      core.RoleDialog,
		Label:     req.Title,
		Focusable: func() bool { return true },
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Warning.Fill,
			BorderWidth:  1,
			BorderColor:  p.Warning.Edge,
			Padding:      core.All(12),
			Gap:          8,
			FocusVisible: p.FocusRing(2),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Clock(),
						Style: core.Style{
							Width:  core.Pt(14),
							Height: core.Pt(14),
							Color:  p.Warning.Fg,
						},
					},
					core.Text{
						Content: req.Title,
						Style: core.Style{
							FontSize:   13,
							FontWeight: 600,
							Color:      p.Warning.Fg,
						},
					},
				},
			},
			core.Text{
				Content: req.Body,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			},
		},
	}
}

// ConnectAgentDialogView renders the "Connect an agent" modal dialog matching docs/design/editor/Connect.dc.html
// using exclusively Spec.dc.html tokens: three choices, the one-time `go tool game mcp install` command with Copy,
// a live waiting state that turns to connected when the first MCP client attaches, four starting permission checkboxes,
// Later, Done, and targeted step troubleshooting when reopened from Disconnected.
func ConnectAgentDialogView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := SpecPaletteFor(darkMemo.Get())
		choiceCard := func(testID string, mode AgentMode, name, desc, badge string) core.Node {
			return core.View{
				TestID: testID,
				Role:   core.RoleRadio,
				Label:  name,
				StyleFunc: func() core.Style {
					_ = rev.Get()
					selected := ed.ConnectDialogChoice() == mode
					bg := p.Raised
					borderCol := p.LineRaised
					borderW := core.Points(1)
					if selected {
						bg = p.Accent.Fill
						borderCol = p.Accent.Fg
						borderW = 1.5
					}
					return core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          14,
						Padding:      core.Symmetric(14, 16),
						CornerRadius: 12,
						Background:   bg,
						BorderWidth:  borderW,
						BorderColor:  borderCol,
						FocusVisible: p.FocusRing(2),
					}
				},
				OnPress: func(core.PressEvent) {
					ed.SetConnectChoice(mode)
				},
				Children: []core.Node{
					// Radio dot indicator
					core.View{
						StyleFunc: func() core.Style {
							_ = rev.Get()
							selected := ed.ConnectDialogChoice() == mode
							if selected {
								return core.Style{
									Width:        core.Pt(16),
									Height:       core.Pt(16),
									CornerRadius: 8,
									BorderWidth:  5,
									BorderColor:  p.Accent.Fg,
									Background:   p.Panel,
									FlexShrink:   0,
								}
							}
							return core.Style{
								Width:        core.Pt(16),
								Height:       core.Pt(16),
								CornerRadius: 8,
								BorderWidth:  1.5,
								BorderColor:  p.TextFaint,
								FlexShrink:   0,
							}
						},
					},
					core.View{
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							Gap:        3,
						},
						Children: []core.Node{
							core.Text{
								Content: name,
								Style: core.Style{
									FontSize:   13,
									FontWeight: 500,
									Color:      p.TextPrimary,
								},
							},
							core.Text{
								Content: desc,
								Style: core.Style{
									FontSize: 12,
									Color:    p.TextFaint,
								},
							},
						},
					},
					core.Show{
						When: func() bool { return badge != "" },
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								Style: core.Style{
									Height:         core.Pt(22),
									Padding:        core.Symmetric(0, 8),
									CornerRadius:   11,
									Background:     p.Added.Fill,
									BorderWidth:    1,
									BorderColor:    p.Added.Edge,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FlexShrink:     0,
								},
								Children: []core.Node{
									core.Text{
										Content: badge,
										Style: core.Style{
											FontSize:   11,
											FontWeight: 500,
											Color:      p.Added.Fg,
										},
									},
								},
							}
						},
					},
				},
			}
		}

		permCheckbox := func(testID, label string, get func(AgentPermissions) bool, set func(*AgentPermissions, bool)) core.Node {
			return core.View{
				Style: core.Style{
					FlexGrow:   1,
					FlexBasis:  core.Pt(0),
					MinWidth:   core.Pt(0),
					AlignItems: core.AlignStart,
				},
				Children: []core.Node{
					ui.Checkbox{
						TestID: testID,
						Text:   label,
						Checked: func() bool {
							_ = rev.Get()
							return get(ed.ConnectDialogPermissions())
						},
						OnChange: func(checked bool) {
							perms := ed.ConnectDialogPermissions()
							set(&perms, checked)
							ed.SetConnectPermissions(perms)
						},
					},
				},
			}
		}

		body := core.View{
			TestID: "connect-dialog-body",
			Style: core.Style{
				Padding:    core.All(28),
				Gap:        20,
				Background: p.Panel,
			},
			Children: []core.Node{
				// Title and intro
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							TestID:  "connect-dialog-title",
							Content: "Connect an agent",
							Style: core.Style{
								FontSize:   20,
								FontWeight: 600,
								Color:      p.TextPrimary,
							},
						},
						core.Text{
							Content: "The editor serves this project over MCP: the agent reads and edits the scene, sees the viewport, runs checks and playtests. Pick how it reaches the agent; you can change this any time.",
							Style: core.Style{
								FontSize: 13,
								Color:    p.TextFaint,
							},
						},
					},
				},
				// Troubleshoot banner when reopened from Disconnected at a failed step
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.ConnectDialogTroubleshooting()
					},
					Then: func(cx *core.Scope) core.Node {
						return core.View{
							TestID: "connect-troubleshoot-banner",
							Style: core.Style{
								CornerRadius: 12,
								Background:   p.Warning.Fill,
								BorderWidth:  1,
								BorderColor:  p.Warning.Edge,
								Padding:      core.Symmetric(10, 14),
								Gap:          4,
							},
							Children: []core.Node{
								core.Text{
									TestID: "connect-troubleshoot-step",
									ContentFunc: func() string {
										_ = rev.Get()
										switch ed.ConnectDialogStep() {
										case ConnectStepInstall:
											return "Troubleshooting · Step 1: Project MCP configuration"
										case ConnectStepAPIKey:
											return "Troubleshooting · API key configuration"
										default:
											return "Troubleshooting · Waiting for MCP connection"
										}
									},
									Style: core.Style{
										FontSize:   12,
										FontWeight: 600,
										Color:      p.Warning.Fg,
									},
								},
								core.Text{
									ContentFunc: func() string {
										_ = rev.Get()
										if cause := ed.AgentFailedCause(); cause != "" {
											return cause
										}
										if ed.ConnectDialogStep() == ConnectStepAPIKey {
											return "The built-in API key could not be verified. Check your key in the system keychain or paste a new key."
										}
										return "The MCP connection stopped answering. Confirm the project configuration and reconnect your agent."
									},
									Style: core.Style{
										FontSize: 12,
										Color:    p.TextSecondary,
									},
								},
							},
						}
					},
				},
				// Three choices (radio group)
				core.View{
					TestID: "connect-choices",
					Role:   core.RoleRadioGroup,
					Label:  "How the editor reaches the agent",
					Style:  core.Style{Gap: 8},
					Children: []core.Node{
						choiceCard("connect-choice-local", AgentModeLocal, "An agent on this computer", "Claude Code, Codex or any MCP client you already use", "Recommended"),
						choiceCard("connect-choice-key", AgentModeAPIKey, "Built in, with your API key", "The editor talks to the model itself", ""),
						choiceCard("connect-choice-none", AgentModeNone, "No agent", "Edit by hand; the Agent panel stays closed", ""),
					},
				},
				// Local MCP detail card
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.ConnectDialogChoice() == AgentModeLocal
					},
					Then: func(cx *core.Scope) core.Node {
						return core.View{
							TestID: "connect-local-panel",
							StyleFunc: func() core.Style {
								_ = rev.Get()
								borderCol := p.LineRaised
								if ed.ConnectDialogTroubleshooting() && (ed.ConnectDialogStep() == ConnectStepInstall || ed.ConnectDialogStep() == ConnectStepWaiting) {
									borderCol = p.Accent.Fg
								}
								return core.Style{
									CornerRadius: 12,
									Background:   p.Bg,
									BorderWidth:  1,
									BorderColor:  borderCol,
									Padding:      core.All(14),
									Gap:          10,
								}
							},
							Children: []core.Node{
								core.Text{
									Content: "Run this once in the project folder; the agent then finds the editor by itself:",
									Style: core.Style{
										FontSize: 12.5,
										Color:    p.TextMuted,
									},
								},
								core.View{
									TestID: "connect-install-box",
									StyleFunc: func() core.Style {
										_ = rev.Get()
										borderCol := p.LineRaised
										if ed.ConnectDialogTroubleshooting() && ed.ConnectDialogStep() == ConnectStepInstall {
											borderCol = p.Warning.Fg
										}
										return core.Style{
											Direction:    core.Row,
											AlignItems:   core.AlignCenter,
											Height:       core.Pt(36),
											Padding:      core.Symmetric(0, 12),
											Gap:          10,
											CornerRadius: 8,
											Background:   p.Raised,
											BorderWidth:  1,
											BorderColor:  borderCol,
										}
									},
									Children: []core.Node{
										core.Text{
											TestID:  "connect-install-command",
											Content: AgentInstallCommand,
											Style: core.Style{
												FlexGrow:   1,
												FontFamily: core.FontMonospace,
												FontSize:   12,
												Color:      p.TextSecondary,
											},
										},
										core.View{
											TestID: "connect-copy-command",
											Role:   core.RoleButton,
											Label:  "Copy install command",
											Style: core.Style{
												Height:         core.Pt(24),
												Padding:        core.Symmetric(0, 10),
												CornerRadius:   6,
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
												Background:     p.LineRaised,
												FocusVisible:   p.FocusRing(2),
											},
											OnPress: func(core.PressEvent) {
												ed.CopyConnectInstallCommand()
											},
											Children: []core.Node{
												core.Text{
													ContentFunc: func() string {
														_ = rev.Get()
														if ed.ConnectDialogCopied() {
															return "Copied"
														}
														return "Copy"
													},
													Style: core.Style{
														FontSize: 12,
														Color:    p.TextSecondary,
													},
												},
											},
										},
									},
								},
								core.View{
									TestID:     "connect-waiting-status",
									Role:       core.RoleStatus,
									LiveRegion: core.LiveRegionPolite,
									StyleFunc: func() core.Style {
										_ = rev.Get()
										var bg, border core.Color
										var bw core.Points
										var pad core.Edges
										if ed.ConnectDialogTroubleshooting() && ed.ConnectDialogStep() == ConnectStepWaiting && len(ed.AgentClients()) == 0 {
											bg = p.Warning.Fill
											border = p.Warning.Edge
											bw = 1
											pad = core.Symmetric(6, 10)
										}
										return core.Style{
											Direction:    core.Row,
											AlignItems:   core.AlignCenter,
											Gap:          8,
											CornerRadius: 8,
											Background:   bg,
											BorderWidth:  bw,
											BorderColor:  border,
											Padding:      pad,
										}
									},
									Children: []core.Node{
										core.View{
											TestID: "connect-waiting-dot",
											StyleFunc: func() core.Style {
												_ = rev.Get()
												col := p.Changed.Fg
												if len(ed.AgentClients()) > 0 {
													col = p.Added.Fg
												}
												return core.Style{
													Width:        core.Pt(7),
													Height:       core.Pt(7),
													CornerRadius: 4,
													Background:   col,
													FlexShrink:   0,
												}
											},
										},
										core.Text{
											TestID: "connect-waiting-text",
											ContentFunc: func() string {
												_ = rev.Get()
												if clients := ed.AgentClients(); len(clients) > 0 {
													return fmt.Sprintf("Connected · %s", clients[0].HoverLabel())
												}
												return "Waiting for an agent to connect…"
											},
											StyleFunc: func() core.Style {
												_ = rev.Get()
												col := p.TextFaint
												if len(ed.AgentClients()) > 0 {
													col = p.Added.Fg
												}
												return core.Style{
													FontSize: 12.5,
													Color:    col,
												}
											},
										},
									},
								},
							},
						}
					},
				},
				// Built-in API key detail card
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.ConnectDialogChoice() == AgentModeAPIKey
					},
					Then: func(cx *core.Scope) core.Node {
						return core.View{
							TestID: "connect-key-panel",
							Style: core.Style{
								CornerRadius: 12,
								Background:   p.Bg,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
								Padding:      core.All(14),
								Gap:          10,
							},
							Children: []core.Node{
								core.Text{
									Content: "API key",
									Style: core.Style{
										FontSize: 12.5,
										Color:    p.TextMuted,
									},
								},
								ui.Input{
									TestID:      "connect-api-key-input",
									Label:       "API key",
									Placeholder: "Paste a key",
									Masked:      true,
									Value: func() string {
										_ = rev.Get()
										return ed.ConnectDialogAPIKey()
									},
									OnChange: func(text string) {
										ed.SetConnectAPIKey(text)
									},
									Style: core.Style{
										Height:           core.Pt(36),
										CornerRadius:     8,
										Background:       p.Raised,
										BorderWidth:      1,
										BorderColor:      p.LineRaised,
										Padding:          core.Symmetric(0, 12),
										FontSize:         12.5,
										Color:            p.TextSecondary,
										PlaceholderColor: p.TextFaint,
										FocusVisible:     p.FocusRing(2),
									},
								},
								core.Text{
									Content: "Kept in the system keychain, never in the project files.",
									Style: core.Style{
										FontSize: 12,
										Color:    p.TextFaint,
									},
								},
								core.Show{
									When: func() bool {
										_ = rev.Get()
										return ed.HasAgentAPIKey(DefaultAgentAPIKeyAccount)
									},
									Then: func(cx *core.Scope) core.Node {
										return core.View{
											TestID: "connect-key-saved-status",
											Style: core.Style{
												Direction:  core.Row,
												AlignItems: core.AlignCenter,
												Gap:        8,
											},
											Children: []core.Node{
												core.View{
													Style: core.Style{
														Width:        core.Pt(7),
														Height:       core.Pt(7),
														CornerRadius: 4,
														Background:   p.Added.Fg,
													},
												},
												core.Text{
													Content: "Key stored in system keychain · leave blank to keep current key",
													Style: core.Style{
														FontSize: 12,
														Color:    p.Added.Fg,
													},
												},
											},
										}
									},
								},
							},
						}
					},
				},
				// Four starting permission checkboxes
				core.View{
					TestID: "connect-permissions",
					Style:  core.Style{Gap: 8},
					Children: []core.Node{
						core.Text{
							Content: "THE AGENT MAY, WITHOUT ASKING",
							Style: core.Style{
								FontSize:      11,
								FontWeight:    600,
								LetterSpacing: 0.7,
								Color:         p.TextFaint,
							},
						},
						core.View{
							Style: core.Style{Direction: core.Row, Gap: 12},
							Children: []core.Node{
								permCheckbox("connect-perm-scenes", "Edit scenes and data (always reviewable)",
									func(p AgentPermissions) bool { return p.EditScenesAndData },
									func(p *AgentPermissions, v bool) { p.EditScenesAndData = v }),
								permCheckbox("connect-perm-check", "Run game check",
									func(p AgentPermissions) bool { return p.RunGameCheck },
									func(p *AgentPermissions, v bool) { p.RunGameCheck = v }),
							},
						},
						core.View{
							Style: core.Style{Direction: core.Row, Gap: 12},
							Children: []core.Node{
								permCheckbox("connect-perm-playtest", "Run headless playtests",
									func(p AgentPermissions) bool { return p.RunHeadlessPlaytests },
									func(p *AgentPermissions, v bool) { p.RunHeadlessPlaytests = v }),
								permCheckbox("connect-perm-go", "Edit Go code",
									func(p AgentPermissions) bool { return p.EditGoCode },
									func(p *AgentPermissions, v bool) { p.EditGoCode = v }),
							},
						},
					},
				},
				// Footer
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        10,
					},
					Children: []core.Node{
						core.Text{
							Content: "Anything not ticked asks first.",
							Style: core.Style{
								FontSize: 12,
								Color:    p.TextFaint,
							},
						},
						core.View{Style: core.Style{FlexGrow: 1}},
						core.View{
							TestID: "connect-later",
							Role:   core.RoleButton,
							Label:  "Later",
							Style: core.Style{
								Height:         core.Pt(36),
								Padding:        core.Symmetric(0, 16),
								CornerRadius:   8,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.CloseConnectDialog()
							},
							Children: []core.Node{
								core.Text{
									Content: "Later",
									Style: core.Style{
										FontSize: 13,
										Color:    p.TextMuted,
									},
								},
							},
						},
						core.View{
							TestID: "connect-done",
							Role:   core.RoleButton,
							Label:  "Done",
							Style: core.Style{
								Height:         core.Pt(36),
								Padding:        core.Symmetric(0, 18),
								CornerRadius:   8,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Background:     p.TextPrimary,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.CompleteConnectDialog()
							},
							Children: []core.Node{
								core.Text{
									Content: "Done",
									Style: core.Style{
										FontSize:   13,
										FontWeight: 600,
										Color:      p.OnTextPrimary,
									},
								},
							},
						},
					},
				},
			},
		}

		return ui.Dialog{
			TestID: "connect-agent-dialog",
			Label:  "Connect an agent",
			Bare:   true,
			Open: func() bool {
				_ = rev.Get()
				return ed.ConnectDialogOpen()
			},
			OnOpenChange: func(open bool, _ base.Reason) {
				if !open {
					ed.CloseConnectDialog()
				}
			},
			Style: core.Style{
				Width:        core.Pt(640),
				MaxWidth:     core.Pct(95),
				CornerRadius: 18,
				Background:   p.Panel,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{body},
		}
	})
}
