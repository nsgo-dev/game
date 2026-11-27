package releaseplan

import (
	"debug/buildinfo"
	"debug/elf"
	"debug/gosym"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var (
	// ErrMissingReleaseTag is wrapped when a built desktop release binary's Go build info does not record the release build tag.
	ErrMissingReleaseTag = errors.New("release binary missing release build tag")
	// ErrDebugSymbolLinked is wrapped when a built desktop release binary links a development-only symbol that should be compiled out under -tags release.
	ErrDebugSymbolLinked = errors.New("release binary links development-only debug symbol")
	// ErrNoDesktopBinary is wrapped when a release build requested desktop targets but no desktop executable was found in the output directory.
	ErrNoDesktopBinary = errors.New("no built desktop binary found to verify")
)

// ForbiddenDebugSymbols lists Go symbols that exist only in //go:build !release files across the game packages and must never appear in a release binary.
var ForbiddenDebugSymbols = []string{
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).sceneInspectorView",
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).aiInspectorView",
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).netInspectorView",
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).gcSummaryView",
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).drawStatsView",
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).profilerScopesView",
	"github.com/nsgo-dev/game/pkg/debug.(*Overlay).memorySummaryView",
	"github.com/nsgo-dev/game/pkg/debug.overlayPanelStyle",
	"github.com/nsgo-dev/game/pkg/debug.buildAITreeView",
	"github.com/nsgo-dev/game/pkg/debug.buildAIMachineView",
	"github.com/nsgo-dev/game/pkg/debug.(*ShapeOverlay).strokeWidth",
	"github.com/nsgo-dev/game/pkg/debug.drawSpriteBounds",
	"github.com/nsgo-dev/game/pkg/debug.drawingScale",
	"github.com/nsgo-dev/game/pkg/debug.(*Console).execHelp",
	"github.com/nsgo-dev/game/pkg/debug.(*Console).execSet",
	"github.com/nsgo-dev/game/pkg/tooling/hotreload.(*Watcher).loop",
	"github.com/nsgo-dev/game/pkg/tooling/hotreload.(*Watcher).poll",
	"github.com/nsgo-dev/game/pkg/tooling/devstate.(*Store).writeRaw",
	"github.com/nsgo-dev/game/pkg/tooling/devstate.(*Hook).saveUnlocked",
	"github.com/nsgo-dev/game/pkg/physics.resolveDebugStyle",
	"github.com/nsgo-dev/game/pkg/physics3d.resolveDebugStyle",
	"github.com/nsgo-dev/game/pkg/ai.clampGridIndex",
}

// DesktopTargets expands targets to the concrete NSGo desktop target names they include (macOS, Windows, and Linux), preserving order and omitting non-desktop targets such as web, ios, and android.
func DesktopTargets(targets []string) []string {
	var out []string
	add := func(names ...string) {
		for _, name := range names {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	for _, t := range targets {
		switch t {
		case "macos":
			add("macos_arm64", "macos_x64")
		case "macos_arm64", "macos_x64", "macos_universal":
			add(t)
		case "linux":
			add("linux_x64", "linux_arm64")
		case "linux_x64", "linux_arm64":
			add(t)
		case "windows":
			add("windows_x64", "windows_arm64")
		case "windows_x64", "windows_arm64":
			add(t)
		}
	}
	return out
}

// VerifyDesktopRelease locates the desktop executable(s) produced by plan in plan.OutputDir and verifies that each carries the release build tag in its Go build info and links no development-only debug symbol. Plans with no desktop targets (for example web-only or mobile-only builds) succeed without inspecting binaries.
func VerifyDesktopRelease(plan *Plan) error {
	if plan == nil {
		return nil
	}
	desktop := DesktopTargets(plan.Targets)
	if len(desktop) == 0 {
		return nil
	}
	bins := FindDesktopBinaries(plan)
	if len(bins) == 0 {
		return fmt.Errorf("%w in %s for targets %s", ErrNoDesktopBinary, plan.OutputDir, strings.Join(desktop, ", "))
	}
	for _, bin := range bins {
		if err := VerifyBinary(bin); err != nil {
			return err
		}
	}
	return nil
}

// FindDesktopBinaries returns the paths to built desktop executables under plan.OutputDir for the desktop targets in plan.Targets.
func FindDesktopBinaries(plan *Plan) []string {
	if plan == nil || plan.OutputDir == "" {
		return nil
	}
	desktop := DesktopTargets(plan.Targets)
	if len(desktop) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, target := range desktop {
		if p := findDesktopBinaryForTarget(plan.OutputDir, plan.AppName, plan.AppVersion, target); p != "" {
			add(p)
		}
	}
	return out
}

func findDesktopBinaryForTarget(outDir, appName, appVersion, target string) string {
	plat := PlatformOf(target)
	if appName != "" && appVersion != "" {
		pkgDir := filepath.Join(outDir, fmt.Sprintf("%s-%s-%s", appName, appVersion, target))
		if p := desktopBinaryInDir(pkgDir, plat, appName); p != "" {
			return p
		}
	}
	// Fall back to scanning package directories ending with -<target> in case nsgo.yaml defaulted name or version.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return ""
	}
	suffix := "-" + target
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		if p := desktopBinaryInDir(filepath.Join(outDir, entry.Name()), plat, appName); p != "" {
			return p
		}
	}
	return ""
}

func desktopBinaryInDir(pkgDir, platform, appName string) string {
	switch platform {
	case "macos":
		if appName != "" {
			p := filepath.Join(pkgDir, appName+".app", "Contents", "MacOS", appName)
			if isRegularFile(p) {
				return p
			}
		}
		entries, err := os.ReadDir(pkgDir)
		if err != nil {
			return ""
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}
			base := strings.TrimSuffix(entry.Name(), ".app")
			p := filepath.Join(pkgDir, entry.Name(), "Contents", "MacOS", base)
			if isRegularFile(p) {
				return p
			}
		}
	case "windows":
		if appName != "" {
			p := filepath.Join(pkgDir, appName+".exe")
			if isRegularFile(p) {
				return p
			}
		}
		entries, err := os.ReadDir(pkgDir)
		if err != nil {
			return ""
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			lower := strings.ToLower(entry.Name())
			if strings.HasSuffix(lower, ".exe") && !strings.Contains(lower, "setup") && !strings.Contains(lower, "uninstall") {
				p := filepath.Join(pkgDir, entry.Name())
				if isRegularFile(p) {
					return p
				}
			}
		}
	case "linux":
		if appName != "" {
			p := filepath.Join(pkgDir, appName)
			if isRegularFile(p) {
				return p
			}
		}
		entries, err := os.ReadDir(pkgDir)
		if err != nil {
			return ""
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if info.Mode()&0o111 == 0 {
				continue
			}
			p := filepath.Join(pkgDir, entry.Name())
			if hasELFMagic(p) {
				return p
			}
		}
	}
	return ""
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func hasELFMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic == [4]byte{0x7f, 'E', 'L', 'F'}
}

// VerifyBinary verifies that the Go executable at path was compiled with ReleaseTag (checked via Go build info, equivalent to `go version -m`) and links none of ForbiddenDebugSymbols (checked via Go function/symbol tables, working on both unstripped and `-s -w` stripped binaries).
func VerifyBinary(path string) error {
	if err := verifyBinaryBuildInfo(path); err != nil {
		return err
	}
	if err := verifyBinarySymbols(path); err != nil {
		return err
	}
	return nil
}

func verifyBinaryBuildInfo(path string) error {
	infos, err := readAllBuildInfos(path)
	if err != nil {
		return fmt.Errorf("read Go build info from %s: %w", path, err)
	}
	for _, bi := range infos {
		tagsVal, hasRelease := buildInfoHasReleaseTag(bi)
		if !hasRelease {
			if tagsVal == "" {
				return fmt.Errorf("%w: %s has no -tags setting in Go build info (expected %q)", ErrMissingReleaseTag, path, ReleaseTag)
			}
			return fmt.Errorf("%w: %s was built with -tags=%s (expected %q)", ErrMissingReleaseTag, path, tagsVal, ReleaseTag)
		}
	}
	return nil
}

func readAllBuildInfos(path string) ([]*buildinfo.BuildInfo, error) {
	if bi, err := buildinfo.ReadFile(path); err == nil {
		return []*buildinfo.BuildInfo{bi}, nil
	} else {
		// Universal macOS binaries wrap multiple Mach-O slices; inspect every slice.
		fat, fatErr := macho.OpenFat(path)
		if fatErr != nil {
			return nil, err
		}
		defer fat.Close()
		raw, openErr := os.Open(path)
		if openErr != nil {
			return nil, openErr
		}
		defer raw.Close()
		infos := make([]*buildinfo.BuildInfo, 0, len(fat.Arches))
		for _, arch := range fat.Arches {
			sr := io.NewSectionReader(raw, int64(arch.Offset), int64(arch.Size))
			bi, biErr := buildinfo.Read(sr)
			if biErr != nil {
				return nil, fmt.Errorf("universal slice %v: %w", arch.Cpu, biErr)
			}
			infos = append(infos, bi)
		}
		if len(infos) == 0 {
			return nil, err
		}
		return infos, nil
	}
}

func buildInfoHasReleaseTag(bi *buildinfo.BuildInfo) (tagsVal string, ok bool) {
	if bi == nil {
		return "", false
	}
	for _, s := range bi.Settings {
		if s.Key != "-tags" {
			continue
		}
		tagsVal = s.Value
		for _, raw := range strings.Split(s.Value, ",") {
			if strings.TrimSpace(raw) == ReleaseTag {
				return tagsVal, true
			}
		}
	}
	return tagsVal, false
}

func verifyBinarySymbols(path string) error {
	syms, err := readGoSymbols(path)
	if err != nil {
		return fmt.Errorf("inspect symbols in %s: %w", path, err)
	}
	for _, sym := range syms {
		for _, forbidden := range ForbiddenDebugSymbols {
			if sym == forbidden || strings.HasPrefix(sym, forbidden+".") {
				return fmt.Errorf("%w: %s links %s", ErrDebugSymbolLinked, path, sym)
			}
		}
	}
	return nil
}

// readGoSymbols extracts Go function and symbol names from an ELF, Mach-O (thin or universal), or PE executable, using .gopclntab/__gopclntab so stripped (`-s -w`) release binaries are inspected accurately.
func readGoSymbols(path string) ([]string, error) {
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		return readELFSymbols(f), nil
	}
	if f, err := macho.Open(path); err == nil {
		defer f.Close()
		return readMachOSymbols(f), nil
	}
	if fat, err := macho.OpenFat(path); err == nil {
		defer fat.Close()
		var out []string
		for _, arch := range fat.Arches {
			out = append(out, readMachOSymbols(arch.File)...)
		}
		return out, nil
	}
	if f, err := pe.Open(path); err == nil {
		defer f.Close()
		return readPESymbols(f), nil
	}
	return nil, errors.New("unrecognized executable format")
}

func readELFSymbols(f *elf.File) []string {
	var out []string
	if sec := f.Section(".gopclntab"); sec != nil {
		if data, err := sec.Data(); err == nil {
			var textAddr uint64
			if text := f.Section(".text"); text != nil {
				textAddr = text.Addr
			}
			out = append(out, pclntabFuncNames(data, textAddr)...)
		}
	}
	if syms, err := f.Symbols(); err == nil {
		for _, s := range syms {
			if s.Name != "" {
				out = append(out, s.Name)
			}
		}
	}
	return out
}

func readMachOSymbols(f *macho.File) []string {
	var out []string
	if sec := f.Section("__gopclntab"); sec != nil {
		if data, err := sec.Data(); err == nil {
			var textAddr uint64
			if text := f.Section("__text"); text != nil {
				textAddr = text.Addr
			}
			out = append(out, pclntabFuncNames(data, textAddr)...)
		}
	}
	if f.Symtab != nil {
		for _, s := range f.Symtab.Syms {
			if s.Name != "" {
				out = append(out, strings.TrimPrefix(s.Name, "_"))
			}
		}
	}
	return out
}

func readPESymbols(f *pe.File) []string {
	var out []string
	for _, s := range f.Symbols {
		if s.Name != "" {
			out = append(out, s.Name)
		}
	}
	return out
}

func pclntabFuncNames(data []byte, textAddr uint64) []string {
	if len(data) == 0 {
		return nil
	}
	pcln := gosym.NewLineTable(data, textAddr)
	tab, err := gosym.NewTable(nil, pcln)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(tab.Funcs))
	for _, fn := range tab.Funcs {
		if fn.Name != "" {
			out = append(out, fn.Name)
		}
	}
	return out
}
