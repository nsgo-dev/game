// Package assetbuild builds a game's assets from one game.yaml: it packs sprite atlases, encodes KTX2 textures, converts audio and generates the Go asset manifest, and keeps a text cache of what each output was built from so a later run rebuilds only the targets whose inputs or settings changed.
package assetbuild

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

// CurrentVersion is the schema version of the game.yaml file this package reads and writes.
const CurrentVersion = 1

// DefaultCachePath is where the build cache is kept, relative to game.yaml's directory, when Assets.Cache is empty. Its directory starts with a dot, so the manifest's scan of an assets directory never lists it.
const DefaultCachePath = ".game/assets-cache.yaml"

// FileName is the name the game command looks for when it is not told which project file to read.
const FileName = "game.yaml"

// Project is the root of a versioned game.yaml file.
type Project struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Assets declares the targets game assets builds.
	Assets Assets `yaml:"assets,omitempty"`
}

// Assets declares every asset target of a project. Paths in it are slash-separated and relative to game.yaml's directory, and may not leave it.
type Assets struct {
	// Cache is the build cache's path (DefaultCachePath when empty).
	Cache string `yaml:"cache,omitempty"`
	// Atlases are the sprite atlases to pack, each from one folder of PNG images.
	Atlases []Atlas `yaml:"atlases,omitempty"`
	// Textures are the images to encode into KTX2 containers.
	Textures []Texture `yaml:"textures,omitempty"`
	// Audio are the WAV files to normalize, resample and convert.
	Audio []Audio `yaml:"audio,omitempty"`
	// Manifest, when set, is the typed Go asset manifest generated after every other target is built.
	Manifest *Manifest `yaml:"manifest,omitempty"`
}

// Atlas is one sprite atlas packed from a folder of PNG images, as game pack packs it.
type Atlas struct {
	// Name is the base name of the atlas's page files, such as "ui" for ui.png and ui.json.
	Name string `yaml:"name"`
	// In is the folder of PNG images, searched recursively.
	In string `yaml:"in"`
	// Out is the folder the pages are written to.
	Out string `yaml:"out"`
	// Padding is the transparent gap between frames in pixels (1 when omitted).
	Padding *int `yaml:"padding,omitempty"`
	// Extrude is how many edge pixels are repeated around each frame.
	Extrude int `yaml:"extrude,omitempty"`
	// MaxWidth bounds a page's width in pixels (2048 when 0).
	MaxWidth int `yaml:"maxWidth,omitempty"`
	// MaxHeight bounds a page's height in pixels (2048 when 0).
	MaxHeight int `yaml:"maxHeight,omitempty"`
	// Scale is written to meta.scale (1 when 0).
	Scale int `yaml:"scale,omitempty"`
	// Trim removes transparent borders around each image (true when omitted).
	Trim *bool `yaml:"trim,omitempty"`
}

// Texture is one PNG or JPEG image encoded into a KTX2 container, as game tex encodes it.
type Texture struct {
	// In is the source image.
	In string `yaml:"in"`
	// Out is the .ktx2 file written.
	Out string `yaml:"out"`
	// Format is rgba8, bc1, bc3 or etc2 (rgba8 when empty).
	Format string `yaml:"format,omitempty"`
	// Mipmaps writes the whole mipmap chain down to 1x1.
	Mipmaps bool `yaml:"mipmaps,omitempty"`
	// SRGB marks the texture sRGB and filters its mipmaps in linear light.
	SRGB bool `yaml:"srgb,omitempty"`
}

// Audio is one WAV file normalized, resampled and converted, as game audio converts it.
type Audio struct {
	// In is the source WAV file.
	In string `yaml:"in"`
	// Out is the file written.
	Out string `yaml:"out"`
	// Format is wav, aac or mp3 (wav when empty); aac and mp3 need an external encoder, as game audio describes.
	Format string `yaml:"format,omitempty"`
	// Rate is the sample rate in Hz (the input's when 0).
	Rate int `yaml:"rate,omitempty"`
	// Channels is 1 for mono or 2 for stereo (the input's when 0).
	Channels int `yaml:"channels,omitempty"`
	// PeakDB normalizes the peak to this level in dBFS; it may not be set with RMSDB.
	PeakDB *float64 `yaml:"peakDB,omitempty"`
	// RMSDB normalizes the loudness to this level in dBFS with soft clipping; it may not be set with PeakDB.
	RMSDB *float64 `yaml:"rmsDB,omitempty"`
	// Bitrate is the AAC or MP3 bitrate in kbps (128 when 0).
	Bitrate int `yaml:"bitrate,omitempty"`
}

// Manifest is the typed Go asset manifest generated from a folder of assets, as game manifest generates it.
type Manifest struct {
	// In is the assets folder scanned.
	In string `yaml:"in"`
	// Out is the .go file written.
	Out string `yaml:"out"`
	// Package is the generated file's package name ("assets" when empty).
	Package string `yaml:"package,omitempty"`
	// Prefix is prepended to each asset's path to form its URI ("asset:" when empty).
	Prefix string `yaml:"prefix,omitempty"`
	// Embed emits //go:embed directives instead of URI references.
	Embed bool `yaml:"embed,omitempty"`
	// EmbedDir is prepended to the //go:embed patterns.
	EmbedDir string `yaml:"embedDir,omitempty"`
}

// CachePath returns the build cache's slash-separated path relative to game.yaml's directory: Cache, or DefaultCachePath when it is empty.
func (a Assets) CachePath() string {
	if a.Cache == "" {
		return DefaultCachePath
	}
	return a.Cache
}

// Load reads and parses the game.yaml file at path.
func Load(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("assetbuild: %w", err)
	}
	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// Parse decodes a game.yaml file and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*Project, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("assetbuild: empty game.yaml")
	}
	var p Project
	if err := decodeStrict(data, &p); err != nil {
		return nil, fmt.Errorf("assetbuild: decode game.yaml: %w", err)
	}
	if err := Validate(&p); err != nil {
		return nil, err
	}
	// An empty list is written as no list, so Parse reads both the same way and Encode then Parse gives back what Parse returned.
	a := &p.Assets
	if len(a.Atlases) == 0 {
		a.Atlases = nil
	}
	if len(a.Textures) == 0 {
		a.Textures = nil
	}
	if len(a.Audio) == 0 {
		a.Audio = nil
	}
	return &p, nil
}

// Encode validates p and writes it as deterministic 2-space YAML that Parse reads back unchanged.
func Encode(p *Project) ([]byte, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	return encode(p)
}

// Validate checks that p follows the version 1 schema: known version, relative slash-separated paths that stay inside the project, settings in range, known formats, no two targets writing the same file, no atlas writing its pages where an atlas reads its images, and no cache where the manifest lists assets.
func Validate(p *Project) error {
	if p == nil {
		return errors.New("assetbuild: nil project")
	}
	if p.Version != CurrentVersion {
		return fmt.Errorf("assetbuild: unsupported version %d (want %d)", p.Version, CurrentVersion)
	}
	a := &p.Assets
	if a.Cache != "" {
		if err := validateFile(a.Cache); err != nil {
			return fmt.Errorf("assetbuild: cache: %w", err)
		}
	}
	// files maps each single file a target writes to the target; atlas pages are checked against it by name, since how many pages an atlas writes is known only when it is packed.
	files := map[string]string{}
	var order []string
	claim := func(out, owner string) error {
		if other, dup := files[out]; dup {
			return fmt.Errorf("assetbuild: %s writes %s, which %s also writes", owner, out, other)
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
		if err := validateAtlas(at); err != nil {
			return fmt.Errorf("assetbuild: %s: %w", where, err)
		}
		for j, prev := range a.Atlases[:i] {
			if prev.Out != at.Out {
				continue
			}
			if page, clash := pagesClash(prev.Name, at.Name); clash {
				return fmt.Errorf("assetbuild: %s writes %s, which atlases[%d] also writes", where, path.Join(at.Out, page), j)
			}
		}
	}
	// An atlas whose pages land where an atlas reads its images would pack its own pages on the next run, so it would never be up to date.
	for i, at := range a.Atlases {
		for j, src := range a.Atlases {
			if scannedUnder(at.Out, src.In) {
				return fmt.Errorf("assetbuild: atlases[%d] writes its pages to %s, inside atlases[%d]'s images in %s; write them elsewhere or to a folder whose name starts with a dot", i, at.Out, j, src.In)
			}
		}
	}
	for i, t := range a.Textures {
		where := fmt.Sprintf("textures[%d]", i)
		if err := validateTexture(t); err != nil {
			return fmt.Errorf("assetbuild: %s: %w", where, err)
		}
		if err := claim(t.Out, where); err != nil {
			return err
		}
	}
	for i, au := range a.Audio {
		where := fmt.Sprintf("audio[%d]", i)
		if err := validateAudio(au); err != nil {
			return fmt.Errorf("assetbuild: %s: %w", where, err)
		}
		if err := claim(au.Out, where); err != nil {
			return err
		}
	}
	if m := a.Manifest; m != nil {
		if err := validateManifest(*m); err != nil {
			return fmt.Errorf("assetbuild: manifest: %w", err)
		}
		if err := claim(m.Out, "manifest"); err != nil {
			return err
		}
		// The cache is saved after the manifest is generated, so a cache the manifest's scan reaches would be listed as an asset one run later.
		if scannedUnder(a.CachePath(), m.In) {
			return fmt.Errorf("assetbuild: cache %s is inside the manifest's folder %s; move it out or into a folder whose name starts with a dot", a.CachePath(), m.In)
		}
	}
	for _, f := range order {
		for i, at := range a.Atlases {
			if path.Dir(f) == at.Out && isPage(at.Name, path.Base(f)) {
				return fmt.Errorf("assetbuild: %s writes %s, which atlases[%d] also writes", files[f], f, i)
			}
		}
	}
	return nil
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

func validateAtlas(a Atlas) error {
	if a.Name == "" {
		return errors.New("name is required")
	}
	if strings.ContainsAny(a.Name, `/\`) || strings.HasPrefix(a.Name, ".") || !fs.ValidPath(a.Name) {
		return fmt.Errorf("name %q must be a plain file name that does not start with a dot", a.Name)
	}
	if err := validateIO(a.In, a.Out, true); err != nil {
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

func validateTexture(t Texture) error {
	if err := validateIO(t.In, t.Out, false); err != nil {
		return err
	}
	if t.Format != "" {
		f, err := ktx2.ParseFormat(t.Format)
		if err != nil || string(f) != t.Format {
			return fmt.Errorf("format %q must be rgba8, bc1, bc3 or etc2", t.Format)
		}
	}
	return nil
}

func validateAudio(a Audio) error {
	if err := validateIO(a.In, a.Out, false); err != nil {
		return err
	}
	if a.Format != "" {
		f, err := audioconv.ParseFormat(a.Format)
		if err != nil || string(f) != a.Format {
			return fmt.Errorf("format %q must be wav, aac or mp3", a.Format)
		}
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

func validateManifest(m Manifest) error {
	if err := validateIO(m.In, m.Out, false); err != nil {
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

// validateIO checks a target's input and output paths; out is a file unless outDir is set.
func validateIO(in, out string, outDir bool) error {
	if err := validatePath(in); err != nil {
		return fmt.Errorf("in: %w", err)
	}
	check := validateFile
	if outDir {
		check = validatePath
	}
	if err := check(out); err != nil {
		return fmt.Errorf("out: %w", err)
	}
	return nil
}

// validateFile checks that p is a path validatePath accepts that can name a file, so not the project directory itself.
func validateFile(p string) error {
	if p == "." {
		return errors.New(`path "." names the project directory, not a file`)
	}
	return validatePath(p)
}

// validatePath checks that p is a clean, non-empty, slash-separated path inside the project.
func validatePath(p string) error {
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

func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return err
	}
	return nil
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("assetbuild: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("assetbuild: encode: %w", err)
	}
	return buf.Bytes(), nil
}
