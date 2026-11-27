package tileshape_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/lights2d"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/tileshape"
)

// hill is a Tiled map whose tileset gives tile 2 a 45 degree slope in its collision editor; tile 1 is solid.
const hill = `{
	"orientation": "orthogonal", "width": 4, "height": 2, "tilewidth": 16, "tileheight": 16,
	"layers": [{"type": "tilelayer", "name": "ground", "width": 4, "height": 2,
		"data": [0, 2, 1, 2147483650,
		         1, 1, 1, 1]}],
	"tilesets": [{"firstgid": 1, "name": "terrain", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 2,
		"tiles": [
			{"id": 0, "properties": [{"name": "solid", "type": "bool", "value": true}]},
			{"id": 1, "objectgroup": {"objects": [{"id": 1, "x": 0, "y": 0, "polygon": [{"x": 0, "y": 16}, {"x": 16, "y": 0}, {"x": 16, "y": 16}]}]}}
		]}]
}`

// A View turns a Tiled map's solid tiles and slope shapes into a physics grid, slopes and light occluders.
func Example() {
	m, err := tilemap.LoadTiled([]byte(hill), tilemap.TilesetLoader{})
	if err != nil {
		fmt.Println(err)
		return
	}
	v := tileshape.New(m, tileshape.Options{})

	// The slope tiles collide through slopes, one of them flipped by its GID's flip bit.
	for _, s := range v.Slopes() {
		fmt.Println("slope", s.A, s.B)
	}

	// The view is the TileGrid a character walks with those slopes.
	body := physics.NewCharacterBody(physics.NewBoxBody(1, 0, 12, 16))
	for range 30 {
		body.Body.VX = 60
		body.Body.VY += 900 * float32(time.Second.Seconds()/60)
		body.Move(v, v.Slopes(), nil, time.Second/60)
	}
	b := body.Body.Bounds()
	fmt.Printf("character at x=%.1f, feet at y=%.1f, grounded %v\n", b.X, b.Bottom(), body.Grounded)

	// Lights cast shadows from the outline, which follows a tile change.
	scene := lights2d.Scene{Occluders: v.Occluders()}
	fmt.Println("outline segments", len(v.Outline()), "in", len(scene.Occluders), "occluder")
	m.SetTile(0, 2, 0, 0)
	fmt.Println("after removing a tile", len(v.Outline()))
	// Output:
	// slope {16 16} {32 0}
	// slope {48 0} {64 16}
	// character at x=31.0, feet at y=0.0, grounded true
	// outline segments 7 in 1 occluder
	// after removing a tile 9
}
