//go:build !release

package hotreload

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Watcher polls a directory off the engine goroutine and delivers asset reloads back onto the engine goroutine via core.Scope.Post.
//
// It bumps Version on every reload so a core.Canvas or core.Effect reading Version.Get or Generation redraws automatically, and stops itself when the scope that owns it is disposed.
type Watcher struct {
	// Version increments on every delivered reload. Read it (or call Generation) inside a core.Canvas Draw callback or core.Effect to react automatically.
	Version *core.Signal[int]

	cx  *core.Scope
	dir string
	cfg config

	mu       sync.Mutex
	watches  []*fileWatch
	patterns []*patternWatch

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopped  atomic.Bool
}

type fileFingerprint struct {
	exists  bool
	modTime time.Time
	size    int64
	hash    [sha256.Size]byte
	// hashedAt is when hash was computed from the file's contents.
	hashedAt time.Time
}

// racyWindow is how close to hashedAt a modification time may be for the fingerprint to vouch for the contents by modification time and size alone. File systems keep modification times at a granularity (a kernel tick on Linux, two seconds on FAT), so a write of the same length within that granularity after hashing leaves both unchanged; as git does for racily clean index entries, such a fingerprint compares contents again.
const racyWindow = 2 * time.Second

// statUnchanged reports whether info shows the fingerprinted contents for certain: the same modification time and size, with the modification time far enough before hashedAt that a later write would have moved it.
func (fp fileFingerprint) statUnchanged(modTime time.Time, size int64) bool {
	return fp.exists && modTime.Equal(fp.modTime) && size == fp.size && fp.hashedAt.Sub(modTime) >= racyWindow
}

type fileEntry struct {
	fullPath  string
	relPath   string
	delivered fileFingerprint

	hasPending bool
	pending    fileFingerprint
	pendingAt  time.Time
}

type fileWatch struct {
	entry    *fileEntry
	onChange func(data []byte)
}

type patternWatch struct {
	ext      string
	onChange func(relPath string, data []byte)
	files    map[string]*fileEntry
}

// NewWatcher starts a Watcher polling dir under cx until cx is disposed or Stop is called.
func NewWatcher(cx *core.Scope, dir string, opts ...Option) *Watcher {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	w := &Watcher{
		Version: core.NewSignal(cx, 0),
		cx:      cx,
		dir:     filepath.Clean(dir),
		cfg:     cfg,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	cx.Cleanup(w.Stop)
	go w.loop()
	return w
}

// Generation returns the current reload generation counter. Reading it inside a reactive computation or core.Canvas Draw callback subscribes to future reloads.
func (w *Watcher) Generation() int {
	if w == nil || w.Version == nil {
		return 0
	}
	return w.Version.Get()
}

// Dir returns the cleaned root directory being watched.
func (w *Watcher) Dir() string {
	if w == nil {
		return ""
	}
	return w.dir
}

// Stop halts background polling. It is safe to call more than once and is registered automatically on the watcher's scope cleanup.
func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		w.stopped.Store(true)
		close(w.stopCh)
	})
}

// Watch watches relPath under the watcher's directory and calls onChange on the engine goroutine whenever the file's content changes and stabilizes.
func (w *Watcher) Watch(relPath string, onChange func(data []byte)) {
	if w == nil || onChange == nil || w.stopped.Load() {
		return
	}
	cleanRel := filepath.Clean(relPath)
	full := filepath.Join(w.dir, cleanRel)
	entry := &fileEntry{
		fullPath:  full,
		relPath:   filepath.ToSlash(cleanRel),
		delivered: snapshotFile(full),
	}
	fw := &fileWatch{entry: entry, onChange: onChange}
	w.mu.Lock()
	w.watches = append(w.watches, fw)
	w.mu.Unlock()
}

// WatchAtlas watches a sprite atlas JSON file at jsonPath, parses it with sprite.LoadAtlas using img, and delivers the result to onReload on the engine goroutine.
func (w *Watcher) WatchAtlas(jsonPath string, img core.ImageSource, onReload func(*sprite.Atlas, error)) {
	if w == nil || onReload == nil || w.stopped.Load() {
		return
	}
	w.Watch(jsonPath, func(data []byte) {
		atlas, err := sprite.LoadAtlas(img, data)
		onReload(atlas, err)
	})
}

// WatchResources reloads lib's resources when their files change: it watches every .yaml and .yml file under the watcher's directory and passes a changed file that lib has loaded to resource.Library.Reload on the engine goroutine, so everything holding the resource sees the edit. The watcher's directory must be lib's root, so the paths match. onReload, if not nil, is then called with the file's slash-separated path and Reload's error, nil on success; files lib has not loaded are skipped.
func (w *Watcher) WatchResources(lib *resource.Library, onReload func(path string, err error)) {
	if w == nil || lib == nil || w.stopped.Load() {
		return
	}
	reload := func(relPath string, data []byte) {
		loaded, err := lib.Reload(relPath, data)
		if loaded && onReload != nil {
			onReload(relPath, err)
		}
	}
	w.WatchPattern(".yaml", reload)
	w.WatchPattern(".yml", reload)
}

// WatchPattern watches every file under the watcher's directory whose extension matches ext (for example ".json" or "json", or "" / "*" for all files) and calls onChange on the engine goroutine with the slash-separated relative path and file bytes when a matching file is created or modified.
func (w *Watcher) WatchPattern(ext string, onChange func(relPath string, data []byte)) {
	if w == nil || onChange == nil || w.stopped.Load() {
		return
	}
	normExt := normalizeExt(ext)
	files := make(map[string]*fileEntry)
	_ = filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != w.dir && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !matchesExt(d.Name(), normExt) {
			return nil
		}
		rel, err := filepath.Rel(w.dir, path)
		if err != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		files[slashRel] = &fileEntry{
			fullPath:  path,
			relPath:   slashRel,
			delivered: snapshotFile(path),
		}
		return nil
	})
	pw := &patternWatch{
		ext:      normExt,
		onChange: onChange,
		files:    files,
	}
	w.mu.Lock()
	w.patterns = append(w.patterns, pw)
	w.mu.Unlock()
}

func (w *Watcher) loop() {
	defer close(w.doneCh)
	timer := time.NewTimer(w.cfg.pollInterval)
	defer timer.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-timer.C:
			if w.stopped.Load() {
				return
			}
			nextDelay := w.poll(time.Now())
			if nextDelay <= 0 {
				nextDelay = time.Millisecond
			}
			timer.Reset(nextDelay)
		}
	}
}

func (w *Watcher) poll(now time.Time) time.Duration {
	w.mu.Lock()
	watches := make([]*fileWatch, len(w.watches))
	copy(watches, w.watches)
	patterns := make([]*patternWatch, len(w.patterns))
	copy(patterns, w.patterns)
	w.mu.Unlock()

	nextDelay := w.cfg.pollInterval
	considerPending := func(e *fileEntry) {
		if !e.hasPending {
			return
		}
		rem := w.cfg.debounce - now.Sub(e.pendingAt)
		if rem < time.Millisecond {
			rem = time.Millisecond
		}
		if rem < nextDelay {
			nextDelay = rem
		}
	}

	for _, fw := range watches {
		if w.stopped.Load() {
			return nextDelay
		}
		info, err := os.Stat(fw.entry.fullPath)
		if err != nil {
			fw.entry.hasPending = false
			continue
		}
		if data, ok := w.checkEntry(fw.entry, info, now); ok {
			cb := fw.onChange
			w.deliver(func() { cb(data) })
		}
		considerPending(fw.entry)
	}

	for _, pw := range patterns {
		if w.stopped.Load() {
			return nextDelay
		}
		seen := make(map[string]struct{}, len(pw.files))
		_ = filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || w.stopped.Load() {
				return nil
			}
			if d.IsDir() {
				if path != w.dir && d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !matchesExt(d.Name(), pw.ext) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(w.dir, path)
			if err != nil {
				return nil
			}
			slashRel := filepath.ToSlash(rel)
			seen[slashRel] = struct{}{}
			entry, ok := pw.files[slashRel]
			if !ok {
				entry = &fileEntry{
					fullPath: path,
					relPath:  slashRel,
				}
				pw.files[slashRel] = entry
			}
			if data, changed := w.checkEntry(entry, info, now); changed {
				cb := pw.onChange
				relCopy := slashRel
				w.deliver(func() { cb(relCopy, data) })
			}
			considerPending(entry)
			return nil
		})
		for rel, entry := range pw.files {
			if _, ok := seen[rel]; !ok {
				entry.delivered = fileFingerprint{}
				entry.hasPending = false
			}
		}
	}

	return nextDelay
}

// checkEntry advances entry's stat/hash/debounce state machine and returns the stable file contents when a change is ready to deliver.
func (w *Watcher) checkEntry(entry *fileEntry, info fs.FileInfo, now time.Time) ([]byte, bool) {
	modTime := info.ModTime()
	size := info.Size()

	if !entry.hasPending {
		if entry.delivered.statUnchanged(modTime, size) {
			return nil, false
		}
		data, err := os.ReadFile(entry.fullPath)
		if err != nil || int64(len(data)) != size {
			return nil, false
		}
		sum := sha256.Sum256(data)
		fp := fileFingerprint{
			exists:   true,
			modTime:  modTime,
			size:     size,
			hash:     sum,
			hashedAt: now,
		}
		if entry.delivered.exists && sum == entry.delivered.hash {
			entry.delivered = fp
			return nil, false
		}
		if w.cfg.debounce <= 0 {
			entry.delivered = fp
			return data, true
		}
		entry.hasPending = true
		entry.pending = fp
		entry.pendingAt = now
		return nil, false
	}

	// A pending change is waiting for debounce stability.
	if !modTime.Equal(entry.pending.modTime) || size != entry.pending.size {
		data, err := os.ReadFile(entry.fullPath)
		if err != nil || int64(len(data)) != size {
			return nil, false
		}
		sum := sha256.Sum256(data)
		fp := fileFingerprint{
			exists:   true,
			modTime:  modTime,
			size:     size,
			hash:     sum,
			hashedAt: now,
		}
		if entry.delivered.exists && sum == entry.delivered.hash {
			entry.delivered = fp
			entry.hasPending = false
			return nil, false
		}
		entry.pending = fp
		entry.pendingAt = now
		return nil, false
	}

	if now.Sub(entry.pendingAt) < w.cfg.debounce {
		return nil, false
	}

	// Re-read once more after debounce elapses to confirm the file content itself is stable.
	data, err := os.ReadFile(entry.fullPath)
	if err != nil || int64(len(data)) != size {
		return nil, false
	}
	sum := sha256.Sum256(data)
	if sum != entry.pending.hash {
		entry.pending = fileFingerprint{
			exists:   true,
			modTime:  modTime,
			size:     size,
			hash:     sum,
			hashedAt: now,
		}
		entry.pendingAt = now
		return nil, false
	}

	entry.delivered = entry.pending
	entry.hasPending = false
	return data, true
}

func (w *Watcher) deliver(fn func()) {
	if w.stopped.Load() {
		return
	}
	w.cx.Post(func() {
		if w.stopped.Load() || w.cx.Disposed() {
			return
		}
		fn()
		w.Version.Set(w.Version.Get() + 1)
	})
}

func snapshotFile(path string) fileFingerprint {
	info, err := os.Stat(path)
	if err != nil {
		return fileFingerprint{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileFingerprint{}
	}
	return fileFingerprint{
		exists:   true,
		modTime:  info.ModTime(),
		size:     info.Size(),
		hash:     sha256.Sum256(data),
		hashedAt: time.Now(),
	}
}

func normalizeExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" || ext == "*" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return strings.ToLower(ext)
}

func matchesExt(name, normExt string) bool {
	if normExt == "" {
		return true
	}
	return strings.EqualFold(filepath.Ext(name), normExt)
}
