package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultRemoteRetryBackoff is the initial wait before the first retry when
// RemoteOptions.Retries > 0 and RemoteOptions.RetryBackoff <= 0.
const DefaultRemoteRetryBackoff = 100 * time.Millisecond

// RemoteOptions configures loading loose asset files from a remote origin by
// content-hashed URL, verified against size and SHA-256 and cached under a
// byte quota.
type RemoteOptions struct {
	// Manifest maps logical asset URIs to content-hashed paths, exact byte
	// sizes and SHA-256 digests. URIs not listed in Manifest fall through to
	// the Manager's local reader.
	Manifest *RemoteManifest
	// Base is the remote origin URL prefix (for example "https://cdn.example.com/game/").
	// When empty, Manifest.Base is used.
	Base string
	// Fetch retrieves url and returns its response body and expected content
	// length (-1 if unknown). The caller closes the returned io.ReadCloser.
	// When nil, net/http with http.DefaultClient is used.
	Fetch func(ctx context.Context, url string) (io.ReadCloser, int64, error)
	// Cache stores verified remote files keyed by SHA-256 and enforces a byte
	// quota with LRU eviction. When nil, a MemoryRemoteCache with
	// DefaultRemoteCacheQuota is used.
	Cache RemoteCache
	// Retries is the number of additional attempts after a transient fetch
	// error (network failure, HTTP 429 or 5xx). Integrity mismatches and 4xx
	// responses are never retried. Zero disables retries.
	Retries int
	// RetryBackoff is the initial delay before the first retry (doubled on
	// each subsequent attempt). When <= 0, DefaultRemoteRetryBackoff is used.
	RetryBackoff time.Duration
}

// WithRemote configures Manager to load any asset URI listed in opts.Manifest
// from a remote origin by its content-hashed URL, verifying exact size and
// SHA-256 and storing verified files in opts.Cache. URIs not present in
// opts.Manifest continue to load from the Manager's local source (WithReadAsset,
// WithFS, or packaged app resources).
func WithRemote(opts RemoteOptions) Option {
	return func(m *Manager) {
		m.remote = newRemoteLoader(opts, m.ctx)
	}
}

// RemoteCache returns the RemoteCache configured via WithRemote, or nil if no
// remote origin is configured. Callers can read Usage() for a settings screen.
func (m *Manager) RemoteCache() RemoteCache {
	if m == nil || m.remote == nil {
		return nil
	}
	return m.remote.cache
}

type remoteLoader struct {
	parentCtx    context.Context
	base         string
	fetch        func(ctx context.Context, url string) (io.ReadCloser, int64, error)
	cache        RemoteCache
	retries      int
	retryBackoff time.Duration
	entries      map[string]RemoteEntry
	initErr      error

	mu       sync.Mutex
	inFlight map[string]*remoteCall
}

type remoteCall struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	data    []byte
	err     error
}

type remoteHTTPStatusError struct {
	statusCode int
	status     string
}

func (e *remoteHTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %s", e.status)
}

func (e *remoteHTTPStatusError) retryable() bool {
	return e.statusCode == http.StatusTooManyRequests || e.statusCode >= 500
}

func newRemoteLoader(opts RemoteOptions, parentCtx context.Context) *remoteLoader {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	cache := opts.Cache
	if cache == nil {
		cache = NewMemoryRemoteCache()
	}
	fetchFn := opts.Fetch
	if fetchFn == nil {
		fetchFn = defaultRemoteFetch
	}
	backoff := opts.RetryBackoff
	if backoff <= 0 {
		backoff = DefaultRemoteRetryBackoff
	}
	retries := opts.Retries
	if retries < 0 {
		retries = 0
	}
	rl := &remoteLoader{
		parentCtx:    parentCtx,
		fetch:        fetchFn,
		cache:        cache,
		retries:      retries,
		retryBackoff: backoff,
		entries:      map[string]RemoteEntry{},
		inFlight:     map[string]*remoteCall{},
	}
	if opts.Manifest == nil {
		return rl
	}
	if err := validateRemoteManifest(opts.Manifest); err != nil {
		rl.initErr = err
		return rl
	}
	base := strings.TrimSpace(opts.Base)
	if base == "" {
		base = strings.TrimSpace(opts.Manifest.Base)
	}
	if base != "" {
		if _, err := parseRemoteBaseURL(base); err != nil {
			rl.initErr = fmt.Errorf("assets: remote base: %w", err)
		}
	}
	rl.base = base
	for _, f := range opts.Manifest.Files {
		rl.entries[normalizeURI(f.URI)] = f
	}
	return rl
}

func (r *remoteLoader) lookup(uri string) (RemoteEntry, bool) {
	if r == nil || len(r.entries) == 0 {
		return RemoteEntry{}, false
	}
	e, ok := r.entries[normalizeURI(uri)]
	return e, ok
}

func (r *remoteLoader) has(uri string) bool {
	_, ok := r.lookup(uri)
	return ok
}

// resolveImageURI picks the remote URI for img at ratio, checking the
// density-selected variant first when Scales are configured and falling back
// to img.URI.
func (r *remoteLoader) resolveImageURI(img core.ImageSource, ratio float32) (string, RemoteEntry, bool) {
	if r == nil || !needsPrefetch(img) {
		return "", RemoteEntry{}, false
	}
	if img.Scales != 0 && ratio > 0 {
		picked := core.PickImageScale(img, ratio)
		if picked.URI != "" {
			if e, ok := r.lookup(picked.URI); ok {
				return picked.URI, e, true
			}
		}
	}
	if e, ok := r.lookup(img.URI); ok {
		return img.URI, e, true
	}
	return "", RemoteEntry{}, false
}

func (r *remoteLoader) flush() {
	if r == nil || r.cache == nil {
		return
	}
	if flusher, ok := r.cache.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
}

func (r *remoteLoader) fetchBytes(ctx context.Context, uri string) ([]byte, error) {
	if r.initErr != nil {
		return nil, r.initErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry, ok := r.lookup(uri)
	if !ok {
		return nil, fmt.Errorf("assets: remote: %q not in manifest: %w", uri, fs.ErrNotExist)
	}

	// 1. Check cache first; verify exact size and SHA-256 so a corrupted or
	// truncated cache entry is purged and re-fetched automatically.
	if cached, err := r.cache.Get(entry.SHA256); err == nil {
		if verifyRemoteBytes(cached, entry) == nil {
			return cached, nil
		}
		_ = r.cache.Remove(entry.SHA256)
	} else if !errors.Is(err, fs.ErrNotExist) {
		// On unexpected cache read errors, fall through to network fetch after
		// attempting to drop the unreadable entry.
		_ = r.cache.Remove(entry.SHA256)
	}

	// 2. Deduplicate concurrent fetches for the same content digest.
	r.mu.Lock()
	call, exists := r.inFlight[entry.SHA256]
	if !exists {
		fetchCtx, cancel := context.WithCancel(r.parentCtx)
		call = &remoteCall{
			done:    make(chan struct{}),
			cancel:  cancel,
			waiters: 1,
		}
		r.inFlight[entry.SHA256] = call
		go r.runCall(fetchCtx, call, entry)
	} else {
		call.waiters++
	}
	r.mu.Unlock()

	select {
	case <-call.done:
		if call.err != nil {
			return nil, call.err
		}
		return slices.Clone(call.data), nil
	case <-ctx.Done():
		r.mu.Lock()
		call.waiters--
		shouldCancel := call.waiters == 0
		r.mu.Unlock()
		if shouldCancel {
			call.cancel()
		}
		return nil, ctx.Err()
	}
}

func (r *remoteLoader) runCall(ctx context.Context, call *remoteCall, entry RemoteEntry) {
	defer func() {
		call.cancel()
		r.mu.Lock()
		if r.inFlight[entry.SHA256] == call {
			delete(r.inFlight, entry.SHA256)
		}
		r.mu.Unlock()
		close(call.done)
	}()

	// Re-check cache in case a racing fetch committed right before we entered.
	if cached, err := r.cache.Get(entry.SHA256); err == nil {
		if verifyRemoteBytes(cached, entry) == nil {
			call.data = cached
			return
		}
		_ = r.cache.Remove(entry.SHA256)
	}

	fullURL, err := RemoteURL(r.base, entry.Path)
	if err != nil {
		call.err = fmt.Errorf("assets: remote %q: %w", entry.URI, err)
		return
	}

	var (
		data    []byte
		lastErr error
	)
	for attempt := 0; attempt <= r.retries; attempt++ {
		if attempt > 0 {
			delay := r.retryBackoff << (attempt - 1)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				call.err = ctx.Err()
				return
			case <-timer.C:
			}
		}
		data, lastErr = r.fetchAndVerifyOnce(ctx, fullURL, entry)
		if lastErr == nil {
			break
		}
		if !isRetryableRemoteError(lastErr) || attempt == r.retries {
			call.err = lastErr
			return
		}
	}

	if err := r.cache.Put(entry.SHA256, data); err != nil {
		call.err = fmt.Errorf("assets: remote %q: cache store: %w", entry.URI, err)
		return
	}
	call.data = data
}

func (r *remoteLoader) fetchAndVerifyOnce(ctx context.Context, fullURL string, entry RemoteEntry) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, contentLen, err := r.fetch(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("assets: remote %q: fetch %s: %w", entry.URI, fullURL, err)
	}
	defer body.Close()

	if contentLen >= 0 && contentLen != entry.Size {
		return nil, fmt.Errorf("assets: remote %q: %w: server content-length %d != manifest size %d",
			entry.URI, ErrSizeMismatch, contentLen, entry.Size)
	}

	var buf bytes.Buffer
	if entry.Size > 0 && entry.Size <= 4<<20 {
		buf.Grow(int(entry.Size))
	}
	h := sha256.New()
	chunk := make([]byte, 32*1024)
	limited := io.LimitReader(body, entry.Size+1)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := limited.Read(chunk)
		if n > 0 {
			total += int64(n)
			if total > entry.Size {
				return nil, fmt.Errorf("assets: remote %q: %w: response exceeded expected size %d",
					entry.URI, ErrSizeMismatch, entry.Size)
			}
			_, _ = buf.Write(chunk[:n])
			_, _ = h.Write(chunk[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("assets: remote %q: read body: %w", entry.URI, readErr)
		}
	}

	if total != entry.Size {
		return nil, fmt.Errorf("assets: remote %q: %w: got %d bytes, want %d",
			entry.URI, ErrSizeMismatch, total, entry.Size)
	}
	expectedHash, err := hex.DecodeString(entry.SHA256)
	if err != nil {
		return nil, fmt.Errorf("assets: remote %q: %w: %v", entry.URI, ErrHashMismatch, err)
	}
	actualHash := h.Sum(nil)
	if subtle.ConstantTimeCompare(actualHash, expectedHash) != 1 {
		return nil, fmt.Errorf("assets: remote %q: %w: got %x, want %s",
			entry.URI, ErrHashMismatch, actualHash, entry.SHA256)
	}
	return buf.Bytes(), nil
}

func verifyRemoteBytes(data []byte, entry RemoteEntry) error {
	if int64(len(data)) != entry.Size {
		return fmt.Errorf("%w: got %d bytes, want %d", ErrSizeMismatch, len(data), entry.Size)
	}
	expectedHash, err := hex.DecodeString(entry.SHA256)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHashMismatch, err)
	}
	actualHash := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(actualHash[:], expectedHash) != 1 {
		return fmt.Errorf("%w: got %x, want %s", ErrHashMismatch, actualHash, entry.SHA256)
	}
	return nil
}

func isRetryableRemoteError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrHashMismatch) || errors.Is(err, ErrSizeMismatch) {
		return false
	}
	var statusErr *remoteHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.retryable()
	}
	return true
}

func defaultRemoteFetch(ctx context.Context, rawURL string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, -1, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, -1, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, -1, &remoteHTTPStatusError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
		}
	}
	return resp.Body, resp.ContentLength, nil
}
