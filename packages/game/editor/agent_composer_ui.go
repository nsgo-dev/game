package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

// agentComposerSummaryCard renders the compacted or fresh-thread summary card inside AgentPanelView.
func agentComposerSummaryCard(ed *Editor, p SpecPalette, summary string) core.Node {
	return core.View{
		TestID: "agent-thread-summary",
		Style: core.Style{
			Padding:      core.Symmetric(specSpace8, specSpace12),
			CornerRadius: specRadiusButton,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Gap:          specSpace4,
		},
		Children: []core.Node{
			specSectionLabel(p, ed.T("composer.summaryHeading")),
			core.Text{
				Content: summary,
				Style: core.Style{
					FontSize: 11.5,
					Color:    p.TextMuted,
				},
			},
		},
	}
}

// agentComposerThreadNodes renders folded earlier composer requests and the latest composer turn inside AgentPanelView.
func agentComposerThreadNodes(ed *Editor, p SpecPalette, turns []AgentThreadTurn) []core.Node {
	if len(turns) == 0 {
		return nil
	}
	var nodes []core.Node
	if len(turns) > 1 {
		earlier := turns[:len(turns)-1]
		nodes = append(nodes, specSectionLabel(p, ed.T("composer.earlierRequests", l10n.Param("count", len(earlier)))))
		for i, tr := range earlier {
			nodes = append(nodes, core.View{
				TestID: fmt.Sprintf("agent-turn-folded-%d", i),
				Style: core.Style{
					Height:       core.Pt(specRowHeight),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace8,
					Padding:      core.Symmetric(0, specSpace8),
					CornerRadius: specRadiusControl,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(16),
							Height:         core.Pt(16),
							FlexShrink:     0,
							CornerRadius:   8,
							Background:     p.Added.Fill,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{specIcon(lucide.Check, 10, p.Added.Fg)},
					},
					core.Text{
						Content:  tr.Prompt,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							FontSize:   12,
							Color:      p.TextSecondary,
						},
					},
				},
			})
		}
		nodes = append(nodes, core.View{
			Style: core.Style{
				Height:     core.Pt(1),
				Background: p.Line,
				Margin:     core.Symmetric(specSpace4, 0),
			},
		})
	}

	latest := turns[len(turns)-1]
	nodes = append(nodes, core.View{
		TestID: "agent-turn-latest",
		Style: core.Style{
			Gap: specSpace8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					AlignSelf:    core.AlignEnd,
					MaxWidth:     core.Pt(300),
					Padding:      core.Symmetric(specSpace8, specSpace12),
					CornerRadius: specRadiusCard,
					Background:   p.Accent.Fill,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: latest.Prompt,
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextPrimary,
						},
					},
				},
			},
		},
	})
	if strings.TrimSpace(latest.Reply) != "" {
		nodes = append(nodes, core.Text{
			TestID:  "agent-turn-latest-reply",
			Content: latest.Reply,
			Style: core.Style{
				FontSize: 12,
				Color:    p.TextMuted,
			},
		})
	}
	return nodes
}

// AgentComposerView renders the agent composer card with:
//   - @-mention picker popover (match count, group tabs, ↑↓/↵/Tab/Esc)
//   - Context chips (attached, suggested selection chip, Always chips, toggleable Viewport image)
//   - Hover preview showing the exact text/YAML or image sent to the agent
//   - Context usage ring (normal / over 70% / compact now) and 6-row breakdown popover with Compact & Start fresh
func AgentComposerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	inputRef := &core.Ref{}
	composerFocused := core.NewSignal(cx, false)
	var textInputFocused bool
	var suppressSubmit bool
	cx.Cleanup(func() {
		if textInputFocused {
			textInputFocused = false
			ed.noteTextInputFocus(false)
		}
	})
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionInspector, inputRef, func() bool {
			return ed.RightRailTab() == RightTabAgent
		})
	}
	lastFocusSeq := core.NewSignal(cx, ed.agentComposer.composerFocusSeq)

	// Focus input when FocusComposer bumps composerFocusSeq.
	cx.Effect(func() {
		_ = rev.Get()
		seq := ed.agentComposer.composerFocusSeq
		if seq != lastFocusSeq.GetUntracked() {
			lastFocusSeq.Set(seq)
			inputRef.Focus()
		}
	})

	// Shift+Enter inserts a newline while the composer is focused; plain Enter submits.
	cx.OnKeyWith("shift+enter", core.KeyOptions{When: core.KeyAlways}, func() {
		if ed.composerKeysBlocked() {
			return
		}
		if (composerFocused.Get() || inputRef.HoldsFocus()) && !ed.MentionPickerOpen() {
			ed.InsertAgentComposerNewline()
		}
	})

	// Keyboard navigation while the @-mention picker is open (↑↓ move, ↵ add, Tab next group, Esc close).
	// Unmounted automatically whenever a higher-priority modal, Play-in-editor, or non-Agent tab is active.
	pickerKeyScope := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.MentionPickerOpen() && !ed.composerKeysBlocked()
		},
		Then: func(cx *core.Scope) core.Node {
			opts := core.KeyOptions{When: core.KeyAlways}
			cx.OnKeyWith("down", opts, func() {
				if ed.composerKeysBlocked() {
					return
				}
				ed.MoveMentionSelection(+1)
			})
			cx.OnKeyWith("up", opts, func() {
				if ed.composerKeysBlocked() {
					return
				}
				ed.MoveMentionSelection(-1)
			})
			cx.OnKeyWith("tab", opts, func() {
				if ed.composerKeysBlocked() {
					return
				}
				ed.CycleMentionPickerGroup(+1)
			})
			cx.OnKeyWith("shift+tab", opts, func() {
				if ed.composerKeysBlocked() {
					return
				}
				ed.CycleMentionPickerGroup(-1)
			})
			cx.OnKeyWith("enter", opts, func() {
				if ed.composerKeysBlocked() {
					return
				}
				suppressSubmit = true
				ed.AcceptActiveMention()
				inputRef.Focus()
				cx.Post(func() {
					suppressSubmit = false
				})
			})
			cx.OnKeyWith("escape", opts, func() {
				if ed.composerKeysBlocked() {
					return
				}
				ed.CloseMentionPicker()
				inputRef.Focus()
			})
			return nil
		},
	}

	// Escape closes the context breakdown popover when open.
	breakdownKeyScope := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.ContextBreakdownOpen() && !ed.composerKeysBlocked()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.composerKeysBlocked() {
					return
				}
				ed.SetContextBreakdownOpen(false)
			})
			return nil
		},
	}

	// Composer keys are mounted only while the composer input actually holds focus (and for
	// Backspace/Up only when the draft is empty and the picker is closed) so newest-wins key
	// dispatch never shadows parent shortcuts like Mod+Enter (Keep turn/review) or Backspace
	// (Delete object) while focus is elsewhere.
	composerFocusedKeyScope := core.Show{
		When: func() bool {
			_ = rev.Get()
			return (composerFocused.Get() || inputRef.HoldsFocus()) && !ed.composerKeysBlocked()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("mod+enter", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.composerKeysBlocked() {
					return
				}
				if !inputRef.HoldsFocus() && !composerFocused.Get() {
					return
				}
				if ed.MentionPickerOpen() {
					suppressSubmit = true
					ed.AcceptActiveMention()
					inputRef.Focus()
					cx.Post(func() {
						suppressSubmit = false
					})
					return
				}
				ed.submitComposerFromUI()
			})
			return nil
		},
	}
	composerEmptyKeyScope := core.Show{
		When: func() bool {
			_ = rev.Get()
			return (composerFocused.Get() || inputRef.HoldsFocus()) &&
				!ed.composerKeysBlocked() && !ed.MentionPickerOpen() && ed.ComposerDraft() == ""
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("backspace", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.composerKeysBlocked() || ed.MentionPickerOpen() || ed.ComposerDraft() != "" {
					return
				}
				if inputRef.HoldsFocus() || composerFocused.Get() {
					ed.RemoveLastComposerChip()
				}
			})
			cx.OnKeyWith("up", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.composerKeysBlocked() || ed.MentionPickerOpen() || ed.ComposerDraft() != "" {
					return
				}
				if inputRef.HoldsFocus() || composerFocused.Get() {
					ed.RecallLastComposerPrompt()
				}
			})
			return nil
		},
	}

	// Fine-grained memo keys so normal typing in composerInput never triggers disk I/O or rebuilds
	// chip rows, action buttons, or popovers on every keystroke.
	refMemo := core.NewMemo(cx, func() agentComposerRefKey {
		_ = rev.Get()
		return buildAgentComposerRefKey(ed)
	})

	popoverKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		rk := refMemo.Get()
		pickerOpen := ed.MentionPickerOpen()
		breakdownOpen := ed.ContextBreakdownOpen()
		hovered := ed.HoveredChipID()
		hasRefPopover := rk.previewID != "" || rk.pinNoticeSig != "" || rk.attachErr != "" || rk.dragOver
		if !pickerOpen && !breakdownOpen && hovered == "" && !hasRefPopover {
			return ""
		}
		dark := ed.SpecPalette(cx).Dark
		lang := ed.ResolvedLanguage()
		var b strings.Builder
		fmt.Fprintf(&b, "%v|%s|%v|%v|%s|", dark, lang, pickerOpen, breakdownOpen, hovered)
		if pickerOpen {
			fmt.Fprintf(&b, "p:%s|%s|%d|%d|%d|%d|",
				ed.MentionPickerGroup(), ed.MentionPickerQuery(), ed.MentionPickerActiveIndex(),
				len(ed.agentComposer.attached), ed.filesRev, ed.Revision())
		}
		if breakdownOpen {
			fmt.Fprintf(&b, "b:%d|%d|%v|%d|",
				ed.agentComposer.threadRev, len(ed.agentComposer.attached), ed.ViewportImageIncluded(), ed.Revision())
		}
		if hovered != "" {
			fmt.Fprintf(&b, "h:%s|%d|", hovered, ed.Revision())
		}
		if hasRefPopover {
			fmt.Fprintf(&b, "r:%s|%s|%v|%s|%v|%s|%s|",
				rk.previewID, rk.pinNoticeSig, rk.canUndoPin, rk.attachErr, rk.dragOver, rk.attachedSig, rk.briefSig)
		}
		return b.String()
	})

	chipRowsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		rk := refMemo.Get()
		dark := ed.SpecPalette(cx).Dark
		lang := ed.ResolvedLanguage()
		var b strings.Builder
		fmt.Fprintf(&b, "%v|%s|%s|%s|%v|%s|%d|%d|%s|%v|%s|%s|",
			dark, lang, ed.SelectedID, ed.HoveredChipID(), ed.ViewportImageIncluded(),
			ed.ProjectPath, ed.filesRev, ed.AgentDocsRevision(),
			rk.attachedSig, rk.briefExists, rk.briefSig, rk.previewID)
		for _, c := range ed.agentComposer.attached {
			b.WriteString(c.ID)
			b.WriteByte('=')
			b.WriteString(c.Name)
			b.WriteByte(';')
		}
		return b.String()
	})

	actionsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		dark := ed.SpecPalette(cx).Dark
		lang := ed.ResolvedLanguage()
		hasDraft := strings.TrimSpace(ed.ComposerDraft()) != ""
		meter := ed.ContextMeter()
		return fmt.Sprintf("%v|%s|%v|%v|%v|%v|%v|%d|%d",
			dark, lang, ed.MentionPickerOpen(), ed.ContextBreakdownOpen(),
			hasDraft, ed.AgentWorking(), ed.AgentOffline(), meter.Percent, meter.Level)
	})

	// Popovers above the composer card (rebuilt only when popoverKey changes).
	popoversView := core.Dynamic(func(cx *core.Scope) core.Node {
		if popoverKey.Get() == "" {
			return nil
		}
		p := ed.SpecPalette(cx)
		pickerOpen := ed.MentionPickerOpen()
		breakdownOpen := ed.ContextBreakdownOpen()
		hoverPayload, hasHover := ed.HoveredChipPayload()

		var topPopovers []core.Node
		if pickerOpen {
			topPopovers = append(topPopovers, mentionPickerView(p, ed, inputRef))
		}
		if breakdownOpen {
			topPopovers = append(topPopovers, contextBreakdownView(p, ed))
		}
		if hasHover && !pickerOpen && !breakdownOpen {
			topPopovers = append(topPopovers, chipHoverPreviewView(p, hoverPayload))
		}
		if pop := agentImagePreviewPopover(ed, p); pop != nil {
			topPopovers = append(topPopovers, pop)
		}
		if pinCard := agentPinNoticeCardView(ed, p); pinCard != nil {
			topPopovers = append(topPopovers, pinCard)
		}
		if errBanner := agentAttachErrorBannerView(ed, p); errBanner != nil {
			topPopovers = append(topPopovers, errBanner)
		}
		if dropZone := agentComposerDropZoneView(ed, p); dropZone != nil {
			topPopovers = append(topPopovers, dropZone)
		}
		if len(topPopovers) == 0 {
			return nil
		}
		return core.View{
			Style:    core.Style{Gap: specSpace8},
			Children: topPopovers,
		}
	})

	// Chip rows (attached, suggested, always, viewport image toggle) above the stable Input.
	chipRowsView := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = chipRowsKey.Get()
		p := ed.SpecPalette(cx)
		attached := ed.ComposerChips()
		suggested, hasSuggested := ed.SuggestedChip()
		always := ed.AlwaysChips()
		vpOn := ed.ViewportImageIncluded()

		var chipNodes []core.Node
		for i, c := range attached {
			chip := c
			dotColor := chipDotColor(p, chip.ColorRole)
			chipSelected := ed.HoveredChipID() == chip.ID
			chipBorder := p.LineRaised
			if chipSelected {
				chipBorder = p.Accent.Edge
			}
			chipNodes = append(chipNodes, core.View{
				TestID: fmt.Sprintf("agent-chip-%d", i),
				OnMouseEnter: func(core.MouseEvent) {
					ed.SetHoveredChip(chip.ID)
				},
				OnMouseLeave: func(core.MouseEvent) {
					if ed.HoveredChipID() == chip.ID {
						ed.SetHoveredChip("")
					}
				},
				Style: core.Style{
					Height:       core.Pt(24),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace4,
					Padding:      core.Edges{Left: specSpace8, Right: 2},
					CornerRadius: specRadiusControl,
					Background:   p.LineRaised,
					BorderWidth:  1,
					BorderColor:  chipBorder,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 2,
							Background:   dotColor,
							FlexShrink:   0,
						},
					},
					core.Text{
						Content:  chip.Name,
						MaxLines: 1,
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextPrimary,
						},
					},
					base.Button{
						TestID: fmt.Sprintf("agent-chip-remove-%d", i),
						Label:  ed.T("composer.removeChip", l10n.Param("name", chip.Name)),
						Style: core.Style{
							Width:          core.Pt(20),
							Height:         core.Pt(20),
							CornerRadius:   4,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Cursor:         core.CursorPointer,
							Hover:          &core.Style{Background: specHoverOverlay(p)},
							FocusVisible:   p.FocusRing(-2),
						},
						OnPress: func(core.PressEvent) {
							ed.RemoveComposerChip(chip.ID)
							inputRef.Focus()
						},
						Children: []core.Node{
							specIcon(lucide.X, 10, p.TextMuted),
						},
					},
				},
			})
		}
		if hasSuggested {
			sug := suggested
			chipNodes = append(chipNodes, base.Button{
				TestID: "agent-chip-suggested",
				Label:  ed.T("composer.addSuggested", l10n.Param("name", sug.Name)),
				Style: core.Style{
					Height:       core.Pt(24),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace4,
					Padding:      core.Symmetric(0, specSpace8),
					CornerRadius: specRadiusControl,
					Background:   core.Transparent,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Cursor:       core.CursorPointer,
					Hover:        &core.Style{Background: specHoverOverlay(p)},
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.AcceptSuggestedChip()
					inputRef.Focus()
				},
				Children: []core.Node{
					specIcon(lucide.Plus, 10, p.TextMuted),
					core.Text{
						Content:  sug.Name + " · " + ed.T("composer.selected"),
						MaxLines: 1,
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextMuted,
						},
					},
				},
			})
		}

		chipNodes = append(chipNodes, agentAttachedImageChipsView(ed, p)...)

		var alwaysNodes []core.Node
		alwaysNodes = append(alwaysNodes, core.Text{
			Content: ed.T("composer.always"),
			Style: core.Style{
				FontSize: 11,
				Color:    p.TextFaint,
				Margin:   core.Edges{Right: specSpace4},
			},
		})
		renderedBrief := false
		for _, a := range always {
			ac := a
			if ac.Name == "brief.yaml" {
				if briefChip := agentBriefAlwaysChipView(ed, p, ac.ID); briefChip != nil {
					alwaysNodes = append(alwaysNodes, briefChip)
					renderedBrief = true
					continue
				}
			}
			hovered := ed.HoveredChipID() == ac.ID
			bCol := p.LineRaised
			if hovered {
				bCol = p.Accent.Edge
			}
			alwaysNodes = append(alwaysNodes, core.View{
				TestID: "agent-chip-always-" + sanitizeChipTestID(ac.Name),
				OnMouseEnter: func(core.MouseEvent) {
					ed.SetHoveredChip(ac.ID)
				},
				OnMouseLeave: func(core.MouseEvent) {
					if ed.HoveredChipID() == ac.ID {
						ed.SetHoveredChip("")
					}
				},
				Tooltip: specTooltipNode(p, ed.T("composer.alwaysTooltip")),
				Style: core.Style{
					Height:       core.Pt(22),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          specSpace4,
					Padding:      core.Symmetric(0, specSpace8),
					CornerRadius: specRadiusControl,
					BorderWidth:  1,
					BorderColor:  bCol,
				},
				Children: []core.Node{
					specIcon(lucide.Lock, 10, p.TextFaint),
					core.Text{
						Content: ac.Name,
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextFaint,
						},
					},
				},
			})
		}
		if !renderedBrief {
			if briefChip := agentBriefAlwaysChipView(ed, p, ""); briefChip != nil {
				alwaysNodes = append(alwaysNodes, briefChip)
			}
		}

		vpBg := core.Transparent
		vpBorder := p.LineRaised
		vpText := p.TextFaint
		if vpOn {
			vpText = p.TextMuted
			if ed.HoveredChipID() == viewportImageChipID {
				vpBorder = p.Accent.Edge
			}
		} else {
			vpBg = p.Inset
		}
		alwaysNodes = append(alwaysNodes, core.View{
			OnMouseEnter: func(core.MouseEvent) {
				if ed.ViewportImageIncluded() {
					ed.SetHoveredChip(viewportImageChipID)
				}
			},
			OnMouseLeave: func(core.MouseEvent) {
				if ed.HoveredChipID() == viewportImageChipID {
					ed.SetHoveredChip("")
				}
			},
			Children: []core.Node{
				base.Toggle{
					TestID:  "agent-chip-viewport-image",
					Label:   ed.T("composer.viewportImageToggle"),
					Pressed: ed.ViewportImageIncluded,
					OnChange: func(on bool) {
						ed.SetViewportImageIncluded(on)
					},
					Style: core.Style{
						Height:       core.Pt(22),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          specSpace4,
						Padding:      core.Symmetric(0, specSpace8),
						CornerRadius: specRadiusControl,
						Background:   vpBg,
						BorderWidth:  1,
						BorderColor:  vpBorder,
						Cursor:       core.CursorPointer,
						Hover:        &core.Style{Background: specHoverOverlay(p)},
						FocusVisible: p.FocusRing(-2),
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("composer.viewportImage"),
							Style: core.Style{
								FontSize: 11.5,
								Color:    vpText,
							},
						},
					},
				},
			},
		})

		var rows []core.Node
		if len(chipNodes) > 0 {
			rows = append(rows, core.View{
				TestID: "agent-composer-chips",
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					Gap:        specSpace4,
				},
				Children: chipNodes,
			})
		}
		rows = append(rows, core.View{
			TestID: "agent-composer-always",
			Style: core.Style{
				Direction:  core.Row,
				Wrap:       core.Wrap,
				AlignItems: core.AlignCenter,
				Gap:        specSpace4,
			},
			Children: alwaysNodes,
		})
		return core.View{
			Style:    core.Style{Gap: specSpace8},
			Children: rows,
		}
	})

	// Action bar (@, context meter ring, Send) below the stable Input.
	actionsView := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = actionsKey.Get()
		p := ed.SpecPalette(cx)
		pickerOpen := ed.MentionPickerOpen()
		breakdownOpen := ed.ContextBreakdownOpen()
		meter := ed.ContextMeter()

		atBg := core.Transparent
		atFg := p.TextMuted
		if pickerOpen {
			atBg = p.LineRaised
			atFg = p.TextPrimary
		}

		ringBtnBg := core.Transparent
		ringBtnHoverBg := p.LineRaised
		ringBtnBorder := core.Transparent
		var ringBtnBorderW core.Points
		ringTextCol := p.TextMuted
		switch meter.Level {
		case ContextMeterWarning:
			ringTextCol = p.Changed.Fg
		case ContextMeterCritical:
			ringBtnBg = p.Warning.Fill
			ringBtnHoverBg = p.Warning.Fill
			ringBtnBorder = p.Warning.Edge
			ringBtnBorderW = 1
			ringTextCol = p.Warning.Fg
		}
		if breakdownOpen && meter.Level != ContextMeterCritical {
			ringBtnBg = p.LineRaised
		}

		ringLabelText := fmt.Sprintf("%d%%", meter.Percent)
		if meter.Level == ContextMeterCritical {
			ringLabelText = fmt.Sprintf("%d%% · %s", meter.Percent, ed.T("composer.compactNow"))
		}

		hasDraft := strings.TrimSpace(ed.ComposerDraft()) != ""
		sendBg := p.LineRaised
		sendFg := p.TextFaint
		if hasDraft {
			sendBg = p.TextPrimary
			sendFg = p.OnTextPrimary
		}

		var actionBtn core.Node
		if ed.AgentWorking() {
			stopLabel := ed.T("toolbar.stop")
			actionBtn = base.Button{
				TestID: "agent-composer-stop",
				Label:  stopLabel,
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Height:         core.Pt(specButtonHeight),
					Padding:        core.Symmetric(0, specSpace12),
					Gap:            6,
					CornerRadius:   specRadiusButton,
					Background:     p.LineRaised,
					Cursor:         core.CursorPointer,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.StopAgentRun()
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
						Content: stopLabel,
						Style: core.Style{
							FontSize:   12,
							FontWeight: core.FontWeightMedium,
							Color:      p.TextPrimary,
						},
					},
				},
			}
		} else if ed.AgentOffline() {
			actionBtn = base.Button{
				TestID: "agent-composer-queue",
				Label:  "Queue",
				Style: core.Style{
					Height:         core.Pt(specButtonHeight),
					Padding:        core.Symmetric(0, specSpace12),
					CornerRadius:   specRadiusButton,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     p.LineRaised,
					Cursor:         core.CursorPointer,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.QueueAgentRequest("")
				},
				Children: []core.Node{
					core.Text{
						Content: "Queue",
						Style: core.Style{
							FontSize:   12.5,
							FontWeight: core.FontWeightMedium,
							Color:      p.TextPrimary,
						},
					},
				},
			}
		} else {
			actionBtn = base.Button{
				TestID:  "agent-composer-send",
				Label:   ed.T("composer.send"),
				Tooltip: specTooltipNode(p, specShortcutTip(ed.T("composer.send"), FormatShortcut("mod+enter", cx.Env().OS()))),
				Disabled: func() bool {
					return !hasDraft
				},
				Style: core.Style{
					Width:          core.Pt(specButtonHeight),
					Height:         core.Pt(specButtonHeight),
					CornerRadius:   specRadiusButton,
					Background:     sendBg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Cursor:         core.CursorPointer,
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.submitComposerFromUI()
				},
				Children: []core.Node{
					specIcon(lucide.ArrowUp, 14, sendFg),
				},
			}
		}

		return core.View{
			TestID: "agent-composer-actions",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        specSpace4,
			},
			Children: []core.Node{
				base.Button{
					TestID:  "agent-composer-at-btn",
					Label:   ed.T("composer.mentionLabel"),
					Tooltip: specTooltipNode(p, ed.T("composer.mentionTip")),
					Style: core.Style{
						Width:          core.Pt(specRowHeight),
						Height:         core.Pt(specRowHeight),
						CornerRadius:   specRadiusControl,
						Background:     atBg,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Cursor:         core.CursorPointer,
						Hover:          &core.Style{Background: p.LineRaised},
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.ToggleMentionPicker()
						inputRef.Focus()
					},
					Children: []core.Node{
						specIcon(lucide.AtSign, specIconSize, atFg),
					},
				},
				agentAttachImageButtonView(ed, p),
				core.View{Style: core.Style{FlexGrow: 1}},
				base.Button{
					TestID:  "agent-context-ring-btn",
					Label:   ed.T("composer.contextRingLabel", l10n.Param("pct", meter.Percent)),
					Tooltip: specTooltipNode(p, ed.T("composer.contextRingTip", l10n.Param("pct", meter.Percent))),
					Style: core.Style{
						Height:       core.Pt(specRowHeight),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          specSpace4,
						Padding:      core.Edges{Left: specSpace4, Right: specSpace8},
						CornerRadius: specRadiusControl,
						Background:   ringBtnBg,
						BorderWidth:  ringBtnBorderW,
						BorderColor:  ringBtnBorder,
						Cursor:       core.CursorPointer,
						Hover:        &core.Style{Background: ringBtnHoverBg},
						FocusVisible: p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						ed.ToggleContextBreakdown()
					},
					Children: []core.Node{
						contextRingCanvas(p, meter.Percent, meter.Level),
						core.Text{
							TestID:  "agent-context-ring-pct",
							Content: ringLabelText,
							Style: core.Style{
								FontSize:   11,
								FontFamily: core.FontMonospace,
								Color:      ringTextCol,
							},
						},
					},
				},
				actionBtn,
			},
		}
	})

	// Stably mounted composer input so typing never remounts the field or drops focus.
	composerInput := core.Input{
		Ref:    inputRef,
		TestID: "agent-composer-input",
		LabelFunc: func() string {
			_ = rev.Get()
			if ed.AgentOffline() {
				return "Queue a request"
			}
			return ed.T("composer.inputLabel")
		},
		PlaceholderFunc: func() string {
			_ = rev.Get()
			if ed.AgentOffline() {
				return "Queue a request…"
			}
			return ed.T("composer.placeholder")
		},
		Multiline:      true,
		SubmitBehavior: core.SubmitBehaviorSubmit,
		ReturnKeyType:  core.ReturnKeySend,
		Value: func() string {
			_ = rev.Get()
			return ed.ComposerDraft()
		},
		OnFocus: func() {
			composerFocused.Set(true)
			if !textInputFocused {
				textInputFocused = true
				ed.noteTextInputFocus(true)
			}
		},
		OnBlur: func() {
			composerFocused.Set(false)
			if textInputFocused {
				textInputFocused = false
				ed.noteTextInputFocus(false)
			}
		},
		OnChange: func(v string) {
			ed.updateComposerDraftFromInput(v)
		},
		OnPaste: func(ev core.PasteEvent) {
			_, _, err := ed.HandleAgentComposerPaste(ev)
			ed.report(err)
		},
		OnSubmit: func(string) {
			if suppressSubmit {
				suppressSubmit = false
				return
			}
			if ed.composerKeysBlocked() {
				return
			}
			if ed.MentionPickerOpen() {
				ed.AcceptActiveMention()
				inputRef.Focus()
				return
			}
			ed.submitComposerFromUI()
		},
		StyleFunc: func() core.Style {
			_ = rev.Get()
			p := ed.SpecPalette(cx)
			return core.Style{
				Width:            core.Pct(100),
				MinHeight:        core.Pt(40),
				MaxHeight:        core.Pt(96),
				FontSize:         12.5,
				Color:            p.TextPrimary,
				PlaceholderColor: p.TextFaint,
				Background:       core.Transparent,
				BorderWidth:      0,
				CaretColor:       p.Accent.Edge,
			}
		},
	}

	composerCard := core.View{
		TestID: "agent-composer-card",
		StyleFunc: func() core.Style {
			_ = rev.Get()
			p := ed.SpecPalette(cx)
			return core.Style{
				CornerRadius: specRadiusCard,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Padding:      core.Symmetric(specSpace8, specSpace12),
				Gap:          specSpace8,
				FocusWithin:  p.FocusRing(2),
			}
		},
		Children: []core.Node{
			chipRowsView,
			composerInput,
			actionsView,
		},
	}

	composerHint := core.Text{
		TestID: "agent-composer-hint",
		ContentFunc: func() string {
			_ = rev.Get()
			if ed.HasPendingAgentTurn() {
				return ed.T("agentTurn.composerHintPending")
			}
			return ed.T("agent.composerHint")
		},
		StyleFunc: func() core.Style {
			_ = rev.Get()
			p := ed.SpecPalette(cx)
			return core.Style{
				FontSize: 11,
				Color:    p.TextFaint,
				Padding:  core.Symmetric(0, 2),
			}
		},
	}

	return core.View{
		TestID: "agent-composer",
		OnFileDragEnter: func() {
			ed.SetAgentComposerDragOver(true)
		},
		OnFileDragLeave: func() {
			ed.SetAgentComposerDragOver(false)
		},
		OnFileDrop: func(uris []string) {
			_, err := ed.DropAgentReferenceFiles(uris)
			ed.report(err)
		},
		StyleFunc: func() core.Style {
			_ = rev.Get()
			p := ed.SpecPalette(cx)
			return core.Style{
				FlexShrink:  0,
				Padding:     core.All(specSpace12),
				Gap:         specSpace8,
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
				Background:  p.Panel,
			}
		},
		Children: []core.Node{
			pickerKeyScope,
			breakdownKeyScope,
			composerFocusedKeyScope,
			composerEmptyKeyScope,
			popoversView,
			composerCard,
			composerHint,
		},
	}
}

const maxMentionPickerRenderedRows = 200

func mentionPickerView(p SpecPalette, ed *Editor, inputRef *core.Ref) core.Node {
	items := ed.MentionPickerItems()
	activeGroup := ed.MentionPickerGroup()
	activeIdx := ed.MentionPickerActiveIndex()
	query := ed.MentionPickerQuery()

	matchText := ed.T("composer.matchCount", l10n.Param("count", len(items)))
	if len(items) == 1 {
		matchText = ed.T("composer.matchCountOne")
	}

	var tabNodes []core.Node
	for _, g := range mentionGroupOrder {
		grp := g
		selected := grp == activeGroup
		bg := p.LineRaised
		fg := p.TextMuted
		weight := core.FontWeightNormal
		if selected {
			bg = p.TextPrimary
			fg = p.OnTextPrimary
			weight = core.FontWeightMedium
		}
		tabNodes = append(tabNodes, base.Button{
			TestID: "agent-mention-tab-" + string(grp),
			Label:  mentionGroupLabel(ed, grp),
			Style: core.Style{
				Height:         core.Pt(22),
				Padding:        core.Symmetric(0, specSpace8),
				CornerRadius:   specRadiusControl,
				Background:     bg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Cursor:         core.CursorPointer,
				FocusVisible:   p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.SetMentionPickerGroup(grp)
				if inputRef != nil {
					inputRef.Focus()
				}
			},
			Children: []core.Node{
				core.Text{
					Content: mentionGroupLabel(ed, grp),
					Style: core.Style{
						FontSize:   11.5,
						FontWeight: weight,
						Color:      fg,
					},
				},
			},
		})
	}

	renderedLimit := min(len(items), maxMentionPickerRenderedRows)
	var rowNodes []core.Node
	var lastGroup MentionGroup
	for i := 0; i < renderedLimit; i++ {
		item := items[i]
		if item.Group != lastGroup {
			lastGroup = item.Group
			rowNodes = append(rowNodes, core.View{
				Style: core.Style{
					Padding: core.Edges{Top: specSpace8, Right: specSpace8, Bottom: specSpace4, Left: specSpace8},
				},
				Children: []core.Node{
					specSectionLabel(p, mentionSectionHeading(ed, item.Group)),
				},
			})
		}
		isActive := i == activeIdx
		rowBg := core.Transparent
		rowBorder := core.Transparent
		var rowBorderW core.Points
		if isActive {
			rowBg = p.Accent.Fill
			rowBorder = p.Accent.Edge
			rowBorderW = 1
		}
		rowNodes = append(rowNodes, base.Button{
			TestID: fmt.Sprintf("agent-mention-item-%d", i),
			Label:  item.Name + " " + item.Meta,
			Style: core.Style{
				Width:        core.Pct(100),
				Height:       core.Pt(40),
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          specSpace8,
				Padding:      core.Symmetric(0, specSpace8),
				CornerRadius: specRadiusControl,
				Background:   rowBg,
				BorderWidth:  rowBorderW,
				BorderColor:  rowBorder,
				Cursor:       core.CursorPointer,
				Hover:        &core.Style{Background: p.Accent.Fill},
				FocusVisible: p.FocusRing(-2),
			},
			OnPress: func(core.PressEvent) {
				ed.AcceptMentionItem(item.ID)
				if inputRef != nil {
					inputRef.Focus()
				}
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:          core.Pt(24),
						Height:         core.Pt(24),
						FlexShrink:     0,
						CornerRadius:   specRadiusControl,
						Background:     p.Panel,
						BorderWidth:    1,
						BorderColor:    p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{mentionIconNode(p, item.Icon)},
				},
				core.View{
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						Gap:        2,
					},
					Children: []core.Node{
						core.Text{
							Content:  item.Name,
							MaxLines: 1,
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextPrimary,
							},
						},
						core.Text{
							Content:  item.Meta,
							MaxLines: 1,
							Style: core.Style{
								FontSize: 11.5,
								Color:    p.TextFaint,
							},
						},
					},
				},
			},
		})
	}
	if len(rowNodes) == 0 {
		rowNodes = append(rowNodes, core.View{
			TestID: "agent-mention-empty",
			Style: core.Style{
				Padding:    core.All(specSpace16),
				AlignItems: core.AlignCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("composer.noMatches"),
					Style:   core.Style{FontSize: 12, Color: p.TextFaint},
				},
			},
		})
	}

	hintPair := func(keyText, labelText string) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        specSpace4,
			},
			Children: []core.Node{
				core.Text{
					Content: keyText,
					Style: core.Style{
						FontSize:   11,
						FontFamily: core.FontMonospace,
						Color:      p.TextMuted,
					},
				},
				core.Text{
					Content: labelText,
					Style: core.Style{
						FontSize: 11,
						Color:    p.TextFaint,
					},
				},
			},
		}
	}

	return core.View{
		TestID: "agent-mention-picker",
		Role:   core.RoleRegion,
		Label:  ed.T("composer.mentionPickerLabel"),
		Style: core.Style{
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Overflow:     core.Hidden,
		},
		Children: []core.Node{
			// Query + match count header (36 pt)
			core.View{
				Style: core.Style{
					Height:      core.Pt(36),
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         specSpace8,
					Padding:     core.Symmetric(0, specSpace12),
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: "@",
						Style: core.Style{
							FontSize:   11.5,
							FontFamily: core.FontMonospace,
							Color:      p.TextFaint,
						},
					},
					core.Text{
						TestID:  "agent-mention-query",
						Content: query,
						Style: core.Style{
							FontSize:   11.5,
							FontFamily: core.FontMonospace,
							Color:      p.TextPrimary,
						},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					core.Text{
						TestID:  "agent-mention-count",
						Content: matchText,
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextFaint,
						},
					},
				},
			},
			// Group tabs
			core.View{
				TestID: "agent-mention-tabs",
				Style: core.Style{
					Direction:   core.Row,
					Wrap:        core.Wrap,
					Gap:         specSpace4,
					Padding:     core.Symmetric(specSpace8, specSpace12),
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.LineRaised,
				},
				Children: tabNodes,
			},
			// Items list
			core.Scroll{
				TestID: "agent-mention-list",
				Style: core.Style{
					Height: core.Pt(236),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Padding: core.All(specSpace4),
						},
						Children: rowNodes,
					},
				},
			},
			// Keyboard hints footer (30 pt)
			core.View{
				Style: core.Style{
					Height:      core.Pt(30),
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         specSpace12,
					Padding:     core.Symmetric(0, specSpace12),
					Borders:     core.Edges{Top: 1},
					BorderColor: p.LineRaised,
				},
				Children: []core.Node{
					hintPair("↑↓", ed.T("composer.hintMove")),
					hintPair("↵", ed.T("composer.hintAdd")),
					hintPair("Tab", ed.T("composer.hintNextGroup")),
					hintPair("Esc", ed.T("composer.hintClose")),
				},
			},
		},
	}
}

func chipHoverPreviewView(p SpecPalette, payload AgentChipPayload) core.Node {
	var bodyNodes []core.Node
	if payload.Header != "" {
		bodyNodes = append(bodyNodes, core.Text{
			TestID:  "agent-chip-hover-header",
			Content: payload.Header,
			Style: core.Style{
				FontSize:   11.5,
				FontFamily: core.FontMonospace,
				Color:      p.TextFaint,
			},
		})
	}
	if payload.ImageSrc.Pixels != nil {
		bodyNodes = append(bodyNodes, core.Image{
			TestID:     "agent-chip-hover-image",
			Source:     payload.ImageSrc,
			ResizeMode: core.Contain,
			Style: core.Style{
				Width:        core.Pct(100),
				Height:       core.Pt(112),
				CornerRadius: specRadiusControl,
				Background:   p.Inset,
			},
		})
	}
	if payload.Text != "" {
		bodyNodes = append(bodyNodes, core.Scroll{
			Style: core.Style{MaxHeight: core.Pt(168)},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-chip-hover-body",
					Content: payload.Text,
					Style: core.Style{
						FontSize:   11.5,
						FontFamily: core.FontMonospace,
						Color:      p.TextMuted,
					},
				},
			},
		})
	}
	return core.View{
		TestID: "agent-chip-hover-preview",
		Style: core.Style{
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.Symmetric(specSpace8, specSpace12),
			Gap:          specSpace4,
		},
		Children: bodyNodes,
	}
}

func contextBreakdownView(p SpecPalette, ed *Editor) core.Node {
	meter := ed.ContextMeter()
	titleText := ed.T("composer.contextUsedTitle", l10n.Param("pct", meter.Percent))
	tokensText := ed.T("composer.contextTokensOf",
		l10n.Param("used", formatTokenCountShort(meter.UsedTokens)),
		l10n.Param("limit", formatTokenCountShort(meter.LimitTokens)),
	)

	var barRows []core.Node
	for _, r := range meter.Rows {
		barColor := p.TextMuted
		if r.Highlight {
			barColor = p.Changed.Fg
		}
		barRows = append(barRows, core.View{
			TestID: "agent-context-row-" + r.Key,
			Style: core.Style{
				Gap: specSpace4,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						JustifyContent: core.JustifySpaceBetween,
						AlignItems:     core.AlignCenter,
						Gap:            specSpace8,
					},
					Children: []core.Node{
						core.Text{
							Content:  r.Label,
							MaxLines: 1,
							Style: core.Style{
								FlexGrow:   1,
								FlexShrink: 1,
								FontSize:   11.5,
								Color:      p.TextSecondary,
							},
						},
						core.Text{
							TestID:  "agent-context-row-pct-" + r.Key,
							Content: fmt.Sprintf("%d%%", r.Percent),
							Style: core.Style{
								FontSize:   11.5,
								FontFamily: core.FontMonospace,
								Color:      p.TextFaint,
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Height:       core.Pt(4),
						CornerRadius: 2,
						Background:   p.LineRaised,
						Overflow:     core.Hidden,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pct(float32(max(0, min(100, r.Percent)))),
								Height:       core.Pt(4),
								CornerRadius: 2,
								Background:   barColor,
							},
						},
					},
				},
			},
		})
	}

	canCompact := meter.CompactableRequests > 0 || (ed.agentComposer.reportedUsage != nil && ed.agentComposer.reportedUsage.Conversation > 0)
	caption := ed.T("composer.compactCaption", l10n.Param("count", max(1, meter.CompactableRequests)))

	return core.View{
		TestID: "agent-context-breakdown",
		Role:   core.RoleRegion,
		Label:  titleText,
		Style: core.Style{
			CornerRadius: specRadiusCard,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(specSpace12),
			Gap:          specSpace12,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					JustifyContent: core.JustifySpaceBetween,
					AlignItems:     core.AlignBaseline,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "agent-context-breakdown-title",
						Content: titleText,
						Style: core.Style{
							FontSize:   14,
							FontWeight: core.FontWeightSemibold,
							Color:      p.TextPrimary,
						},
					},
					core.Text{
						TestID:  "agent-context-breakdown-tokens",
						Content: tokensText,
						Style: core.Style{
							FontSize:   11.5,
							FontFamily: core.FontMonospace,
							Color:      p.TextFaint,
						},
					},
				},
			},
			core.View{
				Style:    core.Style{Gap: specSpace8},
				Children: barRows,
			},
			core.View{
				Style: core.Style{
					Direction: core.Row,
					Gap:       specSpace8,
				},
				Children: []core.Node{
					specPanelButton(p, specPanelButtonSpec{
						TestID:   "agent-context-compact",
						Text:     ed.T("composer.compactThread"),
						Variant:  specBtnPrimary,
						Disabled: func() bool { return !canCompact },
						Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
						OnPress: func() {
							_, err := ed.CompactAgentThread()
							ed.report(err)
						},
					}),
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "agent-context-start-fresh",
						Text:    ed.T("composer.startFresh"),
						Variant: specBtnSecondary,
						Style:   core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
						OnPress: func() {
							_, err := ed.StartFreshAgentThread()
							ed.report(err)
						},
					}),
				},
			},
			core.Text{
				Content: caption,
				Style: core.Style{
					FontSize: 11.5,
					Color:    p.TextFaint,
				},
			},
		},
	}
}

// contextRingCanvas draws a 20×20 circular progress ring matching Threads.dc.html using SpecPalette tokens.
func contextRingCanvas(p SpecPalette, pct int, level ContextMeterLevel) core.Node {
	trackCol := p.LineRaised
	arcCol := p.TextMuted
	switch level {
	case ContextMeterWarning:
		arcCol = p.Changed.Fg
	case ContextMeterCritical:
		trackCol = p.Warning.Edge
		arcCol = p.Warning.Fg
	}
	clamped := max(0, min(100, pct))
	return core.Canvas{
		TestID: "agent-context-ring-canvas",
		Style: core.Style{
			Width:      core.Pt(20),
			Height:     core.Pt(20),
			FlexShrink: 0,
		},
		Draw: func(d *core.Drawing, size core.Size) {
			cx := float32(size.Width) / 2
			cy := float32(size.Height) / 2
			r := float32(7)
			if minSide := min(float32(size.Width), float32(size.Height)); minSide > 0 && r > minSide/2-2 {
				r = minSide/2 - 2
			}
			drawRingArc(d, cx, cy, r, 1.0, trackCol, 2.5)
			if clamped > 0 {
				drawRingArc(d, cx, cy, r, float64(clamped)/100.0, arcCol, 2.5)
			}
		},
	}
}

func drawRingArc(d *core.Drawing, cx, cy, r float32, frac float64, col core.Color, strokeWidth float32) {
	if d == nil || r <= 0 || frac <= 0 {
		return
	}
	if frac > 1 {
		frac = 1
	}
	const totalSegs = 48
	segs := max(2, int(math.Ceil(totalSegs*frac)))
	startAngle := -math.Pi / 2
	endAngle := startAngle + 2*math.Pi*frac

	d.Save()
	d.SetStrokeColor(col)
	d.SetLineWidth(strokeWidth)
	d.SetLineCap(core.CapRound)
	d.BeginPath()
	for i := 0; i <= segs; i++ {
		t := float64(i) / float64(segs)
		a := startAngle + (endAngle-startAngle)*t
		x := cx + r*float32(math.Cos(a))
		y := cy + r*float32(math.Sin(a))
		if i == 0 {
			d.MoveTo(x, y)
		} else {
			d.LineTo(x, y)
		}
	}
	d.Stroke()
	d.Restore()
}

func chipDotColor(p SpecPalette, role string) core.Color {
	switch role {
	case "added":
		return p.Added.Fg
	case "changed":
		return p.Changed.Fg
	case "warning":
		return p.Warning.Fg
	case "removed":
		return p.Removed.Fg
	case "agent":
		return p.Agent.Fg
	case "accent":
		return p.Accent.Fg
	default:
		return p.TextMuted
	}
}

func mentionIconNode(p SpecPalette, kind MentionIconKind) core.Node {
	switch kind {
	case MentionIconObject:
		return specIcon(lucide.Square, 13, p.TextMuted)
	case MentionIconPrefab:
		return specIcon(lucide.Package, 13, p.Agent.Fg)
	case MentionIconImage:
		return specIcon(lucide.Image, 13, p.Added.Fg)
	case MentionIconSound:
		return specIcon(lucide.AudioLines, 13, p.TextMuted)
	case MentionIconProblem:
		return specIcon(lucide.TriangleAlert, 13, p.Warning.Fg)
	case MentionIconPlaytest:
		return specIcon(lucide.Play, 13, p.TextMuted)
	case MentionIconHistory:
		return specIcon(lucide.Clock, 13, p.TextMuted)
	case MentionIconRule:
		return specIcon(lucide.FileText, 13, p.TextMuted)
	default:
		return specIcon(lucide.FileCode, 13, p.TextMuted)
	}
}

func mentionGroupLabel(ed *Editor, g MentionGroup) string {
	switch g {
	case MentionGroupAll:
		return ed.T("composer.group.all")
	case MentionGroupObjects:
		return ed.T("composer.group.objects")
	case MentionGroupFiles:
		return ed.T("composer.group.files")
	case MentionGroupPrefabs:
		return ed.T("composer.group.prefabs")
	case MentionGroupAssets:
		return ed.T("composer.group.assets")
	case MentionGroupProblems:
		return ed.T("composer.group.problems")
	case MentionGroupPlaytests:
		return ed.T("composer.group.playtests")
	case MentionGroupHistory:
		return ed.T("composer.group.history")
	case MentionGroupRules:
		return ed.T("composer.group.rules")
	default:
		return string(g)
	}
}

func mentionSectionHeading(ed *Editor, g MentionGroup) string {
	switch g {
	case MentionGroupObjects:
		return ed.T("composer.section.objects")
	case MentionGroupFiles:
		return ed.T("composer.section.files")
	case MentionGroupPrefabs:
		return ed.T("composer.section.prefabs")
	case MentionGroupAssets:
		return ed.T("composer.section.assets")
	case MentionGroupProblems:
		return ed.T("composer.section.problems")
	case MentionGroupPlaytests:
		return ed.T("composer.section.playtests")
	case MentionGroupHistory:
		return ed.T("composer.section.history")
	case MentionGroupRules:
		return ed.T("composer.section.rules")
	default:
		return strings.ToUpper(string(g))
	}
}

func sanitizeChipTestID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
