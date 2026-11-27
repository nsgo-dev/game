package main

import (
	"bytes"
	_ "embed"
	"os"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

//go:embed nsgo.yaml
var nsgoYAML []byte

// TestProjectFileIsValidAndCanonical checks that game.yaml is in the form gameproject fmt writes, that nsgo.yaml agrees with it and that its autoloads load, as gameproject check and sync -check do.
func TestProjectFileIsValidAndCanonical(t *testing.T) {
	fsys := fstest.MapFS{project.FileName: {Data: gameYAML}, project.NSGoFileName: {Data: nsgoYAML}}
	level, err := os.ReadFile("resources/level1.tmj")
	if err != nil {
		t.Fatal(err)
	}
	fsys["resources/level1.tmj"] = &fstest.MapFile{Data: level}
	if _, err := project.Check(fsys); err != nil {
		t.Fatal(err)
	}
	if canonical, err := project.Encode(gameProject); err != nil || !bytes.Equal(canonical, gameYAML) {
		t.Fatalf("game.yaml is not canonical (%v); run gameproject fmt:\n%s", err, canonical)
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
	app := nsgotest.Start(t, Root, caveRunTestMediaOptions())
	if app.Title != gameProject.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, gameProject.Title())
	}
}
