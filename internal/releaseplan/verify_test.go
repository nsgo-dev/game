package releaseplan

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopTargetsExpansion(t *testing.T) {
	got := DesktopTargets([]string{"macos", "web", "linux_x64", "ios", "windows", "android_arm64", "macos_arm64"})
	want := []string{"macos_arm64", "macos_x64", "linux_x64", "windows_x64", "windows_arm64"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DesktopTargets = %q, want %q", got, want)
	}
	if nonDesktop := DesktopTargets([]string{"web", "ios", "android", "harmonyos"}); len(nonDesktop) != 0 {
		t.Fatalf("DesktopTargets(non-desktop) = %q, want empty", nonDesktop)
	}
}

func TestVerifyDesktopReleaseSkipsNonDesktopTargets(t *testing.T) {
	plan := &Plan{
		Targets:   []string{"web", "ios", "android"},
		OutputDir: t.TempDir(),
	}
	if err := VerifyDesktopRelease(plan); err != nil {
		t.Fatalf("VerifyDesktopRelease(non-desktop) = %v, want nil", err)
	}
}

func TestVerifyDesktopReleaseFailsWhenDesktopBinaryMissing(t *testing.T) {
	plan := &Plan{
		AppName:    "Demo",
		AppVersion: "1.0.0",
		Targets:    []string{"linux_x64"},
		OutputDir:  t.TempDir(),
	}
	err := VerifyDesktopRelease(plan)
	if !errors.Is(err, ErrNoDesktopBinary) {
		t.Fatalf("VerifyDesktopRelease(missing) = %v, want ErrNoDesktopBinary", err)
	}
}

func TestVerifyBinaryDetectsMissingReleaseTagAndDebugSymbols(t *testing.T) {
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" || runtime.GOOS == "android" || runtime.GOOS == "ios" {
		t.Skip("requires a desktop host Go toolchain")
	}

	// Build a minimal real main package that references pkg/debug's overlay and console,
	// once without -tags=release and once with GOFLAGS=-tags=release (both stripped with
	// -ldflags "-s -w", matching nsgo build).
	srcDir := t.TempDir()
	mainGo := `package main

import (
	"fmt"
	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	ov := debug.New(debug.Options{ToggleKey: "F3"})
	_ = ov.View((*core.Scope)(nil))
	_, _ = ov.Console().Exec("help")
	fmt.Println(debug.Enabled())
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}

	buildBin := func(name string, env []string) string {
		t.Helper()
		out := filepath.Join(t.TempDir(), name)
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, filepath.Join(srcDir, "main.go"))
		cmd.Env = env
		if combined, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s failed: %v\n%s", name, err, combined)
		}
		return out
	}

	// Strip any inherited GOFLAGS so the debug build really has no -tags=release.
	cleanEnv := removeEnvKey(os.Environ(), "GOFLAGS")
	debugBin := buildBin("demo-debug", cleanEnv)
	releaseBin := buildBin("demo-release", WithReleaseEnv(cleanEnv))

	// Debug binary must fail VerifyBinary with ErrMissingReleaseTag.
	if err := VerifyBinary(debugBin); !errors.Is(err, ErrMissingReleaseTag) {
		t.Fatalf("VerifyBinary(debug) = %v, want ErrMissingReleaseTag", err)
	}

	// On ELF/Mach-O, symbol inspection alone must also catch linked debug symbols even on a -s -w stripped binary.
	if runtime.GOOS != "windows" {
		err := verifyBinarySymbols(debugBin)
		if !errors.Is(err, ErrDebugSymbolLinked) {
			t.Fatalf("verifyBinarySymbols(debug) = %v, want ErrDebugSymbolLinked", err)
		}
	}

	// Release binary must pass both build info and symbol checks.
	if err := VerifyBinary(releaseBin); err != nil {
		t.Fatalf("VerifyBinary(release) = %v, want nil", err)
	}

	// VerifyDesktopRelease finds and validates the binary in nsgo build's package layout.
	outDir := t.TempDir()
	hostTarget := hostDesktopTarget()
	pkgDir := filepath.Join(outDir, "Demo-1.0.0-"+hostTarget)
	var dest string
	switch runtime.GOOS {
	case "darwin":
		dest = filepath.Join(pkgDir, "Demo.app", "Contents", "MacOS", "Demo")
	case "windows":
		dest = filepath.Join(pkgDir, "Demo.exe")
	default:
		dest = filepath.Join(pkgDir, "Demo")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(releaseBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := &Plan{
		AppName:    "Demo",
		AppVersion: "1.0.0",
		Targets:    []string{hostTarget},
		OutputDir:  outDir,
	}
	if err := VerifyDesktopRelease(plan); err != nil {
		t.Fatalf("VerifyDesktopRelease(release) = %v, want nil", err)
	}

	// Replacing the packaged binary with the debug build causes VerifyDesktopRelease to fail.
	debugData, err := os.ReadFile(debugBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, debugData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDesktopRelease(plan); !errors.Is(err, ErrMissingReleaseTag) {
		t.Fatalf("VerifyDesktopRelease(debug) = %v, want ErrMissingReleaseTag", err)
	}
}

func hostDesktopTarget() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	switch runtime.GOOS {
	case "darwin":
		return "macos_" + arch
	case "windows":
		return "windows_" + arch
	default:
		return "linux_" + arch
	}
}

func removeEnvKey(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if k, _, ok := strings.Cut(entry, "="); ok && strings.EqualFold(k, key) {
			continue
		}
		out = append(out, entry)
	}
	return out
}
