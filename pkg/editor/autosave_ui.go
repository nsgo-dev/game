package editor

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// saveStatusBarView renders the status bar's save status item matching design/editor/Checkpoints.dc.html
// ("Save status · status bar") using theme-aware theme.Palette tokens (Dark & Light):
//   - Saved: muted Spec styling with a green check icon; hovering shows the last save time.
//   - Saving…: appears only after SavingIndicatorDelay (300 ms) so quick saves do not flicker.
//   - Failed autosave: persistent alert pill with problem red border/icon, failure reason, Retry, and Save as… buttons.
func saveStatusBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type statusChromeKey struct {
		kind SaveStatusKind
		lang string
		dark bool
	}
	chromeKey := core.NewMemo(cx, func() statusChromeKey {
		_ = rev.Get()
		return statusChromeKey{
			kind: ed.SaveStatus().Kind,
			lang: ed.ResolvedLanguage(),
			dark: cx.Dark(),
		}
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		key := chromeKey.Get()
		p := ed.Palette(cx)
		switch key.kind {
		case SaveStatusFailed:
			prob := p.Problem()
			return core.View{
				TestID: "status-save-error",
				Role:   core.RoleAlert,
				LabelFunc: func() string {
					_ = rev.Get()
					return ed.SaveStatus().ErrorMessage
				},
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Height:       core.Pt(26),
					Padding:      core.Edges{Left: 10, Right: 4},
					CornerRadius: 8,
					Background:   prob.Fill,
					BorderWidth:  1,
					BorderColor:  prob.Edge,
					FlexShrink:   0,
				},
				Children: []core.Node{
					core.Icon{
						SVG: lucide.CircleAlert(),
						Style: core.Style{
							Width:      core.Pt(13),
							Height:     core.Pt(13),
							Color:      prob.Fg,
							FlexShrink: 0,
						},
					},
					core.Text{
						TestID: "status-save-error-text",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.SaveStatus().ErrorMessage
						},
						MaxLines:      1,
						EllipsizeMode: core.EllipsizeTail,
						Style: core.Style{
							FontSize:   11.5,
							Color:      p.TextPrimary,
							MaxWidth:   core.Pt(360),
							FlexShrink: 1,
						},
					},
					ui.Button{
						TestID:  "status-save-retry",
						Text:    ed.T("saveStatus.retry"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color:   func() core.Color { return p.LineRaised },
						TextStyle: core.Style{
							FontSize: 11.5,
							Color:    p.TextPrimary,
						},
						Style: core.Style{
							Height:       core.Pt(20),
							CornerRadius: 6,
							Padding:      core.Symmetric(0, 8),
							FocusVisible: p.FocusRing(2),
						},
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing()
						},
						OnPress: func() {
							_ = ed.RetrySave()
						},
					},
					ui.Button{
						TestID:  "status-save-as",
						Text:    ed.T("saveStatus.saveAs"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonGhost,
						Color:   func() core.Color { return p.LineRaised },
						TextStyle: core.Style{
							FontSize: 11.5,
							Color:    p.TextPrimary,
						},
						Style: core.Style{
							Height:       core.Pt(20),
							CornerRadius: 6,
							Padding:      core.Symmetric(0, 8),
							FocusVisible: p.FocusRing(2),
						},
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing() || ed.Project == nil || ed.ProjectPath == ""
						},
						OnPress: func() {
							ed.openSaveAsFromStatusBar()
						},
					},
				},
			}

		case SaveStatusSaving:
			return core.View{
				TestID: "status-save-saving",
				Role:   core.RoleStatus,
				Label:  ed.T("saveStatus.saving"),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
					Height:     core.Pt(22),
					Padding:    core.Symmetric(0, 8),
					FlexShrink: 0,
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "status-save-spinner",
						Style: core.Style{
							Width:      core.Pt(12),
							Height:     core.Pt(12),
							FlexShrink: 0,
						},
						Draw: func(d *core.Drawing, size core.Size) {
							cx := float32(size.Width) / 2
							cy := float32(size.Height) / 2
							r := min(cx, cy) - 1
							d.SetStrokeColor(p.TextFaint).
								SetLineWidth(1.5).
								SetLineCap(core.CapRound).
								BeginPath().
								Arc(cx, cy, r, float32(-math.Pi/2), float32(math.Pi), false).
								Stroke()
						},
					},
					core.Text{
						TestID:  "status-save-saving-text",
						Content: ed.T("saveStatus.saving"),
						Style: core.Style{
							FontSize: 11.5,
							Color:    p.TextSecondary,
						},
					},
				},
			}

		default:
			return ui.Tooltip{
				TestID:   "status-save-tooltip",
				TextFunc: func() string { _ = rev.Get(); return ed.SaveStatusTooltip() },
				Trigger: core.View{
					TestID: "status-save-saved",
					Role:   core.RoleStatus,
					LabelFunc: func() string {
						_ = rev.Get()
						return ed.SaveStatusTooltip()
					},
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        6,
						Height:     core.Pt(22),
						Padding:    core.Symmetric(0, 6),
						FlexShrink: 0,
					},
					Children: []core.Node{
						core.Icon{
							SVG: lucide.Check(),
							Style: core.Style{
								Width:      core.Pt(13),
								Height:     core.Pt(13),
								Color:      p.Added.Fg,
								FlexShrink: 0,
							},
						},
						core.Text{
							TestID:  "status-save-saved-text",
							Content: ed.T("saveStatus.saved"),
							Style: core.Style{
								FontSize: 11.5,
								Color:    p.TextFaint,
							},
						},
					},
				},
			}
		}
	})
}
