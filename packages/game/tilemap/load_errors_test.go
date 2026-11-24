package tilemap_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestLoadTiledErrorsNameFileLineAndFix(t *testing.T) {
	base := `"tilewidth": 16, "tileheight": 16, "width": 2, "height": 1`
	cases := []struct {
		name, json, field string
	}{
		{"orientation", `{` + base + `, "orientation": "diagonal"}`, "orientation"},
		{"tile size", `{"width": 2, "height": 1, "tilewidth": 0, "tileheight": 16}`, "tilewidth"},
		{"negative size", `{"tilewidth": 16, "tileheight": 16, "width": -1, "height": 1}`, "width"},
		{"firstgid", `{` + base + `, "tilesets": [{"name": "t"}]}`, "tilesets[0].firstgid"},
		{"property", `{` + base + `, "properties": [{"name": "n", "type": "int", "value": "x"}]}`, "properties[0].value"},
		{"tile animation", `{` + base + `, "tilesets": [{"firstgid": 1, "tiles": [{"id": 0, "animation": [{"tileid": 0, "duration": -5}]}]}]}`, "tilesets[0].tiles[0].animation[0].duration"},
		{"layer data length", `{` + base + `, "layers": [{"type": "tilelayer", "name": "g", "data": [1]}]}`, "layers[0].data"},
		{"compressed layer", `{` + base + `, "layers": [{"type": "tilelayer", "name": "g", "encoding": "base64"}]}`, "layers[0].encoding"},
		{"nested object property", `{` + base + `, "layers": [{"type": "group", "layers": [{"type": "objectgroup", "objects": [{"id": 1, "properties": [{"name": "b", "type": "bool", "value": 3}]}]}]}]}`, "layers[0].layers[0].objects[0].properties[0].value"},
		{"chunk size", `{` + base + `, "infinite": true, "layers": [{"type": "tilelayer", "chunks": [{"x": 0, "y": 0, "width": 0, "height": 16, "data": []}]}]}`, "layers[0].chunks[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "cave.tmj"), "cave.tmj", data, tc.field)
		})
	}

	t.Run("external tileset names its own file", func(t *testing.T) {
		tsj := loaderrtest.Indent(`{"name": "t", "tiles": [{"id": 0, "properties": [{"name": "solid", "type": "bool", "value": "yes"}]}]}`)
		data := loaderrtest.Indent(`{` + base + `, "tilesets": [{"firstgid": 1, "source": "tiles/cave.tsj"}]}`)
		_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{LoadJSON: func(string) ([]byte, error) { return tsj, nil }})
		loaderrtest.ExpectField(t, loaderr.WithFile(err, "cave.tmj"), "tiles/cave.tsj", tsj, "tiles[0].properties[0].value")
	})

	t.Run("syntax", func(t *testing.T) {
		_, err := tilemap.LoadTiled([]byte("{\n  \"width\": 2,\n  \"height\" 1\n}"), tilemap.TilesetLoader{})
		loaderrtest.Expect(t, loaderr.WithFile(err, "cave.tmj"), "cave.tmj", 3)
	})
}

func TestLoadLDtkErrorsNameFileLineAndFix(t *testing.T) {
	noImage := func(string) core.ImageSource { return core.ImageSource{} }
	level := `"identifier": "L0", "pxWid": 32, "pxHei": 16`
	cases := []struct {
		name, json, level, field string
	}{
		{"no levels", `{"defaultGridSize": 16, "levels": []}`, "", "levels"},
		{"missing level", `{"defaultGridSize": 16, "levels": [{` + level + `}]}`, "L9", "levels"},
		{"grid size", `{"levels": [{` + level + `}]}`, "", "levels[0]"},
		{"level field", `{"defaultGridSize": 16, "levels": [{` + level + `, "fieldInstances": [{"__identifier": "hp", "__type": "Int", "__value": "lots"}]}]}`, "", "levels[0].fieldInstances[0].__value"},
		{"entity field", `{"defaultGridSize": 16, "levels": [{` + level + `, "layerInstances": [{"__type": "Entities", "__gridSize": 16, "entityInstances": [{"fieldInstances": [{"__identifier": "on", "__type": "Bool", "__value": 2}]}]}]}]}`, "", "levels[0].layerInstances[0].entityInstances[0].fieldInstances[0].__value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := tilemap.LoadLDtk(data, tc.level, noImage)
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "world.ldtk"), "world.ldtk", data, tc.field)
			if tc.level == "" {
				_, err = tilemap.LoadLDtkAll(data, noImage)
				if tc.name != "no levels" {
					loaderrtest.ExpectField(t, loaderr.WithFile(err, "world.ldtk"), "world.ldtk", data, tc.field)
				}
			}
		})
	}
}
