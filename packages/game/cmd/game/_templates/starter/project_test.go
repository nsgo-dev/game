package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestProjectFileIsValidAndCanonical checks the project as gameproject check does, game.yaml, nsgo.yaml beside it and the scenes and resources game.yaml autoloads, and that game.yaml is in the form gameproject fmt writes.
func TestProjectFileIsValidAndCanonical(t *testing.T) {
	if _, err := project.Check(os.DirFS(".")); err != nil {
		t.Fatal(err)
	}
	if canonical, err := project.Encode(Project); err != nil || !bytes.Equal(canonical, gameYAML) {
		t.Fatalf("game.yaml is not canonical (%v); run gameproject fmt:\n%s", err, canonical)
	}
}

func TestRootTitlesTheWindowFromTheProject(t *testing.T) {
	app := nsgotest.Start(t, Root)
	if app.Title != Project.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, Project.Title())
	}
}
