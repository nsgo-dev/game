package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCommittedGLBMatchesGenerator keeps assets/lantern_shrine.glb reproducible from this generator, as CREDITS.md promises: a change to either one without the other fails here. Regenerate it with go run ./examples/lantern3d/tools/genassets/main.go from the repository root.
func TestCommittedGLBMatchesGenerator(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "assets", "lantern_shrine.glb"))
	if err != nil {
		t.Fatalf("read committed glb: %v", err)
	}
	got, err := buildLanternShrineGLB()
	if err != nil {
		t.Fatalf("buildLanternShrineGLB: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generated glb (%d bytes) differs from assets/lantern_shrine.glb (%d bytes); regenerate it with go run ./examples/lantern3d/tools/genassets/main.go from the repository root", len(got), len(want))
	}
}
