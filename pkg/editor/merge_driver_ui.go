package editor

import (
	"reflect"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type mergeDriverBannerState struct {
	visible bool
	errMsg  string
	lang    string
}

// mergeDriverBannerView renders a non-blocking warning bar below the toolbar when the open
// Git repository's .gitattributes names merge=nsgo-scene without a configured local driver,
// offering one-click setup and a dismiss action. It takes zero space when not needed.
func mergeDriverBannerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.SceneMergeDriverBannerVisible()
		},
		Then: func(cx *core.Scope) core.Node {
			// Rebuild the banner only when its own state or language changes, not on every editor notify,
			// so background problem checks or autosave never remount the button under a press.
			state := core.NewMemoFunc(cx, func() mergeDriverBannerState {
				_ = rev.Get()
				return mergeDriverBannerState{
					visible: ed.SceneMergeDriverBannerVisible(),
					errMsg:  ed.SceneMergeDriverError(),
					lang:    ed.ResolvedLanguage(),
				}
			}, func(a, b mergeDriverBannerState) bool { return reflect.DeepEqual(a, b) })

			return core.Dynamic(func(cx *core.Scope) core.Node {
				s := state.Get()
				if !s.visible {
					return core.View{}
				}
				msgChildren := []core.Node{
					core.Text{
						TestID:  "merge-driver-message",
						Content: ed.T("mergeDriver.bannerMessage"),
						Style:   core.Style{FontSize: 12, FontWeight: 600, Color: colors.Text()},
					},
				}
				if s.errMsg != "" {
					msgChildren = append(msgChildren, core.Text{
						TestID:  "merge-driver-error",
						Content: s.errMsg,
						Style:   core.Style{FontSize: 11, Color: colors.Danger()},
					})
				}
				return core.View{
					TestID: "merge-driver-banner",
					Role:   core.RoleRegion,
					Label:  ed.T("mergeDriver.bannerMessage"),
					Style: core.Style{
						Direction:      core.Row,
						Wrap:           core.Wrap,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Padding:        core.Symmetric(6, 12),
						Gap:            8,
						Background:     ui.Alpha(colors.Warn(), 0.12),
						Borders:        core.Edges{Bottom: 1},
						BorderColor:    ui.Alpha(colors.Warn(), 0.45),
					},
					Children: []core.Node{
						ui.HStack{
							Gap: 8,
							Children: []core.Node{
								ui.Icon(lucide.GitMerge, 14, colors.Warn),
								core.View{
									Style:    core.Style{Gap: 2, FlexShrink: 1},
									Children: msgChildren,
								},
							},
						},
						ui.HStack{
							Gap: 6,
							Children: []core.Node{
								ui.Button{
									TestID:  "merge-driver-setup",
									Text:    ed.T("mergeDriver.setupButton"),
									Size:    ui.SizeSmall,
									Variant: ui.ButtonPrimary,
									OnPress: func() {
										ed.report(ed.SetupSceneMergeDriver())
									},
								},
								ui.Button{
									TestID:  "merge-driver-dismiss",
									Text:    ed.T("mergeDriver.dismissButton"),
									Size:    ui.SizeSmall,
									Variant: ui.ButtonGhost,
									OnPress: func() {
										ed.DismissSceneMergeDriverBanner()
									},
								},
							},
						},
					},
				}
			})
		},
	}
}
