package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// RightRailView renders the editor's right sidebar: the Agent / Inspector tab bar when an agent is not disabled,
// keeping InspectorView active by default at 272 pt and showing AgentPanelView (380 pt) when the Agent tab is selected.
// When AgentModeNone is chosen, the Agent panel stays closed and only InspectorView renders.
func RightRailView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
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
										FlexGrow:  1,
										FlexBasis: core.Pt(0),
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
}

// rightRailTabBar renders the 44 pt Agent / Inspector header bar matching docs/design/editor/Main.dc.html and AgentStates.dc.html.
func rightRailTabBar(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, showFullChip bool) core.Node {
	tabBtn := func(testID, label string, tab RightRailTab) core.Node {
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
			Children: []core.Node{
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
			},
		}
	}

	var trailing core.Node
	if showFullChip {
		trailing = AgentStatusChipView(cx, ed, rev)
	} else {
		trailing = compactAgentChip(ed, rev, p)
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
					case AgentStatusDisconnected:
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
						return "MCP"
					case AgentStatusWorking:
						return ed.T("agent.status.workingShort")
					case AgentStatusWaitingForYou:
						return ed.T("agent.status.waitingShort")
					case AgentStatusReconnecting:
						return ed.T("agent.status.reconnectingShort")
					case AgentStatusDisconnected:
						return ed.T("agent.status.disconnected")
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
						TestID:  "agent-status-text",
						Content: "Connected · MCP",
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
// scrollable thread of user requests / streamed steps / open requests, and the labelled multiline composer.
func AgentPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	composerFocused := core.NewSignal(cx, false)
	openReqRef := new(core.Ref)
	panelRef := new(core.Ref)
	ed.SetAgentPanelRef(panelRef)
	cx.Cleanup(func() {
		ed.SetAgentPanelRef(nil)
	})

	// Shift+Enter inserts a newline while the composer is focused; plain Enter submits via SubmitBehaviorSubmit.
	cx.OnKeyWith("shift+enter", core.KeyOptions{When: core.KeyAlways}, func() {
		if composerFocused.Get() {
			ed.InsertAgentComposerNewline()
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
		fmt.Fprintf(&b, "%s|%d|%d|", ed.AgentStatus(), len(ed.agent.turns), ed.AgentTurnSeq())
		for _, t := range ed.agent.turns {
			fmt.Fprintf(&b, "%s:%s:%d:%v:%v:%v:%d:%d:%d|", t.ID, t.Status, len(t.Steps), t.Done, t.Stopped, t.SkipPlan, len(t.Summary), len(t.Reply), len(t.Error))
			for _, st := range t.Steps {
				fmt.Fprintf(&b, "%s=%s;", st.ID, st.Status)
			}
		}
		if ed.agent.openRequest != nil {
			fmt.Fprintf(&b, "req:%s", ed.agent.openRequest.ID)
		}
		return b.String()
	})

	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := SpecPaletteFor(darkMemo.Get())
		threadBody := core.Dynamic(func(cx *core.Scope) core.Node {
			_ = threadKey.Get()
			turns := ed.AgentTurns()
			openReq := ed.AgentOpenRequest()

			var items []core.Node
			if len(turns) == 0 && openReq == nil {
				items = append(items, agentEmptyStateCard(ed, p))
			}
			for ti, turn := range turns {
				items = append(items, agentTurnView(ed, ti, turn, p))
			}
			if openReq != nil {
				items = append(items, agentOpenRequestCard(ed, openReq, openReqRef, p))
			}
			return core.View{
				TestID: "agent-thread-list",
				Style: core.Style{
					Gap:     14,
					Padding: core.All(16),
				},
				Children: items,
			}
		})

		return core.View{
			TestID: "agent-panel",
			Ref:    panelRef,
			Role:   core.RoleRegion,
			Label:  "Agent panel",
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
				ui.ScrollView{
					TestID: "agent-thread-scroll",
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
					Children: []core.Node{threadBody},
				},
				agentComposerView(cx, ed, rev, p, composerFocused),
			},
		}
	})
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

func agentTurnView(ed *Editor, turnIdx int, turn AgentTurn, p SpecPalette) core.Node {
	var children []core.Node
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
	} else if turn.Prompt != "" {
		promptChildren := []core.Node{
			core.Text{
				Content: turn.Prompt,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			},
		}
		chips := agentTurnContextChips(ed, turn, p)
		if len(chips) > 0 {
			promptChildren = append(promptChildren, core.View{
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
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Gap:          6,
			},
			Children: promptChildren,
		})
	}

	if len(turn.Steps) > 0 {
		stepNodes := make([]core.Node, 0, len(turn.Steps))
		for si, step := range turn.Steps {
			stepNodes = append(stepNodes, agentStepRow(turnIdx, si, step, p))
		}
		children = append(children, core.View{
			TestID: fmt.Sprintf("agent-turn-steps-%d", turnIdx),
			Style: core.Style{
				Gap: 8,
			},
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

	if turn.Stopped {
		children = append(children, core.Text{
			TestID:  fmt.Sprintf("agent-turn-stopped-%d", turnIdx),
			Content: "Stopped · edits already made stay undoable.",
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}

	if turn.Error != "" {
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

func agentComposerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette, composerFocused *core.Signal[bool]) core.Node {
	type composerContextKey struct {
		sel   string
		scene string
	}
	contextMemo := core.NewMemo(cx, func() composerContextKey {
		_ = rev.Get()
		var sceneName string
		if ed.Doc != nil {
			sceneName = ed.Doc.Name
		}
		return composerContextKey{sel: ed.SelectedID, scene: sceneName}
	})
	contextChips := core.Dynamic(func(cx *core.Scope) core.Node {
		ctxKey := contextMemo.Get()
		var chips []core.Node
		if ctxKey.sel != "" {
			chips = append(chips, core.View{
				TestID: "agent-context-selection",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(22),
					Padding:      core.Symmetric(0, 8),
					Gap:          5,
					CornerRadius: 6,
					Background:   p.LineRaised,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 2,
							Background:   p.Changed.Fg,
						},
					},
					core.Text{
						Content: ctxKey.sel,
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextMuted,
						},
					},
				},
			})
		}
		if ctxKey.scene != "" {
			chips = append(chips, core.View{
				TestID: "agent-context-scene",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(22),
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 6,
					Background:   p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: ctxKey.scene,
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextMuted,
						},
					},
				},
			})
		}
		if len(chips) == 0 {
			return nil
		}
		return core.View{
			Style: core.Style{
				Direction: core.Row,
				Wrap:      core.Wrap,
				Gap:       6,
			},
			Children: chips,
		}
	})

	return core.View{
		TestID: "agent-composer",
		Style: core.Style{
			Padding:     core.All(12),
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
			Background:  p.Panel,
			FlexShrink:  0,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					CornerRadius: 12,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.Symmetric(10, 12),
					Gap:          8,
				},
				Children: []core.Node{
					contextChips,
					core.Text{
						Content: ed.T("agent.composerLabel"),
						Style: core.Style{
							FontSize:   11,
							FontWeight: 600,
							Color:      p.TextFaint,
						},
					},
					ui.Input{
						TestID:         "agent-composer-input",
						Label:          ed.T("agent.composerLabel"),
						Placeholder:    ed.T("agent.composerPlaceholder"),
						Multiline:      true,
						SubmitBehavior: core.SubmitBehaviorSubmit,
						ReturnKeyType:  core.ReturnKeySend,
						Value: func() string {
							_ = rev.Get()
							return ed.AgentComposerText()
						},
						OnChange: func(text string) {
							ed.SetAgentComposerText(text)
						},
						OnSubmit: func(text string) {
							ed.report(ed.SubmitAgentPrompt(text))
						},
						OnFocus: func() {
							composerFocused.Set(true)
						},
						OnBlur: func() {
							composerFocused.Set(false)
						},
						Style: core.Style{
							MinHeight:        core.Pt(48),
							MaxHeight:        core.Pt(120),
							Background:       p.Bg,
							BorderWidth:      1,
							BorderColor:      p.LineRaised,
							CornerRadius:     8,
							Padding:          core.Symmetric(8, 10),
							FontSize:         12.5,
							Color:            p.TextSecondary,
							PlaceholderColor: p.TextFaint,
							FocusVisible:     p.FocusRing(2),
						},
					},
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("agent.composerHint"),
								Style: core.Style{
									FlexGrow: 1,
									FontSize: 11,
									Color:    p.TextFaint,
								},
							},
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.AgentWorking()
								},
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: "agent-composer-stop",
										Role:   core.RoleButton,
										Label:  "Stop",
										Style: core.Style{
											Direction:      core.Row,
											AlignItems:     core.AlignCenter,
											JustifyContent: core.JustifyCenter,
											Height:         core.Pt(30),
											Padding:        core.Symmetric(0, 12),
											Gap:            6,
											CornerRadius:   8,
											Background:     p.LineRaised,
											FocusVisible:   p.FocusRing(2),
										},
										OnPress: func(core.PressEvent) {
											ed.StopAgentRun()
										},
										Children: []core.Node{
											core.View{
												Style: core.Style{
													Width:        core.Pt(9),
													Height:       core.Pt(9),
													CornerRadius: 2,
													Background:   p.TextPrimary,
												},
											},
											core.Text{
												Content: "Stop",
												Style: core.Style{
													FontSize:   12,
													FontWeight: 500,
													Color:      p.TextPrimary,
												},
											},
										},
									}
								},
								Else: func(cx *core.Scope) core.Node {
									return core.View{
										TestID: "agent-composer-send",
										Role:   core.RoleButton,
										Label:  "Send",
										Style: core.Style{
											Width:          core.Pt(30),
											Height:         core.Pt(30),
											CornerRadius:   8,
											AlignItems:     core.AlignCenter,
											JustifyContent: core.JustifyCenter,
											Background:     p.TextPrimary,
											FocusVisible:   p.FocusRing(2),
										},
										OnPress: func(core.PressEvent) {
											ed.report(ed.SubmitAgentPrompt(""))
										},
										Children: []core.Node{
											core.SVGView{
												SVG: lucide.ArrowUp(),
												Style: core.Style{
													Width:  core.Pt(14),
													Height: core.Pt(14),
													Color:  p.OnTextPrimary,
												},
											},
										},
									}
								},
							},
						},
					},
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
