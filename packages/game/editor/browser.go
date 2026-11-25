package editor

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// AtlasLister may optionally be implemented by a sceneformat.AssetResolver to advertise available atlas names to the Asset Browser, as project.Resolver does. sceneformat.AtlasMap is supported automatically.
type AtlasLister interface {
	AtlasNames() []string
}

// AssetEntry describes one selectable/droppable frame from a registered sprite.Atlas.
type AssetEntry struct {
	Atlas string
	Frame string
	Image core.ImageSource
	Rect  sprite.Frame
}

// Key uniquely identifies the asset entry as "atlas/frame".
func (a AssetEntry) Key() string {
	return a.Atlas + "/" + a.Frame
}

// Assets returns all available atlas frames in deterministic sorted order (by Atlas, then Frame).
func (e *Editor) Assets() []AssetEntry {
	if e == nil || e.Resolver == nil {
		return nil
	}
	e.syncProjectAssets()
	var atlasNames []string
	switch r := e.Resolver.(type) {
	case sceneformat.AtlasMap:
		atlasNames = make([]string, 0, len(r))
		for name := range r {
			atlasNames = append(atlasNames, name)
		}
	case AtlasLister:
		atlasNames = append(atlasNames, r.AtlasNames()...)
	default:
		return nil
	}
	slices.Sort(atlasNames)

	var out []AssetEntry
	for _, atlasName := range atlasNames {
		atlas, err := e.Resolver.Atlas(atlasName)
		if err != nil || atlas == nil {
			continue
		}
		frameNames := make([]string, 0, len(atlas.Frames))
		for fn := range atlas.Frames {
			frameNames = append(frameNames, fn)
		}
		slices.Sort(frameNames)
		for _, fn := range frameNames {
			out = append(out, AssetEntry{
				Atlas: atlasName,
				Frame: fn,
				Image: atlas.Image,
				Rect:  atlas.Frames[fn],
			})
		}
	}
	return out
}

// SelectAsset marks the named atlas frame as selected in the Asset Browser.
func (e *Editor) SelectAsset(atlasName, frameName string) bool {
	if e == nil || e.Resolver == nil {
		return false
	}
	atlas, err := e.Resolver.Atlas(atlasName)
	if err != nil || atlas == nil {
		return false
	}
	frame, ok := atlas.Frames[frameName]
	if !ok {
		return false
	}
	entry := AssetEntry{
		Atlas: atlasName,
		Frame: frameName,
		Image: atlas.Image,
		Rect:  frame,
	}
	e.SelectedAsset = &entry
	e.SelectedPrefab = ""
	e.Status = "Selected asset " + entry.Key()
	e.notify()
	return true
}

// DropAtlasFrame places a new sprite object referencing (atlasName, frameName) at world/layer
// coordinates (x, y) on the active layer as an undoable PlaceObject command.
func (e *Editor) DropAtlasFrame(atlasName, frameName string, x, y float32) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	if e.Resolver == nil {
		return "", fmt.Errorf("editor: no asset resolver configured")
	}
	atlas, err := e.Resolver.Atlas(atlasName)
	if err != nil {
		return "", fmt.Errorf("editor: resolve atlas %q: %w", atlasName, err)
	}
	if atlas == nil {
		return "", fmt.Errorf("editor: atlas %q is nil", atlasName)
	}
	if _, ok := atlas.Frames[frameName]; !ok {
		return "", fmt.Errorf("editor: atlas %q has no frame %q", atlasName, frameName)
	}

	return e.PlaceObject(e.ActiveLayer, sceneformat.ObjectDoc{
		Name: frameName,
		X:    x,
		Y:    y,
		Sprite: &sceneformat.SpriteRef{
			Atlas: atlasName,
			Frame: frameName,
		},
	})
}

// AssetBrowserView renders the left-side Asset Browser panel listing all available atlases, their frames with canvas thumbnails, and click-to-select / quick-place actions, then the models to place in 3D (Models), the built-in primitives and the project's glTF files, with thumbnails rendered offscreen (ModelThumbnail). Pressing a model selects it for the Place tool (SelectModel) and turns the tool on; dragging it onto the 3D viewport places it where it is dropped (PlaceModelAt3D). It updates reactively whenever atlases, frames or models change (for example after ReloadAssets).
func AssetBrowserView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	assets := core.NewMemoFunc(cx, func() []AssetEntry {
		_ = rev.Get()
		return ed.Assets()
	}, func(a, b []AssetEntry) bool {
		return slices.EqualFunc(a, b, func(x, y AssetEntry) bool {
			return x.Atlas == y.Atlas && x.Frame == y.Frame && x.Rect == y.Rect && sameImage(x.Image, y.Image)
		})
	})

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		entries := assets.Get()
		if len(entries) == 0 {
			return core.Text{
				Content: ed.T("assets.empty"),
				Style: core.Style{
					FontSize: 12,
					Color:    colors.Muted(),
				},
			}
		}

		rows := make([]core.Node, 0, len(entries))
		for _, entry := range entries {
			ent := entry
			rowID := "asset-" + ent.Atlas + "-" + ent.Frame
			placeID := "asset-place-" + ent.Atlas + "-" + ent.Frame

			rows = append(rows, core.View{
				TestID: rowID,
				StyleFunc: func() core.Style {
					_ = rev.Get()
					selected := ed.SelectedAsset != nil && ed.SelectedAsset.Key() == ent.Key()
					bg := colors.Surface()
					border := colors.Line()
					if selected {
						bg = colors.Raised()
						border = colors.Accent()
					}
					return core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          8,
						Padding:      core.All(6),
						CornerRadius: 6,
						BorderWidth:  1,
						BorderColor:  border,
						Background:   bg,
						FocusVisible: ed.SpecPalette(cx).FocusRing(-2),
					}
				},
				OnPress: func(core.PressEvent) {
					ed.SelectAsset(ent.Atlas, ent.Frame)
					ed.SetTool(ToolPlace)
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "asset-thumb-" + ent.Atlas + "-" + ent.Frame,
						Style: core.Style{
							Width:        core.Pt(24),
							Height:       core.Pt(24),
							Background:   colors.Inset(),
							CornerRadius: 4,
						},
						Draw: func(d *core.Drawing, size core.Size) {
							f := ent.Rect
							sw, sh := float32(size.Width), float32(size.Height)
							if f.W <= 0 || f.H <= 0 || sw <= 0 || sh <= 0 {
								return
							}
							scale := min(sw/f.W, sh/f.H)
							dw, dh := f.W*scale, f.H*scale
							dx, dy := (sw-dw)/2, (sh-dh)/2
							d.DrawImageFrom(ent.Image, f.X, f.Y, f.W, f.H, dx, dy, dw, dh)
						},
					},
					core.View{
						Style: core.Style{
							FlexGrow: 1,
							Gap:      2,
						},
						Children: []core.Node{
							core.Text{
								Content: ent.Frame,
								Style: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      colors.Text(),
								},
							},
							core.Text{
								Content: ent.Atlas,
								Style: core.Style{
									FontSize: 10,
									Color:    colors.Muted(),
								},
							},
						},
					},
					ui.Button{
						TestID:  placeID,
						Text:    "+",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing()
						},
						OnPress: func() {
							cx, cy := float32(32), float32(32)
							if ed.Doc != nil && ed.Doc.VirtualWidth > 0 && ed.Doc.VirtualHeight > 0 {
								cx = ed.Doc.VirtualWidth / 2
								cy = ed.Doc.VirtualHeight / 2
							}
							sx, sy := ed.SnapPosition2D(float64(cx), float64(cy))
							_, _ = ed.DropAtlasFrame(ent.Atlas, ent.Frame, float32(sx), float32(sy))
						},
					},
				},
			})
		}

		return core.View{
			Style:    core.Style{Gap: 6},
			Children: rows,
		}
	})

	return core.View{
		TestID: "asset-browser",
		Style: dockedStyle(cx, core.Style{
			Width:       core.Pt(224),
			Background:  colors.Inset(),
			Borders:     core.Edges{Right: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(10),
			Gap:         8,
		}),
		Children: []core.Node{
			core.Text{
				TestID:  "assets-title",
				Content: ed.T("assets.title"),
				Style: core.Style{
					FontSize:   13,
					FontWeight: 700,
					Color:      colors.Text(),
				},
			},
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinHeight: core.Pt(0),
				},
				ContentStyle: core.Style{
					Gap: 6,
				},
				Children: []core.Node{
					ownDragArea(cx, prefabListView(cx, ed, rev)),
					list,
					ownDragArea(cx, modelListView(cx, ed, rev)),
				},
			},
		},
	}
}

func prefabListView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	names := core.NewMemoFunc(cx, func() []string {
		_ = rev.Get()
		return ed.PrefabNames()
	}, slices.Equal[[]string])

	return core.Dynamic(func(cx *core.Scope) core.Node {
		entries := names.Get()
		if len(entries) == 0 {
			return nil
		}
		rows := make([]core.Node, 0, len(entries))
		for _, n := range entries {
			name := n
			detail := ed.T("assets.prefab")
			if p, ok := ed.Prefabs[name]; ok && p.Sprite != nil && p.Sprite.Atlas != "" {
				detail = p.Sprite.Atlas + "/" + p.Sprite.Frame
			}
			row := core.View{
				TestID: "prefab-" + name,
				Label:  name,
				StyleFunc: func() core.Style {
					_ = rev.Get()
					bg, border := colors.Surface(), colors.Line()
					if ed.SelectedPrefab == name {
						bg, border = colors.Raised(), colors.Accent()
					}
					return core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          8,
						Padding:      core.All(6),
						CornerRadius: 6,
						BorderWidth:  1,
						BorderColor:  border,
						Background:   bg,
						FocusVisible: ed.SpecPalette(cx).FocusRing(-2),
					}
				},
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing()
				},
				OnPress: func(core.PressEvent) {
					ed.SelectPrefab(name)
					ed.SetTool(ToolPlace)
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
						Children: []core.Node{
							core.Text{
								Content: name,
								Style:   core.Style{FontSize: 12, FontWeight: 600, Color: colors.Text()},
							},
							core.Text{
								Content: detail,
								Style:   core.Style{FontSize: 10, Color: colors.Muted()},
							},
						},
					},
					ui.Button{
						TestID:  "prefab-place-" + name,
						Text:    "+",
						Label:   ed.T("assets.placePrefab"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							return ed.Playing()
						},
						OnPress: func() {
							cx, cy := ed.defaultPlacePos()
							_, err := ed.PlacePrefab(ed.ActiveLayer, name, cx, cy)
							ed.report(err)
						},
					},
				},
			}
			rows = append(rows, dragdrop.Source{
				View: row,
				Data: func() any {
					if ed.Playing() {
						return nil
					}
					return prefabDrag{name: name}
				},
				Preview: func(any) core.Node {
					return core.Text{
						Content: name,
						Style: core.Style{
							FontSize:     12,
							Padding:      core.Symmetric(4, 8),
							CornerRadius: 4,
							Color:        colors.Text(),
							Background:   colors.Raised(),
						},
					}
				},
			})
		}
		return core.View{
			TestID: "prefab-list",
			Style:  core.Style{Gap: 6},
			Children: []core.Node{
				core.Text{
					TestID:  "prefabs-title",
					Content: ed.T("assets.prefabs"),
					Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
				},
				core.View{Style: core.Style{Gap: 6}, Children: rows},
			},
		}
	})
}
