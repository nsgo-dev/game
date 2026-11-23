package rooms_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/rooms"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestFromLDtkReadsLevels(t *testing.T) {
	const project = `{"worldLayout":"GridVania","levels":[
		{"identifier":"Entrance","worldX":0,"worldY":0,"pxWid":320,"pxHei":180,"layerInstances":[]},
		{"identifier":"Hall","worldX":320,"worldY":0,"pxWid":640,"pxHei":180}
	]}`
	got, err := rooms.FromLDtk([]byte(project), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []rooms.Room{
		{ID: "Entrance", Width: 320, Height: 180},
		{ID: "Hall", X: 320, Width: 640, Height: 180},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rooms = %+v", got)
	}
	m := &rooms.Map{Version: rooms.FormatVersion, Transition: &rooms.Transition{Kind: rooms.KindSlide}, Rooms: got}
	if err := rooms.Validate(m); err != nil {
		t.Fatal(err)
	}
}

func TestFromLDtkMultiWorlds(t *testing.T) {
	const project = `{"worldLayout":null,"levels":[],"worlds":[
		{"identifier":"Surface","worldLayout":"Free","levels":[{"identifier":"Field","worldX":-100,"worldY":40,"pxWid":200,"pxHei":100}]},
		{"identifier":"Depths","worldLayout":"GridVania","levels":[{"identifier":"Cave","worldX":0,"worldY":0,"pxWid":64,"pxHei":64}]}
	]}`
	got, err := rooms.FromLDtk([]byte(project), "Surface")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (rooms.Room{ID: "Field", X: -100, Y: 40, Width: 200, Height: 100}) {
		t.Fatalf("rooms = %+v", got)
	}
	for world, msg := range map[string]string{"": "name one", "Sky": "no world"} {
		if _, err := rooms.FromLDtk([]byte(project), world); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("FromLDtk(%q) error = %v, want %q", world, err, msg)
		}
	}
}

func TestFromLDtkRefusesUnusableProjects(t *testing.T) {
	cases := map[string]string{
		"json":   `{`,
		"linear": `{"worldLayout":"LinearHorizontal","levels":[{"identifier":"A","worldX":-1,"worldY":-1,"pxWid":10,"pxHei":10}]}`,
		"empty":  `{"worldLayout":"Free","levels":[]}`,
		"no id":  `{"levels":[{"identifier":"","pxWid":10,"pxHei":10}]}`,
		"size":   `{"levels":[{"identifier":"A","pxWid":0,"pxHei":10}]}`,
	}
	for name, src := range cases {
		if _, err := rooms.FromLDtk([]byte(src), ""); err == nil {
			t.Errorf("%s: FromLDtk succeeded", name)
		}
	}
}

func TestFromTiledLayer(t *testing.T) {
	objs := []tilemap.Object{
		{ID: 3, Name: "entrance", Shape: tilemap.ObjectRect, X: 0, Y: 0, Width: 320, Height: 180},
		{ID: 7, Name: "hall", Shape: tilemap.ObjectRect, X: 320, Y: 0, Width: 640, Height: 180},
	}
	got, err := rooms.FromTiledLayer(&tilemap.ObjectLayer{Objects: objs})
	if err != nil {
		t.Fatal(err)
	}
	want := []rooms.Room{{ID: "entrance", Width: 320, Height: 180}, {ID: "hall", X: 320, Width: 640, Height: 180}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rooms = %+v", got)
	}

	bad := map[string]tilemap.Object{
		"ellipse": {ID: 1, Name: "a", Shape: tilemap.ObjectEllipse, Width: 10, Height: 10},
		"tile":    {ID: 1, Name: "a", Shape: tilemap.ObjectRect, GID: 5, Width: 10, Height: 10},
		"rotated": {ID: 1, Name: "a", Shape: tilemap.ObjectRect, Rotation: 45, Width: 10, Height: 10},
		"no name": {ID: 1, Shape: tilemap.ObjectRect, Width: 10, Height: 10},
		"empty":   {ID: 1, Name: "a", Shape: tilemap.ObjectRect},
		"polygon": {ID: 1, Name: "a", Shape: tilemap.ObjectPolygon, Points: []core.Point{{}, {X: 1}, {Y: 1}}},
	}
	for name, o := range bad {
		if _, err := rooms.FromTiledLayer(&tilemap.ObjectLayer{Objects: []tilemap.Object{o}}); err == nil {
			t.Errorf("%s: FromTiledLayer succeeded", name)
		}
	}
	if _, err := rooms.FromTiledLayer(nil); err == nil {
		t.Error("nil layer: FromTiledLayer succeeded")
	}
}

func TestFromTiledLayerAddsTheLayerOffset(t *testing.T) {
	src := `{"orientation":"orthogonal","width":4,"height":4,"tilewidth":16,"tileheight":16,"layers":[
		{"type":"group","name":"world","offsetx":100,"offsety":40,"layers":[
			{"type":"objectgroup","name":"rooms","offsetx":16,"offsety":-8,"objects":[
				{"id":1,"name":"hall","x":0,"y":10,"width":320,"height":180}
			]}
		]}
	]}`
	tm, err := tilemap.LoadTiled([]byte(src), tilemap.TilesetLoader{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tm.Objects) != 1 {
		t.Fatalf("object layers = %d", len(tm.Objects))
	}
	got, err := rooms.FromTiledLayer(&tm.Objects[0])
	if err != nil {
		t.Fatal(err)
	}
	want := []rooms.Room{{ID: "hall", X: 116, Y: 42, Width: 320, Height: 180}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rooms = %+v, want %+v", got, want)
	}
}
