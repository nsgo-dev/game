package world3d

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"go.yaml.in/yaml/v3"
)

// GridMapFormatVersion is the current YAML grid map schema version.
const GridMapFormatVersion = 1

// GridMapSchemaURL is where the JSON Schema of version 1 grid map documents is published.
const GridMapSchemaURL = "https://game.nsgo.dev/schemas/gridmap.v1.json"

// GridMapSchemaComment is the first line [EncodeGridMap] writes so YAML language
// servers validate and complete grid map files automatically.
const GridMapSchemaComment = "# yaml-language-server: $schema=" + GridMapSchemaURL

const fixGridMapDoc = "correct the grid map document to match its version 1 schema in docs/en/world3d.md"

// GridMapDoc is the root of a versioned YAML modular 3D grid map document
// referencing a mesh library and placing tiles on an integer 3D lattice.
type GridMapDoc struct {
	// Version is the schema version; must be 1.
	Version int `yaml:"version" schema:"required"`
	// Name is an optional human-readable name for the grid map.
	Name string `yaml:"name,omitempty"`
	// Origin is the world-space [x, y, z] origin of grid cell (0, 0, 0); omitted defaults to [0, 0, 0].
	Origin *[3]float32 `yaml:"origin,omitempty"`
	// CellSize is the world-space [x, y, z] dimensions of one grid cell; omitted defaults to [1, 1, 1].
	CellSize *[3]float32 `yaml:"cellSize,omitempty"`
	// MeshLibrary is the optional asset path to the glTF 2.0 (.gltf) mesh library supplying named tiles.
	MeshLibrary string `yaml:"meshLibrary,omitempty"`
	// Boxes lists non-overlapping filled axis-aligned coordinate box runs placed before individual cells, written in canonical (y, z, x) order.
	Boxes []GridBoxDoc `yaml:"boxes,omitempty"`
	// Cells lists individual occupied cells placed in canonical (y, z, x) coordinate order.
	Cells []GridCellDoc `yaml:"cells,omitempty"`
}

// GridBoxDoc describes an axis-aligned box run of identical tiles placed from Min to Max (inclusive).
type GridBoxDoc struct {
	// Min is the inclusive minimum integer [x, y, z] grid coordinate of the filled box.
	Min *[3]int `yaml:"min" schema:"required"`
	// Max is the inclusive maximum integer [x, y, z] grid coordinate of the filled box; each axis must be >= min.
	Max *[3]int `yaml:"max" schema:"required"`
	// Tile is the name of the tile in the referenced mesh library.
	Tile string `yaml:"tile" schema:"required,minLength=1"`
	// Orientation is the orthogonal cube rotation index in 0..23 (0..3 are 0°, 90°, 180°, 270° around +Y); omitted defaults to 0.
	Orientation uint8 `yaml:"orientation,omitempty" schema:"minimum=0,maximum=23"`
}

// GridCellDoc describes one occupied cell in a [GridMapDoc].
type GridCellDoc struct {
	// At is the discrete integer [x, y, z] grid coordinate of the cell.
	At *[3]int `yaml:"at" schema:"required"`
	// Tile is the name of the tile in the referenced mesh library.
	Tile string `yaml:"tile" schema:"required,minLength=1"`
	// Orientation is the orthogonal cube rotation index in 0..23 (0..3 are 0°, 90°, 180°, 270° around +Y); omitted defaults to 0.
	Orientation uint8 `yaml:"orientation,omitempty" schema:"minimum=0,maximum=23"`
}

// EffectiveOrigin returns d.Origin as a [math3d.Vec3], defaulting to the zero vector when omitted.
func (d *GridMapDoc) EffectiveOrigin() math3d.Vec3 {
	if d == nil || d.Origin == nil {
		return math3d.Vec3Zero()
	}
	return math3d.Vec3{X: d.Origin[0], Y: d.Origin[1], Z: d.Origin[2]}
}

// EffectiveCellSize returns d.CellSize as a [math3d.Vec3], defaulting to (1, 1, 1) when omitted.
func (d *GridMapDoc) EffectiveCellSize() math3d.Vec3 {
	if d == nil || d.CellSize == nil {
		return math3d.Vec3{X: 1, Y: 1, Z: 1}
	}
	return math3d.Vec3{X: d.CellSize[0], Y: d.CellSize[1], Z: d.CellSize[2]}
}

// ParseGridMap decodes and validates a YAML grid map document. Unknown fields
// and trailing YAML documents are rejected. Returned errors are [*loaderr.Error]
// values carrying line, column, field path, and a fix hint.
func ParseGridMap(data []byte) (*GridMapDoc, error) {
	return ParseGridMapWith(data, "")
}

// ParseGridMapWith is [ParseGridMap] with an optional source file name attached to errors.
func ParseGridMapWith(data []byte, file string) (*GridMapDoc, error) {
	doc, err := parseGridMapDoc(data)
	if err != nil {
		return nil, loaderr.Locate(err, file, data, fixGridMapDoc)
	}
	return doc, nil
}

func parseGridMapDoc(data []byte) (*GridMapDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			Problem: "world3d: empty grid map document",
			Fix:     fixGridMapDoc,
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc GridMapDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("world3d: decode grid map: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    terrainTrailingDocumentLine(data),
				Problem: "world3d: unexpected trailing YAML document",
				Fix:     "keep one grid map per file; remove the --- and the document after it",
			}
		}
		return nil, fmt.Errorf("world3d: decode grid map: %w", err)
	}
	if err := ValidateGridMap(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// ValidateGridMap checks that doc conforms to the version 1 grid map schema.
func ValidateGridMap(doc *GridMapDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "world3d: nil grid map document", Fix: fixGridMapDoc}
	}
	if doc.Version != GridMapFormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("world3d: unsupported grid map version %d", doc.Version),
			Expected: strconv.Itoa(GridMapFormatVersion),
			Fix:      fmt.Sprintf("set version to %d", GridMapFormatVersion),
		}
	}
	if doc.Origin != nil {
		for i, v := range *doc.Origin {
			if !terrainFinite(v) {
				return &loaderr.Error{
					Field:    loaderr.Path("origin", i),
					Problem:  fmt.Sprintf("world3d: non-finite origin[%d] %v", i, v),
					Expected: "a finite number",
					Fix:      "use finite coordinates for origin",
				}
			}
		}
	}
	if doc.CellSize != nil {
		for i, v := range *doc.CellSize {
			if !terrainFinite(v) || v <= 0 {
				return &loaderr.Error{
					Field:    loaderr.Path("cellSize", i),
					Problem:  fmt.Sprintf("world3d: invalid cellSize[%d] %v", i, v),
					Expected: "a finite positive number",
					Fix:      "set every cellSize component to a positive number or omit cellSize to use [1, 1, 1]",
				}
			}
		}
	}

	type indexedBox struct {
		idx int
		min [3]int
		max [3]int
	}
	boxes := make([]indexedBox, 0, len(doc.Boxes))
	for i, b := range doc.Boxes {
		if b.Min == nil {
			return &loaderr.Error{
				Field:    loaderr.Path("boxes", i, "min"),
				Problem:  "world3d: box min coordinate is required",
				Expected: "[x, y, z] integer array",
				Fix:      "set min to the inclusive minimum [x, y, z] cell coordinate",
			}
		}
		if b.Max == nil {
			return &loaderr.Error{
				Field:    loaderr.Path("boxes", i, "max"),
				Problem:  "world3d: box max coordinate is required",
				Expected: "[x, y, z] integer array",
				Fix:      "set max to the inclusive maximum [x, y, z] cell coordinate",
			}
		}
		for axis := range 3 {
			if b.Min[axis] > b.Max[axis] {
				return &loaderr.Error{
					Field:    loaderr.Path("boxes", i, "max", axis),
					Problem:  fmt.Sprintf("world3d: box max[%d] (%d) is less than min[%d] (%d)", axis, b.Max[axis], axis, b.Min[axis]),
					Expected: fmt.Sprintf("integer >= %d", b.Min[axis]),
					Fix:      "ensure min[axis] <= max[axis] on every axis",
				}
			}
		}
		if b.Tile == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("boxes", i, "tile"),
				Problem:  "world3d: box tile name must not be empty",
				Expected: "non-empty tile name from the mesh library",
				Fix:      "set tile to the name of a tile in meshLibrary",
			}
		}
		if b.Orientation >= GridOrientationCount {
			return &loaderr.Error{
				Field:    loaderr.Path("boxes", i, "orientation"),
				Problem:  fmt.Sprintf("world3d: box orientation %d out of range", b.Orientation),
				Expected: fmt.Sprintf("integer in 0..%d", GridOrientationCount-1),
				Fix:      fmt.Sprintf("set orientation between 0 and %d", GridOrientationCount-1),
			}
		}
		boxes = append(boxes, indexedBox{idx: i, min: *b.Min, max: *b.Max})
	}
	if len(boxes) > 1 {
		slices.SortFunc(boxes, func(a, b indexedBox) int {
			return cmp.Or(
				cmp.Compare(a.min[1], b.min[1]),
				cmp.Compare(a.min[2], b.min[2]),
				cmp.Compare(a.min[0], b.min[0]),
				cmp.Compare(a.idx, b.idx),
			)
		})
		for i := range boxes {
			for j := i + 1; j < len(boxes); j++ {
				if boxes[j].min[1] > boxes[i].max[1] {
					break
				}
				if gridBoxesOverlap(boxes[i].min, boxes[i].max, boxes[j].min, boxes[j].max) {
					later := max(boxes[i].idx, boxes[j].idx)
					earlier := min(boxes[i].idx, boxes[j].idx)
					return &loaderr.Error{
						Field:   loaderr.Path("boxes", later),
						Problem: fmt.Sprintf("world3d: box %d overlaps box %d", later, earlier),
						Fix:     "split overlapping boxes into disjoint runs or override individual tiles under cells",
					}
				}
			}
		}
	}

	seenCell := make(map[GridCoord]int, len(doc.Cells))
	for i, c := range doc.Cells {
		if c.At == nil {
			return &loaderr.Error{
				Field:    loaderr.Path("cells", i, "at"),
				Problem:  "world3d: cell coordinate is required",
				Expected: "[x, y, z] integer array",
				Fix:      "set at to the cell's [x, y, z] integer coordinate",
			}
		}
		coord := GridCoord{X: c.At[0], Y: c.At[1], Z: c.At[2]}
		if prev, dup := seenCell[coord]; dup {
			return &loaderr.Error{
				Field:   loaderr.Path("cells", i, "at"),
				Problem: fmt.Sprintf("world3d: duplicate cell coordinate (%d, %d, %d) (first defined at cells[%d])", coord.X, coord.Y, coord.Z, prev),
				Fix:     "remove the duplicate cell entry so each coordinate appears at most once",
			}
		}
		seenCell[coord] = i
		if c.Tile == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("cells", i, "tile"),
				Problem:  "world3d: cell tile name must not be empty",
				Expected: "non-empty tile name from the mesh library",
				Fix:      "set tile to the name of a tile in meshLibrary",
			}
		}
		if c.Orientation >= GridOrientationCount {
			return &loaderr.Error{
				Field:    loaderr.Path("cells", i, "orientation"),
				Problem:  fmt.Sprintf("world3d: cell orientation %d out of range", c.Orientation),
				Expected: fmt.Sprintf("integer in 0..%d", GridOrientationCount-1),
				Fix:      fmt.Sprintf("set orientation between 0 and %d", GridOrientationCount-1),
			}
		}
	}
	return nil
}

func gridBoxesOverlap(aMin, aMax, bMin, bMax [3]int) bool {
	return aMin[0] <= bMax[0] && bMin[0] <= aMax[0] &&
		aMin[1] <= bMax[1] && bMin[1] <= aMax[1] &&
		aMin[2] <= bMax[2] && bMin[2] <= aMax[2]
}

func compareGridBoxes(a, b GridBoxDoc) int {
	return cmp.Or(
		cmp.Compare(a.Min[1], b.Min[1]),
		cmp.Compare(a.Min[2], b.Min[2]),
		cmp.Compare(a.Min[0], b.Min[0]),
		cmp.Compare(a.Max[1], b.Max[1]),
		cmp.Compare(a.Max[2], b.Max[2]),
		cmp.Compare(a.Max[0], b.Max[0]),
		cmp.Compare(a.Tile, b.Tile),
		cmp.Compare(a.Orientation, b.Orientation),
	)
}

func compareGridCells(a, b GridCellDoc) int {
	return cmp.Or(
		cmp.Compare(a.At[1], b.At[1]),
		cmp.Compare(a.At[2], b.At[2]),
		cmp.Compare(a.At[0], b.At[0]),
		cmp.Compare(a.Tile, b.Tile),
		cmp.Compare(a.Orientation, b.Orientation),
	)
}

// EncodeGridMap validates doc and serializes it as deterministic 2-space YAML
// prefixed with [GridMapSchemaComment], with boxes and cells sorted into
// canonical (y, z, x) order. The input doc is not modified.
func EncodeGridMap(doc *GridMapDoc) ([]byte, error) {
	if err := ValidateGridMap(doc); err != nil {
		return nil, err
	}
	canonical := *doc
	if len(doc.Boxes) > 0 {
		canonical.Boxes = slices.Clone(doc.Boxes)
		slices.SortFunc(canonical.Boxes, compareGridBoxes)
	}
	if len(doc.Cells) > 0 {
		canonical.Cells = slices.Clone(doc.Cells)
		slices.SortFunc(canonical.Cells, compareGridCells)
	}

	var buf bytes.Buffer
	buf.WriteString(GridMapSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&canonical); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("world3d: encode grid map: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("world3d: encode grid map: %w", err)
	}
	return buf.Bytes(), nil
}

// Build validates d and constructs a [*GridMap] populated with d's boxes and
// cells, resolving tile names against lib.
func (d *GridMapDoc) Build(lib *MeshLibrary) (*GridMap, error) {
	if err := ValidateGridMap(d); err != nil {
		return nil, err
	}
	if lib == nil && (len(d.Boxes) > 0 || len(d.Cells) > 0) {
		return nil, &loaderr.Error{
			Field:   "meshLibrary",
			Problem: "world3d: grid map requires a MeshLibrary to resolve tile names",
			Fix:     "pass a MeshLibrary containing the tiles named in boxes and cells",
		}
	}
	g := NewGridMap(d.EffectiveCellSize(), lib)
	g.Origin = d.EffectiveOrigin()

	for i, b := range d.Boxes {
		def, ok := lib.ByName(b.Tile)
		if !ok {
			return nil, &loaderr.Error{
				Field:   loaderr.Path("boxes", i, "tile"),
				Problem: fmt.Sprintf("world3d: unknown tile %q in mesh library", b.Tile),
				Fix:     "use a tile name present in the mesh library",
			}
		}
		g.FillBox(
			GridCoord{X: b.Min[0], Y: b.Min[1], Z: b.Min[2]},
			GridCoord{X: b.Max[0], Y: b.Max[1], Z: b.Max[2]},
			def.ID,
			b.Orientation,
		)
	}
	for i, c := range d.Cells {
		def, ok := lib.ByName(c.Tile)
		if !ok {
			return nil, &loaderr.Error{
				Field:   loaderr.Path("cells", i, "tile"),
				Problem: fmt.Sprintf("world3d: unknown tile %q in mesh library", c.Tile),
				Fix:     "use a tile name present in the mesh library",
			}
		}
		g.SetCell(GridCoord{X: c.At[0], Y: c.At[1], Z: c.At[2]}, def.ID, c.Orientation)
	}
	return g, nil
}

// Load loads d.MeshLibrary from fsys (when set, as glTF 2.0) and builds a
// runtime [*GridMap].
func (d *GridMapDoc) Load(fsys fs.FS) (*GridMap, error) {
	if err := ValidateGridMap(d); err != nil {
		return nil, err
	}
	var lib *MeshLibrary
	if d.MeshLibrary != "" {
		if fsys == nil {
			return nil, fmt.Errorf("world3d: nil filesystem")
		}
		gltfBytes, err := fs.ReadFile(fsys, d.MeshLibrary)
		if err != nil {
			return nil, &loaderr.Error{
				Field:   "meshLibrary",
				Problem: fmt.Sprintf("world3d: read mesh library %q", d.MeshLibrary),
				Err:     err,
				Fix:     "ensure meshLibrary points to an existing glTF 2.0 (.gltf) file",
			}
		}
		lib, err = LoadMeshLibraryFromGLTF(gltfBytes)
		if err != nil {
			return nil, loaderr.WithFile(err, d.MeshLibrary)
		}
	} else {
		lib = NewMeshLibrary()
	}
	return d.Build(lib)
}

// LoadGridMapFS reads the YAML grid map at name in fsys, loads its referenced
// glTF mesh library from fsys, and returns the constructed [*GridMap] alongside
// the parsed [*GridMapDoc].
func LoadGridMapFS(fsys fs.FS, name string) (*GridMap, *GridMapDoc, error) {
	if fsys == nil {
		return nil, nil, fmt.Errorf("world3d: nil filesystem")
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, nil, fmt.Errorf("world3d: read grid map %q: %w", name, err)
	}
	doc, err := ParseGridMapWith(data, name)
	if err != nil {
		return nil, nil, err
	}
	g, err := doc.Load(fsys)
	if err != nil {
		return nil, nil, loaderr.Locate(err, name, data, fixGridMapDoc)
	}
	return g, doc, nil
}

// ToDoc exports g as a canonical deterministic [*GridMapDoc], compressing
// contiguous axis-aligned runs of identical tiles into [GridBoxDoc] entries and
// emitting isolated cells as [GridCellDoc] entries in (y, z, x) order.
// Every occupied cell's TileID must resolve to a named [MeshTileDef] in g.Library.
func (g *GridMap) ToDoc(name, meshLibrary string) (*GridMapDoc, error) {
	doc := &GridMapDoc{
		Version:     GridMapFormatVersion,
		Name:        name,
		MeshLibrary: meshLibrary,
	}
	if g == nil {
		return doc, nil
	}
	if g.Origin != (math3d.Vec3{}) {
		o := [3]float32{g.Origin.X, g.Origin.Y, g.Origin.Z}
		doc.Origin = &o
	}
	if g.CellSize != (math3d.Vec3{}) && g.CellSize != (math3d.Vec3{X: 1, Y: 1, Z: 1}) {
		cs := [3]float32{g.CellSize.X, g.CellSize.Y, g.CellSize.Z}
		doc.CellSize = &cs
	}
	if g.count == 0 {
		return doc, nil
	}
	if g.Library == nil {
		return nil, fmt.Errorf("world3d: cannot export grid map with %d cells and nil Library", g.count)
	}

	type namedCell struct {
		tile   string
		orient uint8
	}
	coords := make([]GridCoord, 0, g.count)
	grid := make(map[GridCoord]namedCell, g.count)
	for _, ch := range g.chunks {
		for coord, cell := range ch.cells {
			def, ok := g.Library.Get(cell.TileID)
			if !ok || def.Name == "" {
				return nil, fmt.Errorf("world3d: cell (%d, %d, %d) tile ID %d has no named MeshTileDef in Library", coord.X, coord.Y, coord.Z, cell.TileID)
			}
			coords = append(coords, coord)
			grid[coord] = namedCell{tile: def.Name, orient: cell.Orientation}
		}
	}
	slices.SortFunc(coords, func(a, b GridCoord) int {
		return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.Z, b.Z), cmp.Compare(a.X, b.X))
	})

	visited := make(map[GridCoord]bool, len(coords))
	matches := func(c GridCoord, want namedCell) bool {
		if visited[c] {
			return false
		}
		got, ok := grid[c]
		return ok && got == want
	}

	for _, c := range coords {
		if visited[c] {
			continue
		}
		want := grid[c]
		x0, y0, z0 := c.X, c.Y, c.Z
		x1, y1, z1 := x0, y0, z0

		for matches(GridCoord{X: x1 + 1, Y: y0, Z: z0}, want) {
			x1++
		}
		for {
			ok := true
			for x := x0; x <= x1; x++ {
				if !matches(GridCoord{X: x, Y: y0, Z: z1 + 1}, want) {
					ok = false
					break
				}
			}
			if !ok {
				break
			}
			z1++
		}
		for {
			ok := true
			for z := z0; z <= z1 && ok; z++ {
				for x := x0; x <= x1; x++ {
					if !matches(GridCoord{X: x, Y: y1 + 1, Z: z}, want) {
						ok = false
						break
					}
				}
			}
			if !ok {
				break
			}
			y1++
		}

		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				for x := x0; x <= x1; x++ {
					visited[GridCoord{X: x, Y: y, Z: z}] = true
				}
			}
		}

		if x0 != x1 || y0 != y1 || z0 != z1 {
			lo := [3]int{x0, y0, z0}
			hi := [3]int{x1, y1, z1}
			doc.Boxes = append(doc.Boxes, GridBoxDoc{
				Min:         &lo,
				Max:         &hi,
				Tile:        want.tile,
				Orientation: want.orient,
			})
		} else {
			at := [3]int{x0, y0, z0}
			doc.Cells = append(doc.Cells, GridCellDoc{
				At:          &at,
				Tile:        want.tile,
				Orientation: want.orient,
			})
		}
	}
	return doc, nil
}
