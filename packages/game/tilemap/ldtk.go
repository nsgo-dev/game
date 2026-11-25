package tilemap

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
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

// LoadLDtk parses an LDtk JSON project and builds a Tilemap for the level matching levelIdentifier (or the first level when levelIdentifier is empty).
//
// Tiles (gridTiles), AutoLayer (autoLayerTiles), IntGrid (intGridCsv mapped to TileSolid/TileFlags plus optional autoLayerTiles), and Entities (entityInstances mapped to ObjectLayer with fieldInstances properties) are supported, including LDtk flip bits (bit 0 = FlipHorizontal, bit 1 = FlipVertical).
// For Free and GridVania layouts, the returned Tilemap's Origin is set from the level's (worldX, worldY) and its Neighbours are populated from __neighbours.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func LoadLDtk(data []byte, levelIdentifier string, resolveImage func(relPath string) core.ImageSource) (*Tilemap, error) {
	m, err := loadLDtk(data, levelIdentifier, resolveImage)
	return m, loaderr.Locate(err, "", data, fixLDtk)
}

// fixLDtk is the fix for an LDtk project error that has no more specific one.
const fixLDtk = "correct the project in LDtk and save it again"

func loadLDtk(data []byte, levelIdentifier string, resolveImage func(relPath string) core.ImageSource) (*Tilemap, error) {
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
		return buildLDtkLevel(&proj, proj.Levels, proj.WorldLayout, "levels", index, resolveImage)
	}
	if len(proj.Worlds) > 0 {
		for wi := range proj.Worlds {
			w := &proj.Worlds[wi]
			for li := range w.Levels {
				if levelIdentifier == "" || w.Levels[li].Identifier == levelIdentifier {
					return buildLDtkLevel(&proj, w.Levels, w.WorldLayout, loaderr.Path("worlds", wi, "levels"), li, resolveImage)
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
	out, err := loadLDtkAll(data, resolveImage)
	return out, loaderr.Locate(err, "", data, fixLDtk)
}

func loadLDtkAll(data []byte, resolveImage func(relPath string) core.ImageSource) (map[string]*Tilemap, error) {
	var proj ldtkProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, err
	}
	if len(proj.Levels) > 0 {
		out := make(map[string]*Tilemap, len(proj.Levels))
		for i := range proj.Levels {
			m, err := buildLDtkLevel(&proj, proj.Levels, proj.WorldLayout, "levels", i, resolveImage)
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
			m, err := buildLDtkLevel(&proj, w.Levels, w.WorldLayout, levelsPath, li, resolveImage)
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
	w, err := loadLDtkWorld(data, worldIdentifier, resolveImage)
	return w, loaderr.Locate(err, "", data, fixLDtk)
}

func loadLDtkWorld(data []byte, worldIdentifier string, resolveImage func(relPath string) core.ImageSource) (*LevelWorld, error) {
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
				m, err := buildLDtkLevel(&proj, levels, layout, levelsPath, idx, resolveImage)
				return m, loaderr.Locate(err, "", rawCopy, fixLDtk)
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

// buildLDtkLevel builds levels[index].
func buildLDtkLevel(proj *ldtkProject, levels []ldtkLevel, layout *string, levelsPath string, index int, resolveImage func(relPath string) core.ImageSource) (*Tilemap, error) {
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

	mapW := lvl.PxWid / gridSize
	mapH := lvl.PxHei / gridSize
	for _, li := range lvl.LayerInstances {
		if li.CWid > mapW {
			mapW = li.CWid
		}
		if li.CHei > mapH {
			mapH = li.CHei
		}
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
	var nextGID uint32 = 1
	for _, def := range proj.Defs.Tilesets {
		tw := def.TileGridSize
		if tw <= 0 {
			tw = gridSize
		}
		cols := def.CWid
		rows := def.CHei
		step := tw + def.Spacing
		if cols <= 0 && def.PxWid > 0 && step > 0 {
			cols = (def.PxWid - 2*def.Padding + def.Spacing) / step
		}
		if rows <= 0 && def.PxHei > 0 && step > 0 {
			rows = (def.PxHei - 2*def.Padding + def.Spacing) / step
		}
		tileCount := max(1, cols*rows)

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
			FirstGID:   nextGID,
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
		nextGID += uint32(tileCount)
	}

	// Index IntGrid value definitions from defs.layers by layer UID and identifier.
	intGridDefsByUID := make(map[int]map[int]string, len(proj.Defs.Layers))
	intGridDefsByName := make(map[string]map[int]string, len(proj.Defs.Layers))
	for _, ldef := range proj.Defs.Layers {
		if len(ldef.IntGridValues) == 0 {
			continue
		}
		vals := make(map[int]string, len(ldef.IntGridValues))
		for _, v := range ldef.IntGridValues {
			vals[v.Value] = v.Identifier
		}
		intGridDefsByUID[ldef.UID] = vals
		intGridDefsByName[ldef.Identifier] = vals
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
		if lw <= 0 {
			lw = m.Width
		}
		if lh <= 0 {
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
				Name:    li.Identifier,
				Width:   lw,
				Height:  lh,
				Data:    data,
				OffsetX: ox,
				OffsetY: oy,
				Opacity: opacity,
				Visible: visible,
			})

		case "AutoLayer":
			ts := resolveLDtkLayerTileset(li, tilesetsByUID)
			def := ldtkTilesetDef{}
			if li.TilesetDefUID != nil {
				def = tilesetDefsByUID[*li.TilesetDefUID]
			}
			data := buildLDtkTileData(li.AutoLayerTiles, lw, lh, layerGrid, ts, def)
			m.addTileLayer(TileLayer{
				Name:    li.Identifier,
				Width:   lw,
				Height:  lh,
				Data:    data,
				OffsetX: ox,
				OffsetY: oy,
				Opacity: opacity,
				Visible: visible,
			})

		case "IntGrid":
			valNames := intGridDefsByUID[li.LayerDefUID]
			if valNames == nil {
				valNames = intGridDefsByName[li.Identifier]
			}
			for idx, v := range li.IntGridCSV {
				if v == 0 {
					continue
				}
				col := idx % lw
				row := idx / lw
				flag := ldtkIntGridFlag(v, valNames[v])
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
					Name:    li.Identifier,
					Width:   lw,
					Height:  lh,
					Data:    data,
					OffsetX: ox,
					OffsetY: oy,
					Opacity: opacity,
					Visible: visible,
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
				Name:    li.Identifier,
				OffsetX: ox,
				OffsetY: oy,
				Opacity: opacity,
				Visible: visible,
				Objects: objs,
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
	if lw <= 0 || lh <= 0 {
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
			tc := (tile.Src[0] - padding) / tileStep
			tr := (tile.Src[1] - padding) / tileStep
			if tc < 0 || tr < 0 {
				continue
			}
			localID = uint32(tr*cols + tc)
		}
		gid := ts.FirstGID + localID
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

func ldtkIntGridFlag(value int, identifier string) TileFlags {
	if value <= 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(identifier)) {
	case "solid", "wall", "walls", "ground", "collision", "collisions":
		return TileSolid
	case "oneway", "one_way", "one-way", "platform":
		return TileOneWay
	}
	if value == 1 {
		return TileSolid
	}
	if value <= 32 {
		return TileFlags(1 << uint(value-1))
	}
	return TileSolid
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
	UID           int                `json:"uid"`
	Identifier    string             `json:"identifier"`
	Type          string             `json:"type"`
	GridSize      int                `json:"gridSize"`
	IntGridValues []ldtkIntGridValue `json:"intGridValues"`
}

type ldtkIntGridValue struct {
	Value      int    `json:"value"`
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
