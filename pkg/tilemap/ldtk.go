package tilemap

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// maxLDtkLayerTiles caps a single LDtk layer grid at 16,777,216 cells (64 MiB of uint32 GIDs), matching Tiled's maxTiledLayerTiles.
	maxLDtkLayerTiles = maxTiledLayerTiles
	// maxLDtkTotalTiles caps the total dense tile cells allocated across one LoadLDtk or LoadLDtkAll call (64 Mi cells, 256 MiB of uint32 GIDs).
	maxLDtkTotalTiles = 64 << 20
)

// LevelNeighbour describes one adjacent or overlapping level in a multi-level world (such as an LDtk project).
type LevelNeighbour struct {
	// Identifier is the neighbour level's identifier, resolved from the world's level list when available.
	Identifier string
	// IID is the neighbour level's LDtk instance ID (levelIid), when present.
	IID string
	// UID is the neighbour level's numeric LDtk UID (levelUid), when present.
	UID int
	// Dir is the direction from this level to the neighbour:
	// "n", "s", "e", "w", "ne", "nw", "se", "sw", "o" (overlapping), "<" (lower depth), or ">" (higher depth).
	Dir string
}

// LDtkOptions configures LoadLDtkWith, LoadLDtkAllWith, and LoadLDtkWorldWith.
type LDtkOptions struct {
	// ResolveImage maps an LDtk tileset relative path into an ImageSource.
	// When nil, core.ImageSource{URI: relPath} is used.
	ResolveImage func(relPath string) core.ImageSource
	// IntGridFlags maps LDtk IntGrid value identifiers or IntGrid value group identifiers
	// (case-insensitive) to TileFlags, overriding the built-in heuristic.
	// Mapping a name to 0 explicitly marks matching IntGrid cells non-colliding.
	IntGridFlags map[string]TileFlags
	// IntGridValueFlags maps positive numeric LDtk IntGrid values (1, 2, ...) directly to TileFlags,
	// taking precedence over IntGridFlags and the built-in heuristic.
	// Mapping a value to 0 explicitly marks matching IntGrid cells non-colliding.
	IntGridValueFlags map[int]TileFlags
}

// LoadLDtk parses an LDtk JSON project and builds a Tilemap for the level matching levelIdentifier (or the first level when levelIdentifier is empty).
//
// Tiles (gridTiles), AutoLayer (autoLayerTiles), IntGrid (intGridCsv mapped to TileSolid/TileFlags plus optional autoLayerTiles), and Entities (entityInstances mapped to ObjectLayer with fieldInstances properties) are supported, including LDtk flip bits (bit 0 = FlipHorizontal, bit 1 = FlipVertical).
// For Free and GridVania layouts, the returned Tilemap's Origin is set from the level's (worldX, worldY) and its Neighbours are populated from __neighbours.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func LoadLDtk(data []byte, levelIdentifier string, resolveImage func(relPath string) core.ImageSource) (*Tilemap, error) {
	return LoadLDtkWith(data, levelIdentifier, LDtkOptions{ResolveImage: resolveImage})
}

// LoadLDtkWith parses an LDtk JSON project with opts (including custom IntGrid-to-TileFlags mappings, such as those produced by project.File.LDtkOptions)
// and builds a Tilemap for the level matching levelIdentifier (or the first level when levelIdentifier is empty).
func LoadLDtkWith(data []byte, levelIdentifier string, opts LDtkOptions) (*Tilemap, error) {
	m, err := loadLDtk(data, levelIdentifier, opts)
	return m, loaderr.LocateJSON(err, "", data, fixLDtk)
}

// fixLDtk is the fix for an LDtk project error that has no more specific one.
const fixLDtk = "correct the project in LDtk and save it again"

func loadLDtk(data []byte, levelIdentifier string, opts LDtkOptions) (*Tilemap, error) {
	var proj ldtkProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, err
	}
	if len(proj.Levels) > 0 {
		index := 0
		if levelIdentifier != "" {
			index = -1
			for i := range proj.Levels {
				if proj.Levels[i].Identifier == levelIdentifier {
					index = i
					break
				}
			}
			if index < 0 {
				return nil, loaderr.Errorf("levels", "pass the identifier of a level in the project, or an empty one for the first level", "level %q not found", levelIdentifier)
			}
		}
		var totalTiles int
		return buildLDtkLevel(&proj, proj.Levels, proj.WorldLayout, "levels", index, opts, &totalTiles)
	}
	if len(proj.Worlds) > 0 {
		for wi := range proj.Worlds {
			w := &proj.Worlds[wi]
			for li := range w.Levels {
				if levelIdentifier == "" || w.Levels[li].Identifier == levelIdentifier {
					var totalTiles int
					return buildLDtkLevel(&proj, w.Levels, w.WorldLayout, loaderr.Path("worlds", wi, "levels"), li, opts, &totalTiles)
				}
			}
		}
		if levelIdentifier != "" {
			return nil, loaderr.Errorf("worlds", "pass the identifier of a level in the project, or an empty one for the first level", "level %q not found", levelIdentifier)
		}
	}
	return nil, loaderr.Errorf("levels", "add a level to the project in LDtk", "project contains no levels")
}

// LoadLDtkAll parses an LDtk JSON project and builds a Tilemap for every level, keyed by level identifier.
// For Free and GridVania layouts, each Tilemap carries its world Origin (worldX, worldY) and Neighbours list.
// Its errors are *loaderr.Error values, as LoadLDtk's are.
func LoadLDtkAll(data []byte, resolveImage func(relPath string) core.ImageSource) (map[string]*Tilemap, error) {
	return LoadLDtkAllWith(data, LDtkOptions{ResolveImage: resolveImage})
}

// LoadLDtkAllWith parses an LDtk JSON project with opts and builds a Tilemap for every level, keyed by level identifier.
func LoadLDtkAllWith(data []byte, opts LDtkOptions) (map[string]*Tilemap, error) {
	out, err := loadLDtkAll(data, opts)
	return out, loaderr.LocateJSON(err, "", data, fixLDtk)
}

func loadLDtkAll(data []byte, opts LDtkOptions) (map[string]*Tilemap, error) {
	var proj ldtkProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, err
	}
	var totalTiles int
	if len(proj.Levels) > 0 {
		out := make(map[string]*Tilemap, len(proj.Levels))
		for i := range proj.Levels {
			m, err := buildLDtkLevel(&proj, proj.Levels, proj.WorldLayout, "levels", i, opts, &totalTiles)
			if err != nil {
				return nil, err
			}
			out[proj.Levels[i].Identifier] = m
		}
		return out, nil
	}
	out := make(map[string]*Tilemap)
	for wi := range proj.Worlds {
		w := &proj.Worlds[wi]
		levelsPath := loaderr.Path("worlds", wi, "levels")
		for li := range w.Levels {
			m, err := buildLDtkLevel(&proj, w.Levels, w.WorldLayout, levelsPath, li, opts, &totalTiles)
			if err != nil {
				return nil, err
			}
			out[w.Levels[li].Identifier] = m
		}
	}
	return out, nil
}

// LoadLDtkWorld parses an LDtk JSON project (.ldtk) into a LevelWorld whose levels carry their world Origin, pixel Bounds, WorldDepth, and Neighbours list, and load lazily into *Tilemap values on demand.
//
// An empty worldIdentifier selects the project's top-level levels (or the sole world when multi-worlds has one entry); a multi-world project with multiple worlds names the world by its identifier.
// The world's layout must be Free or GridVania (or omitted, defaulting to Free); LinearHorizontal and LinearVertical layouts are refused because they assign no 2D world coordinates.
//
// Its errors are *loaderr.Error values, as LoadLDtk's are.
func LoadLDtkWorld(data []byte, worldIdentifier string, resolveImage func(relPath string) core.ImageSource) (*LevelWorld, error) {
	return LoadLDtkWorldWith(data, worldIdentifier, LDtkOptions{ResolveImage: resolveImage})
}

// LoadLDtkWorldWith parses an LDtk JSON project (.ldtk) into a LevelWorld using opts (including custom IntGrid-to-TileFlags mappings).
func LoadLDtkWorldWith(data []byte, worldIdentifier string, opts LDtkOptions) (*LevelWorld, error) {
	w, err := loadLDtkWorld(data, worldIdentifier, opts)
	return w, loaderr.LocateJSON(err, "", data, fixLDtk)
}

func loadLDtkWorld(data []byte, worldIdentifier string, opts LDtkOptions) (*LevelWorld, error) {
	var proj ldtkProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, err
	}

	var (
		worldName   string
		layout      *string
		levels      []ldtkLevel
		levelsPath  string
		layoutField string
	)

	if worldIdentifier == "" && len(proj.Levels) > 0 {
		layout = proj.WorldLayout
		levels = proj.Levels
		levelsPath = "levels"
		layoutField = "worldLayout"
	} else if len(proj.Worlds) > 0 {
		wi := -1
		if worldIdentifier == "" {
			if len(proj.Worlds) == 1 {
				wi = 0
			} else {
				return nil, loaderr.Errorf("worlds", "pass the identifier of one of the project's worlds to LoadLDtkWorld", "LDtk project has %d worlds; name one", len(proj.Worlds))
			}
		} else {
			for i := range proj.Worlds {
				if proj.Worlds[i].Identifier == worldIdentifier {
					wi = i
					break
				}
			}
			if wi < 0 {
				return nil, loaderr.Errorf("worlds", "pass the identifier of a world defined in the LDtk project", "world %q not found", worldIdentifier)
			}
		}
		worldName = proj.Worlds[wi].Identifier
		layout = proj.Worlds[wi].WorldLayout
		levels = proj.Worlds[wi].Levels
		levelsPath = loaderr.Path("worlds", wi, "levels")
		layoutField = loaderr.Path("worlds", wi, "worldLayout")
	} else if worldIdentifier != "" {
		return nil, loaderr.Errorf("worlds", "pass an empty world identifier for a single-world LDtk project, or enable multi-worlds in LDtk", "world %q not found", worldIdentifier)
	} else {
		return nil, loaderr.Errorf("levels", "add a level to the project in LDtk", "project contains no levels")
	}

	if !hasLDtkWorldPositions(layout) {
		return nil, &loaderr.Error{
			Field:    layoutField,
			Problem:  fmt.Sprintf("LDtk world layout %q gives levels no 2D world positions", *layout),
			Expected: `"Free" or "GridVania"`,
			Fix:      "set the world layout to Free or GridVania in LDtk",
		}
	}
	if len(levels) == 0 {
		return nil, loaderr.Errorf(levelsPath, "add a level to the world in LDtk", "world contains no levels")
	}

	layoutName := "Free"
	if layout != nil && *layout != "" {
		layoutName = *layout
	}

	rawCopy := append([]byte(nil), data...)
	world := &LevelWorld{
		Name:   worldName,
		Layout: layoutName,
		Levels: make([]WorldLevel, len(levels)),
	}
	for i := range levels {
		lvl := &levels[i]
		if lvl.PxWid < 0 {
			return nil, loaderr.Errorf(loaderr.Join(levelsPath, i, "pxWid"), "set the level's pixel width to 0 or more in LDtk", "level %q pixel width %d must be non-negative", lvl.Identifier, lvl.PxWid)
		}
		if lvl.PxHei < 0 {
			return nil, loaderr.Errorf(loaderr.Join(levelsPath, i, "pxHei"), "set the level's pixel height to 0 or more in LDtk", "level %q pixel height %d must be non-negative", lvl.Identifier, lvl.PxHei)
		}
		origin := core.Point{X: core.Points(lvl.WorldX), Y: core.Points(lvl.WorldY)}
		neighbours := resolveLDtkNeighbours(levels, lvl)
		idx := i
		world.Levels[i] = WorldLevel{
			ID:         lvl.Identifier,
			IID:        lvl.IID,
			UID:        lvl.UID,
			Origin:     origin,
			Bounds:     core.Bounds{Origin: origin, Size: core.Size{Width: core.Points(lvl.PxWid), Height: core.Points(lvl.PxHei)}},
			WorldDepth: lvl.WorldDepth,
			Neighbours: neighbours,
			Load: func() (*Tilemap, error) {
				var totalTiles int
				m, err := buildLDtkLevel(&proj, levels, layout, levelsPath, idx, opts, &totalTiles)
				return m, loaderr.LocateJSON(err, "", rawCopy, fixLDtk)
			},
		}
	}
	return world, nil
}

func hasLDtkWorldPositions(layout *string) bool {
	if layout == nil || *layout == "" || *layout == "Free" || *layout == "GridVania" {
		return true
	}
	return false
}

func resolveLDtkNeighbours(levels []ldtkLevel, lvl *ldtkLevel) []LevelNeighbour {
	if len(lvl.Neighbours) == 0 {
		return nil
	}
	byIID := make(map[string]*ldtkLevel, len(levels))
	byUID := make(map[int]*ldtkLevel, len(levels))
	for i := range levels {
		l := &levels[i]
		if l.IID != "" {
			byIID[l.IID] = l
		}
		byUID[l.UID] = l
	}
	out := make([]LevelNeighbour, len(lvl.Neighbours))
	for i, n := range lvl.Neighbours {
		ln := LevelNeighbour{
			IID: n.LevelIID,
			Dir: n.Dir,
		}
		if n.LevelUID != nil {
			ln.UID = *n.LevelUID
		}
		var target *ldtkLevel
		if n.LevelIID != "" {
			target = byIID[n.LevelIID]
		}
		if target == nil && n.LevelUID != nil {
			target = byUID[*n.LevelUID]
		}
		if target != nil {
			ln.Identifier = target.Identifier
			if ln.IID == "" {
				ln.IID = target.IID
			}
			if n.LevelUID == nil {
				ln.UID = target.UID
			}
		}
		out[i] = ln
	}
	return out
}

func isLDtkGridLayer(typ string) bool {
	return typ == "Tiles" || typ == "AutoLayer" || typ == "IntGrid"
}

// buildLDtkLevel builds levels[index].
func buildLDtkLevel(proj *ldtkProject, levels []ldtkLevel, layout *string, levelsPath string, index int, opts LDtkOptions, totalTiles *int) (*Tilemap, error) {
	resolveImage := opts.ResolveImage
	lvl := &levels[index]
	at := loaderr.Join(levelsPath, index)
	gridSize := proj.DefaultGridSize
	if gridSize <= 0 {
		for _, li := range lvl.LayerInstances {
			if li.GridSize > 0 {
				gridSize = li.GridSize
				break
			}
		}
	}
	if gridSize <= 0 {
		return nil, loaderr.Errorf(at, "set the project's default grid size, or a layer's grid size, above 0 in LDtk", "level %q has no positive grid size", lvl.Identifier)
	}
	if lvl.PxWid < 0 {
		return nil, loaderr.Errorf(loaderr.Join(at, "pxWid"), "set the level's pixel width to 0 or more in LDtk", "level %q pixel width %d must be non-negative", lvl.Identifier, lvl.PxWid)
	}
	if lvl.PxHei < 0 {
		return nil, loaderr.Errorf(loaderr.Join(at, "pxHei"), "set the level's pixel height to 0 or more in LDtk", "level %q pixel height %d must be non-negative", lvl.Identifier, lvl.PxHei)
	}

	mapW := lvl.PxWid / gridSize
	mapH := lvl.PxHei / gridSize
	if mapW > 0 && mapH > 0 && mapW > math.MaxInt/mapH {
		return nil, loaderr.Errorf(loaderr.Join(at, "pxWid"), "reduce the level dimensions or increase the grid size in LDtk", "level %q grid size %dx%d overflows", lvl.Identifier, mapW, mapH)
	}

	// Validate explicit layer dimensions before merging them into the level grid size.
	for i, li := range lvl.LayerInstances {
		layerAt := loaderr.Join(at, "layerInstances", i)
		if li.CWid < 0 {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "set the layer's grid width to 0 or more in LDtk", "layer %q width %d must be non-negative", li.Identifier, li.CWid)
		}
		if li.CHei < 0 {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cHei"), "set the layer's grid height to 0 or more in LDtk", "layer %q height %d must be non-negative", li.Identifier, li.CHei)
		}
		if isLDtkGridLayer(li.Type) && li.CWid > 0 && li.CHei > 0 {
			if li.CWid > math.MaxInt/li.CHei {
				return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "reduce the layer dimensions in LDtk", "layer %q size %dx%d overflows", li.Identifier, li.CWid, li.CHei)
			}
			if cells := li.CWid * li.CHei; cells > maxLDtkLayerTiles {
				return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "reduce the layer dimensions in LDtk", "layer %q size %dx%d (%d cells) exceeds maximum allowed layer size (%d cells)", li.Identifier, li.CWid, li.CHei, cells, maxLDtkLayerTiles)
			}
		}
		if li.CWid > mapW {
			mapW = li.CWid
		}
		if li.CHei > mapH {
			mapH = li.CHei
		}
	}

	// Validate each grid layer's effective dimensions, intGridCsv length, and dense allocation budget before allocating any tile buffers.
	for i, li := range lvl.LayerInstances {
		if !isLDtkGridLayer(li.Type) {
			continue
		}
		layerAt := loaderr.Join(at, "layerInstances", i)
		lw, lh := li.CWid, li.CHei
		if lw == 0 {
			lw = mapW
		}
		if lh == 0 {
			lh = mapH
		}
		if lw <= 0 {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "give the layer or level a positive grid width in LDtk", "layer %q effective width %d must be positive", li.Identifier, lw)
		}
		if lh <= 0 {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cHei"), "give the layer or level a positive grid height in LDtk", "layer %q effective height %d must be positive", li.Identifier, lh)
		}
		if lw > math.MaxInt/lh {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "reduce the layer or level dimensions in LDtk", "layer %q size %dx%d overflows", li.Identifier, lw, lh)
		}
		cells := lw * lh
		if cells > maxLDtkLayerTiles {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "reduce the layer or level dimensions in LDtk", "layer %q size %dx%d (%d cells) exceeds maximum allowed layer size (%d cells)", li.Identifier, lw, lh, cells, maxLDtkLayerTiles)
		}
		if li.Type == "IntGrid" && len(li.IntGridCSV) > cells {
			return nil, loaderr.Errorf(loaderr.Join(layerAt, "intGridCsv"), "re-save the project in LDtk so intGridCsv has at most __cWid * __cHei cells", "layer %q intGridCsv has %d cells, more than %dx%d (%d)", li.Identifier, len(li.IntGridCSV), lw, lh, cells)
		}
		allocatesTiles := li.Type == "Tiles" || li.Type == "AutoLayer" || (li.Type == "IntGrid" && len(li.AutoLayerTiles) > 0)
		if allocatesTiles && totalTiles != nil {
			if cells > maxLDtkTotalTiles-*totalTiles {
				return nil, loaderr.Errorf(loaderr.Join(layerAt, "__cWid"), "reduce layer dimensions or load levels lazily with LoadLDtkWorld", "layer %q size %dx%d exceeds maximum total tile allocation (%d cells)", li.Identifier, lw, lh, maxLDtkTotalTiles)
			}
			*totalTiles += cells
		}
	}
	if mapW > 0 && mapH > 0 && mapW > math.MaxInt/mapH {
		return nil, loaderr.Errorf(at, "reduce the level or layer dimensions in LDtk", "level %q size %dx%d overflows", lvl.Identifier, mapW, mapH)
	}

	props, err := parseLDtkFields(lvl.FieldInstances, loaderr.Join(at, "fieldInstances"))
	if err != nil {
		return nil, err
	}

	var origin core.Point
	if hasLDtkWorldPositions(layout) {
		origin = core.Point{X: core.Points(lvl.WorldX), Y: core.Points(lvl.WorldY)}
	}
	m := &Tilemap{
		Width:      mapW,
		Height:     mapH,
		TileWidth:  float32(gridSize),
		TileHeight: float32(gridSize),
		Origin:     origin,
		Properties: props,
		Neighbours: resolveLDtkNeighbours(levels, lvl),
	}

	// Build tilesets from defs.tilesets with sequential non-overlapping FirstGIDs.
	tilesetsByUID := make(map[int]*Tileset, len(proj.Defs.Tilesets))
	tilesetDefsByUID := make(map[int]ldtkTilesetDef, len(proj.Defs.Tilesets))
	var nextGID uint64 = 1
	for ti, def := range proj.Defs.Tilesets {
		tw := def.TileGridSize
		if tw <= 0 {
			tw = gridSize
		}
		cols := def.CWid
		rows := def.CHei
		if cols < 0 {
			cols = 0
		}
		if rows < 0 {
			rows = 0
		}
		step := int64(tw) + int64(def.Spacing)
		if cols == 0 && def.PxWid > 0 && step > 0 {
			if n := (int64(def.PxWid) - 2*int64(def.Padding) + int64(def.Spacing)) / step; n > 0 && n <= math.MaxInt {
				cols = int(n)
			}
		}
		if rows == 0 && def.PxHei > 0 && step > 0 {
			if n := (int64(def.PxHei) - 2*int64(def.Padding) + int64(def.Spacing)) / step; n > 0 && n <= math.MaxInt {
				rows = int(n)
			}
		}
		tileCount := 1
		if cols > 0 && rows > 0 {
			if cols > int(TileIDMask)/rows {
				return nil, loaderr.Errorf(loaderr.Path("defs", "tilesets", ti, "__cWid"), "reduce the tileset grid dimensions in LDtk", "tileset %q size %dx%d exceeds maximum tile ID range", def.Identifier, cols, rows)
			}
			tileCount = cols * rows
		}
		if nextGID > uint64(TileIDMask) || uint64(tileCount) > uint64(TileIDMask)-nextGID+1 {
			return nil, loaderr.Errorf(loaderr.Path("defs", "tilesets", ti), "reduce tileset tile counts in LDtk", "tileset %q firstgid overflows tile ID range", def.Identifier)
		}

		var img core.ImageSource
		if def.RelPath != "" {
			if resolveImage != nil {
				img = resolveImage(def.RelPath)
			} else {
				img = core.ImageSource{URI: def.RelPath}
			}
		}

		ts := &Tileset{
			Name:       def.Identifier,
			FirstGID:   uint32(nextGID),
			Image:      img,
			TileWidth:  float32(tw),
			TileHeight: float32(tw),
			Columns:    cols,
			TileCount:  tileCount,
			Spacing:    float32(def.Spacing),
			Margin:     float32(def.Padding),
		}
		m.Tilesets = append(m.Tilesets, ts)
		tilesetsByUID[def.UID] = ts
		tilesetDefsByUID[def.UID] = def
		nextGID += uint64(tileCount)
	}

	// Index IntGrid value definitions (and their group names) from defs.layers by layer UID and identifier.
	type ldtkIntGridEntry struct {
		identifier string
		group      string
	}
	intGridDefsByUID := make(map[int]map[int]ldtkIntGridEntry, len(proj.Defs.Layers))
	intGridDefsByName := make(map[string]map[int]ldtkIntGridEntry, len(proj.Defs.Layers))
	for _, ldef := range proj.Defs.Layers {
		if len(ldef.IntGridValues) == 0 {
			continue
		}
		groups := make(map[int]string, len(ldef.IntGridValuesGroups))
		for _, g := range ldef.IntGridValuesGroups {
			groups[g.UID] = g.Identifier
		}
		vals := make(map[int]ldtkIntGridEntry, len(ldef.IntGridValues))
		for _, v := range ldef.IntGridValues {
			vals[v.Value] = ldtkIntGridEntry{
				identifier: v.Identifier,
				group:      groups[v.GroupUID],
			}
		}
		intGridDefsByUID[ldef.UID] = vals
		intGridDefsByName[ldef.Identifier] = vals
	}
	var customNameFlags map[string]TileFlags
	if len(opts.IntGridFlags) > 0 {
		customNameFlags = make(map[string]TileFlags, len(opts.IntGridFlags))
		for k, f := range opts.IntGridFlags {
			customNameFlags[strings.ToLower(strings.TrimSpace(k))] = f
		}
	}

	// LDtk lists layerInstances top-to-bottom (foreground first); iterate bottom-to-top for painter's draw order.
	for i := len(lvl.LayerInstances) - 1; i >= 0; i-- {
		li := lvl.LayerInstances[i]
		visible := true
		if li.Visible != nil {
			visible = *li.Visible
		}
		opacity := float32(1)
		if li.Opacity != nil {
			opacity = *li.Opacity
		}
		ox := float32(li.PxTotalOffsetX)
		oy := float32(li.PxTotalOffsetY)

		layerGrid := li.GridSize
		if layerGrid <= 0 {
			layerGrid = gridSize
		}
		lw, lh := li.CWid, li.CHei
		if lw == 0 {
			lw = m.Width
		}
		if lh == 0 {
			lh = m.Height
		}

		switch li.Type {
		case "Tiles":
			ts := resolveLDtkLayerTileset(li, tilesetsByUID)
			def := ldtkTilesetDef{}
			if li.TilesetDefUID != nil {
				def = tilesetDefsByUID[*li.TilesetDefUID]
			}
			data := buildLDtkTileData(li.GridTiles, lw, lh, layerGrid, ts, def)
			m.addTileLayer(TileLayer{
				Name:        li.Identifier,
				Width:       lw,
				Height:      lh,
				Data:        data,
				OffsetX:     ox,
				OffsetY:     oy,
				ParallaxX:   1,
				ParallaxY:   1,
				HasParallax: true,
				Opacity:     opacity,
				Visible:     visible,
			})

		case "AutoLayer":
			ts := resolveLDtkLayerTileset(li, tilesetsByUID)
			def := ldtkTilesetDef{}
			if li.TilesetDefUID != nil {
				def = tilesetDefsByUID[*li.TilesetDefUID]
			}
			data := buildLDtkTileData(li.AutoLayerTiles, lw, lh, layerGrid, ts, def)
			m.addTileLayer(TileLayer{
				Name:        li.Identifier,
				Width:       lw,
				Height:      lh,
				Data:        data,
				OffsetX:     ox,
				OffsetY:     oy,
				ParallaxX:   1,
				ParallaxY:   1,
				HasParallax: true,
				Opacity:     opacity,
				Visible:     visible,
			})

		case "IntGrid":
			valDefs := intGridDefsByUID[li.LayerDefUID]
			if valDefs == nil {
				valDefs = intGridDefsByName[li.Identifier]
			}
			for idx, v := range li.IntGridCSV {
				if v == 0 {
					continue
				}
				col := idx % lw
				row := idx / lw
				entry := valDefs[v]
				flag := resolveLDtkIntGridFlag(v, entry.identifier, entry.group, opts.IntGridValueFlags, customNameFlags)
				if flag != 0 {
					m.SetCellFlags(col, row, m.FlagsAt(col, row)|flag)
				}
			}
			if len(li.AutoLayerTiles) > 0 {
				ts := resolveLDtkLayerTileset(li, tilesetsByUID)
				def := ldtkTilesetDef{}
				if li.TilesetDefUID != nil {
					def = tilesetDefsByUID[*li.TilesetDefUID]
				}
				data := buildLDtkTileData(li.AutoLayerTiles, lw, lh, layerGrid, ts, def)
				m.addTileLayer(TileLayer{
					Name:        li.Identifier,
					Width:       lw,
					Height:      lh,
					Data:        data,
					OffsetX:     ox,
					OffsetY:     oy,
					ParallaxX:   1,
					ParallaxY:   1,
					HasParallax: true,
					Opacity:     opacity,
					Visible:     visible,
				})
			}

		case "Entities":
			objs := make([]Object, len(li.EntityInstances))
			for j, ent := range li.EntityInstances {
				ep, err := parseLDtkFields(ent.FieldInstances, loaderr.Join(at, "layerInstances", i, "entityInstances", j, "fieldInstances"))
				if err != nil {
					return nil, err
				}
				shape := ObjectRect
				if ent.Width == 0 && ent.Height == 0 {
					shape = ObjectPoint
				}
				var ex, ey float32
				if len(ent.Px) >= 2 {
					ex = float32(ent.Px[0])
					ey = float32(ent.Px[1])
				}
				objs[j] = Object{
					ID:         j + 1,
					Name:       ent.Identifier,
					Type:       ent.Identifier,
					Shape:      shape,
					X:          ex,
					Y:          ey,
					Width:      float32(ent.Width),
					Height:     float32(ent.Height),
					Visible:    true,
					Properties: ep,
				}
			}
			m.Objects = append(m.Objects, ObjectLayer{
				Name:        li.Identifier,
				OffsetX:     ox,
				OffsetY:     oy,
				ParallaxX:   1,
				ParallaxY:   1,
				HasParallax: true,
				Opacity:     opacity,
				Visible:     visible,
				Objects:     objs,
			})
		}
	}

	return m, nil
}

func resolveLDtkLayerTileset(li ldtkLayerInstance, byUID map[int]*Tileset) *Tileset {
	if li.OverrideTilesetUID != nil {
		if ts, ok := byUID[*li.OverrideTilesetUID]; ok {
			return ts
		}
	}
	if li.TilesetDefUID != nil {
		if ts, ok := byUID[*li.TilesetDefUID]; ok {
			return ts
		}
	}
	return nil
}

func buildLDtkTileData(tiles []ldtkTileInstance, lw, lh, gridSize int, ts *Tileset, def ldtkTilesetDef) []uint32 {
	if lw <= 0 || lh <= 0 || lw > math.MaxInt/lh || lw*lh > maxLDtkLayerTiles {
		return nil
	}
	data := make([]uint32, lw*lh)
	if ts == nil || gridSize <= 0 {
		return data
	}
	tileStep := int(ts.TileWidth + ts.Spacing)
	if tileStep <= 0 {
		tileStep = gridSize
	}
	padding := def.Padding
	cols := ts.Columns
	for _, tile := range tiles {
		if len(tile.Px) < 2 {
			continue
		}
		col := tile.Px[0] / gridSize
		row := tile.Px[1] / gridSize
		if col < 0 || row < 0 || col >= lw || row >= lh {
			continue
		}
		var localID uint32
		if tile.T != nil && *tile.T >= 0 {
			localID = uint32(*tile.T)
		} else if len(tile.Src) >= 2 && cols > 0 {
			tc := (int64(tile.Src[0]) - int64(padding)) / int64(tileStep)
			tr := (int64(tile.Src[1]) - int64(padding)) / int64(tileStep)
			if tc < 0 || tr < 0 {
				continue
			}
			localID = uint32(tr*int64(cols) + tc)
		}
		gid := (ts.FirstGID + localID) & TileIDMask
		if tile.F&1 != 0 {
			gid |= FlipHorizontal
		}
		if tile.F&2 != 0 {
			gid |= FlipVertical
		}
		data[row*lw+col] = gid
	}
	return data
}

func resolveLDtkIntGridFlag(value int, identifier, group string, valueFlags map[int]TileFlags, nameFlags map[string]TileFlags) TileFlags {
	if value <= 0 {
		return 0
	}
	if len(valueFlags) > 0 {
		if f, ok := valueFlags[value]; ok {
			return f
		}
	}
	if len(nameFlags) > 0 {
		if id := strings.ToLower(strings.TrimSpace(identifier)); id != "" {
			if f, ok := nameFlags[id]; ok {
				return f
			}
		}
		if g := strings.ToLower(strings.TrimSpace(group)); g != "" {
			if f, ok := nameFlags[g]; ok {
				return f
			}
		}
	}
	return ldtkIntGridFlag(value, identifier, group)
}

func ldtkIntGridFlag(value int, identifier, group string) TileFlags {
	if value <= 0 {
		return 0
	}
	if f, ok := ldtkIntGridNameFlag(identifier); ok {
		return f
	}
	if f, ok := ldtkIntGridNameFlag(group); ok {
		return f
	}
	if value == 1 {
		return TileSolid
	}
	if value <= 32 {
		return TileFlags(1 << uint(value-1))
	}
	return TileSolid
}

func ldtkIntGridNameFlag(name string) (TileFlags, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "solid", "wall", "walls", "ground", "collision", "collisions":
		return TileSolid, true
	case "oneway", "one_way", "one-way", "platform":
		return TileOneWay, true
	default:
		return 0, false
	}
}

// parseLDtkFields reads the field instances found at the field path at.
func parseLDtkFields(fields []ldtkFieldInstance, at string) (Properties, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	props := make(Properties, len(fields))
	for fi, f := range fields {
		bad := func(err error) error {
			return loaderr.Errorf(loaderr.Join(at, fi, "__value"), fmt.Sprintf("give the field a value of its type, %s, in LDtk", f.Type), "field %q (%s): %w", f.Identifier, f.Type, err)
		}
		if len(f.Value) == 0 || string(f.Value) == "null" {
			props[f.Identifier] = Property{Name: f.Identifier, Type: f.Type, Value: nil}
			continue
		}
		var typ string
		var val any
		switch {
		case f.Type == "Int":
			typ = "int"
			var n int
			if err := json.Unmarshal(f.Value, &n); err != nil {
				var fl float64
				if err2 := json.Unmarshal(f.Value, &fl); err2 != nil {
					return nil, bad(err)
				}
				n = int(fl)
			}
			val = n
		case f.Type == "Float":
			typ = "float"
			var fl float64
			if err := json.Unmarshal(f.Value, &fl); err != nil {
				return nil, bad(err)
			}
			val = fl
		case f.Type == "Bool":
			typ = "bool"
			var b bool
			if err := json.Unmarshal(f.Value, &b); err != nil {
				return nil, bad(err)
			}
			val = b
		case f.Type == "Color":
			typ = "color"
			var s string
			if err := json.Unmarshal(f.Value, &s); err != nil {
				return nil, bad(err)
			}
			val = s
		case f.Type == "String" || f.Type == "Multilines" || f.Type == "FilePath" || strings.HasPrefix(f.Type, "LocalEnum.") || strings.HasPrefix(f.Type, "ExternEnum."):
			typ = "string"
			var s string
			if err := json.Unmarshal(f.Value, &s); err != nil {
				return nil, bad(err)
			}
			val = s
		default:
			typ = strings.ToLower(f.Type)
			if err := json.Unmarshal(f.Value, &val); err != nil {
				return nil, bad(err)
			}
		}
		props[f.Identifier] = Property{Name: f.Identifier, Type: typ, Value: val}
	}
	return props, nil
}

type ldtkProject struct {
	DefaultGridSize int         `json:"defaultGridSize"`
	WorldLayout     *string     `json:"worldLayout"`
	Defs            ldtkDefs    `json:"defs"`
	Levels          []ldtkLevel `json:"levels"`
	Worlds          []ldtkWorld `json:"worlds"`
}

type ldtkWorld struct {
	Identifier  string      `json:"identifier"`
	IID         string      `json:"iid"`
	WorldLayout *string     `json:"worldLayout"`
	Levels      []ldtkLevel `json:"levels"`
}

type ldtkDefs struct {
	Tilesets []ldtkTilesetDef `json:"tilesets"`
	Layers   []ldtkLayerDef   `json:"layers"`
}

type ldtkTilesetDef struct {
	UID          int    `json:"uid"`
	Identifier   string `json:"identifier"`
	RelPath      string `json:"relPath"`
	PxWid        int    `json:"pxWid"`
	PxHei        int    `json:"pxHei"`
	TileGridSize int    `json:"tileGridSize"`
	Spacing      int    `json:"spacing"`
	Padding      int    `json:"padding"`
	CWid         int    `json:"__cWid"`
	CHei         int    `json:"__cHei"`
}

type ldtkLayerDef struct {
	UID                 int                     `json:"uid"`
	Identifier          string                  `json:"identifier"`
	Type                string                  `json:"type"`
	GridSize            int                     `json:"gridSize"`
	IntGridValues       []ldtkIntGridValue      `json:"intGridValues"`
	IntGridValuesGroups []ldtkIntGridValueGroup `json:"intGridValuesGroups"`
}

type ldtkIntGridValue struct {
	Value      int    `json:"value"`
	Identifier string `json:"identifier"`
	Color      string `json:"color"`
	GroupUID   int    `json:"groupUid"`
}

type ldtkIntGridValueGroup struct {
	UID        int    `json:"uid"`
	Identifier string `json:"identifier"`
	Color      string `json:"color"`
}

type ldtkLevel struct {
	Identifier     string              `json:"identifier"`
	IID            string              `json:"iid"`
	UID            int                 `json:"uid"`
	WorldX         int                 `json:"worldX"`
	WorldY         int                 `json:"worldY"`
	WorldDepth     int                 `json:"worldDepth"`
	PxWid          int                 `json:"pxWid"`
	PxHei          int                 `json:"pxHei"`
	Neighbours     []ldtkNeighbour     `json:"__neighbours"`
	FieldInstances []ldtkFieldInstance `json:"fieldInstances"`
	LayerInstances []ldtkLayerInstance `json:"layerInstances"`
}

type ldtkNeighbour struct {
	LevelIID string `json:"levelIid"`
	LevelUID *int   `json:"levelUid"`
	Dir      string `json:"dir"`
}

type ldtkLayerInstance struct {
	Identifier         string               `json:"__identifier"`
	Type               string               `json:"__type"`
	CWid               int                  `json:"__cWid"`
	CHei               int                  `json:"__cHei"`
	GridSize           int                  `json:"__gridSize"`
	Opacity            *float32             `json:"__opacity"`
	PxTotalOffsetX     int                  `json:"__pxTotalOffsetX"`
	PxTotalOffsetY     int                  `json:"__pxTotalOffsetY"`
	TilesetDefUID      *int                 `json:"__tilesetDefUid"`
	OverrideTilesetUID *int                 `json:"overrideTilesetUid"`
	LayerDefUID        int                  `json:"layerDefUid"`
	Visible            *bool                `json:"visible"`
	IntGridCSV         []int                `json:"intGridCsv"`
	GridTiles          []ldtkTileInstance   `json:"gridTiles"`
	AutoLayerTiles     []ldtkTileInstance   `json:"autoLayerTiles"`
	EntityInstances    []ldtkEntityInstance `json:"entityInstances"`
}

type ldtkTileInstance struct {
	Px  []int `json:"px"`
	Src []int `json:"src"`
	F   int   `json:"f"`
	T   *int  `json:"t"`
}

type ldtkEntityInstance struct {
	Identifier     string              `json:"__identifier"`
	Px             []int               `json:"px"`
	Width          int                 `json:"width"`
	Height         int                 `json:"height"`
	IID            string              `json:"iid"`
	FieldInstances []ldtkFieldInstance `json:"fieldInstances"`
}

type ldtkFieldInstance struct {
	Identifier string          `json:"__identifier"`
	Type       string          `json:"__type"`
	Value      json.RawMessage `json:"__value"`
}
