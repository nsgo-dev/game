//go:build !js

package assets_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/assets"
)

func TestDLCMountServesVerifiedBytesAfterCacheFileIsReplaced(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"story.txt": []byte("verified story")})
	shaHex := sha256Hex(packBytes)
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()

	cacheDir := t.TempDir()
	dlc := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, Cache: assets.NewDirPackCache(cacheDir), HTTPClient: srv.Client()})
	spec := assets.PackSpec{ID: "pack", URL: srv.URL + "/pack.zip", Size: int64(len(packBytes)), SHA256: shaHex}
	packFS, err := dlc.Fetch(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Swap the committed cache file for an unverified archive of the same size.
	evil := buildTestZip(t, map[string][]byte{"story.txt": []byte("evil  story!!!")})
	matches, _ := filepath.Glob(filepath.Join(cacheDir, "*.pack"))
	if len(matches) != 1 {
		t.Fatalf("cache files = %v, want one .pack", matches)
	}
	tmp := matches[0] + ".swap"
	if err := os.WriteFile(tmp, evil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, matches[0]); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(packFS, "story.txt")
	if err != nil || string(got) != "verified story" {
		t.Fatalf("mounted pack read %q, %v after the cache file was replaced; want the verified bytes", got, err)
	}
}

func TestDLCPackLimitsAreConfigurable(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"a.txt": []byte("a"), "b.txt": []byte("b")})
	shaHex := sha256Hex(packBytes)
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()
	spec := assets.PackSpec{ID: "pack", URL: srv.URL + "/pack.zip", Size: int64(len(packBytes)), SHA256: shaHex}

	strict := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, HTTPClient: srv.Client(), Limits: assets.PackLimits{MaxEntries: 1}})
	if _, err := strict.Fetch(context.Background(), spec, nil); !errors.Is(err, assets.ErrCorruptPack) {
		t.Fatalf("Fetch with MaxEntries 1 = %v, want ErrCorruptPack", err)
	}
	if def := assets.DefaultPackLimits(); def.MaxEntries <= 1 || def.MaxFileBytes == 0 || def.MaxTotalBytes == 0 {
		t.Fatalf("DefaultPackLimits = %+v", def)
	}
	loose := assets.NewDLCManager(assets.DLCOptions{AllowUnsigned: true, HTTPClient: srv.Client()})
	if _, err := loose.Fetch(context.Background(), spec, nil); err != nil {
		t.Fatalf("Fetch with default limits: %v", err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestDLCWithoutPublicKeyFailsClosed(t *testing.T) {
	packBytes := buildTestZip(t, map[string][]byte{"a.txt": []byte("a")})
	srv := httptest.NewServer(serveRangeBytes(packBytes, nil))
	defer srv.Close()
	spec := assets.PackSpec{ID: "pack", URL: srv.URL + "/pack.zip", Size: int64(len(packBytes)), SHA256: sha256Hex(packBytes)}

	strict := assets.NewDLCManager(assets.DLCOptions{HTTPClient: srv.Client()})
	if _, err := strict.Fetch(context.Background(), spec, nil); !errors.Is(err, assets.ErrInvalidSignature) {
		t.Fatalf("Fetch without a key or AllowUnsigned = %v, want ErrInvalidSignature", err)
	}
	dev := assets.NewDLCManager(assets.DLCOptions{HTTPClient: srv.Client(), AllowUnsigned: true})
	if _, err := dev.Fetch(context.Background(), spec, nil); err != nil {
		t.Fatalf("Fetch with AllowUnsigned: %v", err)
	}
}
