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

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
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
	// Check reports which targets are out of date without writing anything: their status is Stale instead of Built.
	Check bool
}

// cacheFormat is mixed into every target's key, so a change to what a key covers rebuilds every target.
const cacheFormat = "assetbuild/1"

// Build builds p's targets in order, skipping each whose key (a hash of its settings, its inputs' paths and contents, and Version) and outputs match the cache, then saves the cache when it changed. A second Build with nothing changed writes no file. When a target fails, Build saves what it built so far and returns the report up to that target with the error.
func (b *Builder) Build(ctx context.Context, p *Project) (*Report, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	if b.Force && b.Check {
		return nil, errors.New("assetbuild: Force and Check cannot both be set")
	}
	cachePath := b.abs(p.Assets.CachePath())
	prev := loadCache(cachePath)
	next := &cache{Version: cacheVersion}
	r := &Report{}

	type step struct {
		id   string
		kind Kind
		// prepare reads the target's inputs and returns its key and what builds it.
		prepare func() (key string, build func() ([]string, error), err error)
	}
	var steps []step
	for _, a := range p.Assets.Atlases {
		steps = append(steps, step{"atlas " + path.Join(a.Out, a.Name), KindAtlas, func() (string, func() ([]string, error), error) { return b.atlas(a) }})
	}
	for _, t := range p.Assets.Textures {
		steps = append(steps, step{"texture " + t.Out, KindTexture, func() (string, func() ([]string, error), error) { return b.texture(t) }})
	}
	for _, au := range p.Assets.Audio {
		steps = append(steps, step{"audio " + au.Out, KindAudio, func() (string, func() ([]string, error), error) { return b.audio(ctx, au) }})
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
		key, build, err := s.prepare()
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
			t.Status = Stale
			if old != nil {
				t.Outputs = old.paths()
			}
		} else {
			outs, err := build()
			if err != nil {
				runErr = fmt.Errorf("assetbuild: %s: %w", s.id, err)
				break
			}
			entry, err := b.record(s.id, key, outs)
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

	if runErr == nil && p.Assets.Manifest != nil {
		t, err := b.manifest(*p.Assets.Manifest)
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
	// After a failure, the targets the run did not reach keep their entries, so the next run does not rebuild what was already current. Entries of targets no longer in game.yaml are dropped.
	if runErr != nil {
		for _, s := range steps {
			if next.lookup(s.id) == nil {
				if e := prev.lookup(s.id); e != nil {
					next.Targets = append(next.Targets, *e)
				}
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
	return filepath.Join(b.Root, filepath.FromSlash(rel))
}

// rel returns the slash-separated path of name relative to b.Root.
func (b *Builder) rel(name string) (string, error) {
	r, err := filepath.Rel(b.Root, name)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(r), nil
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

func (b *Builder) atlas(a Atlas) (string, func() ([]string, error), error) {
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
		return "", nil, err
	}
	inDir := b.abs(a.In)
	names, err := pack.ScanDir(inDir)
	if err != nil {
		return "", nil, err
	}
	files := make([][]byte, len(names))
	for i, name := range names {
		data, err := os.ReadFile(filepath.Join(inDir, filepath.FromSlash(name)))
		if err != nil {
			return "", nil, err
		}
		files[i] = data
		k.input(name, data)
	}
	return k.key(), func() ([]string, error) {
		inputs := make([]pack.Input, len(names))
		for i, name := range names {
			img, err := png.Decode(bytes.NewReader(files[i]))
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", path.Join(a.In, name), err)
			}
			inputs[i] = pack.Input{Name: name, Image: img}
		}
		res, err := pack.PackImages(inputs, opts)
		if err != nil {
			return nil, err
		}
		return res.Write(b.abs(a.Out))
	}, nil
}

func (b *Builder) texture(t Texture) (string, func() ([]string, error), error) {
	format, err := ktx2.ParseFormat(t.Format)
	if err != nil {
		return "", nil, err
	}
	opts := ktx2.Options{Format: format, Mipmaps: t.Mipmaps, SRGB: t.SRGB}
	k, err := newKeyer(b, KindTexture, opts)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(b.abs(t.In))
	if err != nil {
		return "", nil, err
	}
	k.input(t.In, data)
	return k.key(), func() ([]string, error) {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", t.In, err)
		}
		out, err := ktx2.Encode(img, opts)
		if err != nil {
			return nil, err
		}
		dest := b.abs(t.Out)
		if err := writeFile(dest, out); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}, nil
}

// audioSettings is what an audio target's key covers.
type audioSettings struct {
	Format  audioconv.Format
	Options audioconv.Options
}

func (b *Builder) audio(ctx context.Context, au Audio) (string, func() ([]string, error), error) {
	format := audioconv.FormatWAV
	if au.Format != "" {
		f, err := audioconv.ParseFormat(au.Format)
		if err != nil {
			return "", nil, err
		}
		format = f
	}
	opts := audioconv.Options{SampleRate: au.Rate, Channels: au.Channels, PeakDBFS: au.PeakDB, RMSDBFS: au.RMSDB, Bitrate: au.Bitrate}
	if opts.Bitrate == 0 {
		opts.Bitrate = 128
	}
	k, err := newKeyer(b, KindAudio, audioSettings{format, opts})
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(b.abs(au.In))
	if err != nil {
		return "", nil, err
	}
	k.input(au.In, data)
	return k.key(), func() ([]string, error) {
		buf, err := audioconv.DecodeWAV(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", au.In, err)
		}
		enc := b.Audio
		if enc == nil {
			enc = &audioconv.Encoder{}
		}
		dest := b.abs(au.Out)
		if err := enc.ConvertBuffer(ctx, buf, dest, format, opts); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}, nil
}

// manifest generates the manifest and writes it only when it differs from the file there; it has no cache entry, because generating it reads no file's content and costs less than hashing.
func (b *Builder) manifest(m Manifest) (Target, error) {
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

// record hashes the files a target wrote into its cache entry.
func (b *Builder) record(id, key string, outs []string) (*cacheEntry, error) {
	e := &cacheEntry{Target: id, Key: key}
	for _, o := range outs {
		data, err := os.ReadFile(o)
		if err != nil {
			return nil, err
		}
		rel, err := b.rel(o)
		if err != nil {
			return nil, err
		}
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
	for _, o := range old.Outputs {
		if !fs.ValidPath(o.Path) || slices.ContainsFunc(next.Outputs, func(n cacheOutput) bool { return n.Path == o.Path }) {
			continue
		}
		if _, ok := written[o.Path]; ok {
			continue
		}
		if err := os.Remove(b.abs(o.Path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeFile writes data to name, creating its directory.
func writeFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}
