package editormcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func TestDefaultPlaytestRunnerReceivesResolvedGoEnv(t *testing.T) {
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
	logFile := filepath.Join(t.TempDir(), "playtest-go.log")

	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportFixture := filepath.Join(t.TempDir(), "fixture-report.yaml")
	reportBytes, err := playtest.EncodeReport(&playtest.Report{
		Version:   playtest.ReportVersion,
		Scope:     playtest.Scope{Scene: "scenes/main.scene.yaml"},
		Bot:       playtest.BotConfig{Profile: "platformer-runner"},
		Rate:      60,
		Speed:     1,
		SeedCount: 1,
		Passed:    true,
		Runs:      []playtest.RunReport{{Index: 1, Seed: 1, Outcome: playtest.OutcomeFinished, Steps: 10, DurationSeconds: 0.2}},
	})
	if err != nil {
		t.Fatalf("EncodeReport: %v", err)
	}
	if err := os.WriteFile(reportFixture, reportBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  echo 'go version go1.27.4 darwin/arm64'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf 'ARGS=%s|PATH=%s\\n' \"$*\" \"$PATH\" >> \"" + logFile + "\"\n" +
		"if [ -n \"$" + playtest.EnvOut + "\" ]; then\n" +
		"  while IFS= read -r line || [ -n \"$line\" ]; do\n" +
		"    printf '%s\\n' \"$line\"\n" +
		"  done < \"" + reportFixture + "\" > \"$" + playtest.EnvOut + "\"\n" +
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

	projRoot := t.TempDir()
	cfgPath := filepath.Join(projRoot, "playtest.yaml")
	outPath := filepath.Join(projRoot, "reports", "latest.yaml")
	rep, err := defaultRunPlaytest(context.Background(), PlaytestRunRequest{
		ProjectRoot: projRoot,
		ConfigFile:  "playtest.yaml",
		ConfigPath:  cfgPath,
		OutFile:     "reports/latest.yaml",
		OutPath:     outPath,
		Pattern:     "TestPlaytest",
		Config:      &playtest.Config{},
	})
	if err != nil {
		t.Fatalf("defaultRunPlaytest: %v", err)
	}
	if rep == nil {
		t.Fatal("defaultRunPlaytest returned nil report")
	}

	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read playtest go log: %v", err)
	}
	line := strings.TrimSpace(string(logged))
	if !strings.Contains(line, "tool nsgo test -run TestPlaytest ./... -count=1") {
		t.Fatalf("playtest invocation = %q, want nsgo test args", line)
	}
	if !strings.Contains(line, goDir) {
		t.Fatalf("playtest invocation = %q, want resolved PATH containing %q", line, goDir)
	}
}
