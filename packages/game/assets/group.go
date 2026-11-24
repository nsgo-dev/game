package assets

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

type assetKind uint8

const (
	kindAtlas assetKind = iota + 1
	kindSound
	kindImage
	kindBytes
	kindCustom
)

type groupState uint8

const (
	stateIdle groupState = iota
	stateLoading
	stateLoaded
	stateFailed
)

type assetSpec struct {
	name    string
	kind    assetKind
	uri     string
	img     core.ImageSource
	sound   audio.Source
	loader  CustomLoader
	release func(any)
}

type customEntry struct {
	value   any
	release func(any)
}

// Group is a named collection of assets loaded and released together, typically
// one per scene. Its progress and status methods are backed by core.Signal so
// NSGo views and effects update automatically as assets load or are released.
type Group struct {
	m    *Manager
	name string
	fs   fs.FS
	pack *mountedPack // the pack FetchGroup mounted as fs, released when it is replaced

	specs []assetSpec
	index map[string]int

	state      groupState
	gen        uint64
	loaded     int
	loadCancel context.CancelFunc
	waiters    []func(error)

	atlases map[string]*sprite.Atlas
	sounds  map[string]*audio.Sound
	images  map[string]core.ImageSource
	bytes   map[string][]byte
	custom  map[string]customEntry

	progressSig *core.Signal[float64]
	loadedSig   *core.Signal[int]
	totalSig    *core.Signal[int]
	doneSig     *core.Signal[bool]
	loadingSig  *core.Signal[bool]
	errSig      *core.Signal[error]
}

func newGroup(m *Manager, name string) *Group {
	g := &Group{
		m:     m,
		name:  name,
		index: map[string]int{},
	}
	m.cx.Run(func() {
		g.progressSig = core.NewSignal(m.cx, float64(0))
		g.loadedSig = core.NewSignal(m.cx, 0)
		g.totalSig = core.NewSignal(m.cx, 0)
		g.doneSig = core.NewSignal(m.cx, false)
		g.loadingSig = core.NewSignal(m.cx, false)
		g.errSig = core.NewSignalFunc[error](m.cx, nil, func(a, b error) bool {
			return a == nil && b == nil
		})
	})
	return g
}

// Name returns the group's name.
func (g *Group) Name() string { return g.name }

// SetFS mounts fsys as the primary file system for this group's file-backed assets, such as a downloaded DLC zip archive. When set, asset URIs in this group are read from fsys first and fall back to the Manager's asset reader if a file is not present in fsys.
//
// Replacing the pack DLCManager.FetchGroup mounted on the group releases that pack, as Reset does; setting that same pack again keeps it.
func (g *Group) SetFS(fsys fs.FS) *Group {
	if g.pack == nil || fsys != fs.FS(g.pack) {
		g.releasePack()
	}
	g.fs = fsys
	return g
}

// FS returns the file system mounted on this group via SetFS, or nil.
func (g *Group) FS() fs.FS { return g.fs }

// AddAtlas adds a sprite atlas whose JSON is read from jsonURI and parsed with
// sprite.LoadAtlas for img. When img names a URI without inline Data or Pixels,
// Load also prefetches img via core.Scope.PrefetchImage (in nsgotest, register
// the URI in App.ImageSizes).
func (g *Group) AddAtlas(name, jsonURI string, img core.ImageSource) *Group {
	return g.addSpec(assetSpec{
		name: name,
		kind: kindAtlas,
		uri:  jsonURI,
		img:  img,
	})
}

// AddSound adds a short sound decoded through audio.LoadSound.
func (g *Group) AddSound(name string, src audio.Source) *Group {
	return g.addSpec(assetSpec{
		name:  name,
		kind:  kindSound,
		sound: src,
	})
}

// AddImage adds an image source. When src names a URI without inline Data or
// Pixels, Load prefetches it via core.Scope.PrefetchImage (in nsgotest,
// register the URI in App.ImageSizes).
//
// Keeping decoded images pinned outside NSGo's 64-source canvas cache waits on
// nsgo-dev/nsgo#1127.
func (g *Group) AddImage(name string, src core.ImageSource) *Group {
	return g.addSpec(assetSpec{
		name: name,
		kind: kindImage,
		img:  src,
	})
}

// AddBytes adds a raw byte asset read from uri off the engine goroutine.
func (g *Group) AddBytes(name, uri string) *Group {
	return g.addSpec(assetSpec{
		name: name,
		kind: kindBytes,
		uri:  uri,
	})
}

// AddCustom adds an arbitrary asset read from uri and parsed off the engine
// goroutine by parse, such as a tilemap, bitmap font or JSON table. When
// release is non-nil, it is invoked when the group is released or when a late
// load is discarded after release or scope disposal.
func (g *Group) AddCustom(name, uri string, parse func([]byte) (any, error), release func(any)) *Group {
	var loader CustomLoader
	if parse != nil {
		loader = func(_ context.Context, data []byte) (any, error) {
			return parse(data)
		}
	}
	return g.addSpec(assetSpec{
		name:    name,
		kind:    kindCustom,
		uri:     uri,
		loader:  loader,
		release: release,
	})
}

// AddCustomLoader is like AddCustom, passing the load's cancellable context to
// load so long parses can stop promptly on Release or scope disposal.
func (g *Group) AddCustomLoader(name, uri string, load CustomLoader, release func(any)) *Group {
	return g.addSpec(assetSpec{
		name:    name,
		kind:    kindCustom,
		uri:     uri,
		loader:  load,
		release: release,
	})
}

func (g *Group) addSpec(spec assetSpec) *Group {
	if i, ok := g.index[spec.name]; ok {
		g.specs[i] = spec
	} else {
		g.index[spec.name] = len(g.specs)
		g.specs = append(g.specs, spec)
	}
	if g.state == stateIdle && !g.m.isDisposed() {
		g.totalSig.Set(len(g.specs))
	}
	return g
}

// Progress returns loading progress in [0, 1]. Read in a reactive computation,
// it subscribes to progress updates.
func (g *Group) Progress() float64 { return g.progressSig.Get() }

// Loaded returns how many assets in the group have finished loading. Read in a
// reactive computation, it subscribes to updates.
func (g *Group) Loaded() int { return g.loadedSig.Get() }

// Total returns the number of assets in the group. Read in a reactive
// computation, it subscribes to updates.
func (g *Group) Total() int { return g.totalSig.Get() }

// Done reports whether every asset in the group finished loading without error.
// Read in a reactive computation, it subscribes to updates.
func (g *Group) Done() bool { return g.doneSig.Get() }

// Loading reports whether the group is currently loading. Read in a reactive
// computation, it subscribes to updates.
func (g *Group) Loading() bool { return g.loadingSig.Get() }

// Err returns the error that stopped the group's last load, or nil. Read in a
// reactive computation, it subscribes to updates.
func (g *Group) Err() error { return g.errSig.Get() }

// Atlas returns the loaded atlas named name, if present in the group.
func (g *Group) Atlas(name string) (*sprite.Atlas, bool) {
	a, ok := g.atlases[name]
	return a, ok
}

// Sound returns the loaded sound named name, if present in the group.
func (g *Group) Sound(name string) (*audio.Sound, bool) {
	s, ok := g.sounds[name]
	return s, ok
}

// Image returns the loaded image source named name, if present in the group.
func (g *Group) Image(name string) (core.ImageSource, bool) {
	img, ok := g.images[name]
	return img, ok
}

// Bytes returns the loaded raw bytes named name, if present in the group.
func (g *Group) Bytes(name string) ([]byte, bool) {
	b, ok := g.bytes[name]
	return b, ok
}

// Get returns any loaded asset named name (custom, atlas, sound, image or raw
// bytes), if present in the group.
func (g *Group) Get(name string) (any, bool) {
	if c, ok := g.custom[name]; ok {
		return c.value, true
	}
	if a, ok := g.atlases[name]; ok {
		return a, true
	}
	if s, ok := g.sounds[name]; ok {
		return s, true
	}
	if img, ok := g.images[name]; ok {
		return img, true
	}
	if b, ok := g.bytes[name]; ok {
		return b, true
	}
	return nil, false
}

// Load starts loading every asset in the group asynchronously and invokes
// onDone on the engine goroutine once finished.
//
// Load is idempotent: if the group is already loaded, onDone(nil) runs
// immediately; if it is currently loading, onDone is queued onto the active
// load. An empty group completes immediately with progress 1.
func (g *Group) Load(onDone func(error)) {
	if g.m.isDisposed() {
		return
	}
	switch g.state {
	case stateLoaded:
		if onDone != nil {
			onDone(nil)
		}
		return
	case stateLoading:
		if onDone != nil {
			g.waiters = append(g.waiters, onDone)
		}
		return
	case stateFailed:
		// Clear any partial state before retrying.
		g.releaseResources(false)
	}

	total := len(g.specs)
	if total == 0 {
		g.state = stateLoaded
		g.loaded = 0
		g.m.cx.Batch(func() {
			g.loadedSig.Set(0)
			g.totalSig.Set(0)
			g.progressSig.Set(1)
			g.loadingSig.Set(false)
			g.doneSig.Set(true)
			g.errSig.Set(nil)
		})
		if onDone != nil {
			onDone(nil)
		}
		return
	}

	g.gen++
	gen := g.gen
	g.state = stateLoading
	g.loaded = 0
	if onDone != nil {
		g.waiters = append(g.waiters[:0], onDone)
	} else {
		g.waiters = nil
	}
	loadCtx, cancel := context.WithCancel(g.m.ctx)
	g.loadCancel = cancel

	g.atlases = map[string]*sprite.Atlas{}
	g.sounds = map[string]*audio.Sound{}
	g.images = map[string]core.ImageSource{}
	g.bytes = map[string][]byte{}
	g.custom = map[string]customEntry{}

	g.m.cx.Batch(func() {
		g.loadedSig.Set(0)
		g.totalSig.Set(total)
		g.progressSig.Set(0)
		g.loadingSig.Set(true)
		g.doneSig.Set(false)
		g.errSig.Set(nil)
	})

	readFn := g.readAssetFunc()
	specs := make([]assetSpec, len(g.specs))
	copy(specs, g.specs)

	for _, spec := range specs {
		g.startSpec(gen, loadCtx, readFn, spec)
	}
}

func (g *Group) readAssetFunc() func(context.Context, string) ([]byte, error) {
	baseRead := g.m.readAssetFunc()
	fsys := g.fs
	if fsys == nil {
		return baseRead
	}
	return func(ctx context.Context, uri string) ([]byte, error) {
		data, err := defaultReadAsset(fsys, ctx, uri)
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

func (g *Group) startSpec(gen uint64, loadCtx context.Context, readFn func(context.Context, string) ([]byte, error), spec assetSpec) {
	switch spec.kind {
	case kindAtlas:
		g.startAtlas(gen, loadCtx, readFn, spec)
	case kindSound:
		g.startSound(gen, spec)
	case kindImage:
		g.startImage(gen, spec)
	case kindBytes:
		g.startBytes(gen, loadCtx, readFn, spec)
	case kindCustom:
		g.startCustom(gen, loadCtx, readFn, spec)
	}
}

func (g *Group) startAtlas(gen uint64, loadCtx context.Context, readFn func(context.Context, string) ([]byte, error), spec assetSpec) {
	prefetch := needsPrefetch(spec.img)
	var (
		parsed   *sprite.Atlas
		parsedOK bool
		imgOK    = !prefetch
		failed   bool
	)
	finish := func(err error) {
		if failed || !g.isActive(gen) {
			return
		}
		if err != nil {
			failed = true
			g.failLoad(gen, err)
			return
		}
		if parsedOK && imgOK {
			g.atlases[spec.name] = parsed
			g.completeItem(gen)
		}
	}

	if prefetch {
		g.m.cx.Run(func() {
			g.m.cx.PrefetchImage(spec.img, func(ok bool) {
				if !ok {
					finish(fmt.Errorf("assets: atlas %q: failed to prefetch image %q", spec.name, spec.img.URI))
					return
				}
				imgOK = true
				finish(nil)
			})
		})
	}

	go func() {
		data, err := readFn(loadCtx, spec.uri)
		if err != nil {
			g.m.postWithCleanup(nil, func() {
				finish(fmt.Errorf("assets: atlas %q: read %q: %w", spec.name, spec.uri, err))
			})
			return
		}
		atlas, err := sprite.LoadAtlas(spec.img, data)
		if err != nil {
			g.m.postWithCleanup(nil, func() {
				finish(loaderr.WithFile(err, spec.uri))
			})
			return
		}
		g.m.postWithCleanup(nil, func() {
			parsed = atlas
			parsedOK = true
			finish(nil)
		})
	}()
}

func (g *Group) startSound(gen uint64, spec assetSpec) {
	g.m.cx.Run(func() {
		audio.LoadSound(g.m.cx, spec.sound, func(s *audio.Sound, err error) {
			if !g.isActive(gen) {
				if s != nil {
					s.Close()
				}
				return
			}
			if err != nil {
				if s != nil {
					s.Close()
				}
				g.failLoad(gen, fmt.Errorf("assets: sound %q: %w", spec.name, err))
				return
			}
			g.sounds[spec.name] = s
			g.completeItem(gen)
		})
	})
}

func (g *Group) startImage(gen uint64, spec assetSpec) {
	if needsPrefetch(spec.img) {
		g.m.cx.Run(func() {
			g.m.cx.PrefetchImage(spec.img, func(ok bool) {
				if !g.isActive(gen) {
					return
				}
				if !ok {
					g.failLoad(gen, fmt.Errorf("assets: image %q (%s): failed to prefetch", spec.name, spec.img.URI))
					return
				}
				g.images[spec.name] = spec.img
				g.completeItem(gen)
			})
		})
		return
	}
	g.m.postWithCleanup(nil, func() {
		if !g.isActive(gen) {
			return
		}
		g.images[spec.name] = spec.img
		g.completeItem(gen)
	})
}

func (g *Group) startBytes(gen uint64, loadCtx context.Context, readFn func(context.Context, string) ([]byte, error), spec assetSpec) {
	go func() {
		data, err := readFn(loadCtx, spec.uri)
		if err != nil {
			g.m.postWithCleanup(nil, func() {
				if !g.isActive(gen) {
					return
				}
				g.failLoad(gen, fmt.Errorf("assets: read %q: %w", spec.uri, err))
			})
			return
		}
		g.m.postWithCleanup(nil, func() {
			if !g.isActive(gen) {
				return
			}
			g.bytes[spec.name] = data
			g.completeItem(gen)
		})
	}()
}

func (g *Group) startCustom(gen uint64, loadCtx context.Context, readFn func(context.Context, string) ([]byte, error), spec assetSpec) {
	go func() {
		data, err := readFn(loadCtx, spec.uri)
		if err != nil {
			g.m.postWithCleanup(nil, func() {
				if !g.isActive(gen) {
					return
				}
				g.failLoad(gen, fmt.Errorf("assets: custom %q: read %q: %w", spec.name, spec.uri, err))
			})
			return
		}
		var val any = data
		if spec.loader != nil {
			val, err = spec.loader(loadCtx, data)
			if err != nil {
				g.m.postWithCleanup(nil, func() {
					if !g.isActive(gen) {
						return
					}
					g.failLoad(gen, fmt.Errorf("assets: custom %q: %w", spec.name, err))
				})
				return
			}
		}
		var release func()
		if spec.release != nil && val != nil {
			v := val
			release = func() { spec.release(v) }
		}
		g.m.postWithCleanup(release, func() {
			if !g.isActive(gen) {
				if release != nil {
					release()
				}
				return
			}
			g.custom[spec.name] = customEntry{value: val, release: spec.release}
			g.completeItem(gen)
		})
	}()
}

func (g *Group) isActive(gen uint64) bool {
	return !g.m.isDisposed() && g.state == stateLoading && g.gen == gen
}

func (g *Group) completeItem(gen uint64) {
	if !g.isActive(gen) {
		return
	}
	total := g.totalSig.GetUntracked()
	g.loaded++
	if g.loaded >= total {
		g.state = stateLoaded
		if g.loadCancel != nil {
			g.loadCancel()
			g.loadCancel = nil
		}
		waiters := g.waiters
		g.waiters = nil
		g.m.cx.Batch(func() {
			g.loadedSig.Set(total)
			g.progressSig.Set(1)
			g.loadingSig.Set(false)
			g.doneSig.Set(true)
			g.errSig.Set(nil)
		})
		for _, fn := range waiters {
			if g.m.isDisposed() {
				break
			}
			fn(nil)
		}
		return
	}
	progress := float64(g.loaded) / float64(total)
	g.m.cx.Batch(func() {
		g.loadedSig.Set(g.loaded)
		g.progressSig.Set(progress)
	})
}

func (g *Group) failLoad(gen uint64, err error) {
	if !g.isActive(gen) {
		return
	}
	g.gen++
	if g.loadCancel != nil {
		g.loadCancel()
		g.loadCancel = nil
	}
	waiters := g.waiters
	g.waiters = nil
	g.releaseLoadedValues()
	g.state = stateFailed
	g.loaded = 0
	g.m.cx.Batch(func() {
		g.loadedSig.Set(0)
		g.loadingSig.Set(false)
		g.doneSig.Set(false)
		g.errSig.Set(err)
	})
	for _, fn := range waiters {
		if g.m.isDisposed() {
			break
		}
		fn(err)
	}
}

// Reset drops the group's asset specs and loaded resources, cancels any in-flight load, and leaves the group idle so a new asset set (such as a freshly mounted DLC pack) can be registered into it. It also unmounts the group's file system, releasing a pack DLCManager.FetchGroup mounted. Concurrent mounts of the same group must be serialized by the caller.
func (g *Group) Reset() {
	if g == nil {
		return
	}
	g.releaseResources(!g.m.isDisposed())
	g.specs = nil
	g.index = make(map[string]int)
	g.releasePack()
	g.fs = nil
	if !g.m.isDisposed() {
		g.m.cx.Batch(func() { g.totalSig.Set(0) })
	}
}

// Release closes every loaded *audio.Sound in the group (ignoring already-closed sounds), invokes custom release hooks, drops all atlas, image and byte references for GC, cancels any in-flight load, and resets progress to 0 so the group can be loaded again later. It keeps the group's specs and mounted file system, so a DLC pack stays mounted for the next Load.
//
// Keeping decoded atlas images pinned outside NSGo's 64-source canvas cache
// waits on nsgo-dev/nsgo#1127.
func (g *Group) Release() {
	g.releaseResources(!g.m.isDisposed())
}

func (g *Group) releaseResources(updateSignals bool) {
	g.gen++
	if g.loadCancel != nil {
		g.loadCancel()
		g.loadCancel = nil
	}
	g.waiters = nil
	g.releaseLoadedValues()
	g.state = stateIdle
	g.loaded = 0
	if updateSignals {
		g.m.cx.Batch(func() {
			g.loadedSig.Set(0)
			g.totalSig.Set(len(g.specs))
			g.progressSig.Set(0)
			g.loadingSig.Set(false)
			g.doneSig.Set(false)
			g.errSig.Set(nil)
		})
	}
}

func (g *Group) releaseLoadedValues() {
	for _, s := range g.sounds {
		if s != nil {
			s.Close()
		}
	}
	for _, c := range g.custom {
		if c.release != nil && c.value != nil {
			c.release(c.value)
		}
	}
	g.atlases = nil
	g.sounds = nil
	g.images = nil
	g.bytes = nil
	g.custom = nil
}
