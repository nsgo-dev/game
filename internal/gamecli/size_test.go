package gamecli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/sizecheck"
)

// sizeTestEntry is one file of an archive sizeTestZip writes, stored uncompressed so its size in the archive is len(data).
type sizeTestEntry struct {
	name string
	data []byte
}

// sizeTestZip writes the archive dir/name holding entries and returns its path.
func sizeTestZip(t *testing.T, dir, name string, entries []sizeTestEntry) string {
	t.Helper()
	p := filepath.Join(dir, name)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// sizeTestBytes returns n bytes of filler.
func sizeTestBytes(n int) []byte {
	return bytes.Repeat([]byte{0x5a}, n)
}

func TestSizeExitCodesAndOutputModes(t *testing.T) {
	dir := t.TempDir()
	aabPath := sizeTestZip(t, dir, "sample.aab", []sizeTestEntry{
		{"base/manifest/AndroidManifest.xml", sizeTestBytes(1024)},
		{"base/dex/classes.dex", sizeTestBytes(2048)},
		{"base/lib/arm64-v8a/libnsgo.so", sizeTestBytes(16384)},
		{"base/lib/armeabi-v7a/libnsgo.so", sizeTestBytes(10240)},
		{"base/assets/sprites/hero.png", sizeTestBytes(8192)},
	})
	apkPath := sizeTestZip(t, dir, "sample.apk", []sizeTestEntry{
		{"lib/arm64-v8a/libgame.so", sizeTestBytes(20000)},
		{"lib/arm64-v8a/libaudio.so", sizeTestBytes(5000)},
		{"lib/armeabi-v7a/libgame.so", sizeTestBytes(14000)},
		{"classes.dex", sizeTestBytes(6000)},
		{"AndroidManifest.xml", sizeTestBytes(1200)},
	})
	ipaPath := sizeTestZip(t, dir, "sample.ipa", []sizeTestEntry{
		{"Payload/SpaceQuest.app/Info.plist", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>SpaceQuestBin</string>
</dict>
</plist>`)},
		{"Payload/SpaceQuest.app/SpaceQuestBin", sizeTestBytes(32768)},
		{"Payload/SpaceQuest.app/assets/hero.png", sizeTestBytes(8192)},
	})

	// An AAB within the default budget, as Markdown, exits 0.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"size", "-kind", "aab", "-markdown", aabPath}, a); code != 0 {
		t.Fatalf("aab markdown exit = %d, want 0; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "## Android AAB Size Budget — PASS") || !strings.Contains(stdout.String(), "`arm64-v8a`") {
		t.Fatalf("unexpected aab markdown output:\n%s", stdout)
	}

	// An APK within the default budget, as JSON, exits 0 and writes the report.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"size", "-kind", "apk", "-json", "-file", apkPath}, a); code != 0 {
		t.Fatalf("apk json exit = %d, want 0; stderr=%s", code, stderr)
	}
	var apkRep sizecheck.MobilePackageReport
	if err := json.Unmarshal(stdout.Bytes(), &apkRep); err != nil {
		t.Fatalf("unmarshal apk json: %v\n%s", err, stdout)
	}
	if !apkRep.Passed || apkRep.ABIs["arm64-v8a"].CompressedBytes != 25000 {
		t.Fatalf("unexpected apk json report: %+v", apkRep)
	}

	// An IPA's kind is inferred from its extension.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"size", ipaPath}, a); code != 0 {
		t.Fatalf("ipa auto-detect exit = %d, want 0; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "## iOS IPA Size Budget — PASS") {
		t.Fatalf("unexpected ipa output:\n%s", stdout)
	}

	// Over the total budget set by -budget-mb exits 1 after writing the breakdown.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"size", "-kind", "aab", "-budget-mb", "0.01", "-markdown", aabPath}, a); code != 1 {
		t.Fatalf("over-budget aab exit = %d, want 1; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "## Android AAB Size Budget — FAIL") || !strings.Contains(stdout.String(), "### Budget Violations") {
		t.Fatalf("expected failure breakdown in markdown:\n%s", stdout)
	}
	if !strings.Contains(stderr.String(), "game size: aab size budget exceeded") {
		t.Fatalf("over-budget aab stderr = %q, want the budget failure", stderr)
	}

	// Over the per-ABI budget set by -abi-budget-mb exits 1, and -json output stays valid JSON.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"size", "-kind", "apk", "-abi-budget-mb", "0.02", "-json", apkPath}, a); code != 1 {
		t.Fatalf("over-abi-budget apk exit = %d, want 1; stderr=%s", code, stderr)
	}
	var failRep sizecheck.MobilePackageReport
	if err := json.Unmarshal(stdout.Bytes(), &failRep); err != nil {
		t.Fatalf("over-budget -json output must remain valid JSON: %v\n%s", err, stdout)
	}
	if failRep.Passed || len(failRep.Violations) == 0 {
		t.Fatalf("expected failed json report with violations, got %+v", failRep)
	}

	// A WebAssembly binary passes with -markdown and fails with -json.
	wasmPath := filepath.Join(dir, "app.wasm")
	if err := os.WriteFile(wasmPath, sizeTestBytes(8192), 0o644); err != nil {
		t.Fatal(err)
	}
	wasmBudgetPath := filepath.Join(dir, "web-budget.json")
	if err := os.WriteFile(wasmBudgetPath, []byte(`{"target":"./internal/sizeprobe","max_raw_bytes":16384,"max_gzip_bytes":16384}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"size", "-kind", "wasm", "-budget", wasmBudgetPath, "-wasm", wasmPath, "-markdown"}, a); code != 0 || !strings.Contains(stdout.String(), "## WebAssembly Size Budget — PASS") {
		t.Fatalf("wasm markdown pass: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	a, stdout, _ = newTestApp(t)
	if code := run([]string{"size", "-kind", "wasm", "-budget", wasmBudgetPath, "-wasm", wasmPath, "-budget-mb", "0.001", "-json"}, a); code != 1 {
		t.Fatalf("wasm over-budget exit = %d, want 1", code)
	}
	var wasmJSON struct {
		Kind   sizecheck.PackageKind `json:"kind"`
		Passed bool                  `json:"passed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &wasmJSON); err != nil || wasmJSON.Kind != sizecheck.PackageKindWasm || wasmJSON.Passed {
		t.Fatalf("unexpected wasm json fail output: err=%v parsed=%+v raw=%s", err, wasmJSON, stdout)
	}

	// A wrong call exits 2, a package that cannot be read exits 1.
	corruptPath := filepath.Join(dir, "corrupt.aab")
	if err := os.WriteFile(corruptPath, []byte("not-a-zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	errorCases := []struct {
		name string
		args []string
		want int
	}{
		{"invalid kind", []string{"-kind", "dmg", aabPath}, 2},
		{"missing path", []string{"-kind", "aab"}, 2},
		{"json and markdown conflict", []string{"-kind", "aab", "-json", "-markdown", aabPath}, 2},
		{"negative budget-mb", []string{"-kind", "aab", "-budget-mb", "-1", aabPath}, 2},
		{"two packages", []string{aabPath, apkPath}, 2},
		{"unknown extension", []string{filepath.Join(dir, "game.zip")}, 2},
		{"missing file", []string{"-kind", "ipa", filepath.Join(dir, "missing.ipa")}, 1},
		{"corrupt archive", []string{"-kind", "aab", corruptPath}, 1},
		{"missing budget", []string{"-kind", "wasm", "-budget", filepath.Join(dir, "missing.json"), "-wasm", wasmPath}, 1},
	}
	for _, tc := range errorCases {
		a, _, stderr := newTestApp(t)
		if got := run(append([]string{"size"}, tc.args...), a); got != tc.want {
			t.Errorf("%s: exit = %d, want %d (stderr=%q)", tc.name, got, tc.want, stderr)
		}
	}
}
