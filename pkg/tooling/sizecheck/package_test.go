package sizecheck_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/sizecheck"
)

func writeStoredZip(t *testing.T, zipPath string, files map[string][]byte) int64 {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		hdr := &zip.FileHeader{
			Name:   name,
			Method: zip.Store,
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return int64(buf.Len())
}

func TestMeasureBuiltArtifactEmptyOutputReportsNotFound(t *testing.T) {
	outDir := t.TempDir()
	for _, plat := range []string{"web", "android", "ios", "macos", "windows", "linux"} {
		m, err := sizecheck.MeasureBuiltArtifact(outDir, "Demo", "dev.example.demo", "1.0.0", plat, nil)
		if err != nil {
			t.Fatalf("MeasureBuiltArtifact(%s): %v", plat, err)
		}
		if m.Found {
			t.Fatalf("MeasureBuiltArtifact(%s) Found = true on empty outDir: %+v", plat, m)
		}
		if m.SizeBytes != 0 || m.Categories.Total() != 0 {
			t.Fatalf("MeasureBuiltArtifact(%s) reported non-zero bytes on empty outDir: %+v", plat, m)
		}
	}
}

func TestMeasureBuiltArtifactWebPackageDirAndWasm(t *testing.T) {
	outDir := t.TempDir()
	pkgDir := sizecheck.PackageDir(outDir, "Dungeon Run", "0.4.0", "web")
	if err := os.MkdirAll(filepath.Join(pkgDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	wasmPayload := append([]byte("\x00asm\x01\x00\x00\x00"), bytes.Repeat([]byte("code-bytes-"), 2048)...)
	jsPayload := []byte("console.log('wasm_exec');")
	pngPayload := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1000)
	oggPayload := bytes.Repeat([]byte("OggS"), 750)
	htmlPayload := []byte("<!doctype html><html></html>")

	writeFile := func(rel string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pkgDir, filepath.FromSlash(rel)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("app.wasm", wasmPayload)
	writeFile("app.wasm.gz", []byte("ignored-sidecar"))
	writeFile("wasm_exec.js", jsPayload)
	writeFile("index.html", htmlPayload)
	writeFile("assets/hero.png", pngPayload)
	writeFile("assets/jump.ogg", oggPayload)

	m, err := sizecheck.MeasureBuiltArtifact(outDir, "Dungeon Run", "dev.example.dungeon", "0.4.0", "web", []string{"web"})
	if err != nil {
		t.Fatalf("MeasureBuiltArtifact(web): %v", err)
	}
	if !m.Found {
		t.Fatal("expected web artifact to be found")
	}
	wantCode := int64(len(wasmPayload) + len(jsPayload))
	wantTex := int64(len(pngPayload))
	wantAudio := int64(len(oggPayload))
	wantOther := int64(len(htmlPayload))
	wantTotal := wantCode + wantTex + wantAudio + wantOther

	if m.SizeBytes != wantTotal {
		t.Fatalf("SizeBytes = %d, want %d", m.SizeBytes, wantTotal)
	}
	if m.Categories.CodeBytes != wantCode || m.Categories.TexturesBytes != wantTex || m.Categories.AudioBytes != wantAudio || m.Categories.OtherBytes != wantOther {
		t.Fatalf("Categories = %+v, want code=%d tex=%d audio=%d other=%d", m.Categories, wantCode, wantTex, wantAudio, wantOther)
	}
	if m.WasmMeasurement == nil || m.WasmMeasurement.RawBytes != int64(len(wasmPayload)) || m.WasmMeasurement.GzipBytes <= 0 {
		t.Fatalf("WasmMeasurement = %+v, want raw=%d and positive gzip", m.WasmMeasurement, len(wasmPayload))
	}
	if m.WebBuildDir != pkgDir {
		t.Fatalf("WebBuildDir = %q, want %q", m.WebBuildDir, pkgDir)
	}
}

func TestMeasureBuiltArtifactAndroidAABFixture(t *testing.T) {
	outDir := t.TempDir()
	aabPath := filepath.Join(outDir, "releases", sizecheck.ReleaseName("Dungeon Run", "dev.example.dungeon", "0.4.0", "android_arm64", ".aab"))
	libBytes := bytes.Repeat([]byte{0x7f, 'E', 'L', 'F'}, 2000)
	dexBytes := bytes.Repeat([]byte("dex\n"), 500)
	pngBytes := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1200)
	oggBytes := bytes.Repeat([]byte("OggS"), 600)
	manifestBytes := []byte("<manifest/>")

	archiveBytes := writeStoredZip(t, aabPath, map[string][]byte{
		"base/lib/arm64-v8a/libgame.so":     libBytes,
		"base/dex/classes.dex":              dexBytes,
		"base/assets/sprites/hero.png":      pngBytes,
		"base/assets/sfx/jump.ogg":          oggBytes,
		"base/manifest/AndroidManifest.xml": manifestBytes,
	})

	m, err := sizecheck.MeasureBuiltArtifact(outDir, "Dungeon Run", "dev.example.dungeon", "0.4.0", "android", []string{"android_arm64"})
	if err != nil {
		t.Fatalf("MeasureBuiltArtifact(android): %v", err)
	}
	if !m.Found || m.Kind != sizecheck.PackageKindAAB {
		t.Fatalf("unexpected measurement: %+v", m)
	}
	if m.ArtifactPath != aabPath || m.SizeBytes != archiveBytes {
		t.Fatalf("artifact = (%q, %d), want (%q, %d)", m.ArtifactPath, m.SizeBytes, aabPath, archiveBytes)
	}
	wantCode := int64(len(libBytes) + len(dexBytes))
	wantTex := int64(len(pngBytes))
	wantAudio := int64(len(oggBytes))
	if m.Categories.CodeBytes != wantCode || m.Categories.TexturesBytes != wantTex || m.Categories.AudioBytes != wantAudio {
		t.Fatalf("Categories = %+v, want code=%d tex=%d audio=%d", m.Categories, wantCode, wantTex, wantAudio)
	}
	if m.Categories.Total() != archiveBytes {
		t.Fatalf("Categories.Total() = %d, want archiveBytes %d", m.Categories.Total(), archiveBytes)
	}
	if m.MobileReport == nil || m.MobileReport.ABIs["arm64-v8a"].CompressedBytes != int64(len(libBytes)) {
		t.Fatalf("MobileReport missing ABI breakdown: %+v", m.MobileReport)
	}
}

func TestMeasureBuiltArtifactDesktopPackageDir(t *testing.T) {
	outDir := t.TempDir()
	pkgDir := sizecheck.PackageDir(outDir, "Demo", "1.0.0", "macos_arm64")
	binPath := filepath.Join(pkgDir, "Demo.app", "Contents", "MacOS", "Demo")
	texPath := filepath.Join(pkgDir, "Demo.app", "Contents", "Resources", "hero.png")
	sfxPath := filepath.Join(pkgDir, "Demo.app", "Contents", "Resources", "jump.wav")
	plistPath := filepath.Join(pkgDir, "Demo.app", "Contents", "Info.plist")

	for p, data := range map[string][]byte{
		binPath:   bytes.Repeat([]byte{0xcf, 0xfa, 0xed, 0xfe}, 1500),
		texPath:   bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 800),
		sfxPath:   bytes.Repeat([]byte("RIFF"), 400),
		plistPath: []byte("<plist/>"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m, err := sizecheck.MeasureBuiltArtifact(outDir, "Demo", "dev.example.demo", "1.0.0", "macos", []string{"macos_arm64"})
	if err != nil {
		t.Fatalf("MeasureBuiltArtifact(macos): %v", err)
	}
	if !m.Found {
		t.Fatal("expected macos package dir to be found")
	}
	if m.Categories.CodeBytes != 6000 || m.Categories.TexturesBytes != 3200 || m.Categories.AudioBytes != 1600 || m.Categories.OtherBytes != 8 {
		t.Fatalf("unexpected desktop categories: %+v", m.Categories)
	}
	if m.SizeBytes != 6000+3200+1600+8 {
		t.Fatalf("SizeBytes = %d, want %d", m.SizeBytes, 6000+3200+1600+8)
	}
}
