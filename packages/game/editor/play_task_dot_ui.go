package editor

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// PlayTaskDotView renders the single corner dot shown over the viewport during Play mode
// when one or more agent tasks need attention (failed, waiting for permission, or ready to review),
// matching docs/design/editor/Tasks.dc.html ("DURING PLAY") using exclusively Spec.dc.html tokens.
//
// Nothing pops over the running game: only the 8 pt dot with its subtle 3 pt halo is drawn,
// colored for the most urgent state (TaskFailed > TaskNeedsYou > TaskReadyToReview). The label pill
// (for example "1 task needs you · stop Play to open") shows only while hovered or keyboard-focused.
func PlayTaskDotView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	hovered := core.NewSignal(cx, false)
	focused := core.NewSignal(cx, false)

	type dotKey struct {
		playing bool
		dot     PlayTaskDotState
		dark    bool
	}
	stateMemo := core.NewMemo(cx, func() dotKey {
		_ = rev.Get()
		if ed == nil || !ed.Playing() {
			return dotKey{}
		}
		return dotKey{
			playing: true,
			dot:     ed.PlayTaskDot(),
			dark:    ed.SpecPalette(cx).Dark,
		}
	})

	cx.Effect(func() {
		k := stateMemo.Get()
		if !k.playing || !k.dot.Visible {
			hovered.Set(false)
			focused.Set(false)
		}
	})

	return core.Show{
		When: func() bool {
			k := stateMemo.Get()
			return k.playing && k.dot.Visible
		},
		Then: func(cx *core.Scope) core.Node {
			// Keep play-task-dot mounted stably across task state/count escalations during Play
			// so pointer hover and keyboard focus are never dropped by a subtree remount.
			return core.View{
				TestID:        "play-task-dot-overlay",
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Left:     core.Pt(0),
					Height:   core.Pt(40),
				},
				Children: []core.Node{
					// Label pill shown strictly on hover or keyboard focus, left of the corner dot.
					core.Show{
						When: func() bool {
							return hovered.Get() || focused.Get()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID:        "play-task-dot-label",
								PointerEvents: core.PointerEventsNone,
								StyleFunc: func() core.Style {
									p := SpecPaletteFor(stateMemo.Get().dark)
									return core.Style{
										Position:       core.Absolute,
										Right:          core.Pt(28),
										Top:            core.Pt(6),
										Height:         core.Pt(22),
										Padding:        core.Symmetric(0, 8),
										CornerRadius:   6,
										Background:     p.PillBg,
										BorderWidth:    1,
										BorderColor:    p.LineRaised,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
									}
								},
								Children: []core.Node{
									core.Text{
										TestID: "play-task-dot-label-text",
										ContentFunc: func() string {
											return stateMemo.Get().dot.HoverLabel
										},
										StyleFunc: func() core.Style {
											p := SpecPaletteFor(stateMemo.Get().dark)
											return core.Style{
												FontSize: 11,
												Color:    p.TextSecondary,
											}
										},
									},
								},
							}
						},
					},
					// 28 x 28 hit target (Spec minimum) keeping the 8 pt dot centered at top: 12, right: 12.
					core.View{
						TestID: "play-task-dot",
						Role:   core.RoleStatus,
						LabelFunc: func() string {
							return stateMemo.Get().dot.AriaLabel
						},
						Focusable: func() bool { return true },
						StyleFunc: func() core.Style {
							p := SpecPaletteFor(stateMemo.Get().dark)
							return core.Style{
								Position:       core.Absolute,
								Top:            core.Pt(2),
								Right:          core.Pt(2),
								Width:          core.Pt(28),
								Height:         core.Pt(28),
								CornerRadius:   14,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								FocusVisible:   p.FocusRing(2),
							}
						},
						OnMouseEnter: func(core.MouseEvent) {
							hovered.Set(true)
						},
						OnMouseLeave: func(core.MouseEvent) {
							hovered.Set(false)
						},
						OnFocus: func() {
							focused.Set(true)
						},
						OnBlur: func() {
							focused.Set(false)
						},
						Children: []core.Node{
							core.View{
								TestID: "play-task-dot-halo",
								StyleFunc: func() core.Style {
									k := stateMemo.Get()
									p := SpecPaletteFor(k.dark)
									return core.Style{
										Width:          core.Pt(14),
										Height:         core.Pt(14),
										CornerRadius:   7,
										Background:     ui.Alpha(k.dot.Color(p), 0.25),
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
									}
								},
								Children: []core.Node{
									core.View{
										TestID: "play-task-dot-core",
										StyleFunc: func() core.Style {
											k := stateMemo.Get()
											p := SpecPaletteFor(k.dark)
											return core.Style{
												Width:        core.Pt(8),
												Height:       core.Pt(8),
												CornerRadius: 4,
												Background:   k.dot.Color(p),
											}
										},
									},
								},
							},
						},
					},
				},
			}
		},
	}
}

// AgentTaskNotificationCard renders one Spec-styled preview card for an AgentTaskNotification
// matching docs/design/editor/Tasks.dc.html ("SYSTEM NOTIFICATIONS · ONLY THESE THREE") using
// exclusively tokens from p.
func AgentTaskNotificationCard(p SpecPalette, n AgentTaskNotification, when string) core.Node {
	titleColor := p.TextPrimary
	switch n.State {
	case TaskNeedsYou:
		titleColor = p.Warning.Fg
	case TaskFailed:
		titleColor = p.Removed.Fg
	}
	shadowAlpha := float32(0.5)
	if !p.Dark {
		shadowAlpha = 0.16
	}
	var actions []core.Node
	if n.PrimaryAction != "" {
		actions = append(actions, core.View{
			Style: core.Style{
				Height:         core.Pt(28),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				Background:     p.LineRaised,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: n.PrimaryAction,
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextPrimary,
					},
				},
			},
		})
	}
	if n.SecondaryAction != "" {
		actions = append(actions, core.View{
			Style: core.Style{
				Height:         core.Pt(28),
				Padding:        core.Symmetric(0, 12),
				CornerRadius:   8,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: n.SecondaryAction,
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextMuted,
					},
				},
			},
		})
	}

	return core.View{
		TestID: "agent-notification-card-" + n.TaskID,
		Style: core.Style{
			Direction:    core.Row,
			Gap:          12,
			Padding:      core.All(12),
			CornerRadius: 18,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Shadows: []core.BoxShadow{
				ui.Shadow(0, 12, 32, 0, ui.Black(shadowAlpha)),
			},
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:          core.Pt(36),
					Height:         core.Pt(36),
					FlexShrink:     0,
					CornerRadius:   8,
					Background:     p.Accent.Fg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pt(18), Height: core.Pt(18)},
						Draw: func(d *core.Drawing, _ core.Size) {
							d.SetStrokeColor(p.Accent.OnFill).SetLineWidth(2.2).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
							d.BeginPath().MoveTo(4, 14).LineTo(9, 4).LineTo(14, 14).Stroke()
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinWidth:  core.Pt(0),
					Gap:       4,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							AlignItems:     core.AlignCenter,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{
								Content: n.Header,
								Style: core.Style{
									FontSize: 11,
									Color:    p.TextFaint,
								},
							},
							core.Text{
								Content: when,
								Style: core.Style{
									FontSize: 11,
									Color:    p.TextFaint,
								},
							},
						},
					},
					core.Text{
						Content: n.Title,
						Style: core.Style{
							FontSize:   12.5,
							FontWeight: 600,
							Color:      titleColor,
						},
					},
					core.Text{
						Content: n.Body,
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextSecondary,
						},
					},
					core.View{
						Style: core.Style{
							Direction: core.Row,
							Gap:       8,
							Margin:    core.Edges{Top: 4},
						},
						Children: actions,
					},
				},
			},
		},
	}
}
