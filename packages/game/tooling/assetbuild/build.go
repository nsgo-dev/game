package assetbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
	"github.com/nsgo-dev/game/packages/game/tooling/manifest"
	"github.com/nsgo-dev/game/packages/game/tooling/pack"
)

// Kind is the sort of work a target does.
type Kind string

const (
	// KindAtlas packs a folder of PNG images into atlas pages.
	KindAtlas Kind = "atlas"
	// KindTexture encodes an image into a KTX2 container.
	KindTexture Kind = "texture"
	// KindAudio converts a WAV file.
	KindAudio Kind = "audio"
	// KindManifest generates the Go asset manifest.
	KindManifest Kind = "manifest"
)

// Status is what a build did with a target.
type Status string

const (
	// Built means the target's outputs were written.
	Built Status = "built"
	// UpToDate means the target's inputs, settings and outputs matched the cache, so nothing was written.
	UpToDate Status = "up to date"
	// Stale means a check found the target out of date; nothing was written.
	Stale Status = "stale"
)

// Target is one target's outcome in a Report.
type Target struct {
	// ID names the target: its kind and its output, such as "atlas assets/atlases/ui" or "texture assets/albedo.ktx2".
	ID string
	// Kind is the sort of work the target does.
	Kind Kind
	// Status is what the build did with it.
	Status Status
	// Outputs are the slash-separated paths, relative to the project directory, of the files the target wrote or would write, sorted; when Status is Stale they are the outputs of the previous build, if any.
	Outputs []string
}

// Report lists every target of a build in the order it ran: atlases, textures and audio in their game.yaml order, the manifest last.
type Report struct {
	Targets []Target
}

// Count returns how many targets in r have status s.
func (r *Report) Count(s Status) int {
	n := 0
	for _, t := range r.Targets {
		if t.Status == s {
			n++
		}
	}
	return n
}

// Builder builds a project's asset targets against the files in its directory.
type Builder struct {
	// Root is the directory game.yaml's paths are relative to, normally the one holding game.yaml.
	Root string
	// Audio converts audio targets (a zero Encoder when nil).
	Audio *audioconv.Encoder
	// Version names the tools that build the targets, such as the game layer's module version; a target built by a different Version is rebuilt.
	Version string
	// Force rebuilds every target even when the cache says it is up to date.
	Force bool
	// Check reports which targets are out of date without writing anything under Root: their status is Stale instead of Built. A target whose cache entry does not match is built in a temporary folder and counts as up to date when Root already holds exactly the files it writes, so a check passes on a fresh checkout whose committed outputs are current even without the cache. AAC and MP3 targets are the exception: an external encoder writes them, and another encoder or encoder version writes different bytes for the same input, so with no cache entry for one, as in a fresh checkout, it counts as up to date when its output exists. An atlas also counts as stale when its output folder holds a page file of its name that it no longer writes.
	Check bool

	// tools memoizes Audio's Tool for each format during a build.
	tools map[audioconv.Format]string
	// readers maps each input file of a build, slash-separated and relative to Root, to the kinds of target that read it.
	readers map[string]reader
}

// reader is a set of the kinds of target that read one input file.
type reader uint8

const (
	readByAtlas reader = 1 << iota
	readByTexture
	readByAudio
)

// cacheFormat is mixed into every target's key, so a change to what a key covers rebuilds every target.
const cacheFormat = "assetbuild/1"

// Build builds the targets of a, game.yaml's assets section, in order, skipping each whose key (a hash of its settings, its inputs' paths and contents, those of the import settings sidecars beside them, and Version) and outputs match the cache, then saves the cache when it changed. A nil a declares no targets: Build then reads and writes nothing. A setting in a sidecar overrides the target's: a texture target takes an image sidecar's filter, format, mipmaps and sRGB, an atlas cuts each image whose sidecar has a slice into frames, and an audio target takes every setting of a sound's sidecar, loop points included. An image that both an atlas and a texture target read may hold both kinds of settings in its sidecar, each target taking its own; otherwise a setting no target of the image reads is refused. Before building, Build refuses a sidecar in a folder a target reads from whose asset no target reads, such as one with a mistyped name, and one whose name differs from its asset's in case, unless it holds only a credit section and its asset is there. A sidecar's credit section changes no output, though, like the rest of the sidecar, its bytes are part of the key of the targets that read its asset. A second Build with nothing changed writes no file. When a target fails, Build saves what it built so far and returns the report up to that target with the error.
func (b *Builder) Build(ctx context.Context, a *project.Assets) (*Report, error) {
	if b.Force && b.Check {
		return nil, errors.New("assetbuild: Force and Check cannot both be set")
	}
	if a == nil {
		return &Report{}, nil
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	b.tools = nil
	b.readers = b.inputs(a)
	if err := b.checkSidecars(a); err != nil {
		return nil, err
	}
	cachePath := b.abs(a.CachePath())
	prev := loadCache(cachePath)
	next := &cache{Version: cacheVersion}
	r := &Report{}

	type step struct {
		id   string
		kind Kind
		// prepare reads the target's inputs and import settings and returns what builds it.
		prepare func() (prepared, error)
		// atlas is the atlas target a KindAtlas step packs, whose output folder may hold pages of an earlier build.
		atlas *project.AtlasTarget
	}
	var steps []step
	for _, at := range a.Atlases {
		steps = append(steps, step{id: "atlas " + path.Join(at.Out, at.Name), kind: KindAtlas, prepare: func() (prepared, error) { return b.atlas(at) }, atlas: &at})
	}
	for _, t := range a.Textures {
		steps = append(steps, step{id: "texture " + t.Out, kind: KindTexture, prepare: func() (prepared, error) { return b.texture(t) }})
	}
	for _, au := range a.Audio {
		steps = append(steps, step{id: "audio " + au.Out, kind: KindAudio, prepare: func() (prepared, error) { return b.audio(ctx, au) }})
	}

	var runErr error
	// written maps each output path claimed in this run to the target that claimed it.
	written := map[string]string{}
	for _, s := range steps {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		old := prev.lookup(s.id)
		p, err := s.prepare()
		key, build := p.key, p.build
		if err != nil {
			runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
			break
		}
		t := Target{ID: s.id, Kind: s.kind}
		if !b.Force && old != nil && old.Key == key && b.outputsIntact(old) {
			t.Status = UpToDate
			t.Outputs = old.paths()
			next.Targets = append(next.Targets, *old)
		} else if b.Check {
			var current bool
			var outs []string
			if p.encoded != "" && old == nil {
				// Another encoder, or another version of one, writes other bytes for the same input, so with no cache entry from this machine only the output's presence can be checked.
				_, err := os.Stat(b.abs(p.encoded))
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
					break
				}
				if err == nil {
					current, outs = true, []string{p.encoded}
				}
			} else {
				// Without a matching cache entry, as in a fresh checkout that does not commit the cache, the target is built in a temporary folder and is up to date when the files on disk are what it writes.
				current, outs, err = b.matchesDisk(build)
				if err != nil {
					runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
					break
				}
			}
			switch {
			case current:
				t.Status = UpToDate
				t.Outputs = outs
			case old != nil:
				t.Status = Stale
				t.Outputs = old.paths()
			default:
				t.Status = Stale
			}
		} else {
			outs, err := build(b.Root)
			if err != nil {
				runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
				break
			}
			entry, err := record(b.Root, s.id, key, outs)
			if err != nil {
				runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
				break
			}
			if old != nil {
				if err := b.removeStale(old, entry, written); err != nil {
					runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
					break
				}
			}
			t.Status = Built
			t.Outputs = entry.paths()
			next.Targets = append(next.Targets, *entry)
		}
		if s.atlas != nil && t.Status != Stale {
			// Pages of this atlas's name that it does not write, such as ui-1.png left by a build that needed more pages and committed without the cache, would be listed by the manifest as current.
			extra, err := b.extraPages(*s.atlas, t.Outputs)
			if err != nil {
				runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
				break
			}
			if len(extra) > 0 && b.Check {
				t.Status = Stale
				t.Outputs = nil
				if old != nil {
					t.Outputs = old.paths()
				}
			}
			if len(extra) > 0 && !b.Check {
				if err := b.remove(extra, written); err != nil {
					runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
					break
				}
			}
		}
		for _, o := range t.Outputs {
			if other, dup := written[o]; dup {
				runErr = fmt.Errorf("assetbuild: %s and %s both write %s", other, s.id, o)
				break
			}
			written[o] = s.id
		}
		r.Targets = append(r.Targets, t)
		if runErr != nil {
			break
		}
	}

	if runErr == nil && !b.Check {
		// A target renamed, moved or removed from game.yaml leaves its cache entry behind; its files go before the manifest scans for assets, so neither the output folders nor the manifest keep them.
		for _, e := range prev.Targets {
			if slices.ContainsFunc(steps, func(s step) bool { return s.id == e.Target }) {
				continue
			}
			if err := b.remove(e.paths(), written); err != nil {
				runErr = fmt.Errorf("assetbuild: remove the outputs of %s, no longer in %s: %w", e.Target, project.FileName, err)
				break
			}
		}
	}

	if runErr == nil && a.Manifest != nil {
		t, err := b.manifest(*a.Manifest)
		if err != nil && b.Check && r.Count(Stale) > 0 {
			// The manifest's folder may not exist until the stale targets are built, so it is stale with them.
			t.Status, err = Stale, nil
		}
		if err != nil {
			runErr = fmt.Errorf("assetbuild: %s: %w", t.ID, err)
		} else {
			r.Targets = append(r.Targets, t)
		}
	}

	if b.Check {
		return r, runErr
	}
	// After a failure, the targets the run did not reach keep their entries, so the next run does not rebuild what was already current, and so do targets no longer in game.yaml, so the next run that succeeds still removes their files. After a success, their entries are dropped with their files.
	if runErr != nil {
		for _, e := range prev.Targets {
			if next.lookup(e.Target) == nil {
				next.Targets = append(next.Targets, e)
			}
		}
	}
	if err := saveCache(cachePath, next); err != nil && runErr == nil {
		runErr = err
	}
	return r, runErr
}

// abs returns the filesystem path of rel, a slash-separated path relative to b.Root.
func (b *Builder) abs(rel string) string {
	return under(b.Root, rel)
}

// builder writes a target's outputs under root, the directory its slash-separated output paths are relative to, and returns the files it wrote.
type builder func(root string) ([]string, error)

// prepared is a target whose inputs and import settings were read: its key and what builds it.
type prepared struct {
	key   string
	build builder
	// encoded is the output of an AAC or MP3 target, which an external encoder writes, so its bytes depend on the machine.
	encoded string
}

// under returns the filesystem path of rel, a slash-separated path relative to root.
func under(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// matchesDisk builds a target in a new temporary folder and reports whether every file it writes is already in b.Root with the same content, with the slash-separated paths of those files. It writes nothing under b.Root.
func (b *Builder) matchesDisk(build builder) (current bool, outputs []string, err error) {
	tmp, err := os.MkdirTemp("", "game-assets-check-")
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	outs, err := build(tmp)
	if err != nil {
		return false, nil, err
	}
	e, err := record(tmp, "", "", outs)
	if err != nil {
		return false, nil, err
	}
	return b.outputsIntact(e), e.paths(), nil
}

// keyer accumulates what a target's key covers.
type keyer struct{ buf bytes.Buffer }

func newKeyer(b *Builder, kind Kind, settings any) (*keyer, error) {
	k := &keyer{}
	js, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&k.buf, "%s\nversion %q\nkind %s\nsettings %s\n", cacheFormat, b.Version, kind, js)
	return k, nil
}

// input adds one input file's path and content.
func (k *keyer) input(rel string, data []byte) {
	fmt.Fprintf(&k.buf, "input %q %s\n", rel, sum(data))
}

func (k *keyer) key() string { return sum(k.buf.Bytes()) }

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (b *Builder) atlas(a project.AtlasTarget) (prepared, error) {
	opts := pack.DefaultOptions()
	opts.Name = a.Name
	if a.Padding != nil {
		opts.Padding = *a.Padding
	}
	opts.Extrude = a.Extrude
	if a.MaxWidth != 0 {
		opts.MaxWidth = a.MaxWidth
	}
	if a.MaxHeight != 0 {
		opts.MaxHeight = a.MaxHeight
	}
	if a.Scale != 0 {
		opts.Scale = a.Scale
	}
	if a.Trim != nil {
		opts.Trim = *a.Trim
	}
	k, err := newKeyer(b, KindAtlas, opts)
	if err != nil {
		return prepared{}, err
	}
	inDir := b.abs(a.In)
	names, err := pack.ScanDir(inDir)
	if err != nil {
		return prepared{}, err
	}
	files := make([][]byte, len(names))
	sheets := make([]*importsettings.Slice, len(names))
	for i, name := range names {
		data, err := os.ReadFile(filepath.Join(inDir, filepath.FromSlash(name)))
		if err != nil {
			return prepared{}, err
		}
		files[i] = data
		k.input(name, data)
		rel := path.Join(a.In, name)
		im, sc, err := b.imageSettings(rel)
		if err != nil {
			return prepared{}, err
		}
		if im != nil {
			if set := textureSettings(im); len(set) > 0 && b.readers[rel]&readByTexture == 0 {
				return prepared{}, fmt.Errorf("%s: %s %s for a texture target, and none reads %s; an atlas packs it into its pages, so only slice applies", importsettings.Path(rel), strings.Join(set, ", "), are(len(set)), rel)
			}
			sheets[i] = im.Slice
		}
		if sc != nil {
			k.input(importsettings.Path(name), sc)
		}
	}
	return prepared{key: k.key(), build: func(root string) ([]string, error) {
		inputs := make([]pack.Input, 0, len(names))
		for i, name := range names {
			img, err := png.Decode(bytes.NewReader(files[i]))
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", path.Join(a.In, name), err)
			}
			if sheets[i] == nil {
				inputs = append(inputs, pack.Input{Name: name, Image: img})
				continue
			}
			frames, err := sliceFrames(name, img, *sheets[i])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", importsettings.Path(path.Join(a.In, name)), err)
			}
			inputs = append(inputs, frames...)
		}
		// A frame cut from a sheet may take the name of an image beside it, such as run_0.png beside run.png sliced; packing both would leave one of them out of the atlas's JSON.
		seen := make(map[string]bool, len(inputs))
		for _, in := range inputs {
			if seen[in.Name] {
				return nil, fmt.Errorf("two frames of %s are named %s; rename the image or the sheet sliced into that frame", a.In, in.Name)
			}
			seen[in.Name] = true
		}
		res, err := pack.PackImages(inputs, opts)
		if err != nil {
			return nil, err
		}
		return res.Write(under(root, a.Out))
	}}, nil
}

func (b *Builder) texture(t project.TextureTarget) (prepared, error) {
	im, sc, err := b.imageSettings(t.In)
	if err != nil {
		return prepared{}, err
	}
	var filter string
	if im != nil {
		if im.Slice != nil && b.readers[path.Clean(t.In)]&readByAtlas == 0 {
			return prepared{}, fmt.Errorf("%s: slice cuts a sprite sheet into an atlas's frames, and no atlas reads %s; a texture target encodes the whole image", importsettings.Path(t.In), t.In)
		}
		if im.Format != "" {
			t.Format = im.Format
		}
		if im.Mipmaps != nil {
			t.Mipmaps = *im.Mipmaps
		}
		if im.SRGB != nil {
			t.SRGB = *im.SRGB
		}
		filter = im.Filter
	}
	format, err := ktx2.ParseFormat(t.Format)
	if err != nil {
		return prepared{}, err
	}
	opts := ktx2.Options{Format: format, Mipmaps: t.Mipmaps, SRGB: t.SRGB}
	if filter != "" {
		opts.KVD = map[string]string{importsettings.FilterKey: filter}
	}
	k, err := newKeyer(b, KindTexture, opts)
	if err != nil {
		return prepared{}, err
	}
	data, err := os.ReadFile(b.abs(t.In))
	if err != nil {
		return prepared{}, err
	}
	k.input(t.In, data)
	if sc != nil {
		k.input(importsettings.Path(t.In), sc)
	}
	return prepared{key: k.key(), build: func(root string) ([]string, error) {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", t.In, err)
		}
		out, err := ktx2.Encode(img, opts)
		if err != nil {
			return nil, err
		}
		dest := under(root, t.Out)
		if err := writeFile(dest, out); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}}, nil
}

// audioSettings is what an audio target's key covers.
type audioSettings struct {
	Format  audioconv.Format
	Options audioconv.Options
	// Encoder is the external encoder an AAC or MP3 target is converted with, as audioconv.Encoder.Tool describes it, or "none" when there is none; it is empty for WAV.
	Encoder string
}

// encoderTool returns Audio's Tool for format, memoized for one Build so each format runs ffmpeg -version at most once per build. A missing encoder is "none": a target keyed with it is rebuilt, and fails, until an encoder is installed.
func (b *Builder) encoderTool(ctx context.Context, enc *audioconv.Encoder, format audioconv.Format) (string, error) {
	if tool, ok := b.tools[format]; ok {
		return tool, nil
	}
	tool, err := enc.Tool(ctx, format)
	if errors.Is(err, audioconv.ErrEncoderNotFound) {
		tool, err = "none", nil
	}
	if err != nil {
		return "", err
	}
	if b.tools == nil {
		b.tools = map[audioconv.Format]string{}
	}
	b.tools[format] = tool
	return tool, nil
}

func (b *Builder) audio(ctx context.Context, au project.AudioTarget) (prepared, error) {
	settings, sc, err := b.sidecar(au.In)
	if err != nil {
		return prepared{}, err
	}
	var loop *importsettings.Loop
	if settings != nil && settings.Image != nil {
		return prepared{}, fmt.Errorf("%s holds image settings, but an audio target converts %s as a sound", importsettings.Path(au.In), au.In)
	}
	if settings != nil && settings.Audio != nil {
		s := settings.Audio
		if s.Format != "" {
			au.Format = s.Format
		}
		if s.Rate != 0 {
			au.Rate = s.Rate
		}
		if s.Channels != 0 {
			au.Channels = s.Channels
		}
		if s.Bitrate != 0 {
			au.Bitrate = s.Bitrate
		}
		if s.PeakDB != nil || s.RMSDB != nil {
			au.PeakDB, au.RMSDB = s.PeakDB, s.RMSDB
		}
		loop = s.Loop
	}
	format := audioconv.FormatWAV
	if au.Format != "" {
		f, err := audioconv.ParseFormat(au.Format)
		if err != nil {
			return prepared{}, err
		}
		format = f
	}
	if loop != nil && format != audioconv.FormatWAV {
		return prepared{}, fmt.Errorf("%s: loop points need wav output, but %s converts %s to %s, which carries none", importsettings.Path(au.In), au.Out, au.In, format)
	}
	opts := audioconv.Options{SampleRate: au.Rate, Channels: au.Channels, PeakDBFS: au.PeakDB, RMSDBFS: au.RMSDB, Bitrate: au.Bitrate}
	if opts.Bitrate == 0 {
		opts.Bitrate = 128
	}
	enc := b.Audio
	if enc == nil {
		enc = &audioconv.Encoder{}
	}
	tool, err := b.encoderTool(ctx, enc, format)
	if err != nil {
		return prepared{}, err
	}
	k, err := newKeyer(b, KindAudio, audioSettings{format, opts, tool})
	if err != nil {
		return prepared{}, err
	}
	data, err := os.ReadFile(b.abs(au.In))
	if err != nil {
		return prepared{}, err
	}
	k.input(au.In, data)
	if settings != nil {
		k.input(importsettings.Path(au.In), sc)
	}
	p := prepared{key: k.key(), build: func(root string) ([]string, error) {
		buf, err := audioconv.DecodeWAV(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", au.In, err)
		}
		if loop != nil {
			end := loop.End
			if end == 0 {
				end = buf.Frames()
			}
			if loop.Start >= end || end > buf.Frames() {
				return nil, fmt.Errorf("%s: loop from frame %d to %d is not inside %s's %d frames", importsettings.Path(au.In), loop.Start, end, au.In, buf.Frames())
			}
			buf.Loop = &audioconv.Loop{Start: loop.Start, End: end}
		}
		dest := under(root, au.Out)
		if err := enc.ConvertBuffer(ctx, buf, dest, format, opts); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}}
	if format != audioconv.FormatWAV {
		p.encoded = au.Out
	}
	return p, nil
}

// sidecar reads the import settings beside the asset at rel, slash-separated and relative to b.Root: the parsed file and its bytes, or nil when the asset has none.
func (b *Builder) sidecar(rel string) (*importsettings.File, []byte, error) {
	name := importsettings.Path(rel)
	data, err := os.ReadFile(b.abs(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	f, err := importsettings.Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return f, data, nil
}

// inputs maps each file a's targets read to the kinds of target that read it. An atlas whose folder cannot be scanned adds nothing; its own step reports the error.
func (b *Builder) inputs(a *project.Assets) map[string]reader {
	m := map[string]reader{}
	for _, at := range a.Atlases {
		names, err := pack.ScanDir(b.abs(at.In))
		if err != nil {
			continue
		}
		for _, name := range names {
			m[path.Join(at.In, name)] |= readByAtlas
		}
	}
	for _, t := range a.Textures {
		m[path.Clean(t.In)] |= readByTexture
	}
	for _, au := range a.Audio {
		m[path.Clean(au.In)] |= readByAudio
	}
	return m
}

// checkSidecars refuses each sidecar in a folder a's targets read from (an atlas's folder and the folders below it, and the folder of each texture and audio input) whose asset no target reads, since its settings would apply to nothing; a sidecar holding only a credit section is allowed beside any file there of exactly its asset's name. On a case-insensitive file system a sidecar named Hero.PNG.import.yaml is read for hero.png, but not on a case-sensitive one, so it is refused everywhere. Hidden files and folders are skipped, as an atlas skips them.
func (b *Builder) checkSidecars(a *project.Assets) error {
	type folder struct {
		dir     string
		recurse bool
	}
	var folders []folder
	for _, at := range a.Atlases {
		folders = append(folders, folder{path.Clean(at.In), true})
	}
	for _, t := range a.Textures {
		folders = append(folders, folder{path.Dir(path.Clean(t.In)), false})
	}
	for _, au := range a.Audio {
		folders = append(folders, folder{path.Dir(path.Clean(au.In)), false})
	}
	seen := map[string]bool{}
	for _, f := range folders {
		root := b.abs(f.dir)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root && errors.Is(err, fs.ErrNotExist) {
					return fs.SkipAll
				}
				return err
			}
			if d.IsDir() {
				if p == root {
					return nil
				}
				if !f.recurse || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || !importsettings.IsSidecar(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(b.Root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				return nil
			}
			seen[rel] = true
			asset := strings.TrimSuffix(rel, importsettings.Suffix)
			if b.readers[asset] == 0 {
				// A sidecar holding only a credit says nothing about how the asset is built, so it may sit beside any file, such as a font in a texture's folder, as long as that file is there under exactly its name.
				creditOnly, err := b.creditOnly(rel)
				if err != nil {
					return err
				}
				if creditOnly {
					return nil
				}
				return fmt.Errorf("assetbuild: %s: no target in %s reads %s, so these import settings apply to nothing; name a sidecar exactly after its asset, case included, or remove it", rel, project.FileName, asset)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// creditOnly reports whether the sidecar at rel, slash-separated and relative to b.Root, holds only a credit section and sits beside a file named exactly as its asset, case included.
func (b *Builder) creditOnly(rel string) (bool, error) {
	data, err := os.ReadFile(b.abs(rel))
	if err != nil {
		return false, err
	}
	f, err := importsettings.Parse(data)
	if err != nil {
		return false, fmt.Errorf("assetbuild: %s: %w", rel, err)
	}
	if f.Image != nil || f.Audio != nil {
		return false, nil
	}
	entries, err := os.ReadDir(filepath.Dir(b.abs(rel)))
	if err != nil {
		return false, err
	}
	name := path.Base(strings.TrimSuffix(rel, importsettings.Suffix))
	for _, e := range entries {
		if e.Name() == name && !e.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

// imageSettings reads the import settings beside the image at rel, as sidecar does, and returns its image section, nil when it holds only a credit, and the sidecar's bytes, or nil and nil when the image has no sidecar.
func (b *Builder) imageSettings(rel string) (*importsettings.Image, []byte, error) {
	f, data, err := b.sidecar(rel)
	if err != nil || f == nil {
		return nil, nil, err
	}
	if f.Audio != nil {
		return nil, nil, fmt.Errorf("%s holds audio settings, but %s is read as an image", importsettings.Path(rel), rel)
	}
	return f.Image, data, nil
}

// textureSettings returns the keys of the settings im sets that only a texture target reads.
func textureSettings(im *importsettings.Image) []string {
	var set []string
	if im.Filter != "" {
		set = append(set, "filter")
	}
	if im.Format != "" {
		set = append(set, "format")
	}
	if im.Mipmaps != nil {
		set = append(set, "mipmaps")
	}
	if im.SRGB != nil {
		set = append(set, "srgb")
	}
	return set
}

// are returns the verb that agrees with n settings.
func are(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// sliceFrames cuts img into the frames s describes, row by row from the top left, naming frame i of name.png name_i.png.
func sliceFrames(name string, img image.Image, s importsettings.Slice) ([]pack.Input, error) {
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil, fmt.Errorf("cannot cut a %T image into frames", img)
	}
	bounds := img.Bounds()
	cols := cellsAlong(bounds.Dx(), s.Width, s.Margin, s.Spacing)
	rows := cellsAlong(bounds.Dy(), s.Height, s.Margin, s.Spacing)
	n := cols * rows
	if n == 0 {
		return nil, fmt.Errorf("no %dx%d cell fits the %dx%d image after a margin of %d", s.Width, s.Height, bounds.Dx(), bounds.Dy(), s.Margin)
	}
	if s.Count > n {
		return nil, fmt.Errorf("slice count %d is more than the %d cells that fit the %dx%d image", s.Count, n, bounds.Dx(), bounds.Dy())
	}
	if s.Count > 0 {
		n = s.Count
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	frames := make([]pack.Input, n)
	for i := range n {
		x := bounds.Min.X + s.Margin + (i%cols)*(s.Width+s.Spacing)
		y := bounds.Min.Y + s.Margin + (i/cols)*(s.Height+s.Spacing)
		frames[i] = pack.Input{Name: fmt.Sprintf("%s_%d%s", stem, i, ext), Image: sub.SubImage(image.Rect(x, y, x+s.Width, y+s.Height))}
	}
	return frames, nil
}

// cellsAlong returns how many cells of size cell, spaced spacing apart, fit along size after margin, as Tiled counts a tileset image's columns.
func cellsAlong(size, cell, margin, spacing int) int {
	if size-margin < cell {
		return 0
	}
	return (size-margin-cell)/(cell+spacing) + 1
}

// manifest generates the manifest and writes it only when it differs from the file there; it has no cache entry, because generating it reads no file's content and costs less than hashing.
func (b *Builder) manifest(m project.ManifestTarget) (Target, error) {
	t := Target{ID: "manifest " + m.Out, Kind: KindManifest, Outputs: []string{m.Out}}
	opts := manifest.DefaultOptions()
	if m.Package != "" {
		opts.Package = m.Package
	}
	if m.Prefix != "" {
		opts.Prefix = m.Prefix
	}
	opts.Embed = m.Embed
	opts.EmbedDir = m.EmbedDir
	src, err := manifest.Generate(b.abs(m.In), opts)
	if err != nil {
		return t, err
	}
	dest := b.abs(m.Out)
	cur, err := os.ReadFile(dest)
	switch {
	case err == nil && !b.Force && bytes.Equal(cur, src):
		t.Status = UpToDate
		return t, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return t, err
	case b.Check:
		t.Status = Stale
		return t, nil
	}
	if err := writeFile(dest, src); err != nil {
		return t, err
	}
	t.Status = Built
	return t, nil
}

// record hashes the files a target wrote under root into its cache entry.
func record(root, id, key string, outs []string) (*cacheEntry, error) {
	e := &cacheEntry{Target: id, Key: key}
	for _, o := range outs {
		data, err := os.ReadFile(o)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, o)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		e.Outputs = append(e.Outputs, cacheOutput{Path: rel, SHA256: sum(data)})
	}
	slices.SortFunc(e.Outputs, func(x, y cacheOutput) int { return strings.Compare(x.Path, y.Path) })
	return e, nil
}

// outputsIntact reports whether every output e records is on disk with the content it was written with.
func (b *Builder) outputsIntact(e *cacheEntry) bool {
	if len(e.Outputs) == 0 {
		return false
	}
	for _, o := range e.Outputs {
		if !fs.ValidPath(o.Path) {
			return false
		}
		data, err := os.ReadFile(b.abs(o.Path))
		if err != nil || sum(data) != o.SHA256 {
			return false
		}
	}
	return true
}

// removeStale deletes the files old recorded that next no longer writes, such as the pages of an atlas that now fits on fewer, so an output folder holds only current files. It removes only files the cache recorded as this target's, and none another target wrote in this run.
func (b *Builder) removeStale(old, next *cacheEntry, written map[string]string) error {
	var gone []string
	for _, o := range old.Outputs {
		if !slices.ContainsFunc(next.Outputs, func(n cacheOutput) bool { return n.Path == o.Path }) {
			gone = append(gone, o.Path)
		}
	}
	return b.remove(gone, written)
}

// remove deletes the files at paths, slash-separated and relative to b.Root, skipping any path that is not a valid relative path, that another target wrote in this run, or that names game.yaml. A file already gone is not an error.
func (b *Builder) remove(paths []string, written map[string]string) error {
	for _, p := range paths {
		if !fs.ValidPath(p) || p == "." || p == project.FileName {
			continue
		}
		if _, ok := written[p]; ok {
			continue
		}
		if err := os.Remove(b.abs(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// extraPages returns the slash-separated paths of the page files of a's name in its output folder, as game pack names them, that outputs does not list, sorted. A missing output folder has none.
func (b *Builder) extraPages(a project.AtlasTarget, outputs []string) ([]string, error) {
	entries, err := os.ReadDir(b.abs(a.Out))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var extra []string
	for _, e := range entries {
		p := path.Join(a.Out, e.Name())
		if e.Type().IsRegular() && isPage(a.Name, e.Name()) && !slices.Contains(outputs, p) {
			extra = append(extra, p)
		}
	}
	return extra, nil
}

// isPage reports whether base is the name of a page file game pack writes for the atlas name: name.png and name.json, or name-N.png and name-N.json when the atlas needs several pages. It matches the rule package project's Validate uses to keep other targets' files out of those names.
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

// writeFile writes data to name, creating its directory.
func writeFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}
