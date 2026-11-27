package main

import (
	"bytes"
	_ "embed"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

//go:embed nsgo.yaml
var nsgoYAML []byte

// TestProjectFileIsValidAndCanonical checks that game.yaml is in the form project.Encode writes and that nsgo.yaml agrees with it.
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

// TestTouchLayoutIsCanonical checks that controls.touch.yaml is in the form input.EncodeTouchLayout writes, so a layout the game saves for the player starts from the same text.
func TestTouchLayoutIsCanonical(t *testing.T) {
	canonical, err := input.EncodeTouchLayout(touchLayout)
	if err != nil || !bytes.Equal(canonical, touchLayoutYAML) {
		t.Fatalf("controls.touch.yaml is not canonical (%v); write it as input.EncodeTouchLayout does:\n%s", err, canonical)
	}
}

func TestRootTitlesTheWindowFromTheProject(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(320, 180), nsgotest.ManualFrames())
	if app.Title != gameProject.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, gameProject.Title())
	}
}
