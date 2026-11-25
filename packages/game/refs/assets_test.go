package refs_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/refs"
)

func TestAssetUsageUnreferencedAndMissing(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte(`version: 1
name: DungeonRun
id: dev.nsgo.dungeon
appVersion: 0.1.0
window:
  width: 640
  height: 360
assets:
  textures:
    - in: art/props/coin.png
      out: built/coin.ktx2
  audio:
    - in: audio/sfx/jump.wav
      out: built/jump.ogg
  fonts:
    - in: fonts/PixelOperator.ttf
      out: built/pixel.fnt
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`)},
		"scenes/level_01.scene.yaml": {Data: []byte(`version: 2
layers:
  - name: actors
    objects:
      - id: door
        material: materials/door.material.yaml
`)},
		"materials/door.material.yaml": {Data: []byte(`type: material
version: 1
name: Door
texture: art/props/door_open.png
`)},
		"screens/hud.screen.yaml": {Data: []byte(`version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: hearts
    type: sprite_row
    sprite: art/ui/heart.png
`)},
		"tilesets/stone.tileset.yaml": {Data: []byte(`version: 1
tileWidth: 16
tileHeight: 16
image: art/tiles/stone_tiles.png
`)},
		"art/props/coin.png":             {Data: []byte("png")},
		"art/props/torch_old.png":        {Data: []byte("png")},
		"art/props/coin.png.import.yaml": {Data: []byte("version: 1\n")},
		"art/ui/heart.png":               {Data: []byte("png")},
		"art/tiles/stone_tiles.png":      {Data: []byte("png")},
		"audio/sfx/jump.wav":             {Data: []byte("wav")},
		"audio/sfx/unused_beep.wav":      {Data: []byte("wav")},
		"fonts/PixelOperator.ttf":        {Data: []byte("ttf")},
		"built/coin.ktx2":                {Data: []byte("ktx2")},
		"built/jump.ogg":                 {Data: []byte("ogg")},
	}

	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Missing assets: only door_open.png is referenced by a non-output ref and absent on disk.
	gotMissing := proj.MissingAssets()
	wantMissing := []string{"art/props/door_open.png"}
	if !slices.Equal(gotMissing, wantMissing) {
		t.Fatalf("MissingAssets() = %v, want %v", gotMissing, wantMissing)
	}

	// Unreferenced assets: torch_old.png, unused_beep.wav, and tilesets/stone.tileset.yaml (no scene references the tileset).
	// built/jump.ogg is an assets output so it is excluded; sidecars are excluded.
	gotUnused := proj.UnreferencedAssets()
	wantUnused := []string{"art/props/torch_old.png", "audio/sfx/unused_beep.wav", "tilesets/stone.tileset.yaml"}
	if !slices.Equal(gotUnused, wantUnused) {
		t.Fatalf("UnreferencedAssets() = %v, want %v", gotUnused, wantUnused)
	}

	// UsedBy returns the referring file and field for both existing and missing assets.
	doorRefs := proj.UsedBy("art/props/door_open.png")
	if len(doorRefs) != 1 || doorRefs[0].File != "materials/door.material.yaml" || doorRefs[0].Field != refs.FieldTexture {
		t.Fatalf("UsedBy(door_open.png) = %+v", doorRefs)
	}
	heartRefs := proj.UsedBy("art/ui/heart.png")
	if len(heartRefs) != 1 || heartRefs[0].File != "screens/hud.screen.yaml" || heartRefs[0].Field != refs.FieldScreenSprite {
		t.Fatalf("UsedBy(heart.png) = %+v", heartRefs)
	}
	if len(proj.UsedBy("art/props/torch_old.png")) != 0 {
		t.Fatalf("UsedBy(torch_old.png) = %+v, want empty", proj.UsedBy("art/props/torch_old.png"))
	}
}
