package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryMatchesCC0Adventurers(t *testing.T) {
	root := filepath.Join("..", "..", "library", "packs", "cc0-adventurers")
	models := make([]model, 0, 2)
	for _, name := range []string{"mage", "knight"} {
		m, err := readModel(filepath.Join(root, name+".gltf"))
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Clips) != 12 {
			t.Fatalf("%s has %d clips, want 12", name, len(m.Clips))
		}
		models = append(models, m)
	}
	want := render(models)
	got, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("README.md is stale, run go run ./tools/clipinventory -out library/packs/cc0-adventurers/README.md library/packs/cc0-adventurers/mage.gltf library/packs/cc0-adventurers/knight.gltf")
	}
}
