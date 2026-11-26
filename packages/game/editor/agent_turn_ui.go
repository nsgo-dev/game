package editor

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// RightPanelView delegates to RightRailView for callers referencing the right-hand Agent / Inspector panel.
func RightPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return RightRailView(cx, ed, rev)
}

// agentTurnSection renders the active, pending, stopped, kept, or reverted multi-file agent turn
// (prompt bubble, progress steps + Stop control, completion message, summary card, and post-decision note)
// matching docs/design/editor/Main.dc.html using exclusively SpecPalette tokens.
func agentTurnSection(cx *core.Scope, ed *Editor, p SpecPalette, snap AgentTurnSnapshot) core.Node {
	var thread []core.Node

	// User request bubble (right-aligned, matching Main.dc.html).
	if snap.Prompt != "" {
		thread = append(thread, core.View{
			Style: core.Style{
				Direction:      core.Row,
				JustifyContent: core.JustifyEnd,
			},
			Children: []core.Node{
				core.View{
					TestID: "agent-turn-prompt",
					Style: core.Style{
						MaxWidth:     core.Pt(300),
						Padding:      core.Symmetric(10, 12),
						CornerRadius: 12,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.Text{
							Content: snap.Prompt,
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextPrimary,
							},
						},
					},
				},
			},
		})
	}

	// Progress steps list.
	if len(snap.Steps) > 0 || snap.Status == AgentTurnWorking {
		var stepNodes []core.Node
		for i, st := range snap.Steps {
			stepNodes = append(stepNodes, core.View{
				TestID: fmt.Sprintf("agent-turn-step-%d", i),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(14),
							Height:         core.Pt(14),
							CornerRadius:   7,
							Background:     p.Added.Fill,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Canvas{
								Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
								Draw: func(d *core.Drawing, _ core.Size) {
									d.SetStrokeColor(p.Added.Fg).SetLineWidth(1.8).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
									d.BeginPath().MoveTo(1.5, 4.2).LineTo(3.2, 6.2).LineTo(6.8, 2.0).Stroke()
								},
							},
						},
					},
					core.Text{
						Content: st.Label,
						Style: core.Style{
							FontSize:   12,
							Color:      p.TextFaint,
							FlexShrink: 1,
						},
					},
				},
			})
		}
		if snap.Status == AgentTurnWorking {
			stepNodes = append(stepNodes, core.View{
				TestID: "agent-turn-working-row",
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            8,
					Padding:        core.Symmetric(6, 10),
					CornerRadius:   8,
					Background:     p.Agent.Fill,
					BorderWidth:    1,
					BorderColor:    p.Agent.Edge,
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
								Style: core.Style{
									Width:        core.Pt(8),
									Height:       core.Pt(8),
									CornerRadius: 4,
									Background:   p.Agent.Fg,
								},
							},
							core.Text{
								Content: "Agent working…",
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.Agent.Fg,
								},
							},
						},
					},
					core.View{
						TestID:    "agent-turn-stop",
						Role:      core.RoleButton,
						Label:     "Stop agent",
						Focusable: func() bool { return true },
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							Gap:            6,
							Height:         core.Pt(28),
							Padding:        core.Symmetric(0, 10),
							CornerRadius:   6,
							Background:     p.Raised,
							BorderWidth:    1,
							BorderColor:    p.LineRaised,
							FocusVisible:   p.FocusRing(2),
							JustifyContent: core.JustifyCenter,
						},
						OnPress: func(core.PressEvent) {
							ed.report(ed.StopAgentTurn())
						},
						Children: []core.Node{
							core.Text{
								Content: "Stop",
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextPrimary,
								},
							},
							core.Text{
								Content: FormatShortcut("mod+.", cx.Env().OS()),
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   11,
									Color:      p.TextFaint,
								},
							},
						},
					},
				},
			})
		}
		thread = append(thread, core.View{
			Style:    core.Style{Gap: 8},
			Children: stepNodes,
		})
	}

	// Completion message.
	if snap.Summary != "" {
		thread = append(thread, core.Text{
			TestID:  "agent-turn-summary-text",
			Content: snap.Summary,
			Style: core.Style{
				FontSize: 12.5,
				Color:    p.TextSecondary,
			},
		})
	}

	// Small Summary Card when Pending or Stopped (matching Main.dc.html).
	if snap.IsPending() {
		thread = append(thread, agentTurnSummaryCard(cx, ed, p, snap))
	}

	// Kept / Reverted post-decision states (matching Main.dc.html lines 175-180).
	if snap.Status == AgentTurnKept {
		undoKey := FormatShortcut("mod+z", cx.Env().OS())
		thread = append(thread, core.Text{
			TestID:  "agent-turn-kept",
			Content: fmt.Sprintf("Kept · one undo step (%s)", undoKey),
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}
	if snap.Status == AgentTurnReverted {
		thread = append(thread, core.Text{
			TestID:  "agent-turn-reverted",
			Content: "Reverted · the scene is as it was before this request",
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}

	return core.View{
		TestID:   "agent-turn-section",
		Style:    core.Style{Gap: 12},
		Children: thread,
	}
}

func agentTurnSummaryCard(cx *core.Scope, ed *Editor, p SpecPalette, snap AgentTurnSnapshot) core.Node {
	fileCount := len(snap.Files)
	headerTitle := fmt.Sprintf("%d %s · %d %s",
		snap.TotalChanges, turnPlural(snap.TotalChanges, "change", "changes"),
		fileCount, turnPlural(fileCount, "file", "files"))
	if snap.RevertedCount > 0 {
		headerTitle = fmt.Sprintf("%d of %d %s · %d %s",
			snap.ActiveChanges, snap.TotalChanges, turnPlural(snap.TotalChanges, "change", "changes"),
			fileCount, turnPlural(fileCount, "file", "files"))
	}

	removedColor := p.TextFaint
	if snap.RemovedLines > 0 {
		removedColor = p.Removed.Fg
	}

	headerRight := []core.Node{
		core.Text{
			TestID:  "agent-turn-lines-added",
			Content: fmt.Sprintf("+%d", snap.AddedLines),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11,
				FontWeight: 500,
				Color:      p.Added.Fg,
			},
		},
		core.Text{
			TestID:  "agent-turn-lines-removed",
			Content: fmt.Sprintf("−%d", snap.RemovedLines),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11,
				FontWeight: 500,
				Color:      removedColor,
			},
		},
	}

	headerLeft := []core.Node{
		core.Text{
			TestID:  "agent-turn-card-header",
			Content: headerTitle,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: 600,
				Color:      p.TextPrimary,
			},
		},
	}
	if snap.Status == AgentTurnStopped {
		headerLeft = append(headerLeft, core.View{
			TestID: "agent-turn-stopped-badge",
			Style: core.Style{
				Height:         core.Pt(20),
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   6,
				Background:     p.Warning.Fill,
				BorderWidth:    1,
				BorderColor:    p.Warning.Edge,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: "Stopped",
					Style: core.Style{
						FontSize:   11,
						FontWeight: 600,
						Color:      p.Warning.Fg,
					},
				},
			},
		})
	}

	// Build change list rows (with per-file header & toggle when multiple files or review expanded).
	var changeRows []core.Node
	multiFile := len(snap.Files) > 1
	showFileHeaders := multiFile || snap.ReviewOpen

	if showFileHeaders {
		changeIdx := 0
		for fi, fs := range snap.Files {
			filePath := fs.File
			allReverted := fs.ActiveCount == 0
			fileActionLabel := "Revert file"
			if allReverted {
				fileActionLabel = "Restore file"
			}
			changeRows = append(changeRows, core.View{
				TestID: fmt.Sprintf("agent-turn-file-%d", fi),
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            8,
					Padding:        core.Edges{Top: 4, Bottom: 2},
				},
				Children: []core.Node{
					core.Text{
						Content:  filePath,
						MaxLines: 1,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11,
							FontWeight: 500,
							Color:      p.TextMuted,
							FlexShrink: 1,
						},
					},
					core.View{
						TestID:    fmt.Sprintf("agent-turn-file-toggle-%d", fi),
						Role:      core.RoleButton,
						Label:     fileActionLabel + " " + filePath,
						Focusable: func() bool { return true },
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Background:     p.Inset,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if allReverted {
								ed.report(ed.RestoreAgentTurnFile(filePath))
							} else {
								ed.report(ed.RevertAgentTurnFile(filePath))
							}
						},
						Children: []core.Node{
							core.Text{
								Content: fileActionLabel,
								Style: core.Style{
									FontSize: 11,
									Color:    p.TextMuted,
								},
							},
						},
					},
				},
			})
			for _, ch := range snap.Changes {
				if ch.File != fs.File {
					continue
				}
				changeRows = append(changeRows, agentTurnChangeRow(ed, p, changeIdx, ch))
				changeIdx++
			}
		}
	} else {
		// Group cleanly by kind order: added, changed, removed.
		kindOrder := []AgentChangeKind{AgentChangeAdded, AgentChangeChanged, AgentChangeRemoved}
		idx := 0
		for _, k := range kindOrder {
			for _, ch := range snap.Changes {
				if ch.Kind != k {
					continue
				}
				changeRows = append(changeRows, agentTurnChangeRow(ed, p, idx, ch))
				idx++
			}
		}
	}

	if len(changeRows) == 0 {
		changeRows = append(changeRows, core.Text{
			Content: "No file changes in this turn.",
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextFaint,
			},
		})
	}

	keepLabel := "Keep"
	if snap.RevertedCount > 0 && snap.TotalChanges > 0 {
		keepLabel = fmt.Sprintf("Keep %d of %d", snap.ActiveChanges, snap.TotalChanges)
	}
	reviewLabel := "Review"
	if snap.ReviewOpen {
		reviewLabel = "Compact"
	}

	return core.View{
		TestID: "agent-turn-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			// Card Header
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Height:         core.Pt(38),
					Padding:        core.Symmetric(0, 12),
					Borders:        core.Edges{Bottom: 1},
					BorderColor:    p.Line,
					Gap:            8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: headerLeft,
					},
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        6,
						},
						Children: headerRight,
					},
				},
			},
			// Changes list
			core.View{
				Style: core.Style{
					Padding: core.Symmetric(8, 12),
					Gap:     6,
				},
				Children: changeRows,
			},
			// Actions footer (Keep / Review / Revert)
			core.View{
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(10, 12),
					Borders:     core.Edges{Top: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{
					core.View{
						TestID:    "agent-turn-keep",
						Role:      core.RoleButton,
						Label:     keepLabel,
						Focusable: func() bool { return true },
						Style: core.Style{
							FlexGrow:       1,
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Background:     p.Accent.Fg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.report(ed.KeepAgentTurn())
						},
						Children: []core.Node{
							core.Text{
								Content: keepLabel,
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 600,
									Color:      p.Accent.OnFill,
								},
							},
						},
					},
					core.View{
						TestID:    "agent-turn-review",
						Role:      core.RoleButton,
						Label:     reviewLabel,
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
							ed.SetAgentTurnReviewOpen(!ed.AgentTurnReviewOpen())
						},
						Children: []core.Node{
							core.Text{
								Content: reviewLabel,
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      p.TextSecondary,
								},
							},
						},
					},
					core.View{
						TestID:    "agent-turn-revert",
						Role:      core.RoleButton,
						Label:     "Revert",
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
							ed.report(ed.RevertAgentTurn())
						},
						Children: []core.Node{
							core.Text{
								Content: "Revert",
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      p.TextSecondary,
								},
							},
						},
					},
				},
			},
		},
	}
}

func agentTurnChangeRow(ed *Editor, p SpecPalette, idx int, ch AgentTurnChange) core.Node {
	sign := "+"
	signColor := p.Added.Fg
	switch ch.Kind {
	case AgentChangeChanged:
		sign = "~"
		signColor = p.Changed.Fg
	case AgentChangeRemoved:
		sign = "−"
		signColor = p.Removed.Fg
	}

	titleColor := p.TextMuted
	var deco core.TextDecoration
	if ch.Reverted {
		titleColor = p.TextFaint
		deco = core.TextDecorationLineThrough
	}
	label := ch.Title
	if ch.Detail != "" {
		label = ch.Title + " · " + ch.Detail
	}

	toggleText := "Revert"
	if ch.Reverted {
		toggleText = "Restore"
	}
	changeID := ch.ID

	mainRow := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
			MinHeight:  core.Pt(24),
		},
		Children: []core.Node{
			core.Text{
				Content: sign,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   12,
					FontWeight: 600,
					Color:      signColor,
					Width:      core.Pt(10),
					FlexShrink: 0,
				},
			},
			core.Text{
				Content:  label,
				MaxLines: 1,
				Style: core.Style{
					FontSize:       12,
					Color:          titleColor,
					TextDecoration: deco,
					FlexGrow:       1,
					FlexShrink:     1,
					MinWidth:       core.Pt(0),
				},
			},
			core.View{
				TestID:    fmt.Sprintf("agent-turn-toggle-%d", idx),
				Role:      core.RoleButton,
				Label:     toggleText + " " + ch.Title,
				Focusable: func() bool { return true },
				Style: core.Style{
					Height:         core.Pt(24),
					Padding:        core.Symmetric(0, 8),
					CornerRadius:   6,
					Background:     p.Inset,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.ToggleAgentTurnChange(changeID))
				},
				Children: []core.Node{
					core.Text{
						Content: toggleText,
						Style: core.Style{
							FontSize: 11,
							Color:    p.TextMuted,
						},
					},
				},
			},
		},
	}

	if ch.Reason == "" {
		return core.View{
			TestID:   fmt.Sprintf("agent-turn-change-%d", idx),
			Children: []core.Node{mainRow},
		}
	}

	return core.View{
		TestID: fmt.Sprintf("agent-turn-change-%d", idx),
		Style:  core.Style{Gap: 2},
		Children: []core.Node{
			mainRow,
			core.Text{
				Content: "Why: " + ch.Reason,
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextFaint,
					Padding:  core.Edges{Left: 18},
				},
			},
		},
	}
}

// pendingTurnDialog asks the person to Keep, Revert, or Cancel when closing the window, opening
// another scene, or opening another project while an agent turn is still pending.
func pendingTurnDialog(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	onClose := bindDialogFocusRestore(cx, ed, func() bool {
		_ = rev.Get()
		return ed.PendingTurnPromptOpen()
	})
	return ui.AlertDialog{
		TestID: "pending-turn-dialog",
		Open: func() bool {
			_ = rev.Get()
			return ed.PendingTurnPromptOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				ed.CancelPendingTurnPrompt()
			}
		},
		OnClose: onClose,
		Title:   "Pending agent turn",
		DescriptionFunc: func() string {
			_ = rev.Get()
			snap, _ := ed.CurrentAgentTurn()
			summary := ed.PendingTurnPromptSummary()
			if summary != "" {
				return fmt.Sprintf("You have a pending agent turn (%d %s). Keep or revert it before proceeding (%s)?",
					snap.ActiveChanges, turnPlural(snap.ActiveChanges, "change", "changes"), summary)
			}
			return fmt.Sprintf("You have a pending agent turn (%d %s). Keep or revert it before proceeding?",
				snap.ActiveChanges, turnPlural(snap.ActiveChanges, "change", "changes"))
		},
		Actions: []ui.AlertAction{
			{
				TestID:  "pending-turn-cancel",
				Text:    "Cancel",
				Variant: ui.ButtonSecondary,
				Role:    core.AlertCancel,
				OnPress: func() {
					ed.CancelPendingTurnPrompt()
				},
			},
			{
				TestID:  "pending-turn-revert",
				Text:    "Revert",
				Variant: ui.ButtonSecondary,
				Role:    core.AlertDestructive,
				OnPress: func() {
					ed.report(ed.ConfirmRevertPendingTurnPrompt())
				},
			},
			{
				TestID:  "pending-turn-keep",
				Text:    "Keep",
				Variant: ui.ButtonPrimary,
				Default: true,
				OnPress: func() {
					ed.report(ed.ConfirmKeepPendingTurnPrompt())
				},
			},
		},
	}
}
