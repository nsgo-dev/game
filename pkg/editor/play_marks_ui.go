package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived composite tints for Play mode header and panic card (all derived strictly from spec_theme.yaml tokens).
func playRunBg(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.Added.Fg, 0.06), p.Bg)
}

func playRunBorder(p SpecPalette) core.Color {
	return specAlpha(p.Added.Fg, 0.35)
}

func playPauseBg(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.Warning.Fg, 0.05), p.Bg)
}

func playPanicCardBg(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.Removed.Fg, 0.06), p.Raised)
}

func playAgentBubbleBg(p SpecPalette) core.Color {
	return CompositeOver(specAlpha(p.Accent.Fg, 0.08), p.Raised)
}

// playModeBarView renders the Play Mode control bar below the main toolbar while local Play mode is active
// (and not already showing the Remote Play bar), styled strictly with Spec.dc.html tokens matching PlayMode.dc.html.
func playModeBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Playing() && !ed.RemoteConnected() && !ed.RemoteConnecting()
		},
		Then: func(cx *core.Scope) core.Node {
			pal := ed.SpecPalette(cx)
			scaleBtn := func(scale float64, label string) core.Node {
				id := fmt.Sprintf("play-timescale-%g", scale)
				return base.Button{
					TestID: id,
					Label:  ed.T("playMarks.setTimeScaleLabel", l10n.Param("scale", label)),
					StyleFunc: func() core.Style {
						_ = rev.Get()
						bg := core.Color(0)
						if math.Abs(ed.PlayTimeScale()-scale) < 1e-6 {
							bg = pal.LineRaised
						}
						return core.Style{
							Height:         core.Pt(26),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Background:     bg,
							Cursor:         core.CursorPointer,
							Hover:          &core.Style{Background: pal.LineRaised},
							FocusVisible:   pal.FocusRing(-2),
						}
					},
					OnPress: func(core.PressEvent) {
						ed.SetPlayTimeScale(scale)
					},
					Children: []core.Node{
						core.Text{
							Content: label,
							Style: core.Style{
								FontSize:   11,
								FontWeight: 500,
								Color:      pal.TextSecondary,
							},
						},
					},
				}
			}

			return core.View{
				TestID: "play-mode-bar",
				Role:   core.RoleRegion,
				Label:  ed.T("playMarks.controlsLabel"),
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
							Background:  playPauseBg(pal),
							BorderColor: pal.Warning.Edge,
						}
					}
					return core.Style{
						Background:  playRunBg(pal),
						BorderColor: playRunBorder(pal),
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
									Background:  pal.Warning.Fill,
									BorderColor: pal.Warning.Edge,
								}
							}
							return core.Style{
								Background:  pal.Added.Fill,
								BorderColor: specAlpha(pal.Added.Edge, 0.45),
							}
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									if ed.PlayPaused() {
										return core.Style{Background: pal.Warning.Fg}
									}
									return core.Style{Background: pal.Added.Fg}
								},
							},
							core.Text{
								TestID: "play-status-pill-text",
								ContentFunc: func() string {
									_ = rev.Get()
									frameText := formatPlayFrameNumber(ed.PlaySteps())
									if ed.PlayPaused() {
										if ed.AskFrozenFrameOpen() {
											return ed.T("playMarks.status.frozen", l10n.Param("frame", frameText))
										}
										return ed.T("playMarks.status.paused", l10n.Param("frame", frameText))
									}
									sc := ed.PlayTimeScale()
									if math.Abs(sc-1) > 1e-6 {
										return ed.T("playMarks.status.playingScale", l10n.Param("scale", fmt.Sprintf("%g", sc)))
									}
									return ed.T("playMarks.status.playing")
								},
								Style: core.Style{FontSize: 11.5, FontWeight: 500},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									if ed.PlayPaused() {
										return core.Style{Color: pal.Warning.Fg}
									}
									return core.Style{Color: pal.TextSecondary}
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
							Background:   pal.Raised,
							BorderWidth:  1,
							BorderColor:  pal.LineRaised,
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
										return ed.T("playMarks.resumeLabel", l10n.Param("key", cx.KeyLabel("mod+shift+p")))
									}
									return ed.T("playMarks.pauseLabel", l10n.Param("key", cx.KeyLabel("mod+shift+p")))
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Tint:    func() core.Color { return pal.TextPrimary },
								Style:   core.Style{Width: core.Pt(30), Height: core.Pt(26), CornerRadius: 6},
								OnPress: func() { ed.SetPlayPaused(!ed.PlayPaused()) },
							},
							ui.Button{
								TestID:  "play-step-1",
								Icon:    lucide.StepForward,
								Label:   ed.T("playMarks.stepOneFrameLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									_ = rev.Get()
									return !ed.PlayPaused()
								},
								Tint:  func() core.Color { return pal.TextPrimary },
								Style: core.Style{Width: core.Pt(30), Height: core.Pt(26), CornerRadius: 6},
								OnPress: func() {
									ed.StepPlay(1)
								},
							},
							core.View{
								Style: core.Style{
									Width:      core.Pt(1),
									Height:     core.Pt(16),
									Background: pal.LineRaised,
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
								Label:   ed.T("playMarks.markLabel", l10n.Param("key", cx.KeyLabel("alt+f"))),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								TextFunc: func() string {
									_ = rev.Get()
									n := len(ed.PlayMarks())
									key := cx.KeyLabel("alt+f")
									if n > 0 {
										return ed.T("playMarks.markWithCount", l10n.Param("key", key), l10n.Param("count", n))
									}
									return ed.T("playMarks.markWithKey", l10n.Param("key", key))
								},
								Color: func() core.Color { return pal.Raised },
								Tint:  func() core.Color { return pal.TextSecondary },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      pal.TextSecondary,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 10),
									CornerRadius: 8,
									Background:   pal.Raised,
									BorderWidth:  1,
									BorderColor:  pal.LineRaised,
									Hover:        &core.Style{Background: pal.LineRaised},
								},
								OnPress: func() {
									_, err := ed.AddPlayMark()
									ed.report(err)
								},
							},
							ui.Button{
								TestID:  "play-ask-btn",
								Icon:    lucide.Sparkles,
								Text:    ed.T("playMarks.askButton", l10n.Param("key", cx.KeyLabel("mod+l"))),
								Label:   ed.T("playMarks.askButtonLabel", l10n.Param("key", cx.KeyLabel("mod+l"))),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return pal.Raised },
								Tint:    func() core.Color { return pal.Agent.Fg },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      pal.TextSecondary,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 10),
									CornerRadius: 8,
									Background:   pal.Raised,
									BorderWidth:  1,
									BorderColor:  pal.LineRaised,
									Hover:        &core.Style{Background: pal.LineRaised},
								},
								OnPress: func() {
									ed.report(ed.OpenAskFrozenFrame())
								},
							},
							ui.Button{
								TestID:  "play-stop-btn",
								Icon:    lucide.Square,
								Text:    ed.T("playMarks.stop"),
								Label:   ed.T("playMarks.stopLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return pal.TextPrimary },
								Tint:    func() core.Color { return pal.OnTextPrimary },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      pal.OnTextPrimary,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 12),
									CornerRadius: 8,
									Background:   pal.TextPrimary,
									Hover:        &core.Style{Background: pal.PrimaryHover},
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
			pal := ed.SpecPalette(cx)
			return core.View{
				TestID: "play-replay-footer",
				Style: core.Style{
					FlexShrink:  0,
					Padding:     core.Symmetric(10, 8),
					Gap:         4,
					Background:  pal.Bg,
					Borders:     core.Edges{Top: 1},
					BorderColor: pal.Line,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("playMarks.replayTitle"),
						Style: core.Style{
							FontSize:   11,
							FontWeight: core.FontWeightSemibold,
							Color:      pal.TextFaint,
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
							Color:       pal.TextSecondary,
						},
					},
					core.Text{
						Content: ed.T("playMarks.replayHint"),
						Style: core.Style{
							FontSize: 11,
							Color:    pal.TextFaint,
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.PlayLastReplayPath() != ""
						},
						Then: func(cx *core.Scope) core.Node {
							p := ed.SpecPalette(cx)
							return core.Text{
								TestID: "play-replay-last-path",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.PlayLastReplayPath()
								},
								Style: core.Style{
									FontSize: 10.5,
									Color:    p.TextMuted,
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
							pal := ed.SpecPalette(cx)
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
									Background:   specAlpha(pal.PillBg, 0.92),
									BorderWidth:  1,
									BorderColor:  pal.Agent.Edge,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Width:        core.Pt(6),
											Height:       core.Pt(6),
											CornerRadius: 3,
											Background:   pal.Agent.Fg,
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
											Color:       pal.TextSecondary,
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
							pal := ed.SpecPalette(cx)
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
									Background:   pal.Warning.Fill,
									BorderWidth:  1,
									BorderColor:  pal.Warning.Edge,
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											return ed.T("playMarks.pausedHint",
												l10n.Param("frame", formatPlayFrameNumber(ed.PlaySteps())),
												l10n.Param("key", cx.KeyLabel("mod+shift+p")),
											)
										},
										Style: core.Style{
											FontSize:    11.5,
											FontVariant: core.FontTabularNums,
											Color:       pal.Warning.Fg,
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
			pal := ed.SpecPalette(cx)
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				if ed.AskFrozenFrameOpen() {
					ed.CloseAskFrozenFrame()
				}
			})

			chip := func(p SpecPalette, text string, dot core.Color) core.Node {
				return core.View{
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          4,
						Height:       core.Pt(22),
						Padding:      core.Symmetric(0, 8),
						CornerRadius: 6,
						Background:   p.Panel,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
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
								Color:    p.TextMuted,
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
				Label:  ed.T("playMarks.askDialogLabel"),
				Style: core.Style{
					Position:     core.Absolute,
					Right:        core.Pt(292),
					Top:          core.Pt(118),
					Width:        core.Pt(344),
					Padding:      core.All(12),
					Gap:          10,
					CornerRadius: 12,
					Background:   pal.Raised,
					BorderWidth:  1,
					BorderColor:  pal.LineRaised,
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
									return ed.T("playMarks.askDialogTitle", l10n.Param("frame", formatPlayFrameNumber(ed.AskFrozenFrameNumber())))
								},
								Style: core.Style{
									FontSize:   14,
									FontWeight: core.FontWeightSemibold,
									Color:      pal.TextPrimary,
								},
							},
							core.Text{
								Content: ed.T("playMarks.gameFrozen"),
								Style: core.Style{
									FontSize: 11.5,
									Color:    pal.TextFaint,
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
							Background:   pal.Bg,
							BorderWidth:  1,
							BorderColor:  pal.Warning.Edge,
						},
						Children: []core.Node{
							core.Canvas{
								TestID: "play-ask-frame-canvas",
								Label:  ed.T("playMarks.frozenFrameImageLabel"),
								Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
								Draw: func(d *core.Drawing, size core.Size) {
									_ = rev.Get()
									d.Save()
									d.SetFillColor(ed.activeSpecPalette().Bg).FillRect(0, 0, float32(size.Width), float32(size.Height))
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
							p := ed.SpecPalette(cx)
							k := chipsMemo.Get()
							sec := float64(k.rollingFrames) / float64(k.rate)
							replayLabel := ed.T("playMarks.chip.replayFrames", l10n.Param("frames", formatPlayFrameNumber(k.rollingFrames)))
							if sec >= 1 {
								replayLabel = ed.T("playMarks.chip.replaySeconds", l10n.Param("seconds", fmt.Sprintf("%.0f", math.Round(sec))))
							}
							chips := []core.Node{
								chip(p, ed.T("playMarks.chip.frame", l10n.Param("frame", formatPlayFrameNumber(k.frame))), p.TextMuted),
								chip(p, replayLabel, p.TextMuted),
							}
							if k.selectedID != "" {
								chips = append(chips, chip(p, ed.T("playMarks.chip.selectedLiveState", l10n.Param("id", k.selectedID)), p.Changed.Fg))
							} else {
								chips = append(chips, chip(p, ed.T("playMarks.chip.liveState"), p.Added.Fg))
							}
							if k.liveChanges > 0 {
								chips = append(chips, chip(p, ed.Plural("playMarks.chip.liveChanges", k.liveChanges), p.Changed.Fg))
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
						Label:       ed.T("playMarks.askInputLabel"),
						Placeholder: ed.T("playMarks.askInputPlaceholder"),
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
							Background:   pal.Panel,
							Color:        pal.TextPrimary,
							CornerRadius: 8,
							BorderWidth:  1,
							BorderColor:  pal.Accent.Edge,
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
								Content: ed.T("playMarks.askReviewNote"),
								Style: core.Style{
									FontSize: 11.5,
									Color:    pal.TextFaint,
									FlexGrow: 1,
								},
							},
							ui.Button{
								TestID:  "play-ask-cancel",
								Text:    ed.T("playMarks.cancel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								TextStyle: core.Style{
									FontSize: 12,
									Color:    pal.TextMuted,
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
								Text:    ed.T("playMarks.ask"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Color:   func() core.Color { return pal.Accent.Fg },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: core.FontWeightSemibold,
									Color:      pal.Accent.OnFill,
								},
								Style: core.Style{
									Height:       core.Pt(30),
									Padding:      core.Symmetric(0, 14),
									CornerRadius: 8,
									Background:   pal.Accent.Fg,
									Hover:        &core.Style{Background: pal.AccentHover},
									Active:       &core.Style{Background: pal.AccentPressed},
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
	pal := ed.SpecPalette(cx)
	tabBtn := func(testID, label string, target PlayInspectorTab) core.Node {
		return base.Button{
			TestID: testID,
			Label:  label,
			StyleFunc: func() core.Style {
				_ = rev.Get()
				bg := core.Color(0)
				if ed.PlayInspectorTab() == target {
					bg = playAgentBubbleBg(pal)
				}
				return core.Style{
					Height:         core.Pt(26),
					Padding:        core.Symmetric(0, 8),
					CornerRadius:   6,
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     bg,
					Cursor:         core.CursorPointer,
					Hover:          &core.Style{Background: pal.LineRaised},
					FocusVisible:   pal.FocusRing(-2),
				}
			},
			OnPress: func(core.PressEvent) {
				ed.SetPlayInspectorTab(target)
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      pal.TextPrimary,
					},
				},
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
			Background:     pal.Panel,
			BorderWidth:    1,
			BorderColor:    pal.Line,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 2,
				Children: []core.Node{
					tabBtn("play-tab-inspector", ed.T("playMarks.tab.inspector"), PlayTabInspector),
					tabBtn("play-tab-agent", ed.T("playMarks.tab.agent"), PlayTabAgent),
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
							Background:  pal.Agent.Fill,
							BorderColor: pal.Agent.Edge,
						}
					}
					return core.Style{
						Background:  pal.Raised,
						BorderColor: pal.LineRaised,
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3},
						StyleFunc: func() core.Style {
							_ = rev.Get()
							if ed.PlayAgentActive() {
								return core.Style{Background: pal.Agent.Fg}
							}
							return core.Style{Background: pal.Added.Fg}
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
						Style: core.Style{FontSize: 10.5, Color: pal.TextMuted},
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
		pal := ed.SpecPalette(cx)
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
						Background:   playAgentBubbleBg(pal),
						BorderWidth:  1,
						BorderColor:  pal.LineRaised,
					},
					Children: []core.Node{
						core.Text{
							Content: question,
							Style:   core.Style{FontSize: 12, Color: pal.TextPrimary},
						},
						core.Text{
							Content: questionMeta,
							Style:   core.Style{FontSize: 11, Color: pal.TextMuted},
						},
					},
				})
			}
			for i, st := range steps {
				dotFill := core.Color(0)
				dotBorder := pal.LineRaised
				textColor := pal.TextFaint
				switch st.Status {
				case "done":
					dotFill = pal.Added.Fill
					dotBorder = pal.Added.Fg
					textColor = pal.TextMuted
				case "active":
					dotFill = pal.Agent.Fill
					dotBorder = pal.Agent.Fg
					textColor = pal.TextPrimary
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
				Content: ed.T("playMarks.agentPauseNote"),
				Style:   core.Style{FontSize: 11, Color: pal.TextFaint},
			})
		} else {
			children = append(children, core.Text{
				Content: ed.T("playMarks.agentEmptyHint", l10n.Param("key", cx.KeyLabel("mod+l"))),
				Style:   core.Style{FontSize: 11.5, Color: pal.TextMuted},
			})
		}

		// MARKS · N section
		children = append(children, core.Text{
			TestID:  "play-marks-header",
			Content: ed.T("playMarks.marksHeader", l10n.Param("count", len(marks))),
			Style: core.Style{
				FontSize:   11,
				FontWeight: core.FontWeightSemibold,
				Color:      pal.TextFaint,
				Padding:    core.Edges{Top: 6},
			},
		})
		for i, m := range marks {
			statusText := ed.T("playMarks.replayAttached")
			if m.Sent {
				statusText = ed.T("playMarks.replayAttachedSent")
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
					Background:   pal.Raised,
					BorderWidth:  1,
					BorderColor:  pal.LineRaised,
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Flag(),
						Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: pal.TextMuted},
					},
					core.Text{
						Content: m.TimeLabel,
						Style: core.Style{
							FontSize:    11,
							FontVariant: core.FontTabularNums,
							Color:       pal.TextPrimary,
						},
					},
					core.Text{
						Content: ed.T("playMarks.frameLabel", l10n.Param("frame", formatPlayFrameNumber(m.Frame))),
						Style: core.Style{
							FontSize:    11,
							FontVariant: core.FontTabularNums,
							Color:       pal.TextFaint,
							FlexGrow:    1,
						},
					},
					core.Text{
						Content: statusText,
						Style: core.Style{
							FontSize: 10.5,
							Color:    pal.TextFaint,
						},
					},
				},
			})
		}
		children = append(children,
			core.Text{
				Content: ed.T("playMarks.marksFooterHint"),
				Style:   core.Style{FontSize: 11, Color: pal.TextFaint},
			},
			ui.Button{
				TestID:  "play-send-marks-btn",
				Text:    ed.T("playMarks.sendMarksNow"),
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					return unsentCount == 0
				},
				Color: func() core.Color { return pal.Raised },
				TextStyle: core.Style{
					FontSize: 12,
					Color:    pal.TextSecondary,
				},
				Style: core.Style{
					Height:       core.Pt(30),
					Padding:      core.Symmetric(0, 12),
					CornerRadius: 8,
					Background:   pal.Raised,
					BorderWidth:  1,
					BorderColor:  pal.LineRaised,
					Hover:        &core.Style{Background: pal.LineRaised},
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
	pal := ed.SpecPalette(cx)
	gameFrames := p.GameFrames()
	rtFrames := p.RuntimeFrames()

	headerMeta := p.Time.Format("15:04:05")
	if p.Frame > 0 {
		headerMeta = ed.T("playMarks.panicMetaFrame",
			l10n.Param("time", headerMeta),
			l10n.Param("frame", formatPlayFrameNumber(p.Frame)),
		)
	}
	runLabel := ed.T("playMarks.panicTitle", l10n.Param("run", max(1, p.RunNumber)))

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
						Color:    pal.TextSecondary,
						FlexGrow: 1,
					},
				},
				ui.Button{
					TestID:  fmt.Sprintf("output-panic-link-%d", i),
					Text:    loc,
					Label:   ed.T("playMarks.panicOpenLocation", l10n.Param("loc", loc)),
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					TextStyle: core.Style{
						FontSize: 11.5,
						Color:    pal.Accent.Fg,
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
		foldText := ed.Plural("playMarks.panicRuntimeFrames", len(rtFrames))
		toggleText := ed.T("playMarks.panicShow")
		toggleA11yLabel := ed.T("playMarks.panicShowRuntimeLabel")
		if runtimeExpanded {
			toggleText = ed.T("playMarks.panicHide")
			toggleA11yLabel = ed.T("playMarks.panicHideRuntimeLabel")
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
						Color:    pal.TextFaint,
						FlexGrow: 1,
					},
				},
				ui.Button{
					TestID:  "output-panic-runtime-toggle",
					Text:    toggleText,
					Label:   toggleA11yLabel,
					Size:    ui.SizeSmall,
					Variant: ui.ButtonGhost,
					TextStyle: core.Style{
						FontSize: 11.5,
						Color:    pal.Accent.Fg,
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
							Style:   core.Style{FontSize: 11, Color: pal.TextFaint, FlexGrow: 1},
						},
						core.Text{
							Content: loc,
							Style:   core.Style{FontSize: 11, Color: pal.TextFaint},
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
			Background:   playPanicCardBg(pal),
			BorderWidth:  1,
			BorderColor:  specAlpha(pal.Removed.Edge, 0.50),
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
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: pal.Removed.Fg},
					},
					core.Text{
						TestID:  "output-panic-title",
						Content: runLabel,
						Style:   core.Style{FontSize: 12.5, FontWeight: core.FontWeightSemibold, Color: pal.TextPrimary},
					},
					core.Text{
						TestID:  "output-panic-meta",
						Content: headerMeta,
						Style:   core.Style{FontSize: 11.5, FontVariant: core.FontTabularNums, Color: pal.TextFaint},
					},
				},
			},
			// Panic message
			core.Text{
				TestID:  "output-panic-message",
				Content: strings.TrimSpace(p.Message),
				Style: core.Style{
					FontSize: 11.5,
					Color:    pal.Removed.Fg,
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
						Text:    ed.T("playMarks.panicAskFix"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color:   func() core.Color { return pal.Accent.Fg },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: core.FontWeightSemibold,
							Color:      pal.Accent.OnFill,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 8,
							Background:   pal.Accent.Fg,
							Hover:        &core.Style{Background: pal.AccentHover},
							Active:       &core.Style{Background: pal.AccentPressed},
						},
						OnPress: func() {
							ed.report(ed.AskAgentToFixPanic())
						},
					},
					ui.Button{
						TestID:  "output-panic-replay-btn",
						Text:    ed.T("playMarks.panicReplay"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Disabled: func() bool {
							return !canReplay
						},
						Color: func() core.Color { return pal.Raised },
						TextStyle: core.Style{
							FontSize: 12,
							Color:    pal.TextSecondary,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 8,
							Background:   pal.Raised,
							BorderWidth:  1,
							BorderColor:  pal.LineRaised,
							Hover:        &core.Style{Background: pal.LineRaised},
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
