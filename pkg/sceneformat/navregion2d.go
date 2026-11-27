package sceneformat

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/pathfind/navbake"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/tilemap/tileshape"
	"github.com/nsgo-dev/nsgo/core"
)

// NavRegion2DComponentVersion is the schema version of the built-in 2D navigation region component (navregion2d).
const NavRegion2DComponentVersion = 1

// NavRegion2DComponentName is the built-in 2D navigation region polygon component key in scene YAML files.
const NavRegion2DComponentName = "navregion2d"

var navRegion2DModes = []string{"walkable", "obstacle"}

// IsNavRegion2DComponent reports whether name is the built-in 2D navigation region component type.
func IsNavRegion2DComponent(name string) bool {
	return name == NavRegion2DComponentName
}

// NavRegion2DComponent returns the ComponentType schema for the built-in "navregion2d" component.
func NavRegion2DComponent() ComponentType {
	return ComponentType{
		Name: NavRegion2DComponentName,
		Doc:  "2D navigation region polygon (walkable area or obstacle cutout) baked into BuiltScene.NavMesh2D via navbake.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: NavRegion2DComponentVersion, Min: Float64(NavRegion2DComponentVersion), Max: Float64(NavRegion2DComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "mode", Kind: FieldString, Default: "walkable", Enum: navRegion2DModes, Doc: `Region role: "walkable" (adds walkable area) or "obstacle" (cuts a hole out of walkable regions).`},
			{Name: "points", Kind: FieldPointList, Doc: "Local [x, y] vertices of the closed navigation polygon."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local horizontal offset from the object origin."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local vertical offset from the object origin."},
			{Name: "subtractTileCollision", Kind: FieldBool, Default: false, Doc: "When true, subtracts solid tile collision geometry from the scene's tilemaps when baking the 2D navigation mesh."},
			{Name: "tileLayer", Kind: FieldString, Default: "", Doc: "Scene layer whose tilemap provides subtracted collision (empty uses this object's layer or all visible world-space tilemap layers)."},
			{Name: "tileLayers", Kind: FieldStringList, Doc: "Optional tile layer names inside the tilemap whose solid collision is subtracted (empty uses all tile layers)."},
			{Name: "agentRadius", Kind: FieldFloat, Default: 0.0, Min: Float64(0), Step: 1, Doc: "Agent clearance radius in world units (>= 0) used to shrink walkable edges and inflate obstacles during baking."},
			{Name: "enabled", Kind: FieldBool, Default: true, Doc: "Includes this navigation region in the built 2D navigation mesh when true."},
		},
	}
}

// RegisterNavRegion2DComponent registers the built-in "navregion2d" component type on reg.
func RegisterNavRegion2DComponent(reg *ComponentRegistry) error {
	return reg.Register(NavRegion2DComponent())
}

// NavRegion2DComponentRegistry returns a new ComponentRegistry pre-populated with the built-in "navregion2d" component.
func NavRegion2DComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterNavRegion2DComponent(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinNavRegion2DReg = NavRegion2DComponentRegistry()

func resolveNavRegion2DComponent(objID string, raw any) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sceneformat: object %q component %q must be a mapping", objID, NavRegion2DComponentName)
	}
	res, err := builtinNavRegion2DReg.Resolve(NavRegion2DComponentName, m)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: object %q %w", objID, err)
	}
	return res, nil
}

// NavRegion2DDebug describes a resolved navregion2d component in local and world space for editor overlays and polygon handles.
type NavRegion2DDebug struct {
	Mode                  string
	Points                [][2]float64
	OffsetX               float32
	OffsetY               float32
	SubtractTileCollision bool
	TileLayer             string
	TileLayers            []string
	AgentRadius           float32
	Enabled               bool
	// WorldVertices are the transformed closed polygon vertices in world space.
	WorldVertices []core.Point
}

// ExtractNavRegion2D resolves a navregion2d component from o at world (or o's local transform when world is nil).
func ExtractNavRegion2D(o *ObjectDoc, world *math2d.Affine) (NavRegion2DDebug, bool, error) {
	if o == nil || len(o.Components) == 0 {
		return NavRegion2DDebug{}, false, nil
	}
	raw, ok := o.Components[NavRegion2DComponentName]
	if !ok {
		return NavRegion2DDebug{}, false, nil
	}
	res, err := resolveNavRegion2DComponent(o.ID, raw)
	if err != nil {
		return NavRegion2DDebug{}, false, err
	}
	m := o.LocalTransform()
	if world != nil {
		m = *world
	}
	mode := res["mode"].(string)
	pts, _ := res["points"].([][2]float64)
	ox := float32(res["offsetX"].(float64))
	oy := float32(res["offsetY"].(float64))
	if !isFinite64(float64(ox)) || !isFinite64(float64(oy)) {
		return NavRegion2DDebug{}, false, fmt.Errorf("sceneformat: object %q component %q offset overflows float32", o.ID, NavRegion2DComponentName)
	}
	subtract := res["subtractTileCollision"].(bool)
	tileLayer := res["tileLayer"].(string)
	tileLayers, _ := res["tileLayers"].([]string)
	agentRadius := float32(res["agentRadius"].(float64))
	if !isFinite64(float64(agentRadius)) {
		return NavRegion2DDebug{}, false, fmt.Errorf("sceneformat: object %q component %q agentRadius overflows float32", o.ID, NavRegion2DComponentName)
	}
	enabled := res["enabled"].(bool)

	dbg := NavRegion2DDebug{
		Mode:                  mode,
		Points:                slices.Clone(pts),
		OffsetX:               ox,
		OffsetY:               oy,
		SubtractTileCollision: subtract,
		TileLayer:             tileLayer,
		TileLayers:            slices.Clone(tileLayers),
		AgentRadius:           agentRadius,
		Enabled:               enabled,
	}
	if len(pts) > 0 {
		dbg.WorldVertices = make([]core.Point, len(pts))
		for i, lp := range pts {
			lx, ly := float32(lp[0]), float32(lp[1])
			if !isFinite64(float64(lx)) || !isFinite64(float64(ly)) {
				return NavRegion2DDebug{}, false, fmt.Errorf("sceneformat: object %q component %q point %d overflows float32", o.ID, NavRegion2DComponentName, i)
			}
			px, py := ox+lx, oy+ly
			if !isFinite64(float64(px)) || !isFinite64(float64(py)) {
				return NavRegion2DDebug{}, false, fmt.Errorf("sceneformat: object %q component %q point %d with offset overflows float32", o.ID, NavRegion2DComponentName, i)
			}
			wp := m.TransformPoint(math2d.Vec2{X: px, Y: py})
			if !isFinite64(float64(wp.X)) || !isFinite64(float64(wp.Y)) {
				return NavRegion2DDebug{}, false, fmt.Errorf("sceneformat: object %q component %q world point %d overflows float32", o.ID, NavRegion2DComponentName, i)
			}
			dbg.WorldVertices[i] = core.Point{X: core.Points(wp.X), Y: core.Points(wp.Y)}
		}
	}
	return dbg, true, nil
}

// BuildNavMesh2D constructs a baked *navbake.Mesh and runtime *pathfind.NavMesh from all built-in navregion2d components on visible world-space layers of scene, optionally subtracting solid tilemap collision geometry and attaching any authored 2D nav_link components.
//
// It returns (nil, nil, nil) when the scene declares no navregion2d components.
// When navregion2d components exist but no enabled walkable polygon has at least 3 vertices, it returns an empty *navbake.Mesh and empty *pathfind.NavMesh without error.
func BuildNavMesh2D(scene *BuiltScene) (*navbake.Mesh, *pathfind.NavMesh, error) {
	if scene == nil {
		return nil, nil, nil
	}
	var src navbake.Source
	var maxRadius float32
	hasAny := false

	type tileSubKey struct {
		layer      *BuiltLayer
		tileLayers string
	}
	seenTileSubs := make(map[tileSubKey]bool)
	type pendingLink struct {
		objID string
		raw   any
		world math2d.Affine
	}
	var linkObjects []pendingLink

	var walk func(layer *BuiltLayer, objs []*BuiltObject) error
	walk = func(layer *BuiltLayer, objs []*BuiltObject) error {
		for _, bo := range objs {
			if bo == nil || bo.Doc == nil {
				continue
			}
			if _, hasNav := bo.Doc.Components[NavRegion2DComponentName]; hasNav {
				reg, ok, err := ExtractNavRegion2D(bo.Doc, &bo.World)
				if err != nil {
					return err
				}
				if ok {
					hasAny = true
					if reg.Enabled {
						if reg.AgentRadius > maxRadius {
							maxRadius = reg.AgentRadius
						}
						if len(reg.WorldVertices) >= 3 {
							poly := normalizeNavPolyWinding(reg.WorldVertices)
							if len(poly) >= 3 {
								if reg.Mode == "obstacle" {
									src.Obstacles = append(src.Obstacles, poly)
								} else {
									src.Outlines = append(src.Outlines, poly)
								}
							}
						}
						if reg.SubtractTileCollision {
							targets := resolveTilemapTargets(scene, layer, reg.TileLayer)
							sortedNames := slices.Clone(reg.TileLayers)
							slices.Sort(sortedNames)
							keyNames := strings.Join(sortedNames, "\x00")
							for _, target := range targets {
								k := tileSubKey{layer: target, tileLayers: keyNames}
								if seenTileSubs[k] {
									continue
								}
								seenTileSubs[k] = true
								src.Obstacles = appendTilemapNavObstacles(src.Obstacles, target.Map, reg.TileLayers)
							}
						}
					}
				}
			}
			if rawLink, hasLink := bo.Doc.Components[NavLinkComponentName]; hasLink {
				linkObjects = append(linkObjects, pendingLink{objID: bo.Doc.ID, raw: rawLink, world: bo.World})
			}
			if len(bo.Children) > 0 {
				if err := walk(layer, bo.Children); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, layer := range scene.Layers {
		if !layer.Visible || layer.Space != SpaceWorld {
			continue
		}
		if err := walk(layer, layer.Objects); err != nil {
			return nil, nil, err
		}
	}

	if !hasAny {
		return nil, nil, nil
	}
	if len(src.Outlines) == 0 {
		empty := &navbake.Mesh{
			Version: navbake.FormatVersion,
			Agent:   "default",
			Radius:  maxRadius,
		}
		return empty, pathfind.BuildNavMesh(nil), nil
	}
	for _, pl := range linkObjects {
		d, err := decodeNavLink(pl.objID, pl.raw)
		if err != nil {
			return nil, nil, err
		}
		start := pl.world.TransformPoint(math2d.V2(d.startX, d.startY))
		end := pl.world.TransformPoint(math2d.V2(d.endX, d.endY))
		ud, _ := d.userData.(string)
		src.Links = append(src.Links, navbake.OffMeshLink{
			ID:            d.id,
			Start:         navbake.Point{start.X, start.Y},
			End:           navbake.Point{end.X, end.Y},
			Radius:        d.radius,
			Bidirectional: d.bidirectional,
			Cost:          d.cost,
			Area:          d.area,
			Flags:         d.flags,
			UserData:      ud,
		})
	}
	mesh, err := navbake.Bake(src, navbake.Agent{Name: "default", Radius: maxRadius}, navbake.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("sceneformat: build 2D navmesh: %w", err)
	}
	return mesh, mesh.NavMesh(), nil
}

// SyncNavMesh2D rebuilds s.NavBake2D and s.NavMesh2D from the scene's current object transforms, navregion2d components, and tilemaps.
func (s *BuiltScene) SyncNavMesh2D() error {
	if s == nil {
		return nil
	}
	bake, nav, err := BuildNavMesh2D(s)
	if err != nil {
		return err
	}
	s.NavBake2D = bake
	s.NavMesh2D = nav
	return nil
}

// normalizeNavPolyWinding returns a copy of pts wound with positive signed area so overlapping walkable regions always union (and overlapping obstacles always union) under navbake's nonzero winding rule.
func normalizeNavPolyWinding(pts []core.Point) []core.Point {
	if len(pts) < 3 {
		return nil
	}
	out := slices.Clone(pts)
	var area2 float64
	for i := range out {
		a := out[i]
		b := out[(i+1)%len(out)]
		p1 := float64(float64(a.X) * float64(b.Y))
		p2 := float64(float64(b.X) * float64(a.Y))
		area2 += p1 - p2
	}
	if math.Abs(area2) <= 1e-9 || math.IsNaN(area2) || math.IsInf(area2, 0) {
		return nil
	}
	if area2 < 0 {
		slices.Reverse(out)
	}
	return out
}

func resolveTilemapTargets(scene *BuiltScene, currentLayer *BuiltLayer, layerName string) []*BuiltLayer {
	if layerName != "" {
		if target := scene.Layer(layerName); target != nil && target.Map != nil {
			return []*BuiltLayer{target}
		}
		return nil
	}
	if currentLayer != nil && currentLayer.Map != nil {
		return []*BuiltLayer{currentLayer}
	}
	var out []*BuiltLayer
	for _, l := range scene.Layers {
		if l.Visible && l.Space == SpaceWorld && l.Map != nil {
			out = append(out, l)
		}
	}
	return out
}

func appendTilemapNavObstacles(dst [][]core.Point, m *tilemap.Tilemap, tileLayers []string) [][]core.Point {
	if m == nil || m.Width <= 0 || m.Height <= 0 || !(m.TileWidth > 0) || !(m.TileHeight > 0) {
		return dst
	}
	if m.Orientation == tilemap.OrientationOrthogonal {
		v := tileshape.New(m, tileshape.Options{LayerNames: tileLayers})
		for _, box := range v.Boxes() {
			if box.W <= 0 || box.H <= 0 {
				continue
			}
			x0, y0 := core.Points(box.X), core.Points(box.Y)
			x1, y1 := core.Points(box.X+box.W), core.Points(box.Y+box.H)
			dst = append(dst, []core.Point{
				{X: x0, Y: y0},
				{X: x1, Y: y0},
				{X: x1, Y: y1},
				{X: x0, Y: y1},
			})
		}
		// Append any non-rectangular closed solid tile shapes (such as slope triangles or custom polygons).
		if tilemapHasShapes(m) {
			dst = appendNonRectTileShapes(dst, m, tileLayers)
		}
		return dst
	}

	// Non-orthogonal orientations (isometric, staggered, hexagonal): collect solid cell polygons honoring m.Origin and tileLayers.
	layerIndices := selectedTileLayerIndices(m, tileLayers)
	for row := range m.Height {
		for col := range m.Width {
			if !cellIsSolidInLayers(m, layerIndices, col, row) {
				continue
			}
			if poly := normalizeNavPolyWinding(tileCellPolygon(m, col, row)); len(poly) >= 3 {
				dst = append(dst, poly)
			}
		}
	}
	return dst
}

func tilemapHasShapes(m *tilemap.Tilemap) bool {
	for _, ts := range m.Tilesets {
		if ts != nil && len(ts.Shapes) > 0 {
			return true
		}
	}
	return false
}

func selectedTileLayerIndices(m *tilemap.Tilemap, tileLayers []string) []int {
	if len(tileLayers) == 0 {
		out := make([]int, len(m.Layers))
		for i := range m.Layers {
			out[i] = i
		}
		return out
	}
	var out []int
	seen := make(map[int]bool)
	for _, name := range tileLayers {
		for i := range m.Layers {
			if m.Layers[i].Name == name && !seen[i] {
				seen[i] = true
				out = append(out, i)
			}
		}
	}
	return out
}

func cellIsSolidInLayers(m *tilemap.Tilemap, layerIndices []int, col, row int) bool {
	if f, ok := m.CellFlags(col, row); ok {
		return f&tilemap.TileSolid != 0
	}
	for _, li := range layerIndices {
		gid := m.TileAt(li, col, row)
		if gid&tilemap.TileIDMask == 0 {
			continue
		}
		if m.Flags(gid)&tilemap.TileSolid != 0 {
			return true
		}
	}
	return false
}

func appendNonRectTileShapes(dst [][]core.Point, m *tilemap.Tilemap, tileLayers []string) [][]core.Point {
	layerIndices := selectedTileLayerIndices(m, tileLayers)
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	ox, oy := float64(m.Origin.X), float64(m.Origin.Y)
	for row := range m.Height {
		for col := range m.Width {
			if f, ok := m.CellFlags(col, row); ok {
				if f&tilemap.TileSolid != 0 {
					continue // already included in v.Boxes()
				}
				continue
			}
			for _, li := range layerIndices {
				gid := m.TileAt(li, col, row)
				if gid&tilemap.TileIDMask == 0 {
					continue
				}
				f := m.Flags(gid)
				if f&tilemap.TileOneWay != 0 && f&tilemap.TileSolid == 0 {
					continue
				}
				shapes := m.TileShapes(gid)
				if len(shapes) == 0 {
					continue
				}
				_, tileH := m.TileBox(gid)
				baseX := ox + float64(col)*tw
				baseY := oy + float64(row)*th + (th - float64(tileH))
				for _, s := range shapes {
					if !s.Closed() || len(s.Points) < 3 {
						continue
					}
					pts := make([]core.Point, 0, len(s.Points))
					valid := true
					for _, p := range s.Points {
						x, y := baseX+float64(p.X), baseY+float64(p.Y)
						if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
							valid = false
							break
						}
						pts = append(pts, core.Point{X: core.Points(x), Y: core.Points(y)})
					}
					if !valid || isAxisAlignedRectPoints(pts) {
						continue
					}
					if poly := normalizeNavPolyWinding(pts); len(poly) >= 3 {
						dst = append(dst, poly)
					}
				}
			}
		}
	}
	return dst
}

func isAxisAlignedRectPoints(pts []core.Point) bool {
	if len(pts) != 4 {
		return false
	}
	for i := range 4 {
		a, b := pts[i], pts[(i+1)%4]
		if math.Abs(float64(a.X-b.X)) > 1e-5 && math.Abs(float64(a.Y-b.Y)) > 1e-5 {
			return false
		}
	}
	return true
}

func tileCellPolygon(m *tilemap.Tilemap, col, row int) []core.Point {
	ox, oy := float64(m.Origin.X), float64(m.Origin.Y)
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	pt := func(x, y float64) core.Point {
		return core.Point{X: core.Points(ox + x), Y: core.Points(oy + y)}
	}
	switch m.Orientation {
	case tilemap.OrientationOrthogonal:
		x0, y0 := float64(col)*tw, float64(row)*th
		return []core.Point{pt(x0, y0), pt(x0+tw, y0), pt(x0+tw, y0+th), pt(x0, y0+th)}
	case tilemap.OrientationIsometric:
		lat := func(c, r int) core.Point {
			return pt(float64(c-r)*tw/2+tw/2, float64(c+r)*th/2)
		}
		return []core.Point{lat(col, row), lat(col+1, row), lat(col+1, row+1), lat(col, row+1)}
	case tilemap.OrientationHexagonal:
		o := m.TileOrigin(col, row)
		x, y := float64(o.X)-ox, float64(o.Y)-oy
		if m.StaggerAxis == tilemap.StaggerX {
			s := float64(m.HexSideLength)
			if s <= 0 {
				s = tw / 2
			}
			return []core.Point{
				pt(x+(tw-s)/2, y), pt(x+(tw+s)/2, y), pt(x+tw, y+th/2),
				pt(x+(tw+s)/2, y+th), pt(x+(tw-s)/2, y+th), pt(x, y+th/2),
			}
		}
		s := float64(m.HexSideLength)
		if s <= 0 {
			s = th / 2
		}
		return []core.Point{
			pt(x+tw/2, y), pt(x+tw, y+(th-s)/2), pt(x+tw, y+(th+s)/2),
			pt(x+tw/2, y+th), pt(x, y+(th+s)/2), pt(x, y+(th-s)/2),
		}
	default:
		o := m.TileOrigin(col, row)
		x, y := float64(o.X)-ox, float64(o.Y)-oy
		return []core.Point{pt(x+tw/2, y), pt(x+tw, y+th/2), pt(x+tw/2, y+th), pt(x, y+th/2)}
	}
}
