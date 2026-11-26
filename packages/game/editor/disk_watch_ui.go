package editor

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// diskWatchBannerView renders either the external-change conflict bar (with optional per-object Compare merge picker) or the quiet external reload notice directly below the toolbar, taking zero space when nothing changed on disk.
func diskWatchBannerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			if _, ok := ed.PrimaryDiskConflict(); ok {
				return true
			}
			_, ok := ed.DiskReloadNotice()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				if conflict, ok := ed.PrimaryDiskConflict(); ok {
					return diskConflictBarView(cx, ed, rev, conflict, colors)
				}
				if notice, ok := ed.DiskReloadNotice(); ok {
					return diskReloadNoticeView(cx, ed, rev, notice, colors)
				}
				return core.View{}
			})
		},
	}
}

func diskConflictBarView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], conflict DiskConflict, colors ui.Colors) core.Node {
	kind := conflict.Kind
	canCompare := kind == DiskFileScene && len(conflict.Entries) > 0
	comparing := canCompare && ed.DiskCompareOpen()

	actions := make([]core.Node, 0, 3)
	if canCompare {
		variant := ui.ButtonSecondary
		if comparing {
			variant = ui.ButtonPrimary
		}
		actions = append(actions, ui.Button{
			TestID:  "disk-conflict-compare",
			Text:    ed.T("disk.compare"),
			Size:    ui.SizeSmall,
			Variant: variant,
			OnPress: func() {
				ed.SetDiskCompareOpen(!ed.DiskCompareOpen())
			},
		})
	}
	actions = append(actions,
		ui.Button{
			TestID:  "disk-conflict-reload",
			Text:    ed.T("disk.reload"),
			Size:    ui.SizeSmall,
			Variant: ui.ButtonSecondary,
			OnPress: func() {
				ed.report(ed.ReloadFromDisk(kind))
			},
		},
		ui.Button{
			TestID:  "disk-conflict-keep",
			Text:    ed.T("disk.keepMine"),
			Size:    ui.SizeSmall,
			Variant: ui.ButtonSecondary,
			OnPress: func() {
				ed.report(ed.KeepLocalVersion(kind))
			},
		},
	)

	children := []core.Node{
		core.View{
			TestID: "disk-conflict-bar",
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
						ui.Icon(lucide.TriangleAlert, 14, colors.Warn),
						core.Text{
							TestID:  "disk-conflict-message",
							Content: conflict.Summary,
							Style:   core.Style{FontSize: 12, FontWeight: 600, Color: colors.Text()},
						},
					},
				},
				ui.HStack{
					Gap:      6,
					Children: actions,
				},
			},
		},
	}

	if comparing {
		children = append(children, diskComparePanelView(cx, ed, conflict, colors))
	}

	return core.View{Children: children}
}

func diskComparePanelView(_ *core.Scope, ed *Editor, conflict DiskConflict, colors ui.Colors) core.Node {
	rows := make([]core.Node, 0, len(conflict.Entries))
	mineOverlaps := 0
	for _, entry := range conflict.Entries {
		ent := entry
		if ent.Overlap && ent.Choice == DiskMergeMine {
			mineOverlaps++
		}
		mineVariant := ui.ButtonSecondary
		diskVariant := ui.ButtonSecondary
		if ent.Choice == DiskMergeMine {
			mineVariant = ui.ButtonPrimary
		} else {
			diskVariant = ui.ButtonPrimary
		}
		mineLabel := ed.T("disk.mine")
		if ent.MineSummary != "" && ent.MineSummary != "—" {
			mineLabel += ": " + ent.MineSummary
		}
		diskLabel := ed.T("disk.disk")
		if ent.DiskSummary != "" && ent.DiskSummary != "—" {
			diskLabel += ": " + ent.DiskSummary
		}
		id := ent.ObjectID
		rows = append(rows, core.View{
			TestID: "disk-conflict-row-" + id,
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Padding:        core.Symmetric(6, 10),
				Gap:            12,
				Background:     colors.Raised(),
				CornerRadius:   6,
				Borders:        core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
				BorderColor:    colors.Line(),
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Gap: 2, FlexGrow: 1, FlexShrink: 1},
					Children: []core.Node{
						core.Text{
							Content: ent.ObjectName,
							Style:   core.Style{FontSize: 12, FontWeight: 600, Color: colors.Text()},
						},
						core.Text{
							Content: ent.Note,
							Style:   core.Style{FontSize: 11, Color: colors.Muted()},
						},
					},
				},
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						ui.Button{
							TestID:   "disk-conflict-mine-" + id,
							Text:     mineLabel,
							Size:     ui.SizeSmall,
							Variant:  mineVariant,
							Disabled: func() bool { return !ent.Overlap },
							OnPress: func() {
								ed.report(ed.SetSceneConflictChoice(id, DiskMergeMine))
							},
						},
						ui.Button{
							TestID:   "disk-conflict-disk-" + id,
							Text:     diskLabel,
							Size:     ui.SizeSmall,
							Variant:  diskVariant,
							Disabled: func() bool { return !ent.Overlap },
							OnPress: func() {
								ed.report(ed.SetSceneConflictChoice(id, DiskMergeDisk))
							},
						},
					},
				},
			},
		})
	}

	headerText := fmt.Sprintf("%d objects · %d touched on both sides", len(conflict.Entries), conflict.Overlap)
	summaryText := fmt.Sprintf("%d of %d overlaps use yours", mineOverlaps, conflict.Overlap)

	return core.View{
		TestID: "disk-conflict-compare-panel",
		Style: core.Style{
			Padding:     core.Symmetric(8, 12),
			Gap:         6,
			Background:  colors.Surface(),
			Borders:     core.Edges{Bottom: 1},
			BorderColor: colors.Line(),
		},
		Children: []core.Node{
			core.Text{
				Content: headerText,
				Style:   core.Style{FontSize: 11, FontWeight: 700, Color: colors.Muted()},
			},
			core.View{
				Style:    core.Style{Gap: 4},
				Children: rows,
			},
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            8,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "disk-conflict-merge-summary",
						Content: summaryText,
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					},
					ui.Button{
						TestID:  "disk-conflict-apply-merge",
						Text:    ed.T("disk.useThese"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonPrimary,
						OnPress: func() {
							ed.report(ed.ApplySceneConflictMerge())
						},
					},
				},
			},
		},
	}
}

func diskReloadNoticeView(_ *core.Scope, ed *Editor, _ *core.Signal[uint64], notice DiskReloadNotice, colors ui.Colors) core.Node {
	actions := make([]core.Node, 0, 2)
	if len(notice.Diffs) > 0 {
		actions = append(actions, ui.Button{
			TestID:  "disk-reload-show-diff",
			Text:    ed.T("disk.showDiff"),
			Size:    ui.SizeSmall,
			Variant: ui.ButtonSecondary,
			OnPress: func() {
				ed.SetDiskReloadDiffOpen(!ed.DiskReloadDiffOpen())
			},
		})
	}
	actions = append(actions, ui.Button{
		TestID:  "disk-reload-dismiss",
		Text:    ed.T("disk.dismiss"),
		Size:    ui.SizeSmall,
		Variant: ui.ButtonGhost,
		OnPress: func() {
			ed.DismissDiskReloadNotice()
		},
	})

	children := []core.Node{
		core.View{
			TestID: "disk-reload-notice",
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Padding:        core.Symmetric(5, 12),
				Gap:            8,
				Background:     colors.Raised(),
				Borders:        core.Edges{Bottom: 1},
				BorderColor:    colors.Line(),
			},
			Children: []core.Node{
				ui.HStack{
					Gap: 8,
					Children: []core.Node{
						ui.Icon(lucide.RefreshCw, 13, colors.Muted),
						core.Text{
							TestID:  "disk-reload-message",
							Content: notice.Summary,
							Style:   core.Style{FontSize: 12, Color: colors.Text()},
						},
					},
				},
				ui.HStack{
					Gap:      6,
					Children: actions,
				},
			},
		},
	}

	if ed.DiskReloadDiffOpen() && len(notice.Diffs) > 0 {
		diffRows := make([]core.Node, 0, len(notice.Diffs))
		for _, d := range notice.Diffs {
			diffRows = append(diffRows, core.Text{
				Content: fmt.Sprintf("%s (%s): %s", d.ObjectName, d.Layer, d.Detail),
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			})
		}
		children = append(children, core.View{
			TestID: "disk-reload-diff",
			Style: core.Style{
				Padding:     core.Symmetric(6, 12),
				Gap:         2,
				Background:  colors.Inset(),
				Borders:     core.Edges{Bottom: 1},
				BorderColor: colors.Line(),
			},
			Children: diffRows,
		})
	}

	return core.View{Children: children}
}

// saveConflictDialog asks before overwriting a file that changed on disk since the editor opened or last saved it.
func saveConflictDialog(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	onClose := bindDialogFocusRestore(cx, ed, func() bool {
		_ = rev.Get()
		return ed.SaveConflictPromptOpen()
	})
	return ui.AlertDialog{
		TestID: "save-conflict-dialog",
		Open: func() bool {
			_ = rev.Get()
			return ed.SaveConflictPromptOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				ed.CancelSaveConflict()
			}
		},
		OnClose: onClose,
		Title:   ed.T("dialog.diskConflictTitle"),
		DescriptionFunc: func() string {
			_ = rev.Get()
			file := ed.SceneLabel()
			if pending, ok := ed.PendingSaveConflict(); ok && pending.Path != "" {
				file = pending.Path
			}
			return ed.T("dialog.diskConflictDesc", l10n.Param("file", file))
		},
		Actions: []ui.AlertAction{
			{
				TestID:  "save-conflict-cancel",
				Text:    ed.T("dialog.cancel"),
				Variant: ui.ButtonSecondary,
				Role:    core.AlertCancel,
				OnPress: func() {
					ed.CancelSaveConflict()
				},
			},
			{
				TestID:  "save-conflict-reload",
				Text:    ed.T("disk.reload"),
				Variant: ui.ButtonSecondary,
				OnPress: func() {
					ed.report(ed.reloadPendingSaveConflict())
				},
			},
			{
				TestID:  "save-conflict-overwrite",
				Text:    ed.T("dialog.overwrite"),
				Variant: ui.ButtonPrimary,
				Role:    core.AlertDestructive,
				OnPress: func() {
					ed.report(ed.ConfirmOverwriteSave())
				},
			},
		},
	}
}
