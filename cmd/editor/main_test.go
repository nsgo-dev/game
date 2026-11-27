package main

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEditorArgs(t *testing.T) {
	got := editorArgs([]string{"-psn_0_12345", "scenes/main.scene.yaml"})
	want := []string{"editor", "scenes/main.scene.yaml"}
	if !slices.Equal(got, want) {
		t.Fatalf("editorArgs = %v, want %v", got, want)
	}

	gotEmpty := editorArgs(nil)
	wantEmpty := []string{"editor"}
	if !slices.Equal(gotEmpty, wantEmpty) {
		t.Fatalf("editorArgs(nil) = %v, want %v", gotEmpty, wantEmpty)
	}
}

func TestEditorBinaryBuildsAndShowsHelp(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "nsgo-game-editor")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/editor: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "-help").CombinedOutput()
	if err != nil {
		t.Fatalf("editor -help: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "usage: game editor") {
		t.Errorf("editor -help output = %q, want usage line", out)
	}
}
