package editor

import (
	"cmp"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// AgentThreadsView renders the Agent panel for the open scene's thread: status chip, thread
// switcher header (with ⌘N new thread and grouped scene switcher popover), folded earlier
// requests with History links, Jump to latest pill, empty-thread view with always-read context
// and brief suggestions, live turns, and the prompt composer.
func AgentThreadsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return AgentPanelView(cx, ed, rev)
}

func agentThreadHeaderBar(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], switcherBtnRef *core.Ref) core.Node {
	newShortcut := FormatShortcut("mod+n", cx.Env().OS())
	return core.View{
		TestID: "agent-thread-header",
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				Height:      core.Pt(specHeaderHeight),
				FlexShrink:  0,
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Padding:     core.Symmetric(0, 8),
				Gap:         4,
				Background:  p.Panel,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			}
		},
		Children: []core.Node{
			core.View{
				Ref:    switcherBtnRef,
				TestID: "agent-thread-switcher-trigger",
				Role:   core.RoleButton,
				LabelFunc: func() string {
					_ = rev.Get()
					active := ed.ActiveAgentThread()
					title := ed.T("threads.newThread")
					if active != nil && active.Title != "" {
						title = active.Title
					}
					return ed.T("threads.switcherTriggerLabel", l10n.Param("title", title))
				},
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					bg := core.Color(0)
					if ed.AgentSwitcherOpen() {
						bg = p.LineRaised
					}
					return core.Style{
						Height:       core.Pt(specButtonHeight),
						FlexGrow:     1,
						FlexBasis:    core.Pt(0),
						MinWidth:     core.Pt(0),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Padding:      core.Symmetric(0, 8),
						Gap:          8,
						CornerRadius: specRadiusButton,
						Background:   bg,
						Hover:        &core.Style{Background: specHoverOverlay(p)},
						FocusVisible: p.FocusRing(2),
					}
				},
				OnPress: func(core.PressEvent) {
					ed.ToggleAgentSwitcher()
				},
				Children: []core.Node{
					specDynamicIcon(func() *core.SVG { return lucide.MessageSquare() }, 14, func() core.Color {
						return ed.SpecPalette(cx).TextFaint
					}),
					core.Text{
						TestID: "agent-thread-title",
						ContentFunc: func() string {
							_ = rev.Get()
							if active := ed.ActiveAgentThread(); active != nil && active.Title != "" {
								return active.Title
							}
							return ed.T("threads.newThread")
						},
						MaxLines: 1,
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{
								FlexGrow:   1,
								FlexShrink: 1,
								FontSize:   12.5,
								FontWeight: core.FontWeightMedium,
								Color:      p.TextPrimary,
							}
						},
					},
					specDynamicIcon(func() *core.SVG {
						_ = rev.Get()
						if ed.AgentSwitcherOpen() {
							return lucide.ChevronUp()
						}
						return lucide.ChevronDown()
					}, 12, func() core.Color {
						return ed.SpecPalette(cx).TextFaint
					}),
				},
			},
			ui.Tooltip{
				Text: specShortcutTip(ed.T("threads.newThread"), newShortcut),
				Trigger: core.View{
					TestID: "agent-thread-new",
					Role:   core.RoleButton,
					Label:  specShortcutTip(ed.T("threads.newThread"), newShortcut),
					StyleFunc: func() core.Style {
						p := ed.SpecPalette(cx)
						return core.Style{
							Width:          core.Pt(28),
							Height:         core.Pt(28),
							FlexShrink:     0,
							CornerRadius:   specRadiusControl,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Hover:          &core.Style{Background: specHoverOverlay(p)},
							FocusVisible:   p.FocusRing(2),
						}
					},
					OnPress: func(core.PressEvent) {
						ed.NewAgentThread()
					},
					Children: []core.Node{
						specDynamicIcon(func() *core.SVG { return lucide.SquarePen() }, 14, func() core.Color {
							return ed.SpecPalette(cx).TextMuted
						}),
					},
				},
			},
		},
	}
}

func agentThreadSwitcherPopover(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	newShortcut := FormatShortcut("mod+n", cx.Env().OS())
	groupsMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%d|%s|%s|%v", ed.agentThreads.rev, ed.agentThreads.switcherQuery, ed.agentThreads.activeThreadID, cx.Dark())
	})

	return core.View{
		TestID: "agent-thread-switcher",
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				Position:     core.Absolute,
				Top:          core.Pt(4),
				Left:         core.Pt(8),
				Right:        core.Pt(8),
				Bottom:       core.Pt(8),
				CornerRadius: specRadiusCard,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Overflow:     core.Hidden,
			}
		},
		Children: []core.Node{
			core.View{
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Padding:     core.All(8),
						Gap:         8,
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.LineRaised,
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.Input{
								TestID:      "agent-thread-search",
								Label:       ed.T("threads.searchPlaceholder"),
								Placeholder: ed.T("threads.searchPlaceholder"),
								Value: func() string {
									_ = rev.Get()
									return ed.AgentSwitcherQuery()
								},
								OnChange: func(v string) {
									ed.SetAgentSwitcherQuery(v)
								},
								OnFocus: func() { ed.noteTextInputFocus(true) },
								OnBlur:  func() { ed.noteTextInputFocus(false) },
							},
						},
					},
					core.View{
						TestID: "agent-thread-new-btn",
						Role:   core.RoleButton,
						Label:  specShortcutTip(ed.T("threads.newButton"), newShortcut),
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{
								Height:         core.Pt(28),
								FlexShrink:     0,
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Padding:        core.Symmetric(0, 8),
								Gap:            6,
								CornerRadius:   specRadiusControl,
								Background:     p.LineRaised,
								Hover:          &core.Style{Background: specHoverOverlay(p)},
								FocusVisible:   p.FocusRing(2),
							}
						},
						OnPress: func(core.PressEvent) {
							ed.NewAgentThread()
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("threads.newButton"),
								StyleFunc: func() core.Style {
									return core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: ed.SpecPalette(cx).TextPrimary}
								},
							},
							core.Text{
								Content: newShortcut,
								StyleFunc: func() core.Style {
									return core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: ed.SpecPalette(cx).TextMuted}
								},
							},
						},
					},
				},
			},
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
				},
				ContentStyle: core.Style{
					Padding: core.Edges{Top: 4, Right: 4, Bottom: 8, Left: 4},
					Gap:     2,
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = groupsMemo.Get()
						p := ed.SpecPalette(cx)
						groups := ed.AgentThreadGroups(ed.AgentSwitcherQuery())
						if len(groups) == 0 {
							return core.View{
								Style: core.Style{Padding: core.All(12)},
								Children: []core.Node{
									core.Text{
										TestID:  "agent-thread-switcher-empty",
										Content: ed.T("threads.switcherEmpty"),
										Style:   core.Style{FontSize: 12, Color: p.TextFaint},
									},
								},
							}
						}
						var rows []core.Node
						for gi, g := range groups {
							groupTestID := fmt.Sprintf("agent-thread-group-%d", gi)
							rows = append(rows, core.View{
								TestID: groupTestID,
								Style:  core.Style{Padding: core.Edges{Top: 10, Right: 8, Bottom: 4, Left: 8}},
								Children: []core.Node{
									core.Text{
										Content: g.Heading,
										Style: core.Style{
											FontSize:      11,
											FontWeight:    core.FontWeightSemibold,
											LetterSpacing: 0.7,
											Color:         p.TextFaint,
										},
									},
								},
							})
							for _, item := range g.Threads {
								it := item
								rowBg := core.Color(0)
								if it.Current {
									rowBg = p.Accent.Fill
								}
								rows = append(rows, core.View{
									TestID: "agent-thread-item-" + it.ID,
									Role:   core.RoleButton,
									Label:  it.Title + " · " + it.Meta,
									Style: core.Style{
										Padding:      core.All(8),
										Gap:          2,
										CornerRadius: specRadiusControl,
										Background:   rowBg,
										Hover:        &core.Style{Background: specHoverOverlay(p)},
										FocusVisible: p.FocusRing(-2),
									},
									OnPress: func(core.PressEvent) {
										ed.report(ed.SelectAgentThread(it.ID))
									},
									Children: []core.Node{
										core.Text{
											Content:  it.Title,
											MaxLines: 1,
											Style: core.Style{
												FontSize:   12.5,
												FontWeight: core.FontWeightMedium,
												Color:      p.TextPrimary,
											},
										},
										core.Text{
											Content:  it.Meta,
											MaxLines: 1,
											Style: core.Style{
												FontSize: 11.5,
												Color:    p.TextFaint,
											},
										},
									},
								})
							}
						}
						return core.View{Style: core.Style{Gap: 2}, Children: rows}
					}),
				},
			},
			core.View{
				TestID: "agent-thread-switcher-footer",
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Height:      core.Pt(30),
						FlexShrink:  0,
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Padding:     core.Symmetric(0, 12),
						Borders:     core.Edges{Top: 1},
						BorderColor: p.LineRaised,
					}
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("threads.storageFooter"),
						StyleFunc: func() core.Style {
							return core.Style{
								FontSize:   11,
								FontFamily: core.FontMonospace,
								Color:      ed.SpecPalette(cx).TextFaint,
							}
						},
					},
				},
			},
		},
	}
}

func agentEmptyThreadBody(cx *core.Scope, ed *Editor, p SpecPalette) core.Node {
	items := ed.AgentAlwaysReadItems()
	chips := make([]core.Node, 0, len(items))
	for _, it := range items {
		bg := core.Color(0)
		fg := p.TextFaint
		borderW := core.Points(1)
		if it.Primary {
			bg = p.LineRaised
			fg = p.TextPrimary
			borderW = 0
		}
		chips = append(chips, core.View{
			TestID: "agent-empty-context-" + it.ID,
			Style: core.Style{
				Height:         core.Pt(22),
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   specRadiusControl,
				Background:     bg,
				BorderWidth:    borderW,
				BorderColor:    p.LineRaised,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: it.Label,
					Style: core.Style{
						FontSize: 11.5,
						Color:    fg,
					},
				},
			},
		})
	}

	suggestions := ed.AgentBriefSuggestions()
	suggNodes := make([]core.Node, 0, len(suggestions))
	for i, text := range suggestions {
		prompt := text
		suggNodes = append(suggNodes, core.View{
			TestID: fmt.Sprintf("agent-brief-suggestion-%d", i),
			Role:   core.RoleButton,
			Label:  prompt,
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Padding:      core.All(12),
				Gap:          12,
				CornerRadius: specRadiusCard,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Hover:        &core.Style{Background: specHoverOverlay(p)},
				FocusVisible: p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				_, err := ed.SendAgentRequest(prompt)
				ed.report(err)
			},
			Children: []core.Node{
				core.Text{
					Content: prompt,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						FontSize:   12.5,
						LineHeight: 18,
						Color:      p.TextSecondary,
					},
				},
				specIcon(lucide.ArrowRight, 14, p.TextFaint),
			},
		})
	}

	emptyChildren := []core.Node{
		core.View{
			Style: core.Style{Gap: 8},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-empty-reads-heading",
					Content: ed.T("threads.empty.readsHeading"),
					Style: core.Style{
						FontSize:   12.5,
						LineHeight: 19,
						Color:      p.TextSecondary,
					},
				},
				core.View{
					Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
					Children: chips,
				},
			},
		},
		core.View{
			Style: core.Style{Gap: 8},
			Children: append([]core.Node{
				core.Text{
					TestID:  "agent-empty-brief-heading",
					Content: ed.T("threads.empty.fromBrief"),
					Style: core.Style{
						FontSize:      11,
						FontWeight:    core.FontWeightSemibold,
						LetterSpacing: 0.7,
						Color:         p.TextFaint,
					},
				},
			}, suggNodes...),
		},
	}
	if ed.AgentStatus() != AgentStatusConnected {
		emptyChildren = append(emptyChildren, agentEmptyStateCard(ed, p))
	}

	return ui.ScrollView{
		TestID: "agent-empty-thread",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
		},
		ContentStyle: core.Style{
			Padding: core.Edges{Top: 20, Right: 16, Bottom: 16, Left: 16},
			Gap:     16,
		},
		Children: emptyChildren,
	}
}

func agentThreadConversationBody(cx *core.Scope, ed *Editor, p SpecPalette, t *AgentThread, turns []AgentTurn, openReq *AgentOpenRequest, openReqRef *core.Ref) core.Node {
	expandedID := ed.ExpandedAgentRequestID()
	var nodes []core.Node
	if ready := ed.ReadyToReviewTurns(); len(ready) > 0 {
		nodes = append(nodes, agentReadyToReviewBanner(ed, p, len(ready)))
	}
	queued := ed.AgentQueuedRequests()
	limitReached := ed.AgentWeeklyLimitReached()
	if limitReached {
		nodes = append(nodes, agentLimitReachedCard(ed, p))
		if len(queued) > 0 {
			nodes = append(nodes, agentQueuedRequestsView(ed, queued, limitReached, p))
		}
	}

	earlier := t.Requests[:len(t.Requests)-1]
	latest := t.Requests[len(t.Requests)-1]

	if len(earlier) > 0 {
		heading := ed.T("threads.earlierRequestsMany", l10n.Param("count", len(earlier)))
		if len(earlier) == 1 {
			heading = ed.T("threads.earlierRequestsOne")
		}
		nodes = append(nodes, core.Text{
			TestID:  "agent-earlier-requests-heading",
			Content: heading,
			Style: core.Style{
				FontSize:      11,
				FontWeight:    core.FontWeightSemibold,
				LetterSpacing: 0.7,
				Color:         p.TextFaint,
				Padding:       core.Edges{Bottom: 4},
			},
		})
		for _, req := range earlier {
			r := req
			isOpen := expandedID == r.ID
			rowBg := core.Color(0)
			if isOpen {
				rowBg = p.Raised
			}
			iconBg := p.Added.Fill
			iconFg := p.Added.Fg
			iconSVG := lucide.Check
			if r.Outcome == AgentThreadOutcomeReverted {
				iconBg = p.LineRaised
				iconFg = p.TextMuted
				iconSVG = lucide.Undo2
			}
			meta := ed.formatRequestOutcomeShort(r)
			if clk := formatRequestClockTime(r.CreatedAt); clk != "" {
				meta += " · " + clk
			}
			reqTitle := cmp.Or(r.Title, deriveThreadTitle(r.Prompt))

			nodes = append(nodes, core.View{
				TestID: "agent-request-row-" + r.ID,
				Role:   core.RoleButton,
				Label:  reqTitle + " · " + meta,
				Style: core.Style{
					Height:       core.Pt(specRowHeight),
					FlexShrink:   0,
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Padding:      core.Symmetric(0, 8),
					Gap:          8,
					CornerRadius: specRadiusControl,
					Background:   rowBg,
					Hover:        &core.Style{Background: specHoverOverlay(p)},
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.ToggleAgentRequestExpanded(r.ID)
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(16),
							Height:         core.Pt(16),
							FlexShrink:     0,
							CornerRadius:   8,
							Background:     iconBg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							specIcon(iconSVG, 10, iconFg),
						},
					},
					core.Text{
						Content:  reqTitle,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							FontSize:   12.5,
							Color:      p.TextSecondary,
						},
					},
					core.Text{
						Content:  meta,
						MaxLines: 1,
						Style: core.Style{
							FlexShrink: 0,
							FontSize:   11.5,
							Color:      p.TextFaint,
						},
					},
				},
			})

			if isOpen {
				detailChildren := []core.Node{
					core.Text{
						Content: r.Prompt,
						Style: core.Style{
							FontSize:   12.5,
							LineHeight: 18,
							Color:      p.TextSecondary,
						},
					},
				}
				if r.Detail != "" {
					detailChildren = append(detailChildren, core.Text{
						Content: r.Detail,
						Style: core.Style{
							FontSize:   11.5,
							LineHeight: 17,
							Color:      p.TextFaint,
						},
					})
				}
				for _, st := range r.Steps {
					step := st
					detailChildren = append(detailChildren, core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							agentStepGlyph(p, step.Status),
							core.Text{
								Content:  step.Label,
								MaxLines: 1,
								Style: core.Style{
									FlexGrow:   1,
									FlexShrink: 1,
									FontSize:   11.5,
									Color:      p.TextMuted,
								},
							},
						},
					})
				}
				firstStepID := ""
				if len(r.Steps) > 0 {
					firstStepID = r.Steps[len(r.Steps)-1].ID
				}
				detailChildren = append(detailChildren, core.View{
					TestID: "agent-open-history-" + r.ID,
					Role:   core.RoleButton,
					Label:  ed.T("threads.openInHistory"),
					Style: core.Style{
						AlignSelf:    core.AlignStart,
						Padding:      core.Symmetric(2, 0),
						CornerRadius: 4,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.OpenHistoryStep(t.ID, r.ID, firstStepID))
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("threads.openInHistory"),
							Style: core.Style{
								FontSize:   11.5,
								FontWeight: core.FontWeightMedium,
								Color:      p.Accent.Fg,
							},
						},
					},
				})
				nodes = append(nodes, core.View{
					TestID: "agent-request-detail-" + r.ID,
					Style: core.Style{
						Margin:       core.Edges{Top: 2, Bottom: 6, Left: 24},
						Padding:      core.Symmetric(8, 12),
						Gap:          5,
						CornerRadius: specRadiusButton,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: detailChildren,
				})
			}
		}

		// Divider between folded earlier requests and the latest request.
		nodes = append(nodes, core.View{
			Style: core.Style{
				Height:     core.Pt(1),
				Background: p.Line,
				Margin:     core.Symmetric(8, 0),
			},
		})
	}

	// Latest request unfolded.
	latestNodes := []core.Node{
		core.View{
			TestID: "agent-latest-prompt",
			Style: core.Style{
				AlignSelf:    core.AlignEnd,
				MaxWidth:     core.Pt(300),
				Padding:      core.Symmetric(8, 12),
				CornerRadius: specRadiusCard,
				Background:   p.Accent.Fill,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.Text{
					Content: latest.Prompt,
					Style: core.Style{
						FontSize:   12.5,
						LineHeight: 18,
						Color:      p.TextPrimary,
					},
				},
			},
		},
	}

	if len(latest.Steps) > 0 {
		stepNodes := make([]core.Node, 0, len(latest.Steps))
		for _, st := range latest.Steps {
			step := st
			stepNodes = append(stepNodes, core.View{
				TestID: "agent-step-" + step.ID,
				Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					agentStepGlyph(p, step.Status),
					core.Text{
						Content: step.Label,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							FontSize:   12,
							Color:      p.TextFaint,
						},
					},
				},
			})
		}
		latestNodes = append(latestNodes, core.View{
			Style:    core.Style{Gap: 8},
			Children: stepNodes,
		})
	} else if latest.Outcome == AgentThreadOutcomePending {
		// <300ms gray placeholder step before the agent reports its first real step.
		sceneFile := "scene.yaml"
		if t.ScenePath != "" {
			sceneFile = t.ScenePath
		}
		latestNodes = append(latestNodes, core.View{
			TestID: "agent-step-placeholder",
			Style:  core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(14),
						Height:       core.Pt(14),
						CornerRadius: 7,
						BorderWidth:  2,
						BorderColor:  p.TextFaint,
						FlexShrink:   0,
					},
				},
				core.Text{
					Content: ed.T("threads.readingPlaceholder", l10n.Param("file", sceneFile)),
					Style: core.Style{
						FontSize: 12,
						Color:    p.TextFaint,
					},
				},
			},
		})
	}

	if latest.Reply != "" {
		latestNodes = append(latestNodes, core.Text{
			TestID:  "agent-latest-reply",
			Content: latest.Reply,
			Style: core.Style{
				FontSize:   12.5,
				LineHeight: 19,
				Color:      p.TextSecondary,
			},
		})
	}

	if latest.Outcome == AgentThreadOutcomePending && latest.ChangesTotal > 0 {
		summary := ed.T("threads.reviewCard.summary", l10n.Param("changes", latest.ChangesTotal), l10n.Param("files", max(len(latest.Files), 1)))
		latestNodes = append(latestNodes, core.View{
			TestID: "agent-latest-review-card",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Padding:      core.Edges{Top: 8, Right: 8, Bottom: 8, Left: 12},
				Gap:          8,
				CornerRadius: specRadiusCard,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.Text{
					Content: summary,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						FontSize:   12.5,
						FontWeight: core.FontWeightMedium,
						Color:      p.TextPrimary,
					},
				},
				core.View{
					TestID: "agent-latest-keep",
					Role:   core.RoleButton,
					Label:  ed.T("threads.action.keep"),
					Style: core.Style{
						Height:         core.Pt(specButtonHeight),
						Padding:        core.Symmetric(0, 12),
						CornerRadius:   specRadiusButton,
						Background:     p.Accent.Fg,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.KeepAgentRequest(t.ID, latest.ID))
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("threads.action.keep"),
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: core.FontWeightSemibold,
								Color:      p.Accent.OnFill,
							},
						},
					},
				},
				core.View{
					TestID: "agent-latest-revert",
					Role:   core.RoleButton,
					Label:  ed.T("threads.action.revert"),
					Style: core.Style{
						Height:         core.Pt(specButtonHeight),
						Padding:        core.Symmetric(0, 12),
						CornerRadius:   specRadiusButton,
						Background:     p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Hover:          &core.Style{Background: specHoverOverlay(p)},
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.RevertAgentRequest(t.ID, latest.ID))
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("threads.action.revert"),
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextSecondary,
							},
						},
					},
				},
			},
		})
	} else if latest.Outcome == AgentThreadOutcomeKept || latest.Outcome == AgentThreadOutcomeReverted {
		latestNodes = append(latestNodes, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 8},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-latest-outcome",
					Content: ed.formatRequestOutcomeShort(latest),
					Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
				},
				core.View{
					TestID: "agent-open-history-" + latest.ID,
					Role:   core.RoleButton,
					Label:  ed.T("threads.openInHistory"),
					Style: core.Style{
						CornerRadius: 4,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						stepID := ""
						if len(latest.Steps) > 0 {
							stepID = latest.Steps[len(latest.Steps)-1].ID
						}
						ed.report(ed.OpenHistoryStep(t.ID, latest.ID, stepID))
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("threads.openInHistory"),
							Style:   core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: p.Accent.Fg},
						},
					},
				},
			},
		})
	}

	nodes = append(nodes, core.View{
		TestID:   "agent-latest-request",
		Style:    core.Style{Gap: 12},
		Children: latestNodes,
	})
	for ti, turn := range turns {
		nodes = append(nodes, agentTurnView(ed, ti, turn, p))
	}
	if ed.HasAgentPanelCard() {
		nodes = append(nodes, agentReviewPanelCards(ed, p, turns)...)
	}
	if ed.DataTableReviewPending() {
		nodes = append(nodes, agentPendingDataTableReviewCard(ed, p))
	}
	if turnSnap, hasPendingTurn := ed.CurrentAgentTurn(); hasPendingTurn {
		nodes = append(nodes, agentTurnSection(cx, ed, p, turnSnap))
	}
	if openReq != nil {
		nodes = append(nodes, agentOpenRequestCard(ed, openReq, openReqRef, p))
	}
	if !limitReached && len(queued) > 0 {
		nodes = append(nodes, agentQueuedRequestsView(ed, queued, limitReached, p))
	}

	bodyChildren := []core.Node{
		ui.ScrollView{
			TestID: "agent-thread-scroll",
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
			},
			ContentStyle: core.Style{
				Padding: core.Edges{Top: 12, Right: 16, Bottom: 48, Left: 16},
				Gap:     4,
			},
			Children: nodes,
		},
	}

	if expandedID != "" {
		bodyChildren = append(bodyChildren, core.View{
			TestID: "agent-jump-latest",
			Role:   core.RoleButton,
			Label:  ed.T("threads.jumpToLatest"),
			Style: core.Style{
				Position:       core.Absolute,
				Bottom:         core.Pt(12),
				Left:           core.Pt(112),
				Height:         core.Pt(28),
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Padding:        core.Symmetric(0, 12),
				Gap:            6,
				CornerRadius:   14,
				Background:     p.LineRaised,
				BorderWidth:    1,
				BorderColor:    p.Line,
				Hover:          &core.Style{Background: specHoverOverlay(p)},
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.JumpToLatestAgentRequest()
			},
			Children: []core.Node{
				specIcon(lucide.ArrowDown, 12, p.TextPrimary),
				core.Text{
					Content: ed.T("threads.jumpToLatest"),
					Style: core.Style{
						FontSize:   11.5,
						FontWeight: core.FontWeightMedium,
						Color:      p.TextPrimary,
					},
				},
			},
		})
	}

	return core.View{
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: bodyChildren,
	}
}

func agentStepGlyph(p SpecPalette, status AgentThreadStepStatus) core.Node {
	switch status {
	case AgentThreadStepDone:
		return core.View{
			Style: core.Style{
				Width:          core.Pt(14),
				Height:         core.Pt(14),
				CornerRadius:   7,
				Background:     p.Added.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{specIcon(lucide.Check, 9, p.Added.Fg)},
		}
	case AgentThreadStepFailed:
		return core.View{
			Style: core.Style{
				Width:          core.Pt(14),
				Height:         core.Pt(14),
				CornerRadius:   7,
				Background:     p.Removed.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{specIcon(lucide.X, 9, p.Removed.Fg)},
		}
	case AgentThreadStepRunning:
		return core.View{
			Style: core.Style{
				Width:        core.Pt(14),
				Height:       core.Pt(14),
				CornerRadius: 7,
				BorderWidth:  2,
				BorderColor:  p.Agent.Edge,
				FlexShrink:   0,
			},
		}
	default:
		return core.View{
			Style: core.Style{
				Width:        core.Pt(14),
				Height:       core.Pt(14),
				CornerRadius: 7,
				BorderWidth:  2,
				BorderColor:  p.TextFaint,
				FlexShrink:   0,
			},
		}
	}
}

// AgentHistoryView renders the right-rail History timeline combining agent steps and manual edits,
// highlighting the step opened via "Open this step in History".
func AgentHistoryView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	historyKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		sel := ed.SelectedHistoryStep()
		return fmt.Sprintf("%d|%d|%s|%s|%s|%v", ed.agentThreads.rev, ed.History.Index(), sel.ThreadID, sel.RequestID, sel.StepID, cx.Dark())
	})

	return core.View{
		TestID: "agent-history-panel",
		Role:   core.RoleRegion,
		Label:  ed.T("threads.history.title"),
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				FlexGrow:   1,
				FlexBasis:  core.Pt(0),
				Background: p.Panel,
				Overflow:   core.Hidden,
			}
		},
		Children: []core.Node{
			core.View{
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Height:         core.Pt(specHeaderHeight),
						FlexShrink:     0,
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Padding:        core.Symmetric(0, 14),
						Borders:        core.Edges{Bottom: 1},
						BorderColor:    p.Line,
					}
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("threads.history.title"),
						StyleFunc: func() core.Style {
							return core.Style{FontSize: 13, FontWeight: core.FontWeightSemibold, Color: ed.SpecPalette(cx).TextPrimary}
						},
					},
					core.Text{
						Content: ed.T("threads.history.subtitle"),
						StyleFunc: func() core.Style {
							return core.Style{FontSize: 11.5, Color: ed.SpecPalette(cx).TextFaint}
						},
					},
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				ContentStyle: core.Style{Padding: core.Symmetric(14, 16), Gap: 6},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = historyKey.Get()
						p := ed.SpecPalette(cx)
						entries := ed.AgentHistoryEntries()
						rows := make([]core.Node, 0, len(entries))
						for i, en := range entries {
							entry := en
							dotFill := p.Panel
							dotBorder := p.TextFaint
							switch entry.Kind {
							case "now":
								dotFill = p.Accent.Fg
								dotBorder = p.Accent.Edge
							case "agent":
								dotBorder = p.Agent.Edge
							case "saved":
								dotBorder = p.Added.Edge
							}
							rowBg := core.Color(0)
							rowBorder := core.Color(0)
							rowBW := core.Points(0)
							if entry.Highlighted {
								rowBg = p.Accent.Fill
								rowBorder = p.Accent.Edge
								rowBW = 1
							}
							var actions []core.Node
							if entry.ThreadID != "" {
								actions = append(actions, core.View{
									TestID: "history-open-thread-" + entry.RequestID,
									Role:   core.RoleButton,
									Label:  ed.T("threads.history.backToThread"),
									Style: core.Style{
										AlignSelf:    core.AlignStart,
										Margin:       core.Edges{Top: 4},
										CornerRadius: 4,
										FocusVisible: p.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										ed.report(ed.OpenAgentRequest(entry.ThreadID, entry.RequestID))
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("threads.history.backToThread"),
											Style:   core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: p.Accent.Fg},
										},
									},
								})
							}
							var lineNode core.Node
							if i < len(entries)-1 {
								lineNode = core.View{
									Style: core.Style{
										Width:      core.Pt(1.5),
										FlexGrow:   1,
										MinHeight:  core.Pt(24),
										Background: p.LineRaised,
									},
								}
							}
							rows = append(rows, core.View{
								TestID: "history-entry-" + entry.ID,
								Style: core.Style{
									Direction:    core.Row,
									Gap:          12,
									Padding:      core.Symmetric(6, 8),
									CornerRadius: specRadiusButton,
									Background:   rowBg,
									BorderWidth:  rowBW,
									BorderColor:  rowBorder,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{AlignItems: core.AlignCenter, Width: core.Pt(14), FlexShrink: 0, Padding: core.Edges{Top: 3}},
										Children: []core.Node{
											core.View{
												Style: core.Style{
													Width:        core.Pt(10),
													Height:       core.Pt(10),
													CornerRadius: 5,
													Background:   dotFill,
													BorderWidth:  1.5,
													BorderColor:  dotBorder,
												},
											},
											lineNode,
										},
									},
									core.View{
										Style: core.Style{FlexGrow: 1, FlexShrink: 1, Gap: 2},
										Children: append([]core.Node{
											core.Text{
												Content: entry.Title,
												Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightMedium, Color: p.TextPrimary},
											},
											core.Text{
												Content: entry.Meta,
												Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
											},
										}, actions...),
									},
								},
							})
						}
						return core.View{Style: core.Style{Gap: 4}, Children: rows}
					}),
				},
			},
		},
	}
}

// inspectorThreadFooterView renders the pinned Inspector footer button linking the selected
// object to the agent request that last changed it ("Changed in ‘…’ · 2 min ago").
func inspectorThreadFooterView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	footerKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if shouldShowRemoteInspector(ed) || ed.SelectedID == "" || len(ed.SelectedIDs()) > 1 {
			return ""
		}
		attr, ok := ed.LastAgentChangeForObject(ed.SelectedID)
		if !ok {
			return ""
		}
		return fmt.Sprintf("%s|%s|%s|%s|%v", ed.SelectedID, attr.ThreadID, attr.RequestID, attr.AgeLabel, cx.Dark())
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if footerKey.Get() == "" {
			return nil
		}
		attr, ok := ed.LastAgentChangeForObject(ed.SelectedID)
		if !ok {
			return nil
		}
		p := ed.SpecPalette(cx)
		label := ed.T("threads.inspectorFooter",
			l10n.Param("title", attr.RequestTitle),
			l10n.Param("age", attr.AgeLabel),
		)
		return core.View{
			Style: core.Style{
				FlexShrink:  0,
				Padding:     core.All(10),
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
				Background:  p.Panel,
			},
			Children: []core.Node{
				core.View{
					TestID: "inspector-thread-footer",
					Role:   core.RoleButton,
					Label:  label,
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						MinHeight:    core.Pt(specButtonHeight),
						Padding:      core.Symmetric(6, 8),
						Gap:          8,
						CornerRadius: specRadiusButton,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Hover:        &core.Style{Background: specHoverOverlay(p)},
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.OpenAgentRequest(attr.ThreadID, attr.RequestID))
					},
					Children: []core.Node{
						specIcon(lucide.MessageSquare, 14, p.TextMuted),
						core.Text{
							TestID:   "inspector-thread-footer-text",
							Content:  label,
							MaxLines: 2,
							Style: core.Style{
								FlexGrow:   1,
								FlexShrink: 1,
								FontSize:   11.5,
								LineHeight: 16,
								Color:      p.TextSecondary,
							},
						},
					},
				},
			},
		}
	})
}
