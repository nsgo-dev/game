package tilemap

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// fixTiledWorld is the fix for a Tiled .world error that has no more specific one.
const fixTiledWorld = "correct the world file in Tiled and save it again (.world)"

// TiledWorldMap describes one map placement inside a Tiled .world file.
type TiledWorldMap struct {
	FileName string
	X        int
	Y        int
	Width    int
	Height   int
}

// Origin returns the map's top-left world position (X, Y) in points.
func (m TiledWorldMap) Origin() core.Point {
	return core.Point{X: core.Points(m.X), Y: core.Points(m.Y)}
}

// Bounds returns the map's world bounding rectangle. Width and Height may be 0 if omitted in the .world file.
func (m TiledWorldMap) Bounds() core.Bounds {
	return core.Bounds{
		Origin: m.Origin(),
		Size:   core.Size{Width: core.Points(m.Width), Height: core.Points(m.Height)},
	}
}

// TiledWorldPattern describes one regex-based map grid pattern inside a Tiled .world file.
//
// Capturing group 1 gives the grid X index and capturing group 2 (when present) gives the grid Y index;
// the world position is (gx*MultiplierX + OffsetX, gy*MultiplierY + OffsetY).
// Following Tiled's specification, MultiplierX/MapWidth and MultiplierY/MapHeight default to each other when one is 0.
type TiledWorldPattern struct {
	Regexp      string
	MultiplierX int
	MultiplierY int
	OffsetX     int
	OffsetY     int
	MapWidth    int
	MapHeight   int

	re *regexp.Regexp
}

func (p *TiledWorldPattern) effectiveMetrics() (mulX, mulY, mapW, mapH int) {
	mulX, mulY = p.MultiplierX, p.MultiplierY
	mapW, mapH = p.MapWidth, p.MapHeight
	if mulX == 0 && mapW > 0 {
		mulX = mapW
	}
	if mulY == 0 && mapH > 0 {
		mulY = mapH
	}
	if mapW == 0 && mulX != 0 {
		if mulX < 0 {
			mapW = -mulX
		} else {
			mapW = mulX
		}
	}
	if mapH == 0 && mulY != 0 {
		if mulY < 0 {
			mapH = -mulY
		} else {
			mapH = mulY
		}
	}
	return mulX, mulY, mapW, mapH
}

// Match tests fileName against p's regular expression and returns its resolved TiledWorldMap when it matches.
// If fileName includes a directory prefix and does not match directly, its base filename is tested as well.
func (p *TiledWorldPattern) Match(fileName string) (TiledWorldMap, bool) {
	if p == nil || p.Regexp == "" {
		return TiledWorldMap{}, false
	}
	re := p.re
	if re == nil {
		var err error
		re, err = regexp.Compile(p.Regexp)
		if err != nil {
			return TiledWorldMap{}, false
		}
		p.re = re
	}
	sub := re.FindStringSubmatch(fileName)
	if sub == nil {
		if base := path.Base(fileName); base != fileName {
			sub = re.FindStringSubmatch(base)
		}
	}
	if len(sub) < 2 {
		return TiledWorldMap{}, false
	}
	gx, err := strconv.Atoi(sub[1])
	if err != nil {
		return TiledWorldMap{}, false
	}
	gy := 0
	if len(sub) >= 3 && sub[2] != "" {
		gy, err = strconv.Atoi(sub[2])
		if err != nil {
			return TiledWorldMap{}, false
		}
	}
	mulX, mulY, mapW, mapH := p.effectiveMetrics()
	return TiledWorldMap{
		FileName: fileName,
		X:        gx*mulX + p.OffsetX,
		Y:        gy*mulY + p.OffsetY,
		Width:    mapW,
		Height:   mapH,
	}, true
}

// TiledWorld is the parsed representation of a Tiled .world JSON file.
type TiledWorld struct {
	Maps                 []TiledWorldMap
	Patterns             []TiledWorldPattern
	OnlyShowAdjacentMaps bool
}

// ParseTiledWorld parses a Tiled .world JSON file, compiling and validating every pattern regular expression.
//
// Both explicit maps entries (fileName, x, y, optional width and height) and regex patterns
// (regexp, multiplierX, multiplierY, offsetX, offsetY, mapWidth, mapHeight) are supported.
//
// Its errors are *loaderr.Error values that give the line, column and field of the problem and how to fix it;
// a caller that read data from a file names it with loaderr.WithFile.
func ParseTiledWorld(data []byte) (*TiledWorld, error) {
	w, err := parseTiledWorld(data)
	return w, loaderr.Locate(err, "", data, fixTiledWorld)
}

func parseTiledWorld(data []byte) (*TiledWorld, error) {
	var raw rawTiledWorld
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Type != "" && raw.Type != "world" {
		return nil, &loaderr.Error{
			Field:    "type",
			Problem:  "tiled world type " + strconv.Quote(raw.Type) + " is not supported",
			Expected: `"world"`,
			Fix:      `set "type": "world" or save the file from Tiled as a .world file`,
		}
	}

	maps := make([]TiledWorldMap, len(raw.Maps))
	for i, rm := range raw.Maps {
		if rm.FileName == "" {
			return nil, loaderr.Errorf(loaderr.Path("maps", i, "fileName"), "set fileName to the map's path relative to the .world file", "world map entry %d has an empty fileName", i)
		}
		if rm.Width < 0 {
			return nil, loaderr.Errorf(loaderr.Path("maps", i, "width"), "set width to 0 or a positive pixel width", "world map %q has negative width %d", rm.FileName, rm.Width)
		}
		if rm.Height < 0 {
			return nil, loaderr.Errorf(loaderr.Path("maps", i, "height"), "set height to 0 or a positive pixel height", "world map %q has negative height %d", rm.FileName, rm.Height)
		}
		maps[i] = TiledWorldMap{
			FileName: rm.FileName,
			X:        rm.X,
			Y:        rm.Y,
			Width:    rm.Width,
			Height:   rm.Height,
		}
	}

	patterns := make([]TiledWorldPattern, len(raw.Patterns))
	for i, rp := range raw.Patterns {
		if rp.Regexp == "" {
			return nil, loaderr.Errorf(loaderr.Path("patterns", i, "regexp"), `give the pattern a regular expression with capturing groups for X and Y, such as "map_(-?\\d+)_(-?\\d+)\\.tmj"`, "world pattern %d has an empty regexp", i)
		}
		re, err := regexp.Compile(rp.Regexp)
		if err != nil {
			return nil, loaderr.Errorf(loaderr.Path("patterns", i, "regexp"), "fix the regular expression syntax so Go's regexp package accepts it", "world pattern %d invalid regexp %q: %w", i, rp.Regexp, err)
		}
		if re.NumSubexp() < 1 {
			return nil, loaderr.Errorf(loaderr.Path("patterns", i, "regexp"), `include capturing groups for the map's grid coordinates, such as "map_(-?\\d+)_(-?\\d+)\\.tmj"`, "world pattern %d regexp %q has no capturing groups", i, rp.Regexp)
		}
		if rp.MapWidth < 0 {
			return nil, loaderr.Errorf(loaderr.Path("patterns", i, "mapWidth"), "set mapWidth to 0 or a positive pixel width", "world pattern %d has negative mapWidth %d", i, rp.MapWidth)
		}
		if rp.MapHeight < 0 {
			return nil, loaderr.Errorf(loaderr.Path("patterns", i, "mapHeight"), "set mapHeight to 0 or a positive pixel height", "world pattern %d has negative mapHeight %d", i, rp.MapHeight)
		}
		p := TiledWorldPattern{
			Regexp:      rp.Regexp,
			MultiplierX: rp.MultiplierX,
			MultiplierY: rp.MultiplierY,
			OffsetX:     rp.OffsetX,
			OffsetY:     rp.OffsetY,
			MapWidth:    rp.MapWidth,
			MapHeight:   rp.MapHeight,
			re:          re,
		}
		p.MultiplierX, p.MultiplierY, p.MapWidth, p.MapHeight = p.effectiveMetrics()
		patterns[i] = p
	}

	return &TiledWorld{
		Maps:                 maps,
		Patterns:             patterns,
		OnlyShowAdjacentMaps: raw.OnlyShowAdjacentMaps,
	}, nil
}

// Resolve expands w into every concrete map placement: explicit Maps first (in file order),
// followed by any candidate file in fileNames matching Patterns (sorted lexicographically,
// with the first matching pattern winning and files already listed in Maps skipped).
func (w *TiledWorld) Resolve(fileNames []string) []TiledWorldMap {
	if w == nil {
		return nil
	}
	out := make([]TiledWorldMap, 0, len(w.Maps)+len(fileNames))
	seen := make(map[string]bool, len(w.Maps))
	for _, m := range w.Maps {
		out = append(out, m)
		seen[m.FileName] = true
		seen[path.Clean(m.FileName)] = true
	}
	if len(w.Patterns) == 0 || len(fileNames) == 0 {
		return out
	}
	candidates := slices.Clone(fileNames)
	slices.Sort(candidates)
	for _, fn := range candidates {
		clean := path.Clean(fn)
		if seen[fn] || seen[clean] {
			continue
		}
		for i := range w.Patterns {
			if m, ok := w.Patterns[i].Match(fn); ok {
				out = append(out, m)
				seen[fn] = true
				seen[clean] = true
				break
			}
		}
	}
	return out
}

// TiledWorldLoader configures how LoadTiledWorld reads .tmj map files and discovers pattern-matched filenames.
type TiledWorldLoader struct {
	// LoadMap reads the raw .tmj JSON bytes for fileName (as written in the .world file or returned by ListFiles).
	LoadMap func(fileName string) ([]byte, error)
	// ListFiles returns candidate map filenames (relative to the .world file) to match against Patterns.
	// Optional when the .world file defines no patterns.
	ListFiles func() ([]string, error)
	// Tilesets resolves external .tsj tilesets and images for each map when ResolveTilesets is nil.
	Tilesets TilesetLoader
	// ResolveTilesets optionally builds a map-specific TilesetLoader for fileName (for example when maps live in subdirectories).
	ResolveTilesets func(fileName string) TilesetLoader
}

// LoadTiledWorld parses a Tiled .world JSON file and builds a LevelWorld whose levels load lazily through loader.
//
// Each loaded *Tilemap has its Origin set to the map's (x, y) placement from the .world file.
// When an explicit map entry omits width or height, LoadTiledWorld reads the map's header via loader.LoadMap
// to populate exact world bounds for streaming; entries with explicit dimensions remain completely lazy until loaded.
func LoadTiledWorld(data []byte, loader TiledWorldLoader) (*LevelWorld, error) {
	tw, err := ParseTiledWorld(data)
	if err != nil {
		return nil, err
	}
	if loader.LoadMap == nil {
		return nil, loaderr.Errorf("", "set TiledWorldLoader.LoadMap to read the .tmj maps referenced by the world", "LoadTiledWorld requires TiledWorldLoader.LoadMap")
	}

	var files []string
	if len(tw.Patterns) > 0 && loader.ListFiles != nil {
		files, err = loader.ListFiles()
		if err != nil {
			return nil, loaderr.Errorf("patterns", "check that the world's directory can be listed", "list world pattern files: %w", err)
		}
	}

	placements := tw.Resolve(files)
	world := &LevelWorld{
		Layout: "Tiled",
		Levels: make([]WorldLevel, len(placements)),
	}

	for i, wm := range placements {
		origin := wm.Origin()
		bounds := wm.Bounds()
		if bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
			raw, err := loader.LoadMap(wm.FileName)
			if err != nil {
				return nil, loaderr.Errorf(loaderr.Path("maps", i, "fileName"), "check that the map file exists relative to the .world file", "load map %q: %w", wm.FileName, err)
			}
			b, err := peekTiledMapBounds(raw, origin)
			if err != nil {
				return nil, loaderr.WithFile(err, wm.FileName)
			}
			bounds = b
		}

		placement := wm
		world.Levels[i] = WorldLevel{
			ID:       placement.FileName,
			FileName: placement.FileName,
			Origin:   origin,
			Bounds:   bounds,
			Load: func() (*Tilemap, error) {
				raw, err := loader.LoadMap(placement.FileName)
				if err != nil {
					return nil, loaderr.Errorf("", "check that the map file exists relative to the .world file", "load map %q: %w", placement.FileName, err)
				}
				tsLoader := loader.Tilesets
				if loader.ResolveTilesets != nil {
					tsLoader = loader.ResolveTilesets(placement.FileName)
				}
				m, err := LoadTiled(raw, tsLoader)
				if err != nil {
					return nil, loaderr.WithFile(err, placement.FileName)
				}
				m.Origin = placement.Origin()
				return m, nil
			},
		}
	}
	return world, nil
}

func peekTiledMapBounds(data []byte, origin core.Point) (core.Bounds, error) {
	var hdr struct {
		Width         int     `json:"width"`
		Height        int     `json:"height"`
		TileWidth     float32 `json:"tilewidth"`
		TileHeight    float32 `json:"tileheight"`
		Orientation   string  `json:"orientation"`
		StaggerAxis   string  `json:"staggeraxis"`
		StaggerIndex  string  `json:"staggerindex"`
		HexSideLength float32 `json:"hexsidelength"`
	}
	if err := json.Unmarshal(data, &hdr); err != nil {
		return core.Bounds{}, loaderr.Locate(err, "", data, fixTiled)
	}
	orient, err := parseTiledOrientation(hdr.Orientation)
	if err != nil {
		return core.Bounds{}, loaderr.Locate(&loaderr.Error{Field: "orientation", Problem: err.Error(), Expected: `"orthogonal", "isometric", "staggered" or "hexagonal"`, Fix: "set the map's orientation to one of those in Tiled's map properties", Err: err}, "", data, fixTiled)
	}
	axis, err := parseTiledStaggerAxis(hdr.StaggerAxis)
	if err != nil {
		return core.Bounds{}, loaderr.Locate(&loaderr.Error{Field: "staggeraxis", Problem: err.Error(), Expected: `"x" or "y"`, Fix: "set the map's stagger axis to x or y in Tiled", Err: err}, "", data, fixTiled)
	}
	idx, err := parseTiledStaggerIndex(hdr.StaggerIndex)
	if err != nil {
		return core.Bounds{}, loaderr.Locate(&loaderr.Error{Field: "staggerindex", Problem: err.Error(), Expected: `"odd" or "even"`, Fix: "set the map's stagger index to odd or even in Tiled", Err: err}, "", data, fixTiled)
	}
	m := Tilemap{
		Width:         hdr.Width,
		Height:        hdr.Height,
		TileWidth:     hdr.TileWidth,
		TileHeight:    hdr.TileHeight,
		Origin:        origin,
		Orientation:   orient,
		StaggerAxis:   axis,
		StaggerIndex:  idx,
		HexSideLength: hdr.HexSideLength,
	}
	return m.WorldBounds(), nil
}

type rawTiledWorld struct {
	Maps                 []rawTiledWorldMap     `json:"maps"`
	Patterns             []rawTiledWorldPattern `json:"patterns"`
	OnlyShowAdjacentMaps bool                   `json:"onlyShowAdjacentMaps"`
	Type                 string                 `json:"type"`
}

type rawTiledWorldMap struct {
	FileName string `json:"fileName"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type rawTiledWorldPattern struct {
	Regexp      string `json:"regexp"`
	MultiplierX int    `json:"multiplierX"`
	MultiplierY int    `json:"multiplierY"`
	OffsetX     int    `json:"offsetX"`
	OffsetY     int    `json:"offsetY"`
	MapWidth    int    `json:"mapWidth"`
	MapHeight   int    `json:"mapHeight"`
}
