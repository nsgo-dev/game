package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// TilesetEditorPanelView renders the docked Tileset Editor below the viewport while a YAML tileset (*.tileset.yaml) is open:
// terrain set creation/selection (4-bit cardinal or 8-bit blob, plus default tile), a visual tile grid showing each tile's assigned edges and corners,
// and an interactive 3x3 edge-and-corner mask editor for the selected tile.
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
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	newName := core.NewSignal(cx, "")
	newMode := core.NewSignal(cx, "blob")

	header := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{
				Content: "Tileset Editor",
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
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
				Style: core.Style{FontSize: 12, Color: colors.Muted()},
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
	terrainCol := core.View{
		TestID: "tileset-terrains",
		Style: core.Style{
			Width: core.Pt(230),
			Gap:   6,
		},
		Children: []core.Node{
			core.Text{
				Content: "Terrains",
				Style:   core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text()},
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
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
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
							Style:   core.Style{FontSize: 11, Color: colors.Muted()},
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
	}

	// Middle column: tile picker grid with assigned edge/corner overlays.
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
				img, frame, hasVis := ed.tilesetTileVisual(tileID)
				border := colors.Line()
				if tileID == selTile {
					border = colors.Accent()
				}
				items = append(items, core.View{
					TestID: fmt.Sprintf("tileset-tile-%d", tileID),
					Label:  fmt.Sprintf("Tile %d", tileID),
					Style: core.Style{
						Padding:      core.All(2),
						BorderWidth:  2,
						BorderColor:  border,
						CornerRadius: 3,
						Background:   colors.Surface(),
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
								Width:  core.Pt(32),
								Height: core.Pt(32),
							},
							Draw: func(d *core.Drawing, size core.Size) {
								w, h := float32(size.Width), float32(size.Height)
								if hasVis {
									drawPaletteTile(d, PaletteTile{Image: img, Frame: frame}, w, h)
								}
								if assigned {
									drawTileMaskDots(d, w, h, mask)
								}
								if isDefault {
									d.SetFillColor(core.RGBA(0xFBBF24, 0.95)).FillRect(w-7, 1, 6, 6)
								}
							},
						},
						core.Text{
							Content: fmt.Sprintf("%d", tileID),
							Style:   core.Style{FontSize: 10, Color: colors.Muted()},
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

	// Right column: interactive 3x3 edge & corner mask editor for the selected tile.
	maskCol := core.View{
		TestID: "tileset-mask-editor",
		Style: core.Style{
			Width: core.Pt(260),
			Gap:   6,
		},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			_ = rev.Get()
			if !ed.TilesetOpen() {
				return nil
			}
			st := ed.tileset
			selTile := st.selectedTile
			selTerrain := st.selectedTerrain
			if selTerrain == "" {
				return core.Text{
					Content: "Add or select a terrain to edit tile edges and corners.",
					Style:   core.Style{FontSize: 11, Color: colors.Muted()},
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
								Background: colors.Surface(),
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
									Style:   core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text()},
								},
								core.Text{
									TestID:  "tileset-mask-summary",
									Content: summary,
									Style:   core.Style{FontSize: 11, Color: colors.Muted()},
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
							Style:   core.Style{FontSize: 11, Color: colors.Danger(), FlexGrow: 1},
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

			return core.View{Style: core.Style{Gap: 6}, Children: children}
		})},
	}

	return core.View{
		TestID: "tileset-editor-panel",
		Style: core.Style{
			Height:      core.Pt(250),
			Padding:     core.Symmetric(6, 12),
			Gap:         6,
			Background:  colors.Inset(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
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
						Style: core.Style{FontSize: 11, Color: colors.Danger()},
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
				Children: []core.Node{terrainCol, tileGridCol, maskCol},
			},
		},
	}
}

// drawTileMaskDots draws small emerald indicators on the center and every active edge and corner of a tile thumbnail.
func drawTileMaskDots(d *core.Drawing, w, h float32, mask uint8) {
	if w <= 0 || h <= 0 {
		return
	}
	const s float32 = 4
	cx, cy := (w-s)/2, (h-s)/2
	d.SetFillColor(core.RGBA(0x34D399, 0.9))
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
