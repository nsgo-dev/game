package main

import (
	"bytes"
	_ "embed"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

//go:embed nsgo.yaml
var nsgoYAML []byte

// TestProjectFileIsValidAndCanonical checks that game.yaml is in the form
// project.Encode writes and that nsgo.yaml agrees with it.
func TestProjectFileIsValidAndCanonical(t *testing.T) {
	fsys := fstest.MapFS{
		project.FileName:     {Data: gameYAML},
		project.NSGoFileName: {Data: nsgoYAML},
	}
	if _, err := project.Check(fsys); err != nil {
		t.Fatal(err)
	}
	canonical, err := project.Encode(gameProject)
	if err != nil || !bytes.Equal(canonical, gameYAML) {
		t.Fatalf("game.yaml is not canonical (%v):\n%s", err, canonical)
	}
}

func TestRootTitlesTheWindowFromTheProject(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(320, 180), nsgotest.ManualFrames())
	if app.Title != gameProject.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, gameProject.Title())
	}
}
