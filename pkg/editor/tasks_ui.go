package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// tasksStatusIndicatorView renders the Tasks status bar button matching design/editor/Tasks.dc.html:
// idle ("No tasks"), single running ("1 running · 0:38"), or mixed counts most urgent first
// (Failed, Needs you, Running, Ready to review, Queued) with zero counts omitted.
// The outer button stays stably mounted across task updates so keyboard focus is never dropped.
func tasksStatusIndicatorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	statusKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		parts := ed.TaskStatusParts()
		var b strings.Builder
		if ed.SpecPalette(cx).Dark {
			b.WriteString("D:")
		} else {
			b.WriteString("L:")
		}
		for _, part := range parts {
			b.WriteString(string(part.State))
			b.WriteByte('=')
			b.WriteString(part.Label)
			b.WriteByte('|')
		}
		return b.String()
	})

	partsNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = statusKey.Get()
		p := ed.SpecPalette(cx)
		parts := ed.TaskStatusParts()
		kids := make([]core.Node, 0, len(parts))
		for i, part := range parts {
			dotColor := p.TextFaint
			textColor := p.TextFaint
			switch part.State {
			case TaskFailed:
				dotColor = p.Removed.Fg
				textColor = p.Removed.Fg
			case TaskNeedsYou:
				dotColor = p.Warning.Fg
				textColor = p.Warning.Fg
			case TaskRunning:
				dotColor = p.Agent.Fg
				textColor = p.TextSecondary
			case TaskReadyToReview:
				dotColor = p.Accent.Fg
				textColor = p.TextSecondary
			case TaskQueued:
				dotColor = p.TextFaint
				textColor = p.TextMuted
			}
			kids = append(kids, core.View{
				TestID: fmt.Sprintf("status-tasks-part-%d", i),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        4,
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
						Content: part.Label,
						Style: core.Style{
							FontSize:   11.5,
							FontWeight: 500,
							Color:      textColor,
						},
					},
				},
			})
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        10,
			},
			Children: kids,
		}
	})

	return core.View{
		TestID: "status-tasks",
		Role:   core.RoleButton,
		LabelFunc: func() string {
			_ = rev.Get()
			parts := ed.TaskStatusParts()
			ariaParts := make([]string, 0, len(parts))
			for _, part := range parts {
				ariaParts = append(ariaParts, part.Label)
			}
			return strings.Join(ariaParts, ", ") + ". Show tasks"
		},
		Focusable: func() bool { return true },
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(24),
			Padding:      core.Symmetric(0, 8),
			CornerRadius: 6,
			BorderWidth:  1,
			FlexShrink:   0,
		},
		StyleFunc: func() core.Style {
			_ = rev.Get()
			p := ed.SpecPalette(cx)
			border := p.LineRaised
			if ed.TasksPanelOpen() {
				border = p.Accent.Edge
			}
			return core.Style{
				Background:   p.Raised,
				BorderColor:  border,
				Cursor:       core.CursorPointer,
				Hover:        &core.Style{Background: p.LineRaised},
				FocusVisible: p.FocusRing(2),
			}
		},
		OnPress: func(core.PressEvent) {
			ed.ToggleTasksPanel()
		},
		Children: []core.Node{partsNode},
	}
}

// TasksPanelView renders the Agent right panel with its collapsible Tasks list (grouped under
// Needs you, Running, Ready to review, Queued, and Failed), bulk Dismiss for Ready to review,
// focused task detail with live steps & review actions, and Queue / Run in parallel composer,
// matching design/editor/Tasks.dc.html using exclusively SpecPalette tokens.
// The panel container, ScrollViews, and composer input remain stably mounted across task updates
// so background progress never steals typing focus or resets scroll position.
func TasksPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	panelRef := new(core.Ref)
	ed.SetAgentPanelRef(panelRef)

	themeKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.SpecPalette(cx).Dark {
			return "D|" + ed.ResolvedLanguage()
		}
		return "L|" + ed.ResolvedLanguage()
	})

	expandedMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.TasksListExpanded()
	})

	summaryKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		s := ed.TaskSummary()
		return fmt.Sprintf("%v|%d|%d|%d|%d", ed.TasksListExpanded(), s.Running, s.NeedsYou, s.ReadyToReview, s.Failed)
	})

	listKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		fmt.Fprintf(&b, "focus=%s|sel=%d", ed.FocusedTaskID(), len(ed.SelectedDismissTaskIDs()))
		for _, t := range ed.Tasks() {
			fmt.Fprintf(&b, "|%s:%s:%d:%v:%v:%s:%s:%d:%s:%d",
				t.ID, t.State, t.Number, t.Dismissed, ed.TaskDismissSelected(t.ID),
				t.BlockedOn, t.StepSummary, t.ElapsedSeconds, t.TimeLabel, t.Review.EffectiveChangeCount())
		}
		return b.String()
	})

	detailKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		focused, ok := ed.FocusedTask()
		if !ok {
			return "none"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s|%s|%d|%s|%s|%s|%s|%d",
			focused.ID, focused.State, focused.Number, focused.Title, focused.BlockedOn,
			focused.PermissionDetail, focused.Error, len(focused.Steps))
		for _, st := range focused.Steps {
			fmt.Fprintf(&b, "|%s:%v:%v:%v", st.Summary, st.Done, st.Active, st.Blocked)
		}
		return b.String()
	})

	draftSig := core.NewSignal(cx, ed.TaskComposerDraft())
	cx.Effect(func() {
		_ = rev.Get()
		d := ed.TaskComposerDraft()
		if draftSig.GetUntracked() != d {
			draftSig.Set(d)
		}
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = themeKey.Get()
		p := ed.SpecPalette(cx)

		header := core.View{
			TestID: "tasks-panel-header",
			Style: core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Height:      core.Pt(44),
				FlexShrink:  0,
				Padding:     core.Edges{Left: 16, Right: 8},
				Gap:         8,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("tasks.panelTitle"),
					Style: core.Style{
						FontSize:   13,
						FontWeight: 600,
						Color:      p.TextPrimary,
						FlexGrow:   1,
					},
				},
				AgentStatusChipView(cx, ed, rev),
				ui.Button{
					TestID:   "tasks-panel-close",
					Icon:     lucide.X,
					Label:    ed.T("tasks.close"),
					Variant:  ui.ButtonGhost,
					Size:     ui.SizeSmall,
					Side:     28,
					IconSize: 14,
					Tint:     func() core.Color { return p.TextFaint },
					Style: core.Style{
						CornerRadius: 6,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func() {
						ed.SetTasksPanelOpen(false)
					},
				},
			},
		}

		// Summary counts inside the collapsible "Tasks" header; only the inner chips update.
		listToggleContent := core.Dynamic(func(cx *core.Scope) core.Node {
			_ = summaryKey.Get()
			summary := ed.TaskSummary()
			summaryBadges := []core.Node{
				core.Text{
					Content: ed.T("tasks.sectionTitle"),
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
			}
			if summary.Running > 0 {
				summaryBadges = append(summaryBadges, taskCountChip(p.Agent.Fg, p.TextMuted, fmt.Sprintf("%d running", summary.Running)))
			}
			if summary.NeedsYou > 0 {
				summaryBadges = append(summaryBadges, taskCountChip(p.Warning.Fg, p.Warning.Fg, fmt.Sprintf("%d needs you", summary.NeedsYou)))
			}
			if summary.ReadyToReview > 0 {
				summaryBadges = append(summaryBadges, taskCountChip(p.Accent.Fg, p.TextMuted, fmt.Sprintf("%d to review", summary.ReadyToReview)))
			}
			if summary.Failed > 0 {
				summaryBadges = append(summaryBadges, taskCountChip(p.Removed.Fg, p.Removed.Fg, fmt.Sprintf("%d failed", summary.Failed)))
			}
			summaryBadges = append(summaryBadges, core.View{Style: core.Style{FlexGrow: 1}})
			chevronIcon := lucide.ChevronDown
			if ed.TasksListExpanded() {
				chevronIcon = lucide.ChevronUp
			}
			summaryBadges = append(summaryBadges, core.Icon{
				SVG:   chevronIcon(),
				Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: p.TextFaint},
			})
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					FlexGrow:   1,
					Gap:        10,
				},
				Children: summaryBadges,
			}
		})

		listToggle := core.View{
			TestID:    "tasks-list-toggle",
			Role:      core.RoleButton,
			Label:     ed.T("tasks.sectionTitle"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(40),
				Padding:      core.Symmetric(0, 16),
				FocusVisible: p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleTasksListExpanded()
			},
			Children: []core.Node{listToggleContent},
		}

		// Build the five grouped task sections inside a stably-mounted ScrollView.
		listRows := core.Dynamic(func(cx *core.Scope) core.Node {
			_ = listKey.Get()
			selectedDismiss := ed.SelectedDismissTaskIDs()
			focused, hasFocused := ed.FocusedTask()
			type groupDef struct {
				state TaskState
				head  string
			}
			groups := []groupDef{
				{TaskNeedsYou, ed.T("tasks.group.needsYou")},
				{TaskRunning, ed.T("tasks.group.running")},
				{TaskReadyToReview, ed.T("tasks.group.readyToReview")},
				{TaskQueued, ed.T("tasks.group.queued")},
				{TaskFailed, ed.T("tasks.group.failed")},
			}

			var groupNodes []core.Node
			for _, g := range groups {
				items := ed.TasksInState(g.state)
				if len(items) == 0 {
					continue
				}
				headChildren := []core.Node{
					core.Text{
						Content: fmt.Sprintf("%s · %d", g.head, len(items)),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
							FlexGrow:      1,
						},
					},
				}
				if g.state == TaskReadyToReview {
					selN := len(selectedDismiss)
					dismissBg := core.Transparent
					dismissColor := p.TextFaint
					if selN > 0 {
						dismissBg = p.LineRaised
						dismissColor = p.TextPrimary
					}
					headChildren = append(headChildren, core.View{
						TestID:    "tasks-dismiss-selected",
						Role:      core.RoleButton,
						Label:     fmt.Sprintf("Dismiss %d", selN),
						Disabled:  func() bool { return selN == 0 },
						Focusable: func() bool { return selN > 0 },
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Background:     dismissBg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.DismissSelectedTasks()
						},
						Children: []core.Node{
							core.Text{
								Content: fmt.Sprintf("%s %d", ed.T("tasks.action.dismiss"), selN),
								Style: core.Style{
									FontSize:   11,
									FontWeight: 500,
									Color:      dismissColor,
								},
							},
						},
					})
				}
				groupNodes = append(groupNodes, core.View{
					TestID: "tasks-group-" + string(g.state),
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Height:     core.Pt(24),
						Padding:    core.Symmetric(0, 8),
						Gap:        8,
					},
					Children: headChildren,
				})
				for _, item := range items {
					groupNodes = append(groupNodes, taskRowView(ed, p, item, hasFocused && focused.ID == item.ID))
				}
			}

			if len(groupNodes) == 0 {
				groupNodes = append(groupNodes, core.View{
					TestID: "tasks-empty-state",
					Style: core.Style{
						Padding: core.Symmetric(12, 16),
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("tasks.empty"),
							Style: core.Style{
								FontSize:   12,
								LineHeight: 17,
								Color:      p.TextFaint,
							},
						},
					},
				})
			}

			return core.View{
				TestID: "tasks-list-rows",
				Style: core.Style{
					Padding: core.Edges{Left: 8, Right: 8, Bottom: 8},
					Gap:     2,
				},
				Children: groupNodes,
			}
		})

		listSection := core.View{
			TestID: "tasks-list-section",
			Style: core.Style{
				FlexShrink:  0,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				listToggle,
				core.Show{
					When: expandedMemo.Get,
					Then: func(cx *core.Scope) core.Node {
						return ui.ScrollView{
							Style: core.Style{
								MaxHeight: core.Pt(468),
							},
							Children: []core.Node{listRows},
						}
					},
				},
			},
		}

		// Focused task detail area inside a stably-mounted ScrollView.
		detailSection := ui.ScrollView{
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				MinHeight: core.Pt(0),
			},
			Children: []core.Node{
				core.Dynamic(func(cx *core.Scope) core.Node {
					_ = detailKey.Get()
					focused, hasFocused := ed.FocusedTask()
					return taskDetailView(ed, p, focused, hasFocused)
				}),
			},
		}

		// Stably-mounted composer section at the bottom matching Tasks.dc.html.
		// Never remounted on task progress so typing focus is preserved.
		segBtn := func(testID, label string, target TaskStartMode) core.Node {
			return core.View{
				TestID:    testID,
				Role:      core.RoleRadio,
				Label:     label,
				Focusable: func() bool { return true },
				Style: core.Style{
					Height:         core.Pt(26),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					if ed.TaskComposerMode() == target {
						return core.Style{Background: p.LineRaised}
					}
					return core.Style{Background: core.Transparent}
				},
				OnPress: func(core.PressEvent) {
					ed.SetTaskComposerMode(target)
				},
				Children: []core.Node{
					core.Text{
						Content: label,
						Style: core.Style{
							FontSize: 11.5,
						},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							if ed.TaskComposerMode() == target {
								return core.Style{FontWeight: 500, Color: p.TextPrimary}
							}
							return core.Style{FontWeight: 400, Color: p.TextMuted}
						},
					},
				},
			}
		}

		placeholder := ed.T("tasks.composerPlaceholder")
		composer := core.View{
			TestID: "tasks-composer",
			Style: core.Style{
				FlexShrink:  0,
				Padding:     core.All(12),
				Gap:         8,
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						CornerRadius: 12,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Padding:      core.Edges{Top: 8, Right: 8, Bottom: 8, Left: 12},
						Gap:          8,
					},
					Children: []core.Node{
						ui.Input{
							TestID:      "tasks-composer-input",
							Label:       ed.T("tasks.newRequest"),
							Placeholder: placeholder,
							Value:       draftSig.Get,
							OnChange: func(s string) {
								draftSig.Set(s)
								ed.SetTaskComposerDraft(s)
							},
							OnSubmit: func(s string) {
								ed.SetTaskComposerDraft(s)
								if _, err := ed.SubmitComposerTask(); err != nil {
									ed.report(err)
								}
							},
							OnFocus: func() {
								ed.noteTextInputFocus(true)
							},
							OnBlur: func() {
								ed.noteTextInputFocus(false)
							},
							Style: core.Style{
								Height:           core.Pt(28),
								Background:       core.Transparent,
								BorderWidth:      0,
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
								core.View{
									TestID: "tasks-mode-group",
									Role:   core.RoleRadioGroup,
									Label:  ed.T("tasks.whenToStart"),
									Style: core.Style{
										Direction:    core.Row,
										Gap:          2,
										Padding:      core.All(2),
										CornerRadius: 8,
										Background:   p.Panel,
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
									},
									Children: []core.Node{
										segBtn("tasks-mode-queue", ed.T("tasks.mode.queue"), TaskStartQueue),
										segBtn("tasks-mode-parallel", ed.T("tasks.mode.parallel"), TaskStartParallel),
									},
								},
								core.View{Style: core.Style{FlexGrow: 1}},
								core.View{
									TestID:    "tasks-composer-send",
									Role:      core.RoleButton,
									Label:     ed.T("tasks.send"),
									Focusable: func() bool { return true },
									Style: core.Style{
										Width:          core.Pt(30),
										Height:         core.Pt(30),
										CornerRadius:   8,
										Background:     p.TextPrimary,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   p.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										if _, err := ed.SubmitComposerTask(); err != nil {
											ed.report(err)
										}
									},
									Children: []core.Node{
										core.Icon{
											SVG:   lucide.ArrowUp(),
											Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.OnTextPrimary},
										},
									},
								},
							},
						},
					},
				},
				core.Text{
					TestID: "tasks-mode-hint",
					ContentFunc: func() string {
						_ = rev.Get()
						return ed.TaskComposerHint()
					},
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextFaint,
					},
				},
			},
		}

		return core.View{
			Ref:    panelRef,
			TestID: "tasks-panel",
			Role:   core.RoleRegion,
			Label:  ed.T("tasks.panelTitle"),
			Style: dockedStyle(cx, core.Style{
				Width:       core.Pt(368),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
			}),
			Children: []core.Node{
				header,
				listSection,
				detailSection,
				composer,
			},
		}
	})
}

func taskCountChip(dotColor, textColor core.Color, label string) core.Node {
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        4,
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
				Content: label,
				Style: core.Style{
					FontSize: 11.5,
					Color:    textColor,
				},
			},
		},
	}
}

func taskRowView(ed *Editor, p SpecPalette, t AgentTask, focused bool) core.Node {
	id := t.ID
	rowBg := core.Transparent
	if focused {
		rowBg = ui.Alpha(p.Accent.Fg, 0.16)
	}
	padLeft := core.Points(8)
	if t.State == TaskReadyToReview {
		padLeft = 0
	}

	var children []core.Node
	if t.State == TaskReadyToReview {
		checked := ed.TaskDismissSelected(id)
		boxStyle := core.Style{
			Width:          core.Pt(14),
			Height:         core.Pt(14),
			CornerRadius:   4,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		}
		var boxKids []core.Node
		if checked {
			boxStyle.Background = p.Accent.Fg
			boxKids = []core.Node{
				core.Icon{
					SVG:   lucide.Check(),
					Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: p.Accent.OnFill},
				},
			}
		} else {
			boxStyle.BorderWidth = 1.5
			boxStyle.BorderColor = p.TextFaint
		}
		children = append(children, core.View{
			TestID:    "task-check-" + id,
			Role:      core.RoleCheckbox,
			Label:     fmt.Sprintf("Select %q", t.Title),
			Focusable: func() bool { return true },
			Style: core.Style{
				Width:          core.Pt(28),
				Height:         core.Pt(28),
				FlexShrink:     0,
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleTaskDismissSelection(id)
			},
			Children: []core.Node{
				core.View{Style: boxStyle, Children: boxKids},
			},
		})
	}

	metaColor := p.TextFaint
	switch t.State {
	case TaskNeedsYou:
		metaColor = p.Warning.Fg
	case TaskFailed:
		metaColor = p.Removed.Fg
	}

	elapsed := ed.TaskElapsedString(t)
	mainKids := []core.Node{
		taskStateIcon(p, t),
		core.View{
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				MinWidth:  core.Pt(0),
				Gap:       2,
			},
			Children: []core.Node{
				core.Text{
					Content:  t.Title,
					MaxLines: 1,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 500,
						Color:      p.TextPrimary,
					},
				},
				core.Text{
					TestID:   "task-meta-" + id,
					Content:  ed.FormatTaskMeta(t),
					MaxLines: 1,
					Style: core.Style{
						FontSize: 11.5,
						Color:    metaColor,
					},
				},
			},
		},
	}
	if elapsed != "" {
		mainKids = append(mainKids, core.Text{
			TestID:  "task-time-" + id,
			Content: elapsed,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11,
				Color:      p.TextFaint,
				FlexShrink: 0,
			},
		})
	}

	children = append(children, core.View{
		TestID:    "task-focus-" + id,
		Role:      core.RoleButton,
		Label:     t.Title,
		Focusable: func() bool { return true },
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			MinWidth:     core.Pt(0),
			Height:       core.Pt(40),
			Gap:          8,
			FocusVisible: p.FocusRing(-2),
		},
		OnPress: func(core.PressEvent) {
			ed.FocusTask(id)
		},
		Children: mainKids,
	})

	// Row action button (Open / Review / Retry).
	switch t.State {
	case TaskNeedsYou:
		children = append(children, taskRowActionButton(
			"task-action-"+id, ed.T("tasks.action.open"),
			p.Warning.Fill, p.Warning.Edge, p.Warning.Fg, p,
			func() { ed.report(ed.OpenTask(id)) },
		))
	case TaskReadyToReview:
		children = append(children, taskRowActionButton(
			"task-action-"+id, ed.T("tasks.action.review"),
			p.LineRaised, core.Transparent, p.TextPrimary, p,
			func() { ed.report(ed.OpenTask(id)) },
		))
	case TaskFailed:
		children = append(children, taskRowActionButton(
			"task-action-"+id, ed.T("tasks.action.retry"),
			p.LineRaised, core.Transparent, p.TextPrimary, p,
			func() { ed.report(ed.RetryTask(id)) },
		))
	}

	return core.View{
		TestID: "task-row-" + id,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			Padding:      core.Edges{Left: padLeft, Right: 8},
			CornerRadius: 8,
			Background:   rowBg,
		},
		Children: children,
	}
}

func taskRowActionButton(testID, label string, bg, border, fg core.Color, p SpecPalette, onPress func()) core.Node {
	st := core.Style{
		Height:         core.Pt(28),
		Padding:        core.Symmetric(0, 12),
		FlexShrink:     0,
		CornerRadius:   8,
		Background:     bg,
		AlignItems:     core.AlignCenter,
		JustifyContent: core.JustifyCenter,
		FocusVisible:   p.FocusRing(2),
	}
	if border != core.Transparent {
		st.BorderWidth = 1
		st.BorderColor = border
	}
	return core.View{
		TestID:    testID,
		Role:      core.RoleButton,
		Label:     label,
		Focusable: func() bool { return true },
		Style:     st,
		OnPress:   func(core.PressEvent) { onPress() },
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize:   11.5,
					FontWeight: 500,
					Color:      fg,
				},
			},
		},
	}
}

func taskStateIcon(p SpecPalette, t AgentTask) core.Node {
	return core.Canvas{
		Style: core.Style{Width: core.Pt(16), Height: core.Pt(16), FlexShrink: 0},
		Draw: func(d *core.Drawing, _ core.Size) {
			d.SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
			switch {
			case t.State == TaskRunning && !t.Holding():
				d.SetStrokeColor(p.Agent.Fg).SetLineWidth(2)
				d.BeginPath().Arc(8, 8, 5.5, -float32(math.Pi/2), float32(math.Pi), false).Stroke()
			case t.State == TaskRunning && t.Holding():
				d.SetStrokeColor(p.Agent.Fg).SetLineWidth(1.6)
				d.BeginPath().Ellipse(8, 8, 6, 6).Stroke()
				d.BeginPath().MoveTo(8, 5).LineTo(8, 8).LineTo(10.5, 9.5).Stroke()
			case t.State == TaskNeedsYou:
				d.SetStrokeColor(p.Warning.Fg).SetLineWidth(1.6)
				d.BeginPath().Ellipse(8, 8, 6, 6).Stroke()
				d.BeginPath().MoveTo(8, 5).LineTo(8, 8.6).Stroke()
				d.SetFillColor(p.Warning.Fg).BeginPath().Ellipse(8, 11, 0.9, 0.9).Fill()
			case t.State == TaskReadyToReview:
				d.SetFillColor(p.Accent.Fg).BeginPath().Ellipse(8, 8, 4, 4).Fill()
			case t.State == TaskQueued:
				d.SetStrokeColor(p.TextFaint).SetLineWidth(1.5).SetLineDash(2.5, 2)
				d.BeginPath().Ellipse(8, 8, 5.2, 5.2).Stroke()
			case t.State == TaskFailed:
				d.SetStrokeColor(p.Removed.Fg).SetLineWidth(1.6)
				d.BeginPath().Ellipse(8, 8, 6, 6).Stroke()
				d.BeginPath().MoveTo(5.8, 5.8).LineTo(10.2, 10.2).MoveTo(10.2, 5.8).LineTo(5.8, 10.2).Stroke()
			}
		},
	}
}

func taskDetailView(ed *Editor, p SpecPalette, focused AgentTask, hasFocused bool) core.Node {
	if !hasFocused {
		return core.View{
			TestID: "tasks-detail",
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Padding:   core.Symmetric(12, 16),
			},
		}
	}

	badgeNum := ""
	if focused.Number > 0 {
		badgeNum = fmt.Sprintf("%d", focused.Number)
	}
	var headKids []core.Node
	if badgeNum != "" {
		badgeStyle := core.Style{
			Width:          core.Pt(16),
			Height:         core.Pt(16),
			CornerRadius:   8,
			FlexShrink:     0,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		}
		numColor := p.Agent.OnFill
		if focused.Holding() {
			badgeStyle.BorderWidth = 1.5
			badgeStyle.BorderColor = p.Agent.Edge
			numColor = p.TextSecondary
		} else {
			badgeStyle.Background = p.Agent.Fg
		}
		headKids = append(headKids, core.View{
			Style: badgeStyle,
			Children: []core.Node{
				core.Text{
					Content: badgeNum,
					Style: core.Style{
						FontSize:   10,
						FontWeight: 600,
						Color:      numColor,
					},
				},
			},
		})
	}
	headKids = append(headKids, core.Text{
		TestID:   "tasks-detail-title",
		Content:  strings.ToUpper(focused.Title),
		MaxLines: 1,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.6,
			Color:         p.TextFaint,
			FlexGrow:      1,
			FlexBasis:     core.Pt(0),
		},
	})

	detailKids := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: headKids,
		},
	}

	for _, step := range focused.Steps {
		textColor := p.TextFaint
		if step.Active {
			textColor = p.TextPrimary
		} else if step.Blocked {
			textColor = p.TextMuted
		}
		icon := core.Canvas{
			Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), FlexShrink: 0},
			Draw: func(d *core.Drawing, _ core.Size) {
				d.SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
				switch {
				case step.Done:
					d.SetFillColor(p.Added.Fill).BeginPath().Ellipse(7, 7, 7, 7).Fill()
					d.SetStrokeColor(p.Added.Fg).SetLineWidth(1.8)
					d.BeginPath().MoveTo(4.2, 7.2).LineTo(6.2, 9.2).LineTo(10, 5).Stroke()
				case step.Blocked:
					d.SetStrokeColor(p.Agent.Fg).SetLineWidth(1.5)
					d.BeginPath().Ellipse(7, 7, 5.5, 5.5).Stroke()
					d.BeginPath().MoveTo(7, 4.5).LineTo(7, 7).LineTo(9, 8.2).Stroke()
				default:
					d.SetStrokeColor(p.Agent.Fg).SetLineWidth(1.8)
					d.BeginPath().Arc(7, 7, 5.2, -float32(math.Pi/2), float32(math.Pi), false).Stroke()
				}
			},
		}
		detailKids = append(detailKids, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				icon,
				core.Text{
					Content:  step.Summary,
					MaxLines: 2,
					Style: core.Style{
						FontSize:  12,
						Color:     textColor,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
				},
			},
		})
	}

	// Contextual action buttons for the focused task.
	id := focused.ID
	var actionRow []core.Node
	switch focused.State {
	case TaskNeedsYou:
		if focused.PermissionDetail != "" {
			detailKids = append(detailKids, core.Text{
				Content: focused.PermissionDetail,
				Style: core.Style{
					FontSize:   12,
					LineHeight: 17,
					Color:      p.TextSecondary,
				},
			})
		}
		actionRow = append(actionRow,
			taskRowActionButton("tasks-detail-allow", ed.T("tasks.action.allowOnce"), p.Accent.Fg, core.Transparent, p.Accent.OnFill, p, func() {
				ed.report(ed.ResolveTaskPermission(id, true))
			}),
			taskRowActionButton("tasks-detail-deny", ed.T("tasks.action.deny"), p.LineRaised, core.Transparent, p.TextSecondary, p, func() {
				ed.report(ed.ResolveTaskPermission(id, false))
			}),
		)
	case TaskRunning:
		actionRow = append(actionRow,
			taskRowActionButton("tasks-detail-stop", ed.T("tasks.action.stop"), p.LineRaised, core.Transparent, p.TextSecondary, p, func() {
				ed.report(ed.StopTask(id))
			}),
		)
	case TaskReadyToReview:
		actionRow = append(actionRow,
			taskRowActionButton("tasks-detail-keep", ed.T("tasks.action.keep"), p.Accent.Fg, core.Transparent, p.Accent.OnFill, p, func() {
				ed.report(ed.KeepTask(id))
			}),
			taskRowActionButton("tasks-detail-revert", ed.T("tasks.action.revert"), p.LineRaised, core.Transparent, p.TextSecondary, p, func() {
				ed.report(ed.RevertTask(id))
			}),
		)
	case TaskQueued:
		actionRow = append(actionRow,
			taskRowActionButton("tasks-detail-move-up", ed.T("tasks.action.moveUp"), p.LineRaised, core.Transparent, p.TextSecondary, p, func() {
				ed.report(ed.ReorderQueuedTask(id, -1))
			}),
			taskRowActionButton("tasks-detail-cancel", ed.T("tasks.action.cancel"), p.LineRaised, core.Transparent, p.TextSecondary, p, func() {
				ed.report(ed.StopTask(id))
			}),
		)
	case TaskFailed:
		if focused.Error != "" {
			detailKids = append(detailKids, core.Text{
				Content: focused.Error,
				Style: core.Style{
					FontSize: 12,
					Color:    p.Removed.Fg,
				},
			})
		}
		actionRow = append(actionRow,
			taskRowActionButton("tasks-detail-retry", ed.T("tasks.action.retry"), p.LineRaised, core.Transparent, p.TextPrimary, p, func() {
				ed.report(ed.RetryTask(id))
			}),
			taskRowActionButton("tasks-detail-dismiss", ed.T("tasks.action.dismiss"), p.Raised, p.LineRaised, p.TextMuted, p, func() {
				ed.DismissTasks([]string{id})
			}),
		)
	}
	if len(actionRow) > 0 {
		detailKids = append(detailKids, core.View{
			Style: core.Style{
				Direction: core.Row,
				Gap:       8,
				Margin:    core.Edges{Top: 4},
			},
			Children: actionRow,
		})
	}

	return core.View{
		TestID: "tasks-detail",
		Style: core.Style{
			Padding: core.Symmetric(12, 16),
			Gap:     8,
		},
		Children: detailKids,
	}
}

// viewportTaskLegendOverlay renders the compact bottom-left task overlay legend in the viewport
// whenever one or more running tasks have active numbered overlays on the open scene, matching
// design/editor/Tasks.dc.html.
func viewportTaskLegendOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	legendKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Playing() || ed.ViewMode != ViewMode2D {
			return ""
		}
		ovs := ed.SceneTaskOverlays()
		if len(ovs) == 0 {
			return ""
		}
		var b strings.Builder
		if cx.Dark() {
			b.WriteString("D:")
		} else {
			b.WriteString("L:")
		}
		seen := make(map[string]bool)
		for _, ov := range ovs {
			k := fmt.Sprintf("%d:%v", ov.TaskNumber, ov.Holding)
			if seen[k] {
				continue
			}
			seen[k] = true
			b.WriteString(k)
			b.WriteByte('|')
		}
		return b.String()
	})

	return core.Show{
		When: func() bool { return legendKey.Get() != "" },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = legendKey.Get()
				p := ed.SpecPalette(cx)
				ovs := ed.SceneTaskOverlays()
				type entry struct {
					num     int
					holding bool
				}
				var entries []entry
				seen := make(map[entry]bool)
				for _, ov := range ovs {
					ent := entry{num: ov.TaskNumber, holding: ov.Holding}
					if !seen[ent] {
						seen[ent] = true
						entries = append(entries, ent)
					}
				}
				rows := make([]core.Node, 0, len(entries))
				for _, ent := range entries {
					col := p.Agent.Edge
					label := fmt.Sprintf("task %d editing", ent.num)
					if ent.holding {
						col = ui.Alpha(p.Agent.Edge, 0.45)
						label = fmt.Sprintf("task %d holding, not editing yet", ent.num)
					}
					rows = append(rows, core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(10),
									Height:       core.Pt(10),
									CornerRadius: 2,
									BorderWidth:  1.5,
									BorderColor:  col,
									FlexShrink:   0,
								},
							},
							core.Text{
								Content: label,
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextMuted,
								},
							},
						},
					})
				}
				return core.View{
					TestID:        "viewport-task-legend",
					PointerEvents: core.PointerEventsNone,
					Style: core.Style{
						Position:     core.Absolute,
						Left:         core.Pt(16),
						Bottom:       core.Pt(60),
						Padding:      core.Symmetric(8, 12),
						Gap:          4,
						CornerRadius: 12,
						Background:   ui.Alpha(p.Panel, 0.94),
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: rows,
				}
			})
		},
	}
}
