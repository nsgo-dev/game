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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/loaderr"
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
	// ErrPackTooLarge reports that a pack archive, or a file read from a mounted pack, would exceed PackLimits: a download without a PackSpec.Size that grows past MaxPackBytes, or an entry that decompresses past MaxFileBytes.
	ErrPackTooLarge = errors.New("assets: pack exceeds its size limit")
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
	// Packs lists the packs the catalog offers.
	Packs []PackSpec `json:"packs"`
}

// ParsePackManifest decodes a JSON pack manifest containing either a
// PackManifest object ({"packs": [...]}) or a bare JSON array of PackSpec.
//
// A field the manifest does not define, such as a misspelt "sha265", is refused rather than ignored, as are a key given twice in one object and bytes after the manifest, so a typo cannot silently drop a check and a reader cannot be shown one value while another is used (#546).
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func ParsePackManifest(data []byte) ([]PackSpec, error) {
	const fix = `write the manifest as {"packs": [{"id": "...", "url": "...", "version": "...", "sha256": "..."}]} or as a bare array of packs`
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, &loaderr.Error{Problem: "empty manifest", Fix: fix}
	}
	// Decode data rather than trimmed, so the decoder's offsets are the file's.
	if trimmed[0] == '[' {
		var packs []PackSpec
		if err := decodeStrictJSON(data, &packs); err != nil {
			return nil, loaderr.Locate(err, "", data, fix)
		}
		return packs, nil
	}
	var manifest PackManifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return nil, loaderr.Locate(err, "", data, fix)
	}
	return manifest.Packs, nil
}

// decodeStrictJSON decodes the one JSON value in data into v, refusing unknown fields, a key given twice in one object and anything after the value.
func decodeStrictJSON(data []byte, v any) error {
	// json.Unmarshal reports bytes after the value as a located syntax error, which a Decoder would leave unread.
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := refuseDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// refuseDuplicateKeys reports the first key that appears twice in one object of data, which must be valid JSON. encoding/json keeps the last of duplicate keys and matches keys to fields without regard to case, so keys are compared as it matches them.
func refuseDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		var seen map[string]bool
		if delim == '{' {
			seen = map[string]bool{}
		}
		for dec.More() {
			if seen != nil {
				start := dec.InputOffset()
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := keyTok.(string)
				folded := foldJSONKey(key)
				if seen[folded] {
					line, column := jsonKeyPosition(data, start)
					return &loaderr.Error{Line: line, Column: column, Problem: fmt.Sprintf("key %q appears more than once in one object", key), Fix: "give each key once; JSON keeps only the last value of a repeated key"}
				}
				seen[folded] = true
			}
			if err := value(); err != nil {
				return err
			}
		}
		_, err = dec.Token() // the closing delimiter
		return err
	}
	return value()
}

// foldJSONKey maps key to one spelling per class of keys that encoding/json matches to the same field: each rune becomes the smallest rune of its simple case-folding orbit.
func foldJSONKey(key string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, key)
}

// jsonKeyPosition returns the 1-based line and column of the key that starts after offset in data, skipping the separator and white space before it.
func jsonKeyPosition(data []byte, offset int64) (line, column int) {
	i := int(min(offset, int64(len(data))))
	for i < len(data) && strings.IndexByte(", \t\r\n", data[i]) >= 0 {
		i++
	}
	line, column = 1, 1
	for _, b := range data[:i] {
		if b == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return line, column
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
	return openFileReaderAt(packPath)
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
	return openFileReaderAt(partPath)
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

// fileReaderAt reads a cache entry through one open file, so the bytes a pack was verified against are the bytes its mount serves even if the cache file is later replaced or removed (#546). The file is closed by Close, or when the reader (and the zip.Reader mounting it) becomes unreachable.
type fileReaderAt struct {
	f *os.File
}

func openFileReaderAt(path string) (io.ReaderAt, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, fs.ErrNotExist
		}
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	r := &fileReaderAt{f: f}
	runtime.AddCleanup(r, func(f *os.File) { _ = f.Close() }, f)
	return r, info.Size(), nil
}

// ReadAt implements io.ReaderAt; it is safe for concurrent use.
func (r *fileReaderAt) ReadAt(b []byte, off int64) (int, error) {
	return r.f.ReadAt(b, off)
}

// Close closes the underlying file.
func (r *fileReaderAt) Close() error {
	return r.f.Close()
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
//
// A pack is held once: readers share the stored bytes, which are never written after they are stored, and CommitPart moves the partial download into place instead of copying it (#546).
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
	c.mu.RUnlock()
	if !ok {
		return nil, 0, fs.ErrNotExist
	}
	// Appends write past len(data) or into a new array and a truncating OpenPartWriter starts a new slice, so the bytes this reader sees never change.
	return bytes.NewReader(data), int64(len(data)), nil
}

// CommitPart implements PackCache.
func (c *MemoryPackCache) CommitPart(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.parts[key]
	if !ok {
		return fs.ErrNotExist
	}
	// The part leaves the map, so no writer appends to this slice again.
	c.packs[key] = data
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
	// each pack's 32-byte SHA-256 digest. Without it every pack is refused with ErrInvalidSignature unless AllowUnsigned is set.
	PublicKey ed25519.PublicKey
	// AllowUnsigned accepts packs without a signature when PublicKey is empty, checking only their size and SHA-256 from the PackSpec. Whoever supplies the PackSpec then chooses what is mounted, so set it only for development or when the specs ship inside the app (#546).
	AllowUnsigned bool
	// Cache stores completed packs and resumable .part downloads. When nil, a
	// new MemoryPackCache is used.
	Cache PackCache
	// HTTPClient performs pack downloads. When nil, http.DefaultClient is used.
	HTTPClient *http.Client
	// ChunkSize sets the buffer size for streaming reads and progress updates.
	// When <= 0, DefaultDLCChunkSize (32 KiB) is used.
	ChunkSize int
	// Limits caps how large a pack may be and what it may contain; zero fields use DefaultPackLimits. A pack over any limit is refused before it is mounted, a download stops as soon as it would pass MaxPackBytes, and a mounted pack reads each file as it decompresses, failing with ErrPackTooLarge past MaxFileBytes, so the caps also bound what reading an asset can allocate.
	Limits PackLimits
}

// PackLimits bounds the size and structure of a DLC pack ZIP.
type PackLimits struct {
	// MaxEntries is the largest number of files a pack may hold.
	MaxEntries int
	// MaxFileBytes is the largest uncompressed size of one file, checked against the archive's headers before mounting and against the bytes themselves as a mounted pack reads them.
	MaxFileBytes uint64
	// MaxTotalBytes is the largest uncompressed size of all files together.
	MaxTotalBytes uint64
	// MaxPackBytes is the largest archive a DLCManager downloads or opens. It bounds the .part file of a PackSpec without a Size, which would otherwise grow as long as the server sends bytes, and a PackSpec whose Size is larger is refused before anything is downloaded.
	MaxPackBytes int64
}

// DefaultPackLimits returns the limits a DLCManager uses for zero PackLimits fields: 32768 entries, 256 MiB per file, 1 GiB in total and a 1088 MiB (1 GiB + 64 MiB) archive.
func DefaultPackLimits() PackLimits {
	return PackLimits{MaxEntries: maxPackEntries, MaxFileBytes: maxPackFileUnpacked, MaxTotalBytes: maxPackTotalUnpacked, MaxPackBytes: maxPackArchive}
}

func (l PackLimits) withDefaults() PackLimits {
	def := DefaultPackLimits()
	if l.MaxEntries <= 0 {
		l.MaxEntries = def.MaxEntries
	}
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = def.MaxFileBytes
	}
	if l.MaxTotalBytes == 0 {
		l.MaxTotalBytes = def.MaxTotalBytes
	}
	if l.MaxPackBytes <= 0 {
		l.MaxPackBytes = def.MaxPackBytes
	}
	return l
}

// DLCManager downloads, resumes, verifies, caches, and mounts DLC pack ZIP
// archives as fs.FS instances and assets.Groups.
type DLCManager struct {
	appBuild      int
	publicKey     ed25519.PublicKey
	allowUnsigned bool
	cache         PackCache
	httpClient    *http.Client
	chunkSize     int
	limits        PackLimits

	locksMu sync.Mutex
	locks   map[string]*keyLock

	mountsMu sync.Mutex
	mounts   map[string]*packMount
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
		appBuild:      opts.AppBuild,
		publicKey:     pubKey,
		allowUnsigned: opts.AllowUnsigned,
		cache:         cache,
		httpClient:    client,
		chunkSize:     chunkSize,
		limits:        opts.Limits.withDefaults(),
		locks:         map[string]*keyLock{},
		mounts:        map[string]*packMount{},
	}
}

// Cache returns the PackCache used by this DLCManager.
func (d *DLCManager) Cache() PackCache { return d.cache }

// Remove deletes both the cached pack and any partial download for spec.
//
// A pack that Fetch or FetchGroup mounted and that is still in use is not pulled from under its readers: its partial download goes at once, and the cached pack when the last mount is released, by closing the fs.FS Fetch returned or by resetting or re-mounting the group FetchGroup loaded (#546). A Fetch of the same pack before then keeps it.
func (d *DLCManager) Remove(spec PackSpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return errors.New("assets: dlc: pack ID is required")
	}
	key := spec.CacheKey()
	unlock := d.lockKey(key)
	defer unlock()
	errPart := d.cache.RemovePart(key)
	d.mountsMu.Lock()
	if m := d.mounts[key]; m != nil {
		m.removePending = true
		d.mountsMu.Unlock()
		return errPart
	}
	d.mountsMu.Unlock()
	errPack := d.cache.Remove(key)
	return errors.Join(errPack, errPart)
}

// checkSpec refuses a PackSpec that could never verify, before Fetch touches the cache, so a malformed spec cannot delete a good cached pack (#546).
func (d *DLCManager) checkSpec(spec PackSpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return errors.New("assets: dlc: pack ID is required")
	}
	if spec.Size < 0 {
		return fmt.Errorf("%w: pack %q has a negative size %d", ErrSizeMismatch, spec.ID, spec.Size)
	}
	if spec.Size > d.limits.MaxPackBytes {
		return fmt.Errorf("%w: pack %q is %d bytes, more than %d", ErrPackTooLarge, spec.ID, spec.Size, d.limits.MaxPackBytes)
	}
	if _, err := decodeSHA256(spec.SHA256); err != nil {
		return fmt.Errorf("%w: pack %q: %v", ErrHashMismatch, spec.ID, err)
	}
	if strings.TrimSpace(spec.Signature) != "" {
		if _, err := decodeSignature(spec.Signature); err != nil {
			return fmt.Errorf("%w: pack %q: %v", ErrInvalidSignature, spec.ID, err)
		}
	}
	return nil
}

// Fetch ensures spec is compatible with AppBuild, checks and re-verifies any cached pack, downloads missing or resumed bytes via HTTP Range requests into a .part staging entry, strictly verifies size, SHA-256, Ed25519 signature, and ZIP structure, commits atomically to the cache, and returns the mounted ZIP archive as an fs.FS.
//
// The returned fs.FS also implements io.Closer and fs.ReadFileFS. It reads through the one cache reader it was verified on, and each file as it decompresses, bounded by PackLimits. Close it when the pack is no longer used: that releases the cache entry for Remove; one that is never closed is released when it is garbage collected. FetchGroup closes the pack it mounts when the group is reset or mounted again.
//
// On network or context interruption, the .part entry is preserved so a subsequent Fetch resumes via HTTP Range ("bytes=<offset>-"). A spec that could never verify (a missing ID, a negative Size, a malformed SHA256 or Signature) is refused before the cache is touched. A cached pack whose bytes do not match the spec's size or SHA-256, and a download that fails verification, are removed from the cache; a cached pack whose bytes match but whose signature or contents are refused is kept, because downloading it again would fetch the same bytes.
func (d *DLCManager) Fetch(ctx context.Context, spec PackSpec, onProgress func(downloaded, total int64)) (fsys fs.FS, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := d.checkSpec(spec); err != nil {
		return nil, err
	}

	key := spec.CacheKey()
	unlock := d.lockKey(key)
	defer unlock()
	// A Remove pending on a mounted pack is held off while this Fetch runs, so the last mount released meanwhile cannot delete the pack this Fetch is opening or has just committed; a Fetch that fails puts it back (#546).
	if d.takePendingRemove(key) {
		defer func() {
			if fsys == nil {
				d.restorePendingRemove(key)
			}
		}()
	}

	// 1. Check build compatibility upfront before any network or mount work.
	if !spec.Compatible(d.appBuild) {
		_ = d.cache.RemovePart(key)
		d.removeUnlessMounted(key)
		return nil, fmt.Errorf("%w: app build %d incompatible with pack %q (min=%d max=%d)",
			ErrIncompatibleBuild, d.appBuild, spec.ID, spec.MinBuild, spec.MaxBuild)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 2. Re-verify an already cached pack before using it.
	if r, size, err := d.cache.Open(key); err == nil {
		zr, bytesBad, verifyErr := d.verifyPack(r, size, spec)
		if verifyErr == nil {
			if onProgress != nil {
				onProgress(size, size)
			}
			return d.newPackFS(key, r, zr), nil
		}
		closeReader(r)
		if bytesBad {
			// Bytes that fail this very spec's size or hash are deleted even under a mount, which keeps its open file where the platform allows: keeping them would fail every later Fetch until the mount went.
			_ = d.cache.Remove(key)
			_ = d.cache.RemovePart(key)
		}
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
	if limit := d.maxDownload(spec); offset > limit {
		_ = d.cache.RemovePart(key)
		offset = 0
	} else if spec.Size > 0 && offset == spec.Size {
		if r, size, openErr := d.cache.OpenPartReader(key); openErr == nil {
			_, _, verifyErr := d.verifyPack(r, size, spec)
			closeReader(r) // release the staging file before it is renamed
			if verifyErr == nil {
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
		d.removeUnlessMounted(key)
		return nil, fmt.Errorf("assets: dlc %q: open partial pack: %w", spec.ID, err)
	}
	_, _, err = d.verifyPack(r, size, spec)
	closeReader(r) // release the staging file before it is renamed
	if err != nil {
		_ = d.cache.RemovePart(key)
		d.removeUnlessMounted(key)
		return nil, err
	}

	// 6. Atomically commit .part to the final cached pack and open as fs.FS.
	if err := d.cache.CommitPart(key); err != nil {
		_ = d.cache.RemovePart(key)
		return nil, fmt.Errorf("assets: dlc %q: commit pack: %w", spec.ID, err)
	}
	return d.openVerifiedCommitted(key, spec)
}

// maxDownload is the most bytes a download of spec may write: its Size, or PackLimits.MaxPackBytes when it has none.
func (d *DLCManager) maxDownload(spec PackSpec) int64 {
	if spec.Size > 0 {
		return spec.Size
	}
	return d.limits.MaxPackBytes
}

// openVerifiedCommitted opens the committed pack and verifies it again on the very reader it mounts, so bytes swapped in between the staging check and the mount are never served (#546).
func (d *DLCManager) openVerifiedCommitted(key string, spec PackSpec) (fs.FS, error) {
	r, size, err := d.cache.Open(key)
	if err != nil {
		return nil, fmt.Errorf("assets: dlc %q: open committed pack: %w", spec.ID, err)
	}
	zr, _, err := d.verifyPack(r, size, spec)
	if err != nil {
		closeReader(r)
		_ = d.cache.Remove(key)
		return nil, err
	}
	return d.newPackFS(key, r, zr), nil
}

// closeFS closes fsys when it holds a resource, such as a pack Fetch mounted.
func closeFS(fsys fs.FS) {
	if c, ok := fsys.(io.Closer); ok {
		_ = c.Close()
	}
}

// closeReader releases r early when it holds a resource, such as the open file of a DirPackCache entry; otherwise the resource is released when r is garbage collected.
func closeReader(r io.ReaderAt) {
	if c, ok := r.(io.Closer); ok {
		_ = c.Close()
	}
}

// FetchGroup fetches and verifies spec, mounts the resulting ZIP fs.FS onto
// loader.Group(spec.Group) (or spec.ID when spec.Group is empty), invokes
// registerFn on the engine goroutine to register assets in the group, and
// loads the group to completion.
//
// FetchGroup blocks until download and group loading finish or ctx (or
// loader's scope) is cancelled, so it should be called off the engine
// goroutine.
//
// The group owns the mounted pack: Group.Reset, Group.SetFS, a later FetchGroup into the same group and disposing loader's scope release it, and with it the cache entry for Remove.
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
			closeFS(packFS)
			doneCh <- loadResult{err: context.Canceled}
			return
		}
		g := loader.Group(groupName)
		// A reused group must forget the previous pack's specs; otherwise its next load reads the old pack's asset list against the new pack. Reset also releases the previous pack.
		g.Reset()
		g.SetFS(packFS)
		g.pack, _ = packFS.(*mountedPack)
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

	limit := d.maxDownload(spec)
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if offset == 0 {
			return false, fmt.Errorf("assets: dlc %q: unexpected 206 Partial Content without Range request", spec.ID)
		}
		// Appending a body whose range the server does not state could splice the wrong bytes onto the partial download, so a 206 without Content-Range starts over (#546).
		cr := resp.Header.Get("Content-Range")
		if cr == "" {
			return true, nil
		}
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
				return false, fmt.Errorf("%w: server total %d != spec size %d", ErrSizeMismatch, total, spec.Size)
			}
			if total > limit {
				_ = d.cache.RemovePart(key)
				return false, fmt.Errorf("%w: server total %d is more than %d", ErrPackTooLarge, total, limit)
			}
			if totalSize < 0 {
				totalSize = total
			}
		}
		appendMode = true
		downloaded = offset

	case http.StatusOK:
		if spec.Size > 0 && resp.ContentLength >= 0 && resp.ContentLength != spec.Size {
			_ = d.cache.RemovePart(key)
			return false, fmt.Errorf("%w: server content-length %d != spec size %d", ErrSizeMismatch, resp.ContentLength, spec.Size)
		}
		if resp.ContentLength > limit {
			_ = d.cache.RemovePart(key)
			return false, fmt.Errorf("%w: server content-length %d is more than %d", ErrPackTooLarge, resp.ContentLength, limit)
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
					_, _, verifyErr := d.verifyPack(r, size, spec)
					closeReader(r)
					if verifyErr == nil {
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
			if downloaded+int64(n) > limit {
				_ = w.Close()
				_ = d.cache.RemovePart(key)
				if spec.Size > 0 {
					return false, fmt.Errorf("%w: download exceeded expected size %d", ErrSizeMismatch, spec.Size)
				}
				return false, fmt.Errorf("%w: download of pack %q without a size passed %d bytes", ErrPackTooLarge, spec.ID, limit)
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

// verifyPack checks the size, SHA-256, signature and ZIP structure of the pack r holds. bytesBad reports a failure of the bytes themselves (their size, their hash, or reading them), which another download could fix, as opposed to a spec or a pack that verifies the same way every time.
func (d *DLCManager) verifyPack(r io.ReaderAt, size int64, spec PackSpec) (zr *zip.Reader, bytesBad bool, err error) {
	if spec.Size > 0 && size != spec.Size {
		return nil, true, fmt.Errorf("%w: got %d bytes, want %d", ErrSizeMismatch, size, spec.Size)
	}
	if size > d.limits.MaxPackBytes {
		return nil, true, fmt.Errorf("%w: pack %q is %d bytes, more than %d", ErrPackTooLarge, spec.ID, size, d.limits.MaxPackBytes)
	}

	expectedHash, err := decodeSHA256(spec.SHA256)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrHashMismatch, err)
	}

	h := sha256.New()
	sr := io.NewSectionReader(r, 0, size)
	buf := make([]byte, d.chunkSize)
	n, err := io.CopyBuffer(h, sr, buf)
	if err != nil {
		return nil, true, fmt.Errorf("%w: read pack: %v", ErrCorruptPack, err)
	}
	if n != size {
		return nil, true, fmt.Errorf("%w: read %d bytes, expected %d", ErrSizeMismatch, n, size)
	}

	actualHash := h.Sum(nil)
	if subtle.ConstantTimeCompare(actualHash, expectedHash) != 1 {
		return nil, true, fmt.Errorf("%w: got %x, want %x", ErrHashMismatch, actualHash, expectedHash)
	}

	if len(d.publicKey) > 0 || strings.TrimSpace(spec.Signature) != "" || !d.allowUnsigned {
		if len(d.publicKey) != ed25519.PublicKeySize {
			return nil, false, fmt.Errorf("%w: public key missing or invalid length (set DLCOptions.AllowUnsigned to accept unsigned packs)", ErrInvalidSignature)
		}
		sigBytes, err := decodeSignature(spec.Signature)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		}
		if !ed25519.Verify(d.publicKey, actualHash, sigBytes) {
			return nil, false, fmt.Errorf("%w: verification failed for pack %q", ErrInvalidSignature, spec.ID)
		}
	}

	zr, err = zip.NewReader(r, size)
	if err != nil {
		return nil, false, fmt.Errorf("%w: invalid zip archive: %v", ErrCorruptPack, err)
	}
	if err := validatePackZip(zr, d.limits); err != nil {
		return nil, false, err
	}
	return zr, false, nil
}

const (
	maxPackEntries       = 1 << 15
	maxPackFileUnpacked  = 256 << 20
	maxPackTotalUnpacked = 1 << 30
	// maxPackArchive leaves room for the headers of a pack that stores maxPackTotalUnpacked bytes uncompressed.
	maxPackArchive = maxPackTotalUnpacked + 64<<20
)

// validatePackZip bounds what a downloaded pack can expand to, so a hostile or
// corrupt archive cannot exhaust memory when an asset is read.
func validatePackZip(zr *zip.Reader, limits PackLimits) error {
	if zr == nil {
		return fmt.Errorf("%w: nil zip archive", ErrCorruptPack)
	}
	limits = limits.withDefaults()
	if len(zr.File) > limits.MaxEntries {
		return fmt.Errorf("%w: %d entries exceeds the %d limit", ErrCorruptPack, len(zr.File), limits.MaxEntries)
	}
	var total uint64
	for _, f := range zr.File {
		if f.UncompressedSize64 > limits.MaxFileBytes {
			return fmt.Errorf("%w: entry %q unpacks to %d bytes, more than %d", ErrCorruptPack, f.Name, f.UncompressedSize64, limits.MaxFileBytes)
		}
		total += f.UncompressedSize64
		if total > limits.MaxTotalBytes {
			return fmt.Errorf("%w: pack unpacks to more than %d bytes", ErrCorruptPack, limits.MaxTotalBytes)
		}
	}
	return nil
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
