package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testZipEntry struct {
	name   string
	data   []byte
	method uint16 // zip.Store or zip.Deflate (0 defaults to zip.Store)
	isDir  bool
}

func writeTestZip(t *testing.T, dst string, entries []testZipEntry) {
	t.Helper()
	f, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		if e.isDir {
			if _, err := zw.Create(e.name); err != nil {
				_ = zw.Close()
				_ = f.Close()
				t.Fatalf("create dir %s: %v", e.name, err)
			}
			continue
		}
		hdr := &zip.FileHeader{
			Name:   e.name,
			Method: e.method,
		}
		if hdr.Method == 0 {
			hdr.Method = zip.Store
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatalf("create header %s: %v", e.name, err)
		}
		if _, err := w.Write(e.data); err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatalf("write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("close zip %s: %v", dst, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file %s: %v", dst, err)
	}
}

func syntheticBytes(seed byte, n int) []byte {
	out := make([]byte, n)
	x := uint32(seed)*7919 + 0x9e3779b9
	for i := range out {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		out[i] = byte(x)
	}
	return out
}

func buildSampleAAB(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "sample.aab")
	writeTestZip(t, p, []testZipEntry{
		{name: "base/assets/", isDir: true},
		{name: "BundleConfig.pb", data: bytes.Repeat([]byte("c"), 128), method: zip.Store},
		{name: "META-INF/MANIFEST.MF", data: bytes.Repeat([]byte("m"), 256), method: zip.Store},
		{name: "BUNDLE-METADATA/com.android.tools.build.obfuscation/proguard.map", data: bytes.Repeat([]byte("p"), 512), method: zip.Store},
		{name: "base/manifest/AndroidManifest.xml", data: bytes.Repeat([]byte("x"), 1024), method: zip.Store},
		{name: "base/native.pb", data: bytes.Repeat([]byte("n"), 64), method: zip.Store},
		{name: "base/dex/classes.dex", data: bytes.Repeat([]byte("dex-bytecode-0123456789"), 200), method: zip.Deflate},
		{name: "base/dex/classes2.dex", data: syntheticBytes(1, 2048), method: zip.Store},
		{name: "base/lib/arm64-v8a/libnsgo.so", data: syntheticBytes(2, 16384), method: zip.Store},
		{name: "base/lib/armeabi-v7a/libnsgo.so", data: syntheticBytes(3, 10240), method: zip.Store},
		{name: "base/lib/x86_64/libnsgo.so", data: syntheticBytes(4, 12288), method: zip.Store},
		{name: "base/assets/sprites/hero.png", data: syntheticBytes(5, 8192), method: zip.Store},
		{name: "base/assets/maps/level1.tmj", data: syntheticBytes(6, 4096), method: zip.Store},
		{name: "base/res/drawable-xxhdpi/ic_launcher.png", data: syntheticBytes(7, 3072), method: zip.Store},
		{name: "base/resources.pb", data: syntheticBytes(8, 1024), method: zip.Store},
		{name: "base/root/kotlin/kotlin.kotlin_builtins", data: syntheticBytes(9, 768), method: zip.Store},
	})
	return p
}

func buildSampleAPK(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "sample.apk")
	writeTestZip(t, p, []testZipEntry{
		{name: "lib/arm64-v8a/libgame.so", data: syntheticBytes(10, 20000), method: zip.Store},
		{name: "lib/arm64-v8a/libaudio.so", data: syntheticBytes(11, 5000), method: zip.Store},
		{name: "lib/armeabi-v7a/libgame.so", data: syntheticBytes(12, 14000), method: zip.Store},
		{name: "lib/x86_64/libgame.so", data: syntheticBytes(13, 18000), method: zip.Store},
		{name: "classes.dex", data: syntheticBytes(14, 6000), method: zip.Store},
		{name: "classes2.dex", data: syntheticBytes(15, 2500), method: zip.Store},
		{name: "assets/atlases/world.png", data: syntheticBytes(16, 12000), method: zip.Store},
		{name: "assets/audio/bgm.ogg", data: syntheticBytes(17, 9000), method: zip.Store},
		{name: "res/mipmap-xxxhdpi/ic_launcher.png", data: syntheticBytes(18, 3500), method: zip.Store},
		{name: "resources.arsc", data: syntheticBytes(19, 1500), method: zip.Store},
		{name: "AndroidManifest.xml", data: syntheticBytes(20, 1200), method: zip.Store},
		{name: "META-INF/CERT.SF", data: syntheticBytes(21, 800), method: zip.Store},
		{name: "DebugProbesKt.bin", data: syntheticBytes(22, 900), method: zip.Store},
	})
	return p
}

func buildSampleIPA(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "sample.ipa")
	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>dev.nsgo.spacequest</string>
	<key>CFBundleExecutable</key>
	<string>SpaceQuestBin</string>
</dict>
</plist>`

	writeTestZip(t, p, []testZipEntry{
		{name: "Payload/SpaceQuest.app/", isDir: true},
		{name: "Payload/SpaceQuest.app/Info.plist", data: []byte(infoPlist), method: zip.Store},
		{name: "Payload/SpaceQuest.app/SpaceQuestBin", data: syntheticBytes(30, 32768), method: zip.Store},
		{name: "Payload/SpaceQuest.app/Frameworks/GameRuntime.framework/GameRuntime", data: syntheticBytes(31, 16384), method: zip.Store},
		{name: "Payload/SpaceQuest.app/Frameworks/libextra.dylib", data: syntheticBytes(32, 4096), method: zip.Store},
		{name: "Payload/SpaceQuest.app/Assets.car", data: syntheticBytes(33, 12288), method: zip.Store},
		{name: "Payload/SpaceQuest.app/assets/hero.png", data: syntheticBytes(34, 8192), method: zip.Store},
		{name: "Payload/SpaceQuest.app/shaders/default.metallib", data: syntheticBytes(35, 2048), method: zip.Store},
		{name: "Payload/SpaceQuest.app/en.lproj/Localizable.strings", data: syntheticBytes(36, 1024), method: zip.Store},
		{name: "Payload/SpaceQuest.app/zh-Hans.lproj/Localizable.strings", data: syntheticBytes(37, 1536), method: zip.Store},
		{name: "Payload/SpaceQuest.app/_CodeSignature/CodeResources", data: syntheticBytes(38, 2048), method: zip.Store},
		{name: "Payload/SpaceQuest.app/embedded.mobileprovision", data: syntheticBytes(39, 1024), method: zip.Store},
		{name: "Payload/SpaceQuest.app/PkgInfo", data: []byte("APPL????"), method: zip.Store},
		{name: "iTunesMetadata.plist", data: syntheticBytes(40, 512), method: zip.Store},
	})
	return p
}

func TestInspectAndroidAABBreakdown(t *testing.T) {
	aabPath := buildSampleAAB(t, t.TempDir())
	rep, err := InspectMobilePackage(aabPath, PackageKindAAB)
	if err != nil {
		t.Fatalf("InspectMobilePackage(aab): %v", err)
	}

	if rep.Kind != PackageKindAAB {
		t.Fatalf("Kind = %q, want %q", rep.Kind, PackageKindAAB)
	}
	if rep.FileCount != 15 {
		t.Fatalf("FileCount = %d, want 15 (dir entries must be skipped)", rep.FileCount)
	}
	if rep.ArchiveBytes <= rep.CompressedBytes {
		t.Fatalf("expected ArchiveBytes (%d) > CompressedBytes (%d) due to zip headers", rep.ArchiveBytes, rep.CompressedBytes)
	}

	// Verify deflated classes.dex has compressed < uncompressed.
	dex := rep.Categories[CategoryDEX]
	wantDexUncomp := int64(len("dex-bytecode-0123456789")*200 + 2048)
	if dex.FileCount != 2 || dex.UncompressedBytes != wantDexUncomp {
		t.Fatalf("dex breakdown = %+v, want FileCount=2 UncompressedBytes=%d", dex, wantDexUncomp)
	}
	if dex.CompressedBytes >= dex.UncompressedBytes || dex.CompressedBytes <= 2048 {
		t.Fatalf("expected deflated dex compressed bytes between 2049 and %d, got %d", dex.UncompressedBytes-1, dex.CompressedBytes)
	}

	// Verify exact stored categories.
	wantStored := map[string]struct {
		bytes int64
		files int
	}{
		CategoryNativeLibs:   {bytes: 16384 + 10240 + 12288, files: 3},
		CategoryAssets:       {bytes: 8192 + 4096, files: 2},
		CategoryResources:    {bytes: 3072 + 1024, files: 2},
		CategoryManifestMeta: {bytes: 128 + 256 + 512 + 1024 + 64, files: 5},
		CategoryOther:        {bytes: 768, files: 1},
	}
	for cat, want := range wantStored {
		got := rep.Categories[cat]
		if got.FileCount != want.files || got.CompressedBytes != want.bytes || got.UncompressedBytes != want.bytes {
			t.Errorf("category %q = %+v, want files=%d bytes=%d", cat, got, want.files, want.bytes)
		}
	}

	// Verify per-ABI breakdown.
	wantABIs := map[string]int64{
		"arm64-v8a":   16384,
		"armeabi-v7a": 10240,
		"x86_64":      12288,
	}
	if len(rep.ABIs) != len(wantABIs) {
		t.Fatalf("ABIs len = %d, want %d (%+v)", len(rep.ABIs), len(wantABIs), rep.ABIs)
	}
	for abi, wantBytes := range wantABIs {
		got := rep.ABIs[abi]
		if got.FileCount != 1 || got.CompressedBytes != wantBytes || got.UncompressedBytes != wantBytes {
			t.Errorf("ABI %q = %+v, want files=1 bytes=%d", abi, got, wantBytes)
		}
	}

	// Largest files capped at DefaultTopLargestFiles and sorted descending.
	if len(rep.LargestFiles) != DefaultTopLargestFiles {
		t.Fatalf("LargestFiles len = %d, want %d", len(rep.LargestFiles), DefaultTopLargestFiles)
	}
	if rep.LargestFiles[0].Path != "base/lib/arm64-v8a/libnsgo.so" || rep.LargestFiles[0].CompressedBytes != 16384 {
		t.Fatalf("LargestFiles[0] = %+v, want arm64-v8a/libnsgo.so (16384 B)", rep.LargestFiles[0])
	}
	if rep.LargestFiles[1].Path != "base/lib/x86_64/libnsgo.so" {
		t.Fatalf("LargestFiles[1] = %+v, want x86_64/libnsgo.so", rep.LargestFiles[1])
	}
}

func TestInspectMultiABIAndroidAPKBreakdown(t *testing.T) {
	apkPath := buildSampleAPK(t, t.TempDir())
	rep, err := InspectMobilePackage(apkPath, PackageKindAPK)
	if err != nil {
		t.Fatalf("InspectMobilePackage(apk): %v", err)
	}

	wantCats := map[string]struct {
		bytes int64
		files int
	}{
		CategoryNativeLibs:   {bytes: 20000 + 5000 + 14000 + 18000, files: 4},
		CategoryDEX:          {bytes: 6000 + 2500, files: 2},
		CategoryAssets:       {bytes: 12000 + 9000, files: 2},
		CategoryResources:    {bytes: 3500 + 1500, files: 2},
		CategoryManifestMeta: {bytes: 1200 + 800, files: 2},
		CategoryOther:        {bytes: 900, files: 1},
	}
	for cat, want := range wantCats {
		got := rep.Categories[cat]
		if got.FileCount != want.files || got.CompressedBytes != want.bytes || got.UncompressedBytes != want.bytes {
			t.Errorf("APK category %q = %+v, want files=%d bytes=%d", cat, got, want.files, want.bytes)
		}
	}

	// arm64-v8a sums both libgame.so and libaudio.so.
	arm64 := rep.ABIs["arm64-v8a"]
	if arm64.FileCount != 2 || arm64.CompressedBytes != 25000 {
		t.Fatalf("arm64-v8a = %+v, want files=2 bytes=25000", arm64)
	}
	if rep.ABIs["armeabi-v7a"].CompressedBytes != 14000 {
		t.Fatalf("armeabi-v7a = %+v, want 14000", rep.ABIs["armeabi-v7a"])
	}
	if rep.ABIs["x86_64"].CompressedBytes != 18000 {
		t.Fatalf("x86_64 = %+v, want 18000", rep.ABIs["x86_64"])
	}
}

func TestInspectIOSIPABreakdown(t *testing.T) {
	ipaPath := buildSampleIPA(t, t.TempDir())
	rep, err := InspectMobilePackage(ipaPath, PackageKindIPA)
	if err != nil {
		t.Fatalf("InspectMobilePackage(ipa): %v", err)
	}

	if len(rep.ABIs) != 0 {
		t.Fatalf("expected no Android ABIs for IPA, got %+v", rep.ABIs)
	}

	// Info.plist is counted under resources along with hero.png and default.metallib.
	infoPlistLen := rep.Categories[CategoryResources].CompressedBytes - (8192 + 2048)
	if infoPlistLen <= 0 {
		t.Fatalf("expected Info.plist to be counted in resources, got %+v", rep.Categories[CategoryResources])
	}

	wantExact := map[string]struct {
		bytes int64
		files int
	}{
		CategoryExecutable:    {bytes: 32768, files: 1},
		CategoryFrameworks:    {bytes: 16384 + 4096, files: 2},
		CategoryAssetsCar:     {bytes: 12288, files: 1},
		CategoryLproj:         {bytes: 1024 + 1536, files: 2},
		CategoryCodeSignature: {bytes: 2048 + 1024, files: 2},
		CategoryOther:         {bytes: 8 + 512, files: 2},
	}
	for cat, want := range wantExact {
		got := rep.Categories[cat]
		if got.FileCount != want.files || got.CompressedBytes != want.bytes || got.UncompressedBytes != want.bytes {
			t.Errorf("IPA category %q = %+v, want files=%d bytes=%d", cat, got, want.files, want.bytes)
		}
	}
	if rep.Categories[CategoryResources].FileCount != 3 {
		t.Errorf("IPA resources FileCount = %d, want 3", rep.Categories[CategoryResources].FileCount)
	}

	// Also verify fallback to <AppName> stem when Info.plist is absent.
	stemIPA := filepath.Join(t.TempDir(), "stem.ipa")
	writeTestZip(t, stemIPA, []testZipEntry{
		{name: "Payload/Probe.app/Probe", data: syntheticBytes(50, 4096), method: zip.Store},
	})
	stemRep, err := InspectMobilePackage(stemIPA, PackageKindIPA)
	if err != nil {
		t.Fatalf("InspectMobilePackage(stem ipa): %v", err)
	}
	if stemRep.Categories[CategoryExecutable].CompressedBytes != 4096 {
		t.Fatalf("stem executable = %+v, want 4096 B", stemRep.Categories[CategoryExecutable])
	}
}

func TestCheckMobilePackageBudgetPassAndFailures(t *testing.T) {
	dir := t.TempDir()
	aabPath := buildSampleAAB(t, dir)
	apkPath := buildSampleAPK(t, dir)
	ipaPath := buildSampleIPA(t, dir)

	// Under-budget passes with default budgets.
	if rep, err := CheckMobilePackage(aabPath, PackageKindAAB, DefaultAndroidAABBudget()); err != nil || !rep.Passed {
		t.Fatalf("AAB default budget failed: err=%v rep=%+v", err, rep)
	}
	if rep, err := CheckMobilePackage(apkPath, PackageKindAPK, DefaultAndroidAPKBudget()); err != nil || !rep.Passed {
		t.Fatalf("APK default budget failed: err=%v rep=%+v", err, rep)
	}
	if rep, err := CheckMobilePackage(ipaPath, PackageKindIPA, DefaultIOSIPABudget()); err != nil || !rep.Passed {
		t.Fatalf("IPA default budget failed: err=%v rep=%+v", err, rep)
	}

	// 1. Over total budget failure.
	rep, err := CheckMobilePackage(aabPath, PackageKindAAB, MobileBudget{
		MaxTotalBytes: 10 * 1024,
	})
	var budgetErr *BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected *BudgetExceededError for total budget, got %v", err)
	}
	if rep.Passed || len(rep.Violations) != 1 {
		t.Fatalf("expected 1 violation, got passed=%v violations=%v", rep.Passed, rep.Violations)
	}
	errMsg := err.Error()
	for _, want := range []string{"total Android AAB archive size", "### Category Breakdown", "### Native Libraries by ABI", "### Largest Files"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("BudgetExceededError missing %q:\n%s", want, errMsg)
		}
	}

	// 2. Over per-ABI budget failure (only arm64-v8a exceeds 20000 B in sample APK;
	// arm64-v8a is 25000 B while x86_64 is 18000 B and armeabi-v7a is 14000 B).
	rep, err = CheckMobilePackage(apkPath, PackageKindAPK, MobileBudget{
		MaxTotalBytes:  10 * bytesPerMiB,
		MaxPerABIBytes: 20000,
	})
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected *BudgetExceededError for per-ABI budget, got %v", err)
	}
	if len(rep.Violations) != 1 || !strings.Contains(rep.Violations[0], `ABI "arm64-v8a"`) {
		t.Fatalf("expected single arm64-v8a violation, got %v", rep.Violations)
	}
	if strings.Contains(rep.Violations[0], "armeabi-v7a") || strings.Contains(rep.Violations[0], "x86_64") {
		t.Fatalf("unexpected non-violating ABI in violation: %s", rep.Violations[0])
	}
	md := FormatMobileReport(rep)
	if !strings.Contains(md, "(OVER)") {
		t.Fatalf("expected (OVER) marker in Markdown report:\n%s", md)
	}

	// 3. Over per-category budget failure on iOS IPA (executable=32768, frameworks=20480).
	rep, err = CheckMobilePackage(ipaPath, PackageKindIPA, MobileBudget{
		MaxTotalBytes: 10 * bytesPerMiB,
		MaxCategoryBytes: map[string]int64{
			CategoryExecutable: 16384,
			CategoryFrameworks: 10000,
			CategoryLproj:      10000,
		},
	})
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected *BudgetExceededError for category budget, got %v", err)
	}
	if len(rep.Violations) != 2 {
		t.Fatalf("expected 2 category violations (executable & frameworks), got %v", rep.Violations)
	}
	if !strings.Contains(err.Error(), `category "executable"`) || !strings.Contains(err.Error(), `category "frameworks"`) {
		t.Fatalf("expected executable and frameworks category violations in error:\n%s", err.Error())
	}
}

func TestDefaultBudgetsIsolationAndLoadMobileBudget(t *testing.T) {
	b1 := DefaultAndroidAABBudget()
	b1.MaxCategoryBytes[CategoryDEX] = 1
	b2 := DefaultAndroidAABBudget()
	if b2.MaxCategoryBytes[CategoryDEX] == 1 {
		t.Fatal("mutating DefaultAndroidAABBudget() affected subsequent calls")
	}

	if DefaultAndroidAPKBudget().MaxPerABIBytes <= 0 {
		t.Fatal("expected positive default APK per-ABI budget")
	}
	if DefaultIOSIPABudget().MaxTotalBytes <= 0 {
		t.Fatal("expected positive default IPA total budget")
	}

	budgetFile := filepath.Join(t.TempDir(), "mobile-budget.json")
	raw := `{"max_total_bytes": 10485760, "max_per_abi_bytes": 5242880, "max_category_bytes": {"dex": 1048576}}`
	if err := os.WriteFile(budgetFile, []byte(raw), 0o644); err != nil {
		t.Fatalf("write budget file: %v", err)
	}
	loaded, err := LoadMobileBudget(budgetFile)
	if err != nil {
		t.Fatalf("LoadMobileBudget: %v", err)
	}
	if loaded.MaxTotalBytes != 10485760 || loaded.MaxPerABIBytes != 5242880 || loaded.MaxCategoryBytes[CategoryDEX] != 1048576 {
		t.Fatalf("unexpected loaded mobile budget: %+v", loaded)
	}

	// Verify checked-in CI mobile budget files load cleanly.
	for _, name := range []string{"android-aab-budget.json", "ios-ipa-budget.json"} {
		p := filepath.Join("..", "..", "benchmarks", name)
		b, err := LoadMobileBudget(p)
		if err != nil {
			t.Fatalf("LoadMobileBudget(%s): %v", p, err)
		}
		if b.MaxTotalBytes <= 0 {
			t.Fatalf("%s: expected positive max_total_bytes, got %+v", name, b)
		}
	}
}

func TestCLIExitCodesAndOutputModes(t *testing.T) {
	dir := t.TempDir()
	aabPath := buildSampleAAB(t, dir)
	apkPath := buildSampleAPK(t, dir)
	ipaPath := buildSampleIPA(t, dir)

	// 1. AAB pass with -markdown -> exit 0.
	var stdout, stderr bytes.Buffer
	code := run([]string{"-kind", "aab", "-markdown", aabPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("aab markdown exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "## Android AAB Size Budget — PASS") || !strings.Contains(stdout.String(), "`arm64-v8a`") {
		t.Fatalf("unexpected aab markdown output:\n%s", stdout.String())
	}

	// 2. APK pass with -json -> exit 0 and valid JSON.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-kind", "apk", "-json", "-file", apkPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("apk json exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	var apkRep MobilePackageReport
	if err := json.Unmarshal(stdout.Bytes(), &apkRep); err != nil {
		t.Fatalf("unmarshal apk json: %v\n%s", err, stdout.String())
	}
	if !apkRep.Passed || apkRep.ABIs["arm64-v8a"].CompressedBytes != 25000 {
		t.Fatalf("unexpected apk json report: %+v", apkRep)
	}

	// 3. IPA pass with inferred kind from extension -> exit 0.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{ipaPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ipa auto-detect exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "## iOS IPA Size Budget — PASS") {
		t.Fatalf("unexpected ipa output:\n%s", stdout.String())
	}

	// 4. Over total budget via -budget-mb -> exit 1.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-kind", "aab", "-budget-mb", "0.01", "-markdown", aabPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("over-budget aab exit = %d, want 1; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "## Android AAB Size Budget — FAIL") || !strings.Contains(stdout.String(), "### Budget Violations") {
		t.Fatalf("expected failure breakdown in markdown:\n%s", stdout.String())
	}

	// 5. Over per-ABI budget via -abi-budget-mb -> exit 1, valid JSON when -json is set.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-kind", "apk", "-abi-budget-mb", "0.02", "-json", apkPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("over-abi-budget apk exit = %d, want 1; stderr=%s", code, stderr.String())
	}
	var failRep MobilePackageReport
	if err := json.Unmarshal(stdout.Bytes(), &failRep); err != nil {
		t.Fatalf("over-budget -json output must remain valid JSON: %v\n%s", err, stdout.String())
	}
	if failRep.Passed || len(failRep.Violations) == 0 {
		t.Fatalf("expected failed json report with violations, got %+v", failRep)
	}

	// 6. Wasm CLI pass and fail with -kind wasm -markdown and -json.
	wasmPath := filepath.Join(dir, "app.wasm")
	if err := os.WriteFile(wasmPath, syntheticBytes(99, 8192), 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	wasmBudgetPath := filepath.Join(dir, "web-budget.json")
	if err := os.WriteFile(wasmBudgetPath, []byte(`{"target":"./internal/sizeprobe","max_raw_bytes":16384,"max_gzip_bytes":16384}`), 0o644); err != nil {
		t.Fatalf("write wasm budget: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-kind", "wasm", "-budget", wasmBudgetPath, "-wasm", wasmPath, "-markdown"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "## WebAssembly Size Budget — PASS") {
		t.Fatalf("wasm markdown pass: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-kind", "wasm", "-budget", wasmBudgetPath, "-wasm", wasmPath, "-budget-mb", "0.001", "-json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("wasm over-budget exit = %d, want 1", code)
	}
	var wasmJSON struct {
		Kind   PackageKind `json:"kind"`
		Passed bool        `json:"passed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &wasmJSON); err != nil || wasmJSON.Kind != PackageKindWasm || wasmJSON.Passed {
		t.Fatalf("unexpected wasm json fail output: err=%v parsed=%+v raw=%s", err, wasmJSON, stdout.String())
	}

	// 7. Usage and I/O errors -> exit 2.
	corruptPath := filepath.Join(dir, "corrupt.aab")
	if err := os.WriteFile(corruptPath, []byte("not-a-zip"), 0o644); err != nil {
		t.Fatalf("write corrupt aab: %v", err)
	}
	errorCases := []struct {
		name string
		args []string
	}{
		{name: "invalid kind", args: []string{"-kind", "dmg", aabPath}},
		{name: "missing path", args: []string{"-kind", "aab"}},
		{name: "missing file", args: []string{"-kind", "ipa", filepath.Join(dir, "missing.ipa")}},
		{name: "corrupt archive", args: []string{"-kind", "aab", corruptPath}},
		{name: "json and markdown conflict", args: []string{"-kind", "aab", "-json", "-markdown", aabPath}},
		{name: "negative budget-mb", args: []string{"-kind", "aab", "-budget-mb", "-1", aabPath}},
	}
	for _, tc := range errorCases {
		stdout.Reset()
		stderr.Reset()
		if got := run(tc.args, &stdout, &stderr); got != 2 {
			t.Errorf("%s: exit = %d, want 2 (stderr=%q)", tc.name, got, stderr.String())
		}
	}
}
