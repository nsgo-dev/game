package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/playtest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints strictly from Spec.dc.html color tokens (never introducing off-spec hex values).
var (
	specPlaytestMissedFill   = core.RGBA(0xFF6B6B, 0.08)
	specPlaytestHeatRedHigh  = core.RGBA(0xFF6B6B, 0.56)
	specPlaytestHeatRedMid   = core.RGBA(0xFF6B6B, 0.28)
	specPlaytestHeatRedSoft  = core.RGBA(0xFF6B6B, 0.12)
	specPlaytestHeatAmberHi  = core.RGBA(0xE9A84A, 0.48)
	specPlaytestHeatAmberMid = core.RGBA(0xE9A84A, 0.22)
	specPlaytestHeatAmberLow = core.RGBA(0xE9A84A, 0.10)
	specPlaytestLineArea     = core.RGBA(0x3B7BFF, 0.14)
	specPlaytestGridLine     = core.RGBA(0x8B92A0, 0.16)
	specPlaytestAgentBanner  = core.RGBA(0x9B7BFF, 0.10)
	specPlaytestAgentBorder  = core.RGBA(0x9B7BFF, 0.50)
)

// PlaytestReportView renders the full-window Playtest Report screen matching docs/design/editor/Playtest.dc.html
// and strictly using docs/design/editor/Spec.dc.html color, spacing, radius, type, and WCAG contrast tokens
// (including #101216 text on #3B7BFF primary accent buttons).
func PlaytestReportView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if !ed.PlaytestReportOpen() {
			return
		}
		if s := ed.PlaytestSession(); s != nil {
			if s.ReviewProposalID != "" {
				ed.ClosePlaytestProposalReview()
				return
			}
			if s.ConfigPreviewOpen {
				ed.ClosePlaytestConfigPreview()
				return
			}
		}
		ed.ClosePlaytestReport()
	})

	ptRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.playtestStateRev()
	})

	return core.View{
		TestID: "playtest-report-screen",
		Role:   core.RoleRegion,
		Label:  "Playtest report",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = ptRev.Get()
				sess := ed.PlaytestSession()
				entry := sess.ActiveEntry()
				if entry == nil || entry.Report == nil {
					return playtestEmptyStateView(ed)
				}
				return core.View{
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Width:     core.Pct(100),
						Height:    core.Pct(100),
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						playtestHeaderView(ed, sess, entry),
						playtestBotProfileView(ed, sess, entry),
						playtestDrawersView(ed, sess),
						playtestBodyView(ed, sess, entry),
					},
				}
			}),
		},
	}
}

func playtestEmptyStateView(ed *Editor) core.Node {
	return core.View{
		TestID: "playtest-empty-state",
		Style: core.Style{
			FlexGrow:       1,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            12,
			Padding:        core.All(24),
			Background:     specBg,
		},
		Children: []core.Node{
			core.Text{
				Content: "No playtest report loaded",
				Style:   core.Style{FontSize: 16, FontWeight: 600, Color: specTextStrong},
			},
			core.Text{
				Content: "Run a headless playtest via game_playtest or open playtest.report.yaml from the project file browser.",
				Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
			},
			ui.Button{
				TestID:  "playtest-empty-back",
				Text:    "Back to scene editor",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specRaised },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      specText,
				},
				Style: core.Style{
					Height:       core.Pt(32),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 14),
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.ClosePlaytestReport()
				},
			},
		},
	}
}

func playtestHeaderView(ed *Editor, sess *PlaytestReportSession, entry *PlaytestReportEntry) core.Node {
	rep := entry.Report
	scope := rep.Scope.Scene
	if scope == "" {
		scope = "scene"
	}
	subParts := []string{}
	if rep.Scope.Project != "" {
		subParts = append(subParts, rep.Scope.Project)
	}
	if entry.RunsSummary != "" {
		subParts = append(subParts, entry.RunsSummary)
	}
	subtitle := strings.Join(subParts, " · ")

	watchRun, hasRun := findPlaytestRun(rep, entry.WatchRunIndex)
	hasRecording := hasRun && strings.TrimSpace(watchRun.Recording) != ""

	return core.View{
		TestID: "playtest-header",
		Role:   core.RoleRegion,
		Label:  "Playtest report header",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Height:      core.Pt(52),
			FlexShrink:  0,
			Padding:     core.Symmetric(0, 16),
			Gap:         12,
			Background:  specBg,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			ui.Button{
				TestID:  "playtest-back",
				Icon:    lucide.ArrowLeft,
				Label:   "Back to scene editor",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specRaised },
				Tint:    func() core.Color { return specTextSecondary },
				Style: core.Style{
					Width:        core.Pt(30),
					Height:       core.Pt(30),
					CornerRadius: 8,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.ClosePlaytestReport()
				},
			},
			core.Text{
				TestID:  "playtest-title",
				Content: "Playtest · " + scope,
				Style: core.Style{
					FontSize:   14,
					FontWeight: 600,
					Color:      specTextStrong,
				},
			},
			core.Text{
				TestID:  "playtest-subtitle",
				Content: subtitle,
				Style: core.Style{
					FontSize: 12.5,
					Color:    specTextMuted,
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:   "playtest-watch-run",
				Text:     entry.WatchLabel,
				Label:    entry.WatchLabel + " in Play mode",
				Variant:  ui.ButtonGhost,
				Size:     ui.SizeSmall,
				Disabled: func() bool { return !hasRecording },
				Color:    func() core.Color { return specRaised },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      specText,
				},
				Style: core.Style{
					Height:       core.Pt(32),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 12),
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.report(ed.WatchPlaytestRun(entry.WatchRunIndex))
				},
			},
			ui.Button{
				TestID:  "playtest-ask-tune",
				Text:    "Ask the agent to tune it",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specAccent },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 600,
					Color:      specBg, // Spec.dc.html contrast table: #101216 on #3B7BFF (4.88:1 AA), never #FFFFFF on accent.
				},
				Style: core.Style{
					Height:       core.Pt(32),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 16),
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.AskAgentToTunePlaytest()
				},
			},
		},
	}
}

func playtestBotProfileView(ed *Editor, sess *PlaytestReportSession, entry *PlaytestReportEntry) core.Node {
	rep := entry.Report
	configFile := entry.ConfigPath
	if configFile == "" {
		configFile = playtest.FileName
	}
	templateText := ""
	if strings.TrimSpace(rep.Scope.Template) != "" {
		templateText = "from template " + rep.Scope.Template
	}
	strategy := BotProfileDefaultStrategy(rep.Bot)

	// Segmented profile group: shows all entries in the session (or the active profile cleanly when a single report is loaded).
	profileButtons := make([]core.Node, 0, len(sess.Entries))
	for i, ent := range sess.Entries {
		idx := i
		active := i == sess.ActiveIndex
		label := BotProfileDisplayName(ent.Report.Bot.Profile)
		bg := core.Transparent
		fg := specTextSecondary
		if active {
			bg = specLineRaised
			fg = specTextStrong
		}
		profileButtons = append(profileButtons, ui.Button{
			TestID:  fmt.Sprintf("playtest-profile-%d", idx),
			Text:    label,
			Label:   "Bot profile " + label,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return bg },
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      fg,
			},
			Style: core.Style{
				Height:       core.Pt(28),
				CornerRadius: 6,
				Padding:      core.Symmetric(0, 12),
				FocusVisible: specFocusRingStyle,
			},
			OnPress: func() {
				ed.SelectPlaytestReportIndex(idx)
			},
		})
	}

	topRowChildren := []core.Node{
		core.Text{
			TestID:  "playtest-bot-profile-label",
			Content: "BOT PROFILE",
			Style: core.Style{
				FontSize:      11,
				FontWeight:    600,
				LetterSpacing: 0.66,
				Color:         specTextMuted,
			},
		},
		core.View{
			TestID: "playtest-profile-group",
			Role:   core.RoleGroup,
			Label:  "Bot profile",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          2,
				Padding:      core.All(3),
				CornerRadius: 8,
				Background:   specRaised,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
			},
			Children: profileButtons,
		},
		core.View{Style: core.Style{FlexGrow: 1}},
		core.Text{
			TestID:  "playtest-config-file",
			Content: configFile,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      specTextSecondary,
			},
		},
	}
	if templateText != "" {
		topRowChildren = append(topRowChildren, core.Text{
			TestID:  "playtest-config-template",
			Content: templateText,
			Style: core.Style{
				FontSize: 12,
				Color:    specTextMuted,
			},
		})
	}
	topRowChildren = append(topRowChildren, ui.Button{
		TestID:  "playtest-open-config",
		Text:    "Open",
		Label:   "Open " + configFile,
		Variant: ui.ButtonGhost,
		Size:    ui.SizeSmall,
		Color:   func() core.Color { return specRaised },
		TextStyle: core.Style{
			FontSize:   12,
			FontWeight: 500,
			Color:      specAccent, // Spec contrast: #3B7BFF on #171A21 is 4.53:1 AA.
		},
		Style: core.Style{
			Height:       core.Pt(28),
			CornerRadius: 6,
			Padding:      core.Symmetric(0, 10),
			BorderWidth:  1,
			BorderColor:  specLineRaised,
			FocusVisible: specFocusRingStyle,
		},
		OnPress: func() {
			ed.report(ed.OpenPlaytestConfigPreview())
		},
	})

	strategyRowChildren := []core.Node{
		core.Text{
			Content: "Strategy",
			Style: core.Style{
				FontSize: 12.5,
				Color:    specTextMuted,
			},
		},
		core.Text{
			TestID:   "playtest-bot-strategy",
			Content:  strategy,
			MaxLines: 1,
			Style: core.Style{
				FontSize:   12.5,
				Color:      specTextSecondary,
				FlexShrink: 1,
			},
		},
		core.View{Style: core.Style{FlexGrow: 1}},
	}

	if prop := sess.FirstPendingProposal(); prop != nil {
		propID := prop.ID
		strategyRowChildren = append(strategyRowChildren, core.View{
			TestID: "playtest-proposal-banner",
			Role:   core.RoleRegion,
			Label:  "Proposed playtest metric",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(28),
				FlexShrink:   0,
				Gap:          8,
				Padding:      core.Edges{Left: 12, Right: 4},
				CornerRadius: 8,
				Background:   specPlaytestAgentBanner,
				BorderWidth:  1,
				BorderColor:  specPlaytestAgentBorder,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 3,
						Background:   specAgent,
					},
				},
				core.Text{
					TestID:  "playtest-proposal-text",
					Content: prop.BannerText(),
					Style: core.Style{
						FontSize: 12,
						Color:    specText,
					},
				},
				ui.Button{
					TestID:  "playtest-proposal-review",
					Text:    "Review",
					Label:   "Review proposed metric " + prop.MetricName,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specRaised },
					TextStyle: core.Style{
						FontSize:   11.5,
						FontWeight: 500,
						Color:      specText,
					},
					Style: core.Style{
						Height:       core.Pt(22),
						MinWidth:     core.Pt(28),
						CornerRadius: 6,
						Padding:      core.Symmetric(0, 8),
						BorderWidth:  1,
						BorderColor:  specLineRaised,
						FocusVisible: specFocusRingStyle,
					},
					OnPress: func() {
						ed.OpenPlaytestProposalReview(propID)
					},
				},
			},
		})
	}

	return core.View{
		TestID: "playtest-bot-profile",
		Role:   core.RoleRegion,
		Label:  "Bot profile",
		Style: core.Style{
			FlexShrink:  0,
			Padding:     core.Symmetric(12, 16),
			Gap:         8,
			Background:  specPanel,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        12,
				},
				Children: topRowChildren,
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					MinHeight:  core.Pt(28),
					Gap:        12,
				},
				Children: strategyRowChildren,
			},
		},
	}
}

func playtestDrawersView(ed *Editor, sess *PlaytestReportSession) core.Node {
	if sess.ReviewProposalID != "" {
		if prop := sess.ProposalByID(sess.ReviewProposalID); prop != nil {
			propID := prop.ID
			diffLines := strings.Split(strings.TrimSuffix(prop.Diff, "\n"), "\n")
			diffNodes := make([]core.Node, 0, len(diffLines))
			for _, line := range diffLines {
				col := specTextSecondary
				bg := core.Transparent
				switch {
				case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
					col = specAdded
					bg = specAddedFill
				case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
					col = specProblem
					bg = specPlaytestMissedFill
				case strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
					col = specTextMuted
				}
				diffNodes = append(diffNodes, core.View{
					Style: core.Style{
						Padding:    core.Symmetric(1, 8),
						Background: bg,
					},
					Children: []core.Node{
						core.Text{
							Content: line,
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   11.5,
								Color:      col,
							},
						},
					},
				})
			}
			return core.View{
				TestID: "playtest-proposal-drawer",
				Role:   core.RoleRegion,
				Label:  "Review proposed playtest metric",
				Style: core.Style{
					FlexShrink:  0,
					Padding:     core.Symmetric(12, 16),
					Gap:         8,
					Background:  specRaised,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: specPlaytestAgentBorder,
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
									Background:   specAgent,
								},
							},
							core.Text{
								Content: fmt.Sprintf("Review playtest_define · %s (%s)", prop.MetricName, prop.File),
								Style:   core.Style{FontSize: 13, FontWeight: 600, Color: specTextStrong},
							},
							core.View{Style: core.Style{FlexGrow: 1}},
							ui.Button{
								TestID:  "playtest-proposal-dismiss",
								Text:    "Dismiss",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return specLineRaised },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      specText,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 6,
									Padding:      core.Symmetric(0, 10),
									FocusVisible: specFocusRingStyle,
								},
								OnPress: func() {
									ed.report(ed.DismissPlaytestProposal(propID))
								},
							},
							ui.Button{
								TestID:  "playtest-proposal-apply",
								Text:    "Apply to " + prop.File,
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return specAccent },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      specBg,
								},
								Style: core.Style{
									Height:       core.Pt(28),
									CornerRadius: 6,
									Padding:      core.Symmetric(0, 12),
									FocusVisible: specFocusRingStyle,
								},
								OnPress: func() {
									ed.report(ed.ApplyPlaytestProposal(propID))
								},
							},
							ui.Button{
								TestID:  "playtest-proposal-close",
								Icon:    lucide.X,
								Label:   "Close review",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return specPanel },
								Tint:    func() core.Color { return specTextSecondary },
								Style: core.Style{
									Width:        core.Pt(28),
									Height:       core.Pt(28),
									CornerRadius: 6,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
									FocusVisible: specFocusRingStyle,
								},
								OnPress: func() {
									ed.ClosePlaytestProposalReview()
								},
							},
						},
					},
					core.View{
						Style: core.Style{
							MaxHeight:    core.Pt(140),
							CornerRadius: 8,
							Background:   specBg,
							BorderWidth:  1,
							BorderColor:  specLine,
							Padding:      core.Symmetric(6, 0),
						},
						Children: []core.Node{
							ui.ScrollView{
								Style:    core.Style{MaxHeight: core.Pt(128)},
								Children: diffNodes,
							},
						},
					},
				},
			}
		}
	}

	if sess.ConfigPreviewOpen {
		body := sess.ConfigPreviewText
		col := specText
		if sess.ConfigPreviewErr != "" {
			body = sess.ConfigPreviewErr
			col = specProblem
		}
		return core.View{
			TestID: "playtest-config-drawer",
			Role:   core.RoleRegion,
			Label:  "playtest.yaml preview",
			Style: core.Style{
				FlexShrink:  0,
				Padding:     core.Symmetric(12, 16),
				Gap:         8,
				Background:  specRaised,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: specLineRaised,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
					},
					Children: []core.Node{
						core.Text{
							Content: sess.ConfigPreviewPath,
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   12,
								FontWeight: 600,
								Color:      specTextStrong,
							},
						},
						core.View{Style: core.Style{FlexGrow: 1}},
						ui.Button{
							TestID:  "playtest-config-close",
							Text:    "Close",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return specLineRaised },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      specText,
							},
							Style: core.Style{
								Height:       core.Pt(28),
								CornerRadius: 6,
								Padding:      core.Symmetric(0, 10),
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								ed.ClosePlaytestConfigPreview()
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						MaxHeight:    core.Pt(150),
						CornerRadius: 8,
						Background:   specBg,
						BorderWidth:  1,
						BorderColor:  specLine,
						Padding:      core.All(10),
					},
					Children: []core.Node{
						ui.ScrollView{
							Style: core.Style{MaxHeight: core.Pt(130)},
							Children: []core.Node{
								core.Text{
									TestID:  "playtest-config-content",
									Content: body,
									Style: core.Style{
										FontFamily: core.FontMonospace,
										FontSize:   11.5,
										Color:      col,
									},
								},
							},
						},
					},
				},
			},
		}
	}

	return core.View{Style: core.Style{Height: core.Pt(0)}}
}

func playtestBodyView(ed *Editor, sess *PlaytestReportSession, entry *PlaytestReportEntry) core.Node {
	return core.View{
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.All(16),
			Gap:       16,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			playtestMetricCardsRowView(entry.Cards),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Gap:       16,
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					playtestChartCardView(ed, entry.Chart),
					playtestAgentReadCardView(ed, sess, entry),
				},
			},
		},
	}
}

func playtestMetricCardsRowView(cards []PlaytestMetricCard) core.Node {
	displayCards := cards
	if len(displayCards) > 4 {
		displayCards = displayCards[:4]
	}
	nodes := make([]core.Node, 0, len(displayCards))
	for _, c := range displayCards {
		nodes = append(nodes, playtestMetricCardView(c))
	}
	return core.View{
		TestID: "playtest-metric-cards",
		Role:   core.RoleRegion,
		Label:  "Playtest metrics",
		Style: core.Style{
			Direction:  core.Row,
			Height:     core.Pt(132),
			FlexShrink: 0,
			Gap:        16,
		},
		Children: nodes,
	}
}

func playtestMetricCardView(c PlaytestMetricCard) core.Node {
	badgeBg := core.Transparent
	badgeBorder := specLineRaised
	badgeFg := specTextSecondary
	var badgeIcon *core.SVG
	if c.Met {
		badgeFg = specAdded
		badgeBorder = specAddedEdge
		badgeIcon = lucide.Check()
	} else if c.Missed {
		badgeFg = specProblem
		badgeBg = specPlaytestMissedFill
		badgeBorder = specProblemEdge
		badgeIcon = lucide.X()
	}

	badgeChildren := []core.Node{}
	if badgeIcon != nil {
		badgeChildren = append(badgeChildren, core.Icon{
			SVG: badgeIcon,
			Style: core.Style{
				Width:  core.Pt(11),
				Height: core.Pt(11),
				Color:  badgeFg,
			},
		})
	}
	badgeChildren = append(badgeChildren, core.Text{
		TestID:  "playtest-metric-badge-text-" + c.Name,
		Content: c.Badge,
		Style: core.Style{
			FontSize:   11,
			FontWeight: 600,
			Color:      badgeFg,
		},
	})

	return core.View{
		TestID: "playtest-metric-" + c.Name,
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   specPanel,
			BorderWidth:  1,
			BorderColor:  specLine,
			Padding:      core.All(16),
			Gap:          4,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.Text{
						Content:  c.Label,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow: 1,
							FontSize: 12.5,
							Color:    specTextMuted,
						},
					},
					core.View{
						TestID: "playtest-metric-badge-" + c.Name,
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Height:       core.Pt(20),
							FlexShrink:   0,
							Gap:          4,
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 10,
							Background:   badgeBg,
							BorderWidth:  1,
							BorderColor:  badgeBorder,
						},
						Children: badgeChildren,
					},
				},
			},
			core.Text{
				TestID:  "playtest-metric-value-" + c.Name,
				Content: c.ValueText,
				Style: core.Style{
					FontSize:   28,
					FontWeight: 600,
					Color:      specTextStrong,
				},
			},
			core.Text{
				TestID:   "playtest-metric-foot-" + c.Name,
				Content:  c.Foot,
				MaxLines: 1,
				Style: core.Style{
					FontSize: 12,
					Color:    specTextMuted,
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.Text{
						TestID:   "playtest-metric-goal-" + c.Name,
						Content:  c.GoalText,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							FontSize:   11.5,
							Color:      specTextSecondary,
						},
					},
					core.Text{
						TestID:  "playtest-metric-source-" + c.Name,
						Content: c.Source,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							FlexShrink: 0,
							Color:      specTextMuted,
						},
					},
				},
			},
		},
	}
}

func playtestChartCardView(ed *Editor, spec PlaytestChartSpec) core.Node {
	doc := ed.Doc
	return core.View{
		TestID: "playtest-chart-card",
		Role:   core.RoleRegion,
		Label:  spec.Title,
		Style: core.Style{
			FlexGrow:     3,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   specPanel,
			BorderWidth:  1,
			BorderColor:  specLine,
			Padding:      core.All(16),
			Gap:          12,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					FlexShrink:     0,
					Gap:            12,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "playtest-chart-title",
						Content: spec.Title,
						Style: core.Style{
							FontSize:   13,
							FontWeight: 600,
							Color:      specTextStrong,
						},
					},
					core.Text{
						TestID:  "playtest-chart-subtitle",
						Content: spec.Subtitle,
						Style: core.Style{
							FontSize: 12,
							Color:    specTextMuted,
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					CornerRadius: 8,
					Background:   specBg,
					BorderWidth:  1,
					BorderColor:  specLine,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "playtest-chart-canvas",
						Style: core.Style{
							Width:  core.Pct(100),
							Height: core.Pct(100),
						},
						Draw: func(d *core.Drawing, size core.Size) {
							DrawPlaytestChart(d, size, spec, doc)
						},
					},
				},
			},
		},
	}
}

func playtestAgentReadCardView(ed *Editor, sess *PlaytestReportSession, entry *PlaytestReportEntry) core.Node {
	suggestionNodes := make([]core.Node, 0, len(entry.Suggestions))
	for i, sg := range entry.Suggestions {
		idx := i
		borderCol := specLineRaised
		bgCol := specRaised
		statusText := "Ask agent"
		statusCol := specTextMuted
		if sg.Requested {
			borderCol = specPlaytestAgentBorder
			bgCol = specPlaytestAgentBanner
			statusText = "Review requested"
			statusCol = specAgent
		}
		suggestionNodes = append(suggestionNodes, core.View{
			TestID: fmt.Sprintf("playtest-suggestion-%d", idx),
			Role:   core.RoleButton,
			Label:  "Request suggestion: " + sg.Text,
			Style: core.Style{
				Padding:      core.Symmetric(8, 12),
				Gap:          4,
				CornerRadius: 8,
				Background:   bgCol,
				BorderWidth:  1,
				BorderColor:  borderCol,
				Hover:        &core.Style{BorderColor: specAccent},
				FocusVisible: specFocusRingStyle,
			},
			OnPress: func(core.PressEvent) {
				_, err := ed.RequestPlaytestSuggestion(idx)
				ed.report(err)
			},
			Children: []core.Node{
				core.Text{
					Content: sg.Text,
					Style: core.Style{
						FontSize: 12.5,
						Color:    specText,
					},
				},
				core.Text{
					Content: statusText,
					Style: core.Style{
						FontSize:   11,
						FontWeight: 600,
						Color:      statusCol,
					},
				},
			},
		})
	}

	children := []core.Node{
		core.Text{
			TestID:  "playtest-agent-read-title",
			Content: "Agent's read",
			Style: core.Style{
				FontSize:   13,
				FontWeight: 600,
				Color:      specTextStrong,
			},
		},
		core.Text{
			TestID:  "playtest-agent-read-text",
			Content: entry.AgentRead,
			Style: core.Style{
				FontSize: 12.5,
				Color:    specText,
			},
		},
		core.View{
			TestID:   "playtest-suggestions",
			Style:    core.Style{Gap: 8},
			Children: suggestionNodes,
		},
	}

	if sess.Notice != "" {
		children = append(children, core.View{
			TestID: "playtest-notice",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Padding:      core.Symmetric(6, 10),
				CornerRadius: 8,
				Background:   specPlaytestAgentBanner,
				BorderWidth:  1,
				BorderColor:  specPlaytestAgentBorder,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 3,
						Background:   specAgent,
					},
				},
				core.Text{
					Content: sess.Notice,
					Style: core.Style{
						FontSize:   11.5,
						Color:      specText,
						FlexShrink: 1,
					},
				},
			},
		})
	}

	children = append(children,
		core.View{Style: core.Style{FlexGrow: 1}},
		core.Text{
			TestID:  "playtest-footnote",
			Content: "Runs use the replay system, so any run can be watched frame by frame or turned into a test. Metrics and the bot come from playtest.yaml; the agent proposes new ones with playtest_define and they arrive as a review.",
			Style: core.Style{
				FontSize: 11.5,
				Color:    specTextMuted,
			},
		},
	)

	return core.View{
		TestID: "playtest-agent-read-card",
		Role:   core.RoleRegion,
		Label:  "Agent's read",
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   specPanel,
			BorderWidth:  1,
			BorderColor:  specLine,
			Padding:      core.All(16),
			Gap:          12,
			Overflow:     core.Hidden,
		},
		Children: children,
	}
}

// DrawPlaytestChart renders spec onto d using exclusively Spec.dc.html color tokens.
// Exported so tests can verify all 4 chart kinds directly against core.Drawing as well as through EditorView.
func DrawPlaytestChart(d *core.Drawing, size core.Size, spec PlaytestChartSpec, scene *sceneformat.SceneDoc) {
	if d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	w := float32(size.Width)
	h := float32(size.Height)

	d.Save()
	d.SetFillColor(specBg).FillRect(0, 0, w, h)

	switch spec.Kind {
	case PlaytestChartHeatMap:
		drawPlaytestHeatMapChart(d, w, h, spec.HeatMap, scene)
	case PlaytestChartBars:
		drawPlaytestBarChart(d, w, h, spec.Bars)
	case PlaytestChartLine:
		drawPlaytestLineChart(d, w, h, spec.Line)
	case PlaytestChartMatrix:
		drawPlaytestMatrixChart(d, w, h, spec.Matrix)
	default:
		d.SetFillColor(specTextMuted).FillText("No chart data available", 16, h/2, core.Font{Size: 12})
	}
	d.Restore()
}

func drawPlaytestHeatMapChart(d *core.Drawing, w, h float32, chart *PlaytestHeatChart, scene *sceneformat.SceneDoc) {
	if chart == nil || len(chart.Points) == 0 {
		d.SetFillColor(specTextMuted).FillText("No positioned events recorded for heat map", 16, h/2, core.Font{Size: 12})
		return
	}

	pad := float32(16)
	plotX := pad
	plotY := pad
	plotW := max(float32(32), w-pad*2)
	plotH := max(float32(32), h-pad*2)

	// Subtle spatial grid lines in Spec tokens.
	d.Save()
	d.SetStrokeColor(specPlaytestGridLine).SetLineWidth(1)
	cols, rows := 12, 6
	for i := 1; i < cols; i++ {
		gx := plotX + plotW*float32(i)/float32(cols)
		d.BeginPath().MoveTo(gx, plotY).LineTo(gx, plotY+plotH).Stroke()
	}
	for j := 1; j < rows; j++ {
		gy := plotY + plotH*float32(j)/float32(rows)
		d.BeginPath().MoveTo(plotX, gy).LineTo(plotX+plotW, gy).Stroke()
	}
	d.Restore()

	// World-to-canvas projection (guarding non-finite or inverted bounds).
	minX, maxX := chart.MinX, chart.MaxX
	minY, maxY := chart.MinY, chart.MaxY
	if !isFinite64(minX) || !isFinite64(maxX) || maxX <= minX {
		minX, maxX = 0, 640
	}
	if !isFinite64(minY) || !isFinite64(maxY) || maxY <= minY {
		minY, maxY = 0, 360
	}
	spanX := max(1.0, maxX-minX)
	spanY := max(1.0, maxY-minY)
	toX := func(wx float64) float32 {
		return plotX + float32((wx-minX)/spanX)*plotW
	}
	toY := func(wy float64) float32 {
		return plotY + float32((wy-minY)/spanY)*plotH
	}

	// Ground / level structural backdrop so spatial clusters read over the level silhouette.
	if chart.IsTimePerTile {
		// Top-down corridors and rooms silhouette using Spec raised/line tokens.
		d.SetFillColor(specRaised).BeginPath().RoundRect(plotX+plotW*0.06, plotY+plotH*0.42, plotW*0.72, 16, 8).Fill()
		d.SetFillColor(specRaised).BeginPath().RoundRect(plotX+plotW*0.38, plotY+plotH*0.14, 16, plotH*0.72, 8).Fill()
		d.SetFillColor(specPanel).BeginPath().RoundRect(plotX+plotW*0.10, plotY+plotH*0.16, plotW*0.18, plotH*0.22, 6).Fill()
		d.SetStrokeColor(specLineRaised).SetLineWidth(1).BeginPath().RoundRect(plotX+plotW*0.10, plotY+plotH*0.16, plotW*0.18, plotH*0.22, 6).Stroke()
	} else {
		// Side-view ground floor + mid platform silhouette.
		floorH := max(float32(28), plotH*0.18)
		d.SetFillColor(specRaised).FillRect(plotX, plotY+plotH-floorH, plotW, floorH)
		d.SetStrokeColor(specLineRaised).SetLineWidth(1.5).BeginPath().MoveTo(plotX, plotY+plotH-floorH).LineTo(plotX+plotW, plotY+plotH-floorH).Stroke()
		platY := plotY + plotH*0.58
		d.SetFillColor(specLineRaised).BeginPath().RoundRect(plotX+plotW*0.50, platY, plotW*0.24, 12, 3).Fill()
	}

	// Optional open-scene object markers when scene coordinates overlap the heat map bounds.
	if scene != nil {
		for _, layer := range scene.Layers {
			for _, obj := range layer.Objects {
				ox, oy := float64(obj.X), float64(obj.Y)
				if ox >= chart.MinX && ox <= chart.MaxX && oy >= chart.MinY && oy <= chart.MaxY {
					px, py := toX(ox), toY(oy)
					d.SetFillColor(specLineRaised).BeginPath().RoundRect(px-8, py-6, 16, 12, 3).Fill()
				}
			}
		}
	}

	// Draw layered concentric translucent radial heat blobs for each cluster.
	for _, cl := range chart.Clusters {
		if !isFinite64(cl.X) || !isFinite64(cl.Y) {
			continue
		}
		share := cl.Share
		if !isFinite64(share) || share < 0 {
			share = 0
		}
		cx := toX(cl.X)
		cy := toY(cl.Y)
		baseR := float32(28) + float32(share)*46
		if baseR > 84 {
			baseR = 84
		}
		outer, mid, coreCol := specPlaytestHeatAmberLow, specPlaytestHeatAmberMid, specPlaytestHeatAmberHi
		if cl.Problem {
			outer, mid, coreCol = specPlaytestHeatRedSoft, specPlaytestHeatRedMid, specPlaytestHeatRedHigh
		}
		d.SetFillColor(outer).BeginPath().Arc(cx, cy, baseR, 0, float32(2*math.Pi), false).Fill()
		d.SetFillColor(mid).BeginPath().Arc(cx, cy, baseR*0.64, 0, float32(2*math.Pi), false).Fill()
		d.SetFillColor(coreCol).BeginPath().Arc(cx, cy, baseR*0.34, 0, float32(2*math.Pi), false).Fill()
	}

	// Individual event sample dots so single events remain visible.
	for _, pt := range chart.Points {
		if !isFinite64(pt.X) || !isFinite64(pt.Y) {
			continue
		}
		px, py := toX(pt.X), toY(pt.Y)
		d.SetFillColor(specTextStrong).BeginPath().Arc(px, py, 2.5, 0, float32(2*math.Pi), false).Fill()
	}

	// Draw hotspot callout chips above each top cluster.
	chipFont := core.Font{Size: 11, Weight: 500}
	for i, chip := range chart.Chips {
		if !isFinite64(chip.X) || !isFinite64(chip.Y) {
			continue
		}
		px := toX(chip.X)
		py := toY(chip.Y) - 24 - float32(i%2)*14
		chipW := float32(len(chip.Text))*5.8 + 20
		chipH := float32(22)
		rx := px - chipW/2
		if rx < plotX+6 {
			rx = plotX + 6
		}
		if rx+chipW > plotX+plotW-6 {
			rx = plotX + plotW - 6 - chipW
		}
		ry := py - chipH
		if ry < plotY+6 {
			ry = plotY + 6 + float32(i)*26
		}
		borderCol := specLineRaised
		textCol := specText
		if chip.Problem {
			borderCol = specProblemEdge
			textCol = specProblem
		}
		d.SetFillColor(specBgOverlay).BeginPath().RoundRect(rx, ry, chipW, chipH, 11).Fill()
		d.SetStrokeColor(borderCol).SetLineWidth(1).BeginPath().RoundRect(rx, ry, chipW, chipH, 11).Stroke()
		d.SetFillColor(textCol).FillText(chip.Text, rx+10, ry+15, chipFont)
	}

	// Compact bottom-right heat legend (density + text, not color alone).
	legFont := core.Font{Size: 10.5}
	legY := plotY + plotH - 10
	legX := plotX + plotW - 188
	if legX < plotX+8 {
		legX = plotX + 8
	}
	d.SetFillColor(specBgOverlay).BeginPath().RoundRect(legX-8, legY-13, 190, 20, 6).Fill()
	d.SetStrokeColor(specLine).SetLineWidth(1).BeginPath().RoundRect(legX-8, legY-13, 190, 20, 6).Stroke()
	d.SetFillColor(specChanged).BeginPath().Arc(legX+4, legY-3, 4, 0, float32(2*math.Pi), false).Fill()
	d.SetFillColor(specTextSecondary).FillText("Moderate", legX+14, legY+1, legFont)
	d.SetFillColor(specProblem).BeginPath().Arc(legX+82, legY-3, 4, 0, float32(2*math.Pi), false).Fill()
	d.SetFillColor(specTextSecondary).FillText("Hotspot cluster", legX+92, legY+1, legFont)
}

func drawPlaytestBarChart(d *core.Drawing, w, h float32, chart *PlaytestSeriesChart) {
	if chart == nil || len(chart.Entries) == 0 {
		d.SetFillColor(specTextMuted).FillText("No bar series entries to chart", 16, h/2, core.Font{Size: 12})
		return
	}

	padL, padR := float32(28), float32(28)
	padT, padB := float32(28), float32(42)
	plotW := max(float32(40), w-padL-padR)
	plotH := max(float32(40), h-padT-padB)
	baseY := padT + plotH

	// Subtle baseline and top guide.
	d.SetStrokeColor(specLine).SetLineWidth(1).BeginPath().MoveTo(padL, baseY).LineTo(padL+plotW, baseY).Stroke()
	d.SetStrokeColor(specPlaytestGridLine).SetLineWidth(1).BeginPath().MoveTo(padL, padT+plotH*0.5).LineTo(padL+plotW, padT+plotH*0.5).Stroke()

	maxVal := chart.MaxVal
	if !isFinite64(maxVal) || maxVal <= 0 {
		maxVal = 1
	}

	// Dashed horizontal goal line when TargetMax is set.
	if chart.TargetMax != nil && isFinite64(*chart.TargetMax) {
		ratio := float32(*chart.TargetMax / maxVal)
		if ratio > 1 {
			ratio = 1
		}
		if ratio < 0 {
			ratio = 0
		}
		gy := baseY - ratio*plotH
		d.Save()
		d.SetStrokeColor(specTextMuted).SetLineWidth(1).SetLineDash(5, 4)
		d.BeginPath().MoveTo(padL, gy).LineTo(padL+plotW, gy).Stroke()
		d.Restore()
		if chart.TargetText != "" {
			tw := float32(len(chart.TargetText)) * 6.0
			tx := padL + plotW - tw
			if tx < padL {
				tx = padL
			}
			d.SetFillColor(specTextSecondary).FillText(chart.TargetText, tx, max(padT, gy-6), core.Font{Size: 11})
		}
	}

	n := len(chart.Entries)
	gap := float32(14)
	if n > 1 && gap*float32(n-1) > plotW*0.35 {
		gap = max(float32(2), (plotW*0.35)/float32(n-1))
	}
	slotW := (plotW - gap*float32(max(0, n-1))) / float32(n)
	if slotW < 4 {
		gap = 0
		slotW = max(float32(4), plotW/float32(n))
	}
	barW := min(float32(64), min(slotW, max(float32(3), slotW*0.64)))

	valFont := core.Font{Family: core.FontMonospace, Size: 11.5}
	lblFont := core.Font{Size: 11.5}

	for i, e := range chart.Entries {
		val := e.Value
		if !isFinite64(val) {
			val = 0
		}
		slotX := padL + float32(i)*(slotW+gap)
		barX := slotX + (slotW-barW)/2
		bh := float32((max(0.0, val) / maxVal) * float64(plotH))
		if bh < 3 && val > 0 {
			bh = 3
		}
		if bh > plotH {
			bh = plotH
		}
		barY := baseY - bh

		flaggedProblem := e.Flag == playtest.FlagProblem || (chart.TargetMax != nil && isFinite64(*chart.TargetMax) && val > *chart.TargetMax)
		flaggedWarn := !flaggedProblem && e.Flag == playtest.FlagWarning

		barCol := specBarNormal
		valCol := specTextSecondary
		lblCol := specTextMuted
		if flaggedProblem {
			barCol = specProblem
			valCol = specProblem
			lblCol = specTextStrong
		} else if flaggedWarn {
			barCol = specChanged
			valCol = specChanged
			lblCol = specTextStrong
		}

		d.SetFillColor(barCol).BeginPath().RoundRect(barX, barY, barW, bh, 5).Fill()

		valText := formatCleanNumber(val, chart.Decimals)
		if flaggedProblem {
			valText += " !"
		}
		vw := float32(len(valText)) * 6.8
		d.SetFillColor(valCol).FillText(valText, slotX+(slotW-vw)/2, max(float32(14), barY-6), valFont)

		lw := float32(len(e.Label)) * 6.2
		d.SetFillColor(lblCol).FillText(e.Label, slotX+(slotW-lw)/2, baseY+20, lblFont)
	}
}

func drawPlaytestLineChart(d *core.Drawing, w, h float32, chart *PlaytestSeriesChart) {
	if chart == nil || len(chart.Entries) == 0 {
		d.SetFillColor(specTextMuted).FillText("No level progression entries to chart", 16, h/2, core.Font{Size: 12})
		return
	}

	padL, padR := float32(44), float32(28)
	padT, padB := float32(32), float32(44)
	plotW := max(float32(40), w-padL-padR)
	plotH := max(float32(40), h-padT-padB)
	baseY := padT + plotH

	maxVal := chart.MaxVal
	if !isFinite64(maxVal) || maxVal <= 0 {
		maxVal = 1
	}

	// Horizontal reference grid lines with Y-axis labels.
	axisFont := core.Font{Family: core.FontMonospace, Size: 10.5}
	for step := 0; step <= 4; step++ {
		frac := float32(step) / 4.0
		y := baseY - frac*plotH
		col := specPlaytestGridLine
		if step == 0 {
			col = specLine
		}
		d.SetStrokeColor(col).SetLineWidth(1).BeginPath().MoveTo(padL, y).LineTo(padL+plotW, y).Stroke()
		val := maxVal * float64(frac)
		d.SetFillColor(specTextMuted).FillText(formatCleanNumber(val, chart.Decimals), 8, y+4, axisFont)
	}

	// Optional dashed target line.
	if chart.TargetMax != nil && isFinite64(*chart.TargetMax) {
		ratio := float32(min(1.0, max(0.0, *chart.TargetMax/maxVal)))
		gy := baseY - ratio*plotH
		d.Save()
		d.SetStrokeColor(specTextMuted).SetLineWidth(1).SetLineDash(5, 4)
		d.BeginPath().MoveTo(padL, gy).LineTo(padL+plotW, gy).Stroke()
		d.Restore()
		if chart.TargetText != "" {
			tw := float32(len(chart.TargetText)) * 6.0
			d.SetFillColor(specTextSecondary).FillText(chart.TargetText, max(padL, padL+plotW-tw), max(padT, gy-6), core.Font{Size: 11})
		}
	}

	n := len(chart.Entries)
	xs := make([]float32, n)
	ys := make([]float32, n)
	for i, e := range chart.Entries {
		if n == 1 {
			xs[i] = padL + plotW/2
		} else {
			xs[i] = padL + plotW*float32(i)/float32(n-1)
		}
		val := e.Value
		if !isFinite64(val) {
			val = 0
		}
		ratio := float32(min(1.0, max(0.0, val/maxVal)))
		ys[i] = baseY - ratio*plotH
	}

	// Subtle translucent area fill beneath the line.
	if n >= 2 {
		p := d.SetFillColor(specPlaytestLineArea).BeginPath().MoveTo(xs[0], baseY)
		for i := range n {
			p.LineTo(xs[i], ys[i])
		}
		p.LineTo(xs[n-1], baseY).ClosePath().Fill()

		// Primary line stroke in Spec accent (#3B7BFF).
		lp := d.SetStrokeColor(specAccent).SetLineWidth(2.2).BeginPath().MoveTo(xs[0], ys[0])
		for i := 1; i < n; i++ {
			lp.LineTo(xs[i], ys[i])
		}
		lp.Stroke()
	}

	valFont := core.Font{Family: core.FontMonospace, Size: 11}
	lblFont := core.Font{Size: 11}

	// Draw data points, value labels, and problem/warning markers.
	for i, e := range chart.Entries {
		px, py := xs[i], ys[i]
		dotCol := specAccent
		valCol := specTextSecondary
		lblCol := specTextMuted
		r := float32(4.5)
		if e.Flag == playtest.FlagProblem {
			dotCol = specProblem
			valCol = specProblem
			lblCol = specTextStrong
			r = 6
		} else if e.Flag == playtest.FlagWarning {
			dotCol = specChanged
			valCol = specChanged
			lblCol = specTextStrong
			r = 5.5
		}

		// Halo ring around flagged points so dips/spikes stand out clearly without relying on hue alone.
		if e.Flag != "" {
			d.SetStrokeColor(dotCol).SetLineWidth(1.5).BeginPath().Arc(px, py, r+4, 0, float32(2*math.Pi), false).Stroke()
		}
		d.SetFillColor(dotCol).BeginPath().Arc(px, py, r, 0, float32(2*math.Pi), false).Fill()
		d.SetStrokeColor(specBg).SetLineWidth(1.5).BeginPath().Arc(px, py, r, 0, float32(2*math.Pi), false).Stroke()

		val := e.Value
		if !isFinite64(val) {
			val = 0
		}
		valText := formatCleanNumber(val, chart.Decimals)
		if e.Flag == playtest.FlagProblem {
			valText += " !"
		}
		vw := float32(len(valText)) * 6.4
		lblOffset := float32(12)
		if e.Flag != "" {
			lblOffset = 16
		}
		d.SetFillColor(valCol).FillText(valText, px-vw/2, max(float32(14), py-lblOffset), valFont)

		lw := float32(len(e.Label)) * 6.0
		d.SetFillColor(lblCol).FillText(e.Label, px-lw/2, baseY+20, lblFont)
	}
}

func drawPlaytestMatrixChart(d *core.Drawing, w, h float32, chart *PlaytestMatrixChart) {
	if chart == nil || len(chart.Labels) == 0 {
		d.SetFillColor(specTextMuted).FillText("No win-rate matrix data to chart", 16, h/2, core.Font{Size: 12})
		return
	}

	n := len(chart.Labels)
	gap := float32(8)
	rowHeaderW := float32(92)
	colHeaderH := float32(30)
	captionH := float32(26)
	pad := float32(20)

	availW := max(float32(80), w-pad*2-rowHeaderW-gap*float32(n))
	availH := max(float32(80), h-pad*2-colHeaderH-captionH-gap*float32(max(0, n-1)))
	cellW := min(float32(148), availW/float32(n))
	cellH := min(float32(88), availH/float32(n))

	totalW := rowHeaderW + gap + float32(n)*cellW + float32(max(0, n-1))*gap
	totalH := colHeaderH + gap + float32(n)*cellH + float32(max(0, n-1))*gap + captionH
	originX := max(pad, (w-totalW)/2)
	originY := max(pad, (h-totalH)/2)

	headFont := core.Font{Size: 12, Weight: 500}
	rowFont := core.Font{Size: 12.5, Weight: 600}
	valFont := core.Font{Size: 20, Weight: 600}
	noteFont := core.Font{Size: 11, Weight: 500}

	// Column headers ("vs <label>").
	for c, lbl := range chart.Labels {
		cx := originX + rowHeaderW + gap + float32(c)*(cellW+gap)
		text := "vs " + lbl
		tw := float32(len(text)) * 6.4
		d.SetFillColor(specTextMuted).FillText(text, cx+(cellW-tw)/2, originY+20, headFont)
	}

	// Rows + cells.
	flagThresholdText := fmt.Sprintf("above %s%s", formatCleanNumber(chart.FlagAbove, 0), chart.Unit)
	for r, rowLbl := range chart.Labels {
		ry := originY + colHeaderH + gap + float32(r)*(cellH+gap)
		rw := float32(len(rowLbl)) * 7.0
		d.SetFillColor(specTextStrong).FillText(rowLbl, originX+rowHeaderW-rw-8, ry+cellH/2+5, rowFont)

		for c := range n {
			cx := originX + rowHeaderW + gap + float32(c)*(cellW+gap)
			var cell *float64
			if r < len(chart.Cells) && c < len(chart.Cells[r]) {
				cell = chart.Cells[r][c]
			}

			fill := specRaised
			border := specLineRaised
			borderW := float32(1)
			valText := "—"
			valCol := specTextMuted
			noteText := "same deck"
			noteCol := specTextMuted

			if cell != nil && !isFinite64(*cell) {
				cell = nil
			}
			if cell != nil {
				v := *cell
				valText = formatCleanNumber(v, 0) + chart.Unit
				valCol = specTextStrong
				switch {
				case v > chart.FlagAbove:
					fill = specProblemTint
					border = specProblem
					borderW = 1.5
					noteText = flagThresholdText
					noteCol = specProblem
				case v > chart.FlagAbove-5:
					fill = specWarningFill
					border = specWarningEdge
					noteText = "watch"
					noteCol = specWarning
				default:
					fill = specRaised
					border = specLineRaised
					noteText = "balanced"
					noteCol = specTextMuted
				}
			}

			d.SetFillColor(fill).BeginPath().RoundRect(cx, ry, cellW, cellH, 8).Fill()
			if cell != nil {
				d.SetStrokeColor(border).SetLineWidth(borderW).BeginPath().RoundRect(cx, ry, cellW, cellH, 8).Stroke()
			}

			// Draw a small flag mark on >60% cells so flagged cells are distinguished by shape + text + border width, not color alone.
			if cell != nil && *cell > chart.FlagAbove {
				d.SetFillColor(specProblem).BeginPath().Arc(cx+cellW-12, ry+12, 3.5, 0, float32(2*math.Pi), false).Fill()
			}

			vw := float32(len(valText)) * 11.0
			d.SetFillColor(valCol).FillText(valText, cx+(cellW-vw)/2, ry+cellH*0.48, valFont)

			nw := float32(len(noteText)) * 6.0
			d.SetFillColor(noteCol).FillText(noteText, cx+(cellW-nw)/2, ry+cellH*0.76, noteFont)
		}
	}

	// Footer caption.
	if chart.Caption != "" {
		cw := float32(len(chart.Caption)) * 6.1
		capY := originY + colHeaderH + gap + float32(n)*(cellH+gap) + 14
		d.SetFillColor(specTextMuted).FillText(chart.Caption, max(pad, (w-cw)/2), capY, core.Font{Size: 12})
	}
}
