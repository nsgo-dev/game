package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// problemAlertIcon renders the Spec alert triangle stroke icon (never hue alone).
func problemAlertIcon(col core.Color, size core.Points) core.Node {
	return problemAlertIconFunc(func() core.Color { return col }, size)
}

// problemAlertIconFunc renders the Spec alert triangle stroke icon with a
// reactive color callback so live Dark ↔ Light theme switches repaint without remounting.
func problemAlertIconFunc(colorFunc func() core.Color, size core.Points) core.Node {
	return core.Canvas{
		Style: core.Style{Width: core.Pt(size), Height: core.Pt(size), FlexShrink: 0},
		Draw: func(d *core.Drawing, sz core.Size) {
			col := colorFunc()
			w, h := float32(sz.Width), float32(sz.Height)
			d.SetStrokeColor(col).SetLineWidth(1.6).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
			d.BeginPath().MoveTo(w*0.5, h*0.14).LineTo(w*0.89, h*0.84).LineTo(w*0.11, h*0.84).ClosePath().Stroke()
			d.BeginPath().MoveTo(w*0.5, h*0.40).LineTo(w*0.5, h*0.62).Stroke()
			d.SetFillColor(col).BeginPath().Arc(w*0.5, h*0.74, max(0.75, w*0.055), 0, float32(2*math.Pi), false).Fill()
		},
	}
}

// ProblemsView renders the Problems panel below the viewport while a project is
// open: one row per live (non-ignored) problem found by assetcheck.CheckWith
// (over disk plus unsaved open documents) and project.Check, showing its
// file:line:col location, severity pill, message, proposed-fix badge, and
// Ignore action. Problems use the Spec removed/problem token with the alert
// icon and warnings use the Spec warning token with its fill, edge, and alert
// icon. Pressing or activating a row via keyboard opens the scene or selects
// the file in the file browser. It renders nothing while no project is open.
func ProblemsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Project != nil && ed.ProjectPath != ""
	})
	type snapshot struct {
		problems  []assetcheck.Problem
		proposals []ProposedProblemFix
		checking  bool
		errMsg    string
		dark      bool
		lang      string
	}
	state := core.NewMemoFunc(cx, func() snapshot {
		_ = rev.Get()
		var errMsg string
		if err := ed.ProblemErr(); err != nil {
			errMsg = err.Error()
		}
		return snapshot{
			problems:  ed.Problems(),
			proposals: ed.ProposedFixes(),
			checking:  ed.CheckingProblems(),
			errMsg:    errMsg,
			dark:      cx.Dark(),
			lang:      ed.ResolvedLanguage(),
		}
	}, func(a, b snapshot) bool {
		if a.checking != b.checking || a.errMsg != b.errMsg || a.dark != b.dark || a.lang != b.lang || !slices.Equal(a.problems, b.problems) || len(a.proposals) != len(b.proposals) {
			return false
		}
		for i := range a.proposals {
			if a.proposals[i].ID != b.proposals[i].ID || a.proposals[i].Problem != b.proposals[i].Problem {
				return false
			}
		}
		return true
	})

	// listState omits background checking transitions while problems are already displayed
	// so live-edit CheckProblems completions never remount problem rows mid-press.
	type listSnapshot struct {
		problems  []assetcheck.Problem
		proposals []ProposedProblemFix
		loading   bool
		errMsg    string
		dark      bool
		lang      string
	}
	listState := core.NewMemoFunc(cx, func() listSnapshot {
		s := state.Get()
		return listSnapshot{
			problems:  s.problems,
			proposals: s.proposals,
			loading:   s.checking && len(s.problems) == 0,
			errMsg:    s.errMsg,
			dark:      s.dark,
			lang:      s.lang,
		}
	}, func(a, b listSnapshot) bool {
		if a.loading != b.loading || a.errMsg != b.errMsg || a.dark != b.dark || a.lang != b.lang || !slices.Equal(a.problems, b.problems) || len(a.proposals) != len(b.proposals) {
			return false
		}
		for i := range a.proposals {
			if a.proposals[i].ID != b.proposals[i].ID || a.proposals[i].Problem != b.proposals[i].Problem {
				return false
			}
		}
		return true
	})

	checkShortcut := FormatShortcut(ed.EffectiveShortcut(CmdCheckProblems), cx.Env().OS())

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		s := listState.Get()
		p := ed.SpecPalette(cx)
		if s.errMsg != "" {
			retryBtn := specPanelButton(p, specPanelButtonSpec{
				TestID:     "problems-error-retry",
				Text:       ed.T("problems.retry"),
				Icon:       lucide.RefreshCw,
				Label:      ed.T("problems.checkLabel"),
				Tooltip:    specShortcutTip(ed.T("problems.checkLabel"), checkShortcut),
				Variant:    specBtnQuiet,
				Compact:    true,
				InsetFocus: true,
				OnPress:    ed.CheckProblems,
			})
			return specStatusBanner(p, "problems-error", false, s.errMsg, nil, retryBtn)
		}
		if s.loading {
			return specLoadingState(p, "problems-loading", ed.T("problems.checking"))
		}
		if len(s.problems) == 0 {
			return specEmptyState(p, "problems-empty", lucide.CircleCheck, p.Added.Fg, ed.T("problems.empty"), "", nil)
		}
		proposalByProb := make(map[assetcheck.Problem]ProposedProblemFix, len(s.proposals))
		for _, prop := range s.proposals {
			proposalByProb[prop.Problem] = prop
		}
		rows := make([]core.Node, 0, len(s.problems))
		for i, prob := range s.problems {
			item := prob
			loc := item.Location()
			isWarn := problemIsWarning(item)
			_, hasFix := proposalByProb[item]

			rowBg := p.Raised
			rowBorder := p.LineRaised
			rowHover := p.LineRaised
			locColor := p.TextPrimary
			badgeLabel := ed.T("problems.error")
			if isWarn {
				rowBg = p.Warning.Fill
				rowBorder = p.Warning.Edge
				rowHover = p.Warning.Edge
				locColor = p.Warning.Fg
				badgeLabel = ed.T("problems.warning")
			}

			rowAria := fmt.Sprintf("%s: %s — %s", badgeLabel, loc, item.Message)
			rowBtn := specPanelRowButton(p, specPanelRowButtonSpec{
				TestID:  fmt.Sprintf("problems-row-%d", i),
				Label:   rowAria,
				Tooltip: "Open " + loc,
				Style: core.Style{
					FlexGrow:    1,
					MinWidth:    core.Pt(0),
					Background:  rowBg,
					BorderColor: rowBorder,
					Hover:       &core.Style{Background: rowHover, BorderColor: rowBorder},
				},
				OnPress: func() {
					ed.report(ed.OpenProblem(item))
				},
				Children: []core.Node{
					specSeverityPill(p, "", isWarn, badgeLabel),
					core.Text{
						Content: loc,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							FontWeight: core.FontWeightSemibold,
							Color:      locColor,
							FlexShrink: 0,
						},
					},
					core.Text{
						Content: item.Message,
						Style: core.Style{
							FontSize:   12.5,
							Color:      p.TextSecondary,
							FlexGrow:   1,
							MinWidth:   core.Pt(0),
							FlexShrink: 1,
						},
					},
				},
			})

			rowLine := []core.Node{rowBtn}
			if hasFix {
				rowLine = append(rowLine, specPanelButton(p, specPanelButtonSpec{
					TestID:  fmt.Sprintf("problems-row-fix-badge-%d", i),
					Text:    ed.T("problems.fixProposed"),
					Label:   ed.T("problems.fixProposed"),
					Variant: specBtnQuiet,
					Compact: true,
					Style: core.Style{
						Background:  p.Agent.Fill,
						BorderWidth: 1,
						BorderColor: p.Agent.Edge,
						Color:       p.Agent.Fg,
						FlexShrink:  0,
					},
					OnPress: func() {
						ed.report(ed.ShowProblem(item))
					},
				}))
			}
			rowLine = append(rowLine, specPanelButton(p, specPanelButtonSpec{
				TestID:  fmt.Sprintf("problems-row-ignore-%d", i),
				Text:    ed.T("problems.ignore"),
				Label:   ed.T("problems.ignore"),
				Variant: specBtnGhost,
				Compact: true,
				Style:   core.Style{FlexShrink: 0},
				OnPress: func() {
					ed.IgnoreProblem(item)
				},
			}))

			rows = append(rows, core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        specSpace4,
				},
				Children: rowLine,
			})
		}
		return core.View{Style: core.Style{Gap: specSpace4}, Children: rows}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			headerIcon := core.Dynamic(func(cx *core.Scope) core.Node {
				s := state.Get()
				p := ed.SpecPalette(cx)
				if s.checking {
					return ui.Spinner{
						Size:  specIconSize,
						Label: ed.T("problems.checking"),
						Tint:  func() core.Color { return p.Accent.Fg },
					}
				}
				if s.errMsg != "" {
					return specIcon(lucide.TriangleAlert, specIconSize, p.Problem().Fg)
				}
				if len(s.problems) == 0 {
					return specIcon(lucide.CircleCheck, specIconSize, p.Added.Fg)
				}
				hasErr := false
				for _, item := range s.problems {
					if !problemIsWarning(item) {
						hasErr = true
						break
					}
				}
				if hasErr {
					return specIcon(lucide.TriangleAlert, specIconSize, p.Problem().Fg)
				}
				return specIcon(lucide.TriangleAlert, specIconSize, p.Warning.Fg)
			})

			headerCounts := core.Dynamic(func(cx *core.Scope) core.Node {
				s := listState.Get()
				p := ed.SpecPalette(cx)
				var errs, warns int
				for _, item := range s.problems {
					if problemIsWarning(item) {
						warns++
					} else {
						errs++
					}
				}
				if errs == 0 && warns == 0 {
					return nil
				}
				var pills []core.Node
				if errs > 0 {
					pills = append(pills, specSeverityPill(p, "", false, fmt.Sprintf("%d %s", errs, ed.T("problems.error"))))
				}
				if warns > 0 {
					pills = append(pills, specSeverityPill(p, "", true, fmt.Sprintf("%d %s", warns, ed.T("problems.warning"))))
				}
				return core.View{
					Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
					Children: pills,
				}
			})

			header := specPanelHeader(p,
				headerIcon,
				core.Text{
					TestID: "problems-title",
					ContentFunc: func() string {
						s := state.Get()
						if s.checking && len(s.problems) == 0 {
							return ed.T("problems.titleChecking")
						}
						return ed.T("problems.title", l10n.Param("count", len(s.problems)))
					},
					Style: core.Style{
						FontSize:   14,
						FontWeight: core.FontWeightSemibold,
					},
					StyleFunc: func() core.Style {
						return core.Style{Color: ed.SpecPalette(cx).TextPrimary}
					},
				},
				headerCounts,
				core.View{Style: core.Style{FlexGrow: 1}},
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "problems-refresh",
					Text:    ed.T("problems.check"),
					Icon:    lucide.RefreshCw,
					Label:   ed.T("problems.checkLabel"),
					Tooltip: specShortcutTip(ed.T("problems.checkLabel"), checkShortcut),
					Variant: specBtnQuiet,
					Compact: true,
					Disabled: func() bool {
						return state.Get().checking
					},
					Busy: func() bool {
						return state.Get().checking
					},
					OnPress: ed.CheckProblems,
				}),
			)

			body := ui.ScrollView{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
				ContentStyle: core.Style{
					Padding: core.Symmetric(specSpace8, specSpace16),
					Gap:     specSpace8,
				},
				Children: []core.Node{list},
			}

			return specPanelContainer(cx, p, "problems-panel", ed.T("problems.title", l10n.Param("count", 0)), 164, header, body)
		},
	}
}

// ProblemAgentPanelView renders the right-hand Agent problem card matching
// docs/design/editor/Problem.dc.html and Spec.dc.html when ProblemAgentPanelOpen
// is true: problem location card (file:line:col), agent explanation, before/after
// diff card with Apply fix, Show me and Ignore, footer note, and the
// "Ask about this problem…" composer. Styled exclusively with SpecPalette tokens.
// The outer panel chrome, ScrollView, and ask input composer remain mounted
// across rev updates so typing and background checks never lose focus or scroll.
func ProblemAgentPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return !ed.Playing() && ed.ProblemAgentPanelOpen()
	})
	askDraft := core.NewSignal(cx, "")

	type agentSnapshot struct {
		dark      bool
		lang      string
		hasProb   bool
		prob      assetcheck.Problem
		hasFix    bool
		fixID     string
		fixProb   assetcheck.Problem
		fixExpl   string
		fixDiff   []ProblemDiffLine
		hlLoc     string
		hlSnippet string
		questions []ProblemQuestionRecord
	}
	agentState := core.NewMemoFunc(cx, func() agentSnapshot {
		_ = rev.Get()
		prob, hasProb := ed.ActiveProblem()
		fix, hasFix := ed.ActiveProposedFix()
		if hasFix {
			prob = fix.Problem
			hasProb = true
		}
		hlLoc, hlSnippet := ed.HighlightedProblemLine()
		return agentSnapshot{
			dark:      cx.Dark(),
			lang:      ed.ResolvedLanguage(),
			hasProb:   hasProb,
			prob:      prob,
			hasFix:    hasFix,
			fixID:     fix.ID,
			fixProb:   fix.Problem,
			fixExpl:   fix.Explanation,
			fixDiff:   fix.Diff,
			hlLoc:     hlLoc,
			hlSnippet: hlSnippet,
			questions: ed.ProblemQuestions(),
		}
	}, func(a, b agentSnapshot) bool {
		return a.dark == b.dark &&
			a.lang == b.lang &&
			a.hasProb == b.hasProb &&
			a.prob == b.prob &&
			a.hasFix == b.hasFix &&
			a.fixID == b.fixID &&
			a.fixProb == b.fixProb &&
			a.fixExpl == b.fixExpl &&
			a.hlLoc == b.hlLoc &&
			a.hlSnippet == b.hlSnippet &&
			slices.Equal(a.fixDiff, b.fixDiff) &&
			slices.Equal(a.questions, b.questions)
	})

	submitQuestion := func() {
		prob, hasProb := ed.ActiveProblem()
		if fix, hasFix := ed.ActiveProposedFix(); hasFix {
			prob = fix.Problem
			hasProb = true
		}
		if !hasProb {
			return
		}
		q := strings.TrimSpace(askDraft.GetUntracked())
		if q == "" {
			return
		}
		askDraft.Set("")
		ed.report(ed.AskAboutProblem(prob, q))
	}

	bodyNode := core.Dynamic(func(cx *core.Scope) core.Node {
		s := agentState.Get()
		spec := SpecPaletteFor(s.dark)
		prob, hasProb := s.prob, s.hasProb
		hasFix := s.hasFix
		hlLoc, hlSnippet := s.hlLoc, s.hlSnippet
		questions := s.questions

		body := make([]core.Node, 0, 6)
		if !hasProb {
			body = append(body, core.Text{
				TestID:  "problem-agent-empty",
				Content: ed.T("problems.empty"),
				Style:   core.Style{FontSize: 12.5, Color: spec.TextFaint},
			})
		} else {
			// 1. Problem location card (matching Problem.dc.html lines 47-50).
			cardKids := []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						problemAlertIcon(spec.Problem().Fg, 14),
						core.Text{
							TestID:  "problem-card-banner",
							Content: ed.T("problems.foundBanner"),
							Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: spec.Problem().Fg},
						},
					},
				},
				core.Text{
					TestID:  "problem-card-location",
					Content: prob.Location(),
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						FontWeight: 500,
						Color:      spec.TextPrimary,
					},
				},
				core.Text{
					TestID:  "problem-card-message",
					Content: prob.Message,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						LineHeight: 17,
						Color:      spec.TextSecondary,
					},
				},
			}
			if hlLoc != "" && hlSnippet != "" {
				cardKids = append(cardKids, core.View{
					TestID: "problem-card-snippet",
					Style: core.Style{
						Padding:      core.Symmetric(4, 8),
						CornerRadius: 6,
						Background:   spec.Raised,
						BorderWidth:  1,
						BorderColor:  spec.LineRaised,
					},
					Children: []core.Node{
						core.Text{
							Content: fmt.Sprintf("%s  %s", hlLoc, strings.TrimSpace(hlSnippet)),
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   11,
								Color:      spec.TextMuted,
							},
						},
					},
				})
			}
			body = append(body, core.View{
				TestID: "problem-agent-card",
				Style: core.Style{
					Padding:      core.All(12),
					Gap:          6,
					CornerRadius: 12,
					Background:   spec.Problem().Fill,
					BorderWidth:  1,
					BorderColor:  spec.Problem().Edge,
				},
				Children: cardKids,
			})

			// 2. Agent explanation + proposed diff card (when a fix is proposed).
			if hasFix {
				body = append(body, core.Text{
					TestID:  "problem-fix-explanation",
					Content: s.fixExpl,
					Style: core.Style{
						FontSize:   12.5,
						LineHeight: 19,
						Color:      spec.TextSecondary,
					},
				})

				diffNodes := make([]core.Node, 0, len(s.fixDiff))
				for i, dl := range s.fixDiff {
					prefix := "  "
					fg := spec.TextMuted
					bg := core.Transparent
					switch dl.Kind {
					case "removed":
						prefix = "- "
						fg = spec.Removed.Fg
						bg = spec.Removed.Fill
					case "added":
						prefix = "+ "
						fg = spec.Added.Fg
						bg = spec.Added.Fill
					}
					diffNodes = append(diffNodes, core.View{
						TestID: fmt.Sprintf("problem-fix-diff-%d", i),
						Style: core.Style{
							Padding:      core.Symmetric(2, 6),
							CornerRadius: 4,
							Background:   bg,
						},
						Children: []core.Node{
							core.Text{
								Content: prefix + dl.Text,
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   11.5,
									LineHeight: 18,
									Color:      fg,
								},
							},
						},
					})
				}

				fixID := s.fixID
				targetProb := prob
				body = append(body, core.View{
					TestID: "problem-fix-card",
					Style: core.Style{
						CornerRadius: 12,
						Background:   spec.Raised,
						BorderWidth:  1,
						BorderColor:  spec.LineRaised,
						Overflow:     core.Hidden,
					},
					Children: []core.Node{
						core.View{
							Style:    core.Style{Padding: core.All(10), Gap: 2},
							Children: diffNodes,
						},
						core.View{
							Style: core.Style{
								Direction:   core.Row,
								AlignItems:  core.AlignCenter,
								Gap:         8,
								Padding:     core.All(10),
								Borders:     core.Edges{Top: 1},
								BorderColor: spec.Line,
							},
							Children: []core.Node{
								core.View{
									TestID:    "problem-fix-apply",
									Role:      core.RoleButton,
									Label:     ed.T("problems.applyFix"),
									Focusable: func() bool { return true },
									Style: core.Style{
										FlexGrow:       1,
										Height:         core.Pt(30),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     spec.Accent.Fg,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   spec.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										ed.report(ed.ApplyProposedFix(fixID))
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("problems.applyFix"),
											Style: core.Style{
												FontSize:   12.5,
												FontWeight: 600,
												Color:      spec.Accent.OnFill,
											},
										},
									},
								},
								core.View{
									TestID:    "problem-fix-show",
									Role:      core.RoleButton,
									Label:     ed.T("problems.showMe"),
									Focusable: func() bool { return true },
									Style: core.Style{
										Height:         core.Pt(30),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     spec.LineRaised,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   spec.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										ed.report(ed.ShowProblem(targetProb))
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("problems.showMe"),
											Style: core.Style{
												FontSize:   12.5,
												FontWeight: 500,
												Color:      spec.TextSecondary,
											},
										},
									},
								},
								core.View{
									TestID:    "problem-fix-ignore",
									Role:      core.RoleButton,
									Label:     ed.T("problems.ignore"),
									Focusable: func() bool { return true },
									Style: core.Style{
										Height:         core.Pt(30),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   spec.FocusRing(2),
									},
									OnPress: func(core.PressEvent) {
										ed.IgnoreProblem(targetProb)
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("problems.ignore"),
											Style: core.Style{
												FontSize: 12.5,
												Color:    spec.TextFaint,
											},
										},
									},
								},
							},
						},
					},
				})
			} else {
				targetProb := prob
				body = append(body, core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						core.View{
							TestID:    "problem-fix-show",
							Role:      core.RoleButton,
							Label:     ed.T("problems.showMe"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(30),
								Padding:        core.Symmetric(0, 12),
								CornerRadius:   8,
								Background:     spec.LineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   spec.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.report(ed.ShowProblem(targetProb))
							},
							Children: []core.Node{
								core.Text{
									Content: ed.T("problems.showMe"),
									Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: spec.TextSecondary},
								},
							},
						},
						core.View{
							TestID:    "problem-fix-ignore",
							Role:      core.RoleButton,
							Label:     ed.T("problems.ignore"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(30),
								Padding:        core.Symmetric(0, 12),
								CornerRadius:   8,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   spec.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.IgnoreProblem(targetProb)
							},
							Children: []core.Node{
								core.Text{
									Content: ed.T("problems.ignore"),
									Style:   core.Style{FontSize: 12.5, Color: spec.TextFaint},
								},
							},
						},
					},
				})
			}

			// 3. Always-visible safety principle note.
			body = append(body, core.Text{
				TestID:  "problem-agent-note",
				Content: ed.T("problems.agentNote"),
				Style: core.Style{
					FontSize:   11.5,
					LineHeight: 17,
					Color:      spec.TextFaint,
				},
			})

			// 4. Recent questions attached to this problem.
			for qi, q := range questions {
				if q.Problem != prob {
					continue
				}
				body = append(body, core.View{
					TestID: fmt.Sprintf("problem-question-%d", qi),
					Style: core.Style{
						Padding:      core.All(10),
						Gap:          4,
						CornerRadius: 10,
						Background:   spec.Raised,
						BorderWidth:  1,
						BorderColor:  spec.LineRaised,
					},
					Children: []core.Node{
						core.Text{
							Content: q.Question,
							Style:   core.Style{FontSize: 12, Color: spec.TextSecondary},
						},
						core.Text{
							Content: q.Problem.Location(),
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   10.5,
								Color:      spec.TextFaint,
							},
						},
					},
				})
			}
		}
		return core.View{
			Style:    core.Style{Padding: core.All(16), Gap: 14},
			Children: body,
		}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "problem-agent-panel",
				Style: dockedStyle(cx, core.Style{
					Width:      core.Pt(380),
					FlexShrink: 0,
					Borders:    core.Edges{Left: 1},
				}),
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Background:  p.Panel,
						BorderColor: p.Line,
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Height:     core.Pt(44),
							Padding:    core.Symmetric(0, 14),
							Borders:    core.Edges{Bottom: 1},
							FlexShrink: 0,
						},
						StyleFunc: func() core.Style {
							return core.Style{BorderColor: ed.SpecPalette(cx).Line}
						},
						Children: []core.Node{
							core.Text{
								TestID: "problem-agent-title",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.T("problems.agentHeader")
								},
								Style: core.Style{
									FontSize:   14,
									FontWeight: 600,
									FlexGrow:   1,
								},
								StyleFunc: func() core.Style {
									return core.Style{Color: ed.SpecPalette(cx).TextPrimary}
								},
							},
							core.View{
								TestID:    "problem-agent-close",
								Role:      core.RoleButton,
								Label:     ed.T("problems.closeAgentPanel"),
								Focusable: func() bool { return true },
								Style: core.Style{
									Width:          core.Pt(28),
									Height:         core.Pt(28),
									CornerRadius:   6,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								StyleFunc: func() core.Style {
									return core.Style{FocusVisible: ed.SpecPalette(cx).FocusRing(2)}
								},
								OnPress: func(core.PressEvent) {
									ed.SetProblemAgentPanelOpen(false)
								},
								Children: []core.Node{
									core.Canvas{
										Style: core.Style{Width: core.Pt(14), Height: core.Pt(14)},
										Draw: func(d *core.Drawing, _ core.Size) {
											d.SetStrokeColor(ed.SpecPalette(cx).TextFaint).SetLineWidth(1.6).SetLineCap(core.CapRound)
											d.BeginPath().MoveTo(3.5, 3.5).LineTo(10.5, 10.5).MoveTo(10.5, 3.5).LineTo(3.5, 10.5).Stroke()
										},
									},
								},
							},
						},
					},
					ui.ScrollView{
						Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
						Children: []core.Node{bodyNode},
					},
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
							Padding:    core.All(12),
							Borders:    core.Edges{Top: 1},
							FlexShrink: 0,
						},
						StyleFunc: func() core.Style {
							return core.Style{BorderColor: ed.SpecPalette(cx).Line}
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									FlexGrow:     1,
									MinWidth:     core.Pt(0),
									Height:       core.Pt(34),
									CornerRadius: 10,
									BorderWidth:  1,
								},
								StyleFunc: func() core.Style {
									p := ed.SpecPalette(cx)
									return core.Style{
										Background:  p.Raised,
										BorderColor: p.LineRaised,
									}
								},
								Children: []core.Node{
									ui.Input{
										TestID:      "problem-ask-input",
										Label:       ed.T("problems.askPlaceholder"),
										Placeholder: ed.T("problems.askPlaceholder"),
										Value:       askDraft.Get,
										OnChange:    askDraft.Set,
										OnSubmit:    func(s string) { askDraft.Set(s); submitQuestion() },
										Style: core.Style{
											FlexGrow:    1,
											MinWidth:    core.Pt(0),
											Height:      core.Pt(32),
											BorderWidth: 0,
											Background:  core.Transparent,
										},
									},
								},
							},
							core.View{
								TestID:    "problem-ask-send",
								Role:      core.RoleButton,
								Label:     ed.T("problems.askSend"),
								Focusable: func() bool { return true },
								Style: core.Style{
									Height:         core.Pt(32),
									Padding:        core.Symmetric(0, 12),
									CornerRadius:   8,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								StyleFunc: func() core.Style {
									p := ed.SpecPalette(cx)
									return core.Style{
										Background:   p.TextPrimary,
										FocusVisible: p.FocusRing(2),
									}
								},
								OnPress: func(core.PressEvent) {
									submitQuestion()
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											return ed.T("problems.askSend")
										},
										Style: core.Style{
											FontSize:   12.5,
											FontWeight: 600,
										},
										StyleFunc: func() core.Style {
											return core.Style{Color: ed.SpecPalette(cx).OnTextPrimary}
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
