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

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

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

	header := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{
				Content: "Tileset Editor",
				Style:   core.Style{FontSize: 13, FontWeight: 600, Color: specTextStrong},
			},
			core.Text{
				TestID: "tileset-editor-path",
				ContentFunc: func() string {
					_ = rev.Get()
					p := ed.TilesetPath()
					if p == "" {
						p = "Untitled"
					} else {
						p = filepath.Base(p)
					}
					if ed.TilesetDirty() {
						p += "*"
					}
					return p
				},
				Style: core.Style{FontSize: 12, Color: specTextMuted},
			},
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					Gap:          4,
					Padding:      core.All(2),
					CornerRadius: 6,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					tabBtn("tileset-tab-terrain", "Terrain", TilesetTabTerrain),
					tabBtn("tileset-tab-collision", "Collision", TilesetTabCollision),
					tabBtn("tileset-tab-animation", "Animation", TilesetTabAnimation),
					tabBtn("tileset-tab-data", "Data", TilesetTabData),
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:   "tileset-editor-save",
				Text:     "Save Tileset",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.SaveTileset())
				},
			},
			ui.Button{
				TestID:   "tileset-editor-close",
				Text:     "Close",
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
					Content: "TERRAINS",
					Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
				},
				core.Dynamic(func(cx *core.Scope) core.Node {
					_ = rev.Get()
					if !ed.TilesetOpen() {
						return nil
					}
					doc := ed.tileset.doc
					sel := ed.tileset.selectedTerrain
					if len(doc.Terrains) == 0 {
						return core.Text{
							TestID:  "tileset-terrains-empty",
							Content: "No terrains yet",
							Style:   core.Style{FontSize: 11, Color: specTextMuted},
						}
					}
					var btns []core.Node
					for _, td := range doc.Terrains {
						name := td.Name
						modeLabel := "4-bit"
						if td.Mode == "blob" {
							modeLabel = "8-bit"
						}
						btns = append(btns, ui.ToggleButton{
							TestID:   "tileset-terrain-" + name,
							Text:     fmt.Sprintf("%s (%s)", name, modeLabel),
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
								Label:       "New terrain name",
								Placeholder: "Terrain name...",
								Disabled:    disabled,
								Value:       newName.Get,
								OnChange:    newName.Set,
							}},
						},
						ui.Button{
							TestID:  "tileset-add-terrain",
							Text:    "+ Add",
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
							Text:     "4-bit",
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
							Text:     "8-bit",
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
					if !ed.TilesetOpen() || ed.tileset.selectedTerrain == "" {
						return nil
					}
					sel := ed.tileset.selectedTerrain
					_, td := terrainDocByName(ed.tileset.doc, sel)
					if td == nil {
						return nil
					}
					defText := "Default: mask 0"
					if td.Default != nil {
						defText = fmt.Sprintf("Default: tile %d", *td.Default)
					}
					return core.View{
						Style: core.Style{Gap: 4},
						Children: []core.Node{
							ui.HStack{
								Gap: 4,
								Children: []core.Node{
									ui.ToggleButton{
										TestID:   "tileset-mode-cardinal",
										Text:     "Cardinal (4-bit)",
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
										Text:     "Blob (8-bit)",
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
								Style:   core.Style{FontSize: 11, Color: specTextMuted},
							},
							ui.HStack{
								Gap: 4,
								Children: []core.Node{
									ui.Button{
										TestID:   "tileset-set-default",
										Text:     "Set Default",
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
										Text:    "Clear",
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
										Text:     "Delete",
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
				border := specLineRaised
				if tileID == selTile {
					border = specAccent
				}
				items = append(items, core.View{
					TestID: fmt.Sprintf("tileset-tile-%d", tileID),
					Label:  fmt.Sprintf("Tile %d", tileID),
					Style: core.Style{
						Padding:      core.All(2),
						BorderWidth:  2,
						BorderColor:  border,
						CornerRadius: 4,
						Background:   specRaised,
						FocusVisible: specFocusRingStyle,
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
								Background: specBg,
							},
							Draw: func(d *core.Drawing, size core.Size) {
								w, h := float32(size.Width), float32(size.Height)
								if hasVis {
									drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
								}
								if tileMeta != nil {
									if tileMeta.Solid {
										d.SetStrokeColor(specAccent).SetLineWidth(1.5).BeginPath().Rect(1, 1, w-2, h-2).Stroke()
									} else if tileMeta.OneWay {
										d.SetFillColor(specChanged).FillRect(0, 0, w, 3)
									}
									if len(tileMeta.Shapes) > 0 {
										drawTileShapesOverlay(d, doc.TileWidth, doc.TileHeight, w, h, tileMeta.Shapes, -1, nil)
									}
									if len(tileMeta.Animation) > 0 {
										d.SetFillColor(specAgent).FillRect(1, h-6, 5, 5)
									}
								}
								if assigned {
									drawTileMaskDots(d, w, h, mask)
								}
								if isDefault {
									d.SetFillColor(specWarning).FillRect(w-7, 1, 6, 6)
								}
							},
						},
						core.Text{
							Content: fmt.Sprintf("%d", tileID),
							Style:   core.Style{FontSize: 10, Color: specTextMuted},
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
			if !ed.TilesetOpen() {
				return nil
			}
			switch ed.TilesetTab() {
			case TilesetTabCollision:
				return tilesetCollisionTab(ed, disabled, flagsDraft)
			case TilesetTabAnimation:
				return tilesetAnimationTab(ed, disabled, tileAnimRev, animTileDraft, animDurDraft)
			case TilesetTabData:
				return tilesetDataTab(ed, disabled, nameDraft, weightDraft, propKeyDraft, propTypeDraft, propValDraft)
			default:
				return tilesetTerrainMaskTab(ed, disabled)
			}
		})},
	}

	return core.View{
		TestID: "tileset-editor-panel",
		Style: core.Style{
			Height:      core.Pt(270),
			Padding:     core.Symmetric(8, 12),
			Gap:         8,
			Background:  specPanel,
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			header,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.TilesetError() != nil
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID: "tileset-editor-error",
						ContentFunc: func() string {
							_ = rev.Get()
							if err := ed.TilesetError(); err != nil {
								return err.Error()
							}
							return ""
						},
						Style: core.Style{FontSize: 11, Color: specProblem},
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
}

func tilesetTerrainMaskTab(ed *Editor, disabled func() bool) core.Node {
	st := ed.tileset
	selTile := st.selectedTile
	selTerrain := st.selectedTerrain
	if selTerrain == "" {
		return core.View{
			TestID: "tileset-mask-editor",
			Style:  core.Style{Gap: 6},
			Children: []core.Node{
				core.Text{
					Content: "Add or select a terrain to edit tile edges and corners.",
					Style:   core.Style{FontSize: 11, Color: specTextMuted},
				},
			},
		}
	}
	_, td := terrainDocByName(st.doc, selTerrain)
	isBlob := td != nil && td.Mode == "blob"
	_, assigned := tileDocMask(st.doc, selTile, selTerrain)
	mask := st.editMask
	conflict := st.conflictTile

	summary := "unassigned"
	if assigned || conflict >= 0 {
		names := tilemap.TerrainMaskNeighbors(mask)
		if len(names) == 0 {
			summary = "center (isolated / mask 0)"
		} else {
			summary = strings.Join(names, ", ")
		}
	}

	bitBtn := func(testID, label string, bit uint8, corner bool) core.Node {
		active := mask&bit != 0
		return ui.ToggleButton{
			TestID: testID,
			Text:   label,
			Label:  fmt.Sprintf("Toggle %s neighbor", label),
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
		Text:     "C",
		Label:    "Toggle tile terrain membership",
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
						Background: specBg,
					},
					Draw: func(d *core.Drawing, size core.Size) {
						w, h := float32(size.Width), float32(size.Height)
						if hasVis {
							drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
						}
						if assigned || conflict >= 0 {
							drawTileMaskDots(d, w, h, mask)
						}
					},
				},
				core.View{
					Style: core.Style{FlexGrow: 1, Gap: 2},
					Children: []core.Node{
						core.Text{
							TestID:  "tileset-mask-title",
							Content: fmt.Sprintf("Tile %d — %s", selTile, selTerrain),
							Style:   core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong},
						},
						core.Text{
							TestID:  "tileset-mask-summary",
							Content: summary,
							Style:   core.Style{FontSize: 11, Color: specTextMuted},
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
					Content: fmt.Sprintf("Owned by tile %d", conflict),
					Style:   core.Style{FontSize: 11, Color: specProblem, FlexGrow: 1},
				},
				ui.Button{
					TestID:   "tileset-mask-reassign",
					Text:     "Reassign",
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
		Text:    "Clear Tile",
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

func tilesetCollisionTab(ed *Editor, disabled func() bool, flagsDraft *core.Signal[string]) core.Node {
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
			Text:     fmt.Sprintf("B%d", b),
			Label:    fmt.Sprintf("Toggle custom flag bit %d", b),
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
		Label:  "Tile collision shape editor canvas",
		Style: core.Style{
			Width:        core.Pt(core.Points(canvasSize)),
			Height:       core.Pt(core.Points(canvasSize)),
			BorderWidth:  1,
			BorderColor:  specLineRaised,
			CornerRadius: 4,
			Background:   specBg,
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
					w, h := float32(size.Width), float32(size.Height)
					if hasVis {
						drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
					}
					// Subtle midpoint crosshair guides.
					d.SetStrokeColor(specLineRaised).SetLineWidth(1)
					d.BeginPath().MoveTo(w/2, 0).LineTo(w/2, h).MoveTo(0, h/2).LineTo(w, h/2).Stroke()
					drawTileShapesOverlay(d, doc.TileWidth, doc.TileHeight, w, h, info.Shapes, selShape, draftPoly)
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
					Text:     "Rect",
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
					Text:     "Polygon",
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
					Text:     "+ Full Rect",
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
					Text:     "+ Slope",
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
					Content: fmt.Sprintf("%d pts", len(draftPoly)),
					Style:   core.Style{FontSize: 11, Color: specChanged},
				},
				ui.Button{
					TestID:  "tileset-shape-finish-poly",
					Text:    "Finish",
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
					Text:     "Cancel",
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
		desc := s.Kind
		if s.Kind == "rect" || s.Kind == "ellipse" {
			desc = fmt.Sprintf("%s (%.0f,%.0f %.0fx%.0f)", s.Kind, s.X, s.Y, s.Width, s.Height)
		} else {
			desc = fmt.Sprintf("%s (%d pts)", s.Kind, len(s.Points))
		}
		rowBg := specRaised
		if idx == selShape {
			rowBg = specAccentTint
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
					Style:   core.Style{FontSize: 11, Color: specText, FlexGrow: 1},
				},
				ui.Button{
					TestID:   fmt.Sprintf("tileset-shape-remove-%d", idx),
					Text:     "×",
					Label:    fmt.Sprintf("Remove collision shape %d", idx),
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
			Content: "COLLISION",
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				collBtn("tileset-coll-solid", "Solid", "solid"),
				collBtn("tileset-coll-oneway", "One-way", "one-way"),
				collBtn("tileset-coll-none", "None", "none"),
			},
		},
		core.View{
			Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4, AlignItems: core.AlignCenter},
			Children: append(bitBtns, core.View{
				Style: core.Style{Width: core.Pt(64)},
				Children: []core.Node{ui.Input{
					TestID:   "tileset-flags-input",
					Label:    "Custom flags mask",
					Disabled: disabled,
					Value:    flagsDraft.Get,
					OnChange: flagsDraft.Set,
					OnSubmit: func(text string) {
						v, err := strconv.ParseUint(strings.TrimSpace(text), 0, 32)
						if err != nil {
							ed.report(fmt.Errorf("editor: invalid flags mask %q", text))
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

func tilesetAnimationTab(ed *Editor, disabled func() bool, tileAnimRev *core.Signal[uint64], tileDraft, durDraft *core.Signal[string]) core.Node {
	selTile := ed.TilesetSelectedTile()
	info, _ := ed.TilesetTileInfo(selTile)
	playing := ed.TilesetAnimPlaying()

	playLabel := "Play"
	if playing {
		playLabel = "Pause"
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
				Background:   specRaised,
			},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("#%d  tile %d  %s", idx, f.Tile, f.Duration),
					Style:   core.Style{FontSize: 11, Color: specText, FlexGrow: 1},
				},
				ui.Button{
					TestID:   fmt.Sprintf("tileset-anim-remove-%d", idx),
					Text:     "×",
					Label:    fmt.Sprintf("Remove animation frame %d", idx),
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
			Content: "ANIMATION",
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
		},
		ui.HStack{
			Gap: 8,
			Children: []core.Node{
				core.Canvas{
					TestID: "tileset-anim-preview",
					Style: core.Style{
						Width:        core.Pt(52),
						Height:       core.Pt(52),
						Background:   specBg,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
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
								return fmt.Sprintf("Showing tile %d (%d frames)", ed.TilesetAnimPreviewTile(), len(info.Animation))
							},
							Style: core.Style{FontSize: 11, Color: specTextSecondary},
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
						Label:       "Frame tile ID",
						Placeholder: "tile ID",
						Disabled:    disabled,
						Value:       tileDraft.Get,
						OnChange:    tileDraft.Set,
					}},
				},
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-anim-add-duration",
						Label:       "Frame duration",
						Placeholder: "150ms",
						Disabled:    disabled,
						Value:       durDraft.Get,
						OnChange:    durDraft.Set,
					}},
				},
				ui.Button{
					TestID:   "tileset-anim-add-frame",
					Text:     "+ Frame",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						tid, err := strconv.Atoi(strings.TrimSpace(tileDraft.Get()))
						if err != nil {
							ed.report(fmt.Errorf("editor: invalid frame tile ID %q", tileDraft.Get()))
							return
						}
						dur, err := time.ParseDuration(strings.TrimSpace(durDraft.Get()))
						if err != nil || dur <= 0 {
							ed.report(fmt.Errorf("editor: invalid duration %q (use e.g. 150ms)", durDraft.Get()))
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
			Content: "Static tile. Add frames to animate.",
			Style:   core.Style{FontSize: 11, Color: specTextMuted},
		})
	}

	return core.View{
		TestID:   "tileset-animation-editor",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
}

func tilesetDataTab(ed *Editor, disabled func() bool, nameDraft, weightDraft, keyDraft, typeDraft, valDraft *core.Signal[string]) core.Node {
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
		kind := "string"
		switch v.(type) {
		case bool:
			kind = "bool"
		case int:
			kind = "int"
		case float64:
			kind = "float"
		}
		propRows = append(propRows, core.View{
			TestID: "tileset-prop-row-" + key,
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Padding:      core.Symmetric(2, 6),
				CornerRadius: 4,
				Background:   specRaised,
			},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("%s (%s) = %v", key, kind, v),
					Style:   core.Style{FontSize: 11, Color: specText, FlexGrow: 1},
				},
				ui.Button{
					TestID:   "tileset-prop-delete-" + key,
					Text:     "×",
					Label:    "Delete property " + key,
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
			Content: "TILE DATA",
			Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-tile-name",
						Label:       "Tile name",
						Placeholder: "Name (e.g. moss)...",
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
						Label:       "Variant weight",
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
								ed.report(fmt.Errorf("editor: invalid weight %q", trimmed))
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
				typeBtn("tileset-prop-type-string", "str", "string"),
				typeBtn("tileset-prop-type-int", "int", "int"),
				typeBtn("tileset-prop-type-float", "float", "float"),
				typeBtn("tileset-prop-type-bool", "bool", "bool"),
			},
		},
		ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-prop-key",
						Label:       "Property key",
						Placeholder: "key",
						Disabled:    disabled,
						Value:       keyDraft.Get,
						OnChange:    keyDraft.Set,
					}},
				},
				core.View{
					Style: core.Style{FlexGrow: 1},
					Children: []core.Node{ui.Input{
						TestID:      "tileset-prop-value",
						Label:       "Property value",
						Placeholder: "value",
						Disabled:    disabled,
						Value:       valDraft.Get,
						OnChange:    valDraft.Set,
					}},
				},
				ui.Button{
					TestID:   "tileset-prop-add",
					Text:     "+ Set",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						k := strings.TrimSpace(keyDraft.Get())
						raw := strings.TrimSpace(valDraft.Get())
						val, err := parseTypedTileProperty(typeDraft.Get(), raw)
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
			Content: "No custom properties.",
			Style:   core.Style{FontSize: 11, Color: specTextMuted},
		})
	}

	return core.View{
		TestID:   "tileset-data-editor",
		Style:    core.Style{Gap: 6},
		Children: children,
	}
}

func parseTypedTileProperty(kind, raw string) (any, error) {
	switch kind {
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("editor: invalid bool %q (use true or false)", raw)
		}
		return b, nil
	case "int":
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("editor: invalid int %q", raw)
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("editor: invalid float %q", raw)
		}
		return f, nil
	default:
		return raw, nil
	}
}

// tileInspectorView renders the right-side Inspector panel for a picked tile when a tile tool is active,
// matching docs/design/editor/Tiles.dc.html with Spec tokens: tile swatch, name and source,
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
		collText := "none"
		if tileMeta != nil {
			if tileMeta.Solid {
				collMode = "solid"
				collText = "solid"
			} else if tileMeta.OneWay {
				collMode = "one-way"
				collText = "one-way top"
			}
		}
		terrainText := "none"
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
					BorderColor:    specLine,
				},
				Children: []core.Node{
					core.Text{Content: k, Style: core.Style{FontSize: 12, Color: specTextSecondary}},
					core.Text{Content: v, Style: core.Style{FontSize: 11.5, Color: specText}},
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
								Background:   specBg,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
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
									Style:   core.Style{FontSize: 13, FontWeight: 600, Color: specTextStrong},
								},
								core.Text{
									TestID:  "tile-inspector-source",
									Content: tileSource,
									Style:   core.Style{FontSize: 11, Color: specTextMuted},
								},
							},
						},
					},
				},
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: "COLLISION",
							Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
						},
						core.View{
							TestID: "tile-inspector-collision-group",
							Style: core.Style{
								Direction:    core.Row,
								Gap:          4,
								Padding:      core.All(3),
								CornerRadius: 8,
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
							},
							Children: []core.Node{
								collSeg("tile-inspector-coll-solid", "Solid", "solid"),
								collSeg("tile-inspector-coll-oneway", "One-way", "one-way"),
								collSeg("tile-inspector-coll-none", "None", "none"),
							},
						},
					},
				},
				core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						core.Text{
							Content: "RULES",
							Style:   core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted},
						},
						core.View{
							TestID: "tile-inspector-rules",
							Style: core.Style{
								CornerRadius: 10,
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
							},
							Children: []core.Node{
								ruleRow("Terrain set", terrainText, false),
								ruleRow("Collision", collText, false),
								ruleRow("Variant weight", weightText, true),
							},
						},
						ui.HStack{
							Gap: 6,
							Children: []core.Node{
								core.Text{Content: "Weight", Style: core.Style{FontSize: 11, Color: specTextSecondary}},
								core.View{
									Style: core.Style{FlexGrow: 1},
									Children: []core.Node{ui.Input{
										TestID:      "tile-inspector-weight",
										Label:       "Variant weight",
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
												ed.report(fmt.Errorf("editor: invalid weight %q", trimmed))
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
					Content: "The agent inferred these rules when it sliced the tileset. Change one here, or ask it to change a whole set.",
					Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
				},
				ui.Button{
					TestID:   "tile-inspector-open-editor",
					Text:     "Edit Shapes, Animation & Data",
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
func drawTileShapesOverlay(d *core.Drawing, tileW, tileH int, canvasW, canvasH float32, shapes []tilemap.TileShapeDoc, selectedIdx int, draftPoly [][2]float32) {
	if tileW <= 0 || tileH <= 0 || canvasW <= 0 || canvasH <= 0 {
		return
	}
	sx := canvasW / float32(tileW)
	sy := canvasH / float32(tileH)

	for i, s := range shapes {
		stroke := specAdded
		fill := specAddedFill
		if i == selectedIdx {
			stroke = specAccent
			fill = specAccentTint
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
		d.SetStrokeColor(specChanged).SetFillColor(specChanged).SetLineWidth(1.5)
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
func drawTileMaskDots(d *core.Drawing, w, h float32, mask uint8) {
	if w <= 0 || h <= 0 {
		return
	}
	const s float32 = 4
	cx, cy := (w-s)/2, (h-s)/2
	d.SetFillColor(specAdded)
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
