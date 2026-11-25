package importsettings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
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

// Image holds how an image is imported. A texture target reads Filter, Format, Mipmaps and SRGB; an atlas target reads Slice. Each field left out keeps the target's setting in game.yaml.
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
		return nil, errors.New("importsettings: empty sidecar")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("importsettings: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("importsettings: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("importsettings: decode: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
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
		return errors.New("importsettings: nil sidecar")
	}
	if f.Version != CurrentVersion {
		return fmt.Errorf("importsettings: version %d is not supported; want %d", f.Version, CurrentVersion)
	}
	if f.Credit != nil {
		if err := f.Credit.validate(); err != nil {
			return fmt.Errorf("importsettings: credit: %w", err)
		}
	}
	switch sections := f.Sections(); {
	case len(sections) == 0:
		if f.Credit == nil {
			return errors.New("importsettings: a sidecar needs an image, an audio, a font, a model or a credit section")
		}
	case len(sections) > 1:
		return fmt.Errorf("importsettings: a sidecar holds one of the image, audio, font and model sections, not %s", strings.Join(sections, " and "))
	case f.Image != nil:
		if err := f.Image.validate(); err != nil {
			return fmt.Errorf("importsettings: image: %w", err)
		}
	case f.Audio != nil:
		if err := f.Audio.validate(); err != nil {
			return fmt.Errorf("importsettings: audio: %w", err)
		}
	case f.Font != nil:
		if err := f.Font.validate(); err != nil {
			return fmt.Errorf("importsettings: font: %w", err)
		}
	default:
		if err := f.Model.validate(); err != nil {
			return fmt.Errorf("importsettings: model: %w", err)
		}
	}
	return nil
}

func (im *Image) validate() error {
	if im.Filter != "" && !slices.Contains(Filters, im.Filter) {
		return fmt.Errorf("filter %q must be %s", im.Filter, strings.Join(Filters, " or "))
	}
	if im.Format != "" && !slices.Contains(project.TextureFormats, im.Format) {
		return fmt.Errorf("format %q must be %s", im.Format, strings.Join(project.TextureFormats, ", "))
	}
	if s := im.Slice; s != nil {
		if s.Width <= 0 || s.Height <= 0 {
			return fmt.Errorf("slice: width %d and height %d must be positive", s.Width, s.Height)
		}
		for _, v := range []struct {
			name string
			n    int
		}{{"margin", s.Margin}, {"spacing", s.Spacing}, {"count", s.Count}} {
			if v.n < 0 {
				return fmt.Errorf("slice: %s %d must not be negative", v.name, v.n)
			}
		}
	}
	return nil
}

func (fo *Font) validate() error {
	if err := project.ValidateFontSize(fo.Size); err != nil {
		return err
	}
	if fo.Padding != nil && *fo.Padding < 0 {
		return fmt.Errorf("padding %d must not be negative", *fo.Padding)
	}
	return project.ValidateFontChars(fo.Chars)
}

func (m *Model) validate() error {
	if err := project.ValidateModelScale(m.Scale); err != nil {
		return err
	}
	if math.IsNaN(float64(m.LODHysteresis)) || math.IsInf(float64(m.LODHysteresis), 0) || m.LODHysteresis < 0 || m.LODHysteresis > 0.5 {
		return fmt.Errorf("lodHysteresis %v must be finite and in [0, 0.5]", m.LODHysteresis)
	}
	for i, lv := range m.LODs {
		if lv.Model != "" {
			if !fs.ValidPath(lv.Model) || lv.Model == "." {
				return fmt.Errorf("lods[%d]: model %q must be a slash-separated path relative to the project root", i, lv.Model)
			}
			ext := strings.ToLower(path.Ext(lv.Model))
			if ext != ".gltf" && ext != ".glb" {
				return fmt.Errorf("lods[%d]: model %q must be a glTF file ending in .gltf or .glb", i, lv.Model)
			}
		}
		if lv.Node != "" && lv.Mesh != "" {
			return fmt.Errorf("lods[%d]: node %q and mesh %q may not both be set", i, lv.Node, lv.Mesh)
		}
		if math.IsNaN(float64(lv.MinScreenRatio)) || math.IsInf(float64(lv.MinScreenRatio), 0) || lv.MinScreenRatio < 0 || lv.MinScreenRatio > 1 {
			return fmt.Errorf("lods[%d]: minScreenRatio %v must be finite and in [0, 1]", i, lv.MinScreenRatio)
		}
		if math.IsNaN(float64(lv.MaxDistance)) || math.IsInf(float64(lv.MaxDistance), 0) || lv.MaxDistance < 0 {
			return fmt.Errorf("lods[%d]: maxDistance %v must be finite and non-negative", i, lv.MaxDistance)
		}
		if i < len(m.LODs)-1 && lv.MinScreenRatio == 0 && lv.MaxDistance == 0 {
			return fmt.Errorf("lods[%d]: only the coarsest (last) LOD level may leave both minScreenRatio and maxDistance zero", i)
		}
		if i > 0 {
			prev := m.LODs[i-1]
			if prev.MinScreenRatio > 0 && lv.MinScreenRatio > 0 && lv.MinScreenRatio >= prev.MinScreenRatio {
				return fmt.Errorf("lods[%d]: minScreenRatio %v must be less than lods[%d] (%v)", i, lv.MinScreenRatio, i-1, prev.MinScreenRatio)
			}
			if prev.MinScreenRatio == 0 && lv.MinScreenRatio > 0 {
				return fmt.Errorf("lods[%d]: minScreenRatio %v cannot follow lods[%d] with minScreenRatio 0", i, lv.MinScreenRatio, i-1)
			}
			if prev.MaxDistance > 0 && lv.MaxDistance > 0 && lv.MaxDistance <= prev.MaxDistance {
				return fmt.Errorf("lods[%d]: maxDistance %v must be greater than lods[%d] (%v)", i, lv.MaxDistance, i-1, prev.MaxDistance)
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
		return fmt.Errorf("format %q must be %s", a.Format, strings.Join(project.AudioFormats, ", "))
	}
	if a.Rate < 0 {
		return fmt.Errorf("rate %d must not be negative", a.Rate)
	}
	if a.Channels < 0 || a.Channels > 2 {
		return fmt.Errorf("channels %d must be 1, 2, or 0 to keep the target's", a.Channels)
	}
	if a.Bitrate < 0 {
		return fmt.Errorf("bitrate %d must not be negative", a.Bitrate)
	}
	if a.PeakDB != nil && a.RMSDB != nil {
		return errors.New("peakDB and rmsDB may not both be set")
	}
	for _, v := range []struct {
		name string
		v    *float64
	}{{"peakDB", a.PeakDB}, {"rmsDB", a.RMSDB}} {
		if v.v != nil && (math.IsNaN(*v.v) || math.IsInf(*v.v, 0)) {
			return fmt.Errorf("%s must be a finite number", v.name)
		}
	}
	if l := a.Loop; l != nil {
		if l.Start < 0 || l.End < 0 {
			return fmt.Errorf("loop: start %d and end %d must not be negative", l.Start, l.End)
		}
		if l.End != 0 && l.End <= l.Start {
			return fmt.Errorf("loop: end %d must be after start %d, or 0 for the end of the sound", l.End, l.Start)
		}
		if a.Format != "" && a.Format != "wav" {
			return fmt.Errorf("loop: %s carries no loop points; convert a looping sound to wav", a.Format)
		}
	}
	return nil
}
