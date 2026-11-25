package editor

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ProblemsView renders the Problems panel below the viewport while a project is
// open: one row per problem found by assetcheck.CheckWith (with the editor's
// component registry) and project.Check, showing its file, line and message.
// Pressing a row opens the scene or selects the file in the file browser. It
// renders nothing while no project is open.
func ProblemsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
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

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		s := state.Get()
		if s.errMsg != "" {
			return core.Text{
				TestID:  "problems-error",
				Content: s.errMsg,
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		if len(s.problems) == 0 {
			msg := "No problems"
			if s.checking {
				msg = "Checking project…"
			}
			return core.Text{
				TestID:  "problems-empty",
				Content: msg,
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		rows := make([]core.Node, 0, len(s.problems))
		for i, prob := range s.problems {
			p := prob
			loc := p.File
			if p.Line > 0 {
				loc += ":" + strconv.Itoa(p.Line)
			}
			rows = append(rows, core.View{
				TestID: fmt.Sprintf("problems-row-%d", i),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(3, 6),
					CornerRadius: 4,
					Background:   colors.Surface(),
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.OpenProblem(p))
				},
				Children: []core.Node{
					core.Text{
						Content: loc,
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Text()},
					},
					core.Text{
						Content: p.Message,
						Style:   core.Style{FontSize: 11, Color: colors.Muted(), FlexGrow: 1},
					},
				},
			})
		}
		return core.View{Style: core.Style{Gap: 2}, Children: rows}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "problems-panel",
				Style: core.Style{
					Height:      core.Pt(140),
					Background:  colors.Inset(),
					Borders:     core.Edges{Top: 1},
					BorderColor: colors.Line(),
					Padding:     core.All(8),
					Gap:         6,
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							core.Text{
								TestID: "problems-title",
								ContentFunc: func() string {
									s := state.Get()
									if s.checking && len(s.problems) == 0 {
										return "Problems (checking…)"
									}
									return fmt.Sprintf("Problems (%d)", len(s.problems))
								},
								Style: core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
							},
							ui.Button{
								TestID:  "problems-refresh",
								Text:    "Check",
								Label:   "Re-run project and asset checks",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									return state.Get().checking
								},
								OnPress: func() {
									ed.CheckProblems()
								},
							},
						},
					},
					ui.ScrollView{
						Style:    core.Style{FlexGrow: 1},
						Children: []core.Node{list},
					},
				},
			}
		},
	}
}
