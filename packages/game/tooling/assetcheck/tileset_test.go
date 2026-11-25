package assetcheck_test

import (
	"strings"
	"testing"
	"testing/fstest"
)

const (
	caveTilesetYAML = `version: 1
name: cave
tileWidth: 16
tileHeight: 16
image: tiles/cave.png
columns: 2
tileCount: 4
tiles:
  - id: 0
    solid: true
`
	propsTilesetYAML = `version: 1
tileWidth: 16
tileHeight: 16
atlas: actors
frames:
  - hero
`
	layeredScene = `version: 2
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 3
      height: 1
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 1h 3
  - name: props
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 1
      height: 1
      tilesetFile: tilesets/props.tileset.yaml
      tileLayers:
        - name: props
          rows:
            - "0"
  - name: world
    tilemap:
      source: world.ldtk
      level: Level_0
`
)

// tilesetProject is project with YAML tilesets, one cut from a grid image and one of atlas frames, and a scene that places their tiles in layered grids beside an LDtk level.
func tilesetProject(t *testing.T) fstest.MapFS {
	fsys := project(t)
	fsys["tilesets/cave.tileset.yaml"] = &fstest.MapFile{Data: []byte(caveTilesetYAML)}
	fsys["tilesets/props.tileset.yaml"] = &fstest.MapFile{Data: []byte(propsTilesetYAML)}
	fsys["tiles/cave.png"] = &fstest.MapFile{Data: pngBytes(t, 32, 32)}
	fsys["scenes/layered.scene.yaml"] = &fstest.MapFile{Data: []byte(layeredScene)}
	return fsys
}

func TestYAMLTilesetsAndLayeredTilemaps(t *testing.T) {
	r := mustCheck(t, tilesetProject(t))
	if !r.OK() {
		t.Fatalf("problems in a clean project:\n%s", problems(r))
	}
	if r.Scenes != 2 || r.Tilesets != 3 {
		t.Fatalf("counted %d scenes and %d tilesets, want 2 and 3", r.Scenes, r.Tilesets)
	}

	cases := []struct {
		name, file, old, new  string
		wantFile, marker, msg string
	}{
		{"tile outside the tileset", "scenes/layered.scene.yaml", "- 0 1h 3", "- 0 1h 4", "scenes/layered.scene.yaml", "- 0 1h 4", `tile layer "floor" row 0 column 2 places tile 4, but tileset "tilesets/cave.tileset.yaml" (tilesets/cave.tileset.yaml) has 4 tiles`},
		{"missing tileset file", "scenes/layered.scene.yaml", "tilesetFile: tilesets/cave.tileset.yaml", "tilesetFile: tilesets/rock.tileset.yaml", "scenes/layered.scene.yaml", "tilesets/rock.tileset.yaml", `tileset file "tilesets/rock.tileset.yaml": not found`},
		{"invalid tileset", "tilesets/cave.tileset.yaml", "columns: 2", "columns: 0", "tilesets/cave.tileset.yaml", "columns: 0", "columns 0 must be positive"},
		{"tileset unknown field", "tilesets/cave.tileset.yaml", "name: cave", "name: cave\nsize: 3", "tilesets/cave.tileset.yaml", "size: 3", "field size not found"},
		{"missing tileset image", "tilesets/cave.tileset.yaml", "image: tiles/cave.png", "image: tiles/rock.png", "tilesets/cave.tileset.yaml", "image:", `tileset image "tiles/rock.png": not found`},
		{"tileset image too small", "tilesets/cave.tileset.yaml", "tileCount: 4", "tileCount: 6", "tilesets/cave.tileset.yaml", "image:", `tileset image "tiles/cave.png" is 32x32, smaller than the 32x48`},
		{"tileset frame not in atlas", "tilesets/props.tileset.yaml", "  - hero", "  - hero\n  - villain", "tilesets/props.tileset.yaml", "- villain", `frame "villain" is not in tileset atlas "actors" (atlases/actors.json)`},
		{"unknown tileset atlas", "tilesets/props.tileset.yaml", "atlas: actors", "atlas: heroes", "tilesets/props.tileset.yaml", "atlas: heroes", `tileset atlas "heroes" not found`},
		{"unknown ldtk level", "scenes/layered.scene.yaml", "level: Level_0", "level: Level_9", "scenes/layered.scene.yaml", "level: Level_9", `tilemap level "Level_9"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := tilesetProject(t)
			text := string(fsys[tc.file].Data)
			if !strings.Contains(text, tc.old) {
				t.Fatalf("%s does not contain %q", tc.file, tc.old)
			}
			fsys[tc.file] = &fstest.MapFile{Data: []byte(strings.Replace(text, tc.old, tc.new, 1))}
			r := mustCheck(t, fsys)
			if len(r.Problems) != 1 {
				t.Fatalf("%d problems, want 1:\n%s", len(r.Problems), problems(r))
			}
			p := r.Problems[0]
			want := lineOf(t, string(fsys[tc.wantFile].Data), tc.marker)
			if p.File != tc.wantFile || p.Line != want || !strings.Contains(p.Message, tc.msg) {
				t.Fatalf("problem %q, want %s:%d with %q", p, tc.wantFile, want, tc.msg)
			}
		})
	}
}

const (
	caveTMX = `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down" width="2" height="1" tilewidth="16" tileheight="16" infinite="0">
 <tileset firstgid="1" source="tilesets/cave.tsx"/>
 <layer id="1" name="ground" width="2" height="1">
  <data encoding="csv">1,2</data>
 </layer>
</map>
`
	caveTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="cave" tilewidth="16" tileheight="16" tilecount="2" columns="2">
 <image source="../art/cave.png" width="32" height="16"/>
</tileset>
`
)

func TestTMXTilemapSources(t *testing.T) {
	tmxProject := func() fstest.MapFS {
		fsys := project(t)
		fsys["maps/cave.tmx"] = &fstest.MapFile{Data: []byte(caveTMX)}
		fsys["maps/tilesets/cave.tsx"] = &fstest.MapFile{Data: []byte(caveTSX)}
		fsys["maps/art/cave.png"] = &fstest.MapFile{Data: pngBytes(t, 32, 16)}
		fsys["scenes/cave.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 1\nlayers:\n  - name: ground\n    tilemap:\n      source: maps/cave.tmx\n")}
		return fsys
	}
	r := mustCheck(t, tmxProject())
	if !r.OK() {
		t.Fatalf("problems in a clean project:\n%s", problems(r))
	}
	if r.Maps != 3 {
		t.Fatalf("counted %d maps, want 3", r.Maps)
	}

	for _, tc := range []struct {
		name, file, old, new, msg string
	}{
		{"missing external tileset", "maps/cave.tmx", "tilesets/cave.tsx", "tilesets/rock.tsx", "rock.tsx"},
		{"missing tileset image", "maps/tilesets/cave.tsx", "../art/cave.png", "../art/rock.png", `image "art/rock.png": not found`},
		{"not a map", "maps/cave.tmx", "<map ", "<mop ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := tmxProject()
			text := string(fsys[tc.file].Data)
			if !strings.Contains(text, tc.old) {
				t.Fatalf("%s does not contain %q", tc.file, tc.old)
			}
			fsys[tc.file] = &fstest.MapFile{Data: []byte(strings.Replace(text, tc.old, tc.new, 1))}
			r := mustCheck(t, fsys)
			if len(r.Problems) != 1 || r.Problems[0].File != "maps/cave.tmx" || !strings.Contains(r.Problems[0].Message, tc.msg) {
				t.Fatalf("want one problem in maps/cave.tmx with %q:\n%s", tc.msg, problems(r))
			}
		})
	}
}
