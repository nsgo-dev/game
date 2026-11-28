package editor

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// SceneTabsView renders the horizontal tab bar above the viewport: one tab per open scene showing its name, a dirty marker when it has unsaved edits, and a close button. Pressing a tab switches to it without clearing any undo history; closing a dirty tab prompts to save, discard or cancel.
func SceneTabsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	var lastTabs []SceneTab
	computed, lastPlaying := false, false
	tabs := core.NewMemoFunc(cx, func() []SceneTab {
		r := rev.Get()
		playing := ed.Playing()
		rigStepOnly := ed.RigPlaying() && r == ed.rigStepRev
		// Skip per-frame YAML dirty checks while Play or Rig playback ticks every frame.
		if !computed || (!playing && !rigStepOnly) || (playing && !lastPlaying) {
			lastTabs = ed.Tabs()
			computed = true
		}
		lastPlaying = playing
		return lastTabs
	}, slices.Equal)

	playing := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	return core.View{
		TestID: "scene-tabs",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Height:      core.Pt(32),
			Background:  colors.Inset(),
			Borders:     core.Edges{Bottom: 1},
			BorderColor: colors.Line(),
			Padding:     core.Symmetric(0, 6),
		},
		Children: []core.Node{
			ui.ScrollView{
				Horizontal: true,
				Style: core.Style{
					FlexGrow: 1,
					Height:   core.Pct(100),
				},
				ContentStyle: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        4,
					Height:     core.Pct(100),
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						p := ed.Palette(cx)
						list := tabs.Get()
						nodes := make([]core.Node, 0, len(list))
						for i, tab := range list {
							idx := i
							t := tab
							bg := colors.Inset()
							textColor := colors.Muted()
							borderBottom := core.Points(0)
							borderBottomColor := colors.Line()
							fontWeight := core.FontWeight(500)
							if t.Active {
								bg = colors.Surface()
								textColor = colors.Text()
								borderBottom = 2
								borderBottomColor = colors.Accent()
								fontWeight = core.FontWeight(700)
							}
							labelChildren := []core.Node{
								core.Text{
									TestID:   fmt.Sprintf("scene-tab-label-%d", idx),
									Content:  t.Label,
									MaxLines: 1,
									Style: core.Style{
										FontSize:   12,
										FontWeight: fontWeight,
										Color:      textColor,
									},
								},
							}
							if t.Dirty || t.SaveFailed {
								dotBg := p.Changed.Fg
								dotLabel := ed.T("toolbar.unsavedLabel")
								if t.SaveFailed {
									dotBg = p.Problem().Fg
									dotLabel = ed.T("toolbar.unsavedFailedLabel")
								}
								labelChildren = append(labelChildren, core.View{
									TestID: fmt.Sprintf("scene-tab-dirty-%d", idx),
									Label:  dotLabel,
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 3,
										Background:   dotBg,
									},
								})
							}
							a11yLabel := t.Path
							if a11yLabel == "" {
								a11yLabel = t.Label
							}
							nodes = append(nodes, core.View{
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          4,
									Padding:      core.Edges{Left: 8, Right: 4, Top: 4, Bottom: 4},
									CornerRadius: 4,
									Background:   bg,
									Borders:      core.Edges{Bottom: borderBottom},
									BorderColors: core.EdgeColors{Bottom: borderBottomColor},
								},
								Children: []core.Node{
									core.View{
										TestID: fmt.Sprintf("scene-tab-%d", idx),
										Label:  a11yLabel,
										Style: core.Style{
											Direction:  core.Row,
											AlignItems: core.AlignCenter,
											Gap:        4,
										},
										OnPress: func(core.PressEvent) {
											if !ed.Playing() {
												ed.report(ed.RequestSelectTab(idx))
											}
										},
										Children: labelChildren,
									},
									ui.IconButton{
										TestID:   fmt.Sprintf("scene-tab-close-%d", idx),
										Icon:     lucide.X,
										Size:     18,
										IconSize: 11,
										Label:    ed.T("tabsExtra.closeTab", l10n.Param("label", t.Label)),
										Disabled: playing,
										OnPress: func() {
											if !ed.Playing() {
												ed.report(ed.RequestCloseTab(idx))
											}
										},
									},
								},
							})
						}
						return core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        4,
								Height:     core.Pct(100),
							},
							Children: nodes,
						}
					}),
				},
			},
		},
	}
}
