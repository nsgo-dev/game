package editor

import (
	"errors"
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/base/dragdrop"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// dragAreaKey says a dragdrop.Area of EditorView's spans the asset browser and the viewport, so a model dragged from the one drops on the other; a browser or viewport mounted on its own makes its own area.
var dragAreaKey = core.NewKey("editor.dragArea", false)

// ownDragArea wraps node in a dragdrop.Area of its own unless EditorView's shared one holds it.
func ownDragArea(cx *core.Scope, node core.Node) core.Node {
	if dragAreaKey.Use(cx) {
		return node
	}
	return dragdrop.Area{Children: []core.Node{node}}
}

// SetPlaceOptions3D sets how the Place tool puts models in the 3D viewport: the ground plane's height (PlaceHeight3D), snapping to the grid (PlaceSnap3D) and aligning to the surface's normal (PlaceAlign3D). Like the camera, they are a view setting, not an edit: not undoable and not saved. It refuses a height that is not finite.
func (e *Editor) SetPlaceOptions3D(height float32, snap, align bool) error {
	if !isFiniteFloat32(height) {
		return errors.New("editor: ground height must be finite")
	}
	if e.PlaceHeight3D == height && e.PlaceSnap3D == snap && e.PlaceAlign3D == align {
		return nil
	}
	e.PlaceHeight3D, e.PlaceSnap3D, e.PlaceAlign3D = height, snap, align
	e.notify()
	return nil
}

// sceneFileDrag is carried while a scene file row is dragged from the file browser onto the viewport.
type sceneFileDrag struct{ path string }

// prefabDrag is carried while a prefab row is dragged from the asset browser onto the viewport.
type prefabDrag struct{ name string }

// defaultPlacePos returns the virtual viewport center used by quick-place ("+") buttons.
func (e *Editor) defaultPlacePos() (float32, float32) {
	if e != nil && e.Doc != nil && e.Doc.VirtualWidth > 0 && e.Doc.VirtualHeight > 0 {
		return e.Doc.VirtualWidth / 2, e.Doc.VirtualHeight / 2
	}
	return 32, 32
}

// modelDropTarget makes frame, the viewport's frame, take models dragged from the asset browser (in 3D),
// sprite assets dragged from the Asset Drawer (in 2D), scene files dragged from the file browser, and prefabs dragged from the asset browser.
func modelDropTarget(cx *core.Scope, ed *Editor, frame core.View) core.Node {
	return ownDragArea(cx, dragdrop.Target{
		View: frame,
		Accept: func(data any) bool {
			if ed.Playing() {
				return false
			}
			switch data.(type) {
			case ModelEntry:
				return ed.ViewMode == ViewMode3D
			case AssetDrawerDrag:
				return ed.ViewMode == ViewMode2D
			case sceneFileDrag, prefabDrag:
				return true
			default:
				return false
			}
		},
		OnEnter: func(ev dragdrop.Event) {
			if d, ok := ev.Data.(AssetDrawerDrag); ok && ed.ViewMode == ViewMode2D {
				ed.updateAssetDragPayload(d, float32(ev.Position.X), float32(ev.Position.Y))
			}
		},
		OnMove: func(ev dragdrop.Event) {
			if d, ok := ev.Data.(AssetDrawerDrag); ok && ed.ViewMode == ViewMode2D {
				ed.updateAssetDragPayload(d, float32(ev.Position.X), float32(ev.Position.Y))
			}
		},
		OnLeave: func(ev dragdrop.Event) {
			if _, ok := ev.Data.(AssetDrawerDrag); ok {
				ed.ClearAssetDragHover()
			}
		},
		OnDrop: func(ev dragdrop.Event) {
			switch d := ev.Data.(type) {
			case ModelEntry:
				ed.report(ed.SelectModel(d))
				cw, ch := ed.CanvasSize()
				_, err := ed.PlaceModelAt3D(d, float32(ev.Position.X), float32(ev.Position.Y), cw, ch)
				ed.report(err)
			case AssetDrawerDrag:
				_, _, err := ed.dropAssetDragAt(d, float32(ev.Position.X), float32(ev.Position.Y))
				ed.report(err)
			case sceneFileDrag:
				var wx, wy float32
				if ed.ViewMode == ViewMode2D {
					wx, wy = ed.layerCoordsForActive(float32(ev.Position.X), float32(ev.Position.Y))
				}
				_, err := ed.PlaceScene(ed.ActiveLayer, d.path, wx, wy)
				ed.report(err)
			case prefabDrag:
				var wx, wy float32
				if ed.ViewMode == ViewMode2D {
					wx, wy = ed.layerCoordsForActive(float32(ev.Position.X), float32(ev.Position.Y))
				}
				_, err := ed.PlacePrefab(ed.ActiveLayer, d.name, wx, wy)
				ed.report(err)
			}
		},
	})
}

// dropModelFiles places the glTF files dropped on the viewport from another app. A file drop carries no position (NSGo v0.3.0's core.View.OnFileDrop takes only the URLs, a gap reported to nsgo-dev/nsgo), so the models land where the view's center points, which a drag from the asset browser does not need.
func (e *Editor) dropModelFiles(uris []string) {
	if e.ViewMode != ViewMode3D {
		e.report(errors.New("editor: switch the viewport to 3D to place dropped models"))
		return
	}
	cw, ch := e.CanvasSize()
	_, err := e.DropModelFiles(uris, cw/2, ch/2, cw, ch)
	e.report(err)
}

// modelTestID turns a model's key into a test ID: its letters and digits, with every other run of characters a dash ("models/crate.glb" is "models-crate-glb").
func modelTestID(m ModelEntry) string {
	var b strings.Builder
	dash := false
	for _, r := range m.Key() {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
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

// modelListView lists the models the asset browser offers (Models), each with its thumbnail (ModelThumbnail). Pressing one selects it for the Place tool and, in 3D, turns the tool on; dragging one onto the 3D viewport places it where it is dropped.
func modelListView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	thumbRev := core.NewSignal(cx, uint64(0))
	ed.thumbs.onReady = func() { thumbRev.Set(thumbRev.GetUntracked() + 1) }
	cx.Cleanup(func() { ed.thumbs.onReady = nil })
	models := core.NewMemoFunc(cx, func() []ModelEntry {
		_ = rev.Get()
		return ed.Models()
	}, slices.Equal[[]ModelEntry])

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		entries := models.Get()
		rows := make([]core.Node, 0, len(entries))
		for _, entry := range entries {
			m := entry
			id := modelTestID(m)
			detail := "Primitive"
			if m.Path != "" {
				detail = m.Path
			}
			row := core.View{
				TestID: "model-" + id,
				Label:  m.Name(),
				StyleFunc: func() core.Style {
					_ = rev.Get()
					bg, border := colors.Surface(), colors.Line()
					if ed.SelectedModel != nil && *ed.SelectedModel == m {
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
					}
				},
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing()
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.SelectModel(m))
					// The 2D Place tool adds atlas frames, not models, so a model turns it on only in 3D.
					if ed.ViewMode == ViewMode3D {
						ed.SetTool(ToolPlace)
					}
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "model-thumb-" + id,
						Style: core.Style{
							Width:        core.Pt(32),
							Height:       core.Pt(32),
							Background:   colors.Inset(),
							CornerRadius: 4,
						},
						Draw: func(d *core.Drawing, size core.Size) {
							_ = thumbRev.Get()
							_ = rev.Get()
							img, err := ed.ModelThumbnail(m)
							if err != nil || img == nil {
								return
							}
							sw, sh := float32(size.Width), float32(size.Height)
							side := min(sw, sh)
							px := float32(img.Bounds().Dx())
							d.DrawImageFrom(core.ImageSource{Pixels: img}, 0, 0, px, px, (sw-side)/2, (sh-side)/2, side, side)
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
						Children: []core.Node{
							core.Text{
								Content: m.Name(),
								Style:   core.Style{FontSize: 12, FontWeight: 600, Color: colors.Text()},
							},
							core.Text{
								Content: detail,
								Style:   core.Style{FontSize: 10, Color: colors.Muted()},
							},
						},
					},
				},
			}
			rows = append(rows, dragdrop.Source{
				View: row,
				Data: func() any { return m },
				Preview: func(any) core.Node {
					return core.Text{
						Content: m.Name(),
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
		return core.View{Style: core.Style{Gap: 6}, Children: rows}
	})
	return core.View{
		TestID: "model-list",
		Style:  core.Style{Gap: 6},
		Children: []core.Node{
			core.Text{
				Content: "Models",
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
			},
			list,
		},
	}
}

// place3DToolsView is the 3D toolbar's Place group: Select and Place, which places the model selected in the asset browser, snapping to the grid, aligning to the surface and the ground plane's height.
func place3DToolsView(rev *core.Signal[uint64], ed *Editor, notPlaying func() bool) core.Node {
	placing := func() bool {
		_ = rev.Get()
		return ed.Tool == ToolPlace
	}
	return ui.ToolbarGroup{
		Label: "Place",
		Children: []core.Node{
			ui.ToggleButton{TestID: "tool3d-select", Text: "Select", Disabled: notPlaying, On: func() bool {
				_ = rev.Get()
				return !placing() && !(ed.Tool == ToolPlugin && ed.activePluginToolSupports3D())
			}, OnChange: func(on bool) {
				if on {
					ed.SetTool(ToolSelect)
				}
			}},
			ui.ToggleButton{TestID: "tool3d-place", Text: "Place", Label: "Place the model selected in the asset browser", Disabled: notPlaying, On: placing, OnChange: func(on bool) {
				if on {
					ed.SetTool(ToolPlace)
				}
			}},
			ui.ToggleButton{TestID: "place3d-snap", Text: "Snap", Label: "Snap placed models to the grid", On: func() bool {
				_ = rev.Get()
				return ed.PlaceSnap3D
			}, OnChange: func(on bool) {
				ed.report(ed.SetPlaceOptions3D(ed.PlaceHeight3D, on, ed.PlaceAlign3D))
			}},
			ui.ToggleButton{TestID: "place3d-align", Text: "Align", Label: "Align placed models to the surface", On: func() bool {
				_ = rev.Get()
				return ed.PlaceAlign3D
			}, OnChange: func(on bool) {
				ed.report(ed.SetPlaceOptions3D(ed.PlaceHeight3D, ed.PlaceSnap3D, on))
			}},
			ui.NumberInput{
				TestID: "place3d-height",
				Label:  "Ground height",
				Step:   0.5,
				Style:  core.Style{Width: core.Pt(72)},
				Value: func() float64 {
					_ = rev.Get()
					return float64(ed.PlaceHeight3D)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetPlaceOptions3D(float32(v), ed.PlaceSnap3D, ed.PlaceAlign3D))
				},
			},
		},
	}
}

// modelField is the 3D inspector's Model field for objID: it picks one of the open project's glTF models (ProjectModels), searchable, and its clear button names none. It is disabled on an object that draws a built-in primitive, which SetObjectModel refuses.
func modelField(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool) core.Node {
	current := func() string {
		_ = rev.Get()
		if o, _ := ed.FindResolvedObject(objID); o != nil {
			return o.Model
		}
		return ""
	}
	return ui.Select[string]{
		TestID:      "inspector-model",
		Label:       "Model",
		Placeholder: "None",
		Searchable:  true,
		Style:       core.Style{Width: core.Pct(100)},
		Disabled: func() bool {
			if disabled() {
				return true
			}
			o, _ := ed.FindResolvedObject(objID)
			return o != nil && o.Mesh != ""
		},
		OptionsFunc: func() []ui.Option[string] {
			cur := current()
			models := ed.ProjectModels()
			opts := make([]ui.Option[string], 0, len(models)+1)
			// A model the project does not list, such as one named by hand that is not there, still shows as the object's.
			if cur != "" && !slices.Contains(models, cur) {
				opts = append(opts, ui.Option[string]{Value: cur, Label: cur})
			}
			for _, p := range models {
				opts = append(opts, ui.Option[string]{Value: p, Label: p})
			}
			return opts
		},
		Value:    current,
		OnChange: func(p string) { ed.report(ed.SetObjectModel(objID, p)) },
		OnClear:  func() { ed.report(ed.SetObjectModel(objID, "")) },
	}
}
