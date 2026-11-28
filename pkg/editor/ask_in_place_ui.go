package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// AskInPlaceComposerView renders the anchored ⌘L in-place agent composer inside the editor viewport
// using exclusively theme.Palette tokens (matching design/editor/AskInPlace.dc.html and Spec.dc.html).
func AskInPlaceComposerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	openMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.AskInPlaceOpen()
	})
	return core.Show{
		When: openMemo.Get,
		Then: func(cx *core.Scope) core.Node {
			var inputRef core.Ref
			var closeRef core.Ref
			var sendRef core.Ref
			var cardRef core.Ref
			var sugRefs [2]core.Ref
			inputFocused := core.NewSignal(cx, false)

			stepFocus := func(delta int) {
				if ed.askInPlaceKeyBlocked() {
					return
				}
				stops := make([]*core.Ref, 0, 5)
				if refMounted(&inputRef) {
					stops = append(stops, &inputRef)
				}
				nSug := min(len(ed.askInPlace.suggestions), len(sugRefs))
				for i := 0; i < nSug; i++ {
					if refMounted(&sugRefs[i]) {
						stops = append(stops, &sugRefs[i])
					}
				}
				if refMounted(&sendRef) {
					stops = append(stops, &sendRef)
				}
				if refMounted(&closeRef) {
					stops = append(stops, &closeRef)
				}
				if len(stops) == 0 {
					return
				}
				cur := -1
				for i, r := range stops {
					if r.HoldsFocus() {
						cur = i
						break
					}
				}
				next := 0
				if cur >= 0 {
					next = (cur + delta + len(stops)) % len(stops)
				} else if delta < 0 {
					next = len(stops) - 1
				}
				stops[next].Focus()
			}

			// Bind composer keys guarded against shortcut capture / higher-priority modals so Esc closes
			// cleanly and returns focus, Tab/Shift+Tab stay trapped inside the card, ↵ / ⌘↵ send only when
			// the input holds focus (never hijacking Close or Suggestion buttons), ⇧↵ inserts a newline,
			// and Up/Down navigate suggestions without blocking typing.
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.askInPlaceKeyBlocked() {
					return
				}
				ed.CloseAskInPlace()
			})
			cx.OnKeyWith("tab", core.KeyOptions{When: core.KeyAlways}, func() {
				stepFocus(1)
			})
			cx.OnKeyWith("shift+tab", core.KeyOptions{When: core.KeyAlways}, func() {
				stepFocus(-1)
			})
			cx.OnKeyWith("mod+enter", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.askInPlaceKeyBlocked() || !ed.AskInPlaceOpen() {
					return
				}
				if _, err := ed.SubmitAskInPlace(); err != nil {
					ed.report(err)
				}
			})
			cx.OnKeyWith("shift+enter", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.askInPlaceKeyBlocked() || !inputFocused.Get() {
					return
				}
				ed.InsertAskInPlaceNewline()
			})
			cx.OnKeyWith("enter", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.askInPlaceKeyBlocked() || !ed.AskInPlaceOpen() || !inputFocused.Get() {
					return
				}
				if _, err := ed.SubmitAskInPlace(); err != nil {
					ed.report(err)
				}
			})
			cx.OnKeyWith("down", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.askInPlaceKeyBlocked() || !inputFocused.Get() {
					return
				}
				if !strings.Contains(ed.askInPlace.draft, "\n") || ed.askInPlace.selectedSuggestion >= 0 {
					ed.StepAskInPlaceSuggestion(1)
				}
			})
			cx.OnKeyWith("up", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.askInPlaceKeyBlocked() || !inputFocused.Get() {
					return
				}
				if !strings.Contains(ed.askInPlace.draft, "\n") || ed.askInPlace.selectedSuggestion >= 0 {
					ed.StepAskInPlaceSuggestion(-1)
				}
			})

			cx.Effect(func() {
				_ = openMemo.Get()
				inputRef.Focus()
			})

			type chromeKey struct {
				dark bool
				lang string
				seq  uint64
			}
			// Keep ui.Input mounted across selection-highlight and layout updates so typing is never interrupted.
			cardKey := core.NewMemo(cx, func() chromeKey {
				_ = rev.Get()
				return chromeKey{
					dark: ed.Palette(cx).Dark,
					lang: ed.ResolvedLanguage(),
					seq:  ed.askInPlace.seq,
				}
			})

			type suggestKey struct {
				dark   bool
				lang   string
				seq    uint64
				selIdx int
			}
			suggestionsKey := core.NewMemo(cx, func() suggestKey {
				_ = rev.Get()
				return suggestKey{
					dark:   ed.Palette(cx).Dark,
					lang:   ed.ResolvedLanguage(),
					seq:    ed.askInPlace.seq,
					selIdx: ed.AskInPlaceSelectedSuggestion(),
				}
			})

			return core.Dynamic(func(cx *core.Scope) core.Node {
				k := cardKey.Get()
				p := theme.PaletteFor(k.dark)
				snap := ed.AskInPlaceSnapshot()
				targetOS := cx.Env().OS()

				shadowAlpha := float32(0.55)
				if !p.Dark {
					shadowAlpha = 0.16
				}

				// Build the announced selection chip row (count pill + individual object chips).
				chipChildren := make([]core.Node, 0, len(snap.TargetNames)+3)
				if len(snap.TargetIDs) == 0 {
					chipChildren = append(chipChildren, core.View{
						TestID: "ask-in-place-scene-pill",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          6,
							Height:       core.Pt(22),
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 6,
							Background:   p.Inset,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
						},
						Children: []core.Node{
							ui.Icon(lucide.Layers, 11, func() core.Color { return p.Accent.Fg }),
							core.Text{
								Content:  snap.ChipSummary,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   11.5,
									FontWeight: core.FontWeightMedium,
									Color:      p.TextMuted,
								},
							},
						},
					})
				} else {
					countText := ed.T("askInPlace.selectedCount", l10n.Param("count", fmt.Sprintf("%d", len(snap.TargetIDs))))
					chipChildren = append(chipChildren, core.View{
						TestID: "ask-in-place-count-badge",
						Style: core.Style{
							Height:         core.Pt(22),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Background:     p.Accent.Fg,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Text{
								Content:  countText,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   11,
									FontWeight: core.FontWeightSemibold,
									Color:      p.Accent.OnFill,
								},
							},
						},
					})
					const maxVisibleNames = 3
					shown := snap.TargetNames
					extra := 0
					if len(shown) > maxVisibleNames {
						extra = len(shown) - maxVisibleNames
						shown = shown[:maxVisibleNames]
					}
					for i, name := range shown {
						chipChildren = append(chipChildren, core.View{
							TestID: fmt.Sprintf("ask-in-place-target-%d", i),
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          5,
								Height:       core.Pt(22),
								Padding:      core.Symmetric(0, 8),
								CornerRadius: 6,
								Background:   p.Inset,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 2,
										Background:   p.Added.Fg,
										FlexShrink:   0,
									},
								},
								core.Text{
									Content:  name,
									MaxLines: 1,
									Style: core.Style{
										FontSize: 11.5,
										Color:    p.TextMuted,
									},
								},
							},
						})
					}
					if extra > 0 {
						chipChildren = append(chipChildren, core.Text{
							Content: fmt.Sprintf("+%d", extra),
							Style: core.Style{
								FontSize: 11,
								Color:    p.TextFaint,
							},
						})
					}
				}

				chipRow := core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            8,
					},
					Children: []core.Node{
						core.View{
							TestID:     "ask-in-place-selection-chip",
							Role:       core.RoleStatus,
							LiveRegion: core.LiveRegionPolite,
							Label:      snap.ChipAnnouncement,
							Style: core.Style{
								Direction:  core.Row,
								Wrap:       core.Wrap,
								AlignItems: core.AlignCenter,
								Gap:        6,
								FlexGrow:   1,
								FlexShrink: 1,
								MinWidth:   core.Pt(0),
							},
							Children: chipChildren,
						},
						core.View{
							TestID:    "ask-in-place-close",
							Ref:       &closeRef,
							Role:      core.RoleButton,
							Label:     ed.T("askInPlace.close"),
							Focusable: func() bool { return true },
							Style: core.Style{
								Width:          core.Pt(28),
								Height:         core.Pt(28),
								CornerRadius:   6,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FlexShrink:     0,
								FocusVisible:   p.FocusRing(2),
							},
							OnPress: func(core.PressEvent) {
								ed.CloseAskInPlace()
							},
							Children: []core.Node{
								ui.Icon(lucide.X, 14, func() core.Color { return p.TextFaint }),
							},
						},
					},
				}

				// Real labelled multi-line text field wrapped with specInputField so Editor.IsTyping stays
				// accurate and unmount-safe while typing and navigating suggestions.
				inputNode := specInputField(cx, ed, ui.Input{
					TestID:         "ask-in-place-input",
					Ref:            &inputRef,
					Label:          snap.InputLabel,
					Placeholder:    snap.Placeholder,
					Multiline:      true,
					AutoFocus:      true,
					SubmitBehavior: core.SubmitBehaviorSubmit,
					Value: func() string {
						_ = rev.Get()
						return ed.askInPlace.draft
					},
					OnFocus: func() {
						inputFocused.Set(true)
					},
					OnBlur: func() {
						inputFocused.Set(false)
					},
					OnChange: func(text string) {
						ed.SetAskInPlaceDraft(text)
					},
					OnSubmit: func(text string) {
						if !ed.AskInPlaceOpen() || ed.askInPlaceKeyBlocked() {
							return
						}
						if strings.TrimSpace(text) != "" && ed.askInPlace.draft != text {
							ed.SetAskInPlaceDraft(text)
						}
						if _, err := ed.SubmitAskInPlace(); err != nil {
							ed.report(err)
						}
					},
					Style: core.Style{
						Width:            core.Pct(100),
						MinHeight:        core.Pt(44),
						MaxHeight:        core.Pt(110),
						Padding:          core.Symmetric(6, 8),
						CornerRadius:     8,
						Background:       p.Inset,
						BorderWidth:      1,
						BorderColor:      p.LineRaised,
						Color:            p.TextSecondary,
						PlaceholderColor: p.TextFaint,
						SelectionColor:   p.Accent.Fg,
						FontSize:         13,
						FocusVisible:     p.FocusRing(-2),
					},
				})

				sendBtn := core.View{
					TestID:    "ask-in-place-send",
					Ref:       &sendRef,
					Role:      core.RoleButton,
					Label:     ed.T("askInPlace.send"),
					Focusable: func() bool { return true },
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Gap:            4,
						Height:         core.Pt(28),
						Padding:        core.Symmetric(0, 10),
						CornerRadius:   8,
						Background:     p.TextPrimary,
						FlexShrink:     0,
						FocusVisible:   p.FocusRing(2),
					},
					OnPress: func(core.PressEvent) {
						if _, err := ed.SubmitAskInPlace(); err != nil {
							ed.report(err)
						}
					},
					Children: []core.Node{
						core.Text{
							Content:  ed.T("askInPlace.send"),
							MaxLines: 1,
							Style: core.Style{
								FontSize:   11.5,
								FontWeight: core.FontWeightSemibold,
								Color:      p.OnTextPrimary,
							},
						},
						core.Text{
							Content: "↵",
							Style: core.Style{
								FontFamily: core.FontMonospace,
								FontSize:   11,
								Color:      p.OnTextPrimary,
							},
						},
					},
				}

				// Suggestions row re-renders only its chips when arrow selection changes, never remounting inputNode.
				suggestionsRow := core.View{
					TestID: "ask-in-place-suggestions",
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            8,
					},
					Children: []core.Node{
						core.Dynamic(func(cx *core.Scope) core.Node {
							sk := suggestionsKey.Get()
							sp := theme.PaletteFor(sk.dark)
							curSnap := ed.AskInPlaceSnapshot()
							footerChildren := make([]core.Node, 0, len(curSnap.Suggestions)+1)
							if len(curSnap.Suggestions) > 0 {
								footerChildren = append(footerChildren, core.Text{
									Content:  ed.T("askInPlace.suggested"),
									MaxLines: 1,
									Style: core.Style{
										FontSize:   11.5,
										Color:      sp.TextFaint,
										FlexShrink: 0,
									},
								})
								for i, s := range curSnap.Suggestions {
									idx := i
									active := sk.selIdx == idx
									chipBg := sp.Raised
									chipBorder := sp.LineRaised
									chipColor := sp.TextMuted
									if active {
										chipBg = sp.Accent.Fill
										chipBorder = sp.Accent.Edge
										chipColor = sp.TextPrimary
									}
									var sugRef *core.Ref
									if idx < len(sugRefs) {
										sugRef = &sugRefs[idx]
									}
									footerChildren = append(footerChildren, core.View{
										TestID:    fmt.Sprintf("ask-in-place-suggestion-%d", idx),
										Ref:       sugRef,
										Role:      core.RoleButton,
										Label:     s.Label,
										Focusable: func() bool { return true },
										Style: core.Style{
											Height:         core.Pt(28),
											Padding:        core.Symmetric(0, 10),
											CornerRadius:   12,
											Background:     chipBg,
											BorderWidth:    1,
											BorderColor:    chipBorder,
											AlignItems:     core.AlignCenter,
											JustifyContent: core.JustifyCenter,
											FocusVisible:   sp.FocusRing(2),
										},
										OnPress: func(core.PressEvent) {
											if _, err := ed.PickAskInPlaceSuggestion(idx); err != nil {
												ed.report(err)
											}
										},
										Children: []core.Node{
											core.Text{
												Content:  s.Label,
												MaxLines: 1,
												Style: core.Style{
													FontSize: 11.5,
													Color:    chipColor,
												},
											},
										},
									})
								}
							}
							return core.View{
								Style: core.Style{
									Direction:  core.Row,
									Wrap:       core.Wrap,
									AlignItems: core.AlignCenter,
									Gap:        6,
									FlexGrow:   1,
									FlexShrink: 1,
									MinWidth:   core.Pt(0),
								},
								Children: footerChildren,
							}
						}),
						sendBtn,
					},
				}

				askKey := FormatShortcut(ed.EffectiveShortcut(CmdAskAgent), targetOS)
				searchKey := FormatShortcut(ed.EffectiveShortcut(CmdCommandPalette), targetOS)
				hintText := ed.T("askInPlace.hint",
					l10n.Param("askKey", askKey),
					l10n.Param("searchKey", searchKey),
				)

				composerCard := core.View{
					TestID: "ask-in-place-composer",
					Ref:    &cardRef,
					Role:   core.RoleDialog,
					Label:  snap.InputLabel,
					OnLayout: func(b core.Bounds) {
						ed.setAskInPlaceMeasuredSize(float32(b.Size.Width), float32(b.Size.Height))
					},
					Style: core.Style{
						Position:     core.Absolute,
						Padding:      core.All(12),
						Gap:          8,
						CornerRadius: 12,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.Accent.Edge,
						Shadows: []core.BoxShadow{
							ui.Shadow(0, 16, 40, 0, ui.Black(shadowAlpha)),
						},
					},
					StyleFunc: func() core.Style {
						_ = rev.Get()
						pl := ed.AskInPlacePlacement()
						return core.Style{
							Left:  core.Pt(core.Points(pl.Left)),
							Top:   core.Pt(core.Points(pl.Top)),
							Width: core.Pt(core.Points(pl.Width)),
						}
					},
					Children: []core.Node{
						chipRow,
						inputNode,
						suggestionsRow,
					},
				}

				nodes := make([]core.Node, 0, 3)
				if len(snap.TargetIDs) > 1 {
					tagText := ed.T("askInPlace.selectedCount", l10n.Param("count", fmt.Sprintf("%d", len(snap.TargetIDs))))
					nodes = append(nodes, core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.AskInPlacePlacement().HasSelectionBounds
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID:        "ask-in-place-selection-tag",
								PointerEvents: core.PointerEventsNone,
								Style: core.Style{
									Position:       core.Absolute,
									Height:         core.Pt(18),
									Padding:        core.Symmetric(0, 6),
									CornerRadius:   6,
									Background:     p.Accent.Fg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									pl := ed.AskInPlacePlacement()
									vw, vh := ed.CanvasSize()
									if vw <= 0 || vh <= 0 {
										vw, vh = ed.ViewportSize()
									}
									tagLeft := max(core.Points(8), core.Points(pl.SelectionBounds[0]))
									if vw > 0 {
										tagLeft = min(tagLeft, max(core.Points(8), core.Points(vw)-96))
									}
									tagTop := max(core.Points(8), core.Points(pl.SelectionBounds[1]-24))
									if vh > 0 {
										tagTop = min(tagTop, max(core.Points(8), core.Points(vh)-26))
									}
									return core.Style{
										Left: core.Pt(tagLeft),
										Top:  core.Pt(tagTop),
									}
								},
								Children: []core.Node{
									core.Text{
										Content:  tagText,
										MaxLines: 1,
										Style: core.Style{
											FontSize:   10.5,
											FontWeight: core.FontWeightSemibold,
											Color:      p.Accent.OnFill,
										},
									},
								},
							}
						},
					})
				}

				nodes = append(nodes,
					composerCard,
					core.View{
						TestID:        "ask-in-place-hint",
						PointerEvents: core.PointerEventsNone,
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(16),
							Bottom:         core.Pt(58),
							Height:         core.Pt(26),
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
								Content:  hintText,
								MaxLines: 1,
								Style: core.Style{
									FontSize: 11.5,
									Color:    p.TextFaint,
								},
							},
						},
					},
				)

				return core.View{
					TestID:        "ask-in-place-layer",
					PointerEvents: core.PointerEventsBoxNone,
					Style: core.Style{
						Position: core.Absolute,
						Left:     core.Pt(0),
						Top:      core.Pt(0),
						Right:    core.Pt(0),
						Bottom:   core.Pt(0),
					},
					Children: nodes,
				}
			})
		},
	}
}
