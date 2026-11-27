package refs_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/refs"
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

func TestAssetUsageRootDirRefAndNilProject(t *testing.T) {
	var nilProj *refs.Project
	if nilProj.Paths() != nil || nilProj.UsedBy("a.png") != nil || nilProj.MissingAssets() != nil || nilProj.UnreferencedAssets() != nil {
		t.Fatal("nil Project methods should return nil")
	}

	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte(`version: 1
name: DirAtlas
id: dev.nsgo.diratlas
appVersion: 0.1.0
window:
  width: 640
  height: 360
assets:
  atlases:
    - name: ui
      in: art/ui
      out: built/ui
`)},
		"art/ui/heart.png":   {Data: []byte("png")},
		"art/orphan.png":     {Data: []byte("png")},
		"built/ui/sheet.png": {Data: []byte("png")},
	}
	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if used := proj.UsedBy("art/ui/heart.png"); len(used) != 1 {
		t.Fatalf("UsedBy(art/ui/heart.png) = %+v, want 1 dir ref", used)
	}
	if unused := proj.UnreferencedAssets(); !slices.Equal(unused, []string{"art/orphan.png"}) {
		t.Fatalf("UnreferencedAssets() = %v, want [art/orphan.png]", unused)
	}

	// Verify root "." Dir ref matches every asset path in UsedBy and UnreferencedAssets.
	proj.Refs = append(proj.Refs, refs.Ref{File: "game.yaml", Field: "assets atlas in", Target: ".", Dir: true})
	if used := proj.UsedBy("art/orphan.png"); len(used) == 0 {
		t.Fatalf("UsedBy(art/orphan.png) = empty, want match for root dir ref \".\"")
	}
	if unused := proj.UnreferencedAssets(); len(unused) != 0 {
		t.Fatalf("UnreferencedAssets() = %v, want empty when root dir ref \".\" covers project", unused)
	}
}

func TestTSXClassificationModScriptsVsTiledTilesets(t *testing.T) {
	if refs.IsAssetPath("mods/x/mod.tsx") {
		t.Fatal("IsAssetPath(mods/x/mod.tsx) = true, want false")
	}
	if refs.IsAssetPath("maps/tiles.tsx") {
		t.Fatal("IsAssetPath(maps/tiles.tsx) = true, want false (ambiguous without project context)")
	}
	if !refs.IsAssetPath("maps/tiles.tsj") || !refs.IsAssetPath("art/hero.png") {
		t.Fatal("IsAssetPath should still accept unambiguous asset extensions")
	}
	var nilProj *refs.Project
	if nilProj.IsAsset("mods/x/mod.tsx") || !nilProj.IsAsset("art/hero.png") {
		t.Fatal("nil Project.IsAsset should fall back to IsAssetPath")
	}

	const tiledXML = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="cave" tilewidth="16" tileheight="16" tilecount="4" columns="2">
 <image source="cave.png" width="32" height="32"/>
</tileset>
`
	const modScript = `import { Text, View } from "nsgo";
import { onStep } from "game";

onStep(() => {});
export default function () { return <View><Text>hi</Text></View>; }
`

	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte(`version: 1
name: ModTSX
id: dev.nsgo.modtsx
appVersion: 0.1.0
window:
  width: 640
  height: 360
`)},
		// Mod with mod.yaml naming mod.tsx as its entry.
		"mods/x/mod.yaml": {Data: []byte(`manifestVersion: 1
id: com.example.x
name: X
version: 1.0.0
entry: mod.tsx
`)},
		"mods/x/mod.tsx":   {Data: []byte(modScript)},
		"mods/x/panel.tsx": {Data: []byte(modScript)},
		// Even if a mod entry's file looks like XML or is named by a map, mod.yaml entry wins.
		"mods/tricky/mod.yaml": {Data: []byte(`manifestVersion: 1
id: com.example.tricky
name: Tricky
version: 1.0.0
entry: ./mod.tsx
`)},
		"mods/tricky/mod.tsx": {Data: []byte(tiledXML)},
		// Standalone Tiled XML tileset (unreferenced -> must be reported as an unreferenced asset).
		"maps/unused_tiles.tsx": {Data: []byte(tiledXML)},
		// Tiled XML tileset with UTF-8 BOM and no XML declaration.
		"maps/bom_tiles.tsx": {Data: append([]byte{0xEF, 0xBB, 0xBF}, []byte(`<tileset name="bom" tilewidth="16" tileheight="16"/>`)...)},
		// Tiled XML tileset referenced by a .tmj map (and another missing .tsx referenced by .tmj).
		"maps/level.tmj":      {Data: []byte(`{"type":"map","tilesets":[{"firstgid":1,"source":"used_tiles.tsx"},{"firstgid":10,"source":"missing_tiles.tsx"}]}`)},
		"maps/used_tiles.tsx": {Data: []byte(tiledXML)},
		// Non-XML stub .tsx referenced by a .tmx map still classifies as a tileset via map reference.
		"maps/cave.tmx": {Data: []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" width="2" height="2" tilewidth="16" tileheight="16">
 <tileset firstgid="1" source="from_tmx.tsx"/>
</map>`)},
		"maps/from_tmx.tsx": {Data: []byte("stub")},
	}

	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, notAsset := range []string{"mods/x/mod.tsx", "mods/x/panel.tsx", "mods/tricky/mod.tsx"} {
		if proj.IsAsset(notAsset) {
			t.Errorf("IsAsset(%q) = true, want false", notAsset)
		}
	}
	for _, wantAsset := range []string{"maps/unused_tiles.tsx", "maps/bom_tiles.tsx", "maps/used_tiles.tsx", "maps/missing_tiles.tsx", "maps/from_tmx.tsx"} {
		if !proj.IsAsset(wantAsset) {
			t.Errorf("IsAsset(%q) = false, want true", wantAsset)
		}
	}

	gotUnused := proj.UnreferencedAssets()
	wantUnused := []string{"maps/bom_tiles.tsx", "maps/from_tmx.tsx", "maps/unused_tiles.tsx"}
	if !slices.Equal(gotUnused, wantUnused) {
		t.Fatalf("UnreferencedAssets() = %v, want %v", gotUnused, wantUnused)
	}

	gotMissing := proj.MissingAssets()
	wantMissing := []string{"maps/missing_tiles.tsx"}
	if !slices.Equal(gotMissing, wantMissing) {
		t.Fatalf("MissingAssets() = %v, want %v", gotMissing, wantMissing)
	}
}
