package importsettings

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"io/fs"
	"maps"
	"math"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"go.yaml.in/yaml/v3"
)

// Suffix is appended to an asset's file name to name its sidecar, such as hero.png.import.yaml beside hero.png.
const Suffix = ".import.yaml"

// CurrentVersion is the sidecar schema version this package reads and writes.
const CurrentVersion = 1

// SchemaURL is where the JSON Schema of version 1 sidecars is published. Encode names it in a yaml-language-server comment on a sidecar's first line, so editors that run the YAML language server validate and complete the file as it is written.
const SchemaURL = "https://game.nsgo.dev/schemas/import.v1.json"

// SchemaComment is the first line Encode writes: a YAML comment that points a YAML language server at SchemaURL. Parse ignores it like any other comment.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

// FilterKey is the key of a KTX2 texture's key/value data that game assets and game tex write Image.Filter under and render3d.LoadKTX2 reads.
const FilterKey = "game.filter"

// Filters are the sampling filters an image's sidecar may name: "linear" blends neighboring texels when the image is scaled, "nearest" keeps each texel sharp, as pixel art wants.
var Filters = []string{"linear", "nearest"}

// Path returns the slash-separated path of the sidecar of the asset at the slash-separated path asset.
func Path(asset string) string {
	return asset + Suffix
}

// IsSidecar reports whether the file name or path name is a sidecar, so a scan for assets can skip it.
func IsSidecar(name string) bool {
	return strings.HasSuffix(name, Suffix)
}

// File is one asset's import settings and credit. It holds at least one section: at most one of Image, Audio, Font and Model, for how game assets imports the asset, and Credit for who made it and under what license.
type File struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Image holds the settings of an image (PNG or JPEG).
	Image *Image `yaml:"image,omitempty"`
	// Audio holds the settings of a sound (WAV).
	Audio *Audio `yaml:"audio,omitempty"`
	// Font holds the settings of a font (TrueType or OpenType) that a font target bakes into a bitmap font.
	Font *Font `yaml:"font,omitempty"`
	// Model holds the settings of a model (glTF or GLB) that a model target packs into one GLB file.
	Model *Model `yaml:"model,omitempty"`
	// Credit records who made the asset and its license, which game release and game credits list in the credits file; any kind of asset may carry one.
	Credit *Credit `yaml:"credit,omitempty"`
}

// Credit records where an asset comes from, for the credits file a release ships with.
type Credit struct {
	// Author is who made the asset, a person, a group or a company, as its license asks it to be attributed.
	Author string `yaml:"author" schema:"required,minLength=1"`
	// License is the asset's license as an SPDX license expression, such as "CC0-1.0", "CC-BY-4.0" or "MIT OR Apache-2.0"; a license with no SPDX identifier is named "LicenseRef-" followed by a name of its own, with its terms in Notice.
	License string `yaml:"license" schema:"required,minLength=1"`
	// Source is where the asset was obtained, usually a URL; empty for an asset made for the game.
	Source string `yaml:"source,omitempty"`
	// Notice is text the license requires to be shipped with the asset, such as a copyright line or a license's terms; it may span several lines.
	Notice string `yaml:"notice,omitempty"`
}

// Image holds how an image is imported. A texture target reads Filter, Format, Mipmaps and SRGB; an atlas target reads Slice, Pivot, Collision, Points and CellPoints. Each field left out keeps the target's setting in game.yaml.
type Image struct {
	// Filter is the sampling filter written into the texture's KTX2 key/value data under FilterKey, which render3d.LoadKTX2 sets the sampler's filters from: "linear" or "nearest".
	Filter string `yaml:"filter,omitempty" schema:"enum=linear|nearest"`
	// Format is the KTX2 compression format: "rgba8", "bc1", "bc3" or "etc2".
	Format string `yaml:"format,omitempty" schema:"enum=rgba8|bc1|bc3|etc2"`
	// Mipmaps writes the whole mipmap chain down to 1x1 when true and only the full-size image when false.
	Mipmaps *bool `yaml:"mipmaps,omitempty"`
	// SRGB marks the texture sRGB and filters its mipmaps in linear light when true.
	SRGB *bool `yaml:"srgb,omitempty"`
	// Slice cuts a sprite sheet into frames on a grid before an atlas packs them; nil packs the image as one frame.
	Slice *Slice `yaml:"slice,omitempty"`
	// Pivot sets the normalized pivot point(s) written to TexturePacker JSON when an atlas packs the image, as a default for every frame and optional per-cell overrides for a sliced sheet.
	Pivot *Pivot `yaml:"pivot,omitempty"`
	// Collision configures per-frame 2D collision shapes baked into packed atlas metadata when an atlas packs the image, as a default for every frame and optional per-cell overrides for a sliced sheet.
	Collision *Collision `yaml:"collision,omitempty"`
	// Points maps named image points (such as "muzzle" or "feet") to [x, y] pixel coordinates in the untrimmed frame, applied to an unsliced image and as defaults for every cell of a sliced sheet.
	Points map[string]Point2D `yaml:"points,omitempty"`
	// CellPoints overrides or adds named image points for individual 0-based cells of a sliced sprite sheet, in ascending cell index order.
	CellPoints []CellPoints `yaml:"cellPoints,omitempty"`
}

// PivotPoint is a normalized [x, y] pivot coordinate as fractions of the untrimmed frame's width and height: [0, 0] is the top-left corner, [0.5, 0.5] is the center, and [0.5, 1] is bottom-center.
type PivotPoint [2]float64

// UnmarshalYAML decodes a [x, y] sequence of two finite numbers into p.
func (p *PivotPoint) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.SequenceNode || len(value.Content) != 2 {
		return &loaderr.Error{
			Line:    value.Line,
			Column:  value.Column,
			Problem: "pivot must be a [x, y] sequence of 2 numbers",
			Fix:     "write pivot as a two-number flow list such as [0.5, 0.5]",
		}
	}
	for i, item := range value.Content {
		if item.Kind != yaml.ScalarNode || (item.ShortTag() != "!!int" && item.ShortTag() != "!!float") {
			return &loaderr.Error{
				Line:    item.Line,
				Column:  item.Column,
				Problem: fmt.Sprintf("pivot[%d] must be a number", i),
				Fix:     "set each pivot coordinate to a finite number such as 0.5",
			}
		}
		var v float64
		if err := item.Decode(&v); err != nil {
			return &loaderr.Error{
				Line:    item.Line,
				Column:  item.Column,
				Problem: fmt.Sprintf("pivot[%d]: %v", i, err),
				Fix:     "set each pivot coordinate to a finite number such as 0.5",
				Err:     err,
			}
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return &loaderr.Error{
				Line:    item.Line,
				Column:  item.Column,
				Problem: fmt.Sprintf("pivot[%d] must be a finite number", i),
				Fix:     "replace NaN or Inf with a finite number such as 0.5",
			}
		}
		if v == 0 {
			v = 0
		}
		p[i] = v
	}
	return nil
}

// MarshalYAML encodes p as a deterministic YAML flow sequence [x, y].
func (p PivotPoint) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for i, v := range p {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("pivot[%d] must be a finite number", i)
		}
		if v == 0 {
			v = 0
		}
		var elem yaml.Node
		if err := elem.Encode(v); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &elem)
	}
	return node, nil
}

// Pivot sets the normalized pivot points written into a packed atlas's TexturePacker frame metadata. Default applies to an unsliced image and to every cell of a sliced sheet without an entry in Cells.
type Pivot struct {
	// Default is the normalized [x, y] pivot applied to an unsliced image and to every cell of a sliced sheet not listed in Cells.
	Default *PivotPoint `yaml:"default,omitempty" schema:"minItems=2"`
	// Cells overrides the normalized [x, y] pivot for individual 0-based cells of a sliced sprite sheet, in ascending cell index order.
	Cells []CellPivot `yaml:"cells,omitempty"`
}

// CellPivot overrides the normalized pivot for one 0-based cell of a sliced sprite sheet.
type CellPivot struct {
	// Cell is the 0-based cell index (row by row from the top left), matching the frame suffix _<cell>.
	Cell int `yaml:"cell" schema:"required,minimum=0"`
	// Pivot is the normalized [x, y] pivot for this cell.
	Pivot PivotPoint `yaml:"pivot" schema:"required,minItems=2"`
}

// UnmarshalYAML decodes a CellPivot mapping, requiring both cell and pivot keys and refusing unknown or repeated keys.
func (c *CellPivot) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return &loaderr.Error{
			Line:    value.Line,
			Column:  value.Column,
			Problem: "cell pivot must be a mapping with cell and pivot",
			Fix:     "write each cell pivot entry as a mapping with cell and pivot fields",
		}
	}
	var hasCell, hasPivot bool
	for i := 0; i+1 < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		switch k.Value {
		case "cell":
			if hasCell {
				return &loaderr.Error{
					Line:    k.Line,
					Column:  k.Column,
					Field:   "cell",
					Problem: "duplicate field \"cell\" in cell pivot",
					Fix:     "remove the duplicate cell key",
				}
			}
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int" {
				return &loaderr.Error{
					Line:    v.Line,
					Column:  v.Column,
					Field:   "cell",
					Problem: "cell must be an integer",
					Fix:     "set cell to a non-negative integer index such as 0",
				}
			}
			if err := v.Decode(&c.Cell); err != nil {
				return &loaderr.Error{
					Line:    v.Line,
					Column:  v.Column,
					Field:   "cell",
					Problem: fmt.Sprintf("cell: %v", err),
					Fix:     "set cell to a non-negative integer index such as 0",
					Err:     err,
				}
			}
			hasCell = true
		case "pivot":
			if hasPivot {
				return &loaderr.Error{
					Line:    k.Line,
					Column:  k.Column,
					Field:   "pivot",
					Problem: "duplicate field \"pivot\" in cell pivot",
					Fix:     "remove the duplicate pivot key",
				}
			}
			if err := v.Decode(&c.Pivot); err != nil {
				if le, ok := loaderr.As(err); ok {
					return le
				}
				return &loaderr.Error{
					Line:    v.Line,
					Column:  v.Column,
					Field:   "pivot",
					Problem: fmt.Sprintf("pivot: %v", err),
					Fix:     "write pivot as a two-number sequence [x, y]",
					Err:     err,
				}
			}
			hasPivot = true
		default:
			return &loaderr.Error{
				Line:    k.Line,
				Column:  k.Column,
				Field:   k.Value,
				Problem: fmt.Sprintf("field %s not found in type importsettings.CellPivot", k.Value),
				Fix:     "remove the unknown key; cell pivot entries accept only cell and pivot",
			}
		}
	}
	if !hasCell {
		return &loaderr.Error{
			Line:    value.Line,
			Column:  value.Column,
			Field:   "cell",
			Problem: "cell is required in cell pivot",
			Fix:     "add cell: <index> to the cell pivot entry",
		}
	}
	if !hasPivot {
		return &loaderr.Error{
			Line:    value.Line,
			Column:  value.Column,
			Field:   "pivot",
			Problem: "pivot is required in cell pivot",
			Fix:     "add pivot: [x, y] to the cell pivot entry",
		}
	}
	return nil
}

// ForImage returns the pivot for an unsliced image, or false when no default pivot is set.
func (p *Pivot) ForImage() (PivotPoint, bool) {
	if p == nil || p.Default == nil {
		return PivotPoint{}, false
	}
	return *p.Default, true
}

// ForCell returns the effective pivot for the 0-based sliced cell, preferring an explicit override in Cells and falling back to Default, or false when neither is set.
func (p *Pivot) ForCell(cell int) (PivotPoint, bool) {
	if p == nil {
		return PivotPoint{}, false
	}
	for _, c := range p.Cells {
		if c.Cell == cell {
			return c.Pivot, true
		}
	}
	if p.Default != nil {
		return *p.Default, true
	}
	return PivotPoint{}, false
}

// Slice is the grid a sprite sheet is cut into, in pixels. Its cells are taken row by row from the top left, and frame i of the image name.png is packed as name_i.png.
type Slice struct {
	// Width and Height are each cell's size; both must be positive.
	Width  int `yaml:"width" schema:"required,minimum=1"`
	Height int `yaml:"height" schema:"required,minimum=1"`
	// Margin is the gap between the image's top and left edges and the first row and column of cells.
	Margin int `yaml:"margin,omitempty" schema:"minimum=0"`
	// Spacing is the gap between neighboring cells.
	Spacing int `yaml:"spacing,omitempty" schema:"minimum=0"`
	// Count is how many cells are frames, counted from the first; 0 takes every cell that fits.
	Count int `yaml:"count,omitempty" schema:"minimum=0"`
}

// MaxSliceCells is the maximum number of cell rectangles [Slice.Cells] will
// materialize for one sprite sheet to prevent excessive memory allocation.
const MaxSliceCells = 65536

// GridCounts validates s against bounds and returns the resulting cell count,
// column count, and row count without allocating cell rectangles. It fails when
// s is invalid, when no cell fits, when the grid cell count overflows int, or
// when Count exceeds the cells that fit.
func (s Slice) GridCounts(bounds image.Rectangle) (count, cols, rows int, err error) {
	if s.Width <= 0 || s.Height <= 0 {
		return 0, 0, 0, fmt.Errorf("slice: width %d and height %d must be positive", s.Width, s.Height)
	}
	if s.Margin < 0 || s.Spacing < 0 || s.Count < 0 {
		return 0, 0, 0, fmt.Errorf("slice: margin %d, spacing %d and count %d must not be negative", s.Margin, s.Spacing, s.Count)
	}
	cols = cellsAlong(bounds.Dx(), s.Width, s.Margin, s.Spacing)
	rows = cellsAlong(bounds.Dy(), s.Height, s.Margin, s.Spacing)
	if cols == 0 || rows == 0 {
		return 0, 0, 0, fmt.Errorf("no %dx%d cell fits the %dx%d image after a margin of %d", s.Width, s.Height, bounds.Dx(), bounds.Dy(), s.Margin)
	}
	if int64(cols) > int64(math.MaxInt)/int64(rows) {
		if s.Count > 0 {
			return s.Count, cols, rows, nil
		}
		return 0, cols, rows, fmt.Errorf("slice grid %dx%d exceeds supported cell count", cols, rows)
	}
	n := cols * rows
	if s.Count > n {
		return 0, cols, rows, fmt.Errorf("slice count %d is more than the %d cells that fit the %dx%d image", s.Count, n, bounds.Dx(), bounds.Dy())
	}
	if s.Count > 0 {
		n = s.Count
	}
	return n, cols, rows, nil
}

// CellRect computes the pixel rectangle for 0-based cell in a grid with cols
// columns across bounds without allocating a full slice of rectangles.
func (s Slice) CellRect(bounds image.Rectangle, cols, cell int) (image.Rectangle, bool) {
	if cols <= 0 || cell < 0 || s.Width <= 0 || s.Height <= 0 || s.Margin < 0 || s.Spacing < 0 {
		return image.Rectangle{}, false
	}
	col := cell % cols
	row := cell / cols
	var dx, dy int
	if col > 0 {
		if s.Spacing > math.MaxInt-s.Width {
			return image.Rectangle{}, false
		}
		strideX := s.Width + s.Spacing
		if int64(col) > int64(math.MaxInt)/int64(strideX) {
			return image.Rectangle{}, false
		}
		dx = col * strideX
	}
	if row > 0 {
		if s.Spacing > math.MaxInt-s.Height {
			return image.Rectangle{}, false
		}
		strideY := s.Height + s.Spacing
		if int64(row) > int64(math.MaxInt)/int64(strideY) {
			return image.Rectangle{}, false
		}
		dy = row * strideY
	}
	x := bounds.Min.X + s.Margin + dx
	y := bounds.Min.Y + s.Margin + dy
	return image.Rect(x, y, x+s.Width, y+s.Height), true
}

// Cells returns the pixel rectangles of every cell s cuts from an image with bounds, row by row from the top left, along with the grid's column and row counts. It fails when s is invalid, when no cell fits, when Count exceeds the cells that fit, or when the resulting cell count exceeds [MaxSliceCells].
func (s Slice) Cells(bounds image.Rectangle) (rects []image.Rectangle, cols, rows int, err error) {
	n, cols, rows, err := s.GridCounts(bounds)
	if err != nil {
		return nil, cols, rows, err
	}
	if n > MaxSliceCells {
		return nil, cols, rows, fmt.Errorf("slice produces %d cells, exceeding maximum %d", n, MaxSliceCells)
	}
	rects = make([]image.Rectangle, n)
	for i := range n {
		r, ok := s.CellRect(bounds, cols, i)
		if !ok {
			return nil, cols, rows, fmt.Errorf("slice cell %d coordinates overflow", i)
		}
		rects[i] = r
	}
	return rects, cols, rows, nil
}

// cellsAlong returns how many cells of size cell, spaced spacing apart, fit along size after margin, as Tiled counts a tileset image's columns.
func cellsAlong(size, cell, margin, spacing int) int {
	if size <= 0 || cell <= 0 || margin < 0 || spacing < 0 || margin >= size {
		return 0
	}
	rem := size - margin
	if rem < cell {
		return 0
	}
	if spacing > math.MaxInt-cell {
		return 1
	}
	return (rem-cell)/(cell+spacing) + 1
}

// Audio holds how a sound is converted by an audio target. Each field left out keeps the target's setting in game.yaml.
type Audio struct {
	// Format is the output format: "wav", "aac" or "mp3".
	Format string `yaml:"format,omitempty" schema:"enum=wav|aac|mp3"`
	// Rate is the output sample rate in Hz.
	Rate int `yaml:"rate,omitempty" schema:"minimum=0"`
	// Channels is 1 for mono or 2 for stereo.
	Channels int `yaml:"channels,omitempty" schema:"minimum=0,maximum=2"`
	// PeakDB normalizes the peak to this level in dBFS; it may not be set with RMSDB. Setting either replaces the target's normalization.
	PeakDB *float64 `yaml:"peakDB,omitempty"`
	// RMSDB normalizes the loudness to this level in dBFS with soft clipping; it may not be set with PeakDB.
	RMSDB *float64 `yaml:"rmsDB,omitempty"`
	// Bitrate is the AAC or MP3 bitrate in kbps.
	Bitrate int `yaml:"bitrate,omitempty" schema:"minimum=0"`
	// Loop is the region a player repeats, written into a WAV output's smpl chunk; AAC and MP3 carry no loop points, so a sound with a loop must be converted to WAV.
	Loop *Loop `yaml:"loop,omitempty"`
}

// Loop is a region of a sound in sample frames of the source file, from Start up to, but not including, End. A target that resamples the sound scales it to the new rate.
type Loop struct {
	// Start is the first frame of the loop.
	Start int `yaml:"start" schema:"minimum=0"`
	// End is the frame after the loop's last one; 0 is the end of the sound.
	End int `yaml:"end,omitempty" schema:"minimum=0"`
}

// Font holds how a font target bakes a TrueType or OpenType font into an AngelCode BMFont. Each field left out keeps the target's setting in game.yaml.
type Font struct {
	// Size is the font size in pixels the glyphs are drawn at; 0 keeps the target's.
	Size int `yaml:"size,omitempty" schema:"minimum=0,maximum=2048"`
	// Chars are the characters baked into the font, each once, in any order, such as " 0123456789"; empty keeps the target's.
	Chars string `yaml:"chars,omitempty"`
	// Padding is the transparent gap around each glyph on the font's pages, in pixels.
	Padding *int `yaml:"padding,omitempty" schema:"minimum=0"`
}

// ModelLOD describes one level of detail of a glTF model (Model.LODs), ordered from finest (LOD 0) to coarsest.
type ModelLOD struct {
	// Model optionally names a separate glTF or GLB model file (slash-separated path from the project root) providing this level's geometry; empty uses this model file.
	Model string `yaml:"model,omitempty"`
	// Node optionally names a node inside the model (this file, or Model when set) providing this level's geometry; cannot be combined with Mesh.
	Node string `yaml:"node,omitempty"`
	// Mesh optionally names a mesh inside the model (this file, or Model when set) providing this level's geometry; cannot be combined with Node.
	Mesh string `yaml:"mesh,omitempty"`
	// MinScreenRatio is the minimum projected viewport-height fraction in [0, 1] at which this level is drawn (render3d.LODLevel.MinScreenRatio). Levels must strictly decrease in MinScreenRatio; 0 marks an unbounded coarsest fallback.
	MinScreenRatio float32 `yaml:"minScreenRatio,omitempty" schema:"minimum=0,maximum=1"`
	// MaxDistance is an optional maximum camera distance in world units at which this level is drawn (render3d.LODLevel.MaxDistance); 0 disables distance-based switching for this level.
	MaxDistance float32 `yaml:"maxDistance,omitempty" schema:"minimum=0"`
}

// Model holds how a model target packs a glTF model into one GLB file and how scenemodel builds its levels of detail. Each field left out keeps the target's setting in game.yaml.
type Model struct {
	// Scale multiplies the size of every scene of the model, such as 0.01 for a model exported in centimeters; 0 keeps the target's.
	Scale float64 `yaml:"scale,omitempty" schema:"minimum=0"`
	// Animations keeps the model's animations when true and leaves them out of the GLB when false.
	Animations *bool `yaml:"animations,omitempty"`
	// LODs declares the model's levels of detail from finest (index 0) to coarsest, with screen-ratio (and optional distance) thresholds. When omitted, scenemodel.Build pairs glTF nodes following the _LOD1, _LOD2 naming convention with default screen-ratio thresholds.
	LODs []ModelLOD `yaml:"lods,omitempty"`
	// LODHysteresis is the optional relative deadband in [0, 0.5] used by render3d.Queue to prevent LOD popping near thresholds.
	LODHysteresis float32 `yaml:"lodHysteresis,omitempty" schema:"minimum=0,maximum=0.5"`
}

// Sections returns the names of the import sections f holds, of "image", "audio", "font" and "model" in that order; Validate allows at most one.
func (f *File) Sections() []string {
	var names []string
	for _, s := range []struct {
		name string
		set  bool
	}{{"image", f.Image != nil}, {"audio", f.Audio != nil}, {"font", f.Font != nil}, {"model", f.Model != nil}} {
		if s.set {
			names = append(names, s.name)
		}
	}
	return names
}

// Parse decodes a sidecar and validates it.
func Parse(data []byte) (*File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add an import sidecar starting with version: %d and at least one section", CurrentVersion), "importsettings: empty sidecar"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("importsettings: decode: %w", err), "", data, "check the YAML syntax and field names in the import sidecar")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the import sidecar and remove any --- separator", "importsettings: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("importsettings: decode: %w", err), "", data, "remove trailing YAML content after the import sidecar")
	}
	if err := f.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	if f.Image != nil {
		f.Image.normalize()
	}
	if f.Audio != nil {
		f.Audio.normalize()
	}
	if f.Model != nil {
		f.Model.normalize()
	}
	return &f, nil
}

// Encode validates f and writes it as deterministic 2-space YAML, SchemaComment on its first line, that Parse reads back unchanged.
func Encode(f *File) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if f.Image != nil && (f.Image.Pivot != nil || f.Image.Collision != nil || len(f.Image.Points) > 0 || len(f.Image.CellPoints) > 0) {
		c, im := *f, *f.Image
		if f.Image.Pivot != nil {
			pv := *f.Image.Pivot
			if pv.Default != nil {
				d := *pv.Default
				pv.Default = &d
			}
			if len(pv.Cells) > 0 {
				pv.Cells = slices.Clone(pv.Cells)
			}
			im.Pivot = &pv
		}
		if f.Image.Collision != nil {
			im.Collision = cloneCollision(f.Image.Collision)
		}
		if len(f.Image.Points) > 0 {
			im.Points = maps.Clone(f.Image.Points)
		}
		if len(f.Image.CellPoints) > 0 {
			im.CellPoints = cloneCellPoints(f.Image.CellPoints)
		}
		im.normalize()
		c.Image = &im
		f = &c
	}
	if f.Audio != nil {
		c, au := *f, *f.Audio
		au.normalize()
		c.Audio = &au
		f = &c
	}
	if f.Model != nil {
		c, m := *f, *f.Model
		m.LODs = slices.Clone(m.LODs)
		m.normalize()
		c.Model = &m
		f = &c
	}
	var buf bytes.Buffer
	buf.WriteString(SchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("importsettings: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("importsettings: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks f: the version, at least one section and at most one of image, audio, font and model, known formats and filters, values in range, and a credit with an author and a valid SPDX license expression.
func (f *File) Validate() error {
	if f == nil {
		return loaderr.Errorf("", "pass a non-nil *importsettings.File to Validate", "importsettings: nil sidecar")
	}
	if f.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the import sidecar", CurrentVersion), "importsettings: version %d is not supported; want %d", f.Version, CurrentVersion)
	}
	if f.Credit != nil {
		if err := f.Credit.validate(); err != nil {
			return err
		}
	}
	switch sections := f.Sections(); {
	case len(sections) == 0:
		if f.Credit == nil {
			return loaderr.Errorf("", "add an image, audio, font, model, or credit section to the sidecar", "importsettings: a sidecar needs an image, an audio, a font, a model or a credit section")
		}
	case len(sections) > 1:
		return loaderr.Errorf(sections[1], fmt.Sprintf("keep only one import section (image, audio, font, or model) per sidecar and remove %s", sections[1]), "importsettings: a sidecar holds one of the image, audio, font and model sections, not %s", strings.Join(sections, " and "))
	case f.Image != nil:
		if err := f.Image.validate(); err != nil {
			return err
		}
	case f.Audio != nil:
		if err := f.Audio.validate(); err != nil {
			return err
		}
	case f.Font != nil:
		if err := f.Font.validate(); err != nil {
			return err
		}
	default:
		if err := f.Model.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (im *Image) validate() error {
	if im.Filter != "" && !slices.Contains(Filters, im.Filter) {
		return loaderr.Errorf("image.filter", fmt.Sprintf("set image.filter to %s", strings.Join(Filters, " or ")), "importsettings: image: filter %q must be %s", im.Filter, strings.Join(Filters, " or "))
	}
	if im.Format != "" && !slices.Contains(project.TextureFormats, im.Format) {
		return loaderr.Errorf("image.format", fmt.Sprintf("set image.format to one of %s", strings.Join(project.TextureFormats, ", ")), "importsettings: image: format %q must be %s", im.Format, strings.Join(project.TextureFormats, ", "))
	}
	if s := im.Slice; s != nil {
		if s.Width <= 0 || s.Height <= 0 {
			field := "image.slice.width"
			if s.Width > 0 {
				field = "image.slice.height"
			}
			return loaderr.Errorf(field, "set both image.slice.width and image.slice.height to positive pixel counts", "importsettings: image: slice: width %d and height %d must be positive", s.Width, s.Height)
		}
		for _, v := range []struct {
			name string
			n    int
		}{{"margin", s.Margin}, {"spacing", s.Spacing}, {"count", s.Count}} {
			if v.n < 0 {
				return loaderr.Errorf("image.slice."+v.name, fmt.Sprintf("set image.slice.%s to 0 or a positive integer", v.name), "importsettings: image: slice: %s %d must not be negative", v.name, v.n)
			}
		}
	}
	if p := im.Pivot; p != nil {
		if p.Default == nil && len(p.Cells) == 0 {
			return loaderr.Errorf("image.pivot", "set image.pivot.default or at least one entry in image.pivot.cells", "importsettings: image: pivot: must set default or at least one cell")
		}
		if p.Default != nil {
			if err := validatePivotPoint("image.pivot.default", "default", *p.Default); err != nil {
				return err
			}
		}
		if len(p.Cells) > 0 && im.Slice == nil {
			return loaderr.Errorf("image.pivot.cells", "add an image.slice grid or remove image.pivot.cells", "importsettings: image: pivot: cells require image.slice")
		}
		for i, c := range p.Cells {
			cPath := fmt.Sprintf("image.pivot.cells[%d]", i)
			if c.Cell < 0 {
				return loaderr.Errorf(cPath+".cell", "set cell to a non-negative integer index", "importsettings: image: pivot: cell %d must not be negative", c.Cell)
			}
			if i > 0 {
				prev := p.Cells[i-1].Cell
				if c.Cell == prev {
					return loaderr.Errorf(cPath+".cell", "remove the duplicate cell pivot entry", "importsettings: image: pivot: duplicate cell %d", c.Cell)
				}
				if c.Cell < prev {
					return loaderr.Errorf(cPath+".cell", "sort image.pivot.cells in ascending cell index order", "importsettings: image: pivot: cells must be in ascending cell order (%d before %d)", prev, c.Cell)
				}
			}
			if im.Slice != nil && im.Slice.Count > 0 && c.Cell >= im.Slice.Count {
				return loaderr.Errorf(cPath+".cell", fmt.Sprintf("set cell to an index less than image.slice.count (%d)", im.Slice.Count), "importsettings: image: pivot: cell %d is out of range for slice count %d", c.Cell, im.Slice.Count)
			}
			if err := validatePivotPoint(cPath+".pivot", fmt.Sprintf("cell %d", c.Cell), c.Pivot); err != nil {
				return err
			}
		}
	}
	if col := im.Collision; col != nil {
		if col.Mode == "" && len(col.Cells) == 0 {
			return errors.New("collision: must set mode or at least one cell")
		}
		if col.Mode == "" {
			if col.Tolerance != 0 || col.MaxVertices != 0 || len(col.Points) > 0 {
				return errors.New("collision: tolerance, maxVertices and points require mode")
			}
		} else {
			if err := validateCollisionSpec("default", col.Mode, col.Tolerance, col.MaxVertices, col.Points); err != nil {
				return err
			}
		}
		if len(col.Cells) > 0 && im.Slice == nil {
			return errors.New("collision: cells require image.slice")
		}
		for i, c := range col.Cells {
			if c.Cell < 0 {
				return fmt.Errorf("collision: cell %d must not be negative", c.Cell)
			}
			if i > 0 {
				prev := col.Cells[i-1].Cell
				if c.Cell == prev {
					return fmt.Errorf("collision: duplicate cell %d", c.Cell)
				}
				if c.Cell < prev {
					return fmt.Errorf("collision: cells must be in ascending cell order (%d before %d)", prev, c.Cell)
				}
			}
			if im.Slice != nil && im.Slice.Count > 0 && c.Cell >= im.Slice.Count {
				return fmt.Errorf("collision: cell %d is out of range for slice count %d", c.Cell, im.Slice.Count)
			}
			if err := validateCollisionSpec(fmt.Sprintf("cell %d", c.Cell), c.Mode, c.Tolerance, c.MaxVertices, c.Points); err != nil {
				return err
			}
		}
	}
	if len(im.Points) > 0 {
		if err := validatePointMap("points", im.Points); err != nil {
			return err
		}
	}
	if len(im.CellPoints) > 0 {
		if im.Slice == nil {
			return errors.New("cellPoints: require image.slice")
		}
		for i, cp := range im.CellPoints {
			if cp.Cell < 0 {
				return fmt.Errorf("cellPoints: cell %d must not be negative", cp.Cell)
			}
			if i > 0 {
				prev := im.CellPoints[i-1].Cell
				if cp.Cell == prev {
					return fmt.Errorf("cellPoints: duplicate cell %d", cp.Cell)
				}
				if cp.Cell < prev {
					return fmt.Errorf("cellPoints: cells must be in ascending cell order (%d before %d)", prev, cp.Cell)
				}
			}
			if im.Slice != nil && im.Slice.Count > 0 && cp.Cell >= im.Slice.Count {
				return fmt.Errorf("cellPoints: cell %d is out of range for slice count %d", cp.Cell, im.Slice.Count)
			}
			if len(cp.Points) == 0 {
				return fmt.Errorf("cellPoints: cell %d must define at least one point", cp.Cell)
			}
			if err := validatePointMap(fmt.Sprintf("cellPoints[%d]", cp.Cell), cp.Points); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePivotPoint(field, label string, pt PivotPoint) error {
	for i, v := range pt {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return loaderr.Errorf(fmt.Sprintf("%s[%d]", field, i), "replace NaN or Inf with a finite pivot coordinate such as 0.5", "importsettings: image: pivot: %s[%d] must be a finite number", label, i)
		}
	}
	return nil
}

// normalize replaces negative zero coordinates with zero so Encode writes the same bytes Parse reads back.
func (im *Image) normalize() {
	if im.Pivot != nil {
		if im.Pivot.Default != nil {
			d := *im.Pivot.Default
			if d[0] == 0 {
				d[0] = 0
			}
			if d[1] == 0 {
				d[1] = 0
			}
			im.Pivot.Default = &d
		}
		for i := range im.Pivot.Cells {
			if im.Pivot.Cells[i].Pivot[0] == 0 {
				im.Pivot.Cells[i].Pivot[0] = 0
			}
			if im.Pivot.Cells[i].Pivot[1] == 0 {
				im.Pivot.Cells[i].Pivot[1] = 0
			}
		}
	}
	if im.Collision != nil {
		for i := range im.Collision.Points {
			normalizePoint2D(&im.Collision.Points[i])
		}
		for i := range im.Collision.Cells {
			for j := range im.Collision.Cells[i].Points {
				normalizePoint2D(&im.Collision.Cells[i].Points[j])
			}
		}
	}
	for k, pt := range im.Points {
		normalizePoint2D(&pt)
		im.Points[k] = pt
	}
	for i := range im.CellPoints {
		for k, pt := range im.CellPoints[i].Points {
			normalizePoint2D(&pt)
			im.CellPoints[i].Points[k] = pt
		}
	}
}

func (fo *Font) validate() error {
	if err := project.ValidateFontSize(fo.Size); err != nil {
		return loaderr.Errorf("font.size", "set font.size to 0 (inherit target) or an integer in [1, 2048]", "importsettings: font: %w", err)
	}
	if fo.Padding != nil && *fo.Padding < 0 {
		return loaderr.Errorf("font.padding", "set font.padding to 0 or a positive integer", "importsettings: font: padding %d must not be negative", *fo.Padding)
	}
	if err := project.ValidateFontChars(fo.Chars); err != nil {
		return loaderr.Errorf("font.chars", "remove duplicate or invalid characters from font.chars", "importsettings: font: %w", err)
	}
	return nil
}

func (m *Model) validate() error {
	if err := project.ValidateModelScale(m.Scale); err != nil {
		return loaderr.Errorf("model.scale", "set model.scale to 0 (inherit target) or a finite positive number", "importsettings: model: %w", err)
	}
	if math.IsNaN(float64(m.LODHysteresis)) || math.IsInf(float64(m.LODHysteresis), 0) || m.LODHysteresis < 0 || m.LODHysteresis > 0.5 {
		return loaderr.Errorf("model.lodHysteresis", "set model.lodHysteresis to a finite number in [0, 0.5]", "importsettings: model: lodHysteresis %v must be finite and in [0, 0.5]", m.LODHysteresis)
	}
	for i, lv := range m.LODs {
		lPath := fmt.Sprintf("model.lods[%d]", i)
		if lv.Model != "" {
			if !fs.ValidPath(lv.Model) || lv.Model == "." {
				return loaderr.Errorf(lPath+".model", "set model to a slash-separated path relative to the project root", "importsettings: model: lods[%d]: model %q must be a slash-separated path relative to the project root", i, lv.Model)
			}
			ext := strings.ToLower(path.Ext(lv.Model))
			if ext != ".gltf" && ext != ".glb" {
				return loaderr.Errorf(lPath+".model", "point model to a .gltf or .glb file", "importsettings: model: lods[%d]: model %q must be a glTF file ending in .gltf or .glb", i, lv.Model)
			}
		}
		if lv.Node != "" && lv.Mesh != "" {
			return loaderr.Errorf(lPath, "set either node or mesh on the LOD entry, not both", "importsettings: model: lods[%d]: node %q and mesh %q may not both be set", i, lv.Node, lv.Mesh)
		}
		if math.IsNaN(float64(lv.MinScreenRatio)) || math.IsInf(float64(lv.MinScreenRatio), 0) || lv.MinScreenRatio < 0 || lv.MinScreenRatio > 1 {
			return loaderr.Errorf(lPath+".minScreenRatio", "set minScreenRatio to a finite number in [0, 1]", "importsettings: model: lods[%d]: minScreenRatio %v must be finite and in [0, 1]", i, lv.MinScreenRatio)
		}
		if math.IsNaN(float64(lv.MaxDistance)) || math.IsInf(float64(lv.MaxDistance), 0) || lv.MaxDistance < 0 {
			return loaderr.Errorf(lPath+".maxDistance", "set maxDistance to 0 or a finite positive distance", "importsettings: model: lods[%d]: maxDistance %v must be finite and non-negative", i, lv.MaxDistance)
		}
		if i < len(m.LODs)-1 && lv.MinScreenRatio == 0 && lv.MaxDistance == 0 {
			return loaderr.Errorf(lPath, "set a positive minScreenRatio or maxDistance on every LOD level before the last", "importsettings: model: lods[%d]: only the coarsest (last) LOD level may leave both minScreenRatio and maxDistance zero", i)
		}
		if i > 0 {
			prev := m.LODs[i-1]
			if prev.MinScreenRatio > 0 && lv.MinScreenRatio > 0 && lv.MinScreenRatio >= prev.MinScreenRatio {
				return loaderr.Errorf(lPath+".minScreenRatio", fmt.Sprintf("order LOD levels from finest to coarsest so minScreenRatio is less than %v", prev.MinScreenRatio), "importsettings: model: lods[%d]: minScreenRatio %v must be less than lods[%d] (%v)", i, lv.MinScreenRatio, i-1, prev.MinScreenRatio)
			}
			if prev.MinScreenRatio == 0 && lv.MinScreenRatio > 0 {
				return loaderr.Errorf(lPath+".minScreenRatio", "set minScreenRatio on earlier LOD levels or set this level's minScreenRatio to 0", "importsettings: model: lods[%d]: minScreenRatio %v cannot follow lods[%d] with minScreenRatio 0", i, lv.MinScreenRatio, i-1)
			}
			if prev.MaxDistance > 0 && lv.MaxDistance > 0 && lv.MaxDistance <= prev.MaxDistance {
				return loaderr.Errorf(lPath+".maxDistance", fmt.Sprintf("order LOD levels from nearest to farthest so maxDistance is greater than %v", prev.MaxDistance), "importsettings: model: lods[%d]: maxDistance %v must be greater than lods[%d] (%v)", i, lv.MaxDistance, i-1, prev.MaxDistance)
			}
		}
	}
	return nil
}

// normalize replaces negative zero float32 fields with positive zero so Encode and Parse round-trip identically.
func (m *Model) normalize() {
	if m.LODHysteresis == 0 {
		m.LODHysteresis = 0
	}
	for i := range m.LODs {
		if m.LODs[i].MinScreenRatio == 0 {
			m.LODs[i].MinScreenRatio = 0
		}
		if m.LODs[i].MaxDistance == 0 {
			m.LODs[i].MaxDistance = 0
		}
	}
}

// normalize replaces a level of negative zero with zero, which YAML writes as -0 but reads back as 0, so Encode writes the same bytes for a File as for the one Parse reads from them.
func (a *Audio) normalize() {
	for _, p := range []**float64{&a.PeakDB, &a.RMSDB} {
		if *p != nil && **p == 0 {
			zero := 0.0
			*p = &zero
		}
	}
}

func (a *Audio) validate() error {
	if a.Format != "" && !slices.Contains(project.AudioFormats, a.Format) {
		return loaderr.Errorf("audio.format", fmt.Sprintf("set audio.format to one of %s", strings.Join(project.AudioFormats, ", ")), "importsettings: audio: format %q must be %s", a.Format, strings.Join(project.AudioFormats, ", "))
	}
	if a.Rate < 0 {
		return loaderr.Errorf("audio.rate", "set audio.rate to 0 (inherit target) or a positive sample rate in Hz", "importsettings: audio: rate %d must not be negative", a.Rate)
	}
	if a.Channels < 0 || a.Channels > 2 {
		return loaderr.Errorf("audio.channels", "set audio.channels to 1 (mono), 2 (stereo), or 0 (inherit target)", "importsettings: audio: channels %d must be 1, 2, or 0 to keep the target's", a.Channels)
	}
	if a.Bitrate < 0 {
		return loaderr.Errorf("audio.bitrate", "set audio.bitrate to 0 (inherit target) or a positive kbps value", "importsettings: audio: bitrate %d must not be negative", a.Bitrate)
	}
	if a.PeakDB != nil && a.RMSDB != nil {
		return loaderr.Errorf("audio", "set either audio.peakDB or audio.rmsDB, not both", "importsettings: audio: peakDB and rmsDB may not both be set")
	}
	for _, v := range []struct {
		name string
		v    *float64
	}{{"peakDB", a.PeakDB}, {"rmsDB", a.RMSDB}} {
		if v.v != nil && (math.IsNaN(*v.v) || math.IsInf(*v.v, 0)) {
			return loaderr.Errorf("audio."+v.name, fmt.Sprintf("set audio.%s to a finite decibel value", v.name), "importsettings: audio: %s must be a finite number", v.name)
		}
	}
	if l := a.Loop; l != nil {
		if l.Start < 0 || l.End < 0 {
			field := "audio.loop.start"
			if l.Start >= 0 {
				field = "audio.loop.end"
			}
			return loaderr.Errorf(field, "set audio.loop.start and audio.loop.end to non-negative sample frame indices", "importsettings: audio: loop: start %d and end %d must not be negative", l.Start, l.End)
		}
		if l.End != 0 && l.End <= l.Start {
			return loaderr.Errorf("audio.loop.end", "set audio.loop.end greater than audio.loop.start, or 0 for the end of the sound", "importsettings: audio: loop: end %d must be after start %d, or 0 for the end of the sound", l.End, l.Start)
		}
		if a.Format != "" && a.Format != "wav" {
			return loaderr.Errorf("audio.format", "set audio.format to wav (or omit it) when using audio.loop", "importsettings: audio: loop: %s carries no loop points; convert a looping sound to wav", a.Format)
		}
	}
	return nil
}
