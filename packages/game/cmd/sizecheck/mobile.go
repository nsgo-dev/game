package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// PackageKind identifies the package artifact format inspected by sizecheck.
type PackageKind string

const (
	PackageKindWasm PackageKind = "wasm"
	PackageKindAAB  PackageKind = "aab"
	PackageKindAPK  PackageKind = "apk"
	PackageKindIPA  PackageKind = "ipa"
)

// Content category keys for Android AAB/APK and iOS IPA packages.
const (
	// Shared categories.
	CategoryResources = "resources"
	CategoryOther     = "other"

	// Android categories (AAB and APK).
	CategoryNativeLibs   = "native_libs"
	CategoryDEX          = "dex"
	CategoryAssets       = "assets"
	CategoryManifestMeta = "manifest_meta"

	// iOS categories (IPA).
	CategoryExecutable    = "executable"
	CategoryFrameworks    = "frameworks"
	CategoryAssetsCar     = "assets_car"
	CategoryLproj         = "lproj"
	CategoryCodeSignature = "code_signature"
)

// DefaultTopLargestFiles is the default number of largest archive entries kept
// in MobilePackageReport.LargestFiles.
const DefaultTopLargestFiles = 10

const bytesPerMiB int64 = 1024 * 1024

var (
	androidCategoryOrder = []string{
		CategoryNativeLibs,
		CategoryDEX,
		CategoryAssets,
		CategoryResources,
		CategoryManifestMeta,
		CategoryOther,
	}
	iosCategoryOrder = []string{
		CategoryExecutable,
		CategoryFrameworks,
		CategoryAssetsCar,
		CategoryResources,
		CategoryLproj,
		CategoryCodeSignature,
		CategoryOther,
	}
)

// SizeBreakdown holds compressed and uncompressed byte totals plus entry count
// for a single category or ABI bucket.
type SizeBreakdown struct {
	CompressedBytes   int64 `json:"compressed_bytes"`
	UncompressedBytes int64 `json:"uncompressed_bytes"`
	FileCount         int   `json:"file_count"`
}

// FileEntry describes one file inside a mobile package archive.
type FileEntry struct {
	Path              string `json:"path"`
	Category          string `json:"category"`
	ABI               string `json:"abi,omitempty"`
	CompressedBytes   int64  `json:"compressed_bytes"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
}

// MobileBudget defines size budgets (in bytes) for Android AAB/APK and iOS IPA
// packages. Any limit <= 0 is treated as unbounded.
type MobileBudget struct {
	// MaxTotalBytes limits the total package archive size on disk.
	MaxTotalBytes int64 `json:"max_total_bytes"`
	// MaxPerABIBytes limits the compressed native library bytes for any single
	// Android ABI (e.g. arm64-v8a, armeabi-v7a, x86_64).
	MaxPerABIBytes int64 `json:"max_per_abi_bytes,omitempty"`
	// MaxCategoryBytes limits compressed bytes per category key
	// (e.g. "native_libs", "dex", "executable", "frameworks").
	MaxCategoryBytes map[string]int64 `json:"max_category_bytes,omitempty"`
	// Note documents the rationale behind the budget thresholds.
	Note string `json:"note,omitempty"`
}

// DefaultAndroidAABBudget returns the documented default size budget for an
// Android App Bundle (.aab).
//
// Google Play caps per-device base-module downloads at 200 MiB. Keeping the
// combined AAB within 50 MiB total and 20 MiB per native ABI leaves headroom
// for game assets while preserving fast mobile install conversion.
func DefaultAndroidAABBudget() MobileBudget {
	return MobileBudget{
		MaxTotalBytes:  50 * bytesPerMiB,
		MaxPerABIBytes: 20 * bytesPerMiB,
		MaxCategoryBytes: map[string]int64{
			CategoryNativeLibs:   35 * bytesPerMiB,
			CategoryDEX:          5 * bytesPerMiB,
			CategoryAssets:       25 * bytesPerMiB,
			CategoryResources:    15 * bytesPerMiB,
			CategoryManifestMeta: 2 * bytesPerMiB,
		},
		Note: "Default Android AAB budget: 50 MiB total archive, 20 MiB per ABI (Google Play base download limit is 200 MiB).",
	}
}

// DefaultAndroidAPKBudget returns the documented default size budget for an
// Android APK (.apk).
//
// Multi-ABI APKs ship native libraries for every targeted ABI directly in the
// installable file. Keeping APKs within 40 MiB total and 20 MiB per ABI keeps
// direct downloads lean (well below the 100 MiB legacy APK ceiling).
func DefaultAndroidAPKBudget() MobileBudget {
	return MobileBudget{
		MaxTotalBytes:  40 * bytesPerMiB,
		MaxPerABIBytes: 20 * bytesPerMiB,
		MaxCategoryBytes: map[string]int64{
			CategoryNativeLibs:   30 * bytesPerMiB,
			CategoryDEX:          5 * bytesPerMiB,
			CategoryAssets:       20 * bytesPerMiB,
			CategoryResources:    15 * bytesPerMiB,
			CategoryManifestMeta: 2 * bytesPerMiB,
		},
		Note: "Default Android APK budget: 40 MiB total archive, 20 MiB per ABI (legacy Play APK limit is 100 MiB).",
	}
}

// DefaultIOSIPABudget returns the documented default size budget for an iOS
// App Store Package (.ipa).
//
// Apple enforces a 200 MiB cellular download threshold. Keeping IPAs within
// 50 MiB total ensures fast cellular downloads even before App Thinning.
func DefaultIOSIPABudget() MobileBudget {
	return MobileBudget{
		MaxTotalBytes: 50 * bytesPerMiB,
		MaxCategoryBytes: map[string]int64{
			CategoryExecutable:    25 * bytesPerMiB,
			CategoryFrameworks:    15 * bytesPerMiB,
			CategoryAssetsCar:     15 * bytesPerMiB,
			CategoryResources:     25 * bytesPerMiB,
			CategoryLproj:         5 * bytesPerMiB,
			CategoryCodeSignature: 2 * bytesPerMiB,
		},
		Note: "Default iOS IPA budget: 50 MiB total archive (Apple cellular download threshold is 200 MiB).",
	}
}

// DefaultMobileBudget returns the default MobileBudget for kind.
func DefaultMobileBudget(kind PackageKind) (MobileBudget, error) {
	switch kind {
	case PackageKindAAB:
		return DefaultAndroidAABBudget(), nil
	case PackageKindAPK:
		return DefaultAndroidAPKBudget(), nil
	case PackageKindIPA:
		return DefaultIOSIPABudget(), nil
	default:
		return MobileBudget{}, fmt.Errorf("no default mobile budget for package kind %q", kind)
	}
}

// LoadMobileBudget reads and decodes a MobileBudget JSON file from filePath. Its errors are *loaderr.Error values that name filePath, with the line of the problem and how to fix it.
func LoadMobileBudget(filePath string) (MobileBudget, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return MobileBudget{}, loaderr.Locate(err, filePath, nil, "check the budget file path")
	}
	var b MobileBudget
	if err := json.Unmarshal(data, &b); err != nil {
		return MobileBudget{}, loaderr.Locate(err, filePath, data, "correct the budget file; see docs/en/package-size-budgets.md")
	}
	return b, nil
}

// ParsePackageKind parses a CLI -kind value into a PackageKind.
func ParsePackageKind(raw string) (PackageKind, error) {
	switch PackageKind(strings.ToLower(strings.TrimSpace(raw))) {
	case PackageKindWasm:
		return PackageKindWasm, nil
	case PackageKindAAB:
		return PackageKindAAB, nil
	case PackageKindAPK:
		return PackageKindAPK, nil
	case PackageKindIPA:
		return PackageKindIPA, nil
	default:
		return "", fmt.Errorf("unsupported package kind %q (expected aab, apk, ipa, or wasm)", raw)
	}
}

// DetectPackageKind infers a PackageKind from filePath's extension.
func DetectPackageKind(filePath string) (PackageKind, bool) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".aab":
		return PackageKindAAB, true
	case ".apk":
		return PackageKindAPK, true
	case ".ipa":
		return PackageKindIPA, true
	case ".wasm":
		return PackageKindWasm, true
	default:
		return "", false
	}
}

// MobilePackageReport holds the full size breakdown and budget evaluation for
// an Android AAB/APK or iOS IPA archive.
type MobilePackageReport struct {
	Path              string                   `json:"path"`
	Kind              PackageKind              `json:"kind"`
	ArchiveBytes      int64                    `json:"archive_bytes"`
	CompressedBytes   int64                    `json:"compressed_bytes"`
	UncompressedBytes int64                    `json:"uncompressed_bytes"`
	FileCount         int                      `json:"file_count"`
	Categories        map[string]SizeBreakdown `json:"categories"`
	ABIs              map[string]SizeBreakdown `json:"abis,omitempty"`
	LargestFiles      []FileEntry              `json:"largest_files"`
	Budget            MobileBudget             `json:"budget"`
	Violations        []string                 `json:"violations,omitempty"`
	Passed            bool                     `json:"passed"`
}

// BudgetExceededError is returned by CheckMobilePackage when one or more
// limits in MobileBudget are exceeded. Its Error text includes all violations
// and the full Markdown space breakdown.
type BudgetExceededError struct {
	Report MobilePackageReport
}

func (e *BudgetExceededError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s size budget exceeded for %s (%d violation(s)):\n",
		packageKindLabel(e.Report.Kind), e.Report.Path, len(e.Report.Violations))
	for _, v := range e.Report.Violations {
		fmt.Fprintf(&b, "  - %s\n", v)
	}
	b.WriteString("\n")
	b.WriteString(FormatMobileReport(e.Report))
	return b.String()
}

// InspectMobilePackage opens the ZIP-based mobile archive at pkgPath and
// computes its category, ABI, and largest-file breakdowns.
func InspectMobilePackage(pkgPath string, kind PackageKind) (MobilePackageReport, error) {
	if !isMobileKind(kind) {
		return MobilePackageReport{}, fmt.Errorf("InspectMobilePackage requires aab, apk, or ipa kind, got %q", kind)
	}
	f, err := os.Open(pkgPath)
	if err != nil {
		return MobilePackageReport{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return MobilePackageReport{}, err
	}
	return InspectMobileReader(f, info.Size(), pkgPath, kind)
}

// InspectMobileReader inspects a ZIP-based mobile package from r with total
// archive byte length archiveSize.
func InspectMobileReader(r io.ReaderAt, archiveSize int64, displayPath string, kind PackageKind) (MobilePackageReport, error) {
	if !isMobileKind(kind) {
		return MobilePackageReport{}, fmt.Errorf("InspectMobileReader requires aab, apk, or ipa kind, got %q", kind)
	}
	zr, err := zip.NewReader(r, archiveSize)
	if err != nil {
		return MobilePackageReport{}, fmt.Errorf("open %s zip archive %q: %w", kind, displayPath, err)
	}

	rep := MobilePackageReport{
		Path:         displayPath,
		Kind:         kind,
		ArchiveBytes: archiveSize,
		Categories:   initCategories(kind),
		Passed:       true,
	}
	if kind == PackageKindAAB || kind == PackageKindAPK {
		rep.ABIs = make(map[string]SizeBreakdown)
	}

	var iosBundle iosBundleInfo
	if kind == PackageKindIPA {
		iosBundle = detectIOSBundle(zr.File)
	}

	entries := make([]FileEntry, 0, len(zr.File))
	for _, zf := range zr.File {
		name := normalizeZipPath(zf.Name)
		if name == "" || zf.FileInfo().IsDir() || strings.HasSuffix(zf.Name, "/") {
			continue
		}

		comp := int64(zf.CompressedSize64)
		uncomp := int64(zf.UncompressedSize64)

		var cat, abi string
		switch kind {
		case PackageKindAAB:
			cat, abi = classifyAABPath(name)
		case PackageKindAPK:
			cat, abi = classifyAPKPath(name)
		case PackageKindIPA:
			cat = classifyIPAPath(name, iosBundle)
		}

		rep.CompressedBytes += comp
		rep.UncompressedBytes += uncomp
		rep.FileCount++

		cb := rep.Categories[cat]
		cb.CompressedBytes += comp
		cb.UncompressedBytes += uncomp
		cb.FileCount++
		rep.Categories[cat] = cb

		if abi != "" {
			ab := rep.ABIs[abi]
			ab.CompressedBytes += comp
			ab.UncompressedBytes += uncomp
			ab.FileCount++
			rep.ABIs[abi] = ab
		}

		entries = append(entries, FileEntry{
			Path:              name,
			Category:          cat,
			ABI:               abi,
			CompressedBytes:   comp,
			UncompressedBytes: uncomp,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CompressedBytes != entries[j].CompressedBytes {
			return entries[i].CompressedBytes > entries[j].CompressedBytes
		}
		if entries[i].UncompressedBytes != entries[j].UncompressedBytes {
			return entries[i].UncompressedBytes > entries[j].UncompressedBytes
		}
		return entries[i].Path < entries[j].Path
	})
	if len(entries) > DefaultTopLargestFiles {
		entries = entries[:DefaultTopLargestFiles]
	}
	rep.LargestFiles = entries

	return rep, nil
}

// EvaluateMobileBudget evaluates an inspected MobilePackageReport against
// budget, populating Budget, Violations, and Passed.
func EvaluateMobileBudget(rep MobilePackageReport, budget MobileBudget) MobilePackageReport {
	rep.Budget = cloneMobileBudget(budget)
	rep.Violations = nil

	if budget.MaxTotalBytes > 0 && rep.ArchiveBytes > budget.MaxTotalBytes {
		rep.Violations = append(rep.Violations, fmt.Sprintf(
			"total %s archive size %s exceeds budget %s (+%s)",
			packageKindLabel(rep.Kind),
			formatBytes(rep.ArchiveBytes),
			formatBytes(budget.MaxTotalBytes),
			formatBytes(rep.ArchiveBytes-budget.MaxTotalBytes),
		))
	}

	if budget.MaxPerABIBytes > 0 && len(rep.ABIs) > 0 {
		abis := sortedKeys(rep.ABIs)
		for _, abi := range abis {
			st := rep.ABIs[abi]
			if st.CompressedBytes > budget.MaxPerABIBytes {
				rep.Violations = append(rep.Violations, fmt.Sprintf(
					"ABI %q native libs compressed size %s (uncompressed %s) exceeds per-ABI budget %s (+%s)",
					abi,
					formatBytes(st.CompressedBytes),
					formatBytes(st.UncompressedBytes),
					formatBytes(budget.MaxPerABIBytes),
					formatBytes(st.CompressedBytes-budget.MaxPerABIBytes),
				))
			}
		}
	}

	if len(budget.MaxCategoryBytes) > 0 {
		cats := make([]string, 0, len(budget.MaxCategoryBytes))
		for k := range budget.MaxCategoryBytes {
			cats = append(cats, k)
		}
		sort.Strings(cats)
		for _, cat := range cats {
			limit := budget.MaxCategoryBytes[cat]
			if limit <= 0 {
				continue
			}
			st := rep.Categories[cat]
			if st.CompressedBytes > limit {
				rep.Violations = append(rep.Violations, fmt.Sprintf(
					"category %q compressed size %s (uncompressed %s) exceeds budget %s (+%s)",
					cat,
					formatBytes(st.CompressedBytes),
					formatBytes(st.UncompressedBytes),
					formatBytes(limit),
					formatBytes(st.CompressedBytes-limit),
				))
			}
		}
	}

	rep.Passed = len(rep.Violations) == 0
	return rep
}

// CheckMobilePackage inspects pkgPath as kind and enforces budget.
//
// When the archive is readable, CheckMobilePackage always returns the populated
// MobilePackageReport. If any budget limit is exceeded, it also returns a
// *BudgetExceededError whose message contains every violation and the full
// Markdown space breakdown.
func CheckMobilePackage(pkgPath string, kind PackageKind, budget MobileBudget) (MobilePackageReport, error) {
	rep, err := InspectMobilePackage(pkgPath, kind)
	if err != nil {
		return MobilePackageReport{}, err
	}
	rep = EvaluateMobileBudget(rep, budget)
	if !rep.Passed {
		return rep, &BudgetExceededError{Report: rep}
	}
	return rep, nil
}

// FormatMobileReport renders a GitHub-flavored Markdown summary suitable for
// terminal output and $GITHUB_STEP_SUMMARY.
func FormatMobileReport(rep MobilePackageReport) string {
	var b strings.Builder
	status := "PASS"
	if !rep.Passed {
		status = "FAIL"
	}

	fmt.Fprintf(&b, "## %s Size Budget — %s\n\n", packageKindLabel(rep.Kind), status)
	fmt.Fprintf(&b, "- **Package**: `%s`\n", rep.Path)
	if rep.Budget.MaxTotalBytes > 0 {
		fmt.Fprintf(&b, "- **Archive size**: %s / %s (%.1f%%)\n",
			formatBytes(rep.ArchiveBytes),
			formatBytes(rep.Budget.MaxTotalBytes),
			pctOf(rep.ArchiveBytes, rep.Budget.MaxTotalBytes))
	} else {
		fmt.Fprintf(&b, "- **Archive size**: %s (no total limit)\n", formatBytes(rep.ArchiveBytes))
	}
	fmt.Fprintf(&b, "- **Entries**: %d files (%s compressed, %s uncompressed)\n",
		rep.FileCount,
		formatBytes(rep.CompressedBytes),
		formatBytes(rep.UncompressedBytes))
	if rep.Budget.Note != "" {
		fmt.Fprintf(&b, "- **Budget note**: %s\n", rep.Budget.Note)
	}
	b.WriteString("\n")

	if len(rep.Violations) > 0 {
		b.WriteString("### Budget Violations\n\n")
		for _, v := range rep.Violations {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Category Breakdown\n\n")
	b.WriteString("| Category | Files | Compressed | Uncompressed | Budget | Usage |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, cat := range orderedCategories(rep.Kind, rep.Categories) {
		st := rep.Categories[cat]
		limit := rep.Budget.MaxCategoryBytes[cat]
		budgetCell := "-"
		usageCell := "-"
		if limit > 0 {
			budgetCell = formatBytes(limit)
			usageCell = fmt.Sprintf("%.1f%%", pctOf(st.CompressedBytes, limit))
			if st.CompressedBytes > limit {
				usageCell += " (OVER)"
			}
		}
		fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %s | %s |\n",
			cat,
			st.FileCount,
			formatBytes(st.CompressedBytes),
			formatBytes(st.UncompressedBytes),
			budgetCell,
			usageCell)
	}
	b.WriteString("\n")

	if len(rep.ABIs) > 0 {
		b.WriteString("### Native Libraries by ABI\n\n")
		b.WriteString("| ABI | Files | Compressed | Uncompressed | Per-ABI Budget | Usage |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
		for _, abi := range sortedKeys(rep.ABIs) {
			st := rep.ABIs[abi]
			budgetCell := "-"
			usageCell := "-"
			if rep.Budget.MaxPerABIBytes > 0 {
				budgetCell = formatBytes(rep.Budget.MaxPerABIBytes)
				usageCell = fmt.Sprintf("%.1f%%", pctOf(st.CompressedBytes, rep.Budget.MaxPerABIBytes))
				if st.CompressedBytes > rep.Budget.MaxPerABIBytes {
					usageCell += " (OVER)"
				}
			}
			fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %s | %s |\n",
				abi,
				st.FileCount,
				formatBytes(st.CompressedBytes),
				formatBytes(st.UncompressedBytes),
				budgetCell,
				usageCell)
		}
		b.WriteString("\n")
	}

	if len(rep.LargestFiles) > 0 {
		fmt.Fprintf(&b, "### Largest Files (Top %d)\n\n", len(rep.LargestFiles))
		b.WriteString("| # | Path | Category | Compressed | Uncompressed |\n")
		b.WriteString("| ---: | --- | --- | ---: | ---: |\n")
		for i, f := range rep.LargestFiles {
			fmt.Fprintf(&b, "| %d | `%s` | `%s` | %s | %s |\n",
				i+1,
				f.Path,
				f.Category,
				formatBytes(f.CompressedBytes),
				formatBytes(f.UncompressedBytes))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// FormatWasmMarkdownReport renders a Markdown summary for a WebAssembly BudgetReport.
func FormatWasmMarkdownReport(rep BudgetReport) string {
	var b strings.Builder
	status := "PASS"
	if !rep.Passed() {
		status = "FAIL"
	}
	fmt.Fprintf(&b, "## WebAssembly Size Budget — %s\n\n", status)
	if rep.Budget.Target != "" {
		fmt.Fprintf(&b, "- **Target**: `%s`\n", rep.Budget.Target)
	}
	if rep.Budget.Note != "" {
		fmt.Fprintf(&b, "- **Budget note**: %s\n", rep.Budget.Note)
	}
	b.WriteString("\n")
	b.WriteString("| Metric | Measured | Budget | Usage |\n")
	b.WriteString("| --- | ---: | ---: | ---: |\n")
	fmt.Fprintf(&b, "| Raw `.wasm` | %s | %s | %.1f%% |\n",
		formatBytes(rep.Measured.RawBytes),
		formatBytes(rep.Budget.MaxRawBytes),
		pctOf(rep.Measured.RawBytes, rep.Budget.MaxRawBytes))
	fmt.Fprintf(&b, "| Gzipped `.wasm` | %s | %s | %.1f%% |\n",
		formatBytes(rep.Measured.GzipBytes),
		formatBytes(rep.Budget.MaxGzipBytes),
		pctOf(rep.Measured.GzipBytes, rep.Budget.MaxGzipBytes))
	b.WriteString("\n")
	if len(rep.Violations) > 0 {
		b.WriteString("### Budget Violations\n\n")
		for _, v := range rep.Violations {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func isMobileKind(kind PackageKind) bool {
	return kind == PackageKindAAB || kind == PackageKindAPK || kind == PackageKindIPA
}

func packageKindLabel(kind PackageKind) string {
	switch kind {
	case PackageKindAAB:
		return "Android AAB"
	case PackageKindAPK:
		return "Android APK"
	case PackageKindIPA:
		return "iOS IPA"
	case PackageKindWasm:
		return "WebAssembly"
	default:
		return string(kind)
	}
}

func initCategories(kind PackageKind) map[string]SizeBreakdown {
	cats := make(map[string]SizeBreakdown)
	switch kind {
	case PackageKindAAB, PackageKindAPK:
		for _, c := range androidCategoryOrder {
			cats[c] = SizeBreakdown{}
		}
	case PackageKindIPA:
		for _, c := range iosCategoryOrder {
			cats[c] = SizeBreakdown{}
		}
	}
	return cats
}

func orderedCategories(kind PackageKind, present map[string]SizeBreakdown) []string {
	var base []string
	switch kind {
	case PackageKindAAB, PackageKindAPK:
		base = androidCategoryOrder
	case PackageKindIPA:
		base = iosCategoryOrder
	}
	seen := make(map[string]bool, len(base))
	out := make([]string, 0, len(present))
	for _, c := range base {
		seen[c] = true
		out = append(out, c)
	}
	var extras []string
	for c := range present {
		if !seen[c] {
			extras = append(extras, c)
		}
	}
	sort.Strings(extras)
	return append(out, extras...)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cloneMobileBudget(b MobileBudget) MobileBudget {
	out := b
	if len(b.MaxCategoryBytes) > 0 {
		out.MaxCategoryBytes = make(map[string]int64, len(b.MaxCategoryBytes))
		for k, v := range b.MaxCategoryBytes {
			out.MaxCategoryBytes[k] = v
		}
	}
	return out
}

func normalizeZipPath(name string) string {
	p := strings.ReplaceAll(name, "\\", "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	p = strings.TrimPrefix(p, "/")
	return p
}

// classifyAABPath categorizes a normalized ZIP path inside an Android App Bundle (.aab).
func classifyAABPath(p string) (category, abi string) {
	if strings.HasPrefix(p, "META-INF/") ||
		strings.HasPrefix(p, "BUNDLE-METADATA/") ||
		p == "BundleConfig.pb" {
		return CategoryManifestMeta, ""
	}

	_, rel, hasSlash := strings.Cut(p, "/")
	if !hasSlash {
		switch {
		case strings.HasSuffix(strings.ToLower(p), ".dex"):
			return CategoryDEX, ""
		case p == "AndroidManifest.xml":
			return CategoryManifestMeta, ""
		case p == "resources.pb" || p == "resources.arsc":
			return CategoryResources, ""
		default:
			return CategoryOther, ""
		}
	}

	switch {
	case strings.HasPrefix(rel, "lib/"):
		afterLib := strings.TrimPrefix(rel, "lib/")
		if seg, _, ok := strings.Cut(afterLib, "/"); ok && seg != "" {
			return CategoryNativeLibs, seg
		}
		return CategoryNativeLibs, ""
	case strings.HasPrefix(rel, "dex/") || strings.HasSuffix(strings.ToLower(rel), ".dex"):
		return CategoryDEX, ""
	case strings.HasPrefix(rel, "assets/"):
		return CategoryAssets, ""
	case strings.HasPrefix(rel, "res/") || rel == "resources.pb" || rel == "resources.arsc":
		return CategoryResources, ""
	case strings.HasPrefix(rel, "manifest/") ||
		rel == "AndroidManifest.xml" ||
		rel == "native.pb" ||
		rel == "assets.pb":
		return CategoryManifestMeta, ""
	default:
		return CategoryOther, ""
	}
}

// classifyAPKPath categorizes a normalized ZIP path inside an Android APK (.apk).
func classifyAPKPath(p string) (category, abi string) {
	switch {
	case strings.HasPrefix(p, "lib/"):
		afterLib := strings.TrimPrefix(p, "lib/")
		if seg, _, ok := strings.Cut(afterLib, "/"); ok && seg != "" {
			return CategoryNativeLibs, seg
		}
		return CategoryNativeLibs, ""
	case strings.HasSuffix(strings.ToLower(p), ".dex"):
		return CategoryDEX, ""
	case strings.HasPrefix(p, "assets/"):
		return CategoryAssets, ""
	case strings.HasPrefix(p, "res/") || p == "resources.arsc":
		return CategoryResources, ""
	case p == "AndroidManifest.xml" || strings.HasPrefix(p, "META-INF/"):
		return CategoryManifestMeta, ""
	default:
		return CategoryOther, ""
	}
}

type iosBundleInfo struct {
	appPrefix string // e.g. "Payload/MyGame.app/"
	appStem   string // e.g. "MyGame"
	execName  string // CFBundleExecutable from Info.plist, or appStem
}

func detectIOSBundle(files []*zip.File) iosBundleInfo {
	var info iosBundleInfo
	var infoPlist *zip.File

	for _, zf := range files {
		p := normalizeZipPath(zf.Name)
		if !strings.HasPrefix(p, "Payload/") {
			continue
		}
		rest := strings.TrimPrefix(p, "Payload/")
		appDir, rel, ok := strings.Cut(rest, "/")
		if !ok || !strings.HasSuffix(appDir, ".app") {
			continue
		}
		prefix := "Payload/" + appDir + "/"
		if info.appPrefix == "" {
			info.appPrefix = prefix
			info.appStem = strings.TrimSuffix(appDir, ".app")
			info.execName = info.appStem
		}
		if prefix == info.appPrefix && rel == "Info.plist" {
			infoPlist = zf
		}
	}

	if infoPlist != nil {
		if execName := readPlistExecutable(infoPlist); execName != "" {
			info.execName = execName
		}
	}
	return info
}

func readPlistExecutable(zf *zip.File) string {
	const maxPlistBytes = 1 << 20 // 1 MiB safety cap
	rc, err := zf.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, maxPlistBytes))
	if err != nil || bytes.HasPrefix(data, []byte("bplist")) {
		return ""
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dictDepth := 0
	wantString := false
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				dictDepth++
				wantString = false
			case "key":
				if dictDepth == 1 {
					var key string
					if err := dec.DecodeElement(&key, &t); err != nil {
						return ""
					}
					wantString = strings.TrimSpace(key) == "CFBundleExecutable"
				}
			case "string":
				if dictDepth == 1 && wantString {
					var val string
					if err := dec.DecodeElement(&val, &t); err != nil {
						return ""
					}
					return strings.TrimSpace(val)
				}
				wantString = false
			default:
				if dictDepth == 1 {
					wantString = false
				}
			}
		case xml.EndElement:
			if t.Name.Local == "dict" && dictDepth > 0 {
				dictDepth--
			}
		}
	}
	return ""
}

// classifyIPAPath categorizes a normalized ZIP path inside an iOS IPA (.ipa).
func classifyIPAPath(p string, bundle iosBundleInfo) string {
	if bundle.appPrefix != "" && strings.HasPrefix(p, bundle.appPrefix) {
		rel := strings.TrimPrefix(p, bundle.appPrefix)
		if rel == "" {
			return CategoryOther
		}

		if strings.HasPrefix(rel, "_CodeSignature/") ||
			rel == "embedded.mobileprovision" ||
			strings.HasPrefix(rel, "SC_Info/") {
			return CategoryCodeSignature
		}

		if strings.HasPrefix(rel, "Frameworks/") ||
			strings.HasPrefix(rel, "PlugIns/") ||
			strings.HasSuffix(strings.ToLower(rel), ".dylib") {
			return CategoryFrameworks
		}

		if strings.HasSuffix(strings.ToLower(rel), ".car") {
			return CategoryAssetsCar
		}

		if hasPathSegmentSuffix(rel, ".lproj") {
			return CategoryLproj
		}

		if !strings.Contains(rel, "/") && (rel == bundle.execName || rel == bundle.appStem) {
			return CategoryExecutable
		}

		if isIOSResourcePath(rel) {
			return CategoryResources
		}

		return CategoryOther
	}

	if strings.HasPrefix(p, "SwiftSupport/") {
		return CategoryFrameworks
	}
	return CategoryOther
}

func hasPathSegmentSuffix(rel, suffix string) bool {
	lowerSuffix := strings.ToLower(suffix)
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasSuffix(strings.ToLower(seg), lowerSuffix) {
			return true
		}
	}
	return false
}

var iosResourceDirPrefixes = []string{
	"assets/",
	"Resources/",
	"resources/",
	"res/",
	"shaders/",
	"audio/",
	"sound/",
	"sounds/",
	"images/",
	"sprites/",
	"tilemaps/",
	"maps/",
	"fonts/",
	"data/",
}

var iosResourceExts = map[string]bool{
	".plist":      true,
	".png":        true,
	".jpg":        true,
	".jpeg":       true,
	".webp":       true,
	".gif":        true,
	".svg":        true,
	".pdf":        true,
	".icns":       true,
	".ttf":        true,
	".otf":        true,
	".woff":       true,
	".woff2":      true,
	".wav":        true,
	".mp3":        true,
	".m4a":        true,
	".aac":        true,
	".ogg":        true,
	".opus":       true,
	".caf":        true,
	".aiff":       true,
	".json":       true,
	".tmj":        true,
	".tsj":        true,
	".tmx":        true,
	".tsx":        true,
	".xml":        true,
	".yaml":       true,
	".yml":        true,
	".txt":        true,
	".strings":    true,
	".nib":        true,
	".storyboard": true,
	".metallib":   true,
	".metal":      true,
	".spv":        true,
	".glsl":       true,
	".wgsl":       true,
	".atlas":      true,
	".fnt":        true,
	".pak":        true,
	".dat":        true,
	".bin":        true,
	".glb":        true,
	".gltf":       true,
}

func isIOSResourcePath(rel string) bool {
	for _, prefix := range iosResourceDirPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	if hasPathSegmentSuffix(rel, ".bundle") ||
		hasPathSegmentSuffix(rel, ".storyboardc") ||
		hasPathSegmentSuffix(rel, ".nib") {
		return true
	}
	ext := strings.ToLower(path.Ext(rel))
	return iosResourceExts[ext]
}
