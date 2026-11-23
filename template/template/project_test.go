package main

import (
	"bytes"
	_ "embed"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/project"
)

//go:embed nsgo.yaml
var nsgoYAML []byte

// TestProjectFileIsValidAndCanonical checks that game.yaml is in the form gameproject fmt writes and that nsgo.yaml agrees with it, as gameproject check and sync -check do.
func TestProjectFileIsValidAndCanonical(t *testing.T) {
	if _, err := project.Check(fstest.MapFS{project.FileName: {Data: gameYAML}, project.NSGoFileName: {Data: nsgoYAML}}); err != nil {
		t.Fatal(err)
	}
	if canonical, err := project.Encode(Project); err != nil || !bytes.Equal(canonical, gameYAML) {
		t.Fatalf("game.yaml is not canonical (%v); run gameproject fmt:\n%s", err, canonical)
	}
}
