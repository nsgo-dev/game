package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// planHeaderPillView renders the centered agent-violet status pill in the top toolbar
// whenever an agent plan is active (Draft, Running, or Stopped), matching docs/design/editor/Plan.dc.html
// using exclusively SpecPalette tokens.
func planHeaderPillView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type pillKey struct {
		dark   bool
		phase  PlanPhase
		active bool
	}
	keyMemo := core.NewMemo(cx, func() pillKey {
		_ = rev.Get()
		p := ed.ActivePlan()
		if p == nil {
			return pillKey{}
		}
		return pillKey{
			dark:   ed.SpecPalette(cx).Dark,
			phase:  p.Phase,
			active: true,
		}
	})
	return core.Show{
		When: func() bool { return keyMemo.Get().active },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := keyMemo.Get()
				p := SpecPaletteFor(k.dark)
				running := k.phase == PlanPhaseRunning
				var dot core.Node
				if running {
					dot = core.View{
						Style: core.Style{
							Width:          core.Pt(11),
							Height:         core.Pt(11),
							CornerRadius:   6,
							Background:     ui.Alpha(p.Agent.Fg, 0.25),
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(7),
									Height:       core.Pt(7),
									CornerRadius: 4,
									Background:   p.Agent.Fg,
								},
							},
						},
					}
				} else {
					dot = core.View{
						Style: core.Style{
							Width:        core.Pt(7),
							Height:       core.Pt(7),
							CornerRadius: 4,
							Background:   p.Agent.Fg,
							FlexShrink:   0,
						},
					}
				}
				return core.View{
					TestID:     "plan-header-pill",
					Role:       core.RoleStatus,
					LiveRegion: core.LiveRegionPolite,
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Height:       core.Pt(28),
						Padding:      core.Symmetric(0, 12),
						Gap:          8,
						CornerRadius: 14,
						Background:   p.Agent.Fill,
						BorderWidth:  1,
						BorderColor:  p.Agent.Edge,
						FlexShrink:   1,
						MinWidth:     core.Pt(0),
					},
					Children: []core.Node{
						dot,
						core.Text{
							TestID: "plan-header-pill-text",
							ContentFunc: func() string {
								_ = rev.Get()
								return ed.PlanHeaderPillText()
							},
							MaxLines:      1,
							EllipsizeMode: core.EllipsizeTail,
							Style: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      p.Agent.Fg,
								FlexShrink: 1,
							},
						},
					},
				}
			})
		},
	}
}

// planViewportLegendView renders the top-left planned-ghost legend in the 2D viewport
// whenever an agent plan is active ("planned · not in the file yet", plus "agent working here" while running).
func planViewportLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type legendKey struct {
		dark    bool
		active  bool
		running bool
		lang    string
	}
	keyMemo := core.NewMemo(cx, func() legendKey {
		_ = rev.Get()
		if ed.Playing() || ed.ViewMode != ViewMode2D {
			return legendKey{}
		}
		pl := ed.ActivePlan()
		if pl == nil {
			return legendKey{}
		}
		return legendKey{
			dark:    ed.SpecPalette(cx).Dark,
			active:  true,
			running: pl.Phase == PlanPhaseRunning,
			lang:    ed.ResolvedLanguage(),
		}
	})
	return core.Show{
		When: func() bool { return keyMemo.Get().active },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := keyMemo.Get()
				p := SpecPaletteFor(k.dark)
				rows := []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: []core.Node{
							planLegendSwatch(p.Agent.Edge, ui.Alpha(p.Agent.Fg, 0.14), true),
							core.Text{
								Content: ed.T("plan.legend.planned"),
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextMuted,
								},
							},
						},
					},
				}
				if k.running {
					rows = append(rows, core.View{
						TestID: "plan-legend-working",
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: []core.Node{
							planLegendSwatch(p.Agent.Edge, ui.Alpha(p.Agent.Fg, 0.18), false),
							core.Text{
								Content: ed.T("plan.legend.working"),
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextMuted,
								},
							},
						},
					})
				}
				return core.View{
					TestID:        "plan-viewport-legend",
					PointerEvents: core.PointerEventsNone,
					Style: core.Style{
						Position:     core.Absolute,
						Left:         core.Pt(16),
						Top:          core.Pt(14),
						Padding:      core.Symmetric(6, 10),
						CornerRadius: 8,
						Background:   ui.Alpha(p.PillBg, 0.88),
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Gap:          6,
					},
					Children: rows,
				}
			})
		},
	}
}

func planLegendSwatch(stroke, fill core.Color, dashed bool) core.Node {
	return core.Canvas{
		Style: core.Style{
			Width:      core.Pt(12),
			Height:     core.Pt(12),
			FlexShrink: 0,
		},
		Draw: func(d *core.Drawing, size core.Size) {
			w, h := float32(size.Width), float32(size.Height)
			d.Save()
			d.SetFillColor(fill).FillRect(1, 1, w-2, h-2)
			d.SetStrokeColor(stroke).SetLineWidth(1.5)
			if dashed {
				d.SetLineDash(3, 2)
			}
			d.StrokeRect(1, 1, w-2, h-2)
			d.SetLineDash()
			d.Restore()
		},
	}
}

// planFilesFooterView renders the "FILES IN THIS PLAN" list pinned at the bottom of the Scene Tree sidebar
// whenever an agent plan is active, striking out files whose owning steps were removed.
func planFilesFooterView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type filesKey struct {
		dark   bool
		active bool
		seq    uint64
		lang   string
	}
	keyMemo := core.NewMemo(cx, func() filesKey {
		_ = rev.Get()
		pl := ed.ActivePlan()
		if pl == nil {
			return filesKey{}
		}
		return filesKey{
			dark:   ed.SpecPalette(cx).Dark,
			active: true,
			seq:    ed.PlanSeq(),
			lang:   ed.ResolvedLanguage(),
		}
	})
	return core.Show{
		When: func() bool { return keyMemo.Get().active },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := keyMemo.Get()
				p := SpecPaletteFor(k.dark)
				files := ed.ResolvedPlanFiles()
				activeCount := 0
				for _, f := range files {
					if f.InPlan {
						activeCount++
					}
				}
				rows := make([]core.Node, 0, len(files)+1)
				rows = append(rows, core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Padding:        core.Edges{Left: 4, Right: 4, Bottom: 4},
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("plan.filesHeader"),
							Style: core.Style{
								FontSize:      10.5,
								FontWeight:    600,
								LetterSpacing: 0.6,
								Color:         p.TextFaint,
							},
						},
						core.Text{
							TestID:  "plan-files-count",
							Content: strconv.Itoa(activeCount),
							Style: core.Style{
								FontSize:   11,
								FontWeight: 500,
								Color:      p.TextFaint,
							},
						},
					},
				})
				for idx, f := range files {
					pathCol := p.TextSecondary
					var deco core.TextDecoration
					if !f.InPlan {
						pathCol = p.TextFaint
						deco = core.TextDecorationLineThrough
					}
					tagCol := p.Changed.Fg
					switch f.Tag {
					case PlanFileNew:
						tagCol = p.Added.Fg
					case PlanFileDelete:
						tagCol = p.Removed.Fg
					}
					rows = append(rows, core.View{
						TestID: fmt.Sprintf("plan-file-row-%d", idx),
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Height:       core.Pt(26),
							Padding:      core.Symmetric(0, 6),
							Gap:          6,
							CornerRadius: 6,
						},
						Children: []core.Node{
							core.Text{
								Content:       f.Path,
								MaxLines:      1,
								EllipsizeMode: core.EllipsizeTail,
								Style: core.Style{
									FlexGrow:       1,
									FlexShrink:     1,
									MinWidth:       core.Pt(0),
									FontFamily:     core.FontMonospace,
									FontSize:       10.5,
									Color:          pathCol,
									TextDecoration: deco,
								},
							},
							core.Text{
								Content: string(f.Tag),
								Style: core.Style{
									FontSize:   10.5,
									Color:      tagCol,
									FlexShrink: 0,
								},
							},
						},
					})
				}
				return core.View{
					TestID: "plan-files-panel",
					Style: core.Style{
						Borders:     core.Edges{Top: 1},
						BorderColor: p.Line,
						Padding:     core.Edges{Top: 8, Bottom: 4, Left: 2, Right: 2},
						Gap:         2,
						FlexShrink:  0,
					},
					Children: rows,
				}
			})
		},
	}
}

// sceneTreePlannedNodesView renders the planned nodes section inside SceneTreeView for non-removed steps,
// showing dashed violet square markers and circular step badges matching docs/design/editor/Plan.dc.html.
func sceneTreePlannedNodesView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type nodesKey struct {
		dark   bool
		active bool
		seq    uint64
	}
	keyMemo := core.NewMemo(cx, func() nodesKey {
		_ = rev.Get()
		pl := ed.ActivePlan()
		if pl == nil {
			return nodesKey{}
		}
		return nodesKey{
			dark:   ed.SpecPalette(cx).Dark,
			active: true,
			seq:    ed.PlanSeq(),
		}
	})
	return core.Show{
		When: func() bool { return keyMemo.Get().active },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := keyMemo.Get()
				p := SpecPaletteFor(k.dark)
				allNodes := ed.ResolvedPlanTreeNodes()
				var planned []ResolvedPlanTreeNode
				for _, n := range allNodes {
					if !n.Existing {
						planned = append(planned, n)
					}
				}
				if len(planned) == 0 {
					return core.View{}
				}
				rows := make([]core.Node, 0, len(planned))
				for idx, n := range planned {
					rows = append(rows, core.View{
						TestID: fmt.Sprintf("scene-tree-planned-node-%d-%d", n.Step, idx),
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Height:       core.Pt(26),
							Padding:      core.Edges{Left: 18, Right: 6},
							Gap:          8,
							CornerRadius: 6,
						},
						Children: []core.Node{
							planLegendSwatch(p.Agent.Edge, ui.Alpha(p.Agent.Fg, 0.10), true),
							core.Text{
								Content:       n.Name,
								MaxLines:      1,
								EllipsizeMode: core.EllipsizeTail,
								Style: core.Style{
									FlexGrow:   1,
									FlexShrink: 1,
									MinWidth:   core.Pt(0),
									FontSize:   11.5,
									Color:      p.Agent.Fg,
								},
							},
							planStepSmallBadge(p, n.Step),
						},
					})
				}
				return core.View{
					TestID: "scene-tree-planned-nodes",
					Style: core.Style{
						Gap:        2,
						Padding:    core.Edges{Top: 4, Bottom: 4},
						FlexShrink: 0,
					},
					Children: rows,
				}
			})
		},
	}
}

func planStepSmallBadge(p SpecPalette, step int) core.Node {
	return core.View{
		Style: core.Style{
			Width:          core.Pt(16),
			Height:         core.Pt(16),
			CornerRadius:   8,
			Background:     p.Agent.Fill,
			BorderWidth:    1,
			BorderColor:    p.Agent.Edge,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FlexShrink:     0,
		},
		Children: []core.Node{
			core.Text{
				Content: strconv.Itoa(step),
				Style: core.Style{
					FontSize:   10,
					FontWeight: 600,
					Color:      p.Agent.Fg,
				},
			},
		},
	}
}

// agentPlanCardView renders the interactive Plan card inside AgentPanelView matching docs/design/editor/Plan.dc.html
// using exclusively SpecPalette tokens (Dark + Light) and memoized keys so the inline step note input never remounts on unrelated rev ticks.
func agentPlanCardView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], p SpecPalette) core.Node {
	type planCardKey struct {
		dark     bool
		lang     string
		seq      uint64
		hasPlan  bool
		editStep int
	}
	cardMemo := core.NewMemo(cx, func() planCardKey {
		_ = rev.Get()
		pl := ed.ActivePlan()
		if pl == nil {
			return planCardKey{}
		}
		return planCardKey{
			dark:     ed.SpecPalette(cx).Dark,
			lang:     ed.ResolvedLanguage(),
			seq:      ed.PlanSeq(),
			hasPlan:  true,
			editStep: pl.EditingStep,
		}
	})

	return core.Show{
		When: func() bool { return cardMemo.Get().hasPlan },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := cardMemo.Get()
				pal := SpecPaletteFor(k.dark)
				pl := ed.ActivePlan()
				if pl == nil {
					return core.View{}
				}
				targetOS := cx.Env().OS()
				approveShortcut := FormatShortcut("mod+enter", targetOS)
				draft := pl.Phase == PlanPhaseDraft
				running := pl.Phase == PlanPhaseRunning
				stopped := pl.Phase == PlanPhaseStopped

				sections := []core.Node{
					planCardHeaderView(ed, pl, pal),
				}

				if draft && len(pl.Questions) > 0 {
					sections = append(sections, planQuestionsSectionView(ed, pl, pal))
				}

				if !draft && len(pl.Questions) > 0 {
					if picked := ed.PlanPickedLine(); picked != "" {
						sections = append(sections, core.View{
							TestID: "plan-picked-banner",
							Style: core.Style{
								Padding:     core.Symmetric(8, 12),
								Borders:     core.Edges{Bottom: 1},
								BorderColor: pal.LineRaised,
							},
							Children: []core.Node{
								core.Text{
									TestID:  "plan-picked-line",
									Content: picked,
									Style: core.Style{
										FontSize: 12,
										Color:    pal.TextMuted,
									},
								},
							},
						})
					}
				}

				sections = append(sections, planStepsListView(ed, pl, pal))

				if draft && pl.EditingStep > 0 {
					if st := findPlanStepPtr(pl, pl.EditingStep); st != nil && !st.Removed {
						sections = append(sections, planStepEditorDrawerView(cx, ed, rev, pl, *st, pal))
					}
				}

				sections = append(sections, planCardFooterView(ed, pl, pal, draft, running, stopped, approveShortcut))

				return core.View{
					TestID: "agent-plan-card",
					Role:   core.RoleRegion,
					Label:  pl.Title,
					Style: core.Style{
						CornerRadius: 12,
						Background:   pal.Raised,
						BorderWidth:  1,
						BorderColor:  pal.LineRaised,
						Overflow:     core.Hidden,
					},
					Children: sections,
				}
			})
		},
	}
}

func planCardHeaderView(ed *Editor, pl *AgentPlan, p SpecPalette) core.Node {
	return core.View{
		TestID: "plan-card-header",
		Style: core.Style{
			Padding:     core.All(12),
			Gap:         4,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.LineRaised,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.List(),
						Style: core.Style{
							Width:      core.Pt(13),
							Height:     core.Pt(13),
							Color:      p.Agent.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						Content: ed.T("plan.badge"),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
						},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{
						Content: "plan_propose",
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11,
							Color:      p.TextFaint,
						},
					},
				},
			},
			core.Text{
				TestID:  "plan-card-title",
				Content: pl.Title,
				Style: core.Style{
					FontSize:   14,
					FontWeight: 600,
					Color:      p.TextPrimary,
				},
			},
			core.Text{
				TestID:  "plan-card-summary",
				Content: formatPlanSummary(pl),
				Style: core.Style{
					FontSize: 12,
					Color:    p.TextMuted,
				},
			},
		},
	}
}

func planQuestionsSectionView(ed *Editor, pl *AgentPlan, p SpecPalette) core.Node {
	unanswered := unansweredPlanQuestions(pl)
	var heading string
	switch unanswered {
	case 0:
		heading = ed.T("plan.questions.applied")
	case 1:
		heading = ed.T("plan.questions.one")
	default:
		heading = ed.T("plan.questions.many", l10n.Param("count", strconv.Itoa(unanswered)))
	}

	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Sparkles(),
					Style: core.Style{
						Width:      core.Pt(14),
						Height:     core.Pt(14),
						Color:      p.Agent.Fg,
						FlexShrink: 0,
					},
				},
				core.Text{
					TestID:  "plan-questions-title",
					Content: heading,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 500,
						Color:      p.TextPrimary,
					},
				},
			},
		},
	}

	for _, q := range pl.Questions {
		qCopy := q
		answered := qCopy.SelectedID != "" && pl.ReopenedQuestion != qCopy.ID
		if answered {
			chosen, _ := qCopy.SelectedOption()
			children = append(children, core.View{
				TestID: "plan-q-" + qCopy.ID + "-summary",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Padding:      core.Edges{Left: 8, Right: 4},
					Gap:          8,
					CornerRadius: 6,
					Background:   p.Inset,
				},
				Children: []core.Node{
					core.Text{
						Content: questionShortLabel(qCopy),
						Style: core.Style{
							Width:      core.Pt(48),
							FontSize:   12,
							Color:      p.TextFaint,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID:   "plan-q-" + qCopy.ID + "-answer",
						Content:  chosen.Label,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12,
							Color:      p.TextPrimary,
						},
					},
					core.View{
						TestID: "plan-q-" + qCopy.ID + "-change",
						Role:   core.RoleButton,
						Label:  ed.T("plan.question.change") + " " + questionShortLabel(qCopy),
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							_ = ed.ReopenPlanQuestion(qCopy.ID)
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("plan.question.change"),
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.Accent.Fg,
								},
							},
						},
					},
				},
			})
			continue
		}

		// Expanded question with option radio cards.
		optNodes := make([]core.Node, 0, len(qCopy.Options))
		for _, opt := range qCopy.Options {
			optCopy := opt
			selected := qCopy.SelectedID == optCopy.ID
			bg := p.Raised
			borderCol := p.LineRaised
			var borderW core.Points = 1
			if selected {
				bg = p.Accent.Fill
				borderCol = p.Accent.Edge
				borderW = 1.5
			}
			noteCol := p.TextFaint
			if optCopy.Suggested || strings.EqualFold(strings.TrimSpace(optCopy.Note), "suggested") {
				noteCol = p.Added.Fg
			}
			optChildren := make([]core.Node, 0, 3)
			if optCopy.Preview != nil {
				optChildren = append(optChildren, planOptionPreviewView(optCopy.Preview, p))
			}
			optChildren = append(optChildren, core.Text{
				Content: optCopy.Label,
				Style: core.Style{
					FontSize:   11.5,
					FontWeight: 500,
					Color:      p.TextPrimary,
				},
			})
			if optCopy.Note != "" {
				optChildren = append(optChildren, core.Text{
					Content: optCopy.Note,
					Style: core.Style{
						FontSize: 10.5,
						Color:    noteCol,
					},
				})
			}
			optNodes = append(optNodes, core.View{
				TestID:   "plan-q-" + qCopy.ID + "-opt-" + optCopy.ID,
				Role:     core.RoleRadio,
				Label:    optCopy.Label,
				Selected: func() bool { return selected },
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					MinWidth:     core.Pt(0),
					Padding:      core.All(6),
					Gap:          4,
					CornerRadius: 8,
					Background:   bg,
					BorderWidth:  borderW,
					BorderColor:  borderCol,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					_ = ed.AnswerPlanQuestion(qCopy.ID, optCopy.ID)
				},
				Children: optChildren,
			})
		}

		children = append(children, core.View{
			TestID: "plan-q-" + qCopy.ID,
			Style: core.Style{
				Gap: 6,
			},
			Children: []core.Node{
				core.Text{
					Content: qCopy.Prompt,
					Style: core.Style{
						FontSize: 12,
						Color:    p.TextSecondary,
					},
				},
				core.View{
					Role:  core.RoleRadioGroup,
					Label: qCopy.Prompt,
					Style: core.Style{
						Direction: core.Row,
						Gap:       6,
					},
					Children: optNodes,
				},
			},
		})
	}

	return core.View{
		TestID: "plan-questions-section",
		Style: core.Style{
			Padding:     core.All(12),
			Gap:         10,
			Background:  p.Panel,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.LineRaised,
		},
		Children: children,
	}
}

func planOptionPreviewView(pr *PlanOptionPreview, p SpecPalette) core.Node {
	if pr == nil {
		return core.View{}
	}
	switch pr.Kind {
	case "strip":
		segs := pr.Segments
		return core.Canvas{
			Style: core.Style{
				Height: core.Pt(10),
				Width:  core.Pct(100),
			},
			Draw: func(d *core.Drawing, size core.Size) {
				w, h := float32(size.Width), float32(size.Height)
				y := (h - 6) * 0.5
				d.Save()
				d.SetFillColor(p.LineRaised).FillRect(0, y, w, 6)
				for _, seg := range segs {
					if !seg.Active {
						continue
					}
					x0 := w * (seg.From / 100)
					x1 := w * (seg.To / 100)
					if x1 > x0 {
						d.SetFillColor(p.TextMuted).FillRect(x0, y, x1-x0, 6)
					}
				}
				d.Restore()
			},
		}
	default: // "room"
		split := pr.RoomSplitPercent
		if split <= 0 {
			split = 46
		}
		gatePct := pr.GatePercent
		leverPct := pr.LeverPercent
		return core.Canvas{
			Style: core.Style{
				Height:       core.Pt(34),
				Width:        core.Pct(100),
				CornerRadius: 6,
				Overflow:     core.Hidden,
			},
			Draw: func(d *core.Drawing, size core.Size) {
				w, h := float32(size.Width), float32(size.Height)
				d.Save()
				// Room background + planned room tint.
				d.SetFillColor(p.Inset).FillRect(0, 0, w, h)
				sx := w * (split / 100)
				d.SetFillColor(ui.Alpha(p.Agent.Fg, 0.12)).FillRect(sx, 0, w-sx, h)
				// Floor.
				const floorH float32 = 7
				d.SetFillColor(p.LineRaised).FillRect(0, h-floorH, w, floorH)
				// Knight + Door markers.
				d.SetFillColor(p.Changed.Fg).FillRect(w*0.10, h-floorH-9, 5, 9)
				d.SetFillColor(p.TextFaint).FillRect(w*0.40, h-floorH-11, 5, 11)
				// Planned gate (dashed agent violet).
				gx := w * (gatePct / 100)
				d.SetFillColor(ui.Alpha(p.Agent.Fg, 0.28)).FillRect(gx, h-floorH-16, 4, 16)
				d.SetStrokeColor(p.Agent.Edge).SetLineWidth(1).SetLineDash(2, 2)
				d.StrokeRect(gx, h-floorH-16, 4, 16)
				d.SetLineDash()
				// Planned lever.
				lx := w * (leverPct / 100)
				d.SetFillColor(p.Agent.Fg).FillRect(lx, h-floorH-8, 3, 8)
				d.Restore()
			},
		}
	}
}

func planStepsListView(ed *Editor, pl *AgentPlan, p SpecPalette) core.Node {
	draft := pl.Phase == PlanPhaseDraft
	running := pl.Phase == PlanPhaseRunning
	rows := make([]core.Node, 0, len(pl.Steps))

	for _, st := range pl.Steps {
		stepCopy := st
		active := running && pl.ActiveStep == stepCopy.Number
		expanded := draft && pl.EditingStep == stepCopy.Number

		var rowBg core.Color
		if expanded {
			rowBg = p.Accent.Fill
		} else if active {
			rowBg = p.Agent.Fill
		}

		// Leading step badge.
		var badge core.Node
		switch {
		case active:
			badge = core.View{
				Style: core.Style{
					Width:          core.Pt(20),
					Height:         core.Pt(20),
					CornerRadius:   10,
					Background:     p.Agent.Fill,
					BorderWidth:    1.5,
					BorderColor:    p.Agent.Edge,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(8),
							Height:       core.Pt(8),
							CornerRadius: 4,
							Background:   p.Agent.Fg,
						},
					},
				},
			}
		case stepCopy.Status == PlanStepDone:
			badge = core.View{
				Style: core.Style{
					Width:          core.Pt(20),
					Height:         core.Pt(20),
					CornerRadius:   10,
					Background:     p.Added.Fill,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Check(),
						Style: core.Style{
							Width:  core.Pt(11),
							Height: core.Pt(11),
							Color:  p.Added.Fg,
						},
					},
				},
			}
		default:
			badge = core.View{
				Style: core.Style{
					Width:          core.Pt(20),
					Height:         core.Pt(20),
					CornerRadius:   10,
					Background:     p.LineRaised,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.Text{
						Content: strconv.Itoa(stepCopy.Number),
						Style: core.Style{
							FontSize:   11,
							FontWeight: 600,
							Color:      p.TextSecondary,
						},
					},
				},
			}
		}

		titleCol := p.TextPrimary
		var titleDeco core.TextDecoration
		if stepCopy.Removed {
			titleCol = p.TextFaint
			titleDeco = core.TextDecorationLineThrough
		}

		metaItems := []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        4,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.FileText(),
						Style: core.Style{
							Width:  core.Pt(11),
							Height: core.Pt(11),
							Color:  p.TextMuted,
						},
					},
					core.Text{
						Content: resolvedPlanStepFilesLabel(stepCopy),
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   10.5,
							Color:      p.TextMuted,
						},
					},
				},
			},
		}
		if adds := resolvedPlanStepAdds(pl, stepCopy); adds != "" {
			metaItems = append(metaItems, core.Text{
				Content: adds,
				Style: core.Style{
					FontSize: 11,
					Color:    p.TextFaint,
				},
			})
		}
		if stepCopy.Permission != "" && !stepCopy.Removed {
			metaItems = append(metaItems, core.View{
				TestID: fmt.Sprintf("plan-step-perm-%d", stepCopy.Number),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(20),
					Padding:      core.Symmetric(0, 6),
					Gap:          4,
					CornerRadius: 6,
					Background:   p.Warning.Fill,
					BorderWidth:  1,
					BorderColor:  p.Warning.Edge,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Lock(),
						Style: core.Style{
							Width:  core.Pt(10),
							Height: core.Pt(10),
							Color:  p.Warning.Fg,
						},
					},
					core.Text{
						Content: stepCopy.Permission,
						Style: core.Style{
							FontSize:   10.5,
							FontWeight: 500,
							Color:      p.Warning.Fg,
						},
					},
				},
			})
		}

		bodyNodes := []core.Node{
			core.Text{
				TestID:  fmt.Sprintf("plan-step-title-%d", stepCopy.Number),
				Content: resolvedPlanStepTitle(pl, stepCopy),
				Style: core.Style{
					FontSize:       12.5,
					Color:          titleCol,
					TextDecoration: titleDeco,
				},
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: metaItems,
			},
		}
		if strings.TrimSpace(stepCopy.Note) != "" && !stepCopy.Removed {
			bodyNodes = append(bodyNodes, core.Text{
				TestID:  fmt.Sprintf("plan-step-note-badge-%d", stepCopy.Number),
				Content: ed.T("plan.step.notePrefix", l10n.Param("note", strings.TrimSpace(stepCopy.Note))),
				Style: core.Style{
					FontSize: 11,
					Color:    p.Agent.Fg,
				},
			})
		}

		rowChildren := []core.Node{
			badge,
			core.View{
				Style: core.Style{
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					Gap:        4,
				},
				Children: bodyNodes,
			},
		}

		if draft && !stepCopy.Removed {
			editBg := core.Color(0)
			editFg := p.TextFaint
			if expanded {
				editBg = p.LineRaised
				editFg = p.TextPrimary
			}
			rowChildren = append(rowChildren, core.View{
				Style: core.Style{
					Direction:  core.Row,
					Gap:        2,
					FlexShrink: 0,
				},
				Children: []core.Node{
					core.View{
						TestID: fmt.Sprintf("plan-step-edit-%d", stepCopy.Number),
						Role:   core.RoleButton,
						Label:  ed.T("plan.step.editLabel", l10n.Param("n", strconv.Itoa(stepCopy.Number))),
						Style: core.Style{
							Width:          core.Pt(28),
							Height:         core.Pt(28),
							CornerRadius:   6,
							Background:     editBg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							_ = ed.EditPlanStep(stepCopy.Number)
						},
						Children: []core.Node{
							core.SVGView{
								SVG: lucide.Pencil(),
								Style: core.Style{
									Width:  core.Pt(13),
									Height: core.Pt(13),
									Color:  editFg,
								},
							},
						},
					},
					core.View{
						TestID: fmt.Sprintf("plan-step-remove-%d", stepCopy.Number),
						Role:   core.RoleButton,
						Label:  ed.T("plan.step.removeLabel", l10n.Param("n", strconv.Itoa(stepCopy.Number))),
						Style: core.Style{
							Width:          core.Pt(28),
							Height:         core.Pt(28),
							CornerRadius:   6,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							_ = ed.RemovePlanStep(stepCopy.Number)
						},
						Children: []core.Node{
							core.SVGView{
								SVG: lucide.Trash(),
								Style: core.Style{
									Width:  core.Pt(13),
									Height: core.Pt(13),
									Color:  p.TextFaint,
								},
							},
						},
					},
				},
			})
		} else if stepCopy.Removed && !running {
			rowChildren = append(rowChildren, core.View{
				TestID: fmt.Sprintf("plan-step-restore-%d", stepCopy.Number),
				Role:   core.RoleButton,
				Label:  ed.T("plan.step.restore"),
				Style: core.Style{
					Direction:      core.Row,
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 8),
					Gap:            4,
					CornerRadius:   6,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					_ = ed.RestorePlanStep(stepCopy.Number)
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Undo2(),
						Style: core.Style{
							Width:  core.Pt(12),
							Height: core.Pt(12),
							Color:  p.Accent.Fg,
						},
					},
					core.Text{
						Content: ed.T("plan.step.restore"),
						Style: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      p.Accent.Fg,
						},
					},
				},
			})
		}

		rowStyle := core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignStart,
			Gap:          10,
			Padding:      core.All(8),
			CornerRadius: 8,
			Background:   rowBg,
		}
		if stepCopy.Removed {
			rowStyle.Opacity = core.Alpha(0.65)
		}

		rows = append(rows, core.View{
			TestID:   fmt.Sprintf("plan-step-row-%d", stepCopy.Number),
			Style:    rowStyle,
			Children: rowChildren,
		})
	}

	return core.View{
		TestID: "plan-steps-list",
		Style: core.Style{
			Padding: core.All(4),
			Gap:     2,
		},
		Children: rows,
	}
}

func planStepEditorDrawerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], _ *AgentPlan, st PlanStep, p SpecPalette) core.Node {
	stepNum := st.Number
	children := []core.Node{
		core.Text{
			TestID:  "plan-step-editor-title",
			Content: ed.T("plan.step.editHeading", l10n.Param("n", strconv.Itoa(stepNum))),
			Style: core.Style{
				FontSize:      11,
				FontWeight:    600,
				LetterSpacing: 0.6,
				Color:         p.TextFaint,
			},
		},
	}

	for _, f := range st.Fields {
		field := f
		children = append(children, core.View{
			TestID: "plan-field-row-" + field.Key,
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.Text{
					Content: field.Label,
					Style: core.Style{
						Width:    core.Pt(56),
						FontSize: 12,
						Color:    p.TextMuted,
					},
				},
				core.View{
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Padding:      core.All(2),
						Gap:          2,
						CornerRadius: 8,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.View{
							TestID: "plan-field-" + field.Key + "-dec",
							Role:   core.RoleButton,
							Label:  "Decrease " + field.Label,
							Style: core.Style{
								Width:          core.Pt(28),
								Height:         core.Pt(24),
								CornerRadius:   6,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								_ = ed.AdjustPlanStepField(stepNum, field.Key, -1)
							},
							Children: []core.Node{
								core.SVGView{
									SVG: lucide.Minus(),
									Style: core.Style{
										Width:  core.Pt(12),
										Height: core.Pt(12),
										Color:  p.TextSecondary,
									},
								},
							},
						},
						core.Text{
							TestID: "plan-field-" + field.Key + "-val",
							ContentFunc: func() string {
								_ = rev.Get()
								if cur := ed.ActivePlan(); cur != nil {
									if curSt := findPlanStepPtr(cur, stepNum); curSt != nil {
										for _, cf := range curSt.Fields {
											if cf.Key == field.Key {
												return strconv.Itoa(cf.Value)
											}
										}
									}
								}
								return strconv.Itoa(field.Value)
							},
							Style: core.Style{
								Width:      core.Pt(28),
								TextAlign:  core.TextAlignCenter,
								FontFamily: core.FontMonospace,
								FontSize:   11.5,
								Color:      p.TextPrimary,
							},
						},
						core.View{
							TestID: "plan-field-" + field.Key + "-inc",
							Role:   core.RoleButton,
							Label:  "Increase " + field.Label,
							Style: core.Style{
								Width:          core.Pt(28),
								Height:         core.Pt(24),
								CornerRadius:   6,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								_ = ed.AdjustPlanStepField(stepNum, field.Key, +1)
							},
							Children: []core.Node{
								core.SVGView{
									SVG: lucide.Plus(),
									Style: core.Style{
										Width:  core.Pt(12),
										Height: core.Pt(12),
										Color:  p.TextSecondary,
									},
								},
							},
						},
					},
				},
				core.Text{
					Content: field.Hint,
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextFaint,
					},
				},
			},
		})
	}

	placeholder := strings.TrimSpace(st.NotePlaceholder)
	if placeholder == "" {
		placeholder = ed.T("plan.step.notePlaceholder")
	}
	children = append(children, core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
		},
		Children: []core.Node{
			specInputField(cx, ed, ui.Input{
				TestID:      "plan-step-note-input",
				Label:       ed.T("plan.step.noteLabel"),
				Placeholder: placeholder,
				Value: func() string {
					_ = rev.Get()
					if cur := ed.ActivePlan(); cur != nil {
						if curSt := findPlanStepPtr(cur, stepNum); curSt != nil {
							return curSt.Note
						}
					}
					return ""
				},
				OnChange: func(s string) {
					_ = ed.SetPlanStepNote(stepNum, s)
				},
				OnSubmit: func(s string) {
					_ = ed.SetPlanStepNote(stepNum, s)
					_ = ed.EditPlanStep(0)
				},
				Style: core.Style{
					FlexGrow: 1,
					MinWidth: core.Pt(0),
					Height:   core.Pt(30),
					FontSize: 12,
					Color:    p.TextSecondary,
				},
			}),
			core.View{
				TestID: "plan-step-edit-done",
				Role:   core.RoleButton,
				Label:  ed.T("plan.step.done"),
				Style: core.Style{
					Height:         core.Pt(30),
					Padding:        core.Symmetric(0, 12),
					CornerRadius:   8,
					Background:     p.LineRaised,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					_ = ed.EditPlanStep(0)
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("plan.step.done"),
						Style: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      p.TextPrimary,
						},
					},
				},
			},
		},
	})

	return core.View{
		TestID: "plan-step-editor",
		Style: core.Style{
			Margin:       core.Edges{Left: 12, Right: 12, Bottom: 12},
			Padding:      core.All(12),
			Gap:          8,
			CornerRadius: 8,
			Background:   p.Panel,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: children,
	}
}

func planCardFooterView(ed *Editor, pl *AgentPlan, p SpecPalette, draft, running, stopped bool, approveShortcut string) core.Node {
	var children []core.Node
	switch {
	case draft:
		permSum := ed.EvaluatePlanPermissions(pl)
		canApprove, approveTip := canApprovePlan(pl)
		approveBg := p.Accent.Fg
		approveFg := p.Accent.OnFill
		approveKeyFg := ui.Alpha(p.Accent.OnFill, 0.85)
		if !canApprove {
			approveBg = p.LineRaised
			approveFg = p.TextFaint
			approveKeyFg = p.TextFaint
		}
		children = append(children,
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					Gap:        4,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "plan-footer-reason",
						Content: ed.T("plan.footer.nothingChanges") + " " + permSum.Reason + " ·",
						Style: core.Style{
							FontSize:   11.5,
							Color:      p.TextMuted,
							FlexShrink: 1,
						},
					},
					core.View{
						TestID: "plan-permissions-link",
						Role:   core.RoleButton,
						Label:  ed.T("plan.footer.permissionsLink"),
						Style: core.Style{
							Height:         core.Pt(22),
							Padding:        core.Symmetric(0, 4),
							CornerRadius:   4,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.OpenSettingsPage(SettingsPagePermissions)
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("plan.footer.permissionsLink"),
								Style: core.Style{
									FontSize:   11.5,
									FontWeight: 500,
									Color:      p.Accent.Fg,
								},
							},
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
						TestID: "plan-just-do-it",
						Role:   core.RoleButton,
						Label:  ed.T("plan.action.justDoIt"),
						Style: core.Style{
							Height:         core.Pt(32),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Background:     p.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if _, err := ed.JustDoItAgentPlan(); err != nil {
								ed.report(err)
							}
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("plan.action.justDoIt"),
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      p.TextPrimary,
								},
							},
						},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.View{
						TestID:   "plan-approve",
						Role:     core.RoleButton,
						Label:    approveTip,
						Disabled: func() bool { return !canApprove },
						Style: core.Style{
							Direction:      core.Row,
							Height:         core.Pt(32),
							Padding:        core.Symmetric(0, 14),
							Gap:            8,
							CornerRadius:   8,
							Background:     approveBg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if !canApprove {
								return
							}
							if _, err := ed.ApproveAgentPlan(); err != nil {
								ed.report(err)
							}
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("plan.action.approve"),
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 600,
									Color:      approveFg,
								},
							},
							core.Text{
								Content: approveShortcut,
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   11,
									Color:      approveKeyFg,
								},
							},
						},
					},
				},
			},
		)

	case running:
		liveTotal := max(1, livePlanStepCount(pl))
		pos := max(1, liveStepPosition(pl, pl.ActiveStep))
		children = append(children,
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
							Background:   p.Agent.Fg,
							FlexShrink:   0,
						},
					},
					core.Text{
						TestID: "plan-running-line",
						Content: ed.T("plan.running.line",
							l10n.Param("step", strconv.Itoa(pos)),
							l10n.Param("total", strconv.Itoa(liveTotal))),
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12.5,
							Color:      p.TextPrimary,
						},
					},
					core.View{
						TestID: "plan-stop",
						Role:   core.RoleButton,
						Label:  ed.T("plan.action.stop"),
						Style: core.Style{
							Direction:      core.Row,
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							Gap:            6,
							CornerRadius:   8,
							Background:     p.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if _, err := ed.StopAgentPlan(); err != nil {
								ed.report(err)
							}
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(9),
									Height:       core.Pt(9),
									CornerRadius: 2,
									Background:   p.TextPrimary,
								},
							},
							core.Text{
								Content: ed.T("plan.action.stop"),
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      p.TextPrimary,
								},
							},
						},
					},
				},
			},
			core.Text{
				Content: ed.T("plan.running.reviewNote"),
				Style: core.Style{
					FontSize: 11.5,
					Color:    p.TextFaint,
				},
			},
		)

	case stopped:
		stoppedText := ed.T("plan.stopped.nothingWritten")
		if !pl.NothingWritten {
			stoppedText = ed.T("plan.stopped.partial",
				l10n.Param("done", strconv.Itoa(pl.WrittenSteps)),
				l10n.Param("total", strconv.Itoa(max(1, livePlanStepCount(pl)))))
		}
		children = append(children,
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "plan-stopped-line",
						Content: stoppedText,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12,
							Color:      p.TextMuted,
						},
					},
					core.View{
						TestID: "plan-edit-again",
						Role:   core.RoleButton,
						Label:  ed.T("plan.action.editPlan"),
						Style: core.Style{
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Background:     p.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if err := ed.EditStoppedPlan(); err != nil {
								ed.report(err)
							}
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("plan.action.editPlan"),
								Style: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextPrimary,
								},
							},
						},
					},
					core.View{
						TestID: "plan-run-again",
						Role:   core.RoleButton,
						Label:  ed.T("plan.action.runAgain"),
						Style: core.Style{
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Background:     p.Accent.Fg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
							FocusVisible:   p.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if _, err := ed.RunStoppedPlanAgain(); err != nil {
								ed.report(err)
							}
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("plan.action.runAgain"),
								Style: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      p.Accent.OnFill,
								},
							},
						},
					},
				},
			},
		)
	}

	return core.View{
		TestID: "plan-card-footer",
		Style: core.Style{
			Padding:     core.All(12),
			Gap:         8,
			Borders:     core.Edges{Top: 1},
			BorderColor: p.LineRaised,
			Background:  p.Raised,
		},
		Children: children,
	}
}

// agentTurnPlanReviewView renders the completed/reviewed plan step checklist inside an AgentTurn card,
// listing every plan step and marking which were done, skipped/removed, or failed.
func agentTurnPlanReviewView(ed *Editor, turnIdx int, review *AgentPlanReview, p SpecPalette) core.Node {
	if review == nil || len(review.Steps) == 0 {
		return core.View{}
	}
	done, total := review.DoneCount()
	rows := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("plan.review.title"),
					Style: core.Style{
						FontSize:      11,
						FontWeight:    600,
						LetterSpacing: 0.6,
						Color:         p.TextFaint,
					},
				},
				core.Text{
					TestID: fmt.Sprintf("agent-turn-plan-review-count-%d", turnIdx),
					Content: ed.T("plan.review.doneCount",
						l10n.Param("done", strconv.Itoa(done)),
						l10n.Param("total", strconv.Itoa(total))),
					Style: core.Style{
						FontSize: 11,
						Color:    p.Added.Fg,
					},
				},
			},
		},
	}

	for _, st := range review.Steps {
		var icon core.Node
		textCol := p.TextSecondary
		var deco core.TextDecoration
		switch {
		case st.Removed || st.Status == PlanStepSkipped:
			textCol = p.TextFaint
			deco = core.TextDecorationLineThrough
			icon = core.View{
				Style: core.Style{
					Width:        core.Pt(14),
					Height:       core.Pt(14),
					CornerRadius: 7,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FlexShrink:   0,
				},
			}
		case st.Status == PlanStepDone:
			icon = core.View{
				Style: core.Style{
					Width:          core.Pt(14),
					Height:         core.Pt(14),
					CornerRadius:   7,
					Background:     p.Added.Fill,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Check(),
						Style: core.Style{
							Width:  core.Pt(9),
							Height: core.Pt(9),
							Color:  p.Added.Fg,
						},
					},
				},
			}
		case st.Status == PlanStepFailed:
			icon = core.View{
				Style: core.Style{
					Width:          core.Pt(14),
					Height:         core.Pt(14),
					CornerRadius:   7,
					Background:     p.Removed.Fill,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.X(),
						Style: core.Style{
							Width:  core.Pt(9),
							Height: core.Pt(9),
							Color:  p.Removed.Fg,
						},
					},
				},
			}
		default:
			textCol = p.TextFaint
			icon = core.View{
				Style: core.Style{
					Width:        core.Pt(14),
					Height:       core.Pt(14),
					CornerRadius: 7,
					BorderWidth:  1.5,
					BorderColor:  p.Agent.Edge,
					FlexShrink:   0,
				},
			}
		}

		rows = append(rows, core.View{
			TestID: fmt.Sprintf("agent-turn-plan-review-step-%d-%d", turnIdx, st.Number),
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				icon,
				core.Text{
					Content: fmt.Sprintf("%d. %s", st.Number, st.Title),
					Style: core.Style{
						FlexGrow:       1,
						FlexShrink:     1,
						MinWidth:       core.Pt(0),
						FontSize:       12,
						Color:          textCol,
						TextDecoration: deco,
					},
				},
			},
		})
	}

	return core.View{
		TestID: fmt.Sprintf("agent-turn-plan-review-%d", turnIdx),
		Style: core.Style{
			CornerRadius: 10,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(10),
			Gap:          6,
		},
		Children: rows,
	}
}
