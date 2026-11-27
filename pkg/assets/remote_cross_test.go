package assets_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func remoteSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func makeRemoteEntry(uri, relPath string, data []byte) assets.RemoteEntry {
	sum := remoteSHA256Hex(data)
	return assets.RemoteEntry{
		URI:    uri,
		Path:   assets.HashedPath(relPath, sum),
		Size:   int64(len(data)),
		SHA256: sum,
	}
}

func TestHashedPath(t *testing.T) {
	hash := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	cases := []struct {
		rel  string
		want string
	}{
		{"music/theme.m4a", "music/theme.9f86d081.m4a"},
		{"hero.png", "hero.9f86d081.png"},
		{"sprites/hero@2x.png", "sprites/hero@2x.9f86d081.png"},
		{"scenes/level1.scene.yaml", "scenes/level1.scene.9f86d081.yaml"},
		{"data/README", "data/README.9f86d081"},
		{".keep", ".keep.9f86d081"},
		{"dir/.keep", "dir/.keep.9f86d081"},
	}
	for _, tc := range cases {
		if got := assets.HashedPath(tc.rel, hash); got != tc.want {
			t.Errorf("HashedPath(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestRemoteURL(t *testing.T) {
	u, err := assets.RemoteURL("https://cdn.example.com/game", "music/boss theme.9f86d081.m4a")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if want := "https://cdn.example.com/game/music/boss%20theme.9f86d081.m4a"; u != want {
		t.Fatalf("RemoteURL = %q, want %q", u, want)
	}
	u2, err := assets.RemoteURL("https://cdn.example.com/game/", "hero.9f86d081.png")
	if err != nil {
		t.Fatalf("RemoteURL with trailing slash: %v", err)
	}
	if want := "https://cdn.example.com/game/hero.9f86d081.png"; u2 != want {
		t.Fatalf("RemoteURL = %q, want %q", u2, want)
	}

	badCases := []struct {
		base, rel string
	}{
		{"", "a.png"},
		{"ftp://cdn.example.com/game/", "a.png"},
		{"https://cdn.example.com/game?token=1", "a.png"},
		{"https://cdn.example.com/game#frag", "a.png"},
		{"https://cdn.example.com/game/", "../secret.txt"},
		{"https://cdn.example.com/game/", "/abs.txt"},
		{"https://cdn.example.com/game/", `a\b.png`},
		{"https://cdn.example.com/game/", ""},
	}
	for _, tc := range badCases {
		if got, err := assets.RemoteURL(tc.base, tc.rel); err == nil {
			t.Errorf("RemoteURL(%q, %q) succeeded with %q, want error", tc.base, tc.rel, got)
		}
	}
}

func TestParseRemoteManifestValidation(t *testing.T) {
	validHash := remoteSHA256Hex([]byte("hello"))
	validYAML := "version: 1\nbase: https://cdn.example.com/game/\nfiles:\n  - uri: asset:b.txt\n    path: b." + validHash[:8] + ".txt\n    size: 5\n    sha256: " + validHash + "\n  - uri: asset:a.txt\n    path: a." + validHash[:8] + ".txt\n    size: 5\n    sha256: " + validHash + "\n"

	m, err := assets.ParseRemoteManifest([]byte(validYAML))
	if err != nil {
		t.Fatalf("ParseRemoteManifest valid: %v", err)
	}
	encoded, err := assets.EncodeRemoteManifest(m)
	if err != nil {
		t.Fatalf("EncodeRemoteManifest: %v", err)
	}
	m2, err := assets.ParseRemoteManifest(encoded)
	if err != nil {
		t.Fatalf("re-parse encoded: %v", err)
	}
	if len(m2.Files) != 2 || m2.Files[0].URI != "asset:a.txt" || m2.Files[1].URI != "asset:b.txt" {
		t.Fatalf("files not sorted canonically: %+v", m2.Files)
	}
	if e, ok := m2.Lookup("asset:/a.txt"); !ok || e.URI != "asset:a.txt" {
		t.Fatalf("Lookup normalized failed: %+v, %v", e, ok)
	}

	badDocs := []struct {
		name string
		src  string
	}{
		{"empty", ""},
		{"wrong version", "version: 2\n"},
		{"missing version", "base: https://cdn.example.com/\n"},
		{"unknown field", "version: 1\nextra: true\n"},
		{"trailing doc", "version: 1\n---\nversion: 1\n"},
		{"bad base scheme", "version: 1\nbase: file:///tmp\n"},
		{"duplicate normalized uri", "version: 1\nfiles:\n  - uri: asset:a.txt\n    path: a.txt\n    size: 1\n    sha256: " + validHash + "\n  - uri: asset:/a.txt\n    path: b.txt\n    size: 1\n    sha256: " + validHash + "\n"},
		{"path traversal", "version: 1\nfiles:\n  - uri: asset:a.txt\n    path: ../a.txt\n    size: 1\n    sha256: " + validHash + "\n"},
		{"negative size", "version: 1\nfiles:\n  - uri: asset:a.txt\n    path: a.txt\n    size: -1\n    sha256: " + validHash + "\n"},
		{"uppercase sha256", "version: 1\nfiles:\n  - uri: asset:a.txt\n    path: a.txt\n    size: 1\n    sha256: " + strings.ToUpper(validHash) + "\n"},
		{"short sha256", "version: 1\nfiles:\n  - uri: asset:a.txt\n    path: a.txt\n    size: 1\n    sha256: deadbeef\n"},
	}
	for _, tc := range badDocs {
		if _, err := assets.ParseRemoteManifest([]byte(tc.src)); err == nil {
			t.Errorf("%s: ParseRemoteManifest succeeded, want error", tc.name)
		}
	}
}

func TestMemoryRemoteCacheQuotaAndPinning(t *testing.T) {
	cache := assets.NewMemoryRemoteCache(100)
	d1 := bytes.Repeat([]byte("a"), 40)
	d2 := bytes.Repeat([]byte("b"), 40)
	d3 := bytes.Repeat([]byte("c"), 40)
	h1, h2, h3 := remoteSHA256Hex(d1), remoteSHA256Hex(d2), remoteSHA256Hex(d3)

	if err := cache.Put(h1, d1); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(h2, d2); err != nil {
		t.Fatal(err)
	}
	// Pin h1 (oldest) so eviction must skip h1 and evict h2 instead.
	cache.Pin(h1)
	if err := cache.Put(h3, d3); err != nil {
		t.Fatal(err)
	}
	if !cache.Has(h1) || cache.Has(h2) || !cache.Has(h3) {
		t.Fatalf("pin-aware LRU failed: h1=%v h2=%v h3=%v", cache.Has(h1), cache.Has(h2), cache.Has(h3))
	}
	u := cache.Usage()
	if u.Bytes != 80 || u.Quota != 100 || u.Files != 2 || u.PinnedBytes != 40 {
		t.Fatalf("unexpected usage: %+v", u)
	}

	// Mutating returned slice must not mutate cache contents.
	got, err := cache.Get(h1)
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 'X'
	gotAgain, err := cache.Get(h1)
	if err != nil || !bytes.Equal(gotAgain, d1) {
		t.Fatalf("cache bytes mutated through returned slice")
	}

	// Clear must spare pinned h1 and remove unpinned h3.
	cache.Clear()
	if !cache.Has(h1) || cache.Has(h3) {
		t.Fatalf("Clear did not spare pinned entry: h1=%v h3=%v", cache.Has(h1), cache.Has(h3))
	}
	cache.Unpin(h1)
	cache.Clear()
	if cache.Has(h1) || cache.Usage().Bytes != 0 {
		t.Fatalf("Clear after Unpin did not empty cache: %+v", cache.Usage())
	}
}

func TestWithRemoteCustomFetchCrossPlatform(t *testing.T) {
	payload := []byte("remote-level-bytes-v1")
	entry := makeRemoteEntry("asset:levels/l1.bin", "levels/l1.bin", payload)
	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    "https://cdn.example.com/game/",
		Files:   []assets.RemoteEntry{entry},
	}
	cache := assets.NewMemoryRemoteCache(1024)

	var (
		fetches  atomic.Int32
		failNext atomic.Bool
		gate     = make(chan struct{})
	)
	fetchFn := func(ctx context.Context, rawURL string) (io.ReadCloser, int64, error) {
		fetches.Add(1)
		if failNext.Load() {
			return nil, -1, errors.New("offline")
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, -1, ctx.Err()
		}
		return io.NopCloser(bytes.NewReader(payload)), int64(len(payload)), nil
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Fetch:    fetchFn,
			Cache:    cache,
		}))
		return core.View{}
	})

	// Launch two concurrent LoadBytes calls for the same remote URI -> single Fetch.
	var (
		wg      sync.WaitGroup
		results = make([][]byte, 2)
		errs    = make([]error, 2)
	)
	wg.Add(2)
	for i := range 2 {
		idx := i
		mgr.LoadBytes(entry.URI, func(b []byte, err error) {
			results[idx] = b
			errs[idx] = err
			wg.Done()
		})
	}
	close(gate)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.Advance(16 * time.Millisecond)
		select {
		case <-done:
			goto finished
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("timed out waiting for concurrent remote loads")

finished:
	for i := range 2 {
		if errs[i] != nil || !bytes.Equal(results[i], payload) {
			t.Fatalf("load %d: err=%v len=%d", i, errs[i], len(results[i]))
		}
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("concurrent loads issued %d fetches, want 1", got)
	}

	// Simulate offline mode: fetch fails, cache hit serves without calling fetchFn.
	failNext.Store(true)
	hitCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	mgr.LoadBytes(entry.URI, func(b []byte, err error) {
		if err != nil {
			errCh <- err
			return
		}
		hitCh <- b
	})
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.Advance(16 * time.Millisecond)
		select {
		case err := <-errCh:
			t.Fatalf("offline cache hit failed: %v", err)
		case b := <-hitCh:
			if !bytes.Equal(b, payload) {
				t.Fatalf("offline payload mismatch: got %q", b)
			}
			goto offlineOK
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("timed out waiting for offline cache hit")

offlineOK:
	if got := fetches.Load(); got != 1 {
		t.Fatalf("offline hit triggered a fetch: total=%d, want 1", got)
	}
}
