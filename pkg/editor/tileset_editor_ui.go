package editor

import (
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

func tilesetDirLabel(ed *Editor, dir string) string {
	switch dir {
	case "N":
		return ed.T("tilesetEditor.dir.n")
	case "NE":
		return ed.T("tilesetEditor.dir.ne")
	case "E":
		return ed.T("tilesetEditor.dir.e")
	case "SE":
		return ed.T("tilesetEditor.dir.se")
	case "S":
		return ed.T("tilesetEditor.dir.s")
	case "SW":
		return ed.T("tilesetEditor.dir.sw")
	case "W":
		return ed.T("tilesetEditor.dir.w")
	case "NW":
		return ed.T("tilesetEditor.dir.nw")
	default:
		return dir
	}
}

func tilesetShapeKindLabel(ed *Editor, kind string) string {
	switch kind {
	case "rect":
		return ed.T("tilesetEditor.shapeKind.rect")
	case "ellipse":
		return ed.T("tilesetEditor.shapeKind.ellipse")
	case "polygon":
		return ed.T("tilesetEditor.shapeKind.polygon")
	case "polyline":
		return ed.T("tilesetEditor.shapeKind.polyline")
	default:
		return kind
	}
}

// TilesetEditorPanelView renders the docked Tileset Editor below the viewport while a YAML tileset (*.tileset.yaml) is open:
// terrain set management, a visual tile grid with collision/shape/animation/terrain indicators,
// and tabbed per-tile editors for Terrain masks, Collision flags and shapes (drawn over the tile), Animation (with live preview), and Custom Data.
func TilesetEditorPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.TilesetOpen()
		},
		Then: func(cx *core.Scope) core.Node { return tilesetEditorPanel(cx, ed, rev) },
	}
}

func tilesetEditorPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	tileAnimRev := core.NewSignal(cx, ed.TileAnimRevision())
	unsubTileAnim := ed.SubscribeTileAnim(func() {
		tileAnimRev.Set(ed.TileAnimRevision())
	})
	cx.Cleanup(unsubTileAnim)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	newName := core.NewSignal(cx, "")
	newMode := core.NewSignal(cx, "blob")
	animTileDraft := core.NewSignal(cx, "0")
	animDurDraft := core.NewSignal(cx, "150ms")
	propKeyDraft := core.NewSignal(cx, "")
	propTypeDraft := core.NewSignal(cx, "string")
	propValDraft := core.NewSignal(cx, "")
	flagsDraft := core.NewSignal(cx, "")
	nameDraft := core.NewSignal(cx, "")
	weightDraft := core.NewSignal(cx, "")

	// Sync tile-scoped input drafts when selected tile or revision changes.
	syncedTileKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.TilesetOpen() {
			return ""
		}
		sel := ed.TilesetSelectedTile()
		info, _ := ed.TilesetTileInfo(sel)
		wStr := ""
		if info.Weight != nil {
			wStr = strconv.FormatFloat(*info.Weight, 'g', -1, 64)
		}
		return fmt.Sprintf("%d|%s|%s|%d", sel, info.Name, wStr, info.Flags)
	})
	cx.Effect(func() {
		_ = syncedTileKey.Get()
		cx.Untrack(func() {
			if !ed.TilesetOpen() {
				return
			}
			sel := ed.TilesetSelectedTile()
			info, _ := ed.TilesetTileInfo(sel)
			nameDraft.Set(info.Name)
			if info.Weight != nil {
				weightDraft.Set(strconv.FormatFloat(*info.Weight, 'g', -1, 64))
			} else {
				weightDraft.Set("")
			}
			flagsDraft.Set(strconv.FormatUint(uint64(info.Flags), 10))
			animTileDraft.Set(strconv.Itoa(max(sel, 0)))
		})
	})

	tabBtn := func(testID, text string, tab TilesetEditorTab) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     text,
			Disabled: disabled,
			On: func() bool {
				_ = rev.Get()
				return ed.TilesetTab() == tab
			},
			OnChange: func(on bool) {
				if on {
					ed.SetTilesetTab(tab)
				}
			},
		}
	}

	return core.Dynamic(func(cx *core.Scope) core.Node {
		p := ed.Palette(cx)

		header := core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Wrap: core.Wrap, Gap: 8},
			Children: []core.Node{
				core.Text{
					Content: ed.T("tilesetEditor.title"),
					Style:   core.Style{FontSize: 13, FontWeight: 600, Color: p.TextPrimary},
				},
				core.Text{
					TestID: "tileset-editor-path",
					ContentFunc: func() string {
						_ = rev.Get()
						tp := ed.TilesetPath()
						if tp == "" {
							tp = ed.T("tilesetEditor.untitled")
						} else {
							tp = filepath.Base(tp)
						}
						if ed.TilesetDirty() {
							tp += "*"
						}
						return tp
					},
					Style: core.Style{FontSize: 12, Color: p.TextFaint},
				},
				core.View{
					Style: core.Style{
						Direction:    core.Row,
						Gap:          4,
						Padding:      core.All(2),
						CornerRadius: 6,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						tabBtn("tileset-tab-terrain", ed.T("tilesetEditor.tab.terrain"), TilesetTabTerrain),
						tabBtn("tileset-tab-collision", ed.T("tilesetEditor.tab.collision"), TilesetTabCollision),
						tabBtn("tileset-tab-animation", ed.T("tilesetEditor.tab.animation"), TilesetTabAnimation),
						tabBtn("tileset-tab-data", ed.T("tilesetEditor.tab.data"), TilesetTabData),
					},
				},
				core.View{Style: core.Style{FlexGrow: 1}},
				ui.Button{
					TestID:   "tileset-editor-save",
					Text:     ed.T("tilesetEditor.save"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonOutline,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.SaveTileset())
					},
				},
				ui.Button{
					TestID:   "tileset-editor-close",
					Text:     ed.T("tilesetEditor.close"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.CloseTileset())
					},
				},
			},
		}

		// Left column: terrain sets list, add form, mode & default controls.
		terrainCol := ui.ScrollView{
			Style: core.Style{Width: core.Pt(228)},
			Children: []core.Node{core.View{
				TestID: "tileset-terrains",
				Style:  core.Style{Gap: 6},
				Children: []core.Node{
					core.Text{
						Content: ed.T("tilesetEditor.terrainsHeading"),
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = rev.Get()
						curPal := ed.Palette(cx)
						if !ed.TilesetOpen() {
							return nil
						}
						doc := ed.tileset.doc
						sel := ed.tileset.selectedTerrain
						if len(doc.Terrains) == 0 {
							return core.Text{
								TestID:  "tileset-terrains-empty",
								Content: ed.T("tilesetEditor.noTerrains"),
								Style:   core.Style{FontSize: 11, Color: curPal.TextFaint},
							}
						}
						var btns []core.Node
						for _, td := range doc.Terrains {
							name := td.Name
							modeLabel := ed.T("tilesetEditor.mode4Bit")
							if td.Mode == "blob" {
								modeLabel = ed.T("tilesetEditor.mode8Bit")
							}
							btns = append(btns, ui.ToggleButton{
								TestID:   "tileset-terrain-" + name,
								Text:     ed.T("tilesetEditor.terrainItem", l10n.Param("name", name), l10n.Param("mode", modeLabel)),
								Disabled: disabled,
								On:       func() bool { return sel == name },
								OnChange: func(on bool) {
									if on && !ed.Playing() {
										ed.SetTilesetSelectedTerrain(name)
									}
								},
							})
						}
						return core.View{
							Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
							Children: btns,
						}
					}),
					ui.HStack{
						Gap: 4,
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1},
								Children: []core.Node{ui.Input{
									TestID:      "tileset-new-terrain-name",
									Label:       ed.T("tilesetEditor.newTerrainLabel"),
									Placeholder: ed.T("tilesetEditor.newTerrainPlaceholder"),
									Disabled:    disabled,
									Value:       newName.Get,
									OnChange:    newName.Set,
								}},
							},
							ui.Button{
								TestID:  "tileset-add-terrain",
								Text:    ed.T("tilesetEditor.addTerrain"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									return disabled() || strings.TrimSpace(newName.Get()) == ""
								},
								OnPress: func() {
									name := strings.TrimSpace(newName.Get())
									if err := ed.AddTilesetTerrain(name, newMode.Get()); err != nil {
										ed.report(err)
										return
									}
									newName.Set("")
								},
							},
						},
					},
					ui.HStack{
						Gap: 4,
						Children: []core.Node{
							ui.ToggleButton{
								TestID:   "tileset-new-terrain-mode-cardinal",
								Text:     ed.T("tilesetEditor.mode4Bit"),
								Disabled: disabled,
								On:       func() bool { return newMode.Get() == "cardinal" },
								OnChange: func(on bool) {
									if on {
										newMode.Set("cardinal")
									}
								},
							},
							ui.ToggleButton{
								TestID:   "tileset-new-terrain-mode-blob",
								Text:     ed.T("tilesetEditor.mode8Bit"),
								Disabled: disabled,
								On:       func() bool { return newMode.Get() == "blob" },
								OnChange: func(on bool) {
									if on {
										newMode.Set("blob")
									}
								},
							},
						},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = rev.Get()
						curPal := ed.Palette(cx)
						if !ed.TilesetOpen() || ed.tileset.selectedTerrain == "" {
							return nil
						}
						sel := ed.tileset.selectedTerrain
						_, td := terrainDocByName(ed.tileset.doc, sel)
						if td == nil {
							return nil
						}
						defText := ed.T("tilesetEditor.defaultMaskZero")
						if td.Default != nil {
							defText = ed.T("tilesetEditor.defaultTile", l10n.Param("tile", *td.Default))
						}
						return core.View{
							Style: core.Style{Gap: 4},
							Children: []core.Node{
								ui.HStack{
									Gap: 4,
									Children: []core.Node{
										ui.ToggleButton{
											TestID:   "tileset-mode-cardinal",
											Text:     ed.T("tilesetEditor.modeCardinal"),
											Disabled: disabled,
											On:       func() bool { return td.Mode == "cardinal" },
											OnChange: func(on bool) {
												if on {
													ed.report(ed.SetTilesetTerrainMode(sel, "cardinal"))
												}
											},
										},
										ui.ToggleButton{
											TestID:   "tileset-mode-blob",
											Text:     ed.T("tilesetEditor.modeBlob"),
											Disabled: disabled,
											On:       func() bool { return td.Mode == "blob" },
											OnChange: func(on bool) {
												if on {
													ed.report(ed.SetTilesetTerrainMode(sel, "blob"))
												}
											},
										},
									},
								},
								core.Text{
									TestID:  "tileset-default-info",
									Content: defText,
									Style:   core.Style{FontSize: 11, Color: curPal.TextFaint},
								},
								ui.HStack{
									Gap: 4,
									Children: []core.Node{
										ui.Button{
											TestID:   "tileset-set-default",
											Text:     ed.T("tilesetEditor.setDefault"),
											Size:     ui.SizeSmall,
											Variant:  ui.ButtonSecondary,
											Disabled: disabled,
											OnPress: func() {
												id := ed.TilesetSelectedTile()
												if id >= 0 {
													ed.report(ed.SetTilesetTerrainDefault(sel, &id))
												}
											},
										},
										ui.Button{
											TestID:  "tileset-clear-default",
											Text:    ed.T("tilesetEditor.clear"),
											Size:    ui.SizeSmall,
											Variant: ui.ButtonGhost,
											Disabled: func() bool {
												return disabled() || td.Default == nil
											},
											OnPress: func() {
												ed.report(ed.SetTilesetTerrainDefault(sel, nil))
											},
										},
										ui.Button{
											TestID:   "tileset-delete-terrain",
											Text:     ed.T("tilesetEditor.delete"),
											Size:     ui.SizeSmall,
											Variant:  ui.ButtonDanger,
											Disabled: disabled,
											OnPress: func() {
												ed.report(ed.RemoveTilesetTerrain(sel))
											},
										},
									},
								},
							},
						}
					}),
				},
			}},
		}

		// Middle column: atlas/image tile picker grid with collision, shape, animation, and terrain overlays.
		tileGridCol := ui.ScrollView{
			Style: core.Style{FlexGrow: 1},
			Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				curPal := ed.Palette(cx)
				if !ed.TilesetOpen() {
					return nil
				}
				doc := ed.tileset.doc
				selTile := ed.tileset.selectedTile
				selTerrain := ed.tileset.selectedTerrain
				_, td := terrainDocByName(doc, selTerrain)
				count := min(doc.Count(), maxPaletteTiles)
				items := make([]core.Node, 0, count)
				for id := range count {
					tileID := id
					mask, assigned := tileDocMask(doc, tileID, selTerrain)
					isDefault := td != nil && ((td.Default != nil && *td.Default == tileID) || (td.Default == nil && assigned && mask == 0))
					tileMeta := findTileDoc(doc, tileID)
					img, frame, hasVis := ed.tilesetTileVisual(tileID)
					border := curPal.LineRaised
					if tileID == selTile {
						border = curPal.Accent.Fg
					}
					items = append(items, core.View{
						TestID: fmt.Sprintf("tileset-tile-%d", tileID),
						Label:  ed.T("tilesetEditor.tileLabel", l10n.Param("id", tileID)),
						Style: core.Style{
							Padding:      core.All(2),
							BorderWidth:  2,
							BorderColor:  border,
							CornerRadius: 4,
							Background:   curPal.Raised,
							FocusVisible: curPal.FocusRing(2),
						},
						OnPress: func(core.PressEvent) {
							if !ed.Playing() {
								ed.SetTilesetSelectedTile(tileID)
							}
						},
						Children: []core.Node{
							core.Canvas{
								TestID: fmt.Sprintf("tileset-tile-thumb-%d", tileID),
								Style: core.Style{
									Width:      core.Pt(32),
									Height:     core.Pt(32),
									Background: curPal.Bg,
								},
								Draw: func(d *core.Drawing, size core.Size) {
									drawPal := ed.Palette(cx)
									w, h := float32(size.Width), float32(size.Height)
									if hasVis {
										drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
									}
									if tileMeta != nil {
										if tileMeta.Solid {
											d.SetStrokeColor(drawPal.Accent.Fg).SetLineWidth(1.5).BeginPath().Rect(1, 1, w-2, h-2).Stroke()
										} else if tileMeta.OneWay {
											d.SetFillColor(drawPal.Changed.Fg).FillRect(0, 0, w, 3)
										}
										if len(tileMeta.Shapes) > 0 {
											drawTileShapesOverlay(d, drawPal, doc.TileWidth, doc.TileHeight, w, h, tileMeta.Shapes, -1, nil)
										}
										if len(tileMeta.Animation) > 0 {
											d.SetFillColor(drawPal.Agent.Fg).FillRect(1, h-6, 5, 5)
										}
									}
									if assigned {
										drawTileMaskDots(d, drawPal, w, h, mask)
									}
									if isDefault {
										d.SetFillColor(drawPal.Warning.Fg).FillRect(w-7, 1, 6, 6)
									}
								},
							},
							core.Text{
								Content: fmt.Sprintf("%d", tileID),
								Style:   core.Style{FontSize: 10, Color: curPal.TextFaint},
							},
						},
					})
				}
				return core.View{
					TestID:   "tileset-tile-grid",
					Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
					Children: items,
				}
			})},
		}

		// Right column: tabbed inspector for the selected tile (Terrain, Collision, Animation, Data).
		rightCol := ui.ScrollView{
			Style: core.Style{Width: core.Pt(328)},
			Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				curPal := ed.Palette(cx)
				if !ed.TilesetOpen() {
					return nil
				}
				switch ed.TilesetTab() {
				case TilesetTabCollision:
					return tilesetCollisionTab(cx, ed, curPal, disabled, flagsDraft)
				case TilesetTabAnimation:
					return tilesetAnimationTab(ed, curPal, disabled, tileAnimRev, animTileDraft, animDurDraft)
				case TilesetTabData:
					return tilesetDataTab(ed, curPal, disabled, nameDraft, weightDraft, propKeyDraft, propTypeDraft, propValDraft)
				default:
					return tilesetTerrainMaskTab(cx, ed, curPal, disabled)
				}
			})},
		}

		return core.View{
			TestID: "tileset-editor-panel",
			Style: dockedStyle(cx, core.Style{
				Height:      core.Pt(270),
				Padding:     core.Symmetric(8, 12),
				Gap:         8,
				Background:  p.Panel,
				Borders:     core.Edges{Top: 1},
				BorderColor: p.Line,
			}),
			Children: []core.Node{
				header,
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.TilesetError() != nil
					},
					Then: func(cx *core.Scope) core.Node {
						curPal := ed.Palette(cx)
						return core.Text{
							TestID: "tileset-editor-error",
							ContentFunc: func() string {
								_ = rev.Get()
								if err := ed.TilesetError(); err != nil {
									return err.Error()
								}
								return ""
							},
							Style: core.Style{FontSize: 11, Color: curPal.Removed.Fg},
						}
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Gap:       12,
					},
					Children: []core.Node{terrainCol, tileGridCol, rightCol},
				},
			},
		}
	})
}

func tilesetTerrainMaskTab(cx *core.Scope, ed *Editor, p theme.Palette, disabled func() bool) core.Node {
	st := ed.tileset
	selTile := st.selectedTile
	selTerrain := st.selectedTerrain
	if selTerrain == "" {
		return core.View{
			TestID: "tileset-mask-editor",
			Style:  core.Style{Gap: 6},
			Children: []core.Node{
				core.Text{
					Content: ed.T("tilesetEditor.maskEmptyHint"),
					Style:   core.Style{FontSize: 11, Color: p.TextFaint},
				},
			},
		}
	}
	_, td := terrainDocByName(st.doc, selTerrain)
	isBlob := td != nil && td.Mode == "blob"
	_, assigned := tileDocMask(st.doc, selTile, selTerrain)
	mask := st.editMask
	conflict := st.conflictTile

	summary := ed.T("tilesetEditor.maskUnassigned")
	if assigned || conflict >= 0 {
		names := tilemap.TerrainMaskNeighbors(mask)
		if len(names) == 0 {
			summary = ed.T("tilesetEditor.maskIsolated")
		} else {
			locNames := make([]string, len(names))
			for i, n := range names {
				locNames[i] = tilesetDirLabel(ed, n)
			}
			summary = strings.Join(locNames, ed.T("tilesetEditor.listSep"))
		}
	}

	bitBtn := func(testID, dir string, bit uint8, corner bool) core.Node {
		active := mask&bit != 0
		label := tilesetDirLabel(ed, dir)
		return ui.ToggleButton{
			TestID: testID,
			Text:   label,
			Label:  ed.T("tilesetEditor.toggleNeighbor", l10n.Param("dir", label)),
			Disabled: func() bool {
				return disabled() || (corner && !isBlob)
			},
			On: func() bool { return active },
			OnChange: func(bool) {
				ed.report(ed.ToggleTilesetEdgeOrCorner(bit))
			},
		}
	}

	img, frame, hasVis := ed.tilesetTileVisual(selTile)
	centerBtn := ui.ToggleButton{
		TestID:   "tileset-mask-center",
		Text:     ed.T("tilesetEditor.dir.c"),
		Label:    ed.T("tilesetEditor.toggleCenter"),
		Disabled: disabled,
		On:       func() bool { return assigned },
		OnChange: func(bool) {
			ed.report(ed.ToggleTilesetTileCenter())
		},
	}

	children := []core.Node{
		ui.HStack{
			Gap: 6,
			Children: []core.Node{
				core.Canvas{
					TestID: "tileset-mask-preview",
					Style: core.Style{
						Width:      core.Pt(32),
						Height:     core.Pt(32),
						Background: p.Bg,
					},
					Draw: func(d *core.Drawing, size core.Size) {
						w, h := float32(size.Width), float32(size.Height)
						if hasVis {
							drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
						}
						if assigned || conflict >= 0 {
							drawTileMaskDots(d, ed.Palette(cx), w, h, mask)
						}
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, Gap: 2},
					Children: []core.Node{
						core.Text{
							TestID:  "tileset-mask-title",
							Content: ed.T("tilesetEditor.maskTitle", l10n.Param("id", selTile), l10n.Param("terrain", selTerrain)),
							Style:   core.Style{FontSize: 12, FontWeight: 600, Color: p.TextPrimary},
						},
						core.Text{
							TestID:  "tileset-mask-summary",
							Content: summary,
							Style:   core.Style{FontSize: 11, Color: p.TextFaint},
						},
					},
				},
			},
		},
		core.View{
			Style: core.Style{Gap: 4},
			Children: []core.Node{
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						bitBtn("tileset-mask-nw", "NW", tilemap.MaskNorthWest, true),
						bitBtn("tileset-mask-n", "N", tilemap.MaskNorth, false),
						bitBtn("tileset-mask-ne", "NE", tilemap.MaskNorthEast, true),
					},
				},
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						bitBtn("tileset-mask-w", "W", tilemap.MaskWest, false),
						centerBtn,
						bitBtn("tileset-mask-e", "E", tilemap.MaskEast, false),
					},
				},
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						bitBtn("tileset-mask-sw", "SW", tilemap.MaskSouthWest, true),
						bitBtn("tileset-mask-s", "S", tilemap.MaskSouth, false),
						bitBtn("tileset-mask-se", "SE", tilemap.MaskSouthEast, true),
					},
				},
			},
		},
	}

	if conflict >= 0 {
		children = append(children, ui.HStack{
			Gap: 6,
			Children: []core.Node{
				core.Text{
					TestID:  "tileset-mask-conflict",
					Content: ed.T("tilesetEditor.maskConflict", l10n.Param("tile", conflict)),
					Style:   core.Style{FontSize: 11, Color: p.Removed.Fg, FlexGrow: 1},
				},
				ui.Button{
					TestID:   "tileset-mask-reassign",
					Text:     ed.T("tilesetEditor.reassign"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.ReassignTilesetEditMask())
					},
				},
			},
		})
	}

	children = append(children, ui.Button{
		TestID:  "tileset-tile-clear-terrain",
		Text:    ed.T("tilesetEditor.clearTile"),
		Size:    ui.SizeSmall,
		Variant: ui.ButtonGhost,
		Disabled: func() bool {
			return disabled() || !assigned
		},
		OnPress: func() {
			ed.report(ed.ClearTilesetTileTerrain(selTile))
		},
	})

	return core.View{TestID: "tileset-mask-editor", Style: core.Style{Gap: 6}, Children: children}
}

func tilesetCollisionTab(cx *core.Scope, ed *Editor, p theme.Palette, disabled func() bool, flagsDraft *core.Signal[string]) core.Node {
	st := ed.tileset
	doc := st.doc
	selTile := st.selectedTile
	info, _ := ed.TilesetTileInfo(selTile)
	img, frame, hasVis := ed.tilesetTileVisual(selTile)
	selShape := ed.TilesetSelectedShape()
	tool := ed.TilesetShapeTool()
	draftPoly := ed.TilesetDraftPolygon()

	collMode := "none"
	if info.Solid {
		collMode = "solid"
	} else if info.OneWay {
		collMode = "one-way"
	}

	collBtn := func(testID, label, mode string) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: disabled,
			On:       func() bool { return collMode == mode },
			OnChange: func(on bool) {
				if on {
					ed.report(ed.SetTilesetTileCollision(selTile, mode))
				}
			},
		}
	}

	var bitBtns []core.Node
	for bit := uint(2); bit <= 7; bit++ {
		b := bit
		on := info.Flags&tilemap.TileFlags(1<<b) != 0
		bitBtns = append(bitBtns, ui.ToggleButton{
			TestID:   fmt.Sprintf("tileset-flag-bit-%d", b),
			Text:     ed.T("tilesetEditor.flagBitShort", l10n.Param("bit", b)),
			Label:    ed.T("tilesetEditor.flagBitLabel", l10n.Param("bit", b)),
			Disabled: disabled,
			On:       func() bool { return on },
			OnChange: func(bool) {
				ed.report(ed.ToggleTilesetTileFlagBit(selTile, b))
			},
		})
	}

	const canvasSize float32 = 88
	tw, th := float32(max(doc.TileWidth, 1)), float32(max(doc.TileHeight, 1))

	shapeCanvas := core.View{
		TestID: "tileset-shape-canvas",
		Label:  ed.T("tilesetEditor.shapeCanvasLabel"),
		Style: core.Style{
			Width:        core.Pt(core.Points(canvasSize)),
			Height:       core.Pt(core.Points(canvasSize)),
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			CornerRadius: 4,
			Background:   p.Bg,
		},
		OnPress: func(ev core.PressEvent) {
			if ed.Playing() {
				return
			}
			tx := float32(math.Round(float64(ev.Position.X) / float64(canvasSize) * float64(tw)))
			ty := float32(math.Round(float64(ev.Position.Y) / float64(canvasSize) * float64(th)))
			tx = max(0, min(tw, tx))
			ty = max(0, min(th, ty))
			if ed.TilesetShapeTool() == TilesetShapeToolPolygon {
				ed.AddTilesetDraftPolygonPoint(tx, ty)
				return
			}
			// Rect tool: add a rect centered around click or half-tile box clamped inside tile.
			rw := max(float32(1), float32(math.Round(float64(tw)/2)))
			rh := max(float32(1), float32(math.Round(float64(th)/2)))
			rx := max(0, min(tw-rw, float32(math.Round(float64(tx-rw/2)))))
			ry := max(0, min(th-rh, float32(math.Round(float64(ty-rh/2)))))
			ed.report(ed.AddTilesetTileShape(selTile, tilemap.TileShapeDoc{
				Kind:   "rect",
				X:      rx,
				Y:      ry,
				Width:  rw,
				Height: rh,
			}))
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "tileset-shape-canvas-draw",
				Style:  core.Style{Width: core.Pt(core.Points(canvasSize)), Height: core.Pt(core.Points(canvasSize))},
				Draw: func(d *core.Drawing, size core.Size) {
					drawPal := ed.Palette(cx)
					w, h := float32(size.Width), float32(size.Height)
					if hasVis {
						drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
					}
					// Subtle midpoint crosshair guides.
					d.SetStrokeColor(drawPal.LineRaised).SetLineWidth(1)
					d.BeginPath().MoveTo(w/2, 0).LineTo(w/2, h).MoveTo(0, h/2).LineTo(w, h/2).Stroke()
					drawTileShapesOverlay(d, drawPal, doc.TileWidth, doc.TileHeight, w, h, info.Shapes, selShape, draftPoly)
				},
			},
		},
	}

	shapeControls := []core.Node{
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				ui.ToggleButton{
					TestID:   "tileset-shape-tool-rect",
					Text:     ed.T("tilesetEditor.shapeToolRect"),
					Disabled: disabled,
					On:       func() bool { return tool == TilesetShapeToolRect },
					OnChange: func(on bool) {
						if on {
							ed.SetTilesetShapeTool(TilesetShapeToolRect)
						}
					},
				},
				ui.ToggleButton{
					TestID:   "tileset-shape-tool-polygon",
					Text:     ed.T("tilesetEditor.shapeToolPolygon"),
					Disabled: disabled,
					On:       func() bool { return tool == TilesetShapeToolPolygon },
					OnChange: func(on bool) {
						if on {
							ed.SetTilesetShapeTool(TilesetShapeToolPolygon)
						}
					},
				},
			},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				ui.Button{
					TestID:   "tileset-shape-add-rect",
					Text:     ed.T("tilesetEditor.shapeAddFullRect"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.AddTilesetTileShape(selTile, tilemap.TileShapeDoc{
							Kind:   "rect",
							Width:  tw,
							Height: th,
						}))
					},
				},
				ui.Button{
					TestID:   "tileset-shape-add-slope",
					Text:     ed.T("tilesetEditor.shapeAddSlope"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.AddTilesetTileShape(selTile, tilemap.TileShapeDoc{
							Kind:   "polygon",
							Points: [][]float32{{0, th}, {tw, 0}, {tw, th}},
						}))
					},
				},
			},
		},
	}
	if len(draftPoly) > 0 {
		shapeControls = append(shapeControls, ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.Text{
					TestID:  "tileset-shape-draft-count",
					Content: ed.T("tilesetEditor.shapeDraftPts", l10n.Param("count", len(draftPoly))),
					Style:   core.Style{FontSize: 11, Color: p.Changed.Fg},
				},
				ui.Button{
					TestID:  "tileset-shape-finish-poly",
					Text:    ed.T("tilesetEditor.finish"),
					Size:    ui.SizeSmall,
					Variant: ui.ButtonPrimary,
					Disabled: func() bool {
						return disabled() || len(draftPoly) < 3
					},
					OnPress: func() {
						ed.report(ed.CommitTilesetDraftPolygon())
					},
				},
				ui.Button{
					TestID:   "tileset-shape-cancel-poly",
					Text:     ed.T("tilesetEditor.cancel"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress:  ed.ClearTilesetDraftPolygon,
				},
			},
		})
	}

	var shapeRows []core.Node
	for i, s := range info.Shapes {
		idx := i
		kindLabel := tilesetShapeKindLabel(ed, s.Kind)
		var desc string
		if s.Kind == "rect" || s.Kind == "ellipse" {
			desc = ed.T("tilesetEditor.shapeDescBox",
				l10n.Param("kind", kindLabel),
				l10n.Param("x", fmt.Sprintf("%.0f", s.X)),
				l10n.Param("y", fmt.Sprintf("%.0f", s.Y)),
				l10n.Param("w", fmt.Sprintf("%.0f", s.Width)),
				l10n.Param("h", fmt.Sprintf("%.0f", s.Height)),
			)
		} else {
			desc = ed.T("tilesetEditor.shapeDescPoly",
				l10n.Param("kind", kindLabel),
				l10n.Param("count", len(s.Points)),
			)
		}
		rowBg := p.Raised
		if idx == selShape {
			rowBg = ui.Alpha(p.Accent.Fg, 0.18)
		}
		shapeRows = append(shapeRows, core.View{
			TestID: fmt.Sprintf("tileset-shape-row-%d", idx),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          4,
				Padding:      core.Symmetric(2, 6),
				CornerRadius: 4,
				Background:   rowBg,
			},
			OnPress: func(core.PressEvent) {
				ed.SetTilesetSelectedShape(idx)
			},
			Children: []core.Node{
				core.Text{
					Content: desc,
					Style:   core.Style{FontSize: 11, Color: p.TextSecondary, FlexGrow: 1},
				},
				ui.Button{
					TestID:   fmt.Sprintf("tileset-shape-remove-%d", idx),
					Text:     "×",
					Label:    ed.T("tilesetEditor.shapeRemoveLabel", l10n.Param("index", idx)),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.RemoveTilesetTileShape(selTile, idx))
					},
				},
			},
		})
	}

	children := []core.Node{
		core.Text{
			Content: ed.T("tilesetEditor.collisionHeading"),
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				collBtn("tileset-coll-solid", ed.T("tilesetEditor.collSolid"), "solid"),
				collBtn("tileset-coll-oneway", ed.T("tilesetEditor.collOneWay"), "one-way"),
				collBtn("tileset-coll-none", ed.T("tilesetEditor.collNone"), "none"),
			},
		},
		core.View{
			Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4, AlignItems: core.AlignCenter},
			Children: append(bitBtns, core.View{
				Style: core.Style{Width: core.Pt(64)},
				Children: []core.Node{ui.Input{
					TestID:   "tileset-flags-input",
					Label:    ed.T("tilesetEditor.flagsInputLabel"),
					Disabled: disabled,
					Value:    flagsDraft.Get,
					OnChange: flagsDraft.Set,
					OnSubmit: func(text string) {
						v, err := strconv.ParseUint(strings.TrimSpace(text), 0, 32)
						if err != nil {
							ed.report(fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidFlags", l10n.Param("value", strconv.Quote(text)))))
							return
						}
						ed.report(ed.SetTilesetTileFlags(selTile, info.Solid, info.OneWay, tilemap.TileFlags(v)))
					},
				}},
			}),
		},
		ui.HStack{
			Gap: 8,
			Children: []core.Node{
				shapeCanvas,
				core.View{Style: core.Style{FlexGrow: 1, Gap: 4}, Children: shapeControls},
			},
		},
	}
	if len(shapeRows) > 0 {
		children = append(children, core.View{Style: core.Style{Gap: 3}, Children: shapeRows})
	}

	return core.View{
		TestID:   "tileset-collision-editor",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
}

func tilesetAnimationTab(ed *Editor, p theme.Palette, disabled func() bool, tileAnimRev *core.Signal[uint64], tileDraft, durDraft *core.Signal[string]) core.Node {
	selTile := ed.TilesetSelectedTile()
	info, _ := ed.TilesetTileInfo(selTile)
	playing := ed.TilesetAnimPlaying()

	playLabel := ed.T("tilesetEditor.animPlay")
	if playing {
		playLabel = ed.T("tilesetEditor.animPause")
	}

	var frameRows []core.Node
	for i, f := range info.Animation {
		idx := i
		frameRows = append(frameRows, core.View{
			TestID: fmt.Sprintf("tileset-anim-frame-%d", idx),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Padding:      core.Symmetric(2, 6),
				CornerRadius: 4,
				Background:   p.Raised,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("tilesetEditor.animFrameRow",
						l10n.Param("index", idx),
						l10n.Param("tile", f.Tile),
						l10n.Param("duration", f.Duration),
					),
					Style: core.Style{FontSize: 11, Color: p.TextSecondary, FlexGrow: 1},
				},
				ui.Button{
					TestID:   fmt.Sprintf("tileset-anim-remove-%d", idx),
					Text:     "×",
					Label:    ed.T("tilesetEditor.animRemoveLabel", l10n.Param("index", idx)),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.RemoveTilesetTileAnimFrame(selTile, idx))
					},
				},
			},
		})
	}

	children := []core.Node{
		core.Text{
			Content: ed.T("tilesetEditor.animHeading"),
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
		},
		ui.HStack{
			Gap: 8,
			Children: []core.Node{
				core.Canvas{
					TestID: "tileset-anim-preview",
					Style: core.Style{
						Width:        core.Pt(52),
						Height:       core.Pt(52),
						Background:   p.Bg,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						CornerRadius: 4,
					},
					Draw: func(d *core.Drawing, size core.Size) {
						_ = tileAnimRev.Get()
						previewTile := ed.TilesetAnimPreviewTile()
						if img, frame, ok := ed.tilesetTileVisual(previewTile); ok {
							drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, float32(size.Width), float32(size.Height))
						}
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, Gap: 4},
					Children: []core.Node{
						core.Text{
							TestID: "tileset-anim-status",
							ContentFunc: func() string {
								_ = tileAnimRev.Get()
								return ed.T("tilesetEditor.animStatus",
									l10n.Param("tile", ed.TilesetAnimPreviewTile()),
									l10n.Param("frames", len(info.Animation)),
								)
							},
							Style: core.Style{FontSize: 11, Color: p.TextMuted},
						},
						ui.Button{
							TestID:   "tileset-anim-play-toggle",
							Text:     playLabel,
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonSecondary,
							Disabled: disabled,
							OnPress: func() {
								ed.SetTilesetAnimPlaying(!playing)
							},
						},
					},
				},
			},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.View{
					Style: core.Style{Width: core.Pt(64)},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-anim-add-tile",
						Label:       ed.T("tilesetEditor.animTileIDLabel"),
						Placeholder: ed.T("tilesetEditor.animTileIDPlaceholder"),
						Disabled:    disabled,
						Value:       tileDraft.Get,
						OnChange:    tileDraft.Set,
					}},
				},
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-anim-add-duration",
						Label:       ed.T("tilesetEditor.animDurationLabel"),
						Placeholder: "150ms",
						Disabled:    disabled,
						Value:       durDraft.Get,
						OnChange:    durDraft.Set,
					}},
				},
				ui.Button{
					TestID:   "tileset-anim-add-frame",
					Text:     ed.T("tilesetEditor.animAddFrame"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						tid, err := strconv.Atoi(strings.TrimSpace(tileDraft.Get()))
						if err != nil {
							ed.report(fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidFrameTileID", l10n.Param("value", strconv.Quote(tileDraft.Get())))))
							return
						}
						dur, err := time.ParseDuration(strings.TrimSpace(durDraft.Get()))
						if err != nil || dur <= 0 {
							ed.report(fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidDuration", l10n.Param("value", strconv.Quote(durDraft.Get())))))
							return
						}
						ed.report(ed.AddTilesetTileAnimFrame(selTile, tilemap.TileFrameDoc{Tile: tid, Duration: dur}))
					},
				},
			},
		},
	}
	if len(frameRows) > 0 {
		children = append(children, core.View{Style: core.Style{Gap: 3}, Children: frameRows})
	} else {
		children = append(children, core.Text{
			TestID:  "tileset-anim-empty",
			Content: ed.T("tilesetEditor.animEmpty"),
			Style:   core.Style{FontSize: 11, Color: p.TextFaint},
		})
	}

	return core.View{
		TestID:   "tileset-animation-editor",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
}

func tilesetDataTab(ed *Editor, p theme.Palette, disabled func() bool, nameDraft, weightDraft, keyDraft, typeDraft, valDraft *core.Signal[string]) core.Node {
	selTile := ed.TilesetSelectedTile()
	info, _ := ed.TilesetTileInfo(selTile)

	typeBtn := func(testID, label, kind string) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     label,
			Disabled: disabled,
			On:       func() bool { return typeDraft.Get() == kind },
			OnChange: func(on bool) {
				if on {
					typeDraft.Set(kind)
				}
			},
		}
	}

	var propRows []core.Node
	for _, k := range slices.Sorted(maps.Keys(info.Properties)) {
		key := k
		v := info.Properties[key]
		kindLabel := ed.T("tilesetEditor.propTypeNameString")
		switch v.(type) {
		case bool:
			kindLabel = ed.T("tilesetEditor.propTypeNameBool")
		case int:
			kindLabel = ed.T("tilesetEditor.propTypeNameInt")
		case float64:
			kindLabel = ed.T("tilesetEditor.propTypeNameFloat")
		}
		propRows = append(propRows, core.View{
			TestID: "tileset-prop-row-" + key,
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Padding:      core.Symmetric(2, 6),
				CornerRadius: 4,
				Background:   p.Raised,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("tilesetEditor.propRow",
						l10n.Param("key", key),
						l10n.Param("kind", kindLabel),
						l10n.Param("value", v),
					),
					Style: core.Style{FontSize: 11, Color: p.TextSecondary, FlexGrow: 1},
				},
				ui.Button{
					TestID:   "tileset-prop-delete-" + key,
					Text:     "×",
					Label:    ed.T("tilesetEditor.propDeleteLabel", l10n.Param("key", key)),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.DeleteTilesetTileProperty(selTile, key))
					},
				},
			},
		})
	}

	children := []core.Node{
		core.Text{
			Content: ed.T("tilesetEditor.dataHeading"),
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-tile-name",
						Label:       ed.T("tilesetEditor.tileNameLabel"),
						Placeholder: ed.T("tilesetEditor.tileNamePlaceholder"),
						Disabled:    disabled,
						Value:       nameDraft.Get,
						OnChange:    nameDraft.Set,
						OnSubmit: func(text string) {
							ed.report(ed.SetTilesetTileName(selTile, text))
						},
					}},
				},
				core.View{
					Style: core.Style{Width: core.Pt(78)},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-tile-weight",
						Label:       ed.T("tilesetEditor.variantWeightLabel"),
						Placeholder: "1.0",
						Disabled:    disabled,
						Value:       weightDraft.Get,
						OnChange:    weightDraft.Set,
						OnSubmit: func(text string) {
							trimmed := strings.TrimSpace(text)
							if trimmed == "" {
								ed.report(ed.SetTilesetTileWeight(selTile, nil))
								return
							}
							v, err := strconv.ParseFloat(trimmed, 64)
							if err != nil {
								ed.report(fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidWeight", l10n.Param("value", strconv.Quote(trimmed)))))
								return
							}
							ed.report(ed.SetTilesetTileWeight(selTile, &v))
						},
					}},
				},
			},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				typeBtn("tileset-prop-type-string", ed.T("tilesetEditor.propTypeStr"), "string"),
				typeBtn("tileset-prop-type-int", ed.T("tilesetEditor.propTypeInt"), "int"),
				typeBtn("tileset-prop-type-float", ed.T("tilesetEditor.propTypeFloat"), "float"),
				typeBtn("tileset-prop-type-bool", ed.T("tilesetEditor.propTypeBool"), "bool"),
			},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-prop-key",
						Label:       ed.T("tilesetEditor.propKeyLabel"),
						Placeholder: ed.T("tilesetEditor.propKeyPlaceholder"),
						Disabled:    disabled,
						Value:       keyDraft.Get,
						OnChange:    keyDraft.Set,
					}},
				},
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-prop-value",
						Label:       ed.T("tilesetEditor.propValLabel"),
						Placeholder: ed.T("tilesetEditor.propValPlaceholder"),
						Disabled:    disabled,
						Value:       valDraft.Get,
						OnChange:    valDraft.Set,
					}},
				},
				ui.Button{
					TestID:   "tileset-prop-add",
					Text:     ed.T("tilesetEditor.propSet"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						k := strings.TrimSpace(keyDraft.Get())
						raw := strings.TrimSpace(valDraft.Get())
						val, err := parseTypedTileProperty(ed, typeDraft.Get(), raw)
						if err != nil {
							ed.report(err)
							return
						}
						if err := ed.SetTilesetTileProperty(selTile, k, val); err != nil {
							ed.report(err)
							return
						}
						keyDraft.Set("")
						valDraft.Set("")
					},
				},
			},
		},
	}
	if len(propRows) > 0 {
		children = append(children, core.View{Style: core.Style{Gap: 3}, Children: propRows})
	} else {
		children = append(children, core.Text{
			TestID:  "tileset-prop-empty",
			Content: ed.T("tilesetEditor.propEmpty"),
			Style:   core.Style{FontSize: 11, Color: p.TextFaint},
		})
	}

	return core.View{
		TestID:   "tileset-data-editor",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
}

func parseTypedTileProperty(ed *Editor, kind, raw string) (any, error) {
	switch kind {
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidBool", l10n.Param("value", strconv.Quote(raw))))
		}
		return b, nil
	case "int":
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidInt", l10n.Param("value", strconv.Quote(raw))))
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidFloat", l10n.Param("value", strconv.Quote(raw))))
		}
		return f, nil
	default:
		return raw, nil
	}
}

// tileInspectorView renders the right-side Inspector panel for a picked tile when a tile tool is active,
// matching design/editor/Tiles.dc.html with Spec tokens: tile swatch, name and source,
// COLLISION segmented control (Solid, One-way, None), RULES card (terrain set, collision, variant weight),
// the inference note, and a button to open the full Tileset Editor.
func tileInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	weightDraft := core.NewSignal(cx, "")
	syncedKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		tsPath, tileID, ok := ed.inspectedTilesetTile()
		if !ok {
			return ""
		}
		doc, err := ed.loadTilesetDoc(tsPath)
		if err != nil || doc == nil {
			return ""
		}
		w := "1"
		if t := findTileDoc(doc, tileID); t != nil && t.Weight != nil {
			w = strconv.FormatFloat(*t.Weight, 'g', -1, 64)
		}
		return fmt.Sprintf("%s|%d|%s", tsPath, tileID, w)
	})
	cx.Effect(func() {
		k := syncedKey.Get()
		cx.Untrack(func() {
			if parts := strings.Split(k, "|"); len(parts) == 3 {
				weightDraft.Set(parts[2])
			}
		})
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = rev.Get()
		p := ed.Palette(cx)
		tsPath, tileID, ok := ed.inspectedTilesetTile()
		if !ok {
			return nil
		}
		doc, err := ed.loadTilesetDoc(tsPath)
		if err != nil || doc == nil {
			return nil
		}
		tileMeta := findTileDoc(doc, tileID)
		tileName := TilesetTileDisplayName(doc, tileID)
		tileSource := TilesetTileSource(doc, tileID)

		collMode := "none"
		collText := ed.T("tilesetEditor.ruleNone")
		if tileMeta != nil {
			if tileMeta.Solid {
				collMode = "solid"
				collText = ed.T("tilesetEditor.ruleSolid")
			} else if tileMeta.OneWay {
				collMode = "one-way"
				collText = ed.T("tilesetEditor.ruleOneWayTop")
			}
		}
		terrainText := ed.T("tilesetEditor.ruleNone")
		if tileMeta != nil && tileMeta.Terrain != "" {
			terrainText = tileMeta.Terrain
		}
		weightText := "1"
		if tileMeta != nil && tileMeta.Weight != nil {
			weightText = fmt.Sprintf("%g · %s", *tileMeta.Weight, tileName)
		}

		// Resolve tile picture for the inspector swatch.
		var swatchImg core.ImageSource
		var swatchFrame sprite.Frame
		var hasSwatch bool
		if doc.Atlas != "" && ed.Resolver != nil {
			if atlas, err := ed.Resolver.Atlas(doc.Atlas); err == nil && atlas != nil {
				swatchImg = atlas.Image
				swatchFrame, hasSwatch = tilemap.TilesetFrame(doc, atlas, tileID)
			}
		} else {
			swatchImg = core.ImageSource{URI: doc.Image}
			if ir, ok := ed.Resolver.(sceneformat.ImageResolver); ok {
				if resolved, err := ir.Image(doc.Image); err == nil {
					swatchImg = resolved
				}
			}
			swatchFrame, hasSwatch = tilemap.TilesetFrame(doc, nil, tileID)
		}

		setCollision := func(mode string) {
			if err := ed.ensureTilesetOpenForPath(tsPath); err != nil {
				ed.report(err)
				return
			}
			ed.SetTilesetSelectedTile(tileID)
			ed.report(ed.SetTilesetTileCollision(tileID, mode))
		}

		collSeg := func(testID, label, mode string) core.Node {
			return ui.ToggleButton{
				TestID:   testID,
				Text:     label,
				Disabled: disabled,
				On:       func() bool { return collMode == mode },
				OnChange: func(on bool) {
					if on {
						setCollision(mode)
					}
				},
			}
		}

		ruleRow := func(k, v string, last bool) core.Node {
			borders := core.Edges{Bottom: 1}
			if last {
				borders = core.Edges{}
			}
			return core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(6, 10),
					Borders:        borders,
					BorderColor:    p.Line,
				},
				Children: []core.Node{
					core.Text{Content: k, Style: core.Style{FontSize: 12, Color: p.TextMuted}},
					core.Text{Content: v, Style: core.Style{FontSize: 11.5, Color: p.TextSecondary}},
				},
			}
		}

		return core.View{
			TestID: "inspector-tile",
			Style:  core.Style{Gap: 12},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10},
					Children: []core.Node{
						core.Canvas{
							TestID: "tile-inspector-swatch",
							Style: core.Style{
								Width:        core.Pt(36),
								Height:       core.Pt(36),
								CornerRadius: 6,
								Background:   p.Bg,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Draw: func(d *core.Drawing, size core.Size) {
								if hasSwatch {
									drawPaletteTile(d, PaletteTile{Image: swatchImg, Frame: swatchFrame}, float32(size.Width), float32(size.Height))
								}
							},
						},
						core.View{
							Style: core.Style{FlexGrow: 1, Gap: 2},
							Children: []core.Node{
								core.Text{
									TestID:  "tile-inspector-name",
									Content: tileName,
									Style:   core.Style{FontSize: 13, FontWeight: 600, Color: p.TextPrimary},
								},
								core.Text{
									TestID:  "tile-inspector-source",
									Content: tileSource,
									Style:   core.Style{FontSize: 11, Color: p.TextFaint},
								},
							},
						},
					},
				},
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: ed.T("tilesetEditor.collisionHeading"),
							Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
						},
						core.View{
							TestID: "tile-inspector-collision-group",
							Style: core.Style{
								Direction:    core.Row,
								Gap:          4,
								Padding:      core.All(3),
								CornerRadius: 8,
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Children: []core.Node{
								collSeg("tile-inspector-coll-solid", ed.T("tilesetEditor.collSolid"), "solid"),
								collSeg("tile-inspector-coll-oneway", ed.T("tilesetEditor.collOneWay"), "one-way"),
								collSeg("tile-inspector-coll-none", ed.T("tilesetEditor.collNone"), "none"),
							},
						},
					},
				},
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: ed.T("tilesetEditor.rulesHeading"),
							Style:   core.Style{FontSize: 11, FontWeight: 600, Color: p.TextFaint},
						},
						core.View{
							TestID: "tile-inspector-rules",
							Style: core.Style{
								CornerRadius: 10,
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
							},
							Children: []core.Node{
								ruleRow(ed.T("tilesetEditor.ruleTerrainSet"), terrainText, false),
								ruleRow(ed.T("tilesetEditor.ruleCollision"), collText, false),
								ruleRow(ed.T("tilesetEditor.variantWeightLabel"), weightText, true),
							},
						},
						ui.HStack{
							Gap: 6,
							Children: []core.Node{
								core.Text{Content: ed.T("tilesetEditor.weightLabel"), Style: core.Style{FontSize: 11, Color: p.TextMuted}},
								core.View{
									Style: core.Style{FlexGrow: 1},
									Children: []core.Node{ui.Input{
										TestID:      "tile-inspector-weight",
										Label:       ed.T("tilesetEditor.variantWeightLabel"),
										Placeholder: "1.0",
										Disabled:    disabled,
										Value:       weightDraft.Get,
										OnChange:    weightDraft.Set,
										OnSubmit: func(text string) {
											if err := ed.ensureTilesetOpenForPath(tsPath); err != nil {
												ed.report(err)
												return
											}
											ed.SetTilesetSelectedTile(tileID)
											trimmed := strings.TrimSpace(text)
											if trimmed == "" {
												ed.report(ed.SetTilesetTileWeight(tileID, nil))
												return
											}
											v, err := strconv.ParseFloat(trimmed, 64)
											if err != nil {
												ed.report(fmt.Errorf("%s", ed.T("tilesetEditor.errInvalidWeight", l10n.Param("value", strconv.Quote(trimmed)))))
												return
											}
											ed.report(ed.SetTilesetTileWeight(tileID, &v))
										},
									}},
								},
							},
						},
					},
				},
				core.Text{
					TestID:  "tile-inspector-note",
					Content: ed.T("tilesetEditor.inspectorNote"),
					Style:   core.Style{FontSize: 11.5, Color: p.TextFaint},
				},
				ui.Button{
					TestID:   "tile-inspector-open-editor",
					Text:     ed.T("tilesetEditor.editShapesAnimData"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						if err := ed.ensureTilesetOpenForPath(tsPath); err != nil {
							ed.report(err)
							return
						}
						ed.SetTilesetSelectedTile(tileID)
						ed.SetTilesetTab(TilesetTabCollision)
					},
				},
			},
		}
	})
}

// drawTileShapesOverlay draws a tile's collision shapes (rect, ellipse, polygon, polyline) and any in-progress draft polygon over a tile canvas.
func drawTileShapesOverlay(d *core.Drawing, p theme.Palette, tileW, tileH int, canvasW, canvasH float32, shapes []tilemap.TileShapeDoc, selectedIdx int, draftPoly [][2]float32) {
	if tileW <= 0 || tileH <= 0 || canvasW <= 0 || canvasH <= 0 {
		return
	}
	sx := canvasW / float32(tileW)
	sy := canvasH / float32(tileH)

	for i, s := range shapes {
		stroke := p.Added.Fg
		fill := ui.Alpha(p.Added.Fg, 0.12)
		if i == selectedIdx {
			stroke = p.Accent.Fg
			fill = ui.Alpha(p.Accent.Fg, 0.18)
		}
		d.SetFillColor(fill).SetStrokeColor(stroke).SetLineWidth(1.5)
		switch s.Kind {
		case "rect":
			d.FillRect(s.X*sx, s.Y*sy, s.Width*sx, s.Height*sy)
			d.BeginPath().Rect(s.X*sx, s.Y*sy, s.Width*sx, s.Height*sy).Stroke()
		case "ellipse":
			cx := (s.X + s.Width/2) * sx
			cy := (s.Y + s.Height/2) * sy
			d.BeginPath().Ellipse(cx, cy, s.Width*sx/2, s.Height*sy/2).Fill().Stroke()
		case "polygon", "polyline":
			if len(s.Points) < 2 {
				continue
			}
			d.BeginPath().MoveTo(s.Points[0][0]*sx, s.Points[0][1]*sy)
			for _, pt := range s.Points[1:] {
				d.LineTo(pt[0]*sx, pt[1]*sy)
			}
			if s.Kind == "polygon" {
				d.ClosePath().Fill().Stroke()
			} else {
				d.Stroke()
			}
		}
	}

	if len(draftPoly) > 0 {
		d.SetStrokeColor(p.Changed.Fg).SetFillColor(p.Changed.Fg).SetLineWidth(1.5)
		if len(draftPoly) >= 2 {
			d.BeginPath().MoveTo(draftPoly[0][0]*sx, draftPoly[0][1]*sy)
			for _, pt := range draftPoly[1:] {
				d.LineTo(pt[0]*sx, pt[1]*sy)
			}
			d.Stroke()
		}
		for _, pt := range draftPoly {
			d.FillRect(pt[0]*sx-2, pt[1]*sy-2, 4, 4)
		}
	}
}

// drawTileMaskDots draws small Spec-token indicators on the center and every active edge and corner of a tile thumbnail.
func drawTileMaskDots(d *core.Drawing, p theme.Palette, w, h float32, mask uint8) {
	if w <= 0 || h <= 0 {
		return
	}
	const s float32 = 4
	cx, cy := (w-s)/2, (h-s)/2
	d.SetFillColor(p.Added.Fg)
	d.FillRect(cx, cy, s, s)
	if mask&tilemap.MaskNorth != 0 {
		d.FillRect(cx, 1, s, s)
	}
	if mask&tilemap.MaskSouth != 0 {
		d.FillRect(cx, h-s-1, s, s)
	}
	if mask&tilemap.MaskWest != 0 {
		d.FillRect(1, cy, s, s)
	}
	if mask&tilemap.MaskEast != 0 {
		d.FillRect(w-s-1, cy, s, s)
	}
	if mask&tilemap.MaskNorthWest != 0 {
		d.FillRect(1, 1, s, s)
	}
	if mask&tilemap.MaskNorthEast != 0 {
		d.FillRect(w-s-1, 1, s, s)
	}
	if mask&tilemap.MaskSouthWest != 0 {
		d.FillRect(1, h-s-1, s, s)
	}
	if mask&tilemap.MaskSouthEast != 0 {
		d.FillRect(w-s-1, h-s-1, s, s)
	}
}
