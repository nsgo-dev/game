package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived composite tints for Play mode header and panic card (all derived strictly from Spec.dc.html tokens).
var (
	specPlayRunBg      = core.RGB(0x16221F) // #101216 + 6% #4FD38A
	specPlayRunBorder  = core.RGBA(0x4FD38A, 0.35)
	specPlayPauseBg    = core.RGB(0x1E1D19) // #101216 + 5% #F2C14E
	specPanicCardBg    = core.RGB(0x211A21) // #171A21 + 6% #FF6B6B
	specAgentBubbleBg  = core.RGB(0x1C2130)
	specAgentStepDoneB = core.RGB(0x1F3A2B)
)

// playModeBarView renders the Play Mode control bar below the main toolbar while local Play mode is active
// (and not already showing the Remote Play bar), styled strictly with Spec.dc.html tokens matching PlayMode.dc.html.
func playModeBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Playing() && !ed.RemoteConnected() && !ed.RemoteConnecting()
		},
		Then: func(cx *core.Scope) core.Node {
			scaleBtn := func(scale float64, label string) core.Node {
				id := fmt.Sprintf("play-timescale-%g", scale)
				return ui.Button{
					TestID:  id,
					Text:    label,
					Label:   fmt.Sprintf("Set play time scale to %s", label),
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					Color: func() core.Color {
						_ = rev.Get()
						if math.Abs(ed.PlayTimeScale()-scale) < 1e-6 {
							return specLineRaised
						}
						return core.RGBA(0, 0)
					},
					TextStyle: core.Style{
						FontSize:   11,
						FontWeight: 500,
						Color:      specText,
					},
					Style: core.Style{
						Height:       core.Pt(26),
						Padding:      core.Symmetric(0, 8),
						CornerRadius: 6,
					},
					OnPress: func() {
						ed.SetPlayTimeScale(scale)
					},
				}
			}

			return core.View{
				TestID: "play-mode-bar",
				Role:   core.RoleRegion,
				Label:  "Play mode controls",
				Style: core.Style{
					Direction:      core.Row,
					Wrap:           core.Wrap,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(6, 12),
					Gap:            8,
					Borders:        core.Edges{Bottom: 1},
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					if ed.PlayPaused() {
						return core.Style{
							Background:  specPlayPauseBg,
							BorderColor: specWarningEdge,
						}
					}
					return core.Style{
						Background:  specPlayRunBg,
						BorderColor: specPlayRunBorder,
					}
				},
				Children: []core.Node{
					// Left: Live / Paused / Frozen status pill
					core.View{
						TestID: "play-status-pill",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Height:       core.Pt(24),
							Padding:      core.Symmetric(0, 10),
							CornerRadius: 12,
							BorderWidth:  1,
						},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							if ed.PlayPaused() {
								return core.Style{
									Background:  specWarningFill,
									BorderColor: specWarningEdge,
								}
							}
							return core.Style{
								Background:  specAddedFill,
								BorderColor: specAddedEdge,
							}
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									if ed.PlayPaused() {
										return core.Style{Background: specWarning}
									}
									return core.Style{Background: specAdded}
								},
							},
							core.Text{
								TestID: "play-status-pill-text",
								ContentFunc: func() string {
									_ = rev.Get()
									frameText := formatPlayFrameNumber(ed.PlaySteps())
									if ed.PlayPaused() {
										if ed.AskFrozenFrameOpen() {
											return "Frozen for your question · frame " + frameText
										}
										return "Paused · frame " + frameText
									}
									sc := ed.PlayTimeScale()
									if math.Abs(sc-1) > 1e-6 {
										return fmt.Sprintf("Playing · live · %g×", sc)
									}
									return "Playing · live"
								},
								Style: core.Style{FontSize: 11.5, FontWeight: 500},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									if ed.PlayPaused() {
										return core.Style{Color: specWarning}
									}
									return core.Style{Color: specText}
								},
							},
						},
					},
					// Center: Pause/Resume, Step 1, Time scale segmented pill
					core.View{
						TestID: "play-transport-group",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          2,
							Padding:      core.All(3),
							CornerRadius: 8,
							Background:   specRaised,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
						},
						Children: []core.Node{
							ui.Button{
								TestID: "play-pause-resume",
								Icon: func() *core.SVG {
									_ = rev.Get()
									if ed.PlayPaused() {
										return lucide.Play()
									}
									return lucide.Pause()
								},
								LabelFunc: func() string {
									_ = rev.Get()
									if ed.PlayPaused() {
										return "Resume (" + cx.KeyLabel("mod+shift+p") + ")"
									}
									return "Pause (" + cx.KeyLabel("mod+shift+p") + ")"
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Tint:    func() core.Color { return specTextStrong },
								Style:   core.Style{Width: core.Pt(30), Height: core.Pt(26), CornerRadius: 6},
								OnPress: func() { ed.SetPlayPaused(!ed.PlayPaused()) },
							},
							ui.Button{
								TestID:  "play-step-1",
								Icon:    lucide.StepForward,
								Label:   "Step one frame (.)",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									_ = rev.Get()
									return !ed.PlayPaused()
								},
								Tint:  func() core.Color { return specTextStrong },
								Style: core.Style{Width: core.Pt(30), Height: core.Pt(26), CornerRadius: 6},
								OnPress: func() {
									ed.StepPlay(1)
								},
							},
							core.View{
								Style: core.Style{
									Width:      core.Pt(1),
									Height:     core.Pt(16),
									Background: specLineRaised,
									Margin:     core.Symmetric(0, 4),
								},
							},
							scaleBtn(0.25, "0.25×"),
							scaleBtn(0.5, "0.5×"),
							scaleBtn(1, "1×"),
							scaleBtn(2, "2×"),
						},
					},
					// Right: Mark ⌥F [N], Something looks wrong ⌘L, Stop
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							ui.Button{
								TestID:  "play-mark-btn",
								Icon:    lucide.Flag,
								Label:   "Mark this moment without stopping (" + cx.KeyLabel("alt+f") + ")",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								TextFunc: func() string {
									_ = rev.Get()
									n := len(ed.PlayMarks())
									key := cx.KeyLabel("alt+f")
									if n > 0 {
										return fmt.Sprintf("Mark %s · %d", key, n)
									}
									return fmt.Sprintf("Mark %s", key)
								},
								Color: func() core.Color { return specRaised },
								Tint:  func() core.Color { return specText },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      specText,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 10),
									CornerRadius: 8,
								},
								OnPress: func() {
									_, err := ed.AddPlayMark()
									ed.report(err)
								},
							},
							ui.Button{
								TestID:  "play-ask-btn",
								Icon:    lucide.Sparkles,
								Text:    "Something looks wrong " + cx.KeyLabel("mod+l"),
								Label:   "Pause and ask the agent about this frame (" + cx.KeyLabel("mod+l") + ")",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return specRaised },
								Tint:    func() core.Color { return specAgent },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      specText,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 10),
									CornerRadius: 8,
								},
								OnPress: func() {
									ed.report(ed.OpenAskFrozenFrame())
								},
							},
							ui.Button{
								TestID:  "play-stop-btn",
								Icon:    lucide.Square,
								Text:    "Stop",
								Label:   "Stop Play mode",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return specTextStrong },
								Tint:    func() core.Color { return specBg },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      specBg,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 12),
									CornerRadius: 8,
								},
								OnPress: func() {
									ed.report(ed.Stop())
								},
							},
						},
					},
				},
			}
		},
	}
}

// playReplayFooterView renders the left-rail REPLAY summary card ("Last 60 s kept · N frames")
// below the Scene Tree while Play mode or a remote game is active, matching PlayMode.dc.html.
func playReplayFooterView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Playing() || ed.RemoteConnected()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "play-replay-footer",
				Style: core.Style{
					FlexShrink:  0,
					Padding:     core.Symmetric(10, 8),
					Gap:         4,
					Background:  specBg,
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.Text{
						Content: "REPLAY",
						Style: core.Style{
							FontSize:   11,
							FontWeight: core.FontWeightSemibold,
							Color:      specTextMuted,
						},
					},
					core.Text{
						TestID: "play-replay-summary",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.PlayRollingSummary()
						},
						Style: core.Style{
							FontSize:    12,
							FontVariant: core.FontTabularNums,
							Color:       specText,
						},
					},
					core.Text{
						Content: "Saved as an input recording when you mark or ask",
						Style: core.Style{
							FontSize: 11,
							Color:    specTextMuted,
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.PlayLastReplayPath() != ""
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "play-replay-last-path",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.PlayLastReplayPath()
								},
								Style: core.Style{
									FontSize: 10.5,
									Color:    specTextSecondary,
								},
							}
						},
					},
				},
			}
		},
	}
}

// playViewportOverlaysView renders the Play-mode viewport overlays:
// the agent stepping pill ("agent stepping frames A–B"), the paused hint pill,
// and the "Ask about frame N" floating dialog.
func playViewportOverlaysView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Playing() || ed.RemoteConnected() || ed.AskFrozenFrameOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Position: core.Absolute,
					Left:     core.Pt(0),
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
				},
				Children: []core.Node{
					// Viewport agent stepping pill ("agent stepping frames 4 800–4 812")
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.PlayAgentPill() != ""
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID:        "play-agent-pill",
								PointerEvents: core.PointerEventsNone,
								Style: core.Style{
									Position:     core.Absolute,
									Left:         core.Pt(16),
									Top:          core.Pt(16),
									Height:       core.Pt(24),
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          6,
									Padding:      core.Symmetric(0, 10),
									CornerRadius: 12,
									Background:   specBgOverlay,
									BorderWidth:  1,
									BorderColor:  specAgentEdge,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Width:        core.Pt(6),
											Height:       core.Pt(6),
											CornerRadius: 3,
											Background:   specAgent,
										},
									},
									core.Text{
										TestID: "play-agent-pill-text",
										ContentFunc: func() string {
											_ = rev.Get()
											return ed.PlayAgentPill()
										},
										Style: core.Style{
											FontSize:    11.5,
											FontVariant: core.FontTabularNums,
											Color:       specText,
										},
									},
								},
							}
						},
					},
					// Paused hint pill at bottom center of viewport
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.Playing() && ed.PlayPaused()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID:        "play-paused-hint",
								PointerEvents: core.PointerEventsNone,
								Style: core.Style{
									Position:     core.Absolute,
									Left:         core.Pt(16),
									Bottom:       core.Pt(12),
									Height:       core.Pt(26),
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Padding:      core.Symmetric(0, 12),
									CornerRadius: 13,
									Background:   specWarningFill,
									BorderWidth:  1,
									BorderColor:  specWarningEdge,
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											return fmt.Sprintf("Paused at frame %s · . steps one frame · %s resumes", formatPlayFrameNumber(ed.PlaySteps()), cx.KeyLabel("mod+shift+p"))
										},
										Style: core.Style{
											FontSize:    11.5,
											FontVariant: core.FontTabularNums,
											Color:       specWarning,
										},
									},
								},
							}
						},
					},
				},
			}
		},
	}
}

func playAskFrozenFrameDialog(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.AskFrozenFrameOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.AskFrozenFrameOpen() {
					ed.CloseAskFrozenFrame()
				}
			})

			chip := func(text string, dot core.Color) core.Node {
				return core.View{
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          4,
						Height:       core.Pt(22),
						Padding:      core.Symmetric(0, 8),
						CornerRadius: 6,
						Background:   specPanel,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pt(6),
								Height:       core.Pt(6),
								CornerRadius: 2,
								Background:   dot,
							},
						},
						core.Text{
							Content: text,
							Style: core.Style{
								FontSize: 11,
								Color:    specTextSecondary,
							},
						},
					},
				}
			}

			submit := func() {
				_, err := ed.SubmitAskFrozenFrame(ed.AskFrozenFrameDraft())
				ed.report(err)
			}

			return core.View{
				TestID: "play-ask-dialog",
				Role:   core.RoleDialog,
				Label:  "Ask about frozen frame",
				Style: core.Style{
					Position:     core.Absolute,
					Right:        core.Pt(292),
					Top:          core.Pt(118),
					Width:        core.Pt(344),
					Padding:      core.All(12),
					Gap:          10,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					// Header: "Ask about frame N" + "game frozen"
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{
								TestID: "play-ask-title",
								ContentFunc: func() string {
									_ = rev.Get()
									return "Ask about frame " + formatPlayFrameNumber(ed.AskFrozenFrameNumber())
								},
								Style: core.Style{
									FontSize:   14,
									FontWeight: core.FontWeightSemibold,
									Color:      specTextStrong,
								},
							},
							core.Text{
								Content: "game frozen",
								Style: core.Style{
									FontSize: 11.5,
									Color:    specTextMuted,
								},
							},
						},
					},
					// Captured frozen frame preview thumbnail
					core.View{
						TestID: "play-ask-frame-preview",
						Style: core.Style{
							Height:       core.Pt(110),
							CornerRadius: 8,
							Overflow:     core.Hidden,
							Background:   specBg,
							BorderWidth:  1,
							BorderColor:  specWarningEdge,
						},
						Children: []core.Node{
							core.Canvas{
								TestID: "play-ask-frame-canvas",
								Label:  "Frozen frame image",
								Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
								Draw: func(d *core.Drawing, size core.Size) {
									_ = rev.Get()
									d.Save()
									d.SetFillColor(specBg).FillRect(0, 0, float32(size.Width), float32(size.Height))
									// Preserve the main viewport's canvas size so drawing the thumbnail
									// does not overwrite Editor.canvasW/canvasH.
									prevW, prevH := ed.canvasW, ed.canvasH
									defer func() { ed.canvasW, ed.canvasH = prevW, prevH }()
									ed.DrawViewport(d, size)
									d.Restore()
								},
							},
						},
					},
					// Attachment chips (memoized so typing in play-ask-input does not remount chips)
					func() core.Node {
						type chipKey struct {
							frame         int
							rollingFrames int
							rate          int
							selectedID    string
							liveChanges   int
						}
						chipsMemo := core.NewMemo(cx, func() chipKey {
							_ = rev.Get()
							return chipKey{
								frame:         ed.AskFrozenFrameNumber(),
								rollingFrames: ed.PlayRollingFrameCount(),
								rate:          max(1, ed.PlayRateHz()),
								selectedID:    ed.SelectedID,
								liveChanges:   len(ed.RemoteLiveChanges()),
							}
						})
						return core.Dynamic(func(cx *core.Scope) core.Node {
							k := chipsMemo.Get()
							sec := float64(k.rollingFrames) / float64(k.rate)
							replayLabel := fmt.Sprintf("Replay · %s frames", formatPlayFrameNumber(k.rollingFrames))
							if sec >= 1 {
								replayLabel = fmt.Sprintf("Replay · last %.0f s", math.Round(sec))
							}
							chips := []core.Node{
								chip("Frame "+formatPlayFrameNumber(k.frame), specTextSecondary),
								chip(replayLabel, specTextSecondary),
							}
							if k.selectedID != "" {
								chips = append(chips, chip(k.selectedID+" · live state", specChanged))
							} else {
								chips = append(chips, chip("Live state", specAdded))
							}
							if k.liveChanges > 0 {
								unit := "live changes"
								if k.liveChanges == 1 {
									unit = "live change"
								}
								chips = append(chips, chip(fmt.Sprintf("%d %s", k.liveChanges, unit), specChanged))
							}
							return core.View{
								Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
								Children: chips,
							}
						})
					}(),
					// Prompt field
					ui.Input{
						TestID:      "play-ask-input",
						Label:       "What looks wrong?",
						Placeholder: "What looks wrong? e.g. the slime walks through the wall",
						AutoFocus:   true,
						Value: func() string {
							_ = rev.Get()
							return ed.AskFrozenFrameDraft()
						},
						OnChange: func(s string) {
							ed.SetAskFrozenFrameDraft(s)
						},
						OnSubmit: func(s string) {
							ed.SetAskFrozenFrameDraft(s)
							submit()
						},
						Style: core.Style{
							MinHeight:    core.Pt(56),
							Background:   specPanel,
							Color:        specTextStrong,
							CornerRadius: 8,
							BorderWidth:  1,
							BorderColor:  specAccent,
						},
					},
					// Footer: "A fix comes back as a review", Cancel, Ask
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: []core.Node{
							core.Text{
								TestID:  "play-ask-review-note",
								Content: "A fix comes back as a review",
								Style: core.Style{
									FontSize: 11.5,
									Color:    specTextMuted,
									FlexGrow: 1,
								},
							},
							ui.Button{
								TestID:  "play-ask-cancel",
								Text:    "Cancel",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								TextStyle: core.Style{
									FontSize: 12,
									Color:    specTextSecondary,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 10),
									CornerRadius: 8,
								},
								OnPress: func() {
									ed.CloseAskFrozenFrame()
								},
							},
							ui.Button{
								TestID:  "play-ask-submit",
								Text:    "Ask",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return specAccent },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: core.FontWeightSemibold,
									Color:      specBg, // Spec contrast table: #101216 on #3B7BFF (4.88:1 AA)
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 14),
									CornerRadius: 8,
								},
								OnPress: submit,
							},
						},
					},
				},
			}
		},
	}
}

// shouldShowPlayInspectorTabs reports whether the right panel should show the Play-mode Inspector / Agent tabs.
func shouldShowPlayInspectorTabs(ed *Editor) bool {
	if ed == nil {
		return false
	}
	return ed.Playing() || ed.RemoteConnected() || len(ed.PlayMarks()) > 0 || ed.PlayAgentActive()
}

// playInspectorTabBarView renders the Play-mode Inspector | Agent tab strip + MCP status chip at the top of InspectorView.
func playInspectorTabBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	tabBtn := func(testID, label string, target PlayInspectorTab) core.Node {
		return ui.Button{
			TestID:  testID,
			Text:    label,
			Size:    ui.SizeSmall,
			Variant: ui.ButtonGhost,
			Color: func() core.Color {
				_ = rev.Get()
				if ed.PlayInspectorTab() == target {
					return specAgentBubbleBg
				}
				return core.RGBA(0, 0)
			},
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      specTextStrong,
			},
			Style: core.Style{
				Height:       core.Pt(26),
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 6,
			},
			OnPress: func() {
				ed.SetPlayInspectorTab(target)
			},
		}
	}

	return core.View{
		TestID: "play-inspector-tabs",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            4,
			Padding:        core.All(4),
			CornerRadius:   8,
			Background:     specPanel,
			BorderWidth:    1,
			BorderColor:    specLine,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 2,
				Children: []core.Node{
					tabBtn("play-tab-inspector", "Inspector", PlayTabInspector),
					tabBtn("play-tab-agent", "Agent", PlayTabAgent),
				},
			},
			core.View{
				TestID: "play-agent-chip",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          5,
					Height:       core.Pt(22),
					Padding:      core.Symmetric(0, 7),
					CornerRadius: 11,
					BorderWidth:  1,
					FlexShrink:   0,
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					if ed.PlayAgentActive() {
						return core.Style{
							Background:  specAgentTint,
							BorderColor: specAgentEdge,
						}
					}
					return core.Style{
						Background:  specRaised,
						BorderColor: specLineRaised,
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							if ed.PlayAgentActive() {
								return core.Style{Background: specAgent}
							}
							return core.Style{Background: specAdded}
						},
					},
					core.Text{
						ContentFunc: func() string {
							_ = rev.Get()
							if steps := ed.PlayAgentSteps(); len(steps) > 0 && ed.PlayAgentActive() {
								done := 0
								for _, st := range steps {
									if st.Status == "done" {
										done++
									}
								}
								return fmt.Sprintf("%d/%d", min(done+1, len(steps)), len(steps))
							}
							return "MCP"
						},
						Style: core.Style{FontSize: 10.5, Color: specTextSecondary},
					},
				},
			},
		},
	}
}

// playAgentPanelView renders the Play-mode Agent tab in the right inspector:
// MARKS · N list with time, frame, "replay attached", "Send marks now", and active question + step log.
func playAgentPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	panelKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		b.WriteString(ed.playMarks.sentQuestion)
		b.WriteByte('\x00')
		b.WriteString(ed.playMarks.sentMeta)
		b.WriteByte('\x00')
		for _, st := range ed.PlayAgentSteps() {
			b.WriteString(st.Status)
			b.WriteByte(':')
			b.WriteString(st.Text)
			b.WriteByte('\x00')
		}
		b.WriteByte('|')
		for _, m := range ed.PlayMarks() {
			fmt.Fprintf(&b, "%d:%d:%v:%s;", m.ID, m.Frame, m.Sent, m.TimeLabel)
		}
		return b.String()
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = panelKey.Get()
		marks := ed.PlayMarks()
		unsentCount := len(ed.UnsentPlayMarks())
		steps := ed.PlayAgentSteps()
		question := ed.playMarks.sentQuestion
		questionMeta := ed.playMarks.sentMeta

		var children []core.Node

		// Active question + agent stepping progress
		if question != "" || len(steps) > 0 {
			if question != "" {
				children = append(children, core.View{
					TestID: "play-agent-question-card",
					Style: core.Style{
						Padding:      core.Symmetric(8, 10),
						Gap:          4,
						CornerRadius: 10,
						Background:   specAgentBubbleBg,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					Children: []core.Node{
						core.Text{
							Content: question,
							Style:   core.Style{FontSize: 12, Color: specTextStrong},
						},
						core.Text{
							Content: questionMeta,
							Style:   core.Style{FontSize: 11, Color: specTextSecondary},
						},
					},
				})
			}
			for i, st := range steps {
				dotFill := core.RGBA(0, 0)
				dotBorder := specLineRaised
				textColor := specTextMuted
				switch st.Status {
				case "done":
					dotFill = specAgentStepDoneB
					dotBorder = specAdded
					textColor = specTextSecondary
				case "active":
					dotFill = specAgentTint
					dotBorder = specAgent
					textColor = specTextStrong
				}
				children = append(children, core.View{
					TestID: fmt.Sprintf("play-agent-step-%d", i),
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignStart,
						Gap:        8,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pt(12),
								Height:       core.Pt(12),
								Margin:       core.Edges{Top: 2},
								FlexShrink:   0,
								CornerRadius: 6,
								Background:   dotFill,
								BorderWidth:  1.5,
								BorderColor:  dotBorder,
							},
						},
						core.Text{
							Content: st.Text,
							Style: core.Style{
								FontSize:   11.5,
								Color:      textColor,
								FlexGrow:   1,
								FlexShrink: 1,
							},
						},
					},
				})
			}
			children = append(children, core.Text{
				TestID:  "play-agent-pause-note",
				Content: "The game stays paused while the agent steps through the replay. Resume whenever you like; its fix comes back as a review.",
				Style:   core.Style{FontSize: 11, Color: specTextMuted},
			})
		} else {
			children = append(children, core.Text{
				Content: "The agent sees the running game through the editor's MCP server: live entities, the last 60 s of replay and your marks. Ask with " + cx.KeyLabel("mod+l") + " to freeze the frame first.",
				Style:   core.Style{FontSize: 11.5, Color: specTextSecondary},
			})
		}

		// MARKS · N section
		children = append(children, core.Text{
			TestID:  "play-marks-header",
			Content: fmt.Sprintf("MARKS · %d", len(marks)),
			Style: core.Style{
				FontSize:   11,
				FontWeight: core.FontWeightSemibold,
				Color:      specTextMuted,
				Padding:    core.Edges{Top: 6},
			},
		})
		for i, m := range marks {
			statusText := "replay attached"
			if m.Sent {
				statusText = "replay attached · sent"
			}
			children = append(children, core.View{
				TestID: fmt.Sprintf("play-mark-row-%d", i),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          6,
					Height:       core.Pt(32),
					Padding:      core.Symmetric(0, 10),
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Flag(),
						Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: specTextSecondary},
					},
					core.Text{
						Content: m.TimeLabel,
						Style: core.Style{
							FontSize:    11,
							FontVariant: core.FontTabularNums,
							Color:       specTextStrong,
						},
					},
					core.Text{
						Content: "frame " + formatPlayFrameNumber(m.Frame),
						Style: core.Style{
							FontSize:    11,
							FontVariant: core.FontTabularNums,
							Color:       specTextMuted,
							FlexGrow:    1,
						},
					},
					core.Text{
						Content: statusText,
						Style: core.Style{
							FontSize: 10.5,
							Color:    specTextMuted,
						},
					},
				},
			})
		}
		children = append(children,
			core.Text{
				Content: "When you stop, all marks go to the agent as one thread.",
				Style:   core.Style{FontSize: 11, Color: specTextMuted},
			},
			ui.Button{
				TestID:  "play-send-marks-btn",
				Text:    "Send marks now",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					return unsentCount == 0
				},
				Color: func() core.Color { return specRaised },
				TextStyle: core.Style{
					FontSize: 12,
					Color:    specText,
				},
				Style: core.Style{
					Height:       core.Pt(30),
					Padding:      core.Symmetric(0, 12),
					CornerRadius: 8,
				},
				OnPress: func() {
					ed.SendPlayMarks()
				},
			},
		)

		return core.View{
			TestID:   "play-agent-panel",
			Style:    core.Style{Gap: 8},
			Children: children,
		}
	})
}

// outputPanicCardView renders the Spec-styled panic card inside the Output panel when a run panicked,
// showing the panic message, clickable file:line game stack frames, folded runtime frames ("N frames in the game runtime · Show"),
// "Ask the agent to fix", and "Replay up to the panic".
func outputPanicCardView(cx *core.Scope, ed *Editor, p *PanicReport, runtimeExpanded bool) core.Node {
	if p == nil {
		return nil
	}
	gameFrames := p.GameFrames()
	rtFrames := p.RuntimeFrames()

	headerMeta := p.Time.Format("15:04:05")
	if p.Frame > 0 {
		headerMeta += " · frame " + formatPlayFrameNumber(p.Frame)
	}
	runLabel := fmt.Sprintf("Run %d stopped on a panic", max(1, p.RunNumber))

	var frameRows []core.Node
	for i, f := range gameFrames {
		frame := f
		loc := fmt.Sprintf("%s:%d", frame.File, frame.Line)
		frameRows = append(frameRows, core.View{
			TestID: fmt.Sprintf("output-panic-frame-%d", i),
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            12,
			},
			Children: []core.Node{
				core.Text{
					Content: frame.Function,
					Style: core.Style{
						FontSize: 11.5,
						Color:    specText,
						FlexGrow: 1,
					},
				},
				ui.Button{
					TestID:  fmt.Sprintf("output-panic-link-%d", i),
					Text:    loc,
					Label:   "Open " + loc,
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					TextStyle: core.Style{
						FontSize: 11.5,
						Color:    specAccent,
					},
					Style: core.Style{
						Height:  core.Pt(20),
						Padding: core.Symmetric(0, 4),
					},
					OnPress: func() {
						ed.report(ed.OpenOutputEntry(OutputEntry{
							Text: loc,
							File: frame.File,
							Line: frame.Line,
						}))
					},
				},
			},
		})
	}

	if len(rtFrames) > 0 {
		foldText := fmt.Sprintf("%d frames in the game runtime", len(rtFrames))
		if len(rtFrames) == 1 {
			foldText = "1 frame in the game runtime"
		}
		toggleLabel := "Show"
		if runtimeExpanded {
			toggleLabel = "Hide"
		}
		frameRows = append(frameRows, core.View{
			TestID: "output-panic-runtime-row",
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            12,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "output-panic-runtime-label",
					Content: foldText,
					Style: core.Style{
						FontSize: 11.5,
						Color:    specTextMuted,
						FlexGrow: 1,
					},
				},
				ui.Button{
					TestID:  "output-panic-runtime-toggle",
					Text:    toggleLabel,
					Label:   toggleLabel + " runtime stack frames",
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					TextStyle: core.Style{
						FontSize: 11.5,
						Color:    specAccent,
					},
					Style: core.Style{
						Height:  core.Pt(20),
						Padding: core.Symmetric(0, 4),
					},
					OnPress: func() {
						ed.SetOutputPanicRuntimeExpanded(!runtimeExpanded)
					},
				},
			},
		})
		if runtimeExpanded {
			for i, f := range rtFrames {
				loc := fmt.Sprintf("%s:%d", f.File, f.Line)
				frameRows = append(frameRows, core.View{
					TestID: fmt.Sprintf("output-panic-runtime-frame-%d", i),
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            12,
						Padding:        core.Edges{Left: 8},
					},
					Children: []core.Node{
						core.Text{
							Content: f.Function,
							Style:   core.Style{FontSize: 11, Color: specTextMuted, FlexGrow: 1},
						},
						core.Text{
							Content: loc,
							Style:   core.Style{FontSize: 11, Color: specTextMuted},
						},
					},
				})
			}
		}
	}

	canReplay := p.Frame > 0 && (p.ReplayAbs != "" || p.ReplayPath != "" || ed.playMarks.lastPanicRec != nil || ed.playRolling != nil)

	return core.View{
		TestID: "output-panic-card",
		Role:   core.RoleRegion,
		Label:  runLabel,
		Style: core.Style{
			Padding:      core.All(10),
			Gap:          6,
			CornerRadius: 10,
			Background:   specPanicCardBg,
			BorderWidth:  1,
			BorderColor:  specProblemEdge,
		},
		Children: []core.Node{
			// Title row
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.TriangleAlert(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: specProblem},
					},
					core.Text{
						TestID:  "output-panic-title",
						Content: runLabel,
						Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightSemibold, Color: specTextStrong},
					},
					core.Text{
						TestID:  "output-panic-meta",
						Content: headerMeta,
						Style:   core.Style{FontSize: 11.5, FontVariant: core.FontTabularNums, Color: specTextMuted},
					},
				},
			},
			// Panic message
			core.Text{
				TestID:  "output-panic-message",
				Content: strings.TrimSpace(p.Message),
				Style: core.Style{
					FontSize: 11.5,
					Color:    specProblem,
				},
			},
			// Stack frames
			core.View{
				Style:    core.Style{Gap: 2},
				Children: frameRows,
			},
			// Action buttons
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
					Padding:    core.Edges{Top: 2},
				},
				Children: []core.Node{
					ui.Button{
						TestID:  "output-panic-ask-btn",
						Text:    "Ask the agent to fix",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color:   func() core.Color { return specAccent },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: core.FontWeightSemibold,
							Color:      specBg,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 8,
						},
						OnPress: func() {
							ed.report(ed.AskAgentToFixPanic())
						},
					},
					ui.Button{
						TestID:  "output-panic-replay-btn",
						Text:    "Replay up to the panic",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Disabled: func() bool {
							return !canReplay
						},
						Color: func() core.Color { return specRaised },
						TextStyle: core.Style{
							FontSize: 12,
							Color:    specText,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 8,
						},
						OnPress: func() {
							ed.report(ed.ReplayUpToPanic())
						},
					},
				},
			},
		},
	}
}
