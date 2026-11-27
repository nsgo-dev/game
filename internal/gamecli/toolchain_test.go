package gamecli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/editor"
)

func TestTemplateMinGoVersionMatchesEmbeddedTemplates(t *testing.T) {
	got := templateMinGoVersion()
	if got == "" {
		t.Fatal("templateMinGoVersion() returned empty")
	}
	if gotoolchain.CompareVersion(got, "1.27") < 0 {
		t.Fatalf("templateMinGoVersion() = %q, want >= 1.27", got)
	}
	if got != gotoolchain.DefaultMinVersion {
		t.Fatalf("templateMinGoVersion() = %q, want match with gotoolchain.DefaultMinVersion (%q)", got, gotoolchain.DefaultMinVersion)
	}

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ed.Close() }()
	configureEditorStartScreen(ed, nil)
	if ed.MinGoVersion() != got {
		t.Fatalf("configureEditorStartScreen set MinGoVersion = %q, want %q", ed.MinGoVersion(), got)
	}
}

func TestGameCLIExecSitesReceiveResolvedGoEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell fake go test")
	}

	root := t.TempDir()
	home := t.TempDir()
	minimalBin := filepath.Join(root, "usr", "bin")
	if err := os.MkdirAll(minimalBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goDir := filepath.Join(root, "usr", "local", "go", "bin")
	goBin := filepath.Join(goDir, "go")
	logFile := filepath.Join(t.TempDir(), "cli-go.log")

	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  echo 'go version go1.27.4 darwin/arm64'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf 'ARGS=%s|PATH=%s\\n' \"$*\" \"$PATH\" >> \"" + logFile + "\"\n" +
		"if [ \"$1\" = \"list\" ]; then\n" +
		"  echo 'false'\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	restore := gotoolchain.SetSearchEnvForTest(gotoolchain.SearchEnv{
		GOOS:           runtime.GOOS,
		PathEnv:        minimalBin,
		HomeDir:        home,
		RootDir:        root,
		LoginShellPATH: func() string { return "" },
	})
	defer restore()
	t.Setenv("PATH", minimalBin)

	workDir := t.TempDir()
	checkLast := func(label, wantArgs string) {
		t.Helper()
		raw, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("%s: read log: %v", label, err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		last := lines[len(lines)-1]
		if !strings.Contains(last, wantArgs) {
			t.Fatalf("%s: invocation = %q, want args containing %q", label, last, wantArgs)
		}
		if !strings.Contains(last, goDir) {
			t.Fatalf("%s: invocation = %q, want resolved PATH containing %q", label, last, goDir)
		}
	}

	// 1. runGo (used by createProject / Start Screen Create)
	if err := runGo(workDir, io.Discard, io.Discard, "mod", "tidy"); err != nil {
		t.Fatalf("runGo: %v", err)
	}
	checkLast("runGo", "mod tidy")

	// 2. isStdPackage (used by createProject / projectData)
	std, err := isStdPackage("my-game")
	if err != nil || std {
		t.Fatalf("isStdPackage = (%v, %v), want (false, nil)", std, err)
	}
	checkLast("isStdPackage", "list -e -f {{.Standard}} -- my-game")

	// 3. runGoIn (used by game editor launching a project's own editor)
	if err := runGoIn(workDir, []string{"run", "./cmd/editor"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("runGoIn: %v", err)
	}
	checkLast("runGoIn", "run ./cmd/editor")

	// 4. runNSGo (used by game release)
	if err := runNSGo([]string{"tool", "nsgo", "build"}, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("runNSGo: %v", err)
	}
	checkLast("runNSGo", "tool nsgo build")

	// 5. runNSGoDev (used by game dev)
	if err := runNSGoDev(context.Background(), devRun{
		args:   []string{"tool", "nsgo", "dev"},
		stdout: io.Discard,
		stderr: io.Discard,
	}); err != nil {
		t.Fatalf("runNSGoDev: %v", err)
	}
	checkLast("runNSGoDev", "tool nsgo dev")

	// 6. runGoToolNSGoEnv (used by game test / check / bench)
	var out bytes.Buffer
	if err := runGoToolNSGoEnv([]string{"FOO=1"}, []string{"check", "./..."}, &out, &out); err != nil {
		t.Fatalf("runGoToolNSGoEnv: %v", err)
	}
	checkLast("runGoToolNSGoEnv", "tool nsgo check ./...")
}
