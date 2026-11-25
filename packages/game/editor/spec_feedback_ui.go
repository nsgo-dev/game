package editor

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ToastKind identifies the visual tone and semantic icon of an editor toast (docs/design/editor/Toasts.dc.html).
type ToastKind string

const (
	ToastSuccess  ToastKind = "success"
	ToastInfo     ToastKind = "info"
	ToastWarning  ToastKind = "warning"
	ToastError    ToastKind = "error"
	ToastProgress ToastKind = "progress"
)

// Toast describes a transient feedback message shown at the bottom center of the editor viewport
// (docs/design/editor/Toasts.dc.html). Decisions never go in a toast; at most one action button is shown.
type Toast struct {
	// ID optionally deduplicates repeated identical events in the toast queue.
	ID string
	// Kind selects the icon shape and surface styling (ToastWarning uses Spec Warning.Fill and Warning.Edge).
	Kind ToastKind
	// Message is the single-line feedback text.
	Message string
	// ActionLabel is an optional short verb button label (for example "Undo", "Show", or "Retry").
	ActionLabel string
	// ActionShortcut is an optional keyboard shortcut hint shown next to ActionLabel (for example "⌘Z").
	ActionShortcut string
	// OnAction runs when the user activates the toast's action button.
	OnAction func()
	// Progress is the 0..1 completion fraction for ToastProgress toasts.
	Progress float32
	// Dismissible reports whether a Dismiss button is shown; defaults to true when Kind != ToastProgress.
	Dismissible *bool
}

func (t Toast) canDismiss() bool {
	if t.Dismissible != nil {
		return *t.Dismissible
	}
	return t.Kind != ToastProgress
}

// ShowToast appends t to the editor's FIFO toast queue, merging duplicates with the same non-empty ID
// into the existing queued entry, and repaints the editor.
func (e *Editor) ShowToast(t Toast) {
	if e == nil || t.Message == "" {
		return
	}
	if t.Kind == "" {
		t.Kind = ToastInfo
	}
	if t.ID != "" {
		for i := range e.toasts {
			if e.toasts[i].ID == t.ID {
				e.toasts[i] = t
				e.notify()
				return
			}
		}
	}
	e.toasts = append(e.toasts, t)
	e.notify()
}

// DismissToast removes the currently visible toast and advances the queue.
func (e *Editor) DismissToast() {
	if e == nil || len(e.toasts) == 0 {
		return
	}
	e.toasts = e.toasts[1:]
	e.notify()
}

// ClearToasts removes all active and queued toasts.
func (e *Editor) ClearToasts() {
	if e == nil || len(e.toasts) == 0 {
		return
	}
	e.toasts = nil
	e.notify()
}

// ActiveToast returns the currently visible toast at the head of the queue, if any.
func (e *Editor) ActiveToast() (Toast, bool) {
	if e == nil || len(e.toasts) == 0 {
		return Toast{}, false
	}
	return e.toasts[0], true
}

// QueuedToastCount returns how many additional toasts are waiting behind the visible toast.
func (e *Editor) QueuedToastCount() int {
	if e == nil || len(e.toasts) <= 1 {
		return 0
	}
	return len(e.toasts) - 1
}

// OverlayLegendOpen reports whether the compact viewport overlay legend is visible.
func (e *Editor) OverlayLegendOpen() bool {
	return e != nil && e.overlayLegendOpen
}

// SetOverlayLegendOpen shows or hides the compact viewport overlay legend in the viewport top-left corner.
func (e *Editor) SetOverlayLegendOpen(open bool) {
	if e == nil || e.overlayLegendOpen == open {
		return
	}
	e.overlayLegendOpen = open
	e.notify()
}

// ToastView renders the editor's active toast at the bottom center of the viewport using exclusively
// SpecPalette tokens (matching docs/design/editor/Toasts.dc.html) in both Dark and Light appearances.
func ToastView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			_, ok := ed.ActiveToast()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				t, ok := ed.ActiveToast()
				if !ok {
					return core.View{}
				}
				p := ed.SpecPalette(cx)
				waiting := ed.QueuedToastCount()
				return core.View{
					TestID:        "editor-toast-host",
					Role:          core.RoleStatus,
					PointerEvents: core.PointerEventsBoxNone,
					Style: core.Style{
						Position:       core.Absolute,
						Left:           core.Pt(12),
						Right:          core.Pt(12),
						Bottom:         core.Pt(16),
						Direction:      core.Row,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						SpecToastCard(p, t, waiting, ed.DismissToast),
					},
				}
			})
		},
	}
}

// SpecToastCard renders a single Spec toast pill (40 pt tall, 12 pt radius, single line, semantic icon,
// optional waiting count badge, optional action button with shortcut, optional dismiss button) using
// exclusively tokens from p.
func SpecToastCard(p SpecPalette, t Toast, waiting int, onDismiss func()) core.Node {
	bg := p.Raised
	border := p.LineRaised
	actionBg := p.LineRaised
	actionBorder := core.Transparent
	if t.Kind == ToastWarning {
		bg = p.Warning.Fill
		border = p.Warning.Edge
		actionBg = p.Warning.Fill
		actionBorder = p.Warning.Edge
	}

	shadowAlpha := float32(0.42)
	if !p.Dark {
		shadowAlpha = 0.14
	}

	children := []core.Node{
		specToastIcon(p, t.Kind),
		core.Text{
			TestID:  "editor-toast-message",
			Content: t.Message,
			Style: core.Style{
				FontSize:   12.5,
				FontWeight: 400,
				Color:      p.TextSecondary,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				Margin:     core.Edges{Right: 6},
			},
		},
	}

	if waiting > 0 {
		badgeBg := p.LineRaised
		if t.Kind == ToastWarning {
			badgeBg = p.Warning.Edge
		}
		badgeFg := p.TextMuted
		if t.Kind == ToastWarning {
			badgeFg = p.TextPrimary
		}
		children = append(children, core.View{
			TestID: "editor-toast-waiting",
			Style: core.Style{
				Height:         core.Pt(20),
				FlexShrink:     0,
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   6,
				Background:     badgeBg,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("+%d", waiting),
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11,
						FontWeight: 500,
						Color:      badgeFg,
					},
				},
			},
		})
	}

	if t.ActionLabel != "" {
		actionChildren := []core.Node{
			core.Text{
				Content: t.ActionLabel,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      p.TextPrimary,
				},
			},
		}
		if t.ActionShortcut != "" {
			actionChildren = append(actionChildren, core.Text{
				Content: t.ActionShortcut,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					FontWeight: 400,
					Color:      p.TextFaint,
				},
			})
		}
		actionStyle := core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			FlexShrink:     0,
			Gap:            6,
			Height:         core.Pt(28),
			Padding:        core.Symmetric(0, 10),
			CornerRadius:   8,
			Background:     actionBg,
			FocusVisible:   p.FocusRing(2),
			JustifyContent: core.JustifyCenter,
		}
		if actionBorder != core.Transparent {
			actionStyle.BorderWidth = 1
			actionStyle.BorderColor = actionBorder
		}
		children = append(children, core.View{
			TestID:    "editor-toast-action",
			Role:      core.RoleButton,
			Label:     t.ActionLabel,
			Focusable: func() bool { return true },
			Style:     actionStyle,
			OnPress: func(core.PressEvent) {
				if t.OnAction != nil {
					t.OnAction()
				}
			},
			Children: actionChildren,
		})
	}

	if t.canDismiss() {
		children = append(children, core.View{
			TestID:    "editor-toast-dismiss",
			Role:      core.RoleButton,
			Label:     "Dismiss",
			Focusable: func() bool { return true },
			Style: core.Style{
				Width:          core.Pt(28),
				Height:         core.Pt(28),
				FlexShrink:     0,
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				if onDismiss != nil {
					onDismiss()
				}
			},
			Children: []core.Node{
				core.Canvas{
					Style: core.Style{Width: core.Pt(14), Height: core.Pt(14)},
					Draw: func(d *core.Drawing, _ core.Size) {
						d.SetStrokeColor(p.TextFaint).SetLineWidth(1.6).SetLineCap(core.CapRound)
						d.BeginPath().MoveTo(3.5, 3.5).LineTo(10.5, 10.5).MoveTo(10.5, 3.5).LineTo(3.5, 10.5).Stroke()
					},
				},
			},
		})
	}

	if t.Kind == ToastProgress {
		frac := t.Progress
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		children = append(children, core.View{
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:     core.Absolute,
				Left:         core.Pt(12),
				Right:        core.Pt(12),
				Bottom:       core.Pt(0),
				Height:       core.Pt(2),
				CornerRadius: 1,
				Background:   p.LineRaised,
				Overflow:     core.Hidden,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pct(frac * 100),
						Height:       core.Pt(2),
						CornerRadius: 1,
						Background:   p.TextMuted,
					},
				},
			},
		})
	}

	return core.View{
		TestID: "editor-toast",
		Style: core.Style{
			Position:     core.Relative,
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			MaxWidth:     core.Pct(100),
			Height:       core.Pt(40),
			Padding:      core.Edges{Left: 12, Right: 6},
			CornerRadius: 12,
			Background:   bg,
			BorderWidth:  1,
			BorderColor:  border,
			Overflow:     core.Hidden,
			Shadows: []core.BoxShadow{
				ui.Shadow(0, 10, 28, 0, ui.Black(shadowAlpha)),
			},
		},
		Children: children,
	}
}

func specToastIcon(p SpecPalette, kind ToastKind) core.Node {
	return core.Canvas{
		Style: core.Style{Width: core.Pt(16), Height: core.Pt(16), FlexShrink: 0},
		Draw: func(d *core.Drawing, _ core.Size) {
			d.SetLineWidth(1.6).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
			switch kind {
			case ToastSuccess:
				d.SetStrokeColor(p.Added.Fg)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.BeginPath().MoveTo(5.2, 8.3).LineTo(7.1, 10.2).LineTo(11.1, 6.1).Stroke()
			case ToastWarning:
				d.SetStrokeColor(p.Warning.Fg)
				d.BeginPath().MoveTo(8, 2.2).LineTo(14.2, 13.4).LineTo(1.8, 13.4).ClosePath().Stroke()
				d.BeginPath().MoveTo(8, 6.4).LineTo(8, 9.6).Stroke()
				d.SetFillColor(p.Warning.Fg).BeginPath().Ellipse(8, 11.4, 0.85, 0.85).Fill()
			case ToastError:
				d.SetStrokeColor(p.Removed.Fg)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.BeginPath().MoveTo(8, 4.8).LineTo(8, 8.8).Stroke()
				d.SetFillColor(p.Removed.Fg).BeginPath().Ellipse(8, 11.2, 0.85, 0.85).Fill()
			case ToastProgress:
				d.SetStrokeColor(p.LineRaised)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.SetStrokeColor(p.TextMuted)
				d.BeginPath().Arc(8, 8, 6.2, -float32(math.Pi/2), 0, false).Stroke()
			default: // ToastInfo
				d.SetStrokeColor(p.TextMuted)
				d.BeginPath().Ellipse(8, 8, 6.2, 6.2).Stroke()
				d.BeginPath().MoveTo(8, 7.2).LineTo(8, 11.2).Stroke()
				d.SetFillColor(p.TextMuted).BeginPath().Ellipse(8, 5.1, 0.85, 0.85).Fill()
			}
		},
	}
}

// viewportOverlayLegendView renders the compact Spec overlay legend pill in the top-left of the viewport
// when Editor.OverlayLegendOpen is true (matching Spec.dc.html's line-style legend).
func viewportOverlayLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.OverlayLegendOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			row := func(testID, label string, col core.Color, dashed bool) core.Node {
				return core.View{
					TestID: testID,
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
						Height:     core.Pt(22),
					},
					Children: []core.Node{
						core.Canvas{
							Style: core.Style{Width: core.Pt(18), Height: core.Pt(10), FlexShrink: 0},
							Draw: func(d *core.Drawing, _ core.Size) {
								d.SetStrokeColor(col).SetLineWidth(1.8)
								if dashed {
									d.SetLineDash(4, 2.5)
								}
								d.BeginPath().MoveTo(1, 5).LineTo(17, 5).Stroke()
							},
						},
						core.Text{
							Content: label,
							Style: core.Style{
								FontSize:   11.5,
								FontWeight: 500,
								Color:      p.TextSecondary,
							},
						},
					},
				}
			}
			return core.View{
				TestID: "viewport-overlay-legend",
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(12),
					Top:          core.Pt(12),
					Padding:      core.Symmetric(8, 12),
					Gap:          2,
					CornerRadius: 10,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: "OVERLAYS",
						Style: core.Style{
							FontSize:      10.5,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
							Margin:        core.Edges{Bottom: 2},
						},
					},
					row("legend-selection", "Selection", p.Accent.Edge, false),
					row("legend-agent", "Agent working", p.Agent.Edge, false),
					row("legend-added", "Added", p.Added.Edge, true),
					row("legend-changed", "Changed", p.Changed.Edge, true),
					row("legend-removed", "Removed", p.Removed.Edge, false),
					row("legend-problem", "Problem", p.Problem().Edge, false),
				},
			}
		},
	}
}

// SpecOverlayLegendView renders the complete 6-card Spec.dc.html Overlay Legend board
// (Selection, Agent working, Added, Changed, Removed, Problem) on the active theme's viewport surround,
// using exclusively SpecPalette tokens so every outline (>= 3:1) and pill label (>= 4.5:1) can be
// inspected side by side in Dark and Light appearances.
func SpecOverlayLegendView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		p := ed.SpecPalette(cx)
		modeLabel := "Light theme"
		if p.Dark {
			modeLabel = "Dark theme"
		}

		card := func(testID, title, desc string, preview core.Node) core.Node {
			return core.View{
				TestID: testID,
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Gap:       8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Position:     core.Relative,
							Height:       core.Pt(164),
							CornerRadius: 12,
							Background:   p.Viewport,
							BorderWidth:  1,
							BorderColor:  p.Line,
							Overflow:     core.Hidden,
						},
						Children: []core.Node{preview},
					},
					core.Text{
						Content: title,
						Style: core.Style{
							FontSize:   14,
							FontWeight: 600,
							Color:      p.TextPrimary,
						},
					},
					core.Text{
						Content: desc,
						Style: core.Style{
							FontSize: 12.5,
							Color:    p.TextMuted,
						},
					},
				},
			}
		}

		drawVPBackdrop := func(d *core.Drawing, sz core.Size) {
			w, h := float32(sz.Width), float32(sz.Height)
			d.SetFillColor(p.Viewport).FillRect(0, 0, w, h)
			d.SetFillColor(p.ViewportDot)
			for y := float32(8); y < h; y += 16 {
				for x := float32(8); x < w; x += 16 {
					d.FillRect(x, y, 1.2, 1.2)
				}
			}
			// Floor strip using Spec Inset / LineRaised tokens.
			floorY := h - 34
			d.SetFillColor(p.Inset).FillRect(0, floorY, w, 34)
			d.SetFillColor(p.LineRaised).FillRect(0, floorY, w, 3)
		}

		pillNode := func(testID, label string, fg, bg, border core.Color, strikePrefix string) core.Node {
			kids := []core.Node{}
			if strikePrefix != "" {
				kids = append(kids, core.Text{
					Content: strikePrefix,
					Style: core.Style{
						FontFamily:     core.FontMonospace,
						FontSize:       11,
						FontWeight:     500,
						Color:          fg,
						TextDecoration: core.TextDecorationLineThrough,
					},
				})
			}
			kids = append(kids, core.Text{
				Content: label,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					FontWeight: 600,
					Color:      fg,
				},
			})
			st := core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				Gap:            6,
				Height:         core.Pt(22),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   11,
				Background:     bg,
				JustifyContent: core.JustifyCenter,
			}
			if border != core.Transparent {
				st.BorderWidth = 1
				st.BorderColor = border
			}
			return core.View{TestID: testID, Style: st, Children: kids}
		}

		selectionCard := card(
			"spec-legend-selection",
			"Selection",
			"accent · 1.5 px solid · four handles and a name tag.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
							d.SetStrokeColor(p.Accent.Edge).SetLineWidth(1.5).BeginPath().RoundRect(cx-18, fy-40, 36, 46, 6).Stroke()
							for _, pt := range [4][2]float32{{cx - 18, fy - 40}, {cx + 18, fy - 40}, {cx - 18, fy + 6}, {cx + 18, fy + 6}} {
								d.SetFillColor(p.Raised).FillRect(pt[0]-4, pt[1]-4, 8, 8)
								d.SetStrokeColor(p.Accent.Edge).SetLineWidth(1.5).StrokeRect(pt[0]-4, pt[1]-4, 8, 8)
							}
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(56),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.View{
								TestID: "spec-legend-selection-tag",
								Style: core.Style{
									Height:         core.Pt(20),
									Padding:        core.Symmetric(0, 8),
									CornerRadius:   5,
									Background:     p.Accent.Fg,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								Children: []core.Node{
									core.Text{
										Content: "Knight",
										Style: core.Style{
											FontSize:   11,
											FontWeight: 600,
											Color:      p.Accent.OnFill,
										},
									},
								},
							},
						},
					},
				},
			},
		)

		agentCard := card(
			"spec-legend-agent",
			"Agent working",
			"agent · 1.5 px solid · pulsing violet · agent pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(ui.Alpha(p.Agent.Fg, 0.28)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 8).Fill()
							d.SetStrokeColor(p.Agent.Edge).SetLineWidth(1.5).BeginPath().RoundRect(cx-19, fy-26, 38, 30, 10).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(68),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.View{
								TestID: "spec-legend-agent-pill",
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          6,
									Height:       core.Pt(22),
									Padding:      core.Symmetric(0, 8),
									CornerRadius: 11,
									Background:   p.PillBg,
									BorderWidth:  1,
									BorderColor:  p.Agent.Edge,
								},
								Children: []core.Node{
									core.Canvas{
										Style: core.Style{Width: core.Pt(10), Height: core.Pt(10)},
										Draw: func(d *core.Drawing, _ core.Size) {
											d.SetFillColor(p.Agent.Fg).BeginPath().MoveTo(5, 0.5).LineTo(6.6, 3.4).LineTo(9.5, 5).LineTo(6.6, 6.6).LineTo(5, 9.5).LineTo(3.4, 6.6).LineTo(0.5, 5).LineTo(3.4, 3.4).ClosePath().Fill()
										},
									},
									core.Text{
										Content: "placing slime_02",
										Style: core.Style{
											FontSize:   11,
											FontWeight: 500,
											Color:      p.TextSecondary,
										},
									},
								},
							},
						},
					},
				},
			},
		)

		addedCard := card(
			"spec-legend-added",
			"Added",
			"added · 1.5 px dashed · “+ name” pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(ui.Alpha(p.Added.Fg, 0.28)).BeginPath().RoundRect(cx-15, fy-22, 30, 22, 8).Fill()
							d.SetStrokeColor(p.Added.Edge).SetLineWidth(1.5).SetLineDash(5, 3).BeginPath().RoundRect(cx-19, fy-26, 38, 30, 10).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(68),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-added-pill", "+ slime_03", p.Added.Fg, p.Added.Fill, p.Added.Edge, ""),
						},
					},
				},
			},
		)

		changedCard := card(
			"spec-legend-changed",
			"Changed",
			"changed · 1.5 px dashed · field old → new pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(p.Changed.Fg).BeginPath().RoundRect(cx-12, fy-34, 24, 34, 4).Fill()
							d.SetStrokeColor(p.Changed.Edge).SetLineWidth(1.5).SetLineDash(5, 3).BeginPath().RoundRect(cx-17, fy-39, 34, 44, 6).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(56),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-changed-pill", "z 10 → 12", p.Changed.Fg, p.Changed.Fill, p.Changed.Edge, ""),
						},
					},
				},
			},
		)

		removedCard := card(
			"spec-legend-removed",
			"Removed",
			"removed · 1.5 px solid around ghost · struck-through name.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(ui.Alpha(p.TextFaint, 0.30)).BeginPath().Ellipse(cx, fy-16, 8, 8).Fill()
							d.SetStrokeColor(p.Removed.Edge).SetLineWidth(1.5).BeginPath().RoundRect(cx-13, fy-29, 26, 26, 6).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(64),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-removed-pill", "removed", p.Removed.Fg, p.Removed.Fill, p.Removed.Edge, "coin_04"),
						},
					},
				},
			},
		)

		problemCard := card(
			"spec-legend-problem",
			"Problem",
			"problem · 2 px solid on live object · filled pill.",
			core.View{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), Position: core.Relative},
				Children: []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, sz core.Size) {
							drawVPBackdrop(d, sz)
							cx := float32(sz.Width) / 2
							fy := float32(sz.Height) - 34
							d.SetFillColor(p.TextMuted).BeginPath().RoundRect(cx-14, fy-38, 28, 38, 4).Fill()
							d.SetStrokeColor(p.Problem().Edge).SetLineWidth(2).BeginPath().RoundRect(cx-18, fy-42, 36, 46, 6).Stroke()
						},
					},
					core.View{
						Style: core.Style{
							Position:       core.Absolute,
							Left:           core.Pt(0),
							Right:          core.Pt(0),
							Top:            core.Pt(52),
							Direction:      core.Row,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							pillNode("spec-legend-problem-pill", "door: lever_02 missing", p.Problem().OnFill, p.Problem().Fg, core.Transparent, ""),
						},
					},
				},
			},
		)

		return core.View{
			TestID: "spec-overlay-legend-board",
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Padding:    core.All(24),
				Gap:        20,
				Background: p.Bg,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{Gap: 4},
							Children: []core.Node{
								core.Text{
									Content: "Overlay legend",
									Style: core.Style{
										FontSize:   20,
										FontWeight: 600,
										Color:      p.TextPrimary,
									},
								},
								core.Text{
									Content: "One meaning per color and line. Every overlay also has a second cue: handles, a label pill, a ghost or a pulse.",
									Style: core.Style{
										FontSize: 12.5,
										Color:    p.TextFaint,
									},
								},
							},
						},
						core.View{
							Style: core.Style{
								Height:         core.Pt(26),
								Padding:        core.Symmetric(0, 10),
								CornerRadius:   13,
								Background:     p.Raised,
								BorderWidth:    1,
								BorderColor:    p.LineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							},
							Children: []core.Node{
								core.Text{
									Content: modeLabel,
									Style: core.Style{
										FontSize:   11.5,
										FontWeight: 600,
										Color:      p.TextSecondary,
									},
								},
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       16,
					},
					Children: []core.Node{selectionCard, agentCard, addedCard},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       16,
					},
					Children: []core.Node{changedCard, removedCard, problemCard},
				},
			},
		}
	})
}
