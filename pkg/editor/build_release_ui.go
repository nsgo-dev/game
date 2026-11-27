package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/internal/qrcode"
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

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		return core.View{
			TestID: "build-release-screen",
			Role:   core.RoleRegion,
			Label:  "Build and release",
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

func buildReleaseHeaderView(cx *core.Scope, p SpecPalette, ed *Editor, brRev *core.Memo[uint64], versionDraft *core.Signal[string]) core.Node {
	projectNameMemo := core.NewMemo(cx, func() string {
		_ = brRev.Get()
		name := ed.BuildReleaseSnapshot().ProjectName
		if name == "" {
			return "Project"
		}
		return name
	})

	// Keep the outer header and version ui.Input outside core.Dynamic so streaming log lines
	// and release-notes keystrokes (which increment brRev) never remount or drop focus on
	// build-release-version.
	return core.View{
		TestID: "build-release-header",
		Role:   core.RoleRegion,
		Label:  "Release header",
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
				Label:  "Back to scene editor",
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
						cur := ed.SpecPalette(cx)
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
						Content: "Release",
						Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
					},
				},
			},
			core.Text{
				Content: "Version",
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.View{
				Style: core.Style{Width: core.Pt(72)},
				Children: []core.Node{
					ui.Input{
						TestID:      "build-release-version",
						Label:       "Release version",
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
				p := ed.SpecPalette(cx)
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

				chipText := fmt.Sprintf("Ready · %d presets", includedCount)
				var chipBg core.Color
				chipBorder := p.LineRaised
				chipFg := p.TextMuted
				dotColor := p.TextFaint
				switch {
				case snap.Running:
					chipText = fmt.Sprintf("Building · %d of %d done", doneCount, includedCount)
					chipBg = p.Agent.Fill
					chipBorder = p.Agent.Edge
					chipFg = p.TextSecondary
					dotColor = p.Agent.Fg
				case snap.Stopped:
					chipText = fmt.Sprintf("Stopped · %d of %d done", doneCount, includedCount)
					chipBorder = p.LineRaised
					chipFg = p.TextMuted
					dotColor = p.TextFaint
				case failedCount > 0:
					chipText = fmt.Sprintf("%d of %d done · %d failed", doneCount, includedCount, failedCount)
					chipBg = p.Removed.Fill
					chipBorder = p.Removed.Edge
					chipFg = p.TextSecondary
					dotColor = p.Removed.Fg
				case doneCount > 0 && doneCount == includedCount:
					chipText = fmt.Sprintf("Done · %d of %d built", doneCount, includedCount)
					chipBg = p.Added.Fill
					chipBorder = p.Added.Edge
					chipFg = p.TextSecondary
					dotColor = p.Added.Fg
				}

				actionLabel := "Build"
				actionBg := p.Accent.Fg
				actionFg := p.Accent.OnFill
				actionIcon := lucide.Play
				if snap.Running {
					actionLabel = "Stop"
					actionBg = p.LineRaised
					actionFg = p.TextPrimary
					actionIcon = lucide.Square
				} else if snap.Stopped {
					actionLabel = "Resume"
					actionBg = p.Accent.Fg
					actionFg = p.Accent.OnFill
					actionIcon = lucide.Play
				}

				commit := snap.GitCommit
				if commit == "" {
					commit = "local"
				}
				branch := snap.GitBranch
				if branch == "" {
					branch = "main"
				}
				metaText := fmt.Sprintf("from %s on %s · last release %s", commit, branch, snap.LastReleaseVersion)

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
						buildReleasePillButton(p, "build-release-dry-run", "Dry Run", snap.DryRun, snap.Running, func() {
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

func buildReleasePillButton(p SpecPalette, testID, label string, active, disabled bool, onPress func()) core.Node {
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

func buildReleaseLeftColumnView(cx *core.Scope, p SpecPalette, ed *Editor, brRev *core.Memo[uint64], notesDraft *core.Signal[string]) core.Node {
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
				cur := ed.SpecPalette(cx)
				snap := ed.BuildReleaseSnapshot()
				return buildReleasePreflightSection(cur, snap.Preflight)
			}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = brRev.Get()
				cur := ed.SpecPalette(cx)
				snap := ed.BuildReleaseSnapshot()
				return buildReleasePresetsSection(cur, ed, snap)
			}),
			buildReleaseNotesSection(p, ed, brRev, notesDraft),
			buildReleasePublishFooter(p, ed),
		},
	}
}

func buildReleasePreflightSection(p SpecPalette, pf BuildPreflightSummary) core.Node {
	checkIcon := lucide.Check()
	checkColor := p.Added.Fg
	checkDetail := "not run yet"
	if pf.CheckRan {
		elapsed := formatBuildDuration(pf.CheckElapsed)
		if elapsed == "" {
			elapsed = "<1 s"
		}
		if pf.CheckErr != "" {
			checkIcon = lucide.CircleAlert()
			checkColor = p.Removed.Fg
			checkDetail = "error · " + elapsed
		} else if pf.CheckProblems > 0 {
			checkIcon = lucide.CircleAlert()
			checkColor = p.Removed.Fg
			checkDetail = fmt.Sprintf("%d problems · %s", pf.CheckProblems, elapsed)
		} else {
			checkDetail = fmt.Sprintf("0 problems · %s", elapsed)
		}
	}

	smokeDetail := pf.SmokeSummary
	if smokeDetail == "" {
		smokeDetail = "60 s per build, main scene"
	}

	profile := pf.QualityProfile
	if profile == "" {
		profile = "Low"
	}
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
				Content: "BEFORE EVERY BUILD",
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
									core.Text{Content: "nsgo check", Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
								},
							},
							core.Text{Content: checkDetail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
					core.View{
						TestID: "build-preflight-smoke",
						Style:  cardStyle,
						Children: []core.Node{
							core.Text{Content: "Smoke playtest", Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
							core.Text{Content: smokeDetail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
					core.View{
						TestID: "build-preflight-budgets",
						Style:  cardStyle,
						Children: []core.Node{
							core.Text{Content: fmt.Sprintf("Budgets · %s profile", profile), Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
							core.Text{Content: budgetDetail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
						},
					},
				},
			},
		},
	}
}

func buildReleasePresetsSection(p SpecPalette, ed *Editor, snap BuildReleaseSnapshot) core.Node {
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
						Content: "EXPORT PRESETS · export/",
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.7,
							Color:         p.TextFaint,
						},
					},
					core.Text{
						Content: "size vs " + snap.LastReleaseVersion,
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

func buildPresetRowView(p SpecPalette, ed *Editor, entry BuildPresetEntry, selected, running bool) core.Node {
	plat := entry.Platform
	var rowBg core.Color
	var leftBarColor core.Color
	if selected {
		rowBg = specAlpha(p.Accent.Fg, 0.14)
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
		Content: entry.StatusText,
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
				Label:  "Include " + entry.Name + " in this release",
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
				Label:  "Select " + entry.Name + " preset",
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
							core.Text{Content: entry.Summary, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
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
								Content: entry.DeltaText,
								Style:   core.Style{FontSize: 11, Color: p.TextFaint, FontVariant: core.FontTabularNums},
							},
						},
					},
				},
			},
		},
	}
}

func buildPresetTitleRow(p SpecPalette, ed *Editor, entry BuildPresetEntry) core.Node {
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

func presetStatusChipStyle(p SpecPalette, st BuildPresetStatus) (bg, border, fg core.Color, icon *core.SVG, iconColor core.Color) {
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

func buildReleaseNotesSection(p SpecPalette, ed *Editor, brRev *core.Memo[uint64], notesDraft *core.Signal[string]) core.Node {
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
				cur := ed.SpecPalette(cx)
				snap := ed.BuildReleaseSnapshot()
				headerChildren := []core.Node{
					core.Text{
						Content: "RELEASE NOTES",
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
					unit := "commits"
					if snap.CommitsSinceRelease == 1 {
						unit = "commit"
					}
					provenance = fmt.Sprintf("Drafted from %d %s since %s", snap.CommitsSinceRelease, unit, since)
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
						Label:       "Release notes",
						Multiline:   true,
						Placeholder: "Summarize what changed in this release…",
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
				p := ed.SpecPalette(cx)
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
							Content: fmt.Sprintf("Saved to %s · game release -release-notes", notesFile),
							Style:   core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), FontSize: 11, Color: p.TextFaint},
						},
						core.View{
							TestID: "build-release-notes-redraft",
							Role:   core.RoleButton,
							Label:  "Redraft release notes",
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
									Content: "Redraft",
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

func buildReleasePublishFooter(p SpecPalette, ed *Editor) core.Node {
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
					core.Text{Content: "Publish to itch.io, Google Play internal, TestFlight", Style: core.Style{FontSize: 12.5, Color: p.TextPrimary}},
					core.Text{Content: "Each upload asks first, whatever the permission profile", Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
				},
			},
			core.View{
				Ref:    reviewBtnRef,
				TestID: "build-release-review-uploads",
				Role:   core.RoleButton,
				Label:  "Review uploads…",
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
						Content: "Review uploads…",
						Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
					},
				},
			},
		},
	}
}

func buildReleaseDetailPaneView(cx *core.Scope, p SpecPalette, ed *Editor, brRev *core.Memo[uint64]) core.Node {
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
				p := ed.SpecPalette(cx)
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
								Content: "No export presets configured yet.",
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
					headerChildren = append(headerChildren, buildReleaseSecondaryButtonSpec(p, "build-detail-show-finder", "Show in Finder", func() {
						ed.report(ed.ShowBuildArtifactInFinder(plat))
					}))
				}
				editLabel := "Edit preset"
				if snap.EditingPreset {
					editLabel = "Done editing"
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

func buildInlinePresetEditor(p SpecPalette, ed *Editor, entry BuildPresetEntry, running bool) core.Node {
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
				Content: "TARGETS FOR " + strings.ToUpper(entry.Name),
				Style:   core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: p.TextFaint},
			},
			core.View{
				Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6},
				Children: targetPills,
			},
		},
	}
}

func buildDetailRunningView(p SpecPalette, ed *Editor, entry BuildPresetEntry, followLog bool) core.Node {
	stepRows := make([]core.Node, 0, len(entry.Steps))
	for _, st := range entry.Steps {
		stepRows = append(stepRows, buildStepRowView(p, st))
	}

	logContent := strings.Join(entry.LogLines, "\n")
	if logContent == "" {
		logContent = "Waiting for build output…"
	}
	followLabel := "Following"
	if !followLog {
		followLabel = "Paused"
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
							core.Text{Content: "Log", Style: core.Style{FontSize: 12, Color: p.TextMuted}},
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

func buildStepRowView(p SpecPalette, st BuildStep) core.Node {
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
			core.Text{Content: st.Label, Style: core.Style{FontSize: 12.5, Color: labelColor}},
			core.Text{Content: st.Detail, Style: core.Style{FontSize: 12, Color: p.TextFaint}},
			core.View{Style: core.Style{FlexGrow: 1}},
			core.Text{
				TestID:  "build-step-time-" + string(st.ID),
				Content: formatBuildDuration(st.Elapsed),
				Style:   core.Style{FontSize: 11, Color: p.TextFaint, FontVariant: core.FontTabularNums},
			},
		},
	}
}

func buildDetailFailedView(p SpecPalette, ed *Editor, entry BuildPresetEntry, showWholeLog bool) core.Node {
	f := entry.Failure
	if f == nil {
		f = &BuildFailureInfo{
			Headline:   "Build failed",
			StepIndex:  1,
			TotalSteps: len(entry.Steps),
			StepLabel:  "Build",
			Notice:     BuildFailureNotice,
		}
	}
	notice := f.Notice
	if notice == "" {
		notice = BuildFailureNotice
	}
	stepMeta := fmt.Sprintf("step %d of %d · %s", f.StepIndex, f.TotalSteps, f.StepLabel)
	if dur := formatBuildDuration(f.Elapsed); dur != "" {
		stepMeta += " · after " + dur
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
			footnote = "Edits " + prop.FilePath
		}
		fixCard := core.View{
			TestID: "build-fix-card",
			Style: core.Style{
				CornerRadius: 12,
				Background:   specAlpha(p.Agent.Fg, 0.06),
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
							Label:  "Apply and rebuild " + entry.Name,
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
									Content: "Apply and rebuild " + entry.Name,
									Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightMedium, Color: p.Accent.OnFill},
								},
							},
						},
						buildReleaseSecondaryButtonSpec(p, "build-whole-log", "Whole log", func() {
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
	spec := ed.SpecPalette(cx)

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
		{"Code", res.Breakdown.CodeBytes, spec.Accent.Fg},
		{"Textures", res.Breakdown.TexturesBytes, spec.Changed.Fg},
		{"Audio", res.Breakdown.AudioBytes, spec.Added.Fg},
		{"Other", res.Breakdown.OtherBytes, spec.TextFaint},
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

	deltaColor := spec.TextFaint
	deltaText := "new"
	if res.HasLastRelease {
		deltaText = formatDeltaMB(res.DeltaBytes)
		if res.DeltaBytes > 0 {
			deltaColor = spec.Changed.Fg
		} else if res.DeltaBytes < 0 {
			deltaColor = spec.Added.Fg
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
						Content: res.ArtifactName,
						Style:   core.Style{FontSize: 12, Color: spec.TextPrimary},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{
						TestID:  "build-result-size",
						Content: formatSizeMB(res.SizeBytes),
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
				Style: core.Style{Padding: core.Symmetric(12, 14), Gap: 10},
				Children: []core.Node{
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
						Content: res.Breakdown.GrowthSummary,
						Style:   core.Style{FontSize: 12, Color: spec.TextFaint},
					},
				},
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
	sizePct := clampBudgetPct((float64(res.SizeBytes) / (1024 * 1024) / dlBudget) * 100)
	framePct := clampBudgetPct((res.FrameTimeP95Ms / frameBudget) * 100)

	budgetsRow := core.View{
		TestID: "build-result-budgets",
		Style:  core.Style{Direction: core.Row, Gap: 12},
		Children: []core.Node{
			buildBudgetCard(spec, "build-budget-download", "Download size",
				fmt.Sprintf("%s / %.0f MB", formatSizeMB(res.SizeBytes), dlBudget),
				res.DownloadBudgetMet, sizePct),
			buildBudgetCard(spec, "build-budget-frame", "Frame time p95",
				fmt.Sprintf("%.1f / %.1f ms", res.FrameTimeP95Ms, frameBudget),
				res.FrameTimeBudgetMet, framePct),
		},
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
					buildSpecIcon(lucide.Check(), 13, spec.Added.Fg),
					core.Text{Content: "Smoke playtest · 60 s", Style: core.Style{FontSize: 12.5, Color: spec.TextPrimary}},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{Content: res.SmokePlaytestWhere, Style: core.Style{FontSize: 11.5, Color: spec.TextFaint}},
				},
			},
			core.Text{
				Content: res.SmokePlaytestSummary,
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
						Content: "RUN ON DEVICE",
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

func buildBudgetCard(p SpecPalette, testID, title, metric string, met bool, pct float32) core.Node {
	badgeText := "Met"
	badgeBg := p.Added.Fill
	badgeBorder := p.Added.Edge
	badgeFg := p.Added.Fg
	barColor := p.Added.Fg
	if !met {
		badgeText = "Missed"
		badgeBg = p.Removed.Fill
		badgeBorder = p.Removed.Edge
		badgeFg = p.Removed.Fg
		barColor = p.Removed.Fg
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
							core.Text{Content: badgeText, Style: core.Style{FontSize: 10.5, FontWeight: core.FontWeightMedium, Color: badgeFg}},
						},
					},
				},
			},
			core.Text{Content: metric, Style: core.Style{FontSize: 12, Color: p.TextPrimary, FontVariant: core.FontTabularNums}},
			core.View{
				Style: core.Style{Height: core.Pt(6), CornerRadius: 3, Background: p.LineRaised, Overflow: core.Hidden},
				Children: []core.Node{
					core.View{Style: core.Style{Height: core.Pt(6), Width: core.Pct(pct), CornerRadius: 3, Background: barColor}},
				},
			},
		},
	}
}

func buildDetailReadinessView(p SpecPalette, ed *Editor, entry BuildPresetEntry) core.Node {
	_, totalIssues := countReadinessIssues(entry.Readiness)
	heading := fmt.Sprintf("READINESS · %d to fix before %s can build", totalIssues, entry.Name)
	if totalIssues == 0 {
		heading = fmt.Sprintf("READINESS · %s is ready to build", entry.Name)
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

	guidance := fmt.Sprintf("The agent can handle project-side setup for %s; account sign-in and store credentials require you.", entry.Name)
	if len(agentItems) > 0 && len(personItems) > 0 {
		guidance = fmt.Sprintf("The agent can write %s from what the game uses. %s needs your account, so you complete sign-in directly.",
			strings.Join(agentItems, ", "), capitalizeProfile(strings.Join(personItems, ", ")))
	} else if len(agentItems) > 0 {
		guidance = fmt.Sprintf("The agent can complete %s automatically.", strings.Join(agentItems, ", "))
	} else if len(personItems) > 0 {
		guidance = fmt.Sprintf("%s requires your account sign-in before %s can build.", capitalizeProfile(strings.Join(personItems, ", ")), entry.Name)
	}

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
					Background:   specAlpha(p.Agent.Fg, 0.06),
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
						Label:  "Set up " + entry.Name + " with the agent",
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
								Content: "Set up " + entry.Name + " with the agent",
								Style:   core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: p.Accent.OnFill},
							},
						},
					},
				},
			},
		},
	}
}

func buildDetailQueuedView(p SpecPalette, ed *Editor, snap BuildReleaseSnapshot, entry BuildPresetEntry) core.Node {
	title := "Queued"
	if snap.ActivePlatform != "" && snap.ActivePlatform != entry.Platform {
		title = "Queued after " + platformDisplayName(snap.ActivePlatform)
	}
	subtitle := "Builds run one at a time so the editor stays responsive."
	actionLabel := "Build " + entry.Name + " next"
	actionID := "build-queued-next"
	plat := entry.Platform
	onAct := func() {
		ed.PrioritizeBuildPreset(plat)
	}

	if snap.Stopped || entry.StoppedAt {
		title = entry.Name + " stopped"
		subtitle = "Finished steps are kept; Resume continues from the step that was running."
		actionLabel = "Resume"
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
			stepRows = append(stepRows, buildStepRowView(p, st))
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

func buildWebQRCardView(ed *Editor, spec SpecPalette, ws WebBuildServerSnapshot, qrURL string) core.Node {
	statusLabel := "LAN test link"
	statusDot := spec.Added.Fg
	statusFg := spec.TextSecondary
	statusBg := spec.Added.Fill
	statusBorder := spec.Added.Edge
	switch {
	case ws.Serving && len(ws.ConnectedClients) > 0:
		c := ws.ConnectedClients[len(ws.ConnectedClients)-1]
		statusLabel = "MCP bridge · " + c.Device
		statusDot = spec.Agent.Fg
		statusFg = spec.TextPrimary
		statusBg = spec.Agent.Fill
		statusBorder = spec.Agent.Edge
	case ws.Serving:
		statusLabel = "Serving on LAN"
		statusDot = spec.Added.Fg
		statusFg = spec.TextPrimary
		statusBg = spec.Added.Fill
		statusBorder = spec.Added.Edge
	case ws.StoppedByUser:
		statusLabel = "Stopped"
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
			qrNode = core.View{
				Role:  core.RoleImage,
				Label: "QR code for " + qrURL,
				Style: core.Style{
					Width:        core.Pt(160),
					Height:       core.Pt(160),
					CornerRadius: 8,
					Background:   lightSpecPalette.Raised,
					BorderWidth:  qrBorderW,
					BorderColor:  qrBorderCol,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "build-qr-canvas",
						Label:  "QR code for " + qrURL,
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
					Content: "Server stopped",
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

	caption := "Scan on a phone on this Wi-Fi. Play mode there connects to the MCP bridge, so the agent can debug it too."
	if ws.StoppedByUser {
		caption = "Local server stopped and port released. Start serving again to test on a phone on this Wi-Fi."
	}
	children = append(children, core.Text{
		TestID:  "build-qr-caption",
		Content: caption,
		Style:   core.Style{FontSize: 11.5, Color: spec.TextMuted},
	})

	var actions []core.Node
	if !ws.StoppedByUser && qrURL != "" {
		actions = append(actions, buildReleaseSecondaryButtonSpec(spec, "build-qr-copy-url", "Copy URL", func() {
			_, err := ed.CopyWebBuildURL()
			ed.report(err)
		}))
	}
	if ws.Serving {
		actions = append(actions, buildReleaseSecondaryButtonSpec(spec, "build-qr-toggle-serve", "Stop serving", func() {
			ed.report(ed.StopWebBuildServer())
		}))
	} else if ws.ServeDir != "" {
		actions = append(actions, buildReleaseSecondaryButtonSpec(spec, "build-qr-toggle-serve", "Start serving", func() {
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
	d.SetFillColor(lightSpecPalette.Raised).FillRect(0, 0, float32(w), float32(h))

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

	d.SetFillColor(darkSpecPalette.Bg)
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

func deviceStateDotColor(spec SpecPalette, st BuildDeviceState, available bool) core.Color {
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

func buildReleaseSecondaryButtonSpec(p SpecPalette, testID, label string, onPress func()) core.Node {
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

	p := ed.SpecPalette(cx)

	headerNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = brRev.Get()
		cur := ed.SpecPalette(cx)
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
							Content: "Review uploads",
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
									Content: "Always asks first",
									Style:   core.Style{FontSize: 11.5, FontWeight: core.FontWeightMedium, Color: cur.Warning.Fg},
								},
							},
						},
					},
				},
				core.Text{
					Content: "Check each artifact, store destination, and version before anything is sent.",
					Style:   core.Style{FontSize: 12, Color: cur.TextMuted},
				},
				core.Text{
					TestID:  "build-review-uploads-policy",
					Content: fmt.Sprintf("Permission profile: %s · Each upload asks first, whatever the permission profile.", rev.PermissionProfile.DisplayName()),
					Style:   core.Style{FontSize: 11.5, Color: cur.TextFaint},
				},
			},
		}
	})

	itemsAndSummaryNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = brRev.Get()
		cur := ed.SpecPalette(cx)
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
						Content: "No export presets are ready to upload yet. Build at least one preset first.",
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
						core.Text{Content: "ARTIFACT", Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: "DESTINATION", Style: core.Style{Width: core.Pt(184), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: "VERSION", Style: core.Style{Width: core.Pt(62), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: "SIZE", Style: core.Style{Width: core.Pt(68), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
						core.Text{Content: "KEYCHAIN", Style: core.Style{Width: core.Pt(122), FlexShrink: 0, FontSize: 10.5, FontWeight: core.FontWeightSemibold, LetterSpacing: 0.7, Color: cur.TextFaint}},
					},
				},
			}, rows...),
		}

		notesPreview := rev.ReleaseNotesPreview
		if notesPreview == "" {
			notesPreview = "(empty)"
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
					core.Text{Content: "Release notes:", Style: core.Style{FontSize: 11.5, Color: cur.TextFaint}},
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
			cur := ed.SpecPalette(cx)
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
									return fmt.Sprintf("Store %s credential in system keychain", destMemo.Get().DisplayName())
								},
								Style: core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: cur.TextPrimary, FlexGrow: 1},
							},
							core.Text{
								Content: "Never written to project files",
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
										Label:       "Store account",
										Placeholder: "Account or publisher name",
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
										Label:       "API key or token",
										Placeholder: "API key / token",
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
							buildReleaseSecondaryButtonSpec(cur, "build-keychain-cancel", "Cancel", func() {
								kcSecret.Set("")
								ed.CloseKeychainCredentialEditor()
							}),
							core.View{
								TestID: "build-keychain-save",
								Role:   core.RoleButton,
								Label:  "Save to keychain",
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
										Content: "Save to keychain",
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
		cur := ed.SpecPalette(cx)
		rev := ed.BuildReleaseSnapshot().UploadReview
		selectedCount := 0
		for _, it := range rev.Items {
			if it.Included {
				selectedCount++
			}
		}
		cancelLabel := "Cancel"
		if rev.Completed {
			cancelLabel = "Close"
		}
		confirmLabel := fmt.Sprintf("Confirm and upload %d", selectedCount)
		if selectedCount == 1 {
			confirmLabel = "Confirm and upload 1"
		}
		if rev.Running {
			confirmLabel = "Uploading…"
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
					Content: "Credentials stay in the system keychain · never in project files.",
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
		Label:  "Review uploads",
		StyleFunc: func() core.Style {
			cur := ed.SpecPalette(cx)
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
				EdgeColor:    func() core.Color { return ed.SpecPalette(cx).Raised },
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
		Label:  "Review uploads",
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

func buildReviewUploadItemRow(p SpecPalette, ed *Editor, it ReleaseUploadItem, running, isLast bool) core.Node {
	plat := it.Platform
	dest := it.Destination

	var boxBg core.Color
	boxBorder := p.TextFaint
	var boxBorderW core.Points = 1.5
	var checkChildren []core.Node
	if it.Included {
		boxBg = p.Accent.Fg
		boxBorder = p.Accent.Fg
		boxBorderW = 0
		checkChildren = []core.Node{buildSpecIcon(lucide.Check(), 10, p.Accent.OnFill)}
	}

	var statusNode core.Node
	switch it.Status {
	case UploadStatusUploading:
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
				core.Text{Content: fmt.Sprintf("Uploading %d%%", pct), Style: core.Style{FontSize: 11, Color: p.TextSecondary}},
			},
		}
	case UploadStatusDone:
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
				core.Text{Content: "Uploaded", Style: core.Style{FontSize: 11, Color: p.Added.Fg}},
			},
		}
	case UploadStatusFailed:
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
				core.Text{Content: "Failed", Style: core.Style{FontSize: 11, Color: p.Removed.Fg}},
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
					core.Text{Content: "Key in keychain", Style: core.Style{FontSize: 11, Color: p.Added.Fg}},
				},
			}
		} else {
			statusNode = core.View{
				TestID: "build-upload-key-" + plat,
				Role:   core.RoleButton,
				Label:  "Add keychain key for " + it.DestinationLabel,
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
					core.Text{Content: "Add key…", Style: core.Style{FontSize: 11, Color: p.Warning.Fg}},
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

	rowChildren := []core.Node{
		core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
			Children: []core.Node{
				core.View{
					TestID: "build-upload-check-" + plat,
					Role:   core.RoleCheckbox,
					Label:  "Upload " + it.ArtifactName + " to " + it.DestinationLabel,
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
				},
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
						core.Text{Content: it.DestinationLabel, Style: core.Style{FontSize: 12, Color: p.TextSecondary}},
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
			Content: "Error: " + it.Err,
			Style:   core.Style{FontSize: 11, Color: p.Removed.Fg, Padding: core.Edges{Left: 28}},
		})
	} else if it.ResultURL != "" {
		rowChildren = append(rowChildren, core.Text{
			Content: it.ResultURL,
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
