package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func agentAccentButton(testID, label string, p SpecPalette, onPress func()) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  label,
		Style: core.Style{
			Height:         core.Pt(30),
			Padding:        core.Symmetric(0, 12),
			CornerRadius:   8,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     p.Accent.Fg,
			FocusVisible:   p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if onPress != nil {
				onPress()
			}
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 600,
					Color:      p.Accent.OnFill,
				},
			},
		},
	}
}

func agentSecondaryButton(testID, label string, activeBorder bool, p SpecPalette, onPress func()) core.Node {
	borderCol := p.LineRaised
	borderW := core.Points(1)
	if activeBorder {
		borderCol = p.Accent.Edge
		borderW = 1.5
	}
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  label,
		Style: core.Style{
			Height:         core.Pt(30),
			Padding:        core.Symmetric(0, 12),
			CornerRadius:   8,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     p.Raised,
			BorderWidth:    borderW,
			BorderColor:    borderCol,
			FocusVisible:   p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if onPress != nil {
				onPress()
			}
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      p.TextSecondary,
				},
			},
		},
	}
}

func agentQuietButton(testID, label string, p SpecPalette, onPress func()) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  label,
		Style: core.Style{
			Height:         core.Pt(30),
			Padding:        core.Symmetric(0, 10),
			CornerRadius:   8,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FocusVisible:   p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if onPress != nil {
				onPress()
			}
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      p.TextMuted,
				},
			},
		},
	}
}

// agentPartialSection renders the Partial outcome card, pointing prompt, kept confirmation,
// and review strip matching design/editor/AgentStates.dc.html (Partial column).
func agentPartialSection(ed *Editor, turnIdx int, turn AgentTurn, p SpecPalette) []core.Node {
	ps := turn.Partial
	if ps == nil {
		return nil
	}
	var out []core.Node
	if !ps.Kept && !ps.TryingAnother {
		cardKids := []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.TriangleAlert(),
						Style: core.Style{
							Width:      core.Pt(15),
							Height:     core.Pt(15),
							Color:      p.Warning.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID:  fmt.Sprintf("agent-partial-title-%d", turnIdx),
						Content: ps.EffectiveTitle(),
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12.5,
							FontWeight: 600,
							Color:      p.Warning.Fg,
						},
					},
				},
			},
		}
		if strings.TrimSpace(ps.Reason) != "" {
			cardKids = append(cardKids, core.Text{
				TestID:  fmt.Sprintf("agent-partial-reason-%d", turnIdx),
				Content: ps.Reason,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			})
		}
		if ps.Pointing {
			cardKids = append(cardKids, core.View{
				TestID: fmt.Sprintf("agent-partial-point-banner-%d", turnIdx),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					MinHeight:    core.Pt(32),
					Padding:      core.Symmetric(6, 12),
					Gap:          8,
					CornerRadius: 8,
					Background:   p.Bg,
					BorderWidth:  1,
					BorderColor:  p.Accent.Edge,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Crosshair(),
						Style: core.Style{
							Width:      core.Pt(14),
							Height:     core.Pt(14),
							Color:      p.Accent.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						Content: ps.EffectivePointPrompt(),
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12.5,
							Color:      p.TextSecondary,
						},
					},
					core.Text{
						Content: "Esc",
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11,
							Color:      p.TextFaint,
							FlexShrink: 0,
						},
					},
				},
			})
		}
		turnID := turn.ID
		cardKids = append(cardKids, core.View{
			Style: core.Style{
				Direction: core.Row,
				Wrap:      core.Wrap,
				Gap:       8,
			},
			Children: []core.Node{
				agentAccentButton(fmt.Sprintf("agent-partial-keep-%d", turnIdx), "Keep these", p, func() {
					ed.KeepAgentPartial(turnID)
				}),
				agentSecondaryButton(fmt.Sprintf("agent-partial-point-%d", turnIdx), "Let me point", ps.Pointing, p, func() {
					ed.ToggleAgentPointing(turnID)
				}),
				agentSecondaryButton(fmt.Sprintf("agent-partial-try-%d", turnIdx), "Try another way", false, p, func() {
					ed.report(ed.TryAnotherWayAgentTurn(turnID))
				}),
			},
		})
		out = append(out, core.View{
			TestID: fmt.Sprintf("agent-partial-card-%d", turnIdx),
			Role:   core.RoleRegion,
			Label:  ps.EffectiveTitle(),
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				Padding:      core.All(12),
				Gap:          10,
			},
			Children: cardKids,
		})
	} else if ps.Kept {
		count := 0
		if turn.Review != nil {
			count = turn.Review.Count()
		}
		msg := "Kept changes · ⌘Z undoes"
		if count > 0 && ps.Target != "" {
			msg = fmt.Sprintf("Kept %d changes · %s left out · ⌘Z undoes", count, ps.Target)
		} else if count > 0 {
			msg = fmt.Sprintf("Kept %d changes · ⌘Z undoes", count)
		} else if ps.Target != "" {
			msg = fmt.Sprintf("Kept changes · %s left out · ⌘Z undoes", ps.Target)
		}
		out = append(out, core.View{
			TestID: fmt.Sprintf("agent-partial-kept-%d", turnIdx),
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Check(),
					Style: core.Style{
						Width:      core.Pt(14),
						Height:     core.Pt(14),
						Color:      p.Added.Fg,
						FlexShrink: 0,
					},
				},
				core.Text{
					Content: msg,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontSize:   12.5,
						Color:      p.TextMuted,
					},
				},
			},
		})
	}

	if turn.Review != nil && turn.Review.Count() > 0 && !turn.Review.Discarded {
		out = append(out, agentReviewSummaryBar(ed, turnIdx, turn.ID, turn.Review, p))
	}
	return out
}

func agentReviewSummaryBar(ed *Editor, turnIdx int, turnID string, rev *AgentTurnReview, p SpecPalette) core.Node {
	n := rev.Count()
	changesText := fmt.Sprintf("%d changes · 1 file", n)
	if n == 1 {
		changesText = "1 change · 1 file"
	}
	return core.View{
		TestID: fmt.Sprintf("agent-review-bar-%d", turnIdx),
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.Symmetric(8, 12),
			Gap:            8,
			CornerRadius:   12,
			Background:     p.Raised,
			BorderWidth:    1,
			BorderColor:    p.LineRaised,
		},
		Children: []core.Node{
			core.Text{
				Content: changesText,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      p.TextPrimary,
				},
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.Text{
						Content: fmt.Sprintf("+%d", rev.PlusLines),
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      p.Added.Fg,
						},
					},
					core.Text{
						Content: fmt.Sprintf("−%d", rev.MinusLines),
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      p.TextFaint,
						},
					},
				},
			},
			core.View{
				TestID: fmt.Sprintf("agent-review-open-%d", turnIdx),
				Role:   core.RoleButton,
				Label:  "Review",
				Style: core.Style{
					Height:         core.Pt(24),
					Padding:        core.Symmetric(0, 6),
					CornerRadius:   6,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.OpenAgentTurnReview(turnID)
				},
				Children: []core.Node{
					core.Text{
						Content: "Review",
						Style: core.Style{
							FontSize:   12.5,
							FontWeight: 500,
							Color:      p.Accent.Fg,
						},
					},
				},
			},
		},
	}
}

// agentStuckSection renders the Stuck warning card with the 3 refused calls & errors,
// Hint input, Continue with hint, and Stop here matching design/editor/AgentStates.dc.html.
func agentStuckSection(ed *Editor, rev *core.Signal[uint64], turnIdx int, turn AgentTurn, p SpecPalette) []core.Node {
	st := turn.Stuck
	if st == nil {
		return nil
	}
	turnID := turn.ID
	if st.Resolution == AgentStuckStopped {
		msg := "Stopped. Nothing was changed; the failed edits were refused before they touched the file."
		if !st.NothingChanged {
			msg = "Stopped. The failed edits were refused before they touched the file; earlier changes stay in review."
		}
		return []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("agent-stuck-stopped-%d", turnIdx),
				Content: msg,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextMuted,
				},
			},
		}
	}
	if st.Resolution == AgentStuckContinued {
		return nil
	}

	failRows := make([]core.Node, 0, len(st.Failures)*2)
	for i, f := range st.Failures {
		failRows = append(failRows,
			core.Text{
				TestID:  fmt.Sprintf("agent-stuck-call-%d-%d", turnIdx, i),
				Content: fmt.Sprintf("%d %s", i+1, f.Call),
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextSecondary,
				},
			},
			core.Text{
				TestID:  fmt.Sprintf("agent-stuck-err-%d-%d", turnIdx, i),
				Content: f.Error,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.Removed.Fg,
					Padding:    core.Edges{Left: 14},
				},
			},
		)
	}

	return []core.Node{
		core.View{
			TestID: fmt.Sprintf("agent-stuck-card-%d", turnIdx),
			Role:   core.RoleRegion,
			Label:  "Paused instead of guessing again",
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				Padding:      core.All(12),
				Gap:          10,
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
							SVG: lucide.Pause(),
							Style: core.Style{
								Width:      core.Pt(15),
								Height:     core.Pt(15),
								Color:      p.Warning.Fg,
								FlexShrink: 0,
							},
						},
						core.Text{
							TestID:  fmt.Sprintf("agent-stuck-title-%d", turnIdx),
							Content: "Paused instead of guessing again",
							Style: core.Style{
								FlexGrow:   1,
								FlexShrink: 1,
								MinWidth:   core.Pt(0),
								FontSize:   12.5,
								FontWeight: 600,
								Color:      p.Warning.Fg,
							},
						},
					},
				},
				core.View{
					TestID: fmt.Sprintf("agent-stuck-failures-%d", turnIdx),
					Style: core.Style{
						CornerRadius: 8,
						Background:   p.Bg,
						Padding:      core.Symmetric(8, 12),
						Gap:          3,
					},
					Children: failRows,
				},
				core.View{
					Style: core.Style{Gap: 4},
					Children: []core.Node{
						core.Text{
							Content: "Hint for the agent",
							Style: core.Style{
								FontSize: 12,
								Color:    p.TextSecondary,
							},
						},
						ui.Input{
							TestID:      fmt.Sprintf("agent-stuck-hint-%d", turnIdx),
							Label:       "Hint for the agent",
							Placeholder: "Tell the agent what to use…",
							Value: func() string {
								_ = rev.Get()
								if t := ed.findAgentTurn(turnID); t != nil && t.Stuck != nil {
									return t.Stuck.HintDraft
								}
								return ""
							},
							OnChange: func(val string) {
								ed.SetAgentStuckHint(turnID, val)
							},
							OnSubmit: func(val string) {
								ed.SetAgentStuckHint(turnID, val)
								ed.report(ed.ContinueAgentWithHint(turnID))
							},
							Style: core.Style{
								Height:           core.Pt(32),
								CornerRadius:     6,
								Background:       p.Bg,
								BorderWidth:      1,
								BorderColor:      p.Accent.Edge,
								Padding:          core.Symmetric(0, 10),
								FontSize:         12.5,
								Color:            p.TextPrimary,
								PlaceholderColor: p.TextFaint,
								FocusVisible:     p.FocusRing(2),
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Wrap:      core.Wrap,
						Gap:       8,
					},
					Children: []core.Node{
						agentAccentButton(fmt.Sprintf("agent-stuck-continue-%d", turnIdx), "Continue with hint", p, func() {
							ed.report(ed.ContinueAgentWithHint(turnID))
						}),
						agentSecondaryButton(fmt.Sprintf("agent-stuck-stop-%d", turnIdx), "Stop here", false, p, func() {
							ed.StopStuckAgentTurn(turnID)
						}),
					},
				},
			},
		},
	}
}

// agentLostSection renders the Lost card (connection lost mid-request or model API error),
// plus the post-reconnect "ask before picking up" confirmation card.
func agentLostSection(ed *Editor, turnIdx int, turn AgentTurn, p SpecPalette) []core.Node {
	lost := turn.Lost
	if lost == nil {
		return nil
	}
	turnID := turn.ID
	var out []core.Node

	if lost.Kind == AgentLostModelError {
		var btns []core.Node
		btns = append(btns, agentSecondaryButton(fmt.Sprintf("agent-lost-retry-%d", turnIdx), "Retry now", false, p, func() {
			ed.report(ed.RetryAgentModelErrorNow(turnID))
		}))
		if lost.FallbackAgent != "" {
			btns = append(btns, agentQuietButton(fmt.Sprintf("agent-lost-fallback-%d", turnIdx), "Use "+lost.FallbackAgent, p, func() {
				ed.report(ed.SwitchAgentFallback(turnID))
			}))
		}
		out = append(out, core.View{
			TestID: fmt.Sprintf("agent-lost-card-%d", turnIdx),
			Role:   core.RoleRegion,
			Label:  lost.EffectiveTitle(),
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
					TestID:  fmt.Sprintf("agent-lost-title-%d", turnIdx),
					Content: lost.EffectiveTitle(),
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.Removed.Fg,
					},
				},
				core.Text{
					TestID:  fmt.Sprintf("agent-lost-body-%d", turnIdx),
					Content: lost.EffectiveBody(),
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
					Children: btns,
				},
			},
		})
		return out
	}

	// Connection lost card.
	reviewCount := 0
	if turn.Review != nil && !turn.Review.Discarded {
		reviewCount = turn.Review.Count()
	}
	btns := []core.Node{
		agentAccentButton(fmt.Sprintf("agent-lost-reconnect-%d", turnIdx), "Reconnect", p, func() {
			ed.ReconnectAgent()
		}),
	}
	if reviewCount > 0 {
		label := fmt.Sprintf("Review %d changes", reviewCount)
		if reviewCount == 1 {
			label = "Review 1 change"
		}
		btns = append(btns, agentSecondaryButton(fmt.Sprintf("agent-lost-review-%d", turnIdx), label, false, p, func() {
			ed.OpenAgentTurnReview(turnID)
		}))
	}
	btns = append(btns, agentQuietButton(fmt.Sprintf("agent-lost-troubleshoot-%d", turnIdx), "Troubleshoot", p, func() {
		ed.TroubleshootAgentConnection()
	}))

	out = append(out, core.View{
		TestID: fmt.Sprintf("agent-lost-card-%d", turnIdx),
		Role:   core.RoleRegion,
		Label:  lost.EffectiveTitle(),
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Removed.Fill,
			BorderWidth:  1,
			BorderColor:  p.Removed.Edge,
			Padding:      core.All(12),
			Gap:          10,
		},
		Children: []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("agent-lost-title-%d", turnIdx),
				Content: lost.EffectiveTitle(),
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 600,
					Color:      p.Removed.Fg,
				},
			},
			core.Text{
				TestID:  fmt.Sprintf("agent-lost-body-%d", turnIdx),
				Content: lost.EffectiveBody(),
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
				Children: btns,
			},
		},
	})

	nextStep := lost.DuringStep
	if nextStep <= 0 {
		nextStep = lost.AfterStep + 1
	}
	if lost.AskResumePending {
		out = append(out, core.View{
			TestID: fmt.Sprintf("agent-lost-ask-resume-%d", turnIdx),
			Role:   core.RoleRegion,
			Label:  fmt.Sprintf("Pick up at step %d?", nextStep),
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				Padding:      core.All(12),
				Gap:          10,
			},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("Reconnected · pick up at step %d?", nextStep),
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.Warning.Fg,
					},
				},
				core.Text{
					Content: fmt.Sprintf("Steps 1–%d are already done. Continue from step %d?", max(1, lost.AfterStep), nextStep),
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
						agentAccentButton(fmt.Sprintf("agent-lost-resume-confirm-%d", turnIdx), fmt.Sprintf("Continue at step %d", nextStep), p, func() {
							ed.report(ed.ConfirmResumeLostAgentTurn(turnID))
						}),
						agentSecondaryButton(fmt.Sprintf("agent-lost-resume-dismiss-%d", turnIdx), "Not now", false, p, func() {
							ed.DismissResumeLostAgentTurn(turnID)
						}),
					},
				},
			},
		})
	} else {
		out = append(out, core.Text{
			TestID:  fmt.Sprintf("agent-lost-caption-%d", turnIdx),
			Content: fmt.Sprintf("After reconnecting, the agent asks before it picks up at step %d.", nextStep),
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}
	return out
}

// agentStoppedSection renders the Stopped card with DONE and NOT DONE sections,
// Resume (skips done steps), Review N changes, and caption matching design/editor/AgentStates.dc.html.
func agentStoppedSection(ed *Editor, turnIdx int, turn AgentTurn, p SpecPalette) []core.Node {
	st := turn.StoppedState
	if st == nil {
		return nil
	}
	turnID := turn.ID
	var doneNodes []core.Node
	var todoNodes []core.Node
	firstTodoTitle := ""
	for si, step := range turn.Steps {
		if step.Status == AgentStepDone {
			doneNodes = append(doneNodes, agentStepRow(turnIdx, si, step, p))
		} else {
			if firstTodoTitle == "" {
				firstTodoTitle = step.Title
			}
			todoNodes = append(todoNodes, agentStepRow(turnIdx, si, step, p))
		}
	}

	cardKids := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Square(),
					Style: core.Style{
						Width:      core.Pt(15),
						Height:     core.Pt(15),
						Color:      p.TextPrimary,
						FlexShrink: 0,
					},
				},
				core.Text{
					TestID:  fmt.Sprintf("agent-stopped-title-%d", turnIdx),
					Content: st.EffectiveTitle(),
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
			},
		},
	}

	if len(doneNodes) > 0 {
		cardKids = append(cardKids,
			core.Text{
				Content: "DONE",
				Style: core.Style{
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.6,
					Color:         p.TextFaint,
				},
			},
			core.View{
				TestID:   fmt.Sprintf("agent-stopped-done-%d", turnIdx),
				Style:    core.Style{Gap: 8},
				Children: doneNodes,
			},
		)
	}

	if len(todoNodes) > 0 {
		cardKids = append(cardKids,
			core.Text{
				Content: "NOT DONE",
				Style: core.Style{
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.6,
					Color:         p.TextFaint,
				},
			},
			core.View{
				TestID:   fmt.Sprintf("agent-stopped-todo-%d", turnIdx),
				Style:    core.Style{Gap: 8},
				Children: todoNodes,
			},
		)
	}

	reviewCount := 0
	if turn.Review != nil && !turn.Review.Discarded {
		reviewCount = turn.Review.Count()
	}

	if !st.Resumed {
		var btns []core.Node
		if len(todoNodes) > 0 {
			btns = append(btns, agentAccentButton(fmt.Sprintf("agent-stopped-resume-%d", turnIdx), "Resume", p, func() {
				ed.report(ed.ResumeAgentTurn(turnID))
			}))
		}
		if reviewCount > 0 {
			label := fmt.Sprintf("Review %d changes", reviewCount)
			if reviewCount == 1 {
				label = "Review 1 change"
			}
			btns = append(btns, agentSecondaryButton(fmt.Sprintf("agent-stopped-review-%d", turnIdx), label, false, p, func() {
				ed.OpenAgentTurnReview(turnID)
			}))
		}
		if len(btns) > 0 {
			cardKids = append(cardKids, core.View{
				Style: core.Style{
					Direction: core.Row,
					Wrap:      core.Wrap,
					Gap:       8,
				},
				Children: btns,
			})
		}
	}

	startLabel := "the first step not done"
	if firstTodoTitle != "" {
		startLabel = lowerFirstRune(firstTodoTitle)
	}
	caption := fmt.Sprintf("Resume starts at %s; steps already done are not redone.", startLabel)
	if reviewCount > 0 && turn.Review != nil && !turn.Review.Kept {
		if reviewCount == 1 {
			caption += " The 1 change stays unkept until you review it."
		} else {
			caption += fmt.Sprintf(" The %d changes stay unkept until you review them.", reviewCount)
		}
	}

	return []core.Node{
		core.View{
			TestID: fmt.Sprintf("agent-stopped-card-%d", turnIdx),
			Role:   core.RoleRegion,
			Label:  st.EffectiveTitle(),
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Padding:      core.All(12),
				Gap:          10,
			},
			Children: cardKids,
		},
		core.Text{
			TestID:  fmt.Sprintf("agent-stopped-caption-%d", turnIdx),
			Content: caption,
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		},
	}
}

// agentMismatchSection renders the Mismatch warning card with the WHAT / SUMMARY / REVIEW table,
// known reason, Ask why, Open review, and trust-the-review caption matching design/editor/AgentStates.dc.html.
func agentMismatchSection(ed *Editor, turnIdx int, turn AgentTurn, p SpecPalette) []core.Node {
	mm := turn.Mismatch
	if mm == nil {
		return nil
	}
	turnID := turn.ID

	tableRows := []core.Node{
		core.View{
			Style: core.Style{
				Direction: core.Row,
				Padding:   core.Symmetric(8, 12),
				Gap:       8,
			},
			Children: []core.Node{
				core.Text{
					Content: "WHAT",
					Style: core.Style{
						FlexGrow:      1,
						FlexBasis:     core.Pt(0),
						FontSize:      11,
						FontWeight:    600,
						LetterSpacing: 0.6,
						Color:         p.TextFaint,
					},
				},
				core.Text{
					Content: "SUMMARY",
					Style: core.Style{
						Width:         core.Pt(68),
						FontSize:      11,
						FontWeight:    600,
						LetterSpacing: 0.6,
						Color:         p.TextFaint,
					},
				},
				core.Text{
					Content: "REVIEW",
					Style: core.Style{
						Width:         core.Pt(68),
						FontSize:      11,
						FontWeight:    600,
						LetterSpacing: 0.6,
						Color:         p.TextFaint,
					},
				},
			},
		},
	}

	for ri, row := range mm.Rows {
		rowCol := p.TextMuted
		revCol := p.TextMuted
		revWeight := core.FontWeight(400)
		if row.Disagrees {
			rowCol = p.TextSecondary
			revCol = p.Warning.Fg
			revWeight = 600
		}
		tableRows = append(tableRows, core.View{
			TestID: fmt.Sprintf("agent-mismatch-row-%d-%d", turnIdx, ri),
			Style: core.Style{
				Direction:   core.Row,
				Padding:     core.Symmetric(8, 12),
				Gap:         8,
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				core.Text{
					Content: row.What,
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						MinWidth:  core.Pt(0),
						FontSize:  12.5,
						Color:     rowCol,
					},
				},
				core.Text{
					Content: row.Summary,
					Style: core.Style{
						Width:    core.Pt(68),
						FontSize: 12.5,
						Color:    rowCol,
					},
				},
				core.Text{
					Content: row.Review,
					Style: core.Style{
						Width:      core.Pt(68),
						FontSize:   12.5,
						FontWeight: revWeight,
						Color:      revCol,
					},
				},
			},
		})
	}

	cardKids := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.TriangleAlert(),
					Style: core.Style{
						Width:      core.Pt(15),
						Height:     core.Pt(15),
						Color:      p.Warning.Fg,
						FlexShrink: 0,
					},
				},
				core.Text{
					TestID:  fmt.Sprintf("agent-mismatch-title-%d", turnIdx),
					Content: mm.Title,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.Warning.Fg,
					},
				},
			},
		},
		core.View{
			TestID: fmt.Sprintf("agent-mismatch-table-%d", turnIdx),
			Style: core.Style{
				CornerRadius: 8,
				Background:   p.Bg,
				Overflow:     core.Hidden,
			},
			Children: tableRows,
		},
	}

	if strings.TrimSpace(mm.Reason) != "" {
		cardKids = append(cardKids, core.Text{
			TestID:  fmt.Sprintf("agent-mismatch-reason-%d", turnIdx),
			Content: mm.Reason,
			Style: core.Style{
				FontSize: 12.5,
				Color:    p.TextSecondary,
			},
		})
	}

	cardKids = append(cardKids, core.View{
		Style: core.Style{
			Direction: core.Row,
			Wrap:      core.Wrap,
			Gap:       8,
		},
		Children: []core.Node{
			agentSecondaryButton(fmt.Sprintf("agent-mismatch-ask-why-%d", turnIdx), "Ask why", false, p, func() {
				ed.report(ed.AskAgentWhyMismatch(turnID))
			}),
			agentSecondaryButton(fmt.Sprintf("agent-mismatch-open-review-%d", turnIdx), "Open review", false, p, func() {
				ed.OpenAgentTurnReview(turnID)
			}),
		},
	})

	return []core.Node{
		core.View{
			TestID: fmt.Sprintf("agent-mismatch-card-%d", turnIdx),
			Role:   core.RoleRegion,
			Label:  mm.Title,
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				Padding:      core.All(12),
				Gap:          10,
			},
			Children: cardKids,
		},
		core.Text{
			TestID:  fmt.Sprintf("agent-mismatch-caption-%d", turnIdx),
			Content: "The editor checks every summary against the actual diff. The review, not the summary, is what Keep applies.",
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		},
	}
}

// agentOfflineAndQueueSection renders the "You're offline" notice card (while offline) and the
// QUEUED · N list (while offline AND after returning online until each item is explicitly sent or removed).
func agentOfflineAndQueueSection(ed *Editor, p SpecPalette) []core.Node {
	offline := ed.AgentOffline()
	allQueued := ed.AgentQueuedRequests()
	var queue []AgentQueuedRequest
	for _, q := range allQueued {
		if q.Offline {
			queue = append(queue, q)
		}
	}
	if !offline && len(queue) == 0 {
		return nil
	}
	var out []core.Node
	if offline {
		out = append(out, core.View{
			TestID: "agent-offline-card",
			Role:   core.RoleRegion,
			Label:  "You're offline",
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Padding:      core.All(12),
				Gap:          6,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-offline-title",
					Content: "You're offline",
					Style: core.Style{
						FontSize:   13,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
				core.Text{
					TestID:  "agent-offline-body",
					Content: "Select, move, the inspector, game check and Play all work. Requests wait here.",
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextSecondary,
					},
				},
			},
		})
	}

	out = append(out, core.Text{
		TestID:  "agent-queue-header",
		Content: fmt.Sprintf("QUEUED · %d", len(queue)),
		Style: core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.6,
			Color:         p.TextFaint,
		},
	})

	if len(queue) == 0 {
		out = append(out, core.Text{
			TestID:  "agent-queue-empty",
			Content: "Nothing queued.",
			Style: core.Style{
				FontSize: 12.5,
				Color:    p.TextFaint,
			},
		})
	} else {
		rowNodes := make([]core.Node, 0, len(queue))
		for qi, q := range queue {
			qid := q.ID
			meta := "queued"
			if !q.QueuedAt.IsZero() {
				meta = "queued " + q.QueuedAt.Format("15:04")
			}
			rowKids := []core.Node{
				core.View{
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						Gap:        2,
					},
					Children: []core.Node{
						core.Text{
							TestID:  fmt.Sprintf("agent-queue-text-%d", qi),
							Content: q.Prompt,
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextSecondary,
							},
						},
						core.Text{
							TestID:  fmt.Sprintf("agent-queue-meta-%d", qi),
							Content: meta,
							Style: core.Style{
								FontSize: 11.5,
								Color:    p.TextFaint,
							},
						},
					},
				},
			}
			if !offline && !ed.AgentWeeklyLimitReached() {
				rowKids = append(rowKids, core.View{
					TestID: fmt.Sprintf("agent-queue-send-%d", qi),
					Role:   core.RoleButton,
					Label:  fmt.Sprintf("Send %q", q.Prompt),
					Style: core.Style{
						Height:         core.Pt(26),
						Padding:        core.Symmetric(0, 10),
						CornerRadius:   6,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Background:     p.Accent.Fg,
						FocusVisible:   p.FocusRing(2),
						FlexShrink:     0,
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.SendQueuedAgentRequest(qid))
					},
					Children: []core.Node{
						core.Text{
							Content: "Send",
							Style: core.Style{
								FontSize:   11.5,
								FontWeight: 600,
								Color:      p.Accent.OnFill,
							},
						},
					},
				})
			}
			rowKids = append(rowKids, core.View{
				TestID: fmt.Sprintf("agent-queue-remove-%d", qi),
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("Remove “%s” from the queue", q.Prompt),
				Style: core.Style{
					Width:          core.Pt(28),
					Height:         core.Pt(28),
					CornerRadius:   6,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
					FlexShrink:     0,
				},
				OnPress: func(core.PressEvent) {
					ed.RemoveQueuedAgentRequest(qid)
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.X(),
						Style: core.Style{
							Width:  core.Pt(14),
							Height: core.Pt(14),
							Color:  p.TextMuted,
						},
					},
				},
			})
			rowNodes = append(rowNodes, core.View{
				TestID: fmt.Sprintf("agent-queue-item-%d", qi),
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
				Children: rowKids,
			})
		}
		out = append(out, core.View{
			TestID:   "agent-queue-list",
			Style:    core.Style{Gap: 8},
			Children: rowNodes,
		})
	}

	out = append(out, core.Text{
		TestID:  "agent-queue-caption",
		Content: "Back online, nothing runs on its own: each queued request waits for you to send it.",
		Style: core.Style{
			FontSize: 12,
			Color:    p.TextFaint,
		},
	})
	return out
}

// agentTurnReviewDrawer renders the inline review card for a turn's actual scene diff with Keep and Discard.
func agentTurnReviewDrawer(ed *Editor, review *AgentTurnReview, p SpecPalette) core.Node {
	if review == nil {
		return nil
	}
	turnID := review.TurnID
	var rows []core.Node
	addRow := func(sign string, col core.Color, obj SceneObjectChange) {
		detail := obj.Name
		if obj.Summary != "" && obj.Summary != "added" && obj.Summary != "removed" {
			detail += " · " + obj.Summary
		}
		rows = append(rows, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
				Padding:    core.Symmetric(4, 0),
			},
			Children: []core.Node{
				core.Text{
					Content: sign,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   12,
						FontWeight: 600,
						Color:      col,
						Width:      core.Pt(12),
					},
				},
				core.Text{
					Content: detail,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontSize:   12,
						Color:      p.TextSecondary,
					},
				},
			},
		})
	}
	for _, a := range review.Added {
		addRow("+", p.Added.Fg, a)
	}
	for _, c := range review.Changed {
		addRow("~", p.Changed.Fg, c)
	}
	for _, r := range review.Removed {
		addRow("−", p.Removed.Fg, r)
	}
	if len(rows) == 0 {
		rows = append(rows, core.Text{
			Content: "No object changes in this review.",
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}

	var actionBtns []core.Node
	if !review.Kept && !review.Discarded {
		actionBtns = append(actionBtns,
			agentAccentButton("agent-review-keep", "Keep", p, func() {
				ed.KeepAgentTurnDiffReview(turnID)
			}),
			agentSecondaryButton("agent-review-discard", "Discard", false, p, func() {
				ed.DiscardAgentTurnReview(turnID)
			}),
		)
	}
	actionBtns = append(actionBtns, agentQuietButton("agent-review-close", "Close", p, func() {
		ed.CloseAgentTurnReview()
	}))

	return core.View{
		TestID: "agent-review-drawer",
		Role:   core.RoleRegion,
		Label:  "Review changes",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.Accent.Edge,
			Padding:      core.All(12),
			Gap:          10,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            8,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "agent-review-drawer-title",
						Content: fmt.Sprintf("Review · %s (%d changes)", review.File, review.Count()),
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12.5,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
					core.Text{
						Content: fmt.Sprintf("+%d −%d", review.PlusLines, review.MinusLines),
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      p.Added.Fg,
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					CornerRadius: 8,
					Background:   p.Bg,
					Padding:      core.Symmetric(6, 10),
				},
				Children: rows,
			},
			core.View{
				Style: core.Style{
					Direction: core.Row,
					Gap:       8,
				},
				Children: actionBtns,
			},
		},
	}
}

// viewportAgentPointingOverlay renders a compact Spec pill at the top center of the viewport while
// "Let me point" is active so the author sees what clicking will place and that Esc cancels.
func viewportAgentPointingOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.AgentPointingActive()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				p := SpecPaletteFor(darkMemo.Get())
				return core.View{
					TestID:        "viewport-agent-pointing-pill",
					PointerEvents: core.PointerEventsNone,
					Style: core.Style{
						Position:       core.Absolute,
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Top:            core.Pt(12),
						Direction:      core.Row,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          8,
								Height:       core.Pt(32),
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 10,
								Background:   p.PillBg,
								BorderWidth:  1.5,
								BorderColor:  p.Accent.Edge,
							},
							Children: []core.Node{
								core.SVGView{
									SVG: lucide.Crosshair(),
									Style: core.Style{
										Width:  core.Pt(14),
										Height: core.Pt(14),
										Color:  p.Accent.Fg,
									},
								},
								core.Text{
									ContentFunc: func() string {
										_ = rev.Get()
										return ed.AgentPointingPrompt()
									},
									Style: core.Style{
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextSecondary,
									},
								},
								core.Text{
									Content: "Esc",
									Style: core.Style{
										FontFamily: core.FontMonospace,
										FontSize:   11,
										Color:      p.TextFaint,
									},
								},
							},
						},
					},
				}
			})
		},
	}
}
