package sizecheck

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// CategoryTotals aggregates real measured package bytes into four top-level
// buckets displayed by the editor's Build and Release screen and release reports.
type CategoryTotals struct {
	CodeBytes     int64 `json:"code_bytes"`
	TexturesBytes int64 `json:"textures_bytes"`
	AudioBytes    int64 `json:"audio_bytes"`
	OtherBytes    int64 `json:"other_bytes"`
}

// Total returns the sum of all four category totals.
func (c CategoryTotals) Total() int64 {
	return c.CodeBytes + c.TexturesBytes + c.AudioBytes + c.OtherBytes
}

// ArtifactMeasurement is the result of finding and measuring a real package
// written by `nsgo build` under a release output directory.
// When no package exists for the requested platform, Found is false and all
// byte counts are zero.
type ArtifactMeasurement struct {
	Found           bool                 `json:"found"`
	ArtifactPath    string               `json:"artifact_path,omitempty"`
	ArtifactName    string               `json:"artifact_name,omitempty"`
	Kind            PackageKind          `json:"kind,omitempty"`
	SizeBytes       int64                `json:"size_bytes"`
	WasmMeasurement *Measurement         `json:"wasm_measurement,omitempty"`
	MobileReport    *MobilePackageReport `json:"mobile_report,omitempty"`
	Categories      CategoryTotals       `json:"categories"`
	WebBuildDir     string               `json:"web_build_dir,omitempty"`
}

// PackageSlug returns NSGo's package file slug for an app (matching NSGo's
// cmd/nsgo/installer.go packageSlug): lowercase ASCII words from appName joined
// by hyphens, else the last dot-separated segment of appID, else "app".
func PackageSlug(appName, appID string) string {
	if s := asciiSlug(appName); s != "" {
		return s
	}
	id := appID
	if i := strings.LastIndexByte(id, '.'); i >= 0 {
		id = id[i+1:]
	}
	if s := asciiSlug(id); s != "" {
		return s
	}
	return "app"
}

func asciiSlug(s string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
		} else {
			hyphen = true
		}
	}
	return b.String()
}

// ReleaseName returns NSGo's release file name for target and suffix:
// `<slug>-<version>-<target-with-hyphens><suffix>` (matching cmd/nsgo/installer.go).
func ReleaseName(appName, appID, appVersion, target, suffix string) string {
	return PackageSlug(appName, appID) + "-" + appVersion + "-" + strings.ReplaceAll(target, "_", "-") + suffix
}

// PackageDir returns NSGo's per-target package directory under outDir:
// `<outDir>/<appName>-<appVersion>-<target>` (matching cmd/nsgo/build.go).
func PackageDir(outDir, appName, appVersion, target string) string {
	return filepath.Join(outDir, fmt.Sprintf("%s-%s-%s", appName, appVersion, target))
}

// MeasureBuiltArtifact locates the package `nsgo build` wrote under outDir for
// platform (and its concrete targets) and measures its real bytes and categories.
// If no built artifact exists in outDir, it returns ArtifactMeasurement{Found: false}, nil.
func MeasureBuiltArtifact(outDir, appName, appID, appVersion, platform string, targets []string) (ArtifactMeasurement, error) {
	if strings.TrimSpace(outDir) == "" {
		return ArtifactMeasurement{}, nil
	}
	if _, err := os.Stat(outDir); err != nil {
		return ArtifactMeasurement{}, nil
	}
	expanded := expandTargetsForPlatform(platform, targets)
	switch platform {
	case "web":
		return measureWebArtifact(outDir, appName, appID, appVersion, expanded)
	case "android":
		return measureMobileArtifact(outDir, appName, appID, appVersion, platform, expanded, []PackageKind{PackageKindAAB, PackageKindAPK})
	case "ios":
		return measureMobileArtifact(outDir, appName, appID, appVersion, platform, expanded, []PackageKind{PackageKindIPA})
	default:
		return measureDesktopOrGenericArtifact(outDir, appName, appID, appVersion, platform, expanded)
	}
}

func expandTargetsForPlatform(platform string, configured []string) []string {
	var out []string
	add := func(ts ...string) {
		for _, t := range ts {
			t = strings.TrimSpace(t)
			if t != "" && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	for _, t := range configured {
		switch t {
		case "macos":
			add("macos_universal", "macos_arm64", "macos_x64")
		case "windows":
			add("windows_x64", "windows_arm64")
		case "linux":
			add("linux_x64", "linux_arm64")
		case "android":
			add("android_universal", "android_arm64", "android_x64")
		default:
			add(t)
		}
	}
	if len(out) == 0 {
		switch platform {
		case "macos":
			add("macos_universal", "macos_arm64", "macos_x64")
		case "windows":
			add("windows_x64", "windows_arm64")
		case "linux":
			add("linux_x64", "linux_arm64")
		case "android":
			add("android_universal", "android_arm64", "android_x64")
		case "ios":
			add("ios", "ios_simulator")
		default:
			add(platform)
		}
	}
	return out
}

func findPackageDirs(outDir, appName, appVersion string, targets []string) []string {
	var dirs []string
	add := func(p string) {
		if p == "" || slices.Contains(dirs, p) {
			return
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			dirs = append(dirs, p)
		}
	}
	for _, target := range targets {
		if appName != "" && appVersion != "" {
			add(PackageDir(outDir, appName, appVersion, target))
		}
	}
	entries, err := os.ReadDir(outDir)
	if err == nil {
		for _, target := range targets {
			suffix := "-" + target
			for _, ent := range entries {
				if ent.IsDir() && strings.HasSuffix(ent.Name(), suffix) {
					add(filepath.Join(outDir, ent.Name()))
				}
			}
		}
	}
	return dirs
}

func measureWebArtifact(outDir, appName, appID, appVersion string, targets []string) (ArtifactMeasurement, error) {
	// 1. Locate unpacked web build directory (nsgo build writes <outDir>/<Name>-<Version>-web/).
	webDir := ""
	pkgDirs := findPackageDirs(outDir, appName, appVersion, targets)
	if st, err := os.Stat(filepath.Join(outDir, "web")); err == nil && st.IsDir() {
		pkgDirs = append(pkgDirs, filepath.Join(outDir, "web"))
	}
	for _, d := range pkgDirs {
		if dirHasWebContent(d) {
			webDir = d
			break
		}
	}

	// 2. Locate web release archive (<outDir>/releases/<slug>-<version>-web.zip) if written.
	releaseZip := findReleaseFile(outDir, appName, appID, appVersion, targets, []string{".zip"})
	if releaseZip != "" {
		meas, err := measureZipArtifact(releaseZip, PackageKindWasm)
		if err != nil {
			return ArtifactMeasurement{}, err
		}
		if webDir != "" && hasIndexHTML(webDir) {
			meas.WebBuildDir = webDir
		}
		return meas, nil
	}

	// 3. Measure unpacked web package directory when present (e.g. <outDir>/<Name>-<Version>-web/app.wasm).
	if webDir != "" {
		return measureWebDir(webDir)
	}

	// 4. Fallback: a standalone .wasm file placed directly in outDir or outDir/releases.
	for _, searchDir := range []string{filepath.Join(outDir, "releases"), outDir} {
		if wasmPath := findFirstFileWithExts(searchDir, []string{".wasm"}); wasmPath != "" {
			return measureStandaloneWasm(wasmPath)
		}
	}

	return ArtifactMeasurement{}, nil
}

func dirHasWebContent(dir string) bool {
	if hasIndexHTML(dir) {
		return true
	}
	return findFirstFileWithExts(dir, []string{".wasm"}) != ""
}

func hasIndexHTML(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !st.IsDir()
}

func measureStandaloneWasm(wasmPath string) (ArtifactMeasurement, error) {
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	wm, err := MeasureWasm(data)
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	return ArtifactMeasurement{
		Found:           true,
		ArtifactPath:    wasmPath,
		ArtifactName:    filepath.Base(wasmPath),
		Kind:            PackageKindWasm,
		SizeBytes:       wm.RawBytes,
		WasmMeasurement: &wm,
		Categories: CategoryTotals{
			CodeBytes: wm.RawBytes,
		},
	}, nil
}

func measureWebDir(dir string) (ArtifactMeasurement, error) {
	var totals CategoryTotals
	var totalBytes int64
	var wasmPath string
	var wasmMeas *Measurement
	fileCount := 0

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		lowerName := strings.ToLower(d.Name())
		// Skip precompressed sidecar files (.wasm.gz / .wasm.br) so the module is not double-counted;
		// gzip size is measured directly via MeasureWasm on the .wasm module.
		if strings.HasSuffix(lowerName, ".wasm.gz") || strings.HasSuffix(lowerName, ".wasm.br") {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		fileCount++
		sz := info.Size()
		totalBytes += sz

		if strings.ToLower(filepath.Ext(p)) == ".wasm" {
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			wm, merr := MeasureWasm(data)
			if merr != nil {
				return merr
			}
			if wasmMeas == nil {
				cp := wm
				wasmMeas = &cp
				wasmPath = p
			} else {
				wasmMeas.RawBytes += wm.RawBytes
				wasmMeas.GzipBytes += wm.GzipBytes
			}
			totals.CodeBytes += wm.RawBytes
			return nil
		}

		classifyFileByPath(&totals, p, sz)
		return nil
	})
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	if fileCount == 0 {
		return ArtifactMeasurement{}, nil
	}

	artPath := dir
	artName := filepath.Base(dir)
	if fileCount == 1 && wasmPath != "" {
		artPath = wasmPath
		artName = filepath.Base(wasmPath)
	}
	meas := ArtifactMeasurement{
		Found:           true,
		ArtifactPath:    artPath,
		ArtifactName:    artName,
		Kind:            PackageKindWasm,
		SizeBytes:       totalBytes,
		WasmMeasurement: wasmMeas,
		Categories:      totals,
	}
	if hasIndexHTML(dir) {
		meas.WebBuildDir = dir
	}
	return meas, nil
}

func measureMobileArtifact(outDir, appName, appID, appVersion, platform string, targets []string, kinds []PackageKind) (ArtifactMeasurement, error) {
	exts := make([]string, len(kinds))
	for i, k := range kinds {
		exts[i] = "." + string(k)
	}

	// 1. Check <outDir>/releases/<slug>-<version>-<target><ext> in kind preference order (e.g. .aab before .apk).
	if p := findReleaseFile(outDir, appName, appID, appVersion, targets, exts); p != "" {
		kind, _ := DetectPackageKind(p)
		return measureMobileFile(p, kind)
	}

	// 2. Check inside each per-target package dir <outDir>/<Name>-<Version>-<target>/.
	pkgDirs := findPackageDirs(outDir, appName, appVersion, targets)
	for _, ext := range exts {
		for _, d := range pkgDirs {
			if p := findFirstFileWithExts(d, []string{ext}); p != "" {
				kind, _ := DetectPackageKind(p)
				return measureMobileFile(p, kind)
			}
		}
	}

	// 3. Fallback: direct fixture file in <outDir>/releases or <outDir> matching platform extensions (.aab/.apk for android, .ipa for ios).
	for _, ext := range exts {
		for _, searchDir := range []string{filepath.Join(outDir, "releases"), outDir} {
			if p := findFirstFileWithExts(searchDir, []string{ext}); p != "" {
				kind, _ := DetectPackageKind(p)
				return measureMobileFile(p, kind)
			}
		}
	}

	// 4. For iOS simulator builds that write an unpacked .app without an .ipa, measure the package dir.
	if platform == "ios" {
		for _, d := range pkgDirs {
			if meas, err := measureDirectoryArtifact(d); err == nil && meas.Found {
				return meas, nil
			}
		}
	}

	return ArtifactMeasurement{}, nil
}

func measureMobileFile(pkgPath string, kind PackageKind) (ArtifactMeasurement, error) {
	rep, totals, err := inspectMobilePackageWithTotals(pkgPath, kind)
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	return ArtifactMeasurement{
		Found:        true,
		ArtifactPath: pkgPath,
		ArtifactName: filepath.Base(pkgPath),
		Kind:         kind,
		SizeBytes:    rep.ArchiveBytes,
		MobileReport: &rep,
		Categories:   totals,
	}, nil
}

func measureDesktopOrGenericArtifact(outDir, appName, appID, appVersion, platform string, targets []string) (ArtifactMeasurement, error) {
	var releaseExts []string
	switch platform {
	case "macos":
		releaseExts = []string{".dmg", ".pkg", ".zip"}
	case "windows":
		releaseExts = []string{"-setup.exe", ".msi", ".zip", ".exe"}
	case "linux":
		releaseExts = []string{".AppImage", ".deb", ".pkg.tar.gz", ".tar.gz", ".flatpak", ".zip"}
	case "harmonyos":
		releaseExts = []string{".hap", ".zip"}
	default:
		releaseExts = []string{".zip", ".tar.gz"}
	}

	pkgDirs := findPackageDirs(outDir, appName, appVersion, targets)
	releasePath := findReleaseFile(outDir, appName, appID, appVersion, targets, releaseExts)

	if releasePath != "" {
		lower := strings.ToLower(releasePath)
		if strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".hap") {
			return measureZipArtifact(releasePath, "")
		}
		info, err := os.Stat(releasePath)
		if err != nil {
			return ArtifactMeasurement{}, err
		}
		if info.Size() <= 0 {
			return ArtifactMeasurement{}, nil
		}
		// If an unpacked package directory also exists, classify its real files to produce
		// accurate category proportions scaled to the real release artifact size; otherwise
		// attribute the single-file installer/executable honestly.
		totals := CategoryTotals{}
		if len(pkgDirs) > 0 {
			if dirMeas, derr := measureDirectoryArtifact(pkgDirs[0]); derr == nil && dirMeas.Found && dirMeas.Categories.Total() > 0 {
				totals = scaleTotalsToSize(dirMeas.Categories, info.Size())
			}
		}
		if totals.Total() == 0 {
			if isCodeFilePath(releasePath) || strings.HasSuffix(releasePath, ".AppImage") {
				totals.CodeBytes = info.Size()
			} else {
				totals.OtherBytes = info.Size()
			}
		}
		return ArtifactMeasurement{
			Found:        true,
			ArtifactPath: releasePath,
			ArtifactName: filepath.Base(releasePath),
			SizeBytes:    info.Size(),
			Categories:   totals,
		}, nil
	}

	for _, d := range pkgDirs {
		meas, err := measureDirectoryArtifact(d)
		if err != nil {
			return ArtifactMeasurement{}, err
		}
		if meas.Found {
			return meas, nil
		}
	}

	return ArtifactMeasurement{}, nil
}

func scaleTotalsToSize(raw CategoryTotals, target int64) CategoryTotals {
	sum := raw.Total()
	if sum <= 0 || target <= 0 {
		return CategoryTotals{}
	}
	scale := func(v int64) int64 {
		return int64(float64(v) / float64(sum) * float64(target))
	}
	out := CategoryTotals{
		CodeBytes:     scale(raw.CodeBytes),
		TexturesBytes: scale(raw.TexturesBytes),
		AudioBytes:    scale(raw.AudioBytes),
	}
	rem := target - (out.CodeBytes + out.TexturesBytes + out.AudioBytes)
	if rem < 0 {
		rem = 0
	}
	out.OtherBytes = rem
	return out
}

func measureZipArtifact(zipPath string, kind PackageKind) (ArtifactMeasurement, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return ArtifactMeasurement{}, fmt.Errorf("open zip archive %q: %w", zipPath, err)
	}

	var totals CategoryTotals
	var compSum int64
	var wasmMeas *Measurement
	for _, zf := range zr.File {
		name := normalizeZipPath(zf.Name)
		if name == "" || zf.FileInfo().IsDir() || strings.HasSuffix(zf.Name, "/") {
			continue
		}
		comp := int64(zf.CompressedSize64)
		compSum += comp

		if strings.ToLower(path.Ext(name)) == ".wasm" {
			if rc, oerr := zf.Open(); oerr == nil {
				data, rerr := io.ReadAll(rc)
				_ = rc.Close()
				if rerr == nil {
					if wm, merr := MeasureWasm(data); merr == nil {
						if wasmMeas == nil {
							cp := wm
							wasmMeas = &cp
						} else {
							wasmMeas.RawBytes += wm.RawBytes
							wasmMeas.GzipBytes += wm.GzipBytes
						}
					}
				}
			}
			totals.CodeBytes += comp
			continue
		}
		classifyFileByPath(&totals, name, comp)
	}
	if overhead := info.Size() - compSum; overhead > 0 {
		totals.OtherBytes += overhead
	}

	return ArtifactMeasurement{
		Found:           true,
		ArtifactPath:    zipPath,
		ArtifactName:    filepath.Base(zipPath),
		Kind:            kind,
		SizeBytes:       info.Size(),
		WasmMeasurement: wasmMeas,
		Categories:      totals,
	}, nil
}

func measureDirectoryArtifact(dir string) (ArtifactMeasurement, error) {
	var totals CategoryTotals
	var totalBytes int64
	fileCount := 0

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		fileCount++
		sz := info.Size()
		totalBytes += sz
		classifyDirFile(&totals, p, info, sz)
		return nil
	})
	if err != nil {
		return ArtifactMeasurement{}, err
	}
	if fileCount == 0 || totalBytes <= 0 {
		return ArtifactMeasurement{}, nil
	}
	return ArtifactMeasurement{
		Found:        true,
		ArtifactPath: dir,
		ArtifactName: filepath.Base(dir),
		SizeBytes:    totalBytes,
		Categories:   totals,
	}, nil
}

func findReleaseFile(outDir, appName, appID, appVersion string, targets, exts []string) string {
	relDir := filepath.Join(outDir, "releases")
	searchDirs := []string{relDir, outDir}

	// Exact NSGo releaseName match first.
	if appVersion != "" {
		for _, ext := range exts {
			for _, target := range targets {
				name := ReleaseName(appName, appID, appVersion, target, ext)
				for _, sd := range searchDirs {
					candidate := filepath.Join(sd, name)
					if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
						return candidate
					}
				}
			}
		}
	}

	// Fallback: scan releases/ for any file matching -<target-with-hyphens><ext>.
	entries, err := os.ReadDir(relDir)
	if err != nil {
		return ""
	}
	for _, ext := range exts {
		lowerExt := strings.ToLower(ext)
		for _, target := range targets {
			needle := "-" + strings.ReplaceAll(strings.ToLower(target), "_", "-") + lowerExt
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				lowerName := strings.ToLower(ent.Name())
				if strings.HasSuffix(lowerName, ".sha256") || strings.HasSuffix(lowerName, ".zsync") {
					continue
				}
				if strings.HasSuffix(lowerName, needle) {
					return filepath.Join(relDir, ent.Name())
				}
			}
		}
	}
	return ""
}

func findFirstFileWithExts(dir string, exts []string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, ext := range exts {
		lowerExt := strings.ToLower(ext)
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(ent.Name()), lowerExt) {
				return filepath.Join(dir, ent.Name())
			}
		}
	}
	return ""
}

func classifyDirFile(totals *CategoryTotals, filePath string, info fs.FileInfo, size int64) {
	slashed := filepath.ToSlash(filePath)
	if strings.Contains(slashed, "/Contents/MacOS/") || strings.Contains(slashed, "/Contents/Frameworks/") {
		totals.CodeBytes += size
		return
	}
	if isCodeFilePath(slashed) {
		totals.CodeBytes += size
		return
	}
	if isTextureExt(strings.ToLower(filepath.Ext(slashed))) {
		totals.TexturesBytes += size
		return
	}
	if isAudioExt(strings.ToLower(filepath.Ext(slashed))) {
		totals.AudioBytes += size
		return
	}
	// Extensionless executable binaries (such as Linux ELF binaries) count as code.
	if filepath.Ext(slashed) == "" && info != nil && info.Mode()&0o111 != 0 {
		totals.CodeBytes += size
		return
	}
	totals.OtherBytes += size
}

func classifyFileByPath(totals *CategoryTotals, filePath string, size int64) {
	slashed := filepath.ToSlash(filePath)
	if strings.Contains(slashed, "/Contents/MacOS/") || strings.Contains(slashed, "/Contents/Frameworks/") {
		totals.CodeBytes += size
		return
	}
	ext := strings.ToLower(path.Ext(slashed))
	switch {
	case isCodeExt(ext):
		totals.CodeBytes += size
	case isTextureExt(ext):
		totals.TexturesBytes += size
	case isAudioExt(ext):
		totals.AudioBytes += size
	default:
		totals.OtherBytes += size
	}
}

func isCodeFilePath(slashed string) bool {
	return isCodeExt(strings.ToLower(path.Ext(slashed)))
}

func isCodeExt(ext string) bool {
	switch ext {
	case ".wasm", ".js", ".mjs", ".exe", ".dll", ".dylib", ".so", ".dex", ".metallib", ".spv":
		return true
	default:
		return false
	}
}

func isTextureExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".ktx2", ".dds", ".svg", ".gif", ".avif", ".bmp", ".car", ".icns", ".ico":
		return true
	default:
		return false
	}
}

func isAudioExt(ext string) bool {
	switch ext {
	case ".wav", ".ogg", ".opus", ".mp3", ".flac", ".m4a", ".aac", ".caf", ".aiff":
		return true
	default:
		return false
	}
}
