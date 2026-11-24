package assets

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"runtime"
	"testing"
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
