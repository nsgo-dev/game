package editor

import (
	"fmt"
	"sync"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/postfx"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

var (
	colorVisionPreviewModesList = []a11y.ColorVision{
		a11y.VisionNormal,
		a11y.VisionProtanopia,
		a11y.VisionDeuteranopia,
		a11y.VisionTritanopia,
	}

	protanopiaPreviewChain = sync.OnceValue(func() *postfx.Chain {
		return postfx.MustCompile(postfx.ColorVisionEffect(postfx.ColorVisionProtanopia, 1.0, false))
	})
	deuteranopiaPreviewChain = sync.OnceValue(func() *postfx.Chain {
		return postfx.MustCompile(postfx.ColorVisionEffect(postfx.ColorVisionDeuteranopia, 1.0, false))
	})
	tritanopiaPreviewChain = sync.OnceValue(func() *postfx.Chain {
		return postfx.MustCompile(postfx.ColorVisionEffect(postfx.ColorVisionTritanopia, 1.0, false))
	})
)

func colorVisionPreviewChain(mode a11y.ColorVision) *postfx.Chain {
	switch mode {
	case a11y.VisionProtanopia:
		return protanopiaPreviewChain()
	case a11y.VisionDeuteranopia:
		return deuteranopiaPreviewChain()
	case a11y.VisionTritanopia:
		return tritanopiaPreviewChain()
	default:
		return nil
	}
}

func normalizeColorVisionPreview(mode a11y.ColorVision) a11y.ColorVision {
	switch mode {
	case a11y.VisionProtanopia, a11y.VisionDeuteranopia, a11y.VisionTritanopia:
		return mode
	default:
		return a11y.VisionNormal
	}
}

// ColorVisionPreviewModes returns the color-vision simulation preview modes in
// display order: Normal, Protanopia, Deuteranopia, and Tritanopia.
func (e *Editor) ColorVisionPreviewModes() []a11y.ColorVision {
	out := make([]a11y.ColorVision, len(colorVisionPreviewModesList))
	copy(out, colorVisionPreviewModesList)
	return out
}

// ColorVisionPreview returns the active color-vision deficiency simulation mode
// for the scene viewport and HUD layout preview (a11y.VisionNormal when off).
func (e *Editor) ColorVisionPreview() a11y.ColorVision {
	if e == nil {
		return a11y.VisionNormal
	}
	return normalizeColorVisionPreview(e.colorVisionPreview)
}

// SetColorVisionPreview selects the active color-vision deficiency simulation mode
// for both the 2D scene viewport and the HUD layout preview.
func (e *Editor) SetColorVisionPreview(mode a11y.ColorVision) {
	if e == nil {
		return
	}
	norm := normalizeColorVisionPreview(mode)
	if e.colorVisionPreview == norm {
		return
	}
	e.colorVisionPreview = norm
	e.hud.rev++
	if norm == a11y.VisionNormal {
		e.Status = "Color-vision preview off"
	} else {
		e.Status = "Color-vision preview: " + norm.String()
	}
	e.notify()
}

// CycleColorVisionPreview advances the color-vision preview through Normal,
// Protanopia, Deuteranopia, Tritanopia, and back to Normal.
func (e *Editor) CycleColorVisionPreview() a11y.ColorVision {
	if e == nil {
		return a11y.VisionNormal
	}
	cur := e.ColorVisionPreview()
	for i, m := range colorVisionPreviewModesList {
		if m == cur {
			next := colorVisionPreviewModesList[(i+1)%len(colorVisionPreviewModesList)]
			e.SetColorVisionPreview(next)
			return next
		}
	}
	e.SetColorVisionPreview(a11y.VisionNormal)
	return a11y.VisionNormal
}

// ColorVisionPreviewEffect returns the simulation postfx.Effect for the editor's
// active ColorVisionPreview mode, or (zero, false) when preview is Normal.
func (e *Editor) ColorVisionPreviewEffect() (postfx.Effect, bool) {
	return a11y.ColorVisionSimulation(e.ColorVisionPreview(), 1.0)
}

func (e *Editor) previewColor(c core.Color) core.Color {
	if e == nil || c == 0 {
		return c
	}
	mode := e.ColorVisionPreview()
	if mode == a11y.VisionNormal {
		return c
	}
	return a11y.Simulate(c, mode)
}

func (e *Editor) colorVisionPreviewLabel(mode a11y.ColorVision) string {
	switch mode {
	case a11y.VisionProtanopia:
		return e.T("overlays.colorVision.protanopia")
	case a11y.VisionDeuteranopia:
		return e.T("overlays.colorVision.deuteranopia")
	case a11y.VisionTritanopia:
		return e.T("overlays.colorVision.tritanopia")
	default:
		return e.T("overlays.colorVision.normal")
	}
}

// HUDContrastRow summarizes a HUD text/button node's foreground and background colors
// and WCAG contrast ratio both normally and under the active color-vision preview.
type HUDContrastRow struct {
	NodeID         string
	Foreground     core.Color
	Background     core.Color
	SimForeground  core.Color
	SimBackground  core.Color
	NormalRatio    float64
	SimulatedRatio float64
}

// HUDContrastRows returns per-node contrast measurements for the active HUD screen,
// including both normal and simulated WCAG 2.1 contrast ratios under ColorVisionPreview.
func (e *Editor) HUDContrastRows() []HUDContrastRow {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	mode := e.ColorVisionPreview()
	var rows []HUDContrastRow
	var walk func(nodes []gameui.ScreenNodeDoc)
	walk = func(nodes []gameui.ScreenNodeDoc) {
		for i := range nodes {
			n := &nodes[i]
			if n.Type == gameui.NodeLabel || n.Type == gameui.NodeButton {
				fg, bg := hudNodeContrastColors(rec.doc, n)
				simFG := a11y.Simulate(fg, mode)
				simBG := a11y.Simulate(bg, mode)
				rows = append(rows, HUDContrastRow{
					NodeID:         n.ID,
					Foreground:     fg,
					Background:     bg,
					SimForeground:  simFG,
					SimBackground:  simBG,
					NormalRatio:    a11y.ContrastRatio(fg, bg),
					SimulatedRatio: a11y.ContrastRatio(simFG, simBG),
				})
			}
			walk(n.Children)
		}
	}
	walk(rec.doc.Nodes)
	return rows
}

// viewportColorVisionOverlayView renders a live GPU color-vision simulation pass
// (via postfx.View over a retained core.OffscreenImage of DrawViewport) and an
// active-mode badge pill whenever ColorVisionPreview is not VisionNormal.
func viewportColorVisionOverlayView(
	cx *core.Scope,
	ed *Editor,
	rev, tileAnimRev, agentPulseRev, overlayAnimRev *core.Signal[uint64],
) core.Node {
	active := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ViewMode == ViewMode2D && ed.ColorVisionPreview() != a11y.VisionNormal
	})
	return core.Show{
		When: active.Get,
		Then: func(cx *core.Scope) core.Node {
			cw, ch := ed.CanvasSize()
			if cw <= 0 || ch <= 0 {
				cw, ch = 640, 360
			}
			off := cx.NewOffscreenImage(core.Size{Width: core.Points(cw), Height: core.Points(ch)}, 0, func(d *core.Drawing) {
				w, h := ed.CanvasSize()
				if w <= 0 || h <= 0 {
					w, h = 640, 360
				}
				ed.DrawViewport(d, core.Size{Width: core.Points(w), Height: core.Points(h)})
			})
			cx.Effect(func() {
				_ = rev.Get()
				_ = tileAnimRev.Get()
				_ = agentPulseRev.Get()
				_ = overlayAnimRev.Get()
				w, h := ed.CanvasSize()
				if w > 0 && h > 0 {
					off.Resize(core.Size{Width: core.Points(w), Height: core.Points(h)}, 0)
				}
				off.Invalidate()
			})

			p := ed.Palette(cx)
			return core.View{
				TestID:        "viewport-color-vision-layer",
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Position: core.Absolute,
					Left:     core.Pt(0),
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = rev.Get()
						mode := ed.ColorVisionPreview()
						chain := colorVisionPreviewChain(mode)
						if chain == nil {
							return core.Group{}
						}
						return core.View{
							PointerEvents: core.PointerEventsNone,
							Style: core.Style{
								Position: core.Absolute,
								Left:     core.Pt(0),
								Top:      core.Pt(0),
								Right:    core.Pt(0),
								Bottom:   core.Pt(0),
							},
							Children: []core.Node{
								postfx.View{
									TestID:    "viewport-color-vision-shader",
									Chain:     chain,
									FrameFunc: off.Source,
									Style: core.Style{
										Width:  core.Pct(100),
										Height: core.Pct(100),
									},
								},
							},
						}
					}),
					core.View{
						TestID: "viewport-color-vision-badge",
						Style: core.Style{
							Position:     core.Absolute,
							Top:          core.Pt(12),
							Left:         core.Pt(12),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          6,
							Padding:      core.Edges{Left: 10, Right: 4, Top: 4, Bottom: 4},
							CornerRadius: 8,
							Background:   ui.Alpha(p.Raised, 0.96),
							BorderWidth:  1,
							BorderColor:  p.Warning.Edge,
						},
						Children: []core.Node{
							core.SVGView{
								SVG:   lucide.Eye(),
								Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: p.Warning.Fg},
							},
							core.Text{
								TestID: "viewport-color-vision-badge-label",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.colorVisionBadge", l10n.Param("mode", ed.colorVisionPreviewLabel(ed.ColorVisionPreview())))
								},
								Style: core.Style{
									FontSize:   11.5,
									FontWeight: core.FontWeightMedium,
									Color:      p.TextPrimary,
								},
							},
							ui.Button{
								TestID: "viewport-color-vision-clear",
								Icon:   lucide.X,
								LabelFunc: func() string {
									_ = rev.Get()
									return ed.T("overlays.colorVisionReset")
								},
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Side:     22,
								IconSize: 12,
								Tint:     func() core.Color { return p.TextMuted },
								Style:    core.Style{CornerRadius: 5},
								OnPress: func() {
									ed.SetColorVisionPreview(a11y.VisionNormal)
								},
							},
						},
					},
				},
			}
		},
	}
}

// hudColorVisionPanelView renders the color-vision simulation toggle and live contrast
// table beside the floating TEXT WIDTH, PX table in HUDLayoutView.
func hudColorVisionPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], stableTick func()) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		stableTick()
		_ = rev.Get()
		p := ed.Palette(cx)
		activeMode := ed.ColorVisionPreview()
		rows := ed.HUDContrastRows()
		selID := ed.HUDSelectedNodeID()

		modeBtns := make([]core.Node, 0, len(colorVisionPreviewModesList))
		for _, m := range colorVisionPreviewModesList {
			mode := m
			on := mode == activeMode
			bg := core.Transparent
			fg := p.TextFaint
			if on {
				bg = p.LineRaised
				fg = p.TextPrimary
			}
			modeBtns = append(modeBtns, core.View{
				TestID: "hud-color-vision-" + mode.String(),
				Role:   core.RoleButton,
				Label:  ed.T("hudLayout.colorVisionModeLabel", l10n.Param("mode", ed.colorVisionPreviewLabel(mode))),
				Style: core.Style{
					Height:         core.Pt(24),
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Padding:        core.Symmetric(0, 8),
					CornerRadius:   6,
					Background:     bg,
					Hover:          &core.Style{Background: p.Raised},
					FocusVisible:   p.FocusRing(2),
				},
				OnPress: func(core.PressEvent) {
					ed.SetColorVisionPreview(mode)
				},
				Children: []core.Node{
					core.Text{
						Content: ed.colorVisionPreviewLabel(mode),
						Style: core.Style{
							FontSize:   11.5,
							FontWeight: core.FontWeightMedium,
							Color:      fg,
						},
					},
				},
			})
		}

		children := []core.Node{
			core.View{
				Style: core.Style{
					Height:     core.Pt(32),
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
					Padding:    core.Symmetric(0, 12),
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("hudLayout.colorVisionHeading"),
						Style: core.Style{
							FlexGrow:      1,
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.66,
							Color:         p.TextFaint,
						},
					},
					core.Text{
						Content: ed.T("hudLayout.colorVisionRatio"),
						Style: core.Style{
							Width:         core.Pt(56),
							TextAlign:     core.TextAlignEnd,
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.66,
							Color:         p.TextFaint,
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        2,
					Padding:    core.Edges{Left: 8, Right: 8, Bottom: 6},
				},
				Children: modeBtns,
			},
		}

		for _, r := range rows {
			row := r
			on := row.NodeID == selID
			bg := core.Transparent
			nameColor := p.TextSecondary
			if on {
				bg = ui.Alpha(p.Accent.Fg, 0.14)
				nameColor = p.TextPrimary
			}
			ratio := row.SimulatedRatio
			ratioColor := p.TextMuted
			if ratio < 4.5 {
				ratioColor = p.Removed.Fg
			}
			children = append(children, core.View{
				TestID: "hud-contrast-row-" + row.NodeID,
				Role:   core.RoleButton,
				Label:  ed.T("hudLayout.contrastRowLabel", l10n.Param("id", row.NodeID), l10n.Param("ratio", fmt.Sprintf("%.1f", ratio))),
				Style: core.Style{
					Height:       core.Pt(26),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(0, 12),
					Background:   bg,
					Hover:        &core.Style{Background: p.Raised},
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.SelectHUDNode(row.NodeID)
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(18),
							Height:         core.Pt(14),
							CornerRadius:   3,
							Background:     row.SimBackground,
							BorderWidth:    1,
							BorderColor:    p.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(8),
									Height:       core.Pt(6),
									CornerRadius: 1,
									Background:   row.SimForeground,
								},
							},
						},
					},
					core.Text{
						Content: row.NodeID,
						Style: core.Style{
							FlexGrow: 1,
							FontSize: 12,
							Color:    nameColor,
						},
					},
					core.Text{
						TestID:  "hud-contrast-ratio-" + row.NodeID,
						Content: fmt.Sprintf("%.1f : 1", ratio),
						Style: core.Style{
							Width:      core.Pt(56),
							TextAlign:  core.TextAlignEnd,
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      ratioColor,
						},
					},
				},
			})
		}

		return core.View{
			TestID: "hud-color-vision-panel",
			Role:   core.RoleRegion,
			Label:  ed.T("hudLayout.colorVisionRegionLabel"),
			Style: core.Style{
				Width:        core.Pt(328),
				Padding:      core.Edges{Bottom: 8},
				CornerRadius: 12,
				Background:   p.Panel,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
			},
			Children: children,
		}
	})
}
