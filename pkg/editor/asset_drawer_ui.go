package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type versionedAssetSnap struct {
	key  assetDrawerRailKey
	snap AssetDrawerSnapshot
}

func newAssetSnapMemo(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) *core.Memo[versionedAssetSnap] {
	keyMemo := core.NewMemo(cx, func() assetDrawerRailKey {
		_ = rev.Get()
		return ed.assetDrawerRailKey()
	})
	return core.NewMemoFunc(cx, func() versionedAssetSnap {
		k := keyMemo.Get()
		return versionedAssetSnap{key: k, snap: ed.AssetDrawerSnapshot()}
	}, func(a, b versionedAssetSnap) bool {
		return a.key == b.key
	})
}

func sanitizeAssetTestID(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// LeftRailSwitchView renders the Scene / Assets segmented switch at the top of the left rail,
// matching design/editor/Assets.dc.html screen 1 with Spec.dc.html tokens.
func LeftRailSwitchView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	sceneTabRef := &core.Ref{}
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionSceneTree, sceneTabRef, nil)
	}
	modeBtn := func(testID, label string, mode LeftRailMode) core.Node {
		var ref *core.Ref
		if mode == LeftRailScene {
			ref = sceneTabRef
		}
		return base.Radio{
			Ref:    ref,
			TestID: testID,
			Label:  label,
			Checked: func() bool {
				_ = rev.Get()
				return ed.LeftRailMode() == mode
			},
			OnChange: func(checked bool) {
				if checked {
					ed.SetLeftRailMode(mode)
				}
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				cur := ed.SpecPalette(cx)
				active := ed.LeftRailMode() == mode
				bg := core.Transparent
				if active {
					bg = cur.LineRaised
				}
				return core.Style{
					FlexGrow:       1,
					FlexBasis:      core.Pt(0),
					Height:         core.Pt(specRowHeight),
					MinHeight:      core.Pt(specRowHeight),
					MinWidth:       core.Pt(specRowHeight),
					CornerRadius:   specRadiusControl,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     bg,
					Cursor:         core.CursorPointer,
					FocusVisible:   cur.FocusRing(2),
				}
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						cur := ed.SpecPalette(cx)
						col := cur.TextFaint
						if ed.LeftRailMode() == mode {
							col = cur.TextPrimary
						}
						return core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      col,
						}
					},
				},
			},
		}
	}

	return core.View{
		TestID: "left-rail-switch",
		StyleFunc: func() core.Style {
			cur := ed.SpecPalette(cx)
			return core.Style{
				Height:      core.Pt(specHeaderHeight),
				FlexShrink:  0,
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Padding:     core.Symmetric(0, specSpace8),
				Background:  cur.Bg,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: cur.Line,
			}
		},
		Children: []core.Node{
			base.RadioGroup{
				TestID: "left-rail-mode-group",
				Label:  ed.T("region.sceneTree"),
				StyleFunc: func() core.Style {
					cur := ed.SpecPalette(cx)
					return core.Style{
						FlexGrow:     1,
						Direction:    core.Row,
						Gap:          2,
						Padding:      core.All(3),
						CornerRadius: specRadiusButton,
						Background:   cur.Raised,
						BorderWidth:  1,
						BorderColor:  cur.LineRaised,
					}
				},
				Children: []core.Node{
					modeBtn("rail-mode-scene", "Scene", LeftRailScene),
					modeBtn("rail-mode-assets", "Assets", LeftRailAssets),
				},
			},
		},
	}
}

// AssetsLeftRailView renders the 232 pt left rail when Assets mode is active:
// Scene / Assets switch, FOLDERS with counts, SHOW ONLY filters with counts, and a drop zone.
func AssetsLeftRailView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	snapMemo := newAssetSnapMemo(cx, ed, rev)

	body := core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		selectedRowFill := specAlpha(p.Accent.Fg, 0.14)
		snap := snapMemo.Get().snap
		folderRows := make([]core.Node, 0, len(snap.Folders))
		for _, f := range snap.Folders {
			folder := f
			selected := snap.ActiveFolder == folder.Path
			id := "asset-folder-all"
			if folder.Path != "" {
				id = "asset-folder-" + sanitizeAssetTestID(folder.Path)
			}
			var bg core.Color
			fg := p.TextMuted
			if selected {
				bg = selectedRowFill
				fg = p.TextPrimary
			}
			folderRows = append(folderRows, core.View{
				TestID: id,
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("%s (%d)", folder.Label, folder.Count),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Gap:          8,
					Padding:      core.Edges{Left: core.Points(8 + folder.Depth*14), Right: 8},
					CornerRadius: 6,
					Background:   bg,
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.SetAssetDrawerFolder(folder.Path)
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.Folder(),
						Style: core.Style{
							Width:      core.Pt(14),
							Height:     core.Pt(14),
							FlexShrink: 0,
							Color:      fg,
						},
					},
					core.Text{
						Content:  folder.Label,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12.5,
							Color:      fg,
						},
					},
					core.Text{
						TestID:  id + "-count",
						Content: fmt.Sprintf("%d", folder.Count),
						Style: core.Style{
							FontSize:   11,
							Color:      p.TextFaint,
							FlexShrink: 0,
						},
					},
				},
			})
		}

		type filterSpec struct {
			id    string
			key   AssetStatusFilter
			label string
			count int
			color core.Color
		}
		filters := []filterSpec{
			{"asset-filter-unused", AssetFilterUnused, "Unused", snap.Counts.Unused, p.TextFaint},
			{"asset-filter-missing", AssetFilterMissing, "Missing files", snap.Counts.Missing, p.Removed.Fg},
			{"asset-filter-generated", AssetFilterGenerated, "Generated", snap.Counts.Generated, p.Agent.Fg},
			{"asset-filter-proposed", AssetFilterProposed, "Proposed", snap.Counts.Proposed, p.Added.Fg},
		}
		filterRows := make([]core.Node, 0, len(filters))
		for _, fl := range filters {
			f := fl
			active := snap.ActiveStatus == f.key
			var bg core.Color
			fg := p.TextMuted
			if active {
				bg = selectedRowFill
				fg = p.TextPrimary
			}
			filterRows = append(filterRows, core.View{
				TestID: f.id,
				Role:   core.RoleButton,
				Label:  fmt.Sprintf("%s (%d)", f.label, f.count),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Gap:          8,
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 6,
					Background:   bg,
					FocusVisible: p.FocusRing(-2),
				},
				OnPress: func(core.PressEvent) {
					ed.ToggleAssetDrawerStatusFilter(f.key)
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(10),
							Height:       core.Pt(10),
							FlexShrink:   0,
							CornerRadius: 3,
							BorderWidth:  1.5,
							BorderColor:  f.color,
						},
					},
					core.Text{
						Content:  f.label,
						MaxLines: 1,
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							FontSize:   12.5,
							Color:      fg,
						},
					},
					core.Text{
						TestID:  f.id + "-count",
						Content: fmt.Sprintf("%d", f.count),
						Style: core.Style{
							FontSize:   11,
							Color:      p.TextFaint,
							FlexShrink: 0,
						},
					},
				},
			})
		}

		return core.View{
			Style: core.Style{FlexGrow: 1, Gap: 4},
			Children: []core.Node{
				core.Text{
					Content: "FOLDERS",
					Style: core.Style{
						Padding:    core.Edges{Top: 12, Left: 14, Right: 14, Bottom: 4},
						FontSize:   11,
						FontWeight: 600,
						Color:      p.TextFaint,
					},
				},
				core.View{
					Style:    core.Style{Padding: core.Symmetric(0, 6), Gap: 2},
					Children: folderRows,
				},
				core.Text{
					Content: "SHOW ONLY",
					Style: core.Style{
						Padding:    core.Edges{Top: 16, Left: 14, Right: 14, Bottom: 4},
						FontSize:   11,
						FontWeight: 600,
						Color:      p.TextFaint,
					},
				},
				core.View{
					Style:    core.Style{Padding: core.Symmetric(0, 6), Gap: 2},
					Children: filterRows,
				},
			},
		}
	})

	return core.View{
		TestID: "assets-left-rail",
		Role:   core.RoleRegion,
		Label:  "Asset folders and filters",
		OnFileDrop: func(uris []string) {
			_, err := ed.ImportAssetFiles(uris)
			ed.report(err)
		},
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				Width:       core.Pt(232),
				FlexShrink:  0,
				Background:  p.Bg,
				Borders:     core.Edges{Right: 1},
				BorderColor: p.Line,
				Overflow:    core.Hidden,
			}
		},
		Children: []core.Node{
			LeftRailSwitchView(cx, ed, rev),
			ui.ScrollView{
				Style:     core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				EdgeColor: func() core.Color { return ed.SpecPalette(cx).Bg },
				Children:  []core.Node{body},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				p := ed.SpecPalette(cx)
				return core.View{
					TestID: "assets-drop-zone",
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          12,
						Margin:       core.All(12),
						Padding:      core.All(12),
						CornerRadius: 12,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Background:   p.Panel,
					},
					Children: []core.Node{
						core.SVGView{
							SVG: lucide.Upload(),
							Style: core.Style{
								Width:      core.Pt(16),
								Height:     core.Pt(16),
								FlexShrink: 0,
								Color:      p.TextFaint,
							},
						},
						core.Text{
							Content: "Drop files anywhere to import",
							Style: core.Style{
								FlexGrow:   1,
								FlexShrink: 1,
								FontSize:   12,
								Color:      p.TextFaint,
							},
						},
					},
				}
			}),
		},
	}
}

// AssetDrawerView renders the bottom asset drawer below the viewport in Assets mode:
// kind tabs, name/path filter, sort toggle, Import & Generate actions, asset cards (draggable to the viewport),
// empty states, and the "N assets · M shown" + missing-problem footer.
func AssetDrawerView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	snapMemo := newAssetSnapMemo(cx, ed, rev)

	kindBtn := func(testID, label string, kind AssetDrawerKind) core.Node {
		return core.View{
			TestID: testID,
			Role:   core.RoleButton,
			Label:  label,
			StyleFunc: func() core.Style {
				_ = rev.Get()
				p := ed.SpecPalette(cx)
				var bg core.Color
				if ed.AssetDrawerKind() == kind {
					bg = p.LineRaised
				}
				return core.Style{
					Height:         core.Pt(24),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Background:     bg,
					FocusVisible:   p.FocusRing(2),
				}
			},
			OnPress: func(core.PressEvent) {
				ed.SetAssetDrawerKind(kind)
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						p := ed.SpecPalette(cx)
						col := p.TextFaint
						if ed.AssetDrawerKind() == kind {
							col = p.TextPrimary
						}
						return core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      col,
						}
					},
				},
			},
		}
	}

	header := core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		return core.View{
			TestID: "asset-drawer-header",
			Style: core.Style{
				Height:      core.Pt(44),
				FlexShrink:  0,
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         8,
				Padding:     core.Symmetric(0, 12),
				Background:  p.Panel,
				Borders:     core.Edges{Bottom: 1},
				BorderColor: p.Line,
			},
			Children: []core.Node{
				core.Text{
					Content: "ASSETS",
					Style: core.Style{
						FontSize:   11,
						FontWeight: 600,
						Color:      p.TextFaint,
						FlexShrink: 0,
					},
				},
				core.View{
					Style: core.Style{
						Direction:    core.Row,
						Gap:          2,
						Padding:      core.All(3),
						CornerRadius: 8,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FlexShrink:   0,
					},
					Children: []core.Node{
						kindBtn("asset-kind-all", "All", AssetKindAll),
						kindBtn("asset-kind-sprites", "Sprites", AssetKindSprites),
						kindBtn("asset-kind-tiles", "Tiles", AssetKindTiles),
						kindBtn("asset-kind-audio", "Audio", AssetKindAudio),
						kindBtn("asset-kind-fonts", "Fonts", AssetKindFonts),
					},
				},
				core.View{
					Style: core.Style{
						FlexGrow:     1,
						FlexShrink:   1,
						MinWidth:     core.Pt(96),
						Height:       core.Pt(30),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          6,
						Padding:      core.Symmetric(0, 8),
						CornerRadius: 8,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.SVGView{
							SVG: lucide.Search(),
							Style: core.Style{
								Width:      core.Pt(13),
								Height:     core.Pt(13),
								FlexShrink: 0,
								Color:      p.TextFaint,
							},
						},
						ui.Input{
							TestID:      "asset-drawer-search",
							Label:       "Filter assets by name or path",
							Placeholder: "Name or path",
							Style: core.Style{
								FlexGrow:    1,
								FlexShrink:  1,
								MinWidth:    core.Pt(0),
								Height:      core.Pt(26),
								BorderWidth: 0,
								Background:  p.Raised,
								Color:       p.TextSecondary,
								FontSize:    12,
							},
							Value: func() string {
								_ = rev.Get()
								return ed.AssetDrawerQuery()
							},
							OnChange: func(v string) {
								ed.SetAssetDrawerQuery(v)
							},
						},
					},
				},
				ui.Button{
					TestID:  "asset-drawer-sort",
					Icon:    lucide.ArrowUpDown,
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					LabelFunc: func() string {
						_ = rev.Get()
						if ed.AssetDrawerSort() == AssetSortRecent {
							return "Sort: recently changed"
						}
						return "Sort: name"
					},
					Color: func() core.Color { return p.Raised },
					Tint:  func() core.Color { return p.TextMuted },
					Style: core.Style{
						Width:        core.Pt(30),
						Height:       core.Pt(30),
						CornerRadius: 8,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						FlexShrink:   0,
					},
					OnPress: func() {
						if ed.AssetDrawerSort() == AssetSortRecent {
							ed.SetAssetDrawerSort(AssetSortName)
						} else {
							ed.SetAssetDrawerSort(AssetSortRecent)
						}
					},
				},
				ui.Button{
					TestID:  "asset-drawer-import",
					Icon:    lucide.Upload,
					Text:    "Import",
					Label:   "Import asset files",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return p.LineRaised },
					Tint:    func() core.Color { return p.TextSecondary },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.TextSecondary,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 12),
						FlexShrink:   0,
					},
					OnPress: func() {
						ed.TriggerAssetImport()
					},
				},
				ui.Button{
					TestID:  "asset-drawer-generate",
					Icon:    lucide.Sparkles,
					Text:    "Generate",
					Label:   "Generate asset with AI",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return p.LineRaised },
					Tint:    func() core.Color { return p.Agent.Fg },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 500,
						Color:      p.TextSecondary,
					},
					Style: core.Style{
						Height:       core.Pt(30),
						CornerRadius: 8,
						Padding:      core.Symmetric(0, 12),
						FlexShrink:   0,
					},
					OnPress: func() {
						ed.TriggerAssetGenerate()
					},
				},
			},
		}
	})

	gridBody := core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		snap := snapMemo.Get().snap
		var children []core.Node

		if snap.ProposedCount > 0 {
			children = append(children, proposedImportBanner(ed, snap.ProposedCount, p))
		}

		if len(snap.VisibleItems) == 0 {
			children = append(children, assetDrawerEmptyState(ed, snap, p))
			return core.View{
				Style:    core.Style{FlexGrow: 1, Padding: core.All(12), Gap: 12},
				Children: children,
			}
		}

		selectedPath := ""
		if snap.Selected != nil {
			selectedPath = snap.Selected.Path
		}
		cards := make([]core.Node, 0, len(snap.VisibleItems))
		for _, it := range snap.VisibleItems {
			cards = append(cards, assetDrawerCard(ed, it, it.Path == selectedPath, p))
		}
		children = append(children, core.View{
			TestID: "asset-drawer-grid",
			Style: core.Style{
				Direction: core.Row,
				Wrap:      core.Wrap,
				Gap:       12,
			},
			Children: cards,
		})
		return core.View{
			Style:    core.Style{Padding: core.All(12), Gap: 12},
			Children: children,
		}
	})

	footer := core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		snap := snapMemo.Get().snap
		countText := fmt.Sprintf("%d assets · %d shown", len(snap.AllItems), len(snap.VisibleItems))
		rightNodes := []core.Node{}
		if len(snap.MissingNames) > 0 {
			probText := fmt.Sprintf("1 problem · %s missing", snap.MissingNames[0])
			if len(snap.MissingNames) > 1 {
				probText = fmt.Sprintf("%d problems · %s missing", len(snap.MissingNames), snap.MissingNames[0])
			}
			rightNodes = append(rightNodes, core.View{
				TestID: "asset-drawer-footer-problem",
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Removed.Fg,
						},
					},
					core.Text{
						Content: probText,
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
					},
				},
			})
		}
		rightNodes = append(rightNodes, core.Text{
			Content: "Re-imports on save",
			Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
		})
		return core.View{
			TestID: "asset-drawer-footer",
			Style: core.Style{
				Height:         core.Pt(32),
				FlexShrink:     0,
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            16,
				Padding:        core.Symmetric(0, 14),
				Background:     p.Bg,
				Borders:        core.Edges{Top: 1},
				BorderColor:    p.Line,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "asset-drawer-footer-count",
					Content: countText,
					Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
				},
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        16,
					},
					Children: rightNodes,
				},
			},
		}
	})

	return core.View{
		TestID: "asset-drawer",
		Role:   core.RoleRegion,
		Label:  "Asset drawer",
		OnFileDrop: func(uris []string) {
			_, err := ed.ImportAssetFiles(uris)
			ed.report(err)
		},
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				FlexGrow:    1,
				FlexBasis:   core.Pt(0),
				Background:  p.Panel,
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
				Overflow:    core.Hidden,
			}
		},
		Children: []core.Node{
			header,
			ui.ScrollView{
				Style:     core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				EdgeColor: func() core.Color { return ed.SpecPalette(cx).Panel },
				Children:  []core.Node{gridBody},
			},
			footer,
		},
	}
}

func proposedImportBanner(ed *Editor, count int, p SpecPalette) core.Node {
	title := fmt.Sprintf("Import %d proposed file", count)
	if count != 1 {
		title = fmt.Sprintf("Import %d proposed files", count)
	}
	return core.View{
		TestID: "asset-proposed-banner",
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            12,
			Padding:        core.Symmetric(10, 14),
			CornerRadius:   12,
			Background:     p.Raised,
			BorderWidth:    1,
			BorderColor:    specAlpha(p.Added.Fg, 0.45),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: p.Added.Fg},
					},
					core.Text{
						Content: title,
						Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: p.TextPrimary},
					},
					core.Text{
						Content: "· review import settings before keeping",
						Style:   core.Style{FontSize: 12, Color: p.TextFaint},
					},
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					ui.Button{
						TestID:  "asset-proposed-keep",
						Text:    "Keep",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Color:   func() core.Color { return p.Accent.Fg },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 600,
							Color:      p.Accent.OnFill,
						},
						Style: core.Style{Height: core.Pt(28), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
						OnPress: func() {
							ed.KeepProposedAssets()
						},
					},
					ui.Button{
						TestID:  "asset-proposed-revert",
						Text:    "Revert",
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Color:   func() core.Color { return p.LineRaised },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 500,
							Color:      p.TextSecondary,
						},
						Style: core.Style{Height: core.Pt(28), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
						OnPress: func() {
							ed.RevertProposedAssets()
						},
					},
				},
			},
		},
	}
}

func assetDrawerEmptyState(ed *Editor, snap AssetDrawerSnapshot, p SpecPalette) core.Node {
	if snap.EmptyFolder != "" {
		isAudioFolder := strings.Contains(strings.ToLower(snap.EmptyFolder), "audio") ||
			strings.Contains(strings.ToLower(snap.EmptyFolder), "music") ||
			strings.Contains(strings.ToLower(snap.EmptyFolder), "sfx")
		icon := lucide.FolderOpen()
		hint := fmt.Sprintf("%s is empty · Drop files here, or ask the agent.", snap.EmptyFolder)
		if isAudioFolder {
			icon = lucide.Music()
			hint = fmt.Sprintf("%s is empty · Drop .ogg or .wav files here, or ask the agent.", snap.EmptyFolder)
		}
		return core.View{
			TestID: "asset-drawer-empty-folder",
			Style: core.Style{
				FlexGrow:       1,
				MinHeight:      core.Pt(180),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Gap:            12,
				Padding:        core.All(24),
				CornerRadius:   12,
				Background:     p.Raised,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
			},
			Children: []core.Node{
				core.SVGView{
					SVG:   icon,
					Style: core.Style{Width: core.Pt(28), Height: core.Pt(28), Color: p.TextFaint},
				},
				core.Text{
					Content: snap.EmptyFolder + " is empty",
					Style:   core.Style{FontSize: 14, FontWeight: 600, Color: p.TextPrimary},
				},
				core.Text{
					TestID:  "asset-drawer-empty-hint",
					Content: hint,
					Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
				},
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 8},
					Children: []core.Node{
						ui.Button{
							TestID:  "asset-empty-import",
							Text:    "Import",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return p.LineRaised },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      p.TextSecondary,
							},
							Style: core.Style{Height: core.Pt(30), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
							OnPress: func() {
								ed.TriggerAssetImport()
							},
						},
						ui.Button{
							TestID:  "asset-empty-ask-agent",
							Icon:    lucide.Sparkles,
							Text:    "Ask the agent",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return p.LineRaised },
							Tint:    func() core.Color { return p.Agent.Fg },
							TextStyle: core.Style{
								FontSize:   12,
								FontWeight: 500,
								Color:      p.TextSecondary,
							},
							Style: core.Style{Height: core.Pt(30), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
							OnPress: func() {
								ed.AskAgentAboutAsset(snap.EmptyFolder, "Create assets for "+snap.EmptyFolder)
							},
						},
					},
				},
			},
		}
	}

	return core.View{
		TestID: "asset-drawer-empty-match",
		Style: core.Style{
			FlexGrow:       1,
			MinHeight:      core.Pt(180),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            12,
			Padding:        core.All(24),
			CornerRadius:   12,
			Background:     p.Raised,
			BorderWidth:    1,
			BorderColor:    p.LineRaised,
		},
		Children: []core.Node{
			core.SVGView{
				SVG:   lucide.Search(),
				Style: core.Style{Width: core.Pt(24), Height: core.Pt(24), Color: p.TextFaint},
			},
			core.Text{
				Content: "Nothing matches",
				Style:   core.Style{FontSize: 14, FontWeight: 600, Color: p.TextPrimary},
			},
			core.Text{
				TestID:  "asset-drawer-empty-hint",
				Content: "Clear a filter to see more assets.",
				Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
			},
			ui.Button{
				TestID:  "asset-drawer-clear-filters",
				Text:    "Clear filters",
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return p.LineRaised },
				TextStyle: core.Style{
					FontSize:   12,
					FontWeight: 500,
					Color:      p.TextSecondary,
				},
				Style: core.Style{Height: core.Pt(30), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
				OnPress: func() {
					ed.ClearAssetDrawerFilters()
				},
			},
		},
	}
}

func assetDrawerCard(ed *Editor, it AssetDrawerItem, selected bool, p SpecPalette) core.Node {
	cardID := "asset-card-" + sanitizeAssetTestID(it.Path)
	selectedRowFill := specAlpha(p.Accent.Fg, 0.14)
	bgOverlay := specAlpha(p.Bg, 0.92)

	bg := p.Raised
	borderColor := p.LineRaised
	borderW := core.Points(1)
	if it.Missing {
		borderColor = p.Removed.Fg
		borderW = 1.5
	} else if it.Proposed {
		borderColor = p.Added.Fg
		borderW = 1.5
	}
	if selected {
		bg = selectedRowFill
		borderColor = p.Accent.Fg
		borderW = 1.5
	}

	nameCol := p.TextSecondary
	if selected {
		nameCol = p.TextPrimary
	}
	metaCol := p.TextFaint
	if it.Missing {
		metaCol = p.Removed.Fg
	}

	var badgeNode core.Node
	if it.Badge != "" {
		bc := p.TextMuted
		switch it.Badge {
		case "Missing":
			bc = p.Removed.Fg
		case "new":
			bc = p.Added.Fg
		case "Generated":
			bc = p.Agent.Fg
		case "Not used":
			bc = p.TextMuted
		}
		badgeNode = core.View{
			TestID: cardID + "-badge",
			Style: core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(4),
				Top:            core.Pt(4),
				Height:         core.Pt(18),
				Padding:        core.Symmetric(0, 6),
				CornerRadius:   6,
				Background:     bgOverlay,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: it.Badge,
					Style: core.Style{
						FontSize:   10.5,
						FontWeight: 600,
						Color:      bc,
					},
				},
			},
		}
	}

	thumbChildren := []core.Node{
		assetThumbnailCanvas(it, 112, 72, false, p),
	}
	if it.Missing {
		thumbChildren = append(thumbChildren, core.View{
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(0),
				Top:            core.Pt(0),
				Right:          core.Pt(0),
				Bottom:         core.Pt(0),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.SVGView{
					SVG:   lucide.ImageOff(),
					Style: core.Style{Width: core.Pt(24), Height: core.Pt(24), Color: p.Removed.Fg},
				},
			},
		})
	} else if it.Kind == AssetKindFonts {
		thumbChildren = append(thumbChildren, core.View{
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(0),
				Top:            core.Pt(0),
				Right:          core.Pt(0),
				Bottom:         core.Pt(0),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: it.FontSample,
					Style: core.Style{
						FontSize:   16,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
			},
		})
	}
	if badgeNode != nil {
		thumbChildren = append(thumbChildren, badgeNode)
	}

	cardView := core.View{
		TestID: cardID,
		Role:   core.RoleButton,
		Label:  it.Name + ", " + it.Meta,
		Style: core.Style{
			Width:        core.Pt(126),
			Gap:          4,
			Padding:      core.Edges{Top: 6, Left: 6, Right: 6, Bottom: 8},
			CornerRadius: 12,
			Background:   bg,
			BorderWidth:  borderW,
			BorderColor:  borderColor,
			Overflow:     core.Hidden,
			FocusVisible: p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			ed.SelectAssetPath(it.Path)
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Height:       core.Pt(72),
					CornerRadius: 8,
					Position:     core.Relative,
					Overflow:     core.Hidden,
				},
				Children: thumbChildren,
			},
			core.Text{
				Content:  it.Name,
				MaxLines: 1,
				Style: core.Style{
					Padding:  core.Symmetric(0, 2),
					FontSize: 12,
					Color:    nameCol,
				},
			},
			core.Text{
				Content:  it.Meta,
				MaxLines: 1,
				Style: core.Style{
					Padding:  core.Symmetric(0, 2),
					FontSize: 11,
					Color:    metaCol,
				},
			},
		},
	}

	// Make non-missing sprite and tile assets draggable directly onto the 2D viewport.
	if !it.Missing && (it.Kind == AssetKindSprites || it.Kind == AssetKindTiles) {
		dragPayload := AssetDrawerDrag{
			Path:  it.Path,
			Name:  it.Name,
			Atlas: it.Atlas,
			Frame: it.Frame,
			Image: it.Image,
			Rect:  it.Rect,
		}
		return dragdrop.Source{
			View: cardView,
			Data: func() any { return dragPayload },
			OnStart: func(any) {
				ed.SelectAssetPath(it.Path)
				ed.beginAssetDragPayload(dragPayload)
			},
			OnEnd: func(res dragdrop.Result) {
				if !res.Dropped {
					ed.CancelAssetDrag()
				}
			},
			Preview: func(any) core.Node {
				return core.View{
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          6,
						Padding:      core.Symmetric(4, 8),
						CornerRadius: 6,
						Background:   bgOverlay,
						BorderWidth:  1,
						BorderColor:  p.Added.Fg,
					},
					Children: []core.Node{
						core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: p.Added.Fg}},
						core.Text{Content: it.Name, Style: core.Style{FontSize: 11.5, FontWeight: 600, Color: p.TextPrimary}},
					},
				}
			},
		}
	}
	return cardView
}

// AssetInspectorView renders the 380 pt right panel for the selected asset in Assets mode:
// preview (clips playable frame-by-frame, sounds playable), missing-file actions, import settings, and USED BY from refs.
func AssetInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	snapMemo := newAssetSnapMemo(cx, ed, rev)
	inspectorKeyMemo := core.NewMemo(cx, func() assetDrawerInspectorKey {
		_ = rev.Get()
		return ed.assetDrawerInspectorKey()
	})
	previewKeyMemo := core.NewMemo(cx, func() assetDrawerPreviewKey {
		_ = rev.Get()
		return ed.assetDrawerPreviewKey()
	})

	content := core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.SpecPalette(cx)
		_ = inspectorKeyMemo.Get()
		snap := snapMemo.Get().snap
		if snap.Selected == nil {
			return core.View{
				Style: core.Style{FlexGrow: 1, Padding: core.All(16), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
				Children: []core.Node{
					core.Text{
						Content: "Select an asset to inspect its preview and references.",
						Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
					},
				},
			}
		}
		sel := *snap.Selected
		hasClips := len(sel.Clips) > 0
		isAudio := sel.Kind == AssetKindAudio && !sel.Missing

		// Isolate preview box & clip/audio playback controls into a preview-keyed Dynamic so
		// 8 Hz clip ticks only update preview nodes and 60 Hz drag moves trigger zero rebuilds.
		previewSection := core.Dynamic(func(cx *core.Scope) core.Node {
			cur := ed.SpecPalette(cx)
			bgOverlay := specAlpha(cur.Bg, 0.92)
			_ = previewKeyMemo.Get()
			clip := activePreviewClip(&sel, ed.AssetPreviewClip())
			frameIdx := ed.AssetPreviewFrame()
			if hasClips && len(clip.Frames) > 0 && frameIdx >= len(clip.Frames) {
				frameIdx = frameIdx % len(clip.Frames)
			}

			// Build preview item (using active clip frame when playing/scrubbing clips).
			previewItem := sel
			if hasClips && len(clip.Frames) > 0 {
				f := clip.Frames[frameIdx]
				previewItem.Image = f.Image
				previewItem.Rect = f.Rect
			}
			audioPlaying := isAudio && ed.AssetAudioPlayingPath() == sel.Path

			previewChildren := []core.Node{
				assetThumbnailCanvas(previewItem, 348, 168, audioPlaying, cur),
			}
			if sel.Missing {
				previewChildren = append(previewChildren, core.View{
					PointerEvents: core.PointerEventsNone,
					Style: core.Style{
						Position:       core.Absolute,
						Left:           core.Pt(0),
						Top:            core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.SVGView{
							SVG:   lucide.ImageOff(),
							Style: core.Style{Width: core.Pt(36), Height: core.Pt(36), Color: cur.Removed.Fg},
						},
					},
				})
			} else if sel.Kind == AssetKindFonts {
				previewChildren = append(previewChildren, core.View{
					PointerEvents: core.PointerEventsNone,
					Style: core.Style{
						Position:       core.Absolute,
						Left:           core.Pt(0),
						Top:            core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Text{
							Content: sel.FontSample,
							Style:   core.Style{FontSize: 28, FontWeight: 500, Color: cur.TextPrimary},
						},
					},
				})
			}
			if sel.Dims != "" {
				previewChildren = append(previewChildren, core.Text{
					TestID:  "asset-preview-dims",
					Content: sel.Dims,
					Style: core.Style{
						Position: core.Absolute,
						Left:     core.Pt(12),
						Top:      core.Pt(8),
						FontSize: 11,
						Color:    cur.TextFaint,
					},
				})
			}
			if hasClips || isAudio {
				playingNow := (hasClips && ed.AssetPreviewPlaying()) || audioPlaying
				playIcon := lucide.Play
				playLabel := "Play"
				if playingNow {
					playIcon = lucide.Pause
					playLabel = "Pause"
				}
				previewChildren = append(previewChildren, core.View{
					Style: core.Style{
						Position: core.Absolute,
						Right:    core.Pt(8),
						Bottom:   core.Pt(8),
					},
					Children: []core.Node{
						ui.Button{
							TestID:  "asset-preview-play",
							Icon:    playIcon,
							Label:   playLabel,
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return bgOverlay },
							Tint:    func() core.Color { return cur.TextPrimary },
							Style: core.Style{
								Width:        core.Pt(28),
								Height:       core.Pt(28),
								CornerRadius: 6,
								BorderWidth:  1,
								BorderColor:  cur.LineRaised,
							},
							OnPress: func() {
								if isAudio {
									ed.report(ed.ToggleAssetAudioPreview(sel.Path))
								} else {
									ed.SetAssetPreviewPlaying(!ed.AssetPreviewPlaying())
								}
							},
						},
					},
				})
			}

			secNodes := []core.Node{
				core.View{
					TestID: "asset-preview-box",
					Style: core.Style{
						Height:       core.Pt(168),
						FlexShrink:   0,
						CornerRadius: 12,
						Position:     core.Relative,
						BorderWidth:  1,
						BorderColor:  cur.LineRaised,
						Overflow:     core.Hidden,
					},
					Children: previewChildren,
				},
			}

			// Clips & frame-by-frame controls when the sprite has multiple frames/clips.
			if hasClips {
				clipChips := make([]core.Node, 0, len(sel.Clips))
				for _, cl := range sel.Clips {
					cName := cl.Name
					on := cName == clip.Name
					bg := cur.Raised
					fg := cur.TextMuted
					border := cur.LineRaised
					if on {
						bg = cur.LineRaised
						fg = cur.TextPrimary
						border = cur.LineRaised
					}
					clipChips = append(clipChips, core.View{
						TestID: "asset-clip-" + sanitizeAssetTestID(cName),
						Role:   core.RoleButton,
						Label:  "Clip " + cName,
						Style: core.Style{
							Height:         core.Pt(26),
							Padding:        core.Symmetric(0, 10),
							CornerRadius:   6,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Background:     bg,
							BorderWidth:    1,
							BorderColor:    border,
							FocusVisible:   cur.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.SetAssetPreviewClip(cName)
						},
						Children: []core.Node{
							core.Text{Content: cName, Style: core.Style{FontSize: 12, Color: fg}},
						},
					})
				}

				maxThumbs := min(len(clip.Frames), 6)
				frameButtons := make([]core.Node, 0, maxThumbs+3)
				frameButtons = append(frameButtons, ui.Button{
					TestID:  "asset-frame-prev",
					Icon:    lucide.ChevronLeft,
					Label:   "Previous frame",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return cur.Raised },
					Tint:    func() core.Color { return cur.TextMuted },
					Style:   core.Style{Width: core.Pt(24), Height: core.Pt(32), CornerRadius: 6, BorderWidth: 1, BorderColor: cur.LineRaised},
					OnPress: func() {
						ed.SetAssetPreviewPlaying(false)
						ed.StepAssetPreviewFrame(-1)
					},
				})
				for i := range maxThumbs {
					idx := i
					fr := clip.Frames[idx]
					active := idx == frameIdx
					bCol := cur.LineRaised
					bW := core.Points(1)
					if active {
						bCol = cur.Accent.Fg
						bW = 1.5
					}
					frItem := AssetDrawerItem{Kind: AssetKindSprites, Image: fr.Image, Rect: fr.Rect}
					frameButtons = append(frameButtons, core.View{
						TestID: fmt.Sprintf("asset-frame-%d", idx),
						Role:   core.RoleButton,
						Label:  fmt.Sprintf("Frame %d", idx+1),
						Style: core.Style{
							Width:        core.Pt(36),
							Height:       core.Pt(40),
							CornerRadius: 6,
							BorderWidth:  bW,
							BorderColor:  bCol,
							Overflow:     core.Hidden,
							FocusVisible: cur.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							ed.SetAssetPreviewFrame(idx)
						},
						Children: []core.Node{
							assetThumbnailCanvas(frItem, 36, 40, false, cur),
						},
					})
				}
				frameButtons = append(frameButtons, ui.Button{
					TestID:  "asset-frame-next",
					Icon:    lucide.ChevronRight,
					Label:   "Next frame",
					Variant: ui.ButtonGhost,
					Size:    ui.SizeSmall,
					Color:   func() core.Color { return cur.Raised },
					Tint:    func() core.Color { return cur.TextMuted },
					Style:   core.Style{Width: core.Pt(24), Height: core.Pt(32), CornerRadius: 6, BorderWidth: 1, BorderColor: cur.LineRaised},
					OnPress: func() {
						ed.SetAssetPreviewPlaying(false)
						ed.StepAssetPreviewFrame(1)
					},
				})

				secNodes = append(secNodes, core.View{
					TestID: "asset-preview-clips",
					Style:  core.Style{Gap: 8},
					Children: []core.Node{
						core.View{
							Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
							Children: clipChips,
						},
						core.View{
							Style: core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifySpaceBetween,
								Gap:            4,
							},
							Children: []core.Node{
								core.View{
									Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
									Children: frameButtons,
								},
								core.Text{
									TestID:  "asset-frame-label",
									Content: fmt.Sprintf("%d / %d · %d ms", frameIdx+1, len(clip.Frames), clip.FrameMS),
									Style:   core.Style{FontSize: 11, Color: cur.TextFaint},
								},
							},
						},
					},
				})
			}

			return core.View{
				Style:    core.Style{Gap: 16},
				Children: secNodes,
			}
		})

		bodyNodes := []core.Node{previewSection}

		// Missing asset alert card with Replace with a file and Ask the agent.
		if sel.Missing {
			refSummary := "project files"
			if len(sel.UsedBy) == 1 {
				refSummary = sel.UsedBy[0].File
			} else if len(sel.UsedBy) > 1 {
				refSummary = fmt.Sprintf("%s and %d other file(s)", sel.UsedBy[0].File, len(sel.UsedBy)-1)
			}
			bodyNodes = append(bodyNodes, core.View{
				TestID: "asset-missing-card",
				Style: core.Style{
					Gap:          12,
					Padding:      core.All(12),
					CornerRadius: 12,
					Background:   p.Raised,
					BorderWidth:  1.5,
					BorderColor:  p.Removed.Fg,
				},
				Children: []core.Node{
					core.Text{
						Content: fmt.Sprintf("%s is not on disk. %s still reference it; the build fails until it is back or replaced.", sel.Name, refSummary),
						Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
						Children: []core.Node{
							ui.Button{
								TestID:  "asset-missing-replace",
								Text:    "Replace with a file",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return p.LineRaised },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextSecondary,
								},
								Style: core.Style{Height: core.Pt(30), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
								OnPress: func() {
									ed.TriggerReplaceMissingAsset(sel.Path)
								},
							},
							ui.Button{
								TestID:  "asset-missing-ask-agent",
								Text:    "Ask the agent",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								Color:   func() core.Color { return p.Panel },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 500,
									Color:      p.TextMuted,
								},
								Style: core.Style{Height: core.Pt(30), CornerRadius: 8, Padding: core.Symmetric(0, 12)},
								OnPress: func() {
									ed.AskAgentAboutAsset(sel.Path, "Find or recreate missing asset "+sel.Path)
								},
							},
						},
					},
				},
			})
		}

		// Compact IMPORT settings summary for existing assets in a project (cached by path, filesRev, importRev).
		if !sel.Missing && ed.Project != nil {
			if info, ok := ed.cachedAssetImportSummary(sel.Path); ok {
				bodyNodes = append(bodyNodes, assetImportSummarySection(info, p))
			}
		}

		// USED BY section directly backed by refs.
		usedNodes := []core.Node{
			core.Text{
				TestID:  "asset-used-by-header",
				Content: fmt.Sprintf("USED BY · %d", len(sel.UsedBy)),
				Style: core.Style{
					FontSize:   11,
					FontWeight: 600,
					Color:      p.TextFaint,
					Padding:    core.Edges{Bottom: 4},
				},
			},
		}
		if len(sel.UsedBy) == 0 {
			usedNodes = append(usedNodes, core.Text{
				TestID:  "asset-used-by-empty",
				Content: "Nothing references it. The agent can propose deleting it; deleting files always asks you.",
				Style: core.Style{
					Padding:  core.Symmetric(4, 8),
					FontSize: 12,
					Color:    p.TextFaint,
				},
			})
		} else {
			for i, r := range sel.UsedBy {
				refEntry := r
				usedNodes = append(usedNodes, core.View{
					TestID: fmt.Sprintf("asset-ref-%d", i),
					Role:   core.RoleButton,
					Label:  refEntry.File + " " + refEntry.What,
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Height:       core.Pt(28),
						Gap:          8,
						Padding:      core.Symmetric(0, 8),
						CornerRadius: 6,
						FocusVisible: p.FocusRing(-2),
					},
					OnPress: func(core.PressEvent) {
						ed.report(ed.OpenReference(refEntry.Ref))
					},
					Children: []core.Node{
						core.Text{
							Content:  refEntry.File,
							MaxLines: 1,
							Style: core.Style{
								FlexGrow:   1,
								FlexShrink: 1,
								MinWidth:   core.Pt(0),
								FontSize:   11.5,
								Color:      p.TextMuted,
							},
						},
						core.Text{
							Content:  refEntry.What,
							MaxLines: 1,
							Style: core.Style{
								FontSize:   11.5,
								Color:      p.TextFaint,
								FlexShrink: 0,
							},
						},
					},
				})
			}
		}
		bodyNodes = append(bodyNodes, core.View{
			TestID:   "asset-used-by-section",
			Style:    core.Style{Gap: 2},
			Children: usedNodes,
		})

		return core.View{
			Style: core.Style{FlexGrow: 1},
			Children: []core.Node{
				core.View{
					TestID: "asset-inspector-header",
					Style: core.Style{
						Height:      core.Pt(44),
						FlexShrink:  0,
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Gap:         8,
						Padding:     core.Symmetric(0, 16),
						Borders:     core.Edges{Bottom: 1},
						BorderColor: p.Line,
					},
					Children: []core.Node{
						core.Text{
							TestID:   "asset-inspector-name",
							Content:  sel.Name,
							MaxLines: 1,
							Style: core.Style{
								FontSize:   14,
								FontWeight: 600,
								Color:      p.TextPrimary,
								FlexShrink: 1,
							},
						},
						core.Text{
							TestID:  "asset-inspector-kind",
							Content: sel.KindLabel,
							Style:   core.Style{FontSize: 12, Color: p.TextFaint, FlexShrink: 0},
						},
					},
				},
				ui.ScrollView{
					Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
					EdgeColor:    func() core.Color { return p.Panel },
					ContentStyle: core.Style{Padding: core.All(16), Gap: 16},
					Children:     bodyNodes,
				},
				core.View{
					Style: core.Style{
						Height:      core.Pt(44),
						FlexShrink:  0,
						Direction:   core.Row,
						AlignItems:  core.AlignCenter,
						Padding:     core.Symmetric(0, 16),
						Borders:     core.Edges{Top: 1},
						BorderColor: p.Line,
					},
					Children: []core.Node{
						core.Text{
							Content: "Re-imports by itself when the file changes on disk",
							Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
						},
					},
				},
			},
		}
	})

	return core.View{
		TestID: "asset-inspector",
		Role:   core.RoleRegion,
		Label:  "Selected asset inspector",
		StyleFunc: func() core.Style {
			p := ed.SpecPalette(cx)
			return core.Style{
				Width:       core.Pt(380),
				FlexShrink:  0,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				Overflow:    core.Hidden,
			}
		},
		Children: []core.Node{content},
	}
}

func assetImportSummarySection(info AssetImportInfo, p SpecPalette) core.Node {
	row := func(label, val string) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
				Padding:    core.Symmetric(4, 8),
			},
			Children: []core.Node{
				core.Text{Content: label, Style: core.Style{Width: core.Pt(92), FontSize: 12, Color: p.TextMuted}},
				core.View{
					Style: core.Style{
						Padding:      core.Symmetric(2, 8),
						CornerRadius: 6,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.Text{Content: val, Style: core.Style{FontSize: 12, Color: p.TextSecondary}},
					},
				},
			},
		}
	}

	rows := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Padding:        core.Edges{Bottom: 4},
			},
			Children: []core.Node{
				core.Text{Content: "IMPORT", Style: core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint}},
				core.Text{Content: info.SidecarPath, MaxLines: 1, Style: core.Style{FontSize: 11, Color: p.TextFaint}},
			},
		},
	}
	switch info.Kind {
	case AssetImportImage:
		filter, _ := info.EffectiveImageFilter()
		format, _ := info.EffectiveImageFormat()
		rows = append(rows, row("Filter", filter), row("Format", format))
		if info.AtlasTarget != nil {
			rows = append(rows, row("Atlas", info.AtlasTarget.Name))
		}
	case AssetImportAudio:
		format, _ := info.EffectiveAudioFormat()
		rate, _ := info.EffectiveAudioRate()
		rows = append(rows, row("Format", format), row("Sample rate", fmt.Sprintf("%d Hz", rate)))
	case AssetImportFont:
		size, _ := info.EffectiveFontSize()
		pad, _ := info.EffectiveFontPadding()
		rows = append(rows, row("Size", fmt.Sprintf("%d px", size)), row("Padding", fmt.Sprintf("%d px", pad)))
	}
	return core.View{
		TestID:   "asset-import-summary",
		Style:    core.Style{Gap: 4},
		Children: rows,
	}
}

func assetThumbnailCanvas(it AssetDrawerItem, w, h float32, audioPlaying bool, p SpecPalette) core.Node {
	checkerAlt := specAlpha(p.LineRaised, 0.36)
	return core.Canvas{
		Style: core.Style{
			Width:  core.Pct(100),
			Height: core.Pct(100),
		},
		Draw: func(d *core.Drawing, size core.Size) {
			sw, sh := float32(size.Width), float32(size.Height)
			if sw <= 0 || sh <= 0 {
				sw, sh = w, h
			}
			// Checkerboard background built strictly from Spec tokens.
			d.SetFillColor(p.Raised).FillRect(0, 0, sw, sh)
			const cell float32 = 8
			d.SetFillColor(checkerAlt)
			for y := float32(0); y < sh; y += cell {
				row := int(y / cell)
				for x := float32(0); x < sw; x += cell {
					if (row+int(x/cell))%2 == 0 {
						d.FillRect(x, y, min(cell, sw-x), min(cell, sh-y))
					}
				}
			}

			if it.Missing || it.Kind == AssetKindFonts {
				return
			}

			if it.Kind == AssetKindAudio {
				wave := it.AudioWave
				if len(wave) == 0 {
					wave = synthWaveform(it.Path, 36)
				}
				padX := sw * 0.10
				usableW := max(16, sw-padX*2)
				usableH := sh * 0.62
				midY := sh / 2
				barCol := p.Accent.Fg
				if it.Proposed {
					barCol = p.Added.Fg
				}
				d.SetFillColor(barCol)
				step := usableW / float32(len(wave))
				barW := max(1.5, step*0.55)
				for i, amp := range wave {
					bh := max(2, amp*usableH)
					bx := padX + float32(i)*step
					d.FillRect(bx, midY-bh/2, barW, bh)
				}
				if audioPlaying {
					hx := padX + usableW*0.38
					d.SetFillColor(p.TextPrimary).FillRect(hx, sh*0.14, 1.5, sh*0.72)
				}
				return
			}

			// Sprite or Tileset with loaded image pixels/URI/Data.
			if it.Rect.W > 0 && it.Rect.H > 0 && hasImageSource(it.Image) {
				pad := min(sw, sh) * 0.14
				availW := max(8, sw-pad*2)
				availH := max(8, sh-pad*2)
				scale := min(availW/it.Rect.W, availH/it.Rect.H)
				dw, dh := it.Rect.W*scale, it.Rect.H*scale
				dx, dy := (sw-dw)/2, (sh-dh)/2
				d.DrawImageFrom(it.Image, it.Rect.X, it.Rect.Y, it.Rect.W, it.Rect.H, dx, dy, dw, dh)
				return
			}

			// Tileset fallback grid preview when image bytes are not decoded.
			if it.Kind == AssetKindTiles {
				gw := min(sw*0.62, 160)
				gh := min(sh*0.65, 112)
				gx, gy := (sw-gw)/2, (sh-gh)/2
				d.SetFillColor(p.LineRaised).FillRect(gx, gy, gw, gh)
				d.SetStrokeColor(p.Line).SetLineWidth(1.5)
				for x := gx; x <= gx+gw; x += 16 {
					d.BeginPath().MoveTo(x, gy).LineTo(x, gy+gh).Stroke()
				}
				for y := gy; y <= gy+gh; y += 16 {
					d.BeginPath().MoveTo(gx, y).LineTo(gx+gw, y).Stroke()
				}
			}
		},
	}
}

// assetDragViewportOverlay renders the floating drag status pill ("coin.png new Sprite on Actors · snapped to 16 px")
// and the bottom-right viewport hint ("Drop on an object to swap its sprite · Esc cancels") in Assets mode.
func assetDragViewportOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		p := ed.SpecPalette(cx)
		bgOverlay := specAlpha(p.Bg, 0.92)
		inAssets := ed.LeftRailMode() == LeftRailAssets
		st := ed.AssetDragState()
		if !inAssets && !st.Active {
			return core.View{PointerEvents: core.PointerEventsNone}
		}

		var nodes []core.Node
		if inAssets {
			vw, vh := ed.ViewportSize()
			nodes = append(nodes,
				core.Text{
					TestID:  "assets-viewport-camera-label",
					Content: fmt.Sprintf("Camera · %.0f × %.0f", vw, vh),
					Style: core.Style{
						Position: core.Absolute,
						Left:     core.Pt(16),
						Top:      core.Pt(8),
						FontSize: 11,
						Color:    p.TextFaint,
					},
				},
				core.Text{
					TestID:  "assets-viewport-drop-hint",
					Content: "Drop on an object to swap its sprite · Esc cancels",
					Style: core.Style{
						Position: core.Absolute,
						Right:    core.Pt(16),
						Bottom:   core.Pt(6),
						FontSize: 11,
						Color:    p.TextFaint,
					},
				},
			)
		}

		if st.Active && st.HoveringViewport {
			px := max(float32(12), st.CanvasX-120)
			py := max(float32(28), st.CanvasY-42)
			nodes = append(nodes, core.View{
				TestID: "asset-drag-pill",
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(core.Points(px)),
					Top:          core.Pt(core.Points(py)),
					Height:       core.Pt(24),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 6,
					Background:   bgOverlay,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   p.Added.Fg,
						},
					},
					core.Text{
						Content: st.PillName,
						Style:   core.Style{FontSize: 11.5, FontWeight: 600, Color: p.TextPrimary},
					},
					core.Text{
						Content: st.PillDetail,
						Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
					},
				},
			})
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
			Children: nodes,
		}
	})
}
