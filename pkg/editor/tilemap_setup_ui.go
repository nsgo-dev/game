package editor

import (
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// layerTilemapSchemaTag returns a stable mode tag for inspectorSchemaKey so InspectorView remounts the layer inspector only when the layer's tilemap switches mode (none, inline/ref x single/layered/external).
func layerTilemapSchemaTag(ed *Editor, l *sceneformat.LayerDoc) string {
	if l == nil || l.Tilemap == nil {
		return "none"
	}
	tm := l.Tilemap
	prefix := "inline:"
	if tm.IsRef() {
		prefix = "ref:"
		if ed != nil && ed.Doc != nil {
			tm = ed.Doc.Tilemaps[tm.Ref]
		} else {
			tm = nil
		}
		if tm == nil {
			return "ref:missing"
		}
	}
	switch {
	case tm.IsExternal():
		return prefix + "external"
	case tm.IsLayered():
		return prefix + "layered"
	default:
		return prefix + "single"
	}
}

// layerTilemapSection builds the TILEMAP section inside layerInspectorView: adding an inline tilemap when the layer has none, or editing the tileset, tile size, grid edges, top-level ref storage, and removal when it has one.
func layerTilemapSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	layerName string,
	layer func() *sceneformat.LayerDoc,
	disabled func() bool,
	sectionHeader func(string) core.Node,
	fieldRow func(string, core.Node) core.Node,
) []core.Node {
	colors := ui.ColorsOf(cx)
	formatInt := func(v float64) string { return strconv.Itoa(int(math.Round(v))) }

	atlasOptions := func(current string) []ui.Option[string] {
		names := ed.atlasNames()
		opts := make([]ui.Option[string], 0, len(names)+1)
		listed := false
		for _, n := range names {
			listed = listed || n == current
			opts = append(opts, ui.Option[string]{Value: n, Label: n})
		}
		if current != "" && !listed {
			opts = append([]ui.Option[string]{{Value: current, Label: current}}, opts...)
		}
		return opts
	}

	pairRow := func(label string, a, b core.Node) core.Node {
		return fieldRow(label, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
			Children: []core.Node{
				core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{a}},
				core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{b}},
			},
		})
	}

	// Read the layer untracked: the enclosing inspector Dynamic already rebuilds when the tilemap's shape changes (inspectorSchemaKey includes layerTilemapSchemaTag), and tracking rev here would rebuild the whole layer inspector on every notify, such as a live problem check finishing, remounting its rows under a press and resetting the add-tilemap form.
	lNow := core.Untracked(cx, layer)
	if lNow == nil || lNow.Tilemap == nil {
		tileset := core.NewSignal(cx, ed.defaultTileAtlas())
		tileW := core.NewSignal(cx, 16)
		tileH := core.NewSignal(cx, 16)
		gridW := core.NewSignal(cx, 16)
		gridH := core.NewSignal(cx, 12)

		var tilesetControl core.Node
		if len(ed.atlasNames()) > 0 {
			tilesetControl = ui.Select[string]{
				TestID:      "inspector-layer-tilemap-add-tileset",
				Label:       ed.T("tilemapSetup.tilesetAtlasLabel"),
				Placeholder: ed.T("tilemapSetup.chooseAtlas"),
				Searchable:  true,
				Disabled:    disabled,
				Style:       core.Style{Width: core.Pct(100)},
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					return atlasOptions(tileset.Get())
				},
				Value:    tileset.Get,
				OnChange: tileset.Set,
			}
		} else {
			tilesetControl = ui.Input{
				TestID:      "inspector-layer-tilemap-add-tileset",
				Label:       ed.T("tilemapSetup.tilesetAtlasLabel"),
				Placeholder: ed.T("tilemapSetup.tilesPlaceholder"),
				Disabled:    disabled,
				Value:       tileset.Get,
				OnChange:    tileset.Set,
			}
		}

		return []core.Node{
			sectionHeader(ed.T("tilemapSetup.heading")),
			fieldRow(ed.T("tilemapSetup.tileset"), tilesetControl),
			pairRow(ed.T("tilemapSetup.tileSize"),
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-tile-w",
					Label:       ed.T("tilemapSetup.tileWidth"),
					LeadingText: ed.T("tilemapSetup.axisW"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(tileW.Get()) },
					OnChange:    func(v float64) { tileW.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-tile-h",
					Label:       ed.T("tilemapSetup.tileHeight"),
					LeadingText: ed.T("tilemapSetup.axisH"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(tileH.Get()) },
					OnChange:    func(v float64) { tileH.Set(int(math.Round(v))) },
				},
			),
			pairRow(ed.T("tilemapSetup.gridSize"),
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-width",
					Label:       ed.T("tilemapSetup.gridWidth"),
					LeadingText: ed.T("tilemapSetup.axisW"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(gridW.Get()) },
					OnChange:    func(v float64) { gridW.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-height",
					Label:       ed.T("tilemapSetup.gridHeight"),
					LeadingText: ed.T("tilemapSetup.axisH"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(gridH.Get()) },
					OnChange:    func(v float64) { gridH.Set(int(math.Round(v))) },
				},
			),
			ui.Button{
				TestID:   "inspector-layer-tilemap-add",
				Text:     ed.T("tilemapSetup.addTilemap"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonPrimary,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.AddLayerTilemap(
						layerName,
						strings.TrimSpace(tileset.Get()),
						tileW.Get(),
						tileH.Get(),
						gridW.Get(),
						gridH.Get(),
					))
				},
			},
		}
	}

	resolvedTM := func() (*sceneformat.TilemapDoc, string) {
		l := layer()
		if l == nil || l.Tilemap == nil {
			return nil, ""
		}
		if l.Tilemap.IsRef() {
			ref := l.Tilemap.Ref
			if ed.Doc != nil {
				return ed.Doc.Tilemaps[ref], ref
			}
			return nil, ref
		}
		return l.Tilemap, ""
	}

	nodes := []core.Node{
		sectionHeader(ed.T("tilemapSetup.heading")),
		core.Text{
			TestID: "inspector-layer-tilemap-summary",
			ContentFunc: func() string {
				tm, ref := resolvedTM()
				if tm == nil {
					if ref != "" {
						return ed.T("tilemapSetup.refMissing", l10n.Param("ref", ref))
					}
					return ed.T("tilemapSetup.noTilemap")
				}
				prefix := ed.T("tilemapSetup.prefixInline")
				if ref != "" {
					prefix = ed.T("tilemapSetup.prefixRef", l10n.Param("ref", ref))
				}
				switch {
				case tm.IsExternal():
					return ed.T("tilemapSetup.summaryExternal", l10n.Param("prefix", prefix), l10n.Param("source", tm.Source))
				case tm.IsLayered():
					s := ed.T("tilemapSetup.summaryLayered",
						l10n.Param("prefix", prefix),
						l10n.Param("width", tm.Width),
						l10n.Param("height", tm.Height),
						l10n.Param("tileW", tm.TileWidth),
						l10n.Param("tileH", tm.TileHeight),
					)
					if tm.OffsetX != 0 || tm.OffsetY != 0 {
						s += ed.T("tilemapSetup.summaryOffset",
							l10n.Param("x", strconv.FormatFloat(float64(tm.OffsetX), 'g', -1, 32)),
							l10n.Param("y", strconv.FormatFloat(float64(tm.OffsetY), 'g', -1, 32)),
						)
					}
					return s
				default:
					s := ed.T("tilemapSetup.summarySingle",
						l10n.Param("prefix", prefix),
						l10n.Param("width", tm.Width),
						l10n.Param("height", tm.Height),
						l10n.Param("tileW", tm.TileWidth),
						l10n.Param("tileH", tm.TileHeight),
					)
					if tm.OffsetX != 0 || tm.OffsetY != 0 {
						s += ed.T("tilemapSetup.summaryOffset",
							l10n.Param("x", strconv.FormatFloat(float64(tm.OffsetX), 'g', -1, 32)),
							l10n.Param("y", strconv.FormatFloat(float64(tm.OffsetY), 'g', -1, 32)),
						)
					}
					return s
				}
			},
			Style: core.Style{FontSize: 11, Color: colors.Muted()},
		},
	}

	// Read the resolved tilemap untracked so layers that already have a tilemap do not subscribe the enclosing inspector Dynamic to rev.
	type resolvedTilemapSnapshot struct {
		tm  *sceneformat.TilemapDoc
		ref string
	}
	tmSnap := core.Untracked(cx, func() resolvedTilemapSnapshot {
		tm, ref := resolvedTM()
		return resolvedTilemapSnapshot{tm: tm, ref: ref}
	})
	tmNow, refNow := tmSnap.tm, tmSnap.ref
	if tmNow != nil && tmNow.IsInline() {
		if !tmNow.IsLayered() {
			nodes = append(nodes, fieldRow(ed.T("tilemapSetup.tileset"), ui.Select[string]{
				TestID:      "inspector-layer-tilemap-tileset",
				Label:       ed.T("tilemapSetup.tilesetAtlasLabel"),
				Placeholder: ed.T("tilemapSetup.chooseAtlas"),
				Searchable:  true,
				Disabled:    disabled,
				Style:       core.Style{Width: core.Pct(100)},
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					cur := ""
					if tm, _ := resolvedTM(); tm != nil {
						cur = tm.Tileset
					}
					return atlasOptions(cur)
				},
				Value: func() string {
					if tm, _ := resolvedTM(); tm != nil {
						return tm.Tileset
					}
					return ""
				},
				OnChange: func(v string) {
					ed.report(ed.SetLayerTilemapTileset(layerName, v))
				},
			}))
		}

		nodes = append(nodes,
			pairRow(ed.T("tilemapSetup.tileSize"),
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-tile-w",
					Label:       ed.T("tilemapSetup.tileWidth"),
					LeadingText: ed.T("tilemapSetup.axisW"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value: func() float64 {
						if tm, _ := resolvedTM(); tm != nil {
							return float64(tm.TileWidth)
						}
						return 0
					},
					OnChange: func(v float64) {
						if tm, _ := resolvedTM(); tm != nil {
							ed.report(ed.SetLayerTilemapTileSize(layerName, int(math.Round(v)), tm.TileHeight))
						}
					},
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-tile-h",
					Label:       ed.T("tilemapSetup.tileHeight"),
					LeadingText: ed.T("tilemapSetup.axisH"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value: func() float64 {
						if tm, _ := resolvedTM(); tm != nil {
							return float64(tm.TileHeight)
						}
						return 0
					},
					OnChange: func(v float64) {
						if tm, _ := resolvedTM(); tm != nil {
							ed.report(ed.SetLayerTilemapTileSize(layerName, tm.TileWidth, int(math.Round(v))))
						}
					},
				},
			),
		)

		leftDelta := core.NewSignal(cx, 0)
		rightDelta := core.NewSignal(cx, 0)
		topDelta := core.NewSignal(cx, 0)
		bottomDelta := core.NewSignal(cx, 0)

		applyResize := func() {
			if err := ed.ResizeLayerTilemap(layerName, leftDelta.Get(), rightDelta.Get(), topDelta.Get(), bottomDelta.Get()); err != nil {
				ed.report(err)
				return
			}
			leftDelta.Set(0)
			rightDelta.Set(0)
			topDelta.Set(0)
			bottomDelta.Set(0)
		}

		nodes = append(nodes,
			sectionHeader(ed.T("tilemapSetup.resizeEdgesHeading")),
			pairRow(ed.T("tilemapSetup.leftRight"),
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-left",
					Label:       ed.T("tilemapSetup.resizeLeftLabel"),
					LeadingText: ed.T("tilemapSetup.edgeL"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(leftDelta.Get()) },
					OnChange:    func(v float64) { leftDelta.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-right",
					Label:       ed.T("tilemapSetup.resizeRightLabel"),
					LeadingText: ed.T("tilemapSetup.edgeR"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(rightDelta.Get()) },
					OnChange:    func(v float64) { rightDelta.Set(int(math.Round(v))) },
				},
			),
			pairRow(ed.T("tilemapSetup.topBottom"),
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-top",
					Label:       ed.T("tilemapSetup.resizeTopLabel"),
					LeadingText: ed.T("tilemapSetup.edgeT"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(topDelta.Get()) },
					OnChange:    func(v float64) { topDelta.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-bottom",
					Label:       ed.T("tilemapSetup.resizeBottomLabel"),
					LeadingText: ed.T("tilemapSetup.edgeB"),
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(bottomDelta.Get()) },
					OnChange:    func(v float64) { bottomDelta.Set(int(math.Round(v))) },
				},
			),
			ui.Button{
				TestID:   "inspector-layer-tilemap-resize",
				Text:     ed.T("tilemapSetup.resizeGrid"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonSecondary,
				Disabled: disabled,
				OnPress:  applyResize,
			},
		)
	}

	removeBtn := ui.Button{
		TestID:   "inspector-layer-tilemap-remove",
		Text:     ed.T("tilemapSetup.remove"),
		Label:    ed.T("tilemapSetup.removeLabel"),
		Size:     ui.SizeSmall,
		Variant:  ui.ButtonOutline,
		Disabled: disabled,
		OnPress: func() {
			ed.report(ed.RemoveLayerTilemap(layerName))
		},
	}

	if refNow == "" {
		refDraft := core.NewSignal(cx, ed.newTilemapRefName(layerName))
		nodes = append(nodes,
			fieldRow(ed.T("tilemapSetup.refName"), ui.Input{
				TestID:      "inspector-layer-tilemap-ref-name",
				Label:       ed.T("tilemapSetup.refNameLabel"),
				Placeholder: layerName,
				Disabled:    disabled,
				Value:       refDraft.Get,
				OnChange:    refDraft.Set,
				OnSubmit: func(s string) {
					ed.report(ed.ExtractLayerTilemap(layerName, strings.TrimSpace(s)))
				},
			}),
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					ui.Button{
						TestID:   "inspector-layer-tilemap-extract",
						Text:     ed.T("tilemapSetup.moveToTopLevel"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress: func() {
							ed.report(ed.ExtractLayerTilemap(layerName, strings.TrimSpace(refDraft.Get())))
						},
					},
					removeBtn,
				},
			},
		)
	} else {
		nodes = append(nodes, ui.HStack{
			Gap: 6,
			Children: []core.Node{
				ui.Button{
					TestID:   "inspector-layer-tilemap-inline",
					Text:     ed.T("tilemapSetup.moveInline"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.InlineLayerTilemap(layerName))
					},
				},
				removeBtn,
			},
		})
	}

	return nodes
}
