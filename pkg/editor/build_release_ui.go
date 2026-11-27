package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/internal/qrcode"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

func buildSpecIcon(svg *core.SVG, size core.Points, color core.Color) core.Node {
	return core.Icon{
		SVG: svg,
		Style: core.Style{
			Width:  core.Pt(size),
			Height: core.Pt(size),
			Color:  color,
		},
	}
}

// BuildReleaseScreenView renders the full-window Build and Release screen matching
// design/editor/Build.dc.html using exclusively Spec color, spacing, radius,
// typography, and focus-ring tokens from design/editor/Spec.dc.html.
func BuildReleaseScreenView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	brRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.buildReleaseRev()
	})

	versionDraft := core.NewSignal(cx, "")
	notesDraft := core.NewSignal(cx, "")

	projectVer := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project != nil {
			return ed.Project.AppVersion
		}
		return ""
	})
	cx.Effect(func() {
		versionDraft.Set(projectVer.Get())
	})

	snapNotes := core.NewMemo(cx, func() string {
		_ = brRev.Get()
		return ed.BuildReleaseSnapshot().ReleaseNotes
	})
	cx.Effect(func() {
		if notesDraft.Get() != snapNotes.Get() {
			notesDraft.Set(snapNotes.Get())
		}
	})

	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = langMemo.Get()
		p := ed.Palette(cx)
		return core.View{
			TestID: "build-release-screen",
			Role:   core.RoleRegion,
			Label:  ed.T("buildRelease.screenLabel"),
			Style: core.Style{
				Position:   core.Relative,
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
				Overflow:   core.Hidden,
			},
			Children: []core.Node{
				buildReleaseHeaderView(cx, p, ed, brRev, versionDraft),
				core.View{
					Style: core.Style{
						Direction: core.Row,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						buildReleaseLeftColumnView(cx, p, ed, brRev, notesDraft),
						buildReleaseDetailPaneView(cx, p, ed, brRev),
					},
				},
				buildReleaseReviewUploadsOverlay(cx, ed, brRev),
			},
		}
	})
}

func buildReleaseHeaderView(cx *core.Scope, p theme.Palette, ed *Editor, brRev *core.Memo[uint64], versionDraft *core.Signal[string]) core.Node {
	projectNameMemo := core.NewMemo(cx, func() string {
		_ = brRev.Get()
		name := ed.BuildReleaseSnapshot().ProjectName
		if name == "" {
			return ed.T("buildRelease.defaultProjectName")
		}
		return name
	})

	// Keep the outer header and version ui.Input outside core.Dynamic so streaming log lines
	// and release-notes keystrokes (which increment brRev) never remount or drop focus on
	// build-release-version.
	return core.View{
		TestID: "build-release-header",
		Role:   core.RoleRegion,
		Label:  ed.T("buildRelease.headerLabel"),
		Style: core.Style{
			Height:      core.Pt(44),
			FlexShrink:  0,
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         10,
			Padding:     core.Symmetric(0, 12),
			Background:  p.Bg,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: []core.Node{
			core.View{
				TestID: "build-release-back",
				Role:   core.RoleButton,
				Label:  ed.T("buildRelease.backLabel"),
				Style: core.Style{
					Height:       core.Pt(28),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(0, 6),
					CornerRadius: 6,
					Hover:        &core.Style{Background: p.Raised},
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.CloseBuildReleaseScreen()
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(20),
							Height:       core.Pt(20),
							CornerRadius: 5,
							Background:   p.Accent.Fg,
						},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						cur := ed.Palette(cx)
						return core.Text{
							Content: projectNameMemo.Get(),
							Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightSemibold, Color: cur.TextPrimary},
						}
					}),
					core.Text{
						Content: "/",
						Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
					},
					core.Text{
						Content: ed.T("buildRelease.releaseBreadcrumb"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
					},
				},
			},
			core.Text{
				Content: ed.T("buildRelease.versionHeading"),
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.View{
				Style: core.Style{Width: core.Pt(72)},
				Children: []core.Node{
					ui.Input{
						TestID:      "build-release-version",
						Label:       ed.T("buildRelease.versionLabel"),
						Placeholder: "0.1.0",
						Disabled: func() bool {
							_ = brRev.Get()
							return ed.ReleaseBuildRunning()
						},
						Value:    versionDraft.Get,
						OnChange: versionDraft.Set,
						OnSubmit: func(v string) {
							ed.report(ed.SetReleaseVersion(v))
						},
						Style: core.Style{
							Width:        core.Pct(100),
							Height:       core.Pt(28),
							Background:   p.Raised,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							CornerRadius: 6,
							Color:        p.TextPrimary,
							FontSize:     11.5,
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				p := ed.Palette(cx)
				snap := ed.BuildReleaseSnapshot()

				includedCount := 0
				doneCount := 0
				failedCount := 0
				for _, pr := range snap.Presets {
					if pr.Included && pr.Status != BuildPresetSetup {
						includedCount++
						switch pr.Status {
						case BuildPresetDone:
							doneCount++
						case BuildPresetFailed:
							failedCount++
						}
					}
				}

				chipText := ed.T("buildRelease.chipReady", l10n.Param("count", includedCount))
				var chipBg core.Color
				chipBorder := p.LineRaised
				chipFg := p.TextMuted
				dotColor := p.TextFaint
				switch {
				case snap.Running:
					chipText = ed.T("buildRelease.chipBuilding", l10n.Param("done", doneCount), l10n.Param("total", includedCount))
					chipBg = p.Agent.Fill
					chipBorder = p.Agent.Edge
					chipFg = p.TextSecondary
					dotColor = p.Agent.Fg
				case snap.Stopped:
					chipText = ed.T("buildRelease.chipStopped", l10n.Param("done", doneCount), l10n.Param("total", includedCount))
					chipBorder = p.LineRaised
					chipFg = p.TextMuted
					dotColor = p.TextFaint
				case failedCount > 0:
					chipText = ed.T("buildRelease.chipFailed", l10n.Param("done", doneCount), l10n.Param("total", includedCount), l10n.Param("failed", failedCount))
					chipBg = p.Removed.Fill
					chipBorder = p.Removed.Edge
					chipFg = p.TextSecondary
					dotColor = p.Removed.Fg
				case doneCount > 0 && doneCount == includedCount:
					chipText = ed.T("buildRelease.chipDone", l10n.Param("done", doneCount), l10n.Param("total", includedCount))
					chipBg = p.Added.Fill
					chipBorder = p.Added.Edge
					chipFg = p.TextSecondary
					dotColor = p.Added.Fg
				}

				actionLabel := ed.T("buildRelease.actionBuild")
				actionBg := p.Accent.Fg
				actionFg := p.Accent.OnFill
				actionIcon := lucide.Play
				if snap.Running {
					actionLabel = ed.T("buildRelease.actionStop")
					actionBg = p.LineRaised
					actionFg = p.TextPrimary
					actionIcon = lucide.Square
				} else if snap.Stopped {
					actionLabel = ed.T("buildRelease.actionResume")
					actionBg = p.Accent.Fg
					actionFg = p.Accent.OnFill
					actionIcon = lucide.Play
				}

				commit := localizedBuildCommit(ed, snap.GitCommit)
				branch := snap.GitBranch
				if branch == "" {
					branch = "main"
				}
				lastRel := localizedBuildLastRelease(ed, snap.LastReleaseVersion)
				metaText := ed.T("buildRelease.metaLine", l10n.Param("commit", commit), l10n.Param("branch", branch), l10n.Param("lastRelease", lastRel))

				return core.View{
					Style: core.Style{
						FlexGrow:   1,
						FlexBasis:  core.Pt(0),
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        10,
					},
					Children: []core.Node{
						core.Text{
							TestID:  "build-release-meta",
							Content: metaText,
							Style:   core.Style{FontSize: 12, Color: p.TextFaint, FontVariant: core.FontTabularNums},
						},
						core.View{Style: core.Style{FlexGrow: 1}},
						buildReleasePillButton(p, "build-release-dry-run", ed.T("buildRelease.dryRun"), snap.DryRun, snap.Running, func() {
							ed.SetReleaseDryRun(!snap.DryRun)
						}),
						core.View{
							TestID: "build-release-status-chip",
							Style: core.Style{
								Height:       core.Pt(28),
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          8,
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 14,
								Background:   chipBg,
								BorderWidth:  1,
								BorderColor:  chipBorder,
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 3,
										Background:   dotColor,
									},
								},
								core.Text{
									Content: chipText,
									Style:   core.Style{FontSize: 12, Color: chipFg},
								},
							},
						},
						core.View{
							TestID: "build-release-primary-action",
							Role:   core.RoleButton,
							Label:  actionLabel,
							Style: core.Style{
								Height:       core.Pt(30),
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          6,
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 8,
								Background:   actionBg,
								FocusVisible: p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								switch {
								case snap.Running:
									ed.StopReleaseBuild()
								case snap.Stopped:
									ed.report(ed.ResumeReleaseBuild())
								default:
									ed.report(ed.StartReleaseBuild())
								}
							},
							Children: []core.Node{
								buildSpecIcon(actionIcon(), 12, actionFg),
								core.Text{
									Content: actionLabel,
									Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightMedium, Color: actionFg},
								},
							},
						},
					},
				}
			}),
		},
	}
}

func buildReleasePillButton(p theme.Palette, testID, label string, active, disabled bool, onPress func()) core.Node {
	bg := p.Raised
	border := p.LineRaised
	fg := p.TextMuted
	if active {
		bg = p.Accent.Fill
		border = p.Accent.Fg
		fg = p.TextPrimary
	}
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  label,
		Style: core.Style{
			Height:       core.Pt(28),
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Padding:      core.Symmetric(0, 10),
			CornerRadius: 8,
			Background:   bg,
			BorderWidth:  1,
			BorderColor:  border,
			FocusVisible: p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if !disabled {
				onPress()
			}
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style:   core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: fg},
			},
		},
	}
}

func buildReleaseLeftColumnView(cx *core.Scope, p theme.Palette, ed *Editor, brRev *core.Memo[uint64], notesDraft *core.Signal[string]) core.Node {
	return core.View{
		TestID: "build-release-left-column",
		Style: core.Style{
			Width:       core.Pt(500),
			Height:      core.Pct(100),
			FlexShrink:  0,
			Background:  p.Bg,
			Borders:     core.Edges{Right: 1},
			BorderColor: p.Line,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				cur := ed.Palette(cx)
				snap := ed.BuildReleaseSnapshot()
				return buildReleasePreflightSection(cur, ed, snap.Preflight)
			}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				cur := ed.Palette(cx)
				snap := ed.BuildReleaseSnapshot()
				return buildReleasePresetsSection(cur, ed, snap)
			}),
			buildReleaseNotesSection(p, ed, brRev, notesDraft),
			buildReleasePublishFooter(p, ed),
		},
	}
}

func buildReleasePreflightSection(p theme.Palette, ed *Editor, pf BuildPreflightSummary) core.Node {
	checkIcon := lucide.Check()
	checkColor := p.Added.Fg
	checkDetail := ed.T("buildRelease.preflightNotRun")
	if pf.CheckRan {
		elapsed := formatBuildDuration(pf.CheckElapsed)
		if elapsed == "" {
			elapsed = ed.T("buildRelease.lessThanOneSec")
		}
		if pf.CheckErr != "" {
			checkIcon = lucide.CircleAlert()
			checkColor = p.Removed.Fg
			checkDetail = ed.T("buildRelease.preflightError", l10n.Param("elapsed", elapsed))
		} else if pf.CheckProblems > 0 {
			checkIcon = lucide.CircleAlert()
			checkColor = p.Removed.Fg
			checkDetail = ed.T("buildRelease.preflightProblems", l10n.Param("count", pf.CheckProblems), l10n.Param("elapsed", elapsed))
		} else {
			checkDetail = ed.T("buildRelease.preflightProblems", l10n.Param("count", 0), l10n.Param("elapsed", elapsed))
		}
	}

	smokeDetail := localizedBuildSmokeSummary(ed, pf.SmokeSummary)

	profile := localizedBuildQualityProfile(ed, pf.QualityProfile)
	dlBudget := pf.DownloadSizeBudgetMB
	if dlBudget <= 0 {
		dlBudget = DefaultDownloadSizeBudgetMB
	}
	frameBudget := pf.FrameTimeP95BudgetMs
	if frameBudget <= 0 {
		frameBudget = DefaultFrameTimeP95BudgetMs
	}
	budgetDetail := fmt.Sprintf("≤ %.0f MB · p95 ≤ %.1f ms", dlBudget, frameBudget)

	cardStyle := core.Style{
		FlexGrow:     1,
		FlexBasis:    core.Pt(0),
		Padding:      core.Symmetric(10, 12),
		CornerRadius: 12,
		Background:   p.Raised,
		BorderWidth:  1,
		BorderColor:  p.LineRaised,
		Gap:          4,
	}

	return core.View{
		TestID: "build-release-preflight",
		Style: core.Style{
			Padding: core.Edges{Top: 14, Left: 16, Right: 16, Bottom: 8},
			Gap:     8,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("buildRelease.preflightHeading"),
				Style: core.Style{
					FontSize:      11,
					FontWeight:    core.FontWeightSemibold,
					LetterSpacing: 0.7,
					Color:         p.TextFaint,
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, Gap: 8},
				Children: []core.Node{
					core.View{
						TestID: "build-preflight-check",
						Style:  cardStyle,
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
								Children: []core.Node{
									buildSpecIcon(checkIcon, 13, checkColor),
									core.Text{Content: ed.T("buildRelease.preflightCheckTitle"), Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
								},
							},
							core.Text{Content: checkDetail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
					core.View{
						TestID: "build-preflight-smoke",
						Style:  cardStyle,
						Children: []core.Node{
							core.Text{Content: ed.T("buildRelease.preflightSmokeTitle"), Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
							core.Text{Content: smokeDetail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
					core.View{
						TestID: "build-preflight-budgets",
						Style:  cardStyle,
						Children: []core.Node{
							core.Text{Content: ed.T("buildRelease.preflightBudgetsTitle", l10n.Param("profile", profile)), Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
							core.Text{Content: budgetDetail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
				},
			},
		},
	}
}

func buildReleasePresetsSection(p theme.Palette, ed *Editor, snap BuildReleaseSnapshot) core.Node {
	rows := make([]core.Node, 0, len(snap.Presets))
	for _, pr := range snap.Presets {
		rows = append(rows, buildPresetRowView(p, ed, pr, pr.Platform == snap.SelectedPlatform, snap.Running))
	}
	return core.View{
		TestID: "build-release-presets",
		Style: core.Style{
			Padding: core.Edges{Top: 6, Left: 8, Right: 8, Bottom: 4},
			Gap:     2,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Height:         core.Pt(26),
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(0, 8),
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("buildRelease.presetsHeading"),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.7,
							Color:         p.TextFaint,
						},
					},
					core.Text{
						Content: ed.T("buildRelease.sizeVsVersion", l10n.Param("version", localizedBuildLastRelease(ed, snap.LastReleaseVersion))),
						Style:   core.Style{FontSize: 11, FontWeight: core.FontWeightMedium, Color: p.TextFaint},
					},
				},
			},
			core.View{
				Style:    core.Style{Gap: 2},
				Children: rows,
			},
		},
	}
}

func buildPresetRowView(p theme.Palette, ed *Editor, entry BuildPresetEntry, selected, running bool) core.Node {
	plat := entry.Platform
	var rowBg core.Color
	var leftBarColor core.Color
	if selected {
		rowBg = ui.Alpha(p.Accent.Fg, 0.14)
		leftBarColor = p.Accent.Fg
	}
	var opacity core.Opacity
	if !entry.Included {
		opacity = core.Alpha(0.6)
	}

	var boxBg core.Color
	boxBorder := p.TextFaint
	var boxBorderW core.Points = 1.5
	var checkChildren []core.Node
	if entry.Included {
		boxBg = p.Accent.Fg
		boxBorder = p.Accent.Fg
		boxBorderW = 0
		checkChildren = []core.Node{buildSpecIcon(lucide.Check(), 10, p.Accent.OnFill)}
	}

	iconSVG := lucide.Monitor()
	switch entry.IconKind {
	case "web":
		iconSVG = lucide.Globe()
	case "phone":
		iconSVG = lucide.Smartphone()
	}

	chipBg, chipBorder, chipFg, chipIcon, chipIconColor := presetStatusChipStyle(p, entry.Status)
	var chipChildren []core.Node
	if chipIcon != nil {
		chipChildren = append(chipChildren, buildSpecIcon(chipIcon, 11, chipIconColor))
	} else if entry.Status == BuildPresetBuilding || entry.Status == BuildPresetNotarizing {
		chipChildren = append(chipChildren, core.View{
			Style: core.Style{
				Width:        core.Pt(8),
				Height:       core.Pt(8),
				CornerRadius: 4,
				Background:   p.Agent.Fg,
			},
		})
	}
	chipChildren = append(chipChildren, core.Text{
		Content: localizedBuildStatusText(ed, entry.StatusText),
		Style:   core.Style{FontSize: 11.5, Color: chipFg},
	})

	return core.View{
		TestID: "build-preset-row-" + plat,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          10,
			Height:       core.Pt(50),
			Padding:      core.Edges{Left: 8, Right: 12},
			CornerRadius: 8,
			Background:   rowBg,
			Opacity:      opacity,
			Borders:      core.Edges{Left: 2},
			BorderColor:  leftBarColor,
			Hover:        &core.Style{Background: p.Raised},
		},
		Children: []core.Node{
			core.View{
				TestID: "build-preset-check-" + plat,
				Role:   core.RoleCheckbox,
				Label:  ed.T("buildRelease.includePresetLabel", l10n.Param("name", entry.Name)),
				Style: core.Style{
					Width:          core.Pt(16),
					Height:         core.Pt(16),
					FlexShrink:     0,
					CornerRadius:   4,
					Background:     boxBg,
					BorderWidth:    boxBorderW,
					BorderColor:    boxBorder,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					if !running {
						ed.report(ed.SetBuildPresetIncluded(plat, !entry.Included))
					}
				},
				Children: checkChildren,
			},
			core.View{
				TestID: "build-preset-pick-" + plat,
				Role:   core.RoleButton,
				Label:  ed.T("buildRelease.selectPresetLabel", l10n.Param("name", entry.Name)),
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					Height:       core.Pct(100),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          10,
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.SelectBuildPreset(plat)
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(28),
							Height:         core.Pt(28),
							FlexShrink:     0,
							CornerRadius:   8,
							Background:     p.Line,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							buildSpecIcon(iconSVG, 15, p.TextMuted),
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2, Overflow: core.Hidden},
						Children: []core.Node{
							buildPresetTitleRow(p, ed, entry),
							core.Text{Content: localizedBuildSummary(ed, entry.Summary), Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
					core.View{
						TestID: "build-preset-status-" + plat,
						Style: core.Style{
							Height:       core.Pt(22),
							FlexShrink:   0,
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          5,
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 11,
							Background:   chipBg,
							BorderWidth:  1,
							BorderColor:  chipBorder,
						},
						Children: chipChildren,
					},
					core.View{
						Style: core.Style{Width: core.Pt(82), FlexShrink: 0, AlignItems: core.AlignEnd, Gap: 2},
						Children: []core.Node{
							core.Text{
								TestID:  "build-preset-size-" + plat,
								Content: entry.SizeText,
								Style:   core.Style{FontSize: 11.5, Color: p.TextSecondary, FontVariant: core.FontTabularNums},
							},
							core.Text{
								TestID:  "build-preset-delta-" + plat,
								Content: localizedBuildDeltaText(ed, entry.DeltaText),
								Style:   core.Style{FontSize: 11, Color: p.TextFaint, FontVariant: core.FontTabularNums},
							},
						},
					},
				},
			},
		},
	}
}

func buildPresetTitleRow(p theme.Palette, ed *Editor, entry BuildPresetEntry) core.Node {
	nameText := core.Text{
		Content: entry.Name,
		Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightMedium, Color: p.TextPrimary},
	}
	if !entry.Experimental {
		return nameText
	}
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
		Children: []core.Node{
			nameText,
			core.View{
				TestID: "build-preset-experimental-" + entry.Platform,
				Style: core.Style{
					Height:         core.Pt(18),
					Padding:        core.Symmetric(0, 6),
					CornerRadius:   9,
					Background:     p.Warning.Fill,
					BorderWidth:    1,
					BorderColor:    p.Warning.Edge,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("settings.metaExperimental"),
						Style:   core.Style{FontSize: 10.5, FontWeight: core.FontWeightSemibold, Color: p.Warning.Fg},
					},
				},
			},
		},
	}
}

func presetStatusChipStyle(p theme.Palette, st BuildPresetStatus) (bg, border, fg core.Color, icon *core.SVG, iconColor core.Color) {
	switch st {
	case BuildPresetBuilding, BuildPresetNotarizing:
		return p.Agent.Fill, p.Agent.Edge, p.TextSecondary, nil, p.Agent.Fg
	case BuildPresetDone:
		return 0, p.LineRaised, p.TextSecondary, lucide.Check(), p.Added.Fg
	case BuildPresetFailed:
		return p.Removed.Fill, p.Removed.Edge, p.Removed.Fg, lucide.X(), p.Removed.Fg
	case BuildPresetSetup:
		return p.Warning.Fill, p.Warning.Edge, p.Warning.Fg, nil, p.Warning.Fg
	default:
		return 0, p.LineRaised, p.TextMuted, nil, p.TextMuted
	}
}

func buildReleaseNotesSection(p theme.Palette, ed *Editor, brRev *core.Memo[uint64], notesDraft *core.Signal[string]) core.Node {
	return core.View{
		TestID: "build-release-notes-section",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.All(14),
			Gap:       8,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				cur := ed.Palette(cx)
				snap := ed.BuildReleaseSnapshot()
				headerChildren := []core.Node{
					core.Text{
						Content: ed.T("buildRelease.notesHeading"),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.7,
							Color:         cur.TextFaint,
						},
					},
				}
				provenance := snap.ReleaseNotesProvenance
				if provenance == "" && snap.CommitsSinceRelease > 0 {
					since := snap.ReleaseNotesSinceRef
					if since == "" {
						since = snap.LastReleaseVersion
					}
					provenance = ed.Plural("buildRelease.notesDraftedFrom", snap.CommitsSinceRelease, l10n.Param("since", since))
				}
				if provenance != "" {
					headerChildren = append(headerChildren, core.View{
						TestID: "build-release-notes-provenance",
						Style: core.Style{
							Height:       core.Pt(22),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          5,
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 11,
							Background:   cur.Agent.Fill,
							BorderWidth:  1,
							BorderColor:  cur.Agent.Edge,
						},
						Children: []core.Node{
							buildSpecIcon(lucide.Sparkles(), 11, cur.Agent.Fg),
							core.Text{
								Content: provenance,
								Style:   core.Style{FontSize: 11, Color: cur.TextSecondary},
							},
						},
					})
				}
				return core.View{
					Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: headerChildren,
				}
			}),
			core.View{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					ui.Input{
						TestID:      "build-release-notes",
						Label:       ed.T("buildRelease.notesLabel"),
						Multiline:   true,
						Placeholder: ed.T("buildRelease.notesPlaceholder"),
						Value:       notesDraft.Get,
						OnChange: func(v string) {
							notesDraft.Set(v)
							ed.SetReleaseNotesText(v)
						},
						Style: core.Style{
							Width:        core.Pct(100),
							Height:       core.Pct(100),
							Background:   p.Raised,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							CornerRadius: 12,
							Padding:      core.Symmetric(8, 10),
							Color:        p.TextSecondary,
							FontSize:     12.5,
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				p := ed.Palette(cx)
				snap := ed.BuildReleaseSnapshot()
				notesFile := snap.ReleaseNotesFile
				if notesFile == "" {
					notesFile = DefaultReleaseNotesFile
				}
				return core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						core.Text{
							TestID:  "build-release-notes-file",
							Content: ed.T("buildRelease.notesSavedTo", l10n.Param("file", notesFile)),
							Style:   core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), FontSize: 11, Color: p.TextFaint},
						},
						core.View{
							TestID: "build-release-notes-redraft",
							Role:   core.RoleButton,
							Label:  ed.T("buildRelease.redraftLabel"),
							Style: core.Style{
								Height:       core.Pt(24),
								FlexShrink:   0,
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          5,
								Padding:      core.Symmetric(0, 8),
								CornerRadius: 6,
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
								Hover:        &core.Style{Background: p.LineRaised},
								FocusVisible: p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								if _, err := ed.RedraftReleaseNotes(); err != nil {
									ed.report(err)
								}
							},
							Children: []core.Node{
								buildSpecIcon(lucide.RefreshCw(), 11, p.TextMuted),
								core.Text{
									Content: ed.T("buildRelease.redraft"),
									Style:   core.Style{FontSize: 11, Color: p.TextMuted},
								},
							},
						},
					},
				}
			}),
		},
	}
}

func buildReleasePublishFooter(p theme.Palette, ed *Editor) core.Node {
	reviewBtnRef := &core.Ref{}
	return core.View{
		TestID: "build-release-publish-footer",
		Style: core.Style{
			Height:      core.Pt(56),
			FlexShrink:  0,
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         12,
			Padding:     core.Symmetric(0, 16),
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
				Children: []core.Node{
					core.Text{Content: ed.T("buildRelease.publishTitle"), Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
					core.Text{Content: ed.T("buildRelease.publishSubtitle"), Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
				},
			},
			core.View{
				Ref:    reviewBtnRef,
				TestID: "build-release-review-uploads",
				Role:   core.RoleButton,
				Label:  ed.T("buildRelease.reviewUploads"),
				Style: core.Style{
					Height:         core.Pt(30),
					FlexShrink:     0,
					Padding:        core.Symmetric(0, 12),
					CornerRadius:   8,
					Background:     p.LineRaised,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Hover:          &core.Style{Background: p.Line},
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					if ed.focus.ctrl != nil {
						ed.focus.ctrl.recordDialogOpener(reviewBtnRef)
					}
					ed.report(ed.ReviewReleaseUploads())
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("buildRelease.reviewUploads"),
						Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
					},
				},
			},
		},
	}
}

func buildReleaseDetailPaneView(cx *core.Scope, p theme.Palette, ed *Editor, brRev *core.Memo[uint64]) core.Node {
	return core.View{
		TestID: "build-release-detail-pane",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Height:     core.Pct(100),
			Background: p.Panel,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				p := ed.Palette(cx)
				snap := ed.BuildReleaseSnapshot()
				sel := snap.SelectedPreset()
				if sel == nil {
					return core.View{
						Style: core.Style{
							FlexGrow:       1,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Padding:        core.All(24),
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("buildRelease.noPresets"),
								Style:   core.Style{FontSize: 13, Color: p.TextFaint},
							},
						},
					}
				}

				headerChildren := []core.Node{
					core.Text{
						TestID:  "build-detail-title",
						Content: sel.Name,
						Style:   core.Style{FontSize: 14, FontWeight: core.FontWeightSemibold, Color: p.TextPrimary},
					},
					core.Text{
						TestID:  "build-detail-file",
						Content: sel.File,
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
				}
				if sel.Status == BuildPresetDone {
					plat := sel.Platform
					headerChildren = append(headerChildren, buildReleaseSecondaryButtonSpec(p, "build-detail-show-finder", ed.T("buildRelease.showInFinder"), func() {
						ed.report(ed.ShowBuildArtifactInFinder(plat))
					}))
				}
				editLabel := ed.T("buildRelease.editPreset")
				if snap.EditingPreset {
					editLabel = ed.T("buildRelease.doneEditing")
				}
				headerChildren = append(headerChildren, buildReleaseSecondaryButtonSpec(p, "build-detail-edit-preset", editLabel, func() {
					ed.SetReleaseEditingPreset(!snap.EditingPreset)
				}))

				var body core.Node
				switch sel.Status {
				case BuildPresetBuilding, BuildPresetNotarizing:
					body = buildDetailRunningView(p, ed, *sel, snap.FollowLog)
				case BuildPresetFailed:
					body = buildDetailFailedView(p, ed, *sel, snap.ShowWholeLog)
				case BuildPresetDone:
					body = buildDetailDoneView(cx, ed, snap, *sel)
				case BuildPresetSetup:
					body = buildDetailReadinessView(p, ed, *sel)
				default:
					body = buildDetailQueuedView(p, ed, snap, *sel)
				}

				children := []core.Node{
					core.View{
						TestID: "build-detail-header",
						Style: core.Style{
							Height:      core.Pt(52),
							FlexShrink:  0,
							Direction:   core.Row,
							AlignItems:  core.AlignCenter,
							Gap:         12,
							Padding:     core.Symmetric(0, 20),
							Borders:     core.Edges{Bottom: 1},
							BorderColor: p.Line,
						},
						Children: headerChildren,
					},
				}
				if exp, ok := ed.ExportPresetExperimental(sel.Platform); ok {
					children = append(children, core.View{
						TestID: "build-detail-experimental-" + sel.Platform,
						Role:   core.RoleButton,
						Label:  ed.T("settings.metaExperimental") + " — " + exp.DocsURL,
						Style: core.Style{
							Direction:    core.Row,
							Wrap:         core.Wrap,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Padding:      core.Symmetric(8, 20),
							Background:   p.Warning.Fill,
							Borders:      core.Edges{Bottom: 1},
							BorderColor:  p.Warning.Edge,
							FocusVisible: p.FocusRing(-2),
						},
						OnPress: func(core.PressEvent) {
							ed.report(ed.OpenExperimentalPlatformDocs())
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("settings.metaExperimental"),
								Style:   core.Style{FontSize: 11.5, FontWeight: core.FontWeightSemibold, Color: p.Warning.Fg},
							},
							core.Text{
								Content: exp.Missing + " · " + exp.DocsURL,
								Style:   core.Style{FontSize: 11.5, Color: p.Warning.Fg},
							},
						},
					})
				}
				if snap.EditingPreset {
					children = append(children, buildInlinePresetEditor(p, ed, *sel, snap.Running))
				}
				children = append(children, body)

				return core.View{
					Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Overflow: core.Hidden},
					Children: children,
				}
			}),
		},
	}
}

func buildInlinePresetEditor(p theme.Palette, ed *Editor, entry BuildPresetEntry, running bool) core.Node {
	plat := entry.Platform
	validTargets := project.TargetsOf(plat)
	var targetPills []core.Node
	for _, t := range validTargets {
		target := t
		on := slices.Contains(entry.Targets, target)
		targetPills = append(targetPills, buildReleasePillButton(
			p,
			"build-edit-target-"+plat+"-"+target,
			target,
			on,
			running,
			func() {
				if _, ok := ed.Project.Preset(plat); !ok {
					ed.report(ed.AddExportPreset(plat, target))
				} else {
					ed.report(ed.ToggleExportPresetTarget(plat, target))
				}
				ed.syncBuildReleasePresets()
				ed.notify()
			},
		))
	}

	return core.View{
		TestID: "build-inline-preset-editor",
		Style: core.Style{
			Padding:     core.Symmetric(12, 20),
			Gap:         8,
			Background:  p.Raised,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("buildRelease.targetsFor", l10n.Param("name", strings.ToUpper(entry.Name))),
				Style:   core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: p.TextFaint},
			},
			core.View{
				Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6},
				Children: targetPills,
			},
		},
	}
}

func buildDetailRunningView(p theme.Palette, ed *Editor, entry BuildPresetEntry, followLog bool) core.Node {
	stepRows := make([]core.Node, 0, len(entry.Steps))
	for _, st := range entry.Steps {
		stepRows = append(stepRows, buildStepRowView(p, ed, st))
	}

	logContent := strings.Join(entry.LogLines, "\n")
	if logContent == "" {
		logContent = ed.T("buildRelease.waitingLog")
	}
	followLabel := ed.T("buildRelease.logFollowing")
	if !followLog {
		followLabel = ed.T("buildRelease.logPaused")
	}

	return core.View{
		TestID: "build-detail-running",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.All(20),
			Gap:       16,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			core.View{
				TestID:   "build-steps-list",
				Style:    core.Style{Gap: 2},
				Children: stepRows,
			},
			core.View{
				TestID: "build-log-card",
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					CornerRadius: 12,
					Background:   p.Bg,
					BorderWidth:  1,
					BorderColor:  p.Line,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Height:      core.Pt(36),
							FlexShrink:  0,
							Direction:   core.Row,
							AlignItems:  core.AlignCenter,
							Gap:         8,
							Padding:     core.Symmetric(0, 12),
							Borders:     core.Edges{Bottom: 1},
							BorderColor: p.Line,
						},
						Children: []core.Node{
							core.Text{Content: ed.T("buildRelease.logHeading"), Style: core.Style{FontSize: 12, Color: p.TextMuted}},
							core.Text{TestID: "build-log-file", Content: entry.LogFile, Style: core.Style{FontSize: 11, Color: p.TextFaint}},
							core.View{Style: core.Style{FlexGrow: 1}},
							buildReleasePillButton(p, "build-log-follow", followLabel, followLog, false, func() {
								ed.SetReleaseFollowLog(!followLog)
							}),
						},
					},
					ui.ScrollView{
						Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.Symmetric(10, 12)},
						ContentStyle: core.Style{Gap: 2},
						EdgeColor:    func() core.Color { return p.Bg },
						Children: []core.Node{
							core.Text{
								TestID:  "build-log-lines",
								Content: logContent,
								Style:   core.Style{FontSize: 11.5, Color: p.TextMuted, FontVariant: core.FontTabularNums},
							},
						},
					},
				},
			},
		},
	}
}

func buildStepRowView(p theme.Palette, ed *Editor, st BuildStep) core.Node {
	var badge core.Node
	var rowBg core.Color
	labelColor := p.TextPrimary
	switch st.Status {
	case BuildStepDone:
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				FlexShrink:     0,
				CornerRadius:   8,
				Background:     p.Added.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{buildSpecIcon(lucide.Check(), 10, p.Added.Fg)},
		}
	case BuildStepRunning:
		rowBg = p.Agent.Fill
		badge = core.View{
			Style: core.Style{
				Width:        core.Pt(16),
				Height:       core.Pt(16),
				FlexShrink:   0,
				CornerRadius: 8,
				BorderWidth:  2,
				BorderColor:  p.Agent.Fg,
			},
		}
	case BuildStepFailed:
		rowBg = p.Removed.Fill
		badge = core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				Height:         core.Pt(16),
				FlexShrink:     0,
				CornerRadius:   8,
				Background:     p.Removed.Fill,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{buildSpecIcon(lucide.X(), 10, p.Removed.Fg)},
		}
	default:
		labelColor = p.TextFaint
		badge = core.View{
			Style: core.Style{
				Width:        core.Pt(16),
				Height:       core.Pt(16),
				FlexShrink:   0,
				CornerRadius: 8,
				BorderWidth:  1.5,
				BorderColor:  p.LineRaised,
			},
		}
	}

	return core.View{
		TestID: "build-step-" + string(st.ID),
		Style: core.Style{
			Height:       core.Pt(32),
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          10,
			Padding:      core.Symmetric(0, 12),
			CornerRadius: 8,
			Background:   rowBg,
		},
		Children: []core.Node{
			badge,
			core.Text{Content: localizedBuildStepLabel(ed, st.Label), Style: core.Style{FontSize: 12.5, Color: labelColor}},
			core.Text{Content: localizedBuildStepDetail(ed, st.Detail), Style: core.Style{FontSize: 12, Color: p.TextFaint}},
			core.View{Style: core.Style{FlexGrow: 1}},
			core.Text{
				TestID:  "build-step-time-" + string(st.ID),
				Content: formatBuildDuration(st.Elapsed),
				Style:   core.Style{FontSize: 11, Color: p.TextFaint, FontVariant: core.FontTabularNums},
			},
		},
	}
}

func buildDetailFailedView(p theme.Palette, ed *Editor, entry BuildPresetEntry, showWholeLog bool) core.Node {
	f := entry.Failure
	if f == nil {
		f = &BuildFailureInfo{
			Headline:   ed.T("buildRelease.buildFailed"),
			StepIndex:  1,
			TotalSteps: len(entry.Steps),
			StepLabel:  ed.T("buildRelease.actionBuild"),
			Notice:     ed.T("buildRelease.failureNotice"),
		}
	}
	notice := f.Notice
	if notice == "" || notice == BuildFailureNotice {
		notice = ed.T("buildRelease.failureNotice")
	}
	stepMeta := ed.T("buildRelease.failureStepMeta", l10n.Param("step", f.StepIndex), l10n.Param("total", f.TotalSteps), l10n.Param("label", f.StepLabel))
	if dur := formatBuildDuration(f.Elapsed); dur != "" {
		stepMeta = ed.T("buildRelease.failureStepMetaAfter", l10n.Param("step", f.StepIndex), l10n.Param("total", f.TotalSteps), l10n.Param("label", f.StepLabel), l10n.Param("duration", dur))
	}

	var cmdNodes []core.Node
	for _, cmd := range f.Commands {
		cmdNodes = append(cmdNodes, core.Text{
			Content: cmd,
			Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
		})
	}
	if f.ErrorLine != "" {
		cmdNodes = append(cmdNodes, core.View{
			Style: core.Style{
				Background:   p.Removed.Fill,
				Padding:      core.Symmetric(2, 8),
				CornerRadius: 4,
			},
			Children: []core.Node{
				core.Text{Content: f.ErrorLine, Style: core.Style{FontSize: 11.5, Color: p.Removed.Fg}},
			},
		})
	}
	if f.ExitLine != "" {
		cmdNodes = append(cmdNodes, core.Text{
			Content: f.ExitLine,
			Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
		})
	}

	errorCard := core.View{
		TestID: "build-failure-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.Removed.Edge,
			Borders:      core.Edges{Left: 3, Top: 1, Right: 1, Bottom: 1},
			Padding:      core.Symmetric(14, 16),
			Gap:          10,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					buildSpecIcon(lucide.CircleAlert(), 15, p.Removed.Fg),
					core.Text{
						TestID:  "build-failure-headline",
						Content: f.Headline,
						Style:   core.Style{FontSize: 13, FontWeight: core.FontWeightMedium, Color: p.TextPrimary},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{
						TestID:  "build-failure-step",
						Content: stepMeta,
						Style:   core.Style{FontSize: 12, Color: p.TextFaint},
					},
				},
			},
			core.View{
				TestID: "build-failure-commands",
				Style: core.Style{
					CornerRadius: 8,
					Background:   p.Bg,
					Padding:      core.Symmetric(8, 12),
					Gap:          4,
				},
				Children: cmdNodes,
			},
			core.Text{
				TestID:  "build-failure-notice",
				Content: notice,
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
		},
	}

	children := []core.Node{errorCard}

	if prop := f.FixProposal; prop != nil {
		diffNodes := make([]core.Node, 0, len(prop.Diff))
		for _, dl := range prop.Diff {
			var lineBg core.Color
			lineFg := p.TextMuted
			prefix := " "
			switch dl.Kind {
			case "remove":
				lineBg = p.Removed.Fill
				lineFg = p.Removed.Fg
				prefix = "-"
			case "add":
				lineBg = p.Added.Fill
				lineFg = p.Added.Fg
				prefix = "+"
			}
			lineNumStr := ""
			if dl.LineNumber > 0 {
				lineNumStr = fmt.Sprintf("%d", dl.LineNumber)
			}
			diffNodes = append(diffNodes, core.View{
				Style: core.Style{
					Direction:  core.Row,
					Gap:        6,
					Padding:    core.Symmetric(2, 12),
					Background: lineBg,
				},
				Children: []core.Node{
					core.Text{Content: lineNumStr, Style: core.Style{Width: core.Pt(24), FontSize: 11.5, Color: p.TextFaint, FontVariant: core.FontTabularNums}},
					core.Text{Content: prefix, Style: core.Style{Width: core.Pt(12), FontSize: 11.5, Color: lineFg}},
					core.Text{Content: dl.Text, Style: core.Style{FontSize: 11.5, Color: lineFg}},
				},
			})
		}
		plat := entry.Platform
		footnote := prop.Footnote
		if footnote == "" && prop.FilePath != "" {
			footnote = ed.T("buildRelease.fixEditsFile", l10n.Param("file", prop.FilePath))
		}
		applyRebuildLabel := ed.T("buildRelease.applyAndRebuild", l10n.Param("name", entry.Name))
		fixCard := core.View{
			TestID: "build-fix-card",
			Style: core.Style{
				CornerRadius: 12,
				Background:   ui.Alpha(p.Agent.Fg, 0.06),
				BorderWidth:  1,
				BorderColor:  p.Agent.Edge,
				Padding:      core.Symmetric(14, 16),
				Gap:          12,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						buildSpecIcon(lucide.Sparkles(), 14, p.Agent.Fg),
						core.Text{
							TestID:  "build-fix-title",
							Content: prop.Title,
							Style:   core.Style{FontSize: 13, FontWeight: core.FontWeightMedium, Color: p.TextPrimary},
						},
					},
				},
				core.Text{
					Content: prop.Summary,
					Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
				},
				core.View{
					TestID: "build-fix-diff",
					Style: core.Style{
						CornerRadius: 8,
						Background:   p.Bg,
						Padding:      core.Symmetric(8, 0),
						Gap:          1,
					},
					Children: diffNodes,
				},
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						core.View{
							TestID: "build-apply-fix",
							Role:   core.RoleButton,
							Label:  applyRebuildLabel,
							Style: core.Style{
								Height:         core.Pt(32),
								Padding:        core.Symmetric(0, 14),
								CornerRadius:   8,
								Background:     p.Accent.Fg,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.report(ed.ApplyBuildPresetFixAndRebuild(plat))
							},
							Children: []core.Node{
								core.Text{
									Content: applyRebuildLabel,
									Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightMedium, Color: p.Accent.OnFill},
								},
							},
						},
						buildReleaseSecondaryButtonSpec(p, "build-whole-log", ed.T("buildRelease.wholeLog"), func() {
							ed.SetReleaseShowWholeLog(!showWholeLog)
						}),
						core.View{Style: core.Style{FlexGrow: 1}},
						core.Text{
							Content: footnote,
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
					},
				},
			},
		}
		children = append(children, fixCard)
	}

	if showWholeLog && len(entry.LogLines) > 0 {
		children = append(children, core.View{
			TestID: "build-whole-log-view",
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Bg,
				BorderWidth:  1,
				BorderColor:  p.Line,
				Padding:      core.All(12),
			},
			Children: []core.Node{
				core.Text{
					Content: strings.Join(entry.LogLines, "\n"),
					Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
				},
			},
		})
	}

	return ui.ScrollView{
		TestID:       "build-detail-failed",
		Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.All(20)},
		ContentStyle: core.Style{Gap: 16},
		EdgeColor:    func() core.Color { return p.Panel },
		Children:     children,
	}
}

func buildDetailDoneView(cx *core.Scope, ed *Editor, snap BuildReleaseSnapshot, p BuildPresetEntry) core.Node {
	res := p.Result
	if res == nil {
		return core.View{TestID: "build-detail-done"}
	}
	spec := ed.Palette(cx)

	artifactName := res.ArtifactName
	sizeText := ed.T("buildRelease.notMeasured")
	deltaColor := spec.TextFaint
	deltaText := ed.T("buildRelease.deltaNotMeasured")
	var artifactBodyChildren []core.Node

	if res.SizeMeasured {
		if artifactName == "" {
			artifactName = p.Name
		}
		sizeText = formatSizeMB(res.SizeBytes)
		deltaText = ed.T("buildRelease.deltaNew")
		if res.HasLastRelease {
			deltaText = formatDeltaMB(res.DeltaBytes)
			if res.DeltaBytes > 0 {
				deltaColor = spec.Changed.Fg
			} else if res.DeltaBytes < 0 {
				deltaColor = spec.Added.Fg
			}
		}

		totalBytes := res.Breakdown.CodeBytes + res.Breakdown.TexturesBytes + res.Breakdown.AudioBytes + res.Breakdown.OtherBytes
		if totalBytes <= 0 {
			totalBytes = max(res.SizeBytes, 1)
		}
		type part struct {
			label string
			bytes int64
			color core.Color
		}
		parts := []part{
			{ed.T("buildRelease.breakdownCode"), res.Breakdown.CodeBytes, spec.Accent.Fg},
			{ed.T("buildRelease.breakdownTextures"), res.Breakdown.TexturesBytes, spec.Changed.Fg},
			{ed.T("buildRelease.breakdownAudio"), res.Breakdown.AudioBytes, spec.Added.Fg},
			{ed.T("buildRelease.breakdownOther"), res.Breakdown.OtherBytes, spec.TextFaint},
		}

		var barSegs []core.Node
		var legendCols []core.Node
		for _, pt := range parts {
			flex := float32(pt.bytes) / float32(totalBytes)
			if flex < 0.02 && pt.bytes > 0 {
				flex = 0.02
			}
			barSegs = append(barSegs, core.View{
				Style: core.Style{
					Height:     core.Pt(10),
					FlexGrow:   flex,
					FlexBasis:  core.Pt(0),
					Background: pt.color,
				},
			})
			legendCols = append(legendCols, core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							core.View{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8), CornerRadius: 2, Background: pt.color}},
							core.Text{Content: pt.label, Style: core.Style{FontSize: 12, Color: spec.TextMuted}},
						},
					},
					core.Text{
						Content: formatSizeMB(pt.bytes),
						Style:   core.Style{FontSize: 11.5, Color: spec.TextSecondary, Padding: core.Edges{Left: 14}, FontVariant: core.FontTabularNums},
					},
				},
			})
		}

		growthText := res.Breakdown.GrowthSummary
		if res.WasmGzipBytes > 0 {
			if growthText != "" {
				growthText += " "
			}
			growthText += ed.T("buildRelease.wasmGzip", l10n.Param("size", formatSizeMB(res.WasmGzipBytes)))
		}
		artifactBodyChildren = []core.Node{
			core.View{
				Style:    core.Style{Direction: core.Row, Gap: 2, Height: core.Pt(10), CornerRadius: 5, Overflow: core.Hidden},
				Children: barSegs,
			},
			core.View{
				Style:    core.Style{Direction: core.Row, Gap: 8},
				Children: legendCols,
			},
			core.Text{
				TestID:  "build-result-growth",
				Content: growthText,
				Style:   core.Style{FontSize: 12, Color: spec.TextFaint},
			},
		}
	} else {
		if artifactName == "" {
			artifactName = ed.T("buildRelease.noBuiltPackage")
		}
		growthText := res.Breakdown.GrowthSummary
		if growthText == "" {
			growthText = ed.T("buildRelease.growthNotMeasured")
		}
		artifactBodyChildren = []core.Node{
			core.Text{
				TestID:  "build-result-growth",
				Content: growthText,
				Style:   core.Style{FontSize: 12, Color: spec.TextFaint},
			},
		}
	}

	artifactCard := core.View{
		TestID: "build-result-artifact",
		Style: core.Style{
			CornerRadius: 12,
			Background:   spec.Raised,
			BorderWidth:  1,
			BorderColor:  spec.LineRaised,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Height:      core.Pt(44),
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         10,
					Padding:     core.Symmetric(0, 14),
					Borders:     core.Edges{Bottom: 1},
					BorderColor: spec.Line,
				},
				Children: []core.Node{
					buildSpecIcon(lucide.Package(), 15, spec.TextMuted),
					core.Text{
						TestID:  "build-result-artifact-name",
						Content: artifactName,
						Style:   core.Style{FontSize: 12, Color: spec.TextPrimary},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{
						TestID:  "build-result-size",
						Content: sizeText,
						Style:   core.Style{FontSize: 12, Color: spec.TextPrimary, FontVariant: core.FontTabularNums},
					},
					core.Text{
						TestID:  "build-result-delta",
						Content: deltaText,
						Style:   core.Style{FontSize: 11.5, Color: deltaColor, FontVariant: core.FontTabularNums},
					},
				},
			},
			core.View{
				Style:    core.Style{Padding: core.Symmetric(12, 14), Gap: 10},
				Children: artifactBodyChildren,
			},
		},
	}

	dlBudget := snap.Preflight.DownloadSizeBudgetMB
	if dlBudget <= 0 || math.IsNaN(dlBudget) || math.IsInf(dlBudget, 0) {
		dlBudget = DefaultDownloadSizeBudgetMB
	}
	frameBudget := snap.Preflight.FrameTimeP95BudgetMs
	if frameBudget <= 0 || math.IsNaN(frameBudget) || math.IsInf(frameBudget, 0) {
		frameBudget = DefaultFrameTimeP95BudgetMs
	}

	sizeMetric := ed.T("buildRelease.budgetNotMeasuredLimit", l10n.Param("limit", fmt.Sprintf("%.0f MB", dlBudget)))
	var sizePct float32
	if res.SizeMeasured {
		sizeMetric = fmt.Sprintf("%s / %.0f MB", formatSizeMB(res.SizeBytes), dlBudget)
		sizePct = clampBudgetPct((float64(res.SizeBytes) / (1024 * 1024) / dlBudget) * 100)
	}

	frameMetric := ed.T("buildRelease.budgetNotMeasuredLimit", l10n.Param("limit", fmt.Sprintf("%.1f ms", frameBudget)))
	var framePct float32
	if res.FrameTimeMeasured {
		frameMetric = fmt.Sprintf("%.1f / %.1f ms", res.FrameTimeP95Ms, frameBudget)
		framePct = clampBudgetPct((res.FrameTimeP95Ms / frameBudget) * 100)
	}

	budgetsRow := core.View{
		TestID: "build-result-budgets",
		Style:  core.Style{Direction: core.Row, Gap: 12},
		Children: []core.Node{
			buildBudgetCard(spec, ed, "build-budget-download", ed.T("buildRelease.budgetDownloadSize"),
				sizeMetric, res.SizeMeasured, res.DownloadBudgetMet, sizePct),
			buildBudgetCard(spec, ed, "build-budget-frame", ed.T("buildRelease.budgetFrameTime"),
				frameMetric, res.FrameTimeMeasured, res.FrameTimeBudgetMet, framePct),
		},
	}

	playtestIcon := lucide.Clock()
	playtestIconColor := spec.TextFaint
	playtestTitle := ed.T("buildRelease.smokePlaytestNotRun")
	playtestWhere := res.SmokePlaytestWhere
	playtestSummary := res.SmokePlaytestSummary
	if res.SmokePlaytestRan {
		playtestIcon = lucide.Check()
		playtestIconColor = spec.Added.Fg
		playtestTitle = ed.T("buildRelease.preflightSmokeTitle")
	} else {
		if playtestWhere == "" || playtestWhere == "not run" {
			playtestWhere = ed.T("buildRelease.notRun")
		}
		if playtestSummary == "" {
			playtestSummary = ed.T("buildRelease.smokePlaytestNotRunSummary")
		}
	}

	playtestCard := core.View{
		TestID: "build-result-playtest",
		Style: core.Style{
			CornerRadius: 12,
			Background:   spec.Raised,
			BorderWidth:  1,
			BorderColor:  spec.LineRaised,
			Padding:      core.Symmetric(12, 14),
			Gap:          8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					buildSpecIcon(playtestIcon, 13, playtestIconColor),
					core.Text{TestID: "build-result-playtest-title", Content: playtestTitle, Style: core.Style{FontSize: 12.5, Color: spec.TextPrimary}},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{TestID: "build-result-playtest-where", Content: playtestWhere, Style: core.Style{FontSize: 11.5, Color: spec.TextFaint}},
				},
			},
			core.Text{
				TestID:  "build-result-playtest-summary",
				Content: playtestSummary,
				Style:   core.Style{FontSize: 12, Color: spec.TextMuted},
			},
		},
	}

	// Right side rail: QR card (for web builds) + Run on Device list.
	var rightRailChildren []core.Node
	qrURL := res.TestURL
	if snap.WebServer.Serving && snap.WebServer.URL != "" {
		qrURL = snap.WebServer.URL
	}
	showQRCard := qrURL != "" || (p.Platform == "web" && (snap.WebServer.StoppedByUser || snap.WebServer.ServeDir != ""))
	if showQRCard {
		rightRailChildren = append(rightRailChildren, buildWebQRCardView(ed, spec, snap.WebServer, qrURL))
	}

	deviceRows := make([]core.Node, 0, len(snap.Devices))
	for _, dev := range snap.Devices {
		d := dev
		isSel := d.ID == snap.SelectedDeviceID
		var dBg core.Color
		var dBorderW core.Points
		var dBorder core.Color
		dFg := spec.TextPrimary
		dotCol := deviceStateDotColor(spec, d.State, d.Available)
		if !d.Available && d.State != BuildDeviceInstalling {
			dFg = spec.TextMuted
		}
		if isSel {
			dBg = spec.Accent.Fill
			dBorderW = 1
			dBorder = spec.Accent.Edge
			dFg = spec.TextPrimary
		}
		deviceRows = append(deviceRows, core.View{
			TestID: "build-device-" + d.ID,
			Role:   core.RoleButton,
			Label:  d.Name + " · " + d.Note,
			Style: core.Style{
				Height:       core.Pt(32),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 6,
				Background:   dBg,
				BorderWidth:  dBorderW,
				BorderColor:  dBorder,
				FocusVisible: spec.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				if d.Actionable {
					ed.report(ed.LaunchBuildOnDevice(d.ID))
				} else if d.Available {
					ed.SelectBuildDevice(d.ID)
				}
			},
			Children: []core.Node{
				core.View{
					TestID: "build-device-dot-" + d.ID,
					Style:  core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: dotCol},
				},
				core.Text{Content: d.Name, Style: core.Style{FontSize: 12, Color: dFg, FlexGrow: 1}},
				core.Text{
					TestID:  "build-device-note-" + d.ID,
					Content: d.Note,
					Style:   core.Style{FontSize: 11, Color: spec.TextMuted},
				},
			},
		})
	}

	rightRailChildren = append(rightRailChildren, core.View{
		Style: core.Style{
			CornerRadius: 12,
			Background:   spec.Raised,
			BorderWidth:  1,
			BorderColor:  spec.LineRaised,
			Padding:      core.All(8),
			Gap:          2,
		},
		Children: append([]core.Node{
			core.View{
				Style: core.Style{Height: core.Pt(26), JustifyContent: core.JustifyCenter, Padding: core.Symmetric(0, 8)},
				Children: []core.Node{
					core.Text{
						Content: ed.T("buildRelease.runOnDevice"),
						Style:   core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: spec.TextMuted},
					},
				},
			},
		}, deviceRows...),
	})

	return ui.ScrollView{
		TestID:       "build-detail-done",
		Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.All(20)},
		ContentStyle: core.Style{Gap: 16},
		EdgeColor:    func() core.Color { return spec.Panel },
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, Gap: 16},
				Children: []core.Node{
					core.View{
						Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 12},
						Children: []core.Node{artifactCard, budgetsRow, playtestCard},
					},
					core.View{
						Style:    core.Style{Width: core.Pt(268), FlexShrink: 0, Gap: 12},
						Children: rightRailChildren,
					},
				},
			},
		},
	}
}

func clampBudgetPct(v float64) float32 {
	if math.IsNaN(v) || math.IsInf(v, -1) || v < 4 {
		return 4
	}
	if math.IsInf(v, 1) || v > 100 {
		return 100
	}
	return float32(v)
}

func buildBudgetCard(p theme.Palette, ed *Editor, testID, title, metric string, measured, met bool, pct float32) core.Node {
	badgeText := ed.T("buildRelease.notMeasured")
	badgeBg := p.Panel
	badgeBorder := p.LineRaised
	badgeFg := p.TextFaint
	var barChildren []core.Node
	if measured {
		badgeText = ed.T("buildRelease.budgetMet")
		badgeBg = p.Added.Fill
		badgeBorder = p.Added.Edge
		badgeFg = p.Added.Fg
		barColor := p.Added.Fg
		if !met {
			badgeText = ed.T("buildRelease.budgetMissed")
			badgeBg = p.Removed.Fill
			badgeBorder = p.Removed.Edge
			badgeFg = p.Removed.Fg
			barColor = p.Removed.Fg
		}
		barChildren = []core.Node{
			core.View{Style: core.Style{Height: core.Pt(6), Width: core.Pct(pct), CornerRadius: 3, Background: barColor}},
		}
	}
	return core.View{
		TestID: testID,
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.Symmetric(12, 14),
			Gap:          8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 6},
				Children: []core.Node{
					core.Text{Content: title, Style: core.Style{FontSize: 12, Color: p.TextMuted}},
					core.View{
						Style: core.Style{
							Height:         core.Pt(18),
							Padding:        core.Symmetric(0, 6),
							CornerRadius:   9,
							Background:     badgeBg,
							BorderWidth:    1,
							BorderColor:    badgeBorder,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{TestID: testID + "-badge", Content: badgeText, Style: core.Style{FontSize: 10.5, FontWeight: core.FontWeightMedium, Color: badgeFg}},
						},
					},
				},
			},
			core.Text{TestID: testID + "-metric", Content: metric, Style: core.Style{FontSize: 12, Color: p.TextPrimary, FontVariant: core.FontTabularNums}},
			core.View{
				Style:    core.Style{Height: core.Pt(6), CornerRadius: 3, Background: p.LineRaised, Overflow: core.Hidden},
				Children: barChildren,
			},
		},
	}
}

func joinNaturalList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

func buildDetailReadinessView(p theme.Palette, ed *Editor, entry BuildPresetEntry) core.Node {
	_, totalIssues := countReadinessIssues(entry.Readiness)
	heading := ed.T("buildRelease.readinessToFix", l10n.Param("count", totalIssues), l10n.Param("name", entry.Name))
	if totalIssues == 0 {
		heading = ed.T("buildRelease.readinessReady", l10n.Param("name", entry.Name))
	}

	plat := entry.Platform
	var itemRows []core.Node
	var agentItems, personItems []string
	for i, item := range entry.Readiness {
		it := item
		if it.Status != BuildReadinessOK {
			if it.AgentCanHandle {
				agentItems = append(agentItems, strings.ToLower(it.Label))
			} else if it.RequiresPerson {
				personItems = append(personItems, strings.ToLower(it.Label))
			}
		}
		var icon core.Node
		switch it.Status {
		case BuildReadinessOK:
			icon = buildSpecIcon(lucide.Check(), 14, p.Added.Fg)
		case BuildReadinessFail:
			icon = buildSpecIcon(lucide.CircleAlert(), 14, p.Removed.Fg)
		default:
			icon = buildSpecIcon(lucide.TriangleAlert(), 14, p.Warning.Fg)
		}

		rowChildren := []core.Node{
			icon,
			core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
				Children: []core.Node{
					core.Text{Content: it.Label, Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
					core.Text{Content: it.Detail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
				},
			},
		}
		if it.ActionLabel != "" {
			itemID := it.ID
			rowChildren = append(rowChildren, buildReleaseSecondaryButtonSpec(
				p,
				"build-readiness-action-"+itemID,
				it.ActionLabel,
				func() {
					ed.report(ed.RunBuildReadinessAction(plat, itemID))
				},
			))
		}
		borderBottom := core.Points(1)
		if i == len(entry.Readiness)-1 {
			borderBottom = 0
		}
		itemRows = append(itemRows, core.View{
			TestID: "build-readiness-item-" + it.ID,
			Style: core.Style{
				MinHeight:   core.Pt(48),
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         12,
				Padding:     core.Symmetric(8, 14),
				Borders:     core.Edges{Bottom: borderBottom},
				BorderColor: p.Line,
			},
			Children: rowChildren,
		})
	}

	guidance := ed.T("buildRelease.readinessGuidanceDefault", l10n.Param("name", entry.Name))
	if len(agentItems) > 0 && len(personItems) > 0 {
		key := "buildRelease.readinessGuidanceBoth"
		if len(personItems) > 1 {
			key = "buildRelease.readinessGuidanceBothPlural"
		}
		guidance = ed.T(key,
			l10n.Param("agentItems", joinNaturalList(agentItems)),
			l10n.Param("personItems", capitalizeProfile(joinNaturalList(personItems))),
		)
	} else if len(agentItems) > 0 {
		guidance = ed.T("buildRelease.readinessGuidanceAgent", l10n.Param("agentItems", joinNaturalList(agentItems)))
	} else if len(personItems) > 0 {
		key := "buildRelease.readinessGuidancePerson"
		if len(personItems) > 1 {
			key = "buildRelease.readinessGuidancePersonPlural"
		}
		guidance = ed.T(key,
			l10n.Param("personItems", capitalizeProfile(joinNaturalList(personItems))),
			l10n.Param("name", entry.Name),
		)
	}
	setupAgentLabel := ed.T("buildRelease.setupWithAgent", l10n.Param("name", entry.Name))

	return ui.ScrollView{
		TestID:       "build-detail-readiness",
		Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.All(20)},
		ContentStyle: core.Style{Gap: 16},
		EdgeColor:    func() core.Color { return p.Panel },
		Children: []core.Node{
			core.Text{
				TestID:  "build-readiness-heading",
				Content: heading,
				Style:   core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: p.TextFaint},
			},
			core.View{
				TestID: "build-readiness-list",
				Style: core.Style{
					CornerRadius: 12,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Overflow:     core.Hidden,
				},
				Children: itemRows,
			},
			core.View{
				TestID: "build-readiness-agent-card",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          12,
					Padding:      core.Symmetric(12, 14),
					CornerRadius: 12,
					Background:   ui.Alpha(p.Agent.Fg, 0.06),
					BorderWidth:  1,
					BorderColor:  p.Agent.Edge,
				},
				Children: []core.Node{
					buildSpecIcon(lucide.Sparkles(), 14, p.Agent.Fg),
					core.Text{
						TestID:  "build-readiness-guidance",
						Content: guidance,
						Style:   core.Style{FontSize: 12, Color: p.TextSecondary, FlexGrow: 1, FlexBasis: core.Pt(0)},
					},
					core.View{
						TestID: "build-readiness-agent-setup",
						Role:   core.RoleButton,
						Label:  setupAgentLabel,
						Style: core.Style{
							Height:         core.Pt(32),
							FlexShrink:     0,
							Padding:        core.Symmetric(0, 14),
							CornerRadius:   8,
							Background:     p.Accent.Fg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.report(ed.RunBuildReadinessAgentSetup(plat))
						},
						Children: []core.Node{
							core.Text{
								Content: setupAgentLabel,
								Style:   core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: p.Accent.OnFill},
							},
						},
					},
				},
			},
		},
	}
}

func buildDetailQueuedView(p theme.Palette, ed *Editor, snap BuildReleaseSnapshot, entry BuildPresetEntry) core.Node {
	title := ed.T("buildRelease.queued")
	if snap.ActivePlatform != "" && snap.ActivePlatform != entry.Platform {
		title = ed.T("buildRelease.queuedAfter", l10n.Param("name", platformDisplayName(snap.ActivePlatform)))
	}
	subtitle := ed.T("buildRelease.queuedSubtitle")
	actionLabel := ed.T("buildRelease.buildNext", l10n.Param("name", entry.Name))
	actionID := "build-queued-next"
	plat := entry.Platform
	onAct := func() {
		ed.PrioritizeBuildPreset(plat)
	}

	if snap.Stopped || entry.StoppedAt {
		title = ed.T("buildRelease.presetStopped", l10n.Param("name", entry.Name))
		subtitle = ed.T("buildRelease.stoppedSubtitle")
		actionLabel = ed.T("buildRelease.actionResume")
		actionID = "build-queued-resume"
		onAct = func() {
			ed.report(ed.ResumeReleaseBuild())
		}
	}

	children := []core.Node{
		core.View{
			Style: core.Style{
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Padding:        core.Symmetric(28, 20),
				Gap:            10,
				CornerRadius:   12,
				Background:     p.Raised,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
			},
			Children: []core.Node{
				core.Text{TestID: "build-queued-title", Content: title, Style: core.Style{FontSize: 13.5, FontWeight: core.FontWeightMedium, Color: p.TextPrimary}},
				core.Text{TestID: "build-queued-subtitle", Content: subtitle, Style: core.Style{FontSize: 12, Color: p.TextFaint}},
				buildReleaseSecondaryButtonSpec(p, actionID, actionLabel, onAct),
			},
		},
	}

	// Show ordered steps (highlighting completed steps when stopped mid-build).
	if len(entry.Steps) > 0 {
		stepRows := make([]core.Node, 0, len(entry.Steps))
		for _, st := range entry.Steps {
			stepRows = append(stepRows, buildStepRowView(p, ed, st))
		}
		children = append(children, core.View{
			TestID:   "build-queued-steps",
			Style:    core.Style{Gap: 2},
			Children: stepRows,
		})
	}

	return ui.ScrollView{
		TestID:       "build-detail-queued",
		Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Padding: core.All(20)},
		ContentStyle: core.Style{Gap: 16},
		EdgeColor:    func() core.Color { return p.Panel },
		Children:     children,
	}
}

func buildWebQRCardView(ed *Editor, spec theme.Palette, ws WebBuildServerSnapshot, qrURL string) core.Node {
	statusLabel := ed.T("buildRelease.lanTestLink")
	statusDot := spec.Added.Fg
	statusFg := spec.TextSecondary
	statusBg := spec.Added.Fill
	statusBorder := spec.Added.Edge
	switch {
	case ws.Serving && len(ws.ConnectedClients) > 0:
		c := ws.ConnectedClients[len(ws.ConnectedClients)-1]
		statusLabel = ed.T("buildRelease.mcpBridgeDevice", l10n.Param("device", c.Device))
		statusDot = spec.Agent.Fg
		statusFg = spec.TextPrimary
		statusBg = spec.Agent.Fill
		statusBorder = spec.Agent.Edge
	case ws.Serving:
		statusLabel = ed.T("buildRelease.servingOnLan")
		statusDot = spec.Added.Fg
		statusFg = spec.TextPrimary
		statusBg = spec.Added.Fill
		statusBorder = spec.Added.Edge
	case ws.StoppedByUser:
		statusLabel = ed.T("buildRelease.stopped")
		statusDot = spec.TextMuted
		statusFg = spec.TextMuted
		statusBg = spec.Panel
		statusBorder = spec.LineRaised
	}

	var qrNode core.Node
	if !ws.StoppedByUser && qrURL != "" {
		code, err := EncodeBuildQR(qrURL)
		if err == nil && code != nil {
			qrBorderW := core.Points(0)
			var qrBorderCol core.Color
			if !spec.Dark {
				qrBorderW = 1
				qrBorderCol = spec.LineRaised
			}
			qrLabel := ed.T("buildRelease.qrCodeLabel", l10n.Param("url", qrURL))
			qrNode = core.View{
				Role:  core.RoleImage,
				Label: qrLabel,
				Style: core.Style{
					Width:        core.Pt(160),
					Height:       core.Pt(160),
					CornerRadius: 8,
					Background:   theme.Light().Raised,
					BorderWidth:  qrBorderW,
					BorderColor:  qrBorderCol,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "build-qr-canvas",
						Label:  qrLabel,
						Style: core.Style{
							Width:  core.Pt(160),
							Height: core.Pt(160),
						},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawQRCode(d, sz, code)
						},
					},
				},
			}
		}
	}
	if qrNode == nil {
		qrNode = core.View{
			TestID: "build-qr-stopped-placeholder",
			Style: core.Style{
				Width:          core.Pt(160),
				Height:         core.Pt(160),
				CornerRadius:   8,
				Background:     spec.Panel,
				BorderWidth:    1,
				BorderColor:    spec.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Padding:        core.All(12),
				Gap:            6,
			},
			Children: []core.Node{
				buildSpecIcon(lucide.WifiOff(), 20, spec.TextMuted),
				core.Text{
					Content: ed.T("buildRelease.serverStopped"),
					Style:   core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: spec.TextSecondary},
				},
			},
		}
	}

	children := []core.Node{
		core.View{
			TestID: "build-qr-status",
			Style: core.Style{
				Height:       core.Pt(22),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 11,
				Background:   statusBg,
				BorderWidth:  1,
				BorderColor:  statusBorder,
			},
			Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: statusDot}},
				core.Text{Content: statusLabel, Style: core.Style{FontSize: 11, Color: statusFg}},
			},
		},
		qrNode,
	}

	if !ws.StoppedByUser && qrURL != "" {
		children = append(children, core.Text{
			TestID:  "build-qr-url",
			Content: qrURL,
			Style:   core.Style{FontSize: 11.5, Color: spec.TextPrimary, FontVariant: core.FontTabularNums},
		})
	}

	caption := ed.T("buildRelease.qrCaptionServing")
	if ws.StoppedByUser {
		caption = ed.T("buildRelease.qrCaptionStopped")
	}
	children = append(children, core.Text{
		TestID:  "build-qr-caption",
		Content: caption,
		Style:   core.Style{FontSize: 11.5, Color: spec.TextMuted},
	})

	var actions []core.Node
	if !ws.StoppedByUser && qrURL != "" {
		actions = append(actions, buildReleaseSecondaryButtonSpec(spec, "build-qr-copy-url", ed.T("buildRelease.copyURL"), func() {
			_, err := ed.CopyWebBuildURL()
			ed.report(err)
		}))
	}
	if ws.Serving {
		actions = append(actions, buildReleaseSecondaryButtonSpec(spec, "build-qr-toggle-serve", ed.T("buildRelease.stopServing"), func() {
			ed.report(ed.StopWebBuildServer())
		}))
	} else if ws.ServeDir != "" {
		actions = append(actions, buildReleaseSecondaryButtonSpec(spec, "build-qr-toggle-serve", ed.T("buildRelease.startServing"), func() {
			_, err := ed.StartWebBuildServer("")
			ed.report(err)
		}))
	}
	if len(actions) > 0 {
		children = append(children, core.View{
			Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 8},
			Children: actions,
		})
	}

	return core.View{
		TestID: "build-result-qr",
		Style: core.Style{
			CornerRadius: 12,
			Background:   spec.Raised,
			BorderWidth:  1,
			BorderColor:  spec.LineRaised,
			Padding:      core.All(14),
			Gap:          10,
			AlignItems:   core.AlignCenter,
		},
		Children: children,
	}
}

// drawQRCode renders code on a high-contrast light quiet-zone card (#FFFFFF quiet zone,
// #101216 dark modules — 18.75:1 AAA contrast) so phone cameras scan reliably in Dark and Light themes.
func drawQRCode(d *core.Drawing, sz core.Size, code *qrcode.Code) {
	w, h := float64(sz.Width), float64(sz.Height)
	if d == nil || code == nil || code.Size <= 0 || math.IsNaN(w) || math.IsInf(w, 0) || math.IsNaN(h) || math.IsInf(h, 0) || w <= 0 || h <= 0 {
		return
	}
	d.SetFillColor(theme.Light().Raised).FillRect(0, 0, float32(w), float32(h))

	qz := code.QuietZone()
	totalModules := code.Size + 2*qz
	side := min(w, h)
	cell := math.Floor(side / float64(totalModules))
	if cell < 1 {
		cell = side / float64(totalModules)
	}
	gridSide := cell * float64(totalModules)
	ox := math.Floor((w - gridSide) / 2)
	oy := math.Floor((h - gridSide) / 2)

	d.SetFillColor(theme.Dark().Bg)
	cellF := float32(cell)
	for y := range code.Size {
		py := float32(oy + float64(qz+y)*cell)
		for x := range code.Size {
			if code.Black(x, y) {
				px := float32(ox + float64(qz+x)*cell)
				d.FillRect(px, py, cellF, cellF)
			}
		}
	}
}

func deviceStateDotColor(spec theme.Palette, st BuildDeviceState, available bool) core.Color {
	switch st {
	case BuildDeviceReady, BuildDeviceServing, BuildDeviceRunning:
		return spec.Added.Fg
	case BuildDeviceConnected, BuildDeviceInstalling:
		return spec.Agent.Fg
	case BuildDeviceFailed:
		return spec.Removed.Fg
	case BuildDeviceStopped, BuildDeviceDisconnected, BuildDeviceUnavailable:
		return spec.TextMuted
	default:
		if available {
			return spec.Added.Fg
		}
		return spec.TextMuted
	}
}

func buildReleaseSecondaryButtonSpec(p theme.Palette, testID, label string, onPress func()) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  label,
		Style: core.Style{
			Height:         core.Pt(30),
			FlexShrink:     0,
			Padding:        core.Symmetric(0, 12),
			CornerRadius:   8,
			Background:     p.LineRaised,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Hover:          &core.Style{Background: p.Line},
			FocusVisible:   p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			onPress()
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
			},
		},
	}
}

func buildReleaseReviewUploadsOverlay(cx *core.Scope, ed *Editor, brRev *core.Memo[uint64]) core.Node {
	kcAccount := core.NewSignal(cx, "")
	kcSecret := core.NewSignal(cx, "")

	reviewOpen := core.NewMemo(cx, func() bool {
		_ = brRev.Get()
		return ed.BuildReleaseSnapshot().UploadReview.Open
	})
	keychainOpen := core.NewMemo(cx, func() bool {
		_ = brRev.Get()
		rev := ed.BuildReleaseSnapshot().UploadReview
		return rev.Open && rev.KeychainEditorOpen
	})

	// Scrub typed secret/account state whenever the keychain drawer or Review uploads closes
	// (including programmatic CloseReviewUploads or Play/project switch).
	cx.Effect(func() {
		open := reviewOpen.Get()
		kcOpen := keychainOpen.Get()
		cx.Untrack(func() {
			if !open {
				if kcSecret.GetUntracked() != "" {
					kcSecret.Set("")
				}
				if kcAccount.GetUntracked() != "" {
					kcAccount.Set("")
				}
			} else if !kcOpen {
				if kcSecret.GetUntracked() != "" {
					kcSecret.Set("")
				}
			}
		})
	})

	onDialogClose := bindDialogFocusRestore(cx, ed, reviewOpen.Get)

	p := ed.Palette(cx)

	headerNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = brRev.Get()
		cur := ed.Palette(cx)
		rev := ed.BuildReleaseSnapshot().UploadReview
		return core.View{
			Style: core.Style{
				Padding:     core.Edges{Top: 20, Left: 24, Right: 24, Bottom: 14},
				Gap:         6,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: cur.Line,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
					Children: []core.Node{
						core.Text{
							TestID:  "build-review-uploads-title",
							Content: ed.T("buildRelease.reviewUploadsTitle"),
							Style:   core.Style{FlexGrow: 1, FontSize: 14, FontWeight: core.FontWeightSemibold, Color: cur.TextPrimary},
						},
						core.View{
							TestID: "build-review-uploads-guard-badge",
							Style: core.Style{
								Height:       core.Pt(24),
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          6,
								Padding:      core.Symmetric(0, 10),
								CornerRadius: 12,
								Background:   cur.Warning.Fill,
								BorderWidth:  1,
								BorderColor:  cur.Warning.Edge,
							},
							Children: []core.Node{
								buildSpecIcon(lucide.Lock(), 12, cur.Warning.Fg),
								core.Text{
									Content: ed.T("buildRelease.alwaysAsksFirst"),
									Style:   core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: cur.Warning.Fg},
								},
							},
						},
					},
				},
				core.Text{
					Content: ed.T("buildRelease.reviewUploadsSubtitle"),
					Style:   core.Style{FontSize: 12, Color: cur.TextMuted},
				},
				core.Text{
					TestID:  "build-review-uploads-policy",
					Content: ed.T("buildRelease.reviewUploadsPolicy", l10n.Param("profile", rev.PermissionProfile.DisplayName())),
					Style:   core.Style{FontSize: 11.5, Color: cur.TextFaint},
				},
			},
		}
	})

	itemsAndSummaryNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = brRev.Get()
		cur := ed.Palette(cx)
		rev := ed.BuildReleaseSnapshot().UploadReview

		var rows []core.Node
		for i, it := range rev.Items {
			rows = append(rows, buildReviewUploadItemRow(cur, ed, it, rev.Running, i == len(rev.Items)-1))
		}
		if len(rows) == 0 {
			rows = append(rows, core.View{
				Style: core.Style{Padding: core.All(16), AlignItems: core.AlignCenter},
				Children: []core.Node{
					core.Text{
						Content: ed.T("buildRelease.noUploadsReady"),
						Style:   core.Style{FontSize: 12, Color: cur.TextFaint},
					},
				},
			})
		}

		tableNode := core.View{
			TestID: "build-review-uploads-list",
			Style: core.Style{
				CornerRadius: 12,
				Background:   cur.Bg,
				BorderWidth:  1,
				BorderColor:  cur.Line,
				Overflow:     core.Hidden,
			},
			Children: append([]core.Node{
				core.View{
					Style: core.Style{
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Gap:         10,
						Padding:     core.Symmetric(8, 12),
						Borders:     core.Edges{Bottom: 1},
						BorderColor: cur.Line,
					},
					Children: []core.Node{
						core.View{Style: core.Style{Width: core.Pt(20), FlexShrink: 0}},
						core.Text{Content: ed.T("buildRelease.colArtifact"), Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: ed.T("buildRelease.colDestination"), Style: core.Style{Width: core.Pt(184), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: ed.T("buildRelease.colVersion"), Style: core.Style{Width: core.Pt(62), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: ed.T("buildRelease.colSize"), Style: core.Style{Width: core.Pt(68), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: ed.T("buildRelease.colKeychain"), Style: core.Style{Width: core.Pt(122), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
					},
				},
			}, rows...),
		}

		notesPreview := rev.ReleaseNotesPreview
		if notesPreview == "" {
			notesPreview = ed.T("buildRelease.notesEmpty")
		}
		children := []core.Node{
			tableNode,
			core.View{
				TestID: "build-review-uploads-notes",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(8, 12),
					CornerRadius: 8,
					Background:   cur.Bg,
					BorderWidth:  1,
					BorderColor:  cur.Line,
				},
				Children: []core.Node{
					core.Text{Content: ed.T("buildRelease.notesPrefix"), Style: core.Style{FontSize: 11.5, Color: cur.TextFaint}},
					core.Text{Content: rev.ReleaseNotesFile, Style: core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: cur.TextSecondary}},
					core.Text{Content: "·", Style: core.Style{FontSize: 11.5, Color: cur.TextFaint}},
					core.Text{Content: notesPreview, Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), FontSize: 11.5, Color: cur.TextMuted}},
				},
			},
		}

		if rev.LastErr != "" {
			children = append(children, core.View{
				TestID: "build-review-uploads-error",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(8, 12),
					CornerRadius: 8,
					Background:   cur.Removed.Fill,
					BorderWidth:  1,
					BorderColor:  cur.Removed.Edge,
				},
				Children: []core.Node{
					buildSpecIcon(lucide.CircleAlert(), 14, cur.Removed.Fg),
					core.Text{Content: rev.LastErr, Style: core.Style{FontSize: 11.5, Color: cur.TextSecondary, FlexGrow: 1}},
				},
			})
		}

		return core.View{
			Style:    core.Style{Gap: 12},
			Children: children,
		}
	})

	// Keep keychain text inputs outside brRev-driven core.Dynamic so background build/upload
	// ticks never remount build-keychain-account or build-keychain-secret while typing.
	keychainDrawerNode := core.Show{
		When: keychainOpen.Get,
		Then: func(cx *core.Scope) core.Node {
			cur := ed.Palette(cx)
			destMemo := core.NewMemo(cx, func() PublishDestination {
				_ = brRev.Get()
				d := ed.BuildReleaseSnapshot().UploadReview.KeychainDest
				if !d.Valid() {
					return PublishDestItchIO
				}
				return d
			})
			return core.View{
				TestID: "build-keychain-drawer",
				Style: core.Style{
					CornerRadius: 12,
					Background:   cur.Bg,
					BorderWidth:  1,
					BorderColor:  cur.LineRaised,
					Padding:      core.All(12),
					Gap:          8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							buildSpecIcon(lucide.Lock(), 13, cur.Warning.Fg),
							core.Text{
								ContentFunc: func() string {
									return ed.T("buildRelease.keychainTitle", l10n.Param("dest", destMemo.Get().DisplayName()))
								},
								Style: core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: cur.TextPrimary, FlexGrow: 1},
							},
							core.Text{
								Content: ed.T("buildRelease.neverInProjectFiles"),
								Style:   core.Style{FontSize: 11, Color: cur.TextFaint},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								Children: []core.Node{
									specInputField(cx, ed, ui.Input{
										TestID:      "build-keychain-account",
										Label:       ed.T("buildRelease.keychainAccountLabel"),
										Placeholder: keychainAccountPlaceholder(ed.BuildReleaseSnapshot().UploadReview.KeychainDest),
										Value:       kcAccount.Get,
										OnChange:    kcAccount.Set,
										Style: core.Style{
											Width:        core.Pct(100),
											Height:       core.Pt(28),
											Background:   cur.Raised,
											BorderWidth:  1,
											BorderColor:  cur.LineRaised,
											CornerRadius: 6,
											Color:        cur.TextSecondary,
											FontSize:     12,
										},
									}),
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								Children: []core.Node{
									specInputField(cx, ed, ui.Input{
										TestID:      "build-keychain-secret",
										Label:       ed.T("buildRelease.keychainSecretLabel"),
										Placeholder: keychainSecretPlaceholder(ed.BuildReleaseSnapshot().UploadReview.KeychainDest),
										Masked:      true,
										Value:       kcSecret.Get,
										OnChange:    kcSecret.Set,
										Style: core.Style{
											Width:        core.Pct(100),
											Height:       core.Pt(28),
											Background:   cur.Raised,
											BorderWidth:  1,
											BorderColor:  cur.LineRaised,
											CornerRadius: 6,
											Color:        cur.TextSecondary,
											FontSize:     12,
										},
									}),
								},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyEnd, Gap: 8},
						Children: []core.Node{
							buildReleaseSecondaryButtonSpec(cur, "build-keychain-cancel", ed.T("buildRelease.cancel"), func() {
								kcSecret.Set("")
								ed.CloseKeychainCredentialEditor()
							}),
							core.View{
								TestID: "build-keychain-save",
								Role:   core.RoleButton,
								Label:  ed.T("buildRelease.saveToKeychain"),
								Style: core.Style{
									Height:         core.Pt(30),
									Padding:        core.Symmetric(0, 12),
									CornerRadius:   8,
									Background:     cur.Accent.Fg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FocusVisible:   cur.FocusRing(2),
								},
								OnPress: func(core.PressEvent) {
									dest := destMemo.Get()
									acc := kcAccount.Get()
									sec := kcSecret.Get()
									kcSecret.Set("")
									ed.report(ed.StorePublishCredential(dest, acc, sec))
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("buildRelease.saveToKeychain"),
										Style:   core.Style{FontSize: 12, FontWeight: core.FontWeightSemibold, Color: cur.Accent.OnFill},
									},
								},
							},
						},
					},
				},
			}
		},
	}

	footerNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = brRev.Get()
		cur := ed.Palette(cx)
		rev := ed.BuildReleaseSnapshot().UploadReview
		selectedCount := 0
		for _, it := range rev.Items {
			if it.Included {
				selectedCount++
			}
		}
		cancelLabel := ed.T("buildRelease.cancel")
		if rev.Completed {
			cancelLabel = ed.T("buildRelease.close")
		}
		confirmLabel := ed.T("buildRelease.confirmAndUpload", l10n.Param("count", selectedCount))
		if rev.Running {
			confirmLabel = ed.T("buildRelease.uploading")
		}
		canConfirm := !rev.Running && rev.SessionID != 0 && selectedCount > 0
		confirmBg := cur.Accent.Fg
		confirmFg := cur.Accent.OnFill
		if !canConfirm {
			confirmBg = cur.LineRaised
			confirmFg = cur.TextFaint
		}
		sessionID := rev.SessionID

		return core.View{
			Style: core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         8,
				Padding:     core.Edges{Top: 14, Left: 24, Right: 24, Bottom: 18},
				Borders:     core.Edges{Top: 1},
				BorderColor: cur.Line,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("buildRelease.keychainFooterNote"),
					Style:   core.Style{FlexGrow: 1, FontSize: 11.5, Color: cur.TextFaint},
				},
				buildReleaseSecondaryButtonSpec(cur, "build-review-uploads-cancel", cancelLabel, func() {
					if rev.Running {
						ed.CancelReleaseUpload()
					} else {
						kcSecret.Set("")
						ed.CloseReviewUploads()
					}
				}),
				core.View{
					TestID: "build-review-uploads-confirm",
					Role:   core.RoleButton,
					Label:  confirmLabel,
					Style: core.Style{
						Height:       core.Pt(32),
						FlexShrink:   0,
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          6,
						Padding:      core.Symmetric(0, 14),
						CornerRadius: 8,
						Background:   confirmBg,
						FocusVisible: cur.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						if canConfirm {
							ed.report(ed.ConfirmReleaseUploads(sessionID))
						}
					},
					Children: []core.Node{
						buildSpecIcon(lucide.Upload(), 13, confirmFg),
						core.Text{
							Content: confirmLabel,
							Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightSemibold, Color: confirmFg},
						},
					},
				},
			},
		}
	})

	dialogCard := core.View{
		TestID: "build-review-uploads-dialog",
		Role:   core.RoleDialog,
		Label:  ed.T("buildRelease.reviewUploadsTitle"),
		StyleFunc: func() core.Style {
			cur := ed.Palette(cx)
			return core.Style{
				Width:        core.Pt(800),
				MaxHeight:    core.Pt(760),
				CornerRadius: 18,
				Background:   cur.Raised,
				BorderWidth:  1,
				BorderColor:  cur.LineRaised,
				Overflow:     core.Hidden,
			}
		},
		Children: []core.Node{
			headerNode,
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MaxHeight: core.Pt(500), Padding: core.Symmetric(16, 24)},
				ContentStyle: core.Style{Gap: 12},
				EdgeColor:    func() core.Color { return ed.Palette(cx).Raised },
				Children: []core.Node{
					itemsAndSummaryNode,
					keychainDrawerNode,
				},
			},
			footerNode,
		},
	}

	return ui.Dialog{
		TestID: "build-review-uploads-backdrop",
		Bare:   true,
		Label:  ed.T("buildRelease.reviewUploadsTitle"),
		Open:   reviewOpen.Get,
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				kcSecret.Set("")
				if ed.ReleaseUploadRunning() {
					ed.CancelReleaseUpload()
				}
				ed.CloseReviewUploads()
			}
		},
		OnClose: onDialogClose,
		Style: core.Style{
			CornerRadius: 18,
			Background:   p.Raised,
		},
		Children: []core.Node{dialogCard},
	}
}

func keychainAccountPlaceholder(dest PublishDestination) string {
	switch dest {
	case PublishDestItchIO:
		return "itch.io user (optional)"
	case PublishDestGooglePlay:
		return "Service account label (optional)"
	case PublishDestTestFlight:
		return "Key ID:Issuer ID (or leave blank if in JSON)"
	default:
		return "Account or publisher name"
	}
}

func keychainSecretPlaceholder(dest PublishDestination) string {
	switch dest {
	case PublishDestItchIO:
		return "BUTLER_API_KEY"
	case PublishDestGooglePlay:
		return "Service account JSON or file path"
	case PublishDestTestFlight:
		return "App Store Connect .p8 key or JSON {key,key_id,issuer}"
	default:
		return "API key / token"
	}
}

func buildReviewUploadItemRow(p theme.Palette, ed *Editor, it ReleaseUploadItem, running, isLast bool) core.Node {
	plat := it.Platform
	dest := it.Destination
	manual := it.ManualOnly || !dest.Valid()

	var boxBg core.Color
	boxBorder := p.TextFaint
	var boxBorderW core.Points = 1.5
	var checkChildren []core.Node
	if it.Included && !manual {
		boxBg = p.Accent.Fg
		boxBorder = p.Accent.Fg
		boxBorderW = 0
		checkChildren = []core.Node{buildSpecIcon(lucide.Check(), 10, p.Accent.OnFill)}
	} else if manual {
		boxBorder = p.LineRaised
	}

	var statusNode core.Node
	switch {
	case manual:
		statusNode = core.View{
			TestID: "build-upload-manual-" + plat,
			Style: core.Style{
				Height:       core.Pt(22),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          5,
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 11,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.Text{Content: "SteamPipe", Style: core.Style{FontSize: 11, Color: p.TextFaint}},
			},
		}
	case it.Status == UploadStatusUploading:
		pct := int(math.Round(clampUploadFraction(it.Progress) * 100))
		statusNode = core.View{
			Style: core.Style{
				Height:       core.Pt(22),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          5,
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 11,
				Background:   p.Agent.Fill,
				BorderWidth:  1,
				BorderColor:  p.Agent.Edge,
			},
			Children: []core.Node{
				core.Text{Content: ed.T("buildRelease.uploadingPct", l10n.Param("pct", pct)), Style: core.Style{FontSize: 11, Color: p.TextSecondary}},
			},
		}
	case it.Status == UploadStatusDone:
		statusNode = core.View{
			Style: core.Style{
				Height:       core.Pt(22),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          5,
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 11,
				Background:   p.Added.Fill,
				BorderWidth:  1,
				BorderColor:  p.Added.Edge,
			},
			Children: []core.Node{
				buildSpecIcon(lucide.Check(), 10, p.Added.Fg),
				core.Text{Content: ed.T("buildRelease.uploaded"), Style: core.Style{FontSize: 11, Color: p.Added.Fg}},
			},
		}
	case it.Status == UploadStatusFailed:
		statusNode = core.View{
			Style: core.Style{
				Height:       core.Pt(22),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          5,
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 11,
				Background:   p.Removed.Fill,
				BorderWidth:  1,
				BorderColor:  p.Removed.Edge,
			},
			Children: []core.Node{
				buildSpecIcon(lucide.X(), 10, p.Removed.Fg),
				core.Text{Content: ed.T("buildRelease.failed"), Style: core.Style{FontSize: 11, Color: p.Removed.Fg}},
			},
		}
	default:
		if it.HasCredential {
			statusNode = core.View{
				TestID: "build-upload-key-ok-" + plat,
				Style: core.Style{
					Height:       core.Pt(22),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          5,
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 11,
					Background:   p.Added.Fill,
					BorderWidth:  1,
					BorderColor:  p.Added.Edge,
				},
				Children: []core.Node{
					buildSpecIcon(lucide.Lock(), 10, p.Added.Fg),
					core.Text{Content: ed.T("buildRelease.keyInKeychain"), Style: core.Style{FontSize: 11, Color: p.Added.Fg}},
				},
			}
		} else {
			statusNode = core.View{
				TestID: "build-upload-key-" + plat,
				Role:   core.RoleButton,
				Label:  ed.T("buildRelease.addKeyLabel", l10n.Param("dest", it.DestinationLabel)),
				Style: core.Style{
					Height:       core.Pt(24),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          5,
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 8,
					Background:   p.Warning.Fill,
					BorderWidth:  1,
					BorderColor:  p.Warning.Edge,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					if !running {
						ed.OpenKeychainCredentialEditor(dest)
					}
				},
				Children: []core.Node{
					buildSpecIcon(lucide.Lock(), 10, p.Warning.Fg),
					core.Text{Content: ed.T("buildRelease.addKey"), Style: core.Style{FontSize: 11, Color: p.Warning.Fg}},
				},
			}
		}
	}

	sizeText := "—"
	if it.SizeBytes > 0 {
		sizeText = formatSizeMB(it.SizeBytes)
	}

	borderBottom := core.Points(1)
	if isLast {
		borderBottom = 0
	}

	var checkNode core.Node
	if manual {
		checkNode = core.View{
			TestID: "build-upload-check-" + plat,
			Style: core.Style{
				Width:        core.Pt(18),
				Height:       core.Pt(18),
				FlexShrink:   0,
				CornerRadius: 4,
				BorderWidth:  boxBorderW,
				BorderColor:  boxBorder,
			},
		}
	} else {
		checkNode = core.View{
			TestID: "build-upload-check-" + plat,
			Role:   core.RoleCheckbox,
			Label:  ed.T("buildRelease.uploadItemLabel", l10n.Param("artifact", it.ArtifactName), l10n.Param("dest", it.DestinationLabel)),
			Style: core.Style{
				Width:          core.Pt(18),
				Height:         core.Pt(18),
				FlexShrink:     0,
				CornerRadius:   4,
				Background:     boxBg,
				BorderWidth:    boxBorderW,
				BorderColor:    boxBorder,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				if !running {
					ed.report(ed.SetReleaseUploadItemIncluded(plat, !it.Included))
				}
			},
			Children: checkChildren,
		}
	}

	destColor := p.TextSecondary
	if manual {
		destColor = p.TextMuted
	}

	rowChildren := []core.Node{
		core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
			Children: []core.Node{
				checkNode,
				core.View{
					Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2, Overflow: core.Hidden},
					Children: []core.Node{
						core.Text{Content: it.ArtifactName, Style: core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: p.TextPrimary}},
						core.Text{Content: it.ArtifactRelPath, Style: core.Style{FontSize: 11, Color: p.TextFaint}},
					},
				},
				core.View{
					Style: core.Style{Width: core.Pt(184), FlexShrink: 0, Gap: 2},
					Children: []core.Node{
						core.Text{Content: it.DestinationLabel, Style: core.Style{FontSize: 12, Color: destColor}},
						core.Text{Content: it.Track, Style: core.Style{FontSize: 11, Color: p.TextFaint}},
					},
				},
				core.Text{
					Content: it.Version,
					Style:   core.Style{Width: core.Pt(62), FlexShrink: 0, FontSize: 12, Color: p.TextSecondary, FontVariant: core.FontTabularNums},
				},
				core.Text{
					Content: sizeText,
					Style:   core.Style{Width: core.Pt(68), FlexShrink: 0, FontSize: 12, Color: p.TextSecondary, FontVariant: core.FontTabularNums},
				},
				core.View{
					Style:    core.Style{Width: core.Pt(122), FlexShrink: 0, AlignItems: core.AlignEnd},
					Children: []core.Node{statusNode},
				},
			},
		},
	}

	if it.Err != "" {
		rowChildren = append(rowChildren, core.Text{
			Content: ed.T("buildRelease.uploadError", l10n.Param("error", it.Err)),
			Style:   core.Style{FontSize: 11, Color: p.Removed.Fg, Padding: core.Edges{Left: 28}},
		})
	} else if it.Status == UploadStatusUploading && it.StatusMessage != "" {
		rowChildren = append(rowChildren, core.Text{
			Content: it.StatusMessage,
			Style:   core.Style{FontSize: 11, Color: p.TextMuted, Padding: core.Edges{Left: 28}},
		})
	} else if it.ResultURL != "" && it.StatusMessage != "" && it.StatusMessage != "Uploaded" {
		rowChildren = append(rowChildren, core.Text{
			Content: it.StatusMessage + " · " + it.ResultURL,
			Style:   core.Style{FontSize: 11, Color: p.TextFaint, Padding: core.Edges{Left: 28}},
		})
	} else if it.ResultURL != "" {
		rowChildren = append(rowChildren, core.Text{
			Content: it.ResultURL,
			Style:   core.Style{FontSize: 11, Color: p.TextFaint, Padding: core.Edges{Left: 28}},
		})
	} else if it.Status == UploadStatusDone && it.StatusMessage != "" && it.StatusMessage != "Uploaded" {
		rowChildren = append(rowChildren, core.Text{
			Content: it.StatusMessage,
			Style:   core.Style{FontSize: 11, Color: p.TextFaint, Padding: core.Edges{Left: 28}},
		})
	}

	return core.View{
		TestID: "build-upload-item-" + plat,
		Style: core.Style{
			Padding:     core.Symmetric(10, 12),
			Gap:         4,
			Borders:     core.Edges{Bottom: borderBottom},
			BorderColor: p.Line,
		},
		Children: rowChildren,
	}
}

func localizedBuildCommit(ed *Editor, commit string) string {
	if commit == "" || commit == "local" {
		return ed.T("buildRelease.commitLocal")
	}
	return commit
}

func localizedBuildLastRelease(ed *Editor, ver string) string {
	if ver == "" || ver == "none" {
		return ed.T("buildRelease.lastReleaseNone")
	}
	return ver
}

func localizedBuildQualityProfile(ed *Editor, profile string) string {
	switch profile {
	case "", "Low":
		return ed.T("buildRelease.profileLow")
	case "Medium":
		return ed.T("buildRelease.profileMedium")
	case "High":
		return ed.T("buildRelease.profileHigh")
	default:
		return profile
	}
}

func localizedBuildSmokeSummary(ed *Editor, summary string) string {
	if summary == "" || summary == "not configured" {
		return ed.T("buildRelease.preflightSmokeNotConfigured")
	}
	if scene, ok := strings.CutPrefix(summary, "custom runner, "); ok {
		return ed.T("buildRelease.preflightSmokeCustom", l10n.Param("scene", scene))
	}
	if rest, ok := strings.CutPrefix(summary, "headless "); ok {
		if scene, ok := strings.CutSuffix(rest, " (host only)"); ok {
			return ed.T("buildRelease.preflightSmokeHost", l10n.Param("scene", scene))
		}
	}
	return summary
}

func localizedBuildStatusText(ed *Editor, status string) string {
	switch status {
	case "Queued":
		return ed.T("buildRelease.statusQueued")
	case "Set up":
		return ed.T("buildRelease.statusSetUp")
	case "Building":
		return ed.T("buildRelease.statusBuilding")
	case "Notarizing":
		return ed.T("buildRelease.statusNotarizing")
	case "Done":
		return ed.T("buildRelease.statusDone")
	case "Failed":
		return ed.T("buildRelease.statusFailed")
	case "Stopped":
		return ed.T("buildRelease.statusStopped")
	default:
		return status
	}
}

func localizedBuildDeltaText(ed *Editor, delta string) string {
	if delta == "not measured" {
		return ed.T("buildRelease.deltaNotMeasured")
	}
	if countStr, ok := strings.CutSuffix(delta, " to fix"); ok {
		return ed.T("buildRelease.deltaToFix", l10n.Param("count", countStr))
	}
	return delta
}

func localizedBuildSummary(ed *Editor, summary string) string {
	switch summary {
	case "universal · Developer ID · notarized":
		return ed.T("buildRelease.summaryMacOS")
	case "x64 · signed installer":
		return ed.T("buildRelease.summaryWindows")
	case "x64 · AppImage":
		return ed.T("buildRelease.summaryLinux")
	case "arm64 · TestFlight":
		return ed.T("buildRelease.summaryIOS")
	case "arm64 · AAB for Google Play":
		return ed.T("buildRelease.summaryAndroid")
	default:
		return summary
	}
}

func localizedBuildStepLabel(ed *Editor, label string) string {
	switch label {
	case "Check project":
		return ed.T("buildRelease.stepCheckProject")
	case "Compile Go":
		return ed.T("buildRelease.stepCompileGo")
	case "Pack assets":
		return ed.T("buildRelease.stepPackAssets")
	case "Sign":
		return ed.T("buildRelease.stepSign")
	case "Smoke playtest":
		return ed.T("buildRelease.stepSmokePlaytest")
	case "Measure":
		return ed.T("buildRelease.stepMeasure")
	default:
		return label
	}
}

func localizedBuildStepDetail(ed *Editor, detail string) string {
	switch detail {
	case "atlases, audio, and scenes":
		return ed.T("buildRelease.stepDetailPackAssets")
	case "Developer ID · notarized":
		return ed.T("buildRelease.stepDetailSignMacOS")
	case "headless main scene":
		return ed.T("buildRelease.stepDetailSmokeHeadless")
	case "requires playtest runner":
		return ed.T("buildRelease.stepDetailSmokeRunner")
	case "not run":
		return ed.T("buildRelease.notRun")
	case "not measured":
		return ed.T("buildRelease.deltaNotMeasured")
	case "size and frame time":
		return ed.T("buildRelease.stepDetailMeasure")
	default:
		return detail
	}
}
