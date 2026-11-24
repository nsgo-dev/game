// Package assets loads a game's atlases, sounds, images and custom data
// asynchronously without blocking display frames, organizes them into named
// per-scene groups with reactive progress, releases them when a scene ends,
// and provides a loading screen component.
//
// Asset files are read and parsed off the engine goroutine and delivered back
// through core.Scope.Post so animations and game loops keep ticking while
// assets load. Manager and Group methods are called on the engine's goroutine,
// as core and audio services are.
//
// Keeping decoded atlas images pinned outside NSGo's 64-source canvas cache
// waits on nsgo-dev/nsgo#1127; today, image assets are warmed through
// core.Scope.PrefetchImage and dropped for GC when their group is released.
package assets

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// CustomLoader parses raw asset bytes off the engine goroutine into a domain
// value such as a tilemap, bitmap font or JSON table.
type CustomLoader func(ctx context.Context, data []byte) (any, error)

// Option configures a Manager created by NewManager.
type Option func(*Manager)

// WithReadAsset sets how Manager reads raw bytes for asset URIs off the engine
// goroutine.
func WithReadAsset(fn func(ctx context.Context, uri string) ([]byte, error)) Option {
	return func(m *Manager) { m.ReadAsset = fn }
}

// WithFS sets the file system that the default ReadAsset reads "asset:" URIs
// from, such as an embed.FS or os.DirFS.
func WithFS(fsys fs.FS) Option {
	return func(m *Manager) { m.fs = fsys }
}

// Manager loads game assets asynchronously under a scope, groups them by scene
// with reactive progress, and releases them when a group or the scope ends.
//
// ReadAsset may be replaced before loading starts; when nil, Manager reads
// "asset:" URIs from the FS given to WithFS, or from the local "assets"
// directory on disk.
type Manager struct {
	// ReadAsset reads raw bytes for uri off the engine goroutine. When nil,
	// Manager falls back to WithFS or local files under "assets/".
	ReadAsset func(ctx context.Context, uri string) ([]byte, error)

	cx     *core.Scope
	fs     fs.FS
	ctx    context.Context
	cancel context.CancelFunc

	groups     map[string]*Group
	groupOrder []string

	// revision counts the Reload calls that replaced an asset; Revision reads it.
	revision *core.Signal[int]

	// mu protects disposed and pending across worker goroutines and the engine
	// goroutine so late completions never leak resources if cx is disposed
	// between a worker finishing and core.Scope.Post running.
	mu       sync.Mutex
	disposed bool
	pending  map[*pendingResource]struct{}
}

type pendingResource struct {
	close func()
}

// NewManager returns an asset manager owned by cx. When cx is disposed, any
// in-flight loads are cancelled, late completions release their resources
// instead of calling callbacks, and every loaded group is released.
func NewManager(cx *core.Scope, opts ...Option) *Manager {
	ctx, cancel := context.WithCancel(cx.Context())
	m := &Manager{
		cx:      cx,
		ctx:     ctx,
		cancel:  cancel,
		groups:  map[string]*Group{},
		pending: map[*pendingResource]struct{}{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	cx.Run(func() {
		m.revision = core.NewSignal(cx, 0)
		cx.Cleanup(m.dispose)
	})
	return m
}

// Group returns the named asset group, creating it under the manager's scope
// if it does not yet exist.
func (m *Manager) Group(name string) *Group {
	if g, ok := m.groups[name]; ok {
		return g
	}
	g := newGroup(m, name)
	m.groups[name] = g
	m.groupOrder = append(m.groupOrder, name)
	return g
}

// LoadGroup starts loading the named group asynchronously and calls onDone on
// the engine goroutine when loading finishes. Calling LoadGroup on a group that
// is already loaded calls onDone(nil) immediately; calling it while the group
// is loading attaches onDone to the in-flight load. An empty group completes
// immediately with progress 1.
func (m *Manager) LoadGroup(name string, onDone func(error)) {
	m.Group(name).Load(onDone)
}

// ReleaseGroup releases every loaded asset in the named group: closes every
// loaded *audio.Sound, invokes custom release hooks, drops atlas, image and
// byte references for GC, cancels any in-flight load for the group, and resets
// progress to 0 so the group can be loaded again later.
//
// Keeping decoded atlas images pinned outside NSGo's 64-source canvas cache
// waits on nsgo-dev/nsgo#1127.
func (m *Manager) ReleaseGroup(name string) {
	if g, ok := m.groups[name]; ok {
		g.Release()
	}
}

// ReleaseAll releases every group registered on the manager.
func (m *Manager) ReleaseAll() {
	for _, name := range m.groupOrder {
		if g, ok := m.groups[name]; ok {
			g.Release()
		}
	}
}

// Atlas returns the loaded atlas named name from the most recently registered
// group that holds it.
func (m *Manager) Atlas(name string) (*sprite.Atlas, bool) {
	for i := len(m.groupOrder) - 1; i >= 0; i-- {
		if g, ok := m.groups[m.groupOrder[i]]; ok {
			if a, found := g.Atlas(name); found {
				return a, true
			}
		}
	}
	return nil, false
}

// Sound returns the loaded sound named name from the most recently registered
// group that holds it.
func (m *Manager) Sound(name string) (*audio.Sound, bool) {
	for i := len(m.groupOrder) - 1; i >= 0; i-- {
		if g, ok := m.groups[m.groupOrder[i]]; ok {
			if s, found := g.Sound(name); found {
				return s, true
			}
		}
	}
	return nil, false
}

// Image returns the loaded image source named name from the most recently
// registered group that holds it.
func (m *Manager) Image(name string) (core.ImageSource, bool) {
	for i := len(m.groupOrder) - 1; i >= 0; i-- {
		if g, ok := m.groups[m.groupOrder[i]]; ok {
			if img, found := g.Image(name); found {
				return img, true
			}
		}
	}
	return core.ImageSource{}, false
}

// Bytes returns the loaded raw bytes named name from the most recently
// registered group that holds it.
func (m *Manager) Bytes(name string) ([]byte, bool) {
	for i := len(m.groupOrder) - 1; i >= 0; i-- {
		if g, ok := m.groups[m.groupOrder[i]]; ok {
			if b, found := g.Bytes(name); found {
				return b, true
			}
		}
	}
	return nil, false
}

// Get returns the loaded asset named name (custom, atlas, sound, image or raw
// bytes) from the most recently registered group that holds it.
func (m *Manager) Get(name string) (any, bool) {
	for i := len(m.groupOrder) - 1; i >= 0; i-- {
		if g, ok := m.groups[m.groupOrder[i]]; ok {
			if v, found := g.Get(name); found {
				return v, true
			}
		}
	}
	return nil, false
}

// LoadBytes reads uri off the engine goroutine and calls done on the engine
// goroutine with its bytes or error. Nothing runs if the manager's scope is
// disposed before completion.
func (m *Manager) LoadBytes(uri string, done func([]byte, error)) {
	if m.isDisposed() {
		return
	}
	readFn := m.readAssetFunc()
	ctx := m.ctx
	go func() {
		data, err := readFn(ctx, uri)
		if err != nil {
			err = fmt.Errorf("assets: read %q: %w", uri, err)
		}
		m.postWithCleanup(nil, func() {
			if done != nil {
				done(data, err)
			}
		})
	}()
}

// LoadAtlas reads jsonURI and parses it with sprite.LoadAtlas off the engine
// goroutine, and prefetches img through core.Scope.PrefetchImage when img names
// a URI without inline Data or Pixels (in nsgotest, register the URI in
// App.ImageSizes). It calls done on the engine goroutine once ready.
func (m *Manager) LoadAtlas(jsonURI string, img core.ImageSource, done func(*sprite.Atlas, error)) {
	if m.isDisposed() {
		return
	}
	readFn := m.readAssetFunc()
	ctx := m.ctx
	prefetch := needsPrefetch(img)

	var (
		parsed   *sprite.Atlas
		parsedOK bool
		imgOK    = !prefetch
		finished bool
	)
	deliver := func(err error) {
		if finished || m.isDisposed() {
			return
		}
		if err != nil {
			finished = true
			if done != nil {
				done(nil, err)
			}
			return
		}
		if parsedOK && imgOK {
			finished = true
			if done != nil {
				done(parsed, nil)
			}
		}
	}

	if prefetch {
		m.cx.Run(func() {
			m.cx.PrefetchImage(img, func(ok bool) {
				if !ok {
					deliver(fmt.Errorf("assets: atlas %q: failed to prefetch image %q", jsonURI, img.URI))
					return
				}
				imgOK = true
				deliver(nil)
			})
		})
	}

	go func() {
		data, err := readFn(ctx, jsonURI)
		if err != nil {
			m.postWithCleanup(nil, func() {
				deliver(fmt.Errorf("assets: read %q: %w", jsonURI, err))
			})
			return
		}
		atlas, err := sprite.LoadAtlas(img, data)
		if err != nil {
			m.postWithCleanup(nil, func() {
				deliver(loaderr.WithFile(err, jsonURI))
			})
			return
		}
		m.postWithCleanup(nil, func() {
			parsed = atlas
			parsedOK = true
			deliver(nil)
		})
	}()
}

// LoadSound loads src through audio.LoadSound under the manager's scope and
// calls done on the engine goroutine. If the scope is disposed before loading
// completes, any loaded sound is closed and done is not called.
func (m *Manager) LoadSound(src audio.Source, done func(*audio.Sound, error)) {
	if m.isDisposed() {
		return
	}
	m.cx.Run(func() {
		audio.LoadSound(m.cx, src, func(s *audio.Sound, err error) {
			if m.isDisposed() {
				if s != nil {
					s.Close()
				}
				return
			}
			if done != nil {
				done(s, err)
			} else if s != nil {
				s.Close()
			}
		})
	})
}

// LoadCustom reads uri and parses it with load off the engine goroutine, then
// calls done on the engine goroutine. If the manager's scope is disposed before
// done runs (or done is nil), closeFn is called on the loaded value so nothing
// leaks.
func (m *Manager) LoadCustom(uri string, load CustomLoader, closeFn func(any), done func(any, error)) {
	if m.isDisposed() {
		return
	}
	readFn := m.readAssetFunc()
	ctx := m.ctx
	go func() {
		data, err := readFn(ctx, uri)
		if err != nil {
			m.postWithCleanup(nil, func() {
				if done != nil {
					done(nil, fmt.Errorf("assets: read %q: %w", uri, err))
				}
			})
			return
		}
		var val any = data
		if load != nil {
			val, err = load(ctx, data)
			if err != nil {
				m.postWithCleanup(nil, func() {
					if done != nil {
						done(nil, fmt.Errorf("assets: custom %q: %w", uri, err))
					}
				})
				return
			}
		}
		var release func()
		if closeFn != nil && val != nil {
			v := val
			release = func() { closeFn(v) }
		}
		m.postWithCleanup(release, func() {
			if done != nil {
				done(val, nil)
				return
			}
			if release != nil {
				release()
			}
		})
	}()
}

func (m *Manager) dispose() {
	m.mu.Lock()
	if m.disposed {
		m.mu.Unlock()
		return
	}
	m.disposed = true
	pending := m.pending
	m.pending = nil
	m.mu.Unlock()

	m.cancel()
	for _, name := range m.groupOrder {
		if g, ok := m.groups[name]; ok {
			g.releaseResources(false)
			g.releasePack()
		}
	}
	for p := range pending {
		if p.close != nil {
			p.close()
		}
	}
}

func (m *Manager) isDisposed() bool {
	m.mu.Lock()
	d := m.disposed
	m.mu.Unlock()
	return d
}

// postWithCleanup delivers deliver on the engine goroutine under m.cx. If m is
// disposed before deliver runs, release is called once instead of deliver.
func (m *Manager) postWithCleanup(release func(), deliver func()) {
	var pr *pendingResource
	m.mu.Lock()
	if m.disposed {
		m.mu.Unlock()
		if release != nil {
			release()
		}
		return
	}
	if release != nil {
		pr = &pendingResource{close: release}
		m.pending[pr] = struct{}{}
	}
	m.mu.Unlock()

	m.cx.Post(func() {
		m.mu.Lock()
		if pr != nil {
			if _, ok := m.pending[pr]; !ok {
				m.mu.Unlock()
				return
			}
			delete(m.pending, pr)
		}
		disposed := m.disposed
		m.mu.Unlock()

		if disposed {
			if release != nil {
				release()
			}
			return
		}
		deliver()
	})
}

func (m *Manager) readAssetFunc() func(context.Context, string) ([]byte, error) {
	if m.ReadAsset != nil {
		return m.ReadAsset
	}
	fsys := m.fs
	return func(ctx context.Context, uri string) ([]byte, error) {
		return defaultReadAsset(fsys, ctx, uri)
	}
}

func defaultReadAsset(fsys fs.FS, ctx context.Context, uri string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rel, ok := strings.CutPrefix(uri, "asset:"); ok {
		rel = strings.TrimPrefix(rel, "/")
		if fsys != nil {
			return fs.ReadFile(fsys, rel)
		}
		// The default store is rooted at assets/: refuse anything that escapes
		// it, such as "asset:../secret" or an absolute path.
		if !fs.ValidPath(rel) {
			return nil, fmt.Errorf("assets: asset path %q is not inside the assets directory", uri)
		}
		return fs.ReadFile(os.DirFS("assets"), rel)
	}
	if p, ok := strings.CutPrefix(uri, "file://"); ok {
		// A mounted pack may only read its own files; file:// is about the host.
		if fsys != nil {
			return nil, fmt.Errorf("assets: file:// URI %q is not allowed from a mounted pack", uri)
		}
		return os.ReadFile(filepath.FromSlash(p))
	}
	if fsys != nil {
		return fs.ReadFile(fsys, strings.TrimPrefix(uri, "/"))
	}
	return os.ReadFile(uri)
}

func needsPrefetch(img core.ImageSource) bool {
	return img.URI != "" && len(img.Data) == 0 && img.Pixels == nil
}
