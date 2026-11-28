package project

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

// DefaultAssetsCache is where game assets keeps its build cache, relative to the project root, when Assets.Cache is empty. Its directory starts with a dot, so the manifest's scan of an assets directory never lists it.
const DefaultAssetsCache = ".game/assets-cache.yaml"

// TextureFormats are the KTX2 formats a texture target may name, as game tex's -format flag spells them.
var TextureFormats = []string{"rgba8", "bc1", "bc3", "etc2"}

// AudioFormats are the formats an audio target may name, as game audio's -format flag spells them.
var AudioFormats = []string{"wav", "aac", "mp3"}

// DefaultFontSize is the size in pixels a font target bakes its glyphs at when neither it nor its font's import settings set one.
const DefaultFontSize = 32

// MaxFontSize is the largest size in pixels a font target or a font's import settings may set: the width and height of a font's page, which each glyph must fit on.
const MaxFontSize = 2048

// DefaultFontChars are the characters a font target bakes when neither it nor its font's import settings list them: printable ASCII, from the space to the tilde.
const DefaultFontChars = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

// Assets declares the targets game assets builds: the sprite atlases to pack, the textures to encode, the audio to convert, the fonts to bake, the models to pack and the asset manifest to generate. Paths in it are slash-separated and relative to the project root, and may not leave it.
type Assets struct {
	// Cache is the build cache's path; empty is DefaultAssetsCache.
	Cache string `yaml:"cache,omitempty"`
	// Atlases are the sprite atlases to pack, each from one folder of PNG images.
	Atlases []AtlasTarget `yaml:"atlases,omitempty"`
	// Textures are the images to encode into KTX2 containers.
	Textures []TextureTarget `yaml:"textures,omitempty"`
	// Audio are the WAV files to normalize, resample and convert.
	Audio []AudioTarget `yaml:"audio,omitempty"`
	// Fonts are the TrueType and OpenType fonts, and the folders of PNG glyph images, to bake into bitmap fonts.
	Fonts []FontTarget `yaml:"fonts,omitempty"`
	// Models are the glTF models to pack into single GLB files.
	Models []ModelTarget `yaml:"models,omitempty"`
	// Manifest, when set, is the typed Go asset manifest generated after every other target is built.
	Manifest *ManifestTarget `yaml:"manifest,omitempty"`
	// Remote, when set, copies loose assets to content-hashed filenames (<name>.<hash8>.<ext>) under its out folder and writes remote.yaml for CDN hosting.
	Remote *RemoteTarget `yaml:"remote,omitempty"`
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

// FontTarget is one TrueType or OpenType font, or one folder of PNG glyph images, baked into an AngelCode BMFont, which text.LoadBMFont reads: a text .fnt file and the PNG pages its glyphs are packed on, named after it, such as hud.png beside hud.fnt, or hud-0.png, hud-1.png and so on when the glyphs need several pages.
type FontTarget struct {
	// In is the .ttf or .otf font file, or a folder of PNG glyph images when the path has no .ttf or .otf extension.
	In string `yaml:"in"`
	// Out is the .fnt file written; the pages are written beside it.
	Out string `yaml:"out"`
	// Size is the font size in pixels the glyphs are drawn at; 0 is DefaultFontSize. A folder of glyph images keeps its images' pixels instead.
	Size int `yaml:"size,omitempty"`
	// Chars are the characters baked, each once, in any order; empty is DefaultFontChars when CharsFrom and CharsFile are also empty. When CharsFrom or CharsFile is set, Chars is merged with the characters collected from them. A character the font has no glyph for is left out. A folder of glyph images bakes every image it holds instead.
	Chars string `yaml:"chars,omitempty"`
	// CharsFrom lists slash-separated project-relative files whose text characters are collected and merged into the baked set: versioned YAML or JSON localization string tables (all strings and plural forms, with {placeholders} and rich-text markup stripped), YAML data tables (*.table.yaml, all string cells), and scene files (*.scene.yaml). A folder of glyph images refuses CharsFrom.
	CharsFrom []string `yaml:"charsFrom,omitempty"`
	// CharsFile is a slash-separated project-relative UTF-8 plain-text file whose non-control characters are merged into the baked set. A folder of glyph images refuses CharsFile.
	CharsFile string `yaml:"charsFile,omitempty"`
	// Locale is the optional normalized BCP-47 locale tag (such as "zh-Hans" or "en") this font target is meant to draw, checked by game check against that locale's localization string tables. When omitted, game check infers the locale from any localization string tables listed in CharsFrom.
	Locale string `yaml:"locale,omitempty"`
	// Padding is the transparent gap around each glyph on the pages in pixels; nil is 1.
	Padding *int `yaml:"padding,omitempty"`
	// Glyphs maps a glyph image's slash-separated path relative to In to the character it draws, for images whose base name's stem is not one character, such as "plus.png": "+". A folder input only takes it.
	Glyphs map[string]string `yaml:"glyphs,omitempty"`
	// Spacing is the extra advance in pixels after each glyph of a folder input.
	Spacing int `yaml:"spacing,omitempty"`
	// Align is where each glyph of a folder input sits in the line: "top", "center" or "bottom"; empty is "bottom".
	Align string `yaml:"align,omitempty"`
}

// ModelTarget is one glTF model packed into a single binary glTF (.glb) file: its buffers merged into the file's binary chunk and the images it references by file embedded, so the game loads one file.
type ModelTarget struct {
	// In is the .gltf or .glb file; the buffers and images it references by relative path are read beside it.
	In string `yaml:"in"`
	// Out is the .glb file written.
	Out string `yaml:"out"`
	// Scale multiplies the size of every scene of the model, such as 0.01 for a model exported in centimeters; 0 is 1.
	Scale float64 `yaml:"scale,omitempty"`
	// Animations keeps the model's animations when true and leaves them out when false; nil is true.
	Animations *bool `yaml:"animations,omitempty"`
}

// FontPages returns the slash-separated folder a font target writes its pages to and the name they are named after: Out's folder and its base name without the .fnt extension.
func (t FontTarget) FontPages() (dir, name string) {
	return path.Dir(t.Out), strings.TrimSuffix(path.Base(t.Out), path.Ext(t.Out))
}

// GlyphFolder reports whether the font target bakes from a folder of PNG glyph images rather than a TrueType or OpenType font file: In has no .ttf or .otf extension.
func (t FontTarget) GlyphFolder() bool {
	ext := strings.ToLower(path.Ext(t.In))
	return ext != ".ttf" && ext != ".otf"
}

// ValidateFontSize checks the size of a font target or a font's import settings: 0, which keeps the default, up to MaxFontSize.
func ValidateFontSize(size int) error {
	if size < 0 || size > MaxFontSize {
		return fmt.Errorf("size %d must be from 0 to %d", size, MaxFontSize)
	}
	return nil
}

// ValidateFontChars checks the characters of a font target or a font's import settings: valid UTF-8 without control characters, line breaks included. The space is allowed.
func ValidateFontChars(chars string) error {
	if !utf8.ValidString(chars) {
		return errors.New("chars must be valid UTF-8")
	}
	if i := strings.IndexFunc(chars, unicode.IsControl); i >= 0 {
		return fmt.Errorf("chars holds the control character %q; list only characters to draw", chars[i:i+1])
	}
	return nil
}

// ValidateFontCharsFrom checks the charsFrom path list of a font target or a font's import settings: each entry a clean, non-empty, slash-separated relative file path inside the project, with no duplicates.
func ValidateFontCharsFrom(paths []string) error {
	seen := make(map[string]bool, len(paths))
	for i, p := range paths {
		if err := validateAssetFile(p); err != nil {
			return fmt.Errorf("charsFrom[%d]: %w", i, err)
		}
		if seen[p] {
			return fmt.Errorf("charsFrom[%d]: duplicate path %q", i, p)
		}
		seen[p] = true
	}
	return nil
}

// ValidateFontCharsFile checks the charsFile path of a font target or a font's import settings: empty, or a clean, slash-separated relative file path inside the project.
func ValidateFontCharsFile(p string) error {
	if p == "" {
		return nil
	}
	if err := validateAssetFile(p); err != nil {
		return fmt.Errorf("charsFile: %w", err)
	}
	return nil
}

// ValidateModelScale checks the scale of a model target or a model's import settings: a finite number that is not negative, 0 meaning the default.
func ValidateModelScale(scale float64) error {
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0 {
		return fmt.Errorf("scale %v must be a finite number that is not negative", scale)
	}
	return nil
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

// RemoteTarget publishes loose assets into Out under content-hashed filenames (<name>.<hash8>.<ext>) and writes a remote.yaml manifest mapping each asset URI to its hashed path, byte size and SHA-256 digest.
type RemoteTarget struct {
	// In is the folder of assets to publish remotely; when empty, Manifest.In is used, or the outputs built by the other targets when no manifest is configured.
	In string `yaml:"in,omitempty"`
	// Out is the directory written with content-hashed files and remote.yaml, such as "dist/cdn".
	Out string `yaml:"out"`
	// Base is the default origin URL prefix written into remote.yaml, such as "https://cdn.example.com/game/".
	Base string `yaml:"base,omitempty"`
	// Prefix is prepended to each published file's relative path to form its URI in remote.yaml; empty is "asset:".
	Prefix string `yaml:"prefix,omitempty"`
	// Include, when non-empty, limits published files to those matching at least one slash-separated path prefix (ending in "/"), exact relative path, or path.Match glob pattern.
	Include []string `yaml:"include,omitempty"`
}

// CachePath returns the build cache's slash-separated path relative to the project root: Cache, or DefaultAssetsCache when a is nil or Cache is empty.
func (a *Assets) CachePath() string {
	if a == nil || a.Cache == "" {
		return DefaultAssetsCache
	}
	return a.Cache
}

// IsOutput reports whether a target of a writes the file at the slash-separated path p, relative to the project root: the cache, a texture, an audio output, a font's .fnt file or one of its pages (name.png or name-N.png beside it), a model, the manifest, or a page of an atlas (name.png, name.json, name-N.png or name-N.json in its out folder). Such a file is built from the targets' inputs rather than authored, so it is not an asset of its own. A nil a writes nothing.
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
	for _, f := range a.Fonts {
		dir, name := f.FontPages()
		if p == path.Clean(f.Out) || path.Dir(p) == dir && path.Ext(p) == ".png" && isPage(name, path.Base(p)) {
			return true
		}
	}
	for _, m := range a.Models {
		if p == path.Clean(m.Out) {
			return true
		}
	}
	if a.Manifest != nil && p == path.Clean(a.Manifest.Out) {
		return true
	}
	if a.Remote != nil {
		out := path.Clean(a.Remote.Out)
		if p == out || strings.HasPrefix(p, out+"/") {
			return true
		}
	}
	for _, at := range a.Atlases {
		if path.Dir(p) == path.Clean(at.Out) && isPage(at.Name, path.Base(p)) {
			return true
		}
	}
	return false
}

// Validate checks the assets section as Validate checks the whole file: paths that are relative, slash-separated and stay inside the project, settings in range, known formats, no two targets writing the same file, no file written over game.yaml or over a target's input, no atlas or font page or other PNG written where an atlas reads its images, and no cache where the manifest lists assets.
func (a *Assets) Validate() error {
	if a == nil {
		return nil
	}
	if a.Cache != "" {
		if err := validateAssetFile(a.Cache); err != nil {
			return loaderr.Errorf("assets.cache", "set assets.cache to a slash-separated relative file path inside the project", "project: assets cache: %w", err)
		}
	}
	// files maps each single file a target writes to the target; atlas and font pages are checked against it by name, since how many pages a target writes is known only when it is packed.
	files := map[string]string{}
	fileFields := map[string]string{}
	var order []string
	claim := func(field, out, owner string) error {
		if other, dup := files[out]; dup {
			return loaderr.Errorf(field, "choose distinct output paths for every asset target", "project: assets %s writes %s, which %s also writes", owner, out, other)
		}
		files[out] = owner
		fileFields[out] = field
		order = append(order, out)
		return nil
	}
	if err := claim("assets.cache", a.CachePath(), "the cache"); err != nil {
		return err
	}
	// pagers are the targets that write a number of pages known only once they are packed: atlases and fonts.
	var pagers []pager
	for i, at := range a.Atlases {
		where := fmt.Sprintf("atlases[%d]", i)
		if err := validateAtlasTarget(at); err != nil {
			return loaderr.Errorf("assets."+where, "fix the atlas target settings in game.yaml", "project: assets %s: %w", where, err)
		}
		pagers = append(pagers, pager{name: at.Name, dir: at.Out, owner: where})
	}
	for i, f := range a.Fonts {
		where := fmt.Sprintf("fonts[%d]", i)
		if err := validateFontTarget("assets."+where, f); err != nil {
			if _, ok := loaderr.As(err); ok {
				return err
			}
			return loaderr.Errorf("assets."+where, "fix the font target settings in game.yaml", "project: assets %s: %w", where, err)
		}
		dir, name := f.FontPages()
		pagers = append(pagers, pager{name: name, dir: dir, owner: where, font: true})
	}
	for i, pg := range pagers {
		for _, prev := range pagers[:i] {
			if prev.dir != pg.dir {
				continue
			}
			if page, clash := pagesClash(prev.name, pg.name); clash {
				return loaderr.Errorf("assets."+pg.owner, "use distinct output names or folders for atlas and font targets", "project: assets %s writes %s, which %s also writes", pg.owner, path.Join(pg.dir, page), prev.owner)
			}
		}
	}
	// A target whose pages land where an atlas reads its images would have them packed on the next run, so the atlas would never be up to date.
	for _, pg := range pagers {
		for j, src := range a.Atlases {
			if scannedUnder(pg.dir, src.In) {
				return loaderr.Errorf("assets."+pg.owner, "write pages outside every atlas input directory or into a dot-prefixed directory", "project: assets %s writes its pages to %s, inside atlases[%d]'s images in %s; write them elsewhere or to a folder whose name starts with a dot", pg.owner, pg.dir, j, src.In)
			}
		}
	}
	for i, f := range a.Fonts {
		where := fmt.Sprintf("fonts[%d]", i)
		if err := claim("assets."+where+".out", f.Out, where); err != nil {
			return err
		}
	}
	for i, t := range a.Textures {
		where := fmt.Sprintf("textures[%d]", i)
		if err := validateTextureTarget(t); err != nil {
			return loaderr.Errorf("assets."+where, "fix the texture target settings in game.yaml", "project: assets %s: %w", where, err)
		}
		if err := claim("assets."+where+".out", t.Out, where); err != nil {
			return err
		}
	}
	for i, au := range a.Audio {
		where := fmt.Sprintf("audio[%d]", i)
		if err := validateAudioTarget(au); err != nil {
			return loaderr.Errorf("assets."+where, "fix the audio target settings in game.yaml", "project: assets %s: %w", where, err)
		}
		if err := claim("assets."+where+".out", au.Out, where); err != nil {
			return err
		}
	}
	for i, m := range a.Models {
		where := fmt.Sprintf("models[%d]", i)
		if err := validateModelTarget(m); err != nil {
			return loaderr.Errorf("assets."+where, "fix the model target settings in game.yaml", "project: assets %s: %w", where, err)
		}
		if err := claim("assets."+where+".out", m.Out, where); err != nil {
			return err
		}
	}
	if m := a.Manifest; m != nil {
		if err := validateManifestTarget(*m); err != nil {
			return loaderr.Errorf("assets.manifest", "fix the manifest target settings in game.yaml", "project: assets manifest: %w", err)
		}
		if err := claim("assets.manifest.out", m.Out, "manifest"); err != nil {
			return err
		}
		// The cache is saved after the manifest is generated, so a cache the manifest's scan reaches would be listed as an asset one run later.
		if scannedUnder(a.CachePath(), m.In) {
			return loaderr.Errorf("assets.cache", "move assets.cache outside assets.manifest.in or into a dot-prefixed folder", "project: assets cache %s is inside the manifest's folder %s; move it out or into a folder whose name starts with a dot", a.CachePath(), m.In)
		}
	}
	if r := a.Remote; r != nil {
		if err := validateRemoteTarget(*r); err != nil {
			return loaderr.Errorf("assets.remote", "fix the remote target settings in game.yaml", "project: assets remote: %w", err)
		}
		if err := claim("assets.remote.out", path.Join(r.Out, "remote.yaml"), "remote"); err != nil {
			return err
		}
		if r.In != "" && scannedUnder(a.CachePath(), r.In) {
			return loaderr.Errorf("assets.cache", "move assets.cache outside assets.remote.in or into a dot-prefixed folder", "project: assets cache %s is inside remote's folder %s; move it out or into a folder whose name starts with a dot", a.CachePath(), r.In)
		}
		if r.In != "" && scannedUnder(r.Out, r.In) {
			return loaderr.Errorf("assets.remote.out", "write assets.remote.out outside assets.remote.in or into a dot-prefixed folder", "project: assets remote writes to %s, inside its own input folder %s; write it elsewhere or to a folder whose name starts with a dot", r.Out, r.In)
		}
		if a.Manifest != nil && scannedUnder(r.Out, a.Manifest.In) {
			return loaderr.Errorf("assets.remote.out", "write assets.remote.out outside assets.manifest.in or into a dot-prefixed folder", "project: assets remote writes to %s, inside the manifest's folder %s; write it elsewhere or to a folder whose name starts with a dot", r.Out, a.Manifest.In)
		}
		for i, at := range a.Atlases {
			if scannedUnder(r.Out, at.In) {
				return loaderr.Errorf("assets.remote.out", "write assets.remote.out outside every atlas input directory or into a dot-prefixed folder", "project: assets remote writes to %s, inside atlases[%d]'s images in %s; write it elsewhere or to a folder whose name starts with a dot", r.Out, i, at.In)
			}
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
	for i, f := range a.Fonts {
		owner := fmt.Sprintf("fonts[%d]", i)
		read(f.In, owner)
		for _, src := range f.CharsFrom {
			read(src, owner)
		}
		if f.CharsFile != "" {
			read(f.CharsFile, owner)
		}
	}
	for i, m := range a.Models {
		read(m.In, fmt.Sprintf("models[%d]", i))
	}
	if m := a.Manifest; m != nil {
		read(m.In, "manifest")
	}
	if r := a.Remote; r != nil && r.In != "" {
		read(r.In, "remote")
	}
	// Fonts build after the textures and models, so one of those reading a font's page would encode the page of the previous run, or fail on a fresh checkout, and the font would overwrite an image written by hand under that name.
	for in, reader := range reads {
		for _, pg := range pagers {
			if pg.font && path.Dir(in) == pg.dir && path.Ext(in) == ".png" && isPage(pg.name, path.Base(in)) {
				return loaderr.Errorf("assets."+reader, "do not read a font target's generated page as another target's input", "project: assets %s reads %s, a page %s writes", reader, in, pg.owner)
			}
		}
	}
	for _, f := range order {
		field := fileFields[f]
		// A file written over game.yaml or over a target's input destroys what the next run reads, so that run fails or never settles.
		if f == FileName {
			return loaderr.Errorf(field, "write asset outputs to a file other than "+FileName, "project: assets %s writes %s, the project file", files[f], f)
		}
		if reader, ok := reads[f]; ok {
			return loaderr.Errorf(field, "write asset outputs to a path not read as another target's input", "project: assets %s writes %s, which %s reads", files[f], f, reader)
		}
		for _, pg := range pagers {
			if path.Dir(f) == pg.dir && isPage(pg.name, path.Base(f)) {
				return loaderr.Errorf(field, "choose an output path that does not collide with generated pages", "project: assets %s writes %s, which %s also writes", files[f], f, pg.owner)
			}
		}
		for i, at := range a.Atlases {
			// game pack packs every .png file its scan reaches, in any letter case, so a PNG written there would be packed on the next run.
			if strings.EqualFold(path.Ext(f), ".png") && scannedUnder(f, at.In) {
				return loaderr.Errorf(field, "write generated PNG files outside every atlas input directory or into a dot-prefixed folder", "project: assets %s writes %s, inside atlases[%d]'s images in %s; write it elsewhere or to a folder whose name starts with a dot", files[f], f, i, at.In)
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
	if len(a.Fonts) == 0 {
		a.Fonts = nil
	} else {
		for i := range a.Fonts {
			if len(a.Fonts[i].CharsFrom) == 0 {
				a.Fonts[i].CharsFrom = nil
			}
			if len(a.Fonts[i].Glyphs) == 0 {
				a.Fonts[i].Glyphs = nil
			}
		}
	}
	if len(a.Models) == 0 {
		a.Models = nil
	}
	if a.Remote != nil && len(a.Remote.Include) == 0 {
		a.Remote.Include = nil
	}
}

// clone returns a deep copy of a.
func (a *Assets) clone() *Assets {
	if a == nil {
		return nil
	}
	c := Assets{Cache: a.Cache, Manifest: clonePtr(a.Manifest)}
	if a.Remote != nil {
		r := *a.Remote
		r.Include = slices.Clone(a.Remote.Include)
		c.Remote = &r
	}
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
	if a.Fonts != nil {
		c.Fonts = make([]FontTarget, len(a.Fonts))
		for i, f := range a.Fonts {
			f.Padding = clonePtr(f.Padding)
			f.CharsFrom = slices.Clone(f.CharsFrom)
			f.Glyphs = maps.Clone(f.Glyphs)
			c.Fonts[i] = f
		}
	}
	if a.Models != nil {
		c.Models = make([]ModelTarget, len(a.Models))
		for i, m := range a.Models {
			m.Animations = clonePtr(m.Animations)
			c.Models[i] = m
		}
	}
	return &c
}

// pager is a target that writes pages named after name into the folder dir: an atlas, or a font beside its .fnt file.
type pager struct {
	name, dir, owner string
	// font is set for a font target, which builds after the targets that read files, so its pages must not be one of their inputs.
	font bool
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

func validateFontTarget(field string, f FontTarget) error {
	if err := validateAssetIO(f.In, f.Out, false); err != nil {
		return err
	}
	if path.Ext(f.Out) != ".fnt" {
		return fmt.Errorf("out %q must be a .fnt file", f.Out)
	}
	if _, name := f.FontPages(); name == "" || strings.HasPrefix(name, ".") || strings.Contains(name, `"`) {
		return fmt.Errorf("out %q must have a name before .fnt that does not start with a dot or hold a double quote, which its pages are named after", f.Out)
	}
	if f.Padding != nil && *f.Padding < 0 {
		return fmt.Errorf("padding %d must not be negative", *f.Padding)
	}
	if f.Locale != "" {
		if err := l10n.ValidateLocale(f.Locale); err != nil {
			return loaderr.Errorf(field+".locale", "set locale to a canonical BCP-47 tag such as en or zh-Hans", "project: %s: locale: %w", field, err)
		}
	}
	if f.GlyphFolder() {
		if f.Size != 0 {
			return fmt.Errorf("size %d is the pixel size a TTF or OTF font's glyphs are drawn at, and %s is a folder of glyph images, whose pixels it keeps", f.Size, f.In)
		}
		if f.Chars != "" {
			return fmt.Errorf("chars picks the glyphs of a TTF or OTF font, and %s is a folder of glyph images, which bakes every image it holds", f.In)
		}
		if len(f.CharsFrom) > 0 {
			return loaderr.Errorf(field+".charsFrom", "remove charsFrom from a glyph-image folder font target", "project: %s: charsFrom collects glyphs for a TTF or OTF font, and %s is a folder of glyph images, which bakes every image it holds", field, f.In)
		}
		if f.CharsFile != "" {
			return loaderr.Errorf(field+".charsFile", "remove charsFile from a glyph-image folder font target", "project: %s: charsFile collects glyphs for a TTF or OTF font, and %s is a folder of glyph images, which bakes every image it holds", field, f.In)
		}
		if f.Spacing < 0 {
			return fmt.Errorf("spacing %d must not be negative", f.Spacing)
		}
		switch f.Align {
		case "", "top", "center", "bottom":
		default:
			return fmt.Errorf("align %q must be top, center or bottom", f.Align)
		}
		return validateFontGlyphs(f.Glyphs)
	}
	if len(f.Glyphs) > 0 {
		return fmt.Errorf("glyphs maps a folder's glyph images to characters, and %s is a TTF or OTF font, whose glyphs it draws itself", f.In)
	}
	if f.Spacing != 0 {
		return fmt.Errorf("spacing adds advance to the glyphs of a folder of glyph images, and %s is a TTF or OTF font", f.In)
	}
	if f.Align != "" {
		return fmt.Errorf("align places the glyphs of a folder of glyph images in the line, and %s is a TTF or OTF font", f.In)
	}
	if err := ValidateFontSize(f.Size); err != nil {
		return err
	}
	if err := ValidateFontChars(f.Chars); err != nil {
		return err
	}
	seen := make(map[string]bool, len(f.CharsFrom))
	for i, p := range f.CharsFrom {
		elemField := fmt.Sprintf("%s.charsFrom[%d]", field, i)
		if err := validateAssetFile(p); err != nil {
			return loaderr.Errorf(elemField, "set each charsFrom entry to a slash-separated relative file path inside the project", "project: %s: charsFrom[%d]: %w", field, i, err)
		}
		if seen[p] {
			return loaderr.Errorf(elemField, "remove the duplicate path from charsFrom", "project: %s: charsFrom[%d]: duplicate path %q", field, i, p)
		}
		seen[p] = true
	}
	if f.CharsFile != "" {
		if err := validateAssetFile(f.CharsFile); err != nil {
			return loaderr.Errorf(field+".charsFile", "set charsFile to a slash-separated relative file path inside the project", "project: %s: charsFile: %w", field, err)
		}
	}
	return nil
}

// validateFontGlyphs checks the glyphs mapping of a font target that bakes a folder of glyph images: each key the slash-separated relative path of a PNG image whose base name's stem is not one character, each value exactly one character that is not a control character, and no two images mapped to one character.
func validateFontGlyphs(glyphs map[string]string) error {
	byChar := make(map[rune]string, len(glyphs))
	for name, chars := range glyphs {
		if err := validateAssetPath(name); err != nil {
			return fmt.Errorf("glyphs: %w", err)
		}
		if !strings.EqualFold(path.Ext(name), ".png") {
			return fmt.Errorf("glyphs: %q must be a PNG image", name)
		}
		base := path.Base(name)
		stem := strings.TrimSuffix(base, path.Ext(base))
		if rs := []rune(stem); len(rs) == 1 {
			return fmt.Errorf("glyphs: %q needs no mapping; its stem %q is one character and maps to it", name, stem)
		}
		rs := []rune(chars)
		if len(rs) != 1 {
			return fmt.Errorf("glyphs: %q must map to exactly one character, not %q", name, chars)
		}
		if unicode.IsControl(rs[0]) {
			return fmt.Errorf("glyphs: %q maps to the control character %q; map it to a character to draw", name, chars)
		}
		if prev, dup := byChar[rs[0]]; dup {
			return fmt.Errorf("glyphs: %q and %q both map to %q; remove one", prev, name, chars)
		}
		byChar[rs[0]] = name
	}
	return nil
}

func validateModelTarget(m ModelTarget) error {
	if err := validateAssetIO(m.In, m.Out, false); err != nil {
		return err
	}
	if ext := strings.ToLower(path.Ext(m.In)); ext != ".gltf" && ext != ".glb" {
		return fmt.Errorf("in %q must be a .gltf or .glb model", m.In)
	}
	if path.Ext(m.Out) != ".glb" {
		return fmt.Errorf("out %q must be a .glb file", m.Out)
	}
	return ValidateModelScale(m.Scale)
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

func validateRemoteTarget(r RemoteTarget) error {
	if err := validateAssetFile(r.Out); err != nil {
		return fmt.Errorf("out: %w", err)
	}
	if r.In != "" {
		if err := validateAssetPath(r.In); err != nil {
			return fmt.Errorf("in: %w", err)
		}
	}
	if r.Base != "" {
		u, err := url.Parse(r.Base)
		if err != nil {
			return fmt.Errorf("base: invalid URL %q: %w", r.Base, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("base: URL %q must use http or https", r.Base)
		}
		if u.Host == "" {
			return fmt.Errorf("base: URL %q must include a host", r.Base)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("base: URL %q must not contain a query or fragment", r.Base)
		}
	}
	for i, pat := range r.Include {
		if pat == "" {
			return fmt.Errorf("include[%d]: pattern must not be empty", i)
		}
		if strings.Contains(pat, `\`) {
			return fmt.Errorf("include[%d]: pattern %q must use forward slashes", i, pat)
		}
		trimmed := strings.TrimSuffix(pat, "/")
		if trimmed == "" || trimmed == "." || !fs.ValidPath(trimmed) {
			return fmt.Errorf("include[%d]: pattern %q must be relative, clean and inside the project", i, pat)
		}
		if _, err := path.Match(trimmed, "a"); err != nil {
			return fmt.Errorf("include[%d]: invalid pattern %q: %w", i, pat, err)
		}
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
