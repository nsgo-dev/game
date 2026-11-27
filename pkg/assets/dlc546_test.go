//go:build !js

package assets_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func cachedPacks(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.pack"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestDLCRemoveWaitsForTheLastMountOfAPack(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"story.txt": []byte("mounted story")})
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()
	cacheDir := t.TempDir()
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: assets.NewDirPackCache(cacheDir), HTTPClient: srv.Client()})
	spec := assets.PackSpec{ID: "pack", URL: srv.URL, Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}

	first, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	second, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := cachedPacks(t, cacheDir); len(got) != 1 {
		t.Fatalf("cache after Remove of a mounted pack = %v, want the pack kept", got)
	}
	if got, err := fs.ReadFile(first, "story.txt"); err != nil || string(got) != "mounted story" {
		t.Fatalf("read after Remove = %q, %v", got, err)
	}

	closer, ok := first.(io.Closer)
	if !ok {
		t.Fatalf("Fetch returned %T, which has no Close", first)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := fs.ReadFile(first, "story.txt"); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("read after Close = %v, want fs.ErrClosed", err)
	}
	if got := cachedPacks(t, cacheDir); len(got) != 1 {
		t.Fatalf("cache with one mount left = %v, want the pack kept", got)
	}
	if err := second.(io.Closer).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := cachedPacks(t, cacheDir); len(got) != 0 {
		t.Fatalf("cache after the last mount closed = %v, want the removed pack gone", got)
	}
}

func TestDLCFetchAfterRemoveKeepsThePack(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"a.txt": []byte("a")})
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()
	cacheDir := t.TempDir()
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: assets.NewDirPackCache(cacheDir), HTTPClient: srv.Client()})
	spec := assets.PackSpec{ID: "pack", URL: srv.URL, Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}

	first, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	again, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch after Remove: %v", err)
	}
	_ = first.(io.Closer).Close()
	_ = again.(io.Closer).Close()
	if got := cachedPacks(t, cacheDir); len(got) != 1 {
		t.Fatalf("cache = %v; a Fetch after Remove wants the pack kept", got)
	}
}

func TestDLCFetchGroupReleasesThePackOnReset(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"notes/story.txt": []byte("chapter story")})
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()
	cacheDir := t.TempDir()
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: assets.NewDirPackCache(cacheDir), HTTPClient: srv.Client()})
	spec := assets.PackSpec{ID: "chapter", URL: srv.URL, Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx)
		return core.View{}
	})
	type result struct {
		g   *assets.Group
		err error
	}
	done := make(chan result, 1)
	go func() {
		g, err := dlc.FetchGroup(context.Background(), mgr, spec, func(g *assets.Group, _ fs.FS) error {
			g.AddBytes("story", "asset:notes/story.txt")
			return nil
		}, nil)
		done <- result{g, err}
	}()
	var res result
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case res = <-done:
			return true
		default:
			return false
		}
	})
	if res.err != nil {
		t.Fatalf("FetchGroup: %v", res.err)
	}
	if b, ok := res.g.Bytes("story"); !ok || string(b) != "chapter story" {
		t.Fatalf("story = %q, %v", b, ok)
	}

	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := cachedPacks(t, cacheDir); len(got) != 1 {
		t.Fatalf("cache while the group is mounted = %v, want the pack kept", got)
	}
	app.Post(func() { res.g.Reset() })
	app.Settle()
	if got := cachedPacks(t, cacheDir); len(got) != 0 {
		t.Fatalf("cache after Group.Reset = %v, want the removed pack gone", got)
	}
}

func TestDLCMalformedSpecKeepsTheCachedPack(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	_, otherPriv := newTestKeyPair(t)
	packBytes := buildTestZip(t, map[string][]byte{"a.txt": []byte("a")})
	shaHex, sigHex := assets.SignPack(priv, packBytes)
	_, otherSig := assets.SignPack(otherPriv, packBytes)
	var requests atomic.Int32
	srv := httptest.NewServer(serveRangeBytes(packBytes, func(*http.Request, int64) { requests.Add(1) }))
	defer srv.Close()
	cacheDir := t.TempDir()
	dlc := assets.NewDLCManager(assets.DLCOptions{PublicKey: pub, Cache: assets.NewDirPackCache(cacheDir), HTTPClient: srv.Client()})
	good := assets.PackSpec{ID: "pack", URL: srv.URL, Size: int64(len(packBytes)), SHA256: shaHex, Signature: sigHex}
	packFS, err := dlc.Fetch(context.Background(), good, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	_ = packFS.(io.Closer).Close()

	for _, tc := range []struct {
		name string
		edit func(*assets.PackSpec)
		want error
	}{
		{"malformed sha256", func(s *assets.PackSpec) { s.SHA256 = "not-hex" }, assets.ErrHashMismatch},
		{"short sha256", func(s *assets.PackSpec) { s.SHA256 = "abcd" }, assets.ErrHashMismatch},
		{"malformed signature", func(s *assets.PackSpec) { s.Signature = "%%%" }, assets.ErrInvalidSignature},
		{"negative size", func(s *assets.PackSpec) { s.Size = -1 }, assets.ErrSizeMismatch},
		{"wrong signature", func(s *assets.PackSpec) { s.Signature = otherSig }, assets.ErrInvalidSignature},
	} {
		spec := good
		tc.edit(&spec)
		if _, err := dlc.Fetch(context.Background(), spec, nil); !errors.Is(err, tc.want) {
			t.Fatalf("%s: Fetch = %v, want %v", tc.name, err, tc.want)
		}
		if got := cachedPacks(t, cacheDir); len(got) != 1 {
			t.Fatalf("%s: cache = %v, want the verified pack kept", tc.name, got)
		}
	}
	if _, err := dlc.Fetch(context.Background(), good, nil); err != nil {
		t.Fatalf("Fetch with the good spec: %v", err)
	}
	if n := requests.Load(); n != 1 {
		t.Fatalf("HTTP requests = %d, want 1: the cached pack should have survived", n)
	}
}

// serveUnsized streams data without a Content-Length.
func serveUnsized(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for chunk := range slices.Chunk(data, 1024) {
			_, _ = w.Write(chunk)
			w.(http.Flusher).Flush()
		}
	}
}

func TestDLCDownloadWithoutSizeStopsAtMaxPackBytes(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 64<<10)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"streamed", serveUnsized(body)},
		{"content-length", serveRangeBytes(body, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			cacheDir := t.TempDir()
			dlc := assets.NewDLCManager(assets.DLCOptions{
				AllowUnsigned: true,
				Cache:         assets.NewDirPackCache(cacheDir),
				HTTPClient:    srv.Client(),
				ChunkSize:     512,
				Limits:        assets.PackLimits{MaxPackBytes: 4096},
			})
			spec := assets.PackSpec{ID: "unsized", URL: srv.URL, SHA256: sha256Hex(body)}
			if _, err := dlc.Fetch(context.Background(), spec, nil); !errors.Is(err, assets.ErrPackTooLarge) {
				t.Fatalf("Fetch = %v, want ErrPackTooLarge", err)
			}
			assertCacheDirEmpty(t, cacheDir)
		})
	}
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Limits: assets.PackLimits{MaxPackBytes: 4096}})
	spec := assets.PackSpec{ID: "big", URL: "http://127.0.0.1:1/never", Size: 5000, SHA256: sha256Hex(body)}
	if _, err := dlc.Fetch(context.Background(), spec, nil); !errors.Is(err, assets.ErrPackTooLarge) {
		t.Fatalf("Fetch of a spec over MaxPackBytes = %v, want ErrPackTooLarge", err)
	}
	if def := assets.DefaultPackLimits(); def.MaxPackBytes < int64(def.MaxTotalBytes) {
		t.Fatalf("DefaultPackLimits().MaxPackBytes = %d, want at least MaxTotalBytes %d", def.MaxPackBytes, def.MaxTotalBytes)
	}
}

func TestDLC206WithoutContentRangeStartsOver(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"a.txt": bytes.Repeat([]byte("abc"), 500)})
	var ranged, full atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// A broken server: 206, but the whole body and no Content-Range.
			ranged.Add(1)
			w.Header().Set("Content-Length", strconv.Itoa(len(packBytes)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(packBytes)
			return
		}
		full.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(packBytes)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(packBytes)
	}))
	defer srv.Close()

	cache := assets.NewMemoryPackCache()
	spec := assets.PackSpec{ID: "pack", URL: srv.URL, Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}
	w, err := cache.OpenPartWriter(spec.CacheKey(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(packBytes[:len(packBytes)/2]); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: cache, HTTPClient: srv.Client()})
	packFS, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch = %v; a 206 without Content-Range should restart the download", err)
	}
	if got, err := fs.ReadFile(packFS, "a.txt"); err != nil || len(got) != 1500 {
		t.Fatalf("read = %d bytes, %v", len(got), err)
	}
	if ranged.Load() != 1 || full.Load() != 1 {
		t.Fatalf("requests: ranged %d, full %d; want one of each", ranged.Load(), full.Load())
	}
}

func TestMemoryPackCacheHoldsAPackOnce(t *testing.T) {
	const size = 8 << 20
	cache := assets.NewMemoryPackCache()
	w, err := cache.OpenPartWriter("pack", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r, n, err := cache.OpenPartReader("pack")
	if err != nil || n != size {
		t.Fatalf("OpenPartReader = %d, %v", n, err)
	}
	if err := cache.CommitPart("pack"); err != nil {
		t.Fatal(err)
	}
	pr, n, err := cache.Open("pack")
	if err != nil || n != size {
		t.Fatalf("Open = %d, %v", n, err)
	}
	runtime.ReadMemStats(&after)
	if grew := after.TotalAlloc - before.TotalAlloc; grew > size/4 {
		t.Fatalf("OpenPartReader, CommitPart and Open allocated %d bytes for an %d-byte pack; want no copies", grew, size)
	}
	runtime.KeepAlive(r)
	runtime.KeepAlive(pr)
}

func BenchmarkMemoryPackCacheCommit(b *testing.B) {
	data := make([]byte, 4<<20)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		cache := assets.NewMemoryPackCache()
		w, _ := cache.OpenPartWriter("pack", false)
		_, _ = w.Write(data)
		_ = w.Close()
		if _, _, err := cache.OpenPartReader("pack"); err != nil {
			b.Fatal(err)
		}
		if err := cache.CommitPart("pack"); err != nil {
			b.Fatal(err)
		}
		if _, _, err := cache.Open("pack"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDirPackCacheMountSurvivesRemoveOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to delete an open file")
	}
	packBytes := buildTestZip(t, map[string][]byte{"a.txt": []byte("kept")})
	cacheDir := t.TempDir()
	cache := assets.NewDirPackCache(cacheDir)
	w, _ := cache.OpenPartWriter("pack", false)
	_, _ = w.Write(packBytes)
	_ = w.Close()
	if err := cache.CommitPart("pack"); err != nil {
		t.Fatal(err)
	}
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: cache})
	packFS, err := dlc.Fetch(context.Background(), assets.PackSpec{ID: "pack", Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}, nil)
	if err != nil {
		t.Fatalf("Fetch from cache: %v", err)
	}
	if err := os.Remove(filepath.Join(cacheDir, "pack.pack")); err != nil {
		t.Fatal(err)
	}
	if got, err := fs.ReadFile(packFS, "a.txt"); err != nil || string(got) != "kept" {
		t.Fatalf("read after the file was deleted = %q, %v", got, err)
	}
}

func TestDLCFetchGroupKeepsThePackWhenRegisterSetsItAgain(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"notes/story.txt": []byte("chapter story")})
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()
	cacheDir := t.TempDir()
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: assets.NewDirPackCache(cacheDir), HTTPClient: srv.Client()})
	spec := assets.PackSpec{ID: "chapter", URL: srv.URL, Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx)
		return core.View{}
	})
	type result struct {
		g   *assets.Group
		err error
	}
	done := make(chan result, 1)
	go func() {
		g, err := dlc.FetchGroup(context.Background(), mgr, spec, func(g *assets.Group, packFS fs.FS) error {
			g.SetFS(packFS) // redundant: FetchGroup already mounted it
			g.AddBytes("story", "asset:notes/story.txt")
			return nil
		}, nil)
		done <- result{g, err}
	}()
	var res result
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case res = <-done:
			return true
		default:
			return false
		}
	})
	if res.err != nil {
		t.Fatalf("FetchGroup: %v", res.err)
	}
	if b, ok := res.g.Bytes("story"); !ok || string(b) != "chapter story" {
		t.Fatalf("story = %q, %v; setting the mounted pack again must not close it", b, ok)
	}
	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := cachedPacks(t, cacheDir); len(got) != 1 {
		t.Fatalf("cache while the group is mounted = %v, want the pack kept", got)
	}
	app.Post(func() { res.g.Reset() })
	app.Settle()
	if got := cachedPacks(t, cacheDir); len(got) != 0 {
		t.Fatalf("cache after Group.Reset = %v, want the removed pack gone", got)
	}
}
