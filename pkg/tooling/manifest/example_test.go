package manifest_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/tooling/manifest"
)

// Scan lists a game's assets with the typed Go names the generated manifest gives them, so code refers to assets by checked identifiers instead of strings; Verify reports references to files that are missing.
func ExampleScan() {
	dir, err := os.MkdirTemp("", "assets")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"hero.png", "sounds/jump.wav", "levels/level-1.tmj"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("x"), 0o644)
	}

	entries, err := manifest.Scan(dir, manifest.DefaultOptions())
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, e := range entries {
		fmt.Println(e.Ident, e.URI, e.Kind)
	}
	fmt.Println(manifest.Verify(dir, []string{"asset:hero.png"}))
	fmt.Println(manifest.Verify(dir, []string{"asset:villain.png"}) != nil)
	// Output:
	// ImageHero asset:hero.png image
	// MapLevelsLevel1 asset:levels/level-1.tmj map
	// SoundSoundsJump asset:sounds/jump.wav audio
	// <nil>
	// true
}
