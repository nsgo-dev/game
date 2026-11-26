package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type historyThemeKey struct {
	dark bool
	lang string
}

type historyPreviewBannerKey struct {
	id   uint64
	ago  string
	dark bool
	lang string
}

func (e *Editor) historyListDigest(dark bool, lang string) string {
	filter := e.HistoryFilter()
	entries := e.FilteredHistoryEntries(filter)
	previewEntry, previewing := e.HistoryPreviewEntry()
	dirty := e.HasUnsavedChanges()
	var b strings.Builder
	fmt.Fprintf(&b, "%t|%s|%s|%t|%d|%d|", dark, lang, filter, dirty, previewEntry.ID, len(entries))
	if previewing {
		b.WriteByte('1')
	} else {
		b.WriteByte('0')
	}
	if e.History != nil {
		fmt.Fprintf(&b, "|%d", e.History.Index())
	}
	for _, entry := range entries {
		ago := e.FormatHistoryRelativeTime(entry.Timestamp)
		day := e.FormatHistoryDayLabel(entry.Timestamp)
		fmt.Fprintf(&b, ";%d:%s:%s:%t:%s:%s", entry.ID, entry.Kind, entry.Author, entry.Undone, day, ago)
	}
	return b.String()
}

// HistoryPanelView renders the unified History timeline right-side panel (AskInPlace.dc.html and
// Checkpoints.dc.html) using exclusively SpecPalette tokens:
//   - Header with All / You / Agent filter segmented control and close button
//   - Active point-in-time preview bar ("Previewing … · editing is off") with Restore and Back (Esc)
//   - Top "Now" row showing unsaved/saved status (clicking exits preview)
//   - Newest-first timeline entries grouped by day (TODAY / YESTERDAY / date), with author badges,
//     detail, touched files, relative times, and saved-point markers
//   - Footer explaining non-destructive preview and Restore
//
// Outer panel chrome, header controls, core.Scroll ("history-panel-list"), and footer are mounted
// once and react through narrow memos so unrelated rev bumps never remount the scroll container or
// drop scroll offset / keyboard focus.
func HistoryPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	themeMemo := core.NewMemo(cx, func() historyThemeKey {
		_ = rev.Get()
		return historyThemeKey{
			dark: ed.SpecPalette(cx).Dark,
			lang: ed.ResolvedLanguage(),
		}
	})
	filterMemo := core.NewMemo(cx, func() HistoryFilter {
		_ = rev.Get()
		return ed.HistoryFilter()
	})
	previewMemo := core.NewMemo(cx, func() historyPreviewBannerKey {
		_ = rev.Get()
		tk := themeMemo.Get()
		entry, ok := ed.HistoryPreviewEntry()
		if !ok {
			return historyPreviewBannerKey{dark: tk.dark, lang: tk.lang}
		}
		return historyPreviewBannerKey{
			id:   entry.ID,
			ago:  ed.FormatHistoryRelativeTime(entry.Timestamp),
			dark: tk.dark,
			lang: tk.lang,
		}
	})
	listKeyMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		tk := themeMemo.Get()
		return ed.historyListDigest(tk.dark, tk.lang)
	})

	pal := func() SpecPalette {
		return SpecPaletteFor(themeMemo.Get().dark)
	}

	filterBtn := func(testID string, target HistoryFilter, labelKey string) core.Node {
		return core.View{
			TestID: testID,
			Role:   core.RoleRadio,
			Label:  ed.T(labelKey),
			Checked: func() bool {
				return filterMemo.Get() == target
			},
			Focusable: func() bool { return true },
			StyleFunc: func() core.Style {
				p := pal()
				bg := core.Transparent
				if filterMemo.Get() == target {
					bg = p.LineRaised
				}
				return core.Style{
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					Background:     bg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				}
			},
			OnPress: func(core.PressEvent) {
				ed.SetHistoryFilter(target)
			},
			Children: []core.Node{
				core.Text{
					ContentFunc: func() string {
						_ = themeMemo.Get()
						return ed.T(labelKey)
					},
					StyleFunc: func() core.Style {
						p := pal()
						fg := p.TextFaint
						if filterMemo.Get() == target {
							fg = p.TextPrimary
						}
						return core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      fg,
						}
					},
				},
			},
		}
	}

	header := core.View{
		TestID: "history-panel-header",
		StyleFunc: func() core.Style {
			p := pal()
			return core.Style{
				Height:         core.Pt(44),
				FlexShrink:     0,
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
				Padding:        core.Edges{Left: 16, Right: 10},
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    p.Line,
			}
		},
		Children: []core.Node{
			core.Text{
				TestID: "history-panel-title",
				ContentFunc: func() string {
					_ = themeMemo.Get()
					return ed.T("history.title")
				},
				StyleFunc: func() core.Style {
					p := pal()
					return core.Style{
						FontSize:   14,
						FontWeight: 600,
						Color:      p.TextPrimary,
						FlexShrink: 0,
					}
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
						StyleFunc: func() core.Style {
							p := pal()
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          2,
								Padding:      core.All(2),
								CornerRadius: 8,
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							}
						},
						Children: []core.Node{
							filterBtn("history-filter-all", HistoryFilterAll, "history.filter.all"),
							filterBtn("history-filter-you", HistoryFilterYou, "history.filter.you"),
							filterBtn("history-filter-agent", HistoryFilterAgent, "history.filter.agent"),
						},
					},
					core.View{
						TestID:    "history-panel-close",
						Role:      core.RoleButton,
						Label:     ed.T("history.close"),
						Focusable: func() bool { return true },
						StyleFunc: func() core.Style {
							p := pal()
							return core.Style{
								Width:          core.Pt(28),
								Height:         core.Pt(28),
								CornerRadius:   6,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							}
						},
						OnPress: func(core.PressEvent) {
							ed.SetHistoryPanelOpen(false)
						},
						Children: []core.Node{
							ui.Icon(lucide.X, 14, func() core.Color { return pal().TextMuted }),
						},
					},
				},
			},
		},
	}

	previewBar := core.Show{
		When: func() bool {
			return previewMemo.Get().id != 0
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "history-panel-preview-bar",
				Role:   core.RoleStatus,
				StyleFunc: func() core.Style {
					p := pal()
					return core.Style{
						FlexShrink:  0,
						Padding:     core.Symmetric(8, 12),
						Gap:         8,
						Background:  p.Accent.Fill,
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.Accent.Edge,
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
							ui.Icon(lucide.Eye, 15, func() core.Color { return pal().Accent.Fg }),
							core.Text{
								TestID: "history-panel-preview-label",
								ContentFunc: func() string {
									pk := previewMemo.Get()
									return fmt.Sprintf(ed.T("history.previewing"), pk.ago)
								},
								StyleFunc: func() core.Style {
									p := pal()
									return core.Style{
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextPrimary,
										FlexShrink: 1,
										MinWidth:   core.Pt(0),
									}
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
								StyleFunc: func() core.Style {
									p := pal()
									return core.Style{
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     p.Accent.Fg,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   p.FocusRing(2),
									}
								},
								OnPress: func(core.PressEvent) {
									if entry, ok := ed.HistoryPreviewEntry(); ok {
										ed.report(ed.RestoreHistoryEntry(entry.ID))
									}
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = themeMemo.Get()
											return ed.T("history.restore")
										},
										StyleFunc: func() core.Style {
											p := pal()
											return core.Style{
												FontSize:   12,
												FontWeight: 600,
												Color:      p.Accent.OnFill,
											}
										},
									},
								},
							},
							core.View{
								TestID:    "history-preview-back",
								Role:      core.RoleButton,
								Label:     ed.T("history.back"),
								Focusable: func() bool { return true },
								StyleFunc: func() core.Style {
									p := pal()
									return core.Style{
										Direction:      core.Row,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										Gap:            6,
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     p.LineRaised,
										FocusVisible:   p.FocusRing(2),
									}
								},
								OnPress: func(core.PressEvent) {
									ed.ExitHistoryPreview()
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = themeMemo.Get()
											return ed.T("history.back")
										},
										StyleFunc: func() core.Style {
											p := pal()
											return core.Style{
												FontSize:   12,
												FontWeight: 500,
												Color:      p.TextPrimary,
											}
										},
									},
									core.Text{
										Content: "Esc",
										StyleFunc: func() core.Style {
											p := pal()
											return core.Style{
												FontFamily: core.FontMonospace,
												FontSize:   11,
												Color:      p.TextMuted,
											}
										},
									},
								},
							},
						},
					},
				},
			}
		},
	}

	scrollList := core.Scroll{
		TestID: "history-panel-list",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.All(8),
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = listKeyMemo.Get()
				p := pal()
				filter := ed.HistoryFilter()
				entries := ed.FilteredHistoryEntries(filter)
				previewEntry, previewing := ed.HistoryPreviewEntry()
				dirty := ed.HasUnsavedChanges()

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

				return core.View{
					Style:    core.Style{Gap: 4},
					Children: listRows,
				}
			}),
		},
	}

	footer := core.View{
		TestID: "history-panel-footer",
		StyleFunc: func() core.Style {
			p := pal()
			return core.Style{
				FlexShrink:  0,
				Padding:     core.Symmetric(10, 14),
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
			}
		},
		Children: []core.Node{
			core.Text{
				ContentFunc: func() string {
					_ = themeMemo.Get()
					return ed.T("history.footer")
				},
				StyleFunc: func() core.Style {
					p := pal()
					return core.Style{
						FontSize: 11.5,
						Color:    p.TextFaint,
					}
				},
			},
		},
	}

	return core.View{
		TestID: "history-panel",
		StyleFunc: func() core.Style {
			p := pal()
			return core.Style{
				Width:       core.Pt(340),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
			}
		},
		Children: []core.Node{
			header,
			previewBar,
			scrollList,
			footer,
		},
	}
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
			Content: ed.T("history.undone"),
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
// Uses a narrow memo so unrelated rev bumps never remount the banner or its action buttons.
func viewportHistoryPreviewBannerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	showMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return !ed.Playing() && ed.HistoryPreviewActive()
	})
	return core.Show{
		When: showMemo.Get,
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
				if ed.HistoryPreviewActive() {
					ed.ExitHistoryPreview()
				}
			})
			bannerMemo := core.NewMemo(cx, func() historyPreviewBannerKey {
				_ = rev.Get()
				dark := ed.SpecPalette(cx).Dark
				lang := ed.ResolvedLanguage()
				entry, ok := ed.HistoryPreviewEntry()
				if !ok {
					return historyPreviewBannerKey{dark: dark, lang: lang}
				}
				return historyPreviewBannerKey{
					id:   entry.ID,
					ago:  ed.FormatHistoryRelativeTime(entry.Timestamp),
					dark: dark,
					lang: lang,
				}
			})
			pal := func() SpecPalette {
				return SpecPaletteFor(bannerMemo.Get().dark)
			}
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
						StyleFunc: func() core.Style {
							p := pal()
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          10,
								Height:       core.Pt(40),
								Padding:      core.Edges{Left: 14, Right: 8},
								CornerRadius: 12,
								Background:   p.Accent.Fill,
								BorderWidth:  1,
								BorderColor:  p.Accent.Edge,
							}
						},
						Children: []core.Node{
							ui.Icon(lucide.Eye, 15, func() core.Color { return pal().Accent.Fg }),
							core.Text{
								TestID: "history-preview-banner-text",
								ContentFunc: func() string {
									bk := bannerMemo.Get()
									return fmt.Sprintf(ed.T("history.previewing"), bk.ago)
								},
								StyleFunc: func() core.Style {
									p := pal()
									return core.Style{
										FontSize:   12.5,
										FontWeight: 500,
										Color:      p.TextPrimary,
									}
								},
							},
							core.View{
								TestID:    "history-viewport-restore",
								Role:      core.RoleButton,
								Label:     ed.T("history.restore"),
								Focusable: func() bool { return true },
								StyleFunc: func() core.Style {
									p := pal()
									return core.Style{
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     p.Accent.Fg,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   p.FocusRing(2),
									}
								},
								OnPress: func(core.PressEvent) {
									if entry, ok := ed.HistoryPreviewEntry(); ok {
										ed.report(ed.RestoreHistoryEntry(entry.ID))
									}
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = bannerMemo.Get()
											return ed.T("history.restore")
										},
										StyleFunc: func() core.Style {
											p := pal()
											return core.Style{
												FontSize:   12,
												FontWeight: 600,
												Color:      p.Accent.OnFill,
											}
										},
									},
								},
							},
							core.View{
								TestID:    "history-viewport-back",
								Role:      core.RoleButton,
								Label:     ed.T("history.back"),
								Focusable: func() bool { return true },
								StyleFunc: func() core.Style {
									p := pal()
									return core.Style{
										Direction:      core.Row,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										Gap:            6,
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 10),
										CornerRadius:   8,
										Background:     p.LineRaised,
										FocusVisible:   p.FocusRing(2),
									}
								},
								OnPress: func(core.PressEvent) {
									ed.ExitHistoryPreview()
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = bannerMemo.Get()
											return ed.T("history.back")
										},
										StyleFunc: func() core.Style {
											p := pal()
											return core.Style{
												FontSize:   12,
												FontWeight: 500,
												Color:      p.TextPrimary,
											}
										},
									},
									core.Text{
										Content: "Esc",
										StyleFunc: func() core.Style {
											p := pal()
											return core.Style{
												FontFamily: core.FontMonospace,
												FontSize:   11,
												Color:      p.TextMuted,
											}
										},
									},
								},
							},
						},
					},
				},
			}
		},
	}
}
