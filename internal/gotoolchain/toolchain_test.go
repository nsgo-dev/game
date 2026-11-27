package gotoolchain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func writeFakeGo(t *testing.T, path, versionLine string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\necho '" + versionLine + "'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func minimalTestPATH(t *testing.T, root string) string {
	t.Helper()
	usrBin := filepath.Join(root, "usr", "bin")
	bin := filepath.Join(root, "bin")
	for _, d := range []string{usrBin, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return usrBin + string(os.PathListSeparator) + bin
}

func TestResolveFindsGoInUsrLocalGoBinWithMinimalPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable mode test")
	}
	root := t.TempDir()
	home := t.TempDir()
	fakeGo := filepath.Join(root, "usr", "local", "go", "bin", "go")
	writeFakeGo(t, fakeGo, "go version go1.27.4 darwin/arm64")

	env := SearchEnv{
		GOOS:           "darwin",
		PathEnv:        minimalTestPATH(t, root),
		HomeDir:        home,
		RootDir:        root,
		LoginShellPATH: func() string { return "" },
	}

	gotBin, ok := ResolveBinaryIn(env)
	if !ok || gotBin != fakeGo {
		t.Fatalf("ResolveBinaryIn() = (%q, %v), want (%q, true)", gotBin, ok, fakeGo)
	}

	st := ProbeIn(env, "1.27.3")
	if !st.OK() || st.Binary != fakeGo || st.Version != "1.27.4" || st.TooOld {
		t.Fatalf("ProbeIn() = %+v, want OK with binary=%q version=1.27.4", st, fakeGo)
	}
	parts := filepath.SplitList(st.PATH)
	if len(parts) == 0 || parts[0] != filepath.Dir(fakeGo) {
		t.Fatalf("resolved PATH = %q, want %q prepended first", st.PATH, filepath.Dir(fakeGo))
	}
}

func TestResolveFindsGoInFakeLoginShellPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable mode test")
	}
	root := t.TempDir()
	home := t.TempDir()
	customDir := filepath.Join(home, "custom-nix-go", "bin")
	fakeGo := filepath.Join(customDir, "go")
	writeFakeGo(t, fakeGo, "go version go1.27.3 linux/amd64")

	minimalPath := minimalTestPATH(t, root)
	env := SearchEnv{
		GOOS:    "linux",
		PathEnv: minimalPath,
		HomeDir: home,
		RootDir: root,
		LoginShellPATH: func() string {
			return customDir + string(os.PathListSeparator) + minimalPath
		},
	}

	gotBin, ok := ResolveBinaryIn(env)
	if !ok || gotBin != fakeGo {
		t.Fatalf("ResolveBinaryIn() = (%q, %v), want (%q, true)", gotBin, ok, fakeGo)
	}

	st := ProbeIn(env, "1.27.3")
	if !st.OK() || st.Binary != fakeGo || st.Version != "1.27.3" {
		t.Fatalf("ProbeIn() = %+v, want OK with binary=%q version=1.27.3", st, fakeGo)
	}
	if !slices.Contains(filepath.SplitList(st.PATH), customDir) {
		t.Fatalf("resolved PATH = %q, want %q included", st.PATH, customDir)
	}
}

func TestResolveFindsGoInHomebrewShimsAndWindowsProgramFiles(t *testing.T) {
	t.Run("homebrew and mise on unix", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix executable mode test")
		}
		root := t.TempDir()
		home := t.TempDir()
		brewGo := filepath.Join(root, "opt", "homebrew", "bin", "go")
		writeFakeGo(t, brewGo, "go version go1.27.4 darwin/arm64")

		env := SearchEnv{
			GOOS:           "darwin",
			PathEnv:        minimalTestPATH(t, root),
			HomeDir:        home,
			RootDir:        root,
			LoginShellPATH: func() string { return "" },
		}
		got, ok := ResolveBinaryIn(env)
		if !ok || got != brewGo {
			t.Fatalf("homebrew lookup = (%q, %v), want (%q, true)", got, ok, brewGo)
		}

		// Remove Homebrew go and verify mise shims are found.
		if err := os.Remove(brewGo); err != nil {
			t.Fatal(err)
		}
		miseGo := filepath.Join(home, ".local", "share", "mise", "shims", "go")
		writeFakeGo(t, miseGo, "go version go1.27.4 darwin/arm64")
		got, ok = ResolveBinaryIn(env)
		if !ok || got != miseGo {
			t.Fatalf("mise lookup = (%q, %v), want (%q, true)", got, ok, miseGo)
		}
	})

	t.Run("windows ProgramFiles Go bin", func(t *testing.T) {
		root := t.TempDir()
		progFiles := filepath.Join(root, "Program Files")
		winGo := filepath.Join(progFiles, "Go", "bin", "go.exe")
		if err := os.MkdirAll(filepath.Dir(winGo), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(winGo, []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
		env := SearchEnv{
			GOOS:         "windows",
			PathEnv:      `C:\Windows\System32`,
			HomeDir:      t.TempDir(),
			ProgramFiles: progFiles,
			RootDir:      root,
			VersionOf: func(context.Context, string, []string) (string, error) {
				return "go version go1.27.4 windows/amd64", nil
			},
		}
		got, ok := ResolveBinaryIn(env)
		if !ok || got != winGo {
			t.Fatalf("windows lookup = (%q, %v), want (%q, true)", got, ok, winGo)
		}
		st := ProbeIn(env, "1.27.3")
		if !st.OK() || st.Version != "1.27.4" {
			t.Fatalf("windows ProbeIn = %+v, want OK 1.27.4", st)
		}
	})
}

func TestProbeDetectsMissingAndTooOldGoAndRefreshPicksUpInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable mode test")
	}
	root := t.TempDir()
	home := t.TempDir()
	env := SearchEnv{
		GOOS:           "darwin",
		PathEnv:        minimalTestPATH(t, root),
		HomeDir:        home,
		RootDir:        root,
		LoginShellPATH: func() string { return "" },
	}
	restore := SetSearchEnvForTest(env)
	defer restore()

	// 1. Missing Go.
	st := ResolveMin("1.27.3")
	if st.Found || st.OK() || st.MinVersion != "1.27.3" {
		t.Fatalf("missing Go status = %+v, want Found=false OK=false MinVersion=1.27.3", st)
	}

	// 2. Install an older Go 1.26.2 in /usr/local/go/bin and Refresh.
	fakeGo := filepath.Join(root, "usr", "local", "go", "bin", "go")
	writeFakeGo(t, fakeGo, "go version go1.26.2 darwin/arm64")
	st = RefreshMin("1.27.3")
	if !st.Found || !st.TooOld || st.OK() || st.Version != "1.26.2" {
		t.Fatalf("old Go status = %+v, want Found=true TooOld=true Version=1.26.2", st)
	}

	// 3. Upgrade to Go 1.27.3 and Refresh.
	writeFakeGo(t, fakeGo, "go version go1.27.3 darwin/arm64")
	st = RefreshMin("1.27.3")
	if !st.OK() || st.Version != "1.27.3" || st.Binary != fakeGo {
		t.Fatalf("upgraded Go status = %+v, want OK Version=1.27.3 Binary=%q", st, fakeGo)
	}

	// 4. Command() and Env() use the resolved binary and enriched PATH.
	cmd := Command("version")
	if cmd.Path != fakeGo {
		t.Fatalf("Command().Path = %q, want %q", cmd.Path, fakeGo)
	}
	gotPath, ok := lookupEnvValue(cmd.Env, "PATH", "darwin")
	if !ok || !slices.Contains(filepath.SplitList(gotPath), filepath.Dir(fakeGo)) {
		t.Fatalf("Command().Env PATH = %q, want %q included", gotPath, filepath.Dir(fakeGo))
	}
}

func TestParseAndCompareGoVersions(t *testing.T) {
	cases := []struct {
		out  string
		want string
		ok   bool
	}{
		{"go version go1.27.4 darwin/arm64", "1.27.4", true},
		{"go version go1.27 linux/amd64", "1.27", true},
		{"go version go1.27rc1 darwin/arm64", "1.27rc1", true},
		{"go version devel go1.28-deadbeef Thu Nov 27 00:00:00 2026 +0000 darwin/arm64", "1.28", true},
		{"not a version", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseGoVersion(tc.out)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseGoVersion(%q) = (%q, %v), want (%q, %v)", tc.out, got, ok, tc.want, tc.ok)
		}
	}

	if CompareVersion("1.27.4", "1.27.3") <= 0 {
		t.Fatal("expected 1.27.4 > 1.27.3")
	}
	if CompareVersion("1.27.3", "1.27.3") != 0 {
		t.Fatal("expected 1.27.3 == 1.27.3")
	}
	if CompareVersion("1.27.2", "1.27.3") >= 0 {
		t.Fatal("expected 1.27.2 < 1.27.3")
	}
	if CompareVersion("1.27rc1", "1.27.3") >= 0 {
		t.Fatal("expected 1.27rc1 < 1.27.3")
	}
	if CompareVersion("1.26.9", "1.27") >= 0 {
		t.Fatal("expected 1.26.9 < 1.27")
	}

	ver, ok := ParseGoModMinVersion([]byte("module example.com/game\n\ngo 1.27.3 // required\n"))
	if !ok || ver != "1.27.3" {
		t.Fatalf("ParseGoModMinVersion = (%q, %v), want (1.27.3, true)", ver, ok)
	}
	if maxV := MaxGoVersion("1.26.0", "1.27.3", "1.27.1"); maxV != "1.27.3" {
		t.Fatalf("MaxGoVersion = %q, want 1.27.3", maxV)
	}
}

func TestEnrichedEnvPreservesEntriesAndPrependsResolvedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix path test")
	}
	root := t.TempDir()
	goDir := filepath.Join(root, "usr", "local", "go", "bin")
	fakeGo := filepath.Join(goDir, "go")
	writeFakeGo(t, fakeGo, "go version go1.27.4 darwin/arm64")

	env := SearchEnv{
		GOOS:    "darwin",
		HomeDir: t.TempDir(),
		RootDir: root,
	}
	got := EnrichedEnvWith([]string{"FOO=bar", "PATH=/usr/bin:/bin"}, fakeGo, env)
	var gotPath string
	var gotFoo string
	for _, kv := range got {
		if after, ok := strings.CutPrefix(kv, "PATH="); ok {
			gotPath = after
		}
		if after, ok := strings.CutPrefix(kv, "FOO="); ok {
			gotFoo = after
		}
	}
	if gotFoo != "bar" {
		t.Fatalf("FOO = %q, want bar", gotFoo)
	}
	parts := filepath.SplitList(gotPath)
	if len(parts) < 3 || parts[0] != goDir || parts[1] != "/usr/bin" || parts[2] != "/bin" {
		t.Fatalf("enriched PATH = %v, want [%q /usr/bin /bin ...]", parts, goDir)
	}
}
