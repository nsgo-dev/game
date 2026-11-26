package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// HistoryPanelView renders the unified History timeline right-side panel (AskInPlace.dc.html and
// Checkpoints.dc.html) using exclusively SpecPalette tokens:
//   - Header with All / You / Agent filter segmented control and close button
//   - Active point-in-time preview bar ("Previewing … · editing is off") with Restore and Back (Esc)
//   - Top "Now" row showing unsaved/saved status (clicking exits preview)
//   - Newest-first timeline entries grouped by day (TODAY / YESTERDAY / date), with author badges,
//     detail, touched files, relative times, and saved-point markers
//   - Footer explaining non-destructive preview and Restore
func HistoryPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		p := ed.SpecPalette(cx)
		filter := ed.HistoryFilter()
		entries := ed.FilteredHistoryEntries(filter)
		previewEntry, previewing := ed.HistoryPreviewEntry()
		dirty := ed.HasUnsavedChanges()

		filterBtn := func(testID string, target HistoryFilter, label string) core.Node {
			active := filter == target
			bg := core.Transparent
			fg := p.TextFaint
			if active {
				bg = p.LineRaised
				fg = p.TextPrimary
			}
			return core.View{
				TestID:    testID,
				Role:      core.RoleRadio,
				Label:     label,
				Checked:   func() bool { return active },
				Focusable: func() bool { return true },
				Style: core.Style{
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					Background:     bg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.SetHistoryFilter(target)
				},
				Children: []core.Node{
					core.Text{
						Content: label,
						Style: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      fg,
						},
					},
				},
			}
		}

		header := core.View{
			TestID: "history-panel-header",
			Style: core.Style{
				Height:         core.Pt(44),
				FlexShrink:     0,
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
				Padding:        core.Edges{Left: 16, Right: 10},
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    p.Line,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "history-panel-title",
					Content: ed.T("history.title"),
					Style: core.Style{
						FontSize:   14,
						FontWeight: 600,
						Color:      p.TextPrimary,
						FlexShrink: 0,
					},
				},
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        6,
					},
					Children: []core.Node{
						core.View{
							TestID: "history-filter-group",
							Role:   core.RoleRadioGroup,
							Label:  ed.T("history.filterLabel"),
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          2,
								Padding:      core.All(2),
								CornerRadius: 8,
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Children: []core.Node{
								filterBtn("history-filter-all", HistoryFilterAll, ed.T("history.filter.all")),
								filterBtn("history-filter-you", HistoryFilterYou, ed.T("history.filter.you")),
								filterBtn("history-filter-agent", HistoryFilterAgent, ed.T("history.filter.agent")),
							},
						},
						core.View{
							TestID:    "history-panel-close",
							Role:      core.RoleButton,
							Label:     ed.T("history.close"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Width:          core.Pt(28),
								Height:         core.Pt(28),
								CornerRadius:   6,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.SetHistoryPanelOpen(false)
							},
							Children: []core.Node{
								ui.Icon(lucide.X, 14, func() core.Color { return p.TextMuted }),
							},
						},
					},
				},
			},
		}

		children := []core.Node{header}

		if previewing {
			ago := ed.FormatHistoryRelativeTime(previewEntry.Timestamp)
			children = append(children, core.View{
				TestID: "history-panel-preview-bar",
				Role:   core.RoleStatus,
				Style: core.Style{
					FlexShrink:  0,
					Padding:     core.Symmetric(8, 12),
					Gap:         8,
					Background:  p.Accent.Fill,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.Accent.Edge,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: []core.Node{
							ui.Icon(lucide.Eye, 15, func() core.Color { return p.Accent.Fg }),
							core.Text{
								TestID:  "history-panel-preview-label",
								Content: fmt.Sprintf("Previewing %s · editing is off", ago),
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextPrimary,
									FlexShrink: 1,
									MinWidth:   core.Pt(0),
								},
							},
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
								TestID:    "history-preview-restore",
								Role:      core.RoleButton,
								Label:     ed.T("history.restore"),
								Focusable: func() bool { return true },
								Style: core.Style{
									Height:         core.Pt(28),
									Padding:        core.Symmetric(0, 12),
									CornerRadius:   8,
									Background:     p.Accent.Fg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FocusVisible:   p.FocusRing(2),
								},
								OnPress: func(core.PressEvent) {
									ed.report(ed.RestoreHistoryEntry(previewEntry.ID))
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("history.restore"),
										Style: core.Style{
											FontSize:   12,
											FontWeight: 600,
											Color:      p.Accent.OnFill,
										},
									},
								},
							},
							core.View{
								TestID:    "history-preview-back",
								Role:      core.RoleButton,
								Label:     ed.T("history.back"),
								Focusable: func() bool { return true },
								Style: core.Style{
									Direction:      core.Row,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									Gap:            6,
									Height:         core.Pt(28),
									Padding:        core.Symmetric(0, 12),
									CornerRadius:   8,
									Background:     p.LineRaised,
									FocusVisible:   p.FocusRing(2),
								},
								OnPress: func(core.PressEvent) {
									ed.ExitHistoryPreview()
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("history.back"),
										Style: core.Style{
											FontSize:   12,
											FontWeight: 500,
											Color:      p.TextPrimary,
										},
									},
									core.Text{
										Content: "Esc",
										Style: core.Style{
											FontFamily: core.FontMonospace,
											FontSize:   11,
											Color:      p.TextMuted,
										},
									},
								},
							},
						},
					},
				},
			})
		}

		listRows := make([]core.Node, 0, len(entries)+6)

		// Top "Now" row (AskInPlace.dc.html): marked unsaved when dirty; clicking ends preview.
		nowStatus := ed.T("history.saved")
		nowStatusColor := p.Added.Fg
		if dirty {
			nowStatus = ed.T("history.unsaved")
			nowStatusColor = p.Changed.Fg
		}
		nowRowStyle := core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignStart,
			Gap:          10,
			Padding:      core.Symmetric(8, 10),
			CornerRadius: 8,
			FocusVisible: p.FocusRing(-2),
		}
		if !previewing {
			nowRowStyle.Background = p.Raised
			nowRowStyle.BorderWidth = 1
			nowRowStyle.BorderColor = p.LineRaised
		}
		listRows = append(listRows, core.View{
			TestID:    "history-row-now",
			Role:      core.RoleButton,
			Label:     ed.T("history.now"),
			Focusable: func() bool { return true },
			Style:     nowRowStyle,
			OnPress: func(core.PressEvent) {
				ed.ExitHistoryPreview()
			},
			Children: []core.Node{
				historyTimelineRail(p, p.Accent.Fg, true, len(entries) > 0),
				core.View{
					Style: core.Style{
						FlexGrow: 1,
						MinWidth: core.Pt(0),
						Gap:      2,
					},
					Children: []core.Node{
						core.Text{
							TestID:  "history-now-title",
							Content: ed.T("history.now"),
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: 600,
								Color:      p.TextPrimary,
							},
						},
						core.Text{
							TestID:  "history-now-status",
							Content: nowStatus,
							Style: core.Style{
								FontSize: 11.5,
								Color:    nowStatusColor,
							},
						},
					},
				},
			},
		})

		if len(entries) == 0 {
			emptyText := ed.T("history.empty")
			if filter == HistoryFilterAgent {
				emptyText = ed.T("history.emptyAgent")
			} else if filter == HistoryFilterYou {
				emptyText = ed.T("history.emptyYou")
			}
			listRows = append(listRows, core.View{
				TestID: "history-empty",
				Style: core.Style{
					Padding: core.Symmetric(16, 12),
				},
				Children: []core.Node{
					core.Text{
						Content: emptyText,
						Style: core.Style{
							FontSize: 12,
							Color:    p.TextFaint,
						},
					},
				},
			})
		} else {
			lastDay := ""
			for i, entry := range entries {
				day := ed.FormatHistoryDayLabel(entry.Timestamp)
				if day != lastDay {
					lastDay = day
					listRows = append(listRows, core.View{
						TestID: "history-day-" + strings.ToLower(day),
						Style: core.Style{
							Height:         core.Pt(28),
							Padding:        core.Edges{Left: 8, Right: 8, Bottom: 4},
							JustifyContent: core.JustifyEnd,
						},
						Children: []core.Node{
							core.Text{
								Content: day,
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.6,
									Color:         p.TextFaint,
								},
							},
						},
					})
				}
				isSelected := previewing && previewEntry.ID == entry.ID
				hasMore := i < len(entries)-1
				listRows = append(listRows, historyEntryRowView(ed, p, entry, isSelected, hasMore))
			}
		}

		children = append(children,
			core.Scroll{
				TestID: "history-panel-list",
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Padding:   core.All(8),
				},
				Children: []core.Node{
					core.View{
						Style:    core.Style{Gap: 4},
						Children: listRows,
					},
				},
			},
			core.View{
				TestID: "history-panel-footer",
				Style: core.Style{
					FlexShrink:  0,
					Padding:     core.Symmetric(10, 14),
					Borders:     core.Edges{Top: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("history.footer"),
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextFaint,
						},
					},
				},
			},
		)

		return core.View{
			TestID: "history-panel",
			Style: core.Style{
				Width:       core.Pt(340),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
			},
			Children: children,
		}
	})
}

func historyTimelineRail(p SpecPalette, dotColor core.Color, filled bool, drawLine bool) core.Node {
	return core.Canvas{
		Style: core.Style{
			Width:      core.Pt(16),
			Height:     core.Pt(36),
			FlexShrink: 0,
		},
		Draw: func(d *core.Drawing, sz core.Size) {
			cx := float32(sz.Width) / 2
			cy := float32(9)
			if drawLine {
				d.SetStrokeColor(p.LineRaised).SetLineWidth(1.5)
				d.BeginPath().MoveTo(cx, cy+6).LineTo(cx, float32(sz.Height)).Stroke()
			}
			if filled {
				d.SetFillColor(p.Accent.Fill).BeginPath().Ellipse(cx, cy, 7, 7).Fill()
				d.SetFillColor(dotColor).BeginPath().Ellipse(cx, cy, 4.5, 4.5).Fill()
			} else {
				d.SetFillColor(p.Panel).BeginPath().Ellipse(cx, cy, 4.5, 4.5).Fill()
				d.SetStrokeColor(dotColor).SetLineWidth(1.5).BeginPath().Ellipse(cx, cy, 4.5, 4.5).Stroke()
			}
		},
	}
}

func historyEntryBadgeColors(p SpecPalette, entry HistoryEntry) (label string, dot, bg, fg core.Color) {
	if entry.Kind == HistoryEntrySaved {
		return "Saved", p.Added.Fg, p.Added.Fill, p.Added.Fg
	}
	switch entry.Author {
	case HistoryAuthorAgent:
		return "Agent", p.Agent.Fg, p.Agent.Fill, p.Agent.Fg
	case HistoryAuthorGit:
		return "git", p.TextMuted, p.LineRaised, p.TextSecondary
	default:
		return "You", p.TextFaint, p.Accent.Fill, p.Accent.Fg
	}
}

func historyEntryRowView(ed *Editor, p SpecPalette, entry HistoryEntry, selected bool, hasMore bool) core.Node {
	badgeLabel, dotColor, badgeBg, badgeFg := historyEntryBadgeColors(p, entry)
	relTime := ed.FormatHistoryRelativeTime(entry.Timestamp)
	headline := FormatHistoryEntryHeadline(entry)
	subtitle := FormatHistoryEntrySubtitle(entry, relTime)
	clockStr := entry.Timestamp.Format("15:04")

	rowStyle := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignStart,
		Gap:          10,
		Padding:      core.Symmetric(8, 10),
		CornerRadius: 8,
		FocusVisible: p.FocusRing(-2),
	}
	if selected {
		rowStyle.Background = p.Accent.Fill
		rowStyle.BorderWidth = 1.5
		rowStyle.BorderColor = p.Accent.Edge
	}

	metaChildren := []core.Node{
		core.Text{
			Content: clockStr,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   10.5,
				Color:      p.TextFaint,
				FlexShrink: 0,
			},
		},
		core.View{
			Style: core.Style{
				Height:         core.Pt(18),
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   5,
				Background:     badgeBg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.Text{
					Content: badgeLabel,
					Style: core.Style{
						FontSize:   10.5,
						FontWeight: 600,
						Color:      badgeFg,
					},
				},
			},
		},
	}
	if entry.Undone {
		metaChildren = append(metaChildren, core.Text{
			Content: "undone",
			Style: core.Style{
				FontSize: 10.5,
				Color:    p.TextFaint,
			},
		})
	}

	bodyChildren := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: metaChildren,
		},
		core.Text{
			TestID:  fmt.Sprintf("history-row-title-%d", entry.ID),
			Content: headline,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: 500,
				Color:      p.TextPrimary,
			},
		},
		core.Text{
			TestID:  fmt.Sprintf("history-row-subtitle-%d", entry.ID),
			Content: subtitle,
			Style: core.Style{
				FontSize: 11.5,
				Color:    p.TextFaint,
			},
		},
	}

	if entry.Kind != HistoryEntrySaved && len(entry.Files) > 0 {
		bases := make([]string, 0, len(entry.Files))
		for _, f := range entry.Files {
			bases = append(bases, filepath.Base(filepath.FromSlash(f)))
		}
		filesLine := strings.Join(bases, " · ")
		if filesLine != "" && filesLine != entry.Detail {
			bodyChildren = append(bodyChildren, core.Text{
				TestID:  fmt.Sprintf("history-row-files-%d", entry.ID),
				Content: filesLine,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   10.5,
					Color:      p.TextMuted,
				},
			})
		}
	}

	entryID := entry.ID
	return core.View{
		TestID:    fmt.Sprintf("history-row-%d", entry.ID),
		Role:      core.RoleButton,
		Label:     headline + " · " + subtitle,
		Focusable: func() bool { return true },
		Style:     rowStyle,
		OnPress: func(core.PressEvent) {
			ed.report(ed.PreviewHistoryEntry(entryID))
		},
		Children: []core.Node{
			historyTimelineRail(p, dotColor, false, hasMore),
			core.View{
				Style: core.Style{
					FlexGrow: 1,
					MinWidth: core.Pt(0),
					Gap:      2,
				},
				Children: bodyChildren,
			},
		},
	}
}

// viewportHistoryPreviewBannerView renders the floating read-only preview banner at the top of the
// viewport while a History entry is being previewed, and binds Escape to exit preview cleanly.
func viewportHistoryPreviewBannerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return !ed.Playing() && ed.HistoryPreviewActive()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
				if ed.HistoryPreviewActive() {
					ed.ExitHistoryPreview()
				}
			})
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				entry, ok := ed.HistoryPreviewEntry()
				if !ok {
					return core.View{}
				}
				p := ed.SpecPalette(cx)
				ago := ed.FormatHistoryRelativeTime(entry.Timestamp)
				return core.View{
					TestID:        "history-preview-banner-host",
					PointerEvents: core.PointerEventsBoxNone,
					Style: core.Style{
						Position:       core.Absolute,
						Top:            core.Pt(12),
						Left:           core.Pt(12),
						Right:          core.Pt(12),
						Direction:      core.Row,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.View{
							TestID: "history-preview-banner",
							Role:   core.RoleStatus,
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          10,
								Height:       core.Pt(40),
								Padding:      core.Edges{Left: 14, Right: 8},
								CornerRadius: 12,
								Background:   p.Accent.Fill,
								BorderWidth:  1,
								BorderColor:  p.Accent.Edge,
							},
							Children: []core.Node{
								ui.Icon(lucide.Eye, 15, func() core.Color { return p.Accent.Fg }),
								core.Text{
									TestID:  "history-preview-banner-text",
									Content: fmt.Sprintf("Previewing %s · editing is off", ago),
									Style: core.Style{
										FontSize:   12.5,
										FontWeight: 500,
										Color:      p.TextPrimary,
									},
								},
								core.View{
									TestID:    "history-viewport-restore",
									Role:      core.RoleButton,
									Label:     ed.T("history.restore"),
									Focusable: func() bool { return true },
									Style: core.Style{
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     p.Accent.Fg,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   p.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										ed.report(ed.RestoreHistoryEntry(entry.ID))
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("history.restore"),
											Style: core.Style{
												FontSize:   12,
												FontWeight: 600,
												Color:      p.Accent.OnFill,
											},
										},
									},
								},
								core.View{
									TestID:    "history-viewport-back",
									Role:      core.RoleButton,
									Label:     ed.T("history.back"),
									Focusable: func() bool { return true },
									Style: core.Style{
										Direction:      core.Row,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										Gap:            6,
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 10),
										CornerRadius:   8,
										Background:     p.LineRaised,
										FocusVisible:   p.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										ed.ExitHistoryPreview()
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("history.back"),
											Style: core.Style{
												FontSize:   12,
												FontWeight: 500,
												Color:      p.TextPrimary,
											},
										},
										core.Text{
											Content: "Esc",
											Style: core.Style{
												FontFamily: core.FontMonospace,
												FontSize:   11,
												Color:      p.TextMuted,
											},
										},
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
