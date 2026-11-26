package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildAndHelp(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "game")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/game: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "help").CombinedOutput()
	if err != nil {
		t.Fatalf("game help: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "usage: game <command>") {
		t.Errorf("game help output = %q, want usage line", out)
	}
}
