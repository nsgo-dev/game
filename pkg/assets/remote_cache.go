package assets

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// DefaultRemoteCacheQuota is the default byte budget (200 MiB) for
// DirRemoteCache and MemoryRemoteCache, matching the local file system cap of
// mini-game runtimes such as WeChat.
const DefaultRemoteCacheQuota int64 = 200 << 20

const (
	remoteCacheIndexVersion = 1
	remoteCacheIndexName    = "index.yaml"
	remoteCacheFilesDir     = "files"
)

// RemoteCacheUsage summarizes the footprint of a RemoteCache, suitable for
// displaying in a game's settings or storage management screen.
type RemoteCacheUsage struct {
	// Bytes is the total size in bytes of all cached files.
	Bytes int64
	// Quota is the configured maximum cache size in bytes.
	Quota int64
	// Files is the number of cached files.
	Files int
	// PinnedBytes is the total size in bytes of cached files currently pinned by loaded groups.
	PinnedBytes int64
}

// RemoteCache stores verified remote asset files keyed by their lowercase hex
// SHA-256 digest and enforces a byte quota with least-recently-used eviction.
// Implementations must be safe for concurrent use.
type RemoteCache interface {
	// Get returns the cached bytes for sha256Hex and updates its last-access
	// time, or returns fs.ErrNotExist if not cached.
	Get(sha256Hex string) ([]byte, error)
	// Put stores verified bytes for sha256Hex, updates its last-access time,
	// and evicts least-recently-used unpinned entries to stay within Quota.
	Put(sha256Hex string, data []byte) error
	// Remove deletes the cached entry for sha256Hex, returning nil if absent.
	Remove(sha256Hex string) error
	// Pin marks sha256Hex as in use by a loaded or loading group so eviction
	// spares it. Calls are reference-counted and paired with Unpin.
	Pin(sha256Hex string)
	// Unpin releases one Pin reference on sha256Hex.
	Unpin(sha256Hex string)
	// Usage returns the cache's current byte usage, quota, file count, and
	// pinned byte count.
	Usage() RemoteCacheUsage
}

type remoteCacheIndex struct {
	Version int                     `yaml:"version"`
	Entries []remoteCacheIndexEntry `yaml:"entries,omitempty"`
}

type remoteCacheIndexEntry struct {
	SHA256   string `yaml:"sha256"`
	Size     int64  `yaml:"size"`
	Accessed int64  `yaml:"accessed"`
}

func parseRemoteCacheIndex(data []byte) (*remoteCacheIndex, error) {
	const fix = "delete index.yaml so DirRemoteCache rebuilds it from disk"
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fix, "assets: remote cache index: empty document"), "", data, "")
	}
	var idx remoteCacheIndex
	if err := decodeStrictYAML(data, &idx); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("assets: remote cache index: %w", err), "", data, fix)
	}
	if idx.Version != remoteCacheIndexVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("set version: %d or delete index.yaml to rebuild it", remoteCacheIndexVersion), "assets: remote cache index: unsupported version %d (want %d)", idx.Version, remoteCacheIndexVersion), "", data, "")
	}
	seen := make(map[string]bool, len(idx.Entries))
	for i, e := range idx.Entries {
		fieldPrefix := fmt.Sprintf("entries[%d]", i)
		if err := validateLowerSHA256(e.SHA256); err != nil {
			return nil, loaderr.Locate(loaderr.Errorf(fieldPrefix+".sha256", fix, "assets: remote cache index: %s: sha256: %w", fieldPrefix, err), "", data, "")
		}
		if seen[e.SHA256] {
			return nil, loaderr.Locate(loaderr.Errorf(fieldPrefix+".sha256", fix, "assets: remote cache index: %s: duplicate sha256 %s", fieldPrefix, e.SHA256), "", data, "")
		}
		seen[e.SHA256] = true
		if e.Size < 0 {
			return nil, loaderr.Locate(loaderr.Errorf(fieldPrefix+".size", fix, "assets: remote cache index: %s: negative size %d", fieldPrefix, e.Size), "", data, "")
		}
		if e.Accessed < 0 {
			return nil, loaderr.Locate(loaderr.Errorf(fieldPrefix+".accessed", fix, "assets: remote cache index: %s: negative accessed %d", fieldPrefix, e.Accessed), "", data, "")
		}
	}
	return &idx, nil
}

func encodeRemoteCacheIndex(idx *remoteCacheIndex) ([]byte, error) {
	if idx == nil {
		return nil, errors.New("assets: remote cache index is nil")
	}
	out := remoteCacheIndex{
		Version: idx.Version,
	}
	if len(idx.Entries) > 0 {
		out.Entries = slices.Clone(idx.Entries)
		slices.SortFunc(out.Entries, func(a, b remoteCacheIndexEntry) int {
			return strings.Compare(a.SHA256, b.SHA256)
		})
	}
	return encodeYAML(&out)
}

// DirRemoteCache stores remote loose asset files on disk under a directory,
// held to Quota (DefaultRemoteCacheQuota, 200 MiB, when <= 0) via
// least-recently-used eviction that never removes a file pinned by a loaded
// group, with access times persisted in a small YAML index (index.yaml).
type DirRemoteCache struct {
	// Quota is the maximum total size of cached files in bytes. When <= 0,
	// DefaultRemoteCacheQuota (200 MiB) is used.
	Quota int64
	// FlushInterval controls how frequently Get access-time updates are written
	// to index.yaml. Zero writes immediately on every access; Put and Remove
	// always persist the index immediately.
	FlushInterval time.Duration

	dir       string
	nowFn     func() time.Time
	mu        sync.Mutex
	loaded    bool
	entries   map[string]*remoteCacheEntry
	pinned    map[string]int
	totalSize int64
	lastTick  int64
	dirty     bool
	lastFlush time.Time
}

type remoteCacheEntry struct {
	size     int64
	accessed int64
}

// NewDirRemoteCache returns a filesystem-backed RemoteCache rooted at dir.
// If quota is given and positive, it sets Quota; otherwise DefaultRemoteCacheQuota
// (200 MiB) is used. Directories are created lazily on first write.
func NewDirRemoteCache(dir string, quota ...int64) *DirRemoteCache {
	q := DefaultRemoteCacheQuota
	if len(quota) > 0 && quota[0] > 0 {
		q = quota[0]
	}
	return &DirRemoteCache{
		Quota:   q,
		dir:     dir,
		entries: map[string]*remoteCacheEntry{},
		pinned:  map[string]int{},
	}
}

// Dir returns the cache root directory.
func (c *DirRemoteCache) Dir() string { return c.dir }

func (c *DirRemoteCache) effectiveQuotaLocked() int64 {
	if c.Quota <= 0 {
		return DefaultRemoteCacheQuota
	}
	return c.Quota
}

func (c *DirRemoteCache) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

func (c *DirRemoteCache) nextTickLocked() int64 {
	t := c.now().UnixNano()
	if t <= c.lastTick {
		t = c.lastTick + 1
	}
	c.lastTick = t
	return t
}

func (c *DirRemoteCache) indexPath() string {
	return filepath.Join(filepath.Clean(c.dir), remoteCacheIndexName)
}

func (c *DirRemoteCache) filesDir() string {
	return filepath.Join(filepath.Clean(c.dir), remoteCacheFilesDir)
}

func (c *DirRemoteCache) blobPath(sha256Hex string) (string, error) {
	if strings.TrimSpace(c.dir) == "" {
		return "", errors.New("assets: remote cache: directory is empty")
	}
	if err := validateLowerSHA256(sha256Hex); err != nil {
		return "", fmt.Errorf("assets: remote cache: invalid key: %w", err)
	}
	filesDir := c.filesDir()
	p := filepath.Join(filesDir, sha256Hex)
	if filepath.Dir(p) != filesDir {
		return "", fmt.Errorf("assets: remote cache: invalid key %q", sha256Hex)
	}
	return p, nil
}

// ensureLoadedLocked initializes the in-memory index from disk once, reconciling
// against the files directory so a missing or corrupted index.yaml recovers
// cleanly and leftover temporary files from interrupted writes are removed.
func (c *DirRemoteCache) ensureLoadedLocked() error {
	if c.loaded {
		return nil
	}
	if strings.TrimSpace(c.dir) == "" {
		return errors.New("assets: remote cache: directory is empty")
	}
	c.entries = map[string]*remoteCacheEntry{}
	c.totalSize = 0
	c.lastTick = 0

	indexed := map[string]remoteCacheIndexEntry{}
	indexCorrupt := false
	if data, err := os.ReadFile(c.indexPath()); err == nil {
		if idx, parseErr := parseRemoteCacheIndex(data); parseErr == nil {
			for _, e := range idx.Entries {
				indexed[e.SHA256] = e
				if e.Accessed > c.lastTick {
					c.lastTick = e.Accessed
				}
			}
		} else {
			indexCorrupt = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	dirEntries, err := os.ReadDir(c.filesDir())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		c.loaded = true
		if indexCorrupt || len(indexed) > 0 {
			c.dirty = true
		}
		return nil
	}

	reconciled := indexCorrupt
	onDisk := make(map[string]bool, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		full := filepath.Join(c.filesDir(), name)
		if strings.HasPrefix(name, ".tmp-") {
			_ = os.Remove(full)
			continue
		}
		if de.IsDir() || validateLowerSHA256(name) != nil {
			continue
		}
		info, statErr := de.Info()
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		onDisk[name] = true
		size := info.Size()
		var accessed int64
		if prev, ok := indexed[name]; ok {
			accessed = prev.Accessed
			if prev.Size != size {
				reconciled = true
			}
		} else {
			accessed = info.ModTime().UnixNano()
			if accessed < 0 {
				accessed = 0
			}
			reconciled = true
		}
		if accessed > c.lastTick {
			c.lastTick = accessed
		}
		c.entries[name] = &remoteCacheEntry{
			size:     size,
			accessed: accessed,
		}
		c.totalSize += size
	}
	if len(onDisk) != len(indexed) {
		reconciled = true
	}
	c.loaded = true
	if reconciled {
		c.dirty = true
		_ = c.flushLocked(true)
	}
	return nil
}

// Has reports whether sha256Hex is currently present in the cache.
func (c *DirRemoteCache) Has(sha256Hex string) bool {
	if validateLowerSHA256(sha256Hex) != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLoadedLocked(); err != nil {
		return false
	}
	_, ok := c.entries[sha256Hex]
	return ok
}

// Get implements RemoteCache.
func (c *DirRemoteCache) Get(sha256Hex string) ([]byte, error) {
	p, err := c.blobPath(sha256Hex)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLoadedLocked(); err != nil {
		return nil, err
	}
	entry, ok := c.entries[sha256Hex]
	if !ok {
		return nil, fs.ErrNotExist
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.totalSize -= entry.size
			delete(c.entries, sha256Hex)
			c.dirty = true
			_ = c.flushLocked(true)
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	entry.accessed = c.nextTickLocked()
	c.dirty = true
	_ = c.flushLocked(false)
	return data, nil
}

// Put implements RemoteCache using atomic temp-file writes and LRU eviction of
// unpinned entries to stay within Quota.
func (c *DirRemoteCache) Put(sha256Hex string, data []byte) error {
	p, err := c.blobPath(sha256Hex)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLoadedLocked(); err != nil {
		return err
	}
	if err := os.MkdirAll(c.filesDir(), 0o755); err != nil {
		return fmt.Errorf("assets: remote cache: mkdir: %w", err)
	}
	if err := writeFileAtomic(p, ".tmp-"+sha256Hex[:8]+"-*", data); err != nil {
		return fmt.Errorf("assets: remote cache: write %s: %w", sha256Hex, err)
	}
	size := int64(len(data))
	if prev, ok := c.entries[sha256Hex]; ok {
		c.totalSize -= prev.size
	}
	c.entries[sha256Hex] = &remoteCacheEntry{
		size:     size,
		accessed: c.nextTickLocked(),
	}
	c.totalSize += size
	c.evictLocked(sha256Hex)
	c.dirty = true
	return c.flushLocked(true)
}

// Remove implements RemoteCache.
func (c *DirRemoteCache) Remove(sha256Hex string) error {
	p, err := c.blobPath(sha256Hex)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLoadedLocked(); err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if entry, ok := c.entries[sha256Hex]; ok {
		c.totalSize -= entry.size
		delete(c.entries, sha256Hex)
		c.dirty = true
		return c.flushLocked(true)
	}
	return nil
}

// Pin implements RemoteCache.
func (c *DirRemoteCache) Pin(sha256Hex string) {
	if validateLowerSHA256(sha256Hex) != nil {
		return
	}
	c.mu.Lock()
	c.pinned[sha256Hex]++
	c.mu.Unlock()
}

// Unpin implements RemoteCache.
func (c *DirRemoteCache) Unpin(sha256Hex string) {
	c.mu.Lock()
	if n := c.pinned[sha256Hex]; n <= 1 {
		delete(c.pinned, sha256Hex)
	} else {
		c.pinned[sha256Hex] = n - 1
	}
	c.mu.Unlock()
}

// Usage implements RemoteCache.
func (c *DirRemoteCache) Usage() RemoteCacheUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ensureLoadedLocked()
	var pinnedBytes int64
	for k, e := range c.entries {
		if c.pinned[k] > 0 {
			pinnedBytes += e.size
		}
	}
	return RemoteCacheUsage{
		Bytes:       c.totalSize,
		Quota:       c.effectiveQuotaLocked(),
		Files:       len(c.entries),
		PinnedBytes: pinnedBytes,
	}
}

// Clear removes every unpinned file from the cache, sparing files currently
// pinned by loaded groups.
func (c *DirRemoteCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLoadedLocked(); err != nil {
		return err
	}
	var errs []error
	changed := false
	for k, e := range c.entries {
		if c.pinned[k] > 0 {
			continue
		}
		if p, err := c.blobPath(k); err == nil {
			if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				errs = append(errs, rmErr)
				continue
			}
		}
		c.totalSize -= e.size
		delete(c.entries, k)
		changed = true
	}
	if changed {
		c.dirty = true
		if err := c.flushLocked(true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Flush persists any buffered access-time updates to index.yaml on disk.
func (c *DirRemoteCache) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded || !c.dirty {
		return nil
	}
	return c.flushLocked(true)
}

// Close flushes any pending index updates to disk.
func (c *DirRemoteCache) Close() error {
	return c.Flush()
}

func (c *DirRemoteCache) evictLocked(keepKey string) {
	quota := c.effectiveQuotaLocked()
	if c.totalSize <= quota {
		return
	}
	type candidate struct {
		key      string
		accessed int64
		size     int64
	}
	candidates := make([]candidate, 0, len(c.entries))
	for k, e := range c.entries {
		if k == keepKey || c.pinned[k] > 0 {
			continue
		}
		candidates = append(candidates, candidate{key: k, accessed: e.accessed, size: e.size})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.accessed != b.accessed {
			if a.accessed < b.accessed {
				return -1
			}
			return 1
		}
		return strings.Compare(a.key, b.key)
	})
	for _, cand := range candidates {
		if c.totalSize <= quota {
			break
		}
		if p, err := c.blobPath(cand.key); err == nil {
			if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				continue
			}
		}
		c.totalSize -= cand.size
		delete(c.entries, cand.key)
	}
}

func (c *DirRemoteCache) flushLocked(force bool) error {
	if !c.dirty {
		return nil
	}
	now := c.now()
	if !force && c.FlushInterval > 0 && !c.lastFlush.IsZero() && now.Sub(c.lastFlush) < c.FlushInterval {
		return nil
	}
	if err := os.MkdirAll(filepath.Clean(c.dir), 0o755); err != nil {
		return fmt.Errorf("assets: remote cache: mkdir: %w", err)
	}
	idx := &remoteCacheIndex{
		Version: remoteCacheIndexVersion,
		Entries: make([]remoteCacheIndexEntry, 0, len(c.entries)),
	}
	for k, e := range c.entries {
		idx.Entries = append(idx.Entries, remoteCacheIndexEntry{
			SHA256:   k,
			Size:     e.size,
			Accessed: e.accessed,
		})
	}
	data, err := encodeRemoteCacheIndex(idx)
	if err != nil {
		return fmt.Errorf("assets: remote cache: encode index: %w", err)
	}
	if err := writeFileAtomic(c.indexPath(), ".tmp-index-*.yaml", data); err != nil {
		return fmt.Errorf("assets: remote cache: save index: %w", err)
	}
	c.dirty = false
	c.lastFlush = now
	return nil
}

func writeFileAtomic(dest, tempPattern string, data []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	serr := tmp.Sync()
	merr := tmp.Chmod(0o644)
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, merr, cerr); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// MemoryRemoteCache is an in-memory RemoteCache enforcing Quota, LRU eviction,
// and loaded-group pinning on every target including Web/WASM and unit tests.
type MemoryRemoteCache struct {
	// Quota is the maximum total size of cached files in bytes. When <= 0,
	// DefaultRemoteCacheQuota (200 MiB) is used.
	Quota int64

	mu        sync.Mutex
	entries   map[string]*memRemoteEntry
	pinned    map[string]int
	totalSize int64
	tick      int64
}

type memRemoteEntry struct {
	data     []byte
	accessed int64
}

// NewMemoryRemoteCache returns an empty in-memory RemoteCache. If quota is
// given and positive, it sets Quota; otherwise DefaultRemoteCacheQuota is used.
func NewMemoryRemoteCache(quota ...int64) *MemoryRemoteCache {
	q := DefaultRemoteCacheQuota
	if len(quota) > 0 && quota[0] > 0 {
		q = quota[0]
	}
	return &MemoryRemoteCache{
		Quota:   q,
		entries: map[string]*memRemoteEntry{},
		pinned:  map[string]int{},
	}
}

func (c *MemoryRemoteCache) effectiveQuotaLocked() int64 {
	if c.Quota <= 0 {
		return DefaultRemoteCacheQuota
	}
	return c.Quota
}

// Has reports whether sha256Hex is currently cached.
func (c *MemoryRemoteCache) Has(sha256Hex string) bool {
	c.mu.Lock()
	_, ok := c.entries[sha256Hex]
	c.mu.Unlock()
	return ok
}

// Get implements RemoteCache.
func (c *MemoryRemoteCache) Get(sha256Hex string) ([]byte, error) {
	if err := validateLowerSHA256(sha256Hex); err != nil {
		return nil, fmt.Errorf("assets: remote cache: invalid key: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[sha256Hex]
	if !ok {
		return nil, fs.ErrNotExist
	}
	c.tick++
	entry.accessed = c.tick
	return slices.Clone(entry.data), nil
}

// Put implements RemoteCache.
func (c *MemoryRemoteCache) Put(sha256Hex string, data []byte) error {
	if err := validateLowerSHA256(sha256Hex); err != nil {
		return fmt.Errorf("assets: remote cache: invalid key: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.entries[sha256Hex]; ok {
		c.totalSize -= int64(len(prev.data))
	}
	c.tick++
	cloned := slices.Clone(data)
	c.entries[sha256Hex] = &memRemoteEntry{
		data:     cloned,
		accessed: c.tick,
	}
	c.totalSize += int64(len(cloned))
	c.evictLocked(sha256Hex)
	return nil
}

// Remove implements RemoteCache.
func (c *MemoryRemoteCache) Remove(sha256Hex string) error {
	c.mu.Lock()
	if prev, ok := c.entries[sha256Hex]; ok {
		c.totalSize -= int64(len(prev.data))
		delete(c.entries, sha256Hex)
	}
	c.mu.Unlock()
	return nil
}

// Pin implements RemoteCache.
func (c *MemoryRemoteCache) Pin(sha256Hex string) {
	if validateLowerSHA256(sha256Hex) != nil {
		return
	}
	c.mu.Lock()
	c.pinned[sha256Hex]++
	c.mu.Unlock()
}

// Unpin implements RemoteCache.
func (c *MemoryRemoteCache) Unpin(sha256Hex string) {
	c.mu.Lock()
	if n := c.pinned[sha256Hex]; n <= 1 {
		delete(c.pinned, sha256Hex)
	} else {
		c.pinned[sha256Hex] = n - 1
	}
	c.mu.Unlock()
}

// Usage implements RemoteCache.
func (c *MemoryRemoteCache) Usage() RemoteCacheUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	var pinnedBytes int64
	for k, e := range c.entries {
		if c.pinned[k] > 0 {
			pinnedBytes += int64(len(e.data))
		}
	}
	return RemoteCacheUsage{
		Bytes:       c.totalSize,
		Quota:       c.effectiveQuotaLocked(),
		Files:       len(c.entries),
		PinnedBytes: pinnedBytes,
	}
}

// Clear removes all unpinned cached entries.
func (c *MemoryRemoteCache) Clear() {
	c.mu.Lock()
	for k, e := range c.entries {
		if c.pinned[k] > 0 {
			continue
		}
		c.totalSize -= int64(len(e.data))
		delete(c.entries, k)
	}
	c.mu.Unlock()
}

func (c *MemoryRemoteCache) evictLocked(keepKey string) {
	quota := c.effectiveQuotaLocked()
	if c.totalSize <= quota {
		return
	}
	type candidate struct {
		key      string
		accessed int64
		size     int64
	}
	candidates := make([]candidate, 0, len(c.entries))
	for k, e := range c.entries {
		if k == keepKey || c.pinned[k] > 0 {
			continue
		}
		candidates = append(candidates, candidate{key: k, accessed: e.accessed, size: int64(len(e.data))})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.accessed != b.accessed {
			if a.accessed < b.accessed {
				return -1
			}
			return 1
		}
		return strings.Compare(a.key, b.key)
	})
	for _, cand := range candidates {
		if c.totalSize <= quota {
			break
		}
		c.totalSize -= cand.size
		delete(c.entries, cand.key)
	}
}
