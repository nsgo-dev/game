package project

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
)

// DefaultAssetsCache is where game assets keeps its build cache, relative to the project root, when Assets.Cache is empty. Its directory starts with a dot, so the manifest's scan of an assets directory never lists it.
const DefaultAssetsCache = ".game/assets-cache.yaml"

// TextureFormats are the KTX2 formats a texture target may name, as game tex's -format flag spells them.
var TextureFormats = []string{"rgba8", "bc1", "bc3", "etc2"}

// AudioFormats are the formats an audio target may name, as game audio's -format flag spells them.
var AudioFormats = []string{"wav", "aac", "mp3"}

// Assets declares the targets game assets builds: the sprite atlases to pack, the textures to encode, the audio to convert and the asset manifest to generate. Paths in it are slash-separated and relative to the project root, and may not leave it.
type Assets struct {
	// Cache is the build cache's path; empty is DefaultAssetsCache.
	Cache string `yaml:"cache,omitempty"`
	// Atlases are the sprite atlases to pack, each from one folder of PNG images.
	Atlases []AtlasTarget `yaml:"atlases,omitempty"`
	// Textures are the images to encode into KTX2 containers.
	Textures []TextureTarget `yaml:"textures,omitempty"`
	// Audio are the WAV files to normalize, resample and convert.
	Audio []AudioTarget `yaml:"audio,omitempty"`
	// Manifest, when set, is the typed Go asset manifest generated after every other target is built.
	Manifest *ManifestTarget `yaml:"manifest,omitempty"`
}

// AtlasTarget is one sprite atlas packed from a folder of PNG images, as game pack packs it.
type AtlasTarget struct {
	// Name is the base name of the atlas's page files, such as "ui" for ui.png and ui.json.
	Name string `yaml:"name"`
	// In is the folder of PNG images, searched recursively.
	In string `yaml:"in"`
	// Out is the folder the pages are written to.
	Out string `yaml:"out"`
	// Padding is the transparent gap between frames in pixels; nil is 1.
	Padding *int `yaml:"padding,omitempty"`
	// Extrude is how many edge pixels are repeated around each frame.
	Extrude int `yaml:"extrude,omitempty"`
	// MaxWidth bounds a page's width in pixels; 0 is 2048.
	MaxWidth int `yaml:"maxWidth,omitempty"`
	// MaxHeight bounds a page's height in pixels; 0 is 2048.
	MaxHeight int `yaml:"maxHeight,omitempty"`
	// Scale is written to the pages' meta.scale; 0 is 1.
	Scale int `yaml:"scale,omitempty"`
	// Trim removes transparent borders around each image; nil is true.
	Trim *bool `yaml:"trim,omitempty"`
}

// TextureTarget is one PNG or JPEG image encoded into a KTX2 container, as game tex encodes it.
type TextureTarget struct {
	// In is the source image.
	In string `yaml:"in"`
	// Out is the .ktx2 file written.
	Out string `yaml:"out"`
	// Format is one of TextureFormats; empty is rgba8.
	Format string `yaml:"format,omitempty"`
	// Mipmaps writes the whole mipmap chain down to 1x1.
	Mipmaps bool `yaml:"mipmaps,omitempty"`
	// SRGB marks the texture sRGB and filters its mipmaps in linear light.
	SRGB bool `yaml:"srgb,omitempty"`
}

// AudioTarget is one WAV file normalized, resampled and converted, as game audio converts it.
type AudioTarget struct {
	// In is the source WAV file.
	In string `yaml:"in"`
	// Out is the file written.
	Out string `yaml:"out"`
	// Format is one of AudioFormats; empty is wav. aac and mp3 need an external encoder, as game audio describes.
	Format string `yaml:"format,omitempty"`
	// Rate is the sample rate in Hz; 0 keeps the input's.
	Rate int `yaml:"rate,omitempty"`
	// Channels is 1 for mono or 2 for stereo; 0 keeps the input's.
	Channels int `yaml:"channels,omitempty"`
	// PeakDB normalizes the peak to this level in dBFS; it may not be set with RMSDB.
	PeakDB *float64 `yaml:"peakDB,omitempty"`
	// RMSDB normalizes the loudness to this level in dBFS with soft clipping; it may not be set with PeakDB.
	RMSDB *float64 `yaml:"rmsDB,omitempty"`
	// Bitrate is the AAC or MP3 bitrate in kbps; 0 is 128.
	Bitrate int `yaml:"bitrate,omitempty"`
}

// ManifestTarget is the typed Go asset manifest generated from a folder of assets, as game manifest generates it.
type ManifestTarget struct {
	// In is the assets folder scanned.
	In string `yaml:"in"`
	// Out is the .go file written.
	Out string `yaml:"out"`
	// Package is the generated file's package name; empty is "assets".
	Package string `yaml:"package,omitempty"`
	// Prefix is prepended to each asset's path to form its URI; empty is "asset:".
	Prefix string `yaml:"prefix,omitempty"`
	// Embed emits //go:embed directives instead of URI references.
	Embed bool `yaml:"embed,omitempty"`
	// EmbedDir is prepended to the //go:embed patterns.
	EmbedDir string `yaml:"embedDir,omitempty"`
}

// CachePath returns the build cache's slash-separated path relative to the project root: Cache, or DefaultAssetsCache when a is nil or Cache is empty.
func (a *Assets) CachePath() string {
	if a == nil || a.Cache == "" {
		return DefaultAssetsCache
	}
	return a.Cache
}

// IsOutput reports whether a target of a writes the file at the slash-separated path p, relative to the project root: the cache, a texture, an audio output, the manifest, or a page of an atlas (name.png, name.json, name-N.png or name-N.json in its out folder). Such a file is built from the targets' inputs rather than authored, so it is not an asset of its own. A nil a writes nothing.
func (a *Assets) IsOutput(p string) bool {
	if a == nil {
		return false
	}
	p = path.Clean(p)
	if p == path.Clean(a.CachePath()) {
		return true
	}
	for _, t := range a.Textures {
		if p == path.Clean(t.Out) {
			return true
		}
	}
	for _, au := range a.Audio {
		if p == path.Clean(au.Out) {
			return true
		}
	}
	if a.Manifest != nil && p == path.Clean(a.Manifest.Out) {
		return true
	}
	for _, at := range a.Atlases {
		if path.Dir(p) == path.Clean(at.Out) && isPage(at.Name, path.Base(p)) {
			return true
		}
	}
	return false
}

// Validate checks the assets section as Validate checks the whole file: paths that are relative, slash-separated and stay inside the project, settings in range, known formats, no two targets writing the same file, no file written over game.yaml or over a target's input, no atlas page or other PNG written where an atlas reads its images, and no cache where the manifest lists assets.
func (a *Assets) Validate() error {
	if a == nil {
		return nil
	}
	if a.Cache != "" {
		if err := validateAssetFile(a.Cache); err != nil {
			return fmt.Errorf("project: assets cache: %w", err)
		}
	}
	// files maps each single file a target writes to the target; atlas pages are checked against it by name, since how many pages an atlas writes is known only when it is packed.
	files := map[string]string{}
	var order []string
	claim := func(out, owner string) error {
		if other, dup := files[out]; dup {
			return fmt.Errorf("project: assets %s writes %s, which %s also writes", owner, out, other)
		}
		files[out] = owner
		order = append(order, out)
		return nil
	}
	if err := claim(a.CachePath(), "the cache"); err != nil {
		return err
	}
	for i, at := range a.Atlases {
		where := fmt.Sprintf("atlases[%d]", i)
		if err := validateAtlasTarget(at); err != nil {
			return fmt.Errorf("project: assets %s: %w", where, err)
		}
		for j, prev := range a.Atlases[:i] {
			if prev.Out != at.Out {
				continue
			}
			if page, clash := pagesClash(prev.Name, at.Name); clash {
				return fmt.Errorf("project: assets %s writes %s, which atlases[%d] also writes", where, path.Join(at.Out, page), j)
			}
		}
	}
	// An atlas whose pages land where an atlas reads its images would pack its own pages on the next run, so it would never be up to date.
	for i, at := range a.Atlases {
		for j, src := range a.Atlases {
			if scannedUnder(at.Out, src.In) {
				return fmt.Errorf("project: assets atlases[%d] writes its pages to %s, inside atlases[%d]'s images in %s; write them elsewhere or to a folder whose name starts with a dot", i, at.Out, j, src.In)
			}
		}
	}
	for i, t := range a.Textures {
		where := fmt.Sprintf("textures[%d]", i)
		if err := validateTextureTarget(t); err != nil {
			return fmt.Errorf("project: assets %s: %w", where, err)
		}
		if err := claim(t.Out, where); err != nil {
			return err
		}
	}
	for i, au := range a.Audio {
		where := fmt.Sprintf("audio[%d]", i)
		if err := validateAudioTarget(au); err != nil {
			return fmt.Errorf("project: assets %s: %w", where, err)
		}
		if err := claim(au.Out, where); err != nil {
			return err
		}
	}
	if m := a.Manifest; m != nil {
		if err := validateManifestTarget(*m); err != nil {
			return fmt.Errorf("project: assets manifest: %w", err)
		}
		if err := claim(m.Out, "manifest"); err != nil {
			return err
		}
		// The cache is saved after the manifest is generated, so a cache the manifest's scan reaches would be listed as an asset one run later.
		if scannedUnder(a.CachePath(), m.In) {
			return fmt.Errorf("project: assets cache %s is inside the manifest's folder %s; move it out or into a folder whose name starts with a dot", a.CachePath(), m.In)
		}
	}
	// reads maps each path a target reads, file or folder, to the first target that reads it.
	reads := map[string]string{}
	read := func(in, owner string) {
		if _, ok := reads[in]; !ok {
			reads[in] = owner
		}
	}
	for i, at := range a.Atlases {
		read(at.In, fmt.Sprintf("atlases[%d]", i))
	}
	for i, t := range a.Textures {
		read(t.In, fmt.Sprintf("textures[%d]", i))
	}
	for i, au := range a.Audio {
		read(au.In, fmt.Sprintf("audio[%d]", i))
	}
	if m := a.Manifest; m != nil {
		read(m.In, "manifest")
	}
	for _, f := range order {
		// A file written over game.yaml or over a target's input destroys what the next run reads, so that run fails or never settles.
		if f == FileName {
			return fmt.Errorf("project: assets %s writes %s, the project file", files[f], f)
		}
		if reader, ok := reads[f]; ok {
			return fmt.Errorf("project: assets %s writes %s, which %s reads", files[f], f, reader)
		}
		for i, at := range a.Atlases {
			if path.Dir(f) == at.Out && isPage(at.Name, path.Base(f)) {
				return fmt.Errorf("project: assets %s writes %s, which atlases[%d] also writes", files[f], f, i)
			}
			// game pack packs every .png file its scan reaches, in any letter case, so a PNG written there would be packed on the next run.
			if strings.EqualFold(path.Ext(f), ".png") && scannedUnder(f, at.In) {
				return fmt.Errorf("project: assets %s writes %s, inside atlases[%d]'s images in %s; write it elsewhere or to a folder whose name starts with a dot", files[f], f, i, at.In)
			}
		}
	}
	return nil
}

// normalize drops empty target lists, which Encode writes as no list, so Parse reads both forms the same way and a round trip through Encode and Parse gives back what Parse returned.
func (a *Assets) normalize() {
	if len(a.Atlases) == 0 {
		a.Atlases = nil
	}
	if len(a.Textures) == 0 {
		a.Textures = nil
	}
	if len(a.Audio) == 0 {
		a.Audio = nil
	}
}

// clone returns a deep copy of a.
func (a *Assets) clone() *Assets {
	if a == nil {
		return nil
	}
	c := Assets{Cache: a.Cache, Manifest: clonePtr(a.Manifest)}
	if a.Atlases != nil {
		c.Atlases = make([]AtlasTarget, len(a.Atlases))
		for i, at := range a.Atlases {
			at.Padding = clonePtr(at.Padding)
			at.Trim = clonePtr(at.Trim)
			c.Atlases[i] = at
		}
	}
	c.Textures = slices.Clone(a.Textures)
	if a.Audio != nil {
		c.Audio = make([]AudioTarget, len(a.Audio))
		for i, au := range a.Audio {
			au.PeakDB = clonePtr(au.PeakDB)
			au.RMSDB = clonePtr(au.RMSDB)
			c.Audio[i] = au
		}
	}
	return &c
}

// isPage reports whether base is the name of a page file game pack writes for the atlas name: name.png and name.json, or name-N.png and name-N.json when the atlas needs several pages.
func isPage(name, base string) bool {
	ext := path.Ext(base)
	if ext != ".png" && ext != ".json" {
		return false
	}
	stem := strings.TrimSuffix(base, ext)
	if stem == name {
		return true
	}
	n, ok := strings.CutPrefix(stem, name+"-")
	if !ok || n == "" {
		return false
	}
	for _, c := range n {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// pagesClash reports whether atlases named a and b, written to one folder, can write a page file of the same name, and returns that file's name.
func pagesClash(a, b string) (string, bool) {
	switch {
	case a == b || isPage(a, b+".png"):
		return b + ".png", true
	case isPage(b, a+".png"):
		return a + ".png", true
	}
	return "", false
}

// scannedUnder reports whether p is dir or inside it with no path element below dir that starts with a dot, so a scan of dir that skips dot-prefixed names, as game pack's and game manifest's scans do, reaches p.
func scannedUnder(p, dir string) bool {
	var rest string
	switch {
	case p == dir:
		return true
	case dir == ".":
		rest = p
	case strings.HasPrefix(p, dir+"/"):
		rest = p[len(dir)+1:]
	default:
		return false
	}
	for _, e := range strings.Split(rest, "/") {
		if strings.HasPrefix(e, ".") {
			return false
		}
	}
	return true
}

func validateAtlasTarget(a AtlasTarget) error {
	if a.Name == "" {
		return errors.New("name is required")
	}
	if strings.ContainsAny(a.Name, `/\`) || strings.HasPrefix(a.Name, ".") || !fs.ValidPath(a.Name) {
		return fmt.Errorf("name %q must be a plain file name that does not start with a dot", a.Name)
	}
	if err := validateAssetIO(a.In, a.Out, true); err != nil {
		return err
	}
	if a.Padding != nil && *a.Padding < 0 {
		return fmt.Errorf("padding %d must not be negative", *a.Padding)
	}
	for _, f := range []struct {
		name string
		v    int
	}{{"extrude", a.Extrude}, {"maxWidth", a.MaxWidth}, {"maxHeight", a.MaxHeight}, {"scale", a.Scale}} {
		if f.v < 0 {
			return fmt.Errorf("%s %d must not be negative", f.name, f.v)
		}
	}
	return nil
}

func validateTextureTarget(t TextureTarget) error {
	if err := validateAssetIO(t.In, t.Out, false); err != nil {
		return err
	}
	if t.Format != "" && !slices.Contains(TextureFormats, t.Format) {
		return fmt.Errorf("format %q must be %s", t.Format, strings.Join(TextureFormats, ", "))
	}
	return nil
}

func validateAudioTarget(a AudioTarget) error {
	if err := validateAssetIO(a.In, a.Out, false); err != nil {
		return err
	}
	if a.Format != "" && !slices.Contains(AudioFormats, a.Format) {
		return fmt.Errorf("format %q must be %s", a.Format, strings.Join(AudioFormats, ", "))
	}
	if a.Rate < 0 {
		return fmt.Errorf("rate %d must not be negative", a.Rate)
	}
	if a.Channels < 0 || a.Channels > 2 {
		return fmt.Errorf("channels %d must be 1, 2, or 0 to keep the input's", a.Channels)
	}
	if a.Bitrate < 0 {
		return fmt.Errorf("bitrate %d must not be negative", a.Bitrate)
	}
	if a.PeakDB != nil && a.RMSDB != nil {
		return errors.New("peakDB and rmsDB may not both be set")
	}
	for _, f := range []struct {
		name string
		v    *float64
	}{{"peakDB", a.PeakDB}, {"rmsDB", a.RMSDB}} {
		if f.v != nil && (math.IsNaN(*f.v) || math.IsInf(*f.v, 0)) {
			return fmt.Errorf("%s must be a finite number", f.name)
		}
	}
	return nil
}

func validateManifestTarget(m ManifestTarget) error {
	if err := validateAssetIO(m.In, m.Out, false); err != nil {
		return err
	}
	if path.Ext(m.Out) != ".go" {
		return fmt.Errorf("out %q must be a .go file", m.Out)
	}
	if m.EmbedDir != "" && m.EmbedDir != "." && !fs.ValidPath(m.EmbedDir) {
		return fmt.Errorf("embedDir %q must be a slash-separated relative path", m.EmbedDir)
	}
	return nil
}

// validateAssetIO checks a target's input and output paths; out is a file unless outDir is set.
func validateAssetIO(in, out string, outDir bool) error {
	if err := validateAssetPath(in); err != nil {
		return fmt.Errorf("in: %w", err)
	}
	check := validateAssetFile
	if outDir {
		check = validateAssetPath
	}
	if err := check(out); err != nil {
		return fmt.Errorf("out: %w", err)
	}
	return nil
}

// validateAssetFile checks that p is a path validateAssetPath accepts that can name a file, so not the project directory itself.
func validateAssetFile(p string) error {
	if p == "." {
		return errors.New(`path "." names the project directory, not a file`)
	}
	return validateAssetPath(p)
}

// validateAssetPath checks that p is a clean, non-empty, slash-separated path inside the project.
func validateAssetPath(p string) error {
	switch {
	case p == "":
		return errors.New("path is required")
	case strings.Contains(p, `\`):
		return fmt.Errorf("path %q must use forward slashes", p)
	case !fs.ValidPath(p):
		return fmt.Errorf("path %q must be relative, clean and inside the project", p)
	}
	return nil
}
