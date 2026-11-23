package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
)

// stubDev replaces startNSGoDev for the test and returns the runs game dev starts.
func stubDev(t *testing.T, err error) *[]devRun {
	t.Helper()
	var runs []devRun
	prev := startNSGoDev
	startNSGoDev = func(_ context.Context, run devRun) error {
		runs = append(runs, run)
		return err
	}
	t.Cleanup(func() { startNSGoDev = prev })
	return &runs
}

// newProject returns a project directory with an nsgo.yaml whose dev.target is target and, when assets is not empty, that asset directory.
func newProject(t *testing.T, target, assets string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Test\ndev:\n  target: "+target+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if assets != "" {
		if err := os.MkdirAll(filepath.Join(dir, assets), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDevRunsNSGoDevWithTheAssetDirectory(t *testing.T) {
	runs := stubDev(t, nil)
	dir := newProject(t, "native", "resources")

	a, _, stderr := newTestApp(t)
	if code := run([]string{"dev", "-fps", dir, "-lang", "fr"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if len(*runs) != 1 {
		t.Fatalf("%d runs, want 1", len(*runs))
	}
	got := (*runs)[0]
	if want := []string{"tool", "nsgo", "dev", "-fps", "-lang", "fr", dir}; !slices.Equal(got.args, want) {
		t.Fatalf("args = %q, want %q", got.args, want)
	}
	want := hotreload.AssetsDirEnv + "=" + filepath.Join(dir, "resources")
	if !slices.Equal(got.env, []string{want}) {
		t.Fatalf("env = %q, want %q", got.env, want)
	}
	if !filepath.IsAbs(strings.TrimPrefix(got.env[0], hotreload.AssetsDirEnv+"=")) {
		t.Fatalf("asset directory %q is not absolute", got.env[0])
	}
	if got.stdout != a.stdout || got.stderr != a.stderr {
		t.Fatal("nsgo dev does not write to game dev's output")
	}
	if !strings.Contains(stderr.String(), "reloading assets from "+filepath.Join(dir, "resources")) {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestDevInTheCurrentDirectoryWithAnotherAssetDirectoryAndTarget(t *testing.T) {
	runs := stubDev(t, nil)
	dir := newProject(t, "native", "art")
	t.Chdir(dir)

	a, _, stderr := newTestApp(t)
	if code := run([]string{"dev", "-assets", "art", "-target", "tui"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	got := (*runs)[0]
	if want := []string{"tool", "nsgo", "dev", "-target", "tui"}; !slices.Equal(got.args, want) {
		t.Fatalf("args = %q, want %q", got.args, want)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{hotreload.AssetsDirEnv + "=" + filepath.Join(wd, "art")}; !slices.Equal(got.env, want) {
		t.Fatalf("env = %q, want %q", got.env, want)
	}
}

func TestDevNotesTargetsThatCannotReadTheAssets(t *testing.T) {
	for _, tc := range []struct {
		name, yamlTarget string
		args             []string
		target           string
	}{
		{"from nsgo.yaml", "web", nil, "web"},
		{"from -target", "native", []string{"-target", "ios_simulator"}, "ios_simulator"},
		{"from -target after the directory", "native", []string{"DIR", "-target", "web"}, "web"},
		{"from -target= after the directory", "native", []string{"DIR", "--target=android"}, "android"},
		{"passed on over game dev's -target", "native", []string{"-target", "tui", "DIR", "-target", "web"}, "web"},
		{"android without -android-host", "android", []string{"DIR", "-android-host=false"}, "android"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubDev(t, nil)
			dir := newProject(t, tc.yamlTarget, "resources")
			a, _, stderr := newTestApp(t)
			if code := run(devArgs(tc.args, dir), a); code != 0 {
				t.Fatalf("exit %d (stderr: %s)", code, stderr)
			}
			if !strings.Contains(stderr.String(), "assets reload on the native and tui targets, not "+tc.target) {
				t.Fatalf("stderr = %q", stderr)
			}
		})
	}
}

func TestDevReloadsOnTargetsRunningHere(t *testing.T) {
	for _, tc := range []struct {
		name, yamlTarget string
		args             []string
	}{
		{"tui after the directory", "web", []string{"DIR", "-target", "tui"}},
		{"android host", "native", []string{"DIR", "-target=android", "-android-host"}},
		{"android host from nsgo.yaml", "android", []string{"DIR", "--android-host=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runs := stubDev(t, nil)
			dir := newProject(t, tc.yamlTarget, "resources")
			a, _, stderr := newTestApp(t)
			args := devArgs(tc.args, dir)
			if code := run(args, a); code != 0 {
				t.Fatalf("exit %d (stderr: %s)", code, stderr)
			}
			if !strings.Contains(stderr.String(), "reloading assets from "+filepath.Join(dir, "resources")) {
				t.Fatalf("stderr = %q", stderr)
			}
			if want := append([]string{"tool", "nsgo", "dev"}, append(slices.Clone(args[2:]), dir)...); !slices.Equal((*runs)[0].args, want) {
				t.Fatalf("args = %q, want %q", (*runs)[0].args, want)
			}
		})
	}
}

// devArgs returns game dev's command line with args, DIR among them replaced by dir, and dir appended when they have no DIR.
func devArgs(args []string, dir string) []string {
	out := []string{"dev"}
	named := false
	for _, arg := range args {
		if arg == "DIR" {
			arg, named = dir, true
		}
		out = append(out, arg)
	}
	if !named {
		out = append(out, dir)
	}
	return out
}

func TestDevWithoutAnAssetDirectoryRunsWithoutReload(t *testing.T) {
	runs := stubDev(t, nil)
	dir := newProject(t, "native", "")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"dev", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if env := (*runs)[0].env; len(env) != 0 {
		t.Fatalf("env = %q, want none", env)
	}
	if !strings.Contains(stderr.String(), "no asset directory at "+filepath.Join(dir, "resources")) {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestDevErrors(t *testing.T) {
	stubDev(t, errors.New("exit status 1"))
	dir := newProject(t, "native", "resources")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"dev", dir}, a); code != 1 {
		t.Fatalf("failed nsgo dev exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "game dev: exit status 1") {
		t.Fatalf("stderr = %q", stderr)
	}

	runs := stubDev(t, nil)
	a, _, _ = newTestApp(t)
	if code := run([]string{"dev", filepath.Join(dir, "missing")}, a); code != 1 {
		t.Fatalf("missing directory exit %d, want 1", code)
	}
	file := filepath.Join(dir, "nsgo.yaml")
	a, _, _ = newTestApp(t)
	if code := run([]string{"dev", file}, a); code != 2 {
		t.Fatalf("file as directory exit %d, want 2", code)
	}
	if len(*runs) != 0 {
		t.Fatalf("nsgo dev ran %d times for bad directories", len(*runs))
	}
}

func TestProjectDevTarget(t *testing.T) {
	if got := projectDevTarget(t.TempDir()); got != "native" {
		t.Fatalf("no nsgo.yaml: %q, want native", got)
	}
	if got := projectDevTarget(newProject(t, "android", "")); got != "android" {
		t.Fatalf("android project: %q", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := projectDevTarget(dir); got != "native" {
		t.Fatalf("broken nsgo.yaml: %q, want native", got)
	}
}
