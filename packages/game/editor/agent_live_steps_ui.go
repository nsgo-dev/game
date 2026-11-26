package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func moveAgentCardFocus(refs []*core.Ref, delta int) {
	idx := -1
	for i, r := range refs {
		if r != nil && r.HoldsFocus() {
			idx = i
			break
		}
	}
	next := 0
	if idx >= 0 {
		next = (idx + delta + len(refs)) % len(refs)
	}
	if refMounted(refs[next]) {
		refs[next].Focus()
	}
}

func agentRunningSpinnerIcon(col core.Color) core.Node {
	return core.Canvas{
		Style: core.Style{Width: core.Pt(16), Height: core.Pt(16), FlexShrink: 0},
		Draw: func(d *core.Drawing, _ core.Size) {
			d.SetStrokeColor(col).SetLineWidth(2).SetLineCap(core.CapRound)
			d.BeginPath().Arc(8, 8, 6.2, -float32(math.Pi/2), float32(math.Pi), false).Stroke()
		},
	}
}

func agentLiveStepRow(ed *Editor, turnIdx, stepIdx int, step AgentStep, p SpecPalette) core.Node {
	testID := fmt.Sprintf("agent-step-%d-%d", turnIdx, stepIdx)
	if step.Status == AgentStepPlaceholder {
		testID = "agent-step-placeholder"
	}

	var badge core.Node
	textCol := p.TextMuted
	switch step.Status {
	case AgentStepPlaceholder:
		textCol = p.TextFaint
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(10),
						Height:       core.Pt(10),
						CornerRadius: 5,
						Background:   p.LineRaised,
					},
				},
			},
		}
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
		badge = agentRunningSpinnerIcon(p.Agent.Fg)
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
	case AgentStepDenied:
		textCol = p.TextPrimary
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.LineRaised,
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
						Color:  p.TextMuted,
					},
				},
			},
		}
	case AgentStepBlocked:
		textCol = p.TextPrimary
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Lock(),
					Style: core.Style{
						Width:  core.Pt(10),
						Height: core.Pt(10),
						Color:  p.TextSecondary,
					},
				},
			},
		}
	case AgentStepStopped:
		textCol = p.TextPrimary
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				CornerRadius:   8,
				Background:     p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 1.5,
						Background:   p.TextPrimary,
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

	contentKids := []core.Node{
		core.Text{
			Content: label,
			Style: core.Style{
				FontSize:   12.5,
				LineHeight: 18,
				Color:      textCol,
			},
		},
	}
	if step.Status == AgentStepBlocked && step.RuleSummary != "" {
		ruleIdx := step.RuleIndex
		contentKids = append(contentKids, core.View{
			TestID:    "agent-step-blocked-rule-" + step.ID,
			Role:      core.RoleLink,
			Label:     step.RuleSummary,
			Focusable: func() bool { return true },
			Style: core.Style{
				Padding:      core.Symmetric(6, 10),
				CornerRadius: 8,
				Background:   p.Inset,
				BorderWidth:  1,
				BorderColor:  p.Line,
				FocusVisible: p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.OpenPermissionsSettings(ruleIdx)
			},
			Children: []core.Node{
				core.Text{
					Content: step.RuleSummary,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextPrimary,
					},
				},
			},
		})
	}

	rowKids := []core.Node{
		badge,
		core.View{
			Style: core.Style{
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Gap:        4,
			},
			Children: contentKids,
		},
	}

	if step.Status == AgentStepDenied && step.CanAllowNow {
		stepID := step.ID
		rowKids = append(rowKids, core.View{
			TestID:    "agent-step-allow-now-" + stepID,
			Role:      core.RoleButton,
			Label:     ed.T("agent.allowNow"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(28),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.AllowDeniedStepNow(stepID)
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("agent.allowNow"),
					Style: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.Accent.Fg,
					},
				},
			},
		})
	}

	if step.Status == AgentStepStopped {
		rowKids = append(rowKids, core.View{
			TestID:    "agent-step-review",
			Role:      core.RoleButton,
			Label:     ed.T("agent.review"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(28),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.FocusAgentTurnReview()
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("agent.review"),
					Style: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.Accent.Fg,
					},
				},
			},
		})
	}

	return core.View{
		TestID: testID,
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
		},
		Children: rowKids,
	}
}

func agentFollowUpBubble(p SpecPalette, testID, text string) core.Node {
	return core.View{
		TestID: testID,
		Style: core.Style{
			AlignSelf:    core.AlignEnd,
			MaxWidth:     core.Pt(296),
			Padding:      core.Symmetric(8, 12),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: []core.Node{
			core.Text{
				Content: text,
				Style: core.Style{
					FontSize:   12.5,
					LineHeight: 19,
					Color:      p.TextSecondary,
				},
			},
		},
	}
}

func agentPermissionCardView(
	p SpecPalette,
	ed *Editor,
	ask AgentPermissionPrompt,
	osKind core.OS,
	cardRef, allowOnceRef, allowAlwaysRef, denyRef *core.Ref,
) core.Node {
	bodyText := strings.TrimSpace(ask.Why)
	if ask.Effect != "" {
		if bodyText != "" {
			bodyText += " " + ask.Effect
		} else {
			bodyText = ask.Effect
		}
	}
	alwaysLabel := ask.AlwaysLabel
	if alwaysLabel == "" {
		alwaysLabel = ed.T("permissions.allowAlways")
	}

	allowOnceHint := FormatShortcut("mod+enter", osKind)
	allowAlwaysHint := FormatShortcut("alt+enter", osKind)
	denyHint := FormatShortcut("mod+backspace", osKind)

	permButton := func(ref *core.Ref, testID, label, hint string, bg, border, fg, hintBg, hintFg core.Color, weight core.FontWeight, onPress func()) core.Node {
		st := core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(32),
			Padding:      core.Edges{Left: 12, Right: 8},
			Gap:          8,
			CornerRadius: 8,
			Background:   bg,
			FocusVisible: p.FocusRing(2),
		}
		if border != core.Transparent {
			st.BorderWidth = 1
			st.BorderColor = border
		}
		return core.View{
			Ref:       ref,
			TestID:    testID,
			Role:      core.RoleButton,
			Label:     label,
			Focusable: func() bool { return true },
			Style:     st,
			OnPress: func(core.PressEvent) {
				onPress()
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						FlexGrow:   1,
						FontSize:   12.5,
						FontWeight: weight,
						Color:      fg,
					},
				},
				core.View{
					Style: core.Style{
						Height:         core.Pt(20),
						Padding:        core.Symmetric(0, 6),
						CornerRadius:   6,
						Background:     hintBg,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{
						core.Text{
							Content: hint,
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   11,
								FontWeight: 500,
								Color:      hintFg,
							},
						},
					},
				},
			},
		}
	}

	ruleBoxKids := []core.Node{
		core.Text{
			Content: ed.T("agent.alwaysAddsRule"),
			Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
		},
	}
	if ask.RulePreview != "" {
		ruleBoxKids = append(ruleBoxKids, core.Text{
			TestID:  "agent-perm-rule-preview",
			Content: ask.RulePreview,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.TextPrimary,
			},
		})
	}
	if ask.RuleSource != "" {
		ruleBoxKids = append(ruleBoxKids, core.Text{
			TestID:  "agent-perm-rule-source",
			Content: ask.RuleSource,
			Style:   core.Style{FontSize: 11.5, LineHeight: 16, Color: p.TextFaint},
		})
	}

	return core.View{
		Ref:        cardRef,
		TestID:     "agent-permission-card",
		Role:       core.RoleAlertDialog,
		Label:      ask.Title,
		LiveRegion: core.LiveRegionPolite,
		OnFocusIn: func() {
			ed.setLivePermissionCardFocused(true)
		},
		OnFocusOut: func() {
			if cardRef == nil || !cardRef.HoldsFocus() {
				ed.setLivePermissionCardFocused(false)
			}
		},
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Warning.Fill,
			BorderWidth:  1,
			BorderColor:  p.Warning.Edge,
			Padding:      core.All(12),
			Gap:          12,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Play(),
						Style: core.Style{
							Width:      core.Pt(14),
							Height:     core.Pt(14),
							Color:      p.Warning.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID:  "agent-perm-title",
						Content: ask.Title,
						Style: core.Style{
							FontSize:   13.5,
							FontWeight: 600,
							Color:      p.Warning.Fg,
							FlexShrink: 1,
						},
					},
				},
			},
			core.Text{
				TestID:  "agent-perm-body",
				Content: bodyText,
				Style: core.Style{
					FontSize:   12.5,
					LineHeight: 19,
					Color:      p.TextSecondary,
				},
			},
			core.View{
				Style: core.Style{Gap: 8},
				Children: []core.Node{
					permButton(
						allowOnceRef, "agent-perm-allow-once", ed.T("permissions.allowOnce"), allowOnceHint,
						p.Accent.Fg, core.Transparent, p.Accent.OnFill, ui.Alpha(p.Accent.OnFill, 0.14), p.Accent.OnFill, 600,
						func() { ed.AllowPermissionOnce() },
					),
					permButton(
						allowAlwaysRef, "agent-perm-allow-always", alwaysLabel, allowAlwaysHint,
						p.Raised, p.Warning.Edge, p.TextPrimary, p.LineRaised, p.TextSecondary, 500,
						func() { ed.AllowPermissionAlways() },
					),
					permButton(
						denyRef, "agent-perm-deny", ed.T("permissions.deny"), denyHint,
						p.Warning.Fill, p.Warning.Edge, p.TextSecondary, p.Raised, p.TextSecondary, 400,
						func() { ed.DenyPermission() },
					),
				},
			},
			core.View{
				TestID: "agent-perm-rule-info",
				Style: core.Style{
					Padding:      core.Symmetric(8, 12),
					CornerRadius: 8,
					Background:   p.Inset,
					Gap:          4,
				},
				Children: ruleBoxKids,
			},
		},
	}
}

func agentTurnReviewCardView(p SpecPalette, ed *Editor, rev AgentPendingReview, osKind core.OS) core.Node {
	headerText := fmt.Sprintf("%d steps pending review", rev.StepsDone)
	if rev.StepsDone == 1 {
		headerText = "1 step pending review"
	}
	if rev.ChangesCount > 0 {
		headerText = fmt.Sprintf("%d changes · %d steps", rev.ChangesCount, rev.StepsDone)
	}

	headerKids := []core.Node{
		core.Text{
			Content: headerText,
			Style: core.Style{
				FlexGrow:   1,
				FontSize:   12.5,
				FontWeight: 600,
				Color:      p.TextPrimary,
			},
		},
	}
	for _, badge := range rev.NotChecked {
		headerKids = append(headerKids, core.View{
			TestID: "agent-review-unchecked-badge",
			Style: core.Style{
				Height:         core.Pt(22),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				Background:     p.Warning.Fill,
				BorderWidth:    1,
				BorderColor:    p.Warning.Edge,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: badge,
					Style: core.Style{
						FontSize:   11,
						FontWeight: 500,
						Color:      p.Warning.Fg,
					},
				},
			},
		})
	}

	kids := []core.Node{
		core.View{
			Style: core.Style{
				Direction:   core.Row,
				Wrap:        core.Wrap,
				AlignItems:  core.AlignCenter,
				Gap:         8,
				Padding:     core.Symmetric(10, 12),
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: headerKids,
		},
	}
	if rev.Summary != "" {
		kids = append(kids, core.View{
			Style: core.Style{Padding: core.Symmetric(8, 12)},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-review-summary",
					Content: rev.Summary,
					Style: core.Style{
						FontSize:   12,
						LineHeight: 18,
						Color:      p.TextSecondary,
					},
				},
			},
		})
	}

	switch {
	case rev.Kept:
		kids = append(kids, core.View{
			Style: core.Style{Padding: core.Symmetric(8, 12)},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("Kept · %s undoes", FormatShortcut("mod+z", osKind)),
					Style:   core.Style{FontSize: 12, Color: p.TextFaint},
				},
			},
		})
	case rev.Reverted:
		kids = append(kids, core.View{
			Style: core.Style{Padding: core.Symmetric(8, 12)},
			Children: []core.Node{
				core.Text{
					Content: "Reverted · scene restored",
					Style:   core.Style{FontSize: 12, Color: p.TextFaint},
				},
			},
		})
	default:
		kids = append(kids, core.View{
			Style: core.Style{
				Direction: core.Row,
				Gap:       8,
				Padding:   core.All(12),
			},
			Children: []core.Node{
				core.View{
					TestID:    "agent-review-keep",
					Role:      core.RoleButton,
					Label:     ed.T("agent.keep"),
					Focusable: func() bool { return true },
					Style: core.Style{
						FlexGrow:       1,
						Height:         core.Pt(30),
						CornerRadius:   8,
						Background:     p.Accent.Fg,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.KeepPendingAgentTurnReview()
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("agent.keep"),
							Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.Accent.OnFill},
						},
					},
				},
				core.View{
					TestID:    "agent-review-revert",
					Role:      core.RoleButton,
					Label:     ed.T("agent.revert"),
					Focusable: func() bool { return true },
					Style: core.Style{
						Height:         core.Pt(30),
						Padding:        core.Symmetric(0, 12),
						CornerRadius:   8,
						Background:     p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.RevertPendingAgentTurnReview()
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("agent.revert"),
							Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
						},
					},
				},
			},
		})
	}

	return core.View{
		TestID: "agent-review-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Overflow:     core.Hidden,
		},
		Children: kids,
	}
}

func agentWorkingFooterView(cx *core.Scope, ed *Editor, p SpecPalette, addDraft *core.Signal[string]) core.Node {
	osKind := cx.Env().OS()
	var addInputFocused bool
	cx.Cleanup(func() {
		if addInputFocused {
			addInputFocused = false
			ed.noteTextInputFocus(false)
		}
	})
	submitAdd := func(val string) {
		txt := strings.TrimSpace(val)
		if txt == "" {
			return
		}
		if err := ed.AddToAgentRequest(txt); err != nil {
			ed.report(err)
			return
		}
		addDraft.Set("")
	}
	return core.View{
		TestID: "agent-composer",
		Style: core.Style{
			Padding:     core.All(12),
			Gap:         8,
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
			Background:  p.Panel,
			FlexShrink:  0,
		},
		Children: []core.Node{
			SpecFocusWithinCard{
				TestID: "agent-add-card",
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Padding:    core.Edges{Top: 6, Right: 6, Bottom: 6, Left: 12},
					Gap:        8,
				},
				Children: []core.Node{
					core.Input{
						TestID:      "agent-add-input",
						Label:       ed.T("agent.addPlaceholder"),
						Placeholder: ed.T("agent.addPlaceholder"),
						Value:       addDraft.Get,
						OnChange:    addDraft.Set,
						OnSubmit:    submitAdd,
						OnFocus: func() {
							if !addInputFocused {
								addInputFocused = true
								ed.noteTextInputFocus(true)
							}
						},
						OnBlur: func() {
							if addInputFocused {
								addInputFocused = false
								ed.noteTextInputFocus(false)
							}
						},
						Style: core.Style{
							FlexGrow:         1,
							FlexBasis:        core.Pt(0),
							Height:           core.Pt(28),
							FontSize:         12.5,
							Color:            p.TextPrimary,
							PlaceholderColor: p.TextFaint,
						},
					},
					core.View{
						TestID:    "agent-composer-stop",
						Role:      core.RoleButton,
						Label:     ed.T("agent.stop"),
						Focusable: func() bool { return true },
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							Gap:            6,
							CornerRadius:   8,
							Background:     p.LineRaised,
							FlexShrink:     0,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.StopAgent()
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
								Content: ed.T("agent.stop"),
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextPrimary,
								},
							},
						},
					},
				},
			},
			core.Text{
				TestID:  "agent-footer-hint",
				Content: fmt.Sprintf("%s · %s", FormatShortcut("mod+.", osKind), ed.T("agent.footerHint")),
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextFaint,
				},
			},
		},
	}
}

// agentStatusBarActivityView shows what the agent is currently doing in the bottom status bar
// while reminding the person that manual edits stay available.
func agentStatusBarActivityView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type activityKey struct {
		text    string
		waiting bool
		dark    bool
	}
	actMemo := core.NewMemo(cx, func() activityKey {
		_ = rev.Get()
		return activityKey{
			text:    ed.AgentStatusBarText(),
			waiting: ed.AgentStatus() == AgentStatusWaitingForYou,
			dark:    ed.SpecPalette(cx).Dark,
		}
	})
	return core.Show{
		When: func() bool {
			return actMemo.Get().text != ""
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := actMemo.Get()
				p := SpecPaletteFor(k.dark)
				dotColor := p.Agent.Fg
				if k.waiting {
					dotColor = p.Warning.Fg
				}
				return core.View{
					TestID:     "status-agent-activity",
					Role:       core.RoleStatus,
					LiveRegion: core.LiveRegionPolite,
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        6,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pt(6),
								Height:       core.Pt(6),
								CornerRadius: 3,
								Background:   dotColor,
								FlexShrink:   0,
							},
						},
						core.Text{
							TestID:  "status-agent-activity-text",
							Content: k.text,
							Style: core.Style{
								FontSize: 11,
								Color:    p.TextMuted,
							},
						},
					},
				}
			})
		},
	}
}
