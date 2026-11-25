package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints for agent-accented cards and buttons (base color is Spec.dc.html `agent` #9B7BFF).
var (
	specAgentTint     = core.RGBA(0x9B7BFF, 0.12)
	specAgentCardTint = core.RGBA(0x9B7BFF, 0.06)
	specAgentEdge     = core.RGBA(0x9B7BFF, 0.45)
	specAccentEdge    = core.RGBA(0x3B7BFF, 0.55)
	specChangedTint   = core.RGBA(0xE9A84A, 0.18)
	specProblemTint   = core.RGBA(0xFF6B6B, 0.14)
)

// MergeConflictView renders the full-window Scene Merge Conflict screen matching
// docs/design/editor/VersionControl.dc.html (screen 2) and using only Spec.dc.html tokens.
func MergeConflictView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	keyOpt := core.KeyOptions{When: core.KeyNotEditing}
	cx.OnKeyWith("up", keyOpt, func() {
		if ed.MergeConflictOpen() {
			ed.StepMergeConflictFocus(-1)
		}
	})
	cx.OnKeyWith("down", keyOpt, func() {
		if ed.MergeConflictOpen() {
			ed.StepMergeConflictFocus(1)
		}
	})
	cx.OnKeyWith("1", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.FocusedID != "" {
			ed.report(ed.ResolveMergeConflict(s.FocusedID, MergeChoiceAgent))
		}
	})
	cx.OnKeyWith("a", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.FocusedID != "" {
			ed.report(ed.ResolveMergeConflict(s.FocusedID, MergeChoiceAgent))
		}
	})
	cx.OnKeyWith("2", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.FocusedID != "" {
			ed.report(ed.ResolveMergeConflict(s.FocusedID, MergeChoiceMine))
		}
	})
	cx.OnKeyWith("m", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.FocusedID != "" {
			ed.report(ed.ResolveMergeConflict(s.FocusedID, MergeChoiceMine))
		}
	})
	cx.OnKeyWith("3", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.FocusedID != "" {
			ed.report(ed.ResolveMergeConflict(s.FocusedID, MergeChoiceTheirs))
		}
	})
	cx.OnKeyWith("t", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.FocusedID != "" {
			ed.report(ed.ResolveMergeConflict(s.FocusedID, MergeChoiceTheirs))
		}
	})
	cx.OnKeyWith("e", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil {
			if item := s.FocusedItem(); item != nil {
				ed.report(ed.SetMergeCustomEditing(item.ID, !item.EditingCustom))
			}
		}
	})
	cx.OnKeyWith("mod+enter", keyOpt, func() {
		if s := ed.MergeSession(); ed.MergeConflictOpen() && s != nil && s.AllResolved() && !s.Finished {
			ed.report(ed.FinishMerge())
		}
	})

	mergeRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.mergeStateRev()
	})

	return core.View{
		TestID: "scene-merge-conflict",
		Role:   core.RoleRegion,
		Label:  "Scene merge conflict",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			mergeHeaderView(cx, ed, mergeRev),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					mergeSidebarView(cx, ed, mergeRev),
					mergeCenterView(cx, ed, mergeRev),
					mergeInspectorView(cx, ed, mergeRev),
				},
			},
		},
	}
}

func mergeHeaderView(cx *core.Scope, ed *Editor, mergeRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = mergeRev.Get()
		sess := ed.MergeSession()
		if sess == nil {
			return core.View{}
		}
		total := len(sess.Conflicts)
		remaining := sess.RemainingCount()
		allDone := sess.AllResolved()

		pillBg := specWarningFill
		pillBorder := specWarningEdge
		pillFg := specWarning
		pillIcon := lucide.GitMerge
		progressText := fmt.Sprintf("Merging %s into %s · %d of %d objects need you",
			sess.Theirs.Branch, sess.Mine.Branch, remaining, total)
		if sess.Finished {
			pillBg = specAddedFill
			pillBorder = specAddedEdge
			pillFg = specAdded
			pillIcon = lucide.Check
			if sess.CommitSHA != "" {
				progressText = fmt.Sprintf("Merged %s into %s · %s", sess.Theirs.Branch, sess.Mine.Branch, sess.CommitSHA)
			} else {
				progressText = fmt.Sprintf("Merged %s into %s · verified", sess.Theirs.Branch, sess.Mine.Branch)
			}
		} else if allDone {
			progressText = fmt.Sprintf("Merging %s into %s · ready to finish", sess.Theirs.Branch, sess.Mine.Branch)
		}

		finishLabel := fmt.Sprintf("Finish merge · %d left", remaining)
		finishBg := specLine
		finishFg := specTextMuted
		var finishBorder core.Color
		var finishBorderW core.Points
		if sess.Finished {
			finishLabel = "Merged"
			finishBg = specAddedFill
			finishFg = specAdded
			finishBorder = specAddedEdge
			finishBorderW = 1
		} else if allDone {
			finishLabel = "Finish merge"
			finishBg = specAccent
			finishFg = specBg // Spec contrast table: #101216 on #3B7BFF (4.88:1 AA).
		}

		rightActions := []core.Node{}
		if sess.Finished {
			rightActions = append(rightActions, ui.Button{
				TestID:  "merge-back-to-editor",
				Text:    "Back to editor",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specLineRaised },
				TextStyle: core.Style{
					FontSize:   12,
					FontWeight: 500,
					Color:      specText,
				},
				Style: core.Style{
					Height:       core.Pt(30),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 12),
				},
				OnPress: func() {
					ed.CloseMergeConflict()
				},
			})
		} else {
			rightActions = append(rightActions, ui.Button{
				TestID:  "merge-abort",
				Text:    "Abort merge",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specLineRaised },
				TextStyle: core.Style{
					FontSize:   12,
					FontWeight: 500,
					Color:      specText,
				},
				Style: core.Style{
					Height:       core.Pt(30),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 12),
				},
				OnPress: func() {
					ed.report(ed.AbortMerge())
				},
			})
		}
		rightActions = append(rightActions, ui.Button{
			TestID:  "merge-finish",
			Text:    finishLabel,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Disabled: func() bool {
				return !allDone || sess.Finished
			},
			Color: func() core.Color { return finishBg },
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 600,
				Color:      finishFg,
			},
			Style: core.Style{
				Height:       core.Pt(30),
				CornerRadius: 8,
				Padding:      core.Symmetric(0, 14),
				BorderWidth:  finishBorderW,
				BorderColor:  finishBorder,
			},
			OnPress: func() {
				if allDone && !sess.Finished {
					ed.report(ed.FinishMerge())
				}
			},
		})

		return core.View{
			TestID: "merge-header",
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Height:         core.Pt(44),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 12),
				Gap:            12,
				Background:     specBg,
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    specLine,
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
								Width:        core.Pt(22),
								Height:       core.Pt(22),
								CornerRadius: 6,
								Background:   specAccent,
							},
						},
						core.Text{
							Content: sess.ProjectName,
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: 600,
								Color:      specTextStrong,
							},
						},
						core.Text{
							Content: "/",
							Style: core.Style{
								FontSize: 12.5,
								Color:    specTextMuted,
							},
						},
						core.Text{
							Content: sess.SceneName,
							Style: core.Style{
								FontSize: 12.5,
								Color:    specTextSecondary,
							},
						},
					},
				},
				core.View{
					TestID: "merge-progress-pill",
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Height:       core.Pt(28),
						Padding:      core.Symmetric(0, 12),
						Gap:          8,
						CornerRadius: 14,
						Background:   pillBg,
						BorderWidth:  1,
						BorderColor:  pillBorder,
					},
					Children: []core.Node{
						core.Icon{
							SVG: pillIcon(),
							Style: core.Style{
								Width:  core.Pt(13),
								Height: core.Pt(13),
								Color:  pillFg,
							},
						},
						core.Text{
							TestID:  "merge-progress-text",
							Content: progressText,
							Style: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      pillFg,
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
					Children: rightActions,
				},
			},
		}
	})
}

func mergeSidebarView(cx *core.Scope, ed *Editor, mergeRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = mergeRev.Get()
		sess := ed.MergeSession()
		if sess == nil {
			return core.View{}
		}
		total := len(sess.Conflicts)
		resolved := sess.ResolvedCount()

		conflictRows := make([]core.Node, 0, total)
		for _, item := range sess.Conflicts {
			id := item.ID
			focused := sess.FocusedID == id
			dotColor := specChanged
			chipText := "Unresolved"
			chipBg := specWarningFill
			chipBorder := specWarningEdge
			var chipBorderW core.Points = 1
			chipFg := specWarning
			if item.Choice != MergeChoiceUnresolved {
				dotColor = specTextMuted
				chipBg = specLine
				chipBorderW = 0
				chipFg = specText
				switch item.Choice {
				case MergeChoiceAgent:
					chipText = "Agent"
				case MergeChoiceMine:
					chipText = "Mine"
				case MergeChoiceTheirs:
					chipText = "Theirs"
				case MergeChoiceCustom:
					chipText = "Custom"
				}
			}

			var rowBg core.Color
			var rowBorder core.Color
			var rowBorderW core.Points
			if focused {
				rowBg = specAccentTint
				rowBorder = specAccentEdge
				rowBorderW = 1
			}

			conflictRows = append(conflictRows, core.View{
				TestID: "merge-conflict-" + id,
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("Conflict %s (%s): %s", item.Name, chipText, item.Summary),
				Style: core.Style{
					Width:        core.Pct(100),
					Padding:      core.All(8),
					Gap:          4,
					CornerRadius: 8,
					Background:   rowBg,
					BorderWidth:  rowBorderW,
					BorderColor:  rowBorder,
					FocusVisible: specRowFocusRingStyle,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.FocusMergeConflict(id))
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
									FlexShrink:   0,
									CornerRadius: 2,
									Background:   dotColor,
								},
							},
							core.Text{
								Content: item.Name,
								Style: core.Style{
									FlexGrow:   1,
									FlexBasis:  core.Pt(0),
									FontSize:   12.5,
									FontWeight: 500,
									Color:      specTextStrong,
								},
							},
							core.View{
								Style: core.Style{
									Height:         core.Pt(20),
									Padding:        core.Symmetric(0, 8),
									CornerRadius:   10,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									Background:     chipBg,
									BorderWidth:    chipBorderW,
									BorderColor:    chipBorder,
								},
								Children: []core.Node{
									core.Text{
										TestID:  "merge-chip-" + id,
										Content: chipText,
										Style: core.Style{
											FontSize: 11,
											Color:    chipFg,
										},
									},
								},
							},
						},
					},
					core.Text{
						Content: item.Summary,
						Style: core.Style{
							Padding:  core.Edges{Left: 15},
							FontSize: 11.5,
							Color:    specTextMuted,
						},
					},
				},
			})
		}

		autoRows := make([]core.Node, 0, len(sess.AutoMerged))
		for i, a := range sess.AutoMerged {
			markColor := specChanged
			if a.Action == "added" {
				markColor = specAdded
			} else if a.Action == "deleted" {
				markColor = specProblem
			}
			autoRows = append(autoRows, core.View{
				TestID: fmt.Sprintf("merge-auto-%d", i),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Height:     core.Pt(28),
					Padding:    core.Symmetric(0, 8),
					Gap:        8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(8),
							Height:       core.Pt(8),
							FlexShrink:   0,
							CornerRadius: 2,
							BorderWidth:  1.5,
							BorderColor:  markColor,
						},
					},
					core.Text{
						Content: a.Name,
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							FontSize:  12.5,
							Color:     specTextSecondary,
						},
					},
					core.Text{
						Content: a.SideLabel,
						Style: core.Style{
							FontSize: 11.5,
							Color:    specTextMuted,
						},
					},
				},
			})
		}

		autoCountLabel := fmt.Sprintf("%d objects", len(sess.AutoMerged))
		if len(sess.AutoMerged) == 1 {
			autoCountLabel = "1 object"
		}

		return core.View{
			TestID: "merge-sidebar",
			Style: core.Style{
				Width:       core.Pt(300),
				FlexShrink:  0,
				Height:      core.Pct(100),
				Background:  specBg,
				Borders:     core.Edges{Right: 1},
				BorderColor: specLine,
				Padding:     core.All(8),
				Gap:         4,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Height:         core.Pt(28),
						JustifyContent: core.JustifyCenter,
						Padding:        core.Symmetric(0, 8),
					},
					Children: []core.Node{
						core.Text{
							TestID:  "merge-scene-path",
							Content: nonEmpty(sess.ScenePath, sess.SceneName+".scene.yaml"),
							Style: core.Style{
								FontFamily: "monospace",
								FontSize:   11.5,
								Color:      specTextStrong,
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignEnd,
						JustifyContent: core.JustifySpaceBetween,
						Height:         core.Pt(28),
						Padding:        core.Edges{Left: 8, Right: 8, Bottom: 4},
					},
					Children: []core.Node{
						core.Text{
							Content: "CONFLICTS",
							Style: core.Style{
								FontSize:      11,
								FontWeight:    600,
								LetterSpacing: 0.7,
								Color:         specTextMuted,
							},
						},
						core.Text{
							TestID:  "merge-resolved-count",
							Content: fmt.Sprintf("%d of %d resolved", resolved, total),
							Style: core.Style{
								FontSize:   11,
								FontWeight: 500,
								Color:      specTextMuted,
							},
						},
					},
				},
				ui.ScrollView{
					TestID:       "merge-sidebar-scroll",
					Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
					ContentStyle: core.Style{Gap: 4},
					EdgeColor:    func() core.Color { return specBg },
					Children: append(
						conflictRows,
						core.View{
							Style: core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignEnd,
								JustifyContent: core.JustifySpaceBetween,
								Height:         core.Pt(36),
								Padding:        core.Edges{Left: 8, Right: 8, Bottom: 4},
							},
							Children: []core.Node{
								core.Text{
									Content: "MERGED BY ID",
									Style: core.Style{
										FontSize:      11,
										FontWeight:    600,
										LetterSpacing: 0.7,
										Color:         specTextMuted,
									},
								},
								core.Text{
									TestID:  "merge-auto-count",
									Content: autoCountLabel,
									Style: core.Style{
										FontSize:   11,
										FontWeight: 500,
										Color:      specTextMuted,
									},
								},
							},
						},
						core.View{
							TestID:   "merge-auto-list",
							Style:    core.Style{Gap: 2},
							Children: autoRows,
						},
					),
				},
				ui.Button{
					TestID:  "merge-take-all-agent",
					Text:    fmt.Sprintf("Take the agent's suggestion for all %d", total),
					Icon:    lucide.Sparkles,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return specAgentTint },
					Tint:    func() core.Color { return specAgent },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      specTextStrong,
					},
					Style: core.Style{
						Width:          core.Pct(100),
						Height:         core.Pt(32),
						JustifyContent: core.JustifyCenter,
						CornerRadius:   8,
						BorderWidth:    1,
						BorderColor:    specAgentEdge,
					},
					OnPress: func() {
						ed.report(ed.TakeAllAgentMergeSuggestions())
					},
				},
			},
		}
	})
}

func mergeCenterView(cx *core.Scope, ed *Editor, mergeRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = mergeRev.Get()
		sess := ed.MergeSession()
		if sess == nil {
			return core.View{}
		}
		focused := sess.FocusedItem()
		var focusedChoice MergeChoice
		focusedID := ""
		if focused != nil {
			focusedChoice = focused.Choice
			focusedID = focused.ID
		}

		sideCard := func(testID, canvasID, title string, meta MergeSideMeta, selected bool, doc *sceneformat.SceneDoc, built *sceneformat.BuiltScene, side string, onPick func()) core.Node {
			bg := specPanel
			border := specLine
			var bw core.Points = 1
			if selected {
				bg = specRaised
				border = specAccent
				bw = 1.5
			}
			authorMeta := strings.TrimSpace(meta.Author)
			if meta.Commit != "" {
				if authorMeta != "" {
					authorMeta += " · " + meta.Commit
				} else {
					authorMeta = meta.Commit
				}
			}
			return core.View{
				TestID: testID,
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("Pick %s (%s)", title, meta.Branch),
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					Padding:      core.All(12),
					Gap:          8,
					CornerRadius: 12,
					Background:   bg,
					BorderWidth:  bw,
					BorderColor:  border,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func(core.PressEvent) {
					onPick()
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
								Content: title,
								Style: core.Style{
									FontSize:      11,
									FontWeight:    600,
									LetterSpacing: 0.7,
									Color:         specTextStrong,
								},
							},
							core.Text{
								Content: meta.Branch,
								Style: core.Style{
									FontFamily: "monospace",
									FontSize:   11,
									Color:      specTextSecondary,
								},
							},
							core.View{Style: core.Style{FlexGrow: 1}},
							core.Text{
								Content: authorMeta,
								Style: core.Style{
									FontSize: 11.5,
									Color:    specTextMuted,
								},
							},
						},
					},
					core.View{
						Style: core.Style{
							Width:        core.Pct(100),
							Height:       core.Pt(180),
							CornerRadius: 6,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
							Background:   specBg,
							Overflow:     core.Hidden,
						},
						Children: []core.Node{
							core.Canvas{
								TestID: canvasID,
								Label:  title + " scene preview",
								Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
								Draw: func(d *core.Drawing, size core.Size) {
									drawMergeScenePreview(d, size, sess, doc, built, side)
								},
							},
						},
					},
				},
			}
		}

		unresolvedRemaining := sess.RemainingCount() > 0
		var resultLegend []core.Node
		if unresolvedRemaining {
			resultLegend = []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        6,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pt(10),
								Height:       core.Pt(10),
								CornerRadius: 2,
								BorderWidth:  1.5,
								BorderColor:  specChanged,
							},
						},
						core.Text{
							Content: "unresolved, both positions shown",
							Style: core.Style{
								FontSize: 11.5,
								Color:    specTextMuted,
							},
						},
					},
				},
			}
		}

		return core.View{
			TestID: "merge-center",
			Style: core.Style{
				FlexGrow:   1,
				FlexBasis:  core.Pt(0),
				Padding:    core.All(16),
				Gap:        12,
				Background: specBg,
				Overflow:   core.Hidden,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						Gap:        12,
						FlexShrink: 0,
					},
					Children: []core.Node{
						sideCard("merge-card-mine", "merge-canvas-mine", "MINE", sess.Mine, focusedChoice == MergeChoiceMine, sess.MineDoc, sess.mineBuilt, "mine", func() {
							if focusedID != "" {
								ed.report(ed.ResolveMergeConflict(focusedID, MergeChoiceMine))
							}
						}),
						sideCard("merge-card-theirs", "merge-canvas-theirs", "THEIRS", sess.Theirs, focusedChoice == MergeChoiceTheirs, sess.TheirsDoc, sess.theirsBuilt, "theirs", func() {
							if focusedID != "" {
								ed.report(ed.ResolveMergeConflict(focusedID, MergeChoiceTheirs))
							}
						}),
					},
				},
				core.View{
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Gap:       8,
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        8,
								FlexShrink: 0,
							},
							Children: append([]core.Node{
								core.Text{
									Content: "RESULT",
									Style: core.Style{
										FontSize:      11,
										FontWeight:    600,
										LetterSpacing: 0.7,
										Color:         specTextStrong,
									},
								},
								core.Text{
									Content: fmt.Sprintf("what %s will be after the merge", sess.SceneName),
									Style: core.Style{
										FontSize: 12,
										Color:    specTextMuted,
									},
								},
								core.View{Style: core.Style{FlexGrow: 1}},
							}, resultLegend...),
						},
						core.View{
							TestID: "merge-result-frame",
							Style: core.Style{
								FlexGrow:     1,
								FlexBasis:    core.Pt(0),
								CornerRadius: 12,
								BorderWidth:  1,
								BorderColor:  specLine,
								Background:   specBg,
								Overflow:     core.Hidden,
							},
							Children: []core.Node{
								core.Canvas{
									TestID: "merge-canvas-result",
									Label:  "Merged result scene preview",
									Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
									Draw: func(d *core.Drawing, size core.Size) {
										resDoc, _ := sess.ResultDoc()
										drawMergeScenePreview(d, size, sess, resDoc, sess.resultBuilt, "result")
									},
								},
							},
						},
					},
				},
			},
		}
	})
}

func mergeInspectorView(cx *core.Scope, ed *Editor, mergeRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = mergeRev.Get()
		sess := ed.MergeSession()
		if sess == nil {
			return core.View{}
		}
		item := sess.FocusedItem()
		if item == nil {
			return core.View{
				Style: core.Style{
					Width:       core.Pt(400),
					FlexShrink:  0,
					Background:  specPanel,
					Borders:     core.Edges{Left: 1},
					BorderColor: specLine,
				},
			}
		}

		optionCard := func(testID string, opt MergeOption, isAgent bool) core.Node {
			selected := item.Choice == opt.Choice
			bg := specRaised
			border := specLineRaised
			var bw core.Points = 1
			if selected {
				border = specAccent
				bw = 1.5
			} else if isAgent {
				bg = specRaised
				border = specAgentEdge
			}
			radioBorder := specTextMuted
			if selected {
				radioBorder = specAccent
			}
			var radioDot []core.Node
			if selected {
				radioDot = []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(8),
							Height:       core.Pt(8),
							CornerRadius: 4,
							Background:   specAccent,
						},
					},
				}
			}

			cardChildren := []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:          core.Pt(16),
								Height:         core.Pt(16),
								FlexShrink:     0,
								CornerRadius:   8,
								BorderWidth:    1.5,
								BorderColor:    radioBorder,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: radioDot,
						},
						core.Text{
							Content: opt.Title,
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: 500,
								Color:      specTextStrong,
							},
						},
						core.View{Style: core.Style{FlexGrow: 1}},
						core.Text{
							Content: opt.Meta,
							Style: core.Style{
								FontSize: 11.5,
								Color:    specTextMuted,
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Width:        core.Pct(100),
						Padding:      core.Symmetric(8, 10),
						CornerRadius: 6,
						Background:   specBg,
					},
					Children: []core.Node{
						core.Text{
							Content: opt.YAML,
							Style: core.Style{
								FontFamily: "monospace",
								FontSize:   11.5,
								Color:      specText,
							},
						},
					},
				},
			}
			if opt.Reason != "" {
				cardChildren = append(cardChildren, core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignStart,
						Gap:        8,
					},
					Children: []core.Node{
						core.Icon{
							SVG: lucide.Sparkles(),
							Style: core.Style{
								Width:      core.Pt(13),
								Height:     core.Pt(13),
								FlexShrink: 0,
								Margin:     core.Edges{Top: 2},
								Color:      specAgent,
							},
						},
						core.Text{
							Content: opt.Reason,
							Style: core.Style{
								FlexGrow:  1,
								FlexBasis: core.Pt(0),
								FontSize:  12,
								Color:     specText,
							},
						},
					},
				})
			}

			choice := opt.Choice
			itemID := item.ID
			return core.View{
				TestID: testID,
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("%s (%s) for %s", opt.Title, opt.Meta, item.Name),
				Style: core.Style{
					Width:        core.Pct(100),
					Padding:      core.All(12),
					Gap:          8,
					CornerRadius: 12,
					Background:   bg,
					BorderWidth:  bw,
					BorderColor:  border,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.ResolveMergeConflict(itemID, choice))
				},
				Children: cardChildren,
			}
		}

		itemID := item.ID
		bodyChildren := []core.Node{
			optionCard("merge-option-agent", item.Agent, true),
			optionCard("merge-option-mine", item.Mine, false),
			optionCard("merge-option-theirs", item.Theirs, false),
		}

		if item.Choice == MergeChoiceCustom && !item.EditingCustom {
			bodyChildren = append(bodyChildren, core.View{
				TestID: "merge-custom-active",
				Style: core.Style{
					Width:        core.Pct(100),
					Padding:      core.All(12),
					Gap:          8,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1.5,
					BorderColor:  specAccent,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
						},
						Children: []core.Node{
							core.Text{
								Content: "Hand-edited YAML",
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: 500,
									Color:      specTextStrong,
								},
							},
							core.Text{
								Content: "custom",
								Style: core.Style{
									FontSize: 11.5,
									Color:    specTextMuted,
								},
							},
						},
					},
					core.View{
						Style: core.Style{
							Width:        core.Pct(100),
							Padding:      core.Symmetric(8, 10),
							CornerRadius: 6,
							Background:   specBg,
						},
						Children: []core.Node{
							core.Text{
								Content: item.CustomYAML,
								Style: core.Style{
									FontFamily: "monospace",
									FontSize:   11.5,
									Color:      specText,
								},
							},
						},
					},
				},
			})
		}

		editLabel := "Edit the result YAML by hand"
		if item.EditingCustom {
			editLabel = "Hide YAML editor"
		}
		bodyChildren = append(bodyChildren, ui.Button{
			TestID:  "merge-edit-yaml",
			Text:    editLabel,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			TextStyle: core.Style{
				FontSize: 12,
				Color:    specTextSecondary,
			},
			Style: core.Style{
				Height:       core.Pt(28),
				Padding:      core.Symmetric(0, 10),
				CornerRadius: 8,
				AlignSelf:    core.AlignStart,
			},
			OnPress: func() {
				ed.report(ed.SetMergeCustomEditing(itemID, !item.EditingCustom))
			},
		})

		if item.EditingCustom {
			var errNodes []core.Node
			if item.CustomErr != "" {
				errNodes = []core.Node{
					core.Text{
						TestID:  "merge-custom-yaml-error",
						Content: item.CustomErr,
						Style: core.Style{
							FontSize: 11.5,
							Color:    specProblem,
						},
					},
				}
			}
			editorChildren := append([]core.Node{
				ui.Input{
					TestID:      "merge-custom-yaml-input",
					Label:       "Result YAML for " + item.Name,
					Multiline:   true,
					Placeholder: "id: " + item.ObjectID + "\nx: 600",
					Value: func() string {
						_ = mergeRev.Get()
						if s := ed.MergeSession(); s != nil {
							if cur := s.FocusedItem(); cur != nil && cur.ID == itemID {
								return cur.CustomYAML
							}
						}
						return ""
					},
					OnChange: func(v string) {
						ed.SetMergeCustomYAML(itemID, v)
					},
					Style: core.Style{
						Width:        core.Pct(100),
						Height:       core.Pt(120),
						Background:   specBg,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
						CornerRadius: 6,
						Color:        specText,
						FontFamily:   "monospace",
						FontSize:     11.5,
					},
				},
			}, errNodes...)
			editorChildren = append(editorChildren, core.View{
				Style: core.Style{
					Direction: core.Row,
					Gap:       8,
				},
				Children: []core.Node{
					ui.Button{
						TestID:  "merge-custom-yaml-apply",
						Text:    "Apply YAML",
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
							CornerRadius: 8,
							Padding:      core.Symmetric(0, 12),
						},
						OnPress: func() {
							if s := ed.MergeSession(); s != nil {
								if cur := s.FocusedItem(); cur != nil && cur.ID == itemID {
									ed.report(ed.ApplyMergeCustomYAML(itemID, cur.CustomYAML))
								}
							}
						},
					},
					ui.Button{
						TestID:  "merge-custom-yaml-cancel",
						Text:    "Cancel",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Color:   func() core.Color { return specLineRaised },
						TextStyle: core.Style{
							FontSize: 12,
							Color:    specText,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							CornerRadius: 8,
							Padding:      core.Symmetric(0, 10),
						},
						OnPress: func() {
							ed.report(ed.SetMergeCustomEditing(itemID, false))
						},
					},
				},
			})

			bodyChildren = append(bodyChildren, core.View{
				TestID: "merge-custom-yaml-card",
				Style: core.Style{
					Width:        core.Pct(100),
					Padding:      core.All(12),
					Gap:          8,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: editorChildren,
			})
		}

		if sess.VerifyErr != "" {
			bodyChildren = append(bodyChildren, core.View{
				TestID: "merge-verify-error",
				Style: core.Style{
					Width:        core.Pct(100),
					Padding:      core.All(12),
					Gap:          4,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specProblemEdge,
				},
				Children: []core.Node{
					core.Text{
						Content: "Merge verification failed",
						Style: core.Style{
							FontSize:   12,
							FontWeight: 600,
							Color:      specProblem,
						},
					},
					core.Text{
						Content: sess.VerifyErr,
						Style: core.Style{
							FontSize: 11.5,
							Color:    specText,
						},
					},
				},
			})
		}

		var footer []core.Node
		if sess.Finished {
			mergedTitle := "Scene resolved and saved"
			if sess.CommitSHA != "" {
				mergedTitle = "Merged as " + sess.CommitSHA
			}
			footer = []core.Node{
				core.View{
					TestID: "merge-result-card",
					Style: core.Style{
						Margin:       core.Edges{Left: 16, Right: 16, Bottom: 16},
						Padding:      core.Symmetric(12, 14),
						Gap:          4,
						CornerRadius: 12,
						Background:   specRaised,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					Children: []core.Node{
						core.Text{
							TestID:  "merge-result-title",
							Content: mergedTitle,
							Style: core.Style{
								FontSize:   12.5,
								FontWeight: 500,
								Color:      specTextStrong,
							},
						},
						core.Text{
							TestID:  "merge-result-summary",
							Content: sess.VerifySummary,
							Style: core.Style{
								FontSize: 12,
								Color:    specTextMuted,
							},
						},
					},
				},
			}
		}

		return core.View{
			TestID: "merge-inspector",
			Style: core.Style{
				Width:       core.Pt(400),
				FlexShrink:  0,
				Height:      core.Pct(100),
				Background:  specPanel,
				Borders:     core.Edges{Left: 1},
				BorderColor: specLine,
				Overflow:    core.Hidden,
			},
			Children: append([]core.Node{
				core.View{
					Style: core.Style{
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Height:      core.Pt(44),
						FlexShrink:  0,
						Padding:     core.Symmetric(0, 16),
						Gap:         8,
						Borders:     core.Edges{Bottom: 1},
						BorderColor: specLine,
					},
					Children: []core.Node{
						core.Text{
							TestID:  "merge-focus-name",
							Content: item.Name,
							Style: core.Style{
								FontSize:   14,
								FontWeight: 600,
								Color:      specTextStrong,
							},
						},
						core.Text{
							TestID:  "merge-focus-kind",
							Content: item.Kind,
							Style: core.Style{
								FontSize: 12.5,
								Color:    specTextMuted,
							},
						},
					},
				},
				ui.ScrollView{
					TestID:       "merge-inspector-scroll",
					Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
					ContentStyle: core.Style{Padding: core.All(16), Gap: 12},
					EdgeColor:    func() core.Color { return specPanel },
					Children:     bodyChildren,
				},
			}, footer...),
		}
	})
}

// --- 2D Scene Preview Canvas Rendering for MINE, THEIRS, and RESULT ---

func drawMergeScenePreview(
	d *core.Drawing,
	size core.Size,
	sess *MergeConflictSession,
	doc *sceneformat.SceneDoc,
	built *sceneformat.BuiltScene,
	mode string,
) {
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 || sess == nil {
		return
	}

	// Dot-grid viewport surround using only Spec tokens.
	d.SetFillColor(specBg)
	d.BeginPath().Rect(0, 0, w, h).Fill()
	d.SetFillColor(specLineRaised)
	const gridStep float32 = 16
	for gx := gridStep; gx < w; gx += gridStep {
		for gy := gridStep; gy < h; gy += gridStep {
			d.BeginPath().Rect(gx, gy, 1.2, 1.2).Fill()
		}
	}

	vw, vh := float32(640), float32(360)
	if doc != nil && doc.VirtualWidth > 0 && doc.VirtualHeight > 0 {
		vw, vh = doc.VirtualWidth, doc.VirtualHeight
	} else if sess.MineDoc != nil && sess.MineDoc.VirtualWidth > 0 && sess.MineDoc.VirtualHeight > 0 {
		vw, vh = sess.MineDoc.VirtualWidth, sess.MineDoc.VirtualHeight
	}

	pad := float32(8)
	if mode == "result" {
		pad = 20
	}
	availW := max(w-pad*2, 1)
	availH := max(h-pad*2, 1)
	scale := min(availW/vw, availH/vh)
	if mode == "result" && scale > 1 {
		scale = 1
	}
	frameW := vw * scale
	frameH := vh * scale
	ox := (w - frameW) / 2
	oy := (h - frameH) / 2

	// Scene frame backdrop.
	d.Save()
	d.SetFillColor(specPanel)
	d.BeginPath().RoundRect(ox, oy, frameW, frameH, 4).Fill()

	// Clip to scene frame and transform into virtual scene coordinates.
	d.Save()
	d.BeginPath().Rect(ox, oy, frameW, frameH).Clip()
	d.Translate(ox, oy)
	d.Scale(scale, scale)

	// Draw built sprite/tilemap layers when present.
	if built != nil {
		built.SyncMaterials()
		for _, layer := range built.Layers {
			if !layer.Visible {
				continue
			}
			drawBuiltLayer(d, layer)
		}
	}

	// Build conflict lookup by object ID.
	conflictByObj := make(map[string]MergeConflictItem, len(sess.Conflicts))
	for _, c := range sess.Conflicts {
		if c.Scope == "object" && c.ObjectID != "" {
			conflictByObj[c.ObjectID] = c
		}
	}

	// Draw schematic representations for objects without resolved sprites (or all scene objects as crisp Spec blocks).
	if doc != nil {
		for _, layer := range doc.Layers {
			if !layer.IsVisible() {
				continue
			}
			var walk func(objs []sceneformat.ObjectDoc, px, py float32)
			walk = func(objs []sceneformat.ObjectDoc, px, py float32) {
				for i := range objs {
					o := &objs[i]
					wx, wy := px+o.X, py+o.Y
					item, isConflict := conflictByObj[o.ID]

					// In RESULT mode, unresolved conflicted objects draw as dual ghosts below instead of a solid single choice.
					if mode == "result" && isConflict && item.Choice == MergeChoiceUnresolved {
						walk(o.Children, wx, wy)
						continue
					}

					// Only draw placeholder block when the built object has no loaded sprite image.
					hasSprite := built != nil && built.Objects[o.ID] != nil && built.Objects[o.ID].Sprite != nil
					if !hasSprite {
						drawMergeObjectBlock(d, o, wx, wy, false)
					}
					if isConflict {
						rx, ry, rw, rh := mergeObjectRect(o, wx, wy)
						if mode != "result" {
							drawDashedRect(d, rx-2, ry-2, rw+4, rh+4, specChanged, 1.5/scale)
						}
						if sess.FocusedID == item.ID {
							drawFocusRing(d, rx, ry, rw, rh, scale)
						}
					}
					walk(o.Children, wx, wy)
				}
			}
			walk(layer.Objects, 0, 0)
		}
	}

	// Draw deleted-object markers in MINE/THEIRS and dual-position ghosts for unresolved conflicts in RESULT.
	for _, item := range sess.Conflicts {
		if item.Scope != "object" {
			continue
		}
		isFocused := sess.FocusedID == item.ID
		switch mode {
		case "mine":
			if item.MineObj == nil && item.TheirsObj != nil {
				rx, ry, rw, rh := mergeObjectRect(item.TheirsObj, item.TheirsObj.X, item.TheirsObj.Y)
				drawMergeGhost(d, rx, ry, rw, rh, specProblemTint, specProblem, false, scale)
				if isFocused {
					drawFocusRing(d, rx, ry, rw, rh, scale)
				}
			}
		case "theirs":
			if item.TheirsObj == nil && item.MineObj != nil {
				rx, ry, rw, rh := mergeObjectRect(item.MineObj, item.MineObj.X, item.MineObj.Y)
				drawMergeGhost(d, rx, ry, rw, rh, specProblemTint, specProblem, false, scale)
				if isFocused {
					drawFocusRing(d, rx, ry, rw, rh, scale)
				}
			}
		case "result":
			if item.Choice == MergeChoiceUnresolved {
				if item.MineObj != nil {
					rx, ry, rw, rh := mergeObjectRect(item.MineObj, item.MineObj.X, item.MineObj.Y)
					drawMergeGhost(d, rx, ry, rw, rh, specChangedTint, specChanged, true, scale)
					if isFocused {
						drawFocusRing(d, rx, ry, rw, rh, scale)
					}
				}
				if item.TheirsObj != nil {
					samePos := item.MineObj != nil &&
						math.Abs(float64(item.MineObj.X-item.TheirsObj.X)) < 1 &&
						math.Abs(float64(item.MineObj.Y-item.TheirsObj.Y)) < 1
					if !samePos {
						rx, ry, rw, rh := mergeObjectRect(item.TheirsObj, item.TheirsObj.X, item.TheirsObj.Y)
						drawMergeGhost(d, rx, ry, rw, rh, specChangedTint, specChanged, true, scale)
					}
				}
			} else if isFocused {
				// If focused object was deleted in the chosen resolution, show a subtle focus indicator at its prior spot.
				var ref *sceneformat.ObjectDoc
				if item.Choice == MergeChoiceTheirs && item.TheirsObj == nil {
					ref = item.MineObj
				} else if item.Choice == MergeChoiceMine && item.MineObj == nil {
					ref = item.TheirsObj
				} else if item.Choice == MergeChoiceAgent && item.Agent.Deleted {
					ref = firstNonNilObj(item.MineObj, item.TheirsObj)
				}
				if ref != nil {
					rx, ry, rw, rh := mergeObjectRect(ref, ref.X, ref.Y)
					drawMergeGhost(d, rx, ry, rw, rh, specProblemTint, specProblem, false, scale)
					drawFocusRing(d, rx, ry, rw, rh, scale)
				}
			}
		}
	}

	d.Restore()

	// Frame border.
	d.SetStrokeColor(specLineRaised).SetLineWidth(1)
	d.BeginPath().RoundRect(ox, oy, frameW, frameH, 4).Stroke()
	d.Restore()
}

func mergeObjectRect(o *sceneformat.ObjectDoc, wx, wy float32) (x, y, w, h float32) {
	w, h = 24, 24
	if o != nil {
		if pw, ok := propFloat(o.Properties, "width"); ok && pw > 0 {
			w = pw
		} else if pw, ok := propFloat(o.Properties, "w"); ok && pw > 0 {
			w = pw
		}
		if ph, ok := propFloat(o.Properties, "height"); ok && ph > 0 {
			h = ph
		} else if ph, ok := propFloat(o.Properties, "h"); ok && ph > 0 {
			h = ph
		}
		// Sensible defaults when width/height aren't explicitly stored on fixture objects.
		if w == 24 && h == 24 {
			lower := strings.ToLower(o.ID + " " + o.Name + " " + o.Prefab)
			switch {
			case strings.Contains(lower, "floor") || strings.Contains(lower, "ground"):
				w, h = 640, 60
			case strings.Contains(lower, "plank") || strings.Contains(lower, "platform"):
				w, h = 160, 12
			case strings.Contains(lower, "door"):
				w, h = 30, 40
			case strings.Contains(lower, "knight") || strings.Contains(lower, "hero") || strings.Contains(lower, "player"):
				w, h = 24, 34
			case strings.Contains(lower, "slime") || strings.Contains(lower, "enemy"):
				w, h = 30, 20
			case strings.Contains(lower, "coin") || strings.Contains(lower, "pickup"):
				w, h = 10, 10
			case strings.Contains(lower, "lever") || strings.Contains(lower, "switch"):
				w, h = 8, 18
			case strings.Contains(lower, "torch") || strings.Contains(lower, "light"):
				w, h = 10, 16
			}
		}
		return wx - o.OriginX, wy - o.OriginY, w, h
	}
	return wx, wy, w, h
}

func drawMergeObjectBlock(d *core.Drawing, o *sceneformat.ObjectDoc, wx, wy float32, ghost bool) {
	rx, ry, rw, rh := mergeObjectRect(o, wx, wy)
	lower := strings.ToLower(o.ID + " " + o.Name + " " + o.Prefab)

	fill := specLineRaised
	radius := float32(3)
	switch {
	case strings.Contains(lower, "floor") || strings.Contains(lower, "ground") || strings.Contains(lower, "plank") || strings.Contains(lower, "platform"):
		fill = specLineRaised
		radius = 2
	case strings.Contains(lower, "knight") || strings.Contains(lower, "hero") || strings.Contains(lower, "player"):
		fill = specChanged
		radius = 5
	case strings.Contains(lower, "slime") || strings.Contains(lower, "enemy"):
		fill = core.RGBA(0x4FD38A, 0.55)
		radius = 6
	case strings.Contains(lower, "coin") || strings.Contains(lower, "pickup"):
		fill = core.RGBA(0xF2C14E, 0.80)
		radius = 5
	case strings.Contains(lower, "door"):
		fill = specTextMuted
		radius = 4
	case strings.Contains(lower, "lever") || strings.Contains(lower, "torch"):
		fill = specTextSecondary
		radius = 2
	}
	if ghost {
		fill = core.RGBA(0x8B92A0, 0.18)
	}
	d.Save()
	d.SetFillColor(fill)
	d.BeginPath().RoundRect(rx, ry, rw, rh, radius).Fill()
	if strings.Contains(lower, "floor") || strings.Contains(lower, "ground") {
		d.SetFillColor(specTextMuted)
		d.BeginPath().Rect(rx, ry, rw, 2).Fill()
	}
	d.Restore()
}

func drawMergeGhost(d *core.Drawing, x, y, w, h float32, fill, stroke core.Color, dashed bool, scale float32) {
	d.Save()
	d.SetFillColor(fill)
	d.BeginPath().RoundRect(x, y, w, h, 4).Fill()
	if dashed {
		drawDashedRect(d, x-2, y-2, w+4, h+4, stroke, 1.5/scale)
	} else {
		d.SetStrokeColor(stroke).SetLineWidth(1.5 / scale)
		d.BeginPath().RoundRect(x-2, y-2, w+4, h+4, 4).Stroke()
	}
	d.Restore()
}

func drawDashedRect(d *core.Drawing, x, y, w, h float32, color core.Color, lineW float32) {
	d.Save()
	d.SetStrokeColor(color).SetLineWidth(lineW).SetLineDash(5, 3)
	d.BeginPath().Rect(x, y, w, h).Stroke()
	d.SetLineDash()
	d.Restore()
}

func drawFocusRing(d *core.Drawing, x, y, w, h, scale float32) {
	pad := 5 / scale
	d.Save()
	d.SetStrokeColor(specAccent).SetLineWidth(2 / scale)
	d.BeginPath().RoundRect(x-pad, y-pad, w+pad*2, h+pad*2, 5/scale).Stroke()
	d.Restore()
}

func propFloat(props map[string]any, key string) (float32, bool) {
	if props == nil {
		return 0, false
	}
	v, ok := props[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return float32(n), true
	case float32:
		return n, true
	case int:
		return float32(n), true
	case int64:
		return float32(n), true
	default:
		return 0, false
	}
}
