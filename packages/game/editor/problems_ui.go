package editor

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// ProblemsView renders the Problems panel below the viewport while a project is
// open: one row per problem found by assetcheck.CheckWith (with the editor's
// component registry) and project.Check, showing its file, line and message.
// Problems use the Spec removed/problem token with the alert icon and warnings
// use the Spec warning token with its fill, edge, and alert icon. Pressing or
// activating a row via keyboard opens the scene or selects the file in the file
// browser. It renders nothing while no project is open.
func ProblemsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Project != nil && ed.ProjectPath != ""
	})
	type snapshot struct {
		problems []assetcheck.Problem
		checking bool
		errMsg   string
	}
	state := core.NewMemoFunc(cx, func() snapshot {
		_ = rev.Get()
		var errMsg string
		if err := ed.ProblemErr(); err != nil {
			errMsg = err.Error()
		}
		return snapshot{
			problems: ed.Problems(),
			checking: ed.CheckingProblems(),
			errMsg:   errMsg,
		}
	}, func(a, b snapshot) bool {
		return a.checking == b.checking && a.errMsg == b.errMsg && slices.Equal(a.problems, b.problems)
	})

	checkShortcut := FormatShortcut("mod+shift+b", cx.Env().OS())

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		s := state.Get()
		if s.errMsg != "" {
			retryBtn := specPanelButton(specPanelButtonSpec{
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
			return specStatusBanner("problems-error", false, s.errMsg, nil, retryBtn)
		}
		if s.checking && len(s.problems) == 0 {
			return specLoadingState("problems-loading", ed.T("problems.checking"))
		}
		if len(s.problems) == 0 {
			return specEmptyState("problems-empty", lucide.CircleCheck, specAdded, ed.T("problems.empty"), "", nil)
		}
		rows := make([]core.Node, 0, len(s.problems))
		for i, prob := range s.problems {
			p := prob
			loc := p.File
			if p.Line > 0 {
				loc += ":" + strconv.Itoa(p.Line)
			}
			isWarn := problemIsWarning(p)

			rowBg := specRaised
			rowBorder := specLineRaised
			rowHover := specLineRaised
			locColor := specTextStrong
			badgeLabel := ed.T("problems.error")
			if isWarn {
				rowBg = specWarningFill
				rowBorder = specWarningEdge
				rowHover = specWarningEdge
				locColor = specWarning
				badgeLabel = ed.T("problems.warning")
			}

			rowAria := fmt.Sprintf("%s: %s — %s", badgeLabel, loc, p.Message)
			rows = append(rows, specPanelRowButton(specPanelRowButtonSpec{
				TestID:  fmt.Sprintf("problems-row-%d", i),
				Label:   rowAria,
				Tooltip: "Open " + loc,
				Style: core.Style{
					Background:  rowBg,
					BorderColor: rowBorder,
					Hover:       &core.Style{Background: rowHover, BorderColor: rowBorder},
				},
				OnPress: func() {
					ed.report(ed.OpenProblem(p))
				},
				Children: []core.Node{
					specSeverityPill("", isWarn, badgeLabel),
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
						Content: p.Message,
						Style: core.Style{
							FontSize:   12.5,
							Color:      specText,
							FlexGrow:   1,
							FlexShrink: 1,
						},
					},
				},
			}))
		}
		return core.View{Style: core.Style{Gap: specSpace4}, Children: rows}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			headerIcon := core.Dynamic(func(*core.Scope) core.Node {
				s := state.Get()
				if s.checking {
					return ui.Spinner{
						Size:  specIconSize,
						Label: ed.T("problems.checking"),
						Tint:  func() core.Color { return specAccent },
					}
				}
				if s.errMsg != "" {
					return specIcon(lucide.TriangleAlert, specIconSize, specProblem)
				}
				if len(s.problems) == 0 {
					return specIcon(lucide.CircleCheck, specIconSize, specAdded)
				}
				hasErr := false
				for _, p := range s.problems {
					if !problemIsWarning(p) {
						hasErr = true
						break
					}
				}
				if hasErr {
					return specIcon(lucide.TriangleAlert, specIconSize, specProblem)
				}
				return specIcon(lucide.TriangleAlert, specIconSize, specWarning)
			})

			headerCounts := core.Dynamic(func(*core.Scope) core.Node {
				s := state.Get()
				var errs, warns int
				for _, p := range s.problems {
					if problemIsWarning(p) {
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
					pills = append(pills, specSeverityPill("", false, fmt.Sprintf("%d %s", errs, ed.T("problems.error"))))
				}
				if warns > 0 {
					pills = append(pills, specSeverityPill("", true, fmt.Sprintf("%d %s", warns, ed.T("problems.warning"))))
				}
				return core.View{
					Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: specSpace8},
					Children: pills,
				}
			})

			header := specPanelHeader(
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
						Color:      specTextStrong,
					},
				},
				headerCounts,
				core.View{Style: core.Style{FlexGrow: 1}},
				specPanelButton(specPanelButtonSpec{
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
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				ContentStyle: core.Style{
					Padding: core.Symmetric(specSpace8, specSpace16),
					Gap:     specSpace8,
				},
				Children: []core.Node{list},
			}

			return specPanelContainer("problems-panel", ed.T("problems.title", l10n.Param("count", 0)), 164, header, body)
		},
	}
}
