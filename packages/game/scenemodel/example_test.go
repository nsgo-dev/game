package scenemodel_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
)

// A scene file places built-in primitives; Build gives each object's node its geometry and bounds, ready for a Renderer or render3d.Queue.
func ExampleBuild() {
	doc, err := sceneformat.Parse([]byte(`version: 2
layers:
  - name: level
    objects:
      - id: floor
        scaleX: 10
        scaleZ: 10
        mesh: plane
      - id: pillar
        x: 2
        y: 1.5
        scaleY: 3
        mesh: cylinder
`))
	if err != nil {
		panic(err)
	}
	sc := scene3d.NewScene()
	built, err := scenemodel.Build(doc, sc.Root(), nil) // no models, so no ModelResolver
	if err != nil {
		panic(err)
	}
	for _, d := range built.Drawables {
		b := d.Node.WorldBounds()
		fmt.Printf("%s: %d triangles, bounds %v to %v\n", d.Object, len(d.Parts[0].Mesh.Indices)/3, b.Min, b.Max)
	}
	fmt.Println(len(built.AppendDrawItems(nil)), "draw items")
	// Output:
	// floor: 2 triangles, bounds {-5 0 -5} to {5 0 5}
	// pillar: 128 triangles, bounds {1.5 0 -0.5} to {2.5 3 0.5}
	// 2 draw items
}
