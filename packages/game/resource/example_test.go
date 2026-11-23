package resource_test

import (
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/resource"
)

const stoneMaterial = `type: material
version: 1
name: Stone
baseColor: '#8a8f99'
opacity: 0.75
blend: multiply
`

// A Library shares one value per path, so everything that loads a material holds the same pointer, and Reload changes it in place for all of them.
func ExampleLibrary() {
	lib := resource.NewLibrary(fstest.MapFS{
		"materials/stone.material.yaml": {Data: []byte(stoneMaterial)},
	})
	wall, err := lib.Material("materials/stone.material.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	floor, _ := lib.Material("materials/stone.material.yaml")
	fmt.Println(wall == floor, wall.Name, wall.EffectiveOpacity(), wall.Blend)

	// An edit to the file, as hotreload.Watcher.WatchResources delivers it, reaches every holder.
	loaded, err := lib.Reload("materials/stone.material.yaml", []byte("type: material\nversion: 1\nname: Mossy stone\nopacity: 0.5\n"))
	fmt.Println(loaded, err, floor.Name, floor.EffectiveOpacity(), lib.Revision())
	// Output:
	// true Stone 0.75 multiply
	// true <nil> Mossy stone 0.5 1
}

// ParseAs reads a resource file of an expected type; Eval samples a curve between its keys with each key's interpolation.
func ExampleParseAs() {
	curve, err := resource.ParseAs[*resource.Curve]([]byte(`type: curve
version: 1
name: Jump height
keys:
  - time: 0
    value: 0
    interp: smooth
  - time: 0.5
    value: 1
  - time: 1
    value: 0
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(curve.Name, curve.Eval(0.25), curve.Eval(0.5), curve.Eval(0.75), curve.Eval(2))

	_, err = resource.ParseAs[*resource.Curve]([]byte(stoneMaterial))
	fmt.Println(err != nil)
	// Output:
	// Jump height 0.5 1 0.5 0
	// true
}

// Encode writes a resource with its header and fields in a fixed order, so a file Encode wrote parses and encodes back to the same bytes; Parse refuses unknown fields.
func ExampleEncode() {
	m := &resource.Material{Header: resource.NewHeader(resource.KindMaterial), Name: "Glass", Blend: "screen"}
	out, err := resource.Encode(m)
	fmt.Print(string(out))
	fmt.Println(err)

	r, err := resource.Parse(out)
	fmt.Println(r.Kind(), err)
	_, err = resource.Parse([]byte(stoneMaterial + "shininess: 3\n"))
	fmt.Println(err != nil)
	// Output:
	// type: material
	// version: 1
	// name: Glass
	// blend: screen
	// <nil>
	// material <nil>
	// true
}
