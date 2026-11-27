package editor

import (
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	reviewRegionHeader = "review-header"
	reviewRegionList   = "review-list"
	reviewRegionCenter = "review-center"
	reviewRegionRight  = "review-right"
)

var reviewRegionOrder = []string{
	reviewRegionHeader,
	reviewRegionList,
	reviewRegionCenter,
	reviewRegionRight,
}

// ReviewScreenView renders the full-window change-by-change agent turn Review screen
// matching design/editor/Review.dc.html and design/editor/Spec.dc.html
// using exclusively SpecPalette tokens (supporting both Dark and Light appearances).
func ReviewScreenView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	reviewRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		r := ed.reviewUIRev()
		if ed.ResolvedLanguage() != "en" {
			r ^= 1 << 63
		}
		return r
	})
	activeRegion := core.NewSignal(cx, "")
	headerRef := &core.Ref{}
	listRef := &core.Ref{}
	centerRef := &core.Ref{}
	rightRef := &core.Ref{}
	revertBtnRef := &core.Ref{}
	filterInputRef := &core.Ref{}
	yamlInputRef := &core.Ref{}

	regionRefs := map[string]*core.Ref{
		reviewRegionHeader: headerRef,
		reviewRegionList:   listRef,
		reviewRegionCenter: centerRef,
		reviewRegionRight:  rightRef,
	}

	cycleReviewRegion := func(delta int) {
		if !ed.ReviewOpen() || ed.reviewModalBlocking() {
			return
		}
		cur := activeRegion.GetUntracked()
		idx := slices.Index(reviewRegionOrder, cur)
		var next int
		if idx < 0 {
			if delta > 0 {
				next = 0
			} else {
				next = len(reviewRegionOrder) - 1
			}
		} else {
			next = (idx + delta%len(reviewRegionOrder) + len(reviewRegionOrder)) % len(reviewRegionOrder)
		}
		target := reviewRegionOrder[next]
		activeRegion.Set(target)
		ed.review.activeRegion = target
		if ref := regionRefs[target]; ref != nil {
			ref.Focus()
		}
	}

	// Bind ScopeReview command shortcuts (j, k, x, b, mod+enter, mod+backspace, plus any user overrides)
	// strictly inside ReviewScreenView's scope so they never intercept keys when Review is closed.
	ed.bindReviewShortcuts(cx, rev)

	// Track held B key for Before-request peek while Review is open and not typing in a text field.
	cx.Effect(func() {
		held := core.KeyHeld(cx, "b")
		if ed.ReviewOpen() && !ed.reviewModalBlocking() && !ed.IsTyping() {
			ed.SetReviewPeekBefore(held)
		} else if ed.ReviewPeekingBefore() {
			ed.SetReviewPeekBefore(false)
		}
	})
	cx.Cleanup(func() {
		ed.SetReviewPeekBefore(false)
	})

	// F6 / Shift-F6 cycles Review's 4 regions: Header -> List -> Center -> Right.
	cx.OnKeyWith("f6", core.KeyOptions{When: core.KeyAlways}, func() {
		if ed.ReviewOpen() && !ed.reviewModalBlocking() {
			cycleReviewRegion(+1)
		}
	})
	cx.OnKeyWith("shift+f6", core.KeyOptions{When: core.KeyAlways}, func() {
		if ed.ReviewOpen() && !ed.reviewModalBlocking() {
			cycleReviewRegion(-1)
		}
	})

	// Esc steps back cleanly: close revert dialog -> yield to overlays -> cancel note composer -> blur filter field -> pause review back to editor.
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
		if !ed.ReviewOpen() || ed.shortcutCaptureActive {
			return
		}
		if ed.ReviewRevertAllDialogOpen() {
			ed.CancelReviewRevertAllDialog()
			return
		}
		if ed.reviewModalBlocking() {
			return
		}
		if ed.ReviewNoteComposerChangeID() != "" {
			ed.CancelReviewNoteComposer()
			return
		}
		if ed.IsTyping() {
			if filterInputRef.HoldsFocus() || yamlInputRef.HoldsFocus() {
				headerRef.Focus()
			}
			return
		}
		ed.PauseReview()
	})

	// Arrow keys walk visible rows (file headers, kind headers, and change cards) when not typing.
	cx.OnKeyWith("down", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.ReviewOpen() && !ed.reviewModalBlocking() {
			ed.StepReviewRow(+1)
		}
	})
	cx.OnKeyWith("up", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.ReviewOpen() && !ed.reviewModalBlocking() {
			ed.StepReviewRow(-1)
		}
	})

	// Revert All confirmation modal with Spec dialog focus trapping and opener focus restoration.
	revertModalOpen := func() bool {
		_ = reviewRev.Get()
		return ed.ReviewRevertAllDialogOpen()
	}
	onRevertModalClose := bindDialogFocusRestore(cx, ed, revertModalOpen)

	revertModal := ui.Dialog{
		TestID: "review-revert-all-dialog",
		Bare:   true,
		Open:   revertModalOpen,
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				ed.CancelReviewRevertAllDialog()
			}
		},
		OnClose: onRevertModalClose,
		Label:   ed.T("reviewScreen.revertAllDialogLabel"),
		Style: core.Style{
			CornerRadius: 18,
			Background:   core.Transparent,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = reviewRev.Get()
				p := ed.SpecPalette(cx)
				snap := ed.ReviewStatus()
				undoKey := ed.reviewShortcutHint(CmdUndo, "mod+z")
				fileList := ed.ReviewRevertAllFileList()
				noteWarning := ""
				if dropped := ed.ReviewUnsentOrPendingNoteCount(); dropped == 1 {
					noteWarning = ed.T("reviewScreen.revertAllDropNoteOne")
				} else if dropped > 1 {
					noteWarning = ed.T("reviewScreen.revertAllDropNoteMany", l10n.Param("count", dropped))
				}
				body := ed.T("reviewScreen.revertAllBodyMany", l10n.Param("files", fileList), l10n.Param("noteWarning", noteWarning), l10n.Param("undoKey", undoKey))
				if snap.FileCount == 1 {
					body = ed.T("reviewScreen.revertAllBodyOne", l10n.Param("files", fileList), l10n.Param("noteWarning", noteWarning), l10n.Param("undoKey", undoKey))
				}
				shadowAlpha := float32(0.50)
				if !p.Dark {
					shadowAlpha = 0.20
				}
				revertTitle := ed.T("reviewScreen.revertAllTitleMany", l10n.Param("count", snap.TotalChanges))
				if snap.TotalChanges == 1 {
					revertTitle = ed.T("reviewScreen.revertAllTitleOne")
				}
				return core.View{
					Role:  core.RoleAlertDialog,
					Label: ed.T("reviewScreen.revertAllAlertLabel", l10n.Param("count", snap.TotalChanges)),
					Style: core.Style{
						Width:        core.Pt(440),
						Padding:      core.All(24),
						Gap:          12,
						CornerRadius: 18,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Shadows: []core.BoxShadow{
							ui.Shadow(0, 24, 60, 0, ui.Black(shadowAlpha)),
						},
					},
					Children: []core.Node{
						core.Text{
							TestID:  "review-revert-all-title",
							Content: revertTitle,
							Style: core.Style{
								FontSize:   14,
								FontWeight: 600,
								Color:      p.TextPrimary,
							},
						},
						core.Text{
							TestID:  "review-revert-all-body",
							Content: body,
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextMuted,
							},
						},
						core.View{
							Style: core.Style{
								Direction:      core.Row,
								JustifyContent: core.JustifyEnd,
								Gap:            8,
								Margin:         core.Edges{Top: 8},
							},
							Children: []core.Node{
								specReviewButton("review-revert-all-cancel", ed.T("reviewScreen.cancel"), p.Panel, p.LineRaised, p.TextSecondary, p, func() {
									ed.CancelReviewRevertAllDialog()
								}),
								specReviewButton("review-revert-all-confirm", ed.T("reviewScreen.revertAllConfirm", l10n.Param("count", snap.TotalChanges)), p.Removed.Fg, core.Transparent, p.Removed.OnFill, p, func() {
									ed.report(ed.ConfirmReviewRevertAll())
								}),
							},
						},
					},
				}
			}),
		},
	}

	return core.View{
		TestID: "review-screen",
		Role:   core.RoleRegion,
		Label:  ed.T("reviewScreen.screenLabel"),
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
				Overflow:   core.Hidden,
			}
		},
		Children: []core.Node{
			reviewHeaderView(cx, ed, reviewRev, activeRegion, headerRef, revertBtnRef),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinHeight: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					reviewSidebarView(cx, ed, reviewRev, activeRegion, listRef, filterInputRef),
					reviewCenterView(cx, ed, reviewRev, activeRegion, centerRef),
					reviewRightView(cx, ed, reviewRev, activeRegion, rightRef, yamlInputRef),
				},
			},
			revertModal,
		},
	}
}

func reviewRegionStyle(p SpecPalette, activeRegion *core.Signal[string], id string, base core.Style) core.Style {
	if activeRegion != nil && activeRegion.Get() == id {
		return base.Merge(*p.FocusRing(-2))
	}
	return base
}

func specReviewButton(testID, label string, bg, border, fg core.Color, p SpecPalette, onPress func()) core.Node {
	st := core.Style{
		Height:         core.Pt(32),
		Padding:        core.Symmetric(0, 14),
		CornerRadius:   8,
		Background:     bg,
		AlignItems:     core.AlignCenter,
		JustifyContent: core.JustifyCenter,
		FlexShrink:     0,
		FocusVisible:   p.FocusRing(2),
	}
	if border != core.Transparent {
		st.BorderWidth = 1
		st.BorderColor = border
	}
	return core.View{
		TestID:    testID,
		Role:      core.RoleButton,
		Label:     label,
		Focusable: func() bool { return true },
		Style:     st,
		OnPress: func(core.PressEvent) {
			if onPress != nil {
				onPress()
			}
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 600,
					Color:      fg,
				},
			},
		},
	}
}

func specReviewKeycap(text string, p SpecPalette) core.Node {
	return core.View{
		Style: core.Style{
			Height:         core.Pt(18),
			Padding:        core.Symmetric(0, 5),
			CornerRadius:   4,
			BorderWidth:    1,
			BorderColor:    p.LineRaised,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FlexShrink:     0,
		},
		Children: []core.Node{
			core.Text{
				Content: text,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					Color:      p.TextFaint,
				},
			},
		},
	}
}

func reviewHeaderView(cx *core.Scope, ed *Editor, reviewRev *core.Memo[uint64], activeRegion *core.Signal[string], headerRef, revertBtnRef *core.Ref) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		snap := ed.ReviewStatus()

		keepTotal := snap.KeptCount + snap.RevertedCount
		keepLabel := ed.T("reviewScreen.keepCount", l10n.Param("kept", snap.KeptCount), l10n.Param("total", keepTotal))
		blocked := len(snap.Blockers) > 0
		keepTitle := ed.T("reviewScreen.keepTitle", l10n.Param("key", ed.reviewShortcutHint(CmdReviewKeep, "mod+enter")))
		if blocked {
			keepTitle = snap.Blockers[0].Reason
		}

		headerChildren := []core.Node{
			core.View{
				TestID:    "review-back-to-editor",
				Role:      core.RoleButton,
				Label:     ed.T("reviewScreen.backLabel"),
				Focusable: func() bool { return true },
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Padding:      core.Symmetric(0, 8),
					Gap:          8,
					CornerRadius: 6,
					FlexShrink:   0,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.PauseReview()
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.ArrowLeft(),
						Style: core.Style{Width: core.Pt(15), Height: core.Pt(15), Color: p.TextMuted},
					},
					core.Text{
						Content: ed.T("reviewScreen.editor"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
					},
					specReviewKeycap("Esc", p),
				},
			},
			core.View{
				Style: core.Style{Width: core.Pt(1), Height: core.Pt(20), Background: p.Line, FlexShrink: 0},
			},
			core.Text{
				TestID:  "review-title",
				Content: ed.T("reviewScreen.title"),
				Style: core.Style{
					FontSize:   14,
					FontWeight: 600,
					Color:      p.TextPrimary,
					FlexShrink: 0,
				},
			},
		}

		if snap.Request != "" {
			headerChildren = append(headerChildren, core.Text{
				TestID:   "review-request-quote",
				Content:  "“" + snap.Request + "”",
				MaxLines: 1,
				Style: core.Style{
					FontSize:   12.5,
					Color:      p.TextFaint,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					MaxWidth:   core.Pt(380),
					Overflow:   core.Hidden,
				},
			})
		}

		if snap.Round > 1 {
			roundTabs := make([]core.Node, 0, snap.Round)
			for r := 1; r <= snap.Round; r++ {
				roundNum := r
				active := snap.ViewRound == roundNum
				bg := core.Transparent
				fg := p.TextMuted
				if active {
					bg = p.LineRaised
					fg = p.TextPrimary
				}
				roundText := ed.T("reviewScreen.round", l10n.Param("round", roundNum))
				roundTabs = append(roundTabs, core.View{
					TestID:    fmt.Sprintf("review-round-tab-%d", roundNum),
					Role:      core.RoleButton,
					Label:     roundText,
					Focusable: func() bool { return true },
					Style: core.Style{
						Height:         core.Pt(26),
						Padding:        core.Symmetric(0, 10),
						CornerRadius:   6,
						Background:     bg,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.SetReviewViewRound(roundNum))
					},
					Children: []core.Node{
						core.Text{
							Content: roundText,
							Style:   core.Style{FontSize: 12, FontWeight: 500, Color: fg},
						},
					},
				})
			}
			headerChildren = append(headerChildren, core.View{
				TestID: "review-round-tabs",
				Role:   core.RoleGroup,
				Label:  ed.T("reviewScreen.roundGroupLabel"),
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
				Children: roundTabs,
			})
		}

		headerChildren = append(headerChildren, core.View{Style: core.Style{FlexGrow: 1}})

		if blocked {
			blocker := snap.Blockers[0]
			reason := blocker.Reason
			blockerRole := p.Warning
			if blocker.Kind == "yaml_error" {
				blockerRole = p.Removed
			}
			headerChildren = append(headerChildren, core.View{
				TestID:    "review-blocker-pill",
				Role:      core.RoleButton,
				Label:     reason,
				Focusable: func() bool { return true },
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Padding:      core.Symmetric(0, 10),
					Gap:          8,
					CornerRadius: 6,
					Background:   blockerRole.Fill,
					BorderWidth:  1,
					BorderColor:  blockerRole.Edge,
					FlexShrink:   0,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.JumpToReviewBlocker()
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.TriangleAlert(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: blockerRole.Fg},
					},
					core.Text{
						Content: reason,
						Style:   core.Style{FontSize: 12, FontWeight: 500, Color: blockerRole.Fg},
					},
				},
			})
		}

		headerChildren = append(headerChildren,
			core.View{
				Ref:       revertBtnRef,
				TestID:    "review-revert-all",
				Role:      core.RoleButton,
				Label:     ed.T("reviewScreen.revertAllLabel", l10n.Param("key", ed.reviewShortcutHint(CmdReviewRevertAll, "mod+backspace"))),
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
					FlexShrink:     0,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					if ed.focus.ctrl != nil {
						ed.focus.ctrl.recordDialogOpener(revertBtnRef)
					}
					ed.OpenReviewRevertAllDialog()
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("reviewScreen.revertAll"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
					},
				},
			},
		)

		hasDraftNotes := snap.DraftNoteCount > 0 && !snap.ViewingReadOnlyRound
		keepBg := p.Accent.Fg
		keepFg := p.Accent.OnFill
		keepBorder := core.Transparent
		keepBorderW := core.Points(0)
		if hasDraftNotes {
			keepBg = p.Raised
			keepFg = p.TextSecondary
			keepBorder = p.LineRaised
			keepBorderW = 1
		}
		var keepOpacity core.Opacity
		if blocked {
			keepOpacity = core.Alpha(0.55)
		}
		headerChildren = append(headerChildren, core.View{
			TestID:    "review-keep",
			Role:      core.RoleButton,
			Label:     keepLabel + " · " + keepTitle,
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(32),
				Padding:        core.Symmetric(0, 14),
				CornerRadius:   8,
				Background:     keepBg,
				BorderWidth:    keepBorderW,
				BorderColor:    keepBorder,
				Opacity:        keepOpacity,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				if err := ed.KeepTurnReview(); err != nil && !errors.Is(err, ErrReviewBlocked) {
					ed.report(err)
				}
			},
			Children: []core.Node{
				core.Text{
					TestID:  "review-keep-label",
					Content: keepLabel,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      keepFg,
					},
				},
			},
		})

		if hasDraftNotes {
			sendLabel := ed.T("reviewScreen.sendNotesMany", l10n.Param("count", snap.DraftNoteCount))
			if snap.DraftNoteCount == 1 {
				sendLabel = ed.T("reviewScreen.sendNotesOne")
			}
			headerChildren = append(headerChildren, core.View{
				TestID:    "review-send-notes",
				Role:      core.RoleButton,
				Label:     sendLabel,
				Focusable: func() bool { return true },
				Style: core.Style{
					Height:         core.Pt(32),
					Padding:        core.Symmetric(0, 14),
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Gap:            8,
					CornerRadius:   8,
					Background:     p.Accent.Fg,
					FlexShrink:     0,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					if _, err := ed.SendReviewNotes(); err != nil {
						ed.report(err)
					}
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.MessageSquare(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.Accent.OnFill},
					},
					core.Text{
						TestID:  "review-send-notes-label",
						Content: sendLabel,
						Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.Accent.OnFill},
					},
				},
			})
		}

		base := core.Style{
			Height:      core.Pt(44),
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         12,
			Padding:     core.Symmetric(0, 12),
			Background:  p.Bg,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: p.Line,
			FlexShrink:  0,
		}
		return core.View{
			Ref:       headerRef,
			TestID:    "review-header",
			Role:      core.RoleRegion,
			Label:     ed.T("reviewScreen.headerLabel"),
			Focusable: func() bool { return true },
			OnFocusIn: func() {
				if activeRegion.GetUntracked() != "" && activeRegion.GetUntracked() != reviewRegionHeader {
					activeRegion.Set("")
				}
			},
			StyleFunc: func() core.Style {
				return reviewRegionStyle(p, activeRegion, reviewRegionHeader, base)
			},
			Children: headerChildren,
		}
	})
}

func reviewSidebarView(cx *core.Scope, ed *Editor, reviewRev *core.Memo[uint64], activeRegion *core.Signal[string], listRef, filterInputRef *core.Ref) core.Node {
	// Keep filter input mounted outside dynamic rows so typing never loses focus.
	searchSig := core.NewSignal(cx, ed.review.search)
	// Keep composer draft in a dedicated signal synced ONLY when the composing change ID switches
	// so typing never triggers a list rebuild or loses focus.
	noteDraftSig := core.NewSignal(cx, ed.ReviewNoteDraft())
	lastComposerID := ed.ReviewNoteComposerChangeID()
	cx.Effect(func() {
		_ = reviewRev.Get()
		cur := ed.review.search
		if searchSig.GetUntracked() != cur {
			searchSig.Set(cur)
		}
		cid := ed.ReviewNoteComposerChangeID()
		if cid != lastComposerID {
			lastComposerID = cid
			noteDraftSig.Set(ed.ReviewNoteDraft())
		}
	})

	filterBar := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
		},
		Children: []core.Node{
			core.View{
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						FlexGrow:     1,
						FlexBasis:    core.Pt(0),
						MinWidth:     core.Pt(0),
						Height:       core.Pt(28),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          6,
						Padding:      core.Symmetric(0, 8),
						CornerRadius: 6,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					}
				},
				Children: []core.Node{
					core.Icon{
						SVG: lucide.Search(),
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: p.TextFaint, FlexShrink: 0}
						},
					},
					specInputField(cx, ed, ui.Input{
						Ref:         filterInputRef,
						TestID:      "review-filter-input",
						Label:       ed.T("reviewScreen.filterLabel"),
						Placeholder: ed.T("reviewScreen.filterPlaceholder"),
						Value:       searchSig.Get,
						OnChange: func(v string) {
							searchSig.Set(v)
							ed.SetReviewSearch(v)
						},
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{
								FlexGrow:     1,
								FlexBasis:    core.Pt(0),
								MinWidth:     core.Pt(0),
								Height:       core.Pt(26),
								BorderWidth:  0,
								Background:   core.Transparent,
								FontSize:     12,
								Color:        p.TextPrimary,
								FocusVisible: p.FocusRing(-2),
							}
						},
					}),
				},
			},
			core.Text{
				TestID: "review-seen-counter",
				ContentFunc: func() string {
					_ = reviewRev.Get()
					st := ed.ReviewStatus()
					return ed.T("reviewScreen.seenCounter", l10n.Param("seen", st.SeenCount), l10n.Param("total", st.TotalChanges))
				},
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						FontSize:   11.5,
						Color:      p.TextFaint,
						FlexShrink: 0,
					}
				},
			},
			core.View{
				TestID:    "review-prev-btn",
				Role:      core.RoleButton,
				Label:     ed.T("reviewScreen.prevChangeLabel"),
				Focusable: func() bool { return true },
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Width:          core.Pt(28),
						Height:         core.Pt(28),
						CornerRadius:   6,
						Background:     p.Raised,
						BorderWidth:    1,
						BorderColor:    p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
						FocusVisible:   p.FocusRing(2),
					}
				},
				OnPress: func(core.PressEvent) {
					ed.StepReviewChange(-1)
				},
				Children: []core.Node{
					core.Icon{
						SVG: lucide.ChevronUp(),
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.TextMuted}
						},
					},
				},
			},
			core.View{
				TestID:    "review-next-btn",
				Role:      core.RoleButton,
				Label:     ed.T("reviewScreen.nextChangeLabel"),
				Focusable: func() bool { return true },
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Width:          core.Pt(28),
						Height:         core.Pt(28),
						CornerRadius:   6,
						Background:     p.Raised,
						BorderWidth:    1,
						BorderColor:    p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
						FocusVisible:   p.FocusRing(2),
					}
				},
				OnPress: func(core.PressEvent) {
					ed.StepReviewChange(+1)
				},
				Children: []core.Node{
					core.Icon{
						SVG: lucide.ChevronDown(),
						StyleFunc: func() core.Style {
							p := ed.SpecPalette(cx)
							return core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.TextMuted}
						},
					},
				},
			},
		},
	}

	chipsAndBar := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		snap := ed.ReviewStatus()
		chipSpecs := []struct {
			id    ReviewFilter
			label string
		}{
			{ReviewFilterAll, ed.T("reviewScreen.chip.all")},
			{ReviewFilterAdded, ed.T("reviewScreen.chip.added")},
			{ReviewFilterChanged, ed.T("reviewScreen.chip.changed")},
			{ReviewFilterRemoved, ed.T("reviewScreen.chip.removed")},
			{ReviewFilterUnseen, ed.T("reviewScreen.chip.unseen")},
			{ReviewFilterNotes, ed.T("reviewScreen.chip.notes")},
		}
		chipNodes := make([]core.Node, 0, len(chipSpecs))
		for _, cs := range chipSpecs {
			f := cs.id
			cnt := ed.ReviewChipCount(f)
			active := snap.Filter == f
			bg := core.Transparent
			border := p.Line
			fg := p.TextMuted
			cntColor := p.TextFaint
			if active {
				bg = p.LineRaised
				border = p.LineRaised
				fg = p.TextPrimary
				cntColor = p.TextMuted
			}
			chipNodes = append(chipNodes, core.View{
				TestID:    "review-chip-" + string(f),
				Role:      core.RoleButton,
				Label:     ed.T("reviewScreen.chipLabel", l10n.Param("label", cs.label), l10n.Param("count", cnt)),
				Focusable: func() bool { return true },
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          4,
					Height:       core.Pt(24),
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 6,
					Background:   bg,
					BorderWidth:  1,
					BorderColor:  border,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.SetReviewFilter(f)
				},
				Children: []core.Node{
					core.Text{Content: cs.label, Style: core.Style{FontSize: 11.5, FontWeight: 500, Color: fg}},
					core.Text{Content: strconv.Itoa(cnt), Style: core.Style{FontSize: 11, Color: cntColor}},
				},
			})
		}

		pct := float32(0)
		if snap.TotalChanges > 0 {
			pct = float32(snap.SeenCount) / float32(snap.TotalChanges) * 100
		}

		return core.View{
			Style: core.Style{Gap: 8},
			Children: []core.Node{
				core.View{
					Role:  core.RoleGroup,
					Label: ed.T("reviewScreen.filterGroupLabel"),
					Style: core.Style{
						Direction: core.Row,
						Wrap:      core.Wrap,
						Gap:       4,
					},
					Children: chipNodes,
				},
				core.View{
					TestID: "review-seen-bar",
					Style: core.Style{
						Height:       core.Pt(3),
						CornerRadius: 2,
						Background:   p.Line,
						Overflow:     core.Hidden,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pct(pct),
								Height:       core.Pt(3),
								CornerRadius: 2,
								Background:   p.TextFaint,
							},
						},
					},
				},
			},
		}
	})

	roundBanner := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		snap := ed.ReviewStatus()
		if snap.Round <= 1 || snap.RoundBanner == "" {
			return nil
		}
		p := ed.SpecPalette(cx)
		return core.View{
			TestID: "review-round-banner",
			Role:   core.RoleStatus,
			Style: core.Style{
				Margin:       core.Edges{Top: 8, Left: 12, Right: 12},
				Padding:      core.Symmetric(8, 12),
				Direction:    core.Row,
				AlignItems:   core.AlignStart,
				Gap:          8,
				CornerRadius: 8,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				FlexShrink:   0,
			},
			Children: []core.Node{
				core.Icon{
					SVG:   lucide.RefreshCw(),
					Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.TextMuted, Margin: core.Edges{Top: 2}, FlexShrink: 0},
				},
				core.Text{
					TestID:  "review-round-banner-text",
					Content: snap.RoundBanner,
					Style: core.Style{
						FontSize:  12,
						Color:     p.TextMuted,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						MinWidth:  core.Pt(0),
					},
				},
			},
		}
	})

	rowsList := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		snap := ed.ReviewStatus()
		vis := ed.ReviewVisibleRows()

		if len(vis) == 0 {
			return core.View{
				TestID: "review-empty-rows",
				Style:  core.Style{Padding: core.Symmetric(24, 12), AlignItems: core.AlignCenter},
				Children: []core.Node{
					core.Text{
						Content: ed.T("reviewScreen.emptyRows"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
					},
				},
			}
		}

		nodes := make([]core.Node, 0, len(vis))
		for _, r := range vis {
			switch r.RowKind {
			case ReviewRowFile:
				nodes = append(nodes, reviewFileRowView(ed, p, r, snap.FocusedRowKey == r.Key, snap.ViewingReadOnlyRound))
			case ReviewRowKindHeader:
				nodes = append(nodes, reviewKindRowView(ed, p, r, snap.FocusedRowKey == r.Key, snap.ViewingReadOnlyRound))
			case ReviewRowCard:
				if ch := ed.review.changesByID[r.ChangeID]; ch != nil {
					nodes = append(nodes, reviewCardView(cx, ed, p, snap, ch, noteDraftSig, snap.FocusedID == ch.ID && snap.FocusedRowKey == r.Key, snap.FocusedID == ch.ID))
				}
			}
		}
		return core.View{Style: core.Style{Gap: 2}, Children: nodes}
	})

	footer := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		hint := func(key, text string) core.Node {
			return core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
				Children: []core.Node{
					core.Text{Content: key, Style: core.Style{FontFamily: core.FontMonospace, FontSize: 11, Color: p.TextSecondary}},
					core.Text{Content: text, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
				},
			}
		}
		return core.View{
			TestID: "review-shortcut-footer",
			Style: core.Style{
				Height:      core.Pt(36),
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         10,
				Padding:     core.Symmetric(0, 12),
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
				FlexShrink:  0,
			},
			Children: []core.Node{
				hint("J K", ed.T("reviewScreen.hint.move")),
				hint("C", ed.T("reviewScreen.hint.note")),
				hint("X", ed.T("reviewScreen.hint.revert")),
				hint("B", ed.T("reviewScreen.hint.before")),
				hint(ed.reviewShortcutHint(CmdReviewKeep, "mod+enter"), ed.T("reviewScreen.hint.keep")),
			},
		}
	})

	return core.View{
		Ref:       listRef,
		TestID:    "review-sidebar",
		Role:      core.RoleRegion,
		Label:     ed.T("reviewScreen.sidebarLabel"),
		Focusable: func() bool { return true },
		OnFocusIn: func() {
			if activeRegion.GetUntracked() != "" && activeRegion.GetUntracked() != reviewRegionList {
				activeRegion.Set("")
			}
		},
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return reviewRegionStyle(p, activeRegion, reviewRegionList, core.Style{
				Width:       core.Pt(372),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Right: 1},
				BorderColor: p.Line,
				MinHeight:   core.Pt(0),
			})
		},
		Children: []core.Node{
			core.View{
				StyleFunc: func() core.Style {
					p := ed.SpecPalette(cx)
					return core.Style{
						Padding:     core.Edges{Top: 12, Left: 12, Right: 12, Bottom: 8},
						Gap:         8,
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.Line,
						FlexShrink:  0,
					}
				},
				Children: []core.Node{filterBar, chipsAndBar},
			},
			roundBanner,
			ui.ScrollView{
				TestID: "review-changes-scroll",
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinHeight: core.Pt(0),
					Padding:   core.Edges{Top: 4, Left: 8, Right: 8, Bottom: 12},
				},
				Children: []core.Node{rowsList},
			},
			footer,
		},
	}
}

func reviewFileRowView(ed *Editor, p SpecPalette, r ReviewVisibleRow, rowFocused, readOnlyRound bool) core.Node {
	filePath := r.File
	chev := lucide.ChevronDown()
	if r.Collapsed {
		chev = lucide.ChevronRight()
	}
	fileIcon := lucide.FileText()
	switch r.FileKind {
	case ReviewFileScene:
		fileIcon = lucide.Image()
	case ReviewFilePrefab:
		fileIcon = lucide.Box()
	case ReviewFileData:
		fileIcon = lucide.Table()
	case ReviewFileGo:
		fileIcon = lucide.Code()
	}

	var countNodes []core.Node
	if r.Added > 0 {
		countNodes = append(countNodes, core.Text{
			Content: fmt.Sprintf("+%d", r.Added),
			Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11, Color: p.Added.Fg},
		})
	}
	if r.Changed > 0 {
		countNodes = append(countNodes, core.Text{
			Content: fmt.Sprintf("~%d", r.Changed),
			Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11, Color: p.Changed.Fg},
		})
	}
	if r.Removed > 0 {
		countNodes = append(countNodes, core.Text{
			Content: fmt.Sprintf("−%d", r.Removed),
			Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11, Color: p.Removed.Fg},
		})
	}

	bg := core.Transparent
	borderColor := core.Transparent
	borderW := core.Points(0)
	if rowFocused {
		bg = p.Raised
		borderColor = p.Accent.Edge
		borderW = 1
	}

	foldLabel := ed.T("reviewScreen.collapseFile", l10n.Param("file", filePath))
	if r.Collapsed {
		foldLabel = ed.T("reviewScreen.expandFile", l10n.Param("file", filePath))
	}
	rowChildren := []core.Node{
		core.View{
			TestID:    "review-file-fold-" + filePath,
			Role:      core.RoleButton,
			Label:     foldLabel,
			Focusable: func() bool { return true },
			Style: core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				MinWidth:     core.Pt(0),
				Height:       core.Pt(32),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Symmetric(0, 4),
				CornerRadius: 6,
				FocusVisible: p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.FocusReviewFileRow(filePath)
				ed.ToggleReviewFileCollapsed(filePath)
			},
			Children: []core.Node{
				core.Icon{SVG: chev, Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: p.TextFaint, FlexShrink: 0}},
				core.Icon{SVG: fileIcon, Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.TextMuted, FlexShrink: 0}},
				core.Text{
					Content:  r.FileName,
					MaxLines: 1,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextSecondary,
						FlexGrow:   1,
						FlexBasis:  core.Pt(0),
						MinWidth:   core.Pt(0),
						Overflow:   core.Hidden,
					},
				},
				core.View{
					Style:    core.Style{Direction: core.Row, Gap: 4, FlexShrink: 0},
					Children: countNodes,
				},
			},
		},
	}

	if r.CanRevertGroup && !readOnlyRound {
		toggleText := ed.T("reviewScreen.revertFile")
		toggleColor := p.TextMuted
		if r.AllReverted {
			toggleText = ed.T("reviewScreen.restoreFile")
			toggleColor = p.Accent.Fg
		}
		rowChildren = append(rowChildren, core.View{
			TestID:    "review-file-toggle-" + filePath,
			Role:      core.RoleButton,
			Label:     ed.T("reviewScreen.toggleFileLabel", l10n.Param("action", toggleText), l10n.Param("file", r.FileName)),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(24),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleReviewFileReverted(filePath)
			},
			Children: []core.Node{
				core.Text{Content: toggleText, Style: core.Style{FontSize: 11.5, Color: toggleColor}},
			},
		})
	}

	return core.View{
		TestID: "review-file-row-" + filePath,
		Style: core.Style{
			Height:       core.Pt(36),
			Margin:       core.Edges{Top: 4},
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          4,
			CornerRadius: 6,
			Background:   bg,
			Borders:      core.Edges{Top: 1, Left: borderW, Right: borderW, Bottom: borderW},
			BorderColor:  ternaryColor(rowFocused, borderColor, p.Line),
		},
		Children: rowChildren,
	}
}

func reviewChangeKindHeading(ed *Editor, kind ReviewChangeKind) string {
	switch kind {
	case ReviewChangeAdded:
		return ed.T("reviewScreen.kindHeading.added")
	case ReviewChangeChanged:
		return ed.T("reviewScreen.kindHeading.changed")
	case ReviewChangeRemoved:
		return ed.T("reviewScreen.kindHeading.removed")
	default:
		return strings.ToUpper(string(kind))
	}
}

func reviewChangeKindName(ed *Editor, kind ReviewChangeKind) string {
	switch kind {
	case ReviewChangeAdded:
		return ed.T("reviewScreen.kind.added")
	case ReviewChangeChanged:
		return ed.T("reviewScreen.kind.changed")
	case ReviewChangeRemoved:
		return ed.T("reviewScreen.kind.removed")
	default:
		return string(kind)
	}
}

func reviewKindRowView(ed *Editor, p SpecPalette, r ReviewVisibleRow, rowFocused, readOnlyRound bool) core.Node {
	filePath := r.File
	kind := r.Kind
	chev := lucide.ChevronDown()
	if r.Collapsed {
		chev = lucide.ChevronRight()
	}
	kindLabel := reviewChangeKindHeading(ed, kind)
	kindName := reviewChangeKindName(ed, kind)
	toggleText := ed.T("reviewScreen.revertCount", l10n.Param("count", r.Count))
	toggleColor := p.TextMuted
	if r.AllReverted {
		toggleText = ed.T("reviewScreen.restoreCount", l10n.Param("count", r.Count))
		toggleColor = p.Accent.Fg
	}

	bg := core.Transparent
	if rowFocused {
		bg = p.Raised
	}

	kindFoldLabel := ed.T("reviewScreen.collapseKindInFile", l10n.Param("kind", kindName), l10n.Param("file", filePath))
	if r.Collapsed {
		kindFoldLabel = ed.T("reviewScreen.expandKindInFile", l10n.Param("kind", kindName), l10n.Param("file", filePath))
	}
	kids := []core.Node{
		core.View{
			TestID:    fmt.Sprintf("review-kind-fold-%s-%s", filePath, kind),
			Role:      core.RoleButton,
			Label:     kindFoldLabel,
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:       core.Pt(24),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Symmetric(0, 4),
				CornerRadius: 6,
				FocusVisible: p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.FocusReviewKindRow(filePath, kind)
				ed.ToggleReviewKindCollapsed(filePath, kind)
			},
			Children: []core.Node{
				core.Icon{SVG: chev, Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: p.TextFaint}},
				core.Text{
					Content: kindLabel,
					Style: core.Style{
						FontSize:      11,
						FontWeight:    600,
						LetterSpacing: 0.6,
						Color:         p.TextFaint,
					},
				},
				core.Text{
					Content: strconv.Itoa(r.Count),
					Style:   core.Style{FontSize: 11, FontWeight: 500, Color: p.TextFaint},
				},
			},
		},
		core.View{Style: core.Style{FlexGrow: 1}},
	}
	if r.CanRevertGroup && !readOnlyRound {
		kids = append(kids, core.View{
			TestID:    fmt.Sprintf("review-kind-toggle-%s-%s", filePath, kind),
			Role:      core.RoleButton,
			Label:     ed.T("reviewScreen.toggleKindInFile", l10n.Param("action", toggleText), l10n.Param("kind", kindName), l10n.Param("file", filePath)),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(24),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleReviewKindReverted(filePath, kind)
			},
			Children: []core.Node{
				core.Text{Content: toggleText, Style: core.Style{FontSize: 11.5, Color: toggleColor}},
			},
		})
	}

	return core.View{
		TestID: fmt.Sprintf("review-kind-row-%s-%s", filePath, kind),
		Style: core.Style{
			Height:       core.Pt(28),
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          4,
			Padding:      core.Edges{Left: 16},
			CornerRadius: 6,
			Background:   bg,
		},
		Children: kids,
	}
}

func reviewCardView(cx *core.Scope, ed *Editor, p SpecPalette, snap ReviewSnapshot, ch *ReviewChange, noteDraftSig *core.Signal[string], rowFocused, changeFocused bool) core.Node {
	id := ch.ID
	ver := ch.VersionFor(snap.ViewRound)
	readOnly := snap.ViewingReadOnlyRound
	composing := !readOnly && snap.ComposingChangeID == id
	hasDraftNote := strings.TrimSpace(ch.Note) != ""
	role := reviewKindSpecRole(p, ch.Kind)
	cardBg := core.Transparent
	borderW := core.Points(0)
	borderCol := core.Transparent
	if changeFocused || rowFocused {
		cardBg = p.Raised
		borderW = 1
		borderCol = p.Accent.Edge
	}
	var opacity core.Opacity
	titleDeco := core.TextDecorationNone
	if ch.Reverted && !ch.GoRequiresConfirm {
		opacity = core.Alpha(0.55)
		titleDeco = core.TextDecorationLineThrough
	}

	// Seen indicator circle.
	seenStyle := core.Style{
		Width:          core.Pt(16),
		Height:         core.Pt(16),
		CornerRadius:   8,
		AlignItems:     core.AlignCenter,
		JustifyContent: core.JustifyCenter,
		FlexShrink:     0,
	}
	var seenKids []core.Node
	if ch.Seen {
		seenStyle.Background = p.LineRaised
		seenKids = []core.Node{
			core.Icon{SVG: lucide.Check(), Style: core.Style{Width: core.Pt(9), Height: core.Pt(9), Color: p.TextMuted}},
		}
	} else {
		seenStyle.BorderWidth = 1
		seenStyle.BorderColor = p.TextFaint
	}

	// Kind mark badge (+, pencil, −).
	kindIcon := lucide.Plus()
	switch ch.Kind {
	case ReviewChangeChanged:
		kindIcon = lucide.Pencil()
	case ReviewChangeRemoved:
		kindIcon = lucide.Minus()
	}

	toggleText := ed.T("reviewScreen.revert")
	toggleFg := p.TextSecondary
	if ch.Reverted {
		toggleText = ed.T("reviewScreen.restore")
		toggleFg = p.Accent.Fg
	}

	seenText := ed.T("reviewScreen.notSeen")
	if ch.Seen {
		seenText = ed.T("reviewScreen.seen")
	}
	rowKids := []core.Node{
		core.View{
			TestID:    "review-card-focus-" + id,
			Role:      core.RoleButton,
			Label:     ed.T("reviewScreen.cardFocusLabel", l10n.Param("title", ch.Title), l10n.Param("kind", reviewChangeKindName(ed, ch.Kind)), l10n.Param("detail", ver.Detail), l10n.Param("seen", seenText)),
			Focusable: func() bool { return true },
			Style: core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				MinWidth:     core.Pt(0),
				Height:       core.Pt(36),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				CornerRadius: 6,
				FocusVisible: p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.FocusReviewChange(id)
			},
			Children: []core.Node{
				core.View{
					TestID:   "review-card-seen-" + id,
					Style:    seenStyle,
					Children: seenKids,
				},
				core.View{
					Style: core.Style{
						Width:          core.Pt(18),
						Height:         core.Pt(18),
						CornerRadius:   6,
						Background:     role.Fill,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{
						core.Icon{SVG: kindIcon, Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: role.Fg}},
					},
				},
				core.Text{
					Content:  ch.Title,
					MaxLines: 1,
					Style: core.Style{
						FontSize:       12.5,
						FontWeight:     500,
						Color:          p.TextPrimary,
						MaxWidth:       core.Pt(120),
						TextDecoration: titleDeco,
						Overflow:       core.Hidden,
						FlexShrink:     0,
					},
				},
				core.Text{
					Content:  ver.Detail,
					MaxLines: 1,
					Style: core.Style{
						FontSize:  11.5,
						Color:     p.TextFaint,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						MinWidth:  core.Pt(0),
						Overflow:  core.Hidden,
					},
				},
			},
		},
	}

	if !readOnly {
		noteBtnBg := core.Transparent
		noteBtnFg := p.TextFaint
		if hasDraftNote || composing {
			noteBtnBg = p.LineRaised
			noteBtnFg = p.TextPrimary
		}
		noteBtnLabel := ed.T("reviewScreen.noteBtnAdd", l10n.Param("title", ch.Title))
		if hasDraftNote {
			noteBtnLabel = ed.T("reviewScreen.noteBtnEdit", l10n.Param("title", ch.Title))
		}
		rowKids = append(rowKids, core.View{
			TestID:    "review-card-note-btn-" + id,
			Role:      core.RoleButton,
			Label:     noteBtnLabel,
			Focusable: func() bool { return true },
			Style: core.Style{
				Width:          core.Pt(26),
				Height:         core.Pt(26),
				CornerRadius:   6,
				Background:     noteBtnBg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				if composing {
					ed.CancelReviewNoteComposer()
				} else {
					ed.report(ed.OpenReviewNoteComposer(id))
				}
			},
			Children: []core.Node{
				core.Icon{SVG: lucide.MessageSquare(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: noteBtnFg}},
			},
		})
	}

	if !ch.GoRequiresConfirm && !readOnly {
		rowKids = append(rowKids, core.View{
			TestID:    "review-card-toggle-" + id,
			Role:      core.RoleButton,
			Label:     ed.T("reviewScreen.cardToggleLabel", l10n.Param("action", toggleText), l10n.Param("title", ch.Title)),
			Focusable: func() bool { return true },
			Style: core.Style{
				Height:         core.Pt(26),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				Background:     p.Raised,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FlexShrink:     0,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleReviewChangeReverted(id)
			},
			Children: []core.Node{
				core.Text{Content: toggleText, Style: core.Style{FontSize: 11.5, Color: toggleFg}},
			},
		})
	}

	cardChildren := []core.Node{
		core.View{
			Style: core.Style{
				Height:     core.Pt(36),
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
				Padding:    core.Edges{Left: 8, Right: 4},
			},
			Children: rowKids,
		},
	}

	// Saved unsent note box (with Edit & Remove when viewing the latest round).
	if hasDraftNote && !composing {
		noteHeaderKids := []core.Node{
			core.Text{
				Content: ed.T("reviewScreen.noteNotSent"),
				Style:   core.Style{FontSize: 11.5, Color: p.TextFaint, FlexGrow: 1},
			},
		}
		if !readOnly {
			noteHeaderKids = append(noteHeaderKids,
				core.View{
					TestID:    "review-card-note-edit-" + id,
					Role:      core.RoleButton,
					Label:     ed.T("reviewScreen.editNoteLabel", l10n.Param("title", ch.Title)),
					Focusable: func() bool { return true },
					Style: core.Style{
						Height:         core.Pt(22),
						Padding:        core.Symmetric(0, 4),
						CornerRadius:   4,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.OpenReviewNoteComposer(id))
					},
					Children: []core.Node{
						core.Text{Content: ed.T("reviewScreen.edit"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
					},
				},
				core.View{
					TestID:    "review-card-note-remove-" + id,
					Role:      core.RoleButton,
					Label:     ed.T("reviewScreen.removeNoteLabel", l10n.Param("title", ch.Title)),
					Focusable: func() bool { return true },
					Style: core.Style{
						Height:         core.Pt(22),
						Padding:        core.Symmetric(0, 4),
						CornerRadius:   4,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.RemoveReviewNote(id))
					},
					Children: []core.Node{
						core.Text{Content: ed.T("reviewScreen.remove"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
					},
				},
			)
		}
		cardChildren = append(cardChildren, core.View{
			TestID: "review-card-note-" + id,
			Style: core.Style{
				Margin:       core.Edges{Left: 42, Right: 8, Bottom: 8},
				Padding:      core.Symmetric(8, 12),
				Gap:          4,
				CornerRadius: 8,
				Background:   p.Bg,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.View{
					Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: noteHeaderKids,
				},
				core.Text{
					TestID:  "review-card-note-text-" + id,
					Content: ch.Note,
					Style:   core.Style{FontSize: 12, Color: p.TextPrimary},
				},
			},
		})
	}

	// Inline note composer ("Note for the agent · not sent").
	if composing {
		cardChildren = append(cardChildren, core.View{
			TestID: "review-note-composer-" + id,
			Style: core.Style{
				Margin: core.Edges{Left: 42, Right: 8, Bottom: 8},
				Gap:    8,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("reviewScreen.noteNotSent"),
					Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
				},
				specInputField(cx, ed, ui.Input{
					TestID:      "review-note-input",
					Label:       ed.T("reviewScreen.noteInputLabel", l10n.Param("title", ch.Title)),
					Placeholder: ed.T("reviewScreen.notePlaceholder"),
					Multiline:   true,
					Value:       noteDraftSig.Get,
					OnChange: func(v string) {
						noteDraftSig.Set(v)
						ed.SetReviewNoteDraft(v)
					},
					Style: core.Style{
						MinHeight:    core.Pt(52),
						Padding:      core.Symmetric(8, 10),
						CornerRadius: 8,
						Background:   p.Bg,
						BorderWidth:  1.5,
						BorderColor:  p.Accent.Fg,
						FontSize:     12,
						Color:        p.TextPrimary,
						FocusVisible: p.FocusRing(2),
					},
				}),
				core.View{
					Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6, AlignItems: core.AlignCenter},
					Children: []core.Node{
						core.View{
							TestID:    "review-note-add",
							Role:      core.RoleButton,
							Label:     ed.T("reviewScreen.addNote"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(28),
								Padding:        core.Symmetric(0, 12),
								CornerRadius:   8,
								Background:     p.Accent.Fg,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.report(ed.CommitReviewNote())
							},
							Children: []core.Node{
								core.Text{Content: ed.T("reviewScreen.addNote"), Style: core.Style{FontSize: 12, FontWeight: 600, Color: p.Accent.OnFill}},
							},
						},
						core.View{
							TestID:    "review-note-revert-redo",
							Role:      core.RoleButton,
							Label:     ed.T("reviewScreen.revertAndRedo"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(28),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   8,
								Background:     p.Raised,
								BorderWidth:    1,
								BorderColor:    p.LineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								if _, err := ed.RevertAndRedoReviewChange(id, noteDraftSig.GetUntracked()); err != nil {
									ed.report(err)
								}
							},
							Children: []core.Node{
								core.Text{Content: ed.T("reviewScreen.revertAndRedo"), Style: core.Style{FontSize: 11.5, Color: p.TextSecondary}},
							},
						},
						core.View{
							TestID:    "review-note-cancel",
							Role:      core.RoleButton,
							Label:     ed.T("reviewScreen.cancel"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:         core.Pt(28),
								Padding:        core.Symmetric(0, 8),
								CornerRadius:   8,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.CancelReviewNoteComposer()
							},
							Children: []core.Node{
								core.Text{Content: ed.T("reviewScreen.cancel"), Style: core.Style{FontSize: 11.5, Color: p.TextMuted}},
							},
						},
					},
				},
			},
		})
	}

	// Sent note awaiting agent's redo.
	if !readOnly && strings.TrimSpace(ch.SentNote) != "" {
		cardChildren = append(cardChildren, core.View{
			TestID: "review-card-sent-note-" + id,
			Style: core.Style{
				Margin:       core.Edges{Left: 42, Right: 8, Bottom: 8},
				Padding:      core.Symmetric(6, 10),
				Gap:          2,
				CornerRadius: 8,
				Background:   p.Bg,
				BorderWidth:  1,
				BorderColor:  p.Accent.Edge,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("reviewScreen.sentForRound", l10n.Param("round", snap.Round+1)),
					Style:   core.Style{FontSize: 11, Color: p.Accent.Fg},
				},
				core.Text{
					Content: ch.SentNote,
					Style:   core.Style{FontSize: 11.5, Color: p.TextSecondary},
				},
			},
		})
	}

	// Revised in Round 2+ annotation (shown on the latest round, hidden on read-only Round 1).
	if !readOnly && ch.RedoneInRound > 1 && strings.TrimSpace(ch.RedoneNote) != "" {
		cardChildren = append(cardChildren, core.View{
			TestID: "review-card-revised-" + id,
			Style: core.Style{
				Margin:     core.Edges{Left: 42, Right: 8, Bottom: 8},
				Direction:  core.Row,
				AlignItems: core.AlignStart,
				Gap:        6,
			},
			Children: []core.Node{
				core.Icon{
					SVG:   lucide.RefreshCw(),
					Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: p.TextMuted, Margin: core.Edges{Top: 2}, FlexShrink: 0},
				},
				core.Text{
					Content: ed.T("reviewScreen.revisedInRound", l10n.Param("round", ch.RedoneInRound), l10n.Param("note", ch.RedoneNote)),
					Style: core.Style{
						FontSize:  11.5,
						Color:     p.TextMuted,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						MinWidth:  core.Pt(0),
					},
				},
			},
		})
	}

	if !readOnly && len(ch.Conflicts) > 0 && !ch.Reverted {
		cardChildren = append(cardChildren, reviewConflictCardView(ed, ch, p))
	}

	if !readOnly && ch.YAMLError != nil && !ch.Reverted {
		cardChildren = append(cardChildren, core.View{
			TestID: "review-card-yaml-error-" + ch.ObjectID,
			Style: core.Style{
				Margin:       core.Edges{Left: 42, Right: 8, Bottom: 8},
				Padding:      core.Symmetric(6, 10),
				CornerRadius: 8,
				Background:   p.Removed.Fill,
				BorderWidth:  1,
				BorderColor:  p.Removed.Edge,
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
			},
			Children: []core.Node{
				core.Icon{SVG: lucide.TriangleAlert(), Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: p.Removed.Fg}},
				core.Text{Content: ch.YAMLError.Title, Style: core.Style{FontSize: 11.5, FontWeight: 500, Color: p.Removed.Fg}},
			},
		})
	} else if !readOnly && ch.HandEdited && !ch.Reverted {
		cardChildren = append(cardChildren, core.View{
			TestID: "review-card-hand-edited-" + ch.ObjectID,
			Style: core.Style{
				Margin:       core.Edges{Left: 42, Right: 8, Bottom: 8},
				Padding:      core.Symmetric(4, 8),
				CornerRadius: 6,
				Background:   p.Bg,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
			},
			Children: []core.Node{
				core.Icon{SVG: lucide.Pencil(), Style: core.Style{Width: core.Pt(11), Height: core.Pt(11), Color: p.TextMuted}},
				core.Text{Content: ed.T("reviewScreen.editedInReview"), Style: core.Style{FontSize: 11, Color: p.TextMuted}},
			},
		})
	}

	if ch.Blocker != "" && !ch.Reverted {
		cardChildren = append(cardChildren, core.View{
			Style: core.Style{
				Margin:       core.Edges{Left: 42, Right: 8, Bottom: 8},
				Padding:      core.Symmetric(6, 10),
				CornerRadius: 8,
				Background:   p.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  p.Warning.Edge,
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
			},
			Children: []core.Node{
				core.Icon{SVG: lucide.TriangleAlert(), Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: p.Warning.Fg}},
				core.Text{Content: ch.Blocker, Style: core.Style{FontSize: 11.5, FontWeight: 500, Color: p.Warning.Fg}},
			},
		})
	}

	return core.View{
		TestID: "review-card-" + id,
		Style: core.Style{
			Margin:       core.Edges{Top: 2, Bottom: 2, Left: 8},
			CornerRadius: 8,
			Background:   cardBg,
			BorderWidth:  borderW,
			BorderColor:  borderCol,
			Opacity:      opacity,
		},
		Children: cardChildren,
	}
}

func reviewConflictCardView(ed *Editor, ch *ReviewChange, p SpecPalette) core.Node {
	resolved := ch.ConflictChoice != ReviewConflictUnresolved
	changeID := ch.ID
	objID := ch.ObjectID
	if objID == "" {
		objID = ch.Title
	}

	boxBg := p.Warning.Fill
	boxBorder := p.Warning.Edge
	headFg := p.Warning.Fg
	headIcon := lucide.TriangleAlert()
	headIconColor := p.Warning.Fg
	headTitle := ed.T("reviewScreen.conflictTitle", l10n.Param("title", ch.Title))
	segBorder := p.Warning.Edge

	if resolved {
		boxBg = p.Bg
		boxBorder = p.LineRaised
		headFg = p.TextPrimary
		headIcon = lucide.Check()
		headIconColor = p.Added.Fg
		segBorder = p.LineRaised
		switch ch.ConflictChoice {
		case ReviewConflictBoth:
			headTitle = ed.T("reviewScreen.conflictMergedBoth")
		case ReviewConflictMine:
			headTitle = ed.T("reviewScreen.conflictMergedMine")
		case ReviewConflictAgent:
			headTitle = ed.T("reviewScreen.conflictMergedAgent")
		}
	}

	var diffLines []core.Node
	for _, c := range ch.Conflicts {
		for _, ml := range c.MineLines {
			diffLines = append(diffLines, reviewConflictLineRow(string(ml.Author), ml.Sign+" "+ml.Text, p))
		}
		for _, al := range c.AgentLines {
			diffLines = append(diffLines, reviewConflictLineRow(string(al.Author), al.Sign+" "+al.Text, p))
		}
	}

	segBtn := func(testID, label string, choice ReviewConflictChoice) core.Node {
		selected := ch.ConflictChoice == choice
		bg := core.Transparent
		fg := p.TextMuted
		if selected {
			bg = p.LineRaised
			fg = p.TextPrimary
		}
		return core.View{
			TestID:    testID,
			Role:      core.RoleButton,
			Label:     ed.T("reviewScreen.conflictChoiceLabel", l10n.Param("choice", label), l10n.Param("title", ch.Title)),
			Focusable: func() bool { return true },
			Style: core.Style{
				FlexGrow:       1,
				FlexBasis:      core.Pt(0),
				Height:         core.Pt(26),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				Background:     bg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.ResolveReviewConflict(changeID, choice))
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

	return core.View{
		TestID: "review-conflict-card-" + objID,
		Role:   core.RoleGroup,
		Label:  ed.T("reviewScreen.conflictGroupLabel", l10n.Param("title", ch.Title)),
		Style: core.Style{
			Margin:       core.Edges{Right: 8, Bottom: 8, Left: 42},
			Padding:      core.All(12),
			CornerRadius: 8,
			Background:   boxBg,
			BorderWidth:  1,
			BorderColor:  boxBorder,
			Gap:          8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.Icon{
						SVG:   headIcon,
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: headIconColor, FlexShrink: 0},
					},
					core.Text{
						TestID:  "review-conflict-title-" + objID,
						Content: headTitle,
						Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: headFg},
					},
				},
			},
			core.View{
				Style:    core.Style{Gap: 3},
				Children: diffLines,
			},
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					Gap:          2,
					Padding:      core.All(2),
					CornerRadius: 8,
					Background:   p.Bg,
					BorderWidth:  1,
					BorderColor:  segBorder,
				},
				Children: []core.Node{
					segBtn("review-conflict-mine-"+objID, ed.T("reviewScreen.conflictMine"), ReviewConflictMine),
					segBtn("review-conflict-agent-"+objID, ed.T("reviewScreen.conflictAgent"), ReviewConflictAgent),
					segBtn("review-conflict-both-"+objID, ed.T("reviewScreen.conflictBoth"), ReviewConflictBoth),
				},
			},
		},
	}
}

func reviewConflictLineRow(author, text string, p SpecPalette) core.Node {
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter},
		Children: []core.Node{
			core.Text{
				Content: author,
				Style: core.Style{
					Width:      core.Pt(48),
					FlexShrink: 0,
					FontSize:   11.5,
					FontFamily: core.FontMonospace,
					Color:      p.TextFaint,
				},
			},
			core.Text{
				Content: text,
				Style: core.Style{
					FlexGrow:   1,
					FlexBasis:  core.Pt(0),
					MinWidth:   core.Pt(0),
					FontSize:   11.5,
					FontFamily: core.FontMonospace,
					Color:      p.TextSecondary,
				},
			},
		},
	}
}

func reviewKindSpecRole(p SpecPalette, kind ReviewChangeKind) SpecRole {
	switch kind {
	case ReviewChangeAdded:
		return p.Added
	case ReviewChangeChanged:
		return p.Changed
	default:
		return p.Removed
	}
}

// --- Center pane (Scene viewport / Prefab impact card / Small data table / Go diff) ---

func reviewCenterView(cx *core.Scope, ed *Editor, reviewRev *core.Memo[uint64], activeRegion *core.Signal[string], centerRef *core.Ref) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		ch := ed.review.changesByID[ed.review.focusedID]

		var content core.Node
		if ch == nil {
			content = core.View{
				Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
				Children: []core.Node{
					core.Text{Content: ed.T("reviewScreen.selectToInspect"), Style: core.Style{FontSize: 13, Color: p.TextFaint}},
				},
			}
		} else {
			switch ch.FileKind {
			case ReviewFileScene:
				content = reviewSceneCenterView(ed, p, ch)
			case ReviewFilePrefab:
				content = reviewPrefabCenterView(ed, p, ch)
			case ReviewFileData:
				content = reviewDataTableCenterView(ed, p, ch)
			default:
				content = reviewGenericFileCenterView(ed, p, ch)
			}
		}

		base := core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			MinWidth:   core.Pt(0),
			MinHeight:  core.Pt(0),
			Background: p.Viewport,
			Overflow:   core.Hidden,
		}
		return core.View{
			Ref:       centerRef,
			TestID:    "review-center",
			Role:      core.RoleRegion,
			Label:     ed.T("reviewScreen.centerLabel"),
			Focusable: func() bool { return true },
			OnFocusIn: func() {
				if activeRegion.GetUntracked() != "" && activeRegion.GetUntracked() != reviewRegionCenter {
					activeRegion.Set("")
				}
			},
			StyleFunc: func() core.Style {
				return reviewRegionStyle(p, activeRegion, reviewRegionCenter, base)
			},
			Children: []core.Node{content},
		}
	})
}

func reviewSceneCenterView(ed *Editor, p SpecPalette, focused *ReviewChange) core.Node {
	fs := ed.review.filesByPath[focused.File]
	peek := ed.ReviewPeekingBefore()
	overlays := ed.reviewOverlaysForFile(focused.File)
	pins := ed.reviewViewportPinsForFile(focused.File)
	readOnlyRound := ed.ReviewViewingReadOnlyRound()
	viewRound := ed.ReviewViewRound()

	// Count total changes in this scene for the top-right badge.
	sceneChangeCount := 0
	for _, ch := range ed.review.changes {
		if ch.File == focused.File {
			sceneChangeCount++
		}
	}

	legendToggle := func(id, label string, col core.Color, dashed, on bool) core.Node {
		var op core.Opacity
		if !on {
			op = core.Alpha(0.45)
		}
		return core.View{
			TestID:    "review-legend-" + id,
			Role:      core.RoleSwitch,
			Label:     ed.T("reviewScreen.toggleOverlayLabel", l10n.Param("label", label)),
			Focusable: func() bool { return true },
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Height:       core.Pt(24),
				Padding:      core.Symmetric(0, 6),
				CornerRadius: 6,
				Opacity:      op,
				FocusVisible: p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleReviewOverlayLegend(id)
			},
			Children: []core.Node{
				core.Canvas{
					Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), FlexShrink: 0},
					Draw: func(d *core.Drawing, _ core.Size) {
						d.Save()
						d.SetStrokeColor(col).SetLineWidth(1.5)
						if dashed {
							d.SetLineDash(3, 2)
						}
						d.StrokeRect(1, 1, 10, 10)
						d.Restore()
					},
				},
				core.Text{
					Content: label,
					Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
				},
			},
		}
	}

	peekBg := p.PillBg
	peekFg := p.TextMuted
	peekBorder := p.LineRaised
	if peek {
		peekBg = p.LineRaised
		peekFg = p.TextPrimary
		peekBorder = p.Accent.Edge
	}

	var focusedOverlay *ReviewSceneOverlay
	for i := range overlays {
		if overlays[i].Focused {
			focusedOverlay = &overlays[i]
			break
		}
	}

	children := []core.Node{
		core.Canvas{
			TestID: "review-scene-canvas",
			Style: core.Style{
				Width:  core.Pct(100),
				Height: core.Pct(100),
			},
			Draw: func(d *core.Drawing, sz core.Size) {
				drawReviewSceneCanvas(ed, d, sz, p, fs, peek, overlays, pins)
			},
		},
		// Top-left overlay legend toggles (per Spec.dc.html) + top-right scene label & Before peek button.
		core.View{
			PointerEvents: core.PointerEventsBoxNone,
			Style: core.Style{
				Position:       core.Absolute,
				Top:            core.Pt(12),
				Left:           core.Pt(16),
				Right:          core.Pt(16),
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
			},
			Children: []core.Node{
				core.View{
					TestID: "review-overlay-legend",
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          4,
						Padding:      core.Symmetric(2, 6),
						CornerRadius: 10,
						Background:   p.PillBg,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						legendToggle("added", ed.T("reviewScreen.legend.added"), p.Added.Edge, true, ed.review.legend.Added),
						legendToggle("changed", ed.T("reviewScreen.legend.changed"), p.Changed.Edge, true, ed.review.legend.Changed),
						legendToggle("removed", ed.T("reviewScreen.legend.removed"), p.Removed.Edge, false, ed.review.legend.Removed),
						legendToggle("selected", ed.T("reviewScreen.legend.selected"), p.Accent.Edge, false, ed.review.legend.Selected),
					},
				},
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						core.View{
							TestID: "review-scene-pill",
							Style: core.Style{
								Height:         core.Pt(24),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   12,
								Background:     p.PillBg,
								BorderWidth:    1,
								BorderColor:    p.LineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: func() string {
										s := ed.T("reviewScreen.scenePillMany", l10n.Param("file", path.Base(focused.File)), l10n.Param("count", sceneChangeCount))
										if sceneChangeCount == 1 {
											s = ed.T("reviewScreen.scenePillOne", l10n.Param("file", path.Base(focused.File)))
										}
										if readOnlyRound {
											s = ed.T("reviewScreen.scenePillReadOnly", l10n.Param("base", s), l10n.Param("round", viewRound))
										}
										return s
									}(),
									Style: core.Style{FontSize: 11.5, Color: p.TextMuted},
								},
							},
						},
						core.View{
							TestID:    "review-peek-before",
							Role:      core.RoleButton,
							Label:     ed.T("reviewScreen.peekBeforeLabel"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Height:       core.Pt(28),
								Padding:      core.Symmetric(0, 8),
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          6,
								CornerRadius: 6,
								Background:   peekBg,
								BorderWidth:  1,
								BorderColor:  peekBorder,
								FocusVisible: p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.ToggleReviewPeekBefore()
							},
							Children: []core.Node{
								core.Icon{SVG: lucide.Eye(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: peekFg}},
								core.Text{Content: ed.T("reviewScreen.before"), Style: core.Style{FontSize: 11.5, Color: peekFg}},
								specReviewKeycap("B", p),
							},
						},
					},
				},
			},
		},
	}

	if peek {
		children = append(children, core.View{
			TestID:        "review-peek-banner",
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:       core.Absolute,
				Top:            core.Pt(52),
				Left:           core.Pt(0),
				Right:          core.Pt(0),
				Direction:      core.Row,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Height:         core.Pt(26),
						Padding:        core.Symmetric(0, 12),
						CornerRadius:   13,
						Background:     p.PillBg,
						BorderWidth:    1,
						BorderColor:    p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Text{Content: ed.T("reviewScreen.beforeThisRequest"), Style: core.Style{FontSize: 12, FontWeight: 500, Color: p.TextPrimary}},
					},
				},
			},
		})
	} else if focusedOverlay != nil {
		// Outlined focused change pill in its OWN semantic kind color (never accent!).
		role := reviewKindSpecRole(p, focusedOverlay.Kind)
		var pillKids []core.Node
		if focusedOverlay.Kind == ReviewChangeRemoved {
			pillKids = []core.Node{
				core.Text{
					Content: focused.Title,
					Style: core.Style{
						FontFamily:     core.FontMonospace,
						FontSize:       11,
						Color:          role.Fg,
						TextDecoration: core.TextDecorationLineThrough,
					},
				},
				core.Text{
					Content: ed.T("reviewScreen.kind.removed"),
					Style:   core.Style{FontSize: 11, Color: role.Fg},
				},
			}
		} else {
			pillKids = []core.Node{
				core.Text{
					Content: focusedOverlay.PillText,
					Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11, FontWeight: 500, Color: role.Fg},
				},
			}
		}
		children = append(children, core.View{
			TestID:        "review-focused-pill",
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:       core.Absolute,
				Bottom:         core.Pt(16),
				Left:           core.Pt(0),
				Right:          core.Pt(0),
				Direction:      core.Row,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Height:         core.Pt(24),
						Padding:        core.Symmetric(0, 10),
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						Gap:            6,
						CornerRadius:   12,
						Background:     p.PillBg,
						BorderWidth:    1,
						BorderColor:    role.Edge,
						JustifyContent: core.JustifyCenter,
					},
					Children: pillKids,
				},
			},
		})
	}

	return core.View{
		TestID: "review-center-scene",
		Style: core.Style{
			Position: core.Relative,
			FlexGrow: 1,
			Width:    core.Pct(100),
			Height:   core.Pct(100),
			Overflow: core.Hidden,
		},
		Children: children,
	}
}

func drawReviewSceneCanvas(ed *Editor, d *core.Drawing, sz core.Size, p SpecPalette, fs *reviewFileState, peek bool, overlays []ReviewSceneOverlay, pins []ReviewViewportPin) {
	w, h := float32(sz.Width), float32(sz.Height)
	if w <= 0 || h <= 0 {
		return
	}
	// Spec viewport dot backdrop.
	d.SetFillColor(p.Viewport).FillRect(0, 0, w, h)
	d.SetFillColor(p.ViewportDot)
	for y := float32(8); y < h; y += 16 {
		for x := float32(8); x < w; x += 16 {
			d.FillRect(x, y, 1.2, 1.2)
		}
	}
	if fs == nil {
		return
	}
	fs.canvasW, fs.canvasH = w, h

	built := ed.reviewSceneBuilt(fs, peek)
	v := fs.sceneView
	if !fs.sceneFramed || v.Zoom <= 0 {
		vw, vh := float32(640), float32(360)
		if built != nil && built.Doc != nil && built.Doc.VirtualWidth > 0 && built.Doc.VirtualHeight > 0 {
			vw, vh = built.Doc.VirtualWidth, built.Doc.VirtualHeight
		}
		zoom := min((w-48)/max(vw, 1), (h-96)/max(vh, 1))
		zoom = clampView2DZoom(max(zoom, 0.25))
		v = View2D{X: vw / 2, Y: vh / 2, Zoom: zoom}
	}

	viewAff := math2d.Translate(w/2, h/2).
		Mul(math2d.Scale(v.Zoom, v.Zoom)).
		Mul(math2d.Translate(-v.X, -v.Y))

	if built != nil {
		vw, vh := float32(640), float32(360)
		if built.Doc != nil && built.Doc.VirtualWidth > 0 && built.Doc.VirtualHeight > 0 {
			vw, vh = built.Doc.VirtualWidth, built.Doc.VirtualHeight
		}
		p0 := viewAff.TransformPoint(math2d.Vec2{X: 0, Y: 0})
		p1 := viewAff.TransformPoint(math2d.Vec2{X: vw, Y: vh})
		bgCol := p.Inset
		if built.HasBackground {
			bgCol = built.BackgroundColor
		}
		d.Save()
		d.SetFillColor(bgCol).FillRect(p0.X, p0.Y, p1.X-p0.X, p1.Y-p0.Y)
		d.SetStrokeColor(p.LineRaised).SetLineWidth(1).StrokeRect(p0.X, p0.Y, p1.X-p0.X, p1.Y-p0.Y)
		applyAffine(d, viewAff)
		for _, layer := range built.Layers {
			if !layer.Visible {
				continue
			}
			drawBuiltLayer(d, layer)
			// Draw subtle Spec marker blocks for non-sprite objects so objects without textures stay clearly visible.
			for _, bo := range layer.Objects {
				drawReviewObjectFallbackMarker(d, bo, p)
			}
		}
		d.Restore()
	}

	if peek {
		return
	}

	// Draw change overlays (unfocused first at 1.5 px, focused last on top at 2.5 px in its OWN kind color — never accent).
	drawOne := func(ov ReviewSceneOverlay) {
		role := reviewKindSpecRole(p, ov.Kind)
		p0 := viewAff.TransformPoint(math2d.Vec2{X: ov.X, Y: ov.Y})
		p1 := viewAff.TransformPoint(math2d.Vec2{X: ov.X + ov.W, Y: ov.Y + ov.H})
		rx := min(p0.X, p1.X) - 3
		ry := min(p0.Y, p1.Y) - 3
		rw := float32(math.Abs(float64(p1.X-p0.X))) + 6
		rh := float32(math.Abs(float64(p1.Y-p0.Y))) + 6

		d.Save()
		if ov.Kind == ReviewChangeRemoved {
			// Removed: grey ghost at 25% inside solid red outline.
			d.SetFillColor(ui.Alpha(p.TextFaint, 0.25))
			d.BeginPath().RoundRect(rx+3, ry+3, max(rw-6, 4), max(rh-6, 4), 4).Fill()
		} else {
			alpha := float32(0.16)
			if ov.Focused {
				alpha = 0.26
			}
			d.SetFillColor(ui.Alpha(role.Fg, alpha))
			d.BeginPath().RoundRect(rx+2, ry+2, max(rw-4, 4), max(rh-4, 4), 4).Fill()
		}
		d.SetStrokeColor(role.Edge).SetLineWidth(ov.StrokeWidth)
		if ov.Dashed {
			d.SetLineDash(5, 3)
		} else {
			d.SetLineDash()
		}
		d.BeginPath().RoundRect(rx, ry, rw, rh, 6).Stroke()
		d.Restore()
	}
	for _, ov := range overlays {
		if !ov.Focused {
			drawOne(ov)
		}
	}
	for _, ov := range overlays {
		if ov.Focused {
			drawOne(ov)
		}
	}

	// Draw Spec-token note pins beside noted objects (per Review.dc.html).
	for _, pin := range pins {
		pt := viewAff.TransformPoint(math2d.Vec2{X: pin.X, Y: pin.Y})
		drawReviewViewportNotePin(d, pt.X+4, pt.Y-20, p, pin.Redone)
	}
}

// drawReviewViewportNotePin draws a crisp 20×20 Spec speech-bubble pin beside a noted object
// in both Spec Dark and Spec Light themes.
func drawReviewViewportNotePin(d *core.Drawing, x, y float32, p SpecPalette, redone bool) {
	if d == nil {
		return
	}
	fill := p.TextPrimary
	ink := p.Bg
	edge := p.LineRaised
	if redone {
		fill = p.Accent.Fg
		ink = p.Accent.OnFill
		edge = p.Accent.Edge
	}
	d.Save()
	d.SetFillColor(fill)
	d.BeginPath().RoundRect(x, y, 20, 20, 8).Fill()
	d.SetStrokeColor(edge).SetLineWidth(1)
	d.BeginPath().RoundRect(x, y, 20, 20, 8).Stroke()
	// Speech-bubble glyph inside the 20×20 pin.
	d.SetStrokeColor(ink).SetLineWidth(1.6)
	d.BeginPath().
		MoveTo(x+5.5, y+6).
		LineTo(x+14.5, y+6).
		LineTo(x+14.5, y+12.5).
		LineTo(x+9.2, y+12.5).
		LineTo(x+6.2, y+15).
		LineTo(x+6.2, y+12.5).
		LineTo(x+5.5, y+12.5).
		ClosePath().
		Stroke()
	d.Restore()
}

func drawReviewObjectFallbackMarker(d *core.Drawing, bo *sceneformat.BuiltObject, p SpecPalette) {
	if bo == nil || bo.Doc == nil {
		return
	}
	if bo.Sprite == nil || bo.Sprite.Frame.Width <= 0 || bo.Sprite.Frame.Height <= 0 {
		rx, ry, rw, rh := objectOutline(bo)
		p0 := bo.World.TransformPoint(math2d.Vec2{X: rx, Y: ry})
		fill := p.TextMuted
		if bo.Doc.Prefab != "" || bo.Doc.Scene != "" {
			fill = p.Added.Fg
		}
		d.Save()
		d.SetFillColor(ui.Alpha(fill, 0.55))
		d.BeginPath().RoundRect(p0.X, p0.Y, rw, rh, 4).Fill()
		d.Restore()
	}
	for _, child := range bo.Children {
		drawReviewObjectFallbackMarker(d, child, p)
	}
}

func reviewPrefabCenterView(ed *Editor, p SpecPalette, ch *ReviewChange) core.Node {
	role := reviewKindSpecRole(p, ch.Kind)
	oldText := ch.OldValue
	newText := ch.NewValue
	if oldText == "" {
		oldText = ed.T("reviewScreen.valueNone")
	}
	if newText == "" {
		newText = ed.T("reviewScreen.valueRemoved")
	}
	if ch.Reverted {
		newText = oldText
	}

	diffChildren := []core.Node{
		core.Text{
			Content: ch.Title,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				Color:      p.TextMuted,
				Width:      core.Pt(130),
				FlexShrink: 0,
			},
		},
		core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, FlexGrow: 1},
			Children: []core.Node{
				core.Text{
					Content: oldText,
					Style: core.Style{
						FontSize:       12.5,
						Color:          p.TextFaint,
						TextDecoration: core.TextDecorationLineThrough,
					},
				},
				core.Text{Content: "→", Style: core.Style{FontSize: 12.5, Color: p.TextFaint}},
				core.Text{Content: newText, Style: core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary}},
			},
		},
	}
	if ch.DeltaPct != "" && !ch.Reverted {
		diffChildren = append(diffChildren, core.Text{
			TestID:  "review-prefab-delta",
			Content: ch.DeltaPct,
			Style: core.Style{
				FontFamily: core.FontMonospace,
				FontSize:   11.5,
				FontWeight: 600,
				Color:      role.Fg,
				FlexShrink: 0,
			},
		})
	}

	impact := ch.PrefabImpact
	if impact == "" {
		impact = ed.T("reviewScreen.prefabDefaultImpact")
	}

	return core.View{
		TestID: "review-center-prefab",
		Style: core.Style{
			FlexGrow:       1,
			Width:          core.Pct(100),
			Height:         core.Pct(100),
			Padding:        core.All(24),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(440),
					CornerRadius: 12,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					// Visual Before -> After comparison strip.
					core.View{
						Style: core.Style{
							Height:         core.Pt(148),
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Gap:            40,
							Background:     p.Viewport,
							Borders:        core.Edges{Bottom: 1},
							BorderColor:    p.Line,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{AlignItems: core.AlignCenter, Gap: 8},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Width:        core.Pt(56),
											Height:       core.Pt(34),
											CornerRadius: 12,
											Background:   p.Added.Fill,
											BorderWidth:  1,
											BorderColor:  p.Added.Edge,
										},
									},
									core.Text{Content: oldText, Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
								},
							},
							core.Icon{SVG: lucide.ArrowRight(), Style: core.Style{Width: core.Pt(18), Height: core.Pt(18), Color: p.TextFaint}},
							core.View{
								Style: core.Style{AlignItems: core.AlignCenter, Gap: 8},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Width:        core.Pt(56),
											Height:       core.Pt(34),
											CornerRadius: 12,
											Background:   p.Added.Fill,
											BorderWidth:  2,
											BorderColor:  role.Edge,
										},
									},
									core.Text{Content: newText, Style: core.Style{FontSize: 11.5, FontWeight: 600, Color: p.TextPrimary}},
								},
							},
						},
					},
					core.View{
						Style: core.Style{Padding: core.All(16), Gap: 12},
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
								Children: []core.Node{
									core.Text{Content: path.Base(ch.File), Style: core.Style{FontSize: 14, FontWeight: 600, Color: p.TextPrimary}},
									core.Text{Content: ed.T("reviewScreen.prefabTag"), Style: core.Style{FontSize: 11.5, Color: p.TextFaint}},
								},
							},
							core.View{
								TestID: "review-prefab-field-row",
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 12),
									Gap:          8,
									CornerRadius: 6,
									Background:   role.Fill,
									BorderWidth:  1.5,
									BorderColor:  role.Edge,
								},
								Children: diffChildren,
							},
							core.Text{
								TestID:  "review-prefab-impact",
								Content: impact,
								Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
							},
						},
					},
				},
			},
		},
	}
}

func reviewDataTableCenterView(ed *Editor, p SpecPalette, ch *ReviewChange) core.Node {
	tbl := ed.ReviewDataTablePreviewFor(ch.File)
	if tbl == nil {
		return reviewGenericFileCenterView(ed, p, ch)
	}

	headerCells := make([]core.Node, 0, len(tbl.Columns))
	for i, col := range tbl.Columns {
		st := core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.6,
			Color:         p.TextFaint,
		}
		if i == 0 {
			st.Width = core.Pt(112)
			st.FlexShrink = 0
		} else {
			st.FlexGrow = 1
			st.FlexBasis = core.Pt(0)
			st.MinWidth = core.Pt(0)
		}
		headerCells = append(headerCells, core.Text{Content: strings.ToUpper(col), Style: st})
	}

	rowNodes := []core.Node{
		core.View{
			Style: core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Height:      core.Pt(32),
				Padding:     core.Symmetric(0, 16),
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: headerCells,
		},
	}

	for _, r := range tbl.Rows {
		rowBg := core.Transparent
		leftColor := core.Transparent
		hasAccentBar := false
		if r.Focused || r.Changed || r.Added || r.Removed {
			rowBg = p.Raised
			hasAccentBar = true
			switch {
			case r.Added:
				leftColor = p.Added.Edge
			case r.Removed:
				leftColor = p.Removed.Edge
			default:
				leftColor = p.Changed.Edge
			}
		}

		cells := make([]core.Node, 0, len(tbl.Columns)+1)
		if hasAccentBar {
			cells = append(cells, core.View{
				Style: core.Style{
					Position:   core.Absolute,
					Top:        core.Pt(0),
					Bottom:     core.Pt(0),
					Left:       core.Pt(0),
					Width:      core.Pt(2),
					Background: leftColor,
				},
			})
		}
		for i, col := range tbl.Columns {
			val := r.Values[col]
			if diff, ok := r.CellDiffs[col]; ok {
				label := fmt.Sprintf("%s → %s", diff.OldValue, diff.NewValue)
				if diff.DeltaPct != "" {
					label += " " + diff.DeltaPct
				}
				wrapStyle := core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
				}
				if i == 0 {
					wrapStyle.Width = core.Pt(112)
					wrapStyle.FlexShrink = 0
				} else {
					wrapStyle.FlexGrow = 1
					wrapStyle.FlexBasis = core.Pt(0)
					wrapStyle.MinWidth = core.Pt(0)
				}
				cells = append(cells, core.View{
					TestID: fmt.Sprintf("review-table-cell-%s-%s", r.ID, col),
					Style:  wrapStyle,
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Height:         core.Pt(24),
								Padding:        core.Symmetric(0, 8),
								CornerRadius:   6,
								Background:     p.Changed.Fill,
								BorderWidth:    1.5,
								BorderColor:    p.Changed.Edge,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: label,
									Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, FontWeight: 500, Color: p.TextPrimary},
								},
							},
						},
					},
				})
				continue
			}
			cellStyle := core.Style{
				FontSize: 12,
				Color:    p.TextMuted,
			}
			if i == 0 {
				cellStyle.Width = core.Pt(112)
				cellStyle.FlexShrink = 0
				cellStyle.FontFamily = core.FontMonospace
				cellStyle.FontSize = 11.5
				cellStyle.Color = p.TextPrimary
			} else {
				cellStyle.FlexGrow = 1
				cellStyle.FlexBasis = core.Pt(0)
				cellStyle.MinWidth = core.Pt(0)
			}
			if r.Removed {
				cellStyle.TextDecoration = core.TextDecorationLineThrough
				cellStyle.Color = p.Removed.Fg
			}
			cells = append(cells, core.Text{Content: val, Style: cellStyle})
		}

		rowNodes = append(rowNodes, core.View{
			TestID: "review-table-row-" + r.ID,
			Style: core.Style{
				Position:    core.Relative,
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Height:      core.Pt(34),
				Padding:     core.Symmetric(0, 16),
				Background:  rowBg,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: cells,
		})
	}

	tableSummary := ed.T("reviewScreen.tableSummaryMany", l10n.Param("changed", tbl.ChangedCount), l10n.Param("total", tbl.TotalRows))
	if tbl.TotalRows == 1 {
		tableSummary = ed.T("reviewScreen.tableSummaryOne", l10n.Param("changed", tbl.ChangedCount))
	}
	return core.View{
		TestID: "review-center-data",
		Style: core.Style{
			FlexGrow:   1,
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Padding:    core.All(24),
			Gap:        12,
			Background: p.Bg,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.Text{Content: tbl.File, Style: core.Style{FontSize: 14, FontWeight: 600, Color: p.TextPrimary}},
					core.Text{
						Content: tableSummary,
						Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
					},
				},
			},
			core.View{
				TestID: "review-data-table",
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					MinHeight:    core.Pt(0),
					CornerRadius: 12,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.Line,
					Overflow:     core.Hidden,
				},
				Children: []core.Node{
					rowNodes[0],
					ui.ScrollView{
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							MinHeight: core.Pt(0),
						},
						Children: rowNodes[1:],
					},
				},
			},
		},
	}
}

func reviewGenericFileCenterView(ed *Editor, p SpecPalette, ch *ReviewChange) core.Node {
	return core.View{
		TestID: "review-center-file",
		Style: core.Style{
			FlexGrow:   1,
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Padding:    core.All(24),
			Gap:        12,
			Background: p.Bg,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
				Children: []core.Node{
					core.Text{Content: ch.File, Style: core.Style{FontFamily: core.FontMonospace, FontSize: 14, FontWeight: 600, Color: p.TextPrimary}},
					core.Text{Content: ch.Detail, Style: core.Style{FontFamily: core.FontMonospace, FontSize: 12, Color: p.TextMuted}},
				},
			},
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					CornerRadius: 12,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.Line,
					Padding:      core.Symmetric(8, 0),
				},
				Children: []core.Node{reviewYAMLDiffLinesView(ed, p, ch.YAMLLines)},
			},
		},
	}
}

// --- Right pane: Why + YAML lines touched + in-place YAML editing ---

func reviewRightView(cx *core.Scope, ed *Editor, reviewRev *core.Memo[uint64], activeRegion *core.Signal[string], rightRef, yamlInputRef *core.Ref) core.Node {
	// Key the YAML input strictly on (editing, changeID, yamlEditSessionRev) — NEVER on reviewRev —
	// so typing in the multiline YAML editor never remounts ui.Input, steals focus, or resets caret!
	yamlEditKey := core.NewMemo(cx, func() string {
		_ = reviewRev.Get()
		if ed.ReviewViewingReadOnlyRound() {
			return ""
		}
		ch := ed.review.changesByID[ed.review.focusedID]
		if ch == nil || !ch.EditingYAML {
			return ""
		}
		return fmt.Sprintf("%s:%d", ch.ID, ed.review.yamlEditSessionRev)
	})

	headerNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		ch := ed.review.changesByID[ed.review.focusedID]
		readOnly := ed.ReviewViewingReadOnlyRound()

		title := "YAML"
		var headerActions []core.Node
		if ch != nil {
			title = ed.T("reviewScreen.rightTitleYAML", l10n.Param("title", ch.Title))
			if ch.FileKind == ReviewFileGo {
				title = ed.T("reviewScreen.rightTitleDiff", l10n.Param("title", ch.Title))
			}
			changeID := ch.ID
			editing := !readOnly && ch.EditingYAML
			canEdit := !readOnly && !editing && ch.CanEditYAML()
			yamlBroken := ch.YAMLError != nil

			if canEdit {
				headerActions = append(headerActions, core.View{
					TestID:    "review-yaml-edit",
					Role:      core.RoleButton,
					Label:     ed.T("reviewScreen.editYAMLLabel", l10n.Param("title", ch.Title)),
					Focusable: func() bool { return true },
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Height:       core.Pt(28),
						Padding:      core.Symmetric(0, 10),
						Gap:          6,
						CornerRadius: 6,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FlexShrink:   0,
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.BeginReviewYAMLEdit(changeID))
					},
					Children: []core.Node{
						core.Icon{
							SVG:   lucide.Pencil(),
							Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: p.TextSecondary},
						},
						core.Text{
							Content: ed.T("reviewScreen.edit"),
							Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
						},
					},
				})
			}
			if editing {
				doneStyle := core.Style{
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 12),
					CornerRadius:   6,
					Background:     p.Accent.Fg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
					FocusVisible:   p.FocusRing(2),
				}
				if yamlBroken {
					doneStyle.Opacity = core.Alpha(0.45)
				}
				headerActions = append(headerActions,
					core.Text{
						TestID:  "review-yaml-editing-label",
						Content: ed.T("reviewScreen.editing"),
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint, FlexShrink: 0},
					},
					core.View{
						TestID:    "review-yaml-done",
						Role:      core.RoleButton,
						Label:     ed.T("reviewScreen.doneEditingYAML"),
						Focusable: func() bool { return true },
						Style:     doneStyle,
						OnPress: func(core.PressEvent) {
							if err := ed.FinishReviewYAMLEdit(changeID); err != nil && !errors.Is(err, ErrReviewYAMLInvalid) {
								ed.report(err)
							}
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("reviewScreen.done"),
								Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.Accent.OnFill},
							},
						},
					},
				)
			}
		}

		return core.View{
			Style: core.Style{
				Height:      core.Pt(44),
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         8,
				Padding:     core.Edges{Left: 14, Right: 10},
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
				FlexShrink:  0,
			},
			Children: append([]core.Node{
				core.Text{
					TestID:   "review-right-title",
					Content:  title,
					MaxLines: 1,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 600,
						Color:      p.TextPrimary,
						FlexGrow:   1,
						MinWidth:   core.Pt(0),
						Overflow:   core.Hidden,
					},
				},
			}, headerActions...),
		}
	})

	diffBodyNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		ch := ed.review.changesByID[ed.review.focusedID]
		readOnly := ed.ReviewViewingReadOnlyRound()
		viewRound := ed.ReviewViewRound()

		whyText := ""
		var ylines []ReviewYAMLLine
		reverted := false
		editing := false
		yamlBroken := false
		if ch != nil {
			reverted = ch.Reverted
			if readOnly {
				ver := ch.VersionFor(viewRound)
				whyText = ver.Why
				ylines = ver.YAMLLines
			} else {
				whyText = ch.Why
				ylines = ch.YAMLLines
				editing = ch.EditingYAML
				yamlBroken = ch.YAMLError != nil
			}
		}

		var bodyKids []core.Node
		if whyText != "" {
			bodyKids = append(bodyKids, core.View{
				TestID: "review-why-box",
				Style: core.Style{
					Padding:     core.All(14),
					Gap:         6,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.Line,
					FlexShrink:  0,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("reviewScreen.whyHeading"),
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
						},
					},
					core.Text{
						TestID:  "review-why-text",
						Content: whyText,
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextSecondary,
						},
					},
				},
			})
		}

		if reverted {
			bodyKids = append(bodyKids, core.View{
				TestID: "review-reverted-banner",
				Style: core.Style{
					Margin:       core.Edges{Top: 10, Left: 14, Right: 14},
					Padding:      core.Symmetric(8, 10),
					CornerRadius: 8,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FlexShrink:   0,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("reviewScreen.revertedBanner"),
						Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
					},
				},
			})
		}

		scrollStyle := core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			MinHeight: core.Pt(0),
			Padding:   core.Symmetric(10, 0),
		}
		if editing {
			scrollStyle.Background = p.Inset
			scrollStyle.Borders = core.Edges{Bottom: 1}
			if yamlBroken {
				scrollStyle.BorderColor = p.Removed.Edge
			} else {
				scrollStyle.BorderColor = p.Accent.Edge
			}
		}

		bodyKids = append(bodyKids, ui.ScrollView{
			TestID:   "review-yaml-scroll",
			Style:    scrollStyle,
			Children: []core.Node{reviewYAMLDiffLinesView(ed, p, ylines)},
		})

		return core.View{
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				MinHeight: core.Pt(0),
			},
			Children: bodyKids,
		}
	})

	yamlEditorNode := core.Dynamic(func(cx *core.Scope) core.Node {
		key := yamlEditKey.Get()
		if key == "" {
			return core.View{Style: core.Style{Height: core.Pt(0), FlexShrink: 0}}
		}
		ch := ed.review.changesByID[ed.review.focusedID]
		if ch == nil || !ch.EditingYAML {
			return core.View{Style: core.Style{Height: core.Pt(0), FlexShrink: 0}}
		}
		changeID := ch.ID
		title := ch.Title
		draftSig := core.NewSignal(cx, ch.HandEditYAML)

		return core.View{
			TestID: "review-yaml-editor-box",
			StyleFunc: func() core.Style {
				p := ed.SpecPalette(cx)
				return core.Style{
					Padding:     core.All(12),
					Background:  p.Inset,
					Borders:     core.Edges{Top: 1},
					BorderColor: p.Line,
					Gap:         6,
					FlexShrink:  0,
				}
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("reviewScreen.editYAMLHeading"),
					StyleFunc: func() core.Style {
						p := ed.SpecPalette(cx)
						return core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
						}
					},
				},
				specInputField(cx, ed, ui.Input{
					Ref:       yamlInputRef,
					TestID:    "review-yaml-input",
					Label:     ed.T("reviewScreen.editYAMLLabel", l10n.Param("title", title)),
					Multiline: true,
					Value:     draftSig.Get,
					OnChange: func(v string) {
						draftSig.Set(v)
						_ = ed.SetReviewYAMLDraft(changeID, v)
					},
					StyleFunc: func() core.Style {
						p := ed.SpecPalette(cx)
						return core.Style{
							Width:        core.Pct(100),
							Height:       core.Pt(160),
							Background:   p.Bg,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							CornerRadius: 6,
							Padding:      core.All(8),
							FontSize:     11.5,
							FontFamily:   core.FontMonospace,
							Color:        p.TextPrimary,
							FocusVisible: p.FocusRing(-2),
						}
					},
				}),
			},
		}
	})

	yamlFooterNode := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = reviewRev.Get()
		p := ed.SpecPalette(cx)
		if ed.ReviewViewingReadOnlyRound() {
			return core.View{Style: core.Style{Height: core.Pt(0), FlexShrink: 0}}
		}
		ch := ed.review.changesByID[ed.review.focusedID]
		if ch == nil {
			return core.View{Style: core.Style{Height: core.Pt(0), FlexShrink: 0}}
		}
		changeID := ch.ID
		if ch.YAMLError != nil {
			errObj := ch.YAMLError
			var errActions []core.Node
			if errObj.Fix != nil {
				errActions = append(errActions, core.View{
					TestID:    "review-yaml-fix",
					Role:      core.RoleButton,
					Label:     errObj.Fix.Label,
					Focusable: func() bool { return true },
					Style: core.Style{
						Height:         core.Pt(28),
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
						ed.report(ed.ApplyReviewYAMLFix(changeID))
					},
					Children: []core.Node{
						core.Text{
							Content: errObj.Fix.Label,
							Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
						},
					},
				})
			}
			errActions = append(errActions, core.View{
				TestID:    "review-yaml-undo",
				Role:      core.RoleButton,
				Label:     ed.T("reviewScreen.undoTyping"),
				Focusable: func() bool { return true },
				Style: core.Style{
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 8),
					CornerRadius:   8,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.UndoReviewYAMLTyping(changeID))
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("reviewScreen.undoTyping"),
						Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
					},
				},
			})

			return core.View{
				TestID: "review-yaml-error",
				Role:   core.RoleAlert,
				Style: core.Style{
					Margin:       core.Edges{Top: 10, Right: 14, Bottom: 14, Left: 14},
					Padding:      core.All(12),
					CornerRadius: 12,
					Background:   p.Removed.Fill,
					BorderWidth:  1,
					BorderColor:  p.Removed.Edge,
					Gap:          8,
					FlexShrink:   0,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
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
								TestID:  "review-yaml-error-title",
								Content: errObj.Title,
								Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.Removed.Fg},
							},
						},
					},
					core.Text{
						TestID:  "review-yaml-error-message",
						Content: errObj.Message,
						Style:   core.Style{FontSize: 12.5, Color: p.TextSecondary},
					},
					core.View{
						Style:    core.Style{Direction: core.Row, Gap: 8},
						Children: errActions,
					},
				},
			}
		}
		if ch.EditingYAML {
			return core.View{
				Style: core.Style{
					Margin:     core.Edges{Top: 8, Right: 14, Bottom: 12, Left: 14},
					FlexShrink: 0,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "review-yaml-valid-hint",
						Content: ed.T("reviewScreen.yamlValidHint"),
						Style:   core.Style{FontSize: 12, Color: p.TextFaint},
					},
				},
			}
		}
		return core.View{Style: core.Style{Height: core.Pt(0), FlexShrink: 0}}
	})

	return core.View{
		Ref:       rightRef,
		TestID:    "review-right-pane",
		Role:      core.RoleRegion,
		Label:     ed.T("reviewScreen.rightPaneLabel"),
		Focusable: func() bool { return true },
		OnFocusIn: func() {
			if activeRegion.GetUntracked() != "" && activeRegion.GetUntracked() != reviewRegionRight {
				activeRegion.Set("")
			}
		},
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			base := core.Style{
				Width:       core.Pt(392),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				MinHeight:   core.Pt(0),
				Overflow:    core.Hidden,
			}
			return reviewRegionStyle(p, activeRegion, reviewRegionRight, base)
		},
		Children: []core.Node{
			headerNode,
			diffBodyNode,
			yamlEditorNode,
			yamlFooterNode,
		},
	}
}

func reviewYAMLDiffLinesView(ed *Editor, p SpecPalette, lines []ReviewYAMLLine) core.Node {
	nodes := make([]core.Node, 0, len(lines))
	for _, l := range lines {
		bg := core.Transparent
		fg := p.TextFaint
		signColor := p.TextFaint
		switch l.Sign {
		case "+":
			bg = p.Added.Fill
			fg = p.Added.Fg
			signColor = p.Added.Fg
		case "-":
			bg = p.Removed.Fill
			fg = p.Removed.Fg
			signColor = p.Removed.Fg
		default:
			fg = p.TextMuted
		}
		if l.Error {
			bg = p.Removed.Fill
			fg = p.TextPrimary
			signColor = p.Removed.Fg
		}
		lineNum := ""
		if l.Line > 0 {
			lineNum = strconv.Itoa(l.Line)
		}
		sign := l.Sign
		if sign == "-" {
			sign = "−"
		}
		tagText := l.Tag
		if tagText == "" && l.Author != ReviewAuthorNone {
			tagText = string(l.Author)
		}
		tagColor := p.TextFaint
		if l.Error {
			if tagText == "" && l.Line > 0 {
				tagText = ed.T("reviewScreen.lineTag", l10n.Param("line", l.Line))
			}
			tagColor = p.Removed.Fg
		}

		rowChildren := []core.Node{
			core.Text{
				Content: lineNum,
				Style: core.Style{
					Width:      core.Pt(30),
					FontFamily: core.FontMonospace,
					FontSize:   11,
					Color:      p.TextFaint,
					FlexShrink: 0,
				},
			},
			core.Text{
				Content: sign,
				Style: core.Style{
					Width:      core.Pt(12),
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					FontWeight: 600,
					Color:      signColor,
					FlexShrink: 0,
				},
			},
			core.Text{
				Content: l.Text,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11.5,
					Color:      fg,
					FlexGrow:   1,
					MinWidth:   core.Pt(0),
				},
			},
		}
		if tagText != "" {
			rowChildren = append(rowChildren, core.Text{
				TestID:  fmt.Sprintf("review-yaml-tag-%d", l.Line),
				Content: tagText,
				Style: core.Style{
					FontSize:   11,
					Color:      tagColor,
					FlexShrink: 0,
					Padding:    core.Edges{Left: 6},
				},
			})
		}

		rowStyle := core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Padding:    core.Symmetric(2, 12),
			Gap:        6,
			Background: bg,
		}
		if l.Error {
			rowStyle.Borders = core.Edges{Bottom: 1.5}
			rowStyle.BorderColor = p.Removed.Edge
		}
		nodes = append(nodes, core.View{
			Style:    rowStyle,
			Children: rowChildren,
		})
	}
	return core.View{TestID: "review-yaml-lines", Children: nodes}
}

// --- Main Editor right-rail Agent panel cards (Pending / Kept / Reverted) ---

// agentReviewPanelCards renders the agent turn review card (Review waiting + Resume review,
// or the Kept / Reverted outcome summary) inside AgentPanelView's thread list
// (matching design/editor/Review.dc.html).
func agentReviewPanelCards(ed *Editor, p SpecPalette, turns []AgentTurn) []core.Node {
	snap := ed.ReviewStatus()
	var cards []core.Node
	if snap.Request != "" && (len(turns) == 0 || turns[len(turns)-1].Prompt != snap.Request) {
		cards = append(cards, core.View{
			TestID: "agent-request-bubble",
			Style: core.Style{
				AlignSelf:    core.AlignEnd,
				MaxWidth:     core.Pt(296),
				Padding:      core.Symmetric(8, 12),
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.Text{Content: snap.Request, Style: core.Style{FontSize: 12.5, Color: p.TextSecondary}},
			},
		})
	}

	switch snap.Outcome {
	case ReviewOutcomePending:
		var title string
		switch {
		case snap.TotalChanges == 1 && snap.FileCount == 1:
			title = ed.T("reviewScreen.waitingTitleOneOne")
		case snap.TotalChanges == 1:
			title = ed.T("reviewScreen.waitingTitleOneMany", l10n.Param("files", snap.FileCount))
		case snap.FileCount == 1:
			title = ed.T("reviewScreen.waitingTitleManyOne", l10n.Param("changes", snap.TotalChanges))
		default:
			title = ed.T("reviewScreen.waitingTitleManyMany", l10n.Param("changes", snap.TotalChanges), l10n.Param("files", snap.FileCount))
		}
		if snap.Round > 1 {
			title = ed.T("reviewScreen.waitingRoundSuffix", l10n.Param("title", title), l10n.Param("round", snap.Round))
		}
		sub := ed.T("reviewScreen.seenCounter", l10n.Param("seen", snap.SeenCount), l10n.Param("total", snap.TotalChanges))
		if snap.DraftNoteCount == 1 {
			sub = ed.T("reviewScreen.waitingUnsentOne", l10n.Param("sub", sub))
		} else if snap.DraftNoteCount > 1 {
			sub = ed.T("reviewScreen.waitingUnsentMany", l10n.Param("sub", sub), l10n.Param("count", snap.DraftNoteCount))
		}
		if snap.PendingBatch != nil && len(snap.PendingBatch.Notes) > 0 {
			if len(snap.PendingBatch.Notes) == 1 {
				sub = ed.T("reviewScreen.waitingSentOne", l10n.Param("sub", sub))
			} else {
				sub = ed.T("reviewScreen.waitingSentMany", l10n.Param("sub", sub), l10n.Param("count", len(snap.PendingBatch.Notes)))
			}
		}
		if len(snap.Blockers) == 1 {
			sub = ed.T("reviewScreen.waitingBlockerOne", l10n.Param("sub", sub))
		} else if len(snap.Blockers) > 1 {
			sub = ed.T("reviewScreen.waitingBlockerMany", l10n.Param("sub", sub), l10n.Param("count", len(snap.Blockers)))
		}
		cards = append(cards, core.View{
			TestID: "agent-review-waiting-card",
			Style: core.Style{
				Padding:      core.All(12),
				Gap:          8,
				CornerRadius: 12,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: []core.Node{
				core.Text{TestID: "agent-review-waiting-title", Content: title, Style: core.Style{FontSize: 13, FontWeight: 600, Color: p.TextPrimary}},
				core.Text{TestID: "agent-review-waiting-meta", Content: sub, Style: core.Style{FontSize: 12, Color: p.TextMuted}},
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 8},
					Children: []core.Node{
						specReviewButton("agent-resume-review", ed.T("reviewScreen.resumeReview"), p.Accent.Fg, core.Transparent, p.Accent.OnFill, p, func() {
							ed.report(ed.ResumeReview())
						}),
					},
				},
			},
		})
	case ReviewOutcomeKept:
		cards = append(cards, core.View{
			TestID: "agent-review-kept-card",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.Icon{SVG: lucide.Check(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: p.Added.Fg}},
				core.Text{
					TestID:  "agent-review-outcome-text",
					Content: snap.LastOutcomeMsg,
					Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
				},
			},
		})
	case ReviewOutcomeReverted:
		cards = append(cards, core.Text{
			TestID:  "agent-review-outcome-text",
			Content: snap.LastOutcomeMsg,
			Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
		})
	}
	return cards
}

// drawPendingReviewOverlay draws active (non-reverted) pending turn change overlays in the main 2D viewport
// while a review is paused/waiting after pressing Esc.
func (e *Editor) drawPendingReviewOverlay(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing || !e.HasPendingReview() || e.ReviewOpen() {
		return
	}
	overlays := e.ReviewOverlays()
	if len(overlays) == 0 {
		return
	}
	p := e.SpecPalette(nil)
	for _, ov := range overlays {
		role := reviewKindSpecRole(p, ov.Kind)
		x0, y0 := e.WorldToCanvas(ov.X, ov.Y)
		x1, y1 := e.WorldToCanvas(ov.X+ov.W, ov.Y+ov.H)
		rx := min(x0, x1) - 3
		ry := min(y0, y1) - 3
		rw := float32(math.Abs(float64(x1-x0))) + 6
		rh := float32(math.Abs(float64(y1-y0))) + 6

		d.Save()
		if ov.Kind == ReviewChangeRemoved {
			d.SetFillColor(ui.Alpha(p.TextFaint, 0.25))
			d.BeginPath().RoundRect(rx+3, ry+3, max(rw-6, 4), max(rh-6, 4), 4).Fill()
		} else {
			d.SetFillColor(ui.Alpha(role.Fg, 0.14))
			d.BeginPath().RoundRect(rx+2, ry+2, max(rw-4, 4), max(rh-4, 4), 4).Fill()
		}
		d.SetStrokeColor(role.Edge).SetLineWidth(1.5)
		if ov.Dashed {
			d.SetLineDash(5, 3)
		} else {
			d.SetLineDash()
		}
		d.BeginPath().RoundRect(rx, ry, rw, rh, 6).Stroke()
		d.Restore()
	}
	for _, pin := range e.ReviewViewportPins() {
		px, py := e.WorldToCanvas(pin.X, pin.Y)
		drawReviewViewportNotePin(d, px+4, py-20, p, pin.Redone)
	}
}

func ternaryString(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func ternaryColor(cond bool, a, b core.Color) core.Color {
	if cond {
		return a
	}
	return b
}
