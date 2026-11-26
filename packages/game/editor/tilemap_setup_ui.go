package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
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
				Label:       "Tileset Atlas",
				Placeholder: "Choose atlas",
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
				Label:       "Tileset Atlas",
				Placeholder: "tiles",
				Disabled:    disabled,
				Value:       tileset.Get,
				OnChange:    tileset.Set,
			}
		}

		return []core.Node{
			sectionHeader("TILEMAP"),
			fieldRow("Tileset", tilesetControl),
			pairRow("Tile Size",
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-tile-w",
					Label:       "Tile Width",
					LeadingText: "W",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(tileW.Get()) },
					OnChange:    func(v float64) { tileW.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-tile-h",
					Label:       "Tile Height",
					LeadingText: "H",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(tileH.Get()) },
					OnChange:    func(v float64) { tileH.Set(int(math.Round(v))) },
				},
			),
			pairRow("Grid Size",
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-width",
					Label:       "Grid Width",
					LeadingText: "W",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(gridW.Get()) },
					OnChange:    func(v float64) { gridW.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-add-height",
					Label:       "Grid Height",
					LeadingText: "H",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(gridH.Get()) },
					OnChange:    func(v float64) { gridH.Set(int(math.Round(v))) },
				},
			),
			ui.Button{
				TestID:   "inspector-layer-tilemap-add",
				Text:     "Add Tilemap",
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
		sectionHeader("TILEMAP"),
		core.Text{
			TestID: "inspector-layer-tilemap-summary",
			ContentFunc: func() string {
				tm, ref := resolvedTM()
				if tm == nil {
					if ref != "" {
						return "Ref: " + ref + " (missing)"
					}
					return "No tilemap"
				}
				prefix := "Inline"
				if ref != "" {
					prefix = "Ref: " + ref
				}
				switch {
				case tm.IsExternal():
					return prefix + " · " + tm.Source
				case tm.IsLayered():
					s := fmt.Sprintf("%s · layered %dx%d (%dx%d)", prefix, tm.Width, tm.Height, tm.TileWidth, tm.TileHeight)
					if tm.OffsetX != 0 || tm.OffsetY != 0 {
						s += fmt.Sprintf(" · offset (%g, %g)", tm.OffsetX, tm.OffsetY)
					}
					return s
				default:
					s := fmt.Sprintf("%s · %dx%d (%dx%d)", prefix, tm.Width, tm.Height, tm.TileWidth, tm.TileHeight)
					if tm.OffsetX != 0 || tm.OffsetY != 0 {
						s += fmt.Sprintf(" · offset (%g, %g)", tm.OffsetX, tm.OffsetY)
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
			nodes = append(nodes, fieldRow("Tileset", ui.Select[string]{
				TestID:      "inspector-layer-tilemap-tileset",
				Label:       "Tileset Atlas",
				Placeholder: "Choose atlas",
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
			pairRow("Tile Size",
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-tile-w",
					Label:       "Tile Width",
					LeadingText: "W",
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
					Label:       "Tile Height",
					LeadingText: "H",
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
			sectionHeader("RESIZE EDGES"),
			pairRow("L / R",
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-left",
					Label:       "Resize Left Edge",
					LeadingText: "L",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(leftDelta.Get()) },
					OnChange:    func(v float64) { leftDelta.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-right",
					Label:       "Resize Right Edge",
					LeadingText: "R",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(rightDelta.Get()) },
					OnChange:    func(v float64) { rightDelta.Set(int(math.Round(v))) },
				},
			),
			pairRow("T / B",
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-top",
					Label:       "Resize Top Edge",
					LeadingText: "T",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(topDelta.Get()) },
					OnChange:    func(v float64) { topDelta.Set(int(math.Round(v))) },
				},
				ui.NumberInput{
					TestID:      "inspector-layer-tilemap-resize-bottom",
					Label:       "Resize Bottom Edge",
					LeadingText: "B",
					Step:        1,
					Format:      formatInt,
					Disabled:    disabled,
					Value:       func() float64 { return float64(bottomDelta.Get()) },
					OnChange:    func(v float64) { bottomDelta.Set(int(math.Round(v))) },
				},
			),
			ui.Button{
				TestID:   "inspector-layer-tilemap-resize",
				Text:     "Resize Grid",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonSecondary,
				Disabled: disabled,
				OnPress:  applyResize,
			},
		)
	}

	removeBtn := ui.Button{
		TestID:   "inspector-layer-tilemap-remove",
		Text:     "Remove",
		Label:    "Remove Tilemap",
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
			fieldRow("Ref Name", ui.Input{
				TestID:      "inspector-layer-tilemap-ref-name",
				Label:       "Top-Level Tilemap Name",
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
						Text:     "Move to Top-Level",
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
					Text:     "Move Inline",
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
