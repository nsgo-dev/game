//go:build !js

package assets_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func buildTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func newTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	return pub, priv
}

func assertCacheDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read cache dir %q: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected cache dir %q to be empty, found %v", dir, names)
	}
}

func serveRangeBytes(data []byte, onReq func(r *http.Request, start int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		total := int64(len(data))
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			if onReq != nil {
				onReq(r, 0)
			}
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		if !strings.HasPrefix(rangeHdr, "bytes=") {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		startStr, _, _ := strings.Cut(strings.TrimPrefix(rangeHdr, "bytes="), "-")
		start, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || start < 0 || start >= total {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if onReq != nil {
			onReq(r, start)
		}
		chunk := data[start:]
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(chunk)), 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}
}

func TestDLCFetchVerifyMountAndLoadGroup(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	packBytes := buildTestZip(t, map[string][]byte{
		"sprites/hero.json": []byte(heroAtlasJSON),
		"sprites/hero.png":  []byte("fake-png-bytes"),
		"maps/chapter2.dat": []byte("chapter-2-castle"),
		"notes/story.txt":   []byte("welcome to chapter 2"),
	})
	shaHex, sigHex := assets.SignPack(priv, packBytes)

	var reqCount atomic.Int32
	srv := httptest.NewServer(serveRangeBytes(packBytes, func(*http.Request, int64) {
		reqCount.Add(1)
	}))
	defer srv.Close()

	// Verify manifest parsing and base64 signature decoding too.
	sigRaw, err := hex.DecodeString(sigHex)
	if err != nil {
		t.Fatalf("decode hex sig: %v", err)
	}
	manifestJSON, err := json.Marshal(assets.PackManifest{
		Packs: []assets.PackSpec{{
			ID:        "chapter-2",
			Group:     "dlc-chapter-2",
			URL:       srv.URL + "/chapter-2.zip",
			Version:   "1.2.0",
			MinBuild:  10,
			MaxBuild:  20,
			Size:      int64(len(packBytes)),
			SHA256:    shaHex,
			Signature: base64.StdEncoding.EncodeToString(sigRaw),
		}},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	parsedSpecs, err := assets.ParsePackManifest(manifestJSON)
	if err != nil || len(parsedSpecs) != 1 {
		t.Fatalf("ParsePackManifest = %v, %v", parsedSpecs, err)
	}
	spec := parsedSpecs[0]

	cacheDir := t.TempDir()
	cache := assets.NewDirPackCache(cacheDir)
	dlc := assets.NewDLCManager(assets.DLCOptions{
		AppBuild:   15,
		PublicKey:  pub,
		Cache:      cache,
		HTTPClient: srv.Client(),
		ChunkSize:  128,
	})

	var (
		mgr       *assets.Manager
		loopTicks atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l := &game.Loop{
			Update: func(time.Duration) { loopTicks.Add(1) },
		}
		l.Run(cx)
		mgr = assets.NewManager(cx)
		return core.View{}
	}, envWithMedia()...)

	type fetchResult struct {
		group *assets.Group
		err   error
	}
	resCh := make(chan fetchResult, 1)

	var (
		progMu   sync.Mutex
		progress []int64
	)
	go func() {
		g, err := dlc.FetchGroup(context.Background(), mgr, spec, func(g *assets.Group, packFS fs.FS) error {
			pngBytes, err := fs.ReadFile(packFS, "sprites/hero.png")
			if err != nil {
				return err
			}
			g.AddAtlas("hero", "asset:sprites/hero.json", core.ImageSource{Data: pngBytes}).
				AddBytes("story", "asset:notes/story.txt").
				AddCustom("castle", "asset:maps/chapter2.dat", func(b []byte) (any, error) {
					return &fakeMap{Name: string(b)}, nil
				}, nil)
			return nil
		}, func(downloaded, total int64) {
			progMu.Lock()
			progress = append(progress, downloaded)
			progMu.Unlock()
			if total != int64(len(packBytes)) {
				t.Errorf("progress total = %d, want %d", total, len(packBytes))
			}
		})
		resCh <- fetchResult{group: g, err: err}
	}()

	var res fetchResult
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case res = <-resCh:
			return true
		default:
			return false
		}
	})
	if res.err != nil {
		t.Fatalf("FetchGroup failed: %v", res.err)
	}
	if res.group == nil || res.group.Name() != "dlc-chapter-2" {
		t.Fatalf("unexpected group: %#v", res.group)
	}
	if !res.group.Done() || res.group.Progress() != 1 || res.group.FS() == nil {
		t.Fatalf("group state: done=%v progress=%v fs=%v", res.group.Done(), res.group.Progress(), res.group.FS())
	}

	// Verify loaded assets via both Group and Manager.
	atlas, ok := res.group.Atlas("hero")
	if !ok || len(atlas.Frames) != 2 {
		t.Fatalf("atlas = %+v, ok=%v", atlas, ok)
	}
	if mgrAtlas, ok := mgr.Atlas("hero"); !ok || mgrAtlas != atlas {
		t.Fatal("mgr.Atlas(hero) did not return DLC atlas")
	}
	story, ok := res.group.Bytes("story")
	if !ok || string(story) != "welcome to chapter 2" {
		t.Fatalf("story = %q, ok=%v", string(story), ok)
	}
	castleVal, ok := res.group.Get("castle")
	if !ok {
		t.Fatal("castle custom asset missing")
	}
	if fm, ok := castleVal.(*fakeMap); !ok || fm.Name != "chapter-2-castle" {
		t.Fatalf("castle = %#v", castleVal)
	}

	// Verify monotonic progress ending at exact pack size.
	progMu.Lock()
	if len(progress) == 0 || progress[len(progress)-1] != int64(len(packBytes)) {
		t.Fatalf("unexpected progress history: %v", progress)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("progress moved backwards: %v", progress)
		}
	}
	progMu.Unlock()

	// Verify .pack is committed and no .part file remains.
	if !cache.Has(spec.CacheKey()) {
		t.Fatal("expected committed pack in cache")
	}
	if cache.HasPart(spec.CacheKey()) {
		t.Fatal("expected .part file to be removed after commit")
	}
	if reqCount.Load() != 1 {
		t.Fatalf("HTTP requests = %d, want 1", reqCount.Load())
	}

	// Second Fetch uses verified cache with zero network requests.
	cachedFS, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("second Fetch failed: %v", err)
	}
	if reqCount.Load() != 1 {
		t.Fatalf("second Fetch made network request: count=%d, want 1", reqCount.Load())
	}
	gotStory, err := fs.ReadFile(cachedFS, "notes/story.txt")
	if err != nil || string(gotStory) != "welcome to chapter 2" {
		t.Fatalf("cached fs read = %q, %v", string(gotStory), err)
	}
}

func TestDLCRejectsAndRemovesInvalidPacks(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	_, otherPriv := newTestKeyPair(t)
	validZip := buildTestZip(t, map[string][]byte{
		"data/hello.txt": []byte("hello dlc world"),
	})
	validSHA, validSig := assets.SignPack(priv, validZip)

	t.Run("corrupt bytes hash mismatch", func(t *testing.T) {
		corrupt := append([]byte(nil), validZip...)
		corrupt[len(corrupt)/2] ^= 0xff

		srv := httptest.NewServer(serveRangeBytes(corrupt, nil))
		defer srv.Close()

		cacheDir := t.TempDir()
		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   100,
			PublicKey:  pub,
			Cache:      assets.NewDirPackCache(cacheDir),
			HTTPClient: srv.Client(),
		})
		spec := assets.PackSpec{
			ID:        "corrupt-pack",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(validZip)),
			SHA256:    validSHA,
			Signature: validSig,
		}
		_, err := dlc.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrHashMismatch) {
			t.Fatalf("err = %v, want ErrHashMismatch", err)
		}
		assertCacheDirEmpty(t, cacheDir)
	})

	t.Run("size mismatch", func(t *testing.T) {
		truncated := validZip[:len(validZip)-10]
		srv := httptest.NewServer(serveRangeBytes(truncated, nil))
		defer srv.Close()

		cacheDir := t.TempDir()
		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   100,
			PublicKey:  pub,
			Cache:      assets.NewDirPackCache(cacheDir),
			HTTPClient: srv.Client(),
		})
		spec := assets.PackSpec{
			ID:        "short-pack",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(validZip)),
			SHA256:    validSHA,
			Signature: validSig,
		}
		_, err := dlc.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrSizeMismatch) {
			t.Fatalf("err = %v, want ErrSizeMismatch", err)
		}
		assertCacheDirEmpty(t, cacheDir)
	})

	t.Run("bad ed25519 signature", func(t *testing.T) {
		_, wrongSig := assets.SignPack(otherPriv, validZip)
		srv := httptest.NewServer(serveRangeBytes(validZip, nil))
		defer srv.Close()

		cacheDir := t.TempDir()
		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   100,
			PublicKey:  pub,
			Cache:      assets.NewDirPackCache(cacheDir),
			HTTPClient: srv.Client(),
		})
		spec := assets.PackSpec{
			ID:        "bad-sig-pack",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(validZip)),
			SHA256:    validSHA,
			Signature: wrongSig,
		}
		_, err := dlc.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrInvalidSignature) {
			t.Fatalf("err = %v, want ErrInvalidSignature", err)
		}
		assertCacheDirEmpty(t, cacheDir)

		// Missing signature when PublicKey is configured must also fail.
		spec.Signature = ""
		_, err = dlc.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrInvalidSignature) {
			t.Fatalf("missing sig err = %v, want ErrInvalidSignature", err)
		}
		assertCacheDirEmpty(t, cacheDir)
	})

	t.Run("incompatible build", func(t *testing.T) {
		var reqCount atomic.Int32
		srv := httptest.NewServer(serveRangeBytes(validZip, func(*http.Request, int64) {
			reqCount.Add(1)
		}))
		defer srv.Close()

		cacheDir := t.TempDir()
		cache := assets.NewDirPackCache(cacheDir)

		// First cache it with a compatible build, then try fetching with an
		// incompatible build to confirm it is rejected and cleaned up without
		// hitting the network.
		spec := assets.PackSpec{
			ID:        "build-gated",
			URL:       srv.URL,
			Version:   "1.0.0",
			MinBuild:  50,
			MaxBuild:  60,
			Size:      int64(len(validZip)),
			SHA256:    validSHA,
			Signature: validSig,
		}
		okDLC := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   55,
			PublicKey:  pub,
			Cache:      cache,
			HTTPClient: srv.Client(),
		})
		if _, err := okDLC.Fetch(context.Background(), spec, nil); err != nil {
			t.Fatalf("compatible Fetch failed: %v", err)
		}
		if reqCount.Load() != 1 {
			t.Fatalf("reqCount = %d, want 1", reqCount.Load())
		}

		// Too old build -> ErrIncompatibleBuild, zero new HTTP requests, removed from cache.
		oldDLC := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   49,
			PublicKey:  pub,
			Cache:      cache,
			HTTPClient: srv.Client(),
		})
		_, err := oldDLC.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrIncompatibleBuild) {
			t.Fatalf("old build err = %v, want ErrIncompatibleBuild", err)
		}
		if reqCount.Load() != 1 {
			t.Fatalf("incompatible build made HTTP request: count=%d", reqCount.Load())
		}
		assertCacheDirEmpty(t, cacheDir)

		// Too new build -> ErrIncompatibleBuild, zero HTTP requests.
		newDLC := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   61,
			PublicKey:  pub,
			Cache:      cache,
			HTTPClient: srv.Client(),
		})
		_, err = newDLC.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrIncompatibleBuild) {
			t.Fatalf("new build err = %v, want ErrIncompatibleBuild", err)
		}
		if reqCount.Load() != 1 {
			t.Fatalf("new build made HTTP request: count=%d", reqCount.Load())
		}
		assertCacheDirEmpty(t, cacheDir)
	})

	t.Run("invalid zip archive", func(t *testing.T) {
		notAZip := []byte("this is definitely not a zip archive, even though it is properly signed")
		notZipSHA, notZipSig := assets.SignPack(priv, notAZip)

		srv := httptest.NewServer(serveRangeBytes(notAZip, nil))
		defer srv.Close()

		cacheDir := t.TempDir()
		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   100,
			PublicKey:  pub,
			Cache:      assets.NewDirPackCache(cacheDir),
			HTTPClient: srv.Client(),
		})
		spec := assets.PackSpec{
			ID:        "not-a-zip",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(notAZip)),
			SHA256:    notZipSHA,
			Signature: notZipSig,
		}
		_, err := dlc.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrCorruptPack) {
			t.Fatalf("err = %v, want ErrCorruptPack", err)
		}
		assertCacheDirEmpty(t, cacheDir)
	})

	t.Run("tampered cached pack is rejected and removed", func(t *testing.T) {
		var reqCount atomic.Int32
		srv := httptest.NewServer(serveRangeBytes(validZip, func(*http.Request, int64) {
			reqCount.Add(1)
		}))
		defer srv.Close()

		cacheDir := t.TempDir()
		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   100,
			PublicKey:  pub,
			Cache:      assets.NewDirPackCache(cacheDir),
			HTTPClient: srv.Client(),
		})
		spec := assets.PackSpec{
			ID:        "tamper-test",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(validZip)),
			SHA256:    validSHA,
			Signature: validSig,
		}
		if _, err := dlc.Fetch(context.Background(), spec, nil); err != nil {
			t.Fatalf("initial Fetch failed: %v", err)
		}

		// Corrupt the cached .pack file on disk.
		entries, err := os.ReadDir(cacheDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("expected 1 cached file, got %v, %v", entries, err)
		}
		packPath := filepath.Join(cacheDir, entries[0].Name())
		diskBytes, err := os.ReadFile(packPath)
		if err != nil {
			t.Fatalf("read cached pack: %v", err)
		}
		diskBytes[0] ^= 0xff
		if err := os.WriteFile(packPath, diskBytes, 0o644); err != nil {
			t.Fatalf("tamper cached pack: %v", err)
		}

		// Re-verification catches tampering, rejects with ErrHashMismatch, and deletes the file.
		_, err = dlc.Fetch(context.Background(), spec, nil)
		if !errors.Is(err, assets.ErrHashMismatch) {
			t.Fatalf("tampered cache Fetch err = %v, want ErrHashMismatch", err)
		}
		assertCacheDirEmpty(t, cacheDir)

		// Subsequent Fetch cleanly downloads a fresh copy from the server.
		fsys, err := dlc.Fetch(context.Background(), spec, nil)
		if err != nil {
			t.Fatalf("recovery Fetch failed: %v", err)
		}
		if reqCount.Load() != 2 {
			t.Fatalf("reqCount = %d, want 2", reqCount.Load())
		}
		got, err := fs.ReadFile(fsys, "data/hello.txt")
		if err != nil || string(got) != "hello dlc world" {
			t.Fatalf("recovered file = %q, %v", string(got), err)
		}
	})
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

func TestDLCResumesInterruptedDownloadViaRange(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	// Use incompressible random bytes so the ZIP spans many chunks cleanly.
	payload := randomBytes(t, 16*1024)
	packBytes := buildTestZip(t, map[string][]byte{
		"levels/world3.bin": payload,
		"meta/version.txt":  []byte("3.0.0"),
	})
	shaHex, sigHex := assets.SignPack(priv, packBytes)
	cutAt := len(packBytes) / 3

	var (
		mu             sync.Mutex
		rangeHeaders   []string
		bytesSentTotal int64
		attempt        int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt++
		curAttempt := attempt
		rangeHdr := r.Header.Get("Range")
		rangeHeaders = append(rangeHeaders, rangeHdr)
		mu.Unlock()

		total := int64(len(packBytes))
		if curAttempt == 1 {
			// Send Content-Length for the full pack, write only the first chunk,
			// then drop the TCP connection abruptly to simulate network loss.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("ResponseWriter does not implement http.Hijacker")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			conn, rw, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack failed: %v", err)
				return
			}
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nAccept-Ranges: bytes\r\n\r\n", total)
			n, _ := rw.Write(packBytes[:cutAt])
			_ = rw.Flush()
			_ = conn.Close()

			mu.Lock()
			bytesSentTotal += int64(n)
			mu.Unlock()
			return
		}

		// Second attempt MUST send Range: bytes=<cutAt>-
		if !strings.HasPrefix(rangeHdr, "bytes=") {
			t.Errorf("expected Range header on resume, got %q", rangeHdr)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		startStr, _, _ := strings.Cut(strings.TrimPrefix(rangeHdr, "bytes="), "-")
		start, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || start != int64(cutAt) {
			t.Errorf("resume Range start = %d (err=%v), want %d", start, err, cutAt)
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		rest := packBytes[start:]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(rest)), 10))
		w.WriteHeader(http.StatusPartialContent)
		n, _ := w.Write(rest)

		mu.Lock()
		bytesSentTotal += int64(n)
		mu.Unlock()
	}))
	defer srv.Close()

	spec := assets.PackSpec{
		ID:        "world-3",
		Group:     "world-3",
		URL:       srv.URL + "/world-3.zip",
		Version:   "3.0.0",
		MinBuild:  1,
		Size:      int64(len(packBytes)),
		SHA256:    shaHex,
		Signature: sigHex,
	}

	cacheDir := t.TempDir()

	// Launch 1: download is interrupted mid-transfer.
	launch1 := assets.NewDLCManager(assets.DLCOptions{
		AppBuild:   10,
		PublicKey:  pub,
		Cache:      assets.NewDirPackCache(cacheDir),
		HTTPClient: srv.Client(),
		ChunkSize:  1024,
	})
	if _, err := launch1.Fetch(context.Background(), spec, nil); err == nil {
		t.Fatal("expected first Fetch to fail on interrupted connection")
	}

	// Verify .part file was preserved at the exact cut offset and no .pack was committed.
	cacheCheck := assets.NewDirPackCache(cacheDir)
	if cacheCheck.Has(spec.CacheKey()) {
		t.Fatal("partial download should not have committed a .pack file")
	}
	partSize, err := cacheCheck.PartSize(spec.CacheKey())
	if err != nil || partSize != int64(cutAt) {
		t.Fatalf("partSize = %d (err=%v), want %d", partSize, err, cutAt)
	}

	// Launch 2: fresh DLCManager pointing at the same cache directory resumes via 206.
	launch2 := assets.NewDLCManager(assets.DLCOptions{
		AppBuild:   10,
		PublicKey:  pub,
		Cache:      assets.NewDirPackCache(cacheDir),
		HTTPClient: srv.Client(),
		ChunkSize:  1024,
	})
	var firstProgress int64 = -1
	var lastProgress int64
	packFS, err := launch2.Fetch(context.Background(), spec, func(downloaded, total int64) {
		if firstProgress < 0 {
			firstProgress = downloaded
		}
		lastProgress = downloaded
	})
	if err != nil {
		t.Fatalf("resumed Fetch failed: %v", err)
	}

	if firstProgress != int64(cutAt) {
		t.Fatalf("resumed first progress = %d, want %d", firstProgress, cutAt)
	}
	if lastProgress != int64(len(packBytes)) {
		t.Fatalf("resumed final progress = %d, want %d", lastProgress, len(packBytes))
	}

	// Total bytes sent across both launches must equal pack size (no prior bytes re-sent).
	mu.Lock()
	gotRanges := append([]string(nil), rangeHeaders...)
	gotSent := bytesSentTotal
	mu.Unlock()
	if len(gotRanges) != 2 || gotRanges[0] != "" || gotRanges[1] != fmt.Sprintf("bytes=%d-", cutAt) {
		t.Fatalf("Range headers = %v, want [\"\" \"bytes=%d-\"]", gotRanges, cutAt)
	}
	if gotSent != int64(len(packBytes)) {
		t.Fatalf("total bytes sent = %d, want %d (prior bytes were re-downloaded)", gotSent, len(packBytes))
	}

	// Verify mounted ZIP contents and cache cleanliness.
	gotPayload, err := fs.ReadFile(packFS, "levels/world3.bin")
	if err != nil || !bytes.Equal(gotPayload, payload) {
		t.Fatalf("resumed payload mismatch: err=%v len=%d", err, len(gotPayload))
	}
	if !cacheCheck.Has(spec.CacheKey()) || cacheCheck.HasPart(spec.CacheKey()) {
		t.Fatalf("cache state after resume: has=%v hasPart=%v", cacheCheck.Has(spec.CacheKey()), cacheCheck.HasPart(spec.CacheKey()))
	}
}

func TestDLCResumeMemoryCacheAnd200Fallback(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	payload := randomBytes(t, 8*1024)
	packBytes := buildTestZip(t, map[string][]byte{
		"data/mem.bin": payload,
	})
	shaHex, sigHex := assets.SignPack(priv, packBytes)

	t.Run("context cancel resumes on MemoryPackCache", func(t *testing.T) {
		var (
			mu     sync.Mutex
			starts []int64
		)
		srv := httptest.NewServer(serveRangeBytes(packBytes, func(_ *http.Request, start int64) {
			mu.Lock()
			starts = append(starts, start)
			mu.Unlock()
		}))
		defer srv.Close()

		memCache := assets.NewMemoryPackCache()
		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   1,
			PublicKey:  pub,
			Cache:      memCache,
			HTTPClient: srv.Client(),
			ChunkSize:  256,
		})
		spec := assets.PackSpec{
			ID:        "mem-pack",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(packBytes)),
			SHA256:    shaHex,
			Signature: sigHex,
		}

		ctx, cancel := context.WithCancel(context.Background())
		_, err := dlc.Fetch(ctx, spec, func(downloaded, _ int64) {
			if downloaded >= 512 {
				cancel()
			}
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first Fetch err = %v, want context.Canceled", err)
		}
		partSize, err := memCache.PartSize(spec.CacheKey())
		if err != nil || partSize < 512 || partSize >= int64(len(packBytes)) {
			t.Fatalf("unexpected memory partSize = %d, err=%v", partSize, err)
		}

		fsys, err := dlc.Fetch(context.Background(), spec, nil)
		if err != nil {
			t.Fatalf("resumed MemoryPackCache Fetch failed: %v", err)
		}
		got, err := fs.ReadFile(fsys, "data/mem.bin")
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("memory pack content mismatch: err=%v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if len(starts) != 2 || starts[0] != 0 || starts[1] != partSize {
			t.Fatalf("request offsets = %v, want [0 %d]", starts, partSize)
		}
	})

	t.Run("server ignoring Range restarts cleanly on 200 OK", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always ignore Range header and respond with 200 OK full body.
			w.Header().Set("Content-Length", strconv.Itoa(len(packBytes)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(packBytes)
		}))
		defer srv.Close()

		memCache := assets.NewMemoryPackCache()
		spec := assets.PackSpec{
			ID:        "no-range",
			URL:       srv.URL,
			Version:   "1.0.0",
			Size:      int64(len(packBytes)),
			SHA256:    shaHex,
			Signature: sigHex,
		}
		// Seed a partial download before fetching.
		pw, err := memCache.OpenPartWriter(spec.CacheKey(), false)
		if err != nil {
			t.Fatalf("seed part: %v", err)
		}
		_, _ = pw.Write(packBytes[:128])
		_ = pw.Close()

		dlc := assets.NewDLCManager(assets.DLCOptions{
			AppBuild:   1,
			PublicKey:  pub,
			Cache:      memCache,
			HTTPClient: srv.Client(),
		})
		fsys, err := dlc.Fetch(context.Background(), spec, nil)
		if err != nil {
			t.Fatalf("200 OK fallback Fetch failed: %v", err)
		}
		got, err := fs.ReadFile(fsys, "data/mem.bin")
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("content mismatch: err=%v", err)
		}
	})
}
