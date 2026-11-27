package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// AgentReviewPanelView renders the Review changes panel including proposed file deletions
// that Keep all never applies (matching design/editor/Permissions.dc.html "Proposed deletion")
// using strictly SpecPalette tokens in both Dark and Light appearances.
func AgentReviewPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type reviewKey struct {
		open   bool
		revNum uint64
		dark   bool
		lang   string
	}
	key := core.NewMemo(cx, func() reviewKey {
		_ = rev.Get()
		return reviewKey{
			open:   ed.AgentReviewOpen() && !ed.Playing() && !ed.StartScreenOpen() && !ed.MergeConflictOpen(),
			revNum: ed.agentReviewRev(),
			dark:   ed.SpecPalette(cx).Dark,
			lang:   ed.ResolvedLanguage(),
		}
	})

	return core.Show{
		When: func() bool { return key.Get().open },
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := key.Get()
				snap := ed.AgentReview()
				if snap == nil {
					return nil
				}
				p := SpecPaletteFor(k.dark)
				focusRing := p.FocusRing(2)

				unkeptRegular := 0
				activeRegular := 0
				for _, c := range snap.Changes {
					if !c.Reverted {
						activeRegular++
						if !c.Kept {
							unkeptRegular++
						}
					}
				}
				confirmedDels := 0
				for _, d := range snap.Deletions {
					if d.Status == DeletionConfirmed {
						confirmedDels++
					}
				}

				totalItems := len(snap.Changes) + len(snap.Deletions)
				keepLabel := fmt.Sprintf("Keep %d of %d", unkeptRegular, activeRegular)
				if unkeptRegular == 0 && confirmedDels > 0 {
					keepLabel = fmt.Sprintf("Delete %d", confirmedDels)
				} else if confirmedDels > 0 {
					keepLabel = fmt.Sprintf("Keep %d and delete %d", unkeptRegular, confirmedDels)
				} else if unkeptRegular == 0 {
					keepLabel = "Keep all"
				}

				subHeader := fmt.Sprintf("%d CHANGES · %d FILES", totalItems, totalItems)
				if totalItems == 1 {
					subHeader = "1 CHANGE · 1 FILE"
				}
				if snap.Prompt != "" {
					subHeader += fmt.Sprintf(" · from “%s”", snap.Prompt)
				}

				bodyChildren := []core.Node{
					core.Text{
						TestID:  "agent-review-subheader",
						Content: subHeader,
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.66,
							Color:         p.TextFaint,
							Padding:       core.Symmetric(4, 4),
						},
					},
				}

				// Non-deletion change rows.
				for i, c := range snap.Changes {
					chg := c
					sign := "~"
					badgeFg := p.Changed.Fg
					badgeBg := p.Changed.Fill
					if chg.Kind == ReviewChangeAdded {
						sign = "+"
						badgeFg = p.Added.Fg
						badgeBg = p.Added.Fill
					}
					var rightCtrl core.Node
					switch {
					case chg.Kept:
						rightCtrl = core.Text{
							Content: "Kept",
							Style:   core.Style{FontSize: 11.5, FontWeight: 600, Color: p.Added.Fg, Padding: core.Symmetric(0, 8)},
						}
					case chg.Reverted:
						rightCtrl = core.Text{
							Content: "Reverted",
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint, Padding: core.Symmetric(0, 8)},
						}
					default:
						chgID := chg.ID
						rightCtrl = ui.Button{
							TestID:  fmt.Sprintf("agent-review-revert-%d", i),
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Text:    "Revert",
							Label:   "Revert " + chg.Title,
							Color:   func() core.Color { return p.LineRaised },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      p.TextPrimary,
							},
							Style: core.Style{
								Height:       core.Pt(28),
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 6,
								FocusVisible: focusRing,
							},
							OnPress: func() {
								ed.report(ed.RevertReviewChange(chgID))
							},
						}
					}
					bodyChildren = append(bodyChildren, core.View{
						TestID: fmt.Sprintf("agent-review-change-%d", i),
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          12,
							Padding:      core.Edges{Top: 8, Right: 8, Bottom: 8, Left: 12},
							CornerRadius: 12,
							Background:   p.Raised,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:          core.Pt(22),
									Height:         core.Pt(22),
									CornerRadius:   6,
									Background:     badgeBg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FlexShrink:     0,
								},
								Children: []core.Node{
									core.Text{Content: sign, Style: core.Style{FontSize: 12.5, FontWeight: 600, Color: badgeFg}},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
								Children: []core.Node{
									core.Text{Content: chg.Title, Style: core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary}},
									core.Text{Content: chg.Detail, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
								},
							},
							rightCtrl,
						},
					})
				}

				// Proposed deletion cards (never applied by Keep all!).
				var previewDel *ProposedDeletion
				for i, d := range snap.Deletions {
					del := d
					if previewDel == nil && del.Status != DeletionApplied {
						cp := del
						previewDel = &cp
					}
					delPath := del.Path
					cardBg := p.Removed.Fill
					cardBorder := p.Removed.Edge
					titleDec := core.TextDecorationLineThrough
					if del.Status == DeletionKept {
						cardBg = p.Raised
						cardBorder = p.LineRaised
						titleDec = core.TextDecorationNone
					}

					delKids := []core.Node{
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:          core.Pt(22),
										Height:         core.Pt(22),
										CornerRadius:   6,
										Background:     p.Removed.Fill,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FlexShrink:     0,
									},
									Children: []core.Node{
										core.Text{Content: "−", Style: core.Style{FontSize: 13, FontWeight: 600, Color: p.Removed.Fg}},
									},
								},
								core.View{
									Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
									Children: []core.Node{
										core.Text{
											TestID:  fmt.Sprintf("agent-review-del-title-%d", i),
											Content: del.Path,
											Style: core.Style{
												FontSize:       12.5,
												FontWeight:     500,
												Color:          p.TextPrimary,
												TextDecoration: titleDec,
											},
										},
										core.Text{
											TestID:  fmt.Sprintf("agent-review-del-detail-%d", i),
											Content: del.Detail,
											Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
										},
									},
								},
							},
						},
					}

					switch del.Status {
					case DeletionPending:
						delKids = append(delKids,
							core.Text{
								Content: "Keep leaves this file alone until you confirm the deletion here. ⌘Z restores it from the checkpoint.",
								Style:   core.Style{FontSize: 12, Color: p.TextSecondary, Padding: core.Edges{Left: 34}},
							},
							core.View{
								Style: core.Style{Direction: core.Row, Gap: 8, Padding: core.Edges{Left: 34}},
								Children: []core.Node{
									ui.Button{
										TestID:  fmt.Sprintf("agent-review-del-confirm-%d", i),
										Variant: ui.ButtonGhost,
										Size:    ui.SizeSmall,
										Text:    ed.T("review.deleteThisFile"),
										Label:   ed.T("review.deleteThisFile") + " " + del.Path,
										Color:   func() core.Color { return p.Removed.Fill },
										TextStyle: core.Style{
											FontSize:   12,
											FontWeight: 600,
											Color:      p.TextPrimary,
										},
										Style: core.Style{
											Height:       core.Pt(30),
											Padding:      core.Symmetric(0, 12),
											CornerRadius: 8,
											BorderWidth:  1,
											BorderColor:  p.Removed.Edge,
											FocusVisible: focusRing,
										},
										OnPress: func() {
											ed.report(ed.ConfirmProposedDeletion(delPath))
										},
									},
									ui.Button{
										TestID:  fmt.Sprintf("agent-review-del-keep-%d", i),
										Variant: ui.ButtonGhost,
										Size:    ui.SizeSmall,
										Text:    ed.T("review.keepTheFile"),
										Label:   ed.T("review.keepTheFile") + " " + del.Path,
										Color:   func() core.Color { return p.LineRaised },
										TextStyle: core.Style{
											FontSize:   12,
											FontWeight: 500,
											Color:      p.TextPrimary,
										},
										Style: core.Style{
											Height:       core.Pt(30),
											Padding:      core.Symmetric(0, 12),
											CornerRadius: 8,
											FocusVisible: focusRing,
										},
										OnPress: func() {
											ed.report(ed.KeepProposedFile(delPath))
										},
									},
								},
							},
						)
					case DeletionConfirmed:
						delKids = append(delKids, core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Edges{Left: 34}},
							Children: []core.Node{
								core.Text{
									TestID:  fmt.Sprintf("agent-review-del-state-%d", i),
									Content: "Deletion confirmed · happens with Keep",
									Style:   core.Style{FlexGrow: 1, FontSize: 12, Color: p.TextSecondary},
								},
								ui.Button{
									TestID:    fmt.Sprintf("agent-review-del-reset-%d", i),
									Variant:   ui.ButtonGhost,
									Size:      ui.SizeSmall,
									Text:      "Undo",
									Label:     "Undo confirm deletion",
									Color:     func() core.Color { return core.Transparent },
									TextStyle: core.Style{FontSize: 12, FontWeight: 500, Color: p.Accent.Fg},
									Style:     core.Style{Height: core.Pt(28), Padding: core.Symmetric(0, 8), CornerRadius: 6, FocusVisible: focusRing},
									OnPress:   func() { ed.ResetProposedDeletion(delPath) },
								},
							},
						})
					case DeletionKept:
						delKids = append(delKids, core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Edges{Left: 34}},
							Children: []core.Node{
								core.Text{
									TestID:  fmt.Sprintf("agent-review-del-state-%d", i),
									Content: "The file stays · the rest of the review is unchanged",
									Style:   core.Style{FlexGrow: 1, FontSize: 12, Color: p.TextSecondary},
								},
								ui.Button{
									TestID:    fmt.Sprintf("agent-review-del-reset-%d", i),
									Variant:   ui.ButtonGhost,
									Size:      ui.SizeSmall,
									Text:      "Undo",
									Label:     "Undo keep file",
									Color:     func() core.Color { return core.Transparent },
									TextStyle: core.Style{FontSize: 12, FontWeight: 500, Color: p.Accent.Fg},
									Style:     core.Style{Height: core.Pt(28), Padding: core.Symmetric(0, 8), CornerRadius: 6, FocusVisible: focusRing},
									OnPress:   func() { ed.ResetProposedDeletion(delPath) },
								},
							},
						})
					case DeletionApplied:
						delKids = append(delKids, core.Text{
							TestID:  fmt.Sprintf("agent-review-del-state-%d", i),
							Content: "Deleted · ⌘Z restores from checkpoint",
							Style:   core.Style{FontSize: 12, Color: p.TextFaint, Padding: core.Edges{Left: 34}},
						})
					}

					bodyChildren = append(bodyChildren, core.View{
						TestID: fmt.Sprintf("agent-review-deletion-%d", i),
						Role:   core.RoleGroup,
						Label:  del.Path,
						Style: core.Style{
							Gap:          10,
							Padding:      core.All(12),
							CornerRadius: 12,
							Background:   cardBg,
							BorderWidth:  1,
							BorderColor:  cardBorder,
						},
						Children: delKids,
					})
				}

				// Bottom diff preview of the first proposed deletion (matching Permissions.dc.html).
				if previewDel != nil {
					previewNodes := []core.Node{
						core.Text{
							Content: previewDel.Path,
							Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: p.TextFaint},
						},
					}
					for _, ln := range previewDel.PreviewLines {
						previewNodes = append(previewNodes, core.View{
							Style: core.Style{
								Padding:    core.Symmetric(1, 8),
								Background: p.Removed.Fill,
							},
							Children: []core.Node{
								core.Text{
									Content: "− " + strings.TrimRight(ln, "\r\n"),
									Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: p.Removed.Fg},
								},
							},
						})
					}
					if previewDel.MoreLines > 0 {
						previewNodes = append(previewNodes, core.Text{
							Content: fmt.Sprintf("  %d more lines", previewDel.MoreLines),
							Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: p.TextFaint, Padding: core.Symmetric(0, 8)},
						})
					}
					bodyChildren = append(bodyChildren,
						core.View{Style: core.Style{FlexGrow: 1}},
						core.View{
							TestID: "agent-review-del-preview",
							Style: core.Style{
								Gap:          3,
								Padding:      core.All(12),
								CornerRadius: 12,
								Background:   p.Inset,
								BorderWidth:  1,
								BorderColor:  p.Line,
							},
							Children: previewNodes,
						},
					)
				}

				shadowAlpha := float32(0.45)
				if !p.Dark {
					shadowAlpha = 0.16
				}

				return core.View{
					TestID: "agent-review-panel",
					Role:   core.RoleRegion,
					Label:  ed.T("review.title"),
					Style: core.Style{
						Position:     core.Absolute,
						Top:          core.Pt(12),
						Right:        core.Pt(12),
						Bottom:       core.Pt(12),
						Width:        core.Pt(440),
						MaxWidth:     core.Pct(92),
						CornerRadius: 12,
						Background:   p.Panel,
						BorderWidth:  1,
						BorderColor:  p.Line,
						Overflow:     core.Hidden,
						Shadows: []core.BoxShadow{
							ui.Shadow(0, 16, 40, 0, ui.Black(shadowAlpha)),
						},
					},
					Children: []core.Node{
						// Header bar.
						core.View{
							Style: core.Style{
								Height:      core.Pt(44),
								FlexShrink:  0,
								Direction:   core.Row,
								AlignItems:  core.AlignCenter,
								Gap:         8,
								Padding:     core.Edges{Left: 16, Right: 12},
								Borders:     core.Edges{Bottom: 1},
								BorderColor: p.Line,
							},
							Children: []core.Node{
								core.Text{
									TestID:  "agent-review-title",
									Content: ed.T("review.title"),
									Style:   core.Style{FlexGrow: 1, FontSize: 14, FontWeight: 600, Color: p.TextPrimary},
								},
								ui.Button{
									TestID:  "agent-review-revert-all",
									Variant: ui.ButtonGhost,
									Size:    ui.SizeSmall,
									Text:    ed.T("review.revertAll"),
									Label:   ed.T("review.revertAll"),
									Color:   func() core.Color { return p.LineRaised },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextPrimary,
									},
									Style: core.Style{
										Height:       core.Pt(30),
										Padding:      core.Symmetric(0, 12),
										CornerRadius: 8,
										FocusVisible: focusRing,
									},
									OnPress: func() {
										ed.report(ed.RevertAllReviewChanges())
									},
								},
								ui.Button{
									TestID:  "agent-review-keep-all",
									Variant: ui.ButtonGhost,
									Size:    ui.SizeSmall,
									Text:    keepLabel,
									Label:   keepLabel,
									Color:   func() core.Color { return p.Accent.Fg },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 600,
										Color:      p.Accent.OnFill,
									},
									Style: core.Style{
										Height:       core.Pt(30),
										Padding:      core.Symmetric(0, 12),
										CornerRadius: 8,
										FocusVisible: focusRing,
									},
									OnPress: func() {
										_, err := ed.KeepAllReviewChanges()
										ed.report(err)
									},
								},
								ui.Button{
									TestID:  "agent-review-close",
									Variant: ui.ButtonGhost,
									Size:    ui.SizeSmall,
									Text:    "×",
									Label:   "Close review changes panel",
									Color:   func() core.Color { return core.Transparent },
									TextStyle: core.Style{
										FontSize:   14,
										FontWeight: 600,
										Color:      p.TextMuted,
									},
									Style: core.Style{
										Width:        core.Pt(28),
										Height:       core.Pt(28),
										CornerRadius: 6,
										FocusVisible: focusRing,
									},
									OnPress: func() {
										ed.CloseAgentReview()
									},
								},
							},
						},
						// Body.
						ui.ScrollView{
							Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
							ContentStyle: core.Style{Padding: core.All(12), Gap: 8},
							Children:     bodyChildren,
						},
					},
				}
			})
		},
	}
}
