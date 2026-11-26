package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// viewportOverlaysBarView renders the floating 2D viewport bar at bottom-center matching
// docs/design/editor/Inspector.dc.html and docs/design/editor/Spec.dc.html:
// Zoom (− / % / +), Snap, Fit, and the Overlays N button opening a ui.Popover titled
// "SHOW IN VIEWPORT" with a switch and live count per overlay plus the agent screenshot note.
func viewportOverlaysBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	visible := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ViewMode == ViewMode2D && !ed.Playing() && ed.WorkspaceState() == WorkspaceReady
	})
	overlayListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		if ed.SpecPalette(cx).Dark {
			b.WriteString("D:")
		} else {
			b.WriteString("L:")
		}
		b.WriteString(ed.ResolvedLanguage())
		for _, o := range ed.ViewportOverlays() {
			b.WriteByte('\x00')
			b.WriteString(o.ID)
			b.WriteByte('=')
			b.WriteString(ed.overlayDisplayName(o))
		}
		return b.String()
	})

	zoomBy := func(factor float32) {
		cw, ch := ed.CanvasSize()
		ed.ZoomView2DAt(cw*0.5, ch*0.5, factor)
	}

	return core.Show{
		When: visible.Get,
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			barDivider := func() core.Node {
				return core.View{
					Style: core.Style{
						Width:      core.Pt(1),
						Height:     core.Pt(16),
						Background: p.LineRaised,
						Margin:     core.Symmetric(0, 2),
					},
				}
			}

			popoverContent := core.View{
				TestID: "viewport-overlays-popover",
				Style: core.Style{
					Gap: 4,
				},
				Children: []core.Node{
					core.Text{
						TestID: "viewport-overlays-title",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.T("overlays.popoverTitle")
						},
						Style: core.Style{
							FontSize:      11,
							FontWeight:    600,
							LetterSpacing: 0.6,
							Color:         p.TextFaint,
							Padding:       core.Edges{Left: 6, Right: 6, Top: 2, Bottom: 4},
						},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = overlayListKey.Get()
						rowPal := ed.SpecPalette(cx)
						ovs := ed.viewportOverlaysFor(rowPal)
						rows := make([]core.Node, 0, len(ovs))
						for _, ov := range ovs {
							o := ov
							rows = append(rows, viewportOverlayRowView(ed, rev, rowPal, o))
						}
						return core.View{
							Style:    core.Style{Gap: 2},
							Children: rows,
						}
					}),
					core.View{
						Style: core.Style{
							Height:     core.Pt(1),
							Background: p.LineRaised,
							Margin:     core.Symmetric(4, 6),
						},
					},
					core.View{
						TestID: "viewport-overlays-footer",
						Style: core.Style{
							Padding: core.Edges{Left: 6, Right: 6, Top: 2, Bottom: 2},
							Gap:     2,
						},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.agentNote")
								},
								Style: core.Style{
									FontSize:   11.5,
									LineHeight: 16,
									Color:      p.TextFaint,
								},
							},
							core.Text{
								Content: "viewport_overlays",
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   11,
									Color:      p.TextMuted,
								},
							},
						},
					},
				},
			}

			return core.View{
				TestID:        "viewport-overlay-bar-wrap",
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Position:   core.Absolute,
					Left:       core.Pt(0),
					Right:      core.Pt(0),
					Bottom:     core.Pt(12),
					AlignItems: core.AlignCenter,
				},
				Children: []core.Node{
					core.View{
						TestID: "viewport-overlay-bar",
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          2,
							Padding:      core.All(4),
							CornerRadius: 10,
							Background:   ui.Alpha(p.Raised, 0.96),
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
						},
						Children: []core.Node{
							ui.Button{
								TestID: "viewport-zoom-out",
								Icon:   lucide.Minus,
								LabelFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.zoomOut")
								},
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Side:     28,
								IconSize: 13,
								Tint:     func() core.Color { return p.TextMuted },
								Style: core.Style{
									CornerRadius: 6,
								},
								OnPress: func() { zoomBy(1 / 1.25) },
							},
							core.Text{
								TestID: "viewport-zoom-label",
								ContentFunc: func() string {
									_ = rev.Get()
									pct := int(math.Round(float64(ed.View2D().Zoom * 100)))
									return fmt.Sprintf("%d%%", pct)
								},
								Style: core.Style{
									Width:      core.Pt(46),
									TextAlign:  core.TextAlignCenter,
									FontFamily: core.FontMonospace,
									FontSize:   11,
									Color:      p.TextMuted,
								},
							},
							ui.Button{
								TestID: "viewport-zoom-in",
								Icon:   lucide.Plus,
								LabelFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.zoomIn")
								},
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Side:     28,
								IconSize: 13,
								Tint:     func() core.Color { return p.TextMuted },
								Style: core.Style{
									CornerRadius: 6,
								},
								OnPress: func() { zoomBy(1.25) },
							},
							barDivider(),
							base.Switch{
								TestID: "viewport-bar-snap",
								Label:  ed.T("overlays.snapLabel"),
								Checked: func() bool {
									_ = rev.Get()
									return ed.SnapToGrid
								},
								OnChange: func(on bool) {
									ed.SetSnapToGrid(on)
								},
								StyleFunc: func() core.Style {
									_ = rev.Get()
									cur := ed.SpecPalette(cx)
									bg := core.Transparent
									if ed.SnapToGrid {
										bg = cur.LineRaised
									}
									return core.Style{
										Direction:      core.Row,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										Height:         core.Pt(specRowHeight),
										MinHeight:      core.Pt(specRowHeight),
										MinWidth:       core.Pt(specRowHeight),
										Padding:        core.Symmetric(0, specSpace8),
										CornerRadius:   specRadiusControl,
										Background:     bg,
										Cursor:         core.CursorPointer,
										Hover:          &core.Style{Background: cur.LineRaised},
										FocusVisible:   cur.FocusRing(2),
									}
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											return ed.T("overlays.snap")
										},
										StyleFunc: func() core.Style {
											_ = rev.Get()
											cur := ed.SpecPalette(cx)
											col := cur.TextFaint
											if ed.SnapToGrid {
												col = cur.TextPrimary
											}
											return core.Style{
												FontSize:   12,
												FontWeight: core.FontWeightMedium,
												Color:      col,
											}
										},
									},
								},
							},
							ui.Button{
								TestID: "viewport-bar-fit",
								TextFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.fit")
								},
								LabelFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.fitLabel")
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextMuted,
								},
								Style: core.Style{
									Height:       core.Pt(specRowHeight),
									MinHeight:    core.Pt(specRowHeight),
									MinWidth:     core.Pt(specRowHeight),
									Padding:      core.Symmetric(0, 8),
									CornerRadius: 6,
								},
								OnPress: func() {
									ed.FrameAll2D()
								},
							},
							barDivider(),
							ui.Popover{
								TestID:    "viewport-overlays-button",
								Placement: core.PlaceTop,
								Align:     core.AlignEnd,
								Open: func() bool {
									_ = rev.Get()
									return ed.OverlaysPopoverOpen()
								},
								OnOpenChange: func(open bool, _ base.Reason) {
									ed.SetOverlaysPopoverOpen(open)
								},
								Trigger: ui.Button{
									Icon: lucide.Layers,
									TextFunc: func() string {
										_ = rev.Get()
										return fmt.Sprintf("%s %d", ed.T("overlays.button"), ed.ActiveViewportOverlayCount())
									},
									LabelFunc: func() string {
										_ = rev.Get()
										return fmt.Sprintf("%s (%d)", ed.T("overlays.button"), ed.ActiveViewportOverlayCount())
									},
									Size:     ui.SizeSmall,
									Variant:  ui.ButtonGhost,
									IconSize: 14,
									Color: func() core.Color {
										_ = rev.Get()
										if ed.OverlaysPopoverOpen() {
											return p.LineRaised
										}
										return core.Transparent
									},
									Tint: func() core.Color { return p.TextPrimary },
									TextStyle: core.Style{
										FontSize:   12,
										FontWeight: 500,
										Color:      p.TextPrimary,
									},
									Style: core.Style{
										Height:       core.Pt(28),
										Padding:      core.Symmetric(0, 10),
										CornerRadius: 6,
									},
								},
								Style: core.Style{
									Width:        core.Pt(272),
									Padding:      core.All(8),
									Gap:          4,
									CornerRadius: 12,
									Background:   p.Raised,
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
								},
								Children: []core.Node{popoverContent},
							},
						},
					},
				},
			}
		},
	}
}

func viewportOverlayRowView(ed *Editor, rev *core.Signal[uint64], p SpecPalette, o ViewportOverlay) core.Node {
	id := o.ID
	name := ed.overlayDisplayName(o)
	col := o.Color
	if col == 0 {
		col = p.TextMuted
	}
	swatch := core.Canvas{
		Style: core.Style{
			Width:      core.Pt(16),
			Height:     core.Pt(10),
			FlexShrink: 0,
		},
		Draw: func(d *core.Drawing, size core.Size) {
			w, h := float32(size.Width), float32(size.Height)
			d.Save()
			switch o.Swatch {
			case OverlaySwatchBox:
				d.SetStrokeColor(col).SetLineWidth(1.25)
				d.StrokeRect(1, 1, w-2, h-2)
			case OverlaySwatchDashedBox:
				if col == p.Warning.Fg {
					d.SetFillColor(ui.Alpha(p.Warning.Fg, 0.14)).FillRect(1, 1, w-2, h-2)
				}
				d.SetStrokeColor(col).SetLineWidth(1.25).SetLineDash(2, 2)
				d.StrokeRect(1, 1, w-2, h-2)
			case OverlaySwatchLine:
				d.SetStrokeColor(col).SetLineWidth(1.5)
				d.BeginPath().MoveTo(1, h*0.5).LineTo(w-1, h*0.5).Stroke()
			case OverlaySwatchDashedLine:
				d.SetStrokeColor(col).SetLineWidth(1.25).SetLineDash(2, 2)
				d.BeginPath().MoveTo(1, h*0.5).LineTo(w-1, h*0.5).Stroke()
			case OverlaySwatchRegion:
				d.SetFillColor(ui.Alpha(p.TextMuted, 0.25)).FillRect(1, 2, w-2, h-3)
				d.SetStrokeColor(col).SetLineWidth(1.25).SetLineDash(2, 2)
				d.BeginPath().MoveTo(1, 2).LineTo(w-1, 2).Stroke()
			case OverlaySwatchLabel:
				d.SetStrokeColor(col).SetLineWidth(2)
				d.BeginPath().MoveTo(2, h-2).LineTo(w-2, h-2).Stroke()
			}
			d.Restore()
		},
	}

	return core.View{
		TestID: "viewport-overlay-row-" + id,
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Height:     core.Pt(28),
			Gap:        8,
			Padding:    core.Symmetric(0, 6),
		},
		Children: []core.Node{
			swatch,
			core.Text{
				Content:  name,
				MaxLines: 1,
				Style: core.Style{
					FlexGrow: 1,
					FontSize: 12.5,
					Color:    p.TextSecondary,
				},
			},
			core.Text{
				TestID: "viewport-overlay-count-" + id,
				ContentFunc: func() string {
					_ = rev.Get()
					if o.Count == nil {
						return ""
					}
					n := max(0, o.Count(ed))
					if o.CountUnit != nil {
						return o.CountUnit(ed, n)
					}
					return fmt.Sprintf("%d", n)
				},
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   11,
					Color:      p.TextFaint,
				},
			},
			SpecSwitch{
				TestID: "viewport-overlay-switch-" + id,
				Label:  name,
				Size:   ui.SizeSmall,
				Checked: func() bool {
					_ = rev.Get()
					return ed.ViewportOverlayEnabled(id)
				},
				OnChange: func(on bool) {
					ed.report(ed.SetViewportOverlay(id, on))
				},
			},
		},
	}
}
