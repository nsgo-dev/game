package editor

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// GridMapPalette is what the 3D GridMap palette shows for the active GridMap:
// every item in its mesh library with bounds/geometry for its thumbnail, plus
// the current floor height and orientation, or a Message explaining why none
// are available.
type GridMapPalette struct {
	// Path is the open GridMap's path (or name).
	Path string
	// MeshLibrary is the glTF mesh library path referenced by the GridMap.
	MeshLibrary string
	// Floor is the active integer floor height (Y coordinate).
	Floor int
	// Orientation is the active orientation index in 0..23.
	Orientation uint8
	// Items are the mesh library's items in ascending ID order.
	Items []GridMapPaletteItem
	// Message explains why Items is empty, or is empty when Items is non-empty.
	Message string
}

// GridMapPaletteItem is one selectable mesh tile in a GridMapPalette.
type GridMapPaletteItem struct {
	// ID is the MeshLibrary tile ID written into GridMap cells.
	ID uint16
	// Name is the tile's name in the MeshLibrary and YAML file.
	Name string
	// MeshID is the glTF mesh index in the mesh library.
	MeshID uint32
	// LocalBounds is the tile's local axis-aligned bounding box.
	LocalBounds math3d.AABB
	// Thumbnail holds pre-extracted isometric 3D triangles for rendering the item's thumbnail.
	Thumbnail []gridMapThumbTri
}

// Has reports whether p contains an item with the given tile ID.
func (p GridMapPalette) Has(id uint16) bool {
	return slices.ContainsFunc(p.Items, func(it GridMapPaletteItem) bool { return it.ID == id })
}

// GridMapPalette returns the palette of the active editable GridMap's mesh library.
func (e *Editor) GridMapPalette() GridMapPalette {
	p := GridMapPalette{
		Path:        e.GridMapPath(),
		MeshLibrary: e.GridMapMeshLibrary(),
		Floor:       e.GridMapFloor(),
		Orientation: e.GridMapOrientation(),
	}
	if e == nil || e.gridMap.grid == nil {
		if e != nil && e.gridMap.loadErr != nil {
			p.Message = e.gridMap.loadErr.Error()
		} else {
			p.Message = "Open a .gridmap.yaml file or select an object with a GridMap to paint"
		}
		return p
	}
	if e.gridMap.grid.Library == nil {
		p.Message = "GridMap has no mesh library"
		return p
	}
	defs := e.gridMap.grid.Library.Items()
	if len(defs) == 0 {
		p.Message = fmt.Sprintf("Mesh library %q has no items", e.gridMap.meshLibrary)
		return p
	}
	p.Items = make([]GridMapPaletteItem, 0, len(defs))
	for _, def := range defs {
		name := def.Name
		if name == "" {
			name = fmt.Sprintf("tile_%d", def.ID)
		}
		p.Items = append(p.Items, GridMapPaletteItem{
			ID:          def.ID,
			Name:        name,
			MeshID:      def.MeshID,
			LocalBounds: def.LocalBounds,
			Thumbnail:   buildGridMapThumbnail(e.gridMap.gltfDoc, def),
		})
	}
	return p
}

// gridMapThumbTri is one flat-shaded triangle in local normalized [-1, 1] thumbnail space.
type gridMapThumbTri struct {
	a, b, c math3d.Vec3
	color   core.Color
}

func buildGridMapThumbnail(doc *gltf.Document, def world3d.MeshTileDef) []gridMapThumbTri {
	if doc != nil && int(def.MeshID) < len(doc.Meshes) {
		if tris := extractGLTFThumbTris(doc, int(def.MeshID), def.ID); len(tris) > 0 {
			return tris
		}
	}
	return boxThumbTris(def.LocalBounds, def.ID)
}

func rgbColor(r, g, b float64) core.Color {
	clamp := func(v float64) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(math.Round(v * 255))
	}
	return colorFromRGBA8(clamp(r), clamp(g), clamp(b), 255)
}

func extractGLTFThumbTris(doc *gltf.Document, meshIdx int, tileID uint16) []gridMapThumbTri {
	mesh := &doc.Meshes[meshIdx]
	bounds := math3d.AABBEmpty()
	var raw [][3]math3d.Vec3
	var colors []core.Color
	const maxThumbTris = 160

	for pi := range mesh.Primitives {
		prim := &mesh.Primitives[pi]
		if len(prim.Positions) < 3 {
			continue
		}
		baseCol := gridMapTileColor(tileID, 1)
		if prim.Material >= 0 && prim.Material < len(doc.Materials) {
			bc := doc.Materials[prim.Material].BaseColorFactor
			if bc.X+bc.Y+bc.Z > 0.05 {
				baseCol = rgbColor(float64(bc.X), float64(bc.Y), float64(bc.Z))
			}
		}
		for _, pos := range prim.Positions {
			bounds = bounds.ExpandPoint(pos)
		}
		triCount := len(prim.Indices) / 3
		indexed := len(prim.Indices) >= 3
		if !indexed {
			triCount = len(prim.Positions) / 3
		}
		step := 1
		if triCount > maxThumbTris {
			step = (triCount + maxThumbTris - 1) / maxThumbTris
		}
		for t := 0; t < triCount; t += step {
			var p0, p1, p2 math3d.Vec3
			if indexed {
				i0, i1, i2 := prim.Indices[t*3], prim.Indices[t*3+1], prim.Indices[t*3+2]
				if int(i0) >= len(prim.Positions) || int(i1) >= len(prim.Positions) || int(i2) >= len(prim.Positions) {
					continue
				}
				p0, p1, p2 = prim.Positions[i0], prim.Positions[i1], prim.Positions[i2]
			} else {
				p0, p1, p2 = prim.Positions[t*3], prim.Positions[t*3+1], prim.Positions[t*3+2]
			}
			raw = append(raw, [3]math3d.Vec3{p0, p1, p2})
			colors = append(colors, baseCol)
		}
	}
	if len(raw) == 0 || bounds.Empty() {
		return nil
	}
	center := bounds.Center()
	size := bounds.Size()
	extent := max(size.X, max(size.Y, size.Z))
	if extent < 1e-4 {
		extent = 1
	}
	inv := float32(1.4 / float64(extent))
	out := make([]gridMapThumbTri, len(raw))
	for i, tri := range raw {
		out[i] = gridMapThumbTri{
			a:     tri[0].Sub(center).Scale(inv),
			b:     tri[1].Sub(center).Scale(inv),
			c:     tri[2].Sub(center).Scale(inv),
			color: colors[i],
		}
	}
	return out
}

func boxThumbTris(bounds math3d.AABB, tileID uint16) []gridMapThumbTri {
	hx, hy, hz := float32(0.65), float32(0.65), float32(0.65)
	if !bounds.Empty() {
		s := bounds.Size()
		extent := max(s.X, max(s.Y, s.Z))
		if extent > 1e-4 {
			scale := float32(1.3 / float64(extent))
			hx = max(s.X*0.5*scale, 0.14)
			hy = max(s.Y*0.5*scale, 0.14)
			hz = max(s.Z*0.5*scale, 0.14)
		}
	}
	col := gridMapTileColor(tileID, 1)
	v := [8]math3d.Vec3{
		{X: -hx, Y: -hy, Z: -hz},
		{X: +hx, Y: -hy, Z: -hz},
		{X: +hx, Y: +hy, Z: -hz},
		{X: -hx, Y: +hy, Z: -hz},
		{X: -hx, Y: -hy, Z: +hz},
		{X: +hx, Y: -hy, Z: +hz},
		{X: +hx, Y: +hy, Z: +hz},
		{X: -hx, Y: +hy, Z: +hz},
	}
	quads := [6][4]int{
		{4, 5, 6, 7}, // +Z
		{1, 0, 3, 2}, // -Z
		{5, 1, 2, 6}, // +X
		{0, 4, 7, 3}, // -X
		{3, 7, 6, 2}, // +Y
		{0, 1, 5, 4}, // -Y
	}
	out := make([]gridMapThumbTri, 0, 12)
	for _, q := range quads {
		out = append(out,
			gridMapThumbTri{a: v[q[0]], b: v[q[1]], c: v[q[2]], color: col},
			gridMapThumbTri{a: v[q[0]], b: v[q[2]], c: v[q[3]], color: col},
		)
	}
	return out
}

// drawGridMapThumbnail renders an isometric 3D shaded thumbnail of item rotated by orient onto d.
func drawGridMapThumbnail(d *core.Drawing, item GridMapPaletteItem, orient uint8, w, h float32) {
	if d == nil || w <= 0 || h <= 0 || len(item.Thumbnail) == 0 {
		return
	}
	rot := world3d.GridOrientationMat4(orient)
	// Fixed isometric camera basis looking toward origin from (+1, +0.9, +1).
	yaw := math.Pi / 4
	pitch := 0.58
	cy, sy := math.Cos(yaw), math.Sin(yaw)
	cp, sp := math.Cos(pitch), math.Sin(pitch)
	// Camera right, up, forward (toward viewer).
	right := math3d.Vec3{X: float32(cy), Y: 0, Z: float32(-sy)}
	fwd := math3d.Vec3{X: float32(sy * cp), Y: float32(sp), Z: float32(cy * cp)}
	up := fwd.Cross(right).Norm()
	light := math3d.Vec3{X: 0.45, Y: 0.8, Z: 0.38}.Norm()

	scale := min(w, h) * 0.42
	cx, cyPt := w*0.5, h*0.5

	type projTri struct {
		x0, y0, x1, y1, x2, y2 float32
		depth                  float32
		fill                   core.Color
	}
	projected := make([]projTri, 0, len(item.Thumbnail))
	project := func(p math3d.Vec3) (float32, float32, float32) {
		rp := rot.TransformDir(p)
		px := cx + rp.Dot(right)*scale
		py := cyPt - rp.Dot(up)*scale
		pz := rp.Dot(fwd)
		return px, py, pz
	}
	for _, tri := range item.Thumbnail {
		ra := rot.TransformDir(tri.a)
		rb := rot.TransformDir(tri.b)
		rc := rot.TransformDir(tri.c)
		n := rb.Sub(ra).Cross(rc.Sub(ra))
		if n.LenSq() < 1e-10 {
			continue
		}
		n = n.Norm()
		if n.Dot(fwd) <= 0 {
			continue
		}
		x0, y0, z0 := project(tri.a)
		x1, y1, z1 := project(tri.b)
		x2, y2, z2 := project(tri.c)
		ndotl := float64(max(n.Dot(light), 0))
		shade := 0.35 + 0.65*ndotl
		r, g, b, _ := tri.color.Components()
		fill := rgbColor(r*shade, g*shade, b*shade)
		projected = append(projected, projTri{
			x0: x0, y0: y0, x1: x1, y1: y1, x2: x2, y2: y2,
			depth: (z0 + z1 + z2) / 3,
			fill:  fill,
		})
	}
	slices.SortFunc(projected, func(a, b projTri) int {
		return cmp.Compare(a.depth, b.depth)
	})
	d.Save()
	for _, pt := range projected {
		d.SetFillColor(pt.fill).BeginPath().
			MoveTo(pt.x0, pt.y0).
			LineTo(pt.x1, pt.y1).
			LineTo(pt.x2, pt.y2).
			ClosePath().Fill()
	}
	// Subtle wireframe accent on top of shaded thumbnail for crisp legibility at small sizes.
	d.SetStrokeColor(specAlpha(darkSpecPalette.Bg, 0.35)).SetLineWidth(0.75)
	for _, pt := range projected {
		d.BeginPath().MoveTo(pt.x0, pt.y0).LineTo(pt.x1, pt.y1).LineTo(pt.x2, pt.y2).ClosePath().Stroke()
	}
	d.Restore()
}

func sameGridMapPalette(a, b GridMapPalette) bool {
	if a.Path != b.Path || a.MeshLibrary != b.MeshLibrary || a.Message != b.Message || len(a.Items) != len(b.Items) {
		return false
	}
	for i := range a.Items {
		if a.Items[i].ID != b.Items[i].ID || a.Items[i].Name != b.Items[i].Name || a.Items[i].MeshID != b.Items[i].MeshID || a.Items[i].LocalBounds != b.Items[i].LocalBounds || len(a.Items[i].Thumbnail) != len(b.Items[i].Thumbnail) {
			return false
		}
	}
	return true
}

const gridMapThumbSize = 54

// GridMapPaletteView renders the 3D GridMap item palette panel right of the viewport while a GridMap tool
// is active in 3D view: floor height controls, orientation controls (24 orientations), and a grid of the
// mesh library's items with 3D thumbnails.
func GridMapPaletteView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	shown := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ViewMode == ViewMode3D && ed.GridMapTool() != GridMapToolNone
	})
	return core.Show{
		When: shown.Get,
		Then: func(cx *core.Scope) core.Node {
			return gridMapPalettePanel(cx, ed, rev)
		},
	}
}

func gridMapPalettePanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	palette := core.NewMemoFunc(cx, func() GridMapPalette {
		_ = rev.Get()
		return ed.GridMapPalette()
	}, sameGridMapPalette)
	active := core.NewMemo(cx, func() uint16 {
		_ = rev.Get()
		return ed.ActiveGridMapTile()
	})
	orient := core.NewMemo(cx, func() uint8 {
		_ = rev.Get()
		return ed.GridMapOrientation()
	})
	notPlaying := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	grid := core.Dynamic(func(cx *core.Scope) core.Node {
		p := palette.Get()
		if len(p.Items) == 0 {
			return core.Text{
				TestID:  "gridmap-palette-empty",
				Content: p.Message,
				Style:   core.Style{FontSize: 12, Color: colors.Muted()},
			}
		}
		nodes := make([]core.Node, 0, len(p.Items))
		for _, it := range p.Items {
			item := it
			nodes = append(nodes, base.Radio{
				TestID: "gridmap-palette-" + item.Name,
				Label:  item.Name,
				Checked: func() bool {
					return active.Get() == item.ID
				},
				OnChange: func(checked bool) {
					if checked {
						ed.SetActiveGridMapTile(item.ID)
					}
				},
				StyleFunc: func() core.Style {
					ring := colors.Inset()
					if active.Get() == item.ID {
						ring = colors.Accent()
					}
					return core.Style{
						Padding:      core.All(3),
						BorderWidth:  paletteBorder,
						BorderColor:  ring,
						CornerRadius: 4,
						Gap:          2,
						AlignItems:   core.AlignCenter,
						Width:        core.Pt(gridMapThumbSize + 14),
						FocusVisible: ui.FocusRing(cx, 2),
					}
				},
				Children: []core.Node{
					core.Canvas{
						TestID: "gridmap-palette-thumb-" + item.Name,
						Style: core.Style{
							Width:      core.Pt(gridMapThumbSize),
							Height:     core.Pt(gridMapThumbSize),
							Background: colors.Surface(),
						},
						Draw: func(d *core.Drawing, size core.Size) {
							o := orient.Get()
							drawGridMapThumbnail(d, item, o, float32(size.Width), float32(size.Height))
						},
					},
					core.Text{
						Content: item.Name,
						Style: core.Style{
							FontSize: 10,
							Color:    colors.Text(),
						},
					},
				},
			})
		}
		return base.RadioGroup{
			TestID:   "gridmap-palette-grid",
			Label:    fmt.Sprintf("GridMap items of %s", p.MeshLibrary),
			Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4},
			Children: nodes,
		}
	})

	return core.View{
		TestID: "gridmap-palette",
		Style: core.Style{
			Width:       core.Pt(paletteWidth),
			Background:  colors.Inset(),
			Borders:     core.Edges{Left: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(palettePadding),
			Gap:         8,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.Text{
						Content: "GridMap",
						Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
					},
					core.Text{
						TestID: "gridmap-palette-library",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.gridMapLabel()
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
				},
			},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.Text{
						TestID: "gridmap-floor-label",
						ContentFunc: func() string {
							_ = rev.Get()
							return fmt.Sprintf("Floor Y: %d", ed.GridMapFloor())
						},
						Style: core.Style{FontSize: 11, Color: colors.Text(), FlexGrow: 1},
					},
					ui.Button{
						TestID:   "gridmap-floor-down",
						Text:     "-",
						Label:    "Lower floor height ([)",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: notPlaying,
						OnPress:  func() { ed.StepGridMapFloor(-1) },
					},
					ui.Button{
						TestID:   "gridmap-floor-up",
						Text:     "+",
						Label:    "Raise floor height (])",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: notPlaying,
						OnPress:  func() { ed.StepGridMapFloor(1) },
					},
				},
			},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					core.Text{
						TestID: "gridmap-orient-label",
						ContentFunc: func() string {
							_ = rev.Get()
							return fmt.Sprintf("Orient: %d/24", ed.GridMapOrientation())
						},
						Style: core.Style{FontSize: 11, Color: colors.Text(), FlexGrow: 1},
					},
					ui.Button{
						TestID:   "gridmap-rot-x",
						Text:     "X",
						Label:    "Rotate item around X (X)",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: notPlaying,
						OnPress:  func() { ed.report(ed.RotateGridMapOrientation(world3d.GridAxisX, 1)) },
					},
					ui.Button{
						TestID:   "gridmap-rot-y",
						Text:     "Y",
						Label:    "Rotate item around Y (Y)",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: notPlaying,
						OnPress:  func() { ed.report(ed.RotateGridMapOrientation(world3d.GridAxisY, 1)) },
					},
					ui.Button{
						TestID:   "gridmap-rot-z",
						Text:     "Z",
						Label:    "Rotate item around Z (Z)",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: notPlaying,
						OnPress:  func() { ed.report(ed.RotateGridMapOrientation(world3d.GridAxisZ, 1)) },
					},
				},
			},
			ui.ScrollView{
				Style:    core.Style{FlexGrow: 1},
				Children: []core.Node{grid},
			},
		},
	}
}
