package worldmap_test

import (
	"image/color"
	"testing"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/worldmap"
	"github.com/nsgo-dev/nsgo/core"
)

func benchTilemap256(orient tilemap.Orientation) *tilemap.Tilemap {
	sheet := solidTileSheet(16, 16,
		color.RGBA{R: 0x35, G: 0x7a, B: 0x38, A: 0xff},
		color.RGBA{R: 0x2b, G: 0x5c, B: 0x9e, A: 0xff},
		color.RGBA{R: 0x8b, G: 0x73, B: 0x55, A: 0xff},
	)
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet}, 16, 16, 3, 3)
	ts.TileProperties = map[uint32]tilemap.Properties{
		2: {worldmap.DefaultTileColorProperty: {Name: worldmap.DefaultTileColorProperty, Type: "color", Value: "#c8a96e"}},
	}
	m := tilemap.New(256, 256, 16, 16, ts)
	m.Orientation = orient
	data := make([]uint32, 256*256)
	for i := range data {
		data[i] = uint32(1 + i%3)
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("terrain", 256, 256, data))
	return m
}

// BenchmarkTileMinimap256x256SingleTileChange verifies acceptance criterion: a 256x256 map's minimap redraws in well under 1 ms when one tile changes.
func BenchmarkTileMinimap256x256SingleTileChange(b *testing.B) {
	m := benchTilemap256(tilemap.OrientationOrthogonal)
	tm := worldmap.NewTileMinimap(m, nil)
	dst := core.Bounds{Size: core.Size{Width: 256, Height: 256}}
	style := worldmap.DefaultTileMinimapStyle()
	cam := &tilemap.Camera{X: 2048, Y: 2048, Width: 640, Height: 360, Zoom: 1}
	overlay := &worldmap.TileMinimapOverlay{
		Camera: cam,
		Markers: []worldmap.TileMarker{
			{Position: math2d.Vec2{X: 512, Y: 512}, Kind: "ally"},
			{Position: math2d.Vec2{X: 1024, Y: 768}, Kind: "poi"},
		},
	}
	var d core.Drawing
	tm.Draw(&d, dst, overlay, &style)

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		d.Ops = d.Ops[:0]
		m.SetTile(0, 128, 128, uint32(1+(i%3)))
		tm.Draw(&d, dst, overlay, &style)
	}
}

func BenchmarkTileMinimap256x256SingleTileChangeWithFog(b *testing.B) {
	m := benchTilemap256(tilemap.OrientationOrthogonal)
	tm := worldmap.NewTileMinimap(m, nil)
	f := fog.New(256, 256, 16, 16)
	f.AddSource(fog.Source{Team: 1, Col: 128, Row: 128, Radius: 24})
	f.Update()

	dst := core.Bounds{Size: core.Size{Width: 256, Height: 256}}
	style := worldmap.DefaultTileMinimapStyle()
	cam := &tilemap.Camera{X: 2048, Y: 2048, Width: 640, Height: 360, Zoom: 1}
	overlay := &worldmap.TileMinimapOverlay{
		Fog:    f,
		Team:   1,
		Camera: cam,
		Markers: []worldmap.TileMarker{
			{Position: math2d.Vec2{X: 2048, Y: 2048}, Kind: "ally", RequireVisible: true},
		},
	}
	var d core.Drawing
	tm.Draw(&d, dst, overlay, &style)

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		d.Ops = d.Ops[:0]
		m.SetTile(0, 128, 128, uint32(1+(i%3)))
		tm.Draw(&d, dst, overlay, &style)
	}
}

func BenchmarkTileMinimap256x256IsometricSingleTileChange(b *testing.B) {
	m := benchTilemap256(tilemap.OrientationIsometric)
	tm := worldmap.NewTileMinimap(m, nil)
	dst := core.Bounds{Size: core.Size{Width: 256, Height: 128}}
	style := worldmap.DefaultTileMinimapStyle()
	cam := &tilemap.Camera{X: 0, Y: 2048, Width: 640, Height: 360, Zoom: 1}
	overlay := &worldmap.TileMinimapOverlay{Camera: cam}
	var d core.Drawing
	tm.Draw(&d, dst, overlay, &style)

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		d.Ops = d.Ops[:0]
		m.SetTile(0, 128, 128, uint32(1+(i%3)))
		tm.Draw(&d, dst, overlay, &style)
	}
}

func BenchmarkTileMinimap256x256UnchangedFrameWithFog(b *testing.B) {
	m := benchTilemap256(tilemap.OrientationOrthogonal)
	tm := worldmap.NewTileMinimap(m, nil)
	f := fog.New(256, 256, 16, 16)
	f.AddSource(fog.Source{Team: 1, Col: 128, Row: 128, Radius: 24})
	f.Update()

	dst := core.Bounds{Size: core.Size{Width: 256, Height: 256}}
	style := worldmap.DefaultTileMinimapStyle()
	overlay := &worldmap.TileMinimapOverlay{Fog: f, Team: 1}
	var d core.Drawing
	tm.Draw(&d, dst, overlay, &style)

	b.ReportAllocs()
	for b.Loop() {
		d.Ops = d.Ops[:0]
		tm.Draw(&d, dst, overlay, &style)
	}
}
