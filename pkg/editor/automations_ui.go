package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// automationsSettingsPageView renders the Settings › Automations page matching
// docs/design/editor/Settings.dc.html (Automations) using strictly SpecPalette tokens
// in both Spec Dark and Spec Light appearances.
//
// Inputs (automations-draft-input and automations-agent-ask) are kept outside dynamic
// list/error subtrees so typing, clearing validation errors, or background automation
// runs completing never remount text fields or steal keyboard focus.
func automationsSettingsPageView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	askDraft := core.NewSignal(cx, "")

	// Outer memo keys only on theme polarity and language so interactive state updates
	// never remount the page or steal focus from text inputs.
	themeLangKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%v|%s", ed.SpecPalette(cx).Dark, ed.ResolvedLanguage())
	})

	rowsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		file := ed.Automations()
		var b strings.Builder
		fmt.Fprintf(&b, "%d|", len(file.Automations))
		for _, r := range file.Automations {
			fmt.Fprintf(&b, "%s:%v:%d:%s:%s;", r.ID, r.Enabled, ed.AutomationWeeklyRuns(r.ID), r.DisplayName(), r.Instruction)
		}
		return b.String()
	})

	draftCardKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		d := ed.PendingAutomationDraft()
		if d == nil {
			return ""
		}
		return d.Rule.ID + "|" + d.YAML
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = themeLangKey.Get()
		p := ed.SpecPalette(cx)
		focusRing := p.FocusRing(2)

		pauseLabelFunc := func() string {
			_ = rev.Get()
			if ed.AutomationsPausedToday() {
				return ed.T("settings.automations.resume")
			}
			return ed.T("settings.automations.pauseToday")
		}

		loadErrBanner := core.Show{
			When: func() bool {
				_ = rev.Get()
				return ed.AutomationsLoadError() != nil
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					TestID: "automations-load-error",
					Role:   core.RoleAlert,
					Style: core.Style{
						CornerRadius: 10,
						Background:   p.Removed.Fill,
						BorderWidth:  1,
						BorderColor:  p.Removed.Edge,
						Padding:      core.Symmetric(10, 12),
						Gap:          4,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("settings.automations.loadErrorTitle"),
							Style:   core.Style{FontSize: 12, FontWeight: 600, Color: p.Removed.Fg},
						},
						core.Text{
							ContentFunc: func() string {
								_ = rev.Get()
								if err := ed.AutomationsLoadError(); err != nil {
									return err.Error()
								}
								return ""
							},
							Style: core.Style{FontSize: 11.5, Color: p.TextSecondary},
						},
					},
				}
			},
		}

		pausedBanner := core.Show{
			When: func() bool {
				_ = rev.Get()
				return ed.AutomationsPausedToday()
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					TestID: "automations-paused-banner",
					Role:   core.RoleStatus,
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            12,
						CornerRadius:   10,
						Background:     p.Warning.Fill,
						BorderWidth:    1,
						BorderColor:    p.Warning.Edge,
						Padding:        core.Symmetric(10, 14),
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
							Children: []core.Node{
								core.Text{
									Content: ed.T("settings.automations.pausedTitle"),
									Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.Warning.Fg},
								},
								core.Text{
									Content: ed.T("settings.automations.pausedHelp"),
									Style:   core.Style{FontSize: 11.5, Color: p.TextSecondary},
								},
							},
						},
						settingsSpecButton(p, "automations-banner-resume", ed.T("settings.automations.resume"), ed.T("settings.automations.resume"), false, nil, func() {
							ed.report(ed.ResumeAutomations())
						}),
					},
				}
			},
		}

		rowsList := core.Dynamic(func(cx *core.Scope) core.Node {
			_ = rowsKey.Get()
			file := ed.Automations()
			rows := make([]core.Node, 0, len(file.Automations))
			for _, rule := range file.Automations {
				r := rule
				weeklyRuns := ed.AutomationWeeklyRuns(r.ID)
				runsText := ed.T("settings.automations.runsThisWeek", l10n.Param("count", fmt.Sprintf("%d", weeklyRuns)))
				if weeklyRuns == 1 {
					runsText = ed.T("settings.automations.runThisWeekOne")
				}
				runsColor := p.TextFaint
				if weeklyRuns > 0 {
					runsColor = p.Agent.Fg
				}
				whenDetail := automationTriggerDetailText(r.Trigger)
				whenChildren := []core.Node{
					core.Text{
						Content: ed.T("settings.automations.whenLabel"),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.66,
							Color:         p.TextFaint,
						},
					},
					core.Text{
						TestID:  "automation-when-" + r.ID,
						Content: r.DisplayName(),
						Style: core.Style{
							FontSize:   12.5,
							FontWeight: 500,
							Color:      p.TextPrimary,
						},
					},
				}
				if whenDetail != "" {
					whenChildren = append(whenChildren, core.Text{
						Content: whenDetail,
						Style: core.Style{
							FontSize: 11,
							Color:    p.TextFaint,
						},
					})
				}

				rows = append(rows, core.View{
					TestID: "automation-row-" + r.ID,
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          12,
						Padding:      core.Symmetric(12, 14),
						CornerRadius: 12,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:      core.Pt(196),
								FlexShrink: 0,
								Gap:        2,
							},
							Children: whenChildren,
						},
						core.Text{
							Content: "→",
							Style: core.Style{
								FontSize:   12.5,
								Color:      p.TextFaint,
								FlexShrink: 0,
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
									Content: ed.T("settings.automations.agentLabel"),
									Style: core.Style{
										FontSize:      11,
										FontWeight:    600,
										LetterSpacing: 0.66,
										Color:         p.TextFaint,
									},
								},
								core.Text{
									TestID:  "automation-then-" + r.ID,
									Content: r.Instruction,
									Style: core.Style{
										FontSize: 12.5,
										Color:    p.TextPrimary,
									},
								},
								core.Text{
									TestID:  "automation-runs-" + r.ID,
									Content: runsText,
									Style: core.Style{
										FontSize: 11,
										Color:    runsColor,
									},
								},
							},
						},
						SpecSwitch{
							TestID: "automation-switch-" + r.ID,
							Label:  r.DisplayName(),
							Checked: func() bool {
								_ = rev.Get()
								for _, cur := range ed.Automations().Automations {
									if cur.ID == r.ID {
										return cur.Enabled
									}
								}
								return false
							},
							OnChange: func(on bool) {
								ed.report(ed.SetAutomationEnabled(r.ID, on))
							},
						},
					},
				})
			}
			return core.View{
				Style:    core.Style{Gap: 10},
				Children: rows,
			}
		})

		actionsRow := core.View{
			TestID: "automations-actions",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        10,
				Padding:    core.Edges{Top: 4},
			},
			Children: []core.Node{
				ui.Button{
					TestID:  "automations-add-btn",
					Text:    ed.T("settings.automations.newInWords"),
					Label:   ed.T("settings.automations.newInWords"),
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return p.Bg },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.TextMuted,
					},
					Style: core.Style{
						Height:       core.Pt(32),
						Padding:      core.Symmetric(0, 12),
						CornerRadius: 8,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FocusVisible: focusRing,
					},
					OnPress: func() {
						ed.BeginNewAutomationDraft()
					},
				},
				ui.Button{
					TestID:    "automations-pause-btn",
					TextFunc:  pauseLabelFunc,
					LabelFunc: pauseLabelFunc,
					Variant:   ui.ButtonGhost,
					Size:      ui.SizeSmall,
					Color:     func() core.Color { return p.Raised },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 600,
						Color:      p.TextSecondary,
					},
					Style: core.Style{
						Height:       core.Pt(28),
						CornerRadius: 8,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FlexShrink:   0,
						FocusVisible: focusRing,
					},
					OnPress: func() {
						if ed.AutomationsPausedToday() {
							ed.report(ed.ResumeAutomations())
						} else {
							ed.report(ed.PauseAutomationsForToday())
						}
					},
				},
			},
		}

		// Inline "+ New automation, in words" composer; kept stably mounted while open so typing never loses focus.
		draftBox := core.Show{
			When: func() bool {
				_ = rev.Get()
				return ed.AutomationDraftOpen()
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					TestID: "automations-draft-box",
					Style: core.Style{
						CornerRadius: 12,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Padding:      core.All(14),
						Gap:          10,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("settings.automations.draftHeading"),
							Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary},
						},
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        8,
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
									Children: []core.Node{
										ui.Input{
											TestID:      "automations-draft-input",
											Label:       ed.T("settings.automations.draftInputLabel"),
											Placeholder: ed.T("settings.automations.draftPlaceholder"),
											Value: func() string {
												_ = rev.Get()
												return ed.AutomationDraftText()
											},
											OnChange: func(v string) {
												ed.SetAutomationDraftText(v)
											},
											OnSubmit: func(v string) {
												_, _ = ed.DraftAutomationFromWords(v)
											},
										},
									},
								},
								settingsSpecButton(p, "automations-draft-submit", ed.T("settings.automations.draftAction"), ed.T("settings.automations.draftAction"), true, nil, func() {
									_, _ = ed.DraftAutomationFromWords(ed.AutomationDraftText())
								}),
								settingsSpecButton(p, "automations-draft-cancel", ed.T("dialog.cancel"), ed.T("dialog.cancel"), false, nil, func() {
									ed.CancelNewAutomationDraft()
								}),
							},
						},
						core.Show{
							When: func() bool {
								_ = rev.Get()
								return ed.AutomationDraftError() != ""
							},
							Then: func(cx *core.Scope) core.Node {
								return core.Text{
									TestID: "automations-draft-error",
									ContentFunc: func() string {
										_ = rev.Get()
										return ed.AutomationDraftError()
									},
									Style: core.Style{FontSize: 11.5, Color: p.Removed.Fg},
								}
							},
						},
						core.Dynamic(func(cx *core.Scope) core.Node {
							if draftCardKey.Get() == "" {
								return nil
							}
							draft := ed.PendingAutomationDraft()
							if draft == nil {
								return nil
							}
							return core.View{
								TestID: "automations-draft-card",
								Style: core.Style{
									CornerRadius: 10,
									Background:   p.Inset,
									BorderWidth:  1,
									BorderColor:  p.Agent.Edge,
									Padding:      core.All(12),
									Gap:          8,
								},
								Children: []core.Node{
									core.Text{
										Content: fmt.Sprintf("%s → %s", draft.Rule.DisplayName(), draft.Rule.Instruction),
										Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
									},
									core.Text{
										TestID:  "automations-draft-yaml",
										Content: strings.TrimSpace(draft.YAML),
										Style: core.Style{
											FontSize: 11,
											Color:    p.TextMuted,
										},
									},
									core.View{
										Style: core.Style{
											Direction:  core.Row,
											AlignItems: core.AlignCenter,
											Gap:        8,
										},
										Children: []core.Node{
											settingsSpecButton(p, "automations-draft-keep", ed.T("settings.automations.keepDraft"), ed.T("settings.automations.keepDraft"), true, nil, func() {
												ed.report(ed.KeepAutomationDraft())
											}),
											settingsSpecButton(p, "automations-draft-discard", ed.T("dialog.discard"), ed.T("dialog.discard"), false, nil, func() {
												ed.CancelNewAutomationDraft()
											}),
										},
									},
								},
							}
						}),
					},
				}
			},
		}

		// Right-hand "Agent on this page" rail matching docs/design/editor/Settings.dc.html.
		suggestAddText := ed.T("settings.automations.suggestAdd")
		agentCard := core.View{
			TestID: "settings-agent-card",
			Style: core.Style{
				Width:       core.Pt(280),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				Padding:     core.All(16),
				Gap:         12,
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
								Width:        core.Pt(7),
								Height:       core.Pt(7),
								CornerRadius: 4,
								Background:   p.Agent.Fg,
								FlexShrink:   0,
							},
						},
						core.Text{
							Content: ed.T("settings.agentOnPage"),
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: 600,
								Color:      p.Agent.Fg,
							},
						},
					},
				},
				core.Text{
					TestID: "automations-agent-note",
					ContentFunc: func() string {
						_ = rev.Get()
						return automationAgentPageNote(ed, ed.Automations())
					},
					Style: core.Style{
						FontSize: 12,
						Color:    p.TextMuted,
					},
				},
				base.Button{
					TestID: "automations-suggest-add",
					Label:  suggestAddText,
					Style: core.Style{
						Width:        core.Pct(100),
						Padding:      core.Symmetric(10, 12),
						CornerRadius: 10,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FocusVisible: focusRing,
					},
					OnPress: func(core.PressEvent) {
						_, _ = ed.DraftAutomationFromWords(suggestAddText)
					},
					Children: []core.Node{
						core.Text{
							Content: suggestAddText,
							Style: core.Style{
								FontSize:  12,
								Color:     p.TextSecondary,
								TextAlign: core.TextAlignLeft,
							},
						},
					},
				},
				base.Button{
					TestID:    "automations-suggest-pause",
					LabelFunc: pauseLabelFunc,
					Style: core.Style{
						Width:        core.Pct(100),
						Padding:      core.Symmetric(10, 12),
						CornerRadius: 10,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FocusVisible: focusRing,
					},
					OnPress: func(core.PressEvent) {
						if ed.AutomationsPausedToday() {
							ed.report(ed.ResumeAutomations())
						} else {
							ed.report(ed.PauseAutomationsForToday())
						}
					},
					Children: []core.Node{
						core.Text{
							ContentFunc: pauseLabelFunc,
							Style: core.Style{
								FontSize:  12,
								Color:     p.TextSecondary,
								TextAlign: core.TextAlignLeft,
							},
						},
					},
				},
				core.View{Style: core.Style{FlexGrow: 1}},
				ui.Input{
					TestID:      "automations-agent-ask",
					Label:       ed.T("settings.automations.askLabel"),
					Placeholder: ed.T("settings.automations.askPlaceholder"),
					Value:       askDraft.Get,
					OnChange:    askDraft.Set,
					OnSubmit: func(q string) {
						if strings.TrimSpace(q) == "" {
							return
						}
						_, _ = ed.DraftAutomationFromWords(q)
						askDraft.Set("")
					},
				},
			},
		}

		return core.View{
			TestID: "automations-settings-panel",
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				Direction: core.Row,
				Gap:       16,
				Overflow:  core.Hidden,
			},
			Children: []core.Node{
				ui.ScrollView{
					TestID: "automations-list-scroll",
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Gap:     10,
								Padding: core.Edges{Bottom: 16},
							},
							Children: []core.Node{
								loadErrBanner,
								pausedBanner,
								rowsList,
								actionsRow,
								draftBox,
							},
						},
					},
				},
				agentCard,
			},
		}
	})
}

func automationTriggerDetailText(tr AutomationTrigger) string {
	switch tr.Event {
	case AutomationTriggerFileChanged:
		return tr.Glob
	case AutomationTriggerFileAdded:
		if tr.Glob != "" {
			return tr.Glob
		}
		if tr.Kind != "" && tr.Kind != AutomationFileKindAny {
			return string(tr.Kind)
		}
		return ""
	case AutomationTriggerBeforeExport:
		return tr.Platform
	case AutomationTriggerDaily:
		return tr.Time
	case AutomationTriggerReviewKept:
		return tr.Glob
	default:
		return ""
	}
}

func automationAgentPageNote(ed *Editor, file *AutomationsFile) string {
	if ed.AutomationsPausedToday() {
		return ed.T("settings.automations.agentNotePaused")
	}
	topName := ""
	topRuns := 0
	for _, r := range file.Automations {
		if n := ed.AutomationWeeklyRuns(r.ID); n > topRuns {
			topRuns = n
			topName = r.DisplayName()
		}
	}
	if topRuns == 1 {
		return ed.T("settings.automations.agentNoteRunsOne",
			l10n.Param("name", topName),
		)
	}
	if topRuns > 1 {
		return ed.T("settings.automations.agentNoteRuns",
			l10n.Param("name", topName),
			l10n.Param("count", fmt.Sprintf("%d", topRuns)),
		)
	}
	return ed.T("settings.automations.agentNoteDefault")
}
