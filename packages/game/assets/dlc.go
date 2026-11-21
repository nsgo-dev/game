package assets

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// DefaultDLCChunkSize is the default streaming read buffer size (32 KiB) used
// by DLCManager when DLCOptions.ChunkSize is zero or negative.
const DefaultDLCChunkSize = 32 * 1024

var (
	// ErrIncompatibleBuild reports that a pack's MinBuild/MaxBuild range does
	// not include the running application's AppBuild.
	ErrIncompatibleBuild = errors.New("assets: pack incompatible with app build")
	// ErrHashMismatch reports that a pack's SHA-256 digest does not match
	// PackSpec.SHA256.
	ErrHashMismatch = errors.New("assets: pack sha256 mismatch")
	// ErrInvalidSignature reports that a pack's Ed25519 signature failed
	// verification against DLCOptions.PublicKey.
	ErrInvalidSignature = errors.New("assets: pack signature invalid")
	// ErrCorruptPack reports that a pack archive is unreadable or not a valid
	// ZIP archive.
	ErrCorruptPack = errors.New("assets: pack archive corrupt")
	// ErrSizeMismatch reports that a pack's byte length does not match
	// PackSpec.Size.
	ErrSizeMismatch = errors.New("assets: pack size mismatch")
)

// PackSpec describes a downloadable content (DLC) pack archive.
type PackSpec struct {
	// ID uniquely identifies the pack (for example "chapter-2").
	ID string `json:"id"`
	// Group names the assets.Group populated by FetchGroup. When empty, ID is
	// used as the group name.
	Group string `json:"group,omitempty"`
	// URL is the HTTP(S) endpoint serving the pack ZIP archive.
	URL string `json:"url"`
	// Version identifies the pack revision (for example "1.0.0").
	Version string `json:"version"`
	// MinBuild is the minimum inclusive application build compatible with this
	// pack. Zero means no lower bound.
	MinBuild int `json:"min_build,omitempty"`
	// MaxBuild is the maximum inclusive application build compatible with this
	// pack. Zero means no upper bound.
	MaxBuild int `json:"max_build,omitempty"`
	// Size is the expected pack size in bytes. When positive, downloads and
	// cached packs must match Size exactly.
	Size int64 `json:"size,omitempty"`
	// SHA256 is the expected hex-encoded SHA-256 digest of the pack archive.
	SHA256 string `json:"sha256"`
	// Signature is the Ed25519 signature over the raw 32-byte SHA-256 digest of
	// the pack, encoded as hex or base64. Required when DLCOptions.PublicKey is
	// set.
	Signature string `json:"signature,omitempty"`
}

// PackManifest is a JSON catalog of downloadable content packs.
type PackManifest struct {
	Packs []PackSpec `json:"packs"`
}

// ParsePackManifest decodes a JSON pack manifest containing either a
// PackManifest object ({"packs": [...]}) or a bare JSON array of PackSpec.
func ParsePackManifest(data []byte) ([]PackSpec, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("assets: dlc: empty manifest")
	}
	if trimmed[0] == '[' {
		var packs []PackSpec
		if err := json.Unmarshal(trimmed, &packs); err != nil {
			return nil, fmt.Errorf("assets: dlc: parse manifest array: %w", err)
		}
		return packs, nil
	}
	var manifest PackManifest
	if err := json.Unmarshal(trimmed, &manifest); err != nil {
		return nil, fmt.Errorf("assets: dlc: parse manifest: %w", err)
	}
	return manifest.Packs, nil
}

// Compatible reports whether appBuild falls within the pack's inclusive
// [MinBuild, MaxBuild] bounds. Zero bounds are treated as unbounded.
func (s PackSpec) Compatible(appBuild int) bool {
	if s.MinBuild > 0 && appBuild < s.MinBuild {
		return false
	}
	if s.MaxBuild > 0 && appBuild > s.MaxBuild {
		return false
	}
	return true
}

// CacheKey returns the stable cache key for this pack based on its ID and
// Version.
func (s PackSpec) CacheKey() string {
	if s.Version == "" {
		return s.ID
	}
	return s.ID + "@" + s.Version
}

// SignPack hashes data with SHA-256, signs the raw 32-byte digest with priv,
// and returns the hex-encoded SHA-256 digest and hex-encoded Ed25519 signature.
func SignPack(priv ed25519.PrivateKey, data []byte) (sha256Hex, signatureHex string) {
	sum := sha256.Sum256(data)
	sig := ed25519.Sign(priv, sum[:])
	return hex.EncodeToString(sum[:]), hex.EncodeToString(sig)
}

// SignPackDigest signs a hex-encoded SHA-256 digest with priv and returns the
// hex-encoded Ed25519 signature.
func SignPackDigest(priv ed25519.PrivateKey, sha256Hex string) (string, error) {
	digest, err := decodeSHA256(sha256Hex)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ed25519.Sign(priv, digest)), nil
}

// PackCache persists downloaded DLC packs and in-progress resumable partial
// downloads. Implementations must be safe for concurrent use.
type PackCache interface {
	// Open returns a random-access reader and byte size for a completed pack,
	// or fs.ErrNotExist if key is not cached. The returned io.ReaderAt must be
	// safe for concurrent ReadAt calls and must not leak resources without a
	// Close call.
	Open(key string) (io.ReaderAt, int64, error)
	// PartSize returns the byte size of an in-progress partial download for
	// key, or (0, nil) if no partial download exists.
	PartSize(key string) (int64, error)
	// OpenPartWriter opens the partial download entry for key. When appendMode
	// is true, writes append to existing partial bytes; when false, existing
	// partial bytes are truncated.
	OpenPartWriter(key string, appendMode bool) (io.WriteCloser, error)
	// OpenPartReader returns a random-access reader and byte size for an
	// in-progress partial download so it can be verified before CommitPart, or
	// fs.ErrNotExist if no partial download exists.
	OpenPartReader(key string) (io.ReaderAt, int64, error)
	// CommitPart atomically promotes the partial download for key into the
	// completed pack entry.
	CommitPart(key string) error
	// RemovePart deletes any partial download for key, returning nil if absent.
	RemovePart(key string) error
	// Remove deletes any completed pack for key, returning nil if absent.
	Remove(key string) error
}

// DirPackCache stores DLC packs on the local filesystem using ".pack.part"
// staging files and atomic os.Rename commits into ".pack" files.
type DirPackCache struct {
	dir string
	mu  sync.Mutex
}

// NewDirPackCache returns a filesystem-backed PackCache rooted at dir.
// Directories are created lazily on first write.
func NewDirPackCache(dir string) *DirPackCache {
	return &DirPackCache{dir: dir}
}

// Dir returns the root directory of the cache.
func (c *DirPackCache) Dir() string { return c.dir }

// Has reports whether a completed pack exists for key.
func (c *DirPackCache) Has(key string) bool {
	packPath, _, err := c.paths(key)
	if err != nil {
		return false
	}
	info, err := os.Stat(packPath)
	return err == nil && !info.IsDir()
}

// HasPart reports whether a partial ".part" file exists for key.
func (c *DirPackCache) HasPart(key string) bool {
	_, partPath, err := c.paths(key)
	if err != nil {
		return false
	}
	info, err := os.Stat(partPath)
	return err == nil && !info.IsDir()
}

// Open implements PackCache.
func (c *DirPackCache) Open(key string) (io.ReaderAt, int64, error) {
	packPath, _, err := c.paths(key)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(packPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, fs.ErrNotExist
		}
		return nil, 0, err
	}
	if info.IsDir() {
		return nil, 0, ErrCorruptPack
	}
	return pathReaderAt{path: packPath}, info.Size(), nil
}

// PartSize implements PackCache.
func (c *DirPackCache) PartSize(key string) (int64, error) {
	_, partPath, err := c.paths(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(partPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if info.IsDir() {
		return 0, ErrCorruptPack
	}
	return info.Size(), nil
}

// OpenPartWriter implements PackCache.
func (c *DirPackCache) OpenPartWriter(key string, appendMode bool) (io.WriteCloser, error) {
	_, partPath, err := c.paths(key)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return nil, err
	}
	return &syncFileWriter{f: f}, nil
}

// OpenPartReader implements PackCache.
func (c *DirPackCache) OpenPartReader(key string) (io.ReaderAt, int64, error) {
	_, partPath, err := c.paths(key)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(partPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, fs.ErrNotExist
		}
		return nil, 0, err
	}
	if info.IsDir() {
		return nil, 0, ErrCorruptPack
	}
	return pathReaderAt{path: partPath}, info.Size(), nil
}

// CommitPart implements PackCache via atomic os.Rename.
func (c *DirPackCache) CommitPart(key string) error {
	packPath, partPath, err := c.paths(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Rename(partPath, packPath); err != nil {
		return err
	}
	return nil
}

// RemovePart implements PackCache.
func (c *DirPackCache) RemovePart(key string) error {
	_, partPath, err := c.paths(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(partPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Remove implements PackCache.
func (c *DirPackCache) Remove(key string) error {
	packPath, _, err := c.paths(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(packPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (c *DirPackCache) paths(key string) (packPath, partPath string, err error) {
	if strings.TrimSpace(c.dir) == "" {
		return "", "", errors.New("assets: dlc: cache directory is empty")
	}
	if strings.TrimSpace(key) == "" {
		return "", "", errors.New("assets: dlc: pack cache key is empty")
	}
	base := safePackFilename(key)
	cleanDir := filepath.Clean(c.dir)
	packPath = filepath.Join(cleanDir, base+".pack")
	partPath = filepath.Join(cleanDir, base+".pack.part")
	if filepath.Dir(packPath) != cleanDir || filepath.Dir(partPath) != cleanDir {
		return "", "", fmt.Errorf("assets: dlc: invalid cache key %q", key)
	}
	return packPath, partPath, nil
}

func safePackFilename(key string) string {
	clean := !strings.HasPrefix(key, ".") && !strings.Contains(key, "..")
	if clean {
		for i := 0; i < len(key); i++ {
			ch := key[i]
			ok := (ch >= 'a' && ch <= 'z') ||
				(ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') ||
				ch == '.' || ch == '_' || ch == '-' || ch == '@'
			if !ok {
				clean = false
				break
			}
		}
	}
	if clean {
		return key
	}
	var b strings.Builder
	for i := 0; i < len(key) && b.Len() < 48; i++ {
		ch := key[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	sum := sha256.Sum256([]byte(key))
	if b.Len() > 0 {
		b.WriteByte('-')
	}
	b.WriteString(hex.EncodeToString(sum[:8]))
	return b.String()
}

// pathReaderAt implements io.ReaderAt without holding an open file descriptor,
// preventing descriptor leaks and Windows file-locking conflicts when zip.Reader
// is returned as an fs.FS.
type pathReaderAt struct {
	path string
}

func (p pathReaderAt) ReadAt(b []byte, off int64) (int, error) {
	f, err := os.Open(p.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.ReadAt(b, off)
}

type syncFileWriter struct {
	f *os.File
}

func (w *syncFileWriter) Write(p []byte) (int, error) {
	return w.f.Write(p)
}

func (w *syncFileWriter) Close() error {
	syncErr := w.f.Sync()
	closeErr := w.f.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// MemoryPackCache is an in-memory PackCache that works on every target
// including Web/WASM and unit tests.
type MemoryPackCache struct {
	mu    sync.RWMutex
	packs map[string][]byte
	parts map[string][]byte
}

// NewMemoryPackCache returns an empty in-memory PackCache.
func NewMemoryPackCache() *MemoryPackCache {
	return &MemoryPackCache{
		packs: map[string][]byte{},
		parts: map[string][]byte{},
	}
}

// Has reports whether a completed pack exists for key.
func (c *MemoryPackCache) Has(key string) bool {
	c.mu.RLock()
	_, ok := c.packs[key]
	c.mu.RUnlock()
	return ok
}

// HasPart reports whether a partial download exists for key.
func (c *MemoryPackCache) HasPart(key string) bool {
	c.mu.RLock()
	_, ok := c.parts[key]
	c.mu.RUnlock()
	return ok
}

// Open implements PackCache.
func (c *MemoryPackCache) Open(key string) (io.ReaderAt, int64, error) {
	c.mu.RLock()
	data, ok := c.packs[key]
	c.mu.RUnlock()
	if !ok {
		return nil, 0, fs.ErrNotExist
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

// PartSize implements PackCache.
func (c *MemoryPackCache) PartSize(key string) (int64, error) {
	c.mu.RLock()
	data := c.parts[key]
	c.mu.RUnlock()
	return int64(len(data)), nil
}

// OpenPartWriter implements PackCache.
func (c *MemoryPackCache) OpenPartWriter(key string, appendMode bool) (io.WriteCloser, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("assets: dlc: pack cache key is empty")
	}
	c.mu.Lock()
	if !appendMode {
		c.parts[key] = nil
	} else if _, ok := c.parts[key]; !ok {
		c.parts[key] = nil
	}
	c.mu.Unlock()
	return &memPartWriter{cache: c, key: key}, nil
}

// OpenPartReader implements PackCache.
func (c *MemoryPackCache) OpenPartReader(key string) (io.ReaderAt, int64, error) {
	c.mu.RLock()
	data, ok := c.parts[key]
	var cp []byte
	if ok {
		cp = slices.Clone(data)
	}
	c.mu.RUnlock()
	if !ok {
		return nil, 0, fs.ErrNotExist
	}
	return bytes.NewReader(cp), int64(len(cp)), nil
}

// CommitPart implements PackCache.
func (c *MemoryPackCache) CommitPart(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.parts[key]
	if !ok {
		return fs.ErrNotExist
	}
	c.packs[key] = slices.Clone(data)
	delete(c.parts, key)
	return nil
}

// RemovePart implements PackCache.
func (c *MemoryPackCache) RemovePart(key string) error {
	c.mu.Lock()
	delete(c.parts, key)
	c.mu.Unlock()
	return nil
}

// Remove implements PackCache.
func (c *MemoryPackCache) Remove(key string) error {
	c.mu.Lock()
	delete(c.packs, key)
	c.mu.Unlock()
	return nil
}

type memPartWriter struct {
	cache  *MemoryPackCache
	key    string
	closed bool
}

func (w *memPartWriter) Write(p []byte) (int, error) {
	w.cache.mu.Lock()
	defer w.cache.mu.Unlock()
	if w.closed {
		return 0, fs.ErrClosed
	}
	w.cache.parts[w.key] = append(w.cache.parts[w.key], p...)
	return len(p), nil
}

func (w *memPartWriter) Close() error {
	w.cache.mu.Lock()
	w.closed = true
	w.cache.mu.Unlock()
	return nil
}

// DLCOptions configures a DLCManager created by NewDLCManager.
type DLCOptions struct {
	// AppBuild is the running application's build number, checked against each
	// PackSpec's MinBuild and MaxBuild range.
	AppBuild int
	// PublicKey is the Ed25519 public key used to verify pack signatures over
	// each pack's 32-byte SHA-256 digest.
	PublicKey ed25519.PublicKey
	// Cache stores completed packs and resumable .part downloads. When nil, a
	// new MemoryPackCache is used.
	Cache PackCache
	// HTTPClient performs pack downloads. When nil, http.DefaultClient is used.
	HTTPClient *http.Client
	// ChunkSize sets the buffer size for streaming reads and progress updates.
	// When <= 0, DefaultDLCChunkSize (32 KiB) is used.
	ChunkSize int
}

// DLCManager downloads, resumes, verifies, caches, and mounts DLC pack ZIP
// archives as fs.FS instances and assets.Groups.
type DLCManager struct {
	appBuild   int
	publicKey  ed25519.PublicKey
	cache      PackCache
	httpClient *http.Client
	chunkSize  int

	locksMu sync.Mutex
	locks   map[string]*keyLock
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

// NewDLCManager returns a DLCManager configured with opts.
func NewDLCManager(opts DLCOptions) *DLCManager {
	cache := opts.Cache
	if cache == nil {
		cache = NewMemoryPackCache()
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultDLCChunkSize
	}
	var pubKey ed25519.PublicKey
	if len(opts.PublicKey) > 0 {
		pubKey = slices.Clone(opts.PublicKey)
	}
	return &DLCManager{
		appBuild:   opts.AppBuild,
		publicKey:  pubKey,
		cache:      cache,
		httpClient: client,
		chunkSize:  chunkSize,
		locks:      map[string]*keyLock{},
	}
}

// Cache returns the PackCache used by this DLCManager.
func (d *DLCManager) Cache() PackCache { return d.cache }

// Remove deletes both the cached pack and any partial download for spec.
func (d *DLCManager) Remove(spec PackSpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return errors.New("assets: dlc: pack ID is required")
	}
	key := spec.CacheKey()
	unlock := d.lockKey(key)
	defer unlock()
	errPack := d.cache.Remove(key)
	errPart := d.cache.RemovePart(key)
	return errors.Join(errPack, errPart)
}

// Fetch ensures spec is compatible with AppBuild, checks and re-verifies any
// cached pack, downloads missing or resumed bytes via HTTP Range requests into
// a .part staging entry, strictly verifies size, SHA-256, Ed25519 signature,
// and ZIP structure, commits atomically to the cache, and returns the mounted
// ZIP archive as an fs.FS.
//
// On network or context interruption, the .part entry is preserved so a
// subsequent Fetch resumes via HTTP Range ("bytes=<offset>-"). On any
// compatibility or verification failure, corrupt or mismatched .part and pack
// entries are immediately removed from the cache.
func (d *DLCManager) Fetch(ctx context.Context, spec PackSpec, onProgress func(downloaded, total int64)) (fs.FS, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(spec.ID) == "" {
		return nil, errors.New("assets: dlc: pack ID is required")
	}

	key := spec.CacheKey()
	unlock := d.lockKey(key)
	defer unlock()

	// 1. Check build compatibility upfront before any network or mount work.
	if !spec.Compatible(d.appBuild) {
		_ = d.cache.RemovePart(key)
		_ = d.cache.Remove(key)
		return nil, fmt.Errorf("%w: app build %d incompatible with pack %q (min=%d max=%d)",
			ErrIncompatibleBuild, d.appBuild, spec.ID, spec.MinBuild, spec.MaxBuild)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 2. Re-verify an already cached pack before using it.
	if r, size, err := d.cache.Open(key); err == nil {
		zr, verifyErr := d.verifyPack(r, size, spec)
		if verifyErr == nil {
			if onProgress != nil {
				onProgress(size, size)
			}
			return zr, nil
		}
		_ = d.cache.Remove(key)
		_ = d.cache.RemovePart(key)
		return nil, verifyErr
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("assets: dlc %q: open cache: %w", spec.ID, err)
	}

	// 3. Check if an existing .part file already holds the exact expected size
	// (for example after a crash right before commit).
	offset, err := d.cache.PartSize(key)
	if err != nil {
		return nil, fmt.Errorf("assets: dlc %q: stat partial download: %w", spec.ID, err)
	}
	if spec.Size > 0 && offset > spec.Size {
		_ = d.cache.RemovePart(key)
		offset = 0
	} else if spec.Size > 0 && offset == spec.Size {
		if r, size, openErr := d.cache.OpenPartReader(key); openErr == nil {
			if _, verifyErr := d.verifyPack(r, size, spec); verifyErr == nil {
				if commitErr := d.cache.CommitPart(key); commitErr != nil {
					return nil, fmt.Errorf("assets: dlc %q: commit pack: %w", spec.ID, commitErr)
				}
				if onProgress != nil {
					onProgress(size, size)
				}
				return d.openVerifiedCommitted(key, spec)
			}
		}
		_ = d.cache.RemovePart(key)
		offset = 0
	}

	if strings.TrimSpace(spec.URL) == "" {
		return nil, fmt.Errorf("assets: dlc %q: pack URL is required", spec.ID)
	}

	// 4. Perform resumable HTTP download into the .part staging entry.
	if err := d.downloadPart(ctx, key, spec, offset, onProgress); err != nil {
		return nil, err
	}

	// 5. Strictly verify the completed .part before promoting to final pack.
	r, size, err := d.cache.OpenPartReader(key)
	if err != nil {
		_ = d.cache.RemovePart(key)
		_ = d.cache.Remove(key)
		return nil, fmt.Errorf("assets: dlc %q: open partial pack: %w", spec.ID, err)
	}
	if _, err := d.verifyPack(r, size, spec); err != nil {
		_ = d.cache.RemovePart(key)
		_ = d.cache.Remove(key)
		return nil, err
	}

	// 6. Atomically commit .part to the final cached pack and open as fs.FS.
	if err := d.cache.CommitPart(key); err != nil {
		_ = d.cache.RemovePart(key)
		return nil, fmt.Errorf("assets: dlc %q: commit pack: %w", spec.ID, err)
	}
	return d.openVerifiedCommitted(key, spec)
}

func (d *DLCManager) openVerifiedCommitted(key string, spec PackSpec) (fs.FS, error) {
	r, size, err := d.cache.Open(key)
	if err != nil {
		return nil, fmt.Errorf("assets: dlc %q: open committed pack: %w", spec.ID, err)
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		_ = d.cache.Remove(key)
		return nil, fmt.Errorf("%w: open committed zip: %v", ErrCorruptPack, err)
	}
	return zr, nil
}

// FetchGroup fetches and verifies spec, mounts the resulting ZIP fs.FS onto
// loader.Group(spec.Group) (or spec.ID when spec.Group is empty), invokes
// registerFn on the engine goroutine to register assets in the group, and
// loads the group to completion.
//
// FetchGroup blocks until download and group loading finish or ctx (or
// loader's scope) is cancelled, so it should be called off the engine
// goroutine.
func (d *DLCManager) FetchGroup(
	ctx context.Context,
	loader *Manager,
	spec PackSpec,
	registerFn func(g *Group, packFS fs.FS) error,
	onProgress func(downloaded, total int64),
) (*Group, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if loader == nil {
		return nil, errors.New("assets: dlc: asset manager is nil")
	}
	if loader.isDisposed() {
		return nil, context.Canceled
	}

	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()
	stopWatch := context.AfterFunc(loader.ctx, cancelFetch)
	defer stopWatch()

	packFS, err := d.Fetch(fetchCtx, spec, onProgress)
	if err != nil {
		return nil, err
	}

	groupName := spec.Group
	if groupName == "" {
		groupName = spec.ID
	}

	type loadResult struct {
		group *Group
		err   error
	}
	doneCh := make(chan loadResult, 1)

	loader.cx.Post(func() {
		if loader.isDisposed() {
			doneCh <- loadResult{err: context.Canceled}
			return
		}
		g := loader.Group(groupName)
		if g.state == stateLoaded {
			g.Release()
		}
		g.SetFS(packFS)
		if registerFn != nil {
			if err := registerFn(g, packFS); err != nil {
				doneCh <- loadResult{err: err}
				return
			}
		}
		g.Load(func(err error) {
			doneCh <- loadResult{group: g, err: err}
		})
	})

	select {
	case res := <-doneCh:
		if res.err != nil {
			return nil, res.err
		}
		return res.group, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-loader.ctx.Done():
		return nil, loader.ctx.Err()
	}
}

func (d *DLCManager) downloadPart(ctx context.Context, key string, spec PackSpec, offset int64, onProgress func(downloaded, total int64)) error {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
		if err != nil {
			return fmt.Errorf("assets: dlc %q: build request: %w", spec.ID, err)
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}

		resp, err := d.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("assets: dlc %q: download: %w", spec.ID, err)
		}

		retryFromZero, err := d.handleDownloadResponse(ctx, key, spec, offset, resp, onProgress)
		resp.Body.Close()
		if retryFromZero && attempt == 0 {
			_ = d.cache.RemovePart(key)
			offset = 0
			continue
		}
		return err
	}
	return fmt.Errorf("assets: dlc %q: download failed after retry", spec.ID)
}

func (d *DLCManager) handleDownloadResponse(
	ctx context.Context,
	key string,
	spec PackSpec,
	offset int64,
	resp *http.Response,
	onProgress func(downloaded, total int64),
) (retryFromZero bool, err error) {
	var (
		appendMode bool
		downloaded int64
		totalSize  int64 = -1
	)
	if spec.Size > 0 {
		totalSize = spec.Size
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		if offset == 0 {
			return false, fmt.Errorf("assets: dlc %q: unexpected 206 Partial Content without Range request", spec.ID)
		}
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			start, _, total, ok := parseContentRange(cr)
			if !ok {
				return false, fmt.Errorf("assets: dlc %q: invalid Content-Range %q", spec.ID, cr)
			}
			if start != offset {
				return false, fmt.Errorf("assets: dlc %q: Content-Range start %d does not match requested offset %d", spec.ID, start, offset)
			}
			if total >= 0 {
				if spec.Size > 0 && total != spec.Size {
					_ = d.cache.RemovePart(key)
					_ = d.cache.Remove(key)
					return false, fmt.Errorf("%w: server total %d != spec size %d", ErrSizeMismatch, total, spec.Size)
				}
				if totalSize < 0 {
					totalSize = total
				}
			}
		} else if totalSize < 0 && resp.ContentLength >= 0 {
			totalSize = offset + resp.ContentLength
		}
		appendMode = true
		downloaded = offset

	case http.StatusOK:
		if spec.Size > 0 && resp.ContentLength >= 0 && resp.ContentLength != spec.Size {
			_ = d.cache.RemovePart(key)
			_ = d.cache.Remove(key)
			return false, fmt.Errorf("%w: server content-length %d != spec size %d", ErrSizeMismatch, resp.ContentLength, spec.Size)
		}
		if totalSize < 0 && resp.ContentLength >= 0 {
			totalSize = resp.ContentLength
		}
		appendMode = false
		downloaded = 0

	case http.StatusRequestedRangeNotSatisfiable:
		if offset > 0 {
			// If spec.Size was unspecified and the .part file is already the
			// complete archive, let caller verify it cleanly.
			if spec.Size == 0 {
				if r, size, openErr := d.cache.OpenPartReader(key); openErr == nil {
					if _, verifyErr := d.verifyPack(r, size, spec); verifyErr == nil {
						if onProgress != nil {
							onProgress(size, size)
						}
						return false, nil
					}
				}
			}
			return true, nil
		}
		return false, fmt.Errorf("assets: dlc %q: unexpected HTTP status %s", spec.ID, resp.Status)

	default:
		return false, fmt.Errorf("assets: dlc %q: unexpected HTTP status %s", spec.ID, resp.Status)
	}

	w, err := d.cache.OpenPartWriter(key, appendMode)
	if err != nil {
		return false, fmt.Errorf("assets: dlc %q: open partial writer: %w", spec.ID, err)
	}
	if appendMode && downloaded > 0 && onProgress != nil {
		onProgress(downloaded, totalSize)
	}

	buf := make([]byte, d.chunkSize)
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = w.Close()
			return false, ctxErr
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if spec.Size > 0 && downloaded+int64(n) > spec.Size {
				_ = w.Close()
				_ = d.cache.RemovePart(key)
				_ = d.cache.Remove(key)
				return false, fmt.Errorf("%w: download exceeded expected size %d", ErrSizeMismatch, spec.Size)
			}
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				_ = w.Close()
				return false, fmt.Errorf("assets: dlc %q: write partial pack: %w", spec.ID, writeErr)
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, totalSize)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// Preserve .part so the next Fetch resumes from the written offset.
			_ = w.Close()
			return false, fmt.Errorf("assets: dlc %q: read body: %w", spec.ID, readErr)
		}
	}

	if err := w.Close(); err != nil {
		return false, fmt.Errorf("assets: dlc %q: close partial pack: %w", spec.ID, err)
	}
	return false, nil
}

func (d *DLCManager) verifyPack(r io.ReaderAt, size int64, spec PackSpec) (*zip.Reader, error) {
	if spec.Size > 0 && size != spec.Size {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrSizeMismatch, size, spec.Size)
	}

	expectedHash, err := decodeSHA256(spec.SHA256)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHashMismatch, err)
	}

	h := sha256.New()
	sr := io.NewSectionReader(r, 0, size)
	buf := make([]byte, d.chunkSize)
	n, err := io.CopyBuffer(h, sr, buf)
	if err != nil {
		return nil, fmt.Errorf("%w: read pack: %v", ErrCorruptPack, err)
	}
	if n != size {
		return nil, fmt.Errorf("%w: read %d bytes, expected %d", ErrSizeMismatch, n, size)
	}

	actualHash := h.Sum(nil)
	if subtle.ConstantTimeCompare(actualHash, expectedHash) != 1 {
		return nil, fmt.Errorf("%w: got %x, want %x", ErrHashMismatch, actualHash, expectedHash)
	}

	if len(d.publicKey) > 0 || strings.TrimSpace(spec.Signature) != "" {
		if len(d.publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: public key missing or invalid length", ErrInvalidSignature)
		}
		sigBytes, err := decodeSignature(spec.Signature)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		}
		if !ed25519.Verify(d.publicKey, actualHash, sigBytes) {
			return nil, fmt.Errorf("%w: verification failed for pack %q", ErrInvalidSignature, spec.ID)
		}
	}

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid zip archive: %v", ErrCorruptPack, err)
	}
	return zr, nil
}

func (d *DLCManager) lockKey(key string) func() {
	d.locksMu.Lock()
	kl := d.locks[key]
	if kl == nil {
		kl = &keyLock{}
		d.locks[key] = kl
	}
	kl.refs++
	d.locksMu.Unlock()

	kl.mu.Lock()
	return func() {
		kl.mu.Unlock()
		d.locksMu.Lock()
		kl.refs--
		if kl.refs == 0 && d.locks[key] == kl {
			delete(d.locks, key)
		}
		d.locksMu.Unlock()
	}
}

func decodeSHA256(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("missing expected sha256")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid sha256 hex: %w", err)
	}
	if len(b) != sha256.Size {
		return nil, fmt.Errorf("invalid sha256 length %d", len(b))
	}
	return b, nil
}

func decodeSignature(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("missing signature")
	}
	if len(s) == ed25519.SignatureSize*2 {
		b, err := hex.DecodeString(s)
		if err == nil && len(b) == ed25519.SignatureSize {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		b, err := enc.DecodeString(s)
		if err == nil && len(b) == ed25519.SignatureSize {
			return b, nil
		}
	}
	return nil, errors.New("invalid ed25519 signature encoding")
}

func parseContentRange(header string) (start, end, total int64, ok bool) {
	h := strings.TrimSpace(header)
	if len(h) < 6 || !strings.EqualFold(h[:6], "bytes ") {
		return 0, 0, 0, false
	}
	spec := strings.TrimSpace(h[6:])
	rangePart, totalPart, found := strings.Cut(spec, "/")
	if !found {
		return 0, 0, 0, false
	}
	startStr, endStr, found := strings.Cut(rangePart, "-")
	if !found {
		return 0, 0, 0, false
	}
	var err error
	start, err = strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, 0, false
	}
	end, err = strconv.ParseInt(endStr, 10, 64)
	if err != nil || end < start {
		return 0, 0, 0, false
	}
	if totalPart == "*" {
		return start, end, -1, true
	}
	total, err = strconv.ParseInt(totalPart, 10, 64)
	if err != nil || total <= end {
		return 0, 0, 0, false
	}
	return start, end, total, true
}
