package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// TerrainPanelView renders the docked Terrain Sculpt & Paint panel below the
// viewport while a terrain is open: brush mode selector (Raise, Lower, Smooth,
// Flatten, Set Height, Paint Layer, Paint Foliage), falloff selector (Smooth,
// Linear, Flat), numeric inputs for radius, strength, target height, and target
// foliage density, active material/foliage layer pickers, and Save/Close actions.
func TerrainPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.TerrainOpen()
		},
		Then: func(cx *core.Scope) core.Node { return terrainPanel(cx, ed, rev) },
	}
}

func terrainPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	modeBtn := func(testID string, mode TerrainBrushMode) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     mode.String(),
			Disabled: disabled,
			On: func() bool {
				_ = rev.Get()
				return ed.TerrainBrush().Mode == mode
			},
			OnChange: func(on bool) {
				if on {
					ed.SetTerrainBrushMode(mode)
				} else if ed.TerrainBrush().Mode == mode {
					ed.SetTerrainBrushMode(TerrainBrushNone)
				}
			},
		}
	}

	falloffBtn := func(testID string, f TerrainBrushFalloff) core.Node {
		return ui.ToggleButton{
			TestID:   testID,
			Text:     f.String(),
			Disabled: disabled,
			On: func() bool {
				_ = rev.Get()
				return ed.TerrainBrush().Falloff == f
			},
			OnChange: func(on bool) {
				if on {
					b := ed.TerrainBrush()
					b.Falloff = f
					ed.SetTerrainBrush(b)
				}
			},
		}
	}

	floatField := func(testID, label string, get func() float32, set func(float32)) core.Node {
		return core.View{
			Style: core.Style{Width: core.Pt(86), Gap: 2},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style:   core.Style{FontSize: 10, Color: colors.Muted()},
				},
				ui.Input{
					TestID:   testID,
					Label:    label,
					Disabled: disabled,
					Value: func() string {
						_ = rev.Get()
						return strconv.FormatFloat(float64(get()), 'g', 4, 32)
					},
					OnChange: func(text string) {
						if v, err := strconv.ParseFloat(strings.TrimSpace(text), 32); err == nil {
							set(float32(v))
						}
					},
				},
			},
		}
	}

	layerSig := core.NewMemo(cx, func() string {
		_ = rev.Get()
		doc := ed.TerrainDoc()
		if doc == nil {
			return ""
		}
		var b strings.Builder
		for _, l := range doc.Layers {
			b.WriteString(l.Name)
			b.WriteByte(',')
		}
		b.WriteByte('|')
		for _, fl := range doc.Foliage {
			b.WriteString(fl.Name)
			b.WriteByte(',')
		}
		return b.String()
	})

	header := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{
				Content: "Terrain",
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
			},
			core.Text{
				TestID: "terrain-path",
				ContentFunc: func() string {
					_ = rev.Get()
					p := ed.TerrainPath()
					label := "Untitled Terrain"
					if p != "" {
						label = filepath.Base(p)
					} else if doc := ed.TerrainDoc(); doc != nil && doc.Name != "" {
						label = doc.Name
					}
					if hm := ed.Terrain(); hm != nil {
						label = fmt.Sprintf("%s (%dx%d)", label, hm.Cols, hm.Rows)
					}
					if ed.TerrainDirty() {
						label += "*"
					}
					return label
				},
				Style: core.Style{FontSize: 12, Color: colors.Muted()},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:   "terrain-save",
				Text:     "Save Terrain",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.SaveTerrain())
				},
			},
			ui.Button{
				TestID:   "terrain-close",
				Text:     "Close",
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: disabled,
				OnPress: func() {
					ed.CloseTerrain()
				},
			},
		},
	}

	brushes := ui.HStack{
		Gap: 4,
		Children: []core.Node{
			modeBtn("terrain-brush-off", TerrainBrushNone),
			modeBtn("terrain-brush-raise", TerrainBrushRaise),
			modeBtn("terrain-brush-lower", TerrainBrushLower),
			modeBtn("terrain-brush-smooth", TerrainBrushSmooth),
			modeBtn("terrain-brush-flatten", TerrainBrushFlatten),
			modeBtn("terrain-brush-set-height", TerrainBrushSetHeight),
			modeBtn("terrain-brush-paint-layer", TerrainBrushPaintLayer),
			modeBtn("terrain-brush-paint-foliage", TerrainBrushPaintFoliage),
		},
	}

	params := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			floatField("terrain-brush-radius", "Radius",
				func() float32 { return ed.TerrainBrush().Radius },
				func(v float32) {
					if v > 0 {
						b := ed.TerrainBrush()
						b.Radius = v
						ed.SetTerrainBrush(b)
					}
				},
			),
			floatField("terrain-brush-strength", "Strength",
				func() float32 { return ed.TerrainBrush().Strength },
				func(v float32) {
					if v > 0 && v <= 1 {
						b := ed.TerrainBrush()
						b.Strength = v
						ed.SetTerrainBrush(b)
					}
				},
			),
			floatField("terrain-brush-target-height", "Target Y",
				func() float32 { return ed.TerrainBrush().TargetHeight },
				func(v float32) {
					b := ed.TerrainBrush()
					b.TargetHeight = v
					ed.SetTerrainBrush(b)
				},
			),
			floatField("terrain-brush-target-density", "Density",
				func() float32 { return ed.TerrainBrush().TargetDensity },
				func(v float32) {
					b := ed.TerrainBrush()
					b.TargetDensity = clamp01f32(v)
					ed.SetTerrainBrush(b)
				},
			),
			core.View{
				Style: core.Style{Gap: 2},
				Children: []core.Node{
					core.Text{
						Content: "Falloff",
						Style:   core.Style{FontSize: 10, Color: colors.Muted()},
					},
					ui.HStack{
						Gap: 4,
						Children: []core.Node{
							falloffBtn("terrain-falloff-smooth", TerrainFalloffSmooth),
							falloffBtn("terrain-falloff-linear", TerrainFalloffLinear),
							falloffBtn("terrain-falloff-flat", TerrainFalloffFlat),
						},
					},
				},
			},
		},
	}

	layersRow := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = layerSig.Get()
		doc := ed.TerrainDoc()
		if doc == nil {
			return nil
		}
		var layerBtns []core.Node
		layerBtns = append(layerBtns, core.Text{
			Content: "Layers:",
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		})
		for i, l := range doc.Layers {
			idx := i
			layerBtns = append(layerBtns, ui.ToggleButton{
				TestID:   "terrain-layer-" + l.Name,
				Text:     l.Name,
				Disabled: disabled,
				On: func() bool {
					_ = rev.Get()
					return ed.TerrainBrush().LayerIndex == idx
				},
				OnChange: func(on bool) {
					if on {
						b := ed.TerrainBrush()
						b.LayerIndex = idx
						b.Mode = TerrainBrushPaintLayer
						ed.SetTerrainBrush(b)
					}
				},
			})
		}
		if len(doc.Foliage) > 0 {
			layerBtns = append(layerBtns, core.Text{
				Content: "Foliage:",
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			})
			for i, fl := range doc.Foliage {
				idx := i
				layerBtns = append(layerBtns, ui.ToggleButton{
					TestID:   "terrain-foliage-" + fl.Name,
					Text:     fl.Name,
					Disabled: disabled,
					On: func() bool {
						_ = rev.Get()
						return ed.TerrainBrush().FoliageIndex == idx
					},
					OnChange: func(on bool) {
						if on {
							b := ed.TerrainBrush()
							b.FoliageIndex = idx
							b.Mode = TerrainBrushPaintFoliage
							ed.SetTerrainBrush(b)
						}
					},
				})
			}
		}
		return ui.HStack{Gap: 6, Children: layerBtns}
	})

	return core.View{
		TestID: "terrain-panel",
		Style: core.Style{
			Padding:     core.Symmetric(6, 12),
			Gap:         6,
			Background:  colors.Inset(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		},
		Children: []core.Node{
			header,
			brushes,
			params,
			layersRow,
		},
	}
}
