//go:build !js

package assets_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func waitGroupDone(t *testing.T, app *nsgotest.App, ch <-chan error) error {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.Advance(16 * time.Millisecond)
		select {
		case err := <-ch:
			return err
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("timed out waiting for asset operation")
	return nil
}

func TestRemoteAssetsEndToEndHTTPAndCacheHit(t *testing.T) {
	atlasJSON := []byte(heroAtlasJSON)
	heroPNG := []byte("fake-remote-hero-png-bytes")
	bgPNG := []byte("fake-remote-bg-png-bytes")
	themeWAV := []byte("fake-remote-theme-wav-bytes")
	levelBin := []byte("remote-level-binary-payload")
	localBin := []byte("local-config-payload")

	entries := []assets.RemoteEntry{
		makeRemoteEntry("asset:sprites/hero.json", "sprites/hero.json", atlasJSON),
		makeRemoteEntry("asset:sprites/hero.png", "sprites/hero.png", heroPNG),
		makeRemoteEntry("asset:images/bg.png", "images/bg.png", bgPNG),
		makeRemoteEntry("asset:music/theme.wav", "music/theme.wav", themeWAV),
		makeRemoteEntry("asset:levels/l1.bin", "levels/l1.bin", levelBin),
	}
	byPath := map[string][]byte{
		"/" + entries[0].Path: atlasJSON,
		"/" + entries[1].Path: heroPNG,
		"/" + entries[2].Path: bgPNG,
		"/" + entries[3].Path: themeWAV,
		"/" + entries[4].Path: levelBin,
	}

	var (
		mu       sync.Mutex
		requests []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		data, ok := byPath[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   entries,
	}
	cacheDir := t.TempDir()
	cache := assets.NewDirRemoteCache(cacheDir)

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx,
			assets.WithFS(fstest.MapFS{"local/cfg.txt": &fstest.MapFile{Data: localBin}}),
			assets.WithRemote(assets.RemoteOptions{
				Manifest: manifest,
				Cache:    cache,
			}),
		)
		mgr.Group("stage1").
			AddAtlas("hero", "asset:sprites/hero.json", core.Src("asset:sprites/hero.png")).
			AddImage("bg", core.Src("asset:images/bg.png")).
			AddSound("theme", audio.Source{URI: "asset:music/theme.wav"}).
			AddBytes("level", "asset:levels/l1.bin").
			AddBytes("local", "asset:local/cfg.txt")
		return core.View{}
	}, nsgotest.Environment(playsAudio), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{
			string(themeWAV): 250 * time.Millisecond,
		}
	})

	done := make(chan error, 1)
	mgr.LoadGroup("stage1", func(err error) { done <- err })
	if err := waitGroupDone(t, app, done); err != nil {
		t.Fatalf("LoadGroup stage1: %v", err)
	}

	// Verify all loaded assets including atlas image Data, image Data, sound, remote bytes, and local fallback.
	if a, ok := mgr.Atlas("hero"); !ok || !bytes.Equal(a.Image.Data, heroPNG) || len(a.Frames) != 2 {
		t.Fatalf("hero atlas not loaded from remote: ok=%v atlas=%+v", ok, a)
	}
	if img, ok := mgr.Image("bg"); !ok || !bytes.Equal(img.Data, bgPNG) {
		t.Fatalf("bg image not loaded from remote: ok=%v len=%d", ok, len(img.Data))
	}
	if snd, ok := mgr.Sound("theme"); !ok || snd == nil {
		t.Fatalf("theme sound not loaded from remote")
	}
	if b, ok := mgr.Bytes("level"); !ok || !bytes.Equal(b, levelBin) {
		t.Fatalf("level bytes mismatch: ok=%v got=%q", ok, b)
	}
	if b, ok := mgr.Bytes("local"); !ok || !bytes.Equal(b, localBin) {
		t.Fatalf("local bytes fallback mismatch: ok=%v got=%q", ok, b)
	}

	mu.Lock()
	firstReqs := slices.Clone(requests)
	mu.Unlock()
	if len(firstReqs) != 5 {
		t.Fatalf("got %d HTTP requests, want 5: %v", len(firstReqs), firstReqs)
	}
	for _, e := range entries {
		if !slices.Contains(firstReqs, "/"+e.Path) {
			t.Errorf("expected request for hashed path /%s, got %v", e.Path, firstReqs)
		}
	}

	// Release and reload -> every file must hit the cache with ZERO HTTP requests.
	mgr.ReleaseGroup("stage1")
	done2 := make(chan error, 1)
	mgr.LoadGroup("stage1", func(err error) { done2 <- err })
	if err := waitGroupDone(t, app, done2); err != nil {
		t.Fatalf("reload stage1 from cache: %v", err)
	}
	mu.Lock()
	afterReqs := len(requests)
	mu.Unlock()
	if afterReqs != 5 {
		t.Fatalf("cache hit reload made %d extra HTTP requests", afterReqs-5)
	}
}

func TestRemoteAssetsRejectsCorruptedDownload(t *testing.T) {
	good := []byte("verified-good-content-123456")
	entryTampered := makeRemoteEntry("asset:tampered.bin", "tampered.bin", good)
	entryShort := makeRemoteEntry("asset:short.bin", "short.bin", good)
	entryLong := makeRemoteEntry("asset:long.bin", "long.bin", good)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + entryTampered.Path:
			bad := bytes.Clone(good)
			bad[0] ^= 0xff // same length, wrong SHA-256
			_, _ = w.Write(bad)
		case "/" + entryShort.Path:
			_, _ = w.Write(good[:len(good)-4])
		case "/" + entryLong.Path:
			_, _ = w.Write(append(bytes.Clone(good), '!', '!'))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := assets.NewDirRemoteCache(t.TempDir())
	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   []assets.RemoteEntry{entryTampered, entryShort, entryLong},
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Cache:    cache,
		}))
		return core.View{}
	})

	cases := []struct {
		uri     string
		wantErr error
	}{
		{entryTampered.URI, assets.ErrHashMismatch},
		{entryShort.URI, assets.ErrSizeMismatch},
		{entryLong.URI, assets.ErrSizeMismatch},
	}
	for _, tc := range cases {
		ch := make(chan error, 1)
		mgr.LoadBytes(tc.uri, func(_ []byte, err error) { ch <- err })
		err := waitGroupDone(t, app, ch)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: got err=%v, want %v", tc.uri, err, tc.wantErr)
		}
	}

	if u := cache.Usage(); u.Files != 0 || u.Bytes != 0 {
		t.Fatalf("corrupted downloads must not enter cache, got %+v", u)
	}
}

func TestRemoteAssetsQuotaEvictionSparesLoadedGroups(t *testing.T) {
	aBytes := bytes.Repeat([]byte("A"), 40)
	bBytes := bytes.Repeat([]byte("B"), 40)
	cBytes := bytes.Repeat([]byte("C"), 40)

	eA := makeRemoteEntry("asset:a.bin", "a.bin", aBytes)
	eB := makeRemoteEntry("asset:b.bin", "b.bin", bBytes)
	eC := makeRemoteEntry("asset:c.bin", "c.bin", cBytes)

	dataByPath := map[string][]byte{
		"/" + eA.Path: aBytes,
		"/" + eB.Path: bBytes,
		"/" + eC.Path: cBytes,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d, ok := dataByPath[r.URL.Path]; ok {
			_, _ = w.Write(d)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Quota fits 2 files (80 bytes), not 3 (120 bytes).
	cache := assets.NewDirRemoteCache(t.TempDir(), 90)
	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   []assets.RemoteEntry{eA, eB, eC},
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Cache:    cache,
		}))
		mgr.Group("gA").AddBytes("a", eA.URI)
		mgr.Group("gB").AddBytes("b", eB.URI)
		mgr.Group("gC").AddBytes("c", eC.URI)
		return core.View{}
	})

	load := func(name string) {
		ch := make(chan error, 1)
		mgr.LoadGroup(name, func(err error) { ch <- err })
		if err := waitGroupDone(t, app, ch); err != nil {
			t.Fatalf("LoadGroup(%q): %v", name, err)
		}
	}

	// Load gA first (oldest), then gB (newer). Release gB so gA is pinned and gB is unpinned.
	load("gA")
	load("gB")
	mgr.ReleaseGroup("gB")

	// Loading gC pushes cache over quota: gA MUST be spared even though it is older than gB; gB MUST be evicted.
	load("gC")

	if !cache.Has(eA.SHA256) {
		t.Fatal("loaded group gA's file was evicted")
	}
	if cache.Has(eB.SHA256) {
		t.Fatal("released group gB's file should have been evicted to stay within quota")
	}
	if !cache.Has(eC.SHA256) {
		t.Fatal("newly loaded group gC's file missing from cache")
	}
	u := mgr.RemoteCache().Usage()
	if u.Bytes != 80 || u.Quota != 90 || u.Files != 2 || u.PinnedBytes != 80 {
		t.Fatalf("unexpected usage with gA+gC loaded: %+v", u)
	}

	// Now release gA (oldest), keep gC loaded, and reload gB -> gA is now evictable and evicted!
	mgr.ReleaseGroup("gA")
	load("gB")
	if cache.Has(eA.SHA256) {
		t.Fatal("released group gA's file should now be evicted")
	}
	if !cache.Has(eB.SHA256) || !cache.Has(eC.SHA256) {
		t.Fatalf("expected gB and gC in cache, got B=%v C=%v", cache.Has(eB.SHA256), cache.Has(eC.SHA256))
	}
}

func TestRemoteAssetsOfflineStartFromPersistedCache(t *testing.T) {
	payload1 := []byte("offline-asset-one")
	payload2 := []byte("offline-asset-two")
	e1 := makeRemoteEntry("asset:one.bin", "one.bin", payload1)
	e2 := makeRemoteEntry("asset:two.bin", "two.bin", payload2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + e1.Path:
			_, _ = w.Write(payload1)
		case "/" + e2.Path:
			_, _ = w.Write(payload2)
		default:
			http.NotFound(w, r)
		}
	}))
	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   []assets.RemoteEntry{e1, e2},
	}

	cacheDir := t.TempDir()

	// Session 1: populate cache while online.
	{
		cache1 := assets.NewDirRemoteCache(cacheDir)
		var mgr1 *assets.Manager
		app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			mgr1 = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
				Manifest: manifest,
				Cache:    cache1,
			}))
			mgr1.Group("boot").AddBytes("one", e1.URI).AddBytes("two", e2.URI)
			return core.View{}
		})
		ch := make(chan error, 1)
		mgr1.LoadGroup("boot", func(err error) { ch <- err })
		if err := waitGroupDone(t, app1, ch); err != nil {
			t.Fatalf("online warm: %v", err)
		}
		if err := cache1.Close(); err != nil {
			t.Fatalf("cache1.Close: %v", err)
		}
	}

	// Shut down origin completely to simulate offline startup.
	srv.Close()

	// Verify index.yaml was persisted on disk.
	if _, err := os.Stat(filepath.Join(cacheDir, "index.yaml")); err != nil {
		t.Fatalf("index.yaml missing after warm: %v", err)
	}

	// Session 2: fresh DirRemoteCache and Manager start completely offline.
	cache2 := assets.NewDirRemoteCache(cacheDir)
	var mgr2 *assets.Manager
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr2 = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Cache:    cache2,
		}))
		mgr2.Group("boot").AddBytes("one", e1.URI).AddBytes("two", e2.URI)
		return core.View{}
	})
	ch := make(chan error, 1)
	mgr2.LoadGroup("boot", func(err error) { ch <- err })
	if err := waitGroupDone(t, app2, ch); err != nil {
		t.Fatalf("offline load failed: %v", err)
	}
	if b, ok := mgr2.Bytes("one"); !ok || !bytes.Equal(b, payload1) {
		t.Fatalf("offline one mismatch: ok=%v got=%q", ok, b)
	}
	if b, ok := mgr2.Bytes("two"); !ok || !bytes.Equal(b, payload2) {
		t.Fatalf("offline two mismatch: ok=%v got=%q", ok, b)
	}
	if u := cache2.Usage(); u.Files != 2 || u.Bytes != int64(len(payload1)+len(payload2)) {
		t.Fatalf("persisted usage mismatch: %+v", u)
	}
}

func TestRemoteAssetsCorruptAndPartialCacheRecovery(t *testing.T) {
	payload := []byte("self-healing-payload-bytes")
	entry := makeRemoteEntry("asset:heal.bin", "heal.bin", payload)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+entry.Path {
			hits.Add(1)
			_, _ = w.Write(payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	filesDir := filepath.Join(cacheDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a corrupted index.yaml, a tampered blob, and a stale .tmp- partial file.
	if err := os.WriteFile(filepath.Join(cacheDir, "index.yaml"), []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, entry.SHA256), []byte("corrupt-bytes-on-disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTmp := filepath.Join(filesDir, ".tmp-leftover-12345")
	if err := os.WriteFile(staleTmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	cache := assets.NewDirRemoteCache(cacheDir)
	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   []assets.RemoteEntry{entry},
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Cache:    cache,
		}))
		return core.View{}
	})

	ch := make(chan error, 1)
	var got []byte
	mgr.LoadBytes(entry.URI, func(b []byte, err error) {
		got = b
		ch <- err
	})
	if err := waitGroupDone(t, app, ch); err != nil {
		t.Fatalf("recovery load failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("recovered payload = %q, want %q", got, payload)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 re-fetch after corrupt blob, got %d", hits.Load())
	}
	if _, err := os.Stat(staleTmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temp file was not cleaned up: %v", err)
	}
	// Subsequent read must hit the repaired cache with zero requests.
	ch2 := make(chan error, 1)
	mgr.LoadBytes(entry.URI, func(b []byte, err error) {
		if err == nil && !bytes.Equal(b, payload) {
			err = errors.New("mismatch")
		}
		ch2 <- err
	})
	if err := waitGroupDone(t, app, ch2); err != nil {
		t.Fatalf("post-recovery cache hit failed: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("post-recovery hit made extra request: hits=%d", hits.Load())
	}
}

func TestRemoteAssetsConcurrentDeduplicationAndSelectiveCancel(t *testing.T) {
	payload := []byte("shared-dedup-content-1234567890")
	entry := makeRemoteEntry("asset:shared.bin", "shared.bin", payload)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var reqCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			_, _ = w.Write(payload)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   []assets.RemoteEntry{entry},
	}
	cache := assets.NewDirRemoteCache(t.TempDir())

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Cache:    cache,
		}))
		mgr.Group("gCancel").AddBytes("shared", entry.URI)
		mgr.Group("gKeep").AddBytes("shared", entry.URI)
		return core.View{}
	})

	keepDone := make(chan error, 1)
	mgr.LoadGroup("gCancel", nil)
	mgr.LoadGroup("gKeep", func(err error) { keepDone <- err })

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for HTTP request to start")
	}

	// Cancel gCancel while gKeep is still waiting: gKeep MUST still succeed without restarting the request.
	mgr.ReleaseGroup("gCancel")
	close(release)

	if err := waitGroupDone(t, app, keepDone); err != nil {
		t.Fatalf("gKeep failed after sibling group cancelled: %v", err)
	}
	if b, ok := mgr.Group("gKeep").Bytes("shared"); !ok || !bytes.Equal(b, payload) {
		t.Fatalf("gKeep bytes mismatch: ok=%v got=%q", ok, b)
	}
	if got := reqCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 deduplicated HTTP request, got %d", got)
	}
}

func TestRemoteAssetsCancelAbortsInFlightFetch(t *testing.T) {
	payload := []byte("slow-fetch-payload")
	entry := makeRemoteEntry("asset:slow.bin", "slow.bin", payload)

	started := make(chan struct{}, 1)
	aborted := make(chan struct{}, 1)

	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    "https://cdn.example.com/game/",
		Files:   []assets.RemoteEntry{entry},
	}
	fetchFn := func(ctx context.Context, _ string) (io.ReadCloser, int64, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		select {
		case aborted <- struct{}{}:
		default:
		}
		return nil, -1, ctx.Err()
	}

	var mgr *assets.Manager
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Fetch:    fetchFn,
		}))
		mgr.Group("slow").AddBytes("slow", entry.URI)
		return core.View{}
	})

	mgr.LoadGroup("slow", nil)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("fetch did not start")
	}

	// Releasing the only waiting group must cancel the underlying fetch context.
	mgr.ReleaseGroup("slow")
	select {
	case <-aborted:
	case <-time.After(3 * time.Second):
		t.Fatal("releasing sole waiting group did not cancel in-flight fetch")
	}
}

func TestRemoteAssetsRetriesTransientErrors(t *testing.T) {
	good := []byte("flaky-but-recovers")
	eFlaky := makeRemoteEntry("asset:flaky.bin", "flaky.bin", good)
	eMissing := makeRemoteEntry("asset:missing.bin", "missing.bin", []byte("nope"))

	var flakyHits, missingHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + eFlaky.Path:
			if flakyHits.Add(1) <= 2 {
				http.Error(w, "temporary unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write(good)
		case "/" + eMissing.Path:
			missingHits.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    srv.URL + "/",
		Files:   []assets.RemoteEntry{eFlaky, eMissing},
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest:     manifest,
			Cache:        assets.NewDirRemoteCache(t.TempDir()),
			Retries:      2,
			RetryBackoff: 5 * time.Millisecond,
		}))
		return core.View{}
	})

	ch := make(chan error, 1)
	var got []byte
	mgr.LoadBytes(eFlaky.URI, func(b []byte, err error) {
		got = b
		ch <- err
	})
	if err := waitGroupDone(t, app, ch); err != nil {
		t.Fatalf("flaky fetch should succeed after 2 retries: %v", err)
	}
	if !bytes.Equal(got, good) || flakyHits.Load() != 3 {
		t.Fatalf("got=%q hits=%d, want 3 attempts", got, flakyHits.Load())
	}

	// 404 must NOT be retried.
	ch404 := make(chan error, 1)
	mgr.LoadBytes(eMissing.URI, func(_ []byte, err error) { ch404 <- err })
	if err := waitGroupDone(t, app, ch404); err == nil {
		t.Fatal("expected error for 404")
	}
	if missingHits.Load() != 1 {
		t.Fatalf("404 was retried: hits=%d, want 1", missingHits.Load())
	}
}

func TestRemoteAssetsReloadAfterCancelStartsFreshFetch(t *testing.T) {
	payload := []byte("reload-after-cancel-payload")
	entry := makeRemoteEntry("asset:reload.bin", "reload.bin", payload)

	firstStarted := make(chan struct{})
	firstCancelled := make(chan struct{})
	firstGate := make(chan struct{})
	var calls atomic.Int32

	manifest := &assets.RemoteManifest{
		Version: assets.RemoteManifestVersion,
		Base:    "https://cdn.example.com/game/",
		Files:   []assets.RemoteEntry{entry},
	}
	// The first fetch returns only after firstGate closes, as a transport that is slow to abort would; every later fetch succeeds.
	fetchFn := func(ctx context.Context, _ string) (io.ReadCloser, int64, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-ctx.Done()
			close(firstCancelled)
			<-firstGate
			return nil, -1, ctx.Err()
		}
		return io.NopCloser(bytes.NewReader(payload)), int64(len(payload)), nil
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithRemote(assets.RemoteOptions{
			Manifest: manifest,
			Fetch:    fetchFn,
		}))
		mgr.Group("g").AddBytes("data", entry.URI)
		return core.View{}
	})

	mgr.LoadGroup("g", nil)
	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("fetch did not start")
	}
	mgr.ReleaseGroup("g")
	select {
	case <-firstCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("releasing the only waiting group did not cancel the fetch")
	}

	// The abandoned fetch is still unwinding; a new load must start its own fetch rather than join it and inherit context.Canceled.
	done := make(chan error, 1)
	mgr.LoadGroup("g", func(err error) { done <- err })
	close(firstGate)
	if err := waitGroupDone(t, app, done); err != nil {
		t.Fatalf("reload after cancel failed: %v", err)
	}
	if b, ok := mgr.Group("g").Bytes("data"); !ok || !bytes.Equal(b, payload) {
		t.Fatalf("bytes mismatch: ok=%v got=%q", ok, b)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("fetch calls = %d, want 2", got)
	}
}
