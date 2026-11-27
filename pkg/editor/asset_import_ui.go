package editor

import (
	"fmt"
	"image"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// assetImportInspectorView renders the Inspector panel for an asset file selected in the file browser:
// its kind-specific import settings (inheriting defaults from game.yaml's targets, with per-field Reset),
// its credit metadata, and Reimport / Build Assets actions.
func assetImportInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], relPath string, kind AssetImportKind) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	buildDisabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.AssetsBuilding()
	}

	type infoSnapshot struct {
		rev  uint64
		info AssetImportInfo
	}
	infoMemo := core.NewMemoFunc(cx, func() infoSnapshot {
		r := rev.Get()
		info, _ := ed.InspectAssetImport(relPath)
		return infoSnapshot{rev: r, info: info}
	}, func(a, b infoSnapshot) bool {
		return a.rev == b.rev
	})
	readInfo := func() AssetImportInfo {
		return infoMemo.Get().info
	}

	sectionHeader := func(title string) core.Node {
		return core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		}
	}

	resetBtn := func(testID, label string, overridden func() bool, onReset func()) core.Node {
		return ui.IconButton{
			TestID: testID,
			Icon:   lucide.RotateCcw,
			Label:  label,
			Disabled: func() bool {
				return disabled() || !overridden()
			},
			OnPress: onReset,
		}
	}

	fieldRow := func(label string, tip func() string, control core.Node, reset core.Node) core.Node {
		right := control
		if reset != nil {
			right = withTrailing(control, reset)
		}
		var labelNode core.Node = core.Text{
			Content: label,
			Style: core.Style{
				Width:    core.Pt(72),
				FontSize: 12,
				Color:    colors.Text(),
			},
		}
		if tip != nil {
			// Evaluate static tooltip text untracked so InspectorView's outer core.Dynamic
			// does not subscribe to rev and remount this panel (wiping focus and drafts).
			if t := core.Untracked(cx, tip); t != "" {
				labelNode = withTooltip(labelNode, t)
			}
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: []core.Node{
				labelNode,
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{right},
				},
			},
		}
	}

	nodes := []core.Node{
		core.View{
			Style: core.Style{Gap: 4},
			Children: []core.Node{
				core.Text{
					TestID:  "import-asset-path",
					Content: relPath,
					Style: core.Style{
						FontSize:   13,
						FontWeight: 700,
						Color:      colors.Text(),
					},
				},
				ui.HStack{
					Gap: 6,
					Children: []core.Node{
						core.View{
							TestID: "import-asset-kind",
							Children: []core.Node{
								ui.Badge{
									Text:    localizedImportKind(ed, kind),
									Variant: ui.BadgeNeutral,
								},
							},
						},
						core.Text{
							TestID: "import-asset-target",
							ContentFunc: func() string {
								return readInfo().TargetSummary()
							},
							Style: core.Style{
								FontSize: 11,
								Color:    colors.Muted(),
								FlexGrow: 1,
							},
						},
					},
				},
			},
		},
		ui.HStack{
			Gap: 6,
			Children: []core.Node{
				ui.Button{
					TestID:   "import-reimport",
					Text:     ed.T("assetImport.reimport"),
					Label:    ed.T("assetImport.reimportLabel"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonPrimary,
					Disabled: buildDisabled,
					OnPress: func() {
						ed.report(ed.ReimportAsset(relPath))
					},
				},
				ui.Button{
					TestID:   "import-build-assets",
					Text:     ed.T("assetImport.buildAssets"),
					Label:    ed.T("assetImport.buildAssetsLabel"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: buildDisabled,
					OnPress: func() {
						ed.report(ed.BuildAssets())
					},
				},
			},
		},
		core.Show{
			When: func() bool { return readInfo().SidecarErr != nil },
			Then: func(cx *core.Scope) core.Node {
				return core.Text{
					TestID: "import-sidecar-error",
					ContentFunc: func() string {
						if err := readInfo().SidecarErr; err != nil {
							return err.Error()
						}
						return ""
					},
					Style: core.Style{FontSize: 11, Color: colors.Danger()},
				}
			},
		},
	}

	switch kind {
	case AssetImportImage:
		nodes = append(nodes, sectionHeader(ed.T("assetImport.section.image")))
		nodes = append(nodes, imageImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	case AssetImportAudio:
		nodes = append(nodes, sectionHeader(ed.T("assetImport.section.audio")))
		nodes = append(nodes, audioImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	case AssetImportFont:
		nodes = append(nodes, sectionHeader(ed.T("assetImport.section.font")))
		nodes = append(nodes, fontImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	case AssetImportModel:
		nodes = append(nodes, sectionHeader(ed.T("assetImport.section.model")))
		nodes = append(nodes, modelImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	}

	nodes = append(nodes, creditImportSection(cx, ed, rev, relPath, readInfo, disabled, colors))

	return core.View{
		TestID:   "inspector-asset-import",
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func imageImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	filterOpts := make([]ui.Option[string], len(importsettings.Filters))
	for i, f := range importsettings.Filters {
		filterOpts[i] = ui.Option[string]{Value: f, Label: f}
	}
	formatOpts := make([]ui.Option[string], len(project.TextureFormats))
	for i, f := range project.TextureFormats {
		formatOpts[i] = ui.Option[string]{Value: f, Label: f}
	}
	colors := ui.ColorsOf(cx)

	updateSlice := func(mutate func(s *importsettings.Slice)) {
		s, ok := readInfo().EffectiveImageSlice()
		if !ok || s == nil {
			s = &importsettings.Slice{Width: 16, Height: 16}
		}
		mutate(s)
		ed.report(ed.SetImageImportSlice(relPath, s))
	}

	type sliceGridSnapshot struct {
		cellCount int
		cols      int
		rows      int
		errMsg    string
	}
	gridMemo := core.NewMemo(cx, func() sliceGridSnapshot {
		info := readInfo()
		s, ok := info.EffectiveImageSlice()
		if !ok || s == nil || info.ImageWidth <= 0 || info.ImageHeight <= 0 {
			return sliceGridSnapshot{}
		}
		n, c, r, err := s.GridCounts(image.Rect(0, 0, info.ImageWidth, info.ImageHeight))
		if err != nil {
			return sliceGridSnapshot{cols: c, rows: r, errMsg: err.Error()}
		}
		return sliceGridSnapshot{cellCount: n, cols: c, rows: r}
	})
	readGrid := func() sliceGridSnapshot {
		return gridMemo.Get()
	}

	activeCell := func() (int, int) {
		g := readGrid()
		if g.cellCount <= 0 || g.errMsg != "" {
			return 0, 0
		}
		sel := ed.SelectedImportSliceCell()
		if sel < 0 || sel >= g.cellCount {
			sel = 0
		}
		return sel, g.cellCount
	}

	formatPivotNum := func(v float64) string {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}

	var previewW, previewH float32 = 240, 180
	var pressPos core.Point
	var hasPress, panActive, mouseDragging bool

	cellSelectKey := core.NewMemo(cx, func() string {
		g := readGrid()
		if g.cellCount <= 0 || g.errMsg != "" {
			return ""
		}
		if g.cellCount > maxImportPreviewCells {
			return fmt.Sprintf("num:%d", g.cellCount)
		}
		info := readInfo()
		var b strings.Builder
		fmt.Fprintf(&b, "%d:", g.cellCount)
		if pv, ok := info.EffectiveImagePivot(); ok && pv != nil {
			for _, c := range pv.Cells {
				fmt.Fprintf(&b, "%d,", c.Cell)
			}
		}
		return b.String()
	})

	previewSection := core.View{
		Style: core.Style{Gap: 4},
		Children: []core.Node{
			core.View{
				TestID: "import-image-preview",
				Style: core.Style{
					Width:        core.Pct(100),
					Height:       core.Pt(180),
					CornerRadius: 4,
					BorderWidth:  1,
					BorderColor:  colors.Border(),
					Overflow:     core.Hidden,
				},
				OnLayout: func(b core.Bounds) {
					if b.Size.Width > 0 {
						previewW = float32(b.Size.Width)
					}
					if b.Size.Height > 0 {
						previewH = float32(b.Size.Height)
					}
				},
				OnTouch: func(ev core.TouchEvent) {
					if disabled() {
						return
					}
					switch ev.Phase {
					case core.TouchStart:
						for _, t := range ev.Touches {
							if t.Changed {
								pressPos = t.Position
								hasPress = true
								return
							}
						}
					case core.TouchEnd, core.TouchCancel:
						hasPress = false
					}
				},
				OnPan: func(ev core.PanEvent) {
					if disabled() {
						if panActive {
							ed.CancelImagePivotDrag()
							panActive = false
						}
						mouseDragging = false
						return
					}
					switch ev.Phase {
					case core.GestureBegan:
						if mouseDragging && ed.ImagePivotDragging() {
							// Adopt the drag already started by OnPressDown without overwriting origPivot.
							panActive = true
							mouseDragging = false
						} else if hasPress && ed.BeginImagePivotDrag(relPath, float32(pressPos.X), float32(pressPos.Y), previewW, previewH) {
							panActive = true
							mouseDragging = false
						}
					case core.GestureChanged:
						if panActive {
							ed.UpdateImagePivotDrag(
								float32(pressPos.X)+float32(ev.Translation.X),
								float32(pressPos.Y)+float32(ev.Translation.Y),
							)
						}
					case core.GestureEnded:
						if panActive {
							ed.report(ed.EndImagePivotDrag())
							panActive = false
						}
						mouseDragging = false
					case core.GestureCancelled:
						if panActive {
							ed.CancelImagePivotDrag()
							panActive = false
						}
						mouseDragging = false
					}
				},
				OnPressDown: func(ev core.PressEvent) {
					if disabled() || ev.Keyboard || panActive {
						return
					}
					pressPos = ev.Position
					hasPress = true
					if ed.BeginImagePivotDrag(relPath, float32(ev.Position.X), float32(ev.Position.Y), previewW, previewH) {
						mouseDragging = true
					}
				},
				OnMouseMove: func(ev core.MouseEvent) {
					if !mouseDragging || panActive {
						return
					}
					if !ed.ImagePivotDragging() {
						mouseDragging = false
						return
					}
					if ev.PressedButton == core.MouseButtonNone {
						// The button was released outside the preview box; finish the drag cleanly.
						mouseDragging = false
						ed.report(ed.EndImagePivotDrag())
						return
					}
					ed.UpdateImagePivotDrag(float32(ev.Position.X), float32(ev.Position.Y))
				},
				OnPressUp: func(ev core.PressEvent) {
					hasPress = false
					if !mouseDragging {
						return
					}
					mouseDragging = false
					if !panActive {
						ed.report(ed.EndImagePivotDrag())
					}
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "import-image-preview-canvas",
						Label:  ed.T("assetImport.image.previewCanvasLabel"),
						Style: core.Style{
							Width:  core.Pct(100),
							Height: core.Pct(100),
						},
						Draw: func(d *core.Drawing, size core.Size) {
							_ = rev.Get()
							if size.Width > 0 {
								previewW = float32(size.Width)
							}
							if size.Height > 0 {
								previewH = float32(size.Height)
							}
							ed.DrawImageImportPreview(d, size, relPath)
						},
					},
				},
			},
			core.Text{
				TestID: "import-image-preview-info",
				ContentFunc: func() string {
					info := readInfo()
					if info.ImageWidth <= 0 || info.ImageHeight <= 0 {
						return ed.T("assetImport.image.previewUnavailable")
					}
					if _, hasSlice := info.EffectiveImageSlice(); hasSlice {
						g := readGrid()
						if g.errMsg != "" {
							return ed.T("assetImport.image.previewInvalidSlice", l10n.Param("w", info.ImageWidth), l10n.Param("h", info.ImageHeight))
						}
						if g.cellCount > maxImportPreviewCells {
							return ed.T("assetImport.image.previewGridLarge",
								l10n.Param("w", info.ImageWidth),
								l10n.Param("h", info.ImageHeight),
								l10n.Param("cols", g.cols),
								l10n.Param("rows", g.rows),
								l10n.Param("cells", g.cellCount),
							)
						}
						return ed.T("assetImport.image.previewGrid",
							l10n.Param("w", info.ImageWidth),
							l10n.Param("h", info.ImageHeight),
							l10n.Param("cols", g.cols),
							l10n.Param("rows", g.rows),
							l10n.Param("cells", g.cellCount),
						)
					}
					return ed.T("assetImport.image.previewUnsliced", l10n.Param("w", info.ImageWidth), l10n.Param("h", info.ImageHeight))
				},
				Style: core.Style{FontSize: 10, Color: colors.Muted()},
			},
			core.Show{
				When: func() bool {
					return readGrid().errMsg != ""
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID: "import-image-slice-error",
						ContentFunc: func() string {
							return readGrid().errMsg
						},
						Style: core.Style{FontSize: 11, Color: colors.Danger()},
					}
				},
			},
		},
	}

	pivotSection := []core.Node{
		core.Text{
			Content: ed.T("assetImport.section.pivot"),
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		},
		fieldRow(ed.T("assetImport.pivot.default"), func() string {
			return ed.T("assetImport.pivot.defaultTip")
		},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.NumberInput{
								TestID:   "import-image-pivot-default-x",
								Label:    ed.T("assetImport.pivot.defaultX"),
								Step:     0.05,
								Format:   formatPivotNum,
								Disabled: disabled,
								Value: func() float64 {
									pt, _ := readInfo().EffectiveDefaultImagePivot()
									return pt[0]
								},
								OnChange: func(v float64) {
									if math.IsNaN(v) || math.IsInf(v, 0) {
										return
									}
									pt, _ := readInfo().EffectiveDefaultImagePivot()
									pt[0] = roundPivotCoord(v)
									ed.report(ed.SetImageImportDefaultPivot(relPath, &pt))
								},
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.NumberInput{
								TestID:   "import-image-pivot-default-y",
								Label:    ed.T("assetImport.pivot.defaultY"),
								Step:     0.05,
								Format:   formatPivotNum,
								Disabled: disabled,
								Value: func() float64 {
									pt, _ := readInfo().EffectiveDefaultImagePivot()
									return pt[1]
								},
								OnChange: func(v float64) {
									if math.IsNaN(v) || math.IsInf(v, 0) {
										return
									}
									pt, _ := readInfo().EffectiveDefaultImagePivot()
									pt[1] = roundPivotCoord(v)
									ed.report(ed.SetImageImportDefaultPivot(relPath, &pt))
								},
							},
						},
					},
				},
			},
			resetBtn("import-image-pivot-default-reset", ed.T("assetImport.pivot.resetDefault"), func() bool {
				_, over := readInfo().EffectiveDefaultImagePivot()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "pivot.default"))
			}),
		),
		fieldRow(ed.T("assetImport.pivot.preset"), func() string { return ed.T("assetImport.pivot.presetTip") },
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					ui.Button{
						TestID:   "import-image-pivot-preset-tl",
						Text:     ed.T("assetImport.pivot.presetTL"),
						Label:    ed.T("assetImport.pivot.presetTLLabel"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							pt := importsettings.PivotPoint{0, 0}
							ed.report(ed.SetImageImportDefaultPivot(relPath, &pt))
						},
					},
					ui.Button{
						TestID:   "import-image-pivot-preset-center",
						Text:     ed.T("assetImport.pivot.presetCenter"),
						Label:    ed.T("assetImport.pivot.presetCenterLabel"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							pt := importsettings.PivotPoint{0.5, 0.5}
							ed.report(ed.SetImageImportDefaultPivot(relPath, &pt))
						},
					},
					ui.Button{
						TestID:   "import-image-pivot-preset-bottom",
						Text:     ed.T("assetImport.pivot.presetBottom"),
						Label:    ed.T("assetImport.pivot.presetBottomLabel"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							pt := importsettings.PivotPoint{0.5, 1}
							ed.report(ed.SetImageImportDefaultPivot(relPath, &pt))
						},
					},
				},
			},
			nil,
		),
		core.Show{
			When: func() bool {
				g := readGrid()
				return g.cellCount > 0 && g.errMsg == ""
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						fieldRow(ed.T("assetImport.pivot.cell"), func() string { return ed.T("assetImport.pivot.cellTip") },
							core.Dynamic(func(cx *core.Scope) core.Node {
								key := cellSelectKey.Get()
								if key == "" {
									return nil
								}
								g := core.Untracked(cx, readGrid)
								n := g.cellCount
								if n > maxImportPreviewCells {
									return ui.NumberInput{
										TestID:   "import-image-pivot-cell-select",
										Label:    ed.T("assetImport.pivot.cellLabel"),
										Min:      0,
										Max:      float64(n - 1),
										Step:     1,
										Disabled: disabled,
										Value: func() float64 {
											sel, _ := activeCell()
											return float64(sel)
										},
										OnChange: func(v float64) {
											idx := roundInt(v, 0)
											if idx >= n {
												idx = n - 1
											}
											ed.SelectImportSliceCell(idx)
										},
									}
								}
								info := core.Untracked(cx, readInfo)
								overridden := map[int]bool{}
								if pv, ok := info.EffectiveImagePivot(); ok && pv != nil {
									for _, c := range pv.Cells {
										overridden[c.Cell] = true
									}
								}
								opts := make([]ui.Option[int], n)
								for i := range n {
									lbl := ed.T("assetImport.pivot.cellOption", l10n.Param("index", i))
									if overridden[i] {
										lbl += " *"
									}
									opts[i] = ui.Option[int]{Value: i, Label: lbl}
								}
								return ui.Select[int]{
									TestID:   "import-image-pivot-cell-select",
									Label:    ed.T("assetImport.pivot.cellLabel"),
									Options:  opts,
									Disabled: disabled,
									Style:    core.Style{Width: core.Pct(100)},
									Value: func() int {
										sel, _ := activeCell()
										return sel
									},
									OnChange: func(v int) {
										ed.SelectImportSliceCell(v)
									},
								}
							}),
							resetBtn("import-image-pivot-cell-reset", ed.T("assetImport.pivot.resetCell"), func() bool {
								info := readInfo()
								sel, n := activeCell()
								if n == 0 {
									return false
								}
								_, over, _ := info.EffectiveCellImagePivot(sel)
								return over
							}, func() {
								sel, n := activeCell()
								if n > 0 {
									ed.report(ed.SetImageImportCellPivot(relPath, sel, nil))
								}
							}),
						),
						fieldRow(ed.T("assetImport.pivot.cellPivot"), func() string {
							return ed.T("assetImport.pivot.cellPivotTip")
						},
							ui.HStack{
								Gap: 4,
								Children: []core.Node{
									core.View{
										Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
										Children: []core.Node{
											ui.NumberInput{
												TestID:   "import-image-pivot-cell-x",
												Label:    ed.T("assetImport.pivot.cellPivotX"),
												Step:     0.05,
												Format:   formatPivotNum,
												Disabled: disabled,
												Value: func() float64 {
													info := readInfo()
													sel, _ := activeCell()
													pt, _, _ := info.EffectiveCellImagePivot(sel)
													return pt[0]
												},
												OnChange: func(v float64) {
													if math.IsNaN(v) || math.IsInf(v, 0) {
														return
													}
													info := readInfo()
													sel, n := activeCell()
													if n == 0 {
														return
													}
													pt, _, _ := info.EffectiveCellImagePivot(sel)
													pt[0] = roundPivotCoord(v)
													ed.report(ed.SetImageImportCellPivot(relPath, sel, &pt))
												},
											},
										},
									},
									core.View{
										Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
										Children: []core.Node{
											ui.NumberInput{
												TestID:   "import-image-pivot-cell-y",
												Label:    ed.T("assetImport.pivot.cellPivotY"),
												Step:     0.05,
												Format:   formatPivotNum,
												Disabled: disabled,
												Value: func() float64 {
													info := readInfo()
													sel, _ := activeCell()
													pt, _, _ := info.EffectiveCellImagePivot(sel)
													return pt[1]
												},
												OnChange: func(v float64) {
													if math.IsNaN(v) || math.IsInf(v, 0) {
														return
													}
													info := readInfo()
													sel, n := activeCell()
													if n == 0 {
														return
													}
													pt, _, _ := info.EffectiveCellImagePivot(sel)
													pt[1] = roundPivotCoord(v)
													ed.report(ed.SetImageImportCellPivot(relPath, sel, &pt))
												},
											},
										},
									},
								},
							},
							nil,
						),
					},
				}
			},
		},
	}

	nodes := []core.Node{
		previewSection,
		fieldRow(ed.T("assetImport.image.filter"), func() string {
			return ed.T("assetImport.image.filterTip", l10n.Param("default", readInfo().DefaultImageFilter))
		},
			ui.Select[string]{
				TestID:   "import-image-filter",
				Label:    ed.T("assetImport.image.filterLabel"),
				Options:  filterOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					v, _ := readInfo().EffectiveImageFilter()
					return v
				},
				OnChange: func(v string) {
					ed.report(ed.SetImageImportFilter(relPath, v))
				},
			},
			resetBtn("import-image-filter-reset", ed.T("assetImport.image.filterReset"), func() bool {
				_, over := readInfo().EffectiveImageFilter()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "filter"))
			}),
		),
		fieldRow(ed.T("assetImport.image.format"), func() string {
			return ed.T("assetImport.image.formatTip", l10n.Param("default", readInfo().DefaultImageFormat))
		},
			ui.Select[string]{
				TestID:   "import-image-format",
				Label:    ed.T("assetImport.image.formatLabel"),
				Options:  formatOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					v, _ := readInfo().EffectiveImageFormat()
					return v
				},
				OnChange: func(v string) {
					ed.report(ed.SetImageImportFormat(relPath, v))
				},
			},
			resetBtn("import-image-format-reset", ed.T("assetImport.image.formatReset"), func() bool {
				_, over := readInfo().EffectiveImageFormat()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "format"))
			}),
		),
		fieldRow(ed.T("assetImport.image.mipmaps"), func() string {
			return ed.T("assetImport.image.mipmapsTip", l10n.Param("default", readInfo().DefaultImageMipmaps))
		},
			ui.Checkbox{
				TestID:   "import-image-mipmaps",
				Label:    ed.T("assetImport.image.mipmaps"),
				Disabled: disabled,
				Checked: func() bool {
					v, _ := readInfo().EffectiveImageMipmaps()
					return v
				},
				OnChange: func(on bool) {
					ed.report(ed.SetImageImportMipmaps(relPath, &on))
				},
			},
			resetBtn("import-image-mipmaps-reset", ed.T("assetImport.image.mipmapsReset"), func() bool {
				_, over := readInfo().EffectiveImageMipmaps()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "mipmaps"))
			}),
		),
		fieldRow(ed.T("assetImport.image.srgb"), func() string {
			return ed.T("assetImport.image.srgbTip", l10n.Param("default", readInfo().DefaultImageSRGB))
		},
			ui.Checkbox{
				TestID:   "import-image-srgb",
				Label:    ed.T("assetImport.image.srgb"),
				Disabled: disabled,
				Checked: func() bool {
					v, _ := readInfo().EffectiveImageSRGB()
					return v
				},
				OnChange: func(on bool) {
					ed.report(ed.SetImageImportSRGB(relPath, &on))
				},
			},
			resetBtn("import-image-srgb-reset", ed.T("assetImport.image.srgbReset"), func() bool {
				_, over := readInfo().EffectiveImageSRGB()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "srgb"))
			}),
		),
		fieldRow(ed.T("assetImport.image.slice"), func() string { return ed.T("assetImport.image.sliceTip") },
			ui.Checkbox{
				TestID:   "import-image-slice-enabled",
				Label:    ed.T("assetImport.image.sliceLabel"),
				Disabled: disabled,
				Checked: func() bool {
					_, over := readInfo().EffectiveImageSlice()
					return over
				},
				OnChange: func(on bool) {
					if !on {
						ed.report(ed.SetImageImportSlice(relPath, nil))
						return
					}
					ed.report(ed.SetImageImportSlice(relPath, &importsettings.Slice{Width: 16, Height: 16}))
				},
			},
			resetBtn("import-image-slice-reset", ed.T("assetImport.image.sliceReset"), func() bool {
				_, over := readInfo().EffectiveImageSlice()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "slice"))
			}),
		),
		core.Show{
			When: func() bool {
				_, over := readInfo().EffectiveImageSlice()
				return over
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						fieldRow(ed.T("assetImport.image.sliceWidth"), nil, ui.NumberInput{
							TestID:   "import-image-slice-width",
							Label:    ed.T("assetImport.image.sliceWidthLabel"),
							Min:      1,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Width)
								}
								return 16
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Width = roundInt(v, 1) })
							},
						}, nil),
						fieldRow(ed.T("assetImport.image.sliceHeight"), nil, ui.NumberInput{
							TestID:   "import-image-slice-height",
							Label:    ed.T("assetImport.image.sliceHeightLabel"),
							Min:      1,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Height)
								}
								return 16
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Height = roundInt(v, 1) })
							},
						}, nil),
						fieldRow(ed.T("assetImport.image.sliceMargin"), nil, ui.NumberInput{
							TestID:   "import-image-slice-margin",
							Label:    ed.T("assetImport.image.sliceMarginLabel"),
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Margin)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Margin = roundInt(v, 0) })
							},
						}, nil),
						fieldRow(ed.T("assetImport.image.sliceSpacing"), nil, ui.NumberInput{
							TestID:   "import-image-slice-spacing",
							Label:    ed.T("assetImport.image.sliceSpacingLabel"),
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Spacing)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Spacing = roundInt(v, 0) })
							},
						}, nil),
						fieldRow(ed.T("assetImport.image.sliceCount"), nil, ui.NumberInput{
							TestID:   "import-image-slice-count",
							Label:    ed.T("assetImport.image.sliceCountLabel"),
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Count)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Count = roundInt(v, 0) })
							},
						}, nil),
					},
				}
			},
		},
	}
	collisionModeOpts := []ui.Option[string]{
		{Value: "", Label: "None"},
		{Value: "alpha", Label: "Alpha (traced)"},
		{Value: "box", Label: "Box"},
		{Value: "ellipse", Label: "Ellipse"},
		{Value: "polygon", Label: "Polygon"},
	}

	activeCollisionCell := func() int {
		g := readGrid()
		if g.cellCount <= 0 || g.errMsg != "" {
			return -1
		}
		sel, _ := activeCell()
		return sel
	}

	collisionSection := []core.Node{
		core.Text{
			Content: ed.T("assetImport.collisionHeading"),
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		},
		fieldRow(ed.T("assetImport.collisionMode"), func() string {
			return ed.T("assetImport.collisionModeTooltip")
		},
			ui.Select[string]{
				TestID:   "import-image-collision-mode",
				Label:    ed.T("assetImport.collisionModeLabel"),
				Options:  collisionModeOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					if col, ok := readInfo().EffectiveImageCollision(); ok && col != nil {
						if col.Mode == "none" {
							return ""
						}
						return col.Mode
					}
					return ""
				},
				OnChange: func(v string) {
					if v == "" {
						ed.report(ed.SetImageImportDefaultCollision(relPath, "", 0, 0, nil))
						return
					}
					var tol, maxV int
					var pts []importsettings.Point2D
					if col, ok := readInfo().EffectiveImageCollision(); ok && col != nil {
						tol = col.Tolerance
						maxV = col.MaxVertices
						pts = col.Points
					}
					if v == "polygon" && len(pts) < importsettings.MinCollisionVertices {
						info := readInfo()
						w := float64(max(info.ImageWidth, 16))
						h := float64(max(info.ImageHeight, 16))
						if s, ok := info.EffectiveImageSlice(); ok && s != nil {
							w, h = float64(s.Width), float64(s.Height)
						}
						pts = []importsettings.Point2D{{0, 0}, {w, 0}, {w, h}, {0, h}}
						tol = 0
					}
					if v != "polygon" {
						pts = nil
					}
					ed.report(ed.SetImageImportDefaultCollision(relPath, v, tol, maxV, pts))
				},
			},
			resetBtn("import-image-collision-reset", ed.T("assetImport.clearCollision"), func() bool {
				_, over := readInfo().EffectiveImageCollision()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "collision"))
			}),
		),
		core.Show{
			When: func() bool {
				col, ok := readInfo().EffectiveImageCollision()
				return ok && col != nil && (col.Mode == "alpha" || col.Mode == "box" || col.Mode == "ellipse")
			},
			Then: func(cx *core.Scope) core.Node {
				return fieldRow(ed.T("assetImport.collisionTolerance"), func() string {
					return ed.T("assetImport.collisionToleranceTooltip")
				},
					ui.NumberInput{
						TestID:   "import-image-collision-tolerance",
						Label:    ed.T("assetImport.collisionToleranceLabel"),
						Min:      0,
						Max:      255,
						Step:     1,
						Disabled: disabled,
						Value: func() float64 {
							if col, ok := readInfo().EffectiveImageCollision(); ok && col != nil {
								return float64(col.Tolerance)
							}
							return 0
						},
						OnChange: func(v float64) {
							col, ok := readInfo().EffectiveImageCollision()
							if !ok || col == nil || col.Mode == "" {
								return
							}
							tol := min(max(roundInt(v, 0), 0), 255)
							ed.report(ed.SetImageImportDefaultCollision(relPath, col.Mode, tol, col.MaxVertices, col.Points))
						},
					},
					nil,
				)
			},
		},
		core.Show{
			When: func() bool {
				col, ok := readInfo().EffectiveImageCollision()
				return ok && col != nil && (col.Mode == "alpha" || col.Mode == "ellipse" || col.Mode == "polygon")
			},
			Then: func(cx *core.Scope) core.Node {
				return fieldRow(ed.T("assetImport.collisionMaxVerts"), func() string {
					return ed.T("assetImport.collisionMaxVertsTooltip")
				},
					ui.NumberInput{
						TestID:   "import-image-collision-max-vertices",
						Label:    ed.T("assetImport.collisionMaxVertsLabel"),
						Min:      importsettings.MinCollisionVertices,
						Max:      importsettings.MaxCollisionVertices,
						Step:     1,
						Disabled: disabled,
						Value: func() float64 {
							if col, ok := readInfo().EffectiveImageCollision(); ok && col != nil && col.MaxVertices > 0 {
								return float64(col.MaxVertices)
							}
							return importsettings.DefaultCollisionMaxVertices
						},
						OnChange: func(v float64) {
							col, ok := readInfo().EffectiveImageCollision()
							if !ok || col == nil || col.Mode == "" {
								return
							}
							mv := min(max(roundInt(v, importsettings.MinCollisionVertices), importsettings.MinCollisionVertices), importsettings.MaxCollisionVertices)
							ed.report(ed.SetImageImportDefaultCollision(relPath, col.Mode, col.Tolerance, mv, col.Points))
						},
					},
					nil,
				)
			},
		},
		core.Show{
			When: func() bool {
				_, _, active := readInfo().EffectiveCellImageCollision(activeCollisionCell())
				return active
			},
			Then: func(cx *core.Scope) core.Node {
				return core.Text{
					TestID: "import-image-collision-summary",
					ContentFunc: func() string {
						_ = rev.Get()
						cell := activeCollisionCell()
						polys, err := ed.PreviewImageCollisionPolygons(relPath, cell)
						if err != nil {
							return ed.T("assetImport.traceUnavailable")
						}
						if len(polys) == 0 {
							return ed.T("assetImport.noOpaquePixels")
						}
						verts := 0
						for _, p := range polys {
							verts += len(p)
						}
						if len(polys) == 1 {
							return ed.T("assetImport.collisionSummaryOne", l10n.Param("verts", verts))
						}
						return ed.T("assetImport.collisionSummaryMany", l10n.Param("count", len(polys)), l10n.Param("verts", verts))
					},
					Style: core.Style{FontSize: 10, Color: colors.Muted()},
				}
			},
		},
	}

	newPointName := core.NewSignal(cx, "")
	pointsListKey := core.NewMemo(cx, func() string {
		cell := activeCollisionCell()
		pts, over := readInfo().EffectiveCellImagePoints(cell)
		if len(pts) == 0 {
			return fmt.Sprintf("c=%d:", cell)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "c=%d:", cell)
		for _, name := range slices.Sorted(maps.Keys(pts)) {
			fmt.Fprintf(&b, "%s:%v,", name, over[name])
		}
		return b.String()
	})

	addNamedPoint := func() {
		name := strings.TrimSpace(newPointName.Get())
		if name == "" {
			return
		}
		info := readInfo()
		w, h := float64(info.ImageWidth), float64(info.ImageHeight)
		cell := activeCollisionCell()
		if s, ok := info.EffectiveImageSlice(); ok && s != nil && cell >= 0 {
			w, h = float64(s.Width), float64(s.Height)
		}
		pt := importsettings.Point2D{roundPointCoord(w * 0.5), roundPointCoord(h * 0.5)}
		if err := ed.SetImageImportPoint(relPath, name, &pt); err != nil {
			ed.report(err)
			return
		}
		newPointName.Set("")
	}

	pointsSection := []core.Node{
		core.Text{
			Content: ed.T("assetImport.imagePointsHeading"),
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		},
		fieldRow(ed.T("assetImport.addPoint"), func() string {
			return ed.T("assetImport.addPointTooltip")
		},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.Input{
								TestID:      "import-image-point-add-name",
								Label:       ed.T("assetImport.newPointNameLabel"),
								Placeholder: ed.T("assetImport.newPointNamePlaceholder"),
								Disabled:    disabled,
								Value:       func() string { return newPointName.Get() },
								OnChange:    func(v string) { newPointName.Set(v) },
								OnSubmit:    func(string) { addNamedPoint() },
							},
						},
					},
					ui.Button{
						TestID:   "import-image-point-add-btn",
						Text:     ed.T("assetImport.addPointBtn"),
						Label:    ed.T("assetImport.addPointBtnLabel"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress:  addNamedPoint,
					},
				},
			},
			resetBtn("import-image-points-reset", "Clear all image points", func() bool {
				pts, _ := readInfo().EffectiveCellImagePoints(activeCollisionCell())
				return len(pts) > 0
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "points"))
			}),
		),
		core.Dynamic(func(cx *core.Scope) core.Node {
			_ = pointsListKey.Get()
			cell := core.Untracked(cx, activeCollisionCell)
			info := core.Untracked(cx, readInfo)
			pts, over := info.EffectiveCellImagePoints(cell)
			if len(pts) == 0 {
				return nil
			}
			rows := make([]core.Node, 0, len(pts))
			for _, name := range slices.Sorted(maps.Keys(pts)) {
				ptName := name
				isCellOverride := over[ptName]
				label := ptName
				if isCellOverride {
					label += " *"
				}
				updatePoint := func(mutate func(p *importsettings.Point2D)) {
					c := activeCollisionCell()
					curPts, _ := readInfo().EffectiveCellImagePoints(c)
					p := curPts[ptName]
					mutate(&p)
					if c >= 0 {
						ed.report(ed.SetImageImportCellPoint(relPath, c, ptName, &p))
					} else {
						ed.report(ed.SetImageImportPoint(relPath, ptName, &p))
					}
				}
				rows = append(rows, fieldRow(label, func() string {
					return fmt.Sprintf("Frame pixel coordinates [x, y] for point %q", ptName)
				},
					ui.HStack{
						Gap: 4,
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
								Children: []core.Node{
									ui.NumberInput{
										TestID:   "import-image-point-" + ptName + "-x",
										Label:    ptName + " X",
										Step:     1,
										Format:   formatPivotNum,
										Disabled: disabled,
										Value: func() float64 {
											curPts, _ := readInfo().EffectiveCellImagePoints(activeCollisionCell())
											return curPts[ptName][0]
										},
										OnChange: func(v float64) {
											if math.IsNaN(v) || math.IsInf(v, 0) {
												return
											}
											updatePoint(func(p *importsettings.Point2D) { p[0] = roundPointCoord(v) })
										},
									},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
								Children: []core.Node{
									ui.NumberInput{
										TestID:   "import-image-point-" + ptName + "-y",
										Label:    ptName + " Y",
										Step:     1,
										Format:   formatPivotNum,
										Disabled: disabled,
										Value: func() float64 {
											curPts, _ := readInfo().EffectiveCellImagePoints(activeCollisionCell())
											return curPts[ptName][1]
										},
										OnChange: func(v float64) {
											if math.IsNaN(v) || math.IsInf(v, 0) {
												return
											}
											updatePoint(func(p *importsettings.Point2D) { p[1] = roundPointCoord(v) })
										},
									},
								},
							},
						},
					},
					resetBtn("import-image-point-"+ptName+"-remove", "Remove or reset point "+ptName, func() bool {
						return true
					}, func() {
						c := activeCollisionCell()
						_, curOver := readInfo().EffectiveCellImagePoints(c)
						if c >= 0 && curOver[ptName] {
							ed.report(ed.SetImageImportCellPoint(relPath, c, ptName, nil))
						} else {
							ed.report(ed.SetImageImportPoint(relPath, ptName, nil))
						}
					}),
				))
			}
			return core.View{
				Style:    core.Style{Gap: 4},
				Children: rows,
			}
		}),
	}

	nodes = append(nodes, pivotSection...)
	nodes = append(nodes, collisionSection...)
	return append(nodes, pointsSection...)
}

func audioImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	formatOpts := make([]ui.Option[string], len(project.AudioFormats))
	for i, f := range project.AudioFormats {
		formatOpts[i] = ui.Option[string]{Value: f, Label: f}
	}
	channelOpts := []ui.Option[int]{
		{Value: 0, Label: ed.T("assetImport.audio.channelSource")},
		{Value: 1, Label: ed.T("assetImport.audio.channelMono")},
		{Value: 2, Label: ed.T("assetImport.audio.channelStereo")},
	}
	normOpts := []ui.Option[string]{
		{Value: "none", Label: ed.T("assetImport.audio.normNone")},
		{Value: "peakDB", Label: ed.T("assetImport.audio.normPeak")},
		{Value: "rmsDB", Label: ed.T("assetImport.audio.normRMS")},
	}

	updateLoop := func(mutate func(l *importsettings.Loop)) {
		l, ok := readInfo().EffectiveAudioLoop()
		if !ok || l == nil {
			l = &importsettings.Loop{}
		}
		mutate(l)
		ed.report(ed.SetAudioImportLoop(relPath, l))
	}

	return []core.Node{
		fieldRow(ed.T("assetImport.audio.format"), func() string {
			return ed.T("assetImport.audio.formatTip", l10n.Param("default", readInfo().DefaultAudioFormat))
		},
			ui.Select[string]{
				TestID:   "import-audio-format",
				Label:    ed.T("assetImport.audio.formatLabel"),
				Options:  formatOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					v, _ := readInfo().EffectiveAudioFormat()
					return v
				},
				OnChange: func(v string) {
					ed.report(ed.SetAudioImportFormat(relPath, v))
				},
			},
			resetBtn("import-audio-format-reset", ed.T("assetImport.audio.formatReset"), func() bool {
				_, over := readInfo().EffectiveAudioFormat()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "format"))
			}),
		),
		fieldRow(ed.T("assetImport.audio.rate"), func() string {
			return ed.T("assetImport.audio.rateTip", l10n.Param("default", readInfo().DefaultAudioRate))
		},
			ui.NumberInput{
				TestID:   "import-audio-rate",
				Label:    ed.T("assetImport.audio.rateLabel"),
				Min:      0,
				Step:     100,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveAudioRate()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetAudioImportRate(relPath, roundInt(v, 0)))
				},
			},
			resetBtn("import-audio-rate-reset", ed.T("assetImport.audio.rateReset"), func() bool {
				_, over := readInfo().EffectiveAudioRate()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "rate"))
			}),
		),
		fieldRow(ed.T("assetImport.audio.channels"), func() string {
			return ed.T("assetImport.audio.channelsTip", l10n.Param("default", readInfo().DefaultAudioChannels))
		},
			ui.Select[int]{
				TestID:   "import-audio-channels",
				Label:    ed.T("assetImport.audio.channelsLabel"),
				Options:  channelOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() int {
					v, _ := readInfo().EffectiveAudioChannels()
					return v
				},
				OnChange: func(v int) {
					ed.report(ed.SetAudioImportChannels(relPath, v))
				},
			},
			resetBtn("import-audio-channels-reset", ed.T("assetImport.audio.channelsReset"), func() bool {
				_, over := readInfo().EffectiveAudioChannels()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "channels"))
			}),
		),
		fieldRow(ed.T("assetImport.audio.bitrate"), func() string {
			return ed.T("assetImport.audio.bitrateTip", l10n.Param("default", readInfo().DefaultAudioBitrate))
		},
			ui.NumberInput{
				TestID:   "import-audio-bitrate",
				Label:    ed.T("assetImport.audio.bitrateLabel"),
				Min:      0,
				Step:     16,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveAudioBitrate()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetAudioImportBitrate(relPath, roundInt(v, 0)))
				},
			},
			resetBtn("import-audio-bitrate-reset", ed.T("assetImport.audio.bitrateReset"), func() bool {
				_, over := readInfo().EffectiveAudioBitrate()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "bitrate"))
			}),
		),
		fieldRow(ed.T("assetImport.audio.normalize"), func() string { return ed.T("assetImport.audio.normalizeTip") },
			ui.Select[string]{
				TestID:   "import-audio-norm-mode",
				Label:    ed.T("assetImport.audio.normalizeLabel"),
				Options:  normOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					mode, _, _ := readInfo().EffectiveAudioNormalization()
					return mode
				},
				OnChange: func(mode string) {
					switch mode {
					case "peakDB":
						v := -1.0
						ed.report(ed.SetAudioImportPeakDB(relPath, &v))
					case "rmsDB":
						v := -14.0
						ed.report(ed.SetAudioImportRMSDB(relPath, &v))
					default:
						ed.report(ed.ResetAssetImportField(relPath, "normalization"))
					}
				},
			},
			resetBtn("import-audio-norm-reset", ed.T("assetImport.audio.normalizeReset"), func() bool {
				_, _, over := readInfo().EffectiveAudioNormalization()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "normalization"))
			}),
		),
		core.Show{
			When: func() bool {
				mode, _, _ := readInfo().EffectiveAudioNormalization()
				return mode == "peakDB" || mode == "rmsDB"
			},
			Then: func(cx *core.Scope) core.Node {
				return fieldRow(ed.T("assetImport.audio.levelDB"), nil, ui.NumberInput{
					TestID:   "import-audio-norm-db",
					Label:    ed.T("assetImport.audio.levelDBLabel"),
					Step:     0.5,
					Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
					Disabled: disabled,
					Value: func() float64 {
						_, db, _ := readInfo().EffectiveAudioNormalization()
						return db
					},
					OnChange: func(v float64) {
						mode, _, _ := readInfo().EffectiveAudioNormalization()
						if mode == "rmsDB" {
							ed.report(ed.SetAudioImportRMSDB(relPath, &v))
						} else {
							ed.report(ed.SetAudioImportPeakDB(relPath, &v))
						}
					},
				}, nil)
			},
		},
		fieldRow(ed.T("assetImport.audio.loop"), func() string { return ed.T("assetImport.audio.loopTip") },
			ui.Checkbox{
				TestID:   "import-audio-loop-enabled",
				Label:    ed.T("assetImport.audio.loopLabel"),
				Disabled: disabled,
				Checked: func() bool {
					_, over := readInfo().EffectiveAudioLoop()
					return over
				},
				OnChange: func(on bool) {
					if !on {
						ed.report(ed.SetAudioImportLoop(relPath, nil))
						return
					}
					ed.report(ed.SetAudioImportLoop(relPath, &importsettings.Loop{Start: 0, End: 0}))
				},
			},
			resetBtn("import-audio-loop-reset", ed.T("assetImport.audio.loopReset"), func() bool {
				_, over := readInfo().EffectiveAudioLoop()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "loop"))
			}),
		),
		core.Show{
			When: func() bool {
				_, over := readInfo().EffectiveAudioLoop()
				return over
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						fieldRow(ed.T("assetImport.audio.loopStart"), nil, ui.NumberInput{
							TestID:   "import-audio-loop-start",
							Label:    ed.T("assetImport.audio.loopStartLabel"),
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if l, ok := readInfo().EffectiveAudioLoop(); ok {
									return float64(l.Start)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateLoop(func(l *importsettings.Loop) { l.Start = roundInt(v, 0) })
							},
						}, nil),
						fieldRow(ed.T("assetImport.audio.loopEnd"), nil, ui.NumberInput{
							TestID:   "import-audio-loop-end",
							Label:    ed.T("assetImport.audio.loopEndLabel"),
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if l, ok := readInfo().EffectiveAudioLoop(); ok {
									return float64(l.End)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateLoop(func(l *importsettings.Loop) { l.End = roundInt(v, 0) })
							},
						}, nil),
					},
				}
			},
		},
	}
}

func fontImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	type textDraft struct {
		rev   uint64
		dirty bool
		text  string
	}
	charsDraft := core.NewSignal(cx, textDraft{})
	charsVal := func() string {
		_ = rev.Get()
		d := charsDraft.Get()
		if d.dirty && d.rev == ed.importRev {
			return d.text
		}
		v, _ := readInfo().EffectiveFontChars()
		return v
	}
	commitChars := func(text string) {
		info := readInfo()
		if text == info.DefaultFontChars && (info.Sidecar == nil || info.Sidecar.Font == nil || info.Sidecar.Font.Chars == "") {
			charsDraft.Set(textDraft{})
			return
		}
		charsDraft.Set(textDraft{})
		ed.report(ed.SetFontImportChars(relPath, text))
	}

	return []core.Node{
		fieldRow(ed.T("assetImport.font.size"), func() string {
			return ed.T("assetImport.font.sizeTip", l10n.Param("default", readInfo().DefaultFontSize))
		},
			ui.NumberInput{
				TestID:   "import-font-size",
				Label:    ed.T("assetImport.font.sizeLabel"),
				Min:      1,
				Max:      float64(project.MaxFontSize),
				Step:     1,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveFontSize()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetFontImportSize(relPath, roundInt(v, 1)))
				},
			},
			resetBtn("import-font-size-reset", ed.T("assetImport.font.sizeReset"), func() bool {
				_, over := readInfo().EffectiveFontSize()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "size"))
			}),
		),
		fieldRow(ed.T("assetImport.font.padding"), func() string {
			return ed.T("assetImport.font.paddingTip", l10n.Param("default", readInfo().DefaultFontPadding))
		},
			ui.NumberInput{
				TestID:   "import-font-padding",
				Label:    ed.T("assetImport.font.paddingLabel"),
				Min:      0,
				Step:     1,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveFontPadding()
					return float64(v)
				},
				OnChange: func(v float64) {
					p := roundInt(v, 0)
					ed.report(ed.SetFontImportPadding(relPath, &p))
				},
			},
			resetBtn("import-font-padding-reset", ed.T("assetImport.font.paddingReset"), func() bool {
				_, over := readInfo().EffectiveFontPadding()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "padding"))
			}),
		),
		fieldRow(ed.T("assetImport.font.chars"), func() string { return ed.T("assetImport.font.charsTip") },
			ui.Input{
				TestID:   "import-font-chars",
				Label:    ed.T("assetImport.font.charsLabel"),
				Disabled: disabled,
				Value:    charsVal,
				OnChange: func(v string) {
					charsDraft.Set(textDraft{rev: ed.importRev, dirty: true, text: v})
				},
				OnSubmit: commitChars,
				OnBlur: func() {
					d := charsDraft.Get()
					if d.dirty && d.rev == ed.importRev {
						commitChars(d.text)
					}
				},
			},
			resetBtn("import-font-chars-reset", ed.T("assetImport.font.charsReset"), func() bool {
				_, over := readInfo().EffectiveFontChars()
				return over
			}, func() {
				charsDraft.Set(textDraft{})
				ed.report(ed.ResetAssetImportField(relPath, "chars"))
			}),
		),
	}
}

func modelImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	nodes := modelImportPreviewSection(cx, ed, rev, relPath, disabled)
	return append(nodes,
		fieldRow(ed.T("assetImport.model.scale"), func() string {
			return ed.T("assetImport.model.scaleTip", l10n.Param("default", fmt.Sprintf("%g", readInfo().DefaultModelScale)))
		},
			ui.NumberInput{
				TestID:   "import-model-scale",
				Label:    ed.T("assetImport.model.scaleLabel"),
				Min:      0,
				Step:     0.1,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveModelScale()
					return v
				},
				OnChange: func(v float64) {
					ed.report(ed.SetModelImportScale(relPath, v))
				},
			},
			resetBtn("import-model-scale-reset", ed.T("assetImport.model.scaleReset"), func() bool {
				_, over := readInfo().EffectiveModelScale()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "scale"))
			}),
		),
		fieldRow(ed.T("assetImport.model.animations"), func() string {
			return ed.T("assetImport.model.animationsTip", l10n.Param("default", readInfo().DefaultModelAnimations))
		},
			ui.Checkbox{
				TestID:   "import-model-animations",
				Label:    ed.T("assetImport.model.animationsLabel"),
				Disabled: disabled,
				Checked: func() bool {
					v, _ := readInfo().EffectiveModelAnimations()
					return v
				},
				OnChange: func(on bool) {
					ed.report(ed.SetModelImportAnimations(relPath, &on))
				},
			},
			resetBtn("import-model-animations-reset", ed.T("assetImport.model.animationsReset"), func() bool {
				_, over := readInfo().EffectiveModelAnimations()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "animations"))
			}),
		),
		fieldRow(ed.T("assetImport.model.lodHysteresis"), func() string {
			return ed.T("assetImport.model.lodHysteresisTip")
		},
			ui.NumberInput{
				TestID:   "import-model-lod-hysteresis",
				Label:    ed.T("assetImport.model.lodHysteresisLabel"),
				Min:      0,
				Max:      0.5,
				Step:     0.01,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveModelLODHysteresis()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetModelImportLODHysteresis(relPath, float32(v)))
				},
			},
			resetBtn("import-model-lod-hysteresis-reset", ed.T("assetImport.model.lodHysteresisReset"), func() bool {
				_, over := readInfo().EffectiveModelLODHysteresis()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "lodHysteresis"))
			}),
		),
		fieldRow(ed.T("assetImport.model.lods"), func() string {
			return ed.T("assetImport.model.lodsTip")
		},
			core.Text{
				TestID: "import-model-lods",
				ContentFunc: func() string {
					lods, ok := readInfo().EffectiveModelLODs()
					if !ok {
						return ed.T("assetImport.model.lodsAuto")
					}
					parts := make([]string, len(lods))
					for i, lv := range lods {
						parts[i] = ed.T("assetImport.model.lodEntry", l10n.Param("index", i), l10n.Param("ratio", fmt.Sprintf("%.2g", lv.MinScreenRatio)))
					}
					return strings.Join(parts, ", ")
				},
				Style: core.Style{FontSize: 11, Color: ui.ColorsOf(cx).Muted()},
			},
			resetBtn("import-model-lods-reset", ed.T("assetImport.model.lodsReset"), func() bool {
				_, over := readInfo().EffectiveModelLODs()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "lods"))
			}),
		),
	)
}

func creditImportSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	colors ui.Colors,
) core.Node {
	type creditDraft struct {
		rev     uint64
		dirty   bool
		author  string
		license string
		source  string
		notice  string
	}
	draft := core.NewSignal(cx, creditDraft{})
	creditErr := core.NewSignal(cx, "")

	currentCredit := func() importsettings.Credit {
		_ = rev.Get()
		d := draft.Get()
		if d.dirty && d.rev == ed.importRev {
			return importsettings.Credit{
				Author:  d.author,
				License: d.license,
				Source:  d.source,
				Notice:  d.notice,
			}
		}
		if c := readInfo().EffectiveCredit(); c != nil {
			return *c
		}
		return importsettings.Credit{}
	}

	setField := func(mutate func(*importsettings.Credit)) {
		c := currentCredit()
		mutate(&c)
		draft.Set(creditDraft{
			rev:     ed.importRev,
			dirty:   true,
			author:  c.Author,
			license: c.License,
			source:  c.Source,
			notice:  c.Notice,
		})
	}

	commit := func() {
		c := currentCredit()
		c.Author = strings.TrimSpace(c.Author)
		c.License = strings.TrimSpace(c.License)
		c.Source = strings.TrimSpace(c.Source)
		c.Notice = strings.TrimSpace(c.Notice)

		if c.Author == "" && c.License == "" && c.Source == "" && c.Notice == "" {
			creditErr.Set("")
			draft.Set(creditDraft{})
			if readInfo().EffectiveCredit() != nil {
				ed.report(ed.SetAssetImportCredit(relPath, nil))
			}
			return
		}
		if c.Author == "" || c.License == "" {
			creditErr.Set(ed.T("assetImport.credit.authorLicenseRequired"))
			return
		}
		if err := ed.SetAssetImportCredit(relPath, &c); err != nil {
			creditErr.Set(err.Error())
			ed.report(err)
			return
		}
		creditErr.Set("")
		draft.Set(creditDraft{})
	}

	row := func(label string, ctrl core.Node) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						Width:    core.Pt(72),
						FontSize: 12,
						Color:    colors.Text(),
					},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{ctrl},
				},
			},
		}
	}

	return core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.Text{
						Content: ed.T("assetImport.section.credit"),
						Style: core.Style{
							FontSize:   11,
							FontWeight: 700,
							Color:      colors.Muted(),
							FlexGrow:   1,
						},
					},
					ui.IconButton{
						TestID: "import-credit-reset",
						Icon:   lucide.RotateCcw,
						Label:  ed.T("assetImport.credit.clear"),
						Disabled: func() bool {
							if disabled() {
								return true
							}
							d := draft.Get()
							hasDraft := d.dirty && d.rev == ed.importRev
							return !hasDraft && readInfo().EffectiveCredit() == nil
						},
						OnPress: func() {
							creditErr.Set("")
							draft.Set(creditDraft{})
							ed.report(ed.SetAssetImportCredit(relPath, nil))
						},
					},
				},
			},
			row(ed.T("assetImport.credit.author"), ui.Input{
				TestID:      "import-credit-author",
				Label:       ed.T("assetImport.credit.authorLabel"),
				Placeholder: ed.T("assetImport.credit.author"),
				Disabled:    disabled,
				Value:       func() string { return currentCredit().Author },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.Author = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			row(ed.T("assetImport.credit.license"), ui.Input{
				TestID:      "import-credit-license",
				Label:       ed.T("assetImport.credit.licenseLabel"),
				Placeholder: ed.T("assetImport.credit.licensePlaceholder"),
				Disabled:    disabled,
				Value:       func() string { return currentCredit().License },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.License = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			row(ed.T("assetImport.credit.source"), ui.Input{
				TestID:      "import-credit-source",
				Label:       ed.T("assetImport.credit.sourceLabel"),
				Placeholder: ed.T("assetImport.credit.sourcePlaceholder"),
				Disabled:    disabled,
				Value:       func() string { return currentCredit().Source },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.Source = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			row(ed.T("assetImport.credit.notice"), ui.Input{
				TestID:      "import-credit-notice",
				Label:       ed.T("assetImport.credit.noticeLabel"),
				Placeholder: ed.T("assetImport.credit.noticePlaceholder"),
				Disabled:    disabled,
				Value:       func() string { return currentCredit().Notice },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.Notice = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			core.Show{
				When: func() bool { return creditErr.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID:      "import-credit-error",
						ContentFunc: creditErr.Get,
						Style:       core.Style{FontSize: 11, Color: colors.Danger()},
					}
				},
			},
		},
	}
}

func localizedImportKind(ed *Editor, kind AssetImportKind) string {
	switch kind {
	case AssetImportImage:
		return ed.T("assetImport.kind.image")
	case AssetImportAudio:
		return ed.T("assetImport.kind.audio")
	case AssetImportFont:
		return ed.T("assetImport.kind.font")
	case AssetImportModel:
		return ed.T("assetImport.kind.model")
	default:
		return string(kind)
	}
}

// roundInt converts a finite float64 from a NumberInput into an int clamped to [minVal, math.MaxInt32], returning minVal for non-finite inputs.
func roundInt(v float64, minVal int) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return minVal
	}
	r := math.Round(v)
	if r <= float64(minVal) {
		return minVal
	}
	if r >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int(r)
}
