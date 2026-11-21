package tilemap

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// LoadLDtk parses an LDtk JSON project and builds a Tilemap for the level matching levelIdentifier (or the first level when levelIdentifier is empty).
//
// Tiles (gridTiles), AutoLayer (autoLayerTiles), IntGrid (intGridCsv mapped to TileSolid/TileFlags plus optional autoLayerTiles), and Entities (entityInstances mapped to ObjectLayer with fieldInstances properties) are supported, including LDtk flip bits (bit 0 = FlipHorizontal, bit 1 = FlipVertical).
func LoadLDtk(data []byte, levelIdentifier string, resolveImage func(relPath string) core.ImageSource) (*Tilemap, error) {
	var proj ldtkProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, fmt.Errorf("tilemap: ldtk: %w", err)
	}
	if len(proj.Levels) == 0 {
		return nil, fmt.Errorf("tilemap: ldtk: project contains no levels")
	}

	var level *ldtkLevel
	if levelIdentifier == "" {
		level = &proj.Levels[0]
	} else {
		for i := range proj.Levels {
			if proj.Levels[i].Identifier == levelIdentifier {
				level = &proj.Levels[i]
				break
			}
		}
		if level == nil {
			return nil, fmt.Errorf("tilemap: ldtk: level %q not found", levelIdentifier)
		}
	}
	return buildLDtkLevel(&proj, level, resolveImage)
}

// LoadLDtkAll parses an LDtk JSON project and builds a Tilemap for every level, keyed by level identifier.
func LoadLDtkAll(data []byte, resolveImage func(relPath string) core.ImageSource) (map[string]*Tilemap, error) {
	var proj ldtkProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, fmt.Errorf("tilemap: ldtk: %w", err)
	}
	out := make(map[string]*Tilemap, len(proj.Levels))
	for i := range proj.Levels {
		lvl := &proj.Levels[i]
		m, err := buildLDtkLevel(&proj, lvl, resolveImage)
		if err != nil {
			return nil, fmt.Errorf("tilemap: ldtk: level %q: %w", lvl.Identifier, err)
		}
		out[lvl.Identifier] = m
	}
	return out, nil
}

func buildLDtkLevel(proj *ldtkProject, lvl *ldtkLevel, resolveImage func(relPath string) core.ImageSource) (*Tilemap, error) {
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
		return nil, fmt.Errorf("tilemap: ldtk: level %q has no positive grid size", lvl.Identifier)
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

	props, err := parseLDtkFields(lvl.FieldInstances)
	if err != nil {
		return nil, fmt.Errorf("tilemap: ldtk: level properties: %w", err)
	}

	m := &Tilemap{
		Width:      mapW,
		Height:     mapH,
		TileWidth:  float32(gridSize),
		TileHeight: float32(gridSize),
		Properties: props,
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
				ep, err := parseLDtkFields(ent.FieldInstances)
				if err != nil {
					return nil, fmt.Errorf("tilemap: ldtk: entity %q fields: %w", ent.Identifier, err)
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

func parseLDtkFields(fields []ldtkFieldInstance) (Properties, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	props := make(Properties, len(fields))
	for _, f := range fields {
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
					return nil, fmt.Errorf("field %q (%s): %w", f.Identifier, f.Type, err)
				}
				n = int(fl)
			}
			val = n
		case f.Type == "Float":
			typ = "float"
			var fl float64
			if err := json.Unmarshal(f.Value, &fl); err != nil {
				return nil, fmt.Errorf("field %q (%s): %w", f.Identifier, f.Type, err)
			}
			val = fl
		case f.Type == "Bool":
			typ = "bool"
			var b bool
			if err := json.Unmarshal(f.Value, &b); err != nil {
				return nil, fmt.Errorf("field %q (%s): %w", f.Identifier, f.Type, err)
			}
			val = b
		case f.Type == "Color":
			typ = "color"
			var s string
			if err := json.Unmarshal(f.Value, &s); err != nil {
				return nil, fmt.Errorf("field %q (%s): %w", f.Identifier, f.Type, err)
			}
			val = s
		case f.Type == "String" || f.Type == "Multilines" || f.Type == "FilePath" || strings.HasPrefix(f.Type, "LocalEnum.") || strings.HasPrefix(f.Type, "ExternEnum."):
			typ = "string"
			var s string
			if err := json.Unmarshal(f.Value, &s); err != nil {
				return nil, fmt.Errorf("field %q (%s): %w", f.Identifier, f.Type, err)
			}
			val = s
		default:
			typ = strings.ToLower(f.Type)
			if err := json.Unmarshal(f.Value, &val); err != nil {
				return nil, fmt.Errorf("field %q (%s): %w", f.Identifier, f.Type, err)
			}
		}
		props[f.Identifier] = Property{Name: f.Identifier, Type: typ, Value: val}
	}
	return props, nil
}

type ldtkProject struct {
	DefaultGridSize int         `json:"defaultGridSize"`
	Defs            ldtkDefs    `json:"defs"`
	Levels          []ldtkLevel `json:"levels"`
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
	UID            int                 `json:"uid"`
	WorldX         int                 `json:"worldX"`
	WorldY         int                 `json:"worldY"`
	PxWid          int                 `json:"pxWid"`
	PxHei          int                 `json:"pxHei"`
	FieldInstances []ldtkFieldInstance `json:"fieldInstances"`
	LayerInstances []ldtkLayerInstance `json:"layerInstances"`
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
