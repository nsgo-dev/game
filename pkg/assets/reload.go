package assets

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/internal/devimage"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Reload replaces the loaded assets that were read from uri with data, the file's new contents, and returns the names of the assets it replaced, in the order of their groups and of the assets in each; hot reload (tooling/hotreload's Watcher.WatchAssets) calls it when a file changes on disk. It runs on the engine goroutine, as the Manager's other methods do.
//
// An atlas whose JSON is uri is parsed again with sprite.LoadAtlas, and one whose image is uri draws data from then on, its frames unchanged; either way the *sprite.Atlas the group holds is updated in place, so code that kept the pointer sees the edit. An image whose source is uri shows data, and raw bytes read from uri become data. For an image, an atlas's too, that ships in several densities (core.ImageSource.Scales), uri is the file the screen's pixel ratio picks, such as "asset:hero@2x.png" on a 2x screen, and the image keeps that file's name and so its density; saving a density the screen does not show changes nothing. Sounds and custom assets are not replaced. Assets a group has not loaded are skipped, as are an atlas's JSON and raw bytes that a group reads from the file system SetFS mounted on it, such as a DLC pack overriding the base file at uri, and an atlas whose new JSON does not parse keeps its previous frames and adds its error to the one returned.
//
// When it replaces anything, Reload increments Revision, so a core.Canvas or core.Effect that reads Revision draws or runs again.
func (m *Manager) Reload(uri string, data []byte) ([]string, error) {
	if m.isDisposed() {
		return nil, nil
	}
	want := normalizeURI(uri)
	same := func(a, b string) bool { return normalizeURI(a) == normalizeURI(b) }
	ratio := core.Untracked(m.cx, func() float32 { return m.cx.Env().PixelRatio() })
	var (
		names []string
		errs  []error
	)
	for _, groupName := range m.groupOrder {
		g, ok := m.groups[groupName]
		if !ok {
			continue
		}
		for _, spec := range g.specs {
			switch spec.kind {
			case kindAtlas:
				cur, ok := g.atlases[spec.name]
				if !ok || cur == nil {
					continue
				}
				if normalizeURI(spec.uri) == want {
					if g.readsFromPack(spec.uri) {
						continue
					}
					next, err := sprite.LoadAtlas(cur.Image, data)
					if err != nil {
						errs = append(errs, fmt.Errorf("assets: atlas %q: %w", spec.name, err))
						continue
					}
					*cur = *next
					if g.rawBytes == nil {
						g.rawBytes = map[string]int64{}
					}
					g.rawBytes[spec.name] = int64(len(data))
				} else if img, ok := devimage.Reloaded(spec.img, uri, data, ratio, same); ok {
					cur.Image = img
				} else {
					continue
				}
			case kindImage:
				if _, ok := g.images[spec.name]; !ok {
					continue
				}
				img, ok := devimage.Reloaded(spec.img, uri, data, ratio, same)
				if !ok {
					continue
				}
				g.images[spec.name] = img
				if g.rawBytes == nil {
					g.rawBytes = map[string]int64{}
				}
				g.rawBytes[spec.name] = int64(len(data))
			case kindBytes:
				if _, ok := g.bytes[spec.name]; !ok || normalizeURI(spec.uri) != want || g.readsFromPack(spec.uri) {
					continue
				}
				g.bytes[spec.name] = data
			default:
				continue
			}
			names = append(names, spec.name)
		}
	}
	if len(names) > 0 {
		m.revision.Set(m.revision.GetUntracked() + 1)
	}
	return names, errors.Join(errs...)
}

// readsFromPack reports whether g reads uri from the file system SetFS mounted on it, such as a DLC pack's, rather than from the Manager's reader: the pack has the file, so the group shows the pack's copy and a change to the base file is not one it sees.
func (g *Group) readsFromPack(uri string) bool {
	if g.fs == nil {
		return false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(uri, "asset:"), "/")
	if !fs.ValidPath(rel) {
		return false
	}
	_, err := fs.Stat(g.fs, rel)
	return err == nil
}

// Revision counts the Reload, ReloadAsset and ReloadGroup calls that replaced at least one asset; each such call adds one. Reading it inside a core.Canvas Draw callback or a core.Effect subscribes to later reloads.
func (m *Manager) Revision() int {
	return m.revision.Get()
}

// normalizeURI returns uri with an "asset:" path cleaned and without a leading slash, as the default reader resolves it, so "asset:/ui/../hero.json" and "asset:hero.json" name the same file.
func normalizeURI(uri string) string {
	rel, ok := strings.CutPrefix(uri, "asset:")
	if !ok {
		return uri
	}
	return "asset:" + strings.TrimPrefix(path.Clean("/"+rel), "/")
}

// ReloadedAsset describes one loaded asset that ReloadAsset or ReloadGroup matched. Err holds a read or parse failure, after which the previous asset stays in place. Skipped says why an asset was left unchanged without an error.
type ReloadedAsset struct {
	Group   string
	Name    string
	Kind    string
	URI     string
	Bytes   int64
	Frames  int
	Skipped string
	Err     error
}

// Reasons ReloadAsset and ReloadGroup give in ReloadedAsset.Skipped.
const (
	// SkippedSound marks a sound: sounds decode asynchronously on the platform, so they are not replaced in place.
	SkippedSound = "sounds are not reloaded in place"
	// SkippedNoFile marks an image with no file behind it, such as one registered with in-memory pixels, or, in ReloadGroup, one whose URI the group's reader cannot find.
	SkippedNoFile = "no file to reload from"
	// SkippedDensity marks an image file at a density the screen does not show, so replacing it would change nothing on screen.
	SkippedDensity = "file is at a density the screen does not show"
)

// ReloadAsset reloads the loaded assets that match key by asset name, URI or relative path, reading fresh bytes through each group's reader (the group's FS, Manager.ReadAsset, or the Manager's FS). When groupName is non-empty, only that group is searched. It returns an empty slice and a nil error when no loaded asset matches key, and an error when groupName names no group.
//
// The reads, atlas parses and custom loaders run off the engine goroutine, as they do in Group.Load, while ReloadAsset waits for them, so a reader or loader must not wait on the engine goroutine. When ctx is done first, ReloadAsset returns ctx's error and changes nothing. The results are applied on the engine goroutine: an atlas is updated in place (*cur = *next), so the *sprite.Atlas pointers the game holds stay valid, and a failed read or parse leaves the previous asset in place and is reported both in the item's Err and in the joined error. Sounds, images with no file behind them and files at a density the screen does not show are reported in Skipped and left unchanged. Revision goes up by one when at least one asset was replaced.
//
// Call ReloadAsset on the engine goroutine.
func (m *Manager) ReloadAsset(ctx context.Context, groupName, key string) ([]ReloadedAsset, error) {
	if m == nil || m.isDisposed() {
		return nil, errors.New("assets: manager is disposed")
	}
	if ctx == nil {
		ctx = m.ctx
	}
	var groups []*Group
	if groupName != "" {
		g, ok := m.groups[groupName]
		if !ok {
			return nil, fmt.Errorf("assets: group %q not found", groupName)
		}
		groups = []*Group{g}
	} else {
		groups = make([]*Group, 0, len(m.groupOrder))
		for _, name := range m.groupOrder {
			if g, ok := m.groups[name]; ok {
				groups = append(groups, g)
			}
		}
	}

	ratio := core.Untracked(m.cx, func() float32 { return m.cx.Env().PixelRatio() })
	var jobs []*reloadJob
	for _, g := range groups {
		for _, spec := range g.specs {
			matchMode, matchedURI := matchSpec(spec, key, ratio)
			if matchMode == specMatchNone || !g.isSpecLoaded(spec) {
				continue
			}
			jobs = append(jobs, g.planReload(spec, matchMode, matchedURI, ratio, false))
		}
	}
	return m.runReloads(ctx, jobs)
}

// ReloadGroup reloads every loaded asset in the named group in place, as ReloadAsset does for one key. Images whose file the group's reader cannot find are reported as skipped rather than failed, so a group that mixes files with generated textures reloads cleanly.
//
// Call ReloadGroup on the engine goroutine.
func (m *Manager) ReloadGroup(ctx context.Context, groupName string) ([]ReloadedAsset, error) {
	if m == nil || m.isDisposed() {
		return nil, errors.New("assets: manager is disposed")
	}
	g, ok := m.groups[groupName]
	if !ok {
		return nil, fmt.Errorf("assets: group %q not found", groupName)
	}
	if g.state == stateLoading {
		return nil, fmt.Errorf("assets: group %q is currently loading", groupName)
	}
	if g.state != stateLoaded {
		return nil, fmt.Errorf("assets: group %q is not loaded (state: %s)", groupName, groupStateName(g.state))
	}
	if ctx == nil {
		ctx = m.ctx
	}

	ratio := core.Untracked(m.cx, func() float32 { return m.cx.Env().PixelRatio() })
	jobs := make([]*reloadJob, 0, len(g.specs))
	for _, spec := range g.specs {
		if !g.isSpecLoaded(spec) {
			continue
		}
		jobs = append(jobs, g.planReload(spec, specMatchName, "", ratio, true))
	}
	return m.runReloads(ctx, jobs)
}

type specMatchKind uint8

const (
	specMatchNone specMatchKind = iota
	specMatchName
	specMatchURI
	specMatchImageURI
)

func matchSpec(spec assetSpec, key string, ratio float32) (specMatchKind, string) {
	if key == "" {
		return specMatchNone, ""
	}
	if spec.name == key {
		return specMatchName, ""
	}
	if spec.uri != "" && matchURI(spec.uri, key) {
		return specMatchURI, spec.uri
	}
	if spec.img.URI != "" {
		shown := devimage.Shown(spec.img, ratio)
		if matchURI(shown, key) {
			return specMatchImageURI, shown
		}
		if shown != spec.img.URI && matchURI(spec.img.URI, key) {
			return specMatchImageURI, spec.img.URI
		}
	}
	if spec.sound.URI != "" && matchURI(spec.sound.URI, key) {
		return specMatchURI, spec.sound.URI
	}
	return specMatchNone, ""
}

func matchURI(specURI, key string) bool {
	normSpec := normalizeURI(specURI)
	if normalizeURI(key) == normSpec {
		return true
	}
	if !strings.HasPrefix(key, "asset:") && !strings.HasPrefix(key, "file://") {
		if normalizeURI("asset:"+strings.TrimPrefix(key, "/")) == normSpec {
			return true
		}
	}
	return false
}

func (g *Group) isSpecLoaded(spec assetSpec) bool {
	switch spec.kind {
	case kindAtlas:
		a, ok := g.atlases[spec.name]
		return ok && a != nil
	case kindSound:
		s, ok := g.sounds[spec.name]
		return ok && s != nil
	case kindImage:
		_, ok := g.images[spec.name]
		return ok
	case kindBytes:
		_, ok := g.bytes[spec.name]
		return ok
	case kindCustom:
		_, ok := g.custom[spec.name]
		return ok
	default:
		return false
	}
}

// reloadJob is one asset ReloadAsset or ReloadGroup reloads. planReload fills the plan on the engine goroutine, fetch fills the fetched fields off it, and apply swaps the result in on the engine goroutine.
type reloadJob struct {
	g     *Group
	gen   uint64
	spec  assetSpec
	res   ReloadedAsset
	bulk  bool
	ratio float32

	readFn   func(context.Context, string) ([]byte, error)
	dataURI  string
	imgURI   string
	imgOnly  bool
	curImage core.ImageSource

	data    []byte
	img     core.ImageSource
	imgOK   bool
	atlas   *sprite.Atlas
	custom  any
	readErr error
	err     error
}

func (g *Group) reloadReadAssetFunc() func(context.Context, string) ([]byte, error) {
	if g.m.ReadAsset != nil || g.m.fs != nil {
		return g.readAssetFunc()
	}
	// ReloadAsset and ReloadGroup wait on the engine goroutine while fetch runs
	// off-thread, so they cannot post back to core.Scope.ReadAsset without
	// deadlocking. Read from g.fs when mounted, and refuse "asset:" reads that
	// would require an async Scope.ReadAsset round trip.
	baseRead := func(ctx context.Context, uri string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(uri, "asset:") {
			return nil, fmt.Errorf("assets: cannot reload %q from app resources without WithFS or WithReadAsset", uri)
		}
		if p, ok := strings.CutPrefix(uri, "file://"); ok {
			return os.ReadFile(filepath.FromSlash(p))
		}
		return os.ReadFile(uri)
	}
	fsys := g.fs
	if fsys == nil {
		return baseRead
	}
	return func(ctx context.Context, uri string) ([]byte, error) {
		data, err := fsReadAsset(fsys, ctx, uri)
		if err == nil {
			return data, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			if fallbackData, fallbackErr := baseRead(ctx, uri); fallbackErr == nil {
				return fallbackData, nil
			}
		}
		return nil, err
	}
}

func (g *Group) planReload(spec assetSpec, mode specMatchKind, matchedURI string, ratio float32, bulk bool) *reloadJob {
	j := &reloadJob{
		g:      g,
		gen:    g.gen,
		spec:   spec,
		bulk:   bulk,
		ratio:  ratio,
		readFn: g.reloadReadAssetFunc(),
		res: ReloadedAsset{
			Group: g.name,
			Name:  spec.name,
			Kind:  assetKindName(spec.kind),
			URI:   specURI(spec),
		},
	}
	switch spec.kind {
	case kindSound:
		j.res.Skipped = SkippedSound
	case kindAtlas:
		j.curImage = g.atlases[spec.name].Image
		if mode == specMatchImageURI {
			j.imgOnly = true
			j.planImage(matchedURI)
			return j
		}
		j.dataURI = spec.uri
		if mode == specMatchName && spec.img.URI != "" {
			j.imgURI = devimage.Shown(spec.img, ratio)
		}
	case kindImage:
		j.planImage(matchedURI)
	case kindBytes, kindCustom:
		j.dataURI = spec.uri
	}
	return j
}

func (j *reloadJob) planImage(matchedURI string) {
	if j.spec.img.URI == "" {
		j.res.Skipped = SkippedNoFile
		return
	}
	shown := devimage.Shown(j.spec.img, j.ratio)
	target := matchedURI
	if target == "" {
		target = shown
	}
	j.res.URI = target
	if normalizeURI(target) != normalizeURI(shown) {
		j.res.Skipped = SkippedDensity
		return
	}
	j.imgURI = target
}

// fetch reads and parses the job's files. It runs off the engine goroutine and touches only the job.
func (j *reloadJob) fetch(ctx context.Context) {
	if j.res.Skipped != "" {
		return
	}
	same := func(a, b string) bool { return normalizeURI(a) == normalizeURI(b) }
	name := j.spec.name
	if j.imgURI != "" {
		data, err := j.readFn(ctx, j.imgURI)
		switch {
		case err == nil && len(data) > 0:
			j.img, j.imgOK = devimage.Reloaded(j.spec.img, j.imgURI, data, j.ratio, same)
		case j.imgOnly || j.spec.kind == kindImage:
			if err == nil {
				err = errors.New("file is empty")
			}
			j.readErr = err
			j.err = fmt.Errorf("assets: %s %q: read %q: %w", assetKindName(j.spec.kind), name, j.imgURI, err)
			return
		}
	}
	if j.dataURI == "" {
		return
	}
	data, err := j.readFn(ctx, j.dataURI)
	if err != nil {
		j.readErr = err
		j.err = fmt.Errorf("assets: %s %q: read %q: %w", assetKindName(j.spec.kind), name, j.dataURI, err)
		return
	}
	j.data = data
	switch j.spec.kind {
	case kindAtlas:
		img := j.curImage
		if j.imgOK {
			img = j.img
		}
		atlas, err := sprite.LoadAtlas(img, data)
		if err != nil {
			j.err = fmt.Errorf("assets: atlas %q: %w", name, loaderr.WithFile(err, j.dataURI))
			return
		}
		j.atlas = atlas
	case kindCustom:
		var val any = data
		if j.spec.loader != nil {
			val, err = j.spec.loader(ctx, data)
			if err != nil {
				j.err = fmt.Errorf("assets: custom %q: %w", name, err)
				return
			}
		}
		j.custom = val
	}
}

// releaseUnused releases a custom value the job loaded but did not apply.
func (j *reloadJob) releaseUnused(m *Manager) {
	if j.custom == nil || j.spec.release == nil {
		return
	}
	v, release := j.custom, j.spec.release
	j.custom = nil
	done := func() { release(v) }
	m.postWithCleanup(done, done)
}

// runReloads fetches every job off the engine goroutine, waits for the fetches or for ctx, and applies the results on the engine goroutine.
func (m *Manager) runReloads(ctx context.Context, jobs []*reloadJob) ([]ReloadedAsset, error) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, j := range jobs {
			if ctx.Err() != nil {
				return
			}
			j.fetch(ctx)
		}
	}()
	// Waits on the engine goroutine instead of posting the apply back, because an automation tool handler can only answer synchronously (nsgo-dev/nsgo#2342).
	select {
	case <-done:
	case <-ctx.Done():
		go func() {
			<-done
			for _, j := range jobs {
				j.releaseUnused(m)
			}
		}()
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		for _, j := range jobs {
			j.releaseUnused(m)
		}
		return nil, err
	}

	results := make([]ReloadedAsset, 0, len(jobs))
	var (
		errs     []error
		replaced bool
	)
	for _, j := range jobs {
		if j.apply(m) {
			replaced = true
		}
		if j.res.Err != nil {
			errs = append(errs, j.res.Err)
		}
		results = append(results, j.res)
	}
	if replaced {
		m.revision.Set(m.revision.GetUntracked() + 1)
	}
	return results, errors.Join(errs...)
}

// apply swaps the fetched result in on the engine goroutine and reports whether it replaced the asset.
func (j *reloadJob) apply(m *Manager) bool {
	g, spec := j.g, j.spec
	defer func() { j.res.Bytes, j.res.Frames = g.reloadedSize(spec) }()
	if j.res.Skipped != "" {
		return false
	}
	if m.isDisposed() || g.gen != j.gen || g.state != stateLoaded || !g.isSpecLoaded(spec) {
		j.releaseUnused(m)
		j.res.Err = fmt.Errorf("assets: %s %q: group %q changed during the reload", j.res.Kind, spec.name, g.name)
		return false
	}
	if j.err != nil {
		if j.bulk && spec.kind == kindImage && (errors.Is(j.readErr, fs.ErrNotExist) || errors.Is(j.readErr, core.ErrAssetNotFound)) {
			j.res.Skipped = SkippedNoFile
			return false
		}
		j.res.Err = j.err
		return false
	}
	if g.rawBytes == nil {
		g.rawBytes = map[string]int64{}
	}
	switch spec.kind {
	case kindAtlas:
		cur := g.atlases[spec.name]
		if j.imgOnly {
			if !j.imgOK {
				j.res.Skipped = SkippedDensity
				return false
			}
			cur.Image = j.img
			return true
		}
		*cur = *j.atlas
		g.rawBytes[spec.name] = int64(len(j.data))
	case kindImage:
		if !j.imgOK {
			j.res.Skipped = SkippedDensity
			return false
		}
		g.images[spec.name] = j.img
		g.rawBytes[spec.name] = int64(len(j.img.Data))
	case kindBytes:
		g.bytes[spec.name] = j.data
	case kindCustom:
		old := g.custom[spec.name]
		if old.release != nil && old.value != nil {
			old.release(old.value)
		}
		g.custom[spec.name] = customEntry{value: j.custom, release: spec.release}
		g.rawBytes[spec.name] = int64(len(j.data))
		j.custom = nil
	default:
		return false
	}
	return true
}

// reloadedSize reports the estimated bytes and atlas frame count of a loaded asset.
func (g *Group) reloadedSize(spec assetSpec) (int64, int) {
	switch spec.kind {
	case kindAtlas:
		if a := g.atlases[spec.name]; a != nil {
			return EstimateAtlasBytes(a, g.rawBytes[spec.name]), len(a.Frames)
		}
	case kindImage:
		return estimateImageBytes(g.images[spec.name]), 0
	case kindBytes:
		return int64(len(g.bytes[spec.name])), 0
	case kindCustom:
		return estimateCustomBytes(g.custom[spec.name].value, g.rawBytes[spec.name]), 0
	case kindSound:
		return g.assetStats(spec).Bytes, 0
	}
	return 0, 0
}
