package assets

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"runtime"
	"testing"
	"time"
)

// cachedPack puts data into a MemoryPackCache as the completed pack of key.
func cachedPack(t *testing.T, key string, data []byte) *MemoryPackCache {
	t.Helper()
	cache := NewMemoryPackCache()
	w, err := cache.OpenPartWriter(key, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if err := cache.CommitPart(key); err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestMountedPackReadDoesNotAllocateTheDeclaredSize(t *testing.T) {
	// The entry holds 5 bytes but its header claims 200 MiB, within the default limits.
	const claimed = 200 << 20
	data := patchedZip(t, claimed)
	sum := sha256.Sum256(data)
	spec := PackSpec{ID: "liar", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	dlc := NewDLCManager(DLCOptions{AllowUnsigned: true, Cache: cachedPack(t, spec.CacheKey(), data)})
	packFS, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = fs.ReadFile(packFS, "asset.txt")
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("ReadFile of an entry shorter than its header succeeded")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 16<<20 {
		t.Fatalf("ReadFile allocated %d bytes for a 5-byte entry claiming %d; want the buffer to follow the bytes read", grew, claimed)
	}
}

func TestMountedPackRefusesBytesPastMaxFileBytes(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("big.txt")
	_, _ = w.Write(bytes.Repeat([]byte("z"), 4096))
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	// Mount without the pre-mount check, as if the header had under-reported the entry.
	dlc := NewDLCManager(DLCOptions{AllowUnsigned: true, Limits: PackLimits{MaxFileBytes: 1024}})
	p := dlc.newPackFS("k", bytes.NewReader(buf.Bytes()), zr)
	defer p.Close()

	if _, err := p.ReadFile("big.txt"); !errors.Is(err, ErrPackTooLarge) {
		t.Fatalf("ReadFile = %v, want ErrPackTooLarge", err)
	}
	f, err := p.Open("big.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	chunk := make([]byte, 512)
	var readErr error
	for readErr == nil {
		_, readErr = f.Read(chunk)
	}
	if !errors.Is(readErr, ErrPackTooLarge) {
		t.Fatalf("streaming Read = %v, want ErrPackTooLarge", readErr)
	}
}

func TestMountedPackListsDirectories(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"maps/a.txt", "maps/b.txt"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(name))
	}
	_ = zw.Close()
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	spec := PackSpec{ID: "dirs", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	dlc := NewDLCManager(DLCOptions{AllowUnsigned: true, Cache: cachedPack(t, spec.CacheKey(), data)})
	packFS, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	entries, err := fs.ReadDir(packFS, "maps")
	if err != nil || len(entries) != 2 {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}
	matches, err := fs.Glob(packFS, "maps/*.txt")
	if err != nil || len(matches) != 2 {
		t.Fatalf("Glob = %v, %v", matches, err)
	}
}

// smallPack returns a one-file ZIP archive and the spec that verifies it.
func smallPack(t *testing.T, id string) ([]byte, PackSpec) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("a"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return data, PackSpec{ID: id, Version: "1", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func (d *DLCManager) mountCount(key string) int {
	d.mountsMu.Lock()
	defer d.mountsMu.Unlock()
	if m := d.mounts[key]; m != nil {
		return m.refs
	}
	return 0
}

func TestUnclosedPackIsReleasedWhenCollected(t *testing.T) {
	data, spec := smallPack(t, "dropped")
	key := spec.CacheKey()
	cache := cachedPack(t, key, data)
	dlc := NewDLCManager(DLCOptions{AllowUnsigned: true, Cache: cache})
	func() {
		packFS, err := dlc.Fetch(context.Background(), spec, nil)
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if _, err := fs.ReadFile(packFS, "a.txt"); err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
	}()
	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// The cleanup runs on the runtime's cleanup goroutine after a collection finds the pack unreachable; yield until it has, within a deadline.
	deadline := time.Now().Add(10 * time.Second)
	for dlc.mountCount(key) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a dropped pack was not released after garbage collection")
		}
		runtime.GC()
		runtime.Gosched()
	}
	if _, _, err := cache.Open(key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cache.Open after the dropped pack was collected = %v, want the pending Remove done", err)
	}
}

func TestFailedFetchKeepsAPendingRemove(t *testing.T) {
	data, spec := smallPack(t, "pending")
	key := spec.CacheKey()
	cache := cachedPack(t, key, data)
	dlc := NewDLCManager(DLCOptions{AppBuild: 1, AllowUnsigned: true, Cache: cache})
	packFS, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	incompatible := spec
	incompatible.MinBuild = 2
	if _, err := dlc.Fetch(context.Background(), incompatible, nil); !errors.Is(err, ErrIncompatibleBuild) {
		t.Fatalf("Fetch of an incompatible spec = %v, want ErrIncompatibleBuild", err)
	}
	if _, _, err := cache.Open(key); err != nil {
		t.Fatalf("cache.Open while mounted = %v, want the pack kept", err)
	}
	_ = packFS.(io.Closer).Close()
	if _, _, err := cache.Open(key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cache.Open after the last mount closed = %v; a failed Fetch must not cancel the pending Remove", err)
	}
}

func TestPendingRemoveIsHeldOffDuringFetch(t *testing.T) {
	data, spec := smallPack(t, "race")
	key := spec.CacheKey()
	cache := cachedPack(t, key, data)
	dlc := NewDLCManager(DLCOptions{AllowUnsigned: true, Cache: cache})
	old, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := dlc.Remove(spec); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// The last old mount closes while a Fetch of the pack is under way (here from its progress callback, after the cached pack was opened): the pack the Fetch mounts must stay in the cache.
	again, err := dlc.Fetch(context.Background(), spec, func(_, _ int64) { _ = old.(io.Closer).Close() })
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, _, err := cache.Open(key); err != nil {
		t.Fatalf("cache.Open after the old mount closed during a Fetch = %v, want the pack kept", err)
	}
	if got, err := fs.ReadFile(again, "a.txt"); err != nil || string(got) != "a" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	_ = again.(io.Closer).Close()
	if _, _, err := cache.Open(key); err != nil {
		t.Fatalf("cache.Open after closing the new mount = %v; the Fetch cancelled the Remove, want the pack kept", err)
	}
}
