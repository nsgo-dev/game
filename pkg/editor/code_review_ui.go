package editor

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// CodeReviewView renders the full-window Go code review screen matching docs/design/editor/ReviewCode.dc.html
// using exclusively SpecPalette tokens (Dark & Light).
func CodeReviewView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	keyOpt := core.KeyOptions{When: core.KeyNotEditing}
	keysActive := func() bool {
		return ed.CodeReviewOpen() && !ed.CommandPaletteOpen() && !ed.SettingsOpen() && !ed.PermissionsDraftOpen()
	}
	cx.OnKeyWith("escape", keyOpt, func() {
		if keysActive() {
			ed.CloseCodeReview()
		}
	})
	cx.OnKeyWith("mod+enter", keyOpt, func() {
		if keysActive() {
			ed.report(ed.KeepCodeReview())
		}
	})
	cx.OnKeyWith("mod+backspace", keyOpt, func() {
		if keysActive() {
			ed.report(ed.RevertCodeReview())
		}
	})
	cx.OnKeyWith("j", keyOpt, func() {
		if keysActive() {
			ed.StepCodeReviewFile(+1)
		}
	})
	cx.OnKeyWith("k", keyOpt, func() {
		if keysActive() {
			ed.StepCodeReviewFile(-1)
		}
	})
	cx.OnKeyWith("u", keyOpt, func() {
		if keysActive() {
			ed.SetCodeReviewLayout(CodeReviewDiffUnified)
		}
	})
	cx.OnKeyWith("s", keyOpt, func() {
		if keysActive() {
			ed.SetCodeReviewLayout(CodeReviewDiffSplit)
		}
	})
	cx.OnKeyWith("r", keyOpt, func() {
		if keysActive() {
			ed.report(ed.RunCodeReviewChecksAsync())
		}
	})

	type reviewKey struct {
		rev  uint64
		dark bool
		lang string
	}
	stateMemo := core.NewMemo(cx, func() reviewKey {
		_ = rev.Get()
		return reviewKey{
			rev:  ed.codeReviewStateRev(),
			dark: ed.SpecPalette(cx).Dark,
			lang: ed.ResolvedLanguage(),
		}
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		k := stateMemo.Get()
		p := SpecPaletteFor(k.dark)
		sess := ed.CodeReviewSession()
		if sess == nil || !sess.Open {
			return core.View{}
		}
		return core.View{
			TestID: "code-review-screen",
			Role:   core.RoleRegion,
			Label:  ed.T("codeReview.title"),
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
				Overflow:   core.Hidden,
			},
			Children: []core.Node{
				codeReviewHeaderView(ed, sess, p),
				core.View{
					Style: core.Style{
						Direction: core.Row,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						MinHeight: core.Pt(0),
						Overflow:  core.Hidden,
					},
					Children: []core.Node{
						codeReviewSidebarView(ed, sess, p),
						codeReviewMainPaneView(ed, sess, p),
					},
				},
			},
		}
	})
}

func codeReviewHeaderView(ed *Editor, sess *CodeReviewSession, p SpecPalette) core.Node {
	yamlFiles := sess.YAMLFiles()
	goFiles := sess.GoFiles()
	confirmedGo := sess.ConfirmedGoFiles()
	waitingGo := sess.WaitingGoFiles()

	var goNote string
	switch {
	case len(goFiles) == 0:
		goNote = ""
	case len(waitingGo) == 0 && len(goFiles) == 2:
		goNote = ed.T("codeReview.bothGoConfirmed")
	case len(waitingGo) == 0 && len(goFiles) == 1:
		goNote = ed.T("codeReview.oneGoConfirmed")
	case len(waitingGo) == 0:
		goNote = ed.T("codeReview.allGoConfirmed", l10n.Param("count", len(goFiles)))
	case len(waitingGo) == 1:
		goNote = ed.T("codeReview.oneGoNeedsOK")
	default:
		goNote = ed.T("codeReview.manyGoNeedOK", l10n.Param("count", len(waitingGo)))
	}

	canKeep := len(yamlFiles) > 0 || len(confirmedGo) > 0
	keepLabel := fmt.Sprintf("Keep %d of %d", len(yamlFiles), len(yamlFiles))
	if len(yamlFiles) == 0 {
		if len(confirmedGo) == 1 {
			keepLabel = "Apply 1 confirmed"
		} else {
			keepLabel = fmt.Sprintf("Apply %d confirmed", len(confirmedGo))
		}
	}

	leftChildren := []core.Node{
		core.View{
			TestID:    "code-review-back",
			Role:      core.RoleButton,
			Label:     ed.T("codeReview.backLabel"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(28),
				Padding:      core.Symmetric(0, 8),
				Gap:          8,
				CornerRadius: 6,
				FocusVisible: p.FocusRing(2),
				FlexShrink:   0,
			},
			OnPress: func(core.PressEvent) {
				ed.CloseCodeReview()
			},
			Children: []core.Node{
				specIcon(lucide.ArrowLeft, 15, p.TextMuted),
				core.Text{
					Content: ed.T("codeReview.editor"),
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextMuted,
					},
				},
				core.View{
					Style: core.Style{
						Height:         core.Pt(18),
						Padding:        core.Symmetric(0, 5),
						CornerRadius:   4,
						BorderWidth:    1,
						BorderColor:    p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Text{
							Content: "Esc",
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   11,
								Color:      p.TextFaint,
							},
						},
					},
				},
			},
		},
		core.View{
			Style: core.Style{
				Width:      core.Pt(1),
				Height:     core.Pt(20),
				Background: p.Line,
				FlexShrink: 0,
			},
		},
		core.Text{
			TestID:  "code-review-title",
			Content: ed.T("codeReview.title"),
			Style: core.Style{
				FontSize:   14,
				FontWeight: core.FontWeightSemibold,
				Color:      p.TextPrimary,
				FlexShrink: 0,
			},
		},
	}

	if strings.TrimSpace(sess.Prompt) != "" {
		leftChildren = append(leftChildren, core.Text{
			TestID:   "code-review-prompt",
			Content:  "“" + strings.TrimSpace(sess.Prompt) + "”",
			MaxLines: 1,
			Style: core.Style{
				FontSize:   12.5,
				Color:      p.TextFaint,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Overflow:   core.Hidden,
			},
		})
	}

	if sess.Round > 1 {
		leftChildren = append(leftChildren, core.View{
			TestID: "code-review-round-pill",
			Style: core.Style{
				Height:         core.Pt(24),
				Padding:        core.Symmetric(0, 10),
				CornerRadius:   12,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
				Background:     p.Raised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("Round %d", sess.Round),
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextMuted,
					},
				},
			},
		})
	}

	rightChildren := make([]core.Node, 0, 3)
	if goNote != "" {
		rightChildren = append(rightChildren, core.Text{
			TestID:  "code-review-go-note",
			Content: goNote,
			Style: core.Style{
				FontSize:   11.5,
				Color:      p.TextFaint,
				FlexShrink: 0,
			},
		})
	}
	rightChildren = append(rightChildren,
		core.View{
			TestID:    "code-review-revert-all",
			Role:      core.RoleButton,
			Label:     ed.T("codeReview.revertAll"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(32),
				Padding:        core.Symmetric(0, 14),
				CornerRadius:   8,
				Background:     p.Raised,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
				FlexShrink:     0,
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.RevertCodeReview())
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("codeReview.revertAll"),
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextSecondary,
					},
				},
			},
		},
		core.View{
			TestID:    "code-review-keep",
			Role:      core.RoleButton,
			Label:     keepLabel,
			Disabled:  func() bool { return !canKeep },
			Focusable: func() bool { return canKeep },
			Style: core.Style{
				Height:         core.Pt(32),
				Padding:        core.Symmetric(0, 14),
				CornerRadius:   8,
				Background:     p.Accent.Fg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
				FlexShrink:     0,
				Opacity:        core.Alpha(boolOpacity(canKeep)),
			},
			OnPress: func(core.PressEvent) {
				if !canKeep {
					return
				}
				ed.report(ed.KeepCodeReview())
			},
			Children: []core.Node{
				core.Text{
					Content: keepLabel,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.Accent.OnFill,
					},
				},
			},
		},
	)

	return core.View{
		TestID: "code-review-header",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			Height:         core.Pt(44),
			FlexShrink:     0,
			Padding:        core.Symmetric(0, 12),
			Gap:            12,
			Background:     p.Bg,
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    p.Line,
			JustifyContent: core.JustifySpaceBetween,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        10,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
				Children: leftChildren,
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        10,
					FlexShrink: 0,
				},
				Children: rightChildren,
			},
		},
	}
}

func codeReviewSidebarView(ed *Editor, sess *CodeReviewSession, p SpecPalette) core.Node {
	yamlFiles := sess.YAMLFiles()
	goFiles := sess.GoFiles()
	sel := sess.SelectedFile()
	selPath := ""
	if sel != nil {
		selPath = sel.Path
	}

	var listItems []core.Node

	if len(yamlFiles) > 0 {
		listItems = append(listItems, core.View{
			Style: core.Style{
				Height:         core.Pt(28),
				Padding:        core.Symmetric(0, 8),
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				specSectionLabel(p, ed.T("codeReview.sectionPrefabs")),
			},
		})
		for _, f := range yamlFiles {
			file := f
			selected := file.Path == selPath
			listItems = append(listItems, codeReviewYAMLRow(ed, file, selected, p))
		}
	}

	if len(goFiles) > 0 {
		topMargin := core.Points(0)
		if len(yamlFiles) > 0 {
			topMargin = 12
		}
		listItems = append(listItems, core.View{
			Style: core.Style{
				Height:         core.Pt(28),
				Margin:         core.Edges{Top: topMargin},
				Padding:        core.Symmetric(0, 8),
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				specSectionLabel(p, ed.T("codeReview.sectionGo")),
			},
		})
		for _, f := range goFiles {
			file := f
			selected := file.Path == selPath
			listItems = append(listItems, codeReviewGoRow(ed, file, selected, p))
		}
	}

	bottomItems := make([]core.Node, 0, 2)
	if sess.Round > 1 && strings.TrimSpace(sess.RoundSummary) != "" {
		bottomItems = append(bottomItems, core.View{
			TestID: "code-review-round-note",
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignStart,
				Gap:          8,
				Padding:      core.All(12),
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				specIcon(lucide.RefreshCw, 14, p.TextMuted),
				core.Text{
					Content: sess.RoundSummary,
					Style: core.Style{
						FontSize:   12,
						Color:      p.TextMuted,
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
					},
				},
			},
		})
	}

	bottomItems = append(bottomItems, core.View{
		TestID: "code-review-why-go-card",
		Style: core.Style{
			Padding:      core.All(12),
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Gap:          8,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("codeReview.whyGoTitle"),
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: core.FontWeightSemibold,
					Color:      p.TextPrimary,
				},
			},
			core.Text{
				Content: ed.T("codeReview.whyGoBody"),
				Style: core.Style{
					FontSize: 12,
					Color:    p.TextMuted,
				},
			},
		},
	})

	return core.View{
		TestID: "code-review-sidebar",
		Style: core.Style{
			Width:       core.Pt(280),
			FlexShrink:  0,
			Background:  p.Panel,
			Borders:     core.Edges{Right: 1},
			BorderColor: p.Line,
			Padding:     core.Symmetric(12, 8),
			Gap:         8,
		},
		Children: []core.Node{
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Gap: 4,
						},
						Children: listItems,
					},
				},
			},
			core.View{
				Style: core.Style{
					Gap:        8,
					FlexShrink: 0,
				},
				Children: bottomItems,
			},
		},
	}
}

func codeReviewFileRowTestID(relPath string) string {
	r := strings.NewReplacer("/", "-", ".", "-", "_", "-")
	return "code-review-file-" + r.Replace(relPath)
}

func codeReviewYAMLRow(ed *Editor, f *CodeReviewFile, selected bool, p SpecPalette) core.Node {
	var bg core.Color
	if selected {
		bg = p.Accent.Fill
	}
	statText := fmt.Sprintf("~%d", max(1, max(f.AddedLines, f.RemovedLines)))
	return core.View{
		TestID:    codeReviewFileRowTestID(f.Path),
		Role:      core.RoleButton,
		Label:     f.BaseName(),
		Focusable: func() bool { return true },
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(32),
			Padding:      core.Symmetric(0, 8),
			Gap:          8,
			CornerRadius: 8,
			Background:   bg,
			FocusVisible: p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			ed.SelectCodeReviewFile(f.Path)
		},
		Children: []core.Node{
			specIcon(lucide.Box, 14, p.TextMuted),
			core.Text{
				Content:  f.BaseName(),
				MaxLines: 1,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      p.TextSecondary,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					Overflow:   core.Hidden,
				},
			},
			core.Text{
				Content: statText,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					Color:      p.Changed.Fg,
					FlexShrink: 0,
				},
			},
		},
	}
}

func codeReviewGoRow(ed *Editor, f *CodeReviewFile, selected bool, p SpecPalette) core.Node {
	var bg core.Color
	if selected {
		bg = p.Accent.Fill
	}

	badgeText := ed.T("codeReview.badgeNeedsOK")
	badgeBg := p.Warning.Fill
	badgeBorder := p.Warning.Edge
	badgeFg := p.Warning.Fg
	if f.Confirmed {
		badgeText = ed.T("codeReview.badgeOK")
		badgeBg = p.Added.Fill
		badgeBorder = p.Added.Edge
		badgeFg = p.Added.Fg
	}

	var statNodes []core.Node
	if f.AddedLines > 0 {
		statNodes = append(statNodes, core.Text{
			Content: fmt.Sprintf("+%d", f.AddedLines),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11,
				Color:      p.Added.Fg,
			},
		})
	}
	if f.RemovedLines > 0 {
		statNodes = append(statNodes, core.Text{
			Content: fmt.Sprintf("−%d", f.RemovedLines),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11,
				Color:      p.Removed.Fg,
			},
		})
	}

	rowChildren := []core.Node{
		specIcon(lucide.CodeXml, 14, p.TextMuted),
		core.Text{
			Content:  f.BaseName(),
			MaxLines: 1,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.TextSecondary,
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Overflow:   core.Hidden,
			},
		},
	}
	if len(statNodes) > 0 {
		rowChildren = append(rowChildren, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
				FlexShrink: 0,
			},
			Children: statNodes,
		})
	}
	rowChildren = append(rowChildren, core.View{
		TestID: codeReviewFileRowTestID(f.Path) + "-badge",
		Style: core.Style{
			Height:         core.Pt(20),
			Padding:        core.Symmetric(0, 6),
			CornerRadius:   10,
			Background:     badgeBg,
			BorderWidth:    1,
			BorderColor:    badgeBorder,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FlexShrink:     0,
		},
		Children: []core.Node{
			core.Text{
				Content: badgeText,
				Style: core.Style{
					FontSize: 11,
					Color:    badgeFg,
				},
			},
		},
	})

	return core.View{
		TestID:    codeReviewFileRowTestID(f.Path),
		Role:      core.RoleButton,
		Label:     f.BaseName() + ", " + badgeText,
		Focusable: func() bool { return true },
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(32),
			Padding:      core.Symmetric(0, 8),
			Gap:          6,
			CornerRadius: 8,
			Background:   bg,
			FocusVisible: p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			ed.SelectCodeReviewFile(f.Path)
		},
		Children: rowChildren,
	}
}

func codeReviewMainPaneView(ed *Editor, sess *CodeReviewSession, p SpecPalette) core.Node {
	file := sess.SelectedFile()
	if file == nil {
		return core.View{
			Style: core.Style{
				FlexGrow:       1,
				FlexBasis:      core.Pt(0),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("codeReview.empty"),
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextFaint,
					},
				},
			},
		}
	}

	children := []core.Node{
		codeReviewFileHeaderView(ed, sess, file, p),
		codeReviewChecksStripView(ed, sess, p),
	}
	if sess.Checks.Phase == CodeReviewChecksFailed {
		children = append(children, codeReviewFailureCardView(ed, sess, p))
	}
	children = append(children,
		codeReviewDiffBoxView(sess, file, p),
		codeReviewConfirmFooterView(ed, sess, file, p),
	)

	return core.View{
		TestID: "code-review-main",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			MinWidth:  core.Pt(0),
			MinHeight: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: children,
	}
}

func codeReviewFileHeaderView(ed *Editor, sess *CodeReviewSession, f *CodeReviewFile, p SpecPalette) core.Node {
	left := []core.Node{
		core.Text{
			TestID:  "code-review-file-path",
			Content: f.Path,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   12,
				Color:      p.TextPrimary,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
			},
		},
	}
	if f.AddedLines > 0 {
		left = append(left, core.Text{
			Content: fmt.Sprintf("+%d", f.AddedLines),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.Added.Fg,
				FlexShrink: 0,
			},
		})
	}
	if f.RemovedLines > 0 {
		left = append(left, core.Text{
			Content: fmt.Sprintf("−%d", f.RemovedLines),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.Removed.Fg,
				FlexShrink: 0,
			},
		})
	}

	var right []core.Node
	if f.Kind == CodeReviewFileGo {
		segBtn := func(testID, label string, active bool, onPress func()) core.Node {
			var bg core.Color
			fg := p.TextMuted
			if active {
				bg = p.LineRaised
				fg = p.TextPrimary
			}
			return core.View{
				TestID:    testID,
				Role:      core.RoleButton,
				Label:     label,
				Focusable: func() bool { return true },
				Style: core.Style{
					Height:         core.Pt(26),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					Background:     bg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					onPress()
				},
				Children: []core.Node{
					core.Text{
						Content: label,
						Style: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      fg,
						},
					},
				},
			}
		}

		right = append(right,
			core.View{
				TestID: "code-review-layout-group",
				Role:   core.RoleGroup,
				Label:  ed.T("codeReview.diffLayout"),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          2,
					Padding:      core.All(2),
					CornerRadius: 8,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FlexShrink:   0,
				},
				Children: []core.Node{
					segBtn("code-review-layout-unified", ed.T("codeReview.unified"), sess.Layout != CodeReviewDiffSplit, func() {
						ed.SetCodeReviewLayout(CodeReviewDiffUnified)
					}),
					segBtn("code-review-layout-split", ed.T("codeReview.split"), sess.Layout == CodeReviewDiffSplit, func() {
						ed.SetCodeReviewLayout(CodeReviewDiffSplit)
					}),
				},
			},
			core.View{
				TestID:    "code-review-open-external",
				Role:      core.RoleButton,
				Label:     ed.ExternalEditorLabel(),
				Focusable: func() bool { return true },
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Height:         core.Pt(30),
					Padding:        core.Symmetric(0, 12),
					Gap:            8,
					CornerRadius:   8,
					Background:     p.Raised,
					BorderWidth:    1,
					BorderColor:    p.LineRaised,
					FocusVisible:   p.FocusRing(2),
					FlexShrink:     0,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.OpenCodeReviewInExternalEditor(f.Path, 1))
				},
				Children: []core.Node{
					specIcon(lucide.ExternalLink, 13, p.TextSecondary),
					core.Text{
						Content: ed.ExternalEditorLabel(),
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextSecondary,
						},
					},
				},
			},
		)
	}

	return core.View{
		TestID: "code-review-file-header",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Height:         core.Pt(44),
			FlexShrink:     0,
			Padding:        core.Symmetric(0, 16),
			Gap:            12,
			Borders:        core.Edges{Bottom: 1},
			BorderColor:    p.Line,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
				Children: left,
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        10,
					FlexShrink: 0,
				},
				Children: right,
			},
		},
	}
}

func codeReviewChecksStripView(ed *Editor, sess *CodeReviewSession, p SpecPalette) core.Node {
	checks := sess.Checks
	pkgLabel := strings.Join(checks.Packages, ", ")
	if pkgLabel == "" {
		pkgLabel = strings.Join(sess.goPackages(), ", ")
	}
	durLabel := formatCheckDuration(checks.Duration)
	metaText := pkgLabel
	if durLabel != "" && pkgLabel != "" {
		metaText = durLabel + " · " + pkgLabel
	} else if durLabel != "" {
		metaText = durLabel
	}

	var stripBg core.Color
	var children []core.Node
	var liveLabel string

	runAgainBtn := core.View{
		TestID:    "code-review-run-again",
		Role:      core.RoleButton,
		Label:     ed.T("codeReview.runAgain"),
		Focusable: func() bool { return true },
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Height:         core.Pt(28),
			Padding:        core.Symmetric(0, 10),
			Gap:            6,
			CornerRadius:   6,
			FocusVisible:   p.FocusRing(2),
			FlexShrink:     0,
		},
		OnPress: func(core.PressEvent) {
			ed.report(ed.RunCodeReviewChecksAsync())
		},
		Children: []core.Node{
			specIcon(lucide.RefreshCw, 13, p.TextMuted),
			core.Text{
				Content: ed.T("codeReview.runAgain"),
				Style: core.Style{
					FontSize: 12,
					Color:    p.TextMuted,
				},
			},
		},
	}

	switch checks.Phase {
	case CodeReviewChecksPassed:
		testText := "nsgo test · passed"
		if checks.PassedCount > 0 {
			testText = fmt.Sprintf("nsgo test · %d passed", checks.PassedCount)
		}
		liveLabel = "nsgo check passed, " + testText
		children = []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
				Children: []core.Node{
					specIcon(lucide.Check, 14, p.Added.Fg),
					core.Text{
						Content: "nsgo check",
						Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
					},
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
				Children: []core.Node{
					specIcon(lucide.Check, 14, p.Added.Fg),
					core.Text{
						TestID:  "code-review-checks-test-summary",
						Content: testText,
						Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
					},
				},
			},
			core.Text{
				TestID:  "code-review-checks-meta",
				Content: metaText,
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			runAgainBtn,
		}

	case CodeReviewChecksRunning, CodeReviewChecksFixing:
		runningText := "nsgo test running"
		if checks.ExpectedTotal > 0 {
			runningText = fmt.Sprintf("nsgo test running · %d of %d", checks.Completed, checks.ExpectedTotal)
		} else if checks.Completed > 0 {
			runningText = fmt.Sprintf("nsgo test running · %d done", checks.Completed)
		}
		if checks.Phase == CodeReviewChecksFixing {
			runningText = "Agent is fixing · " + runningText
		}
		liveLabel = runningText
		pct := float32(35)
		if checks.ExpectedTotal > 0 && checks.Completed >= 0 {
			pct = float32(min(100, max(8, checks.Completed*100/checks.ExpectedTotal)))
		}
		barColor := p.TextMuted
		if checks.Phase == CodeReviewChecksFixing {
			barColor = p.Agent.Fg
		}
		children = []core.Node{
			specIcon(lucide.RefreshCw, 14, barColor),
			core.Text{
				TestID:  "code-review-checks-running-text",
				Content: runningText,
				Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
			},
			core.View{
				Style: core.Style{
					Width:        core.Pt(180),
					Height:       core.Pt(4),
					CornerRadius: 2,
					Background:   p.LineRaised,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:      core.Pct(pct),
							Height:     core.Pt(4),
							Background: barColor,
						},
					},
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
		}
		if checks.Phase == CodeReviewChecksFixing && checks.AgentEditBadge != "" {
			children = append(children, core.View{
				TestID: "code-review-agent-edit-pill",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 10),
					Gap:          6,
					CornerRadius: 12,
					Background:   p.Agent.Fill,
					BorderWidth:  1,
					BorderColor:  p.Agent.Edge,
					FlexShrink:   0,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Agent.Fg,
						},
					},
					core.Text{
						Content: checks.AgentEditBadge,
						Style:   core.Style{FontSize: 11.5, Color: p.TextSecondary},
					},
				},
			})
		}

	case CodeReviewChecksFailed:
		stripBg = p.Removed.Fill
		checkIcon := lucide.Check
		checkColor := p.Added.Fg
		checkText := "nsgo check"
		if !checks.CheckPassed {
			checkIcon = lucide.X
			checkColor = p.Removed.Fg
			checkText = "nsgo check failed"
		}
		testText := "nsgo test · failed"
		if checks.FailedCount > 0 {
			testText = fmt.Sprintf("nsgo test · %d failed, %d passed", checks.FailedCount, checks.PassedCount)
		} else if !checks.CheckPassed {
			testText = "nsgo test · skipped"
		}
		liveLabel = checkText + ", " + testText
		children = []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
				Children: []core.Node{
					specIcon(checkIcon, 14, checkColor),
					core.Text{
						Content: checkText,
						Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
					},
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
				Children: []core.Node{
					specIcon(lucide.X, 14, p.Removed.Fg),
					core.Text{
						TestID:  "code-review-checks-test-summary",
						Content: testText,
						Style:   core.Style{FontSize: 12.5, Color: p.Removed.Fg},
					},
				},
			},
			core.Text{
				TestID:  "code-review-checks-meta",
				Content: metaText,
				Style:   core.Style{FontSize: 12, Color: p.TextFaint},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			runAgainBtn,
		}

	default:
		liveLabel = "Checks have not run yet"
		children = []core.Node{
			core.Text{
				TestID:  "code-review-checks-idle",
				Content: ed.T("codeReview.checksNotRun"),
				Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			runAgainBtn,
		}
	}

	return core.View{
		TestID:     "code-review-checks-strip",
		Role:       core.RoleStatus,
		LiveRegion: core.LiveRegionPolite,
		Label:      liveLabel,
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Height:      core.Pt(40),
			FlexShrink:  0,
			Padding:     core.Symmetric(0, 16),
			Gap:         14,
			Background:  stripBg,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
		},
		Children: children,
	}
}

func codeReviewFailureCardView(ed *Editor, sess *CodeReviewSession, p SpecPalette) core.Node {
	failure := sess.Checks.PrimaryFailure()
	title := "nsgo check failed"
	msg := sess.Checks.CheckError
	explanation := sess.FailureExplanation
	hasTest := false
	if failure != nil {
		hasTest = true
		title = failure.TestName + " failed"
		msg = failure.Message
		if failure.Explanation != "" {
			explanation = failure.Explanation
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
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 3,
						Background:   p.Removed.Fg,
						FlexShrink:   0,
					},
				},
				core.Text{
					TestID:  "code-review-failure-title",
					Content: title,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.Removed.Fg,
					},
				},
			},
		},
	}

	if msg != "" {
		cardChildren = append(cardChildren, core.Text{
			TestID:  "code-review-failure-msg",
			Content: msg,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.TextPrimary,
			},
		})
	}
	if explanation != "" {
		cardChildren = append(cardChildren, core.Text{
			TestID:  "code-review-failure-why",
			Content: explanation,
			Style: core.Style{
				FontSize: 12.5,
				Color:    p.TextSecondary,
			},
		})
	}

	actions := []core.Node{
		core.View{
			TestID:    "code-review-ask-fix",
			Role:      core.RoleButton,
			Label:     ed.T("codeReview.askFix"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(30),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				Background:     p.Accent.Fg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.AskAgentToFixCodeReview())
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("codeReview.askFix"),
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.Accent.OnFill,
					},
				},
			},
		},
	}

	if hasTest {
		actions = append(actions, core.View{
			TestID:    "code-review-open-test",
			Role:      core.RoleButton,
			Label:     ed.T("codeReview.openTest"),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(30),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				Background:     p.Raised,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.OpenCodeReviewTestFile())
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("codeReview.openTest"),
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextSecondary,
					},
				},
			},
		})
	}

	cardChildren = append(cardChildren, core.View{
		Style: core.Style{
			Direction: core.Row,
			Gap:       8,
		},
		Children: actions,
	})

	return core.View{
		TestID: "code-review-failure-card",
		Role:   core.RoleRegion,
		Label:  title,
		Style: core.Style{
			Margin:       core.Edges{Top: 12, Left: 16, Right: 16},
			Padding:      core.All(12),
			CornerRadius: 12,
			Background:   p.Removed.Fill,
			BorderWidth:  1,
			BorderColor:  p.Removed.Edge,
			Gap:          8,
			FlexShrink:   0,
		},
		Children: cardChildren,
	}
}

func codeReviewDiffBoxView(sess *CodeReviewSession, f *CodeReviewFile, p SpecPalette) core.Node {
	useSplit := f.Kind == CodeReviewFileGo && sess.Layout == CodeReviewDiffSplit
	var rows []core.Node
	if useSplit {
		splitRows := BuildCodeReviewSplitRows(f.Diff)
		rows = make([]core.Node, 0, len(splitRows))
		for i, r := range splitRows {
			rows = append(rows, codeReviewSplitRowView(i, r, p))
		}
	} else {
		rows = make([]core.Node, 0, len(f.Diff))
		for i, l := range f.Diff {
			rows = append(rows, codeReviewUnifiedRowView(i, l, p))
		}
	}

	return core.View{
		TestID: "code-review-diff-box",
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			MinHeight:    core.Pt(0),
			Margin:       core.Symmetric(12, 16),
			CornerRadius: 12,
			Background:   p.Panel,
			BorderWidth:  1,
			BorderColor:  p.Line,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			ui.ScrollView{
				TestID: "code-review-diff-scroll",
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Padding: core.Symmetric(8, 0),
						},
						Children: rows,
					},
				},
			},
		},
	}
}

func codeReviewUnifiedRowView(idx int, l CodeReviewDiffLine, p SpecPalette) core.Node {
	var rowBg core.Color
	textCol := p.TextMuted
	signCol := p.TextFaint
	switch l.Kind {
	case DiffLineAdd:
		rowBg = p.Added.Fill
		textCol = p.TextPrimary
		signCol = p.Added.Fg
	case DiffLineDel:
		rowBg = p.Removed.Fill
		textCol = p.TextSecondary
		signCol = p.Removed.Fg
	}

	rowStyle := core.Style{
		Direction:  core.Row,
		AlignItems: core.AlignCenter,
		MinHeight:  core.Pt(21),
		Padding:    core.Edges{Left: 6, Right: 12, Top: 1, Bottom: 1},
		Gap:        6,
		Background: rowBg,
	}
	if l.Tag == CodeReviewTagFailsTest || l.Tag == CodeReviewTagFailedHere {
		rowStyle.Borders = core.Edges{Left: 2}
		rowStyle.BorderColor = p.Removed.Fg
	}

	oldNum := ""
	if l.OldLine > 0 {
		oldNum = fmt.Sprintf("%d", l.OldLine)
	}
	newNum := ""
	if l.NewLine > 0 {
		newNum = fmt.Sprintf("%d", l.NewLine)
	}

	lineNumCell := func(w core.Points, txt string) core.Node {
		var cellChildren []core.Node
		if txt != "" {
			cellChildren = []core.Node{
				core.Text{
					Content: txt,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextFaint,
					},
				},
			}
		}
		return core.View{
			Style: core.Style{
				Width:          core.Pt(w),
				AlignItems:     core.AlignEnd,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: cellChildren,
		}
	}

	var signChildren []core.Node
	if l.Sign != "" {
		signChildren = []core.Node{
			core.Text{
				Content: l.Sign,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      signCol,
				},
			},
		}
	}

	children := []core.Node{
		lineNumCell(32, oldNum),
		lineNumCell(32, newNum),
		core.View{
			Style: core.Style{
				Width:          core.Pt(16),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: signChildren,
		},
		core.Text{
			Content: expandCodeTabs(l.Text),
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      textCol,
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
			},
		},
	}
	if l.Tag != CodeReviewTagNone {
		children = append(children, codeReviewLineTagNode(idx, l.Tag, p))
	}

	return core.View{
		TestID:   fmt.Sprintf("code-review-diff-line-%d", idx),
		Style:    rowStyle,
		Children: children,
	}
}

func codeReviewSplitRowView(idx int, r CodeReviewSplitRow, p SpecPalette) core.Node {
	leftBg := p.Bg
	leftCol := p.TextMuted
	if r.LeftKind == DiffLineDel {
		leftBg = p.Removed.Fill
		leftCol = p.TextSecondary
	} else if r.LeftKind == DiffLineContext {
		leftBg = 0
	}

	rightBg := p.Bg
	rightCol := p.TextMuted
	if r.RightKind == DiffLineAdd {
		rightBg = p.Added.Fill
		rightCol = p.TextPrimary
	} else if r.RightKind == DiffLineContext {
		rightBg = 0
	}

	oldNum := ""
	if r.OldLine > 0 {
		oldNum = fmt.Sprintf("%d", r.OldLine)
	}
	newNum := ""
	if r.NewLine > 0 {
		newNum = fmt.Sprintf("%d", r.NewLine)
	}

	splitNumCell := func(txt string) core.Node {
		var cellChildren []core.Node
		if txt != "" {
			cellChildren = []core.Node{
				core.Text{
					Content: txt,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextFaint,
					},
				},
			}
		}
		return core.View{
			Style: core.Style{
				Width:          core.Pt(28),
				AlignItems:     core.AlignEnd,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
			},
			Children: cellChildren,
		}
	}

	rightChildren := []core.Node{
		splitNumCell(newNum),
		core.Text{
			Content:  expandCodeTabs(r.RightText),
			MaxLines: 1,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      rightCol,
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Overflow:   core.Hidden,
			},
		},
	}
	if r.RightTag != CodeReviewTagNone {
		rightChildren = append(rightChildren, codeReviewLineTagNode(idx, r.RightTag, p))
	}

	return core.View{
		TestID: fmt.Sprintf("code-review-split-row-%d", idx),
		Style: core.Style{
			Direction: core.Row,
			MinHeight: core.Pt(21),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					FlexGrow:   1,
					FlexBasis:  core.Pt(0),
					MinWidth:   core.Pt(0),
					Gap:        8,
					Padding:    core.Symmetric(1, 8),
					Background: leftBg,
				},
				Children: []core.Node{
					splitNumCell(oldNum),
					core.Text{
						Content:  expandCodeTabs(r.LeftText),
						MaxLines: 1,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      leftCol,
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							Overflow:   core.Hidden,
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					FlexGrow:    1,
					FlexBasis:   core.Pt(0),
					MinWidth:    core.Pt(0),
					Gap:         8,
					Padding:     core.Symmetric(1, 8),
					Background:  rightBg,
					Borders:     core.Edges{Left: 1},
					BorderColor: p.Line,
				},
				Children: rightChildren,
			},
		},
	}
}

func codeReviewLineTagNode(idx int, tag CodeReviewLineTag, p SpecPalette) core.Node {
	bg := p.Raised
	border := p.LineRaised
	fg := p.TextMuted
	switch tag {
	case CodeReviewTagFailsTest, CodeReviewTagFailedHere:
		bg = p.Removed.Fill
		border = p.Removed.Edge
		fg = p.Removed.Fg
	case CodeReviewTagAgentEdited:
		bg = p.Agent.Fill
		border = p.Agent.Edge
		fg = p.Agent.Fg
	}
	return core.View{
		TestID: fmt.Sprintf("code-review-line-tag-%d", idx),
		Style: core.Style{
			Height:         core.Pt(18),
			Padding:        core.Symmetric(0, 8),
			CornerRadius:   6,
			Background:     bg,
			BorderWidth:    1,
			BorderColor:    border,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FlexShrink:     0,
		},
		Children: []core.Node{
			core.Text{
				Content: string(tag),
				Style: core.Style{
					FontSize: 11,
					Color:    fg,
				},
			},
		},
	}
}

func codeReviewConfirmFooterView(ed *Editor, sess *CodeReviewSession, f *CodeReviewFile, p SpecPalette) core.Node {
	var children []core.Node
	if f.Kind == CodeReviewFileGo {
		canConfirm := ed.CanConfirmCodeReviewFile(f.Path)
		confirmed := f.Confirmed
		var hint string
		switch {
		case confirmed:
			hint = ed.T("codeReview.hintConfirmed")
		case canConfirm:
			hint = ed.T("codeReview.hintChecksPass")
		case sess.Checks.Phase == CodeReviewChecksFailed:
			if !sess.Checks.CheckPassed {
				hint = ed.T("codeReview.hintCheckFailed")
			} else {
				hint = ed.T("codeReview.hintTestFailed")
			}
		default:
			hint = ed.T("codeReview.hintWaiting")
		}

		btnLabel := "Confirm " + f.BaseName()
		btnBg := p.Accent.Fg
		btnFg := p.Accent.OnFill
		var btnBorder core.Color
		var btnBorderW core.Points
		if confirmed {
			btnLabel = ed.T("codeReview.confirmedUndo")
			btnBg = p.Raised
			btnFg = p.Added.Fg
			btnBorder = p.LineRaised
			btnBorderW = 1
		}

		enabled := canConfirm || confirmed
		children = []core.Node{
			core.Text{
				TestID:  "code-review-confirm-hint",
				Content: hint,
				Style: core.Style{
					FontSize:   12.5,
					Color:      p.TextMuted,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
			},
			core.View{
				TestID:    "code-review-confirm-btn",
				Role:      core.RoleButton,
				Label:     btnLabel,
				Disabled:  func() bool { return !enabled },
				Focusable: func() bool { return enabled },
				Style: core.Style{
					Height:         core.Pt(32),
					Padding:        core.Symmetric(0, 14),
					CornerRadius:   8,
					Background:     btnBg,
					BorderWidth:    btnBorderW,
					BorderColor:    btnBorder,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
					FlexShrink:     0,
					Opacity:        core.Alpha(boolOpacity(enabled)),
				},
				OnPress: func(core.PressEvent) {
					if !enabled {
						return
					}
					ed.report(ed.ConfirmCodeReviewFile(f.Path, !confirmed))
				},
				Children: []core.Node{
					core.Text{
						Content: btnLabel,
						Style: core.Style{
							FontSize:   12.5,
							FontWeight: core.FontWeightSemibold,
							Color:      btnFg,
						},
					},
				},
			},
		}
	} else {
		hint := f.Note
		if strings.TrimSpace(hint) == "" {
			hint = ed.T("codeReview.yamlPartOfKeep")
		}
		children = []core.Node{
			core.Text{
				TestID:  "code-review-confirm-hint",
				Content: hint,
				Style: core.Style{
					FontSize:   12.5,
					Color:      p.TextMuted,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
				},
			},
		}
	}

	return core.View{
		TestID: "code-review-footer",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			MinHeight:   core.Pt(52),
			FlexShrink:  0,
			Padding:     core.Symmetric(8, 16),
			Gap:         12,
			Background:  p.Panel,
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
		},
		Children: children,
	}
}

// agentCodeReviewWaitingCard renders the Agent panel card for unconfirmed Go files left waiting after Keep
// (or a paused review after Esc), matching docs/design/editor/Review.dc.html.
func agentCodeReviewWaitingCard(ed *Editor, p SpecPalette) core.Node {
	sess := ed.CodeReviewSession()
	if sess == nil || sess.Open {
		return nil
	}
	waitingGo := sess.WaitingGoFiles()
	if sess.KeptYAML && len(waitingGo) > 0 {
		names := make([]string, len(waitingGo))
		for i, f := range waitingGo {
			names[i] = f.BaseName()
		}
		var title, btnLabel string
		if len(names) == 1 {
			title = names[0] + " is waiting for your OK"
			btnLabel = "Review " + names[0]
		} else if len(names) == 2 {
			title = names[0] + " and " + names[1] + " are waiting for your OK"
			btnLabel = ed.T("codeReview.reviewCode")
		} else {
			title = fmt.Sprintf("%d Go files are waiting for your OK", len(names))
			btnLabel = ed.T("codeReview.reviewCode")
		}
		impact := strings.TrimSpace(sess.PlayImpact)
		if impact == "" {
			impact = ed.T("codeReview.waitingDefaultImpact")
		}
		body := ed.T("codeReview.waitingGoPrefix") + " " + impact
		firstPath := waitingGo[0].Path
		return core.View{
			TestID: "agent-go-waiting-card",
			Role:   core.RoleRegion,
			Label:  title,
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Padding:      core.All(12),
				Gap:          8,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-go-waiting-title",
					Content: title,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.TextPrimary,
					},
				},
				core.Text{
					TestID:  "agent-go-waiting-body",
					Content: body,
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextMuted,
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
					},
					Children: []core.Node{
						core.View{
							TestID:    "agent-go-waiting-review",
							Role:      core.RoleButton,
							Label:     btnLabel,
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(30),
								Padding:        core.Symmetric(0, 12),
								CornerRadius:   8,
								Background:     p.Raised,
								BorderWidth:    1,
								BorderColor:    p.Accent.Edge,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.OpenCodeReview(firstPath)
							},
							Children: []core.Node{
								core.Text{
									Content: btnLabel,
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
			},
		}
	}

	if sess.Paused && (len(sess.YAMLFiles()) > 0 || len(sess.GoFiles()) > 0) {
		totalFiles := len(sess.YAMLFiles()) + len(sess.GoFiles())
		title := fmt.Sprintf("Review waiting · %d files", totalFiles)
		if totalFiles == 1 {
			title = "Review waiting · 1 file"
		}
		detail := fmt.Sprintf("%d Go file(s) waiting for your OK", len(waitingGo))
		if len(waitingGo) == 1 {
			detail = "1 Go file waiting for your OK"
		} else if len(waitingGo) > 1 {
			detail = fmt.Sprintf("%d Go files waiting for your OK", len(waitingGo))
		}
		return core.View{
			TestID: "agent-review-paused-card",
			Role:   core.RoleRegion,
			Label:  title,
			Style: core.Style{
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Padding:      core.All(12),
				Gap:          8,
			},
			Children: []core.Node{
				core.Text{
					Content: title,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.TextPrimary,
					},
				},
				core.Text{
					Content: detail,
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextMuted,
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
					},
					Children: []core.Node{
						core.View{
							TestID:    "agent-review-resume",
							Role:      core.RoleButton,
							Label:     ed.T("codeReview.resumeReview"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(30),
								Padding:        core.Symmetric(0, 12),
								CornerRadius:   8,
								Background:     p.Accent.Fg,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.OpenCodeReview("")
							},
							Children: []core.Node{
								core.Text{
									Content: ed.T("codeReview.resumeReview"),
									Style: core.Style{
										FontSize:   12.5,
										FontWeight: core.FontWeightSemibold,
										Color:      p.Accent.OnFill,
									},
								},
							},
						},
					},
				},
			},
		}
	}
	return nil
}

func formatCheckDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	secs := d.Seconds()
	if secs < 0.1 {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1f s", secs)
}

func expandCodeTabs(text string) string {
	return strings.ReplaceAll(text, "\t", "    ")
}

func boolOpacity(enabled bool) float32 {
	if enabled {
		return 1
	}
	return 0.45
}
