package tilemap_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// level is a small Tiled JSON map (.tmj) with an embedded tileset whose tile 1 is marked solid by a custom property.
const level = `{
	"type": "map", "orientation": "orthogonal", "renderorder": "right-down",
	"width": 4, "height": 3, "tilewidth": 16, "tileheight": 16, "infinite": false,
	"layers": [{
		"type": "tilelayer", "name": "ground", "width": 4, "height": 3, "x": 0, "y": 0,
		"opacity": 1, "visible": true,
		"data": [0, 0, 0, 0,
		         0, 0, 0, 2,
		         2, 2, 2, 2]
	}],
	"tilesets": [{
		"firstgid": 1, "name": "terrain", "image": "terrain.png", "imagewidth": 32, "imageheight": 16,
		"tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 2,
		"tiles": [{"id": 1, "properties": [{"name": "kind", "type": "string", "value": "stone"}]}]
	}]
}`

// LoadTiled reads a map exported from Tiled, with its layers, tilesets and properties.
func ExampleLoadTiled() {
	m, err := tilemap.LoadTiled([]byte(level), tilemap.TilesetLoader{
		LoadImage: func(path string) (core.ImageSource, error) { return core.ImageSource{URI: "asset:" + path}, nil },
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m.Width, m.Height, m.Layers[0].Name, m.TileAt(0, 3, 1))
	fmt.Println(m.TileProperties(2)["kind"].Value)
	fmt.Println(m.WorldToTile(core.Point{X: 40, Y: 20}))
	// Output:
	// 4 3 ground 2
	// stone
	// 2 1
}

// SetFlags marks tile kinds as solid, and Raycast finds the first solid tile along a line, as a line-of-sight or ground check does.
func ExampleTilemap_Raycast() {
	m := tilemap.New(8, 4, 16, 16, tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:terrain.png"}, 16, 16, 2, 2))
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 8, 4, make([]uint32, 8*4)))
	m.FillRect(0, 5, 0, 1, 4, 2) // a wall at column 5
	m.SetFlags(2, tilemap.TileSolid)

	hit, ok := m.Raycast(core.Point{X: 8, Y: 24}, core.Point{X: 120, Y: 24}, tilemap.TileSolid)
	fmt.Println(ok, hit.Col, hit.Row, hit.Point, hit.Normal)
	// Output:
	// true 5 1 {80 24} {-1 0}
}

// A Camera follows a target smoothly, stays inside its bounds and converts between world and view points.
func ExampleCamera() {
	cam := &tilemap.Camera{Width: 320, Height: 180, Zoom: 1, Bounds: &core.Bounds{Size: core.Size{Width: 640, Height: 360}}}
	cam.Follow(core.Point{X: 600, Y: 100}, 0, time.Second/60)
	cam.Clamp()
	fmt.Println(cam.X, cam.Y, cam.VisibleBounds())
	fmt.Println(cam.WorldToView(core.Point{X: 600, Y: 100}))
	// Output:
	// 480 100 {{320 10} {320 180}}
	// {280 90}
}

// slopes is a Tiled map whose "collision" object layer holds a polyline drawn as the level's ground: flat, down a slope, and flat again.
const slopes = `{
	"type": "map", "orientation": "orthogonal", "renderorder": "right-down",
	"width": 20, "height": 10, "tilewidth": 16, "tileheight": 16, "infinite": false,
	"layers": [{
		"type": "objectgroup", "name": "collision", "x": 0, "y": 0, "opacity": 1, "visible": false,
		"objects": [{
			"id": 1, "name": "", "type": "ground", "x": 0, "y": 80, "width": 0, "height": 0, "rotation": 0, "visible": true,
			"polyline": [{"x": 0, "y": 0}, {"x": 96, "y": 0}, {"x": 224, "y": 64}, {"x": 320, "y": 64}]
		}]
	}],
	"tilesets": []
}`

// PhysicsBodies turns a Tiled polyline into static terrain: a ball dropped on the map's ground line lands on it and rolls down its slope onto the lower floor.
func ExampleObjectLayer_PhysicsBodies() {
	m, err := tilemap.LoadTiled([]byte(slopes), tilemap.TilesetLoader{})
	if err != nil {
		fmt.Println(err)
		return
	}
	terrain := m.Objects[0].PhysicsBodies()
	fmt.Println(len(terrain), terrain[0].Tag, terrain[0].Shape.Kind == physics.ShapeChain)

	w := &physics.RigidWorld{GravityY: 600}
	for _, b := range terrain {
		w.AddBody(physics.NewStaticRigidBody(b.Shape))
	}
	ball := physics.NewRigidCircle(40, 40, 8, 1)
	w.AddBody(ball)
	for range 200 {
		ball.VX = min(ball.VX+2, 80) // a gentle push to the right
		w.Step(time.Second / 60)
	}
	fmt.Printf("%.0f %v\n", ball.Y, ball.X > 224)
	// Output:
	// 1 ground true
	// 136 true
}

// caveTileset is a YAML tileset (*.tileset.yaml): a grid image whose tile 0 is solid and whose tile 2 is a slope.
const caveTileset = `version: 1
name: cave
tileWidth: 16
tileHeight: 16
image: tiles/cave.png
columns: 4
tileCount: 4
tiles:
  - id: 0
    solid: true
    properties:
      kind: stone
  - id: 2
    shapes:
      - kind: polygon
        points: [[0, 16], [16, 0], [16, 16]]
`

// ParseTileset reads a YAML tileset, and AddTilesetDoc builds it into a map's tileset with its flags, properties and collision shapes.
func ExampleTilemap_AddTilesetDoc() {
	doc, err := tilemap.ParseTileset([]byte(caveTileset))
	if err != nil {
		fmt.Println(err)
		return
	}
	m := tilemap.New(3, 1, 16, 16)
	if _, err := m.AddTilesetDoc(doc, 1, tilemap.TilesetAssets{}); err != nil {
		fmt.Println(err)
		return
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 3, 1, []uint32{1, 0, 3}))
	fmt.Println(m.FlagsAt(0, 0) == tilemap.TileSolid, m.TileProperties(1)["kind"].Value)
	fmt.Println(m.TileShapes(3)[0].Points)
	// Output:
	// true stone
	// [{0 16} {16 0} {16 16}]
}
